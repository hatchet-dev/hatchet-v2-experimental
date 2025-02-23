package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/go-co-op/gocron/v2"
	"github.com/google/uuid"
	"github.com/hashicorp/go-multierror"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"

	"github.com/hatchet-dev/hatchet/internal/cel"
	"github.com/hatchet-dev/hatchet/internal/datautils"
	"github.com/hatchet-dev/hatchet/internal/msgqueue"
	"github.com/hatchet-dev/hatchet/internal/queueutils"
	"github.com/hatchet-dev/hatchet/internal/services/partition"
	"github.com/hatchet-dev/hatchet/internal/services/shared/recoveryutils"
	"github.com/hatchet-dev/hatchet/internal/services/shared/tasktypes"
	"github.com/hatchet-dev/hatchet/pkg/config/shared"
	hatcheterrors "github.com/hatchet-dev/hatchet/pkg/errors"
	"github.com/hatchet-dev/hatchet/pkg/logger"
	"github.com/hatchet-dev/hatchet/pkg/repository"
	"github.com/hatchet-dev/hatchet/pkg/repository/prisma/sqlchelpers"
	v2 "github.com/hatchet-dev/hatchet/pkg/repository/v2"
	"github.com/hatchet-dev/hatchet/pkg/repository/v2/sqlcv2"
	"github.com/hatchet-dev/hatchet/pkg/repository/v2/timescalev2"
)

type TasksController interface {
	Start(ctx context.Context) error
}

type TasksControllerImpl struct {
	mq                     msgqueue.MessageQueue
	pubBuffer              *msgqueue.MQPubBuffer
	l                      *zerolog.Logger
	queueLogger            *zerolog.Logger
	pgxStatsLogger         *zerolog.Logger
	repo                   repository.EngineRepository
	repov2                 v2.Repository
	dv                     datautils.DataDecoderValidator
	s                      gocron.Scheduler
	a                      *hatcheterrors.Wrapped
	p                      *partition.Partition
	celParser              *cel.CELParser
	timeoutTaskOperations  *queueutils.OperationPool
	reassignTaskOperations *queueutils.OperationPool
}

type TasksControllerOpt func(*TasksControllerOpts)

type TasksControllerOpts struct {
	mq             msgqueue.MessageQueue
	l              *zerolog.Logger
	repo           repository.EngineRepository
	repov2         v2.Repository
	dv             datautils.DataDecoderValidator
	alerter        hatcheterrors.Alerter
	p              *partition.Partition
	queueLogger    *zerolog.Logger
	pgxStatsLogger *zerolog.Logger
}

func defaultTasksControllerOpts() *TasksControllerOpts {
	l := logger.NewDefaultLogger("tasks-controller")
	alerter := hatcheterrors.NoOpAlerter{}

	queueLogger := logger.NewDefaultLogger("queue")
	pgxStatsLogger := logger.NewDefaultLogger("pgx-stats")

	return &TasksControllerOpts{
		l:              &l,
		dv:             datautils.NewDataDecoderValidator(),
		alerter:        alerter,
		queueLogger:    &queueLogger,
		pgxStatsLogger: &pgxStatsLogger,
	}
}

func WithMessageQueue(mq msgqueue.MessageQueue) TasksControllerOpt {
	return func(opts *TasksControllerOpts) {
		opts.mq = mq
	}
}

func WithLogger(l *zerolog.Logger) TasksControllerOpt {
	return func(opts *TasksControllerOpts) {
		opts.l = l
	}
}

func WithQueueLoggerConfig(lc *shared.LoggerConfigFile) TasksControllerOpt {
	return func(opts *TasksControllerOpts) {
		l := logger.NewStdErr(lc, "queue")
		opts.queueLogger = &l
	}
}

func WithPgxStatsLoggerConfig(lc *shared.LoggerConfigFile) TasksControllerOpt {
	return func(opts *TasksControllerOpts) {
		l := logger.NewStdErr(lc, "pgx-stats")
		opts.pgxStatsLogger = &l
	}
}

func WithAlerter(a hatcheterrors.Alerter) TasksControllerOpt {
	return func(opts *TasksControllerOpts) {
		opts.alerter = a
	}
}

func WithRepository(r repository.EngineRepository) TasksControllerOpt {
	return func(opts *TasksControllerOpts) {
		opts.repo = r
	}
}

func WithV2Repository(r v2.Repository) TasksControllerOpt {
	return func(opts *TasksControllerOpts) {
		opts.repov2 = r
	}
}

func WithPartition(p *partition.Partition) TasksControllerOpt {
	return func(opts *TasksControllerOpts) {
		opts.p = p
	}
}

func WithDataDecoderValidator(dv datautils.DataDecoderValidator) TasksControllerOpt {
	return func(opts *TasksControllerOpts) {
		opts.dv = dv
	}
}

func New(fs ...TasksControllerOpt) (*TasksControllerImpl, error) {
	opts := defaultTasksControllerOpts()

	for _, f := range fs {
		f(opts)
	}

	if opts.mq == nil {
		return nil, fmt.Errorf("task queue is required. use WithMessageQueue")
	}

	if opts.repov2 == nil {
		return nil, fmt.Errorf("v2 repository is required. use WithV2Repository")
	}

	if opts.repo == nil {
		return nil, fmt.Errorf("repository is required. use WithRepository")
	}

	if opts.p == nil {
		return nil, errors.New("partition is required. use WithPartition")
	}

	newLogger := opts.l.With().Str("service", "tasks-controller").Logger()
	opts.l = &newLogger

	s, err := gocron.NewScheduler(gocron.WithLocation(time.UTC))

	if err != nil {
		return nil, fmt.Errorf("could not create scheduler: %w", err)
	}

	a := hatcheterrors.NewWrapped(opts.alerter)
	a.WithData(map[string]interface{}{"service": "tasks-controller"})

	pubBuffer := msgqueue.NewMQPubBuffer(opts.mq)

	t := &TasksControllerImpl{
		mq:             opts.mq,
		pubBuffer:      pubBuffer,
		l:              opts.l,
		queueLogger:    opts.queueLogger,
		pgxStatsLogger: opts.pgxStatsLogger,
		repo:           opts.repo,
		repov2:         opts.repov2,
		dv:             opts.dv,
		s:              s,
		a:              a,
		p:              opts.p,
		celParser:      cel.NewCELParser(),
	}

	t.timeoutTaskOperations = queueutils.NewOperationPool(opts.l, time.Second*5, "timeout step runs", t.processTaskTimeouts)
	t.reassignTaskOperations = queueutils.NewOperationPool(opts.l, time.Second*5, "reassign step runs", t.processTaskReassignments)

	return t, nil
}

func (tc *TasksControllerImpl) Start() (func() error, error) {
	mqBuffer := msgqueue.NewMQSubBuffer(msgqueue.TASK_PROCESSING_QUEUE, tc.mq, tc.handleBufferedMsgs)
	wg := sync.WaitGroup{}

	tc.s.Start()

	cleanupBuffer, err := mqBuffer.Start()

	if err != nil {
		return nil, fmt.Errorf("could not start message queue buffer: %w", err)
	}

	// always create table partition on startup
	if err := tc.createTablePartition(context.Background()); err != nil {
		return nil, fmt.Errorf("could not create table partition: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	_, err = tc.s.NewJob(
		gocron.DurationJob(time.Second*1),
		gocron.NewTask(
			tc.runTenantTimeoutTasks(ctx),
		),
	)

	if err != nil {
		cancel()
		return nil, fmt.Errorf("could not schedule step run timeout: %w", err)
	}

	_, err = tc.s.NewJob(
		gocron.DurationJob(time.Second*1),
		gocron.NewTask(
			tc.runTenantReassignTasks(ctx),
		),
	)

	if err != nil {
		cancel()
		return nil, fmt.Errorf("could not schedule step run reassignment: %w", err)
	}

	_, err = tc.s.NewJob(
		gocron.DurationJob(time.Minute*15),
		gocron.NewTask(
			tc.runTaskTablePartition(ctx),
		),
	)

	if err != nil {
		cancel()
		return nil, fmt.Errorf("could not schedule task partition method: %w", err)
	}

	cleanup := func() error {
		cancel()

		if err := cleanupBuffer(); err != nil {
			return err
		}

		if err := tc.s.Shutdown(); err != nil {
			return fmt.Errorf("could not shutdown scheduler: %w", err)
		}

		wg.Wait()

		return nil
	}

	return cleanup, nil
}

func (tc *TasksControllerImpl) handleBufferedMsgs(tenantId, msgId string, payloads [][]byte) (err error) {
	defer func() {
		if r := recover(); r != nil {
			recoverErr := recoveryutils.RecoverWithAlert(tc.l, tc.a, r)

			if recoverErr != nil {
				err = recoverErr
			}
		}
	}()

	switch msgId {
	case "task-completed":
		return tc.handleTaskCompleted(context.Background(), tenantId, payloads)
	case "task-failed":
		return tc.handleTaskFailed(context.Background(), tenantId, payloads)
	case "task-cancelled":
		return tc.handleTaskCancelled(context.Background(), tenantId, payloads)
	case "user-event":
		return tc.handleProcessUserEvents(context.Background(), tenantId, payloads)
	case "internal-event":
		return tc.handleProcessInternalEvents(context.Background(), tenantId, payloads)
	case "task-trigger":
		return tc.handleProcessTaskTrigger(context.Background(), tenantId, payloads)
	}

	return fmt.Errorf("unknown message id: %s", msgId)
}

func (tc *TasksControllerImpl) handleTaskCompleted(ctx context.Context, tenantId string, payloads [][]byte) error {
	opts := make([]v2.TaskIdRetryCount, 0)
	idsToData := make(map[int64][]byte)

	msgs := msgqueue.JSONConvert[tasktypes.CompletedTaskPayload](payloads)

	for _, msg := range msgs {
		opts = append(opts, v2.TaskIdRetryCount{
			Id:         msg.TaskId,
			RetryCount: msg.RetryCount,
		})

		idsToData[msg.TaskId] = msg.Output
	}

	releasedTasks, err := tc.repov2.Tasks().CompleteTasks(ctx, tenantId, opts)

	if err != nil {
		return err
	}

	internalEvents := make([]tasktypes.InternalEventTaskPayload, 0)

	for _, task := range releasedTasks {
		taskExternalId := sqlchelpers.UUIDToStr(task.ExternalID)

		data := v2.CompletedData{
			StepReadableId: task.StepReadableID,
			Output:         idsToData[task.ID],
		}

		dataBytes, _ := json.Marshal(data)

		internalEvents = append(internalEvents, tasktypes.InternalEventTaskPayload{
			EventTimestamp: time.Now(),
			EventKey:       v2.GetTaskCompletedEventKey(taskExternalId),
			EventData:      dataBytes,
		})
	}

	tc.notifyQueuesOnCompletion(ctx, tenantId, releasedTasks)

	return tc.sendInternalEvents(ctx, tenantId, internalEvents)
}

func (tc *TasksControllerImpl) handleTaskFailed(ctx context.Context, tenantId string, payloads [][]byte) error {
	opts := make([]v2.FailTaskOpts, 0)

	msgs := msgqueue.JSONConvert[tasktypes.FailedTaskPayload](payloads)
	idsToErrorMsg := make(map[int64]string)

	for _, msg := range msgs {
		opts = append(opts, v2.FailTaskOpts{
			TaskIdRetryCount: &v2.TaskIdRetryCount{
				Id:         msg.TaskId,
				RetryCount: msg.RetryCount,
			},
			IsAppError: msg.IsAppError,
		})

		if msg.ErrorMsg != "" {
			idsToErrorMsg[msg.TaskId] = msg.ErrorMsg
		}
	}

	retriedTasks, failedTasks, err := tc.repov2.Tasks().FailTasks(ctx, tenantId, opts)

	if err != nil {
		return err
	}

	retriedTaskIds := make(map[int64]struct{})

	for _, task := range retriedTasks {
		retriedTaskIds[task.Id] = struct{}{}
	}

	internalEvents := make([]tasktypes.InternalEventTaskPayload, 0)

	for _, task := range failedTasks {
		// if the task is retried, don't send a message to the trigger queue
		if _, ok := retriedTaskIds[task.ID]; ok {
			continue
		}

		taskExternalId := sqlchelpers.UUIDToStr(task.ExternalID)

		data := v2.FailedData{
			StepReadableId: task.StepReadableID,
			Error:          idsToErrorMsg[task.ID],
		}

		dataBytes, _ := json.Marshal(data)

		internalEvents = append(internalEvents, tasktypes.InternalEventTaskPayload{
			EventTimestamp: time.Now(),
			EventKey:       v2.GetTaskFailedEventKey(taskExternalId),
			EventData:      dataBytes,
		})
	}

	tc.notifyQueuesOnCompletion(ctx, tenantId, failedTasks)

	// TODO: MOVE THIS TO THE DATA LAYER?
	err = tc.sendInternalEvents(ctx, tenantId, internalEvents)

	if err != nil {
		return err
	}

	var outerErr error

	// send retried tasks to the olap repository
	for _, task := range retriedTasks {
		taskId := task.Id

		olapMsg, err := tasktypes.MonitoringEventMessageFromInternal(
			tenantId,
			tasktypes.CreateMonitoringEventPayload{
				TaskId:         taskId,
				RetryCount:     task.RetryCount,
				EventType:      timescalev2.V2EventTypeOlapQUEUED,
				EventTimestamp: time.Now(),
			},
		)

		if err != nil {
			outerErr = multierror.Append(outerErr, fmt.Errorf("could not create monitoring event message: %w", err))
			continue
		}

		err = tc.pubBuffer.Pub(
			ctx,
			msgqueue.OLAP_QUEUE,
			olapMsg,
			false,
		)

		if err != nil {
			outerErr = multierror.Append(outerErr, fmt.Errorf("could not publish monitoring event message: %w", err))
		}
	}

	return outerErr
}

func (tc *TasksControllerImpl) handleTaskCancelled(ctx context.Context, tenantId string, payloads [][]byte) error {
	opts := make([]v2.TaskIdRetryCount, 0)

	msgs := msgqueue.JSONConvert[tasktypes.CancelledTaskPayload](payloads)
	shouldTasksNotify := make(map[int64]bool)

	for _, msg := range msgs {
		opts = append(opts, v2.TaskIdRetryCount{
			Id:         msg.TaskId,
			RetryCount: msg.RetryCount,
		})

		shouldTasksNotify[msg.TaskId] = msg.ShouldNotify
	}

	releasedTasks, allTasks, err := tc.repov2.Tasks().CancelTasks(ctx, tenantId, opts)

	if err != nil {
		return err
	}

	tasksToSendToDispatcher := make([]tasktypes.SignalTaskCancelledPayload, 0)

	for _, task := range releasedTasks {
		if shouldTasksNotify[task.ID] {
			tasksToSendToDispatcher = append(tasksToSendToDispatcher, tasktypes.SignalTaskCancelledPayload{
				TaskId:     task.ID,
				WorkerId:   sqlchelpers.UUIDToStr(task.WorkerID),
				RetryCount: task.RetryCount,
			})
		}
	}

	// send task cancellations to the dispatcher
	err = tc.sendTaskCancellationsToDispatcher(ctx, tenantId, tasksToSendToDispatcher)

	if err != nil {
		return fmt.Errorf("could not send task cancellations to dispatcher: %w", err)
	}

	internalEvents := make([]tasktypes.InternalEventTaskPayload, 0)

	for _, task := range allTasks {
		taskExternalId := sqlchelpers.UUIDToStr(task.ExternalID)

		data := v2.FailedData{
			StepReadableId: task.StepReadableID,
			Error:          "task was cancelled",
		}

		dataBytes, _ := json.Marshal(data)

		internalEvents = append(internalEvents, tasktypes.InternalEventTaskPayload{
			EventTimestamp: time.Now(),
			EventKey:       v2.GetTaskCancelledEventKey(taskExternalId),
			EventData:      dataBytes,
		})
	}

	tc.notifyQueuesOnCompletion(ctx, tenantId, releasedTasks)

	// TODO: MOVE THIS TO THE DATA LAYER?
	err = tc.sendInternalEvents(ctx, tenantId, internalEvents)

	if err != nil {
		return err
	}

	var outerErr error

	for _, msg := range msgs {
		taskId := msg.TaskId

		olapMsg, err := tasktypes.MonitoringEventMessageFromInternal(
			tenantId,
			tasktypes.CreateMonitoringEventPayload{
				TaskId:         taskId,
				RetryCount:     msg.RetryCount,
				EventType:      msg.EventType,
				EventTimestamp: time.Now(),
			},
		)

		if err != nil {
			outerErr = multierror.Append(outerErr, fmt.Errorf("could not create monitoring event message: %w", err))
			continue
		}

		err = tc.pubBuffer.Pub(
			ctx,
			msgqueue.OLAP_QUEUE,
			olapMsg,
			false,
		)

		if err != nil {
			outerErr = multierror.Append(outerErr, fmt.Errorf("could not publish monitoring event message: %w", err))
		}
	}

	return err
}

func (tc *TasksControllerImpl) sendTaskCancellationsToDispatcher(ctx context.Context, tenantId string, releasedTasks []tasktypes.SignalTaskCancelledPayload) error {
	workerIds := make([]string, 0)

	for _, task := range releasedTasks {
		workerIds = append(workerIds, task.WorkerId)
	}

	dispatcherIdWorkerIds, err := tc.repo.Worker().GetDispatcherIdsForWorkers(ctx, tenantId, workerIds)

	if err != nil {
		return fmt.Errorf("could not list dispatcher ids for workers: %w", err)
	}

	workerIdToDispatcherId := make(map[string]string)

	for dispatcherId, workerIds := range dispatcherIdWorkerIds {
		for _, workerId := range workerIds {
			workerIdToDispatcherId[workerId] = dispatcherId
		}
	}

	// assemble messages
	dispatcherIdsToPayloads := make(map[string][]tasktypes.SignalTaskCancelledPayload)

	for _, task := range releasedTasks {
		workerId := task.WorkerId
		dispatcherId := workerIdToDispatcherId[workerId]

		dispatcherIdsToPayloads[dispatcherId] = append(dispatcherIdsToPayloads[dispatcherId], task)
	}

	// send messages
	for dispatcherId, payloads := range dispatcherIdsToPayloads {
		msg, err := msgqueue.NewTenantMessage(
			tenantId,
			"task-cancelled",
			false,
			true,
			payloads...,
		)

		if err != nil {
			return fmt.Errorf("could not create message for task cancellation: %w", err)
		}

		err = tc.mq.SendMessage(
			ctx,
			msgqueue.QueueTypeFromDispatcherID(dispatcherId),
			msg,
		)

		if err != nil {
			return fmt.Errorf("could not send message for task cancellation: %w", err)
		}
	}

	return nil
}

func (tc *TasksControllerImpl) notifyQueuesOnCompletion(ctx context.Context, tenantId string, releasedTasks []*sqlcv2.ReleaseTasksRow) {
	tenant, err := tc.repo.Tenant().GetTenantByID(ctx, tenantId)

	if err != nil {
		tc.l.Err(err).Msg("could not get tenant")
		return
	}

	if tenant.SchedulerPartitionId.Valid {
		msg, err := tasktypes.NotifyTaskReleased(tenantId, releasedTasks)

		if err != nil {
			tc.l.Err(err).Msg("could not create message for scheduler partition queue")
		} else {
			err = tc.mq.SendMessage(
				ctx,
				msgqueue.QueueTypeFromPartitionIDAndController(tenant.SchedulerPartitionId.String, msgqueue.Scheduler),
				msg,
			)

			if err != nil {
				tc.l.Err(err).Msg("could not add message to scheduler partition queue")
			}
		}
	}
}

// handleProcessUserEvents is responsible for inserting tasks into the database based on event triggers.
func (tc *TasksControllerImpl) handleProcessUserEvents(ctx context.Context, tenantId string, payloads [][]byte) error {
	msgs := msgqueue.JSONConvert[tasktypes.UserEventTaskPayload](payloads)

	eg := &errgroup.Group{}

	// TODO: RUN IN THE SAME TRANSACTION
	eg.Go(func() error {
		return tc.handleProcessUserEventTrigger(ctx, tenantId, msgs)
	})

	eg.Go(func() error {
		return tc.handleProcessUserEventMatches(ctx, tenantId, msgs)
	})

	return eg.Wait()
}

// handleProcessEventTrigger is responsible for inserting tasks into the database based on event triggers.
func (tc *TasksControllerImpl) handleProcessUserEventTrigger(ctx context.Context, tenantId string, msgs []*tasktypes.UserEventTaskPayload) error {
	opts := make([]v2.EventTriggerOpts, 0, len(msgs))

	for _, msg := range msgs {
		opts = append(opts, v2.EventTriggerOpts{
			EventId:            msg.EventId,
			Key:                msg.EventKey,
			Data:               msg.EventData,
			AdditionalMetadata: msg.EventAdditionalMetadata,
		})
	}

	tasks, dags, err := tc.repov2.Triggers().TriggerFromEvents(ctx, tenantId, opts)

	if err != nil {
		return fmt.Errorf("could not trigger tasks from events: %w", err)
	}

	eg := &errgroup.Group{}

	eg.Go(func() error {
		return tc.signalTasksCreated(ctx, tenantId, tasks)
	})

	eg.Go(func() error {
		return tc.signalDAGsCreated(ctx, tenantId, dags)
	})

	return eg.Wait()
}

// handleProcessUserEventMatches is responsible for signaling or creating tasks based on user event matches.
func (tc *TasksControllerImpl) handleProcessUserEventMatches(ctx context.Context, tenantId string, payloads []*tasktypes.UserEventTaskPayload) error {
	// tc.l.Error().Msg("not implemented")
	return nil
}

// handleProcessEventTrigger is responsible for inserting tasks into the database based on event triggers.
func (tc *TasksControllerImpl) handleProcessInternalEvents(ctx context.Context, tenantId string, payloads [][]byte) error {
	msgs := msgqueue.JSONConvert[tasktypes.InternalEventTaskPayload](payloads)

	return tc.processInternalEvents(ctx, tenantId, msgs)
}

// handleProcessEventTrigger is responsible for inserting tasks into the database based on event triggers.
func (tc *TasksControllerImpl) handleProcessTaskTrigger(ctx context.Context, tenantId string, payloads [][]byte) error {
	msgs := msgqueue.JSONConvert[tasktypes.TriggerTaskPayload](payloads)

	opts := make([]v2.WorkflowNameTriggerOpts, 0, len(msgs))

	for _, msg := range msgs {
		opts = append(opts, v2.WorkflowNameTriggerOpts{
			WorkflowName:       msg.WorkflowName,
			ExternalId:         msg.TaskExternalId,
			Data:               msg.Data,
			AdditionalMetadata: msg.AdditionalMetadata,
			ParentTaskId:       msg.ParentTaskId,
			ChildIndex:         msg.ChildIndex,
			ChildKey:           msg.ChildKey,
		})
	}

	tasks, dags, err := tc.repov2.Triggers().TriggerFromWorkflowNames(ctx, tenantId, opts)

	if err != nil {
		return fmt.Errorf("could not trigger workflows from names: %w", err)
	}

	eg := &errgroup.Group{}

	eg.Go(func() error {
		return tc.signalTasksCreated(ctx, tenantId, tasks)
	})

	eg.Go(func() error {
		return tc.signalDAGsCreated(ctx, tenantId, dags)
	})

	return eg.Wait()
}

func (tc *TasksControllerImpl) sendInternalEvents(ctx context.Context, tenantId string, events []tasktypes.InternalEventTaskPayload) error {
	if len(events) == 0 {
		return nil
	}

	msg, err := tasktypes.NewInternalEventMessage(tenantId, time.Now(), events...)

	if err != nil {
		return fmt.Errorf("could not create internal event message: %w", err)
	}

	return tc.mq.SendMessage(
		ctx,
		msgqueue.TASK_PROCESSING_QUEUE,
		msg,
	)
}

// handleProcessUserEventMatches is responsible for triggering tasks based on user event matches.
func (tc *TasksControllerImpl) processInternalEvents(ctx context.Context, tenantId string, events []*tasktypes.InternalEventTaskPayload) error {
	candidateMatches := make([]v2.CandidateEventMatch, 0)

	for _, event := range events {
		candidateMatches = append(candidateMatches, v2.CandidateEventMatch{
			ID:             uuid.NewString(),
			EventTimestamp: event.EventTimestamp,
			Key:            event.EventKey,
			Data:           event.EventData,
		})
	}

	matchResult, err := tc.repov2.Matches().ProcessInternalEventMatches(ctx, tenantId, candidateMatches)

	if err != nil {
		return fmt.Errorf("could not process internal event matches: %w", err)
	}

	if len(matchResult.CreatedTasks) > 0 {
		err = tc.signalTasksCreated(ctx, tenantId, matchResult.CreatedTasks)

		if err != nil {
			return fmt.Errorf("could not signal created tasks: %w", err)
		}
	}

	return nil
}

func (tc *TasksControllerImpl) signalDAGsCreated(ctx context.Context, tenantId string, dags []*v2.DAGWithData) error {
	// notify that tasks have been created
	// TODO: make this transactionally safe?
	for _, dag := range dags {
		dagCp := dag
		msg, err := tasktypes.CreatedDAGMessage(tenantId, dagCp)

		if err != nil {
			tc.l.Err(err).Msg("could not create message for olap queue")
			continue
		}

		err = tc.pubBuffer.Pub(
			ctx,
			msgqueue.OLAP_QUEUE,
			msg,
			false,
		)

		if err != nil {
			tc.l.Err(err).Msg("could not add message to olap queue")
			continue
		}
	}

	return nil
}

func (tc *TasksControllerImpl) signalTasksCreated(ctx context.Context, tenantId string, tasks []*sqlcv2.V2Task) error {
	// group tasks by initial states
	queuedTasks := make([]*sqlcv2.V2Task, 0)
	failedTasks := make([]*sqlcv2.V2Task, 0)
	cancelledTasks := make([]*sqlcv2.V2Task, 0)
	skippedTasks := make([]*sqlcv2.V2Task, 0)

	for _, task := range tasks {
		switch task.InitialState {
		case sqlcv2.V2TaskInitialStateQUEUED:
			queuedTasks = append(queuedTasks, task)
		case sqlcv2.V2TaskInitialStateFAILED:
			failedTasks = append(failedTasks, task)
		case sqlcv2.V2TaskInitialStateCANCELLED:
			cancelledTasks = append(cancelledTasks, task)
		case sqlcv2.V2TaskInitialStateSKIPPED:
			skippedTasks = append(skippedTasks, task)
		}

		msg, err := tasktypes.CreatedTaskMessage(tenantId, task)

		if err != nil {
			tc.l.Err(err).Msg("could not create message for olap queue")
			continue
		}

		err = tc.pubBuffer.Pub(
			ctx,
			msgqueue.OLAP_QUEUE,
			msg,
			false,
		)

		if err != nil {
			tc.l.Err(err).Msg("could not add message to olap queue")
			continue
		}
	}

	eg := &errgroup.Group{}

	if len(queuedTasks) > 0 {
		eg.Go(func() error {
			err := tc.signalTasksCreatedAndQueued(ctx, tenantId, queuedTasks)

			if err != nil {
				return fmt.Errorf("could not signal created tasks: %w", err)
			}

			return nil
		})
	}

	if len(failedTasks) > 0 {
		eg.Go(func() error {
			err := tc.signalTasksCreatedAndFailed(ctx, tenantId, failedTasks)

			if err != nil {
				return fmt.Errorf("could not signal created tasks: %w", err)
			}

			return nil
		})
	}

	if len(cancelledTasks) > 0 {
		eg.Go(func() error {
			err := tc.signalTasksCreatedAndCancelled(ctx, tenantId, cancelledTasks)

			if err != nil {
				return fmt.Errorf("could not signal created tasks: %w", err)
			}

			return nil
		})
	}

	if len(skippedTasks) > 0 {
		eg.Go(func() error {
			err := tc.signalTasksCreatedAndSkipped(ctx, tenantId, skippedTasks)

			if err != nil {
				return fmt.Errorf("could not signal created tasks: %w", err)
			}

			return nil
		})
	}

	return eg.Wait()
}

func (tc *TasksControllerImpl) signalTasksCreatedAndQueued(ctx context.Context, tenantId string, tasks []*sqlcv2.V2Task) error {
	// get all unique queues and notify them
	queues := make(map[string]struct{})

	for _, task := range tasks {
		queues[task.Queue] = struct{}{}
	}

	tenant, err := tc.repo.Tenant().GetTenantByID(ctx, tenantId)

	if err != nil {
		return err
	}

	if tenant.SchedulerPartitionId.Valid {
		msg, err := tasktypes.NotifyTaskCreated(tenantId, tasks)

		if err != nil {
			tc.l.Err(err).Msg("could not create message for scheduler partition queue")
		} else {
			err = tc.mq.SendMessage(
				ctx,
				msgqueue.QueueTypeFromPartitionIDAndController(tenant.SchedulerPartitionId.String, msgqueue.Scheduler),
				msg,
			)

			if err != nil {
				tc.l.Err(err).Msg("could not add message to scheduler partition queue")
			}
		}
	}

	// notify that tasks have been created
	// TODO: make this transactionally safe?
	for _, task := range tasks {
		msg := ""

		if len(task.ConcurrencyKeys) > 0 {
			msg = "concurrency keys evaluated as:"

			for _, key := range task.ConcurrencyKeys {
				msg += fmt.Sprintf(" %s", key)
			}
		}

		olapMsg, err := tasktypes.MonitoringEventMessageFromInternal(
			tenantId,
			tasktypes.CreateMonitoringEventPayload{
				TaskId:         task.ID,
				RetryCount:     0,
				EventType:      timescalev2.V2EventTypeOlapQUEUED,
				EventTimestamp: time.Now(),
				EventMessage:   msg,
			},
		)

		if err != nil {
			tc.l.Err(err).Msg("could not create monitoring event message")
			continue
		}

		err = tc.pubBuffer.Pub(
			ctx,
			msgqueue.OLAP_QUEUE,
			olapMsg,
			false,
		)

		if err != nil {
			tc.l.Err(err).Msg("could not add monitoring event message to olap queue")
			continue
		}
	}

	return nil
}

func (tc *TasksControllerImpl) signalTasksCreatedAndCancelled(ctx context.Context, tenantId string, tasks []*sqlcv2.V2Task) error {
	internalEvents := make([]tasktypes.InternalEventTaskPayload, 0)

	for _, task := range tasks {
		taskExternalId := sqlchelpers.UUIDToStr(task.ExternalID)

		data := v2.FailedData{
			StepReadableId: task.StepReadableID,
			Error:          "task was cancelled",
		}

		dataBytes, _ := json.Marshal(data)

		internalEvents = append(internalEvents, tasktypes.InternalEventTaskPayload{
			EventTimestamp: time.Now(),
			EventKey:       v2.GetTaskCancelledEventKey(taskExternalId),
			EventData:      dataBytes,
		})
	}

	err := tc.sendInternalEvents(ctx, tenantId, internalEvents)

	if err != nil {
		return err
	}

	// notify that tasks have been cancelled
	// TODO: make this transactionally safe?
	for _, task := range tasks {
		msg, err := tasktypes.MonitoringEventMessageFromInternal(tenantId, tasktypes.CreateMonitoringEventPayload{
			TaskId:         task.ID,
			RetryCount:     task.RetryCount,
			EventType:      timescalev2.V2EventTypeOlapCANCELLED,
			EventTimestamp: time.Now(),
		})

		if err != nil {
			tc.l.Err(err).Msg("could not create message for olap queue")
			continue
		}

		err = tc.pubBuffer.Pub(
			ctx,
			msgqueue.OLAP_QUEUE,
			msg,
			false,
		)

		if err != nil {
			tc.l.Err(err).Msg("could not add message to olap queue")
			continue
		}
	}

	return nil
}

func (tc *TasksControllerImpl) signalTasksCreatedAndFailed(ctx context.Context, tenantId string, tasks []*sqlcv2.V2Task) error {
	internalEvents := make([]tasktypes.InternalEventTaskPayload, 0)

	for _, task := range tasks {
		taskExternalId := sqlchelpers.UUIDToStr(task.ExternalID)

		data := v2.FailedData{
			StepReadableId: task.StepReadableID,
			Error:          task.InitialStateReason.String,
		}

		dataBytes, _ := json.Marshal(data)

		internalEvents = append(internalEvents, tasktypes.InternalEventTaskPayload{
			EventTimestamp: time.Now(),
			EventKey:       v2.GetTaskFailedEventKey(taskExternalId),
			EventData:      dataBytes,
		})
	}

	err := tc.sendInternalEvents(ctx, tenantId, internalEvents)

	if err != nil {
		return err
	}

	// notify that tasks have been cancelled
	// TODO: make this transactionally safe?
	for _, task := range tasks {
		msg, err := tasktypes.MonitoringEventMessageFromInternal(tenantId, tasktypes.CreateMonitoringEventPayload{
			TaskId:         task.ID,
			RetryCount:     task.RetryCount,
			EventType:      timescalev2.V2EventTypeOlapFAILED,
			EventPayload:   task.InitialStateReason.String,
			EventTimestamp: time.Now(),
		})

		if err != nil {
			tc.l.Err(err).Msg("could not create message for olap queue")
			continue
		}

		err = tc.pubBuffer.Pub(
			ctx,
			msgqueue.OLAP_QUEUE,
			msg,
			false,
		)

		if err != nil {
			tc.l.Err(err).Msg("could not add message to olap queue")
			continue
		}
	}

	return nil
}

func (tc *TasksControllerImpl) signalTasksCreatedAndSkipped(ctx context.Context, tenantId string, tasks []*sqlcv2.V2Task) error {
	internalEvents := make([]tasktypes.InternalEventTaskPayload, 0)

	for _, task := range tasks {
		taskExternalId := sqlchelpers.UUIDToStr(task.ExternalID)

		outputMap := map[string]bool{
			"skipped": true,
		}

		outputMapBytes, _ := json.Marshal(outputMap)

		data := v2.CompletedData{
			StepReadableId: task.StepReadableID,
			Output:         outputMapBytes,
		}

		dataBytes, _ := json.Marshal(data)

		internalEvents = append(internalEvents, tasktypes.InternalEventTaskPayload{
			EventTimestamp: time.Now(),
			EventKey:       v2.GetTaskCompletedEventKey(taskExternalId),
			EventData:      dataBytes,
		})
	}

	err := tc.sendInternalEvents(ctx, tenantId, internalEvents)

	if err != nil {
		return err
	}

	// notify that tasks have been cancelled
	// TODO: make this transactionally safe?
	for _, task := range tasks {
		msg, err := tasktypes.MonitoringEventMessageFromInternal(tenantId, tasktypes.CreateMonitoringEventPayload{
			TaskId:         task.ID,
			RetryCount:     task.RetryCount,
			EventType:      timescalev2.V2EventTypeOlapSKIPPED,
			EventTimestamp: time.Now(),
		})

		if err != nil {
			tc.l.Err(err).Msg("could not create message for olap queue")
			continue
		}

		err = tc.pubBuffer.Pub(
			ctx,
			msgqueue.OLAP_QUEUE,
			msg,
			false,
		)

		if err != nil {
			tc.l.Err(err).Msg("could not add message to olap queue")
			continue
		}
	}

	return nil
}

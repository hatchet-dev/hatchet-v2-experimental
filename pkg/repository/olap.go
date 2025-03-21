package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"

	"github.com/hatchet-dev/hatchet/api/v1/server/oas/gen"
	"github.com/hatchet-dev/hatchet/pkg/repository/olap"
	"github.com/hatchet-dev/hatchet/pkg/repository/prisma/sqlchelpers"
	v2 "github.com/hatchet-dev/hatchet/pkg/repository/v2"
	"github.com/hatchet-dev/hatchet/pkg/repository/v2/olapv2"
	"github.com/hatchet-dev/hatchet/pkg/repository/v2/sqlcv2"
)

// TODO: make this dynamic for the instance
const NUM_PARTITIONS = 4

type ListTaskRunOpts struct {
	CreatedAfter time.Time

	Statuses []gen.V2TaskStatus

	WorkflowIds []uuid.UUID

	WorkerId *uuid.UUID

	StartedAfter time.Time

	FinishedBefore *time.Time

	AdditionalMetadata map[string]interface{}

	Limit int64

	Offset int64
}

type ListWorkflowRunOpts struct {
	CreatedAfter time.Time

	Statuses []gen.V2TaskStatus

	WorkflowIds []uuid.UUID

	StartedAfter time.Time

	FinishedBefore *time.Time

	AdditionalMetadata map[string]interface{}

	Limit int64

	Offset int64
}

type ReadTaskRunMetricsOpts struct {
	CreatedAfter time.Time

	WorkflowIds []uuid.UUID
}

type WorkflowRunData struct {
	TenantID           pgtype.UUID                 `json:"tenant_id"`
	InsertedAt         pgtype.Timestamptz          `json:"inserted_at"`
	ExternalID         pgtype.UUID                 `json:"external_id"`
	ReadableStatus     olapv2.V2ReadableStatusOlap `json:"readable_status"`
	Kind               olapv2.V2RunKind            `json:"kind"`
	WorkflowID         pgtype.UUID                 `json:"workflow_id"`
	DisplayName        string                      `json:"display_name"`
	AdditionalMetadata []byte                      `json:"additional_metadata"`
	CreatedAt          pgtype.Timestamptz          `json:"created_at"`
	StartedAt          pgtype.Timestamptz          `json:"started_at"`
	FinishedAt         pgtype.Timestamptz          `json:"finished_at"`
	ErrorMessage       string                      `json:"error_message"`
	WorkflowVersionId  pgtype.UUID                 `json:"workflow_version_id"`
	Input              []byte                      `json:"input"`
}

type V2WorkflowRunPopulator struct {
	WorkflowRun  *WorkflowRunData
	TaskMetadata []TaskMetadata
}

type OLAPEventRepository interface {
	UpdateTablePartitions(ctx context.Context) error
	ReadTaskRun(ctx context.Context, taskExternalId string) (*olapv2.V2TasksOlap, error)
	ReadWorkflowRun(ctx context.Context, workflowRunExternalId pgtype.UUID) (*V2WorkflowRunPopulator, error)
	ReadTaskRunData(ctx context.Context, tenantId pgtype.UUID, taskId int64, taskInsertedAt pgtype.Timestamptz) (*olapv2.PopulateSingleTaskRunDataRow, *pgtype.UUID, error)
	ListTasks(ctx context.Context, tenantId string, opts ListTaskRunOpts) ([]*olapv2.PopulateTaskRunDataRow, int, error)
	ListWorkflowRuns(ctx context.Context, tenantId string, opts ListWorkflowRunOpts) ([]*WorkflowRunData, int, error)
	ListTaskRunEvents(ctx context.Context, tenantId string, taskId int64, taskInsertedAt pgtype.Timestamptz, limit, offset int64) ([]*olapv2.ListTaskEventsRow, error)
	ListTaskRunEventsByWorkflowRunId(ctx context.Context, tenantId string, workflowRunId pgtype.UUID) ([]*olapv2.ListTaskEventsForWorkflowRunRow, error)
	ReadTaskRunMetrics(ctx context.Context, tenantId string, opts ReadTaskRunMetricsOpts) ([]olap.TaskRunMetric, error)
	CreateTasks(ctx context.Context, tenantId string, tasks []*sqlcv2.V2Task) error
	CreateTaskEvents(ctx context.Context, tenantId string, events []olapv2.CreateTaskEventsOLAPParams) error
	CreateDAGs(ctx context.Context, tenantId string, dags []*v2.DAGWithData) error
	GetTaskPointMetrics(ctx context.Context, tenantId string, startTimestamp *time.Time, endTimestamp *time.Time, bucketInterval time.Duration) ([]*olapv2.GetTaskPointMetricsRow, error)
	UpdateTaskStatuses(ctx context.Context, tenantId string) (bool, error)
	UpdateDAGStatuses(ctx context.Context, tenantId string) (bool, error)
	ReadDAG(ctx context.Context, dagExternalId string) (*olapv2.V2DagsOlap, error)
	ListTasksByDAGId(ctx context.Context, tenantId string, dagIds []pgtype.UUID) ([]*olapv2.PopulateTaskRunDataRow, map[int64]uuid.UUID, error)
	ListTasksByIdAndInsertedAt(ctx context.Context, tenantId string, taskMetadata []TaskMetadata) ([]*olapv2.PopulateTaskRunDataRow, error)
}

type olapEventRepository struct {
	pool *pgxpool.Pool
	l    *zerolog.Logger

	eventCache *lru.Cache[string, bool]
	queries    *olapv2.Queries
}

func NewOLAPEventRepository(l *zerolog.Logger) OLAPEventRepository {
	timescaleUrl := os.Getenv("TIMESCALE_URL")

	if timescaleUrl == "" {
		log.Fatal("TIMESCALE_URL is not set")
	}

	timescaleConfig, err := pgxpool.ParseConfig(timescaleUrl)

	if err != nil {
		log.Fatal(err)
	}

	timescaleConfig.MaxConns = 150
	timescaleConfig.MinConns = 10
	timescaleConfig.MaxConnLifetime = 15 * 60 * time.Second

	timescalePool, err := pgxpool.NewWithConfig(context.Background(), timescaleConfig)

	if err != nil {
		log.Fatalf("Unable to create connection pool: %v\n", err)
	}

	eventCache, err := lru.New[string, bool](100000)

	if err != nil {
		log.Fatal(err)
	}

	queries := olapv2.New()

	return &olapEventRepository{
		pool:       timescalePool,
		l:          l,
		queries:    queries,
		eventCache: eventCache,
	}
}

func (o *olapEventRepository) UpdateTablePartitions(ctx context.Context) error {
	err := o.queries.CreateOLAPTaskEventTmpPartitions(ctx, o.pool, NUM_PARTITIONS)

	if err != nil {
		return err
	}

	err = o.queries.CreateOLAPTaskStatusUpdateTmpPartitions(ctx, o.pool, NUM_PARTITIONS)

	if err != nil {
		return err
	}

	err = o.setupRangePartition(
		ctx,
		o.queries.CreateOLAPTaskPartition,
		o.queries.ListOLAPTaskPartitionsBeforeDate,
		"v2_tasks_olap",
	)

	if err != nil {
		return err
	}

	err = o.setupRangePartition(
		ctx,
		o.queries.CreateOLAPDAGPartition,
		o.queries.ListOLAPDAGPartitionsBeforeDate,
		"v2_dags_olap",
	)

	if err != nil {
		return err
	}

	err = o.setupRangePartition(
		ctx,
		o.queries.CreateOLAPRunsPartition,
		o.queries.ListOLAPRunsPartitionsBeforeDate,
		"v2_runs_olap",
	)

	if err != nil {
		return err
	}

	return nil
}

func (o *olapEventRepository) setupRangePartition(
	ctx context.Context,
	create func(ctx context.Context, db olapv2.DBTX, date pgtype.Date) error,
	listBeforeDate func(ctx context.Context, db olapv2.DBTX, date pgtype.Date) ([]string, error),
	tableName string,
) error {
	today := time.Now().UTC()
	tomorrow := today.AddDate(0, 0, 1)
	sevenDaysAgo := today.AddDate(0, 0, -7)

	err := create(ctx, o.pool, pgtype.Date{
		Time:  today,
		Valid: true,
	})

	if err != nil {
		return err
	}

	err = create(ctx, o.pool, pgtype.Date{
		Time:  tomorrow,
		Valid: true,
	})

	if err != nil {
		return err
	}

	partitions, err := listBeforeDate(ctx, o.pool, pgtype.Date{
		Time:  sevenDaysAgo,
		Valid: true,
	})

	if err != nil {
		return err
	}

	for _, partition := range partitions {
		_, err := o.pool.Exec(
			ctx,
			fmt.Sprintf("ALTER TABLE %s DETACH PARTITION %s CONCURRENTLY", tableName, partition),
		)

		if err != nil {
			return err
		}

		_, err = o.pool.Exec(
			ctx,
			fmt.Sprintf("DROP TABLE %s", partition),
		)

		if err != nil {
			return err
		}
	}

	return nil
}

func StringToReadableStatus(status string) olap.ReadableTaskStatus {
	switch status {
	case "QUEUED":
		return olap.READABLE_TASK_STATUS_QUEUED
	case "RUNNING":
		return olap.READABLE_TASK_STATUS_RUNNING
	case "COMPLETED":
		return olap.READABLE_TASK_STATUS_COMPLETED
	case "CANCELLED":
		return olap.READABLE_TASK_STATUS_CANCELLED
	case "FAILED":
		return olap.READABLE_TASK_STATUS_FAILED
	default:
		return olap.READABLE_TASK_STATUS_QUEUED
	}
}

type getRelevantTaskEventsRow struct {
	TaskId         uuid.UUID
	Timestamp      time.Time
	EventType      string
	ReadableStatus string
}

type getTaskRow struct {
	Id                 uuid.UUID
	AdditionalMetadata string
	DisplayName        string
	TenantId           uuid.UUID
	CreatedAt          time.Time
	WorkflowId         uuid.UUID
}

func (r *olapEventRepository) ReadTaskRun(ctx context.Context, taskExternalId string) (*olapv2.V2TasksOlap, error) {
	row, err := r.queries.ReadTaskByExternalID(ctx, r.pool, sqlchelpers.UUIDFromStr(taskExternalId))

	if err != nil {
		return nil, err
	}

	return &olapv2.V2TasksOlap{
		TenantID:           row.TenantID,
		ID:                 row.ID,
		InsertedAt:         row.InsertedAt,
		Queue:              row.Queue,
		ActionID:           row.ActionID,
		StepID:             row.StepID,
		WorkflowID:         row.WorkflowID,
		ScheduleTimeout:    row.ScheduleTimeout,
		StepTimeout:        row.StepTimeout,
		Priority:           row.Priority,
		Sticky:             row.Sticky,
		DesiredWorkerID:    row.DesiredWorkerID,
		DisplayName:        row.DisplayName,
		Input:              row.Input,
		AdditionalMetadata: row.AdditionalMetadata,
		DagID:              row.DagID,
		DagInsertedAt:      row.DagInsertedAt,
		ReadableStatus:     row.ReadableStatus,
		ExternalID:         row.ExternalID,
		LatestRetryCount:   row.LatestRetryCount,
		LatestWorkerID:     row.LatestWorkerID,
	}, nil
}

type TaskMetadata struct {
	TaskID         int64     `json:"task_id"`
	TaskInsertedAt time.Time `json:"task_inserted_at"`
}

func ParseTaskMetadata(jsonData []byte) ([]TaskMetadata, error) {
	var tasks []TaskMetadata
	err := json.Unmarshal(jsonData, &tasks)
	if err != nil {
		return nil, err
	}
	return tasks, nil
}

func (r *olapEventRepository) ReadWorkflowRun(ctx context.Context, workflowRunExternalId pgtype.UUID) (*V2WorkflowRunPopulator, error) {
	row, err := r.queries.ReadWorkflowRunByExternalId(ctx, r.pool, workflowRunExternalId)

	if err != nil {
		return nil, err
	}

	taskMetadata, err := ParseTaskMetadata(row.TaskMetadata)

	if err != nil {
		return nil, err
	}

	return &V2WorkflowRunPopulator{
		WorkflowRun: &WorkflowRunData{
			TenantID:           row.TenantID,
			InsertedAt:         row.InsertedAt,
			ExternalID:         row.ExternalID,
			ReadableStatus:     row.ReadableStatus,
			Kind:               row.Kind,
			WorkflowID:         row.WorkflowID,
			DisplayName:        row.DisplayName,
			AdditionalMetadata: row.AdditionalMetadata,
			CreatedAt:          row.CreatedAt,
			StartedAt:          row.StartedAt,
			FinishedAt:         row.FinishedAt,
			ErrorMessage:       row.ErrorMessage.String,
			WorkflowVersionId:  row.WorkflowVersionID,
			Input:              row.Input,
		},
		TaskMetadata: taskMetadata,
	}, nil
}

func (r *olapEventRepository) ReadTaskRunData(ctx context.Context, tenantId pgtype.UUID, taskId int64, taskInsertedAt pgtype.Timestamptz) (*olapv2.PopulateSingleTaskRunDataRow, *pgtype.UUID, error) {
	taskRun, err := r.queries.PopulateSingleTaskRunData(ctx, r.pool, olapv2.PopulateSingleTaskRunDataParams{
		Taskid:         taskId,
		Tenantid:       tenantId,
		Taskinsertedat: taskInsertedAt,
	})

	if err != nil {
		return nil, nil, err
	}

	workflowRunId := taskRun.ExternalID

	if taskRun.DagID.Valid {
		dagId := taskRun.DagID.Int64
		dagInsertedAt := taskRun.DagInsertedAt

		workflowRunId, err = r.queries.GetWorkflowRunIdFromDagIdInsertedAt(ctx, r.pool, olapv2.GetWorkflowRunIdFromDagIdInsertedAtParams{
			Dagid:         dagId,
			Daginsertedat: dagInsertedAt,
		})

		if err != nil {
			return nil, nil, err
		}
	}

	return taskRun, &workflowRunId, nil
}

func (r *olapEventRepository) ListTasks(ctx context.Context, tenantId string, opts ListTaskRunOpts) ([]*olapv2.PopulateTaskRunDataRow, int, error) {
	tx, err := r.pool.Begin(ctx)

	if err != nil {
		return nil, 0, err
	}

	defer tx.Rollback(ctx)

	params := olapv2.ListTasksParams{
		Tenantid:   sqlchelpers.UUIDFromStr(tenantId),
		Since:      sqlchelpers.TimestamptzFromTime(opts.CreatedAfter),
		Tasklimit:  int32(opts.Limit),
		Taskoffset: int32(opts.Offset),
	}

	countParams := olapv2.CountTasksParams{
		Tenantid: sqlchelpers.UUIDFromStr(tenantId),
		Since:    sqlchelpers.TimestamptzFromTime(opts.CreatedAfter),
	}

	statuses := make([]string, 0)

	for _, status := range opts.Statuses {
		statuses = append(statuses, string(status))
	}

	if len(statuses) == 0 {
		statuses = []string{
			string(olapv2.V2ReadableStatusOlapQUEUED),
			string(olapv2.V2ReadableStatusOlapRUNNING),
			string(olapv2.V2ReadableStatusOlapCOMPLETED),
			string(olapv2.V2ReadableStatusOlapCANCELLED),
			string(olapv2.V2ReadableStatusOlapFAILED),
		}
	}

	params.Statuses = statuses
	countParams.Statuses = statuses

	if len(opts.WorkflowIds) > 0 {
		workflowIdParams := make([]pgtype.UUID, 0)

		for _, id := range opts.WorkflowIds {
			workflowIdParams = append(workflowIdParams, sqlchelpers.UUIDFromStr(id.String()))
		}

		params.WorkflowIds = workflowIdParams
		countParams.WorkflowIds = workflowIdParams
	}

	until := opts.FinishedBefore

	if until != nil {
		params.Until = sqlchelpers.TimestamptzFromTime(*until)
		countParams.Until = sqlchelpers.TimestamptzFromTime(*until)
	}

	workerId := opts.WorkerId

	if workerId != nil {
		params.WorkerId = sqlchelpers.UUIDFromStr(workerId.String())
	}

	for key, value := range opts.AdditionalMetadata {
		params.Keys = append(params.Keys, key)
		params.Values = append(params.Values, value.(string))
		countParams.Keys = append(countParams.Keys, key)
		countParams.Values = append(countParams.Values, value.(string))
	}

	rows, err := r.queries.ListTasks(ctx, tx, params)

	if err != nil {
		return nil, 0, err
	}

	taskIds := make([]int64, 0)
	taskInsertedAts := make([]pgtype.Timestamptz, 0)

	for _, row := range rows {
		taskIds = append(taskIds, row.ID)
		taskInsertedAts = append(taskInsertedAts, row.InsertedAt)
	}

	tasksWithData, err := r.queries.PopulateTaskRunData(ctx, tx, olapv2.PopulateTaskRunDataParams{
		Taskids:         taskIds,
		Taskinsertedats: taskInsertedAts,
		Tenantid:        sqlchelpers.UUIDFromStr(tenantId),
	})

	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, err
	}

	count, err := r.queries.CountTasks(ctx, tx, countParams)

	if err != nil {
		count = int64(len(tasksWithData))
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, 0, err
	}

	return tasksWithData, int(count), nil
}

func (r *olapEventRepository) ListTasksByDAGId(ctx context.Context, tenantId string, dagids []pgtype.UUID) ([]*olapv2.PopulateTaskRunDataRow, map[int64]uuid.UUID, error) {
	tx, err := r.pool.Begin(ctx)
	taskIdToDagExternalId := make(map[int64]uuid.UUID)

	if err != nil {
		return nil, taskIdToDagExternalId, err
	}

	defer tx.Rollback(ctx)

	tasks, err := r.queries.ListTasksByDAGIds(ctx, tx, olapv2.ListTasksByDAGIdsParams{
		Dagids:   dagids,
		Tenantid: sqlchelpers.UUIDFromStr(tenantId),
	})

	if err != nil {
		return nil, taskIdToDagExternalId, err
	}

	for _, row := range tasks {
		taskIdToDagExternalId[row.TaskID] = uuid.MustParse(sqlchelpers.UUIDToStr(row.DagExternalID))
	}

	taskIds := make([]int64, 0)
	taskInsertedAts := make([]pgtype.Timestamptz, 0)

	for _, row := range tasks {
		taskIds = append(taskIds, row.TaskID)
		taskInsertedAts = append(taskInsertedAts, row.TaskInsertedAt)
	}

	tasksWithData, err := r.queries.PopulateTaskRunData(ctx, tx, olapv2.PopulateTaskRunDataParams{
		Taskids:         taskIds,
		Taskinsertedats: taskInsertedAts,
		Tenantid:        sqlchelpers.UUIDFromStr(tenantId),
	})

	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, taskIdToDagExternalId, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, taskIdToDagExternalId, err
	}

	return tasksWithData, taskIdToDagExternalId, nil
}

func (r *olapEventRepository) ListTasksByIdAndInsertedAt(ctx context.Context, tenantId string, taskMetadata []TaskMetadata) ([]*olapv2.PopulateTaskRunDataRow, error) {
	tx, err := r.pool.Begin(ctx)

	if err != nil {
		return nil, err
	}

	defer tx.Rollback(ctx)

	taskIds := make([]int64, 0)
	taskInsertedAts := make([]pgtype.Timestamptz, 0)

	for _, metadata := range taskMetadata {
		taskIds = append(taskIds, metadata.TaskID)
		taskInsertedAts = append(taskInsertedAts, sqlchelpers.TimestamptzFromTime(metadata.TaskInsertedAt))
	}

	tasksWithData, err := r.queries.PopulateTaskRunData(ctx, tx, olapv2.PopulateTaskRunDataParams{
		Taskids:         taskIds,
		Taskinsertedats: taskInsertedAts,
		Tenantid:        sqlchelpers.UUIDFromStr(tenantId),
	})

	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return tasksWithData, nil
}

func (r *olapEventRepository) ListWorkflowRuns(ctx context.Context, tenantId string, opts ListWorkflowRunOpts) ([]*WorkflowRunData, int, error) {
	tx, err := r.pool.Begin(ctx)

	if err != nil {
		return nil, 0, err
	}

	defer tx.Rollback(ctx)

	params := olapv2.FetchWorkflowRunIdsParams{
		Tenantid:               sqlchelpers.UUIDFromStr(tenantId),
		Since:                  sqlchelpers.TimestamptzFromTime(opts.CreatedAfter),
		Listworkflowrunslimit:  int32(opts.Limit),
		Listworkflowrunsoffset: int32(opts.Offset),
	}

	countParams := olapv2.CountWorkflowRunsParams{
		Tenantid: sqlchelpers.UUIDFromStr(tenantId),
		Since:    sqlchelpers.TimestamptzFromTime(opts.CreatedAfter),
	}

	statuses := make([]string, 0)

	for _, status := range opts.Statuses {
		statuses = append(statuses, string(status))
	}

	if len(statuses) == 0 {
		statuses = []string{
			string(olapv2.V2ReadableStatusOlapQUEUED),
			string(olapv2.V2ReadableStatusOlapRUNNING),
			string(olapv2.V2ReadableStatusOlapCOMPLETED),
			string(olapv2.V2ReadableStatusOlapCANCELLED),
			string(olapv2.V2ReadableStatusOlapFAILED),
		}
	}

	params.Statuses = statuses
	countParams.Statuses = statuses

	if len(opts.WorkflowIds) > 0 {
		workflowIdParams := make([]pgtype.UUID, 0)

		for _, id := range opts.WorkflowIds {
			workflowIdParams = append(workflowIdParams, sqlchelpers.UUIDFromStr(id.String()))
		}

		params.WorkflowIds = workflowIdParams
		countParams.WorkflowIds = workflowIdParams
	}

	until := opts.FinishedBefore

	if until != nil {
		params.Until = sqlchelpers.TimestamptzFromTime(*until)
		countParams.Until = sqlchelpers.TimestamptzFromTime(*until)
	}

	for key, value := range opts.AdditionalMetadata {
		params.Keys = append(params.Keys, key)
		params.Values = append(params.Values, value.(string))
		countParams.Keys = append(countParams.Keys, key)
		countParams.Values = append(countParams.Values, value.(string))
	}

	workflowRunIds, err := r.queries.FetchWorkflowRunIds(ctx, tx, params)

	if err != nil {
		return nil, 0, err
	}

	runIdsWithDAGs := make([]int64, 0)
	runInsertedAtsWithDAGs := make([]pgtype.Timestamptz, 0)
	runIdsWithTasks := make([]int64, 0)
	runInsertedAtsWithTasks := make([]pgtype.Timestamptz, 0)

	for _, row := range workflowRunIds {
		if row.Kind == olapv2.V2RunKindDAG {
			runIdsWithDAGs = append(runIdsWithDAGs, row.ID)
			runInsertedAtsWithDAGs = append(runInsertedAtsWithDAGs, row.InsertedAt)
		} else {
			runIdsWithTasks = append(runIdsWithTasks, row.ID)
			runInsertedAtsWithTasks = append(runInsertedAtsWithTasks, row.InsertedAt)
		}
	}

	populatedDAGs, err := r.queries.PopulateDAGMetadata(ctx, tx, olapv2.PopulateDAGMetadataParams{
		Ids:         runIdsWithDAGs,
		Insertedats: runInsertedAtsWithDAGs,
		Tenantid:    sqlchelpers.UUIDFromStr(tenantId),
	})

	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, err
	}

	dagsToPopulated := make(map[string]*olapv2.PopulateDAGMetadataRow)

	for _, dag := range populatedDAGs {
		externalId := sqlchelpers.UUIDToStr(dag.ExternalID)

		dagsToPopulated[externalId] = dag
	}

	populatedTasks, err := r.queries.PopulateTaskRunData(ctx, tx, olapv2.PopulateTaskRunDataParams{
		Taskids:         runIdsWithTasks,
		Taskinsertedats: runInsertedAtsWithTasks,
		Tenantid:        sqlchelpers.UUIDFromStr(tenantId),
	})

	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, err
	}

	tasksToPopulated := make(map[string]*olapv2.PopulateTaskRunDataRow)

	for _, task := range populatedTasks {
		externalId := sqlchelpers.UUIDToStr(task.ExternalID)
		tasksToPopulated[externalId] = task
	}

	count, err := r.queries.CountWorkflowRuns(ctx, tx, countParams)

	if err != nil {
		r.l.Error().Msgf("error counting workflow runs: %v", err)
		count = int64(len(workflowRunIds))
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, 0, err
	}

	res := make([]*WorkflowRunData, 0)

	for _, row := range workflowRunIds {
		externalId := sqlchelpers.UUIDToStr(row.ExternalID)

		if row.Kind == olapv2.V2RunKindDAG {
			dag, ok := dagsToPopulated[externalId]

			if !ok {
				r.l.Error().Msgf("could not find dag with external id %s", externalId)
				continue
			}

			res = append(res, &WorkflowRunData{
				TenantID:           dag.TenantID,
				InsertedAt:         dag.InsertedAt,
				ExternalID:         dag.ExternalID,
				WorkflowID:         dag.WorkflowID,
				DisplayName:        dag.DisplayName,
				ReadableStatus:     dag.ReadableStatus,
				AdditionalMetadata: dag.AdditionalMetadata,
				CreatedAt:          dag.CreatedAt,
				StartedAt:          dag.StartedAt,
				FinishedAt:         dag.FinishedAt,
				ErrorMessage:       dag.ErrorMessage.String,
				Kind:               olapv2.V2RunKindDAG,
				WorkflowVersionId:  dag.WorkflowVersionID,
			})
		} else {
			task, ok := tasksToPopulated[externalId]

			if !ok {
				r.l.Error().Msgf("could not find task with external id %s", externalId)
				continue
			}

			res = append(res, &WorkflowRunData{
				TenantID:           task.TenantID,
				InsertedAt:         task.InsertedAt,
				ExternalID:         task.ExternalID,
				WorkflowID:         task.WorkflowID,
				DisplayName:        task.DisplayName,
				ReadableStatus:     task.Status,
				AdditionalMetadata: task.AdditionalMetadata,
				CreatedAt:          task.InsertedAt,
				StartedAt:          task.StartedAt,
				FinishedAt:         task.FinishedAt,
				ErrorMessage:       task.ErrorMessage.String,
				Kind:               olapv2.V2RunKindTASK,
			})
		}
	}

	return res, int(count), nil
}

func (r *olapEventRepository) ListTaskRunEvents(ctx context.Context, tenantId string, taskId int64, taskInsertedAt pgtype.Timestamptz, limit, offset int64) ([]*olapv2.ListTaskEventsRow, error) {
	rows, err := r.queries.ListTaskEvents(ctx, r.pool, olapv2.ListTaskEventsParams{
		Tenantid:       sqlchelpers.UUIDFromStr(tenantId),
		Taskid:         taskId,
		Taskinsertedat: taskInsertedAt,
	})

	if err != nil {
		return nil, err
	}

	return rows, nil
}

func (r *olapEventRepository) ListTaskRunEventsByWorkflowRunId(ctx context.Context, tenantId string, workflowRunId pgtype.UUID) ([]*olapv2.ListTaskEventsForWorkflowRunRow, error) {
	rows, err := r.queries.ListTaskEventsForWorkflowRun(ctx, r.pool, olapv2.ListTaskEventsForWorkflowRunParams{
		Tenantid:      sqlchelpers.UUIDFromStr(tenantId),
		Workflowrunid: workflowRunId,
	})

	if err != nil {
		return nil, err
	}

	return rows, nil
}

func (r *olapEventRepository) ReadTaskRunMetrics(ctx context.Context, tenantId string, opts ReadTaskRunMetricsOpts) ([]olap.TaskRunMetric, error) {
	var workflowIds []pgtype.UUID

	if len(opts.WorkflowIds) > 0 {
		workflowIds = make([]pgtype.UUID, 0)

		for _, id := range opts.WorkflowIds {
			workflowIds = append(workflowIds, sqlchelpers.UUIDFromStr(id.String()))
		}
	}

	res, err := r.queries.GetTenantStatusMetrics(context.Background(), r.pool, olapv2.GetTenantStatusMetricsParams{
		Tenantid:     sqlchelpers.UUIDFromStr(tenantId),
		Createdafter: sqlchelpers.TimestamptzFromTime(opts.CreatedAfter),
		WorkflowIds:  workflowIds,
	})

	if err != nil {
		return nil, err
	}

	metrics := make([]olap.TaskRunMetric, 0)

	metrics = append(metrics, olap.TaskRunMetric{
		Status: "QUEUED",
		Count:  uint64(res.TotalQueued),
	})

	metrics = append(metrics, olap.TaskRunMetric{
		Status: "RUNNING",
		Count:  uint64(res.TotalRunning),
	})

	metrics = append(metrics, olap.TaskRunMetric{
		Status: "COMPLETED",
		Count:  uint64(res.TotalCompleted),
	})

	metrics = append(metrics, olap.TaskRunMetric{
		Status: "CANCELLED",
		Count:  uint64(res.TotalCancelled),
	})

	metrics = append(metrics, olap.TaskRunMetric{
		Status: "FAILED",
		Count:  uint64(res.TotalFailed),
	})

	return metrics, nil
}

func (r *olapEventRepository) saveEventsToCache(events []olapv2.CreateTaskEventsOLAPParams) {
	for _, event := range events {
		key := getCacheKey(event)
		r.eventCache.Add(key, true)
	}
}

func getCacheKey(event olapv2.CreateTaskEventsOLAPParams) string {
	// key on the task_id, retry_count, and event_type
	return fmt.Sprintf("%d-%s-%d", event.TaskID, event.EventType, event.RetryCount)
}

func (r *olapEventRepository) writeTaskEventBatch(ctx context.Context, tenantId string, events []olapv2.CreateTaskEventsOLAPParams) error {
	// skip any events which have a corresponding event already
	eventsToWrite := make([]olapv2.CreateTaskEventsOLAPParams, 0)
	tmpEventsToWrite := make([]olapv2.CreateTaskEventsOLAPTmpParams, 0)

	for _, event := range events {
		key := getCacheKey(event)

		if _, ok := r.eventCache.Get(key); !ok {
			eventsToWrite = append(eventsToWrite, event)

			tmpEventsToWrite = append(tmpEventsToWrite, olapv2.CreateTaskEventsOLAPTmpParams{
				TenantID:       event.TenantID,
				TaskID:         event.TaskID,
				TaskInsertedAt: event.TaskInsertedAt,
				EventType:      event.EventType,
				RetryCount:     event.RetryCount,
				ReadableStatus: event.ReadableStatus,
				WorkerID:       event.WorkerID,
			})
		}
	}

	if len(eventsToWrite) == 0 {
		return nil
	}

	tx, commit, rollback, err := sqlchelpers.PrepareTx(ctx, r.pool, r.l, 5000)

	if err != nil {
		return err
	}

	defer rollback()

	_, err = r.queries.CreateTaskEventsOLAP(ctx, tx, eventsToWrite)

	if err != nil {
		return err
	}

	_, err = r.queries.CreateTaskEventsOLAPTmp(ctx, tx, tmpEventsToWrite)

	if err != nil {
		return err
	}

	if err := commit(ctx); err != nil {
		return err
	}

	r.saveEventsToCache(eventsToWrite)

	return nil
}

func (r *olapEventRepository) UpdateTaskStatuses(ctx context.Context, tenantId string) (bool, error) {
	var limit int32 = 10000

	// each partition gets its own goroutine
	eg := &errgroup.Group{}
	mu := sync.Mutex{}

	// if any of the partitions are saturated, we return true
	isSaturated := false

	for i := 0; i < NUM_PARTITIONS; i++ {
		partitionNumber := i

		eg.Go(func() error {
			tx, commit, rollback, err := sqlchelpers.PrepareTx(ctx, r.pool, r.l, 15000)

			if err != nil {
				return err
			}

			defer rollback()

			count, err := r.queries.UpdateTaskStatuses(ctx, tx, olapv2.UpdateTaskStatusesParams{
				Partitionnumber: int32(partitionNumber), // nolint: gosec
				Tenantid:        sqlchelpers.UUIDFromStr(tenantId),
				Eventlimit:      limit,
			})

			if err != nil {
				return err
			}

			if err := commit(ctx); err != nil {
				return err
			}

			mu.Lock()
			isSaturated = isSaturated || count == int64(limit)
			mu.Unlock()

			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return false, err
	}

	return isSaturated, nil
}

func (r *olapEventRepository) UpdateDAGStatuses(ctx context.Context, tenantId string) (bool, error) {
	var limit int32 = 10000

	// each partition gets its own goroutine
	eg := &errgroup.Group{}
	mu := sync.Mutex{}

	// if any of the partitions are saturated, we return true
	isSaturated := false

	for i := 0; i < NUM_PARTITIONS; i++ {
		partitionNumber := i

		eg.Go(func() error {
			tx, commit, rollback, err := sqlchelpers.PrepareTx(ctx, r.pool, r.l, 5000)

			if err != nil {
				return err
			}

			defer rollback()

			count, err := r.queries.UpdateDAGStatuses(ctx, tx, olapv2.UpdateDAGStatusesParams{
				Partitionnumber: int32(partitionNumber), // nolint: gosec
				Tenantid:        sqlchelpers.UUIDFromStr(tenantId),
				Eventlimit:      limit,
			})

			if err != nil {
				return err
			}

			if err := commit(ctx); err != nil {
				return err
			}

			mu.Lock()
			isSaturated = isSaturated || count == int64(limit)
			mu.Unlock()

			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return false, err
	}

	return isSaturated, nil
}

func (r *olapEventRepository) writeTaskBatch(ctx context.Context, tenantId string, tasks []*sqlcv2.V2Task) error {
	params := make([]olapv2.CreateTasksOLAPParams, 0)

	for _, task := range tasks {
		params = append(params, olapv2.CreateTasksOLAPParams{
			TenantID:           task.TenantID,
			ID:                 task.ID,
			InsertedAt:         task.InsertedAt,
			Queue:              task.Queue,
			ActionID:           task.ActionID,
			StepID:             task.StepID,
			WorkflowID:         task.WorkflowID,
			ScheduleTimeout:    task.ScheduleTimeout,
			StepTimeout:        task.StepTimeout,
			Priority:           task.Priority,
			Sticky:             olapv2.V2StickyStrategyOlap(task.Sticky),
			DesiredWorkerID:    task.DesiredWorkerID,
			ExternalID:         task.ExternalID,
			DisplayName:        task.DisplayName,
			Input:              task.Input,
			AdditionalMetadata: task.AdditionalMetadata,
			DagID:              task.DagID,
			DagInsertedAt:      task.DagInsertedAt,
		})
	}

	_, err := r.queries.CreateTasksOLAP(ctx, r.pool, params)

	return err
}

func (r *olapEventRepository) writeDAGBatch(ctx context.Context, tenantId string, dags []*v2.DAGWithData) error {
	params := make([]olapv2.CreateDAGsOLAPParams, 0)

	for _, dag := range dags {
		params = append(params, olapv2.CreateDAGsOLAPParams{
			TenantID:           dag.TenantID,
			ID:                 dag.ID,
			InsertedAt:         dag.InsertedAt,
			WorkflowID:         dag.WorkflowID,
			WorkflowVersionID:  dag.WorkflowVersionID,
			ExternalID:         dag.ExternalID,
			DisplayName:        dag.DisplayName,
			Input:              dag.Input,
			AdditionalMetadata: dag.AdditionalMetadata,
		})
	}

	_, err := r.queries.CreateDAGsOLAP(ctx, r.pool, params)

	return err
}

func (r *olapEventRepository) CreateTaskEvents(ctx context.Context, tenantId string, events []olapv2.CreateTaskEventsOLAPParams) error {
	return r.writeTaskEventBatch(ctx, tenantId, events)
}

func (r *olapEventRepository) CreateTasks(ctx context.Context, tenantId string, tasks []*sqlcv2.V2Task) error {
	return r.writeTaskBatch(ctx, tenantId, tasks)
}

func (r *olapEventRepository) CreateDAGs(ctx context.Context, tenantId string, dags []*v2.DAGWithData) error {
	return r.writeDAGBatch(ctx, tenantId, dags)
}

func (r *olapEventRepository) GetTaskPointMetrics(ctx context.Context, tenantId string, startTimestamp *time.Time, endTimestamp *time.Time, bucketInterval time.Duration) ([]*olapv2.GetTaskPointMetricsRow, error) {
	rows, err := r.queries.GetTaskPointMetrics(ctx, r.pool, olapv2.GetTaskPointMetricsParams{
		Interval:      durationToPgInterval(bucketInterval),
		Tenantid:      sqlchelpers.UUIDFromStr(tenantId),
		Createdafter:  sqlchelpers.TimestamptzFromTime(*startTimestamp),
		Createdbefore: sqlchelpers.TimestamptzFromTime(*endTimestamp),
	})

	if err != nil {
		return nil, err
	}

	return rows, nil
}

func (r *olapEventRepository) ReadDAG(ctx context.Context, dagExternalId string) (*olapv2.V2DagsOlap, error) {
	return r.queries.ReadDAGByExternalID(ctx, r.pool, sqlchelpers.UUIDFromStr(dagExternalId))
}

func durationToPgInterval(d time.Duration) pgtype.Interval {
	// Convert the time.Duration to microseconds
	microseconds := d.Microseconds()

	return pgtype.Interval{
		Microseconds: microseconds,
		Valid:        true,
	}
}

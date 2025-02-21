package v2

import (
	"context"

	"github.com/hatchet-dev/hatchet/pkg/repository/prisma/sqlchelpers"
	"github.com/hatchet-dev/hatchet/pkg/repository/v2/sqlcv2"
	"github.com/jackc/pgx/v5/pgtype"
)

type TaskWithQueue struct {
	*TaskIdRetryCount

	Queue string
}

type RunConcurrencyResult struct {
	// The tasks which were enqueued
	Queued []TaskWithQueue

	// If the strategy involves cancelling a task, these are the tasks to cancel
	Cancelled []TaskWithQueue

	// If the step has multiple concurrency strategies, these are the next ones to notify
	NextConcurrencyStrategies []int64
}

type ConcurrencyRepository interface {
	RunConcurrencyStrategy(ctx context.Context, tenantId pgtype.UUID, strategy *sqlcv2.V2StepConcurrency) (*RunConcurrencyResult, error)
}

type ConcurrencyRepositoryImpl struct {
	*sharedRepository
}

func newConcurrencyRepository(s *sharedRepository) ConcurrencyRepository {
	return &ConcurrencyRepositoryImpl{
		sharedRepository: s,
	}
}

func (c *ConcurrencyRepositoryImpl) RunConcurrencyStrategy(
	ctx context.Context,
	tenantId pgtype.UUID,
	strategy *sqlcv2.V2StepConcurrency,
) (res *RunConcurrencyResult, err error) {
	switch strategy.Strategy {
	case sqlcv2.V2ConcurrencyStrategyGROUPROUNDROBIN:
		return c.runGroupRoundRobin(ctx, tenantId, strategy)
	case sqlcv2.V2ConcurrencyStrategyCANCELINPROGRESS:
		return c.runCancelInProgress(ctx, tenantId, strategy)
	case sqlcv2.V2ConcurrencyStrategyCANCELNEWEST:
		return c.runCancelNewest(ctx, tenantId, strategy)
	}

	return nil, nil
}

func (c *ConcurrencyRepositoryImpl) runGroupRoundRobin(
	ctx context.Context,
	tenantId pgtype.UUID,
	strategy *sqlcv2.V2StepConcurrency,
) (res *RunConcurrencyResult, err error) {
	tx, commit, rollback, err := sqlchelpers.PrepareTx(ctx, c.pool, c.l, 5000)

	if err != nil {
		return nil, err
	}

	defer rollback()

	err = c.queries.ConcurrencyAdvisoryLock(ctx, tx, strategy.ID)

	if err != nil {
		return nil, err
	}

	poppedResults, err := c.queries.RunGroupRoundRobin(ctx, tx, sqlcv2.RunGroupRoundRobinParams{
		Tenantid:   tenantId,
		Strategyid: strategy.ID,
		Maxruns:    strategy.MaxConcurrency,
	})

	if err != nil {
		return nil, err
	}

	if err = commit(ctx); err != nil {
		return nil, err
	}

	queued := make([]TaskWithQueue, 0, len(poppedResults))
	nextConcurrencyStrategies := make([]int64, 0, len(poppedResults))

	for _, r := range poppedResults {
		if len(r.NextStrategyIds) > 0 {
			nextConcurrencyStrategies = append(nextConcurrencyStrategies, r.NextStrategyIds[0])
		} else {
			queued = append(queued, TaskWithQueue{
				TaskIdRetryCount: &TaskIdRetryCount{
					Id:         r.TaskID,
					RetryCount: r.TaskRetryCount,
				},
				Queue: r.QueueToNotify,
			})
		}
	}

	return &RunConcurrencyResult{
		Queued:                    queued,
		NextConcurrencyStrategies: nextConcurrencyStrategies,
	}, nil
}

func (c *ConcurrencyRepositoryImpl) runCancelInProgress(
	ctx context.Context,
	tenantId pgtype.UUID,
	strategy *sqlcv2.V2StepConcurrency,
) (res *RunConcurrencyResult, err error) {
	tx, commit, rollback, err := sqlchelpers.PrepareTx(ctx, c.pool, c.l, 5000)

	if err != nil {
		return nil, err
	}

	defer rollback()

	err = c.queries.ConcurrencyAdvisoryLock(ctx, tx, strategy.ID)

	if err != nil {
		return nil, err
	}

	poppedResults, err := c.queries.RunCancelInProgress(ctx, tx, sqlcv2.RunCancelInProgressParams{
		Tenantid:   tenantId,
		Strategyid: strategy.ID,
		Maxruns:    strategy.MaxConcurrency,
	})

	if err != nil {
		return nil, err
	}

	// for any cancelled tasks, call cancelTasks
	cancelledTasks := make([]TaskIdRetryCount, 0, len(poppedResults))

	for _, r := range poppedResults {
		if r.Operation == "CANCELLED" {
			cancelledTasks = append(cancelledTasks, TaskIdRetryCount{
				Id:         r.TaskID,
				RetryCount: r.TaskRetryCount,
			})
		}
	}

	taskIds := make([]int64, len(cancelledTasks))
	retryCounts := make([]int32, len(cancelledTasks))

	for i, task := range cancelledTasks {
		taskIds[i] = task.Id
		retryCounts[i] = task.RetryCount
	}

	// remove tasks from queue
	err = c.queries.DeleteTasksFromQueue(ctx, tx, sqlcv2.DeleteTasksFromQueueParams{
		Taskids:     taskIds,
		Retrycounts: retryCounts,
	})

	if err != nil {
		return nil, err
	}

	if err = commit(ctx); err != nil {
		return nil, err
	}

	queued := make([]TaskWithQueue, 0, len(poppedResults))
	cancelled := make([]TaskWithQueue, 0, len(poppedResults))
	nextConcurrencyStrategies := make([]int64, 0, len(poppedResults))

	for _, r := range poppedResults {
		if len(r.NextStrategyIds) > 0 {
			nextConcurrencyStrategies = append(nextConcurrencyStrategies, r.NextStrategyIds[0])
		} else if r.Operation == "CANCELLED" {
			cancelled = append(cancelled, TaskWithQueue{
				TaskIdRetryCount: &TaskIdRetryCount{
					Id:         r.TaskID,
					RetryCount: r.TaskRetryCount,
				},
				Queue: r.QueueToNotify,
			})
		} else {
			queued = append(queued, TaskWithQueue{
				TaskIdRetryCount: &TaskIdRetryCount{
					Id:         r.TaskID,
					RetryCount: r.TaskRetryCount,
				},
				Queue: r.QueueToNotify,
			})
		}
	}

	return &RunConcurrencyResult{
		Queued:                    queued,
		Cancelled:                 cancelled,
		NextConcurrencyStrategies: nextConcurrencyStrategies,
	}, nil
}

func (c *ConcurrencyRepositoryImpl) runCancelNewest(
	ctx context.Context,
	tenantId pgtype.UUID,
	strategy *sqlcv2.V2StepConcurrency,
) (res *RunConcurrencyResult, err error) {
	tx, commit, rollback, err := sqlchelpers.PrepareTx(ctx, c.pool, c.l, 5000)

	if err != nil {
		return nil, err
	}

	defer rollback()

	err = c.queries.ConcurrencyAdvisoryLock(ctx, tx, strategy.ID)

	if err != nil {
		return nil, err
	}

	poppedResults, err := c.queries.RunCancelNewest(ctx, tx, sqlcv2.RunCancelNewestParams{
		Tenantid:   tenantId,
		Strategyid: strategy.ID,
		Maxruns:    strategy.MaxConcurrency,
	})

	if err != nil {
		return nil, err
	}

	// for any cancelled tasks, call cancelTasks
	cancelledTasks := make([]TaskIdRetryCount, 0, len(poppedResults))

	for _, r := range poppedResults {
		if r.Operation == "CANCELLED" {
			cancelledTasks = append(cancelledTasks, TaskIdRetryCount{
				Id:         r.TaskID,
				RetryCount: r.TaskRetryCount,
			})
		}
	}

	taskIds := make([]int64, len(cancelledTasks))
	retryCounts := make([]int32, len(cancelledTasks))

	for i, task := range cancelledTasks {
		taskIds[i] = task.Id
		retryCounts[i] = task.RetryCount
	}

	// remove tasks from queue
	err = c.queries.DeleteTasksFromQueue(ctx, tx, sqlcv2.DeleteTasksFromQueueParams{
		Taskids:     taskIds,
		Retrycounts: retryCounts,
	})

	if err != nil {
		return nil, err
	}

	if err = commit(ctx); err != nil {
		return nil, err
	}

	queued := make([]TaskWithQueue, 0, len(poppedResults))
	cancelled := make([]TaskWithQueue, 0, len(poppedResults))
	nextConcurrencyStrategies := make([]int64, 0, len(poppedResults))

	for _, r := range poppedResults {
		if len(r.NextStrategyIds) > 0 {
			nextConcurrencyStrategies = append(nextConcurrencyStrategies, r.NextStrategyIds[0])
		} else if r.Operation == "CANCELLED" {
			cancelled = append(cancelled, TaskWithQueue{
				TaskIdRetryCount: &TaskIdRetryCount{
					Id:         r.TaskID,
					RetryCount: r.TaskRetryCount,
				},
				Queue: r.QueueToNotify,
			})
		} else {
			queued = append(queued, TaskWithQueue{
				TaskIdRetryCount: &TaskIdRetryCount{
					Id:         r.TaskID,
					RetryCount: r.TaskRetryCount,
				},
				Queue: r.QueueToNotify,
			})
		}
	}

	return &RunConcurrencyResult{
		Queued:                    queued,
		Cancelled:                 cancelled,
		NextConcurrencyStrategies: nextConcurrencyStrategies,
	}, nil
}

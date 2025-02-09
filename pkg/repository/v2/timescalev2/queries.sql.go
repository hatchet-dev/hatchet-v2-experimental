// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.24.0
// source: queries.sql

package timescalev2

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

const createOLAPPartitions = `-- name: CreateOLAPPartitions :exec
SELECT create_v2_task_events_partitions(
    $1::int
)
`

func (q *Queries) CreateOLAPPartitions(ctx context.Context, db DBTX, partitions int32) error {
	_, err := db.Exec(ctx, createOLAPPartitions, partitions)
	return err
}

type CreateTaskEventsOLAPParams struct {
	TenantID               pgtype.UUID          `json:"tenant_id"`
	TaskID                 int64                `json:"task_id"`
	TaskInsertedAt         pgtype.Timestamptz   `json:"task_inserted_at"`
	EventType              V2EventTypeOlap      `json:"event_type"`
	WorkflowID             pgtype.UUID          `json:"workflow_id"`
	EventTimestamp         pgtype.Timestamptz   `json:"event_timestamp"`
	ReadableStatus         V2ReadableStatusOlap `json:"readable_status"`
	RetryCount             int32                `json:"retry_count"`
	ErrorMessage           pgtype.Text          `json:"error_message"`
	Output                 []byte               `json:"output"`
	WorkerID               pgtype.UUID          `json:"worker_id"`
	AdditionalEventData    pgtype.Text          `json:"additional__event_data"`
	AdditionalEventMessage pgtype.Text          `json:"additional__event_message"`
}

type CreateTaskEventsOLAPTmpParams struct {
	TenantID       pgtype.UUID          `json:"tenant_id"`
	TaskID         int64                `json:"task_id"`
	TaskInsertedAt pgtype.Timestamptz   `json:"task_inserted_at"`
	EventType      V2EventTypeOlap      `json:"event_type"`
	ReadableStatus V2ReadableStatusOlap `json:"readable_status"`
	RetryCount     int32                `json:"retry_count"`
	WorkerID       pgtype.UUID          `json:"worker_id"`
}

type CreateTasksOLAPParams struct {
	TenantID           pgtype.UUID          `json:"tenant_id"`
	ID                 int64                `json:"id"`
	InsertedAt         pgtype.Timestamptz   `json:"inserted_at"`
	Queue              string               `json:"queue"`
	ActionID           string               `json:"action_id"`
	StepID             pgtype.UUID          `json:"step_id"`
	WorkflowID         pgtype.UUID          `json:"workflow_id"`
	ScheduleTimeout    string               `json:"schedule_timeout"`
	StepTimeout        pgtype.Text          `json:"step_timeout"`
	Priority           pgtype.Int4          `json:"priority"`
	Sticky             V2StickyStrategyOlap `json:"sticky"`
	DesiredWorkerID    pgtype.UUID          `json:"desired_worker_id"`
	ExternalID         pgtype.UUID          `json:"external_id"`
	DisplayName        string               `json:"display_name"`
	Input              []byte               `json:"input"`
	AdditionalMetadata []byte               `json:"additional_metadata"`
}

const getTaskPointMetrics = `-- name: GetTaskPointMetrics :many
SELECT
    time_bucket(COALESCE($1::interval, '1 minute'), bucket)::timestamptz as bucket_2,
    SUM(completed_count)::int as completed_count,
    SUM(failed_count)::int as failed_count
FROM
    v2_cagg_task_events_minute
WHERE
    tenant_id = $2::uuid AND
    -- timestamptz makes this fast, apparently:
    -- https://www.timescale.com/forum/t/very-slow-query-planning-time-in-postgresql/255/8
    bucket >= time_bucket('1 minute', $3::timestamptz) AND
    bucket <= time_bucket('1 minute', $4::timestamptz)
GROUP BY bucket_2
ORDER BY bucket_2
`

type GetTaskPointMetricsParams struct {
	Interval      pgtype.Interval    `json:"interval"`
	Tenantid      pgtype.UUID        `json:"tenantid"`
	Createdafter  pgtype.Timestamptz `json:"createdafter"`
	Createdbefore pgtype.Timestamptz `json:"createdbefore"`
}

type GetTaskPointMetricsRow struct {
	Bucket2        pgtype.Timestamptz `json:"bucket_2"`
	CompletedCount int32              `json:"completed_count"`
	FailedCount    int32              `json:"failed_count"`
}

func (q *Queries) GetTaskPointMetrics(ctx context.Context, db DBTX, arg GetTaskPointMetricsParams) ([]*GetTaskPointMetricsRow, error) {
	rows, err := db.Query(ctx, getTaskPointMetrics,
		arg.Interval,
		arg.Tenantid,
		arg.Createdafter,
		arg.Createdbefore,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []*GetTaskPointMetricsRow
	for rows.Next() {
		var i GetTaskPointMetricsRow
		if err := rows.Scan(&i.Bucket2, &i.CompletedCount, &i.FailedCount); err != nil {
			return nil, err
		}
		items = append(items, &i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const getTenantStatusMetrics = `-- name: GetTenantStatusMetrics :one
SELECT
  COALESCE(SUM(queued_count), 0)::bigint AS total_queued,
  COALESCE(SUM(running_count), 0)::bigint AS total_running,
  COALESCE(SUM(completed_count), 0)::bigint AS total_completed,
  COALESCE(SUM(cancelled_count), 0)::bigint AS total_cancelled,
  COALESCE(SUM(failed_count), 0)::bigint AS total_failed
FROM v2_cagg_status_metrics
WHERE
    tenant_id = $1::uuid
    AND bucket >= time_bucket('5 minutes', $2::timestamptz)
    AND (
        $3::uuid[] IS NULL OR workflow_id = ANY($3::uuid[])
    )
`

type GetTenantStatusMetricsParams struct {
	Tenantid     pgtype.UUID        `json:"tenantid"`
	Createdafter pgtype.Timestamptz `json:"createdafter"`
	WorkflowIds  []pgtype.UUID      `json:"workflowIds"`
}

type GetTenantStatusMetricsRow struct {
	TotalQueued    int64 `json:"total_queued"`
	TotalRunning   int64 `json:"total_running"`
	TotalCompleted int64 `json:"total_completed"`
	TotalCancelled int64 `json:"total_cancelled"`
	TotalFailed    int64 `json:"total_failed"`
}

func (q *Queries) GetTenantStatusMetrics(ctx context.Context, db DBTX, arg GetTenantStatusMetricsParams) (*GetTenantStatusMetricsRow, error) {
	row := db.QueryRow(ctx, getTenantStatusMetrics, arg.Tenantid, arg.Createdafter, arg.WorkflowIds)
	var i GetTenantStatusMetricsRow
	err := row.Scan(
		&i.TotalQueued,
		&i.TotalRunning,
		&i.TotalCompleted,
		&i.TotalCancelled,
		&i.TotalFailed,
	)
	return &i, err
}

const listTaskEvents = `-- name: ListTaskEvents :many
WITH aggregated_events AS (
  SELECT
    tenant_id,
    task_id,
    task_inserted_at,
    retry_count,
    event_type,
    MIN(event_timestamp) AS time_first_seen,
    MAX(event_timestamp) AS time_last_seen,
    COUNT(*) AS count,
    MIN(id) AS first_id
  FROM v2_task_events_olap
  WHERE
    tenant_id = $1::uuid
    AND task_id = $2::bigint
    AND task_inserted_at = $3::timestamptz
  GROUP BY tenant_id, task_id, task_inserted_at, retry_count, event_type
)
SELECT
  a.tenant_id,
  a.task_id,
  a.task_inserted_at,
  a.retry_count,
  a.event_type,
  a.time_first_seen,
  a.time_last_seen,
  a.count,
  t.id,
  t.event_timestamp,
  t.readable_status,
  t.error_message,
  t.output,
  t.worker_id,
  t.additional__event_data,
  t.additional__event_message
FROM aggregated_events a
JOIN v2_task_events_olap t
  ON t.tenant_id = a.tenant_id
  AND t.task_id = a.task_id
  AND t.task_inserted_at = a.task_inserted_at
  AND t.id = a.first_id
ORDER BY a.time_first_seen DESC, t.event_timestamp DESC
`

type ListTaskEventsParams struct {
	Tenantid       pgtype.UUID        `json:"tenantid"`
	Taskid         int64              `json:"taskid"`
	Taskinsertedat pgtype.Timestamptz `json:"taskinsertedat"`
}

type ListTaskEventsRow struct {
	TenantID               pgtype.UUID          `json:"tenant_id"`
	TaskID                 int64                `json:"task_id"`
	TaskInsertedAt         pgtype.Timestamptz   `json:"task_inserted_at"`
	RetryCount             int32                `json:"retry_count"`
	EventType              V2EventTypeOlap      `json:"event_type"`
	TimeFirstSeen          interface{}          `json:"time_first_seen"`
	TimeLastSeen           interface{}          `json:"time_last_seen"`
	Count                  int64                `json:"count"`
	ID                     int64                `json:"id"`
	EventTimestamp         pgtype.Timestamptz   `json:"event_timestamp"`
	ReadableStatus         V2ReadableStatusOlap `json:"readable_status"`
	ErrorMessage           pgtype.Text          `json:"error_message"`
	Output                 []byte               `json:"output"`
	WorkerID               pgtype.UUID          `json:"worker_id"`
	AdditionalEventData    pgtype.Text          `json:"additional__event_data"`
	AdditionalEventMessage pgtype.Text          `json:"additional__event_message"`
}

func (q *Queries) ListTaskEvents(ctx context.Context, db DBTX, arg ListTaskEventsParams) ([]*ListTaskEventsRow, error) {
	rows, err := db.Query(ctx, listTaskEvents, arg.Tenantid, arg.Taskid, arg.Taskinsertedat)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []*ListTaskEventsRow
	for rows.Next() {
		var i ListTaskEventsRow
		if err := rows.Scan(
			&i.TenantID,
			&i.TaskID,
			&i.TaskInsertedAt,
			&i.RetryCount,
			&i.EventType,
			&i.TimeFirstSeen,
			&i.TimeLastSeen,
			&i.Count,
			&i.ID,
			&i.EventTimestamp,
			&i.ReadableStatus,
			&i.ErrorMessage,
			&i.Output,
			&i.WorkerID,
			&i.AdditionalEventData,
			&i.AdditionalEventMessage,
		); err != nil {
			return nil, err
		}
		items = append(items, &i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const listTasks = `-- name: ListTasks :many
SELECT
    tenant_id, id, inserted_at, external_id, queue, action_id, step_id, workflow_id, schedule_timeout, step_timeout, priority, sticky, desired_worker_id, display_name, input, additional_metadata, readable_status, latest_retry_count, latest_worker_id
FROM
    v2_tasks_olap
WHERE
    tenant_id = $1::uuid
    AND inserted_at >= $2::timestamptz
    AND (
        $3::text[] IS NULL OR readable_status = ANY(cast($3::text[] as v2_readable_status_olap[]))
    )
    AND (
        $4::uuid[] IS NULL OR workflow_id = ANY($4::uuid[])
    )
    AND (
        $5::uuid IS NULL OR latest_worker_id = $5::uuid
    )
ORDER BY
    inserted_at DESC
LIMIT $6::integer
`

type ListTasksParams struct {
	Tenantid      pgtype.UUID        `json:"tenantid"`
	Insertedafter pgtype.Timestamptz `json:"insertedafter"`
	Statuses      []string           `json:"statuses"`
	WorkflowIds   []pgtype.UUID      `json:"workflowIds"`
	WorkerId      pgtype.UUID        `json:"workerId"`
	Tasklimit     int32              `json:"tasklimit"`
}

func (q *Queries) ListTasks(ctx context.Context, db DBTX, arg ListTasksParams) ([]*V2TasksOlap, error) {
	rows, err := db.Query(ctx, listTasks,
		arg.Tenantid,
		arg.Insertedafter,
		arg.Statuses,
		arg.WorkflowIds,
		arg.WorkerId,
		arg.Tasklimit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []*V2TasksOlap
	for rows.Next() {
		var i V2TasksOlap
		if err := rows.Scan(
			&i.TenantID,
			&i.ID,
			&i.InsertedAt,
			&i.ExternalID,
			&i.Queue,
			&i.ActionID,
			&i.StepID,
			&i.WorkflowID,
			&i.ScheduleTimeout,
			&i.StepTimeout,
			&i.Priority,
			&i.Sticky,
			&i.DesiredWorkerID,
			&i.DisplayName,
			&i.Input,
			&i.AdditionalMetadata,
			&i.ReadableStatus,
			&i.LatestRetryCount,
			&i.LatestWorkerID,
		); err != nil {
			return nil, err
		}
		items = append(items, &i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const populateSingleTaskRunData = `-- name: PopulateSingleTaskRunData :one
WITH latest_retry_count AS (
    SELECT
        MAX(retry_count) AS retry_count
    FROM
        v2_task_events_olap
    WHERE
        tenant_id = $1::uuid
        AND task_id = $2::bigint
        AND task_inserted_at = $3::timestamptz
), relevant_events AS (
    SELECT
        tenant_id, id, inserted_at, task_id, task_inserted_at, event_type, workflow_id, event_timestamp, readable_status, retry_count, error_message, output, worker_id, additional__event_data, additional__event_message
    FROM
        v2_task_events_olap
    WHERE
        tenant_id = $1::uuid
        AND task_id = $2::bigint
        AND task_inserted_at = $3::timestamptz
        AND retry_count = (SELECT retry_count FROM latest_retry_count)
    ORDER BY
        event_timestamp DESC
), finished_at AS (
    SELECT
        MAX(event_timestamp) AS finished_at
    FROM
        relevant_events
    WHERE
        readable_status = ANY(ARRAY['COMPLETED', 'FAILED', 'CANCELLED']::v2_readable_status_olap[])
), started_at AS (
    SELECT
        MAX(event_timestamp) AS started_at
    FROM
        relevant_events
    WHERE
        event_type = 'STARTED'
), task_output AS (
    SELECT
        output
    FROM
        relevant_events
    WHERE
        event_type = 'FINISHED'
), status AS (
    SELECT
        readable_status
    FROM
        relevant_events
    ORDER BY
        readable_status DESC
    LIMIT 1
)
SELECT
    t.tenant_id, t.id, t.inserted_at, t.external_id, t.queue, t.action_id, t.step_id, t.workflow_id, t.schedule_timeout, t.step_timeout, t.priority, t.sticky, t.desired_worker_id, t.display_name, t.input, t.additional_metadata, t.readable_status, t.latest_retry_count, t.latest_worker_id,
    st.readable_status::v2_readable_status_olap as status,
    f.finished_at::timestamptz as finished_at,
    s.started_at::timestamptz as started_at,
    o.output::jsonb as output
FROM
    v2_tasks_olap t
LEFT JOIN
    finished_at f ON true
LEFT JOIN
    started_at s ON true
LEFT JOIN
    task_output o ON true
LEFT JOIN
    status st ON true
WHERE
    (t.tenant_id, t.id, t.inserted_at) = ($1::uuid, $2::bigint, $3::timestamptz)
`

type PopulateSingleTaskRunDataParams struct {
	Tenantid       pgtype.UUID        `json:"tenantid"`
	Taskid         int64              `json:"taskid"`
	Taskinsertedat pgtype.Timestamptz `json:"taskinsertedat"`
}

type PopulateSingleTaskRunDataRow struct {
	TenantID           pgtype.UUID          `json:"tenant_id"`
	ID                 int64                `json:"id"`
	InsertedAt         pgtype.Timestamptz   `json:"inserted_at"`
	ExternalID         pgtype.UUID          `json:"external_id"`
	Queue              string               `json:"queue"`
	ActionID           string               `json:"action_id"`
	StepID             pgtype.UUID          `json:"step_id"`
	WorkflowID         pgtype.UUID          `json:"workflow_id"`
	ScheduleTimeout    string               `json:"schedule_timeout"`
	StepTimeout        pgtype.Text          `json:"step_timeout"`
	Priority           pgtype.Int4          `json:"priority"`
	Sticky             V2StickyStrategyOlap `json:"sticky"`
	DesiredWorkerID    pgtype.UUID          `json:"desired_worker_id"`
	DisplayName        string               `json:"display_name"`
	Input              []byte               `json:"input"`
	AdditionalMetadata []byte               `json:"additional_metadata"`
	ReadableStatus     V2ReadableStatusOlap `json:"readable_status"`
	LatestRetryCount   int32                `json:"latest_retry_count"`
	LatestWorkerID     pgtype.UUID          `json:"latest_worker_id"`
	Status             V2ReadableStatusOlap `json:"status"`
	FinishedAt         pgtype.Timestamptz   `json:"finished_at"`
	StartedAt          pgtype.Timestamptz   `json:"started_at"`
	Output             []byte               `json:"output"`
}

func (q *Queries) PopulateSingleTaskRunData(ctx context.Context, db DBTX, arg PopulateSingleTaskRunDataParams) (*PopulateSingleTaskRunDataRow, error) {
	row := db.QueryRow(ctx, populateSingleTaskRunData, arg.Tenantid, arg.Taskid, arg.Taskinsertedat)
	var i PopulateSingleTaskRunDataRow
	err := row.Scan(
		&i.TenantID,
		&i.ID,
		&i.InsertedAt,
		&i.ExternalID,
		&i.Queue,
		&i.ActionID,
		&i.StepID,
		&i.WorkflowID,
		&i.ScheduleTimeout,
		&i.StepTimeout,
		&i.Priority,
		&i.Sticky,
		&i.DesiredWorkerID,
		&i.DisplayName,
		&i.Input,
		&i.AdditionalMetadata,
		&i.ReadableStatus,
		&i.LatestRetryCount,
		&i.LatestWorkerID,
		&i.Status,
		&i.FinishedAt,
		&i.StartedAt,
		&i.Output,
	)
	return &i, err
}

const populateTaskRunData = `-- name: PopulateTaskRunData :many
WITH input AS (
    SELECT
        UNNEST($2::uuid[]) AS tenant_id,
        UNNEST($3::bigint[]) AS id,
        UNNEST($4::timestamptz[]) AS inserted_at,
        UNNEST($5::int[]) AS retry_count,
        unnest(cast($6::text[] as v2_readable_status_olap[])) AS status
), tasks AS (
    SELECT
        DISTINCT ON(t.tenant_id, t.id, t.inserted_at)
        t.tenant_id,
        t.id,
        t.inserted_at,
        t.queue,
        t.action_id,
        t.step_id,
        t.workflow_id,
        t.schedule_timeout,
        t.step_timeout,
        t.priority,
        t.sticky,
        t.desired_worker_id,
        t.external_id,
        t.display_name,
        t.input,
        t.additional_metadata,
        i.retry_count,
        i.status
    FROM
        v2_tasks_olap t
    JOIN
        input i ON i.tenant_id = t.tenant_id AND i.id = t.id AND i.inserted_at = t.inserted_at
), finished_ats AS (
    SELECT
        e.task_id::bigint,
        MAX(e.event_timestamp) AS finished_at
    FROM
        v2_task_events_olap e
    JOIN
        tasks t ON t.id = e.task_id AND t.tenant_id = e.tenant_id AND t.inserted_at = e.task_inserted_at AND t.retry_count = e.retry_count
    WHERE
        e.readable_status = ANY(ARRAY['COMPLETED', 'FAILED', 'CANCELLED']::v2_readable_status_olap[])
    GROUP BY e.task_id
), started_ats AS (
    SELECT
        e.task_id::bigint,
        MAX(e.event_timestamp) AS started_at
    FROM
        v2_task_events_olap e
    JOIN
        tasks t ON t.id = e.task_id AND t.tenant_id = e.tenant_id AND t.inserted_at = e.task_inserted_at AND t.retry_count = e.retry_count
    WHERE
        e.event_type = 'STARTED'
    GROUP BY e.task_id
)
SELECT
    t.tenant_id,
    t.id,
    t.inserted_at,
    t.external_id,
    t.queue,
    t.action_id,
    t.step_id,
    t.workflow_id,
    t.schedule_timeout,
    t.step_timeout,
    t.priority,
    t.sticky,
    t.display_name,
    t.retry_count,
    t.additional_metadata,
    t.status::v2_readable_status_olap as status,
    f.finished_at::timestamptz as finished_at,
    s.started_at::timestamptz as started_at
FROM
    tasks t
LEFT JOIN
    finished_ats f ON f.task_id = t.id
LEFT JOIN
    started_ats s ON s.task_id = t.id
ORDER BY t.inserted_at DESC, t.id DESC
LIMIT $1::int
`

type PopulateTaskRunDataParams struct {
	Tasklimit       int32                `json:"tasklimit"`
	Tenantids       []pgtype.UUID        `json:"tenantids"`
	Taskids         []int64              `json:"taskids"`
	Taskinsertedats []pgtype.Timestamptz `json:"taskinsertedats"`
	Retrycounts     []int32              `json:"retrycounts"`
	Statuses        []string             `json:"statuses"`
}

type PopulateTaskRunDataRow struct {
	TenantID           pgtype.UUID          `json:"tenant_id"`
	ID                 int64                `json:"id"`
	InsertedAt         pgtype.Timestamptz   `json:"inserted_at"`
	ExternalID         pgtype.UUID          `json:"external_id"`
	Queue              string               `json:"queue"`
	ActionID           string               `json:"action_id"`
	StepID             pgtype.UUID          `json:"step_id"`
	WorkflowID         pgtype.UUID          `json:"workflow_id"`
	ScheduleTimeout    string               `json:"schedule_timeout"`
	StepTimeout        pgtype.Text          `json:"step_timeout"`
	Priority           pgtype.Int4          `json:"priority"`
	Sticky             V2StickyStrategyOlap `json:"sticky"`
	DisplayName        string               `json:"display_name"`
	RetryCount         interface{}          `json:"retry_count"`
	AdditionalMetadata []byte               `json:"additional_metadata"`
	Status             V2ReadableStatusOlap `json:"status"`
	FinishedAt         pgtype.Timestamptz   `json:"finished_at"`
	StartedAt          pgtype.Timestamptz   `json:"started_at"`
}

func (q *Queries) PopulateTaskRunData(ctx context.Context, db DBTX, arg PopulateTaskRunDataParams) ([]*PopulateTaskRunDataRow, error) {
	rows, err := db.Query(ctx, populateTaskRunData,
		arg.Tasklimit,
		arg.Tenantids,
		arg.Taskids,
		arg.Taskinsertedats,
		arg.Retrycounts,
		arg.Statuses,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []*PopulateTaskRunDataRow
	for rows.Next() {
		var i PopulateTaskRunDataRow
		if err := rows.Scan(
			&i.TenantID,
			&i.ID,
			&i.InsertedAt,
			&i.ExternalID,
			&i.Queue,
			&i.ActionID,
			&i.StepID,
			&i.WorkflowID,
			&i.ScheduleTimeout,
			&i.StepTimeout,
			&i.Priority,
			&i.Sticky,
			&i.DisplayName,
			&i.RetryCount,
			&i.AdditionalMetadata,
			&i.Status,
			&i.FinishedAt,
			&i.StartedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, &i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const readTaskByExternalID = `-- name: ReadTaskByExternalID :one
WITH lookup_task AS (
    SELECT
        tenant_id,
        task_id,
        inserted_at
    FROM
        v2_task_lookup_table
    WHERE
        external_id = $1::uuid
)
SELECT
    t.tenant_id, t.id, t.inserted_at, t.external_id, t.queue, t.action_id, t.step_id, t.workflow_id, t.schedule_timeout, t.step_timeout, t.priority, t.sticky, t.desired_worker_id, t.display_name, t.input, t.additional_metadata, t.readable_status, t.latest_retry_count, t.latest_worker_id
FROM
    v2_tasks_olap t
JOIN
    lookup_task lt ON lt.tenant_id = t.tenant_id AND lt.task_id = t.id AND lt.inserted_at = t.inserted_at
`

func (q *Queries) ReadTaskByExternalID(ctx context.Context, db DBTX, externalid pgtype.UUID) (*V2TasksOlap, error) {
	row := db.QueryRow(ctx, readTaskByExternalID, externalid)
	var i V2TasksOlap
	err := row.Scan(
		&i.TenantID,
		&i.ID,
		&i.InsertedAt,
		&i.ExternalID,
		&i.Queue,
		&i.ActionID,
		&i.StepID,
		&i.WorkflowID,
		&i.ScheduleTimeout,
		&i.StepTimeout,
		&i.Priority,
		&i.Sticky,
		&i.DesiredWorkerID,
		&i.DisplayName,
		&i.Input,
		&i.AdditionalMetadata,
		&i.ReadableStatus,
		&i.LatestRetryCount,
		&i.LatestWorkerID,
	)
	return &i, err
}

const updateTaskStatuses = `-- name: UpdateTaskStatuses :one
WITH locked_events AS (
    SELECT
        tenant_id, requeue_after, requeue_retries, id, task_id, task_inserted_at, event_type, readable_status, retry_count, worker_id
    FROM
        list_task_events(
            $1::int,
            $2::uuid,
            $3::int
        )
), max_retry_counts AS (
    SELECT
        tenant_id,
        task_id,
        task_inserted_at,
        MAX(retry_count) AS max_retry_count
    FROM
        locked_events
    GROUP BY
        tenant_id, task_id, task_inserted_at
), updatable_events AS (
    SELECT
        e.tenant_id,
        e.task_id,
        e.task_inserted_at,
        e.retry_count,
        e.worker_id,
        MAX(e.readable_status) AS max_readable_status
    FROM
        locked_events e
    JOIN
        max_retry_counts mrc ON
            e.tenant_id = mrc.tenant_id
            AND e.task_id = mrc.task_id
            AND e.task_inserted_at = mrc.task_inserted_at
            AND e.retry_count = mrc.max_retry_count
    GROUP BY
        e.tenant_id, e.task_id, e.task_inserted_at, e.retry_count, e.worker_id
), locked_tasks AS (
    SELECT
        t.tenant_id,
        t.id,
        t.inserted_at,
        e.retry_count,
        e.max_readable_status
    FROM
        v2_tasks_olap t
    JOIN
        updatable_events e ON
            (t.tenant_id, t.id, t.inserted_at) = (e.tenant_id, e.task_id, e.task_inserted_at)
    ORDER BY
        t.id
    FOR UPDATE
), updated_tasks AS (
    UPDATE
        v2_tasks_olap t
    SET
        readable_status = e.max_readable_status,
        latest_retry_count = e.retry_count,
        latest_worker_id = CASE WHEN e.worker_id IS NOT NULL THEN e.worker_id ELSE t.latest_worker_id END
    FROM
        updatable_events e
    WHERE
        (t.tenant_id, t.id, t.inserted_at) = (e.tenant_id, e.task_id, e.task_inserted_at)
        AND e.retry_count >= t.latest_retry_count
        AND e.max_readable_status > t.readable_status
    RETURNING
        t.tenant_id, t.id, t.inserted_at
), events_to_requeue AS (
    -- Get events which don't have a corresponding locked_task
    SELECT
        e.tenant_id,
        e.requeue_retries,
        e.task_id,
        e.task_inserted_at,
        e.event_type,
        e.readable_status,
        e.retry_count
    FROM
        locked_events e
    LEFT JOIN
        locked_tasks t ON (e.tenant_id, e.task_id, e.task_inserted_at) = (t.tenant_id, t.id, t.inserted_at)
    WHERE
        t.id IS NULL
), deleted_events AS (
    DELETE FROM
        v2_task_events_olap_tmp
    WHERE
        (tenant_id, requeue_after, task_id, id) IN (SELECT tenant_id, requeue_after, task_id, id FROM locked_events)
), requeued_events AS (
    INSERT INTO
        v2_task_events_olap_tmp (
            tenant_id,
            requeue_after,
            requeue_retries,
            task_id,
            task_inserted_at,
            event_type,
            readable_status,
            retry_count
        )
    SELECT
        tenant_id,
        -- Exponential backoff, we limit to 10 retries which is 2048 seconds/34 minutes
        CURRENT_TIMESTAMP + (2 ^ requeue_retries) * INTERVAL '2 seconds',
        requeue_retries + 1,
        task_id,
        task_inserted_at,
        event_type,
        readable_status,
        retry_count + 1
    FROM
        events_to_requeue
    WHERE
        retry_count < 10
    RETURNING
        tenant_id, requeue_after, requeue_retries, id, task_id, task_inserted_at, event_type, readable_status, retry_count, worker_id
)
SELECT
    COUNT(*)
FROM
    locked_events
`

type UpdateTaskStatusesParams struct {
	Partitionnumber int32       `json:"partitionnumber"`
	Tenantid        pgtype.UUID `json:"tenantid"`
	Eventlimit      int32       `json:"eventlimit"`
}

func (q *Queries) UpdateTaskStatuses(ctx context.Context, db DBTX, arg UpdateTaskStatusesParams) (int64, error) {
	row := db.QueryRow(ctx, updateTaskStatuses, arg.Partitionnumber, arg.Tenantid, arg.Eventlimit)
	var count int64
	err := row.Scan(&count)
	return count, err
}

-- name: GetTaskPointMetrics :many
SELECT
    time_bucket(COALESCE(sqlc.narg('interval')::interval, '1 minute'), bucket)::timestamptz as bucket_2,
    SUM(completed_count)::int as completed_count,
    SUM(failed_count)::int as failed_count
FROM
    v2_cagg_task_events_minute
WHERE
    tenant_id = @tenantId::uuid AND
    -- timestamptz makes this fast, apparently:
    -- https://www.timescale.com/forum/t/very-slow-query-planning-time-in-postgresql/255/8
    bucket >= time_bucket('1 minute', @createdAfter::timestamptz) AND
    bucket <= time_bucket('1 minute', @createdBefore::timestamptz)
GROUP BY bucket_2
ORDER BY bucket_2;

SELECT
  COALESCE(SUM(queued_count), 0)::bigint AS total_queued,
  COALESCE(SUM(running_count), 0)::bigint AS total_running,
  COALESCE(SUM(completed_count), 0)::bigint AS total_completed,
  COALESCE(SUM(cancelled_count), 0)::bigint AS total_cancelled,
  COALESCE(SUM(failed_count), 0)::bigint AS total_failed
FROM v2_cagg_status_metrics
WHERE
    tenant_id = @tenantId::uuid
    AND bucket >= time_bucket('5 minutes', @createdAfter::timestamptz)
    AND (
        sqlc.narg('workflowIds')::uuid[] IS NULL OR workflow_id = ANY(sqlc.narg('workflowIds')::uuid[])
    );

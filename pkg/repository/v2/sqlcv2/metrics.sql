-- name: GetTaskPointMetrics :many
SELECT
    DATE_BIN(COALESCE(sqlc.narg('interval')::interval, '1 minute'), task_inserted_at, TIMESTAMPTZ @createdAfter::timestamptz) AS bucket,
    COUNT(*) FILTER (WHERE readable_status = 'COMPLETED') AS completed_count,
    COUNT(*) FILTER (WHERE readable_status = 'FAILED') AS failed_count
FROM
    v2_task_events_olap
WHERE
    tenant_id = @tenantId::uuid AND
    task_inserted_at BETWEEN @createdAfter::timestamptz AND @createdBefore::timestamptz
GROUP BY bucket
ORDER BY bucket;


-- name: GetTenantStatusMetrics :one
SELECT
    TIMESTAMP WITH TIME ZONE 'epoch' + INTERVAL '1 second' * ROUND(EXTRACT('epoch' FROM inserted_at) / 300) * 300 AS bucket,
    tenant_id,
    workflow_id,
    COUNT(*) FILTER (WHERE readable_status = 'QUEUED') AS queued_count,
    COUNT(*) FILTER (WHERE readable_status = 'RUNNING') AS running_count,
    COUNT(*) FILTER (WHERE readable_status = 'COMPLETED') AS completed_count,
    COUNT(*) FILTER (WHERE readable_status = 'CANCELLED') AS cancelled_count,
    COUNT(*) FILTER (WHERE readable_status = 'FAILED') AS failed_count
FROM v2_statuses_olap
WHERE
    tenant_id = @tenantId::uuid
    AND inserted_at >= @createdAfter::timestamptz
    AND (
        sqlc.narg('workflowIds')::uuid[] IS NULL OR workflow_id = ANY(sqlc.narg('workflowIds')::uuid[])
    )
GROUP BY tenant_id, workflow_id, bucket
ORDER BY bucket DESC
;

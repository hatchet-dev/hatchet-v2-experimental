-- name: ListActiveConcurrencyStrategies :many
SELECT
    *
FROM
    v2_step_concurrency
WHERE
    tenant_id = @tenantId::uuid AND
    is_active = TRUE;

-- name: ListConcurrencyStrategiesByStepId :many
SELECT
    *
FROM
    v2_step_concurrency
WHERE
    tenant_id = @tenantId::uuid AND
    step_id = ANY(@stepIds::uuid[])
ORDER BY 
    id ASC;

-- name: CheckStrategyActive :one
-- A strategy is active if the workflow is not deleted, and it is attached to the latest workflow version or it has
-- at least one concurrency slot that is not filled. 
WITH workflow AS (
    SELECT
        w."id" as "id",
        wv."id" as "workflowVersionId",
        w."tenantId" as "tenantId"
    FROM
        "Step" s
    JOIN
        "Job" j ON j."id" = s."jobId"
    JOIN
        "WorkflowVersion" wv ON wv."id" = j."workflowVersionId"
    JOIN
        "Workflow" w ON w."id" = wv."workflowId"
    WHERE
        s."id" = @stepId::uuid
        AND w."tenantId" = @tenantId::uuid
        AND w."deletedAt" IS NULL
        AND wv."deletedAt" IS NULL
), latest_workflow_version AS (
    SELECT DISTINCT ON("workflowId")
        "workflowId",
        workflowVersions."id" AS "workflowVersionId"
    FROM
        "WorkflowVersion" as workflowVersions
    JOIN
        workflow ON workflow."id" = workflowVersions."workflowId"
    WHERE
        workflow."tenantId" = @tenantId::uuid
        AND workflowVersions."deletedAt" IS NULL
    ORDER BY "workflowId", "order" DESC
    LIMIT 1
), active_slot AS (
    SELECT
        *
    FROM
        v2_concurrency_slot
    WHERE
        tenant_id = @tenantId::uuid AND
        strategy_id = @strategyId::bigint
        -- Note we don't check for is_filled=False, because is_filled=True could imply that the task
        -- gets retried and the slot is still active.
    LIMIT 1
), is_active AS (
    SELECT
        EXISTS(SELECT 1 FROM workflow) AND
        (
            workflow."workflowVersionId" = latest_workflow_version."workflowVersionId" OR
            EXISTS(SELECT 1 FROM active_slot)
        ) AS "isActive"
    FROM
        workflow, latest_workflow_version
)
SELECT COALESCE((SELECT "isActive" FROM is_active), FALSE)::bool AS "isActive";

-- name: SetConcurrencyStrategyInactive :exec
UPDATE
    v2_step_concurrency
SET
    is_active = FALSE
WHERE
    workflow_id = @workflowId::uuid AND
    step_id = @stepId::uuid AND
    id = @strategyId::bigint;

-- name: ConcurrencyAdvisoryLock :exec
SELECT pg_advisory_xact_lock(@key::bigint);

-- name: RunGroupRoundRobin :many
WITH slots AS (
    SELECT 
        task_id,
        task_inserted_at,
        task_retry_count,
        key,
        strategy_id,
        tenant_id,
        is_filled,
        row_number() OVER (PARTITION BY key ORDER BY priority DESC, task_id ASC, task_inserted_at ASC) AS rn,
        row_number() OVER (ORDER BY priority DESC, task_id ASC, task_inserted_at ASC) AS seqnum
    FROM    
        v2_concurrency_slot
    WHERE
        tenant_id = @tenantId::uuid AND
        strategy_id = @strategyId::bigint AND
        schedule_timeout_at >= NOW()
), schedule_timeout_slots AS (
    SELECT
        *
    FROM
        v2_concurrency_slot
    WHERE
        tenant_id = @tenantId::uuid AND
        strategy_id = @strategyId::bigint AND
        schedule_timeout_at < NOW() AND
        is_filled = FALSE
    ORDER BY
        task_id, task_inserted_at
    FOR UPDATE
), eligible_slots_per_group AS (
    SELECT
        task_id,
        task_inserted_at,
        task_retry_count,
        tenant_id,
        strategy_id,
        key,
        is_filled,
        rn,
        seqnum
    FROM
        slots
    WHERE
        rn <= @maxRuns::int
), eligible_slots AS (
    SELECT
        *
    FROM
        v2_concurrency_slot
    WHERE 
        (task_inserted_at, task_id, task_retry_count, tenant_id, strategy_id) IN (
            SELECT
                es.task_inserted_at,
                es.task_id,
                es.task_retry_count,
                es.tenant_id,
                es.strategy_id
            FROM
                eligible_slots_per_group es
            ORDER BY
                rn, seqnum
            LIMIT (@maxRuns::int) * (SELECT COUNT(DISTINCT key) FROM slots)
        )
        AND is_filled = FALSE
    ORDER BY
        task_id, task_inserted_at
    FOR UPDATE
), updated_slots AS (
    UPDATE
        v2_concurrency_slot
    SET
        is_filled = TRUE
    FROM
        eligible_slots
    WHERE
        v2_concurrency_slot.task_id = eligible_slots.task_id AND
        v2_concurrency_slot.task_inserted_at = eligible_slots.task_inserted_at AND
        v2_concurrency_slot.task_retry_count = eligible_slots.task_retry_count AND
        v2_concurrency_slot.tenant_id = eligible_slots.tenant_id AND
        v2_concurrency_slot.strategy_id = eligible_slots.strategy_id AND
        v2_concurrency_slot.key = eligible_slots.key
    RETURNING
        v2_concurrency_slot.*
), deleted_slots AS (
    DELETE FROM
        v2_concurrency_slot
    WHERE
        (task_inserted_at, task_id, task_retry_count) IN (
            SELECT
                c.task_inserted_at,
                c.task_id,
                c.task_retry_count
            FROM
                schedule_timeout_slots c
        )
)
SELECT
    *,
    'SCHEDULING_TIMED_OUT' AS "operation"
FROM
    schedule_timeout_slots
UNION ALL
SELECT
    *,
    'RUNNING' AS "operation"
FROM
    updated_slots;

-- name: RunCancelInProgress :many
WITH slots AS (
    SELECT 
        task_id,
        task_inserted_at,
        task_retry_count,
        tenant_id,
        strategy_id,
        key,
        is_filled,
        -- Order slots by rn desc, seqnum desc to ensure that the most recent tasks will be run
        row_number() OVER (PARTITION BY key ORDER BY task_id DESC, task_inserted_at DESC) AS rn,
        row_number() OVER (ORDER BY task_id DESC, task_inserted_at DESC) AS seqnum
    FROM    
        v2_concurrency_slot
    WHERE
        tenant_id = @tenantId::uuid AND
        strategy_id = @strategyId::bigint AND
        schedule_timeout_at >= NOW()
), schedule_timeout_slots AS (
    SELECT
        *
    FROM
        v2_concurrency_slot
    WHERE
        tenant_id = @tenantId::uuid AND
        strategy_id = @strategyId::bigint AND
        schedule_timeout_at < NOW() AND
        is_filled = FALSE
), eligible_running_slots AS (
    SELECT
        task_id,
        task_inserted_at,
        task_retry_count,
        tenant_id,
        strategy_id,
        key,
        is_filled,
        rn,
        seqnum
    FROM
        slots
    WHERE
        rn <= @maxRuns::int
), slots_to_cancel AS (
    SELECT
        *
    FROM
        v2_concurrency_slot
    WHERE
        tenant_id = @tenantId::uuid AND
        strategy_id = @strategyId::bigint AND
        (task_inserted_at, task_id, task_retry_count) NOT IN (
            SELECT
                ers.task_inserted_at,
                ers.task_id,
                ers.task_retry_count
            FROM
                eligible_running_slots ers
        )
    ORDER BY
        task_id, task_inserted_at
    FOR UPDATE
), slots_to_run AS (
    SELECT
        *
    FROM
        v2_concurrency_slot
    WHERE 
        (task_inserted_at, task_id, task_retry_count, tenant_id, strategy_id) IN (
            SELECT
                ers.task_inserted_at,
                ers.task_id,
                ers.task_retry_count,
                ers.tenant_id,
                ers.strategy_id
            FROM
                eligible_running_slots ers
            ORDER BY
                rn, seqnum
        )
    ORDER BY
        task_id, task_inserted_at
    FOR UPDATE
), updated_slots AS (
    UPDATE
        v2_concurrency_slot
    SET
        is_filled = TRUE
    FROM
        slots_to_run
    WHERE
        v2_concurrency_slot.task_id = slots_to_run.task_id AND
        v2_concurrency_slot.task_inserted_at = slots_to_run.task_inserted_at AND
        v2_concurrency_slot.task_retry_count = slots_to_run.task_retry_count AND
        v2_concurrency_slot.key = slots_to_run.key AND
        v2_concurrency_slot.is_filled = FALSE
    RETURNING
        v2_concurrency_slot.*
), deleted_slots AS (
    DELETE FROM
        v2_concurrency_slot
    WHERE
        (task_inserted_at, task_id, task_retry_count) IN (
            SELECT
                c.task_inserted_at,
                c.task_id,
                c.task_retry_count
            FROM
                slots_to_cancel c
        )
)
SELECT
    *,
    'SCHEDULING_TIMED_OUT' AS "operation"
FROM
    schedule_timeout_slots
UNION ALL
SELECT
    *,
    'CANCELLED' AS "operation"
FROM    
    slots_to_cancel
WHERE
    -- not in the schedule_timeout_slots
    (task_inserted_at, task_id, task_retry_count) NOT IN (
        SELECT
            c.task_inserted_at,
            c.task_id,
            c.task_retry_count
        FROM
            schedule_timeout_slots c
    )
UNION ALL
SELECT
    *,
    'RUNNING' AS "operation"
FROM
    updated_slots;

-- name: RunCancelNewest :many
WITH slots AS (
    SELECT 
        task_id,
        task_inserted_at,
        task_retry_count,
        tenant_id,
        strategy_id,
        key,
        is_filled,
        row_number() OVER (PARTITION BY key ORDER BY task_id ASC, task_inserted_at ASC) AS rn,
        row_number() OVER (ORDER BY task_id ASC, task_inserted_at ASC) AS seqnum
    FROM    
        v2_concurrency_slot
    WHERE
        tenant_id = @tenantId::uuid AND
        strategy_id = @strategyId::bigint AND
        schedule_timeout_at >= NOW()
), schedule_timeout_slots AS (
    SELECT
        *
    FROM
        v2_concurrency_slot
    WHERE
        tenant_id = @tenantId::uuid AND
        strategy_id = @strategyId::bigint AND
        schedule_timeout_at < NOW() AND
        is_filled = FALSE
), eligible_running_slots AS (
    SELECT
        task_id,
        task_inserted_at,
        task_retry_count,
        tenant_id,
        strategy_id,
        key,
        is_filled,
        rn,
        seqnum
    FROM
        slots
    WHERE
        rn <= @maxRuns::int
), slots_to_cancel AS (
    SELECT
        *
    FROM
        v2_concurrency_slot
    WHERE
        tenant_id = @tenantId::uuid AND
        strategy_id = @strategyId::bigint AND
        (task_inserted_at, task_id, task_retry_count) NOT IN (
            SELECT
                ers.task_inserted_at,
                ers.task_id,
                ers.task_retry_count
            FROM
                eligible_running_slots ers
        )
    ORDER BY
        task_id ASC, task_inserted_at ASC
    FOR UPDATE
), slots_to_run AS (
    SELECT
        *
    FROM
        v2_concurrency_slot
    WHERE 
        (task_inserted_at, task_id, task_retry_count, tenant_id, strategy_id) IN (
            SELECT
                ers.task_inserted_at,
                ers.task_id,
                ers.task_retry_count,
                ers.tenant_id,
                ers.strategy_id
            FROM
                eligible_running_slots ers
            ORDER BY
                rn, seqnum
        )
    ORDER BY
        task_id ASC, task_inserted_at ASC
    FOR UPDATE
), updated_slots AS (
    UPDATE
        v2_concurrency_slot
    SET
        is_filled = TRUE
    FROM
        slots_to_run
    WHERE
        v2_concurrency_slot.task_id = slots_to_run.task_id AND
        v2_concurrency_slot.task_inserted_at = slots_to_run.task_inserted_at AND
        v2_concurrency_slot.task_retry_count = slots_to_run.task_retry_count AND
        v2_concurrency_slot.key = slots_to_run.key AND
        v2_concurrency_slot.is_filled = FALSE
    RETURNING
        v2_concurrency_slot.*
), deleted_slots AS (
    DELETE FROM
        v2_concurrency_slot
    WHERE
        (task_inserted_at, task_id, task_retry_count) IN (
            SELECT
                c.task_inserted_at,
                c.task_id,
                c.task_retry_count
            FROM
                slots_to_cancel c
        )
)
SELECT
    *,
    'SCHEDULING_TIMED_OUT' AS "operation"
FROM
    schedule_timeout_slots
UNION ALL
SELECT
    *,
    'CANCELLED' AS "operation"
FROM    
    slots_to_cancel
WHERE
    -- not in the schedule_timeout_slots
    (task_inserted_at, task_id, task_retry_count) NOT IN (
        SELECT
            c.task_inserted_at,
            c.task_id,
            c.task_retry_count
        FROM
            schedule_timeout_slots c
    )
UNION ALL
SELECT
    *,
    'RUNNING' AS "operation"
FROM
    updated_slots;
-- name: ListActiveConcurrencyStrategies :many
SELECT
    DISTINCT ON(tenant_id, step_id, expression) *
FROM
    v2_step_concurrency
WHERE
    tenant_id = @tenantId::uuid AND
    is_active = TRUE
ORDER BY tenant_id, step_id, expression, id DESC;

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

-- name: ConcurrencyAdvisoryLock :exec
SELECT pg_advisory_xact_lock(@key::bigint);

-- name: RunGroupRoundRobin :many
WITH slots AS (
    SELECT 
        task_id,
        task_inserted_at,
        task_retry_count,
        key,
        is_filled,
        row_number() OVER (PARTITION BY key ORDER BY task_id ASC, task_inserted_at ASC) AS rn,
        row_number() OVER (ORDER BY task_id ASC, task_inserted_at ASC) AS seqnum
    FROM    
        v2_concurrency_slot
    WHERE
        tenant_id = @tenantId::uuid AND
        strategy_id = @strategyId::bigint
), eligible_slots_per_group AS (
    SELECT
        task_id,
        task_inserted_at,
        task_retry_count,
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
        (task_inserted_at, task_id, task_retry_count) IN (
            SELECT
                es.task_inserted_at,
                es.task_id,
                es.task_retry_count
            FROM
                eligible_slots_per_group es
            ORDER BY
                rn, seqnum
            LIMIT (@maxRuns::int) * (SELECT COUNT(DISTINCT key) FROM slots)
        )
        AND is_filled = FALSE
    ORDER BY
        task_inserted_at, task_id
    FOR UPDATE
)
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
    v2_concurrency_slot.key = eligible_slots.key
RETURNING 
    *;

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
        strategy_id = @strategyId::bigint
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
    'CANCELLED' AS "operation"
FROM    
    slots_to_cancel
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
        strategy_id = @strategyId::bigint
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
    'CANCELLED' AS "operation"
FROM    
    slots_to_cancel
UNION ALL
SELECT
    *,
    'RUNNING' AS "operation"
FROM
    updated_slots;
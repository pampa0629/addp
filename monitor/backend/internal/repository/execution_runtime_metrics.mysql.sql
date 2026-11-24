WITH windowed AS (
    SELECT module, task_type, execution_boundary, status, attempt, retry_of_execution_id,
        GREATEST(TIMESTAMPDIFF(MICROSECOND, created_at, COALESCE(started_at, completed_at, ?)), 0) / 1000.0 AS queue_ms,
        CASE WHEN started_at IS NOT NULL THEN GREATEST(COALESCE(execution_time_ms,
            TIMESTAMPDIFF(MICROSECOND, started_at, COALESCE(completed_at, ?)) / 1000.0), 0) END AS run_ms,
        (TRIM(COALESCE(JSON_UNQUOTE(NULLIF(JSON_EXTRACT(metadata, '$.recovery_reason'), CAST('null' AS JSON))), '')) <> ''
         OR LOWER(COALESCE(JSON_UNQUOTE(NULLIF(JSON_EXTRACT(error_details, '$.code'), CAST('null' AS JSON))), '')) LIKE '%lease_expired%'
         OR LOWER(COALESCE(current_step, '')) LIKE '%lease expired%') AS recovered
    FROM common.task_executions
    WHERE tenant_id = ? AND created_at >= ? AND created_at <= ?
      AND (? = '' OR module = ?) AND /*EXECUTION_READ_SCOPE*/
), ranked AS (
    SELECT windowed.*,
        ROW_NUMBER() OVER (w ORDER BY queue_ms) AS queue_rank,
        ROW_NUMBER() OVER (w ORDER BY run_ms IS NULL, run_ms) AS run_rank,
        1 + 0.95 * (COUNT(*) OVER w - 1) AS queue_position,
        1 + 0.95 * (NULLIF(COUNT(run_ms) OVER w, 0) - 1) AS run_position
    FROM windowed
    WINDOW w AS (PARTITION BY module, task_type, execution_boundary)
), aggregated AS (
    SELECT module, task_type, execution_boundary,
        COUNT(*) AS created_count,
        SUM(status IN ('success','failed','timeout','cancelled')) AS completed_count,
        SUM(status = 'success') AS success_count, SUM(status = 'failed') AS failed_count,
        SUM(status = 'timeout') AS timeout_count, SUM(status = 'cancelled') AS cancelled_count,
        SUM(attempt > 1) AS automatic_retry_count,
        SUM(execution_boundary = 'bounded' AND retry_of_execution_id IS NOT NULL) AS user_retry_count,
        SUM(recovered) AS recovery_count,
        AVG(queue_ms) AS avg_queue_duration_ms, AVG(run_ms) AS avg_execution_duration_ms,
        -- Continuous P95 interpolates the two surrounding ranks in the non-null population.
        SUM(CASE WHEN queue_rank BETWEEN FLOOR(queue_position) AND CEIL(queue_position)
                 THEN queue_ms * (1 - ABS(CAST(queue_rank AS DECIMAL(20,0)) - queue_position)) END) AS p95_queue_duration_ms,
        SUM(CASE WHEN run_rank BETWEEN FLOOR(run_position) AND CEIL(run_position)
                 THEN run_ms * (1 - ABS(CAST(run_rank AS DECIMAL(20,0)) - run_position)) END) AS p95_execution_duration_ms
    FROM ranked GROUP BY module, task_type, execution_boundary
), current_state AS (
    SELECT module, task_type, execution_boundary, SUM(status = 'pending') AS pending_count, SUM(status = 'running') AS running_count
    FROM common.task_executions
    WHERE tenant_id = ? AND status IN ('pending','running')
      AND (? = '' OR module = ?) AND /*EXECUTION_READ_SCOPE*/
    GROUP BY module, task_type, execution_boundary
), metric_groups AS (
    SELECT module, task_type, execution_boundary FROM aggregated
    UNION
    SELECT module, task_type, execution_boundary FROM current_state
)
SELECT g.module, g.task_type, g.execution_boundary,
    COALESCE(a.created_count,0) AS created_count, COALESCE(a.completed_count,0) AS completed_count,
    COALESCE(a.success_count,0) AS success_count, COALESCE(a.failed_count,0) AS failed_count,
    COALESCE(a.timeout_count,0) AS timeout_count, COALESCE(a.cancelled_count,0) AS cancelled_count,
    COALESCE(c.pending_count,0) AS pending_count, COALESCE(c.running_count,0) AS running_count,
    COALESCE(a.automatic_retry_count,0) AS automatic_retry_count, COALESCE(a.user_retry_count,0) AS user_retry_count,
    COALESCE(a.recovery_count,0) AS recovery_count,
    COALESCE(a.avg_queue_duration_ms,0) AS avg_queue_duration_ms, COALESCE(a.p95_queue_duration_ms,0) AS p95_queue_duration_ms,
    COALESCE(a.avg_execution_duration_ms,0) AS avg_execution_duration_ms, COALESCE(a.p95_execution_duration_ms,0) AS p95_execution_duration_ms
FROM metric_groups g LEFT JOIN aggregated a USING (module, task_type, execution_boundary)
LEFT JOIN current_state c USING (module, task_type, execution_boundary)
ORDER BY g.module, g.task_type, g.execution_boundary

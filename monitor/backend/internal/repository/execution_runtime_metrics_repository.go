package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// ExecutionRuntimeMetricRow is one PostgreSQL-aggregated runtime metric group.
type ExecutionRuntimeMetricRow struct {
	Module                 string  `gorm:"column:module"`
	TaskType               string  `gorm:"column:task_type"`
	ExecutionBoundary      string  `gorm:"column:execution_boundary"`
	CreatedCount           int64   `gorm:"column:created_count"`
	CompletedCount         int64   `gorm:"column:completed_count"`
	SuccessCount           int64   `gorm:"column:success_count"`
	FailedCount            int64   `gorm:"column:failed_count"`
	TimeoutCount           int64   `gorm:"column:timeout_count"`
	CancelledCount         int64   `gorm:"column:cancelled_count"`
	PendingCount           int64   `gorm:"column:pending_count"`
	RunningCount           int64   `gorm:"column:running_count"`
	AutomaticRetryCount    int64   `gorm:"column:automatic_retry_count"`
	UserRetryCount         int64   `gorm:"column:user_retry_count"`
	RecoveryCount          int64   `gorm:"column:recovery_count"`
	AvgQueueDurationMs     float64 `gorm:"column:avg_queue_duration_ms"`
	P95QueueDurationMs     float64 `gorm:"column:p95_queue_duration_ms"`
	AvgExecutionDurationMs float64 `gorm:"column:avg_execution_duration_ms"`
	P95ExecutionDurationMs float64 `gorm:"column:p95_execution_duration_ms"`
}

// ExecutionRuntimeMetricsRepository aggregates execution facts without sampling them in the API process.
type ExecutionRuntimeMetricsRepository struct {
	db *gorm.DB
}

func NewExecutionRuntimeMetricsRepository(db *gorm.DB) *ExecutionRuntimeMetricsRepository {
	return &ExecutionRuntimeMetricsRepository{db: db}
}

func (r *ExecutionRuntimeMetricsRepository) List(
	ctx context.Context,
	tenantID int,
	module string,
	windowStartedAt time.Time,
	windowEndedAt time.Time,
) ([]ExecutionRuntimeMetricRow, error) {
	const query = `
WITH windowed AS (
    SELECT
        module,
        task_type,
        execution_boundary,
        status,
        attempt,
        retry_of_execution_id,
        GREATEST(
            EXTRACT(EPOCH FROM (COALESCE(started_at, completed_at, ?::timestamptz) - created_at)) * 1000,
            0
        )::double precision AS queue_duration_ms,
        CASE
            WHEN started_at IS NULL THEN NULL
            ELSE GREATEST(
                COALESCE(
                    execution_time_ms::double precision,
                    EXTRACT(EPOCH FROM (COALESCE(completed_at, ?::timestamptz) - started_at)) * 1000
                ),
                0
            )::double precision
        END AS execution_duration_ms,
        (
            NULLIF(BTRIM(COALESCE(metadata ->> 'recovery_reason', '')), '') IS NOT NULL
            OR COALESCE(error_details ->> 'code', '') ILIKE '%lease_expired%'
            OR COALESCE(current_step, '') ILIKE '%lease expired%'
        ) AS recovered
    FROM common.task_executions
    WHERE tenant_id = ?
      AND created_at >= ?
      AND created_at <= ?
      AND (? = '' OR module = ?)
),
aggregated AS (
    SELECT
        module,
        task_type,
        execution_boundary,
        COUNT(*) AS created_count,
        COUNT(*) FILTER (WHERE status IN ('success', 'failed', 'timeout', 'cancelled')) AS completed_count,
        COUNT(*) FILTER (WHERE status = 'success') AS success_count,
        COUNT(*) FILTER (WHERE status = 'failed') AS failed_count,
        COUNT(*) FILTER (WHERE status = 'timeout') AS timeout_count,
        COUNT(*) FILTER (WHERE status = 'cancelled') AS cancelled_count,
        COUNT(*) FILTER (WHERE attempt > 1) AS automatic_retry_count,
        COUNT(*) FILTER (WHERE retry_of_execution_id IS NOT NULL) AS user_retry_count,
        COUNT(*) FILTER (WHERE recovered) AS recovery_count,
        COALESCE(AVG(queue_duration_ms), 0)::double precision AS avg_queue_duration_ms,
        COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY queue_duration_ms), 0)::double precision AS p95_queue_duration_ms,
        COALESCE(AVG(execution_duration_ms), 0)::double precision AS avg_execution_duration_ms,
        COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY execution_duration_ms), 0)::double precision AS p95_execution_duration_ms
    FROM windowed
    GROUP BY module, task_type, execution_boundary
),
current_state AS (
    SELECT
        module,
        task_type,
        execution_boundary,
        COUNT(*) FILTER (WHERE status = 'pending') AS pending_count,
        COUNT(*) FILTER (WHERE status = 'running') AS running_count
    FROM common.task_executions
    WHERE tenant_id = ?
      AND status IN ('pending', 'running')
      AND (? = '' OR module = ?)
    GROUP BY module, task_type, execution_boundary
)
SELECT
    COALESCE(a.module, c.module) AS module,
    COALESCE(a.task_type, c.task_type) AS task_type,
    COALESCE(a.execution_boundary, c.execution_boundary) AS execution_boundary,
    COALESCE(a.created_count, 0) AS created_count,
    COALESCE(a.completed_count, 0) AS completed_count,
    COALESCE(a.success_count, 0) AS success_count,
    COALESCE(a.failed_count, 0) AS failed_count,
    COALESCE(a.timeout_count, 0) AS timeout_count,
    COALESCE(a.cancelled_count, 0) AS cancelled_count,
    COALESCE(c.pending_count, 0) AS pending_count,
    COALESCE(c.running_count, 0) AS running_count,
    COALESCE(a.automatic_retry_count, 0) AS automatic_retry_count,
    COALESCE(a.user_retry_count, 0) AS user_retry_count,
    COALESCE(a.recovery_count, 0) AS recovery_count,
    COALESCE(a.avg_queue_duration_ms, 0) AS avg_queue_duration_ms,
    COALESCE(a.p95_queue_duration_ms, 0) AS p95_queue_duration_ms,
    COALESCE(a.avg_execution_duration_ms, 0) AS avg_execution_duration_ms,
    COALESCE(a.p95_execution_duration_ms, 0) AS p95_execution_duration_ms
FROM aggregated a
FULL OUTER JOIN current_state c
    ON c.module = a.module
   AND c.task_type = a.task_type
   AND c.execution_boundary = a.execution_boundary
ORDER BY module, task_type, execution_boundary`

	var rows []ExecutionRuntimeMetricRow
	err := r.db.WithContext(ctx).Raw(
		query,
		windowEndedAt.UTC(),
		windowEndedAt.UTC(),
		tenantID,
		windowStartedAt.UTC(),
		windowEndedAt.UTC(),
		module,
		module,
		tenantID,
		module,
		module,
	).Scan(&rows).Error
	return rows, err
}

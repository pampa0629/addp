package repository

import (
	"context"
	_ "embed"
	"strings"
	"time"

	"github.com/addp/common/execution"
	"gorm.io/gorm"
)

//go:embed execution_runtime_metrics.postgres.sql
var postgresRuntimeMetrics string

//go:embed execution_runtime_metrics.mysql.sql
var mysqlRuntimeMetrics string

// ExecutionRuntimeMetricRow is one database-aggregated runtime metric group.
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
	query := postgresRuntimeMetrics
	if r.db.Dialector.Name() == "mysql" {
		query = mysqlRuntimeMetrics
	}

	var rows []ExecutionRuntimeMetricRow
	predicate, readArgs := execution.ReadPredicate(ctx)
	if predicate == "" {
		predicate = "1 = 1"
	}
	scopedQuery := strings.ReplaceAll(query, "/*EXECUTION_READ_SCOPE*/", predicate)
	args := []interface{}{
		windowEndedAt.UTC(),
		windowEndedAt.UTC(),
		tenantID,
		windowStartedAt.UTC(),
		windowEndedAt.UTC(),
		module,
		module,
	}
	args = append(args, readArgs...)
	args = append(args,
		tenantID,
		module,
		module,
	)
	args = append(args, readArgs...)
	err := r.db.WithContext(ctx).Raw(scopedQuery, args...).Scan(&rows).Error
	return rows, err
}

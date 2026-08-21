package repository

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresExecutionRuntimeMetrics(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	db, err := gorm.Open(postgres.Open(runtimeMetricsIntegrationDSN()), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatalf("ensure common execution store: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	tenantID := int(now.UnixNano()%50000000 + 940000000)
	t.Cleanup(func() {
		_ = db.Where("tenant_id = ?", tenantID).Delete(&commonExecution.TaskExecution{}).Error
	})

	startedAt := now.Add(-90 * time.Minute)
	completedAt := now.Add(-60 * time.Minute)
	retryOf := fmt.Sprintf("original-%d", tenantID)
	executionTimeMs := int64(30 * time.Minute / time.Millisecond)
	executions := []commonExecution.TaskExecution{
		{
			TenantID: tenantID, ExecutionID: fmt.Sprintf("metrics-success-%d", tenantID),
			Module: commonExecution.ModuleQuality, TaskType: commonExecution.TaskTypeQualityCheck,
			Source: commonExecution.ModuleQuality, Status: commonExecution.ExecutionStatusSuccess,
			ExecutionBoundary: commonExecution.ExecutionBoundaryBounded, RetryOfExecutionID: &retryOf,
			Attempt: 2, MaxAttempts: 3, TriggerType: "manual",
			Metadata:        models.JSONMap{"recovery_reason": "lease_expired"},
			ExecutionTimeMs: &executionTimeMs, StartedAt: &startedAt, CompletedAt: &completedAt,
			CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now,
		},
		{
			TenantID: tenantID, ExecutionID: fmt.Sprintf("metrics-pending-%d", tenantID),
			Module: commonExecution.ModuleQuality, TaskType: commonExecution.TaskTypeQualityCheck,
			Source: commonExecution.ModuleQuality, Status: commonExecution.ExecutionStatusPending,
			ExecutionBoundary: commonExecution.ExecutionBoundaryBounded,
			Attempt:           0, MaxAttempts: 3, TriggerType: "scheduled",
			CreatedAt: now.Add(-48 * time.Hour), UpdatedAt: now,
		},
	}
	if err := db.Create(&executions).Error; err != nil {
		t.Fatalf("create executions: %v", err)
	}

	rows, err := NewExecutionRuntimeMetricsRepository(db).List(
		context.Background(), tenantID, commonExecution.ModuleQuality, now.Add(-24*time.Hour), now,
	)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1: %#v", len(rows), rows)
	}
	row := rows[0]
	if row.CreatedCount != 1 || row.CompletedCount != 1 || row.SuccessCount != 1 {
		t.Fatalf("window counts = created %d completed %d success %d", row.CreatedCount, row.CompletedCount, row.SuccessCount)
	}
	if row.PendingCount != 1 || row.RunningCount != 0 {
		t.Fatalf("current backlog = pending %d running %d", row.PendingCount, row.RunningCount)
	}
	if row.AutomaticRetryCount != 1 || row.UserRetryCount != 1 || row.RecoveryCount != 1 {
		t.Fatalf("retry/recovery counts = %d/%d/%d", row.AutomaticRetryCount, row.UserRetryCount, row.RecoveryCount)
	}
	if row.AvgQueueDurationMs <= 0 || row.P95QueueDurationMs <= 0 || row.AvgExecutionDurationMs != float64(executionTimeMs) {
		t.Fatalf("duration metrics = queue %v/%v execution %v", row.AvgQueueDurationMs, row.P95QueueDurationMs, row.AvgExecutionDurationMs)
	}
}

func runtimeMetricsIntegrationDSN() string {
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		runtimeMetricsIntegrationEnv("ADDP_TEST_POSTGRES_HOST", "localhost"),
		runtimeMetricsIntegrationEnv("ADDP_TEST_POSTGRES_PORT", "15432"),
		runtimeMetricsIntegrationEnv("ADDP_TEST_POSTGRES_USER", "addp"),
		runtimeMetricsIntegrationEnv("ADDP_TEST_POSTGRES_PASSWORD", "addp_password"),
		runtimeMetricsIntegrationEnv("ADDP_TEST_POSTGRES_DATABASE", "addp_test"),
		runtimeMetricsIntegrationEnv("ADDP_TEST_POSTGRES_SSLMODE", "disable"),
	)
}

func runtimeMetricsIntegrationEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

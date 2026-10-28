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
			Module: commonExecution.ModuleQuality, TaskType: commonExecution.TaskTypeQualityPlan,
			Source: commonExecution.ModuleQuality, Status: commonExecution.ExecutionStatusSuccess,
			ExecutionBoundary: commonExecution.ExecutionBoundaryBounded, RetryOfExecutionID: &retryOf,
			Attempt: 2, MaxAttempts: 3, TriggerType: "manual",
			Metadata:        models.JSONMap{"recovery_reason": "lease_expired"},
			ExecutionTimeMs: &executionTimeMs, StartedAt: &startedAt, CompletedAt: &completedAt,
			CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now,
		},
		{
			TenantID: tenantID, ExecutionID: fmt.Sprintf("metrics-pending-%d", tenantID),
			Module: commonExecution.ModuleQuality, TaskType: commonExecution.TaskTypeQualityPlan,
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
	taskID := "deleted-task"
	if err := db.Model(&commonExecution.TaskExecution{}).Where("execution_id = ?", executions[0].ExecutionID).Update("source_task_id", taskID).Error; err != nil {
		t.Fatal(err)
	}
	scope := commonExecution.ReadScope{TenantID: tenantID, PrincipalID: 9, Module: "quality", Grants: []commonExecution.ReadGrant{{TaskType: "quality_plan", TaskHistory: true}}}
	ctx := commonExecution.WithReadScopes(context.Background(), []commonExecution.ReadScope{scope})
	rows, err = NewExecutionRuntimeMetricsRepository(db).List(ctx, tenantID, "quality", now.Add(-24*time.Hour), now)
	if err != nil || len(rows) != 1 || rows[0].CreatedCount != 1 || rows[0].PendingCount != 0 {
		t.Fatalf("scoped window/backlog=%#v err=%v", rows, err)
	}
	trend, err := commonExecution.NewTaskExecutionRepository(db).GetTrendData(ctx, tenantID, "quality", 7)
	if err != nil || len(trend) != 1 || trend[0].Total != 1 {
		t.Fatalf("scoped trend=%#v err=%v", trend, err)
	}
	ctx = commonExecution.WithReadScopes(context.Background(), []commonExecution.ReadScope{})
	rows, err = NewExecutionRuntimeMetricsRepository(db).List(ctx, tenantID, "", now.Add(-24*time.Hour), now)
	if err != nil || len(rows) != 0 {
		t.Fatalf("denied metrics=%#v err=%v", rows, err)
	}

}

// Exercise the persisted claim/recovery facts through the unchanged SQL formulas.
func TestIntegrationPostgresExecutionRuntimeMetricsRecoveredAttemptTimes(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	db, err := gorm.Open(postgres.Open(runtimeMetricsIntegrationDSN()), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	for _, outcome := range []string{"reclaim", "fail_pending"} {
		t.Run(outcome, func(t *testing.T) {
			ctx := t.Context()
			now := time.Now().UTC().Truncate(time.Microsecond)
			tenantID := int(now.UnixNano()%50000000 + 940000000)
			item := commonExecution.TaskExecution{
				TenantID: tenantID, ExecutionID: fmt.Sprintf("metrics-recovery-%s-%d", outcome, tenantID),
				Module: commonExecution.ModuleQuality, TaskType: commonExecution.TaskTypeQualityPlan, Source: commonExecution.ModuleQuality,
				Status: commonExecution.ExecutionStatusPending, ExecutionBoundary: commonExecution.ExecutionBoundaryBounded,
				TriggerType: commonExecution.TriggerTypeManual, MaxAttempts: 3, CreatedAt: now.Add(-10 * time.Minute), UpdatedAt: now,
			}
			if err := db.Create(&item).Error; err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := db.Where("execution_id = ?", item.ExecutionID).Delete(&commonExecution.TaskExecution{}).Error; err != nil {
					t.Error(err)
				}
			})
			assertMetrics := func(queue, runtime time.Duration, pending, running, completed int64) {
				t.Helper()
				rows, err := NewExecutionRuntimeMetricsRepository(db).List(ctx, tenantID, commonExecution.ModuleQuality, now.Add(-time.Hour), now)
				if err != nil || len(rows) != 1 {
					t.Fatalf("List = %#v, %v", rows, err)
				}
				row := rows[0]
				if row.AvgQueueDurationMs != float64(queue.Milliseconds()) || row.P95QueueDurationMs != float64(queue.Milliseconds()) || row.AvgExecutionDurationMs != float64(runtime.Milliseconds()) || row.P95ExecutionDurationMs != float64(runtime.Milliseconds()) || row.PendingCount != pending || row.RunningCount != running || row.CompletedCount != completed {
					t.Fatalf("attempt time projection = %#v, want queue=%v runtime=%v states=%d/%d/%d", row, queue, runtime, pending, running, completed)
				}
			}
			options := commonExecution.ClaimOptions{Module: item.Module, TaskType: item.TaskType, WorkerID: "metrics-worker", Now: now.Add(-8 * time.Minute), LeaseDuration: time.Minute}
			var firstLease *commonExecution.Lease
			if err := db.Transaction(func(tx *gorm.DB) error {
				_, firstLease, err = commonExecution.ClaimNext(ctx, tx, options)
				return err
			}); err != nil || firstLease == nil {
				t.Fatalf("first claim = %#v, %v", firstLease, err)
			}
			if err := db.Transaction(func(tx *gorm.DB) error {
				return commonExecution.RetryExpired(ctx, tx, *firstLease, now.Add(-6*time.Minute), "worker lease expired; retry pending")
			}); err != nil {
				t.Fatal(err)
			}
			assertMetrics(10*time.Minute, 0, 1, 0, 0)
			if outcome == "fail_pending" {
				// The owner may reject a recovered queue item before a new claim.
				completedAt := now.Add(-3 * time.Minute)
				if err := db.Model(&commonExecution.TaskExecution{}).Where("execution_id = ? AND status = ?", item.ExecutionID, commonExecution.ExecutionStatusPending).Updates(map[string]interface{}{
					"status": commonExecution.ExecutionStatusFailed, "completed_at": completedAt,
					"error_details": models.JSONMap{"code": "quality.execution.authorization_failed"},
				}).Error; err != nil {
					t.Fatal(err)
				}
				assertMetrics(7*time.Minute, 0, 0, 0, 1)
				return
			}
			options.Now, options.LeaseDuration = now.Add(-4*time.Minute), time.Hour
			var secondLease *commonExecution.Lease
			if err := db.Transaction(func(tx *gorm.DB) error {
				_, secondLease, err = commonExecution.ClaimNext(ctx, tx, options)
				return err
			}); err != nil || secondLease == nil || secondLease.Attempt != 2 || secondLease.Token == firstLease.Token {
				t.Fatalf("second claim = %#v, %v", secondLease, err)
			}
			assertMetrics(6*time.Minute, 4*time.Minute, 0, 1, 0)
			completedAt := now.Add(-2 * time.Minute)
			if err := commonExecution.CompleteWithLease(ctx, db, *secondLease, commonExecution.ExecutionStatusSuccess, completedAt, map[string]interface{}{
				"execution_time_ms": completedAt.Sub(options.Now).Milliseconds(),
			}); err != nil {
				t.Fatal(err)
			}
			assertMetrics(6*time.Minute, 2*time.Minute, 0, 0, 1)
		})
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

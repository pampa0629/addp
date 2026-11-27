package repository

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	qualityMigration "github.com/addp/quality/internal/migration"
	"github.com/addp/quality/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresQualityConcurrentPendingClaim(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	db, err := gorm.Open(postgres.Open(qualityRepositoryIntegrationDSN()), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatalf("ensure common execution store: %v", err)
	}
	if err := qualityMigration.NewRunner(db).Run(context.Background()); err != nil {
		t.Fatalf("run quality migrations: %v", err)
	}

	tenantID := time.Now().UnixNano()%100000000 + 910000000
	t.Cleanup(func() {
		_ = db.Where("tenant_id = ?", tenantID).Delete(&commonExecution.TaskExecution{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&models.Issue{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&models.QualityPlan{}).Error
	})
	exerciseQualityConcurrentPendingClaim(t, db, tenantID)
}

func exerciseQualityConcurrentPendingClaim(t *testing.T, db *gorm.DB, tenantID int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	task := createPlanRepositoryTestTask(t, db, tenantID)
	repo := NewPlanRepository(db)
	createdAt := time.Now().UTC()
	execution := newQualityRepositoryTestExecution(uuid.NewString(), int(tenantID), createdAt)
	if _, err := repo.CreateExecution(ctx, task.ID, tenantID, execution, models.PlanRunRequest{}); err != nil {
		t.Fatalf("create pending execution: %v", err)
	}
	if err := repo.AttachPendingAuthorization(ctx, tenantID, execution.ExecutionID, qualityTestAuthorization(int64(1), createdAt.Add(time.Hour))); err != nil {
		t.Fatalf("attach authorization: %v", err)
	}
	start := make(chan struct{})
	ready := make(chan struct{}, 2)
	type claimResult struct {
		workerID  string
		execution *commonExecution.TaskExecution
		err       error
	}
	results := make(chan claimResult, 2)
	for _, workerID := range []string{"worker-a", "worker-b"} {
		go func() {
			result := claimResult{workerID: workerID}
			result.err = db.WithContext(ctx).Connection(func(conn *gorm.DB) error {
				ready <- struct{}{}
				select {
				case <-start:
				case <-ctx.Done():
					return ctx.Err()
				}
				var err error
				result.execution, _, err = NewPlanRepository(conn).ClaimPendingExecution(ctx, workerID, 10*time.Minute)
				return err
			})
			results <- result
		}()
	}
	for range 2 {
		select {
		case <-ready:
		case result := <-results:
			t.Fatalf("worker %s could not reserve a database connection: %v", result.workerID, result.err)
		case <-ctx.Done():
			t.Fatal("timed out reserving two database connections")
		}
	}
	close(start)

	var winner claimResult
	empty := 0
	for range 2 {
		select {
		case result := <-results:
			if result.err != nil {
				t.Fatalf("worker %s: concurrent pending claim: %v", result.workerID, result.err)
			}
			if result.execution == nil {
				empty++
				continue
			}
			winner = result
		case <-ctx.Done():
			t.Fatal("timed out waiting for both claims")
		}
	}
	if winner.execution == nil || empty != 1 || winner.execution.ExecutionID != execution.ExecutionID ||
		winner.execution.LeaseOwner == nil || *winner.execution.LeaseOwner != winner.workerID || winner.execution.LeaseToken == nil {
		t.Fatalf("expected one claim for execution %s and one empty result; empty=%d winner=%s", execution.ExecutionID, empty, winner.workerID)
	}

	var executions []commonExecution.TaskExecution
	if err := db.Where("tenant_id = ? AND module = ? AND task_type = ?", tenantID, commonExecution.ModuleQuality, commonExecution.TaskTypeQualityPlan).
		Find(&executions).Error; err != nil {
		t.Fatalf("load claimed executions: %v", err)
	}
	if len(executions) != 1 {
		t.Fatalf("execution count after claims=%d, want 1", len(executions))
	}
	stored := executions[0]
	if stored.ExecutionID != execution.ExecutionID || stored.Status != commonExecution.ExecutionStatusRunning ||
		stored.StartedAt == nil || stored.Attempt != 1 ||
		stored.LeaseExpiresAt == nil || !stored.LeaseExpiresAt.After(*stored.StartedAt) ||
		stored.LeaseOwner == nil || *stored.LeaseOwner != winner.workerID ||
		stored.LeaseToken == nil || *stored.LeaseToken != *winner.execution.LeaseToken {
		t.Fatalf("persisted execution %s does not have the winning worker's first active lease", stored.ExecutionID)
	}
	var storedTask models.QualityPlan
	if err := db.First(&storedTask, task.ID).Error; err != nil {
		t.Fatalf("load started task: %v", err)
	}
	if storedTask.LastExecutionID != executions[0].ExecutionID || storedTask.LastExecutionStatus != commonExecution.ExecutionStatusRunning {
		t.Fatalf("started task summary = %s/%s", storedTask.LastExecutionID, storedTask.LastExecutionStatus)
	}
	assertQualityEventKinds(t, db, stored.ExecutionID, "started")
	t.Logf("execution=%s winner=%s attempt=%d empty_claims=%d started_events=1 summary=%s",
		stored.ExecutionID, winner.workerID, stored.Attempt, empty, storedTask.LastExecutionStatus)
}

func TestIntegrationPostgresIssueConcurrentFirstObservation(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	db, err := gorm.Open(postgres.Open(qualityRepositoryIntegrationDSN()), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	if err := qualityMigration.NewRunner(db).Run(context.Background()); err != nil {
		t.Fatalf("run quality migrations: %v", err)
	}

	tenantID := time.Now().UnixNano()%100000000 + 920000000
	application := createPlanRepositoryTestTask(t, db, tenantID)
	t.Cleanup(func() {
		_ = db.Where("tenant_id = ?", tenantID).Delete(&models.Issue{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&models.QualityPlan{}).Error
	})

	observation := models.IssueObservation{TargetKey: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		PlanID: application.ID, RuleKey: "00000000-0000-4000-8000-000000000001", RuleType: "not_null", Severity: "error", Message: "required",
		ColumnName: "id", Table: "orders", SchemaName: "public",
		EngineID: 12, FailedCount: 1, TotalCount: 10, PassRate: 90,
	}
	start := make(chan struct{})
	errors := make(chan error, 2)
	for _, executionID := range []string{"concurrent-execution-a", "concurrent-execution-b"} {
		executionID := executionID
		go func() {
			<-start
			errors <- NewIssueRepository(db).Reconcile(context.Background(), tenantID, executionID, []models.IssueObservation{observation}, time.Now().UTC())
		}()
	}
	close(start)
	for range 2 {
		if err := <-errors; err != nil {
			t.Fatalf("concurrent Reconcile: %v", err)
		}
	}

	var issues []models.Issue
	if err := db.Where("tenant_id = ? AND plan_id = ?", tenantID, application.ID).Find(&issues).Error; err != nil {
		t.Fatalf("load reconciled issues: %v", err)
	}
	if len(issues) != 1 || issues[0].Status != "open" || issues[0].FailedCount != 1 {
		t.Fatalf("reconciled issues = %#v, want one open issue", issues)
	}
}

func qualityRepositoryIntegrationDSN() string {
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		qualityRepositoryIntegrationEnv("ADDP_TEST_POSTGRES_HOST", "localhost"),
		qualityRepositoryIntegrationEnv("ADDP_TEST_POSTGRES_PORT", "15432"),
		qualityRepositoryIntegrationEnv("ADDP_TEST_POSTGRES_USER", "addp"),
		qualityRepositoryIntegrationEnv("ADDP_TEST_POSTGRES_PASSWORD", "addp_password"),
		qualityRepositoryIntegrationEnv("ADDP_TEST_POSTGRES_DATABASE", "addp_test"),
		qualityRepositoryIntegrationEnv("ADDP_TEST_POSTGRES_SSLMODE", "disable"),
	)
}

func qualityRepositoryIntegrationEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

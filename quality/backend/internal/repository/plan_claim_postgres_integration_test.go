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
		_ = db.Where("tenant_id = ?", tenantID).Delete(&models.QualityPlan{}).Error
	})
	task := createPlanRepositoryTestTask(t, db, tenantID)
	repo := NewPlanRepository(db)
	createdAt := time.Now().UTC()
	execution := newQualityRepositoryTestExecution(fmt.Sprintf("quality-pg-%d", tenantID), int(tenantID), createdAt)
	if _, err := repo.CreateExecution(context.Background(), task.ID, tenantID, execution); err != nil {
		t.Fatalf("create pending execution: %v", err)
	}
	if err := repo.AttachPendingAuthorization(context.Background(), tenantID, execution.ExecutionID, map[string]interface{}{
		"actor_principal_id": int64(1), "actor_tenant_membership_id": int64(1), "issued_authorization_version": int64(1),
		"execution_authorization_id": int64(1),
		"authorization_expires_at":   createdAt.Add(time.Hour),
	}); err != nil {
		t.Fatalf("attach authorization: %v", err)
	}
	start := make(chan struct{})
	type claimResult struct {
		execution *commonExecution.TaskExecution
		err       error
	}
	results := make(chan claimResult, 2)
	for _, workerID := range []string{"worker-a", "worker-b"} {
		workerID := workerID
		go func() {
			<-start
			claimed, _, claimErr := repo.ClaimPendingExecution(context.Background(), workerID, createdAt, 10*time.Minute)
			results <- claimResult{execution: claimed, err: claimErr}
		}()
	}
	close(start)

	successes, empty := 0, 0
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatalf("concurrent pending claim error: %v", result.err)
		}
		if result.execution != nil {
			successes++
		} else {
			empty++
		}
	}
	if successes != 1 || empty != 1 {
		t.Fatalf("pending claim results success=%d empty=%d, want 1/1", successes, empty)
	}

	var executions []commonExecution.TaskExecution
	if err := db.Where("tenant_id = ? AND module = ? AND task_type = ?", tenantID, commonExecution.ModuleQuality, commonExecution.TaskTypeQualityPlan).
		Find(&executions).Error; err != nil {
		t.Fatalf("load claimed executions: %v", err)
	}
	if len(executions) != 1 || executions[0].Status != commonExecution.ExecutionStatusRunning || executions[0].StartedAt == nil {
		t.Fatalf("claimed executions = %#v", executions)
	}
	var storedTask models.QualityPlan
	if err := db.First(&storedTask, task.ID).Error; err != nil {
		t.Fatalf("load started task: %v", err)
	}
	if storedTask.LastExecutionID != executions[0].ExecutionID || storedTask.LastExecutionStatus != commonExecution.ExecutionStatusRunning {
		t.Fatalf("started task summary = %s/%s", storedTask.LastExecutionID, storedTask.LastExecutionStatus)
	}
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

	observation := models.IssueObservation{
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

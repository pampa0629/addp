package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/quality/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresQualityConcurrentClaimAndStart(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	db, err := gorm.Open(postgres.Open(qualityRepositoryIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS quality").Error; err != nil {
		t.Fatalf("create quality schema: %v", err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatalf("ensure common execution store: %v", err)
	}
	if err := db.AutoMigrate(&models.CheckTask{}); err != nil {
		t.Fatalf("migrate quality check task: %v", err)
	}

	tenantID := time.Now().UnixNano()%100000000 + 910000000
	t.Cleanup(func() {
		_ = db.Where("tenant_id = ?", tenantID).Delete(&commonExecution.TaskExecution{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&models.CheckTask{}).Error
	})
	task := createCheckTaskRepositoryTestTask(t, db, tenantID)
	repo := NewCheckTaskRepository(db)
	createdAt := time.Now().UTC()
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, executionID := range []string{
		fmt.Sprintf("quality-pg-%d-a", tenantID),
		fmt.Sprintf("quality-pg-%d-b", tenantID),
	} {
		executionID := executionID
		go func() {
			<-start
			_, claimErr := repo.ClaimExecution(context.Background(), task.ID, tenantID, newQualityRepositoryTestExecution(executionID, int(tenantID), createdAt))
			results <- claimErr
		}()
	}
	close(start)

	successes, conflicts := 0, 0
	for range 2 {
		claimErr := <-results
		switch {
		case claimErr == nil:
			successes++
		case errors.Is(claimErr, commonAPI.ErrConflict):
			conflicts++
		default:
			t.Fatalf("concurrent claim error: %v", claimErr)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("claim results success=%d conflict=%d, want 1/1", successes, conflicts)
	}

	var executions []commonExecution.TaskExecution
	if err := db.Where("tenant_id = ? AND module = ? AND task_type = ?", tenantID, commonExecution.ModuleQuality, commonExecution.TaskTypeQualityCheck).
		Find(&executions).Error; err != nil {
		t.Fatalf("load claimed executions: %v", err)
	}
	if len(executions) != 1 || executions[0].Status != commonExecution.ExecutionStatusPending || executions[0].StartedAt != nil {
		t.Fatalf("claimed executions = %#v", executions)
	}
	startedAt := createdAt.Add(time.Second)
	if err := repo.StartExecution(context.Background(), task.ID, tenantID, executions[0].ExecutionID, startedAt); err != nil {
		t.Fatalf("start claimed execution: %v", err)
	}
	var storedTask models.CheckTask
	if err := db.First(&storedTask, task.ID).Error; err != nil {
		t.Fatalf("load started task: %v", err)
	}
	if storedTask.LastExecutionID != executions[0].ExecutionID || storedTask.LastExecutionStatus != commonExecution.ExecutionStatusRunning {
		t.Fatalf("started task summary = %s/%s", storedTask.LastExecutionID, storedTask.LastExecutionStatus)
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

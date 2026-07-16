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
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresManagerTileCacheConcurrentClaimAndStart(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	db, err := gorm.Open(postgres.Open(managerTileCacheRepositoryIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS manager").Error; err != nil {
		t.Fatalf("create manager schema: %v", err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatalf("ensure common execution store: %v", err)
	}
	if err := db.AutoMigrate(&models.TileCacheTask{}, &models.TileCache{}); err != nil {
		t.Fatalf("migrate manager tile cache tables: %v", err)
	}

	tenantID := uint(time.Now().UnixNano()%100000000 + 930000000)
	t.Cleanup(func() {
		_ = db.Unscoped().Where("tenant_id = ?", tenantID).Delete(&models.TileCache{}).Error
		_ = db.Where("tenant_id = ?", int(tenantID)).Delete(&commonExecution.TaskExecution{}).Error
		_ = db.Unscoped().Where("tenant_id = ?", tenantID).Delete(&models.TileCacheTask{}).Error
	})
	task := models.TileCacheTask{
		TenantID: tenantID, Name: "manager-tile-cache-integration", Enabled: true,
		Config: commonModels.JSONMap{
			"target": commonModels.JSONMap{"item_fingerprint": fmt.Sprintf("manager-pg-%d", tenantID)},
			"tile":   commonModels.JSONMap{"format": "mvt"},
		},
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create tile cache task: %v", err)
	}

	repo := NewTileCacheRepository(db)
	createdAt := time.Now().UTC()
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, executionID := range []string{
		fmt.Sprintf("manager-tile-pg-%d-a", tenantID),
		fmt.Sprintf("manager-tile-pg-%d-b", tenantID),
	} {
		executionID := executionID
		go func() {
			<-start
			_, claimErr := repo.ClaimExecution(
				context.Background(), task.ID, tenantID,
				newTileCacheRepositoryTestExecution(executionID, int(tenantID), createdAt),
			)
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
	if err := db.Where(
		"tenant_id = ? AND module = ? AND task_type = ? AND source_task_id = ?",
		int(tenantID), commonExecution.ModuleManager, commonExecution.TaskTypeVectorTileCacheGeneration, fmt.Sprint(task.ID),
	).Find(&executions).Error; err != nil {
		t.Fatalf("load claimed executions: %v", err)
	}
	if len(executions) != 1 || executions[0].Status != commonExecution.ExecutionStatusPending || executions[0].StartedAt != nil {
		t.Fatalf("claimed executions = %#v", executions)
	}
	startedAt := createdAt.Add(time.Second)
	if err := repo.StartExecution(context.Background(), task.ID, tenantID, executions[0].ExecutionID, startedAt); err != nil {
		t.Fatalf("start claimed execution: %v", err)
	}
	var storedTask models.TileCacheTask
	if err := db.First(&storedTask, task.ID).Error; err != nil {
		t.Fatalf("load started task: %v", err)
	}
	if storedTask.LastExecutionID == nil || *storedTask.LastExecutionID != executions[0].ExecutionID ||
		storedTask.LastExecutionStatus == nil || *storedTask.LastExecutionStatus != commonExecution.ExecutionStatusRunning {
		t.Fatalf("started task summary = %#v/%#v", storedTask.LastExecutionID, storedTask.LastExecutionStatus)
	}
}

func TestIntegrationPostgresManagerVectorMaterializedViewConcurrentClaimAndStart(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	db, err := gorm.Open(postgres.Open(managerTileCacheRepositoryIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS manager").Error; err != nil {
		t.Fatalf("create manager schema: %v", err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatalf("ensure common execution store: %v", err)
	}
	if err := db.AutoMigrate(&models.VectorMaterializedViewTask{}, &models.VectorMaterializedView{}); err != nil {
		t.Fatalf("migrate manager vector materialized view tables: %v", err)
	}

	tenantID := uint(time.Now().UnixNano()%100000000 + 940000000)
	t.Cleanup(func() {
		_ = db.Unscoped().Where("tenant_id = ?", tenantID).Delete(&models.VectorMaterializedView{}).Error
		_ = db.Where("tenant_id = ?", int(tenantID)).Delete(&commonExecution.TaskExecution{}).Error
		_ = db.Unscoped().Where("tenant_id = ?", tenantID).Delete(&models.VectorMaterializedViewTask{}).Error
	})
	task := models.VectorMaterializedViewTask{
		TenantID: tenantID, Name: "manager-vmv-integration", Enabled: true,
		Config: commonModels.JSONMap{
			"target": commonModels.JSONMap{"item_fingerprint": fmt.Sprintf("manager-vmv-pg-%d", tenantID)},
		},
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create vector materialized view task: %v", err)
	}

	repo := NewVectorMaterializedViewRepository(db)
	createdAt := time.Now().UTC()
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, executionID := range []string{
		fmt.Sprintf("manager-vmv-pg-%d-a", tenantID),
		fmt.Sprintf("manager-vmv-pg-%d-b", tenantID),
	} {
		executionID := executionID
		go func() {
			<-start
			_, claimErr := repo.ClaimExecution(
				context.Background(), task.ID, tenantID,
				newManagerRepositoryTestExecution(
					executionID, int(tenantID), commonExecution.TaskTypeVectorMaterializedViewGeneration, createdAt,
				),
			)
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
	if err := db.Where(
		"tenant_id = ? AND module = ? AND task_type = ? AND source_task_id = ?",
		int(tenantID), commonExecution.ModuleManager, commonExecution.TaskTypeVectorMaterializedViewGeneration, fmt.Sprint(task.ID),
	).Find(&executions).Error; err != nil {
		t.Fatalf("load claimed executions: %v", err)
	}
	if len(executions) != 1 || executions[0].Status != commonExecution.ExecutionStatusPending || executions[0].StartedAt != nil {
		t.Fatalf("claimed executions = %#v", executions)
	}
	startedAt := createdAt.Add(time.Second)
	if err := repo.StartExecution(context.Background(), task.ID, tenantID, executions[0].ExecutionID, startedAt); err != nil {
		t.Fatalf("start claimed execution: %v", err)
	}
	var storedTask models.VectorMaterializedViewTask
	if err := db.First(&storedTask, task.ID).Error; err != nil {
		t.Fatalf("load started task: %v", err)
	}
	if storedTask.LastExecutionID == nil || *storedTask.LastExecutionID != executions[0].ExecutionID ||
		storedTask.LastExecutionStatus == nil || *storedTask.LastExecutionStatus != commonExecution.ExecutionStatusRunning {
		t.Fatalf("started task summary = %#v/%#v", storedTask.LastExecutionID, storedTask.LastExecutionStatus)
	}
}

func managerTileCacheRepositoryIntegrationDSN() string {
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		managerTileCacheRepositoryIntegrationEnv("ADDP_TEST_POSTGRES_HOST", "localhost"),
		managerTileCacheRepositoryIntegrationEnv("ADDP_TEST_POSTGRES_PORT", "15432"),
		managerTileCacheRepositoryIntegrationEnv("ADDP_TEST_POSTGRES_USER", "addp"),
		managerTileCacheRepositoryIntegrationEnv("ADDP_TEST_POSTGRES_PASSWORD", "addp_password"),
		managerTileCacheRepositoryIntegrationEnv("ADDP_TEST_POSTGRES_DATABASE", "addp_test"),
		managerTileCacheRepositoryIntegrationEnv("ADDP_TEST_POSTGRES_SSLMODE", "disable"),
	)
}

func managerTileCacheRepositoryIntegrationEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

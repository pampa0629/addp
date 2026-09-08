package repository

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresManagerUnifiedTaskDefinitionLifecycle(t *testing.T) {
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

	tenantID := uint(time.Now().UnixNano()%100000000 + 960000000)
	legacyExecutionID := fmt.Sprintf("manager-legacy-model3d-tiles-%d", tenantID)
	derivedExecutionID := fmt.Sprintf("manager-derived-vector-tile-cache-%d", tenantID)
	t.Cleanup(func() {
		_ = db.Exec(`DROP TABLE IF EXISTS manager.model_3d_tiles_tasks`).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&models.TaskResourceBinding{}).Error
		_ = db.Unscoped().Where("tenant_id = ?", tenantID).Delete(&models.TaskDefinition{}).Error
		_ = db.Where("tenant_id = ?", int(tenantID)).Delete(&commonExecution.TaskExecution{}).Error
	})

	if err := db.Exec(`DROP TABLE IF EXISTS manager.model_3d_tiles_tasks`).Error; err != nil {
		t.Fatalf("drop stale legacy table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.model_3d_tiles_tasks (id BIGSERIAL PRIMARY KEY)`).Error; err != nil {
		t.Fatalf("create legacy task table: %v", err)
	}
	legacyExecution := &commonExecution.TaskExecution{
		TenantID:          int(tenantID),
		ExecutionID:       legacyExecutionID,
		Module:            commonExecution.ModuleManager,
		TaskType:          "model_3d_tiles_generation",
		Source:            commonExecution.ModuleManager,
		Status:            commonExecution.ExecutionStatusSuccess,
		ExecutionBoundary: commonExecution.ExecutionBoundaryBounded,
		TriggerType:       commonExecution.TriggerTypeManual,
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	if err := db.Create(legacyExecution).Error; err != nil {
		t.Fatalf("create legacy execution: %v", err)
	}
	derivedExecution := *legacyExecution
	derivedExecution.ID = 0
	derivedExecution.ExecutionID = derivedExecutionID
	derivedExecution.TaskType = commonExecution.TaskTypeVectorTileCacheGeneration
	if err := db.Create(&derivedExecution).Error; err != nil {
		t.Fatalf("create derived execution tied to legacy task schema: %v", err)
	}

	if err := dropLegacyQuickViewTables(db); err != nil {
		t.Fatalf("drop legacy quick view state: %v", err)
	}
	if err := ensureTaskDefinitionSchema(db); err != nil {
		t.Fatalf("ensure unified task definition schema: %v", err)
	}
	var legacyTableExists bool
	if err := db.Raw(`SELECT to_regclass('manager.model_3d_tiles_tasks') IS NOT NULL`).Scan(&legacyTableExists).Error; err != nil {
		t.Fatalf("check legacy task table: %v", err)
	}
	if legacyTableExists {
		t.Fatal("legacy model_3d_tiles_tasks table still exists")
	}
	var obsoleteExecutionCount int64
	if err := db.Model(&commonExecution.TaskExecution{}).Where("execution_id IN ?", []string{legacyExecutionID, derivedExecutionID}).Count(&obsoleteExecutionCount).Error; err != nil {
		t.Fatalf("count obsolete executions: %v", err)
	}
	if obsoleteExecutionCount != 0 {
		t.Fatalf("obsolete execution count = %d, want 0", obsoleteExecutionCount)
	}

	task := &models.TaskDefinition{
		TenantID: tenantID,
		Name:     "unknown-engine vector tile set",
		Enabled:  true,
		Config: commonModels.JSONMap{
			"semantic_hash": fmt.Sprintf("manager-task-%d", tenantID),
			"source": commonModels.JSONMap{
				"source_engine_id": float64(990001),
				"locator":          "addp://engine/990001/path/public/roads?type=table&item_id=91",
				"item_id":          float64(91),
				"item_fingerprint": "missing-source",
			},
			"target": commonModels.JSONMap{
				"engine_id":       float64(990002),
				"storage_locator": "addp://engine/990002/path/tiles?type=directory",
			},
		},
	}
	if err := createTaskDefinition(context.Background(), db, commonExecution.TaskTypeVectorTileSetGeneration, task); err != nil {
		t.Fatalf("create unified task definition: %v", err)
	}
	duplicateTask := &models.TaskDefinition{
		TenantID: tenantID,
		Name:     "duplicate semantic identity",
		Enabled:  true,
		Config:   task.Config,
	}
	if err := createTaskDefinition(context.Background(), db, commonExecution.TaskTypeVectorTileSetGeneration, duplicateTask); err == nil {
		t.Fatal("duplicate task semantic identity was accepted")
	}

	var bindingCount int64
	if err := db.Model(&models.TaskResourceBinding{}).Where("task_definition_id = ?", task.ID).Count(&bindingCount).Error; err != nil {
		t.Fatalf("count task resource bindings: %v", err)
	}
	if bindingCount != 2 {
		t.Fatalf("task resource binding count = %d, want 2", bindingCount)
	}

	cleanupRepo := NewCleanupTaskDefinitionRepository(db, []CleanupTaskDefinitionSpec{{
		TaskType: commonExecution.TaskTypeVectorTileSetGeneration,
		Table:    "manager.task_definitions",
	}})
	definitions, err := cleanupRepo.List(context.Background(), tenantID)
	if err != nil || len(definitions) != 1 {
		t.Fatalf("list cleanup definitions = %#v, error = %v", definitions, err)
	}
	if err := cleanupRepo.Disable(context.Background(), definitions[0], "missing_engine"); err != nil {
		t.Fatalf("disable cleanup definition: %v", err)
	}
	disabledDefinitions, err := cleanupRepo.List(context.Background(), tenantID)
	if err != nil || len(disabledDefinitions) != 1 || disabledDefinitions[0].Enabled {
		t.Fatalf("disabled cleanup definitions = %#v, error = %v", disabledDefinitions, err)
	}
	if err := cleanupRepo.HardDelete(context.Background(), disabledDefinitions[0]); err != nil {
		t.Fatalf("hard delete cleanup definition: %v", err)
	}

	var taskCount int64
	if err := db.Unscoped().Model(&models.TaskDefinition{}).Where("id = ? AND tenant_id = ?", task.ID, tenantID).Count(&taskCount).Error; err != nil {
		t.Fatalf("count deleted task: %v", err)
	}
	if err := db.Model(&models.TaskResourceBinding{}).Where("task_definition_id = ?", task.ID).Count(&bindingCount).Error; err != nil {
		t.Fatalf("count deleted task bindings: %v", err)
	}
	if taskCount != 0 || bindingCount != 0 {
		t.Fatalf("cleanup residual task count = %d, binding count = %d", taskCount, bindingCount)
	}
}

func TestIntegrationPostgresManagerPPTXUsesUnifiedTaskDefinition(t *testing.T) {
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
	if err := ensureTaskDefinitionSchema(db); err != nil {
		t.Fatalf("ensure unified task definition schema: %v", err)
	}
	if err := db.AutoMigrate(&models.PPTXPDF{}); err != nil {
		t.Fatalf("migrate PPTX result: %v", err)
	}
	if err := db.Exec(`DROP TABLE IF EXISTS manager.pptx_pdf_tasks CASCADE`).Error; err != nil {
		t.Fatalf("drop stale legacy PPTX task table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.pptx_pdf_tasks (id BIGSERIAL PRIMARY KEY)`).Error; err != nil {
		t.Fatalf("create legacy PPTX task table: %v", err)
	}

	tenantID := uint(time.Now().UnixNano()%100000000 + 970000000)
	executionID := fmt.Sprintf("manager-legacy-pptx-%d", tenantID)
	t.Cleanup(func() {
		_ = db.Exec(`DROP TABLE IF EXISTS manager.pptx_pdf_tasks CASCADE`).Error
		_ = db.Unscoped().Where("tenant_id = ?", tenantID).Delete(&models.PPTXPDF{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&models.TaskResourceBinding{}).Error
		_ = db.Unscoped().Where("tenant_id = ?", tenantID).Delete(&models.TaskDefinition{}).Error
		_ = db.Where("tenant_id = ?", int(tenantID)).Delete(&commonExecution.TaskExecution{}).Error
	})
	if err := db.Create(&commonExecution.TaskExecution{
		TenantID: int(tenantID), ExecutionID: executionID, Module: commonExecution.ModuleManager,
		TaskType: commonExecution.TaskTypePPTXPDFGeneration, Source: commonExecution.ModuleManager,
		Status: commonExecution.ExecutionStatusSuccess, ExecutionBoundary: commonExecution.ExecutionBoundaryBounded,
		TriggerType: commonExecution.TriggerTypeManual, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}).Error; err != nil {
		t.Fatalf("create legacy PPTX execution: %v", err)
	}
	if err := ensurePPTXPDFSchema(db); err != nil {
		t.Fatalf("ensure unified PPTX schema: %v", err)
	}
	var legacyTableExists bool
	if err := db.Raw(`SELECT to_regclass('manager.pptx_pdf_tasks') IS NOT NULL`).Scan(&legacyTableExists).Error; err != nil {
		t.Fatalf("check legacy PPTX task table: %v", err)
	}
	if legacyTableExists {
		t.Fatal("legacy pptx_pdf_tasks table still exists")
	}
	var executionCount int64
	if err := db.Model(&commonExecution.TaskExecution{}).Where("execution_id = ?", executionID).Count(&executionCount).Error; err != nil {
		t.Fatalf("count legacy PPTX execution: %v", err)
	}
	if executionCount != 0 {
		t.Fatalf("legacy PPTX execution count = %d, want 0", executionCount)
	}

	task := &models.PPTXPDFTask{
		TenantID: tenantID, Name: "slides.pptx", Enabled: true,
		Config: commonModels.JSONMap{"source": commonModels.JSONMap{
			"source_engine_id": uint(12), "item_id": uint(77), "item_fingerprint": fmt.Sprintf("pptx-%d", tenantID),
			"item_locator": "addp://engine/12/path/docs/slides.pptx?type=file&item_id=77", "format": "pptx",
		}},
	}
	if err := NewPPTXPDFRepository(db).CreateTask(context.Background(), task); err != nil {
		t.Fatalf("create unified PPTX task: %v", err)
	}
	if task.TaskType != commonExecution.TaskTypePPTXPDFGeneration || task.SemanticKey == "" {
		t.Fatalf("unified PPTX task identity = type %q semantic %q", task.TaskType, task.SemanticKey)
	}
	var bindingCount int64
	if err := db.Model(&models.TaskResourceBinding{}).Where("tenant_id = ? AND task_definition_id = ?", tenantID, task.ID).Count(&bindingCount).Error; err != nil {
		t.Fatalf("count PPTX source binding: %v", err)
	}
	if bindingCount != 1 {
		t.Fatalf("PPTX source binding count = %d, want 1", bindingCount)
	}
}

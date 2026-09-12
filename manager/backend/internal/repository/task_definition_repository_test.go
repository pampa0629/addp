package repository

import (
	"context"
	"errors"
	"testing"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDeleteTaskDefinitionRemovesResourceBindings(t *testing.T) {
	db := newTaskDefinitionRepositoryTestDB(t)

	repo := NewVectorTileSetRepository(db)
	task := &models.VectorTileSetTask{
		TenantID: 7,
		Name:     "roads",
		Enabled:  true,
		Config: commonModels.JSONMap{
			"source": map[string]interface{}{"source_engine_id": float64(11), "locator": "addp://engine/11/path/public/roads", "item_id": float64(91)},
			"target": map[string]interface{}{"engine_id": float64(12), "storage_locator": "addp://engine/12/path/tiles"},
		},
	}
	if err := repo.CreateTask(context.Background(), task); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if task.BindingStatus != models.TaskBindingStatusActive || task.BindingIssue != "" {
		t.Fatalf("created task binding state = %q/%q", task.BindingStatus, task.BindingIssue)
	}
	task.Name = "roads rebound"
	task.BindingStatus = models.TaskBindingStatusMissing
	task.BindingIssue = models.TaskBindingIssueMissingEngine
	if err := repo.UpdateTask(context.Background(), task); err != nil {
		t.Fatalf("UpdateTask() error = %v", err)
	}
	var rebound models.TaskDefinition
	if err := db.Where("id = ?", task.ID).Take(&rebound).Error; err != nil {
		t.Fatalf("load rebound task: %v", err)
	}
	if rebound.BindingStatus != models.TaskBindingStatusActive || rebound.BindingIssue != "" {
		t.Fatalf("rebound task binding state = %q/%q", rebound.BindingStatus, rebound.BindingIssue)
	}
	if err := repo.DeleteTask(context.Background(), task.ID, task.TenantID); err != nil {
		t.Fatalf("DeleteTask() error = %v", err)
	}
	var bindingCount int64
	if err := db.Table("manager.task_resource_bindings").Where("task_definition_id = ?", task.ID).Count(&bindingCount).Error; err != nil {
		t.Fatalf("count bindings: %v", err)
	}
	if bindingCount != 0 {
		t.Fatalf("binding count = %d, want 0", bindingCount)
	}
}

func TestRetireMissingTaskKeepsCanonicalReplacement(t *testing.T) {
	db := newTaskDefinitionRepositoryTestDB(t)
	repo := NewTaskDefinitionRepository(db)
	ctx := context.Background()
	taskType := commonExecution.TaskTypeModel3DGLBGeneration

	missing := &models.TaskDefinition{
		TenantID: 7, Name: "missing", Enabled: false,
		Config: commonModels.JSONMap{"source": commonModels.JSONMap{
			"source_engine_id": uint(11), "item_locator": "addp://engine/11/path/model.ifc", "item_fingerprint": "old",
		}},
	}
	if err := createTaskDefinition(ctx, db, taskType, missing); err != nil {
		t.Fatalf("create missing task: %v", err)
	}
	if err := db.Model(&models.TaskDefinition{}).Where("id = ?", missing.ID).Updates(map[string]interface{}{
		"binding_status": models.TaskBindingStatusMissing,
		"binding_issue":  models.TaskBindingIssueMissingEngine,
	}).Error; err != nil {
		t.Fatalf("mark missing task: %v", err)
	}

	replacement := &models.TaskDefinition{
		TenantID: 7, Name: "replacement", Enabled: true,
		Config: commonModels.JSONMap{"source": commonModels.JSONMap{
			"source_engine_id": uint(12), "item_locator": "addp://engine/12/path/model.ifc", "item_fingerprint": "new",
		}},
	}
	if err := createTaskDefinition(ctx, db, taskType, replacement); err != nil {
		t.Fatalf("create replacement task: %v", err)
	}

	if err := repo.RetireMissingTask(ctx, 7, taskType, missing.ID, missing.Version+1, replacement.ID); !errors.Is(err, ErrTaskDefinitionVersionConflict) {
		t.Fatalf("stale version error = %v, want version conflict", err)
	}
	if err := repo.RetireMissingTask(ctx, 7, taskType, missing.ID, missing.Version, replacement.ID); err != nil {
		t.Fatalf("retire missing task: %v", err)
	}
	if task, err := repo.Get(ctx, 7, taskType, missing.ID); err != nil || task != nil {
		t.Fatalf("retired task = %#v, err = %v", task, err)
	}
	if task, err := repo.Get(ctx, 7, taskType, replacement.ID); err != nil || task == nil {
		t.Fatalf("replacement task = %#v, err = %v", task, err)
	}
	var bindingCount int64
	if err := db.Table("manager.task_resource_bindings").Where("task_definition_id = ?", missing.ID).Count(&bindingCount).Error; err != nil {
		t.Fatalf("count retired bindings: %v", err)
	}
	if bindingCount != 0 {
		t.Fatalf("retired binding count = %d, want 0", bindingCount)
	}
}

func TestCurrentResultStatusesUsesLatestNonDeletedTenantResult(t *testing.T) {
	db := newTaskDefinitionRepositoryTestDB(t)
	if err := db.Exec(`CREATE TABLE manager.model_3d_glb (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		task_id INTEGER,
		status TEXT NOT NULL,
		updated_at DATETIME,
		deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create model_3d_glb: %v", err)
	}

	quickViewTask := &models.TaskDefinition{ID: 41, TenantID: 7, TaskType: commonExecution.TaskTypeModel3DGLBGeneration}
	spatialTask := &models.TaskDefinition{ID: 42, TenantID: 7, TaskType: commonExecution.TaskTypeRasterMosaicGeneration}
	statements := []string{
		`INSERT INTO manager.model_3d_glb (tenant_id, task_id, status, updated_at) VALUES (7, 41, 'ready', '2026-01-01T00:00:00Z')`,
		`INSERT INTO manager.model_3d_glb (tenant_id, task_id, status, updated_at) VALUES (7, 41, 'failed', '2026-01-02T00:00:00Z')`,
		`INSERT INTO manager.model_3d_glb (tenant_id, task_id, status, updated_at, deleted_at) VALUES (7, 41, 'ready', '2026-01-03T00:00:00Z', '2026-01-03T00:00:01Z')`,
		`INSERT INTO manager.model_3d_glb (tenant_id, task_id, status, updated_at) VALUES (8, 41, 'ready', '2026-01-04T00:00:00Z')`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("insert result status: %v", err)
		}
	}

	repo := NewTaskDefinitionRepository(db)
	statuses, err := repo.CurrentResultStatuses(context.Background(), 7, []*models.TaskDefinition{quickViewTask, spatialTask})
	if err != nil {
		t.Fatalf("CurrentResultStatuses() error = %v", err)
	}
	if got := statuses[quickViewTask.ID]; got != "failed" {
		t.Fatalf("latest status = %q, want failed", got)
	}
	if _, exists := statuses[spatialTask.ID]; exists {
		t.Fatalf("spatial task unexpectedly has Manager current result: %#v", statuses)
	}
	status, exists, err := repo.CurrentResultStatus(context.Background(), 7, quickViewTask.TaskType, quickViewTask.ID)
	if err != nil || !exists || status != "failed" {
		t.Fatalf("CurrentResultStatus() = %q, %v, %v; want failed, true, nil", status, exists, err)
	}
}

func newTaskDefinitionRepositoryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec(`ATTACH DATABASE ':memory:' AS manager`).Error; err != nil {
		t.Fatalf("attach manager schema: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.task_definitions (
		id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, task_type TEXT NOT NULL,
		version INTEGER NOT NULL DEFAULT 1, name TEXT NOT NULL, description TEXT, enabled BOOLEAN NOT NULL,
		schedule TEXT, next_run_at DATETIME, last_run_at DATETIME, last_execution_id TEXT,
		last_execution_status TEXT, binding_status TEXT NOT NULL DEFAULT 'active', binding_issue TEXT NOT NULL DEFAULT '',
		semantic_key TEXT, config JSON NOT NULL, created_by INTEGER,
		created_at DATETIME, updated_at DATETIME, deleted_at DATETIME)`).Error; err != nil {
		t.Fatalf("create task_definitions: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.task_resource_bindings (
		id INTEGER PRIMARY KEY AUTOINCREMENT, task_definition_id INTEGER NOT NULL, tenant_id INTEGER NOT NULL,
		role TEXT NOT NULL, engine_id INTEGER NOT NULL, locator TEXT NOT NULL, item_id INTEGER,
		item_fingerprint TEXT, ordinal INTEGER NOT NULL, created_at DATETIME)`).Error; err != nil {
		t.Fatalf("create task_resource_bindings: %v", err)
	}
	return db
}

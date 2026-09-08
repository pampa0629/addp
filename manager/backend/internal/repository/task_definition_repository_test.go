package repository

import (
	"context"
	"testing"

	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDeleteTaskDefinitionRemovesResourceBindings(t *testing.T) {
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
		last_execution_status TEXT, semantic_key TEXT, config JSON NOT NULL, created_by INTEGER,
		created_at DATETIME, updated_at DATETIME, deleted_at DATETIME)`).Error; err != nil {
		t.Fatalf("create task_definitions: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.task_resource_bindings (
		id INTEGER PRIMARY KEY AUTOINCREMENT, task_definition_id INTEGER NOT NULL, tenant_id INTEGER NOT NULL,
		role TEXT NOT NULL, engine_id INTEGER NOT NULL, locator TEXT NOT NULL, item_id INTEGER,
		item_fingerprint TEXT, ordinal INTEGER NOT NULL, created_at DATETIME)`).Error; err != nil {
		t.Fatalf("create task_resource_bindings: %v", err)
	}

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

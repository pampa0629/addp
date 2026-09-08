package repository

import (
	"context"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCleanupTaskDefinitionRepositoryListsDisabledAndHardDeletes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec(`ATTACH DATABASE ':memory:' AS manager`).Error; err != nil {
		t.Fatalf("attach manager schema: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.task_definitions (
		id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, task_type TEXT NOT NULL,
		enabled BOOLEAN NOT NULL, next_run_at DATETIME, last_execution_status TEXT, config JSON NOT NULL,
		updated_at DATETIME, deleted_at DATETIME)`).Error; err != nil {
		t.Fatalf("create task_definitions: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.task_resource_bindings (
		id INTEGER PRIMARY KEY AUTOINCREMENT, task_definition_id INTEGER NOT NULL, tenant_id INTEGER NOT NULL,
		role TEXT NOT NULL, engine_id INTEGER NOT NULL, locator TEXT NOT NULL, item_id INTEGER,
		item_fingerprint TEXT, ordinal INTEGER NOT NULL, created_at DATETIME, updated_at DATETIME)`).Error; err != nil {
		t.Fatalf("create task_resource_bindings: %v", err)
	}
	if err := db.Exec(`
		INSERT INTO manager.task_definitions (tenant_id, task_type, enabled, config)
		VALUES (7, 'model_3d_glb_generation', FALSE, '{}');
		INSERT INTO manager.task_definitions (tenant_id, task_type, enabled, config)
		VALUES (7, 'point_cloud_copc_generation', TRUE, '{}');
		INSERT INTO manager.task_resource_bindings (task_definition_id, tenant_id, role, engine_id, locator, ordinal)
		VALUES (1, 7, 'source', 26, 'addp://engine/26/path/model.glb?type=file', 0);
		INSERT INTO manager.task_resource_bindings (task_definition_id, tenant_id, role, engine_id, locator, ordinal)
		VALUES (2, 7, 'source', 26, 'addp://engine/26/path/cloud.laz?type=file', 0)
	`).Error; err != nil {
		t.Fatalf("insert tasks: %v", err)
	}

	repo := NewCleanupTaskDefinitionRepository(db, []CleanupTaskDefinitionSpec{
		{TaskType: "model_3d_glb_generation", Table: "manager.task_definitions"},
		{TaskType: "point_cloud_copc_generation", Table: "manager.task_definitions"},
	})
	definitions, err := repo.List(context.Background(), 7)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(definitions) != 2 || definitions[0].Enabled || !definitions[1].Enabled {
		t.Fatalf("definitions = %#v", definitions)
	}
	if err := repo.HardDelete(context.Background(), definitions[0]); err != nil {
		t.Fatalf("HardDelete() error = %v", err)
	}
	definitions, err = repo.List(context.Background(), 7)
	if err != nil {
		t.Fatalf("List() after delete error = %v", err)
	}
	if len(definitions) != 1 || definitions[0].TaskType != "point_cloud_copc_generation" {
		t.Fatalf("definitions after delete = %#v", definitions)
	}
	var bindingCount int64
	if err := db.Table("manager.task_resource_bindings").Where("task_definition_id = ?", 1).Count(&bindingCount).Error; err != nil || bindingCount != 0 {
		t.Fatalf("deleted task bindings = %d, error = %v", bindingCount, err)
	}
}

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
	for _, table := range []string{"model_3d_glb_tasks", "point_cloud_copc_tasks"} {
		if err := db.Exec(`CREATE TABLE manager."` + table + `" (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			enabled BOOLEAN NOT NULL,
			next_run_at DATETIME,
			last_execution_status TEXT,
			config JSON NOT NULL,
			updated_at DATETIME,
			deleted_at DATETIME
		)`).Error; err != nil {
			t.Fatalf("create %s: %v", table, err)
		}
	}
	if err := db.Exec(`
		INSERT INTO manager.model_3d_glb_tasks (tenant_id, enabled, config)
		VALUES (7, FALSE, '{"source":{"source_engine_id":26,"item_locator":"addp://engine/26/path/model.glb?type=file"}}');
		INSERT INTO manager.point_cloud_copc_tasks (tenant_id, enabled, config)
		VALUES (7, TRUE, '{"source":{"source_engine_id":26,"item_locator":"addp://engine/26/path/cloud.laz?type=file"}}')
	`).Error; err != nil {
		t.Fatalf("insert tasks: %v", err)
	}

	repo := NewCleanupTaskDefinitionRepository(db, []CleanupTaskDefinitionSpec{
		{TaskType: "model_3d_glb_generation", Table: "manager.model_3d_glb_tasks"},
		{TaskType: "point_cloud_copc_generation", Table: "manager.point_cloud_copc_tasks"},
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
}

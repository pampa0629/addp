package service

import (
	"errors"
	"testing"

	"github.com/addp/develop/backend/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRebindNotebookRuntimeUpdatesOnlyCurrentTaskBinding(t *testing.T) {
	db := newNotebookBindingTestDB(t)
	if err := db.Exec(`
		INSERT INTO develop.dev_tasks (
			id, tenant_id, name, display_name, dev_type, content, execution_config,
			editor_layout, timeout, description, tags, created_by, status
		) VALUES (
			14, 7, 'analysis', 'Analysis', 'script',
			CAST('{"notebook_path":"analysis.ipynb","minio_path":"tenant_7/notebooks/analysis.ipynb","kernel":"old-kernel","parameters":{"limit":10}}' AS BLOB),
			CAST('{"engine_id":8,"retry":2}' AS BLOB), CAST('{}' AS BLOB), 600, 'kept', '{}', 1, 'active'
		)
	`).Error; err != nil {
		t.Fatalf("seed notebook: %v", err)
	}

	service := NewDevTaskService(repository.NewDevTaskRepository(db))
	updated, err := service.RebindNotebookRuntime(14, 7, 3, 10, "python3")
	if err != nil {
		t.Fatalf("RebindNotebookRuntime() error = %v", err)
	}
	if updated.ID != 14 {
		t.Fatalf("task id = %d, want 14", updated.ID)
	}
	if got := updated.GetEngineID(); got == nil || *got != 10 {
		t.Fatalf("engine_id = %v, want 10; value = %#v (%T)", got, updated.ExecutionConfig["engine_id"], updated.ExecutionConfig["engine_id"])
	}
	if got := updated.Content["kernel"]; got != "python3" {
		t.Fatalf("kernel = %#v, want python3", got)
	}
	if got := updated.Content["minio_path"]; got != "tenant_7/notebooks/analysis.ipynb" {
		t.Fatalf("minio_path changed: %#v", got)
	}
	if got := updated.ExecutionConfig["retry"]; got != float64(2) {
		t.Fatalf("execution_config.retry changed: %#v", got)
	}
	if updated.UpdatedBy == nil || *updated.UpdatedBy != 3 {
		t.Fatalf("updated_by = %v, want 3", updated.UpdatedBy)
	}
	stored, err := service.GetDevTask(14, 7)
	if err != nil {
		t.Fatalf("GetDevTask() after rebind error = %v", err)
	}
	if got := stored.GetEngineID(); got == nil || *got != 10 {
		t.Fatalf("stored engine_id = %v, want 10", got)
	}
	if got := stored.Content["kernel"]; got != "python3" {
		t.Fatalf("stored kernel = %#v, want python3", got)
	}
}

func TestRebindNotebookRuntimeRejectsNonNotebookTask(t *testing.T) {
	db := newNotebookBindingTestDB(t)
	if err := db.Exec(`
		INSERT INTO develop.dev_tasks (
			id, tenant_id, name, dev_type, content, execution_config, editor_layout,
			timeout, tags, status
		) VALUES (
			15, 7, 'query', 'query', CAST('{"query":"SELECT 1","query_type":"sql"}' AS BLOB),
			CAST('{"engine_id":8}' AS BLOB), CAST('{}' AS BLOB), 300, '{}', 'active'
		)
	`).Error; err != nil {
		t.Fatalf("seed query: %v", err)
	}

	service := NewDevTaskService(repository.NewDevTaskRepository(db))
	_, err := service.RebindNotebookRuntime(15, 7, 3, 10, "python3")
	if !errors.Is(err, ErrTaskNotNotebook) {
		t.Fatalf("error = %v, want ErrTaskNotNotebook", err)
	}
}

func newNotebookBindingTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS develop").Error; err != nil {
		t.Fatalf("attach develop schema: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE develop.dev_tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL,
			name TEXT NOT NULL, display_name TEXT, dev_type TEXT NOT NULL,
			content JSON NOT NULL, execution_config JSON, editor_layout JSON NOT NULL,
			timeout INTEGER, description TEXT, tags TEXT, created_by INTEGER,
			updated_by INTEGER, created_at DATETIME, updated_at DATETIME,
			deleted_at DATETIME, status TEXT, last_execution_id TEXT,
			last_execution_status TEXT, last_run_at DATETIME
		)
	`).Error; err != nil {
		t.Fatalf("create develop.dev_tasks: %v", err)
	}
	return db
}

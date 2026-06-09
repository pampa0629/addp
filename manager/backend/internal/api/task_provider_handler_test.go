package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestTaskProviderListTasksRejectsUnsupportedTaskType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewTaskProviderHandler(nil, nil, nil)
	router.GET("/tasks", handler.ListTasks)

	req := httptest.NewRequest(http.MethodGet, "/tasks?task_type=unknown", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestManagerPrivateTaskListsUseFixedTaskType(t *testing.T) {
	db := newTaskProviderHandlerTestDB(t)
	mvtRepo := repository.NewMvtTaskRepository(db)
	embeddingRepo := repository.NewEmbeddingRepository(db)

	if err := mvtRepo.Create(context.Background(), &models.MvtTask{
		TenantID:   1,
		Name:       "mvt task",
		Enabled:    true,
		EngineID:   11,
		SchemaName: "public",
		Table:      "roads",
	}); err != nil {
		t.Fatalf("create mvt task: %v", err)
	}
	if err := embeddingRepo.CreateEmbeddingTask(context.Background(), &models.EmbeddingTask{
		TenantID: 1,
		Name:     "embedding task",
		Enabled:  true,
		EngineID: 12,
		Bucket:   "datasets",
		Prefix:   "docs/",
	}); err != nil {
		t.Fatalf("create embedding task: %v", err)
	}

	handler := NewTaskProviderHandler(
		service.NewEmbeddingTaskService(embeddingRepo, nil, nil),
		service.NewMvtTaskService(mvtRepo, nil, nil),
		nil,
	)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("tenant_id", uint(1))
		c.Next()
	})
	router.GET("/mvt_tasks", handler.ListMvtTasks)
	router.GET("/embedding_tasks", handler.ListEmbeddingTasks)

	assertTaskTypes(t, router, "/mvt_tasks", []string{commonExecution.TaskTypeMvtGeneration})
	assertTaskTypes(t, router, "/embedding_tasks", []string{commonExecution.TaskTypeEmbedding})
}

func assertTaskTypes(t *testing.T, router *gin.Engine, path string, want []string) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("%s status = %d, want %d, body=%s", path, w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Data []struct {
			TaskType string `json:"task_type"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("%s decode response: %v; body=%s", path, err, w.Body.String())
	}
	if len(resp.Data) != len(want) {
		t.Fatalf("%s data len = %d, want %d; body=%s", path, len(resp.Data), len(want), w.Body.String())
	}
	for i, item := range resp.Data {
		if item.TaskType != want[i] {
			t.Fatalf("%s data[%d].task_type = %s, want %s; body=%s", path, i, item.TaskType, want[i], w.Body.String())
		}
	}
}

func newTaskProviderHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	if err := db.Exec("ATTACH DATABASE ':memory:' AS manager").Error; err != nil {
		t.Fatalf("attach manager schema: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.mvt_tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		description TEXT,
		enabled BOOLEAN,
		last_execution_id TEXT,
		last_execution_status TEXT,
		last_run_at DATETIME,
		created_by INTEGER,
		engine_id INTEGER NOT NULL,
		schema_name TEXT NOT NULL,
		table_name TEXT NOT NULL,
		min_zoom INTEGER,
		max_zoom INTEGER,
		optimization_config JSON,
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create mvt_tasks table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.embedding_tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		description TEXT,
		enabled BOOLEAN,
		last_execution_id TEXT,
		last_execution_status TEXT,
		last_run_at DATETIME,
		created_by INTEGER,
		engine_id INTEGER NOT NULL,
		bucket TEXT NOT NULL,
		prefix TEXT,
		recursive BOOLEAN,
		modality TEXT,
		file_types TEXT,
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create embedding_tasks table: %v", err)
	}
	return db
}

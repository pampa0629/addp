package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
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
	handler := NewTaskProviderHandler(nil, nil, nil, nil)
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
	tileCacheRepo := repository.NewTileCacheRepository(db)
	embeddingRepo := repository.NewEmbeddingRepository(db)
	qvoRepo := repository.NewQuickViewOptimizationRepository(db)

	if err := tileCacheRepo.CreateTask(context.Background(), &models.TileCacheTask{
		TenantID: 1,
		Name:     "tile cache task",
		Enabled:  true,
		Config: commonModels.JSONMap{
			"target": commonModels.JSONMap{
				"item_id":          "99",
				"source_engine_id": 11,
				"locator":          "postgresql://11/public/roads",
			},
			"tile": commonModels.JSONMap{
				"format":   "mvt",
				"min_zoom": 0,
				"max_zoom": 12,
			},
		},
	}); err != nil {
		t.Fatalf("create tile cache task: %v", err)
	}
	if err := embeddingRepo.CreateEmbeddingTask(context.Background(), &models.EmbeddingTask{
		TenantID: 1,
		Name:     "embedding task",
		Enabled:  true,
		Config: commonModels.JSONMap{
			"target": commonModels.JSONMap{
				"scope":     "node",
				"engine_id": 12,
				"node_id":   34,
			},
		},
	}); err != nil {
		t.Fatalf("create embedding task: %v", err)
	}
	if err := qvoRepo.CreateTask(context.Background(), &models.QuickViewOptimizationTask{
		TenantID: 1,
		Name:     "quick view optimization task",
		Enabled:  true,
		Config: commonModels.JSONMap{
			"target": commonModels.JSONMap{
				"source_engine_id": 11,
				"schema":           "public",
				"table":            "roads",
			},
			"geometry": commonModels.JSONMap{
				"geometry_column": "shape",
				"source_srid":     4326,
				"target_srid":     3857,
			},
		},
	}); err != nil {
		t.Fatalf("create quick view optimization task: %v", err)
	}

	handler := NewTaskProviderHandler(
		service.NewEmbeddingTaskService(embeddingRepo, nil, nil, nil),
		service.NewTileCacheTaskService(tileCacheRepo, nil),
		service.NewQuickViewOptimizationTaskService(qvoRepo, nil),
		nil,
	)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("tenant_id", uint(1))
		c.Next()
	})
	router.GET("/tile_cache_tasks", handler.ListTileCacheTasks)
	router.GET("/embedding_tasks", handler.ListEmbeddingTasks)
	router.GET("/quick_view_optimization_tasks", handler.ListQuickViewOptimizationTasks)

	assertTaskTypes(t, router, "/tile_cache_tasks", []string{commonExecution.TaskTypeTileCacheGeneration})
	assertTaskTypes(t, router, "/embedding_tasks", []string{commonExecution.TaskTypeEmbedding})
	assertTaskTypes(t, router, "/quick_view_optimization_tasks", []string{commonExecution.TaskTypeQuickViewOptimization})
}

func TestCreateEmbeddingTaskRejectsLegacyTopLevelFields(t *testing.T) {
	db := newTaskProviderHandlerTestDB(t)
	embeddingRepo := repository.NewEmbeddingRepository(db)
	handler := NewTaskProviderHandler(
		service.NewEmbeddingTaskService(embeddingRepo, nil, nil, nil),
		nil,
		nil,
		nil,
	)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("tenant_id", uint(1))
		c.Set("user_id", uint(9))
		c.Next()
	})
	router.POST("/embedding_tasks", handler.CreateEmbeddingTask)

	body := `{
		"name":"legacy",
		"engine_id":12,
		"config":{
			"target":{"scope":"node","engine_id":12,"node_id":34}
		}
	}`
	req := httptest.NewRequest(http.MethodPost, "/embedding_tasks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "unknown field") {
		t.Fatalf("body = %s, want unknown field error", w.Body.String())
	}
}

func TestTaskExecuteRejectsUnknownFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewTaskProviderHandler(nil, nil, nil, nil)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("tenant_id", uint(1))
		c.Next()
	})
	router.POST("/tasks/:task_type/:id/execute", handler.TaskExecute)

	body := `{"parameters":{},"legacy":true}`
	req := httptest.NewRequest(http.MethodPost, "/tasks/embedding/1/execute", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "unknown field") {
		t.Fatalf("body = %s, want unknown field error", w.Body.String())
	}
}

func TestDecodeEmbeddingExecutionRequestRejectsUnknownFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/embedding_executions", strings.NewReader(`{
		"scope":"item",
		"target":{"item_id":7},
		"bucket":"legacy"
	}`))

	_, err := decodeEmbeddingExecutionRequest(c)
	if err == nil {
		t.Fatal("decodeEmbeddingExecutionRequest error is nil, want unknown field error")
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("error = %v, want unknown field error", err)
	}
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
	if err := db.Exec(`CREATE TABLE manager.tile_cache_tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			description TEXT,
		enabled BOOLEAN,
		last_execution_id TEXT,
			last_execution_status TEXT,
			last_run_at DATETIME,
			next_run_at DATETIME,
			schedule TEXT,
			created_by INTEGER,
			config JSON,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)`).Error; err != nil {
		t.Fatalf("create tile_cache_tasks table: %v", err)
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
		next_run_at DATETIME,
		schedule TEXT,
		created_by INTEGER,
		config JSON,
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create embedding_tasks table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.quick_view_optimization_tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		description TEXT,
		enabled BOOLEAN,
		last_execution_id TEXT,
		last_execution_status TEXT,
		last_run_at DATETIME,
		next_run_at DATETIME,
		schedule TEXT,
		created_by INTEGER,
		config JSON,
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create quick_view_optimization_tasks table: %v", err)
	}
	return db
}

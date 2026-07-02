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
	handler := NewTaskProviderHandler(nil, nil, nil, nil, nil)
	router.GET("/tasks", handler.ListTasks)

	req := httptest.NewRequest(http.MethodGet, "/tasks?task_type=unknown", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	assertStandardErrorResponse(t, w.Body.Bytes())
}

func TestTaskProviderListTasksUsesStandardItemsShape(t *testing.T) {
	db := newTaskProviderHandlerTestDB(t)
	tileCacheRepo := repository.NewTileCacheRepository(db)
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

	handler := NewTaskProviderHandler(
		nil,
		service.NewTileCacheTaskService(tileCacheRepo, nil),
		nil,
		nil,
		nil,
	)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("tenant_id", uint(1))
		c.Next()
	})
	router.GET("/tasks", handler.ListTasks)

	req := httptest.NewRequest(http.MethodGet, "/tasks?task_type="+commonExecution.TaskTypeVectorTileCacheGeneration, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var resp struct {
		Items []struct {
			TaskType string `json:"task_type"`
		} `json:"items"`
		Data []struct {
			TaskType string `json:"task_type"`
		} `json:"data"`
		Total int64 `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, w.Body.String())
	}
	if len(resp.Items) != 1 || resp.Items[0].TaskType != commonExecution.TaskTypeVectorTileCacheGeneration {
		t.Fatalf("items = %#v, want one tile cache task; body=%s", resp.Items, w.Body.String())
	}
	if resp.Data != nil {
		t.Fatalf("data = %#v, want omitted in TaskProvider standard response; body=%s", resp.Data, w.Body.String())
	}
}

func TestTaskProviderTaskDetailUsesDirectObjectShape(t *testing.T) {
	db := newTaskProviderHandlerTestDB(t)
	tileCacheRepo := repository.NewTileCacheRepository(db)
	task := &models.TileCacheTask{
		TenantID: 1,
		Name:     "tile cache detail",
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
	}
	if err := tileCacheRepo.CreateTask(context.Background(), task); err != nil {
		t.Fatalf("create tile cache task: %v", err)
	}

	handler := NewTaskProviderHandler(
		nil,
		service.NewTileCacheTaskService(tileCacheRepo, nil),
		nil,
		nil,
		nil,
	)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("tenant_id", uint(1))
		c.Next()
	})
	router.GET("/tasks/:task_type/:id", handler.TaskDetail)

	req := httptest.NewRequest(http.MethodGet, "/tasks/"+commonExecution.TaskTypeVectorTileCacheGeneration+"/1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var resp struct {
		ID       uint   `json:"id"`
		TaskType string `json:"task_type"`
		Status   string `json:"status"`
		Data     any    `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, w.Body.String())
	}
	if resp.ID != task.ID || resp.TaskType != commonExecution.TaskTypeVectorTileCacheGeneration {
		t.Fatalf("response = %#v, want direct tile cache task object; body=%s", resp, w.Body.String())
	}
	if resp.Status != "" || resp.Data != nil {
		t.Fatalf("response wraps standard task detail, status=%q data=%#v body=%s", resp.Status, resp.Data, w.Body.String())
	}
}

func TestTaskProviderExecutionStatusUsesDirectObjectShape(t *testing.T) {
	db := newTaskProviderHandlerTestDB(t)
	exec := commonExecution.TaskExecution{
		TenantID:    1,
		ExecutionID: "manager-exec-1",
		Module:      commonExecution.ModuleManager,
		TaskType:    commonExecution.TaskTypeEmbedding,
		Source:      commonExecution.ModuleManager,
		Status:      commonExecution.ExecutionStatusRunning,
		TriggerType: commonExecution.TriggerTypeManual,
	}
	if err := db.Create(&exec).Error; err != nil {
		t.Fatalf("create task execution: %v", err)
	}

	handler := NewTaskProviderHandler(nil, nil, nil, nil, commonExecution.NewTaskExecutionRepository(db))
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("tenant_id", uint(1))
		c.Next()
	})
	router.GET("/executions/:execution_id", handler.ExecutionStatus)

	req := httptest.NewRequest(http.MethodGet, "/executions/manager-exec-1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var resp struct {
		ExecutionID string `json:"execution_id"`
		Status      string `json:"status"`
		Data        any    `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, w.Body.String())
	}
	if resp.ExecutionID != "manager-exec-1" || resp.Status != commonExecution.ExecutionStatusRunning {
		t.Fatalf("response = %#v, want direct execution object; body=%s", resp, w.Body.String())
	}
	if resp.Data != nil {
		t.Fatalf("response wraps standard execution status, data=%#v body=%s", resp.Data, w.Body.String())
	}
}

func TestManagerPrivateTaskListsUseFixedTaskType(t *testing.T) {
	db := newTaskProviderHandlerTestDB(t)
	tileCacheRepo := repository.NewTileCacheRepository(db)
	embeddingRepo := repository.NewEmbeddingRepository(db)
	qvoRepo := repository.NewVectorMaterializedViewRepository(db)
	cogRepo := repository.NewRasterCOGRepository(db)
	model3DTilesRepo := repository.NewModel3DTilesRepository(db)
	model3DGLBRepo := repository.NewModel3DGLBRepository(db)
	gaussianSplatKSplatRepo := repository.NewGaussianSplatKSplatRepository(db)

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
	if err := qvoRepo.CreateTask(context.Background(), &models.VectorMaterializedViewTask{
		TenantID: 1,
		Name:     "vector materialized view task",
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
		t.Fatalf("create vector materialized view task: %v", err)
	}
	if err := cogRepo.CreateTask(context.Background(), &models.RasterCOGTask{
		TenantID: 1,
		Name:     "raster COG generation task",
		Enabled:  true,
		Config: commonModels.JSONMap{
			"target": commonModels.JSONMap{
				"source_engine_id": 11,
				"locator":          "addp://engine/11/path/rasters/large.tif?type=file&item_id=44",
			},
		},
	}); err != nil {
		t.Fatalf("create raster COG generation task: %v", err)
	}
	if err := model3DTilesRepo.CreateTask(context.Background(), &models.Model3DTilesTask{
		TenantID: 1,
		Name:     "model 3d tiles generation task",
		Enabled:  true,
		Config: commonModels.JSONMap{
			"source": commonModels.JSONMap{
				"item_locator":     "addp://engine/11/path/models/osgb?type=item&item_id=45",
				"source_engine_id": uint(11),
				"format":           "osgb_scene",
			},
			"target": commonModels.JSONMap{
				"storage_locator":  "addp://engine/12/path/models/tiles?type=node",
				"target_engine_id": uint(12),
				"dataset_name":     "osgb_3dtiles",
			},
			"tiles": commonModels.JSONMap{
				"format": "3dtiles",
			},
		},
	}); err != nil {
		t.Fatalf("create model 3d tiles generation task: %v", err)
	}
	if err := model3DGLBRepo.CreateTask(context.Background(), &models.Model3DGLBTask{
		TenantID: 1,
		Name:     "model 3d GLB generation task",
		Enabled:  true,
		Config: commonModels.JSONMap{
			"source": commonModels.JSONMap{
				"item_locator":      "addp://engine/11/path/models/tile.osgb?type=file&item_id=46",
				"source_engine_id":  uint(11),
				"item_fingerprint":  "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
				"item_id":           uint(46),
				"format":            "osgb",
				"source_size_bytes": int64(1024),
			},
		},
	}); err != nil {
		t.Fatalf("create model 3d GLB generation task: %v", err)
	}
	if err := gaussianSplatKSplatRepo.CreateTask(context.Background(), &models.GaussianSplatKSplatTask{
		TenantID: 1,
		Name:     "gaussian splat KSplat generation task",
		Enabled:  true,
		Config: commonModels.JSONMap{
			"source": commonModels.JSONMap{
				"item_locator":      "addp://engine/11/path/models/model.ksplat?type=file&item_id=47",
				"source_engine_id":  uint(11),
				"item_fingerprint":  "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
				"item_id":           uint(47),
				"format":            "ksplat",
				"source_size_bytes": int64(1024),
			},
		},
	}); err != nil {
		t.Fatalf("create gaussian splat KSplat generation task: %v", err)
	}

	handler := NewTaskProviderHandler(
		service.NewEmbeddingTaskService(embeddingRepo, nil, nil, nil),
		service.NewTileCacheTaskService(tileCacheRepo, nil),
		service.NewVectorMaterializedViewTaskService(qvoRepo, nil),
		service.NewRasterCOGTaskService(cogRepo, nil),
		nil,
	)
	handler.SetModel3DTilesTaskService(service.NewModel3DTilesTaskService(model3DTilesRepo, nil))
	handler.SetModel3DGLBTaskService(service.NewModel3DGLBTaskService(model3DGLBRepo, nil))
	handler.SetGaussianSplatKSplatTaskService(service.NewGaussianSplatKSplatTaskService(gaussianSplatKSplatRepo, nil))

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("tenant_id", uint(1))
		c.Next()
	})
	router.GET("/vector_tile_cache_tasks", handler.ListTileCacheTasks)
	router.GET("/embedding_tasks", handler.ListEmbeddingTasks)
	router.GET("/vector_materialized_view_tasks", handler.ListVectorMaterializedViewTasks)
	router.GET("/raster_cog_tasks", handler.ListRasterCOGTasks)
	router.GET("/model_3d_tiles_tasks", handler.ListModel3DTilesTasks)
	router.GET("/model_3d_glb_tasks", handler.ListModel3DGLBTasks)
	router.GET("/gaussian_splat_ksplat_tasks", handler.ListGaussianSplatKSplatTasks)

	assertListedTaskTypeValues(t, router, "/vector_tile_cache_tasks", []string{commonExecution.TaskTypeVectorTileCacheGeneration})
	assertListedTaskTypeValues(t, router, "/embedding_tasks", []string{commonExecution.TaskTypeEmbedding})
	assertListedTaskTypeValues(t, router, "/vector_materialized_view_tasks", []string{commonExecution.TaskTypeVectorMaterializedViewGeneration})
	assertListedTaskTypeValues(t, router, "/raster_cog_tasks", []string{commonExecution.TaskTypeRasterCOGGeneration})
	assertListedTaskTypeValues(t, router, "/model_3d_tiles_tasks", []string{commonExecution.TaskTypeModel3DTilesGeneration})
	assertListedTaskTypeValues(t, router, "/model_3d_glb_tasks", []string{commonExecution.TaskTypeModel3DGLBGeneration})
	assertListedTaskTypeValues(t, router, "/gaussian_splat_ksplat_tasks", []string{commonExecution.TaskTypeGaussianSplatKSplatGeneration})
}

func TestCreateEmbeddingTaskRejectsLegacyTopLevelFields(t *testing.T) {
	db := newTaskProviderHandlerTestDB(t)
	embeddingRepo := repository.NewEmbeddingRepository(db)
	handler := NewTaskProviderHandler(
		service.NewEmbeddingTaskService(embeddingRepo, nil, nil, nil),
		nil,
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

func TestCreateEmbeddingTaskUsesDirectObjectShape(t *testing.T) {
	db := newTaskProviderHandlerTestDB(t)
	embeddingRepo := repository.NewEmbeddingRepository(db)
	handler := NewTaskProviderHandler(
		service.NewEmbeddingTaskService(embeddingRepo, nil, nil, nil),
		nil,
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
		"name":"embedding",
		"enabled":true,
		"config":{
			"target":{"scope":"node","engine_id":12,"node_id":34}
		}
	}`
	req := httptest.NewRequest(http.MethodPost, "/embedding_tasks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	var resp struct {
		ID       uint   `json:"id"`
		Name     string `json:"name"`
		TaskType string `json:"task_type"`
		Status   string `json:"status"`
		Data     any    `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, w.Body.String())
	}
	if resp.ID == 0 || resp.Name != "embedding" || resp.TaskType != commonExecution.TaskTypeEmbedding {
		t.Fatalf("response = %#v, want direct embedding task object; body=%s", resp, w.Body.String())
	}
	if resp.Status != "" || resp.Data != nil {
		t.Fatalf("response wraps embedding task, status=%q data=%#v body=%s", resp.Status, resp.Data, w.Body.String())
	}
}

func TestTaskExecuteRejectsUnknownFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewTaskProviderHandler(nil, nil, nil, nil, nil)

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
	assertStandardErrorResponse(t, w.Body.Bytes())
}

func TestTaskExecuteResponseUsesStandardExecutionShape(t *testing.T) {
	body, err := json.Marshal(TaskExecuteResponse{
		ExecutionID: "manager-exec-1",
		Status:      commonExecution.ExecutionStatusRunning,
	})
	if err != nil {
		t.Fatalf("marshal TaskExecuteResponse: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, body)
	}
	if resp["execution_id"] != "manager-exec-1" || resp["status"] != commonExecution.ExecutionStatusRunning {
		t.Fatalf("response = %#v, want execution_id and status", resp)
	}
	for _, legacyField := range []string{"message", "data", "id"} {
		if _, ok := resp[legacyField]; ok {
			t.Fatalf("response contains non-standard field %q: %s", legacyField, body)
		}
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

func assertListedTaskTypeValues(t *testing.T, router *gin.Engine, path string, want []string) {
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

func assertStandardErrorResponse(t *testing.T, body []byte) {
	t.Helper()

	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode error response: %v; body=%s", err, string(body))
	}
	if _, ok := resp["error"]; !ok {
		t.Fatalf("error response missing error field: %s", string(body))
	}
	if _, ok := resp["status"]; ok {
		t.Fatalf("error response contains legacy status field: %s", string(body))
	}
	if _, ok := resp["message"]; ok {
		t.Fatalf("error response contains legacy message field: %s", string(body))
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
	if err := db.Exec(`CREATE TABLE manager.vector_tile_cache_tasks (
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
		t.Fatalf("create vector_tile_cache_tasks table: %v", err)
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
	if err := db.Exec(`CREATE TABLE manager.vector_materialized_view_tasks (
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
		t.Fatalf("create vector_materialized_view_tasks table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.raster_cog_tasks (
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
		t.Fatalf("create raster_cog_tasks table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.model_3d_tiles_tasks (
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
		t.Fatalf("create model_3d_tiles_tasks table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.model_3d_glb_tasks (
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
		t.Fatalf("create model_3d_glb_tasks table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.gaussian_splat_ksplat_tasks (
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
		t.Fatalf("create gaussian_splat_ksplat_tasks table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.model_3d_glb (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		item_fingerprint TEXT NOT NULL,
		item_id INTEGER,
		locator TEXT,
		task_id INTEGER,
		last_execution_id TEXT,
		source_engine_id INTEGER NOT NULL,
		source_format TEXT NOT NULL,
		source_size_bytes INTEGER,
		storage_ref TEXT NOT NULL,
		file_name TEXT,
		size_bytes INTEGER,
		content_url TEXT,
		status TEXT NOT NULL,
		metadata JSON,
		error_message TEXT,
		created_by INTEGER,
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create model_3d_glb table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.gaussian_splat_ksplat (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		item_fingerprint TEXT NOT NULL,
		item_id INTEGER,
		locator TEXT,
		task_id INTEGER,
		last_execution_id TEXT,
		source_engine_id INTEGER NOT NULL,
		source_format TEXT NOT NULL,
		source_size_bytes INTEGER,
		storage_ref TEXT NOT NULL,
		file_name TEXT,
		size_bytes INTEGER,
		content_url TEXT,
		status TEXT NOT NULL,
		metadata JSON,
		error_message TEXT,
		created_by INTEGER,
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create gaussian_splat_ksplat table: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS common").Error; err != nil {
		t.Fatalf("attach common schema: %v", err)
	}
	if err := db.Exec(`CREATE TABLE common.task_executions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		execution_id TEXT NOT NULL,
		module TEXT NOT NULL,
		task_type TEXT NOT NULL,
		source TEXT NOT NULL DEFAULT '',
		source_task_id TEXT,
		source_task_name TEXT,
		parent_execution_id TEXT,
		status TEXT NOT NULL,
		progress INTEGER,
		current_step TEXT,
		trigger_type TEXT NOT NULL,
		triggered_by INTEGER,
		execution_config JSON,
		error_details JSON,
		metadata JSON,
		execution_time_ms INTEGER,
		rows_affected INTEGER,
		records_read INTEGER,
		records_written INTEGER,
		bytes_read INTEGER,
		bytes_written INTEGER,
		started_at DATETIME,
		completed_at DATETIME,
		created_at DATETIME,
		updated_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create task_executions table: %v", err)
	}
	return db
}

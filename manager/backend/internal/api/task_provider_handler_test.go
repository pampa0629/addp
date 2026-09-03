package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
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
		setTenantAuthContextForTest(c, 1, 1)
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
		setTenantAuthContextForTest(c, 1, 1)
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
		ID                uint   `json:"id"`
		TaskType          string `json:"task_type"`
		Status            string `json:"status"`
		Data              any    `json:"data"`
		ExecutionContract struct {
			InputSchema map[string]interface{} `json:"input_schema"`
		} `json:"execution_contract"`
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
	properties, _ := resp.ExecutionContract.InputSchema["properties"].(map[string]interface{})
	if _, ok := properties["existing_result_action"]; !ok {
		t.Fatalf("execution_contract does not expose existing_result_action: %s", w.Body.String())
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
		setTenantAuthContextForTest(c, 1, 1)
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
				"item_fingerprint": "fp-osgb-scene",
				"item_id":          uint(45),
				"format":           "osgb_scene",
			},
			"target_format": "3d_tiles",
			"result":        commonModels.JSONMap{},
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
		service.NewRasterCOGTaskService(cogRepo),
		nil,
	)
	handler.SetModel3DTilesTaskService(service.NewModel3DTilesTaskService(model3DTilesRepo))
	handler.SetModel3DGLBTaskService(service.NewModel3DGLBTaskService(model3DGLBRepo))
	handler.SetGaussianSplatKSplatTaskService(service.NewGaussianSplatKSplatTaskService(gaussianSplatKSplatRepo))

	router := gin.New()
	router.Use(func(c *gin.Context) {
		setTenantAuthContextForTest(c, 1, 1)
		c.Next()
	})
	router.GET("/vector_tile_cache_tasks", handler.ListTileCacheTasks)
	router.GET("/embedding_tasks", handler.ListEmbeddingTasks)
	router.GET("/vector_materialized_view_tasks", handler.ListVectorMaterializedViewTasks)
	router.GET("/raster_cog_tasks", handler.ListRasterCOGTasks)
	router.GET("/model3d_tiles_tasks", handler.ListModel3DTilesTasks)
	router.GET("/model_3d_glb_tasks", handler.ListModel3DGLBTasks)
	router.GET("/gaussian_splat_ksplat_tasks", handler.ListGaussianSplatKSplatTasks)

	assertListedTaskTypeValues(t, router, "/vector_tile_cache_tasks", []string{commonExecution.TaskTypeVectorTileCacheGeneration})
	assertListedTaskTypeValues(t, router, "/embedding_tasks", []string{commonExecution.TaskTypeEmbedding})
	assertListedTaskTypeValues(t, router, "/vector_materialized_view_tasks", []string{commonExecution.TaskTypeVectorMaterializedViewGeneration})
	assertListedTaskTypeValues(t, router, "/raster_cog_tasks", []string{commonExecution.TaskTypeRasterCOGGeneration})
	assertListedTaskTypeValues(t, router, "/model3d_tiles_tasks", []string{commonExecution.TaskTypeModel3DTilesGeneration})
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
		setTenantAuthContextForTest(c, 1, 9)
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
		setTenantAuthContextForTest(c, 1, 9)
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
		setTenantAuthContextForTest(c, 1, 1)
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
		Status:      commonExecution.ExecutionStatusPending,
	})
	if err != nil {
		t.Fatalf("marshal TaskExecuteResponse: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, body)
	}
	if resp["execution_id"] != "manager-exec-1" || resp["status"] != commonExecution.ExecutionStatusPending {
		t.Fatalf("response = %#v, want execution_id and status", resp)
	}
	for _, legacyField := range []string{"message", "data", "id"} {
		if _, ok := resp[legacyField]; ok {
			t.Fatalf("response contains non-standard field %q: %s", legacyField, body)
		}
	}
}

func TestTaskExecuteModel3DTilesRequiresConfirmationForExistingResult(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newTaskProviderHandlerTestDB(t)
	repo := repository.NewModel3DTilesRepository(db)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	task := &models.Model3DTilesTask{
		TenantID: 1, Name: "model3d tiles confirmation contract", Enabled: true,
		Config: commonModels.JSONMap{
			"source": commonModels.JSONMap{
				"item_locator": "addp://engine/11/path/models/site?type=item&item_id=46", "source_engine_id": uint(11),
				"item_fingerprint": "model3d-tiles-api-confirmation", "item_id": uint(46), "format": "osgb_scene",
			},
			"target_format": models.Model3DTilesTargetFormat3DTiles,
		},
	}
	if err := repo.CreateTask(context.Background(), task); err != nil {
		t.Fatalf("create model3d tiles task: %v", err)
	}

	handler := NewTaskProviderHandler(nil, nil, nil, nil, taskExecRepo)
	handler.SetModel3DTilesTaskService(service.NewModel3DTilesTaskService(repo))
	router := gin.New()
	router.Use(func(c *gin.Context) {
		setTenantAuthContextForTest(c, 1, 1)
		c.Next()
	})
	router.POST("/tasks/:task_type/:id/execute", handler.TaskExecute)
	path := "/tasks/" + commonExecution.TaskTypeModel3DTilesGeneration + "/" + strconv.FormatUint(uint64(task.ID), 10) + "/execute"

	first := executeTaskProviderRequest(t, router, path, ``)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first execute status = %d, want 202; body=%s", first.Code, first.Body.String())
	}
	var accepted TaskExecuteResponse
	if err := json.Unmarshal(first.Body.Bytes(), &accepted); err != nil {
		t.Fatalf("decode first response: %v", err)
	}
	waitForTaskProviderExecutionCompletion(t, taskExecRepo, accepted.ExecutionID, 1)

	var countBefore int64
	if err := db.Model(&commonExecution.TaskExecution{}).Count(&countBefore).Error; err != nil {
		t.Fatalf("count executions before rejected refresh: %v", err)
	}
	withoutAction := executeTaskProviderRequest(t, router, path, `{}`)
	if withoutAction.Code != http.StatusConflict {
		t.Fatalf("execute without action status = %d, want 409; body=%s", withoutAction.Code, withoutAction.Body.String())
	}
	var actionError struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if err := json.Unmarshal(withoutAction.Body.Bytes(), &actionError); err != nil {
		t.Fatalf("decode action error: %v", err)
	}
	if actionError.Code != existingResultActionRequiredCode || actionError.Error == "" {
		t.Fatalf("action error = %#v", actionError)
	}
	var countAfter int64
	if err := db.Model(&commonExecution.TaskExecution{}).Count(&countAfter).Error; err != nil {
		t.Fatalf("count executions after rejected refresh: %v", err)
	}
	if countAfter != countBefore {
		t.Fatalf("rejected refresh created execution: before=%d after=%d", countBefore, countAfter)
	}

	for _, body := range []string{
		`{"parameters":{"confirm_existing_result":true}}`,
		`{"parameters":{"existing_result_action":true}}`,
		`{"parameters":{"existing_result_action":"overwrite","unknown":true}}`,
		`{"parameters":{"existing_result_action":"keep"}}`,
	} {
		invalid := executeTaskProviderRequest(t, router, path, body)
		if invalid.Code != http.StatusBadRequest {
			t.Fatalf("invalid parameters status = %d, want 400; body=%s", invalid.Code, invalid.Body.String())
		}
	}

	overwrite := executeTaskProviderRequest(t, router, path, `{"trigger_type":"scheduled","source":"orchestrator","parent_execution_id":"pipeline-exec","parameters":{"existing_result_action":"overwrite"}}`)
	if overwrite.Code != http.StatusAccepted {
		t.Fatalf("overwrite execute status = %d, want 202; body=%s", overwrite.Code, overwrite.Body.String())
	}
	var overwriteResponse TaskExecuteResponse
	if err := json.Unmarshal(overwrite.Body.Bytes(), &overwriteResponse); err != nil {
		t.Fatalf("decode overwrite response: %v", err)
	}
	if overwriteResponse.ExecutionID == "" || overwriteResponse.Status != commonExecution.ExecutionStatusPending || overwriteResponse.ExecutionID == accepted.ExecutionID {
		t.Fatalf("overwrite response = %#v", overwriteResponse)
	}
	waitForTaskProviderExecutionCompletion(t, taskExecRepo, overwriteResponse.ExecutionID, 1)
	overwriteExecution, err := taskExecRepo.GetByExecutionID(context.Background(), overwriteResponse.ExecutionID, 1)
	if err != nil {
		t.Fatalf("load scheduled overwrite execution: %v", err)
	}
	if overwriteExecution.TriggerType != commonExecution.TriggerTypeScheduled || overwriteExecution.Source != commonExecution.ModuleOrchestrator {
		t.Fatalf("scheduled overwrite execution = %#v", overwriteExecution)
	}
	if overwriteExecution.ParentExecutionID == nil || *overwriteExecution.ParentExecutionID != "pipeline-exec" {
		t.Fatalf("scheduled overwrite parent_execution_id = %#v", overwriteExecution.ParentExecutionID)
	}
}

func executeTaskProviderRequest(t *testing.T, router *gin.Engine, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(http.MethodPost, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func waitForTaskProviderExecutionCompletion(t *testing.T, repo *commonExecution.TaskExecutionRepository, executionID string, tenantID int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		exec, err := repo.GetByExecutionID(context.Background(), executionID, tenantID)
		if err == nil && exec.IsCompleted() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("execution %s did not complete", executionID)
}

func TestTaskExecuteTileCacheReturnsPendingAndRejectsActiveExecution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newTaskProviderHandlerTestDB(t)
	tileCacheRepo := repository.NewTileCacheRepository(db)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	taskSvc := service.NewTileCacheTaskService(tileCacheRepo, taskExecRepo)
	task := &models.TileCacheTask{
		TenantID: 1,
		Name:     "tile cache execution contract",
		Enabled:  true,
		Config: commonModels.JSONMap{
			"target": commonModels.JSONMap{"item_fingerprint": "api-contract-fingerprint"},
			"tile":   commonModels.JSONMap{"format": "mvt"},
		},
	}
	if err := tileCacheRepo.CreateTask(context.Background(), task); err != nil {
		t.Fatalf("create tile cache task: %v", err)
	}

	handler := NewTaskProviderHandler(nil, taskSvc, nil, nil, taskExecRepo)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		setTenantAuthContextForTest(c, 1, 1)
		c.Next()
	})
	router.POST("/tasks/:task_type/:id/execute", handler.TaskExecute)

	path := "/tasks/" + commonExecution.TaskTypeVectorTileCacheGeneration + "/" + strconv.FormatUint(uint64(task.ID), 10) + "/execute"
	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodPost, path, nil))
	if first.Code != http.StatusAccepted {
		t.Fatalf("first execute status = %d, want 202; body=%s", first.Code, first.Body.String())
	}
	var accepted TaskExecuteResponse
	if err := json.Unmarshal(first.Body.Bytes(), &accepted); err != nil {
		t.Fatalf("decode first execute response: %v", err)
	}
	if accepted.ExecutionID == "" || accepted.Status != commonExecution.ExecutionStatusPending {
		t.Fatalf("first execute response = %#v, want pending execution", accepted)
	}

	// The in-process worker may finish immediately because no generator is configured.
	// Create a deterministic pending execution to verify the public conflict mapping.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		exec, err := taskExecRepo.GetByExecutionID(context.Background(), accepted.ExecutionID, 1)
		if err == nil && exec.IsCompleted() {
			break
		}
		time.Sleep(time.Millisecond)
	}
	pendingID := "manager-api-active-execution"
	pending := &commonExecution.TaskExecution{
		ExecutionID: pendingID, TenantID: 1, Module: commonExecution.ModuleManager,
		TaskType: commonExecution.TaskTypeVectorTileCacheGeneration, Source: commonExecution.ModuleManager,
		Status: commonExecution.ExecutionStatusPending, TriggerType: commonExecution.TriggerTypeManual,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if _, err := tileCacheRepo.ClaimExecution(context.Background(), task.ID, task.TenantID, pending, false); err != nil {
		t.Fatalf("claim deterministic pending execution: %v", err)
	}

	second := httptest.NewRecorder()
	router.ServeHTTP(second, httptest.NewRequest(http.MethodPost, path, nil))
	if second.Code != http.StatusConflict {
		t.Fatalf("second execute status = %d, want 409; body=%s", second.Code, second.Body.String())
	}
	assertStandardErrorResponse(t, second.Body.Bytes())
}

func TestTaskExecuteRasterCOGReturnsPendingAndRejectsActiveExecution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newTaskProviderHandlerTestDB(t)
	cogRepo := repository.NewRasterCOGRepository(db)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	taskSvc := service.NewRasterCOGTaskService(cogRepo)
	task := &models.RasterCOGTask{
		TenantID: 1,
		Name:     "raster COG execution contract",
		Enabled:  true,
		Config:   commonModels.JSONMap{"invalid": true},
	}
	if err := cogRepo.CreateTask(context.Background(), task); err != nil {
		t.Fatalf("create raster COG task: %v", err)
	}

	handler := NewTaskProviderHandler(nil, nil, nil, taskSvc, taskExecRepo)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		setTenantAuthContextForTest(c, 1, 1)
		c.Next()
	})
	router.POST("/tasks/:task_type/:id/execute", handler.TaskExecute)

	path := "/tasks/" + commonExecution.TaskTypeRasterCOGGeneration + "/" + strconv.FormatUint(uint64(task.ID), 10) + "/execute"
	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodPost, path, nil))
	if first.Code != http.StatusAccepted {
		t.Fatalf("first execute status = %d, want 202; body=%s", first.Code, first.Body.String())
	}
	var accepted TaskExecuteResponse
	if err := json.Unmarshal(first.Body.Bytes(), &accepted); err != nil {
		t.Fatalf("decode first execute response: %v", err)
	}
	if accepted.ExecutionID == "" || accepted.Status != commonExecution.ExecutionStatusPending {
		t.Fatalf("first execute response = %#v, want pending execution", accepted)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		exec, err := taskExecRepo.GetByExecutionID(context.Background(), accepted.ExecutionID, 1)
		if err == nil && exec.IsCompleted() {
			break
		}
		time.Sleep(time.Millisecond)
	}
	pending := &commonExecution.TaskExecution{
		ExecutionID: "manager-api-raster-cog-active", TenantID: 1, Module: commonExecution.ModuleManager,
		TaskType: commonExecution.TaskTypeRasterCOGGeneration, Source: commonExecution.ModuleManager,
		Status: commonExecution.ExecutionStatusPending, TriggerType: commonExecution.TriggerTypeManual,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if _, err := cogRepo.ClaimExecution(context.Background(), task.ID, task.TenantID, pending, false); err != nil {
		t.Fatalf("claim deterministic pending execution: %v", err)
	}

	second := httptest.NewRecorder()
	router.ServeHTTP(second, httptest.NewRequest(http.MethodPost, path, nil))
	if second.Code != http.StatusConflict {
		t.Fatalf("second execute status = %d, want 409; body=%s", second.Code, second.Body.String())
	}
	assertStandardErrorResponse(t, second.Body.Bytes())
}

func TestTaskExecuteRasterMosaicReturnsPendingAndRejectsActiveExecution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newTaskProviderHandlerTestDB(t)
	mosaicRepo := repository.NewRasterMosaicRepository(db)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	taskSvc := service.NewRasterMosaicTaskService(mosaicRepo, taskExecRepo)
	task := &models.RasterMosaicTask{
		TenantID: 1,
		Name:     "raster mosaic execution contract",
		Enabled:  true,
		Config:   commonModels.JSONMap{"invalid": true},
	}
	if err := mosaicRepo.CreateTask(context.Background(), task); err != nil {
		t.Fatalf("create raster mosaic task: %v", err)
	}

	handler := NewTaskProviderHandler(nil, nil, nil, nil, taskExecRepo, taskSvc)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		setTenantAuthContextForTest(c, 1, 1)
		c.Next()
	})
	router.POST("/tasks/:task_type/:id/execute", handler.TaskExecute)

	path := "/tasks/" + commonExecution.TaskTypeRasterMosaicGeneration + "/" + strconv.FormatUint(uint64(task.ID), 10) + "/execute"
	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodPost, path, nil))
	if first.Code != http.StatusAccepted {
		t.Fatalf("first execute status = %d, want 202; body=%s", first.Code, first.Body.String())
	}
	var accepted TaskExecuteResponse
	if err := json.Unmarshal(first.Body.Bytes(), &accepted); err != nil {
		t.Fatalf("decode first execute response: %v", err)
	}
	if accepted.ExecutionID == "" || accepted.Status != commonExecution.ExecutionStatusPending {
		t.Fatalf("first execute response = %#v, want pending execution", accepted)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		exec, err := taskExecRepo.GetByExecutionID(context.Background(), accepted.ExecutionID, 1)
		if err == nil && exec.IsCompleted() {
			break
		}
		time.Sleep(time.Millisecond)
	}
	pending := &commonExecution.TaskExecution{
		ExecutionID: "manager-api-raster-mosaic-active", TenantID: 1, Module: commonExecution.ModuleManager,
		TaskType: commonExecution.TaskTypeRasterMosaicGeneration, Source: commonExecution.ModuleManager,
		Status: commonExecution.ExecutionStatusPending, TriggerType: commonExecution.TriggerTypeManual,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if _, err := mosaicRepo.ClaimExecution(context.Background(), task.ID, task.TenantID, pending); err != nil {
		t.Fatalf("claim deterministic pending execution: %v", err)
	}

	second := httptest.NewRecorder()
	router.ServeHTTP(second, httptest.NewRequest(http.MethodPost, path, nil))
	if second.Code != http.StatusConflict {
		t.Fatalf("second execute status = %d, want 409; body=%s", second.Code, second.Body.String())
	}
	assertStandardErrorResponse(t, second.Body.Bytes())
}

func TestTaskExecuteModel3DGLBReturnsPendingAndRejectsActiveExecution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newTaskProviderHandlerTestDB(t)
	glbRepo := repository.NewModel3DGLBRepository(db)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	taskSvc := service.NewModel3DGLBTaskService(glbRepo)
	task := &models.Model3DGLBTask{
		TenantID: 1,
		Name:     "model 3d GLB execution contract",
		Enabled:  true,
		Config:   commonModels.JSONMap{"invalid": true},
	}
	if err := glbRepo.CreateTask(context.Background(), task); err != nil {
		t.Fatalf("create model 3d GLB task: %v", err)
	}

	handler := NewTaskProviderHandler(nil, nil, nil, nil, taskExecRepo)
	handler.SetModel3DGLBTaskService(taskSvc)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		setTenantAuthContextForTest(c, 1, 1)
		c.Next()
	})
	router.POST("/tasks/:task_type/:id/execute", handler.TaskExecute)

	path := "/tasks/" + commonExecution.TaskTypeModel3DGLBGeneration + "/" + strconv.FormatUint(uint64(task.ID), 10) + "/execute"
	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodPost, path, nil))
	if first.Code != http.StatusAccepted {
		t.Fatalf("first execute status = %d, want 202; body=%s", first.Code, first.Body.String())
	}
	var accepted TaskExecuteResponse
	if err := json.Unmarshal(first.Body.Bytes(), &accepted); err != nil {
		t.Fatalf("decode first execute response: %v", err)
	}
	if accepted.ExecutionID == "" || accepted.Status != commonExecution.ExecutionStatusPending {
		t.Fatalf("first execute response = %#v, want pending execution", accepted)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		exec, err := taskExecRepo.GetByExecutionID(context.Background(), accepted.ExecutionID, 1)
		if err == nil && exec.IsCompleted() {
			break
		}
		time.Sleep(time.Millisecond)
	}
	pending := &commonExecution.TaskExecution{
		ExecutionID: "manager-api-model-3d-glb-active", TenantID: 1, Module: commonExecution.ModuleManager,
		TaskType: commonExecution.TaskTypeModel3DGLBGeneration, Source: commonExecution.ModuleManager,
		Status: commonExecution.ExecutionStatusPending, TriggerType: commonExecution.TriggerTypeManual,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if _, err := glbRepo.ClaimExecution(context.Background(), task.ID, task.TenantID, pending, false); err != nil {
		t.Fatalf("claim deterministic pending execution: %v", err)
	}

	second := httptest.NewRecorder()
	router.ServeHTTP(second, httptest.NewRequest(http.MethodPost, path, nil))
	if second.Code != http.StatusConflict {
		t.Fatalf("second execute status = %d, want 409; body=%s", second.Code, second.Body.String())
	}
	assertStandardErrorResponse(t, second.Body.Bytes())
}

func TestTaskExecuteGaussianSplatKSplatReturnsPendingAndRejectsActiveExecution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newTaskProviderHandlerTestDB(t)
	ksplatRepo := repository.NewGaussianSplatKSplatRepository(db)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	taskSvc := service.NewGaussianSplatKSplatTaskService(ksplatRepo)
	task := &models.GaussianSplatKSplatTask{
		TenantID: 1,
		Name:     "gaussian splat KSplat execution contract",
		Enabled:  true,
		Config:   commonModels.JSONMap{"invalid": true},
	}
	if err := ksplatRepo.CreateTask(context.Background(), task); err != nil {
		t.Fatalf("create gaussian splat KSplat task: %v", err)
	}

	handler := NewTaskProviderHandler(nil, nil, nil, nil, taskExecRepo)
	handler.SetGaussianSplatKSplatTaskService(taskSvc)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		setTenantAuthContextForTest(c, 1, 1)
		c.Next()
	})
	router.POST("/tasks/:task_type/:id/execute", handler.TaskExecute)

	path := "/tasks/" + commonExecution.TaskTypeGaussianSplatKSplatGeneration + "/" + strconv.FormatUint(uint64(task.ID), 10) + "/execute"
	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodPost, path, nil))
	if first.Code != http.StatusAccepted {
		t.Fatalf("first execute status = %d, want 202; body=%s", first.Code, first.Body.String())
	}
	var accepted TaskExecuteResponse
	if err := json.Unmarshal(first.Body.Bytes(), &accepted); err != nil {
		t.Fatalf("decode first execute response: %v", err)
	}
	if accepted.ExecutionID == "" || accepted.Status != commonExecution.ExecutionStatusPending {
		t.Fatalf("first execute response = %#v, want pending execution", accepted)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		exec, err := taskExecRepo.GetByExecutionID(context.Background(), accepted.ExecutionID, 1)
		if err == nil && exec.IsCompleted() {
			break
		}
		time.Sleep(time.Millisecond)
	}
	pending := &commonExecution.TaskExecution{
		ExecutionID: "manager-api-gaussian-splat-ksplat-active", TenantID: 1, Module: commonExecution.ModuleManager,
		TaskType: commonExecution.TaskTypeGaussianSplatKSplatGeneration, Source: commonExecution.ModuleManager,
		Status: commonExecution.ExecutionStatusPending, TriggerType: commonExecution.TriggerTypeManual,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if _, err := ksplatRepo.ClaimExecution(context.Background(), task.ID, task.TenantID, pending, false); err != nil {
		t.Fatalf("claim deterministic pending execution: %v", err)
	}

	second := httptest.NewRecorder()
	router.ServeHTTP(second, httptest.NewRequest(http.MethodPost, path, nil))
	if second.Code != http.StatusConflict {
		t.Fatalf("second execute status = %d, want 409; body=%s", second.Code, second.Body.String())
	}
	assertStandardErrorResponse(t, second.Body.Bytes())
}

func TestTaskExecutePointCloudCOPCReturnsPendingAndRejectsActiveExecution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newTaskProviderHandlerTestDB(t)
	copcRepo := repository.NewPointCloudCOPCRepository(db)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	taskSvc := service.NewPointCloudCOPCTaskService(copcRepo)
	task := &models.PointCloudCOPCTask{
		TenantID: 1, Name: "point cloud COPC execution contract", Enabled: true,
		Config: commonModels.JSONMap{"invalid": true},
	}
	if err := copcRepo.CreateTask(context.Background(), task); err != nil {
		t.Fatalf("create point cloud COPC task: %v", err)
	}
	handler := NewTaskProviderHandler(nil, nil, nil, nil, taskExecRepo)
	handler.SetPointCloudCOPCTaskService(taskSvc)
	router := gin.New()
	router.Use(func(c *gin.Context) { setTenantAuthContextForTest(c, 1, 1); c.Next() })
	router.POST("/tasks/:task_type/:id/execute", handler.TaskExecute)
	path := "/tasks/" + commonExecution.TaskTypePointCloudCOPCGeneration + "/" + strconv.FormatUint(uint64(task.ID), 10) + "/execute"

	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodPost, path, nil))
	if first.Code != http.StatusAccepted {
		t.Fatalf("first execute status = %d, want 202; body=%s", first.Code, first.Body.String())
	}
	var accepted TaskExecuteResponse
	if err := json.Unmarshal(first.Body.Bytes(), &accepted); err != nil {
		t.Fatalf("decode first execute response: %v", err)
	}
	if accepted.ExecutionID == "" || accepted.Status != commonExecution.ExecutionStatusPending {
		t.Fatalf("first execute response = %#v, want pending execution", accepted)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		exec, err := taskExecRepo.GetByExecutionID(context.Background(), accepted.ExecutionID, 1)
		if err == nil && exec.IsCompleted() {
			break
		}
		time.Sleep(time.Millisecond)
	}
	pending := &commonExecution.TaskExecution{
		ExecutionID: "manager-api-point-cloud-copc-active", TenantID: 1, Module: commonExecution.ModuleManager,
		TaskType: commonExecution.TaskTypePointCloudCOPCGeneration, Source: commonExecution.ModuleManager,
		Status: commonExecution.ExecutionStatusPending, TriggerType: commonExecution.TriggerTypeManual,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if _, err := copcRepo.ClaimExecution(context.Background(), task.ID, task.TenantID, pending, false); err != nil {
		t.Fatalf("claim deterministic pending execution: %v", err)
	}
	second := httptest.NewRecorder()
	router.ServeHTTP(second, httptest.NewRequest(http.MethodPost, path, nil))
	if second.Code != http.StatusConflict {
		t.Fatalf("second execute status = %d, want 409; body=%s", second.Code, second.Body.String())
	}
	assertStandardErrorResponse(t, second.Body.Bytes())
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

func TestDecodeTileCacheTaskRequestRejectsScheduleFields(t *testing.T) {
	for _, body := range []string{
		`{"name":"tile cache","schedule":"0 * * * *","config":{}}`,
		`{"name":"tile cache","next_run_at":"2026-07-17T12:00:00Z","config":{}}`,
	} {
		gin.SetMode(gin.TestMode)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/vector_tile_cache_tasks", strings.NewReader(body))

		_, err := decodeTileCacheTaskRequest(c)
		if err == nil || !strings.Contains(err.Error(), "unknown field") {
			t.Fatalf("decode tile cache task body %s error = %v, want unknown field", body, err)
		}
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
	if err := db.Exec(`CREATE TABLE manager.vector_tile_cache (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		task_id INTEGER,
		deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create vector_tile_cache table: %v", err)
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
	if err := db.Exec(`CREATE TABLE manager.raster_cog (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		task_id INTEGER,
		deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create raster_cog table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.raster_mosaic_tasks (
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
		t.Fatalf("create raster_mosaic_tasks table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.model3d_tiles_tasks (
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
		t.Fatalf("create model3d_tiles_tasks table: %v", err)
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
	if err := db.Exec(`CREATE TABLE manager.point_cloud_copc_tasks (
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
		t.Fatalf("create point_cloud_copc_tasks table: %v", err)
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
	if err := db.Exec(`CREATE TABLE manager.model3d_tiles (
		id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, item_fingerprint TEXT NOT NULL,
		item_id INTEGER, locator TEXT, task_id INTEGER, last_execution_id TEXT, source_engine_id INTEGER,
		source_format TEXT, source_size_bytes INTEGER, target_format TEXT, storage_ref TEXT,
		manifest_ref TEXT, file_count INTEGER, size_bytes INTEGER, status TEXT, metadata JSON,
		error_message TEXT, created_by INTEGER, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create model3d_tiles table: %v", err)
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
	if err := db.Exec(`CREATE TABLE manager.point_cloud_copc (
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
		t.Fatalf("create point_cloud_copc table: %v", err)
	}
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatalf("ensure SQLite execution store: %v", err)
	}
	return db
}

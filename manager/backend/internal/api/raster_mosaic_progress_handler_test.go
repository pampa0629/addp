package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/manager/internal/config"
	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

func TestRasterMosaicProgressEndpointRecordsEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newTaskProviderHandlerTestDB(t)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	if err := taskExecRepo.Create(context.Background(), &commonExecution.TaskExecution{
		TenantID:    7,
		ExecutionID: "mosaic-progress-http-1",
		Module:      commonExecution.ModuleManager,
		TaskType:    commonExecution.TaskTypeRasterMosaicGeneration,
		Source:      commonExecution.ModuleManager,
		Status:      commonExecution.ExecutionStatusRunning,
		TriggerType: commonExecution.TriggerTypeManual,
	}); err != nil {
		t.Fatalf("create execution: %v", err)
	}
	rasterMosaicSvc := service.NewRasterMosaicTaskService(repository.NewRasterMosaicRepository(db), taskExecRepo)
	handler := NewTaskProviderHandler(nil, nil, nil, nil, taskExecRepo, rasterMosaicSvc)

	cfg := &config.Config{}
	cfg.InternalAPIKey = "secret"
	router := gin.New()
	router.Use(managerInternalAPIKeyMiddleware(cfg))
	router.POST("/internal/executions/:execution_id/events", handler.RecordRasterMosaicExecutionProgressEvent)

	body := `{"phase":"leaf_cog","event":"file_progress","total_files":10,"processed_files":3,"overall_progress":30}`
	req := httptest.NewRequest(http.MethodPost, "/internal/executions/mosaic-progress-http-1/events", strings.NewReader(body))
	req.Header.Set("X-Internal-API-Key", "secret")
	req.Header.Set("X-Tenant-ID", "7")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	got, err := taskExecRepo.GetByExecutionID(context.Background(), "mosaic-progress-http-1", 7)
	if err != nil {
		t.Fatalf("get execution: %v", err)
	}
	if got.Progress != 30 {
		t.Fatalf("progress = %d, want 30", got.Progress)
	}
}

func TestManagerProgressEndpointRecordsPointCloudCOPCEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newTaskProviderHandlerTestDB(t)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	if err := taskExecRepo.Create(context.Background(), &commonExecution.TaskExecution{
		TenantID:    7,
		ExecutionID: "point-cloud-progress-http-1",
		Module:      commonExecution.ModuleManager,
		TaskType:    commonExecution.TaskTypePointCloudCOPCGeneration,
		Source:      commonExecution.ModuleManager,
		Status:      commonExecution.ExecutionStatusRunning,
		TriggerType: commonExecution.TriggerTypeManual,
	}); err != nil {
		t.Fatalf("create execution: %v", err)
	}
	handler := NewTaskProviderHandler(nil, nil, nil, nil, taskExecRepo)
	handler.SetPointCloudCOPCTaskService(service.NewPointCloudCOPCTaskService(repository.NewPointCloudCOPCRepository(db)))

	cfg := &config.Config{}
	cfg.InternalAPIKey = "secret"
	router := gin.New()
	router.Use(managerInternalAPIKeyMiddleware(cfg))
	router.POST("/internal/executions/:execution_id/events", handler.RecordManagerExecutionProgressEvent)

	body := `{"phase":"convert","event":"progress","message":"生成点云 COPC 文件","overall_progress":48,"metadata":{"output_size_bytes":4096}}`
	req := httptest.NewRequest(http.MethodPost, "/internal/executions/point-cloud-progress-http-1/events", strings.NewReader(body))
	req.Header.Set("X-Internal-API-Key", "secret")
	req.Header.Set("X-Tenant-ID", "7")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	got, err := taskExecRepo.GetByExecutionID(context.Background(), "point-cloud-progress-http-1", 7)
	if err != nil {
		t.Fatalf("get execution: %v", err)
	}
	if got.Progress != 48 {
		t.Fatalf("progress = %d, want 48", got.Progress)
	}
	if got.CurrentStep == nil || *got.CurrentStep != "生成点云 COPC 文件" {
		t.Fatalf("current_step = %#v, want point cloud progress message", got.CurrentStep)
	}
}

func TestManagerProgressEndpointRecordsVectorTileCacheEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newTaskProviderHandlerTestDB(t)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	if err := taskExecRepo.Create(context.Background(), &commonExecution.TaskExecution{
		TenantID:    7,
		ExecutionID: "tile-cache-progress-http-1",
		Module:      commonExecution.ModuleManager,
		TaskType:    commonExecution.TaskTypeVectorTileCacheGeneration,
		Source:      commonExecution.ModuleManager,
		Status:      commonExecution.ExecutionStatusRunning,
		TriggerType: commonExecution.TriggerTypeManual,
		Progress:    5,
	}); err != nil {
		t.Fatalf("create execution: %v", err)
	}
	tileCacheSvc := service.NewTileCacheTaskService(repository.NewTileCacheRepository(db), taskExecRepo)
	handler := NewTaskProviderHandler(nil, tileCacheSvc, nil, nil, taskExecRepo)

	cfg := &config.Config{}
	cfg.InternalAPIKey = "secret"
	router := gin.New()
	router.Use(managerInternalAPIKeyMiddleware(cfg))
	router.POST("/internal/executions/:execution_id/events", handler.RecordManagerExecutionProgressEvent)

	body := `{"phase":"generate","event":"progress","message":"生成矢量瓦片缓存","current_zoom":10,"max_zoom":18,"tiles_processed":367,"tiles_total_estimate":1000,"progress_percent":36.7,"overall_progress":36.7,"metadata":{"worker":"python-workflow"}}`
	req := httptest.NewRequest(http.MethodPost, "/internal/executions/tile-cache-progress-http-1/events", strings.NewReader(body))
	req.Header.Set("X-Internal-API-Key", "secret")
	req.Header.Set("X-Tenant-ID", "7")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	got, err := taskExecRepo.GetByExecutionID(context.Background(), "tile-cache-progress-http-1", 7)
	if err != nil {
		t.Fatalf("get execution: %v", err)
	}
	if got.Progress != 37 {
		t.Fatalf("progress = %d, want 37", got.Progress)
	}
	if got.CurrentStep == nil || *got.CurrentStep != "生成矢量瓦片缓存" {
		t.Fatalf("current_step = %#v, want vector tile progress message", got.CurrentStep)
	}
}

func TestRasterMosaicProgressEndpointRequiresInternalTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	cfg.InternalAPIKey = "secret"
	router := gin.New()
	router.Use(managerInternalAPIKeyMiddleware(cfg))
	router.POST("/internal/executions/:execution_id/events", func(c *gin.Context) {
		c.JSON(http.StatusAccepted, gin.H{"status": "accepted"})
	})

	req := httptest.NewRequest(http.MethodPost, "/internal/executions/exec-1/events", strings.NewReader(`{"phase":"leaf_cog","event":"file_progress"}`))
	req.Header.Set("X-Internal-API-Key", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

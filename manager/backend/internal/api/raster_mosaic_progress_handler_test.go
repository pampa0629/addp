package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

func TestRasterMosaicProgressEndpointRecordsEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newTaskProviderHandlerTestDB(t)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	lease := createManagerProgressExecution(t, taskExecRepo, 7, "mosaic-progress-http-1", commonExecution.TaskTypeRasterMosaicGeneration, 0)
	rasterMosaicSvc := service.NewRasterMosaicTaskService(repository.NewRasterMosaicRepository(db), taskExecRepo)
	handler := NewTaskProviderHandler(nil, nil, nil, nil, taskExecRepo, rasterMosaicSvc)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		setTenantAuthContextForTest(c, 7, 1)
		c.Next()
	})
	router.POST("/executions/:execution_id/events", handler.RecordRasterMosaicExecutionProgressEvent)

	body := fmt.Sprintf(`{"attempt":%d,"lease_token":%q,"phase":"leaf_cog","event":"file_progress","total_files":10,"processed_files":3,"overall_progress":30}`, lease.Attempt, lease.Token)
	req := httptest.NewRequest(http.MethodPost, "/executions/mosaic-progress-http-1/events", strings.NewReader(body))
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
	lease := createManagerProgressExecution(t, taskExecRepo, 7, "point-cloud-progress-http-1", commonExecution.TaskTypePointCloudCOPCGeneration, 0)
	handler := NewTaskProviderHandler(nil, nil, nil, nil, taskExecRepo)
	handler.SetPointCloudCOPCTaskService(service.NewPointCloudCOPCTaskService(repository.NewPointCloudCOPCRepository(db)))

	router := gin.New()
	router.Use(func(c *gin.Context) {
		setTenantAuthContextForTest(c, 7, 1)
		c.Next()
	})
	router.POST("/executions/:execution_id/events", handler.RecordManagerExecutionProgressEvent)

	body := fmt.Sprintf(`{"attempt":%d,"lease_token":%q,"phase":"convert","event":"progress","message":"生成点云 COPC 文件","overall_progress":48,"metadata":{"output_size_bytes":4096}}`, lease.Attempt, lease.Token)
	req := httptest.NewRequest(http.MethodPost, "/executions/point-cloud-progress-http-1/events", strings.NewReader(body))
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
	lease := createManagerProgressExecution(t, taskExecRepo, 7, "tile-cache-progress-http-1", commonExecution.TaskTypeVectorTileCacheGeneration, 5)
	tileCacheSvc := service.NewTileCacheTaskService(repository.NewTileCacheRepository(db), taskExecRepo)
	handler := NewTaskProviderHandler(nil, tileCacheSvc, nil, nil, taskExecRepo)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		setTenantAuthContextForTest(c, 7, 1)
		c.Next()
	})
	router.POST("/executions/:execution_id/events", handler.RecordManagerExecutionProgressEvent)

	body := fmt.Sprintf(`{"attempt":%d,"lease_token":%q,"phase":"generate","event":"progress","message":"生成矢量瓦片缓存","current_zoom":10,"max_zoom":18,"tiles_processed":367,"tiles_total_estimate":1000,"progress_percent":36.7,"overall_progress":36.7,"metadata":{"worker":"geopython-workflow"}}`, lease.Attempt, lease.Token)
	req := httptest.NewRequest(http.MethodPost, "/executions/tile-cache-progress-http-1/events", strings.NewReader(body))
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

func TestManagerProgressEndpointRejectsStaleLease(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newTaskProviderHandlerTestDB(t)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	lease := createManagerProgressExecution(t, taskExecRepo, 7, "tile-cache-progress-stale", commonExecution.TaskTypeVectorTileCacheGeneration, 5)
	tileCacheSvc := service.NewTileCacheTaskService(repository.NewTileCacheRepository(db), taskExecRepo)
	handler := NewTaskProviderHandler(nil, tileCacheSvc, nil, nil, taskExecRepo)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		setTenantAuthContextForTest(c, 7, 1)
		c.Next()
	})
	router.POST("/executions/:execution_id/events", handler.RecordManagerExecutionProgressEvent)

	body := fmt.Sprintf(`{"attempt":%d,"lease_token":"stale-token","phase":"generate","event":"progress","overall_progress":50}`, lease.Attempt)
	req := httptest.NewRequest(http.MethodPost, "/executions/tile-cache-progress-stale/events", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusConflict, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"error_code":"execution_lease_conflict"`) {
		t.Fatalf("body = %s, want stable lease conflict error code", w.Body.String())
	}
	stored, err := taskExecRepo.GetByExecutionID(context.Background(), lease.ExecutionID, lease.TenantID)
	if err != nil {
		t.Fatalf("load execution: %v", err)
	}
	if stored.Progress != 5 {
		t.Fatalf("stale lease changed progress to %d", stored.Progress)
	}
}

func TestManagerProgressEndpointRequiresLeaseIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newTaskProviderHandlerTestDB(t)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	createManagerProgressExecution(t, taskExecRepo, 7, "tile-cache-progress-no-lease", commonExecution.TaskTypeVectorTileCacheGeneration, 5)
	handler := NewTaskProviderHandler(nil, service.NewTileCacheTaskService(repository.NewTileCacheRepository(db), taskExecRepo), nil, nil, taskExecRepo)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		setTenantAuthContextForTest(c, 7, 1)
		c.Next()
	})
	router.POST("/executions/:execution_id/events", handler.RecordManagerExecutionProgressEvent)

	req := httptest.NewRequest(http.MethodPost, "/executions/tile-cache-progress-no-lease/events", strings.NewReader(`{"phase":"generate","event":"progress","overall_progress":50}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"error_code":"invalid_execution_lease"`) {
		t.Fatalf("status = %d body=%s, want invalid lease response", w.Code, w.Body.String())
	}
}

func TestManagerProgressEndpointRecordsVectorTileSetEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newTaskProviderHandlerTestDB(t)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	lease := createManagerProgressExecution(t, taskExecRepo, 7, "tile-set-progress-http-1", commonExecution.TaskTypeVectorTileSetGeneration, 5)
	handler := NewTaskProviderHandler(nil, nil, nil, nil, taskExecRepo)
	handler.SetVectorTileSetTaskService(service.NewVectorTileSetTaskService(repository.NewVectorTileSetRepository(db), taskExecRepo))

	router := gin.New()
	router.Use(func(c *gin.Context) {
		setTenantAuthContextForTest(c, 7, 1)
		c.Next()
	})
	router.POST("/executions/:execution_id/events", handler.RecordManagerExecutionProgressEvent)

	body := fmt.Sprintf(`{"attempt":%d,"lease_token":%q,"phase":"publish","event":"progress","message":"生成矢量瓦片缓存","current_zoom":12,"max_zoom":12,"tiles_processed":18,"tiles_total_estimate":18,"overall_progress":95}`, lease.Attempt, lease.Token)
	req := httptest.NewRequest(http.MethodPost, "/executions/tile-set-progress-http-1/events", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	got, err := taskExecRepo.GetByExecutionID(context.Background(), "tile-set-progress-http-1", 7)
	if err != nil {
		t.Fatalf("get execution: %v", err)
	}
	if got.Progress != 95 {
		t.Fatalf("progress = %d, want 95", got.Progress)
	}
}

func TestRasterMosaicProgressEndpointUsesAuthContextTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newTaskProviderHandlerTestDB(t)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	lease := createManagerProgressExecution(t, taskExecRepo, 7, "exec-1", commonExecution.TaskTypeRasterMosaicGeneration, 0)
	handler := NewTaskProviderHandler(nil, nil, nil, nil, taskExecRepo)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		setTenantAuthContextForTest(c, 8, 1)
		c.Next()
	})
	router.POST("/executions/:execution_id/events", handler.RecordManagerExecutionProgressEvent)

	body := fmt.Sprintf(`{"attempt":%d,"lease_token":%q,"phase":"leaf_cog","event":"file_progress"}`, lease.Attempt, lease.Token)
	req := httptest.NewRequest(http.MethodPost, "/executions/exec-1/events", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

func createManagerProgressExecution(
	t *testing.T,
	repo *commonExecution.TaskExecutionRepository,
	tenantID int,
	executionID string,
	taskType string,
	progress int,
) commonExecution.Lease {
	t.Helper()
	now := time.Now().UTC()
	owner := "manager-progress-test"
	token := "lease-" + executionID
	expiresAt := now.Add(time.Minute)
	execution := &commonExecution.TaskExecution{
		TenantID: tenantID, ExecutionID: executionID, Module: commonExecution.ModuleManager,
		TaskType: taskType, Source: commonExecution.ModuleManager, Status: commonExecution.ExecutionStatusRunning,
		Progress: progress, TriggerType: commonExecution.TriggerTypeManual, Attempt: 1,
		LeaseOwner: &owner, LeaseToken: &token, LeaseExpiresAt: &expiresAt,
		StartedAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.Create(context.Background(), execution); err != nil {
		t.Fatalf("create leased execution: %v", err)
	}
	return commonExecution.Lease{ExecutionID: executionID, TenantID: tenantID, Attempt: 1, Token: token, Owner: owner}
}

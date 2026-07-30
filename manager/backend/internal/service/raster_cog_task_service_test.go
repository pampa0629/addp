package service

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/minio/minio-go/v7"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const testRasterWorkflowEngineType = "tenant_raster_workflow"

func TestRasterCOGTaskCreateReusesSemanticIdentity(t *testing.T) {
	db := newRasterCOGTaskServiceTestDB(t)
	repo := repository.NewRasterCOGRepository(db)
	svc := NewRasterCOGTaskService(repo)
	first := newRasterCOGTaskDefinition()
	if err := svc.Create(context.Background(), first); err != nil {
		t.Fatalf("create first raster COG task: %v", err)
	}

	duplicate := newRasterCOGTaskDefinition()
	duplicate.Name = "更新后的 COG 任务"
	duplicate.Description = "复用语义身份"
	if err := svc.Create(context.Background(), duplicate); err != nil {
		t.Fatalf("reuse raster COG task: %v", err)
	}
	if duplicate.ID != first.ID {
		t.Fatalf("reused task id = %d, want %d", duplicate.ID, first.ID)
	}
	if duplicate.Name != "更新后的 COG 任务" || duplicate.Description != "复用语义身份" {
		t.Fatalf("reused task = %#v, want updated mutable fields", duplicate)
	}
	items, total, err := repo.ListTasks(context.Background(), first.TenantID, 1, 20)
	if err != nil {
		t.Fatalf("list raster COG tasks: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("task total=%d len=%d, want one semantic task", total, len(items))
	}
}

func newRasterCOGTaskDefinition() *models.RasterCOGTask {
	return &models.RasterCOGTask{
		TenantID: 7,
		Name:     "生成 COG",
		Enabled:  true,
		Config: commonModels.JSONMap{
			"target": commonModels.JSONMap{
				"source_engine_id": 26,
				"locator":          "addp://engine/26/path/rasters/large.tif?type=file&item_id=100",
			},
			"raster": commonModels.JSONMap{
				"source_profile":    "geotiff",
				"source_size_bytes": int64(900 * 1024 * 1024),
				"width":             int64(120000),
				"height":            int64(80000),
				"band_count":        int64(3),
				"extent":            []float64{110, 20, 120, 30},
				"extent_srid":       4326,
			},
		},
	}
}

func TestRasterCOGTaskExecuteRecordsFailedExecutionWhenExecutorUnavailable(t *testing.T) {
	db := newRasterCOGTaskServiceTestDB(t)
	cogRepo := repository.NewRasterCOGRepository(db)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	taskSvc := NewRasterCOGTaskService(cogRepo)

	task := &models.RasterCOGTask{
		TenantID: 7,
		Name:     "生成 COG",
		Enabled:  true,
		Config: commonModels.JSONMap{
			"target": commonModels.JSONMap{
				"source_engine_id": 26,
				"locator":          "addp://engine/26/path/rasters/large.tif?type=file&item_id=100",
			},
			"raster": commonModels.JSONMap{
				"source_profile":    "geotiff",
				"source_size_bytes": int64(900 * 1024 * 1024),
				"width":             int64(120000),
				"height":            int64(80000),
				"band_count":        int64(3),
				"extent":            []float64{110, 20, 120, 30},
				"extent_srid":       4326,
			},
		},
	}
	if err := taskSvc.Create(context.Background(), task); err != nil {
		t.Fatalf("create raster COG generation task: %v", err)
	}
	target, ok := asJSONMap(task.Config["target"])
	if !ok || strings.TrimSpace(stringFromConfig(target["item_fingerprint"])) == "" {
		t.Fatalf("normalized target = %#v, want item_fingerprint", task.Config["target"])
	}
	if _, exists := task.Config["artifact"]; exists {
		t.Fatalf("config.artifact must not be persisted: %#v", task.Config)
	}
	if result, ok := asJSONMap(task.Config["result"]); !ok || stringFromConfig(result["storage_ref"]) == "" {
		t.Fatalf("normalized result = %#v, want storage_ref", task.Config["result"])
	}

	executionID, err := taskSvc.Execute(context.Background(), task.ID, task.TenantID, commonExecution.TriggerTypeManual, commonExecution.ModuleManager, nil, false)
	if err != nil {
		t.Fatalf("execute raster COG generation task: %v", err)
	}

	exec := waitForRasterCOGTaskExecution(t, taskExecRepo, executionID, int(task.TenantID))
	if exec.Status != commonExecution.ExecutionStatusFailed {
		t.Fatalf("execution status = %s, want failed", exec.Status)
	}
	if exec.TaskType != commonExecution.TaskTypeRasterCOGGeneration {
		t.Fatalf("task_type = %s, want %s", exec.TaskType, commonExecution.TaskTypeRasterCOGGeneration)
	}
	if message, _ := exec.ErrorDetails["message"].(string); !strings.Contains(message, "executor is not configured") {
		t.Fatalf("error_details.message = %#v, want executor missing", exec.ErrorDetails["message"])
	}

	results, total, err := cogRepo.List(context.Background(), repository.RasterCOGFilter{TenantID: task.TenantID, Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("list raster COG results: %v", err)
	}
	if total != 1 || len(results) != 1 {
		t.Fatalf("results total=%d len=%d, want 1", total, len(results))
	}
	result := results[0]
	if result.Status != models.RasterCOGStatusFailed {
		t.Fatalf("result status = %s, want failed", result.Status)
	}
	if result.StorageRef == "" || !strings.Contains(result.StorageRef, "tenant_7/cog/") {
		t.Fatalf("storage_ref = %q, want tenant scoped infra MinIO object ref", result.StorageRef)
	}
}

func TestRasterCOGTaskRejectsLegacyArtifactConfig(t *testing.T) {
	db := newRasterCOGTaskServiceTestDB(t)
	taskSvc := NewRasterCOGTaskService(repository.NewRasterCOGRepository(db))

	err := taskSvc.Create(context.Background(), &models.RasterCOGTask{
		TenantID: 7,
		Name:     "旧字段 COG",
		Enabled:  true,
		Config: commonModels.JSONMap{
			"target": commonModels.JSONMap{
				"source_engine_id": 26,
				"locator":          "addp://engine/26/path/rasters/large.tif?type=file&item_id=100",
			},
			"artifact": commonModels.JSONMap{
				"file_name": "large.cog.tif",
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "config.artifact is not supported") {
		t.Fatalf("Create() error = %v, want legacy config.artifact rejection", err)
	}
}

func TestRasterCOGPrepareResultReturnsUpdatedExistingResult(t *testing.T) {
	db := newRasterCOGTaskServiceTestDB(t)
	cogRepo := repository.NewRasterCOGRepository(db)
	taskSvc := NewRasterCOGTaskService(cogRepo)

	task := &models.RasterCOGTask{
		TenantID: 7,
		Name:     "重新生成 COG",
		Enabled:  true,
		Config: commonModels.JSONMap{
			"target": commonModels.JSONMap{
				"source_engine_id": 26,
				"locator":          "addp://engine/26/path/rasters/large.tif?type=file&item_id=100",
			},
			"raster": commonModels.JSONMap{
				"source_profile":    "geotiff",
				"source_size_bytes": int64(900 * 1024 * 1024),
				"width":             int64(120000),
				"height":            int64(80000),
				"band_count":        int64(3),
				"extent":            []float64{110, 20, 120, 30},
				"extent_srid":       4326,
			},
		},
	}
	if err := taskSvc.Create(context.Background(), task); err != nil {
		t.Fatalf("create raster COG generation task: %v", err)
	}
	target, _ := asJSONMap(task.Config["target"])
	fingerprint := stringFromConfig(target["item_fingerprint"])
	oldStorageRef := `{"type":"object","provider":"addp_object_storage","bucket":"manager","object":"old.cog.tif"}`
	if err := cogRepo.Create(context.Background(), &models.RasterCOG{
		TenantID:        task.TenantID,
		ItemFingerprint: fingerprint,
		Locator:         "addp://engine/26/path/rasters/large.tif?type=file&item_id=100",
		SourceEngineID:  26,
		TargetKind:      models.RasterCOGTargetKindMinIO,
		StorageRef:      oldStorageRef,
		FileName:        "old.cog.tif",
		Status:          models.RasterCOGStatusStale,
		Metadata:        commonModels.JSONMap{},
	}); err != nil {
		t.Fatalf("create existing raster COG result: %v", err)
	}

	result, execCfg, err := taskSvc.prepareResult(context.Background(), task, "exec-2")
	if err != nil {
		t.Fatalf("prepareResult() error = %v", err)
	}
	if result.StorageRef != execCfg.Result.StorageRef {
		t.Fatalf("returned storage_ref = %q, want updated %q", result.StorageRef, execCfg.Result.StorageRef)
	}
	if result.FileName != execCfg.Result.FileName {
		t.Fatalf("returned file_name = %q, want updated %q", result.FileName, execCfg.Result.FileName)
	}
	if result.Status != models.RasterCOGStatusBuilding {
		t.Fatalf("returned status = %q, want building", result.Status)
	}
	if result.Width != execCfg.Raster.Width || result.Height != execCfg.Raster.Height || result.BandCount != execCfg.Raster.BandCount {
		t.Fatalf("returned raster facts = width:%d height:%d bands:%d, want width:%d height:%d bands:%d",
			result.Width, result.Height, result.BandCount,
			execCfg.Raster.Width, execCfg.Raster.Height, execCfg.Raster.BandCount)
	}
}

func TestManagerRasterCOGExecutorPreparesGDALRuntimeAndInvokesPythonWorkflowOperator(t *testing.T) {
	var invokePayload map[string]interface{}
	workflowServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && r.URL.Path == "/api/operators" {
				writeTiffToCOGOperatorList(w, testRasterWorkflowEngineType, []string{"workflow", "direct"})
				return
			}
			if r.URL.Path != "/api/operators/tiff_to_cog/invoke" {
				t.Fatalf("unexpected workflow path: %s", r.URL.Path)
			}
			if err := json.NewDecoder(r.Body).Decode(&invokePayload); err != nil {
				t.Fatalf("decode operator invoke request: %v", err)
			}
			params := invokePayload["params"].(map[string]interface{})
			if params["source_uri"] != "/mnt/addp-nfs/rasters/large.tif" {
				t.Fatalf("source_uri = %#v", params["source_uri"])
			}
			if params["target_uri"] != "/vsis3/manager/tenant_7/cog/fp/large.cog.tif" {
				t.Fatalf("target_uri = %#v", params["target_uri"])
			}
			gdalEnv := params["gdal_env"].(map[string]interface{})
			if gdalEnv["AWS_S3_ENDPOINT"] != "minio:9000" || gdalEnv["AWS_HTTPS"] != "NO" {
				t.Fatalf("gdal_env = %#v", gdalEnv)
			}
			if gdalEnv["CPL_VSIL_USE_TEMP_FILE_FOR_RANDOM_WRITE"] != "YES" {
				t.Fatalf("gdal_env = %#v, want random write temp file enabled", gdalEnv)
			}
			if params["assign_srs"] != "+proj=longlat +datum=WGS84 +no_defs" {
				t.Fatalf("assign_srs = %#v, want WGS84 proj string", params["assign_srs"])
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"success","execution_id":"py-1","execution_time_ms":45.5,"result":"{\"profile\":\"cog\",\"width\":256,\"height\":128,\"band_count\":3,\"size_bytes\":12,\"extent\":[110,20,120,30],\"extent_srid\":4326,\"source_crs\":\"EPSG:4326\",\"source_crs_definition\":\"GEOGCS[\\\"WGS 84\\\"]\"}"}`))
		}),
	}
	workflowListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer workflowServer.Close()
	go func() { _ = workflowServer.Serve(workflowListener) }()
	_, workflowPort, err := net.SplitHostPort(workflowListener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	systemServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/internal/engines/26" {
				t.Fatalf("unexpected system path: %s", r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":26,"tenant_id":7,"name":"NFS","engine_type":"nfs","connection_info":{"mount_path":"/mnt/addp-nfs"},"lifecycle_state":"active"}`))
		}),
	}
	systemListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer systemServer.Close()
	go func() { _ = systemServer.Serve(systemListener) }()

	executor := NewManagerRasterCOGExecutor(
		commonClient.NewSystemClientWithInternalKey("http://"+systemListener.Addr().String(), "internal-key"),
		recordingWorkflowLister{engines: []commonModels.Engine{{
			ID:         99,
			Name:       "Tenant Raster Workflow",
			EngineType: testRasterWorkflowEngineType,
			ConnectionInfo: commonModels.ConnectionInfo{
				"protocol": "http",
				"host":     "127.0.0.1",
				"port":     workflowPort,
			},
			LifecycleState: "active",
			Capabilities:   testRasterWorkflowCapabilities(t, testRasterWorkflowEngineType),
		}}},
		&recordingCOGObjectStore{size: 34},
		"http://minio:9000",
		"minioadmin",
		"minioadmin",
		false,
		"manager",
	)
	task := &models.RasterCOGTask{TenantID: 7, Name: "生成 COG"}
	result, err := executor.BuildRasterCOG(context.Background(), RasterCOGExecutionRequest{
		Task: task,
		Config: RasterCOGExecutionConfig{
			Target: RasterCOGTargetConfig{
				SourceEngineID:  26,
				ItemFingerprint: "fp",
				Locator:         "addp://engine/26/path/rasters/large.tif?type=file",
				FullName:        "rasters/large.tif",
			},
			Raster: RasterCOGRasterConfig{
				SourceSizeBytes: 7,
				SourceSRID:      4326,
				SourceCRS:       "EPSG:4326",
				Extent:          []float64{110, 20, 120, 30},
				ExtentSRID:      4326,
			},
			COG: RasterCOGOptionsConfig{
				Compression:        "DEFLATE",
				BlockSize:          512,
				OverviewResampling: "NEAREST",
			},
			Result: RasterCOGTargetResultConfig{
				StorageRef: `{"type":"object","provider":"addp_object_storage","bucket":"manager","object":"tenant_7/cog/fp/large.cog.tif"}`,
				FileName:   "large.cog.tif",
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildRasterCOG() error = %v", err)
	}
	if result.StorageRef != `{"type":"object","provider":"addp_object_storage","bucket":"manager","object":"tenant_7/cog/fp/large.cog.tif"}` || result.Width != 256 || result.Height != 128 || result.BandCount != 3 || result.SizeBytes != 34 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Metadata["workflow_runtime"].(commonModels.JSONMap)["execution_id"] != "py-1" {
		t.Fatalf("metadata = %#v, want workflow runtime execution id", result.Metadata)
	}
	workflowRuntime := result.Metadata["workflow_runtime"].(commonModels.JSONMap)
	if workflowRuntime["engine_id"] != uint(99) || workflowRuntime["engine_name"] != "Tenant Raster Workflow" || workflowRuntime["engine_type"] != testRasterWorkflowEngineType {
		t.Fatalf("metadata = %#v, want workflow runtime identity", result.Metadata)
	}
	if workflowRuntime["operator"] != "tiff_to_cog" || workflowRuntime["mode"] != "direct" {
		t.Fatalf("metadata = %#v, want direct mode", result.Metadata)
	}
	if workflowRuntime["execution_time_ms"] != float64(45.5) {
		t.Fatalf("metadata = %#v, want runtime execution time", result.Metadata)
	}
	source := result.Metadata["source"].(commonModels.JSONMap)
	sourceAccess := source["access"].(commonModels.JSONMap)
	if sourceAccess["engine_type"] != "nfs" || sourceAccess["access_method"] != "mounted_path" {
		t.Fatalf("metadata = %#v, want source access audit", result.Metadata)
	}
	if result.SourceCRS != "EPSG:4326" {
		t.Fatalf("source_crs = %q, want short CRS ref", result.SourceCRS)
	}
	rasterFacts, ok := asJSONMap(result.Metadata["raster_facts"])
	if !ok || rasterFacts["source_crs_definition"] != "GEOGCS[\"WGS 84\"]" {
		t.Fatalf("raster facts = %#v, want CRS definition retained in metadata", rasterFacts)
	}
}

func TestAuthoritativeRasterCRSFallsBackToConfiguredSourceFacts(t *testing.T) {
	sourceSRID := authoritativeRasterSRID(0, 9122, 4326)
	extentSRID := authoritativeExtentSRID(9122, 4326, sourceSRID)
	sourceCRS := authoritativeRasterCRS("EPSG:9122", "", sourceSRID)

	if sourceSRID != 4326 {
		t.Fatalf("sourceSRID = %d, want configured 4326", sourceSRID)
	}
	if extentSRID != 4326 {
		t.Fatalf("extentSRID = %d, want configured 4326", extentSRID)
	}
	if sourceCRS != "EPSG:4326" {
		t.Fatalf("sourceCRS = %q, want EPSG:4326", sourceCRS)
	}
}

func TestManagerRasterCOGExecutorPreservesOperatorErrorDetails(t *testing.T) {
	workflowServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && r.URL.Path == "/api/operators" {
				writeTiffToCOGOperatorList(w, testRasterWorkflowEngineType, []string{"workflow", "direct"})
				return
			}
			if r.URL.Path != "/api/operators/tiff_to_cog/invoke" {
				t.Fatalf("unexpected workflow path: %s", r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"status":"failed","error":"算子执行失败","error_code":"EXECUTION_FAILED","details":"gdal_translate stderr"}`))
		}),
	}
	workflowListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer workflowServer.Close()
	go func() { _ = workflowServer.Serve(workflowListener) }()
	_, workflowPort, err := net.SplitHostPort(workflowListener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	systemServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/internal/engines/26" {
				t.Fatalf("unexpected system path: %s", r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":26,"tenant_id":7,"name":"NFS","engine_type":"nfs","connection_info":{"mount_path":"/mnt/addp-nfs"},"lifecycle_state":"active"}`))
		}),
	}
	systemListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer systemServer.Close()
	go func() { _ = systemServer.Serve(systemListener) }()

	executor := NewManagerRasterCOGExecutor(
		commonClient.NewSystemClientWithInternalKey("http://"+systemListener.Addr().String(), "internal-key"),
		recordingWorkflowLister{engines: []commonModels.Engine{{
			ID:         99,
			Name:       "Tenant Raster Workflow",
			EngineType: testRasterWorkflowEngineType,
			ConnectionInfo: commonModels.ConnectionInfo{
				"protocol": "http",
				"host":     "127.0.0.1",
				"port":     workflowPort,
			},
			LifecycleState: "active",
			Capabilities:   testRasterWorkflowCapabilities(t, testRasterWorkflowEngineType),
		}}},
		&recordingCOGObjectStore{size: 34},
		"http://minio:9000",
		"minioadmin",
		"minioadmin",
		false,
		"manager",
	)
	_, err = executor.BuildRasterCOG(context.Background(), RasterCOGExecutionRequest{
		Task: &models.RasterCOGTask{TenantID: 7, Name: "生成 COG"},
		Config: RasterCOGExecutionConfig{
			Target: RasterCOGTargetConfig{
				SourceEngineID:  26,
				ItemFingerprint: "fp",
				Locator:         "addp://engine/26/path/rasters/large.tif?type=file",
				FullName:        "rasters/large.tif",
			},
			COG: RasterCOGOptionsConfig{
				Compression:        "DEFLATE",
				BlockSize:          512,
				OverviewResampling: "NEAREST",
			},
			Result: RasterCOGTargetResultConfig{
				StorageRef: `{"type":"object","provider":"addp_object_storage","bucket":"manager","object":"tenant_7/cog/fp/large.cog.tif"}`,
				FileName:   "large.cog.tif",
			},
		},
	})
	if err == nil {
		t.Fatal("BuildRasterCOG() returned nil error")
	}
	if !strings.Contains(err.Error(), "gdal_translate stderr") || !strings.Contains(err.Error(), "EXECUTION_FAILED") {
		t.Fatalf("error = %q, want operator details", err.Error())
	}
}

func TestManagerRasterCOGExecutorRejectsOperatorWithoutDirectMode(t *testing.T) {
	workflowServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && r.URL.Path == "/api/operators" {
				writeTiffToCOGOperatorList(w, testRasterWorkflowEngineType, []string{"workflow"})
				return
			}
			t.Fatalf("unexpected workflow path: %s", r.URL.Path)
		}),
	}
	workflowListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer workflowServer.Close()
	go func() { _ = workflowServer.Serve(workflowListener) }()
	_, workflowPort, err := net.SplitHostPort(workflowListener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	systemServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/internal/engines/26" {
				t.Fatalf("unexpected system path: %s", r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":26,"tenant_id":7,"name":"NFS","engine_type":"nfs","connection_info":{"mount_path":"/mnt/addp-nfs"},"lifecycle_state":"active"}`))
		}),
	}
	systemListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer systemServer.Close()
	go func() { _ = systemServer.Serve(systemListener) }()

	executor := NewManagerRasterCOGExecutor(
		commonClient.NewSystemClientWithInternalKey("http://"+systemListener.Addr().String(), "internal-key"),
		recordingWorkflowLister{engines: []commonModels.Engine{{
			ID:         99,
			Name:       "Tenant Raster Workflow",
			EngineType: testRasterWorkflowEngineType,
			ConnectionInfo: commonModels.ConnectionInfo{
				"protocol": "http",
				"host":     "127.0.0.1",
				"port":     workflowPort,
			},
			LifecycleState: "active",
			Capabilities:   testRasterWorkflowCapabilities(t, testRasterWorkflowEngineType),
		}}},
		&recordingCOGObjectStore{size: 34},
		"http://minio:9000",
		"minioadmin",
		"minioadmin",
		false,
		"manager",
	)
	_, err = executor.BuildRasterCOG(context.Background(), RasterCOGExecutionRequest{
		Task: &models.RasterCOGTask{TenantID: 7, Name: "生成 COG"},
		Config: RasterCOGExecutionConfig{
			Target: RasterCOGTargetConfig{
				SourceEngineID:  26,
				ItemFingerprint: "fp",
				Locator:         "addp://engine/26/path/rasters/large.tif?type=file",
				FullName:        "rasters/large.tif",
			},
			Result: RasterCOGTargetResultConfig{
				StorageRef: `{"type":"object","provider":"addp_object_storage","bucket":"manager","object":"tenant_7/cog/fp/large.cog.tif"}`,
				FileName:   "large.cog.tif",
			},
		},
	})
	if err == nil {
		t.Fatal("BuildRasterCOG() returned nil error")
	}
	if !strings.Contains(err.Error(), "direct workflow operator") || !strings.Contains(err.Error(), "does not support direct invocation") {
		t.Fatalf("error = %q, want direct mode rejection", err.Error())
	}
}

func TestRasterCOGTaskKeepsRunningStateWhenAtomicCompletionFails(t *testing.T) {
	db := newRasterCOGTaskServiceTestDB(t)
	if err := db.Exec(`CREATE TRIGGER manager.fail_cog_ready_update
		BEFORE UPDATE OF status ON raster_cog
		WHEN NEW.status = 'ready'
		BEGIN
			SELECT RAISE(ABORT, 'ready update failed');
		END`).Error; err != nil {
		t.Fatalf("create fail trigger: %v", err)
	}
	cogRepo := repository.NewRasterCOGRepository(db)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	taskSvc := NewRasterCOGTaskService(cogRepo)
	taskSvc.SetExecutor(staticRasterCOGExecutor{result: &RasterCOGExecutionResult{
		StorageRef: `{"type":"object","provider":"addp_object_storage","bucket":"manager","object":"tenant_7/cog/fp/large.cog.tif"}`,
		FileName:   "large.cog.tif",
		SizeBytes:  12,
		Width:      256,
		Height:     128,
		BandCount:  3,
		Metadata:   commonModels.JSONMap{"raster_facts": commonModels.JSONMap{"profile": "cog"}},
	}})

	task := &models.RasterCOGTask{
		TenantID: 7,
		Name:     "生成 COG",
		Enabled:  true,
		Config: commonModels.JSONMap{
			"target": commonModels.JSONMap{
				"source_engine_id": 26,
				"locator":          "addp://engine/26/path/rasters/large.tif?type=file&item_id=100",
			},
			"raster": commonModels.JSONMap{
				"source_profile":    "geotiff",
				"source_size_bytes": int64(1024),
				"width":             int64(256),
				"height":            int64(128),
				"band_count":        int64(3),
			},
		},
	}
	if err := taskSvc.Create(context.Background(), task); err != nil {
		t.Fatalf("create raster COG generation task: %v", err)
	}
	executionID := "raster-cog-atomic-completion-failure"
	createdAt := time.Now()
	execution := &commonExecution.TaskExecution{
		ExecutionID: executionID, TenantID: int(task.TenantID), Module: commonExecution.ModuleManager,
		TaskType: commonExecution.TaskTypeRasterCOGGeneration, Source: commonExecution.ModuleManager,
		Status: commonExecution.ExecutionStatusPending, TriggerType: commonExecution.TriggerTypeManual,
		CreatedAt: createdAt, UpdatedAt: createdAt,
	}
	claimedTask, err := cogRepo.ClaimExecution(context.Background(), task.ID, task.TenantID, execution, false)
	if err != nil {
		t.Fatalf("claim raster COG generation task: %v", err)
	}
	taskSvc.runRasterCOGGeneration(context.Background(), claimedTask, executionID)

	exec, err := taskExecRepo.GetByExecutionID(context.Background(), executionID, int(task.TenantID))
	if err != nil {
		t.Fatalf("load execution: %v", err)
	}
	if exec.Status != commonExecution.ExecutionStatusRunning || exec.CompletedAt != nil {
		t.Fatalf("execution status=%s completed_at=%v, want running without terminal commit", exec.Status, exec.CompletedAt)
	}
	results, total, err := cogRepo.List(context.Background(), repository.RasterCOGFilter{TenantID: task.TenantID, Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("list results: %v", err)
	}
	if total != 1 || len(results) != 1 {
		t.Fatalf("results total=%d len=%d, want 1", total, len(results))
	}
	if results[0].Status != models.RasterCOGStatusBuilding {
		t.Fatalf("result status = %s, want building after atomic rollback", results[0].Status)
	}
	if results[0].ErrorMessage != "" {
		t.Fatalf("result error = %q, want unchanged building result", results[0].ErrorMessage)
	}
	storedTask, err := cogRepo.GetTask(context.Background(), task.ID, task.TenantID)
	if err != nil {
		t.Fatalf("load task: %v", err)
	}
	if storedTask.LastExecutionStatus == nil || *storedTask.LastExecutionStatus != commonExecution.ExecutionStatusRunning {
		t.Fatalf("task last status = %v, want running after atomic rollback", storedTask.LastExecutionStatus)
	}
}

func newRasterCOGTaskServiceTestDB(t *testing.T) *gorm.DB {
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
	if err := db.Exec("ATTACH DATABASE ':memory:' AS common").Error; err != nil {
		t.Fatalf("attach common schema: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS manager").Error; err != nil {
		t.Fatalf("attach manager schema: %v", err)
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
			item_fingerprint TEXT NOT NULL,
			item_id INTEGER,
			locator TEXT,
			task_id INTEGER,
			last_execution_id TEXT,
			source_engine_id INTEGER NOT NULL,
			source_profile TEXT,
		source_size_bytes INTEGER,
		target_kind TEXT NOT NULL,
		storage_ref TEXT NOT NULL,
		file_name TEXT,
		size_bytes INTEGER,
		width INTEGER,
		height INTEGER,
		band_count INTEGER,
		source_srid INTEGER,
		source_crs TEXT,
		extent JSON,
		extent_srid INTEGER,
		status TEXT NOT NULL,
		metadata JSON,
		error_message TEXT,
		created_by INTEGER,
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create raster_cog table: %v", err)
	}
	return db
}

func waitForRasterCOGTaskExecution(t *testing.T, repo *commonExecution.TaskExecutionRepository, executionID string, tenantID int) *commonExecution.TaskExecution {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		exec, err := repo.GetByExecutionID(context.Background(), executionID, tenantID)
		if err == nil && exec.IsCompleted() {
			return exec
		}
		time.Sleep(10 * time.Millisecond)
	}
	exec, err := repo.GetByExecutionID(context.Background(), executionID, tenantID)
	if err != nil {
		t.Fatalf("load execution after wait: %v", err)
	}
	t.Fatalf("execution status still %s after wait", exec.Status)
	return nil
}

func writeTiffToCOGOperatorList(w http.ResponseWriter, engineType string, executionModes []string) {
	w.Header().Set("Content-Type", "application/json")
	payload := map[string]interface{}{
		"status": "success",
		"operators": []map[string]interface{}{
			{
				"id":              "tiff_to_cog",
				"name":            "tiff_to_cog",
				"display_name":    "TIFF 转 COG",
				"engine_type":     engineType,
				"type":            "raster",
				"category":        "格式转换",
				"category_path":   []string{"格式转换"},
				"description":     "生成云优化 GeoTIFF",
				"execution_modes": executionModes,
				"parameters": []map[string]interface{}{
					{"name": "source_uri", "type": "string", "required": true, "description": "源 TIFF URI"},
					{"name": "target_uri", "type": "string", "required": true, "description": "目标 COG URI"},
					{"name": "gdal_env", "type": "object", "required": false, "description": "GDAL 环境变量"},
				},
				"output_ports": []map[string]interface{}{
					{"name": "default", "type": "object", "description": "COG 生成结果", "is_default": true},
				},
			},
		},
		"count": 1,
	}
	_ = json.NewEncoder(w).Encode(payload)
}

func testRasterWorkflowCapabilities(t *testing.T, engineType string) *commonModels.JSONString {
	t.Helper()
	capabilities, err := plugin.MarshalEngineCapabilities(plugin.NewWorkflowCapabilities(engineType, plugin.WorkflowRuntimeAPIAddpV1))
	if err != nil {
		t.Fatalf("MarshalEngineCapabilities: %v", err)
	}
	value := commonModels.JSONString(capabilities)
	return &value
}

type recordingWorkflowLister struct {
	engines []commonModels.Engine
	onList  func()
}

func (l recordingWorkflowLister) ListWorkflowEngines(uint) ([]commonModels.Engine, error) {
	if l.onList != nil {
		l.onList()
	}
	return l.engines, nil
}

type staticRasterCOGExecutor struct {
	result *RasterCOGExecutionResult
	err    error
}

func (e staticRasterCOGExecutor) BuildRasterCOG(context.Context, RasterCOGExecutionRequest) (*RasterCOGExecutionResult, error) {
	return e.result, e.err
}

func TestSourceObjectClientConfigUsesCommonObjectStoreEndpointRules(t *testing.T) {
	cfg, err := sourceObjectClientConfig("minio", plugin.ConnectionInfo{
		"endpoint":   "http://127.0.0.1:19000",
		"access_key": "ak",
		"secret_key": "sk",
		"use_ssl":    false,
	})
	if err != nil {
		t.Fatalf("sourceObjectClientConfig() error = %v", err)
	}
	if cfg.Endpoint != "127.0.0.1:19000" {
		t.Fatalf("endpoint = %q, want normalized host without scheme", cfg.Endpoint)
	}
	if cfg.UseSSL {
		t.Fatalf("use_ssl = true, want false from endpoint/use_ssl")
	}
	if cfg.AccessKey != "ak" || cfg.SecretKey != "sk" {
		t.Fatalf("credentials = %#v, want parsed access and secret keys", cfg)
	}
}

type recordingCOGObjectStore struct {
	size int64
}

func (e *recordingCOGObjectStore) BucketExists(context.Context, string) (bool, error) {
	return true, nil
}

func (e *recordingCOGObjectStore) MakeBucket(context.Context, string, minio.MakeBucketOptions) error {
	return nil
}

func (e *recordingCOGObjectStore) StatObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error) {
	return minio.ObjectInfo{Size: e.size}, nil
}

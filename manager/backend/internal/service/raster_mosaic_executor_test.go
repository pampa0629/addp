package service

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"testing"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
)

func TestManagerRasterMosaicExecutorSendsAccessPlanToPython(t *testing.T) {
	var capturedParams map[string]interface{}
	workflowServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && r.URL.Path == "/api/operators" {
				writeBuildRasterMosaicOperatorList(w, testRasterWorkflowEngineType, []string{"workflow", "direct"})
				return
			}
			if r.URL.Path != "/api/operators/build_raster_mosaic/invoke" {
				t.Fatalf("unexpected workflow path: %s", r.URL.Path)
			}
			var payload struct {
				Params map[string]interface{} `json:"params"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode workflow request: %v", err)
			}
			capturedParams = payload.Params
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"success","execution_id":"py-mosaic-1","execution_time_ms":123,"result":{"manifest_locator":"addp://engine/31/path/mosaics/srtm/mosaic.addp.json?type=object","manifest_ref":"mosaic.addp.json","index_ref":"index/source-index.json","overview_ref":"overviews/overview.cog.tif","leaf_count":12}}`))
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
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/api/v1/internal/engines/26":
				_, _ = w.Write([]byte(`{"id":26,"tenant_id":7,"name":"Source MinIO","engine_type":"minio","connection_info":{"endpoint":"http://source-minio:9000","access_key":"source-ak","secret_key":"source-sk","use_ssl":false},"lifecycle_state":"active"}`))
			case "/api/v1/internal/engines/31":
				_, _ = w.Write([]byte(`{"id":31,"tenant_id":7,"name":"Target MinIO","engine_type":"minio","connection_info":{"endpoint":"http://target-minio:9000","access_key":"target-ak","secret_key":"target-sk","use_ssl":false},"lifecycle_state":"active"}`))
			default:
				t.Fatalf("unexpected system path: %s", r.URL.Path)
			}
		}),
	}
	systemListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer systemServer.Close()
	go func() { _ = systemServer.Serve(systemListener) }()

	executor := NewManagerRasterMosaicExecutor(
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
		"http://manager.internal",
		"internal-key",
		0,
	)

	result, err := executor.BuildRasterMosaic(context.Background(), RasterMosaicExecutionRequest{
		Task:        &models.RasterMosaicTask{TenantID: 7, Name: "生成 mosaic"},
		ExecutionID: "mosaic-exec-1",
		Config: RasterMosaicExecutionConfig{
			Source: RasterMosaicSourceConfig{
				NodeLocator:     "addp://engine/26/path/source-bucket/rasters?type=directory",
				SourceEngineID:  26,
				Recursive:       true,
				IncludePatterns: []string{"*.tif", "*.tiff"},
			},
			Placement: RasterMosaicPlacementConfig{Mode: "detached"},
			Target: RasterMosaicTargetConfig{
				StorageLocator: "addp://engine/31/path/mosaics?type=directory",
				TargetEngineID: 31,
				DatasetName:    "srtm",
			},
			COG: RasterMosaicCOGConfig{
				Compression:        "DEFLATE",
				BlockSize:          512,
				OverviewResampling: "NEAREST",
				ValidateSourceCOG:  true,
				LeafConcurrency:    2,
				NumThreads:         2,
				LeafRetryAttempts:  3,
			},
			Overview: RasterMosaicOverviewConfig{
				Enabled:    true,
				MaxPixels:  64000000,
				Resampling: "AVERAGE",
			},
			Tiles: RasterMosaicTilesConfig{Enabled: false, Format: "webp"},
		},
	})
	if err != nil {
		t.Fatalf("BuildRasterMosaic() error = %v", err)
	}
	if result.ManifestRef != "mosaic.addp.json" || result.LeafCount != 12 {
		t.Fatalf("unexpected result: %+v", result)
	}
	accessPlan, ok := capturedParams["access_plan"].(map[string]interface{})
	if !ok {
		t.Fatalf("access_plan = %#v, want object", capturedParams["access_plan"])
	}
	sourcePlan := accessPlan["source"].(map[string]interface{})
	targetPlan := accessPlan["target"].(map[string]interface{})
	progress := accessPlan["progress_callback"].(map[string]interface{})
	if sourcePlan["root_uri"] != "/vsis3/source-bucket/rasters" {
		t.Fatalf("source root_uri = %#v", sourcePlan["root_uri"])
	}
	if targetPlan["dataset_root_uri"] != "/vsis3/mosaics/srtm" {
		t.Fatalf("target dataset_root_uri = %#v", targetPlan["dataset_root_uri"])
	}
	sourceEnv := sourcePlan["gdal_env"].(map[string]interface{})
	targetEnv := targetPlan["gdal_env"].(map[string]interface{})
	if sourceEnv["GDAL_DISABLE_READDIR_ON_OPEN"] != "EMPTY_DIR" || targetEnv["GDAL_DISABLE_READDIR_ON_OPEN"] != "EMPTY_DIR" {
		t.Fatalf("gdal env should disable remote readdir: source=%#v target=%#v", sourceEnv, targetEnv)
	}
	if !strings.Contains(progress["endpoint"].(string), "/api/v1/manager/internal/executions/mosaic-exec-1/events") {
		t.Fatalf("progress endpoint = %#v", progress["endpoint"])
	}
	if capturedParams["source"] != nil || capturedParams["target"] != nil {
		t.Fatalf("legacy source/target params should not be sent: %#v", capturedParams)
	}
	cog := capturedParams["cog"].(map[string]interface{})
	if cog["leaf_concurrency"] != float64(2) {
		t.Fatalf("cog.leaf_concurrency = %#v, want 2", cog["leaf_concurrency"])
	}
	if cog["num_threads"] != float64(2) {
		t.Fatalf("cog.num_threads = %#v, want 2", cog["num_threads"])
	}
	if cog["leaf_retry_attempts"] != float64(3) {
		t.Fatalf("cog.leaf_retry_attempts = %#v, want 3", cog["leaf_retry_attempts"])
	}
}

func TestManagerRasterMosaicExecutorRejectsObjectStoreInPlace(t *testing.T) {
	workflowCalled := false
	systemServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path != "/api/v1/internal/engines/26" {
				t.Fatalf("unexpected system path: %s", r.URL.Path)
			}
			_, _ = w.Write([]byte(`{"id":26,"tenant_id":7,"name":"Source MinIO","engine_type":"minio","connection_info":{"endpoint":"http://source-minio:9000","access_key":"source-ak","secret_key":"source-sk","use_ssl":false},"lifecycle_state":"active"}`))
		}),
	}
	systemListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer systemServer.Close()
	go func() { _ = systemServer.Serve(systemListener) }()

	executor := NewManagerRasterMosaicExecutor(
		commonClient.NewSystemClientWithInternalKey("http://"+systemListener.Addr().String(), "internal-key"),
		recordingWorkflowLister{onList: func() {
			workflowCalled = true
		}},
		"http://manager.internal",
		"internal-key",
		0,
	)

	_, err = executor.BuildRasterMosaic(context.Background(), RasterMosaicExecutionRequest{
		Task:        &models.RasterMosaicTask{TenantID: 7, Name: "原地 mosaic"},
		ExecutionID: "mosaic-exec-in-place",
		Config: RasterMosaicExecutionConfig{
			Source: RasterMosaicSourceConfig{
				NodeLocator:    "addp://engine/26/path/source-bucket/rasters?type=directory",
				SourceEngineID: 26,
				Recursive:      true,
			},
			Placement: RasterMosaicPlacementConfig{Mode: "in_place"},
			Target: RasterMosaicTargetConfig{
				StorageLocator: "addp://engine/26/path/source-bucket/rasters?type=directory",
				TargetEngineID: 26,
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "in_place placement is not supported for object store") {
		t.Fatalf("BuildRasterMosaic() error = %v, want object store in_place rejection", err)
	}
	if workflowCalled {
		t.Fatal("workflow runtime should not be selected after object store in_place rejection")
	}
}

func writeBuildRasterMosaicOperatorList(w http.ResponseWriter, engineType string, executionModes []string) {
	w.Header().Set("Content-Type", "application/json")
	payload := map[string]interface{}{
		"status": "success",
		"operators": []map[string]interface{}{
			{
				"id":              "build_raster_mosaic",
				"name":            "build_raster_mosaic",
				"display_name":    "栅格 mosaic 数据集生成",
				"engine_type":     engineType,
				"type":            "raster",
				"category":        "格式转换",
				"category_path":   []string{"格式转换"},
				"description":     "生成栅格 mosaic 数据集",
				"execution_modes": executionModes,
				"effects":         []string{"read", "write"},
				"parameters": []map[string]interface{}{
					{"name": "access_plan", "type": "object", "required": true, "description": "GDAL 访问计划"},
					{"name": "placement", "type": "object", "required": true, "description": "生成位置模式"},
				},
				"output_ports": []map[string]interface{}{
					{"name": "default", "type": "object", "description": "mosaic 生成结果", "is_default": true},
				},
			},
		},
		"count": 1,
	}
	_ = json.NewEncoder(w).Encode(payload)
}

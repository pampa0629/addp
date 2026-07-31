package service

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"testing"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugins/objectstore"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/tilecache"
	"github.com/minio/minio-go/v7"
)

func TestManagerVectorTileCacheWorkflowExecutorUsesVSIS3ForObjectStoreSource(t *testing.T) {
	var capturedParams map[string]interface{}
	workflowServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/api/operators":
				writeVectorTileOperatorList(w, testRasterWorkflowEngineType, []string{"workflow", "direct"})
			case r.URL.Path == "/api/operators/vector_to_pmtiles/invoke":
				var payload struct {
					Params map[string]interface{} `json:"params"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatalf("decode workflow request: %v", err)
				}
				capturedParams = payload.Params
				_, _ = w.Write([]byte(`{"status":"success","execution_id":"py-pmtiles-1","execution_time_ms":92,"result":{"archive_format":"pmtiles","spec_version":3,"tile_format":"mvt","tile_compression":"gzip","header_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","archive_size_bytes":1024,"total_tiles":4,"cached_tiles":2,"tiles_total_estimate":4,"tiles_processed":4,"generated_tiles":2,"empty_tiles":2,"failed_tiles":0,"total_size_bytes":1024,"max_tile_size_bytes":180,"min_tile_size_bytes":76,"actual_max_zoom":10,"stop_reason":"workflow_ogr2ogr_pmtiles","generation_seconds":1.25,"extent":[110,20,120,30],"mvt_options":{"extent":8192,"buffer":160,"max_size":5000000,"publish_concurrency":8}}}`))
			default:
				t.Fatalf("unexpected workflow path: %s", r.URL.Path)
			}
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
			if r.URL.Path != "/api/v1/internal/engines/9" {
				t.Fatalf("unexpected system path: %s", r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":9,"tenant_id":7,"name":"Source S3","engine_type":"s3","connection_info":{"endpoint":"http://localhost:9002","access_key":"source-ak","secret_key":"source-sk","use_ssl":false},"lifecycle_state":"active"}`))
		}),
	}
	systemListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer systemServer.Close()
	go func() { _ = systemServer.Serve(systemListener) }()

	executor := NewManagerVectorTileCacheWorkflowExecutor(
		commonClient.NewSystemClientWithInternalKey("http://"+systemListener.Addr().String(), "internal-key"),
		recordingWorkflowLister{engines: []commonModels.Engine{{
			ID:         99,
			Name:       "Tenant Vector Workflow",
			EngineType: testRasterWorkflowEngineType,
			ConnectionInfo: commonModels.ConnectionInfo{
				"protocol": "http",
				"host":     "127.0.0.1",
				"port":     workflowPort,
			},
			LifecycleState: "active",
			Capabilities:   testRasterWorkflowCapabilities(t, testRasterWorkflowEngineType),
		}}},
		&recordingVectorTileObjectStore{exists: true},
		"http://manager:8081",
		"manager-internal-key",
		"http://minio:9000",
		"manager-ak",
		"manager-sk",
		false,
		"manager",
		0,
	)

	result, metadata, err := executor.GenerateVectorTileCache(context.Background(), WorkflowTileCacheRequest{
		Task:        &models.TileCacheTask{TenantID: 7, Name: "规划用地瓦片缓存"},
		ExecutionID: "mvt-exec-1",
		Identity: tileCacheTaskTargetIdentity{
			EngineID:        9,
			SourceKind:      "object",
			FullName:        "addp/gis/规划用地.shp",
			ItemID:          236,
			ItemFingerprint: commonModels.GenerateItemFingerprint(9, "addp/gis/规划用地.shp"),
			Locator:         "addp://engine/9/path/addp/gis/%E8%A7%84%E5%88%92%E7%94%A8%E5%9C%B0.shp?type=object&item_id=236",
		},
		StorageRef: tilecache.ObjectStorageRef(7, "fp-planning", "profile-planning"),
		Tile: commonModels.JSONMap{
			"format":      "mvt",
			"min_zoom":    9,
			"max_zoom":    10,
			"extent":      []float64{570841.0277, 3404864.0397, 598936.5143, 3434951.8803},
			"extent_srid": 4549,
			"source_srid": 4549,
		},
		Options: commonModels.JSONMap{"geometry_column": "geometry"},
	})
	if err != nil {
		t.Fatalf("GenerateVectorTileCache() error = %v", err)
	}
	if result.CachedTiles != 2 || result.StopReason != "workflow_ogr2ogr_pmtiles" {
		t.Fatalf("result = %+v, want workflow facts", result)
	}
	if metadata["operator"] != "vector_to_pmtiles" || metadata["mode"] != "direct" || metadata["header_hash"] == nil {
		t.Fatalf("metadata = %#v, want direct vector workflow metadata", metadata)
	}
	mvtOptions, _ := asJSONMap(metadata["mvt_options"])
	if intFromTileCacheConfig(mvtOptions["extent"], 0) != 8192 ||
		intFromTileCacheConfig(mvtOptions["max_size"], 0) != 5000000 ||
		intFromTileCacheConfig(mvtOptions["publish_concurrency"], 0) != 8 {
		t.Fatalf("metadata.mvt_options = %#v, want workflow MVT generation options", mvtOptions)
	}

	accessPlan := capturedParams["access_plan"].(map[string]interface{})
	sourcePlan := accessPlan["source"].(map[string]interface{})
	targetPlan := accessPlan["target"].(map[string]interface{})
	progress := accessPlan["progress_callback"].(map[string]interface{})
	if sourcePlan["root_uri"] != "/vsis3/addp/gis/规划用地.shp" {
		t.Fatalf("source root_uri = %#v, want /vsis3/addp/gis/规划用地.shp", sourcePlan["root_uri"])
	}
	sourceEnv := sourcePlan["gdal_env"].(map[string]interface{})
	if sourceEnv["AWS_S3_ENDPOINT"] != objectstore.NormalizeEndpoint("localhost:9002") ||
		sourceEnv["AWS_ACCESS_KEY_ID"] != "source-ak" ||
		sourceEnv["AWS_SECRET_ACCESS_KEY"] != "source-sk" ||
		sourceEnv["AWS_VIRTUAL_HOSTING"] != "FALSE" ||
		sourceEnv["AWS_HTTPS"] != "NO" {
		t.Fatalf("source gdal_env = %#v, want source object store credentials", sourceEnv)
	}
	sourceMetadata := sourcePlan["metadata"].(map[string]interface{})
	if sourceMetadata["access_method"] != "vsis3_object_storage" || sourceMetadata["engine_type"] != "s3" {
		t.Fatalf("source metadata = %#v, want vsis3 object storage audit", sourceMetadata)
	}
	if strings.HasPrefix(sourcePlan["root_uri"].(string), "/vsicurl/") {
		t.Fatalf("source root_uri must not use single-file presigned URL: %#v", sourcePlan["root_uri"])
	}
	if targetPlan["archive_uri"] != "/vsis3/manager/tenant_7/vector-tile-cache/fp-planning/profile-planning.pmtiles" {
		t.Fatalf("target archive_uri = %#v", targetPlan["archive_uri"])
	}
	targetEnv := targetPlan["gdal_env"].(map[string]interface{})
	if targetEnv["AWS_S3_ENDPOINT"] != "minio:9000" || targetEnv["AWS_ACCESS_KEY_ID"] != "manager-ak" || targetEnv["AWS_SECRET_ACCESS_KEY"] != "manager-sk" {
		t.Fatalf("target gdal_env = %#v, want manager infra MinIO credentials", targetEnv)
	}
	if progress["endpoint"] != "http://manager:8081/api/v1/manager/internal/executions/mvt-exec-1/events" ||
		progress["tenant_id"] != float64(7) ||
		progress["execution_id"] != "mvt-exec-1" ||
		progress["internal_api_key"] != "manager-internal-key" {
		t.Fatalf("progress callback = %#v, want Manager execution event callback", progress)
	}
	tile := capturedParams["tile"].(map[string]interface{})
	if tile["source_srs"] != "EPSG:4549" {
		t.Fatalf("tile = %#v, want source_srs from source_srid", tile)
	}
}

func writeVectorTileOperatorList(w http.ResponseWriter, engineType string, executionModes []string) {
	w.Header().Set("Content-Type", "application/json")
	payload := map[string]interface{}{
		"status": "success",
		"operators": []map[string]interface{}{
			{
				"id":              "vector_to_pmtiles",
				"name":            "vector_to_pmtiles",
				"display_name":    "向量 PMTiles 瓦片缓存",
				"engine_type":     engineType,
				"type":            "vector",
				"category":        "瓦片缓存",
				"category_path":   []string{"瓦片缓存"},
				"description":     "从空间数据生成 MVT 瓦片缓存",
				"execution_modes": executionModes,
				"effects":         []string{"read", "write"},
				"parameters": []map[string]interface{}{
					{"name": "access_plan", "type": "object", "required": true},
					{"name": "tile", "type": "object", "required": true},
					{"name": "options", "type": "object", "required": false},
				},
				"output_ports": []map[string]interface{}{
					{"name": "default", "type": "object", "description": "MVT 生成结果", "is_default": true},
				},
			},
		},
		"count": 1,
	}
	_ = json.NewEncoder(w).Encode(payload)
}

type recordingVectorTileObjectStore struct {
	exists bool
}

func (s *recordingVectorTileObjectStore) BucketExists(context.Context, string) (bool, error) {
	return s.exists, nil
}

func (s *recordingVectorTileObjectStore) MakeBucket(context.Context, string, minio.MakeBucketOptions) error {
	s.exists = true
	return nil
}

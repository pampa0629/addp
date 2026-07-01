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
	"github.com/addp/common/resourcetree"
	"github.com/addp/manager/internal/models"
	"github.com/minio/minio-go/v7"
)

const testModel3DWorkflowEngineType = "tenant_model3d_workflow"

func TestModel3DTilesLocalRootUsesMountedFileEngine(t *testing.T) {
	loc, err := resourcetree.ParseURI("addp://engine/26/path/models/osgb?type=item&item_id=77")
	if err != nil {
		t.Fatalf("parse locator: %v", err)
	}
	root, env, access, err := model3DTilesLocalRoot(&commonModels.Engine{
		EngineType: "nfs",
		ConnectionInfo: commonModels.ConnectionInfo{
			"mount_path": "/mnt/addp-nfs",
		},
	}, loc, "white_tower_3dtiles")
	if err != nil {
		t.Fatalf("model3DTilesLocalRoot() error = %v", err)
	}
	if root != "/mnt/addp-nfs/models/osgb/white_tower_3dtiles" {
		t.Fatalf("root = %q, want mounted dataset path", root)
	}
	if len(env) != 0 {
		t.Fatalf("env = %#v, want empty local env", env)
	}
	if access["access_method"] != "mounted_path" || access["engine_type"] != "nfs" {
		t.Fatalf("access = %#v, want mounted path facts", access)
	}
}

func TestModel3DTilesLocalRootRejectsObjectStore(t *testing.T) {
	loc, err := resourcetree.ParseURI("addp://engine/26/path/bucket/models/osgb?type=item&item_id=77")
	if err != nil {
		t.Fatalf("parse locator: %v", err)
	}
	_, _, _, err = model3DTilesLocalRoot(&commonModels.Engine{EngineType: "minio"}, loc, "")
	if err == nil {
		t.Fatal("model3DTilesLocalRoot() error is nil, want object store staging rejection")
	}
	if got := err.Error(); got != "model 3d tiles generation first phase supports nfs/localfs only; object store requires staging support" {
		t.Fatalf("error = %q, want object store staging rejection", got)
	}
}

func TestManagerModel3DTilesExecutorStagesObjectStoreSourceAndPublishesTarget(t *testing.T) {
	var capturedParams map[string]interface{}
	workflowServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/api/operators":
				writeModel3DOperatorList(w, testModel3DWorkflowEngineType, []string{"direct"})
			case r.URL.Path == "/api/operators/osgb_scene_to_3dtiles/invoke":
				var payload struct {
					Params map[string]interface{} `json:"params"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatalf("decode workflow request: %v", err)
				}
				capturedParams = payload.Params
				_, _ = w.Write([]byte(`{"status":"success","execution_id":"model3d-1","execution_time_ms":456,"result":{"tileset_locator":"addp://engine/31/path/target-bucket/tiles/site_a?type=directory","tileset_ref":"tileset.json","tile_count":2,"publish":{"uploaded_files":3,"uploaded_bytes":128}}}`))
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
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/api/v1/internal/engines/26":
				_, _ = w.Write([]byte(`{"id":26,"tenant_id":7,"name":"Source MinIO","engine_type":"minio","connection_info":{"endpoint":"http://source-minio:9000","access_key":"source-ak","secret_key":"source-sk","use_ssl":false},"is_active":true}`))
			case "/api/v1/internal/engines/31":
				_, _ = w.Write([]byte(`{"id":31,"tenant_id":7,"name":"Target MinIO","engine_type":"minio","connection_info":{"endpoint":"http://target-minio:9000","access_key":"target-ak","secret_key":"target-sk","use_ssl":false},"is_active":true}`))
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

	executor := NewManagerModel3DTilesExecutor(
		commonClient.NewSystemClientWithInternalKey("http://"+systemListener.Addr().String(), "internal-key"),
		recordingWorkflowLister{engines: []commonModels.Engine{{
			ID:         99,
			Name:       "Tenant Model3D Workflow",
			EngineType: testModel3DWorkflowEngineType,
			ConnectionInfo: commonModels.ConnectionInfo{
				"protocol": "http",
				"host":     "127.0.0.1",
				"port":     workflowPort,
			},
			IsActive:     true,
			Capabilities: testRasterWorkflowCapabilities(t, testModel3DWorkflowEngineType),
		}}},
		0,
	)

	result, err := executor.BuildModel3DTiles(context.Background(), Model3DTilesExecutionRequest{
		Task:        &models.Model3DTilesTask{TenantID: 7, Name: "生成 3D Tiles"},
		ExecutionID: "model3d-exec-1",
		Config: Model3DTilesExecutionConfig{
			Source: Model3DTilesSourceConfig{
				ItemLocator:    "addp://engine/26/path/source-bucket/models/site_a?type=item&item_id=77",
				SourceEngineID: 26,
				Format:         "osgb_scene",
			},
			Target: Model3DTilesTargetConfig{
				StorageLocator: "addp://engine/31/path/target-bucket/tiles?type=directory",
				TargetEngineID: 31,
				DatasetName:    "site_a",
			},
			Tiles: Model3DTilesTilesConfig{Format: "3dtiles"},
		},
	})
	if err != nil {
		t.Fatalf("BuildModel3DTiles() error = %v", err)
	}
	if result.TilesetLocator != "addp://engine/31/path/target-bucket/tiles/site_a?type=directory" || result.TileCount != 2 {
		t.Fatalf("unexpected result: %+v", result)
	}
	accessPlan := capturedParams["access_plan"].(map[string]interface{})
	sourcePlan := accessPlan["source"].(map[string]interface{})
	targetPlan := accessPlan["target"].(map[string]interface{})
	stage := sourcePlan["stage"].(map[string]interface{})
	publish := targetPlan["publish"].(map[string]interface{})
	if sourcePlan["root_uri"] != "" {
		t.Fatalf("source root_uri = %#v, want empty for object store staging", sourcePlan["root_uri"])
	}
	if stage["method"] != "object_store" || stage["bucket"] != "source-bucket" || stage["prefix"] != "models/site_a" {
		t.Fatalf("stage = %#v, want object store source", stage)
	}
	if stage["endpoint"] != "source-minio:9000" || stage["access_key"] != "source-ak" || stage["secret_key"] != "source-sk" {
		t.Fatalf("stage credentials not passed correctly: %#v", stage)
	}
	if targetPlan["dataset_root_uri"] != "" {
		t.Fatalf("target dataset_root_uri = %#v, want empty for object store publish", targetPlan["dataset_root_uri"])
	}
	if publish["method"] != "object_store" || publish["bucket"] != "target-bucket" || publish["prefix"] != "tiles/site_a" {
		t.Fatalf("publish = %#v, want object store target", publish)
	}
	if publish["endpoint"] != "target-minio:9000" || publish["access_key"] != "target-ak" || publish["secret_key"] != "target-sk" {
		t.Fatalf("publish credentials not passed correctly: %#v", publish)
	}
	metadataBytes, err := json.Marshal(result.Metadata)
	if err != nil {
		t.Fatalf("marshal result metadata: %v", err)
	}
	metadataText := strings.ToLower(strings.TrimSpace(string(metadataBytes)))
	if strings.Contains(metadataText, "target-sk") || strings.Contains(metadataText, "target-ak") ||
		strings.Contains(metadataText, "source-sk") || strings.Contains(metadataText, "source-ak") {
		t.Fatalf("result metadata leaked object store credentials: %s", metadataText)
	}
}

func TestManagerModel3DQuickViewExecutorPublishesGLBFromWorkflowRuntime(t *testing.T) {
	var capturedParams map[string]interface{}
	workflowServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/api/operators":
				writeModel3DOperatorList(w, testModel3DWorkflowEngineType, []string{"direct"})
			case r.URL.Path == "/api/operators/osgb_to_glb/invoke":
				var payload struct {
					Params map[string]interface{} `json:"params"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatalf("decode workflow request: %v", err)
				}
				capturedParams = payload.Params
				_, _ = w.Write([]byte(`{"status":"success","execution_id":"model3d-glb-1","execution_time_ms":123,"result":{"glb_uri":"s3://manager/model3d/preview.glb","glb_ref":"model3d/preview.glb","size_bytes":3,"publish":{"uploaded_files":1,"uploaded_bytes":3}}}`))
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
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/api/v1/internal/engines/26":
				_, _ = w.Write([]byte(`{"id":26,"tenant_id":7,"name":"Business NFS","engine_type":"nfs","connection_info":{"export_path":"/mnt/addp-nfs"},"is_active":true}`))
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

	objectStore := &recordingModel3DQuickViewObjectStore{statSize: 3}
	executor := NewManagerModel3DQuickViewExecutor(
		commonClient.NewSystemClientWithInternalKey("http://"+systemListener.Addr().String(), "internal-key"),
		recordingWorkflowLister{engines: []commonModels.Engine{{
			ID:         99,
			Name:       "Tenant Model3D Workflow",
			EngineType: testModel3DWorkflowEngineType,
			ConnectionInfo: commonModels.ConnectionInfo{
				"protocol": "http",
				"host":     "127.0.0.1",
				"port":     workflowPort,
			},
			IsActive:     true,
			Capabilities: testRasterWorkflowCapabilities(t, testModel3DWorkflowEngineType),
		}}},
		objectStore,
		"minio:9000",
		"minio-ak",
		"minio-sk",
		false,
		"manager",
		0,
	)

	result, err := executor.BuildModel3DQuickView(context.Background(), Model3DQuickViewExecutionRequest{
		Task: &models.Model3DQuickViewTask{TenantID: 7, Name: "生成 GLB"},
		Config: Model3DQuickViewExecutionConfig{
			Source: Model3DQuickViewSourceConfig{
				ItemLocator:     "addp://engine/26/path/3d/single-osgb/tile.osgb?type=file&item_id=77",
				SourceEngineID:  26,
				ItemFingerprint: "fp-1",
				Format:          "osgb",
			},
			Result: Model3DQuickViewResultConfig{
				StorageRef: `{"type":"object","provider":"addp_object_storage","bucket":"manager","object":"model3d/preview.glb"}`,
				FileName:   "preview.glb",
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildModel3DQuickView() error = %v", err)
	}
	if result.StorageRef != `{"type":"object","provider":"addp_object_storage","bucket":"manager","object":"model3d/preview.glb"}` || result.SizeBytes != 3 {
		t.Fatalf("unexpected result: %+v", result)
	}
	accessPlan := capturedParams["access_plan"].(map[string]interface{})
	sourcePlan := accessPlan["source"].(map[string]interface{})
	targetPlan := accessPlan["target"].(map[string]interface{})
	publish := targetPlan["publish"].(map[string]interface{})
	if sourcePlan["local_path"] != "/mnt/addp-nfs/3d/single-osgb/tile.osgb" {
		t.Fatalf("source local_path = %#v", sourcePlan["local_path"])
	}
	if _, exists := targetPlan["local_path"]; exists {
		t.Fatalf("target local_path should not be sent to remote model3d workflow: %#v", targetPlan)
	}
	if targetPlan["file_name"] != "preview.glb" {
		t.Fatalf("target file_name = %#v", targetPlan["file_name"])
	}
	if publish["method"] != "object_store" || publish["endpoint"] != "minio:9000" ||
		publish["bucket"] != "manager" || publish["object"] != "model3d/preview.glb" ||
		publish["access_key"] != "minio-ak" || publish["secret_key"] != "minio-sk" {
		t.Fatalf("publish = %#v, want Manager artifact MinIO publish plan", publish)
	}
	if objectStore.statBucket != "manager" || objectStore.statObject != "model3d/preview.glb" {
		t.Fatalf("object stat = %s/%s, want manager/model3d/preview.glb", objectStore.statBucket, objectStore.statObject)
	}
}

func TestManagerModel3DQuickViewExecutorDispatchesGLTFToGLBOperator(t *testing.T) {
	var capturedPath string
	workflowServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/api/operators":
				writeModel3DOperatorList(w, testModel3DWorkflowEngineType, []string{"direct"})
			case r.URL.Path == "/api/operators/gltf_to_glb/invoke":
				capturedPath = r.URL.Path
				var payload struct {
					Params map[string]interface{} `json:"params"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatalf("decode workflow request: %v", err)
				}
				accessPlan := payload.Params["access_plan"].(map[string]interface{})
				sourcePlan := accessPlan["source"].(map[string]interface{})
				if sourcePlan["local_path"] != "/mnt/addp-nfs/models/scene.gltf" {
					t.Fatalf("source local_path = %#v", sourcePlan["local_path"])
				}
				_, _ = w.Write([]byte(`{"status":"success","execution_id":"model3d-glb-gltf","result":{"glb_uri":"s3://manager/model3d/scene.glb","glb_ref":"model3d/scene.glb","size_bytes":3}}`))
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
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/api/v1/internal/engines/26":
				_, _ = w.Write([]byte(`{"id":26,"tenant_id":7,"name":"Business NFS","engine_type":"nfs","connection_info":{"export_path":"/mnt/addp-nfs"},"is_active":true}`))
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

	executor := NewManagerModel3DQuickViewExecutor(
		commonClient.NewSystemClientWithInternalKey("http://"+systemListener.Addr().String(), "internal-key"),
		recordingWorkflowLister{engines: []commonModels.Engine{{
			ID:         99,
			Name:       "Tenant Model3D Workflow",
			EngineType: testModel3DWorkflowEngineType,
			ConnectionInfo: commonModels.ConnectionInfo{
				"protocol": "http",
				"host":     "127.0.0.1",
				"port":     workflowPort,
			},
			IsActive:     true,
			Capabilities: testRasterWorkflowCapabilities(t, testModel3DWorkflowEngineType),
		}}},
		&recordingModel3DQuickViewObjectStore{statSize: 3},
		"minio:9000",
		"minio-ak",
		"minio-sk",
		false,
		"manager",
		0,
	)

	result, err := executor.BuildModel3DQuickView(context.Background(), Model3DQuickViewExecutionRequest{
		Task: &models.Model3DQuickViewTask{TenantID: 7, Name: "生成 glTF GLB"},
		Config: Model3DQuickViewExecutionConfig{
			Source: Model3DQuickViewSourceConfig{
				ItemLocator:     "addp://engine/26/path/models/scene.gltf?type=file&item_id=77",
				SourceEngineID:  26,
				ItemFingerprint: "fp-gltf",
				Format:          "gltf",
			},
			Result: Model3DQuickViewResultConfig{
				StorageRef: `{"type":"object","provider":"addp_object_storage","bucket":"manager","object":"model3d/scene.glb"}`,
				FileName:   "scene.glb",
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildModel3DQuickView() error = %v", err)
	}
	if capturedPath != "/api/operators/gltf_to_glb/invoke" {
		t.Fatalf("captured workflow path = %q, want gltf_to_glb invoke", capturedPath)
	}
	metadataSource := result.Metadata["source"].(commonModels.JSONMap)
	if metadataSource["format"] != "gltf" {
		t.Fatalf("metadata source format = %#v, want gltf", metadataSource["format"])
	}
}

func TestModel3DQuickViewOperatorForFormat(t *testing.T) {
	tests := []struct {
		formatName string
		operator   string
	}{
		{formatName: "osgb", operator: "osgb_to_glb"},
		{formatName: "gltf", operator: "gltf_to_glb"},
		{formatName: "fbx", operator: "fbx_to_glb"},
		{formatName: "obj", operator: "obj_to_glb"},
		{formatName: "stl", operator: "stl_to_glb"},
	}
	for _, tt := range tests {
		t.Run(tt.formatName, func(t *testing.T) {
			operator, normalized, err := model3DQuickViewOperatorForFormat(tt.formatName)
			if err != nil {
				t.Fatalf("model3DQuickViewOperatorForFormat() error = %v", err)
			}
			if operator != tt.operator || normalized != tt.formatName {
				t.Fatalf("operator=%q normalized=%q, want %q/%q", operator, normalized, tt.operator, tt.formatName)
			}
		})
	}
}

type recordingModel3DQuickViewObjectStore struct {
	statSize   int64
	statBucket string
	statObject string
}

func (s *recordingModel3DQuickViewObjectStore) BucketExists(context.Context, string) (bool, error) {
	return true, nil
}

func (s *recordingModel3DQuickViewObjectStore) MakeBucket(context.Context, string, minio.MakeBucketOptions) error {
	return nil
}

func (s *recordingModel3DQuickViewObjectStore) StatObject(_ context.Context, bucket string, object string, _ minio.StatObjectOptions) (minio.ObjectInfo, error) {
	s.statBucket = bucket
	s.statObject = object
	return minio.ObjectInfo{Size: s.statSize}, nil
}

func writeModel3DOperatorList(w http.ResponseWriter, engineType string, executionModes []string) {
	payload := map[string]interface{}{
		"status": "success",
		"operators": []map[string]interface{}{
			{
				"id":              "osgb_to_glb",
				"name":            "osgb_to_glb",
				"display_name":    "OSGB 转 GLB",
				"engine_type":     engineType,
				"category":        "三维模型转换",
				"category_path":   []string{"三维模型转换"},
				"description":     "生成 GLB",
				"execution_modes": executionModes,
				"parameters": []map[string]interface{}{
					{"name": "access_plan", "type": "object", "required": true, "description": "访问计划"},
				},
				"output_ports": []map[string]interface{}{
					{"name": "default", "type": "object", "description": "GLB 生成结果", "is_default": true},
				},
			},
			{
				"id":              "gltf_to_glb",
				"name":            "gltf_to_glb",
				"display_name":    "glTF 转 GLB",
				"engine_type":     engineType,
				"category":        "三维模型转换",
				"category_path":   []string{"三维模型转换"},
				"description":     "生成 GLB",
				"execution_modes": executionModes,
				"parameters": []map[string]interface{}{
					{"name": "access_plan", "type": "object", "required": true, "description": "访问计划"},
				},
				"output_ports": []map[string]interface{}{
					{"name": "default", "type": "object", "description": "GLB 生成结果", "is_default": true},
				},
			},
			{
				"id":              "fbx_to_glb",
				"name":            "fbx_to_glb",
				"display_name":    "FBX 转 GLB",
				"engine_type":     engineType,
				"category":        "三维模型转换",
				"category_path":   []string{"三维模型转换"},
				"description":     "生成 GLB",
				"execution_modes": executionModes,
				"parameters": []map[string]interface{}{
					{"name": "access_plan", "type": "object", "required": true, "description": "访问计划"},
				},
				"output_ports": []map[string]interface{}{
					{"name": "default", "type": "object", "description": "GLB 生成结果", "is_default": true},
				},
			},
			{
				"id":              "obj_to_glb",
				"name":            "obj_to_glb",
				"display_name":    "OBJ 转 GLB",
				"engine_type":     engineType,
				"category":        "三维模型转换",
				"category_path":   []string{"三维模型转换"},
				"description":     "生成 GLB",
				"execution_modes": executionModes,
				"parameters": []map[string]interface{}{
					{"name": "access_plan", "type": "object", "required": true, "description": "访问计划"},
				},
				"output_ports": []map[string]interface{}{
					{"name": "default", "type": "object", "description": "GLB 生成结果", "is_default": true},
				},
			},
			{
				"id":              "stl_to_glb",
				"name":            "stl_to_glb",
				"display_name":    "STL 转 GLB",
				"engine_type":     engineType,
				"category":        "三维模型转换",
				"category_path":   []string{"三维模型转换"},
				"description":     "生成 GLB",
				"execution_modes": executionModes,
				"parameters": []map[string]interface{}{
					{"name": "access_plan", "type": "object", "required": true, "description": "访问计划"},
				},
				"output_ports": []map[string]interface{}{
					{"name": "default", "type": "object", "description": "GLB 生成结果", "is_default": true},
				},
			},
			{
				"id":              "osgb_scene_to_3dtiles",
				"name":            "osgb_scene_to_3dtiles",
				"display_name":    "OSGB Scene 转 3D Tiles",
				"engine_type":     engineType,
				"category":        "三维模型转换",
				"category_path":   []string{"三维模型转换"},
				"description":     "生成 3D Tiles",
				"execution_modes": executionModes,
				"parameters": []map[string]interface{}{
					{"name": "access_plan", "type": "object", "required": true, "description": "访问计划"},
				},
				"output_ports": []map[string]interface{}{
					{"name": "default", "type": "object", "description": "3D Tiles 生成结果", "is_default": true},
				},
			},
		},
		"count": 4,
	}
	_ = json.NewEncoder(w).Encode(payload)
}

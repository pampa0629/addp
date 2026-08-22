package service

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"testing"

	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/minio/minio-go/v7"
)

const testModel3DWorkflowEngineType = "tenant_model3d_workflow"

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
				_, _ = w.Write([]byte(`{"status":"success","execution_id":"model3d-1","execution_time_ms":456,"result":{"manifest_ref":"tileset.json","tile_count":2}}`))
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
			case "/api/v1/system/engines/26":
				_, _ = w.Write([]byte(`{"id":26,"tenant_id":7,"name":"Source MinIO","engine_type":"minio","connection_info":{"endpoint":"http://source-minio:9000","access_key":"source-ak","secret_key":"source-sk","use_ssl":false},"lifecycle_state":"active"}`))
			case "/api/v1/system/engines/31":
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

	executor := NewManagerModel3DTilesExecutor(
		newTestSystemClient("http://"+systemListener.Addr().String()),
		recordingWorkflowLister{engines: []commonModels.Engine{{
			ID:         99,
			Name:       "Tenant Model3D Workflow",
			EngineType: testModel3DWorkflowEngineType,
			ConnectionInfo: commonModels.ConnectionInfo{
				"protocol": "http",
				"host":     "127.0.0.1",
				"port":     workflowPort,
			},
			LifecycleState:   "active",
			ConnectionStatus: commonModels.EngineConnectionOnline,
			Capabilities:     testRasterWorkflowCapabilities(t, testModel3DWorkflowEngineType),
		}}},
		&fakeModel3DTilesObjectStore{objects: []minio.ObjectInfo{{Key: "tenant_7/model3d-tiles/fp/3d_tiles/tileset.json", Size: 64}, {Key: "tenant_7/model3d-tiles/fp/3d_tiles/Data/tile.b3dm", Size: 128}}},
		"infra-minio:9000", "infra-ak", "infra-sk", false, "manager",
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
			TargetFormat: "3d_tiles",
			Result:       Model3DTilesResultConfig{StorageRef: `{"type":"object","provider":"addp_object_storage","bucket":"manager","object":"tenant_7/model3d-tiles/fp/3d_tiles"}`},
		},
	})
	if err != nil {
		t.Fatalf("BuildModel3DTiles() error = %v", err)
	}
	if result.ManifestRef != "tileset.json" || result.FileCount != 2 || result.SizeBytes != 192 {
		t.Fatalf("unexpected result: %+v", result)
	}
	accessPlan := capturedParams["access_plan"].(map[string]interface{})
	if accessPlan["schema_version"] != "addp.workflow.access-plan/v1" {
		t.Fatalf("schema_version = %#v", accessPlan["schema_version"])
	}
	sourcePlan := accessPlan["source"].(map[string]interface{})
	targetPlan := accessPlan["target"].(map[string]interface{})
	sourceAccess := sourcePlan["access"].(map[string]interface{})
	targetAccess := targetPlan["access"].(map[string]interface{})
	if sourcePlan["kind"] != "directory" || sourcePlan["format"] != "osgb_scene" {
		t.Fatalf("source plan = %#v, want OSGB scene directory", sourcePlan)
	}
	if sourceAccess["method"] != "object_store" || sourceAccess["bucket"] != "source-bucket" || sourceAccess["prefix"] != "models/site_a" {
		t.Fatalf("source access = %#v, want object store source", sourceAccess)
	}
	if sourceAccess["endpoint"] != "source-minio:9000" || sourceAccess["access_key"] != "source-ak" || sourceAccess["secret_key"] != "source-sk" {
		t.Fatalf("source credentials not passed correctly: %#v", sourceAccess)
	}
	if targetPlan["kind"] != "directory" || targetPlan["format"] != "3dtiles" || targetPlan["write_mode"] != "replace" {
		t.Fatalf("target plan = %#v, want replace 3D Tiles dataset", targetPlan)
	}
	if targetAccess["method"] != "object_store" || targetAccess["bucket"] != "manager" || targetAccess["prefix"] != "tenant_7/model3d-tiles/fp/3d_tiles" {
		t.Fatalf("target access = %#v, want object store target", targetAccess)
	}
	if targetAccess["endpoint"] != "infra-minio:9000" || targetAccess["access_key"] != "infra-ak" || targetAccess["secret_key"] != "infra-sk" {
		t.Fatalf("target credentials not passed correctly: %#v", targetAccess)
	}
	metadataBytes, err := json.Marshal(result.Metadata)
	if err != nil {
		t.Fatalf("marshal result metadata: %v", err)
	}
	metadataText := strings.ToLower(strings.TrimSpace(string(metadataBytes)))
	if strings.Contains(metadataText, "infra-sk") || strings.Contains(metadataText, "infra-ak") ||
		strings.Contains(metadataText, "source-sk") || strings.Contains(metadataText, "source-ak") {
		t.Fatalf("result metadata leaked object store credentials: %s", metadataText)
	}
}

func TestManagerModel3DTilesExecutorRejectsIncompleteS3MArtifact(t *testing.T) {
	workflowServer := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/operators":
			writeModel3DOperatorList(w, testModel3DWorkflowEngineType, []string{"direct"})
		case r.URL.Path == "/api/operators/osgb_scene_to_s3m/invoke":
			_, _ = w.Write([]byte(`{"status":"success","execution_id":"s3m-1","result":{"manifest_ref":"config/scene.scp","root_tile_count":2,"file_count":1}}`))
		default:
			t.Fatalf("unexpected workflow path: %s", r.URL.Path)
		}
	})}
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

	systemServer := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/system/engines/26" {
			t.Fatalf("unexpected system path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":26,"tenant_id":7,"name":"Business NFS","engine_type":"nfs","connection_info":{"export_path":"/mnt/addp-nfs"},"lifecycle_state":"active"}`))
	})}
	systemListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer systemServer.Close()
	go func() { _ = systemServer.Serve(systemListener) }()

	executor := NewManagerModel3DTilesExecutor(
		newTestSystemClient("http://"+systemListener.Addr().String()),
		recordingWorkflowLister{engines: []commonModels.Engine{{
			ID: 99, Name: "SuperMap Workflow", EngineType: testModel3DWorkflowEngineType,
			ConnectionInfo: commonModels.ConnectionInfo{"protocol": "http", "host": "127.0.0.1", "port": workflowPort},
			LifecycleState: "active", ConnectionStatus: commonModels.EngineConnectionOnline, Capabilities: testRasterWorkflowCapabilities(t, testModel3DWorkflowEngineType),
		}}},
		&fakeModel3DTilesObjectStore{objects: []minio.ObjectInfo{{Key: "tenant_7/model3d-tiles/fp/s3m/config/scene.scp", Size: 64}}},
		"infra-minio:9000", "infra-ak", "infra-sk", false, "manager", 0,
	)

	_, err = executor.BuildModel3DTiles(context.Background(), Model3DTilesExecutionRequest{
		Task: &models.Model3DTilesTask{TenantID: 7, Name: "生成 S3M"}, ExecutionID: "s3m-exec-1",
		Config: Model3DTilesExecutionConfig{
			Source:       Model3DTilesSourceConfig{ItemLocator: "addp://engine/26/path/3d/baita?type=file&item_id=77", SourceEngineID: 26, Format: "osgb_scene"},
			TargetFormat: "s3m",
			Result:       Model3DTilesResultConfig{StorageRef: `{"type":"object","provider":"addp_object_storage","bucket":"manager","object":"tenant_7/model3d-tiles/fp/s3m"}`},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "incomplete S3M artifact") {
		t.Fatalf("BuildModel3DTiles() error = %v, want incomplete S3M artifact", err)
	}
}

type fakeModel3DTilesObjectStore struct{ objects []minio.ObjectInfo }

func (f *fakeModel3DTilesObjectStore) BucketExists(context.Context, string) (bool, error) {
	return true, nil
}
func (f *fakeModel3DTilesObjectStore) MakeBucket(context.Context, string, minio.MakeBucketOptions) error {
	return nil
}
func (f *fakeModel3DTilesObjectStore) ListObjects(_ context.Context, _ string, _ minio.ListObjectsOptions) <-chan minio.ObjectInfo {
	ch := make(chan minio.ObjectInfo, len(f.objects))
	for _, item := range f.objects {
		ch <- item
	}
	close(ch)
	return ch
}
func (f *fakeModel3DTilesObjectStore) RemoveObject(context.Context, string, string, minio.RemoveObjectOptions) error {
	return nil
}
func (f *fakeModel3DTilesObjectStore) StatObject(_ context.Context, _ string, object string, _ minio.StatObjectOptions) (minio.ObjectInfo, error) {
	for _, item := range f.objects {
		if item.Key == object {
			return item, nil
		}
	}
	return minio.ObjectInfo{}, nil
}

func TestManagerModel3DGLBExecutorPublishesGLBFromWorkflowRuntime(t *testing.T) {
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
			case "/api/v1/system/engines/26":
				_, _ = w.Write([]byte(`{"id":26,"tenant_id":7,"name":"Business NFS","engine_type":"nfs","connection_info":{"export_path":"/mnt/addp-nfs"},"lifecycle_state":"active"}`))
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

	objectStore := &recordingModel3DGLBObjectStore{statSize: 3}
	executor := NewManagerModel3DGLBExecutor(
		newTestSystemClient("http://"+systemListener.Addr().String()),
		recordingWorkflowLister{engines: []commonModels.Engine{{
			ID:         99,
			Name:       "Tenant Model3D Workflow",
			EngineType: testModel3DWorkflowEngineType,
			ConnectionInfo: commonModels.ConnectionInfo{
				"protocol": "http",
				"host":     "127.0.0.1",
				"port":     workflowPort,
			},
			LifecycleState:   "active",
			ConnectionStatus: commonModels.EngineConnectionOnline,
			Capabilities:     testRasterWorkflowCapabilities(t, testModel3DWorkflowEngineType),
		}}},
		objectStore,
		"minio:9000",
		"minio-ak",
		"minio-sk",
		false,
		"manager",
		0,
	)

	result, err := executor.BuildModel3DGLB(context.Background(), Model3DGLBExecutionRequest{
		Task: &models.Model3DGLBTask{TenantID: 7, Name: "生成 GLB"},
		Config: Model3DGLBExecutionConfig{
			Source: Model3DGLBSourceConfig{
				ItemLocator:     "addp://engine/26/path/3d/single-osgb/tile.osgb?type=file&item_id=77",
				SourceEngineID:  26,
				ItemFingerprint: "fp-1",
				Format:          "osgb",
			},
			Result: Model3DGLBResultConfig{
				StorageRef: `{"type":"object","provider":"addp_object_storage","bucket":"manager","object":"model3d/preview.glb"}`,
				FileName:   "preview.glb",
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildModel3DGLB() error = %v", err)
	}
	if result.StorageRef != `{"type":"object","provider":"addp_object_storage","bucket":"manager","object":"model3d/preview.glb"}` || result.SizeBytes != 3 {
		t.Fatalf("unexpected result: %+v", result)
	}
	accessPlan := capturedParams["access_plan"].(map[string]interface{})
	sourcePlan := accessPlan["source"].(map[string]interface{})
	targetPlan := accessPlan["target"].(map[string]interface{})
	sourceAccess := sourcePlan["access"].(map[string]interface{})
	targetAccess := targetPlan["access"].(map[string]interface{})
	if sourceAccess["method"] != "mounted_path" || sourceAccess["path"] != "/mnt/addp-nfs/3d/single-osgb/tile.osgb" {
		t.Fatalf("source access = %#v", sourceAccess)
	}
	if targetPlan["name"] != "preview.glb" || targetPlan["write_mode"] != "replace" || targetPlan["content_type"] != "model/gltf-binary" {
		t.Fatalf("target plan = %#v", targetPlan)
	}
	if targetAccess["method"] != "object_store" || targetAccess["endpoint"] != "minio:9000" ||
		targetAccess["bucket"] != "manager" || targetAccess["object"] != "model3d/preview.glb" ||
		targetAccess["access_key"] != "minio-ak" || targetAccess["secret_key"] != "minio-sk" {
		t.Fatalf("target access = %#v, want Manager artifact MinIO plan", targetAccess)
	}
	if objectStore.statBucket != "manager" || objectStore.statObject != "model3d/preview.glb" {
		t.Fatalf("object stat = %s/%s, want manager/model3d/preview.glb", objectStore.statBucket, objectStore.statObject)
	}
}

func TestManagerModel3DGLBExecutorDispatchesGLTFToGLBOperator(t *testing.T) {
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
				sourceAccess := sourcePlan["access"].(map[string]interface{})
				if sourcePlan["kind"] != "directory" || sourcePlan["entrypoint"] != "scene.gltf" || sourceAccess["method"] != "mounted_path" || sourceAccess["path"] != "/mnt/addp-nfs/models" {
					t.Fatalf("source access = %#v", sourceAccess)
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
			case "/api/v1/system/engines/26":
				_, _ = w.Write([]byte(`{"id":26,"tenant_id":7,"name":"Business NFS","engine_type":"nfs","connection_info":{"export_path":"/mnt/addp-nfs"},"lifecycle_state":"active"}`))
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

	executor := NewManagerModel3DGLBExecutor(
		newTestSystemClient("http://"+systemListener.Addr().String()),
		recordingWorkflowLister{engines: []commonModels.Engine{{
			ID:         99,
			Name:       "Tenant Model3D Workflow",
			EngineType: testModel3DWorkflowEngineType,
			ConnectionInfo: commonModels.ConnectionInfo{
				"protocol": "http",
				"host":     "127.0.0.1",
				"port":     workflowPort,
			},
			LifecycleState:   "active",
			ConnectionStatus: commonModels.EngineConnectionOnline,
			Capabilities:     testRasterWorkflowCapabilities(t, testModel3DWorkflowEngineType),
		}}},
		&recordingModel3DGLBObjectStore{statSize: 3},
		"minio:9000",
		"minio-ak",
		"minio-sk",
		false,
		"manager",
		0,
	)

	result, err := executor.BuildModel3DGLB(context.Background(), Model3DGLBExecutionRequest{
		Task: &models.Model3DGLBTask{TenantID: 7, Name: "生成 glTF GLB"},
		Config: Model3DGLBExecutionConfig{
			Source: Model3DGLBSourceConfig{
				ItemLocator:     "addp://engine/26/path/models/scene.gltf?type=file&item_id=77",
				SourceEngineID:  26,
				ItemFingerprint: "fp-gltf",
				Format:          "gltf",
			},
			Result: Model3DGLBResultConfig{
				StorageRef: `{"type":"object","provider":"addp_object_storage","bucket":"manager","object":"model3d/scene.glb"}`,
				FileName:   "scene.glb",
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildModel3DGLB() error = %v", err)
	}
	if capturedPath != "/api/operators/gltf_to_glb/invoke" {
		t.Fatalf("captured workflow path = %q, want gltf_to_glb invoke", capturedPath)
	}
	metadataSource := result.Metadata["source"].(commonModels.JSONMap)
	if metadataSource["format"] != "gltf" {
		t.Fatalf("metadata source format = %#v, want gltf", metadataSource["format"])
	}
}

func TestModel3DGLBOperatorForFormat(t *testing.T) {
	tests := []struct {
		formatName string
		operator   string
	}{
		{formatName: "osgb", operator: "osgb_to_glb"},
		{formatName: "gltf", operator: "gltf_to_glb"},
		{formatName: "fbx", operator: "fbx_to_glb"},
		{formatName: "obj", operator: "obj_to_glb"},
		{formatName: "stl", operator: "stl_to_glb"},
		{formatName: "ifc", operator: "ifc_to_glb"},
	}
	for _, tt := range tests {
		t.Run(tt.formatName, func(t *testing.T) {
			operator, normalized, err := model3DGLBOperatorForFormat(tt.formatName)
			if err != nil {
				t.Fatalf("model3DGLBOperatorForFormat() error = %v", err)
			}
			if operator != tt.operator || normalized != tt.formatName {
				t.Fatalf("operator=%q normalized=%q, want %q/%q", operator, normalized, tt.operator, tt.formatName)
			}
		})
	}
}

type recordingModel3DGLBObjectStore struct {
	statSize   int64
	statBucket string
	statObject string
}

func (s *recordingModel3DGLBObjectStore) BucketExists(context.Context, string) (bool, error) {
	return true, nil
}

func (s *recordingModel3DGLBObjectStore) MakeBucket(context.Context, string, minio.MakeBucketOptions) error {
	return nil
}

func (s *recordingModel3DGLBObjectStore) StatObject(_ context.Context, bucket string, object string, _ minio.StatObjectOptions) (minio.ObjectInfo, error) {
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
				"effects":         []string{"read", "write"},
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
				"effects":         []string{"read", "write"},
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
				"effects":         []string{"read", "write"},
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
				"effects":         []string{"read", "write"},
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
				"effects":         []string{"read", "write"},
				"parameters": []map[string]interface{}{
					{"name": "access_plan", "type": "object", "required": true, "description": "访问计划"},
				},
				"output_ports": []map[string]interface{}{
					{"name": "default", "type": "object", "description": "GLB 生成结果", "is_default": true},
				},
			},
			{
				"id":              "ifc_to_glb",
				"name":            "ifc_to_glb",
				"display_name":    "IFC 转 GLB",
				"engine_type":     engineType,
				"category":        "三维模型转换",
				"category_path":   []string{"三维模型转换"},
				"description":     "生成 GLB",
				"execution_modes": executionModes,
				"effects":         []string{"read", "write"},
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
				"effects":         []string{"read", "write"},
				"parameters": []map[string]interface{}{
					{"name": "access_plan", "type": "object", "required": true, "description": "访问计划"},
				},
				"output_ports": []map[string]interface{}{
					{"name": "default", "type": "object", "description": "3D Tiles 生成结果", "is_default": true},
				},
			},
			{
				"id":              "osgb_scene_to_s3m",
				"name":            "osgb_scene_to_s3m",
				"display_name":    "OSGB Scene 转 S3M",
				"engine_type":     engineType,
				"category":        "三维模型转换",
				"category_path":   []string{"三维模型转换"},
				"description":     "生成 S3M",
				"execution_modes": executionModes,
				"effects":         []string{"read", "write"},
				"parameters": []map[string]interface{}{
					{"name": "access_plan", "type": "object", "required": true, "description": "访问计划"},
				},
				"output_ports": []map[string]interface{}{
					{"name": "default", "type": "object", "description": "S3M 生成结果", "is_default": true},
				},
			},
		},
		"count": 8,
	}
	_ = json.NewEncoder(w).Encode(payload)
}

package service

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/minio/minio-go/v7"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const testPointCloudWorkflowEngineType = "tenant_pointcloud_workflow"

func TestManagerPointCloudCOPCExecutorInvokesDirectWorkflowAndPublishesArtifact(t *testing.T) {
	var capturedParams map[string]interface{}
	workflowServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/api/operators":
				writePointCloudOperatorList(w, testPointCloudWorkflowEngineType, []string{"direct"})
			case r.URL.Path == "/api/operators/laz_to_copc/invoke":
				var payload struct {
					Params map[string]interface{} `json:"params"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatalf("decode workflow request: %v", err)
				}
				capturedParams = payload.Params
				_, _ = w.Write([]byte(`{"status":"success","execution_id":"pc-1","execution_time_ms":78.5,"result":{"copc_uri":"s3://manager/tenant_7/point-cloud-copc/fp/source.copc.laz","copc_ref":"tenant_7/point-cloud-copc/fp/source.copc.laz","size_bytes":13598,"source_format":"laz","target_format":"copc","converter":"/opt/conda/bin/pdal","elapsed_ms":1234,"publish":{"uploaded_files":1,"uploaded_bytes":13598}}}`))
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
			if r.URL.Path != "/api/v1/internal/engines/26" {
				t.Fatalf("unexpected system path: %s", r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":26,"tenant_id":7,"name":"NFS","engine_type":"nfs","connection_info":{"mount_path":"/mnt/addp-nfs"},"is_active":true}`))
		}),
	}
	systemListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer systemServer.Close()
	go func() { _ = systemServer.Serve(systemListener) }()

	executor := NewManagerPointCloudCOPCExecutor(
		commonClient.NewSystemClientWithInternalKey("http://"+systemListener.Addr().String(), "internal-key"),
		recordingWorkflowLister{engines: []commonModels.Engine{{
			ID:         99,
			Name:       "Tenant PointCloud Workflow",
			EngineType: testPointCloudWorkflowEngineType,
			ConnectionInfo: commonModels.ConnectionInfo{
				"protocol": "http",
				"host":     "127.0.0.1",
				"port":     workflowPort,
			},
			IsActive:     true,
			Capabilities: testRasterWorkflowCapabilities(t, testPointCloudWorkflowEngineType),
		}}},
		&recordingPointCloudCOPCObjectStore{size: 24680},
		"http://manager:8081",
		"manager-internal-key",
		"http://minio:9000",
		"minioadmin",
		"minioadmin",
		false,
		"manager",
		0,
	)

	result, err := executor.BuildPointCloudCOPC(context.Background(), PointCloudCOPCExecutionRequest{
		Task:        &models.PointCloudCOPCTask{TenantID: 7, Name: "生成 COPC"},
		ExecutionID: "point-cloud-exec-1",
		Config: PointCloudCOPCExecutionConfig{
			Source: PointCloudCOPCSourceConfig{
				ItemLocator:     "addp://engine/26/path/pointcloud/source.laz?type=file&item_id=77",
				SourceEngineID:  26,
				ItemFingerprint: "fp",
				ItemID:          77,
				Format:          "laz",
				SourceSizeBytes: 1024,
			},
			Result: PointCloudCOPCResultConfig{
				StorageRef: `{"type":"object","provider":"addp_object_storage","bucket":"manager","object":"tenant_7/point-cloud-copc/fp/source.copc.laz"}`,
				FileName:   "source.copc.laz",
			},
			Options: commonModels.JSONMap{"scale_x": 0.01},
		},
	})
	if err != nil {
		t.Fatalf("BuildPointCloudCOPC() error = %v", err)
	}
	if result.StorageRef != `{"type":"object","provider":"addp_object_storage","bucket":"manager","object":"tenant_7/point-cloud-copc/fp/source.copc.laz"}` || result.FileName != "source.copc.laz" || result.SizeBytes != 24680 {
		t.Fatalf("unexpected result: %+v", result)
	}
	workflowRuntime := result.Metadata["workflow_runtime"].(commonModels.JSONMap)
	if workflowRuntime["engine_id"] != uint(99) || workflowRuntime["engine_name"] != "Tenant PointCloud Workflow" || workflowRuntime["engine_type"] != testPointCloudWorkflowEngineType {
		t.Fatalf("metadata = %#v, want workflow runtime identity", result.Metadata)
	}
	if workflowRuntime["execution_id"] != "pc-1" || workflowRuntime["operator"] != "laz_to_copc" || workflowRuntime["mode"] != "direct" {
		t.Fatalf("metadata = %#v, want direct pointcloud workflow invocation", result.Metadata)
	}
	if workflowRuntime["execution_time_ms"] != float64(78.5) {
		t.Fatalf("metadata = %#v, want runtime execution time", result.Metadata)
	}
	source := result.Metadata["source"].(commonModels.JSONMap)
	if source["format"] != "laz" {
		t.Fatalf("metadata = %#v, want normalized source format", result.Metadata)
	}
	sourceAccess := source["access"].(commonModels.JSONMap)
	if sourceAccess["engine_type"] != "nfs" || sourceAccess["access_method"] != "mounted_path" {
		t.Fatalf("metadata = %#v, want source access audit", result.Metadata)
	}
	copcFacts := result.Metadata["copc_facts"].(commonModels.JSONMap)
	if copcFacts["converter"] != "/opt/conda/bin/pdal" || copcFacts["target_format"] != "copc" {
		t.Fatalf("copc facts = %#v, want converter and target format", copcFacts)
	}

	accessPlan := capturedParams["access_plan"].(map[string]interface{})
	sourcePlan := accessPlan["source"].(map[string]interface{})
	targetPlan := accessPlan["target"].(map[string]interface{})
	progress := capturedParams["progress_callback"].(map[string]interface{})
	sourcePlanAccess := sourcePlan["access"].(map[string]interface{})
	targetPlanAccess := targetPlan["access"].(map[string]interface{})
	options := capturedParams["options"].(map[string]interface{})
	if accessPlan["schema_version"] != "addp.workflow.access-plan/v1" || sourcePlanAccess["path"] != "/mnt/addp-nfs/pointcloud/source.laz" || sourcePlan["format"] != "laz" {
		t.Fatalf("source plan = %#v, want mounted source path and format", sourcePlan)
	}
	if targetPlanAccess["method"] != "object_store" || targetPlanAccess["bucket"] != "manager" || targetPlanAccess["object"] != "tenant_7/point-cloud-copc/fp/source.copc.laz" {
		t.Fatalf("target access = %#v, want Manager infra MinIO target", targetPlanAccess)
	}
	if progress["endpoint"] != "http://manager:8081/api/v1/manager/internal/executions/point-cloud-exec-1/events" ||
		progress["tenant_id"] != float64(7) || progress["execution_id"] != "point-cloud-exec-1" ||
		progress["internal_api_key"] != "manager-internal-key" {
		t.Fatalf("progress callback = %#v, want Manager internal execution event callback", progress)
	}
	if targetPlanAccess["endpoint"] != "minio:9000" || targetPlanAccess["access_key"] != "minioadmin" || targetPlanAccess["secret_key"] != "minioadmin" {
		t.Fatalf("target credentials not passed correctly: %#v", targetPlanAccess)
	}
	if targetPlan["name"] != "source.copc.laz" || targetPlan["content_type"] != pointCloudCOPCContentType || targetPlan["write_mode"] != "replace" {
		t.Fatalf("target plan = %#v, want COPC file name and content type", targetPlan)
	}
	if options["scale_x"] != float64(0.01) {
		t.Fatalf("options = %#v, want scale_x forwarded", options)
	}
}

func TestManagerPointCloudCOPCExecutorRejectsOperatorWithoutDirectMode(t *testing.T) {
	workflowServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && r.URL.Path == "/api/operators" {
				writePointCloudOperatorList(w, testPointCloudWorkflowEngineType, []string{"workflow"})
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
			_, _ = w.Write([]byte(`{"id":26,"tenant_id":7,"name":"NFS","engine_type":"nfs","connection_info":{"mount_path":"/mnt/addp-nfs"},"is_active":true}`))
		}),
	}
	systemListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer systemServer.Close()
	go func() { _ = systemServer.Serve(systemListener) }()

	executor := NewManagerPointCloudCOPCExecutor(
		commonClient.NewSystemClientWithInternalKey("http://"+systemListener.Addr().String(), "internal-key"),
		recordingWorkflowLister{engines: []commonModels.Engine{{
			ID:         99,
			Name:       "Tenant PointCloud Workflow",
			EngineType: testPointCloudWorkflowEngineType,
			ConnectionInfo: commonModels.ConnectionInfo{
				"protocol": "http",
				"host":     "127.0.0.1",
				"port":     workflowPort,
			},
			IsActive:     true,
			Capabilities: testRasterWorkflowCapabilities(t, testPointCloudWorkflowEngineType),
		}}},
		&recordingPointCloudCOPCObjectStore{size: 24680},
		"http://manager:8081",
		"manager-internal-key",
		"http://minio:9000",
		"minioadmin",
		"minioadmin",
		false,
		"manager",
		0,
	)

	_, err = executor.BuildPointCloudCOPC(context.Background(), PointCloudCOPCExecutionRequest{
		Task:        &models.PointCloudCOPCTask{TenantID: 7, Name: "生成 COPC"},
		ExecutionID: "point-cloud-exec-2",
		Config: PointCloudCOPCExecutionConfig{
			Source: PointCloudCOPCSourceConfig{
				ItemLocator:     "addp://engine/26/path/pointcloud/source.las?type=file&item_id=77",
				SourceEngineID:  26,
				ItemFingerprint: "fp",
				Format:          "las",
			},
			Result: PointCloudCOPCResultConfig{
				StorageRef: `{"type":"object","provider":"addp_object_storage","bucket":"manager","object":"tenant_7/point-cloud-copc/fp/source.copc.laz"}`,
				FileName:   "source.copc.laz",
			},
		},
	})
	if err == nil {
		t.Fatal("BuildPointCloudCOPC() returned nil error")
	}
	if !strings.Contains(err.Error(), "direct workflow operator") || !strings.Contains(err.Error(), "does not support direct invocation") {
		t.Fatalf("error = %q, want direct mode rejection", err.Error())
	}
}

func TestPointCloudCOPCOperatorForFormat(t *testing.T) {
	tests := []struct {
		sourceFormat string
		operatorName string
		normalized   string
	}{
		{sourceFormat: string(format.FormatLAS), operatorName: "las_to_copc", normalized: string(format.FormatLAS)},
		{sourceFormat: string(format.FormatLAZ), operatorName: "laz_to_copc", normalized: string(format.FormatLAZ)},
		{sourceFormat: string(format.FormatE57), operatorName: "e57_to_copc", normalized: string(format.FormatE57)},
		{sourceFormat: string(format.FormatPCD), operatorName: "pcd_to_copc", normalized: string(format.FormatPCD)},
		{sourceFormat: string(format.FormatXYZ), operatorName: "xyz_to_copc", normalized: string(format.FormatXYZ)},
	}
	for _, tt := range tests {
		t.Run(tt.sourceFormat, func(t *testing.T) {
			operatorName, normalized, err := pointCloudCOPCOperatorForFormat(tt.sourceFormat)
			if err != nil {
				t.Fatalf("pointCloudCOPCOperatorForFormat() error = %v", err)
			}
			if operatorName != tt.operatorName || normalized != tt.normalized {
				t.Fatalf("operator=%q normalized=%q, want %q %q", operatorName, normalized, tt.operatorName, tt.normalized)
			}
		})
	}
}

func TestPointCloudCOPCTaskExecutionMarksResultReady(t *testing.T) {
	db := newPointCloudCOPCTaskServiceTestDB(t)
	copcRepo := repository.NewPointCloudCOPCRepository(db)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	taskSvc := NewPointCloudCOPCTaskService(copcRepo)
	taskSvc.SetBucket("manager")
	taskSvc.SetExecutor(staticPointCloudCOPCExecutor{result: &PointCloudCOPCExecutionResult{
		StorageRef: `{"type":"object","provider":"addp_object_storage","bucket":"manager","object":"tenant_7/point-cloud-copc/fp/source.copc.laz"}`,
		FileName:   "source.copc.laz",
		SizeBytes:  13598,
		Metadata: commonModels.JSONMap{
			"workflow_runtime": commonModels.JSONMap{
				"engine_id":     uint(99),
				"engine_name":   "Tenant PointCloud Workflow",
				"engine_type":   testPointCloudWorkflowEngineType,
				"execution_id":  "pc-1",
				"operator":      "las_to_copc",
				"mode":          "direct",
				"converter_bin": "/opt/conda/bin/pdal",
			},
			"copc_facts": commonModels.JSONMap{"target_format": "copc"},
		},
	}})

	task := &models.PointCloudCOPCTask{
		TenantID: 7,
		Name:     "生成 COPC",
		Enabled:  true,
		Config: commonModels.JSONMap{
			"source": commonModels.JSONMap{
				"item_locator":      "addp://engine/26/path/pointcloud/source.las?type=file&item_id=100",
				"source_engine_id":  uint(26),
				"item_fingerprint":  "fp",
				"item_id":           uint(100),
				"format":            "las",
				"source_size_bytes": int64(1024),
			},
		},
	}
	if err := taskSvc.Create(context.Background(), task); err != nil {
		t.Fatalf("create point cloud COPC generation task: %v", err)
	}
	executionID, err := taskSvc.Execute(context.Background(), task.ID, task.TenantID, commonExecution.TriggerTypeManual, commonExecution.ModuleManager, nil, false)
	if err != nil {
		t.Fatalf("execute point cloud COPC generation task: %v", err)
	}
	exec := waitForPointCloudCOPCTaskExecution(t, taskExecRepo, executionID, int(task.TenantID))
	if exec.Status != commonExecution.ExecutionStatusSuccess {
		t.Fatalf("execution status = %s, want success", exec.Status)
	}
	if exec.Progress != 100 {
		t.Fatalf("execution progress = %d, want 100", exec.Progress)
	}

	results, total, err := copcRepo.List(context.Background(), repository.PointCloudCOPCFilter{TenantID: task.TenantID, Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("list results: %v", err)
	}
	if total != 1 || len(results) != 1 {
		t.Fatalf("results total=%d len=%d, want 1", total, len(results))
	}
	result := results[0]
	if result.Status != models.PointCloudCOPCStatusReady {
		t.Fatalf("result status = %s, want ready", result.Status)
	}
	if result.StorageRef != `{"type":"object","provider":"addp_object_storage","bucket":"manager","object":"tenant_7/point-cloud-copc/fp/source.copc.laz"}` || result.FileName != "source.copc.laz" || result.SizeBytes != 13598 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.ContentURL != pointCloudCOPCContentURL(result.ID) {
		t.Fatalf("content_url = %q, want point cloud content endpoint", result.ContentURL)
	}
	workflowRuntime, ok := asJSONMap(result.Metadata["workflow_runtime"])
	if !ok {
		t.Fatalf("metadata = %#v, want workflow_runtime object", result.Metadata)
	}
	if workflowRuntime["engine_type"] != testPointCloudWorkflowEngineType || workflowRuntime["operator"] != "las_to_copc" || workflowRuntime["mode"] != "direct" {
		t.Fatalf("metadata = %#v, want pointcloud direct workflow audit", result.Metadata)
	}
}

func TestPointCloudCOPCRecordProgressEventUpdatesExecution(t *testing.T) {
	db := newPointCloudCOPCTaskServiceTestDB(t)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	taskSvc := NewPointCloudCOPCTaskService(repository.NewPointCloudCOPCRepository(db))
	startedAt := time.Now().Add(-2 * time.Second)
	if err := taskExecRepo.Create(context.Background(), &commonExecution.TaskExecution{
		TenantID:    7,
		ExecutionID: "point-cloud-progress-1",
		Module:      commonExecution.ModuleManager,
		TaskType:    commonExecution.TaskTypePointCloudCOPCGeneration,
		Source:      commonExecution.ModuleManager,
		Status:      commonExecution.ExecutionStatusRunning,
		Progress:    0,
		TriggerType: commonExecution.TriggerTypeManual,
		StartedAt:   &startedAt,
	}); err != nil {
		t.Fatalf("create execution: %v", err)
	}
	overall := 42
	if err := taskSvc.RecordProgressEvent(context.Background(), 7, "point-cloud-progress-1", PointCloudCOPCProgressEvent{
		Phase:           "convert",
		Event:           "progress",
		Message:         "生成点云 COPC 文件",
		OverallProgress: &overall,
		Metadata:        commonModels.JSONMap{"output_size_bytes": int64(2048)},
	}); err != nil {
		t.Fatalf("RecordProgressEvent: %v", err)
	}
	exec, err := taskExecRepo.GetByExecutionID(context.Background(), "point-cloud-progress-1", 7)
	if err != nil {
		t.Fatalf("get execution: %v", err)
	}
	if exec.Progress != 42 {
		t.Fatalf("progress = %d, want 42", exec.Progress)
	}
	if exec.CurrentStep == nil || *exec.CurrentStep != "生成点云 COPC 文件" {
		t.Fatalf("current_step = %#v, want progress message", exec.CurrentStep)
	}
	progressEvent, ok := asJSONMap(exec.Metadata["progress_event"])
	if !ok {
		t.Fatalf("metadata progress_event = %#v, want object", exec.Metadata["progress_event"])
	}
	if progressEvent["phase"] != "convert" || progressEvent["event"] != "progress" {
		t.Fatalf("metadata progress_event = %#v, want convert/progress", progressEvent)
	}
}

func TestPointCloudCOPCRecordProgressEventRejectsWrongExecution(t *testing.T) {
	db := newPointCloudCOPCTaskServiceTestDB(t)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	taskSvc := NewPointCloudCOPCTaskService(repository.NewPointCloudCOPCRepository(db))
	if err := taskExecRepo.Create(context.Background(), &commonExecution.TaskExecution{
		TenantID:    7,
		ExecutionID: "point-cloud-progress-wrong",
		Module:      commonExecution.ModuleManager,
		TaskType:    commonExecution.TaskTypeRasterCOGGeneration,
		Source:      commonExecution.ModuleManager,
		Status:      commonExecution.ExecutionStatusRunning,
		TriggerType: commonExecution.TriggerTypeManual,
	}); err != nil {
		t.Fatalf("create execution: %v", err)
	}
	err := taskSvc.RecordProgressEvent(context.Background(), 7, "point-cloud-progress-wrong", PointCloudCOPCProgressEvent{
		Phase: "convert",
		Event: "progress",
	})
	if !errors.Is(err, ErrPointCloudCOPCProgressTargetMismatch) {
		t.Fatalf("RecordProgressEvent error = %v, want ErrPointCloudCOPCProgressTargetMismatch", err)
	}
}

func TestPointCloudCOPCRecordProgressEventRejectsPendingExecution(t *testing.T) {
	db := newPointCloudCOPCTaskServiceTestDB(t)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	taskSvc := NewPointCloudCOPCTaskService(repository.NewPointCloudCOPCRepository(db))
	if err := taskExecRepo.Create(context.Background(), &commonExecution.TaskExecution{
		TenantID:    7,
		ExecutionID: "point-cloud-progress-pending",
		Module:      commonExecution.ModuleManager,
		TaskType:    commonExecution.TaskTypePointCloudCOPCGeneration,
		Source:      commonExecution.ModuleManager,
		Status:      commonExecution.ExecutionStatusPending,
		TriggerType: commonExecution.TriggerTypeManual,
	}); err != nil {
		t.Fatalf("create execution: %v", err)
	}
	err := taskSvc.RecordProgressEvent(context.Background(), 7, "point-cloud-progress-pending", PointCloudCOPCProgressEvent{
		Phase: "convert",
		Event: "progress",
	})
	if !errors.Is(err, ErrPointCloudCOPCExecutionNotRunning) {
		t.Fatalf("RecordProgressEvent error = %v, want ErrPointCloudCOPCExecutionNotRunning", err)
	}
}

func writePointCloudOperatorList(w http.ResponseWriter, engineType string, executionModes []string) {
	w.Header().Set("Content-Type", "application/json")
	payload := map[string]interface{}{
		"status": "success",
		"operators": []map[string]interface{}{
			{
				"id":              "las_to_copc",
				"name":            "las_to_copc",
				"display_name":    "LAS 转 COPC",
				"engine_type":     engineType,
				"category":        "点云转换",
				"category_path":   []string{"点云转换", "快显"},
				"description":     "生成 COPC",
				"execution_modes": executionModes,
				"parameters": []map[string]interface{}{
					{"name": "access_plan", "type": "object", "required": true, "description": "访问计划"},
				},
				"output_ports": []map[string]interface{}{
					{"name": "result", "type": "object", "description": "COPC 生成结果", "is_default": true},
				},
			},
			{
				"id":              "laz_to_copc",
				"name":            "laz_to_copc",
				"display_name":    "LAZ 转 COPC",
				"engine_type":     engineType,
				"category":        "点云转换",
				"category_path":   []string{"点云转换", "快显"},
				"description":     "生成 COPC",
				"execution_modes": executionModes,
				"parameters": []map[string]interface{}{
					{"name": "access_plan", "type": "object", "required": true, "description": "访问计划"},
				},
				"output_ports": []map[string]interface{}{
					{"name": "result", "type": "object", "description": "COPC 生成结果", "is_default": true},
				},
			},
			{
				"id":              "e57_to_copc",
				"name":            "e57_to_copc",
				"display_name":    "E57 转 COPC",
				"engine_type":     engineType,
				"category":        "点云转换",
				"category_path":   []string{"点云转换", "快显"},
				"description":     "生成 COPC",
				"execution_modes": executionModes,
				"parameters": []map[string]interface{}{
					{"name": "access_plan", "type": "object", "required": true, "description": "访问计划"},
				},
				"output_ports": []map[string]interface{}{
					{"name": "result", "type": "object", "description": "COPC 生成结果", "is_default": true},
				},
			},
			{
				"id":              "pcd_to_copc",
				"name":            "pcd_to_copc",
				"display_name":    "PCD 转 COPC",
				"engine_type":     engineType,
				"category":        "点云转换",
				"category_path":   []string{"点云转换", "快显"},
				"description":     "生成 COPC",
				"execution_modes": executionModes,
				"parameters": []map[string]interface{}{
					{"name": "access_plan", "type": "object", "required": true, "description": "访问计划"},
				},
				"output_ports": []map[string]interface{}{
					{"name": "result", "type": "object", "description": "COPC 生成结果", "is_default": true},
				},
			},
			{
				"id":              "xyz_to_copc",
				"name":            "xyz_to_copc",
				"display_name":    "XYZ 转 COPC",
				"engine_type":     engineType,
				"category":        "点云转换",
				"category_path":   []string{"点云转换", "快显"},
				"description":     "生成 COPC",
				"execution_modes": executionModes,
				"parameters": []map[string]interface{}{
					{"name": "access_plan", "type": "object", "required": true, "description": "访问计划"},
				},
				"output_ports": []map[string]interface{}{
					{"name": "result", "type": "object", "description": "COPC 生成结果", "is_default": true},
				},
			},
		},
		"count": 5,
	}
	_ = json.NewEncoder(w).Encode(payload)
}

type recordingPointCloudCOPCObjectStore struct {
	size int64
}

func (s *recordingPointCloudCOPCObjectStore) BucketExists(context.Context, string) (bool, error) {
	return true, nil
}

func (s *recordingPointCloudCOPCObjectStore) MakeBucket(context.Context, string, minio.MakeBucketOptions) error {
	return nil
}

func (s *recordingPointCloudCOPCObjectStore) StatObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error) {
	return minio.ObjectInfo{Size: s.size}, nil
}

type staticPointCloudCOPCExecutor struct {
	result *PointCloudCOPCExecutionResult
	err    error
}

func (e staticPointCloudCOPCExecutor) BuildPointCloudCOPC(context.Context, PointCloudCOPCExecutionRequest) (*PointCloudCOPCExecutionResult, error) {
	return e.result, e.err
}

func newPointCloudCOPCTaskServiceTestDB(t *testing.T) *gorm.DB {
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
	return db
}

func waitForPointCloudCOPCTaskExecution(t *testing.T, repo *commonExecution.TaskExecutionRepository, executionID string, tenantID int) *commonExecution.TaskExecution {
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

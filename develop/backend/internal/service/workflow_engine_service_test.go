package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/models"
	"github.com/google/uuid"
)

func TestPreprocessWorkflowParamsDerivesTableSourceFromLocator(t *testing.T) {
	svc := newWorkflowEngineServiceForTest(t, 12, "postgresql")
	workflow := map[string]interface{}{
		"tasks": []interface{}{
			map[string]interface{}{
				"id":       "load_roads",
				"operator": "load",
				"params": map[string]interface{}{
					"source_type": "table",
					"locator":     "addp://engine/12/path/public/roads?type=table&item_id=99",
				},
				"depends_on": []interface{}{},
			},
		},
	}

	got, err := svc.preprocessWorkflowParams(context.Background(), 7, "geopython_workflow", workflow)
	if err != nil {
		t.Fatalf("preprocessWorkflowParams() error = %v", err)
	}

	params := firstTaskParams(t, got)
	if _, ok := params["locator"]; ok {
		t.Fatalf("locator should be removed before runtime params: %#v", params)
	}
	if params["schema"] != "public" || params["table"] != "roads" {
		t.Fatalf("schema/table = %v/%v, want public/roads", params["schema"], params["table"])
	}
	assertConnectionInfo(t, params, "postgresql")
	originalParams := firstTaskParams(t, workflow)
	if originalParams["locator"] == nil || originalParams["connection_info"] != nil {
		t.Fatalf("original workflow was mutated: %#v", originalParams)
	}
}

func TestPreprocessWorkflowParamsDerivesSuperMapPostgisSourceFromLocator(t *testing.T) {
	svc := newWorkflowEngineServiceForTest(t, 12, "postgresql")
	workflow := map[string]interface{}{
		"tasks": []interface{}{
			map[string]interface{}{
				"id":       "open_postgis",
				"operator": "datasource.open_postgis",
				"params": map[string]interface{}{
					"locator":   "addp://engine/12/path/public/%E7%A4%BA%E4%BE%8B%E6%95%B0%E6%8D%AE?type=table&item_id=99",
					"read_only": true,
				},
				"depends_on": []interface{}{},
			},
		},
	}

	got, err := svc.preprocessWorkflowParams(context.Background(), 7, "supermap_workflow", workflow)
	if err != nil {
		t.Fatalf("preprocessWorkflowParams() error = %v", err)
	}

	params := firstTaskParams(t, got)
	if _, ok := params["locator"]; ok {
		t.Fatalf("locator should be removed before runtime params: %#v", params)
	}
	if params["schema"] != "public" || params["table"] != "示例数据" {
		t.Fatalf("schema/table = %v/%v, want public/示例数据", params["schema"], params["table"])
	}
	if params["read_only"] != true {
		t.Fatalf("read_only should be preserved: %#v", params)
	}
	assertConnectionInfo(t, params, "postgresql")
}

func TestPreprocessWorkflowParamsDerivesSuperMapPostgisTargetFromParentLocator(t *testing.T) {
	svc := newWorkflowEngineServiceForTest(t, 12, "postgresql")
	workflow := map[string]interface{}{
		"tasks": []interface{}{
			map[string]interface{}{
				"id":       "open_postgis_output",
				"operator": "datasource.open_postgis",
				"params": map[string]interface{}{
					"target_parent_locator": "addp://engine/12/path/analytics?type=schema&node_id=31",
					"target_name":           "buffer_result",
					"read_only":             false,
				},
				"depends_on": []interface{}{},
			},
		},
	}

	got, targets, err := svc.preprocessWorkflowParamsWithTargets(context.Background(), 7, "supermap_workflow", workflow)
	if err != nil {
		t.Fatalf("preprocessWorkflowParamsWithTargets() error = %v", err)
	}

	params := firstTaskParams(t, got)
	if _, ok := params["target_parent_locator"]; ok {
		t.Fatalf("target_parent_locator should be removed before runtime params: %#v", params)
	}
	if params["schema"] != "analytics" || params["table"] != "buffer_result" {
		t.Fatalf("schema/table = %v/%v, want analytics/buffer_result", params["schema"], params["table"])
	}
	if params["read_only"] != false {
		t.Fatalf("read_only should be preserved: %#v", params)
	}
	assertConnectionInfo(t, params, "postgresql")
	if len(targets) != 1 || targets[0].Locator != "addp://engine/12/path/analytics/buffer_result?type=table" {
		t.Fatalf("targets = %#v, want table target locator", targets)
	}
}

func TestPreprocessWorkflowParamsDerivesSuperMapUdbxTargetFromNFSDirectory(t *testing.T) {
	svc := newWorkflowEngineServiceWithEnginesForTest(t, map[uint]commonModels.Engine{
		3: {
			ID:             3,
			Name:           "test-nfs",
			EngineType:     "nfs",
			LifecycleState: "active",
			ConnectionInfo: commonModels.ConnectionInfo{
				"server":      "nfs.local",
				"export_path": "/exports/addp",
			},
		},
	})
	workflow := map[string]interface{}{
		"tasks": []interface{}{
			map[string]interface{}{
				"id":       "create_udbx",
				"operator": "datasource.create",
				"params": map[string]interface{}{
					"target_parent_locator": "addp://engine/3/path/project/supermap?type=directory&node_id=18",
					"target_name":           "analysis.udbx",
					"alias":                 "analysis",
				},
				"depends_on": []interface{}{},
			},
		},
	}

	got, targets, err := svc.preprocessWorkflowParamsWithTargets(context.Background(), 7, "supermap_workflow", workflow)
	if err != nil {
		t.Fatalf("preprocessWorkflowParamsWithTargets() error = %v", err)
	}

	params := firstTaskParams(t, got)
	if params["path"] != "project/supermap/analysis.udbx" {
		t.Fatalf("path = %v, want project/supermap/analysis.udbx", params["path"])
	}
	assertConnectionInfo(t, params, "nfs")
	conn := params["connection_info"].(map[string]interface{})
	if conn["server"] != "nfs.local" || conn["export_path"] != "/exports/addp" {
		t.Fatalf("connection_info = %#v, want dynamic NFS binding facts", conn)
	}
	if len(targets) != 1 || targets[0].Locator != "addp://engine/3/path/project/supermap/analysis.udbx?type=file" {
		t.Fatalf("targets = %#v, want file target locator", targets)
	}
}

func TestPreprocessWorkflowParamsDerivesTableTargetFromParentLocator(t *testing.T) {
	svc := newWorkflowEngineServiceForTest(t, 7, "postgresql")
	workflow := map[string]interface{}{
		"tasks": []interface{}{
			map[string]interface{}{
				"id":       "save_result",
				"operator": "save",
				"params": map[string]interface{}{
					"target_parent_locator": "addp://engine/7/path/analytics?type=schema&node_id=23",
					"target_name":           "result_table",
				},
				"depends_on": []interface{}{},
			},
		},
	}

	got, err := svc.preprocessWorkflowParams(context.Background(), 7, "geopython_workflow", workflow)
	if err != nil {
		t.Fatalf("preprocessWorkflowParams() error = %v", err)
	}

	params := firstTaskParams(t, got)
	if _, ok := params["target_parent_locator"]; ok {
		t.Fatalf("target_parent_locator should be removed before runtime params: %#v", params)
	}
	if _, ok := params["target_name"]; ok {
		t.Fatalf("target_name should be removed before runtime params: %#v", params)
	}
	if params["schema"] != "analytics" || params["table"] != "result_table" {
		t.Fatalf("schema/table = %v/%v, want analytics/result_table", params["schema"], params["table"])
	}
	assertConnectionInfo(t, params, "postgresql")
}

func TestPreprocessWorkflowParamsDerivesFileSourceFromLocator(t *testing.T) {
	svc := newWorkflowEngineServiceForTest(t, 3, "nfs")
	workflow := map[string]interface{}{
		"tasks": []interface{}{
			map[string]interface{}{
				"id":       "load_file",
				"operator": "load",
				"params": map[string]interface{}{
					"source_type": "file",
					"locator":     "addp://engine/3/path/data/roads.csv?type=file&item_id=45",
				},
				"depends_on": []interface{}{},
			},
		},
	}

	got, err := svc.preprocessWorkflowParams(context.Background(), 7, "geopython_workflow", workflow)
	if err != nil {
		t.Fatalf("preprocessWorkflowParams() error = %v", err)
	}

	params := firstTaskParams(t, got)
	if _, ok := params["locator"]; ok {
		t.Fatalf("locator should be removed before runtime params: %#v", params)
	}
	if params["path"] != "data/roads.csv" {
		t.Fatalf("path = %v, want data/roads.csv", params["path"])
	}
	assertConnectionInfo(t, params, "nfs")
}

func TestPreprocessWorkflowParamsDerivesObjectSourceAsSparkPath(t *testing.T) {
	svc := newWorkflowEngineServiceForTest(t, 4, "minio")
	workflow := map[string]interface{}{
		"tasks": []interface{}{
			map[string]interface{}{
				"id":       "load_object",
				"operator": "load",
				"params": map[string]interface{}{
					"source_type": "file",
					"locator":     "addp://engine/4/path/addp/lake/roads.parquet?type=object&item_id=46",
				},
				"depends_on": []interface{}{},
			},
		},
	}

	got, err := svc.preprocessWorkflowParams(context.Background(), 7, "spark_workflow", workflow)
	if err != nil {
		t.Fatalf("preprocessWorkflowParams() error = %v", err)
	}

	params := firstTaskParams(t, got)
	if params["path"] != "s3a://addp/lake/roads.parquet" {
		t.Fatalf("path = %v, want s3a://addp/lake/roads.parquet", params["path"])
	}
	assertConnectionInfo(t, params, "minio")
}

func TestPreprocessWorkflowParamsDerivesFileTargetFromParentLocator(t *testing.T) {
	svc := newWorkflowEngineServiceForTest(t, 3, "nfs")
	workflow := map[string]interface{}{
		"tasks": []interface{}{
			map[string]interface{}{
				"id":       "save_file",
				"operator": "save",
				"params": map[string]interface{}{
					"target_parent_locator": "addp://engine/3/path/output/reports?type=directory&node_id=31",
					"target_name":           "result.csv",
				},
				"depends_on": []interface{}{},
			},
		},
	}

	got, err := svc.preprocessWorkflowParams(context.Background(), 7, "geopython_workflow", workflow)
	if err != nil {
		t.Fatalf("preprocessWorkflowParams() error = %v", err)
	}

	params := firstTaskParams(t, got)
	if params["path"] != "output/reports/result.csv" {
		t.Fatalf("path = %v, want output/reports/result.csv", params["path"])
	}
	assertConnectionInfo(t, params, "nfs")
}

func TestPreprocessWorkflowParamsDerivesObjectTargetFromBucketLocator(t *testing.T) {
	svc := newWorkflowEngineServiceForTest(t, 4, "minio")
	workflow := map[string]interface{}{
		"tasks": []interface{}{
			map[string]interface{}{
				"id":       "save_object",
				"operator": "save",
				"params": map[string]interface{}{
					"target_parent_locator": "addp://engine/4/path/addp?type=bucket&node_id=32",
					"target_name":           "result/output.parquet",
				},
				"depends_on": []interface{}{},
			},
		},
	}

	got, err := svc.preprocessWorkflowParams(context.Background(), 7, "spark_workflow", workflow)
	if err != nil {
		t.Fatalf("preprocessWorkflowParams() error = %v", err)
	}

	params := firstTaskParams(t, got)
	if params["path"] != "s3a://addp/result/output.parquet" {
		t.Fatalf("path = %v, want s3a://addp/result/output.parquet", params["path"])
	}
	assertConnectionInfo(t, params, "minio")
}

func TestPreprocessWorkflowParamsRequiresTargetNameWithParentLocator(t *testing.T) {
	svc := newWorkflowEngineServiceForTest(t, 7, "postgresql")
	workflow := map[string]interface{}{
		"tasks": []interface{}{
			map[string]interface{}{
				"id":       "save_result",
				"operator": "save",
				"params": map[string]interface{}{
					"target_parent_locator": "addp://engine/7/path/analytics?type=schema&node_id=23",
				},
				"depends_on": []interface{}{},
			},
		},
	}

	if _, err := svc.preprocessWorkflowParams(context.Background(), 7, "geopython_workflow", workflow); err == nil {
		t.Fatal("preprocessWorkflowParams() error = nil, want target_name error")
	}
}

func TestPreprocessWorkflowParamsRejectsDirectEngineID(t *testing.T) {
	svc := newWorkflowEngineServiceForTest(t, 7, "postgresql")
	workflow := map[string]interface{}{
		"tasks": []interface{}{
			map[string]interface{}{
				"id":       "load_legacy",
				"operator": "load",
				"params": map[string]interface{}{
					"source_type": "table",
					"engine_id":   float64(7),
					"schema":      "public",
					"table":       "roads",
				},
				"depends_on": []interface{}{},
			},
		},
	}

	if _, err := svc.preprocessWorkflowParams(context.Background(), 7, "geopython_workflow", workflow); err == nil {
		t.Fatal("preprocessWorkflowParams() error = nil, want direct engine_id error")
	}
}

func TestPreprocessWorkflowParamsRejectsLocatorForUndeclaredOperator(t *testing.T) {
	svc := newWorkflowEngineServiceForTest(t, 12, "postgresql")
	workflow := map[string]interface{}{
		"tasks": []interface{}{
			map[string]interface{}{
				"id":       "buffer_roads",
				"operator": "buffer",
				"params": map[string]interface{}{
					"locator": "addp://engine/12/path/public/roads?type=table&item_id=99",
				},
				"depends_on": []interface{}{},
			},
		},
	}

	_, err := svc.preprocessWorkflowParams(context.Background(), 7, "geopython_workflow", workflow)
	if err == nil || !strings.Contains(err.Error(), "未声明 Develop Adapter Spec") {
		t.Fatalf("preprocessWorkflowParams() error = %v, want undeclared adapter spec error", err)
	}
}

func TestPreprocessWorkflowParamsRejectsOperatorAdapterFromDifferentRuntime(t *testing.T) {
	svc := newWorkflowEngineServiceForTest(t, 12, "postgresql")
	workflow := map[string]interface{}{
		"tasks": []interface{}{
			map[string]interface{}{
				"id":       "open_postgis",
				"operator": "datasource.open_postgis",
				"params": map[string]interface{}{
					"locator": "addp://engine/12/path/public/roads?type=table&item_id=99",
				},
				"depends_on": []interface{}{},
			},
		},
	}

	_, err := svc.preprocessWorkflowParams(context.Background(), 7, "geopython_workflow", workflow)
	if err == nil || !strings.Contains(err.Error(), "未声明 Develop Adapter Spec") {
		t.Fatalf("preprocessWorkflowParams() error = %v, want runtime-specific adapter spec error", err)
	}
}

func TestPreprocessWorkflowParamsRejectsMissingTasks(t *testing.T) {
	svc := newWorkflowEngineServiceForTest(t, 7, "postgresql")
	workflow := map[string]interface{}{
		"step1": map[string]interface{}{
			"operator": "load",
			"params":   map[string]interface{}{},
		},
	}

	if _, err := svc.preprocessWorkflowParams(context.Background(), 7, "geopython_workflow", workflow); err == nil {
		t.Fatal("preprocessWorkflowParams() error = nil, want missing tasks error")
	}
}

func TestPreprocessWorkflowParamsRejectsEmptyTasks(t *testing.T) {
	svc := newWorkflowEngineServiceForTest(t, 7, "postgresql")
	workflow := map[string]interface{}{
		"tasks": []interface{}{},
	}

	if _, err := svc.preprocessWorkflowParams(context.Background(), 7, "geopython_workflow", workflow); err == nil {
		t.Fatal("preprocessWorkflowParams() error = nil, want empty tasks error")
	}
}

func TestWorkflowRuntimeEngineIDMapsSparkClusterID(t *testing.T) {
	svc := newWorkflowEngineServiceWithEnginesForTest(t, map[uint]commonModels.Engine{
		34: testEngine(34, "spark", true),
	})
	cfg := models.WorkflowExecutionConfig{
		EngineSpecific: map[string]interface{}{
			"spark_cluster_id": float64(34),
		},
	}

	got, err := svc.workflowRuntimeEngineID(7, "spark_workflow", cfg)
	if err != nil {
		t.Fatalf("workflowRuntimeEngineID(7, ) error = %v", err)
	}
	if got != uint(34) {
		t.Fatalf("runtime engine_id = %#v, want 34", got)
	}
}

func TestWorkflowRuntimeEngineIDMapsSparkClusterIDString(t *testing.T) {
	svc := newWorkflowEngineServiceWithEnginesForTest(t, map[uint]commonModels.Engine{
		34: testEngine(34, "spark", true),
	})
	cfg := models.WorkflowExecutionConfig{
		EngineSpecific: map[string]interface{}{
			"spark_cluster_id": "34",
		},
	}

	got, err := svc.workflowRuntimeEngineID(7, "spark_workflow", cfg)
	if err != nil {
		t.Fatalf("workflowRuntimeEngineID(7, ) error = %v", err)
	}
	if got != uint(34) {
		t.Fatalf("runtime engine_id = %#v, want 34", got)
	}
}

func TestWorkflowRuntimeEngineIDRejectsOfflineSparkCluster(t *testing.T) {
	offline := testEngine(34, "spark", true)
	offline.ConnectionStatus = commonModels.EngineConnectionOffline
	svc := newWorkflowEngineServiceWithEnginesForTest(t, map[uint]commonModels.Engine{34: offline})
	cfg := models.WorkflowExecutionConfig{
		EngineSpecific: map[string]interface{}{"spark_cluster_id": float64(34)},
	}

	if _, err := svc.workflowRuntimeEngineID(7, "spark_workflow", cfg); err == nil || !strings.Contains(err.Error(), "当前不可用") {
		t.Fatalf("workflowRuntimeEngineID() error = %v, want offline rejection", err)
	}
}

func TestWorkflowRuntimeEngineIDRequiresSparkClusterIDForSparkWorkflow(t *testing.T) {
	svc := newWorkflowEngineServiceWithEnginesForTest(t, nil)
	cfg := models.WorkflowExecutionConfig{}

	if _, err := svc.workflowRuntimeEngineID(7, "spark_workflow", cfg); err == nil {
		t.Fatal("workflowRuntimeEngineID(7, ) error = nil, want missing spark_cluster_id error")
	}
}

func TestWorkflowRuntimeEngineIDRejectsSparkClusterIDForNonSparkWorkflow(t *testing.T) {
	svc := newWorkflowEngineServiceWithEnginesForTest(t, nil)
	cfg := models.WorkflowExecutionConfig{
		EngineSpecific: map[string]interface{}{
			"spark_cluster_id": float64(34),
		},
	}

	if _, err := svc.workflowRuntimeEngineID(7, "geopython_workflow", cfg); err == nil {
		t.Fatal("workflowRuntimeEngineID(7, ) error = nil, want non-spark workflow error")
	}
}

func TestWorkflowRuntimeEngineIDRejectsInvalidSparkClusterID(t *testing.T) {
	svc := newWorkflowEngineServiceWithEnginesForTest(t, nil)
	cfg := models.WorkflowExecutionConfig{
		EngineSpecific: map[string]interface{}{
			"spark_cluster_id": float64(0),
		},
	}

	if _, err := svc.workflowRuntimeEngineID(7, "spark_workflow", cfg); err == nil {
		t.Fatal("workflowRuntimeEngineID(7, ) error = nil, want invalid spark_cluster_id error")
	}
}

func TestWorkflowRuntimeEngineIDRejectsNonSparkRuntimeEngine(t *testing.T) {
	svc := newWorkflowEngineServiceWithEnginesForTest(t, map[uint]commonModels.Engine{
		34: testEngine(34, "postgresql", true),
	})
	cfg := models.WorkflowExecutionConfig{
		EngineSpecific: map[string]interface{}{
			"spark_cluster_id": float64(34),
		},
	}

	if _, err := svc.workflowRuntimeEngineID(7, "spark_workflow", cfg); err == nil {
		t.Fatal("workflowRuntimeEngineID(7, ) error = nil, want non-spark runtime engine error")
	}
}

func TestWorkflowRuntimeEngineIDRejectsInactiveSparkRuntimeEngine(t *testing.T) {
	svc := newWorkflowEngineServiceWithEnginesForTest(t, map[uint]commonModels.Engine{
		34: testEngine(34, "spark", false),
	})
	cfg := models.WorkflowExecutionConfig{
		EngineSpecific: map[string]interface{}{
			"spark_cluster_id": float64(34),
		},
	}

	if _, err := svc.workflowRuntimeEngineID(7, "spark_workflow", cfg); err == nil {
		t.Fatal("workflowRuntimeEngineID(7, ) error = nil, want inactive spark runtime engine error")
	}
}

func TestToWorkflowResponseIncludesRuntimeExecutionTime(t *testing.T) {
	executionTime := 12.5
	resp := toWorkflowResponse(&plugin.WorkflowExecuteResult{
		Status:          "success",
		ExecutionID:     "runtime-exec-1",
		ExecutionTimeMs: &executionTime,
		Result: map[string]interface{}{
			"final_result": map[string]interface{}{"value": 42},
			"all_results": map[string]interface{}{
				"task1": map[string]interface{}{"value": 42},
			},
		},
	})

	if resp.ExecutionTimeMs == nil || *resp.ExecutionTimeMs != executionTime {
		t.Fatalf("execution_time_ms = %#v, want %v", resp.ExecutionTimeMs, executionTime)
	}
	if resp.FinalResult != `{"value":42}` {
		t.Fatalf("final_result = %q, want encoded JSON", resp.FinalResult)
	}
	if resp.AllResults["task1"] != `{"value":42}` {
		t.Fatalf("all_results.task1 = %q, want encoded JSON", resp.AllResults["task1"])
	}
}

func TestWorkflowRuntimeStatusSummaryKeepsDiagnosticFieldsOnly(t *testing.T) {
	executionTime := 32.25
	summary := workflowRuntimeStatusSummary(&plugin.WorkflowExecutionStatus{
		Status:          "success",
		ExecutionID:     "runtime-exec-1",
		Result:          map[string]interface{}{"large": "payload"},
		AllResults:      map[string]interface{}{"task1": "payload"},
		Message:         "finished",
		TaskOrder:       []string{"load", "save"},
		Error:           "warning",
		ErrorCode:       "RUNTIME_WARNING",
		Details:         "diagnostic details",
		Progress:        100,
		StartedAt:       "2026-06-24T10:00:00Z",
		ExecutionTimeMs: &executionTime,
		Raw:             map[string]interface{}{"task_status": "legacy"},
	})

	if summary["status"] != "success" {
		t.Fatalf("status = %v, want success", summary["status"])
	}
	if summary["runtime_execution_id"] != "runtime-exec-1" {
		t.Fatalf("runtime_execution_id = %v, want runtime execution id", summary["runtime_execution_id"])
	}
	if _, ok := summary["execution_id"]; ok {
		t.Fatalf("summary should not include ambiguous execution_id: %#v", summary)
	}
	if summary["progress"] != 100 {
		t.Fatalf("progress = %v, want 100", summary["progress"])
	}
	if summary["execution_time_ms"] != executionTime {
		t.Fatalf("execution_time_ms = %v, want %v", summary["execution_time_ms"], executionTime)
	}
	if _, ok := summary["result"]; ok {
		t.Fatalf("summary should not include full result: %#v", summary)
	}
	if _, ok := summary["all_results"]; ok {
		t.Fatalf("summary should not include all_results: %#v", summary)
	}
	if _, ok := summary["raw"]; ok {
		t.Fatalf("summary should not include raw runtime payload: %#v", summary)
	}
}

func TestExecuteWorkflowSendsCanonicalSparkHTTPRuntimeRequest(t *testing.T) {
	runtime := newWorkflowRuntimeContractServer(t, "spark_workflow")
	svc := newWorkflowEngineServiceWithEnginesForTest(t, map[uint]commonModels.Engine{
		91: {
			ID: 91, Name: "test-spark-workflow", EngineType: "spark_workflow", LifecycleState: "active",
			ConnectionInfo: runtime.connectionInfo,
			Capabilities:   workflowRuntimeCapabilitiesForTest(t, "spark_workflow"),
		},
		34: testEngine(34, "spark", true),
	})

	resp, err := svc.ExecuteWorkflow(
		context.Background(),
		7,
		map[string]interface{}{"tasks": []interface{}{map[string]interface{}{
			"id": "contract", "operator": "contract_op", "params": map[string]interface{}{}, "depends_on": []interface{}{},
		}}},
		nil,
		`{"engine_id":91,"engine_specific":{"spark_cluster_id":34}}`,
	)
	if err != nil {
		t.Fatalf("ExecuteWorkflow() error = %v", err)
	}
	if resp.Status != "success" || resp.ExecutionID != "runtime-contract-exec" {
		t.Fatalf("response = %#v, want successful runtime execution", resp)
	}
	if runtime.captured["engine_id"] != float64(34) {
		t.Fatalf("top-level engine_id = %#v, want 34: %#v", runtime.captured["engine_id"], runtime.captured)
	}
	assertCanonicalWorkflowRuntimeAuthorization(t, runtime.captured)
}

func workflowRuntimeCapabilitiesForTest(t *testing.T, engineType string) *commonModels.JSONString {
	t.Helper()
	payload, err := plugin.MarshalEngineCapabilities(plugin.NewWorkflowCapabilities(engineType, plugin.WorkflowRuntimeAPIAddpV1))
	if err != nil {
		t.Fatalf("marshal workflow capabilities: %v", err)
	}
	value := commonModels.JSONString(payload)
	return &value
}

func TestExecuteWorkflowOmitsTopLevelEngineIDForNonSparkHTTPRuntime(t *testing.T) {
	engineType := "test_http_workflow_contract"
	plugin.Register(plugin.NewHTTPWorkflowRuntimeProvider(engineType, "Test HTTP Workflow Runtime"))
	runtime := newWorkflowRuntimeContractServer(t, engineType)
	svc := newWorkflowEngineServiceWithEnginesForTest(t, map[uint]commonModels.Engine{
		92: {
			ID: 92, Name: "test-http-workflow", EngineType: engineType, LifecycleState: "active",
			ConnectionInfo: runtime.connectionInfo,
		},
	})

	resp, err := svc.ExecuteWorkflow(
		context.Background(),
		7,
		map[string]interface{}{"tasks": []interface{}{map[string]interface{}{
			"id": "contract", "operator": "contract_op", "params": map[string]interface{}{}, "depends_on": []interface{}{},
		}}},
		nil,
		`{"engine_id":92}`,
	)
	if err != nil {
		t.Fatalf("ExecuteWorkflow() error = %v", err)
	}
	if resp.Status != "success" || resp.ExecutionID != "runtime-contract-exec" {
		t.Fatalf("response = %#v, want successful runtime execution", resp)
	}
	if _, exists := runtime.captured["engine_id"]; exists {
		t.Fatalf("non-Spark runtime must not receive top-level engine_id: %#v", runtime.captured)
	}
	assertCanonicalWorkflowRuntimeAuthorization(t, runtime.captured)
}

func assertCanonicalWorkflowRuntimeAuthorization(t *testing.T, captured map[string]interface{}) {
	t.Helper()
	runtimeContext, ok := captured["runtime"].(map[string]interface{})
	if !ok {
		t.Fatalf("runtime context missing: %#v", captured)
	}
	if runtimeContext["tenant_id"] != float64(7) {
		t.Fatalf("runtime tenant_id = %#v, want 7", runtimeContext["tenant_id"])
	}
	authorization, ok := runtimeContext["execution_authorization"].(map[string]interface{})
	if !ok || authorization["id"] != float64(1) {
		t.Fatalf("runtime execution authorization missing: %#v", captured)
	}
	effects, ok := authorization["effects"].([]interface{})
	if !ok || len(effects) != 1 || effects[0] != "read" {
		t.Fatalf("authorization effects = %#v, want [read]", authorization["effects"])
	}
	if _, exists := captured["execution_authorization"]; exists {
		t.Fatalf("execution_authorization must not be flattened: %#v", captured)
	}
}

type workflowRuntimeContractServer struct {
	captured       map[string]interface{}
	connectionInfo commonModels.ConnectionInfo
}

func newWorkflowRuntimeContractServer(t *testing.T, engineType string) *workflowRuntimeContractServer {
	t.Helper()
	fixture := &workflowRuntimeContractServer{}
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/operators":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "success",
				"operators": []map[string]interface{}{
					{
						"id": "contract_op", "name": "contract_op", "display_name": "Contract Operator",
						"engine_type": engineType, "category": "测试", "category_path": []string{"测试"},
						"description": "Canonical HTTP request contract", "execution_modes": []string{"workflow"},
						"effects": []string{"read"}, "parameters": []interface{}{},
						"output_ports": []map[string]interface{}{{"name": "default", "type": "object", "is_default": true}},
					},
				},
			})
		case "/api/workflow":
			if err := json.NewDecoder(r.Body).Decode(&fixture.captured); err != nil {
				t.Errorf("decode workflow request: %v", err)
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "success", "execution_id": "runtime-contract-exec",
			})
		case "/api/executions/runtime-contract-exec":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "success", "execution_id": "runtime-contract-exec", "progress": 100,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(runtime.Close)

	host, port, err := net.SplitHostPort(strings.TrimPrefix(runtime.URL, "http://"))
	if err != nil {
		t.Fatalf("parse runtime URL: %v", err)
	}
	fixture.connectionInfo = commonModels.ConnectionInfo{"protocol": "http", "host": host, "port": port}
	return fixture
}

func TestExecuteWorkflowWaitsForAsyncRuntimeTerminalStatus(t *testing.T) {
	engineType := "test_async_workflow"
	runtime := &testWorkflowRuntimePlugin{
		engineType: engineType,
		statuses: []plugin.WorkflowExecutionStatus{
			{Status: "running", ExecutionID: "runtime-exec-1", Progress: 50},
			{
				Status:      "success",
				ExecutionID: "runtime-exec-1",
				Progress:    100,
				Result:      map[string]interface{}{"value": 42},
				AllResults: map[string]interface{}{
					"task1": map[string]interface{}{"value": 42},
				},
			},
		},
	}
	plugin.Register(runtime)

	previousPollInterval := workflowRuntimeStatusPollInterval
	workflowRuntimeStatusPollInterval = time.Millisecond
	t.Cleanup(func() {
		workflowRuntimeStatusPollInterval = previousPollInterval
	})

	svc := newWorkflowEngineServiceForTest(t, 91, engineType)
	resp, err := svc.ExecuteWorkflow(
		context.Background(),
		7,
		map[string]interface{}{
			"tasks": []interface{}{
				map[string]interface{}{
					"id":         "task1",
					"operator":   "async_op",
					"params":     map[string]interface{}{},
					"depends_on": []interface{}{},
				},
			},
		},
		nil,
		`{"engine_id":91}`,
	)
	if err != nil {
		t.Fatalf("ExecuteWorkflow() error = %v", err)
	}
	if resp.Status != "success" {
		t.Fatalf("status = %q, want success", resp.Status)
	}
	if resp.FinalResult != `{"value":42}` {
		t.Fatalf("final_result = %q, want status result JSON", resp.FinalResult)
	}
	if resp.AllResults["task1"] != `{"value":42}` {
		t.Fatalf("all_results.task1 = %q, want status all_results JSON", resp.AllResults["task1"])
	}
	if resp.RuntimeStatus["progress"] != 100 {
		t.Fatalf("runtime_status.progress = %v, want 100", resp.RuntimeStatus["progress"])
	}
	if runtime.statusCallCount() < 2 {
		t.Fatalf("status call count = %d, want async polling", runtime.statusCallCount())
	}
}

func TestExecuteWorkflowReturnsErrorForTerminalRuntimeFailure(t *testing.T) {
	engineType := "test_failed_workflow"
	runtime := &testWorkflowRuntimePlugin{
		engineType: engineType,
		statuses: []plugin.WorkflowExecutionStatus{
			{
				Status:      "failed",
				ExecutionID: "runtime-exec-1",
				Error:       "boom",
				ErrorCode:   "EXECUTION_FAILED",
				Details:     "runtime details",
			},
		},
	}
	plugin.Register(runtime)

	svc := newWorkflowEngineServiceForTest(t, 92, engineType)
	_, err := svc.ExecuteWorkflow(
		context.Background(),
		7,
		map[string]interface{}{
			"tasks": []interface{}{
				map[string]interface{}{
					"id":         "task1",
					"operator":   "async_op",
					"params":     map[string]interface{}{},
					"depends_on": []interface{}{},
				},
			},
		},
		nil,
		`{"engine_id":92}`,
	)
	if err == nil {
		t.Fatal("ExecuteWorkflow() error = nil, want terminal runtime failure")
	}
	if !strings.Contains(err.Error(), "EXECUTION_FAILED") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error = %v, want runtime failure details", err)
	}
}

func TestConversionAdaptersDeriveAccessPlanV1(t *testing.T) {
	svc := newWorkflowEngineServiceWithEnginesForTest(t, map[uint]commonModels.Engine{
		1: {
			ID: 1, Name: "business-nfs", EngineType: "nfs", LifecycleState: "active",
			ConnectionInfo: commonModels.ConnectionInfo{"mount_path": "/data"},
		},
		2: {
			ID: 2, Name: "business-minio", EngineType: "minio", LifecycleState: "active",
			ConnectionInfo: commonModels.ConnectionInfo{"endpoint": "minio:9000", "access_key": "key", "secret_key": "secret"},
		},
	})
	tests := []struct {
		engineType, operator, sourceName, targetName, sourceKind, targetKind, entrypoint, sourceFormat string
	}{
		{"model3d_workflow", "osgb_to_glb", "models/tile.osgb", "tile.glb", "file", "file", "", "osgb"},
		{"model3d_workflow", "gltf_to_glb", "models/scene.gltf", "scene.glb", "directory", "file", "scene.gltf", "gltf"},
		{"model3d_workflow", "fbx_to_glb", "models/scene.fbx", "scene.glb", "directory", "file", "scene.fbx", "fbx"},
		{"model3d_workflow", "obj_to_glb", "models/scene.obj", "scene.glb", "directory", "file", "scene.obj", "obj"},
		{"model3d_workflow", "stl_to_glb", "models/scene.stl", "scene.glb", "file", "file", "", "stl"},
		{"model3d_workflow", "ifc_to_glb", "models/building.ifc", "building.glb", "file", "file", "", "ifc"},
		{"model3d_workflow", "osgb_scene_to_3dtiles", "scenes/site_a", "site_a", "directory", "directory", "", "osgb_scene"},
		{"model3d_workflow", "gaussian_splat_to_ksplat", "models/cloud.splat", "cloud.ksplat", "file", "file", "", "splat"},
		{"pointcloud_workflow", "las_to_copc", "points/cloud.las", "cloud.copc.laz", "file", "file", "", "las"},
		{"pointcloud_workflow", "laz_to_copc", "points/cloud.laz", "cloud.copc.laz", "file", "file", "", "laz"},
		{"pointcloud_workflow", "e57_to_copc", "points/cloud.e57", "cloud.copc.laz", "file", "file", "", "e57"},
		{"pointcloud_workflow", "pcd_to_copc", "points/cloud.pcd", "cloud.copc.laz", "file", "file", "", "pcd"},
		{"pointcloud_workflow", "xyz_to_copc", "points/cloud.xyz", "cloud.copc.laz", "file", "file", "", "xyz"},
	}
	for _, tt := range tests {
		t.Run(tt.operator, func(t *testing.T) {
			workflow := map[string]interface{}{"tasks": []interface{}{map[string]interface{}{
				"id": "convert", "operator": tt.operator, "depends_on": []interface{}{},
				"params": map[string]interface{}{
					"locator":               "addp://engine/1/path/" + tt.sourceName + "?type=file&item_id=8",
					"target_parent_locator": "addp://engine/2/path/business/results?type=prefix",
					"target_name":           tt.targetName, "write_mode": "create",
				},
			}}}
			got, targets, err := svc.preprocessWorkflowParamsWithTargets(context.Background(), 7, tt.engineType, workflow)
			if err != nil {
				t.Fatalf("preprocess access plan: %v", err)
			}
			params := firstTaskParams(t, got)
			plan := params["access_plan"].(commonModels.JSONMap)
			if plan["schema_version"] != "addp.workflow.access-plan/v1" {
				t.Fatalf("schema_version = %#v", plan["schema_version"])
			}
			source := plan["source"].(commonModels.JSONMap)
			target := plan["target"].(commonModels.JSONMap)
			if source["kind"] != tt.sourceKind || source["format"] != tt.sourceFormat || stringParam(source, "entrypoint") != tt.entrypoint {
				t.Fatalf("source = %#v", source)
			}
			if tt.entrypoint == "" {
				if _, exists := source["entrypoint"]; exists {
					t.Fatalf("source should not include entrypoint: %#v", source)
				}
			}
			if target["kind"] != tt.targetKind || target["name"] != tt.targetName || target["write_mode"] != "create" {
				t.Fatalf("target = %#v", target)
			}
			if len(targets) != 1 || targets[0].EngineID != 2 {
				t.Fatalf("produced targets = %#v", targets)
			}
			encodedTarget, err := json.Marshal(targets[0])
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encodedTarget), "access_plan") || strings.Contains(string(encodedTarget), "secret_key") {
				t.Fatalf("produced target leaked execution access plan: %s", encodedTarget)
			}
			for _, publicName := range []string{"locator", "target_parent_locator", "target_name", "write_mode"} {
				if _, exists := params[publicName]; exists {
					t.Fatalf("public param %s leaked to runtime: %#v", publicName, params)
				}
			}
		})
	}
}

func TestConversionAdapterRejectsInfraTargetEngine(t *testing.T) {
	tenantID := uint(7)
	svc := newWorkflowEngineServiceWithRawEnginesForTest(t, map[uint]commonModels.Engine{
		1: {ID: 1, TenantID: &tenantID, EngineType: "nfs", LifecycleState: "active", ConnectionInfo: commonModels.ConnectionInfo{"mount_path": "/data"}},
		2: {ID: 2, TenantID: nil, EngineType: "minio", LifecycleState: "active", ConnectionInfo: commonModels.ConnectionInfo{"endpoint": "minio:9000", "access_key": "key", "secret_key": "secret"}},
	})
	workflow := map[string]interface{}{"tasks": []interface{}{map[string]interface{}{
		"id": "convert", "operator": "las_to_copc", "depends_on": []interface{}{},
		"params": map[string]interface{}{
			"locator":               "addp://engine/1/path/points/cloud.las?type=file&item_id=8",
			"target_parent_locator": "addp://engine/2/path/infra/results?type=prefix",
			"target_name":           "cloud.copc.laz", "write_mode": "create",
		},
	}}}
	_, _, err := svc.preprocessWorkflowParamsWithTargets(context.Background(), tenantID, "pointcloud_workflow", workflow)
	if err == nil || !strings.Contains(err.Error(), "target engine must be a tenant business engine") {
		t.Fatalf("error = %v, want infra target rejection", err)
	}
}

func TestConversionAdapterDerivesObjectStoreParentSource(t *testing.T) {
	svc := newWorkflowEngineServiceWithEnginesForTest(t, map[uint]commonModels.Engine{
		3: {
			ID: 3, EngineType: "minio", LifecycleState: "active",
			ConnectionInfo: commonModels.ConnectionInfo{"endpoint": "minio:9000", "access_key": "key", "secret_key": "secret"},
		},
		4: {
			ID: 4, EngineType: "nfs", LifecycleState: "active",
			ConnectionInfo: commonModels.ConnectionInfo{"mount_path": "/business"},
		},
	})
	workflow := map[string]interface{}{"tasks": []interface{}{map[string]interface{}{
		"id": "convert", "operator": "gltf_to_glb", "depends_on": []interface{}{},
		"params": map[string]interface{}{
			"locator":               "addp://engine/3/path/bucket/prefix/model.gltf?type=object&item_id=8",
			"target_parent_locator": "addp://engine/4/path/models/output?type=directory",
			"target_name":           "model.glb", "write_mode": "create",
		},
	}}}
	got, _, err := svc.preprocessWorkflowParamsWithTargets(context.Background(), 7, "model3d_workflow", workflow)
	if err != nil {
		t.Fatalf("preprocess object store parent source: %v", err)
	}
	plan := firstTaskParams(t, got)["access_plan"].(commonModels.JSONMap)
	source := plan["source"].(commonModels.JSONMap)
	access := source["access"].(commonModels.JSONMap)
	if source["kind"] != "directory" || source["entrypoint"] != "model.gltf" ||
		access["method"] != "object_store" || access["bucket"] != "bucket" || access["prefix"] != "prefix" {
		t.Fatalf("source = %#v", source)
	}
}

type workflowEngineServiceTestHarness struct {
	service  *WorkflowEngineService
	resolver *workflowEngineAccessResolver
}

func (h *workflowEngineServiceTestHarness) preprocessWorkflowParams(
	ctx context.Context,
	tenantID uint,
	workflowEngineType string,
	workflowDef map[string]interface{},
) (map[string]interface{}, error) {
	return h.service.preprocessWorkflowParams(ctx, tenantID, workflowEngineType, workflowDef, h.resolver)
}

func (h *workflowEngineServiceTestHarness) preprocessWorkflowParamsWithTargets(
	ctx context.Context,
	tenantID uint,
	workflowEngineType string,
	workflowDef map[string]interface{},
) (map[string]interface{}, []WorkflowProducedTarget, error) {
	return h.service.preprocessWorkflowParamsWithTargets(ctx, tenantID, workflowEngineType, workflowDef, h.resolver)
}

func (h *workflowEngineServiceTestHarness) workflowRuntimeEngineID(
	tenantID uint,
	engineType string,
	config models.WorkflowExecutionConfig,
) (uint, error) {
	return h.service.workflowRuntimeEngineID(context.Background(), tenantID, engineType, config, h.resolver)
}

func (h *workflowEngineServiceTestHarness) ExecuteWorkflow(
	ctx context.Context,
	tenantID uint,
	workflowDef map[string]interface{},
	inputData map[string]interface{},
	executionConfig string,
) (*WorkflowResponse, error) {
	authorization := &IssuedWorkflowExecutionAuthorization{
		AuthorizationID: 1,
		Effects:         []string{"read"},
		ExpiresAt:       time.Now().Add(time.Hour),
	}
	return h.service.executeWorkflowWithResolver(
		ctx, tenantID, uuid.New(), workflowDef, inputData, executionConfig, authorization, h.resolver,
	)
}

func newWorkflowEngineServiceForTest(t *testing.T, engineID uint, engineType string) *workflowEngineServiceTestHarness {
	t.Helper()
	return newWorkflowEngineServiceWithEnginesForTest(t, map[uint]commonModels.Engine{
		engineID: {
			ID:             engineID,
			Name:           "test-engine",
			EngineType:     engineType,
			LifecycleState: "active",
			ConnectionInfo: commonModels.ConnectionInfo{
				"host":     "localhost",
				"port":     "5432",
				"user":     "addp",
				"password": "secret",
				"database": "addp",
			},
		},
	})
}

func newWorkflowEngineServiceWithEnginesForTest(t *testing.T, engines map[uint]commonModels.Engine) *workflowEngineServiceTestHarness {
	t.Helper()
	tenantID := uint(7)
	for id, engine := range engines {
		if engine.TenantID == nil {
			engine.TenantID = &tenantID
			engines[id] = engine
		}
	}
	return newWorkflowEngineServiceWithRawEnginesForTest(t, engines)
}

func newWorkflowEngineServiceWithRawEnginesForTest(t *testing.T, engines map[uint]commonModels.Engine) *workflowEngineServiceTestHarness {
	t.Helper()
	cached := make(map[uint]*commonModels.Engine, len(engines))
	for id, engine := range engines {
		if engine.ConnectionStatus == "" {
			engine.ConnectionStatus = commonModels.EngineConnectionOnline
		}
		copy := engine
		cached[id] = &copy
	}
	authorization := &IssuedWorkflowExecutionAuthorization{AuthorizationID: 1, ExpiresAt: time.Now().Add(time.Hour)}
	workflowService := NewWorkflowEngineService(nil)
	workflowService.SetProtectionGate(allowDevelopProtectionGate{})
	return &workflowEngineServiceTestHarness{
		service: workflowService,
		resolver: &workflowEngineAccessResolver{
			tenantID: 7, executionID: uuid.New(), authorization: authorization, engines: cached,
		},
	}
}

func testEngine(id uint, engineType string, active bool) commonModels.Engine {
	lifecycleState := commonModels.EngineLifecycleDisabled
	if active {
		lifecycleState = commonModels.EngineLifecycleActive
	}
	return commonModels.Engine{
		ID:             id,
		Name:           "test-" + engineType,
		EngineType:     engineType,
		LifecycleState: lifecycleState,
		ConnectionInfo: commonModels.ConnectionInfo{
			"host": "localhost",
			"port": "10000",
		},
	}
}

type testWorkflowRuntimePlugin struct {
	engineType string
	mu         sync.Mutex
	statuses   []plugin.WorkflowExecutionStatus
	calls      int
}

func (p *testWorkflowRuntimePlugin) Type() string { return p.engineType }

func (p *testWorkflowRuntimePlugin) DisplayName() string { return "Test Workflow Runtime" }

func (p *testWorkflowRuntimePlugin) EngineOrigin() string { return "extension" }

func (p *testWorkflowRuntimePlugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	return nil
}

func (p *testWorkflowRuntimePlugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return nil
}

func (p *testWorkflowRuntimePlugin) DefaultPort() int { return 0 }

func (p *testWorkflowRuntimePlugin) RequiredFields() []string { return nil }

func (p *testWorkflowRuntimePlugin) SensitiveFields() []string { return nil }

func (p *testWorkflowRuntimePlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{}
}

func (p *testWorkflowRuntimePlugin) RuntimeEndpoint(ctx context.Context, connInfo plugin.ConnectionInfo) (string, error) {
	return "", nil
}

func (p *testWorkflowRuntimePlugin) ListOperators(ctx context.Context, connInfo plugin.ConnectionInfo) ([]plugin.OperatorDescriptor, error) {
	return []plugin.OperatorDescriptor{
		{
			ID:             "async_op",
			Name:           "async_op",
			DisplayName:    "Async Operator",
			EngineType:     p.engineType,
			Category:       "测试",
			CategoryPath:   []string{"测试"},
			Description:    "Async operator for tests",
			ExecutionModes: []string{"workflow"},
			Effects:        []string{"read"},
			Parameters:     []plugin.ParameterDescriptor{},
			OutputPorts: []plugin.OutputPortDescriptor{
				{Name: "default", Type: "object", IsDefault: true},
			},
		},
	}, nil
}

func (p *testWorkflowRuntimePlugin) ExecuteWorkflow(ctx context.Context, connInfo plugin.ConnectionInfo, req plugin.WorkflowExecuteRequest) (*plugin.WorkflowExecuteResult, error) {
	return &plugin.WorkflowExecuteResult{Status: "running", ExecutionID: "runtime-exec-1"}, nil
}

func (p *testWorkflowRuntimePlugin) InvokeOperator(ctx context.Context, connInfo plugin.ConnectionInfo, operatorName string, req plugin.OperatorInvokeRequest) (*plugin.OperatorInvokeResult, error) {
	return nil, fmt.Errorf("not implemented")
}

func (p *testWorkflowRuntimePlugin) GetExecutionStatus(ctx context.Context, connInfo plugin.ConnectionInfo, executionID string) (*plugin.WorkflowExecutionStatus, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	index := p.calls
	if index >= len(p.statuses) {
		index = len(p.statuses) - 1
	}
	p.calls++
	status := p.statuses[index]
	return &status, nil
}

func (p *testWorkflowRuntimePlugin) statusCallCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func firstTaskParams(t *testing.T, workflow map[string]interface{}) map[string]interface{} {
	t.Helper()
	tasks, ok := workflow["tasks"].([]interface{})
	if !ok || len(tasks) == 0 {
		t.Fatalf("tasks missing in workflow: %#v", workflow)
	}
	task, ok := tasks[0].(map[string]interface{})
	if !ok {
		t.Fatalf("task has unexpected type: %T", tasks[0])
	}
	params, ok := task["params"].(map[string]interface{})
	if !ok {
		t.Fatalf("params has unexpected type: %T", task["params"])
	}
	return params
}

func assertConnectionInfo(t *testing.T, params map[string]interface{}, engineType string) {
	t.Helper()
	if _, ok := params["engine_id"]; ok {
		t.Fatalf("engine_id should be replaced by connection_info: %#v", params)
	}
	conn, ok := params["connection_info"].(map[string]interface{})
	if !ok {
		t.Fatalf("connection_info missing or wrong type: %#v", params["connection_info"])
	}
	if conn["engine_type"] != engineType {
		t.Fatalf("connection_info.engine_type = %v, want %s", conn["engine_type"], engineType)
	}
}

func uintText(value uint) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	i := len(digits)
	for value > 0 {
		i--
		digits[i] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[i:])
}

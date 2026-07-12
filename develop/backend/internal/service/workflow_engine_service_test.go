package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/models"
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

	got, err := svc.preprocessWorkflowParams(context.Background(), "geopython_workflow", workflow)
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

	got, err := svc.preprocessWorkflowParams(context.Background(), "supermap_workflow", workflow)
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

	got, targets, err := svc.preprocessWorkflowParamsWithTargets(context.Background(), "supermap_workflow", workflow)
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
			ID:         3,
			Name:       "test-nfs",
			EngineType: "nfs",
			IsActive:   true,
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

	got, targets, err := svc.preprocessWorkflowParamsWithTargets(context.Background(), "supermap_workflow", workflow)
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

	got, err := svc.preprocessWorkflowParams(context.Background(), "geopython_workflow", workflow)
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

	got, err := svc.preprocessWorkflowParams(context.Background(), "geopython_workflow", workflow)
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

	got, err := svc.preprocessWorkflowParams(context.Background(), "spark_workflow", workflow)
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

	got, err := svc.preprocessWorkflowParams(context.Background(), "geopython_workflow", workflow)
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

	got, err := svc.preprocessWorkflowParams(context.Background(), "spark_workflow", workflow)
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

	if _, err := svc.preprocessWorkflowParams(context.Background(), "geopython_workflow", workflow); err == nil {
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

	if _, err := svc.preprocessWorkflowParams(context.Background(), "geopython_workflow", workflow); err == nil {
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

	_, err := svc.preprocessWorkflowParams(context.Background(), "geopython_workflow", workflow)
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

	_, err := svc.preprocessWorkflowParams(context.Background(), "geopython_workflow", workflow)
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

	if _, err := svc.preprocessWorkflowParams(context.Background(), "geopython_workflow", workflow); err == nil {
		t.Fatal("preprocessWorkflowParams() error = nil, want missing tasks error")
	}
}

func TestPreprocessWorkflowParamsRejectsEmptyTasks(t *testing.T) {
	svc := newWorkflowEngineServiceForTest(t, 7, "postgresql")
	workflow := map[string]interface{}{
		"tasks": []interface{}{},
	}

	if _, err := svc.preprocessWorkflowParams(context.Background(), "geopython_workflow", workflow); err == nil {
		t.Fatal("preprocessWorkflowParams() error = nil, want empty tasks error")
	}
}

func TestWorkflowRuntimeOptionsMapsSparkClusterID(t *testing.T) {
	svc := newWorkflowEngineServiceWithEnginesForTest(t, map[uint]commonModels.Engine{
		34: testEngine(34, "spark", true),
	})
	cfg := models.WorkflowExecutionConfig{
		EngineSpecific: map[string]interface{}{
			"spark_cluster_id": float64(34),
		},
	}

	got, err := svc.workflowRuntimeOptions("spark_workflow", cfg)
	if err != nil {
		t.Fatalf("workflowRuntimeOptions() error = %v", err)
	}
	if got["engine_id"] != uint(34) {
		t.Fatalf("runtime engine_id = %#v, want 34", got["engine_id"])
	}
}

func TestWorkflowRuntimeOptionsMapsSparkClusterIDString(t *testing.T) {
	svc := newWorkflowEngineServiceWithEnginesForTest(t, map[uint]commonModels.Engine{
		34: testEngine(34, "spark", true),
	})
	cfg := models.WorkflowExecutionConfig{
		EngineSpecific: map[string]interface{}{
			"spark_cluster_id": "34",
		},
	}

	got, err := svc.workflowRuntimeOptions("spark_workflow", cfg)
	if err != nil {
		t.Fatalf("workflowRuntimeOptions() error = %v", err)
	}
	if got["engine_id"] != uint(34) {
		t.Fatalf("runtime engine_id = %#v, want 34", got["engine_id"])
	}
}

func TestWorkflowRuntimeOptionsRequiresSparkClusterIDForSparkWorkflow(t *testing.T) {
	svc := newWorkflowEngineServiceWithEnginesForTest(t, nil)
	cfg := models.WorkflowExecutionConfig{}

	if _, err := svc.workflowRuntimeOptions("spark_workflow", cfg); err == nil {
		t.Fatal("workflowRuntimeOptions() error = nil, want missing spark_cluster_id error")
	}
}

func TestWorkflowRuntimeOptionsRejectsSparkClusterIDForNonSparkWorkflow(t *testing.T) {
	svc := newWorkflowEngineServiceWithEnginesForTest(t, nil)
	cfg := models.WorkflowExecutionConfig{
		EngineSpecific: map[string]interface{}{
			"spark_cluster_id": float64(34),
		},
	}

	if _, err := svc.workflowRuntimeOptions("geopython_workflow", cfg); err == nil {
		t.Fatal("workflowRuntimeOptions() error = nil, want non-spark workflow error")
	}
}

func TestWorkflowRuntimeOptionsRejectsInvalidSparkClusterID(t *testing.T) {
	svc := newWorkflowEngineServiceWithEnginesForTest(t, nil)
	cfg := models.WorkflowExecutionConfig{
		EngineSpecific: map[string]interface{}{
			"spark_cluster_id": float64(0),
		},
	}

	if _, err := svc.workflowRuntimeOptions("spark_workflow", cfg); err == nil {
		t.Fatal("workflowRuntimeOptions() error = nil, want invalid spark_cluster_id error")
	}
}

func TestWorkflowRuntimeOptionsRejectsNonSparkRuntimeEngine(t *testing.T) {
	svc := newWorkflowEngineServiceWithEnginesForTest(t, map[uint]commonModels.Engine{
		34: testEngine(34, "postgresql", true),
	})
	cfg := models.WorkflowExecutionConfig{
		EngineSpecific: map[string]interface{}{
			"spark_cluster_id": float64(34),
		},
	}

	if _, err := svc.workflowRuntimeOptions("spark_workflow", cfg); err == nil {
		t.Fatal("workflowRuntimeOptions() error = nil, want non-spark runtime engine error")
	}
}

func TestWorkflowRuntimeOptionsRejectsInactiveSparkRuntimeEngine(t *testing.T) {
	svc := newWorkflowEngineServiceWithEnginesForTest(t, map[uint]commonModels.Engine{
		34: testEngine(34, "spark", false),
	})
	cfg := models.WorkflowExecutionConfig{
		EngineSpecific: map[string]interface{}{
			"spark_cluster_id": float64(34),
		},
	}

	if _, err := svc.workflowRuntimeOptions("spark_workflow", cfg); err == nil {
		t.Fatal("workflowRuntimeOptions() error = nil, want inactive spark runtime engine error")
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

func newWorkflowEngineServiceForTest(t *testing.T, engineID uint, engineType string) *WorkflowEngineService {
	t.Helper()
	return newWorkflowEngineServiceWithEnginesForTest(t, map[uint]commonModels.Engine{
		engineID: {
			ID:         engineID,
			Name:       "test-engine",
			EngineType: engineType,
			IsActive:   true,
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

func newWorkflowEngineServiceWithEnginesForTest(t *testing.T, engines map[uint]commonModels.Engine) *WorkflowEngineService {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "/api/v1/internal/engines/"
		if !strings.HasPrefix(r.URL.Path, prefix) {
			http.NotFound(w, r)
			return
		}
		id64, err := strconv.ParseUint(strings.TrimPrefix(r.URL.Path, prefix), 10, 32)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		engine, ok := engines[uint(id64)]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(engine)
	}))
	t.Cleanup(server.Close)
	return NewWorkflowEngineService(commonClient.NewSystemClientWithInternalKey(server.URL, "internal-key"))
}

func testEngine(id uint, engineType string, active bool) commonModels.Engine {
	return commonModels.Engine{
		ID:         id,
		Name:       "test-" + engineType,
		EngineType: engineType,
		IsActive:   active,
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

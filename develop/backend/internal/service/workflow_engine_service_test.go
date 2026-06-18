package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
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
			},
		},
	}

	got, err := svc.preprocessWorkflowParams(context.Background(), workflow)
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
}

func TestPreprocessWorkflowParamsDerivesTableTargetFromParentLocator(t *testing.T) {
	svc := newWorkflowEngineServiceForTest(t, 7, "postgresql")
	workflow := map[string]interface{}{
		"tasks": []interface{}{
			map[string]interface{}{
				"id":       "save_result",
				"operator": "save",
				"params": map[string]interface{}{
					"target_type":           "table",
					"target_parent_locator": "addp://engine/7/path/analytics?type=schema&node_id=23",
					"target_name":           "result_table",
				},
			},
		},
	}

	got, err := svc.preprocessWorkflowParams(context.Background(), workflow)
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

func TestPreprocessWorkflowParamsRequiresTargetNameWithParentLocator(t *testing.T) {
	svc := newWorkflowEngineServiceForTest(t, 7, "postgresql")
	workflow := map[string]interface{}{
		"tasks": []interface{}{
			map[string]interface{}{
				"id":       "save_result",
				"operator": "save",
				"params": map[string]interface{}{
					"target_type":           "table",
					"target_parent_locator": "addp://engine/7/path/analytics?type=schema&node_id=23",
				},
			},
		},
	}

	if _, err := svc.preprocessWorkflowParams(context.Background(), workflow); err == nil {
		t.Fatal("preprocessWorkflowParams() error = nil, want target_name error")
	}
}

func newWorkflowEngineServiceForTest(t *testing.T, engineID uint, engineType string) *WorkflowEngineService {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/internal/engines/"+uintText(engineID) {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(commonModels.Engine{
			ID:         engineID,
			Name:       "test-engine",
			EngineType: engineType,
			ConnectionInfo: commonModels.ConnectionInfo{
				"host":     "localhost",
				"port":     "5432",
				"user":     "addp",
				"password": "secret",
				"database": "addp",
			},
		})
	}))
	t.Cleanup(server.Close)
	return NewWorkflowEngineService(commonClient.NewSystemClientWithInternalKey(server.URL, "internal-key"))
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

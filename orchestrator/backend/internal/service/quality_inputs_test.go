package service

import (
	"context"
	"encoding/json"
	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/orchestrator/internal/models"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestQualityPlanReceivesResolvedUpstreamTargets(t *testing.T) {
	const locator = "addp://engine/12/path/west/orders?type=table"
	const schema = `{"type":"object","properties":{"table_bindings":{"type":"object","properties":{"orders":{"type":"string","format":"resource-locator","minLength":1}},"required":["orders"],"additionalProperties":false}},"required":["table_bindings"],"additionalProperties":false}`
	var received map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/quality/task-provider/tasks/quality_plan/42/execute":
			if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			w.WriteHeader(202)
			json.NewEncoder(w).Encode(map[string]string{"execution_id": "child"})
		case "/api/v1/quality/task-provider/executions/child":
			json.NewEncoder(w).Encode(map[string]interface{}{"execution_id": "child", "status": "success", "outputs": map[string]interface{}{"passed": true}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	executor := &Executor{
		taskProviderResolver: taskProviderResolverWithProvider(&commonModels.TaskProvider{ModuleName: "quality", Backends: taskProviderBackendsForTest(server.URL), Available: true, TaskProviderDeclaration: commonModels.TaskProviderDeclaration{TaskExecuteEndpoint: "/api/v1/quality/task-provider/tasks/{task_type}/{id}/execute", TaskStatusEndpoint: "/api/v1/quality/task-provider/executions/{execution_id}", Capabilities: jsonStringPtr(taskCapabilitiesForTest("quality_plan", false, schema))}}, schema),
		serviceTokens:        commonClient.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) { return "test-orchestrator-token", nil }),
	}
	parameters := map[string]interface{}{"table_bindings": map[string]interface{}{"orders": "{{materialize.outputs.target.locator}}"}}
	upstream := models.StepResults{"materialize": {Status: "success", Result: map[string]interface{}{"outputs": map[string]interface{}{"target": map[string]interface{}{"locator": locator}}}}}
	resolved, err := executor.resolveTemplateReferences(parameters, upstream)
	if err != nil {
		t.Fatal(err)
	}
	result, err := executor.executeWithTaskProvider(context.Background(), &models.Step{Provider: "quality", TaskType: "quality_plan", TaskID: 42, Timeout: 10}, resolved, time.Now(), "parent", "manual", 7)
	if err != nil || result.Status != "success" {
		t.Fatalf("execution %+v %v", result, err)
	}
	if received["parent_execution_id"] != "parent" || received["source"] != "orchestrator" {
		t.Fatalf("lineage lost: %v", received)
	}
	targets := received["parameters"].(map[string]interface{})["table_bindings"].(map[string]interface{})
	if targets["orders"] != locator {
		t.Fatalf("target %v", targets)
	}
	delete(upstream, "materialize")
	if _, err = executor.resolveTemplateReferences(parameters, upstream); err == nil {
		t.Fatal("missing output fell back instead of failing")
	}
}

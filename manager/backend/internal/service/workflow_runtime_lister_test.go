package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	commonClient "github.com/addp/common/client"
)

type workflowRuntimeListerTokenSource struct{}

func (workflowRuntimeListerTokenSource) Token(context.Context, uint) (string, error) {
	return "manager-test-token", nil
}

func (workflowRuntimeListerTokenSource) PlatformToken(context.Context) (string, error) {
	return "manager-platform-token", nil
}

func TestWorkflowRuntimeEngineListerUsesRuntimeDescriptors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/system/runtime/engine-descriptors" {
			t.Fatalf("request = %s %s, want Runtime Descriptor list", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("page") != "1" || r.URL.Query().Get("page_size") != "100" {
			t.Fatalf("query = %q, want page=1&page_size=100", r.URL.RawQuery)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer manager-test-token" {
			t.Fatalf("Authorization = %q, want Bearer manager-test-token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]interface{}{
				{
					"id": 11, "name": "Tenant Workflow", "engine_type": "tenant_workflow",
					"lifecycle_state": "active", "connection_status": "online",
					"runtime_endpoint": map[string]interface{}{"protocol": "http", "host": "runtime", "port": 8103},
					"capabilities": map[string]interface{}{
						"schema_version": "engine.capabilities/v1", "engine_type": "tenant_workflow", "engine_family": "workflow",
						"compute": map[string]interface{}{"workflow": map[string]interface{}{"supported": true, "runtime_api": "addp.workflow/v1", "dynamic_operators": true}},
					},
				},
				{
					"id": 12, "name": "Query Runtime", "engine_type": "query_runtime", "lifecycle_state": "active", "connection_status": "online",
					"runtime_endpoint": map[string]interface{}{"protocol": "http", "host": "query", "port": 8104},
					"capabilities": map[string]interface{}{
						"schema_version": "engine.capabilities/v1", "engine_type": "query_runtime", "engine_family": "query_runtime",
						"compute": map[string]interface{}{"query": map[string]interface{}{"supported": true, "runtime_api": "addp.query-runtime/v1"}},
					},
				},
			},
			"total": 2, "page": 1, "page_size": 100,
		})
	}))
	defer server.Close()

	client := commonClient.NewSystemServiceClient(server.URL, workflowRuntimeListerTokenSource{}, server.Client())
	engines, err := NewWorkflowRuntimeEngineLister(client).ListWorkflowEngines(7)
	if err != nil {
		t.Fatalf("ListWorkflowEngines() error = %v", err)
	}
	if len(engines) != 1 || engines[0].ID != 11 || engines[0].ConnectionInfo["host"] != "runtime" {
		t.Fatalf("workflow engines = %#v, want descriptor-projected tenant workflow", engines)
	}
}

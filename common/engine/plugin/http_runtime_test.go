package plugin

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"testing"
)

func TestHTTPExecuteWorkflowIncludesRuntimeFields(t *testing.T) {
	var got map[string]interface{}
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/workflow" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"success","execution_id":"exec-1","final_result":{"value":42}}`))
		}),
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	go func() {
		_ = server.Serve(listener)
	}()

	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	result, err := HTTPExecuteWorkflow(context.Background(), ConnectionInfo{
		"protocol": "http",
		"host":     "127.0.0.1",
		"port":     port,
	}, WorkflowExecuteRequest{
		WorkflowDef: map[string]interface{}{
			"tasks": []interface{}{},
		},
		InputData: map[string]interface{}{
			"input": "value",
		},
		Runtime: map[string]interface{}{
			"engine_id": float64(34),
		},
	})
	if err != nil {
		t.Fatalf("HTTPExecuteWorkflow returned error: %v", err)
	}
	if result.Status != "success" || result.ExecutionID != "exec-1" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if got["engine_id"] != float64(34) {
		t.Fatalf("engine_id was not included at top level: %#v", got)
	}
	if _, ok := got["workflow_def"].(map[string]interface{}); !ok {
		t.Fatalf("workflow_def missing from request: %#v", got)
	}
	if inputData, ok := got["input_data"].(map[string]interface{}); !ok || inputData["input"] != "value" {
		t.Fatalf("input_data missing from request: %#v", got)
	}
}

package plugin

import (
	"context"
	"encoding/base64"
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
			_, _ = w.Write([]byte(`{"status":"success","execution_id":"exec-1","final_result":{"value":42},"execution_time_ms":12.5}`))
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
			"input":     "value",
			"engine_id": float64(99),
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
	if result.ExecutionTimeMs == nil || *result.ExecutionTimeMs != 12.5 {
		t.Fatalf("execution_time_ms = %#v, want 12.5", result.ExecutionTimeMs)
	}
	if _, ok := result.Result["execution_time_ms"]; ok {
		t.Fatalf("execution_time_ms should be parsed as top-level standard field: %#v", result.Result)
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
	if inputData := got["input_data"].(map[string]interface{}); inputData["engine_id"] != float64(99) {
		t.Fatalf("input_data engine_id should remain input-only: %#v", got)
	}
}

func TestHTTPExecuteWorkflowReturnsErrorForFailedStatusWithOKHTTPStatus(t *testing.T) {
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/workflow" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"failed","execution_id":"exec-1","error":"工作流失败","error_code":"EXECUTION_FAILED","details":"runtime details"}`))
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
	}, WorkflowExecuteRequest{})
	if err == nil {
		t.Fatal("HTTPExecuteWorkflow returned nil error for failed runtime status")
	}
	if result == nil || result.Status != "failed" || result.ExecutionID != "exec-1" || result.ErrorCode != "EXECUTION_FAILED" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestHTTPInvokeOperatorIncludesRuntimeFields(t *testing.T) {
	var got map[string]interface{}
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/operators/tiff_to_cog/invoke" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"success","execution_id":"invoke-1","result":{"artifact_id":7},"execution_time_ms":8}`))
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

	result, err := HTTPInvokeOperator(context.Background(), ConnectionInfo{
		"protocol": "http",
		"host":     "127.0.0.1",
		"port":     port,
	}, "tiff_to_cog", OperatorInvokeRequest{
		Params: map[string]interface{}{
			"source_uri": "s3://bucket/source.tif",
		},
		Runtime: map[string]interface{}{
			"engine_id": float64(34),
		},
	})
	if err != nil {
		t.Fatalf("HTTPInvokeOperator returned error: %v", err)
	}
	if result.Status != "success" || result.ExecutionID != "invoke-1" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.ExecutionTimeMs == nil || *result.ExecutionTimeMs != 8 {
		t.Fatalf("execution_time_ms = %#v, want 8", result.ExecutionTimeMs)
	}
	if _, ok := result.Result["execution_time_ms"]; ok {
		t.Fatalf("execution_time_ms should be parsed as top-level standard field: %#v", result.Result)
	}
	if got["engine_id"] != float64(34) {
		t.Fatalf("engine_id was not included at top level: %#v", got)
	}
	params, ok := got["params"].(map[string]interface{})
	if !ok || params["source_uri"] != "s3://bucket/source.tif" {
		t.Fatalf("params missing from request: %#v", got)
	}
}

func TestHTTPInvokeOperatorIncludesBinaryPayload(t *testing.T) {
	var got map[string]interface{}
	responsePayload := []byte("binary-result")
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/operators/vector_reproject/invoke" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"success","execution_id":"invoke-2","binary_payload":{"content_type":"application/vnd.apache.arrow.stream","encoding":"arrow","name":"geometry_batch","data":"` + base64.StdEncoding.EncodeToString(responsePayload) + `","metadata":{"geometry_column":"geom"}}}`))
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

	result, err := HTTPInvokeOperator(context.Background(), ConnectionInfo{
		"protocol": "http",
		"host":     "127.0.0.1",
		"port":     port,
	}, "vector_reproject", OperatorInvokeRequest{
		Params: map[string]interface{}{
			"source_crs": "EPSG:3857",
			"target_crs": "EPSG:4326",
		},
		BinaryPayload: &BinaryPayload{
			ContentType: "application/vnd.apache.arrow.stream",
			Encoding:    "arrow",
			Name:        "geometry_batch",
			Data:        []byte("request-bytes"),
			Metadata: map[string]interface{}{
				"geometry_column": "geom",
			},
		},
	})
	if err != nil {
		t.Fatalf("HTTPInvokeOperator returned error: %v", err)
	}
	if result.BinaryPayload == nil || string(result.BinaryPayload.Data) != string(responsePayload) {
		t.Fatalf("binary payload not parsed: %+v", result.BinaryPayload)
	}
	if gotPayload, ok := got["binary_payload"].(map[string]interface{}); !ok || gotPayload["name"] != "geometry_batch" {
		t.Fatalf("binary payload missing from request: %#v", got)
	}
}

func TestHTTPInvokeOperatorReturnsErrorForFailedStatusWithOKHTTPStatus(t *testing.T) {
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/operators/tiff_to_cog/invoke" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"failed","execution_id":"invoke-1","error":"算子失败","error_code":"EXECUTION_FAILED","details":"runtime details"}`))
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

	result, err := HTTPInvokeOperator(context.Background(), ConnectionInfo{
		"protocol": "http",
		"host":     "127.0.0.1",
		"port":     port,
	}, "tiff_to_cog", OperatorInvokeRequest{})
	if err == nil {
		t.Fatal("HTTPInvokeOperator returned nil error for failed runtime status")
	}
	if result == nil || result.Status != "failed" || result.ExecutionID != "invoke-1" || result.ErrorCode != "EXECUTION_FAILED" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestHTTPInvokeOperatorParsesErrorDetails(t *testing.T) {
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/operators/tiff_to_cog/invoke" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"status":"failed","error":"算子执行失败","error_code":"EXECUTION_FAILED","details":"gdal_translate stderr"}`))
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

	result, err := HTTPInvokeOperator(context.Background(), ConnectionInfo{
		"protocol": "http",
		"host":     "127.0.0.1",
		"port":     port,
	}, "tiff_to_cog", OperatorInvokeRequest{})
	if err == nil {
		t.Fatal("HTTPInvokeOperator returned nil error for failed status")
	}
	if result == nil || result.Status != "failed" || result.Error != "算子执行失败" || result.ErrorCode != "EXECUTION_FAILED" || result.Details != "gdal_translate stderr" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestHTTPGetExecutionStatusParsesStandardFields(t *testing.T) {
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/executions/runtime-exec-1" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"success","execution_id":"runtime-exec-1","result":{"value":42},"all_results":{"task1":{"value":42}},"message":"done","task_order":["task1"],"progress":100,"started_at":"2026-06-24T12:00:00Z","execution_time_ms":12.5}`))
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

	status, err := HTTPGetExecutionStatus(context.Background(), ConnectionInfo{
		"protocol": "http",
		"host":     "127.0.0.1",
		"port":     port,
	}, "runtime-exec-1")
	if err != nil {
		t.Fatalf("HTTPGetExecutionStatus returned error: %v", err)
	}
	if status.Status != "success" || status.ExecutionID != "runtime-exec-1" || status.Progress != 100 {
		t.Fatalf("unexpected status: %+v", status)
	}
	if status.ExecutionTimeMs == nil || *status.ExecutionTimeMs != 12.5 {
		t.Fatalf("execution_time_ms = %#v, want 12.5", status.ExecutionTimeMs)
	}
	if len(status.TaskOrder) != 1 || status.TaskOrder[0] != "task1" {
		t.Fatalf("task_order = %#v, want task1", status.TaskOrder)
	}
	if status.AllResults["task1"] == nil {
		t.Fatalf("all_results not parsed: %+v", status)
	}
}

func TestHTTPGetExecutionStatusParsesError(t *testing.T) {
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/executions/missing" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"status":"failed","error":"Execution not found","error_code":"EXECUTION_NOT_FOUND"}`))
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

	status, err := HTTPGetExecutionStatus(context.Background(), ConnectionInfo{
		"protocol": "http",
		"host":     "127.0.0.1",
		"port":     port,
	}, "missing")
	if err == nil {
		t.Fatal("HTTPGetExecutionStatus returned nil error for failed status")
	}
	if status == nil || status.Status != "failed" || status.ErrorCode != "EXECUTION_NOT_FOUND" {
		t.Fatalf("unexpected status: %+v", status)
	}
}

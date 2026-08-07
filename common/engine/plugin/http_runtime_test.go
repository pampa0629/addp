package plugin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestHTTPExecuteWorkflowUsesCanonicalRequestShape(t *testing.T) {
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
		EngineID: 34,
		Runtime: &WorkflowRuntimeContext{
			TenantID: 7,
			ExecutionAuthorization: WorkflowExecutionAuthorization{
				ID:      71,
				Effects: []string{"read"},
			},
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
	runtimeOptions, ok := got["runtime"].(map[string]interface{})
	if !ok {
		t.Fatalf("runtime was not included as an object: %#v", got)
	}
	if runtimeOptions["tenant_id"] != float64(7) {
		t.Fatalf("runtime tenant_id missing: %#v", runtimeOptions)
	}
	authorization, ok := runtimeOptions["execution_authorization"].(map[string]interface{})
	if !ok || authorization["id"] != float64(71) {
		t.Fatalf("runtime execution authorization missing: %#v", got)
	}
	if _, exists := got["execution_authorization"]; exists {
		t.Fatalf("execution_authorization must not be flattened: %#v", got)
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

func TestHTTPListOperatorsPreservesParameterType(t *testing.T) {
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/operators" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"status": "success",
				"operators": [{
					"id": "dataset.select",
					"name": "dataset.select",
					"display_name": "选择矢量数据集",
					"engine_type": "supermap_workflow",
					"category": "数据集",
					"category_path": ["数据集"],
					"description": "从 Datasource 中选择 DatasetVector。",
					"execution_modes": ["workflow"],
					"effects": ["read"],
					"parameters": [
						{"name": "datasource", "type": "object", "param_type": "input", "required": true, "description": "上游 Datasource 引用。"},
						{"name": "dataset_name", "type": "string", "param_type": "param", "required": true, "description": "数据集名称。"}
					],
					"output_ports": [{"name": "dataset", "type": "object", "is_default": true}]
				}]
			}`))
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

	operators, err := HTTPListOperators(context.Background(), ConnectionInfo{
		"protocol": "http",
		"host":     "127.0.0.1",
		"port":     port,
	})
	if err != nil {
		t.Fatalf("HTTPListOperators returned error: %v", err)
	}
	if len(operators) != 1 || len(operators[0].Parameters) != 2 {
		t.Fatalf("unexpected operators: %+v", operators)
	}
	if operators[0].Parameters[0].ParamType != "input" {
		t.Fatalf("param_type was not preserved: %+v", operators[0].Parameters[0])
	}
	if operators[0].Parameters[1].ParamType != "param" {
		t.Fatalf("param_type was not preserved: %+v", operators[0].Parameters[1])
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

func TestHTTPInvokeOperatorUsesCanonicalRequestShape(t *testing.T) {
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
		EngineID: 34,
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
	if _, exists := got["engine_id"]; exists {
		t.Fatalf("engine_id must be omitted when the caller does not bind a Spark engine: %#v", got)
	}
}

func TestHTTPInvokeOperatorUsesRequestTimeout(t *testing.T) {
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(50 * time.Millisecond)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"success"}`))
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

	_, err = HTTPInvokeOperator(context.Background(), ConnectionInfo{
		"protocol": "http",
		"host":     "127.0.0.1",
		"port":     port,
	}, "build_raster_mosaic", OperatorInvokeRequest{Timeout: time.Millisecond})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "timeout") {
		t.Fatalf("HTTPInvokeOperator error = %v, want timeout", err)
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

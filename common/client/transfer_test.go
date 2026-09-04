package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTransferClientCreateExecutionUsesOneOffExecutionRoute(t *testing.T) {
	t.Parallel()

	var request CreateTransferExecutionRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/transfer/executions" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer addp_at_transfer_token" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"execution_id":"transfer-exec-1","status":"pending"}`))
	}))
	defer server.Close()

	client := NewTransferClient(server.URL, staticTenantToken("addp_at_transfer_token"))
	result, err := client.CreateExecution(context.Background(), &CreateTransferExecutionRequest{
		Name: "query export", TenantID: 7,
		Config: TransferExecutionConfig{
			Runtime: TransferExecutionRuntime{Boundary: "bounded"},
			Load:    TransferExecutionLoad{Mode: "snapshot"},
			Source: TransferExecutionEndpoint{
				Locator: "addp://engine/1/path/public/orders?type=table", DataType: "table", Representation: "native",
				Query: &TransferExecutionQuery{
					Language: "sql", Statement: "SELECT * FROM orders",
					Inputs: []TransferExecutionQueryInput{{Name: "orders", Locator: "addp://engine/1/path/public/orders?type=table&item_id=42"}},
				},
			},
			Target: TransferExecutionEndpoint{
				ParentLocator: "addp://engine/2/path/exports?type=directory", Name: "orders.csv",
				DataType: "table", Representation: "encoded", Format: "csv",
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateExecution() error = %v", err)
	}
	if result.ExecutionID != "transfer-exec-1" || result.Status != "pending" {
		t.Fatalf("result = %#v", result)
	}
	if request.TenantID != 0 || request.Config.Target.Name != "orders.csv" {
		t.Fatalf("request = %#v", request)
	}
	if request.Config.Source.Query == nil || len(request.Config.Source.Query.Inputs) != 1 || request.Config.Source.Query.Inputs[0].Name != "orders" || request.Config.Source.Query.Inputs[0].Locator != "addp://engine/1/path/public/orders?type=table&item_id=42" {
		t.Fatalf("query inputs = %#v", request.Config.Source.Query)
	}
}

func TestTransferClientCreateTaskParsesDirectResponse(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotAuth string
	var gotLegacyHeaders bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotLegacyHeaders = r.Header.Get("X-Internal-API-Key") != "" || r.Header.Get("X-Tenant-ID") != ""
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":7,"name":"sync roads","status":"draft","auto_scan_metadata":true}`))
	}))
	defer server.Close()

	client := NewTransferClient(server.URL, ServiceTokenProviderFunc(func(_ context.Context, tenantID uint) (string, error) {
		if tenantID != 3 {
			t.Fatalf("tenant ID = %d, want 3", tenantID)
		}
		return "addp_at_transfer_token", nil
	}))
	result, err := client.CreateTask(&CreateTransferTaskRequest{
		Name:     "sync roads",
		TaskType: "sync",
		Config:   map[string]interface{}{"mode": "batch"},
		TenantID: 3,
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if gotPath != "/api/v1/transfer/task-definitions" {
		t.Fatalf("path = %q, want transfer task definition path", gotPath)
	}
	if gotAuth != "Bearer addp_at_transfer_token" || gotLegacyHeaders {
		t.Fatalf("auth = %q legacy_headers=%v", gotAuth, gotLegacyHeaders)
	}
	if result.ID != 7 {
		t.Fatalf("id = %d, want 7", result.ID)
	}
	if result.Name != "sync roads" {
		t.Fatalf("name = %q, want sync roads", result.Name)
	}
	if !result.AutoScanMetadata {
		t.Fatal("auto_scan_metadata = false, want true")
	}
}

func TestTransferClientCreateTaskRejectsWrappedResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"id":7,"name":"sync roads","status":"draft"}}`))
	}))
	defer server.Close()

	client := NewTransferClient(server.URL, staticTenantToken("addp_at_transfer_token"))
	if _, err := client.CreateTask(&CreateTransferTaskRequest{Name: "sync roads", TaskType: "sync"}); err == nil {
		t.Fatal("CreateTask() error = nil, want wrapped response rejection")
	}
}

func TestTransferClientTriggerTaskUsesExecutionUUID(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":10,"execution_id":"exec-uuid-10","status":"pending"}`))
	}))
	defer server.Close()

	client := NewTransferClient(server.URL, staticTenantToken("addp_at_transfer_token"))
	result, err := client.TriggerTask(7, 3)
	if err != nil {
		t.Fatalf("TriggerTask() error = %v", err)
	}
	if gotPath != "/api/v1/transfer/task-definitions/7/start" {
		t.Fatalf("path = %q, want transfer start path", gotPath)
	}
	if gotAuth != "Bearer addp_at_transfer_token" {
		t.Fatalf("authorization header = %q", gotAuth)
	}
	if result.ID != 10 {
		t.Fatalf("id = %d, want 10", result.ID)
	}
	if result.ExecutionID != "exec-uuid-10" {
		t.Fatalf("execution_id = %q, want exec-uuid-10", result.ExecutionID)
	}
}

func TestTransferClientTriggerTaskRejectsEmptyExecutionUUID(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":11,"status":"running"}`))
	}))
	defer server.Close()

	client := NewTransferClient(server.URL, staticTenantToken("addp_at_transfer_token"))
	if _, err := client.TriggerTask(8, 0); err == nil {
		t.Fatal("TriggerTask() error = nil, want empty response data error")
	}
}

func TestTransferClientGetExecutionParsesDirectResponse(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":21,"execution_id":"exec-uuid-21","task_id":7,"status":"success","progress":100,"metadata":{"records_written":12}}`))
	}))
	defer server.Close()

	client := NewTransferClient(server.URL, staticTenantToken("addp_at_transfer_token"))
	result, err := client.GetExecution("exec-uuid-21", 3)
	if err != nil {
		t.Fatalf("GetExecution() error = %v", err)
	}
	if gotPath != "/api/v1/transfer/executions/exec-uuid-21/result" {
		t.Fatalf("path = %q, want module-owned transfer execution result path", gotPath)
	}
	if gotAuth != "Bearer addp_at_transfer_token" {
		t.Fatalf("authorization header = %q", gotAuth)
	}
	if result.ExecutionID != "exec-uuid-21" {
		t.Fatalf("execution_id = %q, want exec-uuid-21", result.ExecutionID)
	}
	if result.Status != "success" {
		t.Fatalf("status = %q, want success", result.Status)
	}
	if result.Metadata["records_written"].(float64) != 12 {
		t.Fatalf("records_written = %v, want 12", result.Metadata["records_written"])
	}
}

func TestTransferClientGetExecutionRejectsWrappedResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"execution_id":"exec-uuid-21","status":"success"}}`))
	}))
	defer server.Close()

	client := NewTransferClient(server.URL, staticTenantToken("addp_at_transfer_token"))
	if _, err := client.GetExecution("exec-uuid-21", 0); err == nil {
		t.Fatal("GetExecution() error = nil, want wrapped response rejection")
	}
}

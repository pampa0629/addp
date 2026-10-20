package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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
		t.Fatal("User submission must not acquire Service credentials")
		return "addp_at_transfer_token", nil
	}))
	result, err := client.CreateTask(context.Background(), "addp_at_transfer_token", &CreateTransferTaskRequest{
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
	if _, err := client.CreateTask(context.Background(), "addp_at_transfer_token", &CreateTransferTaskRequest{Name: "sync roads", TaskType: "sync"}); err == nil {
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
	result, err := client.TriggerTask(context.Background(), "addp_at_transfer_token", 7)
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
	if _, err := client.TriggerTask(context.Background(), "addp_at_transfer_token", 8); err == nil {
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
	result, err := client.GetExecution(context.Background(), "exec-uuid-21", 3)
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
	if _, err := client.GetExecution(context.Background(), "exec-uuid-21", 0); err == nil {
		t.Fatal("GetExecution() error = nil, want wrapped response rejection")
	}
}

func TestTransferUserTaskSubmissionDoesNotLeakOrReuseCredentials(t *testing.T) {
	var calls, redirected int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == "/redirected" {
			redirected++
			return
		}
		if r.Header.Get("Authorization") != "Bearer current-user" {
			t.Error("current credential not forwarded")
		}
		w.Header().Set("Location", "/redirected")
		w.WriteHeader(http.StatusTemporaryRedirect)
		_, _ = w.Write([]byte("secret dependency body"))
	}))
	defer server.Close()
	client := NewTransferClient(server.URL, nil)
	for _, submit := range []func(context.Context, string) error{
		func(ctx context.Context, token string) error {
			_, err := client.CreateTask(ctx, token, &CreateTransferTaskRequest{Name: "import"})
			return err
		},
		func(ctx context.Context, token string) error { _, err := client.TriggerTask(ctx, token, 1); return err },
	} {
		before := calls
		for _, token := range []string{"", "invalid token", "user\nsecret"} {
			if err := submit(context.Background(), token); err == nil {
				t.Fatal("invalid credential accepted")
			}
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := submit(ctx, "current-user"); err == nil {
			t.Fatal("cancelled submission succeeded")
		}
		if calls != before {
			t.Fatal("invalid/cancelled request reached dependency")
		}
		err := submit(context.Background(), "current-user")
		var submission *TransferTaskSubmissionError
		if !errors.As(err, &submission) || submission.StatusCode != http.StatusTemporaryRedirect || strings.Contains(err.Error(), "secret") {
			t.Fatalf("unsafe submission result: %v", err)
		}
		if calls != before+1 || redirected != 0 {
			t.Fatal("User credential followed redirect")
		}
	}
	if client.httpClient.CheckRedirect != nil {
		t.Fatal("shared transport was mutated")
	}
}

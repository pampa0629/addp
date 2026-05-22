package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	commonModels "github.com/addp/common/models"
)

func TestTaskClientCreateTaskUsesWorkflowStandardAndExecutionID(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":       "success",
			"execution_id": "exec-123",
		})
	}))
	defer server.Close()

	connInfo, err := commonModels.ParseBaseURLToConnectionInfo(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	client := NewTaskClient(time.Second)
	taskID, err := client.CreateTask(context.Background(), &commonModels.Engine{
		EngineType:     "python_workflow",
		ConnectionInfo: connInfo,
	}, map[string]interface{}{
		"workflow_def": map[string]interface{}{"tasks": []interface{}{}},
	})
	if err != nil {
		t.Fatalf("CreateTask returned error: %v", err)
	}

	if gotPath != "/api/workflow" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if taskID != "exec-123" {
		t.Fatalf("unexpected task id: %s", taskID)
	}
}

func TestTaskClientGetTaskStatusUsesWorkflowStandard(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "success",
			"message": "done",
		})
	}))
	defer server.Close()

	connInfo, err := commonModels.ParseBaseURLToConnectionInfo(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	client := NewTaskClient(time.Second)
	status, err := client.GetTaskStatus(context.Background(), &commonModels.Engine{
		EngineType:     "python_workflow",
		ConnectionInfo: connInfo,
	}, "exec-123")
	if err != nil {
		t.Fatalf("GetTaskStatus returned error: %v", err)
	}

	if gotPath != "/api/executions/exec-123" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if status.Status != "success" || status.Message != "done" {
		t.Fatalf("unexpected status: %+v", status)
	}
}

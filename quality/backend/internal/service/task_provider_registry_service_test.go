package service

import (
	"context"
	"encoding/json"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/taskprovider"
	"net/http"
	"net/http/httptest"
	"testing"
)

type taskProviderTaskCapability struct {
	Type                    string `json:"type"`
	SupportsSchedule        bool   `json:"supports_schedule"`
	SupportsCancel          bool   `json:"supports_cancel"`
	SupportsInlineExecution bool   `json:"supports_inline_execution"`
	CreateURL               string `json:"create_url"`
	EditURL                 string `json:"edit_url"`
	Deprecated              bool   `json:"deprecated"`
}

type taskProviderTestTokenSource struct{}

func (taskProviderTestTokenSource) Token(context.Context, uint) (string, error) {
	return "tenant-token", nil
}
func (taskProviderTestTokenSource) PlatformToken(context.Context) (string, error) {
	return "platform-token", nil
}

func TestTaskProviderRegistryRegistersStandardQualityContract(t *testing.T) {
	var captured TaskProviderRegistration
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/system/runtime/task-providers" {
			t.Fatalf("path = %s, want OAuth runtime task provider route", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer platform-token" {
			t.Fatalf("Authorization = %q", got)
		}
		var payload commonModels.TaskProvider
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		captured = taskProviderRegistrationForTest(payload)
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	client := commonClient.NewSystemServiceClient(server.URL, taskProviderTestTokenSource{}, server.Client())
	registry := NewTaskProviderRegistryService(client, "http://quality.internal")
	if err := registry.Register(context.Background()); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if captured.ModuleName != "quality" {
		t.Fatalf("module_name = %q, want quality", captured.ModuleName)
	}
	if captured.BaseURL != "http://quality.internal" {
		t.Fatalf("base_url = %q, want http://quality.internal", captured.BaseURL)
	}
	if captured.TaskListEndpoint != "/api/v1/quality/tasks" ||
		captured.TaskDetailEndpoint != "/api/v1/quality/tasks/{task_type}/{id}" ||
		captured.TaskExecuteEndpoint != "/api/v1/quality/tasks/{task_type}/{id}/execute" ||
		captured.TaskStatusEndpoint != "/api/v1/quality/executions/{execution_id}" {
		t.Fatalf("standard endpoints not registered: %#v", captured)
	}

	var capabilities struct {
		SchemaVersion    string                       `json:"schema_version"`
		TaskCapabilities []taskProviderTaskCapability `json:"task_capabilities"`
	}
	if captured.Capabilities == nil {
		t.Fatal("capabilities is nil")
	}
	if _, err := taskprovider.ParseCapabilities(*captured.Capabilities); err != nil {
		t.Fatalf("capabilities contract invalid: %v; capabilities=%s", err, *captured.Capabilities)
	}
	if err := json.Unmarshal([]byte(*captured.Capabilities), &capabilities); err != nil {
		t.Fatalf("decode capabilities: %v; capabilities=%s", err, *captured.Capabilities)
	}
	if capabilities.SchemaVersion != "task.capabilities/v2" {
		t.Fatalf("schema_version = %q, want task.capabilities/v2", capabilities.SchemaVersion)
	}
	if len(capabilities.TaskCapabilities) != 1 {
		t.Fatalf("task_capabilities = %#v, want one entry", capabilities.TaskCapabilities)
	}
	capability := capabilities.TaskCapabilities[0]
	if capability.Type != "check" || capability.CreateURL != "/quality/check-tasks?create=1" || capability.EditURL != "/quality/check-tasks?task_id=:id" {
		t.Fatalf("check capability = %#v", capability)
	}
	if capability.SupportsSchedule || capability.SupportsCancel || capability.SupportsInlineExecution || capability.Deprecated {
		t.Fatalf("check flags = %#v, want all false", capability)
	}
}

func taskProviderRegistrationForTest(payload commonModels.TaskProvider) TaskProviderRegistration {
	var capabilities *string
	if payload.Capabilities != nil {
		value := string(*payload.Capabilities)
		capabilities = &value
	}
	return TaskProviderRegistration{
		ModuleName: payload.ModuleName, DisplayName: payload.DisplayName, Description: payload.Description,
		BaseURL: payload.BaseURL, TaskListEndpoint: payload.TaskListEndpoint,
		TaskDetailEndpoint: payload.TaskDetailEndpoint, TaskExecuteEndpoint: payload.TaskExecuteEndpoint,
		TaskStatusEndpoint: payload.TaskStatusEndpoint, TaskCancelEndpoint: payload.TaskCancelEndpoint,
		Capabilities: capabilities, IsEnabled: payload.IsEnabled,
	}
}

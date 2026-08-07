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
	Type                    string                 `json:"type"`
	DisplayName             string                 `json:"display_name"`
	Description             string                 `json:"description"`
	DefinitionSchema        map[string]interface{} `json:"definition_schema"`
	SupportsSchedule        bool                   `json:"supports_schedule"`
	SupportsCancel          bool                   `json:"supports_cancel"`
	SupportsInlineExecution bool                   `json:"supports_inline_execution"`
	CreateURL               string                 `json:"create_url"`
	EditURL                 string                 `json:"edit_url"`
	Deprecated              bool                   `json:"deprecated"`
}

type taskProviderTestTokenSource struct{}

func (taskProviderTestTokenSource) Token(context.Context, uint) (string, error) {
	return "tenant-token", nil
}
func (taskProviderTestTokenSource) PlatformToken(context.Context) (string, error) {
	return "platform-token", nil
}

func TestTaskProviderRegistryRegistersStandardTransferContract(t *testing.T) {
	var captured TaskProviderRegistration
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
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
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := commonClient.NewSystemServiceClient(server.URL, taskProviderTestTokenSource{}, server.Client())
	registry := NewTaskProviderRegistryService(client, "http://transfer.internal")
	if err := registry.Register(context.Background()); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if captured.ModuleName != "transfer" {
		t.Fatalf("module_name = %q, want transfer", captured.ModuleName)
	}
	if captured.BaseURL != "http://transfer.internal" {
		t.Fatalf("base_url = %q, want http://transfer.internal", captured.BaseURL)
	}
	if captured.TaskListEndpoint != "/api/v1/transfer/tasks" ||
		captured.TaskDetailEndpoint != "/api/v1/transfer/tasks/{task_type}/{id}" ||
		captured.TaskExecuteEndpoint != "/api/v1/transfer/tasks/{task_type}/{id}/execute" ||
		captured.TaskStatusEndpoint != "/api/v1/transfer/executions/{execution_id}" {
		t.Fatalf("standard endpoints not registered: %#v", captured)
	}
	if captured.TaskCancelEndpoint != "" {
		t.Fatalf("task_cancel_endpoint = %q, want empty because sync does not declare standard cancel", captured.TaskCancelEndpoint)
	}

	var capabilities struct {
		SchemaVersion      string                       `json:"schema_version"`
		TaskCapabilities   []taskProviderTaskCapability `json:"task_capabilities"`
		XRuntimeBoundaries []string                     `json:"x_runtime_boundaries"`
		XLoadModes         []string                     `json:"x_load_modes"`
		XChangeDetection   []string                     `json:"x_change_detection"`
		XFeatures          []string                     `json:"x_features"`
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
	if len(capabilities.XRuntimeBoundaries) != 1 || capabilities.XRuntimeBoundaries[0] != "bounded" || len(capabilities.XFeatures) == 0 {
		t.Fatalf("x_ capability extensions should remain declared: %#v", capabilities)
	}
	if len(capabilities.XLoadModes) != 2 || len(capabilities.XChangeDetection) != 1 || capabilities.XChangeDetection[0] != "watermark" {
		t.Fatalf("bounded sync capabilities are incomplete: %#v", capabilities)
	}

	sync := capabilities.TaskCapabilities[0]
	if sync.Type != "sync" || sync.CreateURL != "/transfer/tasks/create" || sync.EditURL != "/transfer/tasks/:id/edit" {
		t.Fatalf("sync capability = %#v", sync)
	}
	if !sync.SupportsSchedule {
		t.Fatal("sync supports_schedule = false, want true")
	}
	if sync.SupportsCancel || sync.SupportsInlineExecution || sync.Deprecated {
		t.Fatalf("sync flags = %#v, want cancel/inline/deprecated false", sync)
	}
	if sync.DefinitionSchema["type"] != "object" {
		t.Fatalf("sync definition_schema must be an object schema: %#v", sync)
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

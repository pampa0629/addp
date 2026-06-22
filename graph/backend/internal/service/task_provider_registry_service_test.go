package service

import (
	"encoding/json"

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

func TestTaskProviderRegistryRegistersStandardGraphContract(t *testing.T) {
	var captured TaskProviderRegistration
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/internal/task-providers/register" {
			t.Fatalf("path = %s, want /api/v1/internal/task-providers/register", r.URL.Path)
		}
		if got := r.Header.Get("X-Internal-API-Key"); got != "internal-key" {
			t.Fatalf("X-Internal-API-Key = %q, want internal-key", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	registry := NewTaskProviderRegistryService(server.URL, "internal-key", "http://graph.internal")
	if err := registry.Register(); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if captured.ModuleName != "graph" {
		t.Fatalf("module_name = %q, want graph", captured.ModuleName)
	}
	if captured.BaseURL != "http://graph.internal" {
		t.Fatalf("base_url = %q, want http://graph.internal", captured.BaseURL)
	}
	if captured.TaskListEndpoint != "/api/v1/graph/tasks" ||
		captured.TaskDetailEndpoint != "/api/v1/graph/tasks/{task_type}/{id}" ||
		captured.TaskExecuteEndpoint != "/api/v1/graph/tasks/{task_type}/{id}/execute" ||
		captured.TaskStatusEndpoint != "/api/v1/graph/executions/{execution_id}" {
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
	if capabilities.SchemaVersion != "task.capabilities/v1" {
		t.Fatalf("schema_version = %q, want task.capabilities/v1", capabilities.SchemaVersion)
	}
	if len(capabilities.TaskCapabilities) != 1 {
		t.Fatalf("task_capabilities = %#v, want one entry", capabilities.TaskCapabilities)
	}
	capability := capabilities.TaskCapabilities[0]
	if capability.Type != "kg_build" || capability.CreateURL != "/graph/graphs" || capability.EditURL != "/graph/graphs/:graph_id/build/tasks/:id" {
		t.Fatalf("kg_build capability = %#v", capability)
	}
	if capability.SupportsSchedule || capability.SupportsCancel || capability.SupportsInlineExecution || capability.Deprecated {
		t.Fatalf("kg_build flags = %#v, want all false", capability)
	}
}

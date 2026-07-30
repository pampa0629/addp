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

type registrationServiceTokens string

func (token registrationServiceTokens) Token(context.Context, uint) (string, error) {
	return string(token), nil
}

func (token registrationServiceTokens) PlatformToken(context.Context) (string, error) {
	return string(token), nil
}

type registrationTaskCapability struct {
	Type                    string                 `json:"type"`
	DisplayName             string                 `json:"display_name"`
	Description             string                 `json:"description"`
	DefinitionSchema        map[string]interface{} `json:"definition_schema"`
	ExecutionSchema         map[string]interface{} `json:"execution_schema"`
	SupportsSchedule        bool                   `json:"supports_schedule"`
	SupportsCancel          bool                   `json:"supports_cancel"`
	SupportsInlineExecution bool                   `json:"supports_inline_execution"`
	CreateURL               string                 `json:"create_url"`
	EditURL                 string                 `json:"edit_url"`
	Deprecated              bool                   `json:"deprecated"`
}

func TestTaskProviderRegistrationRegistersStandardOrchestratorContract(t *testing.T) {
	var captured commonModels.TaskProvider
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/v1/system/runtime/task-providers" {
			t.Fatalf("path = %s, want runtime task provider route", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer addp_at_orchestrator" {
			t.Fatalf("Authorization = %q", got)
		}
		if r.Header.Get("X-Internal-API-Key") != "" || r.Header.Get("X-Tenant-ID") != "" {
			t.Fatal("legacy internal headers must not be sent")
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	systemClient := commonClient.NewSystemServiceClient(server.URL, registrationServiceTokens("addp_at_orchestrator"), server.Client())
	registry := NewTaskProviderRegistrationService(systemClient, "http://orchestrator.internal")
	if err := registry.Register(context.Background()); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if captured.ModuleName != "orchestrator" {
		t.Fatalf("module_name = %q, want orchestrator", captured.ModuleName)
	}
	if captured.BaseURL != "http://orchestrator.internal" {
		t.Fatalf("base_url = %q, want http://orchestrator.internal", captured.BaseURL)
	}
	if captured.TaskListEndpoint != "/api/v1/orchestrator/tasks" ||
		captured.TaskDetailEndpoint != "/api/v1/orchestrator/tasks/{task_type}/{id}" ||
		captured.TaskExecuteEndpoint != "/api/v1/orchestrator/tasks/{task_type}/{id}/execute" ||
		captured.TaskStatusEndpoint != "/api/v1/orchestrator/executions/{execution_id}" {
		t.Fatalf("standard endpoints not registered: %#v", captured)
	}
	if captured.TaskCancelEndpoint != "" {
		t.Fatalf("task_cancel_endpoint = %q, want empty because orchestration does not declare standard cancel", captured.TaskCancelEndpoint)
	}

	var capabilities struct {
		SchemaVersion    string                       `json:"schema_version"`
		TaskCapabilities []registrationTaskCapability `json:"task_capabilities"`
	}
	if captured.Capabilities == nil {
		t.Fatal("capabilities is nil")
	}
	if _, err := taskprovider.ParseCapabilities(string(*captured.Capabilities)); err != nil {
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

	orchestration := capabilities.TaskCapabilities[0]
	if orchestration.Type != "orchestration" ||
		orchestration.CreateURL != "/orchestrator/orchestrations" ||
		orchestration.EditURL != "/orchestrator/orchestrations/:id/edit" {
		t.Fatalf("orchestration capability = %#v", orchestration)
	}
	if !orchestration.SupportsSchedule {
		t.Fatal("orchestration supports_schedule = false, want true")
	}
	if orchestration.SupportsCancel || orchestration.SupportsInlineExecution || orchestration.Deprecated {
		t.Fatalf("orchestration flags = %#v, want cancel/inline/deprecated false", orchestration)
	}
	if orchestration.DefinitionSchema["type"] != "object" || orchestration.ExecutionSchema["type"] != "object" {
		t.Fatalf("orchestration schemas must be object schemas: %#v", orchestration)
	}
}

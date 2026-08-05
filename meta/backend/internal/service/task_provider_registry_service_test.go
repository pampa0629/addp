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

func TestTaskProviderRegistryRegistersStandardMetaContract(t *testing.T) {
	var captured commonModels.TaskProvider
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/system/oauth/token" {
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse token form: %v", err)
			}
			if r.Form.Get("context_type") != "platform" || r.Form.Get("tenant_id") != "" {
				t.Fatalf("token context form = %#v", r.Form)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "addp_at_meta_platform", "token_type": "bearer", "expires_in": 300, "scope": "addp.api",
			})
			return
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/v1/system/runtime/task-providers" {
			t.Fatalf("path = %s, want /api/v1/system/runtime/task-providers", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer addp_at_meta_platform" {
			t.Fatalf("Authorization = %q, want platform service Bearer", got)
		}
		if r.Header.Get("X-Internal-API-Key") != "" || r.Header.Get("X-Tenant-ID") != "" {
			t.Fatal("legacy authentication header was sent")
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	source, err := commonClient.NewOAuthServiceTokenSource(
		server.URL, "addp-meta", "meta-task-provider-test-secret-32-bytes", server.Client(),
	)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewTaskProviderRegistryService(
		commonClient.NewSystemServiceClient(server.URL, source, server.Client()),
		"http://meta.internal",
	)
	if err := registry.Register(context.Background()); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if captured.ModuleName != "meta" {
		t.Fatalf("module_name = %q, want meta", captured.ModuleName)
	}
	if captured.BaseURL != "http://meta.internal" {
		t.Fatalf("base_url = %q, want http://meta.internal", captured.BaseURL)
	}
	if captured.TaskListEndpoint != "/api/v1/meta/tasks" ||
		captured.TaskDetailEndpoint != "/api/v1/meta/tasks/{task_type}/{id}" ||
		captured.TaskExecuteEndpoint != "/api/v1/meta/tasks/{task_type}/{id}/execute" ||
		captured.TaskStatusEndpoint != "/api/v1/meta/executions/{execution_id}" {
		t.Fatalf("standard endpoints not registered: %#v", captured)
	}
	if captured.TaskCancelEndpoint != "" {
		t.Fatalf("task_cancel_endpoint = %q, want empty because scan does not declare standard cancel", captured.TaskCancelEndpoint)
	}

	var capabilities struct {
		SchemaVersion         string                       `json:"schema_version"`
		TaskCapabilities      []taskProviderTaskCapability `json:"task_capabilities"`
		XSupportedSourceModel []string                     `json:"x_supported_source_models"`
		XFeatures             []string                     `json:"x_features"`
	}
	if captured.Capabilities == nil {
		t.Fatal("capabilities is nil")
	}
	capabilitiesText := string(*captured.Capabilities)
	if _, err := taskprovider.ParseCapabilities(capabilitiesText); err != nil {
		t.Fatalf("capabilities contract invalid: %v; capabilities=%s", err, *captured.Capabilities)
	}
	if err := json.Unmarshal([]byte(capabilitiesText), &capabilities); err != nil {
		t.Fatalf("decode capabilities: %v; capabilities=%s", err, *captured.Capabilities)
	}
	if capabilities.SchemaVersion != "task.capabilities/v2" {
		t.Fatalf("schema_version = %q, want task.capabilities/v2", capabilities.SchemaVersion)
	}
	if len(capabilities.TaskCapabilities) != 1 {
		t.Fatalf("task_capabilities = %#v, want one entry", capabilities.TaskCapabilities)
	}
	if len(capabilities.XSupportedSourceModel) == 0 || len(capabilities.XFeatures) == 0 {
		t.Fatalf("x_ capability extensions should remain declared: %#v", capabilities)
	}

	scan := capabilities.TaskCapabilities[0]
	if scan.Type != "scan" || scan.CreateURL != "/meta/scan" || scan.EditURL != "/meta/scan?task_id=:id" {
		t.Fatalf("scan capability = %#v", scan)
	}
	if !scan.SupportsSchedule {
		t.Fatal("scan supports_schedule = false, want true")
	}
	if scan.SupportsCancel || scan.SupportsInlineExecution || scan.Deprecated {
		t.Fatalf("scan flags = %#v, want cancel/inline/deprecated false", scan)
	}
	if scan.DefinitionSchema["type"] != "object" {
		t.Fatalf("scan definition_schema must be an object schema: %#v", scan)
	}
}

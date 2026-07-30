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

type staticServiceTokenSource string

func (source staticServiceTokenSource) Token(context.Context, uint) (string, error) {
	return string(source), nil
}

func (source staticServiceTokenSource) PlatformToken(context.Context) (string, error) {
	return string(source), nil
}

func TestTaskProviderRegistryRegistersStandardDevelopContract(t *testing.T) {
	var payload commonModels.TaskProvider
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/v1/system/runtime/task-providers" {
			t.Fatalf("path = %s, want runtime task provider route", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer addp_at_service" {
			t.Fatalf("Authorization = %q", got)
		}
		if r.Header.Get("X-Internal-API-Key") != "" {
			t.Fatal("legacy internal key header must not be sent")
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	systemClient := commonClient.NewSystemServiceClient(server.URL, staticServiceTokenSource("addp_at_service"), nil)
	svc := NewTaskProviderRegistryService(systemClient, "http://develop.internal")
	if err := svc.Register(context.Background()); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if payload.ModuleName != "develop" {
		t.Fatalf("ModuleName = %q, want develop", payload.ModuleName)
	}
	if payload.BaseURL != "http://develop.internal" {
		t.Fatalf("BaseURL = %q, want http://develop.internal", payload.BaseURL)
	}
	if payload.TaskListEndpoint != "/api/v1/develop/task-provider/tasks" {
		t.Fatalf("TaskListEndpoint = %q", payload.TaskListEndpoint)
	}
	if payload.TaskDetailEndpoint != "/api/v1/develop/task-provider/tasks/{task_type}/{id}" {
		t.Fatalf("TaskDetailEndpoint = %q", payload.TaskDetailEndpoint)
	}
	if payload.TaskExecuteEndpoint != "/api/v1/develop/task-provider/tasks/{task_type}/{id}/execute" {
		t.Fatalf("TaskExecuteEndpoint = %q", payload.TaskExecuteEndpoint)
	}
	if payload.TaskStatusEndpoint != "/api/v1/develop/task-provider/executions/{execution_id}" {
		t.Fatalf("TaskStatusEndpoint = %q", payload.TaskStatusEndpoint)
	}
	if payload.TaskCancelEndpoint != "" {
		t.Fatalf("TaskCancelEndpoint = %q, want empty when supports_cancel=false", payload.TaskCancelEndpoint)
	}

	if payload.Capabilities == nil {
		t.Fatal("capabilities is nil")
	}
	if _, err := taskprovider.ParseCapabilities(string(*payload.Capabilities)); err != nil {
		t.Fatalf("capabilities contract invalid: %v; capabilities=%s", err, *payload.Capabilities)
	}

	caps := decodeCapabilitiesForTest(t, payload.Capabilities)
	if caps["schema_version"] != "task.capabilities/v1" {
		t.Fatalf("schema_version = %v", caps["schema_version"])
	}
	taskCapabilities, ok := caps["task_capabilities"].([]interface{})
	if !ok || len(taskCapabilities) != 3 {
		t.Fatalf("task_capabilities = %#v, want 3 entries", caps["task_capabilities"])
	}

	expectedURLs := map[string][2]string{
		"query":    {"/develop/sql?action=create", "/develop/sql?action=edit&id=:id"},
		"workflow": {"/develop/workflow?action=create", "/develop/workflow?action=edit&id=:id"},
		"script":   {"/develop/notebook?action=create", "/develop/notebook?action=edit&id=:id"},
	}
	seen := map[string]bool{}
	for _, raw := range taskCapabilities {
		taskType, ok := raw.(map[string]interface{})
		if !ok {
			t.Fatalf("task type entry = %#v, want object", raw)
		}
		typeName, _ := taskType["type"].(string)
		urls, ok := expectedURLs[typeName]
		if !ok {
			t.Fatalf("unexpected task type %q", typeName)
		}
		seen[typeName] = true
		if taskType["create_url"] != urls[0] || taskType["edit_url"] != urls[1] {
			t.Fatalf("%s urls = %v / %v", typeName, taskType["create_url"], taskType["edit_url"])
		}
		for _, field := range []string{"supports_schedule", "supports_cancel", "supports_inline_execution", "deprecated"} {
			if value, ok := taskType[field].(bool); !ok || value {
				t.Fatalf("%s.%s = %#v, want false boolean", typeName, field, taskType[field])
			}
		}
		if !isObjectSchema(taskType["definition_schema"]) {
			t.Fatalf("%s definition_schema must be object schema", typeName)
		}
		if !isObjectSchema(taskType["execution_schema"]) {
			t.Fatalf("%s execution_schema must be object schema", typeName)
		}
	}
	for typeName := range expectedURLs {
		if !seen[typeName] {
			t.Fatalf("missing task type %q", typeName)
		}
	}
}

func decodeCapabilitiesForTest(t *testing.T, value *commonModels.JSONString) map[string]interface{} {
	t.Helper()
	if value == nil {
		t.Fatal("capabilities is nil")
	}
	var caps map[string]interface{}
	if err := json.Unmarshal([]byte(string(*value)), &caps); err != nil {
		t.Fatalf("decode capabilities: %v", err)
	}
	return caps
}

func isObjectSchema(value interface{}) bool {
	schema, ok := value.(map[string]interface{})
	if !ok {
		return false
	}
	return schema["type"] == "object"
}

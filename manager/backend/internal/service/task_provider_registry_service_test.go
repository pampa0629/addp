package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type taskProviderTaskTypeCapability struct {
	Type                    string `json:"type"`
	SupportsSchedule        bool   `json:"supports_schedule"`
	SupportsCancel          bool   `json:"supports_cancel"`
	SupportsInlineExecution bool   `json:"supports_inline_execution"`
	CreateURL               string `json:"create_url"`
	EditURL                 string `json:"edit_url"`
	Deprecated              bool   `json:"deprecated"`
}

func TestTaskProviderRegistryQuickViewOptimizationCapability(t *testing.T) {
	var captured TaskProviderRegistration
	var capturedPath string
	var capturedInternalAPIKey string
	var decodeErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedInternalAPIKey = r.Header.Get("X-Internal-API-Key")
		decodeErr = json.NewDecoder(r.Body).Decode(&captured)
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	registry := NewTaskProviderRegistryService(server.URL, "test-internal-key", "http://manager.internal")
	if err := registry.Register(); err != nil {
		t.Fatalf("register task provider: %v", err)
	}

	if capturedPath != "/api/v1/internal/task-providers/register" {
		t.Fatalf("path = %s, want internal task provider register path", capturedPath)
	}
	if capturedInternalAPIKey != "test-internal-key" {
		t.Fatalf("internal api key = %q, want test-internal-key", capturedInternalAPIKey)
	}
	if decodeErr != nil {
		t.Fatalf("decode registration: %v", decodeErr)
	}
	if captured.ModuleName != "manager" {
		t.Fatalf("module_name = %q, want manager", captured.ModuleName)
	}
	if captured.BaseURL != "http://manager.internal" {
		t.Fatalf("base_url = %q, want manager url", captured.BaseURL)
	}
	if captured.TaskCancelEndpoint != "" {
		t.Fatalf("task_cancel_endpoint = %q, want empty because manager task types do not declare standard cancel", captured.TaskCancelEndpoint)
	}
	if captured.Capabilities == nil {
		t.Fatal("capabilities is nil")
	}

	var capabilities struct {
		SchemaVersion string `json:"schema_version"`
		TaskTypes     []taskProviderTaskTypeCapability `json:"task_types"`
	}
	if err := json.Unmarshal([]byte(*captured.Capabilities), &capabilities); err != nil {
		t.Fatalf("decode capabilities: %v; capabilities=%s", err, *captured.Capabilities)
	}
	if capabilities.SchemaVersion != "task.capabilities/v1" {
		t.Fatalf("schema_version = %q, want task.capabilities/v1", capabilities.SchemaVersion)
	}

	quickView := taskProviderCapabilityByType(t, capabilities.TaskTypes, "quick_view_optimization")
	if quickView.SupportsSchedule {
		t.Fatal("quick_view_optimization supports_schedule = true, want false")
	}
	if quickView.SupportsCancel {
		t.Fatal("quick_view_optimization supports_cancel = true, want false")
	}
	if quickView.SupportsInlineExecution {
		t.Fatal("quick_view_optimization supports_inline_execution = true, want false")
	}
	if quickView.CreateURL != "/manager/quick-view-optimization?tab=tasks" {
		t.Fatalf("quick_view_optimization create_url = %q, want quick view optimization tasks page", quickView.CreateURL)
	}
	if quickView.EditURL != "/manager/quick-view-optimization?tab=tasks&task_id=:id" {
		t.Fatalf("quick_view_optimization edit_url = %q, want quick view optimization task edit page", quickView.EditURL)
	}
	if quickView.Deprecated {
		t.Fatal("quick_view_optimization deprecated = true, want false")
	}
}

func taskProviderCapabilityByType(t *testing.T, taskTypes []taskProviderTaskTypeCapability, taskType string) taskProviderTaskTypeCapability {
	t.Helper()
	for _, candidate := range taskTypes {
		if candidate.Type == taskType {
			return candidate
		}
	}
	t.Fatalf("task type %q not found in capabilities", taskType)
	var zero taskProviderTaskTypeCapability
	return zero
}

package service

import (
	"encoding/json"

	"github.com/addp/common/taskprovider"
	"net/http"
	"net/http/httptest"
	"testing"
)

type taskProviderTaskCapability struct {
	Type                    string                 `json:"type"`
	SupportsSchedule        bool                   `json:"supports_schedule"`
	SupportsCancel          bool                   `json:"supports_cancel"`
	SupportsInlineExecution bool                   `json:"supports_inline_execution"`
	CreateURL               string                 `json:"create_url"`
	EditURL                 string                 `json:"edit_url"`
	Deprecated              bool                   `json:"deprecated"`
	ExecutionSchema         map[string]interface{} `json:"execution_schema"`
}

func TestTaskProviderRegistryVectorMaterializedViewCapability(t *testing.T) {
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
	if _, err := taskprovider.ParseCapabilities(*captured.Capabilities); err != nil {
		t.Fatalf("capabilities contract invalid: %v; capabilities=%s", err, *captured.Capabilities)
	}

	var capabilities struct {
		SchemaVersion    string                       `json:"schema_version"`
		TaskCapabilities []taskProviderTaskCapability `json:"task_capabilities"`
	}
	if err := json.Unmarshal([]byte(*captured.Capabilities), &capabilities); err != nil {
		t.Fatalf("decode capabilities: %v; capabilities=%s", err, *captured.Capabilities)
	}
	if capabilities.SchemaVersion != "task.capabilities/v1" {
		t.Fatalf("schema_version = %q, want task.capabilities/v1", capabilities.SchemaVersion)
	}

	tileCache := taskProviderCapabilityByType(t, capabilities.TaskCapabilities, "vector_tile_cache_generation")
	if tileCache.SupportsSchedule {
		t.Fatal("vector_tile_cache_generation supports_schedule = true, want false")
	}
	vectorTileSet := taskProviderCapabilityByType(t, capabilities.TaskCapabilities, "vector_tile_set_generation")
	if vectorTileSet.CreateURL != "/manager/spatial-tasks/vector-tiles?create=1" {
		t.Fatalf("vector_tile_set_generation create_url = %q, want spatial tasks page", vectorTileSet.CreateURL)
	}
	if vectorTileSet.EditURL != "/manager/spatial-tasks/vector-tiles?task_id=:id" {
		t.Fatalf("vector_tile_set_generation edit_url = %q, want spatial tasks edit page", vectorTileSet.EditURL)
	}

	quickView := taskProviderCapabilityByType(t, capabilities.TaskCapabilities, "vector_materialized_view_generation")
	if quickView.SupportsSchedule {
		t.Fatal("vector_materialized_view_generation supports_schedule = true, want false")
	}
	if quickView.SupportsCancel {
		t.Fatal("vector_materialized_view_generation supports_cancel = true, want false")
	}
	if quickView.SupportsInlineExecution {
		t.Fatal("vector_materialized_view_generation supports_inline_execution = true, want false")
	}
	if quickView.CreateURL != "/manager/spatial-quick-view/vector-materialized-view?tab=tasks" {
		t.Fatalf("vector_materialized_view_generation create_url = %q, want vector materialized view tasks page", quickView.CreateURL)
	}
	if quickView.EditURL != "/manager/spatial-quick-view/vector-materialized-view?tab=tasks&task_id=:id" {
		t.Fatalf("vector_materialized_view_generation edit_url = %q, want vector materialized view task edit page", quickView.EditURL)
	}
	if quickView.Deprecated {
		t.Fatal("vector_materialized_view_generation deprecated = true, want false")
	}

	cog := taskProviderCapabilityByType(t, capabilities.TaskCapabilities, "raster_cog_generation")
	if cog.SupportsSchedule {
		t.Fatal("raster_cog_generation supports_schedule = true, want false")
	}
	if cog.SupportsCancel {
		t.Fatal("raster_cog_generation supports_cancel = true, want false")
	}
	if cog.SupportsInlineExecution {
		t.Fatal("raster_cog_generation supports_inline_execution = true, want false")
	}
	if cog.CreateURL != "/manager/spatial-quick-view/raster-cog?tab=tasks" {
		t.Fatalf("raster_cog_generation create_url = %q, want raster COG tasks page", cog.CreateURL)
	}
	if cog.EditURL != "/manager/spatial-quick-view/raster-cog?tab=tasks&task_id=:id" {
		t.Fatalf("raster_cog_generation edit_url = %q, want raster COG task edit page", cog.EditURL)
	}
	if cog.Deprecated {
		t.Fatal("raster_cog_generation deprecated = true, want false")
	}

	for _, taskType := range []string{
		"vector_tile_cache_generation", "vector_materialized_view_generation", "raster_cog_generation",
		"model_3d_glb_generation", "model3d_tiles_generation", "gaussian_splat_ksplat_generation",
		"point_cloud_copc_generation", "cad_preview_generation",
	} {
		capability := taskProviderCapabilityByType(t, capabilities.TaskCapabilities, taskType)
		properties, _ := capability.ExecutionSchema["properties"].(map[string]interface{})
		action, _ := properties["existing_result_action"].(map[string]interface{})
		enum, _ := action["enum"].([]interface{})
		if action["type"] != "string" || len(enum) != 1 || enum[0] != "overwrite" || capability.ExecutionSchema["additionalProperties"] != false {
			t.Fatalf("%s execution_schema = %#v", taskType, capability.ExecutionSchema)
		}
	}
	for _, taskType := range []string{"vector_tile_set_generation", "raster_mosaic_generation", "embedding"} {
		capability := taskProviderCapabilityByType(t, capabilities.TaskCapabilities, taskType)
		if _, exists := capability.ExecutionSchema["properties"]; exists || capability.ExecutionSchema["additionalProperties"] != false {
			t.Fatalf("%s execution_schema = %#v, want empty closed object", taskType, capability.ExecutionSchema)
		}
	}
}

func taskProviderCapabilityByType(t *testing.T, taskCapabilities []taskProviderTaskCapability, taskType string) taskProviderTaskCapability {
	t.Helper()
	for _, candidate := range taskCapabilities {
		if candidate.Type == taskType {
			return candidate
		}
	}
	t.Fatalf("task type %q not found in capabilities", taskType)
	var zero taskProviderTaskCapability
	return zero
}

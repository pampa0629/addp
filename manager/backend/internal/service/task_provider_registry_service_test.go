package service

import (
	"context"
	"encoding/json"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/taskprovider"
	"net/http"
	"net/http/httptest"
	"testing"
)

type taskProviderRegistryTokenSource struct{}

func (taskProviderRegistryTokenSource) Token(context.Context, uint) (string, error) {
	return "tenant-token", nil
}

func (taskProviderRegistryTokenSource) PlatformToken(context.Context) (string, error) {
	return "platform-token", nil
}

type taskProviderTaskCapability struct {
	Type                    string `json:"type"`
	SupportsSchedule        bool   `json:"supports_schedule"`
	SupportsCancel          bool   `json:"supports_cancel"`
	SupportsInlineExecution bool   `json:"supports_inline_execution"`
	CreateURL               string `json:"create_url"`
	EditURL                 string `json:"edit_url"`
	Deprecated              bool   `json:"deprecated"`
}

func TestTaskProviderRegistryVectorMaterializedViewCapability(t *testing.T) {
	var captured TaskProviderRegistration
	var capturedPath string
	var capturedAuthorization string
	var decodeErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedAuthorization = r.Header.Get("Authorization")
		decodeErr = json.NewDecoder(r.Body).Decode(&captured)
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	systemClient := commonClient.NewSystemServiceClient(server.URL, taskProviderRegistryTokenSource{}, server.Client())
	registry := NewTaskProviderRegistryService(systemClient, "http://manager.internal")
	if err := registry.Register(context.Background()); err != nil {
		t.Fatalf("register task provider: %v", err)
	}

	if capturedPath != "/api/v1/system/runtime/task-providers" {
		t.Fatalf("path = %s, want Service Principal task provider register path", capturedPath)
	}
	if capturedAuthorization != "Bearer platform-token" {
		t.Fatalf("authorization = %q, want platform service token", capturedAuthorization)
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
	if _, err := taskprovider.ParseCapabilities(string(*captured.Capabilities)); err != nil {
		t.Fatalf("capabilities contract invalid: %v; capabilities=%s", err, *captured.Capabilities)
	}

	var capabilities struct {
		SchemaVersion    string                       `json:"schema_version"`
		TaskCapabilities []taskProviderTaskCapability `json:"task_capabilities"`
	}
	if err := json.Unmarshal([]byte(*captured.Capabilities), &capabilities); err != nil {
		t.Fatalf("decode capabilities: %v; capabilities=%s", err, *captured.Capabilities)
	}
	if capabilities.SchemaVersion != "task.capabilities/v2" {
		t.Fatalf("schema_version = %q, want task.capabilities/v2", capabilities.SchemaVersion)
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
	embedding := taskProviderCapabilityByType(t, capabilities.TaskCapabilities, "embedding")
	if embedding.CreateURL != "/manager/vectorization-tasks?create=1" {
		t.Fatalf("embedding create_url = %q, want vectorization task create page", embedding.CreateURL)
	}
	if embedding.EditURL != "/manager/vectorization-tasks?task_id=:id" {
		t.Fatalf("embedding edit_url = %q, want vectorization task edit page", embedding.EditURL)
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
	if quickView.CreateURL != "/manager/spatial-quick-view/vector-materialized-view?create=1" {
		t.Fatalf("vector_materialized_view_generation create_url = %q, want vector materialized view tasks page", quickView.CreateURL)
	}
	if quickView.EditURL != "/manager/spatial-quick-view/vector-materialized-view?task_id=:id" {
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
	if cog.CreateURL != "/manager/data-explorer" {
		t.Fatalf("raster_cog_generation create_url = %q, want data explorer", cog.CreateURL)
	}
	if cog.EditURL != "/manager/spatial-quick-view/raster-cog?task_id=:id" {
		t.Fatalf("raster_cog_generation edit_url = %q, want raster COG task detail page", cog.EditURL)
	}
	if cog.Deprecated {
		t.Fatal("raster_cog_generation deprecated = true, want false")
	}

	model3DTiles := taskProviderCapabilityByType(t, capabilities.TaskCapabilities, "model3d_tiles_generation")
	if model3DTiles.CreateURL != "/manager/data-explorer" {
		t.Fatalf("model3d_tiles_generation create_url = %q, want data explorer", model3DTiles.CreateURL)
	}
	if model3DTiles.EditURL != "/manager/model-3d-tiles?task_id=:id" {
		t.Fatalf("model3d_tiles_generation edit_url = %q, want model 3D Tiles task detail page", model3DTiles.EditURL)
	}

	canonicalTaskRoutes := map[string][2]string{
		"vector_tile_cache_generation":     {"/manager/spatial-quick-view/vector-tile-cache?create=1", "/manager/spatial-quick-view/vector-tile-cache?task_id=:id"},
		"raster_mosaic_generation":         {"/manager/spatial-quick-view/raster-mosaic?create=1", "/manager/spatial-quick-view/raster-mosaic?task_id=:id"},
		"model_3d_glb_generation":          {"/manager/model-3d-glb?create=1", "/manager/model-3d-glb?task_id=:id"},
		"gaussian_splat_ksplat_generation": {"/manager/gaussian-splat-ksplat?create=1", "/manager/gaussian-splat-ksplat?task_id=:id"},
		"point_cloud_copc_generation":      {"/manager/point-cloud-copc?create=1", "/manager/point-cloud-copc?task_id=:id"},
		"cad_preview_generation":           {"/manager/spatial-quick-view/cad-preview?create=1", "/manager/spatial-quick-view/cad-preview?task_id=:id"},
	}
	for taskType, routes := range canonicalTaskRoutes {
		capability := taskProviderCapabilityByType(t, capabilities.TaskCapabilities, taskType)
		if capability.CreateURL != routes[0] || capability.EditURL != routes[1] {
			t.Fatalf("%s routes = (%q, %q), want (%q, %q)", taskType, capability.CreateURL, capability.EditURL, routes[0], routes[1])
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

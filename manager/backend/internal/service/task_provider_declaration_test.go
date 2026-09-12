package service

import (
	"strings"
	"testing"

	"github.com/addp/common/taskprovider"
)

func TestManagerTaskProviderDeclaration(t *testing.T) {
	declaration, err := ManagerTaskProviderDeclaration()
	if err != nil {
		t.Fatal(err)
	}
	if err := taskprovider.ValidateDeclaration(declaration); err != nil {
		t.Fatalf("invalid declaration: %v", err)
	}
	capabilities, _ := taskprovider.ParseCapabilities(string(*declaration.Capabilities))
	for _, taskType := range []string{"vector_tile_set_generation", "vector_tile_cache_generation", "pptx_pdf_generation", "embedding"} {
		if capabilities.CapabilityFor(taskType) == nil {
			t.Fatalf("missing task type %s", taskType)
		}
	}
	pptx := capabilities.CapabilityFor("pptx_pdf_generation")
	if pptx == nil || !strings.Contains(pptx.EditURL, "/manager/derived-tasks?") {
		t.Fatalf("pptx_pdf_generation edit_url = %q, want unified generation task route", pptx.EditURL)
	}
	embedding := capabilities.CapabilityFor("embedding")
	if embedding == nil {
		t.Fatal("missing embedding capability")
	}
	if embedding.CreateURL != "/manager/derived-tasks?category=embedding&create=1" || embedding.EditURL != "/manager/derived-tasks?category=embedding&task_id=:id" {
		t.Fatalf("embedding routes = %q / %q, want unified data task route", embedding.CreateURL, embedding.EditURL)
	}
	for _, taskType := range []string{
		"vector_tile_set_generation",
		"raster_mosaic_generation",
		"vector_tile_cache_generation",
		"vector_materialized_view_generation",
		"raster_cog_generation",
		"model3d_tiles_generation",
		"model_3d_glb_generation",
		"gaussian_splat_ksplat_generation",
		"point_cloud_copc_generation",
		"pptx_pdf_generation",
	} {
		capability := capabilities.CapabilityFor(taskType)
		if capability == nil || !strings.HasPrefix(capability.CreateURL, "/manager/derived-tasks?") {
			t.Fatalf("%s create_url = %q, want unified derived task route", taskType, capability.CreateURL)
		}
		if !strings.Contains(capability.CreateURL, "task_type="+taskType) || !strings.Contains(capability.CreateURL, "create=1") {
			t.Fatalf("%s create_url = %q, want task type and create state", taskType, capability.CreateURL)
		}
	}
	want := map[string]string{
		"list":    "/api/v1/manager/task-provider/tasks",
		"detail":  "/api/v1/manager/task-provider/tasks/{task_type}/{id}",
		"execute": "/api/v1/manager/task-provider/tasks/{task_type}/{id}/execute",
		"status":  "/api/v1/manager/task-provider/executions/{execution_id}",
	}
	if declaration.TaskListEndpoint != want["list"] || declaration.TaskDetailEndpoint != want["detail"] || declaration.TaskExecuteEndpoint != want["execute"] || declaration.TaskStatusEndpoint != want["status"] {
		t.Fatalf("TaskProvider endpoints = %#v, want %#v", declaration, want)
	}
}

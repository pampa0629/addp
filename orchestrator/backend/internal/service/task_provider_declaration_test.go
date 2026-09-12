package service

import (
	"testing"

	"github.com/addp/common/taskprovider"
)

func TestOrchestratorTaskProviderDeclaration(t *testing.T) {
	declaration, err := OrchestratorTaskProviderDeclaration()
	if err != nil {
		t.Fatal(err)
	}
	if err := taskprovider.ValidateDeclaration(declaration); err != nil {
		t.Fatalf("invalid declaration: %v", err)
	}
	capabilities, _ := taskprovider.ParseCapabilities(string(*declaration.Capabilities))
	capability := capabilities.CapabilityFor("orchestration")
	if capability == nil || !capability.SupportsSchedule {
		t.Fatalf("capability = %#v", capability)
	}
	want := map[string]string{
		"list":    "/api/v1/orchestrator/task-provider/tasks",
		"detail":  "/api/v1/orchestrator/task-provider/tasks/{task_type}/{id}",
		"execute": "/api/v1/orchestrator/task-provider/tasks/{task_type}/{id}/execute",
		"status":  "/api/v1/orchestrator/task-provider/executions/{execution_id}",
	}
	if declaration.TaskListEndpoint != want["list"] || declaration.TaskDetailEndpoint != want["detail"] || declaration.TaskExecuteEndpoint != want["execute"] || declaration.TaskStatusEndpoint != want["status"] {
		t.Fatalf("TaskProvider endpoints = %#v, want %#v", declaration, want)
	}
}

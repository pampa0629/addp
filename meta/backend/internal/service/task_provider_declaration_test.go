package service

import (
	"testing"

	"github.com/addp/common/taskprovider"
)

func TestMetaTaskProviderDeclaration(t *testing.T) {
	declaration, err := TaskProviderDeclaration()
	if err != nil {
		t.Fatal(err)
	}
	if err := taskprovider.ValidateDeclaration(declaration); err != nil {
		t.Fatalf("invalid declaration: %v", err)
	}
	capabilities, _ := taskprovider.ParseCapabilities(string(*declaration.Capabilities))
	if capabilities.CapabilityFor("scan") == nil {
		t.Fatalf("capabilities = %#v", capabilities.TaskCapabilities)
	}
	want := map[string]string{
		"list":    "/api/v1/meta/task-provider/tasks",
		"detail":  "/api/v1/meta/task-provider/tasks/{task_type}/{id}",
		"execute": "/api/v1/meta/task-provider/tasks/{task_type}/{id}/execute",
		"status":  "/api/v1/meta/task-provider/executions/{execution_id}",
	}
	if declaration.TaskListEndpoint != want["list"] || declaration.TaskDetailEndpoint != want["detail"] || declaration.TaskExecuteEndpoint != want["execute"] || declaration.TaskStatusEndpoint != want["status"] {
		t.Fatalf("TaskProvider endpoints = %#v, want %#v", declaration, want)
	}
}

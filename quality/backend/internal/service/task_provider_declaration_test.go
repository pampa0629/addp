package service

import (
	"testing"

	"github.com/addp/common/taskprovider"
)

func TestQualityTaskProviderDeclaration(t *testing.T) {
	declaration, err := QualityTaskProviderDeclaration()
	if err != nil {
		t.Fatal(err)
	}
	if err := taskprovider.ValidateDeclaration(declaration); err != nil {
		t.Fatalf("invalid declaration: %v", err)
	}
	capabilities, _ := taskprovider.ParseCapabilities(string(*declaration.Capabilities))
	if capabilities.CapabilityFor("check") == nil || capabilities.CapabilityFor("materialization_gate") == nil {
		t.Fatalf("capabilities = %#v", capabilities.TaskCapabilities)
	}
	if declaration.TaskListEndpoint != "/api/v1/quality/task-provider/tasks" ||
		declaration.TaskDetailEndpoint != "/api/v1/quality/task-provider/tasks/{task_type}/{id}" ||
		declaration.TaskExecuteEndpoint != "/api/v1/quality/task-provider/tasks/{task_type}/{id}/execute" ||
		declaration.TaskStatusEndpoint != "/api/v1/quality/task-provider/executions/{execution_id}" {
		t.Fatalf("TaskProvider endpoints are not isolated: %#v", declaration)
	}
}

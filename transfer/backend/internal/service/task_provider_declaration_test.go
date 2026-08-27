package service

import (
	"testing"

	"github.com/addp/common/taskprovider"
)

func TestTransferTaskProviderDeclaration(t *testing.T) {
	declaration, err := TransferTaskProviderDeclaration()
	if err != nil {
		t.Fatal(err)
	}
	if err := taskprovider.ValidateDeclaration(declaration); err != nil {
		t.Fatalf("invalid declaration: %v", err)
	}
	capabilities, _ := taskprovider.ParseCapabilities(string(*declaration.Capabilities))
	if capabilities.CapabilityFor("sync") == nil {
		t.Fatalf("capabilities = %#v", capabilities.TaskCapabilities)
	}
	wantEndpoints := map[string]string{
		"list":    "/api/v1/transfer/task-provider/tasks",
		"detail":  "/api/v1/transfer/task-provider/tasks/{task_type}/{id}",
		"execute": "/api/v1/transfer/task-provider/tasks/{task_type}/{id}/execute",
		"status":  "/api/v1/transfer/task-provider/executions/{execution_id}",
	}
	gotEndpoints := map[string]string{
		"list":    declaration.TaskListEndpoint,
		"detail":  declaration.TaskDetailEndpoint,
		"execute": declaration.TaskExecuteEndpoint,
		"status":  declaration.TaskStatusEndpoint,
	}
	for name, want := range wantEndpoints {
		if gotEndpoints[name] != want {
			t.Errorf("%s endpoint = %q, want %q", name, gotEndpoints[name], want)
		}
	}
}

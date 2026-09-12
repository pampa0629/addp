package taskprovider

import (
	"testing"

	"github.com/addp/common/models"
)

func TestValidateDeclaration(t *testing.T) {
	raw := models.JSONString(`{"schema_version":"task.capabilities/v2","task_capabilities":[{"type":"scan","display_name":"Scan","description":"Scan metadata","definition_schema":{"type":"object"},"supports_schedule":false,"supports_cancel":false,"supports_inline_execution":false,"create_url":"/meta/scan","edit_url":"/meta/scan?id=:id","deprecated":false}]}`)
	declaration := &models.TaskProviderDeclaration{
		DisplayName: "Meta", Description: "Metadata tasks",
		TaskListEndpoint: "/api/v1/meta/task-provider/tasks", TaskDetailEndpoint: "/api/v1/meta/task-provider/tasks/{task_type}/{id}",
		TaskExecuteEndpoint: "/api/v1/meta/task-provider/tasks/{task_type}/{id}/execute", TaskStatusEndpoint: "/api/v1/meta/task-provider/executions/{execution_id}",
		Capabilities: &raw,
	}
	if err := ValidateDeclaration(declaration); err != nil {
		t.Fatalf("ValidateDeclaration() error = %v", err)
	}
	declaration.TaskExecuteEndpoint = "/api/v1/meta/tasks/{id}/run"
	if err := ValidateDeclaration(declaration); err == nil {
		t.Fatal("ValidateDeclaration() accepted a non-standard execute endpoint")
	}

	declaration.TaskExecuteEndpoint = "/api/v1/meta/task-provider/tasks/{task_type}/{id}/execute"
	declaration.TaskListEndpoint = "/api/v1/meta/tasks"
	if err := ValidateDeclaration(declaration); err == nil {
		t.Fatal("ValidateDeclaration() accepted a TaskProvider endpoint in the human route space")
	}
}

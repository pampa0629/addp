package service

import (
	"context"
	"strings"
	"testing"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/develop/backend/internal/models"
)

func TestExecuteWorkflowRejectsInvalidWorkflowDefinitionBeforeRuntime(t *testing.T) {
	executor := &DevExecutor{}
	result, errorMessage := executor.executeWorkflow(context.Background(), &models.DevTask{
		DevType: commonExecution.TaskTypeWorkflow,
		Content: models.DevTaskContent{
			"workflow_definition": map[string]interface{}{
				"tasks": []interface{}{
					map[string]interface{}{
						"id":       "task1",
						"operator": "load",
						"params":   map[string]interface{}{},
					},
				},
			},
		},
		ExecutionConfig: models.DevTaskContent{
			"engine_id": float64(7),
		},
	}, "execution-1", 1)

	if result != nil {
		t.Fatalf("result = %#v, want nil", result)
	}
	if !strings.Contains(errorMessage, "depends_on") {
		t.Fatalf("errorMessage = %q, want depends_on validation error", errorMessage)
	}
}

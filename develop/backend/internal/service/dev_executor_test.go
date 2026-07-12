package service

import (
	"context"
	"reflect"
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

func TestWorkflowProducedTargetScanOptionsUseRefGroupsForFileTargets(t *testing.T) {
	opts := workflowProducedTargetScanOptions(WorkflowProducedTarget{
		EngineID: 26,
		Type:     "file",
		Path:     []string{"supermap", "result.udbx"},
		Locator:  "addp://engine/26/path/supermap/result.udbx?type=file",
	})

	if opts.EngineID != 26 || opts.ScanDepth != "deep" || !opts.Force {
		t.Fatalf("scan options base fields = %#v", opts)
	}
	if len(opts.Targets) != 0 {
		t.Fatalf("file target should not use locator targets: %#v", opts.Targets)
	}
	if len(opts.RefGroups) != 1 || opts.RefGroups[0].Primary != "supermap/result.udbx" {
		t.Fatalf("ref_groups = %#v, want primary supermap/result.udbx", opts.RefGroups)
	}
}

func TestWorkflowProducedTargetScanOptionsUseTargetsForTableTargets(t *testing.T) {
	locator := "addp://engine/8/path/public/result?type=table"
	opts := workflowProducedTargetScanOptions(WorkflowProducedTarget{
		EngineID: 8,
		Type:     "table",
		Path:     []string{"public", "result"},
		Locator:  locator,
	})

	if !reflect.DeepEqual(opts.Targets, []string{locator}) {
		t.Fatalf("targets = %#v, want locator target", opts.Targets)
	}
	if len(opts.RefGroups) != 0 {
		t.Fatalf("table target should not use ref_groups: %#v", opts.RefGroups)
	}
}

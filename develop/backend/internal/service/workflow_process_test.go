package service

import (
	"reflect"
	"testing"

	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/models"
)

func workflowProcessFixture() *commonExecution.TaskExecution {
	return &commonExecution.TaskExecution{
		TaskType: commonExecution.TaskTypeWorkflow,
		Attempt:  1,
		ExecutionConfig: commonModels.JSONMap{"content": map[string]interface{}{
			"workflow_definition": map[string]interface{}{"tasks": []map[string]interface{}{
				{"id": "source", "operator": "load", "params": map[string]interface{}{"locator": "private-data-locator"}},
				{"id": "dissolve", "operator": "dissolve", "params": map[string]interface{}{"input_gdf": map[string]interface{}{"$ref": "source"}}},
				{"id": "save", "operator": "save", "depends_on": []interface{}{"source", "dissolve"},
					"params": map[string]interface{}{"input_df": []interface{}{
						map[string]interface{}{"private-nested-key": map[string]interface{}{"$ref": "dissolve", "port": "members"}},
						map[string]interface{}{"$ref": "dissolve", "port": "result"},
						map[string]interface{}{"$ref": "dissolve", "port": "members"},
					}}},
			}},
		}},
	}
}

func TestWorkflowProcessUsesFrozenReferencesAndKeepsTerminalStateSeparate(t *testing.T) {
	item := workflowProcessFixture()
	item.Status = commonExecution.ExecutionStatusSuccess
	process := workflowProcess(item, commonExecution.Observe(item))
	want := &models.WorkflowProcess{
		Nodes: []models.WorkflowProcessNode{
			{ID: "source", Operator: "load", Status: "unknown"},
			{ID: "dissolve", Operator: "dissolve", Status: "unknown"},
			{ID: "save", Operator: "save", Status: "unknown"},
		},
		DefinitionLinks: []models.WorkflowProcessLink{
			{FromNode: "source", ToNode: "dissolve", InputParameter: "input_gdf"},
			{FromNode: "dissolve", OutputPort: "members", ToNode: "save", InputParameter: "input_df"},
			{FromNode: "dissolve", OutputPort: "result", ToNode: "save", InputParameter: "input_df"},
		},
	}
	if !reflect.DeepEqual(process, want) {
		t.Fatalf("process = %#v, want %#v", process, want)
	}
	item.TaskType = commonExecution.TaskTypeQuery
	if workflowProcess(item, commonExecution.Observe(item)) != nil {
		t.Fatal("query exposed a workflow process")
	}
	item.TaskType = commonExecution.TaskTypeWorkflow
	item.ExecutionConfig = nil
	if workflowProcess(item, commonExecution.Observe(item)) != nil {
		t.Fatal("missing historical snapshot fabricated a process")
	}
}

func TestWorkflowProcessNodeStatusRequiresBoundEvidence(t *testing.T) {
	const time = "2026-11-27T03:00:00Z"
	zero := 0
	start := plugin.WorkflowExecutionLog{Timestamp: time, Event: "workflow_started", Level: "INFO"}
	progress := plugin.WorkflowExecutionLog{Timestamp: time, Event: "task_progress", Level: "INFO", TaskID: "source", Progress: &zero}
	dissolve := progress
	dissolve.TaskID = "dissolve"
	success := plugin.WorkflowExecutionLog{Timestamp: time, Event: "workflow_succeeded", Level: "INFO"}
	failure := plugin.WorkflowExecutionLog{Timestamp: time, Event: "workflow_failed", Level: "ERROR"}
	for _, test := range []struct {
		name             string
		attempt, ordinal int
		logs             []plugin.WorkflowExecutionLog
		want             []string
	}{
		{"partial", 1, 0, []plugin.WorkflowExecutionLog{start, progress}, []string{"observed", "unknown", "unknown"}},
		{"operator success with publication failure", 1, 0, []plugin.WorkflowExecutionLog{start, progress, dissolve, success}, []string{"succeeded", "succeeded", "unknown"}},
		{"bound failure", 1, 2, []plugin.WorkflowExecutionLog{start, progress, dissolve, failure}, []string{"observed", "failed", "unknown"}},
		{"unbound failure", 1, 1, []plugin.WorkflowExecutionLog{start, progress, dissolve, failure}, []string{"observed", "observed", "unknown"}},
		{"unknown attempt", 2, 0, []plugin.WorkflowExecutionLog{start, progress, success}, []string{"unknown", "unknown", "unknown"}},
		{"out of order", 1, 0, []plugin.WorkflowExecutionLog{start, progress, failure, success}, []string{"unknown", "unknown", "unknown"}},
		{"missing first event", 1, 0, []plugin.WorkflowExecutionLog{progress, success}, []string{"unknown", "unknown", "unknown"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			item := workflowProcessFixture()
			item.Attempt, item.Status = test.attempt, commonExecution.ExecutionStatusFailed
			item.Metadata = commonModels.JSONMap{"result": map[string]interface{}{"logs": test.logs}}
			if test.ordinal > 0 {
				item.ErrorDetails = commonModels.JSONMap{"diagnostic": &commonExecution.FailureDiagnostic{
					SchemaVersion: commonExecution.FailureDiagnosticSchemaVersion,
					Stage:         "execution", Reason: "operator_group_fields_invalid",
					Operator: &plugin.WorkflowOperatorFailure{NodeIndex: test.ordinal, OperatorID: "dissolve", Parameter: "group_fields"},
				}}
			}
			process := workflowProcess(item, commonExecution.Observe(item))
			var statuses []string
			for _, node := range process.Nodes {
				statuses = append(statuses, node.Status)
			}
			if !reflect.DeepEqual(statuses, test.want) {
				t.Fatalf("statuses = %v, want %v", statuses, test.want)
			}
		})
	}
}

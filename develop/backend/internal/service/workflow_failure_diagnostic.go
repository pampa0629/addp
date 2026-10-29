package service

import (
	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
)

func executionFailureDetails(taskType, suffix, message string, result map[string]interface{}, definition map[string]interface{}) commonModels.JSONMap {
	details := commonModels.JSONMap{"code": "develop." + taskType + "." + suffix, "message": message}
	if taskType != commonExecution.TaskTypeWorkflow || suffix != "execution_failed" {
		return details
	}
	runtime, ok := result["runtime_status"].(map[string]interface{})
	if ok && (runtime["status"] == "failed" || runtime["status"] == "error") && runtime["error_code"] == "INVALID_PARAMS" {
		diagnostic := plugin.ParseWorkflowInputDiagnostic(runtime["diagnostic"])
		if diagnostic != nil && workflowInputDiagnosticMatches(diagnostic, definition) {
			details["code"] = "develop.workflow.invalid_params"
			details["diagnostic"] = &commonExecution.FailureDiagnostic{SchemaVersion: commonExecution.FailureDiagnosticSchemaVersion,
				Stage: diagnostic.Stage, Reason: diagnostic.Reason, Operator: diagnostic.Operator}
		}
		return details
	}
	if !ok || (runtime["status"] != "failed" && runtime["status"] != "error") || runtime["error_code"] != "TARGET_ALREADY_EXISTS" {
		return details
	}
	details["code"] = "develop.workflow.target_exists"
	details["diagnostic"] = commonExecution.NewFailureDiagnostic("result", "target_exists")
	return details
}

func workflowInputDiagnosticMatches(d *plugin.WorkflowInputDiagnostic, definition map[string]interface{}) bool {
	if d == nil || !d.Operator.Valid(d.Reason) {
		return false
	}
	tasks, ok := definition["tasks"].([]interface{})
	if !ok || d.Operator.NodeIndex > len(tasks) {
		return false
	}
	task, ok := tasks[d.Operator.NodeIndex-1].(map[string]interface{})
	return ok && task["operator"] == d.Operator.OperatorID
}

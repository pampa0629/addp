package service

import (
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
)

func executionFailureDetails(taskType, suffix, message string, result map[string]interface{}) commonModels.JSONMap {
	details := commonModels.JSONMap{"code": "develop." + taskType + "." + suffix, "message": message}
	if taskType != commonExecution.TaskTypeWorkflow || suffix != "execution_failed" {
		return details
	}
	runtime, ok := result["runtime_status"].(map[string]interface{})
	if !ok || (runtime["status"] != "failed" && runtime["status"] != "error") || runtime["error_code"] != "TARGET_ALREADY_EXISTS" {
		return details
	}
	details["code"] = "develop.workflow.target_exists"
	details["diagnostic"] = commonExecution.NewFailureDiagnostic("result", "target_exists")
	return details
}

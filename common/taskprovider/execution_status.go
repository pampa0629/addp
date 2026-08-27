package taskprovider

import (
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
)

// ExecutionStatusResponse is the canonical TaskProvider execution status response.
// Outputs is a top-level HTTP projection of the durable metadata.outputs fact.
type ExecutionStatusResponse struct {
	*commonExecution.TaskExecution
	Outputs commonModels.JSONMap `json:"outputs" binding:"required"`
}

// NewExecutionStatusResponse builds the canonical TaskProvider execution response.
func NewExecutionStatusResponse(execution *commonExecution.TaskExecution) ExecutionStatusResponse {
	response := ExecutionStatusResponse{
		TaskExecution: execution,
		Outputs:       commonModels.JSONMap{},
	}
	if execution != nil {
		response.Outputs = ExecutionOutputs(execution.Metadata)
	}
	return response
}

// ExecutionOutputs returns a detached copy of the stable metadata.outputs object.
// Module-private result payloads are deliberately not interpreted as TaskProvider outputs.
func ExecutionOutputs(metadata commonModels.JSONMap) commonModels.JSONMap {
	if metadata == nil {
		return commonModels.JSONMap{}
	}
	var raw map[string]interface{}
	switch outputs := metadata["outputs"].(type) {
	case commonModels.JSONMap:
		raw = map[string]interface{}(outputs)
	case map[string]interface{}:
		raw = outputs
	default:
		return commonModels.JSONMap{}
	}
	result := make(commonModels.JSONMap, len(raw))
	for key, value := range raw {
		result[key] = value
	}
	return result
}

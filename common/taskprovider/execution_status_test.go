package taskprovider

import (
	"encoding/json"
	"testing"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
)

func TestNewExecutionStatusResponseProjectsOnlyMetadataOutputs(t *testing.T) {
	execution := &commonExecution.TaskExecution{
		ExecutionID: "execution-1",
		Status:      commonExecution.ExecutionStatusSuccess,
		Metadata: commonModels.JSONMap{
			"outputs": commonModels.JSONMap{
				"target_locator": "addp://engine/2/path/public/result?type=table",
			},
			"result": commonModels.JSONMap{
				"outputs": commonModels.JSONMap{"legacy": "must-not-be-used"},
			},
		},
	}

	response := NewExecutionStatusResponse(execution)
	if response.Outputs["target_locator"] == nil || response.Outputs["legacy"] != nil {
		t.Fatalf("outputs = %#v", response.Outputs)
	}
	payload, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if decoded["execution_id"] != "execution-1" {
		t.Fatalf("execution_id = %#v", decoded["execution_id"])
	}
	outputs, ok := decoded["outputs"].(map[string]interface{})
	if !ok || outputs["target_locator"] == nil || outputs["legacy"] != nil {
		t.Fatalf("serialized outputs = %#v", decoded["outputs"])
	}
}

func TestNewExecutionStatusResponseReturnsClosedEmptyOutputs(t *testing.T) {
	response := NewExecutionStatusResponse(&commonExecution.TaskExecution{
		ExecutionID: "execution-2",
		Status:      commonExecution.ExecutionStatusSuccess,
		Metadata: commonModels.JSONMap{
			"result": commonModels.JSONMap{
				"outputs": commonModels.JSONMap{"legacy": "must-not-be-used"},
			},
		},
	})
	if response.Outputs == nil || len(response.Outputs) != 0 {
		t.Fatalf("outputs = %#v, want closed empty object", response.Outputs)
	}
	payload, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	outputs, exists := decoded["outputs"]
	if !exists {
		t.Fatal("serialized response omitted outputs")
	}
	if object, ok := outputs.(map[string]interface{}); !ok || len(object) != 0 {
		t.Fatalf("serialized outputs = %#v, want {}", outputs)
	}
}

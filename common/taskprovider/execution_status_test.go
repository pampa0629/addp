package taskprovider

import (
	"encoding/json"
	"strings"
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

func TestExecutionStatusResponseNeverSerializesProfessionalPayloads(t *testing.T) {
	actor := int64(9)
	token := "lease-secret"
	execution := &commonExecution.TaskExecution{ExecutionID: "safe", Status: "failed", ActorPrincipalID: &actor, ExecutionAuthorizationID: &actor, LeaseToken: &token,
		ExecutionConfig: commonModels.JSONMap{"password": "config-secret"},
		Metadata:        commonModels.JSONMap{"result": map[string]interface{}{"rows": "result-secret"}, "outputs": map[string]interface{}{"target_locator": "addp://engine/2/path/public/result?type=table"}, "unknown": "metadata-secret"},
		ErrorDetails:    commonModels.JSONMap{"message": "password=error-secret", "stack": "stack-secret"},
	}
	payload, err := json.Marshal(NewExecutionStatusResponse(execution))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"config-secret", "result-secret", "metadata-secret", "error-secret", "stack-secret", "lease-secret", "execution_config", "actor_principal_id", "execution_authorization_id"} {
		if strings.Contains(string(payload), field) {
			t.Fatalf("exposed %s: %s", field, payload)
		}
	}
	var decoded map[string]interface{}
	_ = json.Unmarshal(payload, &decoded)
	metadata := decoded["metadata"].(map[string]interface{})
	if len(metadata) != 1 || metadata["outputs"] == nil {
		t.Fatalf("metadata=%#v", metadata)
	}
}

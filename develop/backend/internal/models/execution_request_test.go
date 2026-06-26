package models

import (
	"encoding/json"
	"testing"
)

func TestCreateExecutionRequestUsesExecutionConfigContract(t *testing.T) {
	req := CreateExecutionRequest{
		DevType:     "query",
		TriggerType: "manual",
		Content: map[string]interface{}{
			"query":      "SELECT 1",
			"query_type": "sql",
		},
		ExecutionConfig: map[string]interface{}{
			"engine_id": 7,
		},
		Timeout: 30,
	}

	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal CreateExecutionRequest: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if _, ok := decoded["engine_id"]; ok {
		t.Fatalf("CreateExecutionRequest must not expose top-level engine_id: %s", payload)
	}
	config, ok := decoded["execution_config"].(map[string]interface{})
	if !ok {
		t.Fatalf("execution_config missing or invalid: %s", payload)
	}
	if config["engine_id"] != float64(7) {
		t.Fatalf("execution_config.engine_id = %#v, want 7", config["engine_id"])
	}
}

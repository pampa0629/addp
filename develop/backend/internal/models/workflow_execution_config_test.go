package models

import (
	"encoding/json"
	"testing"
)

func TestWorkflowExecutionConfigUsesEngineIDOnly(t *testing.T) {
	cfg := WorkflowExecutionConfig{
		EngineID: 12,
		EngineSpecific: map[string]interface{}{
			"spark_cluster_id": 34,
		},
	}

	payload, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal WorkflowExecutionConfig: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode WorkflowExecutionConfig: %v", err)
	}
	if decoded["engine_id"] != float64(12) {
		t.Fatalf("engine_id = %#v, want 12", decoded["engine_id"])
	}
	if _, ok := decoded["engine_type"]; ok {
		t.Fatalf("WorkflowExecutionConfig must not expose engine_type: %s", payload)
	}
	if _, ok := decoded["engine_specific"]; !ok {
		t.Fatalf("engine_specific missing: %s", payload)
	}
}

package models

import (
	"encoding/json"
	"testing"
	"time"

	commonModels "github.com/addp/common/models"
)

func TestProviderDevTaskUsesTaskTypeContract(t *testing.T) {
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	item := DevTask{
		ID:              12,
		TenantID:        1,
		Name:            "sample workflow",
		DisplayName:     "Sample Workflow",
		DevType:         "workflow",
		Content:         DevTaskContent{"inputs": map[string]interface{}{"area": "farmland"}},
		ExecutionConfig: DevTaskContent{"type": "workflow"},
		EditorLayout: commonModels.JSONMap{
			"nodes": map[string]interface{}{"load": map[string]interface{}{"x": 10, "y": 20}},
		},
		Timeout:   300,
		Status:    "active",
		CreatedAt: now,
		UpdatedAt: now,
	}

	providerTask := NewProviderDevTask(item)
	if providerTask.TaskType != "workflow" {
		t.Fatalf("TaskType = %q, want workflow", providerTask.TaskType)
	}
	if providerTask.Parameters["area"] != "farmland" {
		t.Fatalf("Parameters[area] = %v, want farmland", providerTask.Parameters["area"])
	}

	payload, err := json.Marshal(providerTask)
	if err != nil {
		t.Fatalf("marshal provider task: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal provider task: %v", err)
	}
	if _, ok := decoded["dev_type"]; ok {
		t.Fatalf("provider task JSON must not expose dev_type: %s", payload)
	}
	if decoded["task_type"] != "workflow" {
		t.Fatalf("task_type = %v, want workflow", decoded["task_type"])
	}
	if _, ok := decoded["editor_layout"]; !ok {
		t.Fatalf("provider task JSON must retain editor_layout: %s", payload)
	}
	if _, ok := decoded["schedule"]; ok {
		t.Fatalf("provider task JSON must not expose schedule while supports_schedule=false: %s", payload)
	}
	if _, ok := decoded["enabled"]; ok {
		t.Fatalf("provider task JSON must not expose enabled while supports_schedule=false: %s", payload)
	}
	if _, ok := decoded["next_run_at"]; ok {
		t.Fatalf("provider task JSON must not expose next_run_at while supports_schedule=false: %s", payload)
	}
}

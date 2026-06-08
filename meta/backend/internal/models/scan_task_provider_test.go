package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestProviderScanTaskUsesTaskTypeContract(t *testing.T) {
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	task := ScanTask{
		ID:          1,
		TenantID:    1,
		EngineID:    9,
		Name:        "Business MinIO 定时扫描",
		Description: "scan bucket",
		Enabled:     true,
		Parameters:  JSONMap{"scan_depth": "deep"},
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	providerTask := NewProviderScanTask(task)
	if providerTask.TaskType != "scan" {
		t.Fatalf("TaskType = %q, want scan", providerTask.TaskType)
	}
	if providerTask.DisplayName != task.Name {
		t.Fatalf("DisplayName = %q, want %q", providerTask.DisplayName, task.Name)
	}

	payload, err := json.Marshal(providerTask)
	if err != nil {
		t.Fatalf("marshal provider task: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal provider task: %v", err)
	}
	if decoded["task_type"] != "scan" {
		t.Fatalf("task_type = %v, want scan", decoded["task_type"])
	}
}

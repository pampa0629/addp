package models

import (
	"encoding/json"
	"strings"
	"testing"

	commonExecution "github.com/addp/common/execution"
)

func TestStepRejectsLegacyEngineIdentifierField(t *testing.T) {
	var steps Steps
	err := json.Unmarshal([]byte(`[
		{
			"id":"legacy",
			"name":"Legacy",
			"engine_identifier":"python_workflow",
			"parameters":{},
			"depends_on":[],
			"timeout":300
		}
	]`), &steps)

	if err == nil {
		t.Fatal("expected legacy engine_identifier field to be rejected")
	}
	if !strings.Contains(err.Error(), "engine_identifier") {
		t.Fatalf("error = %q, want containing engine_identifier", err.Error())
	}
}

func TestValidateStepsRequiresTaskProviderReference(t *testing.T) {
	err := ValidateSteps(Steps{
		{
			ID:         "scan",
			Name:       "Scan",
			Parameters: map[string]interface{}{},
			DependsOn:  []string{},
			Timeout:    300,
		},
	})

	if err == nil {
		t.Fatal("expected missing provider/task_type/task_id to be rejected")
	}
	if !strings.Contains(err.Error(), "provider") {
		t.Fatalf("error = %q, want provider validation", err.Error())
	}
}

func TestValidateStepsAcceptsTaskProviderReference(t *testing.T) {
	err := ValidateSteps(Steps{
		{
			ID:         "scan",
			Name:       "Scan",
			Provider:   "meta",
			TaskType:   "scan",
			TaskID:     1,
			Parameters: map[string]interface{}{},
			DependsOn:  []string{},
			Timeout:    300,
		},
		{
			ID:         "workflow",
			Name:       "Workflow",
			Provider:   "develop",
			TaskType:   "workflow",
			TaskID:     8,
			Parameters: map[string]interface{}{"input": "{{scan.item_id}}"},
			DependsOn:  []string{"scan"},
			Timeout:    1800,
		},
	})

	if err != nil {
		t.Fatalf("ValidateSteps() error = %v, want nil", err)
	}
}

func TestValidateStepsRejectsUnknownDependency(t *testing.T) {
	err := ValidateSteps(Steps{
		{
			ID:         "workflow",
			Name:       "Workflow",
			Provider:   "develop",
			TaskType:   "workflow",
			TaskID:     8,
			Parameters: map[string]interface{}{},
			DependsOn:  []string{"missing"},
			Timeout:    1800,
		},
	})

	if err == nil {
		t.Fatal("expected unknown dependency to be rejected")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("error = %q, want missing dependency name", err.Error())
	}
}

func TestValidateStepsRejectsCircularDependency(t *testing.T) {
	err := ValidateSteps(Steps{
		{
			ID:         "a",
			Name:       "A",
			Provider:   "meta",
			TaskType:   "scan",
			TaskID:     1,
			Parameters: map[string]interface{}{},
			DependsOn:  []string{"b"},
			Timeout:    300,
		},
		{
			ID:         "b",
			Name:       "B",
			Provider:   "develop",
			TaskType:   "workflow",
			TaskID:     8,
			Parameters: map[string]interface{}{},
			DependsOn:  []string{"a"},
			Timeout:    300,
		},
	})

	if err == nil {
		t.Fatal("expected circular dependency to be rejected")
	}
	if !strings.Contains(err.Error(), "circular") {
		t.Fatalf("error = %q, want circular dependency validation", err.Error())
	}
}

func TestNewProviderOrchestrationTaskUsesStandardTaskType(t *testing.T) {
	task := NewProviderOrchestrationTask(Orchestration{
		ID:       7,
		TenantID: 1,
		Name:     "每日编排",
		Enabled:  true,
	})

	if task.TaskType != commonExecution.TaskTypeOrchestration {
		t.Fatalf("TaskType = %q, want %q", task.TaskType, commonExecution.TaskTypeOrchestration)
	}
	if task.DisplayName != "每日编排" {
		t.Fatalf("DisplayName = %q, want 每日编排", task.DisplayName)
	}
}

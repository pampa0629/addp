package models

import (
	"encoding/json"
	"errors"
	"fmt"
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
			"engine_identifier":"geopython_workflow",
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

func TestStepRejectsUnsupportedV1ControlFlowFields(t *testing.T) {
	cases := map[string]string{
		"condition": `{"expression":"{{scan.status}} == \"success\""}`,
		"retry":     `{"max_attempts":3}`,
		"approval":  `{"assignee":"admin"}`,
		"parallel":  `true`,
		"branch":    `[{"when":"success","to":"next"}]`,
	}

	for field, value := range cases {
		t.Run(field, func(t *testing.T) {
			var steps Steps
			payload := fmt.Sprintf(`[
				{
					"id":"scan",
					"name":"Scan",
					"provider":"meta",
					"task_type":"scan",
					"task_id":1,
					"parameters":{},
					"depends_on":[],
					"timeout":300,
					"%s":%s
				}
			]`, field, value)

			err := json.Unmarshal([]byte(payload), &steps)
			if err == nil {
				t.Fatalf("expected unsupported control flow field %q to be rejected", field)
			}
			if !strings.Contains(err.Error(), field) {
				t.Fatalf("error = %q, want containing %s", err.Error(), field)
			}
		})
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

func TestOrchestrationDefinitionRequestAppliesOnlyEditableFields(t *testing.T) {
	createdBy := uint(9)
	orch := Orchestration{ID: 42, TenantID: 7, Name: "old", CreatedBy: &createdBy}
	request := OrchestrationDefinitionRequest{
		Name:        "new",
		Description: "description",
		Steps:       Steps{{ID: "scan", Name: "Scan", Provider: "meta", TaskType: "scan", TaskID: 1}},
		Enabled:     true,
		Schedule:    "0 2 * * *",
	}
	request.ApplyTo(&orch)

	if orch.ID != 42 || orch.TenantID != 7 || orch.CreatedBy == nil || *orch.CreatedBy != 9 {
		t.Fatalf("server-owned fields changed: %#v", orch)
	}
	if orch.Name != "new" || !orch.Enabled || orch.Schedule != "0 2 * * *" {
		t.Fatalf("editable fields not applied: %#v", orch)
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
	var validationErr *StepValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("error = %T %v, want *StepValidationError", err, err)
	}
	if validationErr.Code != StepValidationDependencyUnknown || validationErr.StepIndex != 0 || validationErr.Reference != "missing" {
		t.Fatalf("step validation error = %#v", validationErr)
	}
}

func TestValidateStepsRejectsTemplateReferenceWithoutDependency(t *testing.T) {
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
			DependsOn:  []string{},
			Timeout:    1800,
		},
	})

	if err == nil {
		t.Fatal("expected missing data dependency to be rejected")
	}
	if !strings.Contains(err.Error(), "depends_on does not include") {
		t.Fatalf("error = %q, want depends_on validation", err.Error())
	}
}

func TestValidateStepsRejectsTemplateReferenceToUnknownStep(t *testing.T) {
	err := ValidateSteps(Steps{
		{
			ID:         "workflow",
			Name:       "Workflow",
			Provider:   "develop",
			TaskType:   "workflow",
			TaskID:     8,
			Parameters: map[string]interface{}{"input": "{{missing.item_id}}"},
			DependsOn:  []string{},
			Timeout:    1800,
		},
	})

	if err == nil {
		t.Fatal("expected unknown template reference to be rejected")
	}
	if !strings.Contains(err.Error(), `references unknown step "missing"`) {
		t.Fatalf("error = %q, want unknown template reference validation", err.Error())
	}
}

func TestValidateStepsRejectsTemplateSelfReference(t *testing.T) {
	err := ValidateSteps(Steps{
		{
			ID:         "workflow",
			Name:       "Workflow",
			Provider:   "develop",
			TaskType:   "workflow",
			TaskID:     8,
			Parameters: map[string]interface{}{"input": "{{workflow.item_id}}"},
			DependsOn:  []string{"workflow"},
			Timeout:    1800,
		},
	})

	if err == nil {
		t.Fatal("expected template self reference to be rejected")
	}
	if !strings.Contains(err.Error(), "cannot reference itself") {
		t.Fatalf("error = %q, want self reference validation", err.Error())
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

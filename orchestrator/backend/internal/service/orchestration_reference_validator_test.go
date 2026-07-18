package service

import (
	"errors"
	"strings"
	"testing"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/orchestrator/internal/models"
)

type orchestrationLookupStub map[uint]models.Orchestration

func (s orchestrationLookupStub) GetByIDAndTenant(id uint, tenantID uint) (*models.Orchestration, error) {
	orch, ok := s[id]
	if !ok || orch.TenantID != tenantID {
		return nil, errors.New("not found")
	}
	return &orch, nil
}

func TestValidateNoRecursiveOrchestrationReferencesRejectsSelfReference(t *testing.T) {
	err := ValidateNoRecursiveOrchestrationReferences(orchestrationLookupStub{}, 7, 1, models.Steps{
		orchestrationReferenceStep(7),
	})

	if err == nil {
		t.Fatal("expected self reference to be rejected")
	}
	if !strings.Contains(err.Error(), "cannot reference itself") {
		t.Fatalf("error = %q, want self reference message", err.Error())
	}
}

func TestValidateNoRecursiveOrchestrationReferencesRejectsNestedCycle(t *testing.T) {
	lookup := orchestrationLookupStub{
		8: {
			ID:       8,
			TenantID: 1,
			Name:     "B",
			Steps: models.Steps{
				orchestrationReferenceStep(7),
			},
		},
	}

	err := ValidateNoRecursiveOrchestrationReferences(lookup, 7, 1, models.Steps{
		orchestrationReferenceStep(8),
	})

	if err == nil {
		t.Fatal("expected nested cycle to be rejected")
	}
	if !strings.Contains(err.Error(), "7 -> 8 -> 7") {
		t.Fatalf("error = %q, want cycle path", err.Error())
	}
	var validationErr *OrchestrationReferenceValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("error = %T %v, want *OrchestrationReferenceValidationError", err, err)
	}
	if validationErr.Code != OrchestrationReferenceCycle || len(validationErr.Path) != 3 {
		t.Fatalf("reference validation error = %#v", validationErr)
	}
}

func TestValidateNoRecursiveOrchestrationReferencesAcceptsAcyclicReferences(t *testing.T) {
	lookup := orchestrationLookupStub{
		8: {
			ID:       8,
			TenantID: 1,
			Name:     "B",
			Steps: models.Steps{
				orchestrationReferenceStep(9),
			},
		},
		9: {
			ID:       9,
			TenantID: 1,
			Name:     "C",
			Steps: models.Steps{
				{
					ID:       "scan",
					Name:     "Scan",
					Provider: commonExecution.ModuleMeta,
					TaskType: commonExecution.TaskTypeScan,
					TaskID:   3,
				},
			},
		},
	}

	err := ValidateNoRecursiveOrchestrationReferences(lookup, 7, 1, models.Steps{
		orchestrationReferenceStep(8),
	})

	if err != nil {
		t.Fatalf("ValidateNoRecursiveOrchestrationReferences() error = %v, want nil", err)
	}
}

func TestValidateNoRecursiveOrchestrationReferencesRejectsMissingReference(t *testing.T) {
	err := ValidateNoRecursiveOrchestrationReferences(orchestrationLookupStub{}, 7, 1, models.Steps{
		orchestrationReferenceStep(99),
	})

	if err == nil {
		t.Fatal("expected missing referenced orchestration to be rejected")
	}
	if !strings.Contains(err.Error(), "referenced orchestration 99 not found") {
		t.Fatalf("error = %q, want missing reference message", err.Error())
	}
}

func orchestrationReferenceStep(taskID uint) models.Step {
	return models.Step{
		ID:       "orch",
		Name:     "Orchestration",
		Provider: commonExecution.ModuleOrchestrator,
		TaskType: commonExecution.TaskTypeOrchestration,
		TaskID:   taskID,
	}
}

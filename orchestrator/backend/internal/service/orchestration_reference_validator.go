package service

import (
	"strings"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/orchestrator/internal/models"
)

type OrchestrationLookup interface {
	GetByIDAndTenant(id uint, tenantID uint) (*models.Orchestration, error)
}

// ValidateNoRecursiveOrchestrationReferences 校验编排定义之间不会形成递归执行。
func ValidateNoRecursiveOrchestrationReferences(lookup OrchestrationLookup, currentID uint, tenantID uint, steps models.Steps) error {
	if lookup == nil {
		return &OrchestrationReferenceValidationError{Code: OrchestrationReferenceLookupUnavailable}
	}

	validator := orchestrationReferenceValidator{
		lookup:    lookup,
		currentID: currentID,
		tenantID:  tenantID,
		visiting:  map[uint]bool{},
		visited:   map[uint]bool{},
	}

	for _, refID := range referencedOrchestrationIDs(steps) {
		if currentID > 0 && refID == currentID {
			return &OrchestrationReferenceValidationError{Code: OrchestrationReferenceSelf, OrchestrationID: currentID}
		}
		if err := validator.visit(refID, []uint{currentID}); err != nil {
			return err
		}
	}
	return nil
}

type orchestrationReferenceValidator struct {
	lookup    OrchestrationLookup
	currentID uint
	tenantID  uint
	visiting  map[uint]bool
	visited   map[uint]bool
}

func (v *orchestrationReferenceValidator) visit(orchestrationID uint, path []uint) error {
	if v.currentID > 0 && orchestrationID == v.currentID {
		return &OrchestrationReferenceValidationError{Code: OrchestrationReferenceCycle, OrchestrationID: orchestrationID, Path: append(path, orchestrationID)}
	}
	if v.visiting[orchestrationID] {
		return &OrchestrationReferenceValidationError{Code: OrchestrationReferenceCycle, OrchestrationID: orchestrationID, Path: append(path, orchestrationID)}
	}
	if v.visited[orchestrationID] {
		return nil
	}

	orch, err := v.lookup.GetByIDAndTenant(orchestrationID, v.tenantID)
	if err != nil {
		return &OrchestrationReferenceValidationError{Code: OrchestrationReferenceNotFound, OrchestrationID: orchestrationID, Cause: err}
	}

	v.visiting[orchestrationID] = true
	defer delete(v.visiting, orchestrationID)

	for _, refID := range referencedOrchestrationIDs(orch.Steps) {
		if err := v.visit(refID, append(path, orchestrationID)); err != nil {
			return err
		}
	}

	v.visited[orchestrationID] = true
	return nil
}

func referencedOrchestrationIDs(steps models.Steps) []uint {
	ids := make([]uint, 0)
	for _, step := range steps {
		if strings.TrimSpace(step.Provider) != commonExecution.ModuleOrchestrator {
			continue
		}
		if strings.TrimSpace(step.TaskType) != commonExecution.TaskTypeOrchestration {
			continue
		}
		ids = append(ids, step.TaskID)
	}
	return ids
}

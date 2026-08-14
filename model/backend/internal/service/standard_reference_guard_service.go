package service

import (
	"errors"

	"github.com/addp/model/i18n"
	"github.com/addp/model/internal/apperrors"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
	"gorm.io/gorm"
)

type StandardReferenceGuardService struct {
	repo *repository.StandardReferenceGuardRepository
}

func NewStandardReferenceGuardService(repo *repository.StandardReferenceGuardRepository) *StandardReferenceGuardService {
	return &StandardReferenceGuardService{repo: repo}
}

func (s *StandardReferenceGuardService) SetState(tenantID int64, resourceType string, resourceID int64, state string) (*models.StandardReferenceGuardResponse, error) {
	if tenantID <= 0 || resourceID <= 0 || !validStandardResourceType(resourceType) || !validStandardReferenceGuardState(state) {
		return nil, invalidRequest()
	}
	response, err := s.repo.SetState(tenantID, resourceType, resourceID, state)
	if errors.Is(err, repository.ErrStandardReferenceGuardTerminal) {
		return nil, apperrors.Conflict("standard_reference_guard_state_conflict", i18n.MsgStandardReferenceGuardStateConflict)
	}
	return response, err
}

func validStandardResourceType(value string) bool {
	switch value {
	case models.StandardResourceDomain, models.StandardResourceElement,
		models.StandardResourceDimensionHierarchy, models.StandardResourceMetric:
		return true
	default:
		return false
	}
}

func validStandardReferenceGuardState(value string) bool {
	return value == models.StandardReferenceGuardOpen || value == models.StandardReferenceGuardFrozen || value == models.StandardReferenceGuardDeleted
}

func lockStandardReferences(tx *gorm.DB, tenantID int64, references ...models.StandardReference) error {
	if err := repository.LockStandardReferences(tx, tenantID, references...); err != nil {
		if errors.Is(err, repository.ErrStandardReferenceFrozen) {
			return apperrors.Conflict("standard_reference_deleting", i18n.MsgStandardReferenceDeleting)
		}
		return err
	}
	return nil
}

func standardReference(resourceType string, id *int64) models.StandardReference {
	if id == nil {
		return models.StandardReference{ResourceType: resourceType}
	}
	return models.StandardReference{ResourceType: resourceType, ResourceID: *id}
}

func requiredStandardReference(resourceType string, id int64) models.StandardReference {
	return models.StandardReference{ResourceType: resourceType, ResourceID: id}
}

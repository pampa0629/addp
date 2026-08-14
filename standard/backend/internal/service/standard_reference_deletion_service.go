package service

import (
	"context"
	"errors"
	"fmt"

	commonapi "github.com/addp/common/api"
	commonclient "github.com/addp/common/client"
	"github.com/addp/standard/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	standardLifecycleActive   = "active"
	standardLifecycleDeleting = "deleting"
)

var ErrModelReferenceGuardUnavailable = errors.New("model reference guard unavailable")

type StandardResourceReferencedError struct {
	Impact *commonclient.StandardReferenceGuardResponse
}

func (e *StandardResourceReferencedError) Error() string {
	return "standard resource is referenced by model"
}
func (e *StandardResourceReferencedError) Unwrap() error { return commonapi.ErrConflict }

type StandardReferenceDeletionService struct {
	db    *gorm.DB
	model *commonclient.ModelClient
}

func NewStandardReferenceDeletionService(db *gorm.DB, modelClient *commonclient.ModelClient) *StandardReferenceDeletionService {
	return &StandardReferenceDeletionService{db: db, model: modelClient}
}

func (s *StandardReferenceDeletionService) Delete(
	ctx context.Context,
	tenantID int64,
	resourceType string,
	resourceID int64,
	deleteLocal func() error,
) error {
	if s == nil || s.db == nil || s.model == nil {
		return ErrModelReferenceGuardUnavailable
	}
	if err := s.setLifecycleState(tenantID, resourceType, resourceID, standardLifecycleDeleting); err != nil {
		if errors.Is(err, commonapi.ErrNotFound) {
			return s.finalizeMissingResource(ctx, tenantID, resourceType, resourceID, err)
		}
		return err
	}
	client := s.model.WithTenantID(uint(tenantID))
	impact, err := client.SetStandardReferenceGuard(ctx, resourceType, resourceID, commonclient.StandardReferenceGuardFrozen)
	if err != nil {
		_ = s.setLifecycleState(tenantID, resourceType, resourceID, standardLifecycleActive)
		return fmt.Errorf("%w: freeze model reference guard: %v", ErrModelReferenceGuardUnavailable, err)
	}
	if impact.ReferenceCount > 0 {
		if err := s.restore(ctx, client, tenantID, resourceType, resourceID); err != nil {
			return err
		}
		return &StandardResourceReferencedError{Impact: impact}
	}
	if err := deleteLocal(); err != nil {
		if restoreErr := s.restore(ctx, client, tenantID, resourceType, resourceID); restoreErr != nil {
			return fmt.Errorf("delete standard resource: %v; restore guard: %w", err, restoreErr)
		}
		return err
	}
	if _, err := client.SetStandardReferenceGuard(ctx, resourceType, resourceID, commonclient.StandardReferenceGuardDeleted); err != nil {
		return fmt.Errorf("%w: finalize model reference guard: %v", ErrModelReferenceGuardUnavailable, err)
	}
	return nil
}

func (s *StandardReferenceDeletionService) finalizeMissingResource(
	ctx context.Context,
	tenantID int64,
	resourceType string,
	resourceID int64,
	notFound error,
) error {
	_, err := s.model.WithTenantID(uint(tenantID)).SetStandardReferenceGuard(
		ctx,
		resourceType,
		resourceID,
		commonclient.StandardReferenceGuardDeleted,
	)
	if err == nil {
		return nil
	}
	if status, ok := commonclient.TenantAPIStatusCode(err); ok && status == 409 {
		return notFound
	}
	return fmt.Errorf("%w: finalize missing standard resource guard: %v", ErrModelReferenceGuardUnavailable, err)
}

func (s *StandardReferenceDeletionService) restore(ctx context.Context, client *commonclient.ModelClient, tenantID int64, resourceType string, resourceID int64) error {
	if err := s.setLifecycleState(tenantID, resourceType, resourceID, standardLifecycleActive); err != nil {
		return fmt.Errorf("%w: restore standard lifecycle: %v", ErrModelReferenceGuardUnavailable, err)
	}
	if _, err := client.SetStandardReferenceGuard(ctx, resourceType, resourceID, commonclient.StandardReferenceGuardOpen); err != nil {
		return fmt.Errorf("%w: release model reference guard: %v", ErrModelReferenceGuardUnavailable, err)
	}
	return nil
}

func (s *StandardReferenceDeletionService) setLifecycleState(tenantID int64, resourceType string, resourceID int64, state string) error {
	model, err := standardLifecycleModel(resourceType)
	if err != nil {
		return err
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", resourceID, tenantID).First(model).Error; err != nil {
			return wrapStandardLifecycleError(err)
		}
		return wrapStandardLifecycleError(tx.Model(model).Where("id = ? AND tenant_id = ?", resourceID, tenantID).
			Updates(map[string]interface{}{"lifecycle_state": state}).Error)
	})
}

func standardLifecycleModel(resourceType string) (interface{}, error) {
	switch resourceType {
	case "domain":
		return &models.Domain{}, nil
	case "element":
		return &models.Element{}, nil
	case "dimension_hierarchy":
		return &models.DimensionHierarchy{}, nil
	case "metric":
		return &models.Metric{}, nil
	default:
		return nil, commonapi.ErrBadRequest
	}
}

func wrapStandardLifecycleError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return commonapi.ErrNotFound
	}
	return err
}

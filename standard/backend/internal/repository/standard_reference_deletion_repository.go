package repository

import (
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/standard/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const standardReferenceDeletionBatchSize = 100

type StandardReferenceDeletionRepository struct {
	db *gorm.DB
}

func NewStandardReferenceDeletionRepository(db *gorm.DB) *StandardReferenceDeletionRepository {
	return &StandardReferenceDeletionRepository{db: db}
}

// Ensure creates the durable coordination record and marks an existing
// Standard resource as deleting in one local transaction.
func (r *StandardReferenceDeletionRepository) Ensure(tenantID int64, resourceType string, resourceID int64) (bool, error) {
	model, err := standardReferenceDeletionModel(resourceType)
	if err != nil {
		return false, err
	}
	knownDeletion := false
	now := time.Now()
	err = r.db.Transaction(func(tx *gorm.DB) error {
		var operationCount int64
		if err := tx.Model(&models.StandardReferenceDeletion{}).
			Where("tenant_id = ? AND resource_type = ? AND resource_id = ?", tenantID, resourceType, resourceID).
			Count(&operationCount).Error; err != nil {
			return wrapDBError(err)
		}
		knownDeletion = operationCount > 0
		op := &models.StandardReferenceDeletion{
			TenantID: tenantID, ResourceType: resourceType, ResourceID: resourceID,
			NextAttemptAt: now,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "tenant_id"}, {Name: "resource_type"}, {Name: "resource_id"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"next_attempt_at": now,
				"last_error":      "",
				"updated_at":      now,
			}),
		}).Create(op).Error; err != nil {
			return wrapDBError(err)
		}
		result := tx.Model(model).Where("id = ? AND tenant_id = ?", resourceID, tenantID).
			Updates(map[string]interface{}{"lifecycle_state": "deleting"})
		if result.Error != nil {
			return wrapDBError(result.Error)
		}
		knownDeletion = knownDeletion || result.RowsAffected > 0
		return nil
	})
	return knownDeletion, err
}

func (r *StandardReferenceDeletionRepository) LockOperation(tx *gorm.DB, tenantID int64, resourceType string, resourceID int64) (*models.StandardReferenceDeletion, error) {
	var operation models.StandardReferenceDeletion
	result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("tenant_id = ? AND resource_type = ? AND resource_id = ?", tenantID, resourceType, resourceID).
		Limit(1).Find(&operation)
	if result.Error == nil && result.RowsAffected == 0 {
		return &operation, gorm.ErrRecordNotFound
	}
	err := result.Error
	return &operation, wrapDBError(err)
}

func (r *StandardReferenceDeletionRepository) LockResource(tx *gorm.DB, tenantID int64, resourceType string, resourceID int64) (interface{}, bool, error) {
	model, err := standardReferenceDeletionModel(resourceType)
	if err != nil {
		return nil, false, err
	}
	result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND tenant_id = ?", resourceID, tenantID).
		Limit(1).Find(model)
	err = result.Error
	if err == nil && result.RowsAffected == 0 {
		return model, false, nil
	}
	return model, true, wrapDBError(err)
}

func (r *StandardReferenceDeletionRepository) SetActive(tx *gorm.DB, resourceType string, resourceID, tenantID int64) error {
	return r.setLifecycleState(tx, resourceType, resourceID, tenantID, "active")
}

func (r *StandardReferenceDeletionRepository) SetDeleting(tx *gorm.DB, resourceType string, resourceID, tenantID int64) error {
	return r.setLifecycleState(tx, resourceType, resourceID, tenantID, "deleting")
}

func (r *StandardReferenceDeletionRepository) setLifecycleState(tx *gorm.DB, resourceType string, resourceID, tenantID int64, state string) error {
	model, err := standardReferenceDeletionModel(resourceType)
	if err != nil {
		return err
	}
	result := tx.Model(model).Where("id = ? AND tenant_id = ?", resourceID, tenantID).
		Updates(map[string]interface{}{"lifecycle_state": state})
	if result.Error != nil {
		return wrapDBError(result.Error)
	}
	if result.RowsAffected == 0 {
		return commonapi.ErrNotFound
	}
	return nil
}

func (r *StandardReferenceDeletionRepository) DeleteResource(tx *gorm.DB, resourceType string, resourceID, tenantID int64) error {
	model, err := standardReferenceDeletionModel(resourceType)
	if err != nil {
		return err
	}
	result := tx.Where("id = ? AND tenant_id = ?", resourceID, tenantID).Delete(model)
	return requireAffectedRow(result)
}

func (r *StandardReferenceDeletionRepository) DeleteOperation(tx *gorm.DB, id int64) error {
	return requireAffectedRow(tx.Delete(&models.StandardReferenceDeletion{}, id))
}

func (r *StandardReferenceDeletionRepository) ListDue(now time.Time, limit int) ([]models.StandardReferenceDeletion, error) {
	if limit <= 0 || limit > standardReferenceDeletionBatchSize {
		limit = standardReferenceDeletionBatchSize
	}
	var operations []models.StandardReferenceDeletion
	err := r.db.Where("next_attempt_at <= ?", now).
		Order("next_attempt_at ASC, id ASC").Limit(limit).Find(&operations).Error
	return operations, wrapDBError(err)
}

func (r *StandardReferenceDeletionRepository) RecordFailure(id int64, cause error) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var operation models.StandardReferenceDeletion
		result := tx.Where("id = ?", id).Limit(1).Find(&operation)
		if result.Error == nil && result.RowsAffected == 0 {
			return nil
		}
		if result.Error != nil {
			return wrapDBError(result.Error)
		}
		attempts := operation.Attempts + 1
		backoff := time.Duration(1<<minInt(attempts, 6)) * time.Second
		lastError := ""
		if cause != nil {
			lastError = cause.Error()
		}
		return wrapDBError(tx.Model(&operation).Updates(map[string]interface{}{
			"attempts":        attempts,
			"next_attempt_at": time.Now().Add(backoff),
			"last_error":      lastError,
		}).Error)
	})
}

func (r *StandardReferenceDeletionRepository) RecordFailureByResource(tenantID int64, resourceType string, resourceID int64, cause error) error {
	var operation models.StandardReferenceDeletion
	result := r.db.Where("tenant_id = ? AND resource_type = ? AND resource_id = ?", tenantID, resourceType, resourceID).
		Limit(1).Find(&operation)
	if result.Error == nil && result.RowsAffected == 0 {
		return nil
	}
	if result.Error != nil {
		return wrapDBError(result.Error)
	}
	return r.RecordFailure(operation.ID, cause)
}

func standardReferenceDeletionModel(resourceType string) (interface{}, error) {
	switch resourceType {
	case "domain":
		return &models.Domain{}, nil
	case "element":
		return &models.Element{}, nil
	case "metric":
		return &models.MetricDefinition{}, nil
	default:
		return nil, commonapi.ErrBadRequest
	}
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

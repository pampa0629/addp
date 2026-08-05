package repository

import (
	"context"
	"errors"

	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrInferenceScenarioBindingVersionConflict = errors.New("inference scenario binding version conflict")

type InferenceScenarioBindingRepository struct {
	db *gorm.DB
}

func NewInferenceScenarioBindingRepository(db *gorm.DB) *InferenceScenarioBindingRepository {
	return &InferenceScenarioBindingRepository{db: db}
}

func (r *InferenceScenarioBindingRepository) Resolve(ctx context.Context, tenantID uint, scenario string) (*models.InferenceScenarioBinding, error) {
	var value models.InferenceScenarioBinding
	err := r.db.WithContext(ctx).
		Where("scenario_code = ? AND scope_type = 'tenant' AND tenant_id = ?", scenario, tenantID).
		First(&value).Error
	if err == nil {
		return &value, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	err = r.db.WithContext(ctx).
		Where("scenario_code = ? AND scope_type = 'platform' AND tenant_id IS NULL", scenario).
		First(&value).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &value, err
}

func (r *InferenceScenarioBindingRepository) Get(ctx context.Context, scopeType string, tenantID *uint, scenario string) (*models.InferenceScenarioBinding, error) {
	query := r.db.WithContext(ctx).Where("scenario_code = ? AND scope_type = ?", scenario, scopeType)
	if tenantID == nil {
		query = query.Where("tenant_id IS NULL")
	} else {
		query = query.Where("tenant_id = ?", *tenantID)
	}
	var value models.InferenceScenarioBinding
	err := query.First(&value).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &value, err
}

func (r *InferenceScenarioBindingRepository) Save(ctx context.Context, value *models.InferenceScenarioBinding, expectedVersion uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("scenario_code = ? AND scope_type = ?", value.ScenarioCode, value.ScopeType)
		if value.TenantID == nil {
			query = query.Where("tenant_id IS NULL")
		} else {
			query = query.Where("tenant_id = ?", *value.TenantID)
		}
		var current models.InferenceScenarioBinding
		err := query.First(&current).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if expectedVersion != 0 {
				return ErrInferenceScenarioBindingVersionConflict
			}
			value.Version = 1
			return tx.Create(value).Error
		}
		if err != nil {
			return err
		}
		if current.Version != expectedVersion {
			return ErrInferenceScenarioBindingVersionConflict
		}
		value.ID = current.ID
		value.Version = current.Version + 1
		result := tx.Model(&models.InferenceScenarioBinding{}).
			Where("id = ? AND version = ?", current.ID, expectedVersion).
			Updates(map[string]interface{}{
				"model_profile_id": value.ModelProfileID,
				"version":          value.Version,
				"updated_by":       value.UpdatedBy,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrInferenceScenarioBindingVersionConflict
		}
		return tx.First(value, current.ID).Error
	})
}

package repository

import (
	"context"
	"errors"
	"sort"

	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrBaseMapProviderVersionConflict = errors.New("base map provider version conflict")

type BaseMapProviderRepository struct{ db *gorm.DB }

func NewBaseMapProviderRepository(db *gorm.DB) *BaseMapProviderRepository {
	return &BaseMapProviderRepository{db: db}
}

func (r *BaseMapProviderRepository) List(ctx context.Context, scope string, tenantID *uint) ([]models.BaseMapProvider, error) {
	var values []models.BaseMapProvider
	query := r.db.WithContext(ctx).Where("scope_type = ?", scope)
	if tenantID == nil {
		query = query.Where("tenant_id IS NULL")
	} else {
		query = query.Where("tenant_id = ?", *tenantID)
	}
	err := query.Order("sort_order ASC, provider ASC").Find(&values).Error
	return values, err
}

func (r *BaseMapProviderRepository) GetEffective(ctx context.Context, tenantID uint) ([]models.BaseMapProvider, error) {
	var values []models.BaseMapProvider
	if err := r.db.WithContext(ctx).Where("scope_type = ? AND tenant_id IS NULL", models.MapScopePlatform).Find(&values).Error; err != nil {
		return nil, err
	}
	var tenant []models.BaseMapProvider
	if err := r.db.WithContext(ctx).Where("scope_type = ? AND tenant_id = ?", models.MapScopeTenant, tenantID).Find(&tenant).Error; err != nil {
		return nil, err
	}
	byProvider := make(map[string]models.BaseMapProvider, len(values)+len(tenant))
	for _, value := range values {
		byProvider[value.Provider] = value
	}
	for _, value := range tenant {
		byProvider[value.Provider] = value
	}
	result := make([]models.BaseMapProvider, 0, len(byProvider))
	for _, value := range byProvider {
		if value.Enabled {
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].SortOrder == result[j].SortOrder {
			return result[i].Provider < result[j].Provider
		}
		return result[i].SortOrder < result[j].SortOrder
	})
	return result, nil
}

func (r *BaseMapProviderRepository) Save(ctx context.Context, value *models.BaseMapProvider, expected uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current models.BaseMapProvider
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("scope_type = ? AND provider = ?", value.ScopeType, value.Provider)
		if value.TenantID == nil {
			query = query.Where("tenant_id IS NULL")
		} else {
			query = query.Where("tenant_id = ?", *value.TenantID)
		}
		err := query.First(&current).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if expected != 0 {
				return ErrBaseMapProviderVersionConflict
			}
			value.ID, value.Version = 0, 1
			return tx.Create(value).Error
		}
		if err != nil {
			return err
		}
		if current.Version != expected {
			return ErrBaseMapProviderVersionConflict
		}
		value.ID, value.Version = current.ID, current.Version+1
		result := tx.Model(&models.BaseMapProvider{}).Where("id = ? AND version = ?", current.ID, expected).Updates(map[string]interface{}{
			"version": value.Version, "enabled": value.Enabled, "sort_order": value.SortOrder,
			"a_map_key": value.AMapKey, "a_map_security_js_code": value.AMapSecurityJsCode,
			"tdt_key": value.TDTKey, "updated_by": value.UpdatedBy,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrBaseMapProviderVersionConflict
		}
		return nil
	})
}

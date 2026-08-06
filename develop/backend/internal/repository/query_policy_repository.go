package repository

import (
	"context"
	"errors"

	"github.com/addp/develop/backend/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrQueryPolicyVersionConflict = errors.New("query policy version conflict")

type QueryPolicyRepository struct{ db *gorm.DB }

func NewQueryPolicyRepository(db *gorm.DB) *QueryPolicyRepository {
	return &QueryPolicyRepository{db: db}
}

func (r *QueryPolicyRepository) Get(ctx context.Context, scope string, tenantID *uint) (*models.QueryPolicy, error) {
	var value models.QueryPolicy
	q := r.db.WithContext(ctx).Where("scope_type = ?", scope)
	if tenantID == nil {
		q = q.Where("tenant_id IS NULL")
	} else {
		q = q.Where("tenant_id = ?", *tenantID)
	}
	err := q.First(&value).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &value, err
}

func (r *QueryPolicyRepository) Resolve(ctx context.Context, tenantID uint) (*models.QueryPolicy, error) {
	if value, err := r.Get(ctx, "tenant", &tenantID); err != nil || value != nil {
		return value, err
	}
	return r.Get(ctx, "platform", nil)
}

func (r *QueryPolicyRepository) Save(ctx context.Context, value *models.QueryPolicy, expected uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		q := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("scope_type = ?", value.ScopeType)
		if value.TenantID == nil {
			q = q.Where("tenant_id IS NULL")
		} else {
			q = q.Where("tenant_id = ?", *value.TenantID)
		}
		var current models.QueryPolicy
		err := q.First(&current).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if expected != 0 {
				return ErrQueryPolicyVersionConflict
			}
			value.Version = 1
			return tx.Create(value).Error
		}
		if err != nil {
			return err
		}
		if current.Version != expected {
			return ErrQueryPolicyVersionConflict
		}
		value.ID, value.Version = current.ID, current.Version+1
		result := tx.Model(&models.QueryPolicy{}).Where("id = ? AND version = ?", current.ID, expected).Updates(map[string]interface{}{
			"default_query_timeout": value.DefaultQueryTimeout, "max_query_timeout": value.MaxQueryTimeout,
			"query_result_limit": value.QueryResultLimit, "version": value.Version, "updated_by": value.UpdatedBy,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrQueryPolicyVersionConflict
		}
		return tx.First(value, current.ID).Error
	})
}

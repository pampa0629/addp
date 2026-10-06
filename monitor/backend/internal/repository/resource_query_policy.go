package repository

import (
	"context"
	"errors"
	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/resourcequery"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ResourceQueryPolicyRepository struct{ db *gorm.DB }

func NewResourceQueryPolicyRepository(db *gorm.DB) *ResourceQueryPolicyRepository {
	return &ResourceQueryPolicyRepository{db: db}
}
func (r *ResourceQueryPolicyRepository) Get(ctx context.Context) (models.ResourceQueryPolicy, error) {
	var value models.ResourceQueryPolicy
	err := r.db.WithContext(ctx).First(&value, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.ResourceQueryPolicy{Budget: resourcequery.DefaultBudget()}, nil
	}
	return value, err
}
func (r *ResourceQueryPolicyRepository) Save(ctx context.Context, value models.ResourceQueryPolicy, expected uint64) (models.ResourceQueryPolicy, error) {
	if value.Budget.Validate() != nil {
		return value, resourcequery.ErrInvalid
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// The advisory transaction lock also serializes first-ever CAS inserts.
		if err := tx.Exec("SELECT pg_advisory_xact_lock(1836019811, 3)").Error; err != nil {
			return err
		}
		var current models.ResourceQueryPolicy
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, 1).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if expected != 0 {
				return resourcequery.ErrConflict
			}
			value.ID, value.Version = 1, 1
			return tx.Create(&value).Error
		}
		if err != nil {
			return err
		}
		if current.Version != expected {
			return resourcequery.ErrConflict
		}
		value.ID, value.Version = 1, current.Version+1
		return tx.Save(&value).Error
	})
	return value, err
}

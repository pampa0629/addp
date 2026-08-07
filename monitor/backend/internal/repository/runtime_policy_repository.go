package repository

import (
	"context"
	"errors"

	"github.com/addp/monitor/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrRuntimePolicyVersionConflict = errors.New("runtime policy version conflict")

type RuntimePolicyRepository struct{ db *gorm.DB }

func NewRuntimePolicyRepository(db *gorm.DB) *RuntimePolicyRepository {
	return &RuntimePolicyRepository{db: db}
}

func (r *RuntimePolicyRepository) Get(ctx context.Context) (*models.RuntimePolicy, error) {
	var value models.RuntimePolicy
	err := r.db.WithContext(ctx).First(&value, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &value, err
}

func (r *RuntimePolicyRepository) Save(ctx context.Context, value *models.RuntimePolicy, expected uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current models.RuntimePolicy
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, 1).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if expected != 0 {
				return ErrRuntimePolicyVersionConflict
			}
			value.ID, value.Version = 1, 1
			return tx.Create(value).Error
		}
		if err != nil {
			return err
		}
		if current.Version != expected {
			return ErrRuntimePolicyVersionConflict
		}
		value.ID, value.Version = 1, current.Version+1
		result := tx.Model(&models.RuntimePolicy{}).Where("id = 1 AND version = ?", expected).Updates(value)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrRuntimePolicyVersionConflict
		}
		return tx.First(value, 1).Error
	})
}

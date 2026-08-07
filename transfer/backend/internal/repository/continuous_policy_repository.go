package repository

import (
	"context"
	"errors"

	"github.com/addp/transfer/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrContinuousPolicyVersionConflict = errors.New("continuous policy version conflict")

type ContinuousPolicyRepository struct{ db *gorm.DB }

func NewContinuousPolicyRepository(db *gorm.DB) *ContinuousPolicyRepository {
	return &ContinuousPolicyRepository{db: db}
}

func (r *ContinuousPolicyRepository) Get(ctx context.Context) (*models.ContinuousPolicy, error) {
	var value models.ContinuousPolicy
	err := r.db.WithContext(ctx).First(&value, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &value, err
}

func (r *ContinuousPolicyRepository) Save(ctx context.Context, value *models.ContinuousPolicy, expectedVersion uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current models.ContinuousPolicy
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, 1).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if expectedVersion != 0 {
				return ErrContinuousPolicyVersionConflict
			}
			value.ID, value.Version = 1, 1
			return tx.Create(value).Error
		}
		if err != nil {
			return err
		}
		if current.Version != expectedVersion {
			return ErrContinuousPolicyVersionConflict
		}
		value.ID, value.Version = 1, current.Version+1
		result := tx.Model(&models.ContinuousPolicy{}).Where("id = 1 AND version = ?", expectedVersion).Updates(value)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrContinuousPolicyVersionConflict
		}
		return tx.First(value, 1).Error
	})
}

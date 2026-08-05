package repository

import (
	"context"
	"errors"

	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrEmbeddingConfigurationVersionConflict = errors.New("embedding configuration version conflict")

type EmbeddingConfigurationRepository struct {
	db *gorm.DB
}

func NewEmbeddingConfigurationRepository(db *gorm.DB) *EmbeddingConfigurationRepository {
	return &EmbeddingConfigurationRepository{db: db}
}

func (r *EmbeddingConfigurationRepository) Get(ctx context.Context) (*models.EmbeddingConfiguration, error) {
	var value models.EmbeddingConfiguration
	err := r.db.WithContext(ctx).First(&value, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &value, err
}

func (r *EmbeddingConfigurationRepository) Save(ctx context.Context, value *models.EmbeddingConfiguration, expectedVersion uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current models.EmbeddingConfiguration
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, 1).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if expectedVersion != 0 {
				return ErrEmbeddingConfigurationVersionConflict
			}
			value.ID = 1
			value.Version = 1
			return tx.Create(value).Error
		}
		if err != nil {
			return err
		}
		if current.Version != expectedVersion {
			return ErrEmbeddingConfigurationVersionConflict
		}
		value.ID = 1
		value.Version = current.Version + 1
		result := tx.Model(&models.EmbeddingConfiguration{}).
			Where("id = 1 AND version = ?", expectedVersion).
			Updates(map[string]interface{}{
				"version": value.Version, "base_url": value.BaseURL, "model": value.Model,
				"timeout_seconds": value.TimeoutSeconds, "max_distance": value.MaxDistance,
				"max_file_size_mb": value.MaxFileSizeMB, "batch_concurrency": value.BatchConcurrency,
				"updated_by": value.UpdatedBy,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrEmbeddingConfigurationVersionConflict
		}
		return tx.First(value, 1).Error
	})
}

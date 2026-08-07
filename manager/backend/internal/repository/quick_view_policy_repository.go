package repository

import (
	"context"
	"errors"

	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrQuickViewPolicyVersionConflict = errors.New("quick view policy version conflict")

type QuickViewPolicyRepository struct{ db *gorm.DB }

func NewQuickViewPolicyRepository(db *gorm.DB) *QuickViewPolicyRepository {
	return &QuickViewPolicyRepository{db: db}
}
func (r *QuickViewPolicyRepository) Get(ctx context.Context) (*models.QuickViewPolicy, error) {
	var value models.QuickViewPolicy
	err := r.db.WithContext(ctx).First(&value, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &value, err
}
func (r *QuickViewPolicyRepository) Save(ctx context.Context, value *models.QuickViewPolicy, expected uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current models.QuickViewPolicy
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, 1).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if expected != 0 {
				return ErrQuickViewPolicyVersionConflict
			}
			value.ID, value.Version = 1, 1
			return tx.Create(value).Error
		}
		if err != nil {
			return err
		}
		if current.Version != expected {
			return ErrQuickViewPolicyVersionConflict
		}
		value.ID, value.Version = 1, current.Version+1
		result := tx.Model(&models.QuickViewPolicy{}).Where("id = 1 AND version = ?", expected).Updates(map[string]interface{}{"version": value.Version, "direct_flatgeobuf_max_rows": value.DirectFlatGeobufMaxRows, "realtime_tile_timeout_ms": value.RealtimeTileTimeoutMS, "realtime_tile_retry_after_sec": value.RealtimeTileRetryAfterSec, "raster_mosaic_generation_timeout_sec": value.RasterMosaicGenerationTimeoutSec, "updated_by": value.UpdatedBy})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrQuickViewPolicyVersionConflict
		}
		return tx.First(value, 1).Error
	})
}

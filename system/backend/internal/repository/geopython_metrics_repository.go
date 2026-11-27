package repository

import (
	"context"
	"time"

	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *EngineRepository) DeclareRuntimeMetrics(ctx context.Context, identity commonmodels.RuntimeMetricsIdentity) (bool, error) {
	var accepted bool
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// The engine row serializes the first declaration with renewals, replacements and disablement.
		var id uint
		engine := tx.Table("system.engines").Select("id").Where(
			"id=? AND engine_type=? AND engine_origin='extension' AND is_builtin AND tenant_id IS NULL AND lifecycle_state='active'",
			identity.EngineID, commonmodels.RuntimeMetricsEngineType(identity.Kind),
		).Clauses(clause.Locking{Strength: "UPDATE"}).Find(&id)
		if engine.Error != nil || engine.RowsAffected == 0 {
			return engine.Error
		}
		var current models.RuntimeMetricsLease
		lease := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("engine_id=?", id).Find(&current)
		if lease.Error != nil {
			return lease.Error
		}
		identity.StartedAt = identity.StartedAt.UTC().Truncate(time.Microsecond)
		if lease.RowsAffected != 0 && (identity.StartedAt.Before(current.StartedAt) ||
			(identity.StartedAt.Equal(current.StartedAt) && (identity.InstanceID != current.InstanceID || identity.Endpoint != current.Endpoint))) {
			return nil
		}
		next := models.RuntimeMetricsLease{RuntimeMetricsIdentity: identity}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "engine_id"}}, DoUpdates: clause.AssignmentColumns([]string{"instance_id", "started_at", "endpoint", "lease_expires_at"})}).Create(&next).Error; err != nil {
			return err
		}
		accepted = true
		return nil
	})
	return accepted && err == nil, err
}

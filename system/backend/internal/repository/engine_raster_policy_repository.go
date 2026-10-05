package repository

import (
	"context"
	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type EngineRasterPolicyRepository struct{ db *gorm.DB }

func NewEngineRasterPolicyRepository(db *gorm.DB) *EngineRasterPolicyRepository {
	return &EngineRasterPolicyRepository{db: db}
}

func (r *EngineRasterPolicyRepository) Engines(ctx context.Context) ([]models.Engine, error) {
	var engines []models.Engine
	err := r.db.WithContext(ctx).Where("engine_type = ? AND is_builtin = ? AND tenant_id IS NULL AND lifecycle_state = ?", "geopython_workflow", true, models.EngineLifecycleActive).Order("id").Find(&engines).Error
	return engines, err
}

// WithPolicy serializes platform and tenant changes through the engine row.
// Missing rows materialize the declared definition default, never an env fallback.
func (r *EngineRasterPolicyRepository) WithPolicy(ctx context.Context, id uint, tenant uint, fn func(*gorm.DB, *models.EngineRasterPolicy, *models.EngineRasterQuota) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var engine models.Engine
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND engine_type = ? AND is_builtin = ? AND tenant_id IS NULL AND lifecycle_state = ?", id, "geopython_workflow", true, models.EngineLifecycleActive).First(&engine).Error; err != nil {
			return err
		}
		policy := models.DefaultEngineRasterPolicy(id)
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&policy).Error; err != nil {
			return err
		}
		if err := tx.First(&policy, "engine_id = ?", id).Error; err != nil {
			return err
		}
		var quota *models.EngineRasterQuota
		if tenant > 0 {
			quota = &models.EngineRasterQuota{EngineID: id, TenantID: tenant, Version: 1}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(quota).Error; err != nil {
				return err
			}
			if err := tx.First(quota, "engine_id = ? AND tenant_id = ?", id, tenant).Error; err != nil {
				return err
			}
		}
		return fn(tx, &policy, quota)
	})
}
func SaveRasterPolicy(tx *gorm.DB, p *models.EngineRasterPolicy, expected int64) error {
	if expected != p.Version {
		return commonapi.ErrConflict
	}
	p.Version++
	return tx.Save(p).Error
}
func SaveRasterQuota(tx *gorm.DB, q *models.EngineRasterQuota, expected int64) error {
	if expected != q.Version {
		return commonapi.ErrConflict
	}
	q.Version++
	return tx.Save(q).Error
}

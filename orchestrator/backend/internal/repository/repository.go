package repository

import (
	"context"
	"errors"
	"time"

	"github.com/addp/orchestrator/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// OrchestrationRepository 编排数据访问
type OrchestrationRepository struct {
	db *gorm.DB
}

// NewOrchestrationRepository 创建编排仓库
func NewOrchestrationRepository(db *gorm.DB) *OrchestrationRepository {
	return &OrchestrationRepository{db: db}
}

// Create 创建编排
func (r *OrchestrationRepository) Create(orch *models.Orchestration) error {
	return r.db.Create(orch).Error
}

// GetByIDInternal loads by globally unique ID for the owner scheduler only.
func (r *OrchestrationRepository) GetByIDInternal(id uint) (*models.Orchestration, error) {
	var orch models.Orchestration
	if err := r.db.First(&orch, id).Error; err != nil {
		return nil, err
	}
	return &orch, nil
}

// GetByIDAndTenant 根据 ID 和租户获取编排。
func (r *OrchestrationRepository) GetByIDAndTenant(id uint, tenantID uint) (*models.Orchestration, error) {
	var orch models.Orchestration
	if err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&orch).Error; err != nil {
		return nil, err
	}
	return &orch, nil
}

// List 列出租户的编排
func (r *OrchestrationRepository) List(tenantID uint) ([]models.Orchestration, error) {
	var orchs []models.Orchestration
	err := r.db.Where("tenant_id = ?", tenantID).Order("created_at DESC").Find(&orchs).Error
	return orchs, err
}

// ListPaged 分页列出租户的编排。
func (r *OrchestrationRepository) ListPaged(tenantID uint, page, pageSize int) ([]models.Orchestration, int64, error) {
	var total int64
	query := r.db.Model(&models.Orchestration{}).Where("tenant_id = ?", tenantID)
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var orchs []models.Orchestration
	err := query.
		Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&orchs).Error
	return orchs, total, err
}

// ListEnabled 列出所有启用的编排
func (r *OrchestrationRepository) ListEnabled() ([]models.Orchestration, error) {
	var orchs []models.Orchestration
	err := r.db.Where("enabled = ?", true).Find(&orchs).Error
	return orchs, err
}

func (r *OrchestrationRepository) ListMissingNextRun(ctx context.Context) ([]models.Orchestration, error) {
	var orchs []models.Orchestration
	err := r.db.WithContext(ctx).
		Where("enabled = ? AND schedule <> '' AND next_run_at IS NULL", true).
		Find(&orchs).Error
	return orchs, err
}

func (r *OrchestrationRepository) UpdateNextRunAt(ctx context.Context, id uint, nextRunAt *time.Time) error {
	return r.db.WithContext(ctx).
		Model(&models.Orchestration{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{"next_run_at": nextRunAt}).Error
}

func (r *OrchestrationRepository) ListDueIDs(ctx context.Context, now time.Time, limit int) ([]uint, error) {
	if limit <= 0 {
		limit = 100
	}
	var ids []uint
	err := r.db.WithContext(ctx).
		Model(&models.Orchestration{}).
		Where("enabled = ? AND schedule <> '' AND next_run_at IS NOT NULL AND next_run_at <= ?", true, now).
		Order("next_run_at ASC").
		Limit(limit).
		Pluck("id", &ids).Error
	return ids, err
}

func (r *OrchestrationRepository) ClaimDue(ctx context.Context, id uint, schedule string, now time.Time, nextRunAt *time.Time) (*models.Orchestration, error) {
	var claimed *models.Orchestration
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var orch models.Orchestration
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("id = ? AND enabled = ? AND schedule = ? AND next_run_at IS NOT NULL AND next_run_at <= ?", id, true, schedule, now).
			First(&orch).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}

		if err := tx.Model(&models.Orchestration{}).
			Where("id = ?", orch.ID).
			Updates(map[string]interface{}{"next_run_at": nextRunAt}).Error; err != nil {
			return err
		}
		claimed = &orch
		return nil
	})
	return claimed, err
}

// UpdateForTenant updates only user-editable orchestration fields within one tenant.
func (r *OrchestrationRepository) UpdateForTenant(orch *models.Orchestration) error {
	result := r.db.Model(&models.Orchestration{}).
		Where("id = ? AND tenant_id = ?", orch.ID, orch.TenantID).
		Select("name", "description", "steps", "enabled", "schedule", "next_run_at", "updated_at").
		Updates(orch)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// DeleteForTenant soft-deletes an orchestration only within one tenant.
func (r *OrchestrationRepository) DeleteForTenant(id, tenantID uint) error {
	result := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).Delete(&models.Orchestration{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// 注意：ExecutionRepository 已废弃，现在使用统一的 ExecutionService
// 统一执行记录存储在 common.task_executions 表中

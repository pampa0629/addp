package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
)

// GaussianSplatQuickViewRepository 维护高斯泼溅 KSplat 快显任务定义和结果。
type GaussianSplatQuickViewRepository struct {
	db *gorm.DB
}

func NewGaussianSplatQuickViewRepository(db *gorm.DB) *GaussianSplatQuickViewRepository {
	return &GaussianSplatQuickViewRepository{db: db}
}

func (r *GaussianSplatQuickViewRepository) CreateTask(ctx context.Context, task *models.GaussianSplatQuickViewTask) error {
	return r.db.WithContext(ctx).Create(task).Error
}

func (r *GaussianSplatQuickViewRepository) GetTask(ctx context.Context, id uint, tenantID uint) (*models.GaussianSplatQuickViewTask, error) {
	var task models.GaussianSplatQuickViewTask
	err := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &task, err
}

func (r *GaussianSplatQuickViewRepository) GetTaskByItemFingerprint(ctx context.Context, tenantID uint, itemFingerprint string) (*models.GaussianSplatQuickViewTask, error) {
	itemFingerprint = strings.TrimSpace(itemFingerprint)
	if itemFingerprint == "" {
		return nil, nil
	}
	var task models.GaussianSplatQuickViewTask
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND config->'source'->>'item_fingerprint' = ?", tenantID, itemFingerprint).
		Order("updated_at DESC, id DESC").
		First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &task, err
}

func (r *GaussianSplatQuickViewRepository) ListTasks(ctx context.Context, tenantID uint, page, pageSize int) ([]*models.GaussianSplatQuickViewTask, int64, error) {
	query := r.db.WithContext(ctx).
		Model(&models.GaussianSplatQuickViewTask{}).
		Where("tenant_id = ?", tenantID)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page, pageSize = normalizePage(page, pageSize)
	var tasks []*models.GaussianSplatQuickViewTask
	err := query.
		Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&tasks).Error
	return tasks, total, err
}

func (r *GaussianSplatQuickViewRepository) UpdateTask(ctx context.Context, task *models.GaussianSplatQuickViewTask) error {
	return r.db.WithContext(ctx).Save(task).Error
}

func (r *GaussianSplatQuickViewRepository) DeleteTask(ctx context.Context, id uint, tenantID uint) error {
	return r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Delete(&models.GaussianSplatQuickViewTask{}).Error
}

func (r *GaussianSplatQuickViewRepository) UpdateTaskLastExecution(ctx context.Context, id uint, tenantID uint, executionID, status string, runAt time.Time) error {
	return r.db.WithContext(ctx).
		Model(&models.GaussianSplatQuickViewTask{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Updates(map[string]interface{}{
			"last_execution_id":     executionID,
			"last_execution_status": status,
			"last_run_at":           runAt,
		}).Error
}

func (r *GaussianSplatQuickViewRepository) Create(ctx context.Context, result *models.GaussianSplatQuickView) error {
	return r.db.WithContext(ctx).Create(result).Error
}

func (r *GaussianSplatQuickViewRepository) GetByID(ctx context.Context, id uint, tenantID uint) (*models.GaussianSplatQuickView, error) {
	var result models.GaussianSplatQuickView
	err := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &result, err
}

type GaussianSplatQuickViewFilter struct {
	TenantID        uint
	ItemID          uint
	ItemFingerprint string
	TaskID          uint
	Status          string
	Q               string
	Page            int
	PageSize        int
}

func (r *GaussianSplatQuickViewRepository) List(ctx context.Context, filter GaussianSplatQuickViewFilter) ([]*models.GaussianSplatQuickView, int64, error) {
	query := r.db.WithContext(ctx).
		Model(&models.GaussianSplatQuickView{}).
		Where("tenant_id = ?", filter.TenantID)
	if filter.ItemID > 0 {
		query = query.Where("item_id = ?", filter.ItemID)
	}
	if itemFingerprint := strings.TrimSpace(filter.ItemFingerprint); itemFingerprint != "" {
		query = query.Where("item_fingerprint = ?", itemFingerprint)
	}
	if filter.TaskID > 0 {
		query = query.Where("task_id = ?", filter.TaskID)
	}
	if status := strings.TrimSpace(filter.Status); status != "" {
		query = query.Where("status = ?", status)
	}
	if q := strings.TrimSpace(filter.Q); q != "" {
		like := "%" + q + "%"
		query = query.Where(
			"locator ILIKE ? OR item_fingerprint ILIKE ? OR file_name ILIKE ? OR error_message ILIKE ?",
			like, like, like, like,
		)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page, pageSize := normalizePage(filter.Page, filter.PageSize)
	var results []*models.GaussianSplatQuickView
	err := query.
		Order("updated_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&results).Error
	return results, total, err
}

func (r *GaussianSplatQuickViewRepository) GetLatestReadyByFingerprint(ctx context.Context, tenantID uint, itemFingerprint string) (*models.GaussianSplatQuickView, error) {
	itemFingerprint = strings.TrimSpace(itemFingerprint)
	if itemFingerprint == "" {
		return nil, nil
	}
	var result models.GaussianSplatQuickView
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND item_fingerprint = ? AND status = ?", tenantID, itemFingerprint, models.GaussianSplatQuickViewStatusReady).
		Order("updated_at DESC, id DESC").
		First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &result, err
}

func (r *GaussianSplatQuickViewRepository) GetCurrentByFingerprint(ctx context.Context, tenantID uint, itemFingerprint string) (*models.GaussianSplatQuickView, error) {
	itemFingerprint = strings.TrimSpace(itemFingerprint)
	if itemFingerprint == "" {
		return nil, nil
	}
	var result models.GaussianSplatQuickView
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND item_fingerprint = ? AND status <> ?", tenantID, itemFingerprint, models.GaussianSplatQuickViewStatusDeleted).
		Order("updated_at DESC, id DESC").
		First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &result, err
}

func (r *GaussianSplatQuickViewRepository) UpdateFields(ctx context.Context, id uint, tenantID uint, fields map[string]interface{}) error {
	return r.db.WithContext(ctx).
		Model(&models.GaussianSplatQuickView{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Updates(fields).Error
}

func (r *GaussianSplatQuickViewRepository) Delete(ctx context.Context, id uint, tenantID uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.GaussianSplatQuickView{}).
			Where("id = ? AND tenant_id = ?", id, tenantID).
			Update("status", models.GaussianSplatQuickViewStatusDeleted).Error; err != nil {
			return err
		}
		return tx.Where("id = ? AND tenant_id = ?", id, tenantID).
			Delete(&models.GaussianSplatQuickView{}).Error
	})
}

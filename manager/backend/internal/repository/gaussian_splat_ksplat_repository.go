package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
)

// GaussianSplatKSplatRepository 维护 3DGS - KSplat 快显任务定义和结果。
type GaussianSplatKSplatRepository struct {
	db *gorm.DB
}

func NewGaussianSplatKSplatRepository(db *gorm.DB) *GaussianSplatKSplatRepository {
	return &GaussianSplatKSplatRepository{db: db}
}

func (r *GaussianSplatKSplatRepository) CreateTask(ctx context.Context, task *models.GaussianSplatKSplatTask) error {
	return r.db.WithContext(ctx).Create(task).Error
}

func (r *GaussianSplatKSplatRepository) GetTask(ctx context.Context, id uint, tenantID uint) (*models.GaussianSplatKSplatTask, error) {
	var task models.GaussianSplatKSplatTask
	err := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &task, err
}

func (r *GaussianSplatKSplatRepository) GetTaskByItemFingerprint(ctx context.Context, tenantID uint, itemFingerprint string) (*models.GaussianSplatKSplatTask, error) {
	itemFingerprint = strings.TrimSpace(itemFingerprint)
	if itemFingerprint == "" {
		return nil, nil
	}
	var task models.GaussianSplatKSplatTask
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND config->'source'->>'item_fingerprint' = ?", tenantID, itemFingerprint).
		Order("updated_at DESC, id DESC").
		First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &task, err
}

func (r *GaussianSplatKSplatRepository) ListTasks(ctx context.Context, tenantID uint, page, pageSize int) ([]*models.GaussianSplatKSplatTask, int64, error) {
	query := r.db.WithContext(ctx).
		Model(&models.GaussianSplatKSplatTask{}).
		Where("tenant_id = ?", tenantID)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page, pageSize = normalizePage(page, pageSize)
	var tasks []*models.GaussianSplatKSplatTask
	err := query.
		Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&tasks).Error
	return tasks, total, err
}

func (r *GaussianSplatKSplatRepository) UpdateTask(ctx context.Context, task *models.GaussianSplatKSplatTask) error {
	return r.db.WithContext(ctx).Save(task).Error
}

func (r *GaussianSplatKSplatRepository) DeleteTask(ctx context.Context, id uint, tenantID uint) error {
	return r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Delete(&models.GaussianSplatKSplatTask{}).Error
}

func (r *GaussianSplatKSplatRepository) UpdateTaskLastExecution(ctx context.Context, id uint, tenantID uint, executionID, status string, runAt time.Time) error {
	return r.db.WithContext(ctx).
		Model(&models.GaussianSplatKSplatTask{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Updates(map[string]interface{}{
			"last_execution_id":     executionID,
			"last_execution_status": status,
			"last_run_at":           runAt,
		}).Error
}

func (r *GaussianSplatKSplatRepository) Create(ctx context.Context, result *models.GaussianSplatKSplat) error {
	return r.db.WithContext(ctx).Create(result).Error
}

func (r *GaussianSplatKSplatRepository) GetByID(ctx context.Context, id uint, tenantID uint) (*models.GaussianSplatKSplat, error) {
	var result models.GaussianSplatKSplat
	err := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &result, err
}

type GaussianSplatKSplatFilter struct {
	TenantID        uint
	ItemID          uint
	ItemFingerprint string
	TaskID          uint
	Status          string
	Q               string
	Page            int
	PageSize        int
}

func (r *GaussianSplatKSplatRepository) List(ctx context.Context, filter GaussianSplatKSplatFilter) ([]*models.GaussianSplatKSplat, int64, error) {
	query := r.db.WithContext(ctx).
		Model(&models.GaussianSplatKSplat{}).
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
	var results []*models.GaussianSplatKSplat
	err := query.
		Order("updated_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&results).Error
	return results, total, err
}

func (r *GaussianSplatKSplatRepository) GetLatestReadyByFingerprint(ctx context.Context, tenantID uint, itemFingerprint string) (*models.GaussianSplatKSplat, error) {
	itemFingerprint = strings.TrimSpace(itemFingerprint)
	if itemFingerprint == "" {
		return nil, nil
	}
	var result models.GaussianSplatKSplat
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND item_fingerprint = ? AND status = ?", tenantID, itemFingerprint, models.GaussianSplatKSplatStatusReady).
		Order("updated_at DESC, id DESC").
		First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &result, err
}

func (r *GaussianSplatKSplatRepository) GetCurrentByFingerprint(ctx context.Context, tenantID uint, itemFingerprint string) (*models.GaussianSplatKSplat, error) {
	itemFingerprint = strings.TrimSpace(itemFingerprint)
	if itemFingerprint == "" {
		return nil, nil
	}
	var result models.GaussianSplatKSplat
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND item_fingerprint = ? AND status <> ?", tenantID, itemFingerprint, models.GaussianSplatKSplatStatusDeleted).
		Order("updated_at DESC, id DESC").
		First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &result, err
}

func (r *GaussianSplatKSplatRepository) UpdateFields(ctx context.Context, id uint, tenantID uint, fields map[string]interface{}) error {
	return r.db.WithContext(ctx).
		Model(&models.GaussianSplatKSplat{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Updates(fields).Error
}

func (r *GaussianSplatKSplatRepository) Delete(ctx context.Context, id uint, tenantID uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.GaussianSplatKSplat{}).
			Where("id = ? AND tenant_id = ?", id, tenantID).
			Update("status", models.GaussianSplatKSplatStatusDeleted).Error; err != nil {
			return err
		}
		return tx.Where("id = ? AND tenant_id = ?", id, tenantID).
			Delete(&models.GaussianSplatKSplat{}).Error
	})
}

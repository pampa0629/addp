package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
)

// Model3DQuickViewRepository 维护单 OSGB 快显任务定义和 GLB 结果。
type Model3DQuickViewRepository struct {
	db *gorm.DB
}

func NewModel3DQuickViewRepository(db *gorm.DB) *Model3DQuickViewRepository {
	return &Model3DQuickViewRepository{db: db}
}

func (r *Model3DQuickViewRepository) CreateTask(ctx context.Context, task *models.Model3DQuickViewTask) error {
	return r.db.WithContext(ctx).Create(task).Error
}

func (r *Model3DQuickViewRepository) GetTask(ctx context.Context, id uint, tenantID uint) (*models.Model3DQuickViewTask, error) {
	var task models.Model3DQuickViewTask
	err := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &task, err
}

func (r *Model3DQuickViewRepository) GetTaskByItemFingerprint(ctx context.Context, tenantID uint, itemFingerprint string) (*models.Model3DQuickViewTask, error) {
	itemFingerprint = strings.TrimSpace(itemFingerprint)
	if itemFingerprint == "" {
		return nil, nil
	}
	var task models.Model3DQuickViewTask
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND config->'source'->>'item_fingerprint' = ?", tenantID, itemFingerprint).
		Order("updated_at DESC, id DESC").
		First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &task, err
}

func (r *Model3DQuickViewRepository) ListTasks(ctx context.Context, tenantID uint, page, pageSize int) ([]*models.Model3DQuickViewTask, int64, error) {
	query := r.db.WithContext(ctx).
		Model(&models.Model3DQuickViewTask{}).
		Where("tenant_id = ?", tenantID)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page, pageSize = normalizePage(page, pageSize)
	var tasks []*models.Model3DQuickViewTask
	err := query.
		Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&tasks).Error
	return tasks, total, err
}

func (r *Model3DQuickViewRepository) UpdateTask(ctx context.Context, task *models.Model3DQuickViewTask) error {
	return r.db.WithContext(ctx).Save(task).Error
}

func (r *Model3DQuickViewRepository) DeleteTask(ctx context.Context, id uint, tenantID uint) error {
	return r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Delete(&models.Model3DQuickViewTask{}).Error
}

func (r *Model3DQuickViewRepository) UpdateTaskLastExecution(ctx context.Context, id uint, tenantID uint, executionID, status string, runAt time.Time) error {
	return r.db.WithContext(ctx).
		Model(&models.Model3DQuickViewTask{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Updates(map[string]interface{}{
			"last_execution_id":     executionID,
			"last_execution_status": status,
			"last_run_at":           runAt,
		}).Error
}

func (r *Model3DQuickViewRepository) Create(ctx context.Context, result *models.Model3DQuickView) error {
	return r.db.WithContext(ctx).Create(result).Error
}

func (r *Model3DQuickViewRepository) GetByID(ctx context.Context, id uint, tenantID uint) (*models.Model3DQuickView, error) {
	var result models.Model3DQuickView
	err := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &result, err
}

type Model3DQuickViewFilter struct {
	TenantID        uint
	ItemID          uint
	ItemFingerprint string
	TaskID          uint
	Status          string
	Q               string
	Page            int
	PageSize        int
}

func (r *Model3DQuickViewRepository) List(ctx context.Context, filter Model3DQuickViewFilter) ([]*models.Model3DQuickView, int64, error) {
	query := r.db.WithContext(ctx).
		Model(&models.Model3DQuickView{}).
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
	var results []*models.Model3DQuickView
	err := query.
		Order("updated_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&results).Error
	return results, total, err
}

func (r *Model3DQuickViewRepository) GetLatestReadyByFingerprint(ctx context.Context, tenantID uint, itemFingerprint string) (*models.Model3DQuickView, error) {
	itemFingerprint = strings.TrimSpace(itemFingerprint)
	if itemFingerprint == "" {
		return nil, nil
	}
	var result models.Model3DQuickView
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND item_fingerprint = ? AND status = ?", tenantID, itemFingerprint, models.Model3DQuickViewStatusReady).
		Order("updated_at DESC, id DESC").
		First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &result, err
}

func (r *Model3DQuickViewRepository) GetCurrentByFingerprint(ctx context.Context, tenantID uint, itemFingerprint string) (*models.Model3DQuickView, error) {
	itemFingerprint = strings.TrimSpace(itemFingerprint)
	if itemFingerprint == "" {
		return nil, nil
	}
	var result models.Model3DQuickView
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND item_fingerprint = ? AND status <> ?", tenantID, itemFingerprint, models.Model3DQuickViewStatusDeleted).
		Order("updated_at DESC, id DESC").
		First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &result, err
}

func (r *Model3DQuickViewRepository) UpdateFields(ctx context.Context, id uint, tenantID uint, fields map[string]interface{}) error {
	return r.db.WithContext(ctx).
		Model(&models.Model3DQuickView{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Updates(fields).Error
}

func (r *Model3DQuickViewRepository) Delete(ctx context.Context, id uint, tenantID uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Model3DQuickView{}).
			Where("id = ? AND tenant_id = ?", id, tenantID).
			Update("status", models.Model3DQuickViewStatusDeleted).Error; err != nil {
			return err
		}
		return tx.Where("id = ? AND tenant_id = ?", id, tenantID).
			Delete(&models.Model3DQuickView{}).Error
	})
}

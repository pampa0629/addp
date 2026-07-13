package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
)

type CADPreviewRepository struct{ db *gorm.DB }

func NewCADPreviewRepository(db *gorm.DB) *CADPreviewRepository { return &CADPreviewRepository{db: db} }

func (r *CADPreviewRepository) CreateTask(ctx context.Context, task *models.CADPreviewTask) error {
	return r.db.WithContext(ctx).Create(task).Error
}
func (r *CADPreviewRepository) UpdateTask(ctx context.Context, task *models.CADPreviewTask) error {
	return r.db.WithContext(ctx).Save(task).Error
}
func (r *CADPreviewRepository) DeleteTask(ctx context.Context, id, tenantID uint) error {
	return r.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", id, tenantID).Delete(&models.CADPreviewTask{}).Error
}

func (r *CADPreviewRepository) GetTask(ctx context.Context, id, tenantID uint) (*models.CADPreviewTask, error) {
	var task models.CADPreviewTask
	err := r.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", id, tenantID).First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &task, err
}

func (r *CADPreviewRepository) GetTaskByFingerprint(ctx context.Context, tenantID uint, fingerprint string) (*models.CADPreviewTask, error) {
	var task models.CADPreviewTask
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND config->'source'->>'item_fingerprint' = ?", tenantID, strings.TrimSpace(fingerprint)).Order("updated_at DESC").First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &task, err
}

func (r *CADPreviewRepository) ListTasks(ctx context.Context, tenantID uint, page, pageSize int) ([]*models.CADPreviewTask, int64, error) {
	query := r.db.WithContext(ctx).Model(&models.CADPreviewTask{}).Where("tenant_id = ?", tenantID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	page, pageSize = normalizePage(page, pageSize)
	var tasks []*models.CADPreviewTask
	err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&tasks).Error
	return tasks, total, err
}

func (r *CADPreviewRepository) UpdateTaskLastExecution(ctx context.Context, id, tenantID uint, executionID, status string, runAt time.Time) error {
	return r.db.WithContext(ctx).Model(&models.CADPreviewTask{}).Where("id = ? AND tenant_id = ?", id, tenantID).Updates(map[string]interface{}{
		"last_execution_id": executionID, "last_execution_status": status, "last_run_at": runAt,
	}).Error
}

func (r *CADPreviewRepository) Create(ctx context.Context, result *models.CADPreview) error {
	return r.db.WithContext(ctx).Create(result).Error
}

func (r *CADPreviewRepository) GetByID(ctx context.Context, id, tenantID uint) (*models.CADPreview, error) {
	var result models.CADPreview
	err := r.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", id, tenantID).First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &result, err
}

func (r *CADPreviewRepository) GetCurrentByFingerprint(ctx context.Context, tenantID uint, fingerprint string) (*models.CADPreview, error) {
	var result models.CADPreview
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND item_fingerprint = ? AND status <> ?", tenantID, strings.TrimSpace(fingerprint), models.CADPreviewStatusDeleted).Order("updated_at DESC").First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &result, err
}

func (r *CADPreviewRepository) GetLatestReadyByFingerprint(ctx context.Context, tenantID uint, fingerprint string) (*models.CADPreview, error) {
	var result models.CADPreview
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND item_fingerprint = ? AND status = ?", tenantID, strings.TrimSpace(fingerprint), models.CADPreviewStatusReady).Order("updated_at DESC").First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &result, err
}

type CADPreviewFilter struct {
	TenantID uint
	TaskID   uint
	Status   string
	Q        string
	Page     int
	PageSize int
}

func (r *CADPreviewRepository) List(ctx context.Context, filter CADPreviewFilter) ([]*models.CADPreview, int64, error) {
	query := r.db.WithContext(ctx).Model(&models.CADPreview{}).Where("tenant_id = ?", filter.TenantID)
	if filter.TaskID > 0 {
		query = query.Where("task_id = ?", filter.TaskID)
	}
	if status := strings.TrimSpace(filter.Status); status != "" {
		query = query.Where("status = ?", status)
	}
	if q := strings.TrimSpace(filter.Q); q != "" {
		like := "%" + q + "%"
		query = query.Where(
			"locator ILIKE ? OR item_fingerprint ILIKE ? OR source_format ILIKE ? OR error_message ILIKE ?",
			like, like, like, like,
		)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	page, pageSize := normalizePage(filter.Page, filter.PageSize)
	var results []*models.CADPreview
	err := query.Order("updated_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&results).Error
	return results, total, err
}

func (r *CADPreviewRepository) UpdateFields(ctx context.Context, id, tenantID uint, fields map[string]interface{}) error {
	return r.db.WithContext(ctx).Model(&models.CADPreview{}).Where("id = ? AND tenant_id = ?", id, tenantID).Updates(fields).Error
}

func (r *CADPreviewRepository) Delete(ctx context.Context, id, tenantID uint) error {
	return r.db.WithContext(ctx).Model(&models.CADPreview{}).Where("id = ? AND tenant_id = ?", id, tenantID).Updates(map[string]interface{}{"status": models.CADPreviewStatusDeleted, "deleted_at": time.Now()}).Error
}

package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
)

// QuickViewOptimizationRepository 维护快显性能优化任务定义和结果状态。
type QuickViewOptimizationRepository struct {
	db *gorm.DB
}

func NewQuickViewOptimizationRepository(db *gorm.DB) *QuickViewOptimizationRepository {
	return &QuickViewOptimizationRepository{db: db}
}

func (r *QuickViewOptimizationRepository) CreateTask(ctx context.Context, task *models.QuickViewOptimizationTask) error {
	return r.db.WithContext(ctx).Create(task).Error
}

func (r *QuickViewOptimizationRepository) GetTask(ctx context.Context, id uint, tenantID uint) (*models.QuickViewOptimizationTask, error) {
	var task models.QuickViewOptimizationTask
	err := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &task, err
}

func (r *QuickViewOptimizationRepository) ListTasks(ctx context.Context, tenantID uint, page, pageSize int) ([]*models.QuickViewOptimizationTask, int64, error) {
	query := r.db.WithContext(ctx).
		Model(&models.QuickViewOptimizationTask{}).
		Where("tenant_id = ?", tenantID)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page, pageSize = normalizePage(page, pageSize)
	var tasks []*models.QuickViewOptimizationTask
	err := query.
		Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&tasks).Error
	return tasks, total, err
}

func (r *QuickViewOptimizationRepository) UpdateTask(ctx context.Context, task *models.QuickViewOptimizationTask) error {
	return r.db.WithContext(ctx).Save(task).Error
}

func (r *QuickViewOptimizationRepository) DeleteTask(ctx context.Context, id uint, tenantID uint) error {
	return r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Delete(&models.QuickViewOptimizationTask{}).Error
}

func (r *QuickViewOptimizationRepository) UpdateTaskLastExecution(ctx context.Context, id uint, executionID, status string, runAt time.Time) error {
	return r.db.WithContext(ctx).
		Model(&models.QuickViewOptimizationTask{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"last_execution_id":     executionID,
			"last_execution_status": status,
			"last_run_at":           runAt,
		}).Error
}

type QuickViewOptimizationFilter struct {
	TenantID        uint
	ItemID          uint
	ItemFingerprint string
	TaskID          uint
	Status          string
	Q               string
	Page            int
	PageSize        int
}

func (r *QuickViewOptimizationRepository) ListResults(ctx context.Context, filter QuickViewOptimizationFilter) ([]*models.QuickViewOptimization, int64, error) {
	query := r.db.WithContext(ctx).
		Model(&models.QuickViewOptimization{}).
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
			"locator ILIKE ? OR item_fingerprint ILIKE ? OR target_table ILIKE ? OR error_message ILIKE ?",
			like, like, like, like,
		)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page, pageSize := normalizePage(filter.Page, filter.PageSize)
	var results []*models.QuickViewOptimization
	err := query.
		Order("updated_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&results).Error
	return results, total, err
}

func (r *QuickViewOptimizationRepository) GetResult(ctx context.Context, id uint, tenantID uint) (*models.QuickViewOptimization, error) {
	var result models.QuickViewOptimization
	err := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &result, err
}

func (r *QuickViewOptimizationRepository) GetCurrentResult(ctx context.Context, tenantID uint, itemFingerprint, geometryColumn string, targetSRID int) (*models.QuickViewOptimization, error) {
	var result models.QuickViewOptimization
	err := r.db.WithContext(ctx).
		Where(
			"tenant_id = ? AND item_fingerprint = ? AND source_geometry_column = ? AND target_srid = ?",
			tenantID,
			strings.TrimSpace(itemFingerprint),
			strings.TrimSpace(geometryColumn),
			targetSRID,
		).
		First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &result, err
}

func (r *QuickViewOptimizationRepository) GetLatestReadyByFingerprint(ctx context.Context, tenantID uint, itemFingerprint string) (*models.QuickViewOptimization, error) {
	itemFingerprint = strings.TrimSpace(itemFingerprint)
	if itemFingerprint == "" {
		return nil, nil
	}
	var result models.QuickViewOptimization
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND item_fingerprint = ? AND status = ?", tenantID, itemFingerprint, models.QuickViewOptimizationStatusReady).
		Order("updated_at DESC").
		First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &result, err
}

func (r *QuickViewOptimizationRepository) GetReadyByFingerprintGeometry(ctx context.Context, tenantID uint, itemFingerprint, geometryColumn string, targetSRID int) (*models.QuickViewOptimization, error) {
	var result models.QuickViewOptimization
	err := r.db.WithContext(ctx).
		Where(
			"tenant_id = ? AND item_fingerprint = ? AND source_geometry_column = ? AND target_srid = ? AND status = ?",
			tenantID,
			strings.TrimSpace(itemFingerprint),
			strings.TrimSpace(geometryColumn),
			targetSRID,
			models.QuickViewOptimizationStatusReady,
		).
		First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &result, err
}

func (r *QuickViewOptimizationRepository) CreateResult(ctx context.Context, result *models.QuickViewOptimization) error {
	return r.db.WithContext(ctx).Create(result).Error
}

func (r *QuickViewOptimizationRepository) UpdateResult(ctx context.Context, result *models.QuickViewOptimization) error {
	return r.db.WithContext(ctx).Save(result).Error
}

func (r *QuickViewOptimizationRepository) UpdateResultFields(ctx context.Context, id uint, tenantID uint, fields map[string]interface{}) error {
	return r.db.WithContext(ctx).
		Model(&models.QuickViewOptimization{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Updates(fields).Error
}

func (r *QuickViewOptimizationRepository) MarkResultStale(ctx context.Context, id uint, tenantID uint, reason string) error {
	fields := map[string]interface{}{
		"status": models.QuickViewOptimizationStatusStale,
	}
	if strings.TrimSpace(reason) != "" {
		fields["error_message"] = strings.TrimSpace(reason)
	}
	return r.UpdateResultFields(ctx, id, tenantID, fields)
}

func (r *QuickViewOptimizationRepository) DeleteResult(ctx context.Context, id uint, tenantID uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.QuickViewOptimization{}).
			Where("id = ? AND tenant_id = ?", id, tenantID).
			Update("status", models.QuickViewOptimizationStatusDeleted).Error; err != nil {
			return err
		}
		return tx.Where("id = ? AND tenant_id = ?", id, tenantID).
			Delete(&models.QuickViewOptimization{}).Error
	})
}

func normalizePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return page, pageSize
}

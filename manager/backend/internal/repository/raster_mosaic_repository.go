package repository

import (
	"context"
	"errors"
	"time"

	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
)

// RasterMosaicRepository 维护栅格 mosaic 生成任务定义。
type RasterMosaicRepository struct {
	db *gorm.DB
}

func NewRasterMosaicRepository(db *gorm.DB) *RasterMosaicRepository {
	return &RasterMosaicRepository{db: db}
}

func (r *RasterMosaicRepository) CreateTask(ctx context.Context, task *models.RasterMosaicTask) error {
	return r.db.WithContext(ctx).Create(task).Error
}

func (r *RasterMosaicRepository) GetTask(ctx context.Context, id uint, tenantID uint) (*models.RasterMosaicTask, error) {
	var task models.RasterMosaicTask
	err := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &task, err
}

func (r *RasterMosaicRepository) ListTasks(ctx context.Context, tenantID uint, page, pageSize int) ([]*models.RasterMosaicTask, int64, error) {
	query := r.db.WithContext(ctx).
		Model(&models.RasterMosaicTask{}).
		Where("tenant_id = ?", tenantID)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page, pageSize = normalizePage(page, pageSize)
	var tasks []*models.RasterMosaicTask
	err := query.
		Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&tasks).Error
	return tasks, total, err
}

func (r *RasterMosaicRepository) UpdateTask(ctx context.Context, task *models.RasterMosaicTask) error {
	return r.db.WithContext(ctx).Save(task).Error
}

func (r *RasterMosaicRepository) DeleteTask(ctx context.Context, id uint, tenantID uint) error {
	return r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Delete(&models.RasterMosaicTask{}).Error
}

func (r *RasterMosaicRepository) UpdateTaskLastExecution(ctx context.Context, id uint, tenantID uint, executionID, status string, runAt time.Time) error {
	return r.db.WithContext(ctx).
		Model(&models.RasterMosaicTask{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Updates(map[string]interface{}{
			"last_execution_id":     executionID,
			"last_execution_status": status,
			"last_run_at":           runAt,
		}).Error
}

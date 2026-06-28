package repository

import (
	"context"
	"errors"
	"time"

	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
)

// Model3DTilesRepository 维护倾斜摄影三维模型转 3D Tiles 任务定义。
type Model3DTilesRepository struct {
	db *gorm.DB
}

func NewModel3DTilesRepository(db *gorm.DB) *Model3DTilesRepository {
	return &Model3DTilesRepository{db: db}
}

func (r *Model3DTilesRepository) CreateTask(ctx context.Context, task *models.Model3DTilesTask) error {
	return r.db.WithContext(ctx).Create(task).Error
}

func (r *Model3DTilesRepository) GetTask(ctx context.Context, id uint, tenantID uint) (*models.Model3DTilesTask, error) {
	var task models.Model3DTilesTask
	err := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &task, err
}

func (r *Model3DTilesRepository) ListTasks(ctx context.Context, tenantID uint, page, pageSize int) ([]*models.Model3DTilesTask, int64, error) {
	query := r.db.WithContext(ctx).
		Model(&models.Model3DTilesTask{}).
		Where("tenant_id = ?", tenantID)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page, pageSize = normalizePage(page, pageSize)
	var tasks []*models.Model3DTilesTask
	err := query.
		Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&tasks).Error
	return tasks, total, err
}

func (r *Model3DTilesRepository) UpdateTask(ctx context.Context, task *models.Model3DTilesTask) error {
	return r.db.WithContext(ctx).Save(task).Error
}

func (r *Model3DTilesRepository) DeleteTask(ctx context.Context, id uint, tenantID uint) error {
	return r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Delete(&models.Model3DTilesTask{}).Error
}

func (r *Model3DTilesRepository) UpdateTaskLastExecution(ctx context.Context, id uint, tenantID uint, executionID, status string, runAt time.Time) error {
	return r.db.WithContext(ctx).
		Model(&models.Model3DTilesTask{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Updates(map[string]interface{}{
			"last_execution_id":     executionID,
			"last_execution_status": status,
			"last_run_at":           runAt,
		}).Error
}

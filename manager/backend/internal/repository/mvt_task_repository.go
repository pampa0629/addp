package repository

import (
	"context"
	"errors"
	"time"

	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
)

// MvtTaskRepository MVT 瓦片生成任务仓库
type MvtTaskRepository struct {
	db *gorm.DB
}

// NewMvtTaskRepository 创建 MvtTask 仓库
func NewMvtTaskRepository(db *gorm.DB) *MvtTaskRepository {
	return &MvtTaskRepository{db: db}
}

// Create 创建 MvtTask 任务定义
func (r *MvtTaskRepository) Create(ctx context.Context, task *models.MvtTask) error {
	return r.db.WithContext(ctx).Create(task).Error
}

// GetByID 根据 ID 查询
func (r *MvtTaskRepository) GetByID(ctx context.Context, id uint, tenantID uint) (*models.MvtTask, error) {
	var task models.MvtTask
	err := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &task, err
}

// List 分页查询（按租户过滤）
func (r *MvtTaskRepository) List(ctx context.Context, tenantID uint, page, pageSize int) ([]*models.MvtTask, int64, error) {
	query := r.db.WithContext(ctx).Model(&models.MvtTask{}).Where("tenant_id = ?", tenantID)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var tasks []*models.MvtTask
	err := query.
		Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&tasks).Error
	return tasks, total, err
}

// Update 更新任务定义
func (r *MvtTaskRepository) Update(ctx context.Context, task *models.MvtTask) error {
	return r.db.WithContext(ctx).Save(task).Error
}

// Delete 软删除任务定义
func (r *MvtTaskRepository) Delete(ctx context.Context, id uint, tenantID uint) error {
	return r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Delete(&models.MvtTask{}).Error
}

// UpdateLastExecution 回写最近执行信息
func (r *MvtTaskRepository) UpdateLastExecution(ctx context.Context, id uint, executionID, status string, runAt time.Time) error {
	return r.db.WithContext(ctx).
		Model(&models.MvtTask{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"last_execution_id":     executionID,
			"last_execution_status": status,
			"last_run_at":           runAt,
		}).Error
}

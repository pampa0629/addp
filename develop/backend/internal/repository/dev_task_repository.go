package repository

import (
	"fmt"
	"time"

	"github.com/addp/develop/backend/internal/models"
	"gorm.io/gorm"
)

// DevTaskRepository 开发任务数据访问层
type DevTaskRepository struct {
	db *gorm.DB
}

// NewDevTaskRepository 创建开发任务Repository
func NewDevTaskRepository(db *gorm.DB) *DevTaskRepository {
	return &DevTaskRepository{db: db}
}

// Create 创建开发任务
func (r *DevTaskRepository) Create(item *models.DevTask) error {
	return r.db.Create(item).Error
}

// Update 更新开发任务
func (r *DevTaskRepository) Update(item *models.DevTask) error {
	item.UpdatedAt = time.Now()
	return r.db.Save(item).Error
}

// UpdateNotebookRuntimeBinding 原子更新 Notebook 当前任务的运行时绑定。
func (r *DevTaskRepository) UpdateNotebookRuntimeBinding(item *models.DevTask, userID uint) error {
	updatedAt := time.Now()
	result := r.db.Model(&models.DevTask{}).
		Where("id = ? AND tenant_id = ?", item.ID, item.TenantID).
		Updates(map[string]interface{}{
			"content":          item.Content,
			"execution_config": item.ExecutionConfig,
			"updated_by":       userID,
			"updated_at":       updatedAt,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	item.UpdatedBy = &userID
	item.UpdatedAt = updatedAt
	return nil
}

// FindByID 根据ID获取开发任务
func (r *DevTaskRepository) FindByID(id uint, tenantID uint) (*models.DevTask, error) {
	var item models.DevTask
	if err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// FindByName 根据名称获取开发任务
func (r *DevTaskRepository) FindByName(name string, tenantID uint) (*models.DevTask, error) {
	var item models.DevTask
	if err := r.db.Where("name = ? AND tenant_id = ?", name, tenantID).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// List 查询开发任务列表（支持分页和过滤）
func (r *DevTaskRepository) List(req *models.ListDevTasksRequest, tenantID uint) ([]models.DevTask, int64, error) {
	var items []models.DevTask
	var total int64

	query := r.db.Model(&models.DevTask{}).Where("tenant_id = ?", tenantID)

	// 类型过滤
	if req.DevType != "" {
		query = query.Where("dev_type = ?", req.DevType)
	}

	// 状态过滤
	if req.Status != "" {
		query = query.Where("status = ?", req.Status)
	}

	// 标签过滤 (PostgreSQL array contains)
	if req.Tag != "" {
		query = query.Where("? = ANY(tags)", req.Tag)
	}

	// 关键词搜索（名称或描述）
	if req.Keyword != "" {
		keyword := "%" + req.Keyword + "%"
		query = query.Where("name ILIKE ? OR description ILIKE ?", keyword, keyword)
	}

	// 查询总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 分页查询
	offset := (req.Page - 1) * req.PageSize
	if err := query.Order("created_at DESC").
		Limit(req.PageSize).
		Offset(offset).
		Find(&items).Error; err != nil {
		return nil, 0, err
	}

	return items, total, nil
}

// ListNotebookScripts 查询由 Notebook 形态承载的脚本任务。
func (r *DevTaskRepository) ListNotebookScripts(req *models.ListDevTasksRequest, tenantID uint) ([]models.DevTask, int64, error) {
	var items []models.DevTask
	var total int64

	query := r.db.Model(&models.DevTask{}).
		Where("tenant_id = ? AND dev_type = ? AND content->>'notebook_path' IS NOT NULL", tenantID, "script")

	if req.Status != "" {
		query = query.Where("status = ?", req.Status)
	}

	if req.Tag != "" {
		query = query.Where("? = ANY(tags)", req.Tag)
	}

	if req.Keyword != "" {
		keyword := "%" + req.Keyword + "%"
		query = query.Where("name ILIKE ? OR description ILIKE ?", keyword, keyword)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (req.Page - 1) * req.PageSize
	if err := query.Order("created_at DESC").
		Limit(req.PageSize).
		Offset(offset).
		Find(&items).Error; err != nil {
		return nil, 0, err
	}

	return items, total, nil
}

// Delete 删除开发任务（软删除）
func (r *DevTaskRepository) Delete(id uint, tenantID uint) error {
	return r.db.Where("id = ? AND tenant_id = ?", id, tenantID).
		Delete(&models.DevTask{}).Error
}

// UpdateLastExecution 更新最后执行信息
func (r *DevTaskRepository) UpdateLastExecution(id uint, tenantID uint, executionID string, status string, executedAt time.Time) error {
	return r.db.Model(&models.DevTask{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Updates(map[string]interface{}{
			"last_execution_id":     executionID,
			"last_execution_status": status,
			"last_run_at":           executedAt,
		}).Error
}

// UpdateStatus 更新开发任务状态
func (r *DevTaskRepository) UpdateStatus(id uint, tenantID uint, status string) error {
	return r.db.Model(&models.DevTask{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Update("status", status).Error
}

// ExistsByName 检查名称是否已存在
func (r *DevTaskRepository) ExistsByName(name string, tenantID uint, excludeID *uint) (bool, error) {
	var count int64
	query := r.db.Model(&models.DevTask{}).Where("name = ? AND tenant_id = ?", name, tenantID)

	if excludeID != nil {
		query = query.Where("id != ?", *excludeID)
	}

	if err := query.Count(&count).Error; err != nil {
		return false, err
	}

	return count > 0, nil
}

// CountByType 统计各类型的开发任务数量
func (r *DevTaskRepository) CountByType(tenantID uint) (map[string]int64, error) {
	type Result struct {
		DevType string
		Count   int64
	}

	var results []Result
	if err := r.db.Model(&models.DevTask{}).
		Select("dev_type, COUNT(*) as count").
		Where("tenant_id = ? AND deleted_at IS NULL", tenantID).
		Group("dev_type").
		Scan(&results).Error; err != nil {
		return nil, err
	}

	counts := make(map[string]int64)
	for _, result := range results {
		counts[result.DevType] = result.Count
	}

	return counts, nil
}

// BatchUpdateStatus 批量更新状态
func (r *DevTaskRepository) BatchUpdateStatus(ids []uint, tenantID uint, status string) error {
	if len(ids) == 0 {
		return fmt.Errorf("ids cannot be empty")
	}

	return r.db.Model(&models.DevTask{}).
		Where("id IN ? AND tenant_id = ?", ids, tenantID).
		Update("status", status).Error
}

package repository

import (
	"fmt"
	"time"

	"github.com/addp/develop/backend/internal/models"
	"gorm.io/gorm"
)

// DevItemRepository 开发项数据访问层
type DevItemRepository struct {
	db *gorm.DB
}

// NewDevItemRepository 创建开发项Repository
func NewDevItemRepository(db *gorm.DB) *DevItemRepository {
	return &DevItemRepository{db: db}
}

// Create 创建开发项
func (r *DevItemRepository) Create(item *models.DevItem) error {
	return r.db.Create(item).Error
}

// Update 更新开发项
func (r *DevItemRepository) Update(item *models.DevItem) error {
	item.UpdatedAt = time.Now()
	return r.db.Save(item).Error
}

// FindByID 根据ID获取开发项
func (r *DevItemRepository) FindByID(id uint, tenantID uint) (*models.DevItem, error) {
	var item models.DevItem
	if err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// FindByName 根据名称获取开发项
func (r *DevItemRepository) FindByName(name string, tenantID uint) (*models.DevItem, error) {
	var item models.DevItem
	if err := r.db.Where("name = ? AND tenant_id = ?", name, tenantID).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// List 查询开发项列表（支持分页和过滤）
func (r *DevItemRepository) List(req *models.ListDevItemsRequest, tenantID uint) ([]models.DevItem, int64, error) {
	var items []models.DevItem
	var total int64

	query := r.db.Model(&models.DevItem{}).Where("tenant_id = ?", tenantID)

	// 类型过滤
	if req.DevType != "" {
		query = query.Where("dev_type = ?", req.DevType)
	}

	// 状态过滤
	if req.Status != "" {
		query = query.Where("status = ?", req.Status)
	}

	// 资源ID过滤
	if req.ResourceID != nil {
		query = query.Where("resource_id = ?", *req.ResourceID)
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

// Delete 删除开发项（软删除）
func (r *DevItemRepository) Delete(id uint, tenantID uint) error {
	return r.db.Where("id = ? AND tenant_id = ?", id, tenantID).
		Delete(&models.DevItem{}).Error
}

// UpdateLastExecution 更新最后执行信息
func (r *DevItemRepository) UpdateLastExecution(id uint, tenantID uint, executionID uint, status string, executedAt time.Time) error {
	return r.db.Model(&models.DevItem{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Updates(map[string]interface{}{
			"last_execution_id":     executionID,
			"last_execution_status": status,
			"last_executed_at":      executedAt,
		}).Error
}

// FindScheduledItems 查找所有启用了调度的开发项
func (r *DevItemRepository) FindScheduledItems(tenantID uint) ([]models.DevItem, error) {
	var items []models.DevItem
	if err := r.db.Where("tenant_id = ? AND is_scheduled = ? AND status = ?", tenantID, true, "active").
		Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// UpdateStatus 更新开发项状态
func (r *DevItemRepository) UpdateStatus(id uint, tenantID uint, status string) error {
	return r.db.Model(&models.DevItem{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Update("status", status).Error
}

// ExistsByName 检查名称是否已存在
func (r *DevItemRepository) ExistsByName(name string, tenantID uint, excludeID *uint) (bool, error) {
	var count int64
	query := r.db.Model(&models.DevItem{}).Where("name = ? AND tenant_id = ?", name, tenantID)

	if excludeID != nil {
		query = query.Where("id != ?", *excludeID)
	}

	if err := query.Count(&count).Error; err != nil {
		return false, err
	}

	return count > 0, nil
}

// CountByType 统计各类型的开发项数量
func (r *DevItemRepository) CountByType(tenantID uint) (map[string]int64, error) {
	type Result struct {
		DevType string
		Count   int64
	}

	var results []Result
	if err := r.db.Model(&models.DevItem{}).
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

// FindByResourceID 查找使用指定资源的所有开发项
func (r *DevItemRepository) FindByResourceID(resourceID uint, tenantID uint) ([]models.DevItem, error) {
	var items []models.DevItem
	if err := r.db.Where("resource_id = ? AND tenant_id = ?", resourceID, tenantID).
		Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// BatchUpdateStatus 批量更新状态
func (r *DevItemRepository) BatchUpdateStatus(ids []uint, tenantID uint, status string) error {
	if len(ids) == 0 {
		return fmt.Errorf("ids cannot be empty")
	}

	return r.db.Model(&models.DevItem{}).
		Where("id IN ? AND tenant_id = ?", ids, tenantID).
		Update("status", status).Error
}

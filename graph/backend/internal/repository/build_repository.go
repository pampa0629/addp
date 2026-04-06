package repository

import (
	"github.com/addp/graph/internal/models"
	"gorm.io/gorm"
)

type BuildRepository struct {
	db *gorm.DB
}

func NewBuildRepository(db *gorm.DB) *BuildRepository {
	return &BuildRepository{db: db}
}

// ============ BuildTask ============

func (r *BuildRepository) ListTasks(graphID, tenantID uint) ([]models.BuildTask, error) {
	var tasks []models.BuildTask
	err := r.db.Where("graph_id = ? AND tenant_id = ?", graphID, tenantID).
		Order("created_at DESC").Find(&tasks).Error
	return tasks, err
}

func (r *BuildRepository) GetTask(id, tenantID uint) (*models.BuildTask, error) {
	var task models.BuildTask
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&task).Error
	return &task, err
}

func (r *BuildRepository) CreateTask(task *models.BuildTask) error {
	return r.db.Create(task).Error
}

func (r *BuildRepository) UpdateTask(task *models.BuildTask) error {
	return r.db.Save(task).Error
}

func (r *BuildRepository) DeleteTask(id, tenantID uint) error {
	// 级联删除由 ON DELETE CASCADE 处理（materials + review_items）
	return r.db.Where("id = ? AND tenant_id = ?", id, tenantID).Delete(&models.BuildTask{}).Error
}

// ============ BuildMaterial ============

func (r *BuildRepository) ListMaterials(taskID, tenantID uint) ([]models.BuildMaterial, error) {
	var materials []models.BuildMaterial
	err := r.db.Where("task_id = ? AND tenant_id = ?", taskID, tenantID).
		Order("created_at ASC").Find(&materials).Error
	return materials, err
}

func (r *BuildRepository) GetMaterial(id, tenantID uint) (*models.BuildMaterial, error) {
	var mat models.BuildMaterial
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&mat).Error
	return &mat, err
}

func (r *BuildRepository) CreateMaterial(mat *models.BuildMaterial) error {
	return r.db.Create(mat).Error
}

func (r *BuildRepository) UpdateMaterial(mat *models.BuildMaterial) error {
	return r.db.Save(mat).Error
}

func (r *BuildRepository) DeleteMaterial(id, tenantID uint) error {
	return r.db.Where("id = ? AND tenant_id = ?", id, tenantID).Delete(&models.BuildMaterial{}).Error
}

// ============ ReviewItem ============

type ReviewFilter struct {
	GraphID    uint
	TenantID   uint
	TaskID     uint   // 0 表示不过滤
	ItemType   string // "" 表示不过滤
	Status     string // "" 表示不过滤（默认返回 pending）
	Page       int
	PageSize   int
}

func (r *BuildRepository) ListReviewItems(filter ReviewFilter) ([]models.ReviewItem, int64, error) {
	query := r.db.Where("graph_id = ? AND tenant_id = ?", filter.GraphID, filter.TenantID)
	if filter.TaskID > 0 {
		query = query.Where("task_id = ?", filter.TaskID)
	}
	if filter.ItemType != "" {
		query = query.Where("item_type = ?", filter.ItemType)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	} else {
		query = query.Where("status = ?", models.ReviewStatusPending)
	}

	var total int64
	query.Model(&models.ReviewItem{}).Count(&total)

	page := filter.Page
	if page < 1 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	var items []models.ReviewItem
	err := query.Order("confidence ASC, created_at ASC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error
	return items, total, err
}

func (r *BuildRepository) GetReviewItem(id, tenantID uint) (*models.ReviewItem, error) {
	var item models.ReviewItem
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&item).Error
	return &item, err
}

func (r *BuildRepository) CreateReviewItem(item *models.ReviewItem) error {
	return r.db.Create(item).Error
}

func (r *BuildRepository) UpdateReviewItem(item *models.ReviewItem) error {
	return r.db.Save(item).Error
}

func (r *BuildRepository) CountPendingReview(graphID, tenantID uint) (int64, error) {
	var count int64
	err := r.db.Model(&models.ReviewItem{}).
		Where("graph_id = ? AND tenant_id = ? AND status = ?", graphID, tenantID, models.ReviewStatusPending).
		Count(&count).Error
	return count, err
}

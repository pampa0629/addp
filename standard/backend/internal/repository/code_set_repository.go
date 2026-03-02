package repository

import (
	commonrepo "github.com/addp/common/repository"
	"github.com/addp/standard/internal/models"
	"gorm.io/gorm"
)

type CodeSetRepository struct {
	db *gorm.DB
}

func NewCodeSetRepository(db *gorm.DB) *CodeSetRepository {
	return &CodeSetRepository{db: db}
}

// Create 创建码值集
func (r *CodeSetRepository) Create(codeSet *models.CodeSet) error {
	return r.db.Create(codeSet).Error
}

// GetByID 根据 ID 获取码值集
func (r *CodeSetRepository) GetByID(id int64) (*models.CodeSet, error) {
	var codeSet models.CodeSet
	if err := r.db.First(&codeSet, id).Error; err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	return &codeSet, nil
}

// List 获取码值集列表
func (r *CodeSetRepository) List(tenantID int64, keyword, codeSetType string, page, pageSize int) ([]models.CodeSet, int64, error) {
	var codeSets []models.CodeSet
	var total int64

	query := r.db.Where("tenant_id = ?", tenantID)

	if keyword != "" {
		query = query.Where("(name LIKE ? OR code LIKE ?)", "%"+keyword+"%", "%"+keyword+"%")
	}
	if codeSetType != "" {
		query = query.Where("type = ?", codeSetType)
	}

	if err := query.Model(&models.CodeSet{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	if err := query.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&codeSets).Error; err != nil {
		return nil, 0, err
	}

	return codeSets, total, nil
}

// Update 更新码值集
func (r *CodeSetRepository) Update(codeSet *models.CodeSet) error {
	return r.db.Save(codeSet).Error
}

// Delete 删除码值集
func (r *CodeSetRepository) Delete(id int64) error {
	return r.db.Delete(&models.CodeSet{}, id).Error
}

// ExistsByCode 检查 code 是否存在
func (r *CodeSetRepository) ExistsByCode(tenantID int64, code string, excludeID int64) (bool, error) {
	var count int64
	query := r.db.Model(&models.CodeSet{}).Where("tenant_id = ? AND code = ?", tenantID, code)
	if excludeID > 0 {
		query = query.Where("id != ?", excludeID)
	}
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetItems 获取码值项列表
func (r *CodeSetRepository) GetItems(codeSetID int64) ([]models.CodeItem, error) {
	var items []models.CodeItem
	if err := r.db.Where("code_set_id = ?", codeSetID).Order("sort_order ASC, id ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// CreateItem 创建码值项
func (r *CodeSetRepository) CreateItem(item *models.CodeItem) error {
	return r.db.Create(item).Error
}

// GetItemByID 根据 ID 获取码值项
func (r *CodeSetRepository) GetItemByID(id int64) (*models.CodeItem, error) {
	var item models.CodeItem
	if err := r.db.First(&item, id).Error; err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	return &item, nil
}

// UpdateItem 更新码值项
func (r *CodeSetRepository) UpdateItem(item *models.CodeItem) error {
	return r.db.Save(item).Error
}

// DeleteItem 删除码值项
func (r *CodeSetRepository) DeleteItem(id int64) error {
	return r.db.Delete(&models.CodeItem{}, id).Error
}

// ExistsItemByCode 检查码值项 code 是否存在
func (r *CodeSetRepository) ExistsItemByCode(codeSetID int64, code string, excludeID int64) (bool, error) {
	var count int64
	query := r.db.Model(&models.CodeItem{}).Where("code_set_id = ? AND code = ?", codeSetID, code)
	if excludeID > 0 {
		query = query.Where("id != ?", excludeID)
	}
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

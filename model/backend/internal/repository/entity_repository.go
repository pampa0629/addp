package repository

import (
	commonrepo "github.com/addp/common/repository"
	"github.com/addp/model/internal/models"
	"gorm.io/gorm"
)

type EntityRepository struct {
	db *gorm.DB
}

func NewEntityRepository(db *gorm.DB) *EntityRepository {
	return &EntityRepository{db: db}
}

func (r *EntityRepository) Create(entity *models.Entity) error {
	return commonrepo.WrapDBError(r.db.Create(entity).Error)
}

func (r *EntityRepository) GetByID(id, tenantID int64) (*models.Entity, error) {
	var entity models.Entity
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&entity).Error
	return &entity, commonrepo.WrapDBError(err)
}

type ListEntityOptions struct {
	DomainID *int64
	Status   string
	Keyword  string
	Page     int
	PageSize int
}

func (r *EntityRepository) List(tenantID int64, opts ListEntityOptions) ([]models.Entity, int64, error) {
	if opts.Page <= 0 {
		opts.Page = 1
	}
	if opts.PageSize <= 0 {
		opts.PageSize = 20
	}
	if opts.PageSize > 100 {
		opts.PageSize = 100
	}
	query := r.db.Model(&models.Entity{}).Where("tenant_id = ?", tenantID)

	if opts.DomainID != nil {
		query = query.Where("domain_id = ?", *opts.DomainID)
	}
	if opts.Status != "" {
		query = query.Where("status = ?", opts.Status)
	}
	if opts.Keyword != "" {
		query = query.Where("name ILIKE ? OR code ILIKE ? OR description ILIKE ?",
			"%"+opts.Keyword+"%", "%"+opts.Keyword+"%", "%"+opts.Keyword+"%")
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (opts.Page - 1) * opts.PageSize

	var entities []models.Entity
	err := query.Order("created_at DESC, id DESC").Offset(offset).Limit(opts.PageSize).Find(&entities).Error
	return entities, total, err
}

func (r *EntityRepository) Update(entity *models.Entity) error {
	return commonrepo.WrapDBError(r.db.Save(entity).Error)
}

func (r *EntityRepository) Delete(id, tenantID int64) error {
	result := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).Delete(&models.Entity{})
	if result.Error != nil {
		return commonrepo.WrapDBError(result.Error)
	}
	if result.RowsAffected == 0 {
		return commonrepo.WrapDBError(gorm.ErrRecordNotFound)
	}
	return nil
}

func (r *EntityRepository) UpdateStatus(id, tenantID int64, status string, updatedBy int64) error {
	result := r.db.Model(&models.Entity{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Updates(map[string]interface{}{"status": status, "updated_by": updatedBy})
	if result.Error != nil {
		return commonrepo.WrapDBError(result.Error)
	}
	if result.RowsAffected == 0 {
		return commonrepo.WrapDBError(gorm.ErrRecordNotFound)
	}
	return nil
}

func (r *EntityRepository) ExistsByCode(code string, tenantID int64, excludeID int64) (bool, error) {
	var count int64
	query := r.db.Model(&models.Entity{}).Where("code = ? AND tenant_id = ?", code, tenantID)
	if excludeID > 0 {
		query = query.Where("id != ?", excludeID)
	}
	err := query.Count(&count).Error
	return count > 0, err
}

// GetAttributes 获取实体属性列表
func (r *EntityRepository) GetAttributes(entityID int64) ([]models.EntityAttribute, error) {
	var attrs []models.EntityAttribute
	err := r.db.Where("entity_id = ?", entityID).Order("sort_order ASC, id ASC").Find(&attrs).Error
	return attrs, err
}

// CreateAttribute 创建实体属性
func (r *EntityRepository) CreateAttribute(attr *models.EntityAttribute) error {
	return commonrepo.WrapDBError(r.db.Create(attr).Error)
}

// GetAttributeByID 按 ID 获取属性
func (r *EntityRepository) GetAttributeByID(attrID, entityID int64) (*models.EntityAttribute, error) {
	var attr models.EntityAttribute
	err := r.db.Where("id = ? AND entity_id = ?", attrID, entityID).First(&attr).Error
	return &attr, commonrepo.WrapDBError(err)
}

// UpdateAttribute 更新实体属性
func (r *EntityRepository) UpdateAttribute(attr *models.EntityAttribute) error {
	return commonrepo.WrapDBError(r.db.Save(attr).Error)
}

// DeleteAttribute 删除实体属性
func (r *EntityRepository) DeleteAttribute(attrID, entityID int64) error {
	result := r.db.Where("id = ? AND entity_id = ?", attrID, entityID).Delete(&models.EntityAttribute{})
	if result.Error != nil {
		return commonrepo.WrapDBError(result.Error)
	}
	if result.RowsAffected == 0 {
		return commonrepo.WrapDBError(gorm.ErrRecordNotFound)
	}
	return nil
}

// GetByCode 根据code获取实体
func (r *EntityRepository) GetByCode(tenantID int64, code string) (*models.Entity, error) {
	var entity models.Entity
	err := r.db.Where("tenant_id = ? AND code = ?", tenantID, code).First(&entity).Error
	return &entity, commonrepo.WrapDBError(err)
}

// ListByTenantID 列出租户的所有实体
func (r *EntityRepository) ListByTenantID(tenantID int64) ([]models.Entity, error) {
	var entities []models.Entity
	err := r.db.Where("tenant_id = ?", tenantID).Order("created_at DESC").Find(&entities).Error
	return entities, err
}

// DeleteByEntityID 删除实体的所有属性
func (r *EntityRepository) DeleteByEntityID(entityID int64) error {
	return r.db.Where("entity_id = ?", entityID).Delete(&models.EntityAttribute{}).Error
}

func (r *EntityRepository) DB() *gorm.DB { return r.db }

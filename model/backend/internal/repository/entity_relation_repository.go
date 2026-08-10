package repository

import (
	commonrepo "github.com/addp/common/repository"
	"github.com/addp/model/internal/models"
	"gorm.io/gorm"
)

type EntityRelationRepository struct {
	db *gorm.DB
}

func NewEntityRelationRepository(db *gorm.DB) *EntityRelationRepository {
	return &EntityRelationRepository{db: db}
}

// Create 创建实体关系
func (r *EntityRelationRepository) Create(relation *models.EntityRelation) error {
	return r.db.Create(relation).Error
}

// GetByID 根据ID获取实体关系
func (r *EntityRelationRepository) GetByID(id, tenantID int64) (*models.EntityRelation, error) {
	var relation models.EntityRelation
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&relation).Error
	return &relation, commonrepo.WrapDBError(err)
}

// GetByEntityID 获取某个实体的所有关系（作为源或目标）
func (r *EntityRelationRepository) GetByEntityID(tenantID, entityID int64) ([]models.EntityRelation, error) {
	var relations []models.EntityRelation
	err := r.db.Where("tenant_id = ? AND (source_entity = ? OR target_entity = ?)",
		tenantID, entityID, entityID).
		Order("created_at DESC").
		Find(&relations).Error
	return relations, err
}

// ListByTenantID 列出租户的所有实体关系
func (r *EntityRelationRepository) ListByTenantID(tenantID int64) ([]models.EntityRelation, error) {
	var relations []models.EntityRelation
	err := r.db.Where("tenant_id = ?", tenantID).
		Order("created_at DESC").
		Find(&relations).Error
	return relations, err
}

// Update 更新实体关系
func (r *EntityRelationRepository) Update(relation *models.EntityRelation) error {
	return r.db.Save(relation).Error
}

// Delete 删除实体关系
func (r *EntityRelationRepository) Delete(id, tenantID int64) error {
	result := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).Delete(&models.EntityRelation{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// DeleteByTenantID 删除租户的所有实体关系（用于Mermaid导入时清理）
func (r *EntityRelationRepository) DeleteByTenantID(tenantID int64) error {
	return r.db.Where("tenant_id = ?", tenantID).Delete(&models.EntityRelation{}).Error
}

// DeleteByEntityID 删除某个实体相关的所有关系
func (r *EntityRelationRepository) DeleteByEntityID(entityID int64) error {
	return r.db.Where("source_entity = ? OR target_entity = ?", entityID, entityID).
		Delete(&models.EntityRelation{}).Error
}

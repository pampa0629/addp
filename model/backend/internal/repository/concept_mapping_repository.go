package repository

import (
	commonrepo "github.com/addp/common/repository"
	"github.com/addp/model/internal/models"
	"gorm.io/gorm"
)

type ConceptMappingRepository struct {
	db *gorm.DB
}

func NewConceptMappingRepository(db *gorm.DB) *ConceptMappingRepository {
	return &ConceptMappingRepository{db: db}
}

func (r *ConceptMappingRepository) DB() *gorm.DB { return r.db }

func (r *ConceptMappingRepository) ListTableMappings(tableID, tenantID int64) ([]models.LogicalTableEntityMapping, error) {
	items := make([]models.LogicalTableEntityMapping, 0)
	err := r.db.Where("table_id = ? AND tenant_id = ?", tableID, tenantID).Order("id ASC").Find(&items).Error
	return items, commonrepo.WrapDBError(err)
}

func (r *ConceptMappingRepository) ListFieldMappings(tableID, tenantID int64) ([]models.LogicalFieldAttributeMapping, error) {
	items := make([]models.LogicalFieldAttributeMapping, 0)
	err := r.db.Where("table_id = ? AND tenant_id = ?", tableID, tenantID).Order("field_id ASC, id ASC").Find(&items).Error
	return items, commonrepo.WrapDBError(err)
}

func (r *ConceptMappingRepository) ListRelationMappings(tableID, tenantID int64) ([]models.TableRelationEntityRelationMapping, error) {
	items := make([]models.TableRelationEntityRelationMapping, 0)
	err := r.db.Where("table_id = ? AND tenant_id = ?", tableID, tenantID).Order("table_relation_id ASC, id ASC").Find(&items).Error
	return items, commonrepo.WrapDBError(err)
}

func (r *ConceptMappingRepository) ListTableMappingViews(tableID, tenantID int64) ([]models.TableEntityMappingView, error) {
	items := make([]models.TableEntityMappingView, 0)
	err := r.db.Raw(`
		SELECT mapping.*, entity.name AS entity_name, entity.code AS entity_code,
		       entity.status AS entity_status, entity.version AS current_entity_version,
		       (entity.status = 'approved' AND entity.version = mapping.entity_version) AS in_sync
		FROM model.logical_table_entity_mappings AS mapping
		JOIN model.entities AS entity
		  ON entity.id = mapping.entity_id AND entity.tenant_id = mapping.tenant_id
		WHERE mapping.table_id = ? AND mapping.tenant_id = ?
		ORDER BY mapping.id ASC
	`, tableID, tenantID).Scan(&items).Error
	return items, commonrepo.WrapDBError(err)
}

func (r *ConceptMappingRepository) ListFieldMappingViews(tableID, tenantID int64) ([]models.FieldAttributeMappingView, error) {
	items := make([]models.FieldAttributeMappingView, 0)
	err := r.db.Raw(`
		SELECT mapping.*, field.name AS field_name, field.column_name AS field_column_name,
		       entity.name AS entity_name, entity.code AS entity_code,
		       attribute.name AS entity_attribute_name, attribute.column_name AS attribute_column_name
		FROM model.logical_field_attribute_mappings AS mapping
		JOIN model.logical_fields AS field
		  ON field.id = mapping.field_id AND field.table_id = mapping.table_id
		JOIN model.entity_attributes AS attribute
		  ON attribute.id = mapping.entity_attribute_id AND attribute.entity_id = mapping.entity_id
		JOIN model.entities AS entity ON entity.id = mapping.entity_id
		WHERE mapping.table_id = ? AND mapping.tenant_id = ?
		ORDER BY mapping.field_id ASC, mapping.id ASC
	`, tableID, tenantID).Scan(&items).Error
	return items, commonrepo.WrapDBError(err)
}

func (r *ConceptMappingRepository) ListRelationMappingViews(tableID, tenantID int64) ([]models.RelationConceptMappingView, error) {
	items := make([]models.RelationConceptMappingView, 0)
	err := r.db.Raw(`
		SELECT mapping.*, relation.name AS entity_relation_name,
		       source_entity.name AS source_entity_name, target_entity.name AS target_entity_name,
		       relation.version AS current_entity_relation_version,
		       (source_entity.status = 'approved' AND target_entity.status = 'approved'
		        AND relation.version = mapping.entity_relation_version) AS in_sync
		FROM model.table_relation_entity_relation_mappings AS mapping
		JOIN model.entity_relations AS relation
		  ON relation.id = mapping.entity_relation_id AND relation.tenant_id = mapping.tenant_id
		JOIN model.entities AS source_entity
		  ON source_entity.id = relation.source_entity AND source_entity.tenant_id = relation.tenant_id
		JOIN model.entities AS target_entity
		  ON target_entity.id = relation.target_entity AND target_entity.tenant_id = relation.tenant_id
		WHERE mapping.table_id = ? AND mapping.tenant_id = ?
		ORDER BY mapping.table_relation_id ASC, mapping.id ASC
	`, tableID, tenantID).Scan(&items).Error
	return items, commonrepo.WrapDBError(err)
}

func (r *ConceptMappingRepository) Replace(
	tableID, tenantID int64,
	tableMappings []models.LogicalTableEntityMapping,
	fieldMappings []models.LogicalFieldAttributeMapping,
	relationMappings []models.TableRelationEntityRelationMapping,
) error {
	for _, target := range []interface{}{
		&models.TableRelationEntityRelationMapping{},
		&models.LogicalFieldAttributeMapping{},
		&models.LogicalTableEntityMapping{},
	} {
		if err := r.db.Where("table_id = ? AND tenant_id = ?", tableID, tenantID).Delete(target).Error; err != nil {
			return commonrepo.WrapDBError(err)
		}
	}
	if len(tableMappings) > 0 {
		if err := r.db.Create(&tableMappings).Error; err != nil {
			return commonrepo.WrapDBError(err)
		}
	}
	if len(fieldMappings) > 0 {
		if err := r.db.Create(&fieldMappings).Error; err != nil {
			return commonrepo.WrapDBError(err)
		}
	}
	if len(relationMappings) > 0 {
		if err := r.db.Create(&relationMappings).Error; err != nil {
			return commonrepo.WrapDBError(err)
		}
	}
	return nil
}

func (r *ConceptMappingRepository) HasApprovedLogicalTableForEntity(entityID, tenantID int64) (bool, error) {
	var count int64
	err := r.db.Table("model.logical_table_entity_mappings AS mapping").
		Joins("JOIN model.logical_tables AS logical_table ON logical_table.id = mapping.table_id AND logical_table.tenant_id = mapping.tenant_id").
		Where("mapping.tenant_id = ? AND mapping.entity_id = ? AND logical_table.status = 'approved'", tenantID, entityID).
		Count(&count).Error
	return count > 0, commonrepo.WrapDBError(err)
}

func (r *ConceptMappingRepository) HasLogicalTableForEntity(entityID, tenantID int64) (bool, error) {
	var count int64
	err := r.db.Model(&models.LogicalTableEntityMapping{}).
		Where("tenant_id = ? AND entity_id = ?", tenantID, entityID).
		Count(&count).Error
	return count > 0, commonrepo.WrapDBError(err)
}

func (r *ConceptMappingRepository) HasApprovedLogicalTableForEntityRelation(relationID, tenantID int64) (bool, error) {
	var count int64
	err := r.db.Table("model.table_relation_entity_relation_mappings AS mapping").
		Joins("JOIN model.logical_tables AS logical_table ON logical_table.id = mapping.table_id AND logical_table.tenant_id = mapping.tenant_id").
		Where("mapping.tenant_id = ? AND mapping.entity_relation_id = ? AND logical_table.status = 'approved'", tenantID, relationID).
		Count(&count).Error
	return count > 0, commonrepo.WrapDBError(err)
}

func (r *ConceptMappingRepository) HasLogicalTableForEntityRelation(relationID, tenantID int64) (bool, error) {
	var count int64
	err := r.db.Model(&models.TableRelationEntityRelationMapping{}).
		Where("tenant_id = ? AND entity_relation_id = ?", tenantID, relationID).
		Count(&count).Error
	return count > 0, commonrepo.WrapDBError(err)
}

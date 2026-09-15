package repository

import (
	commonrepo "github.com/addp/common/repository"
	"github.com/addp/model/internal/models"
	"gorm.io/gorm"
)

type TableRelationRepository struct {
	db *gorm.DB
}

func NewTableRelationRepository(db *gorm.DB) *TableRelationRepository {
	return &TableRelationRepository{db: db}
}

func (r *TableRelationRepository) DB() *gorm.DB { return r.db }

// ListDetailsByTable 获取事实表的维度关联或维度表的被引用关系。
func (r *TableRelationRepository) ListDetailsByTable(tableID, tenantID int64) ([]models.TableRelationDetail, error) {
	results := make([]models.TableRelationDetail, 0)
	err := r.db.Raw(`
		SELECT
			tr.id,
			tr.source_table,
			st.name AS source_table_name,
			st.code AS source_table_code,
			tr.source_field,
			sf.name AS source_field_name,
			sf.column_name AS source_field_code,
			tr.target_table,
			lt.name AS target_table_name,
			lt.code AS target_table_code,
			lt.scd_type AS target_scd_type,
			tr.target_field,
			tf.name AS target_field_name,
			tf.column_name AS target_field_code,
			tr.relation_type
		FROM model.table_relations tr
		JOIN model.logical_tables st ON st.id = tr.source_table AND st.tenant_id = tr.tenant_id
		JOIN model.logical_fields sf ON sf.id = tr.source_field AND sf.table_id = tr.source_table
		JOIN model.logical_tables lt ON lt.id = tr.target_table AND lt.tenant_id = tr.tenant_id
		JOIN model.logical_fields tf ON tf.id = tr.target_field AND tf.table_id = tr.target_table
		WHERE (tr.source_table = ? OR tr.target_table = ?) AND tr.tenant_id = ?
		ORDER BY tr.created_at ASC
	`, tableID, tableID, tenantID).Scan(&results).Error
	return results, commonrepo.WrapDBError(err)
}

// Create 创建关联
func (r *TableRelationRepository) Create(rel *models.TableRelation) error {
	return commonrepo.WrapDBError(r.db.Create(rel).Error)
}

func (r *TableRelationRepository) GetByID(id, sourceTable, tenantID int64) (*models.TableRelation, error) {
	var relation models.TableRelation
	err := r.db.Where("id = ? AND source_table = ? AND tenant_id = ?", id, sourceTable, tenantID).First(&relation).Error
	return &relation, commonrepo.WrapDBError(err)
}

func (r *TableRelationRepository) ListByTable(tableID, tenantID int64) ([]models.TableRelation, error) {
	var relations []models.TableRelation
	err := r.db.Where("tenant_id = ? AND (source_table = ? OR target_table = ?)", tenantID, tableID, tableID).
		Order("id ASC").Find(&relations).Error
	return relations, commonrepo.WrapDBError(err)
}

// Exists 检查同一对字段间关联是否已存在
func (r *TableRelationRepository) Exists(sourceTable, sourceField, targetTable, targetField, tenantID, excludeID int64) (bool, error) {
	var count int64
	err := r.db.Model(&models.TableRelation{}).
		Where("source_table = ? AND source_field = ? AND target_table = ? AND target_field = ? AND tenant_id = ?",
			sourceTable, sourceField, targetTable, targetField, tenantID).
		Where("id <> ?", excludeID).
		Count(&count).Error
	return count > 0, commonrepo.WrapDBError(err)
}

// Delete 删除关联（按 ID，并验证归属）
func (r *TableRelationRepository) Delete(id, sourceTable, tenantID int64) error {
	result := r.db.Where("id = ? AND source_table = ? AND tenant_id = ?", id, sourceTable, tenantID).
		Delete(&models.TableRelation{})
	if result.Error != nil {
		return commonrepo.WrapDBError(result.Error)
	}
	if result.RowsAffected == 0 {
		return commonrepo.WrapDBError(gorm.ErrRecordNotFound)
	}
	return nil
}

// Update 原位更新关联；源表、租户与创建时间不可变。
func (r *TableRelationRepository) Update(rel *models.TableRelation) error {
	result := r.db.Model(&models.TableRelation{}).
		Where("id = ? AND source_table = ? AND tenant_id = ?", rel.ID, rel.SourceTable, rel.TenantID).
		Updates(map[string]interface{}{"source_field": rel.SourceField, "target_table": rel.TargetTable, "target_field": rel.TargetField, "relation_type": rel.RelationType})
	if result.Error != nil {
		return commonrepo.WrapDBError(result.Error)
	}
	if result.RowsAffected == 0 {
		return commonrepo.WrapDBError(gorm.ErrRecordNotFound)
	}
	updated, err := r.GetByID(rel.ID, rel.SourceTable, rel.TenantID)
	if err == nil {
		*rel = *updated
	}
	return err
}

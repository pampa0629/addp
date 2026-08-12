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

// TableRelationDetail 关联关系详情（含关联表/字段名）
type TableRelationDetail struct {
	ID              int64  `json:"id"`
	SourceTable     int64  `json:"source_table"`
	SourceField     int64  `json:"source_field"`
	SourceFieldName string `json:"source_field_name"`
	TargetTable     int64  `json:"target_table"`
	TargetTableName string `json:"target_table_name"`
	TargetTableCode string `json:"target_table_code"`
	TargetSCDType   int    `json:"target_scd_type"`
	TargetField     int64  `json:"target_field"`
	TargetFieldName string `json:"target_field_name"`
	RelationType    string `json:"relation_type"`
}

// ListByFactTable 获取事实表关联的维度表（含字段信息）
func (r *TableRelationRepository) ListByFactTable(factTableID, tenantID int64) ([]TableRelationDetail, error) {
	var results []TableRelationDetail
	err := r.db.Raw(`
		SELECT
			tr.id,
			tr.source_table,
			tr.source_field,
			sf.name AS source_field_name,
			tr.target_table,
			lt.name AS target_table_name,
			lt.code AS target_table_code,
			lt.scd_type AS target_scd_type,
			tr.target_field,
			tf.name AS target_field_name,
			tr.relation_type
		FROM model.table_relations tr
		JOIN model.logical_fields sf ON sf.id = tr.source_field
		JOIN model.logical_tables lt ON lt.id = tr.target_table
		JOIN model.logical_fields tf ON tf.id = tr.target_field
		WHERE tr.source_table = ? AND tr.tenant_id = ?
		ORDER BY tr.created_at ASC
	`, factTableID, tenantID).Scan(&results).Error
	return results, err
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
		Find(&relations).Error
	return relations, err
}

// Exists 检查同一对字段间关联是否已存在
func (r *TableRelationRepository) Exists(sourceTable, sourceField, targetTable, targetField, tenantID int64) (bool, error) {
	var count int64
	err := r.db.Model(&models.TableRelation{}).
		Where("source_table = ? AND source_field = ? AND target_table = ? AND target_field = ? AND tenant_id = ?",
			sourceTable, sourceField, targetTable, targetField, tenantID).
		Count(&count).Error
	return count > 0, err
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

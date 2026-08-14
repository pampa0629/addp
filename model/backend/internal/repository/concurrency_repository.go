package repository

import (
	"time"

	commonrepo "github.com/addp/common/repository"
	"github.com/addp/model/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func LockEntity(db *gorm.DB, id, tenantID int64) (*models.Entity, error) {
	var entity models.Entity
	err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND tenant_id = ?", id, tenantID).First(&entity).Error
	return &entity, commonrepo.WrapDBError(err)
}

func LockLogicalTable(db *gorm.DB, id, tenantID int64) (*models.LogicalTable, error) {
	var table models.LogicalTable
	err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND tenant_id = ?", id, tenantID).First(&table).Error
	return &table, commonrepo.WrapDBError(err)
}

func LockLogicalTablesByTenant(db *gorm.DB, tenantID int64) ([]models.LogicalTable, error) {
	var tables []models.LogicalTable
	err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("tenant_id = ?", tenantID).
		Order("id ASC").
		Find(&tables).Error
	return tables, commonrepo.WrapDBError(err)
}

func LockDWLayer(db *gorm.DB, id, tenantID int64) (*models.DWLayer, error) {
	var layer models.DWLayer
	err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND tenant_id = ?", id, tenantID).First(&layer).Error
	return &layer, commonrepo.WrapDBError(err)
}

func LockDWLayerByCode(db *gorm.DB, code string, tenantID int64) (*models.DWLayer, error) {
	var layer models.DWLayer
	err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("layer_code = ? AND tenant_id = ?", code, tenantID).First(&layer).Error
	return &layer, commonrepo.WrapDBError(err)
}

func LockEntityRelation(db *gorm.DB, id, tenantID int64) (*models.EntityRelation, error) {
	var relation models.EntityRelation
	err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND tenant_id = ?", id, tenantID).First(&relation).Error
	return &relation, commonrepo.WrapDBError(err)
}

func AdvanceEntityVersion(db *gorm.DB, id, tenantID, version int64) (int64, error) {
	return advanceVersion(db, &models.Entity{}, id, tenantID, version)
}

func AdvanceLogicalTableVersion(db *gorm.DB, id, tenantID, version int64) (int64, error) {
	return advanceVersion(db, &models.LogicalTable{}, id, tenantID, version)
}

func advanceVersion(db *gorm.DB, model interface{}, id, tenantID, version int64) (int64, error) {
	result := db.Model(model).
		Where("id = ? AND tenant_id = ? AND version = ?", id, tenantID, version).
		Updates(map[string]interface{}{
			"version":    gorm.Expr("version + 1"),
			"updated_at": time.Now(),
		})
	if result.Error != nil {
		return 0, commonrepo.WrapDBError(result.Error)
	}
	if result.RowsAffected != 1 {
		return 0, gorm.ErrRecordNotFound
	}
	return version + 1, nil
}

func LockEntityModelRevision(db *gorm.DB, tenantID int64) (*models.EntityModelRevision, error) {
	revision := &models.EntityModelRevision{TenantID: tenantID, Revision: 1}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(revision).Error; err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("tenant_id = ?", tenantID).First(revision).Error; err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	return revision, nil
}

func AdvanceEntityModelRevision(db *gorm.DB, tenantID, revision int64) (int64, error) {
	result := db.Model(&models.EntityModelRevision{}).
		Where("tenant_id = ? AND revision = ?", tenantID, revision).
		Updates(map[string]interface{}{
			"revision":   gorm.Expr("revision + 1"),
			"updated_at": time.Now(),
		})
	if result.Error != nil {
		return 0, commonrepo.WrapDBError(result.Error)
	}
	if result.RowsAffected != 1 {
		return 0, gorm.ErrRecordNotFound
	}
	return revision + 1, nil
}

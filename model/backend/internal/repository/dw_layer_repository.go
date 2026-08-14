package repository

import (
	commonrepo "github.com/addp/common/repository"
	"github.com/addp/model/internal/models"
	"gorm.io/gorm"
)

type DWLayerRepository struct {
	db *gorm.DB
}

func NewDWLayerRepository(db *gorm.DB) *DWLayerRepository {
	return &DWLayerRepository{db: db}
}

func (r *DWLayerRepository) Create(layer *models.DWLayer) error {
	return commonrepo.WrapDBError(r.db.Create(layer).Error)
}

func (r *DWLayerRepository) GetByID(id, tenantID int64) (*models.DWLayer, error) {
	var layer models.DWLayer
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&layer).Error
	return &layer, commonrepo.WrapDBError(err)
}

func (r *DWLayerRepository) List(tenantID int64) ([]models.DWLayer, error) {
	var layers []models.DWLayer
	err := r.db.Where("tenant_id = ?", tenantID).Order("sort_order ASC, created_at ASC").Find(&layers).Error
	return layers, commonrepo.WrapDBError(err)
}

func (r *DWLayerRepository) Update(layer *models.DWLayer) error {
	result := r.db.Model(&models.DWLayer{}).
		Where("id = ? AND tenant_id = ? AND version = ?", layer.ID, layer.TenantID, layer.Version).
		Updates(map[string]interface{}{
			"layer_name": layer.LayerName, "description": layer.Description, "naming_rule": layer.NamingRule,
			"quality_sla": layer.QualitySLA, "sort_order": layer.SortOrder, "version": gorm.Expr("version + 1"),
		})
	if result.Error != nil {
		return commonrepo.WrapDBError(result.Error)
	}
	if result.RowsAffected != 1 {
		return commonrepo.WrapDBError(gorm.ErrRecordNotFound)
	}
	layer.Version++
	return nil
}

func (r *DWLayerRepository) Delete(id, tenantID, version int64) error {
	result := r.db.Where("id = ? AND tenant_id = ? AND version = ?", id, tenantID, version).Delete(&models.DWLayer{})
	if result.Error != nil {
		return commonrepo.WrapDBError(result.Error)
	}
	if result.RowsAffected == 0 {
		return commonrepo.WrapDBError(gorm.ErrRecordNotFound)
	}
	return nil
}

func (r *DWLayerRepository) DB() *gorm.DB { return r.db }

func (r *DWLayerRepository) ExistsByCode(code string, tenantID int64, excludeID int64) (bool, error) {
	var count int64
	query := r.db.Model(&models.DWLayer{}).Where("layer_code = ? AND tenant_id = ?", code, tenantID)
	if excludeID > 0 {
		query = query.Where("id != ?", excludeID)
	}
	err := query.Count(&count).Error
	return count > 0, commonrepo.WrapDBError(err)
}

func (r *DWLayerRepository) CountLogicalTableReferences(layerCode string, tenantID int64) (int64, error) {
	var count int64
	err := r.db.Model(&models.LogicalTable{}).Where("tenant_id = ? AND layer = ?", tenantID, layerCode).Count(&count).Error
	return count, commonrepo.WrapDBError(err)
}

package repository

import (
	commonrepo "github.com/addp/common/repository"
	"github.com/addp/service/internal/models"
	"gorm.io/gorm"
)

// ============================================================================
// TileServiceRepository 瓦片服务仓储层
// ============================================================================

type TileServiceRepository struct {
	db *gorm.DB
}

func NewTileServiceRepository(db *gorm.DB) *TileServiceRepository {
	return &TileServiceRepository{db: db}
}

// ============================================================================
// 瓦片服务 CRUD
// ============================================================================

// CreateService 创建瓦片服务
func (r *TileServiceRepository) CreateService(service *models.TileService) error {
	return r.db.Create(service).Error
}

// GetServiceByID 根据 ID 获取瓦片服务
func (r *TileServiceRepository) GetServiceByID(id uint) (*models.TileService, error) {
	var service models.TileService
	err := r.db.Preload("Layers").Where("id = ?", id).First(&service).Error
	if err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	return &service, nil
}

// GetServiceByName 根据服务名称获取服务（不过滤租户，用于公开服务）
func (r *TileServiceRepository) GetServiceByName(serviceName string) (*models.TileService, error) {
	var service models.TileService
	err := r.db.Preload("Layers").Where("service_name = ?", serviceName).First(&service).Error
	if err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	return &service, nil
}

// GetServiceByNameAndTenant 根据服务名称和租户 ID 获取服务
func (r *TileServiceRepository) GetServiceByNameAndTenant(serviceName string, tenantID uint) (*models.TileService, error) {
	var service models.TileService
	err := r.db.Preload("Layers").
		Where("service_name = ? AND tenant_id = ?", serviceName, tenantID).
		First(&service).Error
	if err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	return &service, nil
}

// ListServices 列出租户下的所有瓦片服务
func (r *TileServiceRepository) ListServices(tenantID uint, offset int, limit int) ([]models.TileService, int64, error) {
	var services []models.TileService
	var total int64

	query := r.db.Where("tenant_id = ?", tenantID)
	if err := query.Model(&models.TileService{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Preload("Layers").Offset(offset).Limit(limit).Order("created_at DESC").Find(&services).Error; err != nil {
		return nil, 0, err
	}

	return services, total, nil
}

// UpdateService 更新瓦片服务
func (r *TileServiceRepository) UpdateService(id uint, updates map[string]interface{}) error {
	return r.db.Model(&models.TileService{}).Where("id = ?", id).Updates(updates).Error
}

// DeleteService 删除瓦片服务（会级联删除图层）
func (r *TileServiceRepository) DeleteService(id uint) error {
	return r.db.Delete(&models.TileService{}, id).Error
}

// CheckServiceNameUnique 检查服务名称是否唯一
func (r *TileServiceRepository) CheckServiceNameUnique(serviceName string, tenantID uint, excludeID *uint) (bool, error) {
	var count int64
	query := r.db.Model(&models.TileService{}).Where("service_name = ? AND tenant_id = ?", serviceName, tenantID)
	if excludeID != nil {
		query = query.Where("id != ?", *excludeID)
	}
	err := query.Count(&count).Error
	if err != nil {
		return false, err
	}
	return count == 0, nil
}

// SearchServices 搜索瓦片服务
func (r *TileServiceRepository) SearchServices(tenantID uint, keyword string, offset int, limit int) ([]models.TileService, int64, error) {
	var services []models.TileService
	var total int64

	query := r.db.Where("tenant_id = ? AND (title ILIKE ? OR service_name ILIKE ?)", tenantID, "%"+keyword+"%", "%"+keyword+"%")
	if err := query.Model(&models.TileService{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Preload("Layers").Offset(offset).Limit(limit).Order("created_at DESC").Find(&services).Error; err != nil {
		return nil, 0, err
	}

	return services, total, nil
}

// UpdateServiceStatus 更新服务状态
func (r *TileServiceRepository) UpdateServiceStatus(id uint, status string, errorMessage string) error {
	updates := map[string]interface{}{
		"status": status,
	}
	if errorMessage != "" {
		updates["error_message"] = errorMessage
	}
	return r.db.Model(&models.TileService{}).Where("id = ?", id).Updates(updates).Error
}

// ============================================================================
// 瓦片服务图层 CRUD
// ============================================================================

// CreateLayer 创建图层
func (r *TileServiceRepository) CreateLayer(layer *models.TileServiceLayer) error {
	return r.db.Create(layer).Error
}

// GetLayerByID 根据 ID 获取图层
func (r *TileServiceRepository) GetLayerByID(id uint) (*models.TileServiceLayer, error) {
	var layer models.TileServiceLayer
	err := r.db.Where("id = ?", id).First(&layer).Error
	if err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	return &layer, nil
}

// GetLayerByServiceAndName 根据服务 ID 和图层名称获取图层
func (r *TileServiceRepository) GetLayerByServiceAndName(serviceID uint, layerName string) (*models.TileServiceLayer, error) {
	var layer models.TileServiceLayer
	err := r.db.Where("service_id = ? AND layer_name = ?", serviceID, layerName).First(&layer).Error
	if err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	return &layer, nil
}

// ListLayers 列出服务下的所有图层
func (r *TileServiceRepository) ListLayers(serviceID uint) ([]models.TileServiceLayer, error) {
	var layers []models.TileServiceLayer
	err := r.db.Where("service_id = ?", serviceID).Order("display_order ASC, created_at ASC").Find(&layers).Error
	if err != nil {
		return nil, err
	}
	return layers, nil
}

// UpdateLayer 更新图层
func (r *TileServiceRepository) UpdateLayer(id uint, updates map[string]interface{}) error {
	return r.db.Model(&models.TileServiceLayer{}).Where("id = ?", id).Updates(updates).Error
}

// DeleteLayer 删除图层
func (r *TileServiceRepository) DeleteLayer(id uint) error {
	return r.db.Delete(&models.TileServiceLayer{}, id).Error
}

// CheckLayerNameUnique 检查图层名称是否唯一（同一服务下）
func (r *TileServiceRepository) CheckLayerNameUnique(serviceID uint, layerName string, excludeID *uint) (bool, error) {
	var count int64
	query := r.db.Model(&models.TileServiceLayer{}).Where("service_id = ? AND layer_name = ?", serviceID, layerName)
	if excludeID != nil {
		query = query.Where("id != ?", *excludeID)
	}
	err := query.Count(&count).Error
	if err != nil {
		return false, err
	}
	return count == 0, nil
}

// CountLayersByService 统计服务下的图层数量
func (r *TileServiceRepository) CountLayersByService(serviceID uint) (int64, error) {
	var count int64
	err := r.db.Model(&models.TileServiceLayer{}).Where("service_id = ?", serviceID).Count(&count).Error
	return count, err
}

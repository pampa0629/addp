package repository

import (
	"github.com/addp/service/internal/models"
	"gorm.io/gorm"
)

type InternalServiceRepository struct {
	db *gorm.DB
}

func NewInternalServiceRepository(db *gorm.DB) *InternalServiceRepository {
	return &InternalServiceRepository{db}
}

// Create 创建内部服务
func (r *InternalServiceRepository) Create(service *models.InternalService) error {
	return r.db.Create(service).Error
}

// GetByID 根据 ID 获取服务
func (r *InternalServiceRepository) GetByID(id uint) (*models.InternalService, error) {
	var service models.InternalService
	err := r.db.Preload("Layers").Where("id = ?", id).First(&service).Error
	if err != nil {
		return nil, err
	}
	return &service, nil
}

// GetByName 根据服务名称获取服务（用于 OGC 端点查询）
func (r *InternalServiceRepository) GetByName(serviceName string) (*models.InternalService, error) {
	var service models.InternalService
	err := r.db.Preload("Layers").Where("service_name = ?", serviceName).First(&service).Error
	if err != nil {
		return nil, err
	}
	return &service, nil
}

// GetByNameAndTenant 根据服务名称和租户 ID 获取服务
func (r *InternalServiceRepository) GetByNameAndTenant(serviceName string, tenantID uint) (*models.InternalService, error) {
	var service models.InternalService
	err := r.db.Preload("Layers").
		Where("service_name = ? AND tenant_id = ?", serviceName, tenantID).
		First(&service).Error
	if err != nil {
		return nil, err
	}
	return &service, nil
}

// List 列出租户下的所有服务
func (r *InternalServiceRepository) List(tenantID uint, offset int, limit int) ([]models.InternalService, int64, error) {
	var services []models.InternalService
	var total int64

	query := r.db.Where("tenant_id = ?", tenantID)
	if err := query.Model(&models.InternalService{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Offset(offset).Limit(limit).Find(&services).Error; err != nil {
		return nil, 0, err
	}

	return services, total, nil
}

// Update 更新服务
func (r *InternalServiceRepository) Update(id uint, updates map[string]interface{}) error {
	return r.db.Model(&models.InternalService{}).Where("id = ?", id).Updates(updates).Error
}

// Delete 删除服务（软删除）
func (r *InternalServiceRepository) Delete(id uint) error {
	return r.db.Delete(&models.InternalService{}, id).Error
}

// GetLayerByID 根据 ID 获取图层
func (r *InternalServiceRepository) GetLayerByID(layerID uint) (*models.InternalServiceLayer, error) {
	var layer models.InternalServiceLayer
	err := r.db.Where("id = ?", layerID).First(&layer).Error
	if err != nil {
		return nil, err
	}
	return &layer, nil
}

// GetLayerByName 根据服务 ID 和图层名称获取图层
func (r *InternalServiceRepository) GetLayerByName(serviceID uint, layerName string) (*models.InternalServiceLayer, error) {
	var layer models.InternalServiceLayer
	err := r.db.Where("service_id = ? AND layer_name = ?", serviceID, layerName).First(&layer).Error
	if err != nil {
		return nil, err
	}
	return &layer, nil
}

// ListLayers 列出服务下的所有图层
func (r *InternalServiceRepository) ListLayers(serviceID uint) ([]models.InternalServiceLayer, error) {
	var layers []models.InternalServiceLayer
	err := r.db.Where("service_id = ?", serviceID).Order("display_order").Find(&layers).Error
	if err != nil {
		return nil, err
	}
	return layers, nil
}

// AddLayer 添加图层
func (r *InternalServiceRepository) AddLayer(layer *models.InternalServiceLayer) error {
	return r.db.Create(layer).Error
}

// UpdateLayer 更新图层
func (r *InternalServiceRepository) UpdateLayer(layerID uint, updates map[string]interface{}) error {
	return r.db.Model(&models.InternalServiceLayer{}).Where("id = ?", layerID).Updates(updates).Error
}

// DeleteLayer 删除图层
func (r *InternalServiceRepository) DeleteLayer(layerID uint) error {
	return r.db.Delete(&models.InternalServiceLayer{}, layerID).Error
}

// CheckServiceNameUnique 检查服务名称是否唯一
func (r *InternalServiceRepository) CheckServiceNameUnique(serviceName string, tenantID uint, excludeID *uint) (bool, error) {
	var count int64
	query := r.db.Where("service_name = ? AND tenant_id = ?", serviceName, tenantID)
	if excludeID != nil {
		query = query.Where("id != ?", *excludeID)
	}
	err := query.Model(&models.InternalService{}).Count(&count).Error
	if err != nil {
		return false, err
	}
	return count == 0, nil
}

// GetServicesByEngine 获取特定引擎下的所有服务
func (r *InternalServiceRepository) GetServicesByEngine(engineID uint) ([]models.InternalService, error) {
	var services []models.InternalService
	err := r.db.Where("engine_id = ?", engineID).Find(&services).Error
	if err != nil {
		return nil, err
	}
	return services, nil
}

// Search 搜索服务（按标题或服务名称）
func (r *InternalServiceRepository) Search(tenantID uint, keyword string, offset int, limit int) ([]models.InternalService, int64, error) {
	var services []models.InternalService
	var total int64

	query := r.db.Where("tenant_id = ? AND (title ILIKE ? OR service_name ILIKE ?)", tenantID, "%"+keyword+"%", "%"+keyword+"%")
	if err := query.Model(&models.InternalService{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Offset(offset).Limit(limit).Find(&services).Error; err != nil {
		return nil, 0, err
	}

	return services, total, nil
}

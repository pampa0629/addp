package repository

import (
	commonrepo "github.com/addp/common/repository"
	"github.com/addp/service/internal/models"
	"gorm.io/gorm"
)

type RegisteredServiceRepository struct {
	db *gorm.DB
}

func NewRegisteredServiceRepository(db *gorm.DB) *RegisteredServiceRepository {
	return &RegisteredServiceRepository{db}
}

// ===== 注册服务操作 =====

// Create 创建注册服务
func (r *RegisteredServiceRepository) Create(service *models.RegisteredService) error {
	return r.db.Create(service).Error
}

// GetByID 根据 ID 获取服务（包含图层）
func (r *RegisteredServiceRepository) GetByID(id uint) (*models.RegisteredService, error) {
	var service models.RegisteredService
	err := r.db.Preload("Layers").Where("id = ?", id).First(&service).Error
	if err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	return &service, nil
}

// GetByName 根据服务名称获取服务
func (r *RegisteredServiceRepository) GetByName(serviceName string) (*models.RegisteredService, error) {
	var service models.RegisteredService
	err := r.db.Preload("Layers").Where("service_name = ?", serviceName).First(&service).Error
	if err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	return &service, nil
}

// GetByNameAndTenant 根据服务名称和租户 ID 获取服务
func (r *RegisteredServiceRepository) GetByNameAndTenant(serviceName string, tenantID uint) (*models.RegisteredService, error) {
	var service models.RegisteredService
	err := r.db.Preload("Layers").
		Where("service_name = ? AND tenant_id = ?", serviceName, tenantID).
		First(&service).Error
	if err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	return &service, nil
}

// List 列出租户下的所有注册服务
func (r *RegisteredServiceRepository) List(tenantID uint, offset int, limit int) ([]models.RegisteredService, int64, error) {
	var services []models.RegisteredService
	var total int64

	query := r.db.Where("tenant_id = ?", tenantID)
	if err := query.Model(&models.RegisteredService{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Preload("Layers").Offset(offset).Limit(limit).Order("created_at DESC").Find(&services).Error; err != nil {
		return nil, 0, err
	}

	return services, total, nil
}

// ListByServiceType 根据服务类型列出租户下的注册服务
func (r *RegisteredServiceRepository) ListByServiceType(tenantID uint, serviceType string, offset int, limit int) ([]models.RegisteredService, int64, error) {
	var services []models.RegisteredService
	var total int64

	query := r.db.Where("tenant_id = ? AND service_type = ?", tenantID, serviceType)
	if err := query.Model(&models.RegisteredService{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Preload("Layers").Offset(offset).Limit(limit).Order("created_at DESC").Find(&services).Error; err != nil {
		return nil, 0, err
	}

	return services, total, nil
}

// Update 更新服务
func (r *RegisteredServiceRepository) Update(id uint, updates map[string]interface{}) error {
	return r.db.Model(&models.RegisteredService{}).Where("id = ?", id).Updates(updates).Error
}

// Delete 删除服务（级联删除图层）
func (r *RegisteredServiceRepository) Delete(id uint) error {
	return r.db.Delete(&models.RegisteredService{}, id).Error
}

// GetByTenant 获取租户下满足条件的所有服务（不分页）
func (r *RegisteredServiceRepository) GetByTenant(tenantID uint, filters map[string]interface{}) ([]models.RegisteredService, error) {
	var services []models.RegisteredService

	query := r.db.Where("tenant_id = ?", tenantID)

	// 应用过滤条件
	for key, value := range filters {
		query = query.Where(key+" = ?", value)
	}

	if err := query.Preload("Layers").Order("created_at DESC").Find(&services).Error; err != nil {
		return nil, err
	}

	return services, nil
}

// CheckServiceNameUnique 检查服务名称是否唯一
func (r *RegisteredServiceRepository) CheckServiceNameUnique(serviceName string, tenantID uint, excludeID *uint) (bool, error) {
	var count int64
	query := r.db.Model(&models.RegisteredService{}).Where("service_name = ? AND tenant_id = ?", serviceName, tenantID)
	if excludeID != nil {
		query = query.Where("id != ?", *excludeID)
	}
	err := query.Count(&count).Error
	if err != nil {
		return false, err
	}
	return count == 0, nil
}

// Search 搜索服务（按标题或服务名称）
func (r *RegisteredServiceRepository) Search(tenantID uint, keyword string, offset int, limit int) ([]models.RegisteredService, int64, error) {
	var services []models.RegisteredService
	var total int64

	query := r.db.Where("tenant_id = ? AND (title ILIKE ? OR service_name ILIKE ?)", tenantID, "%"+keyword+"%", "%"+keyword+"%")
	if err := query.Model(&models.RegisteredService{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Preload("Layers").Offset(offset).Limit(limit).Order("created_at DESC").Find(&services).Error; err != nil {
		return nil, 0, err
	}

	return services, total, nil
}

// UpdateStatus 更新服务状态
func (r *RegisteredServiceRepository) UpdateStatus(id uint, status string, errorMessage string) error {
	updates := map[string]interface{}{
		"status": status,
	}
	if errorMessage != "" {
		updates["error_message"] = errorMessage
	}
	return r.db.Model(&models.RegisteredService{}).Where("id = ?", id).Updates(updates).Error
}

// UpdateMetadata 更新服务元数据
func (r *RegisteredServiceRepository) UpdateMetadata(id uint, metadata map[string]interface{}) error {
	return r.db.Model(&models.RegisteredService{}).Where("id = ?", id).Update("metadata", metadata).Error
}

// UpdateHealthCheck 更新健康检查时间
func (r *RegisteredServiceRepository) UpdateHealthCheck(id uint) error {
	return r.db.Model(&models.RegisteredService{}).Where("id = ?", id).Update("last_checked_at", gorm.Expr("NOW()")).Error
}

// GetActiveServices 获取所有活跃的服务
func (r *RegisteredServiceRepository) GetActiveServices(tenantID uint) ([]models.RegisteredService, error) {
	var services []models.RegisteredService
	err := r.db.Preload("Layers").
		Where("tenant_id = ? AND status = ?", tenantID, "active").
		Find(&services).Error
	if err != nil {
		return nil, err
	}
	return services, nil
}

// CountByTenant 统计租户下的注册服务数量
func (r *RegisteredServiceRepository) CountByTenant(tenantID uint) (int64, error) {
	var count int64
	err := r.db.Model(&models.RegisteredService{}).Where("tenant_id = ?", tenantID).Count(&count).Error
	return count, err
}

// CountByServiceType 统计特定类型的服务数量
func (r *RegisteredServiceRepository) CountByServiceType(tenantID uint, serviceType string) (int64, error) {
	var count int64
	err := r.db.Model(&models.RegisteredService{}).
		Where("tenant_id = ? AND service_type = ?", tenantID, serviceType).
		Count(&count).Error
	return count, err
}

// ===== 图层操作 =====

// CreateLayer 创建图层
func (r *RegisteredServiceRepository) CreateLayer(layer *models.RegisteredServiceLayer) error {
	return r.db.Create(layer).Error
}

// CreateLayersBatch 批量创建图层
func (r *RegisteredServiceRepository) CreateLayersBatch(layers []models.RegisteredServiceLayer) error {
	if len(layers) == 0 {
		return nil
	}
	return r.db.Create(&layers).Error
}

// GetLayerByID 根据 ID 获取图层
func (r *RegisteredServiceRepository) GetLayerByID(id uint) (*models.RegisteredServiceLayer, error) {
	var layer models.RegisteredServiceLayer
	err := r.db.Where("id = ?", id).First(&layer).Error
	if err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	return &layer, nil
}

// GetLayersByServiceID 获取服务的所有图层
func (r *RegisteredServiceRepository) GetLayersByServiceID(serviceID uint) ([]models.RegisteredServiceLayer, error) {
	var layers []models.RegisteredServiceLayer
	err := r.db.Where("service_id = ?", serviceID).Order("layer_name").Find(&layers).Error
	if err != nil {
		return nil, err
	}
	return layers, nil
}

// UpdateLayer 更新图层
func (r *RegisteredServiceRepository) UpdateLayer(id uint, updates map[string]interface{}) error {
	return r.db.Model(&models.RegisteredServiceLayer{}).Where("id = ?", id).Updates(updates).Error
}

// DeleteLayer 删除图层
func (r *RegisteredServiceRepository) DeleteLayer(id uint) error {
	return r.db.Delete(&models.RegisteredServiceLayer{}, id).Error
}

// DeleteLayersByServiceID 删除服务的所有图层
func (r *RegisteredServiceRepository) DeleteLayersByServiceID(serviceID uint) error {
	return r.db.Where("service_id = ?", serviceID).Delete(&models.RegisteredServiceLayer{}).Error
}

// EnableLayer 启用图层
func (r *RegisteredServiceRepository) EnableLayer(id uint) error {
	return r.db.Model(&models.RegisteredServiceLayer{}).Where("id = ?", id).Update("enabled", true).Error
}

// DisableLayer 禁用图层
func (r *RegisteredServiceRepository) DisableLayer(id uint) error {
	return r.db.Model(&models.RegisteredServiceLayer{}).Where("id = ?", id).Update("enabled", false).Error
}

// GetEnabledLayersByServiceID 获取服务的所有启用的图层
func (r *RegisteredServiceRepository) GetEnabledLayersByServiceID(serviceID uint) ([]models.RegisteredServiceLayer, error) {
	var layers []models.RegisteredServiceLayer
	err := r.db.Where("service_id = ? AND enabled = ?", serviceID, true).Order("layer_name").Find(&layers).Error
	if err != nil {
		return nil, err
	}
	return layers, nil
}

// CheckLayerNameUnique 检查图层名称是否唯一（在同一服务内）
func (r *RegisteredServiceRepository) CheckLayerNameUnique(serviceID uint, layerName string, excludeID *uint) (bool, error) {
	var count int64
	query := r.db.Model(&models.RegisteredServiceLayer{}).Where("service_id = ? AND layer_name = ?", serviceID, layerName)
	if excludeID != nil {
		query = query.Where("id != ?", *excludeID)
	}
	err := query.Count(&count).Error
	if err != nil {
		return false, err
	}
	return count == 0, nil
}

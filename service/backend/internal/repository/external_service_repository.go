package repository

import (
	"fmt"

	"github.com/addp/service/internal/models"
	"gorm.io/gorm"
)

type ExternalServiceRepository struct {
	db *gorm.DB
}

func NewExternalServiceRepository(db *gorm.DB) *ExternalServiceRepository {
	return &ExternalServiceRepository{db: db}
}

// Create 创建外部服务
func (r *ExternalServiceRepository) Create(service *models.ExternalService) error {
	return r.db.Create(service).Error
}

// GetByID 根据 ID 获取外部服务
func (r *ExternalServiceRepository) GetByID(id uint) (*models.ExternalService, error) {
	var service models.ExternalService
	err := r.db.Preload("Layers").First(&service, id).Error
	if err != nil {
		return nil, err
	}
	return &service, nil
}

// List 列出外部服务（支持分页和筛选）
func (r *ExternalServiceRepository) List(tenantID uint, serviceType string, page, pageSize int) ([]models.ExternalService, int64, error) {
	var services []models.ExternalService
	var total int64

	query := r.db.Model(&models.ExternalService{}).Where("tenant_id = ?", tenantID)

	if serviceType != "" {
		query = query.Where("service_type = ?", serviceType)
	}

	// 计算总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 分页查询
	offset := (page - 1) * pageSize
	if err := query.Offset(offset).Limit(pageSize).Preload("Layers").Find(&services).Error; err != nil {
		return nil, 0, err
	}

	return services, total, nil
}

// Update 更新外部服务
func (r *ExternalServiceRepository) Update(service *models.ExternalService) error {
	return r.db.Save(service).Error
}

// Delete 删除外部服务（软删除）
func (r *ExternalServiceRepository) Delete(id uint) error {
	return r.db.Delete(&models.ExternalService{}, id).Error
}

// GetByTenantAndID 根据租户 ID 和服务 ID 获取服务
func (r *ExternalServiceRepository) GetByTenantAndID(tenantID, serviceID uint) (*models.ExternalService, error) {
	var service models.ExternalService
	err := r.db.Where("tenant_id = ? AND id = ?", tenantID, serviceID).
		Preload("Layers").
		First(&service).Error
	if err != nil {
		return nil, err
	}
	return &service, nil
}

// UpdateMetadata 更新服务元数据
func (r *ExternalServiceRepository) UpdateMetadata(id uint, metadata models.JSONB) error {
	return r.db.Model(&models.ExternalService{}).
		Where("id = ?", id).
		Update("metadata", metadata).Error
}

// UpdateHealthCheck 更新健康检查状态
func (r *ExternalServiceRepository) UpdateHealthCheck(id uint, status string) error {
	return r.db.Model(&models.ExternalService{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":       status,
			"last_checked": gorm.Expr("NOW()"),
		}).Error
}

// GetLayersByServiceID 获取服务的所有图层
func (r *ExternalServiceRepository) GetLayersByServiceID(serviceID uint) ([]models.ExternalServiceLayer, error) {
	var layers []models.ExternalServiceLayer
	err := r.db.Where("service_id = ? AND enabled = ?", serviceID, true).
		Find(&layers).Error
	return layers, err
}

// CreateLayer 创建服务图层
func (r *ExternalServiceRepository) CreateLayer(layer *models.ExternalServiceLayer) error {
	return r.db.Create(layer).Error
}

// UpdateLayer 更新服务图层
func (r *ExternalServiceRepository) UpdateLayer(layer *models.ExternalServiceLayer) error {
	return r.db.Save(layer).Error
}

// DeleteLayer 删除服务图层
func (r *ExternalServiceRepository) DeleteLayer(layerID uint) error {
	return r.db.Delete(&models.ExternalServiceLayer{}, layerID).Error
}

// GetActiveServices 获取所有激活的服务
func (r *ExternalServiceRepository) GetActiveServices(tenantID uint) ([]models.ExternalService, error) {
	var services []models.ExternalService
	err := r.db.Where("tenant_id = ? AND status = ?", tenantID, "active").
		Preload("Layers", "enabled = ?", true).
		Find(&services).Error
	return services, err
}

// BulkCreateLayers 批量创建图层
func (r *ExternalServiceRepository) BulkCreateLayers(layers []models.ExternalServiceLayer) error {
	if len(layers) == 0 {
		return nil
	}
	return r.db.Create(&layers).Error
}

// DeleteLayersByServiceID 删除服务的所有图层
func (r *ExternalServiceRepository) DeleteLayersByServiceID(serviceID uint) error {
	return r.db.Where("service_id = ?", serviceID).Delete(&models.ExternalServiceLayer{}).Error
}

// SearchServices 搜索服务（按名称、描述）
func (r *ExternalServiceRepository) SearchServices(tenantID uint, keyword string, page, pageSize int) ([]models.ExternalService, int64, error) {
	var services []models.ExternalService
	var total int64

	query := r.db.Model(&models.ExternalService{}).Where("tenant_id = ?", tenantID)

	if keyword != "" {
		searchPattern := fmt.Sprintf("%%%s%%", keyword)
		query = query.Where("name ILIKE ? OR description ILIKE ?", searchPattern, searchPattern)
	}

	// 计算总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 分页查询
	offset := (page - 1) * pageSize
	if err := query.Offset(offset).Limit(pageSize).Preload("Layers").Find(&services).Error; err != nil {
		return nil, 0, err
	}

	return services, total, nil
}

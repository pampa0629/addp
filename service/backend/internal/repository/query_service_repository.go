package repository

import (
	"github.com/addp/service/internal/models"
	"gorm.io/gorm"
)

type QueryServiceRepository struct {
	db *gorm.DB
}

func NewQueryServiceRepository(db *gorm.DB) *QueryServiceRepository {
	return &QueryServiceRepository{db}
}

// Create 创建查询服务
func (r *QueryServiceRepository) Create(service *models.QueryService) error {
	return r.db.Create(service).Error
}

// GetByID 根据 ID 获取服务
func (r *QueryServiceRepository) GetByID(id uint) (*models.QueryService, error) {
	var service models.QueryService
	err := r.db.Where("id = ?", id).First(&service).Error
	if err != nil {
		return nil, err
	}
	return &service, nil
}

// GetByName 根据服务名称获取服务（用于 API 端点查询）
func (r *QueryServiceRepository) GetByName(serviceName string) (*models.QueryService, error) {
	var service models.QueryService
	err := r.db.Where("service_name = ?", serviceName).First(&service).Error
	if err != nil {
		return nil, err
	}
	return &service, nil
}

// GetByNameAndTenant 根据服务名称和租户 ID 获取服务
func (r *QueryServiceRepository) GetByNameAndTenant(serviceName string, tenantID uint) (*models.QueryService, error) {
	var service models.QueryService
	err := r.db.
		Where("service_name = ? AND tenant_id = ?", serviceName, tenantID).
		First(&service).Error
	if err != nil {
		return nil, err
	}
	return &service, nil
}

// List 列出租户下的所有查询服务
func (r *QueryServiceRepository) List(tenantID uint, offset int, limit int) ([]models.QueryService, int64, error) {
	var services []models.QueryService
	var total int64

	query := r.db.Where("tenant_id = ?", tenantID)
	if err := query.Model(&models.QueryService{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Offset(offset).Limit(limit).Order("created_at DESC").Find(&services).Error; err != nil {
		return nil, 0, err
	}

	return services, total, nil
}

// ListByConfigType 根据配置类型列出租户下的查询服务
func (r *QueryServiceRepository) ListByConfigType(tenantID uint, configType string, offset int, limit int) ([]models.QueryService, int64, error) {
	var services []models.QueryService
	var total int64

	query := r.db.Where("tenant_id = ? AND config_type = ?", tenantID, configType)
	if err := query.Model(&models.QueryService{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Offset(offset).Limit(limit).Order("created_at DESC").Find(&services).Error; err != nil {
		return nil, 0, err
	}

	return services, total, nil
}

// Update 更新服务
func (r *QueryServiceRepository) Update(id uint, updates map[string]interface{}) error {
	return r.db.Model(&models.QueryService{}).Where("id = ?", id).Updates(updates).Error
}

// Delete 删除服务（物理删除）
func (r *QueryServiceRepository) Delete(id uint) error {
	return r.db.Delete(&models.QueryService{}, id).Error
}

// CheckServiceNameUnique 检查服务名称是否唯一
func (r *QueryServiceRepository) CheckServiceNameUnique(serviceName string, tenantID uint, excludeID *uint) (bool, error) {
	var count int64
	query := r.db.Model(&models.QueryService{}).Where("service_name = ? AND tenant_id = ?", serviceName, tenantID)
	if excludeID != nil {
		query = query.Where("id != ?", *excludeID)
	}
	err := query.Count(&count).Error
	if err != nil {
		return false, err
	}
	return count == 0, nil
}

// GetServicesByEngine 获取特定引擎下的所有查询服务
func (r *QueryServiceRepository) GetServicesByEngine(engineID uint) ([]models.QueryService, error) {
	var services []models.QueryService
	err := r.db.Where("engine_id = ?", engineID).Find(&services).Error
	if err != nil {
		return nil, err
	}
	return services, nil
}

// Search 搜索服务（按标题或服务名称）
func (r *QueryServiceRepository) Search(tenantID uint, keyword string, offset int, limit int) ([]models.QueryService, int64, error) {
	var services []models.QueryService
	var total int64

	query := r.db.Where("tenant_id = ? AND (title ILIKE ? OR service_name ILIKE ?)", tenantID, "%"+keyword+"%", "%"+keyword+"%")
	if err := query.Model(&models.QueryService{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Offset(offset).Limit(limit).Order("created_at DESC").Find(&services).Error; err != nil {
		return nil, 0, err
	}

	return services, total, nil
}

// UpdateStatus 更新服务状态
func (r *QueryServiceRepository) UpdateStatus(id uint, status string, errorMessage string) error {
	updates := map[string]interface{}{
		"status": status,
	}
	if errorMessage != "" {
		updates["error_message"] = errorMessage
	}
	return r.db.Model(&models.QueryService{}).Where("id = ?", id).Updates(updates).Error
}

// GetPublicServices 获取所有公开访问的服务
func (r *QueryServiceRepository) GetPublicServices() ([]models.QueryService, error) {
	var services []models.QueryService
	err := r.db.Where("public_access = ? AND status = ?", true, "active").Find(&services).Error
	if err != nil {
		return nil, err
	}
	return services, nil
}

// CountByTenant 统计租户下的查询服务数量
func (r *QueryServiceRepository) CountByTenant(tenantID uint) (int64, error) {
	var count int64
	err := r.db.Model(&models.QueryService{}).Where("tenant_id = ?", tenantID).Count(&count).Error
	return count, err
}

// CountByEngine 统计引擎下的查询服务数量
func (r *QueryServiceRepository) CountByEngine(engineID uint) (int64, error) {
	var count int64
	err := r.db.Model(&models.QueryService{}).Where("engine_id = ?", engineID).Count(&count).Error
	return count, err
}

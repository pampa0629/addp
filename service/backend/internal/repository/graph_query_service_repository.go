package repository

import (
	commonrepo "github.com/addp/common/repository"
	"github.com/addp/service/internal/models"
	"gorm.io/gorm"
)

type GraphQueryServiceRepository struct {
	db *gorm.DB
}

func NewGraphQueryServiceRepository(db *gorm.DB) *GraphQueryServiceRepository {
	return &GraphQueryServiceRepository{db}
}

func (r *GraphQueryServiceRepository) Create(service *models.GraphQueryService) error {
	return r.db.Create(service).Error
}

func (r *GraphQueryServiceRepository) GetByID(id uint) (*models.GraphQueryService, error) {
	var service models.GraphQueryService
	err := r.db.Where("id = ?", id).First(&service).Error
	if err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	return &service, nil
}

// GetByName 根据服务名称获取（不过滤租户，用于数据访问端点）
func (r *GraphQueryServiceRepository) GetByName(serviceName string) (*models.GraphQueryService, error) {
	var service models.GraphQueryService
	err := r.db.Where("service_name = ?", serviceName).First(&service).Error
	if err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	return &service, nil
}

func (r *GraphQueryServiceRepository) GetByNameAndTenant(serviceName string, tenantID uint) (*models.GraphQueryService, error) {
	var service models.GraphQueryService
	err := r.db.Where("service_name = ? AND tenant_id = ?", serviceName, tenantID).First(&service).Error
	if err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	return &service, nil
}

func (r *GraphQueryServiceRepository) List(tenantID uint, offset, limit int) ([]models.GraphQueryService, int64, error) {
	var services []models.GraphQueryService
	var total int64

	query := r.db.Where("tenant_id = ?", tenantID)
	if err := query.Model(&models.GraphQueryService{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := query.Offset(offset).Limit(limit).Order("created_at DESC").Find(&services).Error; err != nil {
		return nil, 0, err
	}
	return services, total, nil
}

func (r *GraphQueryServiceRepository) Search(tenantID uint, keyword string, offset, limit int) ([]models.GraphQueryService, int64, error) {
	var services []models.GraphQueryService
	var total int64

	query := r.db.Where(
		"tenant_id = ? AND (title ILIKE ? OR service_name ILIKE ?)",
		tenantID, "%"+keyword+"%", "%"+keyword+"%",
	)
	if err := query.Model(&models.GraphQueryService{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := query.Offset(offset).Limit(limit).Order("created_at DESC").Find(&services).Error; err != nil {
		return nil, 0, err
	}
	return services, total, nil
}

func (r *GraphQueryServiceRepository) Update(id uint, updates map[string]interface{}) error {
	return r.db.Model(&models.GraphQueryService{}).Where("id = ?", id).Updates(updates).Error
}

func (r *GraphQueryServiceRepository) Delete(id uint) error {
	return r.db.Delete(&models.GraphQueryService{}, id).Error
}

func (r *GraphQueryServiceRepository) CheckServiceNameUnique(serviceName string, tenantID uint, excludeID *uint) (bool, error) {
	var count int64
	query := r.db.Model(&models.GraphQueryService{}).Where("service_name = ? AND tenant_id = ?", serviceName, tenantID)
	if excludeID != nil {
		query = query.Where("id != ?", *excludeID)
	}
	err := query.Count(&count).Error
	if err != nil {
		return false, err
	}
	return count == 0, nil
}

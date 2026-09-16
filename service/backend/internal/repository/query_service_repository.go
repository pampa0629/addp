package repository

import (
	"context"
	"strings"

	commonapi "github.com/addp/common/api"
	commonrepo "github.com/addp/common/repository"
	"github.com/addp/service/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type QueryServiceRepository struct {
	db *gorm.DB
}

// ListConsumerServices returns only Query Services that satisfy the current
// owner resource policy. Every row-level predicate is applied before count and
// pagination so list totals cannot disclose unavailable services.
func (r *QueryServiceRepository) ListConsumerServices(filter models.ConsumerServiceListFilter) ([]models.QueryService, int64, error) {
	var services []models.QueryService
	var total int64

	query := r.consumerServicesQuery(filter.TenantID)
	if search := strings.TrimSpace(filter.Search); search != "" {
		pattern := "%" + search + "%"
		query = query.Where("title ILIKE ? OR description ILIKE ?", pattern, pattern)
	}
	switch filter.OutputKind {
	case models.ConsumerOutputKindSpatialTabular:
		query = query.Where("COALESCE(data_config #>> '{source_snapshot,spatial,primary_geometry_column}', '') <> ''")
	case models.ConsumerOutputKindTabular:
		query = query.Where("COALESCE(data_config #>> '{source_snapshot,spatial,primary_geometry_column}', '') = ''")
	}
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := query.
		Order("title ASC").
		Order("id ASC").
		Offset(filter.Offset).
		Limit(filter.Limit).
		Find(&services).Error; err != nil {
		return nil, 0, err
	}
	return services, total, nil
}

// GetConsumerServiceByID applies the same owner resource policy used by the
// catalog list. Unavailable services are therefore indistinguishable from
// missing services at the consumer boundary.
func (r *QueryServiceRepository) GetConsumerServiceByID(tenantID, serviceID uint) (*models.QueryService, error) {
	var service models.QueryService
	if err := r.consumerServicesQuery(tenantID).Where("id = ?", serviceID).First(&service).Error; err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	return &service, nil
}

func (r *QueryServiceRepository) consumerServicesQuery(tenantID uint) *gorm.DB {
	return r.db.Model(&models.QueryService{}).
		Where("tenant_id = ?", tenantID).
		Where("status = ?", "active").
		Where("protocols @> ?::jsonb", `{"rest_api":{"enabled":true}}`)
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
		return nil, commonrepo.WrapDBError(err)
	}
	return &service, nil
}

// GetByName 根据服务名称获取服务（用于 API 端点查询）
func (r *QueryServiceRepository) GetByName(serviceName string) (*models.QueryService, error) {
	var service models.QueryService
	err := r.db.Where("service_name = ?", serviceName).First(&service).Error
	if err != nil {
		return nil, commonrepo.WrapDBError(err)
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
		return nil, commonrepo.WrapDBError(err)
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

// Delete compares the tenant-scoped version before any deletion or trigger side effect.
func (r *QueryServiceRepository) Delete(ctx context.Context, id, tenantID uint, version int64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var item models.QueryService
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", id, tenantID).First(&item).Error; err != nil {
			return commonrepo.WrapDBError(err)
		}
		if version <= 0 || item.Version != version {
			return commonapi.ErrConflict
		}
		return tx.Delete(&item).Error
	})
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

// ListLineagePublications is an owner-internal replay cursor, never a catalog API.
func (r *QueryServiceRepository) ListLineagePublications(ctx context.Context, afterID uint, limit int) ([]models.QueryService, error) {
	var rows []models.QueryService
	err := r.db.WithContext(ctx).Where("id > ?", afterID).Order("id").Limit(limit).Find(&rows).Error
	return rows, err
}

// UpdateVersioned is the single management mutation path for the QueryService aggregate.
func (r *QueryServiceRepository) UpdateVersioned(ctx context.Context, id, tenantID uint, version int64, change func(*models.QueryService) error) (*models.QueryService, error) {
	var item models.QueryService
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", id, tenantID).First(&item).Error; err != nil {
			return commonrepo.WrapDBError(err)
		}
		if version <= 0 || item.Version != version {
			return commonapi.ErrConflict
		}
		if err := change(&item); err != nil {
			return err
		}
		item.Version++
		return tx.Model(&item).Select("*").Updates(&item).Error
	})
	return &item, err
}

package service

import (
	"fmt"
	"os"
	"strings"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

// ResourceService 资源服务 - 直接读取 system.resources
type ResourceService struct {
	db             *gorm.DB
	systemURL      string
	internalClient *commonClient.SystemClient
}

func NewResourceService(db *gorm.DB, systemURL, internalKey string) *ResourceService {
	// 默认从环境变量读取，便于本地降级
	if systemURL == "" {
		systemURL = os.Getenv("SYSTEM_SERVICE_URL")
		if systemURL == "" {
			systemURL = "http://localhost:8080"
		}
	}
	if internalKey == "" {
		internalKey = os.Getenv("INTERNAL_API_KEY")
	}

	var internalClient *commonClient.SystemClient
	if internalKey != "" {
		internalClient = commonClient.NewSystemClientWithInternalKey(systemURL, internalKey)
	}

	return &ResourceService{
		db:             db,
		systemURL:      systemURL,
		internalClient: internalClient,
	}
}

// GetResourcesByTenant 获取租户的所有数据库类型资源
func (s *ResourceService) GetResourcesByTenant(tenantID uint) ([]*commonModels.Resource, error) {
	if s.internalClient != nil {
		systemResources, err := s.internalClient.ListResources("", tenantID)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch resources from system: %w", err)
		}

		var resources []*commonModels.Resource
		for i := range systemResources {
			res := systemResources[i]
			if !res.IsActive {
				continue
			}
			switch strings.ToLower(res.ResourceType) {
			case "postgresql", "postgres", "mysql", "object_storage", "object-storage", "s3", "minio", "oss":
				if tenantID > 0 && res.TenantID != tenantID {
					continue
				}
				resourceCopy := res
				resources = append(resources, &resourceCopy)
			}
		}
		return resources, nil
	}

	var resources []*commonModels.Resource

	// 直接从 system.resources 表读取
	err := s.db.Table("system.resources").
		Where("tenant_id = ? AND resource_type IN (?, ?, ?, ?, ?, ?, ?)", tenantID,
			"postgresql", "mysql", "s3", "minio", "oss", "object_storage", "object-storage").
		Where("is_active = ?", true).
		Find(&resources).Error

	if err != nil {
		return nil, fmt.Errorf("failed to fetch resources: %w", err)
	}

	return resources, nil
}

// GetResourceByID 根据ID获取资源（从System API获取，密码已解密）
// token: 用户的JWT token，用于认证System API调用
func (s *ResourceService) GetResourceByID(resourceID, tenantID uint, token string) (*commonModels.Resource, error) {
	if s.internalClient != nil {
		resource, err := s.internalClient.GetResource(resourceID)
		if err != nil {
			return nil, fmt.Errorf("failed to get resource from System API: %w", err)
		}
		if tenantID > 0 && resource.TenantID != tenantID {
			return nil, fmt.Errorf("resource not found or access denied")
		}
		return resource, nil
	}

	// 使用用户token创建SystemClient（无内部密钥时降级使用用户接口，敏感字段将被脱敏）
	systemClient := commonClient.NewSystemClient(s.systemURL, token)

	resource, err := systemClient.GetResource(resourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource from System API: %w", err)
	}

	if tenantID > 0 && resource.TenantID != tenantID {
		return nil, fmt.Errorf("resource not found or access denied")
	}

	return resource, nil
}

// GetResourcesWithStats 获取资源及其扫描统计
func (s *ResourceService) GetResourcesWithStats(tenantID uint) ([]*models.ResourceWithStats, error) {
	resources, err := s.GetResourcesByTenant(tenantID)
	if err != nil {
		return nil, err
	}

	var result []*models.ResourceWithStats
	for _, res := range resources {
		// 统计该资源下的 Schema 数量
		var totalSchemas, scannedSchemas int64

		s.db.Table("metadata.schemas").
			Where("resource_id = ? AND tenant_id = ?", res.ID, tenantID).
			Count(&totalSchemas)

		s.db.Table("metadata.schemas").
			Where("resource_id = ? AND tenant_id = ? AND scan_status = ?", res.ID, tenantID, "已扫描").
			Count(&scannedSchemas)

		// 获取最后扫描时间
		var lastScanAt string
		var schema models.MetadataSchema
		err := s.db.Table("metadata.schemas").
			Where("resource_id = ? AND tenant_id = ? AND last_scan_at IS NOT NULL", res.ID, tenantID).
			Order("last_scan_at DESC").
			First(&schema).Error

		if err == nil && schema.LastScanAt != nil {
			lastScanAt = schema.LastScanAt.Format("2006-01-02 15:04:05")
		}

		result = append(result, &models.ResourceWithStats{
			ResourceID:       res.ID,
			ResourceName:     res.Name, // 使用 Name 字段
			ResourceType:     res.ResourceType,
			TotalSchemas:     int(totalSchemas),
			ScannedSchemas:   int(scannedSchemas),
			UnscannedSchemas: int(totalSchemas - scannedSchemas),
			LastScanAt:       lastScanAt,
		})
	}

	return result, nil
}

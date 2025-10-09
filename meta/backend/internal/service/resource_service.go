package service

import (
	"fmt"
	"os"
	"strings"
	"time"

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

	if len(resources) == 0 {
		return []*models.ResourceWithStats{}, nil
	}

	resourceIDs := make([]uint, 0, len(resources))
	for _, res := range resources {
		resourceIDs = append(resourceIDs, res.ID)
	}

	var metaResources []models.MetaResource
	if err := s.db.Where("tenant_id = ? AND resource_id IN ?", tenantID, resourceIDs).
		Find(&metaResources).Error; err != nil && err != gorm.ErrRecordNotFound {
		return nil, fmt.Errorf("failed to load meta resources: %w", err)
	}

	metaResByResourceID := make(map[uint]*models.MetaResource, len(metaResources))
	metaResIDs := make([]uint, 0, len(metaResources))
	for i := range metaResources {
		mr := &metaResources[i]
		metaResByResourceID[mr.ResourceID] = mr
		metaResIDs = append(metaResIDs, mr.ID)
	}

	totalCount := map[uint]int64{}
	scannedCount := map[uint]int64{}
	lastScanByRes := map[uint]*time.Time{}

	if len(metaResIDs) > 0 {
		type countRow struct {
			ResID uint
			Count int64
		}

		var totals []countRow
		if err := s.db.Table("meta_node").
			Where("tenant_id = ? AND res_id IN ?", tenantID, metaResIDs).
			Where("parent_node_id IS NULL").
			Select("res_id, COUNT(*) AS count").
			Group("res_id").
			Scan(&totals).Error; err != nil {
			return nil, fmt.Errorf("failed to count meta nodes: %w", err)
		}
		for _, row := range totals {
			totalCount[row.ResID] = row.Count
		}

		var scanned []countRow
		if err := s.db.Table("meta_node").
			Where("tenant_id = ? AND res_id IN ?", tenantID, metaResIDs).
			Where("parent_node_id IS NULL AND scan_status = ?", "已扫描").
			Select("res_id, COUNT(*) AS count").
			Group("res_id").
			Scan(&scanned).Error; err != nil {
			return nil, fmt.Errorf("failed to count scanned nodes: %w", err)
		}
		for _, row := range scanned {
			scannedCount[row.ResID] = row.Count
		}

		type lastScanRow struct {
			ResID      uint
			LastScanAt *time.Time `gorm:"column:last_scan_at"`
		}
		var lastScans []lastScanRow
		if err := s.db.Table("meta_node").
			Where("tenant_id = ? AND res_id IN ?", tenantID, metaResIDs).
			Where("last_scan_at IS NOT NULL").
			Select("res_id, MAX(last_scan_at) AS last_scan_at").
			Group("res_id").
			Scan(&lastScans).Error; err != nil {
			return nil, fmt.Errorf("failed to query node last scan time: %w", err)
		}
		for _, row := range lastScans {
			lastScanByRes[row.ResID] = row.LastScanAt
		}
	}

	result := make([]*models.ResourceWithStats, 0, len(resources))
	for _, res := range resources {
		totalSchemas := 0
		scannedSchemas := 0
		lastScanAt := ""

		if metaRes, ok := metaResByResourceID[res.ID]; ok {
			if cnt, ok := totalCount[metaRes.ID]; ok {
				totalSchemas = int(cnt)
			}
			if cnt, ok := scannedCount[metaRes.ID]; ok {
				scannedSchemas = int(cnt)
			}
			if ts, ok := lastScanByRes[metaRes.ID]; ok && ts != nil {
				lastScanAt = ts.Format("2006-01-02 15:04:05")
			}
		}

		result = append(result, &models.ResourceWithStats{
			ResourceID:       res.ID,
			ResourceName:     res.Name,
			ResourceType:     res.ResourceType,
			TotalSchemas:     totalSchemas,
			ScannedSchemas:   scannedSchemas,
			UnscannedSchemas: totalSchemas - scannedSchemas,
			LastScanAt:       lastScanAt,
		})
	}

	return result, nil
}

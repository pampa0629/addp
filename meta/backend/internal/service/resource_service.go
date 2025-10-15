package service

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

// ResourceService 资源服务 - 直接读取 system.resources
type ResourceService struct {
	db             *gorm.DB
	systemURL      string
	internalClient *commonClient.SystemClient
	cacheMu        sync.RWMutex
	resourceCache  map[uint]*commonModels.Resource
	log            *slog.Logger
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

	service := &ResourceService{
		db:            db,
		systemURL:     systemURL,
		resourceCache: make(map[uint]*commonModels.Resource),
		log:           logger.With("component", "resource_service"),
	}

	if internalKey != "" {
		service.internalClient = commonClient.NewSystemClientWithInternalKey(systemURL, internalKey)
	}

	return service
}

// ensureInternalClient 尝试按需初始化内部客户端（用于本地脚本未显式传入密钥的情况）
func (s *ResourceService) ensureInternalClient() {
	if s.internalClient != nil {
		return
	}

	if key := os.Getenv("INTERNAL_API_KEY"); key != "" {
		s.internalClient = commonClient.NewSystemClientWithInternalKey(s.systemURL, key)
	}
}

// PreloadResources 在服务启动时从 System 服务加载资源连接信息并缓存在内存中
func (s *ResourceService) PreloadResources() error {
	s.ensureInternalClient()
	if s.internalClient == nil {
		return fmt.Errorf("internal client not configured")
	}

	resources, err := s.internalClient.ListResources("", 0)
	if err != nil {
		return fmt.Errorf("failed to preload resources from System: %w", err)
	}

	cache := make(map[uint]*commonModels.Resource, len(resources))
	for i := range resources {
		res := resources[i]
		if !res.IsActive {
			continue
		}
		resourceCopy := res
		cache[resourceCopy.ID] = &resourceCopy
	}

	s.cacheMu.Lock()
	s.resourceCache = cache
	s.cacheMu.Unlock()

	s.log.Info("资源缓存预加载完成", "active_resources", len(cache), "system_url", s.systemURL)
	return nil
}

func containsMaskedSensitive(info commonModels.ConnectionInfo) bool {
	if info == nil {
		return false
	}

	for k, v := range info {
		lowerKey := strings.ToLower(k)
		if !(strings.Contains(lowerKey, "password") ||
			strings.Contains(lowerKey, "secret") ||
			strings.Contains(lowerKey, "token") ||
			strings.Contains(lowerKey, "key")) {
			continue
		}

		strVal, ok := v.(string)
		if !ok {
			continue
		}
		if strVal == "" {
			continue
		}
		// 判断是否为掩码占位（由系统服务返回的 ****** 或其他星号组合）
		if strVal == "******" || strVal == "****" || strings.Contains(strVal, "*") {
			return true
		}
	}

	return false
}

func (s *ResourceService) cacheResource(resource *commonModels.Resource) {
	if resource == nil {
		return
	}
	resourceCopy := *resource
	s.cacheMu.Lock()
	s.resourceCache[resourceCopy.ID] = &resourceCopy
	s.cacheMu.Unlock()
}

func (s *ResourceService) snapshotCache() map[uint]*commonModels.Resource {
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()

	if len(s.resourceCache) == 0 {
		return nil
	}

	result := make(map[uint]*commonModels.Resource, len(s.resourceCache))
	for id, res := range s.resourceCache {
		if res == nil {
			continue
		}
		resourceCopy := *res
		result[id] = &resourceCopy
	}
	return result
}

// GetResourcesByTenant 获取租户的所有数据库类型资源
func (s *ResourceService) GetResourcesByTenant(tenantID uint) ([]*commonModels.Resource, error) {
	s.ensureInternalClient()

	if s.internalClient != nil {
		systemResources, err := s.internalClient.ListResources("", tenantID)
		if err == nil {
			s.log.Info("从 System API 获取资源列表成功",
				"tenant_id", tenantID,
				"resource_total", len(systemResources),
			)
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

		s.log.Warn("从 System API 获取资源失败，回退至本地缓存", "error", err, "tenant_id", tenantID)
	}

	// fallback to cache snapshot
	cached := s.snapshotCache()
	if len(cached) == 0 && s.internalClient != nil {
		if err := s.PreloadResources(); err == nil {
			cached = s.snapshotCache()
		}
	}

	if len(cached) == 0 {
		return nil, fmt.Errorf("no resource cache available; ensure System 服务可用并配置 INTERNAL_API_KEY")
	}

	var resources []*commonModels.Resource
	for _, resource := range cached {
		if resource == nil || !resource.IsActive {
			continue
		}
		if tenantID > 0 && resource.TenantID != tenantID {
			continue
		}
		switch strings.ToLower(resource.ResourceType) {
		case "postgresql", "postgres", "mysql", "object_storage", "object-storage", "s3", "minio", "oss":
			resourceCopy := *resource
			resources = append(resources, &resourceCopy)
		}
	}

	return resources, nil
}

// GetResourceByID 根据ID获取资源（从System API获取，密码已解密）
// token: 用户的JWT token，用于认证System API调用
func (s *ResourceService) GetResourceByID(resourceID, tenantID uint, token string) (*commonModels.Resource, error) {
	s.ensureInternalClient()

	s.cacheMu.RLock()
	cached, ok := s.resourceCache[resourceID]
	s.cacheMu.RUnlock()
	if ok && cached != nil && (tenantID == 0 || cached.TenantID == tenantID) {
		resourceCopy := *cached
		fields := append(connectionLogFields(&resourceCopy),
			"requested_tenant_id", tenantID,
			"source", "cache",
		)
		s.log.Info("资源连接信息命中缓存", fields...)
		return &resourceCopy, nil
	}

	if s.internalClient != nil {
		resource, err := s.internalClient.GetResource(resourceID)
		if err != nil {
			return nil, fmt.Errorf("failed to get resource from System API: %w", err)
		}
		if tenantID > 0 && resource.TenantID != tenantID {
			return nil, fmt.Errorf("resource not found or access denied")
		}
		s.cacheResource(resource)
		fields := append(connectionLogFields(resource),
			"requested_tenant_id", tenantID,
			"source", "internal_api",
		)
		s.log.Info("通过内部 API 获取资源连接信息成功", fields...)
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

	// 用户接口返回的数据会对敏感字段做掩码，导致后续建链失败。
	// 如果检测到敏感字段被掩码，尝试使用内部客户端重新获取一次。
	s.ensureInternalClient()
	if s.internalClient != nil && containsMaskedSensitive(resource.ConnectionInfo) {
		if internalRes, err := s.internalClient.GetResource(resourceID); err == nil {
			resource = internalRes
			s.log.Info("检测到敏感字段被掩码，使用内部 API 重新获取资源连接信息",
				append(connectionLogFields(resource),
					"requested_tenant_id", tenantID,
					"source", "internal_api_retry",
				)...,
			)
		}
	}

	s.cacheResource(resource)
	fields := append(connectionLogFields(resource),
		"requested_tenant_id", tenantID,
		"source", "user_api",
	)
	s.log.Info("通过用户 API 获取资源连接信息成功", fields...)
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

	totalCount := make(map[uint]int64)
	scannedCount := make(map[uint]int64)
	lastScanByRes := make(map[uint]*time.Time)

	type countRow struct {
		ResID uint
		Count int64
	}
	var totals []countRow
	if err := s.db.Table("meta_node").
		Where("tenant_id = ? AND res_id IN ?", tenantID, resourceIDs).
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
		Where("tenant_id = ? AND res_id IN ?", tenantID, resourceIDs).
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
		Where("tenant_id = ? AND res_id IN ?", tenantID, resourceIDs).
		Where("last_scan_at IS NOT NULL").
		Select("res_id, MAX(last_scan_at) AS last_scan_at").
		Group("res_id").
		Scan(&lastScans).Error; err != nil {
		return nil, fmt.Errorf("failed to query node last scan time: %w", err)
	}
	for _, row := range lastScans {
		lastScanByRes[row.ResID] = row.LastScanAt
	}

	result := make([]*models.ResourceWithStats, 0, len(resources))
	for _, res := range resources {
		totalSchemas := 0
		scannedSchemas := 0
		lastScanAt := ""

		if cnt, ok := totalCount[res.ID]; ok {
			totalSchemas = int(cnt)
		}
		if cnt, ok := scannedCount[res.ID]; ok {
			scannedSchemas = int(cnt)
		}
		if ts, ok := lastScanByRes[res.ID]; ok && ts != nil {
			lastScanAt = ts.Format("2006-01-02 15:04:05")
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

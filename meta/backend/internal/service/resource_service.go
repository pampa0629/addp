package service

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/events"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// resourceCacheEntry 缓存条目，包含资源和过期时间
type resourceCacheEntry struct {
	resource  *commonModels.Resource
	expiresAt time.Time
}

// ResourceService 资源服务 - 直接读取 system.resources
type ResourceService struct {
	db              *gorm.DB
	systemURL       string
	internalClient  *commonClient.SystemClient
	cacheMu         sync.RWMutex
	resourceCache   map[uint]*resourceCacheEntry // 改为存储带过期时间的条目
	cacheTTL        time.Duration                // 缓存生存时间，默认 5 分钟
	log             *slog.Logger
	eventSubscriber *events.ResourceEventSubscriber // Redis 事件订阅器
}

func NewResourceService(db *gorm.DB, systemURL, internalKey string, redisClient *redis.Client) *ResourceService {
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
		resourceCache: make(map[uint]*resourceCacheEntry),
		cacheTTL:      5 * time.Minute, // 默认 5 分钟 TTL
		log:           logger.With("component", "resource_service"),
	}

	if internalKey != "" {
		service.internalClient = commonClient.NewSystemClientWithInternalKey(systemURL, internalKey)
	}

	// 初始化 Redis 事件订阅器
	if redisClient != nil {
		service.eventSubscriber = events.NewResourceEventSubscriber(
			redisClient,
			service.handleResourceChangeEvent,
			service.log,
		)
		// 启动订阅（在后台 goroutine 中）
		go func() {
			if err := service.eventSubscriber.Start(); err != nil {
				service.log.Error("资源事件订阅器启动失败", "error", err)
			}
		}()
		service.log.Info("资源事件订阅器已启动")
	} else {
		service.log.Warn("Redis 未配置，资源变更事件同步功能将被禁用")
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

	cache := make(map[uint]*resourceCacheEntry, len(resources))
	expiresAt := time.Now().Add(s.cacheTTL)
	for i := range resources {
		res := resources[i]
		if !res.IsActive {
			continue
		}
		resourceCopy := res
		cache[resourceCopy.ID] = &resourceCacheEntry{
			resource:  &resourceCopy,
			expiresAt: expiresAt,
		}
	}

	s.cacheMu.Lock()
	s.resourceCache = cache
	s.cacheMu.Unlock()

	s.log.Info("资源缓存预加载完成", "active_resources", len(cache), "system_url", s.systemURL, "ttl_minutes", s.cacheTTL.Minutes())
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
	s.resourceCache[resourceCopy.ID] = &resourceCacheEntry{
		resource:  &resourceCopy,
		expiresAt: time.Now().Add(s.cacheTTL),
	}
	s.cacheMu.Unlock()
}

// ClearCache 清除所有资源缓存
func (s *ResourceService) ClearCache() {
	s.cacheMu.Lock()
	s.resourceCache = make(map[uint]*resourceCacheEntry)
	s.cacheMu.Unlock()
	s.log.Info("资源缓存已清除")
}

// ClearResourceCache 清除指定资源的缓存
func (s *ResourceService) ClearResourceCache(resourceID uint) {
	s.cacheMu.Lock()
	delete(s.resourceCache, resourceID)
	s.cacheMu.Unlock()
	s.log.Info("资源缓存已清除", "resource_id", resourceID)
}

func (s *ResourceService) snapshotCache() map[uint]*commonModels.Resource {
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()

	if len(s.resourceCache) == 0 {
		return nil
	}

	now := time.Now()
	result := make(map[uint]*commonModels.Resource, len(s.resourceCache))
	for id, entry := range s.resourceCache {
		if entry == nil || entry.resource == nil {
			continue
		}
		// 跳过已过期的缓存项
		if now.After(entry.expiresAt) {
			continue
		}
		resourceCopy := *entry.resource
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
		s.log.Info("当前没有可用的资源", "tenant_id", tenantID)
		return []*commonModels.Resource{}, nil // 返回空列表而不是错误
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

	// 检查缓存
	s.cacheMu.RLock()
	entry, ok := s.resourceCache[resourceID]
	s.cacheMu.RUnlock()

	// 如果缓存命中且未过期，返回缓存数据
	if ok && entry != nil && entry.resource != nil && time.Now().Before(entry.expiresAt) {
		if tenantID == 0 || entry.resource.TenantID == tenantID {
			resourceCopy := *entry.resource
			fields := append(connectionLogFields(&resourceCopy),
				"requested_tenant_id", tenantID,
				"source", "cache",
				"expires_in_seconds", int(time.Until(entry.expiresAt).Seconds()),
			)
			s.log.Info("资源连接信息命中缓存", fields...)
			return &resourceCopy, nil
		}
	}

	// 缓存未命中或已过期，从 System API 获取

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
	if err := s.db.Table("metadata.meta_node").
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
	if err := s.db.Table("metadata.meta_node").
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
	if err := s.db.Table("metadata.meta_node").
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

// handleResourceChangeEvent 处理资源变更事件（Redis 订阅回调）
func (s *ResourceService) handleResourceChangeEvent(event events.ResourceChangeEvent) error {
	s.log.Info("收到资源变更事件",
		"resource_id", event.ResourceID,
		"action", event.Action,
		"timestamp", event.Timestamp)

	switch event.Action {
	case events.ActionCreate:
		// 资源创建：不需要特殊处理，等待下次访问时自动加载
		s.log.Debug("资源已创建，等待首次访问时加载", "resource_id", event.ResourceID)

	case events.ActionUpdate:
		// 资源更新：清除缓存，强制下次访问时重新获取
		s.ClearResourceCache(event.ResourceID)
		s.log.Info("资源已更新，缓存已清除", "resource_id", event.ResourceID)

	case events.ActionDelete:
		// 资源删除：清除缓存
		s.ClearResourceCache(event.ResourceID)
		s.log.Info("资源已删除，缓存已清除", "resource_id", event.ResourceID)
		// TODO: 可以考虑删除相关的元数据（meta_node, meta_item）

	default:
		s.log.Warn("未知的资源变更动作", "action", event.Action, "resource_id", event.ResourceID)
	}

	return nil
}

// Stop 停止资源服务（清理资源）
func (s *ResourceService) Stop() {
	if s.eventSubscriber != nil {
		s.eventSubscriber.Stop()
		s.log.Info("资源事件订阅器已停止")
	}
}

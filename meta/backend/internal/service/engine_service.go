package service

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/utils"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanflow"
	"gorm.io/gorm"
)

// engineCacheEntry 缓存条目，包含资源和过期时间
type engineCacheEntry struct {
	resource  *commonModels.Engine
	expiresAt time.Time
}

// EngineService 负责从 System 获取并缓存 engine 连接信息。
type EngineService struct {
	db             *gorm.DB
	systemURL      string
	internalClient *commonClient.SystemClient
	cacheMu        sync.RWMutex
	engineCache    map[uint]*engineCacheEntry
	cacheTTL       time.Duration
	log            *slog.Logger
}

func NewEngineService(db *gorm.DB, systemURL, internalKey string) *EngineService {
	// 默认从环境变量读取，便于本地降级
	if systemURL == "" {
		systemURL = os.Getenv("SYSTEM_URL")
		if systemURL == "" {
			systemURL = "http://localhost:8180"
		}
	}
	if internalKey == "" {
		internalKey = os.Getenv("INTERNAL_API_KEY")
	}

	service := &EngineService{
		db:          db,
		systemURL:   systemURL,
		engineCache: make(map[uint]*engineCacheEntry),
		cacheTTL:    5 * time.Minute, // 默认 5 分钟 TTL
		log:         logger.With("component", "engine_service"),
	}

	if internalKey != "" {
		service.internalClient = commonClient.NewSystemClientWithInternalKey(systemURL, internalKey)
	}

	return service
}

// ensureInternalClient 尝试按需初始化内部客户端（用于本地脚本未显式传入密钥的情况）
func (s *EngineService) ensureInternalClient() {
	if s.internalClient != nil {
		return
	}

	if key := os.Getenv("INTERNAL_API_KEY"); key != "" {
		s.internalClient = commonClient.NewSystemClientWithInternalKey(s.systemURL, key)
	}
}

// PreloadResources 在服务启动时从 System 加载 engine 连接信息并缓存在内存中。
func (s *EngineService) PreloadResources() error {
	s.ensureInternalClient()
	if s.internalClient == nil {
		return fmt.Errorf("internal client not configured")
	}

	engines, err := s.internalClient.ListEngines("", 0)
	if err != nil {
		return fmt.Errorf("failed to preload engines from System: %w", err)
	}

	cache := make(map[uint]*engineCacheEntry, len(engines))
	expiresAt := time.Now().Add(s.cacheTTL)
	rootReconciled := 0
	for i := range engines {
		res := engines[i]
		if !res.IsActive {
			continue
		}
		engineCopy := res
		if s.reconcileCatalogRoot(&engineCopy) {
			rootReconciled++
		}
		cache[engineCopy.ID] = &engineCacheEntry{
			resource:  &engineCopy,
			expiresAt: expiresAt,
		}
	}

	s.cacheMu.Lock()
	s.engineCache = cache
	s.cacheMu.Unlock()

	s.log.Info("引擎缓存预加载完成", "active_engines", len(cache), "root_reconciled", rootReconciled, "system_url", s.systemURL, "ttl_minutes", s.cacheTTL.Minutes())
	return nil
}

func (s *EngineService) reconcileCatalogRoot(resource *commonModels.Engine) bool {
	if resource == nil || !resource.IsActive || resource.TenantID == nil {
		return false
	}
	enginePlugin, err := plugin.Get(resource.EngineType)
	if err != nil {
		s.log.Debug("跳过 root reconcile，插件不存在", "engine_id", resource.ID, "engine_type", resource.EngineType, "error", err)
		return false
	}
	if scanflow.CatalogModelForPlugin(enginePlugin) == nil {
		return false
	}
	if _, err := metaRepo.EnsureCatalogRootNode(metaRepo.NewScanRepository(s.db), *resource.TenantID, resource, enginePlugin); err != nil {
		s.log.Warn("同步 catalog root 失败", "engine_id", resource.ID, "engine_type", resource.EngineType, "error", err)
		return false
	}
	return true
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

func (s *EngineService) cacheEngine(resource *commonModels.Engine) {
	if resource == nil {
		return
	}
	resourceCopy := *resource
	s.cacheMu.Lock()
	s.engineCache[resourceCopy.ID] = &engineCacheEntry{
		resource:  &resourceCopy,
		expiresAt: time.Now().Add(s.cacheTTL),
	}
	s.cacheMu.Unlock()
}

// ClearCache 清除所有引擎缓存
func (s *EngineService) ClearCache() {
	s.cacheMu.Lock()
	s.engineCache = make(map[uint]*engineCacheEntry)
	s.cacheMu.Unlock()
	s.log.Info("引擎缓存已清除")
}

// ClearEngineCache 清除指定资源的缓存
func (s *EngineService) ClearEngineCache(engineID uint) {
	s.cacheMu.Lock()
	delete(s.engineCache, engineID)
	s.cacheMu.Unlock()
	s.log.Info("引擎缓存已清除", "engine_id", engineID)
}

func (s *EngineService) snapshotCache() map[uint]*commonModels.Engine {
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()

	if len(s.engineCache) == 0 {
		return nil
	}

	now := time.Now()
	result := make(map[uint]*commonModels.Engine, len(s.engineCache))
	for id, entry := range s.engineCache {
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

// GetEnginesByTenant 获取租户下所有具备 storage 能力的 active engine。
func (s *EngineService) GetEnginesByTenant(tenantID uint) ([]*commonModels.Engine, error) {
	s.ensureInternalClient()

	if s.internalClient != nil {
		systemResources, err := s.internalClient.ListEngines("", tenantID)
		if err == nil {
			s.log.Info("从 System API 获取资源列表成功",
				"tenant_id", tenantID,
				"resource_total", len(systemResources),
			)
			var engines []*commonModels.Engine
			for i := range systemResources {
				res := systemResources[i]
				if !res.IsActive {
					continue
				}

				// 使用 capability 过滤：只要有 storage 能力就纳入元数据管理
				if utils.HasStorageCapability(&res) {
					if tenantID > 0 && (res.TenantID == nil || *res.TenantID != tenantID) {
						continue
					}
					resourceCopy := res
					engines = append(engines, &resourceCopy)
				}
			}
			s.log.Info("最终引擎列表", "tenant_id", tenantID, "count", len(engines))
			return engines, nil
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
		return []*commonModels.Engine{}, nil // 返回空列表而不是错误
	}

	var engines []*commonModels.Engine
	for _, resource := range cached {
		if resource == nil || !resource.IsActive {
			continue
		}
		if tenantID > 0 && (resource.TenantID == nil || *resource.TenantID != tenantID) {
			continue
		}
		// 使用 capability 过滤：只要有 storage 能力就纳入元数据管理
		if utils.HasStorageCapability(resource) {
			resourceCopy := *resource
			engines = append(engines, &resourceCopy)
		}
	}

	return engines, nil
}

// GetResourceByID 根据 ID 获取 engine 连接信息（优先内部 API，必要时用用户 token 降级）。
func (s *EngineService) GetResourceByID(engineID, tenantID uint, token string) (*commonModels.Engine, error) {
	s.ensureInternalClient()

	// 检查缓存
	s.cacheMu.RLock()
	entry, ok := s.engineCache[engineID]
	s.cacheMu.RUnlock()

	// 如果缓存命中且未过期，返回缓存数据
	if ok && entry != nil && entry.resource != nil && time.Now().Before(entry.expiresAt) {
		if tenantID == 0 || (entry.resource.TenantID != nil && *entry.resource.TenantID == tenantID) {
			resourceCopy := *entry.resource
			fields := append(connectionLogFields(&resourceCopy),
				"requested_tenant_id", tenantID,
				"source", "cache",
				"expires_in_seconds", int(time.Until(entry.expiresAt).Seconds()),
			)
			s.log.Info("引擎连接信息命中缓存", fields...)
			return &resourceCopy, nil
		}
	}

	// 缓存未命中或已过期，从 System API 获取

	if s.internalClient != nil {
		resource, err := s.internalClient.GetEngine(engineID)
		if err != nil {
			return nil, fmt.Errorf("failed to get resource from System API: %w", err)
		}
		if tenantID > 0 && (resource.TenantID == nil || *resource.TenantID != tenantID) {
			return nil, fmt.Errorf("resource not found or access denied")
		}
		s.cacheEngine(resource)
		fields := append(connectionLogFields(resource),
			"requested_tenant_id", tenantID,
			"source", "internal_api",
		)
		s.log.Info("通过内部 API 获取引擎连接信息成功", fields...)
		return resource, nil
	}

	// 使用用户token创建SystemClient（无内部密钥时降级使用用户接口，敏感字段将被脱敏）
	systemClient := commonClient.NewSystemClient(s.systemURL, token)

	resource, err := systemClient.GetEngine(engineID)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource from System API: %w", err)
	}

	if tenantID > 0 && (resource.TenantID == nil || *resource.TenantID != tenantID) {
		return nil, fmt.Errorf("resource not found or access denied")
	}

	// 用户接口返回的数据会对敏感字段做掩码，导致后续建链失败。
	// 如果检测到敏感字段被掩码，尝试使用内部客户端重新获取一次。
	s.ensureInternalClient()
	if s.internalClient != nil && containsMaskedSensitive(resource.ConnectionInfo) {
		if internalRes, err := s.internalClient.GetEngine(engineID); err == nil {
			resource = internalRes
			s.log.Info("检测到敏感字段被掩码，使用内部 API 重新获取引擎连接信息",
				append(connectionLogFields(resource),
					"requested_tenant_id", tenantID,
					"source", "internal_api_retry",
				)...,
			)
		}
	}

	s.cacheEngine(resource)
	fields := append(connectionLogFields(resource),
		"requested_tenant_id", tenantID,
		"source", "user_api",
	)
	s.log.Info("通过用户 API 获取引擎连接信息成功", fields...)
	return resource, nil
}

// GetEngine 实现 MVT 侧 engine 查询接口。
// 用于 MVT 生成器获取引擎连接信息
func (s *EngineService) GetEngine(engineID, tenantID uint) (*commonModels.Engine, error) {
	// 直接调用 GetResourceByID，使用空 token（因为是内部调用）
	return s.GetResourceByID(engineID, tenantID, "")
}

// GetEnginesWithStats 获取 engine 及其扫描统计。
func (s *EngineService) GetEnginesWithStats(tenantID uint) ([]*models.ResourceWithStats, error) {
	engines, err := s.GetEnginesByTenant(tenantID)
	if err != nil {
		return nil, err
	}

	if len(engines) == 0 {
		return []*models.ResourceWithStats{}, nil
	}

	engineIDs := make([]uint, 0, len(engines))
	for _, res := range engines {
		engineIDs = append(engineIDs, res.ID)
	}

	totalCount := make(map[uint]int64)
	scannedCount := make(map[uint]int64)
	lastScanByRes := make(map[uint]*time.Time)

	type countRow struct {
		EngineID uint
		Count    int64
	}
	var totals []countRow
	if err := s.db.Table("meta.meta_node").
		Where("engine_id IN ? AND parent_node_id IN (?)", engineIDs,
			s.db.Table("meta.meta_node").
				Select("id").
				Where("engine_id IN ? AND parent_node_id IS NULL AND full_name = ?", engineIDs, ""),
		).
		Select("engine_id, COUNT(*) AS count").
		Group("engine_id").
		Scan(&totals).Error; err != nil {
		return nil, fmt.Errorf("failed to count meta nodes: %w", err)
	}
	for _, row := range totals {
		totalCount[row.EngineID] = row.Count
	}

	var scanned []countRow
	if err := s.db.Table("meta.meta_node").
		Where("engine_id IN ? AND scan_status = ? AND parent_node_id IN (?)", engineIDs, "completed",
			s.db.Table("meta.meta_node").
				Select("id").
				Where("engine_id IN ? AND parent_node_id IS NULL AND full_name = ?", engineIDs, ""),
		).
		Select("engine_id, COUNT(*) AS count").
		Group("engine_id").
		Scan(&scanned).Error; err != nil {
		return nil, fmt.Errorf("failed to count scanned nodes: %w", err)
	}
	for _, row := range scanned {
		scannedCount[row.EngineID] = row.Count
	}

	type lastScanRow struct {
		EngineID   uint
		LastScanAt *time.Time `gorm:"column:scanned_at"`
	}
	var lastScans []lastScanRow
	if err := s.db.Table("meta.meta_node").
		Where("engine_id IN ?", engineIDs).
		Where("scanned_at IS NOT NULL").
		Select("engine_id, MAX(scanned_at) AS scanned_at").
		Group("engine_id").
		Scan(&lastScans).Error; err != nil {
		return nil, fmt.Errorf("failed to query node last scan time: %w", err)
	}
	for _, row := range lastScans {
		lastScanByRes[row.EngineID] = row.LastScanAt
	}

	result := make([]*models.ResourceWithStats, 0, len(engines))
	for _, res := range engines {
		totalCatalogNodes := 0
		scannedCatalogNodes := 0
		lastScanAt := ""
		engineFamily := ""
		catalogRootTerm := ""
		catalogTopTerm := ""
		catalogTopI18nKey := ""
		catalogLeafTerm := ""
		catalogLeafI18nKey := ""

		if cnt, ok := totalCount[res.ID]; ok {
			totalCatalogNodes = int(cnt)
		}
		if cnt, ok := scannedCount[res.ID]; ok {
			scannedCatalogNodes = int(cnt)
		}
		if ts, ok := lastScanByRes[res.ID]; ok && ts != nil {
			lastScanAt = ts.Format("2006-01-02 15:04:05")
		}

		// 填充连接状态信息
		lastCheckAt := ""
		if res.LastCheckAt != nil {
			lastCheckAt = res.LastCheckAt.Format("2006-01-02 15:04:05")
		}
		if enginePlugin, err := plugin.Get(res.EngineType); err == nil {
			capabilities := enginePlugin.Capabilities()
			engineFamily = capabilities.EngineFamily
			if model := scanflow.CatalogModelForPlugin(enginePlugin); model != nil {
				catalogRootTerm = model.RootTerm
				if level, ok := plugin.CatalogFirstBusinessBranch(*model); ok {
					catalogTopTerm = level.Term
					catalogTopI18nKey = level.I18nKey
					if catalogTopI18nKey == "" {
						catalogTopI18nKey = plugin.CatalogLevelI18nKey(*model, level.Term)
					}
				}
				catalogLeafTerm = plugin.CatalogLeafTerm(*model)
				catalogLeafI18nKey = plugin.CatalogLevelI18nKey(*model, catalogLeafTerm)
			}
		}

		result = append(result, &models.ResourceWithStats{
			EngineID:              res.ID,
			ResourceName:          res.Name,
			ResourceType:          res.EngineType,
			EngineFamily:          engineFamily,
			CatalogRootTerm:       catalogRootTerm,
			CatalogTopTerm:        catalogTopTerm,
			CatalogTopI18nKey:     catalogTopI18nKey,
			CatalogLeafTerm:       catalogLeafTerm,
			CatalogLeafI18nKey:    catalogLeafI18nKey,
			TotalCatalogNodes:     totalCatalogNodes,
			ScannedCatalogNodes:   scannedCatalogNodes,
			UnscannedCatalogNodes: totalCatalogNodes - scannedCatalogNodes,
			ScannedAt:             lastScanAt,
			ConnectionStatus:      res.ConnectionStatus,
			LastCheckAt:           lastCheckAt,
			CheckMessage:          res.CheckMessage,
		})
	}

	return result, nil
}

// TriggerConnectionCheck 触发System检测连接状态
// 仅在连接失败时调用，通知System刷新状态
// 异步触发，不阻塞调用方
func (s *EngineService) TriggerConnectionCheck(engineID uint) {
	s.ensureInternalClient()
	if s.internalClient == nil {
		s.log.Warn("Internal client不可用，无法触发连接检测", "engine_id", engineID)
		return
	}

	url := fmt.Sprintf("%s/internal/engines/%d/check-connection", s.systemURL, engineID)

	// 异步发送请求，不阻塞
	go func() {
		err := s.internalClient.DoRequest("POST", url, nil, nil)
		if err != nil {
			s.log.Debug("触发连接检测失败（非致命）", "engine_id", engineID, "error", err)
		} else {
			s.log.Debug("已触发System刷新连接状态", "engine_id", engineID)
		}
	}()
}

package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/events"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/utils"
	"github.com/addp/meta/internal/models"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// engineCacheEntry 缓存条目，包含资源和过期时间
type engineCacheEntry struct {
	resource  *commonModels.Engine
	expiresAt time.Time
}

// ResourceService 资源服务 - 直接读取 system.engines
type EngineService struct {
	db              *gorm.DB
	systemURL       string
	internalClient  *commonClient.SystemClient
	cacheMu         sync.RWMutex
	engineCache     map[uint]*engineCacheEntry // 改为存储带过期时间的条目
	cacheTTL        time.Duration              // 缓存生存时间，默认 5 分钟
	log             *slog.Logger
	eventSubscriber *events.EngineEventSubscriber // Redis 事件订阅器
	taskService     ScanTaskServiceInterface      // 扫描任务服务（用于处理 ScanConfig）
}

// ScanTaskServiceInterface 扫描任务服务接口（避免循环依赖）
type ScanTaskServiceInterface interface {
	CreateOrUpdateTaskFromScanConfig(resource *commonModels.Engine) error
	DeleteTaskByResourceID(engineID uint) error
	CreateManualRun(ctx context.Context, tenantID, userID uint, token string, req *models.ScanRequest) (*commonModels.TaskExecution, error)
}

func NewEngineService(db *gorm.DB, systemURL, internalKey string, redisClient *redis.Client) *EngineService {
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

	// 初始化 Redis 事件订阅器
	if redisClient != nil {
		service.eventSubscriber = events.NewEngineEventSubscriber(
			redisClient,
			service.handleEngineChangeEvent,
			service.log,
		)
		// 启动订阅（在后台 goroutine 中）
		go func() {
			if err := service.eventSubscriber.Start(); err != nil {
				service.log.Error("引擎事件订阅器启动失败", "error", err)
			}
		}()
		service.log.Info("引擎事件订阅器已启动")
	} else {
		service.log.Warn("Redis 未配置，资源变更事件同步功能将被禁用")
	}

	return service
}

// SetTaskService 设置扫描任务服务（在 main.go 中初始化后调用）
func (s *EngineService) SetTaskService(taskService ScanTaskServiceInterface) {
	s.taskService = taskService
	s.log.Info("扫描任务服务已注入到资源服务")
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

// PreloadResources 在服务启动时从 System 服务加载引擎连接信息并缓存在内存中
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
	for i := range engines {
		res := engines[i]
		if !res.IsActive {
			continue
		}
		engineCopy := res
		cache[engineCopy.ID] = &engineCacheEntry{
			resource:  &engineCopy,
			expiresAt: expiresAt,
		}
	}

	s.cacheMu.Lock()
	s.engineCache = cache
	s.cacheMu.Unlock()

	s.log.Info("引擎缓存预加载完成", "active_engines", len(cache), "system_url", s.systemURL, "ttl_minutes", s.cacheTTL.Minutes())
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

// GetResourcesByTenant 获取租户的所有数据库类型资源
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

// GetResourceByID 根据ID获取资源（从System API获取，密码已解密）
// token: 用户的JWT token，用于认证System API调用
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

// GetResource 实现 mvt.ResourceService 接口
// 用于 MVT 生成器获取引擎连接信息
func (s *EngineService) GetEngine(engineID, tenantID uint) (*commonModels.Engine, error) {
	// 直接调用 GetResourceByID，使用空 token（因为是内部调用）
	return s.GetResourceByID(engineID, tenantID, "")
}

// GetResourcesWithStats 获取资源及其扫描统计
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
	if err := s.db.Table("metadata.meta_node").
		Where("tenant_id = ? AND engine_id IN ?", tenantID, engineIDs).
		Where("parent_node_id IS NULL").
		Select("engine_id, COUNT(*) AS count").
		Group("engine_id").
		Scan(&totals).Error; err != nil {
		return nil, fmt.Errorf("failed to count meta nodes: %w", err)
	}
	for _, row := range totals {
		totalCount[row.EngineID] = row.Count
	}

	var scanned []countRow
	if err := s.db.Table("metadata.meta_node").
		Where("tenant_id = ? AND engine_id IN ?", tenantID, engineIDs).
		Where("parent_node_id IS NULL AND scan_status = ?", "completed").
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
	if err := s.db.Table("metadata.meta_node").
		Where("tenant_id = ? AND engine_id IN ?", tenantID, engineIDs).
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
		totalNamespaces := 0
		scannedNamespaces := 0
		lastScanAt := ""
		engineFamily := ""
		catalogRootTerm := ""
		catalogItemTerm := ""

		if cnt, ok := totalCount[res.ID]; ok {
			totalNamespaces = int(cnt)
		}
		if cnt, ok := scannedCount[res.ID]; ok {
			scannedNamespaces = int(cnt)
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
			if model := catalogModelForPlugin(enginePlugin); model != nil {
				catalogRootTerm = model.RootTerm
				catalogItemTerm = plugin.CatalogItemTerm(*model)
			}
		}

		result = append(result, &models.ResourceWithStats{
			EngineID:            res.ID,
			ResourceName:        res.Name,
			ResourceType:        res.EngineType,
			EngineFamily:        engineFamily,
			CatalogRootTerm:     catalogRootTerm,
			CatalogItemTerm:     catalogItemTerm,
			TotalNamespaces:     totalNamespaces,
			ScannedNamespaces:   scannedNamespaces,
			UnscannedNamespaces: totalNamespaces - scannedNamespaces,
			ScannedAt:           lastScanAt,
			ConnectionStatus:    res.ConnectionStatus,
			LastCheckAt:         lastCheckAt,
			CheckMessage:        res.CheckMessage,
		})
	}

	return result, nil
}

// handleEngineChangeEvent 处理资源变更事件（Redis 订阅回调）
func (s *EngineService) handleEngineChangeEvent(event events.EngineChangeEvent) error {
	s.log.Info("收到资源变更事件",
		"engine_id", event.EngineID,
		"action", event.Action,
		"timestamp", event.Timestamp)

	switch event.Action {
	case events.ActionCreate, events.ActionUpdate:
		// 资源创建或更新：检查 ScanConfig
		s.ClearEngineCache(event.EngineID)

		// 如果配置了 taskService，尝试处理扫描配置
		if s.taskService != nil && s.internalClient != nil {
			// 获取资源详情（包含 ScanConfig）
			resource, err := s.internalClient.GetEngine(event.EngineID)
			if err != nil {
				s.log.Error("获取资源详情失败，跳过扫描配置处理",
					"engine_id", event.EngineID,
					"error", err)
				return nil // 不阻塞事件处理
			}

			// 检查是否有扫描配置
			if resource.ScanConfig != nil && (resource.ScanConfig.ImmediateScan || resource.ScanConfig.ScheduledScan) {
				// 1. 处理立即扫描
				if resource.ScanConfig.ImmediateScan {
					s.log.Info("检测到立即扫描配置，准备触发扫描", "engine_id", event.EngineID)
					go func() {
						if err := s.triggerImmediateScan(resource); err != nil {
							s.log.Error("立即扫描失败",
								"engine_id", event.EngineID,
								"error", err)
						} else {
							s.log.Info("立即扫描已触发", "engine_id", event.EngineID)
						}
					}()
				}

				// 2. 处理定时扫描
				if resource.ScanConfig.ScheduledScan {
					s.log.Info("检测到定时扫描配置，准备创建定时任务",
						"engine_id", event.EngineID,
						"schedule_type", resource.ScanConfig.ScheduleType)
					if err := s.taskService.CreateOrUpdateTaskFromScanConfig(resource); err != nil {
						s.log.Error("创建定时扫描任务失败",
							"engine_id", event.EngineID,
							"error", err)
						return err
					}
					s.log.Info("定时扫描任务已创建", "engine_id", event.EngineID)
				}
			} else {
				// 如果扫描配置被禁用或删除，删除对应的自动任务
				if err := s.taskService.DeleteTaskByResourceID(event.EngineID); err != nil {
					s.log.Warn("删除自动扫描任务失败",
						"engine_id", event.EngineID,
						"error", err)
				} else {
					s.log.Info("扫描配置已禁用，自动任务已删除", "engine_id", event.EngineID)
				}
			}
		}

		if event.Action == events.ActionCreate {
			s.log.Debug("资源已创建", "engine_id", event.EngineID)
		} else {
			s.log.Info("资源已更新，缓存已清除", "engine_id", event.EngineID)
		}

	case events.ActionDelete:
		// 资源删除：清除缓存并删除扫描任务
		s.ClearEngineCache(event.EngineID)

		if s.taskService != nil {
			if err := s.taskService.DeleteTaskByResourceID(event.EngineID); err != nil {
				s.log.Warn("删除资源关联的扫描任务失败",
					"engine_id", event.EngineID,
					"error", err)
			} else {
				s.log.Info("资源已删除，关联任务已清理", "engine_id", event.EngineID)
			}
		}

	default:
		s.log.Warn("未知的资源变更动作", "action", event.Action, "engine_id", event.EngineID)
	}

	return nil
}

// Stop 停止资源服务（清理资源）
func (s *EngineService) Stop() {
	if s.eventSubscriber != nil {
		s.eventSubscriber.Stop()
		s.log.Info("引擎事件订阅器已停止")
	}
}

// triggerImmediateScan 立即触发扫描（用于 immediate 类型的 ScanConfig）
func (s *EngineService) triggerImmediateScan(resource *commonModels.Engine) error {
	if s.taskService == nil {
		return fmt.Errorf("扫描任务服务未初始化")
	}

	if resource.ScanConfig == nil {
		return fmt.Errorf("资源 %d 没有扫描配置", resource.ID)
	}

	// 确定扫描深度：优先使用 ImmediateDepth，否则回退到旧版 ScanDepth
	scanDepth := resource.ScanConfig.ImmediateDepth
	if scanDepth == "" {
		scanDepth = resource.ScanConfig.ScanDepth
	}
	if scanDepth == "" {
		scanDepth = "basic" // 默认使用基础扫描
	}

	// 构建扫描请求（不限制 namespaces/object_paths，系统自动过滤）
	req := &models.ScanRequest{
		EngineID:  resource.ID,
		ScanDepth: scanDepth,
		ScanType:  "auto", // 标记为自动扫描
	}

	// 创建扫描运行（使用系统用户 ID=1，租户 ID 从资源获取）
	var tenantID uint
	if resource.TenantID == nil || *resource.TenantID == 0 {
		tenantID = 1 // 默认租户
	} else {
		tenantID = *resource.TenantID
	}

	// 使用系统内部 token 创建扫描任务
	ctx := context.Background()
	run, err := s.taskService.CreateManualRun(ctx, tenantID, 0, "", req)
	if err != nil {
		return fmt.Errorf("创建立即扫描任务失败: %w", err)
	}

	s.log.Info("立即扫描任务已创建",
		"engine_id", resource.ID,
		"run_id", run.ID,
		"scan_depth", scanDepth,
		"tenant_id", tenantID)

	return nil
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

package service

import (
	"context"
	"log/slog"

	"github.com/addp/common/events"
	"github.com/addp/common/logger"
	"github.com/redis/go-redis/v9"
)

// ScanEventHandler 扫描事件处理器
// 订阅 Meta 模块的扫描完成事件，自动清理相关缓存
type ScanEventHandler struct {
	cacheManager        *CacheManager
	scanEventSubscriber *events.ScanEventSubscriber
	log                 *slog.Logger
}

// NewScanEventHandler 创建扫描事件处理器
// 自动启动事件订阅（如果 Redis 客户端可用）
func NewScanEventHandler(cacheManager *CacheManager, redisClient *redis.Client) *ScanEventHandler {
	handler := &ScanEventHandler{
		cacheManager: cacheManager,
		log:          logger.With("component", "scan_event_handler"),
	}

	// 初始化订阅器
	if redisClient != nil {
		handler.log.Info("初始化扫描事件订阅器")
		handler.scanEventSubscriber = events.NewScanEventSubscriber(
			redisClient,
			handler.handleScanCompleted,
			handler.log,
		)

		// 在单独的 goroutine 中启动订阅（阻塞调用）
		go handler.scanEventSubscriber.Start()
	} else {
		handler.log.Warn("Redis 客户端未配置，扫描事件同步功能将被禁用")
	}

	return handler
}

// handleScanCompleted 处理扫描完成事件
func (h *ScanEventHandler) handleScanCompleted(event events.ScanCompletedEvent) error {
	h.log.Info("收到扫描完成事件",
		"engine_id", event.EngineID,
		"tenant_id", event.TenantID,
		"scan_type", event.ScanType,
		"items_count", event.ScannedItemsCount)

	// 清理资源相关缓存
	ctx := context.Background()
	if err := h.cacheManager.ClearResourceCache(ctx, event.EngineID); err != nil {
		h.log.Error("清理资源缓存失败",
			"engine_id", event.EngineID,
			"tenant_id", event.TenantID,
			"error", err)
		return err
	}

	h.log.Info("扫描完成事件处理完成",
		"engine_id", event.EngineID,
		"tenant_id", event.TenantID)

	return nil
}

// Stop 停止事件订阅
func (h *ScanEventHandler) Stop() {
	if h.scanEventSubscriber != nil {
		h.log.Info("停止扫描事件订阅")
		h.scanEventSubscriber.Stop()
	}
}

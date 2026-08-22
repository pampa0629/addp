package service

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
)

const (
	startupCheckRetryWindow    = 60 * time.Second
	startupCheckRetryInterval  = 2 * time.Second
	DefaultHealthCheckInterval = 30 * time.Second
)

// HealthChecker 资源健康检查器
// 负责在 System 启动时及运行期间检测所有资源的最近连通性
type HealthChecker struct {
	resourceService *EngineService
	log             *slog.Logger
	retryWindow     time.Duration
	retryInterval   time.Duration
}

// NewHealthChecker 创建健康检查器
func NewHealthChecker(resourceService *EngineService) *HealthChecker {
	return &HealthChecker{
		resourceService: resourceService,
		log:             logger.With("component", "health_checker"),
		retryWindow:     startupCheckRetryWindow,
		retryInterval:   startupCheckRetryInterval,
	}
}

// CheckAllResourcesOnStartup 启动时检测所有资源健康状态
// 并发检测，限制并发数为10，避免同时检测过多资源导致资源耗尽
func (h *HealthChecker) CheckAllResourcesOnStartup() {
	h.checkAllResources(context.Background(), true)
}

// Run performs the bounded startup check and then periodically refreshes the
// observation cache. A failed instance never blocks other instances or System
// readiness, and a runtime started later can recover on a subsequent pass.
func (h *HealthChecker) Run(ctx context.Context, interval time.Duration) {
	if ctx == nil {
		ctx = context.Background()
	}
	if interval <= 0 {
		interval = DefaultHealthCheckInterval
	}

	h.checkAllResources(ctx, true)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.checkAllResources(ctx, false)
		}
	}
}

func (h *HealthChecker) checkAllResources(ctx context.Context, retryOffline bool) {
	h.log.Info("开始检测所有资源健康状态...",
		"retry_window", h.retryWindow,
		"retry_interval", h.retryInterval,
		"retry_offline", retryOffline)

	// 获取所有资源列表
	engines, _, err := h.resourceService.repo.List(0, 9999, "")
	if err != nil {
		h.log.Error("获取引擎列表失败", "error", err)
		return
	}

	if len(engines) == 0 {
		h.log.Info("没有资源需要检测")
		return
	}

	// 并发检测，限制并发数为10
	semaphore := make(chan struct{}, 10)
	var wg sync.WaitGroup
	var onlineCount atomic.Int32
	var offlineCount atomic.Int32

	for _, res := range engines {
		wg.Add(1)

		go func(resource commonModels.Engine) {
			defer wg.Done()

			// 信号量只包围实际探测，不包围重试等待，避免前面的慢资源阻塞后续资源首次检测。
			check := func() bool {
				select {
				case semaphore <- struct{}{}:
				case <-ctx.Done():
					return false
				}
				defer func() { <-semaphore }()
				return h.resourceService.checkAndUpdateConnectionStatus(resource.ID, retryOffline)
			}

			if check() {
				onlineCount.Add(1)
				return
			}

			// unknown 不是连通失败（例如 api.* 资源不支持自动检测），不进入重试窗口。
			current, err := h.resourceService.repo.GetByID(resource.ID)
			if err != nil || !strings.EqualFold(strings.TrimSpace(current.ConnectionStatus), "offline") {
				offlineCount.Add(1)
				return
			}

			if retryOffline && retryConnectionCheckContext(ctx, check, h.retryWindow, h.retryInterval) {
				onlineCount.Add(1)
			} else {
				offlineCount.Add(1)
			}
		}(res)
	}

	wg.Wait()
	h.log.Info("资源健康检测完成",
		"total", len(engines),
		"online", onlineCount.Load(),
		"offline", offlineCount.Load())
}

// retryConnectionCheck retries a transiently failed probe within a bounded startup window.
// The first probe is performed by the caller so that only an offline result enters this path.
func retryConnectionCheck(probe func() bool, retryWindow, retryInterval time.Duration) bool {
	return retryConnectionCheckContext(context.Background(), probe, retryWindow, retryInterval)
}

func retryConnectionCheckContext(ctx context.Context, probe func() bool, retryWindow, retryInterval time.Duration) bool {
	if probe == nil {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if retryWindow <= 0 {
		return probe()
	}
	if retryInterval <= 0 {
		retryInterval = time.Millisecond
	}

	deadline := time.Now().Add(retryWindow)
	for {
		if probe() {
			return true
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false
		}
		if retryInterval > remaining {
			retryInterval = remaining
		}
		timer := time.NewTimer(retryInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return false
		case <-timer.C:
		}
	}
}

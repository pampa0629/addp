package service

import (
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
)

// HealthChecker 资源健康检查器
// 负责在System启动时检测所有资源的连接状态
type HealthChecker struct {
	resourceService *ResourceService
	log             *slog.Logger
}

// NewHealthChecker 创建健康检查器
func NewHealthChecker(resourceService *ResourceService) *HealthChecker {
	return &HealthChecker{
		resourceService: resourceService,
		log:             logger.With("component", "health_checker"),
	}
}

// CheckAllResourcesOnStartup 启动时检测所有资源健康状态
// 并发检测，限制并发数为10，避免同时检测过多资源导致资源耗尽
func (h *HealthChecker) CheckAllResourcesOnStartup() {
	h.log.Info("开始检测所有资源健康状态...")

	// 获取所有资源列表
	resources, err := h.resourceService.repo.List(0, 9999, "")
	if err != nil {
		h.log.Error("获取资源列表失败", "error", err)
		return
	}

	if len(resources) == 0 {
		h.log.Info("没有资源需要检测")
		return
	}

	// 并发检测，限制并发数为10
	semaphore := make(chan struct{}, 10)
	var wg sync.WaitGroup
	var onlineCount atomic.Int32
	var offlineCount atomic.Int32

	for _, res := range resources {
		wg.Add(1)
		semaphore <- struct{}{} // 获取信号量

		go func(resource commonModels.Resource) {
			defer wg.Done()
			defer func() { <-semaphore }() // 释放信号量

			// 检测连接并更新状态
			if h.resourceService.CheckAndUpdateConnectionStatus(resource.ID) {
				onlineCount.Add(1)
			} else {
				offlineCount.Add(1)
			}
		}(res)
	}

	wg.Wait()
	h.log.Info("资源健康检测完成",
		"total", len(resources),
		"online", onlineCount.Load(),
		"offline", offlineCount.Load())
}

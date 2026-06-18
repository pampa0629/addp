package service

import (
	"fmt"
	"log/slog"

	"github.com/addp/common/events"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/redis/go-redis/v9"
)

// EngineSyncService 负责引擎变更事件的同步处理
type EngineSyncService struct {
	engineService   *EngineService
	eventSubscriber *events.EngineEventSubscriber
	log             *slog.Logger
}

// NewEngineSyncService 创建引擎同步服务
func NewEngineSyncService(
	redisClient *redis.Client,
	engineService *EngineService,
) *EngineSyncService {
	s := &EngineSyncService{
		engineService: engineService,
		log:           logger.With("component", "engine_sync_service"),
	}

	if redisClient == nil {
		s.log.Warn("Redis 未配置，引擎事件同步已禁用")
		return s
	}

	s.eventSubscriber = events.NewEngineEventSubscriber(redisClient, s.handleEngineChangeEvent, s.log)
	return s
}

// Start 启动引擎事件同步
func (s *EngineSyncService) Start() {
	if s.eventSubscriber == nil {
		return
	}

	go func() {
		if err := s.eventSubscriber.Start(); err != nil {
			s.log.Error("引擎事件订阅器启动失败", "error", err)
		}
	}()
	s.log.Info("引擎事件订阅器已启动")
}

// Stop 停止引擎事件同步
func (s *EngineSyncService) Stop() {
	if s.eventSubscriber == nil {
		return
	}

	s.eventSubscriber.Stop()
	s.log.Info("引擎事件订阅器已停止")
}

// handleEngineChangeEvent 处理资源变更事件（Redis 订阅回调）
func (s *EngineSyncService) handleEngineChangeEvent(event events.EngineChangeEvent) error {
	if s.engineService == nil {
		return fmt.Errorf("engine service not configured")
	}

	s.log.Info("收到资源变更事件",
		"engine_id", event.EngineID,
		"action", event.Action,
		"timestamp", event.Timestamp)

	switch event.Action {
	case events.ActionCreate, events.ActionUpdate:
		s.engineService.ClearEngineCache(event.EngineID)

		resource, err := s.loadEngine(event.EngineID)
		if err != nil {
			s.log.Error("获取资源详情失败，跳过 catalog root 维护",
				"engine_id", event.EngineID,
				"error", err)
			return nil
		}
		if s.engineService.rootReconciler != nil {
			s.engineService.rootReconciler.Reconcile(resource)
		}

		if event.Action == events.ActionCreate {
			s.log.Debug("资源已创建", "engine_id", event.EngineID)
		} else {
			s.log.Info("资源已更新，缓存已清除", "engine_id", event.EngineID)
		}

	case events.ActionDelete:
		s.engineService.ClearEngineCache(event.EngineID)
		s.log.Info("资源已删除，缓存已清除；扫描任务定义残留由 cleanup executor 处理", "engine_id", event.EngineID)
	default:
		s.log.Warn("未知的资源变更动作", "action", event.Action, "engine_id", event.EngineID)
	}

	return nil
}

func (s *EngineSyncService) loadEngine(engineID uint) (*commonModels.Engine, error) {
	if s.engineService == nil {
		return nil, fmt.Errorf("engine service not configured")
	}

	s.engineService.ensureInternalClient()
	if s.engineService.internalClient == nil {
		return nil, fmt.Errorf("internal client not configured")
	}

	resource, err := s.engineService.internalClient.GetEngine(engineID)
	if err != nil {
		return nil, fmt.Errorf("failed to get engine from system api: %w", err)
	}

	return resource, nil
}

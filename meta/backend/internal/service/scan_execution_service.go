package service

import (
	"log/slog"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/logger"
	"github.com/addp/meta/internal/scantask"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// ExecutionDispatcher 负责把 execution 放入调度队列或执行队列
type ExecutionDispatcher interface {
	EnqueueExecution(executionID string)
}

// ScanExecutionService 管理 meta 扫描 execution 的创建、执行、查询和取消
type ScanExecutionService struct {
	db                *gorm.DB
	scanService       *ScanService
	engineService     *EngineService
	dedupService      *ScanDedupService
	taskExecutionRepo *commonExecution.TaskExecutionRepository
	log               *slog.Logger
	dispatcher        ExecutionDispatcher
}

// NewScanExecutionService 创建扫描执行服务
func NewScanExecutionService(db *gorm.DB, scanService *ScanService, engineService *EngineService, redisClient *redis.Client) *ScanExecutionService {
	if scanService == nil {
		scanService = NewScanService(db, engineService)
	}

	var dedupService *ScanDedupService
	if redisClient != nil {
		dedupService = NewScanDedupService(redisClient)
		scanService.SetDedupService(dedupService)
	}

	return &ScanExecutionService{
		db:                db,
		scanService:       scanService,
		engineService:     engineService,
		dedupService:      dedupService,
		taskExecutionRepo: commonExecution.NewTaskExecutionRepository(db),
		log:               logger.With("component", "scan_execution_service"),
	}
}

// SetExecutionDispatcher 设置 execution 分发器
func (s *ScanExecutionService) SetExecutionDispatcher(dispatcher ExecutionDispatcher) {
	s.dispatcher = dispatcher
}

func (s *ScanExecutionService) enqueueExecution(executionID string) {
	if s.dispatcher == nil {
		return
	}
	s.dispatcher.EnqueueExecution(executionID)
}

func (s *ScanExecutionService) lookupStorageType(engineID, tenantID uint) string {
	if s.engineService == nil {
		return "unknown"
	}
	resource, err := s.engineService.GetResourceByID(engineID, tenantID, "")
	if err != nil {
		s.log.Warn("获取资源存储类型失败", "engine_id", engineID, "tenant_id", tenantID, "error", err)
		return "unknown"
	}
	return scantask.NormalizeStorageType(resource.EngineType)
}

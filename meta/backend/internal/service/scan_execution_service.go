package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/logger"
	"github.com/addp/meta/internal/scantask"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// ScanExecutionService 管理 meta 扫描 execution 的创建、执行、查询和取消
type ScanExecutionService struct {
	db                *gorm.DB
	scanService       *ScanService
	engineService     *EngineService
	dedupService      *ScanDedupService
	taskExecutionRepo *commonExecution.TaskExecutionRepository
	log               *slog.Logger
	activeLeases      sync.Map
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

func (s *ScanExecutionService) BindBoundedLease(executionID string, lease commonExecution.Lease) {
	s.activeLeases.Store(executionID, lease)
}

func (s *ScanExecutionService) UnbindBoundedLease(executionID string) {
	s.activeLeases.Delete(executionID)
}

func (s *ScanExecutionService) boundedLease(ctx context.Context, executionID string) (commonExecution.Lease, bool) {
	if lease, ok := commonExecution.LeaseFromContext(ctx); ok && lease.ExecutionID == executionID {
		return lease, true
	}
	value, ok := s.activeLeases.Load(executionID)
	if !ok {
		return commonExecution.Lease{}, false
	}
	lease, ok := value.(commonExecution.Lease)
	return lease, ok
}

func (s *ScanExecutionService) RenewBoundedExecutionLease(ctx context.Context, lease commonExecution.Lease, expiresAt time.Time) error {
	return commonExecution.RenewLease(ctx, s.db, lease, expiresAt)
}

func (s *ScanExecutionService) BoundedExecutionAttemptIsTerminal(ctx context.Context, lease commonExecution.Lease) (bool, error) {
	return commonExecution.AttemptIsTerminal(ctx, s.db, lease)
}

func (s *ScanExecutionService) lookupStorageType(engineID, tenantID uint) string {
	if s.engineService == nil {
		return "unknown"
	}
	resource, err := s.engineService.GetResourceByID(engineID, tenantID)
	if err != nil {
		s.log.Warn("获取资源存储类型失败", "engine_id", engineID, "tenant_id", tenantID, "error", err)
		return "unknown"
	}
	return scantask.NormalizeStorageType(resource.EngineType)
}

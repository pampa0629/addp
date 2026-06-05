package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/logger"
	commonScheduler "github.com/addp/common/scheduler"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

const (
	schedulePollInterval          = time.Minute
	foregroundExecutionWait       = 55 * time.Second
	foregroundExecutionPoll       = 300 * time.Millisecond
	foregroundExecutionWaitErrMsg = "execution wait timed out"
)

// TaskQueue 任务队列接口（避免循环依赖）
type TaskQueue interface {
	EnqueueScanTask(ctx context.Context, executionID string, taskID, tenantID uint) error
	Close() error
}

// ScanTaskService 管理扫描任务、队列与调度
type ScanTaskService struct {
	db                *gorm.DB
	scanService       *ScanService
	engineService     *EngineService
	dedupService      *ScanDedupService
	taskExecutionRepo *commonExecution.TaskExecutionRepository
	log               *slog.Logger
	taskQueue         TaskQueue

	// 本地队列（当 taskQueue 为 nil 时使用）
	queue   chan string // executionID (UUID)
	workers int
	stopCh  chan struct{}
	wg      sync.WaitGroup

	exprBuilder *commonScheduler.ExpressionBuilder
}

// NewScanTaskService 创建任务服务
func NewScanTaskService(db *gorm.DB, scanService *ScanService, engineService *EngineService, redisClient *redis.Client) *ScanTaskService {
	if scanService == nil {
		scanService = NewScanService(db, engineService)
	}

	var dedupService *ScanDedupService
	if redisClient != nil {
		dedupService = NewScanDedupService(redisClient)
		scanService.SetDedupService(dedupService)
	}

	return &ScanTaskService{
		db:                db,
		scanService:       scanService,
		engineService:     engineService,
		dedupService:      dedupService,
		taskExecutionRepo: commonExecution.NewTaskExecutionRepository(db),
		log:               logger.With("component", "scan_task_service"),
		queue:             make(chan string, 128),
		workers:           2,
		stopCh:            make(chan struct{}),
		exprBuilder:       commonScheduler.NewExpressionBuilder(),
	}
}

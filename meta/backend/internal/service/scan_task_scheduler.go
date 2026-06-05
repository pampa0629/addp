package service

import (
	"context"
	"sync"
	"time"
)

const schedulePollInterval = time.Minute

// TaskQueue 任务队列接口（避免循环依赖）
type TaskQueue interface {
	EnqueueScanTask(ctx context.Context, executionID string, taskID, tenantID uint) error
	Close() error
}

// ScanTaskScheduler 负责 execution 入队、队列消费与定时调度
type ScanTaskScheduler struct {
	taskService      *ScanTaskService
	executionService *ScanExecutionService
	taskQueue        TaskQueue

	queue    chan string
	workers  int
	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// NewScanTaskScheduler 创建调度器
func NewScanTaskScheduler(taskService *ScanTaskService, executionService *ScanExecutionService) *ScanTaskScheduler {
	s := &ScanTaskScheduler{
		taskService:      taskService,
		executionService: executionService,
		queue:            make(chan string, 128),
		workers:          2,
		stopCh:           make(chan struct{}),
	}
	if executionService != nil {
		executionService.SetExecutionDispatcher(s)
	}
	return s
}

// SetTaskQueue 设置任务队列（用于异步执行）
func (s *ScanTaskScheduler) SetTaskQueue(queue TaskQueue) {
	s.taskQueue = queue
}

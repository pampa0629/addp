package service

import (
	"sync"
	"time"
)

const schedulePollInterval = time.Minute

// ScanTaskScheduler 只负责 owner task 的定时调度并创建持久化 pending execution。
type ScanTaskScheduler struct {
	taskService      *ScanTaskService
	executionService *ScanExecutionService
	claimGate        func() bool

	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

func (s *ScanTaskScheduler) SetClaimGate(claimGate func() bool) {
	s.claimGate = claimGate
}

// NewScanTaskScheduler 创建调度器
func NewScanTaskScheduler(taskService *ScanTaskService, executionService *ScanExecutionService) *ScanTaskScheduler {
	s := &ScanTaskScheduler{
		taskService:      taskService,
		executionService: executionService,
		stopCh:           make(chan struct{}),
	}
	return s
}

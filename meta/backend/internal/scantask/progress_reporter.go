package scantask

import (
	"fmt"
	"sync"
	"time"

	commonModels "github.com/addp/common/models"
)

type ProgressUpdater interface {
	UpdateExecutionProgress(executionID string, tenantID int, fields map[string]interface{})
}

// ExecProgressReporter 将扫描进度写入 common.task_executions。
type ExecProgressReporter struct {
	updater     ProgressUpdater
	executionID string
	tenantID    int

	total         int
	mu            sync.Mutex
	stats         map[string]int64
	lastFlushTime time.Time
	updateCount   int
}

func NewExecProgressReporter(updater ProgressUpdater, executionID string, tenantID int) *ExecProgressReporter {
	return &ExecProgressReporter{
		updater:       updater,
		executionID:   executionID,
		tenantID:      tenantID,
		stats:         make(map[string]int64),
		lastFlushTime: time.Now(),
	}
}

func (r *ExecProgressReporter) SetTotal(total int) {
	r.mu.Lock()
	r.total = total
	r.mu.Unlock()
}

func (r *ExecProgressReporter) Advance(label string, completed, total int, meta map[string]interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.total = total
	for k, v := range meta {
		switch val := v.(type) {
		case int:
			r.stats[k] += int64(val)
		case int64:
			r.stats[k] += val
		case float64:
			r.stats[k] += int64(val)
		}
	}

	r.updateCount++

	shouldFlush := completed == total ||
		time.Since(r.lastFlushTime) >= 5*time.Second ||
		r.updateCount >= 5

	if shouldFlush {
		progress := CalcProgressPercent(completed, total)
		currentStep := fmt.Sprintf("已完成 %d/%d，最新完成: %s", completed, total, label)

		statsClone := make(commonModels.JSONMap)
		for k, v := range r.stats {
			statsClone[k] = v
		}

		r.mu.Unlock()
		r.updater.UpdateExecutionProgress(r.executionID, r.tenantID, map[string]interface{}{
			"progress":     int(progress),
			"current_step": currentStep,
			"metadata":     statsClone,
		})
		r.mu.Lock()
		r.lastFlushTime = time.Now()
		r.updateCount = 0
	}
}

func (r *ExecProgressReporter) Message(message string) {
	r.updater.UpdateExecutionProgress(r.executionID, r.tenantID, map[string]interface{}{
		"current_step": message,
	})
}

func CalcProgressPercent(done, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(done) * 100.0 / float64(total)
}

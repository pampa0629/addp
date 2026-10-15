package scantask

import (
	"fmt"
	"sync"
	"time"

	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
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
	snapshot      *models.ScanProgress
	flushTimer    *time.Timer
	dirty         bool
	closed        bool
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
	if r.snapshot != nil {
		return // File scans report their logical item plan, never the outer target count.
	}

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
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.snapshot != nil {
		return
	}
	r.updater.UpdateExecutionProgress(r.executionID, r.tenantID, map[string]interface{}{
		"current_step": message,
	})
}

func (r *ExecProgressReporter) Report(progress models.ScanProgress) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	force := r.snapshot == nil || r.snapshot.Phase != progress.Phase
	r.snapshot = &progress
	r.dirty = true
	if !force && time.Since(r.lastFlushTime) < time.Second {
		// Flush the most recent change even when the next content read blocks.
		// This timer never manufactures a progress timestamp or repeats a heartbeat.
		if r.flushTimer == nil {
			var timer *time.Timer
			timer = time.AfterFunc(time.Second-time.Since(r.lastFlushTime), func() {
				r.mu.Lock()
				defer r.mu.Unlock()
				if r.flushTimer != timer {
					return // A forced flush already cancelled or replaced this timer.
				}
				r.flushTimer = nil
				if !r.closed {
					r.flushSnapshot()
				}
			})
			r.flushTimer = timer
		}
		return
	}
	r.flushSnapshot()
}

// Close flushes pending observations before the execution is completed.
func (r *ExecProgressReporter) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.flushSnapshot()
	r.closed = true
}

// Caller holds mu. At most one pending timer exists per execution.
func (r *ExecProgressReporter) flushSnapshot() {
	if r.flushTimer != nil {
		r.flushTimer.Stop()
		r.flushTimer = nil
	}
	if !r.dirty || r.snapshot == nil {
		return
	}
	progress := *r.snapshot
	r.updater.UpdateExecutionProgress(r.executionID, r.tenantID, map[string]interface{}{
		"progress":     progress.Percent(),
		"current_step": progress.Phase,
		"metadata":     commonModels.JSONMap{"scan_progress": progress},
	})
	r.lastFlushTime = time.Now()
	r.dirty = false
}

func CalcProgressPercent(done, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(done) * 100.0 / float64(total)
}

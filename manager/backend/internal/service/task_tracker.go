package service

import (
	"sync"
	"time"
)

// TaskStatus 任务状态
type TaskStatus string

const (
	TaskStatusPending TaskStatus = "pending"
	TaskStatusRunning TaskStatus = "running"
	TaskStatusSuccess TaskStatus = "success"
	TaskStatusFailed  TaskStatus = "failed"
)

// TaskInfo 任务信息
type TaskInfo struct {
	TaskID    string
	Status    TaskStatus
	Message   string
	StartTime time.Time
	EndTime   *time.Time
	Progress  int // 0-100
	Result    interface{}
	Error     string
}

// TaskTracker 任务跟踪器（内存缓存）
type TaskTracker struct {
	tasks sync.Map // key: taskID, value: *TaskInfo
}

// NewTaskTracker 创建任务跟踪器
func NewTaskTracker() *TaskTracker {
	tracker := &TaskTracker{}
	// 启动定期清理过期任务（避免内存泄漏）
	go tracker.cleanupExpiredTasks()
	return tracker
}

// CreateTask 创建任务
func (t *TaskTracker) CreateTask(taskID string, message string) *TaskInfo {
	task := &TaskInfo{
		TaskID:    taskID,
		Status:    TaskStatusPending,
		Message:   message,
		StartTime: time.Now(),
		Progress:  0,
	}
	t.tasks.Store(taskID, task)
	return task
}

// GetTask 获取任务信息
func (t *TaskTracker) GetTask(taskID string) (*TaskInfo, bool) {
	val, ok := t.tasks.Load(taskID)
	if !ok {
		return nil, false
	}
	task, ok := val.(*TaskInfo)
	return task, ok
}

// UpdateStatus 更新任务状态
func (t *TaskTracker) UpdateStatus(taskID string, status TaskStatus, message string) {
	val, ok := t.tasks.Load(taskID)
	if !ok {
		return
	}
	task := val.(*TaskInfo)
	task.Status = status
	task.Message = message
	if status == TaskStatusSuccess || status == TaskStatusFailed {
		now := time.Now()
		task.EndTime = &now
	}
	t.tasks.Store(taskID, task)
}

// UpdateProgress 更新任务进度
func (t *TaskTracker) UpdateProgress(taskID string, progress int, message string) {
	val, ok := t.tasks.Load(taskID)
	if !ok {
		return
	}
	task := val.(*TaskInfo)
	task.Progress = progress
	if message != "" {
		task.Message = message
	}
	t.tasks.Store(taskID, task)
}

// SetTaskResult 设置任务结果
func (t *TaskTracker) SetTaskResult(taskID string, result interface{}) {
	val, ok := t.tasks.Load(taskID)
	if !ok {
		return
	}
	task := val.(*TaskInfo)
	task.Result = result
	t.tasks.Store(taskID, task)
}

// SetTaskError 设置任务错误
func (t *TaskTracker) SetTaskError(taskID string, err error) {
	val, ok := t.tasks.Load(taskID)
	if !ok {
		return
	}
	task := val.(*TaskInfo)
	task.Status = TaskStatusFailed
	task.Error = err.Error()
	now := time.Now()
	task.EndTime = &now
	t.tasks.Store(taskID, task)
}

// cleanupExpiredTasks 定期清理过期任务（超过1小时的已完成/失败任务）
func (t *TaskTracker) cleanupExpiredTasks() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		t.tasks.Range(func(key, value interface{}) bool {
			task := value.(*TaskInfo)
			// 清理1小时前完成或失败的任务
			if task.EndTime != nil && now.Sub(*task.EndTime) > time.Hour {
				t.tasks.Delete(key)
			}
			return true
		})
	}
}

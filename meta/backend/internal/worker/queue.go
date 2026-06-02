package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/hibiken/asynq"
)

// TaskType 定义任务类型常量
const (
	TypeScanTask = "meta:scan"
)

// TaskQueue 任务队列管理器
type TaskQueue struct {
	client    *asynq.Client
	inspector *asynq.Inspector
}

// NewTaskQueue 创建任务队列管理器
func NewTaskQueue(redisAddr, redisPassword string) *TaskQueue {
	client := asynq.NewClient(asynq.RedisClientOpt{
		Addr:     redisAddr,
		Password: redisPassword,
	})

	inspector := asynq.NewInspector(asynq.RedisClientOpt{
		Addr:     redisAddr,
		Password: redisPassword,
	})

	return &TaskQueue{
		client:    client,
		inspector: inspector,
	}
}

// ScanTaskPayload 扫描任务载荷
type ScanTaskPayload struct {
	ExecutionID string `json:"execution_id"` // common.task_executions UUID
	TaskID      uint   `json:"task_id"`
	TenantID    uint   `json:"tenant_id"`
}

// EnqueueScanTask 将扫描任务加入队列
func (q *TaskQueue) EnqueueScanTask(ctx context.Context, executionID string, taskID, tenantID uint) error {
	payload, err := json.Marshal(ScanTaskPayload{
		ExecutionID: executionID,
		TaskID:      taskID,
		TenantID:    tenantID,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	task := asynq.NewTask(TypeScanTask, payload, asynq.Queue("meta:default"))

	info, err := q.client.EnqueueContext(ctx, task)
	if err != nil {
		return fmt.Errorf("failed to enqueue task: %w", err)
	}

	log.Printf("✅ Scan task enqueued: id=%s queue=%s executionID=%s", info.ID, info.Queue, executionID)
	return nil
}

// GetQueueStats 获取队列统计信息
func (q *TaskQueue) GetQueueStats(queueName string) (*QueueStats, error) {
	info, err := q.inspector.GetQueueInfo(queueName)
	if err != nil {
		return nil, err
	}

	return &QueueStats{
		Queue:     queueName,
		Active:    info.Active,
		Pending:   info.Pending,
		Scheduled: info.Scheduled,
		Retry:     info.Retry,
		Archived:  info.Archived,
		Completed: info.Completed,
		Processed: info.Processed,
		Failed:    info.Failed,
		Size:      info.Size,
		Latency:   info.Latency,
	}, nil
}

// QueueStats 队列统计信息
type QueueStats struct {
	Queue     string
	Active    int
	Pending   int
	Scheduled int
	Retry     int
	Archived  int
	Completed int
	Processed int
	Failed    int
	Size      int
	Latency   time.Duration
}

// CancelTask 取消队列中的任务
func (q *TaskQueue) CancelTask(taskID string) error {
	// 使用 meta:default 作为默认队列名称
	err := q.inspector.DeleteTask("meta:default", taskID)
	if err != nil {
		return fmt.Errorf("failed to cancel task: %w", err)
	}
	return nil
}

// Close 关闭队列连接
func (q *TaskQueue) Close() error {
	return q.client.Close()
}

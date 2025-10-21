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
	TypeExecuteTask = "transfer:execute"
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

// ExecuteTaskPayload 任务执行载荷
type ExecuteTaskPayload struct {
	TaskID      uint `json:"task_id"`
	ExecutionID uint `json:"execution_id"`
	TenantID    uint `json:"tenant_id"`
}

// EnqueueExecuteTask 将任务加入队列
func (q *TaskQueue) EnqueueExecuteTask(ctx context.Context, taskID, executionID, tenantID uint, opts ...asynq.Option) error {
	payload, err := json.Marshal(ExecuteTaskPayload{
		TaskID:      taskID,
		ExecutionID: executionID,
		TenantID:    tenantID,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	task := asynq.NewTask(TypeExecuteTask, payload, opts...)

	info, err := q.client.EnqueueContext(ctx, task)
	if err != nil {
		return fmt.Errorf("failed to enqueue task: %w", err)
	}

	log.Printf("✅ Task enqueued: id=%s queue=%s", info.ID, info.Queue)
	return nil
}

// EnqueueScheduledTask 调度延迟执行的任务
func (q *TaskQueue) EnqueueScheduledTask(ctx context.Context, taskID, executionID, tenantID uint, executeAt time.Time) error {
	return q.EnqueueExecuteTask(ctx, taskID, executionID, tenantID,
		asynq.ProcessAt(executeAt),
	)
}

// GetQueueStats 获取队列统计信息
func (q *TaskQueue) GetQueueStats(queueName string) (*QueueStats, error) {
	info, err := q.inspector.GetQueueInfo(queueName)
	if err != nil {
		return nil, err
	}

	return &QueueStats{
		Queue:      queueName,
		Active:     info.Active,
		Pending:    info.Pending,
		Scheduled:  info.Scheduled,
		Retry:      info.Retry,
		Archived:   info.Archived,
		Completed:  info.Completed,
		Processed:  info.Processed,
		Failed:     info.Failed,
		Size:       info.Size,
		Latency:    info.Latency,
	}, nil
}

// QueueStats 队列统计信息
type QueueStats struct {
	Queue      string
	Active     int
	Pending    int
	Scheduled  int
	Retry      int
	Archived   int
	Completed  int
	Processed  int
	Failed     int
	Size       int
	Latency    time.Duration
}

// CancelTask 取消队列中的任务
func (q *TaskQueue) CancelTask(taskID string) error {
	err := q.inspector.DeleteTask(asynq.DefaultQueueName, taskID)
	if err != nil {
		return fmt.Errorf("failed to cancel task: %w", err)
	}
	return nil
}

// Close 关闭队列连接
func (q *TaskQueue) Close() error {
	return q.client.Close()
}

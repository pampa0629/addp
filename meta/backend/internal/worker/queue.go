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
	TypeScanTask       = "meta:scan"
	TypePreprocessTask = "meta:preprocess"
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
	RunID    uint `json:"run_id"`
	TaskID   uint `json:"task_id"`
	TenantID uint `json:"tenant_id"`
}

// PreprocessTaskPayload 预处理任务载荷
type PreprocessTaskPayload struct {
	ItemID   uint   `json:"item_id"`
	TenantID uint   `json:"tenant_id"`
	Type     string `json:"type"` // "mvt_tiles", "vector_embedding"
}

// EnqueueScanTask 将扫描任务加入队列
func (q *TaskQueue) EnqueueScanTask(ctx context.Context, runID, taskID, tenantID uint) error {
	// 默认队列为 "meta:default"
	return q.EnqueueScanTaskWithOptions(ctx, runID, taskID, tenantID,
		asynq.Queue("meta:default"),
	)
}

// EnqueueScanTaskWithOptions 将扫描任务加入队列（支持 Asynq 选项）
func (q *TaskQueue) EnqueueScanTaskWithOptions(ctx context.Context, runID, taskID, tenantID uint, opts ...asynq.Option) error {
	payload, err := json.Marshal(ScanTaskPayload{
		RunID:    runID,
		TaskID:   taskID,
		TenantID: tenantID,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	task := asynq.NewTask(TypeScanTask, payload, opts...)

	info, err := q.client.EnqueueContext(ctx, task)
	if err != nil {
		return fmt.Errorf("failed to enqueue task: %w", err)
	}

	log.Printf("✅ Scan task enqueued: id=%s queue=%s", info.ID, info.Queue)
	return nil
}

// EnqueueScheduledScanTask 调度延迟执行的扫描任务
func (q *TaskQueue) EnqueueScheduledScanTask(ctx context.Context, runID, taskID, tenantID uint, executeAt time.Time) error {
	return q.EnqueueScanTaskWithOptions(ctx, runID, taskID, tenantID,
		asynq.Queue("meta:default"),
		asynq.ProcessAt(executeAt),
	)
}

// EnqueueScanTaskWithPriority 根据优先级将扫描任务加入队列
func (q *TaskQueue) EnqueueScanTaskWithPriority(ctx context.Context, runID, taskID, tenantID uint, priority string) error {
	// 根据优先级选择队列
	queueName := "meta:default"
	switch priority {
	case "critical":
		queueName = "meta:critical"
	case "low":
		queueName = "meta:low"
	default:
		queueName = "meta:default"
	}

	return q.EnqueueScanTaskWithOptions(ctx, runID, taskID, tenantID,
		asynq.Queue(queueName),
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

// EnqueuePreprocessTask 将预处理任务加入队列
func (q *TaskQueue) EnqueuePreprocessTask(ctx context.Context, itemID, tenantID uint, preprocessType string) error {
	// 默认队列为 "meta:default"
	return q.EnqueuePreprocessTaskWithOptions(ctx, itemID, tenantID, preprocessType,
		asynq.Queue("meta:default"),
	)
}

// EnqueuePreprocessTaskWithOptions 将预处理任务加入队列（支持 Asynq 选项）
func (q *TaskQueue) EnqueuePreprocessTaskWithOptions(ctx context.Context, itemID, tenantID uint, preprocessType string, opts ...asynq.Option) error {
	payload, err := json.Marshal(PreprocessTaskPayload{
		ItemID:   itemID,
		TenantID: tenantID,
		Type:     preprocessType,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	task := asynq.NewTask(TypePreprocessTask, payload, opts...)

	info, err := q.client.EnqueueContext(ctx, task)
	if err != nil {
		return fmt.Errorf("failed to enqueue task: %w", err)
	}

	log.Printf("✅ Preprocess task enqueued: id=%s queue=%s type=%s item_id=%d",
		info.ID, info.Queue, preprocessType, itemID)
	return nil
}

// EnqueuePreprocessTaskWithPriority 根据优先级将预处理任务加入队列
func (q *TaskQueue) EnqueuePreprocessTaskWithPriority(ctx context.Context, itemID, tenantID uint, preprocessType, priority string) error {
	// 根据优先级选择队列
	queueName := "meta:default"
	switch priority {
	case "critical":
		queueName = "meta:critical"
	case "low":
		queueName = "meta:low"
	default:
		queueName = "meta:default"
	}

	return q.EnqueuePreprocessTaskWithOptions(ctx, itemID, tenantID, preprocessType,
		asynq.Queue(queueName),
	)
}

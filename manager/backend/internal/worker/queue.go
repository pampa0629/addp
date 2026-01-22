package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	commonModels "github.com/addp/common/models"
	"github.com/hibiken/asynq"
)

// TaskType 定义任务类型常量
const (
	TypeQuickViewTask = "manager:quick_view"
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

// QuickViewTaskPayload 快显任务载荷
type QuickViewTaskPayload struct {
	TenantID           uint                                  `json:"tenant_id"`
	EngineID           uint                                  `json:"engine_id"`
	SchemaName         string                                `json:"schema_name"`
	TableName          string                                `json:"table_name"`
	GeomColumn         string                                `json:"geom_column"`
	SRID               int                                   `json:"srid"`
	PrimaryKey         string                                `json:"primary_key"`
	Extent             []float64                             `json:"extent"` // [minLng, minLat, maxLng, maxLat]
	MinZoom            int                                   `json:"min_zoom"`
	MaxZoom            int                                   `json:"max_zoom"`
	Concurrency        int                                   `json:"concurrency"`
	Fingerprint        string                                `json:"fingerprint"`
	OptimizationConfig *commonModels.OptimizationConfig      `json:"optimization_config,omitempty"` // v2.0
}

// EnqueueQuickViewTask 将快显任务加入队列
func (q *TaskQueue) EnqueueQuickViewTask(ctx context.Context, payload QuickViewTaskPayload) error {
	return q.EnqueueQuickViewTaskWithOptions(ctx, payload,
		asynq.Queue("manager:default"),
	)
}

// EnqueueQuickViewTaskWithOptions 将快显任务加入队列（支持 Asynq 选项）
func (q *TaskQueue) EnqueueQuickViewTaskWithOptions(ctx context.Context, payload QuickViewTaskPayload, opts ...asynq.Option) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	task := asynq.NewTask(TypeQuickViewTask, data, opts...)

	info, err := q.client.EnqueueContext(ctx, task)
	if err != nil {
		return fmt.Errorf("failed to enqueue task: %w", err)
	}

	log.Printf("✅ QuickView task enqueued: id=%s queue=%s engine_id=%d table=%s.%s",
		info.ID, info.Queue, payload.EngineID, payload.SchemaName, payload.TableName)
	return nil
}

// EnqueueQuickViewTaskWithPriority 根据优先级将快显任务加入队列
func (q *TaskQueue) EnqueueQuickViewTaskWithPriority(ctx context.Context, payload QuickViewTaskPayload, priority string) error {
	// 根据优先级选择队列
	queueName := "manager:default"
	switch priority {
	case "critical":
		queueName = "manager:critical"
	case "low":
		queueName = "manager:low"
	default:
		queueName = "manager:default"
	}

	return q.EnqueueQuickViewTaskWithOptions(ctx, payload,
		asynq.Queue(queueName),
	)
}

// Close 关闭队列连接
func (q *TaskQueue) Close() error {
	return q.client.Close()
}

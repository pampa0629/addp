package pipeline

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"
)

// ExecutionEngine 执行引擎
// 负责编排 Reader → Transform → Writer 的数据流管道
type ExecutionEngine struct {
	registry         *ConnectorRegistry
	stateManager     *StateManager
	metricsCollector *MetricsCollector
	logger           *slog.Logger
	config           *EngineConfig
}

// EngineConfig 引擎配置
type EngineConfig struct {
	// DefaultBatchSize 默认批大小
	DefaultBatchSize int

	// PollInterval 流式模式下，无数据时的轮询间隔
	PollInterval time.Duration

	// CheckpointInterval Checkpoint 保存间隔（处理多少批次保存一次）
	CheckpointInterval int

	// MaxConcurrency 最大并发数
	MaxConcurrency int

	// EnableAdaptiveBatch 是否启用自适应批大小
	EnableAdaptiveBatch bool
}

// DefaultEngineConfig 默认引擎配置
func DefaultEngineConfig() *EngineConfig {
	return &EngineConfig{
		DefaultBatchSize:    1000,
		PollInterval:        1 * time.Second,
		CheckpointInterval:  10,
		MaxConcurrency:      1,
		EnableAdaptiveBatch: false,
	}
}

// NewExecutionEngine 创建执行引擎
func NewExecutionEngine(
	registry *ConnectorRegistry,
	stateManager *StateManager,
	logger *slog.Logger,
	config *EngineConfig,
) *ExecutionEngine {
	if config == nil {
		config = DefaultEngineConfig()
	}

	return &ExecutionEngine{
		registry:         registry,
		stateManager:     stateManager,
		metricsCollector: NewMetricsCollector(),
		logger:           logger,
		config:           config,
	}
}

// ExecutionTask 执行任务定义
type ExecutionTask struct {
	TaskID      uint
	ExecutionID uint
	SourceConfig  ConnectorConfig
	TargetConfig  ConnectorConfig
	Transforms    []Transform
	Mode          ReaderMode
}

// Execute 执行数据传输任务
func (e *ExecutionEngine) Execute(ctx context.Context, task *ExecutionTask) error {
	e.logger.Info("starting execution",
		"task_id", task.TaskID,
		"execution_id", task.ExecutionID,
		"mode", task.Mode,
	)

	// 1. 创建 Reader
	reader, err := e.registry.NewReader(task.SourceConfig)
	if err != nil {
		return fmt.Errorf("failed to create reader: %w", err)
	}
	defer reader.Close()

	if err := reader.Open(ctx, task.SourceConfig); err != nil {
		return fmt.Errorf("failed to open reader: %w", err)
	}

	// 2. 创建 Writer
	writer, err := e.registry.NewWriter(task.TargetConfig)
	if err != nil {
		return fmt.Errorf("failed to create writer: %w", err)
	}
	defer writer.Close()

	if err := writer.Open(ctx, task.TargetConfig); err != nil {
		return fmt.Errorf("failed to open writer: %w", err)
	}

	// 3. 恢复 Checkpoint（如果有）
	checkpoint, err := e.stateManager.LoadCheckpoint(task.TaskID, task.ExecutionID)
	if err != nil {
		e.logger.Warn("no checkpoint found, starting from beginning", "error", err)
	} else {
		e.logger.Info("resuming from checkpoint", "offset", checkpoint.Offset)
		if err := reader.SeekTo(checkpoint.Offset); err != nil {
			return fmt.Errorf("failed to seek to checkpoint: %w", err)
		}
	}

	// 4. 执行数据流处理
	if err := e.streamProcess(ctx, task, reader, writer); err != nil {
		return fmt.Errorf("stream process failed: %w", err)
	}

	// 5. 刷新写入器缓冲区
	if err := writer.Flush(ctx); err != nil {
		return fmt.Errorf("failed to flush writer: %w", err)
	}

	e.logger.Info("execution completed",
		"task_id", task.TaskID,
		"execution_id", task.ExecutionID,
		"records_processed", e.metricsCollector.GetRecordsRead(),
	)

	return nil
}

// streamProcess 流式处理数据（微批次模式）
func (e *ExecutionEngine) streamProcess(
	ctx context.Context,
	task *ExecutionTask,
	reader Reader,
	writer Writer,
) error {
	var (
		batchCount     int64 = 0
		sequenceNumber int64 = 0
		isStreamMode         = reader.Mode() == ModeStream || reader.Mode() == ModeMicroBatch
	)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// 1. 读取批次数据
		batch, err := reader.Read(ctx)
		if err != nil {
			if err == io.EOF {
				e.logger.Info("reader reached EOF", "batches_processed", batchCount)
				break
			}
			return fmt.Errorf("reader error: %w", err)
		}

		// 2. 处理空批次（流式模式下可能无数据）
		if batch.IsEmpty() {
			if isStreamMode {
				// 流式模式：短暂休眠后继续
				time.Sleep(e.config.PollInterval)
				continue
			} else {
				// 批处理模式：空批次视为结束
				break
			}
		}

		// 设置批次元数据
		batch.SequenceNumber = sequenceNumber
		sequenceNumber++

		// 3. 应用数据转换
		for _, transform := range task.Transforms {
			batch, err = transform.Apply(ctx, batch)
			if err != nil {
				return fmt.Errorf("transform %s failed: %w", transform.Name(), err)
			}
		}

		// 4. 写入目标
		if err := writer.Write(ctx, batch); err != nil {
			return fmt.Errorf("writer error: %w", err)
		}

		// 5. 更新指标
		e.metricsCollector.RecordBatch(batch)
		batchCount++

		// 6. 保存 Checkpoint（定期）
		if batchCount%int64(e.config.CheckpointInterval) == 0 {
			checkpoint := &Checkpoint{
				TaskID:      task.TaskID,
				ExecutionID: task.ExecutionID,
				Offset:      batch.Offset,
				PartitionID: batch.PartitionID,
				State: map[string]interface{}{
					"batch_count":      batchCount,
					"sequence_number":  sequenceNumber,
					"last_timestamp":   batch.Timestamp,
				},
			}
			if err := e.stateManager.SaveCheckpoint(checkpoint); err != nil {
				e.logger.Warn("failed to save checkpoint", "error", err)
			} else {
				e.logger.Debug("checkpoint saved", "offset", checkpoint.Offset, "batch_count", batchCount)
			}
		}

		// 7. 日志记录（每 100 批次）
		if batchCount%100 == 0 {
			e.logger.Info("processing progress",
				"batch_count", batchCount,
				"records_read", e.metricsCollector.GetRecordsRead(),
				"records_written", e.metricsCollector.GetRecordsWritten(),
			)
		}
	}

	return nil
}

// GetMetrics 获取当前执行指标
func (e *ExecutionEngine) GetMetrics() *Metrics {
	return e.metricsCollector.GetMetrics()
}

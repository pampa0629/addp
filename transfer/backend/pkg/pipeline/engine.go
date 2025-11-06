package pipeline

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// LogBuffer 日志缓冲区，用于捕获执行过程中的日志
type LogBuffer struct {
	mu      sync.Mutex
	entries []string
}

// Append 添加一条日志
func (lb *LogBuffer) Append(level, message string) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	entry := fmt.Sprintf("%s [%s] %s", timestamp, strings.ToUpper(level), message)
	lb.entries = append(lb.entries, entry)
}

// String 返回所有日志内容
func (lb *LogBuffer) String() string {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	return strings.Join(lb.entries, "\n")
}

// Clear 清空日志缓冲区
func (lb *LogBuffer) Clear() {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.entries = nil
}

// ExecutionEngine 执行引擎
// 负责编排 Reader → Transform → Writer 的数据流管道
type ExecutionEngine struct {
	registry         *ConnectorRegistry
	stateManager     *StateManager
	metricsCollector *MetricsCollector
	logger           *slog.Logger
	logBuffer        *LogBuffer
	config           *EngineConfig
	progressCallback func(logs string, metrics *Metrics) // 新增：进度回调
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
		logBuffer:        &LogBuffer{entries: make([]string, 0, 100)},
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
	// 清空之前的日志
	e.logBuffer.Clear()

	e.logger.Info("starting execution",
		"task_id", task.TaskID,
		"execution_id", task.ExecutionID,
		"mode", task.Mode,
	)
	e.logBuffer.Append("INFO", fmt.Sprintf("Starting execution for task %d (execution %d) in %s mode",
		task.TaskID, task.ExecutionID, task.Mode))

	// 1. 创建 Reader
	e.logBuffer.Append("INFO", "Creating source reader")
	reader, err := e.registry.NewReader(task.SourceConfig)
	if err != nil {
		e.logBuffer.Append("ERROR", fmt.Sprintf("Failed to create reader: %v", err))
		return fmt.Errorf("failed to create reader: %w", err)
	}
	defer reader.Close()

	e.logBuffer.Append("INFO", "Opening source connection")
	if err := reader.Open(ctx, task.SourceConfig); err != nil {
		e.logBuffer.Append("ERROR", fmt.Sprintf("Failed to open reader: %v", err))
		return fmt.Errorf("failed to open reader: %w", err)
	}
	e.logBuffer.Append("INFO", "Source connection opened successfully")

	// 2. 创建 Writer
	e.logBuffer.Append("INFO", "Creating target writer")
	writer, err := e.registry.NewWriter(task.TargetConfig)
	if err != nil {
		e.logBuffer.Append("ERROR", fmt.Sprintf("Failed to create writer: %v", err))
		return fmt.Errorf("failed to create writer: %w", err)
	}
	defer writer.Close()

	e.logBuffer.Append("INFO", "Opening target connection")
	if err := writer.Open(ctx, task.TargetConfig); err != nil {
		e.logBuffer.Append("ERROR", fmt.Sprintf("Failed to open writer: %v", err))
		return fmt.Errorf("failed to open writer: %w", err)
	}
	e.logBuffer.Append("INFO", "Target connection opened successfully")

	// 3. 恢复 Checkpoint（如果有）
	checkpoint, err := e.stateManager.LoadCheckpoint(task.TaskID, task.ExecutionID)
	if err != nil {
		e.logger.Warn("no checkpoint found, starting from beginning", "error", err)
		e.logBuffer.Append("INFO", "No checkpoint found, starting from beginning")
	} else {
		e.logger.Info("resuming from checkpoint", "offset", checkpoint.Offset)
		e.logBuffer.Append("INFO", fmt.Sprintf("Resuming from checkpoint at offset %d", checkpoint.Offset))
		if err := reader.SeekTo(checkpoint.Offset); err != nil {
			e.logBuffer.Append("ERROR", fmt.Sprintf("Failed to seek to checkpoint: %v", err))
			return fmt.Errorf("failed to seek to checkpoint: %w", err)
		}
	}

	// 调用进度回调（初始状态）
	if e.progressCallback != nil {
		e.progressCallback(e.logBuffer.String(), e.metricsCollector.GetMetrics())
	}

	// 4. 执行数据流处理
	e.logBuffer.Append("INFO", "Starting data stream processing")
	if err := e.streamProcess(ctx, task, reader, writer); err != nil {
		e.logBuffer.Append("ERROR", fmt.Sprintf("Stream processing failed: %v", err))
		return fmt.Errorf("stream process failed: %w", err)
	}

	// 5. 刷新写入器缓冲区
	e.logBuffer.Append("INFO", "Flushing writer buffer")
	if err := writer.Flush(ctx); err != nil {
		e.logBuffer.Append("ERROR", fmt.Sprintf("Failed to flush writer: %v", err))
		return fmt.Errorf("failed to flush writer: %w", err)
	}

	metrics := e.metricsCollector.GetMetrics()
	e.logger.Info("execution completed",
		"task_id", task.TaskID,
		"execution_id", task.ExecutionID,
		"records_processed", e.metricsCollector.GetRecordsRead(),
	)
	e.logBuffer.Append("INFO", fmt.Sprintf("Execution completed successfully - Records read: %d, Records written: %d, Bytes read: %d, Bytes written: %d",
		metrics.RecordsRead, metrics.RecordsWritten, metrics.BytesRead, metrics.BytesWritten))

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
				e.logBuffer.Append("INFO", fmt.Sprintf("Reached end of source data, total batches processed: %d", batchCount))
				break
			}
			e.logBuffer.Append("ERROR", fmt.Sprintf("Reader error at batch %d: %v", batchCount, err))
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
				e.logBuffer.Append("ERROR", fmt.Sprintf("Transform %s failed at batch %d: %v", transform.Name(), batchCount, err))
				return fmt.Errorf("transform %s failed: %w", transform.Name(), err)
			}
		}

		// 4. 写入目标
		if err := writer.Write(ctx, batch); err != nil {
			e.logBuffer.Append("ERROR", fmt.Sprintf("Writer error at batch %d: %v", batchCount, err))
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
			e.logBuffer.Append("INFO", fmt.Sprintf("Progress: Processed %d batches, %d records read, %d records written",
				batchCount, e.metricsCollector.GetRecordsRead(), e.metricsCollector.GetRecordsWritten()))

			// 调用进度回调
			if e.progressCallback != nil {
				e.progressCallback(e.logBuffer.String(), e.metricsCollector.GetMetrics())
			}
		}
	}

	return nil
}

// GetMetrics 获取当前执行指标
func (e *ExecutionEngine) GetMetrics() *Metrics {
	return e.metricsCollector.GetMetrics()
}

// GetLogs 获取执行日志
func (e *ExecutionEngine) GetLogs() string {
	if e.logBuffer == nil {
		return ""
	}
	return e.logBuffer.String()
}

// ClearLogs 清空执行日志
func (e *ExecutionEngine) ClearLogs() {
	if e.logBuffer != nil {
		e.logBuffer.Clear()
	}
}

// SetProgressCallback 设置进度回调函数
func (e *ExecutionEngine) SetProgressCallback(callback func(logs string, metrics *Metrics)) {
	e.progressCallback = callback
}

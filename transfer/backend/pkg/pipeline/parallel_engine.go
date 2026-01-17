package pipeline

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"
)

// ParallelExecutionEngine 并行执行引擎
// 支持多 Reader 并行读取 + 多 Writer 并行写入,充分利用 CPU 和网络
type ParallelExecutionEngine struct {
	*ExecutionEngine
	numReaders int
	numWriters int
}

// ParallelEngineConfig 并行引擎配置
type ParallelEngineConfig struct {
	*EngineConfig
	NumReaders int `json:"num_readers"` // 并行 Reader 数量
	NumWriters int `json:"num_writers"` // 并行 Writer 数量
}

// NewParallelExecutionEngine 创建并行执行引擎
func NewParallelExecutionEngine(
	registry *ConnectorRegistry,
	stateManager *StateManager,
	logger *slog.Logger,
	config *ParallelEngineConfig,
) *ParallelExecutionEngine {
	if config == nil {
		config = &ParallelEngineConfig{
			EngineConfig: DefaultEngineConfig(),
		}
	}

	if config.NumReaders <= 0 {
		config.NumReaders = runtime.NumCPU() / 2
		if config.NumReaders < 1 {
			config.NumReaders = 1
		}
	}

	if config.NumWriters <= 0 {
		config.NumWriters = 4 // PostgreSQL 连接池默认大小
	}

	baseEngine := NewExecutionEngine(registry, stateManager, logger, config.EngineConfig)

	return &ParallelExecutionEngine{
		ExecutionEngine: baseEngine,
		numReaders:      config.NumReaders,
		numWriters:      config.NumWriters,
	}
}

// Execute 并行执行数据传输任务
func (e *ParallelExecutionEngine) Execute(ctx context.Context, task *ExecutionTask) error {
	// 重置指标收集器（避免保留上次执行的数据）
	e.metricsCollector.Reset()

	e.logger.Info("starting parallel execution",
		"task_id", task.TaskID,
		"execution_id", task.ExecutionID,
		"num_readers", e.numReaders,
		"num_writers", e.numWriters,
	)

	// 检查是否支持并行（Reader 需要支持并行模式）
	if !e.supportsParallel(task.SourceConfig.Type) {
		e.logger.Warn("reader does not support parallel mode, falling back to serial execution")
		return e.ExecutionEngine.Execute(ctx, task)
	}

	// 1. 创建 Reader（使用并行 Reader）
	reader, err := e.registry.NewReader(task.SourceConfig)
	if err != nil {
		return fmt.Errorf("failed to create reader: %w", err)
	}
	defer reader.Close()

	if err := reader.Open(ctx, task.SourceConfig); err != nil {
		return fmt.Errorf("failed to open reader: %w", err)
	}

	// 2. 创建 Writer 池（多个独立连接）
	writerPool, err := e.createWriterPool(ctx, task.TargetConfig)
	if err != nil {
		return fmt.Errorf("failed to create writer pool: %w", err)
	}
	defer e.closeWriterPool(writerPool)

	// 3. 执行并行数据流处理
	if err := e.parallelStreamProcess(ctx, task, reader, writerPool); err != nil {
		return fmt.Errorf("parallel stream process failed: %w", err)
	}

	// 4. 刷新所有 Writer 缓冲区
	for _, writer := range writerPool {
		if err := writer.Flush(ctx); err != nil {
			return fmt.Errorf("failed to flush writer: %w", err)
		}
	}

	e.logger.Info("parallel execution completed",
		"task_id", task.TaskID,
		"execution_id", task.ExecutionID,
		"records_processed", e.metricsCollector.GetRecordsRead(),
	)

	return nil
}

// createWriterPool 创建 Writer 连接池
func (e *ParallelExecutionEngine) createWriterPool(ctx context.Context, config ConnectorConfig) ([]Writer, error) {
	pool := make([]Writer, e.numWriters)

	for i := 0; i < e.numWriters; i++ {
		writer, err := e.registry.NewWriter(config)
		if err != nil {
			// 清理已创建的 writer
			for j := 0; j < i; j++ {
				pool[j].Close()
			}
			return nil, fmt.Errorf("failed to create writer %d: %w", i, err)
		}

		if err := writer.Open(ctx, config); err != nil {
			writer.Close()
			for j := 0; j < i; j++ {
				pool[j].Close()
			}
			return nil, fmt.Errorf("failed to open writer %d: %w", i, err)
		}

		pool[i] = writer
	}

	return pool, nil
}

// closeWriterPool 关闭 Writer 连接池
func (e *ParallelExecutionEngine) closeWriterPool(pool []Writer) {
	for _, writer := range pool {
		writer.Close()
	}
}

// parallelStreamProcess 并行流式处理数据
func (e *ParallelExecutionEngine) parallelStreamProcess(
	ctx context.Context,
	task *ExecutionTask,
	reader Reader,
	writerPool []Writer,
) error {
	var (
		wg             sync.WaitGroup
		batchCount     atomic.Int64
		errChan        = make(chan error, e.numWriters)
		dataChan       = make(chan *DataBatch, e.numWriters*2) // 缓冲通道
		_              atomic.Int32                            // placeholder for removed writerIndex
	)

	// 启动 Writer worker 池
	for i := 0; i < e.numWriters; i++ {
		wg.Add(1)
		go func(workerID int, writer Writer) {
			defer wg.Done()

			for {
				select {
				case batch, ok := <-dataChan:
					if !ok {
						return // 通道关闭,退出
					}

					// 应用数据转换
					transformedBatch := batch
					var err error
					for _, transform := range task.Transforms {
						transformedBatch, err = transform.Apply(ctx, transformedBatch)
						if err != nil {
							errChan <- fmt.Errorf("transform %s failed: %w", transform.Name(), err)
							return
						}
					}

					// 写入目标
					if err := writer.Write(ctx, transformedBatch); err != nil {
						errChan <- fmt.Errorf("writer %d error: %w", workerID, err)
						return
					}

					// 更新指标
					e.metricsCollector.RecordBatch(transformedBatch)
					count := batchCount.Add(1)

					// 定期日志
					if count%100 == 0 {
						e.logger.Info("processing progress",
							"batch_count", count,
							"records_read", e.metricsCollector.GetRecordsRead(),
							"records_written", e.metricsCollector.GetRecordsWritten(),
						)
					}

				case <-ctx.Done():
					return
				}
			}
		}(i, writerPool[i])
	}

	// 读取数据并分发到 Writer worker
	go func() {
		defer close(dataChan)

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			// 读取批次数据
			batch, err := reader.Read(ctx)
			if err != nil {
				if err == io.EOF {
					e.logger.Info("reader reached EOF", "batches_processed", batchCount.Load())
					return
				}
				errChan <- fmt.Errorf("reader error: %w", err)
				return
			}

			// 跳过空批次
			if batch.IsEmpty() {
				continue
			}

			// 分发到 dataChan
			select {
			case dataChan <- batch:
			case <-ctx.Done():
				return
			}
		}
	}()

	// 等待所有 writer 完成或错误
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case err := <-errChan:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// supportsParallel 检查 Reader 类型是否支持并行
func (e *ParallelExecutionEngine) supportsParallel(readerType string) bool {
	// 支持并行的 Reader 类型列表
	parallelReaders := map[string]bool{
		"spatialite_parallel": true,
		"jdbc_parallel":       true,
		"file_parallel":       true,
	}
	return parallelReaders[readerType]
}

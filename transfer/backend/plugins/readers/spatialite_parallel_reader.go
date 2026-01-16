package readers

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"runtime"
	"sync"
	"time"

	"github.com/addp/transfer/pkg/pipeline"
	"github.com/addp/transfer/plugins/utils"
	_ "github.com/mattn/go-sqlite3"
)

// SpatiaLiteParallelReader 并行读取 SpatiaLite 数据库
// 通过主键或 ROWID 分区,支持多 goroutine 并发读取千万级数据
type SpatiaLiteParallelReader struct {
	baseReader    *SpatiaLiteReader
	db            *sql.DB
	config        SpatiaLiteParallelConfig
	partitions    []*partition
	currentPartID int
	mu            sync.Mutex
	wg            sync.WaitGroup
	dataChan      chan *pipeline.DataBatch
	errChan       chan error
}

// SpatiaLiteParallelConfig 并行读取配置
type SpatiaLiteParallelConfig struct {
	SpatiaLiteConfig
	NumWorkers    int    `json:"num_workers"`     // 并行工作线程数（默认为 CPU 核心数）
	PartitionKey  string `json:"partition_key"`   // 分区键（默认使用 ROWID）
	PartitionSize int64  `json:"partition_size"`  // 每个分区的记录数（默认 100,000）
}

type partition struct {
	id        int
	startKey  int64
	endKey    int64
	processed bool
}

// NewSpatiaLiteParallelReader 创建并行 Reader
func NewSpatiaLiteParallelReader(config pipeline.ConnectorConfig) (pipeline.Reader, error) {
	var cfg SpatiaLiteParallelConfig
	if err := utils.MapToStruct(config.Config, &cfg); err != nil {
		return nil, fmt.Errorf("invalid parallel config: %w", err)
	}

	// 默认值
	if cfg.NumWorkers <= 0 {
		cfg.NumWorkers = runtime.NumCPU()
	}
	if cfg.PartitionSize <= 0 {
		cfg.PartitionSize = 100000
	}
	if cfg.PartitionKey == "" {
		cfg.PartitionKey = "ROWID"
	}
	if config.BatchSize <= 0 {
		config.BatchSize = 5000 // 并行模式使用更大批次
	}

	// 创建基础 reader 用于 schema 推断
	baseReader, err := NewSpatiaLiteReader(config)
	if err != nil {
		return nil, err
	}

	return &SpatiaLiteParallelReader{
		baseReader: baseReader.(*SpatiaLiteReader),
		config:     cfg,
		dataChan:   make(chan *pipeline.DataBatch, cfg.NumWorkers*2),
		errChan:    make(chan error, cfg.NumWorkers),
	}, nil
}

// Open 打开数据库并计算分区
func (r *SpatiaLiteParallelReader) Open(ctx context.Context, config pipeline.ConnectorConfig) error {
	// 使用基础 reader 打开连接和推断 schema
	if err := r.baseReader.Open(ctx, config); err != nil {
		return err
	}
	r.db = r.baseReader.db

	// 计算分区
	if err := r.calculatePartitions(ctx); err != nil {
		return fmt.Errorf("failed to calculate partitions: %w", err)
	}

	return nil
}

// calculatePartitions 根据表大小和分区键计算分区范围
func (r *SpatiaLiteParallelReader) calculatePartitions(ctx context.Context) error {
	// 获取分区键的最小值和最大值
	query := fmt.Sprintf("SELECT MIN(%s), MAX(%s), COUNT(*) FROM %s",
		r.config.PartitionKey, r.config.PartitionKey, r.config.Table)

	if r.config.WhereClause != "" {
		query += " WHERE " + r.config.WhereClause
	}

	var minKey, maxKey, count sql.NullInt64
	err := r.db.QueryRowContext(ctx, query).Scan(&minKey, &maxKey, &count)
	if err != nil {
		return fmt.Errorf("failed to query partition bounds: %w", err)
	}

	if !minKey.Valid || !maxKey.Valid || !count.Valid || count.Int64 == 0 {
		return fmt.Errorf("table is empty or partition key is invalid")
	}

	// 根据总记录数和分区大小计算分区数
	numPartitions := int((count.Int64 + r.config.PartitionSize - 1) / r.config.PartitionSize)

	// 均匀划分键空间
	keyRange := maxKey.Int64 - minKey.Int64 + 1
	partitionKeySize := (keyRange + int64(numPartitions) - 1) / int64(numPartitions)

	r.partitions = make([]*partition, 0, numPartitions)
	for i := 0; i < numPartitions; i++ {
		start := minKey.Int64 + int64(i)*partitionKeySize
		end := start + partitionKeySize - 1
		if i == numPartitions-1 {
			end = maxKey.Int64 // 最后一个分区到最大值
		}

		r.partitions = append(r.partitions, &partition{
			id:       i,
			startKey: start,
			endKey:   end,
		})
	}

	return nil
}

// Read 并行读取数据批次
func (r *SpatiaLiteParallelReader) Read(ctx context.Context) (*pipeline.DataBatch, error) {
	// 启动并行读取 worker（首次调用时）
	r.mu.Lock()
	if r.currentPartID == 0 && !r.isWorkersStarted() {
		r.startWorkers(ctx)
	}
	r.mu.Unlock()

	// 从通道接收数据批次
	select {
	case batch, ok := <-r.dataChan:
		if !ok {
			return nil, io.EOF
		}
		return batch, nil
	case err := <-r.errChan:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// startWorkers 启动并行读取 worker
func (r *SpatiaLiteParallelReader) startWorkers(ctx context.Context) {
	for i := 0; i < r.config.NumWorkers; i++ {
		r.wg.Add(1)
		go r.worker(ctx, i)
	}

	// 启动协调 goroutine 等待所有 worker 完成
	go func() {
		r.wg.Wait()
		close(r.dataChan)
		close(r.errChan)
	}()
}

// worker 并行读取 worker 函数
func (r *SpatiaLiteParallelReader) worker(ctx context.Context, workerID int) {
	defer r.wg.Done()

	// 每个 worker 创建独立的数据库连接（SQLite 读并发安全）
	db, err := sql.Open("sqlite3", r.config.FullName)
	if err != nil {
		r.errChan <- fmt.Errorf("worker %d: failed to open db: %w", workerID, err)
		return
	}
	defer db.Close()

	// 加载 SpatiaLite 扩展
	_, _ = db.ExecContext(ctx, "SELECT load_extension('mod_spatialite')")

	for {
		// 获取下一个待处理的分区
		part := r.getNextPartition()
		if part == nil {
			return // 所有分区已处理完成
		}

		// 读取分区数据
		if err := r.readPartition(ctx, db, part, workerID); err != nil {
			r.errChan <- fmt.Errorf("worker %d partition %d: %w", workerID, part.id, err)
			return
		}
	}
}

// getNextPartition 线程安全地获取下一个待处理分区
func (r *SpatiaLiteParallelReader) getNextPartition() *partition {
	r.mu.Lock()
	defer r.mu.Unlock()

	for r.currentPartID < len(r.partitions) {
		part := r.partitions[r.currentPartID]
		r.currentPartID++
		if !part.processed {
			return part
		}
	}
	return nil
}

// readPartition 读取单个分区的数据
func (r *SpatiaLiteParallelReader) readPartition(ctx context.Context, db *sql.DB, part *partition, workerID int) error {
	// 构建分区查询
	query := r.buildPartitionQuery(part)

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query partition: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return err
	}

	// 批量读取
	batchSize := r.baseReader.batchSize
	var batchRows []map[string]interface{}
	offset := part.startKey

	for rows.Next() {
		row, err := r.scanRowFromPartition(rows, columns)
		if err != nil {
			return err
		}
		batchRows = append(batchRows, row)

		// 达到批次大小时发送
		if len(batchRows) >= batchSize {
			batch := &pipeline.DataBatch{
				Rows:      batchRows,
				Schema:    r.baseReader.schema,
				Offset:    offset,
				Timestamp: time.Now(),
				Metadata: map[string]interface{}{
					"partition_id": part.id,
					"worker_id":    workerID,
				},
			}

			select {
			case r.dataChan <- batch:
			case <-ctx.Done():
				return ctx.Err()
			}

			batchRows = make([]map[string]interface{}, 0, batchSize)
			offset += int64(batchSize)
		}
	}

	// 发送剩余数据
	if len(batchRows) > 0 {
		batch := &pipeline.DataBatch{
			Rows:      batchRows,
			Schema:    r.baseReader.schema,
			Offset:    offset,
			Timestamp: time.Now(),
			Metadata: map[string]interface{}{
				"partition_id": part.id,
				"worker_id":    workerID,
			},
		}

		select {
		case r.dataChan <- batch:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	part.processed = true
	return nil
}

// buildPartitionQuery 构建分区查询 SQL
func (r *SpatiaLiteParallelReader) buildPartitionQuery(part *partition) string {
	baseQuery := r.baseReader.baseQuery

	// 添加分区范围过滤
	partitionFilter := fmt.Sprintf("%s BETWEEN %d AND %d", r.config.PartitionKey, part.startKey, part.endKey)

	if r.config.WhereClause != "" {
		baseQuery += " AND " + partitionFilter
	} else {
		baseQuery += " WHERE " + partitionFilter
	}

	// 添加排序以保证数据有序性（可选）
	baseQuery += fmt.Sprintf(" ORDER BY %s", r.config.PartitionKey)

	return baseQuery
}

// scanRowFromPartition 扫描分区查询的行
func (r *SpatiaLiteParallelReader) scanRowFromPartition(rows *sql.Rows, columns []string) (map[string]interface{}, error) {
	values := make([]interface{}, len(columns))
	ptrs := make([]interface{}, len(columns))
	for i := range values {
		ptrs[i] = &values[i]
	}

	if err := rows.Scan(ptrs...); err != nil {
		return nil, err
	}

	row := make(map[string]interface{}, len(columns))
	for i, col := range columns {
		v := values[i]
		if b, ok := v.([]byte); ok {
			row[col] = b
		} else {
			row[col] = v
		}
	}
	return row, nil
}

func (r *SpatiaLiteParallelReader) isWorkersStarted() bool {
	return r.currentPartID > 0 || len(r.partitions) == 0
}

// Schema 返回 schema
func (r *SpatiaLiteParallelReader) Schema() (*pipeline.Schema, error) {
	return r.baseReader.Schema()
}

// SeekTo 不支持（并行模式下不支持 seek）
func (r *SpatiaLiteParallelReader) SeekTo(offset int64) error {
	return fmt.Errorf("SeekTo not supported in parallel mode")
}

// Close 关闭所有连接
func (r *SpatiaLiteParallelReader) Close() error {
	return r.baseReader.Close()
}

// Mode 返回微批次模式
func (r *SpatiaLiteParallelReader) Mode() pipeline.ReaderMode {
	return pipeline.ModeMicroBatch
}

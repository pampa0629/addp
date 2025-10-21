package pipeline

import (
	"context"
	"io"
	"time"
)

// Reader 数据读取器接口 - 支持批处理和流式读取
// 实现微批次（micro-batching）模式，统一批处理和流处理
type Reader interface {
	// Open 打开数据源连接
	Open(ctx context.Context, config ConnectorConfig) error

	// Read 读取一批数据（微批次模式）
	// 返回 nil 表示数据读取完成（批处理模式）
	// 流式模式下会持续返回数据或空批次
	Read(ctx context.Context) (*DataBatch, error)

	// Schema 返回数据 schema（可选，用于类型推断）
	Schema() (*Schema, error)

	// SeekTo 跳转到指定偏移量（用于断点续传）
	SeekTo(offset int64) error

	// Close 关闭连接
	Close() error

	// Mode 返回读取器模式（batch 或 stream）
	Mode() ReaderMode
}

// Writer 数据写入器接口
type Writer interface {
	// Open 打开目标连接
	Open(ctx context.Context, config ConnectorConfig) error

	// Write 写入一批数据
	Write(ctx context.Context, batch *DataBatch) error

	// Flush 刷新缓冲区，确保数据持久化
	Flush(ctx context.Context) error

	// Close 关闭连接
	Close() error
}

// Transform 数据转换接口
type Transform interface {
	// Apply 对数据批次应用转换
	Apply(ctx context.Context, batch *DataBatch) (*DataBatch, error)

	// Name 返回转换器名称
	Name() string
}

// ReaderMode 读取器模式
type ReaderMode string

const (
	// ModeBatch 批处理模式：读取完所有数据后返回 io.EOF
	ModeBatch ReaderMode = "batch"

	// ModeStream 流式模式：持续监听数据源，不会返回 io.EOF
	ModeStream ReaderMode = "stream"

	// ModeMicroBatch 微批次模式：以小批量方式处理流式数据
	ModeMicroBatch ReaderMode = "micro-batch"
)

// DataBatch 数据批次
type DataBatch struct {
	// Rows 行数据，每行是一个 map
	Rows []map[string]interface{}

	// Schema 数据 schema
	Schema *Schema

	// Metadata 批次元数据
	Metadata map[string]interface{}

	// Offset 当前批次的偏移量（用于 checkpoint）
	Offset int64

	// PartitionID 分区 ID（用于 Kafka 等分区数据源）
	PartitionID string

	// Timestamp 批次时间戳
	Timestamp time.Time

	// SequenceNumber 批次序列号（递增）
	SequenceNumber int64
}

// Schema 数据 schema 定义
type Schema struct {
	// Fields 字段列表
	Fields []Field

	// Metadata schema 元数据
	Metadata map[string]interface{}
}

// Field 字段定义
type Field struct {
	// Name 字段名
	Name string

	// Type 字段类型（string, int, float, bool, datetime, json 等）
	Type string

	// Nullable 是否可为空
	Nullable bool

	// Description 字段描述
	Description string

	// DefaultValue 默认值
	DefaultValue interface{}

	// Format 格式说明（如日期格式 "2006-01-02 15:04:05"）
	Format string
}

// ConnectorConfig 连接器配置
type ConnectorConfig struct {
	// Type 连接器类型（postgresql, mysql, s3, kafka, file 等）
	Type string

	// Config 连接配置（JSON 格式）
	Config map[string]interface{}

	// BatchSize 批大小（Reader 专用）
	BatchSize int

	// Timeout 超时时间
	Timeout time.Duration

	// RetryPolicy 重试策略
	RetryPolicy *RetryPolicy
}

// RetryPolicy 重试策略
type RetryPolicy struct {
	// MaxRetries 最大重试次数
	MaxRetries int

	// BackoffPolicy 退避策略（exponential, linear, constant）
	BackoffPolicy string

	// InitialDelay 初始延迟
	InitialDelay time.Duration

	// MaxDelay 最大延迟
	MaxDelay time.Duration
}

// ReaderFactory Reader 工厂函数
type ReaderFactory func(config ConnectorConfig) (Reader, error)

// WriterFactory Writer 工厂函数
type WriterFactory func(config ConnectorConfig) (Writer, error)

// IsEmpty 判断批次是否为空
func (b *DataBatch) IsEmpty() bool {
	return len(b.Rows) == 0
}

// RowCount 返回批次行数
func (b *DataBatch) RowCount() int {
	return len(b.Rows)
}

// Clone 克隆批次（浅拷贝）
func (b *DataBatch) Clone() *DataBatch {
	return &DataBatch{
		Rows:           b.Rows,
		Schema:         b.Schema,
		Metadata:       b.Metadata,
		Offset:         b.Offset,
		PartitionID:    b.PartitionID,
		Timestamp:      b.Timestamp,
		SequenceNumber: b.SequenceNumber,
	}
}

// Validate 校验 schema 合法性
func (s *Schema) Validate() error {
	if len(s.Fields) == 0 {
		return io.ErrUnexpectedEOF
	}
	for _, field := range s.Fields {
		if field.Name == "" {
			return io.ErrUnexpectedEOF
		}
	}
	return nil
}

// GetField 根据名称获取字段
func (s *Schema) GetField(name string) *Field {
	for i := range s.Fields {
		if s.Fields[i].Name == name {
			return &s.Fields[i]
		}
	}
	return nil
}

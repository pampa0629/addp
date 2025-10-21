package pipeline

import (
	"sync"
	"sync/atomic"
	"time"
)

// MetricsCollector 指标收集器
// 收集任务执行过程中的各项指标
type MetricsCollector struct {
	recordsRead    int64
	recordsWritten int64
	bytesRead      int64
	bytesWritten   int64
	errorCount     int64
	batchCount     int64
	startTime      time.Time
	lastBatchTime  time.Time
	mu             sync.RWMutex
}

// Metrics 指标数据
type Metrics struct {
	RecordsRead     int64         `json:"records_read"`
	RecordsWritten  int64         `json:"records_written"`
	BytesRead       int64         `json:"bytes_read"`
	BytesWritten    int64         `json:"bytes_written"`
	ErrorCount      int64         `json:"error_count"`
	BatchCount      int64         `json:"batch_count"`
	Duration        time.Duration `json:"duration"`
	AvgLatency      time.Duration `json:"avg_latency"`
	CurrentQPS      float64       `json:"current_qps"`
	Progress        float64       `json:"progress"`
	LastBatchTime   time.Time     `json:"last_batch_time"`
}

// NewMetricsCollector 创建指标收集器
func NewMetricsCollector() *MetricsCollector {
	now := time.Now()
	return &MetricsCollector{
		startTime:     now,
		lastBatchTime: now,
	}
}

// RecordBatch 记录批次处理指标
func (m *MetricsCollector) RecordBatch(batch *DataBatch) {
	rowCount := int64(batch.RowCount())

	atomic.AddInt64(&m.recordsRead, rowCount)
	atomic.AddInt64(&m.recordsWritten, rowCount)
	atomic.AddInt64(&m.batchCount, 1)

	m.mu.Lock()
	m.lastBatchTime = time.Now()
	m.mu.Unlock()
}

// RecordBytes 记录字节数
func (m *MetricsCollector) RecordBytes(read, written int64) {
	atomic.AddInt64(&m.bytesRead, read)
	atomic.AddInt64(&m.bytesWritten, written)
}

// RecordError 记录错误
func (m *MetricsCollector) RecordError() {
	atomic.AddInt64(&m.errorCount, 1)
}

// GetRecordsRead 获取已读取记录数
func (m *MetricsCollector) GetRecordsRead() int64 {
	return atomic.LoadInt64(&m.recordsRead)
}

// GetRecordsWritten 获取已写入记录数
func (m *MetricsCollector) GetRecordsWritten() int64 {
	return atomic.LoadInt64(&m.recordsWritten)
}

// GetBytesRead 获取已读取字节数
func (m *MetricsCollector) GetBytesRead() int64 {
	return atomic.LoadInt64(&m.bytesRead)
}

// GetBytesWritten 获取已写入字节数
func (m *MetricsCollector) GetBytesWritten() int64 {
	return atomic.LoadInt64(&m.bytesWritten)
}

// GetErrorCount 获取错误计数
func (m *MetricsCollector) GetErrorCount() int64 {
	return atomic.LoadInt64(&m.errorCount)
}

// GetBatchCount 获取批次计数
func (m *MetricsCollector) GetBatchCount() int64 {
	return atomic.LoadInt64(&m.batchCount)
}

// GetMetrics 获取所有指标
func (m *MetricsCollector) GetMetrics() *Metrics {
	m.mu.RLock()
	lastBatchTime := m.lastBatchTime
	m.mu.RUnlock()

	recordsRead := m.GetRecordsRead()
	recordsWritten := m.GetRecordsWritten()
	batchCount := m.GetBatchCount()

	duration := time.Since(m.startTime)

	var avgLatency time.Duration
	if batchCount > 0 {
		avgLatency = duration / time.Duration(batchCount)
	}

	var currentQPS float64
	if duration.Seconds() > 0 {
		currentQPS = float64(recordsRead) / duration.Seconds()
	}

	return &Metrics{
		RecordsRead:    recordsRead,
		RecordsWritten: recordsWritten,
		BytesRead:      m.GetBytesRead(),
		BytesWritten:   m.GetBytesWritten(),
		ErrorCount:     m.GetErrorCount(),
		BatchCount:     batchCount,
		Duration:       duration,
		AvgLatency:     avgLatency,
		CurrentQPS:     currentQPS,
		Progress:       0, // 需要外部设置
		LastBatchTime:  lastBatchTime,
	}
}

// Reset 重置所有指标
func (m *MetricsCollector) Reset() {
	atomic.StoreInt64(&m.recordsRead, 0)
	atomic.StoreInt64(&m.recordsWritten, 0)
	atomic.StoreInt64(&m.bytesRead, 0)
	atomic.StoreInt64(&m.bytesWritten, 0)
	atomic.StoreInt64(&m.errorCount, 0)
	atomic.StoreInt64(&m.batchCount, 0)

	now := time.Now()
	m.mu.Lock()
	m.startTime = now
	m.lastBatchTime = now
	m.mu.Unlock()
}

// GetDuration 获取执行时长
func (m *MetricsCollector) GetDuration() time.Duration {
	return time.Since(m.startTime)
}

// GetQPS 获取当前 QPS
func (m *MetricsCollector) GetQPS() float64 {
	duration := m.GetDuration()
	if duration.Seconds() == 0 {
		return 0
	}
	return float64(m.GetRecordsRead()) / duration.Seconds()
}

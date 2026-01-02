# Transfer 模块流批一体化架构设计与实现

## 一、设计概述

Transfer 模块是 ADDP（全域数据平台）的数据传输服务，采用**流批一体化（Unified Batch & Streaming）**架构设计，实现数据库与文件系统之间的灵活数据传输。

### 核心设计理念
1.1 统一抽象层
Reader/Writer 接口统一：批处理和流处理使用相同的接口
Pipeline 架构：Source → Transform → Sink 的管道模式
可插拔连接器：复用 Meta 模块的插件化扫描器（已实现 S3、PostgreSQL、MySQL 等）
1.2 流批融合策略
微批处理（Micro-batching）：流式传输以小批量方式处理，降低延迟同时保持吞吐量
自适应批大小：根据数据速率和系统负载动态调整批大小
统一状态管理：使用 Checkpoint 机制支持断点续传和增量同步
1.3 架构优势
✅ 复用现有能力：Meta 模块的扫描器、Manager 的预览、System 的资源管理
✅ 插件式扩展：继承 Meta 的 MetadataExtractor 模式
✅ 任务队列解耦：API 服务 + Worker 进程分离（参考 Meta 的 ScanTaskService）
✅ 血缘追踪集成：传输完成后自动写入 Meta 的 meta_change_log
二、系统架构设计
2.1 整体架构图
┌─────────────────────────────────────────────────────────────────┐
│                     Transfer Frontend (Vue)                      │
│  任务配置 → 映射配置 → 调度设置 → 监控面板                         │
└───────────────────────────────┬─────────────────────────────────┘
                                │ HTTP API
┌───────────────────────────────▼─────────────────────────────────┐
│                  Transfer API Server (Gin)                       │
│  ┌─────────────┐  ┌──────────────┐  ┌──────────────────┐       │
│  │ TaskHandler │  │ ExecHandler  │  │ MonitorHandler   │       │
│  └──────┬──────┘  └──────┬───────┘  └────────┬─────────┘       │
│         │                │                    │                  │
│  ┌──────▼────────────────▼────────────────────▼─────────┐       │
│  │           TaskService (业务编排层)                     │       │
│  │  - 任务 CRUD                                           │       │
│  │  - 任务提交到队列 (Redis/Asynq)                        │       │
│  │  - 定时调度 (cron, 复用 Meta 的 robfig/cron)          │       │
│  └─────────────────────────┬───────────────────────────┘       │
└────────────────────────────┼──────────────────────────────────┘
                             │ Enqueue
┌────────────────────────────▼──────────────────────────────────┐
│                  Task Queue (Redis + Asynq)                    │
│  - 任务持久化、优先级、重试                                      │
└────────────────────────────┬──────────────────────────────────┘
                             │ Dequeue
┌────────────────────────────▼──────────────────────────────────┐
│              Transfer Worker (多进程/多线程)                    │
│  ┌───────────────────────────────────────────────────────┐    │
│  │           Execution Engine (核心执行引擎)              │    │
│  │  ┌──────────┐    ┌───────────┐    ┌──────────┐      │    │
│  │  │  Reader  │───→│ Transform │───→│  Writer  │      │    │
│  │  │ (Source) │    │ Pipeline  │    │  (Sink)  │      │    │
│  │  └────┬─────┘    └─────┬─────┘    └─────┬────┘      │    │
│  │       │                 │                 │            │    │
│  │  ┌────▼─────────────────▼─────────────────▼────┐     │    │
│  │  │       State Manager (状态与 Checkpoint)      │     │    │
│  │  └──────────────────────────────────────────────┘     │    │
│  └───────────────────────────────────────────────────────┘    │
│                                                                 │
│  连接器层 (Connector Layer - 插件化)                            │
│  ┌─────────┬─────────┬─────────┬─────────┬─────────┐          │
│  │  JDBC   │   S3    │  Kafka  │  File   │  HTTP   │          │
│  │ Reader/ │ Reader/ │ Reader/ │ Reader/ │ Reader/ │          │
│  │ Writer  │ Writer  │ Writer  │ Writer  │ Writer  │          │
│  └─────────┴─────────┴─────────┴─────────┴─────────┘          │
└─────────────────────────────────────────────────────────────┘
           │                                        │
           ▼                                        ▼
    ┌──────────────┐                      ┌──────────────┐
    │  Data Source │                      │  Data Sink   │
    │ (DB/File/MQ) │                      │ (DB/File/MQ) │
    └──────────────┘                      └──────────────┘
2.2 核心组件说明
A. API Server (transfer/backend/cmd/server/)
职责：RESTful API、任务管理、调度配置
技术栈：Gin + GORM + Redis
端口：8083
关键服务：
TaskService：任务 CRUD、提交任务到队列
ExecutionService：查询执行历史、重试失败任务
SchedulerService：定时任务管理（cron）
B. Worker 进程 (transfer/backend/cmd/worker/)
职责：从队列拉取任务并执行
启动模式：独立进程，可多实例部署
并发控制：每个 Worker 配置并发数（默认 10）
核心组件：
ExecutionEngine：任务编排和执行
ConnectorRegistry：连接器注册与管理
StateManager：Checkpoint 状态管理
C. Execution Engine (执行引擎)
type ExecutionEngine struct {
    connectorRegistry *ConnectorRegistry
    stateManager      *StateManager
    metricsCollector  *MetricsCollector
}

// 执行流程
func (e *ExecutionEngine) Execute(ctx context.Context, task *Task) error {
    // 1. 解析任务配置
    config := task.Config
    
    // 2. 创建 Reader (Source)
    reader := e.connectorRegistry.NewReader(config.SourceType, config.SourceConfig)
    
    // 3. 创建 Writer (Sink)
    writer := e.connectorRegistry.NewWriter(config.TargetType, config.TargetConfig)
    
    // 4. 恢复 Checkpoint (如果有)
    checkpoint := e.stateManager.LoadCheckpoint(task.ID)
    
    // 5. 流式处理 (微批次)
    return e.streamProcess(ctx, reader, writer, config.Transform, checkpoint)
}
三、数据流管道设计
3.1 统一接口定义
// Reader 接口 - 支持批处理和流式读取
type Reader interface {
    // Open 打开数据源连接
    Open(ctx context.Context, config ConnectorConfig) error
    
    // Read 读取一批数据 (微批次模式)
    // 返回 nil 表示数据读取完成
    Read(ctx context.Context) (*DataBatch, error)
    
    // Schema 返回数据 schema (可选，用于类型推断)
    Schema() (*Schema, error)
    
    // Close 关闭连接
    Close() error
}

// Writer 接口
type Writer interface {
    Open(ctx context.Context, config ConnectorConfig) error
    
    // Write 写入一批数据
    Write(ctx context.Context, batch *DataBatch) error
    
    // Flush 刷新缓冲区
    Flush(ctx context.Context) error
    
    Close() error
}

// DataBatch 数据批次
type DataBatch struct {
    Rows      []map[string]interface{} // 行数据
    Schema    *Schema                  // schema 信息
    Metadata  map[string]interface{}   // 批次元数据
    Offset    int64                    // 偏移量（用于 checkpoint）
    Timestamp time.Time                // 批次时间戳
}

// Transform 数据转换接口
type Transform interface {
    Apply(ctx context.Context, batch *DataBatch) (*DataBatch, error)
}
3.2 微批处理流程
func (e *ExecutionEngine) streamProcess(
    ctx context.Context,
    reader Reader,
    writer Writer,
    transforms []Transform,
    checkpoint *Checkpoint,
) error {
    // 从 checkpoint 恢复位置
    if checkpoint != nil {
        reader.SeekTo(checkpoint.Offset)
    }
    
    batchSize := e.config.BatchSize // 默认 1000
    
    for {
        // 1. 读取微批次
        batch, err := reader.Read(ctx)
        if err == io.EOF {
            break // 批处理模式：数据读完
        }
        if err != nil {
            return err
        }
        
        // 2. 应用转换
        for _, transform := range transforms {
            batch, err = transform.Apply(ctx, batch)
            if err != nil {
                return err
            }
        }
        
        // 3. 写入目标
        if err := writer.Write(ctx, batch); err != nil {
            return err
        }
        
        // 4. 保存 Checkpoint
        checkpoint := &Checkpoint{
            TaskID:    task.ID,
            Offset:    batch.Offset,
            Timestamp: batch.Timestamp,
        }
        if err := e.stateManager.SaveCheckpoint(checkpoint); err != nil {
            log.Warn("checkpoint save failed", err)
        }
        
        // 5. 更新指标
        e.metricsCollector.RecordBatch(batch)
        
        // 6. 流式模式：持续监听，批处理模式：继续下一批
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
            // 自适应休眠（流式模式下，如果无数据暂停等待）
            if e.isStreamMode && len(batch.Rows) == 0 {
                time.Sleep(e.config.PollInterval)
            }
        }
    }
    
    return writer.Flush(ctx)
}
四、连接器实现（复用 + 扩展）
4.1 复用 Meta 模块能力
Meta 模块已实现的扫描器可直接改造为 Reader：
S3Scanner → S3Reader (读取对象存储文件)
PostgreSQLScanner → JDBCReader (读取数据库表)
MetadataExtractor → 数据解析器（CSV、JSON、Parquet 等）
改造示例：
// 复用 Meta 的 S3Scanner
type S3Reader struct {
    scanner *scanner.S3Scanner  // 复用 Meta 的扫描器
    bucket  string
    prefix  string
    offset  int64
}

func (r *S3Reader) Read(ctx context.Context) (*DataBatch, error) {
    // 列出对象
    objects := r.scanner.ListObjects(r.bucket, r.prefix, r.offset, batchSize)
    
    var rows []map[string]interface{}
    for _, obj := range objects {
        // 使用 Meta 的 extractor 解析内容
        metadata, err := r.scanner.ExtractMetadata(ctx, obj)
        rows = append(rows, metadata.ToMap())
    }
    
    return &DataBatch{Rows: rows, Offset: r.offset + len(objects)}, nil
}
4.2 新增 Writer 实现
// JDBC Writer (PostgreSQL, MySQL)
type JDBCWriter struct {
    db         *sql.DB
    table      string
    batchStmt  *sql.Stmt
    buffer     []*DataRow
    bufferSize int
}

func (w *JDBCWriter) Write(ctx context.Context, batch *DataBatch) error {
    // 批量插入优化
    return w.batchInsert(ctx, batch.Rows)
}

// S3 Writer (MinIO, OSS)
type S3Writer struct {
    client *minio.Client // 复用 Manager 的 S3 客户端
    bucket string
    prefix string
}

func (w *S3Writer) Write(ctx context.Context, batch *DataBatch) error {
    // 将数据序列化为 JSON/CSV/Parquet
    data := w.serialize(batch)
    
    // 上传到 S3
    return w.client.PutObject(ctx, w.bucket, key, data, ...)
}

// Kafka Writer (流式输出)
type KafkaWriter struct {
    producer sarama.SyncProducer
    topic    string
}

func (w *KafkaWriter) Write(ctx context.Context, batch *DataBatch) error {
    for _, row := range batch.Rows {
        msg := &sarama.ProducerMessage{
            Topic: w.topic,
            Value: sarama.ByteEncoder(json.Marshal(row)),
        }
        _, _, err := w.producer.SendMessage(msg)
        if err != nil {
            return err
        }
    }
    return nil
}
4.3 连接器注册表
type ConnectorRegistry struct {
    readers map[string]ReaderFactory
    writers map[string]WriterFactory
}

func NewConnectorRegistry() *ConnectorRegistry {
    r := &ConnectorRegistry{
        readers: make(map[string]ReaderFactory),
        writers: make(map[string]WriterFactory),
    }
    
    // 注册内置连接器
    r.RegisterReader("postgresql", NewJDBCReader)
    r.RegisterReader("mysql", NewJDBCReader)
    r.RegisterReader("s3", NewS3Reader)
    r.RegisterReader("kafka", NewKafkaReader)
    
    r.RegisterWriter("postgresql", NewJDBCWriter)
    r.RegisterWriter("s3", NewS3Writer)
    r.RegisterWriter("kafka", NewKafkaWriter)
    
    return r
}
五、状态管理与容错
5.1 Checkpoint 机制
type Checkpoint struct {
    ID           uint
    TaskID       uint
    ExecutionID  uint
    Offset       int64                    // 当前处理偏移量
    PartitionID  string                   // 分区 ID（Kafka 等）
    State        map[string]interface{}   // 自定义状态
    CreatedAt    time.Time
}

type StateManager struct {
    db *gorm.DB
}

func (s *StateManager) SaveCheckpoint(cp *Checkpoint) error {
    return s.db.Create(cp).Error
}

func (s *StateManager) LoadCheckpoint(taskID, executionID uint) (*Checkpoint, error) {
    var cp Checkpoint
    err := s.db.Where("task_id = ? AND execution_id = ?", taskID, executionID).
        Order("created_at DESC").
        First(&cp).Error
    return &cp, err
}
5.2 断点续传流程
任务启动 → 查询最新 Checkpoint → Reader.SeekTo(offset) → 继续处理
5.3 失败重试策略
type RetryPolicy struct {
    MaxRetries    int
    BackoffPolicy string // "exponential", "linear", "constant"
    InitialDelay  time.Duration
}

// 在 Asynq 中配置
asynq.Queue("transfer").
    MaxRetry(3).
    Timeout(1 * time.Hour).
    Deadlines(...)
六、数据转换与映射
6.1 Transform Pipeline
type TransformPipeline struct {
    transforms []Transform
}

// 内置转换器
type FieldMappingTransform struct {
    mappings map[string]string // source_field -> target_field
}

type TypeConversionTransform struct {
    conversions map[string]string // field -> target_type
}

type FilterTransform struct {
    condition string // SQL-like 表达式
}

type ScriptTransform struct {
    script string // Lua/JavaScript 脚本
}
6.2 字段映射配置
{
  "mappings": [
    {"source": "user_id", "target": "id", "type": "int"},
    {"source": "user_name", "target": "name", "type": "string"},
    {"source": "created_time", "target": "created_at", "type": "datetime", "format": "2006-01-02 15:04:05"}
  ],
  "filters": [
    {"field": "status", "op": "eq", "value": "active"}
  ]
}
七、监控与可观测性
7.1 指标收集
type Metrics struct {
    TaskID          uint
    ExecutionID     uint
    RecordsRead     int64
    RecordsWritten  int64
    BytesRead       int64
    BytesWritten    int64
    ErrorCount      int64
    AvgLatency      time.Duration
    CurrentQPS      float64
    Progress        float64 // 0-100
    LastCheckpoint  time.Time
}

// 实时更新到 transfer.task_executions 表
7.2 前端监控面板
任务列表：状态、进度、最后执行时间
执行详情：吞吐量曲线、错误日志、Checkpoint 历史
实时监控：当前运行任务的 QPS、延迟、内存使用
八、与其他模块集成
8.1 与 System 模块集成
引擎管理：从 system.engines 获取数据源连接信息（复用 common/models.BuildConnectionString）
用户认证：JWT token 验证
8.2 与 Manager 模块集成
文件预览：传输完成后可在 Manager 中预览结果文件
权限控制：读取 manager.data_source_permissions 校验用户权限
8.3 与 Meta 模块集成
元数据感知：传输前读取 metadata.meta_item 获取 schema
自动扫描触发：传输完成后触发 Meta 扫描新数据，更新元数据
九、数据库 Schema 增强
9.1 扩展 transfer.tasks 表
ALTER TABLE transfer.tasks 
ADD COLUMN mode VARCHAR(20) DEFAULT 'batch',  -- 'batch', 'stream', 'micro-batch'
ADD COLUMN batch_size INT DEFAULT 1000,
ADD COLUMN max_parallelism INT DEFAULT 1,
ADD COLUMN retry_policy JSONB,
ADD COLUMN last_execution_id INT;

ALTER TABLE transfer.task_executions
ADD COLUMN checkpoint_offset BIGINT DEFAULT 0,
ADD COLUMN checkpoint_state JSONB;

CREATE TABLE transfer.checkpoints (
    id SERIAL PRIMARY KEY,
    task_id INT REFERENCES transfer.tasks(id) ON DELETE CASCADE,
    execution_id INT REFERENCES transfer.task_executions(id) ON DELETE CASCADE,
    offset_value BIGINT NOT NULL,
    partition_id VARCHAR(255),
    state JSONB,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_checkpoints_task_exec ON transfer.checkpoints(task_id, execution_id);
十、实施路线图
Phase 1: 基础框架（2周）
✅ 搭建 API Server 和 Worker 基础架构
✅ 实现任务队列（Redis + Asynq）
✅ 实现 ExecutionEngine 核心流程
✅ 定义 Reader/Writer 接口
Phase 2: 核心连接器（2-3周）
✅ 实现 JDBC Reader/Writer (PostgreSQL, MySQL)
✅ 实现 S3 Reader/Writer (复用 Meta/Manager 代码)
✅ 实现 File Reader/Writer (本地文件系统)
✅ 测试批量传输（DB → DB, File → DB）
Phase 3: 流批融合（2周）
✅ 实现 Checkpoint 机制
✅ 实现断点续传
✅ 实现微批处理模式
✅ 测试增量同步
Phase 4: 数据转换（1-2周）
✅ 字段映射配置
✅ 类型转换
✅ 过滤器
✅ 前端配置界面
Phase 5: 高级特性（2周）
✅ 流式连接器（Kafka Reader/Writer）
✅ 自适应批大小调优
✅ 监控面板
✅ 与 Meta 模块集成（血缘追踪）
十一、关键技术选型
组件	技术选型	理由
任务队列	Redis + Asynq	轻量级、支持持久化、与现有 Redis 集成
调度器	robfig/cron	Meta 已使用，成熟稳定
流处理	微批处理（自研）	避免引入 Flink/Spark 等重量级框架
状态存储	PostgreSQL	复用现有数据库，Checkpoint 表
连接器	插件化 Go 接口	复用 Meta 的 MetadataExtractor 模式
监控	Prometheus + Grafana (可选)	标准监控栈，后期扩展
十二、总结
核心亮点
流批一体：统一接口 + 微批处理，无需维护两套代码
插件化扩展：继承 Meta 的设计理念，连接器即插即用
轻量级实现：无需 Flink/Spark，Go 原生实现
系统集成：与 System/Manager/Meta 深度集成，形成闭环
容错能力强：Checkpoint + 队列持久化 + 自动重试
技术债务控制
初期聚焦核心场景（DB ↔ DB, DB ↔ File）
Kafka 等流式连接器作为 Phase 5 扩展
复杂转换（JOIN、聚合）暂不支持，避免重复造轮子
后续演进
支持 CDC (Change Data Capture)
支持更多数据源（ClickHouse、MongoDB）
引入 Schema Registry 管理数据 schema 版本
支持数据质量校验（行数校验、checksum 对比）
User approved the plan
感谢确认！现在我将开始实施 Transfer 模块的流批一体化设计。让我先创建任务清单来跟踪整个实施过程。
Update Todos

Phase 1: 搭建基础架构 - 创建核心接口定义

Phase 1: 实现 ExecutionEngine 核心流程

Phase 1: 集成 Asynq 任务队列

Phase 1: 实现 API Server 基础结构

Phase 2: 实现 JDBC Reader/Writer

Phase 2: 实现 S3 Reader/Writer (复用 Meta 代码)

Phase 3: 实现 Checkpoint 状态管理

Phase 3: 实现断点续传功能

Phase 4: 实现数据转换 Pipeline

Phase 4: 创建前端配置界面
现在开始实施 Phase 1。首先创建核心接口定义和数据结构。
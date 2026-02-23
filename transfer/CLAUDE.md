# Transfer 模块说明

## 核心职责

Transfer 模块是 ADDP 平台的**数据传输中枢**，负责以下核心功能：

1. **数据导入/导出/同步** - 支持多种数据源之间的数据传输（数据库、对象存储、文件）
2. **任务队列管理** - 基于 Asynq 的异步任务队列，支持任务调度、重试、并发控制
3. **流式数据处理** - 支持批处理、流式、微批次三种执行模式，处理大规模数据传输
4. **进度追踪与断点续传** - 实时追踪任务进度，支持 Checkpoint 机制的断点续传
5. **插件化架构** - Reader/Writer/Transform 插件体系，支持多种数据格式和数据源

## 关键架构

### 数据传输架构

```
前端创建任务
  ↓
API Handler (internal/api/)
  ↓
TaskService (创建任务定义 + 执行记录)
  ↓
TaskQueue (Asynq 异步队列)
  ├─ transfer:critical (高优先级队列)
  ├─ transfer:default (默认队列)
  └─ transfer:low (低优先级队列)
  ↓
Worker (后台任务处理器)
  ├─ TaskHandler.HandleExecuteTask()
  └─ TaskService.ExecuteTask()
  ↓
ExecutionEngine (数据流管道编排)
  ├─ Reader (数据读取插件)
  │  ├─ PostgreSQL/MySQL Reader (JDBC)
  │  ├─ MongoDB Reader
  │  ├─ CSV/GeoJSON/Shapefile Reader
  │  └─ MinIO/S3 Reader (对象存储)
  ├─ Transform (数据转换插件)
  │  ├─ 字段映射转换
  │  ├─ 空间坐标转换
  │  ├─ 类型转换/格式化
  │  └─ 自定义转换函数
  └─ Writer (数据写入插件)
     ├─ PostgreSQL/MySQL Writer (JDBC/COPY)
     ├─ MongoDB Writer
     ├─ CSV/GeoJSON/Shapefile Writer
     └─ MinIO/S3 Writer
  ↓
StateManager (Checkpoint 状态管理)
  └─ 保存进度到 transfer.checkpoints
  ↓
MetricsCollector (指标收集)
  ├─ records_read/written
  ├─ bytes_read/written
  └─ duration/throughput
  ↓
ExecutionRepository (更新执行记录)
  └─ transfer.task_executions
```

## 数据库文档

**遇到以下场景时,主动阅读对应文档**:

| 场景 | 必读文档 | 触发关键词 |
|------|---------|----------|
| 数据库表结构查询 | 对应单表文档 | 字段定义、索引、约束 |
| 表之间关系 | 数据库架构.md | 外键、关联、数据流 |
| API端点详情 | 对应单表文档 | API、接口、请求响应 |
| 任务管理 | tasks表 | 导入、导出、同步、定时任务 |
| 执行追踪 | task_executions表 | 执行记录、性能统计、断点 |
| 字段映射 | data_mappings表 | 字段转换、映射规则 |

### 架构说明
- [数据库架构](docs/数据库架构.md) - 表关系、数据流向、设计决策

### 单表文档

详细的表结构和API说明文档：

- [tasks表](docs/tables/tasks表.md) - 传输任务定义表,支持导入/导出/同步
- [task_executions表](docs/tables/task_executions表.md) - 执行记录表,性能统计和断点续传
- [data_mappings表](docs/tables/data_mappings表.md) - 字段映射表,定义转换规则
- [local_engines表](docs/tables/local_engines表.md) - 本地引擎配置表,私有数据源

**重要**：修改表结构或API时，必须同步更新对应的单表文档。

### 插件化架构（核心设计）

Transfer 采用 **Reader → Transform → Writer** 插件化数据流架构：

```go
// 插件注册（plugins/builtin_registration.go）
func RegisterAllConnectors(registry *pipeline.ConnectorRegistry) error {
    // 注册 Readers（数据源读取）
    registry.RegisterReader("postgres", NewPostgresReader)
    registry.RegisterReader("mysql", NewMySQLReader)
    registry.RegisterReader("csv", NewCSVReader)
    registry.RegisterReader("shapefile", NewShapefileReader)
    registry.RegisterReader("geojson", NewGeoJSONReader)
    registry.RegisterReader("s3", NewS3Reader)

    // 注册 Writers（目标数据写入）
    registry.RegisterWriter("postgres", NewPostgresWriter)
    registry.RegisterWriter("postgres_copy", NewPostgresCopyWriter) // 高性能批量导入
    registry.RegisterWriter("mysql", NewMySQLWriter)
    registry.RegisterWriter("csv", NewCSVWriter)
    registry.RegisterWriter("shapefile", NewShapefileWriter)
    registry.RegisterWriter("geojson", NewGeoJSONWriter)
    registry.RegisterWriter("s3", NewS3Writer)

    return nil
}

// 数据流执行
executionEngine.Execute(ctx, &ExecutionConfig{
    Reader: ReaderConfig{
        Type: "postgres",
        Config: {
            "host": "localhost",
            "database": "mydb",
            "table": "users",
        },
    },
    Transforms: []TransformConfig{
        {Type: "field_mapping", Config: {...}},
        {Type: "coordinate_transform", Config: {...}},
    },
    Writer: WriterConfig{
        Type: "csv",
        Config: {
            "path": "/output/users.csv",
            "delimiter": ",",
        },
    },
})
```

**支持的数据源类型**（16+ 种）：
- **关系型数据库**: PostgreSQL, MySQL, Doris, ClickHouse（JDBC）
- **NoSQL 数据库**: MongoDB
- **对象存储**: MinIO, S3
- **空间数据格式**: Shapefile, GeoJSON, GeoPackage, SpatiaLite
- **文件格式**: CSV, JSON, TXT

### Asynq 任务队列架构

```
任务创建（API 调用）
  ↓
TaskService.TriggerTask()
  ↓
TaskQueue.EnqueueExecuteTask()
  ├─ 创建 TaskExecution 记录（status=pending）
  ├─ 序列化任务载荷（TaskID, ExecutionID, TenantID）
  └─ 推送到 Redis 队列（asynq:queues:transfer:*)
  ↓
Worker 消费任务（后台进程）
  ├─ 从 Redis 队列拉取任务
  ├─ 调用 TaskHandler.HandleExecuteTask()
  ├─ 执行数据流管道
  ├─ 更新执行状态（running → success/failed）
  └─ 自动重试（失败时，最多 3 次）
  ↓
任务完成
  └─ 更新 task_executions.status
  └─ 记录执行结果（records_read/written, duration, logs）
```

**队列优先级策略**：
- `transfer:critical` (权重 6) - 紧急任务、手动触发
- `transfer:default` (权重 3) - 常规导入/导出任务
- `transfer:low` (权重 1) - 定时同步任务

### 定时调度架构

```
Scheduler.Start()
  ↓
加载所有定时任务（task.schedule != ""）
  ↓
解析 Cron 表达式（支持秒级精度）
  ├─ "0 2 * * *" - 每天凌晨 2 点
  ├─ "*/5 * * * *" - 每 5 分钟
  └─ "0 */2 * * *" - 每 2 小时
  ↓
注册到 common/scheduler（基于 robfig/cron）
  ↓
定时触发 → 创建 TaskExecution → 入队 → Worker 执行
```

### Checkpoint 断点续传机制

```go
// 每处理 10 批次保存一次 Checkpoint
if batchCount % config.CheckpointInterval == 0 {
    checkpoint := Checkpoint{
        ExecutionID: execID,
        Offset:      currentOffset,
        State: map[string]interface{}{
            "last_id": lastProcessedID,
            "batch_count": batchCount,
        },
    }
    stateManager.SaveCheckpoint(checkpoint)
}

// 任务重启时恢复进度
checkpoint := stateManager.LoadCheckpoint(execID)
if checkpoint != nil {
    startOffset = checkpoint.Offset
    logger.Info("从 Checkpoint 恢复", "offset", startOffset)
}
```

### 进度追踪架构

```
ExecutionEngine.Execute()
  ↓
设置进度回调（每批次更新）
  ↓
progressCallback(logs, metrics)
  ├─ 更新 task_executions.logs（追加日志）
  ├─ 更新 task_executions.records_read
  ├─ 更新 task_executions.records_written
  └─ 更新 task_executions.bytes_read/written
  ↓
前端轮询 /api/executions/:id
  └─ 实时显示进度和日志
```

### 依赖的其他模块

- **System 模块** (`common/client/system.go`) - 获取存储引擎连接信息，验证租户权限
- **Redis** - 两种用途：
  - 任务队列（Asynq，异步执行任务）
  - 资源事件同步（监听存储引擎变更事件）
- **PostgreSQL** - 存储任务定义、执行记录、数据映射配置
- **MinIO/S3** - 临时文件存储（Shapefile 解压、CSV 缓存）

### 使用的中间件资源

- **PostgreSQL Schema**: `transfer`
  - `tasks` 表（任务定义）
  - `task_executions` 表（执行记录）
  - `data_mappings` 表（字段映射配置）
  - `local_engines` 表（本地数据源配置，实验性功能）
  - `checkpoints` 表（断点续传状态，由 StateManager 管理）
- **Redis Key 前缀**:
  - `asynq:queues:transfer:*` - 任务队列
  - `asynq:{transfer}:*` - 任务状态追踪
  - `transfer:events:resource_changed` - 资源变更事件频道
- **Asynq Queue**:
  - `transfer:critical` (高优先级)
  - `transfer:default` (默认优先级)
  - `transfer:low` (低优先级)

## 重要文件位置

### 核心服务文件

- [task_service.go](backend/internal/service/task_service.go) - **任务管理服务**（创建任务、触发执行、查询任务列表）
- [execution_service.go](backend/internal/service/execution_service.go) - **执行管理服务**（查询执行记录、取消执行、清理历史）
- [local_engine_service.go](backend/internal/service/local_engine_service.go) - **本地引擎服务**（管理 Transfer 本地数据源配置）
- [object_storage_service.go](backend/internal/service/object_storage_service.go) - **对象存储服务**（浏览 MinIO/S3 文件）

### 任务队列文件

- [queue.go](backend/internal/worker/queue.go) - **任务队列封装**（Asynq 客户端，入队操作）
- [handler.go](backend/internal/worker/handler.go) - **任务处理器**（Worker 消费任务的核心逻辑）
- [scheduler.go](backend/internal/worker/scheduler.go) - **定时调度器**（基于 Cron 的任务调度）

### 数据流引擎文件

- [engine.go](backend/pkg/pipeline/engine.go) - **执行引擎**（编排 Reader → Transform → Writer 数据流）
- [state_manager.go](backend/pkg/pipeline/state_manager.go) - **状态管理器**（Checkpoint 保存/恢复）
- [metrics.go](backend/pkg/pipeline/metrics.go) - **指标收集器**（统计传输速率、记录数）
- [connector_registry.go](backend/pkg/pipeline/connector_registry.go) - **连接器注册表**（管理所有 Reader/Writer 插件）

### 插件文件

**Reader 插件** (plugins/readers/):
- [jdbc_reader.go](backend/plugins/readers/jdbc_reader.go) - PostgreSQL/MySQL/Doris/ClickHouse 数据读取
- [csv_reader.go](backend/plugins/readers/csv_reader.go) - CSV 文件读取
- [shapefile_reader.go](backend/plugins/readers/shapefile_reader.go) - Shapefile 读取
- [geojson_reader.go](backend/plugins/readers/geojson_reader.go) - GeoJSON 读取
- [s3_reader.go](backend/plugins/readers/s3_reader.go) - S3/MinIO 对象读取
- [geopackage_reader.go](backend/plugins/readers/geopackage_reader.go) - GeoPackage 读取
- [spatialite_reader.go](backend/plugins/readers/spatialite_reader.go) - SpatiaLite 读取

**Writer 插件** (plugins/writers/):
- [jdbc_writer.go](backend/plugins/writers/jdbc_writer.go) - 通用 JDBC 数据写入
- [postgres_copy_writer.go](backend/plugins/writers/postgres_copy_writer.go) - PostgreSQL COPY 批量导入（高性能）
- [csv_writer.go](backend/plugins/writers/csv_writer.go) - CSV 文件写入
- [shapefile_writer.go](backend/plugins/writers/shapefile_writer.go) - Shapefile 写入
- [geojson_writer.go](backend/plugins/writers/geojson_writer.go) - GeoJSON 写入
- [s3_writer.go](backend/plugins/writers/s3_writer.go) - S3/MinIO 对象写入
- [geopackage_writer.go](backend/plugins/writers/geopackage_writer.go) - GeoPackage 写入

**Transform 插件** (internal/transform/):
- [builtin_basic.go](backend/internal/transform/builtin_basic.go) - 基础转换（字段映射、类型转换、默认值）
- [builtin_spatial.go](backend/internal/transform/builtin_spatial.go) - 空间转换（坐标系转换、几何简化）

### API 路由文件

- [router.go](backend/internal/api/router.go) - HTTP 路由定义
- [task_handler.go](backend/internal/api/task_handler.go) - 任务管理 API（创建/更新/删除/查询）
- [execution_handler.go](backend/internal/api/execution_handler.go) - 执行记录 API（查询/取消/日志）
- [operator_handler.go](backend/internal/api/operator_handler.go) - 算子管理 API（查询可用的 Reader/Writer）
- [local_engine_handler.go](backend/internal/api/local_engine_handler.go) - 本地引擎 API
- [object_storage_handler.go](backend/internal/api/object_storage_handler.go) - 对象存储浏览 API

### 数据模型文件

- [task.go](backend/internal/models/task.go) - 任务和执行记录模型（Task, TaskExecution, DataMapping）
- [local_engine.go](backend/internal/models/local_engine.go) - 本地引擎模型

### 前端视图文件

- [Dashboard.vue](frontend/src/views/Dashboard.vue) - **任务仪表盘**（统计卡片、任务列表）
- [TaskFormEnhanced.vue](frontend/src/views/TaskFormEnhanced.vue) - **任务创建向导**（分步表单：基本信息 → 数据源 → 字段映射 → 调度配置）
- [ExecutionList.vue](frontend/src/views/ExecutionList.vue) - **执行记录列表**（历史记录、进度追踪）
- [FieldMappingEditor.vue](frontend/src/components/FieldMappingEditor.vue) - **字段映射编辑器**（可视化字段映射配置）
- [ObjectStoragePathPicker.vue](frontend/src/components/ObjectStoragePathPicker.vue) - **对象存储路径选择器**（浏览 MinIO/S3 文件）

### 配置文件

- [config.go](backend/internal/config/config.go) - 配置加载逻辑
- [.env](../.env) - 环境变量（`TRANSFER_*` 前缀）
- [docker-compose.yml](../docker-compose.yml) - 服务定义（transfer-backend, transfer-worker, transfer-frontend）

## 常见开发场景

### 场景 1：创建导入任务（CSV → PostgreSQL）

**需求示例**：将 CSV 文件导入 PostgreSQL 数据库

**步骤**：

1. **通过 API 创建任务**：
   ```bash
   curl -X POST http://localhost:8083/api/tasks \
     -H "Authorization: Bearer <token>" \
     -H "Content-Type: application/json" \
     -d '{
       "name": "导入用户数据",
       "description": "从 CSV 导入用户信息到 PostgreSQL",
       "type": "import",
       "mode": "batch",
       "source_id": 10,
       "target_id": 1,
       "config": {
         "reader": {
           "type": "csv",
           "path": "/data/users.csv",
           "delimiter": ",",
           "has_header": true
         },
         "writer": {
           "type": "postgres",
           "schema": "public",
           "table": "users",
           "mode": "insert"
         },
         "transforms": [
           {
             "type": "field_mapping",
             "config": {
               "mappings": {
                 "user_id": "id",
                 "user_name": "name",
                 "user_email": "email"
               }
             }
           }
         ]
       },
       "batch_size": 1000,
       "max_parallelism": 1
     }'
   ```

2. **触发任务执行**：
   ```bash
   # 手动触发
   curl -X POST http://localhost:8083/api/tasks/1/trigger \
     -H "Authorization: Bearer <token>"

   # 响应示例
   {
     "execution_id": 123,
     "status": "pending",
     "message": "任务已入队"
   }
   ```

3. **查看执行进度**：
   ```bash
   # 查询执行记录
   curl -H "Authorization: Bearer <token>" \
     http://localhost:8083/api/executions/123

   # 响应示例
   {
     "id": 123,
     "task_id": 1,
     "status": "running",
     "start_time": "2025-12-29T10:00:00",
     "records_read": 5000,
     "records_written": 5000,
     "bytes_read": 102400,
     "logs": "开始读取 CSV 文件...\n已处理 5000 条记录..."
   }
   ```

4. **查看执行日志**：
   ```bash
   # 查看实时日志
   tail -f logs/transfer-worker.log

   # 或通过 API 查询（logs 字段）
   curl -H "Authorization: Bearer <token>" \
     http://localhost:8083/api/executions/123 | jq '.logs'
   ```

**关键配置说明**：
- `source_id` / `target_id` - 关联 `system.engines` 表的存储引擎 ID
- `config.reader.type` - Reader 插件类型（csv, postgres, shapefile, s3 等）
- `config.writer.mode` - 写入模式（`insert`, `upsert`, `replace`, `append`）
- `batch_size` - 批大小（默认 1000，可根据内存调整）

**相关文件**：
- [task_service.go:77-150](backend/internal/service/task_service.go) - 创建任务逻辑
- [csv_reader.go](backend/plugins/readers/csv_reader.go) - CSV 读取插件
- [jdbc_writer.go](backend/plugins/writers/jdbc_writer.go) - PostgreSQL 写入插件

### 场景 2：调试任务执行失败问题

**常见错误类型**：

1. **"failed to connect to database"** → 存储引擎连接信息错误，检查 `system.engines` 表
2. **"reader plugin not found"** → Reader 插件未注册，检查 `plugins/builtin_registration.go`
3. **"checkpoint restore failed"** → Checkpoint 状态损坏，清理 `transfer.checkpoints` 表
4. **"queue enqueue failed"** → Redis 连接问题，检查 `REDIS_HOST` 和 `REDIS_PORT`

**调试步骤**：

```bash
# 1. 查看 Transfer Worker 日志（任务执行日志）
tail -f logs/transfer-worker.log

# 2. 查看 Transfer Backend 日志（API 请求日志）
tail -f logs/transfer-backend.log

# 3. 查询失败的执行记录（包含详细错误信息）
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8083/api/executions?status=failed&limit=10"

# 4. 查看具体执行详情
curl -H "Authorization: Bearer <token>" \
  http://localhost:8083/api/executions/<execution_id> | jq '.error_msg'

# 5. 检查 Redis 队列状态
docker exec -it addp-infra-redis redis-cli
> LLEN asynq:queues:transfer:default  # 查看队列长度
> KEYS asynq:{transfer}:*              # 查看所有 Transfer 相关的键

# 6. 检查 Worker 是否在运行
ps aux | grep transfer-worker
# 或查看 Docker 容器状态
docker ps | grep transfer-worker

# 7. 手动重试失败的任务
curl -X POST http://localhost:8083/api/tasks/<task_id>/trigger \
  -H "Authorization: Bearer <token>"

# 8. 清理错误的 Checkpoint（如果断点续传失败）
psql -h localhost -p 15432 -U addp -d addp
> DELETE FROM transfer.checkpoints WHERE execution_id = <exec_id>;
```

**关键日志位置**：
- [transfer-worker.log](../logs/transfer-worker.log) - Worker 执行日志（任务处理、插件加载、数据传输）
- [transfer-backend.log](../logs/transfer-backend.log) - Backend API 日志（任务创建、触发请求）

**常见错误修复**：

| 错误类型 | 原因 | 解决方案 |
|---------|------|---------|
| "failed to decrypt connection info" | System 模块的 `ENCRYPTION_KEY` 与 Transfer 不一致 | 确保 `.env` 中的 `ENCRYPTION_KEY` 一致 |
| "reader/writer plugin not found" | 插件未注册或类型名称错误 | 检查 `plugins/builtin_registration.go`，确认插件已注册 |
| "database connection timeout" | 数据库连接参数错误或网络问题 | 测试存储引擎连接（通过 System 模块的连接测试 API） |
| "out of memory" | 批大小过大，内存不足 | 减小 `batch_size`（如从 10000 降到 1000） |
| "checkpoint state corrupted" | Checkpoint JSON 格式错误 | 删除对应的 Checkpoint 记录，重新执行任务 |

### 场景 3：添加新的数据源支持（MongoDB Reader）

**需求示例**：支持从 MongoDB 读取数据

**步骤**：

1. **创建 Reader 插件**：
   ```bash
   touch backend/plugins/readers/mongodb_reader.go
   ```

2. **实现 `Reader` 接口**：
   ```go
   package readers

   import (
       "context"
       "github.com/addp/transfer/pkg/pipeline"
       "go.mongodb.org/mongo-driver/mongo"
   )

   type MongoDBReader struct {
       client     *mongo.Client
       database   string
       collection string
       query      map[string]interface{}
   }

   func NewMongoDBReader(config map[string]interface{}) (pipeline.Reader, error) {
       // 解析配置
       host := config["host"].(string)
       database := config["database"].(string)
       collection := config["collection"].(string)

       // 连接 MongoDB
       client, err := mongo.Connect(context.Background(), options.Client().ApplyURI(host))
       if err != nil {
           return nil, err
       }

       return &MongoDBReader{
           client:     client,
           database:   database,
           collection: collection,
       }, nil
   }

   func (r *MongoDBReader) Read(ctx context.Context) (<-chan []map[string]interface{}, <-chan error) {
       dataChan := make(chan []map[string]interface{})
       errChan := make(chan error, 1)

       go func() {
           defer close(dataChan)
           defer close(errChan)

           // 查询 MongoDB
           cursor, err := r.client.Database(r.database).
               Collection(r.collection).Find(ctx, r.query)
           if err != nil {
               errChan <- err
               return
           }
           defer cursor.Close(ctx)

           // 批量读取
           batch := make([]map[string]interface{}, 0, 1000)
           for cursor.Next(ctx) {
               var doc map[string]interface{}
               if err := cursor.Decode(&doc); err != nil {
                   errChan <- err
                   return
               }
               batch = append(batch, doc)

               if len(batch) >= 1000 {
                   dataChan <- batch
                   batch = make([]map[string]interface{}, 0, 1000)
               }
           }

           if len(batch) > 0 {
               dataChan <- batch
           }
       }()

       return dataChan, errChan
   }

   func (r *MongoDBReader) Close() error {
       return r.client.Disconnect(context.Background())
   }
   ```

3. **注册插件**（在 `plugins/builtin_registration.go`）：
   ```go
   func RegisterAllConnectors(registry *pipeline.ConnectorRegistry) error {
       // 注册 MongoDB Reader
       registry.RegisterReader("mongodb", readers.NewMongoDBReader)

       // ... 其他插件
       return nil
   }
   ```

4. **重启 Transfer Worker**：
   ```bash
   bash scripts/dev/restart.sh -transfer

   # 查看日志确认插件加载
   grep "已注册连接器" logs/transfer-worker.log
   # 输出示例：✅ 已注册连接器 - Readers: [postgres mysql csv mongodb ...]
   ```

5. **测试新插件**：
   ```bash
   # 创建任务使用 MongoDB Reader
   curl -X POST http://localhost:8083/api/tasks \
     -H "Authorization: Bearer <token>" \
     -d '{
       "name": "MongoDB 导出任务",
       "type": "export",
       "config": {
         "reader": {
           "type": "mongodb",
           "host": "mongodb://localhost:27017",
           "database": "test",
           "collection": "users"
         },
         "writer": {
           "type": "csv",
           "path": "/output/mongodb_export.csv"
         }
       }
     }'
   ```

**相关文件**：
- [connector_registry.go:1-100](backend/pkg/pipeline/connector_registry.go) - 插件注册表
- [jdbc_reader.go](backend/plugins/readers/jdbc_reader.go) - JDBC Reader 参考实现
- [builtin_registration.go](backend/plugins/builtin_registration.go) - 插件注册入口

### 场景 4：优化大数据传输性能

**问题描述**：传输 1000 万条记录的表非常慢

**优化方案**：

1. **使用 PostgreSQL COPY Writer**（高性能批量导入）：
   ```json
   {
     "writer": {
       "type": "postgres_copy",
       "schema": "public",
       "table": "large_table"
     }
   }
   ```
   - **COPY 模式** 比 INSERT 快 5-10 倍
   - 原理：绕过 SQL 解析，直接写入数据文件

2. **调整批大小**（根据内存和网络情况）：
   ```json
   {
     "batch_size": 5000,
     "max_parallelism": 1
   }
   ```
   - 小批次（1000）：减少内存占用，适合内存受限环境
   - 大批次（10000）：提高吞吐量，适合内存充足的服务器

3. **启用并行传输**（多 Worker 实例）：
   ```bash
   # 修改 docker-compose.yml 增加 Worker 实例
   transfer-worker:
     deploy:
       replicas: 3  # 3 个 Worker 并发处理任务
   ```

4. **启用流式模式**（大文件导出）：
   ```json
   {
     "mode": "stream",
     "config": {
       "reader": {
         "type": "postgres",
         "streaming": true,
         "fetch_size": 10000
       }
     }
   }
   ```

5. **使用 Checkpoint 断点续传**（避免重复传输）：
   ```bash
   # 任务失败后重新触发，自动从断点恢复
   curl -X POST http://localhost:8083/api/tasks/<task_id>/trigger \
     -H "Authorization: Bearer <token>"

   # 查看日志确认恢复
   grep "从 Checkpoint 恢复" logs/transfer-worker.log
   ```

**性能对比**：

| 优化方案 | 传输速度 | 适用场景 |
|---------|---------|---------|
| 默认配置（INSERT + batch_size=1000） | 1000 条/秒 | 小数据量（< 10 万条） |
| PostgreSQL COPY + batch_size=5000 | 10000 条/秒 | 大数据量（> 100 万条） |
| 并行传输（3 Workers） | 30000 条/秒 | 超大数据量（> 1000 万条） |
| 流式模式（streaming=true） | 8000 条/秒 | 大文件导出（避免内存溢出） |

**相关文件**：
- [postgres_copy_writer.go](backend/plugins/writers/postgres_copy_writer.go) - PostgreSQL COPY 高性能写入
- [engine.go:50-100](backend/pkg/pipeline/engine.go) - 批大小配置和流式处理逻辑
- [state_manager.go](backend/pkg/pipeline/state_manager.go) - Checkpoint 断点续传

### 场景 5：创建定时同步任务

**需求示例**：每天凌晨 2 点自动同步 MySQL 数据到 PostgreSQL

**步骤**：

1. **创建定时任务**：
   ```bash
   curl -X POST http://localhost:8083/api/tasks \
     -H "Authorization: Bearer <token>" \
     -H "Content-Type: application/json" \
     -d '{
       "name": "每日数据同步",
       "description": "MySQL → PostgreSQL 增量同步",
       "type": "sync",
       "mode": "batch",
       "source_id": 2,
       "target_id": 1,
       "schedule": "0 2 * * *",
       "config": {
         "reader": {
           "type": "mysql",
           "schema": "production",
           "table": "orders",
           "incremental": true,
           "timestamp_column": "updated_at"
         },
         "writer": {
           "type": "postgres",
           "schema": "public",
           "table": "orders",
           "mode": "upsert",
           "conflict_columns": ["id"]
         }
       },
       "batch_size": 2000
     }'
   ```

2. **验证任务调度**：
   ```bash
   # 查看任务状态（应显示 status=scheduled）
   curl -H "Authorization: Bearer <token>" \
     http://localhost:8083/api/tasks/1

   # 查看调度器日志
   grep "定时调度器" logs/transfer-worker.log
   # 输出示例：✅ 定时调度器已启动，已注册 1 个定时任务
   ```

3. **手动触发测试**（不等到凌晨 2 点）：
   ```bash
   curl -X POST http://localhost:8083/api/tasks/1/trigger \
     -H "Authorization: Bearer <token>"
   ```

4. **查看执行历史**：
   ```bash
   # 查询该任务的所有执行记录
   curl -H "Authorization: Bearer <token>" \
     "http://localhost:8083/api/executions?task_id=1&page=1&page_size=10"
   ```

**Cron 表达式示例**：
- `0 2 * * *` - 每天凌晨 2 点
- `0 */2 * * *` - 每 2 小时
- `*/30 * * * *` - 每 30 分钟
- `0 0 * * 0` - 每周日午夜
- `0 9-17 * * 1-5` - 工作日 9:00-17:00 每小时

**增量同步配置**：
```json
{
  "reader": {
    "incremental": true,
    "timestamp_column": "updated_at",
    "last_sync_time": "2025-12-28T00:00:00"
  },
  "writer": {
    "mode": "upsert",
    "conflict_columns": ["id"]
  }
}
```

**相关文件**：
- [scheduler.go:40-100](backend/internal/worker/scheduler.go) - 定时调度器实现
- [task_service.go:200-300](backend/internal/service/task_service.go) - 任务触发逻辑

## 注意事项

### 1. 租户隔离与权限验证

Transfer 模块严格执行租户隔离：
- 所有任务都绑定到 `tenant_id`
- 用户只能访问自己租户的任务和执行记录
- Worker 执行任务时会验证租户权限

**错误示例**：
```go
// ❌ 错误：直接查询 tasks 表，跳过租户验证
var tasks []models.Task
db.Where("status = ?", "running").Find(&tasks)

// ✅ 正确：通过 API 查询，自动应用租户过滤
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8083/api/tasks?status=running"
```

### 2. Asynq 任务队列配置要点

**重试策略**：
```go
// worker/queue.go
RetryDelayFunc: func(n int, err error, task *asynq.Task) time.Duration {
    return time.Duration(n) * cfg.RetryDelay  // 指数退避（1s, 2s, 4s）
}
```

**队列优先级**：
- 高优先级任务（critical）优先执行
- 低优先级任务（low）在空闲时执行
- 默认队列（default）平衡两者

**陷阱**：
- ❌ 不要在任务载荷中存储大对象（如完整的数据行），只存储 ID
- ❌ 不要在 Worker 中执行长时间阻塞操作（超过 10 分钟），应拆分为多个子任务
- ✅ 任务执行失败后会自动重试（最多 3 次），需保证幂等性

### 3. Checkpoint 断点续传机制

**适用场景**：
- 传输大规模数据（> 100 万条）
- 网络不稳定环境
- 需要支持暂停/恢复的任务

**Checkpoint 保存策略**：
```go
// engine.go
if batchCount % config.CheckpointInterval == 0 {  // 每 10 批次保存一次
    stateManager.SaveCheckpoint(checkpoint)
}
```

**重要**：
- Checkpoint 只保存 **偏移量** 和 **状态元数据**（不保存数据）
- 任务重启后，Reader 从 Checkpoint 的 offset 位置继续读取
- 如果 Checkpoint 状态损坏，需手动清理并重新执行任务

### 4. 插件开发注意事项

**Reader 插件必须实现**：
```go
type Reader interface {
    Read(ctx context.Context) (<-chan []map[string]interface{}, <-chan error)
    Close() error
}
```

**Writer 插件必须实现**：
```go
type Writer interface {
    Write(ctx context.Context, data []map[string]interface{}) error
    Close() error
}
```

**关键原则**：
- Reader 必须是 **流式** 的（通过 channel 分批发送数据）
- Writer 必须是 **批量** 的（接收一批数据一次性写入）
- 插件内部应处理连接池管理（避免频繁创建/销毁连接）
- 插件必须支持 **上下文取消**（`ctx.Done()` 时立即停止）

### 5. 与 System 模块的交互要点

- **Transfer 依赖 System** - 通过 System API 获取存储引擎连接信息
- **连接信息解密** - System 返回的连接信息是加密的，Transfer 需使用相同的 `ENCRYPTION_KEY` 解密
- **服务间认证** - Transfer 使用 `INTERNAL_API_KEY` 调用 System API（服务间调用）

**连接信息获取流程**：
```go
// task_service.go
engineInfo, err := systemClient.GetEngine(ctx, sourceID)
if err != nil {
    return fmt.Errorf("failed to get engine info: %w", err)
}

// 解密连接信息
connectionInfo, err := systemClient.DecryptConnectionInfo(engineInfo.ConnectionInfo)
if err != nil {
    return fmt.Errorf("failed to decrypt connection info: %w", err)
}

// 创建 Reader
reader, err := registry.CreateReader(readerType, connectionInfo)
```

### 6. 性能优化建议

**批大小调优**：
- 小数据量（< 10 万）：`batch_size = 1000`
- 中等数据量（10-100 万）：`batch_size = 5000`
- 大数据量（> 100 万）：`batch_size = 10000`

**内存优化**：
- 使用流式模式（`mode: "stream"`）避免一次性加载所有数据
- Reader 使用游标（cursor）分批读取
- Writer 立即写入，不缓存数据

**网络优化**：
- 数据库到数据库传输：优先使用 COPY 或 BULK INSERT
- 跨网络传输：启用压缩（如 gzip）
- 对象存储传输：使用多线程上传（S3 Multipart Upload）

## 典型开发工作流

### 修改 Transfer Backend 代码后

```bash
# 1. 重启 Transfer Backend 服务（会自动重新编译）
bash scripts/dev/restart.sh -transfer

# 2. 查看启动日志（确认编译成功）
tail -f logs/transfer-backend.log

# 3. 测试 API（使用 Console 登录获取 token）
curl -H "Authorization: Bearer <token>" \
  http://localhost:8083/api/tasks
```

### 修改 Transfer Worker 代码后

```bash
# 1. 重启 Worker 服务
bash scripts/dev/restart.sh -transfer

# 2. 查看 Worker 日志
tail -f logs/transfer-worker.log

# 3. 触发测试任务
curl -X POST http://localhost:8083/api/tasks/<task_id>/trigger \
  -H "Authorization: Bearer <token>"

# 4. 查看执行日志
grep "TaskID: <task_id>" logs/transfer-worker.log
```

### 添加新的 Reader/Writer 插件后

```bash
# 1. 编写插件代码（参考场景 3）
# 2. 在 plugins/builtin_registration.go 中注册
# 3. 重启 Worker 服务
bash scripts/dev/restart.sh -transfer

# 4. 验证插件加载（查看日志）
grep "已注册连接器" logs/transfer-worker.log

# 5. 创建任务测试插件
curl -X POST http://localhost:8083/api/tasks \
  -H "Authorization: Bearer <token>" \
  -d '{"config": {"reader": {"type": "your_new_reader"}, ...}}'
```

### 调试任务执行失败

```bash
# 1. 查看失败的执行记录
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8083/api/executions?status=failed&limit=10"

# 2. 查看具体错误信息
curl -H "Authorization: Bearer <token>" \
  http://localhost:8083/api/executions/<execution_id> | jq '.error_msg'

# 3. 查看 Worker 日志（搜索关键词）
grep "任务执行失败" logs/transfer-worker.log
grep "ExecutionID: <execution_id>" logs/transfer-worker.log

# 4. 检查 Redis 队列状态
docker exec -it addp-infra-redis redis-cli
> LLEN asynq:queues:transfer:default

# 5. 手动重试任务
curl -X POST http://localhost:8083/api/tasks/<task_id>/trigger \
  -H "Authorization: Bearer <token>"
```

## 相关文档

- **存储引擎插件系统** - [docs/数据库插件系统.md](../docs/数据库插件系统.md)
- **System 模块说明** - [system/CLAUDE.md](../system/CLAUDE.md)
- **Manager 模块说明** - [manager/CLAUDE.md](../manager/CLAUDE.md)
- **Meta 模块说明** - [meta/CLAUDE.md](../meta/CLAUDE.md)
- **Asynq 文档** - [github.com/hibiken/asynq](https://github.com/hibiken/asynq)

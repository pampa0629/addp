# Transfer 数据传输模块

## 概述

Transfer 是全域数据平台的数据传输服务，负责数据的导入、导出、同步和转换任务的执行和管理。

## 核心功能

- **数据导入**: 从外部数据源导入数据到平台
- **数据导出**: 将平台数据导出到外部系统
- **数据同步**: 定时或实时同步数据
- **数据转换**: 在传输过程中进行数据格式转换和清洗
- **任务调度**: 管理数据传输任务的执行和调度
- **断点续传**: 支持大数据传输的断点续传

## 技术栈

- **语言**: Go 1.21+
- **框架**: Gin
- **ORM**: GORM
- **任务队列**: Redis + Asynq / RabbitMQ
- **数据库**: PostgreSQL (任务元数据)
- **前端**: Vue 3 + Element Plus

## 项目结构

```
transfer/
├── backend/
│   ├── cmd/
│   │   ├── server/
│   │   │   └── main.go      # API 服务入口
│   │   └── worker/
│   │       └── main.go      # Worker 入口
│   ├── internal/
│   │   ├── api/             # HTTP 处理层
│   │   ├── service/         # 业务逻辑层
│   │   ├── repository/      # 数据访问层
│   │   ├── models/          # 数据模型
│   │   ├── worker/          # 任务执行引擎
│   │   ├── connector/       # 数据源连接器
│   │   │   ├── reader/      # 数据读取器
│   │   │   └── writer/      # 数据写入器
│   │   ├── transformer/     # 数据转换器
│   │   └── scheduler/       # 任务调度器
│   ├── pkg/
│   │   └── pipeline/        # 数据管道
│   ├── Dockerfile
│   └── go.mod
├── frontend/
│   ├── src/
│   │   ├── views/
│   │   │   ├── Tasks.vue          # 任务管理
│   │   │   ├── TaskCreate.vue     # 创建任务
│   │   │   └── TaskMonitor.vue    # 任务监控
│   │   └── api/
│   ├── package.json
│   └── Dockerfile
└── README.md
```

## 数据模型设计

### 传输任务 (TransferTask)
```go
type TransferTask struct {
    ID          uint
    Name        string
    Type        string      // import, export, sync
    SourceID    uint        // 源数据源 ID
    TargetID    uint        // 目标数据源 ID
    Config      JSON        // 任务配置
    Schedule    string      // Cron 表达式
    Status      string      // pending, running, success, failed, paused
    Progress    float64     // 0-100
    CreatedBy   uint
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

### 任务执行记录 (TaskExecution)
```go
type TaskExecution struct {
    ID          uint
    TaskID      uint
    Status      string
    StartTime   time.Time
    EndTime     *time.Time
    RecordsRead int64
    RecordsWritten int64
    BytesRead   int64
    BytesWritten int64
    ErrorMsg    string
    Logs        TEXT        // 执行日志
}
```

### 数据映射配置 (DataMapping)
```go
type DataMapping struct {
    ID          uint
    TaskID      uint
    SourceField string
    TargetField string
    Transform   string      // 转换函数
    DefaultValue string
}
```

## API 端点

### 任务管理
- `POST /api/tasks` - 创建传输任务
- `GET /api/tasks` - 获取任务列表
- `GET /api/tasks/:id` - 获取任务详情
- `PUT /api/tasks/:id` - 更新任务配置
- `DELETE /api/tasks/:id` - 删除任务
- `POST /api/tasks/:id/start` - 启动任务
- `POST /api/tasks/:id/stop` - 停止任务
- `POST /api/tasks/:id/pause` - 暂停任务
- `POST /api/tasks/:id/resume` - 恢复任务

### 任务执行
- `GET /api/tasks/:id/executions` - 获取执行历史
- `GET /api/executions/:id` - 获取执行详情
- `GET /api/executions/:id/logs` - 获取执行日志
- `POST /api/executions/:id/retry` - 重试失败任务

### 任务监控
- `GET /api/tasks/running` - 获取运行中的任务
- `GET /api/tasks/:id/progress` - 获取任务进度
- `GET /api/tasks/statistics` - 获取任务统计信息

## 数据传输流程

### 1. 导入流程
```
外部数据源 → Reader → Transformer → Writer → 平台存储
                ↓
              Meta 模块（记录血缘）
```

### 2. 导出流程
```
平台存储 → Reader → Transformer → Writer → 外部目标
            ↓
          Meta 模块（记录血缘）
```

### 3. 同步流程
```
源系统 → Reader → 增量检测 → Transformer → Writer → 目标系统
           ↓
        Meta 模块（更新元数据）
```

## 支持的数据源

### 数据库
- MySQL / MariaDB
- PostgreSQL
- ClickHouse
- MongoDB
- Redis

### 文件系统
- 本地文件系统
- MinIO / S3
- HDFS
- FTP / SFTP

### 消息队列
- Kafka
- RabbitMQ
- Pulsar

### API
- REST API
- GraphQL

## 数据转换功能

### 字段映射
- 字段重命名
- 字段类型转换
- 默认值填充
- 字段拆分/合并

### 数据过滤
- 条件过滤
- 去重
- 采样

### 数据转换
- 日期格式转换
- 字符串处理
- 数值计算
- 自定义转换函数

## 任务调度

### 调度方式
- **立即执行**: 创建后立即执行一次
- **定时执行**: 基于 Cron 表达式的定时任务
- **手动触发**: 用户手动触发执行
- **事件触发**: 基于特定事件触发（待实现）

### Cron 表达式示例
```
0 0 * * *       # 每天凌晨执行
0 */4 * * *     # 每 4 小时执行
0 0 * * 1       # 每周一凌晨执行
```

## 开发计划

### 阶段 1: 基础传输
- [ ] 任务数据模型设计
- [ ] 任务 CRUD API
- [ ] MySQL → MySQL 简单传输
- [ ] CSV → MySQL 导入
- [ ] MySQL → CSV 导出

### 阶段 2: 任务执行
- [ ] Worker 进程实现
- [ ] 任务队列集成（Asynq）
- [ ] 任务进度跟踪
- [ ] 执行日志记录
- [ ] 任务监控前端

### 阶段 3: 数据转换
- [ ] 字段映射配置
- [ ] 基础数据转换函数
- [ ] 数据过滤功能
- [ ] 转换配置界面

### 阶段 4: 调度系统
- [ ] Cron 调度器集成
- [ ] 定时任务执行
- [ ] 任务依赖管理
- [ ] 失败重试机制

### 阶段 5: 高级特性
- [ ] 断点续传
- [ ] 增量同步
- [ ] 并行传输优化
- [ ] 数据压缩
- [ ] 与 Meta 模块集成（血缘记录）

## 配置说明

### 环境变量

```bash
# API 服务端口
TRANSFER_PORT=8083

# 数据库配置
DB_HOST=postgres
DB_PORT=5432
DB_NAME=transfer
DB_USER=transfer
DB_PASSWORD=password

# Redis 配置（任务队列）
REDIS_HOST=redis
REDIS_PORT=6379
REDIS_PASSWORD=
REDIS_DB=0

# Worker 配置
WORKER_CONCURRENCY=10        # 并发任务数
WORKER_QUEUE_NAME=transfer   # 队列名称

# 传输配置
TRANSFER_BATCH_SIZE=1000     # 批量传输行数
TRANSFER_TIMEOUT=3600s       # 任务超时时间
TRANSFER_MAX_RETRY=3         # 最大重试次数

# 临时文件路径
TEMP_DIR=/tmp/transfer
```

## 运行方式

```bash
# API 服务
cd backend
go run cmd/server/main.go

# Worker 进程
cd backend
go run cmd/worker/main.go

# 前端开发
cd frontend
npm install
npm run dev

# Docker 部署
docker-compose up -d
```

## 性能优化

### 大数据传输
- 批量读写
- 流式处理
- 并行传输
- 数据压缩

### 资源控制
- 内存限制
- 并发控制
- 速率限制
- 超时控制

## 待补充内容

- 各数据源的连接器实现细节
- 数据转换函数库规范
- 增量同步的实现策略
- 断点续传的checkpoint机制
- 与 Meta 模块的血缘记录接口
- 任务失败的告警机制
- 数据一致性保证策略
- 性能监控指标定义
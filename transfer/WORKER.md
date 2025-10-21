# Transfer Worker 使用文档

## 概述

Transfer Worker 是 ADDP 数据传输模块的后台任务执行器，负责处理数据传输任务的实际执行。Worker 基于 **Asynq** 任务队列和 **Cron** 定时调度器实现。

## 架构

```
┌─────────────────────────────────────────────────────────────┐
│                     Transfer Module                         │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ┌──────────────┐         ┌──────────────┐                │
│  │  API Server  │         │    Worker    │                │
│  │  (端口 8083)  │         │   进程       │                │
│  └──────┬───────┘         └──────┬───────┘                │
│         │                        │                         │
│         │  创建任务               │  执行任务               │
│         ▼                        ▼                         │
│  ┌─────────────────────────────────────────┐              │
│  │          Redis (Asynq Queue)            │              │
│  │  ┌────────┐ ┌────────┐ ┌────────┐      │              │
│  │  │Critical│ │Default │ │  Low   │      │              │
│  │  │ Queue  │ │ Queue  │ │ Queue  │      │              │
│  │  └────────┘ └────────┘ └────────┘      │              │
│  └─────────────────────────────────────────┘              │
│         ▲                        │                         │
│         │                        ▼                         │
│  ┌──────┴───────┐         ┌─────────────┐                │
│  │  Scheduler   │────────▶│  Handler    │                │
│  │  (Cron定时)   │         │  (执行逻辑)  │                │
│  └──────────────┘         └─────────────┘                │
│                                  │                         │
│                                  ▼                         │
│                          ┌───────────────┐                │
│                          │ ExecutionEngine│               │
│                          │  (Pipeline)    │               │
│                          └───────────────┘                │
└─────────────────────────────────────────────────────────────┘
```

## 核心组件

### 1. TaskQueue (任务队列)

**位置**: `internal/worker/queue.go`

**功能**:
- 管理 Asynq 客户端和检查器
- 将任务加入 Redis 队列
- 查询队列统计信息
- 取消队列中的任务

**使用示例**:
```go
// 创建队列管理器
taskQueue := worker.NewTaskQueue("localhost:6379", "")

// 加入任务到队列
err := taskQueue.EnqueueExecuteTask(ctx, taskID, executionID, tenantID)

// 查询队列状态
stats, err := taskQueue.GetQueueStats("default")
fmt.Printf("队列中等待任务数: %d\n", stats.Pending)
```

**队列优先级**:
- `critical` (权重 6) - 紧急任务，优先执行
- `default` (权重 3) - 普通任务
- `low` (权重 1) - 低优先级任务

### 2. TaskHandler (任务处理器)

**位置**: `internal/worker/handler.go`

**功能**:
- 处理队列中的任务
- 解析任务载荷
- 调用 TaskService 执行任务
- 记录执行日志

**任务类型**:
- `transfer:execute` - 执行数据传输任务

**执行流程**:
```
1. 从队列中获取任务
2. 解析 JSON 载荷 (TaskID, ExecutionID, TenantID)
3. 调用 TaskService.ExecuteTask()
4. 返回执行结果（成功/失败）
5. 失败时自动重试（根据配置）
```

### 3. Scheduler (定时调度器)

**位置**: `internal/worker/scheduler.go`

**功能**:
- 基于 Cron 表达式定时触发任务
- 启动时加载所有定时任务
- 支持秒级调度精度
- 动态重载定时任务

**Cron 表达式格式**:
```
秒 分 时 日 月 周

示例：
"0 0 * * * *"      # 每小时整点执行
"0 */5 * * * *"    # 每5分钟执行
"0 0 2 * * *"      # 每天凌晨2点执行
"0 0 9 * * 1-5"    # 工作日上午9点执行
```

**使用示例**:
```go
// 创建调度器
scheduler := worker.NewScheduler(taskRepo, executionRepo, taskQueue)

// 启动调度器（自动加载所有定时任务）
scheduler.Start(ctx)

// 添加新的定时任务
scheduler.AddTask(ctx, task)

// 重新加载所有定时任务
scheduler.Reload(ctx)

// 停止调度器
scheduler.Stop()
```

## 启动 Worker

### 方式1: 直接运行

```bash
cd transfer/backend

# 设置环境变量
export PORT=8083
export DB_HOST=localhost
export DB_PORT=5432
export DB_USER=addp
export DB_PASSWORD=addp_password
export DB_NAME=addp
export DB_SCHEMA=transfer
export REDIS_HOST=localhost
export REDIS_PORT=6379
export CONCURRENT_TASKS=10
export MAX_RETRIES=3
export RETRY_DELAY=30s

# 启动 Worker
go run cmd/worker/main.go
```

### 方式2: 编译后运行

```bash
# 编译
cd transfer/backend
go build -o ../../bin/transfer-worker cmd/worker/main.go

# 运行
../../bin/transfer-worker
```

### 方式3: Docker 部署

```dockerfile
# Dockerfile
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o transfer-worker cmd/worker/main.go

FROM alpine:latest
COPY --from=builder /app/transfer-worker /usr/local/bin/
CMD ["transfer-worker"]
```

```bash
docker build -t addp-transfer-worker .
docker run -d \
  --name transfer-worker \
  -e DB_HOST=postgres \
  -e REDIS_HOST=redis \
  addp-transfer-worker
```

## 配置说明

### 环境变量

| 变量名 | 说明 | 默认值 | 示例 |
|--------|------|--------|------|
| `PORT` | API 服务端口 | 8083 | 8083 |
| `DB_HOST` | PostgreSQL 主机 | localhost | postgres |
| `DB_PORT` | PostgreSQL 端口 | 5432 | 5432 |
| `DB_USER` | 数据库用户 | addp | addp |
| `DB_PASSWORD` | 数据库密码 | addp_password | - |
| `DB_NAME` | 数据库名 | addp | addp |
| `DB_SCHEMA` | Schema 名称 | transfer | transfer |
| `REDIS_HOST` | Redis 主机 | localhost | redis |
| `REDIS_PORT` | Redis 端口 | 6379 | 6379 |
| `REDIS_PASSWORD` | Redis 密码 | - | - |
| `CONCURRENT_TASKS` | 并发任务数 | 10 | 20 |
| `MAX_RETRIES` | 最大重试次数 | 3 | 5 |
| `RETRY_DELAY` | 重试延迟 | 30s | 1m |
| `SYSTEM_SERVICE_URL` | System 服务地址 | http://localhost:8080 | - |
| `ENABLE_SERVICE_INTEGRATION` | 启用配置中心 | true | true |

### 重试策略

Worker 会自动重试失败的任务，重试延迟递增：

```
第1次重试: 30s 后
第2次重试: 60s 后
第3次重试: 90s 后
...
```

## 任务执行流程

### 1. API 触发任务

```bash
# 创建任务
curl -X POST http://localhost:8083/api/tasks \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "同步用户数据",
    "type": "sync",
    "mode": "batch",
    "batch_size": 1000,
    "config": {...}
  }'

# 启动任务
curl -X POST http://localhost:8083/api/tasks/1/start \
  -H "Authorization: Bearer $TOKEN"
```

### 2. Worker 处理流程

```
1️⃣ API Server 创建 TaskExecution 记录
2️⃣ API Server 将任务加入 Redis 队列
3️⃣ Worker 从队列中取出任务
4️⃣ Worker 解析载荷，获取 TaskID 和 ExecutionID
5️⃣ Worker 调用 ExecutionEngine 执行
6️⃣ ExecutionEngine 调用 Reader → Transform → Writer Pipeline
7️⃣ 每处理一批数据，保存 Checkpoint
8️⃣ 更新执行记录的指标（records_read, records_written）
9️⃣ 执行完成，更新状态为 success/failed
```

### 3. 定时任务执行流程

```
1️⃣ 创建任务时设置 schedule 字段（Cron 表达式）
2️⃣ Worker 启动时，Scheduler 加载所有定时任务
3️⃣ Scheduler 根据 Cron 表达式注册定时触发器
4️⃣ 时间到达时，Scheduler 创建 TaskExecution 记录
5️⃣ Scheduler 将任务加入队列
6️⃣ Worker 从队列中执行任务（后续同上）
```

## 监控与运维

### 查看队列状态

```bash
# 通过 API 查询队列统计
curl http://localhost:8083/api/worker/queue/stats
```

返回示例：
```json
{
  "queue": "default",
  "active": 2,
  "pending": 5,
  "scheduled": 3,
  "retry": 1,
  "archived": 0,
  "completed": 120,
  "processed": 125,
  "failed": 5
}
```

### 查看执行进度

```bash
# 获取实时进度
curl http://localhost:8083/api/executions/1/progress \
  -H "Authorization: Bearer $TOKEN"
```

返回示例：
```json
{
  "execution_id": 1,
  "task_id": 10,
  "status": "running",
  "records_read": 8500,
  "records_written": 8500,
  "progress": 85.0,
  "qps": 150.5,
  "duration_seconds": 56
}
```

### 查看执行日志

```bash
# 获取任务执行日志
curl http://localhost:8083/api/executions/1/logs?limit=100 \
  -H "Authorization: Bearer $TOKEN"
```

### 优雅关闭

Worker 支持优雅关闭，收到 SIGINT/SIGTERM 信号时：

1. 停止接收新任务
2. 等待当前任务执行完成
3. 保存最后的 Checkpoint
4. 关闭数据库和 Redis 连接
5. 退出进程

```bash
# 发送关闭信号
kill -SIGTERM <worker-pid>
```

## 故障处理

### 问题1: Worker 无法连接 Redis

**症状**: 日志显示 "dial tcp: connection refused"

**解决方案**:
```bash
# 检查 Redis 是否启动
redis-cli ping

# 检查 Redis 配置
echo $REDIS_HOST
echo $REDIS_PORT

# 重启 Redis
docker-compose restart redis
```

### 问题2: 任务重复执行

**原因**: 任务执行时间超过 Asynq 超时限制

**解决方案**:
```go
// 在 worker main.go 中增加超时配置
asynq.Config{
    Timeout: 10 * time.Minute, // 增加超时时间
}
```

### 问题3: 内存占用过高

**原因**: 批量处理数据量过大

**解决方案**:
```bash
# 减小批量大小
export BATCH_SIZE=500  # 默认 1000

# 减少并发任务数
export CONCURRENT_TASKS=5  # 默认 10
```

### 问题4: Checkpoint 未保存

**原因**: 任务中途被强制终止

**解决方案**:
- 使用优雅关闭（SIGTERM）而非强制杀死（SIGKILL）
- 检查 Checkpoint 保存频率配置

## 性能调优

### 1. 并发任务数

根据服务器 CPU 核心数调整：

```bash
# 4核服务器
export CONCURRENT_TASKS=8

# 8核服务器
export CONCURRENT_TASKS=16
```

### 2. 批量大小

根据网络带宽和数据库性能调整：

```bash
# 小数据量，低延迟
export BATCH_SIZE=500

# 大数据量，高吞吐
export BATCH_SIZE=5000
```

### 3. Checkpoint 频率

在 `engine.go` 中调整：

```go
const CheckpointInterval = 10  // 每处理 10 批保存一次

// 高频 Checkpoint（更安全但性能较低）
const CheckpointInterval = 5

// 低频 Checkpoint（性能更高但可能丢失更多进度）
const CheckpointInterval = 20
```

## 最佳实践

### 1. 任务设计

- ✅ 使用合理的批量大小（1000-5000）
- ✅ 设置任务超时时间（避免无限执行）
- ✅ 使用 Checkpoint 支持断点续传
- ✅ 合理设置重试次数（3-5次）

### 2. 定时任务

- ✅ 避免高峰期执行大任务
- ✅ 使用错峰调度（不同任务错开时间）
- ✅ 监控定时任务执行时长
- ✅ 设置任务执行超时告警

### 3. 监控告警

建议监控指标：
- 队列长度（pending tasks）
- 任务失败率（failed / total）
- 平均执行时长
- Worker CPU 和内存使用率
- Redis 连接数

### 4. 日志管理

- ✅ 使用结构化日志（JSON）
- ✅ 记录关键执行节点
- ✅ 定期清理过期日志
- ✅ 集成日志收集系统（ELK/Loki）

## 示例：创建定时同步任务

```bash
# 1. 创建定时任务
curl -X POST http://localhost:8083/api/tasks \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "每日用户数据同步",
    "type": "sync",
    "mode": "batch",
    "batch_size": 2000,
    "schedule": "0 0 2 * * *",
    "config": {
      "source": {
        "driver": "mysql",
        "host": "mysql-host",
        "database": "source_db",
        "table": "users"
      },
      "target": {
        "driver": "postgres",
        "host": "pg-host",
        "database": "target_db",
        "table": "users",
        "write_mode": "upsert"
      }
    },
    "mappings": [
      {"source_field": "id", "target_field": "user_id"},
      {"source_field": "name", "target_field": "username"}
    ]
  }'

# 2. Worker 自动加载定时任务
# 3. 每天凌晨2点自动执行
# 4. 查看执行历史
curl http://localhost:8083/api/tasks/1/executions \
  -H "Authorization: Bearer $TOKEN"
```

## 相关文档

- [DESIGN.md](./DESIGN.md) - 详细设计文档
- [README_IMPLEMENTATION.md](./README_IMPLEMENTATION.md) - 实现总结
- [Asynq 官方文档](https://github.com/hibiken/asynq)
- [Cron 表达式说明](https://pkg.go.dev/github.com/robfig/cron/v3)

---

**版本**: v0.3.0
**最后更新**: 2025-10-21
**维护者**: ADDP Team

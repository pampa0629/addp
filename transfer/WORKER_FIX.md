# Transfer Worker 修复文档

## 问题描述

用户报告数据传输任务一直处于"运行中"状态，但没有实际执行。

## 根本原因

Transfer 模块需要两个进程才能正常工作：
1. **Server** (`cmd/server/main.go`) - API 服务器，处理 HTTP 请求
2. **Worker** (`cmd/worker/main.go`) - 任务执行器，从 Redis 队列获取任务并执行

**问题**：
- Server 已启动并正常运行
- **Worker 没有启动** - 导致任务无法被处理
- Worker 启动时遇到两个错误：
  1. 数据库连接字符串格式错误
  2. Redis 服务未运行

## 修复步骤

### 1. 修复数据库连接格式错误

**文件**: `transfer/backend/cmd/worker/main.go`

**问题**: DBPort 是 string 类型，但使用了 `%d` 格式化

```go
// 错误的代码 (line 134)
dsn := fmt.Sprintf(
    "host=%s port=%d user=%s password=%s dbname=%s sslmode=disable search_path=%s",
    cfg.DBHost, cfg.DBPort, ...  // DBPort 是 string，不是 int
)

// 修复后 (使用 %s)
dsn := fmt.Sprintf(
    "host=%s port=%s user=%s password=%s dbname=%s sslmode=disable search_path=%s",
    cfg.DBHost, cfg.DBPort, ...
)
```

同样修复 line 102 的日志输出：
```go
// 修复前
log.Printf("📊 数据库: %s@%s:%d/%s (schema: %s)", ...)

// 修复后
log.Printf("📊 数据库: %s@%s:%s/%s (schema: %s)", ...)
```

### 2. 启动 Redis 服务

```bash
# 修复 docker-compose.yml 中的重复 driver 配置
# Line 376-377 有重复的 driver: local

# 启动 Redis
cd /Users/pampa/code/addp
docker-compose up -d redis
```

### 3. 启动 Worker 进程

```bash
cd /Users/pampa/code/addp/transfer/backend

# 启动 worker
nohup go run cmd/worker/main.go > /Users/pampa/code/addp/logs/transfer-worker.log 2>&1 &
echo $! > /Users/pampa/code/addp/.dev-pids/transfer-worker.pid
```

## 验证

Worker 成功启动后，日志应显示：

```
✅ 数据库连接成功
✅ 已注册连接器 - Readers: [...], Writers: [...]
✅ 定时调度器已启动，已注册 0 个定时任务
🚀 Transfer Worker 启动中...
📊 数据库: addp@localhost:5432/addp (schema: transfer)
📮 Redis: localhost:6379
🔧 并发数: 10
🔄 重试次数: 3
⏱️  重试延迟: 30s
✅ Transfer Worker 已启动，等待任务...
asynq: pid=44163 2025/10/22 12:38:21.800258 INFO: Starting processing
```

**关键指标**:
- 没有 "connection refused" 错误
- Asynq 显示 "Starting processing"
- 数据库连接成功

## 启动脚本更新建议

应该在 `scripts/dev-start.sh` 中添加 Transfer Worker 的启动：

```bash
# 在启动 Transfer Server 后添加
start_service "Transfer Worker" "/Users/pampa/code/addp/transfer/backend" \
    "go run cmd/worker/main.go" "transfer-worker"
```

## 测试步骤

1. 创建一个新的数据传输任务
2. 点击"启动"按钮
3. 观察 worker 日志：`tail -f logs/transfer-worker.log`
4. 应该看到任务被处理的日志
5. 任务状态应从 "running" 变为 "completed" 或 "failed"

## 注意事项

### 关于现有"running"状态的任务

如果任务在 Worker 启动前就被标记为"running"：
- 数据库中status = "running"
- 但 Redis 队列中没有对应的任务
- Worker **不会**自动重新处理这些任务

**解决方案**:
1. 手动将这些任务的状态改回 "pending"
2. 或者创建新任务进行测试

```sql
-- 重置卡住的任务
UPDATE transfer.tasks
SET status = 'pending'
WHERE status = 'running' AND id IN (1, 3);
```

## 相关文件

- [x] `transfer/backend/cmd/worker/main.go` - Worker 主程序（已修复）
- [x] `docker-compose.yml` - Docker 配置（已修复重复配置）
- [x] `common/config/loader.go` - 配置定义（DBPort 是 string 类型）

## 未来改进

1. **健康检查**: Worker 应该提供健康检查端点
2. **自动恢复**: 检测并恢复卡住的"running"任务
3. **监控**: 添加 Prometheus metrics 监控任务执行情况
4. **统一启动**: 将 Worker 加入 dev-start.sh 脚本

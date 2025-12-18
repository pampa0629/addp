# Transfer 模块队列命名迁移说明

## 修改概述

将 Transfer 模块的 Asynq 队列从通用命名 (`default`, `critical`, `low`) 迁移到带模块前缀的命名 (`transfer:default`, `transfer:critical`, `transfer:low`)。

## 修改原因

1. **避免多模块冲突**: 未来 Meta 等模块使用 Asynq 时，不会与 Transfer 队列冲突
2. **独立监控**: 可以按模块独立监控队列性能和统计
3. **独立扩容**: Worker 只处理所属模块的任务，扩容更精准
4. **符合最佳实践**: 多租户/多模块共享 Redis 应使用命名空间隔离

## 修改内容

### 1. Worker 队列配置

**文件**: `transfer/backend/cmd/worker/main.go` (Lines 86-90)

**修改前**:
```go
Queues: map[string]int{
    "critical": 6,
    "default":  3,
    "low":      1,
},
```

**修改后**:
```go
Queues: map[string]int{
    "transfer:critical": 6,
    "transfer:default":  3,
    "transfer:low":      1,
},
```

### 2. Backend 任务提交

**文件**: `transfer/backend/internal/worker/queue.go`

**修改内容**:
- `EnqueueExecuteTask`: 显式指定 `transfer:default` 队列
- `EnqueueScheduledTask`: 显式指定 `transfer:default` 队列
- `CancelTask`: 更新队列名为 `transfer:default`
- **新增**: `EnqueueExecuteTaskWithPriority` 方法支持按优先级提交任务

**新增方法示例**:
```go
// 根据优先级提交任务
err := taskQueue.EnqueueExecuteTaskWithPriority(ctx, taskID, executionID, tenantID, "critical")
```

支持的优先级:
- `"critical"` → 提交到 `transfer:critical` 队列
- `"default"` 或空字符串 → 提交到 `transfer:default` 队列
- `"low"` → 提交到 `transfer:low` 队列

### 3. 定时调度器

**文件**: `transfer/backend/internal/worker/scheduler.go`

**说明**: 无需修改，调度器调用 `EnqueueExecuteTask` 会自动使用新队列名

## Redis 队列结构变化

### 修改前:
```
asynq:default:pending
asynq:default:active
asynq:critical:pending
asynq:critical:active
asynq:low:pending
asynq:low:active
```

### 修改后:
```
asynq:transfer:default:pending
asynq:transfer:default:active
asynq:transfer:default:scheduled
asynq:transfer:default:retry
asynq:transfer:default:archived

asynq:transfer:critical:pending
asynq:transfer:critical:active
...

asynq:transfer:low:pending
asynq:transfer:low:active
...
```

## 迁移步骤

### 1. 开发环境迁移

如果 Redis 中有旧队列的待处理任务，需要清理:

```bash
# 1. 停止 Worker
docker stop transfer-worker

# 2. 清理旧队列 (确保没有重要任务)
redis-cli -a addp_redis DEL "asynq:{default}:pending"
redis-cli -a addp_redis DEL "asynq:{default}:active"
redis-cli -a addp_redis DEL "asynq:{critical}:pending"
redis-cli -a addp_redis DEL "asynq:{low}:pending"

# 3. 重新部署新版本
make dev-start
```

### 2. 生产环境迁移 (无缝切换)

**推荐方案**: 滚动更新

```bash
# 1. 更新 Docker 镜像
make docker-build-all

# 2. 使用 Docker Swarm 滚动更新 (自动零停机)
docker service update \
  --image addp-transfer-worker:latest \
  addp_transfer-worker

# 3. 监控更新进度
docker service ps addp_transfer-worker

# 4. 验证新队列
redis-cli -a addp_redis KEYS "asynq:transfer:*"
```

**注意**: 由于使用 `order: start-first` 更新策略，新版本 Worker 启动后才停止旧版本，确保零停机。

### 3. 清理旧队列 (可选)

等待所有 Worker 更新完成后，清理旧队列:

```bash
# 验证旧队列已空
redis-cli -a addp_redis LLEN "asynq:{default}:pending"
# 输出: (integer) 0

# 删除旧队列键
redis-cli -a addp_redis DEL "asynq:{default}:pending"
redis-cli -a addp_redis DEL "asynq:{default}:active"
redis-cli -a addp_redis DEL "asynq:{critical}:pending"
redis-cli -a addp_redis DEL "asynq:{low}:pending"
```

## 验证方法

### 1. 编译验证

```bash
cd transfer/backend

# 编译 Worker
go build -o /tmp/transfer-worker cmd/worker/main.go

# 编译 Backend
go build -o /tmp/transfer-server cmd/server/main.go
```

### 2. 队列验证

```bash
# 启动服务后，提交测试任务
curl -X POST http://localhost:8083/api/tasks/1/start \
  -H "Authorization: Bearer $TOKEN"

# 检查新队列是否有任务
redis-cli -a addp_redis KEYS "asynq:transfer:*"
# 应该输出: asynq:transfer:default:pending (或 active)

# 检查队列长度
redis-cli -a addp_redis LLEN "asynq:{transfer:default}:pending"

# 验证旧队列不再使用
redis-cli -a addp_redis KEYS "asynq:{default}:*"
# 应该输出: (empty array) 或只有旧数据
```

### 3. Worker 日志验证

启动 Worker 后检查日志:

```bash
docker logs -f transfer-worker
```

应该看到类似输出:
```
✅ 已注册连接器 - Readers: [...], Writers: [...]
🚀 Transfer Worker 启动中...
📮 Redis: localhost:6379
✅ Transfer Worker 已启动，等待任务...
✅ Task enqueued: id=xxx queue=transfer:default
🔄 开始执行任务 - TaskID: 1, ExecutionID: 2, TenantID: 1
```

## 未来模块规范

为保持一致性，未来其他模块使用 Asynq 时应遵循相同规范:

### Meta 模块示例:
```go
// Worker 配置
Queues: map[string]int{
    "meta:critical": 6,
    "meta:default":  3,
    "meta:low":      1,
}

// 提交任务
metaQueue.EnqueueScanTask(ctx, resourceID,
    asynq.Queue("meta:default"),
)
```

### 命名规范:
- 队列名格式: `{module_name}:{priority}`
- 优先级: `critical`, `default`, `low`
- 模块名: 使用小写模块名 (transfer, meta, export 等)

## 监控命令更新

更新监控脚本中的队列查询命令:

```bash
# 修改前
redis-cli -a addp_redis LLEN "asynq:{default}:pending"

# 修改后
redis-cli -a addp_redis LLEN "asynq:{transfer:default}:pending"
redis-cli -a addp_redis LLEN "asynq:{transfer:critical}:pending"
redis-cli -a addp_redis LLEN "asynq:{transfer:low}:pending"
```

## 回滚方案

如果需要回滚到旧版本:

```bash
# 1. 回滚代码
git revert <commit_hash>

# 2. 重新编译和部署
make docker-build-all
docker service update --image addp-transfer-worker:old_version addp_transfer-worker

# 3. 清理新队列 (如果必要)
redis-cli -a addp_redis DEL "asynq:{transfer:default}:pending"
```

## 总结

本次修改通过引入模块前缀命名，为 ADDP 平台的多模块 Asynq 使用奠定了基础。所有修改:
- ✅ 向后兼容 (通过滚动更新)
- ✅ 零停机部署
- ✅ 编译验证通过
- ✅ 文档已更新

未来 Meta、Export 等模块可直接参考此方案实现独立的任务队列。

# Transfer Worker 启动脚本更新

## 问题

Transfer Worker 没有被纳入到 `scripts/dev-start.sh` 和 `scripts/dev-stop.sh` 中，导致：
1. 运行 `make dev-start` 时 Worker 不会自动启动
2. 运行 `make dev-stop` 时 Worker 不会被停止
3. 需要手动启动和管理 Worker 进程

## 解决方案

已更新启动和停止脚本，将 Transfer Worker 完全集成到开发环境管理流程中。

## 修改文件

### 1. `scripts/dev-start.sh`

#### 添加 Worker 启动步骤（在 Transfer Backend 之后）

**Line 149-155**:
```bash
# 6. 启动 Transfer Worker
echo -e "${YELLOW}Step 6/7: 启动 Transfer Worker${NC}"
(cd transfer/backend && go run cmd/worker/main.go) > logs/transfer-worker.log 2>&1 &
TRANSFER_WORKER_PID=$!
echo -e "${GREEN}✓ Transfer Worker 启动中 (PID: $TRANSFER_WORKER_PID)${NC}"
echo "  注意: Transfer Worker 依赖 Redis，请确保 Redis 已启动"
echo ""
```

#### 更新步骤编号
- Gateway 从 "Step 6/6" 改为 "Step 7/7"

#### 添加 PID 显示和日志路径

**Line 253**:
```bash
echo "  Transfer Worker: $TRANSFER_WORKER_PID"
```

**Line 261**:
```bash
echo "  Transfer Worker: logs/transfer-worker.log"
```

#### 保存 Worker PID 到文件

**Line 275**:
```bash
echo $TRANSFER_WORKER_PID > .dev-pids/transfer-worker.pid
```

### 2. `scripts/dev-stop.sh`

#### 添加 Worker 停止逻辑（在 Transfer Backend 之后）

**Line 74-87**:
```bash
if [ -f ".dev-pids/transfer-worker.pid" ]; then
  TRANSFER_WORKER_PID=$(cat .dev-pids/transfer-worker.pid)
  if ps -p $TRANSFER_WORKER_PID > /dev/null 2>&1; then
    kill $TRANSFER_WORKER_PID
    # 等待进程真正退出
    for i in {1..10}; do
      if ! ps -p $TRANSFER_WORKER_PID > /dev/null 2>&1; then
        break
      fi
      sleep 0.5
    done
    echo "✓ Transfer Worker 已停止 (PID: $TRANSFER_WORKER_PID)"
  fi
fi
```

#### 添加 Worker 到残留进程清理列表

**Line 117**:
```bash
MODULES=(
  "system/backend|go run cmd/server/main.go"
  "manager/backend|go run cmd/server/main.go"
  "meta/backend|go run cmd/server/main.go"
  "transfer/backend|go run cmd/server/main.go"
  "transfer/backend|go run cmd/worker/main.go"  # 新增
  "gateway|go run cmd/gateway/main.go"
)
```

## 使用方法

### 启动所有服务（包括 Worker）

```bash
# 方式 1: 使用 Makefile
make dev-start

# 方式 2: 直接运行脚本
./scripts/dev-start.sh
```

启动后会显示：
```
Step 6/7: 启动 Transfer Worker
✓ Transfer Worker 启动中 (PID: 44163)
  注意: Transfer Worker 依赖 Redis，请确保 Redis 已启动

进程 PID:
  System:   xxxxx
  Manager:  xxxxx
  Meta:     xxxxx
  Transfer: xxxxx
  Transfer Worker: 44163
  Gateway:  xxxxx

日志文件:
  Transfer Worker: logs/transfer-worker.log
```

### 停止所有服务（包括 Worker）

```bash
# 方式 1: 使用 Makefile
make dev-stop

# 方式 2: 直接运行脚本
./scripts/dev-stop.sh
```

会显示：
```
✓ Transfer Backend 已停止 (PID: xxxxx)
✓ Transfer Worker 已停止 (PID: 44163)
✓ Gateway 已停止 (PID: xxxxx)
```

### 查看 Worker 日志

```bash
# 实时查看日志
tail -f logs/transfer-worker.log

# 查看最近日志
tail -50 logs/transfer-worker.log
```

## 注意事项

### Redis 依赖

Transfer Worker 依赖 Redis 作为任务队列。确保在启动 Worker 之前 Redis 已经运行：

```bash
# 启动 Redis (Docker)
docker-compose up -d redis

# 检查 Redis 状态
docker ps | grep redis
```

如果 Redis 未运行，Worker 会启动失败并在日志中显示：
```
ERROR: redis eval error: dial tcp [::1]:6379: connect: connection refused
```

### Worker 健康检查

目前 Worker 没有 HTTP 健康检查端点（与 Backend Server 不同）。判断 Worker 是否正常运行：

1. **检查进程**:
   ```bash
   ps aux | grep "worker.*main.go" | grep -v grep
   ```

2. **查看日志**:
   ```bash
   tail logs/transfer-worker.log
   ```

   正常启动的日志应显示：
   ```
   ✅ 数据库连接成功
   ✅ 已注册连接器
   ✅ 定时调度器已启动
   ✅ Transfer Worker 已启动，等待任务...
   asynq: INFO: Starting processing
   ```

3. **检查 PID 文件**:
   ```bash
   cat .dev-pids/transfer-worker.pid
   ```

## 启动顺序

完整的服务启动顺序（已优化）：

```
1. PostgreSQL (Docker)
   ↓
2. Redis (Docker)
   ↓
3. MinIO (Docker)
   ↓
4. System Backend
   ↓
5. Manager Backend + Meta Backend (并行)
   ↓
6. Transfer Backend
   ↓
7. Transfer Worker  ← 新增
   ↓
8. Gateway
   ↓
9. Frontend 服务 (可选)
```

## 测试验证

### 验证 Worker 已启动

```bash
# 1. 检查进程
ps aux | grep worker

# 2. 检查 PID 文件
cat .dev-pids/transfer-worker.pid

# 3. 检查日志
tail logs/transfer-worker.log

# 4. 创建测试任务（通过前端或 API）
# 观察 worker 日志是否有处理记录
```

### 验证 Worker 停止

```bash
# 运行停止脚本
./scripts/dev-stop.sh

# 确认进程已停止
ps aux | grep worker  # 应该无输出

# 确认 PID 文件已删除
ls .dev-pids/  # 应该看不到 transfer-worker.pid
```

## 相关文件

- [x] `scripts/dev-start.sh` - 启动脚本（已更新）
- [x] `scripts/dev-stop.sh` - 停止脚本（已更新）
- [x] `transfer/backend/cmd/worker/main.go` - Worker 主程序
- [x] `transfer/WORKER_FIX.md` - Worker 修复文档
- [x] `.dev-pids/transfer-worker.pid` - Worker PID 文件（自动生成）
- [x] `logs/transfer-worker.log` - Worker 日志文件（自动生成）

## 未来改进

1. **添加 Worker 健康检查**: 考虑添加简单的 HTTP 端点用于健康检查
2. **优雅关闭**: 改进 Worker 的信号处理，确保正在执行的任务能够完成
3. **监控集成**: 添加 Prometheus metrics 导出
4. **自动重启**: 在 Worker 异常退出时自动重启

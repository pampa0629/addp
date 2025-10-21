# Transfer 模块配置说明

## 配置中心模式

Transfer 模块使用**配置中心模式**，从 System 模块获取共享配置，无需本地 `.env` 文件。

## 配置来源

### 1. 从 System 模块获取（推荐）

当 `ENABLE_SERVICE_INTEGRATION=true` 时（默认），Transfer 会从 System 模块的 `/internal/config` 接口获取以下共享配置：

- `JWT_SECRET` - JWT 签名密钥
- `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME` - PostgreSQL 连接信息
- `ENCRYPTION_KEY` - 数据加密密钥
- `INTERNAL_API_KEY` - 服务间调用认证密钥

### 2. 环境变量配置

Transfer 模块特有的配置通过环境变量或项目根目录的 `.env` 文件设置：

```bash
# 服务配置
PORT=8083                          # Transfer 服务端口
DB_SCHEMA=transfer                 # PostgreSQL schema 名称

# 服务集成
SYSTEM_SERVICE_URL=http://localhost:8080
ENABLE_SERVICE_INTEGRATION=true    # 启用配置中心

# Redis 配置（任务队列）
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASSWORD=addp_redis

# 任务队列配置
WORKER_COUNT=5                     # Worker 数量
TASK_QUEUE_NAME=transfer:tasks     # 任务队列名称
CONCURRENT_TASKS=10                # 并发任务数

# 重试配置
MAX_RETRIES=3                      # 最大重试次数
RETRY_DELAY=30s                    # 重试延迟
```

## 配置加载流程

```
Transfer 启动
   ↓
读取环境变量（PORT, DB_SCHEMA, Redis 等）
   ↓
尝试从 System 获取共享配置 (/internal/config)
   ↓
   ├─ 成功 ✅
   │  └─ 使用 System 配置（JWT_SECRET, DB 连接等）
   │
   └─ 失败 ⚠️
      └─ 回退到环境变量中的 fallback 配置
```

## 开发环境启动

```bash
# 从项目根目录
./scripts/dev-start.sh

# 或使用 Makefile
make dev-start
```

## 生产环境部署

生产环境配置在 `docker-compose.yml` 中设置：

```yaml
transfer-backend:
  environment:
    - PORT=8083
    - DB_SCHEMA=transfer
    - SYSTEM_SERVICE_URL=http://system-backend:8080
    - ENABLE_SERVICE_INTEGRATION=true
    - REDIS_HOST=redis
    - REDIS_PORT=6379
    - REDIS_PASSWORD=${REDIS_PASSWORD}
```

## 配置优先级

1. **环境变量** (最高优先级)
2. **项目根目录 `.env` 文件**
3. **代码中的默认值** (最低优先级)

## 注意事项

- ⚠️ **不要在 transfer/backend/ 目录下创建 `.env` 文件**
- ✅ 所有配置应在项目根目录的 `.env` 文件中统一管理
- ✅ Transfer 特有的配置（如 Redis、Worker 数量）在根目录 `.env` 中设置
- ✅ 共享配置（如 JWT_SECRET）由 System 模块提供，无需在 Transfer 中重复配置

## 故障排除

如果 Transfer 无法从 System 获取配置：

1. 检查 System 服务是否正常运行：`curl http://localhost:8080/health`
2. 检查 `SYSTEM_SERVICE_URL` 环境变量是否正确
3. 查看日志：`tail -f logs/transfer-backend.log`
4. Transfer 会自动回退到环境变量中的 fallback 配置

## 参考文档

- [配置中心使用指南](../../docs/CONFIG_CENTER.md)
- [Common 模块文档](../../docs/COMMON_MODULE.md)

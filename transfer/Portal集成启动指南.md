# Transfer 模块 - Portal 集成启动指南

**更新日期**: 2025-10-21
**版本**: v1.2.0
**目标**: 在 Portal 中启用并使用 Transfer 模块

---

## 🎯 您的问题：重启后能在 Portal 看到并使用 Transfer 吗？

**答案**: **可以！** 但需要确保以下服务都已启动：

```
✅ PostgreSQL (数据库)
✅ Redis (任务队列)
✅ System Backend (认证 + 配置中心)
✅ Transfer Backend (传输服务)
✅ Transfer Frontend (传输 UI)
✅ Portal Frontend (统一入口)
```

---

## 🚀 完整启动流程

### 方式一：一键启动所有服务（推荐）

从项目根目录执行：

```bash
# 启动所有后端和前端服务（按正确顺序）
./scripts/dev-start.sh

# 或使用 Makefile
make dev-start
```

**脚本会自动启动**:

**后端服务**:
- System Backend (localhost:8080)
- Manager Backend (localhost:8081)
- Meta Backend (localhost:8082)
- **Transfer Backend (localhost:8083)** ← v1.2.0 新增
- Gateway (localhost:8000)

**前端服务**:
- Portal Frontend (localhost:5170) - **统一入口**
- System Frontend (localhost:5173)
- Manager Frontend (localhost:5174)
- Meta Frontend (localhost:5175)
- **Transfer Frontend (localhost:5176)** ← v1.2.0 新增

**启动特性**:
- ✅ 自动检查并创建 `transfer/backend/.env` 配置文件
- ✅ 按依赖顺序启动（System → Manager/Meta/Transfer → Gateway）
- ✅ 每个服务启动后等待健康检查通过
- ✅ 所有进程 PID 保存到 `.dev-pids/` 目录
- ✅ 日志输出到 `logs/` 目录

**验证启动成功**:
```bash
# 检查所有服务状态
make dev-health

# 手动检查后端
curl http://localhost:8080/health  # System
curl http://localhost:8081/health  # Manager
curl http://localhost:8082/health  # Meta
curl http://localhost:8083/health  # Transfer ← 新增
curl http://localhost:8000/health  # Gateway

# 手动检查前端
curl http://localhost:5170         # Portal
curl http://localhost:5176/transfer/  # Transfer Frontend
```

**停止所有服务**:
```bash
./scripts/dev-stop.sh

# 或使用 Makefile
make dev-stop
```

---

### 方式二：手动逐个启动（调试用）

#### 步骤 1: 启动基础设施

```bash
# 从项目根目录
docker-compose up -d postgres redis minio
```

#### 步骤 2: 启动 System Backend

```bash
cd system/backend
go run cmd/server/main.go
```

等待输出:
```
System service started on :8080
```

#### 步骤 3: 启动 Transfer Backend

```bash
cd transfer/backend

# 确认 .env 配置
cp .env.example .env  # 如果还没有 .env 文件
# 编辑 .env，设置 INTERNAL_API_KEY (与 System 保持一致)

go run cmd/server/main.go
```

等待输出:
```
INFO SystemClient initialized with internal API key
Transfer service started on :8083
```

#### 步骤 4: 启动前端

同上面方式一的步骤 2

---

## ⚙️ 关键配置检查

### 1. 内部 API Key 配置（必须！）

Transfer 需要与 System 通信，必须配置相同的 `INTERNAL_API_KEY`。

**检查 System 的密钥**:
```bash
# 查看项目根目录 .env
cat .env | grep INTERNAL_API_KEY
```

**配置到 Transfer**:
```bash
# 编辑 transfer/backend/.env
vim transfer/backend/.env

# 或复制模板
cd transfer/backend
cp .env.example .env
```

在 `.env` 中设置:
```bash
# ⚠️ 必须与 System 的 INTERNAL_API_KEY 保持一致
INTERNAL_API_KEY=dev-internal-key

# 或者生成新密钥（需要同步到 System）
# INTERNAL_API_KEY=$(openssl rand -base64 32)
```

**验证配置加载**:
```bash
# 启动 Transfer 后检查日志
# 应该看到：
# ✅ INFO SystemClient initialized with internal API key
#
# 如果看到：
# ⚠️ WARN SystemClient initialized without authentication
# 说明 INTERNAL_API_KEY 未正确配置
```

### 2. 数据库 Schema 初始化

Transfer 使用 PostgreSQL 的 `transfer` schema。

**检查 schema 是否存在**:
```bash
# 连接数据库
PGPASSWORD=addp_password psql -h localhost -U addp -d addp

# 检查 schema
\dn

# 应该看到:
# List of schemas
#   Name     |  Owner
# -----------+----------
#   public   | addp
#   system   | addp
#   manager  | addp
#   metadata | addp
#   transfer | addp       ← 应该存在

# 如果不存在，创建：
CREATE SCHEMA IF NOT EXISTS transfer AUTHORIZATION addp;

# 退出
\q
```

**GORM AutoMigrate** 会在 Transfer Backend 启动时自动创建表。

### 3. Redis 连接配置

Transfer 使用 Redis 作为任务队列。

**检查 Redis**:
```bash
# 测试连接
redis-cli -h localhost -p 6379 -a addp_redis ping
# 应返回: PONG

# 查看队列（启动后）
redis-cli -h localhost -p 6379 -a addp_redis
> KEYS transfer:*
```

**配置文件** (`transfer/backend/.env`):
```bash
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASSWORD=addp_redis
TASK_QUEUE_NAME=transfer:tasks
```

---

## 🧪 验证 Portal 集成

### 1. 访问 Portal

浏览器打开: **http://localhost:5170**

### 2. 登录

使用 System 模块的用户账号登录:
- 用户名: `admin` (或其他已创建用户)
- 密码: (您设置的密码)

### 3. 查看 Transfer 模块

登录后，您应该看到：

#### 首页卡片
```
┌─────────────────────────────────┐
│  🔴 数据传输                    │
│                                 │
│  数据导入、数据导出、任务调度   │
└─────────────────────────────────┘
```

点击卡片会跳转到 **传输任务** 页面。

#### 左侧菜单

```
📊 门户首页
📤 数据传输              ← 应该可点击（不再显示"开发中"）
  ├─ 传输任务           ← /transfer/tasks
  └─ 执行记录           ← /transfer/executions
```

### 4. 测试功能

#### 4.1 创建传输任务

1. 点击 **数据传输 → 传输任务**
2. 点击 **创建任务** 按钮
3. 填写表单:
   ```
   任务名称: 测试传输任务
   来源类型: PostgreSQL
   目标类型: PostgreSQL
   调度表达式: 0 0 * * * (每天零点)
   ```
4. 保存

#### 4.2 使用 SystemClient 集成

Transfer 现在支持从 System 动态获取数据源配置：

**方式 1: 使用 Resource ID（推荐）**
```json
{
  "name": "用户数据同步",
  "source_id": 1,        // ← 引用 System 的 resource_id
  "target_id": 2,
  "config": {
    "source": {
      "query": "SELECT * FROM users WHERE created_at > :last_sync_time"
    },
    "target": {
      "table": "users_backup",
      "write_mode": "append"
    }
  },
  "schedule_expression": "0 2 * * *"
}
```

**方式 2: 直接配置连接信息（Fallback）**
```json
{
  "name": "手动配置传输",
  "config": {
    "source": {
      "type": "jdbc",
      "driver": "postgresql",
      "host": "localhost",
      "port": 5432,
      "database": "source_db",
      "user": "user",
      "password": "pass",
      "query": "SELECT * FROM table"
    },
    "target": {
      "type": "jdbc",
      "driver": "mysql",
      "host": "localhost",
      "port": 3306,
      "database": "target_db",
      "user": "user",
      "password": "pass",
      "table": "target_table"
    }
  }
}
```

Transfer 会优先使用 `source_id`/`target_id` 从 System 获取资源配置，如果失败则 fallback 到任务配置。

#### 4.3 查看执行记录

1. 创建任务后，点击 **执行** 按钮
2. 切换到 **执行记录** 标签
3. 查看任务执行状态、日志、进度

---

## 📊 Portal 路由配置

Portal 已正确配置 Transfer 路由：

### moduleUrls 配置
```javascript
const moduleUrls = {
  system: 'http://localhost:5173',
  manager: 'http://localhost:5174',
  meta: 'http://localhost:5175',
  transfer: 'http://localhost:5176'  // ✅ Transfer URL
}
```

### 路由映射
```javascript
const transferPageMap = {
  'tasks': 'tasks',         // /transfer/tasks → http://localhost:5176/transfer/tasks
  'executions': 'executions', // /transfer/executions → http://localhost:5176/transfer/executions
  '': 'tasks'               // 默认页面
}
```

### 菜单处理
```javascript
// Portal.vue - handleMenuSelect()
if (module === 'transfer') {
  const actualPage = transferPageMap[page] !== undefined ? transferPageMap[page] : page
  if (actualPage) {
    url = `${moduleUrls[module]}/${module}/${actualPage}`
  }
  // 添加 token 参数
  if (token) {
    url = `${url}?token=${encodeURIComponent(token)}`
  }
  iframeUrl.value = url
}
```

### 导航处理
```javascript
// Portal.vue - navigateToModule()
const navigateToModule = (module) => {
  if (module === 'transfer') {
    handleMenuSelect('/transfer/tasks')  // 默认打开任务页面
  }
}
```

---

## 🐛 故障排查

### 问题 1: Portal 菜单中看不到 Transfer

**症状**: 左侧菜单中 "数据传输" 显示为灰色或禁用状态

**原因**: Portal 代码未更新

**解决**:
```bash
# 确认 portal/frontend/src/views/Portal.vue 已更新
grep "disabled" portal/frontend/src/views/Portal.vue

# 应该没有 <el-sub-menu index="transfer" disabled>
# 正确的是: <el-sub-menu index="transfer">

# 如果有 disabled，需要移除并重启 Portal
cd portal/frontend
npm run dev  # 重启开发服务器
```

### 问题 2: 点击 Transfer 菜单后 iframe 空白

**症状**: 点击菜单后右侧显示加载中或空白

**可能原因**:
1. Transfer Frontend 未启动
2. 端口冲突
3. 路由配置错误

**解决**:
```bash
# 1. 检查 Transfer Frontend 是否运行
curl http://localhost:5176/transfer/

# 2. 检查端口占用
lsof -i :5176

# 3. 启动 Transfer Frontend
cd transfer/frontend
npm install  # 首次
npm run dev

# 4. 检查浏览器控制台
# 打开开发者工具 (F12) → Console
# 查看是否有 CORS 错误或 404 错误
```

### 问题 3: Transfer 无法连接 System（401 错误）

**症状**: Transfer Backend 日志显示:
```
ERROR failed to get resource: 401 Unauthorized
WARN falling back to task config
```

**原因**: `INTERNAL_API_KEY` 不匹配或未配置

**解决**:
```bash
# 1. 检查 System 的密钥
cat .env | grep INTERNAL_API_KEY
# 输出: INTERNAL_API_KEY=dev-internal-key

# 2. 检查 Transfer 的密钥
cat transfer/backend/.env | grep INTERNAL_API_KEY
# 应该输出相同的值

# 3. 如果不同，更新 Transfer 配置
vim transfer/backend/.env
# 修改为与 System 相同的值

# 4. 重启 Transfer Backend
# Ctrl+C 停止，然后重新运行
cd transfer/backend
go run cmd/server/main.go
```

### 问题 4: 任务创建失败（数据库错误）

**症状**: 创建任务时报错 "schema transfer does not exist"

**原因**: PostgreSQL schema 未初始化

**解决**:
```bash
# 连接数据库
PGPASSWORD=addp_password psql -h localhost -U addp -d addp

# 创建 schema
CREATE SCHEMA IF NOT EXISTS transfer AUTHORIZATION addp;

# 退出并重启 Transfer Backend
\q

# GORM AutoMigrate 会自动创建表
cd transfer/backend
go run cmd/server/main.go
```

### 问题 5: Redis 连接失败

**症状**: Transfer 启动时报错 "failed to connect to redis"

**原因**: Redis 未启动或密码错误

**解决**:
```bash
# 1. 检查 Redis 是否运行
docker-compose ps redis

# 2. 启动 Redis
docker-compose up -d redis

# 3. 测试连接
redis-cli -h localhost -p 6379 -a addp_redis ping

# 4. 检查密码配置
cat transfer/backend/.env | grep REDIS
# 应该显示:
# REDIS_HOST=localhost
# REDIS_PORT=6379
# REDIS_PASSWORD=addp_redis
```

---

## 🔒 安全检查清单

在生产环境部署前，请确认：

- [ ] `INTERNAL_API_KEY` 已更换为强密钥（不使用 `dev-internal-key`）
- [ ] `JWT_SECRET` 已更换为 64+ 字符的随机字符串
- [ ] 数据库密码已更换（不使用 `addp_password`）
- [ ] Redis 密码已更换（不使用 `addp_redis`）
- [ ] MinIO 密码已更换（不使用 `minioadmin`）
- [ ] 所有服务间通信使用 HTTPS（生产环境）
- [ ] 防火墙已配置，仅允许必要端口访问
- [ ] 日志级别设置为 `warn` 或 `error`（避免泄露敏感信息）

**生成强密钥示例**:
```bash
# 生成 INTERNAL_API_KEY (32 字节)
openssl rand -base64 32

# 生成 JWT_SECRET (64 字节)
openssl rand -base64 64

# 生成数据库密码 (16 字节)
openssl rand -base64 16
```

---

## 📈 性能优化建议

### 1. Worker 配置

根据服务器资源调整 Worker 数量：

```bash
# transfer/backend/.env
WORKER_COUNT=5           # CPU 核心数
CONCURRENT_TASKS=10      # 并发任务数
```

**建议**:
- 小型服务器 (2 核): WORKER_COUNT=2, CONCURRENT_TASKS=5
- 中型服务器 (4 核): WORKER_COUNT=4, CONCURRENT_TASKS=10
- 大型服务器 (8+ 核): WORKER_COUNT=8, CONCURRENT_TASKS=20

### 2. 批量大小

```bash
DEFAULT_BATCH_SIZE=1000  # 每批处理行数
```

**建议**:
- 小数据量 (<100万行): 1000
- 中数据量 (100万-1000万行): 5000
- 大数据量 (>1000万行): 10000

### 3. 超时配置

```bash
TASK_TIMEOUT=3600s       # 任务超时时间（1小时）
```

根据任务复杂度调整。

---

## 📚 相关文档

- [SystemClient 集成指南](./SystemClient集成指南.md) - 如何使用 SystemClient 获取资源配置
- [内部 API 认证配置指南](./内部API认证配置指南.md) - 服务间认证详细说明
- [连接器使用指南](./连接器使用指南.md) - JDBC, File, S3 连接器配置
- [Worker 实现文档](./WORKER.md) - 任务队列和异步执行机制
- [快速开始](./快速开始.md) - Transfer 模块基础使用

---

## ✅ 启动成功标志

如果一切正常，您应该看到：

### 1. 终端输出

**Transfer Backend**:
```
INFO SystemClient initialized with internal API key system_url=http://localhost:8080
INFO Asynq worker started workers=5
INFO Transfer service started on :8083
```

**Portal Frontend**:
```
VITE v4.4.9  ready in 1234 ms

➜  Local:   http://localhost:5170/
➜  Network: use --host to expose
```

**Transfer Frontend**:
```
VITE v4.4.9  ready in 567 ms

➜  Local:   http://localhost:5176/transfer/
➜  Network: use --host to expose
```

### 2. 浏览器访问

1. 访问 http://localhost:5170
2. 登录成功
3. 看到 **数据传输** 卡片和菜单（不再显示"开发中"）
4. 点击后 iframe 加载 Transfer UI
5. 可以创建任务、查看执行记录

### 3. API 测试

```bash
# 健康检查
curl http://localhost:8083/health
# 返回: {"status":"ok"}

# 获取任务列表（需要 token）
TOKEN="your-jwt-token"
curl -H "Authorization: Bearer $TOKEN" http://localhost:8083/api/tasks
# 返回: {"total":0,"data":[]}
```

---

## 🎉 总结

**您的问题的答案**: **是的！重启后可以在 Portal 中看到并使用 Transfer 模块！**

**前提条件**:
1. ✅ 所有必需服务已启动（见上文"完整启动流程"）
2. ✅ `INTERNAL_API_KEY` 已正确配置
3. ✅ PostgreSQL `transfer` schema 已创建
4. ✅ Portal Frontend 已更新并重启

**快速启动命令**:
```bash
# 从项目根目录，一键启动所有服务（后端 + 前端）
./scripts/dev-start.sh

# 等待启动完成后，访问 Portal
open http://localhost:5170
```

**脚本已自动启动**:
- ✅ 所有后端服务（System, Manager, Meta, Transfer, Gateway）
- ✅ 所有前端服务（Portal, System FE, Manager FE, Meta FE, Transfer FE）

**无需手动启动前端！**

**验证方式**:
- Portal 左侧菜单显示 "数据传输"（可点击）
- 首页卡片显示 "数据传输"（可点击）
- 点击后加载 Transfer UI
- 可以创建和执行传输任务

**当前版本**: v1.2.0
**最后更新**: 2025-10-21

---

**祝您使用愉快！** 🚀

如有问题，请查看 [故障排查](#-故障排查) 部分，或检查日志文件：
- Transfer Backend: `logs/transfer-backend.log`
- Portal: 浏览器控制台 (F12)

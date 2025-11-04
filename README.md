
# 全域数据平台 (All Domain Data Platform)

企业级数据平台，提供数据接入、管理、元数据治理和数据传输的完整解决方案。

## 📋 目录

- [架构概述](#架构概述)
- [快速开始](#快速开始)
- [服务模块](#服务模块)
- [技术栈](#技术栈)
- [开发指南](#开发指南)
- [部署说明](#部署说明)
- [API 文档](#api-文档)

## 🏗️ 架构概述

ADDP 采用微服务架构，每个模块独立开发、部署和扩展：

```
┌─────────────────────────────────────────────────────────────┐
│                         客户端                                │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
                  ┌──────────────┐
                  │   Gateway    │  API 网关 (8000)
                  │   (待实现)    │
                  └──────┬───────┘
                         │
        ┌────────────────┼────────────────┐
        ▼                ▼                ▼
   ┌─────────┐     ┌──────────┐    ┌──────────┐
   │ System  │     │ Manager  │    │   Meta   │
   │  8080   │     │   8081   │    │   8082   │
   └────┬────┘     └─────┬────┘    └────┬─────┘
        │                │              │
        └────────────────┼──────────────┘
                         ▼
                   ┌──────────┐
                   │ Transfer │  数据传输 (8083)
                   └──────────┘
                         │
        ┌────────────────┼────────────────┐
        ▼                ▼                ▼
  ┌──────────┐    ┌──────────┐    ┌──────────┐
  │PostgreSQL│    │  Redis   │    │  MinIO   │
  │   5432   │    │   6379   │    │  9002    │
  └──────────┘    └──────────┘    └──────────┘
```

## 🚀 快速开始

### 前置要求

- Docker 和 Docker Compose
- Go 1.21+ (开发模式)
- Node.js 18+ (前端开发)
- Make (可选，用于快捷命令)

### 一键启动（仅 System 模块）

```bash
# 1. 克隆项目
git clone <repository-url>
cd addp

# 2. 初始化配置
make init

# 3. 启动服务
make up

# 4. 查看状态
make status
```

访问地址：
- **System 后端**: http://localhost:8080
- **System 前端**: http://localhost:8090

### 启动完整平台

```bash
# 启动所有服务（包括 Gateway, Manager, Meta, Transfer）
make up-full

# 查看所有服务状态
make status

# 查看日志
make logs
```

## 📦 服务模块

### 1. System 模块 ✅ 已实现
**端口**: 8080 (后端), 8090 (前端)

核心系统能力，提供：
- 用户认证和授权 (JWT)
- 多租户管理
- 用户管理 (CRUD)
- 审计日志记录
- 资源配置管理 (加密存储)
- PostgreSQL 数据存储 (system schema)

**文档**: [system/CLAUDE.md](system/CLAUDE.md)

### 2. Gateway 模块 🚧 待实现
**端口**: 8000

API 网关服务：
- 统一入口和路由
- 请求转发和负载均衡
- 认证传递
- 限流和熔断

**文档**: [gateway/README.md](gateway/README.md)

### 3. Manager 模块 🚧 待实现
**端口**: 8081 (后端), 8091 (前端)

数据管理服务：
- 数据源管理 (MySQL, PostgreSQL, S3, HDFS 等)
- 文件上传和目录组织
- 多格式数据预览 (CSV, JSON, Parquet 等)
- 权限控制

**文档**: [manager/README.md](manager/README.md)

### 4. Meta 模块 🚧 待实现
**端口**: 8082 (后端), 8092 (前端)

元数据管理服务：
- 元数据自动解析
- 元数据存储和查询
- 数据血缘追踪
- 元数据检索
- 可扩展的类型插件

**文档**: [meta/README.md](meta/README.md)

### 5. Transfer 模块 🚧 待实现
**端口**: 8083 (后端), 8093 (前端)

数据传输服务：
- 数据导入/导出
- 数据同步
- 任务调度 (Cron)
- 数据转换
- 断点续传

**文档**: [transfer/README.md](transfer/README.md)

## 💻 技术栈

### 后端
- **语言**: Go 1.21+
- **框架**: Gin (HTTP), GORM (ORM)
- **数据库**: PostgreSQL 15, SQLite (System), Redis
- **存储**: MinIO / S3
- **任务队列**: Asynq (基于 Redis)

### 前端
- **框架**: Vue 3 + TypeScript
- **构建工具**: Vite
- **UI 框架**: Element Plus
- **状态管理**: Pinia
- **路由**: Vue Router

### 基础设施
- **容器化**: Docker, Docker Compose
- **反向代理**: Nginx (生产环境)

## 🛠️ 开发指南

### 开发 System 模块

```bash
# 启动基础设施（PostgreSQL/Redis/MinIO/Elasticsearch）
make infra-up   # 或 ./scripts/infra-up.sh

# 后端开发（自动为本地 Redis 设置认证：REDIS_PASSWORD=addp_redis）
make dev-system

# 前端开发
cd system/frontend
npm install
npm run dev
```

### 开发其他模块

```bash
# Manager 模块
make dev-manager

# Meta 模块
make dev-meta

# Transfer 模块
make dev-transfer   # 将自动为 Redis 设置 REDIS_PASSWORD=addp_redis

# Gateway 模块
make dev-gateway
```

### 常用命令

```bash
make help            # 显示所有可用命令
make ports-validate  # 校验端口分配策略（见 docs/PORTS.md）
make build           # 编译所有服务
make test            # 运行测试
make logs            # 查看日志
make status          # 查看服务状态
make clean           # 清理编译产物
make db-shell        # 连接数据库
make redis-cli       # 连接 Redis
```

### 代码规范

```bash
make fmt             # 格式化代码
make lint            # 代码检查
```

## 🚢 部署说明

### Docker Compose 部署（推荐）

```bash
# 生产环境配置
cp .env.example .env
# 编辑 .env，修改密码和密钥

# 启动服务
make up              # 仅 System 模块
make up-full         # 完整平台

# 停止服务
make down
```

### 独立服务部署

每个模块都可以独立部署：

```bash
# 进入模块目录
cd manager

# 独立部署
docker-compose up -d
```

### 非容器化部署

```bash
# 编译（默认 release 输出到 dist）
make build

# 运行（以当前平台 linux-amd64 为例，按需替换）
./dist/release/backend/system/linux-amd64/system      # System 模块
./dist/release/backend/manager/linux-amd64/manager    # Manager 模块
./dist/release/backend/meta/linux-amd64/meta          # Meta 模块
./dist/release/backend/transfer/linux-amd64/transfer  # Transfer 模块
./dist/release/backend/gateway/linux-amd64/gateway    # Gateway 模块
```

### 向量检索配置

- 配置 `.env` 中的 `VECTOR_DB_*` 变量，指向启用了 `pgvector` 扩展的 PostgreSQL 库，默认会自动创建 `vector_store.document_embeddings` 表。
- 设置在线推理服务地址与密钥：`EMBEDDING_SERVICE_BASE_URL`、`EMBEDDING_SERVICE_API_KEY` 以及各模态模型。默认配置指向阿里云灵积（DashScope）多模态向量接口，可在灵积控制台创建 DashScope API Key 并填入 `EMBEDDING_SERVICE_API_KEY`。
- Meta 模块在元数据扫描时调用推理服务生成文档向量并写入 PgVector；Manager 模块会对查询语句做向量化并在搜索结果中附加 `vector_hits` 列表。
- 若未配置推理服务或 PgVector 连接，向量化相关能力会自动跳过，不影响原有全文检索。

## 📡 API 文档

### System 模块 API

**认证**
```bash
POST /api/auth/register    # 注册
POST /api/auth/login       # 登录
```

**用户管理** (需认证)
```bash
GET    /api/users          # 用户列表
GET    /api/users/:id      # 用户详情
PUT    /api/users/:id      # 更新用户
DELETE /api/users/:id      # 删除用户
```

**日志管理** (需认证)
```bash
GET    /api/logs           # 日志列表
GET    /api/logs/:id       # 日志详情
```

**资源管理** (需认证)
```bash
GET    /api/resources      # 资源列表
POST   /api/resources      # 创建资源
PUT    /api/resources/:id  # 更新资源
DELETE /api/resources/:id  # 删除资源
```

详细 API 文档见各模块 README。

## 🔧 配置说明

### 环境变量

主要配置项在 `.env` 文件中：

```bash
# JWT 密钥（必须修改）
JWT_SECRET=your-secret-key

# 数据库
POSTGRES_PASSWORD=your-db-password

# Redis
REDIS_PASSWORD=your-redis-password

# MinIO
MINIO_ROOT_PASSWORD=your-minio-password
```

### 端口分配

| 服务 | 端口 | 说明 |
|------|------|------|
| Gateway | 8000 | API 网关 |
| System Backend | 8080 | System API |
| System Frontend | 8090 | System UI |
| Manager Backend | 8081 | Manager API |
| Manager Frontend | 8091 | Manager UI |
| Meta Backend | 8082 | Meta API |
| Meta Frontend | 8092 | Meta UI |
| Transfer Backend | 8083 | Transfer API |
| Transfer Frontend | 8093 | Transfer UI |
| PostgreSQL | 5432 | 数据库 |
| Redis | 6379 | 缓存/队列 |
| MinIO API | 9002 | 对象存储 |
| MinIO Console | 9003 | MinIO 管理界面 |
| Elasticsearch REST | 9200 | 全文检索 API |
| Elasticsearch Transport | 9300 | 节点通信 |

## 🗄️ 数据库

### Schema 组织

PostgreSQL 使用 schema 隔离各模块数据：
- `manager` - Manager 模块数据表
- `metadata` - Meta 模块数据表
- `transfer` - Transfer 模块数据表

System 模块使用独立的 SQLite 数据库。

### 数据库操作

```bash
# 连接数据库
make db-shell

# 运行迁移
make db-migrate

# 备份数据库
make backup

# 恢复数据库
make restore FILE=backups/xxx.sql
```

## 📊 监控和健康检查

```bash
# 检查所有服务健康状态
make health

# 查看服务状态
make status

# 查看日志
make logs
make logs-system
make logs-manager
```

## 🤝 开发规范

### 添加新 API

1. 在 `internal/models/` 定义数据模型
2. 在 `internal/repository/` 实现数据访问
3. 在 `internal/service/` 实现业务逻辑
4. 在 `internal/api/` 创建 HTTP 处理器
5. 在 `internal/api/router.go` 注册路由

### 模块间通信

使用 HTTP REST API 进行服务间调用，通过环境变量配置服务地址：

```go
systemClient := &SystemClient{
    BaseURL: os.Getenv("SYSTEM_SERVICE_URL"),
}
```

## 📖 更多文档

- [项目架构说明](CLAUDE.md)
- [System 模块文档](system/CLAUDE.md)
- [Manager 模块文档](manager/README.md)
- [Meta 模块文档](meta/README.md)
- [Transfer 模块文档](transfer/README.md)
- [Gateway 模块文档](gateway/README.md)

## 🐛 故障排查

### 服务无法启动

```bash
# 查看日志
make logs

# 检查端口占用
lsof -i :8080
lsof -i :5432

# 清理并重启
make clean-all
make up
```

### 数据库连接失败

```bash
# 检查 PostgreSQL 状态
docker-compose ps postgres

# 查看 PostgreSQL 日志
docker-compose logs postgres

# 重启数据库
docker-compose restart postgres
```

### MinIO 无法访问

```bash
# 检查 MinIO 状态
docker-compose ps minio

# 初始化 MinIO
make minio-setup
```

## 📝 许可证

[添加许可证信息]

## 👥 贡献

欢迎贡献代码！请阅读贡献指南。

## 📧 联系方式

[添加联系方式]

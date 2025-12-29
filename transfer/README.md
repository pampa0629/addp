# Transfer 数据传输模块

> ADDP 平台的数据传输中枢，负责导入、导出、同步任务的执行和管理

## 📖 文档说明

- **README.md** (本文件) - 快速入门和功能概览
- **[CLAUDE.md](./CLAUDE.md)** - 详细技术文档，包含架构设计、开发指南、常见场景和插件开发

## 🎯 核心功能

- **数据传输**: 导入、导出、同步（支持 16+ 种数据源）
- **任务调度**: Asynq 异步队列 + Cron 定时调度
- **流式处理**: 批处理、流式、微批次三种执行模式
- **断点续传**: Checkpoint 机制支持恢复执行
- **插件架构**: Reader/Writer/Transform 可插拔插件体系

## 🚀 快速开始

### 方式 1: 开发模式（推荐）

```bash
# 启动基础设施
bash scripts/infra/up.sh

# 启动 Transfer 模块
bash scripts/dev/start.sh -transfer
```

- 后端: http://localhost:8083
- 前端: http://localhost:5175

### 方式 2: Docker 部署

```bash
cd transfer
docker-compose up -d
```

## 📡 主要 API 端点

```
任务管理:   GET/POST/PUT/DELETE /api/tasks
任务执行:   GET /api/tasks/:id/executions
任务触发:   POST /api/tasks/:id/trigger
执行查询:   GET /api/executions/:id
对象存储:   GET /api/object-storage/browse
连接器列表: GET /api/operators
```

完整 API 文档请查看 [CLAUDE.md#API 端点](./CLAUDE.md#api-端点)

## 🛠️ 支持的数据源

**关系型数据库**: PostgreSQL, MySQL, Doris, ClickHouse（JDBC）
**NoSQL**: MongoDB
**对象存储**: MinIO, S3
**空间数据**: Shapefile, GeoJSON, GeoPackage, SpatiaLite
**文件格式**: CSV, JSON, TXT

## 📦 项目结构

```
transfer/
├── backend/
│   ├── cmd/
│   │   ├── server/main.go    # API 服务入口
│   │   └── worker/main.go    # 任务 Worker 入口
│   ├── internal/
│   │   ├── api/              # HTTP 处理层
│   │   ├── service/          # 业务逻辑层
│   │   ├── worker/           # 任务队列 & 调度
│   │   └── models/           # 数据模型
│   └── plugins/
│       ├── readers/          # Reader 插件
│       └── writers/          # Writer 插件
├── frontend/
│   └── src/
│       ├── views/
│       │   ├── Dashboard.vue # 仪表盘
│       │   └── TaskForm.vue  # 任务创建
│       └── api/
└── README.md
```

详细架构请查看 [CLAUDE.md#关键架构](./CLAUDE.md#关键架构)

## ⚙️ 配置说明

Transfer 使用项目根目录 `.env` 统一管理配置（无需本地 `.env`）：

```bash
# API 服务端口
TRANSFER_PORT=8083

# Worker 并发配置
WORKER_CONCURRENCY=10

# 传输批大小
TRANSFER_BATCH_SIZE=1000

# 重试配置
TRANSFER_MAX_RETRY=3
```

详细配置说明请查看 [../docs/addp配置介绍.md](../docs/addp配置介绍.md)

## 🔧 常见开发场景

### 创建导入任务

```bash
curl -X POST http://localhost:8083/api/tasks \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "导入用户数据",
    "type": "import",
    "source_id": 10,
    "target_id": 1,
    "config": {
      "reader": {"type": "csv", "path": "/data/users.csv"},
      "writer": {"type": "postgres", "table": "users"}
    }
  }'
```

### 调试任务执行失败

```bash
# 1. 查看失败的执行记录
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8083/api/executions?status=failed"

# 2. 查看执行日志
curl -H "Authorization: Bearer <token>" \
  http://localhost:8083/api/executions/<execution_id> | jq '.logs'

# 3. 查看 Worker 日志
tail -f logs/transfer-worker.log
```

### 添加新的数据源支持

详见 [CLAUDE.md#场景 3：添加新的数据源支持](./CLAUDE.md#场景-3添加新的数据源支持)

### 创建定时同步任务

```bash
curl -X POST http://localhost:8083/api/tasks \
  -d '{
    "name": "每日数据同步",
    "type": "sync",
    "schedule": "0 2 * * *",
    "config": {...}
  }'
```

详见 [CLAUDE.md#场景 5：创建定时同步任务](./CLAUDE.md#场景-5创建定时同步任务)

## 🐛 常见问题

### 如何优化大数据传输性能?

使用 PostgreSQL COPY Writer（比 INSERT 快 5-10 倍）+ 调整批大小 + 并行 Worker。详见 [CLAUDE.md#场景 4：优化大数据传输性能](./CLAUDE.md#场景-4优化大数据传输性能)

### 如何开发自定义插件?

Transfer 支持 Reader/Writer/Transform 插件化开发。详见 [CLAUDE.md#插件式连接器开发](./CLAUDE.md#插件式连接器开发)

### 任务失败了怎么办?

检查执行记录中的 error_msg 字段 + 查看 Worker 日志 + 手动重试。详见 [CLAUDE.md#场景 2：调试任务执行失败问题](./CLAUDE.md#场景-2调试任务执行失败问题)

更多问题请查看 [CLAUDE.md](./CLAUDE.md)

## 📚 相关文档

- **[CLAUDE.md](./CLAUDE.md)** - 完整技术文档（架构、开发指南、API 详解、场景示例）
- **[../docs/addp技术栈规约.md](../docs/addp技术栈规约.md)** - 技术栈和依赖版本
- **[../docs/addp配置介绍.md](../docs/addp配置介绍.md)** - 配置中心说明
- **[../docs/addp新增存储引擎指南.md](../docs/addp新增存储引擎指南.md)** - 新增数据源指南
- **[../system/CLAUDE.md](../system/CLAUDE.md)** - System 模块说明（认证、引擎管理）

---

Copyright © 2025 ADDP Team

# Develop 开发工作台

> ADDP 平台的开发工作台，提供 SQL 查询、算子工作流和 Notebook 任务管理

## 📖 文档说明

- **README.md** (本文件) - 快速入门和功能概览
- **[CLAUDE.md](./CLAUDE.md)** - 详细技术文档，包含架构设计、执行流程、API 详解和常见场景

## 🎯 核心功能

- **SQL 工作台**: Monaco Editor + 多数据库支持（PostgreSQL、MySQL 等）
- **GIS 工作流**: 可视化编辑和执行空间计算工作流（21个 GeoPython Workflow 算子）
- **Notebook 任务**: 上传、执行、下载和显式重绑定 Notebook 引擎，并查看统一执行历史
- **算子管理**: 聚合工作流运行时动态算子，供工作流编辑器使用
- **执行历史**: 保存所有执行记录，支持历史回溯

## 🚀 快速开始

### 方式 1: 开发模式（推荐）

```bash
# 启动基础设施
bash scripts/infra/up.sh

# 启动 Develop 模块
bash scripts/dev/start.sh -develop
```

- 后端: http://localhost:8185
- 前端: http://localhost:5178

### 方式 2: Docker 部署

```bash
docker compose up -d develop-backend develop-frontend jupyter-engine
```

## 📡 主要 API 端点

```
SQL执行:     POST /api/v1/develop/execute
任务定义:    GET/POST /api/v1/develop/task-definitions
存储引擎绑定: GET /api/v1/develop/task-definitions/{id}/storage-engine-bindings
存储引擎重绑定: PUT /api/v1/develop/task-definitions/{id}/storage-engine-bindings/{source_engine_id}
任务执行:    POST /api/v1/develop/task-definitions/:id/execute
算子发现:   GET /api/v1/develop/workflow-engines/{workflow_engine_id}/operators
执行历史:   GET /api/v1/develop/executions
Notebook引擎: GET /api/v1/develop/notebook-engines
Kernel发现: GET /api/v1/develop/notebook-engines/{engine_id}/kernels
Notebook:   GET /api/v1/develop/notebooks
Notebook上传: POST /api/v1/develop/notebooks/upload
Notebook运行绑定: PUT /api/v1/develop/notebooks/{id}/runtime-binding
```

完整 API 文档请查看 [CLAUDE.md#常见开发场景](./CLAUDE.md#常见开发场景)

## 🔐 认证方式

- 用户 API 使用 System 签发的短期 User Access Token，并通过 canonical AuthContext 解析
- 所有 API 请求须携带 `Authorization: Bearer <token>` 头
- 数据按租户自动隔离

## 🐛 常见问题

### SQL 执行超时怎么办？

提高 `timeout` 参数（默认 30 秒，最长 5 分钟）或优化 SQL 查询。详见 [CLAUDE.md#场景1：执行SQL查询](./CLAUDE.md#场景-1执行-sql-查询)

### 工作流执行失败？

检查错误日志：`tail -f logs/develop-backend.log`，或查看 [CLAUDE.md#场景3：调试工作流执行失败](./CLAUDE.md#场景-3调试工作流执行失败)

### 内存不足错误？

大数据集建议使用 Spark Workflow 运行时，并在执行时绑定已注册的 Spark 通用引擎资源。详见 [CLAUDE.md#3-python-workflow-内存管理](./CLAUDE.md#3-python-workflow-内存管理)

## 📚 相关文档

- **[CLAUDE.md](./CLAUDE.md)** - 完整技术文档（核心架构、执行流程、常见场景）
- **[engines/python-workflow/README.md](../engines/python-workflow/README.md)** - GeoPython Workflow 工作流运行时
- **[system/CLAUDE.md](../system/CLAUDE.md)** - 数据库连接与认证
- **[common/dbbridge/README.md](../common/dbbridge/README.md)** - 数据库桥接库

---

Copyright © 2025 ADDP Team

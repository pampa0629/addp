# Develop 开发工作台

> ADDP 平台的在线开发环境，提供 SQL 查询、GIS 工作流、Jupyter Notebook 和算子管理

## 📖 文档说明

- **README.md** (本文件) - 快速入门和功能概览
- **[CLAUDE.md](./CLAUDE.md)** - 详细技术文档，包含架构设计、执行流程、API 详解和常见场景

## 🎯 核心功能

- **SQL 工作台**: Monaco Editor + 多数据库支持（PostgreSQL、MySQL 等）
- **GIS 工作流**: 可视化编辑和执行空间计算工作流（21个 Python Workflow 算子）
- **Jupyter Notebook**: 在线 Python 数据分析和机器学习环境
- **算子管理**: 聚合所有计算引擎算子，供工作流编辑器使用
- **执行历史**: 保存所有执行记录，支持历史回溯

## 🚀 快速开始

### 方式 1: 开发模式（推荐）

```bash
# 启动基础设施
bash scripts/infra/up.sh

# 启动 Develop 模块
bash scripts/dev/start.sh -develop
```

- 后端: http://localhost:8084
- 前端: http://localhost:5177

### 方式 2: Docker 部署

```bash
cd develop
docker-compose up -d
```

## 📡 主要 API 端点

```
SQL执行:     POST /api/v1/sql/execute
工作流管理: GET/POST /api/v1/workflows
工作流执行: POST /api/v1/workflows/:id/execute
算子发现:   GET /api/v1/operators
执行历史:   GET /api/v1/executions
Notebook:   POST/GET /api/v1/notebooks
```

完整 API 文档请查看 [CLAUDE.md#常见开发场景](./CLAUDE.md#常见开发场景)

## 🔐 认证方式

- JWT 认证（与 System 模块集成）
- 所有 API 请求须携带 `Authorization: Bearer <token>` 头
- 数据按租户自动隔离

## 🐛 常见问题

### SQL 执行超时怎么办？

提高 `timeout` 参数（默认 30 秒，最长 5 分钟）或优化 SQL 查询。详见 [CLAUDE.md#场景1：执行SQL查询](./CLAUDE.md#场景-1执行-sql-查询)

### 工作流执行失败？

检查错误日志：`tail -f logs/develop-backend.log`，或查看 [CLAUDE.md#场景3：调试工作流执行失败](./CLAUDE.md#场景-3调试工作流执行失败)

### 内存不足错误？

大数据集建议使用 Spark 工作流引擎。详见 [CLAUDE.md#3-python-workflow-内存管理](./CLAUDE.md#3-python-workflow-内存管理)

## 📚 相关文档

- **[CLAUDE.md](./CLAUDE.md)** - 完整技术文档（核心架构、执行流程、常见场景）
- **[engines/python_workflow/README.md](../engines/python_workflow/README.md)** - Python Workflow 空间计算引擎
- **[system/CLAUDE.md](../system/CLAUDE.md)** - 数据库连接与认证
- **[common/dbbridge/README.md](../common/dbbridge/README.md)** - 数据库桥接库

---

Copyright © 2025 ADDP Team

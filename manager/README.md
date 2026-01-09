# Manager 数据管理模块

> 全域数据平台的数据源接入和文件管理服务

## 📖 文档说明

- **README.md** (本文件) - 快速入门和功能概览
- **[CLAUDE.md](./CLAUDE.md)** - 详细技术文档，包含预览插件架构、MVT 缓存机制、开发场景指南

## 🎯 核心功能

- **存储引擎管理**: 8 种引擎支持（PostgreSQL、MySQL、Doris、ClickHouse、MongoDB、Apache Spark、MinIO、S3）
- **数据预览**: 插件化架构，支持多种格式（CSV、JSON、Parquet、Shapefile、图片等）
- **空间数据可视化**: MVT 矢量瓦片服务 + 三层缓存（PostgreSQL + Redis + 实时生成）
- **数据检索**: 基于 Meilisearch + 向量数据库的混合检索（全文检索 + 语义检索）
- **元数据展示**: 与 Meta 模块集成，展示数据目录树

## 🚀 快速开始

### 开发模式（推荐）

```bash
# 启动基础设施
bash scripts/infra/up.sh

# 启动 Manager 模块（含依赖的 System 模块）
bash scripts/dev/start.sh -manager
```

- 后端: http://localhost:8081
- 前端: http://localhost:5174

### Docker 部署

```bash
cd manager
docker-compose up -d
```

## 📊 支持的数据类型

### 数据库
PostgreSQL、MySQL、Doris、ClickHouse、MongoDB、Apache Spark

### 对象存储
MinIO、S3

### 文件格式
CSV、JSON、Parquet、Excel、Shapefile、GeoJSON、图片、PDF、文本

详细格式支持请查看 [CLAUDE.md#预览插件架构](./CLAUDE.md#预览插件架构核心设计)

## 📡 主要 API 端点

```
数据预览:   GET  /api/v1/preview
MVT 瓦片:   GET  /api/v1/mvt/{z}/{x}/{y}
存储引擎:   GET/POST/PUT/DELETE /api/v1/engines
元数据查询: GET  /api/v1/metadata/nodes
数据检索:   GET  /api/v1/search
```

完整 API 文档和请求示例请查看 [CLAUDE.md#API端点](./CLAUDE.md)

## 🏗️ 核心架构

### 预览插件系统（可扩展）
- 按优先级链式调用预览插件
- 支持 PostgreSQL、MySQL、MongoDB、ClickHouse 等
- 支持外部插件动态加载

### MVT 三层缓存
1. Quick View (PostgreSQL) - 持久化缓存
2. Spatial Preview (Redis) - 热数据缓存 (1 小时)
3. 实时生成 (ST_AsMVT 查询)

详细架构请查看 [CLAUDE.md#预览插件架构](./CLAUDE.md#预览插件架构核心设计)

## 🐛 常见问题

### 数据预览失败？

```bash
# 1. 检查存储引擎连接
curl -H "Authorization: Bearer <token>" \
  http://localhost:8081/api/v1/engines/<engine_id>/test

# 2. 查看后端日志
tail -f logs/manager-backend.log
```

### 地图加载慢？

启用 Quick View 批量缓存（推荐）：

```bash
curl -X POST -H "Authorization: Bearer <token>" \
  http://localhost:8081/api/v1/quick-views/batch \
  -d '{"engine_id": 1, "schema": "public", "table": "large_table"}'
```

更多问题请查看 [CLAUDE.md#常见开发场景](./CLAUDE.md#常见开发场景)

## 📚 相关文档

- **[CLAUDE.md](./CLAUDE.md)** - 完整技术文档（预览插件架构、MVT 缓存机制、开发场景指南）
- **[../docs/addp技术栈规约.md](../docs/addp技术栈规约.md)** - 技术栈和依赖版本
- **[../docs/addp数据库插件系统.md](../docs/addp数据库插件系统.md)** - 存储引擎插件系统
- **[../common-frontend/README.md](../common-frontend/README.md)** - 前端共享组件使用指南

---

Copyright © 2025 ADDP Team

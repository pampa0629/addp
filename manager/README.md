# Manager 数据管理模块

> 全域数据平台的数据管理和预览服务

## 📖 文档说明

- **README.md** (本文件) - 快速入门和功能概览
- **[CLAUDE.md](./CLAUDE.md)** - 详细技术文档，包含预览插件架构、空间快显与瓦片缓存机制、开发场景指南

## 🎯 核心功能

- **数据探查**: 浏览 8 种存储引擎中的数据（PostgreSQL、MySQL、Doris、ClickHouse、MongoDB、Apache Spark、MinIO、S3）
- **数据预览**: 插件化架构，支持多种格式（CSV、JSON、Parquet、Shapefile、图片等）
- **空间数据可视化**: 基础预览 + 快显模式 + 瓦片缓存结果，当前第一阶段实现以 PostGIS + MVT 为主
- **数据检索**: 基于 Meilisearch + 向量数据库的混合检索（全文检索 + 语义检索）
- **向量化**: 对可支持的数据项生成向量表示，支持资源树一次性执行、可调度向量化任务和语义检索消费
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
数据预览:     GET  /api/v1/manager/preview
快显能力:     GET  /api/v1/manager/quick-view/capability?locator={ResourceLocator}
快显GeoJSON: GET  /api/v1/manager/quick-view/geojson?locator={ResourceLocator}
快显MVT瓦片: GET  /api/v1/manager/quick-view/tiles/{z}/{x}/{y}.mvt?locator={ResourceLocator}
瓦片缓存任务: GET  /api/v1/manager/tile_cache_tasks
瓦片缓存结果: GET  /api/v1/manager/tile_cache
数据检索:     GET  /api/v1/manager/search
```

完整 API 文档和请求示例请查看 [CLAUDE.md#API端点](./CLAUDE.md)

## 🏗️ 核心架构

### 预览插件系统（可扩展）
- 按优先级链式调用预览插件
- 支持 PostgreSQL、MySQL、MongoDB、ClickHouse 等
- 支持外部插件动态加载

### 快显与瓦片缓存
1. `quick_view` - 快显偏好，只记录 item 的预览模式偏好。
2. `tile_cache` - 瓦片缓存结果状态，记录存储引用、格式、范围和层级。
3. `tile_cache_tasks` - 瓦片缓存生成任务定义，TaskProvider `task_type=tile_cache_generation`。
4. 当前第一阶段瓦片格式以 MVT 为主，可通过 PostGIS `ST_AsMVT` 生成。
5. 当前 `tile_cache_generation` 由 Manager Backend 内部执行；若后续瓦片生成主要计算负载进入 Manager 进程、需要多执行器并发或引入 GIS 计算引擎，应切换为唯一的 Manager Worker 或 GIS 执行运行时。

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

地图加载慢时，先看快显能力诊断：动态 MVT 走源表转换慢路径或瓦片返回慢路径超时建议时，应从空间预览页进入“执行快显优化”，创建并执行 `quick_view_optimization` 任务；优化结果 ready 后，低层级或大范围稳定浏览再通过“生成瓦片缓存”创建 `tile_cache_generation` 任务。若当前 item 已有 ready 瓦片缓存结果，预览页展示“切换快显”；若仍使用动态 MVT，可同时保留“执行快显优化”和“生成瓦片缓存”的引导入口。

更多问题请查看 [CLAUDE.md#常见开发场景](./CLAUDE.md#常见开发场景)

## 📚 相关文档

- **[CLAUDE.md](./CLAUDE.md)** - 完整技术文档（预览插件架构、空间快显与瓦片缓存机制、开发场景指南）
- **[manager/docs/快显概念总览.md](./docs/快显概念总览.md)** - Manager 快显价值、概念、分工和边界
- **[manager/docs/快显规范与技术路线.md](./docs/快显规范与技术路线.md)** - 快显、瓦片缓存结果和生成任务的表结构、API 契约与技术路线
- **[manager/docs/快显问题与改造思路.md](./docs/快显问题与改造思路.md)** - 快显当前问题、根因判断和改造顺序
- **[manager/docs/向量化概念说明.md](./docs/向量化概念说明.md)** - Manager 向量化价值、对象边界、任务边界和结果边界
- **[manager/docs/向量化能力说明.md](./docs/向量化能力说明.md)** - Manager 向量化结果、任务、执行、检索和 UI 行为说明
- **[../docs/addp技术栈规约.md](../docs/addp技术栈规约.md)** - 技术栈和依赖版本
- **[../docs/addp数据库插件系统.md](../docs/addp数据库插件系统.md)** - 存储引擎插件系统
- **[../common-frontend/README.md](../common-frontend/README.md)** - 前端共享组件使用指南

---

Copyright © 2025 ADDP Team

# Manager 数据管理模块

> 全域数据平台的数据管理和预览服务

## 📖 文档说明

- **README.md** (本文件) - 快速入门和功能概览
- **[CLAUDE.md](./CLAUDE.md)** - 模块定位、核心 API、开发规则和文档导航
- **[manager/docs/数据预览与资源树实现规范.md](./docs/数据预览与资源树实现规范.md)** - 数据探查、资源树、预览 API、PreviewResolver 和 PreviewProvider 当前实现规范
- **[manager/docs/数据预览语义协议.md](./docs/数据预览语义协议.md)** - `content.kind`、`preview_material`、`frontend_renderer` 等预览响应语义
- **[manager/docs/三维模型与高斯泼溅预览说明.md](./docs/三维模型与高斯泼溅预览说明.md)** - 三维模型、3D Tiles 和 3DGS 预览、快显任务与状态说明
- **[manager/docs/存储流与原始下载语义.md](./docs/存储流与原始下载语义.md)** - `storage-stream`、`downloads/file` 与 DownloadPlan 语义

## 🎯 核心功能

- **数据探查**: 浏览 8 种存储引擎中的数据（PostgreSQL、MySQL、Doris、ClickHouse、MongoDB、Apache Spark、MinIO、S3）
- **数据预览**: 插件化架构，支持多种格式（CSV、JSON、Parquet、Shapefile、图片等）
- **空间数据可视化**: 基础预览 + 快显模式 + 瓦片缓存结果 + TIFF/COG 栅格快显，当前矢量主路径以 PostGIS + MVT 为核心实现
- **数据检索**: 基于 Meilisearch + 向量数据库的混合检索（全文检索 + 语义检索）
- **向量化**: 对可支持的数据项生成向量表示，支持资源树一次性执行、可调度向量化任务和语义检索消费
- **元数据展示**: 与 Meta 模块集成，展示资源树 node / item facts

## 🚀 快速开始

### 开发模式（推荐）

```bash
# 启动基础设施
bash scripts/infra/up.sh

# 启动 Manager 模块（含依赖的 System、Meta、Gateway、Console）
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

详细预览契约请查看 [数据预览与资源树实现规范](./docs/数据预览与资源树实现规范.md) 和 [数据预览语义协议](./docs/数据预览语义协议.md)。

## 📡 主要 API 端点

```
数据预览:     GET  /api/v1/manager/preview
存储流预览:   GET  /api/v1/manager/storage-stream
原始下载:     GET  /api/v1/manager/downloads/file?locator={ResourceLocator}
上传数据:     POST /api/v1/manager/uploads
导入数据:     POST /api/v1/manager/imports
导出数据:     POST /api/v1/manager/exports
导出下载:     GET  /api/v1/manager/exports/{id}/file
快显能力:     GET  /api/v1/manager/quick-view/capability?locator={ResourceLocator}
快显GeoJSON: GET  /api/v1/manager/quick-view/geojson?locator={ResourceLocator}
快显MVT瓦片: GET  /api/v1/manager/quick-view/tiles/{z}/{x}/{y}.mvt?locator={ResourceLocator}
栅格快显 COG内容:  GET  /api/v1/manager/raster_cog/{id}/content
栅格快显 COG生成任务: GET  /api/v1/manager/raster_cog_tasks
栅格快显 COG结果:  GET  /api/v1/manager/raster_cog
瓦片缓存任务: GET  /api/v1/manager/vector_tile_cache_tasks
瓦片缓存结果: GET  /api/v1/manager/vector_tile_cache
数据检索:     GET  /api/v1/manager/search
```

完整 API 文档和请求示例请查看 [数据预览与资源树实现规范](./docs/数据预览与资源树实现规范.md) 和 [快显实现规范](./docs/快显实现规范.md)。

## 🏗️ 核心架构

### 预览插件系统（可扩展）
- 资源树事实、搜索和刷新统一由 Meta resource-tree API 提供
- Manager 预览、下载、快显和任务入口统一以 ResourceLocator 定位
- 通过 PreviewResolver 选择 PreviewProvider
- 支持 PostgreSQL、MySQL、MongoDB、ClickHouse 等
- 支持外部插件动态加载
- 预览响应按 `frontend_renderer -> preview_material -> content.kind` 选择前端组件

### 向量化与混合检索
1. 向量化对象是 data item，资源树 node 只作为批量选择范围。
2. 资源树 item / node 向量化是一次性 execution，不创建任务定义。
3. 独立向量化页面创建 `manager.embedding_tasks`，TaskProvider `task_type=embedding`。
4. 向量化结果写入 `manager.embeddings`，搜索只消费 `status=ready` 且模型、维度匹配的结果。
5. Meilisearch 负责全文和属性检索，pgvector 负责向量命中，Manager 搜索服务负责融合结果。

### 快显、矢量物化视图与瓦片缓存
1. `preview_state` - 预览状态，只记录 item 的预览模式偏好和基础预览 / 快显各自的视角状态。
2. `vector_materialized_view` - 矢量物化视图结果，只登记 Manager 创建并拥有生命周期的 3857 优化目标。
3. `vector_materialized_view_tasks` - 矢量物化视图任务定义，TaskProvider `task_type=vector_materialized_view_generation`。
4. `raster_cog` - 栅格快显 COG生成结果，只登记 Manager 创建并上传到 infra MinIO 的 COG 副本。
5. `raster_cog_tasks` - 栅格快显 COG生成任务定义，TaskProvider `task_type=raster_cog_generation`，当前不声明自身定时调度能力。
6. `vector_tile_cache` - 瓦片缓存结果状态，记录存储引用、格式、范围和层级。
7. `vector_tile_cache_tasks` - 瓦片缓存生成任务定义，TaskProvider `task_type=vector_tile_cache_generation`。
8. `model_3d_glb` / `model_3d_glb_tasks` - 单体三维模型 GLB 快显结果和任务定义，TaskProvider `task_type=model_3d_glb_generation`。
9. `gaussian_splat_ksplat` / `gaussian_splat_ksplat_tasks` - 3DGS - KSplat 快显结果和任务定义，TaskProvider `task_type=gaussian_splat_ksplat_generation`。
10. MVT 是瓦片格式，进入 `config.tile.format=mvt`，不是任务类型；COG 是 TIFF profile 或 Manager COG 生成结果，不是新的基础 format。
11. 当前 `vector_tile_cache_generation`、`vector_materialized_view_generation` 和 `raster_cog_generation` 由 Manager Backend 内部执行；COG 生成使用 Manager 预处理 GDAL `source_uri` / `target_uri` / `gdal_env`，再通过 `WorkflowRuntimeProvider.InvokeOperator("tiff_to_cog")` direct 调用 Python Workflow，并直接写入 infra MinIO 的单一路线。

COG 生成运行要求：

- NFS / NAS 源：Python Workflow 运行环境必须能访问 Manager 根据 engine `mount_path` / `export_path` 派生出的挂载路径。
- MinIO / S3 源：Manager 为源对象生成 presigned URL，Python 通过 GDAL `/vsicurl/` 读取。
- 目标 COG：Python 通过 GDAL `/vsis3/` 写入 Manager infra MinIO，Manager 负责登记 `raster_cog`。

三维模型、3D Tiles 和高斯泼溅的预览路线请查看 [三维模型与高斯泼溅预览说明](./docs/三维模型与高斯泼溅预览说明.md)。详细架构请查看 [数据预览与资源树实现规范](./docs/数据预览与资源树实现规范.md)。

## 🐛 常见问题

### 数据预览失败？

```bash
# 1. 检查存储引擎连接
curl -H "Authorization: Bearer <token>" \
  http://localhost:8180/api/v1/system/engines/<engine_id>/test

# 2. 查看后端日志
tail -f logs/manager-backend.log
```

### 地图加载慢？

地图加载慢时，先看快显能力诊断：动态 MVT 走源表转换慢路径或瓦片返回慢路径超时建议时，应从空间预览页进入“执行矢量物化视图”，创建并执行 `vector_materialized_view_generation` 任务；矢量物化视图结果 ready 后，低层级或大范围稳定浏览再通过“生成瓦片缓存”创建 `vector_tile_cache_generation` 任务。若当前 item 已有 ready 瓦片缓存结果，预览页展示“切换快显”；若仍使用动态 MVT，可同时保留“执行矢量物化视图”和“生成瓦片缓存”的引导入口。

更多预览与下载边界请查看 [数据预览语义协议](./docs/数据预览语义协议.md) 和 [存储流与原始下载语义](./docs/存储流与原始下载语义.md)。

## 📚 相关文档

- **[CLAUDE.md](./CLAUDE.md)** - Manager 模块定位、核心 API、开发规则和文档导航
- **[manager/docs/数据预览与资源树实现规范.md](./docs/数据预览与资源树实现规范.md)** - Manager 数据探查、资源树、预览 API 和插件编排实现规范
- **[manager/docs/数据预览语义协议.md](./docs/数据预览语义协议.md)** - Manager 预览响应材料和前端渲染语义
- **[manager/docs/存储流与原始下载语义.md](./docs/存储流与原始下载语义.md)** - Manager 存储叶子流式预览和逻辑对象原始下载语义
- **[manager/docs/快显概念说明.md](./docs/快显概念说明.md)** - Manager 快显、矢量物化视图和瓦片缓存的价值、概念、分工和边界
- **[manager/docs/快显实现规范.md](./docs/快显实现规范.md)** - 快显、矢量物化视图、瓦片缓存结果和生成任务的表结构、API 契约、技术路线与验证记录
- **[manager/docs/向量化概念说明.md](./docs/向量化概念说明.md)** - Manager 向量化价值、对象边界、任务边界和结果边界
- **[manager/docs/向量化能力说明.md](./docs/向量化能力说明.md)** - Manager 向量化结果、任务、执行、检索和 UI 行为说明
- **[manager/docs/数据库架构.md](./docs/数据库架构.md)** - Manager schema 表清单、关系、索引和数据流
- **[../docs/spec/addp技术栈规约.md](../docs/spec/addp技术栈规约.md)** - 技术栈和依赖版本
- **[../docs/spec/addp数据引擎扩展指南.md](../docs/spec/addp数据引擎扩展指南.md)** - 数据引擎扩展指南
- **[../docs/spec/addp引擎插件接口规范.md](../docs/spec/addp引擎插件接口规范.md)** - 引擎插件接口规范
- **[../common-frontend/README.md](../common-frontend/README.md)** - 前端共享组件使用指南

---

Copyright © 2025 ADDP Team

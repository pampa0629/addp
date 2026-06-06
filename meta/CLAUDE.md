# Meta 模块说明

## 模块定位

Meta 模块负责元数据扫描、元数据存储、元数据查询、对象元数据提取、扫描任务调度、扫描运行记录和资产发现接口。它是 Manager 数据探查、Asset 自动发现和搜索索引的重要来源。

## 技术栈与端口

- 后端：Go + Gin + GORM，默认端口 `8082`，环境变量 `META_BACKEND_PORT`。
- 前端：Vue 3 + Element Plus，开发端口 `5175`，启动脚本环境变量 `META_FE_PORT`。
- 数据库：PostgreSQL `meta` schema。
- 依赖：System、Redis、Meilisearch、MinIO，可选 pgvector/嵌入服务。

## 重要目录

```text
meta/
├── backend/
│   ├── cmd/server/main.go
│   ├── internal/api/          # 统一 Handler 与路由
│   ├── internal/models/       # node、item、scan_task、cleanup
│   ├── internal/service/      # 应用服务门面、装配、查询代理、刷新入口
│   ├── internal/scanadapter/  # catalog strategy dispatch 与 object/file path/ref group adapter
│   ├── internal/scanruntime/  # tabular/branch-leaf/object/file 扫描运行时
│   ├── internal/scanprocessor/# item 持久化、deep enrich、content hash、索引调度
│   ├── internal/metatest/     # Meta 后端测试基础设施
│   ├── internal/search/       # Meilisearch indexer
│   ├── internal/worker/       # Asynq 扫描 Worker
│   └── docs/                  # Swagger 产物
├── docs/
│   ├── 数据库架构.md
│   └── tables/
└── frontend/src/
    ├── views/                 # MetadataScan、TaskMonitor
    └── api/
```

## Meta 扫描分层约定

Meta 扫描链路按“通用规则、Meta 编排、Catalog 规划、内容增强、Attributes 落库”分层。后续改造应优先保持以下边界，避免把同一类逻辑散落到多个扫描入口。

### 当前主线状态

- 扫描入口统一到 manual execution、scheduled ScanTask、item refresh、startup/unscanned execution 几类入口；不同入口进入 Meta 后必须先解析为统一 `ScanScope`，再进入 scanner / detector / processor 主线。
- `trigger_type` 只表达 `manual` / `scheduled`。手动 API 只接受空值或 `manual`；`scheduled` 只能由 Meta scheduler 创建。
- `source` 只记录触发模块，例如 `meta`、`manager`、`console`、`transfer`；不得把来源、调度器、前后端通道或业务场景塞进 `trigger_type`。
- `ScanTask` 是扫描调度定义权威，保存 scope、schedule、enabled、owner 和最近执行摘要；执行历史统一进入 `common.task_executions`。
- `scan_tasks.schedule` 是 Cron 表达式；Console-facing 策略模式字段为 `schedule_mode`，只存在于策略载荷，不进入 `scan_tasks` 表。
- System 不知道 Meta，也不保存 Meta 扫描策略。System engine 注册或编辑时的默认扫描体验由 Console 编排：Console 调用 System 保存 engine，再调用 Meta 维护 engine 绑定的 `ScanTask` 或创建一次 manual execution。
- Manager preview 和 Meta 查询 API 只读取已落库 attributes，不暗中触发扫描、不写 attributes、不构建 `access_index`。
- preprocessing、cleanup、查询快捷入口是独立专题，不应混入扫描主线中半截实现。

### 与 common/dataitem 的边界

`common/dataitem` 是跨模块的 data item 规则层，只处理纯事实输入，不打开引擎资源、不读取对象内容、不依赖 Meta 落库模型。

- `common/dataitem` 复用 `common/datatype` 的 `DataType` 和 `common/format` 的 format / layout 规则，不定义新的 data type、type info 或 attributes 分区。
- `common/dataitem/types.go` 定义 `Candidate`、`ResolveInput`、`FormatRule`、`ResolvedItem`、`ItemDescriptor`。
- `common/dataitem/resolve.go` 负责 `ResolveItems`、multi/whole/single 布局识别、`DescriptorFromAttributes`。
- `common/dataitem/format.go` 负责基于显式 format、MIME、名称、路径做基础格式和数据类型推断。

如果需要基于内容前缀、schema、内部文件或外部引擎读取来修正格式和类型，不应直接塞进 `common/dataitem`；应在 Meta 的 enrich 层读取内容后，把识别结果作为事实写回 `DetectedItem` / attributes。

### Meta 内部目录职责

- `internal/metaitem/`：Meta item 识别编排层。负责 resolver 注册、排序、claims 去重，并把 `dataitem.ResolvedItem` 转换成 Meta 可继续增强和落库的 `DetectedItem`。典型文件：`resolver.go`、`single_resource.go`、`multi_table_enrichment.go`。
- `internal/metacatalog/`：Catalog 资源规划层。负责把对象存储或文件系统 catalog entry 规范化为 `StorageResource`，规划 single/composite item 的路径、父节点、fingerprint 和基础 attributes。典型文件：`storage_resource.go`、`object_items.go`。
- `internal/metaenrich/`：内容增强层。凡是需要打开内容、读取 schema、读取容器内部、读取文件前缀来确认格式的逻辑，都应在这里或通过这里统一提供。典型文件：`table_file.go`、`container_children.go`、`single_format.go`。
- `internal/metaattr/`：Attributes 规范写入层。负责把 `DetectedItem` 和增强结果合并成标准落库结构：`item`、`storage`、`type_info`、`format_info`、`access_index`、`capabilities`。典型文件：`item_attributes.go`、`attributes.go`。
- `internal/metapath/`：路径语义工具层。负责 bucket、object、prefix、filesystem path 的切分、规范化和拼接，扫描逻辑不要重复手写路径规则。

Meta 只负责把正式规范中的 data type、type info 和横切事实写入落库 attributes。新增 `data_type`、`type_info.*` 字段或 `capabilities.*` 命名空间前，必须先更新平台概念和规范文档，不得只在 Meta helper 中新增自由字段。

`common/datatype` 只处理自身结构和 JSON payload 的相互转换，不承载 attributes 路径语义。Meta 或其他 MetaClient 消费方需要先取出 `type_info.*`、`capabilities.*` 等标准分区，再调用 `datatype.*FromPayload`；写入 attributes 时也由 `metaattr` 决定分区路径。

### metaattr 输入边界

`internal/metaattr/` 是 Meta 模块内部的 attributes 写入和规范化层，不是扫描编排层、engine 适配层或展示 DTO 层。新增 helper 时只接收三类输入：

- `models.JSONMap` / `map[string]interface{}` 这类 attributes map。
- `common/datatype` 中的通用事实结构，例如 `TableInfo`、`FieldInfo`、`SpatialInfo`。
- 为 attributes 写入定义的轻量输入结构，例如 data item attributes input、dynamic schema attributes input。

`metaattr` 不应接收 `metaitem.DetectedItem`、`plugin.CatalogFacts`、`plugin.IndexFacts`、`models.SpatialMetadata`、Manager DTO 等上层复杂类型。上层模块如果拿到 engine / format / query / 展示模型，应先在本层转换为轻量输入或 `datatype` 事实结构，再调用 `metaattr`，避免 attributes helper 反向依赖扫描、engine 或展示边界。

动态 schema 记录集合的 attributes 写入使用 `BuildDynamicSchemaAttributes` / `ApplyDynamicSchemaStatistics` 这条路径；字段画像写入 `type_info.table.fields`，采样和索引事实分别写入 `capabilities.statistics` / `capabilities.indexing`，不得写入 `type_info.document` 或新增 `type_info.collection`。

### 主要扫描链路

对象存储 catalog scan：

```text
service.ScanService
  -> scanadapter.ContentCatalogScanner
  -> scanruntime.ObjectStorageCatalogRuntime
  -> scanruntime.ScanPaths / ScanRefGroups
  -> scanruntime.DetectObjectCatalogResourceFormats     # 基于内容前缀修正未知格式
  -> scanflow.DetectObjectCatalogCompositeItems
  -> metaitem.ResolveItems
  -> metacatalog.PlanObjectCatalogSingleItem / PlanObjectCatalogCompositeItem
  -> scanprocessor.ObjectSingleInput / ObjectCompositeInput
  -> metaenrich / metaattr
  -> repository.UpsertItemWithDepth
```

文件系统 catalog scan：

```text
service.ScanService
  -> scanadapter.ContentCatalogScanner
  -> scanruntime.FilesystemCatalogRuntime
  -> scanruntime.ScanPaths / ScanRefGroups
  -> metaitem.StorageFileRef
  -> metaitem.ResolveItems                   # multi / whole / table resolver
  -> metaitem.InferSingleResourceItem        # 单文件兜底
  -> scanprocessor.FileDetectedInput / FileSingleInput
  -> metaenrich / metaattr
  -> repository.UpsertItemWithDepth
```

数据库 / branch-leaf catalog scan：

```text
service.ScanService
  -> scanadapter.CatalogDispatcher
  -> scanruntime.DatabaseRuntime / BranchLeafRuntime
  -> metaattr
  -> repository.UpsertNode / UpsertItemWithDepth
```

已知 item refresh：

```text
service.ScanService
  -> scanruntime.ItemRefreshRuntime
  -> dataitem.DescriptorFromAttributes       # 从已落库 attributes 还原 item descriptor
  -> scanflow.KnownItemDetectedItem
  -> scanprocessor.KnownItemInput
  -> metaenrich / metaattr
  -> repository.UpsertItemWithDepth
```

Manager 预览不会重新识别格式，只消费已落库 Meta attributes 中的 `layout`、`data_type`、`format` 选择 preview provider。因此格式识别和类型修正应在 Meta scan / refresh 阶段完成。

## 核心 API

路由前缀：`/api/v1/meta`。

- 资产发现：`GET /assets/discoverable`。
- 引擎：`GET /engines`。
- 扫描：`POST /scan/run/unscanned`、`POST /scan/run/manual`。
- 扫描运行：`GET /scan/runs`、`GET /scan/runs/:run_id`、`POST /scan/runs/:run_id/cancel`。
- 扫描任务：`GET /scan/tasks`、`POST /scan/tasks`、`PUT /scan/tasks/:task_id`、`DELETE /scan/tasks/:task_id`、`POST /scan/tasks/:task_id/trigger`。
- 元数据对象：`GET /metadata/object`。
- 引擎数据项：`GET /engines/:engine_id/items`。
- 树查询：`GET /engines/:engine_id/tree`、`GET /nodes/:node_id`、`GET /nodes/:node_id/children`、`GET /nodes/:node_id/items`、`GET /nodes/by-catalog-path`、`GET /items/by-catalog-path`。
- 字段与空间信息：`GET /items/:item_id/fields`、`GET /items/:item_id/spatial`、`GET /items/:item_id`。
- 统计与缓存：`GET /stats`、`DELETE /cache/engines/:engine_id`、`POST /cache/refresh`。

查询 API 边界：

- 已定位资源优先使用 `node_id` / `item_id` 主资源查询。
- 跨模块定位时使用 `engine_id + catalog_path` 的正式条件查询：`/nodes/by-catalog-path`、`/items/by-catalog-path`。
- `/metadata/object` 是历史对象快捷查询，目前只读、无写入副作用；后续若迁移，应优先迁到 `items/by-catalog-path` 或明确的 item projection，不新增按存储技术形态分叉的新快捷入口。

## 开发规则

- 扫描必须执行租户隔离校验，不能绕过 System 引擎归属与当前用户租户。
- 数据库、对象存储、文件系统和 NoSQL 扫描逻辑按 `scanadapter` / `scanruntime` / `scanprocessor` 分层扩展，`service` 只做应用门面和依赖装配，避免在 Handler 或 service 中堆叠扫描细节。
- `ScanTaskService` 的类型和构造保留在 `scan_task_service.go`；生命周期、execution、任务 CRUD、调度同步分别放在 `scan_task_lifecycle.go`、`scan_task_execution.go`、`scan_task_crud.go`、`scan_task_schedule.go`。
- `CatalogDispatcher` 的类型和总分发保留在 `catalog_dispatcher.go`；tabular、branch-leaf、通用锁和 root 收尾分别放在 `catalog_dispatcher_tabular.go`、`catalog_dispatcher_branch.go`、`catalog_dispatcher_helpers.go`。
- `scanprocessor.Processor` 主流程保留在 `processor.go`；输入构造、文档抽取、内容 hash 分别放在 `processor_inputs.go`、`processor_document.go`、`content_hash.go`。
- `scanruntime.DatabaseRuntime` 类型、构造和 `ScanNamespace` 主入口保留在 `database_runtime.go`；表扫描循环、表详情 attributes、facts 合并、空间元数据分别放在 `database_tables.go`、`database_table_details.go`、`database_table_facts.go`、`database_spatial.go`。
- `scanruntime.ObjectStorageCatalogRuntime` 和 `FilesystemCatalogRuntime` 主文件只保留类型、构造和 `ScanPaths` 入口；对象 resource 持久化放在 `object_resources.go`，文件目录递归扫描放在 `file_directory_scan.go`。
- `scanruntime.BranchLeafRuntime` 类型、构造和 `ScanBranch` 主入口保留在 `branch_leaf_runtime.go`；leaf 扫描循环、动态 schema 转换、清理 helper 分别放在 `branch_leaf_leaves.go`、`branch_leaf_dynamic_schema.go`、`branch_leaf_helpers.go`。
- 空间元数据必须动态检测几何列、SRID、范围和几何类型，不要默认字段名。
- 扫描去重使用 Redis 锁；调试异常扫描时先检查 `meta:scan_dedup:*`。
- 修改 API 后同步 Swagger：`bash scripts/swagger/gen-swagger.sh meta` 和 `bash scripts/swagger/check-route-coverage.sh meta`。

## 开发与验证

```bash
bash scripts/dev/start.sh -meta
bash scripts/dev/restart.sh -meta
curl http://localhost:8082/health
```

常用日志：

- `logs/meta-backend.log`
- `logs/meta-worker.log`

## 相关文档

- `meta/docs/数据库架构.md`
- `meta/docs/tables/meta_node表.md`
- `meta/docs/tables/meta_item表.md`
- `meta/docs/tables/scan_tasks表.md`
- `docs/concepts/addp术语表.md`
- `docs/spec/addp元数据扫描机制规范.md`
- `docs/spec/addp数据项探测器规范.md`
- `docs/spec/addp元数据attributes规范.md`
- `docs/spec/addp存储引擎路径体系规范.md`
- `docs/spec/addp引擎插件接口规范.md`
- `docs/spec/addp引擎能力声明规范.md`
- `docs/next/meta-preprocessing语义与任务边界.md`
- `docs/next/meta-cleanup边界与派生产物清理设计.md`
- `docs/next/meta查询API快捷入口收敛设计.md`
- `manager/CLAUDE.md`

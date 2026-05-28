# Meta 模块说明

## 模块定位

Meta 模块负责元数据扫描、元数据存储、元数据查询、对象元数据提取、扫描任务调度、扫描运行记录和资产发现接口。它是 Manager 数据探查、Asset 自动发现和搜索索引的重要来源。

## 技术栈与端口

- 后端：Go + Gin + GORM，默认端口 `8082`，环境变量 `META_BACKEND_PORT`。
- 前端：Vue 3 + Element Plus，开发端口 `5175`，启动脚本环境变量 `META_FE_PORT`。
- 数据库：PostgreSQL `metadata` schema。
- 依赖：System、Redis、Meilisearch、MinIO，可选 pgvector/嵌入服务。

## 重要目录

```text
meta/
├── backend/
│   ├── cmd/server/main.go
│   ├── internal/api/          # 统一 Handler 与路由
│   ├── internal/models/       # node、item、scan_task、cleanup
│   ├── internal/service/      # scan、repository、database/object/filesystem/nosql 扫描
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

### 与 common/dataitem 的边界

`common/dataitem` 是跨模块的 data item 规则层，只处理纯事实输入，不打开引擎资源、不读取对象内容、不依赖 Meta 落库模型。

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

### 主要扫描链路

对象存储 catalog scan：

```text
service/scan_object_storage_catalog_service.go
  -> metacatalog.StorageResource
  -> metaenrich.DetectSingleFileFormat       # deep scan 下基于内容前缀修正未知格式
  -> metacatalog.DetectObjectCatalogCompositeItems
  -> metaitem.ResolveItems
  -> metaenrich table/container resolver
  -> metacatalog.PlanObjectCatalogSingleItem / PlanObjectCatalogCompositeItem
  -> metaattr.MergeDataItemAttributes
  -> repository.UpsertItemWithDepth
```

文件系统 catalog scan：

```text
service/scan_filesystem_catalog_service.go
  -> plugin.FileEntry
  -> metaitem.ResolveItems                   # multi / whole / table resolver
  -> metaitem.InferSingleResourceItem        # 单文件兜底
  -> metaenrich.EnrichSingleTableFileItem    # deep enrich，必要时基于内容前缀修正格式
  -> metaattr.BuildAttributes
  -> repository.UpsertItemWithDepth
```

已知 item refresh：

```text
service/item_refresh_service.go
  -> dataitem.DescriptorFromAttributes       # 从已落库 attributes 还原 item descriptor
  -> detectedItemFromDescriptor
  -> layout 分支：
       multi  -> metaitem.EnrichKnownMultiTableItem
       single -> metaenrich.EnrichSingleTableFileItem / DetectSingleFileFormat
       whole  -> metaenrich.EnrichContainerChildren
  -> metaattr.MergeDataItemAttributes
  -> restoreKnownItemStorage
  -> 更新 metadata.meta_item.attributes
```

Manager 预览不会重新识别格式，只消费已落库 Meta attributes 中的 `layout`、`data_type`、`format` 选择 preview provider。因此格式识别和类型修正应在 Meta scan / refresh 阶段完成。

## 核心 API

路由前缀：`/api/v1/meta`。

- 资产发现：`GET /assets/discoverable`。
- 引擎：`GET /engines`。
- 扫描：`POST /scan/auto`、`POST /scan/engine`、`POST /scan/run/manual`。
- 扫描运行：`GET /scan/runs`、`GET /scan/runs/:run_id`、`POST /scan/runs/:run_id/cancel`。
- 扫描任务：`GET /scan/tasks`、`POST /scan/tasks`、`PUT /scan/tasks/:task_id`、`DELETE /scan/tasks/:task_id`、`POST /scan/tasks/:task_id/trigger`。
- 元数据对象：`GET /metadata/object`、`POST /metadata/extract`。
- 引擎数据项：`GET /engines/:engine_id/items`。
- 树查询：`GET /engines/:engine_id/tree`、`GET /nodes/:node_id`、`GET /nodes/:node_id/children`、`GET /nodes/:node_id/items`、`GET /nodes/by-catalog-path`、`GET /items/by-catalog-path`。
- 字段与空间信息：`GET /items/:item_id/fields`、`GET /items/:item_id/spatial`、`GET /items/:item_id`。
- 统计与缓存：`GET /stats`、`DELETE /cache/engines/:engine_id`、`POST /cache/refresh`。

## 开发规则

- 扫描必须执行租户隔离校验，不能绕过 System 引擎归属与当前用户租户。
- 数据库、对象存储、文件系统和 NoSQL 扫描逻辑按 service 分层扩展，避免在 Handler 中写扫描细节。
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
- `meta/docs/tables/scan_task_runs表.md`
- `docs/spec/addp引擎插件接口规范.md`
- `docs/spec/addp引擎能力声明规范.md`
- `manager/CLAUDE.md`

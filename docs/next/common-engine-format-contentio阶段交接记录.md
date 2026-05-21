# common engine / format / contentio 阶段交接记录

更新时间：2026-05-21

本文记录本轮围绕 `common/contentio`、`common/format`、`common/dataitem`、Meta、Manager 和 Transfer 的阶段性结论、已完成工作和下一轮需要继续处理的问题。本文只作为交接记录；稳定规则应继续沉淀到 `docs/spec/` 和 `docs/concepts/`。

## 一、已经稳定的架构口径

### 1. contentio 的边界

`common/contentio` 已收敛为 Go `io` 之上的平台内容 I/O 抽象：

- `Ref`：一个已确定 content 的定位器，只包含 path / role 这类底层定位事实。
- `Reader` / `Writer`：按 `Ref` 打开或创建内容流。
- `Lister`：按 scope `Ref` 列举子 content。
- `RangeReader`：按 `Ref` 做字节范围读取。
- `Stat`：单 content 的轻量状态。

`contentio` 不承载 data item、format、data type、primary、required、展示名、字段 schema、Manager DTO 或 engine 连接信息。多个 content 组成一个 data item 时，不在 `contentio` 中引入 multi reader / writer，而由上层显式传递 `[]format.RelatedRef`。

`contentio` 的核心职责是“定位和搬运单个 content”，不是解释 item 结构。multi 只是上层 item 组织方式，不是 `contentio` 里的基础概念。

### 2. format 的边界

`common/format` 已收敛为文件格式身份、能力、info provider、content reader 和 writer 的共享层：

- `FormatPlugin` 声明格式身份、descriptor 和 capability。
- `TableInfoProvider` / `TableSampleReader` / `TableReaderProvider` 服务 single table content。
- `MultiTableInfoProvider` / `MultiTableSampleReader` / `MultiTableReaderProvider` / `MultiTableWriterProvider` 服务 multi ref table content。
- `ScopeTableInfoProvider` / `ScopeTableSampleReader` / `ScopeTableReaderProvider` 服务 whole scope table content。
- `RelatedRefSpec` 表达格式层的 ref 规则；`RelatedRef` 表达已解析 ref + 集合标注。

格式实现不得接 `engine_id` 并自行构造 reader，不得判断 data item 边界，不得把 native field type 泄漏到上层执行链路。各格式的 native field type 与 ADDP 标准字段类型转换由各格式自己负责。

`common/format` 只暴露格式身份、能力和基于 content 的 info / reader / writer 能力，不接管 item 归并和 content 组织。

### 3. dataitem 的边界

`common/dataitem` 是候选 content 集合到 data item 组织结果的共享规则层，不是 Meta 落库层：

- 识别 `layout=single/multi/whole`。
- 归并 multi refs，例如 Shapefile 的 `.shp/.shx/.dbf/.prj/.cpg`。
- 识别 whole scope。
- 输出 `ResolvedItem`、`RefList`、`Claims`、`Exclusive`。

Meta 负责扫描调度、递归遍历、detector 编排、attributes normalizer、fingerprint、node 绑定和落库。Manager 只可在容器预览中临时调用 `common/dataitem` 做动态识别，结果用完即弃，不写入 Meta。

`common/dataitem` 允许被调用方反复递归使用，递归深度和是否落库由调用方控制；Meta 用它做外部资源范围的 item 裁决，Manager 只用它做容器内部动态预览。

### 4. Meta / Manager 的扫描职责

Meta 扫描分为 `basic` 和 `deep`：

- `basic` 建立资源树和 data item 身份，不应读取大内容。
- `deep` 补充字段、行数、容器 children、format info、content index 和横切能力。

Manager 预览 item 时默认要求 item 已达到 deep。如果 item 不是 deep，应调用 Meta 对该 item 做 `scan_depth=deep, force=false` 补齐，完成后重新读取 item 再预览。

Manager 刷新分两类：

- 刷新 node：可以异步触发 Meta deep + force 扫描，树后续刷新即可。
- 刷新 item：调用 Meta 的已知 item 刷新能力，同步完成 deep 属性刷新，然后重新获取 item 元数据和预览。

item refresh 不是 catalog scan，也不是重新发现 item。Meta 应从已入库 item 的标准 attributes 还原 descriptor，再按 format provider 能力刷新字段、行数、format info、content index 和横切能力，并更新同一个 item。对于 `layout=multi`，`attributes.item.refs` 是已确认的 content 组成事实，应传给 provider 读取；它不是 catalog scan target。对于 `layout=whole`，使用 whole scope 根范围；对于 `layout=single`，使用 primary content / `storage.physical_path` / `meta_item.full_name`。如果 item 本身识别错了，应从 node 层重新扫描解决，而不是 item 刷新时扩大范围。

Manager 预览和刷新要区分两条路径：

- 已入库 item 预览：消费 Meta 的标准 attributes。
- 容器内部动态预览：临时调用 `common/dataitem`，不落库。

刷新也要区分：

- node 刷新可以异步。
- item 刷新必须走 Meta Client 的 item refresh 接口，同步完成后再重新读取 item。

### 5. Transfer 的进展

Transfer 的长期口径已稳定为：

```text
Transfer 负责任务与编排
common/engine 负责 engine-native 能力
common/contentio 负责 content I/O
common/format 负责格式和数据类型能力
```

已推进的重点：

- 新 endpoint JSON 以 source / target endpoint 为核心，不再保留旧 connector type 主路径。
- table transfer 统一按 data type 编排，避免按 engine 组合分叉。
- CSV / JSON / Parquet / Shapefile 逐步接入 common format reader / writer。
- Shapefile 已作为 multi table reader / writer 样板推进。
- PostgreSQL、NFS、S3 / MinIO 的读写能力逐步下沉到 engine capability 与 contentadapter。
- 空间几何在预览中可用 WKT，Transfer 连续 reader / writer 可按 `ParseOptions.GeometryEncoding` 请求 WKB / EWKB；不得针对某个 engine 硬编码判断。

Transfer 当前的关键要求不是“先把流程走通”，而是按标准 item attributes、contentio 和 format 能力消费内容，不再自己猜字段类型、空间字段或 related refs。

## 二、本轮已完成的关键修正

### 1. item refresh 收敛为 Meta 的已知 item 刷新能力

本轮已经把 Manager item 刷新从 node / catalog scan 路径中拆出：

- Meta 新增 `POST /api/v1/meta/items/{item_id}/refresh`，语义是刷新已入库 item 的 deep attributes。
- Manager 后端通过 Meta Client 调用该接口；Manager 只负责传递当前 item，不直接构造扫描目标。
- `common/dataitem.DescriptorFromAttributes()` 负责从标准 attributes 还原 item descriptor，供 Meta item refresh、Manager、Transfer 等复用。
- `scanTargetFromItem()` 不再把 `refs` 展开为 catalog scan target，也不再按 item type 推导目录或 namespace。
- multi item 已知刷新使用已入库 `attributes.item.refs` 作为 provider 的 content 输入，不重新枚举 sibling content。

这次问题的根因是把“已知 item 的属性刷新”和“从目录重新发现 item”混成了一条路径。前者的输入是已入库 item descriptor，目标是更新同一个 item 的 attributes；后者的输入是目录、prefix、schema 等 catalog 范围，目标是重新裁决 item 边界并落库。

### 2. Manager 刷新行为分流

刷新行为已经按对象类型区分：

- node refresh：继续异步触发 Meta deep + force scan，适合重新发现目录或 catalog 范围下的 item。
- item refresh：同步调用 Meta item refresh，适合用户发现某个 item 属性过期、缺失或能力未更新时即时刷新。

如果 `refs`、layout 或 item 边界本身错误，必须使用 node refresh 重新识别；item refresh 不负责扩大范围修复 item 身份。

### 3. Shapefile 动态预览的本地索引回退

本轮已经补上 Shapefile 的本地 materialized fallback：

- 当 ZIP / 容器 child 预览无法直接走 `RangeReader` 时，Shapefile multi ref 会先 materialize 到临时目录。
- 只要本地 `.shp/.shx/.dbf` 可用，就继续走 `.shx` 索引窗口 + `.dbf` 连续属性块 + `.shp` 记录窗口，不再退回从第 0 行顺序跳页。
- 这样 `.shx` 原生索引价值可以在本地 fallback 路径里继续生效，不必把大 ZIP 解成完整目录后再慢慢翻页；Shapefile 不再额外落 `content_index` 元数据。

### 4. Manager container 动态预览路径收口

本轮已经继续核查并收口 Manager 的 container 动态预览边界：

- 已入库 item 预览继续消费 Meta 标准 attributes；container 内部子项预览才临时调用 `common/dataitem` 做动态识别。
- `objectcontent` 暴露 `ResolveContainerInfoForPreview()`，复用已有 `common/dataitem.ResolveItems()` 对容器当前层 children 做预览口径归并。
- `ContainerChildPreviewProvider` 在解析 `nested_child_path` 时，优先读取当前层 container children 并按预览口径归并，再继续寻址。这样 `outer.zip -> inner.zip -> roads.shp` 能识别出内层 Shapefile 的 `.shp/.shx/.dbf` refs，而不是只把 `.shp` 当单文件。
- 已补测试确认 SQLite / GeoPackage child 的 `type_info.container.children` 只是子项索引，不会被当作父 item 的 `type_info.table.fields`。
- 已补测试确认带 `child_name + nested_child_path` 的预览路由走 `builtin:container-child`，不误入 `file-table` 或 catalog provider。

本轮相关验证：

```bash
go test ./manager/backend/internal/preview ./manager/backend/internal/objectcontent
go test ./common/dataitem
```

### 5. Transfer 输出后缀归 common/format 管理

针对用户选择 `format=parquet` 但目标名称未带 `.parquet` 后缀，导致写出后 Meta 识别和预览体验割裂的问题，本轮已明确并实现统一规则：

- 期望输出后缀属于格式能力，不属于 Transfer 私有规则。
- `common/format` 提供默认写出后缀能力；Transfer planner 只消费该能力。
- encoded target 缺少格式后缀时自动补齐；已有冲突后缀时拒绝计划，避免写出“格式是 A、文件名像 B”的结果。
- 不为 Parquet 单独硬编码，后续 CSV、JSON、Shapefile 等有明确写出后缀的格式都走同一套能力。

这条规则已同步到 `docs/next/transfer基于common-engine-format改造设计.md` 的 endpoint 规则中：encoded target 默认写出后缀归 `common/format` 定义，Transfer 只消费。

### 6. 无后缀内容格式识别与 Meta refresh 链路

针对 MinIO 中无后缀 Parquet 文件 `lake3`，Manager item refresh 显示成功但预览仍 unsupported 的问题，本轮确认根因不是前端刷新失败，而是 Meta 的已知 item refresh 只基于旧 attributes 还原 descriptor。旧 item 为 `layout=single,data_type=unknown,format=unknown` 时，原有 `EnrichSingleTableFileItem` 会因为没有 table provider 直接跳过，不会重新读取内容识别格式。

本轮已收敛为以下统一方案：

- `common/format.DetectFormat(filename, peek)` 支持在扩展名无法判断时，通过注册格式 descriptor 的 `ContentSignatures` 和格式 plugin 的 `ContentSniffer` 认领内容。
- `common/format/plugins/parquet` 通过 descriptor / sniffer 声明 Parquet 的内容签名，不在 magic fallback 中为 Parquet 写特例。
- `meta/internal/metaenrich/single_format.go` 提供统一的 single content 前缀识别入口，负责读取有限 peek bytes 并调用 `common/format.DetectFormat`。
- `meta/internal/metaenrich/table_file.go` 在 single table enrichment 前，如果 item format 为空或 unknown，先调用统一内容识别，再继续原有 schema / row count / format info 提取。
- `meta/internal/service/item_refresh_service.go` 的已知 item refresh 复用该入口，因此 Manager 对 single item 刷新后可修正 format / data_type。
- `meta/internal/service/scan_object_storage_catalog_service.go` 的 object deep scan 也复用该入口，不再保留单独一套 peek + DetectFormat 实现。

这次改动的关键原则是：内容格式识别能力归 `common/format`，何时打开 engine content 归 Meta enrich / scan 链路；不把内容读取下沉到 `common/dataitem`，也不在各扫描入口分别补特例。

相关验证：

```bash
go test ./common/format ./common/format/plugins/parquet ./meta/backend/internal/metacatalog ./meta/backend/internal/metaenrich ./meta/backend/internal/service
```

功能验证：Manager 对无后缀 Parquet item 执行刷新后，Meta attributes 可被修正，预览可正常选择 table preview provider。

### 7. Meta 扫描分层约定已写入模块文档

为了避免后续在 `common/dataitem`、`metaitem`、`metacatalog`、`metaenrich`、`metaattr` 之间反复摇摆，本轮已将分工记录到 `meta/CLAUDE.md`：

- `common/dataitem` 是跨模块纯规则层，只处理候选事实，不打开内容。
- `internal/metaitem/` 负责 Meta item resolver 编排和 `DetectedItem`。
- `internal/metacatalog/` 负责 catalog 资源规范化和 item plan。
- `internal/metaenrich/` 负责打开内容、读 schema、读容器内部和内容前缀识别。
- `internal/metaattr/` 负责标准 attributes 合并与落库结构。
- `internal/metapath/` 负责路径语义工具。

文档同时列出了对象存储 catalog scan、文件系统 catalog scan、已知 item refresh 三条主链路，以及 Manager 预览只消费 Meta attributes、不重新识别格式的边界。

## 三、当前仍需继续处理的关键问题

### 1. Transfer 消费 Meta item attributes

Transfer planner 已开始消费已入库 Meta item 的标准 attributes，而不是重新猜测 refs、字段类型或空间字段：

- source endpoint 如果携带 `attributes`，planner 会从 attributes 还原 `layout/data_type/format/refs/physical_path/capabilities.spatial`。
- `layout=multi` source 必须使用 `attributes.item.refs` 的完整集合；缺 refs 或 refs 不完整时，提示回到 Meta node scan 重新识别 item。
- 字段类型使用 `type_info.table.fields` 中的 ADDP 标准字段类型，不读取或判断 format / engine native field type。
- 空间字段使用 `capabilities.spatial.primary_geometry_column` 或 `geometry_columns` 中的标准事实，不默认 `geom`。
- encoded spatial source -> native target 的 geometry row value 继续通过 `ParseOptions.GeometryEncoding` 请求 WKB / EWKB，不按具体 engine type 写特殊分支。
- Transfer executor 已接收 planner 提供的 schema、layout 和 related refs；multi source 优先使用 planner refs，避免按 basename 猜 sidecar。

`layout=whole` encoded source 已能被 planner 识别；`common/format` 已补 `ScopeTableReaderProvider`，Transfer executor 可通过连续 scope table reader 执行 whole scope table。Parquet dataset / partitioned table 是第一条落地链路；其他 whole scope 格式不能用 sample reader 冒充全量读取，必须补正式连续 reader。

### 2. Manager 剩余回归

Manager 后续只需用真实 NFS / MinIO / ZIP 样例继续回归：

- 大 ZIP 中 Shapefile 翻页不会退回顺序跳行。
- 嵌套 ZIP / SQLite / GeoPackage / Shapefile 子项显示和字段来源正确。
- 动态识别结果只服务本次预览，不写回 Meta。

## 四、已修订或应继续同步的文档

稳定口径已进入或本轮应同步到以下文档：

- `meta/CLAUDE.md`
- `docs/spec/addp内容IO抽象规范.md`
- `docs/spec/addp数据项探测器规范.md`
- `docs/spec/addp元数据扫描机制规范.md`
- `docs/spec/addp元数据attributes规范.md`
- `docs/spec/addp数据类型与格式能力规范.md`
- `docs/concepts/addp数据项体系图.md`
- `docs/concepts/addp术语表.md`
- `docs/next/common-contentio边界整理设计.md`
- `docs/next/transfer基于common-engine-format改造设计.md`

## 五、下一轮建议执行顺序

1. 如后续调整 item refresh，必须同时验证 single / multi / whole 三类 layout，尤其确认 multi 的 `type_info.table.fields`、`capabilities.spatial` 不因只读 primary content 而丢失。
2. 如果继续扩展无后缀识别，优先给格式 descriptor / plugin 补 `ContentSignatures` / `ContentSniffer`，再复用 Meta enrich 的统一入口；不要在扫描 service 中新增格式特例。
3. Manager 后续只做真实样例回归和小修，不再作为下一轮主线。

### 已完成：真实 `layout=whole` Transfer 回归

2026-05-21 已用真实 NFS / MinIO Parquet dataset 回归 `layout=whole` encoded Transfer 链路：

- MinIO 分区 Parquet dataset -> NFS Parquet：task `13`，execution `522`，`records_read=146180`，`records_written=146180`。目标路径自动补齐为 `exports/codex_minio_whole_to_nfs_20260521.parquet`，写后 deep scan 生成 NFS meta item `1867`。
- NFS whole Parquet dataset -> MinIO Parquet：task `14`，execution `524`，`records_read=219270`，`records_written=219270`。源 item `1841` 的 3 个 part 文件被完整递归读取；目标对象自动补齐为 `manager/regression/codex-nfs-whole-to-minio-20260521.parquet`，写后 deep scan 生成 MinIO meta item `1868`。

本轮实跑发现并修复两类边界问题：

- 对象存储分区目录下的 Parquet dataset 需要在直接父 prefix 之外，额外按分区根 prefix 形成 whole-scope 候选。
- Transfer planner 消费对象存储 whole item 时，source scope 应优先使用 `attributes.storage.physical_path`，再回退到 `storage.path + storage.name` / `storage.path`，避免把父 prefix 当作完整读取范围。

## 六、新会话起手点

如果后续另开会话，建议先从下面三个位置接着看：

1. `docs/next/transfer基于common-engine-format改造设计.md`
2. `docs/spec/addp数据项探测器规范.md`
3. `docs/spec/addp元数据attributes规范.md`

对应代码重点是：

- `meta/CLAUDE.md`
- `common/format/detection.go`
- `common/format/detection_magic.go`
- `common/format/provider.go`
- `meta/backend/internal/metaenrich/single_format.go`
- `meta/backend/internal/metaenrich/table_file.go`
- `meta/backend/internal/service/item_refresh_service.go`
- `meta/backend/internal/service/scan_object_storage_catalog_service.go`
- `transfer/backend/internal/planner/table_export.go`
- `transfer/backend/internal/planner/table_export_test.go`
- `transfer/backend/internal/executor/table_transfer.go`
- `transfer/backend/internal/executor/table_pipeline.go`
- `common/dataitem`

## 七、交接提醒

本轮没有要求保留旧数据兼容。若旧 metadata 与新 attributes 结构不一致，直接删除后重新扫描。实现上不要为了旧结构增加兼容分支。

当前用户明确强调的原则：

- 不硬编码单一格式或单一 engine 的特殊判断。
- 根据协议规范编程，而不是为了走通写临时分支。
- native field type 原则上不能出对应 format / engine 的边界；最多作为只读诊断 attributes 展示，不参与执行。
- 各模块和目录必须守住职责边界，避免“屎上雕花”式补丁。

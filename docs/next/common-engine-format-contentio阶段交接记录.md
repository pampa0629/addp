# common engine / format / contentio 阶段交接记录

更新时间：2026-05-20

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
- `ScopeTableInfoProvider` / `ScopeTableSampleReader` 服务 whole scope table content。
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
- 刷新 item：必须同步等待 Meta deep + force 扫描完成，然后重新获取 item 元数据和预览。

item 重扫只能重扫 item 本身，但必须包含该 item 的所有 content。对于 `layout=multi`，应使用 `attributes.item.refs`；对于 `layout=whole`，应使用 whole scope 根范围；对于 `layout=single`，应使用 primary content / `storage.physical_path` / `meta_item.full_name`。如果 item 本身识别错了，应从 node 层重新扫描解决，而不是 item 刷新时扩大范围。

Manager 预览和刷新要区分两条路径：

- 已入库 item 预览：消费 Meta 的标准 attributes。
- 容器内部动态预览：临时调用 `common/dataitem`，不落库。

刷新也要区分：

- node 刷新可以异步。
- item 刷新必须等待 deep + force 扫描完成后再重新读取 item。

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

## 二、当前正在处理但尚未完成的关键问题

### 1. item 重扫目标仍需统一

当前观察到的实现问题：

- `meta/backend/internal/service/scan_service.go` 中 `scanTargetFromItem()` 仍主要依赖 `ItemType + FullName` 推导扫描目标。
- 这会导致 multi item 重扫时只使用主 content，丢失同一 item 的 related refs。
- Manager item 刷新如果继续走 node refresh 路径，也无法保证等待扫描完成和重新读取 item 元数据。

下一轮建议：

1. 在 `common/dataitem` 增加纯 helper：从标准 item attributes 还原 item content paths / scan targets。
2. Meta `scanTargetFromItem()` 优先使用该 helper；没有标准 attributes 时再按保守规则退回。
3. Manager item refresh 调用专用同步接口，不再复用 node refresh。
4. Manager item refresh 应创建 manual scan run，轮询 run 到终态，然后重新读取树和预览。

### 2. Meta manual scan run 与同步等待

已有基础能力：

- Meta 提供 `POST /api/v1/meta/scan/run/manual`。
- Manager `MetadataService` 已有 `CreateManualScanRun()` 和 `GetScanRun()`。

需要补齐：

- Manager 后端的 `RefreshItem` API。
- 前端 `refreshItem()` 改调 item refresh API。
- item refresh 完成后清理缓存、重载树、重新拉取预览。
- node refresh 保持异步。

### 3. container 动态识别与 Meta 落库边界

已经达成的规则：

- Meta deep scan 只记录容器直接 children，不继续无限套娃识别下一层 data item。
- Manager 预览 container 时可以调用 `common/dataitem` 对容器内部当前层做动态识别。
- 动态识别结果不入 Meta，用完即弃。
- `common/dataitem` 的能力本身应支持多层递归，递归深度和是否落库由调用方控制。

下一轮需要继续核查 Manager 的两条路径：

- 已入库 meta item 预览路径。
- container 内部动态识别路径。

避免两条路径混用字段、合并所有 children 字段、或把 container child kind 与外部 item type 混淆。

## 三、已修订或应继续同步的文档

稳定口径已进入或本轮应同步到以下文档：

- `docs/spec/addp内容IO抽象规范.md`
- `docs/spec/addp数据项探测器规范.md`
- `docs/spec/addp元数据扫描机制规范.md`
- `docs/spec/addp元数据attributes规范.md`
- `docs/spec/addp数据类型与格式能力规范.md`
- `docs/concepts/addp数据项体系图.md`
- `docs/concepts/addp术语表.md`
- `docs/next/common-contentio边界整理设计.md`
- `docs/next/transfer基于common-engine-format改造设计.md`

## 四、下一轮建议执行顺序

1. 先补 `common/dataitem` 的 item attributes -> content paths / scan targets helper，并加单元测试。
2. 改 Meta `scanTargetFromItem()`，确保 multi item 重扫吃完整 refs。
3. 补 Manager item refresh 同步 API 和前端调用。
4. 用 MinIO / NFS Shapefile 验证：刷新 item 后 `content_index`、`type_info.table.fields`、`capabilities.spatial` 不丢失。
5. 再回头整理 Manager container 动态预览路径，确保 SQLite / ZIP / Shapefile 子项显示和字段来源正确。
6. 继续推进 Transfer 对 Meta item attributes 的消费，避免自行猜 refs、字段类型或空间字段。

## 五、新会话起手点

如果后续另开会话，建议先从下面三个位置接着看：

1. `docs/spec/addp元数据扫描机制规范.md`
2. `docs/spec/addp数据项探测器规范.md`
3. `docs/spec/addp内容IO抽象规范.md`

对应代码重点是：

- `common/dataitem`
- `meta/backend/internal/service/scan_service.go`
- `manager/backend/internal/service/metadata_service.go`
- `manager/backend/internal/service/explorer_service.go`
- `transfer/backend/internal/executor/table_pipeline.go`

## 五、交接提醒

本轮没有要求保留旧数据兼容。若旧 metadata 与新 attributes 结构不一致，直接删除后重新扫描。实现上不要为了旧结构增加兼容分支。

当前用户明确强调的原则：

- 不硬编码单一格式或单一 engine 的特殊判断。
- 根据协议规范编程，而不是为了走通写临时分支。
- native field type 原则上不能出对应 format / engine 的边界；最多作为只读诊断 attributes 展示，不参与执行。
- 各模块和目录必须守住职责边界，避免“屎上雕花”式补丁。

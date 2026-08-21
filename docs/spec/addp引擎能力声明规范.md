# ADDP 引擎能力声明规范

本文规范 engine plugin 的结构化能力声明。插件接口边界见 [addp引擎插件接口规范.md](addp引擎插件接口规范.md)，Catalog 路径语义见 [addp存储引擎路径体系规范.md](addp存储引擎路径体系规范.md)。

能力声明用于回答“一个引擎实例自身具备哪些可由 ADDP 统一消费的能力”。上层模块不得只根据 `engine_type` 或 `engine_family` 猜能力。

当前版本固定为：

```text
engine.capabilities/v1
```

---

## 一、基本原则

- capabilities 是引擎实例自身能力与 Provider 实现承诺共同收敛后的事实来源。
- 插件 `Capabilities()` 只返回不连接实例的静态能力模板；实现了实例能力解析接口的插件，必须由 System 在保存或刷新具体引擎记录时执行只读探测，并将解析后的实例能力写入 `system.engines.capabilities`。
- 实例能力探测不得进入任何模块的启动或 readiness 关键路径。System 就绪后的后台刷新必须逐 Engine Instance 隔离；探测失败时保留该实例最后一次成功落库的能力事实并记录失败，不得清空能力、终止进程或阻塞其他实例。
- 创建 Engine Instance、变更连接或凭据以及显式连接测试可以同步探测并把失败返回给当前操作；仅修改名称、描述或生命周期不得要求实例在线，也不得触发能力探测。
- 声明了可调用能力，就必须有对应 Provider 或明确的模块执行面。
- Catalog、Facts、Store、Query、Workflow、Script 是不同能力面，不能混用。
- 核心结构只表达引擎自身原生能力与对应 Provider 能力，不承载模块适配状态。
- `compute.query`、`compute.workflow`、`compute.script` 是计算能力事实源，取代旧版 `dev_modes` 字符串数组；开发界面可以由这些能力派生，但不得再把 `dev_modes` 作为能力声明事实源。
- 没有明确模块消费价值的字段不进入核心声明；后续有真实调用方时再扩展。
- `extensions` 只承载引擎特有补充信息，不得替代核心字段。
- `extensions.spatial_workspaces` 只承载数据库实例中可识别的厂商空间工作区事实，如 SuperMap `sdx_postgis`、`sdx_postgresql` 或 ArcGIS `sde`；这一层用于 System 自动探测、高危启用和实例级 Provider 选择，不得把两个实现不同的 SuperMap 产品合并为 `sdx+`。
- `extensions.spatial_workspaces[].can_enable` 只表示实例在当前条件下具备被显性启用的可能性；是否真的执行启用动作，由 System 的高危操作入口统一触发，不由前端或业务模块直接改写能力 JSON。
- Oracle Engine 声明普通 tabular 的 Catalog、Facts、SQL 参数查询、BatchRead、TableReadSession、TableWriteSession 和基础 Spatial Facts/空间行读写；TableWriteSession 只创建或写入普通 Oracle 表与 `MDSYS.SDO_GEOMETRY`，不创建或修改 ArcGIS geodatabase system tables，也不表达 SDE 注册或版本化能力。不得因为底层数据库可产生 redo、Transfer 已支持 Oracle CDC 或存在 ArcGIS SDE 表就声明 `change_stream_read` 或 SDE 逻辑变化源。Oracle CDC 是 Transfer-owned capture Provider，ArcGIS SDE 仍是后续独立逻辑变化源，两者不得并入 Oracle Engine 的普通 Store 能力。

ArcGIS workspace kind 固定为 `arcgis/sde`。Oracle 实例能力解析器只能通过只读 Oracle data dictionary 探测 `SDE` repository owner 的企业级地理数据库正式核心系统表组合：同一 `SDE` owner 至少同时可见 `TABLE_REGISTRY`、`GDB_ITEMS`、`GDB_ITEMTYPES`、`GEOMETRY_COLUMNS` 四张注册表；`STATES`、`STATE_LINEAGES`、`VERSIONS`、`LAYERS` 等表作为版本化和要素类证据单独记录。仅存在 `SDE` schema、单张同名表或普通 `SDO_GEOMETRY` 列不得判定为 SDE workspace。探测结果写入 `extensions.spatial_workspaces[]`，使用 `backend_engine_type=oracle`、`can_enable=false`、`risk_level=high`；没有正式组合写入 `state=not_detected`，字典可见但核心表读取被拒绝时写入 `state=permission_denied`。本阶段不改变 `storage.store`，不声明 `change_stream_read`，不提供启用入口；后续 SDE 数据面必须由独立 logical change source / table provider 消费该 workspace fact。

SuperMap workspace kind 固定如下：

| `ecosystem/kind` | 前置条件 | geometry 存储 | ADDP table Provider |
| --- | --- | --- | --- |
| `supermap/sdx_postgis` | PostgreSQL 已启用 PostGIS，且实例未启用 `sdx_postgresql` | PostGIS geometry | PostgreSQL/PostGIS 原生 Provider，SuperMap 生命周期操作可附加 Workspace Controller |
| `supermap/sdx_postgresql` | PostgreSQL 可与 PostGIS 扩展共存，实例不得启用 `sdx_postgis` | SuperMap 私有 geometry，默认业务 schema 为 `sdx` | `bound_runtime_engine_id` 指向的兼容 Workflow Runtime 通过 SDK 读写，边界编码为 EWKB |

同一 PostgreSQL 实例最多只能检测或启用其中一种 SuperMap workspace。PostGIS 扩展本身可以与任一 SuperMap workspace 共存；只有 `sdx_postgis` 与 `sdx_postgresql` 两种 SuperMap workspace 互斥。检测到任一 kind 后，另一 kind 必须为不可启用；不得通过兼容分支允许二者并存。

`supermap/sdx_postgresql` 的 `bound_runtime_engine_id` 是 Transfer table session 与 Meta catalog facts 的唯一运行时绑定。两个模块都必须通过 System 的租户内 Runtime Descriptor 精确解析该 ID；绑定缺失、Runtime 不可见、非 active、未声明 `addp.workflow/v1` 或未提供所需 direct 算子时必须明确失败，不得改选其他 Runtime，也不得回退到 PostgreSQL 私有 Blob 读写。

能力详情页不得直接平铺原始 JSON 字段。System 后端应生成 `capabilities_view` 供前端渲染；完整 JSON 仅作为技术查看入口保留。

调整 `engine.capabilities/v1` 字段、能力展示 API 或 System 引擎能力展示模型时，必须同步检查并更新本规范、[ADDP 引擎插件接口规范](addp引擎插件接口规范.md)、`system/docs/tables/engines表.md`，以及依赖能力展示或预览能力判断的 Manager 设计文档。

---

## 二、顶层结构

```go
type EngineCapabilities struct {
    SchemaVersion string                 `json:"schema_version"`
    EngineType    string                 `json:"engine_type"`
    EngineFamily  string                 `json:"engine_family"`
    Storage       *StorageCapabilities   `json:"storage,omitempty"`
    Compute       *ComputeCapabilities   `json:"compute,omitempty"`
    Limits        map[string]interface{} `json:"limits,omitempty"`
    Extensions    map[string]interface{} `json:"extensions,omitempty"`
}
```

| 字段 | 说明 | 保留要求 |
| --- | --- | --- |
| `schema_version` | 能力声明结构版本，必须为 `engine.capabilities/v1`。 | 必须保留。 |
| `engine_type` | 插件类型，如 `postgresql`、`minio`、`neo4j`、`inference_runtime`。 | 必须保留。 |
| `engine_family` | 主引擎族，如 `tabular`、`dynamic_schema`、`graph`、`object`、`file`、`event_stream`、`workflow`、`script`、`inference`。 | 必须保留，但只作为粗分类。 |
| `storage` | 存储、目录、元数据、内容访问能力。 | 具备存储能力的引擎必须声明。 |
| `compute` | 查询、工作流、脚本运行能力。 | 具备计算能力的引擎必须声明。 |
| `limits` | 跨能力通用限制，如预览大小、超时建议。 | 可选，有真实调用方时声明。 |
| `extensions` | 引擎特有扩展。 | 可选，不得替代核心字段。 |

`engine_family` 只表达粗粒度引擎族，不能替代 `storage.catalog_model`、provider 组合或模块自身策略。尤其对 Meta 而言，是否走 namespace/leaf catalog、是否需要内容读取、是否可做动态 schema 采样，必须由 `CatalogModelSpec` 与已实现 provider 一起决定；不得把 `engine_family` 当作扫描策略事实源。

---

## 三、StorageCapabilities

```go
type StorageCapabilities struct {
    CatalogModel *CatalogModelSpec   `json:"catalog_model,omitempty"`
    Catalog      *CatalogCapability  `json:"catalog,omitempty"`
    Facts        *CatalogFactsCapability `json:"facts,omitempty"`
    Store        *StoreCapability    `json:"store,omitempty"`
    Semantics    []string            `json:"semantics,omitempty"`
    NotSupported []string            `json:"not_supported,omitempty"`
}
```

`storage` 是否存在表示引擎是否具备存储能力。存储主类型由顶层 `engine_family` 表达，不再声明 `storage.families`。

| 字段 | 说明 | 保留要求 |
| --- | --- | --- |
| `catalog_model` | 目录层级稳定模型，决定页面树、路径解析、扫描范围和指纹计算。 | 具备 catalog 的存储引擎必须声明。 |
| `catalog` | 是否能实时列出引擎里的 catalog branch / leaf。 | 需要浏览或扫描真实目录时声明。 |
| `facts` | 是否能描述 catalog leaf 的字段、统计、索引、约束、空间信息或原生属性。 | Meta 扫描和资产描述需要时声明。 |
| `store` | 内容访问方式，如流式读、范围读、批量读写。 | 需要预览、导入导出或内容访问时声明。 |
| `semantics` | 补充稳定机器语义，如 `bucket`、`prefix_listing`、`object`。 | 可选，仅在核心字段不足以表达差异且有调用方时声明。 |
| `not_supported` | 显式声明容易误判但不支持的能力，如 `real_directory`、`range_write`。 | 可选，对容易混淆的引擎建议声明。 |

### 3.1 CatalogModelSpec

```go
type CatalogModelSpec struct {
    PathVersion string             `json:"path_version"`
    RootTerm    string             `json:"root_term"`
    Levels      []CatalogLevelSpec `json:"levels"`
}

type CatalogLevelSpec struct {
    Term     string   `json:"term"`
    Kinds    []string `json:"kinds"`
    Role     string   `json:"role"`
    Optional bool     `json:"optional,omitempty"`
    I18nKey  string   `json:"i18n_key,omitempty"`
}
```

`path_version` 当前固定为 `catalog.path/v1`。

| 字段 | 说明 |
| --- | --- |
| `path_version` | 目录路径模型版本。 |
| `root_term` | 结构根语义，如 `server`、`service`、`root`。 |
| `levels.term` | 层级语义，如 `bucket`、`prefix`、`object`。 |
| `levels.kinds` | 该层可能出现的节点类型，如 `table`、`view`、`object`。 |
| `levels.role` | catalog 结构角色，只允许 `branch` / `leaf`。 |
| `levels.optional` | 该层是否可省略。 |
| `levels.i18n_key` | 展示层使用的原生术语国际化 key。内置模型必须声明，推荐格式为 `engine.term.<term>`。 |

推荐层次：

| 引擎 | 层次模型 |
| --- | --- |
| PostgreSQL | `server -> schema -> table/view/materialized_view` |
| MySQL / Doris / ClickHouse | `server -> database -> table/view` |
| MongoDB | `server -> database -> collection` |
| Neo4j | `server -> database -> graph` |
| S3 / MinIO | `service -> bucket -> prefix -> object` |
| NFS | `root -> directory -> file` |
| Kafka | `service -> topic` |

`root_term` 表达显性 catalog root；`levels` 只包含 root 下业务层级，不包含 root 本身。对外展示完整层次时可以把 `root_term` 作为前缀说明，但能力声明中的 `levels[0]` 必须是第一层业务 branch，例如 PostgreSQL 的 `schema`、MinIO 的 `bucket`、NFS 的 `directory`。

`StorageCapabilities.CatalogModel` 是对外 CatalogModel 事实源。如果插件同时实现 `CatalogModelProvider`，其返回值必须与 `storage.catalog_model` 完全一致。

`levels.term` 是机器语义，`levels.i18n_key` 是展示语义。Meta、Manager 等上层模块可以统一消费 `CatalogEntry` / `CatalogPath`，但面向用户的 UI 必须优先使用 `i18n_key` 展示引擎原生术语，例如 PostgreSQL 显示 `Schema`，MySQL/MongoDB 显示 `数据库 / Database`，MinIO/S3 显示 `Bucket`，NFS 显示 `目录 / Directory`。不得把平台内部的 `catalog node` 作为用户可见术语。

消费规则：

- `CatalogModelSpec` 负责回答“catalog 怎么分层、各层叫什么、谁是 branch / leaf”。
- provider 组合负责回答“哪些动作真的可做”，例如是否能列目录、描述 catalog facts、采样字段、读取内容。
- 上层模块可以基于二者形成自己的执行策略，但不得绕过 catalog model 再维护第二套 family 专属目录规则。

### 3.2 CatalogCapability

```go
type CatalogCapability struct {
    Supported       bool     `json:"supported"`
    RealTime        bool     `json:"real_time"`
    SupportsSearch  bool     `json:"supports_search,omitempty"`
    SupportsFilter  bool     `json:"supports_filter,omitempty"`
    SystemFiltering bool     `json:"system_filtering,omitempty"`
    NodeKinds       []string `json:"node_kinds,omitempty"`
}
```

| 字段 | 说明 | 保留要求 |
| --- | --- | --- |
| `supported` | 插件是否能提供统一 catalog 浏览。 | 声明 catalog 时必须保留。 |
| `real_time` | 目录是否来自真实引擎实时查询，而不是平台扫描快照。 | 声明 catalog 时必须保留。 |
| `supports_search` | catalog 浏览是否支持搜索。 | 可选，有 Provider 和调用方时声明。 |
| `supports_filter` | catalog 浏览是否支持过滤。 | 可选，有 Provider 和调用方时声明。 |
| `system_filtering` | 是否能过滤系统库、系统 schema、实例已识别的厂商系统表等噪声。 | 数据库类引擎建议声明。 |
| `node_kinds` | catalog 可能返回的节点类型集合。 | 建议声明。 |

### 3.3 CatalogFactsCapability

```go
type CatalogFactsCapability struct {
    Supported       bool `json:"supported"`
    FieldInfo       bool `json:"field_info,omitempty"`
    Statistics      bool `json:"statistics,omitempty"`
    Indexes         bool `json:"indexes,omitempty"`
    Constraints     bool `json:"constraints,omitempty"`
    Partitioning    bool `json:"partitioning,omitempty"`
    SpatialFacts    bool `json:"spatial_facts,omitempty"`
    Sampling        bool `json:"sampling,omitempty"`
    NativeFacts     bool `json:"native_facts,omitempty"`
}
```

| 字段 | 说明 |
| --- | --- |
| `supported` | 是否能描述叶子数据项。 |
| `field_info` | 是否能获取字段、列或文档字段信息。 |
| `statistics` | 是否能获取行数、大小、采样统计等统计信息。 |
| `indexes` | 是否能获取索引信息。 |
| `constraints` | 是否能获取主键、唯一约束、外键等约束信息。 |
| `partitioning` | 是否能获取表分区策略、分区键和分区数量等正式分区事实。 |
| `spatial_facts` | 是否能获取空间字段、SRID、范围等空间事实。 |
| `sampling` | 是否需要或支持通过采样推断结构。 |
| `native_facts` | 是否能获取引擎原生事实，如对象大小、ETag、修改时间、存储类别等。 |

字段、约束、统计、索引、分区、空间、采样和原生详情能力必须分别声明；不能因为实现了 `CatalogFactsProvider` 就默认拥有全部子能力。`constraints=true` 表示 Provider 能返回主键、唯一约束或外键中的至少一类正式约束事实；`partitioning=true` 表示 Provider 能返回正式 `TablePartitioningFacts`，不得仅凭供应商私有 `native` 字段声明。

### 3.4 StoreCapability

```go
type StoreCapability struct {
    StreamRead                bool                                  `json:"stream_read,omitempty"`
    StreamWrite               bool                                  `json:"stream_write,omitempty"`
    RangeRead                 bool                                  `json:"range_read,omitempty"`
    RangeWrite                bool                                  `json:"range_write,omitempty"`
    Delete                    bool                                  `json:"delete,omitempty"`
    BatchRead                 bool                                  `json:"batch_read,omitempty"`
    TableReadSession          bool                                  `json:"table_read_session,omitempty"`
    RecordReadSession         bool                                  `json:"record_read_session,omitempty"`
    TableReadSpatialTransform bool                                  `json:"table_read_spatial_transform,omitempty"`
    BatchWrite                bool                                  `json:"batch_write,omitempty"`
    TableWriteSession         bool                                  `json:"table_write_session,omitempty"`
    TableWritePrepare         bool                                  `json:"table_write_prepare,omitempty"`
    BoundedWatermarkRead      bool                                  `json:"bounded_watermark_read,omitempty"`
    ChangeStreamRead          *ChangeStreamReadCapability           `json:"change_stream_read,omitempty"`
    TableUpsert               *TableUpsertCapability                `json:"table_upsert,omitempty"`
    PartitionedTableChangeApply *PartitionedTableChangeApplyCapability `json:"partitioned_table_change_apply,omitempty"`
    TableSpatialEncoding      *NativeTableSpatialEncodingCapability `json:"table_spatial_encoding,omitempty"`
}

type ChangeStreamReadCapability struct {
    Supported     bool     `json:"supported"`
    Partitioned   bool     `json:"partitioned"`
    Seek          bool     `json:"seek"`
    PauseResume   bool     `json:"pause_resume"`
    PositionTypes []string `json:"position_types"`
}

type PartitionedTableChangeApplyCapability struct {
    Supported            bool     `json:"supported"`
    AtomicPositionCommit bool     `json:"atomic_position_commit"`
    Monotonic            bool     `json:"monotonic"`
    PositionTypes        []string `json:"position_types"`
    Operations           []string `json:"operations"`
}

type TableUpsertCapability struct {
    Supported  bool `json:"supported"`
    Idempotent bool `json:"idempotent"`
}

type NativeTableSpatialEncodingCapability struct {
    GeometryReadEncodings  []string `json:"geometry_read_encodings,omitempty"`
    GeometryWriteEncodings []string `json:"geometry_write_encodings,omitempty"`
    ReadTransform          bool     `json:"read_transform,omitempty"`
    WriteTransform         bool     `json:"write_transform,omitempty"`
    NativeSpatialFunctions bool     `json:"native_spatial_functions,omitempty"`
}
```

| 字段 | 含义 | 必须对应的 Provider |
| --- | --- | --- |
| `stream_read` | 顺序流式读取单个对象、文件或二进制内容。 | `ContentReadableProvider` |
| `stream_write` | 顺序流式创建或覆盖单个对象、文件内容。 | `ContentWritableProvider` |
| `range_read` | 从指定 byte range 读取内容。 | `RangeReadableProvider`，或 `OpenContent()` 明确支持 offset / length |
| `range_write` | 向指定 byte range / offset 写入内容。 | `RangeWritableProvider` |
| `delete` | 删除 catalog leaf 或空 branch 对应的外部资源。 | `ResourceDeleteProvider` |
| `batch_read` | 执行一次有界的固定 schema 结构化 item 批量读取。动态 schema collection 使用 `record_read_session`，图数据使用 `GraphSampleProvider` / `GraphQueryProvider`。 | `BatchReadableProvider` |
| `table_read_session` | 打开一次表读取会话并连续读取批次，避免大表 `LIMIT/OFFSET` 翻页退化。 | `TableReadSessionProvider` |
| `record_read_session` | 打开一次动态 schema 记录游标并连续读取原生 record；collection 不因此获得 table 语义。 | `RecordReadSessionProvider` |
| `table_read_spatial_transform` | 读取表时是否可通过 read hints 执行空间 CRS 转换。该字段是早期布尔声明，后续优先使用 `table_spatial_encoding.read_transform`。 | `BatchReadableProvider` / `TableReadSessionProvider` |
| `batch_write` | 按批次写入结构化 item。 | `BatchWritableProvider` |
| `table_write_session` | 打开一次表写入会话并连续写入批次，避免每批重复建立 COPY / bulk load 会话。 | `TableWriteSessionProvider` |
| `table_write_prepare` | 执行表级写入前准备动作，如 ensure database / schema、create table、目标表结构校验和安全 schema evolution。该能力不写入数据行，也不承载 replace / append 策略。 | `TableWritePreparer` |
| `bounded_watermark_read` | 在一致性读边界内冻结复合 watermark 上界，并稳定读取 `(committed, upper_bound]`。 | `BoundedWatermarkReadProvider` |
| `change_stream_read` | 按 partition position seek 并持续 poll 原始变化记录，支持受控 pause/resume/close。 | `ChangeStreamReaderProvider` |
| `table_upsert` | 按稳定键批量新增或更新；`idempotent=true` 表示同一批重复应用得到相同目标状态。 | `TableUpsertProvider` |
| `partitioned_table_change_apply` | 将单 partition 的 mapped table changes 与目标 apply ledger position 在同一数据库事务提交；必须声明支持的 position types 和 operations。 | `PartitionedTableChangeApplyProvider` |
| `table_spatial_encoding` | native table provider 可与 ADDP table pipeline 交换的空间 geometry row encoding 能力。读侧能力对应 `BatchReadableProvider` / `TableReadSessionProvider`；写侧能力对应 `BatchWritableProvider` / `TableWriteSessionProvider`。 | 按读写子能力分别对应 native table read / write provider |

`read` / `write` 总开关无独立调用价值，不进入 Store 能力声明。`delete` 只表达引擎能删除对应 catalog 资源，不表达上层业务删除策略、回收流程或级联清理。`atomic_rename`、`transactions`、`formats` 不作为 Store 顶层字段；如有真实调用方，应在对应 Provider 或更具体能力中声明。

`table_spatial_encoding` 只表达跨出 native table provider 后的 row value 编码，不表达数据库内部类型。PostGIS、MySQL `geometry`、Oracle `MDSYS.SDO_GEOMETRY` 这类 engine-internal type 不应作为 encoding 暴露。PostgreSQL / PostGIS 与 MySQL 声明 `geometry_read_encodings=["ewkb","geojson"]`、`geometry_write_encodings=["ewkb"]`、`read_transform=true`、`native_spatial_functions=true`；Oracle 当前声明 `geometry_read_encodings=["ewkb"]` 与 `native_spatial_functions=true`，不声明写入或 transform。各 Provider 读取时必须把内部 geometry binary/对象转换为标准 EWKB 或 GeoJSON，不得把数据库内部 geometry 二进制、普通 `batch_read` / `batch_write` 或建表能力误报为空间行值编码能力。MySQL 8 的 ADDP 空间行值能力只支持二维 geometry；Z/M 输入必须明确拒绝，不得静默降维。

数据库插件声明 `native_spatial_functions=true` 时必须实现 `SpatialFeatureReadProvider`，实现该 Provider 时也必须声明该能力，capability validator 对二者做双向一致性校验。该 Provider 的跨模块返回值固定为 EWKB、centroid EWKB、SRID 与 `SpatialInfo`，数据库原生函数和 axis order 处理留在插件内部；当前 PostgreSQL/PostGIS 与 MySQL 都实现该接口。该接口不是第二套表读取路线，只服务按稳定 identity field 精确读取一个空间要素的交互操作。

`bounded_watermark_read` 与普通 `batch_read` / `table_read_session` 不等价。前者必须冻结 execution 上界、使用稳定复合游标并能从读取行生成 committed position。PostgreSQL 与 MySQL 当前都声明该能力，并分别在一致性只读事务和 InnoDB consistent snapshot 中冻结上界；两者读取空间字段时都必须按协商后的标准 geometry row encoding 返回，不得泄漏数据库内部二进制。`table_upsert` 也不能从 `batch_write` 推导；只有目标 Provider 能校验唯一键并以幂等冲突处理提交批次时才能声明。PostgreSQL 与 MySQL 声明幂等 `table_upsert`。MySQL 目标必须使用 InnoDB，并要求配置键精确匹配非空主键或唯一约束；为避免 `ON DUPLICATE KEY UPDATE` 被其他唯一约束触发，目标表不得存在与配置键不同的唯一约束。

`change_stream_read` 与 content `stream_read`、`batch_read` 都不等价。它必须声明 `partitioned=true`、`seek=true`、`pause_resume=true`，Kafka 第一版 `position_types` 只允许 `kafka_offset/v1`。实现该能力的 reader 必须同时返回每分区当前 earliest/latest position，供运行时计算 lag 和 retention 窗口；这不是独立能力开关。该能力只表达原始 record 和 position 读取，不声明 JSON/Avro/Protobuf、Debezium envelope、Transfer target apply 或 exactly-once。第一版仅业务 Kafka Engine 声明该能力；Infra Kafka 不产生 System capabilities 记录。

`partitioned_table_change_apply` 与普通 `table_upsert` 不等价。PostgreSQL 与 MySQL 当前都声明 `atomic_position_commit=true`、`monotonic=true`、`position_types=["kafka_offset/v1"]`、`operations=["upsert","delete","skip"]`。PostgreSQL 位置账本固定为业务目标库的 `addp_transfer.apply_positions`；MySQL 位置账本固定为目标业务数据库内的 `_addp_transfer_apply_positions` InnoDB 表，不跨数据库创建私有 schema/database。MySQL 同名账本表结构不符合 Provider 唯一规范时必须直接失败。两种实现都必须把目标 upsert/delete/skip 与账本 position 在同一目标数据库事务提交。该能力不表示 Kafka 与 Infra state 之间存在分布式 exactly-once。`skip` 只推进目标 ledger，不修改业务行，供 Transfer 在 dead-letter 事实已持久化后越过该 position。插件不得重复或声明未实现操作。

### Checkpoint / Resume 能力

`table_read_session`、`record_read_session`、`batch_read`、`range_read` 只说明引擎能连续读取、批量读取或按 byte range 读取，不等于支持 checkpoint resume。是否可以从失败点继续执行，必须另行声明恢复语义。

第一版规则：

1. 可恢复读取必须能返回 provider 可解释的 `resume_marker`，并能用该 marker 重新打开读取会话。当前 Go 草案使用 `common/resume.Marker`，引擎表读取侧通过 `TableReadSessionOptions.ResumeMarker` 输入恢复标记，通过 `ResumeMarkerProvider` 输出读取标记。
2. 可恢复 marker 必须携带或关联 `fingerprint`，用于校验源资源、schema、排序、查询条件、读取选项和快照语义未变化。
3. PostgreSQL cursor session 这类进程内 / 事务内 cursor 只属于连续读取优化；进程失败后 cursor 不存在，不能声明 checkpoint resumable。
4. `LIMIT/OFFSET` 可作为 restartable 的从头重跑实现细节，但在没有稳定排序键、快照或版本标记前，不得声明 checkpoint resumable。
5. 写侧 resumable 必须声明提交边界和幂等语义。普通 batch insert、COPY session、对象流式写入或文件流式写入都不能仅凭 provider 存在就推导为可断点续写。当前 Go 草案通过 `TableWriteSessionOptions.ResumeMarker` 输入恢复标记，通过 `CommitMarkerProvider` 暴露提交标记。
6. provider 暂未实现 marker 消费时，收到 `TableReadSessionOptions.ResumeMarker` 或 `TableWriteSessionOptions.ResumeMarker` 必须显式返回 unsupported error，不得静默忽略后从头读取或重新写入。
7. 若后续需要在 `engine.capabilities/v1` 中正式表达该能力，优先放在具体 store 能力的扩展段，而不是新增与 Provider 无关的顶层布尔字段。

对象存储通常声明 `stream_read`、`range_read`，是否声明 `stream_write` / `delete` 必须取决于是否提供对应写入或删除 Provider；对象存储通常不声明 `range_write`。文件系统是否支持 `range_write` / `delete` 必须按真实能力和 Provider 实现声明。

---

## 四、ComputeCapabilities

```go
type ComputeCapabilities struct {
    Query    *QueryCapability    `json:"query,omitempty"`
    Workflow *WorkflowCapability `json:"workflow,omitempty"`
    Script   *ScriptCapability   `json:"script,omitempty"`
    Inference *InferenceCapability `json:"inference,omitempty"`
}
```

`compute` 表达引擎可被 ADDP 统一调用的计算运行时能力，而不是 UI 开发模式标签。旧版 `dev_modes` 只能回答“应该出现在哪个开发界面”，不能表达查询语言、运行协议、动态算子、脚本模式、AI 推理操作和对应 Provider，因此不再进入 `engine.capabilities/v1`。

Develop 等上层模块如仍需面向用户的开发入口，应从 `compute` 能力派生：

| 派生开发入口 | 能力事实源 |
| --- | --- |
| 查询工作台 | `compute.query.supported=true` |
| 工作流编辑器 | `compute.workflow.supported=true` |
| Notebook 无头执行 | `compute.script.supported=true`，并结合 `compute.script.modes` |
| Notebook 交互编辑 | 在 Notebook 模式基础上还要求 `compute.script.interactive=true` |
| AI 推理调用 | `compute.inference.supported=true` |

这些派生名称可作为前端路由或展示文案使用，但不得反向写回为 `dev_modes` 字段。

### 4.1 QueryCapability

```go
type QueryCapability struct {
    Supported       bool     `json:"supported"`
    Languages       []string `json:"languages"`
    DefaultLanguage string   `json:"default_language,omitempty"`
    ResultKinds     []string `json:"result_kinds,omitempty"`
    ReadOnly        bool     `json:"read_only,omitempty"`
    SupportsExplain bool     `json:"supports_explain,omitempty"`
    SupportsCancel  bool     `json:"supports_cancel,omitempty"`
    Parameters      *QueryParameterCapability `json:"parameters,omitempty"`
    Federation      *QueryFederationCapability `json:"federation,omitempty"`
}

type QueryParameterCapability struct {
    Supported bool     `json:"supported"`
    Languages []string `json:"languages"`
    Types     []string `json:"types"`
}

type QueryFederationCapability struct {
    Supported         bool     `json:"supported"`
    RuntimeAPI        string   `json:"runtime_api"`
    SourceEngineTypes []string `json:"source_engine_types,omitempty"`
    ObjectFormats     []string `json:"object_formats,omitempty"`
}
```

| 字段 | 说明 |
| --- | --- |
| `supported` | 是否可作为查询运行时。 |
| `languages` | 支持的查询语言，如 `sql`、`mql`、`cypher`、`opensearch_dsl`、`mango`。 |
| `default_language` | 默认编辑器语言和样例查询语言。 |
| `result_kinds` | 查询结果形态，如 `table`、`document`、`graph`、`scalar`。 |
| `read_only` | 运行时是否只允许只读查询。 |
| `supports_explain` | 是否支持查询计划 / 性能诊断。 |
| `supports_cancel` | 是否支持取消运行中的查询。 |
| `parameters` | 可选的类型化查询参数能力；声明后必须由 Provider 原生安全绑定。 |
| `federation` | 可选的多数据源联邦查询能力；声明后必须实现 `FederatedQueryRuntimeProvider`。 |

`parameters.languages` 只列出当前 Provider 已实现参数绑定的查询语言，`parameters.types` 第一版只允许 `string`、`integer`、`number`、`boolean`。查询工作台只能在当前语言位于该列表时开放参数定义。SQL 的用户输入语法统一为 `:name`，Provider 必须编译为当前驱动占位符并通过 `QueryOptions.Args` 绑定；Cypher 使用 `$name` 并通过原生参数 Map 执行；MQL 使用 `{\"$param\":\"name\"}` 结构化参数节点，在 JSON 解析后替换为类型化值。参数能力不得通过字符串替换实现，也不得用于动态标识符或查询片段。

DuckDB Runtime 第一阶段声明 `runtime_api="addp.query-runtime/v1"`、`source_engine_types=["postgresql","mysql","minio","s3"]`、`object_formats=["parquet"]`。能力只声明当前真正实现并验证过的连接器；Doris、ClickHouse、Spark SQL、MongoDB、Neo4j、Kafka 等不能仅因 ADDP 已支持该 Engine 类型就自动列入。

联邦 Runtime 的资源引用名由 Source Owner preview 作为 `ResourceFact.query_names.federated_sql` 提供，并与 Runtime 解析规则使用同一共享标识符规范。DuckDB 当前形式为 `<sanitized_source_engine_name>.<schema>.<table>` 或对象表的 `<sanitized_source_engine_name>.<table>`。Copilot 和前端不得各自实现 engine name 清洗或从 locator/full_name 拼接联邦引用。

查询语言差异只通过 `languages` / `default_language` 和 `QueryRequest.Language` 表达，不新增按数据库类别拆分的 query provider。`result_kinds=document` 只表示原生查询结果可能是 JSON document / record 形态，不表示 data item 的 `data_type=document`。图结构查询如果需要节点 / 关系结构结果，仍使用 `GraphQueryProvider`。

查询工作台的默认样例不属于静态 capability。样例必须在用户切换具体 Engine Instance 时，通过执行授权消费该实例连接、实时发现有数据的 Catalog leaf，再由 Query Runtime 按 `default_language` 生成。Catalog 发现失败或当前实例没有有数据的 leaf 时返回明确错误，不允许用固定诊断查询伪装成实例样例。

### 4.2 WorkflowCapability

```go
type WorkflowCapability struct {
    Supported             bool     `json:"supported"`
    RuntimeAPI            string   `json:"runtime_api"`
    DynamicOperators      bool     `json:"dynamic_operators"`
    SupportedOperatorMode []string `json:"supported_operator_mode,omitempty"`
}
```

| 字段 | 说明 |
| --- | --- |
| `supported` | 是否可作为工作流运行时。 |
| `runtime_api` | 工作流运行时接口版本或协议。 |
| `dynamic_operators` | 算子是否可动态发现。 |
| `supported_operator_mode` | 支持的算子运行模式。 |

工作流引擎的静态能力声明只回答“是否具备统一工作流运行时，以及使用哪个 runtime API”。算子列表、算子参数、分类、输入输出端口等动态能力，不写入 `capabilities`，必须通过 `WorkflowRuntimeProvider.ListOperators()` 获取；当前 `addp.workflow/v1` 对应的标准 HTTP 入口为 `GET /api/operators`。工作流执行通过 `WorkflowRuntimeProvider.ExecuteWorkflow()`，对应标准 HTTP 入口为 `POST /api/workflow`。执行期绑定的外部运行时资源（例如 `spark_workflow` 绑定某个 Spark 资源 ID）属于执行请求参数，不属于能力声明。

`spatial_workspaces` 中需要持续通过 Workflow Runtime 读写数据的领域工作区只保存 `bound_runtime_engine_id`；当前仅 `sdx_postgresql` 属于此类。`sdx_postgis` 的 Runtime 只服务于显式启用动作，不参与表读写路由，因此不保存持久绑定。工作区不得保存 `runtime_engine_type` 作为能力判断或运行时白名单；调用方必须读取绑定实例的 Runtime Descriptor，并以 `compute.workflow.runtime_api` 和动态 direct 算子目录完成校验。

Workflow Runtime 的注册与健康状态是工作区绑定的控制面事件。兼容 Runtime 进入 `online` 后，System 必须立即重新协调当前 active Engine Instance 中声明为持续 Runtime 依赖的工作区（当前为 `sdx_postgresql`），使用同一 Runtime 的动态 direct 算子目录计算并持久化 `bound_runtime_engine_id`。`sdx_postgis` 仅在显式启用动作中临时发现 Runtime。该协调不得依赖 System 与 Runtime 的启动顺序，也不得要求用户重建存储引擎、手工改写 capabilities 或通过业务扫描触发绑定。

`dynamic_operators=true` 表示调用方可以通过 Provider 动态发现算子。它不是“已有算子列表”的缓存，也不是某个模块对该引擎的适配状态。

当手动注册的扩展运行时声明 `compute.workflow.supported=true` 且 `runtime_api="addp.workflow/v1"` 时，System 保存前必须按该协议做只读探测：`GET /health` 验证运行时可达，`GET /api/operators` 验证算子列表结构，并校验返回算子的 `engine_type` 与注册的引擎类型一致。该探测由能力声明触发，不得依赖 `geopython_workflow`、`spark_workflow`、`math_workflow` 等具体内置类型名称；Math Workflow 参考实现也走同一路径。

### 4.3 ScriptCapability

```go
type ScriptCapability struct {
    Supported bool     `json:"supported"`
    Modes     []string `json:"modes"`
    Languages []string `json:"languages,omitempty"`
}
```

| 字段 | 说明 |
| --- | --- |
| `supported` | 是否可作为脚本或 Notebook 运行时。 |
| `modes` | 交互模式，如 `notebook`。 |
| `interactive` | 是否支持由 owner 模块创建、代理和关闭短期隔离交互会话；不得仅因存在共享 Lab 入口而声明。 |
| `languages` | 支持的语言，如 `python`。 |

### 4.4 InferenceCapability

```go
type InferenceCapability struct {
    Supported  bool     `json:"supported"`
    RuntimeAPI string   `json:"runtime_api"`
    Operations []string `json:"operations"`
    Modalities []string `json:"modalities,omitempty"`
    Streaming  bool     `json:"streaming,omitempty"`
}
```

| 字段 | 说明 |
| --- | --- |
| `supported` | 是否可作为统一 AI 推理运行时。 |
| `runtime_api` | 固定为当前实现的 `addp.inference/v1`。 |
| `operations` | Runtime 真正支持的标准操作，取值为 `chat`、`embedding`、`rerank` 的子集。 |
| `modalities` | Runtime 可接收的输入模态，取值为 `text`、`image` 的子集。 |
| `streaming` | `chat` 是否支持标准流式响应。 |

该能力只表达 Runtime 数据面，不展开动态 Provider Connection、Model Deployment 或 Model Profile。System 保存或刷新 `inference_runtime` Engine Instance 时只探测 `/health` 和 Runtime capability；模型列表及凭据由 Inference owner 控制面管理。声明该能力的插件必须实现 `InferenceRuntimeProvider`，调用方不得根据厂商名称选择私有客户端。

第一版 `inference_runtime` 固定为平台内置单实例。调用方通过 System Runtime Descriptor 发现唯一 active 且声明该能力的实例；零个或多个候选都必须明确失败。后续如需按网络区域、安全域、GPU 集群、故障域或 SLA 部署多个 Runtime，必须先增加显式 Runtime Engine Instance 绑定和实例身份规则，不能按列表顺序或健康状态自动切换。

---

## 五、能力展示模型

能力详情页由 System 后端生成 `capabilities_view`，前端按展示模型渲染并根据当前语言解析 i18n key。

```go
type CapabilitiesView struct {
    Summary  []CapabilityViewBadge   `json:"summary"`
    Sections []CapabilityViewSection `json:"sections"`
    JSONView []CapabilityJSONNode    `json:"json_view"`
}
```

展示模型负责：

- 决定哪些能力进入主展示，哪些只进入 `json_view`。
- 决定能力分组、排序、标签、状态和解释。
- 返回 `label_key`、`description_key`、`reason_key`、`value_key` 和参数，由前端翻译。
- 将未知 `extensions` 转换为可陈列的 key-value 项。

主展示原则：

- 不直接展示布尔字段名，展示业务结论。
- 不展示空分组、空数组和 false 字段，除非该“不支持”会影响用户操作。
- 不把 `schema_version`、`path_version`、`i18n_key`、`supported` 放在主信息区。
- `extensions` 只要存在就展示；未知结构按 key-value 陈列。
- `extensions` 在展示模型中表达当前实例的扩展状态，不等同于核心能力开关；实例未安装的扩展应使用 `not_installed` 状态，而不是 `engine_unavailable`。
- 对 PostgreSQL 这类存在核心扩展和附属扩展的引擎，主展示应突出影响 ADDP 能力面的核心扩展，如 PostGIS、pgvector；Topology、Tiger Geocoder 等附属扩展可合并为“其他已安装扩展”类信息。
- 完整能力声明通过“查看 JSON”入口以 key-value 树形方式查看，不在主页面展示原始 JSON 文本。

---

## 八、校验规则

- `schema_version` 必须存在且等于 `engine.capabilities/v1`。
- 声明 `storage.catalog.supported=true` 的插件必须实现 `CatalogProvider`。
- 声明 `storage.facts.supported=true` 的插件必须实现 `CatalogFactsProvider` 或明确的采样 provider。
- `storage.catalog_model` 是对外 CatalogModel 事实源；如果插件同时实现 `CatalogModelProvider`，其返回值必须与 `storage.catalog_model` 完全一致。
- 声明 `storage.store.stream_read=true` 的插件必须实现 `ContentReadableProvider`。
- 声明 `storage.store.stream_write=true` 的插件必须实现 `ContentWritableProvider`。
- 声明 `storage.store.bounded_watermark_read=true` 的插件必须实现 `BoundedWatermarkReadProvider`。
- 声明 `storage.store.change_stream_read.supported=true` 的插件必须实现 `ChangeStreamReaderProvider`，且 `partitioned`、`seek`、`pause_resume` 必须为 true，`position_types` 必须非空。
- 声明 `storage.store.table_upsert.supported=true` 的插件必须实现 `TableUpsertProvider`，且当前必须同时声明 `idempotent=true`。
- 声明 `storage.store.partitioned_table_change_apply.supported=true` 的插件必须实现 `PartitionedTableChangeApplyProvider`，且 `atomic_position_commit`、`monotonic` 必须为 true，`position_types` 和 `operations` 必须非空。
- 声明 `storage.store.range_read=true` 的插件必须实现 `RangeReadableProvider`，或在 `ContentReadableProvider.OpenContent()` 中明确支持 offset / length。
- 声明 `storage.store.range_write=true` 的插件必须实现 `RangeWritableProvider`。
- 声明 `storage.store.delete=true` 的插件必须实现 `ResourceDeleteProvider`。
- 声明 `storage.store.batch_read=true` 的插件必须实现 `BatchReadableProvider`。
- 声明 `storage.store.table_read_session=true` 的插件必须实现 `TableReadSessionProvider`。
- 声明 `storage.store.record_read_session=true` 的插件必须实现 `RecordReadSessionProvider`。
- 声明 `storage.store.batch_write=true` 的插件必须实现 `BatchWritableProvider`。
- 声明 `storage.store.table_write_session=true` 的插件必须实现 `TableWriteSessionProvider`。
- 声明 `storage.store.table_write_prepare=true` 的插件必须实现 `TableWritePreparer`。
- 声明 `compute.query.supported=true` 的插件必须实现 `QueryRuntimeProvider` 或 `FederatedQueryRuntimeProvider`。声明 `compute.query.federation.supported=true` 时必须实现 `FederatedQueryRuntimeProvider`，且 `runtime_api` 非空。
- 声明 `compute.query.parameters.supported=true` 的插件必须对 `parameters.languages` 中每种语言实现类型化参数绑定，并拒绝缺失、未知或未使用参数；不得只声明 UI 能力而把参数插值交给调用方。
- 声明 `compute.workflow.supported=true` 的编译期插件必须实现 `WorkflowRuntimeProvider`。通过 System 注册的 `addp.workflow/v1` 外部运行时不要求独立编译期插件，由 Common 唯一的 `HTTPWorkflowRuntimeProvider` 消费；System 必须在注册时校验 capabilities 并完成协议探测。
- 声明 `compute.script.supported=true` 的插件必须实现 `ScriptRuntimeProvider`。
- 声明 `compute.inference.supported=true` 的插件必须实现 `InferenceRuntimeProvider`，且 `runtime_api="addp.inference/v1"`、`operations` 非空。
- 反向也必须成立：插件实现 `CatalogModelProvider`、`CatalogProvider`、`CatalogFactsProvider`、`DynamicSchemaSamplingProvider`、具体 Store Provider、`ChangeStreamReaderProvider`、`QueryRuntimeProvider`、`FederatedQueryRuntimeProvider`、`GraphQueryProvider`、`WorkflowRuntimeProvider`、`ScriptRuntimeProvider` 或 `InferenceRuntimeProvider` 时，`Capabilities()` 必须声明对应能力。`StoreProvider` 本身只是 marker，不单独触发能力声明；以具体读写 Provider 为准。
- capabilities 由插件返回结构体，System 统一序列化为 JSONB。插件 `Capabilities()` 是 Provider 能力模板，不得做实例连接或运行时探测。
- 已注册编译期插件引擎的落库能力事实源是插件 `Capabilities()` 与可选实例能力解析结果。普通 Engine API、内部自注册接口和 Registry 能力注册接口收到此类插件引擎提交的 `capabilities` 时都必须忽略，并改用插件模板和实例解析结果生成落库声明。未编译独立插件的 `addp.workflow/v1` Runtime 无论是否属于 ADDP 默认部署，都必须在自注册或手动注册时提交完整 `engine.capabilities/v1`；System 校验并保存该声明，不能按 `engine_type` 生成内置能力。自注册脚本不得额外声明 `workflow_runtime`、`script_runtime` 等平行运行时能力。
- 旧 capabilities 结构不再兼容，发现旧结构可直接刷新或清空。
- Runtime Engine Instance 由对应 Runtime 在自身服务就绪后通过统一接口异步注册。System 不在启动阶段预置或等待内置 Runtime；注册失败不改变模块 readiness，调用方在实际需要该能力时返回明确的 Runtime 不可用错误。

---

## 九、示例

PostgreSQL 示例：

```json
{
  "schema_version": "engine.capabilities/v1",
  "engine_type": "postgresql",
  "engine_family": "tabular",
  "storage": {
    "catalog_model": {
      "path_version": "catalog.path/v1",
      "root_term": "server",
      "levels": [
        {"term": "schema", "kinds": ["schema"], "role": "branch", "i18n_key": "engine.term.schema"},
        {"term": "table", "kinds": ["table", "view", "materialized_view"], "role": "leaf", "i18n_key": "engine.term.table"}
      ]
    },
    "catalog": {"supported": true, "real_time": true, "system_filtering": true},
    "facts": {"supported": true, "field_info": true, "statistics": true, "indexes": true, "constraints": true, "spatial_facts": true},
    "store": {"batch_read": true}
  },
  "compute": {
    "query": {"supported": true, "languages": ["sql"], "default_language": "sql", "result_kinds": ["table", "scalar"]}
  }
}
```

MinIO 示例：

```json
{
  "schema_version": "engine.capabilities/v1",
  "engine_type": "minio",
  "engine_family": "object",
  "storage": {
    "catalog_model": {
      "path_version": "catalog.path/v1",
      "root_term": "service",
      "levels": [
        {"term": "bucket", "kinds": ["bucket"], "role": "branch", "i18n_key": "engine.term.bucket"},
        {"term": "prefix", "kinds": ["prefix"], "role": "branch", "optional": true, "i18n_key": "engine.term.prefix"},
        {"term": "object", "kinds": ["object"], "role": "leaf", "i18n_key": "engine.term.object"}
      ]
    },
    "catalog": {"supported": true, "real_time": true},
    "facts": {"supported": true, "native_facts": true},
    "store": {"stream_read": true, "range_read": true},
    "semantics": ["bucket", "prefix_listing", "object", "stream_read", "range_read"],
    "not_supported": ["range_write", "real_directory"]
  }
}
```

Kafka 示例（插件实现完成后才允许落库声明）：

```json
{
  "schema_version": "engine.capabilities/v1",
  "engine_type": "kafka",
  "engine_family": "event_stream",
  "storage": {
    "catalog_model": {
      "path_version": "catalog.path/v1",
      "root_term": "service",
      "levels": [
        {"term": "topic", "kinds": ["topic"], "role": "leaf", "i18n_key": "engine.term.topic"}
      ]
    },
    "catalog": {
      "supported": true,
      "real_time": true,
      "node_kinds": ["topic"]
    },
    "facts": {"supported": true, "native_facts": true},
    "store": {
      "change_stream_read": {
        "supported": true,
        "partitioned": true,
        "seek": true,
        "pause_resume": true,
        "position_types": ["kafka_offset/v1"]
      }
    }
  }
}
```

# ADDP 引擎能力声明规范

本文规范 engine plugin 的结构化能力声明。插件接口边界见 [addp引擎插件接口规范.md](addp引擎插件接口规范.md)，Catalog 路径语义见 [addp存储引擎路径体系规范.md](addp存储引擎路径体系规范.md)。

能力声明用于回答“一个引擎实例能被哪些模块以什么方式消费”。上层模块不得只根据 `engine_type` 或 `engine_family` 猜能力。

当前版本固定为：

```text
engine.capabilities/v1
```

---

## 一、基本原则

- capabilities 是模块消费能力的事实来源；Provider 是能力实现承诺。
- 声明了可调用能力，就必须有对应 Provider、Adapter 或明确的模块执行面。
- Catalog、Metadata、Store、Query、Workflow、Script、Transfer、Preview 是不同能力面，不能混用。
- 核心结构表达“ADDP 当前可用能力”。引擎本身不具备的能力、ADDP 尚未实现的能力，可由展示模型派生说明，不强行塞入核心结构。
- 没有明确模块消费价值的字段不进入核心声明；后续有真实调用方时再扩展。
- `extensions` 只承载引擎特有补充信息，不得替代核心字段。

能力详情页不得直接平铺原始 JSON 字段。System 后端应生成 `capabilities_view` 供前端渲染；完整 JSON 仅作为技术查看入口保留。

---

## 二、顶层结构

```go
type EngineCapabilities struct {
    SchemaVersion string                 `json:"schema_version"`
    EngineType    string                 `json:"engine_type"`
    EngineFamily  string                 `json:"engine_family"`
    Storage       *StorageCapabilities   `json:"storage,omitempty"`
    Compute       *ComputeCapabilities   `json:"compute,omitempty"`
    Transfer      *TransferCapabilities  `json:"transfer,omitempty"`
    Preview       *PreviewCapabilities   `json:"preview,omitempty"`
    Limits        map[string]interface{} `json:"limits,omitempty"`
    Extensions    map[string]interface{} `json:"extensions,omitempty"`
}
```

| 字段 | 说明 | 保留要求 |
| --- | --- | --- |
| `schema_version` | 能力声明结构版本，必须为 `engine.capabilities/v1`。 | 必须保留。 |
| `engine_type` | 插件类型，如 `postgresql`、`minio`、`neo4j`。 | 必须保留。 |
| `engine_family` | 主引擎族，如 `tabular`、`document`、`graph`、`object`、`file`、`workflow`、`script`。 | 必须保留，但只作为粗分类。 |
| `storage` | 存储、目录、元数据、内容访问能力。 | 具备存储能力的引擎必须声明。 |
| `compute` | 查询、工作流、脚本运行能力。 | 具备计算能力的引擎必须声明。 |
| `transfer` | Transfer 读写适配、连接器和格式范围。 | 需要 Transfer 消费时声明。 |
| `preview` | Manager 内容预览能力。 | 需要预览时声明。 |
| `limits` | 跨能力通用限制，如预览大小、超时建议。 | 可选，有真实调用方时声明。 |
| `extensions` | 引擎特有扩展。 | 可选，不得替代核心字段。 |

---

## 三、StorageCapabilities

```go
type StorageCapabilities struct {
    CatalogModel *CatalogModelSpec   `json:"catalog_model,omitempty"`
    Catalog      *CatalogCapability  `json:"catalog,omitempty"`
    Metadata     *MetadataCapability `json:"metadata,omitempty"`
    Store        *StoreCapability    `json:"store,omitempty"`
    Semantics    []string            `json:"semantics,omitempty"`
    NotSupported []string            `json:"not_supported,omitempty"`
}
```

`storage` 是否存在表示引擎是否具备存储能力。存储主类型由顶层 `engine_family` 表达，不再声明 `storage.families`。

| 字段 | 说明 | 保留要求 |
| --- | --- | --- |
| `catalog_model` | 目录层级稳定模型，决定页面树、路径解析、扫描范围和指纹计算。 | 具备 catalog 的存储引擎必须声明。 |
| `catalog` | 是否能实时列出引擎里的目录和数据项。 | 需要浏览或扫描真实目录时声明。 |
| `metadata` | 是否能描述叶子 item 的字段、统计、索引、约束、空间信息或原生属性。 | Meta 扫描和资产描述需要时声明。 |
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
    Term      string   `json:"term"`
    Kinds     []string `json:"kinds"`
    Container bool     `json:"container"`
    Item      bool     `json:"item,omitempty"`
    Optional  bool     `json:"optional,omitempty"`
    I18nKey   string   `json:"i18n_key,omitempty"`
}
```

`path_version` 当前固定为 `catalog.path/v1`。

| 字段 | 说明 |
| --- | --- |
| `path_version` | 目录路径模型版本。 |
| `root_term` | 根语义，如 `server`、`service`、`root`。 |
| `levels.term` | 层级语义，如 `bucket`、`prefix`、`object`。 |
| `levels.kinds` | 该层可能出现的节点类型，如 `table`、`view`、`object`。 |
| `levels.container` | 该层是否可以继续展开子节点。 |
| `levels.item` | 该层是否是可被描述、预览、读取或写入的数据项。 |
| `levels.optional` | 该层是否可省略。 |
| `levels.i18n_key` | 可选国际化 key。 |

推荐层次：

| 引擎 | 层次模型 |
| --- | --- |
| PostgreSQL | `server -> schema -> table/view/materialized_view` |
| MySQL / Doris / ClickHouse | `server -> database -> table/view` |
| MongoDB | `server -> database -> collection` |
| Neo4j | `server -> database -> label/relationship` |
| S3 / MinIO | `service -> bucket -> prefix -> object` |
| NFS | `root -> directory -> file` |

`StorageCapabilities.CatalogModel` 是对外 CatalogModel 事实源。如果插件同时实现 `CatalogModelProvider`，其返回值必须与 `storage.catalog_model` 完全一致。

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
| `system_filtering` | 是否能过滤系统库、系统 schema 等噪声。 | 数据库类引擎建议声明。 |
| `node_kinds` | catalog 可能返回的节点类型集合。 | 建议声明。 |

### 3.3 MetadataCapability

```go
type MetadataCapability struct {
    Supported       bool `json:"supported"`
    FieldSchema     bool `json:"field_schema,omitempty"`
    Statistics      bool `json:"statistics,omitempty"`
    Indexes         bool `json:"indexes,omitempty"`
    Constraints     bool `json:"constraints,omitempty"`
    SpatialMetadata bool `json:"spatial_metadata,omitempty"`
    Sampling        bool `json:"sampling,omitempty"`
    NativeMetadata  bool `json:"native_metadata,omitempty"`
}
```

| 字段 | 说明 |
| --- | --- |
| `supported` | 是否能描述叶子数据项。 |
| `field_schema` | 是否能获取字段、列或文档字段结构。 |
| `statistics` | 是否能获取行数、大小、采样统计等统计信息。 |
| `indexes` | 是否能获取索引信息。 |
| `constraints` | 是否能获取主键、唯一约束、外键等约束信息。 |
| `spatial_metadata` | 是否能获取空间字段、SRID、范围等空间元数据。 |
| `sampling` | 是否需要或支持通过采样推断结构。 |
| `native_metadata` | 是否能获取引擎原生属性，如对象大小、ETag、修改时间、存储类别等。 |

### 3.4 StoreCapability

```go
type StoreCapability struct {
    StreamRead  bool `json:"stream_read,omitempty"`
    StreamWrite bool `json:"stream_write,omitempty"`
    RangeRead   bool `json:"range_read,omitempty"`
    RangeWrite  bool `json:"range_write,omitempty"`
    BatchRead   bool `json:"batch_read,omitempty"`
    BatchWrite  bool `json:"batch_write,omitempty"`
}
```

| 字段 | 含义 | 必须对应的 Provider |
| --- | --- | --- |
| `stream_read` | 顺序流式读取单个对象、文件或二进制内容。 | `ContentReadableProvider` |
| `stream_write` | 顺序流式创建或覆盖单个对象、文件内容。 | `ContentWritableProvider` |
| `range_read` | 从指定 byte range 读取内容。 | `RangeReadableProvider`，或 `OpenContent()` 明确支持 offset / length |
| `range_write` | 向指定 byte range / offset 写入内容。 | `RangeWritableProvider` |
| `batch_read` | 按批次读取结构化 item，如表、集合、图数据。 | `BatchReadableProvider` |
| `batch_write` | 按批次写入结构化 item。 | `BatchWritableProvider` |

`read` / `write` 总开关无独立调用价值，不进入 Store 能力声明。`atomic_rename`、`transactions`、`formats` 不作为 Store 顶层字段；如有真实调用方，应在对应 Provider 或更具体能力中声明。

对象存储通常声明 `stream_read`、`range_read`，是否声明 `stream_write` 必须取决于是否提供对应写 Provider；对象存储通常不声明 `range_write`。文件系统是否支持 `range_write` 必须按真实能力和 Provider 实现声明。

---

## 四、ComputeCapabilities

```go
type ComputeCapabilities struct {
    Query    *QueryCapability    `json:"query,omitempty"`
    Workflow *WorkflowCapability `json:"workflow,omitempty"`
    Script   *ScriptCapability   `json:"script,omitempty"`
}
```

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
}
```

| 字段 | 说明 |
| --- | --- |
| `supported` | 是否可作为查询运行时。 |
| `languages` | 支持的查询语言，如 `sql`、`mql`、`cypher`。 |
| `default_language` | 默认编辑器语言和样例查询语言。 |
| `result_kinds` | 查询结果形态，如 `table`、`document`、`graph`、`scalar`。 |
| `read_only` | 运行时是否只允许只读查询。 |
| `supports_explain` | 是否支持查询计划 / 性能诊断。 |
| `supports_cancel` | 是否支持取消运行中的查询。 |

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
| `languages` | 支持的语言，如 `python`。 |

---

## 五、TransferCapabilities

```go
type TransferCapabilities struct {
    Read             bool              `json:"read"`
    Write            bool              `json:"write"`
    BulkWrite        bool              `json:"bulk_write,omitempty"`
    StreamRead       bool              `json:"stream_read,omitempty"`
    Checkpoint       bool              `json:"checkpoint,omitempty"`
    ParallelRead     bool              `json:"parallel_read,omitempty"`
    ParallelWrite    bool              `json:"parallel_write,omitempty"`
    ConnectorTypes   map[string]string `json:"connector_types,omitempty"`
    SupportedFormats []string          `json:"supported_formats,omitempty"`
    PreferredWriter  string            `json:"preferred_writer,omitempty"`
}
```

Transfer 的 Reader / Writer 由 Transfer 模块实现。capabilities 只声明是否可读写、推荐 connector 和格式范围。

| 字段 | 说明 | 保留要求 |
| --- | --- | --- |
| `read` | Transfer 是否可把该引擎作为数据来源。 | Transfer 需要消费时必须保留。 |
| `write` | Transfer 是否可把该引擎作为目标端。 | Transfer 需要消费时必须保留。 |
| `bulk_write` | 是否支持高吞吐批量写。 | 可选，有真实 writer 时声明。 |
| `stream_read` | Transfer 读取是否可走流式方式。 | 可选，有真实 reader 时声明。 |
| `checkpoint` | Transfer 任务是否可利用检查点恢复。 | 可选，有执行面支持时声明。 |
| `parallel_read` / `parallel_write` | 是否支持并行读写。 | 可选，有真实执行能力时声明。 |
| `connector_types` | 推荐 Transfer 连接器类型，如 reader=s3、writer=jdbc。 | Transfer 需要消费时建议声明。 |
| `supported_formats` | Transfer 可处理的数据格式范围。当前由 Format Registry 按引擎家族派生。 | Transfer 涉及文件、对象、表或文档格式时阶段性保留。 |
| `preferred_writer` | 多 writer 可选时的默认写端。 | 可选。 |

`supported_formats` 不应在各引擎 builder 中手写维护。当前第一阶段由 `common/format/capability` 集中声明格式、扩展名、数据类型、Transfer / Preview / Parse 能力和适用引擎家族；后续应继续演进为“引擎访问能力 × Format Registry × Transfer / Preview 实现”的完整推导结果。

---

## 六、PreviewCapabilities

```go
type PreviewCapabilities struct {
    Supported     bool     `json:"supported"`
    Modes         []string `json:"modes"`
    MaxRows       int      `json:"max_rows,omitempty"`
    MaxBytes      int64    `json:"max_bytes,omitempty"`
    UsesComposer  bool     `json:"uses_composer,omitempty"`
    DirectPreview bool     `json:"direct_preview,omitempty"`
}
```

| 字段 | 说明 |
| --- | --- |
| `supported` | Manager 是否能预览该引擎的数据内容。 |
| `modes` | 预览方式，如 `tabular_rows`、`object_parse`、`raw_text`。 |
| `max_rows` | 表格、文档、图样本预览的默认行数上限。 |
| `max_bytes` | 对象或文件预览最大读取字节数。 |
| `uses_composer` | 预览是否由 Manager 组合 Store、格式解析、Meta 属性等能力完成。 |
| `direct_preview` | 插件自身是否提供直接预览结果。 |

预览模式：

| 值 | 含义 |
| --- | --- |
| `tabular_rows` | 表格行预览。 |
| `document_samples` | 文档样本预览。 |
| `graph_sample` | 图节点关系预览。 |
| `file_parse` | 文件解析预览。 |
| `object_parse` | 对象解析预览。 |
| `raw_text` | 文本预览。 |
| `binary_metadata` | 二进制文件只预览元数据。 |

---

## 七、能力展示模型

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
- 区分 `available`、`engine_unavailable`、`addp_pending`。
- 返回 `label_key`、`description_key`、`reason_key`、`value_key` 和参数，由前端翻译。
- 将未知 `extensions` 转换为可陈列的 key-value 项。

展示状态：

| 状态 | 含义 |
| --- | --- |
| `available` | 引擎本身具备，ADDP 当前也支持。 |
| `engine_unavailable` | 引擎本身不具备该能力。 |
| `addp_pending` | 引擎本身可以具备，但 ADDP 当前尚未实现。 |

主展示原则：

- 不直接展示布尔字段名，展示业务结论。
- 不展示空分组、空数组和 false 字段，除非该“不支持”会影响用户操作。
- 不把 `schema_version`、`path_version`、`i18n_key`、`supported`、`uses_composer` 放在主信息区。
- `extensions` 只要存在就展示；未知结构按 key-value 陈列。
- 完整能力声明通过“查看 JSON”入口以 key-value 树形方式查看，不在主页面展示原始 JSON 文本。

---

## 八、校验规则

- `schema_version` 必须存在且等于 `engine.capabilities/v1`。
- 声明 `storage.catalog.supported=true` 的插件必须实现 `CatalogProvider`。
- 声明 `storage.metadata.supported=true` 的插件必须实现 `ItemMetadataProvider` 或明确的采样 provider。
- `storage.catalog_model` 是对外 CatalogModel 事实源；如果插件同时实现 `CatalogModelProvider`，其返回值必须与 `storage.catalog_model` 完全一致。
- 声明 `storage.store.stream_read=true` 的插件必须实现 `ContentReadableProvider`。
- 声明 `storage.store.stream_write=true` 的插件必须实现 `ContentWritableProvider`。
- 声明 `storage.store.range_read=true` 的插件必须实现 `RangeReadableProvider`，或在 `ContentReadableProvider.OpenContent()` 中明确支持 offset / length。
- 声明 `storage.store.range_write=true` 的插件必须实现 `RangeWritableProvider`。
- 声明 `storage.store.batch_read=true` 的插件必须实现 `BatchReadableProvider`。
- 声明 `storage.store.batch_write=true` 的插件必须实现 `BatchWritableProvider`。
- 声明 `compute.query.supported=true` 的插件必须实现对应 query runtime provider。
- capabilities 由插件返回结构体，System 统一序列化为 JSONB。
- 旧 capabilities 结构不再兼容，发现旧结构可直接刷新或清空。

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
        {"term": "schema", "kinds": ["schema"], "container": true},
        {"term": "table", "kinds": ["table", "view", "materialized_view"], "container": false, "item": true}
      ]
    },
    "catalog": {"supported": true, "real_time": true, "system_filtering": true},
    "metadata": {"supported": true, "field_schema": true, "statistics": true, "indexes": true, "constraints": true, "spatial_metadata": true},
    "store": {"batch_read": true}
  },
  "compute": {
    "query": {"supported": true, "languages": ["sql"], "default_language": "sql", "result_kinds": ["table", "scalar"]}
  },
  "transfer": {
    "read": true,
    "write": true,
    "bulk_write": true,
    "checkpoint": true,
    "connector_types": {"reader": "jdbc", "writer": "jdbc"},
    "supported_formats": ["table"],
    "preferred_writer": "jdbc"
  },
  "preview": {"supported": true, "modes": ["tabular_rows"], "max_rows": 1000, "uses_composer": true}
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
        {"term": "bucket", "kinds": ["bucket"], "container": true},
        {"term": "prefix", "kinds": ["prefix"], "container": true, "optional": true},
        {"term": "object", "kinds": ["object"], "container": false, "item": true}
      ]
    },
    "catalog": {"supported": true, "real_time": true},
    "metadata": {"supported": true, "native_metadata": true},
    "store": {"stream_read": true, "range_read": true},
    "semantics": ["bucket", "prefix_listing", "object", "stream_read", "range_read"],
    "not_supported": ["range_write", "real_directory"]
  },
  "transfer": {
    "read": true,
    "write": true,
    "connector_types": {"reader": "s3", "writer": "s3"},
    "supported_formats": ["csv", "geojson", "json", "parquet", "shapefile"]
  },
  "preview": {"supported": true, "modes": ["object_parse", "raw_text", "binary_metadata"], "max_bytes": 10485760, "uses_composer": true}
}
```

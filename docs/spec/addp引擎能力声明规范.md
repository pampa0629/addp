# ADDP 引擎能力声明规范

本文只规范 engine plugin 的结构化能力声明。插件接口边界见 [addp引擎插件接口规范.md](addp引擎插件接口规范.md)。

---

## 一、目标

能力声明用于回答“一个引擎实例能被哪些模块以什么方式消费”。上层模块不得只根据 `engine_type` 猜能力。

当前能力声明版本固定为：

```text
engine.capabilities/v1
```

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

字段说明：

| 字段 | 说明 |
| --- | --- |
| `schema_version` | 必须为 `engine.capabilities/v1`。 |
| `engine_type` | 插件类型，如 `postgresql`、`minio`、`neo4j`。 |
| `engine_family` | 主引擎族，如 `tabular`、`document`、`graph`、`object`、`file`、`workflow`、`script`。 |
| `storage` | 存储能力。 |
| `compute` | 查询、工作流、脚本能力。 |
| `transfer` | Transfer 适配能力。 |
| `preview` | Manager 预览能力。 |
| `limits` | 通用限制，如最大预览行数、对象大小、超时建议。 |
| `extensions` | 引擎特有扩展，不得替代核心字段。 |

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

`storage` 是否存在表示引擎是否具备存储能力。存储主类型由顶层 `engine_family` 表达，不再额外声明 `storage.families`；上层功能判断必须读取具体 capability，而不是根据 family 猜能力。

### CatalogModelSpec

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

`path_version` 当前为 `catalog.path/v1`。

推荐层次：

| 引擎 | 层次模型 |
| --- | --- |
| PostgreSQL | `server -> schema -> table/view/materialized_view` |
| MySQL / Doris / ClickHouse | `server -> database -> table/view` |
| MongoDB | `server -> database -> collection` |
| Neo4j | `server -> database -> label/relationship` |
| S3 / MinIO | `service -> bucket -> prefix -> object` |
| NFS | `root -> directory -> file` |

### CatalogCapability

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

### MetadataCapability

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

### StoreCapability

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

字段说明：

| 字段 | 含义 |
| --- | --- |
| `stream_read` | 顺序流式读取单个对象、文件或二进制内容。 |
| `stream_write` | 顺序流式创建或覆盖单个对象、文件内容。 |
| `range_read` | 从指定 byte range 读取内容。 |
| `range_write` | 向指定 byte range / offset 写入内容。 |
| `batch_read` | 按批次读取结构化 item，如表、集合、图数据。 |
| `batch_write` | 按批次写入结构化 item。 |

`read` / `write` 总开关无独立调用价值，不进入核心能力声明；展示层可由细分能力派生。`atomic_rename`、`transactions`、`formats` 不作为 Store 顶层字段：原子重命名、事务、格式支持如有真实调用方，应在对应 Provider 或 Transfer / Query 等更具体能力中声明。

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

### QueryCapability

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

查询语言：

| 值 | 含义 |
| --- | --- |
| `sql` | SQL。 |
| `mql` | MongoDB MQL 或 JSON command。 |
| `cypher` | Neo4j Cypher。 |
| `search_dsl` | 搜索 DSL。 |

结果类型：

| 值 | 含义 |
| --- | --- |
| `table` | 表格结果。 |
| `document` | 文档样本。 |
| `graph` | 图节点和关系。 |
| `scalar` | 单值。 |

### WorkflowCapability

```go
type WorkflowCapability struct {
    Supported             bool     `json:"supported"`
    RuntimeAPI            string   `json:"runtime_api"`
    DynamicOperators      bool     `json:"dynamic_operators"`
    SupportedOperatorMode []string `json:"supported_operator_mode,omitempty"`
}
```

### ScriptCapability

```go
type ScriptCapability struct {
    Supported bool     `json:"supported"`
    Modes     []string `json:"modes"`
    Languages []string `json:"languages,omitempty"`
}
```

Notebook 当前使用 `modes=["notebook"]`。

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

Transfer 的 Reader/Writer 仍由 Transfer 模块实现。capabilities 只声明是否可读写、推荐 connector 和格式范围。

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

## 七、校验规则

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

## 八、示例

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
        {"term": "relation", "kinds": ["table", "view", "materialized_view"], "container": false, "item": true}
      ]
    },
    "catalog": {"supported": true, "real_time": true, "system_filtering": true},
    "metadata": {"supported": true, "field_schema": true, "statistics": true, "indexes": true, "constraints": true, "spatial_metadata": true},
    "store": {"batch_read": true, "batch_write": true}
  },
  "compute": {
    "query": {"supported": true, "languages": ["sql"], "default_language": "sql", "result_kinds": ["table", "scalar"]}
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
  "preview": {"supported": true, "modes": ["object_parse", "raw_text", "binary_metadata"], "max_bytes": 10485760, "uses_composer": true}
}
```

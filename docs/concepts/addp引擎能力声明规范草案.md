# ADDP 引擎能力声明规范草案

本文档定义 ADDP engine plugin 的 `Capabilities()` 结构化能力声明草案。它是 `addp引擎插件接口体系规范草案.md` 的配套文档，用于指导 System、Meta、Manager、Develop、Transfer、Service 等模块判断一个引擎实例是否可被消费。

本文档仍为讨论草案，字段命名和结构可继续调整。

---

## 设计目标

能力声明解决三个问题：

1. **统一发现**  
   上层模块不再根据 `engine_type` 字符串猜能力，而是读取结构化 capabilities。

2. **统一决策**  
   Meta 判断是否可扫描，Manager 判断是否可预览，Develop 判断是否可查询或执行运行时，Transfer 判断是否可读写，都应使用同一份能力声明。

3. **统一演进**  
   新增引擎类型时，优先补能力声明和插件接口实现，而不是在各模块增加硬编码分支。

---

## 总体原则

1. **Capabilities 是插件承诺，不是模块配置**  
   capabilities 描述引擎“能做什么”，不保存某个任务或页面的运行参数。

2. **Capabilities 应由插件返回结构体，System 统一序列化**  
   插件不应返回 JSON 字符串。Go 代码中应是强类型结构，持久化时由 System 序列化为 JSONB。

3. **稳定字段进入 schema，专业差异进入 extensions**  
   平台核心字段需要规范化。引擎特有能力可放入 `extensions`，但不应替代核心字段。

4. **能力声明和接口实现要一致**  
   声明 `storage.catalog=true` 的插件，应实现 `CatalogProvider` 或有明确适配层。

5. **能力必须有版本**  
   capabilities 结构需要 `schema_version`，避免未来字段演进时无法兼容。

---

## 顶层结构

建议 Go 结构：

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

示例：

```json
{
  "schema_version": "engine.capabilities/v1",
  "engine_type": "postgresql",
  "engine_family": "tabular",
  "storage": {},
  "compute": {},
  "transfer": {},
  "preview": {},
  "limits": {},
  "extensions": {}
}
```

字段说明：

| 字段 | 说明 |
| --- | --- |
| `schema_version` | 能力声明 schema 版本，建议从 `engine.capabilities/v1` 开始。 |
| `engine_type` | 插件类型，例如 `postgresql`、`minio`、`neo4j`。 |
| `engine_family` | 主要引擎族，例如 `tabular`、`document`、`graph`、`object`、`file`、`workflow`、`script`。 |
| `storage` | 存储能力声明。 |
| `compute` | 计算能力声明。 |
| `transfer` | Transfer 适配能力声明。 |
| `preview` | Manager 预览能力声明。 |
| `limits` | 通用限制，例如最大预览行数、最大对象大小、超时建议。 |
| `extensions` | 引擎特有扩展字段。 |

---

## StorageCapabilities

建议结构：

```go
type StorageCapabilities struct {
    Families     []string              `json:"families"`
    CatalogModel *CatalogModelSpec     `json:"catalog_model,omitempty"`
    Catalog      *CatalogCapability    `json:"catalog,omitempty"`
    Metadata     *MetadataCapability   `json:"metadata,omitempty"`
    Store        *StoreCapability      `json:"store,omitempty"`
    Semantics    []string              `json:"semantics,omitempty"`
    NotSupported []string              `json:"not_supported,omitempty"`
}
```

### families

`families` 描述存储族，可多选：

| 值 | 含义 |
| --- | --- |
| `tabular` | 表格型存储，如 PostgreSQL、MySQL、Doris、ClickHouse、Hive/Spark Catalog。 |
| `document` | 文档型存储，如 MongoDB。 |
| `graph` | 图存储，如 Neo4j。 |
| `object` | 对象存储，如 S3、MinIO、OSS。 |
| `file` | 文件系统语义存储，如 NFS、HDFS、本地文件系统。 |
| `vector` | 向量存储，预留。 |
| `search` | 搜索索引存储，预留。 |

### CatalogModelSpec

Catalog Model 描述层次术语，不访问真实数据。

```go
type CatalogModelSpec struct {
    PathVersion string             `json:"path_version"`
    RootTerm    string             `json:"root_term"`
    Levels      []CatalogLevelSpec `json:"levels"`
}

type CatalogLevelSpec struct {
    Term        string   `json:"term"`
    Kinds       []string `json:"kinds"`
    Container   bool     `json:"container"`
    Item        bool     `json:"item"`
    Optional    bool     `json:"optional,omitempty"`
    I18nKey     string   `json:"i18n_key,omitempty"`
}
```

示例：

```json
{
  "path_version": "catalog.path/v1",
  "root_term": "server",
  "levels": [
    {
      "term": "schema",
      "kinds": ["namespace"],
      "container": true
    },
    {
      "term": "table",
      "kinds": ["table", "view", "materialized_view"],
      "container": false,
      "item": true
    }
  ]
}
```

术语建议：

| 引擎族 | 层次模型 |
| --- | --- |
| PostgreSQL | `server -> schema -> table/view/materialized_view` |
| MySQL / Doris / ClickHouse | `server -> database -> table/view` |
| MongoDB | `server -> database -> collection` |
| Neo4j | `server -> database -> label/relationship` |
| S3 / MinIO | `service -> bucket -> prefix -> object` |
| NFS / HDFS | `root -> directory -> file` |

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

含义：

- `supported=true` 表示可以获取真实 node/item。
- `real_time=true` 表示每次调用可实时访问引擎。
- `system_filtering=true` 表示插件可过滤系统库、系统 schema 或系统目录。

### MetadataCapability

```go
type MetadataCapability struct {
    Supported       bool     `json:"supported"`
    FieldSchema     bool     `json:"field_schema,omitempty"`
    Statistics      bool     `json:"statistics,omitempty"`
    Indexes         bool     `json:"indexes,omitempty"`
    Constraints     bool     `json:"constraints,omitempty"`
    SpatialMetadata bool     `json:"spatial_metadata,omitempty"`
    Sampling        bool     `json:"sampling,omitempty"`
    NativeMetadata  bool     `json:"native_metadata,omitempty"`
}
```

### StoreCapability

```go
type StoreCapability struct {
    Read          bool     `json:"read"`
    Write         bool     `json:"write"`
    BatchRead     bool     `json:"batch_read,omitempty"`
    BatchWrite    bool     `json:"batch_write,omitempty"`
    StreamRead    bool     `json:"stream_read,omitempty"`
    StreamWrite   bool     `json:"stream_write,omitempty"`
    RangeRead     bool     `json:"range_read,omitempty"`
    RandomWrite   bool     `json:"random_write,omitempty"`
    AtomicRename  bool     `json:"atomic_rename,omitempty"`
    Transactions  bool     `json:"transactions,omitempty"`
    Formats       []string `json:"formats,omitempty"`
}
```

说明：

- 表格型、文档型、图存储通常声明 `batch_read` / `batch_write`。
- 对象存储通常声明 `stream_read`、`stream_write`、`range_read`，但不声明 `random_write` 和 `atomic_rename`。
- 文件系统可根据真实能力声明 `random_write`、`atomic_rename`。

---

## ComputeCapabilities

建议结构：

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
    Supported       bool            `json:"supported"`
    Languages       []QueryLanguage `json:"languages"`
    DefaultLanguage string          `json:"default_language,omitempty"`
    ResultKinds     []string        `json:"result_kinds,omitempty"`
    ReadOnly        bool            `json:"read_only,omitempty"`
    SupportsExplain bool            `json:"supports_explain,omitempty"`
    SupportsCancel  bool            `json:"supports_cancel,omitempty"`
}
```

查询语言建议：

| 值 | 含义 |
| --- | --- |
| `sql` | SQL。 |
| `mql` | MongoDB MQL 或 JSON command。 |
| `cypher` | Neo4j Cypher。 |
| `search_dsl` | Elasticsearch/OpenSearch 查询 DSL。 |

结果类型建议：

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

原则：

- `dynamic_operators=true` 表示算子列表必须从运行时动态获取。
- 工作流能力以 ADDP 工作流计算引擎接口规范为边界。

### ScriptCapability

```go
type ScriptCapability struct {
    Supported bool     `json:"supported"`
    Modes     []string `json:"modes"`
    Languages []string `json:"languages,omitempty"`
}
```

当前建议：

- `modes=["notebook"]`
- `languages=["python"]`

---

## TransferCapabilities

Transfer 能力用于说明该引擎是否可作为 Transfer 的 source 或 target，以及应映射到什么 connector。

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

示例：

```json
{
  "read": true,
  "write": true,
  "bulk_write": true,
  "checkpoint": true,
  "connector_types": {
    "reader": "jdbc",
    "writer": "postgres_copy"
  },
  "supported_formats": ["table"],
  "preferred_writer": "postgres_copy"
}
```

说明：

- Transfer 的 Reader/Writer 实现仍保留在 Transfer 模块。
- capabilities 只声明可用能力和推荐连接器。
- 具体任务配置由 `TransferAdapter.BuildReaderConfig` / `BuildWriterConfig` 生成。

---

## PreviewCapabilities

Preview 能力用于 Manager 判断 item 是否可预览，以及使用哪类预览策略。

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

预览模式建议：

| 值 | 含义 |
| --- | --- |
| `tabular_rows` | 表格行预览。 |
| `document_samples` | 文档样本预览。 |
| `graph_sample` | 图节点关系预览。 |
| `file_parse` | 文件解析预览。 |
| `object_parse` | 对象内容解析预览。 |
| `raw_text` | 文本内容预览。 |
| `binary_metadata` | 二进制文件只预览元数据。 |

原则：

- Manager 默认走 `PreviewProvider` 或 Preview Composer。
- Preview Composer 可以组合 `CatalogProvider`、`StoreProvider`、`QueryRuntimeProvider` 和 `common/format`。
- 不建议 Manager 直接调用 Transfer Reader/Writer 做预览。

---

## CatalogNode 规范建议

Catalog node 应使用结构化 path，并保留扩展空间。

```go
type CatalogPath struct {
    Version  string            `json:"version"`
    EngineID uint              `json:"engine_id"`
    Segments []CatalogSegment  `json:"segments"`
}

type CatalogSegment struct {
    Term  string `json:"term"`
    Kind  string `json:"kind"`
    Name  string `json:"name"`
}
```

`Kind` 建议维护核心枚举，并允许插件扩展：

| Kind | 含义 |
| --- | --- |
| `root` | 根节点。 |
| `namespace` | 命名空间，对应 schema/database 等。 |
| `table` | 表。 |
| `view` | 视图。 |
| `materialized_view` | 物化视图。 |
| `external_table` | 外部表。 |
| `collection` | 文档集合。 |
| `label` | 图节点标签。 |
| `relationship` | 图关系类型。 |
| `bucket` | 对象存储 bucket。 |
| `prefix` | 对象存储 prefix。 |
| `object` | 对象。 |
| `directory` | 目录。 |
| `file` | 文件。 |

`Term` 是展示术语，可由插件提供，例如 PostgreSQL 使用 `schema`，MySQL 使用 `database`。

---

## 示例

### PostgreSQL

```json
{
  "schema_version": "engine.capabilities/v1",
  "engine_type": "postgresql",
  "engine_family": "tabular",
  "storage": {
    "families": ["tabular"],
    "catalog_model": {
      "path_version": "catalog.path/v1",
      "root_term": "server",
      "levels": [
        {"term": "schema", "kinds": ["namespace"], "container": true},
        {"term": "table", "kinds": ["table", "view", "materialized_view"], "container": false, "item": true}
      ]
    },
    "catalog": {"supported": true, "real_time": true, "system_filtering": true},
    "metadata": {
      "supported": true,
      "field_schema": true,
      "statistics": true,
      "indexes": true,
      "constraints": true,
      "spatial_metadata": true
    },
    "store": {
      "read": true,
      "write": true,
      "batch_read": true,
      "batch_write": true,
      "transactions": true,
      "formats": ["table"]
    }
  },
  "compute": {
    "query": {
      "supported": true,
      "languages": ["sql"],
      "default_language": "sql",
      "result_kinds": ["table", "scalar"],
      "supports_explain": true,
      "supports_cancel": true
    }
  },
  "transfer": {
    "read": true,
    "write": true,
    "bulk_write": true,
    "checkpoint": true,
    "connector_types": {
      "reader": "jdbc",
      "writer": "postgres_copy"
    },
    "supported_formats": ["table"],
    "preferred_writer": "postgres_copy"
  },
  "preview": {
    "supported": true,
    "modes": ["tabular_rows"],
    "max_rows": 1000,
    "uses_composer": true
  }
}
```

### MinIO

```json
{
  "schema_version": "engine.capabilities/v1",
  "engine_type": "minio",
  "engine_family": "object",
  "storage": {
    "families": ["object"],
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
    "store": {
      "read": true,
      "write": true,
      "stream_read": true,
      "stream_write": true,
      "range_read": true,
      "random_write": false,
      "atomic_rename": false,
      "formats": ["csv", "geojson", "json", "parquet", "shapefile"]
    },
    "semantics": ["bucket", "prefix_listing", "object", "stream_read", "range_read"],
    "not_supported": ["posix_random_write", "atomic_rename", "real_directory"]
  },
  "transfer": {
    "read": true,
    "write": true,
    "connector_types": {
      "reader": "s3",
      "writer": "s3"
    },
    "supported_formats": ["csv", "geojson", "json", "parquet", "shapefile"]
  },
  "preview": {
    "supported": true,
    "modes": ["object_parse", "raw_text", "binary_metadata"],
    "max_bytes": 10485760,
    "uses_composer": true
  }
}
```

### Neo4j

```json
{
  "schema_version": "engine.capabilities/v1",
  "engine_type": "neo4j",
  "engine_family": "graph",
  "storage": {
    "families": ["graph"],
    "catalog_model": {
      "path_version": "catalog.path/v1",
      "root_term": "server",
      "levels": [
        {"term": "database", "kinds": ["namespace"], "container": true},
        {"term": "label", "kinds": ["label", "relationship"], "container": false, "item": true}
      ]
    },
    "catalog": {"supported": true, "real_time": true, "system_filtering": true},
    "metadata": {"supported": true, "field_schema": true, "statistics": true, "native_metadata": true},
    "store": {"read": true, "write": true, "batch_read": true, "batch_write": true}
  },
  "compute": {
    "query": {
      "supported": true,
      "languages": ["cypher"],
      "default_language": "cypher",
      "result_kinds": ["table", "graph"],
      "supports_explain": true
    }
  },
  "preview": {
    "supported": true,
    "modes": ["graph_sample", "tabular_rows"],
    "max_rows": 1000,
    "uses_composer": true
  }
}
```

---

## 落地建议

1. **先定义 Go 结构体**  
   在 `common/engine/plugin` 或后续公共包中定义 `EngineCapabilities` 及子结构。

2. **System 保存 JSONB**  
   System 仍可持久化 JSONB，但插件返回值应是结构体。

3. **先实现核心引擎样例**  
   从 PostgreSQL、MinIO、Neo4j 三类差异最大的引擎开始校验字段是否够用。

4. **上层模块逐步切换判断逻辑**  
   Meta、Manager、Develop、Transfer 逐步从 `engine_type` 分支迁移到 capabilities 判断。

5. **再补 JSON Schema 文件**  
   Go 结构稳定后，可以生成或手写 JSON Schema，用于文档、校验和前端消费。

---

**文档状态**：讨论草案  
**创建日期**：2026-05-02  
**适用范围**：ADDP engine capabilities 结构化声明设计

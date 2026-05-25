# ADDP 引擎能力声明规范

本文规范 engine plugin 的结构化能力声明。插件接口边界见 [addp引擎插件接口规范.md](addp引擎插件接口规范.md)，Catalog 路径语义见 [addp存储引擎路径体系规范.md](addp存储引擎路径体系规范.md)。

能力声明用于回答“一个引擎实例自身具备哪些可由 ADDP 统一消费的能力”。上层模块不得只根据 `engine_type` 或 `engine_family` 猜能力。

当前版本固定为：

```text
engine.capabilities/v1
```

---

## 一、基本原则

- capabilities 是引擎自身能力与 Provider 实现承诺的事实来源。
- 声明了可调用能力，就必须有对应 Provider 或明确的模块执行面。
- Catalog、Metadata、Store、Query、Workflow、Script 是不同能力面，不能混用。
- 核心结构只表达引擎自身原生能力与对应 Provider 能力，不承载模块适配状态。
- `compute.query`、`compute.workflow`、`compute.script` 是计算能力事实源，取代旧版 `dev_modes` 字符串数组；开发界面可以由这些能力派生，但不得再把 `dev_modes` 作为能力声明事实源。
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
| `limits` | 跨能力通用限制，如预览大小、超时建议。 | 可选，有真实调用方时声明。 |
| `extensions` | 引擎特有扩展。 | 可选，不得替代核心字段。 |

`engine_family` 只表达粗粒度引擎族，不能替代 `storage.catalog_model`、provider 组合或模块自身策略。尤其对 Meta 而言，是否走 namespace/item catalog、是否需要内容读取、是否可做文档采样，必须由 `CatalogModelSpec` 与已实现 provider 一起决定；不得把 `engine_family` 当作扫描策略事实源。

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
| `levels.i18n_key` | 展示层使用的原生术语国际化 key。内置模型必须声明，推荐格式为 `engine.term.<term>`。 |

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

`levels.term` 是机器语义，`levels.i18n_key` 是展示语义。Meta、Manager 等上层模块可以统一消费 `CatalogNode` / `CatalogPath`，但面向用户的 UI 必须优先使用 `i18n_key` 展示引擎原生术语，例如 PostgreSQL 显示 `Schema`，MySQL/MongoDB 显示 `数据库 / Database`，MinIO/S3 显示 `Bucket`，NFS 显示 `目录 / Directory`。不得把平台内部的 `catalog node` 作为用户可见术语。

消费规则：

- `CatalogModelSpec` 负责回答“目录怎么分层、各层叫什么、谁是 item”。
- provider 组合负责回答“哪些动作真的可做”，例如是否能列目录、描述 item、采样字段、读取内容。
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
| `system_filtering` | 是否能过滤系统库、系统 schema 等噪声。 | 数据库类引擎建议声明。 |
| `node_kinds` | catalog 可能返回的节点类型集合。 | 建议声明。 |

### 3.3 MetadataCapability

```go
type MetadataCapability struct {
    Supported       bool `json:"supported"`
    FieldInfo       bool `json:"field_info,omitempty"`
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
| `field_info` | 是否能获取字段、列或文档字段信息。 |
| `statistics` | 是否能获取行数、大小、采样统计等统计信息。 |
| `indexes` | 是否能获取索引信息。 |
| `constraints` | 是否能获取主键、唯一约束、外键等约束信息。 |
| `spatial_metadata` | 是否能获取空间字段、SRID、范围等空间元数据。 |
| `sampling` | 是否需要或支持通过采样推断结构。 |
| `native_metadata` | 是否能获取引擎原生属性，如对象大小、ETag、修改时间、存储类别等。 |

### 3.4 StoreCapability

```go
type StoreCapability struct {
    StreamRead        bool `json:"stream_read,omitempty"`
    StreamWrite       bool `json:"stream_write,omitempty"`
    RangeRead         bool `json:"range_read,omitempty"`
    RangeWrite        bool `json:"range_write,omitempty"`
    BatchRead         bool `json:"batch_read,omitempty"`
    TableReadSession  bool `json:"table_read_session,omitempty"`
    BatchWrite        bool `json:"batch_write,omitempty"`
    TableWriteSession bool `json:"table_write_session,omitempty"`
    TableWritePrepare bool `json:"table_write_prepare,omitempty"`
}
```

| 字段 | 含义 | 必须对应的 Provider |
| --- | --- | --- |
| `stream_read` | 顺序流式读取单个对象、文件或二进制内容。 | `ContentReadableProvider` |
| `stream_write` | 顺序流式创建或覆盖单个对象、文件内容。 | `ContentWritableProvider` |
| `range_read` | 从指定 byte range 读取内容。 | `RangeReadableProvider`，或 `OpenContent()` 明确支持 offset / length |
| `range_write` | 向指定 byte range / offset 写入内容。 | `RangeWritableProvider` |
| `batch_read` | 按批次读取结构化 item，如表、集合、图数据。 | `BatchReadableProvider` |
| `table_read_session` | 打开一次表读取会话并连续读取批次，避免大表 `LIMIT/OFFSET` 翻页退化。 | `TableReadSessionProvider` |
| `batch_write` | 按批次写入结构化 item。 | `BatchWritableProvider` |
| `table_write_session` | 打开一次表写入会话并连续写入批次，避免每批重复建立 COPY / bulk load 会话。 | `TableWriteSessionProvider` |
| `table_write_prepare` | 执行表级写入前准备动作，如 `truncate_insert` 的清表。该能力不写入数据行。 | `TableWritePreparer` |

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

`compute` 表达引擎可被 ADDP 统一调用的计算运行时能力，而不是 UI 开发模式标签。旧版 `dev_modes` 只能回答“应该出现在哪个开发界面”，不能表达查询语言、运行协议、动态算子、脚本模式和对应 Provider，因此不再进入 `engine.capabilities/v1`。

Develop 等上层模块如仍需“开发模式”概念，应从 `compute` 能力派生：

| 派生开发入口 | 能力事实源 |
| --- | --- |
| 查询工作台 | `compute.query.supported=true` |
| 工作流编辑器 | `compute.workflow.supported=true` |
| Notebook / 脚本编辑器 | `compute.script.supported=true`，并结合 `compute.script.modes` |

这些派生名称可作为兼容 API、前端路由或展示文案使用，但不得反向写回为 `dev_modes` 字段。

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

工作流引擎的静态能力声明只回答“是否具备统一工作流运行时，以及使用哪个 runtime API”。算子列表、算子参数、分类、输入输出端口等动态能力，不写入 `capabilities`，必须通过 `WorkflowRuntimeProvider.ListOperators()` 获取；当前 `addp.workflow/v1` 对应的标准 HTTP 入口为 `GET /api/operators`。工作流执行通过 `WorkflowRuntimeProvider.ExecuteWorkflow()`，对应标准 HTTP 入口为 `POST /api/workflow`。执行期绑定的外部运行时资源（例如 `spark_workflow` 绑定某个 Spark 资源 ID）属于执行请求参数，不属于能力声明。

`dynamic_operators=true` 表示调用方可以通过 Provider 动态发现算子。它不是“已有算子列表”的缓存，也不是某个模块对该引擎的适配状态。

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
- 声明 `storage.store.table_read_session=true` 的插件必须实现 `TableReadSessionProvider`。
- 声明 `storage.store.batch_write=true` 的插件必须实现 `BatchWritableProvider`。
- 声明 `storage.store.table_write_session=true` 的插件必须实现 `TableWriteSessionProvider`。
- 声明 `storage.store.table_write_prepare=true` 的插件必须实现 `TableWritePreparer`。
- 声明 `compute.query.supported=true` 的插件必须实现对应 query runtime provider。
- 声明 `compute.workflow.supported=true` 的插件必须实现 `WorkflowRuntimeProvider`。若 `dynamic_operators=true`，则其 `ListOperators()` 必须可调用，并返回符合工作流计算引擎接口规范的算子元数据。
- 声明 `compute.script.supported=true` 的插件必须实现 `ScriptRuntimeProvider`。
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
    "metadata": {"supported": true, "field_info": true, "statistics": true, "indexes": true, "constraints": true, "spatial_metadata": true},
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
  }
}
```

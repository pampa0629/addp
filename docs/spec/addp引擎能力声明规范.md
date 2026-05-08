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

### 顶层字段的价值和保留建议

| 字段 | 价值 / 作用 | 是否必须保留 | 展示建议 |
| --- | --- | --- | --- |
| `schema_version` | 标识能力声明结构版本，保证 System、Manager、Meta、Transfer 等模块能按同一结构解析。后续能力声明升级时，用它区分新旧结构。 | 必须保留。没有它无法安全判断 JSON 结构。 | 不作为重点信息展示，可放在“技术信息”折叠区。 |
| `engine_type` | 标识插件类型，是连接配置、插件注册和能力校验的稳定机器名。 | 必须保留。 | 展示为“引擎类型”，与引擎名称、显示名一起放在概览区。 |
| `engine_family` | 给出引擎主族，用于快速理解引擎定位，如对象存储、表格型、图数据库、工作流运行时。功能判断不能只依赖它。 | 必须保留，但只作为粗分类。 | 展示为“引擎族”，配合中文说明，不展示裸英文枚举。 |
| `storage` | 表达该引擎是否可作为数据存储被浏览、扫描、描述、读取或写入。 | 具备存储能力的引擎必须保留；纯计算引擎可为空。 | 以“存储能力”卡片展示。 |
| `compute` | 表达该引擎是否可执行查询、工作流或脚本。 | 具备计算能力的引擎必须保留；纯存储引擎可为空。 | 以“计算能力”卡片展示；无能力时不显示空表。 |
| `transfer` | 表达 Transfer 模块是否能把该引擎作为读端或写端，以及推荐连接器和格式范围。 | 只有 Transfer 需要消费时必须保留。 | 展示为“传输适配”，重点显示可读、可写、连接器和格式。 |
| `preview` | 表达 Manager 是否能对该引擎的数据项做内容预览，以及预览模式和限制。 | 需要页面预览时必须保留。 | 展示为“预览能力”，用自然语言说明能预览什么。 |
| `limits` | 放置跨能力的通用限制，避免把限制散落到模块私有配置里。 | 可选。只有存在真实调用方时保留。 | 只展示对用户有决策意义的限制。 |
| `extensions` | 承载引擎特有扩展，避免污染核心结构。 | 可选。不得替代核心字段。 | 只要存在就展示为“扩展能力”区块；未知结构按 key-value 陈列。 |

页面展示应区分“用户需要理解的能力”和“系统需要解析的字段”。能力详情页不应把所有 JSON key 平铺成表格；对 `schema_version`、`path_version`、`i18n_key`、`supported` 等技术字段，主展示区不直接展示。技术人员需要查看完整信息时，通过“查看 JSON”入口进入 key-value 树形视图。

### 能力边界和不支持分类

能力声明需要区分两个层面：

| 层面 | 回答的问题 | 例子 |
| --- | --- | --- |
| 引擎本身能力 | 这个能力是否符合该引擎的本质模型。 | MinIO / S3 是对象存储，本身不提供 query runtime。 |
| ADDP 当前支持 | 对于引擎本身可以具备的能力，ADDP 当前是否已经实现对应 Provider、Adapter 或模块执行面。 | 某些引擎理论上可以参与 Transfer，但 ADDP 当前尚未实现 reader / writer。 |

当前 `engine.capabilities/v1` 的核心结构仍以“ADDP 当前可用能力”为主：

- 能力分组存在且 `supported=true`，表示 ADDP 当前可用。
- 能力分组不存在，表示它不属于当前可用能力，但单靠缺失不能区分“引擎本身没有”还是“ADDP 尚未实现”。
- `storage.not_supported` 只用于表达存储语义中容易误判的明确不支持项，例如对象存储没有真实目录、通常不支持范围写入。

后续展示模型必须补充派生的能力边界状态，用于前端展示和后续路线判断：

| 状态 | 含义 | 展示建议 |
| --- | --- | --- |
| `available` | 引擎本身具备，ADDP 当前也支持。 | 正常展示为可用能力。 |
| `engine_unavailable` | 引擎本身不具备该能力。 | 展示为“该引擎不具备”，例如“对象存储不提供查询运行时”。 |
| `addp_pending` | 引擎本身可以具备，但 ADDP 当前尚未实现。 | 展示为“ADDP 暂未支持”，可作为后续建设线索。 |

该状态不要求直接进入 `EngineCapabilities` 核心结构，可由 System 后端基于能力声明、引擎族、插件注册信息和后端展示定义生成 `capabilities_view`。

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

### 存储能力字段的价值和保留建议

| 字段 | 价值 / 作用 | 是否必须保留 | 展示建议 |
| --- | --- | --- | --- |
| `catalog_model` | 描述目录层级的稳定模型，例如 `bucket -> prefix -> object` 或 `schema -> table`。它决定页面树如何组织、路径如何解析、扫描任务如何选择范围。 | 具备 catalog 的存储引擎必须保留。 | 用路径链路展示，不用原始表格堆字段。 |
| `catalog` | 表达是否能实时列出引擎里的目录和数据项。它回答“真实引擎当前有什么”。 | 需要浏览或扫描真实目录时必须保留。 | 显示为“实时目录：支持 / 不支持”，附带搜索、过滤等能力标签。 |
| `metadata` | 表达是否能描述叶子 item 的结构、统计、索引、约束、空间信息或原生属性。它回答“这个数据项是什么样”。 | Meta 扫描和资产描述需要时必须保留。 | 显示为“元数据：字段、统计、索引、空间、原生属性”等标签。 |
| `store` | 表达内容访问方式，如流式读、范围读、批量读写。它回答“数据内容能如何被读写”。 | 需要预览、导入导出或内容访问时必须保留。 | 归并成“读取方式”和“写入方式”，避免逐个布尔字段平铺。 |
| `semantics` | 用稳定机器词补充引擎语义，如对象存储的 `bucket`、`prefix_listing`、`object`。它帮助上层避免把不同引擎误建模为同一种目录。 | 可选。只在核心字段不足以表达差异、且有真实调用方时保留。 | 面向用户时翻译成“桶 / 前缀 / 对象”等自然语言；开发视图可显示机器词。 |
| `not_supported` | 显式声明容易误解但不支持的能力，例如对象存储不支持 `real_directory`、通常不支持 `range_write`。它用于防止页面或模块做错误推断。 | 可选，但对对象存储、文件系统等容易混淆的引擎建议保留。 | 展示为“明确不支持”，只显示会影响用户操作的项。 |

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

Catalog Model 字段说明：

| 字段 | 价值 / 作用 | 是否必须保留 | 展示建议 |
| --- | --- | --- | --- |
| `path_version` | 标识目录路径模型版本，保证路径解析、指纹计算和跨模块传参一致。 | 必须保留。 | 默认折叠到技术信息。 |
| `root_term` | 表达根语义，如 `server`、`service`、`root`。它帮助区分数据库服务、对象存储服务和文件系统根。 | 必须保留。 | 可展示为路径链路起点。 |
| `levels.term` | 某一层目录的语义名称，如 `bucket`、`prefix`、`object`。 | 必须保留。 | 展示为中文层级名称。 |
| `levels.kinds` | 该层可能出现的节点类型，如 `table/view`、`label/relationship`。 | 必须保留。 | 在层级节点旁显示为小标签。 |
| `levels.container` | 表示该层是否可以继续展开子节点。 | 必须保留。 | 用树形图标或“可展开”状态表达，不直接展示布尔值。 |
| `levels.item` | 表示该层是否是可被描述、预览、读取或写入的数据项。 | 必须保留。 | 用“数据项”标签表达。 |
| `levels.optional` | 表示该层可省略，例如对象存储中 object 可以直接位于 bucket 下，也可以在 prefix 下。 | 必须保留。 | 在路径链路上用“可选”弱标签表达。 |
| `levels.i18n_key` | 指向显示文案的国际化 key。 | 可选。仅当前端需要稳定翻译 key 时保留。 | 不在能力详情主区展示。 |

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

字段说明：

| 字段 | 价值 / 作用 | 是否必须保留 | 展示建议 |
| --- | --- | --- | --- |
| `supported` | 表示插件是否能提供统一 catalog 浏览。 | 必须保留。 | 不直接展示字段名；由后端展示模型派生“目录浏览”是否可用。 |
| `real_time` | 表示目录来自真实引擎实时查询，而不是平台已扫描快照。 | 必须保留。 | 显示为“实时目录”。 |
| `supports_search` | 表示 catalog 浏览是否支持搜索。 | 可选。有 provider 和调用方时保留。 | 显示为“可搜索”。 |
| `supports_filter` | 表示 catalog 浏览是否支持过滤。 | 可选。有 provider 和调用方时保留。 | 显示为“可过滤”。 |
| `system_filtering` | 表示插件能过滤系统库、系统 schema 等噪声。 | 建议保留。对数据库类引擎有价值。 | 显示为“隐藏系统对象”。 |
| `node_kinds` | 当前 catalog 可能返回的节点类型集合。 | 建议保留。它能帮助前端选择图标、筛选项和合法操作。 | 展示为节点类型标签。 |

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

字段说明：

| 字段 | 价值 / 作用 | 是否必须保留 | 展示建议 |
| --- | --- | --- | --- |
| `supported` | 表示是否能描述叶子数据项。 | 需要 Meta 扫描时必须保留。 | 不直接展示字段名；由后端展示模型派生“元数据采集”是否可用。 |
| `field_schema` | 表示能获取字段、列或文档字段结构。 | 对表格、文档类引擎建议保留。 | 显示为“字段结构”。 |
| `statistics` | 表示能获取行数、大小、采样统计等统计信息。 | 可选。统计信息成本差异较大，应按真实实现声明。 | 显示为“统计信息”。 |
| `indexes` | 表示能获取索引信息。 | 对数据库类引擎建议保留。 | 显示为“索引”。 |
| `constraints` | 表示能获取主键、唯一约束、外键等约束信息。 | 对关系型数据库建议保留。 | 显示为“约束”。 |
| `spatial_metadata` | 表示能获取空间字段、SRID、范围等空间元数据。 | 只有真实支持空间元数据时保留。 | 显示为“空间元数据”。 |
| `sampling` | 表示需要或支持通过采样推断结构，典型用于文档型数据。 | 文档型、半结构化数据建议保留。 | 显示为“采样推断”。 |
| `native_metadata` | 表示能获取引擎原生属性，如对象大小、ETag、修改时间、存储类别等。 | 对对象、文件、图等非标准字段结构引擎建议保留。 | 显示为“原生属性”。 |

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

保留建议：

| 字段 | 是否必须保留 | 展示建议 |
| --- | --- | --- |
| `stream_read` / `stream_write` | 内容流读写的核心能力，必须保留。 | 展示为“流式读取 / 流式写入”。 |
| `range_read` / `range_write` | 大对象、文件分片、断点读取等场景需要，必须保留。 | 展示为“范围读取 / 范围写入”；对象存储的“不支持范围写入”可放到明确不支持中。 |
| `batch_read` / `batch_write` | 表、集合、图等结构化数据搬运和预览需要，必须保留。 | 展示为“批量读取 / 批量写入”。 |

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

Query 字段说明：

| 字段 | 价值 / 作用 | 是否必须保留 | 展示建议 |
| --- | --- | --- | --- |
| `supported` | 表示是否可作为查询运行时。 | 查询引擎必须保留。 | 不直接展示字段名；由后端展示模型派生“查询”是否可用。 |
| `languages` | 表示支持的查询语言，如 SQL、MQL、Cypher。 | 查询引擎必须保留。 | 显示为语言标签。 |
| `default_language` | 表示默认编辑器语言和样例查询语言。 | 多语言或需要编辑器默认值时必须保留。 | 显示为“默认语言”。 |
| `result_kinds` | 表示查询结果形态，如表格、文档、图、单值。 | 建议保留。它影响预览组件和结果渲染方式。 | 显示为结果类型标签。 |
| `read_only` | 表示运行时是否只允许只读查询。 | 有安全控制需要时必须保留。 | 显示为“只读”。 |
| `supports_explain` | 表示是否支持查询计划。查询计划通常对应数据库 `EXPLAIN` 能力，用于查看是否走索引、是否全表扫描、JOIN 顺序和估算成本等性能诊断信息。 | 可选。Develop 查询体验需要时保留。 | 面向普通用户可展示为“性能诊断”或“查询计划”，不要只写“执行计划”。 |
| `supports_cancel` | 表示是否支持取消运行中的查询。 | 可选。长查询场景建议保留。 | 显示为“可取消”。 |

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

Workflow 字段说明：

| 字段 | 价值 / 作用 | 是否必须保留 | 展示建议 |
| --- | --- | --- | --- |
| `supported` | 表示是否可作为工作流运行时。 | 工作流引擎必须保留。 | 不直接展示字段名；由后端展示模型派生“工作流运行时”是否可用。 |
| `runtime_api` | 表示工作流运行时接口版本或协议。 | 工作流引擎必须保留。 | 默认折叠到技术信息，普通用户不需要优先看到。 |
| `dynamic_operators` | 表示算子是否可动态发现。 | 工作流引擎建议保留。 | 显示为“动态算子”。 |
| `supported_operator_mode` | 表示支持的算子运行模式。 | 可选。有多模式调度时保留。 | 显示为运行模式标签。 |

### ScriptCapability

```go
type ScriptCapability struct {
    Supported bool     `json:"supported"`
    Modes     []string `json:"modes"`
    Languages []string `json:"languages,omitempty"`
}
```

Notebook 当前使用 `modes=["notebook"]`。

Script 字段说明：

| 字段 | 价值 / 作用 | 是否必须保留 | 展示建议 |
| --- | --- | --- | --- |
| `supported` | 表示是否可作为脚本或 Notebook 运行时。 | 脚本引擎必须保留。 | 不直接展示字段名；由后端展示模型派生“脚本 / Notebook”是否可用。 |
| `modes` | 表示交互模式，如 notebook。 | 脚本引擎必须保留。 | 显示为模式标签。 |
| `languages` | 表示支持的语言，如 Python。 | 建议保留。 | 显示为语言标签。 |

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

字段说明：

| 字段 | 价值 / 作用 | 是否必须保留 | 展示建议 |
| --- | --- | --- | --- |
| `read` | 表示 Transfer 是否可把该引擎作为数据来源。 | Transfer 需要消费时必须保留。 | 显示为“可作为来源”。 |
| `write` | 表示 Transfer 是否可把该引擎作为目标端。 | Transfer 需要消费时必须保留。 | 显示为“可作为目标”。 |
| `bulk_write` | 表示是否支持高吞吐批量写。 | 可选。有真实 writer 时保留。 | 显示为“批量写入”。 |
| `stream_read` | 表示 Transfer 读取可走流式方式。 | 可选。有真实 reader 时保留。 | 显示为“流式读取”。 |
| `checkpoint` | 表示 Transfer 任务可利用检查点恢复。 | 可选。有执行面支持时保留。 | 显示为“检查点”。 |
| `parallel_read` / `parallel_write` | 表示是否支持并行读写。 | 可选。有真实执行能力时保留。 | 显示为“并行读取 / 并行写入”。 |
| `connector_types` | 表示推荐 Transfer 连接器类型，如 reader=s3、writer=jdbc。 | Transfer 需要消费时必须保留。 | 展示为“读取连接器 / 写入连接器”。 |
| `supported_formats` | 表示 Transfer 可处理的数据格式范围。当前由各引擎能力 builder 列举；后续应迁移到平台 Format Registry，由“引擎访问能力 × 格式注册表 × Transfer/Preview 实现”共同推导。 | Transfer 涉及文件或对象格式时阶段性保留。 | 展示为格式标签；不要显示 i18n key。 |
| `preferred_writer` | 多 writer 可选时的默认写端。 | 可选。只有多 writer 或默认值有意义时保留。 | 默认折叠或在编辑任务时使用，不必在详情页重点展示。 |

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

字段说明：

| 字段 | 价值 / 作用 | 是否必须保留 | 展示建议 |
| --- | --- | --- | --- |
| `supported` | 表示 Manager 是否能预览该引擎的数据内容。 | 需要预览时必须保留。 | 不直接展示字段名；由后端展示模型派生“内容预览”是否可用。 |
| `modes` | 表示预览方式，如表格行、对象解析、原始文本、二进制元数据。 | 预览能力必须保留。 | 翻译为自然语言能力标签。 |
| `max_rows` | 表示表格、文档、图样本预览的默认行数上限。 | 有行样本预览时必须保留。 | 显示为“最多 N 行”。 |
| `max_bytes` | 表示对象或文件预览最大读取字节数。 | 有对象 / 文件预览时必须保留。 | 转换为 MB/GB 显示。 |
| `uses_composer` | 表示预览由 Manager 组合 Store、格式解析、Meta 属性等能力完成。 | 建议保留。它能解释预览不是引擎原生能力。 | 默认不展示，放入技术信息。 |
| `direct_preview` | 表示插件自身提供直接预览结果。 | 可选。有直接 PreviewProvider 时保留。 | 显示为“引擎直接预览”。 |

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

## 七、能力详情页展示设计草案

当前页面把能力 JSON 近似按结构铺成表格，用户需要自己理解 `real_time`、`uses_composer`、`range_read` 等字段，信息密度高但决策价值不清。改造目标是：先回答“这个引擎能做什么”，再允许开发者查看“为什么系统这么判断”。

能力详情页主展示不得直接消费原始 capabilities JSON 做字段级硬编码。System 后端应基于 `EngineCapabilities` 生成面向前端的 `capabilities_view`，前端按通用展示模型渲染。

`capabilities_view` 的职责：

- 决定哪些能力对用户有用、哪些只进入技术视图。
- 决定能力分组、排序、标签、状态和解释。
- 区分 `available`、`engine_unavailable`、`addp_pending` 等展示状态。
- 提供 i18n key 和参数，不直接返回固定中文文案作为唯一事实源。
- 将未知 `extensions` 转换为可陈列的 key-value 项。

前端职责：

- 渲染后端返回的展示模型。
- 根据当前语言解析 i18n key。
- 提供“查看 JSON”入口，以 key-value 树形方式查看完整能力声明；不在主页面展示原始 JSON 文本。

### 7.1 页面结构

建议能力声明页拆成四层：

| 区域 | 内容 | 展示方式 |
| --- | --- | --- |
| 概览区 | 引擎族、引擎类型、能力组、关键可用动作。 | 顶部摘要卡片和能力标签，例如“对象存储 / 可浏览目录 / 可范围读取 / 可作为传输来源和目标 / 支持对象解析预览”。 |
| 核心能力区 | 存储、计算、传输、预览四类能力。 | 分组卡片，每张卡片只显示用户可理解的能力结论。无能力的分组不显示空表。 |
| 目录模型区 | root 和 levels。 | 用路径链路展示，如 `服务 -> 桶 -> 前缀（可选） -> 对象（数据项）`。 |
| 扩展能力区 | `extensions` 中的引擎特有能力。 | 只要存在就展示，未知结构按 key-value 陈列。 |
| 技术查看入口 | schema version、path version、composer、完整能力声明等。 | 通过“查看 JSON”按钮打开 key-value 树形视图。 |

### 7.2 能力卡片内容

存储能力卡片建议显示：

- 目录浏览：支持实时浏览；支持的节点类型：桶、前缀、对象。
- 元数据采集：支持原生属性；如支持字段、索引、空间元数据则单独显示。
- 内容访问：按“读取方式”和“写入方式”分组，例如“流式读取、范围读取；不支持范围写入”。
- 语义说明：对象存储显示“桶、前缀、对象；前缀是对象 key 的组织方式，不是实体目录”。

传输能力卡片建议显示：

- 可作为来源 / 可作为目标。
- 读取连接器、写入连接器。
- 支持格式列表，使用格式标签展示。
- 检查点、并行读写、批量写等仅在为 true 时展示。

预览能力卡片建议显示：

- 预览方式：对象解析、文本预览、二进制元数据等。
- 限制：最大字节数转换成人类可读单位，例如 `10 MB`。
- 技术实现：`uses_composer` 默认不展示，只在技术信息区说明“由 Manager 组合预览”。

计算能力卡片建议显示：

- 查询语言、默认语言、结果类型。
- 工作流运行时、动态算子。
- Notebook / 脚本模式和语言。

### 7.3 字段文案原则

- 不直接展示布尔字段名，统一展示业务结论，例如把 `range_read: true` 显示为“支持范围读取”。
- 不把 `schema_version`、`path_version`、`i18n_key`、`supported` 放在主信息区。
- 不展示空分组、空数组和 false 字段，除非该“不支持”会阻止用户操作。
- 对 `not_supported` 中会影响理解的项，翻译成明确说明，例如“对象存储没有真实目录，前缀是 key 的组织视图”。
- 技术字段通过“查看 JSON”入口保留，便于开发、排障和规范校验。
- `engine_unavailable` 和 `addp_pending` 必须使用不同文案，不能都笼统显示为“不支持”。

### 7.4 i18n 处理原则

能力展示的 i18n 采用“后端给语义 key，前端负责翻译”的方式：

- 后端 `capabilities_view` 返回 `label_key`、`description_key`、`reason_key`、`value_key` 和 `params`。
- 前端根据当前语言加载 Console 翻译资源并渲染。
- 插件扩展能力如果需要被友好展示，应在后端展示定义中注册稳定 key；未注册的扩展能力仍按 key-value 显示。
- 不得让前端根据原始 capabilities 字段路径临时拼接中文文案。

### 7.5 MinIO 能力详情示例

MinIO 的主展示可整理为：

```text
Business MinIO
对象存储 · minio

能力摘要：
可浏览实时目录 / 可读取对象内容 / 支持范围读取 / 可作为传输来源和目标 / 支持对象解析预览

目录模型：
服务 -> 桶 -> 前缀（可选） -> 对象（数据项）

存储能力：
目录浏览：支持实时浏览；节点类型：桶、前缀、对象
元数据采集：支持原生属性
内容读取：流式读取、范围读取
明确不支持：范围写入、真实目录

传输适配：
可作为来源：是，读取连接器 s3
可作为目标：是，写入连接器 s3
支持格式：CSV、GeoJSON、JSON、Parquet、Shapefile

预览能力：
对象解析、文本预览、二进制元数据
最大读取：10 MB
```

技术人员点击“查看 JSON”后，可以看到完整能力声明的 key-value 树，包括 `schema_version`、`path_version`、`supported`、`uses_composer` 等字段。

这个设计只改变展示组织，不改变能力声明结构。最终是否删减字段，应先基于本规范中的“是否必须保留”列确认。

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

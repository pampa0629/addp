# ADDP 引擎插件接口规范

本文只规范 engine plugin 的接口边界和 provider 组合。能力声明结构见 [addp引擎能力声明规范.md](addp引擎能力声明规范.md)，存储路径语义见 [addp存储引擎路径体系规范.md](addp存储引擎路径体系规范.md)。

---

## 一、定位

Engine Plugin 是 ADDP 对外部数据系统和内部运行时的控制面入口。所有引擎实例由 System 统一登记，插件负责连接校验、连接测试、能力声明，以及目录、元数据、内容读写、查询和运行时等通用能力。

插件不承载业务模块私有逻辑：

- Meta 负责扫描任务和元数据落库。
- Manager 负责页面树、预览组合和缓存。
- Develop 负责编辑器、执行历史和交互体验。
- Transfer 保留自己的 Reader / Writer / DataBatch 执行面；后续如需统一配置来源，应先形成 Transfer 模块适配层规范，再进入 common engine 稳定接口。

---

## 二、基础接口

所有引擎必须实现 `EnginePlugin`：

```go
type EnginePlugin interface {
    Type() string
    DisplayName() string
    EngineOrigin() string
    DefaultPort() int
    RequiredFields() []string
    SensitiveFields() []string
    ValidateConnectionInfo(ConnectionInfo) error
    TestConnection(context.Context, ConnectionInfo) error
    Capabilities() EngineCapabilities
}
```

要求：

- `Type()` 必须稳定、小写、唯一，如 `postgresql`、`minio`、`neo4j`。
- `EngineOrigin()` 表达引擎来源，取值为 `general` 或 `extension`；它不是能力判断字段，上层功能判断必须基于 capabilities。
- `connection_info` 是所有引擎连接信息的统一事实源，保持 key-value map；字段 key 使用稳定英文机器名，不承载 i18n。
- `RequiredFields()`、`SensitiveFields()`、`ValidateConnectionInfo()` 和 `TestConnection()` 共同构成 System 引擎管理层的统一连接信息能力。
- `TestConnection()` 必须执行需要认证的最小只读真实操作，不能只做网络连通检查，也不得创建、更新、删除外部资源。
- `Capabilities()` 必须返回结构化 `engine.capabilities/v1` 能力声明。

`EngineOrigin()` 取值：

| 值 | 含义 |
| --- | --- |
| `general` | 用户熟悉的通用现成技术或主流引擎，如 PostgreSQL、MySQL、MinIO、Neo4j。 |
| `extension` | 按 ADDP 扩展规范实现的引擎或运行时，如 Python Workflow、Math Workflow。 |

### 内置插件加载入口

每个引擎插件仍在自己的包内通过 `init()` 注册，聚合包只负责按 `EngineOrigin()` 口径统一触发 blank import：

| 聚合包 | 覆盖范围 | 典型使用方 |
|---|---|---|
| `common/engine/plugins/builtin/general` | `EngineOrigin() == "general"` 的通用引擎 | Meta、Manager、Develop 等只需要通用数据引擎的进程 |
| `common/engine/plugins/builtin/extension` | `EngineOrigin() == "extension"` 的扩展运行时 | 需要工作流、Notebook、脚本运行时的进程 |
| `common/engine/plugins/builtin/all` | general + extension 全量内置插件 | 集成测试、诊断工具、需要完整注册表的进程 |

上层模块不应散落 blank import 具体引擎插件包。新增内置引擎插件时，应按插件自己的 `EngineOrigin()` 加入对应聚合包；功能开关和路由判断仍必须基于 `Capabilities()`，不能基于聚合包或 origin 推断能力。

### DSNProvider

数据库类 driver / 连接池确实需要 DSN 时，实现可选 `DSNProvider`：

```go
type DSNProvider interface {
    EnginePlugin
    BuildDSN(connInfo ConnectionInfo) (string, error)
}
```

规则：

- SQL、MongoDB、Neo4j 等需要底层 driver DSN 的插件可以实现 `DSNProvider`。
- MinIO、S3、NFS、Workflow、Script 不需要实现 `DSNProvider`。
- System 不依赖 `DSNProvider` 管理引擎连接。
- `BuildDSN()` 返回值不得持久化到 System，不得作为跨模块能力判断依据。
- 非数据库引擎不得返回 JSON 字符串冒充 connection string。

---

## 三、Provider 分层

### CatalogModelProvider

声明引擎目录层级和术语，不访问真实数据。

```go
type CatalogModelProvider interface {
    EnginePlugin
    CatalogModel() CatalogModelSpec
}
```

`CatalogLevelSpec.I18nKey` 是 UI 展示引擎原生术语的事实源。上层模块可以统一按 catalog model 消费目录与路径，但用户界面不应显示内部抽象名，而应使用该 key 翻译成 `Schema`、`数据库`、`Bucket`、`目录` 等原生术语。

示例层级：

| 引擎 | Catalog Model |
| --- | --- |
| PostgreSQL | `schema -> table/view` |
| MySQL / Doris / ClickHouse | `database -> table/view` |
| MongoDB | `database -> collection` |
| Neo4j | `database -> graph` |
| MinIO / S3 | `bucket -> prefix -> object` |
| NFS | `root -> directory -> file` |

### CatalogProvider

连接真实引擎，列出真实 node/item。

```go
type CatalogProvider interface {
    EnginePlugin
    ListChildren(ctx context.Context, connInfo ConnectionInfo, parent CatalogPath, opts ListOptions) ([]CatalogNode, error)
    ResolvePath(ctx context.Context, connInfo ConnectionInfo, path CatalogPath) (*CatalogNode, error)
}
```

公共调用方必须优先使用该统一入口，不再调用 `ListSchemas`、`ListTables`、`ListBuckets`、`ListCollections` 等旧上层接口。

### ItemMetadataProvider

描述叶子 item 的字段、统计、索引、空间信息和原生属性。

```go
type ItemMetadataProvider interface {
    EnginePlugin
    DescribeItem(ctx context.Context, connInfo ConnectionInfo, path CatalogPath, opts MetadataOptions) (*ItemMetadata, error)
}
```

文档型数据库可额外实现 `DocumentMetadataSamplingProvider`，用于采样推断动态字段信息。图数据库可额外实现 `GraphMetadataProvider`，用于描述图整体结构事实。

`ItemMetadata` 是 engine 侧叶子 item 的统一描述结果。对于 table 型 item，必须优先填充 `Table *datatype.TableInfo`，字段、主键、行数、大小、更新时间、表类型、注释和表级 native 事实都随 `TableInfo` 传递；对于 graph 型 item，必须优先填充 `Graph *datatype.GraphInfo`，节点结构、关系结构、连接模式、属性结构、节点数和关系数都随 `GraphInfo` 传递。`Fields`、`Stats`、`Attributes` 仅作为非 table / graph item 的通用补充或必要的 catalog 展示属性，不得成为新的 table / graph 事实源。公共消费方需要 table 字段、table facts 或 graph facts 时，应使用 `ItemMetadataFields()` / `ItemMetadataTableInfo()` / `ItemMetadataGraphInfo()` 这类 helper，而不是直接读 `Fields` / `Stats` 自行拼装。

对 tabular 引擎，`CatalogProvider.ListChildren()` 和 `ItemMetadataProvider.DescribeItem()` 必须围绕同一份 `datatype.TableInfo` 事实表达。表级通用事实进入 `Name`、`Kind`、`Comment`、`RowCount`、`SizeBytes`、`UpdatedAt`、`Fields` 等标准字段；来源原生但仍属表级的事实进入 `TableInfo.Native`，再由公共层透出到 `CatalogNode.Attributes.native` 或 `ItemMetadata.Attributes.native`。不得在列表接口保留一套事实、详情接口丢失另一套事实。

对 graph 引擎，`CatalogProvider` 暴露 graph item，label、relationship type 和 endpoint pattern 作为 `datatype.GraphInfo` 中的 schema / shape facts，而不是作为 graph data type 的主 item 本体。Neo4j label / relationship 只作为 Manager 展示投影或查询筛选条件，不作为公共 catalog item。graph 公共事实应围绕 `GraphInfo.NodeShapes`、`GraphInfo.RelationshipShapes` 和 `GraphRelationshipPatternInfo` 表达，不得继续把 `from_labels[]` / `to_labels[]` 两个集合作为 relationship endpoint 主事实。

`GraphMetadataProvider` / `GraphSampleProvider` 返回的是面向 ADDP 用户的业务图视图。Neo4j 插件、扩展或索引产生的内部节点和内部关系不得进入 graph schema、计数、样本、路径和上层算法投影；例如 Neo4j Spatial 的 `SpatialLayer` 节点以及 `RTREE_METADATA`、`RTREE_REFERENCE`、`RTREE_ROOT` 关系应由 provider 或 Graph 模块服务层过滤，不能要求 `common/datatype.GraphInfo` 携带具体引擎内部规则。

```go
type GraphMetadataProvider interface {
    EnginePlugin
    DescribeGraph(ctx context.Context, connInfo ConnectionInfo, path CatalogPath, opts MetadataOptions) (*datatype.GraphInfo, error)
}

type GraphSampleProvider interface {
    EnginePlugin
    SampleGraph(ctx context.Context, connInfo ConnectionInfo, path CatalogPath, opts GraphSampleOptions) (*GraphData, error)
}
```

`GraphSampleOptions.Filter` 使用强类型过滤条件，不得在 provider 边界继续透传松散 map：

```go
const (
    GraphSampleKindNodeShape         = "node_shape"
    GraphSampleKindRelationshipShape = "relationship_shape"
)

type GraphSampleFilter struct {
    Kind             string
    Labels           []string
    RelationshipType string
    FromLabels       []string
    ToLabels         []string
}
```

`Labels`、`FromLabels`、`ToLabels` 是采样执行时使用的完整 label set。它们只属于 sample provider 的过滤参数，不是 `GraphInfo` 的独立 relationship endpoint 主事实；持久化结构事实仍必须使用 `GraphRelationshipPatternInfo` 表达 from/to 配对模式。

`GraphData` 是 graph sample / graph query 返回的运行时图形数据，由节点和关系组成。它不属于 `common/datatype`，也不得写入 `type_info.graph`。

### StoreProvider

表达 item 内容访问能力。Catalog 回答“有什么”，Metadata 回答“它是什么样”，Store 回答“如何读写内容”。

```go
type StoreProvider interface {
    EnginePlugin
    StoreSemantics() StoreSemantics
}
```

常用扩展：

- `ContentReadableProvider.OpenContent()`：读取对象或文件内容。
- `ContentWritableProvider.CreateContent()`：写对象或文件内容。
- `RangeReadableProvider.OpenRange()`：按 byte range 读取对象或文件内容。
- `RangeWritableProvider.WriteRange()`：按 byte range / offset 写入内容。
- `BatchReadableProvider.ReadBatch()`：批量读取表或集合数据；图数据读取使用 `GraphSampleProvider` / `GraphQueryProvider`。
- `TableReadSessionProvider.OpenTableReadSession()`：打开表读取会话，连续读取批次；适合 PostgreSQL cursor、JDBC cursor、Parquet row group reader 等避免 offset 翻页退化的实现。
- `BatchWritableProvider.WriteBatch()`：批量写入表或集合数据；图写入应由图模块或专用 graph provider 明确建模。
- `TableWriteSessionProvider.OpenTableWriteSession()`：打开表写入会话，连续写入批次；适合 PostgreSQL COPY、JDBC bulk load 等避免每批重复建立写入会话的实现。

对象存储和文件系统不得互相继承，不共享 CatalogModel 或 catalog 拼装实现；二者最多共享内容流读写接口、MIME 推断、格式解析等底层 helper。

`OpenContent()`、`OpenRange()`、`CreateContent()` 等 store 能力接收的仍是**引擎自身 catalog model 下的 item `CatalogPath`**，不是另起一套只为底层 IO 服务的“物理路径 DTO”。调用方不得自行伪造脱离 `CatalogModelSpec` 的快捷路径；如果从物理路径、对象 key 或扫描候选重新定位 item，应使用 engine 公共层提供的路径构造规则，或直接复用 `CatalogNode.Path` / `FileEntry.CatalogPath`。物理路径可作为 node/item attribute 暴露给底层实现，但不能替代统一 catalog path 契约。

```go
type RangeReadableProvider interface {
    StoreProvider
    OpenRange(ctx context.Context, connInfo ConnectionInfo, path CatalogPath, opts ReadOptions) (io.ReadCloser, error)
}

type RangeWritableProvider interface {
    StoreProvider
    WriteRange(ctx context.Context, connInfo ConnectionInfo, path CatalogPath, offset int64, r io.Reader, opts WriteOptions) (int64, error)
}
```

说明：

- `range_read` / `range_write` 是 ADDP 能力层对称术语。
- 对文件系统，`range_write` 底层可由 `pwrite`、`WriteAt` 或 seek + write 实现。
- 对象存储通常支持 `range_read`，通常不支持 `range_write`。
- `atomic_rename`、`transactions`、`formats` 不作为 Store 顶层能力；如后续有真实调用方，再在具体 Provider 能力中设计。

### QueryRuntimeProvider

普通查询是计算能力，不等于表格型存储目录能力。普通查询返回 `QueryResult`，面向表格化、文档化或标量结果消费方，例如 Develop 查询编辑器、Manager 表格预览和查询服务。

```go
type QueryRuntimeProvider interface {
    EnginePlugin
    QueryLanguages() []string
    GenerateSampleQuery(ctx context.Context, connInfo ConnectionInfo, opts SampleQueryOptions) (query string, language string)
    ExecuteRuntimeQuery(ctx context.Context, connInfo ConnectionInfo, req QueryRequest) (*QueryResult, error)
}
```

按语言族细分：

- `SQLQueryRuntimeProvider.ExecuteSQL()`
- `DocumentQueryRuntimeProvider.ExecuteDocumentQuery()`

图查询不属于普通查询的一个返回格式变体。图查询使用独立 `GraphQueryProvider`，返回 `GraphQueryResult`，面向 Graph 模块、图可视化和图算法等需要节点 / 关系结构的调用方。Neo4j 可同时实现 `QueryRuntimeProvider` 和 `GraphQueryProvider`：前者用于普通 Cypher 表格结果和 Manager 预览兜底，后者用于图结构结果。图结构摘要由 `GraphMetadataProvider` 提供，图样本由 `GraphSampleProvider` 或 `GraphQueryProvider` 提供。

```go
type GraphQueryProvider interface {
    EnginePlugin
    ExecuteGraphQuery(ctx context.Context, connInfo ConnectionInfo, cypher string, opts QueryOptions) (*GraphQueryResult, error)
}
```

GORM、database/sql、Mongo driver、Neo4j driver、S3 client 都是实现 helper，不是领域主接口。

### Runtime Provider

工作流和脚本运行时使用独立 provider：

- `WorkflowRuntimeProvider`：工作流端点、算子发现、工作流执行。
- `ScriptRuntimeProvider`：Notebook/脚本端点和会话。

---

## 四、接口组合

| 引擎 | 推荐接口组合 |
| --- | --- |
| PostgreSQL / MySQL / Doris / ClickHouse / Spark SQL | `EnginePlugin` + `CatalogModelProvider` + `CatalogProvider` + `ItemMetadataProvider` + `SQLQueryRuntimeProvider` + `ConnectionPoolPlugin` |
| MongoDB | `EnginePlugin` + `CatalogModelProvider` + `CatalogProvider` + `ItemMetadataProvider` + `DocumentMetadataSamplingProvider` + `DocumentQueryRuntimeProvider` |
| Neo4j | `EnginePlugin` + `CatalogModelProvider` + `CatalogProvider` + `ItemMetadataProvider` + `GraphMetadataProvider` + `GraphSampleProvider` + `QueryRuntimeProvider` + `GraphQueryProvider` |
| MinIO / S3 | `EnginePlugin` + `CatalogModelProvider` + `CatalogProvider` + `ItemMetadataProvider` + `ContentReadableProvider` |
| NFS | `EnginePlugin` + `CatalogModelProvider` + `CatalogProvider` + `ItemMetadataProvider` + `ContentReadableProvider` |
| Python / Spark / Math Workflow | `EnginePlugin` + `WorkflowRuntimeProvider` |
| Jupyter | `EnginePlugin` + `ScriptRuntimeProvider` |

---

## 五、上层消费规则

- System：通过 `EnginePlugin` 做注册、连接测试、连接信息校验和能力声明刷新；通过 `CatalogProvider.ListChildren()` 对外提供实时 catalog 浏览控制面 API：`POST /api/v1/system/engines/:id/catalog/children`。
- Meta：使用 `CatalogProvider` 扫描目录并落库，使用 `ItemMetadataProvider` / `DocumentMetadataSamplingProvider` 获取叶子元数据；扫描编排必须先读取 `CatalogModelSpec`，再结合 provider 组合选择 catalog scan strategy。`engine_family` 只能作为粗分类或展示字段，不能单独决定 namespace 术语、item 术语、扫描层级和内容读取方式。公开 API 应聚焦扫描后元数据快照，不再新增实时浏览公共接口。
- Meta 扫描 API 和任务参数中的路径型目标统一命名为 `catalog_paths`。它表示引擎 catalog model 下的路径。
- Manager：使用 Meta 树构建探查树；预览由 Manager 自身 preview provider / composer 组合完成。结构化数据优先消费 `BatchReadableProvider` 或只读 sample query；graph 预览优先消费 `type_info.graph` / `GraphMetadataProvider` 得到 schema 视图，并通过 `GraphSampleProvider` 或 `GraphQueryProvider` 获取轻量子图样本；对象/文件优先消费 `ContentReadableProvider` 并结合格式解析。
- Develop：使用 `QueryRuntimeProvider`、`WorkflowRuntimeProvider`、`ScriptRuntimeProvider`；图结构展示入口使用 `GraphMetadataProvider` / `GraphQueryProvider`。
- Service：发布普通查询服务时使用 query runtime 和 Meta item/spatial 元数据；图查询服务使用 `GraphQueryProvider`。图查询服务的易用向导应消费 graph item 的 `type_info.graph.node_shapes`，不得再从 Meta 树读取 Neo4j label item。
- Transfer：执行面暂由 Transfer 自己负责；后续如需统一生成 Reader/Writer 配置，应在 Transfer 模块适配层中规范化。高吞吐数据搬运优先消费 batch / stream 能力，而不是 query runtime。

---

## 六、禁止事项

- 不得在上层模块直接依赖旧 `ListXXX` 接口。
- 不得把所有目录层级统一硬编码为 schema/table。
- 不得把对象存储当作 POSIX 文件系统建模。
- 不得让插件返回 JSON 字符串形式的 capabilities。
- 不得让非 DSN 引擎返回 JSON 字符串冒充 connection string。
- 不得在 capabilities 中保存任务级运行参数。
- 不得在 `CatalogProvider` / `ItemMetadataProvider` 中执行写入、DDL、统计刷新等有外部副作用的操作；连接测试也必须保持只读。

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

示例层级：

| 引擎 | Catalog Model |
| --- | --- |
| PostgreSQL | `schema -> table/view` |
| MySQL / Doris / ClickHouse | `database -> table/view` |
| MongoDB | `database -> collection` |
| Neo4j | `database -> label/relationship` |
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

文档型数据库可额外实现 `DocumentMetadataSamplingProvider`，用于采样推断动态字段结构。

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
- `BatchReadableProvider.ReadBatch()`：批量读取表、集合或图数据。
- `BatchWritableProvider.WriteBatch()`：批量写入表、集合或图数据。

对象存储和文件系统不得互相继承，不共享 CatalogModel 或 catalog 拼装实现；二者最多共享内容流读写接口、MIME 推断、格式解析等底层 helper。

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

图查询不属于普通查询的一个返回格式变体。图查询使用独立 `GraphQueryProvider`，返回 `GraphQueryResult`，面向 Graph 模块、图可视化和图算法等需要节点 / 关系结构的调用方。Neo4j 可同时实现 `QueryRuntimeProvider` 和 `GraphQueryProvider`：前者用于普通 Cypher 表格结果和 Manager 预览兜底，后者用于图结构结果。

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
| Neo4j | `EnginePlugin` + `CatalogModelProvider` + `CatalogProvider` + `ItemMetadataProvider` + `QueryRuntimeProvider` + `GraphQueryProvider` |
| MinIO / S3 | `EnginePlugin` + `CatalogModelProvider` + `CatalogProvider` + `ItemMetadataProvider` + `ContentReadableProvider` |
| NFS | `EnginePlugin` + `CatalogModelProvider` + `CatalogProvider` + `ItemMetadataProvider` + `ContentReadableProvider` |
| Python / Spark / Math Workflow | `EnginePlugin` + `WorkflowRuntimeProvider` |
| Jupyter | `EnginePlugin` + `ScriptRuntimeProvider` |

---

## 五、上层消费规则

- System：通过 `EnginePlugin` 做注册、连接测试、连接信息校验和能力声明刷新；通过 `CatalogProvider.ListChildren()` 对外提供实时 catalog 浏览控制面 API：`POST /api/v1/system/engines/:id/catalog/children`。
- Meta：使用 `CatalogProvider` 扫描目录并落库，使用 `ItemMetadataProvider` / `DocumentMetadataSamplingProvider` 获取叶子元数据；公开 API 应聚焦扫描后元数据快照，不再新增实时浏览公共接口。
- Manager：使用 Meta 树构建探查树；预览由 Manager 自身 preview provider / composer 组合完成。结构化数据优先消费 `BatchReadableProvider` 或只读 sample query，图 label / relationship 可使用 `GraphQueryProvider` 采样后表格化展示，对象/文件优先消费 `ContentReadableProvider` 并结合格式解析。
- Develop：使用 `QueryRuntimeProvider`、`WorkflowRuntimeProvider`、`ScriptRuntimeProvider`；图结构展示入口使用 `GraphQueryProvider`。
- Service：发布普通查询服务时使用 query runtime 和 Meta item/spatial 元数据；图查询服务使用 `GraphQueryProvider`。
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

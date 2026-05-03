# ADDP 引擎插件接口规范

本文只规范 engine plugin 的接口边界和 provider 组合。能力声明结构见 [addp引擎能力声明规范.md](addp引擎能力声明规范.md)，存储路径语义见 [addp存储引擎路径体系规范.md](addp存储引擎路径体系规范.md)。

---

## 一、定位

Engine Plugin 是 ADDP 对外部数据系统和内部运行时的控制面入口。所有引擎实例由 System 统一登记，插件负责连接校验、连接测试、能力声明，以及目录、元数据、内容读写、查询和运行时等通用能力。

插件不承载业务模块私有逻辑：

- Meta 负责扫描任务和元数据落库。
- Manager 负责页面树、预览组合和缓存。
- Develop 负责编辑器、执行历史和交互体验。
- Transfer 保留自己的 Reader / Writer / DataBatch 执行面，后续通过 TransferAdapter 消费插件能力和连接配置。

---

## 二、基础接口

所有引擎必须实现 `EnginePlugin`：

```go
type EnginePlugin interface {
    Type() string
    DisplayName() string
    EngineCategory() string
    DefaultPort() int
    RequiredFields() []string
    SensitiveFields() []string
    ValidateConnectionInfo(ConnectionInfo) error
    BuildConnectionString(ConnectionInfo) (string, error)
    TestConnection(context.Context, ConnectionInfo) error
    Capabilities() EngineCapabilities
}
```

要求：

- `Type()` 必须稳定、小写、唯一，如 `postgresql`、`minio`、`neo4j`。
- `TestConnection()` 必须执行需要认证的最小真实操作，不能只做网络连通检查。
- `Capabilities()` 必须返回结构化 `engine.capabilities/v1` 能力声明。
- `BuildConnectionString()` 是当前接口保留项；不适合连接串表达的引擎可返回可读描述或错误，但上层不应依赖它发现能力。

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
- `BatchReadableProvider.ReadBatch()`：批量读取表、集合或图数据。
- `BatchWritableProvider.WriteBatch()`：批量写入表、集合或图数据。

对象存储和文件系统不得互相继承；二者最多共享内容读写 provider。

### QueryRuntimeProvider

查询是计算能力，不等于表格型存储目录能力。

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
- `GraphQueryRuntimeProvider.ExecuteRuntimeGraphQuery()`

GORM、database/sql、Mongo driver、Neo4j driver、S3 client 都是实现 helper，不是领域主接口。

### Runtime Provider

工作流和脚本运行时使用独立 provider：

- `WorkflowRuntimeProvider`：工作流端点、算子发现、工作流执行。
- `ScriptRuntimeProvider`：Notebook/脚本端点和会话。

---

## 四、接口组合

| 引擎 | 推荐接口组合 |
| --- | --- |
| PostgreSQL / MySQL / Doris / ClickHouse / Spark SQL | `EnginePlugin` + `StoragePlugin` + `CatalogModelProvider` + `CatalogProvider` + `ItemMetadataProvider` + `SQLQueryRuntimeProvider` + `ConnectionPoolPlugin` |
| MongoDB | `EnginePlugin` + `StoragePlugin` + `CatalogModelProvider` + `CatalogProvider` + `ItemMetadataProvider` + `DocumentMetadataSamplingProvider` + `DocumentQueryRuntimeProvider` |
| Neo4j | `EnginePlugin` + `StoragePlugin` + `CatalogModelProvider` + `CatalogProvider` + `ItemMetadataProvider` + `GraphDBPlugin` + `GraphQueryRuntimeProvider` |
| MinIO / S3 | `EnginePlugin` + `StoragePlugin` + `CatalogModelProvider` + `CatalogProvider` + `ItemMetadataProvider` + `ContentReadableProvider` |
| NFS | `EnginePlugin` + `StoragePlugin` + `CatalogModelProvider` + `CatalogProvider` + `ItemMetadataProvider` + `ContentReadableProvider` |
| Python / Spark / Math Workflow | `EnginePlugin` + `ComputePlugin` + `WorkflowRuntimeProvider` |
| Jupyter | `EnginePlugin` + `ComputePlugin` + `ScriptRuntimeProvider` |

---

## 五、上层消费规则

- System：只通过 `EnginePlugin` 做注册、连接测试、连接信息校验和能力声明刷新。
- Meta：使用 `CatalogProvider` 扫描目录，使用 `ItemMetadataProvider` / `DocumentMetadataSamplingProvider` 获取叶子元数据。
- Manager：使用 Meta 树构建探查树，预览时组合 `ItemMetadataProvider`、`ContentReadableProvider` 和 query runtime。
- Develop：使用 `QueryRuntimeProvider`、`WorkflowRuntimeProvider`、`ScriptRuntimeProvider`。
- Service：发布查询服务时使用 query runtime 和 Meta item/spatial 元数据。
- Transfer：执行面暂由 Transfer 自己负责，后续通过 `TransferAdapter` 生成 Reader/Writer 配置。

---

## 六、禁止事项

- 不得在上层模块直接依赖旧 `ListXXX` 接口。
- 不得把所有目录层级统一硬编码为 schema/table。
- 不得把对象存储当作 POSIX 文件系统建模。
- 不得让插件返回 JSON 字符串形式的 capabilities。
- 不得在 capabilities 中保存任务级运行参数。

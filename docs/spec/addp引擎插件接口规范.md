# ADDP 引擎插件接口规范

本文只规范 engine plugin 的接口边界和 provider 组合。能力声明结构见 [addp引擎能力声明规范.md](addp引擎能力声明规范.md)，存储路径语义见 [addp存储引擎路径体系规范.md](addp存储引擎路径体系规范.md)。

---

## 一、定位

Engine Plugin 是 ADDP 对外部数据系统和内部运行时的控制面入口。所有引擎实例由 System 统一登记，插件负责连接校验、连接测试、能力声明，以及目录、元数据、内容读写、查询和运行时等通用能力。

插件不承载业务模块私有逻辑：

- Meta 负责扫描任务和元数据落库。
- Manager 负责页面树、预览组合和缓存。
- Develop 负责编辑器、执行历史和交互体验。
- Transfer 负责任务配置、planner、policy、transform、worker、checkpoint、日志、指标和写后 Meta 扫描触发；具体 engine-native 读写必须消费 `common/engine` provider，不在 Transfer 内恢复私有 Reader / Writer 插件体系。

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
- System 管理的内置插件必须额外实现 `ConnectionIdentityProvider.ConnectionIdentityFields()`，返回决定 Engine Instance 物理端点身份的非敏感字段。数据库插件通常声明 `host`、`port`、`database`；对象存储声明 `endpoint`；NFS 声明 `server`、`export_path`；HTTP 工作流运行时声明 `protocol`、`host`、`port`。
- System 创建 Engine Instance 后会冻结身份字段。更新请求改变任一身份字段时返回 HTTP 409，用户必须创建新的 Engine Instance。默认端口和默认数据库应按插件语义归一化后比较，不能把省略默认值和显式默认值误判为不同端点。
- 自研且未编译进当前进程的 extension engine 使用标准 HTTP 运行时身份字段 `protocol + host + port`，不得通过任意非敏感字段猜测身份。
- `TestConnection()` 必须执行需要认证的最小只读真实操作，不能只做网络连通检查，也不得创建、更新、删除外部资源。
- `Capabilities()` 必须返回结构化 `engine.capabilities/v1` 能力模板。该方法不得连接具体实例，不做运行时探测，只表达插件和 Provider 实现的能力上限。
- 需要按实例探测扩展、版本或函数可用性的插件，应额外实现 `InstanceCapabilitiesResolver`，由 System 在保存或刷新具体引擎记录时调用并生成落库能力声明。

```go
type InstanceCapabilitiesResolver interface {
    ResolveCapabilities(context.Context, ConnectionInfo, EngineCapabilities) (EngineCapabilities, error)
}
```

`EngineOrigin()` 取值：

| 值 | 含义 |
| --- | --- |
| `general` | 用户熟悉的通用现成技术或主流引擎，如 PostgreSQL、MySQL、MinIO、Neo4j。 |
| `extension` | 按 ADDP 扩展规范实现的引擎或运行时，如 GeoPython Workflow、Math Workflow。 |

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
| PostgreSQL | `server(root) -> schema -> table/view` |
| MySQL / Doris / ClickHouse | `server(root) -> database -> table/view` |
| MongoDB | `server(root) -> database -> collection` |
| Neo4j | `server(root) -> database -> graph` |
| MinIO / S3 | `service(root) -> bucket -> prefix -> object` |
| NFS | `root -> directory -> file` |
| Kafka | `service(root) -> topic` |

`CatalogModelSpec.RootTerm` 表达结构 root，`Levels` 只描述 root 下的业务层级，不包含 root。所有 catalog path 必须以显性 root segment 开始；`CatalogPath.StringPath()` 与 ResourceLocator 业务路径会跳过该 root segment。

### CatalogProvider

连接真实引擎，列出真实 catalog entry。

```go
type CatalogProvider interface {
    EnginePlugin
    ListChildren(ctx context.Context, connInfo ConnectionInfo, parent CatalogPath, opts ListOptions) ([]CatalogEntry, error)
    ResolvePath(ctx context.Context, connInfo ConnectionInfo, path CatalogPath) (*CatalogEntry, error)
}
```

公共调用方必须优先使用该统一入口，不再调用 `ListSchemas`、`ListTables`、`ListBuckets`、`ListCollections` 等旧上层接口。

`ListChildren` 不接受 empty path 作为业务枚举入口。实时浏览需要展示根层时，由上层通过 `CatalogRootEntry(model, engineID, engineName)` 返回结构 root；枚举第一层业务节点时必须调用 `ListChildren(rootPath)`。provider 收到 empty path 应视为调用错误。

`CatalogEntry.Role` 只允许 `branch` / `leaf`，表达 catalog 结构角色；`CatalogEntry.Term` 表达引擎原生术语，例如 `schema`、`table`、`bucket`、`prefix`、`object`、`file`、`collection`、`graph`。Engine 层不得用 `item` 表达 ADDP data item，也不得用 `is_item` / `is_container` 这类布尔字段作为主路径。

`CatalogEntry` 是实时列表和路径解析用的轻量 catalog 条目，`Entry` 表达“目录条目 / 列表项”，不是“入口”。它回答“当前位置下面有什么、结构上怎么走”，不回答完整详情。稳定列表摘要必须使用显式字段：表格型 leaf 摘要进入 `Table *datatype.TableInfo`，branch 下直接 leaf 数量摘要进入 `LeafCount`，文件 / 对象列表事实进入 `Storage *CatalogStorageFacts` 和 `UpdatedAt`。`CatalogEntry.Table` 只能承载 `Name`、`Kind`、`Comment`、`EstimatedRowCount`、`SizeBytes`、`UpdatedAt`、`Native` 等列表级表摘要；只有来源能保证低成本值就是精确值时才可填充 `RowCount`，不得用估算值填充。列表摘要不应填充 `Fields` / `PrimaryKey`；`CatalogEntry.Storage` 只能承载 `Path`、`ContentType`、`ETag`、`SizeBytes` 等列表级存储摘要，不应填充 `Name` / `Extension` 等详情或派生事实。字段、主键、索引、graph schema、采样、完整 storage facts 等详情事实必须通过 `CatalogFactsProvider` 返回。`CatalogEntry` 不保留 `Attributes` 或 `Stats` 兜底口袋；Meta item attributes 和展示统计是扫描落库后的上层语义，不应回流为 engine listing 字段。

### CatalogFactsProvider

描述 catalog entry 或 leaf 的字段、统计、索引、空间信息和原生属性。

```go
type CatalogFactsProvider interface {
    EnginePlugin
    DescribeCatalogFacts(ctx context.Context, connInfo ConnectionInfo, path CatalogPath, opts CatalogFactsOptions) (*CatalogFacts, error)
}
```

动态 schema 数据库可额外实现 `DynamicSchemaSamplingProvider`，用于采样推断字段结构。该 provider 表达的是字段结构推断能力，不表示 catalog leaf 的 data type 是 `document`，也不承担 Manager 数据剖析。图数据库的整体结构事实必须通过 `CatalogFactsProvider` 返回到 `CatalogFacts.Graph`，不再另设 graph facts provider。

`CatalogFacts` 是 engine 侧 catalog entry 的统一事实详情结果。它回答“这个条目自身有哪些 engine 直接知道的事实”，不同于 `CatalogEntry` 的实时列表结构。对于 table 型 leaf，必须优先填充 `Table *datatype.TableInfo`，字段、主键、行数、大小、更新时间、表类型、注释和表级 native 事实都随 `TableInfo` 传递；对于 graph 型 leaf，必须优先填充 `Graph *datatype.GraphInfo`，节点结构、关系结构、连接模式、属性结构、节点数和关系数都随 `GraphInfo` 传递；对于 file / object leaf，必须优先填充 `Storage *CatalogStorageFacts` 表达名称、路径、大小、MIME、etag、扩展名等存储事实。`CatalogFacts` 不保留 `Stats` 兜底口袋；公共消费方需要 table 字段、graph facts 或完整 storage facts 时，应使用 `CatalogFactsTableInfo()` / `CatalogFactsGraphInfo()` 或直接消费 `CatalogFacts.Storage`；构造列表 entry 时应使用 `CatalogEntryTableInfo()` / `CatalogEntryStorageInfo()` 这类摘要 helper。

`CatalogFacts` 不承载 `DocumentInfo`、`MediaInfo` 或 `ContainerInfo`。文档、图片、音视频、压缩包、Excel、SQLite / GeoPackage 等 encoded content 的标题、语言、页数、宽高、时长、编码、颜色空间、内部 child 列表、默认入口等信息，必须由 Meta / Manager / Transfer 等编排层先通过 StoreProvider 构造内容读取抽象，再交给 `common/format` 的 `DocumentInfoProvider`、`MediaInfoProvider`、`ContainerInfoProvider` 或对应 content reader 提取。Engine 只提供 catalog / storage 事实和内容访问能力，不读取内容后裁决 format 语义。

Kafka topic 使用 `CatalogFacts.Topic *TopicFacts` 表达实时 topic 事实。第一版 `TopicFacts` 只允许 `PartitionCount`、`ReplicationFactor` 和按 partition 的 leader / replica / ISR / earliest offset / latest offset 诊断；不得读取消息样本推断 schema，也不得把 partition 投影为 catalog child。Topic facts 默认只用于实时诊断，在 Meta attributes 正式定义持久化结构前不得塞入 `Native` 或其他兜底 map。

`CatalogFacts` 不定义 `FileInfo`。file、object、directory、bucket、prefix、root 等只表达 catalog / storage 形态，不是 data type 主事实；引擎插件不得返回 `DataTypeFile`、`FileInfo` 或 `type_info.file`。路径、名称、大小、MIME、etag、hash、修改时间等存储事实应放在 `CatalogEntry` / `CatalogFacts.Storage` / `CatalogFacts.UpdatedAt` 标准字段中；内容语义无法识别时使用 `datatype.Unknown`。

对 tabular 引擎，`CatalogProvider.ListChildren()` 和 `CatalogFactsProvider.DescribeCatalogFacts()` 必须围绕同一份 `datatype.TableInfo` 事实表达。表级通用事实进入 `Name`、`Kind`、`Comment`、`RowCount`、`EstimatedRowCount`、`SizeBytes`、`UpdatedAt`、`Fields` 等标准字段；`RowCount` 只表示精确值，`EstimatedRowCount` 只表示估算值，0 是有效精确值。来源原生但仍属表级的事实进入 `TableInfo.Native`，列表接口通过 `CatalogEntry.Table.Native` 透出，详情接口通过 `CatalogFacts.Table.Native` 透出。`CatalogFacts` 不保留 `Attributes` 兜底口袋。不得在列表接口保留一套事实、详情接口丢失另一套事实。

Tabular provider 默认不执行高成本真实 row count。只有调用方显式传入 `CatalogFactsOptions.IncludeStatistics=true` 或走专用计数入口时，才允许调用 `RowCount` callback；该 callback 必须返回精确值。列表和路径解析阶段只能使用元数据来源中已有的统计估算，并写入 `EstimatedRowCount`。PostgreSQL、MySQL/Doris、ClickHouse 这类能从 catalog / system table 获得统计估算的引擎不得把该值写入 `RowCount`；Spark SQL 这类列表阶段没有低成本估算的引擎保持 `EstimatedRowCount` 为空。未知值保持为空，不得用 `0` 表示未知。

SQL catalog facts provider 的实现边界：

- Common Engine 的 provider 是对上层模块的稳定能力契约；SQL catalog facts helper 只是 provider 内部实现复用工具，不作为新的对外抽象层。
- `common/sqldialect` 当前定位为查询 SQL helper，负责标识符引用、表名限定、分页、count/sample SQL 等；不得混入 catalog facts 探测逻辑。
- Catalog facts helper 只在多个引擎共享同一类事实来源时抽取，例如 MySQL/Doris 共享 `information_schema`；PostgreSQL、ClickHouse、Spark SQL 等差异较大的实现应保留在插件内，不做大一统 `SQLCatalogFactsDialect`。
- GORM 只作为连接池、driver 和 raw SQL 执行工具，不承担 ADDP 的 catalog path、catalog facts、系统库过滤、row count 策略等平台元数据语义。

SQL catalog facts provider 差异矩阵：

| 引擎 | namespace 术语 | catalog facts 来源 | 表类型映射 | 字段信息来源 | row count 策略 | 系统 namespace / leaf 过滤 | 当前复用边界 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| PostgreSQL | schema | `information_schema.schemata/tables/columns` + `pg_class` + `pg_stat_user_tables` | `BASE TABLE` -> `table`，`VIEW` -> `view`，其他 `table_type` 转小写下划线 | `information_schema.columns`，主键来自约束表，注释来自 `col_description` | 列表使用 `pg_class.reltuples` 写入 `estimated_row_count`；显式统计执行 `COUNT(*)` 写入 `row_count`，不主动 `ANALYZE` | `pg_catalog`、`information_schema`、`pg_toast`、`pg_temp_*`、`pg_toast_*`；当实例检测到 SuperMap SDX+ 时过滤 `sm*` 系统 leaf | 暂留插件内；PostgreSQL 原生 catalog 语义较强，不与 MySQL/Doris 合并 |
| MySQL | database | `information_schema.schemata/tables/columns` | `BASE TABLE` -> `table`，`VIEW` -> `view`，其他 `table_type` 转小写下划线 | `information_schema.columns`，主键来自 `column_key`，注释来自 `column_comment` | 列表将 `information_schema.tables.table_rows` 写入 `estimated_row_count`；显式统计执行 `COUNT(*)` 写入 `row_count` | `information_schema`、`mysql`、`performance_schema`、`sys` | 与 Doris 共享 `MySQLCompatibleCatalogFactsDialect`；可启用表级 `Native.engine` |
| Doris | database | MySQL 兼容 `information_schema.schemata/tables/columns` | 同 MySQL 兼容逻辑 | 同 MySQL 兼容逻辑，注释能力按引擎实际返回 | 列表将 `information_schema.tables.table_rows` 写入 `estimated_row_count`；显式统计执行 `COUNT(*)` 写入 `row_count` | MySQL 系统库 + `__internal_schema` | 与 MySQL 共享 `MySQLCompatibleCatalogFactsDialect`；`Native.engine` 待确认 `information_schema.tables.engine` 稳定性后再启用 |
| ClickHouse | database | `system.databases`、`system.tables`、`system.columns` | `MaterializedView` -> `materialized_view`，`View`/其他包含 `View` 的 engine -> `view`，其他 -> `table` | `system.columns`，nullable 从类型字符串推断，`DEFAULT` / `MATERIALIZED` / `ALIAS` 映射到通用默认值和生成列字段，当前不表达主键 | 列表将 `system.tables.total_rows` 写入 `estimated_row_count`；显式统计执行 `COUNT(*)` 写入 `row_count` | `system`、`information_schema`、`INFORMATION_SCHEMA` | 暂留插件内；ClickHouse `system.*` 语义独立 |
| Spark SQL | database | `SHOW DATABASES`、`SHOW TABLES`、`DESCRIBE`，部分环境可查询 `information_schema` | 当前 `SHOW TABLES` 结果统一映射为 `table` | `DESCRIBE table` | 列表阶段不做真实 count，未知 `row_count` / `size_bytes` 保持为空；单表 catalog facts 显式请求统计时才执行 `COUNT(*)` | `information_schema`、`sys` | 暂留插件内；Spark catalog facts 更偏命令式接口 |

对 graph 引擎，`CatalogProvider` 暴露 graph catalog leaf，label、relationship type 和 endpoint pattern 作为 `datatype.GraphInfo` 中的 schema / shape facts，而不是作为 graph data type 的主 catalog leaf 本体。Neo4j label / relationship 只作为 Manager 展示投影或查询筛选条件，不作为公共 catalog leaf。graph 公共事实应围绕 `GraphInfo.NodeShapes`、`GraphInfo.RelationshipShapes` 和 `GraphRelationshipPatternInfo` 表达，不得继续把 `from_labels[]` / `to_labels[]` 两个集合作为 relationship endpoint 主事实。

`CatalogFactsProvider` / `GraphSampleProvider` 返回的是面向 ADDP 用户的业务图视图。Neo4j 插件、扩展或索引产生的内部节点和内部关系不得进入 graph schema、计数、样本、路径和上层算法投影；例如 Neo4j Spatial 的 `SpatialLayer` 节点以及 `RTREE_METADATA`、`RTREE_REFERENCE`、`RTREE_ROOT` 关系应由 provider 或 Graph 模块服务层过滤，不能要求 `common/datatype.GraphInfo` 携带具体引擎内部规则。

```go
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

表达 item 内容访问能力。Catalog 回答“有什么”，Facts 回答“engine 直接知道什么”，Store 回答“如何读写内容”。

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
- `TableWritePreparer.PrepareTableWrite()`：执行表级写入前准备动作，例如 ensure database / schema、create table、校验目标表结构和安全 schema evolution。该能力不写入数据行，也不承载 Transfer 的 replace / append policy。
- `BoundedWatermarkReadProvider.OpenBoundedWatermarkRead()`：在引擎一致性读边界内冻结复合 watermark 上界，按稳定顺序读取 `(start, upper_bound]`。session 必须返回上界，并能从已读取行生成 provider 可解释的复合位置；普通 batch reader 不得被推断为具备该语义。
- `TableUpsertProvider.PrepareTableUpsert()` / `UpsertBatch()`：按显式稳定键准备目标并幂等应用 insert/update。Provider 必须校验键字段和唯一约束；普通 `BatchWritableProvider` 或 COPY session 不得被推断为 upsert。
- `ChangeStreamReaderProvider.OpenChangeStream()`：打开 partitioned change stream，按 provider position seek、poll 原始记录并支持受控 pause/resume/close。Kafka topic 不能伪装成 `BatchReadableProvider` 或 content `stream_read`。
- `PartitionedTableChangeApplyProvider.PreparePartitionedTableChangeApply()` / `ApplyPartitionedTableChanges()`：把单个 source partition 的已映射表变化与目标 apply position 在同一目标事务中提交。PostgreSQL 当前真实实现 `upsert|delete|skip`；`skip` 只推进 ledger，不修改业务行。同一 key 在批内只保留最高 position 的最终数据操作，目标行变化和 `addp_transfer.apply_positions` 必须原子提交。普通 `TableUpsertProvider`、Infra state CAS 或 runtime lease 均不得被推断为具备目标侧 monotonic apply 语义。

第一版 PostgreSQL watermark 契约：

- `BoundedWatermarkReadOptions` 必须包含一个 watermark field、至少一个 tie breaker 和可选 committed start cursor。
- Provider 在 PostgreSQL repeatable-read 只读事务中冻结上界；游标字段不得为 NULL，tie breaker 必须匹配非 partial unique/primary key。
- `WatermarkCursor.Values` 使用 canonical string 保存，具体列类型转换由 source Provider 解释。
- `TableUpsertProvider` 使用稳定 keys 和单批事务提交；重复应用同一批必须得到相同目标状态。
- Transfer 只在目标批次提交成功后推进 `transfer.sync_states`，Provider 不直接维护任务状态。

### ChangeStreamReaderProvider

Change stream 是无限、分区、有 committed position 的源能力，与对象内容流和 bounded table batch 不同：

```go
type ChangeStreamReaderProvider interface {
    StoreProvider
    OpenChangeStream(
        ctx context.Context,
        connInfo ConnectionInfo,
        topic CatalogPath,
        opts ChangeStreamReadOptions,
    ) (ChangeStreamReader, error)
}

type ChangeStreamReader interface {
    Poll(ctx context.Context, maxRecords int) (*ChangeRecordBatch, error)
    PositionRanges(ctx context.Context) ([]ChangeStreamPositionRange, error)
    Assignments() []string
    Pause(ctx context.Context, partitions []string) error
    Resume(ctx context.Context, partitions []string) error
    Close(ctx context.Context) error
}
```

稳定数据结构语义：

- `ChangeRecord` 保存 topic、partition、原始 record offset、record timestamp、headers、key bytes、value bytes，以及消费该记录后应提交的 provider position。
- `ChangeRecordBatch.EndPositions` 按 partition 返回批次成功应用后可提交的位置；Transfer 不得根据记录数量自行计算 offset。
- `ChangeStreamPosition` 必须包含 `type`、`version`、`partition` 和 canonical string `values`。Kafka 第一版固定为 `type=kafka_offset`、`version=v1`、`values.next_offset`；`next_offset` 是下一条应消费的 offset，不是最后一条已处理 offset。
- `ChangeStreamReadOptions` 按 partition 传入 committed positions，并显式传入首次无状态 partition 的 `earliest|latest` 策略、poll timeout 和容量上限。Provider 不得静默使用客户端默认 offset reset。
- `ChangeStreamReader.PositionRanges()` 必须通过已打开的 reader/client 返回当前每分区 earliest/latest provider position；`latest` 表示分区末尾的下一位置。该方法用于 lag 与 retention 诊断，不得为每次采样新建第二个长期 consumer，也不得把 fetch position 当作 committed position。
- Kafka Provider 必须关闭 consumer auto commit。consumer group 只可用于 assignment/rebalance，不得覆盖 Transfer 传入的 committed positions。
- rebalance revoke 时 Provider 必须停止被撤销 partition 的新 poll，并让调用方完成或放弃未提交批次；Provider 不得在目标结果未知时自行提交 position。
- `Pause` / `Resume` 只控制 reader 拉取和背压，不改变任务 desired state、execution status 或同步主状态。

Provider 只返回原始 ChangeRecord，不负责 JSON、Debezium、Avro 或 Protobuf 解码。Transfer source adapter 负责把 record 解码、校验并归一化为内部 ChangeEvent；第一版 keyed JSON record 归一化为 `operation=upsert`。

`ChangeEvent`、transform 和 `ChangeApplyWriter` 属于 Transfer runtime，不是 Engine Catalog/Store Provider。Transfer 将已映射行、目标 key、`upsert|delete|skip` 操作、每条记录的 provider position 和单 partition 批次边界交给 `PartitionedTableChangeApplyProvider`；Provider 不解析 JSON、Kafka record、Debezium envelope 或 DLQ envelope，只负责目标数据库内的原子数据应用与位置账本。`skip` 不携带 row，只允许在 Transfer 已持久化对应 dead-letter 事实后使用；Provider 不负责验证 DLQ 存储。

第一版 PostgreSQL 契约：

- Transfer task 提供服务端生成且不可变的 `apply_identity` UUID；Provider 同时记录并校验 source identity、target identity、partition、position type/version。
- Provider 在业务目标 PostgreSQL 的 `addp_transfer.apply_positions` 保存目标 apply ledger；该表不是 Infra PostgreSQL 的 `transfer.sync_states`，也不是用户业务表的隐藏 offset 列。
- 每次 apply 只接受一个 partition，batch 包含 start/end position，且每条变化携带消费后的 position。Kafka v1 只接受单调递增的 `kafka_offset/v1.next_offset`。
- Provider 在事务内锁定或创建 ledger 行；ledger 落后于 batch start 表示位置缺口，必须失败；ledger 位于 batch 内或之后表示重放，必须跳过已应用记录。
- 对过滤后的同 key 多条 upsert，Provider 保留 position 最大的最后一条，再调用目标表 upsert；目标数据与 ledger end position 在同一事务提交。
- `skip` 只推进 ledger，不写入、更新或删除业务行；同批数据操作与 skip 必须仍按 source position 单调处理。插件只有真实实现该事务语义后才可在 capability `operations` 中声明 `skip`。
- 旧 runtime 即使仍持有业务库连接，只要其 batch end position 不大于 ledger，就不得覆盖新状态。

`BatchReadOptions.Hints`、`TableReadSessionOptions.Hints` 和 `BatchData.Hints` 只用于同一次运行时读写链路中的控制提示，例如字段选择、空间字段输出编码、批大小或写入方法。Hints 不是 catalog facts，不得写入 `CatalogFacts`，也不得作为 Meta item attributes 的兜底口袋。

`WriteOptions.UserMetadata` 只表达对象 / 文件写入时需要传给底层存储的用户自定义 metadata，例如 S3 / MinIO user metadata。它不是 engine catalog facts，也不用于表格读写控制；表格读写控制必须使用 Hints 或强类型 options 字段。

对象存储和文件系统不得互相继承，不共享 CatalogModel 或 catalog 拼装实现；二者最多共享内容流读写接口、MIME 推断、格式解析等底层 helper。

`OpenContent()`、`OpenRange()`、`CreateContent()` 等 store 能力接收的仍是**引擎自身 catalog model 下的 leaf `CatalogPath`**，不是另起一套只为底层 IO 服务的“物理路径 DTO”。调用方不得自行伪造脱离 `CatalogModelSpec` 的快捷路径；如果从物理路径、对象 key 或扫描候选重新定位 leaf，应使用 engine 公共层提供的路径构造规则，或直接复用 `CatalogEntry.Path`。物理路径可作为 Meta node/item attribute 暴露给底层实现，但不能替代统一 catalog path 契约。

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

普通查询是计算能力，不等于表格型存储目录能力。普通查询返回 `QueryResult`，面向表格化、记录集或标量结果消费方，例如 Develop 查询编辑器、Manager 表格预览和查询服务。

```go
type QueryRuntimeProvider interface {
    EnginePlugin
    QueryLanguages() []string
    GenerateSampleQuery(ctx context.Context, connInfo ConnectionInfo, opts SampleQueryOptions) (query string, language string)
    ExecuteRuntimeQuery(ctx context.Context, connInfo ConnectionInfo, req QueryRequest) (*QueryResult, error)
}
```

查询语言差异由 `QueryRequest.Language` 与 `capabilities.compute.query.languages` 表达，不按数据库类别新增查询入口。`QueryRuntimeProvider` 是普通查询主路径，适用于 SQL、MQL、Cypher 表格结果、OpenSearch DSL、Mango Query 等能返回 `QueryResult` 的查询。

`SQLQueryRuntimeProvider.ExecuteSQL()` 是 SQL 执行 helper 和 SQL dialect 适配层，当前仍可保留给 SQL 引擎和 batch read 适配使用；新增非 SQL 查询语言不得仿照它继续新增按数据库类别拆分的 provider。旧 `DocumentQueryRuntimeProvider` 已删除，不得恢复。

图查询不属于普通查询的一个返回格式变体。图查询使用独立 `GraphQueryProvider`，返回 `GraphQueryResult`，面向 Graph 模块、图可视化和图算法等需要节点 / 关系结构的调用方。Neo4j 可同时实现 `QueryRuntimeProvider` 和 `GraphQueryProvider`：前者用于普通 Cypher 表格结果和 Manager 预览兜底，后者用于图结构结果。图结构摘要由 `CatalogFactsProvider` 的 `CatalogFacts.Graph` 提供，图样本由 `GraphSampleProvider` 或 `GraphQueryProvider` 提供。

```go
type GraphQueryProvider interface {
    EnginePlugin
    ExecuteGraphQuery(ctx context.Context, connInfo ConnectionInfo, cypher string, opts QueryOptions) (*GraphQueryResult, error)
}
```

GORM、database/sql、Mongo driver、Neo4j driver、S3 client 都是实现 helper，不是领域主接口。

### Runtime Provider

工作流和脚本运行时使用独立 provider：

- `WorkflowRuntimeProvider`：工作流端点、算子描述发现、工作流执行。`ListOperators()` 返回 `OperatorDescriptor`，其中参数和输出端口分别使用 `ParameterDescriptor` / `OutputPortDescriptor`；这些结构只描述运行时算子接口，不是 Meta 模块元数据。
- `ScriptRuntimeProvider`：Notebook/脚本端点和会话。`ScriptSession.Info` 返回会话描述信息，例如 mode、language；它不是 catalog facts，也不是调用方 hints。

`ScriptRuntimeProvider` 的上层消费必须先从 System 获取目标 Engine Instance 的 Runtime Descriptor，再以该 Descriptor 构造的非敏感连接信息调用 `OpenSession()`。Develop Notebook 任务使用 `execution_config.engine_id` 保存实例绑定；上传、Kernel 发现和执行都使用同一个绑定实例，不接受执行时临时覆盖。

```go
type ScriptRuntimeProvider interface {
    EnginePlugin
    RuntimeEndpoint(ctx context.Context, connInfo ConnectionInfo) (string, error)
    OpenSession(ctx context.Context, connInfo ConnectionInfo, req ScriptSessionRequest) (*ScriptSession, error)
}
```

`ScriptSession.Endpoint` 是受控运行 API 的服务端调用地址，不是返回给浏览器的共享交互入口。调用方必须使用自身租户 Service Access Token 调用该端点。引擎健康检查由 System 负责；消费模块不得再维护固定运行时 URL 或专用健康代理。

---

## 四、接口组合

| 引擎 | 推荐接口组合 |
| --- | --- |
| PostgreSQL | 通用 tabular 组合 + `BoundedWatermarkReadProvider` + `TableUpsertProvider` + `PartitionedTableChangeApplyProvider` |
| MySQL / Doris / ClickHouse / Spark SQL | `EnginePlugin` + `CatalogModelProvider` + `CatalogProvider` + `CatalogFactsProvider` + `SQLQueryRuntimeProvider` + `ConnectionPoolPlugin` |
| MongoDB | `EnginePlugin` + `CatalogModelProvider` + `CatalogProvider` + `CatalogFactsProvider` + `DynamicSchemaSamplingProvider` + `QueryRuntimeProvider` |
| Neo4j | `EnginePlugin` + `CatalogModelProvider` + `CatalogProvider` + `CatalogFactsProvider` + `GraphSampleProvider` + `QueryRuntimeProvider` + `GraphQueryProvider` |
| MinIO / S3 | `EnginePlugin` + `CatalogModelProvider` + `CatalogProvider` + `CatalogFactsProvider` + `ContentReadableProvider` + `RangeReadableProvider` + `ContentWritableProvider` + `ResourceDeleteProvider` |
| NFS | `EnginePlugin` + `CatalogModelProvider` + `CatalogProvider` + `CatalogFactsProvider` + `ContentReadableProvider` + `RangeReadableProvider` + `ContentWritableProvider` + `ResourceDeleteProvider` |
| Kafka | `EnginePlugin` + `CatalogModelProvider` + `CatalogProvider` + `CatalogFactsProvider` + `ChangeStreamReaderProvider` |
| Python / Spark / Math Workflow | `EnginePlugin` + `WorkflowRuntimeProvider` |
| Jupyter | `EnginePlugin` + `ScriptRuntimeProvider` |

---

## 五、上层消费规则

- System：通过 `EnginePlugin` 做注册、连接测试、连接信息校验和能力声明刷新；通过 `CatalogProvider.ListChildren()` 对外提供实时 catalog 浏览控制面 API：`POST /api/v1/system/engines/:id/catalog/children`。
- Meta：使用 `CatalogProvider` 扫描目录并落库，使用 `CatalogFactsProvider` / `DynamicSchemaSamplingProvider` 获取 catalog leaf facts；扫描编排必须先读取 `CatalogModelSpec`，再结合 provider 组合选择 catalog scan strategy。`engine_family` 只能作为粗分类或展示字段，不能单独决定 namespace 术语、leaf 术语、扫描层级和内容读取方式。公开 API 应聚焦扫描后元数据快照，不再新增实时浏览公共接口。
- Meta 扫描 API 和任务参数中的路径型目标统一命名为 `catalog_paths`。它表示引擎 catalog model 下的路径。
- Manager：使用 Meta 树构建探查树；预览由 Manager 自身 preview provider / composer 组合完成。结构化数据优先消费 `BatchReadableProvider` 或只读 sample query；graph 预览优先消费 `type_info.graph` / `CatalogFactsProvider` 得到 schema 视图，并通过 `GraphSampleProvider` 或 `GraphQueryProvider` 获取轻量子图样本；对象/文件优先消费 `ContentReadableProvider` 并结合格式解析。
- Develop：使用 `QueryRuntimeProvider`、`WorkflowRuntimeProvider`、`ScriptRuntimeProvider`；Notebook 引擎实例通过 `execution_config.engine_id` 绑定，并以 System Runtime Descriptor + `ScriptRuntimeProvider.OpenSession()` 解析端点；图结构展示入口使用 `CatalogFactsProvider` / `GraphQueryProvider`。
- Service：发布普通查询服务时使用 query runtime 和 Meta item/spatial 元数据；图查询服务使用 `GraphQueryProvider`。图查询服务的易用向导应消费 graph item 的 `type_info.graph.node_shapes`，不得再从 Meta 树读取 Neo4j label item。
- Transfer：使用 source / target endpoint 生成执行计划。snapshot native table 读写消费 `TableReadSessionProvider`、`BatchReadableProvider`、`TableWritePreparer`、`TableWriteSessionProvider`、`BatchWritableProvider`；watermark bounded incremental 必须消费 `BoundedWatermarkReadProvider` 和幂等 `TableUpsertProvider`；encoded file/object 读写先通过 engine content provider 和 `common/engine/contentadapter` 构造 `common/contentio` 抽象，再交给 `common/format` provider。高吞吐数据搬运优先消费 batch / table session / content stream 能力，而不是 query runtime。
- Transfer continuous runtime：业务 Kafka source 必须消费 `ChangeStreamReaderProvider`，由 Transfer adapter 生成 ChangeEvent 并通过 ChangeApplyWriter 组合目标 Provider。第一版目标只允许 PostgreSQL `PartitionedTableChangeApplyProvider`；Infra Kafka 连接来自 ADDP infra 配置，不注册 System Engine，但复用同一 Kafka reader/client 底层实现。

---

## 六、禁止事项

- 不得在上层模块直接依赖旧 `ListXXX` 接口。
- 不得把所有目录层级统一硬编码为 schema/table。
- 不得把 Kafka topic / partition 伪装为 table batch、对象内容流或固定 partition ResourceLocator。
- 不得把对象存储当作 POSIX 文件系统建模。
- 不得让插件返回 JSON 字符串形式的 capabilities。
- 不得让非 DSN 引擎返回 JSON 字符串冒充 connection string。
- 不得在 capabilities 中保存任务级运行参数。
- 不得在 `CatalogProvider` / `CatalogFactsProvider` 中执行写入、DDL、统计刷新等有外部副作用的操作；连接测试也必须保持只读。

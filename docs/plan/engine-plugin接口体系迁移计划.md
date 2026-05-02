# Engine Plugin 接口体系迁移计划

更新时间：2026-05-02 23:14 CST

## 背景与原则

本轮迁移以 `docs/concepts/addp引擎插件接口体系规范草案.md` 和 `docs/concepts/addp引擎能力声明规范草案.md` 为准，目标是把 ADDP 内部各类数据库、对象存储、文件系统、文档库、图库、工作流和脚本引擎纳入统一 engine plugin 体系。

用户已明确确认：

- ADDP 当前不需要保持旧 API、旧数据和旧能力声明的兼容性。
- 旧代码、旧数据可以直接清理。
- 当前代码暂不提交 git。

本轮迁移的核心原则：

- `EnginePlugin` 只负责注册、连接测试、连接信息校验、能力声明。
- 真实目录层次由 `CatalogProvider` 提供。
- 叶子数据项元数据由 `ItemMetadataProvider` 提供。
- 数据读取/写入能力由 `StoreProvider`、`ContentReadableProvider` 等表达。
- 查询计算能力由 `QueryRuntimeProvider` 及其子接口表达。
- 上层模块不再直接依赖各类旧 `ListXXX` 接口。

## 当前已完成

### 能力声明

- `EnginePlugin` 已改为结构化 `Capabilities() EngineCapabilities`。
- 旧 `GenerateCapabilities() string` 已从插件接口和插件实现中删除。
- 旧 capabilities JSON 不再兼容，系统和过滤工具只接受 `engine.capabilities/v1`。
- 新增能力声明相关文件：
  - `common/engine/plugin/capabilities.go`
  - `common/engine/plugin/capability_builders.go`
  - `common/utils/capability_filter_test.go`

### Provider 抽象

新增 provider 文件：

- `common/engine/plugin/providers.go`
- `common/engine/plugin/sql_runtime.go`
- `common/engine/plugin/tabular_catalog.go`
- `common/engine/plugin/filesystem_catalog.go`
- `common/engine/plugin/nosql_catalog.go`

已覆盖的 provider：

- `CatalogModelProvider`
- `CatalogProvider`
- `ItemMetadataProvider`
- `StoreProvider`
- `ContentReadableProvider`
- `SQLQueryRuntimeProvider`
- `DocumentQueryRuntimeProvider`
- `GraphQueryRuntimeProvider`
- `WorkflowRuntimeProvider`
- `ScriptRuntimeProvider`

### 已覆盖插件

所有现有插件均已补齐 `Capabilities()`。

已覆盖 catalog 和 item metadata：

- Tabular：PostgreSQL、MySQL、Doris、ClickHouse、Spark SQL
- Object/File：MinIO、S3、NFS
- Document/Graph：MongoDB、Neo4j

已覆盖 query runtime：

- SQL：PostgreSQL、MySQL、Doris、ClickHouse、Spark SQL
- Document：MongoDB
- Graph：Neo4j

已迁移 workflow/script runtime：

- Python Workflow
- Spark Workflow
- Math Workflow
- Jupyter

### 已删除或弱化的旧接口

已从接口体系中删除或不再作为上层依赖：

- `StoragePlugin.SupportsMetadataQuery()`
- `ComputePlugin.GetSupportedOperators()`
- `ComputePlugin.HealthCheckEndpoint()`
- `QueryablePlugin`
- `GraphQueryPlugin`

注意：`common/engine/plugin/factory.go` 中的 `SupportsMetadataQuery(engineType)` 仍是工具函数，但内部已读结构化能力声明，不再是插件接口方法。

### dbbridge / develop / manager 部分迁移

- `dbbridge.ExecuteQuery` 已优先使用新 query runtime provider：
  - 非 SQL query runtime 走 `ExecuteRuntimeQuery`
  - Spark 保留 Thrift 专用路径
  - SQL runtime 走 `ExecuteSQL`
- `GraphQueryRuntimeProvider` 方法名为 `ExecuteRuntimeGraphQuery(...)`，避免和旧图查询方法冲突。
- `QueryOptions` 已包含 `EngineID`、`EngineType`，SQL runtime 可复用 engine 级连接池。
- manager filesystem preview 内容读取已优先使用 `ContentReadableProvider.OpenContent`。
- manager Neo4j 图预览已改用 `GraphQueryRuntimeProvider.ExecuteRuntimeGraphQuery`。

### meta 消费端迁移

已完成的主要方向：

- `ResourceDiscoveryService` 已收敛到 `CatalogProvider`：
  - `ListAvailableSchemas` 走 `CatalogProvider.ListChildren`
  - `ListObjectStorageNodes` 走 `CatalogProvider.ListChildren`
  - 旧 `RelationalDBPlugin`、`NoSQLPlugin`、`ObjectStoragePlugin`、`FileSystemPlugin` fallback 已删除
- `DatabaseScanService` 不再依赖 `RelationalDBPlugin.ListSchemas/ListTables/ListColumns`：
  - schema/table 列表走 `CatalogProvider`
  - 字段描述走 `ItemMetadataProvider`
  - 连接池仅作为增强能力，用于主键约束名和 PostgreSQL 空间元数据
- `ObjectStorageScanService` 已不再依赖 `ObjectStoragePlugin.ListBuckets/ListObjects`：
  - bucket/object 列表走 `CatalogProvider`
  - `CatalogProvider.ListChildren(..., ListOptions{Recursive:true})` 已支持递归列对象
- `NoSQLScanService` 已不再依赖 `NoSQLPlugin.ListDatabases`、`DocumentDBPlugin.ListCollections`、`GraphDBPlugin.ListNodeLabels/ListRelationshipTypes`：
  - database、collection、label、relationship 列表走 `CatalogProvider`
  - MongoDB 深度 schema 推断仍临时复用 `DocumentDBPlugin.CreateClient`
- `FileSystemScanService` 列根、列目录已走 `CatalogProvider`：
  - `FileSystemPlugin` 暂时仅用于 detector 读取文件内容，如 Parquet schema 提取

### Object/File catalog 语义修正

已修正对象存储和文件系统的 catalog item kind：

- 对象存储叶子：`term=object`、`kind=object`
- 文件系统叶子：`term=file`、`kind=file`

避免 MinIO/S3 对象在上层被误识别为普通文件。

## 当前验证状态

最近通过：

```bash
go test ./common/engine/plugin ./common/engine/plugins/... ./common/utils ./common/models ./common/client ./common/dbbridge ./system/backend/internal/... ./develop/backend/internal/service ./develop/backend/internal/api ./meta/backend/internal/service
git diff --check
```

另一次局部验证：

```bash
go test ./common/engine/plugin ./common/dbbridge ./system/backend/internal/service
```

已知既有失败：

- `go test ./manager/backend/internal/service -run '^$'` 失败：
  - `metadata_service_test.go` 引用已不存在的 `repository.EngineRepository`
  - `repository.NewEngineRepository`
  - `MetadataService.ListExplorerResources`
  - `MetadataService.GetResourceTree`
- 这属于 manager 既有旧测试问题，不是本轮 engine provider 迁移新引入。

## 当前工作树状态提示

当前代码有大量未提交改动，均与 engine plugin 迁移相关。新会话继续时不要回滚。

重要未跟踪新增文件：

- `common/engine/plugin/capabilities.go`
- `common/engine/plugin/capability_builders.go`
- `common/engine/plugin/filesystem_catalog.go`
- `common/engine/plugin/nosql_catalog.go`
- `common/engine/plugin/providers.go`
- `common/engine/plugin/sql_runtime.go`
- `common/engine/plugin/tabular_catalog.go`
- `common/utils/capability_filter_test.go`

## 下一步建议

### 1. 收口旧元数据工具链

优先处理：

- `common/engine/plugin/factory.go`
- `common/dbbridge/bridge.go`
- `system/backend/internal/service/storage_engine_service.go`
- `manager/backend/internal/service/preview_provider_database.go`

目标：

- 保留旧函数签名也可以，但内部应走：
  - `CatalogProvider.ListChildren`
  - `ItemMetadataProvider.DescribeItem`
- 不再向上层扩散 `RelationalDBPlugin.ListSchemas/ListTables/ListColumns`。

当前进展：

- `common/engine/plugin/factory.go` 已开始改造 `ListSchemas/ListTables/ListColumns`，但新会话应先重新跑测试确认无遗漏。
- `GetTableRowCount` 仍走 `RelationalDBPlugin.GetTableRowCount`，可暂时保留为 SQL/tabular 增强能力，后续再考虑并入 `ItemMetadataProvider` stats 或 SQL runtime。

### 2. manager preview 继续迁移

重点文件：

- `manager/backend/internal/service/preview_provider_database.go`
- `manager/backend/internal/service/preview_provider_filesystem.go`
- `manager/backend/internal/service/preview_provider_lake_table.go`
- `manager/backend/internal/service/preview_provider_doc_collection.go`

建议目标：

- 表字段：走 `ItemMetadataProvider` 或 meta 已扫描元数据。
- 文件/目录浏览：走 `CatalogProvider`。
- 文件内容读取：走 `ContentReadableProvider`。
- 湖表预览当前仍依赖 `FileSystemPlugin` 和 parquet helper，可后续抽 `LakeTablePreviewProvider` 所需的内容读取 provider。
- 文档集合预览当前仍依赖 `NoSQLPlugin.CreateClient`，建议后续抽成更明确的 document preview/runtime provider，而不是继续挂在 NoSQLPlugin 上。

### 3. 重新定义旧叶子接口边界

当前旧接口仍存在于 `common/engine/plugin/interfaces.go`，下一步应按职责重新瘦身：

#### RelationalDBPlugin

建议最终只保留：

- `StoragePlugin`
- `ConnectionPoolPlugin`
- SQL/tabular 专项增强能力，如 `GetTableRowCount` 可以临时保留
- 不再保留 `ListSchemas/ListTables/ListColumns`

#### ObjectStoragePlugin

建议拆除对 `FileSystemPlugin` 的继承。

最终对象存储列表能力走：

- `CatalogProvider`

对象内容读写走：

- `ContentReadableProvider`
- `ContentWritableProvider`
- 后续可能补 `RangeReadableProvider` 或扩展 `ReadOptions`

对象存储专属语义应通过：

- `StoreProvider.StoreSemantics()`
- `Capabilities().Storage.Semantics`

#### FileSystemPlugin

当前仍被 detector/parquet preview 使用。

建议下一步拆成更明确的小接口：

- `ContentReadableProvider`
- `CatalogProvider`
- 可能新增 `DirectoryReadableProvider`，如果目录枚举仍需要独立于 catalog 的底层读能力

最终不要让上层模块直接消费 `ListRoots/ListDirectory`。

#### NoSQLPlugin / DocumentDBPlugin / GraphDBPlugin

列表职责应全部迁到 catalog。

当前仍可能保留：

- `CreateClient/CloseClient`，用于 MongoDB parser/preview
- 图数据库 schema 细节能力，后续考虑转入 `ItemMetadataProvider` 或 graph-specific metadata provider

建议新增或细化：

- `DocumentClientProvider`
- `DocumentPreviewProvider`
- `GraphSchemaProvider` 或将图 schema 完整纳入 `ItemMetadataProvider`

### 4. 清理旧 helper 和测试

迁移完成后再清理：

- `common/dbbridge.ListSchemas/ListTables/ListColumns` 是否仍需要保留
- `common/engine/plugin/factory.go` 旧元数据函数命名
- `common/engine/plugin/integration/verify_test.go` 中对旧插件接口的强断言
- `docs/spec/addp数据引擎扩展指南.md` 中旧接口说明
- `docs/concepts/addp引擎体系图.md` 中旧 UML
- manager 旧测试 `metadata_service_test.go`

### 5. 最后再删除旧接口方法

建议删除顺序：

1. 确认所有消费端不再调用旧 `ListXXX`。
2. 更新插件测试，不再要求旧接口。
3. 从插件实现中删除旧方法。
4. 从 `interfaces.go` 删除旧方法。
5. 全量跑测试。

重点删除候选：

- `ObjectStoragePlugin.ListBuckets`
- `ObjectStoragePlugin.ListObjects`
- `RelationalDBPlugin.ListSchemas`
- `RelationalDBPlugin.ListTables`
- `RelationalDBPlugin.ListColumns`
- `NoSQLPlugin.ListDatabases`
- `DocumentDBPlugin.ListCollections`
- `GraphDBPlugin.ListNodeLabels`
- `GraphDBPlugin.ListRelationshipTypes`
- `FileSystemPlugin.ListRoots`
- `FileSystemPlugin.ListDirectory`

其中 `FileSystemPlugin.ReadFile` 应迁移到 `ContentReadableProvider.OpenContent` 后再删。

## 新会话推荐起点

建议新会话第一步执行：

```bash
git status --short
go test ./common/engine/plugin ./common/dbbridge ./system/backend/internal/service ./meta/backend/internal/service
rg -n "RelationalDBPlugin|ObjectStoragePlugin|FileSystemPlugin|NoSQLPlugin|ListSchemas|ListTables|ListColumns|ListBuckets|ListObjects|ListRoots|ListDirectory|ListDatabases|ListCollections|ListNodeLabels|ListRelationshipTypes" common system manager meta develop -S
```

然后优先继续：

1. 完成 `common/engine/plugin/factory.go` 与 `common/dbbridge/bridge.go` 的旧元数据函数 provider 化。
2. 改 `manager/backend/internal/service/preview_provider_database.go`，避免 `dbbridge.ListColumns` 继续走旧关系型接口。
3. 改 `manager/backend/internal/service/preview_provider_filesystem.go`，目录列举走 catalog，内容读取走 `ContentReadableProvider`。

## 注意事项

- 不要提交代码，除非用户明确授权。
- 不要回滚当前工作树。
- 当前文档是迁移计划，不是最终规范；最终规范仍应落在 `docs/spec/` 或已存在的 `docs/concepts/` 草案中。
- 清理旧接口时可以大胆删除，不需要兼容旧 API 和旧数据。

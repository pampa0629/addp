# Engine Plugin 接口体系迁移计划

更新时间：2026-05-03 18:34 CST

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

## 新会话执行原则：先保证功能跑通

用户最新要求：

- 暂时不要继续铺开历史文档清理。
- 下一阶段优先保证现有功能能正确启动、扫描、探查、预览、查询和选择数据源。
- 原有代码、旧 API、旧数据结构、已有元数据可以修改或删除，不需要为了兼容保留旧逻辑。
- 如果现有元数据、旧 capabilities JSON、旧 scan 结果、旧 catalog 节点语义阻碍功能跑通，可以直接清理或重扫。

下一会话应按“功能可运行”优先级推进：

1. 先跑服务和关键页面/接口 smoke test，确认真实用户路径可用。
2. 发现旧 API、旧数据结构、旧元数据阻塞时，直接删除或迁移，不做兼容包装。
3. 只在必要时更新当前计划文档记录决策；不要先投入大规模历史文档修订。

## 当前已完成

以下是截至 2026-05-03 18:34 CST 的迁移状态总览；后续执行记录保留在下一节。

- [x] 能力声明体系已切到 `engine.capabilities/v1`：插件统一实现结构化 `Capabilities()`，System 启动刷新旧 capabilities，旧 `GenerateCapabilities() string` 不再作为插件契约。
- [x] Provider 抽象已建立：`CatalogProvider`、`ItemMetadataProvider`、`StoreProvider`、`ContentReadableProvider`、`SQLQueryRuntimeProvider`、`DocumentQueryRuntimeProvider`、`GraphQueryRuntimeProvider`、workflow/script runtime provider 已落地。
- [x] 现有插件已完成 provider 覆盖：关系型、对象存储、文件系统、MongoDB、Neo4j、Spark SQL、workflow/script 插件均已补齐当前迁移所需能力。
- [x] 旧 `ListXXX` 上层接口依赖已收口：`RelationalDBPlugin.ListSchemas/ListTables/ListColumns`、`ObjectStoragePlugin`、`FileSystemPlugin`、`NoSQLPlugin` 等不再作为公共上层契约暴露。
- [x] `common/engine/plugin` 与 `common/dbbridge` 已切到中性 facade：`ListNamespaces`、`ListItems`、`DescribeItem`、`DescribeNamedItem`、`CountItemRows`，旧 `ListSchemas/ListTables/ListColumns/GetTableRowCount` facade 已删除。
- [x] System/Develop/Copilot 数据源选择链路已切到 catalog 语义：`namespaces/items` 替代 `schemas/tables`，Develop 后端会透传用户 JWT 调用 System catalog API。
- [x] Meta 扫描服务已迁移到 provider：关系型、对象存储、文件系统、MongoDB、Neo4j 的扫描主链路走 `CatalogProvider` / `ItemMetadataProvider` / `ContentReadableProvider`。
- [x] Manager 预览主链路已迁移到 provider：数据库表、文件系统、对象存储、湖表、MongoDB collection、Neo4j label/relationship 预览不再依赖旧插件叶子接口。
- [x] ResourceLocator 类型语义已修正：对象存储叶子为 `object`、文件系统叶子为 `file`、Neo4j label/relationship 分别为 `label` / `relationship`，不再折叠为 `collection`。
- [x] 迁移相关规范已同步：`docs/spec/addp数据引擎扩展指南.md`、`docs/concepts/addp引擎体系图.md`、路径与指纹相关规范已更新到 provider/locator 目标语义。
- [x] 关键测试与构建已有通过记录：Go 单测、plugin integration 局部测试、Develop/Manager 前端构建、Python compileall、`git diff --check` 均已有阶段性通过记录。
- [x] Neo4j Manager 真实链路已验证：`Project` 树节点 locator 为 `type=label`，使用该 locator 预览返回 `preview_type=table`、7 列、8 行。
- [x] Meta 旧命名 API 已删除：`/engines/:id/schemas`、`/engines/:id/schemas/available`、`/metadata/tables`、`/metadata/fields`、`/metadata/tables/spatial` 不再保留。
- [x] Meta 新查询 API 已收口：已扫描元数据使用 `/engines/:id/items`、`/engines/:id/items/fields`、`/engines/:id/items/spatial`、`/items/:id/fields`、`/items/:id/spatial`；实时目录发现使用 System `/engines/:id/namespaces`。
- [x] 扫描请求参数已从 `schema_names` 切到 `namespaces`；对象存储/文件系统选择性扫描只认 `object_paths`，不再把旧字段作为 fallback。
- [x] Console、Meta、Manager、Transfer、Graph、Service 与 `common-frontend/basic` 已同步迁移到新 API。

## 当前剩余重点

- [x] 重新启动服务后验证最新 Neo4j 树节点：`Project` locator 已为 `type=label`，真实树节点预览已返回表格数据。
- [ ] 复核完整 Manager 探查树：Meta、Manager 同时启动时，MySQL、MinIO/S3、NFS、MongoDB、Neo4j 都应能展开到正确叶子类型并可预览。
- [x] 收敛 MinIO/S3 选择性扫描语义：对象存储/文件系统手动扫描使用 `object_paths`；旧 `schema_names` fallback 已删除。
- [x] 验证 Service/Transfer/Graph/Console 等下游模块对 Meta 旧命名 API 的调用是否仍能跑通；语义冲突处已直接替换为 catalog/item API。
- [ ] 功能稳定后再做历史文档清理：阶段性 plan、模块旧说明、Swagger/API 命名说明统一收口。

## 2026-05-03 功能跑通执行记录

本轮执行目标：按“先保证功能跑通”的原则，优先验证真实运行链路，发现旧结构或旧数据阻塞时直接刷新、清理或迁移，不保留旧兼容。

### 2026-05-03 18:34 继续执行记录

本轮按用户“保留最新、符合规范的 API，旧的直接删除；前端同步修改正确”的要求，删除 Meta 旧查询入口并同步后端/前端调用方。

已处理：

- Meta 后端删除旧查询路由：
  - `GET /api/v1/meta/engines/:engine_id/schemas`
  - `GET /api/v1/meta/engines/:engine_id/schemas/available`
  - `GET /api/v1/meta/metadata/tables`
  - `GET /api/v1/meta/metadata/fields`
  - `GET /api/v1/meta/metadata/tables/spatial`
- Meta 后端新增/保留中性查询路由：
  - `GET /api/v1/meta/engines/:engine_id/items?namespace=...`
  - `GET /api/v1/meta/engines/:engine_id/items/fields?namespace=...&name=...`
  - `GET /api/v1/meta/engines/:engine_id/items/spatial?namespace=...&name=...`
  - `GET /api/v1/meta/items/:item_id/fields`
  - `GET /api/v1/meta/items/:item_id/spatial`
- `common/client.MetaClient` 删除旧 `GetSchemas/GetTables/GetTableFields/GetTableSpatialMetadata` 方法，改为 `ListItems/GetItemFields/GetItemSpatialMetadata`。
- Manager、Service、Quality、Transfer 后端调用方已改用新 Meta client 方法。
- Console、Meta、Manager、Transfer、Graph、Service 前端与 `common-frontend/basic` 已同步替换旧 URL：
  - 实时命名空间从 System `/system/engines/:id/namespaces` 获取。
  - 已扫描 item/field/spatial 元数据从 Meta `/meta/engines/:id/items...` 获取。
- 扫描任务和扫描请求的 JSON 字段从 `schema_names` 改为 `namespaces`。
- 对象存储扫描分支删除旧 `schema_names` fallback；对象存储/文件系统选择性扫描只使用 `object_paths`。

已验证：

- `go test ./meta/backend/internal/service ./meta/backend/internal/api ./common/client ./common/models ./manager/backend/internal/service ./manager/backend/internal/worker ./service/backend/internal/api ./service/backend/internal/service ./quality/backend/internal/service`
- `npm run build --prefix meta/frontend`
- `npm run build --prefix transfer/frontend`
- `npm run build --prefix service/frontend`
- `npm run build --prefix graph/frontend`
- `npm run build --prefix console/frontend`
- `npm run build --prefix manager/frontend`
- `git diff --check`

注意：

- `go test ./transfer/backend/internal/service` 当前仍因既有测试引用不存在的 `models.TaskMode/TaskModeStream/TaskModeMicroBatch/TaskModeBatch` 编译失败；本轮涉及的 Transfer 后端代码已通过其他包编译检查覆盖到 `common/client` 调用面，但该包测试需另行修复。

### 2026-05-03 17:47 继续执行记录

本轮继续处理剩余重点：真实验证 Neo4j Manager 探查/预览闭环，并收敛 MinIO/S3 选择性扫描语义。

已处理：

- 在真实服务中重启 System、Meta、Manager 后验证 Neo4j：
  - Manager 引擎列表中 Neo4j 为 `Business Neo4j`，engine_id 为 `25`。
  - `Project` 树节点 locator 已为 `addp://engine/25/path/neo4j/Project?type=label&meta_id=100578`。
  - 使用该真实 locator 请求 Manager preview，返回 `preview_type=table`、7 列、8 行。
- Meta 对象存储扫描分支已修正选择性扫描语义：
  - `object_paths` 仍优先作为对象存储扫描目标。
  - 当时曾临时允许旧 `schema_names` fallback；该过渡逻辑已在 18:34 记录中删除，当前对象存储/文件系统只接受 `object_paths`。

已验证：

- `go test ./meta/backend/internal/service ./common/resource ./manager/backend/internal/service`
- `go test ./meta/backend/internal/service ./common/resource ./manager/backend/internal/service ./manager/backend/...`
- `go test ./common/engine/plugin ./common/engine/plugins/... ./common/utils ./common/models ./common/client ./common/dbbridge ./common/format/db ./common/format/parquet ./system/backend/internal/... ./develop/backend/internal/service ./develop/backend/internal/api ./meta/backend/internal/service`
- `git diff --check`

### 2026-05-03 17:27 继续执行记录

本轮继续排查 Manager 数据探查中 Neo4j `Project` 节点预览为空的问题，根因是 Meta 树到 ResourceLocator 的类型转换把 Neo4j `label/relationship` 折叠成了 `collection`，导致真实树节点 URI 形如 `type=collection`，无法命中 Neo4j 图预览 provider。

已处理：

- `common/resource` 新增 `TypeLabel` 与 `TypeRelationship`，TreeBuilder 对 Meta `label/relationship` 节点保持原类型输出。
- Neo4j 树节点 locator 目标语义调整为：
  - label：`addp://engine/{id}/path/neo4j/Project?type=label&meta_id=...`
  - relationship：`addp://engine/{id}/path/neo4j/WORKS_FOR?type=relationship&meta_id=...`
- 同步修订路径规范文档，明确 MongoDB collection、Neo4j label、Neo4j relationship 三类 locator type 不得混用。

已验证：

- `go test ./common/resource ./manager/backend/internal/service ./manager/backend/...`
- `npm run build --prefix manager/frontend`
- `git diff --check`

### 2026-05-03 17:07 继续执行记录

本轮继续按 smoke test 优先推进，重点补齐 Manager 预览链路中 provider 迁移后的直接 URI 场景。

已处理：

- Manager Backend 已导入 `common/engine/plugins/nfs`，NFS 文件系统引擎可以在 Manager 预览链路中通过 `plugin.Get("nfs")` 找到 provider。
- Manager `PreviewResolver` 会把 `ResourceLocator` 的 `type` 作为直接预览请求的 `ItemType` 兜底来源，避免无 `meta_id` 时 Neo4j `label/relationship`、湖表等 provider 无法命中。
- Manager filesystem preview 修正根目录文件判定：`addp://engine/26/path/README.md?type=file` 这类 URI 不再因转换后 `filePath==""` 被误判为目录。
- Manager object preview 修正无 MetaItem/MetaNode 命中时的 node type 默认值：会优先使用 locator `type=object`，避免 MinIO/S3 直接对象 URI 被当作目录节点预览。
- Manager worker 中 3 处 `fmt.Errorf(errMsg)` 改为 `errors.New(errMsg)`，消除 Go vet 对非常量格式串的阻塞，使 `go test ./manager/backend/...` 可作为门禁运行。

已验证：

- `go test ./manager/backend/...`
- `go test ./common/engine/plugin ./common/engine/plugins/... ./common/utils ./common/models ./common/client ./common/dbbridge ./common/format/db ./common/format/parquet ./system/backend/internal/... ./develop/backend/internal/service ./develop/backend/internal/api ./meta/backend/internal/service ./manager/backend/internal/service`
- `git diff --check`
- 同一 shell 内执行 `./scripts/dev/start.sh` 后 smoke：
  - MySQL `business.customers` 预览：`preview_type=table`、12 列、3 行。
  - MinIO `manager/test.parquet` 预览：`preview_type=object`、`object_kind=table`。
  - NFS `README.md` 预览：`preview_type=object`、`object_kind=markdown`，可返回文本样例。
  - Neo4j `Person` label 预览：`preview_type=table`、7 列、20 行。

注意：

- 在当前工具会话中，启动脚本退出后后台服务仍可能被工具清理；真实接口 smoke 需要像本轮一样在同一 shell 内启动并立即请求，或由用户在本机终端手动启动后用浏览器验证。

已处理：

- System 启动时会刷新所有 `system.engines.capabilities` 为 `engine.capabilities/v1`，旧 capabilities 结构不再保留。
- System 内部自注册入口不再接受旧 capabilities，由服务端按当前插件重新生成能力声明。
- Meta 引擎列表已验证能通过新能力声明识别存储引擎。
- System 前端引擎列表已从旧 `storage[]/compute[]` 解析切换到新 `storage.families`、`compute.query/workflow/script`，默认显示内置和非内置全部引擎。
- Copilot Backend 启动失败根因已处理：全局 `DEBUG=release` 会被 Pydantic Settings 映射到 `debug: bool`，导致 `restart -all` 在 Copilot 健康检查处失败；Copilot 现在将 `release/prod/production` 解析为 `debug=false`。
- Develop catalog 选择器接口已修复：`/develop/engines/:id/namespaces`、`/develop/engines/:id/items` 会透传当前用户 JWT 调用 System catalog API，避免 System 返回“缺少认证令牌”。

已验证：

- `./scripts/dev/restart.sh -all` 可完成全量启动，System、Meta、Manager、Develop、Gateway、Copilot、Agent、各前端均启动成功。
- System 引擎列表返回 10 条，全部为 `engine.capabilities/v1`：
  - 4 条内置扩展计算/脚本引擎：Python Workflow、Math Workflow、Spark Workflow、Jupyter。
  - 6 条标准存储引擎：PostgreSQL、MySQL、MinIO/S3、NFS、MongoDB、Neo4j。
- Meta `/api/v1/meta/engines` 返回 6 条存储引擎。
- System catalog API 六类代表引擎均可用：
  - PostgreSQL：4 个 namespace，`public` 下 24 个 item。
  - MySQL：`business` namespace，5 个 table。
  - MinIO/S3：3 个 bucket。
  - NFS：`.` root 下可列文件。
  - MongoDB：2 个 database，collection 可列。
  - Neo4j：`neo4j` database 下 label/relationship 可列。
- Meta 旧命名查询接口当时仍可用；已在 18:34 记录中删除并替换为 `items/namespaces` 语义。
- Meta 扫描已验证：
  - MySQL `business`：1 schema、5 tables、45 fields。
  - MongoDB `business`：1 database、5 collections、31 fields/sample fields。
  - Neo4j `neo4j`：1 database、13 labels/relationships。
  - MinIO/S3、NFS 基础扫描可完成。
- Manager 已验证：
  - `/manager/engines` 返回 6 条存储引擎。
  - 在 Meta 同时启动时，`/manager/tree/:engine_id` 可从 Meta 树构建 MySQL 资源树。
  - `/manager/preview` 可预览 MySQL `business.customers`，返回 columns、field metadata、rows。
- Develop 已验证：
  - `/develop/engines` 返回 5 条查询引擎（含 DuckDB 虚拟引擎）。
  - `/develop/engines/27/namespaces` 返回 `business`。
  - `/develop/engines/27/items?namespace=business` 返回 5 张表。

本轮下一步验证顺序：

1. 重启/确认 System、Meta、Manager、Develop 等关键服务。
2. 验证 System 引擎列表和 catalog API：`/engines/:id/namespaces`、`/engines/:id/items`。
3. 验证 Meta 引擎发现、扫描入口和各类 catalog 语义。
4. 验证 Manager 探查/预览和 Develop 数据源选择/查询。
5. 如遇旧 Meta 扫描结果或旧 catalog kind 阻塞，直接清理后重扫。

本轮已知待跟进：

- `start.sh -manager` 只启动 Manager + System，不启动 Meta；此时 Manager tree 会降级为空根节点，预览仍可实时工作。完整探查树验证需要先启动 Meta 或使用全量启动。
- MinIO/S3 手动扫描旧 `schema_names` 问题已在 18:34 收口：对象存储/文件系统选择性扫描统一使用 `object_paths`。
- 若通过当前工具执行启动脚本，脚本结束后后台进程可能被工具会话清理；接口 smoke 需在同一 shell 内启动并请求，用户本地浏览器手动验证不受此工具限制。

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
- `RelationalDBPlugin.ListSchemas/ListTables/ListColumns` 已从公共接口移除，现仅作为 `tabular_catalog.go` 内部 adapter 依赖。
- `RelationalDBPlugin.GetTableRowCount/SchemaNodeType` 已从公共接口移除，行数优先来自 `ItemMetadataProvider` stats，缺失时走 `SQLQueryRuntimeProvider`。
- `ObjectStoragePlugin` 与 `FileSystemPlugin` 已从公共接口体系移除，目录、元数据、内容能力由 provider 表达。
- `NoSQLPlugin` 已从公共接口体系移除，`DocumentDBPlugin` / `GraphDBPlugin` 只保留专项增强能力。
- `DocumentDBPlugin.ListCollections`、`GraphDBPlugin.ListNodeLabels/ListRelationshipTypes` 已从公共接口移除，现仅作为插件内部 helper 通过 `nosql_catalog.go` adapter 注入。
- 具体 SQL、对象存储、文件系统、NoSQL/Graph 插件上不再导出旧 `ListXXX` helper。

注意：`common/engine/plugin/factory.go` 中的 `SupportsMetadataQuery(engineType)` 仍是工具函数，但内部已读结构化能力声明，不再是插件接口方法。

### dbbridge / develop / manager 部分迁移

- `dbbridge.ExecuteQuery` 已优先使用新 query runtime provider：
  - 非 SQL query runtime 走 `ExecuteRuntimeQuery`
  - Spark 保留 Thrift 专用路径
  - SQL runtime 走 `ExecuteSQL`
- `GraphQueryRuntimeProvider` 方法名为 `ExecuteRuntimeGraphQuery(...)`，避免和旧图查询方法冲突。
- `QueryOptions` 已包含 `EngineID`、`EngineType`，SQL runtime 可复用 engine 级连接池。
- manager filesystem preview 内容读取已优先使用 `ContentReadableProvider.OpenContent`。
- manager filesystem preview 目录浏览已使用 `CatalogProvider.ListChildren`，文件 stat 使用 `ItemMetadataProvider.DescribeItem`。
- manager lake table preview 已使用 `CatalogProvider + ContentReadableProvider` 读取 Parquet 预览，不再依赖 `FileSystemPlugin`。
- manager document collection preview 已改用 `DocumentQueryRuntimeProvider.ExecuteDocumentQuery` 读取样本，集合统计优先用 `ItemMetadataProvider.DescribeItem`，不再直接创建 `NoSQLPlugin` 客户端。
- manager database table preview 已改用 `SQLQueryRuntimeProvider.ExecuteSQL`，字段走 `ItemMetadataProvider`，行数优先 metadata stats，缺失时通过 SQL runtime 查询。
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
  - MongoDB 深度 schema 推断走 `DocumentMetadataSamplingProvider.SampleDocumentMetadata`
- `FileSystemScanService` 列根、列目录已走 `CatalogProvider`：
  - detector 和 Parquet schema 提取通过 `ContentReadableProvider` 读取内容，不再依赖 `FileSystemPlugin`

### Object/File catalog 语义修正

已修正对象存储和文件系统的 catalog item kind：

- 对象存储叶子：`term=object`、`kind=object`
- 文件系统叶子：`term=file`、`kind=file`

避免 MinIO/S3 对象在上层被误识别为普通文件。

## 当前验证状态

最近通过：

```bash
go test ./common/engine/plugin ./common/engine/plugins/... ./common/utils ./common/models ./common/client ./common/dbbridge ./system/backend/internal/... ./develop/backend/internal/service ./develop/backend/internal/api ./meta/backend/internal/service
go test ./common/engine/plugin ./common/engine/plugins/... ./common/utils ./common/models ./common/client ./common/dbbridge ./common/format/db ./common/format/parquet ./system/backend/internal/... ./develop/backend/internal/service ./develop/backend/internal/api ./meta/backend/internal/service
go test -tags integration ./common/engine/plugin/integration -run '^TestPluginInterfaceImplementation|TestRelationalDBPlugins|TestObjectStoragePlugins$'
git diff --check
```

另一次局部验证：

```bash
go test ./manager/backend/internal/service
go test ./manager/backend/internal/service -run 'TestExplorerEngineListTenantFiltering|TestGetResourceTreeDeniedForOtherTenant' -v
```

`manager/backend/internal/service` 的既有旧测试问题已修复：

- `metadata_service_test.go` 不再依赖已删除的 `repository.EngineRepository` 和 `MetadataService.ListExplorerResources/GetResourceTree`。
- 测试已迁移到当前承担数据探查职责的 `ExplorerService`，通过 `httptest` 覆盖 `SystemClient` 请求路径、租户过滤和跨租户访问拒绝。

## 当前工作树状态提示

当前代码有大量未提交改动，均与 engine plugin 迁移相关。新会话继续时不要回滚。

注意：早期计划中列出的 engine plugin 新增文件在当前工作树中已不再是未跟踪状态；继续工作时以 `git status --short` 为准。

## 功能跑通盘点

下一会话优先检查这些真实运行链路，而不是继续改历史说明文档。

### 必跑服务

建议按仓库规范启动或重启：

```bash
bash scripts/infra/up.sh
bash scripts/dev/start.sh
```

若只验证迁移相关服务，可重点重启：

```bash
./scripts/dev/restart.sh -system
./scripts/dev/restart.sh -meta
./scripts/dev/restart.sh -manager
./scripts/dev/restart.sh -develop
```

如果 `common/` 改动已影响共享包，优先使用：

```bash
./scripts/dev/restart.sh -all
```

### 必验用户路径

- System 引擎列表、创建/更新、连接测试、能力声明生成。
- System catalog API：
  - `GET /api/v1/system/engines/:id/namespaces`
  - `GET /api/v1/system/engines/:id/items?namespace=...`
- Develop 工作流/SQL 数据源选择器：
  - 前端已改为 `/develop/engines/:id/namespaces`
  - 前端已改为 `/develop/engines/:id/items`
- Meta 扫描：
  - 关系型：schema/database -> table/view -> fields。
  - 对象存储：bucket -> prefix -> object。
  - 文件系统：root/dir -> file。
  - MongoDB：database -> collection，深度 schema sampling。
  - Neo4j：database -> label/relationship。
- Manager 探查与预览：
  - database table preview。
  - filesystem/object preview。
  - lake table preview。
  - MongoDB collection preview。
  - Neo4j graph preview。
- Service/Transfer/Graph/Console 中调用 Meta 旧 `schemas/tables` API 的路径已在 18:34 迁移完成。

### 当前已知需重点排查

本节内容已在 18:34 记录中处理完成：旧 Meta `schemas/tables/fields/spatial` 查询入口已删除，调用方已迁移到 System `namespaces` 与 Meta `items/fields/spatial`。

### 可直接清理的数据

如遇到功能异常，可直接清理或重建：

- 旧 `system.engines.capabilities` 中非 `engine.capabilities/v1` 的 JSON。
- 旧 Meta 扫描生成的 `meta_node` / `meta_item`，尤其是对象存储叶子误标为 `file` 的记录。
- 旧 scan task/run/log 中依赖旧 schema/table/object 语义的测试数据。
- 旧 manager/search/cache/quick view 中基于旧 locator 或旧 item kind 的缓存。

清理后优先通过重新扫描生成新元数据，不要写复杂兼容转换。

## 下一步建议

### 1. 复核完整 Manager 探查树

Neo4j 真实链路已通过；下一步应按“功能跑通盘点”把 Manager 探查树和预览扩展到所有代表性存储引擎：

- MySQL / PostgreSQL：database/schema -> table，表预览返回 columns/rows。
- MinIO/S3：bucket -> prefix/object，bucket 或对象预览类型正确。
- NFS：root/dir/file，根目录文件和目录预览都正确。
- MongoDB：database -> collection，collection 预览返回文档样本。
- Neo4j：database -> label/relationship，label/relationship 预览走 graph runtime。

### 2. 继续跑功能链路

建议先跑：

```bash
go test ./common/engine/plugin ./common/engine/plugins/... ./common/utils ./common/models ./common/client ./common/dbbridge ./common/format/db ./common/format/parquet ./system/backend/internal/... ./develop/backend/internal/service ./develop/backend/internal/api ./meta/backend/internal/service ./manager/backend/internal/service
go test -tags integration ./common/engine/plugin/integration -run '^TestPluginInterfaceImplementation|TestRelationalDBPlugins|TestObjectStoragePlugins$'
npm run build --prefix develop/frontend
python3 -m compileall common-python/addp_common/client/develop.py copilot/backend/tools/develop_tools.py copilot/backend/pipelines/data_source_stage.py copilot/backend/tools/tests/test_tools.py
git diff --check
```

然后启动服务验证真实接口和页面。

### 3. 收口下游旧 Meta API

已完成。Service/Transfer/Graph/Console 与共享前端调用面已迁移到 System `namespaces` 和 Meta `items` API；旧 `/engines/:id/schemas`、`/metadata/tables`、`/metadata/fields`、`/metadata/tables/spatial` 不再保留。

### 4. 已完成的接口命名收口

- `common/engine/plugin/factory.go` 已新增：
  - `ListNamespaces`
  - `ListItems`
  - `DescribeItem`
  - `DescribeNamedItem`
  - `CountItemRows`
- `common/dbbridge/bridge.go` 已新增对应中性 facade，并复用 `toPluginEngine` 统一模型转换。
- `common/engine/plugin/factory.go` 的旧 `ListSchemas/ListTables/ListColumns/GetTableRowCount` facade 已删除。
- `common/dbbridge/bridge.go` 的旧 `ListSchemas/ListTables/ListColumns/GetTableRowCount` facade 已删除。
- `system/backend/internal/service/storage_engine_service.go` 已不再为了列 schema/table 预先创建连接池，直接调用 dbbridge 的 catalog 路径。
- `common/dbbridge/bridge.go` 的 `GenerateSampleQuery` 兜底分支已改用 `CatalogProvider` 发现第一张表，不再直接调用 `RelationalDBPlugin.ListSchemas/ListTables`。
- `service/backend/internal/service/data/query_service.go` 的表结构读取已改走 `dbbridge.DescribeNamedItem`，不再调用 `dbbridge.ListColumns`。
- `manager/backend/internal/service/preview_provider_database.go` 已不再通过 `dbbridge.ListColumns`，直接走 `ItemMetadataProvider`。
- System HTTP API 已从 `/engines/:id/schemas`、`/engines/:id/tables` 改为 `/engines/:id/namespaces`、`/engines/:id/items`。
- `common/client.SystemClient` 已从 `ListSchemas/ListTables` 改为 `ListNamespaces/ListCatalogItems`。
- Develop 后端和前端工作流选择器已切到 `/develop/engines/:id/namespaces`、`/develop/engines/:id/items`。
- `common-python/addp_common/client/develop.py` 已切到 `list_namespaces/list_catalog_items`。
- Copilot Develop 工具已切到 catalog 语义。

### 5. 已完成的 manager preview 迁移

- 表格预览查询执行已统一到 `SQLQueryRuntimeProvider.ExecuteSQL`，同时保留 PostgreSQL 空间列渲染表达式能力。
- 文件系统预览、湖表预览、文档集合预览已完成 provider 迁移，不再依赖旧 `FileSystemPlugin` / `NoSQLPlugin`。
- Neo4j label/relationship 预览走 `GraphQueryRuntimeProvider.ExecuteRuntimeGraphQuery`，locator `type` 必须保持 `label` / `relationship`。

### 6. 已完成的旧叶子接口边界收口

旧接口边界已按职责瘦身：

#### RelationalDBPlugin

当前已只保留：

- `StoragePlugin`
- `ConnectionPoolPlugin`
- `IsSystemSchema`

已移除：

- `ListSchemas/ListTables/ListColumns`
- `GetTableRowCount`
- `SchemaNodeType`

具体 SQL 插件中的元数据和行数方法均已下沉为未导出 helper，通过 `tabular_catalog.go` 的 adapter 回调注入。

#### Object/File provider

`ObjectStoragePlugin` / `FileSystemPlugin` 公共接口已移除。

最终对象存储列表能力走：

- `CatalogProvider`

对象内容读写走：

- `ContentReadableProvider`
- `ContentWritableProvider`
- 后续如有大文件预览或断点读取需求，再补 `RangeReadableProvider` 或扩展 `ReadOptions`。

对象存储专属语义应通过：

- `StoreProvider.StoreSemantics()`
- `Capabilities().Storage.Semantics`

文件系统/对象存储底层列根、列目录、读取和 stat 仍存在于插件内部未导出 helper，通过 `filesystem_catalog.go` adapter 注入，不再作为上层契约。

#### DocumentDBPlugin / GraphDBPlugin

列表职责已从公共接口迁到 catalog adapter，MongoDB 深度 schema 推断已迁到 `DocumentMetadataSamplingProvider`。

当前仍保留：

- 图数据库 schema 细节能力，后续考虑转入 `ItemMetadataProvider` 或 graph-specific metadata provider

建议新增或细化：

- `GraphSchemaProvider` 或将图 schema 完整纳入 `ItemMetadataProvider`

### 7. 已完成的旧 helper 和测试清理

- `common/dbbridge.ListSchemas/ListTables/ListColumns/GetTableRowCount` 已删除。
- `common/engine/plugin/factory.go` 旧元数据函数命名已删除。
- `common/engine/plugin/integration/verify_test.go` 中对旧插件接口的强断言已改为 provider 断言。
- `docs/spec/addp数据引擎扩展指南.md` 中旧接口说明已更新为 provider 体系。
- `docs/concepts/addp引擎体系图.md` 中旧 UML 已更新为 provider 体系。
- manager 旧测试 `metadata_service_test.go` 已迁移到 `ExplorerService`。

### 8. 后续可选清理

- 清理或更新阶段性 plan 文档中关于旧 `FileSystemPlugin` / `ObjectStoragePlugin` 的历史设计描述。
- 继续收敛 `docs/concepts/addp元数据体系图.md`、模块功能介绍等与 provider 体系不一致的旧流程图。

## 新会话推荐起点

建议新会话第一步执行：

```bash
git status --short
go test ./common/engine/plugin ./common/engine/plugins/... ./common/utils ./common/models ./common/client ./common/dbbridge ./common/format/db ./common/format/parquet ./system/backend/internal/... ./develop/backend/internal/service ./develop/backend/internal/api ./meta/backend/internal/service
go test -tags integration ./common/engine/plugin/integration -run '^TestPluginInterfaceImplementation|TestRelationalDBPlugins|TestObjectStoragePlugins$'
go test ./manager/backend/internal/service
npm run build --prefix develop/frontend
git diff --check
```

然后优先继续：

1. 先启动服务并验证“功能跑通盘点”中的真实用户路径。
2. 如旧 API、旧数据结构、旧元数据阻塞功能，直接删除或替换，不做兼容。
3. Meta 旧 `/engines/:id/schemas`、`/metadata/tables` 等元数据查询 API 已删除；后续只验证新 `namespaces/items` 链路。
4. 功能稳定后，再回头清理历史文档和 Swagger 生成流程。

## 注意事项

- 不要提交代码，除非用户明确授权。
- 不要回滚当前工作树。
- 当前文档是迁移计划，不是最终规范；最终规范仍应落在 `docs/spec/` 或已存在的 `docs/concepts/` 草案中。
- 清理旧接口时可以大胆删除，不需要兼容旧 API 和旧数据。

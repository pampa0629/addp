# Engine Plugin 接口体系迁移完成记录与后续工作

更新时间：2026-05-04 09:39 CST

## 背景与原则

本轮迁移以正式规范 [docs/spec/addp引擎插件接口规范.md](../spec/addp引擎插件接口规范.md) 和 [docs/spec/addp引擎能力声明规范.md](../spec/addp引擎能力声明规范.md) 为准，目标是把 ADDP 内部各类数据库、对象存储、文件系统、文档库、图库、工作流和脚本引擎纳入统一 engine plugin 体系。

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

## 已完成工作

截至 2026-05-04 00:04 CST，非 Transfer 主线迁移已完成：

- 能力声明已切到 `engine.capabilities/v1`，插件统一实现结构化 `Capabilities()`；旧 `GenerateCapabilities() string` 不再作为插件契约。
- Provider 抽象已落地：`CatalogProvider`、`ItemMetadataProvider`、`StoreProvider`、`ContentReadableProvider`、`SQLQueryRuntimeProvider`、`DocumentQueryRuntimeProvider`、`GraphQueryRuntimeProvider`、workflow/script runtime provider。
- 现有插件已覆盖 provider：PostgreSQL、MySQL、Doris、ClickHouse、Spark SQL、MinIO、S3、NFS、MongoDB、Neo4j、workflow/script 插件。
- 旧上层 `ListXXX` 接口依赖已收口，`common/engine/plugin` 与 `common/dbbridge` 已切到中性 facade：`ListNamespaces`、`ListItems`、`DescribeItem`、`DescribeNamedItem`、`CountItemRows`。
- System、Develop、Copilot 数据源选择链路已切到 catalog 语义：`namespaces/items` 替代 `schemas/tables`。
- Meta 扫描主链路已迁移到 provider，覆盖关系型、对象存储、文件系统、MongoDB、Neo4j。
- Manager 预览主链路已迁移到 provider，覆盖数据库表、文件系统、对象存储、湖表、MongoDB collection、Neo4j label/relationship。
- ResourceLocator 类型语义已修正：对象存储叶子为 `object`，文件系统叶子为 `file`，Neo4j 为 `label` / `relationship`，不再折叠为 `collection`。
- Meta 旧命名 API 已删除并在真实服务中验证为 404：
  - `GET /api/v1/meta/engines/:engine_id/schemas`
  - `GET /api/v1/meta/engines/:engine_id/schemas/available`
  - `GET /api/v1/meta/metadata/tables`
  - `GET /api/v1/meta/metadata/fields`
  - `GET /api/v1/meta/metadata/tables/spatial`
- Meta 当前查询 API 已收口：
  - `GET /api/v1/meta/engines/:engine_id/items?namespace=...`
  - `GET /api/v1/meta/engines/:engine_id/items/fields?namespace=...&name=...`
  - `GET /api/v1/meta/engines/:engine_id/items/spatial?namespace=...&name=...`
  - `GET /api/v1/meta/items/:item_id/fields`
  - `GET /api/v1/meta/items/:item_id/spatial`
- 实时目录发现统一使用 System：
  - `GET /api/v1/system/engines/:id/namespaces`
  - `GET /api/v1/system/engines/:id/items?namespace=...`
- 扫描请求参数已从 `schema_names` 切到 `namespaces`；对象存储/文件系统选择性扫描只认 `object_paths`，不再保留旧字段 fallback。
- Console、Meta、Manager、Graph、Service 与 `common-frontend/basic` 已同步迁移到新 API。
- System 引擎响应中的 `capabilities` 已从 JSON 字符串调整为结构化 JSON 对象；数据库层仍保持 JSONB 存储。
- `common/models.Engine.Capabilities` 已改为 JSONB 边界类型，可接收 JSON object 或旧 JSON string；HTTP 输出仍为 JSON object，数据库写入仍为 JSONB。
- 迁移相关规范已同步到路径、指纹、数据引擎扩展与引擎体系相关文档。
- 两个临时规范草案已整合为正式 spec，`docs/next/` 只保留后续工作记录。
- 旧 Meta 扫描结果、扫描运行记录、Manager 快显/embedding/搜索历史、相关执行记录和搜索索引文档已清理，可按新规范重新扫描生成。

Transfer 模块相关迁移、已知测试问题和后续修复建议已单独整理到 [engine-plugin-transfer后续事项.md](engine-plugin-transfer后续事项.md)，本记录不继续跟踪 Transfer 实现。

## 真实验证记录

已完成真实服务 smoke test：

- 基础设施启动通过：`bash scripts/infra/up.sh`。
- System、Meta、Manager、Develop、Gateway 已通过开发脚本启动验证。
- SuperAdmin 登录通过：`POST /api/v1/system/login`。
- System 引擎列表返回 10 个引擎，能力声明内容均为 `engine.capabilities/v1`。
- System catalog API 验证通过：
  - PostgreSQL engine 8：`namespaces` 返回 `public/tiger/tiger_data/topology`，`items?namespace=public` 返回表列表。
  - MongoDB engine 24：`namespaces` 返回 `Outdoor/business`，`items?namespace=Outdoor` 返回 collection。
  - Neo4j engine 25：`items` 返回 label/relationship。
  - NFS engine 26：`namespaces` 返回 `.`，`items` 返回文件项。
  - Jupyter engine 23：无 `CatalogProvider`，catalog API 返回不支持，符合 script engine 语义。
- Meta scan 验证通过：
  - `POST /api/v1/meta/scan/engine`
  - 请求体：`{"engine_id":8,"namespaces":["public"],"scan_depth":"basic"}`
  - 响应：`namespaces_scanned=1`、`items_scanned=24`、`fields_scanned=207`。
- Meta 落库验证通过：
  - `metadata.meta_node` 生成 `tenant_id=1 engine_id=8 node_type=schema name=public item_count=24`。
  - `metadata.meta_item` 生成 public 下表项。
- Meta 新 API 验证通过：
  - `GET /api/v1/meta/engines/8/items?namespace=public`
  - `GET /api/v1/meta/engines/8/items/fields?namespace=public&name=dltb`
  - `GET /api/v1/meta/engines/8/items/spatial?namespace=public&name=dltb`
- Meta 旧 API 验证为 404：
  - `GET /api/v1/meta/engines/8/schemas`
  - `GET /api/v1/meta/metadata/tables`
- Neo4j Manager 链路验证通过：`Project` 树节点 locator 为 `type=label`，预览返回 `preview_type=table`、7 列、8 行。
- System capabilities 响应形态验证通过：`/api/v1/internal/engines` 返回 `capabilities` JSON object，`schema_version=engine.capabilities/v1`。
- Meta 引擎列表恢复验证通过：`/api/v1/meta/engines` 返回 6 个存储类引擎，且日志中不再出现 `cannot unmarshal object into Go struct field Engine.capabilities of type string`。

## 本轮补充修复

真实 smoke test 暴露并已修复：

- SuperAdmin token 中 `tenant_id=0`，但业务引擎实际属于租户 1。Meta 扫描、执行记录、事件发布和新查询 API 已改为在请求租户为 0 时使用引擎实际租户，避免元数据落库外键错误和查询不到扫描结果。
- 关系型 namespace 扫描失败曾被吞掉并返回 0/0 成功；现在会收集 namespace 扫描错误并返回失败。
- System HTTP 响应已把 `capabilities` 序列化为 JSON 对象，避免上层继续消费 JSON 字符串。
- `common/models.Engine.Capabilities` 已改为 JSONB 边界类型，可接收 JSON object 或旧 JSON string；HTTP 输出仍为 JSON object，数据库写入仍为 JSONB。该修复解决了 Meta/Manager 等 Go 客户端解析 System 新响应时报 `cannot unmarshal object into ... string` 的问题。
- 旧 Meta 扫描系统分析稿已删除，避免后续会话误读 `common/database`、旧扫描服务和旧 `ListSchemas/ListTables` 语义。

## 已跑门禁

本轮已通过：

```bash
go test ./common/engine/plugin ./common/engine/plugins/... ./common/utils ./common/models ./common/client ./common/dbbridge ./common/format/db ./common/format/parquet ./system/backend/internal/... ./develop/backend/internal/service ./develop/backend/internal/api ./meta/backend/internal/service ./manager/backend/internal/service
go test -tags integration ./common/engine/plugin/integration -run '^TestPluginInterfaceImplementation|TestRelationalDBPlugins|TestObjectStoragePlugins$'
go test ./meta/backend/internal/service ./meta/backend/internal/api ./common/client ./common/models ./manager/backend/internal/service ./manager/backend/internal/worker ./service/backend/internal/api ./service/backend/internal/service ./quality/backend/internal/service
go test ./common/models ./common/utils ./common/client ./system/backend/internal/models ./system/backend/internal/api ./system/backend/internal/service ./meta/backend/internal/service ./manager/backend/internal/service
npm run build --prefix develop/frontend
npm run build --prefix meta/frontend
npm run build --prefix manager/frontend
npm run build --prefix service/frontend
npm run build --prefix graph/frontend
npm run build --prefix console/frontend
git diff --check
```

本轮 Swagger/i18n 收尾追加验证已通过：

```bash
bash scripts/swagger/gen-swagger.sh manager meta
go test ./meta/backend/internal/api ./meta/backend/internal/service ./manager/backend/internal/api ./manager/backend/internal/service
npm run build --prefix meta/frontend
npm run build --prefix manager/frontend
git diff --check
```
  
如后续继续调整 Transfer，应追加执行 `docs/next/engine-plugin-transfer后续事项.md` 中列出的 Transfer 门禁。

## 后续要开展的工作

### 1. Transfer 模块收口

Transfer 迁移不纳入本记录主线，继续按 [engine-plugin-transfer后续事项.md](engine-plugin-transfer后续事项.md) 推进。重点包括：

- 恢复或明确移除 Transfer 任务模式领域语义。
- 对齐 Transfer 任务模型、执行管道和前端任务向导中的 `batch` / `stream` / `micro-batch` 语义。
- 修复后补跑 Transfer 后端测试、前端构建和真实创建/更新/执行任务路径。

### 2. 清理或重建旧派生数据（已执行）
 
2026-05-04 已完成一次真实环境清理与重建验证：

- 确认 `system.engines.capabilities` 均为 `engine.capabilities/v1`，无旧能力声明 JSON 残留。
- 备份旧派生数据到 `/tmp/addp_engine_plugin_cleanup_20260504_004600.sql`。
- 清理 `metadata.meta_node` / `metadata.meta_item` / `metadata.scan_tasks` / `metadata.scan_task_runs`。
- 清理 `manager.quick_view` / `manager.search_histories` / `manager.embeddings` / `manager.embedding_tasks` / `manager.mvt_tasks`。
- 清理 Redis 中 Meta/Manager 相关派生缓存和旧 `schema_names` 相关键。
- 清空 Meilisearch `assets` 索引旧文档，并通过重新扫描生成新索引文档。
- 通过 `POST /api/v1/meta/scan/engine` 重扫 PostgreSQL engine 8 的 `public` namespace，扫描结果为 `namespaces_scanned=1`、`items_scanned=24`、`fields_scanned=207`。
- 追加修复 Meta tree API 在 SuperAdmin `tenant_id=0` 场景下未切换到引擎实际租户的问题，避免 Manager tree 在清理重扫后返回空树。
- 复验通过：
  - Meta tree 可返回 PostgreSQL engine 8 的 `public` 等顶层节点和 item。
  - Manager tree 可返回 `Business PostgreSQL -> public -> table` 层级。
  - Manager preview 可通过 `addp://engine/8/path/public/dltb?type=table` 返回表格预览。
  - 旧 `schema_names` 扫描参数残留为 0，Manager quick view/search history 派生表为 0，Redis 相关缓存键为 0。

后续如再次遇到旧派生数据导致的功能异常，可直接清理或重建：

- 旧 `system.engines.capabilities` 中非 `engine.capabilities/v1` 的 JSON。
- 旧 Meta 扫描生成的 `meta_node` / `meta_item`，尤其是旧 locator type 或旧 catalog kind 语义的数据。
- 旧 scan task/run/log 中依赖 `schema_names` 或旧 schema/table/object 语义的测试数据。
- 旧 manager/search/cache/quick view 中基于旧 locator 或旧 item kind 的缓存。

清理后优先通过重新扫描生成新元数据，不要写复杂兼容转换。

### 3. Swagger 与生成文档整理（已执行）

- 2026-05-04 已清理 Manager 空间数据、快显、瓦片、要素定位和导入接口的 Swagger 注解，将公开说明从旧 `Schema/Table` 统称切到 `命名空间 / 数据项` 语义。
- 通过 `bash scripts/swagger/gen-swagger.sh manager meta` 重新生成 Manager 与 Meta Swagger 文档。
- 复扫 `manager/backend/docs`、`meta/backend/docs`、Manager/Meta API 注解，确认不再出现以下旧公开 API/术语：
  - `metadata/tables`
  - `/schemas`
  - `schema_names`
  - `空间数据表`
  - `spatial table`
  - `数据库Schema` / `Database schema`
  - `数据表名` / `Table name`
- 对仍然代表真实关系型对象的内部路由参数名和 service/model 字段名暂不做机械重命名，避免扩大行为改动；公开文档和用户可见描述已按新规范收口。
- 已检查 `docs/plan/` 中与 engine plugin 相关的历史计划；相关文档均已标注“当前实现以 provider 化 engine plugin 体系为准”，不再指导新实现。

### 4. i18n 与前端文案整理（已执行）

- 2026-05-04 已清理 Meta 扫描页面 i18n 中用户可见的旧 `Schema` / `Table` 统称：
  - `Schema List` / `Schema列表` -> `Namespace List` / `命名空间列表`
  - 扫描完成、加载失败、右侧列表标题和数据项计数改为 namespace/path/item 语义。
  - 新增 `namespaceListSuffix`、`namespaceInfoSuffix`、`defaultNamespaceTerm`、`directoryTerm`，避免前端通过字符串替换拼接旧 `Schema`。
- 已清理 Manager 前端 i18n 中用户可见的旧数据库表统称：
  - 元数据管理副标题、数据项列表、元数据详情、扫描结果、纳管提示统一改为 `Data Items` / `数据项`。
  - 扫描任务中的 `Schema List` 改为 `Namespace List` / `命名空间列表`。
  - 导入表单中的目标 `Schema/Table` 改为目标命名空间和目标数据项名称。
- 图数据库 `Schema Graph` 仍保留为图数据库领域术语，不属于旧关系型 schema/table API 语义。

### 5. 更多真实链路验证

- 继续补充 MinIO/S3、MongoDB、MySQL 的 Meta scan、Manager tree、Manager preview 真实路径验证。
- 根据更多真实 smoke test 结果继续补充正式规范中的边界案例。

2026-05-04 补充验证与修复：

- NFS 根目录文件预览标题重复问题已修复：前端 locator 兼容层不再把单段路径同时映射为 `schema=README.md` 和 `path=README.md`，文件/对象标题直接展示完整路径，后端补充 `nfsPhysicalPath("", "README.md") == "/README.md"` 测试。
- Neo4j 预览无效果的根因已定位为旧 Meta 派生数据把 label/relationship 写成 `item_type=table`；同时修复自动扫描旧入口误将 document/graph 引擎落回关系型扫描的问题。
- Meta `UpsertItem` 在指纹命中旧记录时会同步更新 `item_type` / `name`，避免旧 `table` 记录在 graph 重扫时继续保留错误类型。
- Neo4j 重扫时会清理同一 database 节点下残留的旧 `item_type=table` 图数据项。
- 已清理 engine 25 的旧 `metadata.meta_node` / `metadata.meta_item` 派生数据并重新扫描：
  - `POST /api/v1/meta/scan/engine` 请求 `{"engine_id":25,"namespaces":["neo4j"],"scan_depth":"basic"}` 返回 `namespaces_scanned=1`、`items_scanned=13`。
  - `metadata.meta_item` 当前分布为 `label=8`、`relationship=5`，无 active `table` 残留。
  - Manager tree 返回 `type=label` / `type=relationship` locator，例如 `addp://engine/25/path/neo4j/Project?type=label&meta_id=100292`。
  - Manager preview 验证通过：`Project` label 返回 `preview_type=table`、8 行；`WORKS_AT` relationship 返回 `preview_type=table`、20 行，且无业务属性时也展示 `id/type`。

## 注意事项

- 不要提交代码，除非用户明确授权。
- 不要回滚当前工作树。
- 当前文档是迁移完成记录，不是最终规范；最终规范已落在 `docs/spec/`。
- 清理旧接口时可以大胆删除，不需要兼容旧 API 和旧数据。

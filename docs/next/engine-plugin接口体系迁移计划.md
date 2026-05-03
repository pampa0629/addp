# Engine Plugin 接口体系迁移完成记录与后续工作

更新时间：2026-05-04 00:04 CST

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
  
如后续继续调整 Transfer，应追加执行 `docs/next/engine-plugin-transfer后续事项.md` 中列出的 Transfer 门禁。

## 后续要开展的工作

### 1. Transfer 模块收口

Transfer 迁移不纳入本记录主线，继续按 [engine-plugin-transfer后续事项.md](engine-plugin-transfer后续事项.md) 推进。重点包括：

- 恢复或明确移除 Transfer 任务模式领域语义。
- 对齐 Transfer 任务模型、执行管道和前端任务向导中的 `batch` / `stream` / `micro-batch` 语义。
- 修复后补跑 Transfer 后端测试、前端构建和真实创建/更新/执行任务路径。

### 2. 清理或重建旧派生数据
 
如遇功能异常，可直接清理或重建：

- 旧 `system.engines.capabilities` 中非 `engine.capabilities/v1` 的 JSON。
- 旧 Meta 扫描生成的 `meta_node` / `meta_item`，尤其是旧 locator type 或旧 catalog kind 语义的数据。
- 旧 scan task/run/log 中依赖 `schema_names` 或旧 schema/table/object 语义的测试数据。
- 旧 manager/search/cache/quick view 中基于旧 locator 或旧 item kind 的缓存。

清理后优先通过重新扫描生成新元数据，不要写复杂兼容转换。

### 3. Swagger 与生成文档整理

- 重新生成或清理 Swagger 文档中旧字段、旧路由、旧 schema/table 命名。
- 检查 `docs/plan/` 中仍保留的历史计划，确保它们只作为历史记录，不再指导新实现。

### 4. 更多真实链路验证

- 继续补充 MinIO/S3、NFS、MongoDB、Neo4j、MySQL 的 Meta scan、Manager tree、Manager preview 真实路径验证。
- 验证清理旧 Meta 数据后，重新扫描能稳定生成新 catalog/item 语义数据。
- 根据更多真实 smoke test 结果继续补充正式规范中的边界案例。

## 注意事项

- 不要提交代码，除非用户明确授权。
- 不要回滚当前工作树。
- 当前文档是迁移完成记录，不是最终规范；最终规范已落在 `docs/spec/`。
- 清理旧接口时可以大胆删除，不需要兼容旧 API 和旧数据。

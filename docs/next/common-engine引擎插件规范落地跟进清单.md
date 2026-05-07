# Common Engine 引擎插件规范落地跟进清单

更新时间：2026-05-08

本文跟进 `common/engine` 引擎插件概念边界确认后的规范修订、代码迁移和验证事项。概念讨论稿见 `docs/next/common-engine引擎插件概念边界讨论稿.md`；正式规范以 `docs/spec/` 下文档为准。

## 接力标记

> 后续新会话优先阅读本节，再查看未完成复选框。

- 当前状态：概念层已收敛为“就简不就繁”：删除无独立价值字段，保留明确 Provider 映射；连接信息统一使用 `connection_info` map；DSN 改为可选 Provider；NFS root `name="."` 必须保留。
- 已修订规范：
  - `docs/spec/addp引擎能力声明规范.md`
  - `docs/spec/addp引擎插件接口规范.md`
  - `docs/spec/addp存储引擎路径体系规范.md`
  - `docs/spec/addp数据引擎扩展指南.md`
- 尚未迁移代码：`common/engine/plugin` 和各插件仍保留旧接口 / 字段，如 `EngineCategory()`、`BuildConnectionString()`、`storage.families`、`store.read/write/random_write/transactions/formats` 等，后续应按本清单迁移。
- 最近验证：本轮只改文档，未运行测试。

## 一、已确认规范结论

- [x] Store 能力删除 `read` / `write` 总开关。
- [x] Store 能力使用 `stream_read`、`stream_write`、`range_read`、`range_write`、`batch_read`、`batch_write`。
- [x] 能力层使用 `range_write`，不使用 `random_write` 命名；文件系统底层实现可使用 `pwrite` / `WriteAt`。
- [x] `atomic_rename`、`transactions`、`formats` 不作为 Store 顶层能力。
- [x] 删除 `storage.families`，由顶层 `engine_family` 和具体 capability 表达能力。
- [x] CatalogModel 对外事实源为 `storage.catalog_model`；`CatalogModelProvider` 如保留，必须与 capabilities 一致。
- [x] `EngineCategory` 调整为 `EngineOrigin`，取值为 `general` / `extension`。
- [x] 连接信息统一使用 `connection_info` map，不新增 `ConnectionSummary`。
- [x] `BuildConnectionString()` 从基础接口移除，改为可选 `DSNProvider.BuildDSN()`。
- [x] `TestConnection()` 必须只读，不验证写能力。
- [x] NFS root meta_node 必须存在，`name="."`，用于容纳根目录下直接存在的文件。
- [x] 对象存储和文件系统不得共享 CatalogModel / CatalogAdapter。

## 二、已修订文档

- [x] `docs/spec/addp引擎能力声明规范.md`
  - 删除 `StorageCapabilities.Families`。
  - 重写 `StoreCapability` 字段。
  - 补充 Store capability 与 Provider 的校验规则。
  - 更新 PostgreSQL / MinIO 示例。
- [x] `docs/spec/addp引擎插件接口规范.md`
  - 基础接口改为 `EngineOrigin()`。
  - 删除基础接口中的 `BuildConnectionString()`。
  - 新增可选 `DSNProvider`。
  - 新增 `RangeReadableProvider` / `RangeWritableProvider`。
  - 补充连接测试只读要求。
  - 补充 Manager / Transfer / Develop 的能力消费边界。
- [x] `docs/spec/addp存储引擎路径体系规范.md`
  - 强化 NFS root `name="."` 必须存在。
  - 新增对象存储与文件系统模型边界章节。
- [x] `docs/spec/addp数据引擎扩展指南.md`
  - 更新 capabilities 示例。
  - 补充 Store Provider 映射。
  - 补充对象存储和文件系统分离建模要求。

## 三、代码迁移清单

### 能力结构

- [ ] 修改 `common/engine/plugin/capabilities.go`：
  - [ ] 删除 `StorageCapabilities.Families`。
  - [ ] 删除 `StoreCapability.Read` / `Write`。
  - [ ] 删除 `StoreCapability.RandomWrite`。
  - [ ] 删除 `StoreCapability.AtomicRename`。
  - [ ] 删除 `StoreCapability.Transactions`。
  - [ ] 删除 `StoreCapability.Formats`。
  - [ ] 新增 `StoreCapability.RangeWrite`。
- [ ] 修改 `common/engine/plugin/capability_builders.go`：
  - [ ] 更新 tabular/object/file capabilities builder。
  - [ ] MinIO / S3 不声明未实现的 `stream_write` / `range_write`。
  - [ ] NFS 按实际 Provider 能力声明。
- [ ] 补充 Document / Graph capability builder，避免 MongoDB / Neo4j 手写漂移。

### 基础接口与连接信息

- [ ] 修改 `common/engine/plugin/interfaces.go`：
  - [ ] `EngineCategory()` 改为 `EngineOrigin()`。
  - [ ] 从 `EnginePlugin` 移除 `BuildConnectionString()`。
  - [ ] 新增可选 `DSNProvider`。
  - [ ] 新增 `RangeReadableProvider` / `RangeWritableProvider`。
- [ ] 各插件迁移：
  - [ ] `EngineOrigin()` 返回 `general` 或 `extension`。
  - [ ] 数据库类插件实现 `DSNProvider.BuildDSN()`。
  - [ ] 非数据库类插件删除 JSON connection string 返回逻辑。
- [ ] 清理 `common/engine/plugin/factory.go` / `common/dbbridge/bridge.go` 中旧 `BuildConnectionString` 统一入口。
- [ ] 评估并迁移 `common/models.BuildConnectionString()` 旧路径；调用方应改为插件 DSNProvider 或更合适的模块能力。

### Store Provider

- [ ] 增加 `RangeReadableProvider` / `RangeWritableProvider` 实现或适配。
- [ ] MinIO / S3 如声明 `range_read`，必须实现 range 读取，或在 `OpenContent` 中明确支持 `ReadOptions.Offset/Length`。
- [ ] NFS 如声明 `range_write`，必须实现 `WriteRange`。
- [ ] 未实现的写能力不得声明。

### Catalog 模型

- [ ] 拆分对象存储和文件系统 CatalogAdapter：
  - [ ] `ObjectCatalogAdapter`：`bucket -> prefix -> object`。
  - [ ] `FileCatalogAdapter`：`root -> directory -> file`。
- [ ] 保留 NFS 当前正确行为：唯一 root 节点 `name="."`。
- [ ] `Capabilities().Storage.CatalogModel` 与 `CatalogModelProvider.CatalogModel()` 使用同一 helper 生成。

### 测试

- [ ] 新增 capabilities validator：
  - [ ] 校验 schema version、engine type、engine family。
  - [ ] 校验 catalog / metadata / store / query 声明与 Provider 实现一致。
  - [ ] 校验 catalog_model 与 CatalogModelProvider 一致。
- [ ] 更新插件注册测试，覆盖 NFS、Neo4j、Jupyter、Workflow 等全部插件。
- [ ] 补 S3 默认 SSL 行为测试。
- [ ] 补 NFS root `name="."` 和根目录文件归属测试。
- [ ] 补 range read / range write provider 声明一致性测试。

## 四、建议迁移顺序

1. 先改能力结构和 builder，确保编译期暴露所有旧字段。
2. 再改基础接口：`EngineOrigin`、`DSNProvider`、range provider。
3. 迁移各插件，先数据库类，再对象存储 / 文件系统，再 workflow / script。
4. 拆分 object / file catalog adapter，重点保护 NFS root 行为。
5. 最后清理上层旧 `BuildConnectionString` 调用路径和测试。

## 五、验证命令

代码迁移后至少执行：

```bash
go test ./common/engine/plugin ./common/engine/plugins/...
go test -tags integration ./common/engine/plugin/integration -run '^TestPluginInterfaceImplementation'
go test ./system/backend/internal/service ./meta/backend/internal/service ./manager/backend/internal/service
git diff --check
```

如果改动 `common/` 后需要运行时验证：

```bash
./scripts/dev/restart.sh -all
```

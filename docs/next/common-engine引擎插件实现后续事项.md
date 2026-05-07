# Common Engine 引擎插件实现后续事项

更新时间：2026-05-08

本文记录 `common/engine` 引擎插件实现层的已知问题、修复方向和验证建议。概念边界已在 `docs/next/common-engine引擎插件概念边界讨论稿.md` 中收敛；规范落地清单见 `docs/next/common-engine引擎插件规范落地跟进清单.md`。正式规范以 `docs/spec/addp引擎插件接口规范.md`、`docs/spec/addp引擎能力声明规范.md`、`docs/spec/addp存储引擎路径体系规范.md` 为准。

## 接力标记

> 后续新会话优先阅读本节，再查看“改进计划”。

- 当前状态：本轮已完成概念讨论稿和正式规范修订，但 `common/engine` 代码尚未按新规范迁移。
- 当前实现仍保留旧接口和旧字段：`EngineCategory()`、`BuildConnectionString()`、`storage.families`、`store.read/write/random_write/transactions/formats` 等。
- NFS 当前以 `name="."` 的唯一 root 节点容纳挂载根目录下文件，这个外在行为是正确的，后续重构必须保留。
- 最近验证：本轮只改文档，未运行测试。

## 一、已确认的实现目标

- Store 能力改为 `stream_read`、`stream_write`、`range_read`、`range_write`、`batch_read`、`batch_write`。
- 删除 Store 顶层 `read` / `write`、`random_write`、`atomic_rename`、`transactions`、`formats`。
- 删除 `storage.families`。
- `EngineCategory()` 迁移为 `EngineOrigin()`，取值 `general` / `extension`。
- `BuildConnectionString()` 从 `EnginePlugin` 基础接口移除，数据库类插件通过可选 `DSNProvider.BuildDSN()` 提供。
- `connection_info` map 继续作为所有引擎连接信息事实源。
- `CatalogModelProvider.CatalogModel()` 如保留，必须与 `Capabilities().Storage.CatalogModel` 完全一致。
- 对象存储和文件系统不得共享 CatalogModel / CatalogAdapter。
- Catalog / Metadata / TestConnection 必须保持只读，不得执行外部写入、DDL 或统计刷新。

## 二、已发现实现问题

### 1. 对象存储和文件系统 catalog 抽象混用

当前 `common/engine/plugin/filesystem_catalog.go` 中的 `FileSystemCatalogAdapter`、`FileEntry`、`DescribeFileSystemItem` 同时承载 MinIO、S3 和 NFS。

问题：

- MinIO / S3 应建模为 `bucket -> prefix -> object`。
- NFS 应建模为 `root -> directory -> file`。
- 当前 NFS 子目录在通用 adapter 中被标记为 `prefix`。
- `filesystem.go` 注释以 `bucket/prefix/file.parquet` 描述路径，对 NFS 不成立。

修复方向：

- 新增 `ObjectCatalogAdapter`，专用于对象存储。
- 新增 `FileCatalogAdapter`，专用于文件系统。
- MinIO / S3 迁移到 object adapter。
- NFS 迁移到 file adapter，并保留唯一 root `name="."`。

### 2. NFS CatalogModel 与能力声明不一致

当前 `NewFileCapabilities()` 声明 `root -> directory -> file`，但 `NFSPlugin.CatalogModel()` 返回的 `FileCatalogModel()` 实际为 `service -> root -> path -> file`，且 `path` kind 复用 `prefix`。

修复方向：

- `NewFileCapabilities()` 与 `FileCatalogModel()` 使用同一 helper。
- 文件系统目录 term/kind 使用 `directory`。
- NFS 顶层 catalog 保持唯一 root 节点：`Name="."`、`Path="/"`。

### 3. Store capabilities 与 Provider 实现不一致

当前 MinIO / S3 能力声明偏乐观，历史上声明了未实现或未完整实现的写入 / range 能力。NFS 也可能继承了不准确的对象能力。

修复方向：

- 能力声明只保留真实实现。
- MinIO / S3 如声明 `range_read`，必须实现 byte range 读取。
- MinIO / S3 未实现写 Provider 前，不声明 `stream_write` 或 `range_write`。
- NFS 如声明 `range_write`，必须实现 `RangeWritableProvider.WriteRange()`。
- 添加能力声明与 Provider 一致性测试。

### 4. S3 默认 SSL 判断存在 bug

`common/engine/plugins/s3/plugin.go` 中默认 SSL 判断逻辑错误，导致未显式传入 `use_ssl` 时没有按 S3 默认 HTTPS 处理。

修复方向：

- 抽出 S3 connection info 解析 helper。
- 缺省 `use_ssl` 时使用 true。
- 显式传入 false 时尊重用户配置。
- `TestConnection()` 和 `createClient()` 共享同一解析逻辑。

### 5. PostgreSQL catalog 查询存在外部副作用

`common/engine/plugins/postgresql/plugin.go` 的 `listTables()` 在 `reltuples=-1` 时会执行 `ANALYZE`。

问题：

- Catalog / Metadata Provider 应只读。
- `ANALYZE` 会修改外部数据库统计状态，可能带来权限、锁等待和审计问题。

修复方向：

- 移除 catalog 列表中的 `ANALYZE`。
- 行数未知时不写 `row_count`，或保留引擎返回的估算值；不得主动刷新统计。

### 6. SQL 插件重复代码过重

PostgreSQL、MySQL、Doris、ClickHouse、Spark SQL 重复实现大量样板：

- 连接信息解析。
- DSN 构造。
- TestConnection。
- GORM pool 创建。
- QueryRuntime 样板。
- tabular catalog adapter。
- information_schema / system catalog 查询结构。

修复方向：

- 抽取 SQL runtime 通用 mixin。
- 抽取 GORM connection pool 创建 helper。
- 抽取 `DSNProvider.BuildDSN()` 相关 helper。
- MySQL / Doris 优先迁移到 MySQL-compatible metadata dialect。
- PostgreSQL、ClickHouse、Spark SQL 后续按方言逐步迁移。

### 7. MongoDB 和 Neo4j capabilities 手写，容易漂移

MongoDB 和 Neo4j 当前手写完整 `EngineCapabilities`，容易与 `DocumentCatalogModel()` / `GraphCatalogModel()` 漂移。

修复方向：

- 新增 `NewDocumentCapabilities()`。
- 新增 `NewGraphCapabilities()`。
- MongoDB / Neo4j 使用 builder。
- 重新确认 Neo4j metadata 支持边界；未实现的字段结构 / 统计能力不得声明。

### 8. MongoDB 查询语言声明不一致

MongoDB capabilities 和 `QueryLanguages()` 声明 `mql`，但 sample query fallback 返回 `"json"`。

修复方向：

- 统一返回 `mql`。
- JSON command 只是 MQL 的表达方式，不新增未声明语言。

### 9. 测试覆盖不足

当前测试未覆盖全部插件，也缺少 capabilities 与 Provider 的一致性校验。

修复方向：

- 建立 capabilities validator。
- 覆盖全部已注册插件，包括 NFS、Neo4j、Jupyter、Workflow。
- 校验 `storage.catalog_model` 与 `CatalogModelProvider.CatalogModel()` 一致。
- 校验 Store / Query / Workflow / Script 能力声明与 Provider 实现一致。

### 10. 旧命名残留

registry / factory / 注释 / 错误消息仍有 `database plugin`、`unsupported database type`、`filesystem` 承载对象存储等旧命名。

修复方向：

- registry/factory 层统一使用 `engine type`。
- object / file 通用层避免使用 `filesystem` 命名承载对象存储。
- tabular adapter 内部可使用 `namespace`，插件局部再映射 schema/database。

## 三、改进计划

### 阶段一：能力结构和接口迁移

- [ ] 修改 `common/engine/plugin/capabilities.go`：
  - [ ] 删除 `StorageCapabilities.Families`。
  - [ ] 删除 `StoreCapability.Read` / `Write`。
  - [ ] 删除 `StoreCapability.RandomWrite`。
  - [ ] 删除 `StoreCapability.AtomicRename`。
  - [ ] 删除 `StoreCapability.Transactions`。
  - [ ] 删除 `StoreCapability.Formats`。
  - [ ] 新增 `StoreCapability.RangeWrite`。
- [ ] 修改 `common/engine/plugin/interfaces.go`：
  - [ ] `EngineCategory()` 改为 `EngineOrigin()`。
  - [ ] 从 `EnginePlugin` 移除 `BuildConnectionString()`。
  - [ ] 新增可选 `DSNProvider`。
  - [ ] 新增 `RangeReadableProvider` / `RangeWritableProvider`。
- [ ] 更新所有插件接口实现。

### 阶段二：能力声明和 builder 迁移

- [ ] 更新 `NewTabularCapabilities()`。
- [ ] 更新 `NewObjectCapabilities()`。
- [ ] 更新 `NewFileCapabilities()`。
- [ ] 新增 `NewDocumentCapabilities()`。
- [ ] 新增 `NewGraphCapabilities()`。
- [ ] 收紧 MinIO / S3 / NFS 的 Store 能力声明。
- [ ] MongoDB / Neo4j 改用 builder。

### 阶段三：Catalog adapter 拆分

- [ ] 新增 `ObjectCatalogAdapter`。
- [ ] 新增 `FileCatalogAdapter`。
- [ ] MinIO / S3 切换到 object adapter。
- [ ] NFS 切换到 file adapter。
- [ ] 保留 NFS root `name="."` 行为。
- [ ] 删除或重命名误导性的 `FileSystemCatalogAdapter`。

### 阶段四：连接和 DSN 清理

- [ ] 数据库类插件实现 `DSNProvider.BuildDSN()`。
- [ ] 非数据库插件删除 JSON connection string 逻辑。
- [ ] 清理 `common/engine/plugin/factory.go` / `common/dbbridge/bridge.go` 旧 `BuildConnectionString` 统一入口。
- [ ] 评估并迁移 `common/models.BuildConnectionString()` 旧调用方。
- [ ] 保持 `connection_info` map 作为统一连接信息事实源。

### 阶段五：真实 bug 和副作用修复

- [ ] 修复 S3 默认 SSL。
- [ ] 实现或收紧 MinIO / S3 range read。
- [ ] 如声明 NFS range write，实现 `RangeWritableProvider`。
- [ ] 移除 PostgreSQL catalog `ANALYZE`。
- [ ] 统一 MongoDB sample query language 为 `mql`。

### 阶段六：重复代码收敛

- [ ] 抽取 SQL runtime 通用 mixin。
- [ ] 抽取 GORM pool 创建 helper。
- [ ] 抽取 SQL / Mongo / Neo4j DSN helper。
- [ ] MySQL / Doris 优先迁移到 MySQL-compatible dialect。
- [ ] 抽取 S3-compatible object client helper。

### 阶段七：测试补强

- [ ] 新增 capabilities validator。
- [ ] 覆盖全部插件注册。
- [ ] 校验 Store / Query / Workflow / Script Provider 与能力声明一致。
- [ ] 校验 CatalogModel 一致。
- [ ] 补 S3 默认 SSL 测试。
- [ ] 补 NFS root 行为测试。
- [ ] 补 range read / range write provider 测试。

## 四、验证建议

每轮修改后至少执行：

```bash
go test ./common/engine/plugin ./common/engine/plugins/...
go test -tags integration ./common/engine/plugin/integration -run '^TestPluginInterfaceImplementation'
git diff --check
```

如果修改 `common/engine` 对上层消费有影响，继续补跑：

```bash
go test ./system/backend/internal/service ./meta/backend/internal/service ./manager/backend/internal/service
```

如果修改 `common/` 共享能力并需要运行时验证，按项目规范使用：

```bash
./scripts/dev/restart.sh -all
```

## 五、暂不建议直接做的事

- 不建议只在 MinIO / S3 / NFS 中局部改字段名，而继续保留对象存储和文件系统共用同一 CatalogAdapter。
- 不建议提前声明未实现能力。
- 不建议继续为新增 SQL 引擎复制整文件。
- 不建议保留旧能力结构或旧路径语义兼容层。

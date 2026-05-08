# Common Engine 引擎插件实现后续事项

更新时间：2026-05-08

本文是 `common/engine` 引擎插件后续事项的唯一跟进文档，记录实现层已知问题、修复方向、迁移计划和验证建议。概念边界已在 `docs/next/common-engine引擎插件概念边界讨论稿.md` 中收敛；正式规范以 `docs/spec/addp引擎插件接口规范.md`、`docs/spec/addp引擎能力声明规范.md`、`docs/spec/addp存储引擎路径体系规范.md` 为准。

## 接力标记

> 后续新会话优先阅读本节，再查看“改进计划”。

- 当前状态：本轮已按新规范完成 `common/engine` 核心接口、能力声明、Catalog adapter、DSN provider、provider validator、主要插件和上层调用方迁移。
- 旧接口和旧字段已从代码主路径移除：`EngineCategory()`、`BuildConnectionString()` 基础接口、`storage.families`、`store.read/write/random_write/transactions/formats` 等。
- NFS 当前以 `name="."` 的唯一 root 节点容纳挂载根目录下文件，这个外在行为已有测试保护，后续重构必须保留。
- 最近验证：已通过本文“验证记录”中的全部命令。
- 已删除重复的平行 checklist；后续不要再新建同类跟进文档，本文件是唯一真相。

## 相关文档

- 概念讨论历史：`docs/next/common-engine引擎插件概念边界讨论稿.md`
- 正式接口规范：`docs/spec/addp引擎插件接口规范.md`
- 正式能力规范：`docs/spec/addp引擎能力声明规范.md`
- 正式路径规范：`docs/spec/addp存储引擎路径体系规范.md`
- 数据引擎扩展指南：`docs/spec/addp数据引擎扩展指南.md`

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

## 二、已处理问题记录

### 1. 对象存储和文件系统 catalog 抽象混用

原 `common/engine/plugin/filesystem_catalog.go` 中的通用 filesystem adapter 同时承载 MinIO、S3 和 NFS。

问题：

- MinIO / S3 应建模为 `bucket -> prefix -> object`。
- NFS 应建模为 `root -> directory -> file`。
- 当前 NFS 子目录在通用 adapter 中被标记为 `prefix`。
- `filesystem.go` 注释以 `bucket/prefix/file.parquet` 描述路径，对 NFS 不成立。

处理结果：

- 已新增 `ObjectCatalogAdapter`，专用于对象存储。
- 已新增 `FileCatalogAdapter`，专用于文件系统。
- MinIO / S3 已迁移到 object adapter。
- NFS 已迁移到 file adapter，并保留唯一 root `name="."`。

### 2. NFS CatalogModel 与能力声明不一致

原 `NewFileCapabilities()` 与 `NFSPlugin.CatalogModel()` 存在不一致，且文件系统目录曾复用对象存储 `prefix` kind。

处理结果：

- `NewFileCapabilities()` 与 `FileCatalogModel()` 已使用同一 helper。
- 文件系统目录 term/kind 已使用 `directory`。
- NFS 顶层 catalog 保持唯一 root 节点：`Name="."`、`Path="/"`。

### 3. Store capabilities 与 Provider 实现不一致

MinIO / S3 历史上声明了未实现或未完整实现的写入 / range 能力，NFS 也曾继承不准确的对象能力。

处理结果：

- 能力声明已收紧为真实实现。
- MinIO / S3 已实现 byte range 读取，并声明 `range_read`。
- MinIO / S3 未声明未实现写能力。
- NFS 未声明 `range_write`，当前只保留已实现的 `stream_read` / `stream_write`。
- 已添加能力声明与 Provider 一致性测试。

### 4. S3 默认 SSL 判断存在 bug

`common/engine/plugins/s3/plugin.go` 中默认 SSL 判断逻辑错误，导致未显式传入 `use_ssl` 时没有按 S3 默认 HTTPS 处理。

处理结果：

- 已抽出 S3-compatible object client helper。
- S3 缺省 `use_ssl` 时使用 true。
- 显式传入 false 时尊重用户配置。
- `TestConnection()` 和 `createClient()` 已共享解析逻辑。

### 5. PostgreSQL catalog 查询存在外部副作用

`common/engine/plugins/postgresql/plugin.go` 的 `listTables()` 在 `reltuples=-1` 时会执行 `ANALYZE`。

问题：

- Catalog / Metadata Provider 应只读。
- `ANALYZE` 会修改外部数据库统计状态，可能带来权限、锁等待和审计问题。

处理结果：

- 已移除 catalog 列表中的 `ANALYZE`。
- `reltuples=-1` 统计未知时不写入 `row_count`。

### 6. SQL 插件重复代码过重

PostgreSQL、MySQL、Doris、ClickHouse、Spark SQL 重复实现大量样板：

- 连接信息解析。
- DSN 构造。
- TestConnection。
- GORM pool 创建。
- QueryRuntime 样板。
- tabular catalog adapter。
- information_schema / system catalog 查询结构。

处理结果：

- 已抽取 SQL runtime 通用 helper。
- 已抽取 GORM connection pool 创建 helper。
- 已抽取 SQL / Mongo / Neo4j DSN 相关 helper。
- MySQL / Doris 已迁移到 MySQL-compatible metadata dialect。
- PostgreSQL、ClickHouse、Spark SQL 已完成连接参数、DSN 和 pool helper 收敛；更深层 metadata 方言仍可后续按需继续收敛。

### 7. MongoDB 和 Neo4j capabilities 手写，容易漂移

MongoDB 和 Neo4j 当前手写完整 `EngineCapabilities`，容易与 `DocumentCatalogModel()` / `GraphCatalogModel()` 漂移。

处理结果：

- 已新增 `NewDocumentCapabilities()`。
- 已新增 `NewGraphCapabilities()`。
- MongoDB / Neo4j 已使用 builder。
- Neo4j metadata 能力按当前实现收紧。

### 8. MongoDB 查询语言声明不一致

MongoDB capabilities 和 `QueryLanguages()` 声明 `mql`，但 sample query fallback 返回 `"json"`。

处理结果：

- 已统一返回 `mql`。
- JSON command 作为 MQL 表达方式，不新增未声明语言。

### 9. 测试覆盖不足

当前测试未覆盖全部插件，也缺少 capabilities 与 Provider 的一致性校验。

处理结果：

- 已建立 capabilities validator。
- 已覆盖全部已注册插件，包括 NFS、Neo4j、Jupyter、Workflow。
- 已校验 `storage.catalog_model` 与 `CatalogModelProvider.CatalogModel()` 一致。
- 已校验 Store / Query / Workflow / Script 能力声明与 Provider 实现一致。

### 10. 旧命名残留

registry / factory / 注释 / 错误消息仍有 `database plugin`、`unsupported database type`、`filesystem` 承载对象存储等旧命名。

处理结果：

- registry/factory 层已清理主路径旧接口命名。
- object / file 通用层已拆分，避免用 filesystem 承载对象存储。
- tabular adapter 内部继续使用 `namespace`，插件局部映射 schema/database。

## 三、改进计划

### 阶段一：能力结构和接口迁移

- [x] 修改 `common/engine/plugin/capabilities.go`：
  - [x] 删除 `StorageCapabilities.Families`。
  - [x] 删除 `StoreCapability.Read` / `Write`。
  - [x] 删除 `StoreCapability.RandomWrite`。
  - [x] 删除 `StoreCapability.AtomicRename`。
  - [x] 删除 `StoreCapability.Transactions`。
  - [x] 删除 `StoreCapability.Formats`。
  - [x] 新增 `StoreCapability.RangeWrite`。
- [x] 修改 `common/engine/plugin/interfaces.go`：
  - [x] `EngineCategory()` 改为 `EngineOrigin()`。
  - [x] 从 `EnginePlugin` 移除 `BuildConnectionString()`。
  - [x] 新增可选 `DSNProvider`。
  - [x] 新增 `RangeReadableProvider` / `RangeWritableProvider`。
- [x] 更新所有插件接口实现。

### 阶段二：能力声明和 builder 迁移

- [x] 更新 `NewTabularCapabilities()`。
- [x] 更新 `NewObjectCapabilities()`。
- [x] 更新 `NewFileCapabilities()`。
- [x] 新增 `NewDocumentCapabilities()`。
- [x] 新增 `NewGraphCapabilities()`。
- [x] 收紧 MinIO / S3 / NFS 的 Store 能力声明。
- [x] MongoDB / Neo4j 改用 builder。

### 阶段三：Catalog adapter 拆分

- [x] 新增 `ObjectCatalogAdapter`。
- [x] 新增 `FileCatalogAdapter`。
- [x] MinIO / S3 切换到 object adapter。
- [x] NFS 切换到 file adapter。
- [x] 保留 NFS root `name="."` 行为。
- [x] 删除或重命名误导性的 `FileSystemCatalogAdapter`。

### 阶段四：连接和 DSN 清理

- [x] 数据库类插件实现 `DSNProvider.BuildDSN()`。
- [x] 非数据库插件删除 JSON connection string 逻辑。
- [x] 清理 `common/engine/plugin/factory.go` / `common/dbbridge/bridge.go` 旧 `BuildConnectionString` 统一入口。
- [x] 评估并迁移 `common/models.BuildConnectionString()` 旧调用方。
- [x] 保持 `connection_info` map 作为统一连接信息事实源。

### 阶段五：真实 bug 和副作用修复

- [x] 修复 S3 默认 SSL。
- [x] 实现或收紧 MinIO / S3 range read。
- [x] NFS 不声明未实现的 `range_write`。
- [x] 移除 PostgreSQL catalog `ANALYZE`。
- [x] 统一 MongoDB sample query language 为 `mql`。

### 阶段六：重复代码收敛

- [x] 抽取 SQL runtime 通用 helper。
- [x] 抽取 GORM pool 创建 helper。
- [x] 抽取 SQL / Mongo / Neo4j DSN helper。
- [x] MySQL / Doris 优先迁移到 MySQL-compatible dialect。
- [x] 抽取 S3-compatible object client helper。

### 阶段七：测试补强

- [x] 新增 capabilities validator。
- [x] 覆盖全部插件注册。
- [x] 校验 Store / Query / Workflow / Script Provider 与能力声明一致。
- [x] 校验 CatalogModel 一致。
- [x] 补 S3 默认 SSL 测试。
- [x] 补 NFS root 行为测试。
- [x] 补 range read provider 声明一致性测试；NFS 未声明 `range_write`，暂不需要 range write 行为测试。

## 四、剩余后续事项

- PostgreSQL、ClickHouse、Spark SQL 的 metadata 查询仍可继续抽成方言 helper，但当前重复面已从连接、DSN、pool 和 MySQL-compatible metadata 层收敛，不影响新规范落地。
- `engine_category` 仍是数据库/API 存量字段名，但代码语义保存 engine origin：`general` / `extension`。是否重命名字段属于独立迁移事项。
- `manager/docs/数据预览API重构方案.md`、`system/docs/tables/engines表.md` 中仍有旧示例值 `standard`，这些是模块历史文档，不属于本 common-engine 唯一跟进清单；若后续整理模块文档，应同步改为 `general`。

## 五、验证记录

本轮已执行并通过：

```bash
go test ./common/engine/plugin ./common/engine/plugins/... ./common/dbbridge ./common/resource
go test -tags integration ./common/engine/plugin/integration -run '^TestPluginInterfaceImplementation'
go test ./common/spatial ./common/utils ./meta/backend/internal/enginecap
go test ./system/backend/internal/service ./manager/backend/internal/service ./manager/backend/internal/api ./manager/backend/internal/mvt
```

## 六、验证建议

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

## 七、暂不建议直接做的事

- 不建议只在 MinIO / S3 / NFS 中局部改字段名，而继续保留对象存储和文件系统共用同一 CatalogAdapter。
- 不建议提前声明未实现能力。
- 不建议继续为新增 SQL 引擎复制整文件。
- 不建议保留旧能力结构或旧路径语义兼容层。

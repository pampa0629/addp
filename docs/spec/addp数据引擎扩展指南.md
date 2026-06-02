# ADDP 数据引擎扩展指南

本文只说明新增一种数据引擎时的开发步骤。接口边界见 [addp引擎插件接口规范.md](addp引擎插件接口规范.md)，能力声明结构见 [addp引擎能力声明规范.md](addp引擎能力声明规范.md)，路径规则见 [addp存储引擎路径体系规范.md](addp存储引擎路径体系规范.md)。

---

## 一、扩展步骤

1. 在 `common/engine/plugins/<engine_type>/` 新建插件包。
2. 实现 `EnginePlugin` 基础接口。
3. 按引擎能力实现需要的 provider。
4. 返回结构化 `Capabilities()`。
5. 按 `EngineOrigin()` 加入 `common/engine/plugins/builtin/general` 或 `common/engine/plugins/builtin/extension` 聚合包。
6. 补充单元测试和必要的 integration 测试。

上层模块通过 `common/engine/plugins/builtin/general`、`common/engine/plugins/builtin/extension` 或 `common/engine/plugins/builtin/all` 统一加载内置插件，不应散落 blank import 具体引擎插件包。`common/dbbridge` 只消费聚合后的插件注册表。

---

## 二、接口选择

| 引擎类型 | 必选接口 | 常用可选接口 |
| --- | --- | --- |
| 关系型 / SQL 表格型 | `EnginePlugin`、`CatalogModelProvider`、`CatalogProvider`、`CatalogFactsProvider`、`SQLQueryRuntimeProvider` | `ConnectionPoolPlugin` |
| 动态 schema 记录集合型 | `EnginePlugin`、`CatalogModelProvider`、`CatalogProvider`、`CatalogFactsProvider`、`QueryRuntimeProvider` | `DynamicSchemaSamplingProvider` |
| 图数据库 | `EnginePlugin`、`CatalogModelProvider`、`CatalogProvider`、`CatalogFactsProvider`、`QueryRuntimeProvider` | `GraphSampleProvider`、`GraphQueryProvider` |
| 对象存储 | `EnginePlugin`、`CatalogModelProvider`、`CatalogProvider`、`CatalogFactsProvider` | `ContentReadableProvider`、`ContentWritableProvider` |
| 文件系统 | `EnginePlugin`、`CatalogModelProvider`、`CatalogProvider`、`CatalogFactsProvider` | `ContentReadableProvider`、`ContentWritableProvider` |
| 工作流 | `EnginePlugin` | `WorkflowRuntimeProvider` |
| Notebook / 脚本 | `EnginePlugin` | `ScriptRuntimeProvider` |

旧 `ListSchemas/ListTables/ListColumns/ListBuckets/ListObjects/ListCollections` 不作为上层接口扩展点。若需要，可作为插件内部 helper，再通过 `CatalogProvider` 适配为统一目录。

---

## 三、Capabilities 要求

新增插件必须返回 `engine.capabilities/v1`：

```go
func (p *MyPlugin) Capabilities() plugin.EngineCapabilities {
    return plugin.EngineCapabilities{
        SchemaVersion: plugin.CapabilitiesSchemaVersion,
        EngineType:    p.Type(),
        EngineFamily:  "tabular",
        Storage: &plugin.StorageCapabilities{
            CatalogModel: plugin.TabularCatalogModel("database"),
            Catalog:      &plugin.CatalogCapability{Supported: true, RealTime: true},
            Facts:        &plugin.CatalogFactsCapability{Supported: true, FieldInfo: true},
            Store:        &plugin.StoreCapability{BatchRead: true},
        },
    }
}
```

声明和实现必须一致：

- `storage.catalog.supported=true` 时必须实现 `CatalogProvider`。
- `storage.facts.supported=true` 时必须实现 `CatalogFactsProvider` 或采样 provider。
- `storage.store.stream_read=true` 时必须实现 `ContentReadableProvider`。
- `storage.store.stream_write=true` 时必须实现 `ContentWritableProvider`。
- `storage.store.range_read=true` 时必须实现 `RangeReadableProvider` 或在 `OpenContent` 中明确支持 offset / length。
- `storage.store.range_write=true` 时必须实现 `RangeWritableProvider`。
- `storage.store.batch_read=true` 时必须实现 `BatchReadableProvider`。
- `storage.store.table_read_session=true` 时必须实现 `TableReadSessionProvider`，用于大表连续批量读取。
- `storage.store.batch_write=true` 时必须实现 `BatchWritableProvider`。
- `storage.store.table_write_session=true` 时必须实现 `TableWriteSessionProvider`，用于跨批次 bulk load / COPY 写入。
- `storage.store.table_write_prepare=true` 时必须实现 `TableWritePreparer`。
- `compute.query.supported=true` 时必须实现对应 query runtime provider。

`storage.families`、`store.read`、`store.write`、`store.random_write`、`store.atomic_rename`、`store.transactions`、`store.formats` 不再作为新增插件能力声明字段。

---

## 四、路径和目录

新增存储引擎必须先定义 Catalog Model：

- root 术语是什么：server、service、root 等。
- root 下第一层业务术语是什么：schema、database、bucket、directory 等。
- Catalog leaf 术语是什么：table、collection、graph、object、file 等。
- full_name 如何计算。
- ResourceLocator 的 path segments 如何由 full_name 转换。

路径规则必须写入 [addp存储引擎路径体系规范.md](addp存储引擎路径体系规范.md)。

对象存储和文件系统必须分别建模：

- 对象存储：`service(root) -> bucket -> prefix -> object`，`Levels` 只包含 `bucket -> prefix -> object`。
- 文件系统：`root -> directory -> file`，`Levels` 只包含 `directory -> file`。

二者不得共享 CatalogModel 或 catalog 拼装实现；最多共享内容流接口、MIME 推断、格式解析等底层 helper。所有存储引擎都必须有显性结构 root；NFS 的 root `name` 使用引擎实例名称，`full_name` 使用空字符串，原生挂载根 `/` 写入 `attributes.catalog.native_name`，`.` 不得进入 catalog path 或元数据路径。

---

## 五、模块自动消费

完成插件与 capabilities 后，各模块按能力自动消费：

- System：注册、连接测试、能力刷新。
- Meta：扫描 catalog，并按需读取 catalog facts 生成元数据快照。
- Manager：展示探查树并预览 item。
- Develop：筛选 query/workflow/script 引擎。
- Service：发布查询服务或空间服务。
- Transfer：当前执行面仍在模块内维护；后续如需统一对接 Reader/Writer，应先形成 Transfer 模块适配层规范。

---

## 六、验证命令

建议至少执行：

```bash
go test ./common/engine/plugin ./common/engine/plugins/...
go test -tags integration ./common/engine/plugin/integration -run '^TestPluginInterfaceImplementation'
go test ./system/backend/internal/service ./meta/backend/internal/service ./manager/backend/internal/service
git diff --check
```

涉及前端入口时补跑对应模块构建。

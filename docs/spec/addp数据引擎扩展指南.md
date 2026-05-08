# ADDP 数据引擎扩展指南

本文只说明新增一种数据引擎时的开发步骤。接口边界见 [addp引擎插件接口规范.md](addp引擎插件接口规范.md)，能力声明结构见 [addp引擎能力声明规范.md](addp引擎能力声明规范.md)，路径规则见 [addp存储引擎路径体系规范.md](addp存储引擎路径体系规范.md)。

---

## 一、扩展步骤

1. 在 `common/engine/plugins/<engine_type>/` 新建插件包。
2. 实现 `EnginePlugin` 基础接口。
3. 按引擎能力实现需要的 provider。
4. 返回结构化 `Capabilities()`。
5. 在 `common/dbbridge/bridge.go` 中 blank import 插件包。
6. 补充单元测试和必要的 integration 测试。

---

## 二、接口选择

| 引擎类型 | 必选接口 | 常用可选接口 |
| --- | --- | --- |
| 关系型 / SQL 表格型 | `EnginePlugin`、`CatalogModelProvider`、`CatalogProvider`、`ItemMetadataProvider`、`SQLQueryRuntimeProvider` | `ConnectionPoolPlugin` |
| 文档型 | `EnginePlugin`、`CatalogModelProvider`、`CatalogProvider`、`ItemMetadataProvider` | `DocumentMetadataSamplingProvider`、`DocumentQueryRuntimeProvider` |
| 图数据库 | `EnginePlugin`、`CatalogModelProvider`、`CatalogProvider`、`ItemMetadataProvider`、`QueryRuntimeProvider` | `GraphQueryProvider` |
| 对象存储 | `EnginePlugin`、`CatalogModelProvider`、`CatalogProvider`、`ItemMetadataProvider` | `ContentReadableProvider`、`ContentWritableProvider` |
| 文件系统 | `EnginePlugin`、`CatalogModelProvider`、`CatalogProvider`、`ItemMetadataProvider` | `ContentReadableProvider`、`ContentWritableProvider` |
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
            Metadata:     &plugin.MetadataCapability{Supported: true, FieldSchema: true},
            Store:        &plugin.StoreCapability{BatchRead: true},
        },
    }
}
```

声明和实现必须一致：

- `storage.catalog.supported=true` 时必须实现 `CatalogProvider`。
- `storage.metadata.supported=true` 时必须实现 `ItemMetadataProvider` 或采样 provider。
- `storage.store.stream_read=true` 时必须实现 `ContentReadableProvider`。
- `storage.store.stream_write=true` 时必须实现 `ContentWritableProvider`。
- `storage.store.range_read=true` 时必须实现 `RangeReadableProvider` 或在 `OpenContent` 中明确支持 offset / length。
- `storage.store.range_write=true` 时必须实现 `RangeWritableProvider`。
- `storage.store.batch_read=true` 时必须实现 `BatchReadableProvider`。
- `storage.store.batch_write=true` 时必须实现 `BatchWritableProvider`。
- `compute.query.supported=true` 时必须实现对应 query runtime provider。

`storage.families`、`store.read`、`store.write`、`store.random_write`、`store.atomic_rename`、`store.transactions`、`store.formats` 不再作为新增插件能力声明字段。

---

## 四、路径和目录

新增存储引擎必须先定义 Catalog Model：

- 第一层术语是什么：schema、database、bucket、root 等。
- 叶子 item 类型是什么：table、collection、label、relationship、object、file 等。
- full_name 如何计算。
- ResourceLocator 的 path segments 如何由 full_name 转换。

路径规则必须写入 [addp存储引擎路径体系规范.md](addp存储引擎路径体系规范.md)。

对象存储和文件系统必须分别建模：

- 对象存储：`bucket -> prefix -> object`。
- 文件系统：`root -> directory -> file`。

二者不得共享 CatalogModel 或 CatalogAdapter；最多共享内容流接口、MIME 推断、格式解析等底层 helper。NFS 必须保留 `name="."` 的 root meta_node，用于容纳挂载根目录下直接存在的文件。

---

## 五、模块自动消费

完成插件与 capabilities 后，各模块按能力自动消费：

- System：注册、连接测试、能力刷新。
- Meta：扫描 catalog 和 item metadata。
- Manager：展示探查树并预览 item。
- Develop：筛选 query/workflow/script 引擎。
- Service：发布查询服务或空间服务。
- Transfer：后续通过 TransferAdapter 对接 Reader/Writer，当前 Transfer 执行面仍在模块内维护。

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

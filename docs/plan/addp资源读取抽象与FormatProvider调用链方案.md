# ADDP 资源读取抽象与 Format Provider 调用链方案

更新时间：2026-05-09

本文定义 format provider 读取实际数据时的调用边界。本文是 next 阶段草案，不代表当前代码已经实现。

## 核心结论

format provider 不通过 `engine id` 自己构造读取器。

正确方向是：

```text
Meta / Manager / Transfer 编排层
  -> 根据 engine capability 构造读取抽象
  -> 将读取抽象交给 format provider
  -> format provider 解码 / 提取 / 编码
  -> data type provider 归一为平台语义
  -> Manager / Transfer 继续组装各自结果
```

这样可以保持三个边界：

- engine 管连接、权限、路径、对象流、原生表游标。
- format 管编码、解码、组件文件和提交策略。
- data type 管 table / document / media / container / graph 的平台语义。

补充一句：`Manager preview` 不应再区分 `filetable` 和 `laketable` 作为对外能力，它只需要一套表预览能力，内部再根据资源组织方式选择不同读取计划。

## 放置位置

这层抽象建议放在 `common/resource`。

理由：

- 这里已经承载了 `ResourceLocator`、资源树、树构建等共享资源概念。
- 这层抽象是平台级的，不属于 format，也不属于具体 engine 实现。
- Meta、Manager、Transfer 都会消费同一套资源定位和读取语义。
- 先放在 `common/resource`，可以保持与现有资源概念同层，而不把接口散到多个模块里。

建议的物理形态可以先很轻：

- `common/resource/reader.go`：`ResourceReader`、`ComponentReader`、`NativeCursor` 概念。
- `common/resource/ref.go`：`ResourceRef`、`ComponentRef`、`ResourceMetadata` 等定位数据。

如果后续证明读取抽象会不断演化，再考虑单独拆成子包；当前不必急着新造目录。

## 为什么不让 format provider 接 engine id

如果 format provider 直接接 `engine id` 并内部构造读取器，会导致：

- format 层反向依赖 engine registry、凭据、连接池和权限。
- 同一个格式在 S3、MinIO、NFS、本地文件系统之间难以复用。
- Manager、Meta、Transfer 难以显式校验 engine capability 与 format capability 是否匹配。
- format provider 容易膨胀成“半个 connector”。

因此，高层 facade 可以为了调用便利接 `engine id`，但它不应被定义为 format provider 本身。

## 当前 engine 能力现状

当前 `common/engine/plugin` 已有可作为底座的能力：

| 能力 | 当前接口 | 作用 |
|---|---|---|
| catalog | `CatalogProvider` | 列举和解析资源路径 |
| metadata | `ItemMetadataProvider` | 获取引擎原生资源元数据 |
| stream read | `ContentReadableProvider` | 打开完整内容流 |
| range read | `RangeReadableProvider` | 打开范围内容流 |
| stream write | `ContentWritableProvider` | 写入完整内容流 |
| range write | `RangeWritableProvider` | 写入范围内容 |
| batch read | `BatchReadableProvider` | 读取引擎原生批次 |
| batch write | `BatchWritableProvider` | 写入引擎原生批次 |

这些能力已经足够支撑第一版编排层适配。

但它们还不是 format provider 的直接输入契约。format provider 不应直接依赖这些 engine provider 接口，而应依赖编排层适配出的读取抽象。

## 建议读取抽象

第一版只需要概念上固定，不急于落最终 Go 接口。

### ResourceRef

`ResourceRef` 表示一个已被 Meta 或调用方确认的资源定位。

它可以来自：

- `meta_item.full_name`
- `attributes.item.component_files`
- `attributes.storage.physical_path`
- engine-native catalog path

`ResourceRef` 不携带 engine 凭据。

### ResourceReader

`ResourceReader` 负责读取单个资源或列举范围。

建议语义：

```text
Open(resource, options) -> io.ReadCloser
Stat(resource) -> ResourceMetadata
List(scope, options) -> []ResourceRef
```

它由 Meta / Manager / Transfer 编排层基于 engine provider 创建。

### ComponentReader

`ComponentReader` 负责多组件格式。

建议语义：

```text
Components() -> []ComponentRef
OpenComponent(role or ref) -> io.ReadCloser
```

Shapefile 这类格式应使用 `ComponentReader` 读取 `.shp/.shx/.dbf/.prj` 等组件。

### NativeCursor

`NativeCursor` 表示引擎原生批量读取能力。

典型来源：

- PostgreSQL table
- MySQL table
- MongoDB collection
- Neo4j query result

这类场景通常不需要 format provider；它直接进入 data type provider 或 Transfer pipeline。

## 调用链示例

### single 文件

```text
Manager / Transfer
  -> ContentReadableProvider.OpenContent
  -> ResourceReader.Open
  -> CSV / JSON / PDF / Image format provider
  -> data type provider
```

### multi 组件文件

```text
Meta 已确认 component_files
  -> 编排层构造 ComponentReader
  -> Shapefile format provider 读取组件
  -> TableProvider + SpatialProvider
```

format provider 不重新枚举 sibling，也不重新推断 component_files。

### whole scope

```text
Meta 已确认 whole scope
  -> 编排层构造 ResourceReader + scope List
  -> Iceberg / OSGB 等 format provider 读取 manifest 或目录结构
  -> data type provider
```

whole scope 的 claims / exclusive 仍由 Meta 负责。

### engine-native table

```text
Transfer / Manager
  -> BatchReadableProvider.ReadBatch 或 QueryRuntimeProvider
  -> TableProvider
```

这类场景没有文件 format，不能为了统一强行塞入 format provider。

## Manager preview 调用链

Manager preview 的最终 DTO 属于 Manager。

底层只提供：

- table rows sample
- document text fragment
- media metadata / thumbnail material
- container children
- graph sample

如果 preview 需要更多信息，Manager 应向 data type provider 提需求；data type provider 再判断需要 engine provider 还是 format provider 支撑。
对于表预览，Manager 不需要知道是文件表、湖表还是原生表，只需要知道当前对象是否可被组织成 `table`，以及需要哪种读取抽象来补齐样本和字段信息。

## Transfer 调用链

Transfer planner 需要同时校验：

- source engine 是否能读取资源。
- source format 是否能从读取抽象解码。
- target format 是否能编码 DataBatch。
- target engine 是否能提交结果。

运行期可以继续适配现有 `pipeline.Reader` / `pipeline.Writer`，但创建方式应逐步从“按 connector type”迁移到“engine + format + data type provider 组合”。

## 暂不做

- 暂不把 `ResourceReader` / `ComponentReader` 固化到 `common/engine/plugin`。
- 暂不要求所有 format provider 一次性迁移。
- 暂不让 Manager preview DTO 下沉到 format 层。
- 暂不让 format provider 直接接 `engine id`。
- 暂不在 `common/format` 中承载这层抽象。

# ADDP 资源读取抽象规范

更新时间：2026-05-09

本文定义平台级资源读取抽象的最小边界。它服务于 Meta、Manager、Transfer 对 FormatPlugin、info provider 和 content reader 的统一调用，不属于 `common/format`，也不直接沉到 `common/engine/plugin`。

核心结论：FormatPlugin、info provider、content reader 不通过 `engine id` 自己构造读取器。编排层先根据 engine capability 构造读取抽象，再把读取抽象交给格式和数据类型能力实现。

## 目标

统一回答三件事：

1. 一个资源怎么被定位。
2. 一个资源怎么被读取。
3. 多组件资源怎么被组合读取。

同时，这层抽象要支持一个关键收口：上层看到的是同一种 data type 能力，例如 `table`，而不是被迫区分文件表、湖表、原生数据库表。

推荐调用方向：

```text
Meta / Manager / Transfer 编排层
  -> 根据 engine capability 构造读取抽象
  -> 将读取抽象交给 FormatPlugin / content reader
  -> FormatPlugin 解码 / 提取 / 编码
  -> info provider / content reader 归一为平台语义或内容数据
  -> Manager / Transfer 继续组装各自结果
```

## 放置位置

建议放在 `common/resource`。

原因：

- `common/resource` 已经承载 `ResourceLocator`、资源树、`TreeBuilder`。
- 这层抽象是平台共享概念，不属于格式实现。
- Meta、Manager、Transfer 都会消费它。

建议先以轻量概念存在，不急于拆子包或扩复杂接口。

## 最小对象

当前代码已在 `common/resource` 落地最小类型：

- `ResourceRef`
- `ResourceMetadata`
- `ComponentRef`
- `ResourceReader`
- `ComponentReader`
- `StaticComponentReader`
- `SameBasenameComponents`
- `FirstResourceByExtension`

它们只表达平台资源读取抽象，不绑定 engine id、连接信息或具体插件类型。

### ResourceRef

`ResourceRef` 表示一个已确认的资源定位，不携带凭据。

它可来源于：

- `meta_item.full_name`
- `attributes.item.component_files`
- `attributes.storage.physical_path`
- engine-native catalog path

为了服务内容读取和批量读取，`ResourceRef` 还应能表达最少的角色信息：

- `main`：主资源。
- `component`：组件资源。
- `manifest`：清单资源。
- `auxiliary`：辅助资源。
- `scope`：目录、prefix、schema 或其他范围型资源。

这样 Manager 在读取 multi / whole 内容时，不需要再猜哪个是主文件、哪个是组件文件。

### ResourceMetadata

`ResourceMetadata` 表示资源的轻量元数据，例如：

- size
- content type
- modified at
- path
- existence
- children count
- format hint
- data type hint

它不等于完整 item attributes。

### ComponentRef

`ComponentRef` 表示组件文件或组件资源中的一个成员。

用于 Shapefile、GeoPackage、Excel、whole scope manifest 等多组件场景。

组件引用至少应能表达：

- 组件角色。
- 组件是否必需。
- 组件的稳定定位。
- 组件在内容读取或解码中的用途。

## 最小读取抽象

### ResourceReader

负责单资源读取或范围列举。

职责：

- 打开单个资源。
- 读取资源元数据。
- 列举范围下资源。
- 按需要提供可重复打开的内容流。

### ComponentReader

负责多组件格式读取。

职责：

- 枚举已确认的组件集合。
- 按组件角色或组件引用打开内容。
- 确保主资源和组件资源使用同一套稳定引用。

典型链路：

```text
Meta / Manager / Transfer 编排层
  -> ComponentReader
  -> FormatPlugin
  -> info provider / content reader
  -> 上层结果组装
```

例如 Shapefile 内容读取时，Manager 只负责根据已入库 `meta_item.full_name` 和 `attributes.item.component_files` 构造 `ComponentReader`；组件物化和格式解析由 FormatPlugin 完成。

### NativeCursor

负责引擎原生批次读取。

适用场景：

- 数据库表。
- 文档集合。
- 图查询结果。

这类场景通常不需要 FormatPlugin 参与编码解码。

## 表格来源的统一处理

Manager、Transfer 面向的是 `data_type=table`，不应把 `filetable`、`laketable` 暴露成两套上层概念。

推荐把表格来源拆成三种内部读取形态：

| 来源形态 | 典型场景 | 读取抽象 | 是否经过 FormatPlugin |
|---|---|---|---|
| single / multi 文件表 | CSV、JSON、Excel、Shapefile、单 Parquet | `ResourceReader` 或 `ComponentReader` | 是 |
| scope 表 | Parquet 目录、Iceberg/Delta/Hudi 类目录表 | `ResourceReader` + scope list，必要时加 manifest ref | 是 |
| engine-native 表 | PostgreSQL、MySQL、MongoDB collection、Neo4j 查询结果 | `NativeCursor` | 通常否 |

上层统一调用 `TableInfoProvider` / `TableSampleReader` 获取字段信息、样本、分页、空间字段等平台语义或内容数据。

`filetable` / `laketable` 是历史残留名，迁移后应彻底删除，不应再作为对外抽象，也不应继续进入新规范。

## 创建方与消费方

### 创建方

`ResourceReader / ComponentReader / NativeCursor` 由 Meta、Manager、Transfer 的编排层根据 engine capability 适配创建。

它们不由 FormatPlugin、info provider 或 content reader 自己构造，也不直接由 engine provider 暴露给上层业务。

当前 engine 层已有可作为底座的能力：

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

这些能力可以支撑第一版编排层适配，但它们不是 FormatPlugin、info provider 或 content reader 的直接输入契约。

### 消费方

FormatPlugin、info provider 和 content reader 只消费这些抽象，不负责创建它们。

info provider 和 content reader 负责把解码结果归一为平台语义或内容数据。

Manager 负责组装最终面向前端的 DTO。

## 调用链

### single

```text
编排层 -> ResourceReader -> FormatPlugin -> info provider / content reader -> Manager/Transfer
```

### multi

```text
Meta 已确认 component_files
  -> ComponentReader
  -> FormatPlugin
  -> info provider / content reader
```

multi 读取必须优先使用 Meta 已确认的 `component_files`。Manager 和 Transfer 不得按扩展名重新枚举 sibling 后猜组件。

### whole

```text
Meta 已确认 whole scope
  -> ResourceReader + scope list
  -> FormatPlugin
  -> info provider / content reader
```

whole 读取必须从 Meta 已确认的 whole scope 根范围出发。scope path、bucket、physical path 等定位事实应来自 `meta_item.full_name` 和标准 attributes；Manager 和 Transfer 不得临时按目录拼装 whole item。

### engine-native

```text
NativeCursor -> info provider / content reader
```

## 内容读取驱动的最小需求

Manager 和 Transfer 的内容读取需求会反向影响这层抽象的最小形状：

- table 内容读取需要主资源的重复打开能力、范围资源列举能力、组件读取能力、分页或采样读取能力、字段样本读取能力。
- document 内容读取需要正文片段读取能力、范围读取能力、元信息读取能力。
- media 内容读取需要原始资源读取能力、元信息读取能力，必要时由上层派生缩略图。
- container 内容读取需要子对象枚举能力、默认入口定位能力。
- graph 内容读取对于引擎原生数据，通常直接走 `NativeCursor`。

因此，这层抽象不应只描述“一个路径能不能打开”，还要能描述“这个资源在内容读取链路里扮演什么角色”。

## 关系边界

1. engine capability 决定能不能构造读取抽象。
2. resource 抽象决定怎么把资源喂给 FormatPlugin。
3. FormatPlugin 决定怎么解码 / 提取。
4. info provider / content reader 决定怎么变成平台语义或内容数据。
5. Manager / Transfer 决定最终业务结果。

如果 FormatPlugin 或 provider / reader 直接接 `engine id` 并内部构造读取器，会导致：

- format 层反向依赖 engine registry、凭据、连接池和权限。
- 同一个格式难以复用于 S3、MinIO、NFS、本地文件系统等不同 engine。
- Manager、Meta、Transfer 难以显式校验 engine capability 与 format capability 是否匹配。
- FormatPlugin 膨胀成半个 connector。

高层 facade 可以为了调用便利接收 `engine id`，但它不应被定义为 FormatPlugin 或底层 provider / reader 本身。

## 设计约束

资源读取抽象只负责资源定位和读取，不负责格式语义和上层展示：

1. 不把接口一次性扩成最终形态，先保持 Meta、Manager、Transfer 可共同消费的最小边界。
2. 不把 `engine id` 作为 FormatPlugin、info provider 或 content reader 的直接输入契约。
3. 不把 Manager / Frontend DTO 放进资源读取抽象。
4. 不把读取抽象放进 `common/format`。
5. 不把这层直接并入 `common/engine/plugin`。

# ADDP 资源读取抽象规范

更新时间：2026-05-09

本文定义平台级资源读取抽象的最小边界。它服务于 Meta、Manager、Transfer 对 format provider 的统一调用，不属于 `common/format`，也不直接沉到 `common/engine/plugin`。

## 目标

统一回答三件事：

1. 一个资源怎么被定位。
2. 一个资源怎么被读取。
3. 多组件资源怎么被组合读取。

同时，这层抽象要支持一个关键收口：上层看到的是同一种 data type 能力，例如 `table`，而不是被迫区分文件表、湖表、原生数据库表。

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

为了服务预览和批量读取，`ResourceRef` 还应能表达最少的角色信息：

- `main`：主资源。
- `component`：组件资源。
- `manifest`：清单资源。
- `auxiliary`：辅助资源。
- `scope`：目录、prefix、schema 或其他范围型资源。

这样 Manager 在预览 multi / whole 场景时，不需要再猜哪个是主文件、哪个是组件文件。

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
- 组件在预览或解码中的用途。

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

当前已用 Manager Shapefile 文件表预览做第一条样板链路：

```text
Manager 编排层
  -> objectStorageResourceReader
  -> StaticComponentReader
  -> format.ComponentTableProvider
  -> Manager preview DTO
```

这条链路已经完成第一版收口：Manager 只负责把 engine provider 适配为 `ResourceReader / ComponentReader`，Shapefile 组件物化和解析由 format provider 内部完成。

当前也已经用 Manager lake table 预览验证了 scope 表链路：

```text
Manager 编排层
  -> fileSystemResourceReader
  -> format.ScopeTableProvider
  -> Manager preview DTO
```

Parquet scope provider 只通过 `ResourceReader.List` 找到 scope 内的 Parquet 资源，再通过 `ResourceReader.Open` 读取内容，不直接依赖 engine plugin。

### NativeCursor

负责引擎原生批次读取。

适用场景：

- 数据库表。
- 文档集合。
- 图查询结果。

这类场景通常不需要 format provider 参与编码解码。

## 表格来源的统一处理

Manager、Transfer 面向的是 `data_type=table`，不应把 `filetable`、`laketable` 暴露成两套上层概念。

推荐把表格来源拆成三种内部读取形态：

| 来源形态 | 典型场景 | 读取抽象 | 是否经过 format provider |
|---|---|---|---|
| single / multi 文件表 | CSV、JSON、Excel、Shapefile、单 Parquet | `ResourceReader` 或 `ComponentReader` | 是 |
| scope 表 | Parquet 目录、Iceberg/Delta/Hudi 类目录表 | `ResourceReader` + scope list，必要时加 manifest ref | 是 |
| engine-native 表 | PostgreSQL、MySQL、MongoDB collection、Neo4j 查询结果 | `NativeCursor` | 通常否 |

上层统一调用 `TableProvider` 获取 schema、样本、分页、空间字段等平台语义。

`filetable` / `laketable` 可以作为过渡期内部路由名或历史 item_type，但不应成为长期 preview provider 的对外抽象。

## 创建方与消费方

### 创建方

`ResourceReader / ComponentReader / NativeCursor` 由 Meta、Manager、Transfer 的编排层根据 engine capability 适配创建。

它们不由 format provider 自己构造，也不直接由 engine provider 暴露给上层业务。

### 消费方

format provider 只消费这些抽象，不负责创建它们。

data type provider 负责把解码结果归一为平台语义。

Manager 负责组装最终 preview DTO。

## 调用链

### single

```text
编排层 -> ResourceReader -> format provider -> data type provider -> Manager/Transfer
```

### multi

```text
Meta 已确认 component_files
  -> ComponentReader
  -> format provider
  -> data type provider
```

当前 Shapefile 样板暂时由 Manager 根据 format layout 构造同 basename 组件集合。后续 Meta 已确认的 `component_files` 应成为优先来源，Manager 不再按扩展名猜组件。

### whole

```text
Meta 已确认 whole scope
  -> ResourceReader + scope list
  -> format provider
  -> data type provider
```

当前 Parquet lake table 预览已经先落成 `ResourceReader + scope list -> ScopeTableProvider`，用于验证 scope 表的最小读取边界。

Manager 侧已经按 engine 类型适配 lake table reader：对象存储走 `objectStorageResourceReader`，文件系统走 `fileSystemResourceReader`。后续要把目录型 lake table 的 scope path、bucket、physical path 固化到 Meta attributes，之后再删除 Manager 中 `filetable` / `laketable` 两套路由名。

当前 Manager 请求层已经区分 `PhysicalPath` 与 `ScopePath`：单文件表使用 `PhysicalPath`，whole/scope 表使用 `ScopePath`。对象存储 reader 会接受 `bucket/prefix` 或 `prefix` 两种 scope path 输入，并在内部归一化，避免重复拼接 bucket。

### engine-native

```text
NativeCursor -> data type provider
```

## 预览驱动的最小需求

Manager 的预览需求会反向影响这层抽象的最小形状：

- table preview 需要主资源的重复打开能力、范围资源列举能力、组件读取能力、分页或采样读取能力、字段样本读取能力。
- document preview 需要正文片段读取能力、范围读取能力、元信息读取能力。
- media preview 需要原始资源读取能力、元信息读取能力，必要时由上层派生缩略图。
- container preview 需要子对象枚举能力、默认入口定位能力。
- graph preview 对于引擎原生数据，通常直接走 `NativeCursor`。

因此，这层抽象不应只描述“一个路径能不能打开”，还要能描述“这个资源在预览链路里扮演什么角色”。

## 关系边界

1. engine capability 决定能不能构造读取抽象。
2. resource 抽象决定怎么把资源喂给 format provider。
3. format provider 决定怎么解码 / 提取。
4. data type provider 决定怎么变成平台语义。
5. Manager 决定最终 preview DTO。

## 不做什么

- 不在这里把接口一次性扩成最终形态。
- 不把 `engine id` 作为 format provider 的直接输入契约。
- 不把 preview DTO 放进资源读取抽象。
- 不把读取抽象放进 `common/format`。
- 不把这层直接并入 `common/engine/plugin`。

# ADDP 内容 I/O 抽象规范

更新时间：2026-05-18

本文定义平台级内容 I/O 抽象的最小边界。它服务于 Meta、Manager、Transfer 对 FormatPlugin、info provider 和 content reader 的统一调用，不属于 `common/format`，也不直接沉到 `common/engine/plugin`。

核心结论：FormatPlugin、info provider、content reader 不通过 `engine id` 自己构造读取器。编排层先根据 engine capability、权限、连接信息和 catalog path 构造 `contentio` 读取 / 写入抽象，再把这些抽象交给格式和数据类型能力实现。

## 命名和位置

正式包名为 `common/contentio`。

`contentio` 表达的是基于 Go `io` 之上的平台内容 I/O：

- `Ref`：一个已确定 content 的定位器。
- `Reader` / `Writer`：按 `Ref` 打开内容流或创建输出流。
- `RangeReader`：按 `Ref` 打开字节范围内容。
- `RelatedRefSpec`：从主 `Ref` 推导相关 refs 的规则。

多个 content 共同组成一个 format item 时，不在 `contentio` 内引入独立的 multi 读写器，而是由 format / dataitem / 调用编排层显式传递 `[]Ref`。

`common/catalogview` 已退出。目录展示、资源树、`ResourceLocator`、DataSourceService 等偏目录视图和服务编排的能力归入 `common/catalogview`；内容 I/O 归入 `common/contentio`；engine 到 contentio 的适配归入 `common/engine/contentadapter`。

engine 到 contentio 的适配放在 `common/engine/contentadapter`。该包可以依赖 `common/engine/plugin` 和 `common/contentio`；`common/contentio` 本身不得依赖 engine。

## 目标

统一回答三件事：

1. 一个 content 怎么被稳定定位。
2. 一个 content 怎么被读取或写入。
3. 多 content 格式如何显式把一组定位器交给格式实现。

推荐调用方向：

```text
Meta / Manager / Transfer 编排层
  -> 根据 engine capability 构造 contentio.Reader / Writer
  -> 对多 content 格式同时传递 []contentio.Ref
  -> 将内容 I/O 抽象交给 common/format provider
  -> FormatPlugin 解码 / 提取 / 编码
  -> info provider / content reader 归一为平台语义或内容数据
  -> Manager / Transfer 继续组装各自结果
```

## Ref

`contentio.Ref` 表示一个已确定 content 的定位器，不携带凭据，不表示 engine 连接。

它可来源于：

- `meta_item.full_name`
- `attributes.item.refs`
- `attributes.storage.physical_path`
- engine-native catalog path 经编排层适配后的内容路径

`Ref` 至少表达：

- `path`：内容路径。
- `name`：显示或辅助识别名称。
- `role`：该 content 在当前读取链路中的角色。
- `required`：多 content 场景下是否必需。
- `primary`：是否为主 content。

`Ref` 是定位器。需要多个 content 时，使用 `[]Ref`；不再把底层内容组合抽象成独立的 `Part`。

角色建议：

| 角色 | 含义 |
|---|---|
| `main` | 主 content |
| `scope` | 目录、prefix、schema 或其他范围型 content |
| `manifest` | 清单 content |
| `auxiliary` | 辅助 content |
| 格式自定义角色 | 如 Shapefile 的 `index`、`attributes`、`projection`、`encoding` |

旧内容组合兼容命名已删除。新代码统一使用 `Ref`、`Reader` / `Writer`、`[]Ref` 这一组语义。

## RelatedRefSpec

`RelatedRefSpec` 不是定位器，而是从一个主 `Ref` 推导相关 refs 的规则。

典型例子是 Shapefile：

| 扩展名 | role | required | primary |
|---|---|---:|---:|
| `.shp` | `main` | 是 | 是 |
| `.shx` | `index` | 是 | 否 |
| `.dbf` | `attributes` | 是 | 否 |
| `.prj` | `projection` | 否 | 否 |
| `.cpg` | `encoding` | 否 | 否 |

编排层可以用 `SameBasenameRefs` 从主路径推导默认相关 refs。但真实读取时，应优先使用 Meta 已确认并入库的 refs，避免重新猜测 sibling 文件。

## Reader / Writer

`Reader` 负责按 `Ref` 打开内容、读取轻量元数据、列举 scope。

`Writer` 负责按 `Ref` 创建输出流。

`RangeReader` 在 `Reader` 基础上提供字节范围读取，适用于 Shapefile `.shx` 索引读取、媒体头部读取、大文件局部采样等场景。

`Reader.List` 只表达对一个 scope ref 的内容列举能力。按扩展名选择、临时目录物化、本地缓存和格式探测等编排工具不属于 `contentio` 核心，应放在 format、dataitem 或具体模块中。

这些接口不负责：

- engine registry 查询。
- 凭据解析。
- 权限判断。
- 格式解析。
- Manager / Frontend DTO 组装。

## 多 content 调用方式

multi 是 data item / format 层的组织方式，不是 `contentio` 的底层 I/O 原语。

当一个格式需要多个 content 时，调用方应显式传递：

```go
reader contentio.Reader
refs   []contentio.Ref
```

写入时显式传递：

```go
writer contentio.Writer
refs   []contentio.Ref
target contentio.Ref
```

Shapefile 这类格式应在 `common/format` 层声明相关 ref 规则，并由调用编排层生成或读取已确认的 refs；格式实现只按 `Ref` 打开 / 创建内容。

`contentio` 不提供 `OpenRole`、`Refs`、`Commit`、`Abort` 这类把多 content 绑定成一个对象的接口。需要事务或清理语义时，应由具体 engine writer、模块编排层或格式 writer 的一次会话负责。

Shapefile 这类格式必须写完整组 refs，不能只写主文件。

## 表格来源的统一处理

Manager、Transfer 面向的是 `data_type=table`，不应把 `filetable`、`laketable` 暴露成两套上层概念。

推荐把表格来源拆成三种内部读取形态：

| 来源形态 | 典型场景 | 读取抽象 | 是否经过 FormatPlugin |
|---|---|---|---|
| single / multi 文件表 | CSV、JSON、Excel、Shapefile、单 Parquet | `Reader`，多 content 时另传 `[]Ref` | 是 |
| scope 表 | Parquet 目录、Iceberg/Delta/Hudi 类目录表 | `Reader` + scope ref/list，必要时加 manifest ref | 是 |
| engine-native 表 | PostgreSQL、MySQL、MongoDB collection、Neo4j 查询结果 | engine-native batch/session | 通常否 |

上层统一调用 `TableInfoProvider` / `TableSampleReader` / `TableReaderProvider` 获取字段信息、样本、分页、空间字段等平台语义或内容数据。

## 创建方与消费方

`contentio.Reader / Writer` 由 Meta、Manager、Transfer 的编排层根据 engine capability 适配创建。

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

这些 engine 能力不是 FormatPlugin、info provider 或 content reader 的直接输入契约。需要时由 `common/engine/contentadapter` 或模块编排层适配为 `contentio`。

## 调用链

### single

```text
编排层 -> contentio.Reader -> FormatPlugin -> info provider / content reader -> Manager/Transfer
```

### multi

```text
Meta 已确认 refs
  -> `contentio.Reader` + `[]contentio.Ref`
  -> FormatPlugin
  -> info provider / content reader
```

multi 读取必须优先使用 Meta 已确认的refs。Manager 和 Transfer 不得按扩展名重新枚举 sibling 后猜 refs。

### scope

```text
Meta 已确认 scope ref
  -> contentio.Reader + scope list
  -> FormatPlugin
  -> info provider / content reader
```

scope 读取必须从 Meta 已确认的 whole scope 根范围出发。

### engine-native

```text
engine-native batch/session -> info provider / content reader
```

## 关系边界

1. engine capability 决定能不能构造内容 I/O 抽象。
2. contentio 决定怎么把 content 喂给 FormatPlugin。
3. FormatPlugin 决定怎么解码 / 提取 / 编码。
4. info provider / content reader 决定怎么变成平台语义或内容数据。
5. Manager / Transfer 决定最终业务结果。

如果 FormatPlugin 或 provider / reader 直接接 `engine id` 并内部构造读取器，会导致：

- format 层反向依赖 engine registry、凭据、连接池和权限。
- 同一个格式难以复用于 S3、MinIO、NFS、本地文件系统等不同 engine。
- Manager、Meta、Transfer 难以显式校验 engine capability 与 format capability 是否匹配。
- FormatPlugin 膨胀成半个 connector。

高层 facade 可以为了调用便利接收 `engine id`，但它不应被定义为 FormatPlugin 或底层 provider / reader 本身。

## 设计约束

内容 I/O 抽象只负责 content 定位和读写，不负责格式语义和上层展示：

1. 不把 `engine id` 作为 FormatPlugin、info provider 或 content reader 的直接输入契约。
2. 不把 Manager / Frontend DTO 放进 `contentio`。
3. 不把 `contentio` 放进 `common/format`。
4. 不把 `contentio` 直接并入 `common/engine/plugin`。
5. 不在 `contentio` 内依赖 engine 插件、MinIO SDK 或具体存储实现。

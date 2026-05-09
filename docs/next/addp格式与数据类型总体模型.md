# ADDP 格式与数据类型总体模型

更新时间：2026-05-09

本文汇总 ADDP 当前关于 engine、format、data type、attributes 的概念共识，作为后续接口、目录和实现收口的总模型。

## 目标

ADDP 的长期目标是：

- 新增 **format** 时，只要它落在既有 **data type** 上，上层消费者尽量不改。
- 新增 **data type** 时，上层消费者必须显式增加对应 provider / 展示 / 转换能力。
- 上层模块不直接感知具体 engine type 或 format type，而是面向平台 provider 调用能力。

## 概念分层

### 1. Engine

`engine` 表示数据的来源和访问方式，例如 PostgreSQL、MySQL、MongoDB、Neo4j、MinIO、S3、NFS、本地文件系统。

engine 主要回答：

- 如何连接。
- 如何枚举资源。
- 如何读取 / 写入原生数据。
- 能提供哪些原生能力。

### 2. Data Item / Meta Item

`data item` 是平台真正管理、预览、检索、授权、传输的核心对象，对应 `meta_item`。

`data item` 的身份由 Meta 决定，不由 format 决定。

### 3. Organization

`organization` 描述一个 data item 是如何从引擎资源中归并出来的：

- `single`
- `multi`
- `whole`

organization 是 Meta 的识别结果，不是 format 的直接结果。

### 4. Data Type

`data_type` 描述 data item 在用户观感和处理方式上是什么：

- `table`
- `document`
- `media`
- `container`
- `graph`
- `unknown`

data type 是平台统一消费层，后续 provider 体系应围绕它组织。

### 5. Format

`format` 描述 item 的编码方式或格式族，例如：

- `csv`
- `json`
- `shapefile`
- `parquet`
- `sqlite`
- `geopackage`
- `pdf`
- `jpeg`

format 本身不是 data item 归并规则，也不是 engine native 对象。

### 6. Item Attributes

`attributes` 指的是 `meta_item.attributes`，即 Meta 落库后的 JSON 扩展属性。

标准分区如下：

- `attributes.storage`
- `attributes.item`
- `attributes.type_info`
- `attributes.format_info`
- `attributes.capabilities`

其中 `attributes.capabilities` 是扫描后的 item 横切事实，不是插件能力声明。

## 能力三层

### Engine Capability

engine capability 表示引擎本身可提供什么能力，例如 catalog、query、read、write、graph query、content read。

### Format Capability

format capability 表示格式实现可提供什么能力，例如识别、解析、样本提取、写出、转换、容器枚举、空间信息提取。

format capability 与 engine capability 使用同一术语风格，都是“某类插件对平台的能力声明”。

### Item Capabilities

item capabilities 指 `meta_item.attributes.capabilities` 中的事实结果，例如：

- spatial
- extraction
- statistics
- partitioning
- indexing

这三层必须分清：

- capability 声明属于插件。
- item capabilities 属于扫描结果。
- 不能互相冒充。

## Provider 模型

后续平台应面向 provider，而不是面向具体 engine type / format type。

### Data Type Provider

data type provider 是平台层对消费能力的统一抽象，参考 engine 的 provider 组织方式。

建议长期围绕这些 data type provider 演进：

- `TableProvider`
- `DocumentProvider`
- `MediaProvider`
- `ContainerProvider`
- `GraphProvider`
- `SpatialProvider` 作为横切 provider

以后如果出现新 data type，例如 `raster` 或 `point_cloud`，再新增对应 provider。

### Provider 的责任

provider 负责把底层能力包装成平台统一语义，例如：

- table schema
- sample / extract
- text extraction
- children enumeration
- spatial facts
- batch read / batch write
- transform input / output

provider 不负责 item 归并，不负责 claims / exclusive，不负责 `meta_item.full_name`。

## Meta / Manager / Transfer 分工

### Meta

Meta 负责：

- 扫描资源树。
- 调用已注册的 resolver。
- 识别 organization。
- 处理 claims / exclusive。
- 生成稳定 meta item。
- 调用 format / data type provider 提取中间结果。
- 由 normalizer 写入最终 attributes。

Meta 不应写死每个具体格式的规则细节。

### Manager

Manager 负责消费已入库的 item。

对于预览，Manager 后端必须能够从 engine / format / data type provider 取得可组装数据，例如：

- table rows sample
- document text fragment
- media metadata / thumbnail material
- container children
- graph sample

Manager 负责组装最终 preview DTO。底层 provider 只提供数据提取结果，不承载 Manager 的展示模型。

与预览无关的 engine type 分支，例如某些空间服务或非预览执行逻辑，可暂不纳入本轮 provider 收口。

### Transfer

Transfer 负责读、写、转换，是后续 provider 体系中最重要的消费者。

Transfer 同时依赖：

- engine capability：数据在哪里、如何访问。
- format capability：数据如何编码 / 解码。
- data type provider：以什么平台语义读写。

Transfer 的批量读写应该由组合模型驱动，而不是单纯按 connector type 路由。

## Meta 调度与格式描述

Meta 识别 item 时，建议综合格式声明和扫描上下文进行统一调度：

1. 先收集已注册 format 的 layout / identification 描述。
2. 结合当前扫描范围生成 whole / multi / single 候选。
3. 由 Meta 排序、判冲突、处理 claims / exclusive。
4. 最后由 fallback 处理未知文本、未知二进制或仅有后缀信息的资源。

format 只提供描述，不提供面向 item 的 `ResolveItems`。

Meta 的调度顺序需要同时考虑：

- 目录 / prefix / manifest 这类 whole scope 候选。
- 多组件格式的组件完整性。
- 单资源格式的兜底吞吐。
- 格式自身优先级和风险。
- 已认领资源是否应继续参与后续识别。

## format 与 data type 的关系

### 原则

1. format 是实现载体。
2. data type 是平台消费面。
3. 新增 format，优先落到既有 data type。
4. 新增 data type，平台上层必须跟进。

### 例子

- CSV -> `table`
- JSON -> `table` 或 `document`
- JSON + 空间结构 -> `table` + `spatial`
- Shapefile -> `table` + `spatial` + `multi`
- PDF -> `document`
- 图片 -> `media`
- SQLite / GeoPackage -> `container`
- Neo4j label / relationship -> `graph`

## 现阶段收口方向

后续实现建议按以下顺序收口：

1. 先把术语定稳：engine capability、format capability、item capabilities、data type provider。
2. 再把消费者调研结论固化为 provider 需求。
3. 再推导 `common/format` 的目录和接口形态。
4. 最后再逐步改 Meta、Manager、Transfer 的调用方式。

## 禁止事项

- 不要让 Manager 继续按扩展名猜测预览逻辑。
- 不要让 Transfer 继续把具体格式名当作唯一入口。
- 不要让 Meta 为每个新格式写一套分支。
- 不要让 `attributes.capabilities` 和插件 capability 混为一谈。
- 不要把 engine native 能力直接暴露给上层业务模块。

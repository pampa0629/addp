# ADDP Format Capability 与 Data Type Provider 接口草案

更新时间：2026-05-09

本文只讨论概念边界，不定义最终 Go 代码。

## 结论

1. `format capability` 与 `engine capability` 术语对齐。
2. `format` 只描述格式自身事实和能力，不决定 item 归并。
3. `Meta` 负责 `organization`、`claims`、`exclusive` 和最终 `meta_item`。
4. 上层尽量通过 `data type provider` 消费能力。
5. `Transfer` 通过 `engine capability + format capability + data type provider` 组合完成读写。

## FormatPlugin

`FormatPlugin` 是 format 的基础声明，角色类似 `EnginePlugin`，但它不处理 item 归并。

```go
type FormatPlugin interface {
    Type() string
    DisplayName() string
    Capabilities() FormatCapabilities
}
```

它只回答三件事：

- 这是什么格式。
- 这个格式长什么样、如何被识别。
- 这个格式能给平台提供哪些能力。

## FormatCapabilities

`FormatCapabilities` 是格式能力总览，不是 parser 注册表，也不是 `meta_item.attributes.capabilities`。

建议按下面几类理解：

| 能力段 | 说明 | 典型产出 | 主要消费者 |
|---|---|---|---|
| Identification | 如何识别格式 | 扩展名、MIME、magic bytes、内容签名 | Meta、Registry |
| Layout | 这个格式怎么组织资源 | single / multi / whole、主资源、组件规则、manifest 规则 | Meta |
| Parse / Extract | 这个格式能提取什么结构信息 | table / document / media / container / spatial facts | Meta、Manager、Transfer |
| Sample / Extract | 这个格式能否提供展示或处理所需的样本和提取结果 | 行样本、文本片段、缩略图素材、容器树原始结构 | Manager、Meta、Transfer |
| Transfer | 这个格式能否做批量读写和组件提交 | batch read / write、component read / write、commit policy | Transfer |
| Provider hints | 这个格式实现了哪些 provider 家族 | table / document / media / container / graph / spatial | Registry、上层调用方 |

这里的“能力段”只是在说格式实现能提供什么，不是在说最终 attributes 里一定写什么。

### FormatIdentificationCapability

识别能力只负责“看起来像什么格式”，不负责决定 item。

建议关注这些信息：

- `Extensions`：文件名后缀提示。
- `MIMETypes`：传输层或存储层 MIME 提示。
- `MagicBytes`：文件头或固定签名提示。
- `ContentSignatures`：内容结构签名，例如 JSON 数组、特定 manifest、组件文件组合。
- `DefaultDataType`：该格式通常落到哪个 data type。
- `SupportedLayouts`：支持 `single`、`multi`、`whole` 中的哪些布局。
- `Priority`：仅用于注册表冲突排序，不表示 item 优先级。
- `SupportsFallback`：是否允许作为通用文本 / 二进制兜底。

要点：

- `.json` 不能直接等同于空间格式。
- 如果 JSON 结构带空间含义，format 仍然是 `json`，空间语义由 `spatial` 横切能力表达。

### FormatLayoutCapability

这部分回答的是“格式自己如何组织资源”，不是“Meta 如何识别 item”。

建议关注这些信息：

- `ScopeKind`：单文件、目录、prefix、schema、manifest scope。
- `PrimaryResourceRole`：主资源是普通文件、manifest、目录入口，还是整个范围本身。
- `RequiredComponents`：必须出现哪些资源。
- `OptionalComponents`：可以出现哪些附加资源。
- `SameNameRule`：是否要求同 basename、同 prefix、同后缀集。
- `CrossDirectoryAllowed`：是否允许跨目录认领组件。
- `WholeScopeExclusive`：是否天然需要整段范围一起认领。

这类信息的价值是让 Meta 可以统一编排组织方式：

- 单文件格式只需要一个资源。
- 多文件格式需要一组组件资源。
- whole scope 格式需要先认领整个范围，再决定是否落库。

示例：

- CSV：`single`，一个主文件即可。
- Shapefile：`multi`，主资源是 `.shp`，同 basename 的 `.shx/.dbf/.prj` 构成组件。
- Iceberg：`whole`，需要 manifest 和目录范围。

### FormatParse / Extract 能力

这部分回答“格式能让平台抽出什么结构事实”。

常见输出包括：

- table：字段、行数、主键、采样、索引提示。
- document：标题、页数、作者、正文摘要、语言。
- media：图片宽高、时长、编码、颜色模式。
- container：内部子对象列表、默认入口、数量。
- spatial：几何列、SRID、extent、空间索引提示。

这里要强调两点：

1. `spatial` 是横切能力，不是独立 data type。
2. JSON 的空间结构也应该落到 `json + spatial`，不再单独引入 `geojson` 作为顶层格式。

### Format Sample / Extract 能力

这一层不直接负责 Manager preview，只负责提供 Manager 组装 preview 所需的数据提取结果。

它可以提供：

- 表格行样本。
- 文档文本片段。
- 图片缩略图素材或媒体元信息。
- 容器内部对象树。
- 图结构采样。

这些结果仍是格式或 data type 层的中间数据，不是 Manager 面向前端的 preview DTO。

### FormatTransfer 能力

Transfer 能力只关心格式能否参与批量读写和组件提交。

需要明确的点包括：

- 是否支持 batch read。
- 是否支持 batch write。
- 是否支持 component read。
- 是否支持 component write。
- 是否支持原子提交或分步提交。
- 多文件格式的提交边界如何定义。

Format writer 负责把平台批次编码成格式输出，真正写到目标存储仍要结合 engine writer 或 storage writer。

## Data Type Provider

`data type provider` 是上层消费者的主入口，目标是让上层不直接感知具体 `engine type` 或 `format type`。

建议围绕这些家族收口：

- `TableProvider`
- `DocumentProvider`
- `MediaProvider`
- `ContainerProvider`
- `GraphProvider`
- `SpatialProvider` 作为横切 provider

### TableProvider 先行原则

`TableProvider` 是最先需要收口的 data type provider，因为它同时覆盖：

- Manager 的表预览。
- Transfer 的批量读写。
- Meta 对 table 型结果的结构提取。

它应先作为平台最小可用的 provider 标准，再按同一套方式推导其他 provider。

#### TableProvider 需要回答的问题

`TableProvider` 只回答表这个 data type 的平台语义，不回答格式识别，也不回答 item 归并。

它至少要覆盖这些问题：

- 这个对象是不是表。
- 这个表的 schema 是什么。
- 这个表能否分页或采样读取。
- 这个表能否返回行样本。
- 这个表是否带空间列、SRID、extent 等空间信息。
- 这个表能否批量读写。
- 这个表来自单文件、多组件、scope，还是 engine-native。

#### TableProvider 的输入边界

TableProvider 的输入应该尽量轻，只拿已经由上层确认的信息：

- 资源定位信息。
- 已确认的 data item 片段。
- 必要的读取参数，例如分页、采样大小、字段选择。

它不应再自行判断 organization，不应重新枚举 sibling，也不应重新推断 format。

#### TableProvider 的输出边界

TableProvider 的输出建议按平台语义组织，而不是按底层格式组织：

- schema / columns
- rows / sample rows
- total / page / page size
- field metadata
- geometry columns
- SRID / extent
- batch read / write 所需的最小结构信息

如果未来有更多 table 语义，例如分区信息、索引提示、默认排序、主键候选，也应优先落到 TableProvider，而不是塞进 Manager 专用 DTO。

#### TableProvider 的最小能力集

为了避免把 TableProvider 写得过重，第一版只需要固定四类能力：

1. `Describe`：返回表结构与字段元数据。
2. `Sample`：返回分页或采样样本。
3. `ReadBatch`：返回批量读取所需的结构或数据。
4. `WriteBatch`：接受批量写入所需的结构或数据。

这里的 `Describe`、`Sample`、`ReadBatch`、`WriteBatch` 不是最终 Go 方法名，只是能力分组。  
如果某个 data source 只支持只读，那么它可以只实现前两类；如果只需要预览，也可以不实现 `WriteBatch`。

当前代码已先落地一个更小的过渡形态：

```text
common/format.Provider
common/format.TableProvider
common/format.ComponentTableProvider
common/format.ScopeTableProvider
common/format.ProviderRegistry
```

第一版只覆盖：

- `DescribeTable`
- `SampleTable`

它不接 engine id，不构造读取器，不返回 Manager DTO。  
内置文件表格式已经直接注册为 `TableProvider`，Manager 文件表预览也已经迁到 provider registry。旧文件表主接口和注册 API 已删除。

当前已经补了两个表格来源扩展入口：

- `ComponentTableProvider`：用于 Shapefile 这类多组件格式，组件读取由 `common/resource.ComponentReader` 提供。
- `ScopeTableProvider`：用于 Parquet 目录表这类 scope 表，范围列举和内容读取由 `common/resource.ResourceReader` 提供。

这两个入口仍然只返回 table 语义，不返回 Manager preview DTO。后续再逐步补 `ReadBatch` / `WriteBatch`；Transfer 读写单独处理，不和本轮 Manager 预览收口混在一起。

当前 Manager lake table 预览、Manager object content 的 GeoJSON / Parquet 内容预览，以及 Meta lake table schema 提取都已经走 `TableProvider` / `ScopeTableProvider`，不再直接依赖具体 parser 或 engine 绑定 helper。

代码口径上，JSON 表格 provider 位于 `common/format/json`。它支持 GeoJSON FeatureCollection 这类 JSON 空间表结构，但注册的顶层格式仍然是 `FormatJSON`。

#### TableProvider 与 SpatialProvider

`SpatialProvider` 不是另一个表类型，而是横切能力。

推荐的关系是：

- `TableProvider` 负责表的通用语义：schema、rows、分页、批量读写。
- `SpatialProvider` 负责空间事实：geometry columns、SRID、extent、空间索引提示。
- 如果一个表带空间信息，`TableProvider` 可以直接暴露最小结果，但更完整的空间细节应由 `SpatialProvider` 补齐。
- 如果没有 `SpatialProvider`，表预览仍然应该可用，只是空间信息更少。

这样可以避免把 `table` 再拆成 `spatial table`、`geo table` 之类的重复 provider。

### DocumentProvider

`DocumentProvider` 面向文档型 data type，例如 PDF、Word、Markdown、纯文本、富文本集合、文档型数据库记录。

它的核心职责不是“给出最终预览样式”，而是提供可组装的文档提取结果。

#### DocumentProvider 需要回答的问题

DocumentProvider 至少要回答：

- 这个对象是不是文档。
- 这个文档有哪些元信息，例如标题、页数、作者、语言、编码。
- 这个文档是否支持正文片段读取。
- 这个文档是否支持范围读取或页级读取。
- 这个文档是否支持文本提取、摘要或结构化抽取。

#### DocumentProvider 的输出边界

DocumentProvider 的输出应该尽量稳定，优先提供这些中间结果：

- 文档元信息。
- 正文片段。
- 页码或范围上下文。
- 提取状态。
- 可选的原始内容句柄或访问引用。

Manager 不应直接依赖 `DocumentProvider` 的展示格式，而应在此基础上组装当前前端需要的文档预览。

### MediaProvider

`MediaProvider` 面向图片、音频、视频等媒体型 data type。

它关心的是媒体的可访问性和可派生预览材料，而不是最终 UI 呈现。

#### MediaProvider 需要回答的问题

MediaProvider 至少要回答：

- 这个对象是不是媒体。
- 这个媒体的基本元信息是什么，例如宽高、时长、编码、帧率、颜色模式。
- 这个媒体是否支持原始内容访问。
- 这个媒体是否支持缩略图、封面或预览素材派生。
- 这个媒体是否支持必要的解码或转码前信息提取。

#### MediaProvider 的输出边界

MediaProvider 的输出应该主要是：

- 媒体元信息。
- 缩略图或封面素材。
- 可访问内容引用。
- 可选的编码 / 解码辅助信息。

如果某个媒体对象只是一个对象存储文件，那么 `MediaProvider` 也不应直接假设它一定能提供图像预览；它只能返回已经确认的事实和可用素材。

### Document / Media 的共性

Document 和 Media 都属于“先提取、后组装”的 provider：

- 它们不负责 Manager 的最终展示 DTO。
- 它们不负责 item 归并。
- 它们不应该直接依赖 `engine id` 去重建读取器。
- 如果只有最小读取能力，也应该优先返回最小可用事实，而不是硬凑一个完整预览对象。

每个 provider 只回答对应 data type 的问题：

- Table：schema、记录样本、batch read/write。
- Document：元信息、文本片段、文本提取。
- Media：元信息、缩略图、播放或可访问内容。
- Container：内部对象、默认入口、子对象样本。
- Graph：图结构、采样、关系样本。
- Spatial：几何列、SRID、extent、空间索引提示。

### ContainerProvider

`ContainerProvider` 面向容器型 data type，例如目录、压缩包、Excel 工作簿、SQLite/GeoPackage、文档集合、带内部对象树的复合格式。

它关心的是容器内部结构，而不是具体内容的最终展示。

#### ContainerProvider 需要回答的问题

ContainerProvider 至少要回答：

- 这个对象是不是容器。
- 这个容器有哪些子对象。
- 这个容器的默认入口是什么。
- 这个容器内部是否支持按路径或角色读取成员。
- 这个容器是否支持内部对象树枚举或样本抽取。

#### ContainerProvider 的输出边界

ContainerProvider 的输出应该主要是：

- 子对象列表。
- 默认入口。
- 内部对象定位。
- 必要的容器统计信息。

它不负责把内部对象再解释成最终的 table / document / media 预览；那部分应交给对应 data type provider 继续处理。

### GraphProvider

`GraphProvider` 面向图型 data type，例如图数据库查询结果、图结构抽样、节点-关系模型。

它关心的是节点、关系和图样本，而不是关系数据库表格展示本身。

#### GraphProvider 需要回答的问题

GraphProvider 至少要回答：

- 这个对象是不是图。
- 这个图的节点和关系结构是什么。
- 这个图是否支持采样或局部展开。
- 这个图是否支持按标签、类型或路径读取。
- 这个图是否支持从引擎原生结果归一成平台语义。

#### GraphProvider 的输出边界

GraphProvider 的输出应该主要是：

- 节点样本。
- 关系样本。
- 图统计信息。
- 可选的图查询结果归一结构。

GraphProvider 不应该直接把结果包装成某个特定前端图组件 DTO，而应先保持平台语义，再由 Manager 组装。

### Container / Graph 的共性

Container 和 Graph 都是“结构优先”的 provider：

- 它们先回答结构，再回答内容。
- 它们不负责最终展示 DTO。
- 它们不应重新绑定完整 engine 模型。
- 它们的输出最好能被 Manager、Meta、Transfer 共用。

### provider 输入边界

provider 输入应该尽量轻，不要堆成过重的 `EngineID + ItemID + Locator + Attributes + Options`。

更合理的是三类信息：

- 定位信息：已经由 Meta 确认的资源定位或引擎原生对象定位。
- 已确认属性片段：调用该 provider 所需的 `item`、`type_info`、`format_info`、`capabilities` 子集。
- 调用参数：分页、字段选择、采样大小、目标格式选项等。

provider 不应该重新绑定完整 engine 模型，也不应该接收完整 meta item 后自行解释所有 attributes。

### 读取入口边界

format provider 不应通过 `engine id` 自己构造读取器。

更合适的方式是由外层编排者先根据 engine capability 组装读取抽象，再把这个读取抽象交给 format provider：

```text
Meta / Manager / Transfer
  -> Engine provider 构造 ResourceReader / ComponentReader / NativeCursor
  -> Format provider 解码内容
  -> Data type provider 归一为平台语义
```

原因：

- format 层不需要知道 MinIO、S3、本地文件、NFS 或数据库连接细节。
- 同一个 format provider 可以复用于多个 engine。
- engine 凭据、权限、连接池、重试和审计留在 engine 层。
- Transfer planner 可以显式校验 engine capability 与 format capability 的组合是否成立。

如果为了调用便利提供高层 facade，可以让 facade 接收 `engine id`，但 facade 不应被视为 format provider 本身。

### engine 读取能力现状

当前 `common/engine/plugin` 已经具备部分可组合底座：

- `CatalogProvider`：列举和解析资源路径。
- `ContentReadableProvider`：打开完整内容流。
- `RangeReadableProvider`：打开范围读取流。
- `ContentWritableProvider` / `RangeWritableProvider`：写入内容。
- `BatchReadableProvider` / `BatchWritableProvider`：读取或写入引擎原生批次。
- `StoreCapability`：声明 stream / range / batch 读写能力。

这些能力足够作为后续读取抽象的基础，但还不是稳定的 format provider 输入契约。

暂时不建议为了 format provider 立即扩展 engine 基础接口。更稳妥的做法是先定义平台级读取抽象，例如：

```text
ResourceReader
  Open(ctx, resourceRef, readOptions) -> io.ReadCloser
  Stat(ctx, resourceRef) -> ResourceMetadata
  List(ctx, scopeRef, listOptions) -> []ResourceRef

ComponentReader
  OpenComponent(ctx, role) -> io.ReadCloser
  Components() -> []ComponentRef
```

这层抽象可以先由 Manager / Transfer / Meta 的编排层基于现有 engine provider 适配出来。等真实场景验证稳定后，再决定是否把它沉淀进 `common/engine/plugin`。

### Manager preview 边界

`TablePreview`、`DocumentPreview`、`MediaPreview` 这类对象更像 Manager 的展示 DTO，不宜放在 format 层作为通用返回值。

更合理的分工是：

- format provider 提供格式原生可解析的结构事实或记录样本。
- data type provider 把不同来源整理成 table / document / media / container 的平台语义。
- Manager service 再组装成当前前端需要的 preview DTO。

如果 Manager preview 需要新增字段，应向 data type provider 或底层 format / engine provider 提需求；不能把 Manager 的 DTO 反向下沉为 format 层通用模型。

### Batch read / write 边界

批量读写和 preview 有类似问题，但边界更硬。

format 层只负责：

- 把资源流解码成 DataBatch。
- 把 DataBatch 编码成目标格式资源。
- 说明组件文件和提交策略。

engine 层负责：

- 打开对象流。
- 列举目录 / prefix。
- 写入对象或原生表。
- 提交事务或对象集合。

Transfer planner 负责把两者组合起来。format provider 不应该直接知道任务、item、engine 配置的完整业务模型。

## Transfer 组合模型

Transfer 不能只按 `connector type` 路由，也不能只看 format。

它需要同时看三类信息：

| 维度 | 作用 |
|---|---|
| engine capability | 数据在哪里，如何连接，如何列举，如何读写原生资源 |
| format capability | 数据如何编码 / 解码，如何组织组件，如何提交 |
| data type provider | 以什么平台语义组织 schema、样本、batch、children |

### 读取链路

典型读取链路是：

1. engine 打开资源或列举对象，形成读取抽象。
2. format 基于读取抽象解码为平台批次或中间结构。
3. data type provider 补充 schema、样本、空间信息或容器结构。

### 写入链路

典型写入链路是：

1. data type provider 提供待写入的结构语义。
2. format 把批次编码成目标格式。
3. engine 负责对象写入、目录提交或原生表写入。

### 批量读写需要额外确认的约束

- 是否支持列表 / 分区 / 多组件。
- 是否支持 seek / checkpoint。
- 是否支持原子提交。
- 是否支持并行写。
- schema 和空间字段由谁提供。
- format write 与 engine write 的提交边界在哪里。

## 与现有代码的关系

| 当前代码 | 目标方向 |
|---|---|
| `common/format` 的 parser / extractor 命名 | 收敛为 format capability + provider 语义 |
| `meta/backend/internal/metaitem` 的 detector | 继续留在 Meta，负责 item 归并 |
| `manager` 的预览分支 | 尽量转到 data type provider |
| `transfer` 的 reader / writer / loader | 逐步改为 planner + engine provider + format provider |

## 暂不纳入

- 不在 format 层做 item resolver。
- 不在 format 层直接决定 organization。
- 不在 provider 输入里过度绑定 engine type。
- 不把 `geojson` 作为独立顶层格式。

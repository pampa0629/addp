# ADDP 数据类型与格式能力规范

本文定义 `common/format` 中的 FormatPlugin、FormatDescriptor、info provider、content reader 和 writer provider 边界，并规定它们如何消费 `common/datatype` 中的通用 data type / type info 模型。它只约束格式和数据类型能力，不定义 Meta 的 item 归并实现。

概念边界见 [ADDP 数据类型和格式体系图](../concepts/addp数据类型和格式体系图.md)，资源链条和模块边界见 [ADDP 数据项体系图](../concepts/addp数据项体系图.md)，资源读取边界见 [ADDP 内容 I/O 抽象规范](addp内容IO抽象规范.md)。

## 本文边界

| 本文负责 | 不在本文定义 |
|---|---|
| FormatDescriptor 表达格式静态事实，plugin registry 表达当前进程实现状态 | data item 如何归并和 claims 如何合并 |
| FormatPlugin 如何声明格式身份和布局 | `meta_item.name/full_name/item_type` 来源规则 |
| info provider 如何提供 `common/datatype` 定义的 data type info、横切事实和 format info | `meta_item.attributes` 的完整 schema |
| content reader 如何提供内容数据 | contentio Reader / Writer 与 `[]format.RelatedRef` 的具体接口 |

item 归并见 [ADDP 数据项探测器规范](addp数据项探测器规范.md)，attributes 写入见 [ADDP 元数据 attributes 规范](addp元数据attributes规范.md)。

## 核心原则

格式能力分为两层：`FormatDescriptor` 声明格式静态事实，`RegisterFormatPlugin` 注册当前 Go 进程唯一格式实现入口；具体 provider / reader / writer 能力由已注册 plugin 的接口断言动态得到。它不同于 `meta_item.attributes.capabilities`；后者是扫描后的 item 事实结果。

`common/format` 只回答格式和数据类型能力实现问题：

- 这个资源像什么格式。
- 这个格式如何组织资源。
- 这个格式能提取什么结构事实。
- 这个格式能否通过 content reader 提供样本、文本、原始内容，以及是否有 reader / writer provider 支持批量读写。
- 解码结果如何归一为 `common/datatype` 定义的平台 data type 语义。

其中 `xxx info` 是对应 data type 的通用元数据，例如 `datatype.TableInfo`、`datatype.DocumentInfo`、`datatype.MediaInfo`、`datatype.ContainerInfo`。这些结构的事实源属于 `common/datatype`，不属于 `common/format`。`xxx info provider` 只负责从具体格式中提取并返回这类元数据；样本、文本片段、缩略图、raw content、range content 等内容数据必须通过独立 content reader 表达。

## `common/datatype` 与 `common/format` 分工

`common/datatype` 是 ADDP 通用数据类型语义的事实源：

| 类型 | 归属 | 说明 |
|---|---|---|
| `DataType` | `common/datatype` | `table`、`document`、`media`、`container`、`graph`、`unknown` |
| `FieldType` / `FieldInfo` | `common/datatype` | 平台通用字段类型和字段语义 |
| `TableInfo` / `DocumentInfo` / `MediaInfo` / `ContainerInfo` / `GraphInfo` | `common/datatype` | 各 data type 的通用 type info |
| `SpatialInfo` | `common/datatype` | 空间横切事实，落点是 `attributes.capabilities.spatial` |
| `AccessIndex` | 暂居 `common/datatype` | 内容读取索引，落点是 `attributes.access_index.<data_type>`；不是 data type，也不是 type info |

`AccessIndex` 当前放在 `common/datatype` 只是因为 format、Meta 和 Manager preview 都需要复用同一 JSON 结构。它不属于 data type identity 或 `type_info` 主事实，不应作为新增 datatype 的依据。后续只有在 engine range reader、format access index 和 Meta attributes 的边界稳定后，才考虑把它迁到独立的访问索引包。

`file` 不是 ADDP 基础 `DataType`，不得新增 `DataTypeFile`、`FileInfo` 或 `attributes.type_info.file`。文件、对象、目录、bucket、prefix、root 等是 catalog / storage 形态；当内容语义无法识别时，`item.data_type` 统一写为 `unknown`，存储事实写入 `attributes.storage`，格式身份写入 `attributes.item.format`，格式私有事实写入 `attributes.format_info.<format>`。

`common/format` 负责格式侧能力：

- format identity、descriptor、诊断快照和 detection。
- FormatPlugin、info provider、content reader、reader/writer provider 发现。
- 具体格式解析、编码、解码和格式私有信息。
- native type mapper，将格式或引擎原生类型转换为 `datatype.FieldType`。

`common/format` 不再定义通用 data type、field type 或 type info。新增格式实现不得在格式包内新增平行的 `FieldType`、`TableInfo`、`DocumentInfo` 等公共模型。

`common/format` 不定义任何展示概念。页面展示策略、前端渲染器选择和 Manager 返回给前端的 DTO 均属于 Manager / Frontend 边界。

`common/format` 不回答 item 归并问题：

- 不决定最终 `layout`。
- 不决定 claims、exclusive、refs。
- 不决定 `meta_item.full_name`。
- 不直接写最终 `meta_item.attributes`。

Meta 负责把 format descriptor 声明、运行时实现状态、扫描上下文和已认领资源编排成最终 data item。

## FormatPlugin 抽象接口

FormatPlugin 是一个文件格式在 `common/format` 中的主入口。新增格式时，应优先实现 FormatPlugin，并按需同时实现对应的 info provider 和 content reader。

当前抽象接口位于 `common/format/provider.go`，概念形态如下：

```go
type FormatPlugin interface {
    Format() FormatType
}

type FormatDescriptorProvider interface {
    FormatPlugin
    Descriptor() FormatDescriptor
}
```

| 方法 | 负责 | 不负责 |
|---|---|---|
| `Format()` | 返回稳定格式 ID，例如 `csv`、`parquet`、`shapefile` | 不根据输入资源动态猜格式 |
| `Descriptor()` | 返回格式身份、默认数据类型、布局和识别规则 | 不表达某个 data item 的扫描结果，也不声明当前进程 provider / reader / writer 实现状态 |

FormatPlugin 是稳定格式身份入口，不是 detector。`Descriptor()` 是可选静态描述能力，不属于 FormatPlugin 本体：

- 它可以声明“CSV 是 `format=csv`”；如果同时实现 `FormatDescriptorProvider`，还可以声明“默认 `data_type=table`，支持 `single`，扩展名是 `.csv`”。
- 它可以同时实现 `TableInfoProvider`、`TableSampleReader`、`TableReaderProvider`、`TableWriterProvider` 等能力；这些是当前进程可调用实现状态，通过接口断言发现。
- 它不能决定“当前某个文件最终是不是一个 data item”。
- 它不能决定 Shapefile refs如何归并成最终 item；只能声明ref 布局能力，最终归并由 Meta detector 裁决。
- 它不能通过 `engine_id` 自己构造读取器；调用方必须先构造 `io.Reader`、`contentio.Reader`，并在多 content 场景下显式传递 `[]format.RelatedRef`。

### FormatDescriptor

`FormatDescriptor` 是格式的静态身份说明。新增格式至少要说明：

| 字段 | 必填性 | 说明 |
|---|---|---|
| `ID` | 必填 | 稳定 plugin / descriptor ID，例如 `builtin-csv` |
| `Format` | 必填 | 稳定 format ID，例如 `csv` |
| `DataType` | 必填 | 默认数据类型，例如 `table`、`document` |
| `Layouts` | 必填 | 支持的内容布局，例如 `single`、`multi`、`whole` |
| `Identification` | 文件格式通常必填 | 扩展名、MIME、内容签名等识别依据 |

`Descriptor()` 只描述格式身份、识别规则、默认 data type 和 layout。实际 Go 进程里是否已经具备 info provider、content reader 或 writer provider，只能由当前已注册 `FormatPlugin` 是否实现对应接口动态判断，不能写入 descriptor。

内置格式的 descriptor 由各自格式包维护，代码位置为 `common/format/plugins/<format>/`。`common/format` 根包负责 descriptor 注册、查询和冲突诊断，不再集中保存一份内置格式大清单。即使某个格式当前只有静态声明、暂时没有解析 provider，也应建立对应格式包，并让 plugin 实现 `FormatDescriptorProvider`，例如逻辑格式、文档族、媒体族或 ORC / Avro 这类 descriptor-only 阶段能力。

扩展名、MIME、content signature 等格式识别事实优先来自 `FormatDescriptor.Identification`。`common/format` 根包中的 detection fallback 只服务“内置 descriptor 尚未加载时仍可做最低限度识别”和 magic bytes 等算法性判断，不是第二套格式规范清单。fallback 只允许保留 bootstrap 主扩展名 / 主 MIME；完整扩展名、MIME 变体和 wildcard 识别必须来自 descriptor。新增或调整格式时，必须先更新对应格式包的 `Descriptor()`；只有能说明独立价值的通用兜底，才允许进入 fallback。

`IsDocumentFormat`、`IsTableFormat`、`IsImageFormat` 等分类 helper 是 descriptor `DataType` / MIME 事实的派生判断，不维护独立格式分类表。某些格式具备表格解析能力但默认 data type 不是 `table` 时，分类结果仍以 descriptor 为准，例如 Excel 默认属于 `container`，不属于 `IsTableFormat`。

上层模块不得自行把文件扩展名、MIME 或自由字符串转换成 format ID。消费已有 attributes、用户输入、插件返回值或文件名时，必须通过 `format.NormalizeFormat` 得到 canonical format；识别不到必须保留为 `format=unknown`。禁止把裸后缀或未知字符串写入 `meta_item.attributes.item.format`、container child format、Manager preview request format 或 Transfer planner format 字段。

### 注册方式

格式实现注册有两层：

| 注册层 | 入口 | 作用 |
|---|---|---|
| 格式身份声明 | `RegisterFormatDescriptor` 或 `FormatDescriptorProvider.Descriptor()` | 让平台知道这个 format 是谁、默认 data type 是什么、如何识别 |
| 代码实现注册 | `RegisterFormatPlugin` | 注册该 format 的唯一 Go 实现入口，当前进程通过接口断言判断可调用能力 |

推荐做法：

```go
func init() {
    if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
        panic(err)
    }
}
```

`RegisterFormatPlugin` 会记录 plugin 的 `Format()` 身份；若 plugin 同时实现 `FormatDescriptorProvider`，会校验 `Format()` 与 `Descriptor().Format` 完全一致，并在当前进程尚未注册该 format descriptor 时注册 descriptor。后续能力查询统一对该 plugin 做接口断言：

| plugin 同时实现 | 可通过 |
|---|---|
| `FormatInfoProvider` | `GetFormatInfoProvider(format)` |
| `TableInfoProvider` | `GetTableInfoProvider(format)` |
| `TableSampleReader` | `GetTableSampleReader(format)` |
| `MultiTableInfoProvider` | `GetMultiTableInfoProvider(format)` |
| `MultiTableSampleReader` | `GetMultiTableSampleReader(format)` |
| `ScopeTableInfoProvider` | `GetScopeTableInfoProvider(format)` |
| `ScopeTableSampleReader` | `GetScopeTableSampleReader(format)` |
| `TableReaderProvider` | `GetTableReaderProvider(format)` |
| `MultiTableReaderProvider` | `GetMultiTableReaderProvider(format)` |
| `TableWriterProvider` | `GetTableWriterProvider(format)` |
| `MultiTableWriterProvider` | `GetMultiTableWriterProvider(format)` |
| `DocumentInfoProvider` | `GetDocumentInfoProvider(format)` |
| `DocumentTextReader` | `GetDocumentTextReader(format)` |
| `MediaInfoProvider` | `GetMediaInfoProvider(format)` |
| `ContainerInfoProvider` | `GetContainerInfoProvider(format)` |
| `ContainerChildResolver` | `GetContainerChildResolver(format)` |

内置格式通过 `common/format/builtin` 统一 blank import 加载。Meta、Manager 或测试进程如果需要完整内置格式识别、provider / reader / writer 和 type mapper，应显式导入：

```go
import _ "github.com/addp/common/format/builtin"
```

新增内置 format ID 时，必须同时完成：

1. 在 `common/format/plugins/<format>/` 实现 `FormatDescriptorProvider.Descriptor()`。
2. 在同一格式包内按需实现 provider / reader；没有 Go 解析能力时也保留 descriptor-only plugin。
3. 在 `common/format/builtin/init.go` 加入 blank import，使内置加载入口能触发注册。

已有 format 补充 provider / reader 能力时，只修改对应格式包，不新增集中 descriptor。

### 子目录封装要求

新增文件格式应放在 `common/format/plugins/<format>/`，原则上一个子目录完成：

- `plugin.go`：实现 `FormatPlugin`，按需实现 info provider / content reader。
- `parser.go` 或其他内部文件：放格式解析细节。
- `*_test.go`：验证 descriptor、provider / reader 和关键样本读取。

上层模块不应 import 具体格式子目录调用内部 parser；只通过 `common/format` 的 registry 和接口获取能力。

## FormatDescriptor 与能力分层

格式能力总览不是 parser 注册表，也不是 `meta_item.attributes.capabilities`。静态事实由 `FormatDescriptor` 表达，当前进程实现状态由已注册 plugin 的接口断言表达。

| 能力段 | 说明 | 典型产出 | 主要消费者 |
|---|---|---|---|
| Identification | 如何识别格式 | 扩展名、MIME、magic bytes、内容签名 | Meta、Registry |
| Layout | 格式自身如何组织 content | single / multi / whole、primary ref、related refs 规则、manifest 规则 | Meta |
| Info / Facts | 能提取什么 data type info、横切事实和 format info | table / document / media / container info、spatial facts、access index、format_info | Meta、Manager、Transfer |
| Content Reader | 能否提供按 data type 组织后的内容数据 | 行样本、文本片段、容器树、二进制片段 | Manager、Transfer |
| Runtime provider | 当前进程是否已经加载可调用的 reader / writer provider | batch read / write、multi read / write、commit policy | Transfer |

当前代码中 `FormatDescriptor` 是静态声明事实源；`FormatPlugin` 是格式包的代码入口；`FormatDescriptorProvider` 是 plugin 可选实现的静态描述能力；`FormatInfoProvider`、`TableInfoProvider`、`TableSampleReader`、`MultiTableInfoProvider`、`MultiTableSampleReader`、`ScopeTableInfoProvider`、`ScopeTableSampleReader`、`TableReaderProvider`、`TableWriterProvider`、`MultiTableReaderProvider`、`MultiTableWriterProvider`、`DocumentInfoProvider`、`DocumentTextReader`、`BinaryContentReader`、`MediaInfoProvider`、`ContainerInfoProvider`、`ContainerChildResolver`、type mapper 等是具体实现能力。静态声明可以先于 plugin / provider 完整实现存在，因此文档必须区分“声明支持”和“已有实现”。

### Format Identity 与 Format Detection

`format identity` 定义“平台支持的这个格式是谁”，由 `FormatType`、`FormatPlugin.Format()` 和 `FormatDescriptor.Format` 共同约束。`FormatDescriptor` 只包含稳定格式 ID、默认 data type、layout 和识别事实；info provider、content reader、writer provider 和横切事实提取能力属于已注册 plugin 的动态实现状态。

`format detection` 是“给定一个 content，判断它像哪个格式”的动态过程。它输入文件名、MIME、magic bytes、内容签名或 ref 上下文，输出指向某个 format identity 的识别结果。

当扩展名、MIME、内容签名、plugin sniffer 和 magic bytes 都不能识别格式时，轻量文本探测属于 format detection 的最后兜底：若内容前缀可判定为 UTF-8 文本，则返回 `format=text`，上层再据此落为 `data_type=document`；否则继续保持 `format=unknown`，由 unknown 的 `BinaryContentReader` 处理非文本 raw binary 兜底。

`NormalizeFormat` 是 format-like 字符串的归一化入口。它可以接受 canonical format、扩展名、MIME 或文件名，但输出只能是已知 format identity 或 `unknown`。它不读取内容，因此不会把 `.yml`、`.conf` 等未知扩展名直接判为 `text`；这类资源只有在调用方传入内容前缀并经过 `DetectFormat(filename, peek)` 的文本兜底后，才可以升级为 `format=text`。

二者边界如下：

| 维度 | Format Identity | Format Detection |
|---|---|---|
| 回答的问题 | 平台支持哪些格式以及这些格式能做什么 | 当前 content 看起来是什么格式 |
| 性质 | 静态注册事实 | 动态识别过程 |
| 输入 | plugin / descriptor 注册信息 | 文件名、MIME、magic bytes、内容片段、ref 上下文 |
| 输出 | format descriptor / capability snapshot | detection result，指向某个 format |
| 是否决定 item | 不决定 | 不最终决定，只给 Meta detector 提供格式候选 |

Shapefile 这类 multi 格式尤其要区分：单个 `.shp/.dbf/.shx` 的识别不等于 data item 归并；最终 item 边界由 Meta item detector 根据 format layout 和候选 content 上下文决定。

### Identification

识别能力只负责“content 看起来像什么格式”，不负责决定 item。

建议说明：

- `Extensions`：支持的后缀。
- `MIMETypes`：支持的 MIME。
- `MagicBytes`：支持的文件头签名。
- `ContentSignatures`：支持的内容结构特征。
- `DefaultDataType`：默认落点数据类型。
- `SupportedLayouts`：支持的布局类型。
- `Priority`：识别冲突时的排序权重。
- `SupportsFallback`：是否能作为兜底格式处理。

`Priority` 只用于识别排序，不表示 item 优先级。`.json` 不能直接等同于空间格式；带空间结构的 JSON 仍是 `format=json`，空间语义由 `spatial` 横切能力表达。

### Layout / Layout

`layout` 描述格式能力：某个 format 理论上支持怎样的 content layout。`layout` 描述 data item 识别结果：某个已确认 item 最终采用哪一种内容布局。二者使用同一组取值 `single`、`multi`、`whole`，但语义层级不同。

建议说明：

- `ScopeKind`：单 content、相关内容、目录、prefix、schema、manifest scope。
- `PrimaryRefRole`：primary ref 是普通文件、manifest、目录入口，还是整个 scope 本身。
- `RequiredRefs`：必须出现哪些 related ref。
- `OptionalRefs`：可以出现哪些附加 related ref。
- `SameNameRule`：是否要求同 basename、同 prefix、同后缀集。
- `CrossDirectoryAllowed`：是否允许跨目录匹配 ref。
- `WholeScopeExclusive`：是否天然需要整段范围一起认领。

示例：

- CSV：`single`，一个 content 即可。
- Shapefile：`multi`，primary content 加同 basename related refs。
- Iceberg：`whole`，整体范围认领。

这一组能力只服务 Meta 调度，不直接等于 item 结果。dataitem 结果字段直接使用 `format.Layout` 取值，避免形成两套命名体系。

### Info / Facts

Info / facts 能力负责把原始资源转成平台能理解的类型信息、横切事实、访问索引和格式私有事实。

常见产出：

- `type_info.table`
- `type_info.document`
- `type_info.media`
- `type_info.container`
- `format_info.<format>`
- `capabilities.spatial`
- `capabilities.extraction`
- `capabilities.statistics`
- `access_index.<data_type>`

表格解析能力应说明字段名、原始字段类型、行数、主键等 table info 是否可得。文档解析能力应说明标题、页数、语言、字数等 document info 是否可得；正文抽取状态属于 `capabilities.extraction`，不属于 `DocumentInfo`。媒体提取能力应说明宽高、时长、基础编码、颜色模式等通用 media info 是否可得；EXIF、视频 codec、音频 codec、帧率、采样率、码率、轨道数等细粒度事实暂不进入 `datatype.MediaInfo`，需要保留时进入受控 `format_info.<format>` 或 `capabilities.extraction`。容器能力应说明内部对象、默认入口和对象摘要是否可得。空间能力应说明 geometry columns、primary geometry column、SRID / CRS、extent 和 spatial index 是否可得。访问索引能力应说明索引类型、定位单位、锚点和失效判断来源是否可得。

Provider 一次解析可能同时得到多个事实。为避免污染各 data type 的 type info，应通过 describe result 或等价结构将这些事实同级返回，再由 Meta normalizer 写入各自 attributes 分区。

表格描述结果属于 `common/format` provider 边界，不属于 `common/datatype`。概念形态：

```go
type TableDescribeResult struct {
    Table        *datatype.TableInfo
    Spatial      *datatype.SpatialInfo
    AccessIndex *datatype.AccessIndex
    FormatInfo   map[string]interface{}
}
```

映射规则：

| 结果字段 | 规范落点 |
|---|---|
| `Table` | `attributes.type_info.table` |
| `Spatial` | `attributes.capabilities.spatial` |
| `AccessIndex` | `attributes.access_index.table` |
| `FormatInfo` | `attributes.format_info.<format>` |

media 已使用同一原则：`MediaDescribeResult.Media` 写入 `type_info.media`，`MediaDescribeResult.Spatial` 写入 `capabilities.spatial`，`MediaDescribeResult.FormatInfo` 写入 `format_info.<format>`。后续 document、container、graph 如果也存在“一次解析产出多个事实”的场景，应继续按同级结果表达，不能把横切事实或访问索引塞进各自 `TypeInfo`。

空间表的几何字段遵循以下规则：

- `FieldTypeGeometry` 只表达字段语义，不固定行值编码。
- `datatype.SpatialInfo` 是空间事实入口，负责表达主空间字段、几何类型、SRID / CRS、extent、dimension 和空间索引等信息；它不属于 `datatype.TableInfo`。
- table sample 默认返回 WKT 字符串，便于 Manager 预览、日志和调试。
- continuous table reader 可以通过 `ParseOptions.GeometryEncoding` 请求 `wkt`、`wkb` 或 `ewkb`。默认值为 `wkt`。
- `wkb` / `ewkb` 行值使用 `[]byte` 表达，供 Transfer 等批处理链路在目标 writer 明确支持时使用；调用方不得假定所有 engine writer 都能直接接收二进制几何参数。
- SRID 优先由 `SpatialInfo.SRID` 表达；`ewkb` 可以携带 SRID，但不能替代 schema 级空间事实。
- 各格式 native 几何类型必须在对应 format plugin 内转换为 ADDP 通用几何值，不得把 `shp.Shape` 等 native 类型暴露到 format 根接口、engine 或 Transfer 执行层。
- 格式写出空间数据时，应根据 `SpatialInfo.GeometryType` 和 `SpatialInfo.Dimension` 选择自身 native 表达。例如 Shapefile writer 在 `dimension >= 3` 时写出 `PointZ`、`PolyLineZ`、`PolygonZ` 或 `MultiPointZ`；M / measure 不属于 ADDP 当前标准空间维度，除非后续有明确 measure 规范，否则不得伪装为 Z 坐标。

注意：`type_info.*` 只保存对应 data type 的元数据。内容样本、原始内容、缩略图、文本片段不是 info，不能为了上层使用方便塞进 `table info`、`document info` 或 `media info`。空间、时间、统计、提取、语义等横切事实进入 `capabilities`；内容读取索引进入 `access_index`；格式私有事实进入 `format_info`。

容器型 data item 的父级 `type_info.container.children` 只保存轻量子对象索引，例如 child 名称、真实入口名、类型、行数、列数和默认入口。子对象的完整字段信息、行样本和分页内容属于该 child 自身，应在指定 child 后继续调用对应 table / document / media info provider 或 content reader 获取；父容器不能把所有 child 的 `fields`、`rows` 等内容展开塞进自身 attributes。

容器 child、multi ref 和嵌套容器路径必须区分：

- `child_name` 表示当前容器第一层可寻址子对象，例如 Excel sheet、SQLite table、ZIP entry 或已归并的 Shapefile child。
- `ref_path` 只表示 multi 组织格式的ref 路径，例如 Shapefile 的 `.shp`、`.dbf`、`.shx`。它不表示容器内部继续下钻的路径。
- `nested_child_path` 表示当前 child 本身还是容器时继续寻址其内部对象的相对路径，例如 `data/cities.csv` 或 `inner.zip/data/cities.csv`。

Manager 可以把这三个概念暴露为预览请求参数，但 `common/format` 只负责提供 `ContainerChildResolver`、ref 描述和 content reader 能力，不定义 Manager 的 HTTP DTO。

### Content Reader

content reader 负责提供上层可继续组装的数据，不直接负责展示协议，也不等于最终 attributes。

常见结果包括：

- table rows sample
- document text fragment
- media thumbnail
- container children sample
- graph sample
- binary content fragment

Manager 面向前端的 DTO 由 Manager 组装，不应进入 `common/format`。`common/format` 只声明和实现需要格式解码的 reader 能力，例如 `TableSampleReader`、`DocumentTextReader`、`BinaryContentReader`。`raw_content`、`range_content`、预签名 URL 和直接下载属于 engine / contentio / 模块 fetcher 的内容通道能力，不写入 format descriptor，也不作为 format plugin 的静态声明。是否使用这些能力做页面展示、下载、索引或传输，由上层模块决定。

合理链路是：

```text
Manager / Transfer / Meta 构造 contentio.Reader 或 io.Reader
  -> data type info provider 或 content reader 消费输入
  -> 返回 type_info / format_info / 内容数据
  -> Manager / Transfer / Meta 各自按模块边界继续组装
```

不合理链路是：

```text
FormatPlugin 根据 engine id 自己读取文件
FormatPlugin 返回 Manager 专用 DTO 或前端渲染器建议
common/format 使用前端渲染器参与展示决策
```

### Transfer

Transfer 模块负责根据当前进程已注册的 reader / writer provider 判断批量读写、ref 读写和提交边界；`common/format` 的 descriptor 不声明 `TransferRead` / `TransferWrite`。

建议说明：

- `BatchRead`
- `BatchWrite`
- `MultiRead`
- `MultiWrite`
- `CommitPolicy`

Format writer 负责编码格式，Engine writer 负责提交到目标存储。多文件格式必须明确提交边界，不能只写主文件。Transfer 应基于 `TableReaderProvider`、`TableWriterProvider`、`MultiTableReaderProvider`、`MultiTableWriterProvider`、`ScopeTableReaderProvider` 等具体实现状态判断可读写能力。更细的读取抽象和ref 定位规则见 [ADDP 内容 I/O 抽象规范](addp内容IO抽象规范.md)。

## 能力诊断快照

`common/format.ListFormatCapabilitySnapshots()` 是由 descriptor 列表和当前进程已注册 plugin 接口断言临时派生的诊断入口，不是业务事实源。业务调用方需要可执行能力时，应直接调用对应 `Get*Provider` / `Get*Reader` / `Get*Writer`。

| 字段 | 含义 |
|---|---|
| `descriptor` | format 静态事实，包括身份、默认 data type、layout、识别规则 |
| `implementations` | 当前进程内已注册 plugin 实际实现状态，表示 `FormatDescriptorProvider`、`FormatInfoProvider`、`TableInfoProvider`、`TableSampleReader`、`MultiTableInfoProvider`、`MultiTableSampleReader`、`ScopeTableInfoProvider`、`ScopeTableSampleReader`、`TableReaderProvider`、`TableWriterProvider`、`MultiTableReaderProvider`、`MultiTableWriterProvider`、`DocumentInfoProvider`、`DocumentTextReader`、`BinaryContentReader`、`MediaInfoProvider`、`ContainerInfoProvider`、`ContainerChildResolver` 等是否已经可调用 |

上层模块做格式路由时，应优先依据 `format`、`data_type`、`layouts` 判断语义；需要后端实现时直接查询具体 provider / reader。不能从 descriptor 推断某个 Go reader 一定可调用；例如 DOCX / PPTX 当前已有内置 `DocumentTextReader`，WPS 当前只有 document 静态身份和前端/raw 内容处理路径。

`raw_content` / `range_content` 不进入 `FormatDescriptor`，也不进入 format plugin registry。它们是 engine capability、`common/contentio`、预签名 URL 或模块 fetcher 的内容通道能力；需要实际解码或抽取时，使用 `DocumentTextReader`、`TableSampleReader`、`BinaryContentReader` 等已注册插件能力。

Manager 的 `preview_material` 是前端展示材料或展示状态协议，和 `content_readers` 不同层。`preview_material=raw_binary` 表示响应体里携带 base64 原始字节或展示层按原始二进制处理；`preview_material=unsupported` 表示 Manager 不支持该内容在线预览。它们都不是 `raw_content`、`range_content` 或 `binary_content` 声明。format descriptor 中的能力名称不得写入 `preview_material`。

能力诊断快照不替代内置格式规范，也不作为实现进度清单。首批内置格式的确定性落地规则见 [ADDP 内置数据类型与文件格式规范](addp内置数据类型与文件格式规范.md)；当前代码实现状态以 `common/format/README.md` 和测试为准；未完成事项进入 `docs/next/common-format格式完善矩阵.md`。

新增或调整格式后，应检查能力诊断快照：

1. `descriptor` 是否只包含格式身份、默认 data type、layout 和识别规则。
2. `implementations` 是否只反映当前已注册 plugin 的 Go 接口实现。
3. 对未知二进制对象，能力诊断快照不应新增普通 `binary` format；应通过内置 `unknown` format 的 `BinaryContentReader` 表达非文本 raw binary 兜底，上层再投影成自身展示或传输协议。

## 旧 FileMetadataExtractor 退出说明

早期 `FileMetadataExtractor` 按 MIME 类型提取增强元数据，返回结构混合了 storage info、type info、format info、capabilities 和内容数据，和 `xxx info provider` / content reader 主线存在概念冲突。该旁路机制已从 `common/format` 删除，新增格式不得再引入同类注册表。

当前边界：

1. 可确定的 data type 元数据进入对应 `TableInfoProvider`、`DocumentInfoProvider`、`MediaInfoProvider` 或 `ContainerInfoProvider`。
2. 格式私有元信息进入 `FormatInfoProvider`，由 Meta normalizer 写入 `format_info.<format>`。
3. 样本、连续读取会话、文本片段、缩略图、raw content、range content 等进入 content reader / reader provider。
4. Meta 只负责编排 provider / reader 结果并写入标准 attributes 分区；按需对象元信息提取也必须走 FormatInfoProvider / MediaInfoProvider 等主线能力。

## Info Provider 与 Content Reader

info provider 和 content reader 是上层消费者面向数据类型能力的主入口，目标是让上层不直接感知具体 `engine type` 或 `format type`。

建议围绕四类能力收口：

| 类别 | 职责 | 示例 |
|---|---|---|
| info provider | 提供对应 data type 的元数据，供 Meta 写入 `type_info.*` | `TableInfoProvider`、`DocumentInfoProvider`、`MediaInfoProvider`、`ContainerInfoProvider` |
| sample / text reader | 提供按 data type 组织后的轻量内容数据，供 Manager / Search / 轻量探查消费 | `TableSampleReader`、`DocumentTextReader`、`MediaThumbnailReader` |
| continuous reader provider | 打开一次连续读取会话，供 Transfer 等批处理消费 | `TableReaderProvider`、`MultiTableReaderProvider`、`ScopeTableReaderProvider` |
| writer provider | 打开一次连续写出会话，供 Transfer 写侧消费 | `TableWriterProvider`、`MultiTableWriterProvider` |

info provider 只回答对应 data type 的元数据语义；sample / text reader 只回答预览、探查和轻量片段读取；continuous reader provider / writer provider 面向全量批处理。它们都不回答格式识别，也不回答 item 归并。

### Table Info / Sample / Reader / Writer Provider

表格型 info provider 和 content reader 是最先需要稳定的数据类型能力，因为它同时覆盖 Meta 表结构提取、Manager 内容查看和 Transfer 批量读写。

表格能力按消费意图拆成四类：

- `TableInfoProvider`：返回表结构与字段元数据，是 `type_info.table` 的主来源；如果同次解析得到空间事实、访问索引或格式私有事实，应通过 describe result 同级返回。
- `TableSampleReader`：返回分页或采样样本，是 Manager 表格预览和轻量探查的主来源，不能用于 Transfer 全量读取。
- `TableReaderProvider`：打开单资源 table 的连续读取会话，是 Transfer 读取 encoded table 的主入口。
- `TableWriterProvider`：打开单资源 table 的连续写出会话，是 Transfer 写出 encoded table 的主入口。
- `MultiTableInfoProvider` / `MultiTableSampleReader`：面向 Shapefile 等 multi table 的类型信息与样本读取。
- `MultiTableReaderProvider` / `MultiTableWriterProvider`：面向 Shapefile 等 multi table 的连续读写。
- `ScopeTableInfoProvider` / `ScopeTableSampleReader`：面向 Parquet dataset 等 whole scope table 的类型信息与样本读取。
- `ScopeTableReaderProvider`：面向 Parquet dataset 等 whole scope table 的连续读取。Transfer 读取 whole scope table 时必须使用连续 reader，不得用 sample reader 冒充全量读取。

这些能力可以由同一个格式实现同时提供，也可以分别提供。新增能力必须按 info、sample、continuous reader、writer 明确拆分，不再新增同时表达多种消费意图的组合 provider。

字段选择属于 table data type 的通用读取语义，不得作为某个格式的私有能力；术语使用 `field_selection`，不得使用容易与 GIS 坐标投影混淆的 projection。

Transfer 消费 encoded table 时按 layout 选择 provider：

| layout | 读取 / 写出入口 | 规则 |
|---|---|---|
| `single` | `TableReaderProvider` / `TableWriterProvider` | 单个 file/object 通过 contentio Reader / Writer 进入格式 reader / writer。 |
| `multi` | `MultiTableReaderProvider` / `MultiTableWriterProvider` | Shapefile 等多 ref table 必须显式传入 `[]format.RelatedRef`；读取时优先使用 Meta 已确认 refs。 |
| `whole` | `ScopeTableReaderProvider` | Parquet dataset 等 whole scope table 从已确认 scope ref 出发，并结合 contentio Lister 递归读取；第一版不定义通用 whole scope writer。 |

Transfer planner 只根据 data type、representation、layout、format descriptor 和 provider 实现状态选择上述通用入口，不得为 NFS -> MinIO、PostgreSQL -> PostgreSQL、Shapefile -> PostgreSQL 等具体组合建立专用格式路径。

后续完整表格能力至少要覆盖：

- 表结构和字段元数据。
- 分页或采样读取。
- 行样本。
- 空间列、SRID、extent 等空间信息，作为 `datatype.SpatialInfo` 横切事实返回，不进入 `datatype.TableInfo`。
- 批量读取和批量写入边界。

能力可按四类组织：

- `Describe`：返回表结构与字段元数据。
- `Sample`：返回分页或采样样本。
- `ReadSession`：打开连续读取会话。
- `WriteSession`：打开连续写出会话。

这些名称是能力分组，不要求直接成为最终 Go 方法名。只用于元数据和预览的实现可以只实现 `Describe` / `Sample`；不参与 Transfer 写侧的实现不必实现 `WriteSession`。

### Checkpoint / Resume 契约

checkpoint / resume 属于批量执行的恢复契约，不是 sample offset，也不是简单的行号分页。格式层只负责表达“如何把格式内部读取位置稳定地保存并重新打开”，Transfer 只负责保存 marker、校验任务语义未变化并在恢复时传回 provider。

第一版统一术语：

| 术语 | 说明 |
|---|---|
| `resume_marker` | provider 生成并消费的不透明恢复标记，必须可 JSON 序列化。Transfer 不解析其中字段，只保存、回传和用于日志展示。 |
| `position_unit` | marker 的位置单位，例如 `row`、`byte`、`ref_row`、`row_group`。它只用于能力说明和诊断，不作为 Transfer 分支条件。 |
| `read_position` | reader 已经稳定消费到的位置，表示下次恢复应从其后继续。 |
| `commit_position` | writer 已经稳定提交到的位置。encoded writer 通常只有 `Close()` 成功后才产生可恢复提交点。 |
| `fingerprint` | 由 provider 生成的输入结构指纹，用于判断 marker 是否仍适用于当前资源，例如文件大小、mtime、ETag、ref 列表、schema 摘要或 row group 元信息。 |

规则：

1. `TableSampleReader.SampleTable(offset, limit)` 的 `offset` 是样本窗口，不是 resume marker。
2. `TableReaderProvider` / `MultiTableReaderProvider` / `ScopeTableReaderProvider` 如要支持 checkpoint resume，必须通过 `ParseOptions.ResumeMarker` 消费恢复标记；不得让 Transfer 通过类型判断具体格式后自行 seek。当前 Go 草案使用 `common/resume.Marker` 和 `ResumeMarkerProvider`。
3. `resume_marker` 必须由 provider 解释。Parquet 可以表达 `{part_ref,row_group,row_offset}`，Shapefile 可以表达 `{primary_ref,row_offset}`，CSV 可以表达 `{byte_offset,row_offset}`；这些都是格式私有 marker 内容，不进入 Transfer 决策。
4. provider 必须在 marker 中或旁路返回 `fingerprint`。资源变化、schema 变化、refs 变化或读取选项变化时，provider 必须拒绝恢复，而不是静默从错误位置继续。
5. provider 暂未实现 marker 消费时，收到 `ParseOptions.ResumeMarker` 或 `WriteOptions.ResumeMarker` 必须显式返回 unsupported error，不得静默忽略后从头读取或覆盖写入。
6. writer 支持 resumable 的前提是能证明目标提交边界幂等。普通 encoded 文件写出会话通常不支持中途恢复；multi ref writer 也不能只恢复某个 ref 的局部状态。当前 Go 草案通过 `WriteOptions.ResumeMarker` 消费恢复标记，并通过 `CommitMarkerProvider` 暴露提交标记。
7. Transfer checkpoint 中的 `checkpoint_offset` 仍只表示观测进度；真正恢复必须使用 provider 生成的 `common/resume.Marker`。

`SpatialProvider` 不是另一个表类型，而是横切 provider。表格内容查看没有空间细节时仍应可用，只是空间信息更少。

### DocumentInfoProvider / DocumentTextReader

`DocumentInfoProvider` 和 `DocumentTextReader` 面向 PDF、Word、Markdown、纯文本、富文本对象、文档型数据库中的单条阅读型记录等 document data item。MongoDB collection 这类动态 schema 记录集合不属于该 provider 边界。

`DocumentInfoProvider` 提供：

- 文档元信息。
- 页码或范围上下文。

`DocumentTextReader` 提供正文片段。raw content / range content 由对应 content reader 表达。二者都不负责 Manager 的最终前端 DTO。

文档格式不要求都必须在后端完成解析。DOCX / PPTX 当前可以通过内置 `DocumentTextReader` 提取正文片段；WPS 当前不声明后端文本解析能力，原始文件读取由 engine / contentio / URL 内容通道提供。后续如需 WPS、PDF OCR、摘要、脱敏或服务端导出，再补对应文本提取或外部 extraction 能力，不能扩展 `DocumentInfo` 承载正文状态或正文内容。

### BinaryContentReader

`unknown` 是内置 format identity，默认 data type 为 `unknown`。它只提供 `BinaryContentReader`，用于“不认识且非文本”的 raw binary 内容兜底。

`BinaryContentReader` 接收调用方传入的 `io.Reader`，按 limit 返回原始字节片段和截断状态。它不做文本判断，不生成 `type_info.binary`，不把 binary 定义为新的 data type 或 format，也不返回 Manager DTO。上层模块应先把文本内容识别为 `data_type=document, format=text` 并走 `DocumentTextReader`；只有剩余 unknown 非文本内容才走 `format.GetBinaryContentReader(format.FormatUnknown)`。

### MediaInfoProvider

`MediaInfoProvider` 面向图片、音频、视频等媒体型 data item。

它提供：

- 媒体元信息，例如 `kind`、`mime_type`、宽高、时长、`encoding`、`color_space`、`size_bytes`。
- 可选的同级横切事实，例如 GeoTIFF / GPS 解析得到的 `SpatialInfo`。

缩略图、raw content、range content 或可流式 URL 应由对应 content reader / engine 能力表达。`MediaInfoProvider` 只返回已经确认的事实，不硬凑完整展示对象。EXIF、视频 codec、音频 codec、帧率、采样率、码率、轨道数等细粒度事实不进入 `datatype.MediaInfo`；如需持久化，先补 `format_info.<format>`、`capabilities.extraction` 或新的横切能力规范。

### ContainerInfoProvider / ContainerChildResolver

`ContainerInfoProvider` 和 `ContainerChildResolver` 面向目录、压缩包、Excel 工作簿、SQLite / GeoPackage、文档归档包等容器型 data item。MongoDB collection 这类动态 schema 记录集合不属于 container provider 边界。

它提供：

- 子对象列表。该列表是轻量索引，只应包含 child 定位和摘要信息，不承载完整字段数组或行样本。
- 默认入口。
- 内部对象定位。
- 容器统计信息。

它不负责把内部对象解释成最终 table / document / media 内容数据；那部分交给对应 info provider 或 content reader 继续处理。Excel sheet、SQLite table / view 等 child 的字段和行数据必须通过 child 定位参数按需读取。

Meta 默认只做父容器识别和一层 children 轻量索引，不递归扫描 child 内容。ZIP 中嵌套 ZIP、目录中嵌套文件等关系由 Manager 或其他消费者在用户选中 child 后按需解析：如果 child 仍是 `data_type=container`，继续进入同一套 container provider / resolver 链路。

`ContainerChildResolver` 不连接 engine，也不自己构造 engine reader。调用方必须先基于 engine capability、权限、连接信息和资源路径构造 `contentio.Reader` 与父 `Ref`。resolver 只把父资源和 child locator 转换为两类结果之一：

- stream child：返回 child 自己的 `Reader` / `Ref`，例如 ZIP entry、目录文件；后续按 child 的 `data_type` / `format` 调对应 reader。
- native child：复用父资源和 child 定位 options，例如 SQLite table、Excel sheet、GeoPackage layer；后续按父格式的 table reader 读取指定 child。

### CatalogFactsProvider / GraphSampleProvider

`CatalogFactsProvider` 和 `GraphSampleProvider` 面向引擎原生图的结构描述和轻量样本。图结构事实通过 `CatalogFacts.Graph` 进入 `datatype.GraphInfo`，图样本只用于预览或浏览，不作为 Meta 主事实源。

这两个 provider 必须返回业务图视图，而不是引擎内部实现图。插件、扩展、索引或空间能力产生的内部节点和内部关系应在 provider 或 Graph 模块服务层过滤；`datatype.GraphInfo` 只描述通用 graph facts，不承载 Neo4j Spatial、R-tree 等具体实现规则。

它提供：

- 节点形状、关系形状、连接模式和图统计信息。
- 轻量节点样本。
- 轻量关系样本。

它不直接包装某个前端图组件 DTO，也不替代 `GraphQueryProvider` 的图查询结果能力。

`GraphSampleProvider` 的过滤参数应使用 `plugin.GraphSampleFilter`，按 node shape 或 relationship shape 采样。采样过滤中的 label set 是执行条件，不是持久化 schema 事实；Meta 主事实仍以 `datatype.GraphInfo.NodeShapes`、`RelationshipShapes` 和 `Patterns` 为准。

## Provider 输入边界

provider 输入应该尽量轻，不要堆成过重的 `EngineID + ItemID + Locator + Attributes + Options`。

合理输入只包含三类信息：

- 定位信息：已经由 Meta 或调用方确认的资源定位或引擎原生对象定位。
- 已确认属性片段：调用该 provider 所需的 `item`、`type_info`、`format_info`、`capabilities` 子集。
- 调用参数：分页、字段选择、采样大小、目标格式选项等。

provider 不应重新判断 layout，不应重新枚举 sibling，不应重新推断 format，也不应重新绑定完整 engine 模型。

## 读取入口边界

FormatPlugin、info provider、content reader 不应通过 `engine id` 自己构造读取器。

推荐方式是：

1. Meta、Manager 或 Transfer 根据 engine capability 构造读取抽象。
2. FormatPlugin、info provider 或 content reader 接收读取抽象和格式参数。
3. FormatPlugin 输出结构事实、样本或 DataBatch。
4. info provider 或 content reader 归一为平台语义或内容数据。

这样可以把连接、凭据、权限、重试、审计和对象枚举留在 engine 层，把编码 / 解码留在 format 层。

## Manager 结果组装边界

Manager 面向前端的 DTO 属于 Manager 边界，不宜放在 format 层作为通用返回值。

合理分工是：

- FormatPlugin 提供格式原生可解析的结构事实或记录样本。
- info provider 和 content reader 把不同来源整理成 table / document / media / container 的平台语义或内容数据。
- Manager service 再组装成当前前端需要的 DTO。

如果 Manager DTO 需要新增字段，应向 info provider、content reader 或底层 format / engine provider 提需求，不能把 Manager DTO 反向下沉为 format 层通用模型。

## Transfer 边界

Transfer 不能只按 `connector type` 路由，也不能只看 format。它需要同时看：

| 维度 | 作用 |
|---|---|
| engine capability | 数据在哪里，如何连接，如何列举，如何读写原生资源 |
| format descriptor / provider | 数据如何编码 / 解码，如何组织 refs，当前进程是否具备对应 reader / writer |
| info provider / content reader | 以什么平台语义组织 schema、样本、children |

典型读取链路：

1. engine 打开资源或列举对象，形成读取抽象。
2. format 基于读取抽象解码为平台批次或中间结构。
3. info provider / content reader 补充 schema、样本、空间信息或容器结构。

典型写入链路：

1. info provider / content reader 提供待写入的结构语义或内容数据。
2. format 把批次编码成目标格式。
3. engine 负责对象写入、目录提交或原生表写入。

空间表写入原生 engine 时，Transfer planner 只根据 endpoint 表示方式、源 format descriptor 和 provider 实现状态选择行值协议，不根据具体 engine 名称硬编码分支。encoded spatial source 写入 native table target 时，planner 可以请求 `ewkb` 作为批量传输默认编码；目标 engine writer 必须基于标准字段类型、`SpatialInfo` 派生 attributes 和实际行值类型进行消费，支持则写入，不支持则返回明确错误。不得在 planner 中用某个 engine type 为某种编码开白名单。

空间表写入 encoded format 时，planner / executor 只传递标准 schema、标准行值和 format 写出选项。具体 format writer 负责把 `SpatialInfo` 映射为自己的 native 几何组织方式，不能让上层为了某个格式写出而硬编码 native shape type。

批量读写需要额外确认：

- 是否支持列表、分区、多 ref。
- 是否支持 seek / checkpoint。
- 是否支持原子提交。
- 是否支持并行写。
- schema 和空间字段由谁提供。
- format write 与 engine write 的提交边界在哪里。

## 设计约束

1. 不在 `common/format` 放 item resolver。
2. FormatPlugin 不决定最终 `layout`、claims、exclusive 和 `meta_item.full_name`。
3. provider 输入保持轻量，只接已确认定位、必要属性片段和调用参数。
4. FormatPlugin、info provider、content reader 不按 `engine_id` 反向构造 engine reader。
5. Manager 面向前端的 DTO 不进入 format 层。
6. GeoJSON 类结构表达为 `format=json` + `capabilities.spatial`，不作为独立顶层格式。

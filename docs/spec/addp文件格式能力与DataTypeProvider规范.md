# ADDP 文件格式能力与 Data Type Provider 规范

本文定义 `common/format` 中的 format capability、format provider 与 data type provider 边界。它只约束格式和数据类型能力，不定义 Meta 的 item 归并实现。

概念边界见 [ADDP 数据类型与格式体系图](../concepts/addp数据类型与格式体系图.md)，资源读取边界见 [ADDP 资源读取抽象规范](addp资源读取抽象规范.md)。

## 本文边界

| 本文负责 | 不在本文定义 |
|---|---|
| format capability 表达格式实现能做什么 | data item 如何归并和 claims 如何合并 |
| FormatPlugin 如何声明格式身份、能力和布局 | `meta_item.name/full_name/item_type` 来源规则 |
| info provider 如何提供 data type info 和 format info | `meta_item.attributes` 的完整 schema |
| content reader 如何提供内容数据 | ResourceReader / ComponentReader 的具体接口 |

item 归并见 [ADDP 数据项 detector 规范](addp数据项detector规范.md)，attributes 写入见 [ADDP 元数据 attributes 规范](addp元数据attributes规范.md)。

## 核心原则

`format capability` 与 `engine capability` 术语对齐，都是插件或实现对平台声明“我能做什么”。它不同于 `meta_item.attributes.capabilities`；后者是扫描后的 item 事实结果。

`common/format` 只回答格式和数据类型能力问题：

- 这个资源像什么格式。
- 这个格式如何组织资源。
- 这个格式能提取什么结构事实。
- 这个格式能否通过 content reader 提供样本、文本、原始内容、批量读写。
- 解码结果如何归一为平台 data type 语义。

其中 `xxx info` 是对应 data type 的元数据，例如 `TableInfo`、`DocumentInfo`、`MediaInfo`、`ContainerInfo`。`xxx info provider` 只负责提取这类元数据；样本、文本片段、缩略图、raw content、range content 等内容数据必须通过独立 content reader 表达。

`common/format` 不定义任何展示概念。展示策略、前端渲染器选择和 Manager 返回给前端的 DTO 均属于 Manager / Frontend 边界。

`common/format` 不回答 item 归并问题：

- 不决定最终 `organization`。
- 不决定 claims、exclusive、component_files。
- 不决定 `meta_item.full_name`。
- 不直接写最终 `meta_item.attributes`。

Meta 负责把 format capability、扫描上下文和已认领资源编排成最终 data item。

## FormatCapabilities

`FormatCapabilities` 是格式能力总览，不是 parser 注册表，也不是 `meta_item.attributes.capabilities`。

| 能力段 | 说明 | 典型产出 | 主要消费者 |
|---|---|---|---|
| Identification | 如何识别格式 | 扩展名、MIME、magic bytes、内容签名 | Meta、Registry |
| Layout | 格式自身如何组织资源 | single / multi / whole、主资源、组件规则、manifest 规则 | Meta |
| Info / Facts | 能提取什么 data type info 和横切事实 | table / document / media / container info、spatial facts | Meta、Manager、Transfer |
| Content Reader | 能否提供按 data type 组织后的内容数据 | 行样本、文本片段、缩略图、容器树、raw content、range content | Manager、Transfer |
| Transfer | 能否参与批量读写和组件提交 | batch read / write、component read / write、commit policy | Transfer |
| Provider hints | 实现了哪些 provider 家族 | table / document / media / container / graph / spatial | Registry、上层调用方 |

当前代码中 `FormatCapability` 是能力声明；`FormatPlugin` 是格式包的代码入口；`FormatInfoProvider`、`TableInfoProvider`、`TableSampleReader`、兼容期 `TableProvider`、`DocumentInfoProvider`、`DocumentTextReader`、`MediaInfoProvider`、type mapper 等是具体实现能力。能力声明可以先于 plugin / provider 完整实现存在，因此文档必须区分“声明支持”和“已有实现”。

### Format Identity 与 Format Detection

`format identity` 定义“平台支持的这个格式是谁”，通常由 `FormatDescriptor` 表达；当该格式已有代码实现时，由 `FormatPlugin` 作为格式包主入口承载。它包含稳定格式 ID、默认 data type、布局、info provider、content reader、transfer 和横切能力声明。

`format detection` 是“给定一个资源，判断它像哪个格式”的动态过程。它输入文件名、MIME、magic bytes、内容签名或组件上下文，输出指向某个 format identity 的识别结果。

二者边界如下：

| 维度 | Format Identity | Format Detection |
|---|---|---|
| 回答的问题 | 平台支持哪些格式以及这些格式能做什么 | 当前资源看起来是什么格式 |
| 性质 | 静态注册事实 | 动态识别过程 |
| 输入 | plugin / descriptor 注册信息 | 文件名、MIME、magic bytes、内容片段、组件上下文 |
| 输出 | format descriptor / capability | detection result，指向某个 format |
| 是否决定 item | 不决定 | 不最终决定，只给 Meta detector 提供格式候选 |

Shapefile 这类 multi 格式尤其要区分：单个 `.shp/.dbf/.shx` 的识别不等于 data item 归并；最终 item 边界由 Meta item detector 根据 format layout 和资源上下文决定。

### Identification

识别能力只负责“看起来像什么格式”，不负责决定 item。

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

### Layout

布局能力描述格式自身如何组织资源。

建议说明：

- `ScopeKind`：单文件、组件文件、目录、prefix、schema、manifest scope。
- `PrimaryResourceRole`：主资源是普通文件、manifest、目录入口，还是整个范围本身。
- `RequiredComponents`：必须出现哪些组件。
- `OptionalComponents`：可以出现哪些附加组件。
- `SameNameRule`：是否要求同 basename、同 prefix、同后缀集。
- `CrossDirectoryAllowed`：是否允许跨目录匹配组件。
- `WholeScopeExclusive`：是否天然需要整段范围一起认领。

示例：

- CSV：`single`，一个资源即可。
- Shapefile：`multi`，主文件加同 basename 组件。
- Iceberg：`whole`，整体范围认领。

这一组能力只服务 Meta 调度，不直接等于 item 结果。

### Info / Facts

Info / facts 能力负责把原始资源转成平台能理解的类型信息和横切事实。

常见产出：

- `type_info.table`
- `type_info.document`
- `type_info.media`
- `type_info.container`
- `format_info.<format>`
- `capabilities.spatial`
- `capabilities.extraction`
- `capabilities.statistics`

表格解析能力应说明字段名、原始字段类型、行数、主键和索引等 info 是否可得。文档提取能力应说明标题、作者、页数、语言和提取状态等 info 是否可得。媒体提取能力应说明宽高、时长、编码、颜色模式和 EXIF 等 info 是否可得。容器能力应说明内部对象、默认入口和对象摘要是否可得。空间能力应说明 geometry columns、primary geometry column、SRID / CRS、extent 和 spatial index 是否可得。

注意：`type_info.*` 只保存对应 data type 的元数据。内容样本、原始内容、展示材料不是 info，不能为了上层展示方便塞进 `table info`、`document info` 或 `media info`。

### Content Reader

content reader 负责提供上层可继续组装的数据，不直接负责展示协议，也不等于最终 attributes。

常见结果包括：

- table rows sample
- document text fragment
- media thumbnail
- container children sample
- graph sample
- raw content / range content

Manager 展示 DTO 由 Manager 组装，不应进入 `common/format`。`common/format` 只声明和实现 reader 能力，例如 `table_sample`、`document_text`、`raw_content`、`range_content`。是否使用这些 reader 做展示、下载、索引或传输，由上层模块决定。

合理链路是：

```text
Manager / Transfer / Meta 构造 ResourceReader 或 io.Reader
  -> data type info provider 或 content reader 消费输入
  -> 返回 type_info / format_info / 内容数据
  -> Manager / Transfer / Meta 各自按模块边界继续组装
```

不合理链路是：

```text
format provider 根据 engine id 自己读取文件
format provider 返回 Manager 专用 DTO 或前端渲染器建议
common/format 使用展示材料或前端渲染器参与展示决策
```

### Transfer

Transfer 能力负责批量读写、组件读写和提交边界。

建议说明：

- `BatchRead`
- `BatchWrite`
- `ComponentRead`
- `ComponentWrite`
- `CommitPolicy`

Format writer 负责编码格式，Engine writer 负责提交到目标存储。多文件格式必须明确提交边界，不能只写主文件。更细的读取抽象和组件定位规则见 [ADDP 资源读取抽象规范](addp资源读取抽象规范.md)。

## 当前能力矩阵

本节记录当前 ADDP 已有的格式能力和 provider 实现状态。它是实现现状说明，不替代内置格式落地规则。

### FormatCapability 声明

当前 `common/format/capability` 已声明：

| 格式 | 默认 `data_type` | layouts | provider hints | content readers | parse | transfer read/write | 说明 |
|---|---|---|---|---|---|---|---|
| `table` | `table` | `whole` | table | table_sample | 否 | 是 / 是 | 引擎原生表格逻辑格式 |
| `document` | `document` | `whole` | document | document_text、raw_content | 否 | 是 / 是 | 引擎原生文档逻辑格式 |
| `csv` | `table` | `single` | table | table_sample、raw_content | 是 | 是 / 是 | 单文件表 |
| `docx` | `document` | `single` | document | raw_content、range_content | 否 | 否 / 否 | Word 文档；当前后端只声明原始内容读取能力 |
| `excel` | `container` | `single` | container、table | table_sample、raw_content | 否 | 否 / 否 | Excel 工作簿；当前按容器 / 表格内容读取能力表达 |
| `image` / `jpeg` / `png` / `gif` / `tiff` | `media` | `single` | media | raw_content、range_content | 否 | 否 / 否 | 图片格式；已有最小 MediaInfoProvider，`image` 是逻辑聚合格式，具体文件格式由子格式识别 |
| `json` | `document` | `single` | document、table、spatial | table_sample、raw_content | 是 | 是 / 是 | JSON 可按内容识别为文档、表或空间表 |
| `markdown` | `document` | `single` | document | document_text、raw_content | 否 | 是 / 是 | Markdown 文本文档；已有最小 DocumentInfoProvider / DocumentTextReader，可提取 UTF-8 文本片段 |
| `parquet` | `table` | `single`、`whole` | table | table_sample、scope_table_sample、raw_content | 是 | 是 / 是 | 单文件表和 scope 表 |
| `pdf` | `document` | `single` | document | raw_content、range_content | 否 | 否 / 否 | PDF 文档；旧元数据提取待迁移到 DocumentInfoProvider |
| `pptx` | `document` | `single` | document | raw_content、range_content | 否 | 否 / 否 | 演示文档；当前后端只声明原始内容读取能力 |
| `shapefile` | `table` | `multi` | table、spatial | table_sample、component_table_sample、raw_content | 是 | 是 / 是 | 多组件空间表 |
| `sqlite` | `container` | `single` | container、table | table_sample、raw_content | 否 | 否 / 否 | SQLite 容器；当前按容器 / 表格内容读取能力表达 |
| `text` | `document` | `single` | document | document_text、raw_content | 否 | 是 / 是 | 纯文本文档；已有最小 DocumentInfoProvider / DocumentTextReader，可提取 UTF-8 文本片段 |
| `wps` | `document` | `single` | document | raw_content、range_content | 否 | 否 / 否 | WPS 文档；后端不解析正文，由上层按内容读取能力处理 |

注意：此矩阵只表示 capability registry 当前声明，不表示所有格式都已有完整 parser、writer、content reader 和 transfer 端到端实现。

### 能力发现视图

`common/format.ListFormatCapabilityViews()` 是上层模块查询格式能力的稳定入口。视图中需要明确区分两层含义：

| 字段 | 含义 |
|---|---|
| `providers` | descriptor / capability 的声明能力，表示该格式在规范上属于哪些 info provider 家族 |
| `content_readers` | descriptor / capability 的内容读取能力声明，表示该格式当前可提供哪些内容数据读取方式 |
| `implementations` | 当前进程内已经注册的实际实现状态，表示 `FormatInfoProvider`、`TableInfoProvider`、`TableSampleReader`、兼容期 `TableProvider`、`DocumentInfoProvider`、`DocumentTextReader`、`MediaInfoProvider` 等是否已经可调用；其中 `metadata_extractor` 是 legacy 兼容状态字段 |

上层模块做格式路由时，应优先依据 `format`、`data_type`、`providers`、`content_readers` 判断语义，再根据 `implementations` 决定能否直接调用后端实现。不能把 `providers.document_info=true` 理解为一定已经有 `DocumentTextReader`；例如 DOCX / WPS 当前声明为文档格式和 raw/range content reader，但后端正文解析实现尚未稳定。

新增或调整内置格式后，应检查能力发现视图：

1. `providers` 是否表达规范上的目标能力。
2. `implementations` 是否只反映当前已注册的 Go 实现。
3. `content_readers` 是否只描述内容读取方式，不包含前端渲染器或展示策略。
4. 对未知二进制对象，能力发现视图不应新增普通 `binary` format；兜底展示只属于 Manager / Frontend 协议。

### Provider 实现

当前内置注册状态：

| 格式 | FormatInfoProvider | TableInfoProvider | TableSampleReader | 兼容期 TableProvider | DocumentInfoProvider | DocumentTextReader | ComponentTableProvider | ScopeTableProvider | MediaInfoProvider | Legacy FileMetadataExtractor | TypeMapper | 说明 |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| `csv` | 已实现 | 已实现 | 已实现 | 已实现 | 无 | 无 | 无 | 无 | 无 | 无 | 无 | 当前注册为 `format=csv`；TSV 识别规则存在，但独立 `format=tsv` reader 待补 |
| `excel` | 未注册 | 已实现 | 已实现 | 已实现 | 无 | 无 | 无 | 无 | 无 | 无 | 无 | 可提取工作簿中的表格 info 和样本；Meta 规范上外层仍按 container item 表达 |
| `json` | 未注册 | 已实现 | 已实现 | 已实现 | 无 | 无 | 无 | 无 | 无 | 无 | 无 | records / JSON Lines / 空间 JSON 由 parser 判断结构 |
| `markdown` | 无 | 无 | 无 | 无 | 已实现最小能力 | 已实现最小能力 | 无 | 无 | 无 | 无 | 无 | 支持 UTF-8 文本片段提取 |
| `parquet` | 未注册 | 已实现 | 已实现 | 已实现 | 无 | 无 | 无 | 已实现 | 无 | 无 | 无 | 支持单文件和 scope 表读取 |
| `shapefile` | 未注册 | 已实现 | 已实现 | 已实现 | 无 | 无 | 已实现 | 无 | 无 | 无 | 已实现 | 支持组件读取和空间字段映射 |
| `text` | 无 | 无 | 无 | 无 | 已实现最小能力 | 已实现最小能力 | 无 | 无 | 无 | 无 | 无 | 支持 UTF-8 文本片段提取 |
| `sqlite` | 未注册 | 未注册 | 未注册 | 未注册 | 无 | 无 | 无 | 无 | 无 | 无 | SpatiaLite mapper 已注册 | capability 声明 container/table 目标能力，当前作为容器分析能力使用，暂不注册为 TableProvider |
| `geopackage` | 未注册 | 未注册 | 未注册 | 未注册 | 无 | 无 | 无 | 无 | 无 | 无 | 无 | 当前按容器 / 空间元数据链路表达，provider 待补 |
| `image` / `jpeg` / `png` / `gif` / `tiff` | 未注册 | 无 | 无 | 无 | 无 | 无 | 无 | 无 | 已实现 | 已实现 | 无 | MediaInfoProvider 可返回宽高、编码、MIME，GeoTIFF 可补 spatial facts；旧 extractor 待收敛 |
| `pdf` | 未注册 | 无 | 无 | 无 | 无 | 无 | 无 | 无 | 无 | 已实现 | 无 | 旧 FileMetadataExtractor 有 PDF 元数据提取，待收敛为 DocumentInfoProvider |

当前 provider / reader 家族已包含 `TableInfoProvider`、`TableSampleReader`、兼容期 `TableProvider`、最小 `DocumentInfoProvider` / `DocumentTextReader` 和最小 `MediaInfoProvider`；`ContainerProvider`、`GraphProvider` 仍是目标抽象，具体稳定接口和注册表后续随消费场景补齐。

### FileMetadataExtractor 兼容说明

`FileMetadataExtractor` 是早期按 MIME 类型提取增强元数据的旁路机制，当前仍被 Meta 的对象存储扫描和按需提取链路使用。它的问题是返回 `ExtractedMetadata.BasicInfo / SchemaInfo / ContentData / CustomAttrs`，同时混合了 storage info、type info、format info、capabilities 和内容数据，和 `xxx info provider` / content reader 的主线存在概念冲突。

新的格式能力不应继续新增 `FileMetadataExtractor` 实现。已有 image / PDF 相关逻辑应逐步迁移：

1. 可确定的 data type 元数据进入对应 `MediaInfoProvider` / `DocumentInfoProvider`。
2. 文本片段、缩略图、raw content、range content 等进入 content reader。
3. Meta 只负责编排 provider 结果并写入标准 attributes 分区。
4. 旧 `FileMetadataExtractor` 仅作为兼容入口保留到 Meta 调用方完成迁移。

## Data Type Provider

`data type provider` 是上层消费者的主入口，目标是让上层不直接感知具体 `engine type` 或 `format type`。

建议围绕两类 provider 收口：

| 类别 | 职责 | 示例 |
|---|---|---|
| info provider | 提供对应 data type 的元数据，供 Meta 写入 `type_info.*` | `TableInfoProvider`、`DocumentInfoProvider`、`MediaInfoProvider`、`ContainerInfoProvider` |
| content reader | 提供按 data type 组织后的内容数据，供 Manager / Transfer 消费 | `TableSampleReader`、`DocumentTextReader`、`RawContentReader`、`RangeContentReader`、`MediaThumbnailReader` |

Provider 只回答对应 data type 的平台语义，不回答格式识别，也不回答 item 归并。

### Table Info / Sample Provider

表格型 provider 是最先需要稳定的 data type provider，因为它同时覆盖 Meta 表结构提取、Manager 内容查看和 Transfer 批量读写。

第一版拆成两条主能力：

- `TableInfoProvider`：返回表结构与字段元数据，是 `type_info.table` 的主来源。
- `TableSampleReader`：返回分页或采样样本，是 Manager 表格探查和 Transfer 探查的主来源。

二者可以由同一个格式实现同时提供，也可以分别提供。当前代码中的 `TableProvider` 是兼容期组合接口，等价于同时具备 `TableInfoProvider` 和 `TableSampleReader`。

后续完整表格能力至少要覆盖：

- 表结构和字段元数据。
- 分页或采样读取。
- 行样本。
- 空间列、SRID、extent 等空间信息。
- 批量读取和批量写入边界。

能力可按四类组织：

- `Describe`：返回表结构与字段元数据。
- `Sample`：返回分页或采样样本。
- `ReadBatch`：返回批量读取所需的结构或数据。
- `WriteBatch`：接受批量写入所需的结构或数据。

这些名称是能力分组，不要求直接成为最终 Go 方法名。只读来源可以只实现 `Describe` / `Sample`；只用于内容查看的实现不必实现 `WriteBatch`。

`SpatialProvider` 不是另一个表类型，而是横切 provider。表格内容查看没有空间细节时仍应可用，只是空间信息更少。

### DocumentInfoProvider / DocumentTextReader

`DocumentInfoProvider` 和 `DocumentTextReader` 面向 PDF、Word、Markdown、纯文本、富文本集合、文档型数据库记录等文档型 data item。

`DocumentInfoProvider` 提供：

- 文档元信息。
- 页码或范围上下文。
- 提取状态。

`DocumentTextReader` 提供正文片段。raw content / range content 由对应 content reader 表达。二者都不负责 Manager 的最终展示 DTO。

文档格式不要求都必须在后端完成解析。对于 WPS、DOCX、PPTX 这类后端不适合解析的格式，可以先只声明 raw content / range content reader；后续如需全文索引、摘要、脱敏或服务端导出，再补后端文本提取能力。

### MediaInfoProvider

`MediaInfoProvider` 面向图片、音频、视频等媒体型 data item。

它提供：

- 媒体元信息。
- 可选的编码 / 解码辅助信息。

缩略图、raw content、range content 或可流式 URL 应由对应 content reader / engine 能力表达。`MediaInfoProvider` 只返回已经确认的事实，不硬凑完整展示对象。

### ContainerProvider

`ContainerProvider` 面向目录、压缩包、Excel 工作簿、SQLite / GeoPackage、文档集合等容器型 data item。

它提供：

- 子对象列表。
- 默认入口。
- 内部对象定位。
- 容器统计信息。

它不负责把内部对象解释成最终 table / document / media 展示数据；那部分交给对应 data type provider 继续处理。

### GraphProvider

`GraphProvider` 面向图数据库查询结果、图结构抽样、节点-关系模型等图型 data item。

它提供：

- 节点样本。
- 关系样本。
- 图统计信息。
- 可选的图查询结果归一结构。

它不直接包装某个前端图组件 DTO。

## Provider 输入边界

provider 输入应该尽量轻，不要堆成过重的 `EngineID + ItemID + Locator + Attributes + Options`。

合理输入只包含三类信息：

- 定位信息：已经由 Meta 或调用方确认的资源定位或引擎原生对象定位。
- 已确认属性片段：调用该 provider 所需的 `item`、`type_info`、`format_info`、`capabilities` 子集。
- 调用参数：分页、字段选择、采样大小、目标格式选项等。

provider 不应重新判断 organization，不应重新枚举 sibling，不应重新推断 format，也不应重新绑定完整 engine 模型。

## 读取入口边界

format provider 不应通过 `engine id` 自己构造读取器。

推荐方式是：

1. Meta、Manager 或 Transfer 根据 engine capability 构造读取抽象。
2. format provider 接收读取抽象和格式参数。
3. format provider 输出结构事实、样本或 DataBatch。
4. data type provider 归一为平台语义。

这样可以把连接、凭据、权限、重试、审计和对象枚举留在 engine 层，把编码 / 解码留在 format 层。

## Manager 展示边界

Manager 面向前端的展示 DTO 属于 Manager 边界，不宜放在 format 层作为通用返回值。

合理分工是：

- format provider 提供格式原生可解析的结构事实或记录样本。
- data type provider 把不同来源整理成 table / document / media / container 的平台语义。
- Manager service 再组装成当前前端需要的展示 DTO。

如果 Manager 展示 DTO 需要新增字段，应向 data type provider、content reader 或底层 format / engine provider 提需求，不能把 Manager DTO 反向下沉为 format 层通用模型。

## Transfer 边界

Transfer 不能只按 `connector type` 路由，也不能只看 format。它需要同时看：

| 维度 | 作用 |
|---|---|
| engine capability | 数据在哪里，如何连接，如何列举，如何读写原生资源 |
| format capability | 数据如何编码 / 解码，如何组织组件，如何提交 |
| data type provider | 以什么平台语义组织 schema、样本、batch、children |

典型读取链路：

1. engine 打开资源或列举对象，形成读取抽象。
2. format 基于读取抽象解码为平台批次或中间结构。
3. data type provider 补充 schema、样本、空间信息或容器结构。

典型写入链路：

1. data type provider 提供待写入的结构语义。
2. format 把批次编码成目标格式。
3. engine 负责对象写入、目录提交或原生表写入。

批量读写需要额外确认：

- 是否支持列表、分区、多组件。
- 是否支持 seek / checkpoint。
- 是否支持原子提交。
- 是否支持并行写。
- schema 和空间字段由谁提供。
- format write 与 engine write 的提交边界在哪里。

## 设计约束

1. 不在 `common/format` 放 item resolver。
2. format provider 不决定最终 `organization`、claims、exclusive 和 `meta_item.full_name`。
3. provider 输入保持轻量，只接已确认定位、必要属性片段和调用参数。
4. format provider 不按 `engine_id` 反向构造 engine reader。
5. Manager 展示 DTO 不进入 format 层。
6. GeoJSON 类结构表达为 `format=json` + `capabilities.spatial`，不作为独立顶层格式。

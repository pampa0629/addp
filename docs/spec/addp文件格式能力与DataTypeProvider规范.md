# ADDP 文件格式能力与 Data Type Provider 规范

本文定义 `common/format` 中的 format capability、format provider 与 data type provider 边界。它只约束格式和数据类型能力，不定义 Meta 的 item 归并实现。

概念边界见 [ADDP 数据类型与格式体系图](../concepts/addp数据类型与格式体系图.md)，资源读取边界见 [ADDP 资源读取抽象规范](addp资源读取抽象规范.md)。

## 本文边界

| 本文负责 | 不在本文定义 |
|---|---|
| format capability 表达格式实现能做什么 | data item 如何归并和 claims 如何合并 |
| format provider 如何识别、解析、提取、采样、读写 | `meta_item.name/full_name/item_type` 来源规则 |
| data type provider 如何把结果归一为 table / document / media / container / graph 语义 | `meta_item.attributes` 的完整 schema |
| Manager preview 与 Transfer 对 provider 的消费边界 | ResourceReader / ComponentReader 的具体接口 |

item 归并见 [ADDP 数据项 detector 规范](addp数据项detector规范.md)，attributes 写入见 [ADDP 元数据 attributes 规范](addp元数据attributes规范.md)。

## 核心原则

`format capability` 与 `engine capability` 术语对齐，都是插件或实现对平台声明“我能做什么”。它不同于 `meta_item.attributes.capabilities`；后者是扫描后的 item 事实结果。

`common/format` 只回答格式和数据类型能力问题：

- 这个资源像什么格式。
- 这个格式如何组织资源。
- 这个格式能提取什么结构事实。
- 这个格式能否提供样本、提取结果、写出、转换、批量读写。
- 解码结果如何归一为平台 data type 语义。

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
| Parse / Extract | 能提取什么结构事实 | table / document / media / container / spatial facts | Meta、Manager、Transfer |
| Sample / Extract | 能否提供样本或中间提取结果 | 行样本、文本片段、缩略图素材、容器树 | Manager、Meta、Transfer |
| Preview material | 能否提供上层可包装的预览材料 | HTML、Markdown、纯文本、缩略图、raw bytes 引用 | Manager |
| Transfer | 能否参与批量读写和组件提交 | batch read / write、component read / write、commit policy | Transfer |
| Provider hints | 实现了哪些 provider 家族 | table / document / media / container / graph / spatial | Registry、上层调用方 |

当前代码中 `FormatCapability` 是能力声明；`TableProvider`、metadata extractor、type mapper 等是具体实现注册。能力声明可以先于 provider 完整实现存在，因此文档必须区分“声明支持”和“已有 provider 实现”。

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

### Parse / Extract

解析和提取能力负责把原始资源转成平台能理解的结构事实。

常见产出：

- `type_info.table`
- `type_info.document`
- `type_info.media`
- `type_info.container`
- `format_info.<format>`
- `capabilities.spatial`
- `capabilities.extraction`
- `capabilities.statistics`

表格解析能力应说明字段名、原始字段类型、行数、主键、索引和采样是否可得。文档提取能力应说明标题、作者、页数、语言、文本片段和摘要是否可得。媒体提取能力应说明宽高、时长、编码、颜色模式、缩略图和 EXIF 是否可得。容器能力应说明内部对象、默认入口和对象摘要是否可得。空间能力应说明 geometry columns、primary geometry column、SRID / CRS、extent 和 spatial index 是否可得。

### Sample / Extract

样本 / 提取能力负责提供上层可继续组装的数据，不直接负责 Manager preview，也不等于最终 attributes。

常见结果包括：

- table rows sample
- document text fragment
- media thumbnail material
- container children sample
- graph sample

Manager preview DTO 由 Manager 组装，不应进入 `common/format`。

### Preview material

预览材料能力负责回答“上层要预览这个 item 时，格式层可以提供什么材料”。它不是 Manager preview DTO，也不要求格式层自己连接 engine 或读取资源。

预览材料至少应区分以下形态：

| 形态 | 含义 | 示例 |
|---|---|---|
| `text` | 已可直接展示的纯文本 | TXT、日志片段 |
| `markdown` | Markdown 文本，由前端渲染 | Markdown 文件 |
| `html` | 已转换的 HTML 片段 | 后端转换后的文档片段 |
| `json` | 可格式化展示的 JSON 值 | 配置 JSON、文档 JSON |
| `image` | 缩略图或图片数据 | 图片、PDF 页缩略图 |
| `raw_binary` | 原始二进制内容或可访问引用，由前端专用 renderer 处理 | WPS、DOCX、PPTX、PDF |
| `url` | 受控访问 URL | 大文件、视频、可流式媒体 |

预览材料能力必须和格式身份分开。比如 WPS 仍是 `format=wps`、`data_type=document`，即使当前 preview material 是 `raw_binary`；不能因为预览材料是二进制就把 WPS 表达为 `format=binary`。

合理链路是：

```text
Manager / Transfer / Meta 构造 ResourceReader 或 io.Reader
  -> data type provider 或 preview material provider 消费输入
  -> 返回格式无关的预览材料描述
  -> Manager 组装 preview DTO 并执行大小限制、base64 或 URL 策略
  -> 前端 renderer 展示
```

不合理链路是：

```text
format provider 根据 engine id 自己读取文件
format provider 返回 Manager 专用 DTO
Manager 根据扩展名猜哪个前端组件能打开
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

| 格式 | 默认 `data_type` | layouts | provider hints | parse | preview | transfer read/write | 说明 |
|---|---|---|---|---|---|---|---|
| `table` | `table` | `whole` | table | 否 | 是 | 是 / 是 | 引擎原生表格逻辑格式 |
| `document` | `document` | `whole` | document | 否 | 是 | 是 / 是 | 引擎原生文档逻辑格式 |
| `csv` | `table` | `single` | table | 是 | 是 | 是 / 是 | 单文件表 |
| `json` | `document` | `single` | document、table、spatial | 是 | 是 | 是 / 是 | JSON 可按内容识别为文档、表或空间表 |
| `markdown` | `document` | `single` | document | 否 | 是 | 是 / 是 | Markdown 文本文档；当前声明预览材料能力，不声明稳定后端解析 |
| `parquet` | `table` | `single`、`whole` | table | 是 | 是 | 是 / 是 | 单文件表和 scope 表 |
| `shapefile` | `table` | `multi` | table、spatial | 是 | 是 | 是 / 是 | 多组件空间表 |

注意：此矩阵只表示 capability registry 当前声明，不表示所有格式都已有完整 parser、writer、preview 和 transfer 端到端实现。

### Provider / Extractor 实现

当前内置注册状态：

| 格式 | TableProvider | ComponentTableProvider | ScopeTableProvider | Metadata / File Extractor | TypeMapper | 说明 |
|---|---|---|---|---|---|---|
| `csv` | 已实现 | 无 | 无 | 无 | 无 | 当前注册为 `format=csv`；TSV 识别规则存在，但独立 `format=tsv` provider 待补 |
| `excel` | 已实现 | 无 | 无 | 无 | 无 | 当前可作为表格 provider 读取；Meta 规范上外层仍按 container item 表达 |
| `json` | 已实现 | 无 | 无 | 无 | 无 | records / JSON Lines / 空间 JSON 由 parser 判断结构 |
| `markdown` | 无 | 无 | 无 | 无 | 无 | 已有格式识别和 capability；预览走文本 / Markdown preview material，后续补 DocumentProvider |
| `parquet` | 已实现 | 无 | 已实现 | 无 | 无 | 支持单文件和 scope 表读取 |
| `shapefile` | 已实现 | 已实现 | 无 | 无 | 已实现 | 支持组件读取和空间字段映射 |
| `sqlite` | 未注册 | 无 | 无 | 无 | SpatiaLite mapper 已注册 | 当前作为容器分析能力使用，暂不注册为 TableProvider |
| `geopackage` | 未注册 | 无 | 无 | 无 | 无 | 当前按容器 / 空间元数据链路表达，provider 待补 |
| `image` / `jpeg` / `png` / `gif` / `tiff` | 无 | 无 | 无 | 已实现 | 无 | 图片元数据提取；GeoTIFF 可补 spatial facts |
| `pdf` | 无 | 无 | 无 | 已实现 | 无 | PDF 元数据和文本提取状态 |

当前 provider 家族以 `TableProvider` 为主；`DocumentProvider`、`MediaProvider`、`ContainerProvider`、`GraphProvider` 仍是目标抽象，具体稳定接口和注册表后续随消费场景补齐。

## Data Type Provider

`data type provider` 是上层消费者的主入口，目标是让上层不直接感知具体 `engine type` 或 `format type`。

建议围绕这些家族收口：

- `TableProvider`
- `DocumentProvider`
- `MediaProvider`
- `ContainerProvider`
- `GraphProvider`
- `SpatialProvider` 作为横切 provider

Provider 只回答对应 data type 的平台语义，不回答格式识别，也不回答 item 归并。

### TableProvider

`TableProvider` 是最先需要稳定的 data type provider，因为它同时覆盖 Manager 表预览、Transfer 批量读写和 Meta 表结构提取。

它至少要覆盖：

- 表结构和字段元数据。
- 分页或采样读取。
- 行样本。
- 空间列、SRID、extent 等空间信息。
- 批量读取和批量写入边界。

第一版能力可按四类组织：

- `Describe`：返回表结构与字段元数据。
- `Sample`：返回分页或采样样本。
- `ReadBatch`：返回批量读取所需的结构或数据。
- `WriteBatch`：接受批量写入所需的结构或数据。

这些名称是能力分组，不要求直接成为最终 Go 方法名。只读来源可以只实现 `Describe` / `Sample`；只用于预览的实现不必实现 `WriteBatch`。

`SpatialProvider` 不是另一个表类型，而是横切 provider。表预览没有空间细节时仍应可用，只是空间信息更少。

### DocumentProvider

`DocumentProvider` 面向 PDF、Word、Markdown、纯文本、富文本集合、文档型数据库记录等文档型 data item。

它提供：

- 文档元信息。
- 正文片段。
- 页码或范围上下文。
- 提取状态。
- 可选的原始内容引用。
- 可选的预览材料描述，例如 `text`、`markdown`、`html`、`raw_binary` 或 `url`。

它不负责 Manager 的最终展示 DTO。

`DocumentProvider` 不要求所有文档格式都必须在后端完成解析。对于 WPS、DOCX、PPTX 这类当前由前端 renderer 处理更合适的格式，可以先只声明 raw preview material 能力；后续如需全文索引、摘要、脱敏或服务端导出，再补后端文本提取能力。

### MediaProvider

`MediaProvider` 面向图片、音频、视频等媒体型 data item。

它提供：

- 媒体元信息。
- 缩略图或封面素材。
- 可访问内容引用。
- 可选的编码 / 解码辅助信息。
- 可选的预览材料描述，例如缩略图、raw binary 或可流式 URL。

它只返回已经确认的事实和可用素材，不硬凑完整预览对象。

### ContainerProvider

`ContainerProvider` 面向目录、压缩包、Excel 工作簿、SQLite / GeoPackage、文档集合等容器型 data item。

它提供：

- 子对象列表。
- 默认入口。
- 内部对象定位。
- 容器统计信息。

它不负责把内部对象解释成最终 table / document / media 预览；那部分交给对应 data type provider 继续处理。

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

## Manager Preview 边界

`TablePreview`、`DocumentPreview`、`MediaPreview` 这类对象属于 Manager 展示 DTO，不宜放在 format 层作为通用返回值。

合理分工是：

- format provider 提供格式原生可解析的结构事实或记录样本。
- data type provider 把不同来源整理成 table / document / media / container 的平台语义。
- Manager service 再组装成当前前端需要的 preview DTO。

如果 Manager preview 需要新增字段，应向 data type provider 或底层 format / engine provider 提需求，不能把 Manager DTO 反向下沉为 format 层通用模型。

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
5. Manager preview DTO 不进入 format 层。
6. GeoJSON 类结构表达为 `format=json` + `capabilities.spatial`，不作为独立顶层格式。

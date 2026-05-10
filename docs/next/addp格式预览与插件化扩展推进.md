# ADDP 格式预览与插件化扩展推进

更新时间：2026-05-10

本文记录 Manager 预览、`common/format`、纯文本 / 二进制兜底和第三方格式扩展的当前问题与推进思路。本文是 next 阶段工程推进文档，不替代正式规范；形成共识后，应分别回写到数据格式扩展指南、文件格式能力与 Data Type Provider 规范、内置数据格式规范和 Manager 内容预览插件规范。

## 一、当前问题

### 1. Manager 预览有两条能力链路

Manager 并不是所有格式都像 WPS 一样预览。

当前主要存在两类链路：

| 链路 | 典型格式 | 后端处理 | 是否走 `common/format/codecs` |
|---|---|---|---|
| 表格语义预览 | CSV、Excel、JSON records / GeoJSON、Parquet、Shapefile | Manager 构造资源读取器，再调用 `format.TableProvider` / `ComponentTableProvider` / `ScopeTableProvider` 得到字段与样本行 | 是 |
| 对象内容预览 | PDF、DOCX、WPS、PPTX、图片、Markdown、普通文本、未知对象 | Manager 通过 engine 的内容读取能力拿到对象流，再由 `ObjectContentRegistry` 选择内容 handler，组装 `ObjectPreviewContent` | 大多不走；部分 handler 内部复用 format provider |

例外情况：

- GeoJSON 内容预览会在 content handler 内复用 `format.GetTableProvider(format.FormatJSON)` 提取空间和字段摘要。
- Excel / SQLite / Parquet 的对象内容预览在 Manager 内部有专门 handler，部分复用 `common/format/codecs` 的解析能力或同类工具。
- DOCX / WPS / PPTX 当前主要是读取原始二进制并 base64 返回，由前端插件展示。

因此，`common/format/codecs` 少并不等于 Manager 不能预览；但这也暴露出职责分裂：格式识别、内容读取、预览展示、provider 能力分散在多个位置。

### 2. Manager 中存在过多格式硬编码

当前硬编码主要分布在：

- `PreviewResolver` 按 `data_type/format/organization` 拼 provider 名称。
- `object_content_plugin_loader.go` 内置大量 `builtin` content handler。
- `manager/backend/plugins/content/*.json` 再次声明扩展名、Content-Type、优先级。
- 前端 `/plugins/*.js` 再按 `kind`、扩展名、Content-Type 判断组件。

这会带来几个问题：

1. 增加新格式时，需要同时修改 Meta 检测、`common/format`、Manager content handler、插件 JSON、前端插件等多处。
2. 扩展名、MIME、format 名称在多个地方重复维护，容易不一致。
3. Manager 有时会回退到扩展名或 Content-Type 判断，弱化了“消费 Meta 已入库标准 attributes”的原则。
4. `ObjectPreviewContent.kind` 变成隐式协议，但没有统一 schema 或能力声明。

更优雅的方向不是把所有 registry 合成一个大 registry，而是建立统一的格式能力描述和能力发现视图，让各层按同一个 manifest / descriptor 派生各自注册项。

### 3. 二进制读取不应成为格式层的核心 provider

WPS / DOCX 当前链路基本是：

```text
Manager
  -> 根据 Meta attributes 选中对象 / 文件预览 provider
  -> 通过 engine plugin 的 ContentReadableProvider 打开对象流
  -> content handler 按大小限制读取二进制
  -> base64 编码
  -> 返回 ObjectPreviewContent{kind=docx|wps, data, encoding=base64}
  -> 前端插件解码展示
```

这条链路中，“从 engine 读取 bytes”是资源读取能力，不是格式解析能力。格式层不应通过 `engine id` 或连接信息读取内容，也不应成为所有二进制 passthrough 的入口。

但格式层需要提供一种轻量、统一的“原始内容预览材料”抽象，避免 Manager 为每种普通文件都写一套 handler。更合适的边界是：

- `common/resource`：负责打开资源流、组件流、scope。
- `common/format`：根据已确认 format / data_type，把流转成平台语义材料，例如文本片段、文档片段、媒体元信息、原始二进制引用策略。
- `manager`：负责分页、大小限制、base64 或直链策略、安全策略和最终 preview DTO。

因此，不建议新增一个“读取二进制 provider”让格式层自己读数据；建议新增或稳定 `ContentProvider` / `DocumentProvider` / `MediaProvider` 这类 data type provider，让它们消费调用方传入的 `io.Reader` 或内容引用，返回格式无关的预览材料。

### 4. WPS 的格式身份与预览材料不能混为一谈

WPS 是一个典型例子：

```text
format=wps
data_type=document
organization=single
当前后端解析能力：无稳定 DocumentProvider / extractor
当前预览能力：Manager 读取原始 bytes，按 base64 返回
当前渲染能力：前端 DocxPreview 使用浏览器端库和 WPS 兼容逻辑转换为 HTML
```

这说明要拆开五件事：

| 层级 | 回答的问题 | WPS 当前答案 |
|---|---|---|
| 格式身份 | 这个资源是什么格式 | `format=wps` |
| 资源读取 | 如何从 engine 读取内容 | engine plugin / `common/resource` 打开对象流 |
| 后端解析 | 后端能否提取文档语义 | 当前没有稳定能力 |
| 预览材料 | Manager 应把什么材料交给前端 | raw bytes，经 base64 或下载链接传递 |
| 前端渲染 | 浏览器如何展示 | WPS / DOCX 预览组件解码并转换 HTML |

因此，WPS 不应因为当前预览材料是二进制，就被表达为 `format=binary` 或 `data_type=unknown`。它仍然是文档格式，只是当前只声明 raw preview material 能力。

更合适的能力表达是：

```json
{
  "format": "wps",
  "data_type": "document",
  "providers": {
    "document": false,
    "preview_material": "raw_binary",
    "extract_text": false
  },
  "preview": {
    "material": "raw_binary",
    "encoding": ["base64", "url"],
    "frontend_renderer": "wps"
  }
}
```

这也适用于 DOCX、PPTX、PDF、图片等格式：有些格式适合后端提取文本或元数据，有些格式更适合把原始内容交给前端专用 renderer。平台应该声明这种差异，而不是让 Manager 通过硬编码猜。

### 5. Markdown / 文本被识别为 unknown 是概念缺口

Markdown 当前能预览，原因是 Manager content 插件按 `.md`、`text/markdown` 等匹配，并用文本 handler 返回 `kind=markdown`。但如果 Meta / `common/format` 没有把它识别为明确格式，就会出现：

```text
Meta attributes: data_type=unknown 或 format=unknown
Manager preview: 实际能按 markdown / text 展示
```

这与平台“Meta 是事实源，Manager 消费已入库 item”的原则冲突。

Markdown 不应因为没有复杂 codec 就是 unknown。它至少应被视为：

- `organization=single`
- `data_type=document`
- `format=markdown`

普通文本同理：

- `organization=single`
- `data_type=document`
- `format=text`

它们可以没有复杂 parser，但仍应有明确 FormatCapability 和最小 DocumentProvider / extractor。

## 二、纯文本与二进制兜底的判断

### 结论

应补齐纯文本和二进制兜底，但二者角色不同。

| 能力 | 建议表达 | 是否是 format | 说明 |
|---|---|---|---|
| 纯文本 | `data_type=document` + `format=text` | 是 | 可识别、可预览、可全文提取、可作为文档处理 |
| Markdown | `data_type=document` + `format=markdown` | 是 | 是文本型文档格式，不应落为 unknown |
| 二进制兜底 | `data_type=unknown` + `format=unknown`，preview kind 可为 `binary` | 暂不建议作为正式 format | 更像未知内容的安全展示策略，不是具体编码格式 |

### 为什么需要 text / markdown format

对于没有复杂结构的格式，仍然需要平台知道它是什么。否则会产生两类不一致：

- Meta 认为 unknown，Manager 却能预览。
- Search / Asset / Transfer 无法根据 format / data_type 判断文本提取、全文索引、导出策略。

因此，`common/format` 至少应声明：

- `FormatText`
- `FormatMarkdown`
- 对应 extension / MIME / magic 或内容探测规则
- `FormatCapability{DataType: document, Layouts: single, Preview: true, Parse: true 或 Extraction: true}`
- 最小文本提取能力：编码识别、BOM 处理、文本片段、截断信息

### 为什么 binary 暂不作为正式 format

“binary”描述的是内容无法安全解释为文本或已知格式时的处理方式，不是一个稳定文件格式。若把 binary 作为 format，可能会让未知文件都变成“已识别格式”，反而掩盖识别失败。

建议：

- 识别失败仍保持 `format=unknown`、`data_type=unknown`。
- Manager 或未来 `ContentProvider` 提供最后兜底的 `kind=binary`。
- 二进制预览只展示基础元信息、下载提示和有限 hex / magic bytes，不把内容当文本。
- 若未来确有治理需求，再讨论 `format=binary` 是否进入正式内置格式。

## 三、目标架构

目标是新增格式时，只在少数明确位置按接口实现，不再到处硬编码。

### 1. 能力声明统一

引入或扩展 `FormatDescriptor` / manifest，至少覆盖：

```json
{
  "id": "builtin-markdown",
  "format": "markdown",
  "data_type": "document",
  "layouts": ["single"],
  "identification": {
    "extensions": [".md", ".markdown"],
    "mime_types": ["text/markdown", "text/x-markdown"],
    "content_signatures": []
  },
  "providers": {
    "document": true,
    "content": true,
    "preview": true,
    "transfer_read": true,
    "transfer_write": false
  },
  "preview": {
    "kind": "markdown",
    "frontend_component": "MarkdownPreview"
  }
}
```

该 descriptor 不替代各 registry，而是作为共同事实源派生：

- format detection 规则
- FormatCapability
- provider hints
- Manager content handler 匹配规则
- 前端 preview manifest
- 能力发现视图

### 2. 检测规则注册化

`common/format/detection.go` 当前以 switch 为主，应逐步改为注册表：

- extension 规则
- MIME 规则
- magic bytes 规则
- 内容签名规则
- 优先级和冲突诊断

内置格式也通过同一注册入口注册，避免内置与第三方两套逻辑。

### 3. Provider 家族补齐

当前稳定的是 `TableProvider`。为了让文档、媒体、容器和兜底内容也不靠 Manager 硬编码，应逐步补齐：

| Provider | 面向 data_type | 最小能力 |
|---|---|---|
| `TableProvider` | table | schema、样本行、空间扩展 |
| `DocumentProvider` | document | 文档元信息、文本片段、页 / 段落片段、提取状态 |
| `MediaProvider` | media | 宽高、时长、编码、缩略图材料或原始引用策略 |
| `ContainerProvider` | container | children、默认入口、内部对象定位 |
| `ContentProvider` 或 `PreviewMaterialProvider` | 横切 | 将已确认格式的资源流转成 Manager 可包装的预览材料 |

其中 `ContentProvider` 的命名需要继续讨论。核心原则是：provider 消费 `io.Reader` / `ResourceReader`，不持有 engine 连接，不返回 Manager 专用 DTO。

### 4. Preview material 作为横切能力

`DocumentProvider`、`MediaProvider` 等 data type provider 不应被要求一次性承担所有预览方式。应明确区分：

| 能力 | 含义 | 示例 |
|---|---|---|
| `describe` | 返回类型元信息 | 文档页数、标题、图片宽高 |
| `extract` | 返回可检索 / 可分析内容 | 文本片段、OCR 文本、EXIF |
| `sample` | 返回结构化样本 | 表格行、图节点样本、容器 children |
| `preview_material` | 返回 Manager 可包装、前端可渲染的材料 | HTML、Markdown、纯文本、缩略图、raw bytes 引用 |

其中 `preview_material` 是横切能力，不等于格式解析完成。例如：

- Markdown：preview material 可以是 markdown text，前端负责渲染。
- Text：preview material 可以是 plain text。
- WPS：preview material 可以是 raw binary，由前端 WPS renderer 解析。
- PDF：preview material 可以是 raw binary 或页图像，取决于能力实现。
- Image：preview material 可以是 raw binary、缩略图或可访问 URL。

Manager 最终 DTO 仍由 Manager 组装；`common/format` 只返回格式无关的预览材料描述。

### 5. Manager 预览只做编排和 DTO

Manager 目标职责：

1. 读取 Meta 已入库 item 和 attributes。
2. 根据 `data_type + format + organization + capabilities` 选择读取计划。
3. 构造 `ResourceReader`、`ComponentReader`、`ScopePath` 或 native cursor。
4. 调用 data type provider / preview material provider。
5. 组装 Manager preview DTO。
6. 前端按 `kind` 和 manifest 选择组件。

Manager 不应：

- 为每个格式写扩展名和 MIME 判断。
- 自行推断 format / organization。
- 自行枚举 Shapefile sibling 组件。
- 把未知二进制当文本展示。

### 6. 第三方插件形态优先采用进程外协议

Go 原生 plugin 对平台、构建和部署约束较多。建议第三方格式扩展优先支持：

- manifest 声明能力。
- command / gRPC / HTTP 进程外 provider。
- 输入为标准资源引用、已确认 attributes、调用参数。
- 输出为标准 data type provider 结果或 preview material。

内置格式继续可以用 Go 静态注册，第三方扩展走同一 descriptor 和能力发现视图。

## 四、分阶段推进

### 阶段一：补齐兜底格式与识别口径

目标：消除“能预览但 Meta unknown”的明显不一致。

任务：

1. 在概念和内置格式规范中补 `text`、`markdown`、`unknown/binary preview` 口径。
2. `common/format` 补 `FormatMarkdown`，并为 `text`、`markdown` 注册 FormatCapability。
3. 检测规则支持 `.txt`、`.md`、`.markdown`、`text/plain`、`text/markdown`。
4. Meta 扫描将文本型文件写为 `data_type=document`，未知二进制保持 `unknown`。
5. Manager 的最后兜底从“文本全匹配”改为“文本探测通过才按文本，否则按 binary 兜底”。

Markdown 的修复原则：

- `.md` / `.markdown` / `text/markdown` 是格式识别知识，必须放在 `common/format`，不能在 Meta detector 中硬编码。
- Meta 可以有通用转换逻辑：根据 `format.GetFormatCapability(format).DataType` 推断 `data_type`，根据 `DataType` 派生默认 `item_type`。
- Meta 只保留 item 归并、组织方式、组件认领和内容结构判断，例如 Shapefile multi、whole scope、GeoJSON 内容结构。
- 第一阶段允许 Meta 保留旧 switch 作为 fallback，但新增格式必须优先通过 `FormatCapability` 生效。

### 阶段二：Manager 内容插件与 format capability 对齐

目标：减少 Manager 插件配置和 format 识别规则重复。

任务：

1. 让 Manager content plugin 匹配优先使用 Meta `format`，只有 `format=unknown` 时才允许扩展名 / MIME 回退。
2. 为内置 content handler 输出统一 preview material schema，至少区分 `text`、`html`、`markdown`、`json`、`image`、`raw_binary`、`url`。
3. 将 `ObjectPreviewContent.kind` 的稳定值整理成枚举或文档。
4. 为 WPS / DOCX 明确声明 `preview_material=raw_binary` 和 `frontend_renderer=wps|docx`，不再把二进制预览能力误称为二进制格式。
5. 前端 plugin manifest 不再重复猜格式，优先匹配后端返回的 `kind`、`frontend_renderer` 和 Meta attributes。

### 阶段三：FormatDescriptor / 能力发现视图

目标：新增格式时由一个 descriptor 派生多层注册信息。

任务：

1. 定义 `FormatDescriptor` Go 结构和 JSON manifest 子集。
2. 内置格式先用 descriptor 注册 detection 和 capability。
3. 能力发现视图输出 format、provider、preview、transfer 状态。
4. 冲突诊断记录插件 ID、版本、优先级和被覆盖规则。

### 阶段四：Provider 家族补齐

目标：把 Manager 中的文档、媒体、容器解析能力逐步下沉为格式层 provider。

任务：

1. 稳定 `DocumentProvider` 最小接口，先覆盖 text、markdown、pdf。
2. 稳定 `MediaProvider` 最小接口，先覆盖 image。
3. 稳定 `ContainerProvider` 最小接口，先覆盖 excel、sqlite。
4. 定义 preview material 横切结果模型，使 WPS 这类“前端解析型文档”可以只实现 raw material 能力。
5. Manager content handler 改为薄适配层：读取资源、调用 provider 或 preview material provider、包装 DTO。

### 阶段五：第三方格式插件

目标：第三方新增格式不需要修改 Manager、Meta、Transfer 多处硬编码。

任务：

1. 支持第三方 manifest 加载。
2. 支持 command 型 detector / extractor / provider。
3. 标准化输入输出 schema。
4. Meta 扫描记录使用的插件能力版本。
5. Manager 和 Transfer 只消费标准 data type provider 结果。

## 五、近期建议先做的决策

1. 确认 `markdown` 是否进入正式内置格式。当前倾向：进入。
2. 确认 `binary` 是否暂不进入正式 format。当前倾向：暂不进入，只作为 unknown 的兜底预览 kind。
3. 确认是否新增 `ContentProvider` / `PreviewMaterialProvider` 名称。当前倾向：先在文档中使用“preview material provider”描述能力，代码命名等接口稳定后再定。
4. 确认 Manager 文本兜底是否必须经过文本探测。当前倾向：必须，避免未知二进制乱码展示。
5. 确认第三方插件第一阶段采用进程外协议。当前倾向：是。

## 六、关联文档

- [ADDP 数据类型与格式体系图](../concepts/addp数据类型与格式体系图.md)
- [ADDP 数据格式扩展指南](../spec/addp数据格式扩展指南.md)
- [ADDP 文件格式能力与 Data Type Provider 规范](../spec/addp文件格式能力与DataTypeProvider规范.md)
- [ADDP 数据类型与格式模块边界规范](../spec/addp数据类型与格式模块边界规范.md)
- [ADDP 内置数据格式规范](../spec/addp内置数据格式规范.md)
- [ADDP Manager 内容预览插件能力构想](../plan/addpManager内容预览插件能力构想.md)
- [ADDP 第三方插件扩展声明构想](../plan/addp第三方插件扩展声明构想.md)
- [ADDP Registry 与能力发现层构想](../plan/addpRegistry与能力发现层构想.md)

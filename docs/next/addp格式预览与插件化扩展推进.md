# ADDP 格式预览与插件化扩展推进

更新时间：2026-05-10

本文记录 Manager 预览、`common/format`、纯文本 / 未知对象安全兜底和第三方格式扩展的当前问题与推进思路。本文是 next 阶段工程推进文档，不替代正式规范；形成共识后，应分别回写到数据格式扩展指南、文件格式能力与 Data Type Provider 规范、内置数据格式规范和 Manager 内容预览插件规范。

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

## 二、纯文本与未知对象兜底的判断

### 结论

应补齐纯文本格式和未知对象安全兜底，但二者角色不同。

| 能力 | 建议表达 | 是否是 format | 说明 |
|---|---|---|---|
| 纯文本 | `data_type=document` + `format=text` | 是 | 可识别、可预览、可全文提取、可作为文档处理 |
| Markdown | `data_type=document` + `format=markdown` | 是 | 是文本型文档格式，不应落为 unknown |
| 未知 / 不支持对象兜底 | `data_type=unknown` + `format=unknown`，preview kind 为 `unsupported`，preview material 可标记为 `raw_binary` | 不是 format | 更像未知内容的安全展示策略，不是具体编码格式 |

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
- Manager 或未来 `ContentProvider` 提供最后兜底的 `kind=unsupported`。
- 兜底预览可用 `preview_material=raw_binary` 表达“已读取的是原始二进制材料”，但不能把它提升为 `format=binary`。
- 不支持对象只展示基础元信息、下载提示和有限 hex / magic bytes，不把内容当文本。
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

### 6. 格式扩展先限定为 common/format 内部规范化扩展

当前阶段的“格式扩展”不指外部进程插件，也不指 Manager 前端预览插件，而是指在 ADDP 代码库内按 `common/format` 的规范补齐一种格式的能力。

新增格式时，目标是只在少数明确位置做针对性补充：

- 在 `common/format` 中声明 `FormatType`、descriptor、识别规则和 capability。
- 需要解析结构时，在 `common/format/codecs/<format>/` 实现对应 data type provider。
- 需要 Meta 组织方式特例时，只在 Meta 的组织 / 组件规则层补充特例。
- Manager、Transfer、Search 等上层模块优先消费标准 provider / capability / preview material，不再维护格式扩展名清单。

进程外插件、远程 provider、命令型扩展等能力暂不进入当前推进范围。等 ADDP 内部格式体系稳定后，再单独讨论外部插件协议。

## 四、分阶段推进

### 阶段一：补齐兜底格式与识别口径

目标：消除“能预览但 Meta unknown”的明显不一致。

状态：已完成第一轮落地。

任务：

1. 已完成：在概念和规范中补 `text`、`markdown`、unknown / unsupported preview 口径。
2. 已完成：`common/format` 补 `FormatMarkdown`，并为 `text`、`markdown` 注册 FormatCapability。
3. 已完成：检测规则支持 `.txt`、`.md`、`.markdown`、`text/plain`、`text/markdown`；并补齐 WPS MIME 映射。
4. 已完成：Meta 扫描将文本型文件写为 `data_type=document`；未知二进制保持 `unknown`。
5. 已完成：Manager 的最后兜底从“文本全匹配”改为“文本探测通过才按文本，否则返回 `kind=unsupported`，并在 metadata 中标记 `preview_material=raw_binary`”。

已落地代码记录：

- `common/format/detection.go`：声明 `FormatText` / `FormatMarkdown` 识别规则和 WPS MIME 映射。
- `common/format/capability/registry.go`：注册 `text`、`markdown` 的 document + single capability。
- `meta/backend/internal/dataitem/rule.go`：single resource 默认规则从 `FormatCapability` 派生，Meta 显式规则只保留特例。
- `manager/backend/internal/service/object_content_plugin.go`：新增 `unsupported` 兜底处理器，二进制不作为 format；未知 UTF-8 文本仍可安全按 text 预览。
- `manager/backend/plugins/content/190_content_text.json`：移除空匹配，避免 text handler 变成所有未知对象的兜底。
- `manager/backend/plugins/content/999_content_unsupported.json`：新增最终预览兜底，返回 `kind=unsupported`。

Markdown 的修复原则：

- `.md` / `.markdown` / `text/markdown` 是格式识别知识，必须放在 `common/format`，不能在 Meta detector 中硬编码。
- `.txt` / `text/plain` 同样由 `common/format` 的 `text` capability 声明，Meta 不单独维护文本格式清单。
- Meta 可以有通用转换逻辑：根据 `format.GetFormatCapability(format).DataType` 推断 `data_type`，根据 `DataType` 派生默认 `item_type`。
- Meta 只保留 item 归并、组织方式、组件认领和内容结构判断，例如 Shapefile multi、whole scope、GeoJSON 内容结构。
- 第一阶段允许 Meta 保留旧 switch 作为 fallback，但新增格式必须优先通过 `FormatCapability` 生效。
- 第二阶段将 Meta 的 single resource 规则调整为“Meta 显式特例 + `FormatCapability` 派生默认规则”：普通 single 格式由 capability 推导 `data_type` 和默认 `item_type`，Meta 显式规则只表达组织方式、组件、whole scope、容器 children、内容结构判断或历史补充。

### 阶段二：Manager 内容插件与 format capability 对齐

目标：减少 Manager 插件配置和 format 识别规则重复。

状态：进行中，已完成协议显性化、内置 handler descriptor 默认匹配和内置 JSON 配置收薄的第一步。

任务：

1. 已完成第一步：Manager MIME 推断已改为复用 `common/format.GuessContentType`；content plugin matcher 已避免泛型 MIME 误命中仅声明 content type 的 handler；PDF、DOCX、WPS、PPTX、Image、JSON、Excel、SQLite、Markdown、Parquet 等内置插件的 JSON 匹配配置已收薄，默认从 descriptor 派生。
2. 部分完成：`ObjectPreviewContent` 已新增 `preview_material` 和 `frontend_renderer` 字段；内置 handler 已开始输出 `text`、`markdown`、`json`、`geojson`、`image`、`raw_binary`、`table` 等材料类型。
3. 部分完成：后端内置 `ObjectPreviewContent.kind` 已常量化，`kind` / `preview_material` 稳定取值已回写到正式规范；默认 `frontend_renderer` 已优先从 descriptor 派生，`preview_material` 仍按实际返回材料判断。
4. 已完成第一步：WPS / DOCX / PPTX / PDF 这类 base64 原始内容预览已声明 `preview_material=raw_binary`，并输出对应 `frontend_renderer`。
5. 部分完成：前端内置预览插件已优先匹配后端返回的 `frontend_renderer`，再回退到旧的 `kind`、扩展名、Content-Type 判断；后续仍需将 public plugin manifest 化，减少脚本内重复猜格式。

已落地代码记录：

- `manager/backend/internal/models/models.go`：`ObjectPreviewContent` 新增 `preview_material`、`frontend_renderer`。
- `manager/backend/internal/service/object_content_plugin.go`：内置 handler 统一补齐 preview material 与 renderer；命令型插件返回值也会被补齐默认字段。
- `manager/backend/internal/models/models.go`：内置 preview kind 与 preview material 已抽成后端常量。
- `docs/spec/addp文件格式能力与DataTypeProvider规范.md`：已记录 `ObjectPreviewContent.kind`、`preview_material`、`frontend_renderer` 的协议字段和稳定取值。
- `manager/backend/internal/service/object_preview.go`：`inferContentType` 改为复用 `common/format.GuessContentType`，减少 Manager 自维护扩展名 / MIME switch。
- `manager/backend/internal/service/object_content_plugin_loader.go`：PDF、DOCX、WPS、PPTX、Image、JSON、Excel、SQLite、Markdown、Parquet 等内置 content handler 的默认 format / extension / MIME 匹配开始从 `common/format` descriptor 派生，保留 Shapefile、GeoJSON 和 Text 的必要特例。
- `manager/backend/plugins/content/*.json`：内置 PDF、DOCX、WPS、PPTX、Image、JSON、Excel、SQLite、Markdown、Parquet 等 content plugin 配置已移除重复扩展名 / MIME 清单，只保留 Shapefile 和 Text 的必要特例。
- `manager/backend/internal/service/object_preview_test.go`：新增真实 content plugin 配置加载测试，验证收薄后的 JSON 仍能通过 descriptor 默认匹配命中内置 handler。
- `manager/frontend/public/plugins/*-preview.js`：PDF、DOCX、WPS、PPTX、Image、JSON、GeoJSON、Excel、SQLite、Markdown、Table、Shapefile 等内置前端预览插件开始优先消费 `frontend_renderer`，保留旧判断作为过渡兜底。

### 阶段三：FormatDescriptor / 能力发现视图

目标：新增格式时由一个 descriptor 派生多层注册信息。

状态：进行中，已完成 registry 共同事实源、capability 派生、detection 消费、能力发现视图、实现状态展示和冲突诊断第一步。

任务：

1. 已完成第一步：定义 `FormatDescriptor` Go 结构和 JSON manifest 子集，内置 descriptor 覆盖当前 capability registry 已声明的核心格式。
2. 部分完成：`common/format` 的扩展名、MIME 识别、`FormatToMIME` 和格式类别 helper 已优先读取 descriptor / capability，再回退旧 switch；Excel / TSV 等历史表格预览口径仍保留过渡兜底。
3. 已完成第一步：新增独立 `common/format/registry` 子包作为 descriptor 共同事实源，避免 `capability` 子包反向 import 父包形成 import cycle。
4. 已完成第一步：内置 `common/format/capability` 已由 `registry.Descriptor` 派生初始化，减少 capability 与 descriptor 双清单漂移。
5. 已完成：能力发现视图已可输出 format、provider、preview、transfer 状态，并通过 `implementations` 区分 descriptor 声明能力与当前进程已注册的 provider / extractor 实现。
6. 已完成第一步：冲突诊断已记录 descriptor 注册中的 format、extension、MIME 冲突，包含插件 ID、版本、优先级和是否覆盖。
7. 未完成：Meta 插件版本记录和 Manager public plugin manifest 自动派生。

已落地代码记录：

- `common/format/registry/descriptor.go`：新增独立 format registry 共同事实源，负责 descriptor 注册、覆盖优先级和冲突诊断。
- `common/format/registry/discovery.go`：新增 descriptor 声明视图，输出 format、provider、preview、transfer 状态。
- `common/format/descriptor.go` / `common/format/discovery.go`：保留顶层 facade API，避免上层直接依赖 registry 子包；`discovery.go` 在 facade 层补充 `implementations`，展示当前进程实际注册的 TableProvider、DocumentProvider、MediaProvider 和 MetadataExtractor。
- `common/format/capability/registry.go`：内置 capability 从 `registry.Descriptor` 派生初始化。
- `common/format/detection.go`：扩展名、MIME、主 MIME 输出和格式类别 helper 优先消费 descriptor / capability，旧 switch 保留为过渡兜底。
- `common/format/descriptor_test.go`：验证 descriptor 覆盖 `text`、`markdown`，并与当前 capability registry 的核心字段保持一致。
- `common/format/detection_test.go`：验证 `text`、`markdown`、`parquet` 等识别路径已通过 descriptor 生效。
- `common/format/discovery_test.go`：验证能力发现视图能区分声明 provider 与实际 provider / extractor 注册状态。

### 阶段四：Provider 家族补齐

目标：把 Manager 中的文档、媒体、容器解析能力逐步下沉为格式层 provider。

状态：进行中，已完成 `DocumentProvider` 最小接口与 text / markdown 内置实现；已完成 `MediaProvider` 最小接口与 image / jpeg / png / gif / tiff 内置实现。

任务：

1. 部分完成：稳定 `DocumentProvider` 最小接口，已覆盖 text、markdown 的 UTF-8 文本片段提取；PDF DocumentProvider 待补。
2. 已完成第一步：稳定 `MediaProvider` 最小接口，已覆盖 image / jpeg / png / gif / tiff 的宽高、编码、MIME 和 GeoTIFF 空间属性提取。
3. 稳定 `ContainerProvider` 最小接口，先覆盖 excel、sqlite。
4. 定义 preview material 横切结果模型，使 WPS 这类“前端解析型文档”可以只实现 raw material 能力。
5. Manager content handler 改为薄适配层：读取资源、调用 provider 或 preview material provider、包装 DTO。

已落地代码记录：

- `common/format/provider.go`：新增 `DocumentInfo`、`DocumentProvider`、文档 provider 注册表和查询入口。
- `common/format/codecs/text/provider.go`：新增 text / markdown 最小 DocumentProvider，消费调用方传入的 `io.Reader`，返回 UTF-8 文本片段，不读取 engine、不返回 Manager DTO。
- `common/format/builtin/init.go`：内置注册入口纳入 text / markdown DocumentProvider。
- `common/format/provider.go`：新增 `MediaInfo`、`MediaProvider`、媒体 provider 注册表和查询入口。
- `common/format/codecs/image/parser.go`：复用图片 metadata extractor 能力，实现 image / jpeg / png / gif / tiff 最小 MediaProvider。
- `common/format/codecs/image/provider_test.go`：验证 PNG MediaProvider 可返回宽高、MIME 和格式信息。

### 阶段五：内部格式扩展规范化

目标：新增格式时，开发者只需要在 `common/format` 按规范补充 descriptor、provider 和必要 codec，上层模块通过标准能力自动消费。

状态：进行中，已完成 descriptor manifest 加载、注册基础能力和一批现有预览型格式的 descriptor 收拢；外部进程插件不在当前范围。

任务：

1. 已完成第一步：支持 descriptor manifest JSON 文件 / 目录加载，并通过 `RegisterFormatDescriptor` 同步注册到 descriptor registry 与 capability registry。
2. 已完成第一步：把“新增内部格式”的代码位置和最小实现清单固化到本文，覆盖 descriptor、detection、provider、codec、Meta 特例、Manager preview material。
3. 部分完成：为典型内部格式补模板或示例，已覆盖 `text` / `markdown` / `parquet` / `shapefile` 的落地参照；后续可补 `pdf` / `image` / `excel` / `sqlite`。
4. 未完成：Manager 和 Transfer 只消费标准 data type provider 结果，逐步删除格式清单重复维护。
5. 暂不推进：外部进程插件、command 型 provider、远程 provider。

已落地代码记录：

- `common/format/manifest.go`：新增 `FormatPluginManifest`、单文件 manifest 加载 / 注册和目录批量注册入口。
- `common/format/manifest_test.go`：验证 manifest 注册后 capability、MIME detection 都能看到新格式。
- `common/format/registry/descriptor.go`：收拢 PDF、DOCX、PPTX、WPS、image、JPEG、PNG、GIF、TIFF、Excel、SQLite 等已有格式身份、MIME、preview material 和 frontend renderer 声明。除已有 transfer 语义明确的格式外，这些预览型格式暂不声明 transfer read/write。
- `meta/backend/internal/dataitem/rule.go`：从 capability 派生 single resource 规则时要求有 entry，避免 `image` 这类逻辑聚合格式被误作为可扫描文件格式。

内部新增格式最小清单：

1. 在 `common/format/detection.go` 声明或复用 `FormatType` 常量。
2. 在 `common/format/registry/descriptor.go` 增加 descriptor，至少声明 `format`、`data_type`、`layouts`、`identification.extensions/mime_types`、`preview.kind`、`preview.preview_materials`、`preview.frontend_renderer`。
3. 只有确实已有批量读写或稳定导入导出能力时，才声明 `transfer_read/write`，不要把“能预览”误写成 transfer 能力。
4. 如需结构解析，在 `common/format/codecs/<format>/` 实现对应 provider，例如 `TableProvider`、`DocumentProvider`。provider 消费 `io.Reader` / `ResourceReader`，不接 engine id，不返回 Manager DTO。
5. 如果格式是多文件、whole scope、容器或有特殊 item 组织方式，只在 Meta 规则层补组织特例；普通 single resource 格式由 capability 派生。
6. Manager 预览优先消费 `format`、`preview_material`、`frontend_renderer` 和标准 provider 结果，不新增扩展名 / MIME 硬编码清单。

## 五、近期建议先做的决策

1. 确认 `markdown` 是否进入正式内置格式。当前倾向：进入。
2. 确认 `binary` 是否暂不进入正式 format。当前结论：暂不进入；未知二进制对象保持 `format=unknown`，Manager 预览返回 `kind=unsupported`，仅用 `preview_material=raw_binary` 表达材料类型。
3. 确认是否新增 `ContentProvider` / `PreviewMaterialProvider` 名称。当前倾向：先在文档中使用“preview material provider”描述能力，代码命名等接口稳定后再定。
4. 确认 Manager 文本兜底是否必须经过文本探测。当前倾向：必须，避免未知二进制乱码展示。
5. 确认外部进程插件是否进入当前阶段。当前结论：暂不进入；先把 ADDP 内部 `common/format` 扩展路径做顺。

## 六、关联文档

- [ADDP 数据类型与格式体系图](../concepts/addp数据类型与格式体系图.md)
- [ADDP 数据格式扩展指南](../spec/addp数据格式扩展指南.md)
- [ADDP 文件格式能力与 Data Type Provider 规范](../spec/addp文件格式能力与DataTypeProvider规范.md)
- [ADDP 数据类型与格式模块边界规范](../spec/addp数据类型与格式模块边界规范.md)
- [ADDP 内置数据格式规范](../spec/addp内置数据格式规范.md)
- [ADDP Manager 内容预览插件能力构想](../plan/addpManager内容预览插件能力构想.md)
- [ADDP 第三方插件扩展声明构想](../plan/addp第三方插件扩展声明构想.md)
- [ADDP Registry 与能力发现层构想](../plan/addpRegistry与能力发现层构想.md)

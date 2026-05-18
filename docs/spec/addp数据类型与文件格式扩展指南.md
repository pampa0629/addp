# ADDP 数据类型与文件格式扩展指南

本文是新增或修改数据类型、文件格式、容器格式、多组件格式、whole scope 数据集或引擎原生数据表示时的实施清单。概念解释不在本文重复，先读：

- [ADDP 数据项体系图](../concepts/addp数据项体系图.md)
- [ADDP 数据类型和格式体系图](../concepts/addp数据类型和格式体系图.md)
- [ADDP 数据类型与格式能力规范](addp数据类型与格式能力规范.md)
- [ADDP 数据项探测器规范](addp数据项探测器规范.md)
- [ADDP 元数据 attributes 规范](addp元数据attributes规范.md)

## 一句话流程

```text
判断 data item 组织方式
  -> 判断 data type 和 format
  -> 实现 FormatPlugin
  -> 实现需要的 info provider / content reader
  -> 注册 descriptor 和实现
  -> 补 Meta detector / attributes 映射
  -> 补测试和内置规范
```

## 1. 判断数据类型

先判断用户和平台如何理解这个 data item，而不是先看后缀。

| 判断结果 | 适用条件 | 后续要实现的能力 |
|---|---|---|
| `table` | 有字段、行列、记录集合，或能稳定映射成字段集合 | `TableInfoProvider`，需要内容样本时实现 `TableSampleReader` |
| `document` | 以阅读、正文提取、全文索引为主 | `DocumentInfoProvider`，需要后端文本时实现 `DocumentTextReader`；否则至少声明 raw / range content reader |
| `media` | 图片、视频、音频等可感知媒体 | `MediaInfoProvider`，需要缩略图时实现 media content reader |
| `container` | 内部包含 sheet、table、layer、entry 等子对象 | `ContainerInfoProvider` / `ContainerChildResolver`；父容器先写入轻量 `type_info.container`，child 内容按需解析 |
| `graph` | 节点、边、关系结构 | `GraphInfoProvider` / `GraphSampleReader` 目标能力；引擎原生图通常走 engine capability |
| `unknown` | 暂不能归类 | 只保留 storage、item 和必要 raw / range content 能力 |

只有以上数据类型无法表达用户理解方式、内容读取方式和治理方式时，才新增 data type。新增 data type 必须先修订概念文档和能力规范。

## 2. 判断组织方式

组织方式决定 Meta 如何把资源归并成 data item。

| 组织方式 | 判断标准 | 需要补的规则 |
|---|---|---|
| `single` | 一个引擎资源就是一个 data item | entry 识别规则、`meta_item.full_name` 来源 |
| `multi` | 多个明确相关 ref 共同组成一个 data item | 主 ref、必需 ref、可选 ref、claims |
| `whole` | 整个目录、prefix、schema 或扫描范围构成一个 data item | whole scope 根范围、manifest / 关键资源、exclusive 策略 |

容器不是组织方式。Excel、SQLite、GeoPackage、ZIP 等外层通常仍是 `single + container`。

规则归属：

- 组织方式、主资源、组件、claims / exclusive 写入 [ADDP 数据项探测器规范](addp数据项探测器规范.md) 或对应实现。
- 首批内置格式的确定性规则写入 [ADDP 内置数据类型与文件格式规范](addp内置数据类型与文件格式规范.md)。

## 3. 实现 FormatPlugin

新增文件格式应在 `common/format/plugins/<format>/` 下完成主实现。一个稳定 format ID 对应一个长期维护的格式包，descriptor、provider、reader 和测试都应尽量在该目录闭合。

最小文件结构：

```text
common/format/plugins/<format>/
  plugin.go
  parser.go        # 可选，内部解析细节
  plugin_test.go
```

`plugin.go` 至少实现：

```go
type Plugin struct{}

func (p *Plugin) Format() format.FormatType
func (p *Plugin) Descriptor() format.FormatDescriptor
func (p *Plugin) Capabilities() format.FormatCapability
```

`Descriptor()` 必须声明：

| 字段 | 要求 |
|---|---|
| `ID` | 稳定 ID，例如 `builtin-csv` |
| `Format` | 稳定 format ID，例如 `csv` |
| `DataType` | 默认 data type |
| `Layouts` | `single`、`multi`、`whole` |
| `Identification` | 扩展名、MIME、内容签名 |
| `Providers` | 声明 info provider 能力 |
| `ContentReaders` | 声明内容读取能力 |

`Capabilities()` 表达当前实现实际具备的能力。descriptor 是格式身份声明；capability 是实现能力声明；二者都不是某个 data item 的扫描结果。

如果该格式暂时只有识别、默认 data type、layout、transfer 或 content reader 声明，没有后端解析实现，也要建立 descriptor-only plugin 包并实现 `Descriptor()` / `Capabilities()`。不要把新格式补到 `common/format/registry/descriptor.go`；该目录只保留运行时注册表机制。

## 4. 实现 provider 和 reader

按 data type、组织方式和消费意图实现对应接口，不要把 info、sample、连续读写混在一起。详细矩阵见 `common/format/README.md`。

| 场景 | 必需 / 推荐接口 | 注册后主要消费者 |
|---|---|---|
| 格式身份与能力声明 | `FormatPlugin` | Meta、Manager、Transfer、能力发现 |
| 格式私有元信息 | `FormatInfoProvider` | Meta |
| 单资源表格元信息 | `TableInfoProvider` | Meta、Manager、Transfer 探查 |
| 单资源表格样本 | `TableSampleReader` | Manager、Transfer 探查 |
| 单资源表格全量读取 | `TableReaderProvider` | Transfer 主链路 |
| 单资源表格写出 | `TableWriterProvider` | Transfer 写侧 |
| 多组件表格元信息 / 样本 | `MultiTableProvider` | Meta、Manager、Transfer 探查兜底 |
| 多组件表格全量读取 | `MultiTableReaderProvider` | Transfer 主链路 |
| 多组件表格写出 | `MultiTableWriterProvider` | Transfer 写侧 |
| 多组件规格 / 展示描述 | `RelatedRefSpecProvider`、`RefDescriptorProvider` | Meta item detector、Manager |
| scope 表格元信息 / 样本 | `ScopeTableProvider` | Meta、Manager、Transfer 探查 |
| 文档元信息 | `DocumentInfoProvider` | Meta、Manager、Search |
| 文档文本片段 | `DocumentTextReader` | Manager、Search |
| 文档仅前端解析 | descriptor 声明 `raw_content` / `range_content`，后端不实现 `DocumentTextReader` | Manager |
| 媒体元信息 | `MediaInfoProvider` | Meta、Manager |
| 容器内部对象信息 | `ContainerInfoProvider` | Meta、Manager |
| 容器 child 解析 | `ContainerChildResolver` | Manager、Transfer 后续 child 读取 |
| 空间横切事实 | 在 table/media info 中提供候选事实，由 Meta 写入 `capabilities.spatial` | Meta、Manager、Search |

历史组合接口：

- `TableProvider` = `TableInfoProvider` + `TableSampleReader`
- `DocumentProvider` = `DocumentInfoProvider` + `DocumentTextReader`
- `MediaProvider` 是 `MediaInfoProvider` 的旧别名

新增实现优先直接使用拆分后的接口。`MultiTableProvider` 和 `ScopeTableProvider` 只表达 info / sample，不表达连续全量读取；全量批处理读取应使用 `TableReaderProvider` 或 `MultiTableReaderProvider`。后续如果 scope 表格进入 Transfer 主链路，再新增明确的 `ScopeTableReaderProvider`，不得继续扩展组合接口职责。

## 5. 注册方式

推荐在格式子目录的 `init()` 中注册 FormatPlugin：

```go
func init() {
    if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
        panic(err)
    }
}
```

`RegisterFormatPlugin` 会：

1. 校验 `Format()` 与 `Descriptor().Format` 一致。
2. 在当前进程尚未注册该 format descriptor 时注册 `Descriptor()`。
3. 根据 plugin 实现的接口自动注册 info provider 和 content reader。

也可以按能力手动注册：

| 入口 | 使用场景 |
|---|---|
| `RegisterFormatDescriptor` | 只新增格式身份声明，暂时没有 Go 实现 |
| `RegisterFormatInfoProvider` | 只提供 `format_info.<format>` |
| `RegisterTableInfoProvider` | 只提供 `type_info.table` |
| `RegisterTableSampleProvider` | 只提供 table sample content reader |
| `RegisterTableReaderProvider` | 只提供单资源 table 连续读取 |
| `RegisterMultiTableReaderProvider` | 只提供多组件 table 连续读取 |
| `RegisterTableWriterProvider` | 只提供单资源 table 写出 |
| `RegisterMultiTableWriterProvider` | 只提供多组件 table 写出 |
| `RegisterDocumentInfoProvider` | 只提供 `type_info.document` |
| `RegisterDocumentTextReader` | 只提供 document text content reader |
| `RegisterMediaInfoProvider` | 只提供 `type_info.media` |

内置格式还必须加入统一加载入口：

```go
// common/format/builtin/init.go
import _ "github.com/addp/common/format/plugins/<format>"
```

调用方通过 blank import `github.com/addp/common/format/builtin` 一次性加载内置 descriptor、provider / reader 和 type mapper。Meta、Manager、Transfer 或测试不应分别散落导入多个具体格式包。

新增格式时按以下规则判断注册位置：

| 变更 | 应修改的位置 |
|---|---|
| 新增稳定内置 format ID | 新建 `common/format/plugins/<format>/`，实现 `Descriptor()`，并加入 `common/format/builtin/init.go` |
| 已有 format 补解析、预览、样本、批量读写能力 | 修改已有 `common/format/plugins/<format>/`，必要时更新同目录测试 |
| 仅第三方或实验格式 | 通过 plugin manifest 或独立包调用 `RegisterFormatPlugin` / `RegisterFormatDescriptor`，不进入内置加载入口 |
| Manager 需要消费新格式 | 优先消费 capability view、provider / reader 和已入库 attributes，不新增按后缀或引擎类型的 switch |

## 6. 确认 Meta 是否已有通用消费链路

FormatPlugin 不生成最终 data item，但新增格式不必然修改 Meta。普通 `single` 文件格式如果已经能通过 descriptor 声明 `data_type`、`layouts`、`identification`、providers 和 content readers，Meta 应优先通过通用链路消费 `common/format` 能力。

需要检查：

| 检查项 | 不需要改 Meta 的情况 | 需要改 Meta 的情况 |
|---|---|---|
| item 识别 | 现有 single / multi / whole 通用规则可表达 | 新组件规则、manifest 规则、whole scope 规则无法表达 |
| data type / format | descriptor 能给出默认 `data_type` / `format`，或现有内容探测规则可判断 | 需要内容探测后动态决定 data type，且现有 detector 不支持 |
| attributes 映射 | provider 结果已能进入现有 `type_info`、`format_info`、`capabilities` 或 `content_index` | 需要新增标准字段、分区或 normalizer 映射 |
| content index | 不需要索引，或已有通用索引结构可复用 | 需要新增索引结构、锚点语义或失效规则 |
| 容器内部对象 | 现有 `type_info.container.children` 可表达 | 需要新增内部对象模型、默认入口规则或独立子 item 规则 |

如果以上检查都能复用现有通用链路，新增格式应只改 `common/format` 和必要测试，不应修改 Meta、Manager 或 Transfer。

只有突破现有 data item 识别或 attributes 标准映射能力时，才补 Meta detector / normalizer。即便需要补 Meta，也只补通用规则或明确的格式规则，不在 Manager 中按后缀硬编码新格式。

同一事实只能写一个位置。内容样本、原始内容、前端渲染器、Manager DTO 不得写入 `type_info` 或 `format_info`。

## 7. 验证清单

新增或修改格式后至少验证：

1. `FormatPlugin.Descriptor()` 的 format、data type、layouts、identification、providers、content readers 正确。
2. `RegisterFormatPlugin` 后，`ListFormatCapabilityViews()` 能看到声明能力和实现状态。
3. Meta 扫描生成正确数量的 data item。
4. `meta_item.name/full_name/item_type/node_id` 符合探测器规范。
5. `attributes.item/type_info/format_info/content_index/capabilities` 没有重复事实源。
6. multi / whole 场景没有重复落库。
7. Manager 只消费已入库 data item，不按引擎类型或后缀硬编码新格式。
8. Transfer 不重复推断字段类型、组件或组织方式。

推荐测试：

```bash
go test ./common/format/...
go test ./manager/backend/internal/service
go test ./meta/backend/internal/metaitem ./meta/backend/internal/service
```

如果只改文档，可不运行 Go 测试，但必须检查链接和旧术语残留。

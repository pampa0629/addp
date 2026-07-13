# ADDP 数据类型与文件格式扩展指南

新增 CAD 格式时必须优先判断是否需要保留 CAD 原生图层、块、布局、标注等语义。DWG 使用既有 `data_type=cad`，不得因为 entity 可投影为行而归为 `table`；CAD→GIS 必须作为显式转换生成新的 table item。第一阶段 DWG deep scan 和预览统一使用 `supermap_workflow` 的 direct operator，不在 Meta 或 Manager 进程嵌入 SuperMap/ODA SDK，也不保留第二解析路线。

本文是新增或修改数据类型、文件格式、容器格式、多组件格式、whole scope 数据集或引擎原生数据表示时的实施清单。概念解释不在本文重复，先读：

- [ADDP 数据项体系图](../concepts/addp数据项体系图.md)
- [ADDP 数据类型和格式体系图](../concepts/addp数据类型和格式体系图.md)
- [ADDP 数据类型与格式能力规范](addp数据类型与格式能力规范.md)
- [ADDP 数据项探测器规范](addp数据项探测器规范.md)
- [ADDP 元数据 attributes 规范](addp元数据attributes规范.md)

## 一句话流程

```text
判断 data item 内容布局
  -> 判断 data type 和 format
  -> 复用或补充 common/datatype 通用模型
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
| `document` | 以阅读、正文提取、全文索引为主 | `DocumentInfoProvider`，需要后端文本时实现 `DocumentTextReader`；原始文件预览由 engine / contentio / URL 内容通道提供 |
| `media` | 图片、视频、音频等可感知媒体 | `MediaInfoProvider`，需要缩略图时实现 media content reader |
| `container` | 内部包含 sheet、table、layer、entry 等子对象 | `ContainerInfoProvider` / `ContainerChildResolver`；父容器先写入轻量 `type_info.container`，child 内容按需解析 |
| `graph` | 节点、边、关系结构 | 引擎原生图使用 `CatalogFactsProvider` 的 `CatalogFacts.Graph` / `GraphSampleProvider`；文件型图数据先补对应 format 能力 |
| `model_3d` | 三维空间对象、网格、场景、构件、倾斜摄影或 BIM 模型 | `Model3DInfoProvider`；第一阶段可先实现 descriptor 和轻量 info，预览通过 Manager content reader / storage stream |
| `point_cloud` | 三维点集合、点属性、空间范围和抽样 / LOD 是主要消费方式 | `PointCloudInfoProvider`；需要预览时实现点云抽样 reader 或 Manager 派生预览能力 |
| `gaussian_splat` | 三维高斯基元、尺度、旋转、不透明度和视角相关颜色是主要消费方式 | `GaussianSplatInfoProvider`；需要预览时实现高斯泼溅 renderer 或 Manager 派生 splat artifact |
| `unknown` | 暂不能归类 | 只保留 storage、item 等基础事实；原始字节读取由 engine / contentio / URL 内容通道或 `BinaryContentReader` 的探测兜底提供 |

只有以上数据类型无法表达用户理解方式、内容读取方式和治理方式时，才新增 data type。新增 data type 必须先修订概念文档和能力规范。

通用 data type、type info、field type、空间信息和访问索引的事实源是 `common/datatype`。新增格式或 provider 不得在格式包内新增平行的 `FieldType`、`TableInfo`、`DocumentInfo`、`MediaInfo`、`ContainerInfo`、`Model3DInfo`、`PointCloudInfo`、`GaussianSplatInfo` 等公共模型；确有新增通用字段时，先修订 `common/datatype` 设计和相关规范。

三维模型和点云的常见边界：

- GLB / glTF、OBJ、STL、单 OSGB、OSGB Scene、3D Tiles、IFC / Revit BIM 等统一归为 `model_3d`；网格模型、倾斜摄影、BIM 和分块场景由 `type_info.model_3d.model_kind`、format、layout 和 capabilities 区分，不新增平行 data type。
- LAS / LAZ / COPC、PCD、点云型 PLY、EPT / Potree、E57 等统一归为 `point_cloud`；不得仅因点记录可以列化为 x/y/z、intensity、classification 等字段就归为 `table`。
- 3D Gaussian Splatting PLY、`.splat`、`.ksplat`、`.spz` 等统一归为 `gaussian_splat`；不得仅因底层记录形似点集合而归为 `point_cloud`，也不得走 mesh / GLB 的 `model_3d` 快显路线。
- PLY 是内容敏感格式：`face_count > 0` 时归为 `model_3d`；无 face 且具备传统 3DGS 属性，或具备 SuperSplat 压缩 PLY 的 `chunk` element 与 `packed_position` / `packed_rotation` / `packed_scale` / `packed_color` vertex 属性时归为 `gaussian_splat`；无 face 且不满足高斯泼溅属性时归为 `point_cloud`。
- CRS、空间定位和空间范围仍是 `capabilities.spatial`，不是 `model_3d`、`point_cloud` 或 `gaussian_splat` 私有字段。

## 2. 判断内容布局

内容布局决定 Meta 如何把资源归并成 data item。

| 内容布局 | 判断标准 | 需要补的规则 |
|---|---|---|
| `single` | 一个引擎资源就是一个 data item | entry 识别规则、`meta_item.full_name` 来源 |
| `multi` | 多个明确相关 ref 共同组成一个 data item | 主 ref、必需 ref、可选 ref、claims |
| `whole` | 整个目录、prefix、schema 或扫描范围构成一个 data item | whole scope 根范围、manifest / 关键资源、exclusive 策略 |

容器不是内容布局。Excel、SQLite、GeoPackage、ZIP 等外层通常仍是 `single + container`。

规则归属：

- 内容布局、主资源、组件、claims / exclusive 写入 [ADDP 数据项探测器规范](addp数据项探测器规范.md) 或对应实现。
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
```

如果该格式提供静态 descriptor，再实现：

```go
func (p *Plugin) Descriptor() format.FormatDescriptor
```

`Descriptor()` 必须声明：

| 字段 | 要求 |
|---|---|
| `ID` | 稳定 ID，例如 `builtin-csv` |
| `Format` | 稳定 format ID，例如 `csv` |
| `DataType` | 默认 data type |
| `Layouts` | `single`、`multi`、`whole` |
| `Identification` | 扩展名、确定性直接子文件名、确定性相对路径、MIME、内容签名；嵌套 manifest 使用 `RelativePaths`，不得把路径退化为任意文件名匹配 |

`Descriptor()` 是格式身份、识别规则、默认 data type 和 layout 的静态事实源，不是某个 data item 的扫描结果，也不声明当前 Go 进程是否已有 provider / reader / writer。当前进程实际加载了哪些实现，只能由已注册 `FormatPlugin` 是否实现对应接口动态判断。

如果该格式暂时只有识别、默认 data type 或 layout，没有后端解析实现，也要建立 descriptor-only plugin 包并实现 `Descriptor()`。不要在 `common/format` 根包集中追加内置 descriptor 清单；根包只保留 descriptor 注册、查询和冲突诊断机制。

## 4. 实现 provider 和 reader

按 data type、内容布局和消费意图实现对应接口，不要把 info、sample、连续读写混在一起。详细矩阵见 `common/format/README.md`。

| 场景 | 必需 / 推荐接口 | 注册后主要消费者 |
|---|---|---|
| 格式身份 | `FormatPlugin` | Meta、Manager、Transfer、能力发现 |
| 格式私有元信息 | `FormatInfoProvider` | Meta |
| 单资源表格元信息 | `TableInfoProvider` | Meta、Manager 探查、Transfer 规划 |
| 单资源表格样本 | `TableSampleReader` | Manager、轻量探查 |
| 单资源表格全量读取 | `TableReaderProvider` | Transfer 主链路 |
| 单资源表格写出 | `TableWriterProvider` | Transfer 写侧 |
| 多组件表格元信息 | `MultiTableInfoProvider` | Meta、Manager 探查、Transfer 规划 |
| 多组件表格样本 | `MultiTableSampleReader` | Manager、轻量探查 |
| 多组件表格全量读取 | `MultiTableReaderProvider` | Transfer 主链路 |
| 多组件表格写出 | `MultiTableWriterProvider` | Transfer 写侧 |
| 多组件规格 / 展示描述 | `RelatedRefSpecProvider`、`RefDescriptorProvider` | Meta item detector、Manager |
| scope 表格元信息 | `ScopeTableInfoProvider` | Meta、Manager 探查、Transfer 规划 |
| scope 表格样本 | `ScopeTableSampleReader` | Manager、轻量探查 |
| 文档元信息 | `DocumentInfoProvider` | Meta、Manager、Search |
| 文档文本片段 | `DocumentTextReader` | Manager、Search |
| 文档仅前端解析 | descriptor 保留 document 静态身份，后端不实现 `DocumentTextReader`；Manager 基于 engine/contentio 或自身 fetcher 提供 raw/range 内容 | Manager |
| 媒体元信息 | `MediaInfoProvider` | Meta、Manager |
| 容器内部对象信息 | `ContainerInfoProvider` | Meta、Manager |
| 容器 child 解析 | `ContainerChildResolver` | Manager、Transfer 后续 child 读取 |
| 三维模型元信息 | `Model3DInfoProvider` | Meta、Manager |
| 点云元信息 | `PointCloudInfoProvider` | Meta、Manager |
| scope 点云元信息 | `ScopePointCloudInfoProvider` | Meta、Manager |
| 高斯泼溅元信息 | `GaussianSplatInfoProvider` | Meta、Manager |
| 空间横切事实 | 通过 describe result 或等价结构提供 `datatype.SpatialInfo`，由 Meta 写入 `capabilities.spatial` | Meta、Manager、Search |
| 访问定位索引 | 通过 describe result 或等价结构提供 `datatype.AccessIndex`，由 Meta 写入 `access_index.<data_type>`；`AccessIndex` 不是 data type 或 type info | Meta、Manager、Transfer |

新增实现必须直接使用拆分后的接口。multi / scope 的 info、sample、连续全量读取必须分别使用对应接口；Transfer 主链路读取 whole scope table 必须使用明确的 `ScopeTableReaderProvider`，不得引入组合 provider，也不得用 sample reader 冒充全量读取。

Info provider 一次解析可能同时得到多类事实。以 table 为例：

| 解析结果 | 写入位置 |
|---|---|
| `datatype.TableInfo` | `attributes.type_info.table` |
| `datatype.Model3DInfo` | `attributes.type_info.model_3d` |
| `datatype.PointCloudInfo` | `attributes.type_info.point_cloud` |
| `datatype.GaussianSplatInfo` | `attributes.type_info.gaussian_splat` |
| `datatype.SpatialInfo` | `attributes.capabilities.spatial` |
| `datatype.AccessIndex` | `attributes.access_index.table` |
| `format_info.<format>` 候选事实 | `attributes.format_info.<format>` |

这些事实应作为同级结果交给 Meta normalizer，不得为了调用方便把 `SpatialInfo`、`AccessIndex` 或 `format_info` 塞进 `TableInfo`。

`datatype.AccessIndex` 当前只是跨 format、Meta、Manager 复用的访问索引结构暂存位置。新增格式不得因为需要内容定位索引而扩展 `TableInfo` 或新增 data type；索引事实只进入 `attributes.access_index.<data_type>`。

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
3. 将该 plugin 作为该 format 的唯一运行时实现入口。后续 `GetTableInfoProvider`、`GetDocumentTextReader`、`GetTableWriterProvider` 等查询都会对同一个 plugin 做接口断言。

| 入口 | 使用场景 |
|---|---|
| `RegisterFormatDescriptor` | 只新增格式身份声明，暂时没有 Go 实现 |
| `RegisterFormatPlugin` | 注册该 format 的 Go 实现；该实例可同时实现一个或多个 provider / reader / writer 接口 |

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
| 已有 format 补解析、预览、样本、连续读写实现 | 修改已有 `common/format/plugins/<format>/`，必要时更新同目录测试 |
| 仅第三方或实验格式 | 通过 plugin manifest 或独立包调用 `RegisterFormatPlugin` / `RegisterFormatDescriptor`，不进入内置加载入口 |
| Manager 需要消费新格式 | 优先消费 descriptor、provider / reader 和已入库 attributes，不新增按后缀或引擎类型的 switch |

## 6. 确认 Meta 是否已有通用消费链路

FormatPlugin 不生成最终 data item，但新增格式不必然修改 Meta。普通 `single` 文件格式如果已经能通过 descriptor 声明 `data_type`、`layouts`、`identification`、providers 和 content readers，Meta 应优先通过通用链路消费 `common/format` 能力。

需要检查：

| 检查项 | 不需要改 Meta 的情况 | 需要改 Meta 的情况 |
|---|---|---|
| item 识别 | 现有 single / multi / whole 通用规则可表达 | 新组件规则、manifest 规则、whole scope 规则无法表达 |
| data type / format | descriptor 能给出默认 `data_type` / `format`，或现有内容探测规则可判断 | 需要内容探测后动态决定 data type，且现有 detector 不支持 |
| attributes 映射 | provider 结果已能进入现有 `type_info`、`format_info`、`capabilities` 或 `access_index` | 需要新增标准字段、分区或 normalizer 映射 |
| access index | 不需要索引，或已有通用索引结构可复用 | 需要新增索引结构、锚点语义或失效规则 |
| 容器内部对象 | 现有 `type_info.container.children` 可表达 | 需要新增内部对象模型、默认入口规则或独立子 item 规则 |

如果以上检查都能复用现有通用链路，新增格式应只改 `common/format` 和必要测试，不应修改 Meta、Manager 或 Transfer。

只有突破现有 data item 识别或 attributes 标准映射能力时，才补 Meta detector / normalizer。即便需要补 Meta，也只补通用规则或明确的格式规则，不在 Manager 中按后缀硬编码新格式。

同一事实只能写一个位置。内容样本、原始内容、前端渲染器、Manager DTO 不得写入 `type_info` 或 `format_info`。`SpatialInfo` 不写入 `type_info.table`；`AccessIndex` 不写入 `type_info.table` 或 `format_info`。

## 7. 验证清单

新增或修改格式后至少验证：

1. `FormatDescriptorProvider.Descriptor()` 的 format、data type、layouts、identification 正确。
2. `RegisterFormatPlugin` 后，具体 `Get*Provider` / `Get*Reader` / `Get*Writer` 能按插件实际接口实现返回或明确失败。
3. Meta 扫描生成正确数量的 data item。
4. `meta_item.name/full_name/item_type/node_id` 符合探测器规范。
5. `attributes.item/type_info/format_info/access_index/capabilities` 没有重复事实源。
6. multi / whole 场景没有重复落库。
7. Manager 只消费已入库 data item，不按引擎类型或后缀硬编码新格式。
8. Transfer 不重复推断字段类型、组件或内容布局。

推荐测试：

```bash
go test ./common/format/...
go test ./manager/backend/internal/service
go test ./meta/backend/internal/metaitem ./meta/backend/internal/service
```

如果只改文档，可不运行 Go 测试，但必须检查链接和旧术语残留。

# common/format 抽象框架收敛记录

本文记录 `common/format` 中 `FormatPlugin`、`FormatDescriptor`、provider / reader / writer 关系的已确认收敛方向和落地状态。当前文档是 `docs/next` 下的工作记录，正式概念口径已同步到术语表、格式能力规范、格式扩展指南和 `common/format/README.md`。

## 本轮落地状态

本轮已完成以下收敛：

1. `FormatPlugin` 已收敛为只包含 `Format() FormatType` 的最小身份接口。
2. `FormatDescriptorProvider` 已独立表达 descriptor 提供能力；`RegisterFormatPlugin` 只在 plugin 实现该接口时注册 descriptor。
3. `RegisterTableInfoProvider`、`RegisterDocumentTextReader` 等独立 provider / reader / writer 注册入口已删除；动态能力查询统一从已注册 plugin 做接口断言。
4. `FormatDescriptor` 已删除 `Providers`、`ProviderHints`、`ContentReaders`、`Parse`、`Spatial` 字段，只保留格式身份、默认 data type、layout 和识别事实。
5. `FormatSupportView` / `FormatImplementationStatus` 已退出公开核心概念，诊断场景改为 `FormatCapabilitySnapshot` / `FormatImplementationSnapshot`。
6. `FormatType` 已上移为 `common/format` 根包领域类型；descriptor 注册、查询和冲突诊断机制也已内聚到根包，不再保留 `common/format/registry` 子包。
7. 内置格式插件、`common/format` 测试、Manager / Meta / Transfer 相关消费链路已迁移到新主路径。

本轮已完成领域类型和 descriptor 机制的根包收敛；格式 ID 只有 `format.FormatType` 一个主路径。

## 背景问题

当前实现中存在几个概念不够收敛的问题：

1. `Provider` 作为只包含 `Format() FormatType` 的基础接口，名字过大，容易和具体 info provider、writer provider、engine provider 混淆。
2. 当前 `FormatPlugin` 同时包含 `Format()` 和 `Descriptor()`，导致 format 身份存在两个入口：`plugin.Format()` 和 `plugin.Descriptor().Format`。
3. `FormatDescriptor.Providers`、`ContentReaders` 等字段混合了“静态事实”和“当前 Go 进程实际加载实现状态”，容易出现 descriptor 声明与 provider 注册状态不一致。
4. `format.FormatType` 曾由底层机制包间接定义，领域归属方向不够自然；现已改为根包领域类型。

## 已确认抽象方向

把 `FormatPlugin` 收敛为所有格式实现的最小身份接口：

```go
type FormatPlugin interface {
	Format() FormatType
}
```

`FormatPlugin` 表示“这个实现属于哪个 format”，不直接等同于 descriptor 承载者，也不直接等同于某一种 provider。新增格式应至少有一个 `FormatPlugin` 实现；该实现可按需同时实现具体 provider / reader / writer。

descriptor 由单独接口表达：

```go
type FormatDescriptorProvider interface {
	FormatPlugin
	Descriptor() FormatDescriptor
}
```

具体能力继续按消费意图拆分，并组合 `FormatPlugin`：

```go
type TableInfoProvider interface {
	FormatPlugin
	DescribeTable(...)
}

type TableSampleReader interface {
	FormatPlugin
	SampleTable(...)
}

type TableReaderProvider interface {
	FormatPlugin
	OpenTableReader(...)
}
```

这样可以避免新增 `FormatBound` 等额外概念，同时替代当前语义过大的 `Provider`。

provider / reader / writer 的动态可用性只由已注册的 `FormatPlugin` 实例是否实现对应接口决定：

```go
func GetTableInfoProvider(formatType FormatType) (TableInfoProvider, error) {
	plugin, err := GetFormatPlugin(formatType)
	if err != nil {
		return nil, err
	}
	provider, ok := plugin.(TableInfoProvider)
	if !ok {
		return nil, fmt.Errorf("format %s has no table info provider", formatType)
	}
	return provider, nil
}
```

因此已删除单独的 `RegisterTableInfoProvider`、`RegisterDocumentTextReader` 等 provider / reader 注册入口，避免 plugin 断言和独立 provider map 形成双轨。

## 关键原则

1. 严格区分静态事实和动态实现。`FormatDescriptor` 只承载静态事实；provider / reader / writer 是否可调用，只能通过已注册 plugin 的接口实现动态查询。
2. `FormatDescriptor` 仍是 struct，不改为 interface。它是可校验、可序列化、可从 manifest 加载的静态事实结构。
3. provider / reader / writer 不强制携带 descriptor。它们只需要通过 `Format()` 声明自己归属哪个 format。
4. `Descriptor()` 是静态描述能力，不是每个 provider 的必需能力。
5. `FormatSupportView` 和 `FormatImplementationStatus` 不再作为核心公开概念。需要总览时，可以由 `FormatDescriptor` 列表和 plugin 接口断言临时派生诊断快照。
6. 当 `FormatDescriptorProvider` 同时提供 `Format()` 和 `Descriptor().Format` 时，二者必须一致；不自动补齐 descriptor 中的空 format。

## FormatDescriptor 字段分析

| 字段 | 处理结论 | 原因 |
| --- | --- | --- |
| `ID` | 保留 | descriptor / manifest 的稳定身份，用于冲突诊断和第三方格式包识别。 |
| `Version` | 保留为可选 | 用于第三方 manifest、内置格式声明版本和冲突诊断；不参与运行时能力判断。 |
| `Priority` | 保留 | 用于扩展名、MIME、signature 冲突时排序；它不是 data item 优先级。 |
| `Format` | 保留 | 稳定格式 ID，是 descriptor 的核心身份字段。 |
| `I18nKey` | 可弱化为 override | 默认可由 `format.<format>` 派生；仅在需要覆盖默认 key 时显式填写。 |
| `DataType` | 保留 | 表达该格式默认归属的数据类型，不能从 provider 实现可靠推导。 |
| `Layouts` | 保留 | 表达格式支持的 data item 组织形态，不能仅靠 provider 注册状态推导。 |
| `Identification` | 保留 | 扩展名、MIME、内容签名是静态识别事实，不能从 provider 动态获得。 |
| `Providers` | 删除 | provider 是动态实现状态，应由 plugin 接口断言查询，不写入 descriptor。 |
| `ContentReaders` | 删除 | Go reader 是动态实现状态；`raw_content`、`range_content` 是 engine / contentio 内容通道能力，也不属于 format 静态事实。 |
| `ProviderHints` | 删除 | 可由 `DataType`、`Layouts` 和动态 provider 实现状态派生，手工维护价值低。 |
| `Parse` | 删除 | 是否具备解析能力应由具体 info provider / reader / writer 实现状态表达。 |
| `Spatial` | 删除 | 当前 item 是否具有空间事实必须来自扫描结果；格式是否可能产生空间事实不作为当前版本 descriptor 字段。 |

收敛后的 `FormatDescriptor` 目标形态：

```go
type FormatDescriptor struct {
	ID             string
	Version        string
	Priority       int
	Format         FormatType
	I18nKey        string
	DataType       datatype.DataType
	Layouts        []Layout
	Identification FormatIdentification
}
```

## 动态实现查询

provider / reader / writer 不再有独立注册表。`RegisterFormatPlugin` 只注册 `FormatPlugin` 实例，动态实现查询统一对该实例做接口断言：

- `GetFormatPlugin(formatType)`
- `GetFormatDescriptorProvider(formatType)`
- `GetTableInfoProvider(formatType)`
- `GetDocumentTextReader(formatType)`
- `GetTableWriterProvider(formatType)`

manifest-only 或 descriptor-only 格式只能注册静态 descriptor，不能声明运行时 provider。没有 Go plugin，就没有动态实现。

`raw_content` / `range_content` 不进入 `FormatDescriptor`，也不进入 format plugin registry。它们由 engine store capability、`contentio` 能力和消费者策略共同决定。

## FormatSupportView 处理结论

取消 `FormatSupportView` 和 `FormatImplementationStatus` 作为核心公开概念。

原因：

1. `FormatSupportView` 的静态字段与 `FormatDescriptor` 高度重复。
2. `FormatImplementationStatus` 的动态 bool 与 plugin 接口断言重复。
3. 每新增 provider / reader / writer 都要修改状态结构，扩展性差。
4. 容易被调用方误认为第三个事实源。

若 Console、诊断接口或测试仍需要格式能力总览，应提供临时派生的 snapshot API。该 snapshot 不是事实源，只是：

1. 读取 `ListFormatDescriptors()` 的静态事实。
2. 对同 format 的已注册 `FormatPlugin` 做接口断言。
3. 生成展示或诊断结果。

## FormatType 与 registry 的方向

调整前，`FormatType` 由底层机制包间接定义。这个方向不够自然；`FormatType` 是 `common/format` 的领域类型，descriptor 注册和冲突诊断机制应该服务于 `common/format`，而不是反过来定义领域类型。

已调整为：

```go
// common/format
type FormatType string
```

同时选择将 descriptor 存储和冲突诊断逻辑上移到 `common/format` 根包，删除 `common/format/registry` 子包。这样 `FormatType`、`FormatDescriptor`、descriptor 注册表和能力发现都归属于同一个领域边界。

## 迁移清单落地结果

1. 已修订正式文档：
   - `docs/concepts/addp术语表.md`
   - `docs/spec/addp数据类型与格式能力规范.md`
   - `docs/spec/addp数据类型与文件格式扩展指南.md`
   - `common/format/README.md`
2. 已将当前 `Provider` 基础接口替换为瘦身后的 `FormatPlugin`。
3. 已新增 `FormatDescriptorProvider`，将当前 `FormatPlugin` 的 `Descriptor()` 职责迁移过去。
4. 已调整 `RegisterFormatPlugin`：
   - 接收 `FormatPlugin`。
   - 注册 format 实现身份。
   - 如果实现了 `FormatDescriptorProvider`，再注册 descriptor。
   - 不再根据实际实现的 provider / reader / writer 接口自动挂接到独立 map。
5. 已清理 `plugin.Format()` 与 `plugin.Descriptor().Format` 的双事实入口：
   - 二者同时存在时必须完全一致。
   - 不再自动把 `plugin.Format()` 补入空的 `descriptor.Format`。
6. 已删除或降级 `FormatSupportView` / `FormatImplementationStatus`：
   - 核心调用方改为直接查询 `FormatDescriptor` 和具体 provider。
   - 如 UI 或诊断需要，改成临时派生 snapshot。
7. 已删除字段：
   - `Providers`
   - `ProviderHints`
   - `ContentReaders`
   - `Parse`
   - `Spatial`
8. 已调整 `FormatType` 定义方向：
   - 将领域类型放回 `common/format` 根包。
   - descriptor 存储和冲突诊断机制已内聚到 `common/format` 根包。
   - 已删除 `common/format/registry` 子包，不再保留反向定义领域类型的结构。
9. 已删除独立 provider / reader / writer 注册 map 和注册函数：
   - provider 查询改为从已注册 plugin 做接口断言。
   - 保留兼容分支不符合当前开发原则，迁移时应一次性删除旧路径。
10. 已更新内置格式插件和测试：
   - CSV / TSV
   - Shapefile
   - JSON
   - Parquet / ORC / Avro
   - Document / Media / Container 相关插件
11. 已执行验证命令：
   - `go test ./common/format/...`
   - `go test ./manager/backend/internal/preview ./manager/backend/internal/objectcontent`
   - `go test ./meta/backend/internal/extractor ./meta/backend/internal/metaenrich`
   - `go test ./transfer/backend/internal/api ./transfer/backend/internal/planner`

## 已确认问题结论

1. descriptor 中不保留“声明能力”。manifest-only 或 descriptor-only 格式只表达静态格式事实，不表达未来目标 provider。
2. `Spatial` 从 descriptor 删除。真实空间事实来自扫描结果；格式级可能性暂不建字段。
3. `raw_content` / `range_content` 不放入 `ContentReaders`。它们由 engine / contentio / 消费者策略决定。
4. `RegisterFormatDescriptor` 继续允许独立注册静态 descriptor；Go 实现统一通过 `RegisterFormatPlugin` 注册。
5. `FormatType` 上移后，`common/format/registry` 已内聚到根包并删除，不再保留反向定义领域类型的结构。

## 上层迁移落地补充

1. Transfer executor 的 encoded table 读写字段命名已从旧 `FormatProvider` 口径收敛为具体连续读写能力：
   - `SourceTableReadProvider`
   - `SourceMultiReadProvider`
   - `SourceScopeReadProvider`
   - `TargetTableWriterProvider`
   - `tableProvider`
   - `multiReaderProvider`
   - `scopeReaderProvider`
   - `tableWriterProvider`
   Transfer 全量读取不再使用 `TableSampleReader` / `MultiTableSampleReader` / `ScopeTableSampleReader` 兜底；sample reader 只服务 Manager 预览和轻量探查，不能冒充全量迁移 reader。
2. Transfer capabilities 接口仍保留 `csv`、`tsv`、`json`、`jsonl`、`geojson`、`parquet`、`shapefile` 这些用户可选值。这里的 `jsonl` / `geojson` 是 UI 输出形态或编码选项，不是新增顶层 format identity：
   - `jsonl` 使用 `backend_type=json` 和 `json_mode=jsonl`。
   - `geojson` 使用 `backend_type=json` 和 `spatial.target_encoding=geojson`。
   - 可执行能力来自 `FormatDescriptor` 和已加载 plugin 的接口实现状态，而不是 descriptor 内的静态能力字段。
   - 基础表格格式候选已从 `FormatDescriptor` 列表和已加载 plugin 的 table reader / writer 实现状态派生；`jsonl` / `geojson` 仅作为 `FormatJSON` 的用户侧编码变体额外展开。
3. Transfer 前端向导已删除 encoded table / raw copy 的默认格式能力清单兜底：
   - Step1 / Step2 从 `/transfer/capabilities` 读取 `table_formats` 和 `raw_copy_formats`。
   - `useTaskWizardState` 保存同一份 readable encoded formats 和 raw copy formats，底部“下一步”判断与页面展示使用同一个能力源。
   - Step2 目标格式的写出扩展名和空间分组来自 `/transfer/capabilities`，前端只保留 label / hint 展示映射，不再用本地格式清单推断扩展名或空间分组。
   - capabilities 加载失败时不再默认启用 CSV / Parquet / Shapefile 等格式；后端未声明的 encoded 格式暂不可用。
   - 旧任务 JSON / 历史 endpoint 形态不做兼容回填；向导不会根据 endpoint resource 伪造 source item，source item 只来自当前 Meta 资源树选择结果。
4. Manager object content 已从 `builtin:content-parquet` / `parquetContentHandler` 收敛为通用 `builtin:content-table` / `tableContentHandler`，表格内容入口按 `data_type=table` 和当前进程是否具备 table info/sample provider 判断。
5. Manager scope table preview 已收敛为 `layout=whole` 的单一路径，只使用 `ScopeTableInfoProvider` / `ScopeTableSampleReader`；单文件表继续由 `builtin:file-table` 和 single table provider 处理。
6. Manager 容器 child 预览继续保留动态扫描和按需解析能力：
   - `ContainerChildResolver` 仍负责从父容器中解析 child resource。
   - 嵌套 ZIP / 嵌套容器仍允许在预览时按路径逐层解析。
   - unknown child 可在本次预览中读取内容前缀做 `DetectFormat`，该动态识别只服务本次预览，不写回 Meta，也不替代正式扫描事实。
   - table child 进入表格预览时，优先复用已有 Meta table attributes；没有已知表结构时才要求对应 table info provider，避免破坏容器动态预览。
7. Manager file-table 路由和执行已按表格预览所需能力收紧：单文件表至少需要 `TableSampleReader`，并且需要已有 Meta table attributes 或当前进程具备 `TableInfoProvider`；multi table 需要 `MultiTableInfoProvider` 和 `MultiTableSampleReader` 同时存在。
8. Meta table file 识别不再维护本地表格扩展名清单，也不再对 unknown extension 默认 parquet；规则来源按 format descriptor 派生，但可读性按 layout 分别判断：
   - `layout=single` 必须有 `TableInfoProvider`。
   - `layout=whole` 必须有 `ScopeTableInfoProvider`。
   文件级候选、单文件补充和主要格式检测只使用 single table provider，避免把目录级能力误当成单文件可读能力。
9. Meta refresh 中清理旧 `access_index.table` 的逻辑已从 `format=shapefile` 特判泛化为 `layout=multi + data_type=table`。multi table provider 如果本次刷新能重新产出 access index，仍可通过 `TableDescribeResult.AccessIndex` 写回；否则不会保留上一轮单资源索引。
10. Shapefile import 的 ZIP 业务约束仍属于 Manager 上传入口；允许 / 必需 sidecar 扩展名已从 Shapefile plugin 的 `RelatedRefSpecs()` 派生，避免 Manager 维护第二份 ref 清单。
11. Transfer table / raw copy 任务配置解析已改为 strict JSON decode：
   - 只接受 `TableExportTaskSpec` / `RawCopyTaskSpec` 明确声明的字段。
   - 旧 `engine.scope` 不再作为代码概念存在；如果调用方继续传入，会作为未知字段失败。
   - 任务配置事实源只保留 `engine.id` / `engine.type`、`resource`、`data_type`、`representation`、`format`、`options`、`policy` 和 `source.meta_item_id` 等新主路径字段。
12. Transfer source 选择树继续复用 common 资源树语义，树内容来自 Meta 构造结果，不引入 Transfer 专属的“非 Meta item”过滤概念：
   - Step1 不再基于文件名或扩展名推断 `data_type` / `format`。
   - source 是否可选只消费 Meta tree 节点携带的标准事实，例如 `data_type`、`representation`、`format` 和 `attributes.item`。
   - 创建 source endpoint 时优先使用 common tree 中已有的 `item_id` 写入 `source.meta_item_id`。
   - Manager 预览树和 Transfer source 树继续共享资源树展示语义；差异只体现在各模块对已知 Meta 事实的消费策略。

## GeoJSON 格式身份后续修订记录

当前代码和正式规范仍暂按“不升格”执行：`.geojson`、`application/geo+json` 和 `application/vnd.geo+json` 归一为 `format=json`，空间事实由扫描结果写入 `capabilities.spatial`。本轮不修改 `FormatJSON`、JSON plugin 或 `IsGeospatialFormat` 的格式身份语义；Transfer 只把用户侧 `geojson` 作为输出格式 / 空间编码选项处理，不把它混入 common format identity。

后续正式修订时，倾向将 GeoJSON 升格为独立 format identity，原因如下：

1. `spatial` 不是 data type，这一点不变；空间能力仍是横切事实。
2. Shapefile 是独立格式，且格式本身必然属于空间矢量表；没有 `.shp` 主文件时，`.dbf` 不能称为 Shapefile，只能作为 related content。
3. JSON 是独立格式；GeoJSON 因确定的空间内容结构，例如 `{"type":"Feature"}` 或 `{"type":"FeatureCollection"}`，具备足够明确的格式身份，后续应考虑独立 `FormatGeoJSON` 和 `common/format/plugins/geojson`。
4. GeoJSON 不能只靠扩展名判断。`.geojson` 可作为强提示，但 `.json` 也可能承载 GeoJSON，必须通过内容探测确认。
5. CSV 即使包含 WKT / WKB / EWKB 字段，格式本身仍是 CSV，不因此成为空间格式；其空间性只来自扫描结果中的 `SpatialInfo` / `capabilities.spatial`。

待升格时需要同步修改正式文档、README、JSON / GeoJSON detection、descriptor、plugin 注册、测试，以及 Transfer 中将 `geojson` 作为输出格式 / 空间编码的消费链路。升格前，`IsGeospatialFormat` 不应把 `FormatJSON` 视为空间格式，也不应通过 `format=geojson` 推断空间事实。

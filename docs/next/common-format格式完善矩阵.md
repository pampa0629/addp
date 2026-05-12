# common/format 格式完善矩阵

更新时间：2026-05-12

本文用于持续推进 `common/format` 各类格式的完善工作。它不是正式规范，不重复定义概念；正式规则以以下文档为准：

- [ADDP 数据类型与格式能力规范](../spec/addp数据类型与格式能力规范.md)
- [ADDP 数据类型与文件格式扩展指南](../spec/addp数据类型与文件格式扩展指南.md)
- [ADDP 内置数据类型与文件格式规范](../spec/addp内置数据类型与文件格式规范.md)
- [ADDP 数据类型与文件格式跟进清单](addp数据类型与文件格式跟进清单.md)
- [ADDP 数据类型与文件格式待规范事项](addp数据类型与文件格式待规范事项.md)

## 状态标记

| 标记 | 含义 |
|---|---|
| 已完成 | 已有清晰实现，且基本符合当前规范方向。 |
| 部分完成 | 已有实现，但仍有兼容入口、硬编码、归拢不完整或验证不足。 |
| 待补齐 | descriptor 或实现缺关键能力，不能视为格式主线完成。 |
| 暂不需要 | 当前 data type / format 定位下不需要该能力。 |
| 待核实 | 需要用真实样例或端到端流程确认用户体验。 |

## 完善标准

| 标准 | 目标 |
|---|---|
| 抽象入口 | 格式在 `common/format/plugins/<format>/` 下有稳定 FormatPlugin 或等价主入口，不再以零散 parser / legacy extractor 作为新增主线。 |
| descriptor / 注册 | `FormatDescriptor` 声明 format、data type、layout、identification、providers、content readers，并通过 `RegisterFormatPlugin` 或 `RegisterFormatDescriptor` 注册。 |
| info provider | 按 data type 提供 `TableInfoProvider`、`DocumentInfoProvider`、`MediaInfoProvider`、`FormatInfoProvider` 或容器 children 信息来源。 |
| content reader | 按需要提供 `TableSampleReader`、`DocumentTextReader`、raw / range content 声明、component / scope table reader 等内容读取能力。 |
| Meta 硬编码消除 | 普通 single 格式由 descriptor / capability 通用链路识别；只有 multi、whole、容器 children 或 attributes 映射突破通用能力时保留明确 detector / normalizer。 |
| Manager 硬编码消除 | Manager 后端尽量基于 data item attributes、resource 抽象、descriptor、provider / reader 能力选择内容 handler，而不是维护独立后缀清单。 |
| 前端渲染 | Manager 前端有对应预览插件或通用渲染兜底，且渲染器不反向约束 `common/format`。 |
| 代码归拢 | 格式解析、provider、reader、writer、测试尽量归拢到 `common/format/plugins/<format>/`，避免分散到 Meta / Manager / Transfer 重复实现。 |
| 目录文件整理 | 格式目录内文件名按职责简洁统一，例如 `plugin.go` 作为主入口，`provider.go` / `reader.go` / `parser.go` / `writer.go` / `analyzer_internal.go` 按用途拆分；为旧代码做适配的 wrapper / adapter 要明确标注并逐步收敛。 |
| 大数据量高性能预览 | Manager 预览大文件或大对象时应支持分页、抽样、range read、content_index、row group / block / page 等局部读取能力，避免为了首屏预览全量读取或全量解析。 |
| 使用体验核实 | 用真实或代表性样例完成扫描、预览、分页、异常提示、空间 / 容器信息展示等体验核实。 |

## 格式矩阵

| 格式 | 抽象入口 | descriptor / 注册 | info provider | content reader | Meta 硬编码消除 | Manager 硬编码消除 | 前端渲染 | 代码归拢 | 目录文件整理 | 大数据量高性能预览 | 使用体验核实 | 下一步 |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 分隔文本表格（CSV / TSV） | 部分完成：当前一套代码注册 `csv` / `tsv` 两个 format ID，目标口径是同一格式族按 delimiter profile 区分 | 部分完成：当前 descriptor 分列 CSV / TSV；目标是统一分隔文本表格能力，扩展名 / MIME / delimiter 作为识别 profile | 已完成：table / format info | 已完成：table sample、raw 声明、CSV / TSV 稀疏行索引策略 | 部分完成：single 可走 capability；`content_index` 已改为 deep-only 且由格式声明能力驱动，表格文件 detector 仍参与 schema 写入 | 部分完成：文件表预览走 provider，缺失 `content_index` 时可按格式能力调用 Meta 按需建立，但仍有 preview DTO / handler 体系 | 已完成：table 预览 | 已完成：实现已在 `plugins/csv/` 归拢 | 部分完成：`plugin.go` 同时承担主入口、解析和 reader 逻辑，后续可拆出 reader / analyzer 文件 | 部分完成：已有 `content_index.table` 稀疏行索引和 range preview，basic 扫描不再建索引；失效规则和极大文件体验待核实 | 待核实 | 收敛 CSV / TSV 的文档和能力视图口径，保留不同 profile；补 content_index 失效规则，核实大文件分页、编码、表头识别。 |
| JSON（含 GeoJSON 空间结构） | 已完成：同一 `FormatJSON` 插件处理 JSON / GeoJSON | 已完成：同一 descriptor 声明 `.json`、`.geojson` 和 `application/geo+json` | 部分完成：默认 data type 仍为 document；内容解析为 GeoJSON FeatureCollection 或顶层对象数组时可提供 table info，只有实际发现 geometry 结构才提供 `SpatialInfo` | 已完成：table sample、raw 声明；无 geometry 的记录集合不会虚构 geometry 字段，WKB / GeoJSON geometry 值可作为空间字段线索 | 部分完成：`capabilities.spatial` 仅由 `SpatialInfo` 等内容事实写入，已去除单资源 `.geojson` 默认空间能力 | 部分完成：Manager 保留 JSON / GeoJSON 展示 handler，GeoJSON handler 只按显式线索匹配，解析失败回退 JSON；空间展示属于前端表格 + 空间渲染方式 | 已完成：json / geojson 预览 | 已完成：格式解析在 JSON 目录内，展示代码在 Manager | 部分完成：`parser.go` 同时承载 plugin、iterator 和 table reader；serializer 单独存在，后续可按 plugin / reader / iterator 整理 | 部分完成：JSON 记录集合可流式样本，并已提供 `content_index.table` 稀疏行索引；超大文件分页体验和索引失效规则待核实 | 待核实 | 核实 CSV 转 JSON 对象数组、无 geometry FeatureCollection、真实 GeoJSON 的扫描 attributes、content_index 按需构建与 Manager 表格 + 空间渲染体验。 |
| Parquet | 已完成 | 已完成 | 已完成：table info | 已完成：table sample、scope table sample、raw 声明 | 部分完成：single / whole detector 已有，仍需端到端样例核实 | 部分完成：文件表预览走 provider，whole scope 路由需持续核实 | 已完成：table 预览 | 已完成 | 部分完成：已有 `parser.go`、`provider.go`、`table_file.go`，但主入口命名可向 `plugin.go` 收敛 | 部分完成：Parquet 天然适合 row group 局部读取，但当前矩阵需继续核实 reader 是否避免全量扫描和是否暴露 row group index | 待核实 | 核实 partition / part 文件 whole scope、分页和字段类型映射。 |
| ORC | 待补齐 | 部分完成：descriptor 有 raw 和 whole / single 声明 | 待补齐 | 待补齐：当前主要 raw 声明 | 部分完成：可被表格文件 detector 识别，但缺解析能力 | 待补齐 | 待补齐：可能只能下载 / raw | 待补齐 | 待补齐：尚无格式目录 | 待补齐：缺 table reader / row group 或 stripe 级局部读取能力 | 待核实 | 决定是否引入 ORC table info / sample reader，或明确第一阶段仅 raw。 |
| Avro | 待补齐 | 部分完成：descriptor 有 raw 和 whole / single 声明 | 待补齐 | 待补齐：当前主要 raw 声明 | 部分完成：可被表格文件 detector 识别，但缺解析能力 | 待补齐 | 待补齐：可能只能下载 / raw | 待补齐 | 待补齐：尚无格式目录 | 待补齐：缺 block 级样本读取和大文件分页策略 | 待核实 | 决定是否引入 Avro table info / sample reader，或明确第一阶段仅 raw。 |
| Shapefile | 已完成 | 已完成 | 已完成：component table / spatial info | 已完成：component table sample、raw 声明 | 部分完成：仍需要 Meta multi detector 归并组件，这是合理特例 | 部分完成：Manager 有专用 shapefile content handler 和表格预览路径 | 已完成：shapefile / table 预览 | 已完成：代码在 shapefile 目录内较集中 | 部分完成：文件较完整，但 `provider.go` 作为 FormatPlugin adapter，`parser.go` 仍承载较多职责，可继续精简命名边界 | 部分完成：支持 offset / limit 采样，但组件物化和 DBF / SHP 顺序访问在大文件下性能需核实 | 待核实 | 核实组件缺失、编码、空间范围、预览行数和前端地图体验。 |
| Excel | 已完成 | 已完成 | 部分完成：container children 由 Meta 调用 excel analyzer 写入 | 已完成：table sample、raw 声明 | 部分完成：容器 children 仍在 Meta 中有格式分支 | 部分完成：Manager 有专用 Excel handler | 已完成：excel 预览 | 部分完成：analyzer 在 common/format，children 映射在 Meta | 部分完成：`parser.go` 作为插件入口，`analyzer_internal.go` 命名清晰；后续可补 `plugin.go` 主入口 | 部分完成：有 sheet / row / column limit，但 XLSX ZIP 结构通常仍需读取包内容；大文件首屏和多 sheet 体验待核实 | 待核实 | 抽取容器 info provider 边界，核实多 sheet、空 sheet、大文件体验。 |
| SQLite | 已完成 | 已完成 | 部分完成：container children 由 Meta 调用 sqlite analyzer 写入 | 已完成：table sample、raw 声明 | 部分完成：容器 children 仍在 Meta 中有格式分支 | 部分完成：Manager 有专用 SQLite handler | 已完成：sqlite 预览 | 部分完成：analyzer 在 common/format，children 映射在 Meta | 部分完成：`parser.go` 作为插件入口，`analyzer_internal.go` 命名清晰；后续可补 `plugin.go` 主入口 | 部分完成：SQLite 可天然分页查询，但当前对象文件可能需要先物化本地临时文件；大库体验待核实 | 待核实 | 抽取容器 info provider 边界，核实表选择、分页、只读打开错误。 |
| GeoPackage | 部分完成：复用 SQLite / GeoPackage 分支 | 部分完成：descriptor 和插件入口需核实是否独立于 SQLite | 部分完成：Meta children 和 spatial attrs 已有 | 部分完成：主要复用 SQLite / table 能力 | 部分完成：Meta 有 GeoPackage 特例 | 部分完成：Manager 是否按 GeoPackage 独立体验仍需核实 | 待核实 | 部分完成 | 待补齐：当前没有独立 GeoPackage 目录，代码分散在 SQLite / Meta 容器 children 分支 | 部分完成：复用 SQLite 分页潜力，但 layer 空间预览和大文件物化成本需核实 | 待核实 | 明确 GeoPackage 是否需要独立 plugin / descriptor / handler，核实 layer 体验。 |
| Text | 已完成 | 已完成 | 已完成：document info | 已完成：document text、raw 声明 | 部分完成：single 可走 capability，legacy extractor 仍并存 | 部分完成：Manager 有 text handler | 已完成：text 预览 | 已完成 | 部分完成：`provider.go` 同时作为 plugin / info / text reader，体量可控；后续可按需拆 `plugin.go` | 部分完成：有文本读取 limit / 截断概念，但 range preview、编码探测和超大文件体验待核实 | 待核实 | 核实编码识别、大文件截断、全文提取边界。 |
| Markdown | 已完成 | 已完成 | 已完成：document info | 已完成：document text、raw 声明 | 部分完成：single 可走 capability | 部分完成：Manager 有 markdown handler | 已完成：markdown 预览 | 已完成 | 部分完成：与 text 共用目录和 provider，后续可按 text profile 口径整理 | 部分完成：同 text，需核实大文档截断、前端渲染性能和安全处理 | 待核实 | 核实 Markdown 渲染安全、链接、代码块和大文件截断。 |
| Image 通用 | 部分完成：仍以 legacy extractor 为主 | 已完成：通用 image descriptor | 部分完成：media info / legacy extractor 并存 | 已完成：raw / range 声明 | 部分完成：legacy extractor 仍参与 attributes 写入 | 部分完成：Manager 有 image handler | 已完成：image 预览 | 部分完成：image parser 在目录内，但仍是 legacy extractor 注册 | 部分完成：`parser.go` 是 legacy extractor 入口，`geotiff.go` 已拆；后续应新增 `plugin.go` / `provider.go` 收敛 | 待补齐：缺缩略图 / tile / 分辨率降采样等高性能预览策略，超大图不应直接全量 base64 | 待核实 | 收敛到 MediaInfoProvider，核实缩略图、EXIF / GeoTIFF、超大图体验。 |
| JPEG / PNG / GIF / TIFF | 部分完成：复用 image parser | 已完成：各格式 descriptor | 部分完成：media info / legacy extractor 并存，TIFF 有 GeoTIFF 处理 | 已完成：raw / range 声明 | 部分完成 | 部分完成：Manager image handler 有扩展名清单 | 已完成：image 预览 | 部分完成 | 部分完成：作为 image profiles 处理，文件命名应随 image 通用入口收敛 | 待补齐：大图、GeoTIFF、多页 TIFF 需要缩略图或切片预览策略 | 待核实 | 按子格式核实 MIME、EXIF、GeoTIFF 空间能力和前端显示。 |
| PDF | 部分完成：仍以 legacy extractor 为主 | 已完成 | 部分完成：legacy metadata extractor，DocumentInfoProvider 需收敛 | 部分完成：raw / range 声明，后端文本 reader 未稳定 | 部分完成：legacy extraction 写入 type_info / capabilities | 部分完成：Manager 有 PDF handler | 已完成：pdf 预览 | 部分完成 | 待补齐：`parser.go` 是 legacy extractor 入口，后续应补 `plugin.go` / `provider.go` 并标注旧适配 | 部分完成：前端可按 PDF 原生能力加载，但后端 base64 / raw 返回大文件成本需核实，优先 range / URL 方式 | 待核实 | 区分 raw 前端预览、后端文本提取和全文索引目标。 |
| DOCX | 待补齐 | 已完成：raw / range descriptor | 待补齐 | 部分完成：raw / range 声明 | 部分完成：single 可识别，深度解析缺口存在 | 部分完成：Manager 有 DOCX handler | 已完成：docx 预览 | 待补齐 | 待补齐：尚无格式目录，只有 descriptor / Manager 展示链路 | 部分完成：前端 raw 预览可用，但大文件转换 / base64 / 下载式预览成本需核实 | 待核实 | 决定第一阶段只做 raw 前端预览，还是补 DocumentInfoProvider / text reader。 |
| PPTX | 待补齐 | 已完成：raw / range descriptor | 待补齐 | 部分完成：raw / range 声明 | 部分完成 | 部分完成：Manager 有 PPTX handler | 已完成：pptx 预览 | 待补齐 | 待补齐：尚无格式目录，只有 descriptor / Manager 展示链路 | 部分完成：前端 raw 预览可用，但大文件转换 / base64 / 下载式预览成本需核实 | 待核实 | 同 DOCX，明确后端解析边界。 |
| WPS | 待补齐 | 已完成：raw / range descriptor | 待补齐 | 部分完成：raw / range 声明 | 部分完成 | 部分完成：Manager 有 WPS handler | 已完成：wps 预览 | 待补齐 | 待补齐：尚无格式目录，只有 descriptor / Manager 展示链路 | 部分完成：前端 raw 预览可用，但大文件兼容性和传输成本需核实 | 待核实 | 明确 WPS 仅 raw 前端预览还是补后端解析能力。 |

## 跨格式待办

1. 为每个格式补一组最小端到端样例：扫描、attributes、Manager 预览、异常提示。
2. 用 `ListFormatCapabilityViews()` 生成或校验矩阵中的 descriptor / 实现状态，减少人工维护偏差。
3. 将 Manager 后端匹配逻辑逐步从内置格式清单迁移到 descriptor、data item attributes 和 provider / reader 能力。
4. 将容器 children、legacy extractor、特殊空间识别中可以通用化的部分逐步上移到 `common/format` provider / reader。
5. 对仅 raw / range 的文档和媒体格式明确使用体验目标，避免误写成“后端可解析”。
6. 每完成一个格式，补对应测试和真实样例核实记录，再更新本矩阵。

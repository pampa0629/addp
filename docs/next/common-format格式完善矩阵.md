# common/format 格式完善矩阵

更新时间：2026-05-13

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
| 分支收敛 | 每完善一个格式，必须同步清理 Meta / Manager / Transfer 中对应的单引擎、单格式分支判断；新增能力只能进入 descriptor、provider、detector registry 或统一 adapter，不再把 `engine_type`、format 后缀或 format ID 判断散落到调用方。 |
| 前端渲染 | Manager 前端有对应预览插件或通用渲染兜底，且渲染器不反向约束 `common/format`。 |
| 代码归拢 | 格式解析、provider、reader、writer、测试尽量归拢到 `common/format/plugins/<format>/`，避免分散到 Meta / Manager / Transfer 重复实现。 |
| 目录文件整理 | 格式目录内文件名按职责简洁统一，例如 `plugin.go` 作为主入口，`provider.go` / `reader.go` / `parser.go` / `writer.go` / `analyzer_internal.go` 按用途拆分；为旧代码做适配的 wrapper / adapter 要明确标注并逐步收敛。 |
| 大数据量高性能预览 | Manager 预览大文件或大对象时应支持分页、抽样、range read、content_index、row group / block / page 等局部读取能力，避免为了首屏预览全量读取或全量解析。 |
| 使用体验核实 | 用真实或代表性样例完成扫描、预览、分页、异常提示、空间 / 容器信息展示等体验核实。 |

## 格式矩阵

| 格式 | 抽象入口 | descriptor / 注册 | info provider | content reader | Meta 硬编码消除 | Manager 硬编码消除 | 前端渲染 | 代码归拢 | 目录文件整理 | 大数据量高性能预览 | 使用体验核实 | 下一步 |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 分隔文本表格（CSV / TSV） | 部分完成：当前一套代码注册 `csv` / `tsv` 两个 format ID，目标口径是同一格式族按 delimiter profile 区分 | 部分完成：当前 descriptor 分列 CSV / TSV；目标是统一分隔文本表格能力，扩展名 / MIME / delimiter 作为识别 profile | 已完成：table / format info | 已完成：table sample、raw 声明、CSV / TSV 稀疏行索引策略 | 部分完成：single 可走 capability；`content_index` 已改为 deep-only 且由格式声明能力驱动，表格文件 detector 仍参与 schema 写入 | 部分完成：文件表预览走 provider，缺失 `content_index` 时可按格式能力调用 Meta 按需建立，但仍有 preview DTO / handler 体系 | 已完成：table 预览 | 已完成：实现已在 `plugins/csv/` 归拢 | 部分完成：`plugin.go` 同时承担主入口、解析和 reader 逻辑，后续可拆出 reader / analyzer 文件 | 部分完成：已有 `content_index.table` 稀疏行索引和 range preview，basic 扫描不再建索引；失效规则和极大文件体验待核实 | 待核实 | 收敛 CSV / TSV 的文档和能力视图口径，保留不同 profile；补 content_index 失效规则，核实大文件分页、编码、表头识别。 |
| JSON（含 GeoJSON 空间结构） | 已完成：同一 `FormatJSON` 插件处理 JSON / GeoJSON | 已完成：同一 descriptor 声明 `.json`、`.geojson`、`application/geo+json`，并声明 `content_index` | 部分完成：默认 data type 仍为 document；内容解析为顶层对象数组、CSV 转出的对象数组、FeatureCollection features 等记录集合时可提供 table info；只有实际发现 GeoJSON geometry 或 WKB / EWKB 几何字段值才提供 `SpatialInfo` | 已完成：table sample、raw 声明、JSON 稀疏行 content index；无 geometry 的记录集合不会虚构 geometry 字段，WKB / EWKB 会解码为 GeoJSON-like geometry 供前端渲染 | 部分完成：`capabilities.spatial` 仅由 `SpatialInfo` 等内容事实写入；NFS / ObjectStorage 均复用通用 table file info 映射，避免按引擎分叉 | 部分完成：文件表预览能从 Meta attributes 还原 table / spatial info，并可用 `content_index.table` 做 range preview；Manager 仍保留 JSON / GeoJSON 展示 handler，GeoJSON handler 只按显式线索匹配，解析失败回退 JSON | 已完成：json / geojson 预览；空间展示属于前端表格 + 空间渲染方式 | 已完成：格式解析在 JSON 目录内，展示代码在 Manager | 部分完成：`parser.go` 同时承载 plugin、iterator、WKB 解码、bbox 推导和 table reader；serializer 单独存在，后续可按 plugin / reader / iterator / geometry helper 整理 | 部分完成：JSON 记录集合可流式样本，并已提供 `content_index.table` 稀疏行索引和 positioned reader；content index 失效刷新由 Meta 自身流程负责，超大文件端到端体验待核实 | 部分完成：已用 CSV 转 JSON 对象数组、真实 GeoJSON、WKB / EWKB 字段、NFS 与 MinIO 路径做过针对性验证；仍需补系统化样例集 | 补齐真实样例回归集，继续核实大文件分页、复杂嵌套对象数组、GeoJSON 无 geometry / 混合 geometry、WKB / EWKB 空间渲染体验。 |
| Parquet | 已完成 | 已完成 | 已完成：single table info；whole scope 可递归合并多个 `.parquet` 的 schema、总行数和 part 行数，schema 不兼容时报错 | 已完成：single table sample、whole scope 跨文件分页 sample、raw 声明 | 部分完成：single / whole detector 已有，`part-*` 文件和分区目录可识别为 whole scope；Meta whole scope 已复用 `ScopeTableProvider` 写入总行数和 `format_info.parquet.files`；同目录无 part 语义的多个 parquet 仍保持独立 item | 部分完成：文件表预览和 scope table 预览均走 provider；scope table 可从 attributes 复用 table schema / 总行数，并把 Parquet part 行数传回格式插件以加速深分页 | 已完成：table 预览 | 已完成 | 已完成：主入口已收敛到 `plugin.go`，`provider.go` 保留 scope 资源枚举，`table_file.go` 保留表格文件族判断 | 部分完成：whole scope 跨文件深分页可用 part 行数跳过前置文件，单文件 row group 内 offset 使用 parquet seek；单文件仍基于 `io.Reader` 全量读入，Parquet footer / range-aware reader 需要另行设计，不复用 CSV / JSON content_index | 部分完成：已用 NFS whole scope、深分页和 part 行数 hints 做过验证；MinIO whole scope、分区目录和超大单文件仍需补样例 | 补 MinIO / NFS 真实样例回归，核实 `part-*`、分区目录、schema 不兼容提示、大文件 row group 性能；后续设计 Parquet 专用 range / footer 读取边界。 |
| Shapefile | 已完成：`.shp` 是 main component，`.shx` / `.dbf` 是 required component 但不是主文件 | 已完成：component specs 由 Shapefile format plugin 声明，`.prj` / `.cpg` / `.sbn` / `.sbx` 为可选组件 | 已完成：component table / spatial info | 已完成：component table sample、raw 声明；分页样本在 format 内部使用 `.shx` 作为 Shapefile 原生索引，并通过通用 component range reader 局部读取 `.shx` / `.dbf` / `.shp` | 部分完成：Meta 仍需要 multi detector 归并组件，这是合理特例；Shapefile 组件扩展名 / required 信息已从 format ComponentSpecs 派生；Shapefile 信息提取已改为调用 common/format ComponentTableProvider；Meta 不理解 `.shx` 分页索引语义 | 部分完成：Manager 表格预览已优先消费 attributes 中的 table / spatial info，组件内容读取优先消费 `component_files`；Manager 只提供通用 resource range reader，不理解 `.shx` / `.dbf` 内部语义；旧 Shapefile object content handler 已移除 | 已完成：table / spatial 预览 | 已完成：代码在 shapefile 目录内较集中 | 部分完成：`plugin.go` 为 FormatPlugin adapter，`parser.go` 仍承载组件物化、info、sample、WKT 等职责；DescribeTableComponents 已用 SHP / DBF header 快速返回 row_count、extent、schema；单流 table provider 入口已明确拒绝并要求 component input；indexed sample 已收敛到 `indexed_sample.go`；Polygon GeoJSON、WKT 与 DBF 编码正确性已补测试 | 部分完成：Describe 已避免全量遍历；Sample 对支持 range read 的 engine 使用 `.shx` 页索引做局部读取，NFS / S3 / MinIO 通过同一 `RangeReadableProvider` 生效；无 range 能力或不支持的 shape 类型才回退物化组件，必备组件读取失败不静默回退 | 部分完成：已覆盖 component_files 读取、缺组件提示、`.cpg` GBK / GB18030 / UTF-8 BOM 编码、单流入口拒绝、WKT、Polygon hole / multipolygon、坐标转换、`.shx` indexed preview 和 object / filesystem range 透传单测；真实 MinIO 样例已核实性能 OK，NFS 真实端到端仍待核实 | 继续核实 NFS 真实样例、不支持 shape 类型的提示和前端空间表渲染体验；如需地图专用展示，应基于通用 table / spatial 预览 DTO 扩展前端渲染，不再恢复 object content 全量下载 handler。 |
| Excel | 已完成 | 已完成 | 部分完成：已新增 `ContainerInfoProvider` 并由 Excel plugin 输出 workbook / sheet 轻量 children；指定 sheet 时 `TableInfoProvider` 可按 `ParseOptions.SheetName` 只分析目标 sheet | 已完成：table sample、raw 声明；sheet child 预览通过 `TableSampleReader` + `SheetName` 读取 | 部分完成：Excel children 已改为 provider 通用入口，Meta 仍保留 container provider 调度和 GeoPackage 分支 | 部分完成：Manager workbook 概览已统一输出 `kind=container`、`children`、`default_child`、`active_child` 的 container DTO；父容器不携带 child fields / rows，传入 `child_name` 时按 container child 通用规则路由到 `builtin:file-table` 读取目标 sheet | 部分完成：已抽出通用 `ContainerPreview` 前端壳和 `container-preview` 插件；Excel 只做 workbook / sheet 适配，前端事件已收敛为 `child-change`，旧 tabs / 示例限制文案已清理，真实多 sheet / 空 sheet 体验待核实 | 部分完成：analyzer、container provider、sheet table reader 在 common/format，attributes 映射在 Meta，Manager 只做 DTO 组装和 provider 路由 | 部分完成：`plugin.go` 作为插件入口，`analyzer_internal.go` 命名清晰 | 部分完成：sheet child 可分页读取；但 XLSX ZIP 结构通常仍需读取包内容，大文件首屏和多 sheet 切换性能待核实 | 待核实 | 用真实多 sheet、空 sheet、大文件样例核实：Meta children、container 概览、`child_name` 表格分页和 `ContainerPreview` 切换体验；继续把 GeoPackage 纳入同一 container DTO。 |
| SQLite | 已完成 | 已完成 | 部分完成：SQLite plugin 已提供 `ContainerInfoProvider`，Meta 通过 provider 通用入口写入轻量 children；child 真实表名通过 `table` 字段传给 reader options；SQLite 文件自身不写父级 `type_info.table` | 已完成：table sample、raw 声明；表 child 预览通过 `TableSampleReader` + `ExtraParams.table` 读取 | 部分完成：SQLite container children 已走 provider 通用入口；GeoPackage 仍有 layer / spatial 特例 | 部分完成：Manager 概览已统一输出 container DTO；SQLite handler 只保留数据库分析和临时文件物化职责；父容器不携带 child fields / rows；传入 `child_name` 时按 container child 通用规则路由到 `builtin:file-table`，并映射真实 table 名 | 部分完成：SQLite 运行时插件和通用 `container-preview` 插件都走 `ContainerPreview`；表切换复用 `child-change` / child preview 链路，旧单表示例上限 / 截断提示已不再作为预览 UI 入口 | 部分完成：analyzer 和 provider 在 common/format，children 映射在 Meta，Manager 表 child 预览已复用通用 file-table provider | 部分完成：`plugin.go` 作为插件入口，`analyzer_internal.go` 命名清晰 | 部分完成：SQLite 可天然分页查询，但当前对象文件可能需要先物化本地临时文件；大库体验待核实 | 待核实 | 核实真实 SQLite 多表切换、分页、只读打开错误和大库物化成本；下一步判断 GeoPackage 是否有独立 provider / reader 后再接入同一 container preview。 |
| GeoPackage | 部分完成：复用 SQLite / GeoPackage 分支 | 部分完成：descriptor 和插件入口需核实是否独立于 SQLite | 部分完成：Meta children 和 spatial attrs 已有 | 部分完成：主要复用 SQLite / table 能力 | 部分完成：Meta 有 GeoPackage 特例 | 部分完成：Manager 是否按 GeoPackage 独立体验仍需核实 | 待核实 | 部分完成 | 待补齐：当前没有独立 GeoPackage 目录，代码分散在 SQLite / Meta 容器 children 分支 | 部分完成：复用 SQLite 分页潜力，但 layer 空间预览和大文件物化成本需核实 | 待核实 | 明确 GeoPackage 是否需要独立 plugin / descriptor / handler，核实 layer 体验。 |
| Text | 已完成 | 已完成 | 已完成：document info | 已完成：document text、raw 声明 | 部分完成：single 可走 capability，legacy extractor 仍并存 | 部分完成：Manager 有 text handler | 已完成：text 预览 | 已完成 | 部分完成：`provider.go` 同时作为 plugin / info / text reader，体量可控；后续可按需拆 `plugin.go` | 部分完成：有文本读取 limit / 截断概念，但 range preview、编码探测和超大文件体验待核实 | 待核实 | 核实编码识别、大文件截断、全文提取边界。 |
| Markdown | 已完成 | 已完成 | 已完成：document info | 已完成：document text、raw 声明 | 部分完成：single 可走 capability | 部分完成：Manager 有 markdown handler | 已完成：markdown 预览 | 已完成 | 部分完成：与 text 共用目录和 provider，后续可按 text profile 口径整理 | 部分完成：同 text，需核实大文档截断、前端渲染性能和安全处理 | 待核实 | 核实 Markdown 渲染安全、链接、代码块和大文件截断。 |
| Image 通用 | 部分完成：已有 `common/format/plugins/image/plugin.go` 作为 MediaInfoProvider 入口，但仍兼容注册 legacy extractor | 已完成：通用 image descriptor | 部分完成：MediaInfoProvider 可输出 kind、宽高、MIME、编码和 GeoTIFF spatial；EXIF、方向、多帧 / 多页信息仍不足，legacy extractor 并存 | 已完成：raw / range 声明；缺 MediaThumbnailReader | 部分完成：普通 single 可走 capability；legacy extractor 仍参与部分 attributes 写入 | 部分完成：Manager 有 image handler，但仍存在扩展名清单和全量 base64 路径 | 已完成：image 预览 | 部分完成：image provider、GeoTIFF 解析在目录内；Manager 展示逻辑仍在独立 handler | 部分完成：已有 `plugin.go` 和 `geotiff.go`；后续可拆 `provider.go` / `thumbnail.go` 并标注 legacy extractor 退出条件 | 待补齐：缺缩略图 / tile / 分辨率降采样等高性能预览策略，超大图不应直接全量 base64 | 待核实 | 补 EXIF / orientation / 多帧或多页元信息；新增 MediaThumbnailReader 或 raw / range URL 预览策略；收敛 Manager 扩展名清单。 |
| JPEG / PNG / GIF / TIFF | 部分完成：复用 image provider profiles | 已完成：各格式 descriptor | 部分完成：基础 media info 已有，TIFF 有 GeoTIFF 处理；JPEG EXIF、PNG metadata、GIF 帧数、TIFF 多页仍待补齐 | 已完成：raw / range 声明；缺缩略图 / 切片 reader | 部分完成 | 部分完成：Manager image handler 有扩展名清单 | 已完成：image 预览 | 部分完成 | 部分完成：作为 image profiles 处理，文件命名应随 image 通用入口收敛 | 待补齐：大图、GeoTIFF、多页 TIFF、动图需要缩略图、首帧、降采样或切片预览策略 | 待核实 | 按子格式核实 MIME、EXIF / orientation、GIF 帧数、GeoTIFF 空间能力、多页 TIFF 和前端显示。 |
| PDF | 部分完成：仍以 legacy extractor 为主 | 已完成 | 部分完成：legacy metadata extractor，DocumentInfoProvider 需收敛 | 部分完成：raw / range 声明，后端文本 reader 未稳定 | 部分完成：legacy extraction 写入 type_info / capabilities | 部分完成：Manager 有 PDF handler | 已完成：pdf 预览 | 部分完成 | 待补齐：`parser.go` 是 legacy extractor 入口，后续应补 `plugin.go` / `provider.go` 并标注旧适配 | 部分完成：前端可按 PDF 原生能力加载，但后端 base64 / raw 返回大文件成本需核实，优先 range / URL 方式 | 待核实 | 区分 raw 前端预览、后端文本提取和全文索引目标。 |
| DOCX | 待补齐 | 已完成：raw / range descriptor | 待补齐 | 部分完成：raw / range 声明 | 部分完成：single 可识别，深度解析缺口存在 | 部分完成：Manager 有 DOCX handler | 已完成：docx 预览 | 待补齐 | 待补齐：尚无格式目录，只有 descriptor / Manager 展示链路 | 部分完成：前端 raw 预览可用，但大文件转换 / base64 / 下载式预览成本需核实 | 待核实 | 决定第一阶段只做 raw 前端预览，还是补 DocumentInfoProvider / text reader。 |
| PPTX | 待补齐 | 已完成：raw / range descriptor | 待补齐 | 部分完成：raw / range 声明 | 部分完成 | 部分完成：Manager 有 PPTX handler | 已完成：pptx 预览 | 待补齐 | 待补齐：尚无格式目录，只有 descriptor / Manager 展示链路 | 部分完成：前端 raw 预览可用，但大文件转换 / base64 / 下载式预览成本需核实 | 待核实 | 同 DOCX，明确后端解析边界。 |
| WPS | 待补齐 | 已完成：raw / range descriptor | 待补齐 | 部分完成：raw / range 声明 | 部分完成 | 部分完成：Manager 有 WPS handler | 已完成：wps 预览 | 待补齐 | 待补齐：尚无格式目录，只有 descriptor / Manager 展示链路 | 部分完成：前端 raw 预览可用，但大文件兼容性和传输成本需核实 | 待核实 | 明确 WPS 仅 raw 前端预览还是补后端解析能力。 |

## 后续待研发格式

以下格式暂不放入当前主线完善矩阵，作为后续独立研发项处理。进入主线前，先明确第一阶段目标是“仅 raw / download / 前端兜底”还是“后端 table info + sample reader + 大文件分页”。

| 格式 | 当前状态 | 后续研发要点 |
|---|---|---|
| ORC | descriptor 已有 raw 和 single / whole 声明；可被表格文件规则识别，但尚无 `common/format/plugins/orc/` 实现，也没有 table info / sample reader | 评估 Go ORC 依赖；补 `FormatPlugin`、schema 读取、stripe 级样本读取、whole scope 合并、空间字段识别和 Manager 表格预览链路。 |
| Avro | descriptor 已有 raw 和 single / whole 声明；可被表格文件规则识别，但尚无 `common/format/plugins/avro/` 实现，也没有 table info / sample reader | 评估 Go Avro container file 依赖；补 `FormatPlugin`、schema 读取、block 级样本读取、whole scope 合并、逻辑类型映射和 Manager 表格预览链路。 |
| WebP / BMP / SVG / AVIF / HEIC | 目前未进入内置 descriptor 主线；部分格式可能被 image MIME 兜底或前端扩展名清单识别，但缺稳定 format identity、MediaInfoProvider 覆盖和端到端样例 | 先明确 `format` ID、扩展名、MIME、magic bytes、预览策略和后端解析边界；SVG 需额外定义安全渲染策略；AVIF / HEIC 需评估 Go 解码依赖和前端浏览器兼容；仅 raw / range 可用时不得标记为完整 media info。 |
| Video 通用 | common/format 只有 `FormatVideo` 粗粒度检测和 MIME 兜底；registry descriptor 尚未定义 video，Meta 可按 MIME 推断为 media，但缺 format capability 主线 | 第一阶段补 `video` 兜底 descriptor、`MediaInfoProvider` 接口适配或外部探测边界、raw / range / stream content reader 声明和 Manager 播放链路；不做后端转码，不把编码当 format。 |
| MP4 / MOV / MKV / AVI / WebM | 尚无具体 format ID / descriptor / plugin；Manager 有 `/video-stream` 路由，但未收敛到 common/format 能力 | 补具体容器格式 descriptor、扩展名、MIME、magic bytes；输出 `type_info.media.kind=video`、宽高、时长、视频编码、音频编码、帧率、码率、轨道数；前端通过 range / stream URL 播放，核实大文件首屏和拖动体验。 |
| Audio 通用 | common/format 只有 `FormatAudio` 粗粒度检测和 MIME 兜底；registry descriptor 尚未定义 audio，缺 provider 和 Manager 通用音频预览主线 | 第一阶段补 `audio` 兜底 descriptor、raw / range / stream content reader 声明和通用音频预览；语音转写、摘要和语义索引进入 extraction / semantic 能力，不作为格式识别前置条件。 |
| MP3 / WAV / FLAC / AAC / OGG | 尚无具体 format ID / descriptor / plugin；只能通过 MIME 或扩展名粗略推断为 audio | 补具体音频格式 descriptor、扩展名、MIME、magic bytes；输出 `type_info.media.kind=audio`、时长、编码、采样率、声道数、码率；核实 range 播放、封面图和 ID3 / Vorbis comment 等私有 metadata 边界。 |

## 跨格式待办

1. 为每个格式补一组最小端到端样例：扫描、attributes、Manager 预览、异常提示。
2. 用 `ListFormatCapabilityViews()` 生成或校验矩阵中的 descriptor / 实现状态，减少人工维护偏差。
3. 将 Manager 后端匹配逻辑逐步从内置格式清单迁移到 descriptor、data item attributes 和 provider / reader 能力。
4. 将容器 children、legacy extractor、特殊空间识别中可以通用化的部分逐步上移到 `common/format` provider / reader。
5. 对仅 raw / range 的文档和媒体格式明确使用体验目标，避免误写成“后端可解析”。
6. 每完成一个格式，补对应测试和真实样例核实记录，再更新本矩阵。
7. 每完成一个格式，同时检查并收掉 Meta / Manager / Transfer 中对应的 `engine_type`、format 后缀或 format ID 分支；确实不能收敛的分支必须在本矩阵对应格式行说明原因和退出条件。

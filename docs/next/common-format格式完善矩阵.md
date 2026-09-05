# common/format 格式后续事项

更新时间：2026-06-02

只保留未决事项。

说明：本文只讨论格式事实、扫描、预览和 reader / writer 边界；资源选择入口仍统一走 locator / ResourceTreePicker，不在 common/format 中另起一套选择语义。

## 未决事项

1. 需要用真实样例继续核实 CSV / TSV、JSON / GeoJSON、Parquet、Shapefile、Excel、SQLite、GeoPackage、ZIP、text / markdown、image、PDF、DOCX、PPTX、WPS 的扫描、attributes、Manager 预览、分页和异常提示。
2. `ListFormatCapabilitySnapshots()` 仍要定期校验 descriptor 静态事实与当前进程 plugin 接口实现状态。
3. Manager 后端插件配置已拆为 `preview.json` 和 `content.json`，只表达 Manager 预览行为策略；格式事实仍以 `common/format` descriptor 为准。`capabilities.spatial` 到 `SpatialInfo` 的恢复已上收到 `common/datatype`。后续还要继续核实容器 child 动态预览、嵌套容器解析和本次预览内格式探测的边界，不能把它们写成静态格式事实。
4. 容器 children 和特殊空间识别中可通用化的部分还要继续上移到 `common/format` provider / reader、`common/dataitem` 组织规则或 `common/datatype` 标准事实，不能恢复旧 extractor 旁路。
5. 仅 raw / range 的文档和媒体格式还要明确使用体验目标，避免写成“后端可解析”；raw / range 由 engine / contentio / URL / fetcher 提供真实内容流，不进入 `FormatDescriptor`，也不作为 format registry 中的 Go reader。
6. 每完成一个格式，都要同步收掉 Meta、Manager、Transfer 中对应的 `engine_type`、format 后缀或 format ID 分支。
7. unknown 格式已注册 `BinaryContentReader` 作为非文本 raw binary 兜底；`DetectFormat` 已在最后一步用 `LooksLikeTextContent` 将可读文本归入 `format=text`。后续需继续用真实样例核实 Meta / Manager 的文本识别链路，确保剩余 unknown 才进入 binary reader。

## 已有格式待办

| 格式 | 后续事项 |
|---|---|
| CSV / TSV | 还要核实大文件分页、编码、表头识别，继续收敛 CSV / TSV 的格式族口径，并补 access index 失效规则验证。 |
| JSON / GeoJSON 编码 | GeoJSON 已升格为独立 `format=geojson`，`.json` 文件只有内容前缀严格匹配 `FeatureCollection` 时才升格；代码回归已覆盖普通 `.json` 不升格、`.json` GeoJSON deep scan 写入 `format_info.geojson`、Manager `format=geojson` 使用 map 预览材料。后续还要用真实样例核实大文件分页、复杂嵌套对象数组、GeoJSON 无 geometry / 混合 geometry、WKB / EWKB 空间渲染体验。 |
| Parquet | MinIO / NFS 真实样例、`part-*` 和分区目录 whole scope Transfer 已验收；Hive-style 分区字段已能进入 schema 和 row。后续继续核实 schema 不兼容提示、大文件 row group 性能，并设计专用 range / footer 读取边界。 |
| Shapefile | 还要用真实 NFS / MinIO / ZIP 样例核实本地 materialized fallback 也能继续利用 `.shx` 索引分页，嵌套 ZIP 中的 Shapefile 子项能正确归并 `.shp/.shx/.dbf` refs，不支持 shape 类型提示和前端空间表渲染体验；如需地图专用展示，应基于通用 table / spatial 预览 DTO 扩展前端渲染。 |
| Excel | 还要用真实多 sheet、空 sheet、大文件样例核实 Meta children、container 概览、`child_name` 表格分页和 `ContainerPreview` 切换体验。 |
| SQLite | 还要核实真实 SQLite 多表切换、分页、只读打开错误、大库物化成本，以及容器 child 的字段来源不会误用父 item `type_info.table.fields`。 |
| GeoPackage | 还要核实 Meta children、container 概览、layer 切换、分页样本、geometry column / SRID / extent 展示，以及容器 child 的字段来源不会误用父 item `type_info.table.fields`。 |
| ZIP | 还要核实扫描、容器概览、entry 列表截断、CSV entry 分页、文本 entry、嵌套 ZIP 逐层展开和动态识别结果只服务本次预览、不写回 Meta，并设计大压缩包和远程 range-aware entry 读取。 |
| Text / Markdown | 还要核实编码识别、大文件截断、Markdown 渲染安全、链接、代码块和前端性能。 |
| Image / JPEG / PNG / GIF / TIFF | 还要补 EXIF / orientation / 多帧或多页元信息，设计 MediaThumbnailReader 或 raw / range URL 预览策略，并核实大图、GeoTIFF、多页 TIFF、动图体验。 |
| PDF | 还要核实真实 PDF metadata、加密提示、raw / range 预览和大文件传输；如需正文提取，再另行定义 `DocumentTextReader` / extraction 任务边界。 |
| DOC / DOCX / PPTX / WPS | DOC 已有稳定 descriptor，但暂不声明后端文本解析能力。DOCX 已有轻量 `DocumentInfoProvider`，读取 `docProps` 中的 title、language、pages、words；`DocumentTextReader` 从 `word/document.xml` 提取正文，并追加页眉、页脚、脚注、尾注和批注文本。PPTX 已有轻量 `DocumentInfoProvider`，读取 `docProps` 中的 title、language、slides、words；`DocumentTextReader` 从 `ppt/slides/slide*.xml` 按页提取正文，并追加备注页和批注文本。二者可进入 Meta deep scan 的 `type_info.document` 和全文索引链路。后续还要补真实样例、DOCX 修订语义/复杂版面关系、PPTX 母版/隐藏页策略和大文件上限策略。DOC / WPS 格式变体较多，原始文件预览通过 engine / contentio / URL 内容通道交给浏览器统一 Office renderer，deep scan 记录 unsupported。 |

当前 Meta object deep scan 已能对实现了 `DocumentTextReader` 的 document 格式抽取正文：attributes 只写 `capabilities.extraction` 状态和预览，完整正文仅作为本次扫描输入提交给 Manager 内容检索投影。没有 `DocumentTextReader` 的 document 格式仍计算二进制 `storage.content_hash`，并在 `capabilities.extraction` 中记录 `status=unsupported` / `reason=document_text_reader_unavailable`，不写入可搜索正文。

## 后续待研发格式

| 格式 | 当前状态 | 后续研发要点 |
|---|---|---|
| ORC | 已有 `common/format/plugins/orc/` descriptor 壳，表达稳定 format identity、默认 data type、single / whole layout 和识别事实；尚无 table info / sample / reader / writer 实现，因此当前进程不可执行表格读取。 | 评估 Go ORC 依赖，补 schema 读取、stripe 级样本读取、连续 reader / writer、whole scope 合并、空间字段识别和 Manager 表格预览链路；能力对外展示必须同时看 descriptor 静态事实和已加载 plugin 的接口实现状态。 |
| Avro | 已有 `common/format/plugins/avro/` descriptor 壳，表达稳定 format identity、默认 data type、single / whole layout 和识别事实；尚无 table info / sample / reader / writer 实现，因此当前进程不可执行表格读取。 | 评估 Go Avro container file 依赖，补 schema 读取、block 级样本读取、连续 reader / writer、whole scope 合并、逻辑类型映射和 Manager 表格预览链路；能力对外展示必须同时看 descriptor 静态事实和已加载 plugin 的接口实现状态。 |
| WebP / BMP / SVG / AVIF / HEIC | 已有稳定 format identity、扩展名、MIME、默认 data type 和 single layout；尚无 MediaInfoProvider 覆盖和端到端样例。 | 明确预览策略和后端解析边界；SVG 需定义安全渲染策略，AVIF / HEIC 需评估 Go 解码依赖和前端兼容；`raw_content` / `range_content` 属于 engine / contentio / URL 内容通道，不是 format descriptor 字段。 |
| Video 通用 / MP4 / MOV / MKV / AVI / WebM | 已有通用 video 和具体容器 format identity、扩展名、MIME、默认 data type 和 single layout；尚无 MediaInfoProvider 和媒体元信息主线。 | `type_info.media` 只输出 kind、宽高、时长、基础编码等通用字段；帧率、码率、轨道数等细粒度事实进入受控 `format_info.<format>` 或 `capabilities.extraction`，前端通过 range / stream URL 播放。 |
| Audio 通用 / MP3 / WAV / FLAC / AAC / OGG | 已有通用 audio 和具体音频 format identity、扩展名、MIME、默认 data type 和 single layout；Manager 已有通用音频 URL 预览 kind，尚无 MediaInfoProvider 和端到端真实样例验收。 | `type_info.media` 只输出 kind、时长、基础编码等通用字段；采样率、声道数、码率、ID3 / Vorbis comment 等细粒度事实进入受控 `format_info.<format>` 或 `capabilities.extraction`，并核实 range 播放和封面图边界。 |
| 三维模型扩展 / DAE / 3MX / SLPK | GLB、glTF、OBJ、STL、FBX、IFC 识别与轻量摘要、单 OSGB、OSGB Scene 和 3D Tiles 主链路已进入正式规范与模块文档；未落地格式不在本矩阵逐项展开。 | 统一见 `docs/next/三维与点云后续路线.md`。格式事实仍落到 `common/format` descriptor，快显优先转换为 GLB 或 3D Tiles；BIM、倾斜摄影和普通 mesh 不新增平行 data type。 |
| 点云扩展 / LAZ / COPC / E57 / EPT / Potree | LAS / LAZ / COPC / E57 / PCD / XYZ 格式身份与轻量摘要已进入正式规范；LAS / LAZ / E57 / PCD / XYZ 已进入 Manager 私有 COPC 快显主线，源 COPC 直接通过受控 URL 和 Range 动态 LOD 预览。 | 统一见 `docs/next/三维与点云后续路线.md`。EPT / Potree 等 whole-scope 点云数据集仍需单独评估；点云格式归入 `point_cloud`，不混入 `model_3d`，不走 GLB。 |

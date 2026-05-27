# common/format 格式后续事项

更新时间：2026-05-21

只保留未决事项。

## 未决事项

1. 需要用真实样例继续核实 CSV / TSV、JSON / GeoJSON、Parquet、Shapefile、Excel、SQLite、GeoPackage、ZIP、text / markdown、image、PDF、DOCX、PPTX、WPS 的扫描、attributes、Manager 预览、分页和异常提示。
2. `ListFormatCapabilityViews()` 仍要定期校验 descriptor 声明能力与当前进程实现状态。
3. Manager 后端匹配逻辑还要继续从独立扩展名清单收口到 descriptor、data item attributes、contentio 抽象和 provider / reader 能力。
4. 容器 children、特殊空间识别、component / scope table 读取中可通用化的部分还要继续上移到 `common/format` provider / reader 或 `common/dataitem` 组织规则，不能恢复旧 extractor 旁路。
5. 仅 raw / range 的文档和媒体格式还要明确使用体验目标，避免写成“后端可解析”；raw / range 目前只作为内容读取方式声明，由 engine / contentio / URL / fetcher 提供真实内容流，不作为 ProviderRegistry 中的 Go reader。
6. 每完成一个格式，都要同步收掉 Meta、Manager、Transfer 中对应的 `engine_type`、format 后缀或 format ID 分支。
7. unknown 格式已注册 `BinaryContentReader` 作为非文本 raw binary 兜底；`DetectFormat` 已在最后一步用 `LooksLikeTextContent` 将可读文本归入 `format=text`。后续需继续用真实样例核实 Meta / Manager 的文本识别链路，确保剩余 unknown 才进入 binary reader。

## 已有格式待办

| 格式 | 后续事项 |
|---|---|
| CSV / TSV | 还要核实大文件分页、编码、表头识别，继续收敛 CSV / TSV 的格式族口径，并补 content index 失效规则验证。 |
| JSON / GeoJSON | 还要补真实样例回归，核实大文件分页、复杂嵌套对象数组、GeoJSON 无 geometry / 混合 geometry、WKB / EWKB 空间渲染体验。 |
| Parquet | MinIO / NFS 真实样例、`part-*` 和分区目录 whole scope Transfer 已验收；Hive-style 分区字段已能进入 schema 和 row。后续继续核实 schema 不兼容提示、大文件 row group 性能，并设计专用 range / footer 读取边界。 |
| Shapefile | 还要用真实 NFS / MinIO / ZIP 样例核实本地 materialized fallback 也能继续利用 `.shx` 索引分页，嵌套 ZIP 中的 Shapefile 子项能正确归并 `.shp/.shx/.dbf` refs，不支持 shape 类型提示和前端空间表渲染体验；如需地图专用展示，应基于通用 table / spatial 预览 DTO 扩展前端渲染。 |
| Excel | 还要用真实多 sheet、空 sheet、大文件样例核实 Meta children、container 概览、`child_name` 表格分页和 `ContainerPreview` 切换体验。 |
| SQLite | 还要核实真实 SQLite 多表切换、分页、只读打开错误、大库物化成本，以及容器 child 的字段来源不会误用父 item `type_info.table.fields`。 |
| GeoPackage | 还要核实 Meta children、container 概览、layer 切换、分页样本、geometry column / SRID / extent 展示，以及容器 child 的字段来源不会误用父 item `type_info.table.fields`。 |
| ZIP | 还要核实扫描、容器概览、entry 列表截断、CSV entry 分页、文本 entry、嵌套 ZIP 逐层展开和动态识别结果只服务本次预览、不写回 Meta，并设计大压缩包和远程 range-aware entry 读取。 |
| Text / Markdown | 还要核实编码识别、大文件截断、Markdown 渲染安全、链接、代码块和前端性能。 |
| Image / JPEG / PNG / GIF / TIFF | 还要补 EXIF / orientation / 多帧或多页元信息，设计 MediaThumbnailReader 或 raw / range URL 预览策略，并核实大图、GeoTIFF、多页 TIFF、动图体验。 |
| PDF | 还要核实真实 PDF metadata、加密提示、raw / range 预览和大文件传输；如需正文提取，再另行定义 `DocumentTextReader` / extraction 任务边界。 |
| DOCX / PPTX / WPS | DOCX 已有 `DocumentTextReader`，从 `word/document.xml` 提取正文，并追加页眉、页脚、脚注、尾注和批注文本；PPTX 已有基础 `DocumentTextReader`，从 `ppt/slides/slide*.xml` 按页提取正文，并追加备注页和批注文本；二者可进入 Meta deep scan 全文链路。后续还要补真实样例、DOCX 修订语义/复杂版面关系、PPTX 母版/隐藏页策略、轻量 `DocumentInfoProvider` 和大文件上限策略。WPS 格式变体较多，优先保留 raw / range，未实现可靠 reader 时记录 unsupported。 |

当前 Meta object deep scan 已能对实现了 `DocumentTextReader` 的 document 格式抽取正文：attributes 只写 `capabilities.extraction` 状态、预览和 Meilisearch `index_ref`，完整正文仅作为本次扫描输入写入 Meilisearch。没有 `DocumentTextReader` 的 document 格式仍计算二进制 `storage.content_hash`，并在 `capabilities.extraction` 中记录 `status=unsupported` / `reason=document_text_reader_unavailable`，不写入可搜索正文。

## 后续待研发格式

| 格式 | 当前状态 | 后续研发要点 |
|---|---|---|
| ORC | descriptor 已有 raw 和 single / whole 声明；可被表格文件规则识别，但尚无 `common/format/plugins/orc/` 实现，也没有 table info / sample reader。 | 评估 Go ORC 依赖，补 `FormatPlugin`、schema 读取、stripe 级样本读取、whole scope 合并、空间字段识别和 Manager 表格预览链路。 |
| Avro | descriptor 已有 raw 和 single / whole 声明；可被表格文件规则识别，但尚无 `common/format/plugins/avro/` 实现，也没有 table info / sample reader。 | 评估 Go Avro container file 依赖，补 `FormatPlugin`、schema 读取、block 级样本读取、whole scope 合并、逻辑类型映射和 Manager 表格预览链路。 |
| WebP / BMP / SVG / AVIF / HEIC | 部分格式可能被 image MIME 兜底或前端扩展名清单识别，但缺稳定 format identity、MediaInfoProvider 覆盖和端到端样例。 | 明确 format ID、扩展名、MIME、magic bytes、预览策略和后端解析边界；SVG 需定义安全渲染策略，AVIF / HEIC 需评估 Go 解码依赖和前端兼容。 |
| Video 通用 / MP4 / MOV / MKV / AVI / WebM | common/format 只有粗粒度检测和 MIME 兜底；具体 format ID、descriptor、plugin 和媒体元信息主线仍缺。 | 补 video 兜底 descriptor 和具体容器格式 descriptor，输出 `type_info.media.kind=video`、宽高、时长、编码、帧率、码率、轨道数，前端通过 range / stream URL 播放。 |
| Audio 通用 / MP3 / WAV / FLAC / AAC / OGG | common/format 只有粗粒度检测和 MIME 兜底；缺 provider 和 Manager 通用音频预览主线。 | 补 audio 兜底 descriptor 和具体音频格式 descriptor，输出 `type_info.media.kind=audio`、时长、编码、采样率、声道数、码率，核实 range 播放、封面图和 ID3 / Vorbis comment 边界。 |

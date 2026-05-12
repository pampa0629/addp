# ADDP 格式能力与 Manager 内容读取推进

更新时间：2026-05-12

本文记录 Manager 内容读取与 `common/format` 能力收口的当前结论和后续事项。正式概念和规范以以下文档为准：

- [ADDP 数据项体系图](../concepts/addp数据项体系图.md)
- [ADDP 数据类型和格式体系图](../concepts/addp数据类型和格式体系图.md)
- [ADDP 数据类型与格式能力规范](../spec/addp数据类型与格式能力规范.md)
- [ADDP 数据类型与文件格式扩展指南](../spec/addp数据类型与文件格式扩展指南.md)
- [ADDP 资源读取抽象规范](../spec/addp资源读取抽象规范.md)

## 已确认边界

1. `common/format` 不定义 preview 概念，不返回 Manager 面向前端的 DTO，不推荐前端渲染器。
2. `common/format` 只声明和实现 FormatPlugin、format capability、info provider、content reader。
3. Manager 只消费已入库 data item、标准 attributes、resource 抽象和 provider / reader 结果。
4. Manager 可以组装前端 DTO，但不得重新判断 organization、重新猜 format、重新枚举 sibling 组件。
5. WPS / DOCX / PPTX 等文档格式仍是 `data_type=document`，即使当前后端只提供 raw / range content reader。
6. 内容样本、文本片段、缩略图、raw content、range content 属于 content reader，不属于 `xxx info`。

## 当前推进重点

| 事项 | 当前方向 |
|---|---|
| CSV / TSV / JSON / Parquet / Shapefile 表格内容 | Manager 通过 ResourceReader / ComponentReader 调用 `TableInfoProvider` / `TableSampleReader` |
| 文档内容 | 能后端提取文本时使用 `DocumentTextReader`；不能后端解析时走 raw / range content reader，由 Manager 和 Frontend 处理展示 |
| 媒体内容 | 元信息走 `MediaInfoProvider`；原始内容、缩略图、range content 作为 content reader 能力逐步补齐 |
| 容器内容 | 外层 item 先写 `type_info.container`；内部对象读取后续通过 `ContainerInfoProvider` / `ContainerEntryReader` 稳定 |
| 第三方格式扩展 | 先通过 FormatDescriptor / FormatPlugin 进入 common/format；Manager 不维护独立扩展名清单 |

## 待清理问题

1. Manager 仍存在部分对象内容 handler 与前端插件清单，需要逐步改为消费 format descriptor 和 provider / reader 结果。
2. 旧 `ObjectContentRegistry` 可以作为 Manager 内部 DTO 组装层保留，但不应反向定义 common 能力。
3. text / markdown / image 已有最小能力，PDF、Office、WPS 等文档格式仍需按 `DocumentInfoProvider` / `DocumentTextReader` 或 raw / range content reader 逐步补齐。
4. 旧 `FileMetadataExtractor` 仍是兼容入口，应继续向 info provider / content reader 迁移。
5. Manager 应继续收口到资源读取抽象和格式能力发现结果，避免维护独立格式清单。

## 新增格式时的要求

新增格式不要从 Manager handler 开始做。正确顺序是：

1. 判断 data type 和 organization。
2. 在 `common/format/plugins/<format>/` 实现 FormatPlugin。
3. 在 descriptor 中声明 identification、data type、layouts、providers、content readers。
4. 按需实现 info provider 和 content reader。
5. 确认 Meta 是否已有通用消费链路；只有现有 detector / normalizer 无法表达时才补 Meta。
6. Manager 只基于已入库 item 和 reader 结果组装前端 DTO。

详细清单见 [ADDP 数据类型与文件格式扩展指南](../spec/addp数据类型与文件格式扩展指南.md)。

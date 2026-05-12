# ADDP 数据类型与文件格式跟进清单

更新时间：2026-05-12

本文记录数据类型与文件格式方向的当前状态和非 Transfer 后续工作。Transfer 相关事项暂不在本文维护。

具体格式的逐项完善情况维护在 [common/format 格式完善矩阵](common-format格式完善矩阵.md)。该矩阵按“格式 × 完善标准”跟踪抽象入口、注册、provider、reader、Meta / Manager 硬编码消除、前端渲染、代码归拢和使用体验核实。

## 当前正式文档

| 文档 | 作用 |
|---|---|
| [ADDP 术语表](../concepts/addp术语表.md) | 统一 data item、data type、format、detector 等术语 |
| [ADDP 数据项体系图](../concepts/addp数据项体系图.md) | 说明 engine -> node -> data item 链条和模块职责 |
| [ADDP 数据类型和格式体系图](../concepts/addp数据类型和格式体系图.md) | 说明数据类型、文件格式、横切能力和 provider / reader 矩阵 |
| [ADDP 元数据体系图](../concepts/addp元数据体系图.md) | 说明 Meta 如何识别 data item 并生成 attributes |
| [ADDP 数据类型与格式能力规范](../spec/addp数据类型与格式能力规范.md) | 定义 FormatPlugin、FormatDescriptor、info provider、content reader 和注册方式 |
| [ADDP 数据类型与文件格式扩展指南](../spec/addp数据类型与文件格式扩展指南.md) | 新增格式的实施清单 |
| [ADDP 数据项探测器规范](../spec/addp数据项探测器规范.md) | data item 识别、主资源、组件、claims / exclusive 规则 |
| [ADDP 元数据 attributes 规范](../spec/addp元数据attributes规范.md) | attributes 分区和字段归属 |
| [ADDP 内置数据类型与文件格式规范](../spec/addp内置数据类型与文件格式规范.md) | 首批内置格式落地规则 |
| [ADDP 资源读取抽象规范](../spec/addp资源读取抽象规范.md) | ResourceReader、ComponentReader、NativeCursor 边界 |

## 已确认架构结论

1. 数据管理主链路是 `engine -> node -> data item`。
2. `data item` 等同概念层数据项；落库实体是 `meta_item`。
3. `data type` 高于 `format`；新增格式不等于新增数据类型。
4. `spatial`、`temporal`、`statistics`、`extraction`、`semantic`、`partitioning`、`indexing` 是横切能力，不是基础数据类型。
5. `common/format` 保留在 common 层，负责 FormatPlugin、format identity、descriptor、capability、info provider、content reader。
6. `common/format` 不定义 preview 概念，不返回 Manager 面向前端的 DTO。
7. `provider` 用于 info；`reader` 用于 content。新实现不再把内容读取能力统称为 provider。
8. 新增普通 `single` 文件格式优先只改 `common/format`；只有突破现有 item 识别或 attributes 映射能力时才改 Meta。
9. Manager 只消费已入库 data item、标准 attributes、resource 抽象和 provider / reader 结果。
10. 旧 attributes、旧枚举、旧平铺字段不保留兼容；旧数据重新扫描生成新结构。

## 已完成整理

1. 概念文档已拆为术语表、数据项体系图、数据类型和格式体系图、元数据体系图。
2. 规范文档已改名并统一为“数据类型在格式前”的命名。
3. 独立的模块边界规范已删除，职责边界迁入数据项体系图。
4. 数据类型与格式能力规范已补充 FormatPlugin 抽象接口、descriptor 字段、注册方式和子目录封装要求。
5. 数据类型与文件格式扩展指南已改为实施清单，明确新增普通格式默认不改 Meta。
6. 元数据体系图已移除旧 Parser / Extractor / ExtensionInfo 主线，改为 Meta scanner / detector / normalizer 主线。
7. `common/format/README.md` 已按 FormatPlugin、info provider、content reader 和 resource 边界刷新。
8. 与旧命名相关的文档残留已清理：旧文件名、旧 DataTypeProvider 术语、旧 format provider / data type provider 口径不再出现在正式文档和 next 入口中。

## 当前代码侧状态摘要

1. `common/format/provider.go` 已有 `FormatPlugin`、`FormatInfoProvider`、`TableInfoProvider`、`TableSampleReader`、`DocumentInfoProvider`、`DocumentTextReader`、`MediaInfoProvider` 等接口和注册表。
2. `RegisterFormatPlugin` 会自动挂接 plugin 实现的 info provider 和 content reader。
3. `common/format/registry/descriptor.go` 已有内置 descriptor、provider hints、content readers 和冲突诊断。
4. 分隔文本表格（CSV / TSV）、JSON、Parquet 等已开始按 FormatPlugin 入口组织；CSV / TSV 后续按同一格式族的不同 delimiter profile 收敛口径。
5. Shapefile 已处于 `common/format/plugins/shapefile/` 子目录内，并已通过 `RegisterFormatPlugin` 注册 component table 能力；后续重点是端到端体验核实和 Manager 硬编码收敛。
6. `FileMetadataExtractor` 仍作为 legacy 兼容入口存在，不再作为新增格式主线。

## 非 Transfer 后续优先级

1. 按 [common/format 格式完善矩阵](common-format格式完善矩阵.md) 逐个格式推进，优先处理“待补齐”和“待核实”列。
2. 梳理 image / PDF / Office / WPS 的 descriptor、info provider、content reader 状态，区分“后端可解析”和“仅 raw / range content”。
3. 将 Manager 中仍独立维护的格式清单逐步改为消费 format descriptor、data item attributes 和 provider / reader 能力。
4. 校验 Meta 是否通过通用链路消费新增 `single` 格式，避免每个格式都补 Meta 特例。
5. 为分隔文本表格（CSV / TSV）、JSON、Parquet、Shapefile、text / markdown、image、PDF 做端到端重扫样例，并把核实结果回填到矩阵。
6. 继续收敛旧 `FileMetadataExtractor` 到 `DocumentInfoProvider`、`MediaInfoProvider` 或 content reader。
7. 为 `content_index.table` 的 CSV 稀疏行索引补失效规则和更多格式适配判断。

## 当前待讨论

待讨论事项统一维护在 [ADDP 数据类型与文件格式待规范事项](addp数据类型与文件格式待规范事项.md)，当前重点是：

1. 容器内部对象升格条件。
2. 第三方格式 manifest。
3. 能力发现视图。
4. Manager 内容读取插件边界。
5. content_index 扩展。

具体格式推进不放入待讨论事项，统一回到 [common/format 格式完善矩阵](common-format格式完善矩阵.md) 维护。

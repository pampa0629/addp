# Transfer 数据类型与文件格式后续事项

更新时间：2026-05-16

只保留未决事项。

设计讨论稿：[Transfer 基于 common engine / format 的改造设计](transfer基于common-engine-format改造设计.md)。

1. Transfer 不再保留旧任务 JSON 兼容，后续改为新 endpoint 配置结构。
2. Transfer 不再长期保留私有 reader / writer plugins，通用读写能力迁入 `common/engine`、`common/engine/contentadapter`、`common/contentio` 和 `common/format`。
3. 需要继续补齐 common 中缺失的高性能 table reader / writer、contentio.Writer、stream / CDC 抽象；`common/format` 已有 CSV / TSV 最小 `TableWriterProvider`，其他格式按 data type 命名继续由具体 format plugin 实现。
4. Transfer 新框架要稳定成 `TransferPlanner -> TransferPlan -> common engine/resource/format -> TransferExecutor -> worker`。
5. `TransferPlan` 需要把 source / target 拆成 engine、resource、data_item、data_type、representation、format、spatial、policy、mode。
6. `mode` 归入 planner / executor，覆盖 batch、stream、micro-batch 和 CDC。
7. `geojson` 口径保持为 `format=json + spatial.target_encoding=geojson`，不要恢复顶层 `format=geojson`。
8. Transfer 未来要覆盖 table 之外的 document、media、container、graph 等 data type。

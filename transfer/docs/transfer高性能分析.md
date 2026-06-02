# Transfer 高性能分析（历史归档）

更新时间：2026-05-30

本文曾记录旧 Spatialite / 私有 reader-writer 路线下的千万级导入设想。该路线已经被当前 Transfer 主路径取代，不再作为实现依据。

当前性能优化应遵守以下边界：

- native table 连续读取优先进入 `common/engine` 的 `TableReadSessionProvider`。
- native table 连续写入优先进入 `common/engine` 的 `TableWriteSessionProvider`。
- PostgreSQL COPY、MySQL / Doris / ClickHouse 批量写入能力属于 `common/engine` 插件。
- encoded table 的连续读取 / 写入属于 `common/format` 的 `TableReaderProvider`、`TableWriterProvider`、`MultiTableReaderProvider`、`MultiTableWriterProvider`、`ScopeTableReaderProvider`。
- Transfer 只负责 planner / executor 编排、批大小、checkpoint 观测、日志和指标，不实现具体格式或引擎的私有高性能 reader / writer。

当前已稳定的性能相关能力：

| 能力 | 当前状态 |
|---|---|
| PostgreSQL cursor table read session | 已接入，避免大表 `LIMIT/OFFSET` 翻页退化。 |
| PostgreSQL COPY table write session | 已接入。 |
| MySQL table write session | 已接入事务内批量 insert。 |
| Doris table write session | 已接入 MySQL 协议批量 insert；Stream Load 尚未进入。 |
| ClickHouse table write session | 已接入批量 insert；原生批量接口尚未进入。 |
| Parquet whole scope reader | 已支持 range-backed `io.ReaderAt`、row group 顺序读取和 `field_selection` 下推。 |
| Shapefile indexed reader | range source 下可利用 `.shx` / `.dbf` 窗口读取。 |

后续性能增强进入以下文档或规范：

- 当前架构：[Transfer 当前架构设计](design.md)
- 任务配置：[Transfer 模块基本概念及配置说明](transfer-基本概念及配置说明.md)
- 引擎能力：[ADDP 引擎能力声明规范](../../docs/spec/addp引擎能力声明规范.md)
- 格式能力：[ADDP 数据类型与格式能力规范](../../docs/spec/addp数据类型与格式能力规范.md)

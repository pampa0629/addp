# Transfer 高性能分析（历史归档）

更新时间：2026-11-02

本文曾记录旧 Spatialite / 私有 reader-writer 路线下的千万级导入设想。该路线已经被当前 Transfer 主路径取代，不再作为实现依据。

## 归档边界与历史关注点

Transfer 只负责 planner / executor 编排、批大小、checkpoint 观测、日志和指标，不实现具体格式或引擎的私有高性能 reader / writer。引擎 session 与格式 reader / writer 的职责由[架构分层](design.md#二架构分层)、[Provider 消费规则](../../docs/spec/addp引擎插件接口规范.md#五上层消费规则)及[内容 I/O 调用链](../../docs/spec/addp内容IO抽象规范.md#调用链)维护。

本页保留以下历史关注点，不独立声明当前版本的接入或性能结果：

- PostgreSQL cursor 连续读取用于避免大表 `LIMIT/OFFSET` 翻页退化；COPY、MySQL／Doris／ClickHouse 批量写入属于 `common/engine` 插件。
- Parquet whole scope 关注 range-backed `io.ReaderAt`、row group 顺序读取与 `field_selection` 下推；Shapefile range source 关注 `.shx` / `.dbf` 窗口读取。布局与读取入口不能合并为普通单文件路径，现行边界见[table 主路径](design.md#四table-transfer-主路径)及[读取布局与入口](transfer-基本概念及配置说明.md#读取布局与入口)。

## 现行能力与验证导航

- 原生目标的当前写侧与限制见[配置正文](transfer-基本概念及配置说明.md#已接入的原生目标写侧)，格式读写见[格式支持](transfer-基本概念及配置说明.md#33-格式)；不在归档重复维护“已接入”清单。
- Doris Stream Load、ClickHouse 原生批量接口等待评估增强及未支持范围统一见[通用目标与执行增强](../../docs/next/transfer后续能力清单.md#五通用目标与执行增强)，不以现有 SQL INSERT 证明这些增强通过。
- 引擎能力与格式能力分别沿[能力声明规范](../../docs/spec/addp引擎能力声明规范.md)、[数据类型与格式能力规范](../../docs/spec/addp数据类型与格式能力规范.md)。
- 标准入口与环境前提见[README 门禁说明](../README.md#常规模块与专项门禁)，固定版本、失败与修复、清理及未覆盖项见[分版本证据索引](../CLAUDE.md#专项约束与分版本证据)。

本页未附固定源码版本与性能测量证据，不能作为当前实现、千万级吞吐或完整链路已通过的结论。未运行、失败、跳过或未覆盖项不计为通过。

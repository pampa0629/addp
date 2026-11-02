# Transfer 转换器架构分析（历史归档）

更新时间：2026-11-02

本文曾记录旧 Transfer 私有 Reader / Writer / Pipeline / ConnectorRegistry 方案。该方案已经退出当前主路径，不再作为实现依据。

## 归档边界

Transfer 不再维护私有 reader / writer 插件体系。engine-native、格式读写、content 定位与 engine-to-contentio 适配的职责统一见[架构分层](design.md#二架构分层)和[内容 I/O 归属](../../docs/spec/addp内容IO抽象规范.md#命名和位置)，不在归档复制接口清单或当前数据流。

历史归属表中的过滤、派生字段和表达式编排不构成当前已开放的 Transfer transform。现行 `field_mapping` 写入 `config.transforms[]`，范围与排除项见[Transform 边界](design.md#五transform)；源端只读查询与轻量 ETL 单独沿[query source 配置](transfer-基本概念及配置说明.md#23-bounded-query-source-与轻量-etl-构造)，不能把源查询能力记成另一套 Transfer transform。

可被多个模块复用的通用数据处理能力仍应优先上收到 `common/`；新增能力先按[Provider 消费规则](../../docs/spec/addp引擎插件接口规范.md#五上层消费规则)和[内容 I/O 调用链](../../docs/spec/addp内容IO抽象规范.md#调用链)确认归属，不恢复私有 Pipeline 或 ConnectorRegistry 路线。

## 现行正文与验证导航

- table 数据流与 native／encoded single、multi、whole 的入口组合见[table 主路径](design.md#四table-transfer-主路径)。
- 字段投影、重命名、默认值、decimal 精度及 `project`／`passthrough` 分工见[field_mapping 正文](transfer-基本概念及配置说明.md#五field_mapping-transform)，格式与数据类型职责沿[格式能力规范](../../docs/spec/addp数据类型与格式能力规范.md)。
- 未完成项与实施前置条件见[后续能力清单](../../docs/next/transfer后续能力清单.md)，不将归档中的扩展设想列为现行能力。
- 标准入口与环境前提见[README 门禁说明](../README.md#常规模块与专项门禁)，固定版本、失败与修复、清理及未覆盖项见[分版本证据索引](../CLAUDE.md#专项约束与分版本证据)。

本页不保存固定版本运行通过记录。架构归属或数据流说明不能替代真实权限、源读／目标写、恢复与清理验收；未运行、失败、跳过或未覆盖项不计为通过。

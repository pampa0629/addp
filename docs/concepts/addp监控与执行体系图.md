# ADDP 监控与执行体系图

本文档展示 ADDP 平台的统一执行监控架构和跨模块任务追踪机制。

平台运行观测的对象身份、采集、资源查询、Trace 与告警契约统一见[平台运行观测规范](../spec/addp平台运行观测规范.md)；本页既有执行事实不改为遥测存储。当前实现与已验收范围分别认定，状态和历史证据按下表查阅，固定版本、失败及未覆盖项沿原记录保留。

## 专题导航

| 阅读目的 | 专题 |
| --- | --- |
| Owner / Common / Monitor 职责、执行存储与运行时角色 | [执行架构与存储](monitoring/执行架构与存储.md) |
| 执行状态、生命周期和模块任务类型 | [执行状态与任务类型](monitoring/执行状态与任务类型.md) |
| 跨模块读取、诊断、统计、租户告警与通知 | [执行诊断与租户告警](monitoring/执行诊断与租户告警.md) |
| 运行日志、操作审计、平台观测及平台告警 | [平台日志与运行观测](monitoring/平台日志与运行观测.md) |
| Platform 当前能力与剩余范围 | [后续事项能力表](../next/ADDP平台运行监控与可观测性设计.md#一当前实现与剩余工作)；来源、OS 与容器支持见[正式运行环境边界](../spec/addp平台运行观测规范.md#52-部署与运行环境) |
| 固定版本、失败、清理与实际验收范围 | [引擎专业观测](../spec/testing/evidence/Monitor采集来源与引擎指标历史证据.md)、[实例与容器证明](../spec/testing/evidence/Monitor实例与服务发现历史证据.md)、[Trace 与日志](../spec/testing/evidence/MonitorTrace与日志历史证据.md)；其他主题见[历史证据总入口](../spec/testing/evidence/Monitor历史证据.md#按主题查阅) |

## 相关文档

- [平台运行观测规范](../spec/addp平台运行观测规范.md)
- [返回核心概念关系图](addp核心概念关系图.md)
- [ADDP 任务编排体系图](addp任务编排体系图.md)
- [ADDP 任务体系规范](../spec/addp任务体系规范.md)
- [Monitor 模块架构说明](../../monitor/docs/Monitor模块架构说明.md)

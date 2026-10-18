# ADDP 元模型（Meta-Model）

本文档从业务语义层面提炼 ADDP 的核心对象及其关系。
元模型中的对象是**业务概念**，不直接对应数据库表：一个业务对象可能跨多张表，多个对象也可能共享一张表的不同行。

图中选列用于解释业务身份、主要字段和关系，不是完整建表脚本。具体字段、类型和数据库约束以 owner 规范及 migration 为准；尚未确认的模型扩展和待核对的消费边界在对应专题中单独列出。

> IAM 概念以 [ADDP 账号与权限体系](addp账号与权限体系图.md) 为准。本文不展示旧 `user_type` 和 User 单 Tenant 表结构；IAM 具体字段、关系与迁移边界见 [System IAM 数据模型与迁移规范](../../system/docs/IAM数据模型与迁移规范.md)。

---

## 专题导航

| 阅读目的 | 文档 |
| --- | --- |
| 横切能力与模块依赖总图 | [全局能力与模块概览](meta-model/全局能力与模块概览.md) |
| System 对象、模块角色、引擎能力与数据面 API 消费方 | [System 与引擎](meta-model/System与引擎.md) |
| Task 抽象、各 owner 任务与执行关系 | [任务与执行](meta-model/任务与执行.md) |
| 预览、瓦片缓存与向量结果 | [Manager](meta-model/Manager.md) |
| 数据节点、数据项与扫描任务 | [Meta](meta-model/Meta.md) |
| 查询服务、瓦片服务与外部注册服务 | [Service](meta-model/Service.md) |
| 标准身份、修订、指标语义与提炼证据 | [Standard](meta-model/Standard.md) |
| 实体、逻辑表、映射、层级与指标实现 | [Model](meta-model/Model.md) |
| 对象间业务引用、编排与执行产出 | [跨模块关系](meta-model/跨模块关系.md) |

## 阅读边界

- 每篇将同一模块的中英文对象表、关系图与约束放在一起；字段细节以 owner 规范及 migration 为准。
- Task 是跨模块抽象，TaskProvider 是 Module 的可选角色，二者都不要求独立实体表。
- 尚未确认的服务存储模型、码值映射与待核对消费路径，分别保留在 Service、Standard、Model 专题；文档拆分不表示这些事项已实现或验收。

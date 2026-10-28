# ADDP 任务体系规范

> 状态：规范。本文定义 ADDP 全平台任务定义、执行记录、任务提供者、编排和监控的统一边界。模块内部任务语义可在各模块专题中继续细化，但不得违背本文。

## 专题导航

| 阅读目的 | 文档 |
| --- | --- |
| 持久任务、绑定、授权主体与语义身份 | [任务定义与授权](tasks/任务定义与授权.md) |
| 执行字段、状态、触发来源与 claim / lease | [执行记录与租约](tasks/执行记录与租约.md) |
| 发起主体、导入来源、自动扫描与血缘 | [生产来源与产物扫描](tasks/生产来源与产物扫描.md) |
| owner 任务类型、Manager / Develop / Graph 与跨模块交接 | [模块任务契约](tasks/模块任务契约.md) |
| Transfer 的同步维度、bounded / continuous 执行 | [Transfer 同步执行](tasks/Transfer同步执行.md) |
| 数据库 CDC、捕获位点、结构审批与恢复 | [Transfer 数据库 CDC](tasks/Transfer数据库CDC.md) |
| 业务 Kafka DLQ、重放与 Kafka 部署基线 | [Transfer Kafka 与 DLQ](tasks/Transfer-Kafka与DLQ.md) |
| 任务发现、标准接口、参数契约与能力声明 | [TaskProvider 接口](tasks/TaskProvider接口.md) |
| 任务编排、可靠派发、调度和执行边界 | [编排与调度](tasks/编排与调度.md) |
| 编排参数来源、画布连线与绑定操作示例 | [Orchestrator 执行参数专题](../../orchestrator/docs/参数化模板说明.md)（模块操作说明） |
| 安全诊断、过程事件、健康和执行监控页面 | [执行监控](tasks/执行监控.md) |

## 范围

本文回答：

- 什么是任务定义。
- 什么是执行记录。
- 执行结果如何携带统一的 `lineage_facts`。
- `common.task_executions` 如何统一记录 execution。
- TaskProvider 如何声明一个模块可提供的任务类型。
- Orchestrator 如何引用和触发跨模块任务。
- Monitor 如何查询和展示任务执行。

本文不展开：

- Manager 内部瓦片缓存生成、embedding、预览状态和快显结果的详细策略。
- Transfer 各引擎方言和 continuous runtime 的实现细节。
- Develop 算子工作流内部节点模型。
- Meta scan 具体扫描范围和 detector 规则。

这些内容由对应模块或专题文档定义；本文只要求它们能纳入统一任务体系。

## 基本原则

1. ADDP 不建立统一任务总表。
2. `Task` 是抽象概念，不是跨模块实体表。
3. 任务定义归 owner 模块私有表。
4. 执行记录统一进入 `common.task_executions`。
5. 调度定义归任务定义 owner 模块。
6. 运行时队列只是执行机制，不是任务定义。
7. 产物状态归产物 owner 模块。
8. Orchestrator 只编排任务能力，不拥有业务任务定义。
9. Monitor 只聚合观察，不成为任务 owner。
10. ad-hoc-only execution type 可以写入统一执行记录，但在没有持久任务定义前不得声明为 TaskProvider 能力或进入 Orchestrator 任务选择。
11. 真实读写 owner 必须在 execution 结果中写入版本化 `lineage_facts`；Meta 负责消费并维护血缘关系，Orchestrator 不重复生成资源血缘。
12. Quality `quality_plan`、Meta `scan` 和 Transfer bounded `sync` 的 execution worker 必须是 owner 模块附属的独立进程；Develop 全部 `query | workflow | script` bounded execution、Manager bounded execution、Orchestrator `orchestration` 与 Model `logical_table_materialization` 分别由各自 Backend 内嵌的有界执行监督器运行。两种部署形态都必须使用 PostgreSQL execution claim + lease，部署形态不能改变 execution 所有权协议。
13. owner scheduler 运行在 owner Backend，只负责按任务定义发现到期任务并创建 durable `pending` execution；Worker 不可用不得阻止 scheduler 创建 execution。dispatcher 只负责 outbox/delivery 投递，二者都不得替代 execution worker 成为业务执行事实源。
14. bounded runtime queue 的唯一主路线是 `common.task_executions` PostgreSQL claim，不保留 Redis/Asynq、请求内 goroutine 或进程内 channel。独立 Worker 与 owner Backend 内嵌监督器是明确的模块级部署选择，不得在同一模块内双轨消费；continuous runtime、dispatcher 和 maintenance loop 继续使用各自专用协议，不强行迁入 bounded claim。

## 核心对象

| 对象 | Owner | 存储 | 含义 |
| --- | --- | --- | --- |
| 任务定义 | 业务 owner 模块 | owner 模块私有表 | 未来应该按什么策略处理什么对象 |
| 执行记录 | Common | `common.task_executions` | 某一次实际执行了什么、状态如何、结果如何 |
| 调度定义 | 任务定义 owner | owner 模块私有表 | 是否启用定时、Cron 表达式、下一次运行时间 |
| 运行时队列 | 执行 owner | bounded 使用 PostgreSQL claim；continuous 使用 runtime lease | 如何把 execution 交给合法运行者 |
| 产物状态 | 产物 owner 模块 | owner 模块私有表或 artifact manifest | 当前产物是否可用、在哪里、由什么配置生成 |
| 编排定义 | Orchestrator | `orchestrator.orchestrations` | 任务级 DAG，当前只引用已有任务定义；inline execution 后续专题再设计 |

## 模块接入检查清单

模块的持久任务定义要纳入 TaskProvider 和 Orchestrator，至少满足：

- 有明确任务定义 owner。
- 持久任务定义表只保存定义和最近执行摘要。
- 每次执行写入 `common.task_executions`。
- `trigger_type` 只写 `manual` / `scheduled`。
- `source` 写触发来源。
- `task_type` 稳定并声明到 TaskProvider capabilities。
- 多任务类型模块使用一个 provider，并在 `task_capabilities[]` 中声明多个任务类型能力。
- execution 能按 `module + task_type + source_task_id` 回查任务定义。
- ad-hoc execution 保存完整 `execution_config`。
- Swagger 和模块文档同步。

ad-hoc-only execution 不要求存在任务定义或声明 TaskProvider capability，但必须有明确 execution owner、稳定 `task_type`、完整 `execution_config`、统一 execution 状态和 owner API 文档。没有 `source_task_id` 时不得伪造任务详情回查能力。

## 与相关文档的关系

- Meta 扫描任务细节见 [元数据扫描机制规范](addp元数据扫描机制规范.md)。
- Transfer 稳定任务语义以本文、[Transfer 任务语义与同步模式](../../transfer/docs/transfer-任务语义与同步模式.md) 和 [Transfer 模块基本概念及配置说明](../../transfer/docs/transfer-基本概念及配置说明.md) 为准；尚未实现的能力统一记录在 [Transfer 后续能力清单](../next/transfer后续能力清单.md)。
- Manager 派生产物任务内部语义见[快显实现规范](../../manager/docs/快显实现规范.md)和[向量化能力说明](../../manager/docs/向量化能力说明.md)。

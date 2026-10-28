# Monitor 模块架构说明

Monitor 聚合任务执行事实，提供授权诊断、统计、健康观测、告警和通知。本页作为模块导航；稳定概念与规则归平台文档，不保留历史建表 SQL、现场执行记录、阶段完成勾选或笼统的“已上线”结论。

## 职责与事实来源

| 范围 | 权威正文与实现 |
| --- | --- |
| 任务定义与执行事实 | 定义归各业务 Owner；统一执行模型、状态、租约、来源和父子关系见[执行记录与租约](../../docs/spec/tasks/执行记录与租约.md)及[Common execution](../../common/execution/)。Monitor 不保存第二套任务定义或执行主表。 |
| 执行读取与诊断 | [执行监控规范](../../docs/spec/tasks/执行监控.md)定义同步 BFF / Owner 逐请求读取裁决、安全 DTO、稳定错误诊断及前端唯一所有权；可读父执行不自动授予子执行或业务结果权限。 |
| 执行统计与运行健康 | [正式统计契约](../../docs/spec/tasks/执行监控.md#执行统计与运行指标)维护统计 API、口径、Owner 读取范围和验收边界；[Provider 与后台心跳](../../docs/spec/tasks/执行监控.md#provider-与后台运行实例健康)分别维护即时探测与存活观测，不据此判定任务成功。 |
| 租户告警与通知 | [概念说明](../../docs/concepts/monitoring/执行诊断与租户告警.md#执行信号与租户告警)及[术语表](../../docs/concepts/addp术语表.md)维护 incident / event / delivery、通用规则、Webhook、邮件和通知路由；各渠道消费同一生命周期事实，规则不改写 execution。 |
| 平台观测与告警 | [平台运行观测规范](../../docs/spec/addp平台运行观测规范.md)和[运行日志规范](../../docs/spec/addp模块服务运行日志规范.md)维护主机身份、遥测、平台事件及通知；Platform 与 Tenant 分别授权。 |
| Platform 能力状态与验收缺口 | [后续事项能力表](../../docs/next/ADDP平台运行监控与可观测性设计.md#一当前实现与剩余工作)维护实现状态和剩余覆盖；固定版本、失败与实际验收范围见[模块主题证据导航](../CLAUDE.md#历史证据导航)及对应专题。 |
| 服务组失联 | [部署预期与租约投影](../../docs/spec/addp平台运行观测规范.md#48-服务部署预期与分组租约观测)、[运行确认与失联规则](../../docs/spec/addp平台运行观测规范.md#711-服务组失联规则)分别维护事实与判定；真实故障、通知及清理沿[测试场景](../../docs/spec/testing/scenarios/平台观测与语义生命周期.md#服务组失联恢复与通知)，实际结果沿下文主题证据导航。 |
| HTTP 接口质量 | [正式规范 6.4](../../docs/spec/addp平台运行观测规范.md#64-http-接口质量分阶段接入)定义平台职责、全量请求口径、Gateway/Backend 分层、固定窗口与权限；Common 采集、Monitor 固定查询与 HTTP 规则复用既有身份、指标中心和平台 outbox，Tenant 发布服务质量归 Service。 |

## 存储、接口与页面

- 统一执行存储以 [TaskExecution 模型](../../common/execution/task_execution.go)、[EnsureStore](../../common/execution/store.go)及正式任务契约为准，不复制旧字段映射、索引或迁移 DDL。
- 当前管理 API 使用 `/api/v1/monitor`；认证、Permission、参数和 DTO 以[模块契约](../CLAUDE.md)、[Router](../backend/internal/api/router.go)及生成 Swagger 为准，不沿用缺少模块前缀的旧接口列表。
- 页面与共享组件入口见[前端 Router](../frontend/src/router/index.js)；执行详情、安全投影、父子树和通知页签遵守现行监控前端所有权，不用“组件文件存在”代替页面验收。
- 身份使用 System opaque Token / AuthContext；配置归属见[配置规范](../../docs/spec/addp配置介绍.md)。SMTP Relay 由 Monitor 平台配置管理，租户只维护通知目标；不再使用旧 JWT / 五分钟 Redis 认证缓存方案说明当前链路。
- 执行存储维护由 [System 公共维护循环](../../system/backend/internal/service/execution_event_maintenance.go)调用 Common，Monitor 只提供授权诊断；期限、保留裁决与验收导航见[执行留存与验收](#执行留存与验收)。

### 执行留存与验收

留存契约以[执行监控规范](../../docs/spec/tasks/执行监控.md#概览保留与清理契约)为准；下表区分事件清理、概览只读评估与真实周期，不把测试部署销毁计为业务记录删除。

| 范围 | 当前边界与正式正文 | 历史证据及未覆盖项 |
| --- | --- | --- |
| 30 天过程事件 | 已实现事件物理清理；事件过期不授予概览删除权，专业结果、审计和平台日志各沿自身生命周期。标准场景见[事件维护](../../docs/spec/tasks/执行监控.md#30-天过程事件维护真实进程验收)。 | [固定 `8e84cc8a6` 首轮证据](../../docs/spec/testing/evidence/Monitor执行监控与留存历史证据.md#30-天过程事件清理真实进程验收2026-10-26)含实际删除与完整行不变断言、登记失败及 PG 跳过；首轮不外推小时周期或集中日志留存。 |
| 180 天终态概览、步骤摘要与安全错误 | 目标按完整父子树保留，引用或待消费义务可延长；当前[只读评估](../../docs/spec/tasks/执行监控.md#第一期只读候选评估)不提供删除许可或用户 API。Owner 行内事实释放与跨模块引用未核验，概览物理删除尚未实施，专业结果不直接套用 180 天。 | [第一期回归及失败](../../docs/spec/testing/evidence/Monitor执行监控与留存历史证据.md#2026-10-24-概览保留只读评估第一期)、[固定 `86f58b376` 首轮与原失败](../../docs/spec/testing/evidence/Monitor执行监控与留存历史证据.md#执行概览只读维护真实进程验收2026-10-25)分别认定；时间候选不等于可删除，只读结果不替代事件清理或概览删除验收。 |
| 自然小时周期与连续批次 | 按[小时周期场景](../../docs/spec/tasks/执行监控.md#小时周期与连续批次真实进程验收)分别核对事件删除和只读评估；批次计数不能累加为全库唯一树数。 | [固定 `c2f53b007` 周期证据](../../docs/spec/testing/evidence/Monitor执行监控与留存历史证据.md#小时周期与连续批次真实进程验收2026-10-26)限同一 System 进程、合成历史和两个自然小时；持续 Worker 并发、重启／故障恢复等未覆盖项沿原记录，不扩展为 Owner 释放、概览删除或 T5。 |

事件读取的 Owner 授权、分页与截断另见[模块 T2 范围](../CLAUDE.md#开发与验证)及[真实入口历史证据](../../docs/spec/testing/evidence/Monitor执行监控与留存历史证据.md#大量事件分页与截断真实入口验收通过限定范围2026-10-26)，不能代替存储维护验收。固定源码、首次失败、修复、跳过与最终清理分别按原记录认定，不拼接不同版本为当前 HEAD 或生产通过。

## 后续范围与待核对口径

平台资源、采集、告警与 Trace 的状态统一见[后续事项能力表](../../docs/next/ADDP平台运行监控与可观测性设计.md#一当前实现与剩余工作)。下表只维护执行监控领域的后续需求与决策边界。

| 事项 | 保留的需求与决策边界 |
| --- | --- |
| bounded 恢复时间 | `started_at` 采用[当前 attempt 契约](../../docs/spec/tasks/执行记录与租约.md#bounded-execution-领取租约和恢复契约)。Common [RetryExpired](../../common/execution/lease.go) 在过期租约围栏内恢复为 `pending` 时清空 `started_at`，保留 execution UUID 与 `created_at`；再次 claim 写入新开始时间并递增 attempt、生成新 token。Common Go 回归覆盖首次／再次 claim、恢复排队及旧运行体拒绝；Quality SQLite／PostgreSQL 回归覆盖恢复摘要和排队失败不借用旧开始时间／耗时，Monitor PostgreSQL 回归覆盖恢复排队、再次运行／完成和排队失败的时间聚合。沿 `make test-module MODULE=common GO_ONLY=1`、`make test-quality-postgres` 和 `make test-monitor-postgres` 自动发现；现有排队／运行时长 SQL 公式不变，确定性证据不代表 Hosted／生产重启恢复验收。 |
| 实时推送 | WebSocket 执行状态推送仅保留为候选；先明确刷新收益、授权撤销、连接范围和负载，不替换现有读取主路径。 |
| 成功率与 stalled 规则 | 后续扩展须依赖 Owner 公共事实及明确 heartbeat/deadline 契约，不能以 created_at、updated_at 或全局固定时长猜测任务卡死。 |
| 规模与分区 | 按实测索引、查询计划和维护成本评估是否需要分区；删除旧“超过一千万即分区”和手工按月建表步骤，不作为已批准的存储变更。 |
| 保留与归档 | 期限、引用保留与删除边界沿[执行留存与验收](#执行留存与验收)；专业运行时历史和其他 Owner 阶段事件仍须分别接入验收，对象存储归档尚非现行主路径。 |

## 验证与运行边界

从仓库根目录使用 `make test-module MODULE=monitor`，当前工作区按范围使用 `make test-changed`；持久化与页面分别沿 `make test-monitor-postgres`、`make test-monitor-frontend`。测试分层及跨模块场景沿[测试与验收规范](../../docs/spec/addp测试与验收规范.md)；固定版本、失败、清理及未覆盖项统一见[模块主题证据导航](../CLAUDE.md#历史证据导航)，按各专题原记录认定。

验收结论须对应固定源码、实际执行项与完整证据，旧样本数量、成功率、缓存命中率或日期标记不能代替当前验收。真实 System / Gateway / Owner / Worker 链路仍按对应 T4 场景核对，生产部署不由单元测试或历史通过结论外推。端口、启动与重启沿[端口规范](../../docs/spec/addp端口分配.md)和[部署指南](../../docs/guide/addp部署和开发步骤.md)，需要重启时由用户执行标准生命周期命令。

# Monitor 模块架构说明

Monitor 聚合任务执行事实，提供授权诊断、统计、健康观测、告警和通知。本页作为模块导航；稳定概念与规则归平台文档，不保留历史建表 SQL、现场执行记录、阶段完成勾选或笼统的“已上线”结论。

## 职责与事实来源

| 范围 | 权威正文与实现 |
| --- | --- |
| 任务定义与执行事实 | 定义归各业务 Owner；统一执行模型、状态、租约、来源和父子关系见[执行记录与租约](../../docs/spec/tasks/执行记录与租约.md)及[Common execution](../../common/execution/)。Monitor 不保存第二套任务定义或执行主表。 |
| 执行读取与诊断 | [执行监控规范](../../docs/spec/tasks/执行监控.md)定义同步 BFF / Owner 逐请求读取裁决、安全 DTO、稳定错误诊断及前端唯一所有权；可读父执行不自动授予子执行或业务结果权限。 |
| 执行统计与运行健康 | [监控与执行体系图](../../docs/concepts/monitoring/执行诊断与租户告警.md#监控功能)说明统计口径、运行实例与信号；Provider 健康复用 System 声明、有效 Backend、模块 ready 和标准任务发现接口，不读取 Owner 私表。 |
| 租户告警与通知 | [概念说明](../../docs/concepts/monitoring/执行诊断与租户告警.md#监控功能)及[术语表](../../docs/concepts/addp术语表.md)维护 incident / event / delivery、通用规则、Webhook、邮件和通知路由；各渠道消费同一生命周期事实，规则不改写 execution。 |
| 平台观测与告警 | [平台运行观测规范](../../docs/spec/addp平台运行观测规范.md)和[运行日志规范](../../docs/spec/addp模块服务运行日志规范.md)维护主机身份、遥测、平台事件及通知；Platform 与 Tenant 分别授权。 |
| 服务组失联 | [部署预期与租约投影](../../docs/spec/addp平台运行观测规范.md#48-服务部署预期与分组租约观测)、[运行确认与失联规则](../../docs/spec/addp平台运行观测规范.md#711-服务组失联规则)分别维护事实与判定；真实故障、通知及清理沿[测试场景](../../docs/spec/testing/scenarios/平台观测与语义生命周期.md#服务组失联恢复与通知)，实际结果沿下文主题证据导航。 |
| HTTP 接口质量 | [正式规范 6.4](../../docs/spec/addp平台运行观测规范.md#64-http-接口质量分阶段接入)定义平台职责、全量请求口径、Gateway/Backend 分层、固定窗口与权限；Common 采集、Monitor 固定查询与 HTTP 规则复用既有身份、指标中心和平台 outbox，Tenant 发布服务质量归 Service。 |

## 存储、接口与页面

- 统一执行存储以 [TaskExecution 模型](../../common/execution/task_execution.go)、[EnsureStore](../../common/execution/store.go)及正式任务契约为准，不复制旧字段映射、索引或迁移 DDL。
- 当前管理 API 使用 `/api/v1/monitor`；认证、Permission、参数和 DTO 以[模块契约](../CLAUDE.md)、[Router](../backend/internal/api/router.go)及生成 Swagger 为准，不沿用缺少模块前缀的旧接口列表。
- 页面与共享组件入口见[前端 Router](../frontend/src/router/index.js)；执行详情、安全投影、父子树和通知页签遵守现行监控前端所有权，不用“组件文件存在”代替页面验收。
- 身份使用 System opaque Token / AuthContext；配置归属见[配置规范](../../docs/spec/addp配置介绍.md)。SMTP Relay 由 Monitor 平台配置管理，租户只维护通知目标；不再使用旧 JWT / 五分钟 Redis 认证缓存方案说明当前链路。
- 过程事件的 30 天滚动清理由 [System 公共维护循环](../../system/backend/internal/service/execution_event_maintenance.go)调用 Common 完成，不删除 execution 概览或审计。同一维护循环调用 Common 的 180 天概览只读候选评估，只汇总有界批次的完整树、时间条件及保留／未知原因，不提供删除许可或用户 API。Owner 行内事实释放与跨模块引用覆盖尚未核验；终态概览/步骤/安全错误的保留目标及引用延长边界见[执行监控规范](../../docs/spec/tasks/执行监控.md)，不能视为物理清理已经落地。

## 后续范围与待核对口径

| 事项 | 保留的需求与决策边界 |
| --- | --- |
| 实时推送 | WebSocket 执行状态推送仅保留为候选；先明确刷新收益、授权撤销、连接范围和负载，不替换现有读取主路径。 |
| 成功率与 stalled 规则 | 后续扩展须依赖 Owner 公共事实及明确 heartbeat/deadline 契约，不能以 created_at、updated_at 或全局固定时长猜测任务卡死。 |
| 规模与分区 | 按实测索引、查询计划和维护成本评估是否需要分区；删除旧“超过一千万即分区”和手工按月建表步骤，不作为已批准的存储变更。 |
| 保留与归档 | 180 天概览清理、引用保留裁决、专业运行时历史和其他 Owner 阶段事件按实际接入分别验收；对象存储归档尚非现行主路径，不因已有事件清理就视为全部完成。 |

## 验证与运行边界

从仓库根目录使用 `make test-module MODULE=monitor`，当前工作区按范围使用 `make test-changed`；持久化与页面分别沿 `make test-monitor-postgres`、`make test-monitor-frontend`。测试分层及跨模块场景沿[测试与验收规范](../../docs/spec/addp测试与验收规范.md)；固定版本、失败、清理及未覆盖项统一见[模块主题证据导航](../CLAUDE.md#历史证据导航)，按各专题原记录认定。

本页没有新增测试入口，不把旧样本数量、成功率、缓存命中率或日期标记作为当前验收证据。真实 System / Gateway / Owner / Worker 链路仍按对应 T4 场景核对，生产部署不由单元测试或历史通过结论外推。端口、启动与重启沿[端口规范](../../docs/spec/addp端口分配.md)和[部署指南](../../docs/guide/addp部署和开发步骤.md)，需要重启时由用户执行标准生命周期命令。

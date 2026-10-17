# ADDP 智能体能力开放后续事项

阶段 0–6 的稳定能力已归入正式规范，不再在本专题保存实施历史、完成清单或重复架构说明。

## 三、正式规范事实源

当前阅读顺序固定为：

1. `docs/concepts/addp术语表.md`：稳定概念与命名。
2. `docs/spec/addp智能体Tool开放规范.md`：Tool Manifest、ToolExecutor、SDK / CLI / Adapter 单一路线。
3. `docs/skills/addp-Skill规范.md`：Skill 目录、粒度、渐进式加载和 Tool 引用。
4. `docs/spec/addp智能体交互协议规范.md`：AgentRun、AG-UI、消息 parts、Interaction、ResultRef、Presentation 和 A2UI。
5. `docs/spec/addp智能体评测规范.md`：场景、在线证据、门禁、比较和正式发布评测基线。
6. `docs/spec/addp授权上下文规范.md` 与 `docs/spec/addp OAuth授权规范.md`：AuthContext、OAuth、委托身份和 owner 写审批。
7. `docs/spec/addp-API设计规范.md` 与 `docs/spec/addp-Swagger集成指南.md`：HTTP API、错误和 Swagger。
8. `agent/CLAUDE.md`、`agent/docs/agent模块设计共识.md`、`common-python/CLAUDE.md`、`common-frontend/README.md`：模块实现。

本文不再定义上述规范的字段、状态机、上限或命令。实现与本文历史记录冲突时，以正式规范和当前代码为准。


## 七、延期事项与启动条件

以下事项不是当前主线，也不得写成已开放能力：

| 延期事项 | 重新启动条件 |
| --- | --- |
| `GraphView` | 稳定评测场景需要图谱交互，且现有文本/链接无法满足。 |
| MCP Adapter | 出现明确外部消费者、宿主协议和隔离评测；继续复用同一 Manifest、ToolExecutor 和 SDK。 |
| 多智能体委派 | 明确委派身份、预算、隔离、审批和审计场景，并先补正式规范与评测。 |
| 趋势 UI / 阈值 | 外部发布系统已持续归档同一版本报告，并积累足以解释分布的真实样本。 |
| 外部归档产品实现 | 发布系统明确 owner、保留期、不可变对象和 baseline 指针事务。 |


## 维护方式

新增能力先确认实际场景、身份与评测，再更新对应正式规范；完成后从待办移除。验证使用根 Makefile 的 Agent、common-python 与前端标准入口，不将历史在线证据当作本轮验证。

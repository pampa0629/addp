# Agent 模块设计共识

更新时间：2026-10-30

本文只记录 Agent 模块内部边界。平台级 Tool、AG-UI / A2UI 和评测契约分别以 `docs/spec/addp智能体Tool开放规范.md`、`docs/spec/addp智能体交互协议规范.md`、`docs/spec/addp智能体评测规范.md` 为事实源；Skill 与认证分别引用各自既有规范。专题文档只保留架构决策和实施历史。

## 一、模块定位

Agent 是 ADDP 官方提供的自然语言交互产品和一种 Agent Runtime，负责：

- 会话、消息和历史摘要；
- Agent run 和多轮上下文；
- Skill 路由和 Tool 调用循环；
- 对话澄清 Interaction；
- AG-UI 事件输出；
- ResultRef、Interaction 和 Presentation 的消息编排。

Agent 不拥有平台级 Skill、owner 模块业务结果或审批事实，也不复制 Copilot 的结构化生成逻辑。

### 对话工作区（三批实施契约）

1. **会话管理与 Skill 首页**：会话列表按当前 User/Tenant 隔离，以 `search` 对标题做字面量子串检索，使用标准 `page/page_size` 分页。默认只列未归档会话，置顶优先，再按更新时间、ID 倒序；前端对当前页作时间分组，不声称已加载全部历史。`sessions` 增加 BIGINT `version`、`pinned` 和 `archived`。重命名、置顶、归档、恢复与删除提交版本，事务内校验并递增；冲突保留编辑内容，由用户显式重新加载。会话组织操作使用独立 `agent.session.update`，默认属于已有 `tenant.ai_user`，不增加业务 Tool 权限。
2. **运行过程与成果侧栏**：按会话分页列出本人 AgentRun，详情复用已有受限 steps 与指标。仅具备 `agent.run.read` 时加载运行审计；普通聊天不因此新增读取权限要求。成果索引从消息现有 Presentation/ResultRef 投影，点击定位来源消息并复用既有 Renderer；不新增 Artifact 表、结果副本或 owner 状态缓存。
3. **可见计划**：复用 Harness 的 TodoListMiddleware，`write_todos` 仅是 Runtime 私有计划动作，不能作为平台 Tool 或业务批准。最多 12 条、每条 300 字符；条目为 `content + pending|in_progress|completed`。进度是模型报告的计划处理状态，不证明业务执行或验证通过；UI 与真实 steps、正式报告分别展示。简单问答不要求计划。计划经既有 AG-UI Activity 安全事件持久化，正常回复的消息 parts 保存相同展示投影；断线、取消或异常后的最新计划由 Run 详情从安全事件恢复，恢复同一 Run 时从最近已提交安全事件中的计划重建，不序列化框架状态、不增加另一套检查点。暂停、失败、取消、完成均不自动把计划条目标为完成。

同一次回复的计划更新共用稳定 Activity 身份，在流式消息与最终保存的消息 parts 中原位替换当前投影，不累积中间计划卡片；安全事件仍保留更新顺序。后续回复具有独立 Activity 身份，不改写历史消息当时的计划快照。

归档只改变会话组织：默认列表隐藏，归档列表与原详情 URL 仍可查看、恢复；已经接纳的运行继续，不取消 owner execution。归档后新消息、Interaction 恢复及失败重试统一拒绝，恢复后才可继续。接纳请求与归档更新锁定同一会话行，使先后顺序明确；不持会话锁等待模型或业务 HTTP。Owner 审批状态先按当前 User/Tenant 的待处理 Interaction 做只读预取，接纳锁内重读 Interaction 并沿原逻辑核对 owner 身份、指纹和状态；不从客户端传入该结果，不产生批准事实。

Skill 首页从正式注册表输出稳定 Skill 身份及其方法描述，名称、示例等界面文案由 Agent 双语词条持有并校验登记完整。`/agent` 固定展示能力首页，不自动打开列表第一条；详情按路由直接读取，不受列表分页或筛选影响。点击示例只填入输入框，提交后仍走正式路由；目录不代表当前权限、服务在线或业务可执行，也不替代 Ontology 平台能力语义目录。

Agent Schema 由 `backend/migrations/` 的连续版本 SQL 唯一维护；启动在同一 PostgreSQL 事务内持 advisory lock，校验已应用文件名与 SHA-256，拒绝历史漂移、缺号或数据库版本超前。首版迁移建表并收纳原未版本化结构，第二版增加会话组织字段。旧 create_all/零散 ALTER 初始化删除；System migration 不操作 Agent Schema。失败 DDL 和版本记录一起回滚，重复启动不重做已登记迁移。

实现与验证依上述顺序进行。已有 `make test-agent-eval`、`make test-agent-frontend` 和平台授权/OpenAPI 检查承接 T1/T3；新增 `make test-agent-postgres` 使用事务回滚测试 owner Schema 的升级、版本与隔离，并同步 T2 CI 登记。涉及平台 Transfer 冻结来源的文件按既有审查规则同步新修订，不改已发布历史。真实模型与真实部署验收另记，不能用受控门禁代替。

### 进程内执行内核

Agent 保留产品、路由和持久状态边界，唯一执行内核采用 DeerFlow Harness 的 `create_deerflow_agent` SDK，不部署 DeerFlow Server，也不保留自研模型／工具循环。依赖固定到官方源码提交 `130a9ab9f035a8f319502f8d5e044ed05962b2aa`，Agent 使用 Python 3.12；共享 Inference LangChain Adapter 与 Copilot 统一使用 LangChain Core 1.x。

每次请求构建独立、无框架持久化后端的 Harness。ADDP 中间件负责 Skill 轮数上限、受控 Tool 事件、owner 事实投影和暂停；工具仍唯一通过现有 Manifest Adapter → ToolExecutor 调用 owner。工具串行执行，暂停后同一模型响应中的其他调用不得产生副作用；有效澄清优先于同批业务工具。业务 AgentRun ID 仅进入运行上下文，不写入框架 `configurable.run_id`，恢复时从 ADDP 语义检查点重建，不恢复框架消息快照。

本阶段不启用 shell、文件、浏览器、子代理、框架记忆或自主审批；不注册 DeerFlow 内置业务工具。会话摘要沿用 ADDP 原有预算，Harness 不建立第二套持久记忆。模型只经 Inference Runtime；纯文本内容块被共享 Adapter 规范化，非文本内容明确拒绝。

产品镜像保留仓库相对层级：后端位于 `/app/agent/backend`，平台级 Skill 位于 `/app/skills`。开发和容器复用同一 Skill 加载路径，不新增容器专用 loader 或私有 Skill 副本。镜像构建检查应用导入、Skill 装配与依赖一致性；Agent 镜像的增量构建输入包含平台 `skills/`。

## 二、唯一交互协议

平台语义任务使用唯一的两阶段选择：初始模型只识别是否需要平台能力目录；Runtime 用 `platform.capabilities.list` 读取 Ontology 已激活定义后，模型才选择 capability，再由定义中的 Skill/Tool 引用装配领域执行。`platform_routed=true` 的 Skill 禁止从描述直接选中；目录失败不能退回旧路由。当前仅 Transfer 创建领域启用，普通对话和其他 Skill 不产生 Ontology 依赖。业务字段、资源身份与写入策略仍由正式事实和必要用户确认约束，不要求用户输入内部步骤。详细边界见 Ontology 最小设计契约 1.12。

Agent 后端唯一流式接口是：

```text
POST /api/v1/agent/chat
Content-Type: application/json
Request: AG-UI RunAgentInput
Response: text/event-stream
Events: AG-UI
```

不再使用 Vercel AI SDK 私有流、`0:` / `dag:` 前缀或 `result_type + result_data`。

A2UI 通过 AG-UI `ACTIVITY_SNAPSHOT` / `ACTIVITY_DELTA` 传输，第一批 ADDP Catalog 组件为：

- `WorkflowDag`；
- `ClarificationChoice`。

## 三、持久状态

PostgreSQL `agent` schema 当前包含：

- `sessions`：用户会话和历史摘要；
- `messages`：消息文本、AG-UI message id 和有序 parts；
- `runs`：跨 AG-UI 调用存在的 AgentRun、生命周期和语义检查点；
- `run_steps`：Tool 调用与 Runtime 控制动作的受限审计记录；
- `run_events`：可按 sequence 安全回放的 AG-UI 事件投影；
- `interactions`：服务端持久澄清，或 owner approval 的 ID、open URL、请求指纹、摘要和状态投影；
- `skill_usage`：Skill 使用统计。

AgentRun 使用唯一语义恢复路线：

已完成预检的草稿由 Runtime 以 AgentDraftRef 引用既有 `run_steps.input` 与同参 `facts.config_checks`，不新增数据库表或第二份可编辑配置。路由以唯一 `draft_action` 区分纯解释与原参数创建，并选择受信任近期引用；指代不清先询问，不自动选首项。两者复用同身份快照读取与私有 `draft.inspect` 审计。纯解释不装配业务工具；原参数创建在当前 checkpoint 仅保存 `draft_ref`，每次调用恢复原参数，复核和正式创建 Adapter 输入均由 Runtime 绑定，不让模型重写或补默认参数。仍读取当前平台能力/必要资源事实、重新预检和确认，绝不复制旧资源事实、报告、确认或授权。缺失、错版、损坏和越界引用终止，不从正文重建。明确修改仍进入原平台语义与 Skill 路径，不在原参数创建中静默更改；完整契约见交互协议规范 3.4。

草稿卡片的固定事实概要直接读取同一参数，不保存独立摘要；源／目标展示复用共享定位符格式化，边界、装载、写入策略及映射数不由模型重新填写。模型按 Skill 只补充关键解释。后端与前端按事件顺序追加 parts，Presentation 分隔后的文本另起 text part，不将所有文字前置；保存的 `content` 仍完整。概要不推断行粒度、类型或当前执行状态，既有报告、复核、参数摘要与权限路径不变。

1. 新用户目标创建 `agent.runs`，状态进入 `running`。
2. owner Tool 返回的引擎和 locator 等紧凑事实写入 `checkpoint.observed`，Tool 调用写入 `run_steps`。
3. 创建澄清或 owner approval 投影时，Interaction 通过 `agent_run_id` 关联 AgentRun，run 状态进入 `waiting`。
4. 用户回答澄清后，选中的 owner 事实写入 `checkpoint.confirmed`；用户检查审批时，Agent 使用原始 User Access Token 查询 owner，只有 owner 返回 `approved|consumed` 才完成 Interaction；两种情况都恢复同一个 AgentRun。
5. Runtime 使用预算化会话历史、增量摘要、Skill 和 AgentCheckpoint 重建模型调用，不序列化 LangChain 内部对象、隐藏推理或完整 Tool 大结果；通过 Manifest 结构与字节校验的受限 Tool 结果完整进入当前 ToolMessage，不截取 JSON 前缀。审计摘要只记录类型与字节数，不保存原文前缀。Skill 推理轮数耗尽后明确失败，不额外调用未绑定工具的模型收尾。
   显式配置检查返回有效三态报告后由 Runtime 展示报告、精确草稿及固定完成提示并结束本轮，不再请求模型收尾；同批只执行首个已装配的检查，不执行其他调用。输入错误可修参，服务或响应失败明确终止。自动创建预检仍继续原复核流程，后续解释使用既有草稿引用，不从自然语言关键词判断终态。
6. 所有模型调用经 Inference Runtime；Agent 只拥有 `reasoning`、`general-chat` Scenario Binding，同一 AgentRun 固定 reasoning Profile 快照，不拥有 Provider、Deployment 或 credential。
6. 最终进入 `completed`、`failed` 或 `cancelled`。

AG-UI 请求中的 `runId` 是协议调用标识；AgentRun 是服务端逻辑运行身份。一次 AgentRun 可以经历初始调用和若干次 resume 调用，恢复身份以 Interaction 的 `agent_run_id` 为准。

Interaction resume 沿用 AgentRun 已记录的 Skill 和 Tool 白名单，不重新执行主路由选择；协议调用 ID 可以变化，但业务运行身份和能力边界不变。

聊天文字在唯一 pending 澄清下也通过 Interaction resume 恢复原 Run。服务端对唯一精确匹配的选项采用规范候选事实，其他文字只进入模型补充上下文，不写入确认事实。创建复核文字使旧复核失效，不能代替确认按钮；owner approval 仍只由 owner 状态检查恢复。完整契约见交互协议规范 5.1。

Transfer 的配置草稿展示与创建前最终复核分开。用户只要求查看草稿、确认配置且明确不要创建时，模型仍可取证和澄清配置缺口；显式正式预检完成后，由 Runtime 固定展示精确参数、报告和完成提示并结束本轮，不请求模型收尾，也不发出 `transfer_create_review` 或“确认创建”选项。未经正式预检的候选仍由模型提出，不能伪装成已预检快照。只有用户明确要求创建任务，才进入绑定确切参数的最终创建复核。该规则是模型交互指令，不是确定性的自然语言授权解析器；实际创建仍受既有 Runtime 复核指纹、Tool 契约及 owner 权限校验约束。

最终创建复核的提示和确认／取消按钮由 Runtime 统一生成，不采用模型填写的展示文案。提示区分当前条件适配器与 Tool 契约已完成的检查，以及尚未完成的目标类型适配、实际值转换和写入可执行性验证；不把源结构事实当作完整业务值保证。确认仅尝试创建未启动任务，仍由 Transfer 校验权限与配置，不运行任务。展示词条由 Agent i18n 持有，按当前 HTTP `Accept-Language` 传入 Harness；语言与按钮文案不参与参数指纹。普通草稿仍由模型生成，不得宣称未经 owner 证实的可执行性。

用户点击正式“仅创建、不运行”后，Runtime 重读确切草稿引用、当前本体、Manager 源结构与 Meta 目标父节点，再经正式预检核对原指纹。匹配后沿同一创建 Tool 的条件、确认消费和提交屏障调用 owner 一次，并从正式响应生成固定成功提示，不再调用模型决定下一步或收尾。稳定信息变化重新确认同参数；条件失败、依赖失败或结果不明终止，不自动重试。普通创建在接受确认时只从本次复核同协议调用内的唯一已完成预检建立引用，并核对参数、报告与确认身份；已绑定原稿沿用原引用，检查点不复制第二份参数。文字与取消不触发确定性创建。详见交互协议规范 3.4。

断线重连只回放同一 AgentRun 的 `run_events`，并以客户端已处理的 sequence 为游标；事件表不保存通用 Tool 参数事件或原始结果。已预检草稿仅通过受限 `TransferDraftSnapshot` Presentation 保存可展示的精确参数与映射，权限和上限以交互协议规范为准。取消仅停止 Agent Runtime 和 pending Interaction，不取消 owner execution；失败重试在同一 AgentRun 内以新的协议调用 ID 追加事件。

消息 parts 支持：

- `text`；
- `result_ref`；
- `interaction_ref`；
- `presentation_ref`。

ResultRef 只能由 Tool Manifest 的 `result_ref` 声明生成。当前 `execution` 映射只保存 schema、owner、kind 和 `execution:<id>`；Agent 不复制 execution 状态或结果。搜索候选、preview 和 locator 事实没有单一业务结果身份时不得升级为 ResultRef。

AgentRun 可观测事实分为：

- `runs.metrics`：由 step/event 计算的协议调用数、step 数、失败 step 数、事件数、文本字符数和运行时长；不伪造 Provider 未返回的 token usage。
- `runs.context_metrics`：记录本次 Runtime 重建选入、遗漏、截断的消息数和字符预算，不复制消息正文。
- `sessions.summary_message_id`：已压缩的早期消息水位，摘要只合并水位后的新早期消息。

上下文唯一预算为最近 20 条消息、单条 6000 字符、消息总计 24000 字符和摘要 2000 字符，从最新消息向前分配。错误字段唯一使用 `error_source=client|runtime|tool|owner|protocol` + `error_code` + 最多 1000 字符的受限 `error_message`，不保留 `error_type`。

写入和破坏性操作的 approval 不归 Agent 持有。`workflow.run` 的权威审批由 Develop 持有；Agent 不保存完整 workflow payload，只保存 owner approval 的可恢复投影。`ApprovalRequest` 的“检查审批状态”动作只提交 `{action:"check"}`，不能产生批准事实。

## 四、前端边界

Agent 前端使用 Vue 3 和 Element Plus：

- `@ag-ui/client` 负责 AG-UI 请求、SSE 和事件状态；
- `common-frontend/agent-ui` 负责 A2UI MessageProcessor、ADDP Catalog 和 Vue Renderer；
- `MessagePartsRenderer` 负责按顺序渲染消息 parts；
- DAG、地图、表格等继续复用 `common-frontend` 业务组件。

完整 A2UI Surface 不进入 LLM 上下文。客户端遇到未注册组件时拒绝渲染。

Develop execution ResultRef 由前端以当前用户身份调用 owner API 按需加载。AG-UI 传输错误后，Web 客户端只回放同一 AgentRun 的安全事件重建临时界面，不重新提交用户消息或 Tool 调用。

## 五、Tool 与客户端边界

Agent 的 LangChain Tool 只是一种 Tool Adapter。访问 ADDP API 的 Client 统一来自 `common-python/addp_common/client`，Agent 目录不保留模块私有 API Client。

平台级 Skill 只位于仓库根目录 `skills/`。Agent 读取 `agents/addp.yaml`，以 Tool Manifest 的稳定名称建立工具白名单；原私有 Skill 目录已删除，不保留双轨。

## 六、认证

内置 Agent 前端使用 ADDP 现有登录和用户访问令牌。Agent 后端通过 `common-python/addp_common/auth.resolve_authorization_context()` 调用 System `/api/v1/system/auth/context`，不保存 `JWT_SECRET`、不自行解析 Token。原始用户 Token 只用于进入 Runtime 和向 System 申请当前 Tool 调用的短期 Delegated Access Token，不再传给 owner 模块 API。

每次平台 Tool 调用必须绑定当前 AgentRun UUID 和 LangChain `tool_call_id`。System 根据 Tool Manifest 的 owner audience 与稳定 Tool 名 Scope 签发 `addp_dat_`；owner 默认拒绝委托令牌，只在对应 Tool 路由完成 audience、Scope、用户、租户、资源权限和审批校验后放行。

AG-UI 流式请求使用 `common-frontend` 的 `createAuthenticatedFetch()`，与普通 Axios Client 共用 Token 刷新核心；Agent 前端不保存静态请求 Token，也不复制刷新逻辑。

外部 CLI 的 OAuth PKCE / Device Flow 不在 Agent 模块实现。

## 七、相关文档

- `docs/spec/addp智能体Tool开放规范.md`
- `docs/spec/addp智能体交互协议规范.md`
- `docs/spec/addp智能体评测规范.md`
- `docs/skills/addp-Skill规范.md`
- `docs/concepts/addp术语表.md`
- `docs/spec/addp OAuth授权规范.md`
- `docs/spec/addp-API设计规范.md`
- `docs/spec/addp登录认证的统一要求.md`
- `common-python/CLAUDE.md`
- `common-frontend/README.md`
- `docs/next/ADDP智能体能力开放体系专题.md`

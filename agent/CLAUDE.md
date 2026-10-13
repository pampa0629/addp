# Agent 模块

Agent 模块是 ADDP 平台的**自然语言交互入口**，用户通过对话方式完成数据管理、分析和发布操作。

## 模块概述

- **后端**: Python 3.12 + FastAPI + 进程内 DeerFlow Harness（唯一执行内核）
- **前端**: Vue 3 + Element Plus
- **交互协议**: AG-UI + A2UI `addp.catalog/v1`
- **端口**: Backend 8190（开发）| Frontend 5186（开发）
- **数据库**: PostgreSQL `agent` schema

平台级 Tool、交互和评测契约分别以 `docs/spec/addp智能体Tool开放规范.md`、`docs/spec/addp智能体交互协议规范.md`、`docs/spec/addp智能体评测规范.md` 为事实源；本文只记录 Agent 模块实现和运行约束。

平台语义选择：`platform_routed=true` 的任务领域先识别是否需查询目录，再用 `platform.capabilities.list` 读取 Ontology 已激活定义，LLM 根据概念/条件/效果选择 capability，Runtime 核验 owner、Skill 与 Tool 白名单后才进入领域 Harness。不接受模型直接选择该 Skill，不从空目录选择首项，目录失败终止相关 run；普通对话及其他 Skill 不依赖 Ontology。当前仅 Transfer 创建切片启用。`tests/test_platform_routing.py` 用短业务输入验证消费与拒绝边界，是脚本化模型 T1，不宣称真实 LLM 或 T4 已验收。

Transfer 创建条件以运行时 `platform.capability.context` 返回的定义为准，`agents/platform_conditions.py` 只实现有限条件适配，不读取代码 JSON 或复制另一套条件清单。每次具有创建 Tool 的 Harness 调用（包括澄清恢复），Runtime 先通过白名单内的正式 Tool 读取当前能力并保存调用审计与检查点，再交给模型；读取失败终止，不依赖模型记住刷新。模型后续主动刷新也先清空旧上下文，失败不能沿用旧结果。正式 ResourceFacts 刷新替换原结构快照，不保留新响应缺失的旧字段。完整配置复核通过 Runtime 私有 `request_clarification` 的 `transfer_create_review` 完成，指纹绑定确切参数、本体修订/摘要与 owner 事实；变化后重新确认。创建前消费确认，checkpoint 经 AG-UI 消费者事务提交并内部回执后才调用 owner；提交失败或回执前取消不调用 owner，已经发出的调用不承诺撤销。仍只创建 idle/stopped 任务，不执行。详见设计契约 1.13；脚本化门禁覆盖条件、复核绑定和提交屏障，不代替真实对话验收。

Transfer 创建的正式只读预检由 Runtime 在最终复核前及创建前自动通过白名单 `transfer.task.config.check` 执行，使用唯一 Manifest Adapter/SDK，记录同一 Run 的审计。unsupported 阻止创建，unknown 的未检查项与运行要求由固定本地化提示展示并绑定知情确认；supported 不保证执行成功。报告绑定完整 Tool 参数与当前能力/结构事实；仅排除报告自然前进的 observed_at 及其 binding_hash，其他事实变化要求重新确认。失败先清除旧报告、终止当前运行，不吞成 unknown。运行报告保存在受限 checkpoint，不进入平台静态本体图。Copilot 仍只通过可选的 `transfer.draft.generate` 提供草稿，不承担预检、确认或创建职责。

自动预检的 unsupported 报告拒绝复核与创建，同时将 owner 的检查范围、未检查项、运行要求及完整字段诊断作为受限反馈交给当前模型，供解释或修正草稿；不只返回条件错误码，也不传主体及绑定哈希。业务含义变化仍须澄清，新参数重新预检和复核。预检服务失败与无效响应保持终止，不当作可修正的定义拒绝。

Transfer 的行粒度和字段保留范围分开处理：不展开数组不授权删除数组字段。用户要求“检查配置，只给草稿”时，Skill 在参数完整后调用既有正式预检 Tool，Runtime 固定展示结果并结束，不进入创建复核；没有要求检查的普通草稿不自称已预检。该交互指导不声称确定性解析自然语言，最终创建条件与用户复核仍保持原契约。

## 原参数创建（修订 21）

2026-10-13 CI 依赖纠正：修订 21 的实现与本体语义未变，但先前按共享工作区运行 Ontology `tidy`，将未提交 Common 授权事实文件引出的 9 项间接依赖及 129 条校验记录提交到 `main`。CI `38379543401` 的 Go Job 在 Ontology `tidy -diff` 拒绝这些已提交源码未使用的记录；这是本会话的依赖同步处理错误，不是 Provider 修复引入了新契约。已按 `65de8a829` 已提交源码的受控快照收敛依赖，标准 `make test-go` 的 22 个模块及 `make test-go-dependency-policy` 通过。首次快照验证因验证环境的全局 Git 变量干扰临时 Git 夹具失败，改用普通临时仓库后通过，未修改测试断言。该证据不包含共享工作区未提交 Common 文件；相关 owner 后续提交共享源码时须同次同步消费方依赖。没有重启、业务数据操作或新平台修订；本次未重跑真实数据库 T2 或对话 T4，现有 Go workspace 与 Ontology T2 CI 继续承接验证。

2026-10-13 原参数创建已按用户确认的职责契约实现，并冻结为平台 Transfer 修订 21（实现 `1c07a4282`）。唯一路由动作 `draft_action` 区分纯解释和原参数创建，删除 `inspect_draft`；创建候选只来自同用户、租户、会话的受信任草稿引用。检查点仅保存 `draft_ref`，每次确认恢复都重新读取并核验原参数，不保存第二份参数、不导入旧报告、资源事实或确认。绑定原稿时私有复核只提交 Tool 身份，Runtime 用原参数执行预检及复核；正式创建在 Schema 验证、审计和同一 Adapter 前绑定原参数，模型不能改写名称、描述、目标、映射或补可选默认值。当前能力读取、资源事实、两次正式预检、unknown 知情确认、确认消费和提交屏障保持原路径，不放宽权限或支持范围。需要改配置仍走明确修改流程，不在绑定创建内悄悄改参。

本轮 `make test-agent-eval`、`make test-agent-frontend`（69 项单测、7 项受控浏览器及构建）、`make test-platform-review`、`make test-go-dependency-policy`、`make test-ontology-postgres` 和 `make test-ontology-falkor` 通过。回归覆盖原参数复核、模型漂移、恢复重读、引用损坏、歧义、未确认、当前权限拒绝、缺少资源事实及 unsupported 阻止创建；真实 Harness/Manifest/SDK 的受控 HTTP 夹具不创建个人业务任务。Ontology 同步 Common 已引入的 MinIO 间接依赖，版本沿用规约；已提交 Parquet 读取说明经审查属该 native-table 切片之外，只更新来源冻结。完整 Agent 模块门禁首次在公共 T0 因 Meta 权限常量未定义失败；该常量生成后串行复跑越过编译，最终因 System/Meta 新 authorization-facts 接口被 Swagger 覆盖检查判为 stale 而失败，后续 owner 阶段未执行。Ontology 整套模块尝试在公共 Spark 启动夹具 15 秒超时处失败，后续 Agent 串行复跑同项通过，未放宽超时或宣称修复偶发原因；专项通过不替代整套通过。`make test-changed` 因其他 owner 的 T2 连接配置缺失执行前阻断。修订 21 的真实部署创建验收和 Hosted T4 未运行；本轮未运行 Transfer、创建目标表或写入 Outdoor 数据。待公共 Swagger 登记完成并重跑门禁后，由用户重启 Agent/Ontology，再验证原参数创建。

## 目录结构

正式 Transfer 配置预检的固定报告由 Runtime 从已验证 owner 响应产生 `TransferConfigCheckReport` Presentation，显式草稿检查与自动检查共用路径。只投影状态、检查时间、范围和完整诊断，不带主体、绑定哈希或业务数据，不发 action。消息 parts 与安全回放保存同一快照；历史报告不代表当前权限或执行成功。LLM 只解释/修正，服务失败不能生成 unknown 卡片。共享实现与 Transfer 向导共用，现有 Agent eval、共享组件、Agent/Transfer 浏览器与构建门禁覆盖；本次非破坏性 Catalog 扩充不改变既有 props、创建条件或权限。

显式正式预检返回有效三态报告后，由 Runtime 展示固定报告、精确草稿和双语固定完成提示，结束本轮，不再请求模型收尾；同批只保留首个已装配的检查，先于澄清和其他工具。输入错误可修参，服务、权限及无效响应失败仍终止，不伪造 unknown；自动创建预检仍提供完整诊断并沿原复核路径继续。事实概要从原参数直接展示，不新增摘要副本或推测行粒度、物理类型和任务状态；完整引用、映射和参数可展开。后续解释使用既有草稿引用，不复制确认或授权。实时消息、持久 parts 和安全回放按事件顺序组织，不过滤或改写已有文字。生产 Harness 回归核对三态终态、同批隔离、双语提示及自动预检不受影响，真实模型验收仍须重启后进行。

2026-10-13 此调整已冻结为平台 Transfer 修订 20。新增测试先复现旧实现继续调用模型和未过滤同批调用的问题，修复后三态、双语、批次隔离、错误修参、失败和取消回归通过；自动复核及创建前预检保持原路径。完整 `make test-module MODULE=agent`、专项 eval/前端/来源审查与 Ontology PG/FalkorDB 联动门禁通过，前端 69 项单测、7 项受控浏览器及构建通过。共享工作区 `make test-changed` 因其他 owner 的测试连接配置缺失而执行前阻断，不计为通过。新实现的真实模型会话与 Hosted T4 未运行；需用户重启 Agent/Ontology 后另行验证，不以旧会话 30 或受控测试冒充新实现真实验收。

2026-10-13 用户重启后的新会话 31 实际消费修订 20：简短只读请求经三次澄清，选择 Persons、一文档一行并保留全部顶层字段、独立验收目标及 replace。首轮 21 个正式步骤以一次配置预检结束，生成 16 条映射（5 条 array、6 条 json）和 unknown/35 条诊断；正文精确为 Runtime 固定完成提示，没有模型追加收尾。该轮使用可选 `transfer.draft.generate` 辅助构造参数，不代表创建。追问新 Run 仅执行私有 `draft.inspect`，不读取当前平台或业务资源。持久步骤、两轮卡片及安全事件核对原参数、摘要引用、映射和报告投影一致，均无创建复核或创建/执行/样本/查询/扫描 Tool，独立目标对应任务定义数为零。刷新恢复验收未通过：Agent 前端日志出现 Node/Vite 终端输入 `read EIO` 后退出，实际配置的 5186 不再监听，Console `/module-ui/agent/sessions/31` 返回 500；新浏览器页同样空白。未修改启动脚本或擅自重启，已请用户重启 Agent 后仅补验刷新恢复。此记录只证明本地首轮和历史引用切片，不是 Hosted T4、运行成功或整体交互验收通过。

同日用户再次重启 Agent 后，仅刷新原会话 31 补验：模块加载恢复，DOM 中两轮完整参数、同一草稿引用及卡片/文本顺序与原审计证据一致。按同一用户、租户、会话限定的刷新前后只读数据库证据逐字相同，仍只有原草稿和历史解释两个 Run、一次正式预检、零对应任务定义，没有新业务操作。仓库外本地证据核对脚本退出 0，固定收尾、历史引用、消息/安全回放及刷新恢复切片通过；不宣称前端 `read EIO` 根因已修复、配置可以运行或 Hosted T4 已通过。本轮仅补验和记录，未修改运行实现、权限或启动脚本。

同日会话 31 继续输入“按刚才草稿创建任务，不运行”，新创建 Run 重新读取平台能力及当前资源事实，并自动预检、停在正式 unknown 创建复核；没有直接创建。对比同参步骤输入，业务 config 与原稿一致，但模型改写 name/description、增加 batch_size=500。未点击确认；要求恢复原稿后，模型又把任务名恢复与目标表改名混淆，进入多余澄清，创建验收未通过。源码追踪确认 `_load_agent_history` 仅加载文字，领域 TaskContext 只有文字摘要及当前 checkpoint；精确 `draft_reader` 只在 `inspect_draft` 分支消费，非解释路线拒绝 draft_ref。当前创建链路没有恢复原参数，不能把重新取证当作精确复用原稿。限定审计确认无 `transfer.task.create`、运行/扫描/查询/样本 Tool，对应目标任务定义仍为零；保留待澄清状态、不补写配置、不绕过确认。将既有草稿引用作为明确选定的创建候选输入、而非旧授权，需要先确认职责契约再实现；本条只记录缺口，不声称方案已批准或创建成功。

正式预检步骤完成并持久化后，Runtime 从其精确输入与同参报告生成 `AgentDraftRef` 和只读 `TransferDraftSnapshot`，复用共享表格／JSON 展示。新 Run 纯解释已有草稿时，路由只选同用户／租户／会话近期受信任目录中的引用，由私有 Runtime `draft.inspect` 读取历史输入；不装配业务 Tool 或进入领域 Harness，不复制旧确认、checkpoint 或授权。缺失、损坏、参数摘要不符及越界引用失败关闭。候选生成／修改／创建仍走原平台语义、Skill 和 owner 路径。本轮后端 SQL 隔离测试使用内存 SQLite，仅属 T1；前端由既有 Agent Vitest、浏览器及构建门禁自动发现，不声称真实模型或 Hosted T4 已通过。

2026-10-13 用户重启后，会话 29 的简短草稿请求消费本体修订 18，完成源集合、行粒度与目标澄清后仅正式预检一次。固定配置为 16 条映射（11 条 json），报告为 unknown、35 条诊断；后续“解释刚才草稿里数组和对象的保存方式”创建新 Run，仅执行 Runtime `draft.inspect`，无新资源读取、预检或业务 Tool。两轮的完整参数、摘要引用、映射和六项报告投影与原步骤证据一致，消息 parts／安全事件及浏览器刷新恢复一致；未进入创建复核或创建／执行／样本读取／扫描流程。该本地真实联调通过草稿事实引用与历史解释，不等于 Hosted T4、配置可运行或回复简洁性验收：首轮仍重复字段清单、未检查项和运行要求，短答收敛未通过。

2026-10-13 再次重启后的会话 30，Run `8737635c-633f-4d52-909d-3e346f1474e8` 实际消费修订 19，沿同一 Run 完成三次业务澄清、20 个受限步骤和一次正式预检。用户选择一文档一行并整体保留顶层数组/对象；本轮配置为 16 条映射（5 条 array、6 条 json），报告仍为 unknown、35 条诊断，不把目标定义支持等同于源实际类型或逐值转换已核验。历史解释的新 Run `2c75dc4f-0270-4218-a4f4-993732eedcb1` 仅执行 Runtime `draft.inspect`。两轮原参数、摘要引用、映射和六项报告投影一致；首轮持久 parts/安全事件/刷新 DOM 均为报告→草稿→文字，历史解释为固定提示→草稿→报告→解释，完整文字保留。没有创建复核、创建/运行/样本/查询/扫描 Tool，对独立目标名称核对的任务定义数为零。当前环境 `make test-agent-eval` 验证生产 Skill 组合确实进入 Harness，且正文按请求读取、SDK 原样传递 system_prompt；不记录真实提示词全文，也不将此受控证据当作上游模型遵从证明。自由模型仍重复源/目标/策略和范围，默认关键解释未通过；另将 `career.topn` 称为顶层数组，属于澄清文案的层级表述错误。固定事实展示通过不等于整体交互验收通过，后续收尾职责变更需先讨论，不继续仅靠提示词修补。

```
agent/
├── authorization/
│   └── permissions.yaml  # Agent owner Permission Manifest
├── backend/
│   ├── agents/          # Agent 核心逻辑与结构化内部事件
│   ├── protocol/        # AG-UI / A2UI 协议适配
│   ├── services/        # Interaction 等领域服务
│   ├── tools/           # LangChain Tool Adapter
│   ├── models/          # SQLAlchemy 数据模型
│   ├── api/             # FastAPI 路由
│   ├── middleware/      # 认证中间件
│   ├── config.py        # 配置加载
│   ├── database.py      # 数据库连接
│   ├── main.py          # 应用入口
│   └── requirements.txt
└── frontend/
    ├── src/
    │   ├── views/       # ChatView.vue, Login.vue
    │   ├── store/       # auth.js
    │   ├── api/         # index.js
    │   └── router/      # index.js
    ├── package.json
    └── vite.config.js
```

## 快速启动

```bash
# 独立启动 Agent 模块
bash scripts/dev/start.sh -agent

# 重启 Agent 后端
bash scripts/dev/restart.sh -agent

# 前端测试与构建
cd agent/frontend && npm test && npm run build
```

本次 Harness 升级需要全新 Agent/Copilot venv，不能在保留旧 LangChain 0.3 依赖的环境中直接升级 Core。先通过标准停止脚本停止服务，将这两个 venv 移到仓库外备份，再由标准启动脚本安装完整声明。AI 不自行停止或重启用户服务。

## API 接口

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | /health/live | 进程存活检查 |
| GET | /health/ready | 模块就绪检查 |
| GET | /api/v1/agent/sessions | 会话列表 |
| POST | /api/v1/agent/sessions | 创建会话 |
| GET | /api/v1/agent/sessions/:id | 会话详情 |
| DELETE | /api/v1/agent/sessions/:id | 删除会话 |
| GET | /api/v1/agent/sessions/:id/messages | 消息历史 |
| POST | /api/v1/agent/chat | 运行智能体（AG-UI SSE） |
| GET | /api/v1/agent/runs/:id | 查询 AgentRun、语义检查点和步骤审计 |
| GET | /api/v1/agent/runs/:id/events?after=:sequence | 按 sequence 回放安全 AG-UI 事件（SSE） |
| POST | /api/v1/agent/runs/:id/cancel | 取消 Agent Runtime 和 pending Interaction |
| POST | /api/v1/agent/runs/:id/retry | 重试失败 AgentRun（AG-UI SSE） |
| GET | /api/v1/agent/settings/inference-bindings/:scenario_code | 读取 Agent 推理场景绑定 |
| PUT | /api/v1/agent/settings/inference-bindings/:scenario_code | 更新 Agent 推理场景绑定 |

## IAM Permission 所有权

Agent 是以下 Permission 的唯一 owner：

- `agent.session.*`
- `agent.run.*`
- `agent.configuration.*`

机器可读事实源是 [authorization/permissions.yaml](authorization/permissions.yaml)。该 Manifest 由 `common/authorization` 在构建/发布期统一发现、校验和聚合，Agent 服务启动时的 Module Registry 注册和心跳只描述服务可用性，不向 System 动态注册 Permission。

路由与 Permission 语义映射固定如下：

- Session 列表、详情和消息历史使用 `agent.session.read`，创建和删除分别使用 `agent.session.create/delete`。
- `/chat` 是单一 AG-UI Operation，新建和 Interaction resume 统一按 all-of 校验 `agent.run.create + agent.run.execute`；独立的失败重试路由只继续已有 AgentRun，校验 `agent.run.execute`。
- AgentRun 详情和事件回放使用 `agent.run.read`，取消使用 `agent.run.cancel`。

Session 和 AgentRun 当前都只允许所有者访问。Role Permission 只提供功能 Allow 候选，不取代 owner 对 `user_id + tenant_id` 归属的最终资源判断。

`delegable` 当前统一保守为 `false`。Agent 调用其他 owner Tool 时使用对方 owner 的 Tool Scope 和短期 Delegated Access Token，不把 `agent.run.*` 当作业务 Tool 权限。

## 数据库 Schema

```sql
agent.sessions    -- 会话管理、增量历史摘要和摘要水位
agent.messages    -- 对话历史（AG-UI message id + 有序 parts）
agent.runs        -- AgentRun 生命周期、checkpoint、运行/上下文指标与错误归因
agent.run_steps   -- Tool / Runtime 步骤审计、紧凑事实投影与错误归因
agent.run_events  -- 可按 sequence 安全回放的 AG-UI 事件投影
agent.interactions -- 服务端持久澄清与 owner approval 投影
agent.skill_usage -- Skill 使用统计
agent.inference_scenario_bindings -- Agent 场景到 Inference Model Profile 的平台默认和 Tenant 覆盖
```

阶段 5 的 Agent 评测场景唯一目录为仓库根 `evals/agent-scenarios/`，统一使用 `addp.agent-scenario/v1`。离线门禁通过脚本化 Runtime 决策和受控 owner 响应消费真实结构化事件，不调用真实 LLM；定向在线层消费同一契约，凭据和环境私有 ID 不进入 fixture。

定向在线评测唯一入口为 `evals/agent-scenarios/online_runner.py`，只经过生产 `ToolExecutor` 验证真实委托和 Owner API，不调用 LLM。User Access Token 只通过正式 `addp` OAuth 登录和 OS Keychain 中的 Refresh Token 刷新获得，不接受环境变量或命令参数注入；workflow 输入和证据 JSON 必须放在 ADDP 仓库外。示例：

```bash
python evals/agent-scenarios/online_runner.py \
  --output /tmp/addp-read-only-evidence.json \
  read-only --query railway \
  --locator 'addp://engine/8/path/public/railway?type=table&item_id=60'
```

审批场景依次运行 `approval-request`、在 Develop Owner 页面明确批准或拒绝，再运行 `approval-resume` 或 `approval-rejection`；Runner 不自动决定审批。

统一评测门禁唯一入口为 `evals/agent-scenarios/gate.py`。默认运行全部场景契约、Agent 评测与持久化测试、common-python 全量测试和 Agent 前端测试；`--require-online` 额外要求三个黄金场景的仓库外在线证据，并重新使用场景 `evaluate_trace` 校验。报告 Schema 固定为 `addp.agent-evaluation-gate/v2`，报告输出也必须位于仓库外。v2 记录 Git revision、脏工作区标志、场景契约摘要、在线证据摘要和检查耗时，但不得记录证据路径、approval/AgentRun 标识或 trace；历史归档由外部发布流程负责。

```bash
python evals/agent-scenarios/gate.py --output /tmp/addp-agent-evaluation-gate.json
```

仓库离线标准入口为 `make test-agent-eval`；根 `make test` 已包含该门禁。人工发布验收统一使用 `make test-release RELEASE_SUITE=agent-evaluation`，并显式提供 `ADDP_AGENT_READ_ONLY_EVIDENCE`、`ADDP_AGENT_APPROVAL_EVIDENCE`、`ADDP_AGENT_REJECTION_EVIDENCE` 三份仓库外证据；T5 分发器再调用 Agent owner 内部门禁并生成统一发布报告。在线证据固定 24 小时有效，未来时间只容忍 5 分钟时钟偏差。

两份已归档 v2 报告使用 `make compare-agent-eval` 只读比较，并显式提供 `ADDP_AGENT_EVAL_BASELINE`、`ADDP_AGENT_EVAL_CURRENT`。比较结果 Schema 为 `addp.agent-evaluation-comparison/v1`；场景/检查删除、passed 状态退化或当前报告失败才返回回归，契约摘要变化和检查耗时差值只用于审查，不设置阈值。比较不重跑门禁、不读取在线证据，也不保存输入路径或证据摘要。

正式发布基线比较使用 `make compare-agent-eval-release`。baseline 和 current 必须都是显式指定的仓库外 `online_required + passed + worktree_dirty=false` 报告，且普通回归比较通过；否则结果为 `regressed|blocked` 并返回非零。dirty 或 offline 报告仍可用于普通开发比较，但不能成为正式 baseline。归档对象和 baseline 指针由外部发布系统持有，比较器不扫描目录、不选择最新文件、不自动更新 baseline，也不重新按当前时间判定历史在线证据过期。

阶段 5 已封板。当前评测协议固定为 `addp.agent-scenario/v1`、`addp.agent-online-evidence/v1`、`addp.agent-evaluation-gate/v2` 和 `addp.agent-evaluation-comparison/v1`；A2UI 继续使用 `addp.catalog/v1`。破坏性修改必须升级版本并删除旧解析路径。`GraphView`、趋势 UI/阈值、外部归档自动化、MCP Adapter 和多智能体委派只有在专题文档规定的真实场景或外部系统条件满足后才能重新启动。

评测 Schema、门禁、比较和正式发布资格的完整规则见 `docs/spec/addp智能体评测规范.md`；本文不另设评测契约。

`messages` 不再使用 `result_type + result_data`。表现内容通过 `presentation_ref` 引用 A2UI Surface，澄清状态通过 `interaction_ref` 引用 `agent.interactions`。

澄清恢复时，`content` 保留模型需要的受限上下文，`parts` 只展示服务端确认的选项名称；消息历史从当前用户/Tenant 的 Interaction 重建同一展示，不把候选事实和复核指纹作为聊天正文输出。共享 `ClarificationChoice` 默认折叠围栏代码块，完整创建参数可展开查看，确认值和 Runtime 校验不变。

唯一 pending 澄清下，聊天框文字通过同一 `/chat` 的 `resume` 与 `payload={"text":...}` 恢复原 AgentRun，不新建 Run。文字上限 2000 字符；唯一精确匹配采用服务端持久选项，否则仅作为补充要求。复核文字不产生批准并使旧复核失效，owner approval 不接受文字。多 pending 澄清不得猜测回答对象。详见交互协议规范 5.1。

`make test-agent-frontend` 包含独立临时端口的 T3 浏览器回归，加载正式聊天页面、AG-UI 客户端和共享 A2UI 组件，以受控 HTTP/SSE 夹具验证短输入、刷新后的文字 resume、配置复核和取消，以及多 pending 不猜测、失败保留输入。后端 `make test-agent-eval` 覆盖真实 Harness、Interaction 服务和 Run 恢复的受控模型链路，验证同一身份和取消后创建条件拒绝；数据库与 owner 是夹具，不计为真实跨模块 T4。CI 的 Agent 前端矩阵安装 Chromium 并保留失败证据，不复用个人服务或登录态。

## 协议约束

- `/api/v1/agent/chat` 请求体使用标准 AG-UI `RunAgentInput`。
- 响应为 `text/event-stream`，事件使用 AG-UI 编码。
- A2UI 通过 AG-UI Activity 传输，当前 Catalog 为 `addp.catalog/v1`。
- `request_clarification` 是 Agent Runtime 私有的暂停控制能力，不属于平台 Tool Manifest；触发后必须创建持久 Interaction，并以 AG-UI interrupt 和 A2UI `ClarificationChoice` 返回。
- `workflow.run` 返回 `approval_required` 时，Agent 只保存 Develop approval ID、open URL、请求指纹和摘要，以 A2UI `ApprovalRequest` 暂停当前 AgentRun。客户端只能打开 Owner 页面或提交 `{action:"check"}`；Agent 必须使用原始 User Access Token 查询 Develop，只有 `approved|consumed` 才恢复同一 AgentRun。
- `workflow.run` 的完整 workflow payload 不得写入 `agent.interactions`、`agent.run_steps` 或 checkpoint。首次 Tool step 只保存引擎 ID、任务数和是否存在 engine-specific 配置；恢复调用只保存 approval ID 与请求指纹。
- 工作流引擎澄清选项必须来自当前 run 的 `engine.list`；资源澄清 locator 必须来自当前 run 的 `data.search`、`resource.ancestors.get`、`resource.children.list`、`resource.facts.get` 或 `data.preview`。目录 Tool 只投影父节点及直接子节点身份，正式资源事实只保存受限 Schema 和查询信息；具体投影边界以交互协议规范为准。Runtime 使用 owner Tool 事实重建选项，未经观察的候选返回 `clarification_option_not_observed`，不得创建 Interaction。
- AgentRun 跨初始 AG-UI 调用和 Interaction resume 调用存在；恢复身份只使用 Interaction 的 `agent_run_id`，不得按新的协议 `runId` 创建第二个 AgentRun。
- Interaction resume 必须沿用该 AgentRun 已记录的 Skill，不重新交给路由模型选择，避免批准后偏离原 Tool 白名单或退化为直接回复。
- 断线重连按 `agent.run_events` 的 run 内 sequence 回放；事件不保存通用 Tool 参数事件或原始结果。正式预检参数仅通过有界只读 `TransferDraftSnapshot` Presentation 保存展示投影，完整边界见交互协议规范 3.4；不复制主体、权限或批准。取消只停止内置 Agent Runtime 和 pending Interaction，不取消 owner execution；失败重试在同一 AgentRun 中追加新的协议调用事件。
- `agent.run_events` 的 SSE 使用标准 `id` 字段承载 run 内 sequence；客户端用 `after` 回放未处理的安全事件。
- AgentCheckpoint 只保存 owner Tool 紧凑事实和用户已确认选择，不保存模型隐藏推理、框架私有内存、完整样本行或大 Tool 结果。
- 持久化总量门禁固定为：单个 replayable run event 512 KiB、单条 message parts 2 MiB、AgentCheckpoint 256 KiB、单个 step facts 128 KiB；超限直接拒绝，不截取 JSON 前缀或转存旁路字段。
- `run_steps.output_summary` 只保存状态、计数、类型和结果字节数等受限摘要；即使 Tool 结果被截断为非 JSON，也不得回退保存原文前缀。可复用 owner 事实写入 `run_steps.facts` 和 run checkpoint。
- Runtime 上下文从最新消息向前分配：最近 20 条、单条 6000 字符、消息总计 24000 字符、历史摘要 2000 字符。摘要以 `sessions.summary_message_id` 增量推进，不重复压缩全部历史。
- `runs.metrics` 只保存可由 step/event 验证的结构指标，`runs.context_metrics` 只保存预算事实；Provider 没有返回时不估算 token usage。
- Agent 只通过 ADDP Inference Runtime 调用模型。`reasoning` 场景用于路由、Tool Calling 和 ReAct，`general-chat` 用于会话摘要；同一 AgentRun 内的 reasoning Profile 首次解析后固定复用。
- 场景按“Tenant 显式绑定 > 平台默认绑定 > inference_scenario_not_configured”解析，不回退环境变量、任意 Profile 或另一个场景。
- Agent 不保存或读取 Provider、endpoint、上游模型名和 API Key；这些事实只属于 Inference。
- 错误归因使用 `error_source=client|runtime|tool|owner|protocol`、稳定 `error_code` 和最多 1000 字符的受限 `error_message`；不保留 `error_type` 兼容字段。
- `data.search` 的非输入参数错误由平台边界确定性终止 run；同批后续 Tool 不执行，也不再请求模型决定重试或换发现路径。内部 `run_failed` 事件映射为既有 AG-UI `RUN_ERROR`，持久状态为 `failed`，公开失败消息保存到会话历史。`manager_search_isolated` 保持 owner 归因；不修改 Manager 保护状态。成功零召回和输入 Schema 错误仍按 Skill 正常处理。
- ResultRef 由 Tool Manifest 声明驱动；当前 execution 引用只保存 `schema + owner_module + kind + execution:<id>`，不得复制 owner execution 状态或结果。locator 候选没有单一结果身份时不得创建 ResultRef。
- Workflow DAG 只能在 `workflow.validate` 返回 `valid=true` 后生成 A2UI Presentation；draft 未校验或校验失败均不得展示为可用 DAG。
- 不保留 `0:`、`dag:` 或 Vercel AI SDK 兼容流。
- 前端只渲染 Catalog 中注册的组件。

## 共享能力

- ADDP API Client 统一来自 `common-python/addp_common/client`。
- Agent 通过 `common-python/addp_common/auth.resolve_authorization_context()` 消费 System AuthContext，不保存 `JWT_SECRET` 或私有 JWT 解析逻辑。
- 原始 User Access Token 只进入 Agent Runtime 和 System 委托签发接口；每次平台 Tool 使用当前 AgentRun UUID 与 LangChain `tool_call_id` 换取短期 Delegated Access Token，owner Client 不接收原始 Token。
- A2UI Vue Renderer 位于 `common-frontend/agent-ui`。
- 平台级 Skill 唯一目录为仓库根目录 `skills/`，Agent 从 `agents/addp.yaml` 装配 Tool Manifest 中的稳定 Tool。
- Agent Tool Provider 是 `common-python` `ToolExecutor` 的 LangChain 薄 Adapter，不直接调用模块 API Client。

## 前端公开路由

- 模块内聊天首页为 `/`，会话为 `/sessions/:session_id`；Console 公开 URL 分别为 `/agent`、`/agent/sessions/:session_id`。
- 当前会话由 path 参数唯一驱动。点击和创建会话使用 `push`，首次自动选择与删除当前会话后的重定向使用 `replace`。
- `/agent` 旧模块内前缀不再接受；无效或无权访问的会话 ID 保留错误上下文，不得静默切换到其他会话。

## 配置项

在根 `.env` 文件中配置：

```env
# Agent Service Principal；Inference Runtime 由 System Runtime Descriptor 发现
AGENT_SERVICE_CLIENT_SECRET=replace-with-unique-agent-secret-32bytes

# Agent 端口
AGENT_BACKEND_PORT=8190
AGENT_FE_PORT=5186
```

## 日志

- 后端日志: `logs/agent-backend.log`
- 后端错误: `logs/agent-backend-stderr.log`
- 前端日志: `logs/agent-frontend.log`
- LLM 和 Tool 调用日志只记录消息类型、数量、字节数、工具名、参数键、错误类型和 token 用量等元信息；不得记录用户提示词、模型正文、Tool 参数值或 Tool 结果正文，也不提供恢复全文日志的兼容开关。

## 相关文档

- `docs/spec/addp智能体Tool开放规范.md`
- `docs/spec/addp智能体交互协议规范.md`
- `docs/spec/addp智能体评测规范.md`
- `docs/skills/addp-Skill规范.md`
- `docs/spec/addp OAuth授权规范.md`
- `agent/docs/agent模块设计共识.md`
- `docs/next/ADDP智能体能力开放体系专题.md`

# ADDP 智能体评测规范

回答阶段的私有 `answer_ready` 在运行步骤中仍完整审计；离线轨迹单列 `runtime_actions`（名称、完成状态和错误），不将其伪装为平台 Manifest Tool 或业务 read 风险。场景可声明 `required_runtime_actions` 并精确比较动作顺序，已启动动作必须具有终态；公开 Tool 的风险、输入字段、顺序及 owner 副作用断言保持原检查。

更新日期：2026-11-03

状态：正式规范。智能体场景、在线证据、统一门禁、报告比较和正式发布评测基线以本文为准。

## 一、目标与边界

评测用于验证 ADDP Agent 在稳定 Skill、Tool、Interaction、ResultRef、Presentation、错误和 owner 副作用上的行为，不逐字匹配模型回答，也不把模型主观评分作为发布事实。

现有场景、在线证据、门禁和比较分别使用以下唯一 Schema：

- `addp.agent-scenario/v1`；
- `addp.agent-online-evidence/v1`；
- `addp.agent-evaluation-gate/v2`；
- `addp.agent-evaluation-comparison/v1`。

真实模型回归另输出 `addp.agent-model-regression/v1`，只证明模型在固定事实下的结构化行为，不作为上述正式发布基线或主观质量评分。

旧 Schema 不兼容读取。在线证据、报告和比较结果必须位于 ADDP 仓库外；凭据、环境私有 ID 和完整业务 payload 不得进入 fixture 或评测报告。

## 二、评测分层

### 2.1 离线层

离线评测使用脚本化 Runtime 决策和受控 owner 响应消费真实结构化事件，不调用真实 LLM，不访问在线 owner API。它验证：

- 场景契约严格加载；
- Skill 与 Tool 选择；
- 澄清、审批、拒绝和越权路径；
- AgentRun、Interaction、ResultRef、Presentation 和持久化边界；
- common-python ToolExecutor 与前端协议组件。

离线层是开发和根 `make test` 的稳定门禁，但不能单独成为正式发布评测基线。

DeerFlow Harness 回归使用真实内核、共享 Inference Adapter 与 Manifest Tool Adapter，模型／owner 响应为受控夹具；覆盖暂停批次、取消传播、白名单、完整结果和租户隔离。Agent 标准门禁可通过 `ADDP_AGENT_PYTHON` 指定仓库外 Python 3.12 测试环境，默认仍使用模块 venv；该解释器同时用于 Agent 与共享 Python 回归，不向运行中的开发环境安装依赖。

### 2.2 定向在线层

在线评测唯一入口为 `evals/agent-scenarios/online_runner.py`。Runner 经过生产 ToolExecutor、System 委托和 owner API 验证真实路径，不调用 LLM，不自动登录，不自动决定审批，也不启动或重启 ADDP 服务。

User Access Token 只能通过正式 `addp` OAuth 登录保存在 OS Keychain 中的 Refresh Token 刷新获得；Runner 不接受 `ADDP_TOKEN`、命令参数或其他手工 Token 注入。workflow 输入、阶段性审批证据和最终证据都必须显式指定仓库外路径。

### 2.3 真实模型回归

唯一实现为 `evals/agent-scenarios/model_runner.py`，公共入口为 `make test-agent-model`，由现有薄脚本映射。它调用生产 `stream_agent_response`、当前 Skill、Manifest Adapter 和 DeerFlow Harness；模型通过 Agent 现有 Inference Service Principal 和当前 Tenant 的 `reasoning` 场景绑定解析，不接入 Provider、不指定另一套模型配置、不修改绑定。Tenant 由 `ADDP_AGENT_MODEL_TENANT_ID` 显式指定；缺失凭据、绑定、服务或超时均失败，不能回退脚本模型或记为通过。

工具边界固定为受控事实：唯一替换点是 ToolExecutor 调用，不发起真实 Owner 请求。每次调用先核对 Manifest、场景白名单、参数 Schema 和必要参数值；所有 write Tool、未声明 Tool 和错误参数均记录为回归并阻断，不因模型自行修正而消除失败。保留生产 Tool Schema 与 Skill 白名单，不靠隐藏写工具或提示词保证安全。不持久化 AgentRun、Interaction 或业务实体；历史草稿使用经生产快照校验器生成的合成输入，不读取真实用户草稿。

首批场景覆盖引擎只读查询、查询候选草稿、传输配置预检和历史草稿说明。沿用 `addp.agent-scenario/v1` 与 `evaluate_trace`，不新增场景 DSL。固定事实和精确参数断言由同目录的模型夹具提供；验证路由、必需与禁止工具、无多余澄清、参数保真、原报告投影及非空真实正文。正文只在内存中检查必要的稳定事实，不逐字比较措辞、不调用模型自评，也不把关键词通过宣称为整体语义质量通过。

路由诊断只从实际模型响应投影 `draft_action`、注册 Skill、是否需要平台目录及是否返回草稿引用；禁止保存原引用或自由文本依据。该投影只用于定位回归，不改变或替代生产路由决定。

报告严格输出源码身份、场景/夹具契约摘要、实际 Skill、工具名称与状态、断言布尔值、稳定错误码及实际耗时/首正文耗时。模型身份只保存实际响应的 deployment 摘要和 profile_version，不保存 Provider 配置。不保存输入输出全文、Tool 参数值、原始结果、历史草稿引用、Tenant ID、凭据或异常原文。报告必须在仓库外原子写入；失败同样生成报告并返回非零。不设置耗时阈值、综合分或自动重跑至通过。每次显式执行保留全部样本，个例修复前后的失败证据不能覆盖。

该层需要真实 Inference 与只读场景绑定查询，属于显式运行的模型集成回归，不是无外部依赖的 T1，也不是生产 Owner 或完整 T4。现有 Agent 离线 CI 自动运行其契约、安全和编排单测，不自动消费在线模型凭据；真实页面验收另行核对真实资源、报告与回答，只允许查询、草稿和预检，不创建或运行业务任务。自由文本事实正确性和表达质量仍需人工复核，结构化通过不能替代该结论。

## 三、场景契约

### 3.1 目录与 Schema

场景唯一目录为：

```text
evals/agent-scenarios/<scenario-name>/scenario.yaml
```

`addp.agent-scenario/v1` 顶层字段固定为 `schema`、`name`、`category`、`skill`、`prompt`、`expectations`，未知字段必须拒绝。断言只使用稳定协议事实，不依赖自然语言措辞、框架内部类名、临时数据库 ID 或固定环境资源。

### 3.2 当前场景

| 场景 | 类型 | 核心断言 |
| --- | --- | --- |
| `read-only-query` | 在线黄金场景 | 只调用允许的 read Tool；使用观测到的 locator；无审批、无写副作用。 |
| `approval-execution` | 在线黄金场景 | 首次 write 返回 owner approval；同一 AgentRun 批准后恢复；只创建一次 execution。 |
| `rejection-and-forbidden` | 在线黄金场景 | 拒绝后不执行；跨 AgentRun 重放返回 `approval_forbidden` 且不泄露终态。 |
| `railway-farmland-area` | 离线端到端场景 | `workflow-analysis` 的检索、澄清、预览、生成、校验、审批、执行和引用链路。 |
| `model-engine-inventory` | 真实模型回归 | 普通引擎查询不误入本体目录，保留离线引擎，不请求多余澄清。 |
| `model-query-draft` | 真实模型回归 | 保留当前引擎、语言及资源事实，生成查询候选但不执行。 |
| `model-transfer-precheck` | 真实模型回归 | 经平台语义路由按完整原参数预检，不创建或运行，不改写正式报告。 |
| `model-historical-draft` | 真实模型回归 | 无 Skill 路由（契约标记 `none`），仅私有 `draft.inspect`，不重新读取资源或预检。 |

增加场景必须解决新的稳定风险或用户路径；不能为单次环境数据复制一个场景 DSL。

## 四、在线证据

`addp.agent-online-evidence/v1` 顶层字段固定为：

```text
schema, scenario, skill, created_at, approval, trace
```

只读场景的 `approval` 必须为 null；审批和拒绝场景的阶段性证据包含 `agent_run_id`、`approval_id`、`request_fingerprint`、`open_url`，仅用于人工跨步骤继续在线验收。最终门禁不会把这些字段复制到报告。

证据禁止包含：

- Authorization、Token 或 Delegated Access Token；
- workflow definition、engine-specific 完整配置或 sample rows；
- 账号、密码、Cookie 或其他认证材料。

在线证据创建后 24 小时内有效，未来时间只容忍 5 分钟时钟偏差。无时区、无法解析、过期或超出未来偏差均失败，不提供放宽参数。

Runner 写文件必须使用临时文件后原子替换。审批场景固定为显式两段：先 `approval-request`，用户在 Develop 页面作出决定，再运行 `approval-resume` 或 `approval-rejection`。自动点击批准不属于评测基础设施职责。

## 五、统一门禁

### 5.1 唯一入口

统一实现为 `evals/agent-scenarios/gate.py`，薄脚本 `scripts/test/agent-evaluation-gate.sh` 和 Makefile 只映射参数，不实现第二套评测逻辑。

公共标准入口为：

```bash
make test-agent-eval
make test-agent-model
make test-release RELEASE_SUITE=agent-evaluation
make compare-agent-eval
make compare-agent-eval-release
```

`test-release` 分发器调用 Agent owner 内部的发布门禁并生成统一 T5 报告；owner 目标不作为第二个公共入口。

### 5.2 离线门禁

离线验收同时包含 `make test-agent-eval` 与 `make test-agent-frontend`。前者必须发现并严格加载全部场景，并运行：

1. 场景契约检查；
2. Agent 后端全量测试（包括评测、协议、持久化和模型回调日志边界）；
3. common-python 全量测试。

`make test-agent-frontend` 单独验证 Agent 前端测试与构建；它的执行结果不伪装成后端评测报告中的检查项。

根 `make test` 必须依赖上述两个目标。任一后端场景或检查失败时，评测报告 `status=failed` 且进程返回非零；前端门禁失败时，其标准入口返回非零。

### 5.3 发布门禁

`make test-release RELEASE_SUITE=agent-evaluation` 在同一离线门禁上，要求三个黄金场景分别通过以下显式环境变量提供仓库外证据：

```text
ADDP_AGENT_READ_ONLY_EVIDENCE
ADDP_AGENT_APPROVAL_EVIDENCE
ADDP_AGENT_REJECTION_EVIDENCE
```

门禁重新使用对应 scenario 的 `evaluate_trace` 校验证据，不信任证据自报状态，不扫描临时目录，不猜测最新文件。报告输出由 `ADDP_AGENT_EVAL_REPORT` 指定；未指定时仍写操作系统临时目录。

## 六、门禁报告

`addp.agent-evaluation-gate/v2` 只保存可归档安全事实：

- `created_at`、`run_id`、`mode=offline|online_required`；
- `source.revision` 和 `source.worktree_dirty`；
- 检查名称、状态、耗时和退出码或场景计数；
- 场景名称、Skill、契约 SHA-256、离线/在线状态；
- 已通过在线证据的规范化创建时间与文件 SHA-256；
- 总状态和稳定失败原因。

报告不得保存证据路径、trace、approval 上下文、AgentRun ID、approval ID、请求指纹、Owner URL、Tool 原始结果或 Token。字段必须严格校验，未知字段和 v1 报告直接拒绝。

`worktree_dirty=true` 是有效开发报告事实，不使普通门禁自动失败；但该报告不能成为正式发布评测基线。

## 七、报告比较

### 7.1 普通比较

`make compare-agent-eval` 只读比较两份显式指定的仓库外 v2 报告：

```text
ADDP_AGENT_EVAL_BASELINE
ADDP_AGENT_EVAL_CURRENT
```

两份报告的 mode 必须相同。比较不重跑门禁、不读取在线证据、不访问 OAuth 或 Tool、不选择目录中的最新文件，也不修改输入报告。

以下情况是回归：

- baseline 中的场景或检查在 current 中被删除；
- baseline 已 passed 的离线、在线或检查状态退化；
- current 报告自身为 failed。

新增场景或检查、契约摘要变化、证据变化和耗时变化只作为审查信息，不设置评分或阈值。

### 7.2 比较报告

`addp.agent-evaluation-comparison/v1` 记录比较身份、模式、策略、两侧安全源码身份、结构变化、状态变化、耗时差值、release readiness、回归列表和最终状态。它不得复制输入路径、证据摘要、证据时间或审批上下文。

状态固定为：

- `passed`：没有回归，且请求的策略条件满足；
- `regressed`：存在普通比较回归；
- `blocked`：没有普通回归，但 release 策略条件不满足。

## 八、正式发布评测基线

`make compare-agent-eval-release` 使用 release 策略。baseline 和 current 都必须满足：

```text
mode = online_required
status = passed
source.worktree_dirty = false
```

同时普通回归比较必须通过。任何一侧 dirty、offline 或 failed，或 current 存在回归，都不能得到 `release_readiness.eligible=true`。

正式 baseline 的接受、不可变归档、保留期和指针更新由外部发布系统持有。Agent 仓库不扫描归档目录、不自动选择 baseline、不更新 baseline 指针，也不在比较历史报告时重新按当前时间判定其在线证据过期。

当前工作区存在未提交改动时，生成的报告只能用于开发验证，不能称为正式 baseline。

## 九、报告安全与文件边界

1. 证据、门禁报告和比较报告必须位于仓库外。
2. 输出使用临时文件后原子替换，不维护共享 JSONL 或仓库内历史目录。
3. 所有输入报告按精确字段严格加载，不能忽略未知字段。
4. 禁止字段安全扫描必须递归覆盖对象和数组。
5. Git revision 必须是当前门禁读取到的 40 位 revision；脏状态必须如实记录。
6. 归档系统不得把原始在线证据当作公开评测报告。

## 十、变更规则

破坏性修改必须升级对应 Schema，并在同一轮删除旧解析、旧 fixture 和旧文档路径。场景、Tool 或交互协议变化时，应先更新对应正式规范，再更新契约和自动化。

以下能力当前明确不属于已实现评测基线：

- 真实 LLM 质量评分或排行榜；
- 趋势 UI、耗时阈值或虚构综合分；
- 外部归档产品实现；
- MCP Adapter 消费场景；
- 多智能体委派和隔离场景。

只有真实需求和稳定事实出现后，才可先修订规范再实施。

## 十一、验证

最低验证：

```bash
make test-agent-eval
```

发布门禁和比较按需要使用四个标准 Make 入口，并显式提供所需仓库外输入。正式发布资格必须通过 `compare-agent-eval-release` 验证，不能用一次 dirty 工作区的发布门禁结果替代。

真实模型回归显式运行：

```bash
ADDP_AGENT_MODEL_TENANT_ID=<当前验收租户ID> \
POSTGRES_PORT=<Infra状态显示的实际端口> \
ADDP_AGENT_EVAL_REPORT=/tmp/addp-agent-model-<本次唯一标识>.json \
make test-agent-model
```

复用根 `.env` 与 `.dev-state/ports.env` 的运行配置，实际 Infra 端口先用 `bash scripts/infra/status.sh` 核对。该入口只读取现有配置，不启动或重启服务，也不更改测试数据库。确定性单测不加载个人 `.env` 或运行配置。

`ADDP_AGENT_MODEL_REPEATS=1..3` 可显式重复整套场景，默认一次，所有样本均写入同一新报告，任一样本失败则本轮失败；不允许筛选成功样本或覆盖已有文件。

## 十二、相关文档

- `docs/concepts/addp术语表.md`
- `docs/spec/addp智能体Tool开放规范.md`
- `docs/spec/addp智能体交互协议规范.md`
- `docs/skills/addp-Skill规范.md`
- `docs/spec/addp OAuth授权规范.md`
- `agent/CLAUDE.md`

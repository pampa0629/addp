# ADDP 智能体评测规范

更新日期：2026-07-31

状态：正式规范。智能体场景、在线证据、统一门禁、报告比较和正式发布评测基线以本文为准。

## 一、目标与边界

评测用于验证 ADDP Agent 在稳定 Skill、Tool、Interaction、ResultRef、Presentation、错误和 owner 副作用上的行为，不逐字匹配模型回答，也不把模型主观评分作为发布事实。

本文规定当前四个唯一 Schema：

- `addp.agent-scenario/v1`；
- `addp.agent-online-evidence/v1`；
- `addp.agent-evaluation-gate/v2`；
- `addp.agent-evaluation-comparison/v1`。

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

### 2.2 定向在线层

在线评测唯一入口为 `evals/agent-scenarios/online_runner.py`。Runner 经过生产 ToolExecutor、System 委托和 owner API 验证真实路径，不调用 LLM，不自动登录，不自动决定审批，也不启动或重启 ADDP 服务。

User Access Token 只能通过正式 `addp` OAuth 登录保存在 OS Keychain 中的 Refresh Token 刷新获得；Runner 不接受 `ADDP_TOKEN`、命令参数或其他手工 Token 注入。workflow 输入、阶段性审批证据和最终证据都必须显式指定仓库外路径。

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

四个标准入口为：

```bash
make test-agent-eval
make test-agent-eval-release
make compare-agent-eval
make compare-agent-eval-release
```

### 5.2 离线门禁

`make test-agent-eval` 必须发现并严格加载全部场景，并运行：

1. 场景契约检查；
2. Agent 评测、协议与持久化测试；
3. common-python 全量测试；
4. Agent 前端测试。

根 `make test` 必须依赖此目标。任一场景或检查失败时，报告 `status=failed` 且进程返回非零。

### 5.3 发布门禁

`make test-agent-eval-release` 在同一离线门禁上，要求三个黄金场景分别通过以下显式环境变量提供仓库外证据：

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

## 十二、相关文档

- `docs/concepts/addp术语表.md`
- `docs/spec/addp智能体Tool开放规范.md`
- `docs/spec/addp智能体交互协议规范.md`
- `docs/skills/addp-Skill规范.md`
- `docs/spec/addp OAuth授权规范.md`
- `agent/CLAUDE.md`

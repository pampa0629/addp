# ADDP Skill 规范

本文定义 ADDP 面向不同 Agent Runtime 的平台级 Skill 规范。Skill 与 ADDP 内置 Agent 解耦，同一个 Skill 应能被 ADDP Agent、Codex、Hermes Agent 等运行时消费。

Tool 契约、交互协议和评测分别见 `docs/spec/addp智能体Tool开放规范.md`、`docs/spec/addp智能体交互协议规范.md`、`docs/spec/addp智能体评测规范.md`。专题文档只保留架构决策和实施历史。

## 一、Skill 定位

ADDP Skill 是面向一类可复用任务的知识与工作方法包，负责说明：

- 何时使用；
- 需要哪些 ADDP Tool；
- 如何分步骤完成任务；
- 何时需要澄清或审批；
- 如何验证结果；
- 哪些行为属于反模式。

Skill 不执行 ADDP 业务逻辑。真实操作统一通过 Tool Adapter、Python SDK 和 owner 模块正式 API 完成。

`workflow-analysis` 是合适的 Skill；“铁路占耕地面积计算”是该 Skill 的评测场景，不得建立为独立 Skill。

## 二、唯一目录

平台级 Skill 的目标落点为仓库根目录 `skills/`：

```text
skills/
├── workflow-analysis/
│   ├── SKILL.md
│   ├── agents/
│   │   ├── addp.yaml
│   │   └── openai.yaml
│   ├── references/
│   ├── scripts/
│   └── assets/
├── data-discovery/
│   ├── SKILL.md
│   └── agents/addp.yaml
├── query-generation/
│   ├── SKILL.md
│   └── agents/addp.yaml
├── notebook-generation/
│   ├── SKILL.md
│   └── agents/addp.yaml
└── transfer-generation/
    ├── SKILL.md
    └── agents/addp.yaml
```

平台级 Skill 只位于根目录 `skills/`。原 `agent/backend/skills/` 已删除，不保留运行时私有 Skill 事实源。

每个 Skill 是独立目录，目录名与 `SKILL.md` front matter 中的 `name` 一致，使用小写连字符命名。

## 三、目录内容

### 3.1 `SKILL.md`

Skill 的唯一入口，必须包含元数据、触发边界、执行步骤、验证方式和反模式。

### 3.2 `references/`

保存只有执行特定步骤时才需要读取的资料，例如：

- 领域参数说明；
- ADDP 公开契约摘要；
- 数据质量检查说明；
- 算子选择指南。

reference 不能复制正式规范全文。稳定平台概念仍以 `docs/concepts/` 和 `docs/spec/` 为事实源。

`agents/` 保存宿主装配元数据。`openai.yaml` 服务 Codex 展示与触发，`addp.yaml` 只声明 ADDP Runtime 所需 Tool 和迭代上限；二者不得复制正文。

`agents/addp.yaml` 可以声明 `required_skills` 组合其他 Skill 的方法正文。组合只影响 Runtime 注入的指导，不继承依赖 Skill 的 Tool 权限；每个 Skill 必须独立声明自己的最小 `required_tools` 白名单。

### 3.3 `scripts/`

保存 Skill 私有的确定性处理脚本，例如：

- 输入规范化；
- 输出字段校验；
- workflow definition 静态检查；
- 报告或图表生成。

脚本不得：

- 自行实现 ADDP HTTP 认证和 token 刷新；
- 直接访问 ADDP 数据库；
- 复制 owner 模块业务规则；
- 自行拼接 ResourceLocator；
- 绕过 SDK 或 CLI 调用高风险接口；
- 在多个 Skill 中复制相同 API Client。

需要访问 ADDP 时，Python 脚本使用共享 SDK，其他脚本使用 `addp` CLI。

### 3.4 `assets/`

保存模板、示例或静态展示资源。不要把某次评测的输入数据放入 Skill；评测 fixture 归 `evals/agent-scenarios/`。

## 四、`SKILL.md` 元数据

最小 front matter：

```yaml
---
name: workflow-analysis
description: 设计、校验和执行可复用的数据分析工作流；需要组合算子、澄清输入或解释 DAG 时使用。
---
```

`SKILL.md` front matter 只使用 Agent Skills 的公共字段 `name` 和 `description`。ADDP Runtime 专用的 Tool 依赖放入 `agents/addp.yaml`，避免通用 Skill 入口携带宿主私有字段：

```yaml
schema: addp.skill-runtime/v1
required_skills:
  - data-discovery
required_tools:
  - data.search
  - data.preview
  - workflow.operators.list
  - workflow.validate
  - workflow.run
  - execution.get
max_iterations: 8
```

约束：

- `name` 全局唯一，与目录名一致。
- `description` 同时说明能力和触发时机。
- `agents/addp.yaml` 只保存 ADDP Runtime 装配信息，不复制 Skill 正文或 Tool Schema。
- `required_skills` 只用于组合方法正文，必须引用仓库中存在的 Skill，不能形成循环依赖，也不继承依赖 Skill 的 Tool 权限。
- `required_tools` 只引用 Tool Manifest 中的稳定名称。
- 风险等级不写在 Skill 元数据中；风险属于具体 Tool 和本次操作。
- 不在通用 front matter 中写 Agent Framework 私有工具名。
- Codex 等宿主需要展示元数据时，可以增加宿主专用文件，但不能改变 Skill 主体语义。

## 五、正文结构

推荐结构：

```markdown
# 工作流分析

说明 Skill 的目标和边界。

## 使用条件

- 用户需要组合多个算子完成分析。
- 用户需要生成、解释、校验或执行 workflow definition。

## 不使用的情况

- 用户只需要浏览单个数据项。
- 用户已经给出 SQL 且只需执行只读查询。

## 前置检查

1. 确认输入数据身份使用 locator。
2. 确认目标 Workflow Runtime 和公开算子能力。
3. 缺少关键信息时创建 clarification，不猜测字段名或参数。

## 执行步骤

1. 调用 `data.search` 查找候选数据。
2. 调用 `data.preview` 检查字段与能力。
3. 调用 `workflow.operators.list` 取得公开算子契约。
4. 生成并调用 `workflow.validate` 校验候选 workflow。
5. 展示 DAG 和关键参数。
6. 需要执行时完成服务端审批并调用 `workflow.run`。
7. 调用 `execution.get` 跟踪结果。

## 验证

- workflow definition 通过正式校验。
- 不存在猜测的 locator、字段名或内部连接参数。
- 写操作具备有效审批。

## 反模式

- 不根据常见习惯假定空间字段名是 `geom`。
- 不绕过 Tool 调用 owner 模块私有接口。
- 不把完整地图、表格或 A2UI Surface 放入模型上下文。
```

Skill 正文不强制角色扮演段落。只有当角色视角能改变专业判断时才使用，不能以冗长人设替代操作规则。

## 六、渐进式加载

Skill 必须支持渐进式加载：

1. 发现阶段只读取 `name`、`description` 和位置。
2. 命中 Skill 后读取完整 `SKILL.md`。
3. 执行步骤明确需要时再读取指定 reference、script 或 asset。
4. 脚本执行不要求先把源码完整放入模型上下文。
5. Tool 结果只返回下一步所需摘要和稳定引用。

不得在 Agent 启动时把所有 Skill 正文、reference 和 Tool Schema 一次性放入 system prompt。

## 七、Skill 粒度与命名

一个 Skill 应覆盖稳定、可复用的能力域。

合适示例：

- `workflow-analysis`；
- `data-discovery`；
- `data-preview`；
- `sql-analysis`；
- `knowledge-graph-exploration`；
- `transfer-generation`。

不合适示例：

- `railway-farmland-analysis`；
- `project-a-monthly-report`；
- `analyse-table-123`；
- 只有一次调用且没有可复用步骤的薄包装。

判断标准：

1. 更换数据集后方法是否仍成立。
2. 更换 Agent Runtime 后 Skill 是否仍可使用。
3. 是否存在多个需要顺序、澄清或验证的步骤。
4. 是否包含值得复用的领域判断或反模式。

若答案主要是否定的，应把内容放入评测场景、用户任务或 Tool 文档，而不是新增 Skill。

## 八、Tool 引用规则

正文统一使用 Tool Manifest 的稳定名称，例如：

```text
`data.search`
`data.preview`
`workflow.validate`
`workflow.run`
```

不要引用：

- `langchain_tools.py` 中的函数名；
- 某个 Agent Runtime 自动生成的工具 ID；
- SDK 内部方法路径；
- HTTP URL；
- MCP 展开后的 server 前缀。

不同 Adapter 负责把稳定 Tool 名称映射为 SDK、CLI 或 MCP 调用。

## 九、澄清、审批与输出

### 9.1 澄清

缺少会改变结果的输入时，Skill 必须要求创建 clarification，例如：

- 候选数据项不唯一；
- 目标坐标系未确定；
- 分析距离或单位未确定；
- 输出位置未确定。

不得用默认猜测掩盖关键业务选择。

### 9.2 审批

Skill 可以说明何时需要审批，但不能自行把操作判定为已审批。审批状态由 owner 模块服务端 Interaction 决定。

### 9.3 输出

Skill 应优先返回：

- 简短文本摘要；
- ResultRef；
- execution id；
- locator；
- PresentationRef 或 `open_url`。

不得把无界表格、完整地图要素、大型文件或完整 A2UI 组件树放入模型上下文。

## 十、评测

Skill 的典型业务例子放入独立评测目录：

```text
evals/agent-scenarios/
└── railway-farmland-area/
    ├── scenario.yaml
    ├── expected-behavior.md
    └── fixtures/
```

评测至少检查：

- 是否选择正确 Skill；
- 是否调用允许的 Tool；
- 是否在必要时澄清；
- 是否避免猜测 locator、字段和单位；
- 是否经过正式 workflow 校验；
- 写操作是否经过服务端审批；
- 输出是否使用稳定引用并控制上下文大小。

所有场景统一使用 `addp.agent-scenario/v1`。评测器只断言稳定 Skill、Tool、Interaction、错误码、AgentRun 关系、ResultRef 和 owner 副作用，不逐字匹配模型回答。普通门禁使用不调用真实 LLM 的离线确定性轨迹；真实环境定向复验消费同一契约，不保存账号、Token 或环境私有 ID。

## 十一、验证清单

新增或修改 Skill 时至少验证：

1. front matter 可以被解析。
2. `name` 与目录名一致且全局唯一。
3. `required_tools` 全部存在于 Tool Manifest。
4. 正文没有 Agent Framework 私有路径或工具名。
5. 没有把单次业务案例误建为 Skill。
6. reference 和 script 只在需要时加载。
7. 至少有一个正向评测和一个关键反模式评测。

## 十二、相关文档

- `docs/concepts/addp术语表.md`
- `docs/spec/addp智能体Tool开放规范.md`
- `docs/spec/addp智能体交互协议规范.md`
- `docs/spec/addp智能体评测规范.md`
- `docs/spec/addp-API设计规范.md`
- `docs/spec/addp登录认证的统一要求.md`
- `common-python/CLAUDE.md`
- `docs/next/ADDP智能体能力开放体系专题.md`

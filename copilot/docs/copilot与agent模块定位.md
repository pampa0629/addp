# Copilot 与 Agent 模块定位

## 两个模块的本质差异

表面上两者都是"Python + HTTP 调用各模块 API"，但它们解决的问题不同：

| 维度 | Copilot | Agent |
|------|---------|-------|
| 触发方式 | 用户在某模块 UI 内主动呼唤 | 用户跳出所有模块，直接对话 |
| 上下文 | 富上下文（已知模块、数据源） | 贫上下文（只有自然语言） |
| 能力深度 | 窄而深（共享资源确认 + 领域生成、验证与修复） | 宽而浅（理解意图、调度能力） |
| 定位 | 模块内的 AI 加速器 | 跨模块的 AI 调度员 |

两者的定位是**互补**的，不是冗余的。

## Copilot：模块内的 AI 加速器

Copilot 嵌入在各模块的 UI 中，帮助用户在特定上下文中加速完成操作。

**核心特征**：
- 用户已经在某个模块里，有明确的操作上下文（当前引擎、数据源、正在编辑的内容）
- 提供深度的领域能力；各生成服务消费已确认事实，输入资源的解析与确认按 Query、Workflow、Notebook、Transfer 的入口职责完成
- 每个模块的 AI 助手能力独立演化，可针对该模块的业务逻辑深度优化

**典型场景**：
- 用户在 Develop 工作流编辑器中，描述分析需求，Copilot 生成完整的工作流 JSON
- 用户在 Develop 查询工作台中，用自然语言描述查询意图，Copilot 按当前引擎 capability 生成 SQL、MQL 或 Cypher 等候选查询
- 用户在 Develop Notebook 编辑器中，用自然语言描述分析需求，Copilot 在当前 Notebook Session 数据范围内确认数据源并生成 Python/GeoPandas 单元
- 用户在 Transfer 向导中，确认单一源和目标边界后，让 Copilot 生成待复核的传输任务草稿
- （规划中）用户在 Meta 模块中，Copilot 根据字段名和样本数据自动填写元数据描述

## Agent：跨模块的 AI 调度员

Agent 提供独立的自然语言入口，用户无需进入任何具体模块，直接通过对话完成 ADDP 的各类操作。

**核心特征**：
- 独立界面，极简入口，实时展示结果
- 需要自行理解用户意图，并决定调用哪些模块的哪些能力
- 负责跨模块的流程编排，例如"导入数据 → 扫描元数据 → 发布服务"

**典型场景**：
- "帮我把这个 Shapefile 导入，扫描元数据，然后发布成 WFS 服务"
- "查一下最近上传的数据里有没有包含人口字段的表"

## 调用关系

两者分离，但 Agent 可以调用 Copilot 作为高级工具：

```
用户自然语言
     ↓
  Agent（意图理解 + 跨模块调度）
     ├── 查询、预览、列表等操作 → 通过正式 Tool 调用业务 owner
     └── 需要 AI 生成 → 通过正式 Tool 调用 Copilot 生成服务
                              ↓
                     Copilot（领域专家）
                              ↓
                        各模块 API
```

**这样分层的收益**：
- Agent 通过正式 Tool 复用 Copilot 的领域生成；资源发现与确认仍遵循各入口的授权和上下文范围
- Copilot 新增模块级 AI 能力后，须通过正式 Tool 契约和 Skill 装配开放给 Agent，不因新增接口就自动取得调用权限
- 两者独立演化，互不干扰

## 注意事项

平台级 Skill 的唯一事实源是仓库根目录 `skills/`。`data-discovery` 提供共享资源发现方法，`query-generation`、`workflow-analysis`、`notebook-generation` 和 `transfer-generation` 通过 `required_skills` 组合它；组合只继承方法正文，不继承 Tool 权限，每个领域 Skill 必须独立声明最小 Tool 白名单。Copilot 是固定领域服务，不复制或私有化这些 Skill，而是遵守相同的资源与生成契约。

Agent 调用 Copilot 时按 `data-discovery` 确认输入：已有 locator 时执行 `resource.ancestors.get → resource.facts.get`；仅有业务描述时，在该场景允许的范围内搜索候选，再确认身份和结构事实；已有明确容器范围时，按目录发现契约枚举候选。必要时基于已验证事实做语义排序，收集并确认资源事实后传入 `resources[]`。`data.preview` 仅用于明确查看样本的请求，不承担生成前的结构事实确认。Query、Workflow 的普通用户入口可由 Copilot 的 `ResourceResolutionService` 完成资源确认；Notebook 的候选范围及事实由 Develop 当前 Session 提供。Transfer 的资源发现和源、目标校验由 Agent Owner Tool 或 Transfer 向导完成，Copilot 只提取源意图或消费上下文生成草稿，不访问资源或申请二次委托。各入口共享 Tool Manifest、ToolExecutor、Python SDK 和 `ResourceFact`，不复制 HTTP Client 或 owner 业务逻辑。

查询生成还必须携带当前查询工作台的 `engine_id` 和 `query_language`，并可携带编辑器已有的 `current_query`。普通原生查询的搜索限定当前引擎；联邦查询的 Source Engine 范围由当前 Runtime capability 声明，不套用工作流的全租户发现规则。MongoDB database 是 Develop 的执行范围，不进入 `resources[]`。已有 MQL 时，Develop 必须解析主 collection 及关联 collection，在当前 database 的 Owner Catalog 中确认具体资源后提交；`current_query` 不能替代字段事实。编辑器为空且已有 database 范围时，使用 `resource_scope_locator`，由 Copilot 通过 `resource.children.list → resource.facts.get` 返回真实候选供确认，不改用模糊搜索。生成结果只是候选，执行仍归 Develop。

## Skill 与生成知识的归属

Copilot 当前不加载这四个完整任务 Skill。任务 Skill 负责调用前后如何组织任务，Copilot 的生成提示词负责当前固定步骤的模型输入和响应格式；确定性契约必须由代码校验。知识归属和提炼条件统一遵循 [ADDP Skill 规范](../../docs/skills/addp-Skill规范.md)，根目录 `skills/` 属于平台共享知识资产，不迁入语言代码库。

| Skill | 任务方法 | Copilot 生成职责及核对结论 |
| --- | --- | --- |
| `query-generation` | 当前 Runtime、输入资源确认、澄清及候选交付 | SQL/Cypher 规划与候选校验，MQL 强类型计划与确定性编译；结构确认统一使用 `resource.facts.get`，不读取样本代替字段事实 |
| `workflow-analysis` | Runtime 选择、资源确认、正式校验、审批及 execution 跟踪 | 消费公开算子契约生成并修复 DAG；参数名和写入模式来自 Public Operator Spec，不固定为 `write_mode` 或一律要求保存输出 |
| `notebook-generation` | Session 范围、候选选择、确认后插入且不执行 | 生成原生表门面的 Pandas/GeoPandas 单元并校验；本场景不生成 `sql(...)` 查询 |
| `transfer-generation` | 平台语义、源和目标确认、复核及创建未启动任务 | 只提取源意图或生成名称、描述和字段映射补丁；不发现资源、不创建或启动任务 |

资源身份、字段、CRS 和执行边界在两层中重复提醒，是各自入口对同一契约的约束；固定响应格式、MQL 编译规则和原生门面参数仍由 Copilot 的实现拥有。当前没有需要由两个生成器共同维护的独立生成任务，不新增生成 Skill、共享提示词加载器或第二套方法正文。

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
- 提供深度的领域能力；输入资源解析与确认由共享 `ResourceResolutionService` 完成，各领域生成服务只消费已确认事实
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
     ├── 简单操作 → 直接调用模块 API（查询、预览、列表等 CRUD）
     └── 需要AI生成 → 调用 Copilot API（工作流生成、查询生成、元数据填写等）
                              ↓
                     Copilot（领域专家）
                              ↓
                        各模块 API
```

**这样分层的收益**：
- Agent 不需要重新实现资源确认和领域生成流程，直接复用 Copilot 的成果
- Copilot 未来新增的模块级 AI 能力（meta 助手、transfer 映射等），Agent 自动获得
- 两者独立演化，互不干扰

## 注意事项

平台级 Skill 的唯一事实源是仓库根目录 `skills/`。`data-discovery` 提供共享资源发现方法，`query-generation`、`workflow-analysis`、`notebook-generation` 和 `transfer-generation` 通过 `required_skills` 组合它；组合只继承方法正文，不继承 Tool 权限，每个领域 Skill 必须独立声明最小 Tool 白名单。Copilot 是固定领域服务，不复制或私有化这些 Skill，而是遵守相同的资源与生成契约。

Agent 调用 Copilot 时按 `data-discovery` 对每个输入做跨语言粗筛召回，再执行 `resource.ancestors.get` 和 `data.preview`，必要时基于已验证事实做语义排序，收集并确认资源事实后传入 `resources[]`。Develop、Workflow 和 Transfer 的普通用户入口可以由 Copilot 的 `ResourceResolutionService` 完成同一流程；Notebook 只消费当前 Session 候选，不获得租户级 `data.search`。两条入口共享 Tool Manifest、ToolExecutor、Python SDK、`ResourceFact` 和资源解析规则，不复制 HTTP Client 或 owner 业务逻辑。

查询生成还必须携带当前查询工作台的 `engine_id` 和 `query_language`，并可携带编辑器已有的 `current_query`。Agent 与 Develop 前端都只能调用带 `engine_id` 过滤的 `data.search`，候选 locator 必须属于该引擎；工作流生成的全租户多引擎发现不能复用于查询工作台。MongoDB database 是 Develop 的执行范围，不进入 `resources[]`；当合法 MQL `current_query` 已声明主 collection 时，`query.draft.generate` 可以在无资源事实的情况下基于现有命令生成。生成结果只是候选文本，执行仍归 Develop。

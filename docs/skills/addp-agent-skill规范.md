# ADDP Agent Skill 规范

本文档说明 ADDP Agent 模块的 Skill 编写规范，格式参考 Claude Code Skills 标准，并结合 ADDP 平台的特点。

## 目录结构

每个 Skill 是一个**独立目录**，目录名即 skill 的唯一标识（连字符分隔），目录内必须包含 `SKILL.md` 文件：

```
agent/backend/skills/
├── data-browse/
│   └── SKILL.md
├── data-preview/
│   └── SKILL.md
├── execute-sql/
│   └── SKILL.md
└── metadata-search/
    └── SKILL.md
```

> 目录内可以有其他辅助文件（如 prompt 模板、示例数据等），但 `SKILL.md` 是唯一的入口。

---

## SKILL.md 格式规范

### 1. Front Matter（必须）

```yaml
---
name: skill-name           # 技能唯一标识符，与目录名一致，连字符分隔
description: "单行描述：说明这个技能做什么，以及何时应该使用它"
---
```

- `name`：全局唯一，与目录名保持一致
- `description`：一句话，供 Agent 用于 skill 匹配/路由判断

### 2. 正文结构

#### 标题 + 角色定义（必须）

```markdown
# 技能标题

**角色（Role）**: 角色名称

一段话说明这个技能的视角和专业能力，定义 AI 在使用该技能时的思维方式。
```

#### 能力清单（推荐）

```markdown
## 能力（Capabilities）

- 能力描述1
- 能力描述2
- 能力描述3
```

#### 触发条件（必须）

```markdown
## 何时使用（Use this skill when）

- 用户说"..."、"..."时
- 用户需要...时
- 触发关键词：xxx、yyy、zzz
```

#### 排除条件（推荐）

```markdown
## 不使用的情况（Do not use this skill when）

- 当...时（应改用 xxx skill）
- 当...时（这不是该技能的职责范围）
```

#### 操作模式/模式（必须）

每个 Pattern 描述一种典型的操作场景，明确说明调用哪些 Tool：

```markdown
## 操作模式（Patterns）

### 模式名称

简要描述这个模式解决什么问题。

**使用时机**: 当用户...

1. 调用 `tool_name(param=...)` 获取/执行...
2. 根据结果...
3. 向用户展示...

```

> **重要**：Tool 引用格式为反引号包裹的函数调用形式，如 `` `list_engines()` ``、`` `execute_sql(engine_id=..., sql=...)` ``。

#### 反模式（推荐）

```markdown
## 反模式（Anti-Patterns）

### ❌ 反模式名称

**问题所在**: 说明为什么这样做不对。

**正确做法**: 应该如何处理。
```

#### 关联技能（推荐）

```markdown
## 关联技能（Related Skills）

Works with: `skill-a`, `skill-b`, `skill-c`
```

---

## 完整示例

```markdown
---
name: execute-sql
description: "在 ADDP 管理的存储引擎上执行 SQL 查询。用户说'查询数据'、'执行SQL'、'查一下...'时使用。"
---

# SQL 查询执行

**角色（Role）**: ADDP 数据分析师

你是 ADDP 平台的 SQL 数据分析师。你知道如何在各种存储引擎上安全地执行查询，帮助用户从数据中提取价值。你只执行只读查询，保护数据安全。

## 能力（Capabilities）

- 在 PostgreSQL、MySQL 等引擎上执行 SELECT 查询
- 帮助用户构造 SQL（如果用户描述的是业务需求而非 SQL 语句）
- 以表格形式展示查询结果
- 解释查询结果的含义

## 何时使用（Use this skill when）

- 用户说"执行 SQL"、"查询数据"、"运行这个语句"
- 用户描述数据分析需求（"统计每个城市的人口"）
- 用户直接提供了 SQL 语句需要执行

## 不使用的情况（Do not use this skill when）

- 用户只想浏览数据结构（使用 `data-browse` 或 `data-preview` skill）
- 目标引擎是对象存储（MinIO/S3 不支持 SQL）

## 操作模式（Patterns）

### 执行用户提供的 SQL

用户已给出 SQL 语句，需要指定引擎执行。

**使用时机**: 用户直接提供了 SQL 语句

1. 如果用户未指定引擎，调用 `list_engines()` 列出可用引擎，让用户选择
2. 调用 `execute_sql(engine_id=..., sql="SELECT ...")` 执行
3. 以 Markdown 表格展示结果，注明返回行数

### 根据描述构造并执行 SQL

用户描述了业务需求，需要 AI 构造 SQL。

**使用时机**: 用户描述"帮我查..."、"统计..."等需求

1. 调用 `list_engines()` 了解可用引擎
2. 如需了解表结构，调用 `preview_data(object_id=...)` 查看字段名
3. 构造 SQL 并告知用户："我将执行以下 SQL：`SELECT ...`"
4. 调用 `execute_sql(engine_id=..., sql=...)` 执行
5. 展示结果

## 反模式（Anti-Patterns）

### ❌ 执行写操作

**问题所在**: INSERT/UPDATE/DELETE/DROP 会修改数据，超出 AI 助手的权限边界，存在数据安全风险。

**正确做法**: 明确告知用户 AI 只执行只读查询，并建议用户通过 ADDP 开发工作台执行修改操作。

### ❌ 不确认引擎就执行

**问题所在**: 在多引擎环境中，不同引擎有不同的数据，直接猜测可能查错数据源。

**正确做法**: 总是先确认目标引擎（通过用户指定或 `list_engines()` 列出后让用户选择）。

## 关联技能（Related Skills）

Works with: `data-browse`, `data-preview`, `metadata-search`
```

---

## 加载机制（两阶段按需加载）

Skill 采用两阶段加载，避免将所有 skill 正文一次性塞入 system prompt（浪费 token、干扰 LLM 判断）。

### 阶段一：启动时加载元数据（轻量）

Agent 后端启动时，扫描 `agent/backend/skills/` 目录，**只解析每个 `SKILL.md` 的 front matter**（`name` + `description`），构建 skill 注册表，并将 skill 清单注入 system prompt：

```
## 可用技能

- `data-browse`: 数据浏览技能：...
- `execute-sql`: 在 ADDP 管理的存储引擎上执行 SQL 查询...
- ...
```

### 阶段二：请求时路由 + 按需加载正文

每次用户发送消息时，执行两步：

1. **Skill 路由**：用一次独立的 LLM 调用，根据用户最新消息从 skill 清单中选出需要激活的 skill（或 `none`）
2. **按需读取正文**：若路由结果非 `none`，读取对应 `SKILL.md` 的正文，追加到本次请求的 system prompt 的"当前激活技能"部分

```python
# agent/backend/agents/main_agent.py

# 阶段一：启动时
registry = _load_skill_registry()   # 只含 name + description

# 阶段二：每次请求
skill_name = await _route_skill(user_message)   # 一次 LLM 调用
if skill_name:
    skill_body = registry[skill_name].load_body()   # 按需读取正文
```

**优点**：
- 正常请求的 system prompt 只含 skill 清单（几十 token），不含完整正文
- 激活某个 skill 后，只有该 skill 的正文被注入，精准且省 token
- 新增 skill 无需改代码，扫描目录自动发现

---

## 设计原则

1. **一个技能一个目录**：技能之间界限清晰，便于独立维护和扩展
2. **Role 先行**：每个技能都有明确的角色定位，让 LLM 快速进入状态
3. **Tool 引用明确**：在 Patterns 中明确说明调用哪个 tool，参数是什么
4. **触发条件具体**：提供具体的用户话语示例，不要只写抽象描述
5. **反模式不可省**：避免 LLM 做出错误决策（如执行写操作）

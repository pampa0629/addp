# Agent 模块设计共识

**文档版本**: v0.1
**创建日期**: 2026-03-24
**状态**: 设计阶段 - 待实施

## 文档目的

本文档记录 ADDP Agent 模块在设计初期达成的关键共识,作为后续详细设计和实施的指导原则。

---

## 1. 模块定位与目标

### 1.1 核心定位

Agent 模块是 ADDP 平台的**自然语言交互入口**,为最终用户提供对话式的数据平台操作体验。

### 1.2 核心目标

- 降低使用门槛:用户通过自然语言完成数据管理、分析、发布等操作
- 提升效率:封装常见操作流程为 Skill,减少重复操作
- 智能辅助:根据上下文提供建议和自动化操作
- 自我成长:积累领域知识,逐步提升能力(远期目标)

### 1.3 与其他模块的关系

- **独立模块**:Agent 作为独立的微服务模块,有自己的前后端
- **Console 集成**:在 Console 中提供**优先入口**(如顶部按钮或侧边栏)
- **API 调用**:通过 HTTP API 调用其他模块(Manager、Meta、Develop、Service、Transfer 等)
- **统一认证**:使用 JWT 认证,与其他模块保持一致

---

## 2. 技术栈选择

### 2.1 后端技术栈

| 技术 | 选择 | 理由 |
|------|------|------|
| **语言** | Python 3.11+ | AI 生态成熟,LangGraph 支持 |
| **框架** | FastAPI | 异步支持,性能好,文档自动生成 |
| **多智能体框架** | LangGraph | 状态图模型,适合复杂流程编排 |
| **HTTP 客户端** | httpx | 异步支持,API 简洁 |
| **LLM 调用** | 可配置 | 支持 OpenAI、Azure、Anthropic、Ollama 等 |

### 2.2 前端技术栈

| 技术 | 选择 | 理由 |
|------|------|------|
| **框架** | Vue 3 | 与 ADDP 其他模块保持一致 |
| **对话 SDK** | Vercel AI SDK (`@ai-sdk/vue`) | 流式输出,Vue 3 原生支持 |
| **组件复用** | ADDP 现有组件 | 复用 `common-frontend` 的组件 |
| **低代码方案** | 暂不引入 Amis | 保持技术栈简单,后续按需评估 |

### 2.3 基础设施

| 组件 | 用途 |
|------|------|
| **PostgreSQL** | 存储会话、消息、Skill 定义 (agent schema) |
| **Redis** | 缓存热会话、Skill 索引 |
| **MinIO** | 存储生成的文件、图表等 |

---

## 3. 架构设计

### 3.1 智能体架构:单智能体 vs 多智能体

**阶段 1 (MVP):单智能体 + 多 Skill**

```
MainAgent (唯一智能体)
  ├─ 意图识别 (LLM)
  ├─ Skill 匹配 (向量搜索或 LLM)
  ├─ Skill 执行
  │   ├─ 数据管理类 Skill (上传、预览、搜索)
  │   ├─ 元数据类 Skill (扫描、查询、血缘)
  │   ├─ 开发类 Skill (SQL、工作流、Notebook)
  │   ├─ 服务类 Skill (发布、配置、监控)
  │   └─ 传输类 Skill (导入、导出、同步)
  └─ Tool 调用
      ├─ manager_client
      ├─ meta_client
      ├─ develop_client
      ├─ service_client
      └─ transfer_client
```

**决策理由**:
- ADDP 的大部分任务是**单领域的**,不需要跨智能体协作
- 单智能体架构**简单、易调试、快速验证**
- 避免过早引入多智能体的复杂度(路由、状态传递、协调)

**阶段 2 (按需演进):多智能体**

只有在以下情况下才考虑引入多智能体:
1. Skill 数量爆炸(超过 50 个),单智能体难以管理
2. 跨领域协作频繁(如"导入 → 分析 → 发布"的完整流程)
3. 需要并行处理(如同时监控多个任务)
4. 需要专家系统(如"SQL 优化专家"、"空间分析专家")

**待探索问题**:
- "导入 → 分析 → 发布"这类跨模块流程,是否需要多智能体?
- 还是用单智能体 + 多个 Skill 串联即可?
- 需要在实践中验证

### 3.2 三层架构:Agent → Skill → Tool

```
┌─────────────────────────────────────────┐
│           MainAgent (智能体)             │
│  - 理解用户意图                          │
│  - 匹配最相关的 Skill                    │
│  - 读取 Skill 文档并执行                 │
│  - 处理异常和多轮对话                    │
└─────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────┐
│         Skills (知识文档 .md)            │
│  - Markdown + YAML frontmatter          │
│  - 描述适用场景和执行步骤                │
│  - 引用需要调用的 Tools                  │
│  - 可选 resources/ 目录存放详细手册      │
│  例: import_shapefile.md                 │
└─────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────┐
│         Tools (Python API 封装)          │
│  - 一对一映射 ADDP HTTP API              │
│  - 参数转换和错误处理                    │
│  例: manager_client.upload_file()        │
└─────────────────────────────────────────┘
```

**职责边界**:

| 层级 | 职责 | 格式 | 示例 |
|------|------|------|------|
| **Agent** | 意图理解、Skill 匹配、执行协调 | Python 代码 | 理解"上传 Shapefile"→ 匹配 import_shapefile.md |
| **Skill** | 领域知识、操作步骤、最佳实践 | Markdown 文档 | import_shapefile.md 描述如何导入空间数据 |
| **Tool** | API 封装、参数转换 | Python 函数 | upload_file(engine_id, file_path) |

---

## 4. 数据流与结果渲染

### 4.1 完整数据流

```
用户输入 (自然语言)
  ↓
MainAgent 理解意图 (LLM)
  ↓
匹配最相关的 Skill (向量搜索或 LLM)
  ↓
读取 Skill 文档 (Markdown)
  ↓
根据 Skill 指导调用 Tools (Python API)
  ↓
返回结构化结果 (JSON)
  ↓
前端渲染 (Vue 组件)
```

### 4.2 结果 JSON 结构

```typescript
interface AgentResult {
  type: 'text' | 'table' | 'chart' | 'map' | 'form' | 'dag' | 'image' | 'video' | 'file' | 'error'
  data: any  // 根据 type 不同而不同
  metadata?: {
    title?: string
    description?: string
    actions?: Action[]  // 可执行的后续操作
  }
}
```

### 4.3 前端渲染策略

- **组件映射**:根据 `result.type` 动态选择 Vue 组件
- **复用现有组件**:直接使用 `common-frontend` 的 TablePreview、GeoJsonPreview、ImagePreview 等
- **智能展示**:不只返回 JSON,而是根据数据类型选择最佳展示方式

**示例**:
```vue
<component :is="getComponent(result.type)" :data="result.data" />
```

---

## 5. 会话管理

### 5.1 会话存储

**数据库设计** (PostgreSQL `agent` schema):
- `sessions` 表:会话元信息(用户、租户、标题、时间)
- `messages` 表:对话历史(角色、内容、结构化结果)
- `skill_usage` 表:Skill 使用统计(名称、使用次数、成功率)

**缓存策略** (Redis):
- 热会话数据缓存(TTL 1 小时)
- Skill 索引缓存(加速匹配)

### 5.2 多会话支持

- 用户可以创建多个会话(类似 ChatGPT 的对话列表)
- 每个会话独立的上下文和历史
- 支持会话重命名、删除、归档

---

## 6. 认证与安全

### 6.1 认证方式

- 使用 **JWT 认证**,与 ADDP 其他模块保持一致
- 用户在 Console 登录后,token 传递给 Agent 前端
- Agent 后端验证 token,提取 user_id 和 tenant_id

### 6.2 权限控制

- Agent 调用其他模块 API 时,携带用户的 JWT token
- 权限检查由各模块的后端负责(Agent 不做额外权限控制)
- 确保用户只能操作自己有权限的资源

---

## 7. LLM 配置

### 7.1 可配置的 LLM Provider
通过 `.env` 配置,支持多种 LLM，暂用现有的，看一下是否需要调整配置信息


### 7.2 LLM 调用场景

- **意图识别**:理解用户想做什么
- **Skill 匹配**:选择最合适的 Skill(也可用向量搜索)
- **参数提取**:从用户输入中提取 Skill 所需参数
- **结果总结**:将 API 返回的数据转换为用户友好的描述

---

## 8. Skill 自我成长(远期目标)

### 8.1 Skill 文档格式

参考 Claude Code Skills 的格式,每个 Skill 包含:

```markdown
---
name: skill_name
description: "简短描述"
---

# Skill 标题

详细说明...

## Use this skill when
- 适用场景 1
- 适用场景 2

## Do not use this skill when
- 不适用场景 1
- 不适用场景 2

## Context
背景说明和目标

## Requirements
$ARGUMENTS (用户输入的参数)

## Instructions
- 步骤 1: 调用 Tool xxx
- 步骤 2: 调用 Tool yyy
- 步骤 3: 返回结果

## Resources
- `resources/implementation-playbook.md` 详细实施手册
```

**目录结构规范**:
```
agent/backend/skills/
├── import-data/               # 数据导入（支持所有格式）
│   ├── SKILL.md
│   └── resources/
│       └── format-guide.md    # 各种格式的处理说明
├── preview-data/              # 数据预览
│   └── SKILL.md
├── scan-metadata/             # 元数据扫描
│   └── SKILL.md
├── query-data/                # 数据查询（SQL）
│   └── SKILL.md
├── create-workflow/           # 创建工作流
│   └── SKILL.md
├── publish-service/           # 发布数据服务
│   └── SKILL.md
└── export-data/               # 数据导出
    └── SKILL.md
```

**命名规范**:
- Skill 目录名使用 kebab-case (小写 + 连字符)
- 主文档必须命名为 `SKILL.md`
- resources 目录可选，存放详细实施手册
- **Skill 粒度要适中**：不要过细（每种格式一个），也不要过粗（所有功能一个）

### 8.2 阶段规划

**阶段 1 (MVP):手动创建 Skill**
- 开发者编写 Markdown 文档
- 放到 `agent/backend/skills/` 目录
- Agent 启动时加载并索引

**阶段 2 (按需):半自动生成 Skill**
- 用户完成复杂操作后,Agent 询问:"是否保存为快捷操作?"
- 用户确认后,Agent 生成 Skill Markdown 文档
- **人工审核**后才启用

**阶段 3 (远期):全自动成长**
- Agent 自动识别高频操作
- 自动生成、测试、启用 Skill
- 定期清理低频 Skill

### 8.3 待解决问题

- 如何判断"值得保存为 Skill"?(频率?复杂度?)
- 如何避免 Skill 爆炸?(去重、合并、版本管理)
- 如何保证生成的 Skill 质量?(测试、人工审核)

---

## 9. 目录结构(草案)

```
agent/
├── backend/              # Python 后端
│   ├── agents/          # 智能体实现
│   │   └── main_agent.py
│   ├── skills/          # Skill 文档目录
│   │   ├── import-data/
│   │   │   ├── SKILL.md
│   │   │   └── resources/
│   │   │       └── format-guide.md
│   │   ├── preview-data/
│   │   │   └── SKILL.md
│   │   ├── scan-metadata/
│   │   │   └── SKILL.md
│   │   ├── query-data/
│   │   │   └── SKILL.md
│   │   ├── create-workflow/
│   │   │   └── SKILL.md
│   │   ├── publish-service/
│   │   │   └── SKILL.md
│   │   └── export-data/
│   │       └── SKILL.md
│   ├── tools/           # ADDP API 封装 (Python)
│   │   ├── manager_client.py
│   │   ├── meta_client.py
│   │   ├── develop_client.py
│   │   ├── service_client.py
│   │   └── transfer_client.py
│   ├── graph/           # LangGraph 状态图
│   │   └── main_graph.py
│   ├── models/          # 数据模型
│   │   ├── session.py
│   │   ├── message.py
│   │   └── skill_usage.py
│   ├── api/             # FastAPI 接口
│   │   ├── chat.py
│   │   ├── session.py
│   │   └── skill.py
│   ├── middleware/      # 中间件
│   │   └── auth.py
│   ├── config.py        # 配置加载
│   └── main.py          # 应用入口
├── frontend/            # Vue 3 前端
│   ├── src/
│   │   ├── components/
│   │   │   ├── ChatInput.vue
│   │   │   ├── MessageList.vue
│   │   │   ├── SessionList.vue
│   │   │   └── ResultRenderer.vue  # 智能渲染组件
│   │   ├── views/
│   │   │   └── AgentChat.vue
│   │   ├── api/
│   │   │   └── agent.js
│   │   └── main.js
│   ├── package.json
│   └── vite.config.js
├── CLAUDE.md            # Agent 模块详细文档
├── docker-compose.yml   # Agent 服务定义
└── README.md
```

---

## 10. 下一步行动

### 10.1 待明确的规范

在开始详细设计前,需要明确以下规范:
- ADDP 模块开发规范(目录结构、命名约定)
- API 设计规范(请求/响应格式、错误处理)
- 数据库设计规范(表命名、字段类型)
- 前端开发规范(组件结构、状态管理)
- 配置管理规范(.env 变量命名)

### 10.2 MVP 范围

**最小可行产品应包含**:
1. 单智能体 + 3-5 个基础 Skill
2. 对接 Manager 模块(上传、预览)
3. 简单的对话界面(输入框 + 消息列表)
4. 基础的结果渲染(文本、表格、地图)
5. 会话管理(创建、切换、历史记录)

**验证目标**:
- 核心流程可行性:自然语言 → 意图识别 → API 调用 → 结果展示
- 技术栈适配性:LangGraph + FastAPI + Vue 3 + Vercel AI SDK
- 用户体验:对话是否流畅,结果是否清晰

### 10.3 待探索问题

1. **跨模块流程**:"导入 → 分析 → 发布"是否需要多智能体?
2. **Skill 粒度**:多细的操作应该封装为 Skill?
3. **意图识别准确率**:如何提升意图识别的准确性?
4. **错误处理**:API 调用失败时,如何友好地提示用户?
5. **性能优化**:LLM 调用延迟如何优化?(缓存、流式输出)

---

## 11. 附录:关键决策记录

| 决策点 | 选择 | 理由 | 日期 |
|--------|------|------|------|
| 前端技术 | Vercel AI SDK | Vue 3 原生支持,流式输出 | 2026-03-24 |
| 组件复用 | 直接复用 ADDP 组件 | 保持一致性,降低复杂度 | 2026-03-24 |
| 智能体架构 | 单智能体 + 多 Skill | 简单、易调试、快速验证 | 2026-03-24 |
| 低代码方案 | 暂不引入 Amis | 避免技术栈复杂化 | 2026-03-24 |
| 模块定位 | 独立模块 + Console 集成 | 架构清晰,用户体验好 | 2026-03-24 |
| 认证方式 | JWT(与其他模块一致) | 统一认证体系 | 2026-03-24 |
| LLM 配置 | .env 可配置 | 灵活支持多种 Provider | 2026-03-24 |

---

**文档维护**:
- 本文档随设计演进持续更新
- 重大决策变更需记录原因和日期
- 实施过程中的新发现及时补充到"待探索问题"

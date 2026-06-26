# Agent 模块开发分步计划

## 背景与目标

### 模块定位
Agent 模块是 ADDP 平台的**自然语言交互入口**,为用户提供对话式的数据平台操作体验。

### 核心目标
- 降低使用门槛：用户通过自然语言完成数据管理、分析、发布等操作
- 提升效率：封装常见操作流程为 Skill，减少重复操作
- 智能辅助：根据上下文提供建议和自动化操作

### 技术栈选择
- **后端**: Python 3.11+ + FastAPI + LangGraph
- **前端**: Vue 3 + Vercel AI SDK
- **基础设施**: PostgreSQL (agent schema) + Redis + MinIO

---

## 阶段一：基础架构搭建

### 1.1 目录结构创建

创建符合 ADDP 规范的模块目录结构：

```
agent/
├── backend/              # Python 后端
│   ├── agents/          # 智能体实现
│   ├── skills/          # Skill 文档目录
│   ├── tools/           # ADDP API 封装
│   ├── graph/           # LangGraph 状态图
│   ├── models/          # 数据模型
│   ├── api/             # FastAPI 接口
│   ├── middleware/      # 中间件
│   ├── config.py        # 配置加载
│   ├── main.py          # 应用入口
│   └── requirements.txt # Python 依赖
├── frontend/            # Vue 3 前端
│   ├── src/
│   │   ├── components/
│   │   ├── views/
│   │   ├── api/
│   │   └── main.js
│   ├── package.json
│   └── vite.config.js
├── docs/                # 模块文档
│   └── Agent模块开发分步计划.md
├── CLAUDE.md            # Agent 模块详细文档
└── README.md
```

### 1.2 端口分配

根据 `docs/spec/addp端口分配.md` 规范，Agent 模块端口分配：

- **Backend 开发端口**: 8087 (已被 Copilot 占用，需要调整)
- **Backend Docker 端口**: 8087
- **Frontend 开发端口**: 5186 (新分配)
- **Frontend Docker 端口**: 8116 (新分配)

**注意**: 当前 8087 端口已被 Copilot 模块占用，建议：
- Agent Backend: 8190 (开发) / 8190 (Docker)
- Agent Frontend: 5186 (开发) / 8116 (Docker)

### 1.3 数据库 Schema 设计

在 PostgreSQL 中创建 `agent` schema，包含以下表：

**sessions 表** - 会话管理
```sql
CREATE TABLE agent.sessions (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL,
    tenant_id INTEGER NOT NULL,
    title VARCHAR(255),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

**messages 表** - 对话历史
```sql
CREATE TABLE agent.messages (
    id SERIAL PRIMARY KEY,
    session_id INTEGER NOT NULL REFERENCES agent.sessions(id) ON DELETE CASCADE,
    role VARCHAR(50) NOT NULL,  -- 'user' | 'assistant' | 'system'
    content TEXT NOT NULL,
    result_type VARCHAR(50),    -- 'text' | 'table' | 'chart' | 'map' | 'error'
    result_data JSONB,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

**skill_usage 表** - Skill 使用统计
```sql
CREATE TABLE agent.skill_usage (
    id SERIAL PRIMARY KEY,
    skill_name VARCHAR(255) NOT NULL,
    user_id INTEGER NOT NULL,
    tenant_id INTEGER NOT NULL,
    success BOOLEAN DEFAULT TRUE,
    execution_time_ms INTEGER,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

---

## 阶段二：后端开发

### 2.1 配置管理

创建 `backend/config.py`，遵循 ADDP 配置规范：

**关键要求**：
- 使用 `common/config.LoadEnv()` 加载环境变量（如果需要与 Go 服务集成）
- 支持从 System 服务获取共享配置（JWT_SECRET、数据库连接）
- 支持降级到本地 .env 配置

**配置项**：
```python
# Agent 模块特有配置
AGENT_BACKEND_PORT = 8190
AGENT_DB_SCHEMA = "agent"

# LLM 配置
LLM_PROVIDER = "openai"  # openai | azure | anthropic | ollama
LLM_API_KEY = "..."
LLM_MODEL = "gpt-4"

# System 服务配置
SYSTEM_URL = "http://localhost:8180"
ENABLE_SERVICE_INTEGRATION = True

# Redis 配置
REDIS_HOST = "localhost"
REDIS_PORT = 16379
REDIS_PASSWORD = "..."

# PostgreSQL 配置（从 System 获取或本地 .env）
DB_HOST = "localhost"
DB_PORT = 15432
DB_USER = "addp"
DB_PASSWORD = "..."
DB_NAME = "addp"
```

### 2.2 数据模型定义

创建 `backend/models/` 目录，定义数据模型：

- `session.py` - Session 模型
- `message.py` - Message 模型
- `skill_usage.py` - SkillUsage 模型

使用 SQLAlchemy ORM，确保在代码中自动创建 schema。

### 2.3 Tools 层实现

创建 `backend/tools/` 目录，封装 ADDP 各模块的 HTTP API：

**必需的 Tools**：
- `manager_client.py` - Manager 模块 API 封装（上传、预览、目录管理）
- `meta_client.py` - Meta 模块 API 封装（元数据扫描、查询）
- `develop_client.py` - Develop 模块 API 封装（SQL 执行、工作流）
- `service_client.py` - Service 模块 API 封装（服务发布）
- `transfer_client.py` - Transfer 模块 API 封装（导入、导出）

**实现要点**：
- 使用 `httpx` 异步 HTTP 客户端
- 携带用户的 JWT token 进行认证
- 统一错误处理和重试机制
- 参数转换和响应解析

### 2.4 Skills 层实现

创建 `backend/skills/` 目录，定义初始 Skill 文档（Markdown 格式）：

**MVP 阶段 Skill 列表**（3-5 个基础 Skill）：
1. `import-data/` - 数据导入（支持多种格式）
2. `preview-data/` - 数据预览
3. `scan-metadata/` - 元数据扫描

**Skill 文档格式**（参考 Claude Code Skills）：
```markdown
---
name: import-data
description: "导入各种格式的数据文件到 ADDP 平台"
---

# 数据导入 Skill

## Use this skill when
- 用户想要上传 Shapefile、GeoJSON、CSV 等文件
- 用户说"导入数据"、"上传文件"

## Do not use this skill when
- 用户只是询问如何导入（应该提供指导）
- 文件格式不支持

## Instructions
1. 调用 manager_client.upload_file() 上传文件
2. 等待上传完成
3. 返回上传结果（文件 ID、路径、大小）
```

### 2.5 Agent 层实现

创建 `backend/agents/main_agent.py`，实现单智能体架构：

**核心功能**：
- 意图识别（使用 LLM）
- Skill 匹配（向量搜索或 LLM）
- Skill 执行（读取 Markdown 文档，调用 Tools）
- 多轮对话管理
- 异常处理

**使用 LangGraph 实现状态图**：
```python
from langgraph.graph import StateGraph

# 定义状态
class AgentState(TypedDict):
    messages: List[Message]
    current_skill: Optional[str]
    tool_calls: List[ToolCall]
    result: Optional[Dict]

# 构建状态图
graph = StateGraph(AgentState)
graph.add_node("understand_intent", understand_intent_node)
graph.add_node("match_skill", match_skill_node)
graph.add_node("execute_skill", execute_skill_node)
graph.add_node("format_result", format_result_node)
```

### 2.6 API 层实现

创建 `backend/api/` 目录，实现 FastAPI 接口：

**核心接口**：
- `POST /api/agent/chat` - 发送消息（支持流式输出）
- `GET /api/agent/sessions` - 获取会话列表
- `POST /api/agent/sessions` - 创建新会话
- `GET /api/agent/sessions/:id` - 获取会话详情
- `DELETE /api/agent/sessions/:id` - 删除会话
- `GET /api/agent/sessions/:id/messages` - 获取会话消息历史

**认证中间件**：
- 验证 JWT token
- 提取 user_id 和 tenant_id
- 传递给 Agent 和 Tools

**响应格式**（遵循 ADDP API 设计规范）：
```json
{
  "type": "text",
  "data": "上传成功！文件 ID: 123",
  "metadata": {
    "title": "上传结果",
    "actions": [
      {"label": "预览数据", "action": "preview", "params": {"file_id": 123}}
    ]
  }
}
```

### 2.7 模块注册

在 `backend/main.py` 中添加模块注册逻辑：

```python
# 启动后向 System 服务注册模块
async def register_module():
    system_client = SystemClient(config.SYSTEM_URL, config.INTERNAL_API_KEY)
    await system_client.register_module({
        "module_name": "agent",
        "module_url": f"http://localhost:{config.AGENT_BACKEND_PORT}",
        "route_prefix": "/agent",
        "health_check_url": f"http://localhost:{config.AGENT_BACKEND_PORT}/health"
    })

    # 启动心跳
    while True:
        await asyncio.sleep(10)
        await system_client.send_heartbeat("agent")
```

---

## 阶段三：前端开发

### 3.1 前端脚手架搭建

从 `system/frontend/` 复制结构，调整为 Agent 模块：

**关键配置**：
- `package.json`: 名称改为 `agent-frontend`
- `vite.config.js`: 端口改为 5186
- `src/router/index.js`: 基础路径设置为 `/agent/`
- `src/api/client.js`: baseURL 指向 Agent 后端 (8190)
- `src/api/auth.js`: 指向 System 后端 (8180)

**主题初始化**（必须）：
```javascript
// src/main.js
import 'element-plus/theme-chalk/dark/css-vars.css'
import '@common-ui/styles/theme.css'
import { useTheme } from '@common-ui'

const { init: initTheme } = useTheme({
  listenToConsole: true,
  storageKey: 'theme-mode'
})
initTheme()
app.mount('#app')
```

### 3.2 Layout 组件

创建 `src/components/Layout.vue`，支持双模式：

**关键要求**：
- 独立访问模式：显示完整的 header + sidebar + content
- Console 嵌入模式：仅显示 `<router-view>`
- **背景色必须使用 CSS 变量**（不得硬编码）：
  ```css
  .header  { background: var(--addp-bg-primary) !important; }
  .sidebar { background: var(--addp-bg-primary) !important; }
  .content { background: var(--addp-bg-secondary) !important; }
  ```

### 3.3 核心组件开发

创建 `src/components/` 目录，实现对话界面组件：

**必需组件**：
- `ChatInput.vue` - 消息输入框（支持多行、快捷键）
- `MessageList.vue` - 消息列表（用户消息 + AI 回复）
- `SessionList.vue` - 会话列表（左侧边栏）
- `ResultRenderer.vue` - 智能结果渲染组件

**ResultRenderer 组件**：
根据 `result.type` 动态选择渲染组件：
```vue
<template>
  <component :is="getComponent(result.type)" :data="result.data" />
</template>

<script setup>
import { TablePreview, GeoJsonPreview } from '@common-ui-map'
import { ImagePreview } from '@common-ui/previews'

const getComponent = (type) => {
  switch (type) {
    case 'table': return TablePreview
    case 'map': return GeoJsonPreview
    case 'image': return ImagePreview
    default: return 'div'
  }
}
</script>
```

### 3.4 Vercel AI SDK 集成

使用 `@ai-sdk/vue` 实现流式对话：

```javascript
import { useChat } from '@ai-sdk/vue'

const { messages, input, handleSubmit, isLoading } = useChat({
  api: '/api/agent/chat',
  headers: {
    Authorization: `Bearer ${authStore.token}`
  }
})
```

### 3.5 API 客户端

创建 `src/api/agent.js`，封装 Agent 后端 API：

**关键要求**：
- 使用 `createAPIClient` 创建客户端
- API 路径格式：`/agent/sessions`（不含 `/api` 前缀）
- 自动携带 JWT token

```javascript
import { createAPIClient } from '@common-ui'
import { useAuthStore } from '../store/auth'

const client = createAPIClient(() => useAuthStore(), {
  moduleName: 'Agent'
})

export const agentAPI = {
  // 会话管理
  listSessions() {
    return client.get('/agent/sessions')
  },
  createSession(data) {
    return client.post('/agent/sessions', data)
  },

  // 对话
  sendMessage(sessionId, message) {
    return client.post(`/agent/sessions/${sessionId}/messages`, { message })
  }
}
```

---

## 阶段四：开发脚本集成

### 4.1 修改 start.sh

在 `scripts/dev/start.sh` 中添加 Agent 模块支持：

**需要修改的位置**（8 处）：
1. 添加启动标志（约第 167 行）
2. 添加到帮助信息（约第 19 行）
3. 添加到参数解析（约第 135 行）
4. 添加全量启动逻辑（约第 199 行）
5. 添加依赖启动逻辑（case 分支）
6. 添加编译逻辑（约第 690 行）- **注意：Agent 是 Python 项目，无需编译**
7. 添加启动逻辑（约第 805 行）
8. 添加前端配置（约第 1764 行）

**特殊处理**：
- Agent 后端是 Python 项目，不需要 Go 编译步骤
- 启动命令：`python backend/main.py`
- 需要先安装依赖：`pip install -r backend/requirements.txt`

### 4.2 修改 restart.sh

在 `scripts/dev/restart.sh` 中添加 Agent 模块支持：

**需要修改的位置**（3 处）：
1. 添加到帮助信息（第 6 行）
2. 添加到帮助选项列表（第 20 行）
3. 添加到参数解析（第 64 行）

### 4.3 修改 detect-common.sh

**注意**：Agent 模块使用 Python，不依赖 Go common 模块，无需修改此脚本。

---

## 阶段五：Console 集成

### 5.1 添加 Agent 入口

在 `console/frontend/src/views/Portal.vue` 中添加 Agent 模块卡片：

```vue
<el-card class="module-card" @click="navigateToModule('agent')">
  <template #header>
    <div class="card-header">
      <el-icon><ChatDotRound /></el-icon>
      <span>AI 助手</span>
    </div>
  </template>
  <div class="card-body">
    通过自然语言对话完成数据管理、分析、发布等操作
  </div>
</el-card>
```

### 5.2 添加顶部快捷入口

在 Console 顶部添加 AI 助手按钮（优先入口）：

```vue
<el-button type="primary" @click="openAgent">
  <el-icon><ChatDotRound /></el-icon>
  AI 助手
</el-button>
```

### 5.3 配置路由

在 `console/frontend/src/router/index.js` 中添加 Agent 路由：

```javascript
{
  path: '/agent',
  name: 'Agent',
  component: () => import('../views/ModuleFrame.vue'),
  meta: {
    requiresAuth: true,
    moduleUrl: import.meta.env.DEV
      ? 'http://localhost:5186'
      : '/agent',
    title: 'AI 助手'
  }
}
```

---

## 阶段六：Docker 集成

### 6.1 后端 Dockerfile

创建 `agent/backend/Dockerfile`：

```dockerfile
FROM python:3.11-slim

WORKDIR /app

# 安装依赖
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# 复制代码
COPY . .

# 暴露端口
EXPOSE 8190

# 启动应用
CMD ["python", "main.py"]
```

### 6.2 前端 Dockerfile

创建 `agent/frontend/Dockerfile`（参考 manager/frontend）：

```dockerfile
FROM node:18-alpine as builder

WORKDIR /app
COPY package*.json ./
RUN npm install
COPY . .
RUN npm run build

FROM nginx:alpine
COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]
```

### 6.3 docker-compose.yml 配置

在根目录 `docker-compose.yml` 中添加 Agent 服务：

```yaml
agent-backend:
  build:
    context: ./agent/backend
  ports:
    - "8190:8190"
  environment:
    - AGENT_BACKEND_PORT=8190
    - DB_HOST=postgres
    - DB_PORT=5432
    - REDIS_HOST=redis
    - REDIS_PORT=6379
  depends_on:
    - postgres
    - redis
  networks:
    - addp-network
  profiles:
    - full

agent-frontend:
  build:
    context: ./agent/frontend
  ports:
    - "8116:80"
  profiles:
    - full
  networks:
    - addp-network
```

---

## 阶段七：文档编写

### 7.1 模块文档

创建 `agent/CLAUDE.md`，包含：
- 模块概述
- 技术栈说明
- 目录结构
- 开发指南
- API 文档
- Skill 开发指南

### 7.2 开发计划文档

创建 `agent/docs/Agent模块开发分步计划.md`（本文档）

### 7.3 更新根文档

更新根目录 `CLAUDE.md`：
- 在模块列表中添加 Agent 模块
- 在端口分配表中添加 Agent 端口
- 在文档导航地图中添加 Agent 相关条目

---

## 阶段八：测试验证

### 8.1 单元测试

**后端测试**：
- Tools 层测试（API 调用、错误处理）
- Agent 层测试（意图识别、Skill 匹配）
- API 层测试（接口响应、认证）

**前端测试**：
- 组件测试（ChatInput、MessageList）
- API 客户端测试

### 8.2 集成测试

**端到端测试场景**：
1. 用户登录 → 打开 AI 助手 → 创建会话
2. 发送消息"上传一个 Shapefile" → Agent 识别意图 → 调用 Manager API → 返回结果
3. 查看会话历史 → 切换会话 → 删除会话

### 8.3 验证清单

**后端验证**：
- [ ] 模块独立启动成功（`python backend/main.py`）
- [ ] 健康检查通过（`GET /health`）
- [ ] 数据库表创建在 `agent` schema
- [ ] 模块注册到 Gateway 成功
- [ ] JWT 认证正常工作
- [ ] 可以调用其他模块 API（Manager、Meta 等）

**前端验证**：
- [ ] 前端独立访问成功（http://localhost:5186）
- [ ] 可以访问后端 API
- [ ] Console 可以嵌入 Agent 前端
- [ ] 切换 Console 主题，Agent 前端背景/边框随之变化
- [ ] 对话界面流畅，消息发送/接收正常
- [ ] 结果渲染正确（文本、表格、地图等）

**脚本验证**：
- [ ] 独立启动：`bash scripts/dev/start.sh -agent`
- [ ] 重启：`bash scripts/dev/restart.sh -agent`
- [ ] 全量启动：`bash scripts/dev/start.sh`

---

## 关键决策记录

| 决策点 | 选择 | 理由 | 日期 |
|--------|------|------|------|
| 后端语言 | Python 3.11+ | AI 生态成熟，LangGraph 支持 | 2026-03-24 |
| 后端框架 | FastAPI | 异步支持，性能好，文档自动生成 | 2026-03-24 |
| 多智能体框架 | LangGraph | 状态图模型，适合复杂流程编排 | 2026-03-24 |
| 前端对话 SDK | Vercel AI SDK | Vue 3 原生支持，流式输出 | 2026-03-24 |
| 智能体架构 | 单智能体 + 多 Skill | 简单、易调试、快速验证 | 2026-03-24 |
| 端口分配 | 8190/5186/8116 | 避免与现有模块冲突 | 2026-03-24 |

---

## 待解决问题

1. **端口冲突**: 8087 已被 Copilot 占用，需要确认新端口分配（建议 8190）
2. **LLM 配置**: 需要确认使用哪个 LLM Provider（OpenAI/Azure/Anthropic/Ollama）
3. **Skill 粒度**: 多细的操作应该封装为 Skill？需要在实践中验证
4. **跨模块流程**: "导入 → 分析 → 发布"是否需要多智能体？还是单智能体 + 多 Skill 串联？
5. **Python 与 Go 集成**: Agent 是 Python 项目，如何与 Go common 模块集成？是否需要 HTTP 方式调用？

---

## 验证目标

**MVP 阶段验证目标**：
1. ✅ 核心流程可行性：自然语言 → 意图识别 → API 调用 → 结果展示
2. ✅ 技术栈适配性：LangGraph + FastAPI + Vue 3 + Vercel AI SDK
3. ✅ 用户体验：对话是否流畅，结果是否清晰
4. ✅ 模块集成：能否正常调用 Manager、Meta 等模块的 API
5. ✅ Console 集成：能否在 Console 中嵌入并正常工作

---

## 参考文档

- `docs/agent模块设计共识.md` - Agent 模块设计共识
- `docs/spec/addp新模块开发指南.md` - 新模块开发规范
- `docs/spec/addp开发原则.md` - ADDP 开发原则
- `docs/spec/addp-API设计规范.md` - API 设计规范
- `docs/spec/addp端口分配.md` - 端口分配规范
- `docs/spec/addp配置介绍.md` - 配置管理规范

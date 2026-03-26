# Agent 模块

Agent 模块是 ADDP 平台的**自然语言交互入口**，用户通过对话方式完成数据管理、分析和发布操作。

## 模块概述

- **后端**: Python 3.11+ + FastAPI + LangGraph
- **前端**: Vue 3 + Element Plus
- **端口**: Backend 8190（开发）| Frontend 5186（开发）
- **数据库**: PostgreSQL `agent` schema

## 目录结构

```
agent/
├── backend/
│   ├── agents/          # Agent 核心逻辑 (LangGraph)
│   ├── tools/           # ADDP 模块 API 封装
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
```

## API 接口

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | /health | 健康检查 |
| GET | /api/agent/sessions | 会话列表 |
| POST | /api/agent/sessions | 创建会话 |
| GET | /api/agent/sessions/:id | 会话详情 |
| DELETE | /api/agent/sessions/:id | 删除会话 |
| GET | /api/agent/sessions/:id/messages | 消息历史 |
| POST | /api/agent/chat | 发送消息（流式） |

## 数据库 Schema

```sql
agent.sessions    -- 会话管理
agent.messages    -- 对话历史（含结果类型）
agent.skill_usage -- Skill 使用统计
```

## 配置项

在根 `.env` 文件中配置：

```env
# LLM 配置
LLM_PROVIDER=openai    # openai | anthropic
LLM_API_KEY=sk-...
LLM_MODEL=gpt-4o
LLM_BASE_URL=          # 可选，用于自定义 API 端点

# Agent 端口
AGENT_BACKEND_PORT=8190
AGENT_FE_PORT=5186
```

## 日志

- 后端日志: `logs/agent-backend.log`
- 后端错误: `logs/agent-backend-stderr.log`
- 前端日志: `logs/agent-frontend.log`

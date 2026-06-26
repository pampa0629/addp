# Copilot 模块说明

本文件为 Claude Code 提供在 Copilot 模块中工作时的指导说明。

## 模块概述

**Copilot 模块**是 ADDP 平台的 AI 辅助模块，基于大语言模型（LLM）提供智能对话能力，支持自然语言转 SQL 和自然语言转工作流。

技术栈：
- **后端**：Python 3.11+ + FastAPI + SQLAlchemy + PostgreSQL
- **AI 框架**：LangChain + 多 LLM 支持（OpenAI、Claude、Ollama、DashScope）
- **部署**：Docker + Docker Compose
- **端口**：后端默认 `8087`（环境变量 `COPILOT_BACKEND_PORT` 或运行时 `PORT`）

核心功能：
- **SQL 生成**：用户输入自然语言，AI 生成 SQL 查询
- **工作流生成**：用户描述需求，AI 生成 GIS 工作流 DAG
- **对话记忆**：支持多轮对话，理解上下文
- **多 LLM 支持**：支持多种 AI 模型，租户可自定义

## 数据库文档

**遇到以下场景时，主动阅读对应文档**：

| 场景 | 必读文档 | 触发关键词 |
|------|---------|----------|
| 数据库表结构查询 | 对应单表文档 | 字段定义、索引、约束 |
| 表之间关系 | 数据库架构.md | 外键、关联、数据流 |
| API端点详情 | 对应单表文档 | API、接口、请求响应 |
| LLM 配置管理 | llm_configs表 | AI模型、API Key、配置 |
| 对话历史管理 | conversations表、messages表 | 对话、消息、上下文 |

### 架构说明

- [数据库架构](docs/数据库架构.md) - 表关系、数据流向、设计决策

### 单表文档

详细的表结构和 API 说明文档：

- [conversations表](docs/tables/conversations表.md) - 对话会话表，管理对话上下文
- [messages表](docs/tables/messages表.md) - 对话消息表，存储对话内容
- [llm_configs表](docs/tables/llm_configs表.md) - LLM配置表，管理AI模型

**重要**：修改表结构或 API 时，必须同步更新对应的单表文档。

## 快速启动

### 开发环境

```bash
# 进入后端目录
cd copilot/backend

# 安装依赖
pip install -r requirements.txt

# 配置环境变量
cp .env.example .env
# 编辑 .env，配置数据库和 LLM API Key

# 推荐通过项目脚本启动
cd ../..
bash scripts/dev/start.sh -copilot

# 或在 copilot/backend 内本地调试
PORT=8087 ./venv/bin/python main.py
```

### 测试 API

```bash
# 健康检查
curl http://localhost:8087/health

# 测试 SQL 生成
curl -X POST http://localhost:8087/api/v1/copilot/sql/generate \
  -H "Content-Type: application/json" \
  -d '{
    "query": "查询所有人口大于100万的城市",
    "tenant_id": 1,
    "user_id": 2
  }'

# 测试工作流生成
curl -X POST http://localhost:8087/api/v1/copilot/workflow/generate \
  -H "Content-Type: application/json" \
  -d '{
    "query": "加载数据，计算100米缓冲区，保存结果",
    "tenant_id": 1,
    "user_id": 2,
    "workflow_engine_id": 1
  }'
```

## 项目结构

```
copilot/
├── backend/
│   ├── main.py              # FastAPI 应用入口
│   ├── config.py            # 配置管理
│   ├── database.py          # 数据库连接
│   ├── models/              # SQLAlchemy 模型
│   │   ├── conversation.py  # 对话会话模型
│   │   ├── message.py       # 消息模型
│   │   └── llm_config.py    # LLM配置模型
│   ├── api/                 # API 路由
│   │   ├── sql_agent_api.py      # SQL 生成 API
│   │   └── workflow_agent_api.py # 工作流生成 API
│   ├── agents/              # AI Agent 实现
│   │   ├── sql_agent.py     # SQL Agent
│   │   └── workflow_agent.py # Workflow Agent
│   ├── services/            # 业务服务
│   │   ├── memory_service.py # 对话记忆服务
│   │   └── metadata_matcher.py # 元数据匹配服务
│   └── requirements.txt     # Python 依赖
└── docs/                    # 文档目录
    ├── tables/              # 单表文档
    └── 数据库架构.md        # 架构文档
```

## 核心功能实现

### SQL Agent 工作流程

1. **接收用户请求**：自然语言查询 + 租户信息
2. **匹配数据源**：调用 Meta 模块查询匹配的表
3. **加载对话历史**：获取最近 N 条消息作为上下文
4. **调用 LLM**：传递上下文 + 数据源信息 + 用户查询
5. **生成 SQL**：LLM 返回 SQL 语句和解释
6. **保存消息**：存储用户消息和助手回复
7. **返回结果**：SQL + 候选数据源 + conversation_id

### Workflow Agent 工作流程

1. **接收用户请求**：自然语言工作流描述
2. **加载对话历史**：获取上下文（如有）
3. **两阶段生成**：
   - 第一阶段：理解需求，规划步骤
   - 第二阶段：生成具体的 DAG 定义
4. **验证工作流**：检查步骤完整性和依赖关系
5. **保存消息**：存储工作流定义到 metadata
6. **返回结果**：workflow DAG + 解释 + conversation_id

### 对话记忆服务

```python
# 保存对话消息
conversation_id = await memory_service.save_message(
    conversation_id=None,  # None 表示创建新对话
    tenant_id=1,
    user_id=2,
    user_message="查询所有城市",
    assistant_message="SELECT * FROM cities",
    metadata={"selected_datasource": {...}},
    context_type='sql'
)

# 加载对话历史
memory = await memory_service.get_memory(conversation_id=1)
# memory = {"messages": [...], "context_type": "sql"}
```

## 环境变量配置

```bash
# 数据库配置
POSTGRES_HOST=localhost
POSTGRES_PORT=15432
POSTGRES_USER=addp
POSTGRES_PASSWORD=addp_password
POSTGRES_DB=addp

# LLM 配置（默认）
DEFAULT_LLM_PROVIDER=openai
DEFAULT_LLM_MODEL=gpt-4-turbo
OPENAI_API_KEY=sk-xxxxx
OPENAI_BASE_URL=https://api.openai.com/v1

# 加密密钥（与 System 模块共享）
ENCRYPTION_KEY=your-base64-encoded-32-byte-key

# 服务配置
PORT=8087
DEBUG=true
```

## 多租户支持

- **对话隔离**：所有对话按 `tenant_id` 隔离
- **用户隔离**：普通用户只能访问自己的对话
- **LLM 配置**：租户可配置自己的 LLM（API Key、模型等）
- **配置优先级**：租户配置 > 全局配置 > 系统默认

## 相关文档

- [ADDP 平台架构](../CLAUDE.md) - 平台整体架构
- [System 模块](../system/CLAUDE.md) - 用户认证和租户管理
- [Meta 模块](../meta/CLAUDE.md) - 元数据查询（SQL Agent 依赖）

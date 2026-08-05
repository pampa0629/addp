# Copilot 模块说明

本文件为 Claude Code 提供在 Copilot 模块中工作时的指导说明。

## 模块概述

**Copilot 模块**是 ADDP 平台的领域 AI 辅助模块，嵌入具体业务页面，支持自然语言转 SQL、自然语言转工作流、导航建议和图谱抽取。

技术栈：
- **后端**：Python 3.11+ + FastAPI + SQLAlchemy + PostgreSQL
- **AI 框架**：LangChain 领域 Pipeline + ADDP Inference Runtime
- **部署**：Docker + Docker Compose
- **端口**：后端默认 `8087`（环境变量 `COPILOT_BACKEND_PORT` 或运行时 `PORT`）

核心功能：
- **SQL 生成**：用户输入自然语言，AI 生成 SQL 查询
- **工作流生成**：用户描述需求，AI 生成 GIS 工作流 DAG
- **领域上下文**：由功能页面提供受限、已验证的业务事实
- **场景绑定**：按 Tenant 显式绑定 > 平台默认绑定解析 Model Profile

## 数据库文档

**遇到以下场景时，主动阅读对应文档**：

| 场景 | 必读文档 | 触发关键词 |
|------|---------|----------|
| 数据库表结构查询 | 对应单表文档 | 字段定义、索引、约束 |
| 表之间关系 | 数据库架构.md | 外键、关联、数据流 |
| API端点详情 | 对应单表文档 | API、接口、请求响应 |
| 推理场景绑定 | inference_scenario_bindings表 | Model Profile、平台默认、租户覆盖 |
| 对话历史管理 | conversations表、messages表 | 对话、消息、上下文 |

### 架构说明

- [数据库架构](docs/数据库架构.md) - 表关系、数据流向、设计决策

### 单表文档

详细的表结构和 API 说明文档：

- [conversations表](docs/tables/conversations表.md) - 对话会话表，管理对话上下文
- [messages表](docs/tables/messages表.md) - 对话消息表，存储对话内容
- [inference_scenario_bindings表](docs/tables/inference_scenario_bindings表.md) - Copilot 场景与 Model Profile 的绑定

**重要**：修改表结构或 API 时，必须同步更新对应的单表文档。

## 快速启动

### 开发环境

```bash
# 进入后端目录
cd copilot/backend

# 安装依赖
pip install -r requirements.txt

# 数据库和 Inference Runtime 服务身份统一配置在仓库根目录 .env
cd ../..
bash scripts/dev/start.sh -copilot

# 或在 copilot/backend 内本地调试
cd copilot/backend
PORT=8087 ./venv/bin/python main.py
```

### 测试 API

```bash
# 健康检查
curl http://localhost:8087/health

# 测试 SQL 生成
curl -X POST http://localhost:8087/api/v1/copilot/sql/generate \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "查询所有人口大于100万的城市"
  }'

# 测试工作流生成
curl -X POST http://localhost:8087/api/v1/copilot/workflow/generate \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "加载数据，计算100米缓冲区，保存结果",
    "workflow_engine_id": 1,
    "resources": [
      {"role": "input", "locator": "addp://engine/8/path/public/roads?type=table&item_id=91"}
    ]
  }'
```

## 项目结构

```
copilot/
├── authorization/
│   └── permissions.yaml       # Copilot owner Permission Manifest
├── backend/
│   ├── main.py              # FastAPI 应用入口
│   ├── config.py            # 配置管理
│   ├── database.py          # 数据库连接
│   ├── models/              # SQLAlchemy 模型
│   │   ├── conversation.py  # 对话会话模型
│   │   ├── message.py       # 消息模型
│   │   └── inference_scenario_binding.py # 推理场景绑定
│   ├── api/                 # API 路由
│   │   ├── sql_agent_api.py      # SQL 生成 API
│   │   ├── workflow_agent_api.py # 工作流生成 API
│   │   ├── kg_extract_api.py     # Graph 内部单 chunk 抽取 API
│   │   └── navigate_api.py       # 导航建议 API
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

## IAM Permission 所有权

Copilot 是以下首批 Permission 的唯一 owner：

- `copilot.sql.execute`
- `copilot.workflow.execute`
- `copilot.configuration.read`
- `copilot.configuration.update`

机器可读事实源是 [authorization/permissions.yaml](authorization/permissions.yaml)。该 Manifest 由 `common/authorization` 在构建/发布期统一发现、校验和聚合，Copilot 服务启动时的 Module Registry 注册和心跳只描述服务可用性，不向 System 动态注册 Permission。

Copilot Permission 只授予“生成候选结果”，不授予候选 SQL、Workflow 或图谱结果的保存、发布或执行权限。真正业务操作仍由 Develop、Graph 等事实 owner 使用自身 Permission 和 Resource Policy 最终校验。

当前授权边界：

- `/sql/generate` 从 System AuthContext 取得 Principal 和 Tenant，请求体禁止 `tenant_id/user_id`，目标 Permission 为 `copilot.sql.execute`。
- `/workflow/generate` 使用 `workflow.draft.generate` Tool Scope，并唯一映射到可委托的 `copilot.workflow.execute`。
- `/kg-build/extract` 只接受 Graph 的 Tenant Service Access Token，请求和令牌 Tenant 必须一致，不消费 User Permission。
- `/navigate/guide` 只要求已认证 User，不读取客户端提交的身份，也不借用其他业务 Permission。

## 核心功能实现

### SQL Agent 工作流程

1. **接收用户请求**：自然语言查询 + 租户信息
2. **匹配数据源**：调用 Meta 模块查询匹配的表
3. **加载对话历史**：获取最近 N 条消息作为上下文
4. **调用 Inference Runtime**：使用 `nl2sql` 场景解析得到的 Model Profile
5. **生成 SQL**：LLM 返回 SQL 语句和解释
6. **保存消息**：存储用户消息和助手回复
7. **返回结果**：SQL + 候选数据源 + conversation_id

### Workflow Agent 工作流程

1. **接收用户请求**：通过 `common-python` 调用 System AuthContext 验证 ADDP 用户访问令牌，取得权威用户和租户，并接收自然语言描述、工作流运行时和 owner Tool 已验证的 `resources[]`
2. **加载对话历史**：获取上下文（如有）
3. **两阶段生成**（Copilot 不重复搜索或猜测资源）：
   - 第一阶段：理解需求，规划步骤
   - 第二阶段：生成具体的 DAG 定义
4. **发现并验证算子**：Copilot 使用 `addp-copilot` Tenant Service Access Token 调用 Develop 的引擎实例和 Public Operator Spec 接口，不转发入口 Delegated Token，不使用 Internal API Key
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

# Inference Runtime 与 Copilot Service Principal
INFERENCE_URL=http://localhost:8191
COPILOT_SERVICE_CLIENT_SECRET=replace-with-unique-copilot-secret-32bytes

# 服务配置
PORT=8087
DEBUG=true
```

## 多租户支持

- **对话隔离**：所有对话按 `tenant_id` 隔离
- **用户隔离**：普通用户只能访问自己的对话
- **推理场景绑定**：Copilot 只保存 Model Profile ID，不保存 Provider、endpoint、上游模型或 API Key
- **配置优先级**：Tenant 显式场景绑定 > 平台默认场景绑定 > 明确未配置错误
- **推理服务身份**：Copilot 使用 `addp-copilot` Client Credentials 获取 Tenant Service Access Token
- **Develop 算子读取**：复用同一 `addp-copilot` Tenant Service Access Token，`tenant.copilot_runtime` 只额外持有 `develop.task.read`

## 相关文档

- [ADDP 平台架构](../CLAUDE.md) - 平台整体架构
- [System 模块](../system/CLAUDE.md) - 用户认证和租户管理
- [Meta 模块](../meta/CLAUDE.md) - 元数据查询（SQL Agent 依赖）

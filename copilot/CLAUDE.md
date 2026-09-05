# Copilot 模块说明

本文件为 Claude Code 提供在 Copilot 模块中工作时的指导说明。

## 模块概述

**Copilot 模块**是 ADDP 平台的领域 AI 辅助模块，嵌入具体业务页面，统一完成输入资源解析与确认，并提供查询、工作流、Notebook、Transfer、导航和图谱领域生成。

技术栈：
- **后端**：Python 3.11+ + FastAPI + SQLAlchemy + PostgreSQL
- **AI 框架**：LangChain 领域 Chain + ADDP Inference Runtime
- **部署**：Docker + Docker Compose
- **端口**：后端默认 `8087`（环境变量 `COPILOT_BACKEND_PORT` 或运行时 `PORT`）

核心功能：
- **查询生成**：用户输入自然语言，AI 按当前 Query Engine capability 生成 SQL、MQL、Cypher 等候选查询
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

### 架构说明

- [数据库架构](docs/数据库架构.md) - 表关系、数据流向、设计决策

### 单表文档

详细的表结构和 API 说明文档：

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
curl http://localhost:8087/health/ready

# 测试查询生成
curl -X POST http://localhost:8087/api/v1/copilot/query/generate \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "查询所有人口大于100万的城市",
    "engine_id": 2,
    "query_language": "sql",
    "resources": []
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
│   │   └── inference_scenario_binding.py # 推理场景绑定
│   ├── api/                 # API 路由
│   │   ├── query_agent_api.py    # 当前 Query Engine 范围内的查询语言生成 API
│   │   ├── notebook_agent_api.py # Notebook Session 候选排序和 Python 单元生成 API
│   │   ├── workflow_agent_api.py # 工作流生成 API
│   │   ├── kg_extract_api.py     # Graph 内部单 chunk 抽取 API
│   │   └── navigate_api.py       # 导航建议 API
│   ├── chains/              # 资源意图、候选推荐和生成链
│   ├── services/            # 业务服务
│   │   ├── resource_resolution.py # 统一输入资源解析与确认
│   │   ├── resource_discovery.py # owner 候选发现和校验
│   │   ├── operator_catalog.py # Develop Public Operator Spec 目录
│   │   ├── workflow_service.py # 工作流编排服务
│   │   ├── query_service.py # 查询语言生成服务
│   │   ├── notebook_service.py # Notebook Python 单元生成服务
│   │   └── kg_extraction_service.py # 图谱抽取服务
│   └── requirements.txt     # Python 依赖
└── docs/                    # 文档目录
    ├── tables/              # 单表文档
    └── 数据库架构.md        # 架构文档
```

## IAM Permission 所有权

Copilot 是以下首批 Permission 的唯一 owner：

- `copilot.sql.execute`（当前覆盖各 Query Engine 声明的候选查询语言生成）
- `copilot.notebook.execute`（只生成候选 Python 单元，不执行代码）
- `copilot.workflow.execute`
- `copilot.transfer.execute`（仅生成 Transfer 待确认草稿，不创建或启动任务）
- `copilot.standard_document.execute`（仅接受 Standard 服务身份并提炼带来源证据的标准候选）
- `copilot.configuration.read`
- `copilot.configuration.update`

机器可读事实源是 [authorization/permissions.yaml](authorization/permissions.yaml)。该 Manifest 由 `common/authorization` 在构建/发布期统一发现、校验和聚合，Copilot 服务启动时的 Module Registry 注册和心跳只描述服务可用性，不向 System 动态注册 Permission。

Copilot Permission 只授予“生成候选结果”，不授予候选查询、Workflow 或图谱结果的保存、发布或执行权限。真正业务操作仍由 Develop、Graph 等事实 owner 使用自身 Permission 和 Resource Policy 最终校验。

当前授权边界：

- `/query/generate` 使用 `query.draft.generate` Tool Scope，并唯一映射到可委托的 `copilot.sql.execute`；请求体禁止 `tenant_id/user_id`。
- `/notebook/generate` 使用 `notebook.draft.generate` Tool Scope，并唯一映射到可委托的 `copilot.notebook.execute`；只接受 Develop 已限定的 Session 候选或已验证资源事实，不自行执行租户级资源搜索。
- `/workflow/generate` 使用 `workflow.draft.generate` Tool Scope，并唯一映射到可委托的 `copilot.workflow.execute`。
- `/transfer/generate` 使用 `transfer.draft.generate` Tool Scope，只要求 `copilot.transfer.execute`；源资源和目标父节点由 owner 重新验证，运行边界和目标策略沿用 Transfer 向导草稿。
- `/kg-build/extract` 只接受 Graph 的 Tenant Service Access Token，请求和令牌 Tenant 必须一致，不消费 User Permission。
- `/standard-documents/extract` 只接受 Standard 的 Tenant Service Access Token；只返回候选内容与绝对行号，不保存、创建或发布正式标准。
- `/navigate/guide` 只要求已认证 User，不读取客户端提交的身份，也不借用其他业务 Permission。

## 核心功能实现

### Query Agent 工作流程

1. **接收工作台上下文**：自然语言、当前 `engine_id`、当前 `query_language`、可选资源事实和可选 `current_query`；`current_query` 只表示编辑器已有候选文本，不表示资源身份，也不接收客户端身份字段
2. **验证当前引擎**：通过共享 `engine.list` Tool 确认当前用户可访问该引擎，查询语言必须属于其 `compute.query` capability
3. **限定资源发现**：已有具体 data item locator 时通过 `resource.facts.get` 直接校验；Develop 在 MongoDB database 上下文中必须把已有 MQL 的主 collection 和 `$lookup/$graphLookup/$unionWith` 引用解析为当前 database 下的具体 collection locator 后提交；编辑器为空时提交独立的 `resource_scope_locator`，确定性执行 `resource.children.list → resource.facts.get` 并原样返回真实候选，不先调用模型生成资源角色或搜索词；没有明确 Catalog 范围时才调用带当前 `engine_id` 的 `data.search → resource.facts.get`
4. **确认歧义**：查询生成统一返回结构化 `clarifications[]`。资源候选、计算规则、统计对象、时间范围、聚合维度、实体匹配、字段映射以及去重/空值/分母规则等任何会改变结果、但无法从用户原话和已验证事实唯一确定的语义缺口，都必须要求用户澄清；前端通过通用控件收集 `clarification_answers` 后继续同一生成请求。不得用字符串消息代替交互，也不得针对“重叠度”等单一措辞硬编码页面逻辑
5. **调用 Inference Runtime**：使用 `query_generation` 场景绑定的 Model Profile；资源解析统一使用 `resource_resolution`
6. **生成与校验候选**：SQL/Cypher 先生成结构化 Query Plan，再基于已验证事实生成并校验只读候选。MQL 使用唯一的强类型语义计划路径：模型只选择已验证 collection、字段、谓词、排序、投影、统计或集合比较语义，不直接生成 MQL；集合比较只允许两个实体、一个或多个数组元素身份字段，以及 `intersection_count`、`jaccard`、`overlap_coefficient` 三种明确口径，编译器按身份字段去重后确定性生成交并集管道。任何必需语义槽位未闭合时都返回结构化澄清，不能只针对“重叠度”设置特例。用户答案作为强约束进入后续规划和编译，编译器必须验证最终计划与答案一致。Copilot 确定性校验字段类型、数组元素类型、操作符、参数、统计对象和歧义后，由 MQL 编译器生成单个 JSON command object。任何未经用户确认的同义词扩展、值翻译或非空 `assumptions` 都必须返回澄清，不能进入编译。MQL 不保留模型自由生成候选的旧路径
7. **返回完整查询草稿**：成功响应同时返回 `query` 和与引用严格一致的 `query_parameters[]`；前端原子回填编辑器与参数面板，但不自动保存或执行，后续仍由 Develop preflight 和 execution API 负责

`resources[]` 只表示具体、可预览的数据项，不表示执行容器。MongoDB `database`、关系库 `schema`、对象存储目录等容器节点不能作为查询生成输入资源；Owner 已正常确认 locator、但 preview 没有平台 `data_type` 时，应按调用参数无效处理，而不是返回 502 上游响应错误。MongoDB database 执行范围仍由 Develop 的 `target_locator` 和执行链路持有；Develop 从已有 MQL 提取所有 collection 引用，在该 database 的 Owner Engine Catalog 中解析为具体 locator 后提交，Copilot 不得自行拼接 locator。编辑器为空时，database locator 只能进入与 `resources[]` 分离的 `resource_scope_locator`，Copilot 通过 Owner Tool 返回范围内经预览验证的候选并要求用户确认；合法 MQL 已声明 collection 时不再允许无字段事实直接生成，也不得退回范围枚举或模糊发现。

### Workflow Agent 工作流程

1. **接收用户请求**：通过 `common-python` 调用 System AuthContext 验证 ADDP 用户访问令牌，取得权威用户和租户；Develop 普通用户首次请求缺少 `resources[]` 时，Copilot 先理解独立输入数据意图，再通过共享 ToolExecutor 对每个输入执行 `data.search → resource.ancestors.get → resource.facts.get` 并返回候选；某个角色首轮零召回时只为该角色生成未尝试的新检索词并补充搜索一次；LLM 只对已验证候选排序和标记推荐项，不删除仍然合理的候选；同一输入角色存在多个候选时由用户选择一个；Agent/确认后的请求直接携带 owner Tool 已验证的 resources
2. **生成工作流**：`WorkflowService` 通过 `OperatorCatalogService` 读取当前 Runtime 的 Public Operator Spec，完成算子筛选、DAG 生成、验证和有限自动修复
3. **返回结果**：workflow DAG、解释、资源事实和验证结果；不保存 Copilot 对话记忆

### Notebook Agent 工作流程

1. 首次调用只理解独立输入数据角色并生成中英文常用检索词，不接收或构造连接信息、ResourceLocator、EngineCatalogPath。
2. Develop 只在当前 Notebook Session 授权的实时 Catalog 内粗筛，随后把候选摘要交给 Copilot；LLM 只能排序和标记推荐项，不能删除、改写或新增候选。
3. 用户逐角色确认后，Develop 重新验证 Engine/path 并读取字段、几何列、几何类型和 CRS 等事实。
4. Copilot 只使用确认事实生成通过 `addp_common.notebook.engines` 访问数据的 Python 单元。Notebook 分析统一通过受控表扫描进入 Pandas/GeoPandas，不生成 `engine.sql(...)`；空间表使用共享 `to_geopandas(...)` 读取真实 EWKB 几何，拒绝旁路连接、未知字段和硬编码空间字段或 CRS 假设。
5. 最终 DataFrame 列名、图例、坐标轴等用户可见标签跟随用户请求语言，面积、距离等结果同时标明单位；Python 内部变量名不承担展示语义。
6. Develop 前端展示代码并由用户确认插入新单元；JupyterLab bridge 只插入，不执行。

## 环境变量配置

```bash
# 数据库配置
POSTGRES_HOST=localhost
POSTGRES_PORT=15432
POSTGRES_USER=addp
POSTGRES_PASSWORD=addp_password
POSTGRES_DB=addp

# Copilot Service Principal；Inference Runtime 由 System Runtime Descriptor 发现
COPILOT_SERVICE_CLIENT_SECRET=replace-with-unique-copilot-secret-32bytes

# 服务配置
PORT=8087
DEBUG=true
```

## 多租户支持

- **推理场景绑定**：Copilot 只保存 Model Profile ID，不保存 Provider、endpoint、上游模型或 API Key
- **配置优先级**：Tenant 显式场景绑定 > 平台默认场景绑定 > 明确未配置错误
- **推理服务身份**：Copilot 使用 `addp-copilot` Client Credentials 获取 Tenant Service Access Token
- **Develop 算子读取**：复用同一 `addp-copilot` Tenant Service Access Token，`tenant.copilot_runtime` 只额外持有 `develop.task.read`

## 相关文档

- [ADDP 平台架构](../CLAUDE.md) - 平台整体架构
- [System 模块](../system/CLAUDE.md) - 用户认证和租户管理
- [Meta 模块](../meta/CLAUDE.md) - 元数据查询（SQL Agent 依赖）

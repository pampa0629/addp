# Develop 模块说明

## 核心职责

Develop 模块是 ADDP 平台的**开发工作台**，负责以下核心功能：

1. **查询开发** - 按 Engine capability 支持 SQL、MQL、Cypher 等在线查询
2. **GIS 工作流管理** - 可视化编辑和执行空间数据工作流（基于 GeoPython Workflow 运行时）
3. **Jupyter Notebook 集成** - 支持 Python 数据分析和机器学习
4. **算子发现** - 聚合工作流运行时的动态算子（GeoPython Workflow、Spark Workflow 等）
5. **执行历史管理** - 保存 SQL/工作流执行记录，支持历史回溯

## 关键架构

### 统一执行架构

```
前端请求（SQL/工作流/Notebook）
  ↓
DevExecutor（统一执行器）
  ├─ 查询执行 → SQLEngineService / FederatedQueryService / GraphQueryProvider
  │  ├─ 普通查询通过 System 获取单引擎执行期连接
  │  ├─ 普通 SQL 使用 common/dbbridge 执行查询
  │  ├─ DuckDB 联邦查询按 execution_config.engine_id 解析真实 Runtime Engine
  │  ├─ 独立 DuckDB Runtime 消费授权并取得各 Source Engine 连接
  │  └─ 返回服务端受限的表格或图结果预览
  ├─ 工作流执行 → WorkflowEngineService
  │  ├─ 解析工作流 JSON（DAG 结构）
  │  ├─ 调用 GeoPython Workflow 运行时（21 个空间算子）
  │  ├─ 或调用 Spark Workflow 运行时（大数据空间计算，执行时绑定 Spark 通用引擎资源）
  │  └─ 返回执行结果（GeoJSON/DataFrame）
  └─ Notebook 执行 → NotebookExecutionService
     ├─ 从 execution_config.engine_id 读取绑定的 System Script Engine
     ├─ 通过 Runtime Descriptor 和 ScriptRuntimeProvider.OpenSession 解析受控端点
     ├─ 使用 Develop 租户 Service Access Token 调用无头 Notebook Runtime
     ├─ 从 MinIO 读取 Notebook，并通过 Papermill 执行 Python Cell
     └─ 将执行结果写回 MinIO，并返回结果摘要
  ↓
TaskExecutionRepository（统一执行记录持久化）
  └─ common.task_executions 表（module=develop）
```

### 算子发现服务

Develop 模块按具体工作流运行时实例聚合算子定义，用于工作流画布。当前内置运行时包括 GeoPython Workflow、Spark Workflow、SuperMap Workflow，以及 Model3D、PointCloud 等专用运行时；用户自研 `addp.workflow/v1` 运行时也走同一发现链路。

算子契约分为三层：

- **Runtime Operator Spec**：来自运行时 `/api/operators` 的 `parameters[]`，只声明运行时真实执行参数。
- **Develop Adapter Spec**：Develop 按 `workflow engine type + operator id` 显式注册资源适配规则，负责 `locator -> connection_info/schema/table/path`。
- **Public Operator Spec**：Develop 算子发现 API 输出的 `public_parameters`，供前端、用户和 AI 使用；资源选择器 UI 也在此层声明。

前端工作流编辑器只消费 `public_parameters`，保存的 workflow definition 只包含公开参数。执行前由 Develop Adapter 派生运行时参数，再把纯 Runtime Operator Spec 参数发送给运行时。

文件、对象或目录型持久化转换算子使用 `addp.workflow.access-plan/v1`。工作流定义只保存 `locator`、`target_parent_locator + target_name`、`write_mode` 和公开转换选项；Develop 必须分别解析源、目标存储引擎并构造执行期访问计划，不能用一个共享 `engine_id/connection_info` 覆盖两端资源。转换成功后由 Develop 根据目标生成 `produced_targets` 并提交 Meta 深度扫描。

**注意**：Meta、Transfer、Manager 模块提供的是**任务**（Tasks），不是算子，它们主要用于 Orchestrator 工作流编排。

### TaskProvider 边界

Develop 作为一个 TaskProvider 注册到 System，声明 `query`、`workflow`、`script` 三种任务类型。算子工作流必须先在 Develop 中保存为 `dev_tasks.dev_type=workflow` 的任务定义，再以 `provider=develop, task_type=workflow, task_id=...` 被 Orchestrator 引用。Notebook 是 `script` 任务的当前实现形态和 UI 入口，不作为独立 `task_type`。当前 Develop 不具备 owner scheduler / `next_run_at` due claim 闭环，因此不声明定时能力，不保存或暴露 `schedule`、`enabled`、`next_run_at`。

具体任务详情必须返回任务级 `execution_contract`。工作流契约根据当前保存的 workflow definition 和目标运行时算子动态生成：未被内部连线占用的公开参数默认可覆盖，内部 `$ref` 连线、DAG 和 Runtime/Adapter 派生参数不得暴露；保存算子只声明可持久化的 ResourceLocator 稳定输出。参数分组按稳定 DAG 拓扑顺序声明 `input_ui_schema.order`，同层算子按 workflow definition 的 `tasks[]` 保存顺序排列，字段按公开参数声明顺序排列。资源参数展示统一消费共享资源树 selection：摘要展示引擎实例名称、引擎原生路径和本地化资源类型，geometry 字段由资源空间事实自动回填，单字段直接确定、多字段使用选择控件，不提供自由文本输入。手动执行和 Orchestrator 调用都只提交本次 `parameters` 部分覆盖，未提交字段使用任务保存值，且不得改写任务定义。`query`、`script` 当前返回空的闭合输入/输出契约。

### 前端任务路由

Develop 任务编辑器遵守 `docs/spec/addp前端路由与可恢复状态规范.md`。Console URL 必须能够恢复当前 `dev_tasks.id`，canonical 路由固定为 `/develop/{sql|workflow|notebook}?action={create|edit}&id={id}`：创建动作不带 `id`，编辑动作只使用 `id`。`/develop/tasks` 只表示任务列表，`taskId` 旧参数不得保留。

查询工作台固定使用左侧 Meta Catalog、右侧编辑器与结果上下分栏。Catalog 直接消费 Meta resource-tree，只展示当前查询 Engine 的原生路径；查询语言、默认语言和结果类型从 `capabilities.compute.query` 读取。即时查询只调用 `POST /api/v1/develop/executions` 创建 `task_type=query`、`source_task_id=null` 的 execution，再按 execution ID 回查结果；不保留 `/develop/execute`。查询任务统一在 `/develop/tasks` 管理，不保留 `/develop/sql-tasks`。

执行列表 `/develop/executions` 的稳定筛选和分页状态使用 `dev_type`、`status`、`trigger_type`、`source_task_id`、`start_date`、`end_date`、`page`、`page_size` query；默认页码和默认每页数量从 URL 省略，未知或无效参数必须通过 `replace` 清理。

TaskProvider、执行状态回查和 Asset 发现属于服务间接口，统一只接受 Bearer Service Access Token 和 canonical AuthContext。TaskProvider 固定由 `addp-orchestrator` 调用，Asset 发现固定由 `addp-asset` 调用；两者还要校验各自精确 Permission。代表用户创建或继续执行任务时必须引用由原 User AuthContext 派生、绑定唯一 execution 的 Execution Authorization；内部调用不能凭 Service Principal 自身权限伪造 User、Tenant、`triggered_by` 或数据访问能力。旧 `/api/v1/develop/internal`、`X-Internal-API-Key` 和 `X-Tenant-ID` 已删除，不保留双轨。

### 执行授权与效果

- SQL、Workflow、Notebook 执行效果固定为 `read | write | ddl | external_effect`。
- `develop.task.execute` / `develop.notebook.execute` 只允许使用执行功能，不自动授予任意数据效果。
- 当前 User AuthContext、owner Resource Rule/Policy、Engine 归属和执行效果共同决定 Execution Authorization；Engine 创建人和 Runtime Service Principal 都不是授权来源。
- 异步执行只保存 Execution Authorization ID、发起 Principal、Tenant Membership、签发授权版本和脱敏效果/资源摘要，不保存 User Token、Service Token、明文连接或 Workflow Access Plan。
- Jupyter 只通过 Develop 受控会话使用数据访问能力，不直接返回共享 Lab 数据访问入口，不注入长期明文 Engine 连接。
- Notebook 交互编辑只允许 `POST /notebooks/{id}/sessions` 创建的短期隔离会话。Develop 返回同源 `/notebook-sessions/{session_id}/...` 路径并设置仅限该路径的 HttpOnly 能力 Cookie；浏览器不得获得 Runtime 地址、Jupyter Token、Service Token 或 URL Token。Develop 在代理 HTTP/WebSocket 前校验会话、Tenant、User、Task 和到期时间；会话关闭、过期或 Develop 重启后 fail-closed。
- Notebook Kernel 获取当前可用查询 Engine 及其实时 Catalog 时，只允许使用 Develop 为同一交互会话签发的短期 Notebook Kernel Capability Token 调用该 Session 的只读 Engine Runtime Descriptor 与 Catalog 代理。Token 绑定 Session、Tenant、User、Task 与 TTL，Develop 只保存 Hash；Runtime 只注入隔离 Kernel process，不写入 Notebook、日志或公开会话响应。接口不得返回 `connection_info`，也不得把 Notebook Script Engine 或长期数据连接伪装成当前可用查询 Engine。
- Notebook Session 创建时，Develop 必须在同步请求栈内使用当前 User Bearer 向 System 签发 Notebook Session Authorization，随后丢弃 User Bearer；只在内存 Session 保存授权 ID。后续 Catalog 以及每次查询/扫描的独立只读 Execution Authorization 派生，都由 `addp-develop` Service Principal 消费该授权，不能依赖自身通用 `system.engine.read`。连接仅在 Develop 受控 Runtime 内使用，不返回 Kernel；关闭、过期、撤权、登出或 Develop 重启后取消活动查询并 fail-closed。
- Notebook Native Engine Facade 只存在于 `common-python`，按具体 Engine 原生术语把 `schemas()`、`tables()`、`collections()` 等调用编译为统一 Catalog 请求。Develop 不新增 PostgreSQL/MySQL/MongoDB/MinIO 专用目录 API，不自行拼接 `CatalogPath`，不为未知引擎提供暴露内部 Catalog 术语的 fallback。
- Notebook 数据读取统一进入当前 Kernel Session 下的 `table-scans`、`record-scans`、`queries`、`graph-samples`、`graph-queries`、`content-reads`、`change-streams` 代理。Develop 每次派生独立只读 Execution Authorization，并按 `common` Provider 契约选择表游标、动态记录游标、图、内容、range 或 change stream；不得为具体引擎增加旁路端点。
- Runtime 在交互会话创建时从任务绑定的 Notebook owner 路径装载文件，在关闭和 TTL 清理时原子保存回同一路径并终止 kernel/process。新建空白 Notebook 与上传 Notebook 都创建同一种 `script` 任务和 MinIO 对象，随后进入同一交互会话；不得恢复共享 Lab、直连 Runtime 或第二套 Notebook 实体。
- Notebook 任务允许用户显式重绑定原任务的 Script Engine 和 Kernel。重绑定只更新任务定义并影响后续执行；历史执行保留创建时的 `execution_config` 快照，不复制任务或 Notebook 文件，也不自动选择替代引擎。
- 算子工作流的存储 Engine 绑定来自 `content` 中的标准 ResourceLocator（主要位于 `workflow_definition`），不是 `execution_config.engine_id` 的工作流运行时绑定。Engine 删除后任务定义和旧 Locator 保留；用户在 Develop 显式选择新存储 Engine 后，Develop 原子改写该旧 Engine 的全部 Locator，保留 path/type 并清除旧 Meta `node_id/item_id`。System 不跨模块回写任务，也不按名称自动匹配新旧 Engine。
- DuckDB 联邦查询先从 SQL 中解析已注册的 Source Engine 引用，为本次 execution 一次性签发只读 Execution Authorization，再由独立 DuckDB Runtime 按 Engine 逐个消费执行期连接；当前联邦查询必须至少引用一个 Source Engine。普通引擎的可执行样例查询也必须先签发并消费单 Engine 的只读 Execution Authorization，再实时发现真实表。两条路径都不得用 `tenant.develop_runtime` 的通用 Engine 明文读取权限替代。
- 查询样例只允许来自当前 Engine Instance 的实时 Catalog 且必须指向确认有数据的 leaf；DuckDB 对象表还必须通过只读 Execution Authorization 取得执行期连接并真实读取成功，不能仅凭 Meta 条目存在就返回。Catalog 失败、对象已失效、无数据或无法构造查询时返回明确错误，前后端都不得回退到 `SELECT 1`、版本查询或占位集合名。
- 查询编辑器必须把样例返回的真实语言（如 `sql`、`mql`、`cypher`）原样带入执行和任务定义。非 SQL 查询不得进入 SQL 效果分类器；各 Query Runtime 必须在 `QueryOptions.ReadOnly=true` 时建立等价只读边界。
- Notebook 数据源注入在接入同一 Execution Authorization 消费路径前必须 fail-closed。

### IAM Permission

Develop 是 `develop.task.*` 和 `develop.notebook.*` 的 Permission owner；定义只存在于 `authorization/permissions.yaml`，通过 `common/authorization` 发布期聚合，不在服务启动时动态注册。`develop.task.cancel` 是 IAM 目标目录能力，当前真实执行取消入口仍待路由覆盖阶段确认。

## 数据库文档

**遇到以下场景时,主动阅读对应文档**:

| 场景 | 必读文档 | 触发关键词 |
|------|---------|----------|
| 数据库表结构查询 | 对应单表文档 | 字段定义、索引、约束 |
| 表之间关系 | 数据库架构.md | 外键、关联、数据流 |
| API端点详情 | 对应单表文档 | API、接口、请求响应 |
| 执行记录管理 | `common.task_executions` | 执行状态、历史记录、性能监控 |
| 开发任务管理 | dev_tasks表 | SQL查询、工作流、Notebook |

### 架构说明
- [数据库架构](frontend/docs/数据库架构.md) - 表关系、数据流向、设计决策

### 单表文档

详细的表结构和API说明文档：

- [dev_tasks表](frontend/docs/tables/dev_tasks表.md) - 开发任务定义表,支持 query/workflow/script
- Develop 执行记录统一写入 `common.task_executions`，不再维护 Develop 私有执行记录表。
- [tool_approvals表](frontend/docs/tables/tool_approvals表.md) - Develop 持有的委托 Tool 审批与一次性消费事实。

**重要**：修改表结构或API时，必须同步更新对应的单表文档。

## 重要文件位置

### 核心服务文件

- [dev_executor.go](backend/internal/service/dev_executor.go) - **统一执行器**（调度 SQL/工作流/Notebook 执行）
- [sql_engine_service.go](backend/internal/service/sql_engine_service.go) - SQL 执行服务
- [workflow_engine_service.go](backend/internal/service/workflow_engine_service.go) - GIS 工作流执行服务
- [jupyter_service.go](backend/internal/service/jupyter_service.go) - Jupyter Notebook 服务
- [operator_discovery_service.go](backend/internal/service/operator_discovery_service.go) - **算子发现服务**（聚合工作流运行时动态算子）
- [dev_task_service.go](backend/internal/service/dev_task_service.go) - 工作项（SQL/工作流）管理服务

### API 路由文件

- [backend/internal/api/router.go](backend/internal/api/router.go) - HTTP 路由定义
- [backend/internal/api/query_handler.go](backend/internal/api/query_handler.go) - 查询执行 API
- [backend/internal/api/dev_execution_handler.go](backend/internal/api/dev_execution_handler.go) - 执行管理 API
- [backend/internal/api/operator_handler.go](backend/internal/api/operator_handler.go) - 算子发现 API
- [backend/internal/api/notebook_handler.go](backend/internal/api/notebook_handler.go) - Jupyter Notebook API

### 前端视图文件

- [frontend/src/views/QueryEditor.vue](frontend/src/views/QueryEditor.vue) - SQL/查询编辑器
- [frontend/src/views/WorkflowEditor.vue](frontend/src/views/WorkflowEditor.vue) - GIS 工作流编辑器
- [frontend/src/views/NotebookEditor.vue](frontend/src/views/NotebookEditor.vue) - Jupyter Notebook 编辑器
- [frontend/src/views/ExecutionMonitor.vue](frontend/src/views/ExecutionMonitor.vue) - 执行历史查看

### 配置文件

- [backend/internal/config/config.go](backend/internal/config/config.go) - 配置加载逻辑
- [.env](../.env) - 环境变量（`DEVELOP_*` 前缀）

## 常见开发场景

### 场景 1：执行 SQL 查询

```bash
# 0. 查询目标发现
# 真实查询引擎来自 System 注册实例
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8185/api/v1/develop/engines"

# 1. 创建 ad-hoc 查询 execution
curl -X POST http://localhost:8185/api/v1/develop/executions \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "dev_type": "query",
    "trigger_type": "manual",
    "content": {
      "query_type": "sql",
      "query": "<sample-query 返回的 query>"
    },
    "execution_config": {
      "engine_id": 1
    },
    "timeout": 30
  }'

# DuckDB 联邦查询使用同一 execution 入口和真实 Runtime Engine ID
curl -X POST http://localhost:8185/api/v1/develop/executions \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "dev_type": "query",
    "trigger_type": "manual",
    "content": {
      "query_type": "sql",
      "query": "SELECT * FROM <source_engine>.<schema>.<table> LIMIT 10"
    },
    "execution_config": {
      "engine_id": 2
    },
    "timeout": 30
  }'

# 2. 创建响应返回 execution_id；按 ID 回查状态和受限结果预览
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8185/api/v1/develop/executions/<execution_id>"

# 3. 查看执行历史
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8185/api/v1/develop/executions?dev_type=query&page=1&page_size=20"
```

### 场景 2：创建算子工作流

```bash
# 1. 获取可用工作流引擎实例
curl -H "Authorization: Bearer <token>" \
  http://localhost:8185/api/v1/develop/workflow-engines

# 2. 按具体工作流引擎实例获取可用算子
curl -H "Authorization: Bearer <token>" \
  http://localhost:8185/api/v1/develop/workflow-engines/{workflow_engine_id}/operators

# 3. 创建工作流（JSON 定义）
curl -X POST http://localhost:8185/api/v1/develop/task-definitions \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "缓冲区分析",
    "dev_type": "workflow",
    "content": {
      "workflow_definition": {
        "tasks": [
          {"id": "load_data", "operator": "load", "params": {"locator": "addp://engine/12/path/public/cities?type=table"}, "depends_on": []},
          {"id": "buffer", "operator": "buffer", "params": {"input_gdf": {"$ref": "load_data"}, "distance": 1000}, "depends_on": ["load_data"]},
          {"id": "to_geojson", "operator": "to_geojson", "params": {"input_gdf": {"$ref": "buffer"}}, "depends_on": ["buffer"]}
        ]
      },
      "inputs": {}
    }
  }'

# 4. 执行前按目标运行时 Public Operator Spec 校验候选 definition
curl -X POST http://localhost:8185/api/v1/develop/workflow-validations \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "workflow_engine_id": 12,
    "workflow_definition": {
      "tasks": [
        {"id": "load_data", "operator": "load", "params": {"locator": "addp://engine/3/path/public/cities?type=table"}, "depends_on": []}
      ]
    }
  }'

# 5. 执行工作流
curl -X POST http://localhost:8185/api/v1/develop/task-definitions/123/execute \
  -H "Authorization: Bearer <token>" \
  -d '{"parameters": {"input_table": "public.cities"}}'
```

### 场景 3：调试工作流执行失败

```bash
# 1. 查看失败的执行记录
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8185/api/v1/develop/executions?status=failed&page=1&page_size=10"

# 2. 查看详细错误信息
curl -H "Authorization: Bearer <token>" \
  http://localhost:8185/api/v1/develop/executions/123

# 3. 查看 Develop 后端日志
tail -f logs/develop-backend.log | grep "execution_id=123"

# 4. 检查 GeoPython Workflow 运行时日志（如果是工作流错误）
tail -f logs/python-workflow-engine.log
```

## 注意事项

### 1. SQL 注入防护

Develop 不再把“数据库连接账号可以做什么”视为用户授权。执行前必须由服务端解析语句并汇总 `read | write | ddl | external_effect`，再校验当前 User 的 Execution Authorization：

- `read` 使用数据库只读事务或等价只读访问能力；仅做字符串前缀判断不构成安全边界。
- `write` 只允许显式具有写效果授权的执行，并保留影响行数和目标摘要。
- `ddl` 单独授权和审计，不包含在普通数据工程写入权限中。
- 不支持可靠效果分类的语句或引擎默认拒绝，不回退到“按连接账号权限执行”。

### 2. 工作流版本管理

工作流定义存储在 `develop.dev_tasks` 表的 `content` 字段（JSONB）：

- 每次修改工作流会覆盖原内容（不保留历史版本）
- 如需版本管理，可在前端实现版本号逻辑（存储到 `metadata` 字段）

### 3. GeoPython Workflow 内存管理

GeoPython Workflow 运行时在内存中处理空间数据（GeoDataFrame）：

- **内存限制** - 受 Python 进程内存限制（默认 4GB）
- **大数据处理** - 对于超大数据集（>100万行），使用 Spark Workflow 运行时，并绑定 Spark 通用引擎资源
- **OOM 风险** - 多个并发工作流可能导致内存不足

**优化建议**：
- 限制输入数据大小（前端提示用户先筛选数据）
- 配置 GeoPython Workflow 运行时的最大内存限制
- 使用 Spark Workflow 运行时处理大规模数据

### 4. 与其他模块的交互

- **System / 控制面** - 获取数据库连接信息（解密后的 ConnectionInfo）
- **工作流运行时** - 执行算子工作流并提供动态算子，例如 GeoPython Workflow、Spark Workflow、SuperMap Workflow 或用户自研 `addp.workflow/v1` 运行时
- **Jupyter 脚本运行时** - 执行 Python 代码和数据分析

### 5. 执行记录清理

执行记录会不断累积，建议定期清理：

```sql
-- 删除 30 天前的执行记录
DELETE FROM common.task_executions
WHERE module = 'develop'
  AND created_at < NOW() - INTERVAL '30 days';
```

如果需要清理旧开发任务，需单独确认任务保留策略后再处理 `develop.dev_tasks`。

## 典型开发工作流

### 修改 Develop 后端代码后

```bash
# 1. 重启 Develop 后端服务
bash scripts/dev/restart.sh -develop

# 2. 查看启动日志
tail -f logs/develop-backend.log

# 3. 测试 API
curl -H "Authorization: Bearer <token>" \
  http://localhost:8185/health
```

### 添加新算子到工作流编辑器

```bash
# 1. 在对应 Workflow Runtime 中添加算子实现和 Runtime Operator Spec
# 2. 确认 /api/operators 的 parameters[] 只包含真实运行时参数
# 3. 如果算子需要 locator 或目标资源选择，在 Develop Adapter Spec registry 中注册 Public Operator Spec 和派生规则
# 4. 重启对应 Workflow Runtime；修改了 Develop adapter registry 时同时重启 Develop

# 5. 前端按工作流引擎实例调用算子发现 API 获取新算子
curl -H "Authorization: Bearer <token>" \
  http://localhost:8185/api/v1/develop/workflow-engines/{workflow_engine_id}/operators

# 6. 校验响应同时包含纯 runtime parameters 和 Develop 聚合的 public_parameters
```

## 相关文档

- **GeoPython Workflow 运行时说明** - [engines/python-workflow](../engines/python-workflow)
- **工作流计算引擎接口规范** - [docs/spec/addp工作流计算引擎接口规范.md](../docs/spec/addp工作流计算引擎接口规范.md)
- **System 模块说明** - [system/CLAUDE.md](../system/CLAUDE.md)
- **共享数据库桥接** - `common/dbbridge`

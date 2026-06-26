# Develop 模块说明

## 核心职责

Develop 模块是 ADDP 平台的**开发工作台**，负责以下核心功能：

1. **SQL 查询执行** - 支持在线 SQL 查询，连接多种数据库（PostgreSQL、MySQL、MongoDB 等）
2. **GIS 工作流管理** - 可视化编辑和执行空间数据工作流（基于 Python Workflow 运行时）
3. **Jupyter Notebook 集成** - 支持 Python 数据分析和机器学习
4. **算子发现** - 聚合工作流运行时的动态算子（Python Workflow、Spark Workflow 等）
5. **执行历史管理** - 保存 SQL/工作流执行记录，支持历史回溯

## 关键架构

### 统一执行架构

```
前端请求（SQL/工作流/Notebook）
  ↓
DevExecutor（统一执行器）
  ├─ SQL 执行 → SQLEngineService / DuckDBService
  │  ├─ 通过 System 获取数据库连接
  │  ├─ 普通 SQL 使用 common/dbbridge 执行查询
  │  ├─ DuckDB 联邦查询使用 execution_config.query_mode="duckdb"
  │  └─ 返回查询结果
  ├─ 工作流执行 → WorkflowEngineService
  │  ├─ 解析工作流 JSON（DAG 结构）
  │  ├─ 调用 Python Workflow 运行时（21 个空间算子）
  │  ├─ 或调用 Spark Workflow 运行时（大数据空间计算，执行时绑定 Spark 通用引擎资源）
  │  └─ 返回执行结果（GeoJSON/DataFrame）
  └─ Notebook 执行 → JupyterService
     ├─ 创建/管理 Jupyter Kernel
     ├─ 执行 Python 代码
     └─ 返回执行结果（含 matplotlib 图表）
  ↓
TaskExecutionRepository（统一执行记录持久化）
  └─ common.task_executions 表（module=develop）
```

### 算子发现服务

Develop 模块聚合了所有**工作流运行时**的算子定义（用于工作流画布）：

- **Python Workflow 运行时** - 21 个空间算子（Buffer、Clip、Union、Intersect 等）
- **Spark Workflow 运行时** - 大数据空间算子（分布式计算；执行时必须绑定 `engine_type=spark` 的通用引擎资源）

前端通过算子发现 API 获取所有可用算子，动态构建工作流编辑器的算子面板。

**注意**：Meta、Transfer、Manager 模块提供的是**任务**（Tasks），不是算子，它们主要用于 Orchestrator 工作流编排。

### TaskProvider 边界

Develop 作为一个 TaskProvider 注册到 System，声明 `query`、`workflow`、`script` 三种任务类型。算子工作流必须先在 Develop 中保存为 `dev_tasks.dev_type=workflow` 的任务定义，再以 `provider=develop, task_type=workflow, task_id=...` 被 Orchestrator 引用。Notebook 是 `script` 任务的当前实现形态和 UI 入口，不作为独立 `task_type`。当前 Develop 不具备 owner scheduler / `next_run_at` due claim 闭环，因此不声明定时能力，不保存或暴露 `schedule`、`enabled`、`next_run_at`。

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

# Develop 内置查询模式单独暴露；DuckDB 不作为 /develop/engines 的虚拟 Engine
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8185/api/v1/develop/query-modes"

# 1. 通过 API 执行普通引擎查询
curl -X POST http://localhost:8185/api/v1/develop/execute \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "content": {
      "query_type": "sql",
      "query": "SELECT * FROM public.cities LIMIT 10"
    },
    "execution_config": {
      "engine_id": 1
    },
    "timeout": 30
  }'

# DuckDB 联邦查询使用同一执行入口
curl -X POST http://localhost:8185/api/v1/develop/execute \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "content": {
      "query_type": "sql",
      "query": "SELECT * FROM postgres_main.public.cities LIMIT 10"
    },
    "execution_config": {
      "query_mode": "duckdb"
    },
    "timeout": 30
  }'

# 2. 查看执行结果
# 返回 JSON: { "columns": [...], "rows": [...], "execution_id": 123 }

# 3. 查看执行历史
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8185/api/v1/develop/executions?dev_type=query&page=1&page_size=20"
```

### 场景 2：创建 GIS 工作流

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

# 3. 执行工作流
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

# 4. 检查 Python Workflow 运行时日志（如果是工作流错误）
tail -f logs/python-workflow-engine.log
```

## 注意事项

### 1. SQL 注入防护

Develop 模块允许用户执行任意 SQL（在其权限范围内），存在潜在的安全风险：

- ✅ **用户隔离** - 只能查询自己租户的数据库
- ✅ **权限继承** - 使用数据库连接的原始权限（不做提权）
- ❌ **不做 SQL 语法检查** - 允许执行 DROP/TRUNCATE 等危险操作（用户自己负责）

**建议**：在生产环境中配置数据库账号为只读权限，或限制危险操作。

### 2. 工作流版本管理

工作流定义存储在 `develop.dev_tasks` 表的 `content` 字段（JSONB）：

- 每次修改工作流会覆盖原内容（不保留历史版本）
- 如需版本管理，可在前端实现版本号逻辑（存储到 `metadata` 字段）

### 3. Python Workflow 内存管理

Python Workflow 运行时在内存中处理空间数据（GeoDataFrame）：

- **内存限制** - 受 Python 进程内存限制（默认 4GB）
- **大数据处理** - 对于超大数据集（>100万行），使用 Spark Workflow 运行时，并绑定 Spark 通用引擎资源
- **OOM 风险** - 多个并发工作流可能导致内存不足

**优化建议**：
- 限制输入数据大小（前端提示用户先筛选数据）
- 配置 Python Workflow 运行时的最大内存限制
- 使用 Spark Workflow 运行时处理大规模数据

### 4. 与其他模块的交互

- **System / 控制面** - 获取数据库连接信息（解密后的 ConnectionInfo）
- **Python Workflow / Spark Workflow 运行时** - 执行空间计算工作流（提供算子）
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
# 1. 在对应 Workflow 运行时中添加新算子实现和元数据
# 2. 确认该运行时实例的 /api/operators 暴露新算子
# 3. 重启对应 Workflow 运行时；Develop 通过实例 ID 动态发现，不需要在 Develop 中注册算子

# 4. 前端按工作流引擎实例调用算子发现 API 获取新算子
curl -H "Authorization: Bearer <token>" \
  http://localhost:8185/api/v1/develop/workflow-engines/{workflow_engine_id}/operators

# 5. 前端工作流编辑器会自动显示新算子
```

## 相关文档

- **Python Workflow 运行时说明** - [engines/python-workflow](../engines/python-workflow)
- **System 模块说明** - [system/CLAUDE.md](../system/CLAUDE.md)
- **共享数据库桥接** - `common/dbbridge`

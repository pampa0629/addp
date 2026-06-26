# dev_tasks 表结构和 API 说明

## 一、表结构概览

`develop.dev_tasks` 表是 Develop 模块的开发任务定义表，统一存储 SQL 查询、工作流、脚本等开发任务。Notebook 是 `script` 任务的当前实现形态，通过 `content.notebook_path` 标识。

### 核心功能

- **统一开发任务管理**：支持 query（SQL/MQL）、workflow（工作流）、script 三类任务
- **灵活内容存储**：使用 JSONB 存储不同类型的开发任务内容
- **执行配置**：支持超时配置、引擎绑定
- **状态追踪**：记录最后执行状态和时间
- **软删除**：支持逻辑删除，保留历史记录

---

## 二、表结构定义

### 2.1 核心字段

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | SERIAL | PRIMARY KEY | 开发任务唯一标识 |
| `tenant_id` | INTEGER | NOT NULL, INDEXED | 租户 ID |
| `name` | VARCHAR(255) | NOT NULL | 开发任务名称（唯一标识符） |
| `display_name` | VARCHAR(255) | | 显示名称（前端优先显示） |
| `dev_type` | VARCHAR(50) | NOT NULL, INDEXED | Develop 内部类型：'query'、'workflow'、'script'；TaskProvider 对外映射为 `task_type` |
| `content` | JSONB | NOT NULL | 开发任务内容（SQL、工作流定义等），推荐结构见下文 |
| `execution_config` | JSONB | | 执行配置（引擎、参数等），推荐结构见下文 |
| `timeout` | INTEGER | DEFAULT 300 | 超时时间（秒） |
| `description` | TEXT | | 描述信息 |
| `tags` | TEXT[] | | 标签数组（用于分类和搜索） |
| `created_by` | INTEGER | | 创建者 ID |
| `updated_by` | INTEGER | | 更新者 ID |
| `created_at` | TIMESTAMP | DEFAULT NOW() | 创建时间 |
| `updated_at` | TIMESTAMP | DEFAULT NOW() | 更新时间 |
| `deleted_at` | TIMESTAMP | INDEXED | 软删除时间（NULL 表示未删除） |
| `status` | VARCHAR(50) | DEFAULT 'active', INDEXED | 状态：'active'、'inactive'、'archived' |
| `last_execution_id` | VARCHAR(36) | | 最近 execution ID，软引用 `common.task_executions.execution_id` |
| `last_execution_status` | VARCHAR(50) | | 最后执行状态 |
| `last_run_at` | TIMESTAMP | | 最后执行时间 |

### 2.2 数据库索引

| 索引名 | 字段 | 说明 |
|--------|------|------|
| `idx_dev_tasks_tenant_type` | tenant_id, dev_type | 按租户和类型查询 |
| `idx_dev_tasks_status` | status | 按状态过滤 |
| `idx_dev_tasks_deleted` | deleted_at | 软删除查询 |
| `idx_dev_tasks_content_query_type` | (content -> 'query_type') | JSONB GIN 索引，查询 content.query_type |
| `idx_dev_tasks_execution_config_engine_id` | (execution_config -> 'engine_id') | JSONB GIN 索引，查询 execution_config.engine_id |

### 2.3 推荐的 Content 结构

#### Query 类型（dev_type='query'）

```json
{
  "query_type": "sql",
  "query": "SELECT * FROM cities WHERE population > 1000000",
  "limit": 1000
}
```

#### Workflow 类型（dev_type='workflow'）

```json
{
  "workflow_definition": {
    "tasks": [
      {
        "id": "load_1",
        "operator": "load",
        "params": {
          "locator": "addp://engine/1/path/public/cities?type=table"
        },
        "depends_on": []
      },
      {
        "id": "save_1",
        "operator": "save",
        "params": {
          "target_parent_locator": "addp://engine/1/path/public?type=schema",
          "target_name": "cities_result"
        },
        "depends_on": ["load_1"]
      }
    ]
  },
  "inputs": {
    "source_locator": "addp://engine/1/path/public/cities?type=table"
  }
}
```

`workflow_definition.tasks[]` 必须遵循工作流计算引擎接口规范：每个任务显式包含 `id`、`operator`、`params`、`depends_on`，无依赖时 `depends_on` 写空数组 `[]`。

### 2.4 推荐的 ExecutionConfig 结构

普通 SQL 查询任务：

```json
{
  "engine_id": 1
}
```

DuckDB 联邦查询任务：

```json
{
  "query_mode": "duckdb"
}
```

DuckDB 是 Develop 内置联邦查询执行模式，不是 System 中注册的普通引擎实例；因此不得使用虚拟 `engine_id=0` 表达 DuckDB。查询目标发现时，`/api/v1/develop/engines` 只返回真实查询引擎，DuckDB 通过 `/api/v1/develop/query-modes` 暴露。

---

## 三、DevType 与 TaskProvider task_type 边界

`dev_type` 是 Develop 私有表字段，只用于 Develop 内部任务定义管理、查询过滤和执行分派。TaskProvider 标准接口、Orchestrator 编排引用、Monitor 回查和 `common.task_executions` 使用统一字段 `task_type`。Develop 对外暴露的 `task_type` 与内部 `dev_type` 一一映射，但外部模块不得直接依赖 `develop.dev_tasks.dev_type`。

| 值 | 含义 | content 推荐结构 |
|---|------|------------|
| `query` | SQL/MQL 查询 | `{"query_type": "sql", "query": "SELECT * FROM cities"}` |
| `workflow` | GIS 工作流 | `{"workflow_definition": {...}}` |
| `script` | 脚本开发；Notebook 形态使用 `content.notebook_path` 标识 | `{"script": "print('hello')", "language": "python"}` 或 `{"notebook_path": "demo.ipynb", "kernel": "python3"}` |

---

## 四、API 端点说明

### 5.1 POST /api/v1/develop/task-definitions - 创建开发任务

**请求体**：

```json
{
  "name": "查询大城市",
  "display_name": "查询人口大于100万的城市",
  "dev_type": "query",
  "content": {
    "query_type": "sql",
    "query": "SELECT * FROM cities WHERE population > 1000000"
  },
  "execution_config": {
    "engine_id": 1
  },
  "description": "查询所有大城市",
  "tags": ["城市", "人口"]
}
```

**响应**（200 OK）：返回完整 DevTask 对象

---

### 5.2 GET /api/v1/develop/task-definitions - 查询开发任务列表

**查询参数**：
- `dev_type`：按类型过滤
- `status`：按状态过滤
- `tag`：按标签过滤
- `keyword`：搜索名称或描述

---

### 5.3 POST /api/v1/develop/task-definitions/{id}/execute - 执行开发任务

**响应**：

```json
{
  "execution_id": "uuid-xxxx",
  "status": "pending"
}
```

---

## 六、相关文档

- Develop 执行记录统一写入 `common.task_executions`。
- [数据库架构](../数据库架构.md) - Develop 模块架构

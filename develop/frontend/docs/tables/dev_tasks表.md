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
| `editor_layout` | JSONB | NOT NULL, DEFAULT `{}` | DAG 编辑器节点坐标和视口，仅用于展示，不参与执行 |
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

其中 `locator`、`target_parent_locator` 等标准 ResourceLocator 形成工作流的存储 Engine 绑定。Engine 删除后任务和旧 Locator 保留，但不可继续执行。用户必须通过 Develop 的存储 Engine 绑定接口显式选择替代 Engine；重绑定只改写 Locator 的 `engine_id`，保留 path/type，并清除属于旧 Meta 快照的 `node_id/item_id`。`execution_config.engine_id` 表示工作流运行时，不能被存储 Engine 重绑定改写。

工作流编辑器布局单独保存在顶层 `editor_layout`：

```json
{
  "nodes": {
    "load_1": { "x": 120, "y": 240 }
  },
  "viewport": {
    "zoom": 1,
    "translate_x": 0,
    "translate_y": 0
  }
}
```

`editor_layout` 是纯展示状态。TaskProvider 执行器只读取 `content.workflow_definition` 和 `execution_config`，不得读取 `editor_layout`。

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
  "engine_id": 2
}
```

DuckDB 是 System 中 `tenant_id=NULL`、`is_builtin=true` 的真实联邦查询计算引擎。查询目标发现统一通过 `/api/v1/develop/engines` 返回真实 Engine，不存在虚拟 `engine_id=0`、空 Engine 或独立查询模式。

执行 DuckDB 任务时，Develop 根据 `execution_config.engine_id` 解析 Runtime，从 `content.query` 解析所引用的 Source Engine，为本次 execution 签发只读 Execution Authorization；独立 DuckDB Runtime 按 Source Engine 即时消费连接。`execution_config` 只保存 Runtime Engine ID，不得保存 Source Engine ID 列表、连接信息或 Token；每次执行都必须重新解析并授权。

### 2.5 定义覆盖、执行快照与并发缺口

#### 当前定义与执行快照

`dev_tasks` 保存任务的当前定义。普通更新提交非空 `content` 时整体替换该 JSONB 内容，不为每次编辑生成独立定义修订；任务模型和更新 DTO 均没有任务级 `metadata` 字段，不能按旧建议在此前端自建版本。

每次执行把内容、执行配置、超时和有效参数冻结到自己的 `common.task_executions.execution_config`；监督器从该 execution 还原运行输入，不重新读取可变任务定义。该快照固定的是本次输入，不是任务定义的版次或恢复接口，也不替代当前执行授权复核；队列、租约、授权与 Notebook 当前限制沿[执行监督契约](../../../../docs/spec/tasks/模块任务契约.md#develop-执行监督与授权)。

#### 正式并发要求与当前缺口

可变主资源的服务端正整数 `version`、事务校验／递增、`409/resource_version_conflict` 和前端保留草稿要求，以[资源版本与并发更新规范](../../../../docs/spec/api/资源版本与并发更新.md#资源版本与并发更新)为准。并发版本不代表业务版次或历史定义修订。

当前 `DevTask`、更新 DTO 和普通任务更新没有接入该 `version` 契约，普通更新仍调用无版本条件的 `Save`。工作流存储 Engine 重绑只使用本次服务端读取的 `updated_at` 做条件更新，冲突返回其既有 HTTP 409；它只保护该次读取至写入之间的窗口，不校验客户端编辑基线，也不能代表普通更新或全部任务写入已符合版本规范。此处记录实现缺口，不建立规范例外或前端替代路线。

#### 验证与证据边界

标准门禁与分版本结果见[Develop 验证入口](../../../../docs/spec/configuration/执行与模块策略.md#develop-验证入口与证据边界)。执行快照、重绑局部 CAS 或现有门禁通过，不得计为旧版本 409、冲突无副作用、版本迁移或前端草稿保留已验收；这些缺口按正式规范分别验证，不将本节事实说明作为实现已完成的证据。

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

### 5.4 GET /api/v1/develop/task-definitions/{id}/storage-engine-bindings - 查询存储 Engine 绑定

返回任务当前 ResourceLocator 引用的 Engine、引用数量、资源类型、可用状态，以及当前租户可选择的存储 Engine 候选。该接口不修改任务。

### 5.5 PUT /api/v1/develop/task-definitions/{id}/storage-engine-bindings/{source_engine_id} - 重绑定存储 Engine

请求体：

```json
{
  "target_engine_id": 15
}
```

Develop 校验任务归属、目标 Engine 生命周期和存储能力，并以目标 Engine 的 `storage.catalog_model` 校验全部 Locator type 的路径语义兼容性后，一次原子替换任务中指向 `source_engine_id` 的全部标准 ResourceLocator。响应返回更新后的任务和替换数量；本次服务端读取至写入之间发生并发修改时返回 `409 Conflict`。其时间戳 CAS 与正式 `version` 的差异见[当前并发缺口](#正式并发要求与当前缺口)，不能据此认定通用任务版本契约已接通。

---

## 六、相关文档

- Develop 执行记录统一写入 `common.task_executions`。
- [数据库架构](../数据库架构.md) - Develop 模块架构

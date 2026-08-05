# 查询工作台与查询任务功能说明

## 概述

查询工作台按 Engine 的 `capabilities.compute.query` 支持 SQL、MQL、Cypher 等查询语言。用户可以直接执行临时查询，也可以保存为可重用的 `query` 任务。当前 Develop 不具备 owner scheduler / `next_run_at` due claim 闭环，因此查询任务只支持手动执行，不提供自身定时调度配置。

## 功能特性

### 1. 查询工作台

**位置**: [develop/frontend/src/views/QueryEditor.vue](develop/frontend/src/views/QueryEditor.vue)

工作台固定采用左侧数据资源、右侧编辑器与结果上下分栏，小屏将数据资源收入抽屉：

- 数据资源直接消费 Meta resource-tree，并固定到当前选择的查询 Engine。
- 查询语言和结果形态来自 Engine capability，不按引擎类型硬编码。
- 执行时优先使用 Monaco 当前选区，没有选区时执行全文。
- 即时查询创建统一 execution，并在当前页面轮询结果和提供统一监控入口。
- 服务端限制结果预览规模，前端显示截断状态并只导出当前预览。
- 未保存内容在离开页面或加载其他任务前进行防丢失确认。
- 当前查询可保存为任务，支持名称、描述、标签和超时时间。

### 2. 查询任务管理

所有 Develop 任务统一在 `/develop/tasks` 管理。查询任务通过 `dev_type=query` 筛选、编辑、执行和删除；不保留 `/develop/sql-tasks` 或独立查询任务页面。

### 3. 任务元数据

每个 SQL 任务包含以下信息:
- **基本信息**: 名称、显示名称、描述
- **查询内容**: 完整查询语句和真实 `query_type`
- **执行目标**: 普通查询和 DuckDB 联邦查询都使用 System 中真实 Engine 的 `engine_id`
- **执行配置**: 超时时间(默认 300 秒)
- **标签**: 多个标签用于分类
- **状态**: active(活跃) / inactive(停用) / archived(归档)
- **执行历史**: 最后执行时间、执行状态、执行 ID

## 后端 API

### 查询目标发现 API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/develop/engines` | 获取 System 中具备 query 能力的真实引擎实例 |
| GET | `/api/v1/develop/engines/{id}/sample-query` | 通过只读 Execution Authorization 获取该引擎的可执行查询模板；可传 `locator` 为指定数据项生成模板 |

DuckDB 联邦查询计算引擎是 System 注册的内置 Engine，前端从 `/develop/engines` 获取其真实 ID，不创建 `id=0`、`engine_id=null` 或其他虚拟选项。

普通引擎查询模板会在执行期消费目标 Engine 的受控访问，从实时资源目录选择确认有数据的真实 leaf；指定 `locator` 时，Provider 必须针对该数据项生成模板并完成只读验证。资源发现失败或没有可查询数据时返回明确错误，不返回版本查询、`SELECT 1`、占位集合名或其他静态兜底。

DuckDB 执行前从 SQL 中解析已注册的 Source Engine 引用，为本次 execution 签发一次只读 Execution Authorization；独立 DuckDB Runtime 逐个消费连接后挂载。任务定义和执行记录不保存 Source Engine ID 列表、连接信息或 User Access Token。

### 即时查询执行 API

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/develop/executions` | 创建 `dev_type=query` 的 ad-hoc execution |
| GET | `/api/v1/develop/executions/{execution_id}` | 回查状态、错误和受限结果预览 |

即时查询 execution 使用 `module=develop`、`task_type=query`、`source_task_id=null`。请求在 `execution_config` 中保存查询内容、真实 Engine ID 和 timeout 快照。查询工作台不调用同步返回结果的 `/develop/execute`。

成功结果位于 `metadata.result`，至少包含 `columns`、`rows_count`、`rows_affected`、`effect`、`result_kind`、`result_limit`、`truncated` 和 `summary.preview_rows`；图查询可以附带 `graph_data`。结果预览上限由服务端 `QUERY_RESULT_LIMIT` 控制，默认 500 行。

### 查询任务管理 API

**基础路径**: `/api/v1/develop/task-definitions`

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/develop/task-definitions` | 创建开发任务定义，SQL 任务使用 `dev_type=query` |
| GET | `/api/v1/develop/task-definitions?dev_type=query` | 获取 SQL 任务列表 |
| GET | `/api/v1/develop/task-definitions/:id` | 获取任务详情 |
| PUT | `/api/v1/develop/task-definitions/:id` | 更新任务 |
| DELETE | `/api/v1/develop/task-definitions/:id` | 删除任务 |

### 创建任务请求示例

```json
{
  "name": "daily_user_report",
  "display_name": "每日用户报表",
  "dev_type": "query",
  "content": {
    "query_type": "sql",
    "query": "SELECT COUNT(*) FROM users WHERE created_at >= CURRENT_DATE"
  },
  "execution_config": {
    "engine_id": 1
  },
  "description": "统计每日新增用户数量",
  "tags": ["报表", "用户"],
  "timeout": 300
}
```

DuckDB 联邦查询任务的 `execution_config.engine_id` 保存真实 DuckDB Runtime Engine ID：

```json
{
  "name": "federated_city_report",
  "display_name": "跨源城市报表",
  "dev_type": "query",
  "content": {
    "query_type": "sql",
    "query": "SELECT * FROM <source_engine>.<schema>.<table> LIMIT 10"
  },
  "execution_config": {
    "engine_id": 2
  },
  "timeout": 300
}
```

### 任务执行

任务可以通过以下方式执行:

1. **手动执行**: 在统一任务管理页面点击"执行"按钮
2. **API 调用**: 通过 `POST /api/v1/develop/task-definitions/{id}/execute` 执行

执行结果会记录在 `common.task_executions` 表中,包含:
- 执行 ID
- 执行状态 (pending/running/success/failed)
- 执行结果 (查询返回的数据)
- 执行时间
- 错误信息(如果失败)

## 数据库结构

### 开发任务表 (develop.dev_tasks)

```sql
CREATE TABLE develop.dev_tasks (
  id SERIAL PRIMARY KEY,
  tenant_id INTEGER NOT NULL,
  name VARCHAR(255) NOT NULL,
  display_name VARCHAR(255),
  dev_type VARCHAR(50) NOT NULL,  -- 'query' | 'workflow' | 'script'

  -- 内容存储 (JSONB)
  content JSONB NOT NULL,  -- { "query_type": "sql", "query": "..." }

  -- 执行配置：统一使用真实查询 Engine ID，包括内置 DuckDB Runtime Engine
  execution_config JSONB,
  timeout INTEGER DEFAULT 300,

  -- 元数据
  description TEXT,
  tags TEXT[],
  created_by INTEGER,
  updated_by INTEGER,

  -- 状态
  status VARCHAR(50) DEFAULT 'active',
  last_execution_id VARCHAR(36),
  last_execution_status VARCHAR(50),
  last_run_at TIMESTAMP,

  -- 审计
  created_at TIMESTAMP DEFAULT NOW(),
  updated_at TIMESTAMP DEFAULT NOW(),
  deleted_at TIMESTAMP
);
```

### 执行记录表 (common.task_executions)

```sql
CREATE TABLE common.task_executions (
  id SERIAL PRIMARY KEY,
  tenant_id INTEGER NOT NULL,
  execution_id VARCHAR(255) UNIQUE NOT NULL,
  module VARCHAR(50) NOT NULL,
  task_type VARCHAR(100) NOT NULL,
  source VARCHAR(50) NOT NULL,
  source_task_id VARCHAR(255),
  source_task_name VARCHAR(255),
  parent_execution_id VARCHAR(36),

  -- 执行信息
  trigger_type VARCHAR(50) NOT NULL,  -- 'manual' | 'scheduled'
  triggered_by INTEGER,
  status VARCHAR(50) NOT NULL,
  progress INTEGER DEFAULT 0,
  current_step VARCHAR(255),

  -- 配置、错误和模块扩展数据
  execution_config JSONB,
  error_details JSONB,
  metadata JSONB,

  -- 统计
  rows_affected BIGINT,
  execution_time_ms BIGINT,

  -- 时间统计
  started_at TIMESTAMP,
  completed_at TIMESTAMP,
  created_at TIMESTAMP DEFAULT NOW(),
  updated_at TIMESTAMP DEFAULT NOW()
);
```

## 访问方式

### 独立访问
- 查询工作台: http://localhost:5178/sql
- 统一任务管理: http://localhost:5178/tasks

### Console 嵌入访问
- 查询工作台: http://localhost:5170/develop/sql
- 统一任务管理: http://localhost:5170/develop/tasks

### 生产环境 (通过 Gateway)
- 查询工作台: http://localhost:8000/develop/ → `/sql`
- 统一任务管理: http://localhost:8000/develop/ → `/tasks`

## 使用流程

### 1. 创建查询任务

1. 访问查询工作台
2. 选择具备 query capability 的 Engine
3. 选择数据资源并生成查询模板
4. 点击"保存为任务"按钮
5. 填写任务信息:
   - 任务名称(唯一标识)
   - 显示名称
   - 描述
   - 标签
   - 超时时间
6. 点击"保存"

### 2. 管理任务

1. 访问统一任务管理页面
2. 查看所有已保存的任务
3. 使用搜索和筛选功能定位任务
4. 操作任务:
   - **执行**: 立即执行任务
   - **编辑**: 修改任务配置
   - **删除**: 删除任务

### 3. 查看执行结果

1. 即时查询或任务执行后，系统返回 execution ID
2. 访问"执行监控"页面查看执行状态
3. 点击执行记录查看详细结果:
   - 查询结果数据
   - 执行时间
   - 影响行数
   - 错误信息(如果失败)

## 注意事项

1. **任务名称唯一性**: 同一租户下任务名称不能重复
2. **超时时间**: 默认 300 秒，最大不超过配置的 `MaxQueryTimeout`
3. **调度能力**: 当前 Develop 不声明自身定时调度能力；如后续需要，必须先补 owner scheduler / `next_run_at` due claim 闭环。
4. **资源权限**: 用户只能访问所属租户的资源和任务
5. **执行授权**: SQL 效果分类、非 SQL 只读边界和 Execution Authorization 由后端强制执行
6. **结果限制**: `QUERY_RESULT_LIMIT` 只控制 execution 预览，不代表查询数据总量

## 后续扩展

### 计划中的功能

1. **参数化查询**: 支持 SQL 参数占位符
2. **结果通知**: 执行完成后通过邮件/Webhook 通知
3. **结果导出**: 支持将查询结果导出为 CSV/Excel
4. **执行队列**: 并发控制和优先级管理
5. **任务依赖**: 支持任务之间的依赖关系
6. **版本管理**: SQL 内容的版本历史

## 技术实现

### 后端架构

- **Handler 层**:
  - `execution_handler.go` 创建和查询统一 execution
  - `query_handler.go` 只负责查询目标连接测试和真实查询模板
  - 查询任务定义统一由 DevTaskService 管理

- **Service 层**:
  - [develop/backend/internal/service/dev_task_service.go](develop/backend/internal/service/dev_task_service.go) - 任务 CRUD
  - [develop/backend/internal/service/dev_executor.go](develop/backend/internal/service/dev_executor.go) - 任务执行
  - [develop/backend/internal/service/sql_engine_service.go](develop/backend/internal/service/sql_engine_service.go) - SQL 执行引擎

- **Repository 层**:
  - [develop/backend/internal/repository/dev_task_repository.go](develop/backend/internal/repository/dev_task_repository.go)
  - `common/execution` - 统一执行记录仓库，写入 `common.task_executions`

### 前端架构

- **页面组件**:
  - `QueryEditor.vue` - 查询编辑器
  - `TaskManagement.vue` - query/workflow/script 统一任务管理

- **对话框组件**:
  - `SaveQueryDialog.vue` - 保存查询任务对话框

- **API 客户端**:
  - `develop/frontend/src/api/query.js` - 查询发现、连接测试和任务保存 API
  - `develop/frontend/src/api/execution.js` - ad-hoc execution 创建与状态回查

- **依赖包**:
  - `dayjs` - 时间格式化

## 总结

查询工作台将数据资源浏览、能力驱动编辑、统一 execution、受限结果预览和可选任务保存收敛为一条路径。即时查询与已保存任务共享执行记录和监控体系，任务只在 `/develop/tasks` 统一管理。

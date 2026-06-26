# SQL 任务功能说明

## 概述

SQL 开发模块支持将 SQL 查询保存为可重用任务。当前 Develop 不具备 owner scheduler / `next_run_at` due claim 闭环，因此 SQL 查询任务只支持手动执行，不提供自身定时调度配置。

## 功能特性

### 1. SQL 编辑器增强

**位置**: [develop/frontend/src/views/QueryEditor.vue](develop/frontend/src/views/QueryEditor.vue)

新增功能:
- **保存为任务按钮**: 在工具栏添加"保存为任务"按钮
- 可将当前编辑的 SQL 保存为可重用的任务
- 支持配置任务名称、描述、标签、超时时间等

### 2. SQL 任务管理

**位置**: [develop/frontend/src/views/QueryTasks.vue](develop/frontend/src/views/QueryTasks.vue)

功能列表:
- **任务列表**: 查看所有已保存的 SQL 任务
- **搜索过滤**: 按名称、描述、状态筛选
- **快速执行**: 一键执行任务
- **任务编辑**: 修改任务配置
- **任务删除**: 删除不需要的任务

### 3. 任务元数据

每个 SQL 任务包含以下信息:
- **基本信息**: 名称、显示名称、描述
- **SQL 内容**: 完整的 SQL 语句
- **执行目标**: 普通 SQL 使用数据库资源 ID；DuckDB 联邦查询使用 `query_mode=duckdb`
- **执行配置**: 超时时间(默认 300 秒)
- **标签**: 多个标签用于分类
- **状态**: active(活跃) / inactive(停用) / archived(归档)
- **执行历史**: 最后执行时间、执行状态、执行 ID

## 后端 API

### 查询目标发现 API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/develop/engines` | 获取 System 中具备 query 能力的真实引擎实例 |
| GET | `/api/v1/develop/query-modes` | 获取 Develop 内置查询模式，例如 DuckDB 联邦查询 |

DuckDB 联邦查询不是 System Engine，不得追加为 `id=0` 的虚拟引擎。

### SQL 任务管理 API

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

DuckDB 联邦查询任务的 `execution_config` 使用内置查询模式，不写虚拟引擎 ID：

```json
{
  "name": "federated_city_report",
  "display_name": "跨源城市报表",
  "dev_type": "query",
  "content": {
    "query_type": "sql",
    "query": "SELECT * FROM postgres_main.public.cities LIMIT 10"
  },
  "execution_config": {
    "query_mode": "duckdb"
  },
  "timeout": 300
}
```

### 任务执行

任务可以通过以下方式执行:

1. **手动执行**: 在任务管理页面点击"执行"按钮
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

  -- 执行配置：普通 SQL 为 { "engine_id": 1 }，DuckDB 联邦查询为 { "query_mode": "duckdb" }
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
- SQL 编辑器: http://localhost:5178/sql
- SQL 任务: http://localhost:5178/sql-tasks

### Console 嵌入访问
- SQL 编辑器: http://localhost:5170 (通过控制台导航访问)
- SQL 任务: http://localhost:5170 (通过控制台导航访问)

### 生产环境 (通过 Gateway)
- SQL 编辑器: http://localhost:8000/develop/ → SQL 编辑器
- SQL 任务: http://localhost:8000/develop/ → SQL 任务

## 使用流程

### 1. 创建 SQL 任务

1. 访问 SQL 编辑器页面
2. 选择数据源
3. 编写 SQL 语句
4. 点击"保存为任务"按钮
5. 填写任务信息:
   - 任务名称(唯一标识)
   - 显示名称
   - 描述
   - 标签
   - 超时时间
6. 点击"保存"

### 2. 管理任务

1. 访问 SQL 任务页面
2. 查看所有已保存的任务
3. 使用搜索和筛选功能定位任务
4. 操作任务:
   - **执行**: 立即执行任务
   - **编辑**: 修改任务配置
   - **删除**: 删除任务

### 3. 查看执行结果

1. 执行任务后,系统返回执行 ID
2. 访问"执行监控"页面查看执行状态
3. 点击执行记录查看详细结果:
   - 查询结果数据
   - 执行时间
   - 影响行数
   - 错误信息(如果失败)

## 注意事项

1. **任务名称唯一性**: 同一租户下任务名称不能重复
2. **超时时间**: 默认 300 秒,最大不超过配置的 `MaxQueryTimeout`
3. **调度能力**: 当前 Develop 不声明自身定时调度能力；如后续需要，必须先补 owner scheduler / `next_run_at` due claim 闭环。
4. **资源权限**: 用户只能访问所属租户的资源和任务
5. **SQL 安全**: 建议对 SQL 内容进行审核,避免危险操作

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

- **Handler 层**: [develop/backend/internal/api/sql_handler.go](develop/backend/internal/api/sql_handler.go)
  - 查询执行相关 API
  - SQL 任务定义统一由 DevTaskService 管理

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
  - `QueryTasks.vue` - 查询任务管理

- **对话框组件**:
  - `SaveQueryDialog.vue` - 保存查询任务对话框

- **API 客户端**:
  - [develop/frontend/src/api/query.js](develop/frontend/src/api/query.js) - 查询与查询任务 API

- **依赖包**:
  - `dayjs` - 时间格式化

## 总结

SQL 任务功能为 ADDP 平台的 SQL 开发模块提供了任务管理能力,使用户能够:

✅ 保存常用的 SQL 查询为可重用任务
✅ 统一管理所有 SQL 任务
✅ 追踪任务执行历史和结果

该功能已完全集成到现有的 Develop 模块架构中,遵循 ADDP 的统一设计模式。

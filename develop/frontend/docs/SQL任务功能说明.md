# SQL 任务功能说明

## 概述

SQL 开发模块现已支持将 SQL 查询保存为可重用的任务,并支持定时调度执行。

## 功能特性

### 1. SQL 编辑器增强

**位置**: [develop/frontend/src/views/SQLEditor.vue](develop/frontend/src/views/SQLEditor.vue:51)

新增功能:
- **保存为任务按钮**: 在工具栏添加"保存为任务"按钮
- 可将当前编辑的 SQL 保存为可重用的任务
- 支持配置任务名称、描述、标签、超时时间等

### 2. SQL 任务管理

**位置**: [develop/frontend/src/views/SQLTasks.vue](develop/frontend/src/views/SQLTasks.vue)

功能列表:
- **任务列表**: 查看所有已保存的 SQL 任务
- **搜索过滤**: 按名称、描述、状态、调度状态筛选
- **快速执行**: 一键执行任务
- **任务编辑**: 修改任务配置
- **任务删除**: 删除不需要的任务

### 3. 任务调度配置

**位置**: [develop/frontend/src/components/SaveSQLDialog.vue](develop/frontend/src/components/SaveSQLDialog.vue:86-89)

**使用 common-frontend 统一调度组件**: `ScheduleConfig`

调度功能:
- **启用/禁用调度**: 开关控制是否启用定时执行
- **11 种快捷预设**: 每分钟、每15分钟、每30分钟、每小时、每天凌晨、每周一零点、每月1号零点等
- **4 种调度模式**:
  - `daily`: 每天指定时刻
  - `weekly`: 指定星期几 + 时刻
  - `monthly`: 指定日期 + 时刻
  - `cron`: 自定义标准 Cron 表达式
- **实时中文描述**: 自动将 Cron 表达式翻译为可读描述
- **在线工具**: 集成 Crontab.guru 链接

常用 Cron 表达式:
```
* * * * *       # 每分钟执行
*/15 * * * *    # 每15分钟执行
0 0 * * *       # 每天凌晨执行
0 0 * * 1       # 每周一零点执行
0 0 1 * *       # 每月1号零点执行
```

### 4. 任务元数据

每个 SQL 任务包含以下信息:
- **基本信息**: 名称、显示名称、描述
- **SQL 内容**: 完整的 SQL 语句
- **数据源**: 关联的数据库资源 ID
- **执行配置**: 超时时间(默认 300 秒)
- **调度配置**: Cron 表达式、是否启用
- **标签**: 多个标签用于分类
- **状态**: active(活跃) / inactive(停用) / archived(归档)
- **执行历史**: 最后执行时间、执行状态、执行 ID

## 后端 API

### SQL 任务管理 API

**基础路径**: `/api/develop/sql/tasks`

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/develop/sql/tasks` | 创建 SQL 任务 |
| GET | `/api/develop/sql/tasks` | 获取任务列表 |
| GET | `/api/develop/sql/tasks/:id` | 获取任务详情 |
| PUT | `/api/develop/sql/tasks/:id` | 更新任务 |
| DELETE | `/api/develop/sql/tasks/:id` | 删除任务 |

### 创建任务请求示例

```json
{
  "name": "daily_user_report",
  "display_name": "每日用户报表",
  "engine_id": 1,
  "sql": "SELECT COUNT(*) FROM users WHERE created_at >= CURRENT_DATE",
  "description": "统计每日新增用户数量",
  "tags": ["报表", "用户"],
  "timeout": 300,
  "is_scheduled": true,
  "schedule": "0 0 * * *"
}
```

### 任务执行

任务可以通过以下方式执行:

1. **手动执行**: 在任务管理页面点击"执行"按钮
2. **定时执行**: 通过配置的 Cron 表达式自动触发(需要调度器支持)
3. **API 调用**: 通过 `POST /api/develop/items/:id/execute` 执行

执行结果会记录在 `dev_executions` 表中,包含:
- 执行 ID
- 执行状态 (pending/running/success/failed)
- 执行结果 (查询返回的数据)
- 执行时间
- 错误信息(如果失败)

## 数据库结构

### 开发项表 (develop.dev_items)

```sql
CREATE TABLE develop.dev_items (
  id SERIAL PRIMARY KEY,
  tenant_id INTEGER NOT NULL,
  name VARCHAR(255) NOT NULL,
  display_name VARCHAR(255),
  dev_type VARCHAR(50) NOT NULL,  -- 'sql' | 'workflow' | 'script'

  -- 内容存储 (JSONB)
  content JSONB NOT NULL,  -- { "sql": "...", "engine_id": 1 }

  -- 执行配置
  engine_id INTEGER,
  schedule VARCHAR(100),
  is_scheduled BOOLEAN DEFAULT false,
  timeout INTEGER DEFAULT 300,

  -- 元数据
  description TEXT,
  tags TEXT[],
  created_by INTEGER,
  updated_by INTEGER,

  -- 状态
  status VARCHAR(50) DEFAULT 'active',
  last_execution_id INTEGER,
  last_execution_status VARCHAR(50),
  last_executed_at TIMESTAMP,

  -- 审计
  created_at TIMESTAMP DEFAULT NOW(),
  updated_at TIMESTAMP DEFAULT NOW(),
  deleted_at TIMESTAMP
);
```

### 执行记录表 (develop.dev_executions)

```sql
CREATE TABLE develop.dev_executions (
  id SERIAL PRIMARY KEY,
  dev_item_id INTEGER REFERENCES develop.dev_items(id),
  tenant_id INTEGER NOT NULL,
  execution_id VARCHAR(50) UNIQUE NOT NULL,
  dev_type VARCHAR(50) NOT NULL,

  -- 执行信息
  trigger_type VARCHAR(50),  -- 'manual' | 'scheduled' | 'api'
  triggered_by INTEGER,
  status VARCHAR(50) NOT NULL,
  progress INTEGER DEFAULT 0,

  -- 结果
  result JSONB,
  error_message TEXT,
  rows_affected BIGINT,
  result_size_bytes BIGINT,

  -- 时间统计
  started_at TIMESTAMP,
  completed_at TIMESTAMP,
  execution_time_ms BIGINT,

  created_at TIMESTAMP DEFAULT NOW()
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
6. 配置调度(可选):
   - 启用调度开关
   - 填写 Cron 表达式
7. 点击"保存"

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
3. **调度执行**: 需要配置调度器服务才能自动执行定时任务
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
  - 新增 5 个 API endpoint
  - 集成 DevItemService 管理任务

- **Service 层**:
  - [develop/backend/internal/service/dev_item_service.go](develop/backend/internal/service/dev_item_service.go) - 任务 CRUD
  - [develop/backend/internal/service/dev_executor.go](develop/backend/internal/service/dev_executor.go) - 任务执行
  - [develop/backend/internal/service/sql_engine_service.go](develop/backend/internal/service/sql_engine_service.go) - SQL 执行引擎

- **Repository 层**:
  - [develop/backend/internal/repository/dev_item_repository.go](develop/backend/internal/repository/dev_item_repository.go)
  - [develop/backend/internal/repository/dev_execution_repository.go](develop/backend/internal/repository/dev_execution_repository.go)

### 前端架构

- **页面组件**:
  - `SQLEditor.vue` - SQL 编辑器(已增强)
  - `SQLTasks.vue` - SQL 任务管理(新增)

- **对话框组件**:
  - `SaveSQLDialog.vue` - 保存任务对话框(新增)

- **API 客户端**:
  - [develop/frontend/src/api/sql.js](develop/frontend/src/api/sql.js) - 新增 5 个 API 方法

- **依赖包**:
  - `@addp/common-frontend` - 统一调度组件 (ScheduleConfig, ScheduleDisplay)
  - `dayjs` - 时间格式化

## 总结

SQL 任务功能为 ADDP 平台的 SQL 开发模块提供了完整的任务管理和调度能力,使用户能够:

✅ 保存常用的 SQL 查询为可重用任务
✅ 配置定时调度自动执行
✅ 统一管理所有 SQL 任务
✅ 追踪任务执行历史和结果

该功能已完全集成到现有的 Develop 模块架构中,遵循 ADDP 的统一设计模式。

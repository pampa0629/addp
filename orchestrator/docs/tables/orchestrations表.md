# orchestrations 表结构和 API 说明

## 一、表结构概览

`orchestrator.orchestrations` 表是 Orchestrator 模块的核心表，用于存储工作流编排定义。每条记录代表一个可执行的 DAG（有向无环图）工作流，支持定时调度和手动触发。

### 核心功能

- **DAG 编排定义**：存储工作流的步骤序列和依赖关系
- **动态引擎调用**：支持通过 `engine_identifier` 动态发现和调用执行引擎
- **定时调度**：基于 Cron 表达式的自动执行
- **向后兼容**：同时支持新的动态模式和旧的硬编码模块模式
- **多租户隔离**：通过 `tenant_id` 字段实现租户数据隔离
- **软删除**：支持逻辑删除，保留历史记录

---

## 二、表结构定义

### 2.1 核心字段

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | SERIAL | PRIMARY KEY | 编排唯一标识 |
| `tenant_id` | INTEGER | NOT NULL, INDEXED | 租户 ID，用于多租户隔离 |
| `name` | VARCHAR(128) | NOT NULL | 编排名称 |
| `description` | VARCHAR(512) | | 编排描述，说明工作流用途 |
| `steps` | JSONB | NOT NULL | 步骤定义（DAG 结构），存储工作流的所有步骤 |
| `enabled` | BOOLEAN | DEFAULT false | 是否启用，控制定时调度是否生效 |
| `schedule` | VARCHAR(128) | | Cron 表达式，定义定时调度规则 |
| `created_at` | TIMESTAMP | DEFAULT NOW() | 创建时间 |
| `updated_at` | TIMESTAMP | DEFAULT NOW() | 更新时间 |
| `deleted_at` | TIMESTAMP | INDEXED | 软删除时间戳，NULL 表示未删除 |

### 2.2 数据库索引

| 索引名 | 字段 | 类型 | 说明 |
|--------|------|------|------|
| `idx_orchestrations_tenant` | `tenant_id` | 普通索引 | 租户查询优化 |
| `idx_orchestrations_deleted` | `deleted_at` | 普通索引 | 软删除查询优化 |

### 2.3 外键关系

本表不直接包含外键约束，但通过以下方式关联其他表：

- `tenant_id` → `system.tenants.id` (逻辑外键，非数据库约束)
- `steps[].engine_identifier` → `system.engines.unique_identifier` (动态引用)

---

## 三、JSONB 字段详细结构

### 3.1 Steps 字段（步骤定义）

**类型**：JSONB
**作用**：存储工作流的所有步骤，每个步骤包含执行信息和依赖关系

#### Go 结构定义

```go
type Steps []Step

type Step struct {
    ID               string                 `json:"id"`                        // 步骤唯一标识
    Name             string                 `json:"name"`                      // 步骤名称

    // 新架构：动态引擎（推荐）
    EngineIdentifier string                 `json:"engine_identifier,omitempty"` // 引擎标识符，如 "meta.scanner.default"

    // 旧架构：硬编码模块（向后兼容）
    Module           string                 `json:"module,omitempty"`          // 模块名，如 "transfer"
    Action           string                 `json:"action,omitempty"`          // 动作名，如 "execute"
    Endpoint         string                 `json:"endpoint,omitempty"`        // API 端点
    Method           string                 `json:"method,omitempty"`          // HTTP 方法

    Parameters       map[string]interface{} `json:"parameters"`                // 步骤参数
    DependsOn        []string               `json:"depends_on"`                // 依赖的步骤 ID 列表
    Timeout          int                    `json:"timeout"`                   // 超时时间（秒）
}
```

#### 新架构示例（推荐）

使用 `engine_identifier` 动态调用引擎：

```json
{
  "steps": [
    {
      "id": "step1",
      "name": "扫描元数据",
      "engine_identifier": "meta.scanner.default",
      "parameters": {
        "engine_id": 1,
        "schema_names": ["public"],
        "scan_depth": "deep"
      },
      "depends_on": [],
      "timeout": 300
    },
    {
      "id": "step2",
      "name": "生成 MVT 瓦片",
      "engine_identifier": "manager.mvt.default",
      "parameters": {
        "engine_id": 1,
        "schema": "public",
        "table": "cities",
        "max_zoom": 18
      },
      "depends_on": ["step1"],
      "timeout": 600
    },
    {
      "id": "step3",
      "name": "数据导出",
      "engine_identifier": "transfer.exporter.default",
      "parameters": {
        "task_id": 10,
        "format": "csv"
      },
      "depends_on": ["step1"],
      "timeout": 1800
    }
  ]
}
```

#### 旧架构示例（向后兼容）

使用硬编码模块名和端点：

```json
{
  "steps": [
    {
      "id": "step1",
      "name": "扫描元数据",
      "module": "meta",
      "action": "scan",
      "endpoint": "/api/scan/tasks",
      "method": "POST",
      "parameters": {
        "engine_id": 1
      },
      "depends_on": [],
      "timeout": 300
    }
  ]
}
```

#### 字段说明

| 字段 | 必填 | 说明 |
|------|------|------|
| `id` | 是 | 步骤唯一标识，用于依赖关系引用 |
| `name` | 是 | 步骤显示名称 |
| `engine_identifier` | 新架构必填 | 引擎标识符，从 System 的能力注册中心获取 |
| `module` | 旧架构必填 | 硬编码模块名（transfer/meta/manager） |
| `endpoint` | 旧架构必填 | API 端点路径 |
| `method` | 旧架构必填 | HTTP 方法（POST/GET/PUT/DELETE） |
| `parameters` | 是 | 步骤参数，传递给执行引擎 |
| `depends_on` | 是 | 依赖的步骤 ID 列表，空数组表示无依赖 |
| `timeout` | 是 | 超时时间（秒），0 表示无限制 |

---

## 四、API 端点说明

### 4.1 外部 API（需要 JWT 认证）

#### 创建编排

```
POST /api/orchestrations
```

**请求头**：
```
Authorization: Bearer <jwt_token>
Content-Type: application/json
```

**请求体**：

```json
{
  "name": "数据处理流水线",
  "description": "自动扫描元数据、生成瓦片并导出数据",
  "steps": [
    {
      "id": "step1",
      "name": "扫描元数据",
      "engine_identifier": "meta.scanner.default",
      "parameters": {
        "engine_id": 1,
        "schema_names": ["public"]
      },
      "depends_on": [],
      "timeout": 300
    },
    {
      "id": "step2",
      "name": "生成瓦片",
      "engine_identifier": "manager.mvt.default",
      "parameters": {
        "engine_id": 1,
        "schema": "public",
        "table": "cities"
      },
      "depends_on": ["step1"],
      "timeout": 600
    }
  ],
  "enabled": true,
  "schedule": "0 2 * * *"
}
```

**响应**（201 Created）：

```json
{
  "id": 1,
  "tenant_id": 1,
  "name": "数据处理流水线",
  "description": "自动扫描元数据、生成瓦片并导出数据",
  "steps": [...],
  "enabled": true,
  "schedule": "0 2 * * *",
  "created_at": "2026-01-01T10:00:00Z",
  "updated_at": "2026-01-01T10:00:00Z"
}
```

---

#### 列出编排

```
GET /api/orchestrations
```

**查询参数**：
- `page`（可选）：页码，默认 1
- `page_size`（可选）：每页条数，默认 20

**响应**（200 OK）：

```json
{
  "orchestrations": [
    {
      "id": 1,
      "tenant_id": 1,
      "name": "数据处理流水线",
      "description": "自动扫描元数据、生成瓦片并导出数据",
      "enabled": true,
      "schedule": "0 2 * * *",
      "created_at": "2026-01-01T10:00:00Z",
      "updated_at": "2026-01-01T10:00:00Z"
    }
  ],
  "total": 1,
  "page": 1,
  "page_size": 20
}
```

**说明**：返回列表不包含 `steps` 字段以减少响应大小

---

#### 获取编排详情

```
GET /api/orchestrations/:id
```

**响应**（200 OK）：

```json
{
  "id": 1,
  "tenant_id": 1,
  "name": "数据处理流水线",
  "description": "自动扫描元数据、生成瓦片并导出数据",
  "steps": [
    {
      "id": "step1",
      "name": "扫描元数据",
      "engine_identifier": "meta.scanner.default",
      "parameters": {
        "engine_id": 1,
        "schema_names": ["public"]
      },
      "depends_on": [],
      "timeout": 300
    }
  ],
  "enabled": true,
  "schedule": "0 2 * * *",
  "created_at": "2026-01-01T10:00:00Z",
  "updated_at": "2026-01-01T10:00:00Z"
}
```

**说明**：包含完整的 `steps` 字段

---

#### 更新编排

```
PUT /api/orchestrations/:id
```

**请求体**：同创建编排

**响应**（200 OK）：返回更新后的编排对象

**特殊行为**：
- 如果 `enabled` 或 `schedule` 发生变化，自动重新调度定时任务
- 如果 `enabled` 从 true 变为 false，停止定时调度
- 修改 `steps` 不影响正在运行的执行实例

---

#### 删除编排

```
DELETE /api/orchestrations/:id
```

**响应**（200 OK）：

```json
{
  "message": "编排删除成功"
}
```

**说明**：
- 软删除，设置 `deleted_at` 为当前时间
- 自动停止该编排的定时调度
- 不影响已创建的执行实例

---

#### 手动触发执行

```
POST /api/orchestrations/:id/execute
```

**响应**（202 Accepted）：

```json
{
  "execution_id": 100
}
```

**说明**：
- 创建新的执行实例并异步执行
- 立即返回 `execution_id`，不等待执行完成
- 使用 Go 协程实现异步执行

---

#### 列出执行记录

```
GET /api/orchestrations/:id/executions
```

**查询参数**：
- `limit`（可选）：记录数，默认 20
- `offset`（可选）：偏移量，默认 0

**响应**（200 OK）：

```json
{
  "items": [
    {
      "id": 100,
      "orchestration_id": 1,
      "tenant_id": 1,
      "status": "completed",
      "current_step": "step2",
      "started_at": "2026-01-01T10:00:00Z",
      "completed_at": "2026-01-01T10:15:00Z",
      "created_at": "2026-01-01T10:00:00Z"
    }
  ],
  "total": 1
}
```

---

## 五、权限控制

### 5.1 权限模型

所有 API 端点都需要 JWT 认证，自动进行租户隔离：

| 操作 | 权限要求 | 说明 |
|------|---------|------|
| 查看编排 | 本租户用户 | 仅返回本租户的编排 |
| 创建编排 | 本租户用户 | 自动关联到用户的租户 |
| 修改编排 | 本租户用户 | 仅能修改本租户的编排 |
| 删除编排 | 本租户用户 | 仅能删除本租户的编排 |
| 执行编排 | 本租户用户 | 仅能执行本租户的编排 |

### 5.2 租户隔离

- 所有查询自动添加 `WHERE tenant_id = <当前用户租户ID>` 条件
- SuperAdmin 不能查看其他租户的编排（编排是租户私有资源）
- 通过中间件 `SystemAuthMiddleware` 自动注入 `tenant_id`

---

## 六、执行流程

### 6.1 DAG 构建和拓扑排序

创建执行实例后，Executor 服务执行以下流程：

1. **构建 DAG**：根据 `steps` 和 `depends_on` 构建邻接表
2. **拓扑排序**：使用 Kahn 算法对步骤排序，检测循环依赖
3. **顺序执行**：按拓扑顺序执行每个步骤
4. **结果存储**：每个步骤的结果存储到 `executions.step_results`

### 6.2 步骤执行

**新架构**（动态引擎）：

```
1. 从 EngineRegistry 获取引擎配置（根据 engine_identifier）
2. 使用 TaskClient 创建任务
3. 使用 TaskClient 执行任务
4. 轮询任务状态直到完成或超时
5. 存储结果到 step_results
```

**旧架构**（硬编码模块）：

```
1. 根据 module 获取模块 URL
2. 使用 ModuleClient 调用指定端点
3. 等待响应
4. 存储结果到 step_results
```

---

## 七、定时调度

### 7.1 调度机制

- 使用 `robfig/cron/v3` 库实现定时调度
- 服务启动时加载所有 `enabled=true` 的编排
- 为每个编排创建 cron 任务
- 维护 `entryIDs` 映射管理任务

### 7.2 Cron 表达式格式

```
┌───────────── 分钟 (0 - 59)
│ ┌───────────── 小时 (0 - 23)
│ │ ┌───────────── 日 (1 - 31)
│ │ │ ┌───────────── 月 (1 - 12)
│ │ │ │ ┌───────────── 星期 (0 - 6) (0 = 星期日)
│ │ │ │ │
* * * * *
```

**示例**：
- `0 2 * * *` - 每天凌晨 2 点执行
- `*/10 * * * *` - 每 10 分钟执行
- `0 0 * * 0` - 每周日午夜执行
- `0 9 1 * *` - 每月 1 号上午 9 点执行

---

## 八、使用示例

### 8.1 创建简单的单步编排

```bash
curl -X POST http://localhost:8084/api/orchestrations \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "每日元数据扫描",
    "description": "每天凌晨2点自动扫描所有引擎的元数据",
    "steps": [
      {
        "id": "scan",
        "name": "扫描元数据",
        "engine_identifier": "meta.scanner.default",
        "parameters": {
          "engine_id": 1,
          "schema_names": ["public"],
          "scan_depth": "deep"
        },
        "depends_on": [],
        "timeout": 600
      }
    ],
    "enabled": true,
    "schedule": "0 2 * * *"
  }'
```

### 8.2 创建复杂的多步依赖编排

```bash
curl -X POST http://localhost:8084/api/orchestrations \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "完整数据处理流水线",
    "description": "扫描→生成瓦片→导出→通知",
    "steps": [
      {
        "id": "scan",
        "name": "扫描元数据",
        "engine_identifier": "meta.scanner.default",
        "parameters": {"engine_id": 1},
        "depends_on": [],
        "timeout": 300
      },
      {
        "id": "mvt",
        "name": "生成MVT瓦片",
        "engine_identifier": "manager.mvt.default",
        "parameters": {
          "engine_id": 1,
          "schema": "public",
          "table": "cities"
        },
        "depends_on": ["scan"],
        "timeout": 600
      },
      {
        "id": "export",
        "name": "导出数据",
        "engine_identifier": "transfer.exporter.default",
        "parameters": {
          "task_id": 10,
          "format": "csv"
        },
        "depends_on": ["scan"],
        "timeout": 1800
      },
      {
        "id": "notify",
        "name": "发送通知",
        "engine_identifier": "notification.email.default",
        "parameters": {
          "to": "admin@example.com",
          "subject": "数据处理完成"
        },
        "depends_on": ["mvt", "export"],
        "timeout": 30
      }
    ],
    "enabled": false,
    "schedule": ""
  }'
```

### 8.3 手动触发执行

```bash
curl -X POST http://localhost:8084/api/orchestrations/1/execute \
  -H "Authorization: Bearer $TOKEN"
```

**响应**：
```json
{
  "execution_id": 100
}
```

### 8.4 查询执行结果

```bash
curl http://localhost:8084/api/orch-executions/100 \
  -H "Authorization: Bearer $TOKEN"
```

---

## 九、重要说明

### 9.1 步骤 ID 命名规范

- 必须在编排内唯一
- 建议使用小写字母和下划线，如 `scan_metadata`、`generate_tiles`
- 避免使用特殊字符和空格

### 9.2 依赖关系约束

- `depends_on` 中引用的步骤 ID 必须存在
- 不允许循环依赖（系统会在执行时检测并报错）
- 空数组 `[]` 表示该步骤可以立即执行

### 9.3 并行执行

当多个步骤没有依赖关系时，系统会自动并行执行：

```json
{
  "steps": [
    {"id": "step1", "depends_on": []},
    {"id": "step2", "depends_on": []},
    {"id": "step3", "depends_on": ["step1", "step2"]}
  ]
}
```

执行顺序：`step1` 和 `step2` 并行 → 等待两者完成 → `step3`

### 9.4 超时处理

- `timeout` 为 0 表示无限制等待（不推荐）
- 超时后步骤标记为 `failed`，不会继续执行后续步骤
- 整个执行实例标记为 `failed`

### 9.5 向后兼容性

系统同时支持新旧两种步骤定义模式：

- **优先使用新架构**（`engine_identifier`）：更灵活，支持动态引擎发现
- **旧架构仍然可用**（`module` + `endpoint`）：确保现有编排不受影响
- 不能在同一个步骤中混用两种模式

---

## 十、相关文档

- [executions 表](./executions表.md) - 执行实例表，存储编排的运行记录
- [数据库架构](../数据库架构.md) - Orchestrator 模块整体架构
- [System 模块 - engines 表](../../system/docs/tables/engines表.md) - 引擎注册和能力声明
- [Orchestrator 模块说明](../../CLAUDE.md) - 模块整体架构和设计理念

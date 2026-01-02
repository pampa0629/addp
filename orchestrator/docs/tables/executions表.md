# executions 表结构和 API 说明

## 一、表结构概览

`orchestrator.executions` 表用于存储编排的执行实例记录。每条记录代表一次工作流的执行过程，包含执行状态、当前步骤、每个步骤的执行结果等信息。

### 核心功能

- **执行状态跟踪**：记录工作流的执行状态（pending/running/completed/failed）
- **步骤结果存储**：保存每个步骤的执行结果、耗时、错误信息
- **执行历史**：提供完整的执行审计记录
- **异步执行**：支持 Go 协程异步执行，立即返回 execution_id
- **多租户隔离**：通过 `tenant_id` 字段实现数据隔离

---

## 二、表结构定义

### 2.1 核心字段

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | SERIAL | PRIMARY KEY | 执行实例唯一标识 |
| `orchestration_id` | INTEGER | NOT NULL, INDEXED | 关联的编排 ID |
| `tenant_id` | INTEGER | NOT NULL, INDEXED | 租户 ID |
| `status` | VARCHAR(32) | NOT NULL | 执行状态：pending/running/completed/failed |
| `current_step` | VARCHAR(64) | | 当前执行的步骤 ID |
| `step_results` | JSONB | | 所有步骤的执行结果 |
| `error_message` | TEXT | | 错误信息（执行失败时） |
| `started_at` | TIMESTAMP | | 开始执行时间 |
| `completed_at` | TIMESTAMP | | 完成时间（成功或失败） |
| `created_at` | TIMESTAMP | DEFAULT NOW() | 创建时间 |

### 2.2 数据库索引

| 索引名 | 字段 | 类型 | 说明 |
|--------|------|------|------|
| `idx_executions_orchestration` | `orchestration_id` | 普通索引 | 查询编排的执行记录 |
| `idx_executions_tenant` | `tenant_id` | 普通索引 | 租户隔离查询 |
| `idx_executions_status` | `status` | 普通索引 | 按状态筛选（如查询运行中的任务） |

### 2.3 外键关系

- `orchestration_id` → `orchestrator.orchestrations.id` (通过 GORM 关联)
- `tenant_id` → `system.tenants.id` (逻辑外键)

---

## 三、JSONB 字段详细结构

### 3.1 StepResults 字段（步骤结果集合）

**类型**：JSONB
**作用**：存储每个步骤的执行结果，key 为步骤 ID，value 为步骤结果对象

#### Go 结构定义

```go
type StepResults map[string]StepResult

type StepResult struct {
    Status    string                 `json:"status"`     // "success" 或 "failed"
    Result    map[string]interface{} `json:"result"`     // 步骤返回的结果数据
    Error     string                 `json:"error"`      // 错误信息（失败时）
    StartedAt time.Time              `json:"started_at"` // 开始时间
    EndedAt   time.Time              `json:"ended_at"`   // 结束时间
    Duration  int64                  `json:"duration"`   // 执行耗时（毫秒）
}
```

#### 示例

```json
{
  "scan": {
    "status": "success",
    "result": {
      "schemas_scanned": 2,
      "tables_scanned": 50,
      "total_rows": 1000000
    },
    "error": "",
    "started_at": "2026-01-01T10:00:00Z",
    "ended_at": "2026-01-01T10:05:00Z",
    "duration": 300000
  },
  "mvt": {
    "status": "success",
    "result": {
      "tiles_generated": 5000,
      "total_size_mb": 120.5
    },
    "error": "",
    "started_at": "2026-01-01T10:05:00Z",
    "ended_at": "2026-01-01T10:15:00Z",
    "duration": 600000
  },
  "export": {
    "status": "failed",
    "result": {},
    "error": "目标存储空间不足",
    "started_at": "2026-01-01T10:05:00Z",
    "ended_at": "2026-01-01T10:06:00Z",
    "duration": 60000
  }
}
```

---

## 四、状态流转

### 4.1 状态值说明

| 状态 | 说明 | 触发条件 |
|------|------|---------|
| `pending` | 待执行 | 创建执行实例但尚未开始 |
| `running` | 执行中 | Go 协程开始执行 DAG |
| `completed` | 执行成功 | 所有步骤都成功完成 |
| `failed` | 执行失败 | 任一步骤失败或超时 |

### 4.2 状态流转图

```
创建执行实例 → pending
    ↓
开始异步执行 → running
    ↓
    ├─ 所有步骤成功 → completed
    └─ 任一步骤失败 → failed
```

---

## 五、API 端点说明

### 5.1 列出所有执行记录

```
GET /api/executions
```

**查询参数**：
- `page`（可选）：页码，默认 1
- `page_size`（可选）：每页条数，默认 20
- `status`（可选）：按状态筛选（pending/running/completed/failed）

**响应**（200 OK）：

```json
{
  "items": [
    {
      "id": 100,
      "orchestration_id": 1,
      "tenant_id": 1,
      "status": "completed",
      "current_step": "mvt",
      "started_at": "2026-01-01T10:00:00Z",
      "completed_at": "2026-01-01T10:15:00Z",
      "created_at": "2026-01-01T09:59:00Z"
    }
  ],
  "total": 1,
  "page": 1,
  "page_size": 20
}
```

---

### 5.2 获取执行详情

```
GET /api/orch-executions/:id
```

**响应**（200 OK）：

```json
{
  "id": 100,
  "orchestration_id": 1,
  "tenant_id": 1,
  "status": "completed",
  "current_step": "export",
  "step_results": {
    "scan": {
      "status": "success",
      "result": {
        "schemas_scanned": 2,
        "tables_scanned": 50
      },
      "started_at": "2026-01-01T10:00:00Z",
      "ended_at": "2026-01-01T10:05:00Z",
      "duration": 300000
    },
    "mvt": {
      "status": "success",
      "result": {
        "tiles_generated": 5000
      },
      "started_at": "2026-01-01T10:05:00Z",
      "ended_at": "2026-01-01T10:15:00Z",
      "duration": 600000
    }
  },
  "error_message": "",
  "started_at": "2026-01-01T10:00:00Z",
  "completed_at": "2026-01-01T10:15:00Z",
  "created_at": "2026-01-01T09:59:00Z",
  "orchestration": {
    "id": 1,
    "name": "数据处理流水线",
    "description": "自动扫描元数据并生成瓦片"
  }
}
```

**说明**：包含关联的 `orchestration` 基本信息

---

### 5.3 列出编排的执行记录

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
      "status": "completed",
      "started_at": "2026-01-01T10:00:00Z",
      "completed_at": "2026-01-01T10:15:00Z",
      "created_at": "2026-01-01T09:59:00Z"
    },
    {
      "id": 99,
      "orchestration_id": 1,
      "status": "failed",
      "error_message": "步骤 export 超时",
      "started_at": "2026-01-01T02:00:00Z",
      "completed_at": "2026-01-01T02:35:00Z",
      "created_at": "2026-01-01T01:59:00Z"
    }
  ],
  "total": 2
}
```

---

## 六、执行流程

### 6.1 创建执行实例

当用户手动触发或定时调度触发时：

1. 创建 `executions` 记录，`status = "pending"`
2. 返回 `execution_id`（不等待执行完成）
3. 启动 Go 协程异步执行

### 6.2 异步执行过程

```
Go 协程开始
  ↓
更新 status = "running", started_at = NOW()
  ↓
构建 DAG → 拓扑排序
  ↓
for each step (按拓扑顺序):
  ↓
  更新 current_step
  ↓
  执行步骤（调用引擎 API）
  ↓
  存储 StepResult 到 step_results
  ↓
  if 失败: break
  ↓
更新 status = "completed" 或 "failed"
更新 completed_at = NOW()
```

### 6.3 结果存储

每个步骤执行完成后，立即更新 `step_results`：

```sql
UPDATE orchestrator.executions
SET
  step_results = jsonb_set(
    step_results,
    '{step_id}',
    '{"status":"success", "result":{...}, ...}'
  ),
  current_step = 'next_step_id'
WHERE id = 100;
```

---

## 七、使用示例

### 7.1 手动触发并查询结果

```bash
# 1. 触发执行
EXECUTION_ID=$(curl -s -X POST http://localhost:8084/api/orchestrations/1/execute \
  -H "Authorization: Bearer $TOKEN" | jq -r '.execution_id')

echo "执行ID: $EXECUTION_ID"

# 2. 轮询状态（每5秒查询一次）
while true; do
  STATUS=$(curl -s http://localhost:8084/api/orch-executions/$EXECUTION_ID \
    -H "Authorization: Bearer $TOKEN" | jq -r '.status')

  echo "当前状态: $STATUS"

  if [ "$STATUS" = "completed" ] || [ "$STATUS" = "failed" ]; then
    break
  fi

  sleep 5
done

# 3. 查看详细结果
curl http://localhost:8084/api/orch-executions/$EXECUTION_ID \
  -H "Authorization: Bearer $TOKEN" | jq
```

### 7.2 查询失败的执行

```bash
curl "http://localhost:8084/api/executions?status=failed&page_size=10" \
  -H "Authorization: Bearer $TOKEN"
```

### 7.3 查看编排的执行历史

```bash
curl http://localhost:8084/api/orchestrations/1/executions \
  -H "Authorization: Bearer $TOKEN"
```

---

## 八、重要说明

### 8.1 数据保留策略

- 执行记录不会自动删除
- 建议定期清理旧的执行记录（如保留最近 30 天）
- 可通过 `created_at` 字段筛选删除

### 8.2 并发执行

- 同一个编排可以有多个并发执行实例
- 每个实例独立运行，互不影响
- 通过 `orchestration_id` 关联到同一个编排定义

### 8.3 错误处理

- 步骤失败后不会继续执行后续步骤
- `error_message` 记录第一个失败步骤的错误信息
- `step_results` 中保留已执行步骤的结果

### 8.4 性能考虑

- `step_results` 为 JSONB 类型，支持索引查询
- 对于长时间运行的工作流，可以实时查询 `current_step` 了解进度
- 建议对 `created_at` 字段创建索引以优化历史查询

---

## 九、相关文档

- [orchestrations 表](./orchestrations表.md) - 编排定义表，工作流模板
- [数据库架构](../数据库架构.md) - Orchestrator 模块整体架构
- [Orchestrator 模块说明](../../CLAUDE.md) - 模块整体架构和执行引擎说明

# dev_executions 表结构和 API 说明

## 字段说明（常见疑问解答）

### execution_id vs id

| 字段 | 类型 | 用途 | 示例 |
|------|------|------|------|
| `id` | SERIAL (自增) | **内部唯一标识**，用于数据库索引、排序、外键关联 | `12345` |
| `execution_id` | VARCHAR (UUID) | **外部唯一标识**，用于 API 响应、跨服务调用、日志追踪 | `550e8400-e29b-41d4-a716-446655440000` |

**为什么需要两个 ID？**
- **UUID** 提供全局唯一性，适合分布式系统、跨服务调用
- **自增 ID** 提供高效的索引性能、范围查询和排序
- API 对外暴露 UUID，内部使用自增 ID 优化性能

### dev_type 字段必要性

**为什么不能删除？**
- **临时执行支持**：当 `dev_item_id` 为空时（临时执行），必须依赖 `dev_type` 路由到正确的执行器
- **独立查询**：支持按类型过滤执行记录，无需关联 dev_items 表
- **统计分析**：按类型统计执行情况，提升查询性能

### trigger_type 字段说明

| 值 | 含义 | 触发来源 |
|---|------|---------|
| `manual` | 手动触发 | 用户在前端点击"执行"按钮 |
| `schedule` | 定时触发 | Cron 调度器自动执行 |
| `orchestrator` | 编排触发 | Orchestrator 模块调用 |
| `api` | API 触发 | 外部系统通过 API 调用 |

### engine_id 为 null 的原因

**什么情况下为 null？**
- SQL 查询：**有值**（必须指定数据库引擎）
- 工作流：**可能为 null**（引擎配置在 execution_config 中）
- Notebook：**可能为 null**（使用默认 Jupyter 引擎）
- 临时执行：**可能为 null**（未关联开发项）

**如何获取引擎信息？**
- 如果 `dev_item_id` 不为空，可从 `dev_items.execution_config` 中读取
- 如果 `dev_item_id` 为空，表示临时执行，引擎信息在执行参数中

### rows_affected 为 null 的原因

**什么情况下有值？**
- SQL DML 语句：**有值**（INSERT/UPDATE/DELETE 影响的行数）
- SQL SELECT 查询：**有值**（返回的行数）

**什么情况下为 null？**
- 工作流执行：**null**（不适用于工作流）
- Notebook 执行：**null**（不适用于 Notebook）
- 执行失败：**null**（未完成执行）

---

## 一、表结构概览

`develop.dev_executions` 表是 Develop 模块的执行记录表，记录所有 SQL 查询、工作流、Notebook 的执行历史。支持执行状态追踪、结果存储、性能监控。

### 核心功能

- **执行记录管理**：记录所有开发项的执行历史
- **状态追踪**：实时追踪执行状态（pending → running → success/failed）
- **结果存储**：存储执行结果、错误信息、性能指标
- **关联开发项**：可关联到 dev_items 表，也可独立执行

---

## 二、表结构定义

### 2.1 核心字段

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | SERIAL | PRIMARY KEY | 执行记录唯一标识 |
| `tenant_id` | INTEGER | NOT NULL, INDEXED | 租户 ID |
| `dev_item_id` | INTEGER | INDEXED | 关联的开发项 ID（可为空） |
| `execution_id` | VARCHAR(255) | NOT NULL, UNIQUE | UUID 执行标识 |
| `dev_type` | VARCHAR(50) | NOT NULL | 开发类型：'query'、'workflow'、'script'、'notebook' |
| `trigger_type` | VARCHAR(50) | INDEXED | 触发类型：'manual'、'schedule'、'orchestrator'、'api' |
| `triggered_by` | INTEGER | | 触发者 ID |
| `status` | VARCHAR(50) | NOT NULL, INDEXED | 执行状态 |
| `progress` | INTEGER | DEFAULT 0 | 执行进度（0-100） |
| `current_step` | VARCHAR(255) | | 当前执行步骤（工作流） |
| `result` | JSONB | | 执行结果（查询结果、工作流输出等） |
| `inputs` | JSONB | | 输入参数 |
| `error_message` | TEXT | | 错误信息（失败时） |
| `execution_time_ms` | BIGINT | | 执行耗时（毫秒） |
| `rows_affected` | BIGINT | | 影响行数（SQL 查询） |
| `result_size_bytes` | BIGINT | | 结果大小（字节） |
| `engine_id` | INTEGER | | 使用的引擎 ID |
| `started_at` | TIMESTAMP | | 开始时间 |
| `completed_at` | TIMESTAMP | | 完成时间 |
| `created_at` | TIMESTAMP | DEFAULT NOW(), INDEXED | 创建时间 |

### 2.2 数据库索引

| 索引名 | 字段 | 说明 |
|--------|------|------|
| `idx_dev_executions_tenant_status` | tenant_id, status | 按租户和状态查询 |
| `idx_dev_executions_item` | dev_item_id | 按开发项查询 |
| `idx_dev_executions_execution_id` | execution_id | UUID 唯一索引 |
| `idx_dev_executions_trigger_type` | trigger_type | 按触发类型过滤 |
| `idx_dev_executions_created_at` | created_at DESC | 按创建时间倒序排列 |

---

## 三、ExecutionStatus 说明

| 值 | 含义 | 说明 |
|---|------|------|
| `pending` | 待执行 | 已提交，等待执行 |
| `running` | 执行中 | 正在执行 |
| `success` | 执行成功 | 正常完成 |
| `failed` | 执行失败 | 执行出错 |
| `timeout` | 超时 | 执行超时 |
| `cancelled` | 已取消 | 用户取消 |

### 状态流转图

```
pending → running → success
               ↓
            failed
               ↓
            timeout
               ↓
            cancelled
```

---

## 四、Result 字段结构

### 4.1 Query 类型（dev_type='query'）

```json
{
  "columns": [
    {"name": "id", "type": "integer"},
    {"name": "name", "type": "text"}
  ],
  "rows": [
    [1, "北京"],
    [2, "上海"]
  ],
  "total_rows": 2,
  "execution_plan": "Seq Scan on cities..."
}
```

### 4.2 Workflow 类型（dev_type='workflow'）

```json
{
  "workflow_id": "workflow-uuid",
  "step_results": [
    {
      "step_id": "step1",
      "status": "success",
      "output": {
        "type": "FeatureCollection",
        "features": [...]
      },
      "execution_time_ms": 123
    }
  ],
  "output_file": "s3://bucket/output.geojson"
}
```

---

## 五、API 端点说明

### 5.1 POST /api/v1/sql/execute - 执行 SQL 查询

**请求体**：

```json
{
  "engine_id": 1,
  "sql": "SELECT * FROM cities WHERE population > 1000000",
  "save_as_dev_item": true,
  "item_name": "查询大城市"
}
```

**响应**（201 Created）：

```json
{
  "execution_id": "uuid-xxxx",
  "status": "success",
  "columns": [...],
  "rows": [...],
  "execution_time_ms": 245,
  "rows_affected": 10
}
```

---

### 5.2 POST /api/v1/workflow/execute - 执行工作流

**请求体**：

```json
{
  "workflow": {
    "steps": [
      {"id": "step1", "type": "data_loader", "params": {...}},
      {"id": "step2", "type": "buffer", "params": {"distance": 100}}
    ]
  },
  "save_as_dev_item": true,
  "item_name": "缓冲区分析工作流"
}
```

**响应**：

```json
{
  "execution_id": "uuid-yyyy",
  "status": "running",
  "message": "工作流已提交执行"
}
```

---

### 5.3 GET /api/v1/executions - 查询执行记录列表

**查询参数**：
- `dev_type`：按类型过滤
- `status`：按状态过滤
- `dev_item_id`：按开发项过滤
- `trigger_type`：按触发类型过滤
- `start_date`、`end_date`：按日期范围过滤
- `page`、`page_size`：分页参数

**响应**：

```json
{
  "executions": [
    {
      "id": 123,
      "execution_id": "uuid-xxxx",
      "dev_type": "query",
      "status": "success",
      "execution_time_ms": 245,
      "created_at": "2025-01-01T10:00:00Z"
    }
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---

### 5.4 GET /api/v1/executions/:execution_id - 获取执行详情

**响应**：返回完整 DevExecution 对象（包含 result JSONB）

---

### 5.5 POST /api/v1/executions/:execution_id/cancel - 取消执行

**响应**：

```json
{
  "message": "执行已取消",
  "execution_id": "uuid-xxxx",
  "status": "cancelled"
}
```

---

## 六、性能监控

### 6.1 执行时间追踪

```sql
-- 查询平均执行时间
SELECT
  dev_type,
  AVG(execution_time_ms) as avg_time_ms,
  MAX(execution_time_ms) as max_time_ms,
  COUNT(*) as total_executions
FROM develop.dev_executions
WHERE status = 'success'
  AND tenant_id = 1
  AND created_at > NOW() - INTERVAL '7 days'
GROUP BY dev_type;
```

### 6.2 失败率分析

```sql
-- 查询失败率
SELECT
  dev_type,
  COUNT(CASE WHEN status = 'failed' THEN 1 END) * 100.0 / COUNT(*) as failure_rate
FROM develop.dev_executions
WHERE tenant_id = 1
  AND created_at > NOW() - INTERVAL '7 days'
GROUP BY dev_type;
```

---

## 七、使用示例

### 示例 1：执行 SQL 并查看结果

```bash
# 1. 执行 SQL
EXECUTION_ID=$(curl -X POST http://localhost:8084/api/v1/sql/execute \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "engine_id": 1,
    "sql": "SELECT * FROM cities LIMIT 10"
  }' | jq -r '.execution_id')

# 2. 查询执行结果
curl -X GET http://localhost:8084/api/v1/executions/$EXECUTION_ID \
  -H "Authorization: Bearer $TOKEN"
```

### 示例 2：查询某个开发项的执行历史

```bash
curl -X GET "http://localhost:8084/api/v1/executions?dev_item_id=5&page=1&page_size=10" \
  -H "Authorization: Bearer $TOKEN"
```

---

## 八、重要说明

### 8.1 执行记录清理策略

- **成功记录**：保留 30 天
- **失败记录**：保留 90 天（用于故障分析）
- **大结果集**：result JSONB 超过 10MB 时，存储到 MinIO，result 中仅保留对象路径

### 8.2 并发控制

- 每个租户最多同时执行 10 个任务
- 超过限制时，新任务进入 pending 状态

### 8.3 超时配置

- **SQL 查询**：默认 300 秒
- **工作流**：默认 600 秒
- **Notebook**：默认 900 秒

---

## 九、相关文档

- [dev_items表](./dev_items表.md) - 开发项定义表
- [数据库架构](../数据库架构.md) - Develop 模块架构

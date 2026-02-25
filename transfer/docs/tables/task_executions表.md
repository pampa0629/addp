# task_executions 表结构和 API 说明

## 字段说明（常见疑问解答）

### LocalTime 时间类型

**什么是 LocalTime？**
- Transfer 模块使用自定义的 `LocalTime` 类型
- 序列化为不带时区的本地时间字符串（格式：`2006-01-02T15:04:05`）
- 避免时区转换带来的困扰，适合展示和记录执行时间

**为什么不用标准 time.Time？**
- 标准 time.Time 序列化时会带上时区信息（如 `2006-01-02T15:04:05+08:00`）
- LocalTime 简化了前端处理，直接显示本地时间
- 数据库存储时仍然使用 TIMESTAMP 类型

### trigger_type vs trigger_by

| 字段 | 类型 | 说明 |
|------|------|------|
| `trigger_type` | VARCHAR | 触发方式：'manual'（手动）、'schedule'（定时）、'api'（API调用） |
| `trigger_by` | INTEGER | 触发者用户 ID（仅 manual 和 api 方式有值） |

### checkpoint_offset vs checkpoint_state

| 字段 | 用途 | 示例 |
|------|------|------|
| `checkpoint_offset` | 简单的数值偏移量（行号、记录数等） | `12500`（已处理 12500 条记录） |
| `checkpoint_state` | 复杂的断点状态（JSONB，支持多种恢复策略） | `{"batch_id": 5, "last_id": 12500, "processed_tables": ["table1"]}` |

**什么时候使用？**
- 批处理任务：主要使用 `checkpoint_offset`
- 复杂工作流：使用 `checkpoint_state` 存储详细状态
- 断点续传：两者结合使用，提供多层次的恢复能力

---

## 一、表结构概览

`transfer.task_executions` 表记录传输任务的每次执行历史，包括执行状态、数据统计、错误信息、断点状态等。

### 核心功能

- **执行历史追踪**：记录每次任务执行的详细信息
- **性能统计**：记录读写记录数、字节数、执行时间
- **断点续传**：保存断点偏移和状态，支持任务中断后恢复
- **错误追踪**：记录执行错误和详细日志
- **触发方式追踪**：区分手动、定时、API 触发

---

## 二、表结构定义

### 2.1 核心字段

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | SERIAL | PRIMARY KEY | 执行记录唯一标识 |
| `task_id` | INTEGER | NOT NULL, INDEXED | 关联任务 ID（外键关联 tasks 表） |
| `status` | VARCHAR(20) | NOT NULL, INDEXED | 执行状态（见下文状态说明） |
| `start_time` | TIMESTAMP | NOT NULL, INDEXED | 开始时间 |
| `end_time` | TIMESTAMP | | 结束时间（NULL 表示未完成） |
| `records_read` | BIGINT | DEFAULT 0 | 读取记录数 |
| `records_written` | BIGINT | DEFAULT 0 | 写入记录数 |
| `bytes_read` | BIGINT | DEFAULT 0 | 读取字节数 |
| `bytes_written` | BIGINT | DEFAULT 0 | 写入字节数 |
| `error_msg` | TEXT | | 错误信息（失败时记录详细错误） |
| `logs` | TEXT | | 执行日志（记录关键步骤） |
| `checkpoint_offset` | BIGINT | DEFAULT 0 | 断点偏移量（用于简单断点续传） |
| `checkpoint_state` | JSONB | | 断点状态（用于复杂断点续传） |
| `trigger_type` | VARCHAR(50) | | 触发方式：'manual'、'schedule'、'api' |
| `trigger_by` | INTEGER | | 触发者用户 ID |

### 2.2 数据库索引

| 索引名 | 字段 | 说明 |
|--------|------|------|
| `idx_executions_task` | task_id | 按任务查询执行历史 |
| `idx_executions_status` | status | 按状态过滤 |
| `idx_executions_start_time` | start_time DESC | 按开始时间倒序排列 |

### 2.3 ExecutionStatus 说明

| 值 | 含义 | 说明 |
|---|------|------|
| `pending` | 待执行 | 任务已提交，等待执行 |
| `running` | 执行中 | 任务正在执行 |
| `success` | 执行成功 | 任务正常完成 |
| `failed` | 执行失败 | 任务执行出错 |

### 状态流转图

```
pending → running → success
               ↓
            failed
```

**注意**：Transfer 模块不支持 `timeout` 和 `cancelled` 状态（与 Develop 模块不同）

---

## 三、checkpoint_state 字段结构

### 3.1 简单批处理场景

```json
{
  "batch_id": 5,
  "last_offset": 12500,
  "last_record_id": "uuid-xxxx"
}
```

### 3.2 多表同步场景

```json
{
  "processed_tables": ["table1", "table2"],
  "current_table": "table3",
  "table_offsets": {
    "table1": 10000,
    "table2": 5000,
    "table3": 2500
  }
}
```

### 3.3 文件分片导入场景

```json
{
  "total_chunks": 10,
  "processed_chunks": [1, 2, 3, 4, 5],
  "current_chunk": 6,
  "chunk_size": 10000
}
```

---

## 四、API 端点说明

### 4.1 GET /api/executions - 查询执行记录列表

**查询参数**：
- `task_id`：按任务 ID 过滤
- `status`：按状态过滤（'pending' | 'running' | 'success' | 'failed'）
- `start_date`：开始日期（YYYY-MM-DD）
- `end_date`：结束日期（YYYY-MM-DD）
- `page`：页码（最小 1）
- `page_size`：每页数量（1-100）

**响应**：
```json
{
  "items": [
    {
      "id": 123,
      "task_id": 1,
      "status": "success",
      "start_time": "2026-01-01T10:00:00",
      "end_time": "2026-01-01T10:05:30",
      "records_read": 50000,
      "records_written": 50000,
      "bytes_read": 10485760,
      "bytes_written": 10485760,
      "trigger_type": "schedule"
    }
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---

### 4.2 GET /api/executions/:id - 获取执行详情

**响应**：返回完整 TaskExecution 对象（包含 logs 和 checkpoint_state）

```json
{
  "id": 123,
  "task_id": 1,
  "status": "success",
  "start_time": "2026-01-01T10:00:00",
  "end_time": "2026-01-01T10:05:30",
  "records_read": 50000,
  "records_written": 50000,
  "bytes_read": 10485760,
  "bytes_written": 10485760,
  "error_msg": "",
  "logs": "2026-01-01 10:00:00 - 开始执行\n2026-01-01 10:00:05 - 连接数据源成功\n...",
  "checkpoint_offset": 50000,
  "checkpoint_state": {
    "batch_id": 50,
    "last_offset": 50000
  },
  "trigger_type": "schedule",
  "trigger_by": null
}
```

---

### 4.3 GET /api/tasks/:task_id/executions - 获取任务的执行历史

按任务 ID 查询其所有执行记录。

**查询参数**：
- `status`：按状态过滤
- `page`、`page_size`：分页参数

---

### 4.4 GET /api/executions/:id/logs - 获取执行日志

**响应**：
```json
{
  "execution_id": 123,
  "logs": "2026-01-01 10:00:00 - 开始执行\n2026-01-01 10:00:05 - 连接数据源成功\n2026-01-01 10:01:00 - 已读取 10000 条记录\n..."
}
```

---

## 五、性能统计

### 5.1 执行时间分析

**计算执行时长**：
```go
duration := execution.Duration() // time.Duration
```

TaskExecution 模型提供了 `Duration()` 方法：
- 如果 `end_time` 为空（执行中），返回从 `start_time` 到现在的时长
- 如果 `end_time` 有值（已完成），返回 `end_time - start_time`

### 5.2 数据吞吐量计算

**读取速度**（记录/秒）：
```
records_read / (duration_seconds)
```

**写入速度**（记录/秒）：
```
records_written / (duration_seconds)
```

**读取带宽**（MB/秒）：
```
bytes_read / 1024 / 1024 / (duration_seconds)
```

### 5.3 SQL 查询示例

```sql
-- 查询任务的平均执行时间
SELECT
  task_id,
  AVG(EXTRACT(EPOCH FROM (end_time - start_time))) as avg_duration_seconds,
  AVG(records_written) as avg_records,
  SUM(records_written) as total_records
FROM transfer.task_executions
WHERE status = 'success'
  AND end_time IS NOT NULL
GROUP BY task_id;

-- 查询任务的成功率
SELECT
  task_id,
  COUNT(CASE WHEN status = 'success' THEN 1 END) * 100.0 / COUNT(*) as success_rate,
  COUNT(*) as total_executions
FROM transfer.task_executions
GROUP BY task_id;
```

---

## 六、断点续传详解

### 6.1 断点续传原理

1. **记录断点**：任务执行过程中定期更新 `checkpoint_offset` 和 `checkpoint_state`
2. **任务中断**：任务失败或被暂停时，保留最后的断点信息
3. **恢复执行**：调用 `/api/tasks/:id/resume` 端点，从断点位置继续执行
4. **状态校验**：恢复前检查断点状态的有效性

### 6.2 断点更新策略

**批处理任务**：每处理一个批次后更新断点
```go
execution.CheckpointOffset = processedRecords
execution.CheckpointState = map[string]interface{}{
    "batch_id": batchID,
    "last_id": lastRecordID,
}
db.Save(&execution)
```

**流式任务**：定时更新断点（如每 10 秒）
```go
ticker := time.NewTicker(10 * time.Second)
for range ticker.C {
    execution.CheckpointOffset = processedRecords
    db.Save(&execution)
}
```

### 6.3 断点恢复示例

```bash
# 1. 查询任务的最后一次执行记录
curl -X GET "http://localhost:8083/api/tasks/1/executions?page=1&page_size=1" \
  -H "Authorization: Bearer $TOKEN"

# 2. 确认执行记录的断点状态
# Response: {"checkpoint_offset": 12500, "checkpoint_state": {...}}

# 3. 恢复任务执行
curl -X POST "http://localhost:8083/api/tasks/1/resume" \
  -H "Authorization: Bearer $TOKEN"
```

---

## 七、使用示例

### 示例 1：查询任务的执行历史

```bash
curl -X GET "http://localhost:8083/api/tasks/1/executions?page=1&page_size=10" \
  -H "Authorization: Bearer $TOKEN"
```

### 示例 2：查询失败的执行记录

```bash
curl -X GET "http://localhost:8083/api/executions?status=failed&page=1&page_size=20" \
  -H "Authorization: Bearer $TOKEN"
```

### 示例 3：获取执行日志

```bash
curl -X GET "http://localhost:8083/api/executions/123/logs" \
  -H "Authorization: Bearer $TOKEN"
```

### 示例 4：按日期范围查询

```bash
curl -X GET "http://localhost:8083/api/executions?start_date=2026-01-01&end_date=2026-01-31&page=1&page_size=50" \
  -H "Authorization: Bearer $TOKEN"
```

---

## 八、重要说明

### 8.1 执行记录清理策略

**建议策略**：
- **成功记录**：保留最近 30 天
- **失败记录**：保留最近 90 天（用于故障分析）
- **运行中记录**：永久保留（直到完成）

**清理脚本示例**：
```sql
-- 删除 30 天前的成功记录
DELETE FROM transfer.task_executions
WHERE status = 'success'
  AND end_time < NOW() - INTERVAL '30 days';

-- 删除 90 天前的失败记录
DELETE FROM transfer.task_executions
WHERE status = 'failed'
  AND end_time < NOW() - INTERVAL '90 days';
```

### 8.2 日志大小限制

- `logs` 字段类型为 TEXT，理论上可存储无限长度
- **建议**：单条执行记录的日志不超过 10MB
- **大日志处理**：超过限制时，将日志写入文件存储（MinIO），logs 字段仅保留文件路径

### 8.3 并发安全

- 同一任务同时只能有一个 `running` 状态的执行记录
- 新执行开始前，检查是否有未完成的执行记录
- 使用数据库事务确保状态更新的原子性

---

## 九、相关文档

- [tasks表](./tasks表.md) - 任务定义表
- [data_mappings表](./data_mappings表.md) - 字段映射表
- [local_engines表](./local_engines表.md) - 本地引擎表
- [数据库架构](../数据库架构.md) - Transfer 模块架构

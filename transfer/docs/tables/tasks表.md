# tasks 表结构和 API 说明

## 一、表结构概览

`transfer.tasks` 表是 Transfer 模块的传输任务定义表，支持数据的导入、导出、同步。支持定时调度和并行控制。

### 核心功能

- **任务配置**：定义数据源和目标配置（source 和 target）
- **定时调度**：支持 Cron 表达式定时执行
- **并行控制**：支持配置批大小和执行策略
- **状态追踪**：追踪任务的运行状态和进度
- **元数据自动扫描**：任务完成后可自动扫描元数据

---

## 二、表结构定义

### 2.1 核心字段

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | SERIAL | PRIMARY KEY | 任务唯一标识 |
| `tenant_id` | INTEGER | NOT NULL, INDEXED | 租户 ID |
| `name` | VARCHAR(255) | NOT NULL | 任务名称 |
| `description` | TEXT | | 任务描述 |
| `config` | JSONB | NOT NULL | 任务配置（包含 source 和 target） |
| `schedule` | VARCHAR(100) | | Cron 表达式 |
| `batch_size` | INTEGER | DEFAULT 1000 | 批大小 |
| `enabled` | BOOLEAN | DEFAULT false, INDEXED | 是否启用（用于定时任务） |
| `auto_scan_metadata` | BOOLEAN | DEFAULT true | 任务完成后是否自动扫描元数据 |
| `status` | VARCHAR(20) | DEFAULT 'idle', INDEXED | 任务状态：'idle'（空闲）、'running'（执行中） |
| `progress` | NUMERIC(5,2) | DEFAULT 0 | 进度（0-100） |
| `created_by` | INTEGER | | 创建人 ID |
| `created_at` | TIMESTAMP | DEFAULT NOW() | 创建时间 |
| `updated_at` | TIMESTAMP | DEFAULT NOW() | 更新时间 |

### 2.2 数据库索引

| 索引名 | 字段 | 说明 |
|--------|------|------|
| `idx_tasks_tenant` | tenant_id | 按租户查询 |
| `idx_tasks_status` | status | 按状态过滤 |
| `idx_tasks_enabled` | enabled | 查询已启用的定时任务 |

### 2.3 TaskStatus 说明

| 值 | 含义 | 说明 |
|---|------|------|
| `idle` | 空闲 | 任务未执行或执行已完成 |
| `running` | 执行中 | 任务正在执行 |

**注意**：Transfer 模块的任务状态较为简化，只有两种状态。详细的执行状态记录在 `task_executions` 表中。

### 2.4 config 字段结构

**config 字段包含 source 和 target 配置**：

```json
{
  "source": {
    "scope": "system",          // "system" | "local"
    "engine_id": 1,             // 引擎 ID（根据 scope 从不同表查询）
    "type": "table",            // 数据源类型
    "table": "public.cities",   // 表名
    "filter": "population > 1000000"
  },
  "target": {
    "scope": "system",
    "engine_id": 8,
    "type": "table",
    "table": "public.target_cities"
  }
}
```

**系统引擎 vs 本地引擎**：
- **系统引擎** (`scope: "system"`)：使用 `engine_id` 引用 `system.engines` 表中的引擎（多租户共享）
- **本地引擎** (`scope: "local"`)：使用 `engine_id` 引用 `transfer.local_engines` 表中的引擎（租户私有）

---

## 三、API 端点说明

### 3.1 POST /api/tasks - 创建任务

**请求体**：
```json
{
  "name": "导入城市数据",
  "description": "从源数据库导入城市数据到目标数据库",
  "config": {
    "source": {
      "scope": "system",
      "engine_id": 1,
      "type": "table",
      "table": "public.source_cities",
      "filter": "population > 100000"
    },
    "target": {
      "scope": "system",
      "engine_id": 2,
      "type": "table",
      "table": "public.target_cities"
    }
  },
  "schedule": "0 2 * * *",
  "batch_size": 5000,
  "auto_scan_metadata": true
}
```

**响应**（201 Created）：返回完整 Task 对象

---

### 3.2 GET /api/tasks - 查询任务列表

**查询参数**：
- `status`：按状态过滤（'idle' | 'running'）
- `page`：页码（最小 1）
- `page_size`：每页数量（1-100）

**响应**：
```json
{
  "items": [
    {
      "id": 1,
      "name": "导入城市数据",
      "description": "从源数据库导入城市数据",
      "status": "idle",
      "progress": 0,
      "enabled": true,
      "created_at": "2026-01-01T10:00:00"
    }
  ],
  "total": 10,
  "page": 1,
  "page_size": 20
}
```

---

### 3.3 GET /api/tasks/:id - 获取任务详情

**响应**：返回完整 Task 对象（包含 config JSONB）

---

### 3.4 PUT /api/tasks/:id - 更新任务

**请求体**（所有字段可选）：
```json
{
  "name": "更新后的任务名称",
  "description": "更新后的描述",
  "config": {...},
  "schedule": "0 3 * * *",
  "batch_size": 10000,
  "enabled": true,
  "auto_scan_metadata": false
}
```

---

### 3.5 DELETE /api/tasks/:id - 删除任务

**响应**（200 OK）：
```json
{
  "message": "任务已删除"
}
```

---

### 3.6 POST /api/tasks/:id/execute - 执行任务

手动触发任务执行。

**响应**（200 OK）：
```json
{
  "message": "任务已提交执行",
  "execution_id": 123,
  "status": "pending"
}
```

---

### 3.7 POST /api/tasks/:id/pause - 暂停任务

暂停正在执行的任务（如果支持）。

**响应**（200 OK）：
```json
{
  "message": "任务已暂停",
  "status": "idle"
}
```

---

### 3.8 POST /api/tasks/:id/resume - 恢复任务

从断点恢复任务执行。

**响应**（200 OK）：
```json
{
  "message": "任务已恢复",
  "execution_id": 124,
  "status": "running"
}
```

---

## 四、任务配置详解

### 4.1 数据源类型（source.type / target.type）

| 类型 | 说明 | 示例 |
|------|------|------|
| `table` | 数据库表 | `"table": "public.cities"` |
| `query` | SQL 查询 | `"query": "SELECT * FROM cities WHERE population > 1000000"` |
| `file` | 文件（CSV、GeoJSON 等） | `"path": "s3://bucket/data.csv"` |

### 4.2 过滤条件（source.filter）

用于限制源数据的范围：
```json
{
  "source": {
    "table": "public.cities",
    "filter": "population > 1000000 AND country = 'China'"
  }
}
```

### 4.3 批处理配置（batch_size）

控制每批处理的记录数，影响：
- **内存使用**：批大小越大，内存占用越高
- **性能**：批大小适中时性能最佳
- **事务大小**：批大小决定事务提交频率

**推荐值**：
- 小数据量（< 10万行）：1000 - 5000
- 中等数据量（10万 - 100万行）：5000 - 10000
- 大数据量（> 100万行）：10000 - 50000

---

## 五、定时调度

### 5.1 启用定时任务

1. 设置 `schedule` 字段（Cron 表达式）
2. 设置 `enabled: true`
3. 任务将按 Cron 表达式自动执行

### 5.2 Cron 表达式示例

| 表达式 | 说明 |
|--------|------|
| `0 2 * * *` | 每天凌晨 2 点执行 |
| `0 */6 * * *` | 每 6 小时执行一次 |
| `0 0 * * 0` | 每周日凌晨执行 |
| `0 0 1 * *` | 每月 1 号凌晨执行 |

---

## 六、元数据自动扫描

### 6.1 功能说明

当 `auto_scan_metadata: true` 时，任务成功完成后会自动触发 Meta 模块扫描目标数据源的元数据，更新元数据索引。

### 6.2 适用场景

- ✅ **导入数据到新表**：自动识别新表结构
- ✅ **更新现有表数据**：刷新表的统计信息
- ❌ **高频执行任务**：建议关闭（避免频繁扫描）
- ❌ **临时测试任务**：建议关闭（减少开销）

---

## 七、使用示例

### 示例 1：创建定时导入任务

```bash
curl -X POST http://localhost:8083/api/tasks \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "每日导入城市数据",
    "description": "每天凌晨 2 点从源数据库导入城市数据",
    "config": {
      "source": {
        "scope": "system",
        "engine_id": 1,
        "type": "table",
        "table": "public.source_cities"
      },
      "target": {
        "scope": "system",
        "engine_id": 2,
        "type": "table",
        "table": "public.target_cities"
      }
    },
    "schedule": "0 2 * * *",
    "batch_size": 10000,
    "enabled": true,
    "auto_scan_metadata": true
  }'
```

### 示例 2：手动执行任务

```bash
curl -X POST http://localhost:8083/api/tasks/1/execute \
  -H "Authorization: Bearer $TOKEN"
```

### 示例 3：查询任务列表

```bash
curl -X GET "http://localhost:8083/api/tasks?status=idle&page=1&page_size=20" \
  -H "Authorization: Bearer $TOKEN"
```

---

## 八、重要说明

### 8.1 任务状态管理

- 任务的 `status` 字段只有两种状态：`idle` 和 `running`
- 详细的执行状态（成功、失败等）记录在 `task_executions` 表中
- 每次执行都会创建新的执行记录

### 8.2 并发限制

- 同一个任务不能同时执行多次
- 如果任务正在执行（`status = 'running'`），新的执行请求会被拒绝
- 定时调度会自动检查任务状态，避免重复执行

### 8.3 断点续传

- 执行记录中保存了 `checkpoint_offset` 和 `checkpoint_state`
- 可通过 `/api/tasks/:id/resume` 端点恢复中断的任务
- 恢复时会从上次的断点位置继续执行

---

## 九、相关文档

- [task_executions表](./task_executions表.md) - 执行记录表
- [data_mappings表](./data_mappings表.md) - 字段映射表
- [local_engines表](./local_engines表.md) - 本地引擎表
- [数据库架构](../数据库架构.md) - Transfer 模块架构

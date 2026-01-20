# Transfer 模块基本概念及配置说明

本文档是 Transfer 模块的核心配置参考，定义了数据传输任务的所有字段、配置项和约束规则。

**最后更新**: 2026-01-20 (简化重构后)

---

## 一、核心数据模型

### 1.1 Task 表（传输任务）

**表名**: `transfer.tasks`

**说明**: 存储数据传输任务的配置和状态信息。每个任务定义了一次完整的数据传输流程，包括数据源、目标、字段映射和调度规则。

| 字段名 | 类型 | 约束 | 默认值 | 说明 |
|--------|------|------|--------|------|
| **id** | uint | 主键，自增 | - | 任务唯一标识 |
| **name** | string(255) | 必填 | - | 任务名称（用户友好的标识） |
| **description** | text | 可选 | - | 任务描述（详细说明任务用途） |
| **config** | jsonb | 必填 | - | 任务配置（包含 source 和 target，详见 1.4） |
| **schedule** | string(100) | 可选 | - | Cron 表达式（定时调度规则，空表示手动触发） |
| **batch_size** | int | - | 1000 | 批处理大小（单次读写的记录数） |
| **enabled** | bool | 索引 | false | 启用状态（定时任务是否生效） |
| **auto_scan_metadata** | bool | - | true | 完成后自动扫描元数据（调用 Meta 模块） |
| **status** | TaskStatus | 索引 | idle | 任务状态（idle/running，详见 1.5） |
| **progress** | numeric(5,2) | - | 0.00 | 执行进度（0-100，百分比） |
| **created_by** | uint | 外键，可空 | - | 创建者用户 ID |
| **tenant_id** | uint | 必填，索引 | - | 租户 ID（多租户隔离） |
| **created_at** | timestamp | - | CURRENT_TIMESTAMP | 创建时间 |
| **updated_at** | timestamp | - | CURRENT_TIMESTAMP | 更新时间 |

**核心原则**:
- **简化状态**: 只有 `idle` 和 `running` 两种状态，不再区分 pending/completed/failed
- **无任务类型**: 删除了 `type` 字段，所有任务都是数据传输任务
- **统一行为**: 所有任务采用覆盖模式写入，目标表不存在时自动创建

---

### 1.2 FieldMapping 表（字段映射）

**表名**: `transfer.field_mappings`

**说明**: 定义源字段到目标字段的映射关系。每个任务可以有多个字段映射，支持字段重命名、类型转换、默认值填充。

| 字段名 | 类型 | 约束 | 默认值 | 说明 |
|--------|------|------|--------|------|
| **id** | uint | 主键，自增 | - | 映射记录唯一标识 |
| **task_id** | uint | 必填，外键，索引 | - | 关联的任务 ID |
| **source_field** | string(255) | 必填 | - | 源字段名（来自源表/查询的字段） |
| **target_field** | string(255) | 必填 | - | 目标字段名（写入目标表的字段） |
| **default_value** | text | 可选 | - | 默认值（源字段为 NULL 时使用） |
| **field_type** | string(50) | 可选 | - | 目标字段类型（详见 1.6） |
| **format** | string(100) | 可选 | - | 格式定义（日期时间格式，如 `2006-01-02 15:04:05`） |
| **nullable** | bool | - | true | 是否允许 NULL |
| **created_at** | timestamp | - | CURRENT_TIMESTAMP | 创建时间 |

**字段映射逻辑**:
1. 如果源字段值不为 NULL，直接使用源字段值
2. 如果源字段值为 NULL 且设置了 `default_value`，使用默认值
3. 如果源字段值为 NULL 且未设置默认值：
   - `nullable = true`: 写入 NULL
   - `nullable = false`: 报错或跳过该记录

**简化说明**:
- **删除了 transform 字段**: 不再支持转换函数表达式（如 `UPPER(name)`），简化为字段重命名和类型转换
- **重命名表**: 从 `data_mappings` 改为 `field_mappings`，更准确表达字段映射的本质

---

### 1.3 TaskExecution 表（执行记录）

**表名**: `transfer.task_executions`

**说明**: 记录每次任务执行的详细信息，包括执行状态、时间、处理量、错误信息等。

| 字段名 | 类型 | 约束 | 默认值 | 说明 |
|--------|------|------|--------|------|
| **id** | uint | 主键，自增 | - | 执行记录唯一标识 |
| **task_id** | uint | 必填，外键，索引 | - | 关联的任务 ID |
| **status** | ExecutionStatus | 必填，索引 | - | 执行状态（pending/running/success/failed） |
| **start_time** | timestamp | 必填，索引 | - | 开始时间 |
| **end_time** | timestamp | 可选 | - | 结束时间 |
| **records_read** | int64 | - | 0 | 读取记录数 |
| **records_written** | int64 | - | 0 | 写入记录数 |
| **bytes_read** | int64 | - | 0 | 读取字节数 |
| **bytes_written** | int64 | - | 0 | 写入字节数 |
| **error_msg** | text | 可选 | - | 错误消息 |
| **logs** | text | 可选 | - | 执行日志 |
| **checkpoint_offset** | int64 | - | 0 | 检查点偏移量 |
| **checkpoint_state** | jsonb | 可选 | - | 检查点状态（断点续传） |
| **trigger_type** | string(50) | - | - | 触发类型（manual/schedule/api） |
| **trigger_by** | uint | 外键，可空 | - | 触发者用户 ID |

---

### 1.4 Config JSON 配置结构

**说明**: `tasks.config` 字段是 JSONB 类型，存储任务的核心配置信息，包括数据源（source）和目标（target）配置。

#### 1.4.1 完整配置示例

```json
{
  "source": {
    "scope": "system",
    "engine_id": 1,
    "connector_type": "postgresql",
    "query_type": "table",
    "schema": "public",
    "table": "source_cities"
  },
  "target": {
    "scope": "system",
    "engine_id": 2,
    "connector_type": "postgresql",
    "schema": "public",
    "table": "target_cities",
    "srid": 4326,
    "geometry_columns": ["geom"]
  }
}
```

#### 1.4.2 Source 配置（数据源）

**公共字段**:

| 字段名 | 类型 | 必填 | 说明 | 示例值 |
|--------|------|------|------|--------|
| **scope** | string | 是 | 引擎来源范围 | `"system"` / `"local"` |
| **engine_id** | int | 是 | 引擎 ID（来自 System 模块的引擎注册表） | `1` |
| **connector_type** | string | 是 | 连接器类型 | `"postgresql"` / `"mysql"` / `"spatialite"` |
| **query_type** | string | 是 | 查询类型 | `"table"` / `"sql"` |

**query_type = "table" 时**（从表读取）:

| 字段名 | 类型 | 必填 | 说明 | 示例值 |
|--------|------|------|------|--------|
| **schema** | string | 是 | 数据库 schema | `"public"` |
| **table** | string | 是 | 表名 | `"source_cities"` |

**query_type = "sql" 时**（自定义 SQL 查询）:

| 字段名 | 类型 | 必填 | 说明 | 示例值 |
|--------|------|------|------|--------|
| **query** | string | 是 | SQL 查询语句 | `"SELECT id, name FROM cities WHERE population > 1000000"` |

**已删除的 Source 配置**:
- ❌ `incremental_field`: 增量同步字段（未实现，已删除）
- ❌ `incremental_type`: 增量类型（timestamp/integer）（未实现，已删除）

#### 1.4.3 Target 配置（目标）

**数据库目标**（connector_type = "postgresql" / "mysql" / "doris" / "clickhouse"）:

| 字段名 | 类型 | 必填 | 说明 | 示例值 |
|--------|------|------|------|--------|
| **scope** | string | 是 | 引擎来源范围 | `"system"` / `"local"` |
| **engine_id** | int | 是 | 引擎 ID | `2` |
| **connector_type** | string | 是 | 连接器类型 | `"postgresql"` / `"mysql"` |
| **schema** | string | 是 | 数据库 schema | `"public"` |
| **table** | string | 是 | 表名 | `"target_cities"` |
| **srid** | int | 可选 | 空间参考系统标识符（仅空间数据） | `4326` |
| **geometry_columns** | array | 可选 | 几何字段列表 | `["geom"]` |

**对象存储目标**（connector_type = "s3" / "minio"）:

| 字段名 | 类型 | 必填 | 说明 | 示例值 |
|--------|------|------|------|--------|
| **scope** | string | 是 | 引擎来源范围 | `"system"` / `"local"` |
| **engine_id** | int | 是 | 引擎 ID | `3` |
| **connector_type** | string | 是 | 连接器类型 | `"s3"` / `"minio"` |
| **output_format** | string | 是 | 输出格式 | `"csv"` / `"json"` / `"geojson"` / `"shapefile"` |
| **output_path** | string | 是 | 输出路径（对象存储路径） | `"exports/cities.csv"` |
| **csv_headers** | bool | 可选 | 是否包含 CSV 表头（仅 CSV 格式） | `true` / `false` |
| **csv_delimiter** | string | 可选 | CSV 分隔符（仅 CSV 格式） | `","` / `"\t"` |
| **geometry_field** | string | 可选 | 几何字段名（仅空间格式） | `"geom"` |

**已删除的 Target 配置**:
- ❌ `write_mode`: 写入模式（insert/upsert/replace/append）（已删除，统一为覆盖模式）
- ❌ `conflict_keys`: 冲突键（用于 upsert 模式）（已删除）
- ❌ `create_table`: 自动建表开关（已删除，默认行为是目标表不存在时自动创建）

---

### 1.5 TaskStatus 枚举（任务状态）

**说明**: 任务的当前状态，简化为两种状态。

| 值 | 说明 | 何时设置 |
|----|------|----------|
| **idle** | 空闲 | 任务创建后默认状态；任务执行完成后恢复为 idle |
| **running** | 执行中 | 任务开始执行时设置为 running |

**状态转换**:
```
idle → running → idle
```

**简化说明**:
- **删除了复杂状态**: 不再区分 pending/completed/failed/paused 等状态
- **执行结果查看**: 查看 `task_executions` 表了解任务的历史执行结果

---

### 1.6 ExecutionStatus 枚举（执行状态）

**说明**: 任务执行记录的状态，详细记录每次执行的结果。

| 值 | 说明 | 何时设置 |
|----|------|----------|
| **pending** | 待执行 | 执行记录创建后默认状态 |
| **running** | 运行中 | 任务开始执行时设置 |
| **success** | 成功 | 任务成功完成 |
| **failed** | 失败 | 任务执行失败 |

**状态转换**:
```
pending → running → success
                  ↘ failed
```

---

### 1.7 FieldType 枚举（字段类型）

**说明**: `field_mappings.field_type` 字段的可选值，定义目标字段的数据类型。

| 值 | 说明 | 格式示例 | 备注 |
|----|------|----------|------|
| **string** | 字符串 | - | 默认类型 |
| **integer** | 整数 | - | 32 位整数 |
| **float** | 单精度浮点数 | - | 32 位浮点数 (float32, 4字节) |
| **double** | 双精度浮点数 | - | 64 位浮点数 (float64, 8字节) |
| **boolean** | 布尔值 | - | true/false |
| **date** | 日期 | `2006-01-02` | 仅日期，无时间部分 |
| **timestamp** | 时间戳 | `2006-01-02 15:04:05` | 日期 + 时间 |
| **json** | JSON | - | JSON 对象或数组 |
| **geometry** | 空间几何 | - | PostGIS 几何类型（POINT/LINESTRING/POLYGON 等） |

**类型转换规则**:
- 系统会尝试自动转换源字段值到目标类型
- 如果转换失败，根据 `nullable` 字段决定是写入 NULL 还是报错

---

## 二、核心行为准则

### 2.1 写入模式：统一覆盖

**行为**: 所有任务采用 **覆盖模式** 写入数据。

**执行步骤**:
1. **清空目标表**: `DELETE FROM target_schema.target_table`
2. **插入新数据**: `INSERT INTO target_schema.target_table (...) VALUES (...)`

**优点**:
- 简单明了，无需配置
- 避免数据重复
- 清晰的数据一致性语义

**注意事项**:
- ⚠️ **所有原有数据会被删除**，确保目标表可以被覆盖
- 如需保留历史数据，请使用不同的目标表或备份

---

### 2.2 建表策略：自动创建

**行为**: 目标表不存在时，系统 **自动创建** 目标表。

**建表依据**:
1. 根据源表结构（字段名、字段类型）
2. 根据字段映射配置（`field_mappings` 表）
3. 考虑几何字段配置（`geometry_columns`、`srid`）

**建表逻辑**:
```sql
-- 示例：自动创建的目标表
CREATE TABLE IF NOT EXISTS public.target_cities (
  id INTEGER,
  name VARCHAR(255),
  population INTEGER,
  geom GEOMETRY(POINT, 4326)
);
```

**注意事项**:
- 自动建表不包含索引、主键、外键等约束
- 如需高级表结构，建议手动预先创建目标表

---

### 2.3 字段映射：灵活重命名

**行为**: 支持字段重命名、类型转换、默认值填充。

**示例**:
```json
{
  "source_field": "user_name",
  "target_field": "name",
  "field_type": "string",
  "default_value": "未知",
  "nullable": false
}
```

**说明**:
- 源字段 `user_name` → 目标字段 `name`
- 如果 `user_name` 为 NULL，使用默认值 `"未知"`
- 不允许 NULL（`nullable: false`）

---

### 2.4 定时调度：Cron 表达式

**字段**: `tasks.schedule`

**说明**: 使用标准 Cron 表达式定义任务执行计划。

**示例**:
| Cron 表达式 | 说明 |
|------------|------|
| `0 0 * * *` | 每天凌晨 0 点执行 |
| `0 */6 * * *` | 每 6 小时执行一次 |
| `*/5 * * * *` | 每 5 分钟执行一次 |
| `0 9 * * 1-5` | 每周一至周五上午 9 点执行 |

**启用调度**:
- `schedule` 不为空且 `enabled = true` 时，任务会按计划自动执行
- `enabled = false` 时，定时任务暂停

---

### 2.5 元数据扫描：自动触发

**字段**: `tasks.auto_scan_metadata`

**行为**: 任务成功完成后，自动调用 Meta 模块扫描目标表的元数据。

**元数据内容**:
- 表结构（字段名、字段类型）
- 数据统计（记录数、数据大小）
- 空间范围（如果包含几何字段）

**配置**:
- `auto_scan_metadata = true`（默认）: 自动扫描
- `auto_scan_metadata = false`: 不扫描

---

## 三、API 请求/响应结构

### 3.1 创建任务（CreateTaskRequest）

```json
{
  "name": "同步城市数据",
  "description": "从 PostgreSQL 同步城市数据到目标库",
  "config": {
    "source": {
      "scope": "system",
      "engine_id": 1,
      "connector_type": "postgresql",
      "query_type": "table",
      "schema": "public",
      "table": "cities"
    },
    "target": {
      "scope": "system",
      "engine_id": 2,
      "connector_type": "postgresql",
      "schema": "public",
      "table": "cities_backup"
    }
  },
  "schedule": "0 2 * * *",
  "batch_size": 5000,
  "auto_scan_metadata": true,
  "mappings": [
    {
      "source_field": "id",
      "target_field": "id",
      "field_type": "integer",
      "nullable": false
    },
    {
      "source_field": "city_name",
      "target_field": "name",
      "field_type": "string",
      "default_value": "未知",
      "nullable": false
    },
    {
      "source_field": "geom",
      "target_field": "geom",
      "field_type": "geometry",
      "nullable": true
    }
  ]
}
```

**必填字段**:
- `name`: 任务名称
- `config`: 任务配置（包含 source 和 target）

**可选字段**:
- `description`: 任务描述
- `schedule`: Cron 表达式
- `batch_size`: 批大小（默认 1000）
- `auto_scan_metadata`: 是否自动扫描元数据（默认 true）
- `mappings`: 字段映射列表

---

### 3.2 更新任务（UpdateTaskRequest）

```json
{
  "name": "同步城市数据（更新）",
  "description": "更新后的描述",
  "enabled": true,
  "batch_size": 10000
}
```

**说明**: 所有字段都是可选的，只更新提供的字段。

---

### 3.3 查询任务列表（ListTasksRequest）

**查询参数**:
```
GET /api/transfer/tasks?status=idle&page=1&page_size=20
```

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| **status** | TaskStatus | 否 | 任务状态（idle/running） |
| **page** | int | 否 | 页码（默认 1） |
| **page_size** | int | 否 | 每页数量（默认 20，最大 100） |

---

## 四、最佳实践

### 4.1 任务命名规范

**推荐格式**: `<数据源> → <目标> - <用途>`

**示例**:
- ✅ `PostgreSQL → MinIO - 每日备份`
- ✅ `业务数据库 → 数仓 - 城市数据同步`
- ❌ `任务1` （不清晰）

---

### 4.2 字段映射原则

1. **保持简单**: 优先使用同名映射，减少配置复杂度
2. **明确类型**: 为几何字段、日期字段显式指定 `field_type`
3. **设置默认值**: 为非空字段设置合理的 `default_value`，避免 NULL 导致的错误

---

### 4.3 批大小调优

**推荐值**:
- 小数据量（< 10 万）: `batch_size = 1000`
- 中等数据量（10 万 - 100 万）: `batch_size = 5000`
- 大数据量（> 100 万）: `batch_size = 10000`

**注意**:
- 批大小过小：频繁 I/O，性能差
- 批大小过大：内存占用高，可能 OOM

---

### 4.4 定时任务管理

1. **启用前测试**: 手动执行任务验证配置正确后再启用定时调度
2. **监控执行记录**: 定期检查 `task_executions` 表，发现执行失败及时处理
3. **合理调度**: 避免多个大任务同时执行，导致资源竞争

---

## 五、常见问题

### Q1: 如何实现增量同步？

**A**: Transfer 模块当前不支持增量同步。所有任务采用全量覆盖模式。

**变通方案**:
- 在源端使用自定义 SQL 查询过滤数据（`query_type = "sql"`）
- 示例: `SELECT * FROM cities WHERE updated_at > '2026-01-01'`

---

### Q2: 如何保留目标表的历史数据？

**A**: Transfer 采用覆盖模式，会清空目标表。

**解决方案**:
- **方案 1**: 每次同步到不同的目标表（如 `cities_2026_01_20`）
- **方案 2**: 在目标表设置触发器，删除前备份到历史表
- **方案 3**: 使用 PostgreSQL 分区表

---

### Q3: 字段类型不匹配怎么办？

**A**: 系统会尝试自动类型转换。如果转换失败：
- `nullable = true`: 写入 NULL
- `nullable = false`: 报错，停止执行

**建议**:
- 手动预先创建目标表，确保字段类型匹配
- 在 `field_mappings` 中显式指定 `field_type`

---

### Q4: 如何处理空间数据（几何字段）？

**A**: Transfer 支持 PostGIS 几何类型。

**配置示例**:
```json
{
  "target": {
    "connector_type": "postgresql",
    "schema": "public",
    "table": "cities",
    "srid": 4326,
    "geometry_columns": ["geom"]
  }
}
```

**字段映射**:
```json
{
  "source_field": "geom",
  "target_field": "geom",
  "field_type": "geometry"
}
```

---

### Q5: 任务执行失败如何排查？

**A**: 查看 `task_executions` 表的执行记录。

**排查步骤**:
1. 查询最新执行记录:
   ```sql
   SELECT * FROM transfer.task_executions
   WHERE task_id = <任务ID>
   ORDER BY start_time DESC LIMIT 1;
   ```
2. 查看 `error_msg` 字段获取错误信息
3. 查看 `logs` 字段获取详细日志
4. 检查 `records_read` 和 `records_written`，判断在哪个阶段失败

---

## 六、版本变更记录

### v0.0.22 (2026-01-20) - 核心简化重构

**删除的功能**:
- ❌ 任务类型（`task.type`）: 所有任务都是数据传输任务
- ❌ 转换函数（`field_mappings.transform`）: 简化为字段重命名和类型转换
- ❌ 增量同步配置（`incremental_field`, `incremental_type`）: 功能未实现，已删除
- ❌ 写入模式配置（`write_mode`, `conflict_keys`）: 统一为覆盖模式
- ❌ 自动建表开关（`create_table`）: 改为默认行为

**新增的行为**:
- ✅ 统一覆盖模式：先清空目标表，后插入数据
- ✅ 自动建表：目标表不存在时自动创建
- ✅ 表重命名：`data_mappings` → `field_mappings`

**核心原则**:
- 保持简单，删除未使用的功能
- 统一行为，减少配置选项
- 准确命名，提高代码可读性

---

## 七、相关文档

- **系统架构**: [transfer/CLAUDE.md](../CLAUDE.md)
- **数据结构**: [transfer/DATA_STRUCTURES.md](./DATA_STRUCTURES.md)
- **API 文档**: [transfer/API.md](./API.md)
- **开发原则**: [docs/addp开发原则.md](../../docs/addp开发原则.md)

---

**本文档是 Transfer 模块的唯一配置准则，所有开发和配置必须遵循本文档定义。**

# 数据传输重复问题分析与解决方案

## 📋 问题概述

用户多次执行 SQLite → PostgreSQL 传输任务时，会导致数据重复写入，原因是部分 Writer 未正确实现 `write_mode` 配置。

## 🔍 现状分析

### 1. JDBC Writer（已实现，逻辑正确）

**文件**: `plugins/writers/jdbc_writer.go`

**支持的写入模式**:
```go
type JDBCWriterConfig struct {
    WriteMode   string `json:"write_mode"`   // insert, upsert, replace
    ConflictKey string `json:"conflict_key"` // upsert 时的唯一键
}
```

#### 模式详解

**1.1 `insert` 模式（默认）**
```sql
INSERT INTO my_table (id, name, geom) VALUES (1, 'test', ST_GeomFromWKB(...))
```
- ✅ **行为**: 直接插入，遇到主键冲突报错
- ⚠️ **风险**: 多次执行会失败（主键冲突）
- 📌 **适用场景**: 全新数据，确保不存在冲突

**1.2 `upsert` 模式**
```sql
-- PostgreSQL
INSERT INTO my_table (id, name, geom) VALUES (1, 'test', ST_GeomFromWKB(...))
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, geom = EXCLUDED.geom

-- MySQL
INSERT INTO my_table (id, name, geom) VALUES (1, 'test', ST_GeomFromWKB(...))
ON DUPLICATE KEY UPDATE name = VALUES(name), geom = VALUES(geom)
```
- ✅ **行为**: 插入或更新（存在则更新，不存在则插入）
- ✅ **防止重复**: 是（基于冲突键去重）
- ⚠️ **前提条件**:
  - 必须指定 `conflict_key`（如 `"id"`）
  - 表必须有对应的主键或唯一索引
- 📌 **适用场景**: 增量同步、定期更新的数据

**1.3 `replace` 模式**
```go
// 在 ensureTable() 中执行（jdbc_writer.go:371-376）
if w.writeMode == "replace" {
    TRUNCATE TABLE my_table;
}
// 然后正常 INSERT
```
- ✅ **行为**: 每次执行前清空表，然后插入
- ✅ **防止重复**: 是（完全覆盖）
- ⚠️ **风险**: 会删除所有旧数据
- 📌 **适用场景**: 全量覆盖、数据快照

**实现状态**: ✅ **已正确实现**

---

### 2. PostgresCOPYWriter（未实现，存在问题）

**文件**: `plugins/writers/postgres_copy_writer.go`

**问题**:
- ❌ **没有 `WriteMode` 配置字段**
- ❌ **始终使用 `INSERT` 模式（通过 COPY 协议）**
- ❌ **多次执行会重复写入数据**

**影响**:
- 用户在前端选择 "替换（REPLACE）" 模式后
- 后端如果使用 `PostgresCOPYWriter`（高性能模式），配置不生效
- 数据仍然会重复

---

## 🎯 解决方案

### 方案 1: PostgresCOPYWriter 增加 write_mode 支持（推荐）

#### 1.1 修改配置结构体

```go
// postgres_copy_writer.go
type PostgresCOPYConfig struct {
    // ... 现有字段 ...

    // 新增字段
    WriteMode   string `json:"write_mode"`   // insert, replace（COPY 不支持 upsert）
}
```

#### 1.2 修改 ensureTable 方法

```go
func (w *PostgresCOPYWriter) ensureTable(ctx context.Context, batch *pipeline.DataBatch) error {
    // ... 现有的 CREATE TABLE IF NOT EXISTS 逻辑 ...

    // 新增：处理 replace 模式
    if w.config.WriteMode == "replace" {
        truncateSQL := fmt.Sprintf("TRUNCATE TABLE %s", w.qualifiedTableName())
        if _, err := w.db.ExecContext(ctx, truncateSQL); err != nil {
            return fmt.Errorf("failed to truncate table %s: %w", w.table, err)
        }
        slog.Info("truncated table for replace mode", "table", w.table)
    }

    w.tableEnsured = true
    return nil
}
```

#### 1.3 修改 NewPostgresCOPYWriter 工厂函数

```go
func NewPostgresCOPYWriter(config pipeline.ConnectorConfig) (pipeline.Writer, error) {
    var cfg PostgresCOPYConfig
    if err := utils.MapToStruct(config.Config, &cfg); err != nil {
        return nil, fmt.Errorf("invalid postgres config: %w", err)
    }

    // 设置默认写入模式
    if cfg.WriteMode == "" {
        cfg.WriteMode = "insert"
    }

    // COPY 协议不支持 upsert，如果配置了 upsert 则报错或降级
    if cfg.WriteMode == "upsert" {
        return nil, fmt.Errorf("PostgresCOPYWriter does not support upsert mode, use JDBCWriter instead")
    }

    return &PostgresCOPYWriter{
        batchSize: batchSize,
        buffer:    make([][]interface{}, 0, batchSize),
        config:    cfg,
    }, nil
}
```

**优点**:
- ✅ 保持 COPY 协议的高性能优势
- ✅ 支持 `replace` 模式防止重复
- ✅ 最小化代码改动

**缺点**:
- ⚠️ 不支持 `upsert` 模式（COPY 协议限制）

---

### 方案 2: 智能 Writer 选择（长期方案）

在 `task_service.go` 的 `resolveConnectorConfig` 或 `buildExecutionTask` 中：

```go
func (s *TaskService) selectOptimalWriter(
    targetConfig map[string]interface{},
    resourceType string,
) string {
    writeMode, _ := targetConfig["write_mode"].(string)

    // 如果是 PostgreSQL 且需要 upsert，使用 JDBCWriter
    if resourceType == "postgresql" && writeMode == "upsert" {
        return "jdbc"
    }

    // 如果是 PostgreSQL 且是 insert/replace，使用 PostgresCOPYWriter（高性能）
    if resourceType == "postgresql" && (writeMode == "insert" || writeMode == "replace") {
        return "postgres_copy"
    }

    // 其他情况使用 JDBC Writer（通用）
    return "jdbc"
}
```

**优点**:
- ✅ 自动选择最优 Writer
- ✅ 用户无需关心底层实现

**缺点**:
- ⚠️ 需要额外的路由逻辑
- ⚠️ 增加系统复杂度

---

## 📝 实施步骤

### 第一阶段：快速修复（PostgresCOPYWriter）

1. ✅ **修改配置结构体** - 添加 `WriteMode` 字段
2. ✅ **修改 ensureTable** - 支持 `replace` 模式的 TRUNCATE
3. ✅ **修改工厂函数** - 验证和设置默认值
4. ✅ **测试验证** - 多次执行传输任务，确认不重复

**预期结果**:
- 用户选择 "替换（REPLACE）" 模式后，每次执行都会清空旧数据
- 用户选择 "插入（INSERT）" 模式后，遇到冲突会报错（提示数据已存在）

### 第二阶段：前端优化

#### 2.1 前端配置界面（已实现）

**文件**: `transfer/frontend/src/views/TaskWizard.vue:515-526`

```vue
<el-form-item label="写入模式">
  <el-radio-group v-model="targetConfig.mode">
    <el-radio-button label="insert">插入（INSERT）</el-radio-button>
    <el-radio-button label="upsert">更新插入（UPSERT）</el-radio-button>
    <el-radio-button label="replace">替换（REPLACE）</el-radio-button>
  </el-radio-group>
  <div class="hint">
    <p>• 插入：遇到冲突则报错</p>
    <p>• 更新插入：遇到冲突则更新</p>
    <p>• 替换：先删除再插入</p>
  </div>
</el-form-item>
```

**注意**: 前端使用 `targetConfig.mode`，后端期望 `write_mode`

#### 2.2 字段名映射修复

需要在前端提交时映射字段名：

```javascript
// TaskWizard.vue - submitForm() 方法
const taskConfig = {
  source: { ... },
  target: {
    ...targetConfig,
    write_mode: targetConfig.mode, // 映射字段名
  }
}
```

**或者后端兼容两种字段名**：

```go
// task_service.go
writeMode := ""
if mode, ok := config["write_mode"].(string); ok {
    writeMode = mode
} else if mode, ok := config["mode"].(string); ok {
    writeMode = mode  // 兼容前端的 "mode" 字段
}
```

### 第三阶段：文档更新

1. 更新 `CLAUDE.md` - 记录写入模式的设计
2. 更新 `transfer/README.md` - 添加使用指南
3. 添加示例配置 - 在 `docs/config_templates.json` 中

---

## 🧪 测试用例

### 测试场景 1: replace 模式防止重复

```bash
# 第一次执行
curl -X POST http://localhost:8083/api/tasks \
  -d '{
    "name": "SQLite to PG (Replace)",
    "type": "sync",
    "source_id": 1,
    "target_id": 2,
    "config": {
      "source": { "table": "my_table" },
      "target": {
        "table": "my_table",
        "write_mode": "replace",
        "create_table": true
      }
    }
  }'

# 验证数据：应该有 N 行
SELECT COUNT(*) FROM my_table;  -- 结果: 1000000

# 第二次执行（相同任务）
curl -X POST http://localhost:8083/api/tasks/{task_id}/start

# 验证数据：仍然是 N 行（不重复）
SELECT COUNT(*) FROM my_table;  -- 结果: 1000000（不是 2000000）
```

### 测试场景 2: insert 模式遇到冲突

```bash
# 第二次执行（insert 模式）
# 预期结果：失败并报错 "duplicate key value violates unique constraint"
```

### 测试场景 3: upsert 模式更新数据

```bash
# 配置 upsert 模式
{
  "target": {
    "table": "my_table",
    "write_mode": "upsert",
    "conflict_key": "id"
  }
}

# 第二次执行：成功，已存在的行会被更新
```

---

## 🎬 最终推荐方案

### **短期（立即实施）**:
1. ✅ 修复 `PostgresCOPYWriter` - 添加 `write_mode` 支持（仅 `insert` 和 `replace`）
2. ✅ 修复前端字段名映射 - `mode` → `write_mode`
3. ✅ 默认设置为 `replace` 模式 - 防止新用户遇到重复问题

### **中期（优化体验）**:
1. ✅ 在前端添加警告提示：
   - 选择 `upsert` 时，提示需要主键/唯一索引
   - 选择 `replace` 时，警告会清空旧数据
2. ✅ 后端添加 Writer 智能选择逻辑（自动选择 JDBC 或 COPY）

### **长期（增强功能）**:
1. ✅ 支持增量同步 - 基于时间戳或序列号
2. ✅ 支持变更数据捕获（CDC）- 跟踪 source 的变化
3. ✅ 支持分区表写入 - 按时间分区避免全表 TRUNCATE

---

## 📌 配置示例

### 示例 1: 全量覆盖（推荐用于数据快照）

```json
{
  "name": "SQLite to PostgreSQL - Full Replace",
  "type": "sync",
  "source_id": 1,
  "target_id": 2,
  "config": {
    "source": {
      "table": "buildings",
      "geometry_fields": ["geom"]
    },
    "target": {
      "table": "public.buildings",
      "write_mode": "replace",
      "create_table": true,
      "srid": 4326,
      "geometry_columns": ["geom"]
    }
  }
}
```

### 示例 2: 增量更新（推荐用于定期同步）

```json
{
  "name": "SQLite to PostgreSQL - Incremental Upsert",
  "type": "sync",
  "source_id": 1,
  "target_id": 2,
  "config": {
    "source": {
      "table": "buildings",
      "where_clause": "updated_at > '2025-01-01'"
    },
    "target": {
      "table": "public.buildings",
      "write_mode": "upsert",
      "conflict_key": "id",
      "create_table": false
    }
  }
}
```

### 示例 3: 首次导入（全新数据）

```json
{
  "name": "SQLite to PostgreSQL - Initial Load",
  "type": "import",
  "source_id": 1,
  "target_id": 2,
  "config": {
    "source": {
      "table": "buildings"
    },
    "target": {
      "table": "public.buildings",
      "write_mode": "insert",
      "create_table": true
    }
  }
}
```

---

## 🚨 注意事项

1. **性能对比**:
   - `PostgresCOPYWriter` (replace/insert): ~10x 性能提升
   - `JDBCWriter` (upsert): 支持去重但较慢

2. **事务一致性**:
   - `replace` 模式会在写入前 TRUNCATE，确保在事务中执行
   - 失败时可能导致表被清空但未写入新数据（需要回滚机制）

3. **权限要求**:
   - `TRUNCATE` 需要表所有者权限或 `TRUNCATE` 权限
   - `INSERT` 只需要 `INSERT` 权限

4. **并发冲突**:
   - `upsert` 模式在高并发下可能有死锁风险
   - 建议串行执行或使用队列

---

## 📚 相关文件

- **后端 Writer**:
  - `transfer/backend/plugins/writers/jdbc_writer.go`
  - `transfer/backend/plugins/writers/postgres_copy_writer.go`
- **前端配置**:
  - `transfer/frontend/src/views/TaskWizard.vue`
- **任务模型**:
  - `transfer/backend/internal/models/task.go`
- **任务服务**:
  - `transfer/backend/internal/service/task_service.go`

---

**文档版本**: v1.0
**最后更新**: 2025-01-15
**负责人**: Transfer 模块维护团队

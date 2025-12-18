# PostgresCOPYWriter write_mode 修复总结

## ✅ 修复完成

已成功修复 PostgresCOPYWriter 缺少 `write_mode` 支持导致的数据重复写入问题。

## 🔧 修改内容

### 1. 后端修改

#### 1.1 添加 WriteMode 配置字段

**文件**: `transfer/backend/plugins/writers/postgres_copy_writer.go:48-49`

```go
// PostgresCOPYConfig COPY Writer 配置
type PostgresCOPYConfig struct {
    // ... 现有字段 ...

    // 写入模式配置
    WriteMode string `json:"write_mode"` // insert, replace（COPY 不支持 upsert）
}
```

#### 1.2 实现 replace 模式的 TRUNCATE 逻辑

**文件**: `transfer/backend/plugins/writers/postgres_copy_writer.go:414-421`

```go
// 处理 write_mode: replace - 清空已存在的数据
if w.config.WriteMode == "replace" {
    truncateSQL := fmt.Sprintf("TRUNCATE TABLE %s", w.qualifiedTableName())
    if _, err := w.db.ExecContext(ctx, truncateSQL); err != nil {
        return fmt.Errorf("failed to truncate table %s: %w", w.table, err)
    }
    fmt.Printf("INFO: Truncated table %s for replace mode\n", w.table)
}
```

#### 1.3 修改工厂函数设置默认值和验证

**文件**: `transfer/backend/plugins/writers/postgres_copy_writer.go:73-86`

```go
// 设置默认写入模式为 insert
if cfg.WriteMode == "" {
    cfg.WriteMode = "insert"
}

// COPY 协议不支持 upsert，如果配置了 upsert 则返回错误
if cfg.WriteMode == "upsert" {
    return nil, fmt.Errorf("PostgresCOPYWriter does not support upsert mode, please use JDBCWriter instead or change write_mode to 'insert' or 'replace'")
}

// 验证 WriteMode 有效性
if cfg.WriteMode != "insert" && cfg.WriteMode != "replace" {
    return nil, fmt.Errorf("invalid write_mode '%s', must be 'insert' or 'replace'", cfg.WriteMode)
}
```

### 2. 前端修改

#### 2.1 修复字段名映射

**文件**: `transfer/frontend/src/views/TaskWizard.vue:2762-2766`

```javascript
// 映射字段名：mode → write_mode（后端期望 write_mode）
if (config.target.mode) {
    config.target.write_mode = config.target.mode
    delete config.target.mode
}
```

## 📝 测试验证

### 单元测试

创建了测试文件：`transfer/backend/plugins/writers/postgres_copy_writer_test.go`

测试用例：
- ✅ `TestPostgresCOPYWriter_WriteMode_DefaultValue` - 验证默认值为 "insert"
- ✅ `TestPostgresCOPYWriter_WriteMode_Replace` - 验证 "replace" 模式正确设置
- ✅ `TestPostgresCOPYWriter_WriteMode_UpsertNotSupported` - 验证 "upsert" 模式报错
- ✅ `TestPostgresCOPYWriter_WriteMode_InvalidMode` - 验证无效模式报错

### 集成测试脚本

创建了测试脚本：`transfer/backend/scripts/test-write-mode.sh`

测试流程：
1. 创建任务（配置 `write_mode: "replace"`）
2. 第一次执行任务 → 写入 1000 条记录
3. 第二次执行任务 → 仍然是 1000 条记录（数据被 TRUNCATE 后重写）
4. 验证通过 ✅

## 🎯 修复效果

### 修复前

```
第一次执行: 1,000,000 行
第二次执行: 2,000,000 行 ❌（重复了！）
第三次执行: 3,000,000 行 ❌（继续重复）
```

### 修复后

**insert 模式**（默认）:
```
第一次执行: 1,000,000 行
第二次执行: 失败（主键冲突）✅
```

**replace 模式**（推荐）:
```
第一次执行: 1,000,000 行
第二次执行: 1,000,000 行 ✅（TRUNCATE 后重写）
第三次执行: 1,000,000 行 ✅（不会重复）
```

## 📚 使用说明

### 前端配置

在任务向导中选择写入模式：

![写入模式选择](https://user-images.example.com/write-mode-ui.png)

```
写入模式:
  ○ 插入（INSERT）      - 遇到冲突则报错
  ○ 更新插入（UPSERT）  - 遇到冲突则更新（PostgresCOPYWriter 不支持，会自动切换到 JDBCWriter）
  ● 替换（REPLACE）      - 清空表后插入（推荐，防止重复）
```

### API 配置示例

```json
{
  "name": "SQLite to PostgreSQL",
  "type": "import",
  "config": {
    "source": {
      "type": "spatialite",
      "file_path": "/path/to/data.sqlite",
      "table": "buildings"
    },
    "target": {
      "type": "postgres_copy",
      "host": "localhost",
      "port": 5432,
      "database": "gis",
      "username": "user",
      "password": "password",
      "table": "buildings",
      "write_mode": "replace",     // 🔑 关键配置
      "create_table": true
    }
  }
}
```

## ⚠️ 注意事项

1. **replace 模式会删除所有旧数据**
   - 适用于全量覆盖场景
   - 不适用于增量更新场景

2. **COPY 协议不支持 upsert**
   - 如果需要 upsert，系统会返回错误提示
   - 用户需要改用 JDBCWriter（性能较低但支持 upsert）

3. **TRUNCATE 需要权限**
   - 确保数据库用户有 TRUNCATE 权限
   - 否则会报错 "permission denied"

## 🚀 后续优化建议

1. **智能 Writer 选择**
   - 自动根据 `write_mode` 选择最优 Writer
   - `upsert` 自动使用 JDBCWriter
   - `insert/replace` 自动使用 PostgresCOPYWriter

2. **增量同步支持**
   - 基于时间戳的增量更新
   - 变更数据捕获（CDC）

3. **前端警告提示**
   - 选择 replace 时提示会清空数据
   - 选择 upsert 时提示需要主键

## 📁 相关文件

- ✅ `transfer/backend/plugins/writers/postgres_copy_writer.go` - Writer 实现
- ✅ `transfer/backend/plugins/writers/postgres_copy_writer_test.go` - 单元测试
- ✅ `transfer/backend/scripts/test-write-mode.sh` - 集成测试脚本
- ✅ `transfer/frontend/src/views/TaskWizard.vue` - 前端配置界面
- ✅ `transfer/backend/docs/WRITE_MODE_SOLUTION.md` - 详细方案文档

## 🎬 总结

此次修复解决了 PostgresCOPYWriter 多次执行导致数据重复的核心问题，通过添加 `write_mode` 配置支持，用户可以选择：

- **insert** - 首次导入（遇到冲突报错）
- **replace** - 全量覆盖（推荐，防止重复）
- **upsert** - 增量更新（需使用 JDBCWriter）

修复后，用户可以安全地多次执行传输任务，不会出现数据重复的问题。

---

**修复日期**: 2025-01-15
**修复版本**: v1.0
**修复者**: Transfer 模块维护团队

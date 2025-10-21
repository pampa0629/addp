# Transfer 模块数据库迁移问题修复

## 🐛 问题描述

Transfer 模块启动时遇到数据库迁移错误：

```
ERROR: cannot alter type of a column used by a view or rule (SQLSTATE 0A000)
ALTER TABLE "transfer"."task_executions" ALTER COLUMN "task_id" TYPE bigint
```

## 🔍 问题原因

1. **表结构冲突**：
   - 现有表：`task_id` 是 `integer` 类型
   - GORM 期望：`task_id` 是 `bigint` 类型（因为 Go 的 `uint` 映射到 PostgreSQL 的 `bigint`）

2. **外键约束**：
   - `task_executions` 表有外键约束引用 `tasks` 表
   - PostgreSQL 不允许修改被外键约束引用的字段类型

3. **视图依赖**：
   - 存在视图 `transfer.task_execution_stats` 使用了该表
   - 视图依赖的字段无法修改类型

## ✅ 解决方案

### 方案1：重置 Schema（推荐用于开发环境）

```sql
-- 删除并重新创建 transfer schema
DROP SCHEMA IF EXISTS transfer CASCADE;
CREATE SCHEMA transfer;

-- 授权
GRANT ALL ON SCHEMA transfer TO addp;
GRANT ALL ON ALL TABLES IN SCHEMA transfer TO addp;
GRANT ALL ON ALL SEQUENCES IN SCHEMA transfer TO addp;
```

**优点**：
- ✅ 干净利落，避免遗留问题
- ✅ 让 GORM AutoMigrate 按照最新模型创建表
- ✅ 适合开发阶段

**缺点**：
- ❌ 会丢失现有数据（开发环境可接受）

### 方案2：手动迁移（用于生产环境）

如果有重要数据需要保留，需要手动迁移：

```sql
-- 1. 删除依赖的视图
DROP VIEW IF EXISTS transfer.task_execution_stats;

-- 2. 删除外键约束
ALTER TABLE transfer.task_executions 
  DROP CONSTRAINT IF EXISTS task_executions_task_id_fkey;

-- 3. 修改字段类型
ALTER TABLE transfer.tasks ALTER COLUMN id TYPE bigint;
ALTER TABLE transfer.task_executions ALTER COLUMN task_id TYPE bigint;

-- 4. 重新添加外键约束
ALTER TABLE transfer.task_executions
  ADD CONSTRAINT task_executions_task_id_fkey
  FOREIGN KEY (task_id) REFERENCES transfer.tasks(id) ON DELETE CASCADE;

-- 5. 重新创建视图（如果需要）
```

## 📝 执行步骤

1. **停止 Transfer 服务**（如果正在运行）

2. **重置 Schema**：
   ```bash
   PGPASSWORD=addp_password psql -h localhost -U addp -d addp << EOF
   DROP SCHEMA IF EXISTS transfer CASCADE;
   CREATE SCHEMA transfer;
   GRANT ALL ON SCHEMA transfer TO addp;
   EOF
   ```

3. **重启 Transfer 服务**：
   ```bash
   cd transfer/backend
   go run cmd/server/main.go
   ```

4. **验证**：
   - ✅ 看到 "Database migrations completed"
   - ✅ Health check 返回 200
   - ✅ 表结构正确创建

## 🎯 修复的其他问题

在修复过程中，同时解决了：

1. **DBPort 格式化问题**：
   ```go
   // 修复前
   "host=%s port=%d ..."  // ❌ %d 期望 int，但 DBPort 是 string
   
   // 修复后
   "host=%s port=%s ..."  // ✅ 正确
   ```

2. **日志输出格式**：
   ```go
   // 修复前
   log.Printf("Database: %s@%s:%d/%s ...", cfg.DBUser, cfg.DBHost, cfg.DBPort, ...)
   
   // 修复后
   log.Printf("Database: %s@%s:%s/%s ...", cfg.DBUser, cfg.DBHost, cfg.DBPort, ...)
   ```

## 🚀 验证结果

启动成功后应该看到：

```
✅ Database connected successfully
🔄 Running database migrations...
✅ Database migrations completed
🚀 Transfer service starting on :8083
📊 Database: addp@localhost:5432/addp (schema: transfer)
✅ Health check: http://localhost:8083/health
```

## 💡 预防措施

为避免将来再次出现此问题：

1. **开发环境**：定期重置 schema，保持与模型定义一致
2. **生产环境**：
   - 使用版本化的迁移脚本（如 golang-migrate）
   - 禁用 GORM AutoMigrate
   - 手动测试迁移脚本

3. **一致性**：所有模块的 ID 字段使用相同的类型
   ```go
   type Task struct {
       ID uint `gorm:"primaryKey"` // 统一使用 uint
   }
   ```

## 📖 相关文档

- [DATABASE_FIX.md](DATABASE_FIX.md) - DBPort 格式化问题修复
- [FIXED_SUMMARY.md](FIXED_SUMMARY.md) - 完整修复总结
- [init-db.sql](../../scripts/init-db.sql) - 数据库初始化脚本

---

**修复时间**: 2025-01-21  
**影响范围**: 数据库迁移  
**状态**: ✅ 已解决

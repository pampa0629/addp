# Transfer 数据库连接修复

## 🐛 问题

Transfer 模块启动失败，错误信息：
```
Failed to connect to database: cannot parse `host=localhost port=%!d(string=5432) ...`: 
invalid port (strconv.ParseUint: parsing "%!d(string=5432)": invalid syntax)
```

## 🔍 根本原因

**类型不匹配**：
- `cfg.DBPort` 是 **string 类型**（继承自 `common.BaseConfig`）
- 但 DSN 格式化使用了 **`%d`**（整数格式）

```go
// 错误的代码
dsn := fmt.Sprintf(
    "host=%s port=%d user=%s ...",  // ❌ %d 期望 int
    cfg.DBHost, cfg.DBPort, ...      // ❌ DBPort 是 string
)
```

## ✅ 解决方案

将格式化符号从 `%d` 改为 `%s`，与 Meta 模块保持一致：

```go
// 正确的代码
dsn := fmt.Sprintf(
    "host=%s port=%s user=%s ...",  // ✅ %s 匹配 string
    cfg.DBHost, cfg.DBPort, ...      // ✅ DBPort 是 string
)
```

## 📚 参考

Meta 模块的正确实现：
```go
// meta/backend/internal/repository/database.go:22
dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable search_path=%s",
    cfg.DBHost,
    cfg.DBPort,  // ✅ Meta 使用 %s
    cfg.DBUser,
    cfg.DBPassword,
    cfg.DBName,
    cfg.DBSchema,
)
```

## 🎯 修复文件

- [transfer/backend/cmd/server/main.go:63](cmd/server/main.go#L63)

## ✅ 验证

```bash
# 编译测试
cd transfer/backend
go build ./cmd/server

# 启动测试
go run cmd/server/main.go
```

应该看到成功的数据库连接日志：
```
✅ Database connected successfully
🔄 Running database migrations...
✅ Database migrations completed
```

---

**修复时间**: 2025-01-21  
**问题类型**: 类型不匹配（格式化错误）  
**影响范围**: 数据库连接初始化

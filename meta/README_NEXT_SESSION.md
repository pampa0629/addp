# 🚀 下次对话快速启动指南

## 📍 当前状态

**已完成**：Meta 模块的设计、数据模型、项目基础（14个文件）
**待完成**：扫描器、服务层、API层（13个文件，约3小时）

---

## ⚡ 快速启动命令

**在新对话中直接复制粘贴以下内容**：

```
继续完成 Meta 模块后端实现。

已完成工作：
✅ 完整设计文档（meta/DESIGN.md, meta/QUICK_IMPLEMENTATION.md）
✅ 数据库模型（5个文件：datasource, database, table, field, sync_log）
✅ 数据库连接和迁移（repository/database.go）
✅ 配置管理（config.go）
✅ 扫描器接口（scanner/types.go）

待完成工作（13个文件）：
1. 扫描器实现（postgres_scanner.go, mysql_scanner.go, factory.go）
2. System客户端（system_client.go）
3. 服务层（sync_service.go, scan_service.go, metadata_service.go）
4. API层（4个handler + router）
5. 中间件（auth.go）
6. 主程序（main.go）

请参考 meta/QUICK_IMPLEMENTATION.md 中的代码框架，快速创建所有剩余文件。

优先级：扫描器 > 服务层 > API层 > 测试
```

---

## 📚 关键文档

1. **meta/QUICK_IMPLEMENTATION.md** ← 最重要！包含所有代码框架
2. **meta/DESIGN.md** ← 完整设计（数据库表、API）
3. **meta/FINAL_STATUS.md** ← 当前状态总结
4. **meta/PROGRESS.md** ← 核心代码示例

---

## 🎯 实施清单

### 第一步：扫描器（1小时）
- [ ] `internal/scanner/postgres_scanner.go`
- [ ] `internal/scanner/mysql_scanner.go`
- [ ] `internal/scanner/factory.go`

### 第二步：System客户端（20分钟）
- [ ] `pkg/utils/system_client.go`

### 第三步：服务层（1小时）
- [ ] `internal/service/sync_service.go`
- [ ] `internal/service/scan_service.go`
- [ ] `internal/service/metadata_service.go`

### 第四步：API层（40分钟）
- [ ] `internal/middleware/auth.go`
- [ ] `internal/api/sync_handler.go`
- [ ] `internal/api/scan_handler.go`
- [ ] `internal/api/metadata_handler.go`
- [ ] `internal/api/router.go`

### 第五步：主程序（20分钟）
- [ ] `cmd/server/main.go`

### 第六步：测试（30分钟）
- [ ] 启动服务测试
- [ ] API测试
- [ ] 修复bug

---

## 🔧 安装依赖

```bash
cd meta/backend
go get github.com/robfig/cron/v3
go get github.com/go-sql-driver/mysql
go mod tidy
```

---

## ✅ 完成标准

Meta 后端完成的标志：

1. ✅ 服务可以启动（`go run cmd/server/main.go`）
2. ✅ 健康检查通过（`curl http://localhost:8082/health`）
3. ✅ 能调用自动同步API（Level 1 - 数据库列表）
4. ✅ 能调用深度扫描API（Level 2 - 表和字段）
5. ✅ 能查询元数据（databases, tables, fields）

---

## 📊 文件统计

**已创建**：14个文件（约30 KB）
**待创建**：13个文件（约1500行代码）
**预计时间**：3小时

---

## 💡 提示

- 所有代码框架都在 `QUICK_IMPLEMENTATION.md` 中
- SQL查询在 `PROGRESS.md` 中
- 如果遇到问题，查看 `DESIGN.md` 了解设计意图

**准备好了吗？在新对话中继续！** 🚀

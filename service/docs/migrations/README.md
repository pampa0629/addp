# Service 模块表重命名：service_layers → external_service_layers

## 修改概要

**修改日期**: 2026-01-31
**影响范围**: Service 模块
**修改类型**: 表重命名（Breaking Change）

## 修改原因

Service 模块现在管理两种类型的服务：
1. **外部服务**（External Services）- 第三方 OGC 服务注册
2. **内部服务**（Internal Services）- ADDP 平台内部数据发布为 OGC 服务

原表名 `service_layers` 不够明确，重命名为 `external_service_layers` 以区分：
- `external_service_layers` - 外部服务的图层
- `internal_service_layers` - 内部服务的图层

## 修改清单

### 1. 数据库层面

| 修改项 | 原名称 | 新名称 |
|--------|--------|--------|
| 表名 | `service.service_layers` | `service.external_service_layers` |
| 索引 | `idx_service_layer_service` | `idx_external_service_layer_service` |
| 外键约束 | `fk_service_layers_service` | `fk_external_service_layers_service` |

### 2. Go 代码层面

| 文件路径 | 修改内容 |
|---------|---------|
| `backend/internal/models/external_service.go` | 结构体 `ServiceLayer` → `ExternalServiceLayer` |
| | TableName: `service.service_layers` → `service.external_service_layers` |
| | DTO: `ServiceLayerDTO` → `ExternalServiceLayerDTO` |
| `backend/internal/repository/external_service_repository.go` | 所有 `ServiceLayer` 引用 → `ExternalServiceLayer` |
| `backend/internal/repository/database.go` | AutoMigrate 模型列表 |
| `backend/internal/service/registry/external_service.go` | 所有函数签名和返回类型 |

### 3. 文档层面

| 文件路径 | 修改内容 |
|---------|---------|
| `CLAUDE.md` | 表名引用更新 |
| `README.md` | 表名引用更新 |
| `docs/数据库架构.md` | 所有表名、索引名、外键名更新，ER图更新 |
| `docs/tables/external_services表.md` | 文档链接更新 |
| `docs/tables/service_layers表.md` → `external_service_layers表.md` | 文件重命名和内容更新 |

## 迁移步骤

### 开发环境

1. **停止 Service 模块**
   ```bash
   bash scripts/dev/stop.sh service
   ```

2. **执行数据库迁移**
   ```bash
   psql -h localhost -p 15432 -U postgres -d addp < service/docs/migrations/rename_service_layers_to_external_service_layers.sql
   ```

3. **拉取最新代码并重启**
   ```bash
   git pull
   bash scripts/dev/restart.sh -service
   ```

### 回滚方案

如果迁移出现问题，可执行回滚：
```bash
psql -h localhost -p 15432 -U postgres -d addp < service/docs/migrations/rollback_rename_service_layers.sql
```

## 验证清单

- [ ] 数据库表已重命名
- [ ] 索引和约束已重命名
- [ ] Service 模块启动无错误
- [ ] API 调用正常（测试服务注册和图层查询）
- [ ] 日志中无 `service_layers` 相关错误

## 影响评估

### 破坏性变更
✅ **仅影响开发环境**：当前 ADDP 处于积极开发阶段，采用激进策略，无需考虑向后兼容。

### 外部依赖
❌ **无影响**：此表为 Service 模块内部使用，无外部 API 直接引用表名。

### 数据丢失风险
❌ **无风险**：表重命名不会丢失数据，仅改变表名和约束名。

## 相关文档

- [数据库迁移脚本](migrations/rename_service_layers_to_external_service_layers.sql)
- [回滚脚本](migrations/rollback_rename_service_layers.sql)
- [Service 模块架构](../CLAUDE.md)
- [数据库架构文档](数据库架构.md)

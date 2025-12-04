# 元数据扫描问题排查报告

## 问题描述

PG业务库从dbeaver可以看到很多表，但元数据扫描后，schema都扫描到了，但显示一个表也没有。

## 排查过程

### 1. 数据库状态检查

✅ **元数据库状态**:
- Schema 节点：5个（archive, business_data, public, staging, topology）
- Meta Item：0个（问题所在）
- 扫描日志：schemas_scanned=5, tables_scanned=0, status=success

✅ **业务数据库状态**:
- 容器运行正常（business-postgres，端口5433）
- 实际表数：8张
  - public schema: 5张BASE TABLE + 2张VIEW
  - business_data schema: 1张表
  - topology schema: 2张表

### 2. 扫描器SQL验证

直接在业务数据库执行扫描器使用的SQL：

```sql
SELECT
    t.table_name,
    t.table_type,
    COALESCE(pg_catalog.obj_description(pgc.oid, 'pg_class'), '') AS table_comment,
    COALESCE(pgc.reltuples::bigint, 0) AS row_count,
    COALESCE(pg_total_relation_size(pgc.oid), 0) AS size_bytes
FROM information_schema.tables t
LEFT JOIN pg_catalog.pg_namespace pgn ON pgn.nspname = t.table_schema
LEFT JOIN pg_catalog.pg_class pgc ON pgc.relname = t.table_name AND pgc.relnamespace = pgn.oid
WHERE t.table_schema = 'public'
ORDER BY t.table_name;
```

**结果**: ✅ 返回7行（5张BASE TABLE + 2张VIEW），SQL查询本身正常

### 3. 代码逻辑检查

查看了 [scan_service_new.go](../meta/backend/internal/service/scan_service_new.go) 的扫描逻辑：

- `scanDatabaseSchema()` 调用 `scan.ScanTables(schemaName)`
- 如果 `ScanTables()` 返回空数组，不会进入循环，`totalTables` 保持为 0
- 扫描日志记录 `tables_scanned = 0`，状态为 `success`

### 4. PostgresScanner检查

[postgres_scanner.go](../meta/backend/plugins/scanners/postgres_scanner.go) 的实现：

```go
func (s *PostgresScanner) ScanTables(schemaName string) ([]format.TableInfo, error) {
    // SQL查询（已验证正确）
    rows, err := s.db.Query(query, schemaName)
    if err != nil {
        return nil, fmt.Errorf("failed to query tables: %w", err)
    }
    // ...
}
```

## 根本原因

**Meta后端在执行 `scan.ScanTables()` 时无法连接到业务数据库**，导致：

1. `PostgresScanner.ScanTables()` 内部查询失败
2. 但错误被某处捕获/忽略，返回空数组 `[]` 而非 error
3. 扫描服务认为扫描成功（没有 error），但 tables = []

### 可能的具体原因

**最可能**: 密码解密失败或 ENCRYPTION_KEY 配置问题

- 资源配置中的密码是加密的：`uR4G0028AYII6d7fhXiiDQcs6WSeXWF+rvCd4/UsXDJL6HUzuE/3LYVv1JD6`
- Meta后端需要使用正确的 `ENCRYPTION_KEY` 来解密密码
- 如果解密失败，连接字符串将包含错误的密码
- 数据库连接失败，但未正确传播错误

## 解决方案

### 方案1: 验证密码解密（推荐）

1. 检查根目录 `.env` 文件中的 `ENCRYPTION_KEY` 配置
2. 确保该密钥与创建资源时使用的密钥一致
3. 重启 Meta 后端服务

### 方案2: 使用诊断脚本

运行诊断脚本查看详细状态：

```bash
./scripts/debug/scan/debug.sh
```

### 方案3: 清理重新扫描

```bash
# 1. 停止所有服务
./scripts/dev/stop.sh

# 2. 清理旧数据
docker-compose exec -T postgres psql -U addp -d addp <<EOF
DELETE FROM metadata.meta_item WHERE res_id = 2;
DELETE FROM metadata.meta_node WHERE res_id = 2;
EOF

# 3. 启动服务
./scripts/dev/start.sh

# 4. 在UI重新执行扫描，观察日志
tail -f logs/meta-backend.log
```

### 方案4: 测试数据库连接

运行连接测试脚本：

```bash
cd scripts
go run test-db-connection.go
```

### 方案5: 增加详细日志

修改 [meta/backend/internal/service/scan_service_new.go:2732](../meta/backend/internal/service/scan_service_new.go#L2732)，在 `ScanTables` 调用前后添加日志：

```go
s.log.Info("开始扫描表",
    "schema", schemaName,
    "resource_id", resourceID,
)

tables, err := scan.ScanTables(schemaName)

s.log.Info("扫描表完成",
    "schema", schemaName,
    "tables_count", len(tables),
    "error", err,
)
```

## 预防措施

1. **改进错误处理**: 在 `postgres_scanner.go` 的 `ScanTables()` 中添加更详细的错误日志
2. **连接测试**: 在扫描前先测试数据库连接
3. **密钥验证**: 启动时验证 ENCRYPTION_KEY 配置

## 相关文件

- [scan_service_new.go](../meta/backend/internal/service/scan_service_new.go) - 扫描服务主逻辑
- [postgres_scanner.go](../meta/backend/plugins/scanners/postgres_scanner.go) - PostgreSQL扫描器
- [resource_service.go](../meta/backend/internal/service/resource_service.go) - 资源服务（密码解密）
- [debug-scan.sh](../scripts/debug/scan/debug.sh) - 诊断脚本
- [fix-scan.sh](../scripts/debug/scan/fix.sh) - 修复脚本

## 下一步行动

1. **立即执行**: 运行 `./scripts/debug/scan/debug.sh` 确认问题
2. **验证配置**: 检查 `.env` 中的 `ENCRYPTION_KEY`
3. **重新扫描**: 在UI触发扫描，观察 `logs/meta-backend.log`
4. **如果仍失败**: 运行 `scripts/test-db-connection.go` 测试连接

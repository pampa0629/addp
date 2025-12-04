#!/bin/bash

# 调试元数据扫描问题的脚本

echo "======================================"
echo "元数据扫描调试脚本"
echo "======================================"
echo ""

# 1. 检查业务数据库是否可访问
echo "1. 测试业务数据库连接..."
docker exec -i business-postgres psql -U business -d business -c "SELECT version();" > /dev/null 2>&1
if [ $? -eq 0 ]; then
    echo "✓ 业务数据库可访问"
else
    echo "✗ 无法访问业务数据库"
    exit 1
fi
echo ""

# 2. 查询实际的表
echo "2. 查询业务数据库中的实际表..."
docker exec -i business-postgres psql -U business -d business <<EOF
SELECT schemaname, COUNT(*) as table_count
FROM pg_tables
WHERE schemaname IN ('public', 'business_data', 'archive', 'staging', 'topology')
GROUP BY schemaname
ORDER BY schemaname;
EOF
echo ""

# 3. 测试扫描器使用的SQL查询
echo "3. 测试扫描器的SQL查询（public schema）..."
docker exec -i business-postgres psql -U business -d business <<EOF
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
EOF
echo ""

# 4. 检查元数据表
echo "4. 检查元数据表中的数据..."
docker-compose exec -T postgres psql -U addp -d addp <<EOF
-- Schema nodes
SELECT 'Schema节点:' as info, COUNT(*) as count FROM metadata.meta_node WHERE node_type = 'schema';

-- Items per schema
SELECT
    mn.name as schema_name,
    COUNT(mi.id) as item_count
FROM metadata.meta_node mn
LEFT JOIN metadata.meta_item mi ON mn.id = mi.node_id
WHERE mn.node_type = 'schema'
GROUP BY mn.id, mn.name
ORDER BY mn.id DESC;

-- Scan logs
SELECT
    'Scan日志:' as info,
    id,
    scan_type,
    status,
    schemas_scanned,
    tables_scanned,
    error_message
FROM metadata.scan_logs
ORDER BY id DESC
LIMIT 3;
EOF
echo ""

# 5. 检查资源配置
echo "5. 检查资源配置..."
docker-compose exec -T postgres psql -U addp -d addp <<EOF
SELECT
    id,
    name,
    resource_type,
    connection_info->>'host' as host,
    connection_info->>'port' as port,
    connection_info->>'database' as database,
    connection_info->>'user' as username
FROM system.resources
WHERE id = 2;
EOF
echo ""

echo "======================================"
echo "诊断完成！"
echo ""
echo "如果上面显示："
echo "  - 业务数据库有表，但"
echo "  - 元数据表中 item_count = 0"
echo "  - 扫描器SQL查询返回结果"
echo ""
echo "那么问题很可能是："
echo "  1. Meta后端无法连接到业务数据库（密码解密失败）"
echo "  2. 扫描过程中出错但被忽略"
echo ""
echo "建议："
echo "  - 检查 .env 中的 ENCRYPTION_KEY"
echo "  - 重启 Meta 后端服务"
echo "  - 在前端UI重新执行扫描，观察日志"
echo "======================================"

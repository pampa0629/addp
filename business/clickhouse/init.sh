#!/bin/bash
# ClickHouse 初始化脚本
# 创建默认数据库和测试表

set -e

echo "=== ClickHouse 初始化开始 ==="

# 等待 ClickHouse 启动
sleep 5

# 创建业务数据库（如果不存在）
clickhouse-client --query "CREATE DATABASE IF NOT EXISTS business"

# 创建测试表示例
clickhouse-client --database=business --query "
CREATE TABLE IF NOT EXISTS test_table (
    id UInt32,
    name String,
    created_at DateTime DEFAULT now()
) ENGINE = MergeTree()
ORDER BY id
"

# 插入测试数据
clickhouse-client --database=business --query "
INSERT INTO test_table (id, name) VALUES
    (1, 'ClickHouse Test 1'),
    (2, 'ClickHouse Test 2'),
    (3, 'ClickHouse Test 3')
"

# 验证数据
echo "验证 ClickHouse 数据..."
clickhouse-client --database=business --query "SELECT count(*) as total FROM test_table"

echo "=== ClickHouse 初始化完成 ==="

#!/bin/bash
# ClickHouse 测试数据脚本
# 创建多个测试表并插入示例数据

set -e

echo "=== ClickHouse 测试数据初始化开始 ==="

# 1. 创建用户分析表
clickhouse-client --database=business --query "
CREATE TABLE IF NOT EXISTS user_analytics (
    user_id UInt32,
    event_type String,
    event_time DateTime,
    page_url String,
    duration_seconds UInt32,
    device_type String
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(event_time)
ORDER BY (user_id, event_time)
"

# 2. 插入用户分析测试数据
clickhouse-client --database=business --query "
INSERT INTO user_analytics (user_id, event_type, event_time, page_url, duration_seconds, device_type) VALUES
    (1001, 'page_view', '2025-12-25 10:00:00', '/home', 120, 'desktop'),
    (1001, 'click', '2025-12-25 10:02:00', '/products', 45, 'desktop'),
    (1002, 'page_view', '2025-12-25 10:05:00', '/home', 90, 'mobile'),
    (1002, 'search', '2025-12-25 10:06:00', '/search?q=data', 30, 'mobile'),
    (1003, 'page_view', '2025-12-25 10:10:00', '/about', 60, 'tablet'),
    (1003, 'click', '2025-12-25 10:11:00', '/contact', 20, 'tablet')
"

# 3. 创建销售数据表
clickhouse-client --database=business --query "
CREATE TABLE IF NOT EXISTS sales_data (
    order_id UInt64,
    product_id UInt32,
    product_name String,
    quantity UInt32,
    price Decimal(10, 2),
    order_date Date,
    customer_id UInt32,
    region String
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(order_date)
ORDER BY (order_date, order_id)
"

# 4. 插入销售测试数据
clickhouse-client --database=business --query "
INSERT INTO sales_data (order_id, product_id, product_name, quantity, price, order_date, customer_id, region) VALUES
    (10001, 101, 'Laptop Pro 15', 2, 1299.99, '2025-12-20', 5001, '华东'),
    (10002, 102, 'Wireless Mouse', 5, 29.99, '2025-12-21', 5002, '华南'),
    (10003, 103, 'USB-C Hub', 3, 49.99, '2025-12-22', 5003, '华北'),
    (10004, 101, 'Laptop Pro 15', 1, 1299.99, '2025-12-23', 5004, '西南'),
    (10005, 104, 'Keyboard RGB', 2, 89.99, '2025-12-24', 5005, '华东'),
    (10006, 102, 'Wireless Mouse', 10, 29.99, '2025-12-25', 5006, '华南')
"

# 5. 创建日志分析表
clickhouse-client --database=business --query "
CREATE TABLE IF NOT EXISTS app_logs (
    log_id UInt64,
    timestamp DateTime,
    level String,
    service_name String,
    message String,
    user_id Nullable(UInt32),
    request_id String
) ENGINE = MergeTree()
PARTITION BY toYYYYMMDD(timestamp)
ORDER BY (timestamp, log_id)
"

# 6. 插入日志测试数据
clickhouse-client --database=business --query "
INSERT INTO app_logs (log_id, timestamp, level, service_name, message, user_id, request_id) VALUES
    (1, '2025-12-25 10:00:00', 'INFO', 'api-gateway', 'Request received', 1001, 'req-001'),
    (2, '2025-12-25 10:00:01', 'INFO', 'auth-service', 'User authenticated', 1001, 'req-001'),
    (3, '2025-12-25 10:00:05', 'WARN', 'payment-service', 'Slow query detected', NULL, 'req-002'),
    (4, '2025-12-25 10:01:00', 'ERROR', 'database', 'Connection timeout', NULL, 'req-003'),
    (5, '2025-12-25 10:02:00', 'INFO', 'api-gateway', 'Request completed', 1002, 'req-004')
"

# 7. 验证数据
echo ""
echo "=== 验证 ClickHouse 测试数据 ==="
echo ""
echo "用户分析数据:"
clickhouse-client --database=business --query "SELECT COUNT(*) as total, COUNT(DISTINCT user_id) as unique_users FROM user_analytics"
echo ""
echo "销售数据:"
clickhouse-client --database=business --query "SELECT COUNT(*) as total_orders, SUM(quantity * price) as total_revenue FROM sales_data"
echo ""
echo "日志数据:"
clickhouse-client --database=business --query "SELECT level, COUNT(*) as count FROM app_logs GROUP BY level ORDER BY count DESC"
echo ""

echo "=== ClickHouse 测试数据初始化完成 ==="

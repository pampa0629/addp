#!/bin/bash
# 测试数据初始化脚本

BASE_URL="http://localhost:9090"

echo "🚀 开始插入测试数据..."

# 创建一个复杂的数据血缘图谱
# 数据流: source_db -> staging -> transform -> target_db
#         file -> staging
#         api -> staging

# 注意：需要先创建 items 表的测试数据
# 为了演示，我们直接通过SQL插入

# 先检查数据库连接
if ! psql -h localhost -U addp -d addp -c "SELECT 1;" > /dev/null 2>&1; then
    echo "❌ 数据库连接失败，请确保PostgreSQL正在运行"
    echo "尝试使用默认密码..."
fi

# 插入测试节点数据
psql -h localhost -U addp -d addp << 'EOF'
-- 插入测试节点
INSERT INTO lineage.items (item_id, name, type, schema_name, attributes, tenant_id) VALUES
  -- 源数据
  (1, 'customers', 'source_table', 'crm', '{"database": "source_db", "description": "客户主数据"}', 1),
  (2, 'orders', 'source_table', 'sales', '{"database": "source_db", "description": "订单数据"}', 1),
  (3, 'products', 'source_table', 'inventory', '{"database": "source_db", "description": "产品数据"}', 1),
  (4, 'user_behavior.json', 'file', '', '{"path": "/data/raw/user_behavior.json", "format": "json"}', 1),
  (5, 'weather_api', 'api', '', '{"endpoint": "https://api.weather.com", "type": "REST"}', 1),

  -- 中间层（Staging）
  (10, 'stg_customers', 'staging_table', 'staging', '{"database": "dwh", "description": "客户暂存表"}', 1),
  (11, 'stg_orders', 'staging_table', 'staging', '{"database": "dwh", "description": "订单暂存表"}', 1),
  (12, 'stg_products', 'staging_table', 'staging', '{"database": "dwh", "description": "产品暂存表"}', 1),
  (13, 'stg_user_behavior', 'staging_table', 'staging', '{"database": "dwh", "description": "用户行为暂存表"}', 1),
  (14, 'stg_weather', 'staging_table', 'staging', '{"database": "dwh", "description": "天气数据暂存表"}', 1),

  -- 数据集市层
  (20, 'dim_customer', 'target_table', 'dw', '{"database": "dwh", "description": "客户维度表"}', 1),
  (21, 'dim_product', 'target_table', 'dw', '{"database": "dwh", "description": "产品维度表"}', 1),
  (22, 'fact_order', 'target_table', 'dw', '{"database": "dwh", "description": "订单事实表"}', 1),
  (23, 'fact_user_behavior', 'target_table', 'dw', '{"database": "dwh", "description": "用户行为事实表"}', 1),

  -- 视图和报表层
  (30, 'v_customer_360', 'view', 'reports', '{"database": "dwh", "description": "客户360视图"}', 1),
  (31, 'v_sales_report', 'view', 'reports', '{"database": "dwh", "description": "销售报表"}', 1),
  (32, 'rpt_monthly_sales', 'target_table', 'reports', '{"database": "dwh", "description": "月度销售报表"}', 1)
ON CONFLICT (item_id) DO NOTHING;

-- 插入血缘关系
-- 源 -> Staging
INSERT INTO lineage.lineage_relations (
  external_workflow_id, workflow_engine,
  source_item_id, source_type, source_path,
  target_item_id, target_type, target_path,
  lineage_type, transform_config, status,
  start_time, end_time, duration_ms, records_processed, bytes_written
) VALUES
  -- 源表到暂存表
  ('etl-daily', 'airflow', 1, 'table', 'source_db.crm.customers', 10, 'table', 'dwh.staging.stg_customers',
   'transform', '{"operation": "extract", "type": "full_load"}', 'success',
   NOW() - INTERVAL '1 hour', NOW() - INTERVAL '50 minutes', 600000, 10000, 500000),

  ('etl-daily', 'airflow', 2, 'table', 'source_db.sales.orders', 11, 'table', 'dwh.staging.stg_orders',
   'transform', '{"operation": "extract", "type": "incremental"}', 'success',
   NOW() - INTERVAL '1 hour', NOW() - INTERVAL '50 minutes', 600000, 50000, 2500000),

  ('etl-daily', 'airflow', 3, 'table', 'source_db.inventory.products', 12, 'table', 'dwh.staging.stg_products',
   'transform', '{"operation": "extract", "type": "full_load"}', 'success',
   NOW() - INTERVAL '1 hour', NOW() - INTERVAL '50 minutes', 600000, 5000, 250000),

  ('etl-daily', 'airflow', 4, 'file', '/data/raw/user_behavior.json', 13, 'table', 'dwh.staging.stg_user_behavior',
   'transform', '{"operation": "parse_json", "type": "batch"}', 'success',
   NOW() - INTERVAL '1 hour', NOW() - INTERVAL '50 minutes', 300000, 100000, 5000000),

  ('etl-daily', 'airflow', 5, 'api', 'weather_api', 14, 'table', 'dwh.staging.stg_weather',
   'transform', '{"operation": "api_fetch", "type": "rest"}', 'success',
   NOW() - INTERVAL '1 hour', NOW() - INTERVAL '50 minutes', 120000, 1000, 50000),

  -- Staging -> 数据集市
  ('dw-transform', 'airflow', 10, 'table', 'dwh.staging.stg_customers', 20, 'table', 'dwh.dw.dim_customer',
   'transform', '{"operation": "scd_type2", "business_key": "customer_id"}', 'success',
   NOW() - INTERVAL '45 minutes', NOW() - INTERVAL '40 minutes', 300000, 10000, 600000),

  ('dw-transform', 'airflow', 12, 'table', 'dwh.staging.stg_products', 21, 'table', 'dwh.dw.dim_product',
   'transform', '{"operation": "scd_type1", "business_key": "product_id"}', 'success',
   NOW() - INTERVAL '45 minutes', NOW() - INTERVAL '40 minutes', 300000, 5000, 300000),

  ('dw-transform', 'airflow', 11, 'table', 'dwh.staging.stg_orders', 22, 'table', 'dwh.dw.fact_order',
   'transform', '{"operation": "fact_load", "grain": "order_line"}', 'success',
   NOW() - INTERVAL '45 minutes', NOW() - INTERVAL '35 minutes', 600000, 50000, 3000000),

  ('dw-transform', 'airflow', 13, 'table', 'dwh.staging.stg_user_behavior', 23, 'table', 'dwh.dw.fact_user_behavior',
   'transform', '{"operation": "fact_load", "grain": "event"}', 'success',
   NOW() - INTERVAL '45 minutes', NOW() - INTERVAL '35 minutes', 600000, 100000, 6000000),

  -- 数据集市 -> 视图/报表
  ('report-build', 'dbt', 20, 'table', 'dwh.dw.dim_customer', 30, 'view', 'dwh.reports.v_customer_360',
   'reference', '{"operation": "join", "type": "view"}', 'success',
   NOW() - INTERVAL '30 minutes', NOW() - INTERVAL '29 minutes', 60000, 10000, 0),

  ('report-build', 'dbt', 22, 'table', 'dwh.dw.fact_order', 30, 'view', 'dwh.reports.v_customer_360',
   'reference', '{"operation": "join", "type": "view"}', 'success',
   NOW() - INTERVAL '30 minutes', NOW() - INTERVAL '29 minutes', 60000, 50000, 0),

  ('report-build', 'dbt', 22, 'table', 'dwh.dw.fact_order', 31, 'view', 'dwh.reports.v_sales_report',
   'reference', '{"operation": "aggregate", "type": "view"}', 'success',
   NOW() - INTERVAL '30 minutes', NOW() - INTERVAL '29 minutes', 60000, 50000, 0),

  ('report-build', 'dbt', 21, 'table', 'dwh.dw.dim_product', 31, 'view', 'dwh.reports.v_sales_report',
   'reference', '{"operation": "join", "type": "view"}', 'success',
   NOW() - INTERVAL '30 minutes', NOW() - INTERVAL '29 minutes', 60000, 5000, 0),

  ('report-daily', 'airflow', 31, 'view', 'dwh.reports.v_sales_report', 32, 'table', 'dwh.reports.rpt_monthly_sales',
   'transform', '{"operation": "materialize", "type": "snapshot"}', 'success',
   NOW() - INTERVAL '25 minutes', NOW() - INTERVAL '20 minutes', 300000, 1000, 50000)
ON CONFLICT DO NOTHING;

SELECT '✅ 测试数据插入完成！';
SELECT '📊 节点统计：', COUNT(*) as node_count FROM lineage.items;
SELECT '🔗 血缘关系统计：', COUNT(*) as relation_count FROM lineage.lineage_relations;
EOF

echo ""
echo "✅ 测试数据初始化完成！"
echo ""
echo "📋 可用的节点ID列表："
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "源数据层："
echo "  1  - customers (客户表)"
echo "  2  - orders (订单表)"
echo "  3  - products (产品表)"
echo "  4  - user_behavior.json (用户行为文件)"
echo "  5  - weather_api (天气API)"
echo ""
echo "Staging层："
echo "  10 - stg_customers"
echo "  11 - stg_orders"
echo "  12 - stg_products"
echo "  13 - stg_user_behavior"
echo "  14 - stg_weather"
echo ""
echo "数据集市层："
echo "  20 - dim_customer (客户维度)"
echo "  21 - dim_product (产品维度)"
echo "  22 - fact_order (订单事实)"
echo "  23 - fact_user_behavior (用户行为事实)"
echo ""
echo "报表层："
echo "  30 - v_customer_360 (客户360视图)"
echo "  31 - v_sales_report (销售报表视图)"
echo "  32 - rpt_monthly_sales (月度销售报表)"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "💡 使用建议："
echo "  1. 在界面输入任意节点ID（如：1, 10, 20, 30）"
echo "  2. 点击「查询血缘图谱」查看完整血缘关系"
echo "  3. 点击「查看全部」查看所有血缘"
echo ""

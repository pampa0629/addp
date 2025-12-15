#!/bin/bash
# ============================================================
# 02-data-load/csv_import.sh
# Doris CSV 文件批量导入完整示例
# ============================================================

set -e

# 配置参数
FE_HOST="127.0.0.1"
FE_HTTP_PORT="18030"
DB_NAME="learning_db"
TABLE_NAME="sales_data"
USER="root"
PASSWORD=""

echo "============================================================"
echo "Doris CSV 文件批量导入完整示例"
echo "============================================================"

# ============================================================
# 1. 生成大批量 CSV 测试数据
# ============================================================

echo "1. 生成大批量 CSV 测试数据..."

cat > /tmp/sales_data.csv <<EOF
order_id,user_id,product_id,category,amount,order_date
100001,1001,2001,Electronics,299.99,2025-01-01
100002,1002,2002,Books,49.99,2025-01-01
100003,1003,2003,Clothing,199.99,2025-01-01
100004,1004,2004,Food,19.99,2025-01-01
100005,1005,2005,Sports,399.99,2025-01-01
100006,1001,2006,Electronics,599.99,2025-01-02
100007,1002,2007,Books,79.99,2025-01-02
100008,1003,2008,Clothing,299.99,2025-01-02
100009,1004,2009,Food,29.99,2025-01-02
100010,1005,2010,Sports,499.99,2025-01-02
EOF

# 生成更多数据（模拟大批量导入）
for i in {11..100}; do
    order_id=$((100000 + i))
    user_id=$((1000 + (i % 5) + 1))
    product_id=$((2000 + i))
    categories=("Electronics" "Books" "Clothing" "Food" "Sports")
    category=${categories[$((i % 5))]}
    amount=$(echo "scale=2; ($RANDOM % 500 + 10) / 1" | bc)
    date="2025-01-0$((i % 3 + 1))"
    echo "${order_id},${user_id},${product_id},${category},${amount},${date}" >> /tmp/sales_data.csv
done

echo "✅ 生成了 100 条 CSV 测试数据：/tmp/sales_data.csv"

# ============================================================
# 2. 创建目标表
# ============================================================

echo ""
echo "2. 创建目标表..."

mysql -h${FE_HOST} -P19030 -u${USER} <<EOF
USE ${DB_NAME};

CREATE TABLE IF NOT EXISTS ${TABLE_NAME} (
    order_id BIGINT COMMENT '订单ID',
    user_id BIGINT COMMENT '用户ID',
    product_id INT COMMENT '商品ID',
    category VARCHAR(100) COMMENT '商品类别',
    amount DECIMAL(10,2) COMMENT '订单金额',
    order_date DATE COMMENT '订单日期'
)
DUPLICATE KEY(order_id, user_id)
COMMENT 'CSV 导入销售数据表'
PARTITION BY RANGE(order_date) (
    PARTITION p20250101 VALUES LESS THAN ("2025-01-02"),
    PARTITION p20250102 VALUES LESS THAN ("2025-01-03"),
    PARTITION p20250103 VALUES LESS THAN ("2025-01-04")
)
DISTRIBUTED BY HASH(order_id) BUCKETS 10;
EOF

echo "✅ 表创建成功：${DB_NAME}.${TABLE_NAME}"

# ============================================================
# 3. 使用 Stream Load 导入 CSV 文件
# ============================================================

echo ""
echo "3. 使用 Stream Load 导入 CSV 文件..."

LABEL="csv_import_$(date +%s)"

RESULT=$(curl -s --location-trusted \
    -u ${USER}:${PASSWORD} \
    -H "label:${LABEL}" \
    -H "format: csv" \
    -H "column_separator: ," \
    -H "skip_header: 1" \
    -T /tmp/sales_data.csv \
    http://${FE_HOST}:${FE_HTTP_PORT}/api/${DB_NAME}/${TABLE_NAME}/_stream_load)

echo ""
echo "导入结果："
echo "$RESULT" | python3 -m json.tool 2>/dev/null || echo "$RESULT"

# ============================================================
# 4. 验证导入结果
# ============================================================

echo ""
echo "4. 验证导入结果..."

mysql -h${FE_HOST} -P19030 -u${USER} <<EOF
USE ${DB_NAME};

-- 查看总数据量
SELECT COUNT(*) as total_orders FROM ${TABLE_NAME};

-- 按类别统计
SELECT
    category,
    COUNT(*) as order_count,
    SUM(amount) as total_amount,
    AVG(amount) as avg_amount
FROM ${TABLE_NAME}
GROUP BY category
ORDER BY total_amount DESC;

-- 按日期统计
SELECT
    order_date,
    COUNT(*) as order_count,
    SUM(amount) as daily_amount
FROM ${TABLE_NAME}
GROUP BY order_date
ORDER BY order_date;

-- 查看前10条数据
SELECT * FROM ${TABLE_NAME} LIMIT 10;
EOF

# ============================================================
# 5. 高级导入示例：部分列导入
# ============================================================

echo ""
echo "5. 高级示例：部分列导入（跳过某些列）..."

# 创建只有部分列的 CSV
cat > /tmp/sales_partial.csv <<EOF
order_id,user_id,amount
200001,1001,299.99
200002,1002,399.99
200003,1003,499.99
EOF

# 使用 columns 参数指定列映射
curl -s --location-trusted \
    -u ${USER}:${PASSWORD} \
    -H "label:csv_partial_$(date +%s)" \
    -H "format: csv" \
    -H "column_separator: ," \
    -H "skip_header: 1" \
    -H "columns: order_id,user_id,amount,product_id=0,category='Unknown',order_date=current_date()" \
    -T /tmp/sales_partial.csv \
    http://${FE_HOST}:${FE_HTTP_PORT}/api/${DB_NAME}/${TABLE_NAME}/_stream_load | python3 -m json.tool 2>/dev/null

echo ""
echo "✅ 部分列导入完成"

# ============================================================
# 6. 高级导入示例：列转换
# ============================================================

echo ""
echo "6. 高级示例：列转换（数据清洗）..."

# 创建需要转换的 CSV
cat > /tmp/sales_transform.csv <<EOF
order_id,user_id,amount_str,order_date_str
300001,1001,$599.99,2025/01/01
300002,1002,$699.99,2025/01/02
300003,1003,$799.99,2025/01/03
EOF

# 使用 columns 参数进行数据转换
curl -s --location-trusted \
    -u ${USER}:${PASSWORD} \
    -H "label:csv_transform_$(date +%s)" \
    -H "format: csv" \
    -H "column_separator: ," \
    -H "skip_header: 1" \
    -H "columns: order_id,user_id,amount_str,order_date_str,product_id=0,category='Premium',amount=cast(replace(amount_str,'$','') as decimal(10,2)),order_date=str_to_date(order_date_str,'%Y/%m/%d')" \
    -T /tmp/sales_transform.csv \
    http://${FE_HOST}:${FE_HTTP_PORT}/api/${DB_NAME}/${TABLE_NAME}/_stream_load | python3 -m json.tool 2>/dev/null

echo ""
echo "✅ 列转换导入完成"

# ============================================================
# 7. 验证最终结果
# ============================================================

echo ""
echo "7. 验证最终导入结果..."

mysql -h${FE_HOST} -P19030 -u${USER} <<EOF
USE ${DB_NAME};

SELECT COUNT(*) as total_orders FROM ${TABLE_NAME};

SELECT * FROM ${TABLE_NAME}
WHERE order_id >= 200000
ORDER BY order_id
LIMIT 10;
EOF

# ============================================================
# 8. 清理临时文件
# ============================================================

echo ""
echo "8. 清理临时文件..."
rm -f /tmp/sales_data.csv /tmp/sales_partial.csv /tmp/sales_transform.csv
echo "✅ 临时文件清理完成"

echo ""
echo "============================================================"
echo "CSV 文件批量导入示例执行完成！"
echo "============================================================"
echo ""
echo "📝 CSV 导入关键参数："
echo "  - format: csv"
echo "  - column_separator: 列分隔符（默认逗号）"
echo "  - skip_header: 跳过表头行数"
echo "  - columns: 列映射和转换规则"
echo ""
echo "💡 高级特性："
echo "  1. 部分列导入：只导入部分列，其他列设置默认值"
echo "  2. 列转换：使用表达式转换数据（replace, cast, str_to_date 等）"
echo "  3. 条件过滤：where 子句过滤不符合条件的数据"
echo ""
echo "🚀 性能提示："
echo "  - 单次导入建议：1-10GB 数据"
echo "  - 大文件建议：拆分成多个小文件并行导入"
echo "  - 网络超时：max_filter_ratio 设置容错率"
echo ""

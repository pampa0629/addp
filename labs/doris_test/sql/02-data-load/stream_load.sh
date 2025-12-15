#!/bin/bash
# ============================================================
# 02-data-load/stream_load.sh
# Doris Stream Load 高性能批量导入示例
# ============================================================

set -e

# 配置参数
FE_HOST="127.0.0.1"
FE_HTTP_PORT="18030"  # 学习环境使用 18030，业务环境使用 8030
DB_NAME="learning_db"
TABLE_NAME="user_events_stream"
USER="root"
PASSWORD=""

echo "============================================================"
echo "Doris Stream Load 批量导入示例"
echo "============================================================"

# ============================================================
# 1. 准备测试数据文件
# ============================================================

echo "1. 准备测试数据文件..."

# 创建 CSV 测试数据
cat > /tmp/user_events.csv <<EOF
1001,2025-01-01 10:00:00,click,/home,Chrome,30
1001,2025-01-01 10:05:00,view,/product/1,Chrome,45
1002,2025-01-01 11:00:00,click,/home,Safari,20
1002,2025-01-01 11:10:00,purchase,/checkout,Safari,120
1003,2025-01-01 12:00:00,view,/product/2,Firefox,60
1003,2025-01-01 12:15:00,cart,/cart,Firefox,15
1004,2025-01-01 13:00:00,click,/home,Edge,25
1004,2025-01-01 13:30:00,view,/product/3,Edge,90
1005,2025-01-01 14:00:00,purchase,/checkout,Chrome,300
1006,2025-01-01 15:00:00,click,/category,Safari,40
EOF

echo "✅ CSV 数据文件创建成功：/tmp/user_events.csv"

# 创建 JSON 测试数据
cat > /tmp/user_events.json <<EOF
[
  {"user_id": 2001, "event_time": "2025-01-02 10:00:00", "event_type": "click", "page_url": "/home", "device": "iOS", "duration": 20},
  {"user_id": 2001, "event_time": "2025-01-02 10:10:00", "event_type": "view", "page_url": "/product/10", "device": "iOS", "duration": 50},
  {"user_id": 2002, "event_time": "2025-01-02 11:00:00", "event_type": "purchase", "page_url": "/checkout", "device": "Android", "duration": 180},
  {"user_id": 2003, "event_time": "2025-01-02 12:00:00", "event_type": "view", "page_url": "/product/20", "device": "Web", "duration": 70},
  {"user_id": 2004, "event_time": "2025-01-02 13:00:00", "event_type": "cart", "page_url": "/cart", "device": "iOS", "duration": 30}
]
EOF

echo "✅ JSON 数据文件创建成功：/tmp/user_events.json"

# ============================================================
# 2. 创建目标表（如果不存在）
# ============================================================

echo ""
echo "2. 创建目标表..."

mysql -h${FE_HOST} -P19030 -u${USER} <<EOF
USE ${DB_NAME};

CREATE TABLE IF NOT EXISTS ${TABLE_NAME} (
    user_id BIGINT COMMENT '用户ID',
    event_time DATETIME COMMENT '事件时间',
    event_type VARCHAR(50) COMMENT '事件类型',
    page_url VARCHAR(200) COMMENT '页面URL',
    device VARCHAR(50) COMMENT '设备类型',
    duration INT COMMENT '停留时长（秒）'
)
DUPLICATE KEY(user_id, event_time)
COMMENT 'Stream Load 导入示例表'
DISTRIBUTED BY HASH(user_id) BUCKETS 10;
EOF

echo "✅ 表创建成功：${DB_NAME}.${TABLE_NAME}"

# ============================================================
# 3. Stream Load 导入 CSV 数据
# ============================================================

echo ""
echo "3. 使用 Stream Load 导入 CSV 数据..."

curl --location-trusted \
    -u ${USER}:${PASSWORD} \
    -H "label:stream_load_csv_$(date +%s)" \
    -H "format: csv" \
    -H "column_separator: ," \
    -T /tmp/user_events.csv \
    http://${FE_HOST}:${FE_HTTP_PORT}/api/${DB_NAME}/${TABLE_NAME}/_stream_load

echo ""
echo "✅ CSV 数据导入完成"

# ============================================================
# 4. Stream Load 导入 JSON 数据
# ============================================================

echo ""
echo "4. 使用 Stream Load 导入 JSON 数据..."

curl --location-trusted \
    -u ${USER}:${PASSWORD} \
    -H "label:stream_load_json_$(date +%s)" \
    -H "format: json" \
    -H "strip_outer_array: true" \
    -T /tmp/user_events.json \
    http://${FE_HOST}:${FE_HTTP_PORT}/api/${DB_NAME}/${TABLE_NAME}/_stream_load

echo ""
echo "✅ JSON 数据导入完成"

# ============================================================
# 5. 验证导入结果
# ============================================================

echo ""
echo "5. 验证导入结果..."

mysql -h${FE_HOST} -P19030 -u${USER} <<EOF
USE ${DB_NAME};

SELECT COUNT(*) as total_count FROM ${TABLE_NAME};

SELECT event_type, COUNT(*) as count
FROM ${TABLE_NAME}
GROUP BY event_type
ORDER BY count DESC;

SELECT * FROM ${TABLE_NAME} LIMIT 10;
EOF

echo ""
echo "✅ 导入验证完成"

# ============================================================
# 6. 清理临时文件
# ============================================================

echo ""
echo "6. 清理临时文件..."
rm -f /tmp/user_events.csv /tmp/user_events.json
echo "✅ 临时文件清理完成"

echo ""
echo "============================================================"
echo "Stream Load 导入示例执行完成！"
echo "============================================================"
echo ""
echo "📝 Stream Load 关键参数说明："
echo "  - label: 导入任务唯一标识（同一 label 不会重复导入）"
echo "  - format: 数据格式（csv, json）"
echo "  - column_separator: CSV 列分隔符（默认 \\t）"
echo "  - strip_outer_array: JSON 数组解析（true 表示解析外层数组）"
echo ""
echo "🚀 性能对比："
echo "  - INSERT 单条：100-500 行/秒"
echo "  - INSERT 批量：1万-5万 行/秒"
echo "  - Stream Load：10万-50万 行/秒 ⭐"
echo ""
echo "💡 适用场景："
echo "  - 大批量数据导入（> 10万行）"
echo "  - 实时数据流导入（配合 Kafka、Flink）"
echo "  - CSV/JSON 文件批量导入"
echo ""

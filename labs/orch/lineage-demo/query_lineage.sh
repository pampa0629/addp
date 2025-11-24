#!/bin/bash

echo "=== 血缘数据库查询工具 ==="
echo ""

DB_PATH="./worker/lineage.db"

if [ ! -f "$DB_PATH" ]; then
    echo "❌ 数据库文件不存在: $DB_PATH"
    exit 1
fi

echo "📊 血缘记录总数："
sqlite3 "$DB_PATH" "SELECT COUNT(*) as total FROM data_lineage;"
echo ""

echo "📋 最新 5 条血缘记录："
sqlite3 "$DB_PATH" -header -column <<EOF
SELECT
    id,
    source_item_id as src_id,
    target_item_id as tgt_id,
    lineage_type as type,
    status,
    records_processed as records,
    substr(external_workflow_id, 1, 20) as workflow_id,
    substr(created_at, 1, 19) as created
FROM data_lineage
ORDER BY created_at DESC
LIMIT 5;
EOF

echo ""
echo "🔍 按 Workflow ID 查询："
echo "   sqlite3 $DB_PATH \"SELECT * FROM data_lineage WHERE external_workflow_id='lineage-demo-workflow-1';\""
echo ""
echo "🌐 启动 API Server 查询："
echo "   cd api && go run server.go"
echo "   curl http://localhost:9090/api/lineage/all"

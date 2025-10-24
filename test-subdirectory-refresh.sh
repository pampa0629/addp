#!/bin/bash
TOKEN="eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VyX2lkIjoxLCJ1c2VybmFtZSI6IlN1cGVyQWRtaW4iLCJleHAiOjE3NjEyNzQwNjQsImlhdCI6MTc2MTI3MjI2NH0.ofR-lTZl6lL_N9jqe_0POoidft6P9LGn6c7ZRw5iRh0"

# Clean data
echo "=== 清理数据 ==="
PGPASSWORD=addp_password psql -h localhost -U addp -d addp -c "DELETE FROM metadata.meta_item WHERE res_id = 9;" > /dev/null
PGPASSWORD=addp_password psql -h localhost -U addp -d addp -c "DELETE FROM metadata.meta_node WHERE res_id = 9 AND parent_node_id IS NOT NULL;" > /dev/null

# Subdirectory refresh only
echo "=== 触发subdirectory刷新 ==="
curl -s -X POST "http://localhost:8081/api/data-explorer/resources/9/refresh" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d '{"node_type": "prefix", "schema": "addp", "path": "shapefile", "full_path": "addp/shapefile"}' > /dev/null
sleep 6

# Check logs
echo ""
echo "=== 日志：处理meta对象 ==="
grep "处理meta对象" /tmp/meta-debug.log | tail -10

echo ""
echo "=== 日志：跳过或特殊处理 ==="
grep "跳过或特殊处理" /tmp/meta-debug.log | tail -5

echo ""
echo "=== 数据库状态 ==="
PGPASSWORD=addp_password psql -h localhost -U addp -d addp -c "SELECT id, parent_node_id, name, full_name, depth FROM metadata.meta_node WHERE res_id = 9 AND (name = 'shapefile' OR full_name LIKE '%shapefile%') ORDER BY depth, id;"

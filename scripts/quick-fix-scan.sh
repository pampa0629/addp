#!/bin/bash

echo "========================================"
echo "快速修复：元数据扫描问题"
echo "========================================"
echo ""

echo "步骤1: 清理旧扫描数据..."
docker-compose exec -T postgres psql -U addp -d addp <<EOF > /dev/null 2>&1
DELETE FROM metadata.meta_item WHERE res_id = 2;
DELETE FROM metadata.meta_node WHERE res_id = 2;
DELETE FROM metadata.scan_logs WHERE resource_id = 2;
EOF
echo "✓ 已清理"

echo ""
echo "步骤2: 停止Meta后端..."
pkill -f "meta.*main.go" 2>/dev/null
echo "✓ 已停止"

echo ""
echo "步骤3: 启动Meta后端并观察日志..."
echo "Meta后端正在启动，请等待..."

cd /Users/pampa/code/addp/meta/backend
go run cmd/server/main.go > /tmp/meta-scan-debug.log 2>&1 &
META_PID=$!
echo "Meta PID: $META_PID"

echo ""
echo "等待10秒让服务启动..."
sleep 10

echo ""
echo "步骤4: 测试Meta API是否正常..."
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8082/health 2>/dev/null || echo "000")
if [ "$HTTP_CODE" = "200" ]; then
    echo "✓ Meta API正常运行"
else
    echo "✗ Meta API未响应 (HTTP $HTTP_CODE)"
    echo "查看日志:"
    tail -20 /tmp/meta-scan-debug.log
    exit 1
fi

echo ""
echo "========================================"
echo "现在请在浏览器中:"
echo "1. 访问 http://localhost:5170"
echo "2. 进入Meta模块"
echo "3. 点击 'pg业务库'"
echo "4. 选择所有schema并点击'扫描'"
echo ""
echo "同时在另一个终端运行:"
echo "  tail -f /tmp/meta-scan-debug.log"
echo ""
echo "观察扫描日志输出，特别注意:"
echo "  - '开始扫描 Schema' 的日志"
echo "  - 'tables_count' 的值"
echo "  - 任何 error 信息"
echo "========================================"

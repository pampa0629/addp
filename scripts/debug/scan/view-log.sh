#!/bin/bash

# 查看 Meta 扫描日志的便捷脚本

echo "========================================="
echo "Meta 后端扫描日志查看器"
echo "========================================="
echo ""

LOG_FILE="logs/meta-backend.log"
ERR_LOG_FILE="logs/meta-backend-stderr.log"

if [ ! -f "$LOG_FILE" ]; then
    echo "❌ 日志文件不存在: $LOG_FILE"
    echo "   请先运行 ./scripts/dev/restart.sh 启动服务"
    exit 1
fi

echo "📊 关键扫描信息:"
echo ""

echo "【1. 连接字符串构建】"
grep "🔑 连接字符串已构建" "$LOG_FILE" | tail -3
echo ""

echo "【2. Scanner创建】"
grep "Scanner创建" "$LOG_FILE" | tail -3
echo ""

echo "【3. 开始扫描Schema】"
grep "开始扫描 Schema" "$LOG_FILE" | tail -5
echo ""

echo "【4. 调用ScanTables】"
grep "🔍 即将调用" "$LOG_FILE" | tail -5
echo ""

echo "【5. ScanTables返回结果】⭐ 最关键 ⭐"
grep "📊 scan.ScanTables 返回结果" "$LOG_FILE" | tail -5
echo ""

echo "【6. 空数组警告】"
WARNINGS=$(grep "⚠️  ScanTables 返回空数组" "$LOG_FILE" | tail -5)
if [ -n "$WARNINGS" ]; then
    echo "$WARNINGS"
else
    echo "✅ 没有空数组警告"
fi
echo ""

echo "【7. 错误信息】"
if [ -f "$ERR_LOG_FILE" ] && [ -s "$ERR_LOG_FILE" ]; then
    echo "错误日志文件: $ERR_LOG_FILE"
    tail -10 "$ERR_LOG_FILE"
else
    echo "✅ 没有错误日志"
fi
echo ""

echo "========================================="
echo ""
echo "💡 提示:"
echo "  - 实时查看日志: tail -f $LOG_FILE"
echo "  - 查看错误日志: tail -f $ERR_LOG_FILE"
echo "  - 搜索关键词: grep '关键词' $LOG_FILE"
echo "========================================="

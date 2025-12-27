#!/bin/bash

# 编译 Vector Tool
# 禁用 go.work 以避免与 ADDP 项目冲突

set -e

echo "🔨 编译 Vector Tool..."
GOWORK=off go build -o vector ./cmd/vector/main.go

echo "✅ 编译完成！"
echo "📍 可执行文件: ./vector"
echo ""
echo "使用示例:"
echo "  ./vector batch ../data          # 批量向量化"
echo "  ./vector search image.jpg 5     # 语义检索"
echo "  ./vector status                 # 查看状态"
echo "  ./vector init test.jpg          # 初始化"

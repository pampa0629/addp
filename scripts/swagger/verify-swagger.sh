#!/bin/bash
# 用途：验证各模块的 Swagger 文档可访问性
# 使用：bash scripts/swagger/verify-swagger.sh

set -e

echo "=== 验证 Swagger 文档可访问性 ==="
echo ""

SUCCESS=0
FAILED=0

# 定义模块列表（格式：模块名:端口）
MODULES="manager:8081 meta:8082 develop:8083 transfer:8084 service:8085 orchestrator:8086 monitor:8087 standard:8088 model:8089"

for entry in $MODULES; do
    IFS=':' read -r module port <<< "$entry"
    url="http://localhost:${port}/swagger/index.html"

    printf "%-15s (:%s) ... " "$module" "$port"

    if curl -s -o /dev/null -w "%{http_code}" "$url" | grep -q "200"; then
        echo "✓ 可访问"
        ((SUCCESS++))
    else
        echo "✗ 不可访问"
        ((FAILED++))
    fi
done

echo ""
echo "=== 验证结果 ==="
echo "成功: $SUCCESS"
echo "失败: $FAILED"

if [ $FAILED -eq 0 ]; then
    echo "✓ 所有模块的 Swagger 文档均可访问"
    exit 0
else
    echo "✗ 部分模块的 Swagger 文档不可访问，请检查"
    exit 1
fi

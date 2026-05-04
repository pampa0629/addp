#!/bin/bash
# 用途：验证各模块的 Swagger 文档内容覆盖和 UI 可访问性
# 使用：
#   bash scripts/swagger/verify-swagger.sh
#   bash scripts/swagger/verify-swagger.sh --coverage-only
#   bash scripts/swagger/verify-swagger.sh --ui-only

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

RUN_COVERAGE=true
RUN_UI=true

for arg in "$@"; do
    case "$arg" in
        --coverage-only)
            RUN_UI=false
            ;;
        --ui-only)
            RUN_COVERAGE=false
            ;;
        -h|--help)
            sed -n '1,5p' "$0" | sed 's/^# //'
            exit 0
            ;;
        *)
            echo "未知参数: $arg"
            exit 1
            ;;
    esac
done

if [ "$RUN_COVERAGE" = true ]; then
    echo "=== 验证 Swagger 路由覆盖 ==="
    echo ""
    SWAGGER_COVERAGE_WARN_ONLY="${SWAGGER_COVERAGE_WARN_ONLY:-1}" bash "${SCRIPT_DIR}/check-route-coverage.sh" all
    echo ""
fi

if [ "$RUN_UI" = false ]; then
    exit 0
fi

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

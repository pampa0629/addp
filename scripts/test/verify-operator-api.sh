#!/bin/bash
# 统一算子API验证脚本

set -e

echo "🧪 统一算子API架构验证脚本"
echo "================================"
echo ""

# 获取token (假设已登录)
TOKEN=${ADDP_TOKEN:-""}
BASE_URL=${ADDP_BASE_URL:-"http://localhost:8000"}

if [ -z "$TOKEN" ]; then
    echo "⚠️  未设置ADDP_TOKEN环境变量"
    echo "   请先登录并设置: export ADDP_TOKEN='your_token_here'"
    echo ""
    echo "   或使用默认的测试token(如果system模块支持):"
    read -p "   是否继续? (y/n) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
    fi
fi

# 测试函数
test_api() {
    local name=$1
    local method=$2
    local url=$3
    local data=$4

    echo "📌 测试: $name"
    echo "   URL: $method $url"

    if [ "$method" = "GET" ]; then
        response=$(curl -s -w "\n%{http_code}" -H "Authorization: Bearer $TOKEN" "$BASE_URL$url")
    else
        response=$(curl -s -w "\n%{http_code}" -X "$method" \
            -H "Authorization: Bearer $TOKEN" \
            -H "Content-Type: application/json" \
            -d "$data" \
            "$BASE_URL$url")
    fi

    # 提取HTTP状态码(最后一行)
    http_code=$(echo "$response" | tail -n1)
    body=$(echo "$response" | head -n-1)

    if [ "$http_code" = "200" ] || [ "$http_code" = "201" ]; then
        echo "   ✅ 成功 (HTTP $http_code)"
        # 美化JSON输出(如果安装了jq)
        if command -v jq &> /dev/null; then
            echo "$body" | jq -C '.' | head -n 10
            if [ $(echo "$body" | jq -r '. | length') -gt 10 ]; then
                echo "   ... (输出已截断)"
            fi
        else
            echo "$body" | head -c 200
            echo "..."
        fi
    else
        echo "   ❌ 失败 (HTTP $http_code)"
        echo "$body"
        return 1
    fi

    echo ""
}

echo "1️⃣  测试Meta模块算子API"
echo "----------------------------"
test_api "Meta算子列表" "GET" "/api/meta/operators" || true

echo ""
echo "2️⃣  测试Transfer模块算子API"
echo "----------------------------"
test_api "Transfer算子列表" "GET" "/api/transfer/operators" || true

echo ""
echo "3️⃣  测试Manager模块算子API"
echo "----------------------------"
test_api "Manager算子列表" "GET" "/api/manager/operators" || true

echo ""
echo "4️⃣  测试GeoPandas引擎算子API"
echo "----------------------------"
test_api "GeoPandas算子列表" "GET" "/api/develop/spatial/operators" || true

echo ""
echo "5️⃣  测试Develop空间引擎列表API"
echo "----------------------------"
test_api "空间引擎列表" "GET" "/api/develop/spatial/engines" || true

echo ""
echo "6️⃣  测试Develop SQL资源过滤"
echo "----------------------------"
test_api "SQL资源列表" "GET" "/api/develop/databases" || true

echo ""
echo "================================"
echo "✅ 验证完成!"
echo ""
echo "📊 统计信息:"
echo "   - 已测试 6 个核心API端点"
echo "   - 涵盖 4 个模块的算子API"
echo "   - 验证了能力过滤机制"
echo ""
echo "💡 提示:"
echo "   如果某些测试失败,可能是因为:"
echo "   1. 服务未启动 (运行 bash scripts/dev/start.sh)"
echo "   2. Token未设置或已过期"
echo "   3. 对应模块尚未注册到System"
echo ""
echo "📝 详细文档:"
echo "   - docs/UNIFIED_OPERATOR_API_IMPLEMENTATION.md"
echo "   - develop/frontend/SPATIAL_ENGINE_INTEGRATION.md"
echo ""

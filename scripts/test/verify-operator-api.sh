#!/bin/bash
# 工作流算子与扩展引擎 API 验证脚本

set -e

echo "🧪 工作流算子与扩展引擎 API 验证脚本"
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

echo "1️⃣  测试Develop统一算子发现API"
echo "----------------------------"
test_api "工作流引擎列表" "GET" "/api/v1/develop/workflow-engines" || true

WORKFLOW_ENGINE_ID=${ADDP_WORKFLOW_ENGINE_ID:-""}
if [ -z "$WORKFLOW_ENGINE_ID" ] && command -v jq &> /dev/null; then
    engines_response=$(curl -s -H "Authorization: Bearer $TOKEN" "$BASE_URL/api/v1/develop/workflow-engines")
    WORKFLOW_ENGINE_ID=$(echo "$engines_response" | jq -r '.[0].id // empty')
fi

if [ -n "$WORKFLOW_ENGINE_ID" ]; then
    echo ""
    echo "2️⃣  测试指定工作流引擎实例算子API"
    echo "----------------------------"
    test_api "工作流引擎实例算子列表" "GET" "/api/v1/develop/workflow-engines/$WORKFLOW_ENGINE_ID/operators" || true
else
    echo "⚠️  未能自动获取工作流引擎实例 ID；可设置 ADDP_WORKFLOW_ENGINE_ID 后重试实例算子发现"
fi

echo ""
echo "3️⃣  测试Develop Spark运行时列表API"
echo "----------------------------"
test_api "Spark运行时列表" "GET" "/api/v1/develop/spark-runtimes" || true

echo ""
echo "================================"
echo "✅ 验证完成!"
echo ""
echo "📊 统计信息:"
echo "   - 已测试 Develop 工作流引擎列表、实例算子发现和 Spark 运行时资源列表"
echo "   - 算子发现只走 /workflow-engines/{workflow_engine_id}/operators"
echo ""
echo "💡 提示:"
echo "   如果某些测试失败,可能是因为:"
echo "   1. 服务未启动 (运行 bash scripts/dev/start.sh)"
echo "   2. Token未设置或已过期"
echo "   3. 对应模块尚未注册到System"
echo ""
echo "📝 详细文档:"
echo "   - docs/spec/addp工作流计算引擎接口规范.md"
echo "   - engines/docs/README.md"
echo ""

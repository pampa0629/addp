#!/bin/bash

# 测试元数据表 API

echo "=== 测试元数据表 API ==="
echo ""

# 使用内部 API Key（绕过 JWT 认证）
INTERNAL_KEY="dev-internal-key"

echo "1. 测试 resource_id=4 (pg业务库)"
curl -s -H "X-Internal-API-Key: $INTERNAL_KEY" \
  "http://localhost:8082/api/meta/metadata/tables?resource_id=4" | jq .

echo ""
echo "2. 预期结果：应该返回14个表"
echo ""

echo "3. 测试 resource_id=9 (min存储 - 对象存储)"
curl -s -H "X-Internal-API-Key: $INTERNAL_KEY" \
  "http://localhost:8082/api/meta/metadata/tables?resource_id=9" | jq .

echo ""
echo "4. 预期结果：应该返回对象列表（不是表）"
echo ""

echo "=== 测试完成 ==="

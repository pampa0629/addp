#!/bin/bash

# Transfer 模块密码加密功能测试脚本

set -e

echo "========================================"
echo "Transfer 模块密码加密功能测试"
echo "========================================"
echo

# 颜色定义
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 获取 Token
echo "步骤 1: 获取认证 Token..."
TOKEN=$(curl -s -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"SuperAdmin","password":"admin123"}' | grep -o '"token":"[^"]*' | cut -d'"' -f4)

if [ -z "$TOKEN" ]; then
  echo -e "${RED}✗ 登录失败，无法获取 Token${NC}"
  exit 1
fi
echo -e "${GREEN}✓ 成功获取 Token${NC}"
echo

# 测试 1: 创建 PostgreSQL 资源（密码应该被加密）
echo "步骤 2: 创建 PostgreSQL 本地资源..."
CREATE_RESPONSE=$(curl -s -X POST http://localhost:8083/api/local-resources \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "测试PostgreSQL",
    "resource_type": "postgresql",
    "description": "密码加密测试",
    "is_active": true,
    "connection_info": {
      "host": "localhost",
      "port": 5432,
      "database": "test_db",
      "user": "test_user",
      "password": "plain_password_123"
    }
  }')

RESOURCE_ID=$(echo $CREATE_RESPONSE | grep -o '"id":[0-9]*' | head -1 | cut -d':' -f2)

if [ -z "$RESOURCE_ID" ]; then
  echo -e "${RED}✗ 创建资源失败${NC}"
  echo "Response: $CREATE_RESPONSE"
  exit 1
fi
echo -e "${GREEN}✓ 成功创建资源 ID: $RESOURCE_ID${NC}"
echo

# 测试 2: 直接查询数据库，验证密码已加密
echo "步骤 3: 检查数据库中的密码是否已加密..."
DB_PASSWORD=$(PGPASSWORD=addp_password psql -h localhost -U addp -d addp -t -c \
  "SELECT connection_info->>'password' FROM transfer.local_resources WHERE id=$RESOURCE_ID;")

if [[ "$DB_PASSWORD" == *"plain_password_123"* ]]; then
  echo -e "${RED}✗ 密码未加密！数据库中存储的是明文${NC}"
  echo "DB Password: $DB_PASSWORD"
  exit 1
else
  echo -e "${GREEN}✓ 密码已加密存储${NC}"
  echo "加密后的密码: $DB_PASSWORD"
fi
echo

# 测试 3: 通过 API 获取资源，验证密码已解密
echo "步骤 4: 通过 API 获取资源，验证密码已解密..."
GET_RESPONSE=$(curl -s http://localhost:8083/api/local-resources \
  -H "Authorization: Bearer $TOKEN")

API_PASSWORD=$(echo $GET_RESPONSE | grep -o '"password":"[^"]*' | head -1 | cut -d'"' -f4)

if [ "$API_PASSWORD" == "plain_password_123" ]; then
  echo -e "${GREEN}✓ API 返回的密码已正确解密${NC}"
  echo "解密后的密码: $API_PASSWORD"
else
  echo -e "${RED}✗ API 返回的密码不正确${NC}"
  echo "期望: plain_password_123"
  echo "实际: $API_PASSWORD"
  exit 1
fi
echo

# 测试 4: 测试连接功能（使用解密后的密码）
echo "步骤 5: 测试连接功能..."
TEST_RESPONSE=$(curl -s -X POST http://localhost:8083/api/local-resources/$RESOURCE_ID/test \
  -H "Authorization: Bearer $TOKEN")

echo "测试连接响应: $TEST_RESPONSE"
echo

# 测试 5: 创建 MinIO 资源（secret_key 应该被加密）
echo "步骤 6: 创建 MinIO 本地资源..."
MINIO_RESPONSE=$(curl -s -X POST http://localhost:8083/api/local-resources \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "测试MinIO",
    "resource_type": "minio",
    "description": "secret_key加密测试",
    "is_active": true,
    "connection_info": {
      "endpoint": "localhost:9002",
      "access_key": "minioadmin",
      "secret_key": "plain_secret_key_456",
      "use_ssl": false
    }
  }')

MINIO_ID=$(echo $MINIO_RESPONSE | grep -o '"id":[0-9]*' | head -1 | cut -d':' -f2)

if [ -z "$MINIO_ID" ]; then
  echo -e "${RED}✗ 创建 MinIO 资源失败${NC}"
  echo "Response: $MINIO_RESPONSE"
  exit 1
fi
echo -e "${GREEN}✓ 成功创建 MinIO 资源 ID: $MINIO_ID${NC}"
echo

# 测试 6: 验证 MinIO secret_key 已加密
echo "步骤 7: 检查 MinIO secret_key 是否已加密..."
DB_SECRET=$(PGPASSWORD=addp_password psql -h localhost -U addp -d addp -t -c \
  "SELECT connection_info->>'secret_key' FROM transfer.local_resources WHERE id=$MINIO_ID;")

if [[ "$DB_SECRET" == *"plain_secret_key_456"* ]]; then
  echo -e "${RED}✗ secret_key 未加密！数据库中存储的是明文${NC}"
  echo "DB Secret: $DB_SECRET"
  exit 1
else
  echo -e "${GREEN}✓ secret_key 已加密存储${NC}"
  echo "加密后的 secret_key: $DB_SECRET"
fi
echo

# 清理测试数据
echo "步骤 8: 清理测试数据..."
curl -s -X DELETE http://localhost:8083/api/local-resources/$RESOURCE_ID \
  -H "Authorization: Bearer $TOKEN" > /dev/null
curl -s -X DELETE http://localhost:8083/api/local-resources/$MINIO_ID \
  -H "Authorization: Bearer $TOKEN" > /dev/null
echo -e "${GREEN}✓ 测试数据已清理${NC}"
echo

echo "========================================"
echo -e "${GREEN}✓ 所有测试通过！${NC}"
echo "========================================"
echo
echo "测试结果总结:"
echo "1. ✓ PostgreSQL 密码在数据库中已加密存储"
echo "2. ✓ API 返回时密码已正确解密"
echo "3. ✓ MinIO secret_key 在数据库中已加密存储"
echo "4. ✓ 加密/解密功能正常工作"
echo

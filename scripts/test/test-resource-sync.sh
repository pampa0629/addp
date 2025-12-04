#!/bin/bash

# 测试资源缓存同步功能
# 演示 System 模块资源更新后，Meta 模块缓存自动失效

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}资源缓存同步功能测试${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# 检查环境变量
if [ -z "$TOKEN" ]; then
    echo -e "${YELLOW}⚠️  TOKEN 环境变量未设置${NC}"
    echo -e "${YELLOW}请先登录获取 token:${NC}"
    echo -e "  export TOKEN=\$(curl -s -X POST http://localhost:8080/api/auth/login \\"
    echo -e "    -H 'Content-Type: application/json' \\"
    echo -e "    -d '{\"username\":\"SuperAdmin\",\"password\":\"admin123\"}' | jq -r '.token')"
    echo ""
    exit 1
fi

SYSTEM_URL="http://localhost:8080"
META_URL="http://localhost:8082"

# 步骤 1: 创建测试资源
echo -e "${GREEN}[步骤 1]${NC} 在 System 模块创建测试资源..."
RESOURCE_RESPONSE=$(curl -s -X POST "$SYSTEM_URL/api/resources" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "测试资源同步-PostgreSQL",
    "resource_type": "postgresql",
    "description": "用于测试资源缓存同步",
    "connection_info": {
      "host": "localhost",
      "port": 5432,
      "database": "testdb",
      "user": "testuser",
      "password": "testpass123"
    }
  }')

RESOURCE_ID=$(echo "$RESOURCE_RESPONSE" | jq -r '.id')
if [ "$RESOURCE_ID" == "null" ] || [ -z "$RESOURCE_ID" ]; then
    echo -e "${RED}❌ 创建资源失败${NC}"
    echo "$RESOURCE_RESPONSE" | jq '.'
    exit 1
fi

echo -e "${GREEN}✅ 资源创建成功，ID: $RESOURCE_ID${NC}"
echo ""

# 等待 Redis 事件传播
sleep 1

# 步骤 2: Meta 模块访问资源（触发缓存）
echo -e "${GREEN}[步骤 2]${NC} Meta 模块访问资源（触发缓存）..."
META_RESPONSE=$(curl -s "$META_URL/api/resources?tenant_id=1" \
  -H "Authorization: Bearer $TOKEN")

echo -e "${BLUE}Meta 模块资源列表（首次访问，写入缓存）:${NC}"
echo "$META_RESPONSE" | jq ".[] | select(.id == $RESOURCE_ID) | {id, name, resource_type}"
echo ""

# 步骤 3: 查看 Meta 模块日志（确认缓存写入）
echo -e "${GREEN}[步骤 3]${NC} 查看 Meta 模块日志（确认缓存写入）..."
echo -e "${YELLOW}预期日志: 通过内部 API 获取资源连接信息成功${NC}"
sleep 1
echo ""

# 步骤 4: 更新资源配置
echo -e "${GREEN}[步骤 4]${NC} 在 System 模块更新资源配置..."
UPDATE_RESPONSE=$(curl -s -X PUT "$SYSTEM_URL/api/resources/$RESOURCE_ID" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "description": "已更新配置（测试缓存同步）",
    "connection_info": {
      "host": "newhost.example.com",
      "port": 5433,
      "database": "newdb",
      "user": "newuser",
      "password": "newpass456"
    }
  }')

echo -e "${GREEN}✅ 资源配置已更新${NC}"
echo "$UPDATE_RESPONSE" | jq '{id, name, description}'
echo ""

# 等待 Redis 事件传播和处理
echo -e "${YELLOW}⏳ 等待 Redis 事件传播...${NC}"
sleep 2

# 步骤 5: 查看 Meta 模块日志（确认收到事件并清除缓存）
echo -e "${GREEN}[步骤 5]${NC} 查看 Meta 模块日志（确认缓存已清除）..."
echo -e "${YELLOW}预期日志:${NC}"
echo -e "  1. 收到资源变更事件: resource_id=$RESOURCE_ID, action=update"
echo -e "  2. 资源已更新，缓存已清除: resource_id=$RESOURCE_ID"
echo ""

if [ -f "logs/meta-backend.log" ]; then
    echo -e "${BLUE}最近的相关日志:${NC}"
    tail -20 logs/meta-backend.log | grep -E "(收到资源变更事件|缓存已清除|resource_id=$RESOURCE_ID)" || echo "未找到相关日志"
else
    echo -e "${YELLOW}⚠️  未找到日志文件 logs/meta-backend.log${NC}"
fi
echo ""

# 步骤 6: Meta 模块再次访问资源（应该从 System 重新获取）
echo -e "${GREEN}[步骤 6]${NC} Meta 模块再次访问资源（应该获取新配置）..."
META_RESPONSE2=$(curl -s "$META_URL/api/resources?tenant_id=1" \
  -H "Authorization: Bearer $TOKEN")

echo -e "${BLUE}Meta 模块资源列表（缓存已清除，重新获取）:${NC}"
echo "$META_RESPONSE2" | jq ".[] | select(.id == $RESOURCE_ID) | {id, name, description}"
echo ""

# 步骤 7: 验证描述字段是否更新
OLD_DESC=$(echo "$META_RESPONSE" | jq -r ".[] | select(.id == $RESOURCE_ID) | .description")
NEW_DESC=$(echo "$META_RESPONSE2" | jq -r ".[] | select(.id == $RESOURCE_ID) | .description")

echo -e "${BLUE}验证结果:${NC}"
echo -e "  旧描述: ${YELLOW}$OLD_DESC${NC}"
echo -e "  新描述: ${GREEN}$NEW_DESC${NC}"

if [ "$NEW_DESC" == "已更新配置（测试缓存同步）" ]; then
    echo -e "${GREEN}✅ 缓存同步测试成功！Meta 模块已获取最新配置${NC}"
else
    echo -e "${RED}❌ 缓存同步测试失败！Meta 模块未获取最新配置${NC}"
fi
echo ""

# 步骤 8: 清理测试资源
echo -e "${GREEN}[步骤 8]${NC} 清理测试资源..."
curl -s -X DELETE "$SYSTEM_URL/api/resources/$RESOURCE_ID" \
  -H "Authorization: Bearer $TOKEN" > /dev/null

echo -e "${GREEN}✅ 测试资源已删除${NC}"
echo ""

# 等待删除事件传播
sleep 1

echo -e "${BLUE}========================================${NC}"
echo -e "${GREEN}✅ 测试完成${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

echo -e "${YELLOW}💡 提示:${NC}"
echo -e "  - 查看完整日志: tail -f logs/meta-backend.log"
echo -e "  - 查看 Redis 事件: redis-cli MONITOR"
echo -e "  - 清除所有缓存: curl -X POST http://localhost:8082/internal/cache/clear"
echo ""

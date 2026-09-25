#!/bin/bash
# ADDP 生产环境启动脚本
# 用途：按正确顺序启动所有 ADDP 服务（基础设施 + 后端 + 前端 + Console）

set -euo pipefail

# 颜色定义
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}ADDP 生产环境启动脚本${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""

connect_business_network() {
  local network="${BUSINESS_DOCKER_NETWORK:-business_business-network}"

  if ! docker network inspect "$network" > /dev/null 2>&1; then
    echo -e "${YELLOW}ℹ️  未检测到 Business 网络 ${network}，跳过业务引擎直连网络配置${NC}"
    return 0
  fi

  echo -e "${YELLOW}连接 ADDP 服务到 Business 网络 (${network})...${NC}"
  for container in $(docker compose -f docker-compose.yml ps -q 2>/dev/null) $(docker compose -f docker-compose.runtimes.yml ps -q 2>/dev/null); do
    if docker inspect "$container" --format '{{json .NetworkSettings.Networks}}' | grep -q "\"${network}\""; then
      continue
    fi

    docker network connect "$network" "$container" > /dev/null 2>&1 || true
  done
  echo -e "${GREEN}✓ Business 网络连接已处理${NC}"
}

# 检查 docker 是否运行
if ! docker info > /dev/null 2>&1; then
    echo -e "${RED}错误: Docker 未运行${NC}"
    exit 1
fi

# 初始化 .env（若不存在则从 .env.example 自动生成随机密钥）
bash scripts/prod/setup-env.sh

# 第一步：启动基础设施层
echo -e "${YELLOW}[1/4] 启动基础设施层...${NC}"
docker compose -f docker-compose.infra.yml up -d
bash scripts/prod/wait-infra.sh

# 第二步：System 先就绪，再启动应用全量服务。
echo -e "${YELLOW}[2/4] 等待 System Backend 就绪...${NC}"
docker compose -f docker-compose.yml up -d --wait --wait-timeout 120 system-backend

echo -e "${YELLOW}[3/4] 启动并等待平台服务就绪...${NC}"
docker compose -f docker-compose.yml up -d --wait --wait-timeout 180

echo -e "${YELLOW}[4/4] 启动并等待内置 Runtime 就绪...${NC}"
docker compose -f docker-compose.runtimes.yml up -d --wait --wait-timeout 180

connect_business_network

published_port="$(docker compose -f docker-compose.yml port nginx 80 | sed 's/.*://')"
public_origin="${ADDP_PUBLIC_ORIGIN:-$(sed -n 's/^ADDP_PUBLIC_ORIGIN=//p' .env | tail -n 1)}"
if [ -z "$public_origin" ]; then
  public_origin="http://localhost:${published_port}"
fi

echo -e "${GREEN}✓ ADDP 容器部署已启动${NC}"
echo -e "统一入口: ${public_origin}"
echo ""
docker compose -f docker-compose.yml ps --format "table {{.Service}}\t{{.State}}\t{{.Health}}\t{{.Ports}}"
docker compose -f docker-compose.runtimes.yml ps --format "table {{.Service}}\t{{.State}}\t{{.Health}}\t{{.Ports}}"

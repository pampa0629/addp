#!/bin/bash
# ADDP 生产环境启动脚本
# 用途：按正确顺序启动所有 ADDP 服务（基础设施 + 后端 + 前端 + Portal）

set -e

# 颜色定义
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}ADDP 生产环境启动脚本${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""

# 检查 docker 是否运行
if ! docker info > /dev/null 2>&1; then
    echo -e "${RED}错误: Docker 未运行${NC}"
    exit 1
fi

# 第一步：启动基础设施层
echo -e "${YELLOW}[1/4] 启动基础设施层 (PostgreSQL, Redis, MinIO, Meilisearch)...${NC}"
docker compose -f docker-compose.infra.yml up -d

echo -e "${YELLOW}等待基础设施就绪...${NC}"
bash scripts/prod/wait-infra.sh

# 第二步：启动 System Backend（其他服务依赖它）
echo -e "${YELLOW}[2/4] 启动 System Backend...${NC}"
docker compose -f docker-compose.yml up -d system-backend

echo -e "${YELLOW}等待 System Backend 就绪...${NC}"
timeout=60
counter=0
until curl -f http://localhost:8080/health > /dev/null 2>&1; do
  sleep 2
  counter=$((counter + 2))
  if [ $counter -ge $timeout ]; then
    echo -e "${RED}错误: System Backend 启动超时${NC}"
    docker compose -f docker-compose.yml logs system-backend | tail -30
    exit 1
  fi
done
echo -e "${GREEN}✓ System Backend 已就绪${NC}"

# 第三步：启动其他后端服务
echo -e "${YELLOW}[3/4] 启动所有业务后端服务...${NC}"
docker compose -f docker-compose.yml up -d \
  manager-backend \
  manager-worker \
  meta-backend \
  meta-worker \
  transfer-backend \
  transfer-worker \
  orchestrator-backend \
  develop-backend \
  service-backend \
  python-workflow-engine \
  spark-workflow-engine \
  jupyter-engine \
  gateway

# 第四步：等待所有后端服务就绪
echo -e "${YELLOW}[4/5] 等待所有后端服务就绪（最多 90 秒）...${NC}"
sleep 10

# 检查各后端服务健康
services=(
  "manager-backend:8081"
  "meta-backend:8082"
  "transfer-backend:8083"
  "orchestrator-backend:8084"
  "develop-backend:8085"
  "service-backend:8086"
  "jupyter-engine:8097"
  "spark-workflow-engine:8098"
  "python-workflow-engine:8099"
  "gateway:8000"
)

all_healthy=true
for service_port in "${services[@]}"; do
  service=$(echo $service_port | cut -d: -f1)
  port=$(echo $service_port | cut -d: -f2)

  # 等待最多 30 秒
  timeout=30
  counter=0
  healthy=false

  while [ $counter -lt $timeout ]; do
    if curl -f http://localhost:$port/health > /dev/null 2>&1; then
      echo -e "${GREEN}✓ $service${NC}"
      healthy=true
      break
    fi
    sleep 2
    counter=$((counter + 2))
  done

  if [ "$healthy" = false ]; then
    echo -e "${RED}✗ $service (超时)${NC}"
    all_healthy=false
  fi
done

# 第五步：启动前端服务和 Portal
echo -e "${YELLOW}[5/5] 启动前端服务和 Portal 统一门户...${NC}"
docker compose -f docker-compose.yml up -d \
  system-frontend \
  manager-frontend \
  meta-frontend \
  transfer-frontend \
  orchestrator-frontend \
  develop-frontend \
  portal \
  nginx

echo -e "${YELLOW}等待前端服务启动...${NC}"
sleep 5

# 检查 Portal 和 Nginx 是否启动
if docker ps | grep -q "portal"; then
  echo -e "${GREEN}✓ Portal 统一门户已启动${NC}"
else
  echo -e "${YELLOW}⚠️  Portal 启动失败或未找到镜像${NC}"
  all_healthy=false
fi

if docker ps | grep -q "nginx"; then
  echo -e "${GREEN}✓ Nginx 统一网关已启动${NC}"
else
  echo -e "${YELLOW}⚠️  Nginx 启动失败或未找到镜像${NC}"
  all_healthy=false
fi

echo ""
echo -e "${GREEN}========================================${NC}"
if [ "$all_healthy" = true ]; then
  echo -e "${GREEN}✅ 所有服务启动成功！${NC}"
else
  echo -e "${YELLOW}⚠️  部分服务启动失败或超时${NC}"
fi
echo -e "${GREEN}========================================${NC}"
echo ""

# 显示服务状态
echo -e "${YELLOW}服务运行状态:${NC}"
echo -e "${YELLOW}基础设施层:${NC}"
docker compose -f docker-compose.infra.yml ps --format "table {{.Service}}\t{{.State}}\t{{.Health}}\t{{.Ports}}"
echo ""
echo -e "${YELLOW}应用服务层:${NC}"
docker compose -f docker-compose.yml ps --format "table {{.Service}}\t{{.State}}\t{{.Health}}\t{{.Ports}}"

echo ""
echo -e "${YELLOW}API 端点:${NC}"
echo -e "  System Backend:         http://localhost:8080"
echo -e "  Manager Backend:        http://localhost:8081"
echo -e "  Meta Backend:           http://localhost:8082"
echo -e "  Transfer Backend:       http://localhost:8083"
echo -e "  Orchestrator Backend:   http://localhost:8084"
echo -e "  Develop Backend:        http://localhost:8085"
echo -e "  Service Backend:        http://localhost:8086"
echo -e "  Python Workflow Engine: http://localhost:8099"
echo -e "  Spark Spark 工作流引擎:    http://localhost:8098"
echo -e "  Gateway API:            http://localhost:8000"

echo ""
echo -e "${YELLOW}前端访问地址:${NC}"
echo -e "  ${GREEN}✨ Portal 统一门户 (推荐):  http://localhost:80${NC}"
echo -e ""
echo -e "  独立模块访问:"
echo -e "  - System Frontend:      http://localhost:8090"
echo -e "  - Manager Frontend:     http://localhost:8091"
echo -e "  - Meta Frontend:        http://localhost:8092"
echo -e "  - Transfer Frontend:    http://localhost:8093"
echo -e "  - Orchestrator Frontend: http://localhost:8094"
echo -e "  - Develop Frontend:     http://localhost:8095"

echo ""
echo -e "${YELLOW}常用命令:${NC}"
echo -e "  查看基础设施日志:   docker compose -f docker-compose.infra.yml logs -f [service-name]"
echo -e "  查看应用日志:       docker compose -f docker-compose.yml logs -f [service-name]"
echo -e "  停止基础设施:       docker compose -f docker-compose.infra.yml down"
echo -e "  停止应用服务:       docker compose -f docker-compose.yml down"
echo -e "  重启服务:           docker compose -f docker-compose.yml restart [service-name]"
echo -e "  健康检查:           bash scripts/prod/health-check.sh"

echo ""

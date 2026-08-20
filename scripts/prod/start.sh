#!/bin/bash
# ADDP 生产环境启动脚本
# 用途：按正确顺序启动所有 ADDP 服务（基础设施 + 后端 + 前端 + Console）

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

connect_business_network() {
  local network="${BUSINESS_DOCKER_NETWORK:-business_business-network}"

  if ! docker network inspect "$network" > /dev/null 2>&1; then
    echo -e "${YELLOW}ℹ️  未检测到 Business 网络 ${network}，跳过业务引擎直连网络配置${NC}"
    return 0
  fi

  echo -e "${YELLOW}连接 ADDP 服务到 Business 网络 (${network})...${NC}"
  for container in $(docker compose -f docker-compose.yml ps -q 2>/dev/null); do
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
until curl -f http://localhost:8180/health > /dev/null 2>&1; do
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
  meta-backend \
  meta-worker \
  transfer-backend \
  transfer-bounded-worker \
  transfer-continuous-worker \
  orchestrator-backend \
  develop-backend \
  service-backend \
  copilot-backend \
  inference-backend \
  monitor-backend \
  standard-backend \
  graph-backend \
  agent-backend \
  model-backend \
  quality-backend \
  quality-worker \
  asset-backend \
  portal-backend \
  geopython-workflow-engine \
  model3d-workflow-engine \
  pointcloud-workflow-engine \
  supermap-workflow-engine \
  spark-workflow-engine \
  jupyter-engine \
  duckdb-engine \
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
  "develop-backend:8185"
  "service-backend:8086"
  "copilot-backend:8087"
  "monitor-backend:8100"
  "standard-backend:8110"
  "graph-backend:8186"
  "agent-backend:8190"
  "inference-backend:8191"
  "model-backend:8181"
  "quality-backend:8182"
  "asset-backend:8183"
  "portal-backend:8184"
  "jupyter-engine:8097"
  "duckdb-engine:8104"
  "spark-workflow-engine:8098"
  "geopython-workflow-engine:8099"
  "model3d-workflow-engine:8101"
  "pointcloud-workflow-engine:8102"
  "supermap-workflow-engine:8103"
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

# 第五步：启动前端服务和 Console
echo -e "${YELLOW}[5/5] 启动前端服务和 Console 控制台...${NC}"
docker compose -f docker-compose.yml up -d \
  system-frontend \
  manager-frontend \
  meta-frontend \
  transfer-frontend \
  orchestrator-frontend \
  develop-frontend \
  service-frontend \
  monitor-frontend \
  standard-frontend \
  graph-frontend \
  agent-frontend \
  inference-frontend \
  model-frontend \
  quality-frontend \
  asset-frontend \
  portal-frontend \
  console \
  nginx

echo -e "${YELLOW}等待前端服务启动...${NC}"
sleep 5

# 检查 Console 和 Nginx 是否启动
if docker ps | grep -q "console"; then
  echo -e "${GREEN}✓ Console 工作台已启动${NC}"
else
  echo -e "${YELLOW}⚠️  Console 启动失败或未找到镜像${NC}"
  all_healthy=false
fi

if docker ps | grep -q "nginx"; then
  echo -e "${GREEN}✓ Nginx 统一网关已启动${NC}"
else
  echo -e "${YELLOW}⚠️  Nginx 启动失败或未找到镜像${NC}"
  all_healthy=false
fi

connect_business_network

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
echo -e "  System Backend:         http://localhost:8180"
echo -e "  Manager Backend:        http://localhost:8081"
echo -e "  Meta Backend:           http://localhost:8082"
echo -e "  Transfer Backend:       http://localhost:8083"
echo -e "  Orchestrator Backend:   http://localhost:8084"
echo -e "  Develop Backend:        http://localhost:8185"
echo -e "  Service Backend:        http://localhost:8086"
echo -e "  Copilot Backend:        http://localhost:8087"
echo -e "  Monitor Backend:        http://localhost:8100"
echo -e "  Standard Backend:       http://localhost:8110"
echo -e "  Model Backend:          http://localhost:8181"
echo -e "  Quality Backend:        http://localhost:8182"
echo -e "  Asset Backend:          http://localhost:8183"
echo -e "  Portal Backend:         http://localhost:8184"
echo -e "  GeoPython Workflow: http://localhost:8099"
echo -e "  Model3D Workflow Engine: http://localhost:8101"
echo -e "  PointCloud Workflow Engine: http://localhost:8102"
echo -e "  Spark Spark 工作流引擎:    http://localhost:8098"
echo -e "  Gateway API:            http://localhost:8000"

echo ""
echo -e "${YELLOW}前端访问地址:${NC}"
echo -e "  ${GREEN}✨ Console 控制台 (推荐):  http://localhost:80${NC}"
echo -e ""
echo -e "  独立模块访问:"
echo -e "  - System Frontend:      http://localhost:8090"
echo -e "  - Manager Frontend:     http://localhost:8091"
echo -e "  - Meta Frontend:        http://localhost:8092"
echo -e "  - Transfer Frontend:    http://localhost:8093"
echo -e "  - Orchestrator Frontend: http://localhost:8094"
echo -e "  - Develop Frontend:     http://localhost:8095"
echo -e "  - Service Frontend:     http://localhost:8096"
echo -e "  - Standard Frontend:    http://localhost:8112"
echo -e "  - Model Frontend:       http://localhost:8111"
echo -e "  - Quality Frontend:     http://localhost:8113"
echo -e "  - Asset Frontend:       http://localhost:8114"
echo -e "  - Portal Frontend:      http://localhost:8115"
echo -e "  - Monitor Frontend:     http://localhost:8116"
echo -e "  - Agent Frontend:       http://localhost:8117"
echo -e "  - Graph Frontend:       http://localhost:8118"
echo -e "  - Inference Frontend:   http://localhost:8119"

echo ""
echo -e "${YELLOW}常用命令:${NC}"
echo -e "  查看基础设施日志:   docker compose -f docker-compose.infra.yml logs -f [service-name]"
echo -e "  查看应用日志:       docker compose -f docker-compose.yml logs -f [service-name]"
echo -e "  停止基础设施:       docker compose -f docker-compose.infra.yml down"
echo -e "  停止应用服务:       docker compose -f docker-compose.yml down"
echo -e "  重启服务:           docker compose -f docker-compose.yml restart [service-name]"
echo -e "  健康检查:           bash scripts/prod/health-check.sh"

echo ""

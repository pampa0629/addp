#!/bin/bash

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}=== 基础设施层健康检查 ===${NC}"

# PostgreSQL
if docker compose -f docker-compose.yml exec -T postgres pg_isready -U addp > /dev/null 2>&1; then
  echo -e "${GREEN}✓ PostgreSQL${NC}"
else
  echo -e "${RED}✗ PostgreSQL${NC}"
fi

# Redis
if docker compose -f docker-compose.yml exec -T redis redis-cli --raw incr ping > /dev/null 2>&1; then
  echo -e "${GREEN}✓ Redis${NC}"
else
  echo -e "${RED}✗ Redis${NC}"
fi

# MinIO
if curl -f http://localhost:19000/minio/health/live > /dev/null 2>&1; then
  echo -e "${GREEN}✓ MinIO${NC}"
else
  echo -e "${RED}✗ MinIO${NC}"
fi

# Meilisearch
if curl -f http://localhost:17700/health > /dev/null 2>&1; then
  echo -e "${GREEN}✓ Meilisearch${NC}"
else
  echo -e "${RED}✗ Meilisearch${NC}"
fi

echo ""
echo -e "${YELLOW}=== 应用服务层健康检查 ===${NC}"

# 检查各个后端服务
for service in system:8180 manager:8081 meta:8082 transfer:8083 orchestrator:8084 develop:8185 service:8086 copilot:8087 inference:8191; do
  name=$(echo $service | cut -d: -f1)
  port=$(echo $service | cut -d: -f2)

  if curl -f http://localhost:$port/health > /dev/null 2>&1; then
    echo -e "${GREEN}✓ ${name}-backend${NC}"
  else
    echo -e "${RED}✗ ${name}-backend${NC}"
  fi
done

# Jupyter Engine
if curl -f http://localhost:8097/health > /dev/null 2>&1; then
  echo -e "${GREEN}✓ jupyter-engine${NC}"
else
  echo -e "${RED}✗ jupyter-engine${NC}"
fi

# DuckDB Federated Query Runtime
if docker compose -f docker-compose.yml exec -T duckdb-engine curl -fsS http://localhost:8104/health > /dev/null 2>&1; then
  echo -e "${GREEN}✓ duckdb-engine${NC}"
else
  echo -e "${RED}✗ duckdb-engine${NC}"
fi

# Spark Spark 工作流引擎
if curl -f http://localhost:8098/health > /dev/null 2>&1; then
  echo -e "${GREEN}✓ spark-workflow-engine${NC}"
else
  echo -e "${RED}✗ spark-workflow-engine${NC}"
fi

# GeoPython Workflow Engine
if curl -f http://localhost:8099/health > /dev/null 2>&1; then
  echo -e "${GREEN}✓ python-workflow-engine${NC}"
else
  echo -e "${RED}✗ python-workflow-engine${NC}"
fi

# Model3D Workflow Engine
if curl -f http://localhost:8101/health > /dev/null 2>&1; then
  echo -e "${GREEN}✓ model3d-workflow-engine${NC}"
else
  echo -e "${RED}✗ model3d-workflow-engine${NC}"
fi

# PointCloud Workflow Engine
if curl -f http://localhost:8102/health > /dev/null 2>&1; then
  echo -e "${GREEN}✓ pointcloud-workflow-engine${NC}"
else
  echo -e "${RED}✗ pointcloud-workflow-engine${NC}"
fi

# SuperMap Workflow Engine
if curl -f http://localhost:8103/health > /dev/null 2>&1; then
  echo -e "${GREEN}✓ supermap-workflow-engine${NC}"
else
  echo -e "${RED}✗ supermap-workflow-engine${NC}"
fi

# Gateway
if curl -f http://localhost:8000/health > /dev/null 2>&1; then
  echo -e "${GREEN}✓ gateway${NC}"
else
  echo -e "${RED}✗ gateway${NC}"
fi

# Nginx
if curl -f http://localhost:8000/ > /dev/null 2>&1; then
  echo -e "${GREEN}✓ nginx${NC}"
else
  echo -e "${RED}✗ nginx${NC}"
fi

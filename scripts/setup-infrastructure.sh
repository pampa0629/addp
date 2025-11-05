#!/bin/bash
# ADDP Infrastructure Setup Script (macOS Compatible)
# 用于部署基础设施镜像到本地registry

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

REGISTRY="${REGISTRY:-localhost:5001}"
ARCH="$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')"

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}ADDP Infrastructure Setup${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""
echo -e "Registry: ${GREEN}${REGISTRY}${NC}"
echo -e "Architecture: ${GREEN}${ARCH}${NC}"
echo ""

# 检查registry
echo -e "${YELLOW}Checking local registry...${NC}"
if ! curl -s http://localhost:5001/v2/_catalog > /dev/null 2>&1; then
    echo -e "${RED}✗ Local registry not running${NC}"
    echo -e "${YELLOW}Starting registry...${NC}"
    docker start addp-registry 2>/dev/null || docker run -d -p 5001:5000 --name addp-registry registry:2
    sleep 2
fi
echo -e "${GREEN}✓ Registry is running${NC}"
echo ""

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}Step 1: Pulling Infrastructure Images${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# PostgreSQL with PostGIS
echo -e "${YELLOW}[1/5] Pulling postgis/postgis:15-3.4-alpine...${NC}"
docker pull --platform linux/${ARCH} postgis/postgis:15-3.4-alpine 2>/dev/null || docker pull postgis/postgis:15-3.4-alpine
echo -e "${GREEN}✓ Pulled postgis/postgis${NC}"

# Redis
echo -e "${YELLOW}[2/5] Pulling redis:7-alpine...${NC}"
docker pull --platform linux/${ARCH} redis:7-alpine 2>/dev/null || docker pull redis:7-alpine
echo -e "${GREEN}✓ Pulled redis${NC}"

# MinIO
echo -e "${YELLOW}[3/5] Pulling minio/minio:latest...${NC}"
docker pull --platform linux/${ARCH} minio/minio:latest 2>/dev/null || docker pull minio/minio:latest
echo -e "${GREEN}✓ Pulled minio${NC}"

# Elasticsearch
echo -e "${YELLOW}[4/5] Pulling elasticsearch:8.11.0...${NC}"
docker pull --platform linux/${ARCH} elasticsearch:8.11.0 2>/dev/null || \
docker pull --platform linux/${ARCH} docker.elastic.co/elasticsearch/elasticsearch:8.11.0 2>/dev/null || \
docker pull elasticsearch:8.11.0 || \
echo -e "${YELLOW}⚠ Elasticsearch pull failed, will try alternative...${NC}"
echo -e "${GREEN}✓ Pulled elasticsearch${NC}"

# Alpine
echo -e "${YELLOW}[5/5] Pulling alpine:latest...${NC}"
docker pull --platform linux/${ARCH} alpine:latest 2>/dev/null || docker pull alpine:latest
echo -e "${GREEN}✓ Pulled alpine${NC}"

echo ""
echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}Step 2: Tagging and Pushing to Registry${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# Tag and push
docker tag postgis/postgis:15-3.4-alpine ${REGISTRY}/addp-infra-postgis:15-3.4-alpine
docker push ${REGISTRY}/addp-infra-postgis:15-3.4-alpine
echo -e "${GREEN}✓ Pushed postgis/postgis${NC}"

docker tag redis:7-alpine ${REGISTRY}/addp-infra-redis:7-alpine
docker push ${REGISTRY}/addp-infra-redis:7-alpine
echo -e "${GREEN}✓ Pushed redis${NC}"

docker tag minio/minio:latest ${REGISTRY}/addp-infra-minio:latest
docker push ${REGISTRY}/addp-infra-minio:latest
echo -e "${GREEN}✓ Pushed minio${NC}"

# Elasticsearch - try multiple tags
if docker images | grep -q "elasticsearch.*8.11.0"; then
    ES_IMAGE=$(docker images | grep "elasticsearch.*8.11.0" | awk '{print $1":"$2}' | head -1)
    docker tag ${ES_IMAGE} ${REGISTRY}/addp-infra-elasticsearch:8.11.0
    docker push ${REGISTRY}/addp-infra-elasticsearch:8.11.0
    echo -e "${GREEN}✓ Pushed elasticsearch${NC}"
else
    echo -e "${YELLOW}⚠ Elasticsearch not found, skipping...${NC}"
fi

docker tag alpine:latest ${REGISTRY}/alpine:latest
docker push ${REGISTRY}/alpine:latest
echo -e "${GREEN}✓ Pushed alpine${NC}"

echo ""
echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}Step 3: Verification${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

echo -e "${YELLOW}Images in registry:${NC}"
curl -s http://localhost:5001/v2/_catalog 2>/dev/null | grep -o '"repositories":\[.*\]' | grep -o '\[.*\]' | tr -d '[]"' | tr ',' '\n' | grep addp-infra | while read repo; do
    echo -e "${GREEN}✓ ${repo}${NC}"
done

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}✓ Infrastructure Setup Complete!${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo -e "Next steps:"
echo -e "  1. Compile: ${YELLOW}./scripts/deploy/0-compile-binaries.sh --arch ${ARCH}${NC}"
echo -e "  2. Build: ${YELLOW}./scripts/deploy/1-build-images.sh${NC}"
echo -e "  3. Deploy: ${YELLOW}cd deploy-package && docker compose up -d${NC}"
echo ""

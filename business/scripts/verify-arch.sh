#!/usr/bin/env bash

set -euo pipefail

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}  Architecture Verification${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# Detect system architecture
SYSTEM_ARCH=$(uname -m)
echo -e "${YELLOW}🖥️  System Architecture: ${SYSTEM_ARCH}${NC}"

# Map to Docker architecture
case "${SYSTEM_ARCH}" in
    x86_64)
        EXPECTED_ARCH="amd64"
        ;;
    aarch64|arm64)
        EXPECTED_ARCH="arm64"
        ;;
    armv7l)
        EXPECTED_ARCH="arm"
        ;;
    *)
        EXPECTED_ARCH="unknown"
        ;;
esac

echo -e "${YELLOW}🎯 Expected Docker Arch: ${EXPECTED_ARCH}${NC}"
echo ""

# Check running containers
echo -e "${YELLOW}📦 Checking running containers...${NC}"
echo ""

# Function to check container architecture
check_container() {
    local container_name="$1"
    local service_name="$2"

    if docker ps --filter "name=${container_name}" --format "{{.Names}}" | grep -q "${container_name}"; then
        local image=$(docker ps --filter "name=${container_name}" --format "{{.Image}}")
        local arch=$(docker inspect "${container_name}" 2>/dev/null | grep -m1 '"Architecture"' | awk -F'"' '{print $4}')

        echo -e "  ${service_name}:"
        echo -e "    Container: ${container_name}"
        echo -e "    Image:     ${image}"
        echo -e "    Arch:      ${arch}"

        if [ "${arch}" = "${EXPECTED_ARCH}" ]; then
            echo -e "    Status:    ${GREEN}✓ MATCH${NC}"
        else
            echo -e "    Status:    ${RED}✗ MISMATCH (expected ${EXPECTED_ARCH})${NC}"
        fi
        echo ""
    else
        echo -e "  ${service_name}: ${RED}✗ Not running${NC}"
        echo ""
    fi
}

# Check business infrastructure containers
check_container "business-postgres" "PostgreSQL"
check_container "business-minio" "MinIO"

# Check local images
echo -e "${YELLOW}💿 Checking local images...${NC}"
echo ""

check_image() {
    local image_name="$1"

    if docker image inspect "${image_name}" &>/dev/null; then
        local arch=$(docker image inspect "${image_name}" 2>/dev/null | grep -m1 '"Architecture"' | awk -F'"' '{print $4}')
        echo -e "  ${image_name}:"
        echo -e "    Arch: ${arch}"

        if [ "${arch}" = "${EXPECTED_ARCH}" ]; then
            echo -e "    ${GREEN}✓ MATCH${NC}"
        else
            echo -e "    ${RED}✗ MISMATCH (expected ${EXPECTED_ARCH})${NC}"
        fi
        echo ""
    else
        echo -e "  ${image_name}: ${YELLOW}Not found locally${NC}"
        echo ""
    fi
}

check_image "postgis/postgis:15-3.4"
check_image "minio/minio:latest"

echo -e "${BLUE}========================================${NC}"
echo ""

# Summary
if docker ps --filter "name=business-postgres" --format "{{.Names}}" | grep -q "business-postgres"; then
    POSTGRES_ARCH=$(docker inspect business-postgres 2>/dev/null | grep -m1 '"Architecture"' | awk -F'"' '{print $4}')
    if [ "${POSTGRES_ARCH}" = "${EXPECTED_ARCH}" ]; then
        echo -e "${GREEN}✅ All containers are running on correct architecture (${EXPECTED_ARCH})${NC}"
    else
        echo -e "${RED}⚠️  Architecture mismatch detected!${NC}"
        echo -e "${YELLOW}   Run: cd business && ./scripts/restart.sh${NC}"
    fi
else
    echo -e "${YELLOW}⚠️  Containers not running${NC}"
    echo -e "${YELLOW}   Run: cd business && ./scripts/start.sh${NC}"
fi

echo ""

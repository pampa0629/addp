#!/bin/bash
# =============================================================================
# ADDP Production Startup Script
# =============================================================================
# Description: Start ADDP production services with automatic configuration
# Usage: ./scripts/start-prod.sh
# =============================================================================

set -e

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$PROJECT_ROOT"

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}ADDP Production Startup${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# Check if .env.prod exists
if [ ! -f ".env.prod" ]; then
    echo -e "${YELLOW}⚠️  .env.prod not found, creating from template...${NC}"

    if [ ! -f ".env.prod.example" ]; then
        echo -e "${RED}Error: .env.prod.example not found${NC}"
        exit 1
    fi

    # Copy template
    cp .env.prod.example .env.prod

    echo -e "${YELLOW}Generating secure keys...${NC}"

    # Generate secure keys
    JWT_SECRET=$(openssl rand -base64 32)
    ENCRYPTION_KEY=$(openssl rand -base64 32)
    INTERNAL_API_KEY=$(openssl rand -base64 32)
    POSTGRES_PASSWORD=$(openssl rand -base64 16 | tr -d '=/+' | cut -c1-16)
    REDIS_PASSWORD=$(openssl rand -base64 16 | tr -d '=/+' | cut -c1-16)
    MINIO_PASSWORD=$(openssl rand -base64 16 | tr -d '=/+' | cut -c1-16)

    # Update .env.prod
    if [[ "$OSTYPE" == "darwin"* ]]; then
        # macOS
        sed -i '' "s|^JWT_SECRET=.*|JWT_SECRET=${JWT_SECRET}|" .env.prod
        sed -i '' "s|^ENCRYPTION_KEY=.*|ENCRYPTION_KEY=${ENCRYPTION_KEY}|" .env.prod
        sed -i '' "s|^INTERNAL_API_KEY=.*|INTERNAL_API_KEY=${INTERNAL_API_KEY}|" .env.prod
        sed -i '' "s|^POSTGRES_PASSWORD=.*|POSTGRES_PASSWORD=${POSTGRES_PASSWORD}|" .env.prod
        sed -i '' "s|^REDIS_PASSWORD=.*|REDIS_PASSWORD=${REDIS_PASSWORD}|" .env.prod
        sed -i '' "s|^MINIO_ROOT_PASSWORD=.*|MINIO_ROOT_PASSWORD=${MINIO_PASSWORD}|" .env.prod
        sed -i '' "s|^REGISTRY=.*|REGISTRY=localhost:5001|" .env.prod
    else
        # Linux
        sed -i "s|^JWT_SECRET=.*|JWT_SECRET=${JWT_SECRET}|" .env.prod
        sed -i "s|^ENCRYPTION_KEY=.*|ENCRYPTION_KEY=${ENCRYPTION_KEY}|" .env.prod
        sed -i "s|^INTERNAL_API_KEY=.*|INTERNAL_API_KEY=${INTERNAL_API_KEY}|" .env.prod
        sed -i "s|^POSTGRES_PASSWORD=.*|POSTGRES_PASSWORD=${POSTGRES_PASSWORD}|" .env.prod
        sed -i "s|^REDIS_PASSWORD=.*|REDIS_PASSWORD=${REDIS_PASSWORD}|" .env.prod
        sed -i "s|^MINIO_ROOT_PASSWORD=.*|MINIO_ROOT_PASSWORD=${MINIO_PASSWORD}|" .env.prod
        sed -i "s|^REGISTRY=.*|REGISTRY=localhost:5001|" .env.prod
    fi

    echo -e "${GREEN}✓ Created .env.prod with secure keys${NC}"
    echo ""
    echo -e "${BLUE}Generated credentials:${NC}"
    echo "  PostgreSQL Password: ${POSTGRES_PASSWORD}"
    echo "  Redis Password: ${REDIS_PASSWORD}"
    echo "  MinIO Password: ${MINIO_PASSWORD}"
    echo ""
    echo -e "${YELLOW}⚠️  Save these credentials securely!${NC}"
    echo ""
fi

# Check if registry is running
echo -e "${YELLOW}Checking Docker registry...${NC}"
if ! curl -sf http://localhost:5001/v2/ > /dev/null 2>&1; then
    echo -e "${RED}Error: Docker registry not accessible at localhost:5001${NC}"
    echo ""
    echo "Please start the registry:"
    echo "  docker run -d -p 5001:5000 --restart=always --name registry registry:2"
    echo ""
    echo "Or check if registry is running:"
    echo "  docker ps | grep registry"
    exit 1
fi
echo -e "${GREEN}✓ Registry is accessible${NC}"

# Check for port conflicts
echo -e "${YELLOW}Checking for port conflicts...${NC}"
PORTS_IN_USE=()

for port in 5432 6379 8080 8090 9000 9001 9200; do
    if lsof -Pi :$port -sTCP:LISTEN -t >/dev/null 2>&1; then
        PORTS_IN_USE+=($port)
    fi
done

if [ ${#PORTS_IN_USE[@]} -gt 0 ]; then
    echo -e "${YELLOW}⚠️  Following ports are in use: ${PORTS_IN_USE[*]}${NC}"
    echo ""
    read -p "Stop conflicting services? (y/N): " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        for port in "${PORTS_IN_USE[@]}"; do
            echo -e "${YELLOW}Stopping processes on port $port...${NC}"
            lsof -ti:$port | xargs kill -9 2>/dev/null || true
        done
        echo -e "${GREEN}✓ Ports cleared${NC}"
    else
        echo -e "${RED}Cannot start services with port conflicts${NC}"
        exit 1
    fi
fi

# Start services
echo ""
echo -e "${YELLOW}Starting ADDP services...${NC}"
docker compose -f docker-compose.prod.yml --env-file .env.prod up -d --remove-orphans

# Wait for services to start
echo ""
echo -e "${YELLOW}Waiting for services to start...${NC}"
sleep 10

# Check service status
echo ""
echo -e "${BLUE}Service Status:${NC}"
docker compose -f docker-compose.prod.yml --env-file .env.prod ps

# Health checks
echo ""
echo -e "${BLUE}Performing health checks...${NC}"

check_service() {
    local name=$1
    local url=$2
    local max_attempts=30
    local attempt=0

    while [ $attempt -lt $max_attempts ]; do
        if curl -sf "$url" > /dev/null 2>&1; then
            echo -e "${GREEN}✓ ${name} is healthy${NC}"
            return 0
        fi
        attempt=$((attempt + 1))
        sleep 1
    done

    echo -e "${YELLOW}⚠ ${name} health check timeout${NC}"
    return 1
}

check_service "System Backend" "http://localhost:8080/health"
check_service "System Frontend" "http://localhost:8090"

# Display access information
echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}ADDP Started Successfully!${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo -e "${BLUE}Access URLs:${NC}"
echo "  System Frontend: http://localhost:8090"
echo "  System Backend:  http://localhost:8080"
echo "  MinIO Console:   http://localhost:9001"
echo ""
echo -e "${BLUE}Super Admin Login:${NC}"
echo "  Username: SuperAdmin"
echo "  Password: 20251001#SuperAdmin"
echo ""
echo -e "${YELLOW}⚠️  IMPORTANT: Change the default password after first login!${NC}"
echo ""
echo -e "${BLUE}Useful Commands:${NC}"
echo "  View logs:    docker compose -f docker-compose.prod.yml logs -f"
echo "  Stop:         docker compose -f docker-compose.prod.yml down"
echo "  Restart:      docker compose -f docker-compose.prod.yml restart"
echo ""

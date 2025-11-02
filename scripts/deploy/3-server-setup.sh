#!/bin/bash
# =============================================================================
# ADDP Server Setup Script
# =============================================================================
# Description: Initialize server environment and deploy ADDP services
# Usage: ./3-server-setup.sh [OPTIONS]
# Options:
#   --registry REGISTRY_URL   Registry URL (default: detect from docker-compose)
#   --skip-docker-install     Skip Docker installation check
#   --skip-image-pull         Skip pulling images from registry
# =============================================================================

set -e

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Default values
REGISTRY=""
SKIP_DOCKER_INSTALL=false
SKIP_IMAGE_PULL=false
FORCE_OVERWRITE=false
SKIP_DOCKER_RESTART=false
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
COMPOSE_FILE="$DEPLOY_DIR/docker-compose.prod.yml"
ENV_FILE="$DEPLOY_DIR/.env.prod"
ENV_EXAMPLE="$DEPLOY_DIR/.env.prod.example"

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --registry)
            REGISTRY="$2"
            shift 2
            ;;
        --skip-docker-install)
            SKIP_DOCKER_INSTALL=true
            shift
            ;;
        --skip-image-pull)
            SKIP_IMAGE_PULL=true
            shift
            ;;
        --force)
            FORCE_OVERWRITE=true
            shift
            ;;
        --skip-docker-restart)
            SKIP_DOCKER_RESTART=true
            shift
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            # Pull failure is not fatal - continue
            ;;
    esac
done

# Try to load registry from config file if not specified via argument
if [ -z "$REGISTRY" ] && [ -f "$DEPLOY_DIR/.registry_config" ]; then
    echo -e "${YELLOW}Loading registry configuration from .registry_config...${NC}"
    source "$DEPLOY_DIR/.registry_config"
    if [ -n "$REGISTRY" ]; then
        echo -e "${GREEN}✓ Registry loaded: ${REGISTRY}${NC}"
    fi
fi

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}ADDP Server Setup${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""
echo -e "Deploy Directory: ${GREEN}${DEPLOY_DIR}${NC}"
echo -e "Compose File: ${GREEN}${COMPOSE_FILE}${NC}"
echo ""

# =============================================================================
# Step 1: Check Prerequisites
# =============================================================================

echo -e "${YELLOW}Step 1: Checking prerequisites...${NC}"

# Check if we're in the right directory
if [ ! -f "$COMPOSE_FILE" ]; then
    echo -e "${RED}Error: docker-compose.prod.yml not found${NC}"
    echo "Please run this script from the deployment directory"
    # Pull failure is not fatal - continue
fi

# Detect OS
detect_os() {
    if [ -f /etc/os-release ]; then
        . /etc/os-release
        OS=$ID
        VER=$VERSION_ID
    elif type lsb_release >/dev/null 2>&1; then
        OS=$(lsb_release -si | tr '[:upper:]' '[:lower:]')
        VER=$(lsb_release -sr)
    elif [ -f /etc/redhat-release ]; then
        OS="centos"
    else
        OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    fi
    echo "$OS"
}

# Function to wait for Docker to be ready
wait_for_docker() {
    local timeout=${1:-60}
    local silent=${2:-false}

    # First, try a simple docker version check (lighter than docker info)
    if docker version &> /dev/null; then
        return 0
    fi

    if [ "$silent" = false ]; then
        echo -e "${YELLOW}Waiting for Docker to be ready...${NC}"
    fi

    local count=0
    while [ $count -lt $timeout ]; do
        # Use docker version instead of docker info for faster response
        if docker version &> /dev/null; then
            if [ "$silent" = false ]; then
                echo ""
                echo -e "${GREEN}✓ Docker is ready${NC}"
            fi
            return 0
        fi
        echo -n "."
        sleep 2
        count=$((count + 2))
    done
    echo ""

    # Provide diagnostic information
    echo -e "${RED}Error: Docker is not responding after ${timeout} seconds${NC}"
    echo ""
    echo -e "${YELLOW}Diagnostic information:${NC}"
    echo -n "Docker command available: "
    if command -v docker &> /dev/null; then
        echo "Yes"
        echo "Docker version check output:"
        docker version 2>&1 | head -5 || echo "Failed to get docker version"
        echo ""
        echo "Docker socket check:"
        ls -la ~/.docker/run/docker.sock 2>/dev/null || echo "Socket not found at ~/.docker/run/docker.sock"
        ls -la /var/run/docker.sock 2>/dev/null || echo "Socket not found at /var/run/docker.sock"
    else
        echo "No"
    fi
    echo ""
    echo "Please ensure Docker Desktop is running and try again"
    return 1
}

OS=$(detect_os)
echo -e "Detected OS: ${GREEN}${OS}${NC}"

# =============================================================================
# Step 2: Install Docker and Docker Compose
# =============================================================================

if [ "$SKIP_DOCKER_INSTALL" = false ]; then
    echo ""
    echo -e "${YELLOW}Step 2: Installing Docker and Docker Compose...${NC}"

    # Check if Docker is installed
    if ! command -v docker &> /dev/null; then
        echo -e "${YELLOW}Docker not found, installing...${NC}"

        case "$OS" in
            ubuntu|debian)
                sudo apt-get update
                sudo apt-get install -y apt-transport-https ca-certificates curl software-properties-common
                curl -fsSL https://get.docker.com -o get-docker.sh
                sudo sh get-docker.sh
                sudo usermod -aG docker $USER
                rm get-docker.sh
                ;;
            centos|rhel)
                sudo yum install -y yum-utils
                sudo yum-config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo
                sudo yum install -y docker-ce docker-ce-cli containerd.io
                sudo systemctl start docker
                sudo systemctl enable docker
                sudo usermod -aG docker $USER
                ;;
            darwin)
                echo -e "${RED}Please install Docker Desktop for Mac manually${NC}"
                echo "https://docs.docker.com/desktop/install/mac-install/"
                # Pull failure is not fatal - continue
                ;;
            *)
                echo -e "${RED}Unsupported OS: $OS${NC}"
                echo "Please install Docker manually"
                # Pull failure is not fatal - continue
                ;;
        esac

        echo -e "${GREEN}✓ Docker installed${NC}"
    else
        echo -e "${GREEN}✓ Docker already installed${NC}"
        docker --version
    fi

    # Check if Docker Compose is installed
    if ! docker compose version &> /dev/null; then
        echo -e "${YELLOW}Docker Compose plugin not found, installing...${NC}"

        case "$OS" in
            ubuntu|debian|centos|rhel)
                # Docker Compose plugin should come with Docker
                # If not, install it manually
                COMPOSE_VERSION=$(curl -s https://api.github.com/repos/docker/compose/releases/latest | grep 'tag_name' | cut -d\" -f4)
                sudo curl -L "https://github.com/docker/compose/releases/download/${COMPOSE_VERSION}/docker-compose-$(uname -s)-$(uname -m)" -o /usr/local/bin/docker-compose
                sudo chmod +x /usr/local/bin/docker-compose
                ;;
            darwin)
                echo -e "${RED}Please install Docker Desktop for Mac${NC}"
                # Pull failure is not fatal - continue
                ;;
        esac

        echo -e "${GREEN}✓ Docker Compose installed${NC}"
    else
        echo -e "${GREEN}✓ Docker Compose already installed${NC}"
        docker compose version
    fi

    # Start Docker service
    if [ "$OS" != "darwin" ]; then
        sudo systemctl start docker
        sudo systemctl enable docker
    fi

else
    echo ""
    echo -e "${YELLOW}Step 2: Skipping Docker installation (--skip-docker-install)${NC}"
fi

# =============================================================================
# Step 3: Configure Docker Registry Access
# =============================================================================

echo ""
echo -e "${YELLOW}Step 3: Configuring Docker registry access...${NC}"

# Detect registry from docker-compose.yml if not specified
if [ -z "$REGISTRY" ]; then
    # Try to detect from ADDP service images (not infrastructure images like minio/postgres)
    REGISTRY=$(awk -F'[:/]' '/image:.*addp-.*:/{print $2; exit}' "$COMPOSE_FILE" | tr -d ' ')

    if [ -z "$REGISTRY" ] || [[ "$REGISTRY" == *"\$"* ]] || [[ "$REGISTRY" == *"{"* ]]; then
        # If detection failed or got a variable, use default
        REGISTRY="localhost:5001"
    fi
fi

echo -e "Registry: ${GREEN}${REGISTRY}${NC}"

# Configure insecure registry if using HTTP
if [[ ! "$REGISTRY" =~ ^https:// ]] && [[ ! "$REGISTRY" =~ ^localhost ]] && [[ ! "$REGISTRY" =~ ^127\.0\.0\.1 ]]; then
    echo -e "${YELLOW}Configuring insecure registry for HTTP access...${NC}"

    DAEMON_JSON="/etc/docker/daemon.json"

    if [ "$OS" = "darwin" ]; then
        echo -e "${YELLOW}Detected macOS - configuring Docker Desktop insecure registry...${NC}"

        # Path to Docker Desktop daemon.json on macOS
        DOCKER_CONFIG_DIR="$HOME/.docker"
        DAEMON_JSON="$DOCKER_CONFIG_DIR/daemon.json"

        # Create .docker directory if it doesn't exist
        mkdir -p "$DOCKER_CONFIG_DIR"

        # Create or update daemon.json
        if [ -f "$DAEMON_JSON" ]; then
            # Backup existing config
            cp "$DAEMON_JSON" "${DAEMON_JSON}.bak.$(date +%Y%m%d_%H%M%S)"

            # Add insecure registry using python (available on macOS)
            REGISTRY_STATUS=$(python3 << EOF
import json
import os

daemon_json = "$DAEMON_JSON"
with open(daemon_json, 'r') as f:
    config = json.load(f)

if 'insecure-registries' not in config:
    config['insecure-registries'] = []

if '$REGISTRY' not in config['insecure-registries']:
    config['insecure-registries'].append('$REGISTRY')
    with open(daemon_json, 'w') as f:
        json.dump(config, f, indent=2)
    print("ADDED")
else:
    print("EXISTS")
EOF
)
        else
            # Create new daemon.json
            cat > "$DAEMON_JSON" << DAEMONJSON
{
  "insecure-registries": ["$REGISTRY"]
}
DAEMONJSON
            echo -e "${GREEN}✓ Created $DAEMON_JSON${NC}"
            REGISTRY_STATUS="ADDED"
        fi

        # Only restart Docker if registry was newly added AND restart is not skipped
        if [ "$REGISTRY_STATUS" = "ADDED" ] && [ "$SKIP_DOCKER_RESTART" = false ]; then
            echo -e "${YELLOW}Restarting Docker Desktop...${NC}"
            echo "Please ensure Docker Desktop applies the new settings"

            # Try to restart Docker Desktop (may require manual intervention)
            osascript -e 'quit app "Docker"' 2>/dev/null || true
            sleep 3
            open -a Docker 2>/dev/null || true

            echo -e "${GREEN}✓ Docker configuration updated${NC}"

            # Wait for Docker to be ready
            wait_for_docker 60 || exit 1
        elif [ "$REGISTRY_STATUS" = "ADDED" ] && [ "$SKIP_DOCKER_RESTART" = true ]; then
            echo -e "${YELLOW}✓ Registry added to configuration${NC}"
            echo -e "${YELLOW}Note: Skipping Docker restart (--skip-docker-restart specified)${NC}"
            echo -e "${YELLOW}Please manually restart Docker Desktop to apply changes${NC}"
        else
            echo -e "${GREEN}✓ Registry already configured, no restart needed${NC}"
        fi
    else
        # Create or update daemon.json
        if [ -f "$DAEMON_JSON" ]; then
            # Backup existing config
            sudo cp "$DAEMON_JSON" "${DAEMON_JSON}.bak"

            # Add insecure registry using jq if available, otherwise use python
            if command -v jq &> /dev/null; then
                sudo jq ". + {\"insecure-registries\": ([.\"insecure-registries\"[]?, \"$REGISTRY\"] | unique)}" "$DAEMON_JSON" > /tmp/daemon.json
                sudo mv /tmp/daemon.json "$DAEMON_JSON"
            else
                # Fallback: use python
                python3 << EOF
import json
with open('$DAEMON_JSON', 'r') as f:
    config = json.load(f)
if 'insecure-registries' not in config:
    config['insecure-registries'] = []
if '$REGISTRY' not in config['insecure-registries']:
    config['insecure-registries'].append('$REGISTRY')
with open('/tmp/daemon.json', 'w') as f:
    json.dump(config, f, indent=2)
EOF
                sudo mv /tmp/daemon.json "$DAEMON_JSON"
            fi
        else
            # Create new daemon.json
            echo "{\"insecure-registries\": [\"$REGISTRY\"]}" | sudo tee "$DAEMON_JSON" > /dev/null
        fi

        # Restart Docker
        sudo systemctl restart docker
        sleep 3

        echo -e "${GREEN}✓ Docker registry configured${NC}"
    fi
else
    if [[ "$REGISTRY" =~ ^localhost ]] || [[ "$REGISTRY" =~ ^127\.0\.0\.1 ]]; then
        echo -e "${GREEN}✓ Using localhost registry (no configuration needed)${NC}"
    else
        echo -e "${GREEN}✓ Using secure registry (HTTPS)${NC}"
    fi
fi

# Test registry connectivity
echo -e "${YELLOW}Testing registry connectivity...${NC}"
if curl -sf "http://${REGISTRY}/v2/" > /dev/null 2>&1 || curl -sf "https://${REGISTRY}/v2/" > /dev/null 2>&1; then
    echo -e "${GREEN}✓ Registry is accessible${NC}"
else
    echo -e "${RED}Warning: Cannot reach registry at ${REGISTRY}${NC}"
    echo "Please ensure:"
    echo "1. Registry is running"
    echo "2. Network connectivity is available"
    echo "3. Firewall allows access"
fi

# =============================================================================
# Step 4: Generate Environment Configuration
# =============================================================================

echo ""
echo -e "${YELLOW}Step 4: Generating environment configuration...${NC}"

if [ -f "$ENV_FILE" ]; then
    if [ "$FORCE_OVERWRITE" = true ]; then
        echo -e "${YELLOW}Backing up existing .env.prod to .env.prod.backup...${NC}"
        cp "$ENV_FILE" "$ENV_FILE.backup.$(date +%Y%m%d_%H%M%S)"
        rm "$ENV_FILE"
        echo -e "${GREEN}✓ Backup created and file removed${NC}"
    else
        echo -e "${YELLOW}Warning: .env.prod already exists${NC}"
        echo -e "${YELLOW}Use --force to automatically overwrite (backup will be created)${NC}"
        read -t 10 -p "Overwrite? (y/N): " -n 1 -r 2>/dev/null || REPLY="N"
        echo
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            echo -e "${YELLOW}Backing up existing .env.prod...${NC}"
            cp "$ENV_FILE" "$ENV_FILE.backup.$(date +%Y%m%d_%H%M%S)"
            rm "$ENV_FILE"
            echo -e "${GREEN}✓ Backup created and file removed${NC}"
        else
            echo -e "${GREEN}✓ Using existing .env.prod${NC}"
            # Update REGISTRY in existing file if specified
            if [ -n "$REGISTRY" ]; then
                sed -i.bak "s|^REGISTRY=.*|REGISTRY=${REGISTRY}|" "$ENV_FILE"
                rm -f "$ENV_FILE.bak"
                echo -e "${GREEN}✓ Updated REGISTRY to ${REGISTRY}${NC}"
            fi
        fi
    fi
fi

if [ ! -f "$ENV_FILE" ]; then
    if [ ! -f "$ENV_EXAMPLE" ]; then
        echo -e "${RED}Error: .env.prod.example not found${NC}"
        # Pull failure is not fatal - continue
    fi

    # Copy template
    cp "$ENV_EXAMPLE" "$ENV_FILE"

    echo -e "${YELLOW}Generating secure keys...${NC}"

    # Generate JWT_SECRET (32 bytes base64)
    JWT_SECRET=$(openssl rand -base64 32)
    sed -i.bak "s|^JWT_SECRET=.*|JWT_SECRET=${JWT_SECRET}|" "$ENV_FILE"

    # Generate ENCRYPTION_KEY (32 bytes base64)
    ENCRYPTION_KEY=$(openssl rand -base64 32)
    sed -i.bak "s|^ENCRYPTION_KEY=.*|ENCRYPTION_KEY=${ENCRYPTION_KEY}|" "$ENV_FILE"

    # Generate INTERNAL_API_KEY (32 bytes base64)
    INTERNAL_API_KEY=$(openssl rand -base64 32)
    sed -i.bak "s|^INTERNAL_API_KEY=.*|INTERNAL_API_KEY=${INTERNAL_API_KEY}|" "$ENV_FILE"

    # Generate POSTGRES_PASSWORD (16 characters alphanumeric)
    POSTGRES_PASSWORD=$(openssl rand -base64 16 | tr -d '=/+' | cut -c1-16)
    sed -i.bak "s|^POSTGRES_PASSWORD=.*|POSTGRES_PASSWORD=${POSTGRES_PASSWORD}|" "$ENV_FILE"

    # Generate REDIS_PASSWORD (16 characters alphanumeric)
    REDIS_PASSWORD=$(openssl rand -base64 16 | tr -d '=/+' | cut -c1-16)
    sed -i.bak "s|^REDIS_PASSWORD=.*|REDIS_PASSWORD=${REDIS_PASSWORD}|" "$ENV_FILE"

    # Generate MINIO_ROOT_PASSWORD (16 characters alphanumeric)
    MINIO_ROOT_PASSWORD=$(openssl rand -base64 16 | tr -d '=/+' | cut -c1-16)
    sed -i.bak "s|^MINIO_ROOT_PASSWORD=.*|MINIO_ROOT_PASSWORD=${MINIO_ROOT_PASSWORD}|" "$ENV_FILE"

    # Generate BUSINESS_MINIO_SECRET_KEY (16 characters alphanumeric)
    BUSINESS_MINIO_SECRET_KEY=$(openssl rand -base64 16 | tr -d '=/+' | cut -c1-16)
    sed -i.bak "s|^BUSINESS_MINIO_SECRET_KEY=.*|BUSINESS_MINIO_SECRET_KEY=${BUSINESS_MINIO_SECRET_KEY}|" "$ENV_FILE"

    # Set REGISTRY
    sed -i.bak "s|^REGISTRY=.*|REGISTRY=${REGISTRY}|" "$ENV_FILE"

    # Clean up backup files
    rm -f "$ENV_FILE.bak"

    echo -e "${GREEN}✓ Generated .env.prod with secure keys${NC}"
    echo ""
    echo -e "${BLUE}Important:${NC} Save these credentials securely:"
    echo "  JWT_SECRET: ${JWT_SECRET:0:20}..."
    echo "  ENCRYPTION_KEY: ${ENCRYPTION_KEY:0:20}..."
    echo "  INTERNAL_API_KEY: ${INTERNAL_API_KEY:0:20}..."
    echo "  POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}"
    echo "  REDIS_PASSWORD: ${REDIS_PASSWORD}"
    echo "  MINIO_ROOT_PASSWORD: ${MINIO_ROOT_PASSWORD}"
    echo "  BUSINESS_MINIO_SECRET_KEY: ${BUSINESS_MINIO_SECRET_KEY}"
fi

# =============================================================================
# Step 5: Build Custom PostgreSQL Image
# =============================================================================

echo ""
echo -e "${YELLOW}Step 5: Building custom PostgreSQL image...${NC}"

# Check if Docker is already ready (skip waiting if it is)
if ! docker version &> /dev/null; then
    echo -e "${YELLOW}Docker not responding, waiting for it to be ready...${NC}"
    wait_for_docker 60 || exit 1
fi

if [ -d "$DEPLOY_DIR/postgres" ] && [ -f "$DEPLOY_DIR/postgres/Dockerfile" ]; then
    echo -e "${YELLOW}Building addp-postgres image with init scripts...${NC}"

    cd "$DEPLOY_DIR/postgres"
    # Force rebuild to ensure latest init-db.sql is included
    if docker build --no-cache -t addp-postgres:15-alpine . 2>&1; then
        echo -e "${GREEN}✓ PostgreSQL image built${NC}"
    else
        echo -e "${RED}✗ Failed to build PostgreSQL image${NC}"
        echo "Docker build error. Please check if Docker Desktop is running properly."
        exit 1
    fi
else
    echo -e "${YELLOW}Warning: PostgreSQL Dockerfile not found${NC}"
    echo "Using standard postgres:15-alpine image"
fi

cd "$DEPLOY_DIR"

# =============================================================================
# Step 6: Pull Service Images
# =============================================================================

if [ "$SKIP_IMAGE_PULL" = false ]; then
    echo ""
    echo -e "${YELLOW}Step 6: Pulling service images from registry...${NC}"

    # Pull images using docker compose
    if docker compose -f "$COMPOSE_FILE" --env-file "$ENV_FILE" pull; then
        echo -e "${GREEN}✓ Images pulled successfully${NC}"
    else
        echo -e "${RED}Warning: Failed to pull some images${NC}"
        echo "This may be normal if images don't exist in registry yet"
    fi
else
    echo ""
    echo -e "${YELLOW}Step 6: Skipping image pull (--skip-image-pull)${NC}"
fi

# =============================================================================
# Step 7: Start Services
# =============================================================================

echo ""
echo -e "${YELLOW}Step 7: Starting ADDP services...${NC}"

# Stop any existing services and clean volumes if this is a fresh deployment
echo -e "${YELLOW}Stopping existing services...${NC}"
if docker compose -f "$COMPOSE_FILE" ps --quiet | grep -q .; then
    # Services are running, check if we should clean volumes
    if [ ! -f "$DEPLOY_DIR/.deployment-initialized" ]; then
        echo -e "${YELLOW}First deployment detected, cleaning volumes...${NC}"
        docker compose -f "$COMPOSE_FILE" --env-file "$ENV_FILE" down -v || true
    else
        docker compose -f "$COMPOSE_FILE" --env-file "$ENV_FILE" down || true
    fi
else
    # No services running, clean volumes to ensure fresh start
    echo -e "${YELLOW}Cleaning any existing volumes...${NC}"
    docker compose -f "$COMPOSE_FILE" --env-file "$ENV_FILE" down -v 2>/dev/null || true
fi

# Start services
echo -e "${YELLOW}Starting all services...${NC}"
docker compose -f "$COMPOSE_FILE" --env-file "$ENV_FILE" --profile full up -d

# Mark deployment as initialized
touch "$DEPLOY_DIR/.deployment-initialized"

# Wait for services to start
echo -e "${YELLOW}Waiting for services to start...${NC}"
sleep 10

# =============================================================================
# Step 8: Health Check
# =============================================================================

echo ""
echo -e "${YELLOW}Step 8: Performing health checks...${NC}"

# Check service status
echo ""
echo -e "${BLUE}Service Status:${NC}"
docker compose -f "$COMPOSE_FILE" ps

# Health check function for HTTP services
check_http_health() {
    local service=$1
    local url=$2
    local max_attempts=30
    local attempt=0

    echo -e "\n${YELLOW}Checking ${service}...${NC}"

    while [ $attempt -lt $max_attempts ]; do
        if curl -sf "$url" > /dev/null 2>&1; then
            echo -e "${GREEN}✓ ${service} is healthy${NC}"
            return 0
        fi
        attempt=$((attempt + 1))
        echo -ne "  Attempt $attempt/$max_attempts\r"
        sleep 2
    done

    echo -e "${RED}✗ ${service} health check failed${NC}"
    return 1
}

# Health check function for Docker services
check_docker_health() {
    local service=$1
    local container_name=$2
    local max_attempts=30
    local attempt=0

    echo -e "\n${YELLOW}Checking ${service}...${NC}"

    while [ $attempt -lt $max_attempts ]; do
        local health_status=$(docker inspect --format='{{.State.Health.Status}}' "$container_name" 2>/dev/null || echo "none")
        if [ "$health_status" = "healthy" ]; then
            echo -e "${GREEN}✓ ${service} is healthy${NC}"
            return 0
        elif [ "$health_status" = "none" ]; then
            # No health check defined, check if running
            if docker inspect --format='{{.State.Running}}' "$container_name" 2>/dev/null | grep -q "true"; then
                echo -e "${GREEN}✓ ${service} is running${NC}"
                return 0
            fi
        fi
        attempt=$((attempt + 1))
        echo -ne "  Attempt $attempt/$max_attempts\r"
        sleep 2
    done

    echo -e "${RED}✗ ${service} health check failed${NC}"
    return 1
}

# Check key services
echo ""
echo -e "${BLUE}Health Checks:${NC}"

check_docker_health "PostgreSQL" "addp-postgres" || true
check_docker_health "Redis" "addp-redis" || true
check_http_health "MinIO" "http://localhost:9000/minio/health/live" || true
check_http_health "System Backend" "http://localhost:8080/health" || true
check_http_health "Gateway" "http://localhost:8000/health" || true
check_http_health "Portal" "http://localhost:8000/" || true

# =============================================================================
# Deployment Summary
# =============================================================================

echo ""
echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}Deployment Summary${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""
echo -e "${GREEN}✓ ADDP deployment complete!${NC}"
echo ""
echo -e "${BLUE}Access Points:${NC}"
echo "  Portal (Main):     http://$(hostname -I | awk '{print $1}'):8000"
echo "  System Backend:    http://$(hostname -I | awk '{print $1}'):8080"
echo "  Manager Backend:   http://$(hostname -I | awk '{print $1}'):8081"
echo "  Meta Backend:      http://$(hostname -I | awk '{print $1}'):8082"
echo "  MinIO Console:     http://$(hostname -I | awk '{print $1}'):9001"
echo ""
echo -e "${BLUE}Default Super Admin:${NC}"
echo "  Username: SuperAdmin"
echo "  Password: 20251001#SuperAdmin"
echo ""
echo -e "${RED}IMPORTANT:${NC} Change the default password after first login!"
echo ""
echo -e "${BLUE}Useful Commands:${NC}"
echo "  View logs:         docker compose -f $COMPOSE_FILE logs -f"
echo "  Stop services:     docker compose -f $COMPOSE_FILE down"
echo "  Restart services:  docker compose -f $COMPOSE_FILE restart"
echo "  Service status:    docker compose -f $COMPOSE_FILE ps"
echo ""
echo -e "${GREEN}Deployment completed successfully!${NC}"

#!/bin/bash
# =============================================================================
# ADDP Deployment Packager
# =============================================================================
# Description: Package all necessary files for server deployment
# Usage: ./2-package-deploy.sh [OPTIONS]
# Options:
#   --output OUTPUT_DIR       Output directory (default: ./deploy-package)
#   --server SERVER_USER@IP   Automatically transfer to server via rsync
#   --registry REGISTRY_URL   Registry URL to use in configs (default: localhost:5001)
# =============================================================================

set -e

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Default values
OUTPUT_DIR="./deploy-package"
SERVER=""
REGISTRY="${REGISTRY:-localhost:5001}"
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --output)
            OUTPUT_DIR="$2"
            shift 2
            ;;
        --server)
            SERVER="$2"
            shift 2
            ;;
        --registry)
            REGISTRY="$2"
            shift 2
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            exit 1
            ;;
    esac
done

cd "$PROJECT_ROOT"

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}ADDP Deployment Packager${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""
echo -e "Output: ${GREEN}${OUTPUT_DIR}${NC}"
echo -e "Registry: ${GREEN}${REGISTRY}${NC}"
if [ -n "$SERVER" ]; then
    echo -e "Target Server: ${GREEN}${SERVER}${NC}"
fi
echo ""

# Clean and create output directory
echo -e "${YELLOW}Preparing output directory...${NC}"
rm -rf "$OUTPUT_DIR"
mkdir -p "$OUTPUT_DIR"

# Create subdirectories
mkdir -p "$OUTPUT_DIR/configs"
mkdir -p "$OUTPUT_DIR/scripts"
mkdir -p "$OUTPUT_DIR/postgres"

echo -e "${GREEN}✓ Output directory created${NC}"

# Copy docker-compose file
echo -e "${YELLOW}Copying docker-compose configuration...${NC}"
if [ -f "docker-compose.prod.yml" ]; then
    cp docker-compose.prod.yml "$OUTPUT_DIR/"

    # Replace registry placeholder with actual registry
    sed -i.bak "s|localhost:5001|${REGISTRY}|g" "$OUTPUT_DIR/docker-compose.prod.yml"
    rm "$OUTPUT_DIR/docker-compose.prod.yml.bak"

    echo -e "${GREEN}✓ docker-compose.prod.yml copied${NC}"
else
    echo -e "${RED}Error: docker-compose.prod.yml not found${NC}"
    exit 1
fi

# Copy environment template
echo -e "${YELLOW}Copying environment template...${NC}"
if [ -f ".env.prod.example" ]; then
    cp .env.prod.example "$OUTPUT_DIR/"
    echo -e "${GREEN}✓ .env.prod.example copied${NC}"
else
    echo -e "${YELLOW}Warning: .env.prod.example not found, creating from .env.example${NC}"
    if [ -f ".env.example" ]; then
        cp .env.example "$OUTPUT_DIR/.env.prod.example"
        echo -e "${GREEN}✓ Created .env.prod.example from .env.example${NC}"
    else
        echo -e "${RED}Error: Neither .env.prod.example nor .env.example found${NC}"
        exit 1
    fi
fi

# Copy Nginx configuration
echo -e "${YELLOW}Copying Nginx configuration...${NC}"
if [ -f "configs/nginx.prod.conf" ]; then
    cp configs/nginx.prod.conf "$OUTPUT_DIR/configs/"
    echo -e "${GREEN}✓ nginx.prod.conf copied${NC}"
else
    echo -e "${YELLOW}Warning: configs/nginx.prod.conf not found${NC}"
    echo "Nginx configuration will be generated on server"
fi

# Copy PostgreSQL initialization script
echo -e "${YELLOW}Copying PostgreSQL initialization scripts...${NC}"
if [ -f "scripts/postgres/init-db.sql" ]; then
    cp scripts/postgres/init-db.sql "$OUTPUT_DIR/postgres/"
    echo -e "${GREEN}✓ init-db.sql copied${NC}"
else
    echo -e "${RED}Error: scripts/postgres/init-db.sql not found${NC}"
    exit 1
fi

# Copy Dockerfile for custom postgres image
if [ -f "scripts/postgres/Dockerfile" ]; then
    cp scripts/postgres/Dockerfile "$OUTPUT_DIR/postgres/"
    echo -e "${GREEN}✓ PostgreSQL Dockerfile copied${NC}"
else
    echo -e "${YELLOW}Warning: PostgreSQL Dockerfile not found${NC}"
fi

# Copy server setup script
echo -e "${YELLOW}Copying server setup script...${NC}"
cp scripts/deploy/3-server-setup.sh "$OUTPUT_DIR/scripts/"
chmod +x "$OUTPUT_DIR/scripts/3-server-setup.sh"
echo -e "${GREEN}✓ Server setup script copied${NC}"

# Create README
echo -e "${YELLOW}Creating deployment README...${NC}"
cat > "$OUTPUT_DIR/README.md" << 'EOF'
# ADDP Deployment Package

This package contains everything needed to deploy ADDP on a server.

## Package Contents

- `docker-compose.prod.yml` - Production Docker Compose configuration
- `.env.prod.example` - Environment variables template
- `configs/` - Configuration files (Nginx, etc.)
- `postgres/` - PostgreSQL initialization scripts and Dockerfile
- `scripts/3-server-setup.sh` - Server setup script

## Deployment Steps

### 1. Transfer Package to Server

```bash
# From your local machine
scp -r deploy-package/* user@server:~/addp/
```

Or use the packager script with --server option:
```bash
./2-package-deploy.sh --server user@server
```

### 2. Run Setup Script on Server

```bash
# SSH into server
ssh user@server

# Go to deployment directory
cd ~/addp

# Run setup script
./scripts/3-server-setup.sh
```

The setup script will:
- Check and install Docker & Docker Compose
- Configure Docker registry access
- Generate secure keys (.env.prod)
- Build custom PostgreSQL image with init scripts
- Pull all service images
- Start all services

### 3. Verify Deployment

```bash
# Check service status
docker compose -f docker-compose.prod.yml ps

# Check logs
docker compose -f docker-compose.prod.yml logs -f

# Access the application
# Portal: http://your-server:8000
```

### 4. Login

**Default Super Admin Account:**
- Username: `SuperAdmin`
- Password: `20251001#SuperAdmin`

**IMPORTANT:** Change the default password after first login!

## Troubleshooting

### Registry Connection Issues

If services can't pull images from registry, check:

```bash
# Verify registry is accessible
curl http://registry-host:5001/v2/

# Check Docker daemon config
cat /etc/docker/daemon.json
```

### Service Won't Start

```bash
# Check service logs
docker compose -f docker-compose.prod.yml logs SERVICE_NAME

# Restart specific service
docker compose -f docker-compose.prod.yml restart SERVICE_NAME
```

### Database Issues

```bash
# Check PostgreSQL logs
docker compose -f docker-compose.prod.yml logs postgres

# Access PostgreSQL shell
docker compose -f docker-compose.prod.yml exec postgres psql -U addp -d addp
```

## Configuration

### Environment Variables

Key variables in `.env.prod`:

- `JWT_SECRET` - JWT signing key (auto-generated)
- `ENCRYPTION_KEY` - Data encryption key (auto-generated)
- `POSTGRES_PASSWORD` - Database password (auto-generated)
- `REGISTRY` - Docker registry URL

### Ports

Default ports (configurable in docker-compose.prod.yml):

- **8000** - Portal & API Gateway (main entry point)
- 8080 - System Backend
- 8081 - Manager Backend
- 8082 - Meta Backend
- 5432 - PostgreSQL
- 6379 - Redis
- 9000/9001 - MinIO

## Updating Deployment

To update to a new version:

```bash
# Pull new images
docker compose -f docker-compose.prod.yml pull

# Restart services
docker compose -f docker-compose.prod.yml up -d

# Check status
docker compose -f docker-compose.prod.yml ps
```

## Support

For issues and questions, refer to project documentation at:
https://github.com/your-org/addp
EOF

echo -e "${GREEN}✓ README created${NC}"

# Create deployment info file
cat > "$OUTPUT_DIR/DEPLOY_INFO.txt" << EOF
ADDP Deployment Package
Generated: $(date)
Registry: ${REGISTRY}
Builder: $(whoami)
Host: $(hostname)
Git Branch: $(git branch --show-current)
Git Commit: $(git rev-parse --short HEAD)
EOF

echo -e "${GREEN}✓ Deployment info created${NC}"

# Create tarball
echo -e "${YELLOW}Creating tarball...${NC}"
TARBALL="addp-deploy-${TIMESTAMP}.tar.gz"
tar -czf "$TARBALL" -C "$OUTPUT_DIR" .
echo -e "${GREEN}✓ Tarball created: ${TARBALL}${NC}"

# Display package contents
echo ""
echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}Package Contents${NC}"
echo -e "${BLUE}========================================${NC}"
tree -L 2 "$OUTPUT_DIR" 2>/dev/null || find "$OUTPUT_DIR" -type f -o -type d | sort

# Transfer to server if specified
if [ -n "$SERVER" ]; then
    echo ""
    echo -e "${YELLOW}Transferring to server ${SERVER}...${NC}"

    # Extract server user and host
    SERVER_HOST="${SERVER#*@}"
    SERVER_USER="${SERVER%@*}"

    # Detect local IP address (for registry access)
    echo -e "${YELLOW}Detecting local IP address for registry access...${NC}"
    LOCAL_IP=$(ifconfig | grep "inet " | grep -v 127.0.0.1 | awk '{print $2}' | head -1)

    if [ -z "$LOCAL_IP" ]; then
        # Fallback: try ip command
        LOCAL_IP=$(ip addr show | grep "inet " | grep -v 127.0.0.1 | awk '{print $2}' | cut -d/ -f1 | head -1)
    fi

    if [ -n "$LOCAL_IP" ]; then
        echo -e "${GREEN}✓ Detected local IP: ${LOCAL_IP}${NC}"
        # Update REGISTRY to use local IP instead of localhost
        ACTUAL_REGISTRY="${LOCAL_IP}:5001"
        echo -e "${YELLOW}Registry will be configured as: ${ACTUAL_REGISTRY}${NC}"

        # Create a registry config file to pass to server
        echo "REGISTRY=${ACTUAL_REGISTRY}" > "$OUTPUT_DIR/.registry_config"
    else
        echo -e "${YELLOW}⚠ Could not detect local IP, using localhost:5001${NC}"
        echo -e "${YELLOW}You may need to manually configure REGISTRY in .env.prod${NC}"
        ACTUAL_REGISTRY="localhost:5001"
        echo "REGISTRY=${ACTUAL_REGISTRY}" > "$OUTPUT_DIR/.registry_config"
    fi

    # SSH options to avoid authentication issues
    SSH_OPTS="-o PasswordAuthentication=yes -o PubkeyAuthentication=no -o PreferredAuthentications=password"

    # Create remote directory
    echo -e "${YELLOW}Creating remote directory...${NC}"
    if ! ssh $SSH_OPTS "$SERVER" "mkdir -p ~/addp" 2>/dev/null; then
        echo -e "${RED}✗ Failed to create remote directory${NC}"
        echo -e "${YELLOW}Hint: Ensure SSH access is configured and password authentication is enabled${NC}"
        echo ""
        echo -e "${YELLOW}Manual deployment option:${NC}"
        echo "1. Copy tarball to server:"
        echo "   scp ${TARBALL} ${SERVER}:~/"
        echo "2. SSH to server and extract:"
        echo "   ssh ${SERVER}"
        echo "   tar -xzf $(basename ${TARBALL})"
        echo "   cd addp && ./scripts/3-server-setup.sh"
        exit 1
    fi

    # Try rsync first, fall back to scp if it fails
    echo -e "${YELLOW}Transferring files...${NC}"
    if command -v rsync &> /dev/null; then
        if rsync -avz --progress -e "ssh $SSH_OPTS" "$OUTPUT_DIR/" "$SERVER:~/addp/"; then
            echo -e "${GREEN}✓ Files transferred successfully (rsync)${NC}"
        else
            echo -e "${YELLOW}rsync failed, trying scp...${NC}"
            if scp -o PasswordAuthentication=yes -o PubkeyAuthentication=no -r "$OUTPUT_DIR"/* "$SERVER:~/addp/"; then
                echo -e "${GREEN}✓ Files transferred successfully (scp)${NC}"
            else
                echo -e "${RED}✗ File transfer failed${NC}"
                exit 1
            fi
        fi
    else
        # No rsync, use scp
        if scp -o PasswordAuthentication=yes -o PubkeyAuthentication=no -r "$OUTPUT_DIR"/* "$SERVER:~/addp/"; then
            echo -e "${GREEN}✓ Files transferred successfully (scp)${NC}"
        else
            echo -e "${RED}✗ File transfer failed${NC}"
            exit 1
        fi
    fi

    echo ""
    echo -e "${GREEN}Next steps:${NC}"
    echo "1. SSH into server: ssh $SERVER"
    echo "2. Run setup script with detected registry:"
    echo "   cd ~/addp && ./scripts/3-server-setup.sh --registry ${ACTUAL_REGISTRY}"
else
    echo ""
    echo -e "${GREEN}========================================${NC}"
    echo -e "${GREEN}Package Ready${NC}"
    echo -e "${GREEN}========================================${NC}"
    echo ""
    echo "Package location: ${OUTPUT_DIR}"
    echo "Tarball: ${TARBALL}"
    echo ""
    echo "To transfer to server:"
    echo "  scp -r ${OUTPUT_DIR}/* user@server:~/addp/"
    echo "Or:"
    echo "  scp ${TARBALL} user@server:~/ && ssh user@server 'cd ~/addp && tar -xzf ~/${TARBALL}'"
    echo ""
    echo "Or use the --server option:"
    echo "  ./2-package-deploy.sh --server user@server"
fi

echo ""
echo -e "${GREEN}✓ Packaging complete${NC}"

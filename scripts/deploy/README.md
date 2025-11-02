# ADDP Deployment Scripts

Production-ready deployment scripts for ADDP platform.

## Quick Start

### One-Click Deployment (Recommended)

```bash
./deploy-all.sh --server user@production-server --registry localhost:5001
```

### Step-by-Step Deployment

```bash
# Step 1: Build multi-arch images (ARM64 + AMD64)
./1-build-images.sh --registry localhost:5001

# Step 2: Package and transfer files
./2-package-deploy.sh --registry localhost:5001 --server user@server

# Step 3: On server, run setup
ssh user@server
cd ~/addp
./scripts/3-server-setup.sh --registry localhost:5001
```

## Scripts Overview

### 1-build-images.sh

Build Docker images for both ARM64 and AMD64 architectures.

**Features:**
- Multi-architecture support (ARM64 + AMD64)
- Docker Buildx integration
- Build cache optimization
- Selective service building

**Usage:**
```bash
./1-build-images.sh [OPTIONS]

Options:
  --registry URL        Registry URL (default: localhost:5001)
  --skip-cache          Force rebuild without cache
  --services LIST       Build specific services (comma-separated)
```

**Examples:**
```bash
# Build all services
./1-build-images.sh --registry localhost:5001

# Build without cache
./1-build-images.sh --skip-cache

# Build specific services only
./1-build-images.sh --services system-backend,gateway
```

### 2-package-deploy.sh

Package deployment files and optionally transfer to server.

**Features:**
- Creates deployment package with all necessary files
- Automatic tarball generation
- Optional rsync transfer to server
- Registry URL substitution in configs

**Usage:**
```bash
./2-package-deploy.sh [OPTIONS]

Options:
  --output DIR          Output directory (default: ./deploy-package)
  --server USER@HOST    Automatically transfer to server
  --registry URL        Registry URL (default: localhost:5001)
```

**Examples:**
```bash
# Package files locally
./2-package-deploy.sh --output ./my-deployment

# Package and auto-transfer
./2-package-deploy.sh --server user@server --registry registry.example.com:5001
```

**Package Contents:**
```
deploy-package/
├── docker-compose.prod.yml
├── .env.prod.example
├── configs/nginx.prod.conf
├── postgres/Dockerfile
├── postgres/init-db.sql
├── scripts/3-server-setup.sh
└── README.md
```

### 3-server-setup.sh

Server initialization and service deployment (run on target server).

**Features:**
- Automatic Docker & Docker Compose installation
- Registry configuration (including insecure registries)
- Secure key generation (JWT, encryption, passwords)
- Custom PostgreSQL image building with init scripts
- Service orchestration and health checks

**Usage:**
```bash
./3-server-setup.sh [OPTIONS]

Options:
  --registry URL            Registry URL (auto-detected from docker-compose)
  --skip-docker-install     Skip Docker installation
  --skip-image-pull         Skip pulling images
```

**What it does:**
1. ✅ Detect OS and install Docker/Docker Compose
2. ✅ Configure Docker registry access
3. ✅ Generate secure keys and passwords
4. ✅ Build custom PostgreSQL image
5. ✅ Pull service images
6. ✅ Start all services
7. ✅ Perform health checks

**Examples:**
```bash
# Full setup
./3-server-setup.sh --registry localhost:5001

# Skip Docker install (already installed)
./3-server-setup.sh --skip-docker-install

# Local setup without registry
./3-server-setup.sh --skip-image-pull
```

### deploy-all.sh

One-click deployment orchestration script.

**Features:**
- Orchestrates all deployment steps
- SSH automation for remote setup
- Progress tracking and error handling
- Deployment summary with access info

**Usage:**
```bash
./deploy-all.sh [OPTIONS]

Options:
  --server USER@HOST    Target server (required)
  --registry URL        Registry URL (default: localhost:5001)
  --skip-build          Skip image building
  --skip-transfer       Skip file transfer
  -h, --help            Show help message
```

**Examples:**
```bash
# Complete deployment
./deploy-all.sh --server user@production-server

# Deploy with custom registry
./deploy-all.sh --server user@server --registry registry.example.com:5001

# Skip build (images already built)
./deploy-all.sh --server user@server --skip-build
```

## Prerequisites

### Developer Machine
- Docker Desktop with Buildx
- SSH access to target server
- Network access to Docker registry

### Target Server
- Ubuntu 20.04+ / Debian 11+ / CentOS 8+ / RHEL 8+
- 4+ CPU cores (ARM64 or AMD64)
- 8GB+ RAM
- 50GB+ free storage
- Ports 8000, 5432, 6379, 9000, 9001 available

## Default Credentials

After deployment, use these credentials for first login:

```
Portal URL: http://server-ip:8000

Super Admin:
  Username: SuperAdmin
  Password: 20251001#SuperAdmin
```

**⚠️ IMPORTANT:** Change the default password immediately after first login!

## Generated Keys

The server setup script automatically generates:

- **JWT_SECRET** - JWT token signing (32 bytes base64)
- **ENCRYPTION_KEY** - Data encryption (32 bytes base64)
- **POSTGRES_PASSWORD** - Database password (16 bytes)
- **REDIS_PASSWORD** - Redis password (16 bytes)
- **MINIO_ROOT_PASSWORD** - MinIO admin password (16 bytes)

Keys are stored in `~/addp/.env.prod` on the server.

## Directory Structure

```
scripts/deploy/
├── README.md                    # This file
├── deploy-all.sh                # One-click deployment
├── 1-build-images.sh            # Multi-arch image builder
├── 2-package-deploy.sh          # Deployment packager
└── 3-server-setup.sh            # Server initialization
```

## Troubleshooting

### Registry Connection Issues

```bash
# Verify registry is accessible
curl http://registry-host:5001/v2/

# Check Docker daemon config
cat /etc/docker/daemon.json

# Restart Docker
sudo systemctl restart docker
```

### Service Won't Start

```bash
# Check logs
docker compose -f docker-compose.prod.yml logs service-name

# Restart service
docker compose -f docker-compose.prod.yml restart service-name
```

### Permission Denied

```bash
# Add user to docker group
sudo usermod -aG docker $USER

# Logout and login again
exit
ssh user@server
```

## Maintenance Commands

```bash
# View logs
docker compose -f docker-compose.prod.yml logs -f

# Check status
docker compose -f docker-compose.prod.yml ps

# Restart services
docker compose -f docker-compose.prod.yml restart

# Stop services
docker compose -f docker-compose.prod.yml down

# Update services
docker compose -f docker-compose.prod.yml pull
docker compose -f docker-compose.prod.yml up -d
```

## Support

- Full Documentation: [docs/DEPLOYMENT.md](../../docs/DEPLOYMENT.md)
- Architecture Guide: [CLAUDE.md](../../CLAUDE.md)
- GitHub Issues: https://github.com/your-org/addp/issues

---

**Version:** 0.0.6
**Last Updated:** 2025-10-31

# ADDP Production Deployment Guide

Complete guide for deploying ADDP (All Domain Data Platform) to production servers.

## Table of Contents

- [Overview](#overview)
- [Prerequisites](#prerequisites)
- [Deployment Methods](#deployment-methods)
- [One-Click Deployment](#one-click-deployment)
- [Step-by-Step Deployment](#step-by-step-deployment)
- [Post-Deployment](#post-deployment)
- [Troubleshooting](#troubleshooting)

---

## Overview

ADDP provides automated deployment scripts that handle:

✅ Multi-architecture Docker image building (ARM64 + AMD64)
✅ Automated key generation (JWT, encryption keys, passwords)
✅ Docker and Docker Compose installation
✅ Registry configuration
✅ Service orchestration
✅ Health checking

### Deployment Architecture

```
Developer Machine                 Production Server
┌─────────────────┐              ┌──────────────────────┐
│                 │              │                      │
│ Build Images    │──────────────>│ Pull Images          │
│ (ARM64 + AMD64) │   Registry   │                      │
│                 │              │ Deploy Services      │
│ Package Files   │──────────────>│ - PostgreSQL (DB)    │
│ Transfer        │   rsync/scp  │ - Redis (Cache)      │
│                 │              │ - MinIO (Storage)    │
└─────────────────┘              │ - Backend Services   │
                                 │ - Frontend (Portal)  │
                                 │ - Gateway (Router)   │
                                 └──────────────────────┘
```

---

## Prerequisites

### Developer Machine

- **Docker Desktop** with Buildx support
- **Git** (for source code)
- **SSH access** to target server
- **Network access** to Docker registry

### Production Server

- **Operating System**: Ubuntu 20.04+ / Debian 11+ / CentOS 8+ / RHEL 8+
- **CPU**: 4+ cores (ARM64 or AMD64)
- **Memory**: 8GB+ RAM
- **Storage**: 50GB+ free space
- **Network**: HTTP/HTTPS access to Docker registry
- **Ports**: 8000, 5432, 6379, 9000, 9001 available

### Network Requirements

- Server must have network connectivity to Docker registry
- SSH port (22) accessible from developer machine
- Outbound HTTPS for Docker image pulls (optional)

---

## Deployment Methods

ADDP offers two deployment approaches:

### Option 1: One-Click Deployment (Recommended)

Single command that orchestrates everything:

```bash
./scripts/deploy/deploy-all.sh --server user@server --registry registry.example.com:5001
```

**Best for:**
- Initial production deployments
- Quick deployments
- Automated CI/CD pipelines

### Option 2: Step-by-Step Deployment

Manual control over each phase:

1. Build images
2. Package and transfer files
3. Setup server

**Best for:**
- Understanding the deployment process
- Debugging deployment issues
- Customizing deployment steps

---

## One-Click Deployment

### 1. Prepare Registry

Start a local Docker registry (if not using external registry):

```bash
docker run -d -p 5001:5000 --restart=always --name registry registry:2
```

### 2. Run One-Click Deployment

```bash
cd /path/to/addp

# Deploy to server
./scripts/deploy/deploy-all.sh \
  --server user@production-server \
  --registry localhost:5001
```

**Options:**

- `--server USER@HOST` - Target server (required)
- `--registry URL` - Registry URL (default: localhost:5001)
- `--skip-build` - Skip image building
- `--skip-transfer` - Skip file transfer

### 3. Monitor Progress

The script will:

1. ✅ Build multi-arch images (5-15 min)
2. ✅ Push images to registry
3. ✅ Package deployment files
4. ✅ Transfer to server via rsync
5. ✅ Install Docker on server (if needed)
6. ✅ Configure registry access
7. ✅ Generate secure keys
8. ✅ Pull and start services
9. ✅ Perform health checks

### 4. Access Application

After successful deployment:

```
Portal:       http://server-ip:8000
Gateway:      http://server-ip:8000/api

Default Admin:
  Username: SuperAdmin
  Password: 20251001#SuperAdmin
```

**⚠️ IMPORTANT: Change the default password immediately!**

---

## Step-by-Step Deployment

### Step 1: Build Multi-Architecture Images

Build Docker images for both ARM64 and AMD64:

```bash
cd /path/to/addp

./scripts/deploy/1-build-images.sh \
  --registry localhost:5001
```

**Options:**

- `--registry URL` - Registry URL
- `--skip-cache` - Force rebuild without cache
- `--services service1,service2` - Build specific services only

**Services built:**

- `addp-system-backend`
- `addp-manager-backend`
- `addp-meta-backend`
- `addp-gateway`
- `addp-portal-frontend`
- `addp-system-frontend`
- `addp-manager-frontend`

### Step 2: Package Deployment Files

Package all necessary files for server deployment:

```bash
./scripts/deploy/2-package-deploy.sh \
  --registry localhost:5001 \
  --server user@server  # Optional: auto-transfer
```

**Options:**

- `--output DIR` - Output directory (default: ./deploy-package)
- `--server USER@HOST` - Automatically transfer via rsync
- `--registry URL` - Registry URL

**Package contents:**

```
deploy-package/
├── docker-compose.prod.yml    # Service definitions
├── .env.prod.example          # Config template
├── configs/
│   └── nginx.prod.conf        # Nginx configuration
├── postgres/
│   ├── Dockerfile             # Custom PostgreSQL image
│   └── init-db.sql            # Database initialization
├── scripts/
│   └── 3-server-setup.sh      # Server setup script
└── README.md                  # Deployment instructions
```

**Manual transfer (if --server not used):**

```bash
# Using tarball
tar -czf deploy.tar.gz -C deploy-package .
scp deploy.tar.gz user@server:~/
ssh user@server "mkdir -p ~/addp && cd ~/addp && tar -xzf ~/deploy.tar.gz"

# Or using rsync
rsync -avz deploy-package/ user@server:~/addp/
```

### Step 3: Server Setup

SSH into the server and run setup:

```bash
ssh user@server

cd ~/addp

./scripts/3-server-setup.sh \
  --registry registry.example.com:5001
```

**Options:**

- `--registry URL` - Registry URL
- `--skip-docker-install` - Skip Docker installation
- `--skip-image-pull` - Skip pulling images

**What it does:**

1. ✅ Detect OS and install Docker/Docker Compose
2. ✅ Configure insecure registry (if HTTP)
3. ✅ Generate secure keys:
   - `JWT_SECRET` (32 bytes base64)
   - `ENCRYPTION_KEY` (32 bytes base64)
   - `POSTGRES_PASSWORD` (16 bytes)
4. ✅ Build custom PostgreSQL image with init scripts
5. ✅ Pull service images from registry
6. ✅ Start all services with docker compose
7. ✅ Wait for services and perform health checks

---

## Post-Deployment

### Verify Deployment

#### 1. Check Service Status

```bash
cd ~/addp
docker compose -f docker-compose.prod.yml ps
```

Expected output:

```
NAME                STATUS              PORTS
postgres            Up 2 minutes        0.0.0.0:5432->5432/tcp
redis               Up 2 minutes        0.0.0.0:6379->6379/tcp
minio-system        Up 2 minutes        0.0.0.0:9000-9001->9000-9001/tcp
system-backend      Up 1 minute         0.0.0.0:8080->8080/tcp
manager-backend     Up 1 minute         0.0.0.0:8081->8081/tcp
meta-backend        Up 1 minute         0.0.0.0:8082->8082/tcp
gateway             Up 1 minute         0.0.0.0:8000->8000/tcp
portal-frontend     Up 1 minute         80/tcp
```

#### 2. Check Logs

```bash
# All services
docker compose -f docker-compose.prod.yml logs -f

# Specific service
docker compose -f docker-compose.prod.yml logs -f system-backend
```

#### 3. Test Endpoints

```bash
# Health check
curl http://localhost:8000/health

# System backend
curl http://localhost:8080/health

# Login API
curl -X POST http://localhost:8000/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"SuperAdmin","password":"20251001#SuperAdmin"}'
```

### Change Default Password

1. Access portal: `http://server-ip:8000`
2. Login with SuperAdmin / 20251001#SuperAdmin
3. Navigate to User Settings
4. Change password to a strong, unique password
5. Logout and login with new password

### Configure Firewall (Optional)

```bash
# Ubuntu/Debian
sudo ufw allow 8000/tcp
sudo ufw allow 22/tcp
sudo ufw enable

# CentOS/RHEL
sudo firewall-cmd --permanent --add-port=8000/tcp
sudo firewall-cmd --reload
```

### Setup SSL/TLS (Recommended for Production)

1. Obtain SSL certificate (Let's Encrypt, commercial CA, etc.)
2. Update `configs/nginx.prod.conf` with SSL configuration
3. Restart nginx service

---

## Troubleshooting

### Registry Connection Issues

**Problem:** Cannot pull images from registry

**Solution:**

```bash
# Check registry accessibility
curl http://registry-host:5001/v2/

# Check Docker daemon config
cat /etc/docker/daemon.json

# Should contain:
# {
#   "insecure-registries": ["registry-host:5001"]
# }

# Restart Docker
sudo systemctl restart docker
```

### Service Won't Start

**Problem:** Service fails to start or crashes

**Solution:**

```bash
# Check logs
docker compose -f docker-compose.prod.yml logs service-name

# Check service status
docker compose -f docker-compose.prod.yml ps

# Restart service
docker compose -f docker-compose.prod.yml restart service-name

# Rebuild and restart
docker compose -f docker-compose.prod.yml up -d --force-recreate service-name
```

### Database Connection Errors

**Problem:** Backend services can't connect to PostgreSQL

**Solution:**

```bash
# Check PostgreSQL logs
docker compose -f docker-compose.prod.yml logs postgres

# Verify PostgreSQL is running
docker compose -f docker-compose.prod.yml exec postgres pg_isready

# Check database initialization
docker compose -f docker-compose.prod.yml exec postgres \
  psql -U addp -d addp -c "\dt system.*"

# Should show system schema tables
```

### Port Conflicts

**Problem:** Port already in use

**Solution:**

```bash
# Find process using port
sudo lsof -i :8000

# Kill process (if safe)
sudo kill -9 <PID>

# Or change port in docker-compose.prod.yml
# Then restart services
```

### Permission Denied Errors

**Problem:** Permission issues with Docker

**Solution:**

```bash
# Add user to docker group
sudo usermod -aG docker $USER

# Logout and login again
exit
ssh user@server

# Verify
docker ps
```

### Out of Memory

**Problem:** Services crash due to memory

**Solution:**

```bash
# Check memory usage
free -h
docker stats

# Adjust service limits in docker-compose.prod.yml:
services:
  system-backend:
    deploy:
      resources:
        limits:
          memory: 1G
```

---

## Maintenance

### View Logs

```bash
# Real-time logs
docker compose -f docker-compose.prod.yml logs -f

# Last 100 lines
docker compose -f docker-compose.prod.yml logs --tail=100

# Specific service
docker compose -f docker-compose.prod.yml logs -f system-backend
```

### Backup Database

```bash
# Backup
docker compose -f docker-compose.prod.yml exec postgres \
  pg_dump -U addp addp > backup_$(date +%Y%m%d).sql

# Restore
cat backup_20251031.sql | docker compose -f docker-compose.prod.yml exec -T postgres \
  psql -U addp addp
```

### Update Deployment

```bash
# Pull new images
docker compose -f docker-compose.prod.yml pull

# Restart services
docker compose -f docker-compose.prod.yml up -d

# Check status
docker compose -f docker-compose.prod.yml ps
```

### Stop Services

```bash
# Stop all services
docker compose -f docker-compose.prod.yml down

# Stop and remove volumes (⚠️ deletes data)
docker compose -f docker-compose.prod.yml down -v
```

---

## Security Best Practices

1. ✅ Change default SuperAdmin password immediately
2. ✅ Use strong, unique passwords for all services
3. ✅ Enable SSL/TLS for production
4. ✅ Configure firewall to restrict access
5. ✅ Regularly backup database and configurations
6. ✅ Keep Docker and images updated
7. ✅ Monitor logs for suspicious activity
8. ✅ Use secrets management for sensitive data

---

## Support

For issues and questions:

- Documentation: `/docs/` in repository
- GitHub Issues: https://github.com/your-org/addp/issues
- CLAUDE.md: Architecture and development guide

---

**Last Updated:** 2025-10-31
**ADDP Version:** 0.0.6

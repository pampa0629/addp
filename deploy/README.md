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

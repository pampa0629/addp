# Business Infrastructure - Architecture Detection

## Overview

The business infrastructure automatically detects CPU architecture and uses the appropriate Docker images for optimal performance.

## Supported Architectures

### ARM64 (Apple Silicon M1/M2/M3/M4)

**PostgreSQL Strategy**:
- Uses `postgres:15` base image (official ARM64 support)
- PostGIS installed dynamically via `install-postgis.sh` script
- PostGIS packages: `postgresql-15-postgis-3`, `postgis`

**Pros**:
- ✅ Native ARM64 performance (no emulation)
- ✅ Automatic package installation
- ✅ PostGIS 3.6.0 with full spatial support

**Cons**:
- ⚠️ Slightly slower first startup (package installation ~30-60 seconds)
- ⚠️ Packages installed in container, not persisted in image

### AMD64 (Intel/AMD x86_64)

**PostgreSQL Strategy**:
- Uses `postgis/postgis:15-3.4` image (PostGIS pre-installed)
- No additional installation needed

**Pros**:
- ✅ Fast startup (PostGIS already included)
- ✅ Official PostGIS Docker image
- ✅ No package installation overhead

## How It Works

### 1. Architecture Detection

The `restart.sh` script automatically detects CPU architecture:

```bash
ARCH=$(uname -m)

case "${ARCH}" in
    x86_64)
        DOCKER_ARCH="linux/amd64"
        POSTGRES_IMAGE="postgis/postgis:15-3.4"
        ;;
    aarch64|arm64)
        DOCKER_ARCH="linux/arm64"
        POSTGRES_IMAGE="postgres:15"
        ;;
    armv7l)
        DOCKER_ARCH="linux/arm/v7"
        ;;
esac
```

### 2. Image Selection

**ARM64 (Apple Silicon)**:
```bash
export POSTGRES_IMAGE="postgres:15"
docker pull --platform=linux/arm64 postgres:15
```

**AMD64 (Intel/AMD)**:
```bash
export POSTGRES_IMAGE="postgis/postgis:15-3.4"
docker pull --platform=linux/amd64 postgis/postgis:15-3.4
```

### 3. PostGIS Installation (ARM64 only)

After container starts, `install-postgis.sh` runs:

```bash
# Install PostGIS packages in container
docker exec business-postgres sh -c '
    apt-get update -qq && \
    apt-get install -y -qq --no-install-recommends \
        postgresql-15-postgis-3 \
        postgis
'

# Create extension in database
docker exec business-postgres psql -U business -d business \
    -c "CREATE EXTENSION IF NOT EXISTS postgis;"
```

## Usage

### Automatic Mode (Recommended)

```bash
cd business
./scripts/restart.sh
```

The script will:
1. Detect your CPU architecture
2. Pull the correct image
3. Start services with proper platform
4. Install PostGIS (ARM64 only)

### Manual Override

You can force a specific image via environment variable:

```bash
# Use specific PostgreSQL image
export POSTGRES_IMAGE="postgres:15-alpine"
./scripts/restart.sh
```

Or set in `.env`:
```bash
POSTGRES_IMAGE=postgres:15
# or
POSTGRES_IMAGE=postgis/postgis:15-3.4
```

## Verification

Check running container architecture:

```bash
docker inspect business-postgres | grep Architecture
# Expected: "Architecture": "arm64"  (on Apple Silicon)
# Expected: "Architecture": "amd64"  (on Intel/AMD)
```

Check PostGIS installation:

```bash
docker exec business-postgres psql -U business -d business \
    -c "SELECT PostGIS_Version();"
```

Expected output:
```
                postgis_version
------------------------------------------------
 3.6 USE_GEOS=1 USE_PROJ=1 USE_STATS=1
(1 row)
```

## Troubleshooting

### ARM64: PostGIS Installation Fails

**Problem**: `install-postgis.sh` fails with package errors

**Solutions**:

1. **Check internet connection** (package download required)
   ```bash
   docker exec business-postgres ping -c 3 deb.debian.org
   ```

2. **Manually install packages**:
   ```bash
   docker exec -it business-postgres bash
   apt-get update
   apt-get install -y postgresql-15-postgis-3 postgis
   exit

   ./scripts/install-postgis.sh
   ```

3. **Use PostGIS image with Rosetta 2** (slower, but works):
   ```bash
   # In business/.env
   POSTGRES_IMAGE=postgis/postgis:15-3.4
   DOCKER_DEFAULT_PLATFORM=linux/amd64

   ./scripts/restart.sh
   ```

### AMD64: Platform Mismatch

**Problem**: Getting ARM64 image on AMD64 system

**Solution**: Clear Docker cache and restart
```bash
docker rmi postgres:15 postgis/postgis:15-3.4
./scripts/restart.sh
```

### Performance Issues on ARM64

**Problem**: Slow startup on ARM64

**Explanation**: First startup installs PostGIS packages (~30-60 seconds). Subsequent restarts are fast if container is not removed.

**Workaround**: Use container restart instead of recreation:
```bash
docker restart business-postgres
# vs
docker-compose up -d --force-recreate  # triggers reinstall
```

## Technical Details

### Why Not Use postgis/postgis on ARM64?

The official `postgis/postgis` Docker images (as of 2025-01) do not provide ARM64 builds:

```bash
docker pull --platform=linux/arm64 postgis/postgis:15-3.4
# Error: image with reference postgis/postgis:15-3.4 was found but does not provide the specified platform (linux/arm64)
```

**Root cause**: PostGIS Docker Hub tags only contain AMD64 manifests

**Evidence**:
```bash
docker manifest inspect postgis/postgis:15-3.4
# Only shows AMD64 architecture
```

### Alternative Solutions Considered

1. **Use Rosetta 2 emulation** (rejected):
   - Performance penalty (~20-30% slower)
   - Extra CPU usage and battery drain
   - Not recommended for production

2. **Build custom ARM64 PostGIS image** (rejected):
   - Complex multi-stage Dockerfile
   - Maintenance burden (tracking PostGIS updates)
   - Slower first deployment (build time)

3. **Use postgres:15 + dynamic PostGIS install** (✅ chosen):
   - Native ARM64 performance
   - Official PostgreSQL image (well-maintained)
   - Minimal complexity (apt-get install)
   - Fast subsequent startups (packages cached)

## Best Practices

### Development

- **ARM64 Macs**: Use default config (postgres:15 + PostGIS script)
- **AMD64 Macs/Linux**: Use default config (postgis/postgis:15-3.4)

### Production

- **ARM64 servers** (AWS Graviton, Oracle Ampere):
  ```bash
  # Build custom ARM64 PostGIS image for faster startup
  FROM postgres:15
  RUN apt-get update && apt-get install -y postgresql-15-postgis-3 postgis
  ```

- **AMD64 servers** (most cloud providers):
  ```bash
  # Use official PostGIS image
  POSTGRES_IMAGE=postgis/postgis:15-3.4
  ```

## References

- [PostGIS Docker Hub](https://hub.docker.com/r/postgis/postgis)
- [PostgreSQL Docker Hub](https://hub.docker.com/_/postgres)
- [PostGIS ARM64 Issue #216](https://github.com/postgis/docker-postgis/issues/216)
- [Docker Multi-Platform Images](https://docs.docker.com/build/building/multi-platform/)

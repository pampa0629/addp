# Multi-Architecture Deployment Guide

## Overview

ADDP now supports **multi-architecture Docker images** (AMD64 + ARM64), allowing flexible deployment across different CPU architectures. The deployment scripts automatically compile binaries and build images for both architectures, and the target server automatically selects the correct architecture when pulling images.

## How It Works

### 1. Build Process (Development Machine)

The `deploy-all.sh` script automatically builds for both architectures:

```bash
./scripts/deploy/deploy-all.sh --server user@remote-host
```

**What happens:**

1. **Step 0: Binary Compilation**
   - Compiles Go binaries for **both linux/amd64 and linux/arm64**
   - Output files: `server-amd64`, `server-arm64`, `worker-amd64`, `worker-arm64`
   - Location: Each service's backend directory

2. **Step 1: Multi-Arch Image Build**
   - Uses Docker Buildx to create multi-platform images
   - Builds for: `linux/amd64,linux/arm64`
   - Pushes to registry with **manifest lists** (Docker's multi-arch metadata)
   - Each image tag (e.g., `addp-system-backend:latest`) contains both architectures

3. **Step 2: Package and Transfer**
   - Packages `docker-compose.prod.yml` and deployment scripts
   - Transfers to remote server via SSH

4. **Step 3: Server Setup**
   - Server pulls images from registry
   - **Docker automatically selects the correct architecture** based on the server's CPU
   - No manual configuration needed!

### 2. Automatic Architecture Selection

When the server runs `docker compose up`, Docker automatically:

1. Detects the server's CPU architecture (amd64 or arm64)
2. Checks the image manifest list in the registry
3. Pulls the matching architecture variant
4. If the exact architecture isn't available, falls back to compatible variants

**Example:**
```bash
# On AMD server (x86_64):
docker pull localhost:5001/addp-system-backend:latest
# → Automatically pulls linux/amd64 variant

# On ARM server (Apple Silicon):
docker pull localhost:5001/addp-system-backend:latest
# → Automatically pulls linux/arm64 variant
```

## Usage

### Full Deployment (Recommended)

Deploy to remote server with automatic multi-arch support:

```bash
./scripts/deploy/deploy-all.sh --server user@192.168.1.182
```

This command:
- ✅ Compiles binaries for both amd64 and arm64
- ✅ Builds multi-arch Docker images
- ✅ Pushes to registry with manifest lists
- ✅ Transfers to server and starts services
- ✅ Server automatically pulls correct architecture

### Manual Steps (Advanced)

If you need to run steps individually:

```bash
# Step 1: Compile binaries for both architectures
./scripts/deploy/0-compile-binaries.sh --arch both

# Step 2: Build multi-arch images
./scripts/deploy/1-build-images.sh --registry localhost:5001 --multi-arch

# Step 3: Package and deploy
./scripts/deploy/2-package-deploy.sh --registry localhost:5001 --server user@host

# Step 4: SSH into server and run setup
ssh user@host
cd ~/addp
./scripts/3-server-setup.sh --force
```

## Verifying Multi-Arch Images

### Check if images support multiple architectures:

```bash
# Inspect image manifest
docker buildx imagetools inspect localhost:5001/addp-system-backend:latest

# Expected output:
# Name:      localhost:5001/addp-system-backend:latest
# MediaType: application/vnd.oci.image.index.v1+json
# Digest:    sha256:xxxxx
#
# Manifests:
#   Name:      localhost:5001/addp-system-backend:latest@sha256:yyyyy
#   MediaType: application/vnd.oci.image.manifest.v1+json
#   Platform:  linux/amd64
#
#   Name:      localhost:5001/addp-system-backend:latest@sha256:zzzzz
#   MediaType: application/vnd.oci.image.manifest.v1+json
#   Platform:  linux/arm64
```

### Check which architecture was pulled on the server:

```bash
# On the deployment server
docker inspect localhost:5001/addp-system-backend:latest | grep Architecture

# Expected output (on AMD server):
# "Architecture": "amd64"

# Expected output (on ARM server):
# "Architecture": "arm64"
```

## Configuration

### Default Behavior (as of latest version)

- **deploy-all.sh** always builds multi-arch by default
- Compiles binaries for both amd64 and arm64
- Builds images with `--multi-arch` flag
- No configuration changes needed

### Skip Multi-Arch (Fast Development Builds)

If you want to build only for your development machine's architecture (faster):

```bash
# Build for native architecture only
./scripts/deploy/1-build-images.sh --registry localhost:5001

# Without --multi-arch flag, builds only for current platform
```

## Requirements

### Development Machine (Build Host)

- Docker Desktop (includes Docker Buildx)
- Go 1.23+
- Network access to target server
- Local registry running on port 5001

### Target Server

- Docker or Docker Desktop
- Network access to development machine's registry
- Either AMD64 or ARM64 CPU architecture
- Insecure registry configured (done automatically by scripts)

## Troubleshooting

### Issue: Buildx not available

**Error:** `Error: Docker Buildx is not available`

**Solution:**
```bash
# Install Docker Desktop (includes buildx), or
docker buildx install
```

### Issue: Server pulls wrong architecture

**Error:** `exec format error` when containers start

**Diagnosis:**
```bash
# Check server architecture
uname -m
# x86_64 = amd64, aarch64 = arm64

# Check pulled image architecture
docker inspect image-name | grep Architecture
```

**Solution:** If architectures mismatch, manually remove and re-pull:
```bash
docker rmi localhost:5001/addp-system-backend:latest
docker pull localhost:5001/addp-system-backend:latest
docker inspect localhost:5001/addp-system-backend:latest | grep Architecture
```

### Issue: Registry manifest not found

**Error:** `manifest for image not found`

**Cause:** Multi-arch build didn't complete or didn't push properly

**Solution:**
```bash
# Rebuild with cache disabled
./scripts/deploy/1-build-images.sh --registry localhost:5001 --multi-arch --skip-cache

# Verify manifest exists
docker buildx imagetools inspect localhost:5001/addp-system-backend:latest
```

### Issue: Cross-compilation failures

**Error:** Compilation fails for non-native architecture

**Solution:**
```bash
# Ensure Go cross-compilation tools are available
go env GOOS GOARCH

# Clean and rebuild
cd system/backend
rm -f server-*
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o server-amd64 ./cmd/server
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o server-arm64 ./cmd/server
```

## Benefits

✅ **Flexibility**: Deploy to AMD or ARM servers without rebuilding
✅ **Efficiency**: Build once, deploy anywhere
✅ **Automation**: Server automatically selects correct architecture
✅ **Future-proof**: Easy to add more architectures (e.g., linux/riscv64)
✅ **No Configuration**: Zero changes needed in docker-compose files

## Architecture Support Matrix

| Service | AMD64 | ARM64 | Notes |
|---------|-------|-------|-------|
| system-backend | ✅ | ✅ | Go binary, cross-compiled |
| manager-backend | ✅ | ✅ | Go binary, cross-compiled |
| meta-backend | ✅ | ✅ | Go binary, cross-compiled |
| transfer-backend | ✅ | ✅ | Go binary, cross-compiled |
| transfer-worker | ✅ | ✅ | Go binary, cross-compiled |
| gateway | ✅ | ✅ | Go binary, cross-compiled |
| *-frontend | ✅ | ✅ | Node.js static build, platform-independent |
| portal | ✅ | ✅ | Node.js static build, platform-independent |
| nginx | ✅ | ✅ | Official multi-arch images |
| postgres | ✅ | ✅ | Official multi-arch images |
| redis | ✅ | ✅ | Official multi-arch images |
| minio | ✅ | ✅ | Official multi-arch images |
| elasticsearch | ✅ | ✅ | Official multi-arch images |

## Technical Details

### Manifest Lists

Docker manifest lists (OCI Image Index) allow a single image tag to reference multiple architecture variants:

```
localhost:5001/addp-system-backend:latest
├── linux/amd64 → sha256:abc123...
└── linux/arm64 → sha256:def456...
```

When you run `docker pull`, Docker:
1. Fetches the manifest list
2. Finds the entry matching the local architecture
3. Pulls only that specific variant

### Buildx Builder

The scripts create a dedicated buildx builder instance:

```bash
docker buildx create \
    --name addp-builder \
    --driver docker-container \
    --driver-opt network=host \
    --use \
    --bootstrap
```

This builder:
- Uses Docker container driver for isolation
- Enables network=host for registry access
- Supports multiple platform builds in single command
- Automatically removed and recreated on each build

### Binary Compilation Strategy

Go binaries are cross-compiled during the build process:

```bash
# For AMD64
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o server-amd64 ./cmd/server

# For ARM64
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o server-arm64 ./cmd/server
```

The Dockerfile then copies the appropriate binary:

```dockerfile
# Multi-stage build selects correct binary based on target architecture
FROM alpine:latest
ARG TARGETARCH
COPY system/backend/server-${TARGETARCH} /app/server
CMD ["/app/server"]
```

## See Also

- [Docker Buildx Documentation](https://docs.docker.com/buildx/working-with-buildx/)
- [OCI Image Manifest Specification](https://github.com/opencontainers/image-spec/blob/main/manifest.md)
- [Go Cross Compilation](https://go.dev/doc/install/source#environment)

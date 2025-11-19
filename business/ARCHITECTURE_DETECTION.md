# Business Infrastructure - CPU 架构自动检测

## 概述

Business 基础设施现在支持根据 CPU 架构自动选择合适的 PostgreSQL + PostGIS Docker 镜像，无需手动配置或安装。

## 支持的架构

### ARM64 (Apple Silicon M1/M2/M3/M4, AWS Graviton)

**PostgreSQL 策略**:
- 使用 `imresamu/postgis-arm64:15-3.4` 镜像（社区维护）
- PostGIS 已预装在镜像中，无需动态安装
- PostGIS 版本: 3.4

**MinIO 策略**:
- 使用 `minio/minio:latest` 官方镜像
- Docker 平台: `linux/arm64`
- 自动适配 ARM64 架构

**优点**:
- ✅ Native ARM64 性能（无模拟）
- ✅ 快速启动（PostGIS 已包含）
- ✅ 完整的空间数据支持
- ✅ 无需额外安装步骤

**注意**:
- ℹ️ PostgreSQL 使用社区维护镜像，非官方 PostGIS
- ✅ MinIO 使用官方多架构镜像

### AMD64 (Intel/AMD x86_64)

**PostgreSQL 策略**:
- 使用 `postgis/postgis:15-3.4` 镜像（官方镜像）
- PostGIS 已预装在镜像中
- PostGIS 版本: 3.4

**MinIO 策略**:
- 使用 `minio/minio:latest` 官方镜像
- Docker 平台: `linux/amd64`
- 自动适配 AMD64 架构

**优点**:
- ✅ 快速启动（PostGIS 已包含）
- ✅ 官方 PostGIS Docker 镜像
- ✅ 官方 MinIO 多架构镜像
- ✅ 无需额外安装步骤
- ✅ 官方维护和支持

## 工作原理

### 1. 架构检测

启动脚本（`start.sh`, `restart.sh`, `deploy-business.sh`）自动检测 CPU 架构：

```bash
ARCH=$(uname -m)

case "${ARCH}" in
    aarch64|arm64)
        export POSTGRES_IMAGE="imresamu/postgis-arm64:15-3.4"
        export DOCKER_PLATFORM="linux/arm64"
        echo "✓ Using ARM64 images"
        ;;
    x86_64)
        export POSTGRES_IMAGE="postgis/postgis:15-3.4"
        export DOCKER_PLATFORM="linux/amd64"
        echo "✓ Using AMD64 images"
        ;;
    *)
        echo "⚠️ Unknown architecture, defaulting to ARM64"
        export POSTGRES_IMAGE="imresamu/postgis-arm64:15-3.4"
        export DOCKER_PLATFORM="linux/arm64"
        ;;
esac
```

### 2. 镜像选择

Docker Compose 配置文件使用环境变量：

**docker-compose.yml** (开发环境):
```yaml
postgres:
  image: ${POSTGRES_IMAGE:-imresamu/postgis-arm64:15-3.4}

minio:
  image: minio/minio:latest
  platform: ${DOCKER_PLATFORM:-linux/arm64}
```

**docker-compose.prod.yml** (生产环境):
```yaml
postgres:
  image: ${POSTGRES_IMAGE:-postgis/postgis:15-3.4}

minio:
  image: minio/minio:latest
  platform: ${DOCKER_PLATFORM:-linux/amd64}
```

### 3. 镜像拉取

脚本自动拉取正确架构的镜像：

**ARM64**:
```bash
docker pull imresamu/postgis-arm64:15-3.4
docker pull --platform=linux/arm64 minio/minio:latest
```

**AMD64**:
```bash
docker pull postgis/postgis:15-3.4
docker pull --platform=linux/amd64 minio/minio:latest
```

## 使用方法

### 开发环境（本地）

**方式 1: 使用启动脚本（推荐）**

```bash
cd business
./scripts/start.sh      # 自动检测架构并启动
```

**方式 2: 使用重启脚本**

```bash
cd business
./scripts/restart.sh    # 自动检测、清理旧镜像、重新拉取并启动
```

**方式 3: 直接使用 docker-compose**

```bash
cd business
docker-compose up -d    # 使用 .env 中的 POSTGRES_IMAGE 或默认值
```

脚本会自动：
1. 检测你的 CPU 架构
2. 拉取正确的镜像
3. 启动服务
4. 验证 PostGIS 扩展

### 生产环境（服务器部署）

**使用部署脚本（推荐）**

```bash
# 在服务器上执行
cd /opt/addp-business
./scripts/deploy-business.sh
```

部署脚本会自动：
1. 检测服务器 CPU 架构
2. 拉取对应架构的镜像
3. 配置环境变量
4. 启动服务
5. 健康检查

### 手动指定镜像（可选）

如果需要覆盖自动检测，可以通过以下方式手动指定镜像：

**方法 1: 环境变量**

```bash
export POSTGRES_IMAGE="postgis/postgis:15-3.4"
./scripts/start.sh
```

**方法 2: .env 文件**

编辑 `business/.env`:
```bash
POSTGRES_IMAGE=imresamu/postgis-arm64:15-3.4
# 或
POSTGRES_IMAGE=postgis/postgis:15-3.4
```

然后启动：
```bash
docker-compose up -d
```

## 验证

### 检查当前系统架构

```bash
uname -m
# ARM64 输出: aarch64 或 arm64
# AMD64 输出: x86_64
```

### 检查容器使用的镜像

```bash
docker ps --filter name=business-postgres --format "{{.Image}}"
# ARM64 预期: imresamu/postgis-arm64:15-3.4
# AMD64 预期: postgis/postgis:15-3.4
```

### 检查镜像架构

```bash
docker image inspect business-postgres --format '{{.Architecture}}'
# ARM64 预期: arm64
# AMD64 预期: amd64
```

### 验证 PostGIS 扩展

```bash
docker exec business-postgres psql -U business -d business \
    -c "SELECT PostGIS_Version();"
```

预期输出:
```
              postgis_version
-----------------------------------------------
 3.4 USE_GEOS=1 USE_PROJ=1 USE_STATS=1
(1 row)
```

## 故障排除

### 问题 1: 镜像拉取失败

**症状**: `docker pull` 失败或超时

**解决方案**:

```bash
# 方法 1: 重试拉取
docker pull imresamu/postgis-arm64:15-3.4  # ARM64
# 或
docker pull postgis/postgis:15-3.4         # AMD64

# 方法 2: 配置 Docker 镜像加速器
# 编辑 /etc/docker/daemon.json (Linux) 或 Docker Desktop 设置 (Mac)
{
  "registry-mirrors": ["https://docker.mirrors.sjtug.sjtu.edu.cn"]
}

# 重启 Docker
sudo systemctl restart docker  # Linux
# 或在 Mac 上重启 Docker Desktop
```

### 问题 2: 架构不匹配警告

**症状**:
```
WARNING: The requested image's platform (linux/arm64) does not match
the detected host platform (linux/amd64) and no specific platform was requested
```

**原因**: 在 AMD64 机器上尝试运行 ARM64 镜像（或反之）

**解决方案**:

```bash
# 1. 检查系统架构
uname -m

# 2. 清理错误的镜像
docker rmi imresamu/postgis-arm64:15-3.4 postgis/postgis:15-3.4

# 3. 重新运行启动脚本（会自动检测并拉取正确镜像）
cd business
./scripts/restart.sh
```

### 问题 3: PostGIS 扩展不可用

**症状**: SQL 查询显示 `function postgis_version() does not exist`

**原因**: PostGIS 扩展未创建

**解决方案**:

```bash
# 手动创建 PostGIS 扩展
docker exec business-postgres psql -U business -d business \
  -c "CREATE EXTENSION IF NOT EXISTS postgis; \
      CREATE EXTENSION IF NOT EXISTS postgis_topology;"

# 验证
docker exec business-postgres psql -U business -d business \
  -c "SELECT PostGIS_Version();"
```

### 问题 4: 容器启动失败

**症状**: `docker-compose up -d` 失败

**检查步骤**:

```bash
# 1. 查看容器日志
docker logs business-postgres

# 2. 检查端口占用
lsof -i :5433

# 3. 检查镜像是否存在
docker images | grep postgis

# 4. 完全清理并重新开始
docker-compose down -v
docker rmi $(docker images -q 'postgis/*' 'imresamu/postgis-arm64')
./scripts/restart.sh
```

## 技术细节

### 为什么 ARM64 使用社区镜像?

官方 `postgis/postgis` Docker 镜像（截至 2025 年 1 月）不提供 ARM64 构建：

```bash
docker pull --platform=linux/arm64 postgis/postgis:15-3.4
# 错误: 镜像不支持指定的平台 (linux/arm64)
```

**根本原因**: PostGIS Docker Hub 标签仅包含 AMD64 manifest

**解决方案**: 使用社区维护的 `imresamu/postgis-arm64` 镜像
- 由 PostGIS 社区成员维护
- 与官方镜像功能相同
- 提供完整的 ARM64 支持

## 最佳实践

### 开发环境

- **ARM64 Mac (M1/M2/M3)**: 使用默认配置（自动检测）
- **AMD64 Mac/Linux**: 使用默认配置（自动检测）
- **推荐**: 始终使用 `./scripts/start.sh` 或 `./scripts/restart.sh`

### 生产环境

**ARM64 服务器** (AWS Graviton, Oracle Ampere):
```bash
# .env 文件
POSTGRES_IMAGE=imresamu/postgis-arm64:15-3.4
```

**AMD64 服务器** (大多数云提供商):
```bash
# .env 文件
POSTGRES_IMAGE=postgis/postgis:15-3.4
```

**推荐**: 使用 `./scripts/deploy-business.sh` 自动检测和部署

### 多架构环境

如果需要在多种架构的服务器上部署：

1. ✅ 不要在 `.env` 中硬编码 `POSTGRES_IMAGE`
2. ✅ 让部署脚本在每台服务器上独立检测架构
3. ✅ 使用统一的部署脚本 `deploy-business.sh`

## 环境变量优先级

```
1. 显式导出的 POSTGRES_IMAGE (export POSTGRES_IMAGE=...)
2. .env 文件中的 POSTGRES_IMAGE
3. Docker Compose 默认值 (docker-compose.yml 中 :- 后面的值)
```

## 相关文件

### 脚本文件

- [scripts/start.sh](scripts/start.sh) - 开发环境启动脚本（含架构检测）
- [scripts/restart.sh](scripts/restart.sh) - 重启脚本（含镜像清理和重新拉取）
- [scripts/deploy-business.sh](scripts/deploy-business.sh) - 生产环境部署脚本

### 配置文件

- [docker-compose.yml](docker-compose.yml) - 开发环境 Docker Compose 配置
- [docker-compose.prod.yml](docker-compose.prod.yml) - 生产环境 Docker Compose 配置
- [.env.example](.env.example) - 开发环境配置模板
- [.env.prod.example](.env.prod.example) - 生产环境配置模板

## 参考资料

- [PostGIS 官方镜像](https://hub.docker.com/r/postgis/postgis)
- [PostgreSQL 官方镜像](https://hub.docker.com/_/postgres)
- [ARM64 PostGIS 社区镜像](https://hub.docker.com/r/imresamu/postgis-arm64)
- [Docker 多架构镜像文档](https://docs.docker.com/build/building/multi-platform/)

## 更新日志

### 2025-11-19
- ✅ 实现自动 CPU 架构检测
- ✅ 更新所有启动和部署脚本
- ✅ 更新 docker-compose 配置文件支持动态镜像选择
- ✅ 添加 .env 配置文档和注释
- ✅ 更新架构检测文档

# ADDP Infrastructure - Architecture Detection

## 概述

ADDP 主系统基础设施现在支持**架构自适应**,自动检测 CPU 架构并使用最优的 PostgreSQL/PostGIS 镜像配置。

## 支持的架构

### ARM64 (Apple Silicon M1/M2/M3/M4)

**PostgreSQL 策略**:
- 使用 `postgres:15` 基础镜像 (原生 ARM64 支持)
- PostGIS 通过 `infra-init-postgis.sh` 脚本动态安装
- PostGIS 包: `postgresql-15-postgis-3`, `postgis`

**优点**:
- ✅ 原生 ARM64 性能 (无模拟)
- ✅ 自动包安装
- ✅ PostGIS 3.6.0 完整空间数据支持

**缺点**:
- ⚠️ 首次启动稍慢 (包安装约 30-60 秒)
- ⚠️ 包安装在容器中,不在镜像中持久化

### AMD64 (Intel/AMD x86_64)

**PostgreSQL 策略**:
- 使用 `postgis/postgis:15-3.4` 镜像 (PostGIS 预装)
- 无需额外安装

**优点**:
- ✅ 快速启动 (PostGIS 已包含)
- ✅ 官方 PostGIS Docker 镜像
- ✅ 无包安装开销

## 工作原理

### 1. 架构检测

`infra-restart.sh` 脚本自动检测 CPU 架构:

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
esac
```

### 2. 镜像选择

**ARM64**:
```bash
export POSTGRES_IMAGE="postgres:15"
docker pull --platform=linux/arm64 postgres:15
```

**AMD64**:
```bash
export POSTGRES_IMAGE="postgis/postgis:15-3.4"
docker pull --platform=linux/amd64 postgis/postgis:15-3.4
```

### 3. PostGIS 安装

#### 对于 ARM64

`infra-up.sh` 检测镜像类型并调用安装脚本:

```bash
# 检查是否使用标准 postgres 镜像
CURRENT_IMAGE=$(docker inspect addp-postgres --format '{{.Config.Image}}')
if [[ "$CURRENT_IMAGE" == "postgres:15" ]]; then
    bash scripts/infra-init-postgis.sh
fi
```

`infra-init-postgis.sh` 执行:

```bash
# 在容器内安装 PostGIS 包
docker exec addp-postgres sh -c '
    apt-get update -qq && \
    apt-get install -y -qq --no-install-recommends \
        postgresql-15-postgis-3 \
        postgis
'

# 在数据库中创建扩展
docker exec addp-postgres psql -U addp -d addp \
    -c "CREATE EXTENSION IF NOT EXISTS postgis;"
```

#### 对于 AMD64

PostGIS 已预装在 `postgis/postgis:15-3.4` 镜像中,只需在数据库中启用扩展。

## 使用方法

### 自动模式 (推荐)

```bash
# 从项目根目录
./scripts/infra-restart.sh
```

脚本将:
1. 检测 CPU 架构
2. 拉取正确的镜像
3. 启动基础设施服务
4. 安装 PostGIS (ARM64) 或跳过 (AMD64)

### 手动指定镜像

通过环境变量强制使用特定镜像:

```bash
# 使用标准 PostgreSQL
export POSTGRES_IMAGE="postgres:15"
./scripts/infra-restart.sh
```

或在 `.env` 中设置:
```bash
POSTGRES_IMAGE=postgres:15
# or
POSTGRES_IMAGE=postgis/postgis:15-3.4
```

## 验证

### 检查容器架构

```bash
docker inspect addp-postgres | grep Architecture
# 期望: "Architecture": "arm64"  (在 Apple Silicon 上)
# 期望: "Architecture": "amd64"  (在 Intel/AMD 上)
```

### 检查 PostGIS 安装

```bash
docker exec addp-postgres psql -U addp -d addp \
    -c "SELECT PostGIS_Version();"
```

期望输出:
```
            postgis_version
---------------------------------------
 3.6 USE_GEOS=1 USE_PROJ=1 USE_STATS=1
(1 row)
```

### 列出已安装扩展

```bash
docker exec addp-postgres psql -U addp -d addp -c "\dx"
```

期望输出包含:
```
 postgis          | 3.6.0   | public     | PostGIS geometry and geography
 postgis_topology | 3.6.0   | topology   | PostGIS topology
```

## 故障排除

### ARM64: PostGIS 安装失败

**问题**: `infra-init-postgis.sh` 报包安装错误

**解决方案**:

1. **检查网络连接** (需要下载包):
   ```bash
   docker exec addp-postgres ping -c 3 deb.debian.org
   ```

2. **手动安装包**:
   ```bash
   docker exec -it addp-postgres bash
   apt-get update
   apt-get install -y postgresql-15-postgis-3 postgis
   exit

   ./scripts/infra-init-postgis.sh
   ```

3. **使用 AMD64 镜像 + Rosetta 2** (较慢):
   ```bash
   export POSTGRES_IMAGE="postgis/postgis:15-3.4"
   export DOCKER_DEFAULT_PLATFORM="linux/amd64"
   ./scripts/infra-restart.sh
   ```

### 数据库初始化错误

**问题**: `ERROR: trigger "update_data_sources_updated_at" for relation "data_sources" already exists`

**原因**: 重复运行 `init-db.sql` 时尝试重复创建触发器

**影响**: 不影响核心功能,PostGIS 和表结构都已正确创建

**解决**: 可忽略此错误,或清理数据卷后重新初始化:
```bash
docker stop addp-postgres
docker rm addp-postgres
docker volume rm addp_postgres_data
./scripts/infra-restart.sh
```

### 平台不匹配警告

**问题**: Docker 提示 "platform mismatch" 或使用错误架构

**解决**: 清理镜像缓存并重新拉取
```bash
docker rmi postgres:15 postgis/postgis:15-3.4
./scripts/infra-restart.sh
```

## 修改的文件

### 新增脚本

- **[scripts/infra-init-postgis.sh](../scripts/infra-init-postgis.sh)**: PostGIS 安装脚本
  - 检测 PostGIS 是否已安装
  - 在容器内安装 PostGIS 包 (ARM64)
  - 创建 `postgis` 和 `postgis_topology` 扩展

### 修改的文件

- **[scripts/infra-restart.sh](../scripts/infra-restart.sh)**:
  - 添加架构检测逻辑
  - 根据架构选择镜像 (ARM64 → `postgres:15`, AMD64 → `postgis/postgis:15-3.4`)
  - 清理和拉取正确架构的镜像
  - 验证镜像架构

- **[scripts/infra-up.sh](../scripts/infra-up.sh)**:
  - 自动检测并设置 `DOCKER_DEFAULT_PLATFORM`
  - 检测镜像类型并决定是否调用 PostGIS 安装脚本
  - 支持 `SKIP_POSTGIS_INSTALL=1` 环境变量跳过安装

- **[docker-compose.yml](../docker-compose.yml)**:
  - PostgreSQL 服务镜像改为 `${POSTGRES_IMAGE:-postgres:15}`
  - 添加 `platform: ${DOCKER_DEFAULT_PLATFORM:-linux/arm64}`

- **[scripts/init-db.sql](../scripts/init-db.sql)**:
  - 移除 `CREATE EXTENSION postgis` (由安装脚本处理)
  - 添加注释说明 PostGIS 安装流程

## 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `POSTGRES_IMAGE` | `postgres:15` | PostgreSQL 镜像名称 |
| `DOCKER_DEFAULT_PLATFORM` | `linux/arm64` | Docker 平台架构 |
| `SKIP_POSTGIS_INSTALL` | `0` | 设为 `1` 跳过 PostGIS 安装 |
| `SKIP_INFRA_DB_INIT` | `0` | 设为 `1` 跳过数据库初始化 |

## 最佳实践

### 开发环境

- **ARM64 Macs**: 使用默认配置 (`postgres:15` + PostGIS 脚本)
- **AMD64 Macs/Linux**: 使用默认配置 (`postgis/postgis:15-3.4`)

### 生产环境

#### ARM64 服务器 (AWS Graviton, Oracle Ampere)

构建自定义 ARM64 PostGIS 镜像以加速启动:

```dockerfile
FROM postgres:15
RUN apt-get update && \
    apt-get install -y postgresql-15-postgis-3 postgis && \
    apt-get clean && rm -rf /var/lib/apt/lists/*
```

#### AMD64 服务器 (大多数云提供商)

使用官方 PostGIS 镜像:
```bash
POSTGRES_IMAGE=postgis/postgis:15-3.4
```

## 与 Business Infrastructure 的一致性

ADDP 主系统和 Business Infrastructure 现在使用**相同的架构检测机制**:

| 项目 | ARM64 镜像 | AMD64 镜像 | PostGIS 安装脚本 |
|------|-----------|-----------|------------------|
| **ADDP 主系统** | `postgres:15` | `postgis/postgis:15-3.4` | `scripts/infra-init-postgis.sh` |
| **Business Infrastructure** | `postgres:15` | `postgis/postgis:15-3.4` | `business/scripts/install-postgis.sh` |

两个系统都能在 ARM64 和 AMD64 架构上原生运行,无需模拟器!

## 相关文档

- [Business Infrastructure 架构检测](../business/ARCHITECTURE_DETECTION.md)
- [PostGIS Docker Hub](https://hub.docker.com/r/postgis/postgis)
- [PostgreSQL Docker Hub](https://hub.docker.com/_/postgres)

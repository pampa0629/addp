# ADDP 本地 Docker 部署脚本

本目录包含用于本地 Docker Compose 部署测试的脚本。

## 概述

`scripts/local/` 提供了一套完整的工具来管理 ADDP 平台的本地 Docker 部署。所有服务通过 Docker Compose 运行,使用预先构建好的镜像。

**关键特性**:
- ✅ **幂等性**: 所有脚本可重复执行,不会造成错误
- ✅ **镜像验证**: 自动检查所需镜像是否存在
- ✅ **健康检查**: 等待服务完全就绪后再继续
- ✅ **分层管理**: 基础设施和应用层独立管理
- ✅ **友好输出**: 彩色输出和清晰的进度提示

## 前置条件

### 1. Docker 环境

确保 Docker 已安装并运行:

```bash
# macOS
open -a Docker

# Linux
sudo systemctl start docker

# 验证
docker info
```

### 2. 构建镜像

在首次使用前,需要构建所有服务镜像:

```bash
# 从项目根目录执行

# 步骤 1: 编译所有二进制文件
bash scripts/build/compile.sh

# 步骤 2: 构建 Docker 镜像
bash scripts/build/build-images.sh

# 验证镜像已创建
docker images | grep localhost:5001/addp
```

**预期输出**:
```
localhost:5001/addp-system-backend       latest
localhost:5001/addp-manager-backend      latest
localhost:5001/addp-meta-backend         latest
localhost:5001/addp-transfer-backend     latest
localhost:5001/addp-orchestrator-backend latest
localhost:5001/addp-develop-backend      latest
localhost:5001/addp-gateway              latest
localhost:5001/addp-portal               latest
localhost:5001/addp-nginx                latest
...
```

## 使用方法

### 快速启动

```bash
# 启动完整 ADDP 平台
bash scripts/local/start.sh

# 访问服务
# - Portal (推荐): http://localhost:80
# - Gateway:        http://localhost:8000
# - System Backend: http://localhost:8180
```

### 脚本说明

#### 1. start.sh - 启动服务

**功能**: 启动完整的 ADDP Docker 环境

```bash
bash scripts/local/start.sh
```

**执行流程**:
1. ✓ 检查 Docker 运行状态
2. ✓ 验证所有必需镜像存在
3. ✓ **调用 scripts/infra/up.sh 启动基础设施**
   - 端口冲突检查
   - 镜像自动拉取
   - 幂等性启动 (已运行的服务会被跳过)
   - 数据库/MinIO/Meilisearch 初始化
   - 健康检查等待
4. ✓ 启动应用层 (所有后端、前端、Worker、Gateway、Nginx)
5. ✓ 等待关键服务健康检查通过
6. ✓ 显示访问地址和管理命令

**幂等性**: 可重复执行,已运行的容器会被跳过,使用 `docker compose up -d` 确保服务存在且运行。

**镜像检查**: 如果发现缺少镜像,会提示:
```
❌ Missing images:
  - localhost:5001/addp-system-backend:latest

Please build images first:
  bash scripts/build/compile.sh
  bash scripts/build/build-images.sh
```

#### 2. stop.sh - 停止服务

**功能**: 停止运行中的 ADDP 服务

```bash
# 停止应用层 (保留基础设施)
bash scripts/local/stop.sh

# 停止所有服务 (包括基础设施)
bash scripts/local/stop.sh --all

# 停止并删除数据卷 (危险操作!)
bash scripts/local/stop.sh --all --volumes
```

**选项**:
- `--all`: 同时停止基础设施层
- `--volumes`: 删除数据卷 (会删除所有数据,需要确认)

**默认行为**: 仅停止应用层,保留基础设施运行,这样重启应用时速度更快。

#### 3. status.sh - 查看状态

**功能**: 显示所有服务的详细状态

```bash
bash scripts/local/status.sh
```

**输出内容**:
- 基础设施层容器状态 (postgres, redis, minio, meilisearch)
- 应用层容器状态 (所有后端、前端、Worker)
- 服务 URL 及可用性 (✓ 运行中 / ✗ 未运行)
- 资源使用情况 (CPU、内存,Top 5)
- 管理命令提示

**示例输出**:
```
=== Service URLs ===

  ✓ Portal (Recommended):  http://localhost:80
  ✓ Gateway:               http://localhost:8000
  ✓ System Backend:        http://localhost:8180

Infrastructure:
  ✓ PostgreSQL:            localhost:5433
  ✓ Redis:                 localhost:6379
  ✓ MinIO Console:         http://localhost:9001
  ✓ Meilisearch:           http://localhost:7700

=== Resource Usage (Top 5 by Memory) ===

NAME                 CPU %     MEM USAGE / LIMIT
postgres             0.12%     85.5MiB / 7.77GiB
system-backend       0.03%     45.2MiB / 7.77GiB
...
```

#### 4. restart.sh - 重启服务

**功能**: 重启 ADDP 服务 (停止 + 启动)

```bash
# 重启应用层
bash scripts/local/restart.sh

# 重启所有服务 (包括基础设施)
bash scripts/local/restart.sh --all
```

**实现**: 顺序调用 `stop.sh` 和 `start.sh`,中间等待 3 秒确保容器完全停止。

## 架构说明

ADDP 本地部署采用**分层架构**:

### 基础设施层 (docker-compose.infra.yml)

```
postgres      (port 5433)  - PostgreSQL 数据库
redis         (port 6379)  - Redis 缓存和队列
minio         (port 9000-9001) - MinIO 对象存储
meilisearch   (port 7700)  - Meilisearch 全文搜索
```

### 应用层 (docker-compose.yml)

```
后端服务:
  system-backend       (port 8180)
  manager-backend      (port 8081)
  meta-backend         (port 8082)
  transfer-backend     (port 8083)
  orchestrator-backend (port 8084)
  develop-backend      (port 8085)
  gateway              (port 8000)

Worker 服务:
  manager-worker
  meta-worker
  transfer-worker

前端服务:
  system-frontend      (port 8090)
  manager-frontend     (port 8091)
  meta-frontend        (port 8092)
  transfer-frontend    (port 8093)
  orchestrator-frontend (port 8094)
  develop-frontend     (port 8095)
  portal               (port 5170)
  nginx                (port 80) - 统一入口
```

### 镜像来源

所有服务使用本地镜像仓库:
```
${REGISTRY:-localhost:5001}/addp-<service>:${IMAGE_TAG:-latest}
```

## 与其他部署模式对比

| 部署模式 | 脚本目录 | 用途 | 运行方式 | 镜像来源 |
|---------|---------|------|---------|---------|
| **开发模式** | scripts/dev/ | 本地开发调试 | 直接运行 Go/npm | 无 (源码) |
| **本地 Docker** | scripts/local/ | 本地容器化测试 | Docker Compose | localhost:5001 |
| **生产环境** | scripts/prod/ | 服务器部署 | Docker Compose/Swarm | 远程 Registry |

## 常见问题

### 1. Docker 未运行

```bash
# 错误提示
❌ Docker is not running

# 解决方法
# macOS
open -a Docker

# Linux
sudo systemctl start docker
```

### 2. 镜像不存在

```bash
# 错误提示
❌ Missing images:
  - localhost:5001/addp-system-backend:latest

# 解决方法
bash scripts/build/compile.sh
bash scripts/build/build-images.sh
```

### 3. 端口被占用

```bash
# 查看端口占用
lsof -i :5433  # PostgreSQL
lsof -i :8180  # System Backend
lsof -i :80    # Nginx

# 解决方法
# 1. 停止占用端口的进程
# 2. 或修改 .env 配置使用其他端口
```

### 4. 容器启动失败

```bash
# 查看容器日志
docker compose -f docker-compose.yml logs system-backend

# 查看所有容器状态
bash scripts/local/status.sh

# 重启特定服务
docker compose -f docker-compose.yml restart system-backend
```

### 5. 健康检查超时

**现象**: 启动时看到 "⚠️ health check timeout"

**原因**: 服务需要更长时间初始化 (首次启动数据库迁移等)

**解决**:
```bash
# 等待一段时间后检查状态
bash scripts/local/status.sh

# 查看服务日志确认是否正常启动
docker compose -f docker-compose.yml logs -f system-backend
```

## 数据持久化

所有数据存储在 Docker volumes 中,停止服务不会丢失数据:

```bash
# 查看 volumes
docker volume ls | grep addp

# 数据卷
postgres_data        - PostgreSQL 数据
redis_data           - Redis 持久化数据
minio_data           - MinIO 对象存储数据
meilisearch_data     - Meilisearch 索引数据
```

**删除数据** (危险操作):
```bash
# 停止服务并删除数据卷
bash scripts/local/stop.sh --all --volumes

# 手动删除特定 volume
docker volume rm addp-infra_postgres_data
```

## 高级用法

### 查看实时日志

```bash
# 所有应用服务
docker compose -f docker-compose.yml logs -f

# 特定服务
docker compose -f docker-compose.yml logs -f system-backend

# 多个服务
docker compose -f docker-compose.yml logs -f system-backend gateway

# 最近 100 行
docker compose -f docker-compose.yml logs --tail=100 system-backend
```

### 进入容器调试

```bash
# 进入 PostgreSQL
docker exec -it postgres psql -U addp -d addp

# 进入 Redis
docker exec -it redis redis-cli -a addp_redis

# 进入后端容器
docker exec -it system-backend sh
```

### 手动管理服务

```bash
# 仅启动基础设施
docker compose -f docker-compose.infra.yml up -d

# 仅启动应用层
docker compose -f docker-compose.yml up -d

# 重启特定服务
docker compose -f docker-compose.yml restart gateway

# 查看特定服务状态
docker compose -f docker-compose.yml ps system-backend
```

## 性能优化建议

1. **资源分配**: 确保 Docker 分配足够资源
   - macOS: Docker Desktop → Settings → Resources
   - 建议: 至少 4 CPU cores, 8GB RAM

2. **镜像缓存**: 使用本地 registry 缓存镜像,避免重复构建

3. **数据卷**: 使用 volumes 而非 bind mounts,性能更好

4. **网络**: 所有服务使用同一网络 (addp-network),通信更快

## 相关文档

- [scripts/build/README.md](../build/README.md) - 镜像构建脚本说明
- [scripts/infra/README.md](../infra/README.md) - 基础设施管理脚本
- [scripts/dev/README.md](../dev/README.md) - 开发模式脚本
- [scripts/prod/README.md](../prod/README.md) - 生产部署脚本
- [CLAUDE.md](../../CLAUDE.md) - 项目整体架构文档
- [docker-compose.infra.yml](../../docker-compose.infra.yml) - 基础设施层配置
- [docker-compose.yml](../../docker-compose.yml) - 应用层配置

## 注意事项

1. **端口冲突**: 确保所需端口未被占用 (特别是 80, 5433, 6379, 8000, 8180)
2. **资源消耗**: 完整平台需要较多资源,建议至少 8GB RAM
3. **首次启动**: 首次启动需要初始化数据库,可能需要 1-2 分钟
4. **网络隔离**: 所有服务在 `addp-network` 网络中,与主机隔离
5. **数据备份**: 定期备份重要数据 (PostgreSQL, MinIO)

## 故障排查流程

1. **查看状态**: `bash scripts/local/status.sh`
2. **检查日志**: `docker compose -f docker-compose.yml logs [service]`
3. **验证镜像**: `docker images | grep addp`
4. **检查端口**: `lsof -i :[port]`
5. **重启服务**: `bash scripts/local/restart.sh`
6. **完全重置**: `bash scripts/local/stop.sh --all --volumes && bash scripts/local/start.sh`

如果问题仍然存在,请查看具体服务日志或联系开发团队。

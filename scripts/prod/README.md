# Production Deployment Scripts 使用指南

本目录包含 ADDP 生产环境部署和管理脚本。

## 目录

- [start.sh](#startsh) - 生产环境启动脚本
- [stop.sh](#stopsh) - 生产环境停止脚本
- [health-check.sh](#health-checksh) - 健康检查脚本
- [wait-infra.sh](#wait-infrash) - 基础设施就绪等待
- [swarm/](#swarm) - Docker Swarm 高可用部署

---

## start.sh

**用途**: 按正确顺序启动完整的 ADDP 生产环境（基础设施 + 后端 + 前端 + Console）

### 使用方法

```bash
# 基本用法（启动所有服务）
./scripts/prod/start.sh

# 从项目根目录调用
cd /path/to/addp
./scripts/prod/start.sh
```

### 启动流程

脚本按以下顺序自动启动所有服务：

```
[1/5] 基础设施层
   ↓
   - PostgreSQL (5432)
   - Redis (6379)
   - MinIO (9000-9001)
   - Meilisearch (7700)
   ↓
   等待基础设施就绪（调用 wait-infra.sh）

[2/5] System Backend
   ↓
   - System Backend (8180)
   ↓
   等待 System Backend 健康检查通过

[3/5] 业务后端服务
   ↓
   - Manager Backend (8081) + Worker
   - Meta Backend (8082) + Worker
   - Transfer Backend (8083) + Worker
   - Orchestrator Backend (8084)
   - Develop Backend (8185)
   - Gateway (8000)

[4/5] 后端健康检查
   ↓
   等待所有后端服务健康（最多 90 秒）

[5/5] 前端服务
   ↓
   - Console Frontend (5170)
   - System Frontend (8090)
   - Manager Frontend (8091)
   - Meta Frontend (8092)
   - Transfer Frontend (8093)
   - Orchestrator Frontend (8094)
   - Develop Frontend (8095)
   - Nginx Gateway (80)
```

### 健康检查机制

- **System Backend**: 检查 `http://localhost:8180/health`（最多等待 60 秒）
- **其他后端**: 检查各自的 `/health` 端点（每个服务最多等待 30 秒）
- **超时处理**: 任何服务启动超时会输出日志并退出

### 访问地址

启动成功后，可通过以下地址访问：

- **✨ 推荐访问**: http://localhost （Nginx 统一入口）
- **Console 独立访问**: http://localhost:5170
- **API Gateway**: http://localhost:8000
- **各模块独立访问**:
  - System: http://localhost:8090
  - Manager: http://localhost:8091
  - Meta: http://localhost:8092
  - Transfer: http://localhost:8093
  - Orchestrator: http://localhost:8094
  - Develop: http://localhost:8095

### 示例

```bash
# 场景 1: 首次部署启动
cd /opt/addp
./scripts/prod/start.sh

# 场景 2: 服务器重启后启动
./scripts/prod/start.sh

# 场景 3: 检查启动日志
./scripts/prod/start.sh 2>&1 | tee startup.log
```

---

## stop.sh

**用途**: 停止所有 ADDP 生产环境服务

### 使用方法

```bash
# 停止所有服务
./scripts/prod/stop.sh

# 停止服务并移除容器
./scripts/prod/stop.sh --remove

# 停止服务并移除卷（⚠️ 会删除数据）
./scripts/prod/stop.sh --volumes
```

### 参数说明

| 参数 | 说明 |
|------|------|
| 无参数 | 停止容器但保留数据 |
| `--remove` | 停止并移除容器 |
| `--volumes` | 停止、移除容器并删除数据卷 |

### 停止顺序

```
1. 停止前端服务（Console, System, Manager, etc.）
2. 停止业务后端（Gateway, Manager, Meta, Transfer, etc.）
3. 停止基础设施（PostgreSQL, Redis, MinIO, Meilisearch）
```

### ⚠️ 重要提示

```bash
# ❌ 危险操作：会删除所有数据
./scripts/prod/stop.sh --volumes

# ✅ 安全操作：仅停止服务，保留数据
./scripts/prod/stop.sh
```

### 示例

```bash
# 场景 1: 临时停止服务（维护）
./scripts/prod/stop.sh

# 场景 2: 完全清理重新部署
./scripts/prod/stop.sh --volumes
docker volume prune -f

# 场景 3: 重启服务
./scripts/prod/stop.sh
./scripts/prod/start.sh
```

---

## health-check.sh

**用途**: 检查所有 ADDP 服务的健康状态

### 使用方法

```bash
# 检查所有服务健康状态
./scripts/prod/health-check.sh

# 持续监控（每 5 秒检查一次）
watch -n 5 ./scripts/prod/health-check.sh

# 仅检查后端服务
./scripts/prod/health-check.sh --backend-only
```

### 检查内容

- ✅ PostgreSQL 连接（端口 5432）
- ✅ Redis 连接（端口 6379）
- ✅ MinIO API（端口 9000）
- ✅ System Backend 健康端点（/health）
- ✅ Manager Backend 健康端点
- ✅ Meta Backend 健康端点
- ✅ Transfer Backend 健康端点
- ✅ Orchestrator Backend 健康端点
- ✅ Develop Backend 健康端点
- ✅ Gateway 健康端点

### 输出格式

```
========================================
ADDP 服务健康检查
========================================

基础设施:
  ✓ PostgreSQL (5432)
  ✓ Redis (6379)
  ✓ MinIO (9000)
  ✓ Meilisearch (7700)

后端服务:
  ✓ System Backend (8180)
  ✓ Manager Backend (8081)
  ✓ Meta Backend (8082)
  ✓ Transfer Backend (8083)
  ✓ Orchestrator Backend (8084)
  ✓ Develop Backend (8185)
  ✓ Gateway (8000)

前端服务:
  ✓ Console Frontend (5170)
  ✓ Nginx Gateway (80)

所有服务运行正常！
```

### 示例

```bash
# 场景 1: 部署后验证
./scripts/prod/start.sh
./scripts/prod/health-check.sh

# 场景 2: 定期监控
watch -n 30 ./scripts/prod/health-check.sh

# 场景 3: 故障排查
if ! ./scripts/prod/health-check.sh; then
  docker-compose -f docker-compose.yml logs --tail=100
fi
```

---

## wait-infra.sh

**用途**: 等待基础设施服务（PostgreSQL, Redis, MinIO, Meilisearch）就绪

### 使用方法

```bash
# 等待基础设施就绪（默认超时 60 秒）
./scripts/prod/wait-infra.sh

# 自定义超时时间
TIMEOUT=120 ./scripts/prod/wait-infra.sh
```

### 检查机制

- **PostgreSQL**: 尝试连接端口 5432
- **Redis**: 尝试 PING 命令
- **MinIO**: 检查 HTTP 端点 `/minio/health/live`
- **Meilisearch**: 检查 HTTP 端点 `/health`

### 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `TIMEOUT` | `60` | 等待超时时间（秒） |
| `RETRY_INTERVAL` | `2` | 重试间隔（秒） |

### 示例

```bash
# 场景 1: 启动脚本自动调用
./scripts/prod/start.sh
# ↓ 内部调用 wait-infra.sh

# 场景 2: 手动检查基础设施
docker-compose -f docker-compose.infra.yml up -d
./scripts/prod/wait-infra.sh

# 场景 3: 自定义超时
TIMEOUT=300 ./scripts/prod/wait-infra.sh
```

---

## swarm/

**用途**: Docker Swarm 高可用部署脚本

Docker Swarm 提供：
- ✅ 服务自动重启（容器崩溃自动恢复）
- ✅ 多副本负载均衡（如 Transfer Worker x2）
- ✅ 滚动更新零停机
- ✅ 资源限制和预留

### Swarm 脚本

| 脚本 | 用途 |
|------|------|
| `swarm/init.sh` | 初始化 Swarm 集群（一次性） |
| `swarm/deploy.sh` | 部署或更新服务栈 |
| `swarm/status.sh` | 查看服务状态和副本 |

### 使用方法

```bash
# 1. 初始化 Swarm（首次）
./scripts/prod/swarm/init.sh

# 2. 部署服务栈
./scripts/prod/swarm/deploy.sh

# 3. 查看状态
./scripts/prod/swarm/status.sh

# 4. 查看特定服务日志
docker service logs -f addp_transfer-bounded-worker

# 5. 手动扩容
docker service scale addp_transfer-bounded-worker=3

# 6. 滚动更新
docker service update --image addp-transfer-bounded-worker:v2.0 addp_transfer-bounded-worker
```

### Swarm vs Compose

| 特性 | Docker Compose | Docker Swarm |
|------|---------------|--------------|
| 开发环境 | ✅ 推荐 | ❌ 过于复杂 |
| 生产环境 | ⚠️ 单机可用 | ✅ 推荐（高可用） |
| 自动重启 | ⚠️ restart: always | ✅ 内置支持 |
| 多副本 | ❌ 需手动启动 | ✅ replicas: 2 |
| 负载均衡 | ❌ 无 | ✅ 内置 VIP LB |
| 零停机更新 | ❌ 无 | ✅ 滚动更新 |

详见: [scripts/prod/README.md](README.md)

---

## 完整的生产部署流程

### 首次部署

```bash
# 1. 准备服务器
ssh user@production-server
sudo apt update && sudo apt install -y docker.io docker-compose

# 2. 克隆或上传项目
cd /opt
git clone https://github.com/your-org/addp.git
cd addp

# 3. 配置环境变量
cp .env.example .env
vi .env  # 修改密码、密钥等

# 4. 启动服务
./scripts/prod/start.sh

# 5. 验证部署
./scripts/prod/health-check.sh

# 6. 访问系统
curl http://localhost:80
```

### 更新部署

```bash
# 1. 拉取最新代码
cd /opt/addp
git pull

# 2. 重新构建镜像（如果有代码变更）
make build BUILD_ARGS="--arch both"
IMAGE_TAG=v1.1.0 make build-images IMAGE_BUILD_ARGS=--multi-arch

# 3. 重启服务
./scripts/prod/stop.sh
./scripts/prod/start.sh

# 4. 验证更新
./scripts/prod/health-check.sh
```

### 回滚部署

```bash
# 1. 切换到旧版本
cd /opt/addp
git checkout v1.0.0

# 2. 停止当前服务
./scripts/prod/stop.sh

# 3. 使用旧版本镜像启动
IMAGE_TAG=v1.0.0 ./scripts/prod/start.sh

# 4. 验证
./scripts/prod/health-check.sh
```

---

## 故障排查

### 问题 1: 服务启动超时

**错误信息**:
```
错误: System Backend 启动超时
```

**解决方法**:
```bash
# 查看日志
docker-compose -f docker-compose.yml logs system-backend | tail -100

# 检查基础设施
docker-compose -f docker-compose.infra.yml ps

# 重启基础设施
docker-compose -f docker-compose.infra.yml restart
```

### 问题 2: 端口冲突

**错误信息**:
```
Error: port 8180 is already in use
```

**解决方法**:
```bash
# 检查占用端口的进程
lsof -i :8180

# 杀死进程或修改端口配置
kill -9 <PID>

# 或修改 docker-compose.yml 中的端口映射
```

### 问题 3: 健康检查失败

**错误信息**:
```
✗ Manager Backend (超时)
```

**解决方法**:
```bash
# 查看容器状态
docker ps | grep manager-backend

# 查看详细日志
docker logs <container-id> --tail=100

# 重启服务
docker-compose -f docker-compose.yml restart manager-backend

# 检查配置
docker exec <container-id> env | grep SYSTEM_URL
```

### 问题 4: 数据丢失

当前仓库尚未提供经过恢复演练的平台级备份入口。生产部署前必须单独建立同时覆盖 PostgreSQL、MinIO、部署配置与密钥材料的备份方案，并记录版本、校验和、保留策略和恢复演练证据；不能把单次 `pg_dump` 或文件复制当作完整平台备份。

---

## 维护命令

### 查看日志

```bash
# 所有服务日志
docker-compose -f docker-compose.infra.yml logs -f
docker-compose -f docker-compose.yml logs -f

# 特定服务日志
docker-compose -f docker-compose.yml logs -f system-backend

# 最近 100 行
docker-compose -f docker-compose.yml logs --tail=100 manager-backend
```

### 查看状态

```bash
# 容器状态
docker-compose -f docker-compose.infra.yml ps
docker-compose -f docker-compose.yml ps

# 资源使用
docker stats

# 磁盘使用
docker system df
```

### 清理资源

```bash
# 清理未使用的镜像
docker image prune -a

# 清理未使用的卷
docker volume prune

# 清理未使用的网络
docker network prune

# 完整清理（⚠️ 谨慎使用）
docker system prune -a --volumes
```

---

## 相关文档

- [scripts/build/README.md](../build/README.md) - 构建脚本文档
- [scripts/dev/README.md](../dev/README.md) - 开发环境脚本文档
- [scripts/README.md](../README.md) - Scripts 架构设计和入口文档
- [docs/guide/addp部署和开发步骤.md](../../docs/guide/addp部署和开发步骤.md) - 部署与开发启动指南
- [CLAUDE.md](../../CLAUDE.md) - 项目总体架构文档

---

## 最佳实践

1. **使用统一入口**: 推荐通过 http://localhost (Nginx) 访问，获得最佳体验
2. **备份恢复**: 上线前建立覆盖 PostgreSQL、MinIO、部署配置和密钥材料的备份策略，并定期完成恢复演练
3. **监控日志**: 使用 `health-check.sh` 定期检查服务健康
4. **渐进式更新**: 先在测试环境验证，再部署到生产环境
5. **使用 Swarm**: 生产环境推荐使用 Docker Swarm 提高可用性
6. **资源限制**: 在 docker-compose.yml 中配置 CPU 和内存限制
7. **安全加固**: 修改默认密码、启用 HTTPS、配置防火墙

---

**Version:** 0.0.12
**Last Updated:** 2025-12-09

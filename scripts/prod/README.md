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

1. 启动基础设施并等待其就绪。
2. 启动 System Backend，使用 Docker Compose 容器健康检查等待就绪。
3. 启动 `addp-platform` 中全部平台服务，并等待有健康检查的容器变为 healthy、其他容器进入 running。
4. 启动 `addp-runtimes` 中全部内置 Runtime，再检查两组服务状态。

容器内部端口固定，平台 Compose 仅将 Nginx 的 `NGINX_PORT` 发布到宿主机。启动脚本报告统一访问地址；`ADDP_PUBLIC_ORIGIN` 留空时为 `http://localhost:<NGINX_PORT>`。域名、HTTPS 或上级反向代理部署须填写用户实际访问的 `ADDP_PUBLIC_ORIGIN`。

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

**用途**: 停止平台与内置 Runtime 容器，保留基础设施、Business 服务及全部数据卷

### 使用方法

```bash
# 停止平台与 Runtime 服务
./scripts/prod/stop.sh
```

该脚本没有 `--remove` 或 `--volumes` 参数。需要停止 Infra 时，单独使用 `scripts/infra/down.sh`，默认保留卷。需要销毁数据时，先核验可恢复备份及目标 Compose project，再走 Infra 专用交互确认流程。

### 示例

```bash
# 场景 1: 临时停止服务（维护）
./scripts/prod/stop.sh

# 场景 2: 重启服务
./scripts/prod/stop.sh
./scripts/prod/start.sh
```

---

## health-check.sh

`./scripts/prod/health-check.sh` 检查基础设施和应用 Compose 中全部容器的运行或健康状态，并通过实际发布的 Nginx 端口请求 `/health`。任一容器未就绪或统一入口不可达时返回非零状态；不依赖模块在宿主机发布固定端口。

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

# 6. 访问系统（使用 .env 中的 ADDP_PUBLIC_ORIGIN；默认 NGINX_PORT=80）
curl http://localhost:80/health
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

`make infra-backup` 与 `make infra-restore-drill` 只覆盖本地 System PostgreSQL、Infra MinIO 当前对象及根 `.env`，并能在隔离容器中验证恢复；它们不是完整生产备份。生产部署前仍须建立覆盖 Business 引擎及其他持久状态的独立备份方案，落实异机存储、加密、保留策略与定期恢复演练；不能把单次本机导出当作灾备。

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
```

不要在 ADDP 主机执行 `docker volume prune` 或带 `--volumes` 的全局清理；停止中的持久化卷可能被删除。检查卷归属后，只通过各项目的专用生命周期入口处理。

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

## 模块实例运行日志

模块服务正文统一由共享接收器按进程实例写入节点源，再由 Alloy 发送到 Loki；平台系统管理员在“System → 模块管理 → 服务实例 → 查看日志”读取。Go、Python、Gateway 和 Worker 使用同一条链路，应用重启保留旧实例集中日志。首次接入需要重新登录以获取独立日志读取权限。

本指南中的 `docker logs`、`docker compose logs`、`docker service logs` 用于容器入口与日志设施自身诊断，不再作为模块运行日志正文入口。业务尚未登记就初始化失败时，通过受控节点源 `logs/runtime/<module>/<instance_id>/*.jsonl` 排查；页面不会虚构未登记的实例。

部署、计数、保留、备份和验证入口见 [Infra 运行日志说明](../infra/README.md#模块实例运行日志)。首版允许明确缺口，不承诺零丢失、完整重放或存储高可用。

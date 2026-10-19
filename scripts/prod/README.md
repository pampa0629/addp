# Production Deployment Scripts 使用指南

本目录包含 ADDP 生产环境部署和管理脚本。

## 目录

- [start.sh](#startsh) - 生产环境启动脚本
- [stop.sh](#stopsh) - 生产环境停止脚本
- [health-check.sh](#health-checksh) - 健康检查脚本
- [wait-infra.sh](#wait-infrash) - 基础设施就绪等待
- [swarm/](#swarm) - 现存 Swarm 脚本及部署缺口

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

**用途**: 等待 PostgreSQL、Redis、FalkorDB、MinIO、Meilisearch 就绪。生产 `start.sh` 在 Infra 标准入口之后调用本脚本；它不会启动或修复设施，也不替代完整 Infra 状态检查。

### 使用方法

从项目根目录执行：

```bash
bash scripts/prod/wait-infra.sh
```

### 检查机制

- **PostgreSQL**: 在所属容器内执行认证后的 `SELECT 1`，验证实际数据库可查询。
- **Redis**: 在所属容器内执行认证后的 `PING`。
- **FalkorDB**: 等待容器健康状态为 `healthy`。
- **MinIO、Meilisearch**: 先读取本工作区容器的实际宿主机映射，再分别检查 `/minio/health/live` 与 `/health`。

各核心服务的重试等待阈值固定为 90 秒、间隔 2 秒，不含探针自身耗时。集中日志启用时额外报告查询代理、Alloy 与清理器的状态；这些警告不改变本脚本核心服务成功的退出结果，生产启动入口仍单独汇总所选观测能力的失败。

### 环境变量

脚本读取根 `.env` 中的设施连接配置，并通过 Infra 端口工具查询实际映射；当前不提供 `TIMEOUT` 或 `RETRY_INTERVAL` 参数。配置及映射规则见[端口规范](../../docs/spec/addp端口分配.md)。

### 示例

```bash
# 只读核对核心设施与可选观测状态、实际端口和归属
bash scripts/infra/status.sh
```

设施确需初始化或应用配置时，由部署方运行 `bash scripts/infra/up.sh`，按[Infra 生命周期指南](../infra/docs/生命周期脚本.md)处理。

---

## swarm/

**当前状态**：仓库保留 Swarm 初始化、部署和状态脚本，但它们尚未与现行容器配置、生命周期和发布验收闭合。服务器部署使用本文 `start.sh` 的 Compose 流程；高可用、Worker 双副本和零停机更新均不能据这些脚本认定已实现或已验收。

### Swarm 脚本

| 脚本 | 当前行为与边界 |
| --- | --- |
| [init.sh](swarm/init.sh) | 初始化单个 Swarm；已激活时可交互选择重建。重建分支会尝试移除当前 Swarm 的全部 Stack，再强制离开 Swarm，操作范围不限于 ADDP |
| [deploy.sh](swarm/deploy.sh) | 对两份根 Compose 配置执行同一条 Stack 部署命令。“Full / System Only”只改变 `COMPOSE_PROFILES`，当前配置没有相应 profiles，未形成独立的 System 服务清单 |
| [status.sh](swarm/status.sh) | 列出当前 Swarm 的节点、服务、Stack、任务和资源信息；副本状态不能替代 Backend readiness 或任务处理验收 |

部署脚本只检查配置文件是否存在，不调用 Infra 标准初始化入口；缺少 `.env` 时复制模板后退出，已有 `.env` 也未由脚本加载到 Stack 插值环境。提交部署后固定等待 10 秒，再列出服务，未等待平台或 Runtime 健康。

### 使用方法

先按上文 Compose 标准入口部署。对于已存在、由部署方管理的 Swarm，可在其 Manager 节点只读核对 ADDP Stack 状态：

```bash
docker stack services addp
docker service ps addp_transfer-bounded-worker --no-trunc
```

Swarm 的启用、多副本与滚动更新需要先明确部署拓扑、配置和凭据注入、迁移与 Worker 停领顺序、持久化及故障恢复边界，再完成实际验收。

### Swarm vs Compose

| 核对项 | 当前 Compose 标准入口 | 现存 Swarm 脚本 |
| --- | --- | --- |
| 生命周期与配置 | Infra、平台、内置 Runtime 独立管理 | 将平台与 Runtime 配置合并提交给一个 Stack，未衔接 Infra 初始化 |
| 配置格式 | 根文件使用 Compose Specification 与 Backend 健康依赖条件 | `docker stack` 使用旧版 Compose v3 格式，当前健康依赖写法未通过 Stack 配置解析 |
| 副本与更新策略 | 按当前服务配置启动 | Meta Worker、Transfer bounded Worker 的 `deploy` 策略仍被注释；其中旧副本值为 1，不能作为“双副本”依据 |
| 就绪与可靠性 | 标准启动入口等待容器状态；业务功能仍需单独验收 | 固定等待和服务列表不证明高可用、故障切换或零停机更新 |

Docker 的格式及部署前提见[官方 Stack 部署说明](https://docs.docker.com/engine/swarm/stack-deploy/)；滚动更新机制见[官方更新说明](https://docs.docker.com/engine/swarm/swarm-tutorial/rolling-update/)。ADDP 高可用与故障切换的认证边界遵循[T5 发布认证规范](../../docs/spec/testing/T5-发布认证规范.md)，通用调度能力不能代替产品验收。

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

# 6. 通过启动输出中的 ADDP_PUBLIC_ORIGIN 访问 Console 并验证所需业务功能
# 上述健康检查仅核对容器状态与 Nginx /health，不代替业务验收
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

保留启动终端的原始报错，按 Infra、System、平台、Runtime 阶段定位失败位置。以下以 System 启动失败为例，只读检查核心设施和对应容器：

```bash
bash scripts/infra/status.sh
docker compose -f docker-compose.yml ps -a system-backend
```

容器入口诊断见[查看日志](#查看日志)，已登记模块的正文见[模块实例运行日志](#模块实例运行日志)。区分初始化耗时、迁移失败、凭据不匹配与依赖不可达，再修正原因；不因单个业务容器超时就重启整套 Infra。可选观测失败与核心设施状态分别处理，详见[Infra 故障排查](../infra/docs/故障排查.md)。

### 问题 2: 端口冲突

平台容器只向宿主机发布 Nginx 入口。先核对启动报错中的绑定地址和部署配置的 `NGINX_PORT`；已创建的 Nginx 可查询实际映射，再对照宿主机监听者：

```bash
docker compose -f docker-compose.yml port nginx 80
lsof -nP -iTCP -sTCP:LISTEN
```

结合 Compose project、service 和工作区标签确认归属，由占用者所属的生命周期入口处理。需调整本部署入口时修改 `NGINX_PORT`，并同步实际对外 `ADDP_PUBLIC_ORIGIN` 后沿标准入口生效；生产入口冲突不会自动改选端口。Backend 的 8180 等端口属于容器内部通信，不能按开发直连地址处理。Infra 端口查询与归属规则见[端口规范](../../docs/spec/addp端口分配.md)。

### 问题 3: 健康检查失败

运行生产健康检查，再按失败服务核对对应 Compose 项目；例如 Manager Backend：

```bash
bash scripts/prod/health-check.sh
docker compose -f docker-compose.yml ps -a manager-backend
```

该检查核对容器状态和实际 Nginx `/health`，不能代替模块业务功能验收。按[查看日志](#查看日志)定位容器入口问题，模块正文使用 System 的实例日志入口；配置问题对照所属模块配置规范核对，不导出整份容器环境。

修正原因后，平台重启使用本文[停止与启动流程](#stopsh)。涉及数据库结构变化时，遵循[Backend 迁移与 Worker 启动顺序](../../docs/spec/addp开发服务生命周期与构建身份规范.md#模块迁移与-worker-启动顺序)，先停止相关 Worker 领取任务并妥善结束在途任务，再迁移和恢复。

### 问题 4: 数据丢失

`make infra-backup` 与 `make infra-restore-drill` 只覆盖本地 System PostgreSQL、Infra MinIO 当前对象及根 `.env`，并能在隔离容器中验证恢复；它们不是完整生产备份。生产部署前仍须建立覆盖 Business 引擎及其他持久状态的独立备份方案，落实异机存储、加密、保留策略与定期恢复演练；不能把单次本机导出当作灾备。

---

## 维护命令

### 查看日志

模块运行正文与授权入口见[模块实例运行日志](#模块实例运行日志)。下列命令用于容器入口、设施和 Runtime 自身诊断；按失败阶段选择对应项目与服务：

```bash
# Infra 容器诊断（以 PostgreSQL 为例）
docker compose -f docker-compose.infra.yml logs --tail=100 postgres

# 平台容器入口诊断
docker compose -f docker-compose.yml logs --tail=100 system-backend

# Runtime 自身诊断
docker compose -f docker-compose.runtimes.yml logs --tail=100 duckdb-engine
```

### 查看状态

```bash
# Infra 状态、实际端口与观测能力诊断
bash scripts/infra/status.sh

# 平台与内置 Runtime，包括已退出容器
docker compose -f docker-compose.yml ps -a
docker compose -f docker-compose.runtimes.yml ps -a

# 以下资源、磁盘统计覆盖当前 Docker Engine
docker stats
docker system df
```

### 清理资源

先通过 `docker system df` 核对占用，确认当前部署和已保留回滚版本需要的镜像后，再按明确的镜像 ID 清理；全局 `docker image prune -a` 可能移除暂未被容器使用的回滚镜像。

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

1. **使用统一入口**: 通过启动输出中的 `ADDP_PUBLIC_ORIGIN` 访问；域名、HTTPS 或上级代理部署显式配置用户实际访问的 origin
2. **备份恢复**: 上线前建立覆盖 PostgreSQL、MinIO、部署配置和密钥材料的备份策略，并定期完成恢复演练
3. **监控日志**: 使用 `health-check.sh` 定期检查服务健康
4. **渐进式更新**: 先在测试环境验证，再部署到生产环境
5. **可靠性边界**: 按实际部署与验收证据说明可用性；Swarm 脚本的现状和缺口见[上文](#swarm)
6. **资源限制**: 在 docker-compose.yml 中配置 CPU 和内存限制
7. **安全加固**: 修改默认密码、启用 HTTPS、配置防火墙

---

## 模块实例运行日志

模块服务正文统一由共享接收器按进程实例写入节点源，再由 Alloy 发送到 Loki；平台系统管理员在“System → 模块管理 → 服务实例 → 查看日志”读取。Go、Python、Gateway 和 Worker 使用同一条链路，应用重启保留旧实例集中日志。首次接入需要重新登录以获取独立日志读取权限。

本指南中的 `docker logs`、`docker compose logs`、`docker service logs` 用于容器入口与日志设施自身诊断，不再作为模块运行日志正文入口。业务尚未登记就初始化失败时，通过受控节点源 `logs/runtime/<module>/<instance_id>/*.jsonl` 排查；页面不会虚构未登记的实例。

部署、计数、保留、备份和验证入口见 [Infra 运行日志说明](../infra/docs/运行日志.md#模块实例运行日志)。首版允许明确缺口，不承诺零丢失、完整重放或存储高可用。

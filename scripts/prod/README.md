# Production Deployment Scripts 使用指南

本目录包含 ADDP 生产环境部署和管理脚本。

## 目录

- [start.sh](#startsh) - 生产环境启动脚本
- [stop.sh](#stopsh) - 生产环境停止脚本
- [health-check.sh](#health-checksh) - 容器状态、回环入口探测与验收范围
- [wait-infra.sh](#wait-infrash) - 基础设施就绪等待
- [swarm/](#swarm) - 现存 Swarm 脚本及部署缺口
- [健康检查失败排查](#问题-3-健康检查失败) · [状态与资源查看](#查看状态) · [测试与验收规范](../../docs/spec/addp测试与验收规范.md)

---

## start.sh

**用途**: 按正确顺序启动完整的 ADDP 生产环境（基础设施 + 后端 + 前端 + Console）

### 使用方法

```bash
# 从项目根目录启动
cd /path/to/addp
./scripts/prod/start.sh
```

### 启动流程

1. 调用 `setup-env.sh` 初始化或校验根 `.env`；配置要求与已有凭据边界见[首次部署](#首次部署)。校验失败时停止启动。
2. 启动基础设施并等待其就绪。
3. 启动 System Backend，使用 Docker Compose 容器健康检查等待就绪。
4. 启动 `addp-platform` 中全部平台服务，并等待有健康检查的容器变为 healthy、其他容器进入 running。
5. 启动 `addp-runtimes` 中全部内置 Runtime，再列出平台与 Runtime 状态；本机入口探测另按[健康检查说明](#health-checksh)执行。

容器内部端口固定，平台 Compose 仅将 Nginx 的 `NGINX_PORT` 发布到宿主机。启动脚本报告统一访问地址；`ADDP_PUBLIC_ORIGIN` 留空时为 `http://localhost:<NGINX_PORT>`。域名、HTTPS 或上级反向代理部署须填写用户实际访问的 `ADDP_PUBLIC_ORIGIN`。

### 示例

首次部署先按[首次部署流程](#首次部署)准备配置；服务器重启后沿用上述启动入口。需要保存本次启动终端输出时使用：

```bash
(
  set -o pipefail
  ./scripts/prod/start.sh 2>&1 | tee startup.log
)
```

`startup.log` 记录部署脚本的终端输出，管道保留启动失败的退出码；模块正文使用[实例日志入口](#模块实例运行日志)。

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

重启沿用上述停止范围，按停止、启动的顺序执行：

```bash
./scripts/prod/stop.sh
./scripts/prod/start.sh
```

---

## health-check.sh

从项目根目录执行：

```bash
bash scripts/prod/health-check.sh
```

该入口单次核对容器状态并探测本机 Nginx HTTP 路径，不承担启动等待或业务验收。实际逻辑见 [health-check.sh](health-check.sh)。

| 检查层 | 实际范围与判定 |
| --- | --- |
| 服务枚举 | 分别读取 `docker-compose.infra.yml`、`docker-compose.yml`、`docker-compose.runtimes.yml` 的 `config --services` 输出；不叠加生产启动入口生成的观测等覆盖文件，不能据此认定本次部署全部可选组件已覆盖 |
| 容器状态 | 对枚举服务使用 `ps -a -q` 找容器，优先读取 `.State.Health.Status`，无健康探针时读取 `.State.Status`；healthy 或 running 通过，缺容器或其他状态记失败。running 不等于模块 Ready 或业务可用 |
| 一次性初始化 | 仅 Infra 的 `redpanda-init` 允许 exited 且退出码 0，表示初始化完成；其他已退出容器不按此例外通过 |
| Nginx 入口 | 从平台 Compose 查询 Nginx 的实际发布端口，再以 `curl -fsS` 请求 `http://127.0.0.1:<端口>/health`；无映射或请求失败记失败，不按模块宿主机固定端口探测 |

Nginx `/health` 由[根配置](../../nginx/nginx.conf)直接返回，脚本也不校验响应正文。该回环探测不使用 `ADDP_PUBLIC_ORIGIN`，不能证明公开域名、HTTPS 证书、上级代理、浏览器入口、认证或数据服务可用；这些须从实际消费入口单独验证。

**失败与覆盖边界**：缺失容器、状态不通过或入口请求失败会累积非零结果；Docker 查询或 inspect 错误也可能提前中断。脚本不轮询等就绪，不自动修复；curl 未配置显式超时，不能套用 [wait-infra.sh](#wait-infrash) 的等待预算。

服务枚举通过进程替换读取，脚本未单独检查 `config --services` 的退出结果；枚举失败时可能漏查服务。须结合原始报错、三份配置的预期服务及实际输出核对覆盖，不能只凭最终退出码推定完整检查。实际状态与失败排查分别见[查看状态](#查看状态)、[健康检查失败](#问题-3-健康检查失败)；可选观测状态还须按[Infra 故障排查](../infra/docs/故障排查.md)核对。

容器检查、回环 HTTP 探测、公开入口和业务结果分别记录。标准门禁、隔离 T4 与 T5 认证遵循[测试与验收规范](../../docs/spec/addp测试与验收规范.md)；未执行、失败或未覆盖项不能计为通过，健康检查成功不代表业务就绪、日志完整或升级/恢复验收通过。

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

先准备可用的 Docker、Docker Compose 和 OpenSSL，将目标版本的源码或部署包放入服务器部署目录，并按 [Build 指南](../build/README.md#build-imagessh)准备对应平台与版本的镜像。

生产配置只使用根 `.env`，通过 [setup-env.sh](setup-env.sh) 从唯一模板 `.env.example` 初始化。文件缺失时生成 `ENV=production` 和独立随机 Secret；已有文件须通过生产配置校验，脚本仅收紧文件权限，不自动补齐、覆盖或轮换已有 Secret。配置规则见[环境变量参考](../../docs/spec/configuration/环境变量参考.md#根目录-env)。

```bash
# 1. 进入服务器上的项目或部署包根目录
cd /opt/addp

# 2. 初始化或校验生产配置
bash scripts/prod/setup-env.sh

# 3. 核对部署参数，保留已有有效凭据
vi .env  # 核对 Registry、镜像版本、公共 URL、反向代理及 Business 连接信息

# 4. 标准启动会再次校验配置，再启动 Infra、平台与 Runtime
./scripts/prod/start.sh

# 5. 核对容器状态与统一入口
./scripts/prod/health-check.sh
```

检测到已有 `addp-infra_postgres_data` 卷而根 `.env` 缺失时，初始化会拒绝重建，须从原部署恢复配置。配置校验失败时先修正对应问题；不要通过重新生成密码连接旧数据卷。

通过启动输出中的 `ADDP_PUBLIC_ORIGIN` 访问 Console，再以合法身份验证所需业务功能；本机探测与公开入口的分工统一见[健康检查与验收范围](#health-checksh)。

### 更新部署

更新前先核对目标 Linux 平台、所需服务和版本，按 [Build 指南](../build/README.md#build-imagessh)构建或准备对应镜像。Model3D 每次只构建一个原生 Linux 平台；SuperMap 要求已准备的私有 Linux ARM64 基础镜像。多架构发布仅对已确认支持的服务使用 `--services` 配合 `--multi-arch`，其他 Runtime 按各自平台规则准备。

部署引用须匹配已发布镜像的 Registry 与版本标签；启动前核对根 `.env` 中的 `REGISTRY`、`IMAGE_TAG` 及 Runtime 平台配置，不能仅用构建时的临时环境变量推断部署版本。

```bash
# 1. 更新部署文件
cd /opt/addp
git pull

# 2. 按上文准备目标平台镜像，核对根 .env 的 Registry、版本与平台
vi .env

# 3. 停止并重新启动平台与内置 Runtime（保留数据卷）
./scripts/prod/stop.sh
./scripts/prod/start.sh

# 4. 检查容器状态与统一入口
./scripts/prod/health-check.sh
```

升级后的业务功能与数据结构仍须独立验收，检查分工与标准入口统一见[健康检查说明](#health-checksh)。

### 回滚部署

切换代码或镜像不会恢复数据库、对象存储及其他持久状态。ADDP 默认不兼容旧实现；现行迁移规范禁止旧进程覆盖较新的 schema，Worker 也须通过模块及公共 schema 版本校验，详见[迁移与 Worker 启动规范](../../docs/spec/addp开发服务生命周期与构建身份规范.md#模块迁移与-worker-启动顺序)。

1. 先核对目标版本的镜像平台、部署配置与持久化状态要求。需要恢复数据时，先核验备份覆盖、恢复点和演练证据，确定已验证的生产恢复方案；范围要求见[备份恢复边界](#问题-4-数据丢失)。`make infra-restore-drill` 只在隔离容器演练，不是生产数据回写入口，工具范围见[Infra 备份专题](../infra/docs/备份恢复与搜索迁移.md#本地-infra-备份与隔离恢复演练)。
2. 涉及数据结构或持久状态恢复时，先停止相关 Worker 领取任务并妥善结束在途任务；执行回退均须按[标准停止入口](#stopsh)停止平台与内置 Runtime。该入口保留 Infra、Business 和数据卷，不执行其数据恢复；这些 owner 的停服与恢复须按对应方案单独处理。
3. 仅在持久状态满足目标版本要求后，切换匹配的部署制品与配置，核对根 `.env` 的 `REGISTRY`、`IMAGE_TAG`、Runtime 平台及与数据匹配的已有凭据；不通过重新生成 Secret 替代配置恢复。
4. 按[首次部署中的标准入口](#首次部署)启动并检查容器状态与统一入口，再验证所需业务功能和数据结果。回滚结论以[测试与验收规范](../../docs/spec/addp测试与验收规范.md)及实际证据为准，不以容器 healthy 代替完整恢复验收。

---

## 故障排查

[返回故障排查总入口](../../docs/guide/addp常见故障排查.md#按现象查找)

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

按[健康检查说明](#health-checksh)运行标准入口并保留原始结果，区分容器状态、一次性初始化与回环 HTTP 请求失败。按[查看状态](#查看状态)核对对应 Compose 项目及已退出容器，再通过[查看日志](#查看日志)定位容器入口问题；模块正文使用 System 的[实例日志入口](#模块实例运行日志)。配置问题对照所属模块规范核对，不导出整份容器环境。

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

以下命令用于状态和资源诊断，不执行入口请求或业务验收；容器与探测结果按[健康检查范围](#health-checksh)分别核对。

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

| 事项 | 操作入口与边界 |
| --- | --- |
| 配置与对外访问 | 根 `.env` 初始化和 Secret 处理见[首次部署](#首次部署)，变量归属与生效见[配置规范](../../docs/spec/addp配置介绍.md)。按实际入口配置 `ADDP_PUBLIC_ORIGIN`、HTTPS/上级代理和防火墙，访问要求见[启动流程](#启动流程)。 |
| 备份恢复 | 生产备份范围、密钥保护与恢复演练要求见[数据丢失与备份边界](#问题-4-数据丢失)。本地 Infra 导出不是完整生产备份；工具与覆盖范围见[Infra 备份专题](../infra/docs/备份恢复与搜索迁移.md#本地-infra-备份与隔离恢复演练)。 |
| 健康与日志 | 检查范围、失败判定与业务验收分工统一见[健康检查](#health-checksh)；日志正文、权限和链路见[模块实例日志](#模块实例运行日志)与[Infra 日志专题](../infra/docs/运行日志.md#模块实例运行日志)。 |
| 更新与可靠性 | 先在测试环境验证目标版本，再按[更新部署](#更新部署)操作。可用性和发布结论以[测试与验收规范](../../docs/spec/addp测试与验收规范.md)及实际证据为准，Swarm 缺口见[现状说明](#swarm)。 |
| 资源限制 | 根据实际部署负载配置并验证 CPU、内存限制；配置位置见[平台 Compose](../../docker-compose.yml)与[Runtime Compose](../../docker-compose.runtimes.yml)。 |

---

## 模块实例运行日志

已登记模块正文从“System → 模块管理 → 服务实例 → 查看日志”读取；有效可信未登记来源可从“未登记实例的运行日志”入口选择，复用同一日志抽屉。两类入口均要求 Platform 上下文及 `platform.module_log.read`，默认仅授予平台系统管理员；权限变更导致旧会话失效时须重新登录。完整身份、授权与目录保留规则见[运行日志规范](../../docs/spec/addp模块服务运行日志规范.md#四可信来源目录)。

`docker logs`、`docker compose logs`、`docker service logs` 用于容器入口与日志设施自身诊断；受控节点源 `logs/runtime/<module>/<instance_id>/*.jsonl` 与启动终端输出用于本地诊断，不从路径推断 System 可信身份。未登记不等于 DOWN 或启动失败，目录存在也不代表正文已送达。

应用停止或重启不停止日志 Infra、不删除日志卷；部署、计数、保留、备份和验证入口见 [Infra 运行日志说明](../infra/docs/运行日志.md#模块实例运行日志)。首版允许明确缺口，不承诺零丢失、完整重放或存储高可用。

# ADDP 本地 Docker 部署脚本

本目录包含用于本地 Docker Compose 部署测试的脚本。运行模式与生命周期归属见[开发与容器部署](../docs/开发与容器部署.md)。

阅读导航：[停止范围](#2-stopsh---停止服务) · [数据持久化与恢复](#数据持久化) · [故障排查](#故障排查流程) · [模块实例日志](#模块实例运行日志)。

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

首次启动前须在本机备齐本次部署所需的 Linux 镜像，架构、`REGISTRY` 和 `IMAGE_TAG` 与部署配置一致。源码构建使用 `make build`、`make build-images`；产物要求、平台限制和验证/推送边界统一见 [Build 指南](../build/README.md#build-imagessh)。

启动前的镜像预检以 [start.sh](start.sh) 的 `check_images` 清单为准；发现本机镜像缺失即退出并提示构建，该预检不自动构建应用镜像。镜像引用的默认值见[镜像来源](#镜像来源)。

## 使用方法

### 快速启动

```bash
# 启动完整 ADDP 平台
bash scripts/local/start.sh

# 访问服务
# - 使用启动输出中的 ADDP_PUBLIC_ORIGIN；未设置时为 http://localhost:<实际 Nginx 发布端口>
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
   - 数据库/MinIO 初始化及 Meilisearch API 健康检查
   - 健康检查等待
4. ✓ 启动应用层 (所有后端、前端、Worker、Gateway、Nginx)
5. ✓ 等待 Compose 容器健康检查通过
6. ✓ 显示访问地址和管理命令

**幂等性**: 可重复执行，使用 `docker compose up -d --wait --remove-orphans` 确保服务存在且就绪，并清理已删除的旧服务容器。

**镜像检查**：出现 `Missing images` 时，按[镜像准备](#2-构建镜像)核对并补齐对应镜像，再运行本节启动命令。

#### 2. stop.sh - 停止服务

**功能**: 先停止内置 Runtime，再停止平台容器。

```bash
# 停止 Runtime 与平台（保留 Infra 和数据卷）
bash scripts/local/stop.sh

# 额外停止 Infra（仍保留数据卷，Business 不在范围内）
bash scripts/local/stop.sh --all
```

`--all` 通过 `scripts/infra/down.sh` 停止基础设施；两种方式都保留数据卷，Business 由所属入口管理。卷与恢复边界统一见[数据持久化](#数据持久化)。

Local 入口按两份 Compose 配置执行 `down`。操作前须核对 Compose project、service 和 `com.docker.compose.project.working_dir`，不能只凭容器名称或端口判断归属；规则与 Infra 停止入口的工作区核验范围见[项目隔离规范](../infra/docs/部署与项目隔离.md#执行顺序)及[容器归属](../infra/docs/部署与项目隔离.md#容器命名规范)。

各阶段顺序执行，途中失败时前序服务可能已经停止；按 Runtime、平台及所选 Infra 的实际状态复核结果，未执行或失败的阶段不能计为停止完成。

#### 3. status.sh - 查看状态

**功能**: 显示所有服务的详细状态

```bash
bash scripts/local/status.sh
```

**输出内容**:
- 基础设施层与应用层容器状态、健康状态及实际端口映射
- Nginx 统一入口 URL（由实际映射与 `ADDP_PUBLIC_ORIGIN` 得到）
- 资源使用情况与管理命令

#### 4. restart.sh - 重启服务

**功能**: 重启 ADDP 服务 (停止 + 启动)

```bash
# 重启应用层
bash scripts/local/restart.sh

# 重启所有服务 (包括基础设施)
bash scripts/local/restart.sh --all
```

**实现**: 顺序调用 `stop.sh` 和 `start.sh`，中间等待 3 秒；停止失败时不执行启动。`--all` 的范围沿用[停止说明](#2-stopsh---停止服务)，该等待时间不代替实际状态与业务验收。

## 架构说明

ADDP 本地部署采用**分层架构**:

### 基础设施层 (docker-compose.infra.yml)

基础设施由 `addp-infra` Compose project 管理，容器内端口与宿主机首选映射见 [Infra 端口映射](../infra/docs/部署与项目隔离.md#端口映射)。首选值不代表运行地址；连接前执行 `bash scripts/infra/status.sh`，核对当前工作区的实际映射，不从示例或 `.env` 首选值推断端口。

### 平台层 (docker-compose.yml)

Console、各模块 Frontend、Backend 和 Gateway 使用 Docker 网络内的服务名与固定容器端口。平台层只将 Nginx 的 `NGINX_PORT` 发布到宿主机，浏览器通过这个统一入口访问模块页面和 `/api/`。

### 内置 Runtime (docker-compose.runtimes.yml)

内置计算与 Notebook Runtime 由 `addp-runtimes` 独立管理，通过外部 `addp-network` 连接平台和 Infra；不发布宿主机端口。`scripts/local/start.sh` 在平台就绪后启动 Runtime，`stop.sh` 先停止 Runtime 再停止平台。

### 镜像来源

平台与内置 Runtime 镜像的 Registry 和标签由 `REGISTRY`、`IMAGE_TAG` 配置，默认引用格式如下：
```
${REGISTRY:-localhost:5001}/addp-<service>:${IMAGE_TAG:-latest}
```

## 与其他部署模式对比

| 部署模式 | 脚本目录 | 用途 | 运行方式 | 镜像来源 |
| --- | --- | --- | --- | --- |
| **开发模式** | `scripts/dev/` | 本地源码开发与调试 | Go 服务、Node 前端和 Python Runtime 使用原生进程；SuperMap 使用 Docker | 源码与原生依赖；容器依赖按各自配置取镜像 |
| **本地 Docker** | `scripts/local/` | 本地容器化测试 | Docker Compose 管理平台与内置 Runtime | `REGISTRY` 可配置，默认 `localhost:5001`；版本由 `IMAGE_TAG` 指定 |
| **生产环境** | `scripts/prod/` | 服务器容器部署 | Docker Compose 管理平台与内置 Runtime | 按部署配置指定 Registry 与版本标签 |

各 Runtime 的开发方式与平台限制见[引擎运行方式](../../engines/README.md#本地开发运行方式)。原生开发不改变产品部署使用正式容器镜像的路线；Infra 与 Business 的容器和网络由各自入口管理。

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

核对 `Missing images` 列出的服务、Registry 和标签，再按[镜像准备](#2-构建镜像)处理。源码构建和单平台验证参数见 [Build 指南](../build/README.md#build-imagessh)。

### 3. 端口被占用

先运行 `bash scripts/infra/status.sh` 查询 Infra 实际映射。Infra 本地首次映射冲突由标准入口自动避让，已运行的本工作区容器复用原映射；不要把 Business PostgreSQL 的 5433 当成平台元数据库端口。

平台容器只发布 Nginx 入口。通过以下命令查询其实际映射，再核对监听者与 Compose project、service、工作区归属：

```bash
docker compose -f docker-compose.yml port nginx 80
```

Nginx 发布端口由部署配置指定，冲突时不自动漂移。核实占用者后，由其所属生命周期入口处理，或调整本部署的 `NGINX_PORT` 与对外 `ADDP_PUBLIC_ORIGIN`，再通过标准入口应用配置；不要按端口盲目终止进程。详见[端口规范](../../docs/spec/addp端口分配.md)。

### 4. 容器启动失败

```bash
# 查看容器日志
docker compose -f docker-compose.yml logs system-backend

# 查看所有容器状态
bash scripts/local/status.sh

# 修正原因后通过标准入口重启平台与 Runtime，保留数据卷
bash scripts/local/restart.sh
```

### 5. 健康检查超时

**现象**: 启动时看到 "⚠️ health check timeout"

**排查**: 先核对实际容器状态和启动失败原因；首次迁移耗时、依赖未就绪、配置或进程异常均需结合证据区分，不能把超时直接视为初始化较慢。

**解决**:
```bash
# 等待一段时间后检查状态
bash scripts/local/status.sh

# 查看容器入口诊断；模块正文通过 System 服务实例页读取
docker compose -f docker-compose.yml logs -f system-backend
```

## 数据持久化

停止保留范围见[stop.sh](#2-stopsh---停止服务)。持久化状态按实际挂载盘点：Infra 具名卷清单、逻辑名与物理前缀统一见[Infra 数据持久化](../infra/docs/部署与项目隔离.md#数据持久化)；模块日志源及 Runtime 工作、数据目录还包含宿主机挂载，具体输入见[平台 Compose](../../docker-compose.yml)与[Runtime Compose](../../docker-compose.runtimes.yml)。

保留卷和宿主机数据不等于已有完整备份或恢复成功。备份覆盖、标准入口及证据要求见[本地 Infra 备份与隔离恢复演练](../infra/docs/备份恢复与搜索迁移.md#本地-infra-备份与隔离恢复演练)：默认覆盖 System PostgreSQL、Infra MinIO 当前对象及根 `.env`，不覆盖全部持久状态或 Business；`make infra-restore-drill` 只在一次性容器验证恢复，不向现有部署回写数据，也不代表完整平台恢复验收。

需要重建数据时，先核验可恢复备份和目标 Compose project，再按[Infra 删除卷规则](../infra/docs/部署与项目隔离.md#执行顺序)走专用交互确认；`--force` 不跳过本地卷删除确认。Docker Desktop 或 Docker CLI 直接删除 volume 不受脚本保护。旧 Meilisearch 卷仍按[迁移与回滚流程](../infra/docs/备份恢复与搜索迁移.md#旧-meilisearch-卷迁移与回滚)保留和处理，普通启动不承担升级。

停止结果、容器健康、隔离恢复演练与业务数据验收须按[测试与验收规范](../../docs/spec/addp测试与验收规范.md)分别记录；未运行、失败或未覆盖项不能计为恢复通过。

## 高级用法

### 查看实时日志

模块正文使用下文[模块实例运行日志](#模块实例运行日志)入口；以下为容器入口诊断，可按需附加 `--tail=100` 限制输出。

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

PostgreSQL、Redis 等设施的连接诊断统一见[Infra 故障排查](../infra/docs/故障排查.md)，按所属 Compose 服务和实际凭据操作。调试平台容器时使用当前项目的服务名：

```bash
docker compose -f docker-compose.yml exec system-backend sh
```

### 手动管理服务

设施初始化或配置应用按[Infra 生命周期指南](../infra/docs/生命周期脚本.md)执行；本地启动、重启与停止统一使用上文[脚本说明](#脚本说明)，操作前核对项目和工作区归属。数据重建与恢复按[持久化边界](#数据持久化)处理。

只读查看单个服务状态，例如：

```bash
docker compose -f docker-compose.yml ps -a system-backend
```

## 性能优化建议

1. **资源分配**: 确保 Docker 分配足够资源
   - macOS: Docker Desktop → Settings → Resources
   - 建议: 至少 4 CPU cores, 8GB RAM

2. **镜像缓存**: 使用本地 registry 缓存镜像,避免重复构建

3. **数据卷**: 使用 volumes 而非 bind mounts,性能更好

4. **网络**: 平台、内置 Runtime 与 Infra 按既有定义使用 `addp-network`；Business 网络独立管理，具体边界见[项目隔离说明](../infra/docs/部署与项目隔离.md#docker-compose-项目)

## 相关文档

- [scripts/build/README.md](../build/README.md) - 镜像构建脚本说明
- [scripts/infra/README.md](../infra/README.md) - 基础设施管理脚本
- [scripts/dev/README.md](../dev/README.md) - 开发模式脚本
- [scripts/prod/README.md](../prod/README.md) - 生产部署脚本
- [CLAUDE.md](../../CLAUDE.md) - 项目整体架构文档
- [docker-compose.infra.yml](../../docker-compose.infra.yml) - 基础设施层配置
- [docker-compose.yml](../../docker-compose.yml) - 平台层配置
- [docker-compose.runtimes.yml](../../docker-compose.runtimes.yml) - 内置 Runtime 配置

## 注意事项

1. **端口冲突**: 核对 Nginx 部署入口与 Infra 实际映射，处理方式见[端口被占用](#3-端口被占用)
2. **资源消耗**: 完整平台需要较多资源,建议至少 8GB RAM
3. **首次启动**: 首次启动需要初始化数据库,可能需要 1-2 分钟
4. **网络边界**: 内部服务通过 Docker 网络互访，对外只发布约定入口；网络连接本身不代替访问鉴权
5. **数据备份**: 按[持久化与恢复边界](#数据持久化)盘点覆盖范围和恢复证据

## 故障排查流程

1. 运行 `bash scripts/local/status.sh`，按 Infra、平台与 Runtime 区分失败位置。
2. 查看对应实例日志或容器入口诊断，并按上文[常见问题](#常见问题)核对镜像、端口与归属。
3. 修正原因后，按[手动管理服务](#手动管理服务)中的标准入口应用；容器状态通过后，再验证所需业务功能。

## 模块实例运行日志

已登记模块正文从“System → 模块管理 → 服务实例 → 查看日志”读取；有效可信未登记来源可从“未登记实例的运行日志”入口选择，复用同一日志抽屉。两类入口均要求 Platform 上下文及 `platform.module_log.read`，默认仅授予平台系统管理员；权限变更导致旧会话失效时须重新登录。完整身份、授权与目录保留规则见[运行日志规范](../../docs/spec/addp模块服务运行日志规范.md#四可信来源目录)。

`docker logs`、`docker compose logs`、`docker service logs` 用于容器入口与日志设施自身诊断；受控节点源 `logs/runtime/<module>/<instance_id>/*.jsonl` 与启动终端输出用于本地诊断，不从路径推断 System 可信身份。未登记不等于 DOWN 或启动失败，目录存在也不代表正文已送达。

应用与 Infra 的停止范围见[停止说明](#2-stopsh---停止服务)，日志卷沿既定数据保留规则处理；部署、计数、保留、备份和验证入口见 [Infra 运行日志说明](../infra/docs/运行日志.md#模块实例运行日志)。首版允许明确缺口，不承诺零丢失、完整重放或存储高可用。

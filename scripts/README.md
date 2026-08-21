# ADDP Scripts 使用指南

本目录包含 ADDP 项目的所有脚本工具，涵盖基础设施管理、开发调试、编译构建、本地部署和生产部署等全流程。

## 核心原则

所有脚本遵循以下 7 个核心原则：

1. **单一职责**: 同样功能只在一处实现，其他地方调用
2. **适应性**: 适应不同环境（OS、CPU 架构），脚本自动适配
3. **清晰明了**: 一看就懂的结构和命名
4. **可重复执行**: 幂等性，多次执行不会破坏系统
5. **易用性**: 用户无需了解技术细节，按顺序执行即可
6. **分散和集中**: 模块相关配置分散，整体管理脚本集中
7. **敢于删除**: 删除重复或无用的内容，避免违反单一职责原则

---

## 目录结构

```
scripts/
├── infra/          # 一、基础设施管理
├── dev/            # 二、本地开发使用
├── build/          # 三、编译和构建
│   ├── compile.sh      # 1) 编译 - go build 生成二进制文件
│   ├── build-images.sh # 2) 构建 - docker build 生成镜像
│   └── package.sh      # 3) 打包 - docker save/push 到磁盘或镜像仓库
├── local/          # 四、本地 Docker 部署
├── prod/           # 五、生产服务器部署
├── registry/       # Docker Registry 管理
├── test/           # 测试门禁与发布认证
└── utils/          # 通用工具脚本
```

### ArcGIS 开放格式集成门禁

真实 Access/PGeo 样本和 Oracle Spatial bounded 导入使用单一入口：

```bash
make test-arcgis-open-formats
```

该门禁要求 GeoPython 容器和业务 Oracle 已启动；默认读取 `business/nfs/data/arcgis` 下的验收样本，也可通过 `ADDP_ARCGIS_PGEO_FIXTURE`、`ADDP_ARCGIS_ACCESS_FIXTURE`、`ADDP_ARCGIS_PGEO_MATRIX_FIXTURE` 指定其他绝对路径。门禁只执行验收，不负责启动或重启服务。

---

## 一、基础设施管理 (infra/)

**用途**: 管理 ADDP 的基础设施容器（PostgreSQL、Redis、MinIO、Meilisearch）

### 核心脚本

| 脚本 | 功能 | 使用场景 |
|------|------|---------|
| `up.sh` | 启动基础设施 + 自动初始化 | 首次启动、重启基础设施 |
| `down.sh` | 停止基础设施 | 维护、完全停止 |
| `status.sh` | 查看服务状态 | 健康检查、故障排查 |
| `init-db.sh` | 初始化数据库 schema | 数据库重置、清理 |
| `init-minio.sh` | 初始化 MinIO buckets | MinIO 重置 |
| `init-postgresql.sh` | 安装 PostgreSQL 扩展 | PostGIS + pgvector 安装 |
| `init-meilisearch.sh` | 初始化 Meilisearch 索引 | 搜索引擎初始化 |
| `init-redis.sh` | 初始化 Redis 配置 | Redis 验证、清理 |

### 快速使用

```bash
# 启动基础设施（自动完成所有初始化）
bash scripts/infra/up.sh

# 查看状态
bash scripts/infra/status.sh

# 停止基础设施
bash scripts/infra/down.sh
```

### 特性

- ✅ **一键启动**: `up.sh` 自动完成镜像拉取、容器启动、健康检查、所有初始化
- ✅ **跨平台支持**: 自动检测 x86_64/ARM64 架构并选择合适的 PostgreSQL 镜像
- ✅ **模块化资源隔离**: PostgreSQL schema、MinIO bucket、Redis key、Meilisearch index 均按模块命名
- ✅ **幂等性**: 所有脚本可重复执行，不会破坏已有数据

详见: [infra/README.md](infra/README.md)

---

## 二、本地开发使用 (dev/)

**用途**: 用于日常开发调试，直接运行 Go 和 npm 进程（非容器化）

### 核心脚本

| 脚本 | 功能 | 使用场景 |
|------|------|---------|
| `start.sh` | 启动完整开发环境（后台运行） | 日常开发启动 |
| `stop.sh` | 停止所有开发服务 | 开发结束、切换分支 |
| `restart.sh` | 重启所有服务 | 代码修改后重启 |
| `modtidy.sh` | 清理 Go 模块依赖 | 依赖冲突、切换分支 |

### 快速使用

```bash
# 一键启动开发环境（基础设施 + 后端 + 前端）
bash scripts/dev/start.sh

# 跳过 Go 依赖检查（节省 5-10 秒）
SKIP_MODTIDY=1 bash scripts/dev/start.sh

# 修改代码后重启
bash scripts/dev/restart.sh

# 停止所有服务
bash scripts/dev/stop.sh
```

### 启动流程

```
[Step 0] Go 依赖检查 (go mod tidy，可跳过)
  ↓
[Step 1] 启动基础设施 (调用 scripts/infra/up.sh)
  ↓
[Step 2-7] 启动后端服务（按依赖顺序）
  - System Backend (8180)
  - Manager Backend (8081) + Meta Backend (8082)
  - Transfer Backend (8083) + Workers
  - Orchestrator Backend (8084)
  - Gateway (8000)
  ↓
[Step 8] 启动前端服务
  - Console (5170), System (5173), Manager (5174), etc.
```

### 特性

- ✅ **自动依赖启动**: 自动调用 `scripts/infra/up.sh` 启动基础设施
- ✅ **智能跳过**: 基础设施已运行时自动跳过，避免 pgvector 重新编译
- ✅ **健康检查**: 等待每个服务的 `/health` 端点返回 200
- ✅ **日志管理**: 所有日志存储在 `logs/*.log`
- ✅ **PID 追踪**: 存储进程 PID，支持优雅停止

详见: [dev/README.md](dev/README.md)

---

## 三、编译和构建 (build/)

**用途**: 编译二进制文件、构建 Docker 镜像、打包部署

根 `Makefile` 只提供两个标准入口：`make build` 调用 `scripts/build/compile.sh`，`make build-images` 调用 `scripts/build/build-images.sh`。构建事实只维护在这两个脚本中，不得在 Makefile、Workflow 或其他脚本中复制服务清单和构建命令。需要传递参数时分别使用 `BUILD_ARGS="..."` 和 `IMAGE_BUILD_ARGS="..."`。

### 核心脚本

| 脚本 | 功能 | 输入 | 输出 |
|------|------|------|------|
| `compile.sh` | 编译二进制文件 | Go 源码 | `dist/release-{os}-{arch}/` |
| `build-images.sh` | 构建 Docker 镜像 | 二进制文件 | Docker 镜像 |
| `push-images.sh` | 推送镜像到 Registry | Docker 镜像 | 远程 Registry |
| `package.sh` | 打包部署包 | 镜像 + 配置 | 部署包 tarball |

### 1. compile.sh - 编译

```bash
# 容器构建（默认，Linux 二进制用于 Docker）
make build

# 本地开发（编译本机 OS 可执行文件）
make build BUILD_ARGS=--local

# 多架构编译（amd64 + arm64）
make build BUILD_ARGS="--arch both"

# 强制重新编译（忽略缓存）
make build BUILD_ARGS=--force
```

**输出**: `dist/release-{os}-{arch}/` 目录下的二进制文件

### 2. build-images.sh - 构建镜像

```bash
# ⚠️ 必须先运行 make build

# 单架构构建
make build-images

# 多架构构建（amd64 + arm64）
make build-images IMAGE_BUILD_ARGS=--multi-arch

# 指定 Registry
make build-images IMAGE_BUILD_ARGS="--registry harbor.example.com:5001"

# 指定镜像标签
IMAGE_TAG=v1.0.0 make build-images
```

**输出**: Docker 镜像 `{REGISTRY}/addp-{service}:{TAG}`

### 3. push-images.sh - 推送镜像

```bash
# ⚠️ 必须先登录 Registry
docker login  # Docker Hub
# 或: docker login harbor.example.com:5001

# 推送所有镜像到 Docker Hub
bash scripts/build/push-images.sh --registry docker.io/myusername

# 推送指定版本
bash scripts/build/push-images.sh \
  --registry docker.io/myusername \
  --tag v1.0.0

# 仅推送部分服务
bash scripts/build/push-images.sh \
  --registry docker.io/myusername \
  --services system-backend,manager-backend,gateway

# 干运行测试（不实际推送）
bash scripts/build/push-images.sh --registry docker.io/myusername --dry-run
```

**输出**: 镜像推送到远程 Registry（Docker Hub、Harbor 等）

### 4. package.sh - 打包

```bash
# 离线部署包（包含构建脚本，用于服务器上编译）
bash scripts/build/package.sh --mode offline

# 镜像仓库部署包（轻量，仅配置文件）
bash scripts/build/package.sh --mode registry --registry harbor.example.com:5001

# 自动传输到服务器
bash scripts/build/package.sh --server ubuntu@192.168.1.100
```

**输出**:
- Offline Mode: `dist/addp-deploy-offline-{timestamp}.tar.gz`
- Registry Mode: `dist/package-registry-{timestamp}/` 目录

### 完整构建流程

```bash
# 生产发布示例（完整流程）
# 1. 编译多架构二进制
make build BUILD_ARGS="--arch both"

# 2. 构建多架构镜像
IMAGE_TAG=v1.0.0 make build-images IMAGE_BUILD_ARGS="--multi-arch --registry localhost:5001"

# 3. 推送镜像到 Registry
docker login  # 或: docker login harbor.example.com:5001
./scripts/build/push-images.sh --registry docker.io/myorg --tag v1.0.0

# 4. 生成部署包
./scripts/build/package.sh --mode registry --registry docker.io/myorg
```

详见: [build/README.md](build/README.md)

---

## 四、本地 Docker 部署 (local/)

**用途**: 在本地使用 Docker Compose 部署完整 ADDP 平台进行测试

### 核心脚本

| 脚本 | 功能 | 使用场景 |
|------|------|---------|
| `start.sh` | 启动完整 Docker 环境 | 本地容器化测试 |
| `stop.sh` | 停止 Docker 服务 | 停止测试 |
| `status.sh` | 查看容器状态 | 健康检查、资源使用 |
| `restart.sh` | 重启服务 | 更新配置后重启 |

### 前置条件

```bash
# 1. 确保 Docker 运行
open -a Docker  # macOS

# 2. 构建镜像
make build
make build-images
```

### 快速使用

```bash
# 启动完整平台
bash scripts/local/start.sh

# 访问服务
# - Console (推荐): http://localhost:80
# - Gateway:        http://localhost:8000
# - System Backend: http://localhost:8180

# 查看状态和资源使用
bash scripts/local/status.sh

# 停止服务（保留基础设施）
bash scripts/local/stop.sh

# 停止所有服务（包括基础设施）
bash scripts/local/stop.sh --all
```

### 特性

- ✅ **镜像验证**: 自动检查所有必需镜像是否存在
- ✅ **智能启动**: 基础设施已运行时跳过，应用层使用 `docker compose up -d` 确保幂等
- ✅ **健康检查**: 等待关键服务健康检查通过
- ✅ **资源监控**: `status.sh` 显示 CPU、内存使用 Top 5

详见: [local/README.md](local/README.md)

---

## 五、生产服务器部署 (prod/)

**用途**: 在生产服务器上部署和管理 ADDP 平台

### 核心脚本

| 脚本 | 功能 | 使用场景 |
|------|------|---------|
| `start.sh` | 启动生产环境（分步启动） | 生产部署启动 |
| `stop.sh` | 停止生产环境 | 维护、停止服务 |
| `health-check.sh` | 健康检查 | 监控、故障排查 |
| `wait-infra.sh` | 等待基础设施就绪 | 启动流程中的依赖检查 |
| `swarm/` | Docker Swarm 高可用部署 | 生产高可用需求 |

### 快速使用

```bash
# 启动完整生产环境
bash scripts/prod/start.sh

# 访问地址
# - ✨ 推荐: http://localhost （Nginx 统一入口）
# - Console: http://localhost:5170
# - Gateway: http://localhost:8000

# 健康检查
bash scripts/prod/health-check.sh

# 停止服务（保留数据）
bash scripts/prod/stop.sh

# 停止并删除容器
bash scripts/prod/stop.sh --remove
```

### 启动流程

```
[1/5] 基础设施层
  - PostgreSQL, Redis, MinIO, Meilisearch
  - 等待就绪（调用 wait-infra.sh）
  ↓
[2/5] System Backend (8180)
  - IAM、模块目录与 System-owned 配置
  - 等待健康检查通过
  ↓
[3/5] 业务后端服务
  - Manager, Meta, Transfer, Orchestrator, Develop
  - Gateway
  ↓
[4/5] 后端健康检查
  - 最多等待 90 秒
  ↓
[5/5] 前端服务
  - Console, 各模块前端, Nginx
```

### Docker Swarm 高可用

```bash
# 1. 初始化 Swarm（首次）
bash scripts/prod/swarm/init.sh

# 2. 部署服务栈
bash scripts/prod/swarm/deploy.sh

# 3. 查看状态
bash scripts/prod/swarm/status.sh

# 4. 手动扩容
docker service scale addp_transfer-bounded-worker=3
```

**Swarm 优势**:
- ✅ 自动重启（容器崩溃自动恢复）
- ✅ 多副本负载均衡（如 Transfer Worker x2）
- ✅ 滚动更新零停机
- ✅ 资源限制和预留

详见: [prod/README.md](prod/README.md), [build/README.md](build/README.md)

---

## 其他辅助目录

### registry/ - Docker Registry 管理

```bash
scripts/registry/
├── init.sh         # 初始化本地 Docker Registry
├── start.sh        # 启动已存在的 Registry
├── check.sh        # 检查 Registry 状态和镜像列表
└── configure.sh    # 配置 Docker daemon 信任 Registry
```

**用途**: 本地镜像仓库管理、离线部署准备

详见: [registry/README.md](registry/README.md)

### utils/ - 通用工具

```bash
scripts/utils/
├── go-mod-tidy-all.sh               # 批量清理 Go 依赖
├── ports-validate.sh                # 端口规范验证
├── test-tile-api.sh                 # MVT 瓦片 API 测试
└── standardize-frontend-docker.sh   # 前端 Docker 配置标准化
```

**用途**: 通用工具函数、批量操作、验证检查

### test/ - 测试与发布门禁

```bash
scripts/test/
├── agent-evaluation-gate.sh  # Agent 离线/发布统一评测门禁
├── check-execution-test-fixtures.sh # 统一执行存储测试夹具门禁
├── common-python-cli-release-gate.sh # ADDP CLI wheel 与 macOS Keychain 产品发布门禁
├── online-gate.py # T4 唯一 suite 登记与分发入口
├── online-gate_test.py # Online 分发协议确定性回归测试
├── online-preflight.py # T4 专用部署安全边界与构建身份预检
├── online-preflight_test.py # Online 预检确定性回归测试
├── standard-model-reference-deletion-online.py # Standard ↔ Model 正式 API 引用删除验收
├── standard-model-reference-deletion-online_test.py # 首个 Online suite 的确定性协议测试
├── quality-postgres-gate.sh # Quality PostgreSQL 集成门禁
├── standard-postgres-gate.sh # Standard PostgreSQL 集成门禁
└── system-iam-postgres-gate.sh # System IAM 与 Fosite 一次性 PostgreSQL 发布门禁
```

平台无外部服务的一致性门禁使用 `make test-platform`，依次校验技术栈规约与全部 `go.mod` 的依赖版本、统一 execution 测试夹具、IAM Manifest、owner 常量、Tool Catalog、SQL seed 和 Swagger 路由覆盖。该入口不启动或重启 ADDP 服务，不连接开发数据库；GitHub Actions 的 Platform CI 在 `main` 推送、每日定时和手工触发时直接调用该入口。

Go 全量测试使用 `make test-go`，根据 Git 已跟踪的全部 `go.mod` 在系统临时目录生成独立 workspace，再逐一运行 `go test ./...`，不依赖或修改本地被忽略的 `go.work`，也不维护第二份模块清单。`make test-execution-fixtures` 禁止业务测试手写 `task_executions` 表；Common 仓储自测、System PostgreSQL 专项测试及 Manager 历史表清理测试使用精确文件白名单。Model 的权限错误、URL 状态、ER 图过滤、DDL 请求和主题 token 回归使用 `make test-model-frontend`；该入口同时运行独立端口上的浏览器 E2E，覆盖 403 明确提示、业务域详情往返恢复和窄窗口深色 DDL 预览，并执行生产构建与 500 KiB 入口分块预算校验。三项均已纳入根目录 `make test`。

Online 唯一入口为 `make test-online ONLINE_SUITE=<suite>`，并要求环境中显式设置 `ADDP_ONLINE_TEST=1`。`scripts/test/online-gate.py` 只接受代码中已登记且已有 owner 门禁实现的 suite；未登记名称直接失败，不以占位场景冒充验收。分发器为整次运行生成或传递统一 Run ID，依次执行通用预检和 owner 门禁，并对二者施加总超时。

首个登记项为 `standard-model-reference-deletion`。它要求 `GATEWAY_URL`、`SYSTEM_URL`、`STANDARD_URL`、`MODEL_URL`、显式测试 Tenant 和 `ADDP_ONLINE_TEST_USER_ACCESS_TOKEN`；Token 对应专用测试 User，必须拥有 Standard Domain 和 Model Entity 的 create/read/delete Permission。业务创建、读取和删除请求全部经 Gateway 转发，直连服务地址只用于构建身份预检。场景创建 Standard Domain 和引用它的 Model Entity，断言 Domain 删除返回 `409 standard_resource_referenced`，删除 Entity 后再删除 Domain，最终通过双方 GET 404 证明零残留；任何业务或清理错误均使门禁失败。

通用预检由分发器向 `scripts/test/online-preflight.py` 传入参与服务的 `module=http://loopback:port`。预检要求显式非默认 Tenant、安全 Run ID、干净工作区，并校验所有 `/health` 构建身份与当前 Git commit 一致；任何非回环服务地址都会被拒绝。分发器与预检器的无外部服务回归测试统一使用 `make test-online-runner`，并已纳入 `make test-platform`。两者不执行未登记的业务断言，不读取或保存 Token，也不接管服务生命周期。

`scripts/ci/check-frontend-ci-registration.py` 是前端 CI 登记完整性检查。它从 Git 跟踪的 `*/frontend/package.json` 自动发现前端，要求每个前端同时具有 `scripts.build`、根 `Makefile` 的 `test-<module>-frontend` 标准入口，并登记到 `platform-ci.yml` 的目标和变更路径中。检查及其反例回归已纳入 `make test-platform`；新增前端时遗漏任一环节会使当次 Platform CI 失败。

`scripts/ci/check-build-registration.py` 是构建登记完整性检查。它自动发现正式 Go Server/Worker、Git 跟踪的前端和 `docker-compose.yml` 中的 ADDP 镜像，校验它们已登记到 `compile.sh`、`build-images.sh`，逐项验证镜像具有明确的 Dockerfile 或专用构建脚本，并核对预编译 Dockerfile 引用的二进制名称与 `compile.sh` 输出一致；同时禁止恢复已经删除的重复 Make 构建目标。检查及其反例回归已纳入 `make test-platform`；新增模块、Worker、前端或 Compose 镜像却遗漏构建登记或构建定义时，当次 Platform CI 会直接失败。

`scripts/ci/check-t2-ci-registration.py` 是 GitHub Hosted Runner 上 disposable PostgreSQL T2 门禁的登记完整性检查。它从 Git 跟踪的 `scripts/test/*-postgres-gate.sh` 自动发现门禁，要求每条门禁同时具有根 `Makefile` 标准入口、`release-and-t2-gates.yml` 调用、脚本路径触发和 owner `backend` 路径触发，并要求 PostgreSQL 15 Service 镜像按 digest 固定。ArcGIS 开放格式等需要专用样本或 Oracle 的 T2/T5 不属于该 Hosted Runner 契约，不会被伪装成普通 PostgreSQL 门禁。

`scripts/ci/check-engine-startup-isolation.py` 是 Engine 启动隔离一致性检查。它校验模块选择启动不会隐式拉起 DuckDB、Inference、Workflow 或 Jupyter Runtime，模块 Backend/Worker 的 Compose `depends_on` 不指向可选 Runtime，并禁止 System 恢复启动期内置 Runtime 代注册、全量能力刷新和对应 URL 配置。检查、反例回归及相关 Go 回归统一由 `make test-engine-startup-isolation` 执行，并纳入 `make test-platform`。

Agent 默认离线门禁使用 `make test-agent-eval`，并已包含在根 `make test`。该门禁分别使用 `agent/backend/venv` 运行 Agent 测试、使用 `common-python/.venv` 运行 Common-Python 全量测试；缺少后者时先执行 `cd common-python && uv sync --extra dev`。人工发布验收使用 `make test-agent-eval-release`，需要显式提供三份仓库外在线证据路径；脚本不自动执行 OAuth 登录或生成在线证据。输出统一为仓库外 `addp.agent-evaluation-gate/v2`，外部发布流程可归档其中的源码版本、契约/证据摘要和检查耗时，脚本自身不维护历史记录。

两份归档报告使用 `make compare-agent-eval` 比较，需要显式提供 `ADDP_AGENT_EVAL_BASELINE` 和 `ADDP_AGENT_EVAL_CURRENT`，结果通过 `ADDP_AGENT_EVAL_REPORT` 写到仓库外。比较只读取严格 v2 报告，输出 `addp.agent-evaluation-comparison/v1`，不重跑测试、不读取在线证据、不设置耗时阈值。

正式发布基线使用 `make compare-agent-eval-release`，复用相同环境变量，但强制 baseline/current 均为 clean、passed 的 `online_required` 报告且不存在回归。普通 dirty/离线报告只用于开发比较；脚本不自动选择、归档或更新 baseline。

阶段 5 封板后，上述五个 Make 目标与 `scripts/test/agent-evaluation-gate.sh` 是唯一标准入口；不新增旁路脚本、仓库内报告归档或兼容旧 Schema 的命令。

正式 `addp` CLI 发布使用 `make test-common-python-cli-release`。该入口构建 wheel，在全新 venv 安装并运行全量测试，校验 venv 中的 `addp` entry point 和版本；随后在临时 pipx 根目录中通过 `PIPX_DEFAULT_BACKEND=pip` 安装并强制重装同一个 wheel，校验包来源、版本、命令入口和卸载无残留，再通过 pipx 入口使用真实 macOS Keychain 执行 Browser PKCE、Device Flow、AuthContext、跨进程刷新、撤销和终端敏感信息产品 E2E。CLI 最终目标支持主流桌面操作系统，Windows 与 Linux 在各自真实 OS 凭据后端建立同等级 E2E 后再加入支持矩阵。门禁不接受明文文件 Keyring 降级，也不替代 System Fosite 与 PostgreSQL 协议测试。

GitHub Actions 的 IAM/CLI 发布工作流并行运行 macOS CLI 产品门禁和 Linux PostgreSQL 15 System 门禁。macOS Job 归档通过 `twine check` 和产品 E2E 的同一个 wheel 及 SHA-256，不在测试后重新构建发布制品；PostgreSQL Job 使用独占的临时 `addp_iam_test` database，不连接开发环境 Infra。

当前唯一正式分发路径是 GitHub Release。版本发布预检复用 common-python 全量测试中的 `tests/test_version.py`，统一校验运行时、安装包、命令和长期文档版本，不增加旁路脚本。发布工作流中的第三方 `uses:` 全部固定到不可变提交 SHA；固定版本的 zizmor 只扫描该工作流，并在现有 required Job 内阻断浮动 Action Tag 和中高风险供应链问题。推送与包版本一致的 `v<version>` Tag 会在同一次工作流中重新运行上述两项门禁；两项均成功后，发布 Job 只下载 `addp-cli-wheel` artifact，校验便携 SHA-256、包名和 wheel `METADATA` 版本，使用 GitHub OIDC 为 wheel 生成 build provenance attestation，再创建 Release。发布 Job 不检出源码、不重新构建 wheel，Release 仍只包含 wheel 和 checksum；attestation 由 GitHub Attestations API 保存，使用 `gh attestation verify` 验证。PyPI 或私有包仓库待账号、权限和发布策略明确后另行设计。

System IAM 和 Fosite 正式发布使用 `make test-system-iam-postgres`，必须显式提供唯一变量 `ADDP_SYSTEM_POSTGRES_TEST_DSN`，且数据库名包含独立的 `test` 或 `disposable` 段。门禁先清理一次性数据库中的 `system` 和 `common` Schema，再串行运行 IAM Domain、Fosite Storage、IAM API 和 Migration 的全部 PostgreSQL `AgainstPostgres` 测试；缺少 DSN、指向非一次性数据库或测试被阻断时立即失败。该入口只能指向专用临时数据库。

Standard 的正式 PostgreSQL 集成门禁使用 `make test-standard-postgres`，必须显式提供唯一变量 `STANDARD_POSTGRES_TEST_DSN`，且数据库名包含 `test` 或 `disposable`。门禁验证 Standard migration、删除约束、引用删除协调的并发锁和失败恢复，并拒绝任何测试 Skip。该入口只操作 Standard owner Schema；Standard ↔ Model 的生产调用通过 `common/client`，不允许使用跨 Schema SQL 模拟 Online 验收。

---

## 使用场景对比

| 场景 | 使用脚本 | 运行方式 | 镜像需求 |
|------|---------|---------|---------|
| **日常开发** | `scripts/dev/` | Go + npm 直接运行 | ❌ 不需要 |
| **本地测试** | `scripts/local/` | Docker Compose | ✅ 需要先构建 |
| **生产部署** | `scripts/prod/` | Docker Compose/Swarm | ✅ 从 Registry 拉取 |
| **构建发布** | `scripts/build/` | 编译 + 构建 + 打包 | ✅ 生成镜像 |

---

## 典型工作流

### 场景 1: 日常开发

```bash
# 1. 启动开发环境
bash scripts/dev/start.sh

# 2. 修改代码
vim system/backend/internal/service/user_service.go

# 3. 重启服务
bash scripts/dev/restart.sh

# 4. 查看日志
tail -f logs/system-backend.log

# 5. 停止环境
bash scripts/dev/stop.sh
```

### 场景 2: 本地容器化测试

```bash
# 1. 构建镜像
make build
make build-images

# 2. 启动 Docker 环境
bash scripts/local/start.sh

# 3. 测试功能
curl http://localhost:8180/health

# 4. 查看状态
bash scripts/local/status.sh

# 5. 停止环境
bash scripts/local/stop.sh
```

### 场景 3: 生产发布

```bash
# 1. 编译多架构二进制
make build BUILD_ARGS="--arch both"

# 2. 构建多架构镜像
IMAGE_TAG=v1.0.0 make build-images \
  IMAGE_BUILD_ARGS="--multi-arch --registry localhost:5001"

# 3. 登录并推送镜像到 Registry
docker login  # Docker Hub
# 或: docker login harbor.example.com:5001  # Harbor

bash scripts/build/push-images.sh \
  --registry docker.io/myorg \
  --tag v1.0.0

# 4. 生成部署包
bash scripts/build/package.sh \
  --mode registry \
  --registry docker.io/myorg \
  --server ubuntu@production-server

# 5. 在生产服务器上部署
ssh ubuntu@production-server
cd /opt/addp
bash scripts/prod/start.sh

# 6. 健康检查
bash scripts/prod/health-check.sh
```

---

## 常见问题

### Q1: 端口冲突怎么办？

```bash
# 检查端口占用
lsof -i :8180
lsof -i :5433

# 杀死占用进程或修改 .env 配置
```

### Q2: 基础设施启动失败？

```bash
# 查看基础设施状态
bash scripts/infra/status.sh

# 查看日志
docker logs postgres
docker logs redis
```

### Q3: 服务健康检查超时？

```bash
# 查看服务日志
tail -f logs/system-backend.log

# 手动测试健康端点
curl http://localhost:8180/health
```

### Q4: Docker 镜像不存在？

```bash
# 重新构建镜像
make build
make build-images

# 验证镜像
docker images | grep addp
```

### Q5: 如何清理所有数据？

```bash
# ⚠️ 危险操作：会删除所有数据

# 开发模式
bash scripts/dev/stop.sh

# 本地 Docker
bash scripts/local/stop.sh --all --volumes

# 生产环境
bash scripts/prod/stop.sh --volumes
```

---

## 最佳实践

1. **开发环境**: 始终使用 `scripts/dev/` 进行日常开发，重启速度快
2. **容器测试**: 使用 `scripts/local/` 验证 Docker 配置和部署流程
3. **生产部署**: 使用 `scripts/prod/` 部署，启用 Docker Swarm 提高可用性
4. **定期备份**: 生产环境定期备份 PostgreSQL 和 MinIO 数据
5. **版本管理**: 生产镜像使用明确的版本标签（如 `v1.0.0`），避免使用 `latest`
6. **日志监控**: 定期查看日志文件和 `health-check.sh` 输出
7. **资源限制**: 在 docker-compose.yml 中配置 CPU 和内存限制

---

## 相关文档

- [CLAUDE.md](../CLAUDE.md) - 项目总体架构文档
- [Makefile](../Makefile) - Make 命令封装
- [docker-compose.infra.yml](../docker-compose.infra.yml) - 基础设施配置
- [docker-compose.yml](../docker-compose.yml) - 应用服务配置
- [docs/guide/addp部署和开发步骤.md](../docs/guide/addp部署和开发步骤.md) - 部署与开发启动指南

---

## 贡献指南

添加新脚本时，请遵循以下规范：

1. **命名规范**: 使用 `kebab-case`（小写 + 连字符）
2. **Shebang**: 始终使用 `#!/usr/bin/env bash`
3. **错误处理**: 使用 `set -e` 或显式错误检查
4. **颜色输出**: 使用统一的颜色变量（GREEN, RED, YELLOW）
5. **幂等性**: 脚本应支持重复执行
6. **文档**: 添加脚本时更新对应的 README.md

---

**Version**: 0.1.14
**Last Updated**: 2026-07-31

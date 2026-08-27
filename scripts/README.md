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
| `init-minio.sh` | 初始化 MinIO buckets | MinIO 重置 |
| `init-postgresql.sh` | 初始化 PostgreSQL 基础能力 | HBA、扩展与模块 schema |
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
- ✅ **就绪检查**: ADDP Backend 等待 `/health/ready` 返回 200；Engine Runtime 使用各自协议的 `/health`
- ✅ **日志管理**: 所有日志存储在 `logs/*.log`
- ✅ **PID 追踪**: 存储进程 PID，支持优雅停止

详见: [dev/README.md](dev/README.md)

---

## 三、编译和构建 (build/)

**用途**: 编译二进制文件、构建 Docker 镜像、打包部署

仓库只维护根 `Makefile`，不允许模块级 Makefile。正式构建只提供两个标准入口：`make build` 调用 `scripts/build/compile.sh`，`make build-images` 调用 `scripts/build/build-images.sh`。构建事实只维护在这两个脚本中，不得在 Makefile、Workflow 或其他脚本中复制服务清单和构建命令。需要传递参数时分别使用 `BUILD_ARGS="..."` 和 `IMAGE_BUILD_ARGS="..."`。

生命周期入口同样只做脚本薄封装：开发环境使用 `make dev-start/dev-restart/dev-stop`，基础设施使用 `make infra-up/infra-restart/infra-down/infra-status`，生产环境使用 `make prod-start/prod-restart/prod-stop/prod-health`。模块级 `go run`、直接 Compose 生命周期目标和兼容别名均不保留；模块参数通过 `make dev-restart ARGS="-<模块名>"` 传递。

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
├── start.sh        # 启动本地构建 Registry
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
└── test-tile-api.sh                 # MVT 瓦片 API 测试
```

**用途**: 通用工具函数、批量操作、验证检查

### test/ - 测试与发布门禁

```bash
scripts/test/
├── agent-evaluation-gate.sh  # Agent 离线/发布统一评测门禁
├── local-macos-ci.sh # 日常 macOS 专用 checkout 的 origin/main 定时巡检入口
├── local-macos-ci_test.py # 定时巡检、成功基线和失败重试回归
├── release-gate.py           # T5 发布套件统一分发与结构化报告
├── check-execution-test-fixtures.sh # 统一执行存储测试夹具门禁
├── common-python-cli-release-gate.sh # ADDP CLI wheel 与 macOS Keychain 产品发布门禁
├── online-gate.py # T4 唯一 suite 登记与分发入口
├── online-gate_test.py # Online 分发协议确定性回归测试
├── online-preflight.py # T4 专用部署安全边界与构建身份预检
├── online-preflight_test.py # Online 预检确定性回归测试
├── online-host-gate.sh # 专用 addp-online Runner 生命周期编排
├── online-host-gate_test.py # 专用主机边界、启动映射与清理回归测试
├── consumer-engine-recovery-online.py # 真实浏览器消费方与 Engine 离线/恢复验收
├── consumer-engine-recovery-online_test.py # 消费方恢复报告与清理协议测试
├── consumer-process-stability-online.py # Manager/Service 与三个 Frontend PID 稳定性证据
├── consumer-process-stability-online_test.py # 消费方进程替换检测测试
├── online-engine-fixture_test.py # 专用业务 PostgreSQL Fixture 安全边界测试
├── online-workbench-mysql-fixture_test.py # Workbench 专用只读 MySQL Fixture 安全边界测试
├── module-lifecycle-process-online.py # 正式 Manager/System/Gateway 乱序观测与证据
├── module-registry-recovery-online.py # System 模块租约与 Gateway 路由恢复验收
├── module-registry-recovery-online_test.py # 模块注册恢复场景确定性协议测试
├── standard-model-reference-deletion-online.py # Standard ↔ Model 正式 API 引用删除验收
├── standard-model-reference-deletion-online_test.py # 首个 Online suite 的确定性协议测试
├── enterprise-catalog-publishing-online.py # Meta → Catalog → Asset → Portal 真实发布验收
├── enterprise-catalog-publishing-online_test.py # 企业目录发布协议确定性测试
├── workbench-service-consumption-online.py # Service → Workbench 的 MySQL 真实消费验收
├── workbench-service-consumption-online_test.py # Workbench 跨模块消费协议测试
├── develop-postgres-gate.sh # Develop 可复用成果变化源 PostgreSQL 集成门禁
├── quality-postgres-gate.sh # Quality PostgreSQL 集成门禁
├── standard-postgres-gate.sh # Standard PostgreSQL 集成门禁
└── system-iam-postgres-gate.sh # System IAM 与 Fosite 一次性 PostgreSQL 发布门禁
```

平台无外部服务的一致性门禁使用 `make test-platform`，依次校验技术栈规约与全部 `go.mod` 的依赖版本、统一 execution 测试夹具、IAM Manifest、owner 常量、Tool Catalog、SQL seed 和 Swagger 路由覆盖。该入口不启动或重启 ADDP 服务，不连接开发数据库；GitHub Actions 的 Platform CI 在 `main` 推送、每日定时和手工触发时直接调用该入口。

根 `make test` 是 T0-T1 全部无外部服务确定性门禁的唯一聚合入口，包含 `make test-platform`、全部 Go 模块、Common Python、Agent 离线评测、Copilot 后端，以及所有已登记前端的测试和生产构建。前端与 Python CI 登记检查同时要求每个自动发现的组件进入该聚合入口，新增测试组件不能只登记 CI 而遗漏本地总门禁。需要专用 PostgreSQL、真实运行服务、在线证据或发布环境的 T2-T5 门禁不并入 `make test`，必须使用各自显式入口。

日常使用的 macOS 可以在独立、干净的 `main` checkout 中定时运行 `make local-ci`。脚本会 fast-forward 到 `origin/main`，首次运行 `make test` 和全部已登记 PostgreSQL 门禁，之后以上次成功 SHA 运行 `make test-changed`；每个新 SHA 还会通过 `make build BUILD_ARGS=--force` 复验全部 Linux 产品二进制。失败不更新基线，后续调度会重试；无新提交时直接跳过。使用 `make local-ci LOCAL_CI_ARGS=--full` 可强制全量复验，使用 `make local-ci LOCAL_CI_ARGS=--check-only` 只检查 macOS、Git、Go 1.24+、Python 3.11+、`.node-version` 声明的 Node.js 24、Docker 和工作区边界。

该入口只启停 `addp-infra` Compose 项目并保留数据卷，不执行 Docker 全局清理。如果发现正在运行的 ADDP Infra，它会拒绝接管。日志、成功 SHA 和运行锁位于 `.git/addp-local-ci/`，不进入工作树。该辅助巡检不替代 GitHub Actions，也不运行 T4/T5。

Agent 的 `test-agent-eval` 与 `test-agent-frontend` 保持独立：前者只运行 Python 离线评测，后者运行前端测试和生产构建。根 `make test` 与模块门禁负责同时选择二者；CI 使用独立 Job 准备 Python 或 Node 环境，不能让评测目标隐式依赖前端安装。

单模块交付使用 `make test-module MODULE=<模块>`。`scripts/test/module-gate.py` 从 Git 跟踪的 Go module、`*/frontend/package.json`、`*/pyproject.toml` 和 `scripts/test/*-postgres-gate.sh` 自动生成该模块的 T0-T3 执行计划：先运行 `test-platform`，再串行执行语言测试、前端测试与生产构建、已登记 PostgreSQL T2。Go T1 会统一剔除 `*_POSTGRES_TEST_DSN` 和 `ADDP_POSTGRES_INTEGRATION`，避免调用者为后续 T2 提供的连接条件提前激活会并发迁移同一测试库的集成测试；PostgreSQL T2 仍只由 owner 的正式 gate 脚本显式启用。`--dry-run` 只用于检查计划；正式入口不跳过缺少连接条件的 T2。模块发现与执行计划的确定性测试已纳入 `make test-platform`。

AI 完成一组改动后使用 `make test-changed`；默认读取相对 `HEAD` 的已跟踪改动及未跟踪文件，或通过 `BASE_REF=<ref>` 指定比较基线。`scripts/test/changed-gate.py` 始终保留平台 T0，把普通路径映射到已登记 owner，并从 `go.mod`、前端 Git 跟踪源码/配置对 `common-frontend` 的实际引用、Python requirements 推导共享模块消费者，再复用模块门禁计划且去重。若受影响模块含 PostgreSQL T2，仍须提供对应安全连接条件，不会自动跳过。

GitHub Actions 中的模块 T0-T3 选择统一调用 `scripts/ci/select-module-gate.py --module <owner>`。该选择器复用 `changed-gate.py` 的 owner 影响计算：workflow、共享 action、CI 脚本、根 `Makefile` 或矩阵实现变更时选中全部模块；普通模块、共享依赖、离线评测场景和已登记 gate 脚本变更则按 owner 选择。Workflow 只负责准备对应的隔离环境并调用根 `Makefile` 标准入口，不再复制 owner 路径表。`scripts/ci/select-gate-by-paths.sh` 仅保留给 CLI wheel / Keychain 等非模块 T5 产品门禁。

Copilot 后端使用 `make test-copilot`，固定从 `copilot/backend` 运行全部 pytest。Platform CI 在 Copilot 或 `common-python` 改动时创建独立 Python 3.11 venv、安装 owner requirements 与 Common Python 测试依赖后调用同一入口。`scripts/ci/check-python-ci-registration.py` 自动发现 Python package 和 Python backend，校验 Make 目标、根 `make test`、workflow 调用及共享模块变更选择器登记。

Go 全量测试使用 `make test-go`，根据 Git 已跟踪的全部 `go.mod` 在系统临时目录生成独立 workspace，再逐一运行 `go test ./...`，不依赖或修改本地被忽略的 `go.work`，也不维护第二份模块清单。`make test-execution-fixtures` 禁止业务测试手写 `task_executions` 表；Common 仓储自测、System PostgreSQL 专项测试及 Manager 历史表清理测试使用精确文件白名单。Model 的权限错误、URL 状态、ER 图过滤、DDL 请求和主题 token 回归使用 `make test-model-frontend`；该入口同时运行独立端口上的浏览器 E2E，覆盖 403 明确提示、业务域详情往返恢复和窄窗口深色 DDL 预览，并执行生产构建与 500 KiB 入口分块预算校验。

Online 唯一入口为 `make test-online ONLINE_SUITE=<suite>`，并要求环境中显式设置 `ADDP_ONLINE_TEST=1`。`scripts/test/online-gate.py` 只接受代码中已登记且已有 owner 门禁实现的 suite；未登记名称直接失败，不以占位场景冒充验收。分发器为整次运行生成或传递统一 Run ID，依次执行通用预检和 owner 门禁，并对二者施加总超时。

首个登记项为 `standard-model-reference-deletion`。它要求 `GATEWAY_URL`、`SYSTEM_URL`、`STANDARD_URL`、`MODEL_URL`、显式测试 Tenant 和 `ADDP_ONLINE_TEST_USER_ACCESS_TOKEN`；Token 对应专用测试 User。套件先通过 System AuthContext 确认 User、Tenant 和 User Access Token 身份，拒绝平台或租户管理员角色，并验证 Standard Domain 和 Model Entity 的 create/read/delete Permission。业务创建、读取和删除请求全部经 Gateway 转发，直连服务地址只用于构建身份预检。场景创建 Standard Domain 和引用它的 Model Entity，断言 Domain 删除返回 `409 standard_resource_referenced`，删除 Entity 后再删除 Domain，最终通过双方 GET 404 证明零残留；任何身份、业务或清理错误均使门禁失败。

`module-registry-recovery` 要求专用部署中的 `GATEWAY_URL`、`SYSTEM_URL`、`MANAGER_URL` 和现有 `MANAGER_SERVICE_CLIENT_SECRET`。Host Gate 先通过受保护的正式进程入口依次启动 Manager、System、Gateway，验证 Manager 在 System 前为 Alive/Not Ready 且业务路由返回 `module_not_ready`，System 到达后以同一 `instance_id` Ready，Gateway 首个快照建立路由；随后停止 System，等待 Manager 转为 Not Ready、租约路由消失，再恢复 System 并验证 Manager 和 Gateway 无需重启即恢复。五个阶段分别写入 `module-lifecycle-*.json`。正式 Manager 优雅停止后，owner 套件再使用 `addp-manager` Platform Service Principal 获取短期 Token，经 System 正式注册、心跳和注销 API验证两个回环探针的幂等注册、租约失效、同 ID 恢复、双实例路由、发布元数据更新、优雅注销和零活跃实例残留。`ADDP_ONLINE_PROCESS_TIMEOUT_SECONDS`、`ADDP_ONLINE_ROUTE_TIMEOUT_SECONDS` 和 `ADDP_ONLINE_LEASE_TIMEOUT_SECONDS` 分别控制正式进程、路由和租约收敛上限。

`consumer-engine-recovery` 要求全量 ADDP 服务、Console、Manager、Service 和一个专用 PostgreSQL Engine Fixture。Host Gate 通过 `business/scripts/online-engine-fixture.sh` 管理物理端点，该入口只允许 `ADDP_ONLINE_HOST=1` 的 macOS 专用 Runner，使用独立 `ADDP_ONLINE_TEST_ENGINE_*` 变量并拒绝接管非 `business/postgres` Compose 容器；它不读取或创建 `business/.env`。suite 使用真实测试 User 用户名和密码登录 Console，并以同一 User 的 Access Token 校验 Tenant AuthContext 与最小权限。Configuration、Manager Data Explorer、Service Query Services 在同一 Browser Context 中各首次导航一次并等待自身首个请求成功；随后保持同一 Manager iframe，停止/恢复物理 Fixture，并通过 `POST /api/v1/system/engines/{id}/test` 记录 `offline → online`，页面必须通过既有轮询自动收敛。Manager、Service Backend 与 Console、Manager、Service Frontend 的 PID 前后必须一致。Engine Instance 由专用环境长期预置，suite 不创建、删除或修改其身份；退出路径恢复 `online` 后再停止 Fixture 与应用。

`enterprise-catalog-publishing` 要求全量 System、Gateway、Meta、Catalog、Asset、Portal 和 Console，并复用专用 PostgreSQL Engine Fixture。Fixture owner 在物理库启动时幂等建立 `public.addp_online_catalog_fixture`；suite 只经 Gateway 使用真实 User Token 连续发起两次 Meta 扫描，证明 fingerprint 与 CatalogEntry UUID 幂等，再验收资源盘点/治理目录视图、七维治理覆盖率、精确来源身份解析和业务编目。随后以 `AssetComponent.catalog_entry_id` 创建并发布资产，从 Portal 校验同一 CatalogEntry 身份；真实浏览器使用同一专用 User 登录 Console，验证覆盖率页、CatalogEntry 详情、Domain / Department / Engine 名称交互，以及资源盘点当前页显式多选和批量治理对话框；浏览器不实际提交第二次治理写入。临时 Asset 按 `published → offline → deleted` 清理，Asset-owned 目录同步删除；已初始化的永久 Catalog fixture 会恢复运行前编目聚合。环境另需 `ADDP_ONLINE_TEST_USER_USERNAME`、`ADDP_ONLINE_TEST_USER_PASSWORD`、`ADDP_ONLINE_TEST_CATALOG_DOMAIN_ID` 和 `ADDP_ONLINE_TEST_CATALOG_DEPARTMENT_ID`。

#### 企业数据目录专用 macOS 完整验证

复用现有两类标准入口，不建立 `test-all` 或另一套目录脚本。T0-T3 与 T4 的身份和基础设施边界不同，应使用同一台专用 macOS 上的两个独立干净 checkout，或分别由 Local CI checkout 与 `addp-online` GitHub Runner checkout 执行：

| 案例 | 层级 | 标准入口 | 通过证据 |
| --- | --- | --- | --- |
| `ECV-00` 验证机准入 | T0/T4 preflight | `make local-ci LOCAL_CI_ARGS=--check-only`；`online-host-gate.sh --check-only` | 工具链、干净 checkout、外部环境文件、独立证据目录、`addp_online` 与 loopback 边界通过 |
| `ECV-01` 全量确定性门禁 | T0-T3 | `make local-ci LOCAL_CI_ARGS=--full` | 全仓 T0-T1、全部已登记 PostgreSQL T2、前端 T3、全部 Linux 产品构建通过 |
| `ECV-02` 构建与身份 | T4 | `enterprise-catalog-publishing` | System/Gateway/Meta/Catalog/Asset/Portal Build Identity 与 checkout 一致；User/Tenant/Permission 验证通过 |
| `ECV-03` 自动建档幂等 | T4 | 同上 | 两次真实 Meta scan 返回成功，fingerprint 与 CatalogEntry UUID 均不变化 |
| `ECV-04` 目录读模型 | T4 | 同上 | 同一条目进入 `inventory` 与编目后的 `governance` 视图；治理状态总数与七个覆盖率维度分母自洽 |
| `ECV-05` 动态来源解析 | T4 | 同上 | Meta fingerprint 经 `POST /catalog/entries/resolve-sources` 精确解析到当前 active CatalogEntry |
| `ECV-06` Console 交互 | T4 browser | 同上 | 覆盖率七维、CatalogEntry 详情、名称选择器、当前页显式多选和批量治理对话框可用，页面不出现 `undefined`，浏览器 warning/error 与失败业务响应均为 0 |
| `ECV-07` 发布消费唯一路线 | T4 | 同上 | `Meta → Catalog → AssetComponent → Portal` 保持同一 CatalogEntry UUID |
| `ECV-08` 清理 | T4 | 同上 | 临时 Asset 下架后删除、Asset-owned 目录删除、Portal 404，`residual_resources=0`，永久 fixture 编目聚合恢复 |

T0-T3 checkout 执行：

```bash
make local-ci LOCAL_CI_ARGS=--check-only
make local-ci LOCAL_CI_ARGS=--full
```

T4 推荐从 GitHub Actions 手工选择 `Online T4 gates / enterprise-catalog-publishing`。需要在 Runner 上直接预检时，只从调用方提供 suite 和仓库外证据目录，Secret 仍全部来自仓库外环境文件：

```bash
ADDP_ONLINE_HOST=1 \
ADDP_ONLINE_ENV_FILE=/absolute/path/to/addp-online.env \
ADDP_ONLINE_ARTIFACT_DIR=/absolute/path/to/evidence \
ONLINE_SUITE=enterprise-catalog-publishing \
bash scripts/test/online-host-gate.sh --check-only
```

预检通过后去掉 `--check-only` 即执行正式生命周期和唯一 `make test-online` 入口。验收必须同时保留 `readiness.txt`、`summary.txt`、`online-report.json`、`enterprise-catalog-publishing-browser.json`、`online-gate.log`、Playwright 失败截图（如有）和服务日志；任何缺失、Skip、清理失败或证据中的构建身份不一致都按失败处理。

`workbench-service-consumption` 要求全量 System、Gateway、Service、Workbench 和 Console，并使用 `business/scripts/online-workbench-mysql-fixture.sh` 管理独立 Business MySQL。Fixture 只接受仓库外 `ADDP_ONLINE_WORKBENCH_MYSQL_*` 变量，不读取或创建 `business/.env`；它重建仓库已有确定性样例并把 Engine 使用的账号收敛为仅有 `SELECT` 的读取账号。suite 使用永久 MySQL Engine Instance，经 Gateway 调用 Service 输出契约检测，临时发布固定 PII-safe SQL 服务 `commerce-order-analysis`，再经 Consumer Descriptor 创建个人 Workbench View；API 层真实验证 cursor、动态筛选、标量类型、有限 CSV、无空间输出和契约指纹变化，浏览器层以同一 User 登录 Console，验证 Table、Chart、Map 能力约束及契约变化后的查询阻断。Host Gate 安装专用 Chromium；View 与 Query Service 只按本轮创建 ID 在 `finally` 删除，不使用名称前缀或数据库清理。

通用预检由分发器向 `scripts/test/online-preflight.py` 传入参与服务的 `module=http://loopback:port`。预检要求显式非默认 Tenant、安全 Run ID、干净工作区和唯一专用数据库 `POSTGRES_DB=addp_online`，并校验所有 `/health/live` 构建身份与当前 Git commit 一致，再要求 `/health/ready` 已就绪；任何非回环服务地址都会被拒绝。宿主机 `--check-only` 在生命周期操作前调用同一预检器的 `--environment-only`，因此不存在第二套数据库或 Tenant 判定。分发器与预检器的无外部服务回归测试统一使用 `make test-online-runner`，并已纳入 `make test-platform`。两者不执行未登记的业务断言，不读取或保存 Token，也不接管服务生命周期。

分发器对同一 `suite + Run ID` 使用操作系统临时目录进程锁，锁覆盖预检、场景和报告写入。成功或失败均生成 `addp.online-gate/v1` 的 `online-report.json`：专用 Runner 写入仓库外 `ADDP_ONLINE_ARTIFACT_DIR` 并由 workflow 归档，本地直接执行则写入操作系统临时目录。报告包含构建身份、脱敏服务地址、Tenant、`addp_online` 数据库类别、阶段耗时、稳定错误码及 owner suite 的身份/创建/清理/残留证据，不保存 Token、Secret 或完整错误响应正文。

专用部署只允许由 `.github/workflows/online-t4-gates.yml` 的手工 `workflow_dispatch` 在带 `self-hosted`、`macOS`、`addp-online` 标签的 Runner 上触发。workflow 首先调用 `bash scripts/test/online-host-gate.sh --check-only`，在不启停任何服务的前提下验证专用主机标记、macOS、仓库外环境文件与证据目录、显式 Tenant、suite 部署 profile、必要命令和干净工作区，并产出不含密钥的 `readiness.txt`；预检通过后才调用同一脚本的默认生命周期模式。该脚本从 `ADDP_ONLINE_ENV_FILE` 指定的仓库外绝对路径加载 T4 密钥、专用 Tenant 和独占基础设施连接；`ONLINE_SUITE` 与 `ADDP_ONLINE_ARTIFACT_DIR` 只能由 workflow 或直接调用方提供，密钥文件中的同名残留值不会改写实际套件和证据落点。仓库根存在 `.env`、源码不干净或证据目录位于仓库内都会直接失败。生命周期模式只调用现有 Infra/开发启停脚本和 `make test-online`，退出时无条件停止应用，证据写入仓库外 `ADDP_ONLINE_ARTIFACT_DIR`。`scripts/ci/check-online-ci-registration.py` 要求 Online suite 登记、部署启动 profile、Runner 预检和 workflow choices 完全一致，并在首次真实运行通过前禁止增加 `schedule`。

Runner 管理员应以根 `.env.example` 为字段清单，在仓库外创建独立环境文件并替换全部 Secret；不能把该文件复制为仓库根 `.env`。除专用 Infra 连接和所有内置 `*_SERVICE_CLIENT_SECRET` 外，至少显式配置 `ADDP_ONLINE_TEST=1`、`ADDP_ONLINE_TEST_TENANT_ID`、`POSTGRES_DB=addp_online`、`SYSTEM_URL`、`GATEWAY_URL`、`MANAGER_URL`、`META_URL`、`CATALOG_URL`、`ASSET_URL`、`PORTAL_URL`、`SERVICE_URL`、`WORKBENCH_URL`、`CONSOLE_URL`、`STANDARD_URL`、`MODEL_URL`。`module-registry-recovery` 使用其中的 `MANAGER_SERVICE_CLIENT_SECRET`；`standard-model-reference-deletion` 使用 `ADDP_ONLINE_TEST_USER_ACCESS_TOKEN`；`consumer-engine-recovery` 另外要求同一专用 User 的 `ADDP_ONLINE_TEST_USER_USERNAME`、`ADDP_ONLINE_TEST_USER_PASSWORD`，以及稳定 `ADDP_ONLINE_TEST_ENGINE_ID`、名称、端口、用户、密码和数据库；`enterprise-catalog-publishing` 同样使用该专用 User 的用户名和密码，并需预置永久 Domain 和 Department ID；`workbench-service-consumption` 另需永久 `ADDP_ONLINE_WORKBENCH_MYSQL_ENGINE_ID`，以及端口、数据库、只读用户、只读密码和 root fixture 密码，其浏览器阶段也使用同一专用 User 的用户名和密码。GitHub Environment `addp-online` 的 Repository Variable `ADDP_ONLINE_ENV_FILE` 只保存该仓库外文件的绝对路径，不保存文件内容。

`scripts/ci/check-frontend-ci-registration.py` 是前端 CI 登记完整性检查。它从 Git 跟踪的 `*/frontend/package.json` 自动发现前端，要求每个前端同时具有 `scripts.build`、根 `Makefile` 的 `test-<module>-frontend` 标准入口，并在 workflow 中登记目标、标准前端环境 action 和共享模块变更选择器。检查及其反例回归已纳入 `make test-platform`；新增前端时遗漏任一环节会使当次 Platform CI 失败。

`scripts/ci/check-build-registration.py` 是构建登记完整性检查。它自动发现正式 Go Server/Worker、Git 跟踪的前端和 `docker-compose.yml` 中的 ADDP 镜像，校验它们已登记到 `compile.sh`、`build-images.sh`，要求每个 Git 跟踪的 Dockerfile 归属于正式镜像或明确的辅助构建，并禁止模块 Makefile 复制根构建入口。检查逐项验证镜像具有被 Git 跟踪的 Dockerfile 或专用构建脚本，核对 Docker build context 内的 `COPY` 源路径存在、被 Git 跟踪且未被对应 `.dockerignore` 排除，本地 Registry 基础镜像已登记到 `seed_base_images` 且源与目标均未使用浮动 `latest` 标签，并检查预编译 Dockerfile 引用的二进制名称与 `compile.sh` 输出一致；同时禁止恢复已经删除的重复 Make 构建目标。Platform CI 必须在干净 Ubuntu Runner 中运行 `make build BUILD_ARGS=--force`，实际编译全部正式二进制。检查及其反例回归已纳入 `make test-platform`；新增模块、Worker、前端、Compose 镜像、Dockerfile 或 Makefile 却遗漏统一登记和分类时，当次 Platform CI 会直接失败。

`scripts/ci/check-t2-ci-registration.py` 是 GitHub Hosted Runner 上 disposable PostgreSQL T2 门禁的登记完整性检查。它从 Git 跟踪的 `scripts/test/*-postgres-gate.sh` 自动发现门禁，要求每条门禁同时具有根 `Makefile` 标准入口、`make test-integration` 串行聚合登记、`release-and-t2-gates.yml` 调用和共享模块变更选择器，并要求 PostgreSQL 15 Service 镜像按 digest 固定。ArcGIS 开放格式等需要专用样本或 Oracle 的 T2/T5 不属于该 Hosted Runner 契约，不会被伪装成普通 PostgreSQL 门禁。

`scripts/ci/check-engine-startup-isolation.py` 是 Engine 启动隔离一致性检查。它校验模块选择启动不会隐式拉起 DuckDB、Inference、Workflow 或 Jupyter Runtime，模块 Backend/Worker 的 Compose `depends_on` 不指向可选 Runtime，并禁止 System 恢复启动期内置 Runtime 代注册、全量能力刷新和对应 URL 配置。检查、反例回归及相关 Go 回归统一由 `make test-engine-startup-isolation` 执行，并纳入 `make test-platform`。

Agent 默认离线门禁使用 `make test-agent-eval`，并已包含在根 `make test`。该门禁只使用 `agent/backend/venv` 这一套 owner 运行时执行 Agent 评测与 Common Python 契约测试；Agent requirements 显式声明所需的 Common Python 测试扩展，禁止重新引入第二套虚拟环境耦合。人工发布验收使用 `make test-release RELEASE_SUITE=agent-evaluation`，需要显式提供三份仓库外在线证据路径；分发器调用 owner 唯一目标 `test-agent-eval-release`，脚本不自动执行 OAuth 登录或生成在线证据。owner 输出统一为仓库外 `addp.agent-evaluation-gate/v2`，外部发布流程可同时归档其中的源码版本、契约/证据摘要、检查耗时和上层 T5 报告，脚本自身不维护历史记录。

T5 发布认证的统一入口是 `make test-release RELEASE_SUITE=<suite>`。当前只登记 `common-python-cli` 和 `agent-evaluation` 两个已有真实 owner 门禁的套件，不以占位项冒充发布认证。分发器把 suite 映射到 owner 的唯一 Make 目标，强制使用仓库外空证据目录，并在成功或失败时生成 `addp.release-gate/v1` 的 `release-report.json` 与 GitHub Step Summary 可直接消费的 `release-summary.md`；报告只记录 suite、owner 目标、结果、耗时与产物相对路径，不保存 Token 或在线证据正文。CLI workflow 使用该入口，Agent 仍由具备三份在线证据的外部发布流程调用；`scripts/ci/check-release-ci-registration.py` 会阻止 workflow 绕过统一入口或遗漏报告摘要。

两份归档报告使用 `make compare-agent-eval` 比较，需要显式提供 `ADDP_AGENT_EVAL_BASELINE` 和 `ADDP_AGENT_EVAL_CURRENT`，结果通过 `ADDP_AGENT_EVAL_REPORT` 写到仓库外。比较只读取严格 v2 报告，输出 `addp.agent-evaluation-comparison/v1`，不重跑测试、不读取在线证据、不设置耗时阈值。

正式发布基线使用 `make compare-agent-eval-release`，复用相同环境变量，但强制 baseline/current 均为 clean、passed 的 `online_required` 报告且不存在回归。普通 dirty/离线报告只用于开发比较；脚本不自动选择、归档或更新 baseline。

阶段 5 封板后，上述五个 Make 目标与 `scripts/test/agent-evaluation-gate.sh` 是唯一标准入口；不新增旁路脚本、仓库内报告归档或兼容旧 Schema 的命令。

正式 `addp` CLI 发布使用 `make test-release RELEASE_SUITE=common-python-cli`；分发器调用 owner 唯一目标 `test-common-python-cli-release`。该门禁构建 wheel，在全新 venv 安装并运行全量测试，校验 venv 中的 `addp` entry point 和版本；随后在临时 pipx 根目录中通过 `PIPX_DEFAULT_BACKEND=pip` 安装并强制重装同一个 wheel，校验包来源、版本、命令入口和卸载无残留，再通过 pipx 入口使用真实 macOS Keychain 执行 Browser PKCE、Device Flow、AuthContext、跨进程刷新、撤销和终端敏感信息产品 E2E。CLI 最终目标支持主流桌面操作系统，Windows 与 Linux 在各自真实 OS 凭据后端建立同等级 E2E 后再加入支持矩阵。门禁不接受明文文件 Keyring 降级，也不替代 System Fosite 与 PostgreSQL 协议测试。

GitHub Actions 的 IAM/CLI 发布工作流并行运行 macOS CLI 产品门禁和 Linux PostgreSQL 15 System 门禁。macOS Job 归档通过 `twine check` 和产品 E2E 的同一个 wheel 及 SHA-256，不在测试后重新构建发布制品；PostgreSQL Job 使用独占的临时 `addp_iam_test` database，不连接开发环境 Infra。

当前唯一正式分发路径是 GitHub Release。版本发布预检复用 common-python 全量测试中的 `tests/test_version.py`，统一校验运行时、安装包、命令和长期文档版本。推送 `v<version>` Tag 后，发布资格门禁要求 Tag 与包版本一致、Tag commit 位于 `origin/main` 历史，并等待相同 SHA 的 Platform CI push run 成功；分支 Ruleset 不保护 Tag，因此该检查不能省略。发布工作流中的第三方 `uses:` 全部固定到不可变提交 SHA；固定版本的 zizmor 只扫描该工作流，并在现有 required Job 内阻断浮动 Action Tag 和中高风险供应链问题。资格检查、CLI macOS Keychain 和 System IAM 三项均成功后，发布 Job 只下载 `addp-cli-wheel` artifact，校验便携 SHA-256、包名和 wheel `METADATA` 版本，使用 GitHub OIDC 为 wheel 生成 build provenance attestation，再创建 Release。发布 Job 不检出源码、不重新构建 wheel，Release 仍只包含 wheel 和 checksum；attestation 由 GitHub Attestations API 保存，使用 `gh attestation verify` 验证。PyPI 或私有包仓库待账号、权限和发布策略明确后另行设计。

准备新版本时只运行 `make prepare-cli-release RELEASE_VERSION=<version>`。该入口要求稳定的递增 `X.Y.Z` 版本，并以 `addp_common.__version__` 为当前事实源，一次性更新运行时、安装示例和长期文档；任一登记位置缺失或重复时会在写文件前整体拒绝。它不会提交、打 Tag 或推送。

版本改动提交并推送到 `main`、同 SHA 的 Platform CI 成功后，创建 Tag 前必须运行 `make check-cli-release RELEASE_TAG=v<version>`。该入口会更新远端 `main` 与 Tag 引用，要求目标 Tag 尚不存在、当前 `HEAD` 等于最新 `origin/main`、版本与包事实源一致，并确认同 SHA 的 Platform CI 已成功；通过后才执行 `git tag` 和 `git push origin <tag>`。公开仓库可匿名查询 Actions 结果，设置 `GITHUB_TOKEN` 时会使用 Token 提高 API 限额。

System IAM 和 Fosite 正式发布使用 `make test-system-iam-postgres`，必须显式提供唯一变量 `ADDP_SYSTEM_POSTGRES_TEST_DSN`，且数据库名包含独立的 `test` 或 `disposable` 段。门禁先清理一次性数据库中的 `system` 和 `common` Schema，再串行运行 IAM Domain、Fosite Storage、IAM API 和 Migration 的全部 PostgreSQL `AgainstPostgres` 测试；缺少 DSN、指向非一次性数据库或测试被阻断时立即失败。该入口只能指向专用临时数据库。

Standard 的正式 PostgreSQL 集成门禁使用 `make test-standard-postgres`，必须显式提供唯一变量 `STANDARD_POSTGRES_TEST_DSN`，且数据库名包含 `test` 或 `disposable`。门禁验证 Standard migration、删除约束、引用删除协调的并发锁和失败恢复，并拒绝任何测试 Skip。该入口只操作 Standard owner Schema；Standard ↔ Model 的生产调用通过 `common/client`，不允许使用跨 Schema SQL 模拟 Online 验收。

具备全部安全连接条件时，使用 `make test-integration` 严格串行运行 System IAM、Quality 和 Standard 的已登记 PostgreSQL 门禁；即使调用方使用 `make -j`，聚合入口也不会让共享测试库并发执行。各 owner 脚本继续负责校验 DSN 或 database 名称并执行清理，聚合入口不提供默认业务库连接，也不复制模块测试逻辑。GitHub Actions 仍分别运行模块级目标，每个 Job 使用独占 PostgreSQL Service。

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
curl http://localhost:8180/health/ready

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
curl http://localhost:8180/health/ready
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

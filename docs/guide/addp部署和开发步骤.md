## 快速开始
需要时，请阅读 docs/guide/addp部署和开发步骤.md。

### 前提条件

**需要 Docker 环境**: ADDP 平台需要安装 Docker 和 Docker Compose。

- 安装 Docker Desktop: https://www.docker.com/products/docker-desktop
- 验证安装: `docker --version` 和 `docker-compose --version`

### 第一步: 启动基础设施 (必须首先执行)

**重要**: 必须先启动基础设施服务 (PostgreSQL、Redis、FalkorDB、MinIO、Meilisearch 等)。FalkorDB 是 Ontology 的私有 Infra，已有环境须在根 `.env` 配置独立的 `INFRA_FALKORDB_PASSWORD`。

```bash
# 从项目根目录
bash scripts/infra/up.sh
```

此脚本自动完成:
- 启动 PostgreSQL (addp-postgres)、Redis (addp-redis)、FalkorDB (addp-falkordb)、MinIO (addp-minio)、Meilisearch (addp-meilisearch) 等容器
- 默认宿主机端口被占用时自动选择空闲端口并打印实际地址；容器内部地址不变
- 初始化所有模块的 PostgreSQL schemas
- 初始化 MinIO buckets 和 Redis 配置
- 配置 Meilisearch 索引

检查基础设施状态:
```bash
bash scripts/infra/status.sh
```

### 第二步: 开发模式 (推荐用于开发)

**按正确顺序启动所有服务**,使用自动化开发脚本:

```bash
# 从项目根目录
bash scripts/dev/start.sh
```

开发环境的 `start.sh`、`restart.sh`、`stop.sh` 和 `keepalive.sh` 使用工作区级生命周期锁。同一工作区已有启动、停止或重启操作执行时，新的生命周期操作会立即失败并显示当前操作信息，不能并行修改 `.dev-pids`、`.dev-bins` 或运行进程。详细契约见 [开发服务生命周期与构建身份规范](../spec/addp开发服务生命周期与构建身份规范.md)。

开发生命周期安装 Node 依赖时以 `package-lock.json` 为不可变构建输入，并统一执行 `npm ci`；缺少锁文件时直接失败，锁文件生成只属于显式的依赖维护流程。因此本地启动和 Hosted Online 门禁不会在安装依赖时改写已跟踪的锁文件或污染构建身份。

启动脚本按 Compose 项目、服务名和容器健康状态检查 Infra；其他服务即使占用 ADDP 的首选端口，也不能被当作 ADDP Infra。实际宿主机映射由 Compose 查询并注入开发进程。Backend 仍须通过 `/health/ready`：Ontology 还需验证 PostgreSQL、FalkorDB 图能力及 System 注册全部就绪。

开发启动还会检查 Gateway、模块 Backend/Frontend 和工作流 Runtime 的首选端口。发生冲突时，启动输出会显示替代端口，实际地址以输出和 `.dev-state/ports.env` 为准；Console 的代理、iframe、API 文档和各前端 Gateway 代理同步使用该结果。停止脚本只清理当前工作区的进程及容器，不会清理占用首选端口的其他服务。

自动启动以下内容:
1. 基础设施 (如未运行)
2. 所有后端服务 (System、Manager、Meta、Transfer、Orchestrator、Develop、GeoPython Workflow、Model3D Workflow Engine、PointCloud Workflow Engine)
3. Gateway 服务
4. 所有前端服务 (可选,提示用户)

停止所有服务:
```bash
bash scripts/dev/stop.sh
```

代码修改后重启:
```bash
bash scripts/dev/restart.sh
```

按模块启动或重启:

```bash
bash scripts/dev/start.sh -manager
bash scripts/dev/restart.sh -transfer
```

单模块开发时，脚本会统一启动公共依赖：System Backend、Meta Backend、Meta Worker、Gateway 和 Console。Meta 用于资源树、元数据扫描和跨模块通用元数据能力。模块自身如有额外依赖，例如 Manager 依赖 Transfer、Model3D Workflow Engine 和 PointCloud Workflow Engine，Develop 依赖 Python/Math/Spark Workflow Engine 和 Jupyter，会在此基础上继续启动。

SuperMap Workflow Engine 使用本地两层镜像。首次安装时，保留完整 SuperMap iObjects C++ SDK 母版，把许可放入 Git 忽略的 `engines/supermap-workflow/vendor/license`，并通过 `SUPERMAP_CPP_SDK_PATH` 显式构建稳定基础镜像：

```bash
SUPERMAP_CPP_SDK_PATH=/path/to/supermap-iobjectscpp-12.1.0-linux-arm64-all \
  bash scripts/build/build-supermap-workflow-base.sh
```

之后全量启动、`restart.sh -all` 和局部重启都会计算当前 ADDP C++ 源码、CMake、运行脚本、Dockerfile、基础镜像和目标平台的构建指纹；输入未变化时直接复用现有代码镜像，输入变化或镜像不存在时才重新编译并测试 C++ Runtime。单独验证超图算子时使用：

```bash
bash scripts/dev/start.sh -supermap-workflow
bash scripts/dev/restart.sh -supermap-workflow
```

### 第三步: 构建模式 (用于 Docker 镜像构建)

```bash
# 编译 Go 二进制文件
make build

# 构建 Docker 镜像
make build-images

# 只构建三维模型转换运行时镜像（Apple Silicon 默认为 linux/arm64）
make build-images IMAGE_BUILD_ARGS="--services model3d-workflow-engine --force"

# 只构建点云转换运行时镜像（内置 PDAL）
make build-images IMAGE_BUILD_ARGS="--services pointcloud-workflow-engine --force"

# 打包并推送镜像 (如需要)
bash scripts/build/package.sh
```

`make build` 与 `make build-images` 是平台唯一标准构建入口，分别薄调用 `scripts/build/compile.sh` 与 `scripts/build/build-images.sh`。`make build BUILD_ARGS="--arch amd64 --services system-backend,gateway"` 可精确构建指定服务；Go 产物按实际依赖指纹判断缓存并写入构建身份，供 Online 预检核对。新增正式服务、Worker、前端或 Compose 镜像时，必须在同一次变更中补齐对应构建登记；`make test-platform` 会自动校验完整性。

代码交付前默认使用统一影响分析入口，不要等推送后的 CI 通知才补测试或构建适配：

```bash
# 当前工作区：包含已跟踪和未跟踪改动
make test-changed

# 一段已提交变更
make test-changed BASE_REF=<ref>

# 明确验证单个 owner 的 T0-T3 门禁
make test-module MODULE=<模块>
```

共享 Go、前端和 Python 代码会根据仓库内真实依赖扩散到消费者。`make test-platform` 负责检查新模块、测试入口、前端、Python 包、PostgreSQL 门禁和产品构建是否完整登记。GitHub Actions 在 `main` push 后和每日定时任务中使用独立 Runner 复验；本地 `start.sh`、`restart.sh`、`git commit` 均不会触发 CI。

`model3d-workflow-engine` 使用专用镜像构建链路：先构建绑定 `_3dtile` 的 `addp-model3d-converter`，再构建内置转换器的 `addp-model3d-workflow-engine`。当前转换器构建一次只支持一个 Linux 平台，Apple Silicon 本机优先使用默认的 `linux/arm64` 容器路径。

`model3d-workflow-engine` 运行在 Docker 中时，NFS/localfs 数据根目录必须挂载进 runtime 容器，并且 Manager 传给 operator 的本地路径必须在容器内可见。Compose 默认提供：

```bash
MODEL3D_DATA_HOST_PATH=./business/nfs/data
MODEL3D_DATA_CONTAINER_PATH=/Users/pampa/code/addp/business/nfs/data
```

单 OSGB 快显生成的 GLB artifact 统一使用 ADDP infra MinIO 配置，不单独配置 `model3d_workflow` 专用 MinIO endpoint。Docker Compose 部署时，Manager 与 `model3d-workflow-engine` 同在 Compose 网络内，统一使用 infra MinIO 的 `minio:9000`；macOS 本机开发时，推荐使用宿主机 Python runtime 加 Docker `_3dtile` wrapper，Manager 与 runtime 统一访问 `localhost:19000`。

`pointcloud-workflow-engine` 使用 Docker runtime 承载 PDAL，不依赖宿主机安装 PDAL。开发模式下 `start.sh -manager` 或 `start.sh -pointcloud-workflow` 会自动构建并启动 `addp-pointcloud-workflow-engine:dev`，默认把 `business/nfs/data` 作为只读点云源目录挂入容器，并把 `data/pointcloud-work` 作为容器内工作目录。可通过以下变量覆盖：

```bash
POINTCLOUD_DATA_HOST_PATH=./business/nfs/data
POINTCLOUD_DATA_CONTAINER_PATH=/Users/pampa/code/addp/business/nfs/data
POINTCLOUD_WORK_HOST_PATH=./data/pointcloud-work
```

点云 COPC artifact 统一使用 ADDP infra MinIO 配置，不单独配置 `pointcloud_workflow` 专用 MinIO endpoint。Docker Compose 部署时，Manager 与 `pointcloud-workflow-engine` 同在 Compose 网络内，统一使用 infra MinIO 的 `minio:9000`；macOS 本机开发时，PointCloud Workflow 容器通过 `host.docker.internal:${MINIO_API_PORT:-19000}` 访问宿主机 infra MinIO。

`supermap-workflow-engine` 使用 Docker runtime 承载 Linux arm64 SuperMap SDK，不依赖宿主机 Linux OS。私有组件只保存在 Git 忽略的 `engines/supermap-workflow/vendor/` 并进入稳定基础镜像；日常代码镜像每次重启都重新编译。可选变量：

```bash
SUPERMAP_WORKFLOW_PORT=8103
SUPERMAP_WORKFLOW_BASE_IMAGE=addp-supermap-workflow-base:local
SUPERMAP_WORKFLOW_IMAGE=addp-supermap-workflow-engine:dev
SUPERMAP_DATA_HOST_PATH=/path/to/supermap/data
SUPERMAP_OUTPUT_HOST_PATH=/tmp/supermap-out
```

Develop 正式任务向 NFS 输出 UDBX 时，不需要预先为某个 NFS 存储引擎配置 SuperMap 专用挂载目录。Develop 会在执行期把用户选择的 NFS 引擎连接事实和相对输出路径传给 `supermap-workflow-engine`；容器需要包含 `nfs-common` 并具备 Linux mount 权限，开发脚本和 Compose 已为该容器启用 `SYS_ADMIN` capability。

### 第四步: 本地 Docker Compose 模式

本地和生产容器部署使用 `addp-infra`、`addp-platform`、`addp-runtimes` 三个 ADDP Compose project；业务数据引擎保留独立 `business` project。平台模块共用 `addp-platform` 生命周期，内置 Runtime 由 `docker-compose.runtimes.yml` 独立启动；所有 ADDP project 在外部 `addp-network` 上以服务名互访。Docker Desktop 中看到的 project 分组不等于 System 模块或引擎注册表。

从旧 `addp-app` 容器部署切换时，须先核对该 project 的容器归属并停止旧项目，再以本节标准入口启动新分组；固定 `container_name` 不能在两个 project 下同时存在。此切换不删除 `addp-infra` 或 `business` 的数据卷。源码开发模式中的容器化 Runtime 仍由 `scripts/dev/` 启停，其项目标签用于 Docker Desktop 分组，不以生产 Compose 命令接管。

```bash
# 通过 Docker Compose 启动完整平台
bash scripts/local/start.sh

# 检查状态
bash scripts/local/status.sh

# 停止所有服务
bash scripts/local/stop.sh
```

### 第五步: 生产模式

**一键生产部署**:

```bash
# 从项目根目录
bash scripts/prod/start.sh
```

**部署流程** (自动执行):

1. **启动基础设施** (PostgreSQL、Redis、MinIO、Meilisearch)
2. **启动 System Backend** (其他服务依赖它)
3. **启动平台服务** (Manager、Meta、Transfer、Orchestrator、Develop 等 Backend、Gateway、前端、Console 和 Nginx)
4. **启动内置 Runtime** (GeoPython Workflow、Model3D Workflow、PointCloud Workflow、SuperMap Workflow 等独立 `addp-runtimes` 服务)
5. **健康检查** (验证所有服务就绪)

**访问地址** (部署完成后): `ADDP_PUBLIC_ORIGIN`；未设置时为 `http://localhost:<NGINX_PORT>`，默认 `http://localhost:80`。Console、模块前端和 `/api/` 均由这个 Nginx 入口转发；容器内使用稳定服务名和端口，应用模块不发布其他宿主机端口。需要限制宿主机监听范围时设置 `NGINX_BIND_HOST`，默认 `0.0.0.0`；隔离验收使用 `127.0.0.1`。域名、HTTPS 或上级反向代理部署时在根 `.env` 配置实际的 `ADDP_PUBLIC_ORIGIN`。

  ### 构建和部署
  - [`Makefile`](Makefile) - 项目范围的编排命令
  - [`scripts/`](scripts/) - 所有用于开发、构建和部署的自动化脚本
    - [`scripts/infra/`](scripts/infra/) - 基础设施管理 (PostgreSQL, Redis, MinIO, Meilisearch)
      - `up.sh` - 启动基础设施 + 自动初始化
      - `down.sh` - 停止基础设施
      - `status.sh` - 检查服务健康状态
      - `init-postgresql.sh` - 初始化数据库 schemas
      - `init-redis.sh` - 初始化 Redis 配置
      - `init-minio.sh` - 初始化 MinIO buckets
      - `init-meilisearch.sh` - 初始化 Meilisearch 索引
    - [`scripts/dev/`](scripts/dev/) - 开发模式脚本 (直接 Go/npm 进程)
      - `start.sh` - 启动完整开发环境 (基础设施 + 后端 + 前端)
      - `stop.sh` - 停止所有开发服务
      - `restart.sh` - 重启所有服务
      - `modtidy.sh` - 清理 Go 模块依赖
    - [`scripts/build/`](scripts/build/) - 编译和 Docker 镜像构建
      - `compile.sh` - 编译 Go 二进制文件 (go build)
      - `build-images.sh` - 构建 Docker 镜像 (docker build)
      - `package.sh` - 打包部署工件 (docker save/push)
      - `push-images.sh` - 推送镜像到仓库
    - [`scripts/local/`](scripts/local/) - 本地 Docker Compose 部署
      - `start.sh` - 通过 Docker Compose 启动完整平台
      - `stop.sh` - 停止 Docker 服务
      - `status.sh` - 查看容器状态和资源使用
    - [`scripts/prod/`](scripts/prod/) - 生产部署脚本
      - `start.sh` - 启动生产环境 (分步执行)
      - `stop.sh` - 停止生产服务
      - `health-check.sh` - 健康监控
      - `swarm/` - Docker Swarm 高可用部署
    - 完整脚本文档参阅 [`scripts/README.md`](scripts/README.md)

### Backend 与 Worker 启动顺序

System Backend 先完成公共 schema 初始化。各模块 Backend 并行迁移自身 schema 并达到 `/health/ready` 后，各自的 Worker 才启动；不同模块不互相等待。Compose 使用对应 Backend 的 `service_healthy` 条件，开发脚本使用按模块的并发等待任务。Worker 初始化不执行平台 schema DDL，独立启动时必须通过模块及公共 schema 版本校验。结构升级前先停止相关 Worker 领取任务并结束在途任务，禁止旧 Worker 与新 schema 混跑。

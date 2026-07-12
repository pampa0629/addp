## 快速开始
需要时，请阅读 docs/guide/addp部署和开发步骤.md。

### 前提条件

**需要 Docker 环境**: ADDP 平台需要安装 Docker 和 Docker Compose。

- 安装 Docker Desktop: https://www.docker.com/products/docker-desktop
- 验证安装: `docker --version` 和 `docker-compose --version`

### 第一步: 启动基础设施 (必须首先执行)

**重要**: 必须先启动基础设施服务 (PostgreSQL、Redis、MinIO、Meilisearch)。

```bash
# 从项目根目录
bash scripts/infra/up.sh
```

此脚本自动完成:
- 启动 PostgreSQL (addp-postgres)、Redis (addp-redis)、MinIO (addp-minio)、Meilisearch (addp-meilisearch) 容器
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

自动启动以下内容:
1. 基础设施 (如未运行)
2. 所有后端服务 (System、Manager、Meta、Transfer、Orchestrator、Develop、GeoPython Workflow Engine、Model3D Workflow Engine、PointCloud Workflow Engine)
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

SuperMap Workflow Engine 依赖宿主机私有 SuperMap iObjects Java Bin、GPA/SPS libs 和许可文件，默认全量启动和 `restart.sh -all` 会一并启动。使用全量启动前需先把许可文件放入 SuperMap Bin 目录，并配置 SDK / GPA libs 挂载路径；单独验证超图算子时也可以显式启动：

```bash
SUPERMAP_OBJECTSJAVA_BIN_HOST=/path/to/sup_iobjectsjava/Bin \
SUPERMAP_GPA_LIB_DIR_HOST=/path/to/scp-dc-scheduler/scheduler/gpa/libs \
SUPERMAP_DATA_HOST_PATH=/path/to/supermap/data \
bash scripts/dev/start.sh -supermap-workflow
```

### 第三步: 构建模式 (用于 Docker 镜像构建)

```bash
# 编译 Go 二进制文件
bash scripts/build/compile.sh

# 构建 Docker 镜像
bash scripts/build/build-images.sh

# 只构建三维模型转换运行时镜像（Apple Silicon 默认为 linux/arm64）
bash scripts/build/build-images.sh --services model3d-workflow-engine --force

# 只构建点云转换运行时镜像（内置 PDAL）
bash scripts/build/build-images.sh --services pointcloud-workflow-engine --force

# 打包并推送镜像 (如需要)
bash scripts/build/package.sh
```

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

`supermap-workflow-engine` 使用 Docker runtime 承载 Linux arm64 SuperMap SDK，不依赖宿主机 Linux OS。开发脚本只负责挂载本机 SDK / GPA libs 并启动运行时，不复制 SDK、native `.so` 或许可到仓库。可选变量：

```bash
SUPERMAP_WORKFLOW_PORT=8103
SUPERMAP_OBJECTSJAVA_BIN_HOST=/path/to/sup_iobjectsjava/Bin
SUPERMAP_GPA_LIB_DIR_HOST=/path/to/gpa/libs
SUPERMAP_DATA_HOST_PATH=/path/to/supermap/data
SUPERMAP_OUTPUT_HOST_PATH=/tmp/supermap-out
SUPERMAP_WORKFLOW_REBUILD=1  # 修改 engines/supermap-workflow 源码后强制重建镜像
```

Develop 正式任务向 NFS 输出 UDBX 时，不需要预先为某个 NFS 存储引擎配置 SuperMap 专用挂载目录。Develop 会在执行期把用户选择的 NFS 引擎连接事实和相对输出路径传给 `supermap-workflow-engine`；容器需要包含 `nfs-common` 并具备 Linux mount 权限，开发脚本和 Compose 已为该容器启用 `SYS_ADMIN` capability。

### 第四步: 本地 Docker Compose 模式

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
3. **启动业务后端** (Manager、Meta、Transfer、Orchestrator、Develop、GeoPython Workflow Engine、Model3D Workflow Engine、PointCloud Workflow Engine、SuperMap Workflow Engine、Gateway)
4. **启动前端服务** (所有模块前端 + Console + Nginx)
5. **健康检查** (验证所有服务就绪)

**访问地址** (部署完成后):

- **✨ Console 控制台 (推荐)**: http://localhost:80
  - 统一登录,一键访问所有模块
  - 通过 Nginx 反向代理,提供最佳用户体验
- **Console 独立访问** (开发调试): http://localhost:5170
- **API Gateway**: http://localhost:8000
- **独立模块访问** (如需单独访问):
  - System: http://localhost:8090
  - Manager: http://localhost:8091
  - Meta: http://localhost:8092
  - Transfer: http://localhost:8093
  - Orchestrator: http://localhost:8094
  - Develop: http://localhost:8095

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

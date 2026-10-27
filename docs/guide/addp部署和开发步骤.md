# ADDP 部署和开发步骤

本文介绍基础设施、开发、构建和部署的标准入口。新增模块的目录、配置、生命周期和前端接入统一见[新模块指南的阅读导航](../spec/addp新模块开发指南.md#阅读导航)，交付前按其[检查清单](../spec/addp新模块开发指南.md#检查清单)核对。

源码开发、本地产品容器与生产脚本的选择及停止范围见[开发与容器部署专题](../../scripts/docs/开发与容器部署.md)。

## 快速开始

### 前提条件

**需要 Docker 环境**: ADDP 平台需要安装 Docker 和 Docker Compose。

- 安装 Docker Desktop: https://www.docker.com/products/docker-desktop
- 验证安装: `docker --version` 和 `docker-compose --version`

### 第一步: 启动基础设施 (必须首先执行)

**重要**: 必须先启动基础设施服务 (PostgreSQL、Redis、FalkorDB、MinIO、Meilisearch 等)。FalkorDB 是 Ontology 的私有 Infra，已有环境须在根 `.env` 配置独立的 `INFRA_FALKORDB_PASSWORD`。

**旧搜索卷升级例外**：源码现固定 Meilisearch `1.54.3`；已有 `1.7.6` 数据卷不能通过下面的普通启动命令直接升级。应先完成 dump、独立新卷导入与回滚验证；Infra 备份默认不包含 Meilisearch，仅显式提供已成功导出的 dump 与任务 UID 时校验收录，不自动导出。标准入口与隔离演练见 [Infra 备份说明](../../scripts/infra/docs/备份恢复与搜索迁移.md#本地-infra-备份与隔离恢复演练)。操作边界见[旧 Meilisearch 卷迁移与回滚](../../scripts/infra/docs/备份恢复与搜索迁移.md#旧-meilisearch-卷迁移与回滚)，未经确认不得启停旧实例或切换卷。新建无历史数据的环境不受此例外影响。

```bash
# 从项目根目录
bash scripts/infra/up.sh
```

此脚本自动完成:
- 启动 PostgreSQL (addp-postgres)、Redis (addp-redis)、FalkorDB (addp-falkordb)、MinIO (addp-minio)、Meilisearch (addp-meilisearch) 等容器
- 默认宿主机端口被占用时自动选择空闲端口并打印实际地址；容器内部地址不变
- 初始化所有模块的 PostgreSQL schemas
- 初始化 MinIO buckets 和 Redis 配置
- 检查 Meilisearch API 健康；各 Owner 模块自行创建和配置业务索引

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

开发启停由工作区级生命周期锁保护。锁冲突、`keepalive.sh` 持锁与退出清理范围、停止归属统一见[停止与进程归属](../../scripts/docs/启动脚本使用指南.md#停止与进程归属)，完整契约见[生命周期规范](../spec/addp开发服务生命周期与构建身份规范.md#一生命周期互斥)。

开发生命周期安装 Node 依赖时以 `package-lock.json` 为不可变构建输入，并统一执行 `npm ci`；缺少锁文件时直接失败，锁文件生成只属于显式的依赖维护流程。因此本地启动和 Hosted Online 门禁不会在安装依赖时改写已跟踪的锁文件或污染构建身份。

需要启动的前端先完成依赖安装，再创建开发服务进程；安装失败直接结束启动。前端 HTTP 就绪检查的 60 秒等待从依赖准备完成后开始，避免冷启动的 `npm ci` 消耗服务就绪时间。已运行的前端沿用现有进程，不重新安装依赖。

启动脚本按 Compose 项目、服务名和容器健康状态检查 Infra；其他服务即使占用 ADDP 的首选端口，也不能被当作 ADDP Infra。实际宿主机映射由 Compose 查询并注入开发进程。Backend 仍须通过 `/health/ready`：Ontology 还需验证 PostgreSQL、FalkorDB 图能力及 System 注册全部就绪。

开发启动还会检查 Gateway、模块 Backend/Frontend 和工作流 Runtime 的首选端口。发生冲突时，启动输出会显示替代端口，实际地址以输出和 `.dev-state/ports.env` 为准；Console 的代理、iframe、API 文档和各前端 Gateway 代理同步使用该结果。

全量、单模块和多模块的启动、重启、停止命令统一见[启停操作速查](../../scripts/docs/启动脚本使用指南.md#选择操作)；复用运行中依赖、Swagger 预检与停止失败后的处理见[启动与重启边界](../../scripts/docs/启动脚本使用指南.md#启动与重启边界)。

核心 Infra 健康时，应用启动和重启直接复用现有容器，不构建 Infra 镜像、不执行 Compose 更新。日志或指标关闭不会触发 Infra 启动；所选观测能力异常会单独提示并返回非零，业务启动继续，需要修复设施时显式运行 `bash scripts/infra/up.sh`。

模块依赖清单和 Asset 等例外统一见[模块选择启动指南](../../scripts/docs/模块选择启动指南.md#如何合并依赖)。普通模块选择保留 System Backend、Gateway 和 Console；Meta 和其他业务依赖按选择结果合并，不统一拉起全部模块前端。Engine Runtime 需[显式选择](../../scripts/docs/模块选择启动指南.md#显式选择-runtime-联调)，无参数全量启动和 Manager 自有 Raster Mosaic 的规则也见该节。

SuperMap Workflow Engine 开发与产品部署均使用与 SDK 平台匹配的 Docker 环境；SDK 母版、两层镜像、构建指纹和运行参数统一见[SuperMap README](../../engines/supermap-workflow/README.md#本地依赖与两层镜像)。首次安装或更新许可时须准备或重建基础镜像，许可限制见[许可边界](../../engines/supermap-workflow/README.md#许可边界)；日常启动和重启使用：

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

# 只构建点云转换运行时镜像（内置 PDAL）
make build-images IMAGE_BUILD_ARGS="--services pointcloud-workflow-engine --force"

# 打包并推送镜像 (如需要)
bash scripts/build/package.sh
```

`make build` 与 `make build-images` 是平台唯一标准构建入口，分别薄调用 `scripts/build/compile.sh` 与 `scripts/build/build-images.sh`。`make build BUILD_ARGS="--arch amd64 --services system-backend,gateway"` 可精确构建指定服务；Go 产物按实际依赖指纹判断缓存并写入构建身份，供 Online 预检核对。

代码交付前默认使用统一影响分析入口，不要等推送后的 CI 通知才补测试或构建适配：

```bash
# 当前工作区：包含已跟踪和未跟踪改动
make test-changed

# 一段已提交变更
make test-changed BASE_REF=<ref>

# 明确验证单个 owner 的 T0-T3 门禁
make test-module MODULE=<模块>
```

共享 Go、前端和 Python 代码会根据仓库内真实依赖扩散到消费者。`make test-platform` 校验构建与测试登记完整性；新增模块的脚本、构建、测试及 CI 接入按[新模块指南](../spec/addp新模块开发指南.md#后端接入步骤)在同一次变更中完成。GitHub Actions 在 `main` push 后和每日定时任务中使用独立 Runner 复验；本地 `start.sh`、`restart.sh`、`git commit` 均不会触发 CI。

GeoPython、Spark、Model3D、PointCloud 和 Document Workflow 在开发模式下使用原生进程，便于调试和快速迭代；产品部署保留正式容器镜像。各引擎的平台支持和依赖要求见下文及模块 README。

GeoPython Workflow 开发需要原生 Python 3.12、GDAL、MDBTools 和 unixODBC。依赖准备与产品镜像入口见 [GeoPython 运行说明](../../engines/geopython-workflow/docs/geopython-workflow容器管理指南.md)。需要重启时，由用户执行 `./scripts/dev/restart.sh -geopython-workflow`；开发入口仅管理原生进程，拒绝接管外部监听者。

Raster Mosaic Runtime 归 Manager 管理，随普通 Manager 启停入口运行；开发使用原生进程，不以 Docker 镜像兜底。环境隔离、GDAL 准备和依赖同步规则统一见[生命周期规范](../spec/addp开发服务生命周期与构建身份规范.md#三运行时依赖构建输入)。需要更新依赖时，由用户执行 `./scripts/dev/restart.sh -manager`，先停止所属 Runtime 再准备；运行中或端口被占用时不得改写环境。

Spark Workflow 的依赖准备、原生启动与产品镜像入口统一见 [Spark README](../../engines/spark-workflow/README.md#配置与启动)。macOS 需在 `/etc/hosts` 配置 `127.0.0.1 host.docker.internal`，Docker Worker 保留内置解析，确保 Driver 与 Worker 都能访问相同的数据端点和 Driver 动态端口。该方式无需启用 Docker Desktop host networking。修改源码后，由用户执行 `./scripts/dev/restart.sh -spark-workflow`；完整重启时序与失败处理见[生命周期规范](../spec/addp开发服务生命周期与构建身份规范.md#一生命周期互斥)。

Model3D Workflow 的原生开发、产品镜像构建与挂载配置统一见 [Model3D README](../../engines/model3d-workflow/README.md)。容器部署时，NFS/localfs 数据根必须挂载到 Runtime，且 Manager 传给 operator 的本地路径在容器内仍可访问。

Manager 的 GLB 快显 artifact 统一使用 ADDP infra MinIO 配置，不另设 `model3d_workflow` 专用 endpoint。Compose 部署时，Manager 与 Runtime 同在 Compose 网络内，统一访问 `minio:9000`；macOS 原生开发时，两者统一访问实际 Infra MinIO 宿主机端口。

PointCloud Workflow 的依赖准备、原生启动与产品镜像入口统一见 [PointCloud README](../../engines/pointcloud-workflow/README.md)。开发服务使用独立 Conda 前缀，PDAL、GDAL、PROJ、E57 原生库与资源目录属于该环境，不继承 Homebrew 或 Conda base 的资源、插件路径；运行中只校验环境，不修改依赖。

原生开发直接读取宿主机 NFS 路径；COPC 先写入 `POINTCLOUD_WORK_HOST_PATH` 指定的受控工作目录（默认 `data/pointcloud-work`），再按 access plan 发布到目标存储，目录须有足够磁盘空间。需要更新依赖时，由用户执行 `./scripts/dev/restart.sh -pointcloud-workflow`，先停止所选 Runtime 再准备；准备失败不启动。

Document Workflow 的依赖准备、原生启动与产品容器说明统一见 [Document Workflow README](../../engines/document-workflow/README.md#启动)。开发态只处理可信文件，以非 root 开发用户运行；独立 venv、LibreOffice profile 和临时目录不提供文件系统沙箱。产品 Compose 保留正式容器镜像及非 root、只读根文件系统和能力限制。需重启时，由用户执行 `./scripts/dev/restart.sh -document-workflow`；检测到同名 `document-workflow-engine` 容器或外部监听者时，开发入口拒绝启动。

点云 COPC artifact 统一使用 ADDP infra MinIO 配置。原生开发直接访问 access plan 中的宿主机实际端点；产品 Compose 使用其 Docker 网络端点。

Develop 向 NFS 输出 UDBX 时，仍在执行期向 Runtime 传入所选 NFS 引擎连接事实和相对输出路径，不预设 SuperMap 专用挂载目录；NFS 依赖及 Linux mount、`SYS_ADMIN` 等容器挂载权限统一按[SuperMap README](../../engines/supermap-workflow/README.md#本地依赖与两层镜像)配置。

### 第四步: 本地 Docker Compose 模式

本地和生产容器部署使用 `addp-infra`、`addp-platform`、`addp-runtimes` 三个 ADDP Compose project；业务数据引擎保留独立 `business` project。平台模块共用 `addp-platform` 生命周期，内置 Runtime 由 `docker-compose.runtimes.yml` 独立启动；所有 ADDP project 在外部 `addp-network` 上以服务名互访。Docker Desktop 中看到的 project 分组不等于 System 模块或引擎注册表。

启停前核对容器的 Compose project 与工作区归属；固定 `container_name` 不能由多个 project 同时占用。平台与 Runtime 的标准停止入口保留数据卷，不删除 `addp-infra` 或 `business` 的数据卷；Infra 卷删除边界见[部署与项目隔离](../../scripts/infra/docs/部署与项目隔离.md#数据持久化)。

源码开发模式中的容器化 Runtime 仍由 `scripts/dev/` 启停，其项目标签用于 Docker Desktop 分组，不以生产 Compose 命令接管。

```bash
# 通过 Docker Compose 启动完整平台
bash scripts/local/start.sh

# 检查状态
bash scripts/local/status.sh

# 停止平台服务与内置 Runtime（保留 Infra 和数据卷）
bash scripts/local/stop.sh
```

### 第五步: 生产模式

**一键生产部署**:

```bash
# 从项目根目录
bash scripts/prod/start.sh
```

首次部署的配置准备和服务启动顺序统一见 Prod README 的[首次部署](../../scripts/prod/README.md#首次部署)与[启动流程](../../scripts/prod/README.md#启动流程)。[健康检查](../../scripts/prod/README.md#health-checksh)仅核对所检查容器的运行/健康状态与 Nginx `/health` 可达性，不替代所需业务功能和数据结果验收；验收范围与标准入口见[测试与验收规范](../spec/addp测试与验收规范.md)。

**访问地址**：公开 origin 和根 `.env` 配置见[配置规范的环境变量参考](../spec/configuration/环境变量参考.md#根目录-env)；默认访问地址、Nginx 端口及监听范围见[端口规范](../spec/addp端口分配.md)。Console、模块前端和 `/api/` 统一经 Nginx 访问，容器内仍使用稳定服务名和端口，不额外发布应用模块宿主机端口。域名、HTTPS 或上级反向代理部署须将 `ADDP_PUBLIC_ORIGIN` 设为用户实际访问的 origin；`NGINX_BIND_HOST` 单独控制宿主机监听范围，隔离验收限制为 `127.0.0.1`。

### 构建和部署

各模式的主要执行命令见上文；目录职责、脚本参数与专题入口统一从[脚本总入口](../../scripts/README.md#目录入口)查阅，平台编排命令见[根 Makefile](../../Makefile)。

现存 Swarm 脚本的部署缺口与操作范围见[生产指南](../../scripts/prod/README.md#swarm)。

### Backend 与 Worker 启动顺序

Backend 与 Worker 的迁移职责、就绪依赖和并行启动规则统一见[生命周期规范](../spec/addp开发服务生命周期与构建身份规范.md#模块迁移与-worker-启动顺序)。结构升级前，必须先停止相关 Worker 领取任务并结束在途任务，再执行迁移和恢复；禁止旧 Worker 与新 schema 混跑。

## 模块实例运行日志部署

应用通过标准生命周期入口接入运行日志；设施组件、初始化、启停、状态检查、备份范围和验证入口统一见 [Infra 运行日志专题](../../scripts/infra/docs/运行日志.md#模块实例运行日志)。

System 平台系统管理员可在“模块管理 → 服务实例 → 查看日志”按实例查看。权限迁移会失效受影响管理员的旧授权会话，需要重新登录。应用重启不删除日志 Infra；Docker 和本机开发都写受控节点源目录，不通过 Docker Socket 或远端任意路径读取日志。普通 `docker logs` 主要用于容器入口和日志设施自身错误，模块正文以本入口为准。

集中日志默认保留 7 天；节点源默认最长保留 48 小时，并受磁盘额度限制。日志可能丢失，不承诺零丢失归档或自动备份；限额、清理与已知缺口见[运行日志规范](../spec/addp模块服务运行日志规范.md#三设施容量与可靠性)。

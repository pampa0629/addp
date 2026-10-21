# ADDP 开发脚本

本目录包含所有与 ADDP 开发模式相关的启动、停止、重启脚本。日常操作选择见[启动脚本使用指南](../docs/启动脚本使用指南.md)。

## 核心原则

遵循以下7个原则(与 scripts/infra/ 一致):

1. **单一职责**: 同样功能只在一处实现
2. **适应性**: 自动适配不同环境
3. **清晰明了**: 一看就懂的结构和命名
4. **可重复执行**: 幂等性,多次执行不会破坏系统
5. **易用性**: 用户无需了解技术细节
6. **分散和集中**: 模块配置分散,脚本集中在 scripts/
7. **敢于删除**: 删除重复或无用内容

## 目录结构

```
scripts/dev/
├── README.md          # 本文件
├── start.sh           # 启动完整开发环境(后台运行)
├── process-metrics-launch.py # 按具体启动单元注入可选采集配置并 exec 标准接收器
├── stop.sh            # 停止所有开发服务
├── stop-exact-process.sh # 仅供 addp-online T4 精确停止正式进程
├── restart.sh         # 智能重启服务；纯扩展服务参数支持局部重启
├── keepalive.sh       # 托管命令环境前台保活入口
├── document-workflow.sh # Document 原生依赖准备、版本检查与 PID 启动
├── modtidy.sh         # 清理 Go 模块依赖
├── clean.sh           # 清理开发环境编译产物
└── install-frontend-deps.sh # 安装前端依赖
```

## 使用方法

### 快速启动(推荐)

```bash
# 一键启动完整环境(基础设施 + 后端 + 前端)
bash scripts/dev/start.sh

# 开发首选地址（实际地址以启动输出和 .dev-state/ports.env 为准）
# - Console: http://localhost:5170
# - System: http://localhost:5173
# - Manager: http://localhost:5174
# - API Gateway: http://localhost:8000
```

### 单独脚本使用

```bash
# 启动开发环境
bash scripts/dev/start.sh
# 按下文 start.sh 的依赖、启动与就绪流程执行

# 停止所有服务
bash scripts/dev/stop.sh
# 自动执行:
# 1. 合并 PID 文件与工作区残留监听者
# 2. 停止其他模块和 Runtime，保留 System 供注销
# 3. 最后停止 System，清理 Vite 缓存和 PID 文件

# 重启服务
bash scripts/dev/restart.sh
# 等同于: stop.sh + start.sh

# 在 Codex 等托管命令环境中启动并前台保活
bash scripts/dev/keepalive.sh restart -orchestrator
# Ctrl+C 退出时会自动调用 stop.sh 清理服务

# 清理 Go 依赖
bash scripts/dev/modtidy.sh
# 对所有 Go 模块执行 go mod tidy
```

## 脚本详细说明

### 开发进程采集计划

根 `.env` 的 `ADDP_DEV_PROCESS_METRICS_PLAN` 按具体启动单元填写独立的五字段部署对象。当前每种 Worker 只启动一个进程，单元名为 `meta-worker`、`transfer-bounded-worker` 和 `transfer-continuous-worker`；后端使用原模块进程名。不同单元可以属于同一个 module/role，但必须分配不同监听地址、采集端点和仓库外证书目录。

标准入口在创建 Go 子进程时调用 `process-metrics-launch.py`，只向该进程注入选中项对应的 `ADDP_PROCESS_METRICS_DEPLOYMENTS` 单项数组，再以相同 PID exec 原日志接收器。没有选择时显式注入空值，根环境补齐不会覆盖它。根 `.env` 中旧 SDK 数组须删除，配置错误只关闭可选采集并报告 `dev_process_metrics_plan_invalid`，不阻断业务启动。端点及证书有效性仍由 Common SDK 唯一校验。

计划不是多副本启动器：未来同类型多副本必须有各自启动单元，由部署入口分别注入；不能让多个副本继承同一来源，也不以配置键代替运行实例 ID。容器部署仍直接注入各自 SDK 输入，不消费开发计划。格式及边界见 [配置规范](../../docs/spec/addp配置介绍.md)。

### start.sh

**功能**: 启动完整开发环境

指定模块时，按所选模块的依赖并集形成启动清单，复用运行中的依赖、补齐缺失项。具体模块边界见[生命周期规范](../../docs/spec/addp开发服务生命周期与构建身份规范.md#一生命周期互斥)，例如单独选择 Asset 不启动 Meta Backend/Worker。Engine Runtime 不属于模块启动依赖：`-manager`、`-develop`、`-service`、`-agent`、`-copilot`、`-gateway`、`-console` 都不会隐式拉起 DuckDB、Inference、Workflow 或 Jupyter Runtime；需要本地运行某个 Runtime 时使用其显式参数。无参数全量启动仍包含默认部署的 Runtime。SuperMap Workflow Engine 依赖预先构建的 iObjects C++ 基础镜像和许可，当前通过 System 引擎管理手动登记。

`--exact-process` 与 `--wait-live` 不是日常开发参数，只允许 `ADDP_ONLINE_HOST=1` 的专用 Runner 使用。它们复用同一构建和正式二进制入口，只为 T4 控制 Manager → System → Gateway 的真实乱序；`stop-exact-process.sh` 同样受专用主机开关保护，并在发信号前验证 PID 对应当前 checkout 的目标 ADDP 二进制。`stop-exact-process.sh --force -manager` 直接发出 SIGKILL，用于异常退出验收；省略 `--force` 时先发送 SIGTERM，等待优雅退出。强制停止仅用于专用 Runner 的故障阶段。普通模块启动仍只有上述自动依赖路径。

**执行步骤**:
1. 检查 Go 依赖（`go mod tidy`，可跳过）。
2. 检查核心 Infra；未就绪时调用 `scripts/infra/up.sh`，已就绪则复用。所选观测能力异常单独报告，业务继续启动但命令返回非零；显式执行 `up.sh` 才应用设施配置或修复观测设施。
3. 解析实际开发端口并保存到 `.dev-state/ports.env`；启动 System 和所选模块 Backend，各模块 Backend `/health/ready` 成功后启动其 Worker。
4. 启动显式选择的 Runtime、Copilot 和 Agent，全部成功后启动 Gateway 与前端。无参数全量启动包含默认 Runtime；Engine Runtime 按各自协议检查 `/health`。
5. 输出本次服务清单、实际访问地址与 PID。

Develop Backend 内嵌 Query Execution Supervisor，领取全部 query execution。

**环境变量**:
```bash
# 跳过 Go 依赖检查(节省5-10秒启动时间)
SKIP_MODTIDY=1 bash scripts/dev/start.sh
```

**日志位置**:

已登记模块正文从“System → 模块管理 → 服务实例 → 查看日志”读取；有效可信未登记来源使用“未登记实例的运行日志”入口，均需 Platform 上下文及 `platform.module_log.read`。下列受控节点源用于本地诊断，不从路径推断 System 可信身份；引擎、前端和容器入口输出不等同于模块正文。完整契约见[日志规范](../../docs/spec/addp模块服务运行日志规范.md#四可信来源目录)。

- 模块服务运行日志: `logs/runtime/<module>/<instance_id>/*.jsonl`；引擎和前端启动输出保留各自日志
- 示例: `logs/runtime/system/<instance_id>/*.jsonl`、`logs/runtime/manager/<instance_id>/*.jsonl`；Worker 同样按独立实例分目录

**PID 文件**:
- 所有 PID: `.dev-pids/*.pid`
- 示例: `.dev-pids/system-backend.pid`

### stop.sh

**功能**: 停止所有开发服务

**执行步骤**:
1. 合并 PID 文件和一次批量端口扫描中的工作区监听者，按 PID 去重
2. 卸载其他模块的 launchd 作业，向其他模块、Worker 和前端发送 TERM
3. 等待最多 5 秒，超时才发送 KILL；System 保持可用，接收实例注销
4. 对工作区 Runtime 容器先执行 `docker stop -t 5`，再删除容器
5. 最后卸载 System 的 launchd 作业、停止 System Backend，独立等待最多 5 秒
6. 清理 Vite 缓存和 PID 文件

**安全性**:
- launchd 作业必须同时满足 `com.addp.codex.*` 标签和当前仓库绝对路径才会被卸载
- PID 文件用于精确停止正常启动的服务，进程名和端口检查用于清理 ADDP 残留进程
- 端口清理只执行一次批量 `lsof` 查询，限定目标 TCP 端口和 `LISTEN` 状态；按 PID 去重后逐个核验身份，不把浏览器等客户端连接当作监听服务，也不终止非 ADDP 监听进程
- launchd 作业卸载失败时返回非零状态，`restart.sh` 会中断，不会带着残留进程继续启动

### restart.sh

**功能**: 智能重启服务。

- 无参数或 `-all` 时先 `stop.sh`，再同步 Swagger 并执行 `start.sh`；指定模块时只停止并启动所选模块。
- 全局重启通过 `stop.sh` 统一停止 Python、Go 和 Runtime，保留 System 供注销；只有优雅退出超时后才强制终止工作区所属进程。
- 所有局部重启统一通过 `stop.sh -<模块> ...` 停止所选服务，再通过 `start.sh -<模块> ...` 启动；运行中的公共依赖复用，缺失的依赖由启动入口补齐。停止失败时不进入准备和启动阶段。
- Go Backend 和 Worker 在构建检查前分别核验进程状态；已经运行的受管服务跳过指纹检查和编译，仅对需要启动的进程按需构建。编译后、启动前重新核验端口和进程归属。单独重启 Runtime 不会顺带编译仍在运行的 Meta Backend/Worker。
- `-all`、无参数和指定 Go 模块参数保留已有二进制及 Go 包缓存。Swagger 同步后，统一比较完整构建指纹：源码、共享依赖、嵌入资源、Go 版本、平台或构建参数变化才重新编译。服务进程仍全部按所选范围重启，未变化产物保留构建身份，新进程具有新的启动时间。
- Swagger 对未变化的 Go 工作区输入和完整产物复用文档，输入或产物变化时重新生成；FastAPI 实时导出。需要生成的模块批量并行执行，并输出耗时，路由覆盖校验仍执行。所有生成任务结束后才进入编译，因为 `docs.go` 本身参与 Go 编译，不能与同一模块的生成任务同时执行。
- Go 后端就绪后，所选 Runtime、Copilot 和 Agent 的准备、启动与健康等待并行执行；全部任务成功后再启动 Gateway 和前端，并输出各任务及整体耗时。Python 依赖安装单独加锁，避免同时写入共享 `common-python` 元数据，已安装环境的检查和启动仍可并行。Spark、GeoPython、Model3D、PointCloud、Document 和 Raster Mosaic 仅在依赖输入或安装环境指纹变化、记录缺失或 `pip check` 失败时重新安装；共享同步入口只安装和追踪调用方显式声明的 editable 本地包，Raster Mosaic 不依赖 `common-python`；相同输入且依赖一致时输出“依赖未变化，跳过安装”。首次使用该机制仍需同步一次并生成成功记录，后续启动复用该记录。所有依赖同步与原生转换验证均在所选服务停止后进行；准备失败的服务不启动，入口返回非零。
- Go 后端和前端在各自启动阶段各查询一次 TCP LISTEN 快照，命中占用后实时复核；单项启动实时查询。端口冲突和扫描失败会中断启动，新进程退出不能被其他监听者的 HTTP 成功响应掩盖。
- 重启编排和缓存保留通过 `make test-dev-lifecycle` 验证，并纳入 `make test-platform` / Platform CI。

**实现**:
```bash
# 局部重启 Manager
bash scripts/dev/restart.sh -manager

# 局部重启扩展服务
bash scripts/dev/restart.sh -geopython-workflow
bash scripts/dev/restart.sh -math-workflow
bash scripts/dev/restart.sh -model3d-workflow
bash scripts/dev/restart.sh -pointcloud-workflow
bash scripts/dev/restart.sh -document-workflow
bash scripts/dev/restart.sh -supermap-workflow
bash scripts/dev/restart.sh -spark-workflow
bash scripts/dev/restart.sh -jupyter
bash scripts/dev/restart.sh -copilot
bash scripts/dev/restart.sh -agent
```

GeoPython Workflow 使用原生 Python 3.12、GDAL 和 MDBTools/ODBC；准备与启动统一在 `geopython-workflow.sh`，虚拟环境不继承系统包。macOS 先安装 `brew install python@3.12 gdal mdbtools pkg-config`。局部与全量重启均先通过 stop.sh 停止所选服务，再准备依赖并验证原生能力；首次切换须由用户先停掉旧开发容器。开发入口不构建或启动 GeoPython 镜像，产品栅格 Hosted 验收通过根构建入口独立完成。

SuperMap Workflow 首次使用或升级 iObjects C++ SDK、许可时，通过 `SUPERMAP_CPP_SDK_PATH` 指向完整 SDK 母版并运行 `bash scripts/build/build-supermap-workflow-base.sh` 构建稳定基础镜像。之后 `restart.sh -supermap-workflow` 和 `restart.sh -all` 根据构建指纹决定是否重新编译当前 C++ 源码并替换 8103 容器，不需要 rebuild 开关，也不挂载宿主机源码或 SDK 目录。

**使用场景**:
- 修改代码后需要重启
- 服务异常需要重置
- 只调整 Python/扩展服务时，避免影响正在运行的 Go 后端服务
- PointCloud Workflow 使用独立 Conda 前缀中的 Python/PDAL 原生进程；局部重启先通过 stop.sh 停止所选服务，再由 start.sh 同步依赖、验证驱动和真实 COPC 写入并启动。开发入口不构建镜像，首次迁移需先移走旧普通 venv 并停止旧开发容器。
- Document Workflow 使用独立 Python 3.12 venv、私有官方 LibreOffice 和固定中文字体；局部重启先通过 stop.sh 停止所选服务，再由 start.sh 准备依赖、检查完整性和版本并启动，核对 PID、监听归属及 HTTP 健康状态。真实三页中文 PDF 转换由 `make test-document-native` 和 CI 独立验证。开发态只处理可信文件，首次迁移需先停止、删除旧开发容器；生产保留容器隔离。

**Infra 处理**：开发重启保留健康的核心 Infra；启动入口只在核心未就绪时调用 `scripts/infra/up.sh`。所选观测能力异常单独报告，不触发健康核心设施重建，业务继续启动但命令返回非零。设施配置应用或故障处理见[Infra 生命周期指南](../infra/docs/生命周期脚本.md)，按实际问题选择操作。

### keepalive.sh

**功能**: 为 Codex 等托管命令环境提供前台保活入口。

**背景**: `start.sh` 和 `restart.sh` 会以后台进程方式启动开发服务。在普通终端中脚本退出后后台服务会继续运行；但 Codex 等托管命令执行环境可能会在命令结束时回收其派生的后台进程，导致脚本内健康检查通过、脚本退出后端口立刻不可用。

**使用方式**:
```bash
# 重启指定模块并保持命令前台运行
bash scripts/dev/keepalive.sh restart -orchestrator

# 启动指定模块并保持命令前台运行
bash scripts/dev/keepalive.sh start -system

# 全量重启并保持命令前台运行
bash scripts/dev/keepalive.sh restart -all
```

**退出行为**: 按 `Ctrl+C` 或终止该命令时，`keepalive.sh` 会调用 `scripts/dev/stop.sh` 清理开发服务。

**适用边界**: 普通本地终端仍优先使用 `start.sh` / `restart.sh`；`keepalive.sh` 只用于命令会话结束后会回收后台进程的托管执行环境。

### modtidy.sh

**功能**: 清理所有 Go 模块的依赖

**执行模块**:
- common
- system/backend
- manager/backend
- meta/backend
- transfer/backend
- orchestrator/backend
- develop/backend
- gateway

**何时使用**:
- 切换 Git 分支后
- 拉取最新代码后
- 添加/删除 Go 依赖后
- 解决 `go.mod` 冲突后

**幂等性**: 可多次执行,不会破坏依赖

## 启动依赖关系

模块参数按依赖并集合并启动清单。System Backend、Gateway 和 Console 构成公共入口；Meta 是否启动、后端依赖是否同时带前端，以及显式 Runtime 的选择规则统一见[模块选择启动指南](../docs/模块选择启动指南.md)。

- 核心 Infra 就绪后启动 System；各模块 Backend 就绪后启动自己的 Worker，模块之间仍并行。
- Manager/Meta/Transfer 通过 **System** 获取 AuthContext、Engine 和模块目录等 System-owned 事实；各模块普通运行配置不从 System 加载。
- Gateway 在本次选择的后端启动阶段完成后启动，业务路由使用 System 的实例发现；前端按本次选择的清单启动。
- Engine Runtime 使用显式参数选择，无参数全量启动包含默认 Runtime；不由模块名称推断应启动全部计算引擎。

具体就绪、迁移和停止约束见[生命周期规范](../../docs/spec/addp开发服务生命周期与构建身份规范.md)。

## IAM 首次初始化

开发和生产环境都不创建默认管理员、默认租户或共享弱密码账号。首次平台系统管理员、安全管理员和审计管理员使用同一套一次性离线 Bootstrap 流程建立；开发环境不保留绕过三员分立的简化路径。

## 故障排查

### 服务启动失败

先检查启动终端的失败信息及 Infra 状态，再读取当前工作区的端口分配与进程记录：

```bash
bash scripts/infra/status.sh
sed -n '/^SYSTEM_BACKEND_PORT=/p' .dev-state/ports.env
ps -p "$(cat .dev-pids/system.pid)" -o pid=,command=
```

PID 文件或端口记录存在不代表服务已就绪；结合实际进程命令行核对工作区归属。已登记实例与有效可信未登记来源均可从 System 的相应入口查询日志；未登记不等于启动失败，目录存在不代表正文已送达。尚未纳入目录或页面不可用时，可结合受控节点源 `logs/runtime/system/<instance_id>/*.jsonl` 及启动终端输出诊断，不从路径推断 System 可信身份。修正原因后，从项目根目录使用 `bash scripts/dev/restart.sh -system` 重启，不绕过标准入口手动运行后端。

### 健康检查超时

先按上节核对进程，再从本工作区状态读取实际端口检查 Backend readiness：

```bash
SYSTEM_BACKEND_PORT=$(sed -n 's/^SYSTEM_BACKEND_PORT=//p' .dev-state/ports.env)
curl -fsS "http://127.0.0.1:${SYSTEM_BACKEND_PORT:?未找到本工作区实际端口}/health/ready"
```

不要用首选端口上的其他服务响应代替本次进程就绪。失败时结合对应实例日志定位原因；基础设施状态与端口归属遵循[端口规范](../../docs/spec/addp端口分配.md)。

### 前端启动失败

```bash
# 1. 检查 Node.js 版本（必须与根目录 .node-version 声明的 Node.js 24 一致）
node -v

# 2. 重新安装依赖
cd system/frontend
rm -rf node_modules
npm install

# 3. 返回项目根目录，由标准入口重启 System 后端与前端
cd ../..
bash scripts/dev/restart.sh -system
```

### PID 文件残留

从项目根目录重新运行 `bash scripts/dev/stop.sh`，由标准入口合并 PID 记录和工作区残留监听者并核验归属。失败时保留错误与 PID 证据，先排查归属或生命周期锁；不要先删除 `.dev-pids/`，也不要按通用进程名执行 `pkill`。停止顺序与互斥要求见[生命周期规范](../../docs/spec/addp开发服务生命周期与构建身份规范.md)。

## 开发工作流

### 日常开发

```bash
# 1. 启动开发环境
bash scripts/dev/start.sh

# 2. 修改代码
vim system/backend/internal/service/user_service.go

# 3. 重启服务(自动重新编译)
bash scripts/dev/restart.sh

# 4. 在 System → 模块管理 → 服务实例 → 查看日志中排查对应实例

# 5. 停止环境
bash scripts/dev/stop.sh
```

### 添加新依赖

```bash
# 1. 添加依赖
cd system/backend
go get github.com/some/package
cd ../..

# 2. 清理依赖
bash scripts/dev/modtidy.sh

# 3. 重启服务
bash scripts/dev/restart.sh
```

### 同步 main 后更新依赖

人工与 AI 开发均直接使用 `main`。同步代码后如需更新依赖或重启，先停止受影响的服务，再从项目根目录执行标准依赖整理和启动入口，不建立功能分支。依赖变更步骤见[添加新依赖](#添加新依赖)。

## 相关文档

- [scripts/infra/README.md](../infra/README.md) - 基础设施脚本文档
- [CLAUDE.md](../../CLAUDE.md) - 项目整体架构
- [开发服务生命周期与构建身份规范](../../docs/spec/addp开发服务生命周期与构建身份规范.md) - 启动、停止、模块迁移与 Worker 顺序

Backend 独占所属模块迁移；Worker 初始化不执行平台 schema DDL。全量启动按模块等待 `/health/ready` 后启动该模块 Worker，各模块仍并行。公共执行与心跳表由 System Backend 初始化，单独 Worker 只读检查 schema 版本。数据库结构变化须同步递增 owner 的 `SchemaVersion`。

Model3D Workflow 的 macOS Apple Silicon 开发启动与重启共用 `model3d-workflow.sh`，绑定 `.dev-state/model3d-native` 的固定版本私有工具包，不使用开发 Docker 转换 wrapper。首次准备需要 Conda、Rust 1.92.0、CMake 4.4.0 和 Xcode Command Line Tools，后续复用已验证缓存。产品构建与 Hosted 验收保留正式 Linux Runtime 镜像。

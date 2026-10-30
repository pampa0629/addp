# ADDP 开发脚本

本目录维护开发脚本的实现细节、专用 Runner 接口和依赖变更流程。日常启停与排障见[启动脚本使用指南](../docs/启动脚本使用指南.md#选择操作)，模块参数、依赖组合与选择器例外见[模块选择指南](../docs/模块选择启动指南.md#选择一个或多个模块)。

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
├── doctor.sh          # 新机器 Node/npm 只读诊断，不安装或启停服务
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

### 新机器准备与诊断

先按[Node 工具链准备流程](../../docs/guide/addp部署和开发步骤.md#node-工具链准备与诊断)安装根 `.node-version` 声明的稳定主版本及 npm，再运行 `make dev-doctor`。该入口复用 `scripts/utils/node-runtime.sh`，报告当前命令实际使用的路径与版本；不会读取业务配置、修改 Shell、安装依赖或启停服务，也不把 Node/npm 检查通过当成其他 Runtime 或服务就绪。多版本机器的每条启动、测试和构建命令都须使用同一正确环境。

### 快速启动(推荐)

全量及所选模块的启停命令统一见[操作速查](../docs/启动脚本使用指南.md#选择操作)。访问地址以启动输出和 `.dev-state/ports.env` 为准；首选端口、实际端口与公开入口遵循[端口规范](../../docs/spec/addp端口分配.md)。

### 单独脚本使用

按下文的 [start.sh](#startsh)、[stop.sh](#stopsh) 和 [restart.sh](#restartsh) 说明核对实现约束；通用失败处理见[启动与重启边界](../docs/启动脚本使用指南.md#启动与重启边界)与[停止及进程归属](../docs/启动脚本使用指南.md#停止与进程归属)。托管环境保活按 [keepalive.sh](#keepalivesh) 的许可和退出范围使用，依赖整理见 [modtidy.sh](#modtidysh)。

## 脚本详细说明

### 开发进程采集计划

根 `.env` 的 `ADDP_DEV_PROCESS_METRICS_PLAN` 按具体启动单元填写独立的五字段部署对象。Meta、Quality 及 Transfer bounded、continuous 四类 Worker 默认各一个、可各选两个进程，单元名分别为 `meta-worker-1/-2`、`quality-worker-1/-2`、`transfer-bounded-worker-1/-2` 和 `transfer-continuous-worker-1/-2`；后端使用原模块进程名。不同单元可以属于同一个 module/role，但必须分配不同监听地址、采集端点和仓库外证书目录。

标准入口在创建 Go 子进程时调用 `process-metrics-launch.py`，只向该进程注入选中项对应的 `ADDP_PROCESS_METRICS_DEPLOYMENTS` 单项数组，再以相同 PID exec 原日志接收器。没有选择时显式注入空值，根环境补齐不会覆盖它。根 `.env` 中旧 SDK 数组须删除，配置错误只关闭可选采集并报告 `dev_process_metrics_plan_invalid`，不阻断业务启动。端点及证书有效性仍由 Common SDK 唯一校验。

副本数量由 [标准生命周期规范](../../docs/spec/addp开发服务生命周期与构建身份规范.md#metatransfer-与-quality-worker-启动单元) 中的四个部署变量分别选择，采集计划只为已选择的编号单元注入配置；不能让多个副本继承同一来源，也不以配置键代替运行实例 ID。容器部署仍直接注入各自 SDK 输入，不消费开发计划。格式及边界见 [配置规范](../../docs/spec/addp配置介绍.md)。

### start.sh

**功能**: 启动完整开发环境

依赖并集、前后端组合、Gateway/Console 选择器和显式 Runtime 的范围统一见[模块选择指南](../docs/模块选择启动指南.md#如何合并依赖)及[Runtime 联调](../docs/模块选择启动指南.md#显式选择-runtime-联调)。SuperMap Workflow Engine 依赖预先构建的 iObjects C++ 基础镜像和许可，当前通过 System 引擎管理手动登记。

`--exact-process` 与 `--wait-live` 不是日常开发参数，只允许 `ADDP_ONLINE_HOST=1` 的专用 Runner 使用。它们复用同一构建和正式二进制入口，只为 T4 控制 Manager → System → Gateway 的真实乱序；`stop-exact-process.sh` 同样受专用主机开关保护，并在发信号前验证 PID 对应当前 checkout 的目标 ADDP 二进制。`stop-exact-process.sh --force -manager` 直接发出 SIGKILL，用于异常退出验收；省略 `--force` 时先发送 SIGTERM，等待优雅退出。强制停止仅用于专用 Runner 的故障阶段。普通模块启动仍只有上述自动依赖路径。

**执行步骤**:
1. 检查 Go 依赖（`go mod tidy`，可跳过）。
2. 检查核心 Infra；未就绪时调用 `scripts/infra/up.sh`，已就绪则复用。所选观测能力异常单独报告，业务继续启动但命令返回非零；显式执行 `up.sh` 才应用设施配置或修复观测设施。
3. 解析实际开发端口并保存到 `.dev-state/ports.env`；启动 System 和所选模块 Backend，各模块 Backend `/health/ready` 成功后启动其 Worker。
4. 启动显式选择的 Runtime、Copilot 和 Agent，全部成功后启动 Gateway 与前端。无参数全量启动包含默认 Runtime；Engine Runtime 按各自协议检查 `/health`。
5. 输出本次服务清单、实际访问地址与 PID。

模块迁移与 Worker 启动顺序统一见[生命周期规范](../../docs/spec/addp开发服务生命周期与构建身份规范.md#模块迁移与-worker-启动顺序)：迁移由所属 Backend 独占，Worker 不执行平台 schema DDL、只读检查版本；公共执行与心跳表由 System Backend 初始化。结构或初始化迁移变化须递增 owner 的 `SchemaVersion`。

Develop Backend 内嵌 Query Execution Supervisor，领取全部 query execution。

**环境变量**:
```bash
# 跳过启动入口的 Go 依赖整理
SKIP_MODTIDY=1 bash scripts/dev/start.sh
```

**日志位置**:

已登记模块正文从“System → 模块管理 → 服务实例 → 查看日志”读取；有效可信未登记来源使用“未登记实例的运行日志”入口，均需 Platform 上下文及 `platform.module_log.read`。下列受控节点源用于本地诊断，不从路径推断 System 可信身份；引擎、前端和容器入口输出不等同于模块正文。完整契约见[日志规范](../../docs/spec/addp模块服务运行日志规范.md#四可信来源目录)。

- 模块服务运行日志: `logs/runtime/<module>/<instance_id>/*.jsonl`；引擎和前端启动输出保留各自日志
- 示例: `logs/runtime/system/<instance_id>/*.jsonl`、`logs/runtime/manager/<instance_id>/*.jsonl`；Worker 同样按独立实例分目录

**PID 文件**:
- 受管进程的 PID 记录: `.dev-pids/*.pid`
- System Backend 示例: `.dev-pids/system.pid`

### stop.sh

**功能**: 停止所有开发服务

停止时序、优雅退出窗口及 Runtime 容器先停后删规则统一见[生命周期规范](../../docs/spec/addp开发服务生命周期与构建身份规范.md#一生命周期互斥)。System 在其他实例注销和退出前保持可用，最后才卸载其 launchd 作业并停止 System Backend。

**安全性**:
- launchd 作业必须同时满足 `com.addp.codex.*` 标签和当前仓库绝对路径才会被卸载
- PID 记录与工作区残留监听者合并、按 PID 去重后，均须核验当前工作区归属；局部停止仅处理所选模块，不凭进程名或端口直接终止进程
- 端口清理只执行一次批量 `lsof` 查询，限定目标 TCP 端口和 `LISTEN` 状态；按 PID 去重后逐个核验身份，不把浏览器等客户端连接当作监听服务，也不终止非 ADDP 监听进程
- launchd 作业卸载失败时返回非零状态，`restart.sh` 会中断，不会带着残留进程继续启动

### restart.sh

**功能**: 智能重启服务。

- 无参数或 `-all` 时先停止，再准备 Agent/Copilot 依赖、同步和检查全部 Swagger，失败后不启动；局部重启先预检所选 Go 模块，失败时保留现有服务。未选 Go 模块时跳过局部预检；所选 Agent/Copilot 在停止后同步依赖并生成、检查 OpenAPI，失败时不启动。完整时序见[生命周期规范](../../docs/spec/addp开发服务生命周期与构建身份规范.md#一生命周期互斥)。
- 局部范围、依赖复用与补齐统一见[启停操作说明](../docs/启动脚本使用指南.md#选择操作)；停止失败时不进入准备和启动阶段，后续失败处理见[启动与重启边界](../docs/启动脚本使用指南.md#启动与重启边界)。
- Go Backend 和 Worker 分别核验进程状态；已运行的受管进程跳过构建检查和编译。编译后、启动前重新核验端口和工作区归属，不跨编译阶段复用监听快照。单独重启 Runtime 不会顺带编译仍在运行的 Meta Backend/Worker。
- Go 增量构建与缓存规则统一见[生命周期规范的原子构建](../../docs/spec/addp开发服务生命周期与构建身份规范.md#二原子构建)。重启保留已有二进制和 Go 包缓存，所有选定服务仍重新启动；复用产物保留 `build_id`、`built_at`，新进程生成新的 `started_at`。
- Swagger/OpenAPI 的生成与复用规则见[Swagger 集成指南](../../docs/spec/addp-Swagger集成指南.md#开发期增量生成)；复用产物仍须通过覆盖校验。生成失败、覆盖不一致或检查无法执行均阻断编译和启动。全部生成结束后才计算源码指纹并编译；同一模块的 `docs.go` 写入不得与两者并行。
- Go 后端就绪后，所选 Runtime、Copilot 和 Agent 的准备、启动与健康等待并行执行；全部任务成功后再启动 Gateway 和前端，并输出各任务及整体耗时。
- Python 依赖的安装互斥、输入与环境指纹、复用与失效条件、成功记录及失败处理统一见[生命周期规范的 Python 依赖同步](../../docs/spec/addp开发服务生命周期与构建身份规范.md#python-依赖同步)。Raster Mosaic 不安装或追踪 `common-python`。所有依赖同步与原生转换验证均在所选服务停止后进行；准备失败的服务不启动，入口返回非零。
- Go 后端和前端在各自启动阶段各查询一次 TCP LISTEN 快照，命中占用后实时复核；单项启动实时查询。端口冲突和扫描失败会中断启动，新进程退出不能被其他监听者的 HTTP 成功响应掩盖。
- 重启编排和缓存保留通过 `make test-dev-lifecycle` 验证，并纳入 `make test-platform` / Platform CI。

**命令与范围**：模块及 Runtime 的重启命令见[操作速查](../docs/启动脚本使用指南.md#选择操作)和[Runtime 启停](../docs/启动脚本使用指南.md#显式启动-runtime)；Gateway 选择器与停止范围的差异见[模块选择说明](../docs/模块选择启动指南.md#重启与停止的范围)。

GeoPython Workflow 使用原生 Python 3.12、GDAL 和 MDBTools/ODBC；准备与启动统一在 `geopython-workflow.sh`，虚拟环境不继承系统包。macOS 先安装 `brew install python@3.12 gdal mdbtools pkg-config`。局部与全量重启均先通过 stop.sh 停止所选服务，再准备依赖并验证原生能力。开发入口不构建或启动 GeoPython 镜像；产品部署保留容器方式，产品栅格 Hosted 验收通过根构建入口独立完成。环境隔离与端口归属要求见[GeoPython 运行说明](../../engines/geopython-workflow/docs/geopython-workflow容器管理指南.md#本地开发)。

Model3D Workflow 的 macOS Apple Silicon 开发启动与重启共用 `model3d-workflow.sh`，绑定 `.dev-state/model3d-native` 的固定版本私有工具包，不使用开发 Docker 转换 wrapper。首次准备需要 Conda、Rust 1.92.0、CMake 4.4.0 和 Xcode Command Line Tools，后续复用已验证缓存。产品构建与 Hosted 验收保留正式 Linux Runtime 镜像。

PointCloud Workflow 使用独立 Conda 前缀中的 Python/PDAL 原生进程；局部重启先通过 stop.sh 停止所选服务，再由 start.sh 同步依赖、验证驱动和真实 COPC 写入并启动。开发入口不构建镜像，拒绝非 Conda 前缀，不自动删除未知环境或接管外部监听者；产品部署保留容器方式。完整约束见[PointCloud README](../../engines/pointcloud-workflow/README.md#开发启动)。

Document Workflow 使用独立 Python 3.12 venv、私有官方 LibreOffice 和固定中文字体；局部重启先通过 stop.sh 停止所选服务，再由 start.sh 准备依赖、检查完整性和版本并启动，核对 PID、监听归属及 HTTP 健康状态。真实三页中文 PDF 转换由 `make test-document-native` 和 CI 独立验证。开发态只处理可信文件，检测到同名 `document-workflow-engine` 容器或外部监听者时拒绝启动；生产保留容器隔离。完整约束见[Document README](../../engines/document-workflow/README.md)。

SuperMap Workflow 首次使用或升级 iObjects C++ SDK、许可时，通过 `SUPERMAP_CPP_SDK_PATH` 指向完整 SDK 母版并运行 `bash scripts/build/build-supermap-workflow-base.sh` 构建稳定基础镜像。之后 `restart.sh -supermap-workflow` 和 `restart.sh -all` 根据构建指纹决定是否重新编译当前 C++ 源码并替换 8103 容器，不需要 rebuild 开关，也不挂载宿主机源码或 SDK 目录。

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

**退出行为**: 退出清理调用不带模块参数的 `scripts/dev/stop.sh`，停止当前工作区整套开发服务；即使只托管单模块，清理范围也不局限于该模块。`Ctrl+C` 和 TERM 会触发该清理。

**生命周期锁**: 整个托管周期持续持有 `.dev-state/lifecycle.lock`，退出清理后才释放；其他生命周期操作必须拒绝干预。锁的所有权与继承规则见[生命周期规范](../../docs/spec/addp开发服务生命周期与构建身份规范.md#一生命周期互斥)。

**适用边界**: 普通本地终端仍优先使用 `start.sh` / `restart.sh`；`keepalive.sh` 只用于命令会话结束后会回收后台进程的托管执行环境，不得用局部保活接管用户外部终端已运行的整套环境。AI 执行前告知原因、涉及模块及全量清理范围并取得本次明确许可，具体要求见[托管环境保活说明](../../docs/guide/troubleshooting/开发服务与构建.md#codex-等托管命令环境中-restartsh-退出后服务立刻不可用)。

### modtidy.sh

**功能**: 对 [modtidy.sh](modtidy.sh) 的 `GO_MODULES` 清单中存在且包含 `go.mod` 的目录并发执行 `go mod tidy`；执行范围以脚本清单为准。

同步 `main` 后依赖发生变化、添加或删除 Go 依赖、解决 `go.mod` 冲突时使用。该操作可能修改各模块的 `go.mod` 和 `go.sum`，执行后核对差异；操作步骤见[添加新依赖](#添加新依赖)。

## 启动依赖关系

模块依赖组合与 Runtime 选择见[模块选择指南](../docs/模块选择启动指南.md#如何合并依赖)；Backend/Worker 的通用就绪顺序见[启动与重启边界](../docs/启动脚本使用指南.md#启动与重启边界)。事实读取与启动编排还须遵循：

- Manager/Meta/Transfer 通过 **System** 获取 AuthContext、Engine 和模块目录等 System-owned 事实；各模块普通运行配置不从 System 加载。
- Gateway 在本次选择的后端启动阶段完成后启动，业务路由使用 System 的实例发现；前端按本次选择的清单启动。

具体就绪、迁移和停止约束见[生命周期规范](../../docs/spec/addp开发服务生命周期与构建身份规范.md)。

## IAM 首次初始化

开发和生产环境都不创建默认管理员、默认租户或共享弱密码账号。首次平台系统管理员、安全管理员和审计管理员使用同一套一次性离线 Bootstrap 流程建立；开发环境不保留绕过三员分立的简化路径。

## 故障排查

### 服务启动失败

按[启动失败排查](../docs/启动脚本使用指南.md#端口与启动状态)核对原始报错、Infra、实际端口与进程归属。System 的日志来源还须核对：

已登记实例与有效可信未登记来源均可从 System 的相应入口查询日志；未登记不等于启动失败，目录存在不代表正文已送达。尚未纳入目录或页面不可用时，可结合受控节点源 `logs/runtime/system/<instance_id>/*.jsonl` 及启动终端输出诊断，不从路径推断 System 可信身份。修正原因后，从项目根目录使用 `bash scripts/dev/restart.sh -system` 重启，不绕过标准入口手动运行后端。

### 健康检查超时

先按上节核对进程，再从本工作区状态读取实际端口检查 Backend readiness：

```bash
SYSTEM_BACKEND_PORT=$(sed -n 's/^SYSTEM_BACKEND_PORT=//p' .dev-state/ports.env)
curl -fsS "http://127.0.0.1:${SYSTEM_BACKEND_PORT:?未找到本工作区实际端口}/health/ready"
```

不要用首选端口上的其他服务响应代替本次进程就绪。失败时结合对应实例日志定位原因；基础设施状态与端口归属遵循[端口规范](../../docs/spec/addp端口分配.md)。

### 前端启动失败

先按[前端工具链规范](../../docs/spec/addp技术栈规约.md#前端技术栈)核对根 `.node-version`，再检查模块 `package.json` 与 `package-lock.json` 是否一致。安装与失败边界遵循[Node 依赖安装规范](../../docs/spec/addp开发服务生命周期与构建身份规范.md#node-依赖安装)：生命周期只执行 `npm ci`，缺少锁文件直接失败；依赖声明变更须走显式维护流程并审查锁文件差异。需要更新依赖时先停止受影响服务；通过标准入口重启，命令与范围见[启停操作速查](../docs/启动脚本使用指南.md#选择操作)。

### PID 文件残留

按[停止与进程归属](../docs/启动脚本使用指南.md#停止与进程归属)复核标准停止结果，保留失败时的错误与 PID 证据；launchd 作业、残留监听者及工作区归属的处理要求见本文 [stop.sh](#stopsh)。

## 开发工作流

### 日常开发

日常启停与代码变化后的重启按[操作速查](../docs/启动脚本使用指南.md#选择操作)执行，实例日志与进程核对见[排查失败](../docs/启动脚本使用指南.md#排查失败)。涉及依赖变化时使用下文的维护流程；启停用于开发联调，测试分层和实际运行验收见[测试与运行验收](../docs/启动脚本使用指南.md#测试与运行验收)。

### 添加新依赖

先停止受影响的服务，再更新和整理依赖，最后通过标准重启入口完成 Swagger 检查、按需构建和启动；任一步失败都不得继续。以下示例从项目根目录、由开发者在自己的终端执行；因 `modtidy.sh` 会整理脚本清单中的多个模块，示例采用全量停止和重启，依赖包路径须替换为实际目标。

```bash
(
  set -e

  # 1. 停止受依赖整理影响的开发服务
  bash scripts/dev/stop.sh

  # 2. 添加实际需要的依赖
  cd system/backend
  go get github.com/some/package
  cd ../..

  # 3. 整理依赖并核对 go.mod / go.sum 差异
  bash scripts/dev/modtidy.sh

  # 4. 经标准重启入口检查 Swagger、按需构建并启动
  bash scripts/dev/restart.sh
)
```

### 同步 main 后更新依赖

人工与 AI 开发均直接使用 `main`，不建立功能分支。同步代码后需要更新依赖时，按[添加新依赖](#添加新依赖)的顺序先停止受影响的服务，再更新和整理依赖，最后通过标准入口启动；停止或依赖更新失败时不继续。仅需重启、不更新依赖时，按[重启说明](#restartsh)执行 Swagger 预检与重启。

## 相关文档

- [启动脚本使用指南](../docs/启动脚本使用指南.md) - 日常启停、停止与进程归属、排障和验收入口
- [模块选择启动指南](../docs/模块选择启动指南.md) - 依赖组合、显式 Runtime 与 Gateway/Console 选择器例外
- [scripts/infra/README.md](../infra/README.md) - 基础设施脚本文档
- [CLAUDE.md](../../CLAUDE.md) - 项目整体架构
- [开发服务生命周期与构建身份规范](../../docs/spec/addp开发服务生命周期与构建身份规范.md) - 启动、停止、模块迁移与 Worker 顺序

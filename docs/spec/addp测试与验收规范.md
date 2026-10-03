# ADDP 测试与验收规范

本文定义 ADDP 测试分层、标准入口、环境边界、CI 编排和 Online / Release 验收的稳定规则。具体可用目标、suite 登记和运行参数以根目录 `Makefile`、`scripts/README.md`、`scripts/test/` 及模块 owner 文档为准。

## 一、基本原则

1. 每项测试必须归入 T0-T5，并明确 owner、依赖、数据边界和触发方式。
2. 平台只统一协议、入口、编排、安全检查和报告；业务夹具、断言、清理及残留核验归 owner 模块。
3. 根 `Makefile` 是测试与验收的唯一公共入口，复杂准备和安全检查放在 `scripts/test/`；不得保留模块私有旁路、兼容别名或在 workflow 中复制业务测试逻辑。
4. 测试结果只有通过、失败和按规范明确跳过三种语义。缺少依赖、凭据或未实际执行测试不得伪装成通过。
5. 开发服务生命周期与测试生命周期分离。`scripts/dev/restart.sh` 不隐式运行测试，日常测试入口也不接管开发者已经启动的服务。
6. 专用 T4 Runner 可以在宿主机准入通过后编排隔离测试部署，但不得复用或接管个人开发环境。

## 二、测试分层

| 层级 | 名称 | 典型内容 | 运行依赖 | 默认触发 |
| --- | --- | --- | --- | --- |
| T0 | 静态与契约检查 | 格式、依赖与登记一致性、授权清单、Swagger 路由覆盖、测试夹具约束 | 无运行中服务 | 每次改动、PR |
| T1 | 单元与组件测试 | Go package、Python 单元、Repository mock、Vue 组件与状态逻辑 | 无，或进程内临时资源 | 每次改动、PR |
| T2 | 模块集成测试 | Migration、Repository、事务、锁、真实 PostgreSQL / Redis / 对象存储 | disposable 基础设施 | 相关模块改动、PR |
| T3 | 前端 Smoke / E2E | 路由、权限提示、状态恢复、关键表单和浏览器交互 | 独立测试端口或受控夹具 | 相关前端改动、PR |
| T4 | 跨模块 Online 验收 | 真实 System 认证、Gateway 路由、多个 owner、Worker、补偿和最终一致性 | 隔离的完整 ADDP 测试部署 | 手工；首跑稳定后可夜间执行 |
| T5 | 发布认证 | 安装包生命周期、真实 OS 凭据后端、HA、故障切换、在线厂商证据 | 产品或 Runtime 专用认证环境 | 发布前、Tag 或手工执行 |

T0-T3 证明确定性代码与模块集成；T4 证明真实运行拓扑；T5 证明产品或 Runtime 的发布条件。测试文件所在目录、与某个发布 workflow 共用编排，都不能改变其层级。

## 三、标准入口

平台公共入口为：

```bash
# T0-T1：全仓无外部服务确定性门禁
make test

# 当前工作区受影响模块的 T0-T3 门禁
make test-changed
make test-changed BASE_REF=<ref>

# 指定 owner 模块的 T0-T3 门禁
make test-module MODULE=<module>

# 全部已登记 disposable 基础设施门禁
make test-integration

# 仅在持有合法 License 的 owner 受控 Linux 主机人工运行，不进入 GitHub Actions
make test-integration-owner-managed

# 本地与 CI 共用的发布工作流安全审计（首次执行需下载固定版本工具）
make test-workflow-security

# 一个已登记的跨模块 Online suite
make test-online ONLINE_SUITE=<suite>

# 一个已登记的产品发布 suite
make test-release RELEASE_SUITE=<suite>
```

约束如下：

- `make test-changed` 与 CI 共用 owner 影响计算；共享代码改动按真实依赖扩散到已登记消费者。
- `make test-module` 自动发现模块的 Go、Python、前端与基础设施门禁，不维护第二份模块清单。
- `make test-integration` 严格串行调用 owner 的 T2 事实入口，不复制测试逻辑。
- `test-online` 与 `test-release` 必须显式选择已实现的 suite；未实现能力不得以占位 suite 登记。
- 不提供跨 T0-T5 的 `test-all`。T4 与 T5 的身份、基础设施和安全前置条件不同，完整认证由 CI 分别编排标准入口并分别报告。
- 开发者和 AI 不得自行拼接一组语言命令替代上述标准入口；模块内部命令仅用于 owner 开发与标准入口实现。

## 四、T0-T3 确定性与基础设施边界

### 4.1 T0-T1

T0-T1 必须：

- 不依赖已经启动的 ADDP 服务。
- 不连接开发业务数据库。
- 可重复执行，失败后不留下外部状态。
- 需要 Online 条件的测试默认不进入普通语言测试；对应专用门禁显式开启并拒绝意外 Skip。
- 模块生命周期公共契约必须在 T1 覆盖 `starting → registered`、`registered → recovering → registered`、确定性拒绝进入 `failed`、取消后 `stopped` 与限时注销。
- Backend 路由测试必须证明 `/health/live` 不触发外部调用，`/health/ready` 只在自身必需 Infra 和 System 注册都就绪时返回 200，业务路由在 Not Ready 时返回 `503 module_not_ready`。
- Worker/Scheduler 测试必须证明未注册或 `recovering` 时不领取新工作，恢复后无需重启进程即继续领取。

### 4.2 T2

T2 使用真实但可丢弃的基础设施，并满足：

- CI Job 使用独占 Service 和随 Job 销毁的数据库。
- 托管外部服务的 owner gate 必须在脚本头声明 `# ADDP_T2_SERVICES=<service,...>`；模块门禁、CI 注册和后续新增数据库类型都消费该声明，不按数据库名称维护发现分支。
- owner 自建的临时服务通过 `ADDP_T2_OWNED_SERVICES` 和 `ADDP_T2_COMPOSE_FILE` 登记，必须拥有独立 Compose 项目、回环随机端口以及退出清理和零残留检查。镜像使用固定 tag 与 digest；需要沿用仓库源码构建时，允许引用仓库内的单一 Dockerfile，但外部基础镜像须固定 tag 与 digest，Git 源码须在同一构建步骤中核对固定提交，构建上下文须位于仓库内，Dockerfile 与本地复制输入须登记到 `ADDP_T2_INPUT_FILES`；目录声明以 `/` 结尾，递归覆盖该仓库子树的新增、修改和删除，构建检查与变更选择共用同一覆盖判定。登记检查拒绝仓库外路径、构建参数覆盖和未固定源码；门禁必须实际构建，不接受开发环境已有同名镜像作为构建证据。
- 必须由调用方注入连接条件的 owner gate 同时声明 `# ADDP_T2_REQUIRED_ENV=<name[|alternative],...>`，逗号表示“同时需要”，竖线表示等价的安全前置条件。`make test-module` 与 `make test-changed` 必须在执行任何 T0/T1 前一次性检查全部所需条件，缺失时失败关闭并给出 owner、变量及安全测试环境提示；`--dry-run` 仅展示计划，不要求真实连接条件。CI 登记检查必须确认对应 Job 显式提供每组条件中的至少一个变量。
- 需要 owner 持有合法 License 或受控介质的门禁必须声明 `# ADDP_T2_OWNER_MANAGED=<runtime>`，只通过 owner 受控 Linux 主机上的 `make test-integration-owner-managed` 人工执行，不进入 GitHub Actions、普通 `make test-integration` 或 macOS 定时巡检。登记检查必须拒绝 workflow 调用这类目标及其聚合入口。脚本必须验证官方介质与 License SHA-256、拥有 disposable 容器全生命周期并验证零残留。
- 本地共享 `addp-postgres` 只允许使用 `addp_test` 与 `addp_iam_test`，并且只能通过根 `Makefile` 或 `scripts/test/` 的标准入口操作。
- 本地 PostgreSQL T2 门禁必须由调用方显式提供测试 DSN 或端口；不得在未提供时回退到 `.env` 的首选端口。调用方先用 `bash scripts/infra/status.sh` 核实 ADDP Compose 实际映射。标准 PostgreSQL 门禁在执行测试或清理前，必须核对本地回环连接的端口与当前工作区 `addp-postgres` 的实际 Compose 映射一致，且 database 仅为 `addp_test` 或 `addp_iam_test`；容器不存在、归属不符或映射不一致时直接失败，不根据 `.env` 猜测。门禁前置检查不得输出一个写死首选端口的可复制连接命令。GitHub Actions 的独占 Service 与辅助 macOS CI 的独占临时 PostgreSQL 使用各自入口显式配置的端口和 database，不套用本地共享 Infra 校验。
- 禁止为单次验证直接创建或删除数据库；现有标准入口不能满足隔离时，先完善入口及自动清理。
- 门禁在任何破坏性动作前校验数据库身份，拒绝开发库、生产库或不满足 owner 安全约束的连接。
- 每个场景只清理自己拥有的 Schema 或带唯一运行标识的事实，并验证零残留。
- 门禁拒绝意外 Skip，避免“命令成功但测试未运行”。
- Common PostgreSQL 的默认 `make test-common-postgres` 保持完整门禁。仅验证查询读取集合及输出血缘时，可在同样的显式测试连接条件下运行 `bash scripts/test/common-postgres-gate.sh --test query-read-set`；该分组包含真实资源改名及同名重建的定位身份案例，保留数据库身份检查、Skip 拒绝和场景清理。分组结果只证明所选范围，不替代完整 T2 门禁；CI 继续使用无分组选项的默认入口。
- Manager 派生任务定义、语义唯一约束、资源绑定与资源回收的持久化契约由 `make test-manager-postgres` 在真实 PostgreSQL 中覆盖；该门禁只允许本地 `addp_test` 或 CI 随 Job 销毁的 disposable database。
- Orchestrator 的独立进程故障回归归入既有 `make test-orchestrator-postgres`：测试进程运行正式监督器，在提交与等待阶段分别强制终止持有租约的运行者，再由新进程验证过期失败收敛、不重发请求、旧租约写入拒绝、子执行引用保留及唯一终态事件。HTTP Provider 是受控夹具，因此不作为 T4 真实拓扑验收。Go 测试自动发现和现有 Hosted PostgreSQL Job 覆盖同一入口，无新增基础设施依赖；子进程和当次数据库事实必须在成功、失败路径清理并核对残留。

### 4.3 T3

T3 的 PR 主路径使用独立端口、受控 API 夹具和非个人登录态。真实 System、Gateway、owner Backend、真实身份与数据源的浏览器链路归入 T4，不与确定性浏览器测试混跑。

租户执行过程事件的真实 PostgreSQL 事务、并发限额、租约过期、Owner 范围分页和清理锁竞争由 `make test-common-postgres` 的 `TestExecutionEventsAgainstPostgres` 覆盖；Transfer 历史自由文本移除由 `make test-transfer-postgres` 的 `TestIntegrationPostgresExecutionEventsRemoveRetiredText` 覆盖。两者均已登记标准入口，Common 的全量门禁包含该执行事件组。共享运行过程组件的唯一所有权由 `make test-common-frontend` 自动发现，Monitor 的事件分页、截断及撤权清除回归由 `make test-monitor-frontend` 的确定性 Playwright 覆盖。上述测试不替代真实 User、Gateway、Owner 与 Worker 的 Online T4，也不代表个人环境已部署新的迁移和保留维护循环。

确定性 Playwright 的每个 Vite `webServer` 必须显式设置 `ADDP_E2E=1`、回环 host、独立端口及 `--strictPort`，并设置 `reuseExistingServer: false` 与带正数超时的 `gracefulShutdown: { signal: 'SIGTERM', timeout: 5000 }`，使缓存清理钩子能够执行。被启动的前端统一使用 `common-frontend/basic/src/utils/viteTestIsolation.mjs` 的 `withFrontendTestIsolation` 包装 Vite 配置：测试关闭 HMR，依赖缓存按模块和进程隔离到操作系统临时目录，Vite 关闭、进程正常退出及收到 `SIGTERM` / `SIGINT` 时清理；开发配置不启用该隔离。模块不得另行维护测试缓存或 HMR 分支。现有前端 CI 登记检查自动发现确定性 Playwright 配置及跨模块夹具服务，在 `make test-platform` 中拒绝遗漏隔离、复用开发服务、端口漂移或地址不一致。Online T4 的专用部署配置不套用此 Vite 夹具规则。

浏览器测试重点证明布局、路由、权限反馈、状态恢复、关键交互和响应式行为，不重复后端已经覆盖的全部字段或业务规则。

前端矩阵 Job 按 `playwright: true` 声明统一上传失败输出目录，产物按模块、工作流运行与 attempt 区分。运行浏览器门禁的步骤通过 `TMPDIR` 对齐测试临时目录与上传步骤使用的 `runner.temp`。System 浏览器门禁保留失败时的 trace、截图与页面上下文；角色分配用例分阶段确认角色选择、下拉菜单关闭、授权原因和提交条件，避免只在最后的按钮点击处报告超时。

## 五、T4 Online 验收协议

### 5.1 专用环境

T4 只在隔离的 ADDP 测试部署执行，并按运行条件选择唯一部署 profile：

- 常规 Online suite 使用带 `self-hosted`、`macOS`、`addp-online` 标签的专用 Runner 和 `addp-online` GitHub Environment，复用专用部署中的稳定 Tenant、User 和 Engine Instance。
- 需要商业 License 的 Linux 引擎使用带 `self-hosted`、`Linux`、产品声明的 `X64` 或 `ARM64` 架构标签和 owner 标签的专用 Runner，通过受保护 GitHub Environment 审批。当前 KingbaseES 正式门禁仍固定 `X64`；DM8 ARM64 若进入正式门禁必须固定 `ARM64` 且匹配厂商认证环境。License 和受控介质不进入 checkout、日志或 Artifact，也不回退到 Hosted 或 macOS profile。
- DM8 在正式 ARM64 主机到位前允许本机 macOS ARM64 Docker Desktop 执行试用期技术 T2/T4：数据库以及所有真正加载官方 Go 驱动的测试/模块进程必须运行于 `linux/arm64` 容器，System 可保持 macOS 开发模式仅承担 Engine Instance 控制面登记。该 profile 是非认证 ABI、非长期且不可调度的本地证据，不登记 GitHub Hosted 或 schedule；试用到期、Docker 架构不符、驱动摘要不符或官方介质不可得时必须失败，不能 Skip 或切换镜像/驱动。
- 明确声明 Linux/CPU 限制，或能够从零建立完整确定性夹具且无需永久测试身份的 suite，可登记 GitHub Hosted profile。该 profile 每轮必须在干净 `ubuntu-24.04` x86_64 Runner 上从零启动 disposable Infra、Tenant、User、Engine Instance 和业务引擎，退出时全部销毁；不使用 GitHub Environment 或仓库 Secret。
- self-hosted Runner 使用独立账号和独立 checkout；Hosted Runner 使用 Actions 当次临时 checkout。两者都不复用个人开发工作区或开发服务进程。
- 公开仓库的 self-hosted Runner 必须是可独立重置的专用测试设备，不得登录个人 Apple ID、保存个人 SSH Key、浏览器会话或访问个人与生产网络。只允许受保护 GitHub Environment 的 `workflow_dispatch` 和已毕业 suite 的固定 `schedule` 调度；`pull_request`、`pull_request_target`、`push`、Issue 事件或可由外部输入改写的动态 workflow 不得选择该 Runner。
- self-hosted Online Job 的 `GITHUB_TOKEN` 只授予 `contents: read`，第三方 Action 必须固定不可变 commit SHA，`addp-online` Environment 必须在 Job 接单前执行必要的人工批准。Runner 不接收仓库 Secret 中的长期业务凭据；永久测试部署的 Secret 只存在专用设备上的 owner-only 仓库外环境文件和 owner 加密存储中。
- 服务只绑定 Runner 可访问的回环地址；通用预检拒绝外部服务地址。
- 仓库根不得保存 T4 `.env`。self-hosted profile 的 Tenant、数据库连接和凭据由仓库外绝对路径环境文件注入；Hosted profile 只能使用当次 disposable 部署产生、位于 `runner.temp` 的 owner-only 凭据文件。
- `ADDP_ONLINE_HOST` 必须精确为 `1`；Hosted profile 还必须同时校验 `GITHUB_ACTIONS=true`、`RUNNER_OS=Linux` 和 `ADDP_ONLINE_HOSTED=1`。生命周期门禁必须在任何停止、启动或重启操作前完成只读准入检查。
- License 受控 owner-managed profile 必须同时校验 `GITHUB_ACTIONS=true`、`RUNNER_OS=Linux`、`ADDP_ONLINE_OWNER_MANAGED=1`、外部 owner-only 环境文件权限、官方介质 SHA-256 与 License SHA-256；Hosted 与 owner-managed 标记必须互斥。
- `POSTGRES_DB` 必须精确为 `addp_online`，并拒绝 `addp`、`addp_test`、`addp_iam_test`。该数据库只属于当前 T4 部署，不属于本地共享 PostgreSQL 测试清单。
- 证据目录必须位于仓库外；凭据目录不得被 artifact 归档。工作区必须干净，构建身份必须与当前 checkout 一致。

三种 profile 共用同一个 `make test-online` 分发器和 owner 业务断言，生命周期脚本只负责当次环境和夹具。编排只调用现有 Infra、开发生命周期脚本和 `make test-online`，不得在 workflow 中复制模块启动逻辑或业务断言。退出路径必须停止本次应用进程并报告清理结果；常规 macOS self-hosted Infra 可在专用 Runner 常驻，Hosted 与 License 受控 owner-managed profile 的 Infra 必须随 Job 销毁。

Manager 内部产物 T4 使用 `manager-internal-artifact-lineage` 单一 suite 和 GitHub Hosted Linux x86_64 disposable profile，不再由 macOS self-hosted 分发。`online-hosted-manager-gate.sh` 复用共享 Hosted 生命周期，从零建立 Infra、最小权限 User、Tenant 和 Business MinIO Engine；浏览器登录凭据只保存到当次仓库外 owner-only Secret 目录。通过正式构建入口构建 Linux amd64 Model3D 转换器，由标准开发生命周期启动 System、Gateway、Meta、Manager、Monitor、Console、PointCloud、Document 和 Model3D Runtime；不启动无关模块或模拟转换器。退出时停止应用并销毁 Business MinIO 容器、网络、数据卷及当次 Infra，检查零残留。Business MinIO 的确定性生成 LAS、仓库已跟踪的 3 页 PPTX、带 PNG 贴图的 DAE 与 3DS 必须经过同一次真实 Meta 深度扫描，再通过对应 Manager owner action/task 与真实 Runtime 生成 COPC、PDF、GLB。模型断言覆盖 `model_3d + single` 分类、贴图资源引用、自包含 GLB 的内嵌 PNG 与非空网格、Manager/Monitor 血缘一致，以及 Console iframe 中 GLB 请求成功、模型加载完成和截图。Online Chromium 统一显式使用 SwiftShader 软件 WebGL，在无独立 GPU 的 Hosted Runner 上仍执行真实模型绘制；不屏蔽业务警告或用截图占位。浏览器报告 v2 单列 Chromium 截图产生的精确 `ReadPixels` GPU 性能诊断计数；页面警告、错误及失败的业务请求仍使验收失败。临时产物及任务按本轮捕获 ID 删除，内容和任务分别确认 404，模型源的用户预览模式恢复原值；执行尚未终结或无法确认资源归属时不得报告零残留。确定性脚本测试沿用 `make test-online-runner`，真实运行沿用 `make test-online ONLINE_SUITE=manager-internal-artifact-lineage`；本次模型扩展首次在 Hosted 环境通过前不计为已完成 T4，也不增加定时调度。

生产 Compose 单入口 T4 使用 Hosted disposable profile：在全新 Runner 上用正式构建入口生成 System Backend、Gateway、Meta Backend、Manager Backend、Transfer Backend、Orchestrator Backend、Console、System Frontend、Meta Frontend、Manager Frontend、Transfer Frontend、Orchestrator Frontend 和 Nginx 镜像，以 `addp-platform` project 启动这些真实容器及当次 Infra。Meta、Manager、Transfer 与 Orchestrator Backend 向 System 自注册，Gateway 从有效 Backend 实例发现对应的 `/api/v1/:module/*`；四个模块的 Frontend 分别由 Nginx `/meta/`、`/manager/`、`/transfer/` 和 `/orchestrator/` 转发。内置 Runtime 属于独立 `addp-runtimes` project，不参与公开入口的业务断言；同一 Runner 额外启动一个真实 GeoPython Runtime 和一个真实 Business MinIO，验证 `addp-runtimes`、`business` 的独立生命周期、Compose project 标签、网络归属和宿主机端口边界。

对外 Nginx 使用非默认回环端口；仅 T4 Compose 覆盖文件允许 System Backend 和 Gateway 额外发布回环诊断端口供通用构建身份预检，正式 Compose 保持只有 Nginx 对外发布。未参与验收的 Nginx 静态 upstream 只提供当次网络名称占位，不模拟参与断言的服务。业务断言必须从公开入口检查 Console、System、Meta、Manager、Transfer、Orchestrator Frontend 及其构建资源，并使用当次一次性 Tenant 的零权限、只读、仅创建 User 验证 System AuthContext 的精确 Permission 集合；System、Meta、Manager、Transfer、Orchestrator 的 API 分别验证未认证 401、无权 403，以及已授权读取。Meta、Transfer、Orchestrator 的写入入口还需用无效请求体证明仅创建权限可越过功能守卫但被参数校验拒绝，且只读账号不能写入；System Role Assignment 详情需证明本租户授权可读、另一租户的授权 ID 返回 404。无效请求体不得创建业务资源。测试还需核对 System 对外 origin、Meta、Manager、Transfer 和 Orchestrator Backend 的就绪与内部地址以及容器实际端口；全部退出路径分别销毁 Runtime、Business MinIO、平台应用、占位容器、镜像仓库和 Infra，核对四个 project 的容器、网络、卷零残留。Transfer Worker 的数据执行和 Orchestrator 的 DAG 执行分别由 owner T4 验收，不以本套入口检查代替。[Hosted T4 run 36141592135](https://github.com/pampa0629/addp/actions/runs/36141592135) 已通过此前的 Transfer 覆盖验收；扩展后的权限矩阵及 Orchestrator 覆盖须在更新提交上手工运行成功后才能计为通过。

质量动态绑定 T4 `quality-dynamic-binding` 使用专用 Tenant 的两个已审批 Model 物化任务作为永久夹具；两个 PostgreSQL 目标表必须预先存在、结构相同、行数稳定且非零。每轮经正式 API 创建带 Run ID 的规则、方案和编排，验证 Model 默认版本输入、上游 `target_locator` 动态绑定、失败阻断、重复失败工单去重、两个目标的结果与工单隔离，以及显式升级规则修订后逐目标恢复。方案默认绑定不得被执行覆盖，Model 定义和物理数据不得修改。清理只删除本轮编排、方案及规则并确认 404；执行审计保留，方案关联工单由 Quality 删除方案事务清理。执行未终结或创建响应丢失时不得报告零残留。入口和确定性脚本测试分别为 `make test-online ONLINE_SUITE=quality-dynamic-binding`、`make test-online-runner`；只登记手工 T4，首次专用环境真实通过前不加入夜间调度。

Orchestrator 执行 T4 `orchestrator-execution` 复用 Hosted 生命周期、System owner 身份夹具和既有一次性 PostgreSQL 样例源，正式启动 System、Gateway、Meta、Orchestrator、Monitor。每轮经 API 创建扫描任务和嵌套串行 DAG，核对父子执行、成功进度、真实数据源中断后的失败阻断、安全步骤/事件以及跨 Tenant 与缺少 Owner 权限时的不可见性。故障操作必须核对当次源容器标签，恢复后删除本轮任务；原始执行历史由 Hosted Infra 销毁，凭据不归档。入口为 `make test-online ONLINE_SUITE=orchestrator-execution`，准备和退出由 `scripts/test/online-hosted-orchestrator-gate.sh` 负责，确定性测试进入 `make test-online-runner` 与 T0。首次真实通过前只允许手动 `workflow_dispatch`。该轮不声称已覆盖后端进程崩溃、POST 响应丢失或续租故障；这些边界的 T1/T2 回归不能替代后续 T4。

### 5.2 开关、身份与拓扑预检

本体修订投影 T4 `ontology-revision-lifecycle` 使用 GitHub Hosted `ubuntu-24.04` 临时部署，从零启动 System、Gateway、Ontology 与 Infra FalkorDB。System owner helper 创建非默认 Tenant 和仅含 `ontology.revision.read/update/publish`、`system.execution_authorization.create` 的 User，不创建业务 Engine 或 Engine Provisioner。北京 Outdoor 只作为合成本体定义，通过 Gateway 验证草稿、审核、发布准入、最新投影发现、异步激活、旧 version 拒绝、第二修订替换与撤回不回退。202 不作为激活成功证据，必须读取确定 generation 的 ready 状态和本体 active 指针。该 suite 没有业务实例读取或推理 API 断言，不宣称 Agent 能力验收。

Ontology 修订、审计与历史图在验收期间保留，不增加测试删除接口或 SQL 清理旁路；所有退出路径由 Hosted 生命周期销毁当次 Infra（含 PG/FalkorDB 数据卷）和凭据，并核验容器、网络、卷零残留。业务报告将历史标为待部署销毁，只有生命周期清理成功才可报告整体通过。首次真实通过前仅登记手工 `workflow_dispatch`。

每次 T4 运行至少满足：

- `ADDP_ONLINE_TEST=1`。
- 显式非默认 `ADDP_ONLINE_TEST_TENANT_ID`，禁止 Tenant 1。
- 全局唯一的 `ADDP_ONLINE_TEST_RUN_ID`；未提供时由分发器生成并贯穿全部子测试。
- 每个参与 HTTP 服务 `/health/live` 可用，且 Build ID、Git commit 或源码指纹与当前 checkout 匹配；进入业务断言前 `/health/ready` 必须返回 200。
- 测试 User、Service Principal 和 OAuth Scope 只具备场景所需的最小权限；不得使用个人会话、平台管理员 Token 或扩大生产身份权限。
- 通过 System AuthContext API 证明 principal、context、Tenant、client、token 与 Permission 事实后，才进入资源创建或注册拓扑操作。
- 同一 `suite + Run ID` 通过进程锁串行化，锁覆盖预检、业务断言和报告落盘全过程。

每个断言只验证规范定义的唯一身份链路，不在 User Token、Service Token 和直接 SQL 之间兼容回退。Repository / Migration 的直接数据库夹具属于 T2，不能替代 T4 API 授权验收。

API 消费方数据面边界由 `workbench-service-consumption` 复用同一 Business MySQL Fixture 和临时 Query Service 验收，不另建平行 suite。该场景必须分离两个真实 User Token：非管理员 Token 只负责 Service / Workbench 发布与消费，专用 Tenant Administrator Token 只负责 API 消费方与凭据生命周期。suite 通过正式 API 创建一个只授权主 Query Service 的 API 消费方，并必须同时证明：已授权服务返回 200；同 Tenant 未授权 Query Service 返回 `403 api_consumer_service_denied`；任一 `/api/v1/*` 控制面路由返回 `401 api_consumer_control_plane_denied`；删除 API 消费方后，旧凭据在 Gateway 30 秒正缓存对应的 45 秒有界收敛窗口内返回 `401 api_consumer_credential_invalid`。报告、日志和错误不得包含完整 API Key，每轮必须按捕获 ID 删除 API 消费方、其级联凭据、Data Application 和两个临时 Query Service，并验证零残留。

模块注册与恢复类 T4 必须使用正式进程入口至少证明：

1. 业务模块先启动、System 后启动时，模块先为 Alive/Not Ready，不能处理业务；System 恢复后使用同一 `instance_id` 自动进入 Ready。
2. System 与业务模块先启动、Gateway 后启动时，Gateway 在首个完整快照后进入 Ready 并建立路由。
3. System 运行中断后，Backend 在心跳失败被观测后转为 Not Ready，Worker/Scheduler 停止领取新工作，Gateway 在已有租约过期后返回 503。
4. System 恢复后所有实例无需重启即重新 Ready，Gateway 自动恢复路由，不需要前端人工刷新或二次点击。

仅启动 Probe Server 并直接调用 System 注册 API 可以验证租约与路由算法，但不能替代上述进程级就绪与恢复证据。

专用 Runner 可以通过受 `ADDP_ONLINE_HOST=1` 强制保护的精确进程参数复用正式开发构建与进程入口，以控制业务 Backend、System 和 Gateway 的真实启动顺序；该入口不得成为日常开发的第二套模块依赖路径。精确停止必须先验证受管 PID 的进程身份。每个进程阶段都必须把 Liveness、Readiness、业务门禁、实例身份和 Gateway 路由投影写入仓库外证据目录。

消费方与 Engine Instance 动态恢复类 T4 必须另外证明：全量重启后真实 User 在同一 Browser Context 首次打开 Console 原生页与模块 iframe 不需要 reload、手工刷新或二次点击；Engine Instance 经正式连接检测 API 从 `online` 转为 `offline` 再恢复 `online` 时，已打开页面自动更新，相关 Backend 与 Frontend PID 保持不变。Engine Instance 是永久身份，此类 suite 使用专用 Runner 预置的长期 Engine Fixture，只控制隔离物理端点的连通性，不得按 Run ID 创建、删除 Engine Instance 或清理墓碑。物理 Fixture 必须由 owner 的 Online 专用生命周期入口管理，要求 `ADDP_ONLINE_HOST=1`、不读取仓库内 `.env`、不复用平台 `POSTGRES_DB`，并在成功、失败和中断路径恢复连接观测后停止。

企业资源目录发布类 T4 使用同一永久 PostgreSQL Engine Instance 及其 owner 生命周期入口创建的稳定表 `public.addp_online_catalog_fixture`。专用环境必须预置可引用的 Standard Domain 和 Department；首次运行可将该永久数据源对应的 `discovered` CatalogEntry 初始化为稳定 `curated` fixture，后续运行必须在验收后恢复其编目聚合。同一 suite 必须重复执行真实 Meta 扫描并证明 fingerprint 与 CatalogEntry UUID 幂等，验证 `inventory` / `governance` 视图、治理覆盖率固定维度和精确来源身份解析；真实浏览器使用同一专用 User 登录 Console，验证覆盖率页、目录详情与 Domain / Department / Engine 名称选择器，并将浏览器 warning/error 计入失败。每轮创建的 Asset 和 AssetCategory 必须经正式 API 下架、删除并证明零残留；不得直接 SQL 清理。

该 suite 同时在上述专用 Catalog fixture 上验证 `curated → deprecated → curated`，以及已弃用阶段的责任子资源更新。使用现有专用 User 增加或移除可选技术负责人，使责任集合发生实际变化；不为验收新增账号、部门或赋权。断言移交仍保持弃用、业务定义、语义、可见范围设置和来源身份，撤销弃用不恢复认证，旧版本请求返回 `409 catalog_entry_version_conflict` 且无副作用。成功和失败路径都经正式 API 恢复完整编目聚合并重新读取核对；写响应丢失时先读当前状态，不重试原命令，恢复失败使整个门禁失败。审计和并发版本不回滚，跨部门移交后的可发现范围及权限交集继续由 Catalog T2 验证，不以单账号 T4 冒充。

该 suite 还验证普通只读共享的正式准备、真实双服务 OAuth 反查与首次受理。专用 User 必须是 fixture 的当前业务负责人，显式具备共享确认、独立办理和批准要求初始化／读取 Permission，并由管理员预置当前有效的引擎管理委派；测试不自动赋权或创建委派。专用部署须配置独立 `SYSTEM_SERVICE_CLIENT_SECRET`，未配置在生命周期启动前拒绝验收，不改变普通部署允许留空的 Ready 边界。只为上述精确稳定表通过正式 API 初始化尚不存在的 Catalog 批准要求，已有独立批准模式拒绝而不覆盖；不可变业务决定、已核清的办理记录与 System 回执属于专用 Tenant 的长期审计事实，单列证据，不按临时资源删除。正式请求显式限时十分钟，办理窗口仍从首次受理起最多五分钟。同编号换用途或批准要求版本须返回 `409 catalog_sharing_decision_conflict`，拒绝后原决定／原回执必须不变；不新增决定或办理编号。此拒绝允许产生操作审计，不能以场景预期数量代替数据库计数验证。同参重放必须保持完整绑定、受理时间及截止时间不变；调用方丢弃已提交响应后恢复只证明调用方边界，不冒充 Catalog→System 网络中断或进程重启。人类 Token 访问机器反查／受理接口必须拒绝；`pending`、绑定错误或恢复失败不能计为通过。受理不等于 Grant 写入或实际可读，本 suite 不验证或创建源访问 Grant。

Manager 平台内部产物类 T4 使用专用 Business MinIO Fixture 和永久 MinIO Engine Instance。Fixture owner 幂等写入仓库内确定性小型 LAS 与多页 PPTX 样本；suite 经 Gateway 触发真实 Meta scan，并使用扫描所得的 ResourceLocator、item ID 和 fingerprint 验证两条正式链路：`point_cloud_copc_generation` 必须由 PointCloud Runtime 从业务对象存储读取源文件、向 Manager infra MinIO 发布 COPC，并由 Manager execution 写入 `addp.lineage-facts/v1`；PPTX 预览必须由 Quick View Capability 声明 `generate_pptx_pdf`，并通过统一 action 入口首次触发 `pptx_pdf_generation`，由 Document Workflow / LibreOffice 发布多页静态 PDF，同源再次读取 Capability 只复用同一任务与结果，不创建第二次 execution。Monitor API 与真实浏览器必须展示同一业务输入和 `addp-infra://` 输出；浏览器还必须在 Data Explorer 翻到后续 PDF 页，跨过 Engine 状态周期刷新后仍保持当前页。退出路径通过 Manager 正式 API 删除两类结果与任务、验证对象不可再读取及临时资源零残留；不得直接操作数据库或 infra MinIO 清理。

Manager 混合检索类 T4 使用同一专用 Business MinIO Fixture 和永久 MinIO Engine Instance，Fixture owner 额外幂等写入仓库内确定性 JPG。专用 Tenant 必须通过正式 Inference 与 Manager 配置 API 预置支持文本和图片、维度为 2560 的 Model Profile 及 `semantic_search_embedding` 场景绑定；Provider Credential 只保存在 Inference 加密存储，Online 环境只声明预期 Model Profile ID。suite 不创建、更新或删除 Provider、Deployment、Model Profile、Credential 和场景绑定，而经 Gateway 触发真实 Meta scan 与 Manager ad-hoc embedding，验证 Inference、pgvector、Meilisearch 和 Manager 对称 RRF 融合的完整链路：同一 `document_id` 的关键词与向量候选只能返回一次，纯向量候选必须在融合后参与统一分页，响应不得包含旧 `vector_hits`。每轮 embedding 结果必须通过 Manager 正式 API 删除并确认零残留；新增 suite 只登记手工 `workflow_dispatch`，首次真实通过前不得加入夜间调度。

指标修订生命周期 T4 `metric-service-revision-lifecycle` 唯一使用 GitHub Hosted `ubuntu-24.04` 临时部署，手工调度时明确选择 PostgreSQL 或 TiDB 业务引擎，每次只运行一个引擎变体；每日夜间调度在独立 Job 中运行两个变体。每轮自动创建独立 Infra、非默认 Tenant、最小权限 User、所选业务数据源，并通过正式 Meta、Standard 和 Model API 建立扫描结构、已发布定义、已审批逻辑表及已发布指标夹具。两个引擎变体复用同一组 `count_distinct` 与 `sum_decimal_by_group` 指标契约、确定性数据和业务断言；各自使用符合其引擎目录模型的命名空间和独立物理夹具。按城市汇总面积的夹具使用 `DECIMAL(38,18)`，断言同城高精度求和、空字符串分组和 NULL 分组排除后的精确字符串结果。两个指标分别使用带 Run ID 的独立实现，验证首次查询的非空确定结果、后续发布不会自动切换服务、撤回绑定修订后服务仍 active 但查询被拒绝，以及显式重绑后恢复。实现和修订历史在验收期间保持完整，禁止 SQL 删除或绕过 Model 生命周期；临时 Query Service 通过正式 API 删除并验证 404。退出时销毁整套临时数据库、业务容器和凭据，仅将无密钥的引擎类型、ID、状态、校验摘要、日志与清理结果归档为 Actions Artifact。不得复用个人环境、永久测试租户或自托管 profile；两个变体须各自首次真实通过并确认清理零残留后才可进入夜间调度。

限时原值访问 T4 必须使用两名不同的专用 User，覆盖 `manager/preview` 的完整流程：申请用户在 Manager 出口发起申请，另一名审批用户在 Security 审批，批准后仅申请用户在有效期内看到原值，审批用户和其他用户仍看到遮盖值。到期后不得刷新 Security 投影或调用 Security 判定，必须直接由 Manager 根据本地投影恢复遮盖。Enrollment、Assessment、AccessRequest、Exemption 聚合及不可变修订属于专用 Tenant 的长期治理与审计事实，不作为临时业务资源删除；Assessment 修订使授权立即失效的事务语义由 Security PostgreSQL T2 覆盖，禁止为了 T4 重复运行而篡改或删除不可变审计历史。

MySQL 邮箱四出口保护类 T4 使用专用 MySQL Fixture 的 `customers.email` 和专用 PostgreSQL Fixture 的固定目标表，要求当前 Tenant 已存在启用的 `email` 敏感类型、`addp.detector.email_metadata/v1` 检测绑定和 `suppress` 默认保护规则。suite 必须经 Meta 真实扫描发现字段，经 Security 正式 API 形成或复用 Enrollment 与 Assessment，并等待 `manager/preview`、`develop/query`、`service/service_execute` 和 `transfer/export` 投影全部确认。四个 Owner 都必须继续返回 5 条非敏感数据，同时在返回结构与每条记录中完全移除 `email`；返回空邮箱、原文邮箱、整个请求被拒绝或整个结果为空都不算通过。每轮临时 Query Service 和 Transfer 任务必须按捕获 ID 删除并确认零残留；Enrollment 与 Assessment 是长期治理事实，不在验收后删除。

OceanBase 消费链路 T4 使用专用 OceanBase CE MySQL 模式 Fixture 和永久 `engine_type=oceanbase` Engine Instance。Fixture 固定维护一个 5 行非空间 InnoDB watermark 源表和一个同构空目标表；suite 必须经 Meta 扫描取得两个 ResourceLocator，以同一条 bounded watermark Transfer 任务依次验证首批 5 行、源表确定性更新/新增后的 2 行增量，以及再次执行的 0 行空增量。目标采用 `upsert` 和唯一稳定键 `id`，不得把 OceanBase 降格注册为 MySQL 或为模块增加类型分支。首批与增量完成后，Manager 表预览、Develop SQL 和临时 Query Service 必须通过各自正式数据出口读取同一目标 ResourceLocator，并得到一致 checksum，最终固定为 6 行且无重复键；Manager 预览还必须确认扫描结构中的完整列顺序。临时 Transfer 任务和 Query Service 必须按捕获 ID 删除并确认 404；物理 Fixture 在成功、失败和中断退出路径恢复源表基线与空目标表后停止。该 suite 只登记手工 `workflow_dispatch`，首次真实通过前不得增加定时触发。

TiDB 消费链路 T4 复用同一个关系引擎 owner 断言，在带 `self-hosted`、`macOS`、`addp-online` 标签的专用 Runner 上使用固定官方 digest、无数据卷的 PD/TiKV/TiDB 三组件 Fixture 和永久 `engine_type=tidb` Engine Instance。Fixture 固定重建 5 行非空间 watermark 源表和同构空目标表；suite 必须经 Meta 扫描取得 ResourceLocator，以同一条 bounded watermark + upsert Transfer 任务验证 5/2/0 三次读取计数，并由 Manager、Develop 与临时 Query Service 对最终 6 行结果计算相同 checksum。上层模块只消费通用 ResourceLocator、SQLDialect 和 Provider，不得把 TiDB 登记为 MySQL 或增加 `tidb` 类型分支。临时任务和服务必须按捕获 ID 删除并确认 404；成功、失败和中断退出路径都必须执行 `down --volumes --remove-orphans` 并验证三组件容器零残留。该 suite 首次真实通过前只登记手工 `workflow_dispatch`，不得增加定时触发。

KingbaseES 消费链路 T4 复用同一个关系引擎 owner 断言，仅在带 `self-hosted`、`Linux`、`X64`、`addp-kingbase` 标签的受保护 Runner 上使用固定 SHA-256 的 V9R1C10 `V009R001C010B0004` 官方介质和 owner 正规 License。Fixture 每轮启动无卷 `DB_MODE=pg` disposable 容器，固定重建 5 行非空间 watermark 源表和同构空目标表，并输出 owner-only Engine 描述；通用注册器以最小权限 Provisioner Token 通过正式 System API 创建当次 `engine_type=kingbase` Engine Instance。suite 经 Meta 扫描取得 ResourceLocator，以 bounded watermark + `ON CONFLICT` upsert 验证 5/2/0 和最终 6 行 checksum，并由 Manager、Transfer、Develop、Service 经各自正式出口取得一致结果。上层模块只消费通用 ResourceLocator、SQLDialect 和 Provider，不得增加 `kingbase` 分支。成功、失败和中断路径都必须删除本轮平台库、Engine Instance、容器、镜像、临时任务、服务和 owner-only 凭据目录，验证零残留；License 和镜像 tar 不归档。首次真实通过前只允许手工 `workflow_dispatch`。

openGauss 消费链路 T4 复用上述关系引擎 owner 断言，但只在 GitHub Hosted `ubuntu-24.04` x86_64 profile 上运行。生命周期从固定 SHA-256 的 openGauss 6.0.6 LTS 官方介质启动独占容器，创建 `addp_online` 平台库、非默认 Tenant、最小权限消费 User、只含 `system.engine.create/read/execute` 的 Engine Provisioner 和临时 `engine_type=opengauss` Engine Instance。Business Fixture 只管理数据库并输出 owner-only Engine 描述，不调用 System API；System IAM owner helper 只创建身份和 Token，通用 Online 注册器使用 Engine Provisioner Token 通过正式 System API 注册并验证 Engine。Fixture 以 `public` schema 作为 namespace，使用 openGauss 原生 `MERGE` 推进相同的 5/2/0 watermark 数据集；Manager、Transfer、Develop 和 Service 必须通过与 OceanBase 相同的通用 ResourceLocator、SQLDialect 和 Provider 路径得到同一 6 行 checksum，上层模块不得新增 `opengauss` 分支。当次 Engine Instance 只随 disposable 平台库销毁，Token 和连接凭据只保存在不归档的 owner-only `runner.temp` 目录；Engine 注册后必须在进入业务断言前从进程环境清除 Provisioner Token 与数据库凭据，业务证据不得包含凭据。该 suite 在首次真实通过并确认构建身份、清理和零残留证据后，同时保留手工 `workflow_dispatch` 并登记每日夜间 `schedule`；定时事件只能选择 openGauss Hosted Job，其他 Online Job 必须显式排除定时事件。

Transfer 仅新增 MySQL T4 使用永久 PostgreSQL 与 MySQL Engine Instance，并由专属复合 Fixture 管理两端物理表。真实 User 必须从 Console 页面选择源表和目标库、进入字段映射、通过正式“分析源数据并推荐精度”接口把无声明精度的 PostgreSQL `numeric` 收敛为不会截断当前值的 MySQL `DECIMAL(6,2)`，再以主键 `id` 作为唯一 watermark 创建任务。页面自动执行的首轮必须读写 6 行；Fixture 同时修改 `id=1` 并新增 `id=7` 后，用户从任务详情再次执行必须只读写 1 行。最终 MySQL 目标必须共 7 行、`id=1` 保持首轮值、`id=7` 为新增值且无重复键，以证明单字段水位只覆盖严格递增新增、不承诺旧记录更新。浏览器还必须断言任务配置的 `tie_breaker=[]`、目标 `upsert` 键为 `id`，捕获并删除临时任务且确认 404；Fixture 在全部退出路径删除两端固定表并停止两个容器。该 suite 只登记手工 `workflow_dispatch`，首次真实通过前不得增加定时触发。

Transfer 关系型 SQL ETL T4 使用永久 PostgreSQL Engine Instance，由专属 Fixture 管理同一数据库中的固定源表和目标表。真实 User 必须从 Console 选择源表，确认单语言引擎只显示固定 `SQL` 而不提供 MQL、语言下拉或高级 SQL 编辑入口，通过基础构造器选择输出字段并配置两个参数化行过滤，再选择 PostgreSQL 目标位置创建 snapshot 任务。浏览器必须断言保存事实只有标准 `source.query` SQL statement 与类型化 parameters，其中 decimal 过滤值以字符串保存而不是 JavaScript Number；下一步字段映射只包含实际投影字段，并等待 Worker 读写恰好 2 行。Fixture 必须核对目标行集合、金额汇总和列顺序，以同时证明过滤、投影和精确参数绑定语义。临时任务必须通过正式 API 删除并确认 404，Fixture 在全部退出路径删除两端固定表并停止 PostgreSQL 容器。该 suite 只登记手工 `workflow_dispatch`，首次真实通过前不得增加定时触发。

### 5.3 数据、超时与清理

T4 临时夹具优先通过 owner 正式 API 创建；正式 API 无法建立必要前置状态时，才允许 owner 提供专用测试 helper。Hosted profile 的全新平台库在尚无可登录 User 时，可由 System-owned helper 调用正式 IAM Service 创建当次 Tenant、User、Role 和 Session；helper 必须限定 GitHub Hosted Linux 及 `addp_online`，不得通过 SQL 写入 Principal、Role、Assignment 或 Token。Engine Instance 等永久身份按上一节使用预置专用 Fixture，不适用“每轮创建后删除”；Hosted disposable profile 的当次 Engine Instance 随平台数据卷整体销毁。跨模块 Online 场景不得以直接 SQL 作为常规夹具路线。

每个 suite 必须：

- 让全部资源名称可追踪到 Run ID。
- 为总场景、单次 HTTP 请求、路由收敛和租约收敛设置明确超时。
- 只对临时传输失败或规范允许的异步收敛执行有界重试；业务冲突、权限失败和 409 不自动重试。
- 在成功、失败、超时与中断路径执行 owner 清理并检查错误。
- 结束时查询并断言残留为零；清理失败时，即使业务断言通过，门禁仍失败。
- 将故障注入放在测试 Transport、测试 Provider 或明确测试 hook，不在生产代码中保留调试分支。

### 5.4 报告

统一分发器必须在仓库外证据目录生成 `addp.online-gate/v1` 报告，成功与失败采用同一结构。报告至少包含：

- suite、scenario、Run ID、Git commit 和工作区状态。
- 参与服务的脱敏地址与构建身份。
- Tenant、数据库类别、阶段耗时和稳定错误码。
- owner suite 创建、清理和零残留证据。

报告、日志和 CI artifact 不得包含 Token、Client Secret、完整敏感响应或可复用凭据，也不得提交到仓库。

### 5.5 触发策略

新增 T4 suite 先通过手工 `workflow_dispatch` 执行。专用 Runner 至少完成一次真实通过，并确认构建身份、清理和零残留证据后，才允许增加夜间 schedule。环境未就绪必须报告环境失败，不得 Skip 为通过。

## 六、T5 发布认证协议

T5 按产品或 Runtime 独立准备真实前置条件，例如 macOS Keychain、安装包生命周期、HA、故障切换或在线厂商证据。统一 `test-release` 分发器只选择 owner 门禁并生成 `addp.release-gate/v1` 报告，不合并不同产品的运行条件。

厂商只提供离线 Docker tar、且 Runtime 受 OS/CPU 架构约束时，官方介质认证归 T5：owner 脚本必须在一处固定官方 HTTPS 地址和 SHA-256，负责下载、校验、`docker load`、原生启动、真实驱动/SQL 断言及所有退出路径清理；workflow 只选择匹配架构的 Runner 并调用统一 `test-release` 入口。此类 suite 不声明 `ADDP_T2_SERVICES`、不进入 `test-integration` 或辅助 macOS 巡检，也不能替代插件实现后的 T2 Provider 集成与 T4 跨模块验收。

官方介质包含限时试用 License 时，T5 只允许将其用于首次准入评估，不得以重新下载介质、重建容器、替换主机或清除状态的方式重置试用期。长期门禁必须由 owner 提供来源合法、当前有效且 SHA-256 明确的 License，门禁报告只记录授权元数据与摘要，不归档 License 原文。

发布 workflow 只准备环境、调用标准 Make 入口、归档证据和执行发布动作，不在 YAML 内重写业务测试。System IAM PostgreSQL 属于 T2，不因与发布流程共用 workflow 而变成 T5。

未具备真实凭据或 Runtime 的 T5 suite 不得登记占位实现；需要人工认证时必须明确记录未验证项和后续责任门禁。

KingbaseES 的 T2 Provider 与 T5 官方介质认证暂不接入 GitHub Actions，保留标准命令与确定性回归，由持有正规 License 的 owner 在受控 Linux x86_64 主机人工执行。T5 未声明 `workflow_job` 表示不由 GitHub Actions 编排，登记检查拒绝 workflow 调用其 suite 或内部 owner 目标；恢复自动执行前需另行确认 Runner 隔离与生命周期方案。此调整不改变 Online T4 的现有协议，脚本回归通过不代表真实授权认证通过。

## 七、CI 编排与登记

产品镜像构建继续由 `make build-images` 唯一入口负责。构建调度最多同时执行两个任务，基础镜像预热先于任务启动；单路和两路使用同一实现。前端 Node 版本来自根 `.node-version`，仅使用锁文件安装依赖。`make test-platform` 校验这些契约，并验证并发上限、失败汇总、日志隔离及中断时的进程和临时目录清理。现有 Platform CI 产品构建 Job 复用该入口验证实际镜像；Node 版本变更必须通过影响选择命中全部前端镜像，不缩减受影响服务范围。

发布工作流安全审计统一由 `make test-workflow-security` 执行，并纳入本地 `test-platform` 与 CI 现有 System IAM required Job。唯一脚本固定 zizmor 1.28.0，只扫描 `.github/workflows/release-and-t2-gates.yml`，使用 auditor、medium 最低严重级别、关闭在线审计，不读取自定义配置或忽略标记；安装、扫描或输入收集失败必须阻断。首次运行需联网取得固定版本的二进制 wheel，使用临时虚拟环境且退出时清理；不依赖 ADDP 服务或数据库。

Workflow 只负责：

- 触发条件与 owner 影响选择。
- Runner、语言版本和 disposable Service 准备。
- 调用唯一 Make / script 入口。
- 超时、并发、Artifact、Step Summary 和 required check 名称。

不得在 workflow 中复制 SQL、业务夹具、测试选择表达式、模块启动逻辑或清理逻辑。能通过 Git 和依赖声明自动发现的事实不维护手写清单；必须手工登记的门禁由 `make test-platform` 的一致性检查验证完整性。凡通过 `ADDP_T2_SERVICES` 声明 PostgreSQL、MongoDB、MySQL、OceanBase、TiDB 或后续数据库 Service 的 T2 门禁，都必须同时登记根 Make 入口、`test-integration` 串行聚合、共享模块变更选择和带摘要的 CI Job；每个声明的 Service 必须存在，并使用显式版本 tag 与镜像 sha256 digest。T5 suite 如声明 `workflow_job`，登记检查必须通用验证该 Job 只经 `test-release` 分发、配置仓库外证据目录并挂接统一摘要，不按产品或数据库类型增加检查分支。

新增或修改模块、测试入口、基础设施依赖、构建方式或 suite 时，必须在同一次变更中同步：

1. owner 测试与安全检查。
2. 根 `Makefile` 标准入口。
3. 自动发现、影响选择或显式 suite 登记。
4. `.github/workflows/` 编排与报告。
5. 对应规范、模块说明或运行指南。

不得提交一个永久排队、连接开发环境或始终 Skip 的占位 workflow。

### 7.1 辅助 macOS 定时巡检

日常使用的 macOS 可以在独立 checkout 中定时运行 `make local-ci`，作为 GitHub Actions 之外的辅助反馈机制。该入口只复用已有 T0-T3 标准门禁，不产生第二套测试事实，也不替代 GitHub required checks。

- checkout 必须专用于自动巡检，处于 `main`，且不得包含已跟踪或未跟踪改动；脚本只使用 `git fetch` 与 fast-forward，不使用 reset 或 clean 覆盖现场。
- 首次运行执行全部确定性门禁和已登记基础设施集成门禁；之后以上次成功 SHA 为 `BASE_REF` 运行 `make test-changed`。每个新 SHA 同时执行根 `make build BUILD_ARGS=--force`，复验全部 Linux 产品二进制。失败不推进成功基线，后续调度必须重试同一提交。
- 测试范围与远端同步是两个正交选择：默认执行“同步 `origin/main` + 增量”，`--full` 强制全量，`--no-fetch` 跳过 `fetch` 与 fast-forward；专用机需要在当前干净 `main` 上执行不拉取的完整巡检时，统一使用 `make local-ci LOCAL_CI_ARGS="--no-fetch --full"`。`--check-only` 是独立的就绪检查，不与其他选项组合。运行日志和 `latest-summary.txt` 必须分别记录实际 `scope` 与 `remote_sync`，不能把“不拉取”误报成“增量”。
- Python 与前端依赖准备只是环境编排；真实断言仍由根 `Makefile` 与 owner 门禁拥有。
- 本地 PostgreSQL 继续只使用 `addp_test` 与 `addp_iam_test`，集成门禁严格串行。持久基础设施只启停 `addp-infra` Compose 项目；巡检专属 MySQL 和 OceanBase 由唯一的 `addp-local-macos-ci` Compose 项目管理，不操作其他 Compose 项目。脚本不执行 Docker 全局 prune，也不删除 `addp-infra` 数据卷。
- MySQL owner 门禁使用本地巡检专属、固定版本且无数据卷的 MySQL 8 服务。该服务只绑定 `127.0.0.1:13306`，必须在确定性门禁和编译前通过健康检查；外层聚合门禁只携带本地作用域标记，真实连接参数只在 `test-common-mysql-data-protection` 进程内生成，不能写入仓库根 `.env`、传给其他 owner 门禁或连接 Business/生产 MySQL。
- OceanBase owner 门禁使用本地巡检专属、固定 digest 且无数据卷的 OceanBase CE 4.4.2 LTS 服务。该服务只绑定 `127.0.0.1:12881`，并且必须在确定性门禁和编译前通过健康检查；外层聚合门禁只携带本地作用域标记，`common-oceanbase-gate.sh` 在该标记下固定生成 `addp_oceanbase_disposable` 连接参数。巡检启动前必须清除继承的 `ADDP_TEST_OCEANBASE_*`，不读取根 `.env`，不连接 Business/生产 OceanBase。
- 两个巡检专属数据库服务都必须在巡检成功、失败和中断的统一退出清理中删除。
- 辅助巡检不运行 T4 Online 或 T5 发布认证；两者继续遵循各自的专用环境、身份与报告协议。
- 日志、上次成功 SHA 与运行锁位于 checkout 的 Git 内部状态目录，不进入工作树；日志不得包含 Token 或可复用凭据。

## 八、交付与完成标准

实施前识别受影响的 T0-T5 层级。交付前优先运行：

```bash
make test-changed
```

验证指定 owner 或已提交区间时分别使用 `make test-module MODULE=<module>`、`make test-changed BASE_REF=<ref>`。受环境限制无法运行时，必须列出未验证项、原因以及负责验证的现有 CI 门禁。

测试体系变更完成必须满足：

1. 新逻辑只有一条标准入口，旧入口、变量和旁路已删除。
2. owner、测试层级、数据与身份边界明确。
3. 本地标准入口、CI 编排、登记检查和文档同步。
4. 最小充分门禁真实执行，未以 Skip、旧进程或错误环境冒充通过。
5. 运行产物位于操作系统临时目录或 CI artifact，仓库无残留。

### Business Redis 门禁

`make test-business-config` 覆盖脚本语法、固定官方镜像、回环绑定及实际端口首次避让与重启复用。`make test-business-redis` 属于 Business owner 的独占 Compose T2：随机密码、随机回环端口、私有数据卷，实际验证 ACL、string/hash/list/set/zset/stream、TTL、二进制 key/value、大整数文本、幂等初始化、类型冲突拒绝和 AOF 重启持久化，退出核对容器、卷、网络零残留。新 owner 从 `ADDP_T2_OWNED_SERVICES` 声明自动发现，不要求为了测试创建新的服务模块或维护第二份 owner 清单。该门禁已登记 `make test-integration` 和 `release-and-t2-gates.yml`；它不证明 Redis Engine Plugin、Meta/Manager/Develop 或 T4 业务链路已经实现。

### Elasticsearch 门禁

- T1：`make test-common-elasticsearch-unit` 验证受限 DSL、精确读集、HTTPS CA/认证、PIT 部分失败/取消清理及通用记录预览。
- T2：`make test-common-elasticsearch` 使用固定官方镜像、独占 Compose project 与随机回环端口，创建只读用户和幂等样例，实际验证 Common、Manager、Meta，完成后检查容器和卷零残留。该脚本已注册根 Makefile、模块/变更自动发现及 `release-and-t2-gates.yml`。
- T4：`make test-online ONLINE_SUITE=elasticsearch-consumer-flow` 通过 System、Meta、Manager、Develop 验证同一索引、空索引、数值精度和来源投影。必须使用标准专用 Online 部署与非默认测试租户，常规用户凭据，以及该租户中已注册的只读 ES 引擎 `ADDP_ONLINE_ELASTICSEARCH_ENGINE_ID`，其中需存在 Business 样例索引。套件已注册 `online-host-gate.sh` 和 `online-t4-gates.yml` 的人工触发选项；禁止在本地共享 PostgreSQL 中创建 Online database。

### Model3D Runtime 确定性门禁

`make test-model3d-workflow` 覆盖 DAE / 3DS 首期源引用和动画边界、GLB 自包含结构、真实 PNG/JPEG 解码以及校验失败时不发布或替换已有 artifact。该入口由 `make test`、`make test-module MODULE=engines` 自动发现和 Platform CI 的 `model3d-workflow-tests` 执行；可通过 `MODEL3D_WORKFLOW_PYTHON` 指定已安装 requirements-dev 的隔离解释器。测试使用受控 converter runner，不计为专业转换器或平台真实链路验收。已登记 Make 入口的 Python engine runtime 由 Python CI 登记检查统一核对根聚合、标准环境准备、共享模块影响选择与 CI Job。

Redis 连接首版门禁：`make test-common-redis-unit` 验证严格配置、认证、取消、TLS 信任链与主机名；`make test-common-redis` 使用独占真实 Redis 验证 Common 插件和 System 登记、密码加密/脱敏与连接状态。与 `make test-business-redis` 共用 `redis-owned-fixture.sh` 生命周期和固定官方镜像，分别验证两个消费边界；均使用随机密码、随机回环端口并检查容器/卷/网络清理，不访问已有 Business 或 Infra Redis。两项 T2 均登记根 Makefile、模块/变更发现和 `release-and-t2-gates.yml`。共享生命周期通过 `ADDP_T2_LIFECYCLE_SCRIPT` 显式登记，CI 检查必须同时确认 owner 脚本调用、变更输入覆盖、Compose 归属和真实启动/清理逻辑，不能以未调用的 helper 冒充门禁。

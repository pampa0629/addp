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
- Common PostgreSQL 的默认 `make test-common-postgres` 保持完整门禁，同时覆盖表写入准备、安全列演进和空间结构拒绝。真实低权限账号必须在没有数据库级 CREATE 权限时完成已有 schema 下的建表与 upsert 准备；缺失 schema 仍须按数据库权限拒绝或创建，测试结束清理本轮 schema、账号及授权并验证零残留。仅验证查询读取集合及输出血缘时，可在同样的显式测试连接条件下运行 `bash scripts/test/common-postgres-gate.sh --test query-read-set`；该分组包含真实资源改名及同名重建的定位身份案例，保留数据库身份检查、Skip 拒绝和场景清理。分组结果只证明所选范围，不替代完整 T2 门禁；CI 继续使用无分组选项的默认入口。
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

企业目录发布的 `enterprise-catalog-publishing` 沿用专用 macOS profile：正式 User 发起共享受理后，生产 Catalog 通过自己的 OAuth 服务身份自动签发。验收脚本不借用该身份，System owner 的现有 Online 测试夹具仅以只读可重复读事务核对精确 Tenant、Engine、请求编号的受理、Grant 和唯一签发成功审计；原参重试后再次核对签发时间与审计次数不变。该观察模式只允许回环 `addp_online`，禁止初始化身份、执行迁移或写入，不是人类 API、Catalog Grant 副本或访问裁决。确定性协议与准入测试归 `make test-online-runner`，真实 PostgreSQL 查询及只读拒写归现有 `make test-system-iam-postgres`，完整 OAuth 链路仍须在现有手工 T4 suite 真实执行后才能计为通过。

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

三种 profile 共用同一个 `make test-online` 分发器和 owner 业务断言，生命周期脚本只负责当次环境和夹具。编排只调用现有 Infra、开发生命周期脚本和 `make test-online`，不得在 workflow 中复制模块启动逻辑或业务断言。Hosted 原生 Linux 经开发入口启动的 GeoPython、PointCloud、Document 容器使用宿主网络，直接以配置端口监听，通过回环地址访问 System、Infra 与 Business MinIO；不得用 bridge 的 host gateway 访问仅发布回环端口的服务。该启动契约由 `make test-dev-lifecycle` 覆盖，真实对象存储读写仍由 Manager T4 验证。退出路径必须停止本次应用进程并报告清理结果；常规 macOS self-hosted Infra 可在专用 Runner 常驻，Hosted 与 License 受控 owner-managed profile 的 Infra 必须随 Job 销毁。

Security 四出口 T4 `security-mysql-owner-protection` 只使用 GitHub Hosted Linux x86_64 disposable profile，入口为 `scripts/test/online-hosted-security-gate.sh`，不再由 macOS self-hosted 分发。每轮创建独立 PostgreSQL/PostGIS 和 MySQL 业务容器，复用共享 Infra 与标准开发生命周期启动真实 System、Gateway、Meta、Security、Manager、Develop、Service、Transfer。System owner helper 使用正式 IAM Service 创建当次 Tenant 和最小权限非管理员验收用户；独立引擎登记及治理初始化用户的令牌只用于准备阶段，初始化通过正式 API 完成。MySQL 邮箱移除、PostgreSQL 空间表三类普通属性算法和 Manager 字段独立配置沿用原有 owner 断言。所有退出路径停止应用、销毁业务容器及 Infra，并核对零残留和删除仓库外凭据。确定性回归进入 `make test-online-runner` 与平台 T0；首次真实 Hosted 运行成功前仅允许手工调度，不计为 T4 已通过。

Manager 内部产物 T4 使用 `manager-internal-artifact-lineage` 单一 suite 和 GitHub Hosted Linux x86_64 disposable profile，不再由 macOS self-hosted 分发。`online-hosted-manager-gate.sh` 复用共享 Hosted 生命周期，从零建立 Infra、最小权限 User、Tenant 和 Business MinIO Engine；浏览器登录凭据只保存到当次仓库外 owner-only Secret 目录。通过正式构建入口构建 Linux amd64 Model3D 转换器，由标准开发生命周期启动 System、Gateway、Meta、Manager、Monitor、Console、PointCloud、Document 和 Model3D Runtime；不启动无关模块或模拟转换器。退出时停止应用并销毁 Business MinIO 容器、网络、数据卷及当次 Infra，检查零残留。Business MinIO 的确定性生成 LAS、仓库已跟踪的 3 页 PPTX、带 PNG 贴图的 DAE 与 3DS 必须经过同一次真实 Meta 深度扫描，再通过对应 Manager owner action/task 与真实 Runtime 生成 COPC、PDF、GLB。模型断言覆盖 `model_3d + single` 分类、贴图资源引用、自包含 GLB 的内嵌 PNG 与非空网格、Manager/Monitor 血缘一致，以及 Console iframe 中 GLB 请求成功、模型加载完成和截图。Online Chromium 统一显式使用 SwiftShader 软件 WebGL，在无独立 GPU 的 Hosted Runner 上仍执行真实模型绘制；不屏蔽业务警告或用截图占位。浏览器报告 v2 单列 Chromium 截图产生的精确 `ReadPixels` GPU 性能诊断计数；页面警告、错误及失败的业务请求仍使验收失败。临时产物及任务按本轮捕获 ID 删除，内容和任务分别确认 404，模型源的用户预览模式恢复原值；执行尚未终结或无法确认资源归属时不得报告零残留。确定性脚本测试沿用 `make test-online-runner`，真实运行沿用 `make test-online ONLINE_SUITE=manager-internal-artifact-lineage`；本次模型扩展首次在 Hosted 环境通过前不计为已完成 T4，也不增加定时调度。

生产 Compose 单入口 T4 使用 Hosted disposable profile：在全新 Runner 上用正式构建入口生成 System Backend、Gateway、Meta Backend、Manager Backend、Transfer Backend、Orchestrator Backend、Console、System Frontend、Meta Frontend、Manager Frontend、Transfer Frontend、Orchestrator Frontend 和 Nginx 镜像，以 `addp-platform` project 启动这些真实容器及当次 Infra。Meta、Manager、Transfer 与 Orchestrator Backend 向 System 自注册，Gateway 从有效 Backend 实例发现对应的 `/api/v1/:module/*`；模块 Frontend 由 Nginx `/module-ui/{frontend}/` 转发，Console 的 `/{module}/` 地址继续由根入口处理。内置 Runtime 属于独立 `addp-runtimes` project，不参与公开入口的业务断言；同一 Runner 额外启动一个真实 GeoPython Runtime 和一个真实 Business MinIO，验证 `addp-runtimes`、`business` 的独立生命周期、Compose project 标签、网络归属和宿主机端口边界。

对外 Nginx 使用非默认回环端口；仅 T4 Compose 覆盖文件允许 System Backend 和 Gateway 额外发布回环诊断端口供通用构建身份预检，正式 Compose 保持只有 Nginx 对外发布。未参与验收的 Nginx 静态 upstream 只提供当次网络名称占位，不模拟参与断言的服务。业务断言必须从公开入口检查 Console、System、Meta、Manager、Transfer、Orchestrator Frontend 及其构建资源，并使用当次一次性 Tenant 的零权限、只读、仅创建 User 验证 System AuthContext 的精确 Permission 集合；System、Meta、Manager、Transfer、Orchestrator 的 API 分别验证未认证 401、无权 403，以及已授权读取。Meta、Transfer、Orchestrator 的写入入口还需用无效请求体证明仅创建权限可越过功能守卫但被参数校验拒绝，且只读账号不能写入；System Role Assignment 详情需证明本租户授权可读、另一租户的授权 ID 返回 404。无效请求体不得创建业务资源。测试还需核对 System 对外 origin、Meta、Manager、Transfer 和 Orchestrator Backend 的就绪与内部地址以及容器实际端口；全部退出路径分别销毁 Runtime、Business MinIO、平台应用、占位容器、镜像仓库和 Infra，核对四个 project 的容器、网络、卷零残留。Transfer Worker 的数据执行和 Orchestrator 的 DAG 执行分别由 owner T4 验收，不以本套入口检查代替。[Hosted T4 run 36141592135](https://github.com/pampa0629/addp/actions/runs/36141592135) 已通过此前的 Transfer 覆盖验收；扩展后的权限矩阵及 Orchestrator 覆盖须在更新提交上手工运行成功后才能计为通过。

同一 `compose-public-origin` suite 必须通过真实生产构建的 Console、Manager iframe 和 `/module-ui/manager/` 独立界面验证 Browser AuthSession。System owner helper 只把既有只读 User 的登录凭据输出到 owner-only 凭据目录；浏览器通过正式登录 UI 建立会话。测试在浏览器中暂时持有共享刷新锁以确定性地制造两个顶层页面同时请求续期，不拦截或伪造任何 HTTP 响应，必须证明只有一次真实 Refresh 请求、HttpOnly Cookie 轮换、三个界面令牌收敛、独立页刷新不额外续期，以及独立页退出后 Cookie 清除和 Console 同步退出。报告和截图不得包含凭据；浏览器证据缺失、身份不匹配或多次续期均使该 suite 失败。确定性验证沿用 `make test-online-runner`，真实部署沿用该 suite 的 Hosted T4 Job。 报告另行记录续期是否保留原 iframe 实例；该诊断不代替未保存业务状态的验收，出现重建时必须在交付中明确说明。

质量动态绑定 T4 `quality-dynamic-binding` 使用专用 Tenant 的两个已审批 Model 物化任务作为永久夹具；两个 PostgreSQL 目标表必须预先存在、结构相同、行数稳定且非零。每轮经正式 API 创建带 Run ID 的规则、方案和编排，验证 Model 默认版本输入、上游 `target_locator` 动态绑定、失败阻断、重复失败工单去重、两个目标的结果与工单隔离，以及显式升级规则修订后逐目标恢复。方案默认绑定不得被执行覆盖，Model 定义和物理数据不得修改。清理只删除本轮编排、方案及规则并确认 404；执行审计保留，方案关联工单由 Quality 删除方案事务清理。执行未终结或创建响应丢失时不得报告零残留。入口和确定性脚本测试分别为 `make test-online ONLINE_SUITE=quality-dynamic-binding`、`make test-online-runner`；只登记手工 T4，首次专用环境真实通过前不加入夜间调度。

Orchestrator 执行 T4 `orchestrator-execution` 复用 Hosted 生命周期、System owner 身份夹具和既有一次性 PostgreSQL 样例源，正式启动 System、Gateway、Meta、Orchestrator、Monitor。每轮经 API 创建扫描任务和嵌套串行 DAG，核对父子执行、成功进度、真实数据源中断后的失败阻断、安全步骤/事件以及跨 Tenant 与缺少 Owner 权限时的不可见性。故障操作必须核对当次源容器标签，恢复后删除本轮任务；原始执行历史由 Hosted Infra 销毁，凭据不归档。入口为 `make test-online ONLINE_SUITE=orchestrator-execution`，准备和退出由 `scripts/test/online-hosted-orchestrator-gate.sh` 负责，确定性测试进入 `make test-online-runner` 与 T0。首次真实通过前只允许手动 `workflow_dispatch`。该首轮只覆盖业务数据源中断；后端进程崩溃和 POST 响应丢失由同一 suite 的故障扩展验收，续租故障仍待后续真实验证。

权限边界扩展沿用同一手动 Hosted T4，不新增认证旁路或生产故障开关。System IAM 夹具提供同 Tenant 的普通读者，其 Orchestrator 读取与 Monitor 读取分别赋权；父执行及嵌套编排可读，Meta 子执行不授予读取权，树仅显示两层编排，子执行详情、树和事件返回 404。独立 Tenant 管理员仅通过正式 IAM 接口撤销该读者的 Orchestrator 角色分配；先验证旧令牌失效为 401，再经正式登录取得同 User、Membership 和 Tenant 的新令牌，确认 Monitor 权限仍在、父执行详情、树及事件均为 404。外部代理只中断本轮 Meta 的 execution-read-scope GET 连接，由真实 Monitor 返回 503 execution_owner_unavailable 且不含执行数据；恢复连接后核对完整执行树不变。凭据与代理控制只存于不归档的 owner-only 临时目录，业务报告不保存角色分配编号，未知撤权结果不重试，失败路径释放故障并由标准生命周期销毁部署。确定性验证归入现有 make test-orchestrator-online-runner 与 make test-online-runner / 平台 CI；扩展首次真实通过前不计为 T4 已通过。

权限扩展已在提交 `759ca7736` 的 [Hosted T4 37126213434](https://github.com/pampa0629/addp/actions/runs/37126213434) 真实通过，本轮共 20 项检查。普通读者可读取父执行与嵌套编排，Meta 子执行的详情、树和事件均不可见；正式撤销 Owner 角色分配后，旧令牌返回 401，重新登录仍保留 Monitor 权限时父执行详情、树和事件返回 404。中断 Meta 读取范围连接时，真实 Monitor 返回无执行数据的 503 `execution_owner_unavailable`，恢复后完整执行树一致。此前的业务源中断、响应丢失与 Backend 崩溃检查同时通过，两类派发故障均无重放。System、Gateway、Meta、Orchestrator、Monitor 的构建身份均匹配该提交；四个临时任务定义经正式 API 删除，生命周期清理通过，Infra 容器、网络及数据卷零残留。61 个归档文件未包含身份凭据及代理控制文件，也未检出标准令牌或夹具密码。此结果不包含续租故障、一次性执行及已删除任务历史的真实验收。

已删除任务历史扩展沿用同一 `orchestrator-execution` 手动 Hosted T4。所有已知父子执行收敛为终态后，经正式 API 删除本轮任务定义；Orchestrator 用任务详情确认删除前存在、删除后 404，Meta 用正式扫描任务列表确认删除前存在、删除后消失。删除前后逐项比较成功与失败执行的 Monitor 概览、安全步骤、错误类别、父子树和根执行事件，以及 Owner 已保存的步骤事实，禁止用当前定义重建历史。删除后再次验证跨 Tenant、缺少 Owner 权限、父可读而 Meta 子不可读；随后正式撤销父读取分配并重新登录，确认保留的历史仍受当前权限限制。删除请求结果未知时不重试，不报告清理成功；定义删除后任一历史或权限断言失败，整套验收仍失败，执行历史由既有 Hosted 生命周期销毁。报告只记录检查名、定义删除数量及已有安全执行身份，不保存前后快照或原始步骤结果。共享 Online HTTP 客户端按调用方声明校验对象或数组响应，Meta 列表明确采用数组契约，不修改公开 API。确定性用例复用 `make test-orchestrator-online-runner`，共享客户端通过 `make test-online-runner` 扩散验证，现有平台 CI 自动覆盖，无需新增登记；实现变更后须重新运行真实 T4 才能计为通过。

已删除任务历史扩展已在提交 `a5d8428ad` 的 [Hosted T4 37129222858](https://github.com/pampa0629/addp/actions/runs/37129222858) 真实通过，共 24 项检查。四个临时任务定义经正式 API 确认删除；删除前后，成功与失败执行共七条记录的安全概览、步骤、错误和父子树保持一致，两个根执行的事件及 Owner 保存的步骤事实也保持一致。删除后，跨 Tenant 和缺少 Owner 权限的读者对七条记录的详情、树和事件均不可见；父读取者仍可读取两层编排，Meta 子执行仍不可见。随后正式撤销 Owner 读取分配，旧令牌为 401，重新登录保留 Monitor 权限时，已保留的父执行详情、树和事件为 404。业务源中断、响应丢失、Backend 崩溃及 Owner 不可用检查同时通过；两个派发故障仍各只有一次请求和一个真实子执行，无重放。System、Gateway、Meta、Orchestrator、Monitor 的构建身份均匹配该提交，生命周期清理通过，Infra 容器、网络和数据卷零残留。55 个归档文件未包含凭据环境文件、代理控制或诊断快照，未检出标准令牌和夹具密码模式。同一提交的 [Platform CI 37129088307](https://github.com/pampa0629/addp/actions/runs/37129088307) 全部通过，其中所有者测试 45 项、Online 聚合测试 333 项；Orchestrator PostgreSQL 可靠性门禁也已通过。此结果不包含一次性执行的真实权限验收与续租故障。

一次性执行权限扩展沿用同一 `orchestrator-execution` 手动 Hosted T4。通过现有 Meta 手动扫描 API 创建无任务定义的真实执行，核对 `source_task_id` 为空、发起主体与当前普通 User 一致、执行收敛成功，Monitor 详情、树和过程事件可读且只含安全投影。System 正式 IAM 夹具增加同 Tenant 的另一普通 User，仅授予 Meta 扫描历史与 Monitor 执行读取权限；先证明该读者能读取已有任务定义的扫描执行，再确认其列表及总数不包含其他 User 的一次性执行，详情、树和事件返回无执行数据的 404。发起人的列表仅增加该执行；跨 Tenant 与缺少 Owner 权限时仍不可见。未知创建结果不重试，已知一次性执行必须收敛为终态，历史随 Hosted Infra 销毁。报告仅增加安全执行 UUID、事件数量、身份权限摘要和闭合检查名，不保存配置、主体编号或响应快照。复用既有 `make test-orchestrator-online-runner`、`make test-online-runner` 与 Platform CI 登记；公开 API、平台进程日志和审计路径不变，真实验收证据见下段。

一次性执行权限扩展已在提交 `d2af33d7b` 的 [Hosted T4 37131353509](https://github.com/pampa0629/addp/actions/runs/37131353509) 真实通过，共 28 项检查。Meta 手动扫描创建无任务定义、主体与发起 User 一致的真实执行并收敛成功；发起人可读取安全详情、单节点树及七条过程事件，执行列表仅增加该记录。同 Tenant 另一普通 User 的 Meta 与 Monitor 读取权限经真实任务历史正向证明后，对该一次性执行的详情、树和事件仍返回无诊断数据的 404，列表及总数保持不变；跨 Tenant 和缺少 Owner 权限时同样不可见。此前的嵌套编排、业务源中断、响应丢失、Backend 崩溃、Owner 不可用、已删除任务历史及正式撤权检查同时通过。四个任务定义确认删除，两类派发故障仍各只有一次请求，无重放。五个服务构建身份均匹配该提交，生命周期清理通过，Infra 容器、网络和数据卷零残留；61 个归档文件未包含凭据环境或代理控制文件，未检出标准令牌及夹具密码模式。同一提交的 [Platform CI 37131346153](https://github.com/pampa0629/addp/actions/runs/37131346153) 全部 29 个任务通过，包括 53 项所有者测试、333 项 Online 聚合测试和正式身份夹具测试；Orchestrator PostgreSQL 可靠性门禁也已通过。[System IAM PostgreSQL 门禁 37131346068](https://github.com/pampa0629/addp/actions/runs/37131346068) 首轮及单独重跑均被共享 main 的后续推送取消，日志未显示测试断言失败，但该门禁尚未完成，不计为通过；需在无新推送的窗口重跑。本轮不包含续租故障。

范围限制：上述一次性执行隔离结论针对 Monitor 通用诊断，不能替代 Meta 专业入口的直接隔离证据。Meta 专业详情与列表的模块归属、任务历史及本人一次性执行读取已由独立回归与 Hosted 验收覆盖，见本文“Meta 专业扫描执行读取”。TaskProvider 服务状态读取仍按专用机器身份与 Permission 独立裁决，不套用用户本人读取规则。

首轮已通过 [Hosted T4 37106631401](https://github.com/pampa0629/addp/actions/runs/37106631401)。故障扩展使用同一手动 suite：Hosted 包装器为真实 Meta 配置只解析到本机的临时网络别名，将 Orchestrator 与 Monitor 的 Meta HTTP 请求交给本轮代理；代理转交真实请求并保存安全身份与计数，可丢弃已接受请求的响应或暂缓状态读取。进程崩溃必须用 pidfd 锁定本轮 Backend，在 UID、可执行文件和部署 Secret 目录核验通过后发送 SIGKILL；替代实例通过 `scripts/dev/start.sh -orchestrator` 启动。断言必须证明父失败原因、依赖未派发、已接受请求只有一次、子执行身份真实、Monitor 投影安全及父终态下步骤仅为最后记录。所有退出路径清理测试代理和本轮网络别名，再由既有生命周期销毁应用与 Infra；新增检查只有在更新提交的 Hosted T4 真实通过后才能计为通过，不自动加入夜间任务。确定性入口为 `make test-orchestrator-online-runner`，由 `make test-online-runner` 和平台 T0/CI 唯一聚合；登记检查同时核对三个 owner 测试文件，禁止遗漏。故障扩展已在提交 `f3d676f5a` 的 [Hosted T4 37122385397](https://github.com/pampa0629/addp/actions/runs/37122385397) 完成 15 项真实检查，并确认构建身份一致、任务定义删除、生命周期清理通过和 Infra 零残留；两类故障均为一次派发、一个真实子执行，父失败且子执行正常完成，等待步骤保持最后记录。续租故障不在此结果的覆盖范围。

续租故障扩展仍归同一手动 Hosted suite：HTTP 代理等待真实子执行状态请求，仅在当次 Infra PostgreSQL 安装指定父 UUID 的续租拒绝触发器，以非事务序列核对真实续租错误。必须同时证明监督器取消在途请求、父 execution 以 coordinator_stopped 收敛且保留等待记录、依赖不派发、原 Backend 自行退出、真实子 execution 成功、一次提交及标准替代进程启动后的终态/事件不变。准入核对 Hosted 私有目录、当次容器 ID、Compose owner 和数据库凭据；禁止在个人或共享本地数据库安装触发器。临时触发器、函数、序列和 schema 清理失败即失败，所有退出路径仍销毁 Hosted Infra。T0/T1 沿用 `make test-platform`、`make test-orchestrator-online-runner` 与 `make test-online-runner`，现有 Online/Platform CI 自动覆盖，无新增 workflow 或生产接口；[Hosted T4 37169854989](https://github.com/pampa0629/addp/actions/runs/37169854989) 已在包含实现 `7dc121432` 的 `c821b4760` 上通过 34 项检查，证明一次真实续租拒绝、请求取消、原 Backend 自行退出、替代进程启动后不重放、终态稳定与临时数据库对象清理；五个服务构建身份一致，Infra 零残留。[同版本 Platform CI](https://github.com/pampa0629/addp/actions/runs/37169830927) 全部 29 个 Job 通过，T0 直接执行 owner 回归 60 项及 Online 聚合 339 项，无跳过。

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

MySQL 邮箱四出口保护类 T4 使用当次 Hosted MySQL Fixture 的 `customers.email` 和当次 Hosted PostgreSQL Fixture 的固定目标表，由独立初始化用户经正式 API 为当次 Tenant 创建 `email` 敏感类型、`addp.detector.email_metadata/v1` 检测绑定和 `suppress` 默认保护规则。suite 必须经 Meta 真实扫描发现字段，经 Security 正式 API 形成 Enrollment 与 Assessment，并等待 `manager/preview`、`develop/query`、`service/service_execute` 和 `transfer/export` 投影全部确认。四个 Owner 都必须继续返回 5 条非敏感数据，同时在返回结构与每条记录中完全移除 `email`；返回空邮箱、原文邮箱、整个请求被拒绝或整个结果为空都不算通过。每轮临时 Query Service 和 Transfer 任务必须按捕获 ID 删除并确认零残留；Enrollment、Assessment 及不可变审计在业务断言期间保留，退出时随 disposable Infra 整体销毁，不通过 SQL 修改治理事实。

OceanBase 消费链路 T4 使用专用 OceanBase CE MySQL 模式 Fixture 和永久 `engine_type=oceanbase` Engine Instance。Fixture 固定维护一个 5 行非空间 InnoDB watermark 源表和一个同构空目标表；suite 必须经 Meta 扫描取得两个 ResourceLocator，以同一条 bounded watermark Transfer 任务依次验证首批 5 行、源表确定性更新/新增后的 2 行增量，以及再次执行的 0 行空增量。目标采用 `upsert` 和唯一稳定键 `id`，不得把 OceanBase 降格注册为 MySQL 或为模块增加类型分支。首批与增量完成后，Manager 表预览、Develop SQL 和临时 Query Service 必须通过各自正式数据出口读取同一目标 ResourceLocator，并得到一致 checksum，最终固定为 6 行且无重复键；Manager 预览还必须确认扫描结构中的完整列顺序。临时 Transfer 任务和 Query Service 必须按捕获 ID 删除并确认 404；物理 Fixture 在成功、失败和中断退出路径恢复源表基线与空目标表后停止。该 suite 只登记手工 `workflow_dispatch`，首次真实通过前不得增加定时触发。

TiDB 消费链路 T4 复用同一个关系引擎 owner 断言，在带 `self-hosted`、`macOS`、`addp-online` 标签的专用 Runner 上使用固定官方 digest、无数据卷的 PD/TiKV/TiDB 三组件 Fixture 和永久 `engine_type=tidb` Engine Instance。Fixture 固定重建 5 行非空间 watermark 源表和同构空目标表；suite 必须经 Meta 扫描取得 ResourceLocator，以同一条 bounded watermark + upsert Transfer 任务验证 5/2/0 三次读取计数，并由 Manager、Develop 与临时 Query Service 对最终 6 行结果计算相同 checksum。上层模块只消费通用 ResourceLocator、SQLDialect 和 Provider，不得把 TiDB 登记为 MySQL 或增加 `tidb` 类型分支。临时任务和服务必须按捕获 ID 删除并确认 404；成功、失败和中断退出路径都必须执行 `down --volumes --remove-orphans` 并验证三组件容器零残留。该 suite 首次真实通过前只登记手工 `workflow_dispatch`，不得增加定时触发。

KingbaseES 消费链路 T4 复用同一个关系引擎 owner 断言，仅在带 `self-hosted`、`Linux`、`X64`、`addp-kingbase` 标签的受保护 Runner 上使用固定 SHA-256 的 V9R1C10 `V009R001C010B0004` 官方介质和 owner 正规 License。Fixture 每轮启动无卷 `DB_MODE=pg` disposable 容器，固定重建 5 行非空间 watermark 源表和同构空目标表，并输出 owner-only Engine 描述；通用注册器以最小权限 Provisioner Token 通过正式 System API 创建当次 `engine_type=kingbase` Engine Instance。suite 经 Meta 扫描取得 ResourceLocator，以 bounded watermark + `ON CONFLICT` upsert 验证 5/2/0 和最终 6 行 checksum，并由 Manager、Transfer、Develop、Service 经各自正式出口取得一致结果。上层模块只消费通用 ResourceLocator、SQLDialect 和 Provider，不得增加 `kingbase` 分支。成功、失败和中断路径都必须删除本轮平台库、Engine Instance、容器、镜像、临时任务、服务和 owner-only 凭据目录，验证零残留；License 和镜像 tar 不归档。首次真实通过前只允许手工 `workflow_dispatch`。

openGauss 消费链路 T4 复用上述关系引擎 owner 断言，但只在 GitHub Hosted `ubuntu-24.04` x86_64 profile 上运行。生命周期从固定 SHA-256 的 openGauss 6.0.6 LTS 官方介质启动独占容器，创建 `addp_online` 平台库、非默认 Tenant、最小权限消费 User、只含 `system.engine.create/read/execute` 的 Engine Provisioner 和临时 `engine_type=opengauss` Engine Instance。Business Fixture 只管理数据库并输出 owner-only Engine 描述，不调用 System API；System IAM owner helper 只创建身份和 Token，通用 Online 注册器使用 Engine Provisioner Token 通过正式 System API 注册并验证 Engine。Fixture 以 `public` schema 作为 namespace，使用 openGauss 原生 `MERGE` 推进相同的 5/2/0 watermark 数据集；Manager、Transfer、Develop 和 Service 必须通过与 OceanBase 相同的通用 ResourceLocator、SQLDialect 和 Provider 路径得到同一 6 行 checksum，上层模块不得新增 `opengauss` 分支。当次 Engine Instance 只随 disposable 平台库销毁，Token 和连接凭据只保存在不归档的 owner-only `runner.temp` 目录；Engine 注册后必须在进入业务断言前从进程环境清除 Provisioner Token 与数据库凭据，业务证据不得包含凭据。该 suite 在首次真实通过并确认构建身份、清理和零残留证据后，同时保留手工 `workflow_dispatch` 并登记每日夜间 `schedule`；定时事件只能选择 openGauss Hosted Job，其他 Online Job 必须显式排除定时事件。

Transfer 仅新增 MySQL T4 使用永久 PostgreSQL 与 MySQL Engine Instance，并由专属复合 Fixture 管理两端物理表。真实 User 必须从 Console 页面选择源表和目标库、进入字段映射、通过正式“分析源数据并推荐精度”接口把无声明精度的 PostgreSQL `numeric` 收敛为不会截断当前值的 MySQL `DECIMAL(6,2)`，再以主键 `id` 作为唯一 watermark 创建任务。页面自动执行的首轮必须读写 6 行；Fixture 同时修改 `id=1` 并新增 `id=7` 后，用户从任务详情再次执行必须只读写 1 行。最终 MySQL 目标必须共 7 行、`id=1` 保持首轮值、`id=7` 为新增值且无重复键，以证明单字段水位只覆盖严格递增新增、不承诺旧记录更新。浏览器还必须断言任务配置的 `tie_breaker=[]`、目标 `upsert` 键为 `id`，捕获并删除临时任务且确认 404；Fixture 在全部退出路径删除两端固定表并停止两个容器。该 suite 只登记手工 `workflow_dispatch`，首次真实通过前不得增加定时触发。

Transfer 关系型 SQL ETL T4 `transfer-relational-sql-etl` 唯一使用 GitHub Hosted `ubuntu-24.04` x86_64 临时部署，不保留 macOS 专用 Runner 路径。生命周期复用 `scripts/utils/hosted-online.sh`，从零建立独立 Infra、非默认 Tenant 和最小权限测试 User；System-owned IAM helper 创建身份，独立 Engine Provisioner 通过正式 System API 注册当次 PostgreSQL Engine Instance，注册后清除 Provisioner Token；业务 User 不持有 `system.engine.read/execute`，只从 Meta 读取同 Tenant 引擎投影。Business Fixture 只管理独占无持久卷 PostgreSQL 容器、同库固定源表和目标表，并输出 owner-only Engine 描述，不调用 System API。数据库用户只能读取源表并在指定 schema 创建、管理自己的目标表。真实 User 必须从 Console 选择源表，确认单语言引擎只显示固定 `SQL` 而不提供 MQL、语言下拉或高级 SQL 编辑入口，通过基础构造器选择输出字段并配置两个参数化行过滤，再选择 PostgreSQL 目标位置创建 snapshot 任务。浏览器必须断言保存事实只有标准 `source.query` SQL statement 与类型化 parameters，其中 decimal 过滤值以字符串保存而不是 JavaScript Number；下一步字段映射只包含实际投影字段，并等待 Worker 读写恰好 2 行。Fixture 必须核对目标行集合、金额汇总和列顺序，以同时证明过滤、投影和精确参数绑定语义。临时任务必须通过正式 API 删除并确认 404。成功、失败和中断退出路径均销毁当次业务容器、Infra、平台数据卷及 owner-only 凭据目录，检查零残留；Engine Instance 与身份随平台库销毁。凭据不进入日志或 Artifact。该 suite 只登记手工 `workflow_dispatch`，首次真实通过前不得增加定时触发。

同一个 `transfer-relational-sql-etl` suite 同时覆盖字段血缘首期：使用无管理员角色、具备 `meta.lineage.read`、`manager.data_item.read` 和 `manager.content.read` 的同一 User，经正式 API 执行原生表 snapshot 的字段改名、decimal 精度转换、同目标 replace 映射更新和两跳搬运。Meta 必须从真实成功执行自动采集，断言 direct/derived、精确字段与结构快照、旧入边关闭及两跳查询不串入其他字段；不得用手工 collect 请求代替自动消费。Console 的 Manager iframe 必须通过 `subject_kind=data_item&granularity=field` 一次请求整表字段图，选择真实目标字段时在本地图中聚焦，不重复查询。该普通 User 不授予 `catalog.entry.read`，字段图不得触发 Catalog 摘要请求。浏览器先完成登录并核对同一 User 与 Tenant，再监听所有受保护业务请求的错误；登录前无会话的 refresh 401 不计入业务请求，但登录后的 401、403、503 均不得忽略。已有 SQL 查询源继续声明 unavailable，浏览器必须显示证据不可用，不能伪造同名字段来源。三个原生任务与一个 SQL 任务均由本轮 Run ID 命名并通过正式 API 删除、确认 404；源表和三个目标表由同一个 Business Fixture 在退出时清理。确定性验证沿用 `make test-online-runner` 和 `make test-platform`，真实 T4 沿用 `make test-online ONLINE_SUITE=transfer-relational-sql-etl`，首次真实通过前不增加 schedule。

### 5.3 数据、超时与清理

栅格工作流 T4 `raster-workflow` 只在 GitHub Hosted `ubuntu-24.04` x86_64 独占部署运行，复用 `scripts/utils/hosted-online.sh`。Business Fixture 管理两套无持久卷、凭据不同的 MinIO，生成含 EPSG:4326、确定性双波段和 NoData 的 TIFF；System-owned IAM helper 创建非默认 Tenant 与普通 User，独立 Provisioner 经正式 API 注册两个业务 Engine。User 经 Develop 提交 `raster_load → raster_band_math → raster_save(profile=cog)`，必须验证 create、重复 create 失败和 replace，并独立读取成果像元、CRS、transform、NoData 与完整 COG 布局。Meta 的轻量 COG profile hint 与完整布局校验分别验证：物理夹具记录成果是否具有 overview，Meta 的 `is_tiled`、`has_overviews` 与 profile hint 须与该真实结构一致；小尺寸、无 overview 的合法 COG 可被轻量探测表达为 `geotiff`，不能据此拒绝已经通过完整布局校验的成果，也不能用 hint 替代完整校验或人为新增 overview 掩盖断言错误。场景还需重投影确定性源到 EPSG:3857 的 1 米网格，裁剪两个重叠区域，分别验证 first/last 镶嵌 COG 的全部像元、NoData、空间属性和原成果未改变；每个成果均验证自动扫描、派生血缘和同一普通 User 的浏览器展示。目标 DataItem 只来自 Develop 自动提交的 Meta scan，血缘只来自 owner 自动采集，禁止测试手工扫描目标或调用 collect 掩盖自动链路缺陷。Monitor API 与 Console 必须展示同一成功执行及源、目标血缘，浏览器登录身份与 API User 相同。凭据仅存 owner-only runner.temp，不进入业务进程环境或证据；全部退出路径销毁业务容器、临时 registry、平台容器和数据卷并检查零残留。首次真实通过前只登记手工 workflow_dispatch。

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

按路径选择门禁的托管工作流（Platform CI、Release/T2、Quality Smoke）使用独立 Runner 和 Job 独占的 disposable 资源，不设置 workflow 或 job 的 `concurrency` 分组。每次运行保留自己的提交范围与选择结果，由 GitHub Runner 配额排队；后续无关提交不能取消正在执行的门禁，也不能替换待执行门禁。仅将 `cancel-in-progress` 改为 `false` 不足以满足此要求：GitHub 默认仍以新运行替换同组已有的 pending 运行。专用 T4 主机的资源互斥遵循其既有 Job 锁，不套用托管门禁的调度规则。

Push 使用事件的 `before...sha`，PR 使用 base 与本次 SHA 的 merge-base 差异；共享依赖和 owner 声明的测试输入继续由 `changed-gate.py` 统一扩散。文档运行跳过测试只表示该提交未命中路径，不能替代前序代码提交的验收证据。`make test-platform` 的 CI 登记与选择回归约束上述调度，并验证连续相关提交后追加文档、共享依赖、取消/失败/意外跳过的判定；原有超时、owner 清理、零残留核验及 required check 汇总必须保留。

历史案例：[37171513445 的首次运行](https://github.com/pampa0629/addp/actions/runs/37171513445/attempts/1) 在同分支文档 Push 后取消正在执行的 System IAM 和日志 T2；后续[文档运行 37171988879](https://github.com/pampa0629/addp/actions/runs/37171988879) 将这两项测试跳过。旧运行的 IAM 汇总正确判为失败，但文档运行成功并未补齐被取消的测试。验证调度修复时，应在相关门禁执行和排队期间追加无关文档提交，再核对前序 Job 继续完成、排队 Job 实际启动，以及文档运行只跳过自身未命中的门禁。

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
- T4：`make test-online ONLINE_SUITE=elasticsearch-consumer-flow` 唯一使用 GitHub Hosted Ubuntu x86_64 临时部署，由 `online-hosted-elasticsearch-gate.sh` 复用共享 Hosted 生命周期。Business 夹具复用固定官方镜像及样例初始化，创建独占 ES 容器、随机回环端口和只读账号；System owner helper 创建非默认 Tenant、普通消费 User 与独立登记身份，通过正式 Engine API 注册引擎，登记凭据在验收前移除。普通用户只取得 System 目录、Meta 扫描/目录、Manager 预览和 Develop 查询权限，不取得基础设施管理权限。API 及同一身份的真实 Console 验证 Meta 重扫和稳定 item 身份、Manager 25 条文档与空索引、Develop ES DSL 查询，并核对 long 精度、对象/nested 结构、来源投影和排序。任何浏览器失败、报告身份不符、截图缺失或清理残留均不能通过。退出销毁业务容器、平台 Infra 和凭据，检查零残留；不使用个人开发部署或本地共享 PostgreSQL。原 macOS profile 删除，仅登记人工 `workflow_dispatch`；首次真实通过前不计为 T4 通过、不登记 schedule。确定性验证纳入 `make test-online-runner` 与现有 Online CI 自动登记检查。

### Model3D Runtime 确定性门禁

`make test-model3d-workflow` 覆盖 DAE / 3DS 首期源引用和动画边界、GLB 自包含结构、真实 PNG/JPEG 解码以及校验失败时不发布或替换已有 artifact。该入口由 `make test`、`make test-module MODULE=engines` 自动发现和 Platform CI 的 `model3d-workflow-tests` 执行；可通过 `MODEL3D_WORKFLOW_PYTHON` 指定已安装 requirements-dev 的隔离解释器。测试使用受控 converter runner，不计为专业转换器或平台真实链路验收。已登记 Make 入口的 Python engine runtime 由 Python CI 登记检查统一核对根聚合、标准环境准备、共享模块影响选择与 CI Job。

Redis 原生 key 首版门禁：`make test-common-redis-unit` 验证配置、认证、取消、TLS、规范键名、RESP 解码前预算、原生类型读取、Manager 路由与保护边界；`make test-common-redis` 使用独占真实 Redis 验证 Common 插件的 ACL、样本预算、精度及 stream 重复字段，System 登记与加密脱敏，Meta 的 unknown 身份扫描与失败保留目录，以及 Manager 原生预览。与 `make test-business-redis` 共用 `redis-owned-fixture.sh` 生命周期和固定官方镜像；均使用随机密码、随机回环端口并检查容器/卷/网络清理，不访问已有 Business 或 Infra Redis。消费 T2 由 Common/System/Meta/Manager/Business 变更触发，登记根 Makefile、模块/变更发现和 `release-and-t2-gates.yml`。共享生命周期通过 `ADDP_T2_LIFECYCLE_SCRIPT` 显式登记，CI 检查必须同时确认 owner 脚本调用、变更输入覆盖、Compose 归属和真实启动/清理逻辑，不能以未调用的 helper 冒充门禁。Manager 浏览器门禁验证 `data_type=unknown` 的 key 仍进入原生预览，并保留二进制、大整数、score 字符串和 stream 字段顺序；不得将其计为真实部署 T4。

Redis 跨模块 T4 门禁为 `make test-online ONLINE_SUITE=redis-consumer-flow`，仅由 `online-t4-gates.yml` 的人工 `redis-hosted-t4` Job 调用 `online-hosted-redis-gate.sh`，复用 Hosted 一次性 Linux x86_64 平台生命周期。Business 物理夹具使用固定官方镜像、随机回环端口/密码和 tmpfs，复用九个原生样例，不访问开发环境。System owner helper 创建非默认 Tenant、最小权限普通用户及独立登记身份；普通用户只取得目录阅读、Meta 扫描/目录和 Manager 阅读权限，不取得基础设施管理权限。登记凭据在业务验收前移除。套件验证 System 实时 server → key 目录/事实、Meta `data_type=unknown` 身份、Manager 九个原生样例及 page=2 拒绝；真实 Console 浏览器从 Meta 页面发起重扫、确认 DataItem ID 稳定，打开 Manager 资源树和各类原生预览，核对同一 User/Tenant 并输出截图和报告。任何浏览器步骤失败、证据缺失/身份不符或清理残留都不能判为通过。脚本与断言测试纳入 `make test-online-runner`，现有 CI 自动发现登记该 Hosted profile；首次真实 Hosted 运行成功前不计为 T4 通过，也不进入 schedule。

Meta 专业扫描执行读取（2026-10-04 已确认）：直接 HTTP 回归覆盖当前 Owner Permission、合法 OAuth User、本人一次性执行、缺失发起人、同 Tenant 不同 User、跨 Tenant、跨模块/任务类型 UUID、已删除定义历史、分页总数及正确 404；TaskProvider 单独覆盖专用 Client、机器 Permission 与其他 User 发起执行。`make test-module MODULE=meta` 自动发现 API T1 与前端 T3；`make test-meta-postgres` 显式选择 `TestMetaScanExecutionReadAgainstPostgres`，事务回滚测试事实，不创建临时 database。现有 Release/T2 工作流自动登记此标准脚本；真实认证、Gateway 与 Worker 的直接 Meta 检查扩展既有 `orchestrator-execution` 手动 Hosted T4；[37166379368](https://github.com/pampa0629/addp/actions/runs/37166379368) 已在 `60faf18da` 上通过 30 项检查，包含专业入口的任务历史、本人一次性执行、列表总数、跨模块 404 与删除定义后详情保留。OAuth User 和 TaskProvider 权限矩阵由 T1/T2 直接 HTTP 回归覆盖，本轮 T4 不声明相应真实身份验收。

Develop 产物自动扫描来源回归复用 Meta 专业读取的同一直接 HTTP T1/T2 夹具：覆盖 Develop 服务与 Permission、父执行及产物绑定、同租户原发起人读取、其他 User 列表总数不增加、跨租户拒绝、错误模块／任务类型／状态／主体来源和未声明目标拒绝、客户端自报主体或授权字段拒绝、失败请求不落子记录、安全投影不输出配置或授权。`make test-meta-postgres` 继续运行 `TestMetaScanExecutionReadAgainstPostgres`；Go 文件由 `make test-go`、Meta／Develop 标准模块入口和现有 CI 自动发现，无新增入口。Develop T1 验证产物先持久化再提交父 UUID，保存失败或父状态不可用时不发请求。真实 User／Gateway／Worker 的原故障复验使用已登记 `raster-workflow` Hosted T4，不以确定性测试代替。

Develop 自动扫描归属的原故障已在 `015dcb90a` 的 [Raster Hosted T4 37190920380](https://github.com/pampa0629/addp/actions/runs/37190920380) 复验闭合：归档中五次 `develop.workflow.produced_target` 扫描完成，八次扫描 execution 详情读取均为 200；既有首次创建、冲突保护、替换和两种 mosaic 链路已越过自动扫描读取检查。六个参与服务的构建提交均为该版本。本轮整体仍为失败：`resample-size` 的 Meta TIFF 元数据未满足 COG profile hint 断言，后续 grid 场景未完成，不计为完整 Raster T4 通过，也不宣称扫描范围的资源权限闭环完成。直接接口的同租户其他 User、跨租户、拒绝来源及安全投影矩阵由本轮 `make test-meta-postgres` 覆盖；包含实现的 `f2ae733d4` 的 [Platform CI Go workspace](https://github.com/pampa0629/addp/actions/runs/37189883711/job/111399672500) 已通过全部 22 个 Go 模块，但整次 Platform CI 因 GeoPython GDAL 测试失败，不计整体通过。

本轮本地验证分别记录：独立 `make test-meta-postgres` 与 Meta Swagger 路由覆盖通过；`make test-module MODULE=meta` 在共享平台 T0 的 Hosted 夹具超时及 Transfer 字段血缘 CI 登记回归失败处中止，后续模块 T1/T3 未运行。工作区 `make test-go` 被未提交 HDFS 插件的 `TestPluginSensitiveFields` 阻断；这与上述已提交版本的 22 模块 Go CI 结果不同，均不得混记为全工作区门禁通过。

Meta 扫描范围精确化门禁：直接 HTTP T1/T2 验证产物完整 locator 冻结到 execution config，非法／跨引擎目标返回本地化 400，失败请求不落记录；Go T1 验证配置 JSON 持久化、去重锁身份以及表、集合、图、直接叶子和 file/object 内容边界。`make test-meta-postgres` 增加 `TestPreciseCatalogScanAgainstPostgres`，复用同一运行时用例与事务回滚，验证局部扫描成功或失败均不改变祖先扫描事实、兄弟 item 保留，显式完整范围仍可清理缺失项。路径中的点号不得截断，空目标不能退回全引擎扫描。标准模块入口与 Release/T2 已自动覆盖 Meta Go 文件及此脚本，无第二套 CI 登记。

公共表格 Provider 的单表解析和事实读取统一使用必需的 `GetTable`，删除 `findTableInfo` 的父级枚举路径；PostgreSQL、MySQL、Oracle、ClickHouse、Doris、TiDB、OceanBase 同步实现目标名称谓词。T1 分别检查四类原生 SQL 的参数绑定、目标谓词、源目录过滤及错误直接传播，并检查解析不读取字段、缺失／不匹配目标不进入详情。Common PostgreSQL 门禁的 `TestIntegrationPostgresCatalogPreciseReadOnly` 使用真实只读源角色，验证单次目标查询、轻量摘要、主键详情、未授权／不存在目标 NotFound 及撤销 schema USAGE 后不可见。

Meta PostgreSQL 门禁登记 `TestPreciseNativePostgresScanAgainstPostgres`：使用真实 PostgreSQL Provider 和只读源角色扫描 A，显式禁止父级 `ListChildren`，同时核对 B 元数据及父级扫描事实保持不变；不可读 B、缺失目标、撤销源 schema USAGE 后的扫描必须失败。它属于源 Provider 与 Meta 持久化链路的 T2，不等于 HTTP／Worker 的 T4，也不证明其余厂商真实服务均已验收。System 当前资源授权、撤权后 Worker 重核及 metadata 可见性仍属于后续授权阶段，不把本期范围测试、此前 Monitor T4 或产物归属 T4 当作该权限闭环通过。

本期 Meta 工作区验证（2026-10-04）：修复首轮自身编译错误后，完整 `make test-module MODULE=meta` 返回 0，包含平台 T0、Meta 全量 Go T1、13 项前端测试与构建、既有 PostgreSQL 门禁及新增四类精确范围持久化回归；Swagger 生成与 Meta 43 个公开路由覆盖通过。共享 T0 的 Agent Swagger 投影因其他会话的 `deerflow` 依赖未就绪产生 `WARN_ONLY` 告警，不计为 Agent Swagger 验证通过。随后获准收紧公共 Provider 并补充真实源范围回归；后续 Common 与 Meta 门禁结果见本节下方，完整资源权限闭环仍待授权阶段完成。

公共 Provider 收敛后的工作区验证（2026-10-04）：`make test-go` 的全部 22 个 Go 模块通过；`make test-common-postgres`、独立 `make test-meta-postgres` 及完整 `make test-module MODULE=meta` 返回 0，后者包含平台 T0、Meta Go T1、13 项前端测试与构建和新增真实源贯通回归。`make test-changed` 因其他 Owner 的 PostgreSQL DSN、MySQL／OceanBase 测试环境缺失，在预检时中止，不计为通过。本地未运行其余厂商真实服务、其余消费者 T2 及真实 HTTP／Worker T4；已登记的 Platform CI Go workspace、Release/T2 各 Provider／Owner 门禁承接共享依赖扩散验证，T4 仍须相应 Hosted 验收。本轮没有重启开发服务。

# ADDP 持续集成体系改进专题

> 状态：阶段 3 已完成，阶段 4 进入环境准备。现状快照核实于 2026-08-21。本文记录当前 GitHub Actions 覆盖、主要缺口和后续实施路线。

## 一、专题定位

ADDP 已经具备较多本地测试、集成门禁和发布验证入口，但 GitHub Actions 只覆盖 IAM / CLI 与 Quality 的少数场景，尚未形成平台级持续集成体系。

本专题负责回答：

1. 哪些确定性门禁必须在每次 `main` 推送后执行。
2. 如何把根 `Makefile` 与 `scripts/test/` 中已有的唯一测试入口接入 CI。
3. 如何区分 PR 门禁、模块集成、Online 验收和发布认证。
4. 如何通过变更路径选择、并发取消和缓存控制执行成本。
5. 如何在不改变单人直接推送 `main` 的开发方式下及时反馈失败。

当前阶段采用“推送后验证”而不是“合并前阻断”：开发者继续直接推送 `main`，CI 失败后由 GitHub 通知并由开发者修复；任何自动部署都必须等待前置检查成功。若未来需要阻止问题代码进入 `main`，再单独讨论 Pull Request 和 required checks，不在本专题首期引入。

测试分层、数据隔离、Online Tenant、安全数据库和 T0-T5 定义以 [ADDP 统一测试与 Online 验收体系方案](ADDP统一测试与Online验收体系方案.md) 为准。本文不建立第二套测试分类，也不在 workflow YAML 中复制业务测试逻辑。

## 二、当前 CI 事实

当前仓库收敛为三个 GitHub Actions workflow。

| Workflow | 触发方式 | Job / 执行入口 | 当前定位 |
| --- | --- | --- | --- |
| `.github/workflows/release-and-t2-gates.yml` | 所有 PR；`main` push；`v*` Tag；手工触发 | 统一选择 System IAM、Quality、Standard PostgreSQL T2 门禁与 macOS CLI 产品门禁；`v*` Tag 只强制 CLI、System IAM 后发布 GitHub Release | T2 / T5 |
| `.github/workflows/quality-frontend-smoke.yml` | 所有 PR；`main` push；手工触发 | 按路径选择 `make test-quality-frontend`，执行 Quality 路由测试、Playwright E2E 和前端构建 | T3 |
| `.github/workflows/platform-ci.yml` | PR；`main` push；每日 02:30（北京时间）；手工触发 | `make test-platform`；`make test-go`；按路径选择 Common Python、Agent、Model 及前端确定性矩阵 | T0 / T1 / T3 |

当前共同特征：

- 三个 workflow 都设置了最小 `contents: read` 权限、超时和同 ref 并发取消。
- GitHub Action 引用固定到不可变 commit digest。
- PostgreSQL 门禁使用固定 PostgreSQL 15 镜像摘要和 disposable database。
- 三个 workflow 均不在 workflow 触发器上使用 `paths` / `paths-ignore`，以保持 Job 名称稳定；模块 Job 在内部按登记路径执行或明确报告跳过。
- 只有 CLI Tag 发布 Job 具有写权限，其余 Job 默认只读。
- 门禁超时按成本分级：选择与汇总 5 分钟，普通 T0/T1 20 分钟，浏览器或多语言门禁 25 分钟，数据库与 macOS 门禁 30 分钟，全仓 Go 45 分钟。
- 普通 Node 和 Python 确定性测试分别按 `package-lock.json`、Python 依赖声明缓存；disposable PostgreSQL 和生成发布产物的 CLI 验证关闭缓存，避免复用环境掩盖集成问题或引入缓存投毒风险。
- 核心门禁统一通过 `.github/actions/ci-gate-summary` 输出 Gate、Selected、Result 三列 Step Summary；CLI 验证 wheel 只保留 7 天。

## 三、当前依赖自动化

`.github/renovate.json` 已配置 Renovate，但 Renovate 不属于 CI 门禁。当前只管理：

- GitHub Actions action 版本与不可变摘要。
- PostgreSQL 15 Service 镜像摘要。

当前策略为每周一检查、至少等待发布七天、需要 Dependency Dashboard 人工批准、不自动合并，并使用 `renovate/` 分支前缀。Go、Python、npm 和 Dockerfile 依赖目前不在 Renovate 管理范围内。

Renovate App 是否启用、Dependency Dashboard 是否存在以及仓库侧权限是否完整属于 GitHub 外部状态，正式开展本专题时必须在线核实，不能只根据配置文件推断。

## 四、正式门禁入口

根 `Makefile` 已经存在以下正式入口：

| 入口 | 能力 | 测试层级 |
| --- | --- | --- |
| `make test-go` | 根据全部已跟踪 `go.mod` 生成临时 workspace，逐模块执行 `go test ./...`，不依赖本地被忽略的 `go.work` | T1 |
| `make test-authorization` | Permission Manifest、owner 常量、Tool Catalog、SQL seed 和 Swagger 路由覆盖报告 | T0 / T1 |
| `make test-execution-fixtures` | 统一 execution 测试夹具约束 | T0 |
| `make test-model-frontend` | Model 前端单元、E2E 和构建 | T1 / T3 |
| `make test-quality-frontend` | Quality 前端路由、E2E 和构建 | T1 / T3 |
| `make test-asset-frontend` | Asset 前端公开路由测试和构建 | T1 |
| `make test-console-frontend` | Console 前端确定性测试和构建 | T1 |
| `make test-develop-frontend` | Develop 前端确定性测试和构建 | T1 |
| `make test-graph-frontend` | Graph 前端确定性测试和构建 | T1 |
| `make test-inference-frontend` | Inference 前端确定性测试和构建 | T1 |
| `make test-manager-frontend` | Manager 前端全量确定性测试和构建 | T1 |
| `make test-meta-frontend` | Meta 前端确定性测试和构建 | T1 |
| `make test-monitor-frontend` | Monitor 前端确定性测试和构建 | T1 |
| `make test-orchestrator-frontend` | Orchestrator 前端确定性测试和构建 | T1 |
| `make test-portal-frontend` | Portal 前端确定性测试和构建 | T1 |
| `make test-service-frontend` | Service 前端全量确定性测试和构建 | T1 |
| `make test-standard-frontend` | Standard 前端确定性测试和构建 | T1 |
| `make test-system-frontend` | System 前端确定性测试和构建 | T1 |
| `make test-transfer-frontend` | Transfer 前端确定性测试和构建 | T1 |
| `make test-agent-eval` | Agent 离线评测门禁 | T1 |
| `make test-common-python` | common-python 全量测试 | T1 |
| `make test-module MODULE=<模块>` | 自动发现并串行运行指定模块的平台、语言、前端及已登记基础设施门禁 | T0-T3，开发交付入口 |
| `make test-integration` | 严格串行运行全部已登记的 disposable 基础设施门禁 | T2，本地或专用集成环境聚合入口 |
| `make test-standard-postgres` | Standard migration、删除约束与引用删除协调的 disposable PostgreSQL 15 门禁 | T2 |
| `make test-arcgis-open-formats` | Access / PGeo / Oracle Spatial 真实样本集成门禁 | T2 / T5，依赖专用环境 |
| `make test-agent-eval-release` | Agent 在线证据发布门禁 | T5 |

此外，`scripts/utils/check-deps-version.sh` 已改为：

- 从 `docs/spec/addp技术栈规约.md` 读取 Go 依赖唯一目标版本。
- 通过 Git 自动发现仓库内全部已跟踪和待提交的 `go.mod`，不依赖 Runner 额外安装 `rg`。
- 拒绝规约对同一 Go 模块声明多个目标版本。
- 校验所有模块中的直接和间接规约依赖声明。

该脚本由 `make test-platform` 调用并已接入 Platform CI，依赖版本漂移会在每次 `main` 推送和夜间任务中自动发现。

## 五、当前主要缺口

### 5.1 平台级 T0 门禁正在接入

当前任意模块都可能修改 `go.mod`、技术栈规约、Permission Manifest、Swagger 或执行夹具。本阶段通过 `make test-platform` 建立唯一的无外部服务平台入口，并在 `main` 推送后自动反馈；它不承诺在推送前阻止代码进入 `main`。

### 5.2 全仓 Go T1 门禁正在接入

`make test-go` 已经具备动态模块发现和临时 workspace 生成，并已接入 CI。它不读取或修改本地被忽略的 `go.work`；其他语言和前端模块仍待后续接入。

### 5.3 前端覆盖仍不完整

Common Python、Quality、Agent 和 Model 已通过统一脚本按各自模块、共享依赖、根 Makefile 和 workflow 自身的变更路径选择正式门禁；Asset、Console、Develop、Graph、Inference、Manager、Meta、Monitor、Orchestrator、Portal、Service、Standard、System 和 Transfer 复用同一个确定性前端矩阵定义，手工触发及平台夜间任务始终执行。当前所有具备前端测试的模块均已登记，后续新增前端模块或测试入口时必须同步纳入矩阵。

### 5.4 模块专项门禁结构仍待统一

Quality 前端、Agent 离线评测、Model 前端和 Common Python 使用 Job 内路径选择。`release-and-t2-gates.yml` 通过单一选择 Job 声明 CLI、System IAM、Quality 与 Standard PostgreSQL 的 path mapping；三个 PostgreSQL Job 统一使用固定 PostgreSQL 15、30 分钟超时、关闭 Go 缓存和相同 Summary 格式，未命中时不会启动数据库 Service。`v*` Tag 只强制执行 CLI 与 System IAM 门禁。Ruleset 要求的 CLI 和 System IAM 检查由汇总 Job 提供：命中路径时等待重测试成功，未命中时明确报告跳过并稳定成功。

`make test-platform` 已接入 T2 CI 登记完整性检查，自动发现 `scripts/test/*-postgres-gate.sh`，并校验 Make 入口、`test-integration` 串行聚合、workflow 调用、脚本路径、owner 后端路径与固定 PostgreSQL 15 镜像。新增 Hosted PostgreSQL T2 门禁时，遗漏任一登记环节都会使 Platform CI 失败。聚合入口面向具备全部安全连接条件的本地或专用集成环境；GitHub Actions 继续用独占 PostgreSQL Service 分 Job 执行模块级目标，以保留隔离、路径选择和并行反馈。

根 `make test` 已收敛为 T0-T1 全部无外部服务确定性门禁，聚合平台一致性、全部 Go 模块、Agent 离线评测及所有已登记前端的测试与生产构建；需要 disposable PostgreSQL、真实运行服务、在线证据或发布环境的 T2-T5 门禁保持显式独立入口。前端登记检查会自动要求每个新前端同时进入 CI 矩阵和根 `make test`，避免本地总门禁与 CI 覆盖漂移。重复的 System 单模块 Go 测试入口已删除，由动态发现全部 `go.mod` 的 `make test-go` 唯一覆盖。

开发交付使用 `make test-module MODULE=<模块>` 运行指定模块的 T0-T3。该入口根据 Git 跟踪文件自动发现 Go、Python、前端和 PostgreSQL 门禁，先执行平台一致性，再串行调用 owner 事实入口；不维护模块清单，也不复制 CI 路径映射。模块存在 PostgreSQL T2 时必须显式提供 owner 要求的安全连接变量，缺失即失败，不能用跳过伪装通过。

平台构建入口也已收敛为单一路线：仓库只维护根 `Makefile`，`make build` 唯一调用 `scripts/build/compile.sh`，`make build-images` 唯一调用 `scripts/build/build-images.sh`，旧的模块 Makefile、分模块、debug/release、生产镜像和 Compose build Make 目标已删除。开发、基础设施和生产生命周期目标分别只封装 `scripts/dev/`、`scripts/infra/` 和 `scripts/prod/` 的标准脚本，直接 `go run`、直接 Compose 生命周期目标及兼容别名已删除。只覆盖 System 却宣称全平台的依赖、lint、格式化目标，会吞掉错误的伪检查，只登记部分旧前端的 Docker 修复脚本，未校验删除范围的清理目标，以及硬编码凭据或缺少完整恢复契约的初始化、数据库终端和备份恢复目标也已删除。Registry 固定使用构建链路的 `localhost:5001` 临时仓库，不再保留端口、容器名和持久化语义冲突的第二套初始化路线。`make test-platform` 自动发现正式 Go Server/Worker、前端与 Compose ADDP 镜像，要求所有 Git 跟踪 Dockerfile 归属于正式镜像或明确的辅助构建，并核对构建登记、Dockerfile/专用构建脚本、Docker build context 的 `COPY` 输入、`.dockerignore` 排除规则、本地 Registry 基础镜像预热登记、浮动 `latest` 标签、预编译二进制名称以及根 `Makefile` 引用脚本是否真实且被 Git 跟踪；以后 AI 新增或调整部署单元时，恢复退休 Make 目标、重复模块 Makefile、引用失效脚本或遗漏构建登记会在同次 CI 中直接暴露。

| T2 门禁 | Owner | Service | 数据库安全检查 | Path mapping |
| --- | --- | --- | --- | --- |
| System IAM PostgreSQL | System | PostgreSQL 15 disposable database | DSN 必须由 CI 注入；门禁先重置专用数据库并拒绝任何测试跳过 | `system/backend/*`、`common/*`、System IAM 门禁脚本与统一 workflow |
| Quality PostgreSQL | Quality | PostgreSQL 15 disposable database | 数据库名必须包含 `test` 或 `disposable`，并拒绝任何测试跳过 | `quality/backend/*`、`common/*`、Quality 门禁脚本与统一 workflow |
| Standard PostgreSQL | Standard | PostgreSQL 15 disposable database | DSN 数据库名必须包含 `test` 或 `disposable`，并拒绝任何测试跳过 | `standard/backend/*`、`common/*`、Standard 门禁脚本与统一 workflow |

### 5.5 CI 与 GitHub 仓库策略没有仓库内闭环

2026-08-21 通过 GitHub API 核实 Ruleset `main release gates`：`main` 要求通过 PR，并要求 `CLI product gate (macOS Keychain)`、`System IAM gate (PostgreSQL 15)` 两个检查；当前维护者账号具有永久绕过权限，因此单人开发仍可直接推送。以下状态仍未核实：

- Actions 是否允许 fork PR、是否需要人工批准。
- macOS Runner 与浏览器门禁的实际耗时和月度成本。
- CI 失败通知、Artifact 保留和历史趋势是否满足需要。

这些外部状态必须在专题实施前读取 GitHub 当前配置，并在实施记录中留下核实日期。

## 六、目标边界

### 6.1 Workflow 只负责编排

业务测试和安全检查继续由根 `Makefile`、`scripts/test/` 与模块测试代码拥有。Workflow 只负责：

- 触发条件与变更路径选择。
- Runner、语言版本和 disposable Service 准备。
- 调用唯一 Make / script 入口。
- 超时、并发、Artifact 和 required check 名称。

不得在 YAML 中复制 SQL、测试选择表达式、业务夹具或模块启动逻辑。

### 6.2 平台门禁与模块门禁分开

- 平台 T0 门禁对每次 `main` 推送、定时任务和手工任务执行，不按模块路径跳过。
- Go T1、前端 T1 / T3、模块 T2 根据受影响路径选择。
- T4 Online 验收只允许手工或夜间执行，不接管开发环境。
- T5 发布认证由产品或 Runtime 独立编排，只在发布条件下执行。

### 6.3 只保留一条正式路线

新增统一 workflow 或入口时，必须同步迁移、合并或删除被替代的旧 workflow。不得长期保留“旧专项 workflow + 新矩阵 workflow”两套重复门禁。

## 七、目标 CI 结构

目标结构应按职责收敛为四类，而不是按每个模块无限新增 workflow：

| 类别 | 默认触发 | 内容 |
| --- | --- | --- |
| 平台一致性门禁 | `main` push、每日定时、手工 | 依赖版本、变更空白错误、execution fixture、Permission / Swagger 等 T0 检查 |
| 确定性测试矩阵 | `main` push、每日定时、手工 | Go workspace T1、Python T1、按模块选择的前端 T1 / T3 |
| 模块集成矩阵 | 相关路径 PR、`main` push、手工 | PostgreSQL、Redis、MinIO 等 disposable T2 门禁 |
| Online / Release | 夜间、手工或 Tag | T4 专用测试部署验收；T5 产品发布认证 |

具体 workflow 文件名、Job 拆分和矩阵生成方式在实施阶段确认；确认后只能保留一套正式结构。

## 八、分阶段实施路线

### 阶段 0：现状记录

- [x] 盘点现有三个 GitHub Actions workflow。
- [x] 盘点 Renovate 当前范围。
- [x] 识别根 Makefile 中尚未接入 CI 的正式测试入口。
- [x] 记录依赖版本检查脚本尚未接入 CI。

### 阶段 1：平台一致性门禁

- [x] 新增唯一平台 T0 workflow。
- [x] 通过根 `make test-platform` 接入 `bash scripts/utils/check-deps-version.sh`。
- [x] 对 PR base 与 HEAD 的提交差异执行 `git diff --check <base>...HEAD`；不在干净 checkout 上运行无范围的 `git diff --check` 冒充有效检查。
- [x] 通过 `make test-platform` 接入 `make test-execution-fixtures`。
- [x] 首期完整调用现有 `make test-authorization`，不在 workflow 中拆分或复制其内部命令；根据真实耗时再决定是否重构 owner 入口。
- [x] 保持 workflow 默认只读权限；当前 Ruleset 存在两个 required checks，维护者以绕过方式直接推送 `main` 后接收失败反馈。

### 阶段 2：全仓确定性测试

- [x] 接入 `make test-go`，保持动态模块发现，不维护手写模块列表。
- [x] 接入 `make test-common-python` 和 Agent 离线评测。
- [x] 建立首个前端模块变更路径登记并迁入已有 `make test-model-frontend`；未命中时保留稳定 Job 并明确报告跳过原因。
- [x] 复用统一选择脚本，将 Agent 离线评测和 Quality 前端纳入变更路径登记，并保持手工及夜间触发始终执行。
- [x] 将 Common Python 纳入变更路径登记；`common-python/` 变化仍会同时触发依赖它的 Agent 离线评测。
- [x] 建立前端确定性矩阵并首批接入 Meta、Portal 的测试和构建入口。
- [x] 将 Monitor、Transfer 的测试和构建入口接入前端确定性矩阵。
- [x] 将 Graph、System 的 Vitest 和构建入口接入前端确定性矩阵。
- [x] 将 Develop、Orchestrator 的确定性测试和构建入口接入前端确定性矩阵；浏览器测试仍归 T3，不混入快速门禁。
- [x] 将 Console、Standard 的确定性测试和构建入口接入前端确定性矩阵；浏览器测试仍归 T3，不混入快速门禁。
- [x] 将 Inference 的单元测试和构建入口接入前端确定性矩阵，完成现有标准 `npm test` 前端模块的登记。
- [x] 将 Manager 的 map、explorer、navigation 测试收敛为唯一 `npm test`，并将全量测试和构建接入前端确定性矩阵。
- [x] 为 Asset 建立公开路由和 canonical query 的首批确定性测试，并将测试和构建接入前端确定性矩阵。
- [x] 将 Service 的查询、引擎、样例和瓦片预览测试收敛为唯一 `npm test`，并将全量测试和构建接入前端确定性矩阵。
- [x] 统一缓存、分级超时和 Step Summary 报告格式；数据库与 CLI 发布产物验证禁用缓存，CLI wheel 保留 7 天。
- [x] 核实当前 required checks：CLI 产品门禁和 System IAM PostgreSQL 门禁；使用汇总 Job 保持路径跳过与真实验证结果都能稳定回报。

### 阶段 3：模块集成矩阵

- [x] 将 System IAM、Quality、Standard PostgreSQL 纳入统一的 T2 结构。
- [x] 为 T2 建立模块 owner、PostgreSQL 15 Service、数据库安全检查和 path mapping。
- [x] 迁移完成后删除被统一结构替代的 `quality-postgres-gate.yml`。
- [x] 保证未命中相关路径时由轻量选择 Job 输出原因，重 Job 明确显示为 skipped；发布 Tag 强制执行所需门禁。

### 阶段 4：Online 与发布门禁

- [x] 核实仓库当前没有自托管 Runner、GitHub Environment 和 Actions Secret，现阶段不能安全启用 T4 workflow。
- [x] 删除伪 Online 的 Standard ↔ Model 混合测试及其默认 Tenant 1、旧开关、跨 Schema SQL 和忽略清理错误路径；现有协调算法与双方数据库行为分别由 T1/T2 证明。
- [x] 建立 Online 通用安全预检器及确定性自测，拒绝非回环地址、默认 Tenant、脏工作区、无效 Run ID 和服务构建身份不匹配。
- [x] 建立唯一 Online suite 分发入口、显式登记、统一 Run ID 和总超时；未实现的场景不登记。
- [x] 通过 Gateway、owner API 和专用身份建立 Standard Domain ↔ Model Entity T4 场景，不复用已删除的跨 Schema 夹具。
- [ ] 准备独立于开发环境的测试部署、显式测试 Tenant、专用测试身份和构建身份校验。
- [ ] 注册带 `self-hosted`、`macOS`、`ARM64`、`addp-online` 标签的专用 Runner，并绑定 `addp-online-test` Environment；Runner 不复用开发服务进程或开发数据库。
- [ ] 按统一测试方案建立手工 / 夜间 T4 workflow；环境准入未完成前不得提交一个会永久排队或连接开发环境的占位 workflow。
- [ ] 保留 CLI 等产品级 T5 门禁，但统一调用、报告和 Artifact 规则。
- [ ] 核实 Tag、GitHub Release、Artifact Attestation 与仓库 Ruleset 的闭环。

阶段 4 只采用一条路线：GitHub Hosted Runner 继续承担 T0-T3 与现有 CLI T5；macOS 自托管 Runner 只承担需要访问专用 ADDP 测试部署的 T4。个人日常开发环境不注册为 Online 测试目标，`restart.sh` 也不触发 CI 或 Online 测试。

T4 环境达到以下条件后才能接入 workflow：

1. macOS 上使用独立 Runner 账号和独立 checkout，不能在日常开发工作区执行任务。
2. 测试部署拥有独立数据库、Redis、对象存储和测试 Tenant；数据库名称必须明确包含 `test` 或 `online`，不得连接 `addp` 开发业务库。所有被测服务只绑定 Runner 可访问的回环地址，通用预检拒绝外部主机。
3. System、Gateway、Standard、Model 等参与服务的 `/health` 必须报告本次提交的 Git commit，任一服务身份不匹配立即按环境失败退出。
4. GitHub Environment 只保存专用测试身份所需 Secret；不得复用个人账号、开发数据库密码或生产凭据。
5. 同一 Environment 最多允许一个 T4 运行；所有资源带唯一 Run ID，失败、中断和超时都必须清理并验证零残留。
6. Runner 至少应具备 Apple Silicon、16 GB 内存和 100 GB 可用空间；若同机承载完整测试部署，建议 32 GB 内存。Docker 运行时、Go、Node、Python 等具体版本继续由仓库规约和 workflow 准备，不依赖机器上的临时开发配置。

### 阶段 5：规范化与清理

- [ ] 更新 `docs/spec/` 中稳定下来的 CI 规则。
- [ ] 更新根 README、开发步骤、模块验证说明和 GitHub required checks 清单。
- [ ] 删除迁移期 workflow、兼容入口和重复说明。
- [ ] 本专题只保留实施历史和尚未完成事项；全部完成后归档或删除。

## 九、阶段 1 验收标准

首期平台一致性门禁完成时至少满足：

1. 任意 PR 修改技术栈规约或任一 `go.mod` 后都会执行依赖一致性检查。
2. 规约版本与模块声明不一致时 Job 必须失败，并明确输出依赖、期望版本、实际版本和模块路径。
3. 新增 Go 模块无需修改 workflow 或检查脚本即可被发现。
4. 同一依赖在规约中声明多个目标版本时 Job 必须失败。
5. PR 提交范围中的空白错误能被真实检测，不能因干净 checkout 而永远通过。
6. Job 只需要仓库只读权限，不使用 Secret，不连接开发或生产服务。
7. Job 名称稳定；当前不配置 required check，失败通过 GitHub Actions 通知开发者修复。
8. 本地命令与 CI 调用完全一致，失败可以在本地复现。

## 十、实施前必须核实的外部状态

开始编码前，先通过 GitHub 当前配置核实：

1. Repository Ruleset / Branch Protection 现状；当前不依赖 required checks。
2. GitHub Actions 默认权限、fork PR 策略和手工批准策略。
3. 最近至少 30 次 workflow 的耗时、失败原因和 Runner 成本。
4. Renovate App 安装状态、Dependency Dashboard 和实际更新记录。
5. 是否需要自托管 Runner；没有明确资源或安全价值时，默认继续使用 GitHub Hosted Runner。

2026-08-21 在线核实结果：仓库自托管 Runner、Environment 和 Actions Secret 数量均为 0；Actions 已启用，默认 workflow 权限为 `contents: read`，workflow 不能审批 Pull Request。阶段 4 必须先完成上述环境准入，不能从仓库现状推断测试机器已经可用。

## 十一、建议的首次开展范围

首次实施开展“阶段 1：平台一致性门禁”，并同时接入已有的全仓 Go 确定性测试；不重构现有 IAM、Quality 和 Release workflow。

阶段 1 稳定后，再根据真实耗时与失败数据设计 Python、前端和模块集成矩阵。首期使用 GitHub Hosted Runner，不接入个人 macOS 机器；该机器仅作为未来 macOS Keychain、CLI 生命周期等必须依赖真实 macOS 能力的候选 self-hosted Runner。

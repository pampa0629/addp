# ADDP CI 与交付规范

[返回测试与验收总入口](../addp测试与验收规范.md)

## CI 编排与登记

按路径选择门禁的托管工作流（Platform CI、Release/T2、Quality Smoke）使用独立 Runner 和 Job 独占的 disposable 资源，不设置 workflow 或 job 的 `concurrency` 分组。每次运行保留自己的提交范围与选择结果，由 GitHub Runner 配额排队；后续无关提交不能取消正在执行的门禁，也不能替换待执行门禁。仅将 `cancel-in-progress` 改为 `false` 不足以满足此要求：GitHub 默认仍以新运行替换同组已有的 pending 运行。专用 T4 主机的资源互斥遵循其既有 Job 锁，不套用托管门禁的调度规则。

Push 使用事件的 `before...sha`，PR 使用 base 与本次 SHA 的 merge-base 差异；共享依赖和 owner 声明的测试输入继续由 `changed-gate.py` 统一扩散。文档运行跳过测试只表示该提交未命中路径，不能替代前序代码提交的验收证据。`make test-platform` 的 CI 登记与选择回归约束上述调度，并验证连续相关提交后追加文档、共享依赖、取消/失败/意外跳过的判定；原有超时、owner 清理、零残留核验及 required check 汇总必须保留。

验证调度修复时，应在相关门禁执行和排队期间追加无关文档提交，再核对前序 Job 继续完成、排队 Job 实际启动，以及文档运行只跳过自身未命中的门禁。

System 与 Transfer 的 Go T1 分别使用 Platform CI 的独立 `System Go CI runner`、`Transfer Go CI runner` Job，按各自 owner／Common 及标准模块入口依赖路径选择，直接复用 `make test-module MODULE=system GO_ONLY=1`、`make test-module MODULE=transfer GO_ONLY=1`。两者沿用 Ubuntu 24.04 和 20 分钟预算，由各自 backend/go.mod 确定 Go 版本；因根 Make 入口仍校验 Node 版本，按 `.node-version` 准备固定版本并登记其输入，不绕过前提检查。独立 Job 不依赖 Go workspace 聚合 Job，不启用外部数据库、个人服务或 Hosted 验收；仅证明各自 Go T1，不计为完整模块门禁。聚合 workspace 继续承担全模块依赖及组合测试，其失败保持失败；不能把它在其它 owner 提前退出解释为 System 或 Transfer 已经执行或通过。独立 Job 须同时核对 selected、标准命令和实际终态。

System Cleanup PostgreSQL 的公共DTO回归实际调用Hosted的唯一任务观察器。该T2入口通过 `ADDP_T2_INPUT_FILES` 显式登记观察器、共享支持和机器契约等运行时脚本依赖，使脚本变更也命中System门禁；函数改名同步全部真实消费者，不恢复旧别名。

产品镜像构建继续由 `make build-images` 唯一入口负责。构建调度最多同时执行两个任务，基础镜像预热先于任务启动；单路和两路使用同一实现。前端 Node 版本来自根 `.node-version`，仅使用锁文件安装依赖。`make test-platform` 校验这些契约，并验证并发上限、失败汇总、日志隔离及中断时的进程和临时目录清理。现有 Platform CI 产品构建 Job 复用该入口验证实际镜像；Node 版本变更必须通过影响选择命中全部前端镜像，不缩减受影响服务范围。

发布工作流安全审计统一由 `make test-workflow-security` 执行，并纳入本地 `test-platform` 与 CI 现有 System IAM required Job。唯一脚本固定 zizmor 1.28.0，只扫描 `.github/workflows/release-and-t2-gates.yml`，使用 auditor、medium 最低严重级别、关闭在线审计，不读取自定义配置或忽略标记；安装、扫描或输入收集失败必须阻断。首次运行需联网取得固定版本的二进制 wheel，使用临时虚拟环境且退出时清理；不依赖 ADDP 服务或数据库。

Manager Hosted 的 Infra MinIO 构建准备复用固定输入缓存：继续使用原 Dockerfile 与 Infra Compose 构建最终镜像，缓存同时保存带 BuildKit inline 元数据的编译阶段和最终镜像，不改变 Server / mc 源码修订或验收断言。缓存键包含 Linux amd64、构建脚本及全部已跟踪 Infra 构建上下文；只恢复精确键，加载前核对归档摘要，加载后核对镜像身份和架构。热构建必须复用 Dockerfile RUN，并保持两个二进制摘要一致；缓存损坏、缺失的必需热验或构建失败均阻断。独立 Infra MinIO build cache 工作流在全新 Runner 核验复用，原样 Compose 构建仍实际执行。转移归档在产品部署前删除以释放磁盘，仅保存公开固定源码构建层，不保存身份、配置、业务数据或测试结果；构建缓存通过不代表业务 T4 通过。

Platform CI 的 `frontend-deterministic` 默认每个 Job 最长20分钟。Monitor 的完整单测、浏览器回归及构建沿唯一 `make test-monitor-frontend`，保持单Worker；两轮原始check-run注释明确20分钟超时，第二轮至423/424仍未出完整报告，此前因此仅 Monitor 获准使用 `timeout_minutes: 30`。Console 沿唯一 `make test-console-frontend`，按用户确认保留20分钟预算，浏览器采用两个Worker按文件并行，`fullyParallel: false` 保持同文件用例顺序；不删减单测、浏览器回归或构建，不改变单用例时限或重试。隔离前提是测试独立的浏览器上下文和page/context级HTTP夹具，Vite只服务源码，运行端口、缓存和证据由共享标准入口隔离与清理。Job统一读取矩阵预算、缺省20，登记检查拒绝扩大Console或其他模块预算，以及删除或扩大Monitor例外。预算包含安装、测试、构建和退出。2026-11-02 的精确0911裁剪来源中，Monitor 133单测和602浏览器通过后，构建在30分钟Job上限被取消；本次将Monitor唯一例外固定为40分钟。GeoPython 的109项NetCDF子集通过，但全量pytest在45分钟Job上限仍无终态，完整Job固定为60分钟。保持原标准入口、Monitor单Worker、所有测试与断言、单用例期限及重试策略；旧取消仍未通过，须以后继自然运行实际完整结果验收。Console 先前 `40254270886`、`40254922673`、`40255039253` 的20分钟超时仍保留为未通过；门禁级隔离或本地并行通过不能替代Linux完整门禁实际通过，文档提交的选择跳过也不能补齐前序缺口。

执行管理确定性专项在 Platform CI 中使用独立的 `Execution online CI runner` Job，复用唯一 `make test-execution-online-ci-runner`，不依赖 Platform 聚合 Job 或复制语言测试列表。该 Job 只做现有 Graph／Cleanup 编排、准入、报告、恢复和登记的 T0/T1，不启动产品部署、数据库或 T4；输入闭包由登记检查约束，并复用标准路径选择的 push／PR 差异与 schedule／manual 语义。跳过必须明确 `selected:false`，不替代前序代码的实际执行证据；专项成功也不替代原聚合及 review 的失败结果。原聚合入口与全部断言保留；`Platform consistency` Job 的总预算固定为 60 分钟，包含冷 Swagger 准备、完整 `make test-platform` 和退出清理。除下述 GeoPython 测试进程调度外，各专项 Job、单项测试期限、串行执行与重试策略保持不变，未引入第二套实现或容错放行。

**2026-11-04 完整门禁耗时优化。** 两次固定提交的 Platform consistency 与 GeoPython GDAL tests 均触及既有 60 分钟上限；预算、全部唯一测试及断言、单用例时限与重试语义保持不变。在 Online 聚合依赖图中，公共分发器、变更选择、栅格血缘观察和登记测试分别由唯一 Make 目标拥有，owner 入口通过依赖复用；Online 聚合以 Make 依赖图串行执行，同一 Make 进程内共享目标只执行一次，单独执行 owner 仍完整检查。不得使用跨运行测试结果缓存、环境分支跳过或重复实现换取通过。登记校验必须沿真实依赖闭包验证 owner 的公共回归可达，不能因命令移入依赖而删除检查。 `.PHONY`、`.NOTPARALLEL` 等 Make 特殊目标只声明属性，不构成 owner 执行依赖；负例必须实际移除普通目标的依赖边，并先确认基线有效且输入确已改变。 原先先行检查 JavaScript 语法的 owner 必须保留该顺序：语法预检作为首个依赖串行完成后，才运行共享回归及其他 owner 依赖；不因去重而改变失败阶段。

GeoPython 沿唯一 `make test-geopython-workflow` 使用固定四个隔离 pytest Worker 和 worksteal 调度，利用现有公开仓库 `ubuntu-24.04` 标准 Runner 的四个 CPU（[GitHub Runner 规格](https://docs.github.com/en/actions/reference/runners/github-hosted-runners)），不提高 Runner 规格或 Job 预算，完整 pytest 阶段保持完整收集集合，每个用例实际执行一次；既有 NetCDF 前置子集仍独立执行，pytest-xdist 仅作为测试依赖。Worker 使用各自临时目录，网络夹具须使用动态端口，不共享可变文件或产品服务；产品 Gunicorn 的单 Worker 约束不变。`--max-worker-restart=0` 禁止崩溃后自动重启，任何失败或不完整执行均阻断；不增加重试或放宽资源、像元、耗时及清理断言。序列收集与并发执行的用例集合必须一致，本地优化与 Linux 完整 Job 的通过证据分别记录。

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

### 辅助 macOS 定时巡检

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

## 交付与完成标准

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

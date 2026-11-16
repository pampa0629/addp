# ADDP T4 Online 验收规范

[返回测试与验收总入口](../addp测试与验收规范.md)

部署 profile、Runner 与工具链版本以 `.github/workflows/online-t4-gates.yml`、Online registry 和当前 Host Gate 为准；Hosted 与 self-hosted 不能共用准备或清理假设。以下按场景归并已有行为契约，每项条款仅适用于其声明的 suite 与 profile；代码就绪不表示真实 T4 已通过。

## 专用环境

T4 只在隔离的 ADDP 测试部署执行，并按运行条件选择唯一部署 profile：

- 常规 Online suite 使用带 `self-hosted`、`macOS`、`addp-online` 标签的专用 Runner 和 `addp-online` GitHub Environment，复用专用部署中的稳定 Tenant、User 和 Engine Instance。
- 需要商业 License 的 Linux 引擎使用带 `self-hosted`、`Linux`、产品声明的 `X64` 或 `ARM64` 架构标签和 owner 标签的专用 Runner，通过受保护 GitHub Environment 审批。当前 KingbaseES 正式门禁仍固定 `X64`；DM8 ARM64 若进入正式门禁必须固定 `ARM64` 且匹配厂商认证环境。License 和受控介质不进入 checkout、日志或 Artifact，也不回退到 Hosted 或 macOS profile。
- DM8 在正式 ARM64 主机到位前允许本机 macOS ARM64 Docker Desktop 执行试用期技术 T2/T4：数据库以及所有真正加载官方 Go 驱动的测试/模块进程必须运行于 `linux/arm64` 容器，System 可保持 macOS 开发模式仅承担 Engine Instance 控制面登记。该 profile 是非认证 ABI、非长期且不可调度的本地证据，不登记 GitHub Hosted 或 schedule；试用到期、Docker 架构不符、驱动摘要不符或官方介质不可得时必须失败，不能 Skip 或切换镜像/驱动。
- SGM 允许通过 `make test-manager-sgm-local` 人工执行本机隔离技术页面验收，限定 macOS/Linux ARM64 与本地 Linux ARM64 Docker。入口从当前已提交源码建立仓库外临时副本，复用正式 Compose 服务定义和产品构建入口，使用本轮独占平台、双 Runtime、PostgreSQL、Redis、Infra/Business MinIO 与普通测试 User；不连接或停止个人开发服务。容器、网络、卷、凭据及入口端口必须独立，退出逐项核验清理。Runtime 和对象存储只使用内部数据网络，Nginx、System、Gateway 与 PostgreSQL 另接本轮独立入口网络，宿主机端口仅绑定回环；正式构建前须通过实际端口映射及 HTTP 准入检查。SDK、许可和厂商样例只在私有本机存在；报告仅保留哈希与断言摘要，不归档原始日志、截图或样例。System 身份夹具仅允许该本机标记用于既有 Manager 十项权限夹具，且与 GitHub Hosted/owner-managed 标记互斥；它不授予个人或生产账号权限。该入口验证任务生成、GLB 页面加载、缓存复用、原文件保持及领域/物理清理，不计为正式 Linux Runner T4、不进入 GitHub Actions 或 schedule，也不自动推广为其他商业引擎的部署例外。确定性生命周期与准入回归纳入既有 `make test-manager-online-runner`，平台登记检查必须拒绝公共 workflow 调用真实私有入口。正式商业许可 T4 继续遵循上一条 Linux Runner 要求。
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

三种 profile 共用同一个 `make test-online` 分发器和 owner 业务断言，生命周期脚本只负责当次环境和夹具。编排只调用现有 Infra、开发生命周期脚本和 `make test-online`，不得在 workflow 中复制模块启动逻辑或业务断言。

Hosted Manager 场景中，经开发入口启动的 PointCloud、Document 使用原生 Python 进程，监听配置的回环端口，按 PID 与监听归属管理；通过宿主机回环访问 System、Infra 与 Business MinIO，不进行 Docker host gateway 改写。启动和进程边界见[显式启动 Runtime](../../../scripts/docs/启动脚本使用指南.md#显式启动-runtime)，访问地址与宿主发布绑定的区别见[端口规范](../addp端口分配.md#system-addp-基础设施)。启动约束的确定性检查仍走 `make test-dev-lifecycle`；原生依赖准备或生命周期检查不替代真实对象读取、COPC／PDF 产物及页面验收，具体断言见[Manager 点云与 PPTX 场景](scenarios/Manager与空间成果.md#点云与-pptx)。

产品容器部署仍按对应产品和 suite／profile 契约执行，不能将上述原生进程说明推广为容器统一使用宿主网络。容器访问仅发布回环端口的服务时，仍不得用 bridge 的 host gateway 代替实际可达路径。

退出路径必须停止本次应用进程并报告清理结果；常规 macOS self-hosted Infra 可在专用 Runner 常驻，Hosted 与 License 受控 owner-managed profile 的 Infra 必须随 Job 销毁。

各 suite 的部署选择、专用夹具和业务断言见[场景索引](../addp测试与验收规范.md#online-场景索引)；专用条款只适用于其声明的 suite 与 profile。

## 正式身份的本地 Trace 技术验收

2026-10-23 用户确认 `make test-monitor-traces-local`：在本机独占一次性容器部署验证正式密码／MFA、Orchestrator → Develop bounded query、Platform Trace 查询和精确日志关联。它是人工本地技术证据，不属于上述 GitHub profile，也不允许伪造 Hosted 环境；不会进入工作流、夜间调度或替代正式 Runner T4。身份、资源、停止线和清理的唯一契约见[运行观测规范](../addp平台运行观测规范.md#正式身份的本机隔离闭环)。System 专用夹具仅放行该明确本地标记与 `execution-trace-local`，不推广 SGM 例外，也不扩大生产权限。

本地 Trace 直接新建的辅助容器（包括版本核对、构建探针与容量采样）必须显式使用本轮 Compose project 和辅助 service 标签，覆盖本地镜像可能携带的个人部署标签；不得被个人 Infra 的部署发现识别为正式引擎。Compose 业务容器仍由本轮 Compose 项目管理，现有资源归属拒绝与退出清理规则不变。

## Catalog 本地隔离浏览器验收

`make test-catalog-browser-local` 是本机一次性 Catalog 浏览器验收入口，不属于 GitHub T4 profile，不调用 `make test-online`，不重跑已通过的 API T4。使用已提交源码、正式产品构建与 Compose 服务定义，独占 System、Gateway、Catalog、Standard、Model、Console 及必需基础设施。具体账号和页面断言见[授权与共享](scenarios/授权与共享.md#catalog-本地普通用户浏览器复验)。

- 本机 Docker、每轮唯一运行 ID、容器／网络／数据卷和回环端口；独立 PostgreSQL 容器仅使用 `addp_test`，不连接共享 `addp-postgres`。配置只从模板生成，拒绝外部数据库连接和 GitHub／其他验收 profile，不加载日常 `.env`。只读记录日常容器身份，验收后核对未修改。
- 浏览器入口使用每轮唯一 `*.localhost` 主机名与精确允许的 Origin，隔离 Cookie、Storage 和跨标签页通信；不以不同端口冒充 Cookie 隔离，不修改系统 hosts 或日常浏览器登录状态。
- System-owned `catalog-browser-local` 夹具在任何身份写入前核验数据库名、运行 ID 对应的 PostgreSQL `cluster_name`、私密输出目录及空 User 状态。通过正式 IAM 服务创建租户、准备身份和限时普通用户，不写 IAM 表、不直接签发浏览器 Token。业务样本通过 Owner API 创建，准备身份只供样本初始化并在页面就绪前回收。普通用户通过正常密码登录和 AuthContext 证明身份，禁止 Mock API、注入 Token 或复用个人管理员会话。
- 主入口负责准备、就绪等待和清理；默认页面租约 60 分钟，最长 120 分钟。独立看护进程处理父进程退出和总时限；正常结束、失败、超时及信号均清理。`make clean-catalog-browser-local CATALOG_BROWSER_RUN_ID=<id>` 只恢复清理该运行 ID 的资源。宿主机／Docker 故障导致未清理时必须报告失败，恢复后使用相同入口清理，不能宣称零残留。
- 退出销毁本轮数据库及已发布历史、服务、网络、数据卷、镜像标签、凭据与临时副本，核对逐项零残留。不得全局 prune、重启或停止个人开发服务。
- `make test-catalog-browser-local-runner` 验证准入、权限、生命周期、清理和 CI 登记，加入现有 Catalog runner；System 夹具测试由 System 标准测试发现。真实 Opera 验收不加入 GitHub workflow／schedule。报告保留提交、身份权限摘要、样本关系、页面断言和清理结果，不归档凭据、Token 或原始认证响应。

页面完成后，使用已有 `make finish-catalog-browser-local CATALOG_BROWSER_RUN_ID=<id> CATALOG_BROWSER_EVIDENCE=<证据文件绝对路径>` 提交真实观察。证据必须匹配本轮 `run_id`、`commit` 和 Opera，且包含[标准入口要求的全部页面检查](../../../scripts/test/catalog-browser-local-gate.py)，每项均为布尔 `true`；缺项、错版本、错运行或其他浏览器不得计通过。

结束命令成功仅表示证据已获接收。主入口随后复核源标准摘要不变并执行清理，以主入口退出和最终报告判定结果；就绪回执或结束命令返回成功均不能单独证明页面和生命周期全部通过。该报告属于本机技术证据，不升级为 Hosted API T4 或全量跨域关系验收。

## 开关、身份与拓扑预检

每次 T4 运行至少满足：

- `ADDP_ONLINE_TEST=1`。
- 显式非默认 `ADDP_ONLINE_TEST_TENANT_ID`，禁止 Tenant 1。
- 全局唯一的 `ADDP_ONLINE_TEST_RUN_ID`；未提供时由分发器生成并贯穿全部子测试。
- 每个参与 HTTP 服务 `/health/live` 可用，且 Build ID、Git commit 或源码指纹与当前 checkout 匹配；进入业务断言前 `/health/ready` 必须返回 200。
- 除[平台节点指标控制面隔离验收的限定例外](scenarios/平台观测与语义生命周期.md#platform-node-metrics)外，测试 User、Service Principal 和 OAuth Scope 只具备场景所需的最小权限；不得使用个人会话或扩大生产身份权限。平台管理员 Token 默认禁止，仅在该限定例外及 `platform-administrator-governance`、`module-registry-recovery` 的一次性 Hosted 三员身份中允许。
- 通过 System AuthContext API 证明 principal、context、Tenant、client、token 与 Permission 事实后，才进入资源创建或注册拓扑操作。
- 同一 `suite + Run ID` 通过进程锁串行化，锁覆盖预检、业务断言和报告落盘全过程。

每个断言只验证规范定义的唯一身份链路，不在 User Token、Service Token 和直接 SQL 之间兼容回退。Repository / Migration 的直接数据库夹具属于 T2，不能替代 T4 API 授权验收。

PostgreSQL 源数据授权、模块与引擎恢复、平台节点指标等专用身份和拓扑断言，按[对应场景](../addp测试与验收规范.md#online-场景索引)执行；其中的平台管理员限定例外不扩展为其他 suite 的权限。

## 数据、超时与清理

T4 临时夹具优先通过 owner 正式 API 创建；正式 API 无法建立必要前置状态时，才允许 owner 提供专用测试 helper。Hosted profile 的全新平台库在尚无可登录 User 时，可由 System-owned helper 调用正式 IAM Service 创建当次 Tenant、User、Role 和 Session；helper 必须限定 GitHub Hosted Linux 及 `addp_online`，不得通过 SQL 写入 Principal、Role、Assignment 或 Token。Engine Instance 等永久身份按[部署 profile](#专用环境)使用预置专用 Fixture，不适用“每轮创建后删除”；Hosted disposable profile 的当次 Engine Instance 随平台数据卷整体销毁。跨模块 Online 场景不得以直接 SQL 作为常规夹具路线。

每个 suite 必须：

- 让全部资源名称可追踪到 Run ID。
- 为总场景、单次 HTTP 请求、路由收敛和租约收敛设置明确超时。
- 只对临时传输失败或规范允许的异步收敛执行有界重试；业务冲突、权限失败和 409 不自动重试。
- 在成功、失败、超时与中断路径执行 owner 清理并检查错误。
- 结束时查询并断言残留为零；清理失败时，即使业务断言通过，门禁仍失败。
- 将故障注入放在测试 Transport、测试 Provider 或明确测试 hook，不在生产代码中保留调试分支。

## 报告

统一分发器必须在仓库外证据目录生成 `addp.online-gate/v1` 报告，成功与失败采用同一结构。报告至少包含：

- suite、scenario、Run ID、Git commit 和工作区状态。
- 参与服务的脱敏地址与构建身份。
- Tenant、数据库类别、阶段耗时和稳定错误码。
- owner suite 创建、清理和零残留证据。

报告、日志和 CI artifact 不得包含 Token、Client Secret、完整敏感响应或可复用凭据，也不得提交到仓库。

## 触发策略

新增 T4 suite 先通过手工 `workflow_dispatch` 执行。专用 Runner 至少完成一次真实通过，并确认构建身份、清理和零残留证据后，才允许增加夜间 schedule。环境未就绪必须报告环境失败，不得 Skip 为通过。

## 专用场景

各场景的部署选择、夹具和固定断言见[总入口场景索引](../addp测试与验收规范.md#online-场景索引)。

## Graph 与 Cleanup 执行生命周期专项

本专项仅人工选择，复用 `make test-online` 和 GitHub Hosted Linux 的一次性生命周期：Graph 使用 `ONLINE_SUITE=graph-protection`、`ADDP_ONLINE_GRAPH_CASE=execution-lifecycle`，分别选择 FalkorDB／Neo4j；Cleanup 使用独立的 `ONLINE_SUITE=cleanup-execution`。登记和确定性测试通过只证明编排代码，实际运行 Hosted 仍须单独授权，不能通过夜间调度或重跑代替授权。

Graph 主／副 Backend 使用相同产品镜像、不同进程 runtime instance、独立 `127.0.0.1:8186`／`127.0.0.1:18186`，并显式配置 lease 30 秒、heartbeat 5 秒；副实例只在该 case 启动。受控抽取器携带同一 run ID 和准确 case。Cleanup 使用 System、Gateway 和 Meta、Manager、Transfer、Develop、Service、Quality、Graph、Asset、Model、Standard 十 Owner；Graph／Asset 各只有一个实例。它们共享正式 runtime 服务身份，不增加业务权限。

前置检查要求准确 Hosted 标记、独占 `addp_online`、外部 owner-only 私有目录、正式测试身份、全部服务同 SHA 的健康与 readiness；Graph 还要求两实例地址和 runtime instance 都不同。控制仅作用于本轮准确容器实例，不能暂停 PostgreSQL，不能复用个人部署。

分发器严格消费 `graph-execution.json`／`cleanup-execution.json`，要求与 stdout 的报告一致、schema 正确、`result=passed`、`control_cleanup=passed` 以及完整必需 checks。Graph 必须保留受控抽取、非真实模型和 Provider 身份不足时拒绝 reopen 的边界，并校验运行中崩溃的 execution／task、attempt=1、真实 started 观测、不重放、未知 holding 和迟到返回不改终态。Cleanup 必须有十 Owner × scan／execute 共二十条唯一 UUID 结果，以及回执一致、其他用户拒绝、GET 不写入断言；`physical_effect_acceptance=not_run` 不能改写为真实外部效果验收。

`hosted_destruction=required` 表示外层义务。失败、超时或信号退出先以同一私有环境调用 `python3 -m scripts.test.execution-online-support resume`，核对准确暂停实例和空 pins，再销毁应用、夹具、Infra、卷和凭据。恢复失败仍继续精确销毁并令 Job 失败；恢复记录缺失表示无暂停意图，非法／他轮记录、被替换容器不得按名称恢复。场景报告成功不能代替外层退出成功及零残留，原始恢复记录、Token、私有 PG 探针和原始控制日志不归档。

本批不证明原生写图回包丢失、Manager／Transfer 原生未知外部效果、Graph 同物理目标的 positive reopen 或完整故障矩阵。Cleanup 同次接入 Owner 的 `browser_revocation_refresh_pagination_late_response`：必须有独立 `cleanup-browser.json`，与汇总 `browser` 完全一致；刷新和翻页两阶段分别证明真实结果先读、精确撤权后真实拒绝、旧内容清除和迟到响应拒绝，身份、run ID、request／execution 均绑定。Console／System 前端、生产 Nginx 入口和 Chromium 属于此 profile 准备；身份口令和原始 Playwright 输出仅保存在私有临时目录，不归档。不得以未执行或拒绝路径替代其它尚未实现的成功能力。

## 平台三员治理隔离验收

`platform-administrator-governance` 仅允许人工触发 GitHub Hosted Linux x86_64 profile，不支持个人开发部署、self-hosted 或夜间调度。生命周期复用 Hosted 准入与清理；新建独占 Infra、`addp_online`、System、Gateway、Console 与 System 页面，退出销毁本轮身份、数据库卷及临时凭据。禁止运行本机开发生命周期作为验收替代。

System-owned fixture 通过正式 Bootstrap 建立三个不同三员，通过正式账号、成员和 MFA 登记服务准备接任候选及无平台权限的租户用户。三员必须经公开密码、TOTP 和 AuthContext 证明当前身份，不直接签发管理员会话；平台身份仅限本场景，不能用于其他业务 suite。

真实断言覆盖目标岗位申请／复核矩阵、自批和错岗拒绝、版本冲突、暂停／恢复、旧会话失效、角色原子交接并交回原任、一次性密码与 MFA 登记及保持暂停状态。浏览器使用真实 Console iframe 完成申请、复核、凭据登记和重新登录，不 mock API。历史审批与审计不物理删除；场景结束核验无待办、原岗位已恢复，身份历史随本轮 Infra 整体销毁。

真实 Console 跨标签页恢复凭据隔离复用上述三员。审计管理员在原标签页批准恢复，测试先从真实 Gateway 得到成功回执，再暂扣该浏览器响应。用同一会话的第二个标签页正常退出，核对旧访问令牌失效、原标签页离开原页面且旧请求被取消。第二个标签页以同一审计员的新会话重新登录，原标签页恢复到新会话后尝试投递旧回执；页面不得显示旧恢复凭据，也不得重复批准。申请人取消这笔已批准的恢复并核对凭据不可再用。另保留同会话正常显示凭据及完成恢复的对照；原页面仍挂载时实际接收迟到响应的边界由 System 前端隔离回归单独验证。

单会话应急撤销复用本场景的 System-owned 夹具。获显式权限的租户申请人通过真实 Console 提交同一普通用户的准确会话族，系统管理员与安全管理员分别以有效 MFA 会话独立批准，申请人在有效 MFA 会话内明确确认并执行。核验十分钟审批、十五分钟执行、单次消费和原执行回执。执行请求由浏览器发出并原样转发至真实 Gateway，测试端确认收到成功执行回执后截断浏览器连接。页面须保留原申请编号、提示结果待核清并阻止新写入，操作人按保留编号发出一次只读查询取得同一执行回执。观测该浏览器从确认到核清只有一次执行 POST 和一次详情 GET。截断期间独立核对执行效果，目标会话的访问凭据失效，同一用户的另一会话仍可访问且授权事实不变。审计管理员在 Console 独立按原编号核清，并通过正式审计接口核对同一请求的申请、两次批准及执行记录，验证无审批权限、无凭据进入审计。浏览器分项与完整 Hosted 门禁分别记录。

离线恢复前，通过正式治理申请及独立复核为已登记 MFA 的候选人增授系统管理员，形成系统岗位两名持有人及安全、审计岗位各一名持有人。按 Principal ID、账号与岗位逐一绑定四名身份，不以岗位名覆盖凭据。恢复后逐名验证新凭据、旧凭据及会话、授权版本和审计；再按原矩阵批准撤销新增岗位，核验其平台会话失效及原三员岗位保留。

同岗多人场景先验收收集阶段中断：前三名持有人完成真实双 TOTP 窗口，在第四名输入密码前向本轮私有 CLI 进程组发送 SIGTERM 并确认退出。逐名验证四人的原密码、MFA、平台会话、角色及授权版本未变，已收集的新密码与 TOTP 不可使用，恢复审计仅有准备记录；随后以同一枚未消费 Secret 重新收齐四人的凭据并完成恢复。该场景只证明正式 Apply 提交前的中断边界，不替代事务提交中故障或数据库故障验收。

离线恢复在同一临时部署中通过标准 `make build-iam-recovery build-iam-bootstrap` 构建的正式 CLI 验收。驱动真实伪终端完成 `prepare/apply`、隐藏密码输入和每名持有人两个连续的真实 TOTP 时间窗口，不增加非交互参数、固定验证码或测试时钟。核验全部当前持有人凭据替换、旧密码／TOTP／会话失效、角色分配与原暂停状态保留、Secret 不可复用、Bootstrap 不可重建及完整脱敏审计；暂停身份仍须另外按原矩阵批准恢复。终端内容和恢复凭据只在进程内存中流转，不写入文件、参数、环境变量或验收归档，子进程退出时关闭终端并清理内存引用。

凭编号核清复用该套件：收集阶段中断并确认 CLI 退出后，新进程只能观察到 prepared，错误恢复编号必须返回 unknown；四人 Apply 正常完成并确认原进程退出后，另启不同进程执行 `status <attempt-id> <submission-id>`，不输入 Secret、密码或验证码。核清必须返回原提交的 completed_this_submission、同一成功提交编号、四名历史 Principal／岗位及恢复时授权版本；另一个提交编号只能得到 completed_other_submission。后续恢复暂停、撤销新增岗位后再次核清，原回执必须不变，重复核清前后恢复审计完整行和当前会话授权事实不变。报告只保存断言布尔值，非敏感编号也不保存为凭据副本。此项仅证明真实 CLI 退出后的跨进程核清，不替代已提交成功响应被截断的 PostgreSQL T2，也不声称跨进程网络确认丢失 T4 已通过。

凭据不写 URL、日志或报告；浏览器不保留截图、Trace、视频或 Storage State，报告只保存固定断言和脱敏身份。准入、业务和清理任一失败均为失败，未实际 Hosted 运行不计 T4 通过。


### 原生 Cleanup 的独立有限验收

`cleanup-native-effects` 是人工选择的独立 Hosted profile，既有 `cleanup-execution` 逻辑回执／浏览器通过不替代原生效果证明。`ADDP_ONLINE_NATIVE_CASE` 只允许 `manager-success`、`manager-object-unknown`、`manager-index-unknown`、`transfer-engine`、`transfer-dlq-unknown`、`transfer-capture-unknown`，无默认／全矩阵；每 case 全新部署，新运行或环境切换须另行确认。标准确定性入口为 `make test-cleanup-native-online-runner`，只证明编排与拒绝条件，不证明 Hosted 已通过。普通 User 的准确权限和每 case checks 唯一维护在[机器契约](../../../scripts/test/execution-online-contract.json)，登记拒绝缺失／重复／跨 case／伪造通过。

#### 身份、来源准备与实际效果

- Manager：先按当前扫描权限生成本轮元数据，独立授权办理员以规范指纹读取 System 的准确 DataItem 事实并办理既定源 Grant；普通消费者获 Grant 后才枚举 Meta，核同一 full_name／fingerprint 后准备 Model3D。指纹只定位事实，不证明身份、权限或存在；缺失／冲突、Grant 拒绝或对象错配即停，不加权限或借机器身份。
- 扫描前 `manager_protection_readiness`：原 Manager User 用既有 `manager.derived_artifact.read` 查询本轮模型指纹 GLB 列表；仅 HTTP 403／`security_protection_required` 可继续等待，其它拒绝立即失败。HTTP 200 后才发一次原扫描；该只读检查不消费源内容，不能替代源 Grant 或库存验证。
- Meta 列表只用于结构身份，不要求 size_bytes、网格统计或内容派生字段。OBJ／FBX 由原 User 下载核固定长度／摘要，OBJ 严格核模型／MTL／纹理全 ZIP，目录大小不能替代字节证明。四个 Manager case 共用既有 GatewayClient，保留 raw 二进制／响应头、按 Content-Type 解析 JSON、原 loopback／User token；ZIP／GLB 不交控制面 JSON 客户端，无新身份、下载旁路或重试。
- Manager 通过正式源 Grant／Model3D 生成真实 GLB，清理后独立核输出对象与索引、原 OBJ／MTL／纹理不变。unknown case 核代理回执、对象／索引隔离、原生索引缺失及拒绝前后事实。`manager_unknown_engine_read`、`manager_repeated_delete_refusal`、`manager_new_generation_refusal` 分别记录读取／重复删除／新生成；快显来源生命周期冲突为 HTTP 409。
- 索引 unknown 的持久隔离绑定准确 Tenant＋Engine／物理索引、原 execution／attempt、出口代次／端点、prepared 操作、唯一删除投递及目标，以及绑定该操作的 hold。原投递可由既有只读历史推进，不要求永远 unknown；推进不表示 Cleanup 完成或允许重发。拒绝前后比较全部业务／隔离事实，仅排除瞬时 unknown 诊断计数，原 11 项断言及请求顺序不变。
- Transfer：区分 Engine 删除保留并禁用定义／DLQ，与 User 显式删任务清 DLQ；真实目标业务行不变。成功 Engine 清理核保留定义禁用；capture unknown 核准确 Tenant／任务、创建与保留读取均为 boolean `enabled=false` 的无调度 continuous 定义、原生运行体已停、unknown 持久隔离且新 producer 被拒绝。禁用不等于清理成功或运行期望。
- DLQ 围栏拒绝新 producer 必须是 HTTP 409／`transfer_cleanup_unresolved`，并有安全本地化文案；报告同时保存 `producer_refusal_http_status=409` 和 `producer_refusal_error_code=transfer_cleanup_unresolved`，响应 `error` 为安全本地化文案。500／其它码不是证明；DELETE 外部清理失败仍为 503。
- `transfer-engine`／`transfer-capture-unknown` 依当前 Tenant／任务权限，不准备无对应消费链的源 Grant 或调用 Manager 同步规则检查；CDC 由正式 runtime 与独立目标行探针证明。本专项不验证 Transfer 当前 Grant／Deny、源撤权或连续运行收权闭环。

#### 期限、阶段与读取约束

| 环节 | 必须保持的范围与失败判定 |
| --- | --- |
| 构建／整体 | 公共构建器 `--jobs 1` 实时输出各镜像及委托 Runtime 日志；固定编译／镜像／启动／身份／业务阶段，保留原退出码并进入销毁链，不输出额外参数／凭据。workflow 120 分钟、业务 900 秒不变，缺终态或清理证明不得通过。 |
| Manager 准备 | 保护就绪、精确 Meta 扫描及本 case 唯一 MinIO 的 active／online 共用原 180 秒绝对截止。Engine User 仅 GET 本轮实例，不连接探测／扩权／增窗；单请求取原 60 秒与余额较小值，耗尽不补读、晚返不扫描／准备模型。失败仅用原 readiness 的安全观测。 |
| CDC 准备 | 两 case 的扫描及 Engine 就绪共用原 180 秒；扫描成功后原 Engine User 仅 GET 本轮 PG／Kafka，核 Tenant／ID／类型、active＋online，再创建任务；不连接测试／增权／重试创建。就绪失败留最后已取身份／闭集状态，创建 400 留准确源／目标绑定与闭集拒绝；启动仍 120 秒。 |
| 初始目标行 | 原物理 SQL／精确行值和 120 秒窗口不变，非零仍 false。只在不匹配且有余额时，原 Transfer User 可 GET 同一自有 execution，timeout 不超 60 秒及余额，读耗计原窗；失败不改物理成功谓词，耗尽不补读／重发业务。 |
| Engine 评估／删除 | 原请求前标记评估受理／读取、删除确认、执行观察及墓碑读取。Transfer 另标停止证明、定义／DLQ 保留、专业结果、显式删任务、删除复核和业务行核验；不复制 URL／请求体／确认名称／错误原文。原七项须全完成，空 checks 不算部分业务通过。 |
| 评估观察 | 原 120 秒读取预算；仅用原 GET 最后观测，准确请求／Engine／原用户／scan／Engine 分支完整七 Owner 全匹配，才投影已授权可见 Manager／Transfer 子项，不补读隐藏 Owner／专业结果／私表。 |
| execute 等待 | 原 360 秒及 Engine GET／成功谓词不变；仅无 execute 引用、存在合法 validation scan 且有余额时，同 User 读 Engine 发布的 validation 公开投影。读耗计原窗、单次不超原 HTTP 上限及余额；有 execute 只读原 execute。validation 终态不当 execute 终态，读失败不改等待结果或覆盖已得安全观测。 |

#### 失败诊断的唯一投影

以下字段**只允许相应阶段失败时出现**，成功报告即使附空值／null 也拒绝。除上表明确允许的原窗口内读取外，仅分类已有异常／回执，不加请求、身份、重试、补读或等待；保留首败。所有诊断禁入原始错误／正文、URL／查询／路径、连接配置、凭据和敏感标识；未知只按闭集归一，不复制原值。

| 字段／阶段 | 允许内容与独有约束 |
| --- | --- |
| `model_prepare_diagnostic`／仅 `manager_model_prepare` HTTP 异常 | 仅 `operation`＋`error_code`。operation：`meta_items_read`、`manager_model_results_read`、`manager_model_tasks_read`、`manager_source_download`、`unrecognized`；错误：`permission_denied`、`security_protection_required`、`source_authorization_required`、`source_composition_incomplete`、`engine_unavailable`、`authorization_service_unavailable`、`download_bundle_invalid`、`download_source_changed`、`unknown`、`unrecognized`。缺码为 unknown，其它为 unrecognized。 |
| `manager_inventory_diagnostic`／`manager_native_inventory` | probe：`storage_reference\|native_object\|index_projection\|artifact_state`；reason：`private_read_failed\|storage_reference_missing\|storage_reference_invalid\|storage_reference_outside_scope\|object_empty\|object_unproven\|index_response_incomplete\|index_scope_mismatch\|index_not_observed\|artifact_not_live\|http_error\|unrecognized`，HTTP 异常可附数字 http_status。原读取顺序／范围／等待不变，不保存私有引用。 |
| `manager_unknown_diagnostic` | probe：`proxy_receipt\|object_hold\|index_hold\|index_absence\|engine_read\|repeated_delete\|new_generation\|facts_recheck\|proxy_recheck`。原快照 `index_unknown/index_targets/index_holds` 及代理 forwarded 仅 `zero\|one\|multiple\|unrecognized`；仅补 binding_matches 布尔与 upstream_response 的 `true\|false\|unrecognized`。后续失败可复用原 execute 已得 engine_cleanup_diagnostic，不补读或存绑定原文。 |
| `initial_records_diagnostic` | CDC／Kafka 种类、物理查询失败／行值不匹配／夹具命令失败闭集，以及窗内最后已取 execution。核任务／执行绑定、闭集状态／安全错误类别和码／停止恢复扫描元信息；最近读失败类别／HTTP 状态另列，不覆盖已有观测。不含 SQL／行值／stderr，不证明 Capture、目标行或后续删除已通过。 |
| `engine_assessment_diagnostic` | 本轮请求 ID、父闭集状态及请求／Engine／原用户／scan／七 Owner 集合匹配布尔值。父完整匹配后才投影可见子项的闭集状态、attempt、是否开始、回执状态／安全原因、准入／授权拒绝／不可用；首次读前失败记未观测，非法结构／错绑定另分。不存时间戳／Metadata／统计。 |
| `engine_cleanup_diagnostic` | 最后同一 Engine 的闭集 lifecycle、四类固定 deletion_error／空／未识别、两引用存在且合法；最后 validation／execute 安全任务观测及最近读失败类别。按 assessment 同样绑定，但精确 action 必须正确；不保存名字／时间戳／Metadata。原六权限／七断言不变。Manager 清理失败分 `manager_cleanup_engine_facts`／`manager_cleanup_inventory`；Transfer 只沿安全 step 投影计划／连接、停止、Capture 原生步骤、DLQ／提交，须同 Owner／当前 attempt1／已开始且 failed／单一 step 与 failed/terminal 一致，阶段集合以 Cleanup 规范为准。 |

#### 故障代理与退出清理

四个一次性代理在应用启动前固定指向本轮 Infra，仅对准确关联操作发送一次真实变更后丢回包并阻止重发。控制端点仅容器 loopback；Kafka 内部 advertised listener 过代理，外部 listener 供独立只读原生探针直达。含秘密 manifest、描述符、代理配置及展开 Compose 保持私有 0600，不归档。

退出先停产品／Worker／Runtime，再销毁代理、业务夹具和 Infra；恢复、销毁或零残留失败均令 Job 失败。六 case 不证明旧 writer 晚写、原生写图丢回包或 positive reopen，报告保留 `not_run`。

#### 额外显式专项：Manager 晚写

`manager-late-writer` 保持六 case 原边界，仅证明 `manager_glb_upload_after_owner_crash`。访问计划直接传 `MINIO_ENDPOINT` 给 Model3D 原生 S3，固定 object proxy 只对当次 pending StorageRef 准确 PUT 设私有有界 barrier，不加产品路由。仅该 case 使用 `MANAGER_EXECUTION_LEASE_DURATION=30s`／`MANAGER_EXECUTION_HEARTBEAT_INTERVAL=5s`，其它默认不变。

必须见完整真实上传进入但尚未发送，才终止并补回本轮 Manager，真实等待旧 lease 失败；Engine 评估因未决 writer 阻断，不能 DELETE。释放同一 PUT 后原生读取核迟到对象出现，而原 failed attempt、pending 审计和新 generation 围栏不被覆盖；不外推 Graph 或正向 reopen。

同次公开 `engine_task_observation` GET 核准确请求／Engine／原用户／scan／完整 Owner，子项唯一合法 UUID、准确父 execution／cleanup_executor。Manager 冻结计划拒未决产物时，子项 attempt1、已开始且 failed、唯一 `manager_cleanup_plan` 失败阶段，无伪造回执；父在原 30 秒协调截止后 timeout。其它 Owner 失败、未开始／错 attempt、缺阶段、错父子、授权拒绝／不可用均不能替代。失败只复用该 GET 已得 assessment，不补读隐藏 Owner／私有错误。

原 11 项目标、请求／读取次数、权限、120 分钟部署／900 秒业务、180 秒 barrier、90 秒租约收敛及各等待上限不变；barrier 到期失败并销毁，不续期／重发。退出恢复本轮暂停实例并释放 barrier，控制状态／原始 body／私有计划不归档。实际 Hosted 仍须独立确认。

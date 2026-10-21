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

三种 profile 共用同一个 `make test-online` 分发器和 owner 业务断言，生命周期脚本只负责当次环境和夹具。编排只调用现有 Infra、开发生命周期脚本和 `make test-online`，不得在 workflow 中复制模块启动逻辑或业务断言。Hosted 原生 Linux 经开发入口启动的 PointCloud、Document 容器使用宿主网络，直接以配置端口监听，通过回环地址访问 System、Infra 与 Business MinIO；不得用 bridge 的 host gateway 访问仅发布回环端口的服务。该启动契约由 `make test-dev-lifecycle` 覆盖，真实对象存储读写仍由 Manager T4 验证。退出路径必须停止本次应用进程并报告清理结果；常规 macOS self-hosted Infra 可在专用 Runner 常驻，Hosted 与 License 受控 owner-managed profile 的 Infra 必须随 Job 销毁。

各 suite 的部署选择、专用夹具和业务断言见[场景索引](../addp测试与验收规范.md#online-场景索引)；专用条款只适用于其声明的 suite 与 profile。

## 开关、身份与拓扑预检

每次 T4 运行至少满足：

- `ADDP_ONLINE_TEST=1`。
- 显式非默认 `ADDP_ONLINE_TEST_TENANT_ID`，禁止 Tenant 1。
- 全局唯一的 `ADDP_ONLINE_TEST_RUN_ID`；未提供时由分发器生成并贯穿全部子测试。
- 每个参与 HTTP 服务 `/health/live` 可用，且 Build ID、Git commit 或源码指纹与当前 checkout 匹配；进入业务断言前 `/health/ready` 必须返回 200。
- 除[平台节点指标控制面隔离验收的限定例外](scenarios/平台观测与语义生命周期.md#platform-node-metrics)外，测试 User、Service Principal 和 OAuth Scope 只具备场景所需的最小权限；不得使用个人会话或扩大生产身份权限。平台管理员 Token 默认禁止，仅在该限定例外及 `platform-administrator-governance` 的一次性 Hosted 三员身份中允许。
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

## 平台三员治理隔离验收

`platform-administrator-governance` 仅允许人工触发 GitHub Hosted Linux x86_64 profile，不支持个人开发部署、self-hosted 或夜间调度。生命周期复用 Hosted 准入与清理；新建独占 Infra、`addp_online`、System、Gateway、Console 与 System 页面，退出销毁本轮身份、数据库卷及临时凭据。禁止运行本机开发生命周期作为验收替代。

System-owned fixture 通过正式 Bootstrap 建立三个不同三员，通过正式账号、成员和 MFA 登记服务准备接任候选及无平台权限的租户用户。三员必须经公开密码、TOTP 和 AuthContext 证明当前身份，不直接签发管理员会话；平台身份仅限本场景，不能用于其他业务 suite。

真实断言覆盖目标岗位申请／复核矩阵、自批和错岗拒绝、版本冲突、暂停／恢复、旧会话失效、角色原子交接并交回原任、一次性密码与 MFA 登记及保持暂停状态。浏览器使用真实 Console iframe 完成申请、复核、凭据登记和重新登录，不 mock API。历史审批与审计不物理删除；场景结束核验无待办、原岗位已恢复，身份历史随本轮 Infra 整体销毁。

离线恢复前，通过正式治理申请及独立复核为已登记 MFA 的候选人增授系统管理员，形成系统岗位两名持有人及安全、审计岗位各一名持有人。按 Principal ID、账号与岗位逐一绑定四名身份，不以岗位名覆盖凭据。恢复后逐名验证新凭据、旧凭据及会话、授权版本和审计；再按原矩阵批准撤销新增岗位，核验其平台会话失效及原三员岗位保留。

同岗多人场景先验收收集阶段中断：前三名持有人完成真实双 TOTP 窗口，在第四名输入密码前向本轮私有 CLI 进程组发送 SIGTERM 并确认退出。逐名验证四人的原密码、MFA、平台会话、角色及授权版本未变，已收集的新密码与 TOTP 不可使用，恢复审计仅有准备记录；随后以同一枚未消费 Secret 重新收齐四人的凭据并完成恢复。该场景只证明正式 Apply 提交前的中断边界，不替代事务提交中故障或数据库故障验收。

离线恢复在同一临时部署中通过标准 `make build-iam-recovery build-iam-bootstrap` 构建的正式 CLI 验收。驱动真实伪终端完成 `prepare/apply`、隐藏密码输入和每名持有人两个连续的真实 TOTP 时间窗口，不增加非交互参数、固定验证码或测试时钟。核验全部当前持有人凭据替换、旧密码／TOTP／会话失效、角色分配与原暂停状态保留、Secret 不可复用、Bootstrap 不可重建及完整脱敏审计；暂停身份仍须另外按原矩阵批准恢复。终端内容和恢复凭据只在进程内存中流转，不写入文件、参数、环境变量或验收归档，子进程退出时关闭终端并清理内存引用。

凭编号核清复用该套件：收集阶段中断并确认 CLI 退出后，新进程只能观察到 prepared，错误恢复编号必须返回 unknown；四人 Apply 正常完成并确认原进程退出后，另启不同进程执行 `status <attempt-id> <submission-id>`，不输入 Secret、密码或验证码。核清必须返回原提交的 completed_this_submission、同一成功提交编号、四名历史 Principal／岗位及恢复时授权版本；另一个提交编号只能得到 completed_other_submission。后续恢复暂停、撤销新增岗位后再次核清，原回执必须不变，重复核清前后恢复审计完整行和当前会话授权事实不变。报告只保存断言布尔值，非敏感编号也不保存为凭据副本。此项仅证明真实 CLI 退出后的跨进程核清，不替代已提交成功响应被截断的 PostgreSQL T2，也不声称跨进程网络确认丢失 T4 已通过。

凭据不写 URL、日志或报告；浏览器不保留截图、Trace、视频或 Storage State，报告只保存固定断言和脱敏身份。准入、业务和清理任一失败均为失败，未实际 Hosted 运行不计 T4 通过。

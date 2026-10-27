# Monitor 执行监控与留存历史证据

[返回 Monitor 历史证据](Monitor历史证据.md) · [返回验收证据索引](../验收证据索引.md)

本专题收录continuous、Owner 不可用、租户日志终审及过程事件留存。固定源码、环境、失败记录、修复结论与未覆盖范围以各记录为准；本次仅整理文档，没有重跑验收或刷新 CI 状态，不推导当前 HEAD 或生产 T5 已通过。跨主题语境与原有顺序见 [Monitor 历史证据入口](Monitor历史证据.md#原记录入口)。

## 2026-10-24 概览保留只读评估第一期

Common 增加 `AssessRetention`，System 沿已有公共维护循环输出有界批次汇总。数据库强制只读／可重复读，使用数据库时间计算 180 天时间候选，并检查完整父子树、租约、关系异常、树外重试／恢复引用及预算。首期始终保留 Owner 行内事实释放和跨模块引用覆盖未核验的原因；不调用 Owner 私表、不新增用户 API、不提供物理删除或专业结果期限。批次不是全库唯一树数，不能累加为完整盘点。

本地标准验证：`make test-module MODULE=common GO_ONLY=1`、`make test-module MODULE=system GO_ONLY=1` 通过；`make test-common-postgres COMMON_POSTGRES_TEST_ARGS="--test execution-retention"` 在实际 Infra 映射 25432／`addp_test` 通过 11 组 T2，覆盖多层树／子种子、年轻子执行、跨租户父子及反向引用、环与断链、恢复引用、非法 JSON 来源、查询／行／树预算、查询失败不发布部分报告、并发新增子执行的一致快照、取消和拒绝继承可写事务。执行行及专业配置／结果／错误和过期过程事件前后不变，夹具逐项清理后零残留。三个门禁的实时输入守卫均未发现变化；Common PostgreSQL 并发锁拒绝验证通过，拒绝发生在数据库测试启动前。默认 Common PostgreSQL CI 已登记并强制要求新增测试，不新增 workflow。

首次 T2 的三个夹具断言失败已保留在会话记录：共享时间指针被后续更新改写、时间精度比较及注入钩子时机；修正后沿同一标准入口通过。一次 System Go 运行虽通过，但期间修改了 Common 测试文件，守卫标记输入变化，不作为最终证据；最终两模块 Go T1 与 PostgreSQL T2 在同一稳定输入指纹下通过。

未运行：工作区整体 `make test-changed`、完整 Common PostgreSQL 聚合及真实维护 T4；共享工作区还有其他会话的业务改动，本批仅验证新增共享评估和实际消费者 System，未重跑其无关门禁。没有启动／重启个人服务，没有进行执行概览删除、Owner 释放裁决、专业结果到期或长期容量验收；此前固定版本 T4 不扩展为本功能通过。

## continuous 诊断与 Owner 不可用

| 尝试 | 源码 | 已记录结果与边界 |
| --- | --- | --- |
| [38329160596](https://github.com/pampa0629/addp/actions/runs/38329160596) | `bfc305953` | 整轮 T4 失败：真实自动刷新、恢复来源和稳定码已验，但撤权只清事件、未清终态概览/步骤；Monitor-only 与后续 CDC 未运行。清理通过、残留 0。本地修正后前端 56 单元/118 浏览器及构建通过，401 用例改为会话失效导航且内容全清，没有放宽旧内容断言。 |
| [38337857450](https://github.com/pampa0629/addp/actions/runs/38337857450) | `b0765f802` | 整轮 T4 失败：live/recovery 与旧内容清空通过，新鲜 Monitor-only 提示断言失败；清理及 Infra 零残留。StatusAnnouncer 与 el-result 同文案造成 strict-mode 双匹配，修正只定位唯一可见标题，保留播报和无详情/步骤/事件/树断言。 |
| [38381655995](https://github.com/pampa0629/addp/actions/runs/38381655995) | `65de8a82938ba1bbdd56df6f7c000be7f67076cc`，包含 `ba2c99652` | 完整 Hosted T4 通过：live/recovery/revocation 三份报告及九组接口/Worker 检查，Worker 失联新 UUID、暂停/恢复、跨/同租户 Owner 权限、Owner 不可用、撤权与新鲜 Monitor-only 拒绝、CDC 事件、删定义后历史读取。五模块源码一致、37 归档无私有控制文件或访问令牌/JWT 模式，领域/Infra 零残留。修正的前端 59 单元/131 浏览器与构建、transfer runner 65 项通过；不含后续 Workflow/Notebook 监督器或 T5。 |
| [39026543398](https://github.com/pampa0629/addp/actions/runs/39026543398) | 原文未单列 SHA | 整轮失败：隔离入口漏启 Security，在 Owner 故障阶段之前退出；清理及 Infra 零残留，不计 Owner 故障通过。后续场景预检 Security，并只读等待独占部署的 Transfer projection initialized，不写保护事实或连接本地共享业务库。 |
| [39082349255](https://github.com/pampa0629/addp/actions/runs/39082349255) | `009ceea39` | Owner503 八项页面检查与同进程停止/恢复通过，三张截图、领域/Infra 清理通过；整轮仍失败，原因是把运行中 CDC 错误要求为 running 终态事件，后续 CDC/审批/调度未运行。修正要求 CDC 仍运行且无终态；终态执行仍恰有一个匹配终态事件。 |
| [39083559464](https://github.com/pampa0629/addp/actions/runs/39083559464) | `946c4d13e` | 完整隔离 Hosted T4 通过：14 业务检查、真实 Owner503 八项页面检查、撤权/Monitor-only、Kafka 恢复、CDC 审批与双向扫描隔离、调度当前主体、删定义后历史。相同 Owner PID 停止/恢复证明 true，归档 `12121475487` 的三张截图已目视核验；业务残留 0、cleanup passed、外层 Infra zero_residuals。保留产品历史，不外推 Notebook、其他 Owner 或 T5。 |
| [39905778336 / Job120257300658](https://github.com/pampa0629/addp/actions/runs/39905778336/job/120257300658) | `5be2c2e0e9c6fb1f49da54a076753f2568462fd4` | 整套失败：14 项检查及四份浏览器分项报告通过，新增多页／保存游标撤权及不可见总数已有分项证据；但旧验收白名单拒绝 Transfer 已声明的专业 `initial_authorization.status/code`，成功的定时执行在校验及清理历史读取时被拒绝，task 3 未完成删除。领域 cleanup failed、残留 1；外层部署销毁不能抵消该失败，不计整套通过。 |
| [39923509570 / Job120314175151](https://github.com/pampa0629/addp/actions/runs/39923509570/job/120314175151) | `9e67500b26136ef909086d70ee9536d925ff3581` | 固定版本完整 Hosted T4 通过：六服务同提交，场景 176.851 秒，16 项唯一检查、四份真实浏览器报告一致通过。补齐保存游标撤权、不可见总数、定时扫描与父子读取隔离及删定义后历史；任务 3／2／1 均 verified_absent，领域残留 0、无不确定写入，部署清理及 Infra zero_residuals。官方归档 `12493568384`，范围及限制见下文。 |

Owner503 场景使用正式普通 User 与真实 Monitor 响应；故障夹具仅停止当次隔离部署中经 pidfd、可执行文件、UID、数据库和私有目录核验的 Transfer Backend，再恢复同一进程。浏览器分别验证终态详情、运行树、非空列表的清空与重新裁决；独立网络中断仅影响事件读取，不冒充 Owner 503。控制文件只在当次私有目录，公开回执不包含 Token、业务样本或完整接口响应。标准入口仍为既有 transfer-continuous-events Hosted suite 与 `make test-transfer-continuous-online-runner`。

## 2026-10-23 固定版本租户日志终审

对 `39923509570` 的官方归档及原始 Job 日志完成独立核验。归档 SHA-256 为 `5c32cadccc438db1e7c1e16cd5edd9056c0169c1a87fa75dbeb2efe57902be2d`；原始标准输出中的 suite JSON、`transfer-continuous-events.json` 与外层 `suite_report` 完全一致，源码干净且 System、Gateway、Meta、Transfer、Monitor、Security 六服务均为上述完整提交。四份浏览器报告逐一核对本轮 Run ID、Tenant、Principal、execution UUID 及各自的闭合检查集合，Owner 不可用详情清空及恢复诊断截图已目视复核，恢复来源绑定以本轮报告和 DOM 断言为据。既有标准入口仍为 `make test-online ONLINE_SUITE=transfer-continuous-events`，由原 Hosted 生命周期执行；修复提交的 `make test-transfer-continuous-online-runner` 已通过 57 项确定性回归及 System 身份夹具 Go 测试，本次终审没有重跑或再次触发。

同一 Tenant 2 普通读取 User 37 在 Owner 与 Monitor 实际读取每页 2 条、共 2 页 4 条事件；正式撤权后旧 Token 为 401，重新登录的同一 Monitor-only User 使用保存的第一页游标仍被 Owner 403／Monitor 404 拒绝，执行列表总数为 0。同租户无 Owner 权限及跨租户身份也返回空列表与零总数。真实 Owner 进程停止／同 PID 恢复、详情／树／步骤／事件与列表旧内容清空、独立事件网络故障，以及 Worker SIGKILL 后换 PID、租约收敛及新 execution 恢复均通过。Kafka、CDC 独立审批人及精确扫描边界沿原断言保持。

此前未达到的定时子阶段本次完整通过：普通 User 33 的 `scheduled` 执行、实际父／Meta scan 绑定、peer 可读父而 scan 为 404、物理行集、调度暂停及目标精确扫描均有报告证据。最后的 `deleted_definition_history_retained_with_owner_permissions` 实际执行。逐项清理回执为 scheduled 3、CDC 2、Kafka 1 全部 `verified_absent`，原始 Backend 日志有三次 DELETE 200 及各自随后 GET 404；suite 的 `cleanup=passed`、`residual_resources=0`、`uncertain_mutation=false` 与外层部署清理、Infra 零残留分别核验。

该通过只覆盖固定版本、此套件的真实身份与拓扑，不外推当前后续 HEAD、其他 Owner、Security 日志专项、Notebook 运行许可、容量、长时间运行、HA 或生产 T5。API 每页 2 条的多页证据不代表超过 100 条的浏览器加载更多、每日 1000 条截断或完整节点历史已验。`history_cleanup=pending_deployment_destruction` 表示本轮保留历史后随独占部署销毁，不能写成执行历史留存清理已验；30 天事件物理维护周期及 180 天概览整树安全删除仍未由此 T4 证明。旧 `5be2c2e0e` 失败保留，不由此次通过覆盖。

## 执行概览只读维护真实进程验收（2026-10-25）

本批仅补第一期只读候选评估的真实 System 维护首轮，人工 Hosted Linux `execution-retention-readonly`，独占 System／Gateway／Infra；不启用 180 天概览物理删除，不改变任何 Owner 专业结果期限、业务实现或现有读取授权。System-owned helper 通过正式 Tenant Service 建立两个非默认 Tenant，普通测试 User 仅获既有 `monitor.execution.read`；实际 AuthContext 校验后，由受限 Common 历史 helper 准备合成旧记录，标准生命周期停止／启动的仅是本轮独占 System。维护使用产品既有首轮与周期，不直接调用评估函数代替真实进程，不增加测试 API 或产品调度参数。

首轮 [Run40138788559／Job121005906648](https://github.com/pampa0629/addp/actions/runs/40138788559/job/121005906648)，固定源码 `2a3cbb1f677af23f955ff02038205d525aafc93d`，**失败**于历史准备，后续 14 棵树的维护断言未运行，不能计为通过。夹具错误地使用了现行 `chk_task_executions_status` 禁止的 `unknown_fixture`；原日志仅记录 `prepare:CalledProcessError`，没有当时的 SQLSTATE，不补写为已观测的数据库错误码。应用及凭据清理为 passed、Infra 为 zero_residuals。官方 Artifact `12577428981` 已下载并独立核对 ZIP SHA-256 `1a4fe210c3d121de2f13e977751df08dfe51f1b47121d105aa87a2527843b9b8`；原日志 `/tmp/addp-retention-t4-40138788559.log`、提取报告 `/tmp/addp-retention-t4-40138788559/`、审查回执 `/tmp/addp-retention-readonly-t4-receipt-20261025.json`。

修正提交 `86f58b3764c4c3e47af1a141d81e21f94109150c` 改用合法 pending 夹具，并补当前存储 CHECK 与所有夹具状态的交叉回归；未知状态的防御仍仅由 Common 单元层覆盖，不放宽或删除生产约束。测试驱动的 SQL 失败只记录 SQLSTATE，汇总失败只保留白名单计数，拒绝归档原始 SQL、私有字段或任意保留原因内容。标准入口 `make test-execution-retention-online-runner` 实际 **28 项 Python 回归通过**，System `cmd/online-test-fixture` Go T1 通过，本批 `TestExecutionRetentionFixtureHasOnlyMonitorReadPermission` 明确 PASS；同包三员、Catalog 授权 observation、PG 授权成员、Metrics 与 Raster Policy 的 5 个既有 PostgreSQL 专项因未设置标准 DSN 跳过，不计为本批 T2 通过；含只读汇总封闭性、计数错误、整行／事件变化拒绝、容器身份拒绝、初次日志落盘等待、准备失败、业务失败与清理残留拒绝，最终显式 PASS／SKIP 日志 `/tmp/addp-retention-t4-runner-final.log`。本轮不重复未变产品的既有 Common PostgreSQL T2；全库注册检查在共享未提交工作区曾受并行 Graph 契约修改阻断，不计为通过，也不修复该 Owner 的业务实现。本批注册验证、唯一人工 Hosted profile、Online 分发器及标准 runner 归属已由上述定向门禁覆盖。

修正版真实验收 [Run40140020076／Job121009605836](https://github.com/pampa0629/addp/actions/runs/40140020076/job/121009605836) 绑定 `86f58b3764c4c3e47af1a141d81e21f94109150c`，已完成且独立审查通过：Run／Job、真实运行步骤均 success，attempt 1；前述排队状态现由实际结果闭合，首轮失败仍保留。验收要求 16 条合成历史形成 14 棵执行树、5 棵时间候选，核对完整超期树、年轻子执行、running／pending、有效 lease、完成时间缺失、缺父／跨 Tenant／自指及树外 retry／recovery 引用；全部树始终保留 Owner 释放和消费者引用未核验原因。维护前后执行完整行与 30 天内事件的数据库摘要必须一致，报告仅保留安全批次事实，退出还须核验独占应用／Infra／卷／凭据清理。该证据不代表用户执行读取权限、Owner 释放、专业结果生命周期、30 天事件物理维护、小时周期、概览删除、容量／HA 或生产 T5 验收；清单实际第327／328／329行继续部分实现。

修正版官方报告实际为 10 项唯一断言通过：16 个种子、14 棵完整树、5 棵时间候选，本批读取 24 行、20 次查询，游标16、无后页、未超预算。保留原因为 Owner 释放未核验14、消费者引用未核验14、活动执行2、完成时间缺失3、有效lease2、未超期1、关系异常4、树外执行引用2；时间候选并非允许删除。执行完整行和保留期内事件不变均为 true，汇总多余字段数为0。System／Gateway 的正式 preflight 均为同一固定源码与独立 Build ID／源码指纹，工作区干净、网络仅回环；普通 Tenant User 的唯一角色为 `online.execution_retention_readonly`，所验证 Permission 仅 `monitor.execution.read`。

Suite 内的 `cleanup=pending_deployment_destruction` 只是场景完成时尚保留合成历史；独立核对最终 Host `summary.txt`，结果和清理均 passed、Infra为 zero_residuals，应用停止、测试卷／网络和凭据销毁均已完成。该销毁不计180天概览物理删除。官方 Artifact `12579077155` 的5个文件仅为既有安全报告及生命周期日志，不含凭据目录或原始 System 进程日志；ZIP已下载，SHA-256独立匹配上传声明 `eaa740d8a10f7a767b66f266b2dc51cb3b2c2f2d0f08457cadc2a4b2807c7c2c`。证据：`/tmp/addp-retention-t4-40140020076.zip`、同名提取目录及 `.log`、`/tmp/addp-retention-t4-40140020076-run.json`、`/tmp/addp-retention-readonly-t4-rerun-receipt-20261025.json`。已实际比较固定源码至后续证据提交：Common execution、System维护与身份夹具、测试驱动、Hosted路径及workflow均无变化；后续仅证据记录与Make中Go结果显式输出，不重复有效T4。

截至2026-10-25 07:17（Asia/Shanghai），同修正提交的 [Platform CI Run40139997741](https://github.com/pampa0629/addp/actions/runs/40139997741) 仍queued，未计为通过；该聚合CI不能由本批T4替代。当前闭合的范围仅为本地28项回归／Go T1与固定版本真实System首轮只读维护；5项既有PG专项的跳过、未运行的更广生命周期及首次失败均继续保留。Excel实际327／328／329行仍为部分实现，已向维护会话同步；Owner引用／释放、专业结果生命周期与物理删除不进入本轮开发。

### 固定版本 Platform CI 复核（2026-10-25）

2026-10-25 23:58（Asia/Shanghai）仅复核本批既有自动 push [Platform Run40139997741／Platform consistency Job121009537951](https://github.com/pampa0629/addp/actions/runs/40139997741/job/121009537951)，attempt 1、固定源码 `86f58b3764c4c3e47af1a141d81e21f94109150c`。Run、Job及实际 `Run platform consistency gate` 均已 completed／success；真实门禁调用 `make test-platform`，执行时间为当日07:19:06–07:41:06，Job于07:41:09完成。前述截至07:17的排队状态由本次实际结果闭合，不以摘要步骤成功代替真实门禁。

原始日志第2972–2997行与该源码Makefile的 `test-platform → test-online-runner → test-execution-retention-online-runner` 第一子入口对应，明确记录本批28项Python回归通过（失败0、跳过0），随后System `cmd/online-test-fixture` 包返回 `ok`；Online CI登记检查也实际输出 `Online CI registration is consistent`。这补齐了租户日志只读维护runner及唯一人工Hosted profile在干净CI中的登记／执行证据。本地显式日志中5项既有PostgreSQL专项的跳过仍保留，不据CI中Go包的成功补计T2；先前共享未提交工作区的本地全库注册阻断也作为不同输入的历史事实保留。

证据为 `/tmp/addp-retention-platform-40139997741-metadata.json`、`-attempt.json`、`-audit.json`及原始 `.log`，日志SHA-256为 `0ca82b64f5dafd79eba1021c3c1962392b25cf9374f1e81f20662bff27ca1e45`。独立核对了固定head SHA、push事件、attempt、真实步骤、标准入口及28项对应日志；没有重跑CI、T4或未变产品测试，也未跟进其他模块Job。该结论仅针对上述固定版本，不外推当前HEAD、其他Owner、专业结果生命周期、小时周期／30天实际事件维护、180天物理删除或T5。

本批CI核对可收口；功能清单实际327／328／329行仍保持部分实现。Owner释放／消费者引用裁决及删除仍需另行讨论确认，不进入本次开发。本轮仅追加现有证据并运行 `git diff --check`，未改产品、期限、权限或工作簿，不启动／重启服务，不新增定时或验收场景。


## 30 天过程事件清理真实进程验收（2026-10-26）

实现提交 `8e84cc8a69eac172bcd74639803c7450b55ce2d3` 仅增加 `execution-events-retention` 验收、共享 Hosted 生命周期适配、最小身份夹具及登记／规范；未修改产品清理逻辑、权限目录、专业结果期限或其他模块业务功能。原 `execution-retention-readonly` 继续验证全部事件不变，新场景沿同一人工 Job 独立选择，不登记夜间调度。System-owned helper 只为当次普通 Tenant User 授予既有 `monitor.execution.read`，正式 Tenant Service 创建两个非默认租户；AuthContext 校验后，标准停止本轮独占 System，再由绑定完整容器身份的受限 helper 写入合成 Common 历史。测试不直接调用清理函数或删除 SQL，不使用个人环境。

[Hosted Run40254456204／Job121353216036](https://github.com/pampa0629/addp/actions/runs/40254456204/job/121353216036) attempt 1、workflow_dispatch，源码固定为上述提交；Run／Job及实际 `Run Hosted execution retention maintenance Online gate` 均 completed／success，实际门禁为09:30:52–09:42:25（Asia/Shanghai）。两个租户各有一条终态和活动执行，共4条；真实 System 首轮分批清理1004条31天前事件，超过单批1000条，8条窗口内事件（29天及新事件）完整行摘要不变。4条 execution 完整行摘要不变，包括冻结配置、私有错误、步骤、合成专业小结果、待消费事实与租约；维护前既有审计按固定ID上界比较完整行摘要，允许新增合法审计，原记录不变；本轮已有 System 进程日志原字节前缀保留。随后概览维护仍报告只读。共10项唯一断言通过，真实删除汇总为 `deleted=1004, retention_days=30, budget_exhausted=false`，无额外私有汇总字段。

正式预检确认 System／Gateway 同一固定源码、不同 Build ID和有效源码指纹、干净 checkout及仅回环网络；普通用户唯一角色为 `online.execution_events_retention`，核验 Permission 仅 `monitor.execution.read`。场景内 `cleanup=pending_deployment_destruction` 是中间状态；最终 Host 报告 result／cleanup 为 passed、Infra为 zero_residuals，原始生命周期日志确认应用停止、PostgreSQL容器／数据卷与网络移除，私有凭据目录由既有退出生命周期销毁并核验。部署销毁不代替前述事件删除断言。标准 Infra 同时启动并销毁既有 Alloy／Loki 伴随组件，本场景不查询其数据，不将伴随启动／销毁或本地日志前缀检查计为平台集中日志保留验收。

标准 `make test-execution-retention-online-runner` 在已提交基线 `ecc212b6a6e50a7d5c89bdf0ca409daf662c25da` 加本批14文件的临时源码快照通过：40项Python回归、System身份夹具Go包及Online CI登记成功，14个输入摘要与最终提交逐一一致。Go日志中5项既有PostgreSQL专项因无标准DSN跳过，不计T2；本批未改产品存储，未重复其既有数据库门禁。首次共享工作区登记曾被他方尚未接完的栅格profile阻断，未修复他方代码；首次隔离登记又因两个Job复用一个生命周期文件的计数不匹配失败，现已收敛为同一个人工Job显式选择两场景并通过，保留原失败日志。`changed-gate.py --dry-run` 只计影响选择，包含共享WIP，不计全部受影响模块通过；本轮没有核对其他Owner CI。未涉及API行为或Swagger。

官方 Artifact `12621729322` 仅含5个安全报告／生命周期文件；ZIP SHA-256 `bdc25ea6e072f9f4299f5a93bda2994d118919b5f774e2660a632f4bdd472f07` 已独立匹配GitHub声明。证据为 `/tmp/addp-events-retention-t4-40254456204.zip`、同名提取目录及 `.log`、`-metadata.json`、`-artifacts.json`、`-audit.json`；本地最终日志 `/tmp/addp-event-retention-isolated-corrected.log`、输入回执 `/tmp/addp-events-validation-source.json`、各次日志摘要 `/tmp/addp-events-retention-local-evidence.json`。首次共享／隔离失败分别保存在 `/tmp/addp-event-retention-runner-final.log` 和 `/tmp/addp-event-retention-isolated-final.log`，不改写为通过。

本次仅闭合固定版本、合成历史、真实 System 首轮的30天事件物理维护；精确等于截止点由既有固定时刻Common测试负责，不以31／29天T4样本冒充。未验证小时周期、并发持续Worker写入、Owner执行读取授权或专业结果生命周期、180天概览删除、容量／HA及生产T5；概览删除仍未启用。清单实际第327／328／329行继续部分实现，未操作个人服务、数据库或定时任务。证据归档仅执行 `git diff --check`，不重复仍有效的运行验收。

## 小时周期与连续批次真实进程验收（2026-10-26）

实现提交 `c2f53b007dfaf73960d24c223f4847009edb724a` 仅补人工 `execution-retention-hourly`、既有测试 helper／Hosted 生命周期适配、最小身份夹具及登记／规范，不修改产品的一小时ticker、100个概览种子／1000条事件单批上限、时钟、权限目录或业务功能。共用原维护Job，只有该人工选项为180分钟，其余两个短场景仍60分钟；Online预算8100秒仅传给观察器，驱动绝对观察期限7500秒，每次等待不超过30秒，不增加产品调试开关或夜间调度。120条合成独立执行跨两个非默认Tenant，终态及活动状态各60条；正式AuthContext和构建预检在观察开始时完成，不续期用户凭据、不外推两小时后的用户权限。

[Hosted Run40259324336／Job121368052606](https://github.com/pampa0629/addp/actions/runs/40259324336/job/121368052606)，attempt1、workflow_dispatch、固定上述源码，Run／Job和实际 `Run Hosted execution retention maintenance Online gate` 均completed／success。实际门禁10:33:21–12:44:44（Asia/Shanghai），Job于12:44:48完成；不是仅摘要步骤成功。三轮均为同一个实际System进程来源，保留原自然周期：

| 轮次及实际System时间 | 概览种子／完整树 | 自增ID游标／后页 | 时间候选／读取行／查询 | 本轮过期事件实际删除 |
| --- | --- | --- | --- | --- |
| 启动，10:44:22 | 100／100 | 100／有 | 50／101／102 | 1004 |
| 第一小时，11:44:22 | 20／20 | 120／无 | 10／20／22 | 4 |
| 第二小时，12:44:22 | 100／100 | 100／有 | 50／101／102 | 4 |

System首末记录实际相差7199.984363秒，观察器单调时钟结合初次记录延迟独立核对为7214.018秒，不以本地等待心跳或Job运行时长代替真实周期。观察器自身实际运行7206秒。第二轮到达末尾后，第三轮自然从首批重新评估；100／20／100不能累加成220棵唯一树。初始1004条31天前事件及两组各4条将在约30／90分钟后自然超期的事件均命中对应轮次，不改写时间、不手动清理、不靠重启制造周期。每轮保留240条29天／新事件的完整摘要和120条execution完整行，包括配置、错误、步骤、合成专业小结果、待消费事实和lease；既有审计按固定ID上界保留完整行，允许合法新增审计，本轮原进程日志字节前缀同样逐轮保留。12项唯一断言通过，三个批次均未超预算且概览保持只读；Owner释放和消费者引用未核验计数分别100／20／100，未伪造可删除结论。

正式预检确认System／Gateway同一固定源码、不同Build ID和有效源码指纹、干净checkout、仅回环；普通Tenant User的唯一角色为 `online.execution_retention_hourly`，核验Permission仅 `monitor.execution.read`。内部 `cleanup=pending_deployment_destruction` 是中间状态；最终Host result／cleanup为passed、Infra为zero_residuals，原始生命周期日志确认应用停止、PostgreSQL容器／卷及网络移除，私有凭据按既有退出路径销毁并核验。标准Infra随部署启动／销毁既有Alloy／Loki伴随组件，本场景不查询其数据，不把本地前缀检查或部署销毁计为平台集中日志生命周期验收，也不以销毁代替实际事件删除断言。

标准 `make test-execution-retention-online-runner` 在共享工作区及排除他方WIP的隔离源码均通过：51项Python回归、System身份夹具Go包及Online CI登记成功，5项既有PG专项因无标准DSN跳过，不计T2。隔离基线 `e3d239cacdb8e202dcca241bea6d26a93531e57a` 加本批14文件，其输入摘要与最终提交逐一一致；提交前另一会话仅新增Catalog前端提交，已核对本批输入未变，复用有效门禁。采用仅含本批差异的索引补丁提交，核对工作区字节不变，未提交或覆盖他方Make／workflow／登记改动。最初新增测试在实现缺失时为ModuleNotFoundError红灯，日志保留 `/tmp/addp-hourly-runner-red.log`；最终隔离日志 `/tmp/addp-hourly-runner-isolated.log`，输入回执 `/tmp/addp-hourly-validation-source.json`，本批补丁 `/tmp/addp-hourly-owned.patch`，本地证据摘要 `/tmp/addp-hourly-local-evidence.json`。影响选择dry-run仅计登记覆盖，不把共享WIP涉及的所有模块计为通过；未跟进其他Owner CI。

官方Artifact `12628720268` 仅含5个安全报告／生命周期文件，ZIP SHA-256 `432c4d7463814ebfabbd689626bdc7199d7c5e8d5502e6a0dbbfea39575f7bce` 已独立匹配GitHub声明。证据 `/tmp/addp-hourly-t4-40259324336.zip`、同名提取目录及 `.log`、`-metadata.json`、`-artifacts.json`、`-audit.json`；独立审查同时核对源码、实际门禁、自然时间间隔、单调时钟、分轮事实、构建指纹、身份及最终清理。

本次只闭合固定版本、120条合成历史、同进程两个真实小时区间及连续种子批次／回绕。未验证持续Worker并发写入、重启／故障恢复、超大树或100000条事件维护预算耗尽、用户Owner读取授权及长时授权有效性、Owner释放与专业结果生命周期、180天概览删除、容量／HA或T5；概览删除仍未启用。清单实际327／328／329行维持部分实现，后续建议优先补大量事件分页／截断的真实入口验收，不扩大业务功能。归档仅运行 `git diff --check`，不重复仍有效的运行验收；未操作个人服务、数据库或定时任务。

## 大量事件分页与截断真实入口验收通过（限定范围，2026-10-26）

实现提交 `4a193091a146ef457ce838258dc9482123463c8e` 仅补人工 `execution-events-pagination`、Common测试夹具、最小正式身份、真实Console浏览器入口及标准门禁登记，不修改产品API、采集／展示上限、权限目录或业务功能。新增独立GitHub Hosted Linux Job，使用原标准Infra与应用生命周期；启动System／Gateway／Meta／Monitor／Console，再将普通用户凭据限定传入测试子进程，不注入产品进程。严格守卫独占 `addp_online`、干净已提交源码、当次PostgreSQL容器及仓库外私有目录，拒绝个人环境与混合运行；不操作本地个人服务或数据库。

两组均为绑定真实普通User身份的合成Meta一次性执行，通过Common真实claim／lease及有界事件写入能力准备。第一组尝试追加1005次，预期只存1000条，唯一末条由Common生成 `truncated`，终态仍为success／progress100；第二组准备昨日600条合法历史，再由当日真实lease写入601条，预期共1201条且无采集截断。读取经Gateway／Monitor／真实Meta Owner裁决，API完整走10／13页；真实密码登录的Console／Monitor页面各加载10页到1000条，区分采集截断与展示上限，核对逐页ID／游标、全部DOM投影、刷新／重载／关闭重开回首批100条，以及第二组真实后页仍可读。另覆盖同租户非发起人／跨租户使用已知游标仍404且列表总数为0、非法分页参数400、安全字段及完整执行／事件行读取前后不变。上述全部断言已由后文固定 `fbdd03f5b` 的一次运行完整核验，前两次失败各自保留；不是Meta扫描Worker业务验收，不新增扫描定义或业务引擎。

标准 `make test-execution-events-pagination-runner` 已在共享工作区及排除他方WIP的隔离输入通过：24项Python、2项无外部服务的Go守卫、System身份夹具Go包、浏览器JS语法及Online CI登记成功。专用Hosted Go夹具本地因未设置运行标志跳过1项；5项既有System PostgreSQL专项因无标准DSN跳过，不计T2或真实写入通过。隔离基线 `d6fecb323a8e1e053fc0136585ac8645b2faadd3` 加本批15个文件，其内容摘要与最终提交逐一一致；仅含本批差异的索引补丁提交后，工作区原文件字节保持不变，未提交或覆盖他方WIP。影响选择dry-run仅计登记覆盖，不代表整区模块门禁通过。

[首次Hosted Run40274854201／Job121415332273](https://github.com/pampa0629/addp/actions/runs/40274854201/job/121415332273) 为attempt1、workflow_dispatch，固定上述初始源码；已从早先queued转为completed／failure。实际Online gate失败，预检和上传／汇总成功不能覆盖业务失败。官方Artifact `12631636857` 为51521字节、仅5个安全报告／生命周期文件，ZIP SHA-256 `97b577e326f44265cbc3f61e0f58b5eb83705cd96bb79dfe3c9b6e1c1d4acec8` 已独立匹配GitHub声明及源码。报告为 `online_scenario_failed`、`failure_stage=scenario`，Online分发器总耗时455毫秒、预检177毫秒；四服务构建与checkout一致，原普通User／Tenant2／两项Permission核验成功，随后 `failed_stage=identity`、`checks=[]`，原始日志为 `execution_events_pagination_failed:KeyError`。Common写入、分页／截断、Owner隔离、Console及截图均未执行，不能计为T4通过。外层 `cleanup=passed`、`infra_cleanup=zero_residuals` 单列为生命周期结果，不算业务通过；保留该首次失败，不rerun或取消。

已核明根因：System测试helper创建peer／foreign后，将三项 `ADDP_ONLINE_EVENTS_PEER_ACCESS_TOKEN`、`ADDP_ONLINE_EVENTS_FOREIGN_ACCESS_TOKEN`、`ADDP_ONLINE_EVENTS_FOREIGN_TENANT_ID` 放入内存values，却遗漏于 `writeEnvironmentFile` 的显式导出清单；场景构造身份列表时首先索引缺失的foreign Tenant，直接KeyError。修复提交 `2350759ad8ee1b05e6394626d7be49886e846e05` 只补这三个显式导出项及回归，不泛化导出values，不增权限或修改产品API。新增回归实际调用writer、核对0600、清空继承环境后source文件、运行实际Python场景的身份准备，并模拟HTTP身份观察，在到达fixture边界时停止；核对三主体／Tenant／两项Permission、引号原样传递、未知字段拒绝导出，不在参数／输出保存真实凭据。该确定性测试先红灯、修复后通过，不能替代真实HTTP或Hosted身份验收。

修后 `make test-execution-events-pagination-runner` 在共享工作区及排除他方WIP的隔离输入均通过：24项Python、Common两个无外部服务Go守卫、包含新增交付回归的System身份夹具包、JS语法与Online CI登记成功；专用Hosted夹具本地SKIP1、既有System PG专项无DSN SKIP5，不计T2或真实写入。隔离基线 `148bca32d853171c47f1aa364edc85775d94b5cf` 仅叠加两个自有Go文件，内容摘要与修复提交一致。既有 `test-online-runner`／Platform入口已调用本标准目标，无需新增CI路线；只提交自有路径，未改动他方已暂存或未提交文件。

去重后派发的[第二次Hosted Run40281399550／Job121435288938](https://github.com/pampa0629/addp/actions/runs/40281399550/job/121435288938)，attempt1、workflow_dispatch、固定 `2350759ad8ee1b05e6394626d7be49886e846e05`，最终completed／failure，不能计整场T4通过。身份交付已实际跨过原失败点；Common真实lease写入与success终态、1000／1201条事件的10／13页、ID顺序／无重复、闭合字段／保留窗口、两类上限区分、非法分页400、同租户peer／跨租户已知游标404与列表总数不泄露，七组场景断言通过。浏览器正式登录／同一主体和第一组十页1000行／唯一采集截断两组断言通过；第二组为 `failed_stage=history_pages`，该粗阶段涵盖加载、DOM和辅助后页API，不能从 `history_rows=0` 推断页面实际没有加载任何历史。刷新／重载／重开和最终完整执行／2201事件读取前后不变均未完成，不能计通过。Online分发器总耗时37875毫秒（预检503毫秒），不是完整部署构建耗时。

该次官方Artifact `12632782532` 为179580字节、7个安全文件，ZIP SHA-256 `d42d9c2f994238e43ce66b9bd99b189202f11a4b6582196f5826e153767f452f` 已独立匹配GitHub及四服务固定源码。唯一页面图 `execution-events-quota.png` 已实际目视核对：安全合成执行为成功，末行“采集截断”且计数为空，显示过程不完整提示；第二组截图未产生，不计视觉通过。最终Host业务failed、cleanup passed／Infra zero_residuals分别记录，不以销毁或上传成功覆盖业务失败；不取消或rerun该历史运行。

代码调查发现一个确定的脚本失配：脚本跨整页导航保留登录时API客户端，随后用旧Token读取历史后页；正式Console初始化会话执行refresh，System `token_family_service.go` 的既有轮换路径会撤销旧Access Token。修复 `fbdd03f5b8eb45385b249c89e75087f50299fe40` 仅复用现有 `browserAPIFromResponse`，从最近一页真实请求凭据建立客户端，重新核验同一User／Tenant／Permission再读后页，立即销毁临时客户端；初始登录客户端不跨导航使用。不修改认证或权限语义、不换账号、不补权、不放宽后页断言。新增固定细阶段区分初始化／首批／各页／DOM／上限／后页身份／后页读取／截图，不输出异常正文、凭据或原始响应。原粗阶段报告不足以唯一证明这就是第二次运行的全部失败原因；后续完整通过也不反推该历史失败的唯一根因。

新增单一路径绑定回归先红灯，修后标准 `make test-execution-events-pagination-runner` 在实际Node24.19.0及排除他方WIP的隔离输入通过：25项Python、Common两个Go守卫、System身份夹具包、JS语法和CI登记；专用Hosted夹具SKIP1及既有PG专项SKIP5仍单列，不计真实DB/T2。既有共享浏览器客户端已有轮换／旧Token401／主体和租户改变拒绝的确定性测试，本轮复用实现，未另造认证路径或修改产品前端。只有Online脚本和其回归两文件变化，输入摘要与提交一致。

前一运行已结束、去重确认新SHA无既有manual运行后，只派发一次[第三次Hosted Run40284854056／Job121446007939](https://github.com/pampa0629/addp/actions/runs/40284854056/job/121446007939)，attempt1、workflow_dispatch、精确绑定 `fbdd03f5b8eb45385b249c89e75087f50299fe40`；Run／Job及实际Validate／Run Hosted gate／Upload步骤均completed／success。Job实际15:37:37–15:58:19（Asia/Shanghai）；Online分发器39263毫秒，其中预检236毫秒、场景39026毫秒，不能将其与完整部署构建耗时混用。四个后端均同一固定源码、不同Build ID及有效源码指纹，checkout clean、仅回环，普通Tenant2 User仅持 `meta.scan_task.read`、`monitor.execution.read`；不拼接不同SHA或不同运行的局部结果凑通过。

本次单次完整通过9组场景及7组页面断言：Common真实lease写入／终态；实际1000／1201条的10／13页；全部ID顺序、无重复及闭合字段／30天窗口；采集截断与展示上限区分；非法分页400；同租户非发起人与跨租户已知游标仍404且列表总数0；真实密码Console／Owner授权分页；读取前后两条完整execution及全部2201条事件摘要一致。两条执行均success／progress100。页面分别实际加载十批至1000行，核对逐页ID与完整DOM投影；quota末条为唯一truncated，history无采集截断但has_more=true且真实后页仍可读，均提示记录不完整并隐藏加载更多。刷新／整页重载／关闭重开均返回首批100行，路由保持对应execution，安全投影不输出私有夹具内容；不是仅截图或HTTP状态码断言。

官方Artifact `12634584022`，304600字节，ZIP SHA-256 `7eadfb17a55ed160feca5dbcc1d89a735b1b270a287cc214e6f47696cda4571a` 独立匹配GitHub声明、准确源码及8个安全文件。两张图已实际目视核对：成功执行、quota末行“采集截断”且步骤／计数为空；history末行正常进度，无采集截断；均显示过程不完整提示。1000行总数由实际API ID与DOM逐行断言证明，不以单张截图独立证明。最终Host result／cleanup为passed、Infra zero_residuals；原始生命周期日志核验当次应用停止、容器／卷／网络销毁，私有凭据按标准退出路径销毁并核验。suite内 `pending_deployment_destruction` 是销毁前阶段，不能当作最终未清理，也不以部署销毁代替180天数据删除证据。未重跑或新增Hosted，仅读取已完成运行并独立审计。

归档 `/tmp/addp-event-pages-t4-40284854056.zip` 及同名提取目录，官方元数据 `-metadata.json`／`-artifacts.json`，独立审查 `-audit.json`（含每文件摘要及两图目视范围）；现有正式脚本输入与该SHA逐一核对一致。本轮收口只更新已有证据，`git diff --check`通过；没有代码、API或依赖变化，复用仍有效的本地门禁，不重复T4、其他Owner CI或无关测试。

本地初版证据 `/tmp/addp-event-pages-runner-isolated-final.log`、`/tmp/addp-event-pages-validation-source.json`、`/tmp/addp-event-pages-owned.patch`；最初实现缺失红灯 `/tmp/addp-event-pages-runner-red.log` 保留。首次Hosted归档 `/tmp/addp-ci-20261026-063018/archive-40274854201.zip` 及同名目录、独立官方摘要 `/tmp/addp-event-pages-t4-40274854201-artifact-verified.json`。修后红／绿及隔离日志 `/tmp/addp-event-env-runner-{red,green,isolated}.log`、输入回执 `/tmp/addp-event-env-validation-source.json`。本轮不启用180天概览删除、不修改专业结果期限；更多Owner／专业节点、持续Worker并发、重启／故障、规模容量、Owner释放／消费者引用裁决及T5不在本批通过范围。后续脚本证据为 `/tmp/addp-event-browser-runner-{red,node24,isolated}.log`、`/tmp/addp-event-browser-validation-source.json`；第二次完整归档／独立审查 `/tmp/addp-event-pages-t4-40281399550.zip`、同名目录及 `-audit.json`。清单实际327／328／329行继续部分实现；“大量加载／截断”可从笼统未验改为本限定合成执行、Common真实lease写入、Owner读取与Console页面已验，仍不代表真实扫描Worker或更多Owner／专业节点全链完成。下一步若继续验收，优先讨论持续事件写入时分页／刷新的一致性，独立限定预算和范围；本轮不自动扩展或触发。

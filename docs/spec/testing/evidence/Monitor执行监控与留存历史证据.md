# Monitor 执行监控与留存历史证据

[返回 Monitor 历史证据](Monitor历史证据.md) · [返回验收证据索引](../验收证据索引.md)

本专题收录 continuous、Owner 不可用、租户日志终审、过程事件留存及运行指标统计。验收结论按各记录的固定源码、环境、运行尝试和层级认定。原顺序见[Monitor 历史证据入口](Monitor历史证据.md#原记录入口)。

原始过程、逐步诊断及详细归档清单见[精简前的固定记录](https://github.com/pampa0629/addp/blob/be8f8daa4d38a7830585577f03b77bf2b5ed3931/docs/spec/testing/evidence/Monitor%E6%89%A7%E8%A1%8C%E7%9B%91%E6%8E%A7%E4%B8%8E%E7%95%99%E5%AD%98%E5%8E%86%E5%8F%B2%E8%AF%81%E6%8D%AE.md)。

## 按验收范围查阅

| 验收范围 | 已归档结论 | 直达记录 |
| --- | --- | --- |
| continuous 与 Owner 不可用 | Transfer Hosted 实际覆盖 Owner 503 后清除旧数据、同进程恢复与重新裁决。各轮的分项通过、整轮失败及清理残留分别认定。 | [刷新与撤权](#刷新恢复与撤权) · [Owner 503](#owner-503-与同进程恢复) · [多页与终审](#多页事件与租户日志终审) |
| 租户日志终审 | 固定版本核验两页共四条事件、旧 Token 401、新 Monitor-only 会话的保存游标拒绝、定时扫描隔离和删定义后历史读取。任务清理与独占部署销毁分别记录。 | [正式身份、原 Job 与终审回执](#2026-10-23-固定版本租户日志终审) |
| 180 天概览只读评估 | 本地 T2 验证只读快照。Hosted 首轮在夹具准备失败，修正后由真实 System 首轮识别 14 棵树、5 棵时间候选，执行行与窗口内事件不变。Platform CI 另证确定性门禁。 | [本地 T2](#2026-10-24-概览保留只读评估第一期) · [Hosted 首败与复验](#执行概览只读维护真实进程验收2026-10-25) · [Platform 终态](#固定版本-platform-ci-复核2026-10-25) |
| 30 天过程事件物理清理 | 独立 Hosted 场景验证真实 System 首轮删除 1004 条过期事件，窗口内事件、执行行与既有审计保留。精确截止点由 Common 固定时刻测试覆盖。 | [首轮删除断言与清理回执](#30-天过程事件清理真实进程验收2026-10-26) |
| 小时周期与连续批次 | 独立 Hosted 场景验证同一 System 进程的两个自然小时区间、概览只读游标回绕，以及三轮分别删除 1004／4／4 条过期事件。持续 Worker 并发、重启恢复和容量未覆盖。 | [逐轮实测与未覆盖项](#小时周期与连续批次真实进程验收2026-10-26) |
| 大量事件分页与截断 | 两次 Hosted 失败各自保留。第三次固定版本通过，覆盖 1000／1201 条事件的 API 分页、Console 展示上限、采集截断及 Owner 读取裁决。该范围限定为合成 Meta 执行。 | [三次运行、页面与清理证据](#大量事件分页与截断真实入口验收通过限定范围2026-10-26) |
| 持续写入期间续页与刷新 | 首轮 Hosted 的整引擎扫描被拒绝。复验通过准备扫描与读取拒绝断言，随后停在目录基线读取。两轮均未执行新增续页、刷新与终态断言。 | [首轮](#首轮整引擎扫描被拒绝) · [复验目录基线读取失败](#复验目录基线读取失败) · [本地回归与未覆盖项](#本地回归与未覆盖项) |
| 用户重试与自动恢复统计 | 本地 PostgreSQL 对照复现旧 SQL 误计，修正后分组计数通过。 | [修正源码、首败与复验](#用户重试与自动恢复统计2026-11-10) |

上表的租户日志终审记录固定版本的两页四条读取与撤权后旧游标拒绝。现行场景另要求在 Kafka 任务持续写入期间沿原游标续读并核对 Monitor 与 Owner 的事件顺序，在 Console 核对自动续页、手动刷新和 Worker 失联后原执行的失败诊断及唯一终态事件。新增断言按对应源码的实际 Hosted 回执认定，确定性 runner 与真实拓扑结果分别记录。[现行 Transfer continuous 验收场景](../scenarios/同步与引擎消费.md#transfer-continuous-events)

180 天概览整树物理删除、Owner 释放和消费者引用裁决均未在上述场景验收。独占部署销毁仅计退出清理。现行规则见[通用执行读取与过程事件](../../tasks/执行监控.md#通用执行读取与过程事件)及[概览保留与清理契约](../../tasks/执行监控.md#概览保留与清理契约)。

## 用户重试与自动恢复统计（2026-11-10）

`0a5846093a40bcf9ea8907e7b3129869668ad851` 包含聚合修正及 `TestIntegrationPostgresExecutionRuntimeMetricsRetryAndRecovery`。[运行指标口径](../../tasks/执行监控.md#运行指标口径)

- **本地首败**。旧 SQL 下，continuous 组的用户重试为 1，预期为 0，恢复数为 1。
- **本地复验**。发布前的 Monitor PostgreSQL 门禁 47 项通过、0 跳过。受限 Transfer 读取范围内，bounded 组的自动 attempt 重试、用户重试各 1，恢复为 0。continuous 组的恢复为 1，两类重试均为 0。用例按租户和五条夹具执行 ID 清理，无删除错误。
- **Linux CI**。固定源码 `0a5846093a40bcf9ea8907e7b3129869668ad851` 的 attempt 1 中，`make test-monitor-postgres` 和新增对照用例均通过。[Run 42788220035／Job 129740158121](https://github.com/pampa0629/addp/actions/runs/42788220035/job/129740158121)
- **覆盖边界**。上述结果覆盖合成执行记录的 PostgreSQL 聚合。真实 System → Gateway → Owner 统计链路、页面刷新、并发和规模均未验证，Hosted T4 未运行。

## 持续写入期间续页与刷新首次 Hosted（2026-11-07）

本节收录两次 Hosted 尝试及后续本地回归。场景与身份要求见[现行 Transfer continuous 验收场景](../scenarios/同步与引擎消费.md#transfer-continuous-events)。

### 首轮：整引擎扫描被拒绝

[Run 42246891848／Job 127991351661](https://github.com/pampa0629/addp/actions/runs/42246891848/job/127991351661) attempt 1 使用固定源码 `566eb1f2526a9e3877db326a8eff8e2d5bba0c13`，干净检出，预检通过。

- **首败**。普通消费 User 有扫描功能权限，夹具未为其建立该 PostgreSQL 引擎的管理范围。按请求 ID `b461a5c1-998b-41ff-bcbb-a884aab659d4` 核对，System AuthContext 返回 200，System `POST /engine-access/metadata-refresh-scopes/query` 返回 403，Meta `POST /scan/run/manual` 与 Gateway 同样返回 403。套件停在 `source_scan`，`checks`、`resources`、`executions` 均为空，原报告为 `uncertain_mutation=true`。
- **清理与归档**。套件清理 `passed`，领域残留 0，外层 `cleanup=passed`、`infra_cleanup=zero_residuals`。归档 `13523751222` 的 ZIP SHA-256 与 GitHub 声明一致，为 `39814a3b62867e9ddd2c248e7b6976e7f34db18ec507f5fefd106ce495bf6001`。

### 复验：目录基线读取失败

2026-11-07 获准仅修隔离验收夹具并复验一次。修订 `ee6d3032578b0e0428bfa6293986838843a3449c` 由 System-owned helper 准备独立的 `tenant.engine_access_delegation_administrator`，仅增加扫描执行／读取两项测试功能权限，Token 与六个业务／控制身份隔离。普通消费 User 保留原十项功能权限。消费与审批身份读取准备扫描均须返回 404，CDC 历史发起人与审批扫描归属仍分别验证。

[Run 42364909753／Job 128380830423](https://github.com/pampa0629/addp/actions/runs/42364909753/job/128380830423) attempt 1 使用固定源码 `80b9369963941456b9c9a3e82e2e26d7d6d0f82c`，干净检出，准入通过。该源码比夹具修订仅增加 Monitor 指标验收文件。

- **已执行断言**。独立准备 User 38 提交扫描返回 201，扫描达到 success，发起人绑定通过。普通消费用户和审批用户各收到 404，`source_scan_preparation` 的两项拒绝证明均为真。扫描 execution 为 `7811a757-2c06-4733-83e9-ceee8fa11f1e`，`checks` 仅含 `formal_source_catalog_scan`。
- **首败**。上述断言完成后，普通用户 `GET /api/v1/meta/engines/:engine_id/items` 返回 200，夹具在目录基线读取期间停止，整套为 `source_scan/request_or_contract_failed`。归档没有响应正文或更细的异常原因，无法区分空列表、字段契约和身份校验。后续源码确认 `ListEngineItems` 经 `FilterMetadataItems` 按当前 `metadata_read`／`read` 裁决。夹具没有为普通消费用户建立目标资源 Grant，却用该用户的可见列表建立物理基线。这个缺口来自源码核对，当次归档不足以确认它是唯一根因。
- **清理与归档**。原报告为 `uncertain_mutation=false`，套件 `cleanup=passed`、领域残留 0，外层 `cleanup=passed`、`infra_cleanup=zero_residuals`。归档 `13571857121` 的 ZIP SHA-256 与 GitHub 声明一致，为 `fa628f6f900640098330ef81b997e6a6e15b5342fedcde29a596be89fabbfcd8`。已使用当次获准的一次复验。

### 本地回归与未覆盖项

标准入口为 `make test-transfer-continuous-online-runner`。验收变更 `368112401733a105bf15b3085354c726772d3d53` 通过 61 项 Python 测试，准备身份修订通过 88 项，后续补齐目录基线诊断后通过 90 项。各阶段的 JavaScript／Bash 语法检查及 System 身份夹具 Go 包均通过。

后续夹具单列 `source_catalog` 阶段和 `source_catalog_baseline` 诊断。报告仅记录请求／列表／身份核验阶段及可见项、目标匹配数量，区分缺失、重复和身份不符。回归验证空可见列表拒绝建立物理基线及非法身份的安全诊断，归档排除目录正文、任意 metadata 和凭据。该批没有新增 Hosted 运行。

2026-11-10 已确认后续夹具在同一独占 Hosted PostgreSQL 只读快照中核对本轮 Tenant、Engine 下四张固定表及对照表祖先。扫描前后的表与节点事实保留原断言。准备扫描的执行归属与可见性、租户日志读取及撤权仍由原身份通过公开接口验证。本地专项通过 95 项 Python 测试、JavaScript／Bash 语法检查及 System 身份夹具 Go 测试。新观察查询在独占 Hosted PostgreSQL 的实际结果仍待复验。

两次 Hosted 均未创建 Kafka 任务，因此同一任务写入前保存游标、写入后沿原游标续页、Monitor 与 Owner 完整事件顺序比对、Console 自动续页与手动刷新、Worker 失联后同页的失败诊断和唯一终态均未执行。准备扫描复验通过及零残留只覆盖各自的断言。[旧版两页四条事件与撤权证据](#2026-10-23-固定版本租户日志终审)

### 2026-11-10 固定源码复验

[Run 42805790493／Job 129794444325](https://github.com/pampa0629/addp/actions/runs/42805790493/job/129794444325) attempt 1 使用干净检出的 `a8b16b39507a0e11b8352735bcf21125281c5d13`。预检通过，System、Gateway、Meta、Transfer、Monitor、Security 的构建源码均与该提交一致。整套结论为失败。

- **通过**。独立准备 User 的扫描成功，普通消费与审批 User 各自读该执行返回 404。PostgreSQL 只读快照核实一张对照表和两级祖先，随后通过 Kafka 与 CDC 首次深扫描取证。Kafka 真实持续写入期间沿原游标续页，与 Owner 完整事件顺序相符。四份 Console 回执均通过，覆盖自动续页、手动刷新、原页面唯一终态、Owner 真实 503 后同进程恢复及撤权。Worker SIGKILL、租约过期和替代进程从已提交位置恢复通过。
- **首败**。CDC 增量审批读取与提交均返回 200。Transfer 在审批后领取 Meta 扫描时记录 `schema change request conflict`，随后回滚 claim。归档中的来源时间小数秒为 `.299069081`，精度高于 PostgreSQL 时间列，源码对重新读出的 `applied_at` 和该来源时间做精确比较。这个精度差能解释冲突，仍待真实 PostgreSQL 回归复现。CDC 审批扫描、重放审批、恢复后的写入和 bounded 定时任务断言未完成。
- **清理**。CDC 定义删除后读回 404。Kafka 定义删除持续轮询运行会话，约 10 秒后 Transfer 返回 503、Gateway 返回 502（请求 `367da1bb-d758-4a8e-ac73-2c83e16c2458`）。业务清理失败，隔离部署销毁前剩余一项。外层销毁通过，Infra 报告零残留。归档中旧 Kafka 执行有会话插入记录，未见退出确认。替代执行和其余两条执行均有退出确认 SQL。删除期间针对该 Kafka 任务轮询了 40 次未确认退出的会话，与当前要求所有历史会话确认退出的裁决一致。
- **归档**。证据归档 `13761547686`，独立核对 ZIP SHA-256 与 GitHub 声明一致，为 `4a0479d5c4c6c6a176d3260d808a408e16f4f473bf9c1da9bab51b6b321eab59`。

## 2026-10-24 概览保留只读评估第一期

Common 的 `AssessRetention` 由 System 既有维护循环调用，以数据库时间和只读、可重复读事务评估 180 天时间候选。首期检查完整执行树、租约、关系异常、树外引用及预算，保留 Owner 释放和消费者引用未核验原因。批次计数按当次读取范围解释，不能累加为全库唯一树数。[现行留存契约](../../tasks/执行监控.md#概览保留与清理契约)

- **本地首败**。首次 T2 有三处夹具断言失败，分别涉及共享时间指针被后续更新改写、时间精度和注入钩子时机。另一次 System Go 测试通过时 Common 测试文件发生变化，该结果未纳入最终证据。
- **同一稳定输入下复验**。`make test-module MODULE=common GO_ONLY=1`、`make test-module MODULE=system GO_ONLY=1` 均通过。`make test-common-postgres COMMON_POSTGRES_TEST_ARGS="--test execution-retention"` 使用当时 Infra 映射端口 25432 和 `addp_test`，11 组 T2 通过。覆盖多层树与子种子、年轻子执行、跨租户父子及反向引用、环与断链、恢复引用、非法 JSON 来源、查询／行／树预算、查询失败不发布部分报告、并发新增子执行的一致快照、取消及拒绝继承可写事务。Common PostgreSQL 并发锁在数据库测试启动前拒绝第二次运行。该专项由既有 Common PostgreSQL CI 登记。
- **数据与清理**。执行行、专业配置／结果／错误及过期过程事件前后不变，夹具逐项清理后零残留。该批未运行工作区整体 `make test-changed`、完整 Common PostgreSQL 聚合或真实维护 T4。真实维护另见[Hosted 首败与复验](#执行概览只读维护真实进程验收2026-10-25)。

## continuous 诊断与 Owner 不可用

按[刷新、恢复与撤权](#刷新恢复与撤权)、[Owner 503 与同进程恢复](#owner-503-与同进程恢复)和[多页事件与租户日志终审](#多页事件与租户日志终审)查阅。各轮只认定实际执行的断言，验收方法沿 [transfer-continuous-events 场景](../scenarios/同步与引擎消费.md#transfer-continuous-events)，确定性入口为 `make test-transfer-continuous-online-runner`。

### 刷新、恢复与撤权

| Hosted Run／源码 | 已执行结果与整轮结论 | 清理与归档 |
| --- | --- | --- |
| [38329160596](https://github.com/pampa0629/addp/actions/runs/38329160596) · `bfc305953` | **失败**。真实自动刷新、恢复来源和稳定码通过。撤权只清事件，终态概览和步骤仍保留。Monitor-only 与后续 CDC 未运行。 | 清理通过，残留 0。 |
| [38337857450](https://github.com/pampa0629/addp/actions/runs/38337857450) · `b0765f802` | **失败**。live／recovery 与旧内容清空通过。重新登录后的 Monitor-only 拒绝提示断言失败，`StatusAnnouncer` 与 `el-result` 同文案触发 strict-mode 双匹配。 | 清理通过，Infra 零残留。 |
| [38381655995](https://github.com/pampa0629/addp/actions/runs/38381655995) · `65de8a82938ba1bbdd56df6f7c000be7f67076cc`（含 `ba2c99652`） | **完整 Hosted T4 通过**。live／recovery／revocation 三份报告与九组接口和 Worker 检查通过，覆盖失联后新 UUID、暂停／恢复、跨租户及同租户 Owner 权限、Owner 不可用、撤权与新会话拒绝、CDC 事件、删定义后历史读取。Workflow／Notebook 监督器与生产 T5 未覆盖。 | 五模块源码一致，37 份归档无私有控制文件或访问令牌／JWT 模式，领域及 Infra 零残留。 |

本地验证单列。首轮修正后前端 56 项单元、118 项浏览器及构建通过，401 断言核对会话失效导航和旧内容全清。第二轮修正只定位唯一可见标题，保留播报及详情、步骤、事件、树均不展示的断言。最终修订的前端 59 项单元、131 项浏览器及构建通过，Transfer runner 65 项通过。

### Owner 503 与同进程恢复

故障夹具使用正式普通 User，只停止当次隔离部署中经 pidfd、可执行文件、UID、数据库和私有目录核验的 Transfer Backend，再恢复同一 PID。浏览器核对终态详情、运行树和非空列表的清空与重新裁决。独立网络中断仅影响事件读取。控制文件留在当次私有目录，公开回执不含 Token、业务样本或完整接口响应。

| Hosted Run／源码 | 已执行结果与整轮结论 | 清理与归档 |
| --- | --- | --- |
| [39026543398](https://github.com/pampa0629/addp/actions/runs/39026543398) · 原记录未单列 SHA | **失败**。隔离入口漏启 Security，Owner 故障阶段未运行。后续预检 Security，并只读等待独占部署的 Transfer projection initialized，不写保护事实或连接本地共享业务库。 | 清理通过，Infra 零残留。 |
| [39082349255](https://github.com/pampa0629/addp/actions/runs/39082349255) · `009ceea39` | **失败**。Owner 503 八项页面检查和同进程停止／恢复通过。后续断言误要求运行中 CDC 存在 running 终态事件，CDC 后续、审批与调度未运行。修正要求运行中 CDC 无终态，终态执行恰有一个匹配终态事件。 | 保留三张截图，领域及 Infra 清理通过。 |
| [39083559464](https://github.com/pampa0629/addp/actions/runs/39083559464) · `946c4d13e` | **完整 Hosted T4 通过**。14 项业务检查含 Owner 503 八项页面检查，同 PID 停止与恢复证明为真。覆盖撤权／Monitor-only、Kafka 恢复、CDC 审批与双向扫描隔离、调度当前主体及删定义后历史读取。Notebook、其他 Owner 与生产 T5 未覆盖。 | 归档 `12121475487` 的三张截图已目视核验。产品历史保留，业务残留 0，`cleanup=passed`，外层 Infra `zero_residuals`。 |

### 多页事件与租户日志终审

| Hosted Run／源码 | 已执行结果与整轮结论 | 清理与归档 |
| --- | --- | --- |
| [39905778336／Job 120257300658](https://github.com/pampa0629/addp/actions/runs/39905778336/job/120257300658) · `5be2c2e0e9c6fb1f49da54a076753f2568462fd4` | **失败**。14 项检查及四份浏览器分项报告通过，包含多页、保存游标撤权及不可见总数。旧验收白名单拒绝 Transfer 已声明的 `initial_authorization.status/code`，成功的定时执行在校验及清理时读取历史被拒绝。 | task 3 未完成删除，领域 `cleanup=failed`、残留 1。外层部署销毁后，领域清理结论仍为失败。 |
| [39923509570／Job 120314175151](https://github.com/pampa0629/addp/actions/runs/39923509570/job/120314175151) · `9e67500b26136ef909086d70ee9536d925ff3581` | **完整 Hosted T4 通过**。六服务同提交，176.851 秒，16 项唯一检查和四份真实浏览器报告通过，补齐保存游标撤权、不可见总数、定时扫描、父子读取隔离与删定义后历史读取。身份、报告与后续未覆盖项见[固定版本租户日志终审](#2026-10-23-固定版本租户日志终审)。 | 官方归档 `12493568384`。任务 3／2／1 均 `verified_absent`，领域残留 0，无不确定写入，部署清理及 Infra 零残留。 |

## 2026-10-23 固定版本租户日志终审

对 `39923509570` 的官方归档及原始 Job 日志完成独立核验。归档 SHA-256 为 `5c32cadccc438db1e7c1e16cd5edd9056c0169c1a87fa75dbeb2efe57902be2d`；原始标准输出中的 suite JSON、`transfer-continuous-events.json` 与外层 `suite_report` 完全一致，源码干净且 System、Gateway、Meta、Transfer、Monitor、Security 六服务均为上述完整提交。四份浏览器报告逐一核对本轮 Run ID、Tenant、Principal、execution UUID 及各自的闭合检查集合，Owner 不可用详情清空及恢复诊断截图已目视复核，恢复来源绑定以本轮报告和 DOM 断言为据。既有标准入口仍为 `make test-online ONLINE_SUITE=transfer-continuous-events`，由原 Hosted 生命周期执行；修复提交的 `make test-transfer-continuous-online-runner` 已通过 57 项确定性回归及 System 身份夹具 Go 测试，本次终审没有重跑或再次触发。

同一 Tenant 2 普通读取 User 37 在 Owner 与 Monitor 实际读取每页 2 条、共 2 页 4 条事件；正式撤权后旧 Token 为 401，重新登录的同一 Monitor-only User 使用保存的第一页游标仍被 Owner 403／Monitor 404 拒绝，执行列表总数为 0。同租户无 Owner 权限及跨租户身份也返回空列表与零总数。真实 Owner 进程停止／同 PID 恢复、详情／树／步骤／事件与列表旧内容清空、独立事件网络故障，以及 Worker SIGKILL 后换 PID、租约收敛及新 execution 恢复均通过。Kafka、CDC 独立审批人及精确扫描边界沿原断言保持。

此前未达到的定时子阶段本次完整通过：普通 User 33 的 `scheduled` 执行、实际父／Meta scan 绑定、peer 可读父而 scan 为 404、物理行集、调度暂停及目标精确扫描均有报告证据。最后的 `deleted_definition_history_retained_with_owner_permissions` 实际执行。逐项清理回执为 scheduled 3、CDC 2、Kafka 1 全部 `verified_absent`，原始 Backend 日志有三次 DELETE 200 及各自随后 GET 404；suite 的 `cleanup=passed`、`residual_resources=0`、`uncertain_mutation=false` 与外层部署清理、Infra 零残留分别核验。

该通过只覆盖固定版本、此套件的真实身份与拓扑，不外推当前后续 HEAD、其他 Owner、Security 日志专项、Notebook 运行许可、容量、长时间运行、HA 或生产 T5。API 每页 2 条的多页证据不代表超过 100 条的浏览器加载更多、每日 1000 条截断或完整节点历史已验。`history_cleanup=pending_deployment_destruction` 表示本轮保留历史后随独占部署销毁，不能写成执行历史留存清理已验；30 天事件物理维护周期及 180 天概览整树安全删除仍未由此 T4 证明。旧 `5be2c2e0e` 失败保留，不由此次通过覆盖。

## 执行概览只读维护真实进程验收（2026-10-25）

人工 Hosted Linux 场景 `execution-retention-readonly` 使用独占 System／Gateway／Infra。System-owned helper 经正式 Tenant Service 创建两个非默认 Tenant，普通测试 User 仅获既有 `monitor.execution.read`。经实际 AuthContext 核验后，受限 Common 历史 helper 准备合成记录。只有当次独占 System 按标准生命周期启停，由产品维护首轮执行评估。

### Hosted 首败：历史夹具未准备成功

[Run 40138788559／Job 121005906648](https://github.com/pampa0629/addp/actions/runs/40138788559/job/121005906648) 固定源码 `2a3cbb1f677af23f955ff02038205d525aafc93d`，在历史准备阶段失败，14 棵树的维护断言未运行。源码核对发现夹具使用了 `chk_task_executions_status` 禁止的 `unknown_fixture`。当次日志仅保留 `prepare:CalledProcessError`，没有 SQLSTATE。

应用及凭据清理为 `passed`，Infra 为 `zero_residuals`。官方归档 `12577428981` 的 ZIP SHA-256 已独立核对，为 `1a4fe210c3d121de2f13e977751df08dfe51f1b47121d105aa87a2527843b9b8`。原始日志和提取报告在 `/tmp/addp-retention-t4-40138788559.log`、`/tmp/addp-retention-t4-40138788559/`，审查回执为 `/tmp/addp-retention-readonly-t4-receipt-20261025.json`。

### 夹具修正与本地回归

`86f58b3764c4c3e47af1a141d81e21f94109150c` 改用合法 `pending` 夹具，增加存储 CHECK 与各夹具状态的交叉回归。未知状态防御仍由 Common 单元测试覆盖，生产约束保持。测试驱动仅记录 SQLSTATE 和白名单计数，归档排除原始 SQL、私有字段和任意保留原因内容。

`make test-execution-retention-online-runner` 通过 28 项 Python 回归及 System `cmd/online-test-fixture` Go T1，`TestExecutionRetentionFixtureHasOnlyMonitorReadPermission` 明确通过。覆盖只读汇总与计数校验、整行／事件变化拒绝、容器身份拒绝、初次日志落盘等待，以及准备失败、业务失败与清理残留。五项既有 PostgreSQL 专项因缺少标准 DSN 跳过，分别涉及三员、Catalog 授权 observation、PG 授权成员、Metrics 与 Raster Policy。日志为 `/tmp/addp-retention-t4-runner-final.log`。共享工作区全库注册检查曾受并行 Graph 契约改动阻断，登记的干净输入证据见[固定版本 Platform CI](#固定版本-platform-ci-复核2026-10-25)。

### Hosted 复验：真实 System 首轮通过

[Run 40140020076／Job 121009605836](https://github.com/pampa0629/addp/actions/runs/40140020076/job/121009605836) attempt 1 固定源码 `86f58b3764c4c3e47af1a141d81e21f94109150c`。Run、Job 与真实运行步骤均为 `success`，独立审查通过。System／Gateway 预检使用同一固定源码及各自 Build ID／源码指纹，工作区干净，网络仅回环。普通 Tenant User 仅持有 `online.execution_retention_readonly` 角色，验证的 Permission 为 `monitor.execution.read`。

- **只读结果**。10 项唯一断言通过。16 个种子形成 14 棵完整树、5 棵时间候选，读取 24 行、查询 20 次，游标 16，无后页，未超预算。覆盖完整超期树、年轻子执行、`running`／`pending`、有效租约、完成时间缺失、缺父／跨租户／自指及树外 retry／recovery 引用。
- **保留原因**。Owner 释放和消费者引用均有 14 棵未核验。其他原因分别为活动执行 2、完成时间缺失 3、有效租约 2、未超期 1、关系异常 4、树外引用 2。执行完整行和 30 天窗口内事件的数据库摘要均保持一致，汇总多余字段数为 0。
- **清理与归档**。套件结束时为 `cleanup=pending_deployment_destruction`。最终 Host `summary.txt` 确认结果与清理均为 `passed`、Infra 为 `zero_residuals`，应用停止，测试卷、网络和凭据均已销毁。官方归档 `12579077155` 的五个文件仅含安全报告及生命周期日志，无凭据目录或原始 System 进程日志。ZIP SHA-256 独立匹配上传声明 `eaa740d8a10f7a767b66f266b2dc51cb3b2c2f2d0f08457cadc2a4b2807c7c2c`。本地留档为 `/tmp/addp-retention-t4-40140020076.zip`、同名提取目录和 `.log`、`/tmp/addp-retention-t4-40140020076-run.json` 及 `/tmp/addp-retention-readonly-t4-rerun-receipt-20261025.json`。

固定源码与后续证据提交之间，Common execution、System 维护及身份夹具、测试驱动、Hosted 路径与 workflow 均未变化。后续只追加证据与 Make 的 Go 结果显式输出，沿用本次 T4。

**覆盖边界**。时间候选只表示满足时间条件。退出销毁只计部署清理。180 天概览物理删除、Owner 释放、消费者引用裁决、用户执行读取授权、专业结果生命周期、30 天事件物理维护、小时周期、容量／HA 和生产 T5 均未由本批验收。原记录中的清单第 327／328／329 行仍为部分实现。

### 固定版本 Platform CI 复核（2026-10-25）

北京时间 07:17 观察 [Platform CI Run 40139997741](https://github.com/pampa0629/addp/actions/runs/40139997741) 时仍在排队。[Platform consistency Job 121009537951](https://github.com/pampa0629/addp/actions/runs/40139997741/job/121009537951) 随后在同一固定源码 `86f58b3764c4c3e47af1a141d81e21f94109150c` 的 push attempt 1 中实际执行 `make test-platform`。门禁于 07:19:06–07:41:06 执行，Job 于 07:41:09 完成，Run、Job 与 `Run platform consistency gate` 均为 `success`。23:58 独立复核终态，沿用原运行。

原始日志第 2972–2997 行对应 `test-platform → test-online-runner → test-execution-retention-online-runner` 的第一子入口。28 项 Python 回归通过，失败和跳过均为 0，System `cmd/online-test-fixture` 返回 `ok`。登记检查实际输出 `Online CI registration is consistent`，覆盖干净 CI 中 runner 的执行及唯一人工 Hosted profile 的登记。

证据为 `/tmp/addp-retention-platform-40139997741-metadata.json`、`-attempt.json`、`-audit.json` 及原始 `.log`，日志 SHA-256 为 `0ca82b64f5dafd79eba1021c3c1962392b25cf9374f1e81f20662bff27ca1e45`。本地五项 PostgreSQL 跳过和共享工作区注册阻断分别保留。CI 结果仅认定该固定版本的确定性门禁，真实维护与未验收项沿上面的 Hosted 记录。

## 30 天过程事件清理真实进程验收（2026-10-26）

`8e84cc8a69eac172bcd74639803c7450b55ce2d3` 增加人工 `execution-events-retention` 场景。它与 `execution-retention-readonly` 共用人工 Job，分别验收物理清理和事件保持不变。两场景都未登记夜间调度，产品清理逻辑、权限和专业结果期限保持原规则。

### System 首轮实际删除

[Hosted Run 40254456204／Job 121353216036](https://github.com/pampa0629/addp/actions/runs/40254456204/job/121353216036) 使用上述固定源码，`workflow_dispatch` attempt 1。Run、Job 和 `Run Hosted execution retention maintenance Online gate` 均为 `success`，实际门禁时间为北京时间 09:30:52–09:42:25。

- **身份与数据**。System-owned helper 经正式 Tenant Service 准备两个非默认租户。普通 User 的唯一角色为 `online.execution_events_retention`，实际 AuthContext 核验的 Permission 仅为 `monitor.execution.read`。按标准生命周期停止本轮独占 System 后，由核验容器身份的受限 helper 写入合成历史，每租户各一条终态和活动执行。实际删除由产品维护首轮执行。
- **删除结果**。10 项唯一断言通过。真实 System 首轮分批删除 1004 条 31 天前事件，跨过 1000 条单批上限。真实汇总为 `deleted=1004, retention_days=30, budget_exhausted=false`，无额外私有字段。后续概览维护仍为只读。
- **保留结果**。8 条 29 天及新事件、4 条 execution 的完整行摘要不变。执行行包含冻结配置、私有错误、步骤、合成专业小结果、待消费事实和租约。既有审计按固定 ID 上界比较完整行，允许新增合法审计。System 原进程日志字节前缀保留。

### 独占部署与退出清理

两次物理清理验收的正式预检均确认 System／Gateway 使用对应的固定源码、不同 Build ID、有效源码指纹、干净检出和仅回环网络。场景内的 `cleanup=pending_deployment_destruction` 为中间状态，两次最终 Host 报告均为 result／cleanup `passed`、Infra `zero_residuals`。原始生命周期日志核实应用停止、PostgreSQL 容器／卷及网络移除、私有凭据目录销毁。部署销毁只计退出清理。标准 Infra 的 Alloy／Loki 随部署启动和销毁，两场景均未查询其数据，进程日志前缀检查也未覆盖平台集中日志保留。

本轮官方归档 `12621729322` 含 5 个安全报告／生命周期文件。ZIP SHA-256 已独立匹配 GitHub 声明 `bdc25ea6e072f9f4299f5a93bda2994d118919b5f774e2660a632f4bdd472f07`。原始证据为 `/tmp/addp-events-retention-t4-40254456204.zip`、同名提取目录及 `.log`、`-metadata.json`、`-artifacts.json`、`-audit.json`。

### 首轮场景的本地回归与边界

`make test-execution-retention-online-runner` 在已提交基线 `ecc212b6a6e50a7d5c89bdf0ca409daf662c25da` 加本批 14 个文件的临时快照中通过 40 项 Python 回归、System 身份夹具 Go 包和 Online CI 登记，14 个输入摘要与最终提交一致。五项既有 PostgreSQL 专项因缺少标准 DSN 跳过，既有产品数据库门禁未重跑。

首次共享登记因他方栅格 profile 未接完而阻断，首次隔离登记因两个 Job 复用一个生命周期文件而计数不符。修正为同一人工 Job 显式选择两场景后通过。原失败日志分别为 `/tmp/addp-event-retention-runner-final.log` 和 `/tmp/addp-event-retention-isolated-final.log`，最终日志为 `/tmp/addp-event-retention-isolated-corrected.log`，输入回执与日志摘要为 `/tmp/addp-events-validation-source.json`、`/tmp/addp-events-retention-local-evidence.json`。影响选择 dry-run 仅核对选择范围。

**覆盖边界**。31／29 天样本只覆盖首轮删除与窗口内保留，精确等于截止点由既有 Common 固定时刻测试覆盖。该轮未验小时周期、持续 Worker 并发写入、Owner 读取授权与专业结果生命周期、180 天概览删除、容量／HA 或生产 T5。原记录中的清单第 327／328／329 行仍为部分实现。

## 小时周期与连续批次真实进程验收（2026-10-26）

`c2f53b007dfaf73960d24c223f4847009edb724a` 补充人工 `execution-retention-hourly` 场景。产品继续使用原一小时周期和每批 100 个概览种子／1000 条事件的上限。沿用人工维护 Job，小时场景的 Job 超时为 180 分钟，两个短场景仍为 60 分钟。Online 的 8100 秒预算仅传给观察器，驱动的绝对观察期限为 7500 秒，单次等待最多 30 秒。

### 同一 System 进程的两个自然小时

[Hosted Run 40259324336／Job 121368052606](https://github.com/pampa0629/addp/actions/runs/40259324336/job/121368052606) 使用上述固定源码，`workflow_dispatch` attempt 1。Run、Job 和 `Run Hosted execution retention maintenance Online gate` 均为 `success`。实际门禁为北京时间 10:33:21–12:44:44，Job 于 12:44:48 完成。

120 条合成执行分布在两个非默认 Tenant，终态和活动状态各 60 条。普通 User 的唯一角色为 `online.execution_retention_hourly`，观察前经实际 AuthContext 核验既有 `monitor.execution.read`。观察期间未续期用户凭据。

| 轮次及实际 System 时间 | 概览种子／完整树 | 自增 ID 游标／后页 | 时间候选／读取行／查询 | 过期事件实际删除 |
| --- | --- | --- | --- | --- |
| 启动，10:44:22 | 100／100 | 100／有 | 50／101／102 | 1004 |
| 第一小时，11:44:22 | 20／20 | 120／无 | 10／20／22 | 4 |
| 第二小时，12:44:22 | 100／100 | 100／有 | 50／101／102 | 4 |

- **自然周期**。System 首末记录相差 7199.984363 秒，观察器用单调时钟和初次记录延迟独立核对为 7214.018 秒，观察器自身运行 7206 秒。第二轮到达末尾，第三轮自然回到首批，100／20／100 的批次计数不能累加为唯一树数。最初的 1004 条为 31 天前事件，后两组各 4 条分别在约 30／90 分钟后自然超期。期间未改写时间、手动清理或重启 System。
- **断言与保留**。12 项唯一断言通过，三批均未超预算，概览保持只读。每轮均保留 240 条 29 天／新事件及 120 条 execution 的完整摘要，执行行保留配置、错误、步骤、合成专业小结果、待消费事实和租约。既有审计按固定 ID 上界核对，允许合法新增审计，原 System 日志字节前缀逐轮保留。Owner 释放与消费者引用未核验的计数均为 100／20／100。
- **退出清理**。当次 Host 清理与 Infra 零残留见[独占部署与退出清理](#独占部署与退出清理)。归档 `12628720268` 含 5 个安全报告／生命周期文件，ZIP SHA-256 独立匹配 GitHub 声明 `432c4d7463814ebfabbd689626bdc7199d7c5e8d5502e6a0dbbfea39575f7bce`。原始证据为 `/tmp/addp-hourly-t4-40259324336.zip`、同名提取目录及 `.log`、`-metadata.json`、`-artifacts.json`、`-audit.json`。

### 小时场景的本地回归与边界

新增测试首败为实现缺失导致的 `ModuleNotFoundError`，见 `/tmp/addp-hourly-runner-red.log`。随后 `make test-execution-retention-online-runner` 在共享工作区和隔离源码均通过 51 项 Python 回归、System 身份夹具 Go 包及 Online CI 登记。五项既有 PostgreSQL 专项因缺少标准 DSN 跳过。隔离基线 `e3d239cacdb8e202dcca241bea6d26a93531e57a` 加本批 14 个文件，输入摘要与最终提交一致。期间新增的 Catalog 前端提交未改变本批输入。最终隔离日志、输入回执、提交补丁和本地证据摘要分别为 `/tmp/addp-hourly-runner-isolated.log`、`/tmp/addp-hourly-validation-source.json`、`/tmp/addp-hourly-owned.patch`、`/tmp/addp-hourly-local-evidence.json`。影响选择 dry-run 仅核对登记范围。

**覆盖边界**。仅验证固定版本、120 条合成执行、同进程两个自然小时和连续批次回绕。未覆盖持续 Worker 并发写入、重启／故障恢复、超大树或 100000 条事件预算耗尽、用户 Owner 读取授权及长时授权有效性、Owner 释放、专业结果生命周期、180 天概览删除、容量／HA 或生产 T5。原记录中的清单第 327／328／329 行仍为部分实现。

## 大量事件分页与截断真实入口验收通过（限定范围，2026-10-26）

人工 `execution-events-pagination` 使用独占 Hosted Linux 的 System、Gateway、Meta、Monitor、Console 和标准 Infra。夹具校验独占 `addp_online`、固定源码、当次 PostgreSQL 容器及仓库外私有目录，普通 User 凭据仅传入测试子进程。两组均为绑定普通 User 的合成 Meta 一次性执行，由 Common 的真实 claim／lease 和有界事件写入准备，读取沿 Gateway → Monitor → Meta Owner 裁决。该范围不含真实 Meta 扫描 Worker、扫描定义或业务引擎。

### 首轮 Hosted：身份准备失败

[Run 40274854201／Job 121415332273](https://github.com/pampa0629/addp/actions/runs/40274854201/job/121415332273) 固定源码 `4a193091a146ef457ce838258dc9482123463c8e`，`workflow_dispatch` attempt 1。四服务构建与 checkout 一致，普通 User／Tenant 2／两项 Permission 的预检通过。实际 Online gate 失败，外层 `online_scenario_failed`、`failure_stage=scenario`，套件 `failed_stage=identity`、`checks=[]`。日志记录 `execution_events_pagination_failed:KeyError`。分发器耗时 455 毫秒，预检 177 毫秒。Common 写入、分页、截断、Owner 隔离、Console 和截图均未执行。

System 测试 helper 创建同租户和跨租户身份后，遗漏了 `writeEnvironmentFile` 中的 `ADDP_ONLINE_EVENTS_PEER_ACCESS_TOKEN`、`ADDP_ONLINE_EVENTS_FOREIGN_ACCESS_TOKEN` 和 `ADDP_ONLINE_EVENTS_FOREIGN_TENANT_ID`。身份准备读取缺失的跨租户 Tenant ID 时抛出 KeyError。修订 `2350759ad8ee1b05e6394626d7be49886e846e05` 仅补三项显式导出及回归，沿用既有产品 API 和权限。

外层 `cleanup=passed`、`infra_cleanup=zero_residuals`。官方归档 `12631636857` 为 51521 字节，含 5 个安全报告／生命周期文件。ZIP SHA-256 独立匹配 GitHub 声明及源码，为 `97b577e326f44265cbc3f61e0f58b5eb83705cd96bb79dfe3c9b6e1c1d4acec8`。本地归档为 `/tmp/addp-ci-20261026-063018/archive-40274854201.zip` 及同名目录，审查回执为 `/tmp/addp-event-pages-t4-40274854201-artifact-verified.json`。

### 第二轮 Hosted：历史分页阶段失败

[Run 40281399550／Job 121435288938](https://github.com/pampa0629/addp/actions/runs/40281399550/job/121435288938) 固定身份修订 `2350759ad8ee1b05e6394626d7be49886e846e05`，`workflow_dispatch` attempt 1，整轮失败。分发器耗时 37875 毫秒，预检 503 毫秒。

- **已执行断言**。7 组场景检查通过，覆盖真实 lease 写入和成功终态、1000／1201 条事件的 10／13 页、ID 顺序与无重复、安全字段与保留窗口、两类上限、非法分页 400，以及同租户非发起人与跨租户已知游标返回 404、列表总数为 0。浏览器通过正式登录与同一主体断言，以及 quota 组十页 1000 行和唯一采集截断断言。
- **首败及未完成项**。第二组停在 `failed_stage=history_pages`，该阶段同时涵盖加载、DOM 和辅助后页 API，`history_rows=0` 无法判定页面实际加载了多少行。刷新、重载、重开及两条 execution 与 2201 条事件的最终读取前后比对未完成。仅生成 `execution-events-quota.png`，已目视核对成功终态、末行采集截断、空计数和过程不完整提示。
- **源码调查与修订**。脚本跨整页导航复用登录时 API 客户端，Console 初始化刷新会话后旧 Token 已被 System 轮换撤销。修订 `fbdd03f5b8eb45385b249c89e75087f50299fe40` 复用 `browserAPIFromResponse`，用最近一页真实请求凭据创建临时客户端，复核同一 User／Tenant／Permission 后读取后页并立即销毁客户端。修订细分初始化、首批、逐页、DOM、上限、后页身份、后页读取及截图阶段，诊断排除异常正文、凭据和原始响应。已确认的是脚本失配，当次粗阶段证据不足以确认唯一根因。
- **清理与归档**。最终 Host 业务失败，`cleanup=passed`、Infra `zero_residuals`。官方归档 `12632782532` 为 179580 字节，含 7 个安全文件，ZIP SHA-256 独立匹配 GitHub 声明及四服务固定源码，为 `d42d9c2f994238e43ce66b9bd99b189202f11a4b6582196f5826e153767f452f`。本地归档为 `/tmp/addp-event-pages-t4-40281399550.zip`、同名目录及 `-audit.json`。

### 第三轮 Hosted：固定版本完整通过

[Run 40284854056／Job 121446007939](https://github.com/pampa0629/addp/actions/runs/40284854056/job/121446007939) 固定源码 `fbdd03f5b8eb45385b249c89e75087f50299fe40`，`workflow_dispatch` attempt 1。Run、Job、Validate、实际 Hosted gate 和 Upload 均通过。Job 为北京时间 15:37:37–15:58:19。分发器耗时 39263 毫秒，其中预检 236 毫秒、场景 39026 毫秒。四后端使用同一固定源码、各自 Build ID 和有效源码指纹，检出干净且仅回环。普通 Tenant 2 User 仅持 `meta.scan_task.read`、`monitor.execution.read`。

该轮完整通过 9 组场景和 7 组页面断言。两条合成执行均为 `success`、`progress=100`。

| 验收维度 | quota 组 | history 组 |
| --- | --- | --- |
| Common 写入与采集截断 | 尝试追加 1005 次，实际保存 1000 条，唯一末条为 `truncated`。 | 昨日 600 条合法历史加当日真实 lease 写入 601 条，共 1201 条，无采集截断。 |
| API 完整分页 | 10 页完整读回 1000 条。 | 13 页完整读回 1201 条。 |
| Console 展示上限 | 真实密码登录后加载十批至 1000 行，末行展示采集截断。 | 同样加载十批至 1000 行，末行是正常进度。此时 API 仍为 `has_more=true`，真实后页可读。 |

- **分页与授权**。核对两组的全部 API ID 顺序、无重复、闭合字段和 30 天窗口。非法分页返回 400，同租户非发起人与跨租户使用已知游标仍为 404，列表总数均为 0。读取前后两条完整 execution 和全部 2201 条事件摘要一致。
- **页面交互**。逐页核对 ID 和完整 DOM 投影。两组均提示过程不完整并隐藏加载更多。刷新、整页重载及关闭重开均回首批 100 行，路由仍指向原 execution，安全投影无私有夹具内容。两张截图已目视核对，quota 末行的步骤／计数为空，history 无采集截断。1000 行数量由 API 与 DOM 逐行断言证明。
- **清理与归档**。套件内 `pending_deployment_destruction` 为中间状态。最终 Host result／cleanup 为 `passed`，Infra 为 `zero_residuals`。原始日志核对应用停止、容器／卷／网络和私有凭据销毁。官方归档 `12634584022` 为 304600 字节，含 8 个安全文件，ZIP SHA-256 独立匹配 GitHub 声明及固定源码，为 `7eadfb17a55ed160feca5dbcc1d89a735b1b270a287cc214e6f47696cda4571a`。本地归档为 `/tmp/addp-event-pages-t4-40284854056.zip`、同名目录及 `-metadata.json`／`-artifacts.json`／`-audit.json`，审查记录含逐文件摘要及两图目视范围。正式脚本输入与该 SHA 一致。

### 本地回归与覆盖边界

三版均使用 `make test-execution-events-pagination-runner`。成功运行均含两个 Common 无外部服务 Go 守卫、System 身份夹具 Go 包、浏览器 JS 语法和 Online CI 登记。专用 Hosted Go 夹具因缺少运行标志跳过 1 项，既有 System PostgreSQL 专项因缺少标准 DSN 跳过 5 项，均未计入本地 T2 或真实写入证据。影响选择 dry-run 只核对登记。

| 版本 | 本地结果与证据 |
| --- | --- |
| 初版 | 共享与隔离输入均通过 24 项 Python。隔离基线 `d6fecb323a8e1e053fc0136585ac8645b2faadd3` 加 15 个文件，摘要与最终提交一致。保留实现缺失首败 `/tmp/addp-event-pages-runner-red.log`，以及 `/tmp/addp-event-pages-runner-isolated-final.log`、`/tmp/addp-event-pages-validation-source.json`、`/tmp/addp-event-pages-owned.patch`。 |
| 身份交付修订 | 新增红绿回归实际调用 writer，核对 0600、清空继承环境后 source、三主体／Tenant／两项 Permission、引号保真及拒绝未知导出。HTTP 身份观察为模拟，至夹具边界停止。共享与隔离输入均通过 24 项 Python。隔离基线 `148bca32d853171c47f1aa364edc85775d94b5cf` 加两个 Go 文件，摘要与修订一致。证据为 `/tmp/addp-event-env-runner-{red,green,isolated}.log`、`/tmp/addp-event-env-validation-source.json`。 |
| 浏览器后页修订 | 单一凭据绑定回归先失败，修复后以 Node 24.19.0 在隔离输入通过 25 项 Python。复用共享客户端既有轮换、旧 Token 401、主体或租户变化拒绝回归。只变更两个 Online 脚本／回归文件，输入摘要与提交一致。证据为 `/tmp/addp-event-browser-runner-{red,node24,isolated}.log`、`/tmp/addp-event-browser-validation-source.json`。 |

第三轮通过限定于合成 Meta 执行、Common 真实 lease 写入、Owner 读取及 Console 页面。未覆盖实际扫描 Worker、其他 Owner／专业节点、持续 Worker 并发、重启／故障恢复、规模容量、Owner 释放与消费者引用裁决、180 天概览删除、专业结果期限或生产 T5。原记录中的清单第 327／328／329 行仍为部分实现。持续写入期间的续页与刷新另见[后续 Hosted 尝试](#持续写入期间续页与刷新首次-hosted2026-11-07)。

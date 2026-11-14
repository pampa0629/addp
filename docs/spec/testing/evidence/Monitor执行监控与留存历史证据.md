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
| 持续写入期间续页与刷新 | 固定源码 Hosted 已通过只读扫描基线、持续写入续页、Console 刷新和原页终态。整套仍因 CDC 审批扫描领取失败及 Kafka 业务清理失败而失败。 | [固定源码复验](#2026-11-10-固定源码复验) · [首轮](#首轮整引擎扫描被拒绝) · [目录基线读取失败](#复验目录基线读取失败) |
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

本节收录三次 Hosted 尝试和各自的本地回归。场景与身份要求见[现行 Transfer continuous 验收场景](../scenarios/同步与引擎消费.md#transfer-continuous-events)。

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

2026-11-10 已确认后续夹具在同一独占 Hosted PostgreSQL 只读快照中核对本轮 Tenant、Engine 下四张固定表及对照表祖先。扫描前后的表与节点事实保留原断言。准备扫描的执行归属与可见性、租户日志读取及撤权仍由原身份通过公开接口验证。本地专项通过 95 项 Python 测试、JavaScript／Bash 语法检查及 System 身份夹具 Go 测试。新观察查询的实际结果见[固定源码复验](#2026-11-10-固定源码复验)。

前两次 Hosted 均未创建 Kafka 任务，因此同一任务写入前保存游标、写入后沿原游标续页、Monitor 与 Owner 完整事件顺序比对、Console 自动续页与手动刷新、Worker 失联后同页的失败诊断和唯一终态均未执行。准备扫描复验通过及零残留只覆盖各自的断言。[旧版两页四条事件与撤权证据](#2026-10-23-固定版本租户日志终审)

### 2026-11-10 固定源码复验

[Run 42805790493／Job 129794444325](https://github.com/pampa0629/addp/actions/runs/42805790493/job/129794444325) attempt 1 使用干净检出的 `a8b16b39507a0e11b8352735bcf21125281c5d13`。预检通过，System、Gateway、Meta、Transfer、Monitor、Security 的构建源码均与该提交一致。整套结论为失败。

- **通过**。独立准备 User 的扫描成功，普通消费与审批 User 各自读该执行返回 404。PostgreSQL 只读快照核实一张对照表和两级祖先，随后通过 Kafka 与 CDC 首次深扫描取证。Kafka 真实持续写入期间沿原游标续页，与 Owner 完整事件顺序相符。四份 Console 回执均通过，覆盖自动续页、手动刷新、原页面唯一终态、Owner 真实 503 后同进程恢复及撤权。Worker SIGKILL、租约过期和替代进程从已提交位置恢复通过。
- **首败**。CDC 增量审批读取与提交均返回 200。Transfer 在审批后领取 Meta 扫描时记录 `schema change request conflict`，随后回滚 claim。归档中的来源时间小数秒为 `.299069081`，精度高于 PostgreSQL 时间列，源码对重新读出的 `applied_at` 和该来源时间做精确比较。这个精度差能解释冲突，仍待真实 PostgreSQL 回归复现。CDC 审批扫描、重放审批、恢复后的写入和 bounded 定时任务断言未完成。
- **清理**。CDC 定义删除后读回 404。Kafka 定义删除持续轮询运行会话，约 10 秒后 Transfer 返回 503、Gateway 返回 502（请求 `367da1bb-d758-4a8e-ac73-2c83e16c2458`）。业务清理失败，隔离部署销毁前剩余一项。外层销毁通过，Infra 报告零残留。归档中旧 Kafka 执行有会话插入记录，未见退出确认。替代执行和其余两条执行均有退出确认 SQL。删除期间针对该 Kafka 任务轮询了 40 次未确认退出的会话，与当前要求所有历史会话确认退出的裁决一致。
- **归档**。证据归档 `13761547686`，独立核对 ZIP SHA-256 与 GitHub 声明一致，为 `4a0479d5c4c6c6a176d3260d808a408e16f4f473bf9c1da9bab51b6b321eab59`。

## 2026-11-12 任务级 CDC 指标 Hosted 首轮

用户明确授权运行新增断言后的既有 `transfer-continuous-events` Hosted T4。[Run 43202861644／Job 131130484246](https://github.com/pampa0629/addp/actions/runs/43202861644/job/131130484246) 为 attempt 1，固定源码 `079dd4b9eb6688f8a8e28a852f1117417ad4b632`；该版本包含 `c4077dd0a` 的任务槽／WAL／定向捕获延迟与暂停恢复断言，之后两次提交未改写相关场景文件。六个服务的正式预检构建身份均绑定该 SHA，隔离 Linux x86_64 准入通过；Job 北京时间 18:45:08 开始、19:02:17 结束，结果为 failure。

- **已通过范围**。报告的 10 项 checks 已完成：正式源扫描、业务 Kafka 初始提交及事件、不可读执行列表隔离、Owner／跨租户／同租户历史权限、持续写入游标、真实 Worker SIGKILL 后租约恢复、Kafka 暂停恢复及坏记录、Owner 不可用时失败关闭、撤权和重新登录后的拒绝、真实多页游标及撤权后列表／总数。`continuous-browser-live/recovery/revocation.json` 三份实际浏览器回执均为 passed。
- **首败与证据边界**。套件停在 `kafka_hard_delete/request_or_contract_failed`，`uncertain_mutation=false`；只创建了 Kafka task 1，尚未进入 CDC task 创建或 `cdc_task_monitoring`，因此新增槽／WAL／延迟／暂停恢复断言全部为未执行，不能记为新增 T4 通过或 CDC 产品失败。同一 DELETE 请求 `2d08d3f0-3556-4b11-a406-374c228c1cdb` 在约 10.009 秒时由 Gateway 记录 `context canceled`、HTTP 502，Transfer 记录 HTTP 503。场景客户端默认 HTTP timeout 为 10 秒，Transfer 原有 runtime stop 等待默认上限为 45 秒，等待预算冲突已确认；但当次安全报告没有区分 DELETE 传输、响应处理、定义读回及数据库后置核验，不能仅凭通用失败码断定具体退出的子步骤。该时限冲突是直接排查方向，未通过调大时限、放宽拒删断言或再次派发来改写本轮结果。
- **清理**。领域报告为 `cleanup=failed`，Kafka task 1 的 `deployment_destruction` 步骤未取得完整拒删证明，`residual_resources=1`、`expected_deployment_residuals=0`。外层标准退出清理为 `cleanup=passed`、`infra_cleanup=zero_residuals`：停止应用、销毁本轮 Business PG／Kafka 夹具、移除 Infra 容器／网络／卷及私有凭据目录的检查完成。部署销毁成功不替代领域任务清理通过；个人开发服务、数据卷及原有逻辑槽未被本轮操作。
- **归档**。Artifact `13937371862` 的 ZIP SHA-256 经独立下载核对，与 GitHub 声明一致：`dac509517e9474ad537cc74b385ef631ed737d53fe69544952b54e0fd752a089`。下载后的 JSON／JSONL／日志／文本未检出所检查的 JWT 模式或 `business-value-sentinel`。原始报告、浏览器回执、日志、ZIP 与独立评估在 `/tmp/addp-cdc-hosted-43202861644/`；没有新增 Hosted 重跑。本轮下一步是先定位并收敛 Kafka 拒删验证的 HTTP 等待边界，再复验原场景，拒删条件与运行会话退出确认要求保持原契约。

## 2026-11-12 任务级 CDC 指标 Hosted 复验

用户确认继续后，仅为 Kafka 强退拒删验证的唯一 DELETE 创建同一身份／Gateway 的 60 秒客户端，覆盖产品原有 45 秒 runtime stop 窗口；后置 GET 仍用原 10 秒客户端，服务端停止策略、拒删状态和持久围栏断言不变。新增失败类型仅公开固定子阶段／原因与已接收的状态，不发布原始异常或响应。原等待预算的确定性用例先复现 TimeoutError，修订 `63193b3c9aabfffc6acdee4289ffb0dda319d30e` 通过 `make test-transfer-continuous-online-runner` 的 115 项测试及 System 身份夹具 Go 检查，`make test-online-registration-unit` 的 156 项测试和登记检查通过。

[Run 43213142409／Job 131165583466](https://github.com/pampa0629/addp/actions/runs/43213142409/job/131165583466) 为第二次独立运行、attempt 1，固定检出上述 SHA，六个正式服务构建身份均一致，repository_clean=true。因并发排队后，Job 北京时间 20:58:31 开始、21:13:46 结束；主门禁 20:59:06—21:13:41。整套结果为 failure，但新增 CDC 指标及暂停恢复断言已实际执行并通过，不能与首轮未执行混同。

### 已闭合的前置和 CDC 断言

Kafka 强退 task 1 的 DELETE 由 Gateway 在 45.008 秒返回 503、Transfer 在 45.007 秒返回 503，请求 ID 为 `dc9efd0d-d27b-4b63-b0db-e6d7b708d2a6`。原未确认退出 invocation、任务保留及零外部删除登记的后置核验全部通过，`hard_exit_delete_refused_without_dlq_dispatch` 成立；未通过缩短产品等待或放宽拒删条件取得结果。

本轮任务 3／generation 1 绑定独占业务 PostgreSQL 引擎 1。逻辑槽、publication 和 Connector 沿该任务代次精确核对，全部阶段的源端／延迟／缓冲快照均新鲜；独立 PostgreSQL 核对源端绑定、原生 WAL 状态与安全余量的空值语义，定向 Jolokia 精确核对捕获延迟。WAL 字节值保持精确十进制字符串，不要求异步时刻的字节读数相等。以下为固定回执中的北京时间；字节值为 WAL 地址距离，不能解释成业务变更量或磁盘占用。

| 阶段 | 缓冲采样时间 | committed / latest | 积压位点差 | 捕获延迟 | 保留 WAL 字节距离 |
| --- | --- | --- | --- | --- | --- |
| 初始追平 | 21:12:15.905 | 6 / 6 | 0 | null，尚无流式事件样本 | 22960 |
| 暂停后、写入前 | 21:12:30.849 | 6 / 6 | 0 | null | 22960 |
| 暂停后写入三条增删改 | 21:12:45.851 | 6 / 9 | 3 | 0.331 s | 23736 |
| 恢复追平 | 21:13:00.843 | 9 / 9 | 0 | 0.331 s | 1656 |
| 后续独立新采样 | 21:13:15.856 | 9 / 9 | 0 | 0.331 s | 1656 |

槽始终活跃，`wal_status/risk_state=reserved`；源库未设置槽 WAL 保留上限，`safe_wal_size_bytes=null`，不补零。初始及暂停前无事件延迟的空值、真实变更后的 331 毫秒都与本任务 Connector 原生值对应。暂停期间目标行未变化，恢复后的精确新增／修改／删除结果通过；generation 保持 1，execution 从 `0ca8e056-ef1c-47f2-9772-ea1386f0e965` 切换为 `e17d8626-9f93-4673-9f5d-c871b3f85717`。后续 additive 审批、不同审批 User 的补扫归属及显式恢复通过；正式 Stop 后三类当前观测全部为空，源端槽／publication 的物理清理通过。

共 17 项 checks 完成，live／recovery／revocation／owner_unavailable 四份真实浏览器回执均为 passed；这些浏览器场景验证既有执行页面行为，不外推为新增 CDC 指标卡片的真实浏览器验收。新增任务指标的 T4 证据来自正式任务 API 与原生只读对照。

### 后续失败与清理边界

套件随后停在 `scheduled_completion/request_or_contract_failed`，`uncertain_mutation=false`。bounded 定时 task 4 的 execution `4679dadb-5e68-4216-9bff-f489c588e855` 在归档中记录 success、2 行已提交；验收要求 `metadata.metadata_scan.status=submitted`，当次 metadata 未含该反馈。后续原始状态出现 `initial_authorization.status=pending/code=scan_failed`。固定源码的原生首次新建目标路径调用 `EnsureProductionBasicScan`，需要对照现行成果识别／限定 basic 补扫契约进一步核清；本轮未用替换反馈字段或删除该断言的方式将整套改记为通过。定时扫描归属／物理结果及末尾已删除定义的授权历史检查未完成。

领域清理 `cleanup=passed`：task 4（scheduled）、task 3（CDC）、task 2（graceful Kafka）经正式删除读回缺失；正常 Kafka 的 invocation 退出、定义删除及准确 DLQ 缺失均有回执。task 1 按强退用例设计 `retained_for_deployment`，因此领域 `residual_resources=expected_deployment_residuals=1`，不是未核清残留。外层停止应用、销毁本轮业务夹具及私有材料完成，`cleanup=passed`、`infra_cleanup=zero_residuals`，计划留待部署销毁的 task 1 随独占数据卷一并移除。领域与外层清理通过不替代整个 suite 业务通过。

Artifact `13946758225` 独立下载 ZIP 的 SHA-256 与 GitHub 声明一致：`cdb7f24a9b5248c5e33844f48b22c98caf3a6ff79c4f457eba601d8b5a5aed41`。下载的 JSON／JSONL／日志／文本未检出所检查 JWT 模式或 `business-value-sentinel`。原始报告、四份浏览器回执、日志、ZIP、摘要核验与独立评估保留在 `/tmp/addp-cdc-hosted-43213142409/`。首轮失败及其证据保持原记录，本轮未改个人开发服务、数据卷或原有逻辑槽。

## 2026-11-13 basic 与 deep 串接及 CDC Hosted 闭环

用户确认 basic 快速建立授权所需身份、deep 自动补齐后续使用所需元数据，并批准复用 Meta 队列持久串接两次扫描。固定实现 `d12e8279b3dacb87f9fc196aa3f2ddb79598e0dd` 为原已提交成果登记唯一 basic／deep 执行，Meta 沿 Common ClaimPending 控制依赖，Worker 重核原回执、精确目标、引擎版本与本次 lease；Transfer 自动扫描反馈指向 deep，并保存独立 basic UUID。System 仍只消费 basic 识别，未增加等待 deep 的授权前提，未把原 deep 字段断言降为 basic。

实现提交前，`make test-module MODULE=common GO_ONLY=1`、Meta／Transfer 各自同入口的 Go T1 通过；`make test-meta-postgres META_POSTGRES_TEST_ARGS='--test production'` 在已核实的本地 `addp_test` 通过既有 production／plan-facts 专项，覆盖父生产执行已结束、并发请求去重、重建 service 后恢复同一关联、basic 运行中不领取 deep、失败／取消／超时的无源读取失败、原回执变化／兄弟目标／旧 lease 拒绝、deep 失败后 basic 事实仍可读取，以及本轮执行／事件清理核对。首次队列回归先复现提前领取 deep 的失败，修复后通过。continuous runner 115 项及 System 身份夹具 Go 检查、Online 登记 158 项通过；Meta／Transfer Swagger 已生成并通过 57／43 个路由方法覆盖检查。这些是确定性与专项 T2 结果，不记为完整模块或全仓 CI 通过。

[Run 43253234380／Job 131304474365](https://github.com/pampa0629/addp/actions/runs/43253234380/job/131304474365) 为第三次独立 Hosted 运行、attempt 1。运行绑定上述 SHA，六个正式服务构建身份完全一致、repository_clean=true；等待 Runner 后，Job 于北京时间 2026-11-13 01:18:00 开始，01:36:51 成功结束。整套 19 项 checks 全部完成，`result=passed`、`uncertain_mutation=false`；前两轮失败仍按各自固定版本保留。

### 定时任务的两阶段扫描

Tenant 2、User 33 的 bounded 定时 task 4，原 execution 为 `9f500b12-96bf-4f4d-91c6-6459ad69e885`。专业反馈给出的 basic `c1fb9768-0aa3-488a-9b0e-c7adedca34b0` 与 deep `0407aaba-2896-4752-b143-8eb9e0ceb6c3` 是不同 UUID，均成功、均保留原人类来源和直接父执行，另一 User 读取两次扫描均为 404。独立复核归档运行日志中的实际持久时间：basic 于 01:36:32.150 领取、01:36:32.222 成功；deep 于 01:36:32.226 领取、01:36:32.271 成功。该时间只描述本轮夹具，不构成性能保证。

原 deep 要求全部保留：目标字段为 `id/label`，目标物理行集由 Business oracle 独立验证为预期两行；目标扫描仅覆盖指定表，兄弟项和两级祖先的扫描状态、时间与深度保持基线。任务已暂停后续调度。本 suite 没有验证 System 初始 Grant 的真实签发／读取／撤销全链路；不能从 basic／deep 成功推导授权已经签发。

### CDC 当前观测与暂停恢复

任务 3／generation 1 的三类当前快照均新鲜，正式任务 API 与原生 PostgreSQL／本 Connector Jolokia 定向读取对应。初始 execution `39e72e37-999c-4384-95f3-685123a81670`，恢复为 `66a31d60-6ef2-47e5-b9c8-43e5940629a0`，generation 不变。

| 阶段 | 北京时间缓冲采样 | committed / latest | 积压位点差 | 捕获延迟 | 保留 WAL 字节距离 |
| --- | --- | --- | --- | --- | --- |
| 初始追平 | 01:35:13.756 | 6 / 6 | 0 | null，尚无流式事件样本 | 23000 |
| 暂停后、写入前 | 01:35:28.668 | 6 / 6 | 0 | null | 23056 |
| 暂停后写入三条增删改 | 01:35:43.691 | 6 / 9 | 3 | 0.357 s | 23952 |
| 恢复追平 | 01:35:58.670 | 9 / 9 | 0 | 0.357 s | 27168 |
| 后续独立新采样 | 01:36:13.671 | 9 / 9 | 0 | 0.357 s | 4168 |

全部源槽活跃、wal_status／risk_state 为 reserved；保留上限未设置，safe_wal_size_bytes 保持 null。WAL 字节保持精确字符串，是地址距离，不代表业务变更量或精确磁盘占用；不要求异步原生字节值相等，也不要求它随消费积压立即归零。暂停期间目标行未变化，恢复后精确增删改及连续两次新采样零积压通过。不同 User 的 additive 审批补扫归属、双向读取隔离、重复审批和显式恢复通过；正式 Stop 清空三类当前快照，自有源槽与 publication 的物理清理通过。

四份实际 Console 浏览器回执 live／recovery／revocation／owner_unavailable 均 passed，范围为既有执行页面的进度／续页／恢复／撤权／Owner 故障行为。任务级 CDC 指标由正式 API 加原生只读对照验证，本轮不外推为新增 CDC 指标卡片的真实浏览器 T4。

### 清理与原件

Kafka 强退拒删收到 503，原 invocation 未确认、任务保留及零外部删除派发均核实；正常退出组的 invocation、正式定义删除、准确 DLQ 缺失及删除后授权历史可读全部通过。领域清理为 passed：scheduled／CDC／正常 Kafka 三个任务正式删除读回 404；强退 task 1 按原用例设计留待部署销毁，`residual_resources=expected_deployment_residuals=1`。外层标准退出完成应用、Business 夹具、Infra 容器／网络／数据卷及私有材料销毁，`cleanup=passed`、`infra_cleanup=zero_residuals`。本轮只操作独占 Hosted 部署，个人开发服务、数据卷及原有两个逻辑槽未被操作。

Artifact `13968601880` 的 ZIP 已独立下载核对，SHA-256 与 GitHub 声明一致：`89e59a8b832d3368956ae95e51568d3b977bb634f0537b724bfff2c505b70765`。39 个 JSON／JSONL／日志／文本中未检出所检查 JWT 模式或 business-value-sentinel；这不是完整敏感信息审计。原始报告、浏览器回执、日志、ZIP、阶段时间及独立断言评估位于 `/tmp/addp-cdc-hosted-43253234380/`。本记录只认定该固定版本的本套件闭环，不覆盖其他生产 owner、引擎或全平台授权验收。

## 2026-11-13 本机 CDC 任务页面与暂停恢复

本机专用任务 `cdc_monitor_local_20261113`（task 61，Tenant 1，创建者 31）通过正式 Transfer 入口创建并启动；源／目标为 Business PostgreSQL 引擎 2 的独立 `public.addp_cdc_monitor_source_20261113`／`public.addp_cdc_monitor_target_20261113`。源表与两条初始记录、后续新增／修改／删除各一条均通过当前 User 的 Develop 正式查询预检与执行入口写入。

Chrome 真实租户页面核对源／目标、活跃任务逻辑槽、WAL `reserved` 与约 10 GiB 的 PostgreSQL 原生写入余量。初始提交／末端／积压为 `6/6/0`；正式暂停后为 `6/9/3`，任务捕获延迟为 `0.141s`，目标物理数据保持暂停前两行；恢复产生新 execution `ed394a55-dcf5-432c-bc40-c45e5ba490e6`，页面及连续两次新采样均为 `9/9/0`。源／目标最终两行逐项相同。只读前后对照确认原 `addp_cdc_t1_task46_g1`、`addp_cdc_t1_task58_g1` 的活跃状态及两项位点未变。

任务及两张表按用户体验用途保留运行，不声明零残留。报告与分阶段原件在 `/tmp/addp-cdc-local-20261113/assessment.json` 及同目录，仅为本机临时证据，不是 Hosted Artifact。首次 UPDATE 因缺少显式目标定位符在预检阶段被拒绝、零写入；补齐正式目标定位符后执行。额外 CLI OAuth 读取 Monitor 执行详情返回 403，符合现行仅接受 unrestricted first-party User Token 的中间件，未调整权限；Chrome first-party 页面实际读回执行进度。此记录证明本机真实页面，不把旧 Hosted 的执行页面回执改判为 CDC 新卡片验收。

## 2026-11-13 CDC 任务卡片的 Hosted 三阶段断言

固定源码 `88416bf8a744066c9d7260807e038fe6bd599ad8` 的 [Run 43357940010／Job 131664345961](https://github.com/pampa0629/addp/actions/runs/43357940010/job/131664345961) 于北京时间 11:26:58 开始，11:43:47 成功完成。沿既有 `transfer-continuous-events` 的正式租户身份、源／目标和任务生命周期，新增 `cdc_initial`、`cdc_growth`、`cdc_resumed` 三份页面回执均为 passed；分别核对当前任务身份、源槽／WAL、捕获延迟、消息位点和 execution 关联，没有新增权限或读取业务样本。

任务 3、generation 1 的初始提交／末端／积压为 `6/6/0`，暂停后写入三条事件形成 `6/9/3`，恢复后连续两次采样为 `9/9/0`。恢复产生新 execution `20f40079-12dd-461a-a8c1-9572bd5c7233`；原生槽始终 active、WAL 状态 reserved，事件后的任务捕获延迟为 0.175 秒。场景核对精确 Connector 的延迟与源端原生身份，并确认停止后当前样本退出。

Artifact `online-t4-transfer-continuous-events-43357940010` 含三份 `continuous-browser-cdc_*.json` 及 `transfer-continuous-events.json`，在线套件 result／cleanup 均为 passed。套件专门验证清理控制返回 503 时不发出删除请求，故该阶段保留一条任务记录（residual_resources=1），不能声明该阶段业务对象为零；完整 Hosted Infra 回收摘要为 `infra_cleanup=zero_residuals`。上述结论不覆盖后续新增不可恢复风险告警的真实故障及渠道送达。

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


## 2026-11-14 最近执行直达与同次结果回跳（T1／T2／T3）

正式契约先提交于 `f9b3bcba1`，核心实现为 `341f24328`；最终功能与 CI 配置取证源码为 `9cb8b4e8c06313f5213930fd96476ebb07389e0f`。任务行按同 Tenant／Develop／任务类型／任务定义的 `created_at DESC, id DESC` 直达最近受理的 execution，包含 pending／running，不按旧执行完成或领取顺序替换。Monitor 保留通用详情；Develop 结果页仅保留识别摘要、专业结果、日志与冻结输入，返回同 UUID 及原筛选／分页；无监控读取权的菜单不留空项。四个任务读取入口在执行事实查询失败时返回安全 500，确无任务仍为 404；不返回旧缓存或数据库错误。

| 固定源码 Linux 门禁 | 实际结果 |
| --- | --- |
| [Develop 前端](https://github.com/pampa0629/addp/actions/runs/43579351187/job/132400529770) | 单测、320 项浏览器回归及构建成功；日志包含两项最小权限菜单用例。 |
| [Console 前端](https://github.com/pampa0629/addp/actions/runs/43579351187/job/132400529787) | 284 项单测、361 项浏览器回归及构建、整 Job 均成功；日志逐项包含 pending／running／success 的任务→Monitor→结果页→刷新及同次回跳。 |
| [Monitor 前端](https://github.com/pampa0629/addp/actions/runs/43579351187/job/132400529845) | 154 项单测、863 项浏览器回归及构建成功；新增 403／404／503 读取拒绝均保持原 UUID、不替换为列表中的其他记录。 |
| [Develop PostgreSQL 15](https://github.com/pampa0629/addp/actions/runs/43579351144/job/132407503241) | 完整标准门禁通过、无 SKIP；`latest_task_includes_pending_and_running` 明确 PASS，覆盖跨 Tenant／模块／类型排除、无事实清空缓存、旧完成不换记录、相同时间按 ID 裁决。 |
| [Execution runner](https://github.com/pampa0629/addp/actions/runs/43579351187/job/132400529277) | 207＋19＋28＋158＋35＝447 项 Python 与登记实际通过。 |

[全仓 Go](https://github.com/pampa0629/addp/actions/runs/43579351187/job/132400529500) 中 Develop API／repository／service 包实际通过；作业随后因 Security 的 `go.mod/go.sum` 依赖清单漂移失败，不能记为全仓通过。[Platform consistency](https://github.com/pampa0629/addp/actions/runs/43579351187/job/132400529800) 实际通过 227 项 Trace runner Python、登记与 System 身份夹具 Go，随后在 `test-dev-lifecycle` 因 Security 缺少 MinIO 依赖校验项失败；尚未执行到共享前端与前端 CI 登记检查。因此 `make test-common-frontend` 的 379 项、`make test-frontend-ci-registration` 的 27 项本轮只有本地通过证据，不冒充 Linux 通过。范围外依赖文件未修改。

本轮首个 Linux 白空格前置失败保留，已在 `2542e819c` 删除新增文件尾部空行；该版本 Console 虽完成 361 项回归及构建，仍因整 Job 的 20 分钟上限取消，未记为门禁通过。最终源码只把 Console 整 Job 明确为 30 分钟，Monitor 40 分钟及其他默认 20 分钟不变；正式规范、登记检查和反例测试同次同步，单例断言、并发与真实验收预算均未改动。已被最终源码替代、尚未执行的 `00d01f792` 两组重复作业取消，不计通过；原失败日志保留。

本地最终 `make test-develop-frontend` 320 项及构建通过；Monitor 为完整 860 项加新增三项拒绝专项及构建，最终 Linux 一次执行了全部 863 项。共享工作区 Console 本地为 284＋373 项及构建，包含其他会话的未提交用例；发布快照的 Linux 为上述 284＋361，不混用计数。此次仅为隔离 HTTP 夹具 T3、Go／受控离线 T1 和真实 PG T2；未重启个人服务、未触发 Hosted、未运行新的正式用户真实链路，其他会话改动未纳入提交。当轮尚缺共享前端／登记检查的 Linux 实证，后继独立 Job 的补齐结果见下节；新的本机隔离真实窗口仍须单独确认。


## 2026-11-14 共享前端与前端登记独立 Linux 取证（T0／T1）

固定源码为 `2d5e63d43af0c87e19971c61481d84186fc7c884`，Platform CI Run `43586730624`。正式 CI 契约先统一，两项现有检查分别使用独立 Ubuntu 24.04 Job，均无其他 Job 依赖、路径跳过或条件测试步骤，复用原 Make 入口、统一摘要和每 Job 10 分钟预算。共享前端只按 `.node-version` 准备 Node；登记检查使用 Runner 自带 Python 3。原平台聚合调用、消费模块门禁和断言均保留。

| Linux 实际终态 | 标准入口与原始日志核验 |
| --- | --- |
| [Common frontend contracts／132422749269](https://github.com/pampa0629/addp/actions/runs/43586730624/job/132422749269) | 北京时间 19:08:49–19:09:11，Job、测试、摘要及退出步骤均成功。`make test-common-frontend` 实际执行 379 项，379 通过、0 失败、0 跳过、0 取消；摘要为 `selected=true / success`。 |
| [Frontend CI registration／132422749381](https://github.com/pampa0629/addp/actions/runs/43586730624/job/132422749381) | 北京时间 19:12:05–19:12:20，Job、测试、摘要及退出步骤均成功。`make test-frontend-ci-registration` 实际执行 34 项 Python、无跳过，21 个前端及两项独立 Job 登记通过；摘要为 `selected=true / success`。固定源码的 34 个测试方法包含新增的 7 项独立 Job 登记回归。 |

本地共享工作区及仅包含已提交源码与本轮改动的隔离副本，均沿上述两个标准入口通过 379／34 项；新增 Job 的 YAML 解析及本轮差异白空格检查通过。登记反例覆盖 Job 缺失／重复、依赖聚合、条件跳过、遗漏或吞掉标准命令失败、错误摘要、运行环境与预算漂移、Node 准备晚于测试。两项 Job 排队后实际运行的原始 JSON、日志和 SHA-256 审计保存在 `/tmp/addp-independent-frontend-ci-2d5e63d43/`，目录 0700、文件 0600。

至此，上节两项“只有本地证据”的缺口已由本轮固定源码的 Linux 实际执行补齐；不改写前轮 Platform consistency／全仓 Go 的失败结果，也不据此声明全仓 CI 通过。本轮未修改或接管 Security 依赖，其他会话改动保留，未安装共享库 `node_modules`，未重启服务、触发 Hosted 或追加真实运行。剩余为新的正式用户隔离真实链路验收，仍需单独确认；本轮 T0／T1 不改变此前真实验收的首失败和未完成项。

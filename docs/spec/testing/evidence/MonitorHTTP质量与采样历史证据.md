# Monitor HTTP 质量与采样历史证据

[返回 Monitor 历史证据](Monitor历史证据.md) · [返回验收证据索引](../验收证据索引.md)

本专题收录公共 mTLS、Go/Python 来源、启动诊断及采样／清理交接。验收结论按各记录的固定源码、环境、运行尝试和层级认定。原顺序见[Monitor 历史证据入口](Monitor历史证据.md#原记录入口)。

原始过程、逐步诊断及详细归档清单见[精简前的固定记录](https://github.com/pampa0629/addp/blob/be8f8daa4d38a7830585577f03b77bf2b5ed3931/docs/spec/testing/evidence/MonitorHTTP%E8%B4%A8%E9%87%8F%E4%B8%8E%E9%87%87%E6%A0%B7%E5%8E%86%E5%8F%B2%E8%AF%81%E6%8D%AE.md)。 现行采样及验收规则见[HTTP 接口质量](../../addp平台运行观测规范.md#64-http-接口质量分阶段接入)。

本轮归并前的逐次快照、归档摘要和完整诊断见[固定快照 `90d13c254`](https://github.com/pampa0629/addp/blob/90d13c2545c602eddcc17a81621a135ab64fa540/docs/spec/testing/evidence/MonitorHTTP%E8%B4%A8%E9%87%8F%E4%B8%8E%E9%87%87%E6%A0%B7%E5%8E%86%E5%8F%B2%E8%AF%81%E6%8D%AE.md)。

[本地一期、公共 mTLS 与 SDK 来源](#http-history-sources) · [私有目录采样与 Go 编译协调](#http-history-sampling) · [自然 Linux 轮询与归档复核](#http-history-ci-polls) · [正式产品窗口、准入与失败诊断](#http-history-product-windows)

<a id="http-history-sources"></a>

## 本地一期、公共 mTLS 与 SDK 来源

### 平台 HTTP 接口质量：本地一期

2026-10-20 本地分项以基础源码 `39b132c9926f9f404287ab5aeaa8c9d94eebd9f4`、产品源码 `3ee46b77fd84fe1a915874a11e0e97b843cac8bd` 认定。T1 覆盖 Common Router、Monitor／System Go、授权和双语 Swagger。T2 覆盖 HTTP CRUD／CAS、冻结恢复与租约、IAM 227 五项权限及刷新令牌撤销。

完整 Metrics T2 以 `688ecb005843ecadf7240a0e5249517de3a8381d` 叠加专项文件验证，包含主机、进程、PostgreSQL 来源与告警。最终 HTTP 专项另验真实 SDK、13 项窗口语义、历史趋势、503 告警、签名重试、恢复同一事件及停源清空，约 195.54 秒。两份快照各自核验零残留，HTTP 专项跳过 PostgreSQL 专业探针和后续主机回归。历史趋势首轮因自然窗口未形成而失败，复验保留原采样和失败条件。

T3 中 Monitor 78 单测／226 浏览器、Console 186／191、Common 前端 241 单测、System 149／189 按各自门禁通过，适用的构建一并通过。正式密码／MFA、Python HTTP、Trace／日志关联、拓扑、Hosted 和 T5 未纳入这一期。

### 2026-10-26 Python 全量 HTTP 质量首批与公共 mTLS 前置失败

用户确认 Common Python 统一 HTTP 观测与受保护来源、首接 Agent/Copilot 单进程 backend。实现提交 `0eca81f9b41c26b4f40375fa661067def49d56f2`：唯一 ASGI 生命周期同时服务全量 HTTP 指标与可选 Trace，删除原独立 Trace 中间件路径；指标不依赖 Trace 开关／采样。复用既有进程声明、开发／产品部署及 Platform 权限，仅发布身份、启动时间和 HTTP 指标，不发布 CPU／RSS／cgroup。每源最多 512 组详细标签、4 MiB 输出，超限整源失败、不截断；私有 Registry、串行 mTLS 来源、完整头读取及关闭边界沿同一实现验证。不新增 Tenant 字段、业务端口指标路由、权限或数据库。

最小标准验证已执行：Common Python 全量 421 项通过、54 subtests 通过，1 项可选 GeoPandas 跳过；`make test-agent-eval ADDP_AGENT_PYTHON=/tmp/addp-p19-http-metrics-gates/agent ADDP_AGENT_EVAL_REPORT=/tmp/addp-p19-http-metrics-agent-verified.json` 通过；`make test-copilot COPILOT_PYTHON=/tmp/addp-p19-http-metrics-gates/copilot` 185 项通过。Common Python 使用仓库既有标准入口与外部依赖覆盖，不修改个人模块 venv；收尾仅移除未使用 import／多余空行后，HTTP 指标及共享 Trace 定向 23 项、21 subtests 再次通过。

`make test-module MODULE=common GO_ONLY=1`、`make test-module MODULE=monitor GO_ONLY=1`、`make test-infra-runtime-log-lifecycle` 均退出 0；后者含 123 项本地 runner 前置及 64 项生命周期／预算测试。`python3 -m unittest scripts/test/metrics-deployment-config_test.py scripts/test/metrics-http-budget_test.py` 90 项通过；Python CI 登记 10 项及 11 owners、T2 登记 22／12／9／2 均通过。Agent／Copilot Swagger 路由覆盖 14／12 一致，公开 API 契约未变。`make test-changed` 已尝试但在 T2 环境前置拒绝（缺少 PostgreSQL／其他已选共享 owner 的连接输入），退出 2，不计全量聚合通过；不因此启动共享数据库或修复无关 owner。相关后续仍由既有 Platform Python／Agent／Copilot 与 Release Linux Metrics／Trace T2 门禁覆盖，本轮未查询、触发或认定其远程通过。

唯一获准的功能窗口使用 `ADDP_HTTP_BUDGET_REPORT=/tmp/addp-p19-python-http-quality-20261026/functional.json make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=http-quality`，2026-10-26 14:18:31.754—14:20:46.220（Asia/Shanghai），134.467 秒，退出 2。先通过八类固定 promtool 窗口语义断言，随后公共前置 `real mTLS scrape and controlled job label` 在原 65 秒自然采样等待中失败：夹具样本为零、发现一个活动目标且 health=down／error_kind=tls；没有观测到 HTTP SD 失败计数。Go 业务源及 Python 实链阶段均未进入，Python 容器未创建，业务请求为零，不能计作 Python T2、告警实链或全套 Metrics 通过。

本窗口采集 32 次资源检查，最大 0.260 秒，均在原完整 4 秒口径内；容器硬配额峰值 2.25 CPU／2.75 GiB，私有可写预算上界峰值约 1.44 GiB（含 tmpfs 配额预留，不能称为实占磁盘），未触发资源守卫。342 项输入摘要运行期间一致。标准退出清理及独立按当次 project 精确过滤的只读复核均确认容器／网络／卷为零、私有工作目录已删除。没有自动重试／续窗、运行 Hosted、重跑容量、设置定时或重启个人服务。共享 ASGI 调整后的真实 Trace T2 未运行，与旧混合链通过证据分开保留。

原报告、日志、分项审计和清理收据在 `/tmp/addp-p19-python-http-quality-20261026/`。早期导入、夹具和登记失败保留于原记录。

### 公共采样失败的离线关联

失败窗口的六项采样相关输入与核对版本一致。宿主机以 collector 身份访问 localhost 成功，中心以实际容器 IP 采样失败。Prometheus 中的匿名、外来 CA 与明文握手日志属于负向探针。localhost 同时匹配初始和最终证书，因此该检查无法区分 Nginx 是否已换证。

历史诊断把证书错误合并为 tls，未保留原始 lastError、实际叶子证书和请求日志，根因无法唯一确认。换证加载／实际 IP SAN 是优先排障方向，也需区分链、有效期及客户端证书。复验保留严格 TLS，核对实际 IP 的预期／实际叶子摘要和匹配结果。

### 共享 Python 依赖与 Agent 冷安装冲突修复（2026-10-26）

`0eca81f9b` 引入的 cryptography 49.0.0 与 DeerFlow Harness 2.1.0 的最低 50.0.0 要求冲突。此前依赖覆盖环境的 T1 缺少完整冷安装。修复 `db5289c7474a91966ab254efc0d61957c17d7479` 统一固定 50.0.2。

新 Python 3.12 环境的 Agent／Copilot 完整安装和 pip check 通过。Agent 导入及七场景门禁、Copilot 185 项通过。Common Python 421 项和 54 subtests 通过，1 项可选 GeoPandas 跳过，锁文件与 CI 登记通过。Linux arm64 Agent 产品镜像完成完整依赖安装、pip check 与导入。make test-changed 因缺少 Monitor PostgreSQL T2 连接而前置退出 2。

安装、镜像和输入散列在 `/tmp/addp-p19-crypto-fix-20261026-9oi78l3t/`。自然 Platform Run `40294294052` 当时仍在运行，后续 Linux 复核另见下一节。此项修复只覆盖冷安装冲突，公共 mTLS、Python HTTP 实链与 Trace T2 分开认定。

### 修复 SHA 的 Linux 复核与 HTTP 守卫取证准备（2026-10-26）

用户完成个人重启后继续专项。本会话仅只读核对已有自然 CI，不运行／重跑本地隔离环境、Hosted、容量或定时。固定 `db5289c7474a91966ab254efc0d61957c17d7479` 的 [Agent offline evaluation](https://github.com/pampa0629/addp/actions/runs/40294294052/job/121476467369)、[Common Python](https://github.com/pampa0629/addp/actions/runs/40294294052/job/121476467434)、[Copilot](https://github.com/pampa0629/addp/actions/runs/40294294052/job/121476467306) 原始日志均确认真实 checkout、selected=true 和 status=success；Agent 完整冷安装使用 Linux x86_64 的 cryptography 50.0.2 wheel，七场景及评测／持久化、Common 聚合通过；Common 421 项及 54 subtests 通过、1 项可选跳过；Copilot 185 项通过、3 项已有警告。至此依赖冲突修复具备该固定源码的 Linux 安装及标准测试证据，不只依赖个人重启或本地 venv。

同一源码的 [Monitor HTTP trace infrastructure](https://github.com/pampa0629/addp/actions/runs/40294293942/job/121476731711) 原始日志中 `TestOwnedTracePipeline` 实际执行 328.23 秒并通过：真实 120 秒尾采样、失败／慢请求及普通 5/200 保留；policy v2 短阶段独立 root、持久 Link 与目标撤权；Go→Python→Go、共享 Alloy→Loki、Tempo 投影、历史参与者、当前 User 撤权和采集器故障／恢复。328 秒是本次测试实际耗时，不是容量证明；直接枚举的 OwnedTraceProcess 是父测试使用的受控子进程入口，其顶层 SKIP 不替代父测试断言。退出回执确认当次容器／网络／卷及临时文件为零。当前追踪相关产品／夹具源与该 SHA 无差异，原“共享 ASGI 调整后真实 Trace T2 未运行”的缺口已由这份自然 Linux T2 证据补齐，不外推 Hosted／正式身份或长留存。

[同轮 HTTP quality](https://github.com/pampa0629/addp/actions/runs/40294293942/job/121476731996) 失败，与此前本地 mTLS 失败分开：日志在固定窗口的 Filesystem 测试刚开始时出现 `No such container` 与 Terminated，尚未进入 initial scrape 的 valid_sample 断言或 Go／Python 业务源。不能将它说成 Linux 复现了本地 TLS 根因。该 Job 只打印预算报告路径，既有 artifact 列表没有预算报告，故无法唯一确认是 inspect／卷读取时容器消失、资源超限还是其他守卫条件；清理日志显示执行了 down／移除，但缺少预算 JSON 的独立回执，不扩写为完整清零验收。

源码已确认两个编排风险：固定窗口 `runPromtoolCases` 使用同项目 `compose run --rm` 临时 Prometheus 容器，守卫 ps→inspect／exec 之间该容器可被自动删除；其继承 2 GiB 配额，与基础栈 2.75 GiB 合计为 4.75 GiB，不能通过忽略临时容器来满足 4 GiB。上述风险与缺失的当次失败详情分开陈述，不由静态可能性反推唯一历史原因。较早其他 SHA 的 Release workflow success 中 Metrics 个案可为 skipped，未将其算作本专项通过。

取证修正本地提交 `e65c8d7b1fde4e529238519b80a22eb8b1cc4681`：固定枚举／inspect／卷／私有目录失败步骤、超时／容器消失／命令失败子类与数值限额；抑制原始命令和 Docker 错误正文，stdout 输出封闭摘要；原 Linux HTTP case 的同一 Job 无论通过或失败均保存预算报告七天，缺失报告为失败。不重试、忽略容器、提高限额或延长完整四秒。`python3 -m unittest scripts/test/metrics-http-budget_test.py` 8 项通过，含守卫失败后清理也保持失败、报告与日志可见且不泄漏注入敏感内容；`make test-infra-runtime-log-lifecycle` 123 项本地 runner／既有 System Go fixture、69 项生命周期聚合退出 0。T2 登记 22／12／9／2、workflow YAML 与归档路径契约、`git diff --check` 通过。该取证修正当时尚未推送。

进一步只读查到固定 Nginx 1.28 支持跨 reload 继承 SSL 对象；[官方规范](https://nginx.org/en/docs/ngx_core_module.html#ssl_object_cache_inheritable)说明文件修改时间和文件标识未变时可继承。旧夹具覆盖原证书文件后只确认 reload 信号成功，未核对实际呈送证书；这加强了换证生效判定的调查价值，但仍缺少当次 inode／mtime／叶子摘要，不能认定该缓存就是原本地失败根因。下一步待确认方案仍需保留严格 TLS、原 4 秒／资源边界，先收敛 promtool 额外容器和证书加载判定，再决定仅公共前置的新窗口；不得自动进入 Python 业务实链。

原始归档见 `/tmp/addp-p19-http-budget-diagnostics-20261026-zd251axo/`。 原始复核、摘要和本地门禁日志位于 `/tmp/addp-p19-http-budget-diagnostics-20261026-zd251axo/`，`audit-summary.json` 区分固定源码通过、本次 HTTP 失败、未知触发原因与尚未运行范围，`artifact-sha256.json` 保存文件摘要。

### 公共 mTLS 单次前置窗口与容器内换证修正（2026-10-26）

用户确认先修 promtool 复用、实际 IP 换证生效与 TLS 分型，通过确定性门禁后只运行一次 20 分钟公共前置；4 CPU／4 GiB、私有可写 2 GiB、完整四秒、原 15 秒采样／65 秒等待保持，通过或失败均清理停止，不进入 Python 实链。实现 `603ba1ed766b5584a834194eba66864f4e6f1344` 新增原标准入口的 `--case=mtls-preflight`；promtool 使用既有 Prometheus 及专用只读规则目录，删除额外 run --rm；新前置在公共采样后退出并要求零业务计数及 TLS 证明。两次新增握手分别验证实际呈送摘要和 IP，每次 TCP／TLS 共享五秒，不重试；不改产品 API／权限或启动个人服务。

窗口前配置／预算 100 项（配置 92、预算 8）通过，补充前置成功仍须 TLS 证明及准确零计数后，预算最新 9 项通过；Monitor Go 标准模块门禁通过，包含真实 fixture 调用的 fake Docker T1，验证只 exec 既有中心、输入可读与退出删除，不冒充 promtool 真实公式执行。生命周期标准入口 123 项 runner／既有 Go fixture、69 项聚合通过；T2 登记 22／12／9／2、shell 语法及 diff 检查通过。首轮确定性检查有一项旧分支正则断言未包含新 case 而失败，已同步并取得后续上述通过，不将旧失败算通过。

唯一实跑：`ADDP_HTTP_BUDGET_REPORT=/tmp/addp-p19-mtls-preflight-20261026-hbhwfxwq/functional.json make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=mtls-preflight`，2026-10-26 18:00:38.942—18:00:54.356（Asia/Shanghai），15.414 秒，Make 退出 2／子入口退出 1，判失败。宿主原子替换证书的程序已经完成 stat／读取断言，随后来源容器的 `nginx -t` 明确报告 `/source/server.crt` 不存在，配置检查失败；尚未 reload、启动中心、执行固定窗口公式、新增 TLS 连接或自然采样。此结论只确认本轮跨宿主／VM 挂载路径的可见性失败；没有保留当时宿主／VM 详细文件状态，不据此推断更早初始采样 TLS 失败的唯一根因或归罪为已证实的 Docker 缺陷。

本轮输入 343 项摘要无漂移，两次资源检查最长 0.171 秒，峰值硬配额 1 CPU／640 MiB，私有可写预算上界 285495296 字节，未触发资源守卫。只有 source／node／当次 PostgreSQL 等准备资源，未启动 Python 或 Go 业务源，业务计数回执尚未形成，不能借退出清理成功计为前置通过。标准清理及独立按当次 project 精确过滤的只读复核确认容器／网络／卷为 0、私有工作目录删除。没有重试、续窗、Hosted、容量或定时。

失败后仅完成同范围的代码与确定性修正 `bd2ab54ef0662b09f64aa9bc7b4f59424f749100`：Nginx 唯一活动证书路径改为来源容器私有 tmpfs；通过 stdin 单次传递最终公开叶子，在 VM 内原子替换并记录文件标识／mtime 比较，私钥及信任根沿原输入不变；宿主文件仅刷新为下次启动引导输入，避免来源 stop/start 回退旧证书。删除在线宿主文件替换路线，不关闭 TLS、不扩大信任或增加容器／配额。发布回执即使后续配置失败也沿现有预算报告保留。T0 使用独立临时目录执行实际 stdin／cat／mv 生命周期，GNU stat 由受控模型模拟；不冒充 VM 文件系统实跑。最终配置／预算 101 项、生命周期 123 项 runner／既有 Go fixture及 70 项聚合、T2 登记和 shell／diff 均通过；修正后的 VM 换证与公共 mTLS 尚未运行，原单次窗口已消费，必须另行确认后才能再开。

原始归档见 `/tmp/addp-p19-mtls-preflight-20261026-hbhwfxwq/`。 原始报告、日志、独立清理回执及摘要：`/tmp/addp-p19-mtls-preflight-20261026-hbhwfxwq/`；`follow-up/` 单列失败后的源码补丁和确定性验证，`audit-summary.json` 分开标注失败源码、后续修正和未验收范围，`artifact-sha256.json` 保留原始文件摘要。

### 容器内换证修正后的公共 mTLS 前置通过（2026-10-26）

用户另行批准原上限的新 20 分钟窗口后，仅运行一次 `ADDP_HTTP_BUDGET_REPORT=/tmp/addp-p19-mtls-preflight-recheck-20261026-co9zukrk/functional.json make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=mtls-preflight`。准备、采样和清理合计 64.570 秒，2026-10-26 20:20:59.130—20:22:03.701（Asia/Shanghai），标准入口退出 0。运行前 HEAD 为 `1c4ec8df446f427da372a296646967879c486d6a`，实际换证实现为 `bd2ab54ef0662b09f64aa9bc7b4f59424f749100`；343 项运行输入摘要前后不变，相关实现输入与此前配置／预算 101 项、生命周期及 Monitor Go 门禁所验证的修正一致，未重复开发或重跑这些确定性检查。

实际容器 tmpfs 发布回执确认文件标识已原子变化。localhost 叶子验证及来源实际 IP 的严格 TLS 握手分别用时 0.003417／0.002220 秒，实际叶子摘要均匹配当次最终证书 `fc77ec0214b3daf7dfd29521b20871e4b884971a00957a63b29cb46bf15c56ec`。八类固定窗口检查（CPU、进程、HTTP、磁盘、磁盘 IO、网络、文件系统及 inode）实际复用既有 Prometheus 容器内的固定 promtool 并通过；真实 mTLS 自然采样、受控 Job 标签、中心自采样及 RSS 通过。不是仅有 reload 成功或宿主证书文件可读。两次握手均未重试，未放宽证书校验、原 15 秒采样／65 秒等待或完整四秒资源检查。

13 次完整资源检查最长 1.007224 秒；峰值硬配额 2.25 CPU／2952790016 字节内存（2.75 GiB），私有可写预算上界 938397696 字节，均低于 4 CPU／4 GiB／2 GiB 停止线。入口在公共前置后按约定退出，业务请求回执为 Go 0、Python 0／0、合计 0；没有进入 Go／Python 业务来源或 HTTP 查询／告警阶段。标准清理与独立按当次 project 过滤的只读复核均确认容器／网络／卷为 0，私有工作目录删除。没有自动重试、续窗、Hosted、容量或定时，亦未重启个人开发服务。

原始归档见 `/tmp/addp-p19-mtls-preflight-recheck-20261026-co9zukrk/`。

另对已有 [Linux HTTP quality Job](https://github.com/pampa0629/addp/actions/runs/40310193163/job/121529457879)（源码 `85d0482ce3169d945dfd37d3ca25e08913620156`、attempt 1）原始日志做离线核对：实际 IP／叶子验证、八类固定窗口、公共采样、ProcessSources 及 HTTPRealSource 已通过；`TestIntegrationMetricsHTTPAlertLifecycle` 在 216.41 秒处因恢复同一事件超时 `unknown/no_data` 失败，不能将整项 HTTP T2 记为通过。该 Job 560.728 秒、清理／输入守卫通过，不能归因为本次 mTLS 或资源超限。日志 `/tmp/addp-ci-20261026-120139/job-121529457879.log` 显示原生来源约 10:53:10 UTC 启动，告警在 10:56:13 打开、10:56:14 通知后切换健康；现有夹具每秒至多一次、总计最多 180 请求，健康切换接近发流结束是优先调查线索。尚未取得逐请求时间与指标窗口证据，不能由时间相近认定唯一根因。下一步优先离线定位受控发流、完整成功窗口及查询判定的关系，保留同一事件恢复断言、原请求总量／超时／资源及权限边界；本轮没有追加业务实链或重跑 CI。

### HTTP 夹具恢复预算修正与确定性验证（2026-10-26）

用户确认分阶段预留后，先修运行观测规范 6.4 和 T0–T3 验收条款，再修改测试夹具；本轮只授权确定性门禁，真实实链另行确认窗口。离线事实：上述失败 Job 的来源 10:53:10 UTC 开始，查询／历史趋势完成后规则仍须自然建立自身窗口，直到 10:56:13 才打开事件、10:56:14 切换健康。旧 Go 从进程启动即按每秒一次、最多 180 次发流，Python 90／30 次来源的设计发流时长同样约 180 秒，没有恢复预留。产品固定窗口以边界计数差计算，停流后失败率为零分母，归一和聚合保留 no_data，连续窗口不得恢复活动事件。这确认夹具存在独立于业务语义的预算分配缺陷；旧运行缺少阶段计数，不能据此声称已经唯一解释该次 CI 超时。

实际发流循环现使用失败／恢复两份预算：Go 90／90，Python 两源 45／45、15／15；失败阶段启动间隔 2／4／12 秒，恢复阶段 1／2／6 秒。只观察到既有健康切换才启用恢复额度，阶段单向推进；晚切换、失败额度耗尽、控制文件清理均不能消耗或重置恢复预算，不补发时隙或因切换突发。每源在实际发送前记录尝试总数及封闭阶段回执，包括额度、各阶段计数和最后一次尝试的相对单调毫秒数。Go 新回执沿原私有目录预建可写文件，不新增容器、权限或监听。产品 SDK、查询公式、身份授权、规则阈值／时长、同一事件及通知断言不变。

删除仅在 Python finally 发布旧计数文件的路线。唯一汇总内核在来源停止后、私有目录删除前读取三个来源，早期 Go 失败也保存已发尝试和未进入 Python 的事实；已启动来源缺失回执、未知字段、损坏／超额／总量不符均失败关闭，不输出原始内容。完整 HTTP 成功还须三源各有失败和恢复尝试；公共前置仍严格零业务计数。原 300 次、5 QPS／4 并发、20 分钟、4 CPU／4 GiB／2 GiB、完整四秒及失败清理边界均保持，没有以请求停止或无数据作为恢复。

实际标准验证：`make test-module MODULE=common GO_ONLY=1`、`make test-module MODULE=monitor GO_ONLY=1` 退出 0；`python3 -m unittest scripts/test/metrics-deployment-config_test.py scripts/test/metrics-http-budget_test.py` 108 项通过，执行实际调度类、晚切换、两阶段耗尽／节奏、源计数校验、实际清理回执程序和完整入口成功拒绝；`make test-infra-runtime-log-lifecycle` 退出 0，123 项本地 runner／既有 System Go fixture及 75 项生命周期聚合通过；`scripts/ci/check-t2-ci-registration_test.py` 38 项及登记 22／12／9／2 通过，shell 语法和本专项 diff 检查通过。新增用例沿 Common／Monitor Go、既有配置／预算／生命周期和原 Metrics T2 自动发现，不新增工作流或标准入口。回归先于调度实现报缺少内核；迁移后另一项旧断言仍读取已删除计数文件，修正为唯一汇总入口后取得上述最终通过，保留初始失败记录。

Monitor T1 实际覆盖停流零分母的归一、零请求权重聚合及活动告警不得恢复，并有持续成功请求的恢复对照；不是把未知改成健康。原 HTTPWindow 中另加“停止发流为未定义”和“成功请求产生真实零失败率”两项 promtool 场景，已编译但本轮未执行，其结果仍待原 T2。历史 CI 的失败不改记通过，修正后的真实 Go／Python HTTP 采集→查询→告警／通知仍未运行；既有 mTLS 通过属于旧固定输入，本次改动后的清理／阶段回执只有确定性证据。

原始归档见 `/tmp/addp-p19-http-phase-fix-20261026-sejp16cy/`。 源码补丁、输入摘要和门禁日志位于 `/tmp/addp-p19-http-phase-fix-20261026-sejp16cy/`，`audit-summary.json` 及 `artifact-sha256.json` 区分已确认夹具缺陷、历史原因未唯一确定和未运行层级。

### 恢复预算修正后的单次本地 HTTP 实链：准备阶段守卫失败（2026-10-26）

用户另行批准原上限完整实链窗口：准备及清理最多 20 分钟、当次容器合计 4 CPU／4 GiB、卷及私有可写 2 GiB 停止线、完整资源检查四秒、受控请求最多 300 次及 5 QPS／4 并发，失败即停并清理、不重试或续窗。仅运行一次 `ADDP_HTTP_BUDGET_REPORT=/tmp/addp-p19-http-full-20261026-u9f8dxrv/functional.json make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=http-quality`。启动 HEAD 为 `bdf8788266408209a1c3da6aab948a0c4321b248`，分阶段修正 `b7a3db50c8d4358966681c2fa485d9a2614e153c` 的相关实现输入无后续差异；本轮 343 项运行输入摘要未变化。

2026-10-26 21:58:51.178—21:59:57.776（Asia/Shanghai），66.598 秒失败，Make 退出 2、子入口被终止退出 143。前置 94 项配置确定性检查通过；实际来源 tmpfs 换证、localhost 叶子与实际 IP 严格 TLS 校验通过，两次握手 0.002179／0.001594 秒，最终叶子摘要 `800eb31537f747f571aeedddfce2918a2f561f8d6dbcf21045a71731315f5eb6` 匹配；匿名、外来 CA 和明文拒绝通过。随后在编译固定窗口测试二进制期间，守卫报告 `http_budget_sample_command_failed`，固定步骤 `private_directory_size`、类别 `command_failed`、命令退出码 1。守卫中止编排，不能将子入口 143 说成编译器自身诊断失败，也不能将配置单元测试内的模拟执行日志计为实际 promtool 场景执行。尚未进入真实固定窗口公式或 Go／Python 业务来源，业务计数 Go 0、Python 0／0、阶段均未启动，未运行恢复预算与告警闭环。

失败前 14 次完整资源检查最长 0.245005 秒；已取得样本的峰值硬配额为 2.25 CPU／2.75 GiB，私有可写预算上界 1406349312 字节。这些是失败前的成功读数，不证明失败读取的目录大小，也不能排除失败瞬间的变化；本次报告是目录命令非零退出，不是已确认的四秒超时或资源越线。标准清理和独立按当次 project 精确过滤的只读复核均确认容器／网络／卷为 0、私有工作目录删除；新的退出阶段汇总在来源未启动的早期失败下保留准确零计数及无阶段事实，不外推真实发流场景通过。没有复用剩余时间、另起环境、续窗、Hosted、容量、定时或个人服务操作。

离线只读核对：该步骤执行 `du -sk <当次私有目录>`；标准入口将 GOCACHE／GOTMPDIR／TMPDIR 都放在被扫描目录中，编译会并发创建／删除文件。“扫描时文件消失”仅为优先假设，原始 stderr 已由封闭守卫丢弃，当前报告未分类，不能排除权限、I/O 或其他错误，不能唯一归因。本轮没有改变／忽略失败、重试 du、缩小扫描范围或放宽预算。下一步建议先补充同一失败调用的有界封闭错误分类和失败耗时取证，保持非零即失败、完整四秒及全目录口径；在原因证据充分前不直接重开实链窗口。

原始归档见 `/tmp/addp-p19-http-full-20261026-u9f8dxrv/`。 本次只记录实际失败与清理；前一固定输入的公共 mTLS 通过、分阶段修正的 T0/T1 和已有独立 Trace 证据分别保留，完整 HTTP 查询／告警／通知仍未通过，不推送或手工触发／重跑 CI。

### 私有目录读取失败的封闭分类与耗时证据（2026-10-26）

仅对私有目录 du 固定 `LC_ALL=C`，按已捕获的最多 4096 字节 stderr 输出封闭类别：`path_disappeared`、`permission_denied`、`io_error`、`mixed`、`unclassified`、`empty`、`over_budget`。超限不保留前缀，无原文和路径；此处为 CLI 文本分类。

同一单调时钟分别保存命令耗时、整次采样耗时和命令启动时剩余预算，不重置四秒，不重试。旧 66.598 秒记录无 stderr，类别和根因不能回填。

预算 17 项、生命周期 123 项前置及 78 项聚合、登记 38 项通过。新增反例覆盖边界、未知／混合、脱敏、剩余预算、首败和无重试；实际 Docker、du 竞态及业务未运行。

诊断源码、输入摘要、红／绿及标准门禁日志保存在 `/tmp/addp-p19-directory-diagnostic-20261026-ap5xjngx/`，见 `audit-summary.json`、`artifact-sha256.json`。本次仅本地提交，不推送或触发／重跑 CI，不运行 Hosted、容量、定时或个人服务操作。下一步应在另行确认的单次准备阶段取证窗口取得真实目录失败分类，未得到该证据前不认定根因已修复，也不直接将完整 HTTP 实链记为通过。

### Linux Go HTTP 子链通过与 Python 来源启动诊断（2026-10-26）

只读核验已有自然 CI 原始日志：[bdf 的 HTTP Job](https://github.com/pampa0629/addp/actions/runs/40334346467/job/121612153803) 精确 checkout `bdf8788266408209a1c3da6aab948a0c4321b248`，Go `TestIntegrationMetricsHTTPAlertLifecycle` 实跑 152.28 秒通过，覆盖同一事件恢复及签名通知；后续真实进程／来源停止断言也通过。[206 的 HTTP Job](https://github.com/pampa0629/addp/actions/runs/40339327143/job/121630064969) 精确源码 `206261e751090a7bfb4e816dd0a8fbd97273d3d3`，相同 Go 子链实跑 150.27 秒通过。两轮均在 Python 发布身份前退出，finally 的非零退出异常再次覆盖首失败；日志回执分别 539.709／547.187 秒、Go 148／Python 0，输入未变且私有目录删除，但 cleanup_verified=false。不能继续将旧 Go unknown/no_data 当作这两轮的实际失败，也不能将 Go 子项通过或目录删除写成整项 HTTP／完整清理通过。

当时尚无 Python 启动子阶段。修正让原脚本成为唯一 stdlib 启动入口，沿原权限复制、安装并导入 SDK。分阶段回执仅保留枚举、退出码和字节数；最多解析 64 KiB 既有安装日志，超限不留前缀。

退出时独立检查两个来源、恢复发现范围，进程内 join 失败后也继续关闭；清理异常另记，首败保留。

配置／预算 116 项、生命周期 123 项前置及 78 项聚合、登记 38 项通过。首败被 finally 覆盖的反例先失败后通过。真实安装、Python 业务链、Hosted、容量未运行。

原始归档见 `/tmp/addp-p19-python-startup-20261026-y1zoacsw/`。

### 启动诊断的 Linux 实跑与同进程导入缓存修正（2026-10-26）

固定 `ffc478e1a8ad0c099d6d21d32477552df32b9abe` 的自然 [HTTP quality Job](https://github.com/pampa0629/addp/actions/runs/40342629801/job/121641589201) 已结束失败。Go 告警子链再次实跑 150.34 秒通过。真实生产者回执首次确认两源均在 imports 阶段 module_missing，安装命令已返回成功；两个退出检查和发现范围恢复均执行，日志保留 Python 身份发布前的首异常，不再被 finally 的退出非零覆盖。日志前部配置 T0 的 prepare 模拟回执不是实际生产者结果，本结论取后部真实回执。

官方 Artifact `12661186120`，19852 字节，下载 SHA256 核对为 `8af2b83c27dbb734ccbb89b68a987860cd6286cf96e878f0062392659467d9db`。预算报告 573.390 秒、整体 failed、输入未变；另有 inspect 非零读取失败（命令 1.022295 秒、检查累计 1.038813 秒、启动剩余 3.983482 秒），随后子入口 -15、cleanup_verified=false、private_directory_removed=false，最终预算未保存 Python 回执。真实启动事实仍在原 Job 日志，但不能称完整清理或 Python 实链通过。原始 ZIP、预算 JSON、日志及 linux-failure-audit.json 位于 `/tmp/addp-p19-python-startup-20261026-y1zoacsw/`。

新入口启动解释器时 `/tmp/deps` 尚不存在，同进程安装后导入可能仍受 PathFinder 负缓存影响。受控 bootstrap 旧版退出 1，安装后刷新缓存退出 0；未运行实际 pip。缺失组件只记录白名单类别，此结果不追认旧 shell 入口的根因。

另修正清理中重复 SIGTERM：仅在当次归属及 `cleanup_started=true` 均匹配时停止新采样并等待原清理，资源首败仍保留；原四秒和清理期限不变。旧报告未保存重叠标记，无法复原该次 inspect 失败时序。

两个反例先失败后通过；最终配置／预算 118 项、生命周期 123 项前置及 79 项聚合、登记 38 项通过。未开本地窗口或运行 Python 实链。

### Python ImportError 分层回执与清理 profile 修正（2026-10-26）

自然 [75c5c42b9 的 HTTP Job](https://github.com/pampa0629/addp/actions/runs/40347081176/job/121657200460) 仍失败，Go 告警子链实跑 153.45 秒通过；Python 失败从 module_missing 变为 imports/import_failed，仍在身份发布前，不能将导入缓存修正写成完整启动通过。577.072 秒的预算回执保留两源启动／退出事实、Go 147 次／Python 0、输入未变；failure_during_cleanup=true，子入口退出 1，私有目录已删除，证明没有再次 SIGTERM 打断这次清理。但 cleanup_verified=false，仍不能称零残留。Artifact 12663195763（20172 字节）下载 SHA256 已核对 d90f3b1552f8eb2fe5de071e04b89f8354cc2721909e14bd600e34756769162d；原始预算及日志在 `/tmp/addp-p19-python-startup-20261026-y1zoacsw/import-profile/`。

只读核对发现确定性清理遗漏：Python 来源通过 --profile python-http up 创建，原唯一 down 未启用该 profile，实际清理日志也未移除两个 Python 服务。[Docker 官方 profile 规则](https://docs.docker.com/compose/how-tos/profiles/)明确默认 down 不包含未激活 profile 的服务。现在唯一 down 同时启用 python-http，继续限当次 project，保留 --volumes／--remove-orphans，不启动任何新服务或更改资源／期限。shell 回归执行真实 cleanup 函数、替换 Docker 命令，原代码缺 profile 时失败，修正后验证同一 profile、原首失败仍非零及私有目录删除；不将受控回归当实际清零通过。

ImportError 尚缺具体组件与版本事实，本批没有猜测升级／降级。启动回执新增固定导入检查点、ImportError 组件白名单及实际安装的预声明发行包版本；未知模块为 unknown，版本仅短版本字符串或 unavailable，不留异常正文、任意模块路径或环境。失败发生在 FastAPI、Starlette、Uvicorn 还是 Common 导入时可以区分，异常所属组件与入口检查点分别解释，不从名称关联推断唯一根因。未运行本地依赖安装、容器、业务实链、Hosted、容量或定时。

配置／预算 120 项、生命周期 123 项前置及 80 项聚合、登记 38 项通过；真实清零及 Python 业务链另见后续固定 Job。

### Python 来源 FastAPI 导入定位与清理实跑核验（2026-10-27）

已核对源码 `e5391ca36843cd0d99233ea3f63949abb45d81d0` 的自然 [Linux HTTP Job](https://github.com/pampa0629/addp/actions/runs/40351996322/job/121674742810)。2026-10-26 23:43:58—23:53:23（Asia/Shanghai）的 HTTP 预算执行 565.507 秒，整体 failed／退出 1；Go 告警生命周期子链 160.70 秒通过，实际 Go 148 次（失败 90／健康 58），Python 两源均 0。两个来源在身份发布前的 imports 阶段、fastapi 检查点报 import_failed，异常组件 unknown；没有异常符号原文，不能将检查点当作唯一根因。安装完成后的发行包回执均为 FastAPI 0.115.0、Pydantic 2.14.1、Starlette 0.38.6、Uvicorn 0.32.0、HTTPX 0.28.1、Common 0.1.14、cryptography 50.0.2、OTel 三包 1.45.1、prometheus-client 0.26.0、protobuf 7.36.2；这些版本事实不等于已证明版本冲突，亦不代表两个正式消费者的运行栈。

本轮 error_code／failure_detail／failure_during_cleanup 均为 null，输入未变；两个来源都完成退出检查，发现范围恢复，唯一带 python-http profile 的 down 后 `cleanup_verified=true`、`private_directory_removed=true`。清理修正已有这次真实 Linux 零残留证据，整体仍因 Python 导入失败而失败；Go 子链通过不代替 Python SDK→mTLS→采集→Monitor 查询／告警的验收。官方 Artifact 12665281043 为 20228 字节，下载 SHA256 已核对 `0cf19a17c641506751c71d472e8868421609c794841d48d4a6822b293b4534dc`。原始日志、ZIP 提取预算和独立摘要位于 `/tmp/addp-p19-python-startup-20261026-y1zoacsw/import-profile/`（`job-121674742810.log`、`linux-budget-e5391ca36.json`、`linux-e539-audit.json`）。

离线核对另发现两项夹具事实。第一，两个测试源都硬编码 FastAPI 0.115.0／Uvicorn 0.32.0，未使用 Agent FastAPI 0.142.2、Copilot FastAPI 0.110.0 的各自正式 HTTP 依赖声明，不满足技术栈规范中分别验证消费者版本的要求。第二，唯一安装目标 /tmp/deps 位于未指定 exec 的 256 MiB tmpfs；[Docker Moby v28.5.1 实现](https://github.com/moby/moby/blob/v28.5.1/daemon/oci_linux.go#L487-L500)默认合并 noexec／nosuid／nodev，[Linux mmap 规则](https://man7.org/linux/man-pages/man2/mmap.2.html)拒绝从 noexec 文件系统建立可执行映射，而 Pydantic 依赖本地扩展。这证明配置与本地扩展加载要求相冲突；本轮未记录实际 mount 标志或扩展 ImportError，仍不能称已唯一还原本次异常，也不能猜测降级 Pydantic。

后继[挂载与正式依赖修正](#python-测试来源本地扩展挂载及正式-http-依赖修正2026-10-27)处理已确认的配置缺口。该固定 Job 的异常组件及唯一根因仍未知。

### Python 测试来源本地扩展挂载及正式 HTTP 依赖修正（2026-10-27）

两源共用的 256 MiB tmpfs 显式设 `exec/nosuid/nodev`，保持非 root、只读根、cap_drop ALL 和 no-new-privileges。安装前核对实际挂载标志，安装后要求 Pydantic Core 本地扩展通过 ExtensionFileLoader 加载；回执只保存挂载及导入事实和白名单字段。

两个来源分别提取正式 requirements.txt 的 FastAPI、Uvicorn（含 extras）、HTTPX、Pydantic 声明，缺项或重复拒绝，删除共享夹具版本。两份声明已纳入既有 CI 选择，仅代表各自 HTTP SDK 夹具。

原挂载／实际标志／依赖提取与扩展加载四项反例先失败；首次聚合仍用旧回执结构，更新后配置／预算 127 项、生命周期 123 项前置及 80 项聚合、登记 38 项通过。没有真实容器、依赖安装或产品导入。

补丁、红／绿门禁日志及输入／选择摘要位于 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-p19-python-native-fix-20261027-040bs6so`。本轮没有新开本地实链窗口、运行 Hosted、容量或定时，也没有修改／重启个人服务。修正后的实际挂载、本地扩展与两源 SDK→mTLS→采集查询告警仍待自然 Linux Metrics CI 单列核验，不将本地确定性通过算作完整 HTTP 实链通过。

### 正式 HTTP 声明两源 Linux 业务子链通过，整门禁仍因清理重叠读取失败（2026-10-27）

已核对修正源码 `44230c5c8d733b737145735a2432b31cb4891c3d` 的自然 [Linux HTTP Job 122046852189](https://github.com/pampa0629/addp/actions/runs/40461400796/job/122046852189)。14:30:26.448—14:44:05.065（Asia/Shanghai），预算执行 818.616 秒，整体 failed，子入口退出 0；没有手工触发／重跑或另开本地窗口。正式版本提取和本地扩展修正已取得实际运行证据：两源 startup 均 running／passed，实际挂载 noexec=false／nosuid=true／nodev=true，native_imported=true。Agent 使用 FastAPI 0.142.2／Uvicorn 0.32.0／HTTPX 0.28.1，Copilot 使用 0.110.0／0.27.0／0.27.0；两源 Pydantic 2.14.1、Pydantic Core 2.50.1。原启动阻塞已跨过，不需猜测降级 Pydantic；此夹具只复用 HTTP 四项直接声明，不是完整产品依赖、正式身份或页面验收。

固定窗口公式 0.62 秒通过；Go 实际查询／趋势、告警打开／签名重试／同一事件恢复（150.38 秒）与来源停止隔离通过；Python 两源 SDK→mTLS→唯一发现与自然完整一分钟→Monitor 查询／历史趋势、告警打开／签名重试／同一事件恢复（164.07 秒）及来源停止隔离通过。两源都正常退出，发现范围恢复，failure_stage=null。实际 Go 148（失败90／健康58）、Python 75（45／30）和28（15／13），总计251，三源均有失败及恢复尝试且未超原额度。产品子链通过与整门禁通过分别陈述。

整门禁仍因资源采样失败：stage=inspect、kind=command_failed、退出1，命令1.075732秒、整次检查至此1.091369秒、命令开始剩余3.984363秒，并非四秒超时；failure_during_cleanup=true。最终 cleanup_verified=true、private_directory_removed=true、inputs_unchanged=true，零残留及原首失败均保留，不将资源失败忽略或改判通过。201个成功采样最长1.226349秒，样本峰值硬配额3.25 CPU／4026531840字节内存、私有可写预算上界1807519807字节；这些只代表已成功取得的样本，不推断失败读取瞬间或容量通过。

已确认旧流程允许多步采样与 down 重叠；本次未保存原始 stderr 或对象，不能将 inspect 退出 1 唯一归因于容器消失。后继[原子交接](#http-采样清理原子交接实现与确定性验证2026-10-27)补上互斥。

官方 artifact `12715410863` 下载摘要已核，原始日志、预算及独立核对见系统临时目录 `addp-p19-python-native-fix-20261027-040bs6so`。整门禁保持失败。

### HTTP 采样／清理原子交接实现与确定性验证（2026-10-27）

当次私有目录的唯一锁覆盖完整采样与资源删除。采样在四秒开始后取锁，再核 project、目录及清理标记；已标记则交接，不发读取或添样本。清理先发布标记，取锁后由 Shell 继承 FD9，覆盖唯一 down、零残留检查和目录删除。取锁失败不宣称清理完成。

锁等待和回执读取消耗原四秒；清理锁也沿原 120 秒绝对期限，逾期即使清零仍失败。真实 inspect／du 失败保留首因，不重试，不走无锁删除。

真实私有锁、受控线程及 Shell 回归覆盖交接两种顺序、FD 继承、期限、归属及首败。父提交反例失败，最终配置／预算 135 项、生命周期 123 项前置及 88 项聚合、登记 38 项通过。

原始门禁、红／绿对照、补丁与输入摘要在 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-p19-http-handoff-20261027-0slhhjwm`。本地没有新建容器或实际运行HTTP／Hosted／容量／定时；修后的真实整体结果待唯一自然Linux HTTP Job单列核验，确定性通过不计为完整门禁通过。

### HTTP 采样／清理交接修正后的 Linux 整门禁通过（2026-10-27）

用户确认保持原四秒及全部边界的互斥修正；已核对精确源码 `fa7ae6dc34af264fb14c31c48b048fc26514efb4` 的自然 [Linux HTTP Job 122070268771](https://github.com/pampa0629/addp/actions/runs/40468624807/job/122070268771)，Job及实际Verify isolated metrics lifecycle步骤均success。预算15:42:29.017—15:56:25.320（Asia/Shanghai）共836.303秒，result=passed、子入口退出0、error_code／failure_detail均null；输入未变，cleanup_verified=true、private_directory_removed=true，原归属容器／网络／卷及临时文件零残留日志实际通过。官方Artifact 12718461481为23818字节，下载SHA256已核对 `d7e5a6d8717498c06b6b194a9bce95724c495eacb93dba5913f3eaaccfdd0c50`；原始日志、ZIP、预算和独立摘要位于 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-p19-http-handoff-20261027-0slhhjwm`（job-122070268771.log、linux-budget-fa7ae6dc3.json、audit-summary.json）。不是只根据Job摘要判通过。

固定窗口公式0.78秒、Go真实来源查询／趋势及告警打开／签名重试／同一事件恢复152.43秒、Python两源对应告警150.29秒，停止来源的缺样和其他来源隔离均通过。Python两源startup=running/passed、native_imported=true，实际挂载noexec=false／nosuid=true／nodev=true；两源均正常退出、发现范围恢复、failure_stage=null。实际Go148（失败90／健康58）、Agent SDK67（45／22）、Copilot SDK26（15／11），合计241，三源均保留失败／恢复记录且未超原额度；本次没有请求借额、补样、停流恢复或身份／告警断言放宽。两个模块沿各自正式HTTP四项直接声明运行，不代表完整产品依赖和正式身份验收。

205次完整资源检查全部小于四秒，最长1.285895秒；采样锁等待最长0.000208秒，清理锁等待0.000446秒，未触发清理超期。本轮在原20分钟内同时完成业务、预算与退出清理；成功样本的容器硬配额峰值3.25 CPU／4026531840字节内存、私有可写预算上界1807532095字节，均未触线，这些不是CPU／RSS实际用量或容量基线。确定性门禁的135项配置／预算、123＋88项生命周期及登记结果沿前述固定修复有效。

本轮仅为标准Linux HTTP T2：真实Go与两个ASGI SDK来源→mTLS／HTTP SD／Prometheus→Monitor查询／自然一分钟／告警与通知闭环及资源守卫清理通过；不冒充个人本地实链窗口、完整Agent/Copilot正式backend登录或页面、正式密码/MFA T4、Hosted、容量、72小时保留或全workflow通过。旧44230c5c8业务子链通过但整门禁失败的证据仍保留，历史inspect退出1缺少stderr及对象的唯一原因仍未知，不因本轮通过倒改历史归因。本次未手工触发／重跑／取消CI，未新增本地窗口、Hosted、容量或定时，也未重启个人服务。下一步建议先明确正式Agent/Copilot产品进程及页面的HTTP观测本地验收边界，再申请运行窗口。

<a id="http-history-sampling"></a>

## 私有目录采样与 Go 编译协调

<a id="go-cache-reclaim-deterministic-20261028"></a>

### 2026-10-28 中途Go缓存回收互斥与独立四秒门禁（仅确定性验证）

用户确认先修规范和确定性门禁，真实新窗口另行确认。实现`2ffd8eb30a2a934f5b3cbb0244b9daa2d0cd4b63`沿唯一共享构建回收内核替换无锁shutil.rmtree，正式Trace与HTTP本地入口均复用，不保留旧删除路径。本批没有启动Docker资源、正式后端、HTTP业务请求或新的真实验收窗口；仅有小型临时文件／原生子进程夹具，不属于产品实链或容量负载。

Go编译命令成功后，先做一次完整资源预检，锁等待、读取／解析合计四秒，读取不重试；有既有失败、预检失败或达到4 GiB即停，不能先删缓存掩盖触线。通过后回收复用sample_lock，锁等待和可中止删除另有独立四秒合格预算；删除仅限当次私有源码.cache/go-build，拒绝越界／符号链接。采样仍继续，自身原四秒和完整统计范围不变；两步分别记时，不合并成更长的扫描预算。

删除子进程未退出或状态未知时不解锁继续；清理会针对该受管子进程终止／回收，仍不能确认退出则保留锁并返回清理失败，不删除相关环境或宣称零残留。成功与失败均保存封闭预检／删除时序及退出状态；严格模式锁等待回执改用实际拿锁时刻计算，不再把总扫描起点误当拿锁时刻。未改变权限、产品API、采样策略／保留、原60分钟／8 CPU／12 GiB／4 GiB上限。

验证：`make test-infra-runtime-log-lifecycle`退出0，包含HTTP23项、Trace及共享回收136项、既有生命周期88项，以及相关System Go夹具／CI登记检查。覆盖完整预检先于删除、预检触线不删、两个方向的采样／删除互斥、绝对预检截止包含锁等待、预检文件消失不重试、删除锁等待扣减预算、返回0但耗尽预算仍失败、子进程终止／未知状态不放锁、原生删除子进程确实回收及越界／符号链接拒绝。向既有用例注入原无锁删除行为时，用例以cache removed while active sample owns lock如期失败；恢复新路径后全部通过，不把该预期红例记为正常门禁失败。

原始归档见 `/tmp/addp-p19-cache-reclaim-preparation-current`。 **未运行**修正后的正式Go编译→镜像→身份→HTTP／页面闭环，未运行Hosted、容量或定时；此前dcf窗口继续保持“运行失败、清理通过”。上次最后一次失败采样没有完整可写值，修正不证明4 GiB必然足够；新窗口若预检触线仍按原标准停止，不能通过回收补成通过。

<a id="http-linux-ci-20261028-private-directory-recurrence"></a>

### 2026-10-28 Linux HTTP T2目录消失复发：独立入口离线核对

只读核对既有[Release/T2 attempt 1日志](https://github.com/pampa0629/addp/actions/runs/40660674352/job/122734708027)，源码`cd0c537d57eb7c3a704ec6cbadfc3bb3d8a41b73`；没有派发／重跑CI，没有新建本地环境。原始日志第936行HTTP完整预算JSON与本机提取文件逐字段一致：result=failed，elapsed_seconds=185.364465198，error_code=http_budget_sample_command_failed，failure_detail.stage=private_directory_size、kind=command_failed、exit_code=1、directory_error=path_disappeared，stderr_bytes=212；du命令0.024313秒、完整采样0.729208秒，**不是四秒超时**。failure_during_cleanup=false、inputs_unchanged=true、cleanup_verified=true、private_directory_removed=true；记录请求Go=0、Python=[0,0]，不能计为HTTP业务闭环通过。聚合Job122763262513传播该失败，不另计一次独立产品故障；其他自然成功不关闭此间歇失败。

当前`metrics-http-budget.py`与失败源码的文件SHA256同为`4056b0d679c10ebcf272a072a5d8539635c7ea7e979af64e1d96d20f6fcacbcf`。Linux入口为Make `test-monitor-metrics MONITOR_METRICS_TEST_ARGS="--case=http-quality"` → metrics-http-budget.py → monitor-metrics-gate.sh，按整个当次WORK_DIR执行du；其go-cache、go-tmp、tmp均纳入该根目录，并由标准脚本在原生`go test -c ./processmetrics`之前设置GOCACHE／GOTMPDIR／TMPDIR。现有跨进程锁只覆盖采样与退出清理，并未将编译器内部文件生命周期纳入锁协议。

本地`2ffd8eb30`回收修正属于另一条入口：monitor-http-local-gate.py复用monitor-traces-local-gate.py的reclaim_go_build_cache，仅保护显式删除当次源码.cache/go-build。Linux预算入口没有调用该函数，故不能用本地136项共享内核检查宣称已覆盖Linux目录消失。相反，Linux失败源码与当前HEAD都已包含同一共享回收内核（文件SHA256=`b3a934a250034eafaff1359b5afd4cb019fec7ce9285e2a01b0ec476a936dfe6`），进一步说明“包含修正提交”不能替代“执行路径覆盖”。本节也不把独立Linux故障与上一节本地CLI列表等待合并。

原始stderr及缺失路径未保存；现有记录只能确认du遇到路径消失，不能唯一确定根目录、Go缓存、Go临时构建对象、其他短命文件或实际删除者。尚未改采样／编译生命周期，不忽略du、不排除目录、不缩小统计或放宽Linux原4 CPU／4 GiB／私有可写小于2 GiB／完整四秒／1200秒预算。下一批最小建议是先讨论封闭的缺失对象类别／身份摘要、根目录存在性及编译阶段单调边界取证，再修改对应规范和确定性门禁；不先猜测互斥范围，也不盲目重跑。

原始归档见 `/tmp/addp-ci-20261028-100049`。 该核对只证明上述固定源码／单次CI失败与路径覆盖关系；未新增产品实现或真实验收，不运行Hosted、容量或定时。

<a id="http-linux-minimal-forensics-20261029"></a>

### 2026-10-29 Linux HTTP目录与编译最小取证（确定性及自然CI分列）

用户确认先修规范／确定性门禁，再沿自然Linux CI验证；实现`05c57272c8116a19cc9ca955cf9f97bafdaf9b82`仅修改既有HTTP独立预算入口及其标准shell接入，不修改产品API、编译／采样互斥或资源上限。缺失对象只保留预定义目录类别与当次project域下的相对身份SHA256，严格解析至多4 KiB／八行的GNU/BSD诊断；越界、穿越、歧义引号／控制字符及重复斜杠不推测路径。根目录观察用lstat，不跟随链接；原四秒耗尽不追加读取，诊断费用计入完整采样。首du失败不改判、不重试、不排除缓存或临时目录，原4 CPU／4 GiB／私有可写小于2 GiB／1200秒仍保持。

编译开始／观察返回以同一主机monotonic_ns原子写入原归属回执，保留非零退出码；中途取消只见开始时记incomplete，不补造结束。du失败另外保留采样／命令的原始单调秒端点、采样取得锁后读取回执时的编译状态，最终编译观察另列，不将两者混成失败当时状态。数值时序只证明观察关系，不唯一证明删除者；旧日志仍无实际缺失对象，不能反推。

验证：既有生命周期Make入口先通过HTTP本地23项／共享内核136项及相关Go夹具，随后被共享工作区中另一专项surface-area持久化注册检查阻断，整条入口不得计通过，未改动该专项。继续执行该入口原有确定性子命令：`python3 -m unittest scripts/test/metrics-http-budget_test.py scripts/test/infra-runtime-log-lifecycle_test.py scripts/test/trace-deployment-config_test.py scripts/test/trace-capacity_test.py scripts/test/postgres-metrics-lifecycle_test.py`合跑96项通过；最终预算取证单独35项通过；既有Metrics配置脚本120项、shell语法、T2 CI登记及diff检查通过。新增重复斜杠拒绝用例初次失败后修正，通过仅指当前取证契约。夹具只有小型临时文件、真实du缺失根读取和受控Go替身子进程（含进程组取消），没有Docker、本地服务或业务负载。

自然push运行[40799494798](https://github.com/pampa0629/addp/actions/runs/40799494798)、HTTP Job[123204132217](https://github.com/pampa0629/addp/actions/runs/40799494798/job/123204132217)，固定实现SHA如上，push／attempt 1已通过，HTTP生命周期及预算artifact上传步骤均成功；未手动dispatch或rerun。原始artifact `12865618656`共24238字节，ZIP SHA256=`ccac6dfe1557dec98cce5e19f912908e93f5f88dae37c59204ea1189877e681b`与API登记摘要一致，CRC完整；唯一monitor-http-budget.json SHA256=`bbb5008607a5310a26f639ca497d85ff650d745829a827d58ed9b4fc4a9f84a8`。独立逐文件核对报告353个输入全部匹配该提交，未以当前HEAD替代。

真实Linux报告result=passed、exit_code=0、error_code=null，elapsed=854.099548秒；209次完整样本最长1.182123秒，私有可写保守上界最大1808158783字节，容器硬配额合计最大3.25 CPU／4026531840字节内存，均在原上限内。请求Go148（失败90／健康58）、Python Agent75（45／30）、Copilot28（15／13），总251次，三源阶段和计数契约一致。process_compile为finished／exit0，原始单调纳秒开始64979234562、观察结束138843033209；inputs_unchanged、cleanup_verified、private_directory_removed均为true。**本次没有复现目录消失，缺失对象及根目录失败诊断现场分支未触发**；编译边界实录和单次Linux功能门禁通过，不等于旧间歇故障已修复／删除者已定位，不用本次成功覆盖此前失败。

原始归档见 `/tmp/addp-p19-http-minimal-forensics-20261029.json`、`/tmp/addp-p19-http-minimal-forensics-linux-result-20261029.json`。 没有新开本地窗口，不运行Hosted、容量或定时，不恢复此前失败窗口；正式产品到页面闭环仍未验收通过，本批取证不替代它。

<a id="http-linux-gotmp-recurrence-20261030"></a>

### 2026-10-30 Linux HTTP复发：go_tmp后代对象与编译边界覆盖缺口

固定源码 `3f43b9bff04fdccb3d689c3efea8a9b9b783e44d`，[自然push attempt 1／run40903636868／HTTP Job123552811295](https://github.com/pampa0629/addp/actions/runs/40903636868/job/123552811295) 失败。

artifact `12911921470` 的 353 项输入匹配固定提交，日志与报告一致。首败 `http_budget_sample_command_failed`，阶段 `private_directory_size`，du 退出 1／`path_disappeared`。命令 0.028348 秒、完整检查 0.656893 秒，未耗尽四秒。首败发生在清理前，业务请求为零，输入稳定、清理及私有目录删除均已核验。

缺失对象分类 `go_tmp`。当次根散列对照排除了 go-tmp 目录本身，指向其后代，私有根目录仍存在。未保留原路径、对象名称或删除进程。

旧 `process_compile` 只记录已完成的 Common 来源编译。HTTP 查询、告警两次后续编译共用 GOTMPDIR，却无边界回执；失败时具体阶段无法复原。后继[三阶段取证](#http-three-compile-stages-20261030)补齐此缺口。

后续 [run40979523356／HTTP Job123816052278](https://github.com/pampa0629/addp/actions/runs/40979523356/job/123816052278) 独立通过。固定源码 `6d5ca9401ac62ac85d660d9ca957d28780de4209`，artifact `12944927519`，353 项固定输入，208 个样本最长 1.264080 秒，251 请求，三源阶段、编译和清理通过。该次没有复现目录消失，历史删除者及根因仍未知。

原始归档见 `/tmp/addp-p19-http-gotmp-recurrence-audit-20261030.json`、`/tmp/addp-ci-20261029-160021`。

<a id="http-three-compile-stages-20261030"></a>

### 2026-10-30 三阶段编译取证：规范、确定性与自然Linux分列

固定实现 `3a004a94759f0414030d99c992e057c2651c2cb5` 将单条 `process_compile` 替换为 Common 来源、HTTP 查询、HTTP 告警三个编译边界。采样时快照与最终观察分列，越序、重复、缺失及取消不完整保持原义，首败不被回执写入失败覆盖。编译和采样协议未改。

生命周期标准入口的 100 项、配置 122 项及登记通过。受控 Go 替身验证两个正式 probe 调用、顺序、异常／取消、首败与缺失阶段拒绝；删除两个 HTTP 回执的反例失败。无本地 Docker 或真实负载。

[run40984761553／HTTP Job123833677076](https://github.com/pampa0629/addp/actions/runs/40984761553/job/123833677076) 自然首轮通过，artifact `12946559397`，353 项输入独立匹配。

报告通过，823.080908 秒，202 个样本最长 1.042991 秒，Go 163／Python [75,28]／合计 266 请求；输入稳定、清理及私有目录删除均已核验。三个阶段按序完成且均退出 0，无旧字段。

本次未复现目录消失，缺失对象现场阶段快照未触发，未定位 `3f43b9bff` 的删除者或根因。

原始归档见 `/tmp/addp-p19-http-three-stage-preparation-20261030.json`、`/tmp/addp-p19-http-three-stage-linux-audit-20261030.json`。

<a id="http-process-source-gotmp-recurrence-20261030"></a>

### 2026-10-30 三阶段命中复发：Common来源观察区间与可变临时树

固定源码 `23bcfb82cfb88be2ca8eb1ca728d6c1c6bb51f22`，[自然push attempt 1／run40999198941／HTTP Job123881246445](https://github.com/pampa0629/addp/actions/runs/40999198941/job/123881246445) 失败。

artifact `12954002076` 的 353 项输入与固定提交匹配。首败 `http_budget_sample_command_failed`，阶段 `private_directory_size`，du 退出 1／`path_disappeared`。命令 0.023743 秒、完整采样 0.579913 秒，退出 143 为其后的取消。无业务请求，清理与私有目录删除已核验。

缺失对象为 `go_tmp` 后代，私有根目录仍在。采样及最终回执均为 Common 来源 `incomplete`，查询和告警 `not_started`。未看到 Common 返回只界定观察区间，不能确定采样瞬间的进程状态、删除者或路径。

日志确认实际Go工具链为go1.24.2 linux/amd64、GOTOOLCHAIN=local，Common来源保持CGO_ENABLED=0。只读核对该固定版本的[Builder工作目录／关闭](https://github.com/golang/go/blob/go1.24.2/src/cmd/go/internal/work/action.go#L283)、[增量对象清理](https://github.com/golang/go/blob/go1.24.2/src/cmd/go/internal/work/exec.go#L1873)及[产物移动](https://github.com/golang/go/blob/go1.24.2/src/cmd/go/internal/work/shell.go#L120)：Go合法路径会创建GOTMPDIR下go-build树、清理中间对象目录，安装／输出还可能重命名源文件。这证明活动编译临时树本来允许变动，完整du遍历与合法移除可并发；它支持采样模型竞态的机制判断，不唯一证明本次散列对象由上述哪一个调用删除。

`-work` 无法禁止产物重命名。后继[当次 Go 组稳定读取](#http-stable-read-owned-go-20261030)验证保持全量读取、四秒及首败口径的协调方案。

原始归档与日志见 `/tmp/addp-ci-20261030-043005`，固定输入、对象及 Go 源码核验见 `/tmp/addp-p19-http-process-source-recurrence-audit-20261030.json`。正式产品页面及停源历史读取仍未覆盖。

<a id="http-stable-read-owned-go-20261030"></a>

### 2026-10-30 当次Go组稳定读取：规范、确定性与自然Linux分列

用户明确允许仅对当次Go编译进程组增加暂停、采样和恢复协调，取代此前不改互斥的边界。先修订运行观测及T0–T3验收规范，再提交实现`4e5487386c1df11236d95b37daa822e66f6e5e4b`：三个固定阶段只经同一受管调用内核启动，独占当次会话／进程组，先自停、在原采样锁内登记平台出生身份、UID、组／会话及直接调用者后才运行。组长保留到持锁回收，禁止下一阶段越过正在采样的锁；发布清理事实与编译事实使用同一当次元数据锁，避免相互覆盖，不增加全局锁或第二编译路线。

采样只对核验归属的当次组发一次SIGSTOP，两次连续成员集合一致且存活成员均停止后才沿原容器／卷／私有目录完整读取，finally恢复并确认不再停止。未知／复用／外来组不发信号；成员不稳定、超预算、根目录缺失及真实du失败均保留失败。锁等待、身份核验、暂停确认、全量读取、恢复与最终判定合计仍须在原四秒内；50毫秒恢复预留从四秒内扣减。截止后必要恢复不构成成功，取消／超时须终止回收自有组；清理先接管且等待调用者回收，未证明回收不删除材料／宣称零残留，首失败不被恢复或清理错误替代。原4 CPU／4 GiB、私有可写小于2 GiB、1200秒整体／1080秒工作／120秒清理、请求和采样节奏保持，不缩小统计、不忽略du、不重试、不扩展到产品服务。

确定性标准入口`make test-infra-runtime-log-lifecycle`最终退出0，包含HTTP本地23项、Trace共享136项及相关Go／CI登记、预算与生命周期聚合120项；`python3 -m unittest scripts/test/metrics-deployment-config_test.py`122项通过，shell语法、T2登记及diff通过。预算套件51项覆盖归属／成员变化拒绝、暂停确认、finally恢复、同四秒、根缺失、取消／超时回收、提前清理交接及首因保留。原生Darwin小型组夹具真实变动少量文件，证明读取期间停止、成功／注入du失败后恢复、超时及清理回收；模型覆盖Linux身份／状态契约，不能代替自然Linux实跑。没有启动本地Docker、真实Go编译负载或新验收窗口。

取消夹具另在`74277133b`修正：heredoc会耗尽标准输入，原read等待可能提前退出，改为与EOF无关的受控等待；仅修夹具，生产协调不变。修后预算51项及上述标准生命周期聚合再次通过。向现有暂停模型回归注入空协调时按预期断言失败，恢复当前实现后通过；早前原生故障注入还伴随夹具收尾超时，单列为不充分红例，不用其证明根因。准备清单和最终日志位于`/tmp/addp-p19-http-stable-preparation-20261030.json`及同前缀lifecycle-cancel-final／config-final／cancellation-final／unpaused-red日志；各层证明分列。

实现提交的[自然push attempt 1／run41021354128／HTTP Job123949620339](https://github.com/pampa0629/addp/actions/runs/41021354128/job/123949620339)于2026-10-30 16:00:11（Asia/Shanghai）completed／success。原始日志确认实际checkout及Go／Python三源、查询／历史趋势、事件／签名重试及同事件恢复、停源隔离通过。取消夹具后续提交不是手动dispatch／rerun，不用当前HEAD替代本次固定实现源码。

原始artifact12962652612共24971字节，实际ZIP SHA256=`733ccd6a69e3de2218b17bd2ebacc9d67bbeacf0a7342c9fc62dfe51152d2b3b`与API登记一致，CRC和唯一JSON成员通过；报告SHA256=`d89a186c79d3565424bebe0d74ac4294305f6daa1c4b53ef989217632fa3b57e`。353项输入独立逐文件匹配固定4e5487386提交，原始日志的摘要与报告逐字段一致。最初两次只读归档下载因网络握手超时失败，后用同一官方归档成功取回；没有重跑作业，不把传输失败记作CI失败。

真实报告passed／exit0／error null、无failure_detail，总800.279188秒；196个完整样本最长0.961270秒，可写保守上界最大1809215551字节（含原tmpfs预留，不称实际磁盘用量），硬配额最大3.25 CPU／4026531840字节内存。Go148／Python[81,30]，总259请求，三源失败／恢复阶段计数与原额度独立核对一致；inputs_unchanged／cleanup_verified／private_directory_removed=true，无cleanup_overdue或compile_cleanup_error，原四秒／资源／1200秒预算满足。

26个样本确实发出当次组暂停，均confirmed／resumed=true、无resume_error，最大5成员；协调起点至恢复结束最长0.606839秒，计入各自完整样本，所有单调端点、阶段区间及最终三阶段顺序独立核验通过。实际暂停覆盖process_source和http_alert；http_query仅约0.823秒，未与四秒采样节奏重叠，不能宣称其实际被暂停，接入同一内核另由确定性回归证明。三个且仅三个编译阶段均完整exit0、旧process_compile字段不存在：

| 阶段 | 开始monotonic_ns | 观察结束monotonic_ns |
| --- | --- | --- |
| process_source | 89175094379 | 148101521593 |
| http_query | 220272646669 | 221095373087 |
| http_alert | 221095831692 | 266597071141 |

原始归档见 `/tmp/addp-p19-http-stable-linux-20261030.audit.json`。 本次未复现目录消失，不能以成功反推历史删除者或保证长期不复发；Linux正常路径与Darwin取消／失败夹具的证明层级分列。

本批针对活动Go临时树与整树读取并发的机制，不宣称已经观测到历史散列对象的删除进程；历史失败保留。正式Agent/Copilot产品后端到Console／Monitor页面、停源后历史读取等原有缺口不被此协调修正替代。没有手动重跑、新开本地窗口、Hosted、容量或定时。

<a id="http-stop-confirmation-recurrence-20261030"></a>

### 2026-10-30 稳定读取后停止确认超时复发与最小取证（根因未定）

此前4e5487386的196完整样本／26次暂停全部恢复的通过记录按原范围保留。本次另核[自然push attempt 1／run41028885736／HTTP Job123973797116](https://github.com/pampa0629/addp/actions/runs/41028885736/job/123973797116)，固定源码fe105602d93195d9b2343032547f1aa74ec10fba，fresh API为completed／failure。已通过4e548、失败fe105及本次取证修正前父提交的预算内核blob均为23218d8f6b866fada508d0292defbb964c66714f；没有把旧通过当作新回归已经解决，也不调查无关CI或聚合传播失败。

独立复核artifact12964943223：18041字节，ZIP SHA256=`bcd45aaad3efa2180a3d54372643241f8b7b1fa7b4e65d76286281ca5093ada4`匹配API，CRC及唯一JSON成员通过；报告SHA256=`cf920a3da52fb6962227132384ed7df81d00a4948c38a9c60e2f9bc89d590e2b`，353输入逐文件匹配固定fe105源码，原始日志第913行摘要逐字段匹配。原报告failed／http_compile_coordination_deadline，stage=compile_inventory、kind=deadline；总47.799267秒，0业务请求，process_source只见开始551089735254ns，query／alert均未开始。compile_cleanup_error为空、inputs_unchanged／cleanup_verified／private_directory_removed=true，退出143是首失败后的取消结果，不改写首因。

失败协调562580569121..566590882118ns，共4.010313秒，stop_sent=true／resumed=true，无confirmed字段，未进入完整du采样；恢复发生在原成功截止耗尽后，仍然失败，不因清理通过补业务通过。members=3是暂停前初始清单，不能当作失败时最后成员或状态。之前9个已保存完整样本中两次编译协调成功（初始1成员及2成员）；本次没有保存确认循环内的读取次数、状态或成员变化，不能由compile_inventory标签唯一断言一次读取耗时过长，也不能断言是D状态、fork竞态或历史目录删除。

只读源码确认实际工具链go1.24.2 linux/amd64、GOTOOLCHAIN=local；该版本[Linux进程启动](https://github.com/golang/go/blob/go1.24.2/src/syscall/exec_linux.go#L305)默认加入CLONE_VFORK／CLONE_VM。参考[Linux v6.11父进程vfork等待](https://github.com/torvalds/linux/blob/v6.11/kernel/fork.c#L1569)可见父进程等待子进程完成，组暂停可能与启动等待相互阻塞；这是待检验机制，不是当次runner内核版本或现场状态的证明。Linux同版本还专门[延迟传播fork期间组信号](https://github.com/torvalds/linux/blob/v6.11/kernel/signal.c#L1171)，不能仅凭fork子进程初始pending为空就断言漏收SIGSTOP。当前不改停止顺序、不补发信号、不将D等非停止状态接纳为安全，不以推测性优化代替事实。

先修观测／T0–T3规范，再提交`f49f59ad39d2d50f0aef69335ca74aa5c1fdb830`补取证缺口：只复用原确认清单读取，记录尝试／完整返回次数、成员集合变化次数、最后尝试起点及最后一次完整读取的单调起止和固定R／S／D／T／t／Z／X／I／other状态计数。仅保留一份完整快照，不保存成员PID、出生身份、原始状态、命令、路径或原始stderr，不增探针／信号／读取；清单未返回与多次快速返回分列。取证开销仍在原四秒及原资源预算内，一次组暂停、全成员停止、完整统计、finally恢复、首失败及清理期限不变；这是诊断修正，尚未修复或唯一定位实际超时根因。

确定性反例先在旧代码缺少confirmation事实时按预期产生断言失败（2用例／3分支，无额外错误），补齐后预算53项通过；逻辑单调时钟分别模拟稳定D／T组反复返回和一次清单耗尽截止，二者均拒绝进入读取、保留首inventory截止并恢复，且证明没有额外组读取或信号。另覆盖成员变化、未知状态封闭归other、只保留最后完整快照及不泄漏身份。这只证明模型／取证契约，不证明真实失败就是该模型。`make test-infra-runtime-log-lifecycle`退出0，包含HTTP本地23项、Trace共享136项及相关Go／登记、预算与生命周期122项；Metrics配置122项、T2登记24／12／9／2及diff通过。新增取证的[自然push attempt 1／run41039374832／HTTP Job124008442804](https://github.com/pampa0629/addp/actions/runs/41039374832/job/124008442804)已于2026-10-30 18:30:33（Asia/Shanghai）completed／success，实际checkout为固定f49f59ad3；没有手动dispatch／rerun。

原始artifact12970980011共26383字节，实际ZIP SHA256=`60252c77529d8833ffc004a3d5c0c371dfbee976e6a08d6603b206d6ada334ce`与API一致，CRC及唯一JSON成员通过；报告SHA256=`f76e4116094d08f9a7016b0ef80f4214d881157497f65c2e4b8215073f82dd5e`，353输入逐文件匹配固定修正源码，原始日志摘要逐字段匹配报告。真实三源采集／查询／历史趋势、告警事件／签名重试／同事件恢复、停源隔离通过；本轮未复现确认超时，不用新成功覆盖fe105失败或宣称超时根因已修复。

报告passed／exit0／error null，无failure_detail／cleanup_overdue／compile_cleanup_error；814.608862秒，200个完整样本最长1.144740秒，可写保守上界最大1809215551字节、硬配额最大3.25 CPU／4026531840字节内存，原四秒和资源／1200秒均满足。Go160／Python[67,26]，总253请求；三源失败／恢复计数与原额度、三阶段完整顺序exit0和无旧process_compile、输入稳定及零残留清理独立核对通过。31次实际暂停全部confirmed／resumed=true、无resume_error，最大5成员，协调跨度最长0.564704秒，计入各自完整样本；覆盖process_source／http_alert，http_query较短未与采样重叠，不补算其实际暂停。

31份confirmation均有attempts=completed=2、membership_changes=0，最后尝试与最后完整快照的起点一致，单调起止均在确认完成之前，固定九类状态的合计不超过64且末次只有T／t／Z／X，未保存原始身份。新增成功字段已在Linux真实接通；一次读取耗尽截止、稳定D／T不确认及未知状态other仍只由确定性反例覆盖，不能宣称已采到实际失败状态。下一步若后续自然复发，先用新计数／端点／末次状态区分等待、成员变化和单次读取耗时，再决定最小修正，不先把D纳入安全或改暂停顺序。

原始归档见 `/tmp/addp-p19-http-confirmation-recurrence-audit-20261030.json`、`/tmp/addp-ci-20261030-100013`。

<a id="http-stop-confirmation-real-dt-20261101"></a>

### 2026-11-01 自然 Linux HTTP 停止确认超时复发：取得真实 D／T 快照，根因未定

固定源码 `11db5f98b47f38d462e59190885f39e62ab4be57`，[run41215610415／HTTP Job124583446674](https://github.com/pampa0629/addp/actions/runs/41215610415/job/124583446674) 自然首轮。实际生命周期失败，预算归档成功；父 workflow 后续取消，HTTP Job 已结束并保持 failure。

artifact `13048079382` 与登记摘要、唯一 JSON 成员、353 项固定输入及原日志核验一致。

首败 `http_compile_coordination_deadline/compile_inventory`。Common 来源 `incomplete`，HTTP 查询／告警未开始，零业务请求，未执行本轮 du。

协调 4.004761687 秒，465 次尝试、464 次完整返回、0 次确认期间成员变化；最后完整清单耗时 0.003513946 秒，D=1、T=2。最后一次未完整返回，现有快照不能把四秒归到最后读取，也不能证明所有返回的状态相同。

members=2仅为发停止前的初始清单，末次完整计数合计3；membership_changes只比较停止后的确认清单，0不代表暂停前后成员未变。没有PID／成员角色／父子关系／等待位置，不能判D来自组长还是后代、I/O还是启动等待。[Linux官方proc文档](https://www.kernel.org/doc/html/latest/filesystems/proc.html)说明D为不可中断等待，wchan可提供等待位置但可能为0；[上游Linux v7.0的fork实现](https://github.com/torvalds/linux/blob/v7.0/kernel/fork.c)保留vfork父进程等待子进程完成的路径，但上游代码与容器日志所见7.0.0-1017-azure版本名不能证明此runner具体补丁及现场阻塞栈。仅属待检验机制，不将D视为安全，不变更停止顺序、补发信号、放宽四秒或声称根因已定位。

暂停已恢复，退出 143 为失败后的取消。首败发生在清理前，输入稳定、清理和私有目录删除均已核验，无编译清理错误或清理超期。真实等待位置和角色未覆盖；后继[有界取证](#http-bounded-wait-evidence-20261101)单独认定。

原预算及核对见 `/tmp/addp-p19-http-stop-recurrence-20261101.zip` 及同前缀报告、审计。

<a id="http-bounded-wait-evidence-20261101"></a>

### 2026-11-01 停止等待位置与成员关系的有界取证实现（自然 Linux 待验）

用户确认[真实 D／T 快照](#http-stop-confirmation-real-dt-20261101)后的取证范围后，先修运行观测与T0–T3条款，再修改唯一`metrics-http-budget.py`及既有测试；实现提交`086ae6ada785264813dec96ace94d64bc190ae5d`已推送main。仅4个本专项路径／片段，运行观测规范用暂存补丁隔离，只包含停止确认条款；另一会话未提交的PostgreSQL来源恢复说明及其他共享工作区修改未混入提交。未改产品API、权限、指标、停止顺序、清理期限或任何预算。

确认持续至少100毫秒且完整清单仍有D成员时，每暂停最多一组，沿当前最多64成员的已核验清单，聚合组长／其他成员九类状态与D父成员到T／t子成员的直接关系计数。仅D成员读取一次Linux wchan，每份最多128字节，达到上限整份拒绝分类；proc目录以O_NOFOLLOW／O_DIRECTORY固定，stat前后绑定出生、UID、会话／组和D状态，不因PID退出复用而换读新对象。权限不足、消失、身份／状态变化、无效／空／0、超长、读取失败及平台不支持均固定分类，无提权、重试、额外组清单或信号。Linux stat解析复用一个实现，无另一条身份路线。

报告仅新增confirmation.wait_observation，包含固定角色／状态、候选等待类别及关系计数、剩余未尝试D成员数和组单调起止；没有PID、出生身份、原始符号／命令／路径。进程启动等待候选、I/O候选和completion类别只帮助后续定位，不当作根因。该组是100毫秒后取得的一次观察，不冒充最后清单；原last_completed及最后尝试端点继续保留。逐次读取沿原绝对截止，预算耗尽停止且不再补读；D仍不得通过停止确认，恢复、首因和原4 CPU／4 GiB／私有可写小于2 GiB／1200秒保持。

| 本轮实际验证 | 结果与边界 |
| --- | --- |
| 旧暂停实现的受控反证 | 将修改前paused_compile函数装入当前替身测试，触发阈值及465次重放两项均按预期断言失败，0额外错误；不执行proc、信号、Docker或真实负载 |
| 预算／Metrics配置组合测试 | 首次186项中1项新增夹具在macOS未显式选择模拟Linux，返回not_supported而失败；修正夹具后186项通过。随后把剩余数量字段明确命名为unattempted_d_members，最终输入另由下两项完整覆盖，不把字段改名前的结果直接归为新输入 |
| `python3 -m unittest scripts/test/infra-runtime-log-lifecycle_test.py scripts/test/metrics-http-budget_test.py scripts/test/trace-deployment-config_test.py scripts/test/trace-capacity_test.py scripts/test/postgres-metrics-lifecycle_test.py` | 最终输入133项通过；为原Make生命周期入口的既有子命令，含64项预算测试、新增11项等待取证测试及原生小型进程组／清理夹具。Linux身份／proc适配为替身及临时文件，Mac原生组协调分列，不当作Linux真实等待位置已取得 |
| `python3 -m unittest scripts/test/metrics-deployment-config_test.py` | 最终输入122项通过，含原预算声明、来源配置、采样及清理编排；不启动真实Docker或Go编译负载 |
| `make test-t2-ci-registration` | 74项通过；现有24个hosted-service、13个gate-owned Compose、9个hosted-only、2个owner-managed登记检查通过，仅登记验证，不表示运行Hosted |
| 语法／CI覆盖／自有差异 | 通过；既有Make生命周期已调用预算测试，T2来源声明包含预算内核，原Release/T2 HTTP矩阵实际调用及预算归档覆盖新字段，无新增依赖、入口或workflow |

模型覆盖100毫秒前及无D不读、一次触发、64／65边界、只读D、每份128字节、目录固定及符号链接拒绝、前后出生／UID／会话／组／状态变化、权限／消失／读错误／0／无效／超长及固定候选分类、角色／关系计数、无原始身份泄漏。真实465次尝试／464次完整、末次D1／T2的模型仍在原四秒失败且恢复，不进入du；取证耗尽截止后不再读其他D成员，恢复失败不覆盖原守卫首因。以上证明取证契约，没有修复或唯一定位真实超时机制。

原始归档见 `/tmp/addp-p19-http-wchan-red-20261101.log`、`/tmp/addp-p19-http-wchan-staged-20261101.json`。

截至2026-11-01 09:03（Asia/Shanghai），该固定源码自然push生成[Release/T2 run41259443591](https://github.com/pampa0629/addp/actions/runs/41259443591)和[Platform CI run41259443660](https://github.com/pampa0629/addp/actions/runs/41259443660)，均queued／无结论，HTTP矩阵尚无实际结果，不能计Linux／真实取证通过。该 Release 候选的实际结果见[固定 086 预算归档](#http-bounded-wait-linux-success-086-20261101)。后续只手动核对本专项实际步骤和原始归档，未触发rerun／dispatch或设置定时；不新开本地窗口，不运行Hosted或容量。前述11db／fe105失败保留；正式Agent/Copilot产品到Console/Monitor页面、停源历史及原长期保留等缺口保持。

<a id="http-bounded-wait-linux-success-086-20261101"></a>

### 2026-11-01 固定086自然Linux HTTP整门禁通过，持续D取证未触发

固定源码 `086ae6ada785264813dec96ace94d64bc190ae5d`，[run41259443591／HTTP Job124714607784](https://github.com/pampa0629/addp/actions/runs/41259443591/job/124714607784) 自然首轮。早期排队和运行中的快照已由实际生命周期成功、预算归档成功及 Job 终态取代。

artifact `13065999126` 原 ZIP 成员通过登记摘要、CRC、唯一 JSON 成员、353 项固定输入和实际 checkout 核验。

原始报告通过，870.748097 秒，214 个完整样本最长 1.24847829 秒，守原时限与资源限额；输入稳定、清理及私有目录删除均已核验。

38 次实际暂停均确认并恢复，无恢复错误，末次清单仅 T／t／Z／X。Common 来源 21 次、HTTP 告警 17 次，查询阶段未重叠。三阶段均按序成功退出。

38 份 `wait_observation` 全为 null。持续至少 100 毫秒且仍有 D 的分支未触发，真实 wchan、D 角色／父子关系及不可用分类均未覆盖；11db／fe105 的超时根因仍未知。

Go 148、Python [75,29]、合计 252 请求。真实 Go 和两个 ASGI SDK 来源的 mTLS、来源隔离、查询／历史趋势、自然窗口、503 告警、签名重试、恢复同一事件及停源隔离通过。正式 Agent／Copilot 产品构建、密码／MFA、Console／Monitor 页面、停源后历史读取、Hosted 和容量未覆盖。

原预算 ZIP 与 Job 日志在 `/tmp/addp-ci-20261101-103029/`。同次输入、38 份确认及预算、请求和清理核验在 `/tmp/addp-p19-http-086-followup-20261101/`。

<a id="http-preparation-directory-categories-20261103"></a>

### 2026-11-03 Linux HTTP准备目录消失调查及固定分类扩展

用户明确将独立Linux HTTP采样失败纳入只读调查，随后确认仅扩展六个准备目录分类、先修规范和确定性门禁；不改采样／互斥、不新增字段或读取、不推送触发构建。该问题与[Monitor APT 索引失败](#http-full-monitor-apt-index-failed-20261103)分开，不合并根因。原失败为[Release run41566037638／Job125708236073](https://github.com/pampa0629/addp/actions/runs/41566037638/job/125708236073)，自然push／main／attempt1，固定eb8af32813dea19088d6190d7633e725b645c67a；原日志SHA256=`7ed6018e0f70c48d7459e9055da2dced46c00e6db97ff7390fb0f79ded8cb027`。日志确认配置136项通过后，实际HTTP预算入口失败，不能用后续143退出或汇总Job替换首因。

官方artifact13205376286为18191字节，ZIP SHA256=`3cf25b73229e407f7528d59c89cfaff668ebe59ab23dbf478ea3363b6ad8de57`，CRC及唯一monitor-http-budget.json成员核验通过，JSON SHA256=`9d92e3c6018a43db9ef48679170bfe7ad18a40e73740bb6efd369e8420377327`。首次完整采样未形成成功样本：error_code=http_budget_sample_command_failed、stage=private_directory_size、kind=command_failed、exit1，du耗时0.002965秒／剩余预算3.982260秒、采样累计0.020797秒；112字节原stderr只记录封闭path_disappeared、一个other对象哈希，根仍directory。三个compile_stages_at_sample_read均not_started、HTTP总请求0，failure_during_cleanup=false；cleanup_verified和private_directory_removed均true，不反转本轮failed，不计业务或完整资源检查通过。

只读核对失败固定源码与当前相关七项输入（预算实现及测试、shell编排、配置测试、private-state、身份准备及配置渲染）逐字节相同，未发现已有修复。源码显示回执发布后即允许扫描私有根目录，准备流程仍有原子发布／临时名称删除，且不在Go组或退出清理协调范围内，存在可解释此类故障的并发机制；但既有固定相对路径候选均未匹配对象哈希，原日志无du路径正文，不能确认本次具体对象、删除者或唯一根因。旧other不按新映射追填，不据目录位置断言原子发布已被直接观测。

实现提交`f0aa912a8abdd04869b703c0a18ff815d5457f3a`：先修运行观测及T0–T3条款，唯一directory_objects映射仅增加postgres-auth→postgres_auth、tls→tls、deployment→deployment、node-tls→node_tls、control→control、query-tls→query_tls。只匹配当次根的精确首层，目录本身和后代同类，未知及嵌套／近似同名仍other；原字段结构、project域相对SHA256、4 KiB／八非空行、归属／穿越／歧义／超限及首失败语义保持。AST反向去掉六项后整个预算模块与准备基线163a24be8完全相同；编排、来源发布、Make和CI登记均未改。

| 本地确定性门禁 | 实际结果 |
| --- | --- |
| 旧映射反证 | make test-infra-runtime-log-lifecycle中145项／44.175秒出现预期14个断言失败，新增类别仍other；不计通过 |
| 首轮修正后的测试误报 | 同入口145项／48.542秒有3个断言失败：旧脱敏断言把允许的tls／deployment／control类别名误作泄露，修正为允许精确类别名，仍拒绝原根路径、后代文件名和SECRET哨兵；不修改产品解析器绕过断言，失败日志保留 |
| 最终配置入口 | make test-monitor-metrics-config退出0，136项／11.952秒通过；标准替身，不是真实部署 |
| 最终生命周期入口 | make test-infra-runtime-log-lifecycle退出0；HTTP44项／0.771秒，Trace169项／4.541秒，各自JS／登记／System身份Go子门禁通过；预算／生命周期／留存／PG等145项／103.378秒通过，较原142项新增3项并扩展原目录身份测试 |
| 原离线TLS子项 | OpenSSL4.1.0、35命令／0.678283秒、材料与输出预留125477字节、4次预期拒绝、清理完成；不等于新产品环境或网络验收 |

新增回归覆盖六目录／临时后代／深层后代、GNU/BSD格式、首层精确性、跨根／project散列与字段不变、未知／近似／嵌套／恶意路径、去重／混合／超限和无新读取；六种类别逐一经原sample失败分支，保持原du命令、两次原有命令调用、首失败和零重试，不继续后续统计。完整四秒、4 CPU／4 GiB、私有可写<2 GiB、1200秒及清理边界不变。确定性组的总测试耗时不是单次资源检查预算，不据此放宽四秒。

原始归档见 `/tmp/addp-ci-20261103-130018/`、`系统临时目录／addp-p19-linux-http-directory-investigation-20261103-ekw91775/audit.json`。

本轮仅本地自有提交及57主题证据归档，并回报维护会话；没有推送、重试构建、新开本地窗口、Hosted、容量、定时或接管个人服务。分类实现及确定性门禁通过，不计修改后的自然Linux采样或正式Agent／Copilot到Console／Monitor页面闭环通过；目录消失是否消除、具体删除者及APT根因均未验。下一步只在后续另行获准的原运行中使用新增类别定位，再讨论必要的准备／采样协调，不现在猜测删除者加锁。

#### 2026-11-03 20:42 远端包含性与自然CI排队核对

远端 main 快照 `785839a5772c1539fe8909d8dc59337b3daacb24` 已包含分类实现 `f0aa912a8abdd04869b703c0a18ff815d5457f3a` 和证据提交 `49e08b6ac`；只核对已有运行，未重复推送。

[自然 Release run41613913398](https://github.com/pampa0629/addp/actions/runs/41613913398)，固定源码 `64a6a3b16fbf639a051d96061502cfe694840135`，预算实现、测试与编排均匹配分类实现；该运行源码尚不含后续证据提交。20:42 快照仍在排队，仅见选择 Job `125842451761`，不计执行。实际 HTTP Job 与预算见[最终核验](#http-natural-linux-budget-passed-20261104)。

<a id="http-natural-linux-budget-passed-20261104"></a>

### 2026-11-04 固定Linux HTTP Job125925007987及原始预算通过

用户要求继续手动核对唯一固定HTTP Job，未手动重跑或开启新窗口。本轮北京时间2026-11-04 05:58取得最终API、完整原始日志及同次预算artifact：[Release run41613913398／HTTP Job125925007987](https://github.com/pampa0629/addp/actions/runs/41613913398/job/125925007987)为main／自然push／attempt1，固定源码`64a6a3b16fbf639a051d96061502cfe694840135`。Job于北京时间2026-11-03 22:41:39—22:55:39实际运行并success，覆盖前次22:40仍queued的历史快照，不改写其当时事实。实际checkout SHA与API一致；该固定源码含f0aa912a8准备目录分类，预算实现／测试／shell与已验证输入一致。整run API也为success，但本次仅核验HTTP专项，不外推其他Job或全仓验收。

| 实际步骤／原始记录 | 结果及边界 |
| --- | --- |
| 真实入口 | Verify isolated metrics lifecycle于22:41:57—22:55:34执行成功，日志明确make test-monitor-metrics及MONITOR_METRICS_TEST_ARGS=--case=http-quality；配置136项／17.694秒通过后进入真实Linux场景，没有将选择／跳过冒充实际执行 |
| 公共前置／Go来源 | mTLS、实际IP换证、匿名／错误CA／明文及非法转发拒绝通过；真实Go进程采集、固定窗口／Monitor查询、来源隔离与停源可见，HTTPWindow／HTTPRealSource实际PASS |
| Go告警 | HTTPAlertLifecycle实际153.34秒PASS，原生请求／固定Prometheus窗口、事件与outbox、签名通知、503重试及新成功窗口恢复同一事件的日志断言完整 |
| Python来源与告警 | 两个真实ASGI SDK来源启动、身份隔离、自然分钟窗口、Monitor查询与历史趋势通过；第二组HTTPAlertLifecycle实际172.87秒PASS，签名重试／同一事件恢复及Python停源隔离通过，不能冒充正式Agent／Copilot产品镜像或Console页面 |
| 编译及原预算 | report passed／exit0／error_code=null，816.455593秒<1200；三个compile_stages均完整、按序且exit0。197次完整样本最长0.969338409秒<4；26次编译采样均confirmed／resumed，无恢复错误。报告未保存每次采样开始端点，采样间隔只由原运行期五秒守卫通过证明，不另造独立间隔最大值 |
| 资源／流量 | 硬配额峰值3.25 CPU／4026531840字节内存，完整私有写上界峰值1542680639字节，分别低于原4 CPU／4 GiB／2 GiB停止线；逐样本核对work＋volume＋writable＋tmpfs等于总量。Go148次、Python75／28次，共251≤300；三源均有失败／健康两阶段且各自预算内，健康最后请求晚于失败阶段 |
| 清理与失败语义 | cleanup_verified=true、private_directory_removed=true，日志确认当次容器／网络／卷及临时文件零残留；无compile_cleanup_error／request_traffic_error／cleanup_overdue，无failure_detail或目录消失。本轮未复发不证明历史删除者或准备并发风险已修复，不据此增加重试或放宽du失败 |

Preserve HTTP functional budget evidence于22:55:34—22:55:35成功，官方artifact13239563817名为monitor-http-budget-41613913398-1，26623字节；下载ZIP的SHA256=`c3b7e325ca3baa7e226409c479eaef427a29f58d110dc3f051d718fe6e56e4b8`与GitHub digest一致，CRC及唯一monitor-http-budget.json成员核验通过。原JSON148561字节，SHA256=`f5605587decceb00c1be9788a037730963aa2b0b1b8089370d52b08356baa6e5`；368项input_sha256逐文件与固定64a6a3b16源码一致，inputs_unchanged=true，日志末尾封闭预算摘要所有字段与原JSON逐项相同。完整日志134242字节，SHA256=`8d572e324e55c06b69550171c23b348d1c34659dc67a3c3b2efeed917e10cef1`；没有只依据绿色徽标或下载摘要判断通过。

原始归档见 `系统临时目录／addp-p19-http-125925007987-20261104-pvikyz67/`。 原eb8af3281准备目录失败保持独立failed，本轮是后继固定源码的一次Linux HTTP T2通过，不宣称根因修复、稳定性、正式密码／MFA产品页面、Hosted或72小时容量验收；未运行Hosted、容量或定时，未新开本地窗口。

<a id="http-linux-resource-windows-managed-20261104"></a>

### 2026-11-04 Linux HTTP漏管指标窗口编译：本地确定性修正

用户先授权只读调查Job126260838041，再确认四阶段最小修正；本批不推送、不手动重跑CI、不新开窗口，不运行Hosted、容量或定时。原Release push Run41731193298／attempt1／固定04ad3e809fdd8e986a093c455f5aace6862052b4的原日志SHA256为429d31702c824503b29640404bd10bfd7a72dcc85b7dba340bac24fb77e77f9f；官方归档HTTP-13281545392.zip为21026字节，SHA256 03ebfdf971cf25a01763412fa7ce0cd5ac81928964118e9a45a165931463d228，CRC通过，内含monitor-http-budget.json的SHA256 e99921a3fd587db1175bb8382448f3eec65ef8daa24349bde2aaca7e6354cf10。原件在/tmp/addp-ci-20261104-073221/，本轮只读核验本地原始归档，不刷新远端Job状态。

失败为126.532236秒时http_budget_sample_command_failed，private_directory_size／path_disappeared；du命令0.018746秒、整次采样0.598849秒，尚余命令预算3.420006秒，不是四秒或1200秒耗尽。目录类别go_tmp，已知go-tmp根目录的当次域散列不匹配，故只能定位其后代；私有根目录仍为directory，原始子路径未保留，不能由哈希反推出名称。failure_during_cleanup=false、HTTP请求0、最终cleanup_verified及private_directory_removed为true，不把清理成功反转采样失败。

固定源码的check_resource_windows在正式probe顶层无条件调用，内部原先直接subprocess.run执行go test -c ./internal/resourcequery，未接入run_owned_compile。原日志北京时间07:41:08.8235109记录窗口二进制编译开始，随后07:41:29.5153736为Terminated，没有对应执行已编译检查的完成后标记。失败采样快照只记process_source在主机单调135453284000ns成功结束、http_query／http_alert未开始；采样开始156.152423103秒、du开始156.732417397秒，分别晚于Common编译完成20.699139103／21.279133397秒。由于漏管窗口编译未登记组，采样在没有compile_group时走普通读取。这是已确认的编排覆盖缺口，解释了三阶段回执不能代表全部活动编译；并未直接观测具体删除进程或精确删除时刻，不将stdout墙钟与主机单调时钟直接校准。28个完整样本中16个含原编译协调，历史故障仍独立保留。

核对时共享HEAD c97ae1a543ab62fcfde314d4641cbce3bcbd06a7与原370项输入为358项相同／12项不同，不能称全输入一致；sampler、其测试及probe当时逐字节相同，shell新增node-recovery分支／输入声明，原HTTP编译正文未变。既往369／370匹配属于当时快照，也不覆盖本次共享HEAD。只读方案证据在/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-p19-linux-go-tmp-readonly-20261104-cxfar_va/assessment.json。

本次先修运行观测及T0–T3规范，再将唯一COMPILE_STAGES改为process_source→resource_windows→http_query→http_alert，窗口编译复用已有run_owned_compile，不保留直接编译旁路。Go argv、cwd、环境、180秒准备期限及后续二进制60秒不变，编译／执行均计入原1200秒。原暂停、出生身份／会话／组校验、锁、完整读取、finally恢复、首因及清理函数均未改；源AST对照仅probe的check_resource_windows函数改变，预算内核仅阶段枚举变化。完整四秒、4 CPU／4 GiB、私有可写<2 GiB、du失败语义、统计范围及全部身份／请求／清理断言保持。旧报告按历史事实保存，不补造第四阶段回执或按新标准追认通过。

旧实现受控反证沿make test-infra-runtime-log-lifecycle执行，最终148项中6项断言失败／2项错误，命中新增阶段、正式窗口函数仍直编译及新阶段不被原内核接纳，退出2。修正后make test-monitor-metrics-config退出0，136项／16.281秒及来源恢复前置通过；make test-infra-runtime-log-lifecycle完整退出0，最终148项／69.597秒。后者含HTTP runner44项／1.086秒、Trace runner183项／5.916秒、JS／登记／System身份Go子门禁全部通过；原离线TLS T1为OpenSSL4.1.0、35命令／0.943794秒、125493字节、4次预期拒绝及清理完成。回归从正式源码提取并执行窗口函数，验证受管调用先于两个HTTP编译、同一内核、确切参数／预算及仅执行编译后的二进制；失败／信号／超时／取消不执行旧文件、缺窗口阶段和前序失败不能通过。同一原生小型替身分别覆盖process_source和resource_windows：采样期间写入不变，模拟du失败仍传播，恢复后阶段成功结束；原超时／取消／组回收及严格du失败反例继续通过。替身不是真实Go构建或Docker负载。

本批实施快照、AST审核及verification.json在/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-p19-resource-windows-20261104-oubmruea/，日志/tmp/addp-p19-resource-windows-{red,config,lifecycle}-20261104.log；输入基准共享HEAD为5f4984a6c671facdad5024a413e719d9b01a2f5d，独立文件摘要绑定本次门禁。结论仅为受管覆盖缺口已本地修正、确定性门禁通过；自然Linux四阶段真实运行、历史删除者／间歇稳定性及正式Agent/Copilot到页面full仍未验收。下一步须用户另行允许推送后跟进自然Linux HTTP及原始预算归档，不手动重跑或追加窗口。

<a id="http-window-fixture-outer-receipt-20261104"></a>

### 2026-11-04 四阶段CI配置前置回归：测试环境隔离的本地修正

Run `41792712024`／Job `126473639226`（`4786f12ace3fe9e619db710210e8b61a88f0344c`，attempt 1）和 Run `41792903360`／Job `126477749395` 均失败。配置夹具继承外层尚未发布的 `ADDP_HTTP_BUDGET_RECEIPT`，读取失败产生 4 项 ERROR。真实四阶段、发流及清理均无回执。日志在 `/tmp/addp-ci-20261104-resume-patrol/`。

本地基线 `2d35280aafc2e86463df4f6f8054c9ba1779c546` 叠加配置夹具修复。局部环境隔离未发布回执，父环境及产品四阶段不变。保留 180 秒准备、60 秒执行及旧二进制拒绝条件。旧夹具两项反证错误，修正后配置 137 项、生命周期 148 项及 HTTP 44／Trace 183 等子门禁通过。证据在 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-p19-window-fixture-20261104-uukj5y5k/`。

两个原 Job 保持失败。本地修正当时未推送，真实窗口、历史删除者与正式页面闭环未验。修正后的固定 Linux 实际结果见[四阶段自然 Linux 实跑](#http-natural-four-stage-budget-passed-20261104)。

<a id="http-natural-four-stage-budget-passed-20261104"></a>

### 2026-11-04 四阶段与窗口夹具修正：自然Linux HTTP实际通过

用户明确允许推送4d5072b7b并跟进自然CI；只读核对时该提交已随共享main进入远端，故本会话没有重复推送或额外触发运行。最新两个绿色Release的HTTP矩阵被skipped，不作为通过证据；本次锁定真正选中的[Run41816593195／HTTP Job126544907993](https://github.com/pampa0629/addp/actions/runs/41816593195/job/126544907993)，push／main／attempt1，完整固定源码fca35fa31adf1376e0415a040266400fb5affe0c。固定源码包含4d5072b7bc8ee3eaacaca7d3f2d6252fb529a84d及b6c669022修正，三份核心实现／配置测试与4d5072b7b一致。选择Job126528775550原日志确认monitor gate selected，SHA256 b0b28c044474ed75c7b3dcc64ea207d4ecfbf269e82cee72b882c785012c3a97；不把未选中汇总绿色或继承关系单独当作实际执行。

HTTP Job北京时间19:31:13—19:46:12，Verify isolated metrics lifecycle于19:31:32—19:46:07实际success，Preserve HTTP functional budget evidence随后success，最终Job completed／success。原始Job日志134834字节，SHA256 646fa1a13f555720f78e883c0e8a2552774ae7f265e969b927374265bdeae9bc；原日志明确137项配置测试／9.953秒及OK，先前四项FileNotFoundError未在本轮发生。真实窗口编译开始／二进制执行后标记、Go真实查询／告警生命周期及Python双来源通过标记均已核对，不仅依据Job状态。

官方artifact13317703287（monitor-http-budget-41816593195-1）ZIP29100字节，SHA256 9b6539a9ccef6b4bb7f43c36487009675a30c3a178c1cb73ed83e26df1ae3d66，与官方digest一致，CRC通过且只有monitor-http-budget.json；原报告177385字节，SHA256 763ea2843de3ec881f5b6ebc90eaf0bb3dcee07a69bcb6652fab17187f47b1c2。报告375项输入逐一与固定fca35fa31adf1376e0415a040266400fb5affe0c的git对象匹配，零差异；inputs_unchanged=true。此结论只绑定本固定来源，不外推收尾时已继续推进的共享HEAD。

报告case=http-quality、result=passed、exit_code=0／error_code=null，hosted／capacity／schedule均false，整体874.387260秒／原1200秒。四阶段均finished／exit0且同一主机单调端点依序不重叠：

| 编译阶段 | 实际耗时 |
| --- | ---: |
| process_source | 89.016223秒 |
| resource_windows | 31.569300秒 |
| http_query | 1.157441秒 |
| http_alert | 73.641286秒 |

214次完整资源样本最长1.264099秒，均小于原四秒。CPU硬限合计峰值3.25核／上限4核，内存硬限合计4026531840字节（3.75 GiB）／上限4 GiB，私有可写上界1543061567字节（约1.4371 GiB）／停止线2 GiB，未触线；不把配额当实际CPU用量或RSS。48次受管采样均确认并恢复且无恢复错误，其中process_source22次、resource_windows8次、http_alert18次；短http_query阶段没有受管采样样本，阶段自身成功边界完整，不补造一次暂停。报告不含每次采样绝对起点，最长五秒开始间隔沿固定入口的原运行守卫判定，不由舍入时长反算独立测量值。

受控请求Go148、Python67／26，总241次，原失败／恢复阶段预算回执用现有complete_request_traffic核验完整，未超过300。Go真实HTTP来源、自然分钟窗口、告警打开、事件／outbox、签名503重试及恢复实际PASS；Python日志明确两个实际ASGI SDK来源、身份隔离、Monitor查询／历史趋势、签名重试恢复及停源隔离通过。两Python来源startup passed／native_imported=true，依赖挂载exec／nosuid／nodev符合；回收无stop／wait／nonzero错误，discovery_restored／traffic_available=true。mTLS localhost叶证书和实际IP检查均passed、无TLS错误，原子换证identity改变且passed=true。cleanup_verified／private_directory_removed=true，compile_cleanup_error／request_traffic_error／cleanup_overdue均无错误；清理与业务通过分别核验。

原始选择／Job元数据、ZIP、JSON、日志及逐项verification.json在/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-p19-fixture-natural-20261104-6jtlv6sc/（私有目录／文件）。本轮仅跟进已有自然CI，不手动重跑、不新开窗口，不运行Hosted、容量或定时。上次两个配置前置失败及更早go_tmp失败原件继续保留，不被本次覆盖；本次证明这一次Linux HTTP四阶段／采样／功能／清理通过，不证明具体历史删除进程已直接定位或长期偶发已根除。正式Agent/Copilot产品镜像、真实密码/MFA及Console/Monitor页面full仍未验收。同源Platform Job126528774946的最后一次只读快照仍in_progress，本批不据此宣称远端Platform整轮或HTTP／Trace确定性runner已实际通过；本地原137／148及HTTP44／Trace183有效结果保持独立。下一步优先回到正式产品页面闭环的准入核对，真实本地窗口另行确认。

<a id="http-history-ci-polls"></a>

## 自然 Linux 轮询与归档复核

以下只认实际 HTTP 子 Job。未生成子 Job、仍在排队或运行的记录，以及 workflow 聚合状态，均不能当作专项通过。轮询按自然首轮、单独重跑和未完成记录分别归档。

各条**原始报告通过**的记录均独立检查归档摘要、ZIP CRC 和唯一 JSON 成员，并将 353 项输入逐一匹配其固定源码。报告通过、退出码 0，无首败，三源请求和阶段计数满足原额度，`inputs_unchanged`、`cleanup_verified` 和 `private_directory_removed` 均为 true。各次保持完整采样四秒、1200 秒、4 CPU／4 GiB、私有可写小于 2 GiB 和最多 300 请求的原口径；表中跨运行合计只描述这些固定记录。两批停止确认及最后一条均为两次完整确认、无成员变更，末次只见 T／t／Z／X，且全部恢复。

这些归档未复现目录消失或停止确认超时。真实失败中的缺失对象、删除进程及确认超时根因仍以各次失败记录为准；正式产品到 Console／Monitor 页面、停源后历史读取、Hosted 和容量验收均不由这些自然运行覆盖。

<a id="http-linux-followup-manual-20261029"></a>

### 2026-10-29 后续自然Linux HTTP门禁手动复核（未复发）

固定于 `05c57272c` 修正后的两条自然首轮 HTTP Job。后续曾列为运行中或排队的 [40809646875／123236922601](https://github.com/pampa0629/addp/actions/runs/40809646875/job/123236922601)、[40810565372／123241061462](https://github.com/pampa0629/addp/actions/runs/40810565372/job/123241061462) 均有[第二次复核的最终证据](#http-linux-followup-second-manual-20261029)，此处不重复计入。

| 固定源码 | 自然运行／HTTP Job（attempt 1） | 结果 | 总耗时（秒） | 完整样本／最大耗时（秒） | 请求Go／Python／合计 | 清理 | 归档 artifact |
| --- | --- | --- | --- | --- | --- | --- | --- |
| aa516137d437ac6de8e5c61df8cfc08305e6b08b | [40806389627／123226471740](https://github.com/pampa0629/addp/actions/runs/40806389627/job/123226471740) | passed | 773.609527 | 189／0.916333 | 148／[81,30]／259 | verified／私有目录已删除 | 12868648938 |
| 24d2f15648494054627c36dfa01f61885e41fddd | [40807277727／123229384045](https://github.com/pampa0629/addp/actions/runs/40807277727/job/123229384045) | passed | 872.804636 | 215／1.216953 | 163／[75,28]／266 | verified／私有目录已删除 | 12869049745 |

<a id="http-linux-followup-second-manual-20261029"></a>

### 2026-10-29 后续自然Linux HTTP门禁第二次手动复核（首轮与重跑分列）

先前未完成的两条和新增一条通过原始预算归档核验。

| 固定源码 | 运行／HTTP Job | 结果 | 耗时（秒） | 完整样本／最长采样（秒） | 请求Go／Python／合计 | 归档 artifact |
| --- | --- | --- | --- | --- | --- | --- |
| ebdb5b82e721834c69eda8d3c1127580cb667583 | [40809646875／123236922601](https://github.com/pampa0629/addp/actions/runs/40809646875/job/123236922601) | passed | 873.542461 | 215／1.312225 | 161／[67, 26]／254 | 12870806065 |
| 49ecae13ecc23bb9d626ab5617234ffdca57f043 | [40810565372／123241061462](https://github.com/pampa0629/addp/actions/runs/40810565372/job/123241061462) | passed | 881.329419 | 217／1.330465 | 161／[75, 28]／264 | 12870867972 |
| 944c4bcf74272a568045a8c066da34878f4c7fa6 | [40877071516／123458861015](https://github.com/pampa0629/addp/actions/runs/40877071516/job/123458861015) | passed | 815.618545 | 200／1.213109 | 148／[67, 26]／241 | 12900440023 |

另有 16 条自然首轮只核对了 HTTP 子 Job 的 `success` 终态，未独立核对请求、峰值和清理。run `40880761833` 的 Job `123475011822` 属于 attempt 2，仅核对到 `success`，attempt 1 未生成 HTTP Job，单列于自然首轮统计之外。

| 固定源码 | 运行／HTTP Job（attempt 1） |
| --- | --- |
| 30d8754538607bb092f21af0055a537b2e74ad42 | [40855443616／123385434700](https://github.com/pampa0629/addp/actions/runs/40855443616/job/123385434700) |
| efc577aa69fc0a190dddba2bc9bff4903fc99f88 | [40841858063／123340144201](https://github.com/pampa0629/addp/actions/runs/40841858063/job/123340144201) |
| 0c8b006a3748d70d31db4775c9fed4f68422bae7 | [40832834357／123310943631](https://github.com/pampa0629/addp/actions/runs/40832834357/job/123310943631) |
| a5b91dc689e294769479a187cfd192a63b47bb8b | [40827356196／123293546414](https://github.com/pampa0629/addp/actions/runs/40827356196/job/123293546414) |
| 68ab3c56de2855dcdbd7132c1486ef0b2c6bd43e | [40823324214／123285345018](https://github.com/pampa0629/addp/actions/runs/40823324214/job/123285345018) |
| d86049faf65f854d92f4f0a252da1ed0fb79a37b | [40821832077／123279408209](https://github.com/pampa0629/addp/actions/runs/40821832077/job/123279408209) |
| cf02a0e9aa0bb26328f09cbc37bb5f0360ffd3f3 | [40820231458／123274702344](https://github.com/pampa0629/addp/actions/runs/40820231458/job/123274702344) |
| b5a99831923efb02fd2cb4b0509c7eff29a5be4b | [40818878003／123271690024](https://github.com/pampa0629/addp/actions/runs/40818878003/job/123271690024) |
| 8904cdd4011b02f6be255fcb7292d9065a54109b | [40816651860／123267185389](https://github.com/pampa0629/addp/actions/runs/40816651860/job/123267185389) |
| b1866c55d88382c68ddb63c0afe57b4f8ca95d44 | [40815937095／123263409449](https://github.com/pampa0629/addp/actions/runs/40815937095/job/123263409449) |
| a904b0de8ffd9eb8cf404b4650f7bf2f86a29fde | [40815855493／123262216602](https://github.com/pampa0629/addp/actions/runs/40815855493/job/123262216602) |
| 1f76f9428bdc18b06c9df64bea771b5cf1811c98 | [40814375122／123258977414](https://github.com/pampa0629/addp/actions/runs/40814375122/job/123258977414) |
| d0eb88434951e9fcb6a2067f786d53bd33e88882 | [40813833534／123256932880](https://github.com/pampa0629/addp/actions/runs/40813833534/job/123256932880) |
| ba7453222ecac82500b42c84b3c722deeba4d502 | [40813548359／123256276127](https://github.com/pampa0629/addp/actions/runs/40813548359/job/123256276127) |
| 19f05cd5e9c744eabf08941f381899d109894a7e | [40811786278／123247851294](https://github.com/pampa0629/addp/actions/runs/40811786278/job/123247851294) |
| 9a151b2c367d08b4620aef042608624ed537de6a | [40811503592／123245523106](https://github.com/pampa0629/addp/actions/runs/40811503592/job/123245523106) |

<a id="http-linux-followup-third-manual-20261029"></a>

### 2026-10-29 后续自然Linux HTTP门禁第三次手动复核（新增一通过、一未完成）

- **原始报告通过**。固定源码 `a62ef412ceff7539ba784184a65c8a8dca9bd268`，[run40891592619／HTTP Job123510009973](https://github.com/pampa0629/addp/actions/runs/40891592619/job/123510009973)，artifact `12907905105`。耗时 848.050459 秒，209 个完整样本，最长 1.282374 秒，请求 Go 148／Python [75,28]／合计 251。旧版 `process_compile` 完成且退出码为 0。
- **记录时未完成**。固定源码 `1a721a53fec6fb7a0b95920ce89cba7b438a9cc9`，[run40891932904／HTTP Job123517711481](https://github.com/pampa0629/addp/actions/runs/40891932904/job/123517711481) 仍在实际生命周期步骤，尚未上传预算报告。

<a id="http-three-stage-followup-manual-20261030"></a>

### 2026-10-30 三阶段修正后手动复核（新增一通过、两运行中）

- **原始报告通过**。固定源码 `18efeb515310ac73bb846314431be25b41938d61`，[run40986375022／HTTP Job123838511477](https://github.com/pampa0629/addp/actions/runs/40986375022/job/123838511477)，artifact `12948087445`。耗时 867.504351 秒，213 个完整样本，最长 1.260555 秒，请求 Go 163／Python [67,26]／合计 256。三阶段按序完成并分别退出 0，单调端点有效，无旧 `process_compile` 字段。三个取证脚本与 `3a004a947` 一致。
- **记录时未完成**。[run40992897301／HTTP Job123860345522](https://github.com/pampa0629/addp/actions/runs/40992897301/job/123860345522)（`31b60be911ded7d3c339e0ea5c6c51ed2cbf1bd4`）和 [run40993642918／HTTP Job123864301497](https://github.com/pampa0629/addp/actions/runs/40993642918/job/123864301497)（`ffa8aed3de8d3c84e5659c4575b30f6f7e8323e4`）均在运行，未取得终态或报告。

<a id="http-confirmation-followup-manual-20261030"></a>

### 2026-10-30 停止确认取证后手动复核：五次自然通过，无新复发

| 固定源码 | 自然run／HTTP Job | 总秒数 | 样本数／最长完整秒数 | 实际暂停数 | 总请求 | 归档 artifact |
| --- | --- | --- | --- | --- | --- | --- |
| f5a5b46d640ef667faf2b3340aad6305fd3579a4 | [41040462293／124011969137](https://github.com/pampa0629/addp/actions/runs/41040462293/job/124011969137) | 803.185325 | 196／0.952018 | 25 | 252 | 12971051051 |
| 8d5d757c930d9dcb032b13de0c5f15394b5dd541 | [41051306029／124048024345](https://github.com/pampa0629/addp/actions/runs/41051306029/job/124048024345) | 873.984330 | 214／1.297289 | 41 | 262 | 12976011235 |
| e09e50ae25837d3a99ec3b7aa9fd49f03e8bccb7 | [41053660178／124056004950](https://github.com/pampa0629/addp/actions/runs/41053660178/job/124056004950) | 865.177393 | 212／1.322332 | 38 | 241 | 12977455242 |
| 182f93649cd1227ef6ff8451fca0902373fbde1b | [41054172788／124063615586](https://github.com/pampa0629/addp/actions/runs/41054172788/job/124063615586) | 856.969005 | 211／0.770688 | 33 | 252 | 12977713231 |
| 873bc9d480196b42541a65c88e01fb294f9b028c | [41055725619／124064113033](https://github.com/pampa0629/addp/actions/runs/41055725619/job/124064113033) | 870.097836 | 214／1.207331 | 41 | 256 | 12979905507 |

五条实际生命周期和归档步骤均成功。共 1047 个完整样本，最长 1.322332 秒。178 次暂停全部确认并恢复，按阶段分为 Common 来源 97、HTTP 查询 2、HTTP 告警 79。查询阶段的实际暂停来自 `f5a5b46d6` 和 `873bc9d48`。后者另核对了实际 checkout 和原始日志摘要。

当时快照的 main `01b5e4054ebf604fc6f32bb7719f15c0b713dcc3` 未生成 HTTP 子 Job，不计入五条。

<a id="http-confirmation-followup-manual-20261031"></a>

### 2026-10-31 后续自然Linux HTTP手动复核：八条通过、一条运行中

| 固定源码 | 自然run／HTTP Job | 总秒数 | 样本数／最长完整秒数 | 实际暂停数 | 总请求 | 归档 artifact |
| --- | --- | --- | --- | --- | --- | --- |
| 9482b5ff0061010b49f708f667a7ff37c3ed058e | [41068378332／124106093330](https://github.com/pampa0629/addp/actions/runs/41068378332/job/124106093330) | 798.657008 | 196／0.632013 | 25 | 258 | 12985037041 |
| 57d7665d2b0b4d5d68e574edb1c15122dc225e79 | [41069979758／124112091876](https://github.com/pampa0629/addp/actions/runs/41069979758/job/124112091876) | 888.693991 | 217／1.300854 | 37 | 266 | 12987055303 |
| 4bc674e695370e7315209f4dd7ea6a57d610943f | [41070506837／124117524396](https://github.com/pampa0629/addp/actions/runs/41070506837/job/124117524396) | 812.653220 | 200／1.059084 | 27 | 241 | 12987027759 |
| c2ab029c5da06449e9d2984a9d999d017c749ce6 | [41074629093／124131000289](https://github.com/pampa0629/addp/actions/runs/41074629093/job/124131000289) | 906.738516 | 222／1.303327 | 42 | 266 | 12988990834 |
| e4397face172028bc25a9092b4fd05ded24177e3 | [41079100106／124143492579](https://github.com/pampa0629/addp/actions/runs/41079100106/job/124143492579) | 834.043597 | 205／0.868081 | 36 | 252 | 12989882567 |
| 44e023c85b6fc12eedc2749b5f66170a72c8b62f | [41081823122／124153160228](https://github.com/pampa0629/addp/actions/runs/41081823122/job/124153160228) | 881.231792 | 217／1.308277 | 38 | 250 | 12990958080 |
| 007c55259f99442da641c72b01803eeab2cbc908 | [41082924718／124161022645](https://github.com/pampa0629/addp/actions/runs/41082924718/job/124161022645) | 874.608077 | 215／1.254327 | 39 | 251 | 12992087731 |
| 85d6b3299c8feb0e061805b82c75e5ac75df6a7e | [41087857172／124174015790](https://github.com/pampa0629/addp/actions/runs/41087857172/job/124174015790) | 830.407493 | 204／0.806500 | 35 | 241 | 12993352482 |

八条实际生命周期和归档步骤均成功。共 1676 个完整样本，最长 1.308277 秒。279 次暂停全部确认并恢复，按阶段分为 Common 来源 153、HTTP 查询 2、HTTP 告警 124。`007c55259` 和 `85d6b3299` 另核对了实际 checkout 和原始日志摘要。`85d6b3299` 曾在运行，完成后已计入上表；同批 [run41087959071／HTTP Job124174366520](https://github.com/pampa0629/addp/actions/runs/41087959071/job/124174366520) 曾排队并进入运行，其[最终通过证据](#http-remaining-completed-20261031)单列。

<a id="http-remaining-completed-20261031"></a>

### 2026-10-31 剩余Linux HTTP专项最终通过核验

固定源码 `96472c487a00d8f90c8600097d9af019a47797cc`，[run41087959071／HTTP Job124174366520](https://github.com/pampa0629/addp/actions/runs/41087959071/job/124174366520)，自然首轮。2026-10-31 00:30（Asia/Shanghai）记录时仍在运行，00:44:44 完成；实际生命周期和预算归档步骤均成功。artifact `12994582770`，实际 checkout 和原始日志摘要另行核对。

原始报告通过，869.634727 秒，214 个完整样本，最长 1.342966 秒，请求 Go 148／Python [67,26]／合计 241。37 次暂停全部确认并恢复，Common 来源 20 次、HTTP 告警 17 次，查询阶段未与采样重叠。与上一批八条合计为 1890 个完整样本、316 次暂停全部恢复；该汇总仅涵盖这些固定运行。

<a id="http-history-product-windows"></a>

## 正式产品窗口、准入与失败诊断

正式 full 窗口使用 `make test-monitor-http-local`（`MONITOR_HTTP_LOCAL_CASE=full`），含准备、构建、业务验收及退出清理。原 60 分钟总期限、55 分钟停止新业务、完整检查四秒、8 CPU／12 GiB、私有可写 4 GiB 停止线和产品构建命令 600 秒期限按适用版本保留；各次独立授权，首败即停，不自动重试或续窗。早期 20 分钟 HTTP SDK 窗口和独立编译取证有各自限额，见对应记录。

下列确定性门禁、镜像构建、真实 SDK 链路及正式产品全链分别认定。正式全链需实际完成密码／MFA、采集与 API、Console／Monitor 页面、停源、两阶段浏览器注销及旧会话拒绝，并保留清理回执。停源后的历史读取、Hosted 和容量验收另行认定。

<a id="formal-python-http-local-20261028-image-auth"></a>

### 2026-10-28 正式Python产品HTTP本地窗口：采样镜像匿名令牌失败

用户按原上限仅授权本轮一次60分钟本地窗口，沿已确认的停源收窄边界：来源存活时验证历史，停源后Copilot为not_active／空观测，Agent保持新鲜；停源后历史读取仍列为另批产品缺口。专项11个构建输入与已验证c15f8fa2a修正一致，`make test-monitor-http-local-runner` 的20项及相关System Go夹具检查通过后，实际执行一次 `make test-monitor-http-local`。启动与源码快照固定为`bfdb7a34f1c8ea0ee57f341dffae37c8faf9e5b6`，不带入共享工作区未提交改动。

2026-10-28 15:11:34至15:11:55（Asia/Shanghai），外层21.046秒、生命周期20.930秒，结果为**failed**，报告阶段owned_volume_preparation、error_code=command_failed_1。实际已越过前轮profile缺项，完成配置投影及当次卷准备；该阶段末尾的固定Nginx镜像拉取失败，封闭诊断包含failed to resolve、failed to authorize、failed to fetch anonymous token；原始输出SHA256为`52e772e82add3ea821f6bc7726a0d15765ab7552a0f1e7d0fbc80b1558e92c4f`。没有保留原始错误响应，不能进一步唯一归因为代理、TLS或远端限流，不把网络根因写成已查明。

尚未启动构建器、完整资源守卫、产品构建、正式登录、采集查询、受控业务请求、页面或停源断言；resource_samples为空、assertions为空。8 CPU／12 GiB、4 GiB停止线和四秒口径均未放宽，但不能把这些配置值当作实际资源峰值或守卫通过。cleanup.zero_owned_resources=true、failures=[]，当次容器／网络／卷及私有材料清理至零残留。本轮失败后停止，不复用原剩余时间、不自动重试或续窗，不运行Hosted、容量、定时、模型／工具或告警重演。

失败后仅只读核对镜像缓存：既定`nginx:1.28.0-alpine@sha256:30f1c0d78e0ad60901648be663a710bdadf19e4c10ac6782c235200619158284`已缓存，仓库摘要精确匹配，平台Linux ARM64，镜像ID为同一sha256。此为失败后的独立元数据证据，不倒写为本轮已进行缓存准入或运行镜像验证。

修正`a5db60d9ae43556edeb4d3227c115cc79d22f3d9`删除该辅助镜像的无条件拉取，在创建当次网络／卷之前只读核验固定仓库摘要、不可变ID与平台；后续卷测量、TLS准备仅以已核验ID和--pull=never运行。缓存缺失、错配、检查失败均停止，无远端fallback、更换镜像或TLS放宽；sampler_image_admission单列阶段，回执只保存封闭元数据。未改变产品依赖来源、身份、请求、清理、四秒或资源上限，也未修改产品查询语义。

修正后验证：新HTTP缓存准入回归先红；`make test-infra-runtime-log-lifecycle`通过，其中HTTP22项、原Trace123项、既有生命周期88项及相关Go／CI登记通过；随后补充当次TLS辅助容器使用确切ID和禁止隐式拉取的用例，`make test-monitor-http-local-runner`最终23项及相关Go检查通过。对失败后已保存的真实缓存元数据，离线验证器也通过；这些结果不代表新完整实链或采样镜像内进程已运行。新修正尚未启动真实新窗口，原Trace／Console／SDK Linux证据仍保持其原固定源码与覆盖范围。

原始归档见 `/tmp/addp-p19-http-product-window-bfdb7a34f1c8`。 本轮结论独立保持“运行失败、清理通过”；构建、真实身份和页面等未运行项不得计通过，下一轮须用户另行授权。

<a id="formal-python-http-local-20261028-cache-reclaim"></a>

### 2026-10-28 正式Python产品HTTP独立窗口：编译后私有目录测量失败

用户明确另开一次原上限60分钟独立本地窗口，接受仅核验复用同一固定摘要Nginx缓存；所有资源／四秒／失败清理及停源收窄边界不变。专项构建输入与已验证a5db60d9a修正一致，实际启动及源码快照为`dcf57cc7344c68135e14eac9c29c065f9dea52ff`。2026-10-28 15:40:28至15:43:45（Asia/Shanghai），外层197.163秒、生命周期197.082秒，结果**failed**。失败后停止并零残留清理，未自动重试、复用剩余时间或续窗。

本轮实际覆盖固定摘要缓存准入、卷采样辅助启动、初始资源检查及标准Go编译命令返回0；尚未进入产品镜像构建、正式后端启动、真实密码/MFA、业务请求、指标查询、Console页面或停源断言。sampler_image回执核对了官方固定仓库摘要、不可变ID及Linux ARM64，acquisition=verified_local_cache。不能把这部分覆盖计为完整产品依赖、正式身份或页面验收通过。

45次已完成资源扫描最长0.537962秒，均小于原四秒。完成观测的硬配额最大2.25 CPU／3288334336字节内存（配额，不是实际内存峰值）；合计可写占用最大4159303680字节，约3.874 GiB。下一次扫描在elapsed=195.04秒、measure_private_bindings阶段因原生du返回1失败，du_missing_file=true，失败扫描历时0.404023秒，**不是四秒超时**。原始错误输出SHA256为`9d5a4fd6ae507e15a2645e0d3e0702c666294907ff0ff1ee1938ec20d9513259`；没有保留原始路径，不能唯一确认哪一个文件消失。

主线程随后报告bounded_builder_resume阶段、command_failed_1；共享构建内核在Go命令成功后直接shutil.rmtree当次.cache/go-build，未与sample_lock互斥。可确认私有文件消失与该回收时段重叠的风险，但未把缺失路径写成已唯一定位。失败采样没有得到完整可写值，不能据前45次最大值推导全程未触4 GiB；未放宽或忽略真实du失败。

cleanup.zero_owned_resources=true、failures=[]，当次构建器／容器／网络／卷及私有材料零残留，当前运行断言仍空。未运行Hosted、容量、定时、模型／工具或告警重演；未迁移SDK Linux或既有Trace/Console证据为本轮通过。

待用户确认的最小方案（本轮未改回收代码）：回收前先沿原四秒完整预检，失败或触线即停，不靠删缓存隐藏已触线；缓存删除复用采样锁，锁等待和可中止删除的合格预算独立四秒。采样自身锁等待及全部读取仍限原四秒，不重试、跳过或缩小扫描；路径仅限当次Go对象缓存。先修规范、唯一共享回收内核及互斥/超时/路径/终止清理确定性门禁，新的真实窗口另行确认。中途回收后仍继续采样，须明确这段生命周期约束，不能直接套用退出清理的停止采样语义。

原始归档见 `/tmp/addp-p19-http-product-window-dcf57cc7344c`。

<a id="formal-python-http-local-20261028-resource-list-deadline"></a>

### 2026-10-28 缓存回收修正后独立窗口：完整资源扫描超时

用户在上一批规范／确定性修正完成后要求继续，本轮沿上一条明确的下一步，仅开启一次原上限60分钟独立本地窗口。专项11个构建输入与2ffd8eb30修正一致，运行前再次执行`make test-infra-runtime-log-lifecycle`通过（HTTP23、共享内核136、生命周期88及相关Go／登记），随后实际源码快照固定为`df82aed91964dadd54da13ca946a8c95c5843d11`，不带入共享工作区未提交改动。

本轮结果**failed**：parent单调计时99.971秒，报告阶段product_compile、error_code=resource_sample_deadline_exceeded，已零残留清理。墙钟起止为2026-10-28 17:36:34至17:38:32（Asia/Shanghai），UTC时间差118.229秒，与单调耗时相差18.258秒；原因未确认，不自行校时、回填或混用两种时间。该差异不用于推导暂停、睡眠、NTP或VM原因，本轮两种口径均远未到60分钟且已按失败停止。

实际覆盖缓存镜像准入、配置投影、构建器／采样辅助及初始检查，随后Go编译被资源守卫中止。尚未进入新的go_cache_precheck／go_cache_reclaim，报告没有go_cache_reclaim回执；因此本轮不能验证该修正的真实运行效果，也未到完整产品镜像、后端启动、真实身份、受控请求、指标查询、页面或停源。

18次已完成完整扫描最长0.650891秒，观测到的合计可写最大1905561600字节。下一次扫描在elapsed=90.34秒失败，完整历时4.025778秒，其中锁等待0.008522秒。列出归属容器步骤已耗3.210680秒（CLI创建0.088640秒、进程等待3.007714秒、输出读取0.001432秒）；inspect_owned_containers仅剩不足0.8秒预算，该步骤0.773644秒（创建0.041333秒、等待0.699569秒、终止处理0.022726秒）。不能把本次解释为inspect自身耗时四秒，更不能从CLI等待直接唯一归因daemon、VM、文件遍历或代理。完整扫描超过原四秒，未重试或放宽；失败扫描没有完整资源数据，不宣称全程资源峰值或四秒守卫通过。

cleanup.zero_owned_resources=true、failures=[]，当次构建器／容器／网络／卷及私有材料零残留。失败即停，不复用剩余时间、不自动重试或续窗，不运行Hosted、容量、定时、模型／工具或告警重演。未因这次CLI等待证据不足而修改HTTP产品或已确认的四秒规则，也未改缓存回收实现；2ffd确定性结果仍仅属于其相应层级，不迁移为本轮产品通过。

原始归档见 `/tmp/addp-p19-http-product-window-df82aed91964`。

<a id="http-formal-page-oracles-20261031"></a>

### 2026-10-31 正式 Python HTTP 页面的验收断言准备（未运行真实窗口）

用户仅授权先修规范及确定性门禁，真实本地窗口另行确认。本次补齐既有 `make test-monitor-http-local` 内的页面验收，不修改产品 API、权限、指标或停源历史语义；没有运行该真实入口、Docker、正式身份或采集环境，也没有 Hosted、容量重跑、定时、CI 手动 dispatch／rerun。

Console 受控回归与人工用例复用唯一 `http-quality-page-support.mjs`：逐一比较七项汇总、六项模板排行、排序和实际画布提示值与同次响应；先等待 Monitor 与 Console 的 canonical query 一致，再刷新核对实例、模板／方法、范围、排序、backend、关闭自动刷新、实际请求及数值和单 iframe。受控反例拒绝错误汇总／排行值、缺样显示为零、错误身份、无有效趋势、趋势提示与响应数值不符和停源前的 Agent 采样。受控停源先展示非空指标，随后在原 iframe 内改变排序重载查询，证明旧汇总／排行／趋势清空且 iframe 未销毁。

人工用例按存活／停源分两个独立真实密码／MFA浏览器阶段，不声称已证明真实页面跨停源驻留；第二阶段由既有单次 Copilot 正常停源与 API `not_active`／空观测之后调用，验证在线列表中该来源已退出、详情提示重新选择且无旧指标及图，Agent 响应与页面仍为停源后新鲜观测。两份回执独立保留，合计浏览器最多80次、控制面最多200次，失败尝试仍计数；顺序错误、重复阶段、预算不足、回执缺失／错归属／不完整均失败，回执解析问题不覆盖首个运行失败。新断言文件纳入固定源码输入与登记检查，公共 CI 仍禁止调用真实人工入口。完整四秒、60分钟／55分钟停新业务、8 CPU／12 GiB、4 GiB停止线、失败清理且不重试／续窗均未改。

| 本轮实际命令 | 结果与边界 |
| --- | --- |
| `make test-monitor-http-local-runner` | 29项通过；含未提交共享断言在任何 Docker 调用前被拒绝；同入口 System 正式 HTTP 身份夹具 Go 单元通过，无真实服务 |
| `cd console/frontend && npm run test:e2e -- http-quality-navigation.spec.js` | 首轮两处测试定位／交互失败（控件名误写、背景刷新被详情遮罩挡住）；修正后4项通过。随后标准完整 Console 门禁执行了增加数值篡改、缺样／趋势反例和原 iframe 停源清空的最终版本 |
| `make test-console-frontend` | 222项单元、274项受控浏览器及构建通过；包含本次四项 HTTP 用例，不等同真实后端／真实身份实链 |
| `make test-monitor-frontend` | 131项单元、539项受控浏览器及构建通过；包含原HTTP页面／规则与身份隔离回归，不扩大为正式后端实测 |
| `make test-frontend-ci-registration` | 27项登记／隔离检查及21个前端登记检查通过；既有路径选择命中新断言的 Console，以及夹具变更的 Monitor／Console，无需新增入口或 workflow |
| `make test-module MODULE=console` | 前置 `test-platform-review` 的 `TestTransferReviewSourceDrift/batch_contract` 因 `docs/spec/addp智能体Tool开放规范.md` 来源漂移失败，未进入 Console owner 门禁；不在本专项修复，不计完整模块通过 |
| Node／Python源语法、本专项 `git diff --check -- <owned paths>` | 通过；人工 JS 仅语法检查与共享断言的受控实测，不声称实际运行真实密码/MFA用例 |

正式产品构建／注册／mTLS采集、真实密码/MFA、API及两阶段真实页面、注销和最终零残留仍未验收；停源后历史读取、5／15分钟完整窗口、模型／工具、5xx／告警通知、Hosted、容量及72小时保留仍不在本批。历史 Mac Docker list／inspect 等待与完整四秒失败原因未确认；Linux SDK 稳定读取路径的成功不能代替本地资源守卫修复。下一步只建议另行确认原60分钟真实本地窗口，未取得授权前不启动。

<a id="http-formal-page-window-projection-failed-20261031"></a>

### 2026-10-31 正式 Agent/Copilot 页面本地窗口：指标投影准备失败并清理

窗口于2026-10-31 19:33:47（Asia/Shanghai）开始，固定源码 `b45cad3a65ecf8bce641ec24016175b48f25dee3`，包含页面断言实现 `5ad3ade67a3ac0dbb851e3e1ee08a94f77899385`。当次run_id=`addp-http-local-3fa6c05f2dd047728a1b0b29925fc05e`。来源快照、固定Nginx摘要缓存身份／Linux ARM64与隔离网络准入已通过；未隐式拉取、更换辅助镜像或修改TLS。报告15项固定输入摘要独立匹配该源码。约4.40秒进入`metrics_deployment_projection`，4.52秒进入清理，含清理总5.277706915978342秒；`make`退出2，报告`failed`，首因`command_failed_1`（子命令退出1）。输出只保留SHA256=`37962b1ee42e4de2504353bed1c3a64808786c8c5f8c1285f734b1c7ce27428d`及`no such file or directory`封闭标记，未保留失败命令类别、原始输出或缺失路径，不能唯一指认具体命令／对象。

阶段包含临时证书、指标配置生成及Compose投影，现有回执不足以区分子步骤。本轮失败后仅做只读源码／配置核对：本机`openssl version`为OpenSSL 4.1.0（23 Oct 2026），`openssl version -d`指向`/opt/homebrew/etc/openssl@4`，其`openssl.cnf`确实不存在；当前临时CA生成的`openssl req`未传显式`-config`。该缺失是待处理的证书准备前置事实，与失败阶段相符，但缺少本轮失败命令及原始输出，不能当作已唯一确认的根因。没有重试证书生成、修改系统配置、降级工具或另起环境；建议下一批先核对该依赖，再讨论显式当次私有OpenSSL配置及封闭步骤诊断的最小修正与确定性回归，实际新窗口另行授权。

本轮尚未创建采样线程／完成任何完整资源样本，`resource_samples=[]`，无`resource_guard_failure`；不能算四秒守卫运行通过，也不是四秒超时复发。没有进入产品构建／启动、完整Python依赖检查、正式密码/MFA、业务请求、采集查询或页面，两阶段及停源断言均未执行，`assertions={}`，没有浏览器回执。不能以已有受控T0/T3、SDK Linux或缓存镜像准入替代这些结果；未取得完整大小样本，也不能认定4 GiB容量足够。

原生命周期自动清理完成，报告`cleanup.zero_owned_resources=true`、`failures=[]`，当次容器／网络／卷／builder残留检查无失败；私有工作目录已删除，本轮只读核对同run_id目录数量为0。首失败保留，原资源及清理期限未改。失败即结束，剩余时间没有被复用，未自动重试或续窗。

原始报告与独立清理审计见 `/tmp/addp-p19-http-product-window-20261031-3fa6c05f/`。

<a id="http-private-openssl-config-20261031"></a>

### 2026-10-31 HTTP 临时证书私有配置与离线T1（无真实新窗口）

用户确认先修规范、HTTP本地入口及确定性门禁，允许一次实际离线证书组，单并发／最多40条命令／含清理60秒／材料与输出预留≤1 MiB；不新开真实部署窗口，不运行Hosted／容量／定时，不手动dispatch／rerun。运行观测及T0–T3规范先修订，再沿唯一HTTP入口修改，无产品API／权限／证书用途／共享资源守卫变更。前次b45cad3a6窗口失败及其未能唯一定位命令的证据保留，不以本次离线成功追认历史唯一根因。

`tls()`在当次0700 certificates目录内以排他创建方式写0600最小请求配置；所有OpenSSL子进程的局部OPENSSL_CONF及req显式-config绑定同一文件，父环境／产品配置不污染，无外部include／provider加载。原系统默认配置依赖调用删除，保留三套CA、RSA2048、一天有效期、显式CA约束、叶子用途及DNS／来源IP SAN和原复制／挂载权限。既有phase增加固定CA／CSR／签发／材料／配置生成／Infra与产品Compose投影步骤：成功恢复外层，异常保留确切步骤，材料复制失败不会误归前一签发；沿原封闭错误／输出摘要及首因，不留原始参数／路径／stderr。共享完整四秒、8 CPU／12 GiB、4 GiB停止线、清理及原单命令10秒口径未变。

OpenSSL依赖在原HTTP runner先检查，Platform consistency既有准备步骤显式安装及检查，先于make test-platform；登记回归拒绝本地准入缺失／晚于测试，以及CI安装缺失／晚于门禁。原HTTP人工入口仍禁止在公共CI执行，无新入口、workflow、版本分支或本机软件安装。实际证书组使用正式tls函数及原有可取消命令内核，两个指标配置生成器仅受控替身，没有Docker、网络、正式身份或采集请求。

| 实际验证 | 结果与边界 |
| --- | --- |
| 原默认配置调用的受控反证 | 新配置绑定回归拒绝旧HEAD的tls实现；仅替身，无OpenSSL子进程 |
| 模拟回归 | 最终36项通过，覆盖原四秒／资源／权限边界与新增配置污染、缺失／损坏、CA／CSR／签发／渲染失败及超时、不继续、复制阶段归属、首因与清理失败、依赖登记；不生成证书 |
| 首轮HTTP runner | 新T1夹具遗漏run_id，在命令内核登记时AttributeError；尚未进入Popen，实际OpenSSL子进程0，临时目录已删除。此轮失败保留，修正夹具并用真实命令内核＋Popen替身验证适配字段后才执行实际离线组 |
| 修正后的`make test-monitor-http-local-runner` | 37项Python通过（含唯一实际离线组）、JS语法及HTTP登记通过；随后Go身份负例因本轮调用者umask077而失败，整条make退出2，不能写成一次完整入口通过 |
| 实际离线证书T1 | macOS ARM64／OpenSSL4.1.0，35条命令、0.617958秒（含清理）、材料与65536字节在途输出预留合计峰值125465字节；三CA约束／2048位／一天有效期、八份叶子的链／用途／DNS或IP验证通过，错误CA／用途／IP／DNS四个反例均拒绝，cleanup_removed=true。私钥／CSR／证书均随临时目录删除；仅公开汇总回执保留 |
| 原runner中的Go身份命令定向复核 | `umask 022; cd system/backend; GOWORK=off go test ./cmd/online-test-fixture -run 'HTTPQualityLocal\|ExecutionTrace\|ProtectionReadiness' -count=1`通过。未改Go断言；此前测试以os.Mkdir(...,0755)构造公开目录，被调用者077掩码变成0700而丢失负例前提。只复核同一既有子门禁，未再次生成证书；完整入口原失败不改写，单次整体exit0待自然CI |
| `make test-build-registration` | 59项＋9项及26个Go服务／59个镜像静态登记通过，不是实际产品构建证据 |
| 独立JS语法、HTTP专属CI登记、本专项差异 | 通过；无前端／共享命令内核改动，不重复原T3和其它无关门禁 |

原始归档见 `/tmp/addp-p19-http-tls-runner-20261031.log`、`/tmp/addp-p19-http-tls-runner-corrected-20261031.log`。 本地各必要子项分列有效证据，不能声称一次完整runner已通过；Linux自然CI与正式产品构建／注册／mTLS采集／MFA／API／两阶段页面／注销／零残留仍待各自实际结果。

实现与规范提交`a517d68becca6d55a29cad1bf9ff48191a0dd23a`已推送main，仅10个本专项路径／片段；共享Makefile仅1行、登记文件仅12行自有增加，其他会话改动未提交。自然push生成[Platform CI run41204767479／Platform consistency Job124547494998](https://github.com/pampa0629/addp/actions/runs/41204767479/job/124547494998)，截至2026-10-31 21:17（Asia/Shanghai）API为queued／conclusion=null／steps=[]，不计Linux或单次完整runner通过。后续须核对该固定提交实际步骤，不能用后续文档提交的选择跳过或其他Job结果补齐；本轮未手动重跑／dispatch，未设置任何定时跟进。

2026-11-01 08:03手动核对同一固定提交的自然CI最终结果：原21:17排队快照保留；run41204767479仍为main／push／attempt 1／`a517d68becca6d55a29cad1bf9ff48191a0dd23a`，Job124547494998已于2026-10-31 21:38:08—21:42:08（Asia/Shanghai）执行并completed／failure。实际checkout日志与固定SHA一致；`Install platform check dependencies`步骤成功，OpenSSL为Ubuntu包3.0.13-0ubuntu3.16，`command -v openssl`返回`/usr/bin/openssl`。这只证明Linux依赖准备成功，不是离线证书组或HTTP runner通过。

实际`make test-platform`在靠前的`test-platform-review`失败：`TestTransferReviewSourceDrift/config`与`batch_contract`分别报告`transfer/docs/transfer-基本概念及配置说明.md`及`docs/spec/addp智能体Tool开放规范.md`来源漂移，make退出2。按固定源码的顺序及完整日志，尚未调用`test-monitor-http-local-runner`，本专项37项、实际离线证书组和Go身份子门禁在这次Linux运行均**未执行**；不能把聚合失败误写为HTTP断言失败，也不能用OpenSSL安装或既有Mac分项结果补齐Linux单次完整入口。仅定位到阻断本专项的首个前置，不调查、修复或跟踪上述无关功能。

本次只读取得Job／run元数据与完整原始日志；日志301934字节，SHA256=`7ec710605d99cef3d09a0e664efecce71b4a550d6dfd550c6f12c579d0bbdd4c`，本机`/tmp/addp-p19-http-platform-124547494998.log`及同前缀`-job.json`／`-run.json`／`-audit.json`保存最终证据与独立核对（文件0600）。仅补充原记录及索引，39主题不变，验证唯一锚点／索引、固定SHA／attempt／步骤、日志散列及本专项差异；没有重跑代码门禁或实际证书组，没有手动CI重跑／dispatch、新开本地窗口、Hosted、容量或定时。Linux完整runner与正式产品到页面验收仍未通过；下一步建议只手动核对后续自然CI中本专项实际执行结果，须重新核验固定输入，不能将后续主分支状态直接归给a517。

<a id="http-formal-admission-runner-linux-20261101"></a>

### 2026-11-01 正式页面准入复核：Linux本地runner通过，真实产品链仍未验收

本次按“继续核对正式产品页面剩余准入”的范围，只读核对源码、规范及既有自然CI，不执行本机标准入口、Docker命令、证书组或任何新环境。[固定 086 的 SDK Linux HTTP T2 通过](#http-bounded-wait-linux-success-086-20261101)结论保持，不用它代替正式产品页面。发现另一项此前尚缺的证据已实际产生：[Platform CI run41259443660／Job124709465322](https://github.com/pampa0629/addp/actions/runs/41259443660/job/124709465322)，main／自然push／attempt 1，固定源码`086ae6ada785264813dec96ace94d64bc190ae5d`，本轮读取的最终API为completed／failure；实际运行区间为2026-11-01 09:34:48—09:43:07（Asia/Shanghai）。此处为本轮读取的终态，不沿用此前queued记录推定通过。

完整日志确认原唯一`test-monitor-http-local-runner`在聚合失败前已完整返回：固定Make配方先检查OpenSSL，依次执行37项Python测试、两份JS语法、HTTP专属登记及System身份Go子门禁；第2349行37项及随后OK、第2429行System fixture包ok，随后make[3]正常返回并进入下一个Trace runner。此前第2346行实际HTTP TLS offline T1为OpenSSL3.0.13／35条命令／1.578248秒／材料与输出预留峰值127023字节，错误CA／用途／名称四反例拒绝，cleanup_removed=true，符合原40条／60秒／1 MiB。因每条原配方命令均带Make失败传播，静默的JS／登记命令与末尾Go成功返回共同支持这一次完整HTTP runner通过；不是只拼接本机分项。

随后Trace runner136项、共享生命周期／预算133项也正常返回；整Job在之后的`test-common-frontend`失败，仍记聚合failure，不宣称Platform整体通过，不调查或修复该无关前端问题。原a517因来源漂移导致HTTP未执行、本机首次fixture失败及umask077造成Go负例失败继续保留；新固定086的这一完整Linux入口结果只关闭“尚无单次完整HTTP runner成功”的缺口，不抹去历史失败。

当前仓库12项OWN_INPUTS逐项比较，工作区与当前已提交HEAD相同，且均与该固定086验证输入逐字节一致，包含HTTP入口／测试、身份夹具、共享Trace生命周期、正式浏览器用例与唯一页面断言。真实入口从已提交main创建独立clone并核验前后HEAD稳定，产品构建使用该快照；因此其他会话未提交的Agent／Common等修改不会混入，也不能被本轮快照的证据覆盖。原三个集成文件按提交内容检查标记，不能用工作区未提交的Make／登记改动替代正式入口准入。该核对只证明入口来源一致，不表示当前全部产品及依赖已构建。

| 剩余准入／验收项 | 本轮核对结果与下一阶段通过要求 |
| --- | --- |
| HTTP入口及临时证书配置的确定性前置 | 固定086的Linux单次完整runner已通过；本机OpenSSL4.1.0的旧离线分项保持，不再将此项写成未执行 |
| 当次本机资源与镜像准入 | 仍须新获授权的真实窗口内验证本机／daemon机型、既定Nginx缓存的摘要／不可变ID、独占网络及完整四秒采样；本次未执行，不能写已通过 |
| 正式产品构建与启动 | 仍须从固定已提交快照构建System／Gateway／Monitor／Agent／Copilot及Console／System／Monitor／Nginx，验证镜像身份、完整Agent Python3.12／Copilot Python3.11依赖和pip check、实际注册及受保护指标采集；SDK两来源T2不覆盖完整产品依赖 |
| 正式身份及API | 已在唯一入口写入密码／MFA／AuthContext、普通用户仅agent.session.read、管理员观测及安全管理员／Tenant拒绝、两源各60次只读2xx／4xx、真实增量／规范模板／排行／历史、注销后旧Token拒绝；仍缺真实环境运行回执 |
| Console／Monitor页面与停源收窄边界 | 唯一浏览器用例及共享断言已具备：七项汇总／六项排行及趋势提示与同次API相符、Console和模块URL／刷新／单iframe恢复、中英文；一次Copilot正常停源后not_active／空观测／页面无旧数据，Agent保持停源后新鲜观测；live／stopped两阶段独立密码MFA回执仍未取得 |
| 最终通过 | 当次完整守卫、全部业务／页面断言、旧会话拒绝及容器／网络／卷／私有材料清理均通过方可判该轮通过；缺任一项仍未验收 |

后续真实入口仍仅`make test-monitor-http-local`，须另行获得原上限单次60分钟窗口授权，55分钟停新业务，8 CPU／12 GiB、私有可写4 GiB停止线、完整四秒、受控请求／身份浏览器总预算、首失败即停清理、不自动重试或续窗不变。当前授权仍是不新开本地窗口，本次没有开始计时或执行运行前Docker／服务检查，也不申请放宽条件。停源后历史读取、完整5／15分钟窗口、模型／工具、5xx／告警通知重演、Hosted、容量与长期保留继续另批。

原始归档见 `/tmp/addp-p19-http-page-admission-086-platform.log`、`/tmp/addp-p19-http-product-admission-20261101/audit.json`。

<a id="http-formal-product-window-agent-apt-failed-20261101"></a>

### 2026-11-01 正式产品60分钟本地窗口：Agent镜像系统包步骤失败，已清理

固定源码 `671fcd9b2b6d8f8844edc5a734f23c599213c938`，run_id `addp-http-local-4b06f791f4f144f29fbdd9f0b9cf52ee`。15 项输入匹配固定提交，生命周期 538.021 秒，Make 退出 2。

ARM64／Nginx、隔离网络、证书和 Compose 投影准入通过。Go 编译和缓存回收完成，System／Gateway／Monitor 构建命令成功返回后进入 Agent。

首败 `product_image_agent_backend/command_failed_2`。Agent Dockerfile 第 5 行同一 APT RUN 退出 100，未到 pip；只留 `dockerfile_process_exit`、`package_install` 及失败标记，无法区分 update／install、具体源／包、DNS 或代理。原 APT stderr 未保留。

130 次完整检查无重试，最长 1.847531 秒，私有可写上界 4174077952 字节，低于 4 GiB；无资源守卫首败。

Copilot／前端构建、正式容器启动、密码／MFA、受控请求、采集／API、页面、停源和注销均未执行。5／15 分钟完整窗口、模型／工具和 5xx 通知重演未覆盖。

标准清理 `zero_owned_resources=true`、无失败，4 条归属检查通过，缓存回收子进程已停止，独立检查私有工作目录零残留。

原始报告和清理审计见 `/tmp/addp-p19-http-product-window-20261101-x5tejves/`。

<a id="http-agent-apt-bounded-diagnostic-20261101"></a>

### 2026-11-01 Agent apt失败调查与有界取证（本地门禁通过，未提交／推送）

用户要求只调查既有失败，讨论后再实施；随后确认首批仅修规范／取证／确定性门禁，不改产品Dockerfile、apt源／包／重试／签名校验、基础镜像或代理，不重试构建、不开新窗口，不运行Hosted／容量／定时。本批实现仅在共享工作区，**未提交、未推送**。现有Platform workflow的product-build无路径跳过，每次push先编译产品，再按baseline选择包含Agent的镜像验证（platform-ci.yml原步骤及select-image-services.py基线）；推送会触发本轮尚未授权的实际构建，因此保留可审阅本地差异，不修改或绕过workflow。

历史4b06窗口的首因及报告不变：Agent Dockerfile第5行同一个apt-get update／install RUN退出100，未到pip，原始构建正文仅留哈希及全局封闭标记，基础FROM解析摘要及APT错误原文未保留。旧public_build只从末尾通用BuildKit错误／框架提取步骤与退出码，未对APT自身E／W／Err行建立唯一步骤归属；全局not found也可能只是新镜像缓存不存在，不能等同缺包，timed out亦不能唯一归属某一源。APT的100是通用错误结果，不唯一确定网络原因。[Debian apt-get文档](https://manpages.debian.org/trixie/apt/apt-get.8.en.html)

本轮仅作有界只读元数据查询，无容器创建、拉取、构建或包管理：当前daemon为ARM64，HTTP／HTTPS代理均为无凭据Desktop内部类别，host CLI为无凭据回环代理，Desktop配置为manual。源码明确从原docker info读取并校验daemon代理，再用于BuildKit环境及RUN的大小写HTTP／HTTPS build-arg；host CLI另用宿主回环代理，私有Docker client不继承用户完整配置。无需在Dockerfile新增ARG即可使用预定义代理参数，[Docker官方说明](https://docs.docker.com/build/building/variables/#proxy-arguments)支持该语义；不能据Dockerfile未声明ARG判定漏传，也不将这份当前配置当成历史网络成功证明。

当前本机Python3.12-slim镜像ID=`sha256:46cb7cc2877e60fbd5e21a9ae6115c30ace7a077b9f8772da879e4590c18c2e3`，本地Registry当前平台manifest为`sha256:5ca4bfc4580d33083387dfc582569b7ec8cf12438961469c74b765c49780aa02`、config为`sha256:9420c53ba876a39b83e2f08732920b62782c33d94cd04860a13c3eaf9dc1a5b0`，Linux ARM64／Python3.12.13，history提到trixie；这些是不同类型的摘要，不混作同一image ID。产品FROM采用可变本地tag，seeding看到tag存在会复用；没有原失败当次解析摘要，当前镜像／Registry内容不能唯一证明当时构建输入。未导出镜像层或启动容器读取apt源文件。

另只读关联已有Docker Desktop httpproxy日志：在UTC 02:56:07—02:58:00粗时间段，60组公开deb.debian.org请求严格按“returning HTTP状态”解析均为200，含Trixie／updates／security及arm64 deb请求，使用应用HTTP代理；未见该段显式代理error／timeout。未用路径哈希、版本或字节数中的407／502／504子串作HTTP状态。该日志缺本次构建归属及主机／VM单调校时证明，200和日志successful不等同APT完整读完正文；既不能直接判代理故障，也不能排除后续正文／连接问题。仓库Document曾有官方HTTPS源的经验，但该次因果证据和重试设置不能直接套到本失败，当前不换源或增加重试。

确认后先修运行观测及T0–T3条款，再扩展唯一public_build分类器：整份已捕获输入≤128 KiB；以唯一失败BuildKit编号绑定RUN apt-get及其输出，其他步骤／缓存警告不参与APT归因。提取索引下载／deb下载／dpkg／unknown阶段、固定错误类别及带错误上下文的HTTP状态；公开Debian源仅输出协议／仓库／固定套件枚举，私有、含凭据或查询参数、畸形端口URL不导出。公共Python FROM只保留唯一已解析sha256及固定kind，多个候选ambiguous、缺失unavailable，不查询当前tag兜底。代理三层证明复用原读取及无凭据校验，只保留来源及类别，不新增Docker调用或保存值。

输入、集合或紧凑JSON UTF-8结构化诊断4 KiB超限时整组明确over_budget，不截尾或截断为完整；未知留空。原构建退出码／哈希及固定标记仍在同一首失败路径，新字段不把业务／身份命令送入公共解析。采样、监测、恢复、清理、资源校验及原Docker命令AST逐项保持，所有产品Dockerfile和workflow均无改动。本修正提高下一次失败的可定位性，不是已修复历史APT原因或已验证真实网络。

| 本轮实际门禁 | 结果与范围 |
| --- | --- |
| 原解析器受控反证 | 原函数装入新步骤归属反例，1项按预期断言失败、0额外错误；仅字节替身 |
| 新APT取证定向测试 | 最终10项通过，覆盖步骤归属、阶段／错误／官方源、隐私与畸形输入、基镜像缺失／多个摘要、代理来源、128 KiB／4 KiB和集合超限、原首因及哈希 |
| `make test-monitor-traces-local-runner` | 最终146项、既有Online CI登记及System身份Go子门禁通过；共享内核单元／夹具，无真实Docker或产品构建 |
| HTTP模拟回归 | `python3 -m unittest scripts.test.monitor-http-local-gate_test.HTTPProductLocalTests scripts.test.monitor-http-local-gate_test.HTTPPrivateTLSConfigTests scripts/test/monitor-http-local-online_test.py scripts/ci/check-http-quality-local-registration_test.py`36项通过；未重复真实离线证书组 |
| `make test-build-registration` | 59项＋9项、26个Go服务／59个镜像静态登记通过，不是镜像构建 |
| AST／语法／差异核对 | 仅原4个函数调整及1个纯代理摘要函数新增，其余函数定义、Docker调用及资源／清理逻辑未变；无新入口、依赖、注册或workflow变更 |

原始归档见 `/tmp/addp-p19-http-apt-investigation-20261101/implementation.patch`、`/tmp/addp-p19-http-apt-*-20261101.log`。

<a id="http-trace-apt-natural-linux-1d15-20261102"></a>

### 2026-11-02 HTTP／Trace取证自然Linux门禁通过（正式产品闭环仍未验收）

本记录补核后续自然push CI的原始日志，不重跑任何作业或本地窗口。上一专题的“未提交／推送”描述保留为初始交付状态；用户随后授权发布，六个自有路径已提交并推送为`5dbd12a3c3a85b2da70111107cb56241573710e4`。该固定提交的Platform检查在前置无关`TestTransferReviewSourceDrift`失败，HTTP／Trace runner未执行，不能计通过。

后续自然push／main、attempt 1的[Platform consistency作业124885911941](https://github.com/pampa0629/addp/actions/runs/41319301973/job/124885911941)实际checkout为`1d15c0b44a9314783ab3efaeecd59009ade818b7`，北京时间2026-11-01 23:24:07—23:52:08完成且success；2026-11-02手动取得原始作业日志。核对固定Makefile调用链`test-platform → test-dev-lifecycle → test-infra-runtime-log-lifecycle`及实际顺序，以下两项均执行完成：

| 实际标准入口／检查 | 原始输出及通过范围 |
| --- | --- |
| `make test-monitor-http-local-runner` | 23:28:24，37项／2.163秒，`OK`；后续JS语法、HTTP Online登记校验和System身份Go子门禁完成，23:29:34退出该子Make |
| HTTP离线证书T1 | OpenSSL 3.0.13，35条命令／2.108583秒，材料及输出峰值127023字节，4次预期拒绝，`cleanup_removed=true`；未启动正式产品环境 |
| `make test-monitor-traces-local-runner` | 23:29:39，146项／4.563秒，`OK`；包含已发布APT有界取证测试，JS语法、Online CI登记及System身份Go子门禁通过，23:29:43退出该子Make |
| 作业最终结果 | `Run platform consistency gate`及整个命名作业均success；不由此推导尚未完成的整个workflow或其他专项结论 |

逐字节比较两组runner实现／测试、Online与登记测试共8个Python路径，以及HTTP页面脚本，共9个路径：`5dbd12a3c`、实际CI提交`1d15c0b44`与本次工作区内容一致。因此上述取证实现已有自然Linux确定性门禁证据，不只依赖本地测试。原始日志为`/tmp/addp-p19-natural-124885911941-20261102.log`（0600），701844字节，SHA256=`ada6b2b904c86fd5e7336f17b05198c1327217304f11c7dfe821a976cd936563`。

另沿用已核验的固定`5dbd12a3c`[Product binary build作业124740304489](https://github.com/pampa0629/addp/actions/runs/41269887330/job/124740304489)证据：基线选中Agent，Linux/amd64镜像实际构建exit 0、117.69秒；APT步骤完成10.8秒，Python依赖安装、`pip check`及应用导入／技能注册／OpenAPI检查通过，image ID为`sha256:130d7b75fa1835e4e23c235eebd655c4465e00eadafb776850b563b17d072d2d`，镜像仅在CI加载、未推送。原始日志`/tmp/addp-p19-5dbd-product-20261101.log`为428418字节，SHA256=`b7f29fdd1050bbba626ba3f07c08d50ecef76f53dc2fc42cc359863b7cb7ff3c`。这是不同提交／作业的构建证据，不与本轮runner合并为同一端到端验收。

当前仍为部分实现／部分验收：APT取证的错误分类由确定性反例验证，本次成功构建未触发真实失败解析，不能宣称历史ARM64 APT退出100的原因已定位或修复。正式Agent/Copilot构建后的真实密码／MFA、采集API、Console／Monitor页面、停源、注销与清理闭环仍未验收；停源历史读取仍为另批产品缺口。本轮只读取已有自然CI并更新两份证据文档，未改产品、构建、采样或资源守卫，未手动重跑、开本地窗口、运行Hosted／容量／定时。

<a id="http-formal-product-window-copilot-apt-failed-20261102"></a>

### 2026-11-02 正式产品本地窗口：Agent构建返回成功，Copilot包下载失败并清理

固定源码 `b147ae753422a62761e9ed4c8cbdb147f4e9fb5d`，run_id `addp-http-local-24215048b58345f2bb270a0b2200b4bf`。15 项输入匹配固定提交，生命周期 924.808 秒，Make 退出 2。

准入、Go 编译和缓存回收完成。System／Gateway／Monitor／Agent 构建命令均成功返回，尚无最终镜像身份或正式运行依赖证明。

首败 `product_image_copilot_backend/command_failed_2`。Copilot Dockerfile 第 5 行的 APT RUN 退出 100，未到 Python 依赖安装；真实失败的 `public_build` 诊断完整且未超限。

| 有界字段 | 本次实际事实 |
| --- | --- |
| APT归属／阶段 | parsed，唯一失败步骤的`deb_download` |
| 固定错误类别 | timeout、connection、download；没有可确认HTTP状态码 |
| 官方源 | HTTP Debian主仓库及security仓库；套件均unknown，不据缺失内容猜测 |
| 公共FROM | resolved／python_3_11_slim，`sha256:47380bf198934891ba58e54d4d9142ab993928c70f5c32e63a61101c90face56` |
| 代理来源 | BuildKit和RUN build-args均由既有docker_info读取，HTTP／HTTPS为desktop_internal；host CLI由既有host_environment读取，均为loopback；未保存代理值 |

本次定位到 deb 包下载，具体包、套件、链路节点及代理根因仍未知；早先 Agent APT 失败独立保留。

230 次完整检查无重试，最长 1.316378 秒，私有可写峰值 4249546752 字节，未触资源停止线。

后续前端等镜像、浏览器准备、正式后端、密码／MFA、采集／API、存活及停源页面、注销均未执行。清理 `zero_owned_resources=true`、无失败，缓存子进程停止，4 条归属检查通过，私有工作目录零残留。

原始报告和清理审计见系统临时目录 `addp-p19-http-product-window-20261102-k6gni89x/`。

<a id="http-apt-transport-diagnostic-local-20261102"></a>

### 2026-11-02 APT连接／正文环节有界分类（本地门禁通过，未提交／推送）

本轮先只读关联上一窗口报告、固定b147ae753的Copilot Dockerfile／构建代理传递链及已有Desktop构建／代理日志，不重试构建或产生新负载。在UTC 00:08:30—00:10:20粗时间段，httpproxy.log.0有90行；其中27组官方Debian deb请求均有严格的returning 200及successful记录，未见显式代理error／timeout，时间范围00:08:40—00:09:15。没有当次构建到请求的归属证明、完整正文交付或主机／VM校时证明；该记录不能证明APT已下载完整、排除代理问题，亦不能唯一定位超时节点。已有Desktop构建日志未提供APT原始错误正文；不把HTTP200、旧Document的HTTPS经验或当前tag当作本次因果证据。

脱敏离线关联回执为`/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-p19-copilot-apt-offline-20261102-_8u79jds/correlation.json`（0600）；只保留时间、分类／计数及日志片段哈希，不保存原始URL或代理值。匹配片段17294字节，SHA256=`08d4c18165980f277d31130f654fd0dd66d5aee674d2ab4a6be38f9d5a36223f`。历史失败报告及其54cb014e哈希不改写，不追造连接端点或补填新字段。

用户随后确认仅补最小取证及确定性门禁。先修运行观测与T0–T3规范，再在原唯一public_build失败步骤解析中增加apt.transport：operation为connect／body_read／unknown，endpoint为desktop_proxy／official_debian／unknown，各自用explicit／unavailable／ambiguous表达文字是否明确／缺失／冲突。仅明确建连或正文读取错误语句参与分类，端点只取该语句直接指向的精确白名单主机；不从Failed to fetch源URL、IP标注、其他步骤、Get成功行、命令回显或泛化timeout猜测。凭据、私有／相似域名、畸形端口等不输出端点结论；原文、包名、主机、IP、端口和URL不导出。冲突独立保留unknown，不取最后一条。缺失APT归属及输入／输出超限同样返回封闭未知字段。

| 本轮实际门禁 | 结果及范围 |
| --- | --- |
| 旧实现受控反证 | 新建连端点反例运行1项，3个子断言按预期失败、无额外错误，证明旧实现缺少摘要；不执行真实网络 |
| APT定向回归 | 最终18项通过，含新增8项transport测试；覆盖环节、端点与维度独立缺失／冲突、步骤归属、源URL误归属、恶意输入／脱敏、128 KiB及4 KiB超限、原首因／哈希 |
| `make test-monitor-traces-local-runner` | 154项／4.611秒、JS语法、Online CI登记及System身份Go子门禁通过 |
| `make test-monitor-http-local-runner` | 37项／0.963秒、JS语法／登记及System身份Go子门禁通过；原离线证书T1为OpenSSL4.1.0、35命令／0.863569秒、峰值125489字节、4次预期拒绝、清理完成，无真实产品环境 |
| AST／差异核验 | 唯一共享解析函数改变；其他顶层定义及生命周期、资源、恢复、清理、Docker调用不变，产品Dockerfile、Make入口与workflow未调整 |

本轮代码、两份规范及两份证据共6个自有路径保留为未提交／未推送差异；现有Platform product-build每次push还会按baseline构建Agent；本轮只授权确定性门禁，因此不以推送触发实际镜像构建。日志位于`/tmp/addp-p19-apt-transport-{red,directed,trace-runner,http-runner}-20261102.log`。本地门禁通过只证明新增封闭分类，不代表自然Linux新版本已验证、历史APT根因修复或正式身份／API／页面闭环通过；真实构建另行讨论确认。原四秒、资源／请求／清理预算均保持，未执行APT、Docker探针、产品构建、新窗口、Hosted、容量或定时。

<a id="http-apt-transport-natural-linux-20261103"></a>

### 2026-11-03 APT连接／正文取证自然Linux通过（仅确定性门禁）

本轮手动读取后续自然push CI，不重跑作业。前述连接／正文环节取证的“未提交／推送”保留为初始交付状态；用户随后授权发布，六个自有路径已提交并推送为`d5a9a3f219d528395f8f4d35560898687f253f83`。该提交及早期后继作业在前置检查退出，未执行runner，历史未运行结论不改写。

本次独立核验[Platform consistency作业125494234191](https://github.com/pampa0629/addp/actions/runs/41508601094/job/125494234191)的完整原始日志，实际checkout为`f1a50c03ad178c6cdbf87d42251a000f5110abee`，自然push／main／attempt 1。作业于北京时间2026-11-03 03:17:51—03:46:33运行；固定Makefile的`test-platform → test-dev-lifecycle → test-infra-runtime-log-lifecycle`顺序与日志中的嵌套子Make边界一致。命令由Make的`@`隐藏，故不以日志没有入口名称推断未执行；以下测试输出、身份结果及子Make成功退出共同确认实际执行。

| 实际标准入口／检查 | 原始输出及范围 |
| --- | --- |
| `make test-monitor-http-local-runner` | 03:23:17，37项／2.536秒，`OK`；后续两项JS语法、HTTP Online登记校验和System身份Go子门禁完成，03:24:54退出该子Make |
| HTTP离线证书T1 | OpenSSL 3.0.13，35条命令／2.427082秒，材料与输出峰值127011字节，4次预期拒绝，`cleanup_removed=true`；仅临时证书检查 |
| `make test-monitor-traces-local-runner` | 03:24:59，154项／4.845秒，`OK`；包括PublicAPTBuildEvidence中的8项新增transport反例；两项JS语法、Online CI登记及System身份Go子门禁通过，03:25:23退出该子Make |
| 整个命名作业／workflow | 作业后续其他runner失败，最终failure；本轮不调查或修复，不将本专项通过扩大为Platform整体通过 |

两组runner实现／单元／Online／登记测试共8个Python路径与HTTP页面脚本共9项逐字节比较：发布提交`d5a9a3f21`、实际CI提交`f1a50c03a`及本次工作区均一致，包含新增封闭的连接／正文与端点分类。原始日志505397字节，SHA256=`58fb981a256759f8ed70fecd103f838bbd45044d640b470c7327dcc8992251dc`；日志、收据及9项来源摘要保存在`/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-p19-http-trace-natural-20261103-xed4ix9r/`（目录0700、文件0600）。同目录其他作业日志不与本固定提交合并为一次验收。

本次补齐新增APT取证的自然Linux确定性门禁证据，不代表真实APT失败分支已运行、历史Copilot下载根因已定位或修复。正式Agent/Copilot构建后的真实密码／MFA、采集API、Console／Monitor页面、停源、注销与清理闭环仍未验收；停源历史读取仍为另批产品缺口。原四秒及资源预算未变；未改产品、构建策略或守卫，未手动重跑、新开本地窗口、运行Hosted／容量／定时。当前仍为部分实现／部分验收。

<a id="http-formal-product-window-resource-timeout-20261103"></a>

### 2026-11-03 正式产品本地窗口：编译期间资源检查超时，失败并清理

固定源码 `92309920b57a591e5b43679c424bee4a08132fe1`，run_id `addp-http-local-c882966d9be242b29074cff21d930fd5`。15 项输入匹配，生命周期 184.123 秒，Make 退出 2。

准入通过后进入 Go 编译。首败 `product_compile/resource_sample_deadline_exceeded`，末步骤 `measure_owned_volumes`。编译器未报告首败，尚未进入缓存回收或镜像构建。

| 失败检查中的原步骤 | 实际耗时与结果 |
| --- | --- |
| 当次容器列表 | 1.018326秒通过，命令等待0.996457秒 |
| `inspect --size`及解析 | 1.130476秒通过，命令等待1.120275秒 |
| 容器状态校验 | 0.000029秒通过；未以OOM或配额超限作为首因 |
| 当次卷列表 | 0.164958秒通过，命令等待0.151944秒 |
| 当次卷测量 | 在剩余预算内未完成，步骤1.691910秒；进程创建0.017756秒、等待1.667163秒、取消回收0.004976秒，记录完整 |
| 整次失败检查 | 4.006712秒（含取消回收等实际收尾），锁等待0.000022秒；维持原四秒判失败，不延长预算、不读取重试 |

前 37 次完整检查通过，最长 3.170970 秒；完整私有可写峰值 1985048576 字节。失败轮缺卷及私有目录完整占用。现有数据支持多步等待累计耗尽四秒，无法拆分 CLI／daemon 等待和容器内卷扫描。

Go 编译未完成，后续构建、业务请求、正式身份、采集／API、页面、停源和注销未执行。清理 `zero_owned_resources=true`、无失败，4 条归属检查通过，私有工作目录零残留。

原始报告和清理审计见系统临时目录 `addp-p19-http-product-window-20261103-liev9el8/`。

<a id="http-volume-checkpoints-local-20261103"></a>

### 2026-11-03 卷测量检查点与有界残片（仅本地确定性门禁）

用户确认先修规范、补检查点与确定性门禁；不重试构建、新开本地窗口或运行Hosted／容量／定时。先只读核对上一专题固定92309920b的424292字节报告及a5fa541e哈希、37次成功检查与末次失败：锁等待0.000022秒，失败轮子进程等待合计3.935839秒、创建合计0.046891秒，输出处理0.001527秒、取消0.004976秒。相邻完整检查从0.458457秒升至1.162273、2.966612、3.170970秒，再耗尽四秒；当时3个容器／7个卷，没有重建测量器。只能定位多条串行命令累计等待，不能唯一归因Docker CLI、daemon、VM调度或实际卷扫描；未据此调整资源、轮询、互斥或读取路径。

先修运行观测和T0–T3规范，再在原有同一次卷测量exec的原du／awk管道前后，用shell内建printf增加固定进入／返回标记。原纯数字返回路线删除，完整标记、原数字与错误输出仍走唯一统计校验。返回标记仅表示该检查点已观测，管道中的du错误仍拒绝；不当成统计完成、成功或底层原因。原串行顺序、CLI／Unix socket与API协商、完整统计范围、辅助容器及资源预算保持。

命令内核仅对采样线程、当次确切测量器及该命令完整参数匹配时，从原私有捕获文件读取最多4 KiB。检查点沿原command_id／run_id／phase保存于同一命令记录；结构化摘要最多512字节，只包含fragment_status、measurement_entered、pipeline_returned及examined_bytes。缺失记not_observed，冲突／畸形、超限、读取失败分别明确invalid／over_limit／unavailable，不保存原文、路径、参数或凭据。诊断耗时另记checkpoint_processing_seconds，计入实际命令和总耗时；成功仍须在原截止前通过最终检查，失败后的残片不能重置截止或反转原异常。超时前已经捕获返回标记也仍保持超时，诊断读取异常不覆盖首因。

| 实际验证 | 结果及证据范围 |
| --- | --- |
| 原管道受控反证 | 以旧无标记管道运行同一受控shell用例，1项按预期失败、0额外错误；不读取真实卷、不调用Docker |
| 新增检查点定向回归 | 9项，纳入下述Trace runner；覆盖实际shell帧格式及原求和、du错误仍可出现返回标记、完整／部分／冲突／截断／超限／不可用、确切命令归属、最多4 KiB读取及512字节摘要、脱敏、原超时／非零退出／诊断异常与成功后到期拒绝 |
| `make test-monitor-traces-local-runner` | 最终163项／5.523秒、JS语法、Online CI登记及System身份Go子门禁通过；无真实部署或构建 |
| `make test-monitor-http-local-runner` | 最终37项／2.063秒及既有JS／HTTP登记／System身份子门禁通过；离线TLS T1为OpenSSL4.1.0、35命令／1.909249秒、峰值125465字节、4次预期拒绝及清理完成，非正式产品身份或采集链 |
| AST／差异核验 | 既有定义仅Lifecycle.command与_sample改变，新增两个私有诊断函数；sample／monitor／互斥、配额／OOM、恢复／清理、builder／产品构建／缓存回收定义保持；未改Dockerfile、Make或workflow |

初轮HTTP runner因离线TLS夹具直接调用命令内核、无新增实例方法而失败；已将捕获实现收敛为内核独立函数，保留原TLS夹具及产品TLS，最终37项恢复通过。HTTP资源错误替身只同步新帧格式，保留原“不读取重试／不忽略du失败”断言。登记检查通过。

原始归档见 `/tmp/addp-p19-volume-checkpoints-{controlled-red,directed,trace-runner,http-runner}-20261103.log`、`系统临时目录／addp-p19-volume-checkpoints-before-20261103-s3m9sy6q/`。 本批7个自有路径本地落地，未推送，避免本轮额外触发基线Agent镜像构建；自然Linux新版本和真实超时残片尚未运行验证，不把本地诊断通过当作四秒性能问题已解决。上一窗口失败及零残留清理结论保持，原4秒、8 CPU／12 GiB、4 GiB停止线、最长五秒开始间隔与清理期限均不变；没有新增Docker请求、容器、线程、探针、/proc读取或跨VM校时。

<a id="http-volume-checkpoints-natural-linux-20261103"></a>

### 2026-11-03 卷测量检查点自然Linux分项通过（整作业取消，真实超时未验）

本轮仅手动核对后续自然CI。上一专题的“本地落地，未推送”保留为当时交付状态；随后只读核验远端main已包含`b0420db65a0c817697255d19c4ba3b1914e19703`，本轮没有执行推送、手工派发或重跑。较早排队／运行中状态不计通过，以下以实际原始日志补齐新版本确定性证据。

自然push／main、attempt 1的[Platform consistency作业125684763917](https://github.com/pampa0629/addp/actions/runs/41565147207/job/125684763917)实际checkout为`dfeff7c9b49cea102d0770e9956f59402196fb99`。作业北京时间10:35:19启动，11:35:39最终cancelled；原始完整日志确认本专项子Make此前已成功退出，不能因后续取消抹去已完成分项，也不能将分项成功写为整作业通过。

| 实际标准入口／检查 | 原始日志及通过范围 |
| --- | --- |
| `make test-monitor-http-local-runner` | 10:40:21，37项／2.669秒，`OK`；两项JS语法、HTTP Online登记及System身份Go子门禁完成，10:41:43退出该子Make |
| HTTP离线证书T1 | OpenSSL3.0.13，35条命令／2.395283秒，材料和输出峰值127027字节，4次预期拒绝，`cleanup_removed=true`；仅临时证书验证，无正式产品环境 |
| `make test-monitor-traces-local-runner` | 10:41:48，163项／4.915秒，`OK`；包含9项新增卷检查点回归；两项JS语法、Online CI登记及System身份Go子门禁完成，10:42:07退出该子Make |
| 整个作业／workflow | 该命名作业后续取消，不计整体通过，不调查无关检查或取消原因。另一作业125683681659元数据success，但两次原始日志读取失败，本轮不使用其元数据扩大本专项通过结论 |

固定提交的Make调用链`test-platform → test-dev-lifecycle → test-infra-runtime-log-lifecycle`、原入口内容及日志嵌套顺序／退出边界一致。逐字节比较两组runner实现／单元／Online／登记测试8个Python路径及HTTP页面脚本共9项：发布版本`b0420db65`、实际CI提交`dfeff7c9b`和本次工作区均一致；源码及输入摘要绑定本次证据，不仅凭相同测试数量认定版本。

原始归档见 `系统临时目录／addp-p19-checkpoints-ci-read-20261103-5aq66577/125684763917.log`。

至此新检查点有本地及自然Linux确定性通过证据，真实卷测量超时的残片尚未运行采集，不能宣称已定位CLI／daemon／卷扫描根因或解决四秒性能问题。固定92309920b本地窗口失败与零残留清理结论不变；正式Agent/Copilot构建、真实密码/MFA、采集/API、页面、停源及注销闭环仍未验收，停源历史读取另批。当前保持部分实现／部分验收。本轮只更新证据与索引，不修改产品、预算或取证实现，不手动重跑、新开本地窗口或运行Hosted／容量／定时。

<a id="http-checkpoint-follow-clauses-20261103"></a>

### 2026-11-03 真实检查点随行采集条款确认（仅准备，未运行）

本次确认随行采集条款，真实采集仍待另行授权。随原负载记录卷测量进入／返回检查点，编译阶段初值为至少 12 次完整检查。样本不足如实记未覆盖。完整四秒、最多五秒开始间隔、8 CPU／12 GiB、4 GiB 停止线和原清理期限保持。

本次范围为文档静态检查。真实采集、超时残片、性能根因和正式产品页面仍未验收。旧窗口失败与 Linux HTTP 37／Trace 163 的分项通过及整 Job 取消分别保留。[T0–T3 验收条款](../T0-T3测试规范.md) · [随行采集准入](../../addp平台运行观测规范.md#volume-checkpoint-follow-acceptance)

<a id="http-compile-checkpoints-local-20261103"></a>

### 2026-11-03 仅Go编译阶段真实检查点采集通过（偶发超时未复现）

用户明确批准先修规范、原入口及确定性门禁，再运行一次独立15分钟本地取证；13分钟停止新工作，保持原四秒、8 CPU／12 GiB和4 GiB停止线，首次失败即停清理、不自动重试或续窗。本授权仅本次compile-checkpoints，不将旧“随行采集待授权”解释为长期禁令或自动新窗口。

准备提交`b5a36b44f1edc1601018b2fbc7d8524095a9f261`：原`make test-monitor-http-local`新增受限`MONITOR_HTTP_LOCAL_CASE=compile-checkpoints`，缺省仍full，非法case在资源创建前拒绝。唯一构建内核只增加编译完成即返回，复用原Go构建、准入、守卫、检查点及清理，不进入缓存回收、构建器恢复、产品镜像、APT、浏览器或身份／业务。所有命令扣减900／780秒及原120秒清理绝对预算，report及终端明确case，不把诊断通过当作正式产品通过。共享登记文件只提交本专项2行校验，其他会话原差异保留；本轮没有执行推送。

实际运行前最小门禁：`make test-monitor-http-local-runner`44项／0.839秒、JS语法／HTTP登记／System身份Go通过；新增6项case回归及1项登记反例，覆盖默认full原setup／business路径、严格case、900／780／120秒、同一Go容器参数、编译后镜像／缓存／身份／业务阻断、最小样本／阶段／归属／重复／缺失／超限／四秒／五秒拒绝与清理。原离线TLS T1为OpenSSL4.1.0、35命令／0.749348秒、125481字节、4次预期拒绝、清理完成。`make test-monitor-traces-local-runner`163项／4.676秒、JS／Online登记／System身份Go通过。AST复核共享内核仅build_products定义改变，命令／四秒扫描／检查点／监测／互斥／清理／builder与缓存回收原定义未改，Make及CI登记同步命中原入口，公共CI仍禁止任何真实case。上述门禁为提交前共享工作区结果，本专项实现及case登记函数与冻结提交一致，其他会话登记差异未并入；不据此宣称固定b5a36b44f全仓CI通过。

本次准备于北京时间2026-11-03 14:16:35.963869开始，13分钟工作截止14:29:35.963869、15分钟总截止14:31:35.963869；先执行标准Infra状态读取，不接管个人服务。只运行一次`make test-monitor-http-local MONITOR_HTTP_LOCAL_CASE=compile-checkpoints`，入口14:17:14.478811开始，14:20:27.967320结束；含准备232.003425秒，生命周期192.703208秒，make退出0／report passed。固定上述b5a36b44f，run_id=`addp-http-local-f62be96e4f1044eea8f537d36012c83f`；12项入口与3项集成文件的15项摘要逐一匹配当次保存值及固定源码，无未提交工作混入。

| 实际检查 | 结果及边界 |
| --- | --- |
| 原Go编译 | 原System／Gateway／Monitor／runtime-log编译命令成功，2 CPU／3 GiB、原线程／依赖缓存设置；约16.86秒进入编译，190.01秒进入诊断汇总，不运行镜像构建或业务 |
| 完整资源检查 | 46次成功：43次product_compile、2次测量器准入、1次最终汇总；最大0.463051秒，最大开始间隔4.005049秒，零读取重试 |
| 真实编译检查点 | 43条当次唯一命令，全部有完整进入／管道返回标记及同run／phase／command_id，原计量全部有效；残片读取最大64字节，摘要最大114字节；超过12次最低要求 |
| 资源峰值 | 合计硬配额2.25 CPU／3288334336字节内存；卷／可写层／私有目录完整占用峰值4027449344字节，低于4 GiB停止线（该字段已包含可写层，不重复相加）。剩余约255 MiB，不由此保证后续完整产品构建空间充足 |
| 退出清理 | cleanup.zero_owned_resources=true、failures为空；4条cleanup归属列表记录完整通过，当次私有工作目录剩余0，标准入口及外部编排均退出，无13分钟截止信号、无重试或续窗 |

本轮assertions为空，报告独立scope为compile_checkpoint_collection_only、controlled_requests=0；没有产品images、Python依赖、浏览器、采集API、停源、注销或缓存回收回执。仅认“编译阶段真实检查点采集通过”。本次未复现偶发超时，不能认定先前CLI／daemon／VM或卷扫描性能问题已定位或修复；旧92309920b失败仍保留，历史Copilot APT下载根因未改变。正式Agent／Copilot产品构建→真实密码／MFA→采集／API→Console／Monitor页面→停源／注销清理闭环仍未验收，停源历史读取另批。本轮未运行Hosted、容量或定时，单次授权已经使用，不复用余时追加其他场景。

原始归档见 `系统临时目录／addp-p19-compile-window-20261103-kc5b9jtp/`、`系统临时目录／addp-p19-compile-profile-before-20261103-auwt01zn/`。

<a id="http-full-monitor-apt-index-failed-20261103"></a>

### 2026-11-03 单次完整本地窗口在Monitor镜像APT索引阶段失败

固定源码 `acff6e395e7f4ae7e7a6ec91a351ddf542b04950`，run_id `addp-http-local-ae777177f61145fc9ed4863e4874235b`。15 项输入匹配，生命周期 467.722 秒，Make 退出 2。

| 实际阶段 | 结果及边界 |
| --- | --- |
| 准入／Go编译 | 固定ARM64 Nginx缓存、当次私有证书／配置投影／空卷／构建器及卷测量器准备成功，14.69秒进入原Go编译；编译命令成功返回，169.55秒进入原缓存预检 |
| Go缓存回收 | 预检0.262398秒，删除含锁等待0.593403秒，分别低于原四秒；child_stopped／cleanup_child_stopped为true，未保留清理锁，不改统计范围或删除其他缓存 |
| 产品镜像 | System构建命令成功返回后于241.71秒进入Gateway，Gateway成功返回后于386.86秒进入Monitor；Monitor构建命令失败，463.42秒进入清理，尚未执行Agent／Copilot或前端镜像构建 |
| 首因 | stage=`product_image_monitor_backend`、error_code=`command_failed_2`；外层构建命令退出2，失败APT进程退出100。唯一失败步骤的封闭诊断为`index_download`，error_kinds为signature／download，官方Debian／HTTP／bookworm；无HTTP状态，连接操作与端点均unknown／unavailable，FROM摘要unavailable |
| 完整资源检查 | 保存116次完整成功样本，其中Go编译38次；最长0.604424秒，开始最大间隔4.005120秒，零读取重试。116条当次唯一命令的进入测量／管道返回检查点均完整，最大已读残片67字节；无resource_guard_failure，本轮未复现四秒超时 |
| 资源峰值 | 硬配额2.25 CPU／3288334336字节内存；卷／容器可写层／私有目录完整占用峰值4027965440字节（约3.75 GiB），低于4 GiB停止线。该字段已包含可写层，不重复相加，不证明未运行的后续镜像与页面空间充足 |
| 清理 | 首次失败即停；cleanup.zero_owned_resources=true、failures为空，4条cleanup归属列表记录完整通过，当次私有工作目录剩余0，入口及外部编排退出；无截止信号、重试或续窗 |

封闭诊断只定位到 APT 索引签名／下载。代理字段只记录配置来源类别，无请求路径或原始正文，未确定仓库、代理、缓存或网络根因。

没有最终镜像身份、Python 依赖、受控请求、密码／MFA、采集／API、页面、停源或注销回执。Agent／Copilot 构建未开始，旧 Copilot deb 下载首败单列。

原始报告和清理审计见系统临时目录 `addp-p19-http-full-window-20261103-7cmces4n/`。

<a id="http-monitor-apt-signature-diagnostics-20261103"></a>

### 2026-11-03 Monitor APT索引只读关联与最小取证门禁

本批先只读关联上一专题固定acff6e395、run_id=addp-http-local-ae777177f61145fc9ed4863e4874235b的原始报告及b0811197哈希，再按用户确认实施最小取证。没有重试构建、运行APT、新开本地窗口或执行Docker探针。冻结源码中Monitor／System／Gateway的FROM及APT命令完全一致，均由本地Registry的debian-slim:bookworm开始，安装ca-certificates／tzdata；标准预热映射到debian:bookworm-slim。预热可以复用已有Registry tag，且三次实际解析摘要未保留，不能由相同Dockerfile或前两项构建成功推定实际基础层完全相同；不读取当前可变tag冒充历史。

只读Desktop既有httpproxy.log，在由入口UTC起点及四舍五入阶段耗时形成的候选时段内，System及Gateway各记录6条官方Bookworm系列索引GET状态行且均200；Monitor时段有5条，其中Bookworm InRelease一条502，updates／security各两条200。该502的同一代理请求共3条日志，06:56:38.296333—06:56:43.298023 UTC跨度5.001690秒，后续虽有successful日志仍不能当作上游200或正文完整。只确认代理请求内关联，缺少构建Owner归属、host／VM校时和正文，不能把这条502回填原APT报告的HTTP状态或唯一归因签名错误。各候选时段选中内容7272／7538／3477字节，只保留计数、封闭分类及片段哈希，未保存原文、请求ID、URL或代理值。旧Copilot deb下载失败继续单列，不合并根因。

实现提交`ea8923111ba6647795a938f9ed4082768de39a2c`仅含唯一分类器、其测试及两份规范。先补运行观测和T0–T3条款，再实现精确Bookworm FROM白名单／摘要及重复去重／多候选ambiguous；APT错误上下文统一沿唯一失败步骤的明确错误及带缩进固定错误续行，成功Get／Hit／Ign、普通进度、空行、其他步骤、未编号行、RUN及ERROR命令回显均中断。签名新增signature_kinds封闭枚举，明确子类优先、没有具体证据才记verification_failed；缺失／不可归属／超限为空，不保留旧独立签名识别路线或任意附带值。原128 KiB整份输入、4 KiB整组诊断、首因／哈希及资源／四秒／清理语义保持。原规范Python-only描述同步收敛到当前白名单，不留冲突口径。

| 确定性验证 | 实际结果与范围 |
| --- | --- |
| 旧解析器反证 | 原make test-monitor-traces-local-runner新增6项测试后运行169项／4.702秒，2 failures／23 subtest errors；包括Debian摘要缺失、错误续行HTTP502遗漏及新签名字段缺失，按预期失败，不计通过 |
| 最终Trace runner | make test-monitor-traces-local-runner退出0；169项／4.559秒、两项JS语法、Online CI登记及System身份Go子门禁通过 |
| 最终HTTP runner | make test-monitor-http-local-runner退出0；44项／0.907秒、两项JS语法、HTTP登记及System身份Go子门禁通过。原离线证书T1为OpenSSL4.1.0、35命令／0.802712秒、材料与在途输出预留125489字节、4次预期拒绝、清理完成；不是产品构建或正式身份实链 |
| 独立自查 | AST仅public_build_failure_diagnostic改变，所有构建／命令／采样／互斥／恢复／清理定义不变；9项产品Dockerfile／Make／CI登记与基线一致，测试沿原聚合自动发现，不新增入口或workflow；diff检查通过 |

首轮绿色测试后日志包装使用zsh只读变量status而退出1，两组子门禁输出当时已通过；未把包装异常当完整入口通过。收紧普通进度边界后改由Python记录最终make退出码，以上最后两组均明确退出0，作为本批交付证据。只运行受控文本、替身、原离线证书及身份子门禁，没有真实APT、镜像或网络探针；不以分类通过声称历史APT或资源超时根因修复，正式Agent／Copilot到Console／Monitor页面仍未验收。

原始归档见 `系统临时目录／addp-p19-monitor-apt-offline-20261103-czdxdxpo/`、`/tmp/addp-p19-monitor-apt-{red,trace-final,http-final}-20261103.log`。

<a id="http-full-agent-build-deadline-20261104"></a>

### 2026-11-04 单次正式产品full窗口在Agent镜像命令期限失败

固定源码 `8a34ecb841881c507ceb700d69f6126c2ef50ec5`，15 项入口／集成文件匹配；准备阶段 HTTP runner 44 项、Trace runner 169 项及离线 TLS 通过。

run_id `addp-http-local-711a69cbda83495383c2a33b08cc62b5`，生命周期 983.118 秒，Make 退出 2。未触发 55 分钟外层截止。

| 实际阶段／核验 | 结果及边界 |
| --- | --- |
| 准入与Go编译 | 固定ARM64 Nginx缓存／隔离网络／私有证书／配置投影／受限构建器／测量器通过；24.21秒进入Go编译，270.29秒进入缓存预检，原编译命令成功 |
| 缓存及镜像 | 完整资源预检0.585184秒、当次Go缓存删除含锁等待1.468580秒，分别守原四秒，回收子进程退出及cleanup_child_stopped=true。System／Gateway／Monitor镜像构建命令依次成功返回，327.24／354.75／378.21秒进入下一阶段；本轮未复发上一acff源码的Monitor APT失败，不据此认定间歇根因修复 |
| Agent首因 | 378.21秒进入product_image_agent_backend，978.81秒进入清理；同一产品镜像命令达到原600秒期限，error_code=command_deadline_exceeded。不是完整资源检查超时、4 GiB停止线或55分钟工作截止，未将其改写为原生进程退出码或APT失败 |
| 输出取证缺口 | command_failure和resource_guard_failure均未出现。原command在等待异常路径先终止／回收子进程后抛出，未进入正常退出后的输出读取／public_build分类，临时输出随上下文关闭。现有归档没有构建正文、哈希或APT／pip细阶段，不能确定是哪类安装／网络等待或唯一根因，亦不能用旧502或其他构建的诊断补填 |
| 完整守卫 | 242次完整成功样本（Go编译61次、Agent构建150次），最长2.187825秒，开始最大间隔4.008027秒，零读取重试；242条同run／phase／唯一command_id的原卷检查点完整，最大读取68字节 |
| 资源峰值 | 硬配额2.25 CPU／3288334336字节内存；卷／容器可写层／私有目录完整占用峰值4158320640字节，约3.87 GiB且低于4294967296字节停止线，已包含可写层，不重复相加。后续未执行阶段空间是否充足仍未知 |
| 清理与未运行项 | cleanup.zero_owned_resources=true、failures=[]；4条cleanup归属列表记录完整通过，当次工作目录0，入口与外部监督退出。assertions为空，无images最终身份、完整Python依赖、受控请求、browser或停源回执；Copilot及前端镜像、密码／MFA、采集／API、页面、停源、注销未执行，不借资源／清理通过反转整轮failed |

原始报告和清理审计见系统临时目录 `addp-p19-http-full-window-20261104-ycdtsdrn/`。

<a id="http-product-timeout-bounded-diagnostics-20261104"></a>

### 2026-11-04 Agent超时只读关联与有界构建诊断门禁

先按用户要求只读关联上条固定8a34ecb84／run_id=addp-http-local-711a69cbda83495383c2a33b08cc62b5原报告及ebe06bd9哈希、冻结Agent Dockerfile和标准构建调度。Agent阶段候选UTC时段为2026-11-03 22:25:45.895965—22:35:46.495965（由入口时间及舍入阶段耗时形成）；既有Desktop httpproxy.log选中240行／45639字节，片段SHA256=`4f725583e18cbff68f4a15f1014a73e9ea3a8d9041cd4e3949b9f6773252fd23`，仅保存封闭计数和哈希。官方Debian GET状态67条均200，最后22:27:11.685694；随后Python包站点相关30行自22:27:36.851383至22:35:46.153004，主要为HTTPS CONNECT活动。未见该候选时段明确错误词或4xx／5xx状态，但无构建Owner归属、host／VM校时及HTTPS正文，不能据此证明APT完整成功、pip具体阶段或网络正常，也不能把隧道successful当包下载完成。

冻结Agent构建先APT，再升级pip／安装第三方依赖、安装Common源码包、pip check及导入验证；候选代理时序支持调查Python依赖相关活动，尚不足以把600秒唯一分配给下载／解析／安装。源码确认超时异常在原等待循环触发，经过原取消finally后直接离开临时输出上下文，未执行正常退出之后的读取及public_build；标准构建调度的临时日志也在退出时清理。历史报告无构建正文及输出哈希，不能恢复细阶段或补造APT／pip错误。

用户随后明确确认先修规范与确定性门禁，不运行真实构建。实现`467957be45c8e0c920fed1e2c33cf90121a27e4e`包含两份规范与原共享解析器／测试四个路径：原command仅增加同一临时输出上下文的捕获协调，限非采样线程、精确product_image服务及当次make build-images命令、原command_deadline_exceeded异常。在原取消流程返回后、输出关闭前最多读取131073字节；不增加等待、进程、Docker或网络命令，不改600秒或取消顺序。完整捕获才保存SHA256并沿唯一public_build解析；超128 KiB明确over_budget、无完整末尾换行记truncated、读取／分类异常记unavailable，均不解析前缀或保留部分哈希；空捕获可complete且progress未知，complete不表示构建完成。

唯一分类器补termination／output_status／progress：正常非零沿原process_exit错误归属规则，deadline只从完整带编号标题及DONE／CACHED／ERROR终态排除已完成候选，CANCELED仅为取消观察仍属未完成；内部标题最多64项。唯一候选输出apt／python_dependency／common_package／verification／other固定类别，无候选unavailable、多候选ambiguous、重复编号invalid，后三种stage均unknown。不导出步骤号、命令、包名、URL、路径或原文，不据CONNECT、成功消息或最后一行猜阶段；4 KiB整组诊断及原集合上限保持。超时回执记读取字节数和诊断实际耗时，不输出取消造成的exit143替代超时首因，诊断异常不覆盖原异常对象，既有四秒／资源／清理期限不重置。

| 本地确定性验证 | 实际结果及边界 |
| --- | --- |
| 旧实现反证 | 原Trace runner新增初轮5项用例后174项／4.547秒，预期1 failure／13 subtest errors，包括缺超时捕获与新上下文参数；不计通过 |
| 最终Trace runner | make test-monitor-traces-local-runner退出0，175项／4.564秒；两项JS语法、Online登记、System身份Go子门禁通过。新增6项并扩展子例覆盖各固定阶段、已完成／取消／并行／重复／编号碰撞／伪命令／64项上限、完整／空／截断／超限／读取和解析异常、精确命令／run归属、非目标线程和错误拒绝、取消先于解析、131073读取上限、首异常及脱敏 |
| 最终HTTP runner | make test-monitor-http-local-runner退出0，44项／0.913秒、JS／HTTP登记／System身份Go子门禁通过；离线TLS OpenSSL4.1.0、35命令／0.782257秒、材料与输出预留125469字节、4次预期拒绝、cleanup_removed=true，不是产品验收 |
| 自查与登记 | AST仅public_build_failure_diagnostic及Lifecycle.command定义改变，并新增一个有界捕获协调函数；原取消finally AST不变，其他采样／互斥／构建／监测／恢复／清理定义不变，产品Dockerfile、Make及标准构建脚本未改。原HTTP／Trace Make聚合及CI自动发现命中，无新入口／workflow。共享登记文件有其他会话未提交的Security改动，未纳入本提交；登记通过是当时共享工作区结果，不声称本提交全仓CI通过 |

原四秒、8 CPU／12 GiB、4 GiB停止线、60／55分钟、600秒及请求／失败清理边界保持。确定性只证明新诊断实现，不补齐历史Agent超时、目录消失根因或正式Agent／Copilot到Console／Monitor页面；旧失败报告不追填新字段。只读关联及冻结文件SHA256在`/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-p19-agent-deadline-offline-20261104-we42ib19/correlation.json`，实现／输入／日志审核为同目录implementation-audit.json（0700／0600）；门禁日志`/tmp/addp-p19-timeout-diagnostic-{red,trace-final,http-final}-20261104.log`（0600）。本轮仅本地提交，不推送触发构建、不重试构建或新开窗口，不运行Hosted／容量／定时；更新60主题证据并回报维护会话。下一步建议在另行批准的原产品运行中使用超时诊断，再依据实际步骤讨论性能或网络修正，不预先放宽期限。

#### 2026-11-04 09:04 单次full授权在额外准备检查停止（正式入口未启动）

冻结源码 `f9737960ee2dd4b9dd44e9cabeef6574293468e4`。外加准备检查错误要求共享 Makefile 整份匹配提交，在比较时发生 `AssertionError`。正式准入只核 12 项自有输入和三份集成标记，失败发生在正式入口之前。

三个实际 Make 目标和 12 项自有文件核对一致。HTTP runner 44 项、Trace runner 175 项及离线 TLS 通过，仅为确定性证据。

`entry_started=false`、`build_attempts=0`、无 runtime run_id 或隔离资源，运行清理为 `not_required`，离线证书已清理。真实构建、密码／MFA、采集／API、页面及超时诊断均未执行。

准备及停止回执见系统临时目录 `addp-p19-http-full-timeout-window-20261104-mtufr_1s/`，没有 full 报告。

<a id="http-full-agent-timeout-over-budget-20261104"></a>

### 2026-11-04 标准准入full窗口Agent再超时及诊断超预算

固定源码 `3506802ce1e6708e919fcdc78a3e356fa7d0094b`。沿标准 12 项自有输入和三份集成标记准入，15 项摘要匹配；HTTP runner 44 项、Trace runner 175 项及离线 TLS 通过。

run_id `addp-http-local-846e85021cdf4cc5873e1f6c650522de`，生命周期 868.563 秒，Make 退出 2。Go 编译、缓存回收和 System／Gateway／Monitor 镜像构建命令成功。

首败 `product_image_agent_backend/command_deadline_exceeded`，Agent 达到原 600 秒期限。新增诊断实际返回 `termination=deadline`、`output_status=over_budget`、`captured_bytes=131073`、`limits_exceeded=[input_bytes]`。128 KiB＋1 仅证明超过读取上限，未保留输出摘要或原文，构建阶段及根因仍未知。

| 守卫与退出核验 | 实际结果 |
| --- | --- |
| 完整样本 | 215次全部成功，含Go编译43次、Agent阶段150次；最长0.501020秒，开始最大间隔4.010097秒，读取重试均0；215条同run／phase／唯一command_id的原卷进入／管道返回检查点完整 |
| 资源峰值 | 硬配额2.25 CPU／3288334336字节内存，卷／可写层／私有目录占用4031909888字节，约3.75 GiB且低于4294967296停止线；总数已含可写层，不重复加。无resource_guard_failure，首因不是资源读取超时或触线 |
| 清理 | cleanup.zero_owned_resources=true、failures=[]；4条cleanup归属列表记录完整通过，当次工作目录0；标准入口及外部监督已退出，未重试／续窗或复用余时 |
| 未运行 | assertions为空，无images最终身份、完整Python依赖、controlled_requests、browser或source_stop_summary回执。Copilot及前端镜像、真实密码／MFA、采集／API、Console／Monitor页面、一次停源及注销均尚未执行，不借构建分项／采样／清理通过反转整轮失败 |

原始报告和清理审计见系统临时目录 `addp-p19-http-full-standard-window-20261104-gvjti9if/`。

<a id="http-incremental-build-progress-20261104"></a>

### 2026-11-04 有界增量构建步骤观测与确定性门禁

旧 `3506802ce` 的 131073 字节只证明捕获超过 128 KiB。历史构建正文混合多个步骤，具体耗时阶段未知。实现 `0a7d3d3682613e00bb184826d6eded20a28ce36d` 改为同一构建命令的有界增量读取。每轮 32 KiB、累计 8 MiB、单行 4 KiB、64 步，解析 CPU 1 秒、摘要 4 KiB。只有完整、连续且唯一的未完成步骤可归类，歧义、截断或超限保留未知。主机观察端点只代表本机观测。保留原 600 秒及完整四秒资源检查。

旧路线反证为 1 项失败、21 个 subtest 错误。最终 Trace runner 178 项、HTTP runner 44 项及 JS、登记、身份和离线 TLS 子门禁通过，覆盖跨块、超限、隐私、取消及首失败保留。真实构建、APT／网络、Hosted、容量和正式产品页面未运行。旧 `a82c1e82` 的失败与历史根因保持原结论。

方案审查与验证原件分别在 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-p19-output-progress-assessment-20261104-fq2xv7n2/` 和 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-p19-stream-progress-20261104-5v3aewmd/`。

<a id="http-full-stream-resource-deadline-20261104"></a>

### 2026-11-04 增量步骤full窗口在Gateway构建期间资源四秒失败

准备源码 `ada61ed137dfa776db8cd97e7d270d97aefee420`，正式运行固定 `8b320fb8eaa35696753dd3d3b40575a94a5324ef`。12 项自有文件未变，最终 15 项摘要匹配。

HTTP runner 44 项、Trace runner 178 项及离线 TLS 通过。run_id `addp-http-local-29078910b1d34180b764f66260daacc8`，生命周期 376.287 秒，Make 退出 2。

Go 编译、缓存回收及 System 镜像命令成功后进入 Gateway 构建。首败 `product_image_gateway/resource_sample_deadline_exceeded`，末步骤 `measure_owned_volumes`；没有 `command_failure` 回执。

| 失败完整检查步骤 | 实际耗时及意义 |
| --- | --- |
| list_owned_containers | 1.418197秒，原CLI等待1.405629秒 |
| inspect_owned_containers | 1.045219秒，等待1.033554秒 |
| validate_container_states | 0.000025秒 |
| list_owned_volumes | 0.097149秒，等待0.090748秒 |
| measure_owned_volumes | 1.449310秒含取消，等待1.433084秒、取消0.009256秒；只剩原完整四秒余量，未独享四秒 |

整轮 4.010071 秒，其中串行 CLI 等待 3.963015 秒。容器 3、卷 7。卷测量检查点为 `partial`、读到 0 字节、进入／返回均未观测；失败轮缺完整卷和目录占用。尚不能确定 daemon、VM 或卷扫描的实际耗时与根因。

此前 86 次完整检查无重试，最长 3.084200 秒，完整私有占用峰值 4261007360 字节；失败轮缺完整数据。

清理 `zero_owned_resources=true`、无失败，4 条归属检查通过，私有工作目录零残留。Monitor／Agent／Copilot 及前端构建、真实身份、采集／API、页面、停源及注销均未执行。600 秒构建分支未触发，无增量步骤摘要，尚未验收该诊断。

原始报告和清理审计见系统临时目录 `addp-p19-http-full-stream-window-20261104-65cs6cc3/`。

<a id="http-resource-wait-poll-deterministic-20261104"></a>

### 2026-11-04 资源检查等待返回汇总：确定性门禁通过，真实采集待授权

固定基线 `cc4c7de44e1503091dd36ebc2eb7c2ea1196398c` 叠加本地实现。仅对原采样等待记录 `wait_poll` 四项汇总，≤512 字节，扣除本次实际 timeout，排除取消等待。原四秒资源和清理边界继续生效。

旧实现 5 项反例错误。最终 Trace runner 183 项、HTTP runner 44 项及 JS、登记、身份和离线 TLS 子门禁通过，覆盖提前返回、异常、剩余时间、脱敏及失败后清理。4.2 秒反例仍失败并回收进程组。

固定 `8b320fb8e` 历史窗口的四秒来自串行 CLI 等待累积。Docker 日志缺少可信跨时钟对齐和请求归属，未观测到检查点不足以判断 daemon 行为。历史报告无 `wait_poll`，当前指标只能表达主机调用的观察值。真实等待、物理根因、繁忙稳定性和正式页面仍未验收。源和门禁证据在 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-p19-wait-poll-20261104-kzwedobr/`，历史关联在 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-p19-resource-wait-readonly-20261104-ftrmi7kn/assessment.json`。

<a id="http-browser-logout-contract-20261104"></a>

### 2026-11-04 正式HTTP浏览器两阶段注销：确定性补齐，真实窗口待确认

固定基线 `37f9724376ca7ac5f72dc5461da610259595f016` 叠加本地门禁。存活和停源两个浏览器阶段均要求 Console 正式注销 204，并以该次旧凭证单次请求 AuthContext 得 401。关页后实际增量恰为 2，仍计入原浏览器 80／控制面 200。预算不足、缺凭证、其他状态或传输失败均拒绝。正式 full 的专项输入为 13 项，连同三份集成标记共 16 项。具体契约见[正式 Python 后端与接口质量页面的本机隔离验收](../../addp平台运行观测规范.md#正式-python-后端与接口质量页面的本机隔离验收)。

旧回执验证 8 个反例失败，缺 Node 实现的反证为 6 失败、1 接线通过。最终 HTTP Python 46、Node 7、Trace 183 及身份、登记、离线 TLS 子门禁通过。受控 Page／fetch 证明请求绑定、脱敏、双阶段计数和失败语义，真实浏览器会话撤销未验。

完整产品构建、正式密码／MFA、采集／API、真实页面与清理未运行。历史 Linux SDK 通过独立保留。原始快照、反证及收据在 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-p19-browser-logout-20261104-hz6vb_p7/`。

<a id="http-full-logout-agent-volume-read-failed-20261105"></a>

### 2026-11-05 正式 HTTP full 在 Agent 构建期间遇到卷读取错误

本机 ARM64 标准 full 单次失败，固定源码 `e30f04c2dc3764d39ff7d899ae748de6db519050`，16 项入口输入匹配。含准备 708.839 秒，退出 2。Go 编译和私有缓存清理完成，System／Gateway／Monitor 镜像命令结束。Agent 构建阶段的首因是 `measure_owned_volumes` 返回 `owned_resource_measurement_failed`。完整检查仅 0.261156 秒，卷子步骤 0.163843 秒，`du` 诊断符合路径消失分类。报告未保留缺失路径或写入方，删除者仍未知。与早先 Gateway 四秒累积超时、独立 Linux T2 的路径归属分别认定。

前 176 次完整检查最长 1.955384 秒，读取重试 0，已完成样本私有占用峰值约 3.76 GiB。失败样本缺完整卷及目录读数。标准退出和独立归属复核确认容器、网络、卷、构建器及镜像标签零残留，私有工作目录已移除。

Copilot 与后续构建、正式密码／MFA、采集／API、真实页面、停源、两阶段浏览器注销及旧会话 401 均未执行。Hosted 与容量未运行。原确定性和 Linux SDK 通过各按其范围认定。Run `addp-http-local-35fa4d468ba04931861d6947cbdc1775`，预算原件、输入及清理收据在 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-p19-http-full-logout-20261105.9aKCoSAd`。

<a id="http-full-system-list-inspect-deadline-20261105"></a>

### 2026-11-05 正式 HTTP full 在 System 镜像构建期间耗尽四秒检查预算

本机 ARM64 单次执行 `make test-monitor-http-local MONITOR_HTTP_LOCAL_CASE=full`，固定源码 `6018887ac4426821941be0003c672ea8b1955d77` 包含卷归属与取消前取证提交 `9b64c5367`。13 项专项输入和 3 项集成输入与报告摘要一致，Docker Unix socket、固定摘要 Nginx 缓存及代理准入通过。Go 编译和当次缓存回收完成，System 镜像构建期间遇到首失败。含准备及清理共 256.178 秒，入口退出 2。

首因是 `resource_sample_deadline_exceeded`，落在 `inspect_owned_containers`。同一检查先用 3.889191 秒完成容器列表，其中 Docker CLI 等待 3.880109 秒，71 次等待超时返回累计比其请求等待时长多约 284 毫秒。随后 inspect 等待 0.103160 秒并取消回收 0.035402 秒。含取消的失败检查耗时 4.038941 秒。该计量来自宿主等待，daemon 内部耗时和根因未定位。失败样本在卷列表和测量前中止，卷路径消失未复现，卷归属无本轮实测回执。

取消前冻结回执与当次 run、System 构建阶段及命令 337 匹配。单调时间核对为构建已启动、失败检查开始和结束、冻结。冻结时该命令已运行约 26.490 秒，已扫描 2355 字节、观察 5 步，摘要为 `invalid`，未完成的具体 BuildKit 步骤为 `unknown`。首因仍为资源检查超时。

57 次完整检查最长 2.237384 秒、最大开始间隔 4.006694 秒，峰值 2.25 CPU、3288334336 字节内存及 4215373824 字节私有可写（约 3.93 GiB），读取重试 0。失败样本缺完整占用数据。标准清理和独立只读复核均确认当次容器、网络、卷、构建器容器及状态卷、镜像标签和私有工作目录零残留。

System 镜像及后续 Agent／Copilot 构建、正式密码／MFA、采集／API、页面、停源、两阶段注销及旧会话 401 均无本轮通过证据。未续窗、重试、运行 Hosted、容量或定时。Run `addp-http-local-29ed3ed8637d409e884211e4418f57c6`，报告 SHA256 `d9e077b448d4d445c4417dbe05d1b24ac9797724b77d12776aa544f3bb977667`。原始报告、输入、首因关联与清理复核在 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-p19-http-full-volume-attribution-20261105.GxTJctoW/`。

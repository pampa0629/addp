# Monitor HTTP 质量与采样历史证据

[返回 Monitor 历史证据](Monitor历史证据.md) · [返回验收证据索引](../验收证据索引.md)

本专题收录公共 mTLS、Go/Python 来源、启动诊断及采样／清理交接。固定源码、环境、失败记录、修复结论与未覆盖范围以各记录为准；本次仅整理文档，没有重跑验收或刷新 CI 状态，不推导当前 HEAD 或生产 T5 已通过。跨主题语境与原有顺序见 [Monitor 历史证据入口](Monitor历史证据.md#原记录入口)。

## 平台 HTTP 接口质量：本地一期

2026-10-20，Common 统一 Router 采集基础已在 `39b132c9926f9f404287ab5aeaa8c9d94eebd9f4` 提交；Monitor 查询、页面、权限迁移 227、HTTP 规则和通知产品代码已纳入 `3ee46b77fd84fe1a915874a11e0e97b843cac8bd`。以下分别记录本地分项，不表示共享工作区整个 HEAD 已重跑。

- T1：基础阶段的固定源码 `make test-go` 全部通过且无跳过；产品阶段 `make test-module MODULE=monitor GO_ONLY=1`、System 对应 T1、`make test-authorization` 及双语 Swagger 覆盖通过。后续没有将基础阶段的全 Go 结果扩大到整个最新 HEAD。
- T2：`make test-monitor-postgres` 通过，覆盖 HTTP CRUD、并发 CAS、冻结恢复证据、缺样保护、查询期间策略变化及完成租约过期；最后一次运行输入无漂移。`make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package migration --test platform-alert-management"` 通过，覆盖五项权限仅授现有平台系统管理员、刷新令牌撤销和幂等重跑。
- T2：完整 `make test-monitor-metrics` 在仓库外固定源码（基线 `688ecb005843ecadf7240a0e5249517de3a8381d` 叠加本专项文件）通过，含既有主机、进程、PostgreSQL 来源及资源告警回归。此后历史趋势补充与最终 HTTP 文件以 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=http-quality` 独立复验通过：真实 SDK 请求、13 项原生窗口场景、有权历史趋势、HTTP 503 窗口打开事件、签名通知首次 503 后重试、成功请求恢复同一事件、停止来源清空数值；HTTP 生命周期约 195.54 秒。两份快照输入均无漂移，退出分别核验当次容器、网络、卷和临时文件零残留。专项跳过 PostgreSQL 专业来源探针与后续主机回归，不能替代完整入口。
- T3：Monitor 78 项单元、226 项浏览器及构建通过，包含持续慢接口、筛选趋势、401/403/404 清空、迟到响应隔离和规则 CAS；Console 186 项单元、191 项浏览器及构建通过；Common 前端 241 项单元、System 前端 149 项单元及 189 项浏览器和构建通过。最后的 HTTP/权限相关文件与已验证输入相符，不扩展为其它会话后续修改的结果。

前次历史趋势试验在过去完整窗口形成前断言失败，未计为通过；最终仅等待自然历史形成，查询错误、采集状态或数据形状异常立即失败，未放宽有效失败点条件。配置选择矩阵验证 HTTP 专项不调用 PG 专业探针，默认及 postgres-source 保留原探针；39 项配置测试通过。

按用户决定，本轮不扩展或运行 Hosted HTTP T4，不借现有主机资源场景身份操作本专项。System 当前 User／后台最小投影在 T2 使用受控夹具，不能冒充真实密码/MFA 身份；个人部署未重启。Python HTTP 采集、Trace／日志链路关联及调用拓扑尚未实现，生产容量与 T5 未验；Tenant 发布服务质量仍由 Service 按对象权限提供。


## 2026-10-26 Python 全量 HTTP 质量首批与公共 mTLS 前置失败

用户确认 Common Python 统一 HTTP 观测与受保护来源、首接 Agent/Copilot 单进程 backend。实现提交 `0eca81f9b41c26b4f40375fa661067def49d56f2`：唯一 ASGI 生命周期同时服务全量 HTTP 指标与可选 Trace，删除原独立 Trace 中间件路径；指标不依赖 Trace 开关／采样。复用既有进程声明、开发／产品部署及 Platform 权限，仅发布身份、启动时间和 HTTP 指标，不发布 CPU／RSS／cgroup。每源最多 512 组详细标签、4 MiB 输出，超限整源失败、不截断；私有 Registry、串行 mTLS 来源、完整头读取及关闭边界沿同一实现验证。不新增 Tenant 字段、业务端口指标路由、权限或数据库。

最小标准验证已执行：Common Python 全量 421 项通过、54 subtests 通过，1 项可选 GeoPandas 跳过；`make test-agent-eval ADDP_AGENT_PYTHON=/tmp/addp-p19-http-metrics-gates/agent ADDP_AGENT_EVAL_REPORT=/tmp/addp-p19-http-metrics-agent-verified.json` 通过；`make test-copilot COPILOT_PYTHON=/tmp/addp-p19-http-metrics-gates/copilot` 185 项通过。Common Python 使用仓库既有标准入口与外部依赖覆盖，不修改个人模块 venv；收尾仅移除未使用 import／多余空行后，HTTP 指标及共享 Trace 定向 23 项、21 subtests 再次通过。

`make test-module MODULE=common GO_ONLY=1`、`make test-module MODULE=monitor GO_ONLY=1`、`make test-infra-runtime-log-lifecycle` 均退出 0；后者含 123 项本地 runner 前置及 64 项生命周期／预算测试。`python3 -m unittest scripts/test/metrics-deployment-config_test.py scripts/test/metrics-http-budget_test.py` 90 项通过；Python CI 登记 10 项及 11 owners、T2 登记 22／12／9／2 均通过。Agent／Copilot Swagger 路由覆盖 14／12 一致，公开 API 契约未变。`make test-changed` 已尝试但在 T2 环境前置拒绝（缺少 PostgreSQL／其他已选共享 owner 的连接输入），退出 2，不计全量聚合通过；不因此启动共享数据库或修复无关 owner。相关后续仍由既有 Platform Python／Agent／Copilot 与 Release Linux Metrics／Trace T2 门禁覆盖，本轮未查询、触发或认定其远程通过。

唯一获准的功能窗口使用 `ADDP_HTTP_BUDGET_REPORT=/tmp/addp-p19-python-http-quality-20261026/functional.json make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=http-quality`，2026-10-26 14:18:31.754—14:20:46.220（Asia/Shanghai），134.467 秒，退出 2。先通过八类固定 promtool 窗口语义断言，随后公共前置 `real mTLS scrape and controlled job label` 在原 65 秒自然采样等待中失败：夹具样本为零、发现一个活动目标且 health=down／error_kind=tls；没有观测到 HTTP SD 失败计数。Go 业务源及 Python 实链阶段均未进入，Python 容器未创建，业务请求为零，不能计作 Python T2、告警实链或全套 Metrics 通过。

本窗口采集 32 次资源检查，最大 0.260 秒，均在原完整 4 秒口径内；容器硬配额峰值 2.25 CPU／2.75 GiB，私有可写预算上界峰值约 1.44 GiB（含 tmpfs 配额预留，不能称为实占磁盘），未触发资源守卫。342 项输入摘要运行期间一致。标准退出清理及独立按当次 project 精确过滤的只读复核均确认容器／网络／卷为零、私有工作目录已删除。没有自动重试／续窗、运行 Hosted、重跑容量、设置定时或重启个人服务。共享 ASGI 调整后的真实 Trace T2 未运行，与旧混合链通过证据分开保留。

证据目录 `/tmp/addp-p19-python-http-quality-20261026/`：`functional.json` SHA256 `e8772a2dd64d4d5053f6dd113395a66126c0467260d2536916510d6a4514d0f3`；`functional.log` SHA256 `81228778f9e8eb36b658ccce3f010e65ef7544ec5291b2359e10c6ef8cd76694`；`cleanup-audit.json` SHA256 `ba6b2e4daefac07de6eede702a8d792a9446923856502d7425c75d0614a35309`。同目录 `audit-summary.json` 保存分项日志摘要及资源／清理汇总。门禁早期的导入、夹具和登记失败日志仍保留，不能被最终确定性通过覆盖。

## 公共采样失败的离线关联

用户要求先离线调查、讨论最小修正及验收后再运行。核对失败窗口的输入 SHA256 与当前源，gate／probe／Compose、Prometheus 模板、配置生成器及中心服务定义六项完全一致。控制面发现已有活动目标；宿主机以 collector 身份通过 `https://localhost:<当次映射>/metrics`，中心却以当前容器 IP 采样失败。Prometheus 06:19:14 UTC 的匿名、外来 CA 与明文握手日志对应既有负向探针，不能用它们认定实际采样为 unknown authority。

现有启动链先用不含当次容器 IP 的证书启动 Nginx，inspect IP 后重签来源证书并执行 `nginx -s reload`；当前就绪只看信号命令成功，没有核对实际呈送的证书已切换且覆盖该 IP。宿主机正向探针验证 localhost，初始／最终证书均含 localhost，不能辨认是否仍在呈送初始证书。因此“换证加载／IP SAN 校验差异”是优先核查假设，尚不是确认根因；也不能排除链校验、有效期或客户端证书等其他 TLS 子类。

已确认的诊断缺口是 `fixture_scrape_diagnostic` 将所有 certificate／TLS 错误折叠为单一 tls；当次原始 lastError、实际呈送叶子证书及请求日志均未保留，清理后无法唯一还原。下一步待讨论：保持严格 TLS 与原资源／时间边界，在原标准入口补充封闭子类、预期／实际公开证书摘要及名称／链／有效期匹配结果、换证后真实 IP 就绪断言；任何未知仍明确未知。不得通过跳过校验、改 server_name、扩大信任或延长原采样等待绕过失败。本文仅记录调查和待确认方案，尚未实施或运行该修正。


## 共享 Python 依赖与 Agent 冷安装冲突修复（2026-10-26）

用户现场 Agent 同步失败后，确认本批 `0eca81f9b` 新增的 `cryptography==49.0.0` 与现有固定 DeerFlow Harness 2.1.0 的 `cryptography>=50.0.0` 互斥。此前使用已有依赖覆盖的 T1 通过没有覆盖完整冷安装，不能用于否认这项集成回归；原 pip 升级提示不构成根因。修复 `db5289c7474a91966ab254efc0d61957c17d7479` 已推 main，统一固定 `50.0.2`，同步技术栈及 uv.lock；锁文件仅该包版本变化。未删除消费者、变更其版本约束或新增跳过依赖解析路径。PyPI 发布元数据及实际下载确认 macOS arm64、Linux arm64 的 wheel 可用；Linux amd64 发布文件也存在，实际构建仍由自然 CI 单列验证。

仓库外全新 Python 3.12 Agent/Copilot 环境分别按完整 requirements 安装，包含正式固定提交的 DeerFlow 两个 package 和共享模块，均退出 0、`pip check` 通过；安装报告实际为 cryptography 50.0.2／deerflow-harness 2.1.0。Agent 新环境 app／OpenAPI／Skills 导入通过，`make test-agent-eval` 通过 7 项场景契约、Agent 评测／持久化及 Common 聚合。`make test-copilot` 185 项通过，3 项已有警告。独立 Python 3.11 使用 `uv sync --locked --extra dev --extra inference-langchain` 后，`make test-common-python` 421 项及 54 subtests 通过、1 项可选 GeoPandas 跳过；`uv lock --check` 通过。`make test-python-ci-registration` 沿现有入口通过，依赖改动已由 Common Python／Agent／Copilot／共享消费者和产品镜像自动发现，无需新增 workflow 或测试路线。

`make build-images IMAGE_BUILD_ARGS="--services agent-backend --verify --jobs 1 --tag p19-crypto-fix-20261026"` 实际退出 0，Linux arm64 产品镜像完成第三方完整依赖解析安装、最终 `pip check` 和 app／OpenAPI／Skills 导入；镜像 `localhost:5001/addp-agent-backend:p19-crypto-fix-20261026`，ID `sha256:9333059e7c4f4eab53471ffe785a97c3850bc5b8fb038c0f632951fd46466747`。验证模式未推送产品镜像，未启动业务服务或挂载个人环境；该本地工作区构建的五项依赖／Dockerfile 摘要与验证开始时一致，不冒充固定 GitHub SHA 的 Linux amd64 构建。`make test-changed` 已尝试，但缺少 Monitor PostgreSQL T2 显式连接输入而前置退出 2，不计全量通过，未因此启动数据库。

原始证据 `/tmp/addp-p19-crypto-fix-20261026-9oi78l3t/`：Agent/Copilot 完整安装 JSON 与日志、Common 锁安装／测试、Agent 评测 JSON、Copilot 测试、镜像日志／身份、分发元数据与输入／产物 SHA256。自然 Platform CI `40294294052` 绑定修复 SHA，回查时仍进行中，未认定整轮通过；未取消／重跑 Actions、触发 Hosted、重跑容量或设置定时。没有操作个人 venv、运行服务、权限或数据卷；用户可自行沿 `./scripts/dev/restart.sh -agent` 重新同步。本修复不代替公共 mTLS 前置的待讨论方案，也不使 Python HTTP 实链或真实 Trace T2 自动通过。


## 修复 SHA 的 Linux 复核与 HTTP 守卫取证准备（2026-10-26）

用户完成个人重启后继续专项。本会话仅只读核对已有自然 CI，不运行／重跑本地隔离环境、Hosted、容量或定时。固定 `db5289c7474a91966ab254efc0d61957c17d7479` 的 [Agent offline evaluation](https://github.com/pampa0629/addp/actions/runs/40294294052/job/121476467369)、[Common Python](https://github.com/pampa0629/addp/actions/runs/40294294052/job/121476467434)、[Copilot](https://github.com/pampa0629/addp/actions/runs/40294294052/job/121476467306) 原始日志均确认真实 checkout、selected=true 和 status=success；Agent 完整冷安装使用 Linux x86_64 的 cryptography 50.0.2 wheel，七场景及评测／持久化、Common 聚合通过；Common 421 项及 54 subtests 通过、1 项可选跳过；Copilot 185 项通过、3 项已有警告。至此依赖冲突修复具备该固定源码的 Linux 安装及标准测试证据，不只依赖个人重启或本地 venv。

同一源码的 [Monitor HTTP trace infrastructure](https://github.com/pampa0629/addp/actions/runs/40294293942/job/121476731711) 原始日志中 `TestOwnedTracePipeline` 实际执行 328.23 秒并通过：真实 120 秒尾采样、失败／慢请求及普通 5/200 保留；policy v2 短阶段独立 root、持久 Link 与目标撤权；Go→Python→Go、共享 Alloy→Loki、Tempo 投影、历史参与者、当前 User 撤权和采集器故障／恢复。328 秒是本次测试实际耗时，不是容量证明；直接枚举的 OwnedTraceProcess 是父测试使用的受控子进程入口，其顶层 SKIP 不替代父测试断言。退出回执确认当次容器／网络／卷及临时文件为零。当前追踪相关产品／夹具源与该 SHA 无差异，原“共享 ASGI 调整后真实 Trace T2 未运行”的缺口已由这份自然 Linux T2 证据补齐，不外推 Hosted／正式身份或长留存。

[同轮 HTTP quality](https://github.com/pampa0629/addp/actions/runs/40294293942/job/121476731996) 失败，与此前本地 mTLS 失败分开：日志在固定窗口的 Filesystem 测试刚开始时出现 `No such container` 与 Terminated，尚未进入 initial scrape 的 valid_sample 断言或 Go／Python 业务源。不能将它说成 Linux 复现了本地 TLS 根因。该 Job 只打印预算报告路径，既有 artifact 列表没有预算报告，故无法唯一确认是 inspect／卷读取时容器消失、资源超限还是其他守卫条件；清理日志显示执行了 down／移除，但缺少预算 JSON 的独立回执，不扩写为完整清零验收。

源码已确认两个编排风险：固定窗口 `runPromtoolCases` 使用同项目 `compose run --rm` 临时 Prometheus 容器，守卫 ps→inspect／exec 之间该容器可被自动删除；其继承 2 GiB 配额，与基础栈 2.75 GiB 合计为 4.75 GiB，不能通过忽略临时容器来满足 4 GiB。上述风险与缺失的当次失败详情分开陈述，不由静态可能性反推唯一历史原因。较早其他 SHA 的 Release workflow success 中 Metrics 个案可为 skipped，未将其算作本专项通过。

取证修正本地提交 `e65c8d7b1fde4e529238519b80a22eb8b1cc4681`：固定枚举／inspect／卷／私有目录失败步骤、超时／容器消失／命令失败子类与数值限额；抑制原始命令和 Docker 错误正文，stdout 输出封闭摘要；原 Linux HTTP case 的同一 Job 无论通过或失败均保存预算报告七天，缺失报告为失败。不重试、忽略容器、提高限额或延长完整四秒。`python3 -m unittest scripts/test/metrics-http-budget_test.py` 8 项通过，含守卫失败后清理也保持失败、报告与日志可见且不泄漏注入敏感内容；`make test-infra-runtime-log-lifecycle` 123 项本地 runner／既有 System Go fixture、69 项生命周期聚合退出 0。T2 登记 22／12／9／2、workflow YAML 与归档路径契约、`git diff --check` 通过。新增用例沿原标准聚合自动发现；本会话暂不推送这批取证修正，避免在用户确认运行方案前间接启动新 T2。

进一步只读查到固定 Nginx 1.28 支持跨 reload 继承 SSL 对象；[官方规范](https://nginx.org/en/docs/ngx_core_module.html#ssl_object_cache_inheritable)说明文件修改时间和文件标识未变时可继承。旧夹具覆盖原证书文件后只确认 reload 信号成功，未核对实际呈送证书；这加强了换证生效判定的调查价值，但仍缺少当次 inode／mtime／叶子摘要，不能认定该缓存就是原本地失败根因。下一步待确认方案仍需保留严格 TLS、原 4 秒／资源边界，先收敛 promtool 额外容器和证书加载判定，再决定仅公共前置的新窗口；不得自动进入 Python 业务实链。

原始复核、摘要和本地门禁日志位于 `/tmp/addp-p19-http-budget-diagnostics-20261026-zd251axo/`，`audit-summary.json` 区分固定源码通过、本次 HTTP 失败、未知触发原因与尚未运行范围，`artifact-sha256.json` 保存文件摘要。没有改个人 venv、服务、权限或数据库；用户重启结果是现场反馈，不冒充本会话正式身份运行验收。


## 公共 mTLS 单次前置窗口与容器内换证修正（2026-10-26）

用户确认先修 promtool 复用、实际 IP 换证生效与 TLS 分型，通过确定性门禁后只运行一次 20 分钟公共前置；4 CPU／4 GiB、私有可写 2 GiB、完整四秒、原 15 秒采样／65 秒等待保持，通过或失败均清理停止，不进入 Python 实链。实现 `603ba1ed766b5584a834194eba66864f4e6f1344` 新增原标准入口的 `--case=mtls-preflight`；promtool 使用既有 Prometheus 及专用只读规则目录，删除额外 run --rm；新前置在公共采样后退出并要求零业务计数及 TLS 证明。两次新增握手分别验证实际呈送摘要和 IP，每次 TCP／TLS 共享五秒，不重试；不改产品 API／权限或启动个人服务。

窗口前配置／预算 100 项（配置 92、预算 8）通过，补充前置成功仍须 TLS 证明及准确零计数后，预算最新 9 项通过；Monitor Go 标准模块门禁通过，包含真实 fixture 调用的 fake Docker T1，验证只 exec 既有中心、输入可读与退出删除，不冒充 promtool 真实公式执行。生命周期标准入口 123 项 runner／既有 Go fixture、69 项聚合通过；T2 登记 22／12／9／2、shell 语法及 diff 检查通过。首轮确定性检查有一项旧分支正则断言未包含新 case 而失败，已同步并取得后续上述通过，不将旧失败算通过。

唯一实跑：`ADDP_HTTP_BUDGET_REPORT=/tmp/addp-p19-mtls-preflight-20261026-hbhwfxwq/functional.json make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=mtls-preflight`，2026-10-26 18:00:38.942—18:00:54.356（Asia/Shanghai），15.414 秒，Make 退出 2／子入口退出 1，判失败。宿主原子替换证书的程序已经完成 stat／读取断言，随后来源容器的 `nginx -t` 明确报告 `/source/server.crt` 不存在，配置检查失败；尚未 reload、启动中心、执行固定窗口公式、新增 TLS 连接或自然采样。此结论只确认本轮跨宿主／VM 挂载路径的可见性失败；没有保留当时宿主／VM 详细文件状态，不据此推断更早初始采样 TLS 失败的唯一根因或归罪为已证实的 Docker 缺陷。

本轮输入 343 项摘要无漂移，两次资源检查最长 0.171 秒，峰值硬配额 1 CPU／640 MiB，私有可写预算上界 285495296 字节，未触发资源守卫。只有 source／node／当次 PostgreSQL 等准备资源，未启动 Python 或 Go 业务源，业务计数回执尚未形成，不能借退出清理成功计为前置通过。标准清理及独立按当次 project 精确过滤的只读复核确认容器／网络／卷为 0、私有工作目录删除。没有重试、续窗、Hosted、容量或定时。

失败后仅完成同范围的代码与确定性修正 `bd2ab54ef0662b09f64aa9bc7b4f59424f749100`：Nginx 唯一活动证书路径改为来源容器私有 tmpfs；通过 stdin 单次传递最终公开叶子，在 VM 内原子替换并记录文件标识／mtime 比较，私钥及信任根沿原输入不变；宿主文件仅刷新为下次启动引导输入，避免来源 stop/start 回退旧证书。删除在线宿主文件替换路线，不关闭 TLS、不扩大信任或增加容器／配额。发布回执即使后续配置失败也沿现有预算报告保留。T0 使用独立临时目录执行实际 stdin／cat／mv 生命周期，GNU stat 由受控模型模拟；不冒充 VM 文件系统实跑。最终配置／预算 101 项、生命周期 123 项 runner／既有 Go fixture及 70 项聚合、T2 登记和 shell／diff 均通过；修正后的 VM 换证与公共 mTLS 尚未运行，原单次窗口已消费，必须另行确认后才能再开。

原始报告、日志、独立清理回执及摘要：`/tmp/addp-p19-mtls-preflight-20261026-hbhwfxwq/`；`follow-up/` 单列失败后的源码补丁和确定性验证，`audit-summary.json` 分开标注失败源码、后续修正和未验收范围，`artifact-sha256.json` 保留原始文件摘要。此前 Go→Python→Go／Link／日志的固定 SHA Linux Trace T2 证据继续独立有效；本轮不扩写为 Python HTTP 查询／告警实链或生产部署通过。本会话未推送本批，未手工触发 CI／Hosted、重启个人服务或更改个人数据与凭据。

## 容器内换证修正后的公共 mTLS 前置通过（2026-10-26）

用户另行批准原上限的新 20 分钟窗口后，仅运行一次 `ADDP_HTTP_BUDGET_REPORT=/tmp/addp-p19-mtls-preflight-recheck-20261026-co9zukrk/functional.json make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=mtls-preflight`。准备、采样和清理合计 64.570 秒，2026-10-26 20:20:59.130—20:22:03.701（Asia/Shanghai），标准入口退出 0。运行前 HEAD 为 `1c4ec8df446f427da372a296646967879c486d6a`，实际换证实现为 `bd2ab54ef0662b09f64aa9bc7b4f59424f749100`；343 项运行输入摘要前后不变，相关实现输入与此前配置／预算 101 项、生命周期及 Monitor Go 门禁所验证的修正一致，未重复开发或重跑这些确定性检查。

实际容器 tmpfs 发布回执确认文件标识已原子变化。localhost 叶子验证及来源实际 IP 的严格 TLS 握手分别用时 0.003417／0.002220 秒，实际叶子摘要均匹配当次最终证书 `fc77ec0214b3daf7dfd29521b20871e4b884971a00957a63b29cb46bf15c56ec`。八类固定窗口检查（CPU、进程、HTTP、磁盘、磁盘 IO、网络、文件系统及 inode）实际复用既有 Prometheus 容器内的固定 promtool 并通过；真实 mTLS 自然采样、受控 Job 标签、中心自采样及 RSS 通过。不是仅有 reload 成功或宿主证书文件可读。两次握手均未重试，未放宽证书校验、原 15 秒采样／65 秒等待或完整四秒资源检查。

13 次完整资源检查最长 1.007224 秒；峰值硬配额 2.25 CPU／2952790016 字节内存（2.75 GiB），私有可写预算上界 938397696 字节，均低于 4 CPU／4 GiB／2 GiB 停止线。入口在公共前置后按约定退出，业务请求回执为 Go 0、Python 0／0、合计 0；没有进入 Go／Python 业务来源或 HTTP 查询／告警阶段。标准清理与独立按当次 project 过滤的只读复核均确认容器／网络／卷为 0，私有工作目录删除。没有自动重试、续窗、Hosted、容量或定时，亦未重启个人开发服务。

原始 `functional.json`、`functional.log`、`source-state.json`、独立 `cleanup-audit.json`、`audit-summary.json` 和 `artifact-sha256.json` 位于 `/tmp/addp-p19-mtls-preflight-recheck-20261026-co9zukrk/`。预算报告 SHA256 为 `003bb738ff2ce2eb1111339a02c82c472135b4f9b0fb5384b38ff1c11973f816`，日志为 `90c5e30311dcc5b53e82d25681c31e4c7307ddd3b13ce67335a015d206e21ac3`。前一窗口的失败证据保持独立；本次通过只关闭公共前置的本地运行缺口。

另对已有 [Linux HTTP quality Job](https://github.com/pampa0629/addp/actions/runs/40310193163/job/121529457879)（源码 `85d0482ce3169d945dfd37d3ca25e08913620156`、attempt 1）原始日志做离线核对：实际 IP／叶子验证、八类固定窗口、公共采样、ProcessSources 及 HTTPRealSource 已通过；`TestIntegrationMetricsHTTPAlertLifecycle` 在 216.41 秒处因恢复同一事件超时 `unknown/no_data` 失败，不能将整项 HTTP T2 记为通过。该 Job 560.728 秒、清理／输入守卫通过，不能归因为本次 mTLS 或资源超限。日志 `/tmp/addp-ci-20261026-120139/job-121529457879.log` 显示原生来源约 10:53:10 UTC 启动，告警在 10:56:13 打开、10:56:14 通知后切换健康；现有夹具每秒至多一次、总计最多 180 请求，健康切换接近发流结束是优先调查线索。尚未取得逐请求时间与指标窗口证据，不能由时间相近认定唯一根因。下一步优先离线定位受控发流、完整成功窗口及查询判定的关系，保留同一事件恢复断言、原请求总量／超时／资源及权限边界；本轮没有追加业务实链或重跑 CI。

## HTTP 夹具恢复预算修正与确定性验证（2026-10-26）

用户确认分阶段预留后，先修运行观测规范 6.4 和 T0–T3 验收条款，再修改测试夹具；本轮只授权确定性门禁，真实实链另行确认窗口。离线事实：上述失败 Job 的来源 10:53:10 UTC 开始，查询／历史趋势完成后规则仍须自然建立自身窗口，直到 10:56:13 才打开事件、10:56:14 切换健康。旧 Go 从进程启动即按每秒一次、最多 180 次发流，Python 90／30 次来源的设计发流时长同样约 180 秒，没有恢复预留。产品固定窗口以边界计数差计算，停流后失败率为零分母，归一和聚合保留 no_data，连续窗口不得恢复活动事件。这确认夹具存在独立于业务语义的预算分配缺陷；旧运行缺少阶段计数，不能据此声称已经唯一解释该次 CI 超时。

实际发流循环现使用失败／恢复两份预算：Go 90／90，Python 两源 45／45、15／15；失败阶段启动间隔 2／4／12 秒，恢复阶段 1／2／6 秒。只观察到既有健康切换才启用恢复额度，阶段单向推进；晚切换、失败额度耗尽、控制文件清理均不能消耗或重置恢复预算，不补发时隙或因切换突发。每源在实际发送前记录尝试总数及封闭阶段回执，包括额度、各阶段计数和最后一次尝试的相对单调毫秒数。Go 新回执沿原私有目录预建可写文件，不新增容器、权限或监听。产品 SDK、查询公式、身份授权、规则阈值／时长、同一事件及通知断言不变。

删除仅在 Python finally 发布旧计数文件的路线。唯一汇总内核在来源停止后、私有目录删除前读取三个来源，早期 Go 失败也保存已发尝试和未进入 Python 的事实；已启动来源缺失回执、未知字段、损坏／超额／总量不符均失败关闭，不输出原始内容。完整 HTTP 成功还须三源各有失败和恢复尝试；公共前置仍严格零业务计数。原 300 次、5 QPS／4 并发、20 分钟、4 CPU／4 GiB／2 GiB、完整四秒及失败清理边界均保持，没有以请求停止或无数据作为恢复。

实际标准验证：`make test-module MODULE=common GO_ONLY=1`、`make test-module MODULE=monitor GO_ONLY=1` 退出 0；`python3 -m unittest scripts/test/metrics-deployment-config_test.py scripts/test/metrics-http-budget_test.py` 108 项通过，执行实际调度类、晚切换、两阶段耗尽／节奏、源计数校验、实际清理回执程序和完整入口成功拒绝；`make test-infra-runtime-log-lifecycle` 退出 0，123 项本地 runner／既有 System Go fixture及 75 项生命周期聚合通过；`scripts/ci/check-t2-ci-registration_test.py` 38 项及登记 22／12／9／2 通过，shell 语法和本专项 diff 检查通过。新增用例沿 Common／Monitor Go、既有配置／预算／生命周期和原 Metrics T2 自动发现，不新增工作流或标准入口。回归先于调度实现报缺少内核；迁移后另一项旧断言仍读取已删除计数文件，修正为唯一汇总入口后取得上述最终通过，保留初始失败记录。

Monitor T1 实际覆盖停流零分母的归一、零请求权重聚合及活动告警不得恢复，并有持续成功请求的恢复对照；不是把未知改成健康。原 HTTPWindow 中另加“停止发流为未定义”和“成功请求产生真实零失败率”两项 promtool 场景，已编译但本轮未执行，其结果仍待原 T2。历史 CI 的失败不改记通过，修正后的真实 Go／Python HTTP 采集→查询→告警／通知仍未运行；既有 mTLS 通过属于旧固定输入，本次改动后的清理／阶段回执只有确定性证据。

源码补丁、输入摘要和门禁日志位于 `/tmp/addp-p19-http-phase-fix-20261026-sejp16cy/`，`audit-summary.json` 及 `artifact-sha256.json` 区分已确认夹具缺陷、历史原因未唯一确定和未运行层级。本次未启动隔离环境、触发／重跑 CI、推送、Hosted、容量或定时，也未重启个人服务或操作业务数据；共享工作区的无关改动未纳入本专项。

## 恢复预算修正后的单次本地 HTTP 实链：准备阶段守卫失败（2026-10-26）

用户另行批准原上限完整实链窗口：准备及清理最多 20 分钟、当次容器合计 4 CPU／4 GiB、卷及私有可写 2 GiB 停止线、完整资源检查四秒、受控请求最多 300 次及 5 QPS／4 并发，失败即停并清理、不重试或续窗。仅运行一次 `ADDP_HTTP_BUDGET_REPORT=/tmp/addp-p19-http-full-20261026-u9f8dxrv/functional.json make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=http-quality`。启动 HEAD 为 `bdf8788266408209a1c3da6aab948a0c4321b248`，分阶段修正 `b7a3db50c8d4358966681c2fa485d9a2614e153c` 的相关实现输入无后续差异；本轮 343 项运行输入摘要未变化。

2026-10-26 21:58:51.178—21:59:57.776（Asia/Shanghai），66.598 秒失败，Make 退出 2、子入口被终止退出 143。前置 94 项配置确定性检查通过；实际来源 tmpfs 换证、localhost 叶子与实际 IP 严格 TLS 校验通过，两次握手 0.002179／0.001594 秒，最终叶子摘要 `800eb31537f747f571aeedddfce2918a2f561f8d6dbcf21045a71731315f5eb6` 匹配；匿名、外来 CA 和明文拒绝通过。随后在编译固定窗口测试二进制期间，守卫报告 `http_budget_sample_command_failed`，固定步骤 `private_directory_size`、类别 `command_failed`、命令退出码 1。守卫中止编排，不能将子入口 143 说成编译器自身诊断失败，也不能将配置单元测试内的模拟执行日志计为实际 promtool 场景执行。尚未进入真实固定窗口公式或 Go／Python 业务来源，业务计数 Go 0、Python 0／0、阶段均未启动，未运行恢复预算与告警闭环。

失败前 14 次完整资源检查最长 0.245005 秒；已取得样本的峰值硬配额为 2.25 CPU／2.75 GiB，私有可写预算上界 1406349312 字节。这些是失败前的成功读数，不证明失败读取的目录大小，也不能排除失败瞬间的变化；本次报告是目录命令非零退出，不是已确认的四秒超时或资源越线。标准清理和独立按当次 project 精确过滤的只读复核均确认容器／网络／卷为 0、私有工作目录删除；新的退出阶段汇总在来源未启动的早期失败下保留准确零计数及无阶段事实，不外推真实发流场景通过。没有复用剩余时间、另起环境、续窗、Hosted、容量、定时或个人服务操作。

离线只读核对：该步骤执行 `du -sk <当次私有目录>`；标准入口将 GOCACHE／GOTMPDIR／TMPDIR 都放在被扫描目录中，编译会并发创建／删除文件。“扫描时文件消失”仅为优先假设，原始 stderr 已由封闭守卫丢弃，当前报告未分类，不能排除权限、I/O 或其他错误，不能唯一归因。本轮没有改变／忽略失败、重试 du、缩小扫描范围或放宽预算。下一步建议先补充同一失败调用的有界封闭错误分类和失败耗时取证，保持非零即失败、完整四秒及全目录口径；在原因证据充分前不直接重开实链窗口。

原始日志、预算报告、源码绑定、独立清理及汇总位于 `/tmp/addp-p19-http-full-20261026-u9f8dxrv/`。`functional.json` SHA256=`ec289ce3c4f0ec42a78ac809e2592d51e778e9ff59b512d0f025cd812df22767`，`functional.log` SHA256=`f1290a1a52daf0ab49a77905881ff043d61a2c085b5e4af53214ca03bf40b206`，其余摘要见 `artifact-sha256.json`。本次只记录实际失败与清理；前一固定输入的公共 mTLS 通过、分阶段修正的 T0/T1 和已有独立 Trace 证据分别保留，完整 HTTP 查询／告警／通知仍未通过，不推送或手工触发／重跑 CI。

## 私有目录读取失败的封闭分类与耗时证据（2026-10-26）

用户继续后，仅推进上节建议的诊断代码与确定性门禁，没有新开运行窗口。先修订运行观测规范 6.4 和 T0–T3 条款，再补 `metrics-http-budget.py`：私有目录 `du` 单独固定 `LC_ALL=C`，原命令、扫描范围和非零失败语义不变。仅对同一次失败已捕获且不超过 4096 字节的 stderr 作封闭后缀分类，保留 `directory_error=path_disappeared/permission_denied/io_error/mixed/unclassified/empty/over_budget`、字节数及超预算标记；超预算不保留前缀，原文、路径、命令和环境不进日志或报告。这是 CLI 文本分类，不是系统 errno 或唯一根因证明；本次未改子进程输出捕获方式，不把解析上限宣称为输出捕获硬上限。

失败详情另保留同一单调时钟的 `command_seconds`（命令返回／抛出时的本身耗时）、`sample_seconds`（本次完整检查至此累计耗时）、`command_budget_seconds`（该命令启动时剩余预算）。不重置四秒、不新增读取或重试；既有 Docker 消失分类保持，超时／命令不可执行仍失败。失败后的清理成功也不能改判通过。旧 66.598 秒运行没有保存 stderr，此修正不能回填当时类别或确认扫描竞态，历史失败和完整 HTTP 实链缺口保持。

最小验证：`python3 -m unittest scripts/test/metrics-http-budget_test.py` 17 项通过；`make test-infra-runtime-log-lifecycle` 退出 0（123 项本地 runner／既有 System Go fixture，78 项聚合）；`python3 scripts/ci/check-t2-ci-registration_test.py` 38 项和原登记 22／12／9／2 通过，专项 diff 检查通过。新增回归先报缺字段／缺分类函数，再由当前实现通过；覆盖 4096 边界与 4097 拒绝、未知／非 UTF-8／混合、敏感正文不保留、仅目录命令 C 环境、两段不同耗时及原共享期限递减、超时／创建失败、单次命令不重试，以及报告／stdout 保留封闭事实且运行继续为失败。没有运行实际 Docker、du 探针、文件并发负载、业务或隔离环境；既有标准入口和 Metrics T2 输入登记已覆盖，无新增 workflow 或旁路。

诊断源码、输入摘要、红／绿及标准门禁日志保存在 `/tmp/addp-p19-directory-diagnostic-20261026-ap5xjngx/`，见 `audit-summary.json`、`artifact-sha256.json`。本次仅本地提交，不推送或触发／重跑 CI，不运行 Hosted、容量、定时或个人服务操作。下一步应在另行确认的单次准备阶段取证窗口取得真实目录失败分类，未得到该证据前不认定根因已修复，也不直接将完整 HTTP 实链记为通过。


## Linux Go HTTP 子链通过与 Python 来源启动诊断（2026-10-26）

只读核验已有自然 CI 原始日志：[bdf 的 HTTP Job](https://github.com/pampa0629/addp/actions/runs/40334346467/job/121612153803) 精确 checkout `bdf8788266408209a1c3da6aab948a0c4321b248`，Go `TestIntegrationMetricsHTTPAlertLifecycle` 实跑 152.28 秒通过，覆盖同一事件恢复及签名通知；后续真实进程／来源停止断言也通过。[206 的 HTTP Job](https://github.com/pampa0629/addp/actions/runs/40339327143/job/121630064969) 精确源码 `206261e751090a7bfb4e816dd0a8fbd97273d3d3`，相同 Go 子链实跑 150.27 秒通过。两轮均在 Python 发布身份前退出，finally 的非零退出异常再次覆盖首失败；日志回执分别 539.709／547.187 秒、Go 148／Python 0，输入未变且私有目录删除，但 cleanup_verified=false。不能继续将旧 Go unknown/no_data 当作这两轮的实际失败，也不能将 Go 子项通过或目录删除写成整项 HTTP／完整清理通过。

当前证据没有容器内安装日志或 Python 启动子阶段，未唯一确认包解析、导入、权限或 TLS 原因；没有改包版本、镜像或 UID／只读约束。先修规范，再将原来源脚本作为唯一 stdlib 启动入口，沿原参数复制／安装后再导入 SDK；删除 Compose 内单独 shell 安装路线。新增复制、依赖、导入、配置、指标监听、HTTP 就绪、身份及运行的固定阶段回执，失败只留枚举／退出码／字节数。仅解析最多 64 KiB 的既有临时安装日志，超预算标记而不保留前缀；不把该解析限额说成输出捕获硬限额。异常正文、依赖日志、路径和凭据不归档；原 120 秒来源启动等待、90／480 秒进程期限、10 秒退出检查、600 秒检查点校验上限、1200 秒整体预算、四秒资源检查及全部硬限额保持。

确定修复的控制流缺陷：Python finally 不再遇首个非零退出就跳过后续来源；两个 stop 文件、退出检查及发现范围恢复独立执行，错误另记，已有首失败保持原异常。进程内退出也在 join 出错后继续关闭来源，不能以清理错误覆盖请求首失败。回执沿原预算报告和原 CI Artifact 保存；即使请求阶段记录损坏，仍独立保留启动／清理事实。没有新增容错路线或把未知／不完整诊断改成业务通过。

`python3 -m unittest scripts/test/metrics-deployment-config_test.py scripts/test/metrics-http-budget_test.py` 116 项通过；`make test-infra-runtime-log-lifecycle` 123 项 runner／既有 System Go fixture及 78 项聚合通过；T2 登记测试 38 项和登记 22／12／9／2、shell／专项 diff 检查通过。首失败被旧 finally 覆盖的回归先失败，修正后通过；覆盖启动子阶段模拟、安装诊断边界／脱敏、超时和恢复失败时仍检查第二来源、关闭全部进程内资源及保留首失败、预算归档回执。此为 T0／确定性证据，未运行本地安装、容器、Python 业务实链、Hosted、容量或定时；真实 Python 启动根因仍待带诊断的自然 Linux CI。

原始日志和补丁／门禁／摘要保存在 `/tmp/addp-p19-python-startup-20261026-y1zoacsw/`。前一轮 66.598 秒本地目录失败和 `1ce3ddac7` 目录分类仅在各自已记范围有效，不是本次 Python 启动原因。本批只提交本会话 hunk，按既有 main 推送产生自然 CI；不手工触发、取消或重跑 Actions，不新开本地窗口。


## 启动诊断的 Linux 实跑与同进程导入缓存修正（2026-10-26）

固定 `ffc478e1a8ad0c099d6d21d32477552df32b9abe` 的自然 [HTTP quality Job](https://github.com/pampa0629/addp/actions/runs/40342629801/job/121641589201) 已结束失败。Go 告警子链再次实跑 150.34 秒通过。真实生产者回执首次确认两源均在 imports 阶段 module_missing，安装命令已返回成功；两个退出检查和发现范围恢复均执行，日志保留 Python 身份发布前的首异常，不再被 finally 的退出非零覆盖。日志前部配置 T0 的 prepare 模拟回执不是实际生产者结果，本结论取后部真实回执。

官方 Artifact `12661186120`，19852 字节，下载 SHA256 核对为 `8af2b83c27dbb734ccbb89b68a987860cd6286cf96e878f0062392659467d9db`。预算报告 573.390 秒、整体 failed、输入未变；另有 inspect 非零读取失败（命令 1.022295 秒、检查累计 1.038813 秒、启动剩余 3.983482 秒），随后子入口 -15、cleanup_verified=false、private_directory_removed=false，最终预算未保存 Python 回执。真实启动事实仍在原 Job 日志，但不能称完整清理或 Python 实链通过。原始 ZIP、预算 JSON、日志及 linux-failure-audit.json 位于 `/tmp/addp-p19-python-startup-20261026-y1zoacsw/`。

确定性重现本批新入口的一个缺陷：解释器启动时 /tmp/deps 尚不存在，PathFinder 可缓存该路径为 None；同进程完成安装后直接导入仍不可见。回归实际建立路径负缓存、模拟安装创建模块，再运行真正 bootstrap，修正前返回 1、刷新导入缓存后返回 0；没有运行 pip。现在安装结束后调用 importlib.invalidate_caches()，不更改依赖版本／UID／权限。缺失组件仅保留固定白名单类别，未知为 unknown。此问题属于 ffc478e1a 引入的同进程路径，不反推更早 shell／exec 入口的唯一失败原因。

另一项确定性控制流修正：采样开始后可能进入清理，再遇资源读取失败；旧父进程会重复 SIGTERM 中断已经开始的强制清理。现在只在回执 project／私有目录与本次归属完全相符且 cleanup_started=true 时不再次发送终止，仍保留该资源错误、停止新采样、等待原清理，整次仍 failed；未进入清理仍沿原取消路线。报告保留 failure_during_cleanup 布尔值。四秒与原 120 秒清理期限不变，不忽略消失容器或将清理重叠错误改判通过。当次旧报告没有重叠标记，此回归证明控制流缺陷，不唯一还原该次 inspect 非零和清理中断的全部时序。

先修规范，两个回归先失败后通过；最终配置／预算 118 项、生命周期 123 项 runner／既有 System Go fixture及 79 项聚合、登记测试 38 项与 22／12／9／2、shell／专项 diff 均通过。补丁和门禁日志在上述目录 follow-up/；后续自然 CI 单独核验，不把这份确定性通过当作实际 Python 已启动。本轮未开本地窗口、安装实际依赖或运行 Python 业务实链、Hosted、容量、定时，也未取消或手工重跑 CI。


## Python ImportError 分层回执与清理 profile 修正（2026-10-26）

自然 [75c5c42b9 的 HTTP Job](https://github.com/pampa0629/addp/actions/runs/40347081176/job/121657200460) 仍失败，Go 告警子链实跑 153.45 秒通过；Python 失败从 module_missing 变为 imports/import_failed，仍在身份发布前，不能将导入缓存修正写成完整启动通过。577.072 秒的预算回执保留两源启动／退出事实、Go 147 次／Python 0、输入未变；failure_during_cleanup=true，子入口退出 1，私有目录已删除，证明没有再次 SIGTERM 打断这次清理。但 cleanup_verified=false，仍不能称零残留。Artifact 12663195763（20172 字节）下载 SHA256 已核对 d90f3b1552f8eb2fe5de071e04b89f8354cc2721909e14bd600e34756769162d；原始预算及日志在 `/tmp/addp-p19-python-startup-20261026-y1zoacsw/import-profile/`。

只读核对发现确定性清理遗漏：Python 来源通过 --profile python-http up 创建，原唯一 down 未启用该 profile，实际清理日志也未移除两个 Python 服务。[Docker 官方 profile 规则](https://docs.docker.com/compose/how-tos/profiles/)明确默认 down 不包含未激活 profile 的服务。现在唯一 down 同时启用 python-http，继续限当次 project，保留 --volumes／--remove-orphans，不启动任何新服务或更改资源／期限。shell 回归执行真实 cleanup 函数、替换 Docker 命令，原代码缺 profile 时失败，修正后验证同一 profile、原首失败仍非零及私有目录删除；不将受控回归当实际清零通过。

ImportError 尚缺具体组件与版本事实，本批没有猜测升级／降级。启动回执新增固定导入检查点、ImportError 组件白名单及实际安装的预声明发行包版本；未知模块为 unknown，版本仅短版本字符串或 unavailable，不留异常正文、任意模块路径或环境。失败发生在 FastAPI、Starlette、Uvicorn 还是 Common 导入时可以区分，异常所属组件与入口检查点分别解释，不从名称关联推断唯一根因。未运行本地依赖安装、容器、业务实链、Hosted、容量或定时。

最终配置／预算 120 项、生命周期 123 项 runner／既有 System Go fixture及 80 项聚合、登记测试 38 项与 22／12／9／2、shell／专项 diff 均通过；实际退出回归、敏感版本／模块拒绝及导入检查点保留均覆盖。标准入口及原 Metrics T2 自动发现／归档已覆盖，未增加旁路或 workflow。后续以新提交的自然 CI 单独核验，旧失败保留，Python 全链与真实清零仍待通过。


## Python 来源 FastAPI 导入定位与清理实跑核验（2026-10-27）

已核对源码 `e5391ca36843cd0d99233ea3f63949abb45d81d0` 的自然 [Linux HTTP Job](https://github.com/pampa0629/addp/actions/runs/40351996322/job/121674742810)。2026-10-26 23:43:58—23:53:23（Asia/Shanghai）的 HTTP 预算执行 565.507 秒，整体 failed／退出 1；Go 告警生命周期子链 160.70 秒通过，实际 Go 148 次（失败 90／健康 58），Python 两源均 0。两个来源在身份发布前的 imports 阶段、fastapi 检查点报 import_failed，异常组件 unknown；没有异常符号原文，不能将检查点当作唯一根因。安装完成后的发行包回执均为 FastAPI 0.115.0、Pydantic 2.14.1、Starlette 0.38.6、Uvicorn 0.32.0、HTTPX 0.28.1、Common 0.1.14、cryptography 50.0.2、OTel 三包 1.45.1、prometheus-client 0.26.0、protobuf 7.36.2；这些版本事实不等于已证明版本冲突，亦不代表两个正式消费者的运行栈。

本轮 error_code／failure_detail／failure_during_cleanup 均为 null，输入未变；两个来源都完成退出检查，发现范围恢复，唯一带 python-http profile 的 down 后 `cleanup_verified=true`、`private_directory_removed=true`。清理修正已有这次真实 Linux 零残留证据，整体仍因 Python 导入失败而失败；Go 子链通过不代替 Python SDK→mTLS→采集→Monitor 查询／告警的验收。官方 Artifact 12665281043 为 20228 字节，下载 SHA256 已核对 `0cf19a17c641506751c71d472e8868421609c794841d48d4a6822b293b4534dc`。原始日志、ZIP 提取预算和独立摘要位于 `/tmp/addp-p19-python-startup-20261026-y1zoacsw/import-profile/`（`job-121674742810.log`、`linux-budget-e5391ca36.json`、`linux-e539-audit.json`）。

离线核对另发现两项夹具事实。第一，两个测试源都硬编码 FastAPI 0.115.0／Uvicorn 0.32.0，未使用 Agent FastAPI 0.142.2、Copilot FastAPI 0.110.0 的各自正式 HTTP 依赖声明，不满足技术栈规范中分别验证消费者版本的要求。第二，唯一安装目标 /tmp/deps 位于未指定 exec 的 256 MiB tmpfs；[Docker Moby v28.5.1 实现](https://github.com/moby/moby/blob/v28.5.1/daemon/oci_linux.go#L487-L500)默认合并 noexec／nosuid／nodev，[Linux mmap 规则](https://man7.org/linux/man-pages/man2/mmap.2.html)拒绝从 noexec 文件系统建立可执行映射，而 Pydantic 依赖本地扩展。这证明配置与本地扩展加载要求相冲突；本轮未记录实际 mount 标志或扩展 ImportError，仍不能称已唯一还原本次异常，也不能猜测降级 Pydantic。

拟仅对当次两源现有 tmpfs 显式允许 exec、保留其余隔离与资源上限，增加实际挂载／扩展加载断言，并由各消费者正式 HTTP 声明生成各自测试依赖，删除第三套夹具版本；执行权限变化已提出确认，尚未修订规范或实施该方案。本轮没有新增本地环境、依赖安装、业务负载、Hosted、容量或定时，也未手工触发／重跑 CI。这里只同步只读证据，未将待讨论方案或旧失败记为通过。


## Python 测试来源本地扩展挂载及正式 HTTP 依赖修正（2026-10-27）

用户确认继续实施最小修正后，先修运行观测规范 6.4 与 T0–T3 条款，再调整原唯一 Python 来源入口。两个当次来源继承的唯一 /tmp tmpfs 显式为 exec／nosuid／nodev，仍为 256 MiB；非 root、只读根、cap_drop ALL、no-new-privileges、CPU／内存／私有可写／完整四秒和全部请求／清理预算均保持。安装前读取当次 /tmp 的实际 statvfs 标志，noexec、缺少 nosuid／nodev 或读取失败均禁止安装和业务启动；安装后必须成功导入 Pydantic Core 本地扩展且加载器为 ExtensionFileLoader，才进入 FastAPI。回执只增加实际挂载布尔值与 native_imported 布尔值、白名单组件／版本；失败不留路径、异常正文或任意模块名。这是已确认配置矛盾的修正，不反推旧 Job 未捕获的唯一异常符号。

删除共用 FastAPI 0.115.0／Uvicorn 0.32.0 版本和共用 requirements 文件。现分别从 Agent／Copilot 正式 requirements.txt 提取 FastAPI、Uvicorn（保留 extras）、HTTPX、Pydantic 四项完整声明，缺项或重复失败关闭，不改变消费者版本或绕过 pip 解析。各来源只读取自己的依赖文件，仍为 HTTP SDK 夹具，不冒充完整 Agent／Copilot 产品依赖和业务启动。两个原 requirements 文件同步纳入既有 ADDP_T2_INPUT_FILES；实际固定摘要收集及共享 CI 选择确认二者都命中 Monitor，不新增 Makefile 入口或 workflow。

新增四项回归先于实现失败，覆盖原 mount 缺 exec、未按实际挂载拒绝以及缺少依赖提取／本地扩展内核；随后补充实际标志读取、本地扩展失败不进入框架、独立来源文件及严格回执脱敏反例。首次完整配置／生命周期聚合发现旧回执测试缺少新增字段，更新唯一结构而非兼容旧结构后重跑。最终 `python3 -m unittest scripts/test/metrics-deployment-config_test.py scripts/test/metrics-http-budget_test.py` 127 项通过；`make test-infra-runtime-log-lifecycle` 的 123 项 runner／既有 System Go fixture及 80 项聚合通过；T2 登记测试 38 项、登记检查 22／12／9／2、shell 语法与本专项 diff 检查通过。T0 没有启动容器、安装实际依赖或导入产品依赖。

补丁、红／绿门禁日志及输入／选择摘要位于 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-p19-python-native-fix-20261027-040bs6so`。本轮没有新开本地实链窗口、运行 Hosted、容量或定时，也没有修改／重启个人服务。修正后的实际挂载、本地扩展与两源 SDK→mTLS→采集查询告警仍待自然 Linux Metrics CI 单列核验，不将本地确定性通过算作完整 HTTP 实链通过。


## 正式 HTTP 声明两源 Linux 业务子链通过，整门禁仍因清理重叠读取失败（2026-10-27）

已核对修正源码 `44230c5c8d733b737145735a2432b31cb4891c3d` 的自然 [Linux HTTP Job 122046852189](https://github.com/pampa0629/addp/actions/runs/40461400796/job/122046852189)。14:30:26.448—14:44:05.065（Asia/Shanghai），预算执行 818.616 秒，整体 failed，子入口退出 0；没有手工触发／重跑或另开本地窗口。正式版本提取和本地扩展修正已取得实际运行证据：两源 startup 均 running／passed，实际挂载 noexec=false／nosuid=true／nodev=true，native_imported=true。Agent 使用 FastAPI 0.142.2／Uvicorn 0.32.0／HTTPX 0.28.1，Copilot 使用 0.110.0／0.27.0／0.27.0；两源 Pydantic 2.14.1、Pydantic Core 2.50.1。原启动阻塞已跨过，不需猜测降级 Pydantic；此夹具只复用 HTTP 四项直接声明，不是完整产品依赖、正式身份或页面验收。

固定窗口公式 0.62 秒通过；Go 实际查询／趋势、告警打开／签名重试／同一事件恢复（150.38 秒）与来源停止隔离通过；Python 两源 SDK→mTLS→唯一发现与自然完整一分钟→Monitor 查询／历史趋势、告警打开／签名重试／同一事件恢复（164.07 秒）及来源停止隔离通过。两源都正常退出，发现范围恢复，failure_stage=null。实际 Go 148（失败90／健康58）、Python 75（45／30）和28（15／13），总计251，三源均有失败及恢复尝试且未超原额度。产品子链通过与整门禁通过分别陈述。

整门禁仍因资源采样失败：stage=inspect、kind=command_failed、退出1，命令1.075732秒、整次检查至此1.091369秒、命令开始剩余3.984363秒，并非四秒超时；failure_during_cleanup=true。最终 cleanup_verified=true、private_directory_removed=true、inputs_unchanged=true，零残留及原首失败均保留，不将资源失败忽略或改判通过。201个成功采样最长1.226349秒，样本峰值硬配额3.25 CPU／4026531840字节内存、私有可写预算上界1807519807字节；这些只代表已成功取得的样本，不推断失败读取瞬间或容量通过。

只读代码确认仍有生命周期竞态窗口：父进程读取 cleanup_started=false 后进入包含 inventory／inspect／du 的多步采样；子入口可同时标记 cleanup_started并开始down。现有协调只在读取失败后阻止再次SIGTERM，不能互斥已开始的采样与资源删除。本轮 failure_during_cleanup证实错误与清理重叠，但没有原始stderr及具体对象，不能唯一证明某容器消失就是本次命令退出1的原因。已提出当次采样与清理互斥、锁等待计入原四秒及真实错误继续失败的最小方案，待用户确认；没有擅自改变生命周期或重试输入。

官方Artifact 12715410863（21672字节），下载SHA256核对 `2fd7f54ef48bc6dc12be5c413cabc4b6097aa9e0d8b5e27118cd97b753eff427`。原始日志、ZIP、预算及独立摘要在 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-p19-python-native-fix-20261027-040bs6so`（job-122046852189.log、linux-budget-44230c5c8.json、audit-summary.json）；首次下载网络EOF，重新只读下载后才核对成功，不是重跑验收。这里只追加证据，`git diff --check`通过，代码的127项配置／预算、123＋80项生命周期和登记门禁沿前述固定提交仍有效；本轮未运行Hosted、容量或定时，未修改个人服务。完整标准HTTP门禁仍未通过，剩余优先项为采样／清理协调及其整门禁验证。


## HTTP 采样／清理原子交接实现与确定性验证（2026-10-27）

核对现行“进入清理后停止新采样”、完整四秒、原120秒清理与1200秒整轮判定后，本次按既有必要CI修复范围落实交接，不新增产品阶段、权限或运行范围。此前将私有协调实现再次列为产品边界确认过宽；本次没有从协调消息推定Hosted、容量、定时或本地窗口授权。旧44230c5c8的业务子链通过与整门禁失败仍保留，不回写为通过。

当次预算目录使用唯一文件锁：父进程在四秒开始计时后取锁，持锁重读相同project／私有目录与清理状态，才执行原完整采样；发布清理标记后等待中的采样终止交接，不发Docker读取，也不增加伪样本。清理先发布同一回执，再取得同一锁；Shell继承的FD9在Python步骤退出后仍保持锁，覆盖唯一down、归属零残留检查和私有目录删除，随原进程退出释放。已开始的采样和删除不再并行；真实inspect／du失败仍保留并释放锁，原强制清理继续，不重试读取、不忽略对象消失、不提供无锁备用删除路线。无法取得交接锁时明确清理未确认，不能假造零残留。

取锁与回执读取消耗同一四秒，原采样命令只使用剩余时间，解析／汇总后再核整轮检查期限；成功回执分列lock_wait_seconds。清理在入口记录唯一120秒单调截止，锁等待不重置截止；报告保留cleanup_lock_wait_seconds及cleanup_overdue，超期即使最终清零也不得判整轮通过。原1200秒、资源／请求上限、首失败及清理重叠时不重复SIGTERM均保持；不更改Docker daemon或个人服务。原先只有清理前一次标志检查的路线已替换，完整采样内核仍唯一。

确定性回归使用真实私有文件锁与受控线程，验证采样先开始时禁止删除、清理先发布时不启动读取、剩余Docker命令预算、四秒及清理锁截止、归属不符、真实命令失败后释放锁、解锁错误不覆盖首失败。实际Shell子进程验证Python退出后FD锁仍由Shell持有、原退出后释放；整轮成功负例验证清理逾期不因零残留改判。相同新回归对父提交临时副本失败、当前实现通过，未改写共享源码做回退。最终配置／预算135项、生命周期123项runner／既有System Go fixture及88项聚合、登记38项与22／12／9／2、Shell语法及专项diff通过。既有固定输入、Makefile和CI自动发现覆盖，未增加新入口／workflow或依赖。

原始门禁、红／绿对照、补丁与输入摘要在 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-p19-http-handoff-20261027-0slhhjwm`。本地没有新建容器或实际运行HTTP／Hosted／容量／定时；修后的真实整体结果待唯一自然Linux HTTP Job单列核验，确定性通过不计为完整门禁通过。


## HTTP 采样／清理交接修正后的 Linux 整门禁通过（2026-10-27）

用户确认保持原四秒及全部边界的互斥修正；已核对精确源码 `fa7ae6dc34af264fb14c31c48b048fc26514efb4` 的自然 [Linux HTTP Job 122070268771](https://github.com/pampa0629/addp/actions/runs/40468624807/job/122070268771)，Job及实际Verify isolated metrics lifecycle步骤均success。预算15:42:29.017—15:56:25.320（Asia/Shanghai）共836.303秒，result=passed、子入口退出0、error_code／failure_detail均null；输入未变，cleanup_verified=true、private_directory_removed=true，原归属容器／网络／卷及临时文件零残留日志实际通过。官方Artifact 12718461481为23818字节，下载SHA256已核对 `d7e5a6d8717498c06b6b194a9bce95724c495eacb93dba5913f3eaaccfdd0c50`；原始日志、ZIP、预算和独立摘要位于 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-p19-http-handoff-20261027-0slhhjwm`（job-122070268771.log、linux-budget-fa7ae6dc3.json、audit-summary.json）。不是只根据Job摘要判通过。

固定窗口公式0.78秒、Go真实来源查询／趋势及告警打开／签名重试／同一事件恢复152.43秒、Python两源对应告警150.29秒，停止来源的缺样和其他来源隔离均通过。Python两源startup=running/passed、native_imported=true，实际挂载noexec=false／nosuid=true／nodev=true；两源均正常退出、发现范围恢复、failure_stage=null。实际Go148（失败90／健康58）、Agent SDK67（45／22）、Copilot SDK26（15／11），合计241，三源均保留失败／恢复记录且未超原额度；本次没有请求借额、补样、停流恢复或身份／告警断言放宽。两个模块沿各自正式HTTP四项直接声明运行，不代表完整产品依赖和正式身份验收。

205次完整资源检查全部小于四秒，最长1.285895秒；采样锁等待最长0.000208秒，清理锁等待0.000446秒，未触发清理超期。本轮在原20分钟内同时完成业务、预算与退出清理；成功样本的容器硬配额峰值3.25 CPU／4026531840字节内存、私有可写预算上界1807532095字节，均未触线，这些不是CPU／RSS实际用量或容量基线。确定性门禁的135项配置／预算、123＋88项生命周期及登记结果沿前述固定修复有效。

本轮仅为标准Linux HTTP T2：真实Go与两个ASGI SDK来源→mTLS／HTTP SD／Prometheus→Monitor查询／自然一分钟／告警与通知闭环及资源守卫清理通过；不冒充个人本地实链窗口、完整Agent/Copilot正式backend登录或页面、正式密码/MFA T4、Hosted、容量、72小时保留或全workflow通过。旧44230c5c8业务子链通过但整门禁失败的证据仍保留，历史inspect退出1缺少stderr及对象的唯一原因仍未知，不因本轮通过倒改历史归因。本次未手工触发／重跑／取消CI，未新增本地窗口、Hosted、容量或定时，也未重启个人服务。下一步建议先明确正式Agent/Copilot产品进程及页面的HTTP观测本地验收边界，再申请运行窗口。


<a id="formal-python-http-local-20261028-image-auth"></a>

## 2026-10-28 正式Python产品HTTP本地窗口：采样镜像匿名令牌失败

用户按原上限仅授权本轮一次60分钟本地窗口，沿已确认的停源收窄边界：来源存活时验证历史，停源后Copilot为not_active／空观测，Agent保持新鲜；停源后历史读取仍列为另批产品缺口。专项11个构建输入与已验证c15f8fa2a修正一致，`make test-monitor-http-local-runner` 的20项及相关System Go夹具检查通过后，实际执行一次 `make test-monitor-http-local`。启动与源码快照固定为`bfdb7a34f1c8ea0ee57f341dffae37c8faf9e5b6`，不带入共享工作区未提交改动。

2026-10-28 15:11:34至15:11:55（Asia/Shanghai），外层21.046秒、生命周期20.930秒，结果为**failed**，报告阶段owned_volume_preparation、error_code=command_failed_1。实际已越过前轮profile缺项，完成配置投影及当次卷准备；该阶段末尾的固定Nginx镜像拉取失败，封闭诊断包含failed to resolve、failed to authorize、failed to fetch anonymous token；原始输出SHA256为`52e772e82add3ea821f6bc7726a0d15765ab7552a0f1e7d0fbc80b1558e92c4f`。没有保留原始错误响应，不能进一步唯一归因为代理、TLS或远端限流，不把网络根因写成已查明。

尚未启动构建器、完整资源守卫、产品构建、正式登录、采集查询、受控业务请求、页面或停源断言；resource_samples为空、assertions为空。8 CPU／12 GiB、4 GiB停止线和四秒口径均未放宽，但不能把这些配置值当作实际资源峰值或守卫通过。cleanup.zero_owned_resources=true、failures=[]，当次容器／网络／卷及私有材料清理至零残留。本轮失败后停止，不复用原剩余时间、不自动重试或续窗，不运行Hosted、容量、定时、模型／工具或告警重演。

失败后仅只读核对镜像缓存：既定`nginx:1.28.0-alpine@sha256:30f1c0d78e0ad60901648be663a710bdadf19e4c10ac6782c235200619158284`已缓存，仓库摘要精确匹配，平台Linux ARM64，镜像ID为同一sha256。此为失败后的独立元数据证据，不倒写为本轮已进行缓存准入或运行镜像验证。

修正`a5db60d9ae43556edeb4d3227c115cc79d22f3d9`删除该辅助镜像的无条件拉取，在创建当次网络／卷之前只读核验固定仓库摘要、不可变ID与平台；后续卷测量、TLS准备仅以已核验ID和--pull=never运行。缓存缺失、错配、检查失败均停止，无远端fallback、更换镜像或TLS放宽；sampler_image_admission单列阶段，回执只保存封闭元数据。未改变产品依赖来源、身份、请求、清理、四秒或资源上限，也未修改产品查询语义。

修正后验证：新HTTP缓存准入回归先红；`make test-infra-runtime-log-lifecycle`通过，其中HTTP22项、原Trace123项、既有生命周期88项及相关Go／CI登记通过；随后补充当次TLS辅助容器使用确切ID和禁止隐式拉取的用例，`make test-monitor-http-local-runner`最终23项及相关Go检查通过。对失败后已保存的真实缓存元数据，离线验证器也通过；这些结果不代表新完整实链或采样镜像内进程已运行。新修正尚未启动真实新窗口，原Trace／Console／SDK Linux证据仍保持其原固定源码与覆盖范围。

本机保留窗口记录、runtime-report.json、缓存只读回执、红绿门禁和evidence-manifest.json；临时归档可由本会话标准指针`/tmp/addp-p19-http-product-window-bfdb7a34f1c8`定位。runtime-report.json SHA256为`94e66706654ad24ecaa4d0c75821701c56c6a5588d16c688b3277b793f044733`。本轮结论独立保持“运行失败、清理通过”；构建、真实身份和页面等未运行项不得计通过，下一轮须用户另行授权。


<a id="formal-python-http-local-20261028-cache-reclaim"></a>

## 2026-10-28 正式Python产品HTTP独立窗口：编译后私有目录测量失败

用户明确另开一次原上限60分钟独立本地窗口，接受仅核验复用同一固定摘要Nginx缓存；所有资源／四秒／失败清理及停源收窄边界不变。专项构建输入与已验证a5db60d9a修正一致，实际启动及源码快照为`dcf57cc7344c68135e14eac9c29c065f9dea52ff`。2026-10-28 15:40:28至15:43:45（Asia/Shanghai），外层197.163秒、生命周期197.082秒，结果**failed**。失败后停止并零残留清理，未自动重试、复用剩余时间或续窗。

本轮实际覆盖固定摘要缓存准入、卷采样辅助启动、初始资源检查及标准Go编译命令返回0；尚未进入产品镜像构建、正式后端启动、真实密码/MFA、业务请求、指标查询、Console页面或停源断言。sampler_image回执核对了官方固定仓库摘要、不可变ID及Linux ARM64，acquisition=verified_local_cache。不能把这部分覆盖计为完整产品依赖、正式身份或页面验收通过。

45次已完成资源扫描最长0.537962秒，均小于原四秒。完成观测的硬配额最大2.25 CPU／3288334336字节内存（配额，不是实际内存峰值）；合计可写占用最大4159303680字节，约3.874 GiB。下一次扫描在elapsed=195.04秒、measure_private_bindings阶段因原生du返回1失败，du_missing_file=true，失败扫描历时0.404023秒，**不是四秒超时**。原始错误输出SHA256为`9d5a4fd6ae507e15a2645e0d3e0702c666294907ff0ff1ee1938ec20d9513259`；没有保留原始路径，不能唯一确认哪一个文件消失。

主线程随后报告bounded_builder_resume阶段、command_failed_1；共享构建内核在Go命令成功后直接shutil.rmtree当次.cache/go-build，未与sample_lock互斥。可确认私有文件消失与该回收时段重叠的风险，但未把缺失路径写成已唯一定位。失败采样没有得到完整可写值，不能据前45次最大值推导全程未触4 GiB；未放宽或忽略真实du失败。

cleanup.zero_owned_resources=true、failures=[]，当次构建器／容器／网络／卷及私有材料零残留，当前运行断言仍空。未运行Hosted、容量、定时、模型／工具或告警重演；未迁移SDK Linux或既有Trace/Console证据为本轮通过。

待用户确认的最小方案（本轮未改回收代码）：回收前先沿原四秒完整预检，失败或触线即停，不靠删缓存隐藏已触线；缓存删除复用采样锁，锁等待和可中止删除的合格预算独立四秒。采样自身锁等待及全部读取仍限原四秒，不重试、跳过或缩小扫描；路径仅限当次Go对象缓存。先修规范、唯一共享回收内核及互斥/超时/路径/终止清理确定性门禁，新的真实窗口另行确认。中途回收后仍继续采样，须明确这段生命周期约束，不能直接套用退出清理的停止采样语义。

本机窗口报告、resource-summary.json、提案及evidence-manifest.json由稳定临时指针`/tmp/addp-p19-http-product-window-dcf57cc7344c`定位；runtime-report.json SHA256=`62100e1d0fcd4e60330276fc8ad60fc6277c50692114e422082de66e32ee4e0a`。本节独立结论仍为“运行失败、清理通过”，不计完整四秒守卫、完整构建或产品闭环通过。


<a id="go-cache-reclaim-deterministic-20261028"></a>

## 2026-10-28 中途Go缓存回收互斥与独立四秒门禁（仅确定性验证）

用户确认先修规范和确定性门禁，真实新窗口另行确认。实现`2ffd8eb30a2a934f5b3cbb0244b9daa2d0cd4b63`沿唯一共享构建回收内核替换无锁shutil.rmtree，正式Trace与HTTP本地入口均复用，不保留旧删除路径。本批没有启动Docker资源、正式后端、HTTP业务请求或新的真实验收窗口；仅有小型临时文件／原生子进程夹具，不属于产品实链或容量负载。

Go编译命令成功后，先做一次完整资源预检，锁等待、读取／解析合计四秒，读取不重试；有既有失败、预检失败或达到4 GiB即停，不能先删缓存掩盖触线。通过后回收复用sample_lock，锁等待和可中止删除另有独立四秒合格预算；删除仅限当次私有源码.cache/go-build，拒绝越界／符号链接。采样仍继续，自身原四秒和完整统计范围不变；两步分别记时，不合并成更长的扫描预算。

删除子进程未退出或状态未知时不解锁继续；清理会针对该受管子进程终止／回收，仍不能确认退出则保留锁并返回清理失败，不删除相关环境或宣称零残留。成功与失败均保存封闭预检／删除时序及退出状态；严格模式锁等待回执改用实际拿锁时刻计算，不再把总扫描起点误当拿锁时刻。未改变权限、产品API、采样策略／保留、原60分钟／8 CPU／12 GiB／4 GiB上限。

验证：`make test-infra-runtime-log-lifecycle`退出0，包含HTTP23项、Trace及共享回收136项、既有生命周期88项，以及相关System Go夹具／CI登记检查。现有Make与生命周期CI已自动覆盖，未新增入口／依赖／workflow。覆盖完整预检先于删除、预检触线不删、两个方向的采样／删除互斥、绝对预检截止包含锁等待、预检文件消失不重试、删除锁等待扣减预算、返回0但耗尽预算仍失败、子进程终止／未知状态不放锁、原生删除子进程确实回收及越界／符号链接拒绝。向既有用例注入原无锁删除行为时，用例以cache removed while active sample owns lock如期失败；恢复新路径后全部通过，不把该预期红例记为正常门禁失败。

本机准备证据由`/tmp/addp-p19-cache-reclaim-preparation-current`定位，preparation-evidence.json记录固定实现提交及红绿日志散列。**未运行**修正后的正式Go编译→镜像→身份→HTTP／页面闭环，未运行Hosted、容量或定时；此前dcf窗口继续保持“运行失败、清理通过”。上次最后一次失败采样没有完整可写值，修正不证明4 GiB必然足够；新窗口若预检触线仍按原标准停止，不能通过回收补成通过。


<a id="formal-python-http-local-20261028-resource-list-deadline"></a>

## 2026-10-28 缓存回收修正后独立窗口：完整资源扫描超时

用户在上一批规范／确定性修正完成后要求继续，本轮沿上一条明确的下一步，仅开启一次原上限60分钟独立本地窗口。专项11个构建输入与2ffd8eb30修正一致，运行前再次执行`make test-infra-runtime-log-lifecycle`通过（HTTP23、共享内核136、生命周期88及相关Go／登记），随后实际源码快照固定为`df82aed91964dadd54da13ca946a8c95c5843d11`，不带入共享工作区未提交改动。

本轮结果**failed**：parent单调计时99.971秒，报告阶段product_compile、error_code=resource_sample_deadline_exceeded，已零残留清理。墙钟起止为2026-10-28 17:36:34至17:38:32（Asia/Shanghai），UTC时间差118.229秒，与单调耗时相差18.258秒；原因未确认，不自行校时、回填或混用两种时间。该差异不用于推导暂停、睡眠、NTP或VM原因，本轮两种口径均远未到60分钟且已按失败停止。

实际覆盖缓存镜像准入、配置投影、构建器／采样辅助及初始检查，随后Go编译被资源守卫中止。尚未进入新的go_cache_precheck／go_cache_reclaim，报告没有go_cache_reclaim回执；因此本轮不能验证该修正的真实运行效果，也未到完整产品镜像、后端启动、真实身份、受控请求、指标查询、页面或停源。

18次已完成完整扫描最长0.650891秒，观测到的合计可写最大1905561600字节。下一次扫描在elapsed=90.34秒失败，完整历时4.025778秒，其中锁等待0.008522秒。列出归属容器步骤已耗3.210680秒（CLI创建0.088640秒、进程等待3.007714秒、输出读取0.001432秒）；inspect_owned_containers仅剩不足0.8秒预算，该步骤0.773644秒（创建0.041333秒、等待0.699569秒、终止处理0.022726秒）。不能把本次解释为inspect自身耗时四秒，更不能从CLI等待直接唯一归因daemon、VM、文件遍历或代理。完整扫描超过原四秒，未重试或放宽；失败扫描没有完整资源数据，不宣称全程资源峰值或四秒守卫通过。

cleanup.zero_owned_resources=true、failures=[]，当次构建器／容器／网络／卷及私有材料零残留。失败即停，不复用剩余时间、不自动重试或续窗，不运行Hosted、容量、定时、模型／工具或告警重演。未因这次CLI等待证据不足而修改HTTP产品或已确认的四秒规则，也未改缓存回收实现；2ffd确定性结果仍仅属于其相应层级，不迁移为本轮产品通过。

本机归档由稳定临时指针`/tmp/addp-p19-http-product-window-df82aed91964`定位，包含原始窗口报告、failed-inspect-summary.json和evidence-manifest.json。runtime-report.json SHA256=`2da4262a248811ecc94a012a9a551c08082373fdb9a131e2555566d6e48c4ac7`。当前前置阻塞是完整资源扫描中的CLI列表等待；下一步应先只读复核已有等待记录、明确可归因范围及保持原预算的最小方案，不直接再次开启真实窗口。正式后端至页面闭环、缓存回收实际覆盖以及停源后历史读取等未验／另批范围如实保留。

<a id="http-linux-ci-20261028-private-directory-recurrence"></a>

## 2026-10-28 Linux HTTP T2目录消失复发：独立入口离线核对

只读核对既有[Release/T2 attempt 1日志](https://github.com/pampa0629/addp/actions/runs/40660674352/job/122734708027)，源码`cd0c537d57eb7c3a704ec6cbadfc3bb3d8a41b73`；没有派发／重跑CI，没有新建本地环境。原始日志第936行HTTP完整预算JSON与本机提取文件逐字段一致：result=failed，elapsed_seconds=185.364465198，error_code=http_budget_sample_command_failed，failure_detail.stage=private_directory_size、kind=command_failed、exit_code=1、directory_error=path_disappeared，stderr_bytes=212；du命令0.024313秒、完整采样0.729208秒，**不是四秒超时**。failure_during_cleanup=false、inputs_unchanged=true、cleanup_verified=true、private_directory_removed=true；记录请求Go=0、Python=[0,0]，不能计为HTTP业务闭环通过。聚合Job122763262513传播该失败，不另计一次独立产品故障；其他自然成功不关闭此间歇失败。

当前`metrics-http-budget.py`与失败源码的文件SHA256同为`4056b0d679c10ebcf272a072a5d8539635c7ea7e979af64e1d96d20f6fcacbcf`。Linux入口为Make `test-monitor-metrics MONITOR_METRICS_TEST_ARGS="--case=http-quality"` → metrics-http-budget.py → monitor-metrics-gate.sh，按整个当次WORK_DIR执行du；其go-cache、go-tmp、tmp均纳入该根目录，并由标准脚本在原生`go test -c ./processmetrics`之前设置GOCACHE／GOTMPDIR／TMPDIR。现有跨进程锁只覆盖采样与退出清理，并未将编译器内部文件生命周期纳入锁协议。

本地`2ffd8eb30`回收修正属于另一条入口：monitor-http-local-gate.py复用monitor-traces-local-gate.py的reclaim_go_build_cache，仅保护显式删除当次源码.cache/go-build。Linux预算入口没有调用该函数，故不能用本地136项共享内核检查宣称已覆盖Linux目录消失。相反，Linux失败源码与当前HEAD都已包含同一共享回收内核（文件SHA256=`b3a934a250034eafaff1359b5afd4cb019fec7ce9285e2a01b0ec476a936dfe6`），进一步说明“包含修正提交”不能替代“执行路径覆盖”。本节也不把独立Linux故障与上一节本地CLI列表等待合并。

原始stderr及缺失路径未保存；现有记录只能确认du遇到路径消失，不能唯一确定根目录、Go缓存、Go临时构建对象、其他短命文件或实际删除者。尚未改采样／编译生命周期，不忽略du、不排除目录、不缩小统计或放宽Linux原4 CPU／4 GiB／私有可写小于2 GiB／完整四秒／1200秒预算。下一批最小建议是先讨论封闭的缺失对象类别／身份摘要、根目录存在性及编译阶段单调边界取证，再修改对应规范和确定性门禁；不先猜测互斥范围，也不盲目重跑。

本机离线材料位于`/tmp/addp-ci-20261028-100049`，原始job-122734708027.log SHA256=`ffeaef3b619ac392218fa8b5079e68639b83408a36ef82a3f1f91b67a48b4767`，提取文件HTTP_cd0_sampler_recurrence_raw_result.json。该核对只证明上述固定源码／单次CI失败与路径覆盖关系；未新增产品实现或真实验收，不运行Hosted、容量或定时。

<a id="http-linux-minimal-forensics-20261029"></a>

## 2026-10-29 Linux HTTP目录与编译最小取证（确定性及自然CI分列）

用户确认先修规范／确定性门禁，再沿自然Linux CI验证；实现`05c57272c8116a19cc9ca955cf9f97bafdaf9b82`仅修改既有HTTP独立预算入口及其标准shell接入，不修改产品API、编译／采样互斥或资源上限。缺失对象只保留预定义目录类别与当次project域下的相对身份SHA256，严格解析至多4 KiB／八行的GNU/BSD诊断；越界、穿越、歧义引号／控制字符及重复斜杠不推测路径。根目录观察用lstat，不跟随链接；原四秒耗尽不追加读取，诊断费用计入完整采样。首du失败不改判、不重试、不排除缓存或临时目录，原4 CPU／4 GiB／私有可写小于2 GiB／1200秒仍保持。

编译开始／观察返回以同一主机monotonic_ns原子写入原归属回执，保留非零退出码；中途取消只见开始时记incomplete，不补造结束。du失败另外保留采样／命令的原始单调秒端点、采样取得锁后读取回执时的编译状态，最终编译观察另列，不将两者混成失败当时状态。数值时序只证明观察关系，不唯一证明删除者；旧日志仍无实际缺失对象，不能反推。

验证：既有生命周期Make入口先通过HTTP本地23项／共享内核136项及相关Go夹具，随后被共享工作区中另一专项surface-area持久化注册检查阻断，整条入口不得计通过，未改动该专项。继续执行该入口原有确定性子命令：`python3 -m unittest scripts/test/metrics-http-budget_test.py scripts/test/infra-runtime-log-lifecycle_test.py scripts/test/trace-deployment-config_test.py scripts/test/trace-capacity_test.py scripts/test/postgres-metrics-lifecycle_test.py`合跑96项通过；最终预算取证单独35项通过；既有Metrics配置脚本120项、shell语法、T2 CI登记及diff检查通过。新增重复斜杠拒绝用例初次失败后修正，通过仅指当前取证契约。夹具只有小型临时文件、真实du缺失根读取和受控Go替身子进程（含进程组取消），没有Docker、本地服务或业务负载。

现有Make生命周期、dev-lifecycle及自然Metrics矩阵已覆盖测试和脚本输入，既有HTTP预算JSON artifact自动保存新增字段，不新增入口／依赖／workflow。自然push运行[40799494798](https://github.com/pampa0629/addp/actions/runs/40799494798)、HTTP Job[123204132217](https://github.com/pampa0629/addp/actions/runs/40799494798/job/123204132217)，固定实现SHA如上，push／attempt 1已通过，HTTP生命周期及预算artifact上传步骤均成功；未手动dispatch或rerun。原始artifact `12865618656`共24238字节，ZIP SHA256=`ccac6dfe1557dec98cce5e19f912908e93f5f88dae37c59204ea1189877e681b`与API登记摘要一致，CRC完整；唯一monitor-http-budget.json SHA256=`bbb5008607a5310a26f639ca497d85ff650d745829a827d58ed9b4fc4a9f84a8`。独立逐文件核对报告353个输入全部匹配该提交，未以当前HEAD替代。

真实Linux报告result=passed、exit_code=0、error_code=null，elapsed=854.099548秒；209次完整样本最长1.182123秒，私有可写保守上界最大1808158783字节，容器硬配额合计最大3.25 CPU／4026531840字节内存，均在原上限内。请求Go148（失败90／健康58）、Python Agent75（45／30）、Copilot28（15／13），总251次，三源阶段和计数契约一致。process_compile为finished／exit0，原始单调纳秒开始64979234562、观察结束138843033209；inputs_unchanged、cleanup_verified、private_directory_removed均为true。**本次没有复现目录消失，缺失对象及根目录失败诊断现场分支未触发**；编译边界实录和单次Linux功能门禁通过，不等于旧间歇故障已修复／删除者已定位，不用本次成功覆盖此前失败。

本机准备散列清单`/tmp/addp-p19-http-minimal-forensics-20261029.json`记录源码／输入／门禁日志，Linux归档核对摘要`/tmp/addp-p19-http-minimal-forensics-linux-result-20261029.json`另记353输入核验与各项边界。没有新开本地窗口，不运行Hosted、容量或定时，不恢复此前失败窗口；正式产品到页面闭环仍未验收通过，本批取证不替代它。

<a id="http-linux-followup-manual-20261029"></a>

## 2026-10-29 后续自然Linux HTTP门禁手动复核（未复发）

本轮仅按用户要求只读核对后续main自然push中的HTTP质量子Job，不手工dispatch／rerun，不新开本地窗口，不运行Hosted、容量或定时。截至2026-10-29 11:19（Asia/Shanghai），修正后的两条后续HTTP子门禁完成且通过，另有一条运行中、一条排队；没有生成HTTP Job的其他运行不计通过。所有结论绑定各自源码和HTTP子Job，不由workflow聚合状态推定。

| 固定源码 | 自然运行／HTTP Job（attempt 1） | 结果 | 总耗时（秒） | 完整样本／最大耗时（秒） | 请求Go／Python／合计 | 清理 |
| --- | --- | --- | --- | --- | --- | --- |
| aa516137d437ac6de8e5c61df8cfc08305e6b08b | [40806389627／123226471740](https://github.com/pampa0629/addp/actions/runs/40806389627/job/123226471740) | passed | 773.609527 | 189／0.916333 | 148／[81,30]／259 | verified／私有目录已删除 |
| 24d2f15648494054627c36dfa01f61885e41fddd | [40807277727／123229384045](https://github.com/pampa0629/addp/actions/runs/40807277727/job/123229384045) | passed | 872.804636 | 215／1.216953 | 163／[75,28]／266 | verified／私有目录已删除 |

两份原始artifact均逐项验证ZIP大小／API登记SHA256／CRC及唯一JSON成员，并独立将各自353个输入散列与固定提交逐文件比对；两版均包含05c57272c修正，metrics-http-budget.py／monitor-metrics-gate.sh与该修正没有差异。result=passed、exit_code=0、error_code=null、无failure_detail；三源均具备健康阶段且阶段计数和原额度一致，inputs_unchanged／cleanup_verified／private_directory_removed均为true。最大私有可写保守上界分别1808130111／1808166975字节，硬配额最大均为3.25 CPU／4026531840字节内存，低于原停止线和资源上限。

编译观察真实落盘：aa516的monotonic_ns开始63274922475、观察结束111259740505；24d2为191068652420／266955472635，两者finished／exit0。两次都**没有复现目录消失**，因此缺失对象类别／身份散列／根目录失败诊断现场分支仍未触发，不能由编译成功及门禁通过反推旧失败删除者或宣布间歇根因已修复。本轮没有形成新的修正方案或改采样／编译生命周期；若后续自然复发，先对照新增对象和单调边界提出最小方案，再经讨论实施。

归档身份：aa516的artifact12868648938为23553字节，ZIP SHA256=`fb62c5433416e4e3819ca6e4cc821ef7dc214b55e0fd3d1227edd0f1d1b19bb1`，报告SHA256=`d4ec9a804f06f876dbc86af4285c703212dff06f5d1d6414bb42894c06963ec5`；24d2的artifact12869049745为24432字节，ZIP SHA256=`06bde55ad5c0b9c930e489d2a3557acb8dca84225eba4819eb18e65d45ce9a2b`，报告SHA256=`cbcc12b7cd387e15dfb4f7f8902d940002fd27bf650925423277f58484bd0626`。本机/tmp/addp-p19-http-followup-verified-20261029.json保留独立核对摘要，另有同前缀Job快照和artifact元数据。

快照中[40809646875／123236922601](https://github.com/pampa0629/addp/actions/runs/40809646875/job/123236922601)（ebdb5b82e721834c69eda8d3c1127580cb667583）运行中，[40810565372／123241061462](https://github.com/pampa0629/addp/actions/runs/40810565372/job/123241061462)（49ecae13ecc23bb9d626ab5617234ffdca57f043）排队，分别保留未完成状态，不计通过。此为一次手动核对，不建立后续监控；正式产品到页面闭环及停源历史读取的原有缺口不由本节替代。

<a id="http-linux-followup-second-manual-20261029"></a>

## 2026-10-29 后续自然Linux HTTP门禁第二次手动复核（首轮与重跑分列）

本次沿用户“继续”仅进行一次只读核对：关闭上一节两条未完成状态，并核对其后main自然push中的HTTP质量子Job，不派发／重跑CI，不新开本地环境，不运行Hosted、容量或定时。2026-10-29 22:20（Asia/Shanghai）元数据快照中，上次两条已完成通过；之后44个push运行里筛出18条HTTP成功子Job，其中17条属于attempt 1。因此本次纳入的自然首轮HTTP状态共19条均success；无HTTP子Job的运行不计通过，不把其他专项的聚合失败归本专项。

其中40880761833／123475011822虽状态success，但属于已有push运行的attempt 2，不能计为自然首轮；原attempt 1没有HTTP子Job。本轮未触发该重跑，也不推测操作者或重跑原因。该项仅作排除说明，不纳入以下自然门禁验收统计。

深核三份自然attempt 1原始预算归档：

| 固定源码 | 运行／HTTP Job | 结果 | 耗时（秒） | 完整样本／最长采样（秒） | 请求Go／Python／合计 |
| --- | --- | --- | --- | --- | --- |
| ebdb5b82e721834c69eda8d3c1127580cb667583 | [40809646875／123236922601](https://github.com/pampa0629/addp/actions/runs/40809646875/job/123236922601) | passed | 873.542461 | 215／1.312225 | 161／[67, 26]／254 |
| 49ecae13ecc23bb9d626ab5617234ffdca57f043 | [40810565372／123241061462](https://github.com/pampa0629/addp/actions/runs/40810565372/job/123241061462) | passed | 881.329419 | 217／1.330465 | 161／[75, 28]／264 |
| 944c4bcf74272a568045a8c066da34878f4c7fa6 | [40877071516／123458861015](https://github.com/pampa0629/addp/actions/runs/40877071516/job/123458861015) | passed | 815.618545 | 200／1.213109 | 148／[67, 26]／241 |

三份ZIP大小／API登记SHA256／CRC及唯一JSON成员验证通过，各自353个输入均独立逐文件匹配其固定提交，预算／编译脚本与05c57272c无差异。result=passed／exit0／error null／无failure_detail，三源阶段及额度一致，编译边界finished／exit0，inputs_unchanged／cleanup_verified／private_directory_removed均为true。最大私有可写保守上界依次1808232511／1808252991／1809166399字节，硬配额最大均3.25 CPU／4026531840字节内存，所有完整样本小于四秒、总耗时小于1200秒。编译开始／观察结束的主机monotonic_ns分别86760929319／164596238320、71591822071／148776493965、82643647001／143610907868。

其余16条只核对HTTP子Job状态success，未下载原始预算归档，不宣称已独立核验其请求／资源峰值或清理：

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

本次没有发现自然首轮HTTP失败，三份深核报告未复现目录消失；缺失对象现场分支仍未触发，历史删除者／根因仍未定位，不以多次成功关闭间歇故障。没有提出推测性修正或改变采样／编译生命周期；后续若自然复发，先以新增类别／身份与单调边界给出最小方案，再经讨论实施。本节不替代正式Agent/Copilot后端到页面、停源历史读取等原有缺口。

三份原始归档身份：

| artifact | ZIP字节数／SHA256 | 报告SHA256 |
| --- | --- | --- |
| 12870806065 | 24322／`45879083b619453b92c6026d4fd8cf50e77d5db320468a97dd81cb7010b65346` | `b419f1d8c34909d8e80edc50b3c2ad3c6d6e81d525dd2b06c0f08b1f397ff48b` |
| 12870867972 | 24429／`354c8e8e1beda61da1aeef07bfa247b9ec608dd8f7417e62773dac66e0169c48` | `1d4fed702c8b6ff9882f76104ce6f3383c2ba4c502013086167110b4691e263b` |
| 12900440023 | 23895／`41d4cce8c8be16ba443e6345110e34841f11923d73be1a5b8275bb8cc7921278` | `7276eee95febede05ef9668cdd2cc5e47688412b6b8f68fd1aad7cac6a30d837` |

本机`/tmp/addp-p19-http-followup2-summary-20261029.json`保留深核／仅状态／排除项分类，`/tmp/addp-p19-http-followup2-verified-20261029.json`保留归档核验摘要，另有同前缀run、attempt及Job快照。本轮只做文档证据变更，校验主题计数／锚点／索引和diff，不重跑产品或代码门禁，不建立持续监控。

<a id="http-linux-followup-third-manual-20261029"></a>

## 2026-10-29 后续自然Linux HTTP门禁第三次手动复核（新增一通过、一未完成）

本轮只读复核上次快照时未形成HTTP结果的两条push及后续自然运行。2026-10-29 23:07（Asia/Shanghai）快照中，13条候选push均为attempt 1，仅两条生成HTTP质量子Job：一条完成通过，另一条运行中；没有HTTP子Job的11条不计通过，也不由workflow聚合失败或取消推导本专项失败。没有手工dispatch／rerun、新建本地窗口、Hosted、容量或定时。本轮没有复查或合并上节已明确排除的attempt 2。

新增通过：固定源码`a62ef412ceff7539ba784184a65c8a8dca9bd268`，[run40891592619／HTTP Job123510009973](https://github.com/pampa0629/addp/actions/runs/40891592619/job/123510009973)，自然push／attempt 1。原始artifact12907905105共24222字节，API登记摘要与实际ZIP SHA256=`c061ddd138545ff0cdf2e4d1a35b2254cd6a706717eb2eb92aeba2e3e07879bd`一致，CRC和唯一JSON成员通过；报告SHA256=`4f51c497dc7176d938cd27260d68fd4c989e1d006c40afff0a6a817d960583c8`。353个输入散列逐文件独立匹配固定提交，预算／编译脚本与05c57272c修正无差异。

报告result=passed／exit0／error null／无failure_detail，elapsed=848.050459秒；209个完整样本，最长1.282374秒，私有可写保守上界最大1809186879字节，硬配额最大3.25 CPU／4026531840字节内存，均守原四秒／1200秒／资源上限。请求Go148、Python[75,28]、总251，三源阶段与计数额度一致；process_compile为finished／exit0，主机monotonic_ns开始72418214675、观察结束149527675593，inputs_unchanged／cleanup_verified／private_directory_removed均为true。**本次未复现目录消失**，对象类别／身份及根目录失败现场分支未覆盖；旧间歇根因及删除者保持未定位，不以新增成功关闭。

未完成项：[run40891932904／HTTP Job123517711481](https://github.com/pampa0629/addp/actions/runs/40891932904/job/123517711481)，固定源码`1a721a53fec6fb7a0b95920ce89cba7b438a9cc9`，自然push／attempt 1，快照仍in_progress、Verify isolated metrics lifecycle在运行，预算上传未开始；不计通过或失败，不把尚无报告解释为缺失对象。

本轮只更新证据，无产品／采样实现变更或推测性修正。若后续自然复发，先核验原始对象类别／身份和单调阶段边界，提出最小方案讨论后实施；正式产品后端到页面及停源历史读取等原有缺口继续单列。仅文档变更执行主题计数、锚点／索引及diff检查，无新代码门禁／运行验收或持续监控。本机`/tmp/addp-p19-http-followup3-verified-20261029.json`保存独立核对摘要，同前缀runs／candidates／jobs／artifact-meta保存此次只读快照。

<a id="http-linux-gotmp-recurrence-20261030"></a>

## 2026-10-30 Linux HTTP复发：go_tmp后代对象与编译边界覆盖缺口

本轮沿用户已批准的“复发先只读定位，再给最小方案讨论”边界，核验已存在的失败归档与源码；未新建环境、未重跑CI／有效验收，未运行Hosted、容量或定时，也未改采样、编译互斥或产品实现。固定失败源码`3f43b9bff04fdccb3d689c3efea8a9b9b783e44d`，[自然push attempt 1／run40903636868／HTTP Job123552811295](https://github.com/pampa0629/addp/actions/runs/40903636868/job/123552811295)当前仍completed／failure。

原始artifact12911921470共20008字节，ZIP SHA256=`34b778b01ee5d455af0c7a050c1a27af620522d4dcdcea9fe6e596569f4facc0`与API登记一致、CRC及唯一JSON成员通过；报告SHA256=`bfcd8249ebd4999bbaf571b496c22efc6d3b9b8023cc2c13c1491ff666a56916`。353个输入散列逐文件独立匹配该固定提交，报告摘要与原始日志第958行／已有脱敏提取JSON逐字段一致。result=failed、error_code=http_budget_sample_command_failed、阶段private_directory_size、du exit1／path_disappeared，命令0.028348秒、整次采样0.656893秒，**不是四秒超时**；完整运行255.494611秒停止。failure_during_cleanup=false，Go/Python请求全0，inputs_unchanged／cleanup_verified／private_directory_removed=true，清理不计业务通过。

新增对象证据首次命中：directory_objects.status=parsed，唯一category=go_tmp，identity_sha256=`99b0b31b779dc97194197add6b5bb1403c933b9269625fa045b7383b081d5748`，根目录lstat观察为directory。用归档中原project及现行相对身份公式，对已知相对名go-tmp计算的散列不等于该身份，因此可排除go-tmp目录本身，确认是其后代对象；没有反向枚举或恢复原始路径。该身份只能关联当次对象，不是精确文件名／类型，也不能说明哪个进程删除。du的路径消失诊断与根目录存在性表示遍历中读取不到对象，不证明此前已有独立成功枚举记录、对象删除时刻或删除系统调用。

采样原始单调秒起点290.890690083，du开始291.519119050、观察退出291.547467366；process_compile已结束于monotonic_ns142259618759／exit0，仅能证明那次已记录编译早于采样148.631071秒。源码核对发现该记录只覆盖monitor-metrics-gate.sh的Common processmetrics来源编译，随后monitor-metrics-probe.py的prepare_http_test_binaries还串行执行HTTP查询resourcequery与HTTP告警service两次go test -c；它们继承同一GOTMPDIR，但没有编译边界记录。该函数在失败版本与当前5f7876931相同，预算／shell脚本也未改变。因此“process_compile已结束”不等于所有编译均结束；不能直接排除后续编译或认定其为删除者。现有记录尚不能区分失败时位于两次准备编译的哪一项，也没有确证删除进程。

原始日志在来源恢复后出现后续Go依赖下载，再由守卫停止；这与静态调用链的后续编译可能性一致，仍非删除因果证据。记录0业务请求及Python startup unavailable只能证明未具备业务闭环，不能单独证明每一条初始化语句都未执行。不因这些先后关系改变互斥、忽略du或缩小统计。

拟议最小下一步（已向用户提出、尚未实施）：以最多三个固定编译阶段（Common来源／HTTP查询／HTTP告警）替换单条process_compile，复用同一记录内核，保存各自开始／观察结束／退出码和取消incomplete，保留前序事实；采样快照与最终观察继续分列，不保留新旧双轨字段，不新增原始路径／stderr或进程删除跟踪。先修规范和确定性门禁，覆盖两处调用真实接入、阶段唯一／有界／保序、异常／取消不补结束及首失败保留，再沿唯一自然Linux CI观察。本提案只修边界覆盖缺口，不提前宣称修复对象消失；保持原4秒、4 CPU／4 GiB／私有可写小于2 GiB／1200秒，不手动重跑、不新开本地窗口，无Hosted／容量／定时。

后续自然成功独立保留：`6d5ca9401ac62ac85d660d9ca957d28780de4209`，[run40979523356／HTTP Job123816052278](https://github.com/pampa0629/addp/actions/runs/40979523356/job/123816052278)，push attempt 1；既有artifact12944927519共24194字节，ZIP SHA256=`aa177f912236e517981cf7f3e29407eab256c7f7b65171f53ca01e8c4c91c4d3`／报告SHA256=`fb7785d7baf2c35ed5da31faaa21f2badcb103374c8281282fb2a8b86b359e23`，API摘要、CRC和353输入独立核验通过。报告passed／exit0／error null，848.407700秒、208样本最长1.264080秒、可写保守上界最大1809223743字节，251请求、三源阶段契约、编译exit0及清理通过；没有重跑，不用此成功关闭3f43的go_tmp复发。

本机`/tmp/addp-p19-http-gotmp-recurrence-audit-20261030.json`记录两份原始归档独立核对、对象根散列排除及编译边界；失败原日志／提取结构位于既有`/tmp/addp-ci-20261029-160021`，通过归档沿既有`/tmp/addp-ci-20261030-004434/http-budget-6d5.zip`复用。仅文档证据变更，验证主题计数、锚点／索引及diff；没有新的代码门禁或运行验收。原正式产品到页面及停源历史读取缺口继续单列。

<a id="http-three-compile-stages-20261030"></a>

## 2026-10-30 三阶段编译取证：规范、确定性与自然Linux分列

用户确认先修规范和确定性门禁，再沿自然Linux CI验证。实现`3a004a94759f0414030d99c992e057c2651c2cb5`用唯一compile_stages替换单条process_compile；三个固定阶段process_source／http_query／http_alert分别由既有shell及正式probe的两次Go编译调用同一原子记录内核。不改命令、环境、原串行顺序、180秒编译超时、编译／采样互斥和清理协议，也不新增运行入口、镜像、权限或依赖。采样时所见compile_stages_at_sample_read与最终compile_stages分列，不保留旧新双轨字段。

每阶段至多一次开始／观察结束，保留前序事实、同一主机单调纳秒和实际退出码；后序只能在前序成功后开始，未知／越序／倒退／重复记录拒绝。未开始／取消不完整／非法分别表达，不补造结束。Python直接观察的负信号退出码原值保留；已有编译错误不被边界写入错误替代。正常HTTP通过要求三个阶段均有完整成功边界，缺失／非法不能证明取证通过；公共mTLS前置仍未进入编译。du首失败、完整统计范围及原4秒／4 CPU／4 GiB／私有可写小于2 GiB／1200秒预算均保持，不添加删除跟踪、原始路径或stderr。

确定性验证：`make test-infra-runtime-log-lifecycle`最终退出0，包括HTTP本地23项、Trace共享内核136项及相关Go／CI登记、预算与生命周期合跑100项；`python3 -m unittest scripts/test/metrics-deployment-config_test.py`122项通过，Shell语法、T2登记及diff检查通过。新增回归从正式probe提取实际函数，以受控Go替身验证两次编译接入、顺序历史、错误／信号／超时和取消不完整，回执写失败不覆盖编译首失败；验证采样快照与最终观察独立、缺任一阶段不能通过。向同一回归注入“省略两次HTTP边界记录”时，测试按预期失败；恢复当前实现全部通过，该预期红例不计正常门禁失败。夹具仅临时小文件／原生子进程，无Docker、业务负载或新本地环境。

现有Make生命周期／dev-lifecycle／Metrics配置门禁与T2输入登记已覆盖上述三个脚本和测试，HTTP预算artifact会保存新字段，不需新增Make／workflow或兼容路线。此次只推送main触发[run40984761553／HTTP Job123833677076](https://github.com/pampa0629/addp/actions/runs/40984761553/job/123833677076)，push attempt 1已通过，未手工dispatch／rerun。原始artifact12946559397共23965字节，ZIP实际SHA256=`aca8ce7f7e09818fc3de1b3a4cadd71f413e8961ae8967c02fa530f59a953278`与API登记一致，CRC和唯一JSON成员验证通过；报告SHA256=`f749a1af0e4d6bf2bcde953813cbd61e38c8b4699e163668ee5acd269916727d`。353个输入独立逐文件匹配固定实现3a004a947，不借当前HEAD替代。

真实报告result=passed／exit0／error null，823.080908秒，202个完整样本最长1.042991秒，私有可写保守上界最大1809215551字节，硬配额最大3.25 CPU／4026531840字节内存，原四秒／资源／1200秒均满足。请求Go163、Python[75,28]、总266，三源阶段和计数契约一致；inputs_unchanged／cleanup_verified／private_directory_removed=true，无failure_detail。compile_stages.status=valid，三个且仅三个阶段均finished／exit0，顺序及单调端点验证通过，报告没有旧process_compile字段：

| 阶段 | 开始monotonic_ns | 观察结束monotonic_ns | 退出码 |
| --- | --- | --- | --- |
| process_source | 103719535750 | 163707494686 | 0 |
| http_query | 245058910654 | 246027629632 | 0 |
| http_alert | 246028054898 | 300201039945 | 0 |

本次没有复现目录消失，缺失对象现场阶段快照分支未触发；此次真实三阶段通过只证明取证覆盖已接通，不证明3f43历史删除者或根因已定位，不把新的成功时序回填为旧失败阶段。

本机准备清单`/tmp/addp-p19-http-three-stage-preparation-20261030.json`保存固定实现输入与最终门禁／预期红例日志SHA256；`/tmp/addp-p19-http-three-stage-linux-audit-20261030.json`另存原始归档、353输入及三阶段独立核对。没有新开本地窗口，不运行Hosted、容量、定时；3f43的go_tmp后代对象复发仍为未定删除者，本批仅修取证覆盖，不宣称已修复间歇根因，原正式产品到页面及停源历史读取缺口不被替代。

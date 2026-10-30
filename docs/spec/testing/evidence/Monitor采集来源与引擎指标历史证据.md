# Monitor 采集来源与引擎指标历史证据

[返回 Monitor 历史证据](Monitor历史证据.md) · [返回验收证据索引](../验收证据索引.md)

本专题收录PostgreSQL 账号、来源准入、引擎身份与专业观测。固定源码、环境、失败记录、修复结论与未覆盖范围以各记录为准；本次仅整理文档，没有重跑验收或刷新 CI 状态，不推导当前 HEAD 或生产 T5 已通过。跨主题语境与原有顺序见 [Monitor 历史证据入口](Monitor历史证据.md#原记录入口)。

## Infra与Business PostgreSQL 自动接入

2026-10-20 已核验 [engine-admission Hosted 专项](https://github.com/pampa0629/addp/actions/runs/39324018076/job/118305077445)，精确源码 `b3dcae5a3e62b37dbbb15f9e28067a94b6454545`（包含成功更新审计状态修复 `0155d5db578ef3324d81db8aa7a8d2482539f55e`）。独占 Linux x86_64 部署通过正式三员 Bootstrap、真实密码与 MFA 建立非委托 Platform User；一次集合请求接入 Infra／Business 两个单容器 PostgreSQL，显式关联同一新建主机。浏览器验证保存、刷新恢复、已接入项禁选与两个当前容器来源；两张截图已目视核对。

场景报告 `engine_admission.result=passed`，覆盖 Owner 列表／详情读取、版本冲突、来源变化／重复成员拒绝、第二项失败整批无根记录、修改与停用退役，身份隔离通过。正式场景约 62.544 秒，浏览器约 26.469 秒；原有身份、断言及预算不变。外层 `summary.txt` 分别记录 `result=passed`、`cleanup=passed`、`infra_cleanup=zero_residuals`，不能仅用页面通过替代完整验收。

修复前实际 PUT 因成功审计缺少 HTTP 状态导致 PostgreSQL 约束拒绝；新增 HTTP Handler／PostgreSQL 回归先复现 400，再验证正常更新、旧版本 409、停用与两条成功审计。`make test-module MODULE=system GO_ONLY=1`、`make test-system-iam-runner`（19 项）及 `make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package api --test engine-deployments"`、对应 `--package repository` 均通过。本地只使用 `addp_iam_test`；T2 的受控认证上下文不冒充真实身份 T4。同源码 [System 前端](https://github.com/pampa0629/addp/actions/runs/39323690209/job/118303942964)、[Platform consistency](https://github.com/pampa0629/addp/actions/runs/39323690209/job/118303942260) 和 [System IAM PostgreSQL](https://github.com/pampa0629/addp/actions/runs/39323690244/job/118304788473) CI 已实际通过。

此结果限于自动发现、管理员确认接入和单主机承载声明，不表示数据库专业指标、容器资源、其他引擎／集群、多真实主机、原生 macOS／Desktop VM、租户连接关联或生产 T5 已验。没有读取租户业务数据或执行专业采集 SQL，个人部署未重启。此前未通过的尝试保留于各次 CI 归档，不计为通过。

## PostgreSQL 固定指标来源与引擎身份投影

2026-10-20，标准 `make test-monitor-metrics` 在专属临时 PostgreSQL 15／SQL Exporter／Nginx／Prometheus Compose 中通过。Infra 与 Business 依次验证独立 SCRAM 身份、固定 16 项指标、真实双向 TLS、无证书／明文／配置及重载路径拒绝、连续 SQL 失败时无旧数据库样本、错误计数递增、恢复产生新快照；关闭统计时不将缺失计数填零。完整主机、进程、趋势、六来源总览及资源告警生命周期回归通过，退出核验当次容器、网络、卷和临时文件零残留。两个作用域顺序复用专属测试 PostgreSQL，不等同两个正式部署或专业指标 T4。

同轮 `make test-module MODULE=common GO_ONLY=1`、`MODULE=system GO_ONLY=1`、`MODULE=monitor GO_ONLY=1`、System Swagger 生成／路由覆盖通过；`make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package repository"` 在标准 `addp_iam_test` 通过，验证同一只读可重复读快照内两个平台引擎身份、私有字段排除、主机及部署停用、Desktop 主机类型拒绝、控制面失败整份拒绝。现有固定 Monitor Service 权限与拒绝其它身份的路由回归仍通过，没有发布新角色或权限。

证据仅覆盖数据库汇总、受保护指标来源及最小后台身份投影。标准可选来源生命周期、Prometheus 引擎发现、当前 User 授权的专业查询与页面仍待接入，未安装个人部署，未计为专业监控正式身份 T4 或生产 T5。

## PostgreSQL 采集账号认证边界

2026-10-20，`make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=postgres-source` 独占 T2 通过。Infra／Business 依次复用同一本轮 PostgreSQL 15：先证实 PUBLIC CONNECT 可让实际采集账号连接镜像默认 `postgres`，安装本方 HBA 首部规则并真实重载后，跨库和 Unix socket 路径明确被 HBA 拒绝，维护库 SCRAM、管理员连接、密码轮换及真实 mTLS 指标读取仍通过。额外验证数据库保留字 `all` 不成为通配授权、输入文件版本不符拒绝写入、原文件字节／属主／权限恢复，30 项配置校验及 38 项 CI 注册测试通过。全退出确认当次容器、网络、卷和临时文件零残留；不创建额外 database，不读取业务表，不触碰个人数据库。该结果交付规则生成和独占认证证据，正式安装生命周期与自动发现尚待串接，不计为专业指标 T4/T5。

## Business PostgreSQL 受管认证目录

2026-10-20，标准 `make test-monitor-metrics` 在仓库外隔离源码快照通过；快照从当时已提交的 `main` 创建，仅叠加本会话认证目录与对应门禁改动，不包含另一会话尚未提交的 HTTP 质量实现。38 项配置校验覆盖工作区归属、私有权限、链接／硬链接、文件漂移、锁竞争、输入版本、模板更新与中断更新拒绝；实际 Compose 解析确认只读目录挂载、禁止自动创建缺失目录且无模板回退。真实专属 PostgreSQL 分别覆盖原 PGDATA 及新只读目录：容器写入拒绝、宿主原子替换可见、重载后认证／mTLS、新样本及原文件恢复；主机、进程、趋势、六来源总览及资源告警生命周期回归通过，告警事件 2、成功投递 2、HTTP 尝试 3，退出核验当次容器、网络、卷及临时文件零残留。

同轮 `make test-business-config` 与 `make test-build-registration` 通过；既有 Monitor metrics CI 依目录输入声明覆盖新文件及 Business 启动／Compose／环境模板，打包入口递归包含 `scripts/infra/`，不新增 Job 或第二条部署路线。该结果不代表共享工作区的其他并行实现、个人 Business 数据卷迁移、采集角色自动安装、来源自动启停、专业发现／查询页面或 T4/T5 已通过；个人实例未改动。

## PostgreSQL 采集账号安装与来源自动启停

2026-10-20，唯一活动安装版本 `000002_lifecycle.sql` 与产品生命周期基础已随 `3ee46b77fd84fe1a915874a11e0e97b843cac8bd` 入库；本批补齐实库验证、容器完整 ID、挂载无序比较、macOS 路径别名及停止流程的锁内操作，避免把格式差异误判为部署变化。Infra／Business 分别按显式开关安装、停用及重新开启本方账号和受控指标来源；安装期间 NOLOGIN，验证 HBA 更新／重载后才启用 Login，不主动终止未知会话，凭据丢失拒绝自动重置。

- 固定源码完整 `make test-monitor-metrics` 复验通过：基线 `c171574023c7ab02a088cec2616baa1c4cb4284e20` 仅叠加本会话产品和验证文件，未混入共享工作区其他未提交改动。覆盖原 PGDATA 与 Business 只读认证目录、两个作用域的真实安装／SCRAM／mTLS、幂等启用／停用／重新启用、TLS 失败关闭、遗留会话拒绝且不杀连接、凭据丢失保护；包含既有主机、进程、趋势、六来源总览和资源告警回归，事件 2、成功投递 2、HTTP 尝试 3。退出核验当次容器、网络、卷与临时文件零残留，不代表整个共享 HEAD 或正式身份 T4。
- 完整门禁首轮在既有停止进程检查的外层 30 秒期限超时，原始未就绪状态未被保留，原因尚未确认；测试仅补充 JSON 状态诊断，并使内部失败能在原外层期限前返回，不延长期限或改变有效性判定。复验该检查在约 0.02 秒通过，不能据此声称已修复首轮超时根因。
- 最新控制面固定快照的 `make test-infra-runtime-log-lifecycle` 47 项通过，包含 8 项纯生命周期契约；Business 启停从各自 `.env` 读取选择前清除四项 Infra 继承值，验证旧配置／缺配置不会误启用，显式 Business 配置可独立生效，并验证可选来源失败后核心仍运行。`make test-business-config` 与 `make test-build-registration` 同一控制快照通过，构建登记核对 26 个 Go 服务／Worker、59 个镜像。完整指标回归先于这一配置隔离补充；最终来源专项另行核对最终输入。

最终 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=postgres-source` 在同一基线叠加最终源码后通过：39 项配置与 8 项生命周期纯契约、两种认证文件布局及产品启停实库检查全部通过，退出零残留；额外核对每次实际 mTLS 返回的采样时间处于当次请求窗口，不接受旧快照。完成后核对本会话运行／探针文件与快照记录无字节漂移。

所有实际 PostgreSQL 验证均由现有标准门禁管理的 UUID 专属 PG15 执行，不使用个人或共享数据库，不修改个人 `.env`、凭据、卷或运行容器。新开关默认关闭；个人认证目录切换、正式 Prometheus 发现、Monitor 引擎专业查询／页面及 T4/T5 尚未验收，不能把来源部署计为专业监控全部完成。既有 Monitor metrics CI 通过目录／输入登记覆盖产品 SQL、Python、Business 启停与两个新增测试文件；纯契约同步进入既有 Infra 生命周期 T1，不新增 Job。

## PostgreSQL 专业发现、查询与页面

2026-10-21，专业发现与查询主体随共享版本 `d5c39624f` 入库；Swagger 引用修复 `667774d20`，两项 Go 测试夹具修复 `5c0b555e625c51510649543e865c4c2520ac845b`。本批仅覆盖已确认纳管并绑定有效主机的 Infra／Business PostgreSQL，不开放 Tenant 引擎、业务表、SQL 正文或凭据。私有投影、统一 SD 准入、当前 User Owner 读取与返回前重核、十二项即时／四项摘要／当前数据库容器段趋势，以及真实产品页面已经具备；不把数据库整体消耗归算给租户。

- T1：仓库外冻结基线 `25addd430` 仅叠加本会话输入，`make test-module MODULE=monitor GO_ONLY=1`、最终 `make test-module MODULE=common GO_ONLY=1` 与 `make test-module MODULE=system GO_ONLY=1` 分别通过。最终 System 输入覆盖投影补充后的 4 MiB 预算和未来容器启动事实拒绝。两项旧失败分别来自负面服务身份误用 Web Token 类型、准入测试使用未启动的固定端口；修正夹具后全包通过，不改变产品授权条件。
- T2：最终 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=postgres-source` 通过：39 项配置、9 项纯生命周期契约、真实 PG15／Prometheus 查询链路的十二项即时、四项摘要、容器隔离及趋势、统计重置速率断点、SQL 权限撤销时 `up=1` 仍清空、恢复后的新鲜数据，以及两作用域／两种认证布局的安装和启停。项目 `addp-metrics-t2-efa0ddcde25c4641b418f2811b582055` 退出核验当次容器、网络、卷与临时文件零残留。最终运行／探针输入与工作区逐文件字节一致；登记断言补充独立再验通过。未操作个人数据库或服务。
- T3：86 项 Monitor Node 单元通过；首次浏览器 240 项中 238 通过、两项新增场景误匹配 Select 箭头 SVG，修正为精确图表可访问名称。最终 PostgreSQL 专项九项浏览器及构建通过，覆盖双语单位／零值、URL 重载与前进后退、503／403／404／错误主体／SQL 缺样清空、Tenant／Service／Delegated／任一 Owner 权限缺失拒绝。其余 232 项浏览器来自同轮有效输入，不声称最终全量 241 项在同一调用重跑。
- T0：最终 `make test-infra-runtime-log-lifecycle` 51 项通过；Swagger 修复已通过正式生成与 86 个公开方法覆盖校验；部署发现 34 项沿无变化输入复用。既有目录和输入登记覆盖新 PG 集成测试与 Common 私有投影文件，未新增重复 CI Job。

专项首轮专业查询通过，但后续只读 HBA 布局的 Docker 读取返回通用失败，原根因未复现；现增加关闭式退出分类且不回显私有输出，最终该分支通过，不能据此宣称原根因已修复。第二轮初始速率在 95 秒期限尚未完整，整批失败；按既有 SD 30 秒、scrape 15 秒、SQL 5 秒、完整一分钟及余量导出 130 秒就绪预算，保留四样本／epoch／reset／新鲜度条件并加入安全诊断，第三轮通过。首两轮失败不计通过。

正式身份 T4 的新增专业读取及真实 Console 用途已于 2026-10-21 获用户确认，已接入现有 `engine-admission`；其后真实 Hosted 的结果见下文，不能由本地契约计为通过。冻结源码基线 `2e46ea4e0` 的 `make test-node-metrics-online-runner` 通过：115 项契约、Node／Bash 语法及 System 的 Metrics 身份夹具 Go 测试；新增两个采集来源的归属、私有状态、各自端口与 mTLS、清理失败处理一并验证。`make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--source-root <冻结源码> --package repository --test engine-deployments"` 亦通过，使用实际映射和标准 `addp_iam_test` owner 锁，覆盖当前声明、主机有效性与整批失败回滚，不改变个人运行库。


来源生命周期提交 `c828ffd3b` 的 Metrics 作业 `118632764390` 已失败：PostgreSQL 来源分支通过，后续 `TestIntegrationMetricsHTTPAlertLifecycle` 将 Compose DNS `metrics-source` 当作 IP 解析。测试声明和唯一 SD 现统一为 Docker inspect 的真实来源 IP，并为同一一次性进程证书补齐该 IP SAN，Desktop 仍沿既有精确回环传输；不修改产品解析／证书校验或扩大准入。第一次本地复验因尚未同步该 SAN 在 SDK 自检处失败，第二次到达历史趋势前置条件而提前退出（当时未保存状态／组数，不能据此确认根因）；现保持原 45 秒等待及有效失败点要求，仅允许零观察组继续等待自然完整窗口，并保留状态／组数诊断。后续本机专项分别在进程观测前置因 `no_sample` 或 CPU `no_data` 失败，未到达最终 HTTP 告警分支；最终诊断显示两来源 UP=1、身份各一且启动时刻匹配，CPU 一分钟窗口仅两采样、前一分钟边界缺失，不能降低四采样／完整边界条件换取通过，亦未归因为并发或已确认的产品缺陷。失败后各次自有容器／网络／卷均只读核对为零。上述失败不计通过，本批 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=http-quality` 未通过，原生 Linux CI 和新的历史等待分支仍待实际运行。旧 Platform 作业已通过本方 47 项生命周期，后续既有开发生命周期聚合失败未定位为本方根因，不把该作业记通过。生产发布、T5 与七天自然留存未验收。

最终冻结输入的 `make test-module MODULE=monitor GO_ONLY=1` 再次通过（含新增历史等待测试文件的全包编译及全部 Go T1）；Python／Bash 语法与配置登记覆盖有效。Hosted 接入的最终查询参数回归沿 `python3 -m unittest scripts/test/platform-node-metrics-online_test.py` 86 项通过；两个新增指标项目的既有资源拒绝另沿 `scripts/test/online-hosted-node-metrics-gate_test.py` 对应场景验证通过。正式 Hosted 专业读取与页面扩展提交为 `89749f1d6`，调度后按真实结果追加，不能由这些本地契约计为 T4 通过。


2026-10-21 后续收口：`009a5e42598038f37cc0a202edfbe88a49acc7bd` 的 [Platform consistency](https://github.com/pampa0629/addp/actions/runs/39519915079/job/118960943889) 已通过。原 [Metrics 作业](https://github.com/pampa0629/addp/actions/runs/39519915536/job/118961147933) 在后续资源总览阶段被取消，不计整项通过。正式 [Hosted engine-admission](https://github.com/pampa0629/addp/actions/runs/39519991703) 首次在基础设施构建时连接 GitHub 超时，证据上传亦超时；第二次真实运行已通过管理员接入页面，但在 `engine-professional-current-container-queries` 阶段由验收脚本错误拒绝默认 `policy_version=0`，专业页面未执行，整项失败。第二次报告明确为 `cleanup=passed`、`infra_cleanup=zero_residuals`。默认预算未保存时合法版本为零；脚本和负面回归修正已随 `c4ff143d7` 入库，本次冻结该输入的 `make test-node-metrics-online-runner` 为 117 项 Python 契约、Node/Bash 语法及 Metrics 身份 Go 夹具通过，仍不替代真实专业 T4。

[Metrics 后续原生 Linux 运行](https://github.com/pampa0629/addp/actions/runs/39522656609/job/118975847648)（`95a8b2a25678475bdaa65ba07649f10344092916`，attempt 1）已通过真实 HTTP 告警的触发、签名通知及恢复，随后 `TestIntegrationMetricsHTTPRealSource` 在停源清空断言失败。旧日志中前置失败来源检查于 `02:31:00.147 UTC` 放行，Go 检查于同秒执行，但未保存失败 scrape 的原始时间戳，不能据此确证旧失败的唯一成因。代码核对及确定性红绿回归确认夹具存在评估时间竞态：前置检查使用 Prometheus 当前时刻，Go 消费者使用向下取整至秒的时刻，后者可能仍位于失败采样之前。现只把停源前置检查统一到消费者的整秒时间，保留原 65 秒期限、完整窗口、来源身份、对等进程存活和停源数值清空断言；不修改产品 PromQL、TLS、权限或新鲜度。本次冻结源码基线为 `53cc8e07ddd810dd43fccbfd0106279050c3b25a`，仅叠加两项夹具输入；完整 `make test-monitor-metrics` 已退出零：40 项部署配置、9 项生命周期契约、PG 专业来源与两种认证布局、PromQL 固定窗口、真实进程/HTTP 查询及告警/签名通知/恢复、停源 HTTP 和进程数值清空、对等进程存活、原生与 Desktop 受限来源、监控中心 WAL 恢复、六来源总览/TOP5 和资源告警生命周期均通过。项目 `addp-metrics-t2-855a0575dbd145eaacb375b10e0478ed` 的容器、网络、卷及临时文件核验零残留，原始日志 `/tmp/addp-http-stop-boundary-metrics.log`。两项脚本最终哈希与冻结输入一致；共享主机未声明独占，不据本轮测试推导性能 SLA。`python3 scripts/ci/check-t2-ci-registration.py --repository .` 通过，沿既有 Metrics Job 自动登记，无需新增 workflow；冻结副本因没有 Git 元数据而无法运行的首次登记检查不计通过。原生 Linux 新输入 CI 与正式专业 T4 仍待后续真实结果，不重跑旧输入、不恢复定时轮询。


## PostgreSQL 来源准入协议修复

2026-10-22，冻结 `c7753fe38fa55d3d2431d42f4ac49c05c3ba754c` 的 `make test-node-metrics-online-runner` 通过 117 项契约及 Metrics 身份 Go 夹具。随后 [Hosted engine-admission 39680591321 / 119501760697](https://github.com/pampa0629/addp/actions/runs/39680591321/job/119501760697) 通过真实管理员引擎接入页面，在 `engine-professional-current-container-queries` 因 130 秒完整采样窗口未就绪而失败，专业页面尚未执行；外层报告为 `cleanup=passed`、`infra_cleanup=zero_residuals`。运行中发现 HTTP SD 由 200 转为 422，不能把接入页面或来源 UP 计为专业 T4 通过。

确定性实源回归复现了协议缺陷：PostgreSQL 的 Nginx 在缺少客户端证书时返回通用 400，而同一 Monitor `SourcePolicy.Admit` 只认可明确 TLS 证书拒绝或 HTTP 401/403。旧来源 T2 直接接受 400，未经过该准入。现保持 `ssl_verify_client on`，仅将 Nginx 内部证书缺失/校验失败 496/495 映射为 403，拒绝请求不转发上游；不修改 Monitor 准入、TLS 信任、权限、窗口或新鲜度。新增实源 Go 集成由原 `postgres-source` 探针调用，覆盖同一生产准入、无证书及不受信证书拒绝；既有输入登记和 Metrics CI 已覆盖，无新增平行门禁。

冻结上述基线、只叠加本方三项来源/测试输入的标准验证：

- 红阶段 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=postgres-source` 在新实源准入测试明确失败为 `metrics source protection could not be verified`，原始日志 `/tmp/addp-pg-admission-red.log`；退出后另核对项目 `addp-metrics-t2-a0a767df28ca404799e582dc6f33cab7` 的容器、网络、卷均为零。
- 修正后的同一完整入口退出 0：十二项指标/四项摘要/当前容器趋势、统计重置、SQL 失败无旧值、两作用域与只读认证目录、安装/停用/恢复及新增正式准入/证书拒绝均通过。项目 `addp-metrics-t2-e4c94596f14645fd83228e116598b893` 的容器、网络、卷与临时文件清零，随后独立读取三类资源均为空；日志 `/tmp/addp-pg-admission-green.log`。
- `make test-module MODULE=monitor GO_ONLY=1` 全包通过，日志 `/tmp/addp-pg-admission-go-t1.log`；首次因冻结副本无 Git 元数据而未运行模块门禁的调用不计通过，补齐副本元数据后沿同一标准入口重跑。T2 CI 登记校验通过，最终三项输入与冻结验证副本逐文件字节一致。本轮不重跑输入未变的 HTTP/主机 T2，不操作个人数据库或服务。

上述实源红绿证明来源协议修复，尚不能替代新源码的原生 Linux CI 或正式专业 T4；后续结果须按实际执行与退出清理记录。


## 引擎监控的当前承载主机导航

2026-10-22，引擎监控列表与详情增加当前承载主机名称和资源跳转；沿 System 当前有效 `node_id`，只在已有 `platform.host_node.read` 下读取当页主机名称。不回用 `declared_node_id` 或旧部署别名；失效关联明确显示当前无有效关联，名称读取失败只保留当前 ID，不清空合法引擎指标。服务与引擎页面共同使用 Monitor 的 `MonitoredHostLink` 领域组件，删除服务原内联导航；名称加载继续复用唯一 `loadHostReferences` 的去重、四并发和迟到结果保护。跳转使用原 Monitor 导航入口及 push，浏览器返回恢复原筛选、详情、指标、范围和刷新选择；不增加权限、API、后台读取或部署操作。

冻结基线 `1e59fda800edf9ff1e8d33ddccb891d123b765f6`、只叠加本方前端输入的 `make test-monitor-frontend` 退出 0：87 项 Node 单元、266 项确定性浏览器及生产构建通过。新增五项浏览器覆盖中英文详情/主机跳转/原趋势 URL 返回、列表当前名称与改名、名称 403 时指标保留、撤权拒绝迟到名称与后续额外读取、失效关联不回用旧声明；原服务主机名称与撤权回归亦包含在完整门禁。中英文截图已检查，保存在 `/tmp/addp-engine-host-nav-visuals/`；完整日志 `/tmp/addp-engine-host-nav-frontend.log`。成功后原浏览器输出目录与端口租约由标准入口移除。

`make test-frontend-ci-registration` 最终通过 20 项登记回归及 21 个受控前端登记检查；首次因冻结副本没有 Git 元数据而未完成实际登记检查，补齐副本元数据后重新执行才计通过。既有 Monitor 前端 owner/Make/CI 路径自动覆盖，无新增测试路线或 workflow。九项实现/测试输入与验证副本逐文件一致，另核对冻结基线的 8677 个未改跟踪 blob 一致；仅文档摘要随后更新，不以共享工作区其他改动替代验证输入。

本批没有读取或轮询云端 CI、调度 Hosted、恢复定时任务、修改个人数据库或重启服务。上述结果为 T0/T1/T3，不替代 PostgreSQL 正式身份 T4、原生多宿主/多进程或生产 T5，云端结果继续等待专门会话回传。


## PostgreSQL 专业观测正式 Hosted 验收

2026-10-22，CI 专门会话回传 [Hosted 39683888149 / Job 119512936927](https://github.com/pampa0629/addp/actions/runs/39683888149/job/119512936927) 的真实通过结果。本会话独立复核官方制品：源码为 `b08f2331dc6c76b520b958dc5a3d79c054f80900`、工作区干净，System／Monitor／Gateway 三服务均为相同提交；正式平台管理员密码/MFA及无权限对照身份与当次输入一致，独立 Prometheus 身份隔离保持。场景报告为 `stage=complete`、`result=passed`，外层 `cleanup=passed`、`infra_cleanup=zero_residuals`。

当次 Infra／Business 两个单容器 PostgreSQL 均覆盖：发现与批量接入、来源变化与重复拒绝、整批回滚、版本管理与退役；专业观测固定十二项即时、当前页四项摘要、当前数据库容器趋势、真实 Console 页面重载/有效零及单位、身份拒绝、来源停止清空与恢复新样。浏览器证据记录一次双部署摘要、四次即时和四次趋势响应，主体及固定目录完整，已检查 `engine-resources-detail.png`。官方制品在 `/tmp/addp-ci-20261022-manual-followup/artifacts-39683888149/`，协调审计为同目录上级的 `independent-passes-audit.json`；本次复核没有重复运行验收。

结论仅关闭上述提交的 PostgreSQL 专业查询与页面正式身份 T4，不能覆盖其后 `4a0a9ea1d` 新增的主机导航、其他引擎/容器/集群、七天自然留存或生产 T5。先前失败仍分别保留，不将本次通过追溯为旧输入通过。

同时回传的 [Release 39683857764 PostgreSQL 来源 T2 / Job 119520816486](https://github.com/pampa0629/addp/actions/runs/39683857764/job/119520816486) 已失败：新增准入探针的 Python `subprocess.run(timeout=45)` 终止整个 `go test ... -timeout=30s` 命令，日志出现依赖下载而未见 `=== RUN TestIntegrationMetricsPostgreSQLAdmission` 或 Go 测试自身超时报错。外层期限包含下载、编译与执行，不能把该结果当作 mTLS 断言失败，也不能据温缓存本地通过解释冷构建耗时的唯一原因。原始日志 `/tmp/addp-ci-20261022-manual-followup/job-119520816486.log`。若后续仍复现，先分清构建准备与实际测试期限，保留原测试超时和准入断言；不直接放宽等待。当前已有自动复验由专门 CI 会话跟进，本会话未查询、重跑、取消或设置定时；该新增 Linux T2 仍不计通过。


## PostgreSQL 准入探针的构建与执行期限

2026-10-22，CI 专门会话再次回传 [4a0a9ea1d 的 PostgreSQL 来源 T2 / Job 119557101259](https://github.com/pampa0629/addp/actions/runs/39696991326/job/119557101259) 失败。它与前次 `b08f2331d` 失败使用完全相同的准入探针，均由外层 45 秒终止包含依赖下载、编译和执行的 `go test`，日志未进入准入测试。现将同一正式准入分为私有目录 `go test -c` 准备和直接执行两个阶段：每次准备最多 180 秒，实际测试仍为 Go 30 秒、外层 45 秒；构建失败立即退出，不执行遗留文件，执行超时不重试、不计成功。产品代码、mTLS 证书拒绝、权限、采样窗口、新鲜度及 Job 20 分钟上限均未改。

冻结基线 `0cad0ddf334524fa31295b167389002b0edd9e1f`，只叠加本方输入，沿标准 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=postgres-source` 执行红绿验证。新增确定性回归用模拟的 70 秒冷准备及 1 秒执行复现旧计时缺陷（无实际等待），并覆盖构建失败禁止执行旧文件、真实执行超时保持失败；红阶段为 2 项失败、1 项错误，停在配置检查，日志 `/tmp/addp-pg-admission-budget-red.log`。修正后同一完整门禁退出 0：45 项配置回归、9 项生命周期契约，以及 Infra、Business、只读认证目录三次真实正式准入均通过；十二项专业指标、四项摘要、当前容器趋势、统计重置、SQL 失败/恢复、只读认证目录的原子更新/重载/还原和安装/停用/恢复生命周期亦通过。日志 `/tmp/addp-pg-admission-budget-green.log`，末尾确认容器、网络、卷及临时文件零残留；随后独立读取精确项目 `addp-metrics-t2-54b61b59556949878eed8c39bd039590` 的三类 Docker 资源均为空。

两个最终脚本与成功验证副本逐字节一致；T2 CI 登记检查通过，既有根 Make、输入登记及 Release 分组已覆盖，无新增工作流或平行门禁。本地真实 T2 使用现有依赖缓存，冷准备预算由确定性回归覆盖，不代替新输入在原生 Linux 的冷构建结果；该项继续等待专门 CI 会话回传。本轮不重复专业 Hosted 已通过验收，不自行查询、重跑云端 CI、设置定时或操作个人服务。


## PostgreSQL 专业来源固定版本 Linux T2 复核（2026-10-24）

专门 CI 会话回传 [Release 40048425338 / job 120741235374](https://github.com/pampa0629/addp/actions/runs/40048425338/job/120741235374)，固定源码 `5d91fa31c326fc636c73810edb542ae7f95e4db0`、attempt 2，由协调会话对原 push 的失败 Job 进行唯一一次定向复验。本会话独立核对原始日志、固定脚本与关键测试断言，没有重新运行或查询／调度 CI。实际 `Verify isolated metrics lifecycle` 执行 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=postgres-source`，51 项准备测试通过（1.118 秒），进入真实 PostgreSQL、mTLS、Prometheus 及查询／生命周期阶段，最终该分组 selected／success。

覆盖十二项原生专业指标、四项摘要、当前数据库容器段趋势、启动边界／外来身份拒绝、统计重置留空及 SQL 失败清空／新样恢复；原始日志含四次 `TestIntegrationMetricsPostgreSQLSource` 和三次 `TestIntegrationMetricsPostgreSQLAdmission` 实际 PASS。Infra／Business 两个作用域均验证固定函数与受限采集身份、mTLS／受限 HTTP、权限拒绝、密码轮转及幂等；Business 另验证维护账号 SCRAM、其他库／socket 拒绝与原管理员规则保留。两作用域账号安装、NOLOGIN、重新启用、已有未知会话拒绝和凭据丢失拒绝通过，仓库外认证目录只读挂载、宿主原子更新、真实 reload 与完整还原亦通过。脚本最后明确报告当次 owned 容器／网络／卷及设施临时文件零残留，不能只以摘要或 cleanup 成功替代这些实际执行断言。

原 [job 120735545333 / attempt 1](https://github.com/pampa0629/addp/actions/runs/40048425338/job/120735545333) 仍记失败：51 项准备中 `test_business_postgres_compose_mount_is_readonly_and_requires_preparation` 的 `docker compose ... config --format json postgres` 在原 15 秒期限超时（共 50 项通过、1 项错误），尚未执行产品采集阶段。原准备测试冻结源码与同 SHA Git 源码逐字节一致，复验门禁脚本亦与该 SHA 一致；本次成功不回写首轮为通过，也不据此断言暂态环境的唯一根因已确认。未修改断言、期限或权限来取得通过。

证据位于 `/tmp/addp-ci-20261024-020017/`：`job-120741235374.log` SHA-256 为 `91633f44e24988756d38ea144d46d6f7f07c9144114aee45add85ce38622f9c3`，`monitor-metrics-gate-frozen.sh` 为 `95cd991422623e4cad7cb193bd4337181af62085b295efb0ab7abe8be1ba2626`，已用哈希及 `git show` 精确比对。`Monitor-PG-5d91-retry-actual-T2-pass.json` 为回传审计，`Monitor-PG-5d91-domain-audit.json` 保存本次领域核对；首失败原始日志与审计保留在 `/tmp/addp-ci-20261024-013016/`。本次只证明该固定版本的 PG source 实源 T2，不代表当前共享 HEAD 全批、七天自然留存、容量或新的 Online T4；既有独立版本专业 T4 按其原证据保留。未操作个人服务／数据库、未设置定时，待确认的容器配额语义及采集范围未实施、未计为已授权。

## 观测身份投影 PostgreSQL 断言误报修复（2026-10-23）

CI `39775896930`／System IAM Job `119824550350` 的原始失败是 `private field leaked: 5433`。旧用例对整个 JSON 搜索端口数字，允许投影的随机 UUID 同样可能命中；原 CI 没有保存当次完整快照，不能还原具体命中的身份。将测试主机固定为合法 UUID `54330000-0000-4000-8000-000000000001`，沿标准 PostgreSQL 入口稳定复现相同误报。现改为解码 JSON 后比较完整预期结构和字段值，包括节点、进程、引擎、来源键及无专业来源时的 `metrics.state=unavailable`；任何额外字段仍拒绝，不删除私有字段的否定验证，不修改产品代码、IAM、投影或授权范围。

已先运行 `bash scripts/infra/status.sh` 核对当前共享 PostgreSQL 为 `25432`。在 `addp_iam_test` 使用标准 `make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package repository"`：旧断言加固定 UUID 的红色复现失败；修正结构预期后通过；随后仅在当次测试输入临时注入嵌套 `source_key.published_ports=[5433]`，同一入口按完整结构断言明确失败，证明泄露仍被拒绝；删除临时注入后最终重新执行，7 项 Repository 顶层 T2 与测试库重置均通过、退出 0，无跳过。最终文件与注入前通过版本逐字节一致。原始日志 `/tmp/addp-observability-projection-red.log`、`/tmp/addp-observability-projection-mutation.log`、`/tmp/addp-observability-projection-final.log`；中间首次结构预期遗漏已有指标状态且错误强制启动时刻为 UTC 的失败已修正，不计产品故障。

本轮仅修改既有测试和此证据，现有 Release/T2 System IAM Job 自动发现，无新增测试入口、依赖或 CI 编排。完整 System IAM CI 等专门会话回传，不把本地 Repository 分包 T2 计为全批 CI；未调度或查询 CI／Hosted、未设置定时、未重启个人服务。Safari 实际弹窗可见性问题仍未解决。


## 个人环境 Infra PostgreSQL 接通与跨模块入口修复（2026-10-29）

用户限定批准先启用 Infra，并在本人终端执行 `bash scripts/infra/up.sh --metrics` 和 `./scripts/dev/restart.sh -monitor`。本会话随后只读核对实际来源与发现快照，未自行启停服务。独立采集账号和固定汇总函数、专属认证规则、SQL Exporter 与 mTLS 入口已由标准生命周期接通；Monitor 独立客户端读取 `https://127.0.0.1:19109/metrics` 返回 200、16 条固定采样，Infra 快照为 available。证书及个人配置只保留仓库外／忽略文件，不进入提交；Business 仍为 not_deployed，未启用其采集。

已有显示名“infra-虚拟机”的部署，其完整数据库容器 ID 实际匹配 Business 发现清单，不能按别名判定 Infra。本轮保留原记录不变，使用 System 正式自动发现只勾选 Infra PostgreSQL，确认绑定既有 Docker Desktop Linux VM 后接入为 `6b262aad-da10-4f00-8fd2-421465e84189`。Prometheus 来源由 6 增至 7，唯一 engine 来源与新部署及当前数据库容器身份精确对应，health=up 且无 lastError。脱敏只读回执 `/tmp/addp-pg-infra-runtime-receipt.json`。

Safari 使用真实平台管理员，前台详情实际显示十二项专业指标、有效零和单位：连接总数约 45—46、最大连接数 200、数据库容量 5.96 GiB；完整一分钟形成后事务提交约 140—150 次/秒、回滚 1.00 次/秒、死锁 0.00 次/秒。五分钟事务趋势有实际曲线，悬停显示时间及 144.07 次/秒；这些为本地真实运行证据，不代表业务表读取、业务吞吐或新增 Hosted 通过。

实际点击 System“查看监控”复现了模块内导航误用：System Router 将 Monitor 路径解析到 forbidden，Console 地址仍停在 System。本轮改为 `openConsoleRoute()`，由唯一共享引擎路由构造器同时服务 System 跨模块入口和 Monitor 页内切换；不改权限、后台接口或采集语义。Safari 修后点击已进入正确 Monitor 部署地址；中间窗口再次报告隐藏，未将隐藏后的空态误计为来源故障。用户重新激活前台后，再从 System 的 `search=Infra` 列表进入：十二项即时值和趋势恢复、自动刷新开启，连接数 47、事务提交 148.87 次/秒、容量 5.97 GiB；观察到趋势评估结束时间从 23:37:26 自然推进至 23:37:41。点击 Safari 工具栏的浏览器返回，地址、搜索框 Infra 与两条原部署记录实际恢复，未出现 forbidden。

本轮验证沿现有标准入口，使用共享开发工作区，不声称冻结 SHA 或全量干净环境：

- 修前 `npm --prefix console/frontend run test:e2e -- e2e/engine-monitor-navigation.spec.js --grep zh-cn` 在正确跨模块地址断言失败；日志 `/tmp/addp-pg-engine-navigation-red.log`。修后同文件三场景通过，覆盖中英文真实 Console／System／Monitor iframe、专业 API 读取、Back 搜索恢复、无权限不显示入口；只允许既有刷新会话 POST，不发生管理写入。首次绿阶段将刷新会话错误算作管理写入，修正测试分类后通过，未改变产品认证。最终日志 `/tmp/addp-pg-engine-navigation-green-final.log`。
- `make test-common-frontend`：302 项通过、无跳过，含共享路径、编码、查询拷贝、非法形状和唯一所有权；日志 `/tmp/addp-pg-common-frontend.log`。
- `make test-console-frontend`：219 项单元、268 项浏览器及构建通过；日志 `/tmp/addp-pg-console-frontend.log`。
- `make test-system-frontend`：177 项单元通过，浏览器 255 通过、1 失败（`engine-data-authorization.e2e.js` 的权限批量操作场景），因此完整门禁失败且其后构建未执行；本轮没有修改该场景及权限实现，不扩展修复范围。另沿标准 `npm --prefix system/frontend run test:e2e -- e2e/engine-deployments.e2e.js` 通过全部 13 项；`npm --prefix system/frontend run build` 独立通过。日志 `/tmp/addp-pg-system-frontend.log`、`/tmp/addp-pg-system-deployments.log`、`/tmp/addp-pg-system-build.log`，不把专项通过记作完整门禁通过。
- `make test-monitor-frontend`：131 项单元、513 项浏览器及构建通过、无跳过；日志 `/tmp/addp-pg-monitor-frontend.log`。
- `make test-frontend-ci-registration`：27 项登记回归和 21 个前端登记检查通过；原入口和 CI 自动发现已覆盖，没有新增并行路线。日志 `/tmp/addp-pg-navigation-registration.log`。

本轮未调度、查询或重跑 CI／Hosted，未设置定时任务。云端证据仍等待专门会话回传；本地真实环境接通不替代新输入的 Hosted 或生产验收。

## Business PostgreSQL 认证文件幂等发布与 Desktop 挂载故障（2026-10-30）

用户授权 Business 专业采集后，在本人终端沿标准入口重建 PostgreSQL，保留 `business_postgres_data`，并应用仓库外只读认证目录。数据库健康，固定汇总函数可在只读事务内以采集角色调用；来源安装失败后采集角色为 NOLOGIN，尚无 Business 采集容器，不能记为采集接通。

实际故障位于宿主认证文件原子替换后的容器读取：宿主文件存在且内容正确，容器 `stat` 能返回大小，但 `head`／原 `wc` 打开同一路径返回 ENOENT。同字节原子重发亦可复现，15.144 秒的有界观察仍不可读，不能以“短暂延迟”认定重试足以修复。最终不保留等待／重试，也不改变目录挂载、权限、认证基线或服务启动边界。

修正同一发布器的幂等性：已有内容与拟写内容相同时，先完成所有者、权限、基线及预期版本核验，再保留原文件和 inode；真实变化仍用唯一原子替换。原代码运行新增契约时明确失败于“不应重新发布未变策略”，修正后验证同内容保留 inode／mtime、旧版本与错误权限仍拒绝。当前个人认证文件执行同内容核验后，inode、mtime 未变，容器完整读取与宿主字节一致；此证据只证明幂等更新，不证明真实变更后的 Desktop 挂载一致性。读取另改为有界流式读取，保留 256 KiB 上限及符号链接／非普通文件拒绝。诊断只发布固定阶段、封闭原因及清理失败标识，清理错误不覆盖首个失败，不输出 HBA、SQL、密码或原始子进程错误。

验证与边界：

- `make test-infra-runtime-log-lifecycle` 退出 0，最终 108 项中包含 17 项 PostgreSQL 生命周期回归；涵盖真实临时文件／FIFO／符号链接、读取上限、幂等更新的版本／权限检查及诊断脱敏。日志 `/tmp/addp-business-pg-lifecycle-final.log`，幂等契约红色复现 `/tmp/addp-business-pg-noop-red.log`。
- `make test-t2-ci-registration` 退出 0，74 项回归及既有服务登记检查通过。现有标准入口、Release 的 Monitor metrics infrastructure 与 Platform 生命周期检查已覆盖修改路径，没有新增 CI 编排或旁路入口。日志 `/tmp/addp-business-pg-registration-final.log`。
- 完整 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=postgres-source` 首轮失败于测试按工作区统计来源时误纳入个人 Infra，已改为只核对本轮项目；第二轮失败于当次 `discovery.json` 的共享挂载 ENOENT，无法确认目标退出发现，未计通过。日志分别为 `/tmp/addp-business-pg-source-diagnostic-gate.log`、`/tmp/addp-business-pg-source-fixed-gate.log`。
- `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=postgres-observation` 也在真实修改认证内容后的容器文件读取失败，后续版本保护拒绝覆盖，未计通过；日志 `/tmp/addp-business-pg-authentication-final.log`。本地两项 T2 的 Desktop 共享挂载缺口仍保留，不用 T1 或当前同内容读取代替。
- 三轮专属测试项目的容器、网络、卷独立查询均为零，个人 Infra 来源未停止。摘要 `/tmp/addp-business-pg-atomic-diagnosis.json`。未自行重启个人数据库或启动采集服务；真实 Business mTLS 快照、自动发现及页面仍待用户标准入口应用后复验。CI／Hosted 等专门会话回传，不设置定时任务。

### 用户标准启动后的 Business 实源接通

用户在本人终端再次执行 `bash business/scripts/start.sh -postgres`，报告 `source deployed`。随后只读核验数据库仍为同一容器 `e2c3d4d1cc9f9e8b11a5b504b28e65a1ac27446464474fbc6fa2a493c0e3425c`，启动时刻 `2026-10-30T02:10:45.211369878Z`、原数据卷及只读认证目录保留，未再重建数据库。Business 独立来源两容器运行，唯一回环端口 `19110`；独立准入客户端 mTLS GET 返回 HTTP 200，固定 16 项原生样本齐全，采样时间距读取约 0.005 秒。总连接数 0、最大连接数 200、数据库容量约 78.33 GiB，累计回滚 1、死锁 0；这些是整体数据库指标，不读取业务表或将用量归算租户。无客户端证书的同地址请求返回 403；采集角色为 LOGIN、非超级用户、无角色成员关系，认证解析错误为零，原有业务认证基线字节保持一致。

Business 部署发现 v2 自动变为 available，专业来源端点、当前数据库容器 ID／启动时刻与真实容器精确一致。Prometheus mTLS API 显示该目标 UP、抓取错误为空，自动沿原平台部署 `9e4fd2b0-3b45-473e-b070-cc6f92e40e11` 接入，没有重复创建部署；当前外部目标 8 个、加中心自身共 9 个，未增加目标预算。受限到该部署／容器身份的原始时序读取显示最近一分钟已有 4 个采样，提交／回滚／死锁累计量均可读。摘要为 `/tmp/addp-pg-business-runtime-receipt.json`，这只计个人环境来源及发现实链，不替代此前失败的 T2、当前 User 专业 API 页面或新的 Hosted。

Safari 单独打开 Business 验证标签后内容区空白。进一步以禁用环境代理的直接本机 HTTP 核对：Monitor 后端 `/health/ready` 为 200，前端 `5179` 无监听、Console 模块代理为 500；`logs/monitor-frontend.log` 保存 Node `read EIO`／TTY 退出，因此尚未认定页面通过。已请用户通过 `bash scripts/dev/start.sh -monitor` 恢复缺失前端，已有服务复用；本会话未自行启动／重启个人服务，仍待页面复验。未运行新的 CI／Hosted 或设置定时。

## PostgreSQL 私有来源投影 T1 的短数字碰撞修复（2026-10-30）

CI 专门会话回传 [Actions 40984761423／Job 123833070594](https://github.com/pampa0629/addp/actions/runs/40984761423/job/123833070594)，源码 `3a004a94759f0414030d99c992e057c2651c2cb5` 的 System Go T1 在 `TestEngineMetricsPrivateProjectionDoesNotChangeAdmissionOrBrowser` 报 `private source leaked into browser`。原断言搜索整个公开 JSON 的短字符串 `19443`，它恰好出现在该次合法公开指纹 `06e61ece304ab35633d28c1f97252ab7a131b4fcd9346360db5b194430ecd96b` 中。独立核对生产发现读取器和 DTO 与失败 SHA 逐字节相同，读取器仍清除私有来源；没有以本次误报证明真实泄漏。

测试保留当前新鲜的发现时间，仅将容器启动时间固定为失败现场的 `2026-10-30T00:29:17Z`，并锁定完整碰撞指纹；原短数字断言因此在标准入口稳定报出相同失败。修正为完整公开投影值比较、逐作用域检查内存中的私有指针及 JSON 私有字段，并拒绝完整私有端点；保留后台来源正确性、快照启动边界、外来 Docker 身份、可选来源身份错误和旧协议拒绝。只修改此测试，未改生产 DTO、发现路由、权限或采集断言。

在当前共享工作区，以已有 Node 24.20.0 的 bin 临时加入测试命令 PATH 后，`make test-module MODULE=system GO_ONLY=1` 红阶段仅该碰撞断言失败，绿阶段完整 System Go T1 退出 0。最初使用默认 Node 25 的调用被仓库新版 Node 24 前置检查拒绝，未进入 Go、不能记为碰撞复现；没有改全局 Node 或依赖。红／绿日志 `/tmp/addp-engine-metrics-privacy-red.log`、`/tmp/addp-engine-metrics-privacy-green.log`。另用 Go overlay 在 OS 临时目录单独向测试结果注入第二作用域私有指针、私有 JSON 字段和完整私有端点，三次同一标准入口均命中各自拒绝断言；共享测试文件及生产文件未改写，记录 `/tmp/addp-engine-metrics-privacy-audit.json`。现有 Go T1 自动发现及自然 CI 已覆盖，无需新增入口；未运行 T2／Hosted、未操作个人服务、未轮询 CI 或设置定时，自然 CI 结果继续等待专门会话回传。

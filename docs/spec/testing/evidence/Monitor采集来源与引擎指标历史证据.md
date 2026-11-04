# Monitor 采集来源与引擎指标历史证据

[返回 Monitor 历史证据](Monitor历史证据.md) · [返回验收证据索引](../验收证据索引.md)

本专题收录PostgreSQL、Redis及MinIO账号、来源准入、引擎身份与专业观测。固定源码、环境、失败记录、修复结论与未覆盖范围以各记录为准；历史整理本身不代表重跑验收或刷新CI状态，不推导当前HEAD或生产T5已通过。跨主题语境与原有顺序见 [Monitor 历史证据入口](Monitor历史证据.md#原记录入口)。

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

CI 专门会话随后回传源码 `cf39ab99fc6dd1e5a022d87dd3c81230a88c9add` 的自然 [Platform CI Go 作业 123880180441](https://github.com/pampa0629/addp/actions/runs/40999452791/job/123880180441) 为 success。本会话只读核对其原始日志 `/tmp/addp-ci-20261030-043005/job-123880180441.log`：检出 SHA 与上述修复一致，第 799 行为 `ok github.com/addp/system/internal/service 1.502s`。据此关闭本项确定性投影误报的自然 Go T1 缺口；原失败记录保留，不据此推定整个 Platform CI、Desktop 真实认证变更 T2 或 Hosted 已通过。未查询或重跑 CI，未操作个人服务。

## 个人环境 Business PostgreSQL 专业页面与趋势复验（2026-10-30）

用户恢复 Monitor 后，本机直连核对 Console 的 Monitor 模块页面为 HTTP 200、Monitor Ready 为 200；Business 来源仍有全部 16 项固定原生样本，读取时距采样约 0.004 秒。本轮通过原生 macOS Safari、真实已登录平台系统管理员，进入原部署 `9e4fd2b0-3b45-473e-b070-cc6f92e40e11`，没有新增或修改主机、部署、规则或采集配置。该部署旧显示名称仍为 `infra-虚拟机`，实际 Business 身份沿前述精确来源关联，不按名称猜测；本轮未改名。

真实列表同时显示 Infra 与 Business 均“正在采集”，Business 四项摘要为总连接 0、活跃连接 0、每秒提交约 0.62、容量 78.3 GiB。详情实际读出全部十二项：七项连接／等待指标均为有效 0，最大连接数 200，提交速率随自然采样约 0.53—0.62 次/秒，回滚及死锁速率均为有效 0.00 次/秒，容量 78.3 GiB；承载主机为既有 Desktop Linux VM。页面明确说明采集事务不等于业务吞吐、整体容量不归算租户。

未点击手动刷新时，五分钟趋势结束时间从 12:31:26 自然推进至 12:31:56，符合当前十五秒刷新选择。通过真实指标下拉框切换到容量，五分钟容量曲线在截图中实际可见，纵轴为 GiB、曲线约 78.3，评估窗口为 12:33:33—12:38:33；切换一小时后，容量趋势返回 11:44:55—12:44:55、十五秒评估间隔。再通过下拉框切回每秒提交，保持 `range=1h`，实际截图显示 11:46:07—12:46:07 的波动曲线及“次/秒”单位。最初趋势超出 Safari 可见窗口；经原生窗口缩放与滚动后实际看到曲线，没有修改产品 CSS 或以仅存在 AX 图像节点当作绘图通过。

随后用 Safari 原生整页刷新，Console 与模块地址均恢复同一 Business 部署及 `range=1h`，所选指标仍为每秒事务提交；十二项即时值重新读出，趋势返回 11:47:02—12:47:02，无 forbidden／空白。摘要 `/tmp/addp-pg-business-page-verification.json`；实际操作与截图保留在本会话。本轮仅增加运行证据，最小文档检查为 `git diff --check`，没有产品改动需要重跑构建。未在本轮新验悬停、七天自然留存、跨数据库重建／统计重置或撤权；不替代此前失败的 Desktop 实源 T2，也不认定新的 CI／Hosted 通过。个人服务均由用户操作，CI／Hosted 继续等待专门会话回传，未查询、重跑或设置定时。

## Business 部署改名与专业趋势时间缩放（2026-10-30）

用户明确同意将 Business 旧显示名称改为 `Business PostgreSQL`。本轮在真实 Safari 的 System 部署详情中，只修改既有部署 `9e4fd2b0-3b45-473e-b070-cc6f92e40e11` 的显示名称，保存成功后版本由 1 变为 2；点击“重新读取并核对”仍返回新名称、启用状态与原 Desktop VM 承载声明。关闭详情后列表保留 Infra／Business 两条独立部署；从改名后行的“查看监控”进入 Monitor，地址仍为同一部署 UUID，标题采用新名称，正在采集、最大连接数 200、容量 78.3 GiB 仍可见。整页刷新后新名称保留，没有重建数据库或采集来源。

专业趋势接入与主机、服务相同的共享 ChartRenderer 缩放能力，只增加页面内临时视口；不新增指标、API、权限或查询，不改固定样本、单位、缺样和容器运行段。查询成功的刷新保留相对视口，切换部署／指标／时段或身份及查询失败时重置；不持久化视口。规范先同步至运行观测规范 6.3，页面复用现有翻译，不新增第二套图表或交互实现。现有 `make test-monitor-frontend`、Playwright 自动发现及 Platform CI 的 Monitor 前端登记覆盖这些输入，无需修改共享入口或 workflow。

新增三项浏览器用例覆盖中英文拖动、零值／缺样与原始行不变、缩放／图例不增发请求、主题重绘及手动／自动刷新保留、指标／时段／返回列表／身份切换重置、仅趋势失败仍保留有效卡片但清空图及视口、整页读取失败和撤权清除。最初专项因 Element Plus 的只读输入被可见选中标签遮挡而失败，已改为点击同一真实选择器外框，专项三项通过；后续完整回归加载了英文按钮修正前的用例，已明确中止并清理该轮隔离入口，未记作通过。

最终输入下，使用已有 Node 24.20.0 的 bin 临时加入 PATH，执行 `make test-monitor-frontend`：确定性测试 131 项通过；浏览器 516 项中 511 项通过，本次引擎全部用例通过，另五项分别在 HTTP 机器身份、采集配置撤权、主机 inode、英文告警筛选及进程比较中超时，整条 Make 门禁退出非零、构建未被执行，不能记作整体通过。沿同一标准 `npm --prefix monitor/frontend run test:e2e` 入口，仅筛选上述五项、保留原断言与超时独立复验，五项全通过（17.3 秒），没有修改对应页面／测试、共享图表或超时预算；随后 `npm --prefix monitor/frontend run build` 通过。日志分别为 `/tmp/addp-engine-zoom-monitor-frontend-final.log`、`/tmp/addp-engine-zoom-five-timeouts.log`、`/tmp/addp-engine-zoom-build.log`；保留完整回归的五项原失败，不把局部复验改写为原整轮通过，超时根因未确证。两轮来源均为受控浏览器 T3，非真实来源正式身份验收。`/tmp/addp-engine-zoom-inputs.sha256` 核对页面、引擎用例及夹具、共享 ChartRenderer 输入未变化，`git diff --check` 通过；原中止轮及最终两轮的隔离 runtime 目录均已清理。改名运行摘要 `/tmp/addp-business-rename-and-engine-zoom.json`。

Safari 已加载新缩放提示与实际提交速率曲线，但本轮截图存在窗口可见区域缺失，尚未取得原生浏览器真实拖动成功证据，不以 AX 节点存在代替交互通过。CI 与 Hosted 继续等待专门会话回传；此前 Desktop 来源／认证变更 T2 的失败不因改名或受控图表回归而消失。

### Safari 原生拖动复验的独立进展（2026-10-30）

后续按用户要求复验同一 Business 部署的趋势缩放。在原生 Safari 进入全屏并激活本会话第二个标签页后，已能实际看到完整曲线及底部滑块；页面显示新名称、正在采集及十二项指标。通过真实刷新间隔下拉框选择关闭自动刷新，URL 保留同一部署并增加 `refresh=off`。随后原生拖动左侧缩放手柄，截图可见选中区域从完整时段收窄至约右侧一半：查询评估窗口仍为 16:17:48—16:22:48，曲线可见横轴变为 16:20:18—16:22:48，提交速率曲线随之放大。此项计为个人 Safari 真实拖动通过；不由截图推定请求数量或全部样本性质。

准备手动刷新时，最新可访问状态已切换到第一标签页的 `/monitor/resource-overview`，并退出全屏；该次刷新不属于 Business 引擎页，明确排除，不能证明引擎视口保持。已暂停输入并请求用户确认临时独占本会话标签页，手动／自动刷新保持及指标切换重置仍未取得本轮原生证据。摘要 `/tmp/addp-engine-zoom-safari-receipt.json`，原生截图与操作记录在本会话；本次只补文档证据，`git diff --check` 为最小检查，没有修改产品、服务、身份或采集配置，不重跑 CI／Hosted、不设置定时任务。前轮受控浏览器通过与完整回归五项超时仍分别保留，不由本次个人拖动覆盖。

### Safari 刷新保持与指标切换重置复验（2026-10-30）

用户随后明确允许本会话短暂独占 Safari。只切换第二个标签页，恢复同一 Business 部署的 `refresh=off` 地址；输入前读取原生可访问状态核对 Console 地址、部署 UUID 和标题。首次完整窗口 17:31:18—17:36:18，实际拖动左手柄后，截图可见横轴变为 17:33:48—17:36:18，滑块约右半段。首次手动刷新只观测到旧值清空、新结果未完成，未计通过；恢复十五秒自动刷新后数据重新可见，Monitor 就绪接口为 200，但不据此确定前次未完成的根因。

自动刷新实际观测到结束时间从 17:43:55 推进到 17:44:25，期间没有手动刷新输入；后一次完整查询窗口为 17:39:25—17:44:25，截图可见横轴 17:41:55—17:44:25，右半段视口保留。随后在已核对的 Business 页真实点击“刷新”，加载完成时完整查询窗口为 17:41:06—17:46:06，截图可见横轴为 17:43:36—17:46:06，滑块仍约右半段。以上分别证明个人原生 Safari 的自动、手动刷新后相对视口保持，不将首次未完成或前一轮错误标签页刷新改记为通过。

再通过实际指标下拉框，从每秒事务提交切换到数据库总容量；Console URL 更新为同一部署及 `metric=postgresql.database.total_bytes`，截图实际看到容量曲线约 78.3 GiB，完整查询与可见横轴均为 17:44:10—17:49:10，滑块恢复全范围，证明指标切换重置。页面最终保留默认十五秒刷新及该容量指标，第一标签页未改动。摘要 `/tmp/addp-engine-zoom-safari-receipt.json` 已逐项更新，截图与实际操作保留于本会话；本轮拖动、手动／自动刷新保持、指标切换重置的个人 Safari 缺口关闭。未验其他时段、部署切换、撤权、长期或 Hosted，不外推完整 CI 或旧 T2；仅补证据及能力状态，`git diff --check` 通过，不需重跑产品门禁，未启停个人服务、修改权限或设置定时任务。

## 引擎监控原位刷新（2026-10-30）

用户反馈十五秒刷新时整页闪动。定位到 EngineResources 每轮 reload 在发请求前清空 owners／observed／history／主机名称，再以 loading 遮罩覆盖内容，趋势随条件消失而销毁重建；原生整页导航并非每轮刷新路径。按规范先明确同一上下文请求在途保留、成功原位更新；首次加载保留反馈，部署／指标／时段／列表范围／身份变化清空，隐藏取消和失败关闭保持原口径。只组合原请求代数、自动刷新及共享 ChartRenderer，不新增接口、权限、依赖或共享刷新实现。刷新间隔变化不清空同一数据上下文；顶部按钮显示加载状态，页面 aria-busy 标识在途。趋势独立失败立即清除旧图，保留本次合法即时值。

新增五项 T3：列表与详情分别在手动／自动刷新时有意阻塞响应，确认原读数、分页和画布不消失、无整块遮罩，释放后容量从 1.00 更新到 2.00 GiB；详情确认同一 canvas 节点前后保持连接。另验等待期间撤权取消并清除，旧响应返回不得复活。修复前详情专项实际因等待中容量变为“—”失败（`/tmp/addp-engine-inplace-red3.log`）；修复后标准 `npm --prefix monitor/frontend run test:e2e -- engine-resources.spec.js` 的全部 25 项通过（1.2 分钟），含原错误、SQL 快照失败、趋势失败、权限／主机失效和缩放回归（`/tmp/addp-engine-inplace-targeted.log`）。

本批 T1／T3及构建由原 `make test-monitor-frontend`、Playwright 自动发现和 Platform CI 已登记的 Monitor frontend 作业覆盖，无需修改 workflow。最终使用已有 Node 24.20.0 的 bin 临时加入 PATH 执行 `make test-monitor-frontend`，退出 0：T1 全部 131 项、T3 全部 521 项（22.1 分钟）及生产构建（6.56 秒）通过；日志 `/tmp/addp-engine-inplace-full.log`。`/tmp/addp-engine-inplace-inputs.sha256` 核对本轮页面／引擎用例／夹具在专项和完整门禁之后未改变，`git diff --check` 通过，标准浏览器入口的当轮隔离 runtime／成功输出目录已清理。本轮完整通过不改写前轮超时原记录，也不代表 backend、来源 T2、CI 或 Hosted 通过。Safari 只读检查仍报告页面隐藏、自动刷新暂停，故本次原位刷新尚未取得个人 Safari 连续可见证据，不回用前一版本的缩放验收代替。 随后只读核对当前开发前端：Console 普通组件地址和直连 Monitor 的同一路径仍返回清空旧值的旧实现，而同一路径带诊断查询参数可返回新上下文逻辑与 aria-busy；两种来源已分别保存于 `/tmp/addp-engine-inplace-served-recheck.vue.js`、`/tmp/addp-engine-inplace-served-public-query.vue.js`。这仅证明普通转换结果尚未更新，未确证缓存失效的更深层根因；不添加产品 cache-busting 路径，已请用户执行标准 `./scripts/dev/restart.sh -monitor` 后再验真实页面。未重启服务、修改个人数据库或权限，CI／Hosted 继续等专门会话回传，未查询、调度或设置定时任务。

### 用户重启后的个人 Safari 复验（2026-10-30）

用户执行标准 Monitor 重启并将 Safari 保持前台后，Console 普通组件地址（无诊断参数）实际返回新 dataContext 原位更新逻辑和 aria-busy，旧普通转换结果不再阻碍验证；源码响应保留于 `/tmp/addp-engine-inplace-after-restart.vue.js`。本轮只使用原生 Safari 第二个 Business 标签页、既有已登录平台系统管理员；地址为同一部署和数据库总容量，默认十五秒自动刷新，没有操作第一标签页、修改登记或采集配置。

真实页面显示正在采集、最大连接数 200、容量 78.3 GiB，卡片及实际容量曲线完整可见。原生拖动滑块收窄至右半段；真实点击“刷新”后的查询窗口为 19:59:31—20:04:31、可见横轴 20:02:01—20:04:31，读数与曲线保留且视口未重置。此后没有手动刷新输入，自动更新窗口从 20:00:03—20:05:03 推进到 20:00:33—20:05:33；后一次截图的可见横轴为 20:03:03—20:05:33，滑块仍在右半段，卡片和容量曲线保持可见。由此补齐新版本在个人 Safari 的手动／自动刷新结果与视口保持；未捕获原生请求在途帧，不以离散截图声称逐帧零闪、画布身份或长时间连续性，这些在途保留／同一 canvas 的断言仍由前述阻塞响应 T3 独立证明。

操作与截图保留于本会话，摘要 `/tmp/addp-engine-inplace-safari-receipt.json`。本轮仅补文档证据与能力状态，最小验证 `git diff --check` 通过，产品代码未变，复用原 131 项 T1／521 项 T3及构建结果，不重跑有效门禁。不代表列表的个人 Safari 专项、其他时段／引擎、旧来源 T2 或新 CI／Hosted 通过；CI 与 Hosted 仍等专门会话回传，未设置定时任务，未由 AI 启停个人服务。


## 引擎监控就近提示（2026-10-31）

按用户“文字写在需要的地方”要求，先更新运行观测规范6.3，再复用唯一 FieldHelp：列表四项摘要的说明放在列名旁，详情十二项说明放在卡片标题旁，当前趋势含义／缩放说明和当前数据库容器／最长七天范围分别放在指标与时间选择器旁。删除对应常驻重复段落；实际数值、单位、查询窗口、当前缺样、采集失败及权限错误仍直接显示。复用既有中英文文案，不新增翻译键、请求、权限、统计口径或共享提示实现，不修改来源及刷新生命周期。

验证由现有 `make test-monitor-frontend` 的 T1／T3／构建入口和 Console 已登记的前端 T3 自动发现覆盖，Platform frontend 编排及 changed-gate 已覆盖 Monitor 与 Console，无需修改 CI 注册。本轮按仅引擎展示的影响范围拆分执行标准入口，使用现有 Node 24.20.0 的 bin 临时加入 PATH：

- `npm --prefix monitor/frontend test`：131项T1全部通过，日志 `/tmp/addp-engine-tips-unit.log`。
- `npm --prefix monitor/frontend run test:e2e -- engine-resources.spec.js`：首次27项中24项通过、3项失败；两项旧断言仍要求容器范围常驻显示，一项新用例在前一 tip 退出动画未结束时读取到两个 tooltip。保留首次失败日志 `/tmp/addp-engine-tips-browser.log`，失败目录 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-monitor-playwright-results-RaDX4K`。修正为实际聚焦读取容器范围 tip，并在切换提示前等待上一提示关闭；未修改产品或放宽指标／请求断言。
- `npm --prefix monitor/frontend run test:e2e -- engine-resources.spec.js --grep 'engine explanations|PostgreSQL direct detail'`：4项通过，日志 `/tmp/addp-engine-tips-recheck.log`。产品输入未改变，结合原24项中的23项不受测试修改影响的通过结果，共27个场景均有有效通过证据；不是一次27项全绿运行。
- `npm --prefix console/frontend run test:e2e -- engine-resource-navigation.spec.js`：3项全部通过，日志 `/tmp/addp-engine-tips-console.log`；实际 Console iframe 中切换中英文，提示随语言更新，iframe timeOrigin、URL和API次数不变，并覆盖620px布局。
- `npm --prefix monitor/frontend run build`：退出0，4.85秒，日志 `/tmp/addp-engine-tips-build.log`；仅既有大分块提示。`git diff --check`通过。

上述受控浏览器新增验证卡片说明不再常驻、实际键盘聚焦提示、容量不按租户归算／采集事务不等同业务吞吐／当前容器七天范围保持、选定指标提示同步、无额外查询，以及采集失败后当前值清空而缺样及快照错误仍直接可见。既有自动筛选、主机导航、刷新与缩放回归分别沿原用例验证。本轮未运行完整 Monitor／Console 前端总门禁、后端或来源T2，不能把专项通过写成完整模块通过。

原生Safari只读检查第二个本会话标签页时已回到登录页（重定向仍指向主机详情）；未操作第一个标签页，未填写或取得认证凭据，已请用户在第二个标签页完成平台系统管理员密码／MFA登录。本轮引擎 tip 的个人Safari真实显示／交互尚未验，不沿用上一版本的刷新证据代替。本次不启停个人服务、不修改数据库／IAM／配置，不查询或触发CI／Hosted，不设置定时任务；CI与Hosted仍等专门会话回传。


### 用户登录后的个人 Safari 就近提示复验（2026-10-31）

用户在第二个标签页完成平台系统管理员密码／MFA登录后，本会话仅操作该原生Safari标签页，经实际菜单进入引擎监控，再打开 Business PostgreSQL（`deployment_id=9e4fd2b0-3b45-473e-b070-cc6f92e40e11`）。列表真实显示 Infra／Business 两个部署，表头问号已出现；详情十二张卡片的说明改为标题旁问号，当前缺样及顶部“采集来源不可用”直接展示。初次原生点击未观察到说明弹出，不计鼠标悬停／点击通过；随后使用Safari键盘 Option＋Tab，实际焦点和截图分别确认：趋势指标旁显示提交速率／采集事务不等同业务吞吐及缩放仅改变可见范围；时间范围旁显示当前数据库容器运行段／最长七天／断点；容量标题旁显示全部非模板数据库合计、不按租户归算。切换焦点后前一提示消失，各提示在对应控件旁出现；中文内容完整、换行可读，未铺开为常驻段落。实际原生截图与可访问状态保留在本会话。

本轮页面查询窗口实际推进至08:20:00—08:25:00、15秒评估间隔，但Infra／Business均显示“采集来源不可用”，详情值为“—”，因此只认定本次个人Safari中文键盘tip与缺样展示通过；不计真实专业指标采集／趋势数据恢复，也未验证本轮鼠标hover、中英文切换、全部十二个提示或长期刷新。第一标签页未操作，退出时提示已关闭、浏览器边栏恢复原显示、仍保留第二个Business详情页及默认十五秒刷新。下一步优先只读定位两库来源不可用，不未经用户操作应用部署或重启服务。

本轮只追加证据并更新能力文档，产品／测试输入没有变化，复用提交`5cb1a9980`的本地分层证据；最小检查`git diff --check`通过。不改数据库、IAM或采集配置，不重跑有效测试，不查询、调度CI／Hosted或设置定时任务。此前登录待验和本轮登录后结果分别保留，不倒改首次未验记录。


### Console 语言切换与窄屏提示的退出同步（2026-10-31）

专门CI会话返回同前端源码的后继Console完整272项浏览器及构建通过，同时保留首轮主机tip宽度失败。本轮定向测量已将首失败定位到尚未退出的桌面语言菜单，非引擎／主机内容布局；引擎同流程测试在缩窄窗口前同样等待语言弹层display:none，原iframe／无额外查询／620px宽度检查不变。最终主机与引擎两项标准定向场景通过，产品未修改；原始失败、几何测量、固定Linux成功版本与本地修复证据统一见[主机证据](Monitor节点与主机资源历史证据.md#console-窄屏-tip-回归的语言弹层退出同步2026-10-31)。不外推来源恢复、当前完整CI或Hosted，继续等专门会话回传，不设定时任务。


### 菜单退出同步修复的 Console CI 结果（2026-10-31）

固定修复提交`4a6a7a4fa`的真实Linux Console Job现已由专门会话回传并核对原始日志：`make test-console-frontend`的222项单元测试、272项浏览器及构建通过，selected=true／整个Job success。完整来源、摘要与原失败分层见[主机证据](Monitor节点与主机资源历史证据.md#菜单退出同步修复的-linux-console-门禁回传2026-10-31)。本轮仅补文档，最小检查`git diff --check`通过，不重复有效验收，不计整个Platform、Hosted或个人两库来源恢复，仍优先只读排查采集来源不可用。


## 个人两库来源不可用的只读诊断（2026-10-31）

用户要求继续后，沿现有部署及发现契约只读核对，不修改采集开关、证书、账号、HBA或个人服务。标准`bash scripts/infra/status.sh`显示两库与Prometheus均运行，指标中心Ready只作中心存活证据，不代表数据库专业来源可用。`docker ps -a`及限定字段inspect确认Infra／Business各自的SQL Exporter和mTLS入口共四个容器均为Exited(255)、OOM=false、无Docker State.Error，restart策略为no。四个来源停止时间均在2026-10-30 16:13:20 UTC附近；两库与Prometheus记录几乎同时停止，随后在16:13:23 UTC重新启动，采用unless-stopped策略。换算北京时间为10月31日00:13。该时间关联不能独立证明Docker Desktop或daemon的具体退出原因，原始255事件不按猜测归因。

两个作用域的配置仍为`ADDP_POSTGRES_METRICS_ENABLED=true`；既有服务器证书存在，`openssl x509 -checkend 0 -noout`均成功，只证明本轮未过期，不替代完整信任链及mTLS验收。两库的当前容器ID与四个来源的数据库ID标签一致，但数据库StartedAt已改变，四个来源的数据库启动时刻标签全部不匹配。标准发现快照infra/business本体均available且持续更新（分别13:00:10Z／13:00:12Z），其中postgresql_metrics.state均为unavailable，与`scripts/utils/deployment-inventory.py::postgres_metrics`要求来源运行、容器身份及启动时刻精确匹配的拒绝路径一致。Monitor沿`engine_observation.go`将私有投影的unavailable映射为source_unavailable；本轮不借用后台身份或绕过当前用户授权调用私有接口。

确认的直接断点为采集容器未恢复，且旧来源声明已不匹配本次数据库启动；不是页面tip修改引入的查询或展示问题。仅docker start旧容器或补重启策略，仍不能重新发布当前启动身份，因此不能据此认定恢复。当前支持的恢复路径仍是标准部署入口：用户在仓库根目录依次运行`bash scripts/infra/up.sh --metrics`及`bash business/scripts/start.sh -postgres`。前者重新应用中心和Infra来源；后者沿现有幂等分支复用已运行Business PostgreSQL，再重新核验采集账号、认证及当前数据库身份并发布来源。暂不要求Monitor重启；恢复后分别复验来源发现、实际专业值和趋势，不用source deployed输出替代采集通过。按AGENTS，实际应用已请求用户操作，本会话未执行。

本轮仅诊断并更新现有证据／能力状态，最小检查`git diff --check`通过；没有产品／测试变更，不运行额外数据库、来源T2、CI或Hosted，不设置定时任务。Prometheus受保护API的无凭据直读未成功，不记为目标状态通过，也不读取或输出服务秘密。当前来源恢复与本次采集新鲜度／趋势有效性仍待用户应用后的验证；后续若要让宿主恢复时自动协调来源，需要另行明确部署侧身份重发布职责，不直接加入restart旁路或放宽StartedAt校验。


## 用户重新应用后的两库来源与 Safari 恢复复验（2026-11-01）

用户在前述两条标准命令请求后回复“已重启”。本会话仅只读复验，没有自行启停或修改部署。`docker ps -a`和限定字段inspect确认四个Infra／Business SQL Exporter与mTLS入口均running、exit=0；数据库本身仍运行，四来源的数据库ID及StartedAt标签全部与当前数据库匹配。最新发现快照infra/business本体及postgresql_metrics.state均available（观察时间分别00:13:58Z／00:14:01Z），前述旧启动声明失配已不再阻碍发现。

使用根配置中既有Monitor→Prometheus的mTLS只读身份，对现有HTTPS `/api/v1/targets`执行一次限定GET：目标总数9，其中两个postgresql目标均health=up、lastError为空，最近采集分别00:15:20.815997426Z／00:15:22.204840669Z。仅输出数量、健康及时间，不输出目标端点、凭据或秘密，不调用SQL或管理接口。此处up只证明来源传输，实际数据库快照及Monitor消费由下述真实页面另证。

原生Safari仍为用户已登录的平台系统管理员第二个标签页；初次读Business页显示“暂无采样”，不直接判恢复。随后刷新动作遇到工具用户操作保护而中止，重新读取后页面已在引擎列表；本会话没有沿旧索引发送输入。当前列表真实显示两库“正在采集”：Infra连接48、活跃0、提交148.61次/秒、容量6.30GiB；Business连接0、活跃0、提交0.60次/秒、容量78.3GiB。随后实际进入两个详情，十二项即时值可见，最大连接数均200，真实零与缺值不混用。Business提交0.53—0.60次/秒、回滚／死锁0.00；Infra空闲48、提交148.60后更新147.51次/秒、回滚1.00、死锁0.00。该速率仍包括观测事务，不当作租户业务吞吐或异常结论。

Business真实截图看到提交曲线，五分钟窗口08:11:12—08:16:12、15秒评估，末段为新采样点，前段保持空缺；后续无手动刷新，窗口推进至08:11:28—08:16:28。Infra随后真实截图看到连续的新提交曲线，窗口从08:12:06—08:17:06推进至08:12:22—08:17:22，页面自动刷新保持开启。截图及原生可访问状态保留在本会话；只认定两库当前真实值、提交趋势及本轮数据推进，不外推所有指标趋势、七天、缩放交互、长期可靠性、原生Linux或新Hosted。第一标签页未操作，浏览器主题／侧栏／刷新偏好未改，最后保留第二个Infra详情页。

前一日四来源停止、旧StartedAt失配与本日恢复结果分别保留；当前个人来源不可用缺口已在上述范围闭合，不倒改停采期间的历史，不填零或回填曲线。宿主自动恢复后如何重新协调部署身份仍未新增，本次依赖用户标准入口重新应用，不以本次恢复证明无人值守恢复。

本轮仅追加证据及更新能力状态，产品和测试未改，最小检查`git diff --check`通过；不重复已有效的T1／T3／来源T2／CI或Hosted，不自行轮询、触发或设置定时任务，不修改IAM、数据库、采集配置或共享工作区其他会话改动。下一步建议先讨论宿主恢复时的部署侧来源协调范围，再在确认后实现，不直接加restart策略或放宽启动身份校验。


## 完整开发启动的 PostgreSQL 来源恢复（2026-11-01）

用户确认首期在完整启动 ADDP 时恢复已安装来源，不加入 Docker 恢复后的常驻协调进程。正式口径见[运行观测规范4.7](../../addp平台运行观测规范.md#47-postgresql-受保护的固定指标来源)。`scripts/dev/start.sh` 仅在无参数／`-all` 且非 `ADDP_ONLINE_HOST=1` 时、刷新发现快照前调用恢复；单模块入口不处理。根与 Business 配置在独立子进程清空本能力四项变量后分别加载，未选择或未安装时不访问 Docker／数据库，任一作用域失败不阻断另一作用域或业务启动。

`lifecycle.py` 将容器事实和采集账号只读校验收敛到 `ObservationDatabase`，管理员安装职责留在唯一显式安装器 `Database`。恢复不构造管理员安装器，不执行管理 SQL、不启用／轮换账号、不写／重载 HBA、不启动数据库；复用同一 `source_document`／`start_sources` 创建来源。锁内核验工作区、Docker Engine、项目／服务、同一数据库容器 ID、旧声明、受管文件权限／内容、凭据与 TLS，以及采集角色／固定函数的目录事实和新鲜快照。健康声明匹配时不重建、不改受管文件；来源停止／缺失或本容器启动时刻改变时，更新来源声明并重建来源对。新容器、停用账号、权限漂移、符号链接、缺失凭据、TLS／配置漂移、并发锁及数据库事实竞争均拒绝，失败只清理明确拥有的来源，不安装或修复数据库权限。

确定性回归覆盖健康无操作、停止与启动时刻变化、丢失来源、只读核验失败、外来归属／新容器／TLS与配置漂移／重复 JSON、竞争失败清理、锁争用、关闭和未安装不访问 Docker、完整启动选择与失败继续、双作用域配置隔离。恢复夹具的命令输出改为轻量 shell，消除为两行输出额外启动 Python 的依赖；保留原五秒期限及全部断言。

真实来源验收仍沿既有 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=postgres-source`，没有旁路或新 Hosted 用途。新增恢复检查分别比较账号属性／密码摘要、HBA、来源容器身份及受管文件字节／inode／mtime；还验证来源停止恢复、同一容器新 StartedAt 重绑定、角色额外成员拒绝和计划停用后不复活。夹具原 PGDATA tmpfs 在容器重启时清空，会重新初始化数据库，不能证明保留数据库后的恢复；现改为当次项目专属 `postgres_data` 卷，保留 CPU／内存限制。目录认证分支销毁当次容器及该卷后新建测试库，最终仍由标准退出清理删卷并按项目标签核对零残留，不触碰共享或个人 PostgreSQL。

保留本轮失败链：初次来源 T2 在证书准备阶段因本机 OpenSSL4.1.0 默认配置文件缺失退出，未到数据库场景；后续仅在本条命令 PATH 选择已安装 OpenSSL3.6.4，不改全局安装／配置或 TLS 断言。随后原查询夹具通过宿主替换发现清单，Nginx 的 Desktop 挂载视图返回404、Prometheus不能移除旧目标；改为以目录 owner 在当次来源容器内完成同一文件的原子发布和精确读回，保持45秒目标移除期限。新增账号基线检查曾误从 `pg_authid` 读取只存在于 `pg_roles` 的配置字段，已修正关联查询；容器重启验收又确认上述 tmpfs 数据丢失，不以延迟／重试掩盖。失败轮次均沿标准退出清理，独立按各项目标签确认容器／网络／卷清零。开发综合门禁还保留旧 Worker／停止夹具超时记录，未修改其期限；不并行运行来源实测的后续验证未重现这些超时。提取启动片段的旧夹具新增显式 `START_ALL=false`，新完整启动分支另有执行断言，不在产品中补默认或兼容分支。

本轮 `make test-infra-runtime-log-lifecycle` 曾完整通过37／136／142项及身份Go夹具；最新 `make test-dev-lifecycle` 的22项与194项通过，但随后 HTTP 预算原生超时夹具缺失 PID 文件，综合门禁失败，未修改该owner路径或按全绿汇报。与本批直接相关的 `bash scripts/test/dev-lifecycle-and-build.sh` 单独执行全部通过，包括本地／Online分支、Infra复用、完整／局部重启、端口、Worker、锁及原子构建；不替代上述综合结果。最终来源T2的结果如下；不把局部成功或清理成功计为全部通过。未操作个人服务，不轮询／触发CI或Hosted，不设置定时任务。

最终执行命令为 `PATH=/opt/homebrew/opt/openssl@3/bin:$PATH make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=postgres-source`。125项配置测试、26项生命周期T1全部通过；原生采集账号／固定汇总、mTLS准入、十二项指标查询／当前容器趋势／摘要／统计重置空缺和SQL失败恢复通过。新增实际产品恢复在Infra和Business两作用域均通过：健康时来源容器及受管文件字节／inode／mtime不变，停止来源后恢复新鲜快照，同一数据库容器重启后保留数据库及账号并重新发布当前StartedAt，账号属性／密码摘要与HBA保持不变；权限漂移拒绝、NOLOGIN计划停用不复活及原生命周期用例均通过。日志分别有 `PostgreSQL recovery T2: infra/business ... passed` 与完整 `PostgreSQL lifecycle T2` 成功行。

该命令整体仍失败，不能写为来源T2全绿：后续目录认证分支在宿主原子替换HBA后，容器读取 `/etc/addp-postgres-auth/pg_hba.conf` 返回非零，finally恢复又因输入版本改变拒绝；尚未到此分支新增恢复用例，不放宽原子性、权限、版本或读回断言。此前普通Business拒绝用例另曾出现未分类命令退出，最终轮未重现，未宣称根因已修复；仅补充退出码／容器running、OOM与退出状态／固定内存事件计数的封闭失败诊断，禁止输出SQL、凭据或原始错误。最终随机项目 `addp-metrics-t2-9ba602eecb87400a91b458a58ea980ee` 由标准退出清理销毁，容器、网络和三个卷均按项目标签独立复核为零；清理不改变整体失败结论。

变更沿已登记的Monitor来源T2输入与 `Platform consistency` 开发生命周期门禁覆盖；未新增CI分组、延长Job／场景期限或触发Hosted。个人环境完整启动以及真实Monitor／Console自动恢复尚未验收；原生Linux目录认证／恢复、新CI及Hosted结果仍缺，等待专门会话回传。本批建议状态为“部分实现”：代码与两作用域新增恢复实测已具备，综合及完整来源门禁仍有上述明确缺口。


## 目录认证 HBA 读取取证与夹具恢复版本修复（2026-11-01）

用户要求继续补齐目录认证分支，CI／Hosted仍由专门会话回传，不设置定时任务。本批只修改同一T2夹具及其确定性回归，没有修改产品认证发布、HBA策略、数据库权限、个人配置或服务启停。入口与CI输入登记已覆盖 `postgres-observation-probe.py`、配置回归和Owner门禁说明；没有新增依赖、旁路、分组或期限。

原夹具在物理发布成功后，直到容器读回／重载成功才更新恢复版本；读回失败时finally仍用旧版本，导致恢复CAS冲突并掩盖首个错误。现在物理发布成功即记录新版本，后续仍严格逐字节读回、校验属主模式、`pg_hba_file_rules`及三秒真实重载；恢复仍沿相同版本校验和原子写入，不覆盖外部漂移。首个失败保留，清理异常只追加类型诊断。新增回归覆盖“发布成功但读回失败后按新版本恢复”、首失败与仅清理失败的区分，以及HBA／SQL／凭据／路径不进入封闭诊断。

标准命令 `PATH=/opt/homebrew/opt/openssl@3/bin:$PATH make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=postgres-observation`，日志 `/tmp/addp-hba-directory-diagnostic.log`：128项配置回归、26项生命周期T1通过，原生认证与权限分支的Infra／Business均通过；目录分支仍失败。失败诊断确认宿主内容精确匹配新声明，661字节、0644；唯一只读bind的源目录也匹配。容器cat返回not_found，随后stat成功但仍见旧元数据 `644:90:0:0`。恢复发布后宿主回到90字节基线，未再产生旧版本CAS冲突；容器读回仍失败，首个cat错误保留并附加清理失败类型，未判恢复通过。

只将该次测试命令的 `TMPDIR` 改为系统 `/private/tmp` 进行对照，其他协议／断言不变，日志 `/tmp/addp-hba-directory-shared-tmp.log`：128／26项再次通过，同一目录读取故障及元数据失配复现，不能据此把问题归因于默认临时路径。两个随机项目 `addp-metrics-t2-c7a40ff34f134729ae5c95d7d20e6071`／`addp-metrics-t2-4261cd8a06c94a3a831c34f3162d0d89` 均执行标准finally销毁，容器／网络／卷另按各项目标签核对；均不计T2通过。

本机Docker.app版本只读确认为4.49.0；现有Business容器的认证挂载在 `/proc/self/mountinfo` 显示只读fakeowner，未读取业务内容或操作SQL。该层级不能直接等同于VirtioFS／gRPC FUSE的设置值；设置文件读取未成功，已中止该次读取，原生窗口工具也超时，未宣称核对了实际选择。当前证据定位为“宿主原子替换后，容器文件共享视图未呈现新文件”，尚不据此指定某个Docker内部缺陷或声称升级版本即可解决。已请用户只查看文件共享实现，不应用或重启；应用设置可能涉及整套容器，须另定窗口。官方选项说明见[Docker Desktop设置](https://docs.docker.com/desktop/settings-and-maintenance/settings/)。

当前交付为夹具恢复版本／首失败保留与安全取证已修复；目录HBA正向读回及后续专业来源／容器重启恢复验收仍未通过，新CI／Hosted未运行或读取。保持既定原子目录发布与不可见即拒绝，不加等待重试、不改为原地覆盖、不放宽认证，也不反复重启数据库。建议先核对并修复本机Docker文件共享环境，再沿同一标准入口补齐正向验收；重复问题的稳定诊断步骤建议汇入现有故障排查指南。

## Docker Desktop 实际共享设置与普通文件对照（2026-11-01）

按用户要求只读查看原生 Docker Desktop 4.49.0 设置：General 当前选择 Apple Virtualization framework 与 VirtioFS，Docker VMM、gRPC FUSE、osxfs 均未选，Enhanced Container Isolation 关闭。Resources／File sharing 列出 `/Users`、`/Volumes`、`/private`、`/tmp`、`/var/folders`；没有已列出的同步缓存共享，界面提示登录后使用 Synchronized file shares。Apply 始终禁用，Engine running；未更改选项、应用设置或重启。设置页保留在 File sharing 供用户查看。

只读运行核对确认 context 为 desktop-linux、本地 Unix socket，VM 进程实际带五项 `--virtiofs` 参数；未输出其路径内容。容器的 fakeowner 挂载层和内核中 grpcfuse 模块不能据此当作 gRPC FUSE 被选择的证据。现有个人 Business 认证目录仍只读；限定读取当前 HBA 的结果是宿主与容器各1367字节且内容一致，未输出内容、密码摘要或凭据，未执行SQL。故障范围是发布替换后的可见性，不能表述为现有HBA持续不可读，也不能归因于临时路径未共享。

沿原有来源T2增加普通文件对照：在挂载前准备 `publication-probe`，容器先读到旧内容，再用现有原子发布器替换该文件；同时核验HBA在两侧均不变。失败仍使原门禁失败，不跳过后续认证、不增加等待或重试，不修改产品安装器或HBA。该夹具、配置回归与门禁文档已在原有CI输入登记中，无新入口、依赖、权限或Workflow。

执行 `PATH=/opt/homebrew/opt/openssl@3/bin:$PATH make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=postgres-observation`，日志 `/tmp/addp-hba-publication-control.log`：129项配置T1、26项生命周期T1通过；Infra／Business原生认证、固定汇总、拒绝越权、幂等、轮换与归属用例均通过。目录前置对照失败：`host_inode_changed=true`、`host_matches_replacement=true`，但容器读取返回1、`container_matches_replacement=false`；`hba_host_unchanged=true`且`hba_container_unchanged=true`。该结果独立排除HBA解析、账号SQL及重载作为本次普通文件故障的触发条件，仍不足以指定Docker上游缺陷或承诺升级修复。

目录HBA正向验收尚未进入，整体T2失败；不将普通分支成功或安全拒绝记为完整通过。项目 `addp-metrics-t2-b8a5b53b8a2d4255ac93ca69559904e5` 标准退出清理后，按项目标签独立确认容器、网络、卷均为零。`py_compile`、`bash -n`及`git diff --check`通过。本轮不修改个人数据库或服务、不轮询／触发CI和Hosted、不设置定时任务。下一步以专门会话回传的原生Linux目录分支结果作对照，再确定共享层维护窗口；本机正向读回及完整来源恢复验收继续保留缺口。


## 已有文件名的替换缓存与目录枚举对照（2026-11-01）

继续沿标准 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=postgres-observation` 取证，命令仅为本次进程在PATH前置既有OpenSSL3；没有调整Docker设置、共享选项、账号／HBA或个人服务，也没有读取／重跑CI、Hosted或设置定时任务。本机macOS只读版本为26.6.2。新增内容仅为既有目录前置检查失败后的封闭诊断；对照结果即使成功，首个读取失败仍保留，门禁仍失败，不以枚举、延迟或重试替代发布后的精确读回。既有来源T2登记和Platform配置测试已覆盖这些文件，无新增入口或依赖。

第一轮 `/tmp/addp-hba-no-chown.log` 中，去掉同属主的 `fchown`、保留权限设置／fsync／原子替换后仍不能读取；两条路径stat成功但都报告旧7字节元数据。第二轮 `/tmp/addp-hba-directory-lookup.log` 将初始创建改成普通文件而非硬链接发布，替换后仍失败；从未使用过的新文件名发布后立即读到14字节新内容。该轮最终枚举目录后，原失败路径也变为新inode及14字节、内容可读，不能据单轮顺序直接排除时间与其他文件变更的影响。

第三轮 `/tmp/addp-hba-directory-enumeration.log` 将目录枚举提前到其他文件变更之前：原路径首次读取仍失败，stat报告旧7字节／inode36122；紧接着枚举目录成功，随后stat报告新14字节／inode36124且内容精确匹配。之后去属主设置对照、普通创建对照分别仍呈现旧7字节元数据和读取不匹配，而新文件名仍立即可读；原路径继续保持新内容。这进一步将故障范围缩小到已有文件名的替换后缓存更新，排除初始硬链接创建及fchown是唯一必要诱因；尚未把枚举后的恢复认定为产品修复或精确上游缺陷，未增加“先ls再读”的发布路线。

三轮各自130项配置T1与26项生命周期T1通过，Infra／Business原生认证、汇总、权限拒绝、幂等、轮换、归属T2均通过；目录前置均失败，HBA宿主与容器原内容均未变，目录认证正向验收未进入，整体T2均失败。三个项目 `addp-metrics-t2-bf1ca637fccc41c79c08f67b8362f9d8`、`addp-metrics-t2-69a07664fddd488ba239cb887d15710a`、`addp-metrics-t2-32552e5a75a1423d85cadb4a760d5ff5` 均沿标准退出清理，容器／网络／卷另按各自项目标签核验全零。最终代码py_compile、bash -n和git diff --check通过，不把局部通过升级为完整来源验收。

上游公开报告仅作假设来源：[Docker for-mac #7882](https://github.com/docker/for-mac/issues/7882) 描述STATX_BTIME查询失败而open仍成功，与本地内容读取失败不同；[#6787](https://github.com/docker/for-mac/issues/6787) 有共享目录ENOENT报告，但版本、工作负载及触发条件不同，不直接认定同一问题或宣称已有确定修复版本。后续正向验收仍待共享层一致性恢复及专门会话回传的原生Linux对照；现阶段保持原子发布和不可见即拒绝。


## 同进程、等时与正反枚举顺序对照（2026-11-01）

按用户要求继续排除进程启动及经过时间的混淆。删除上一批多次启动head／stat／ls的诊断路径，改用既有PG镜像内的一个Bash进程完成每组全部读取、定时read与文件名展开枚举；无外部命令、子进程或新运行依赖。两个子目录在挂载前准备，进程先读到两侧旧内容并握手，宿主才在同一部署锁内原子替换两侧普通文件。先等待一秒且不枚举，同时读取两侧；随后仅枚举第一侧并再次读取两侧，再进行一秒无枚举对照，最后枚举另一侧。正式诊断使用两组新文件交换首先枚举的一侧，每组独立确认同一PID、Linux `/proc/uptime` 单调时间、固定阶段和完整时间区间；单组八秒、输出8192字节封闭预算。区间完整性按内核时钟10毫秒分辨率核验，不用于延长产品读回或等待认证成功。

确定性回归实际运行同一Bash脚本及握手／宿主原子发布，在宿主临时文件上验证两组观测协议和HBA不变；仅在该宿主传输替身中将Linux时钟读取替为Bash内建SECONDS，不将此T1视为共享文件系统证据。另拒绝PID变化、时钟倒退、无枚举区间不足、总预算超限、阶段／状态注入及输出不完整；成功诊断不得改变原发布失败结果。原配置T1及来源T2输入登记已覆盖，无新增case、CI依赖、Workflow或Hosted用途。

第一轮标准命令 `PATH=/opt/homebrew/opt/openssl@3/bin:$PATH make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=postgres-observation`，日志 `/tmp/addp-hba-single-process.log`：同进程在约1010毫秒的无枚举对照后两侧仍不可读，枚举A后同一时钟刻度A读到新内容、B仍不可读；约2010毫秒B在尚未枚举时自行恢复。因此不能断言只有枚举才能恢复，也不能据此指定稳定缓存TTL或安全等待时长。

最终正反对照执行同一标准命令，日志 `/tmp/addp-hba-single-process-balanced.log`。第一组先枚举A：发布后两侧不可读，约1010毫秒仍均不可读；在同一1010毫秒刻度枚举A后A恢复、B仍不可读；约2010毫秒B仍不可读，枚举B后恢复。第二组先枚举B：约1010毫秒两侧仍不可读；在同一刻度枚举B后仅B恢复；约2020毫秒A仍不可读，枚举A后恢复。每组全部阶段通过同一PID核验。该结果支持“目录枚举具有独立的刷新作用”，不能仅用经过时间或新进程启动解释；它不证明精确Docker／VirtioFS内核缺陷、固定TTL或产品已修复。

两轮各131项配置T1、26项生命周期T1通过；Infra／Business原生认证、汇总、权限拒绝、幂等、轮换、归属T2均通过。原目录前置仍失败、两侧HBA原内容不变、正向目录认证未进入，整体T2失败，不因诊断后来可读而改判。两个项目 `addp-metrics-t2-714e53c6268943f787eb249e977cbca3`／`addp-metrics-t2-ddf06be8f89c46f683cb68bae160c5a0` 已标准退出清理，并按各自标签独立确认容器／网络／卷全零；py_compile、bash -n及git diff --check通过。

本轮不修改Docker设置、现有服务、个人数据库或凭据，不加入先枚举／重试／原地覆盖的认证路线；未查询或触发CI、Hosted，未设置定时任务。当前仍是共享层一致性缺口，安全发布保持不可见即拒绝。下一步建议以该同进程双目录对照核对原生Linux结果，并据此讨论共享层维护或发布职责调整；后者若改变既定部署边界，须先确认，不能当作局部兜底直接实现。


## 无数据库的共享目录最小复现与修复建议（2026-11-01）

用户要求去除数据库依赖并明确共享层处理建议，继续禁止修改Docker设置、重启现有服务或设置定时任务。现将同进程Bash协议、单调阶段校验及八秒／8192字节观察收敛到 `scripts/test/directory-publication-probe.py`；PG目录失败诊断只适配命令和已有认证发布器，不再维护另一份脚本／协议。新文件只依赖Python标准库和Docker，复制单文件即可在ADDP仓库外执行；不是新增产品认证路线。

独立复现使用既有固定PG镜像里的Bash，入口覆盖为 `/bin/bash`，不启动PG、SQL、采集器或TLS，不读取ADDP配置、账号和HBA。宿主仅对随机临时目录中的普通文件执行write／flush／chmod／fsync／os.replace／目录fsync；容器只读、无网络／端口／额外能力，CPU0.5、内存64MiB、进程32。镜像声明的数据库VOLUME显式用64KiB tmpfs遮蔽，不创建匿名数据库卷。每次创建的容器都有随机项目及专属owner标签；异常回收前核对owner和项目，不删除外来对象，最后检查项目容器／网络／卷及临时文件为零。selector初始化失败亦回收传输。未匹配首次发布立即失败，后续枚举恢复不改判。

公共入口新增 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=directory-publication`，默认all同样覆盖它；该case的T1仅执行配置／协议所属的 `MetricsDeploymentConfigTest`，其余case继续执行原完整配置套件。新脚本登记到既有T2输入和Release `Monitor metrics infrastructure (directory-publication)` 矩阵，保留fail-fast=false及20分钟上限，无新权限、Hosted用途或运行依赖。新矩阵只为后续自然CI验证登记，本会话未查询、触发或重跑CI／Hosted。

两次最初完整配置预检分别运行133／135项，均因既有 `ServiceExpectationPublicationTest.test_standard_stop_publishes_selected_scope_before_the_first_stop_action` 的fake stop-dev脚本10秒超时而失败，尚未进入最小容器。未修改该夹具、停止逻辑或期限，也不把失败升级为通过。完成新case的正确范围划分后，最终两轮各77项相关T1通过；这不替代上述完整套件结果。

最终标准命令日志 `/tmp/addp-directory-minimal-proof.log`，项目 `addp-metrics-t2-12509b04692b460e8c03524f54a2ff95`：无数据库两组正反顺序均完成。两侧发布后立即不可读；第一组约1010毫秒仍不可读，枚举A后仅A恢复，约2020毫秒B仍不可读、枚举B后恢复。第二组约1010毫秒仍不可读，枚举B后仅B恢复，约2020毫秒A仍不可读、枚举A后恢复。未依赖HBA、CAS、数据库读取或重载即可复现。独立复现按共享契约失败退出1，外层make返回非零，不是修复通过；JSON的cleanup_verified、zero_containers／networks／volumes及temporary_removed均true，之后按该项目标签独立复核三类资源全零。前一轮 `/tmp/addp-directory-minimal-scoped.log` 有一侧在后续等待时自行恢复的记录继续保留，不能推导固定TTL。

`make test-t2-ci-registration` 的74项套件因四项Common execution-lifecycle夹具15秒超时失败，实际仓库检查因make顺序未进入；未改这些非本批夹具。随后只读执行内部事实检查 `python3 scripts/ci/check-t2-ci-registration.py --repository /Users/pampa/code/addp` 通过，确认当前登记命中，不将该局部结果称为整套登记门禁通过。`make test-workflow-security`通过（固定版本zizmor离线模式，覆盖现有工作流安全审计，不是线上CI结果）；py_compile、bash -n、git diff --check通过。PG来源正向完整门禁、本轮新Linux与Hosted仍未获得通过结论。

可交给维护人员的包为 `/private/tmp/addp-directory-minimal-b5kwiyfk.zip`，包含与仓库逐字节一致的独立脚本、运行JSON、有限版本信息和README；脚本SHA256为 `c864911b5034f3f0a62993686acbcd1c2c5161947a6a85120d76a9c5bdfaa4c5`。包内Docker设置注明为本会话前期只读快照，不冒充升级后或重启后的状态；未自动上传或向上游发消息。

修复建议已写入[开发服务与构建故障排查](../../../guide/troubleshooting/开发服务与构建.md#docker-desktop-共享目录原子替换后容器读不到文件)并加入故障总索引：保留不可见即拒绝，先在独立环境或授权维护窗口验证候选受支持Docker版本，保持VMM／VirtioFS不变；仍失败时才单独比较gRPC FUSE，分别跑相同复现及原目录认证／重载／恢复门禁。官方[发行说明](https://docs.docker.com/desktop/release-notes/)记录过缓存／宿主通知修订但未证明本例已修复，不能承诺升级或重启即可解决；官方[设置说明](https://docs.docker.com/desktop/settings-and-maintenance/settings/#general)只作为可选共享实现的事实来源。若长期要求避开跨宿主动态发布，应单独讨论VM内受管卷和部署侧发布职责，不在本批引入。个人环境及产品安全校验未改动。

## Redis 固定采集与最小 ACL 基座（2026-11-01）

用户确认首期实例级INFO元数据、禁止业务键访问的范围后，本批同步维护术语、运行观测规范与 `monitor/backend/internal/redissource`。本批交付为部署侧采集内核与账号配方，不是已安装的专业来源：仅HELLO／PING／固定INFO，使用既有Go Redis客户端，关闭重试、维护通知和客户端信息上报；客户端钩子拒绝额外命令及旧AUTH回退。固定字段保留uint64精度、有效零及AOF未启用差别，缺字段／重复／禁用errorstats／超解析预算／身份变化拒绝；采样前后run_id和uptime保护同容器内重启边界。原始INFO、路径、任意错误标签及上游错误不进入输出。

账号配方完整reset、无键／频道／selector授权；启用只追加三个明确命令，停用同时清空旧会话的命令权限和密码。仍未实施正式部署账号归属、安装／持久化、凭据丢失／漂移处理与mTLS来源，也未修改个人Redis。采集器只读边界与部署admin的安装权分开；INFO账号可读取较宽的实例元数据，这一已确认边界不能表述为服务端字段ACL。

实际验证：

- `make test-monitor-redis-source-unit`：7个确定性测试通过，包括精度／未知与零值、非法／缺失字段、当前运行身份、ACL配方、未声明命令／AUTH回退拒绝、私网端点及错误脱敏／200毫秒请求取消；日志 `/tmp/addp-redis-source-unit-complete.log`。早期新增数值项数量断言错写21、集成代码误用客户端不存在的ACLGetUser方法均在本地失败并修正，未推给CI。
- 最终 `make test-module MODULE=monitor GO_ONLY=1`：Monitor全Go T1退出0，新增包重新运行，其他未变包复用Go缓存；日志 `/tmp/addp-redis-source-monitor-go-final.log`。未启用真实源opt-in，常规Go套件中的集成跳过不计T2；未跑未变前端或完整模块T0—T3。
- 最终 `make test-monitor-redis-source`：1个真实协议测试／infra、business两个分支通过，拒绝skip，退出0；日志 `/tmp/addp-redis-source-t2-registered.log`。复用原Business Redis 7.2.13固定镜像和唯一随机项目生命周期，在同一当次Redis分别测试两种账号，交叉验证时对方账号也真实存在且用独立密码可读取。覆盖固定INFO及原reader保持、业务读／扫描／写／脚本／管理／频道拒绝，停用后的旧连接／新连接拒绝；当次一字节逻辑maxmemory与单次小SET触发OOM，恢复零／原限额后读取，不循环加压或填充内存。该账号隔离测试不等于两套实际部署已接通。项目 `addp-redis-t2-38869-38873` 清理退出0，随后按精确项目标签复核容器／网络／卷全零；早期编译失败及其他本轮项目也沿同一退出清理，未操作个人Redis或现有PG。
- `python3 scripts/ci/check-t2-ci-registration.py --repository /Users/pampa/code/addp`：最终注册事实检查退出0，24个hosted-service、14个owned-Compose、9个hosted-only及2个owner-managed门禁；日志 `/tmp/addp-redis-source-registration-final.log`。Monitor dry-run确实包含 `make test-monitor-redis-source`，根串行集成及新Release同名作业同批登记。最初尝试借Common作业聚合时虽注册检查通过，完整Monitor计划不包含该专项；已改为Monitor薄入口，仍复用原夹具和协议runner，Common消费者不重复运行本专项，旧聚合接法未保留。
- `make test-workflow-security`：固定zizmor离线审计退出0，日志 `/tmp/addp-redis-source-workflow-security-final.log`；新增入口／共享runner的bash语法及差异格式检查通过。

完整 `make test-t2-ci-registration` 本轮74项聚合退出非零：SystemCleanupPostgresRunnerTest的no-tests子场景15秒超时，随后skip-child因当次模拟锁／目录残留失败，日志 `/tmp/addp-redis-source-registration-full.log`。未修改这些非本批夹具、放宽时限或将注册事实局部通过记为完整聚合通过。正式账号生命周期、mTLS、System登记／私有发现投影、Prometheus、Monitor查询／Console和正式身份T4尚未接通，CI／Hosted未自行查询、触发或重跑，继续等待专门会话回传；未设置定时任务、修改Docker设置、重启个人服务或安装个人账号。


## Redis 账号持久化与来源生命周期（2026-11-01）

本批接通 Infra／Business 单机 Redis 的原生 ACL 启动、数据卷内本方账号记录、部署用户私有凭据与归属、显式启停及独立 mTLS 固定来源。唯一配方仍由 `redissource.ObserverACL` 生成；标准生命周期通过来源镜像的受限 stdin 计划调用该配方，不保留另一套账号生成器。源进程只持最小采集凭据，管理员只在安装期进入 stdin；源容器非特权、只读、限额，不挂载数据卷／宿主目录／Docker socket。Business 原 admin／reader 配方保留，Infra 保留原 default 认证。未修改个人 `.env`、安装个人账号或启停个人服务。

本地验证及证据：

- `make test-module MODULE=monitor GO_ONLY=1`：Monitor Go T1 全部退出0，含新来源命令及内核；日志 `/tmp/addp-redis-lifecycle-monitor-go.log`。
- `make test-monitor-redis-source-unit`：两个Go包及11项生命周期T1退出0，覆盖凭据沿用、先停源后清权、journal版本恢复、归属／权限／丢失状态拒绝、私有文件和锁，以及未安装关闭时不联系Docker；日志 `/tmp/addp-redis-lifecycle-unit-final.log`。
- `make test-monitor-redis-source`：最终标准门禁退出0；固定 Redis 7.2.13 镜像、随机密码、专属两个 Redis，既有协议／越权检查和两作用域实际 mTLS 来源均通过。验证运行中及重启后账号精确配方、同一密码沿用、进程重启计数代际变化、停用后重启仍清权、坏TLS重新应用后的停源与持久清权、损坏记录不阻断业务认证且拒绝接管、标记数据保留，以及无客户端／错误用途证书及额外HTTP入口拒绝。日志 `/tmp/addp-redis-lifecycle-t2-final2.log`，项目 `addp-redis-t2-34256-34261`（以该日志中的实际项目标签为准）。两个作用域源容器／配置卷、主夹具容器／网络／数据卷全部清零；另按精确项目标签复核本轮8个项目及对应来源项目零残留。
- `make test-business-redis`：5项原认证、原生样例、幂等、数据保留／重启及权限冲突回归退出0；日志 `/tmp/addp-redis-lifecycle-business-final.log`。
- `make test-infra-runtime-log-lifecycle`：标准入口退出0，其中启动选择／失败边界、HTTP预算、Trace配置与PostgreSQL生命周期等合计142项通过，含共享私有文件／Docker传输抽取后的PostgreSQL生命周期26项回归；日志 `/tmp/addp-redis-lifecycle-infra-unit-final.log`。`make test-monitor-metrics-config` 的136项配置T1退出0，日志 `/tmp/addp-redis-lifecycle-config.log`。未重跑完整PostgreSQL实源T2，不把共享帮助函数的T1当作目录挂载故障已解决。
- Hosted Business Redis物理夹具同步只读挂载唯一认证入口；`python3 -m unittest scripts/test/online-redis-consumer-fixture_test.py` 6项受控命令T1退出0，日志 `/tmp/addp-redis-lifecycle-online-fixture-final.log`。不等于真实Hosted运行。
- T2注册事实检查退出0（24个hosted-service、14个owned-Compose、9个hosted-only、2个owner-managed），日志 `/tmp/addp-redis-lifecycle-registration-final.log`；Online注册事实检查退出0，日志 `/tmp/addp-redis-lifecycle-online-registration-final.log`。沿现有Monitor／根集成／Release专项自动发现，未新增CI身份、Hosted用途或定时任务。脚本语法、Python编译及差异格式检查通过。

过程失败均未计通过：最初Redis CLI `--json`实际协商RESP3对象，而部署代码按数组读取，已改为显式RESP3并只接受对象；当前Python严格TLS验证拒绝缺少密钥标识的测试证书，已补SKI／AKI，未放宽验证；损坏记录恢复测试原先只恢复文件、未重新加载Redis，已补一次性夹具重启后再验停用。Infra配置渲染夹具补齐本身必需的Redis密码。最终标准门禁覆盖这些修正，早期失败项目均清理，不以等待、补零、自动接管或放宽ACL处理。

范围仍为来源与账号生命周期。System Redis部署类型／私有投影、部署发现、Prometheus HTTP SD、Monitor查询／Console尚未接通；正式用户链路、CI及Hosted未在本会话查询、触发或重跑，等待专门会话回传。当前个人环境未应用本批Redis部署，也未声明正式页面已可用。推荐下一批接通Redis部署发现与System精确来源关联，再接查询展示。


## Redis 自动发现与 System 精确来源关联（2026-11-01）

本批为共享工作区实现，尚未提交／推送。Redis复用唯一v2发现文件、System批量确认与CAS／审计／主机绑定、既有Monitor后台最小投影和HTTP SD；不新增权限、角色、租户引擎或业务键读取。发现校验本方工作区摘要项目、scope／Owner／服务、实际数据库容器ID及启动时刻、唯一私网网络身份与显式HTTPS发布端口，前后不一致时拒绝。来源与账号共享唯一项目命名函数；来源缺失／停用、数据库重启导致的标签失配分别表达，不借旧容器或PostgreSQL来源补齐Redis。System私有源只关联有效纳管部署，浏览器发现／声明不含端点。

相同形状的私有来源已统一为Common `EngineMetricsSource`，删除原PostgreSQL专属类型／文件，不保留别名；固定平台单容器目录集中定义PostgreSQL／postgres／5432和Redis／redis／6379。现有列表新增engine_type精确过滤，在计数／分页前执行；Monitor当前PostgreSQL页显式请求postgresql，Redis不被送入PG查询，也不提供尚未实现的专业页面入口。数据库端口缺失原因统一为database_port_missing，不保留旧SQL专用原因。System的原管理员确认界面已可同时确认两个作用域的Redis，默认名称按真实服务类型生成。

实际验证：

- `make test-module MODULE=system GO_ONLY=1`、`make test-module MODULE=monitor GO_ONLY=1`：Go T1退出0，日志 `/tmp/addp-redis-discovery-system-t1-filter.log`、`/tmp/addp-redis-discovery-monitor-t1.log`。Common `go test ./models ./client -count=1` 两包通过，日志 `/tmp/addp-redis-discovery-common-final.log`；包括Redis类型与来源键严格匹配及原Client完整性校验。
- `python3 scripts/test/business-deployments_test.py`：44项通过，覆盖两作用域实际形状、错误Owner／scope／数据库身份／启动时间／网络／端口、重复源／停用及PG事实独立性，日志 `/tmp/addp-redis-discovery-python-final2.log`。
- `make test-monitor-redis-source-unit`：两Go包及11项生命周期T1通过，日志 `/tmp/addp-redis-discovery-lifecycle-unit.log`。`make test-monitor-redis-source`：标准独占Redis T2退出0，日志 `/tmp/addp-redis-discovery-actual-source-final.log`。沿真实来源及数据库容器的Docker只读事实调用同一发现校验，只将项目命名函数替换为一次性命名空间，不放宽归属／身份／网络／端口规则。两作用域启动后available、数据库重启后旧源unavailable、重应用后恢复available且来源可读、停用后not_deployed均通过；沿用原mTLS、清权、重启持久化、损坏记录隔离和数据保留检查。项目 `addp-redis-t2-48920-48925` 及两套来源容器／网络／卷、私有配置卷退出后精确核对全零。
- 标准 `make test-system-iam-postgres` 的 `--package repository --test engine-deployments`、`--package api --test engine-deployments`、`--package migration --test engine-deployments` 分包串行退出0，实际连接经Infra归属／映射核验的addp_iam_test。日志 `/tmp/addp-redis-discovery-pg-filter.log`（repository/API）及 `/tmp/addp-redis-discovery-pg-final.log`（migration）。覆盖Redis登记／重复来源／CAS／停用、当前有效主机与后台身份加入及移除、类型过滤的计数／分页；迁移前拒绝Redis、261后允许、跨类型／服务清单拒绝、重复迁移和授权快照不变。没有操作addp开发库或新建database。
- `make test-system-frontend SYSTEM_FRONTEND_BROWSER_ARGS=e2e/engine-deployments.e2e.js`：156项单测、14项页面T3和构建通过，日志 `/tmp/addp-redis-discovery-system-browser-redis.log`。最终按真实类型限制专业跳转后，`SYSTEM_FRONTEND_BROWSER_ARGS="e2e/engine-deployments.e2e.js --grep Redis"` 再验156项单测、1项Redis批量确认／PG专用跳转T3及构建通过，日志 `/tmp/addp-redis-discovery-system-browser-redis-final.log`。此前13项原PG流程也通过，不把受控浏览器当正式用户T4。
- Monitor完整前端单测131项通过（`/tmp/addp-redis-discovery-monitor-frontend.log`）；同一标准 `scripts/test/frontend-browser-gate.mjs e2e/engine-resources.spec.js` 专项27项通过，含显式类型查询、自动筛选／刷新及返回；构建退出0，日志 `/tmp/addp-redis-discovery-monitor-build.log`。最初System／Monitor全量231／561场景已主动中断并由共享入口清理，不计全量通过、不修改其他会话浏览器进程。
- System标准Swagger生成和覆盖校验退出0，247个公开路由方法一致；日志 `/tmp/addp-redis-discovery-swagger.log`、`/tmp/addp-redis-discovery-swagger-coverage.log`。T2注册事实检查通过，日志 `/tmp/addp-redis-discovery-registration-final.log`；沿现有System IAM、Monitor及发现配置门禁自动发现，不新增CI Job或外部依赖。
- 完整 `make test-monitor-metrics-config` 的136项聚合本轮失败：两项既有ServiceExpectationPublicationTest停止夹具分别在fake ps的5秒和stop脚本10秒期限超时，未修复／延长超时，未计聚合通过，日志 `/tmp/addp-redis-discovery-config.log`。本批实际涉及的 `MetricsDeploymentConfigTest` 77项通过，日志 `/tmp/addp-redis-discovery-config-source.log`。脚本编译、语法与差异格式检查通过。

发布依赖：本批000261只扩展平台部署类型／清单约束，不更新任何声明、身份、权限或角色；当前259／260为其他会话未提交WIP，Catalog要求从000001连续编号，因此不能单独提交／推送261。保存本批路径与共享文件基线于 `/tmp/addp-redis-discovery-owned-paths.json`、`/tmp/addp-redis-discovery-baseline/`，后续提交须精确隔离共享catalog、System文档／i18n／Swagger中的他人变更。未擅自提交前置迁移、改号避开或发布缺号main。清单维护会话进展及依赖协调已尝试发送，但工具以空闲会话无活动turn拒绝，未声称送达。

Redis专业指标查询／页面仍待下一批；CI、Hosted和正式产品运行未在本会话查询、触发或重跑，等待专门会话回传；未设置定时任务、安装个人Redis账号或重启个人服务。推荐先完成前置迁移的发布顺序，再提交本批和开展Redis查询／专业页面。


## Redis 专业指标查询与页面（2026-11-01）

沿上一批共享工作区继续实现，功能尚未提交／推送：前置迁移259／260仍为其他会话未提交WIP，本批261不能越过连续Catalog发布。当前User、部署／有效主机、当前容器和来源在查询前后均重核，沿同一engine_resource_observations／summaries／trends接口和唯一Prometheus传输接入，不新增权限、租户归算或Monitor直连Redis。PG专用查询传输文件已删除，PG／Redis只保留各自固定目录及表达式，共用计划／预算、传输、批量摘要和归一化；不保留旧类型或方法别名。

Redis目录共22项：运行时长、五项内存容量及限额占用率、三项连接、六项计数速率及命中率、五项持久化／加载状态。详情分组10／7／5项，显式切换后只查当前组，沿用单次最多12项预算；列表按System类型过滤后分页，摘要固定四项。内存限额占用率不截断100％，maxmemory为零时无比例；无查找请求时无命中率。六项速率及命中率需要完整一分钟、至少四样本与边界、同一counter_epoch和完整新鲜快照；缺样、状态非法、重复／缺少代际、容器身份不符和可见重置均不输出旧值或补零。沿用当前容器运行段趋势，不宣称识别不可见重置。

Console原引擎页增加类型筛选、Redis分组、指标旁中英tip及语义状态；容量／时长复用共享格式化。趋势沿用现有共享缩放，手动／自动刷新保留图表DOM和视窗，失败或权限撤销清除旧值；System Redis部署的查看监控入口已开放。没有为Redis另建查询页或重复图表实现。

本地验证分层如下：

- `make test-module MODULE=monitor GO_ONLY=1`：最终全Monitor Go T1退出0，日志 `/tmp/addp-redis-query-go-final3.log`，含PG／Redis相同服务授权、版本／容器／来源／策略变化拒绝、12项预算和跨类型拒绝，以及Redis内存比例180％保留／非法状态拒绝。非本入口运行条件的集成用例仍由相应T2证明，不算T1已执行实源。
- `make test-monitor-redis-source`：最终标准门禁退出0，日志 `/tmp/addp-redis-query-t2-final2.log`。同一受管独占Redis夹具中，Infra／Business账号真实读取／越权拒绝、mTLS来源、精确发现、重启持久化、运行代际、停用清权、损坏记录隔离和失败清理全部通过。新增同一固定Prometheus镜像内promtool原生表达式15场景：内存比例、命令速率、命中率、AOF关闭及运行时长正确；零上限、零查找、重置、代际更换／重复／缺失、边界缺失、错误身份、非法状态及缺字段均无结果。该表达式层采用受控固定样本，不等同于真实HTTP SD持续采集或正式用户HTTP链路。专属promtool无网络、只读根目录及有限tmpfs，不新增镜像／运行依赖。
- 该最终项目 `addp-redis-t2-95846-95851` 及两个 `-metrics-infra／-metrics-business` 来源按精确标签独立复核容器／网络／卷均为零，两套私有配置卷亦为零；标准入口报告私有临时文件已删除。Prometheus浮点速率按原生45秒观测向一分钟外推的精确结果断言，不对产品取整／放宽；初次只读根目录缺少promtool临时目录已补限额tmpfs，不放宽长期容器权限。
- Monitor前端 `npm test` 132项通过，日志 `/tmp/addp-redis-query-ui-unit-final3.log`；标准 `node ../../scripts/test/frontend-browser-gate.mjs e2e/engine-resources.spec.js e2e/redis-resources.spec.js` PG27＋Redis5共32项通过，日志 `/tmp/addp-redis-query-ui-final2.log`。补齐owner类型归一跳转前的过期请求检查并整理目录后，Redis专项5项T3再次通过，日志 `/tmp/addp-redis-query-ui-redis-final3.log`，覆盖双语言／直接详情按owner类型归一、分组10／5／7项、tip、零上限／无查找、类型列表返回、撤权清空及手动／自动刷新保留真实canvas和缩放。`npm run build`通过，日志 `/tmp/addp-redis-query-ui-build-final3.log`。这些为受控后端浏览器证据，不是个人Safari或正式用户T4。
- System `make test-system-frontend SYSTEM_FRONTEND_BROWSER_ARGS="e2e/engine-deployments.e2e.js --grep Redis"`：156项单测、1项Redis确认及PG／Redis监控跳转T3和构建通过，日志 `/tmp/addp-redis-query-system-link.log`。未重跑／声称全System浏览器或全Monitor浏览器通过。
- 标准Monitor Swagger生成及覆盖校验通过，96个公开路由方法一致，日志 `/tmp/addp-redis-query-swagger.log`、`/tmp/addp-redis-query-swagger-coverage.log`。既有Monitor Go／前端和Release Redis来源门禁已覆盖本批；Redis T2登记增加resourcequery输入及同一夹具内promtool服务，注册事实检查通过（24／14／9／2），日志 `/tmp/addp-redis-query-registration.log`。无新CI Job、Hosted身份、权限或自动化。

另收到既有Platform CI的确切失败证据（源0e69a25b1，run41304702173／job124842615454），根因在infra-port-resolution.sh临时停止夹具未复制观察来源新依赖。已仅补齐Redis入口及唯一共享私有状态／Docker／发现帮助文件，显式隔离并关闭本夹具Redis来源，再断言未安装停用状态。现工作区和冻结HEAD加单文件改动均沿 `bash scripts/test/infra-port-resolution.sh` 通过，日志 `/tmp/addp-redis-query-ci-fixture-fix.log`、`/tmp/addp-redis-query-ci-fix-frozen-head-final.log`；独立提交并推送 `db402ce4c965c402854ab6a008b6bd276b89349e`，不包含Redis功能或前置迁移。未查询／重跑该CI，不把本地修复当作线上Job已通过。

正式System确认→Prometheus真实HTTP SD→当前User查询→Console全链路、CI／Hosted和个人Redis应用未在本会话运行；继续由专门会话回传，不新增定时任务、不启停个人服务。前置提交后应先精确隔离共享文件并发布本批，再讨论／安排Redis正式全链路验收；现有PG的Hosted用途不能自动扩大为Redis。

本批已按湖南项目约定主动发送上述进展至p0维护会话；消息工具仍以“without an active turn id”拒绝空闲会话投递，未送达、未改XLSX。证据和发布依赖已保存在本节及现有设计状态中。


### Redis 查询发布前的混合摘要与超限呈现复核（2026-11-01）

用户继续推进后，只读确认前置259／260仍未提交，本批261及Redis查询／页面继续保留工作区，不越过发布条件。补足新共享查询路径的有意义边界验证，不扩大产品目录、权限、预算或Hosted用途：同一次PG／Redis批量摘要只发两次共享form读取，按类型分别归一化；有效零值和180％Redis内存占用率保留，Redis快照失败仅清空本部署，外来deployment标签或跨类型指标导致整批拒绝返回。`make test-module MODULE=monitor GO_ONLY=1` 全部通过，日志 `/tmp/addp-redis-query-summary-transport-go.log`。

Console同一Redis受控夹具补中英文“超过100％→未设置上限”场景，验证列表／详情保留180.00％、趋势真实props不裁剪且原生悬停显示180.00％，切换无上限后比例为空并显示未设置上限；未更改产品公式、展示或轴逻辑。标准 `node ../../scripts/test/frontend-browser-gate.mjs e2e/redis-resources.spec.js` 最终7项T3通过（15.9秒），日志 `/tmp/addp-redis-query-over-limit-browser-final.log`；复用原刷新／缩放／tip／分组五项，不计正式Safari、User认证或实源页面T4。只有测试夹具及断言新增，前一轮产品构建／原生PromQL与实源T2输入未改变，不重复运行仍有效的T2或扩大为整套浏览器通过。

System说明已同步为PG／Redis按当前User事实选择固定目录，未混入同文件的其他会话IAM变更。既有路径清单仍在 `/tmp/addp-redis-discovery-owned-paths.json`，发布前有界工作区摘要在 `/tmp/addp-redis-publication-review-snapshot.json`：72项本方路径、共享文件仅按本方hunk处理、前置迁移未提交状态；它是复核快照，不是已暂存／提交内容或永久证据来源。索引保持不动、格式检查通过。CI和Hosted继续等待专门会话回传，未查询／触发／重跑或设置定时任务，未修改个人服务／数据库／配置。


2026-11-01 22:50（Asia/Shanghai）继续只读核对：259／260仍未提交，本批索引未暂存。现有设计增加“Redis正式全链路验收提案（待用户确认，尚未实施）”，具体列出同suite独立redis-engine-admission的当次两Redis、原管理员／Prometheus身份用途、固定指标／缺比率、真实页面、Business来源停用恢复及完整清理；没有修改有效T4身份例外或实现该profile，不将一般继续开发解释为扩大管理员测试用途。提案边界检查确认当前runner及人工workflow都未增加此名称。仅修正Monitor即时指标Swagger标题的多余空格，标准gen-swagger和覆盖校验通过，96个公开路由方法一致（`/tmp/addp-redis-query-swagger-final-review.log`、`/tmp/addp-redis-query-swagger-coverage-final-review.log`）；未改变API行为，不重复仍有效的Go／T2／页面验证，未运行CI／Hosted或定时任务。先请求限定测试用途确认，其他现有授权的发布准备继续保留。

## Redis 正式全链路验收准备（2026-11-01）

用户确认限定用途后，先更新既有《平台观测与语义生命周期》Redis条款，再为同一`platform-node-metrics`增加唯一人工`redis-engine-admission`选项。沿既有正式三员／真实密码MFA、独立Prometheus与当前User链路，不新增权限、角色、测试身份旁路或定时任务。复用原PG的登记／查询／浏览器核对流程，按真实类型选择固定目录；Redis三组10／7／5项、四项摘要和当前容器趋势均须读取，不因单次12项预算拆成隐式后台扫描。PG原profile继续独立。

物理夹具只调用标准Business Redis Compose和唯一`redis-metrics.py`生命周期，生成当次且相互独立的管理／只读凭据；来源进程仅收到自身作用域所需管理凭据，不带业务只读密码、其他数据库密码或System身份。Redis不走PG认证目录，不增加数据库重启场景、键流量或加压。Business纳管停用／恢复和采集来源暂停／恢复各自验证旧值清空、Infra仍有效及新鲜采样恢复。候选确认按精确作用域／服务选择当次两行，不使用表头全选混入另一类引擎。

来源项目名复用生产解析内核，解析禁止写Python缓存，以免改变Hosted只读预检的干净工作区。除Compose项目外，对生产owner管理的两个专属私有输入卷做启动前占用拒绝和退出零残留核对，不盲删外来卷。Business凭据不可读取时仍尝试清理Infra，并保留整体失败；本地模拟Docker测试不能证明真实Linux退出已清零。

本批本地证据：

- `PATH=/opt/homebrew/opt/openssl@3/bin:$PATH make test-node-metrics-online-runner`退出0：173项Python确定性测试及System Metrics身份Go夹具通过；包含真实OpenSSL证书准备、独立Python CLI、严格scope／owner／私有文件、成功与各阶段失败清理、既有PG回归、人工选项登记、22项目录／分组、零上限／无查询比例缺失、私有字段拒绝、两作用域全部三组及趋势浏览器回执。日志`/tmp/addp-redis-hosted-runner-final.log`。其中CLI使用模拟Docker，不冒充实际来源／Hosted。
- `make test-protection-deployment-runner`退出0，39项通过，证明共享Hosted生命周期新增的空专属卷列表不破坏其他既有使用方；日志`/tmp/addp-redis-hosted-shared-lifecycle-final.log`。没有启动或重跑这些场景的真实Hosted。
- `python3 scripts/ci/check-online-ci-registration.py --repository .`退出0，日志`/tmp/addp-redis-hosted-registration-final.log`；现有手工workflow／标准owner入口已登记，无新Job、schedule或放宽期限。shell、Online浏览器脚本语法及`git diff --check`通过。
- Console标准 `npm run test:e2e -- e2e/engine-resource-navigation.spec.js`最终4项T3全部通过（34.9秒），日志`/tmp/addp-redis-hosted-console-navigation-final2.log`。新增Redis分组／刷新／返回保持同一iframe与类型，保留原PG双语自动筛选及语言tip场景；这是受控后端集成T3，不是正式身份T4。
- 前置259／260已由其他会话提交于`7da941ee6`；本批261和产品链路可顺序发布。该提交后`make test-module MODULE=common GO_ONLY=1`、`MODULE=system`、`MODULE=monitor`均退出0，日志依次为`/tmp/addp-redis-release-common-go-final.log`、`/tmp/addp-redis-release-system-go-final.log`、`/tmp/addp-redis-release-monitor-go-final.log`。这些只证明Go T1，不代替先前独立T2或未运行T4。

最初聚合runner在实现中曾因旧PG回执缺少metrics、临时CLI操作系统探测和Bash空数组／解析产生缓存而失败；均按根因补齐唯一生产依赖或严格回执后再由上述最终完整入口验证，不放宽值／身份／清理断言。Console首次专项中新Redis集成和既有语言tip通过，另两项原PG筛选精确请求断言遗漏本批新增engine_type；只同步明确的类型契约，不放松请求次数、URL或iframe断言。

本地Redis实源T2／原生PromQL和Monitor／System受控页面证据仍按前节有效范围独立保留。正式RedisSystem确认→HTTP SD→Prometheus→当前User→真实Console以及全部真实退出清理尚未运行，CI／Hosted等待专门会话安排及回传；未查询、触发或重跑CI／Hosted，未应用到个人Redis或重启服务。

## 引擎当前对象变化的原位刷新保护（2026-11-02）

Redis正式入口提交后继续开发，发现同上下文保留未覆盖已重新读取的System对象事实：列表／详情先更新部署，但旧指标与趋势要等后续观测返回才替换。受控T3在指标响应暂缓期间，真实复现“当前无有效主机关联”与旧“正在采集／1.00 GiB”同时显示。先沿原6.3失效规则明确System对象版本边界，再在唯一引擎页核对部署ID、类型、名称、版本、纳管状态、当前node_id及绑定状态；变化立即清空旧即时值／趋势及缩放。相同事实的新对象引用不触发清空，保留原手动／自动刷新视窗和canvas，不增加接口、权限、指标或查询。

新增PG／Redis×列表／详情×停用／解绑／版本变化12项浏览器场景；修复前12项均在旧内容断言失败（`/tmp/addp-engine-owner-refresh-red.log`），修复后同一标准 `npm run test:e2e -- e2e/engine-resources.spec.js e2e/redis-resources.spec.js` 共46项T3通过（1.7分钟，`/tmp/addp-engine-owner-refresh-green.log`）。同时复验旧单位／零值、tip、故障撤权、跨身份、两语种导航、主机关联、Redis分组及超过100％、缩放与原位刷新。132项前端T1通过（`/tmp/addp-engine-owner-refresh-unit.log`）；两页最终构建通过（`/tmp/addp-owner-refresh-build-final.log`）。Console标准 `npm run test:e2e -- e2e/engine-resource-navigation.spec.js` 4项集成T3通过（39.7秒，`/tmp/addp-owner-refresh-console.log`），包含Redis iframe及PG双语筛选／提示。上述是受控后端T3及构建，不是个人Safari、来源T2或正式身份T4。

同类主机详情保护与独立页面验证见[节点专题](Monitor节点与主机资源历史证据.md#主机详情当前对象变化的原位刷新保护2026-11-02)。既有标准浏览器入口自动发现两份修改的spec，Platform CI原Monitor前端owner门禁覆盖本批；无需改Makefile／workflow或扩大测试身份。CI／Hosted继续等待专门会话回传，未查询、触发、重跑、设置定时或接管个人服务。

## Redis 来源状态与恢复的受控页面验收（2026-11-02）

沿现有Redis固定目录及前端状态契约补验收覆盖，本批没有新增产品状态、API、权限、采集或部署动作。复用唯一PG／Redis受控夹具，分别模拟未部署来源、来源不可用、暂无采样、抓取失败、快照无效、样本过期；采集状态／时间及空点按既有后端分支构造。夹具旧`sql-failed`模式统一改为`snapshot-unavailable`，删除原名，不为Redis保留另一套SQL或来源错误路线。

中英文列表／详情逐一验证异常状态直接可见、当前数值为“—”、恢复后显示本轮不同读数；部署停用／恢复仍沿System对象事实与版本变化。没有有效来源的趋势为空；当前抓取失败／快照无效／过期可与先前有效历史点共存，只将当前缺样置空，不把缺样补零或抹掉合法历史。另补两语种自动刷新：来源不可用后清值，隐藏时所有部署／指标读取暂停，显示时只恢复一次；HTTP403清空数值／canvas并停止自动请求，时间推进及隐藏切换均不能绕过，只有手动重读后恢复。使用受控HTTP与Playwright时钟，不执行真实来源启停或设置应用定时任务。

最终标准 `npm run test:e2e -- e2e/engine-resources.spec.js e2e/redis-resources.spec.js` 52项T3通过（1.8分钟，`/tmp/addp-redis-source-states-final.log`）；含新增6项组合场景和原PG／Redis全部已有断言。共享夹具其他直接消费者也沿标准浏览器门禁复验：Console的`engine-resource-navigation.spec.js`4项及`engine-monitor-navigation.spec.js`3项通过（`/tmp/addp-redis-source-states-console.log`、`/tmp/addp-redis-source-states-owner-navigation.log`）；Monitor的`automatic-filter-enter.spec.js --grep 'engine Enter submits'`1项通过（`/tmp/addp-redis-source-states-enter-final.log`）。最初额外Enter命令的`^engine Enter`正则未匹配Playwright完整标题，报“No tests found”，不计通过；修正调用筛选后实际运行1项，无产品或断言放宽。

对本批三项测试路径调用现有`changed-gate.py`影响函数，精确命中console／monitor；既有两前端CI门禁会自动发现，不新增Makefile／workflow。产品源码、依赖、构建输入和T1输入未变，复用上一批有效构建／单测，不重复无变化的来源T2或Go门禁。此次共60项受控T3，不证明个人Safari、真实HTTP SD／Prometheus或正式身份T4，也不计为完整Monitor／Console浏览器门禁。CI／Hosted仍等专门会话安排回传，未查询、触发、重跑或设置定时任务。

## MinIO 指标身份生命周期与隔离权限（2026-11-02）

本批按已确认的17项边界先实施账号阶段：Infra／Business分别创建仅`admin:Prometheus`内联策略的原生Access Key；部署工具核验本地Engine、Compose归属、唯一当前容器及具创建时刻的数据卷身份。0700／0600私有状态保留同一AK／SK，正常重复启动不切换账号、不改写凭据；未安装且关闭不访问Docker，已安装则沿标准停止入口停用并核验旧Token被拒绝。账号归属、父用户或策略漂移、丢失凭据及创建结果未知均不收养或重建。个人环境未安装、启用或重启，没有改ADDP权限。

真实隔离测试暴露固定MinIO版本的原生enable／disable会在只更新状态时移除内联策略。已删除产品中的此调用，镜像内专用工具复用已锁定mc模块的管理SDK，在一次请求中同时提交固定策略与状态；只接收私有stdin，固定回环端点、16KiB输入及五秒请求预算，未知策略输入拒绝。固定版本读回资源通配符为`**`，只接受这一原生形状和明确非继承策略。最初未完成的协议／策略及夹具尝试均不计通过，没有降低鉴权或权限断言。

最终`make test-monitor-minio-observation`于macOS Docker Desktop／Linux ARM64一次性环境通过（`/tmp/addp-minio-observation-t2-verified.log`），内含14项确定性T1。两个MinIO使用不同随机部署根凭据，无宿主端口；验证真实v3指标200、过期／错误签名403、另一scope凭据403；先用根身份证明相同S3签名请求有效，再验证采集身份的原生GET／PUT返回403及XML `AccessDenied`，列表／对象元信息与账号新增／列举也被原生鉴权拒绝。启停重读最小策略，旧Token停用失效，两scope互不影响；两容器保卷重建后同一身份及权限保持。工具拒绝额外任意策略输入且不改变账号。退出核验当次容器、网络、卷、独占镜像标签及私有状态清理；最终报告containers/networks/volumes/private state均为0。

相关标准回归：最初`make test-infra-runtime-log-lifecycle`的37、146、142项分组及原Go夹具通过（`/tmp/addp-minio-observation-lifecycle-regression.log`）；最终取消Infra重复启动前多余的账号停用后，组合复验的前两组通过，末组142项有1项未改动的HTTP超时夹具失败：`test_native_timeout_reaps_owned_group_without_completing_the_stage`读取PID文件时不存在（`/tmp/addp-minio-observation-lifecycle-final.log`），不计整组通过，不调整超时或断言。受影响的实际Infra启停用例另由`python3 scripts/test/infra-runtime-log-lifecycle_test.py`独立复验，40项通过（`/tmp/addp-minio-observation-lifecycle-scoped.log`）；`make test-deployment-discovery`（44＋6项及Shell语法，`/tmp/addp-minio-observation-discovery-regression.log`）；`bash scripts/test/infra-port-resolution.sh`（`/tmp/addp-minio-observation-port-final.log`）；`make test-t2-ci-registration`（75＋9项及登记检查，`/tmp/addp-minio-observation-registration-post-image.log`）；`make test-infra-minio-cache`（15项，`/tmp/addp-minio-observation-cache.log`）；构建登记检查通过。停止脚本拷贝夹具已携带策略目录，避免可选账号模块缺少依赖；复用该MinIO镜像的门禁输入登记同步补齐，两份原根构建上下文统一为实际Infra上下文。其他领域原生T2未在本批重跑，不以这些登记检查替代它们。

根Makefile与Release/T2 workflow已注册`test-monitor-minio-observation`，使用既有固定MinIO构建缓存；相关源码及标准生命周期变化可命中该门禁。CI／Hosted未查询、触发、重跑或设置定时任务，继续等专门会话回传。未实现固定17项来源适配、mTLS输出、自动发现／System精确来源投影、Monitor查询及Console页面；本次账号T2不等于完整MinIO专业监控、正式用户T4或生产T5验收。


## MinIO 固定 v3 指标与 mTLS 来源（2026-11-02）

沿已确认17项范围接通部署侧来源，未新增产品API或权限。`internal/miniosource`固定读取四个v3端点和前后进程边界，每轮三秒、最多五请求、每响应256KiB／4096样本，不使用代理、重定向、DNS、重试或旧缓存。严格验证固定原生标签／类型、数值和同一进程，输出前移除全部原生标签；计数仅携带当前部署／进程的摘要代际。只输出原始数值，速率／占比仍留给后续唯一查询层。对象扫描时间按该端点的实际读取时刻计算，与整轮采集时间分别记录。

固定上游的`MetricValues.Set`省略零值；真实原生端点确认健康时的离线盘数、在途／5xx及尚未扫描的对象统计可能缺席。来源保持缺样，不据HTTP200补0，不切换v2或其他接口，也不承诺17项健康时全有值。原生接口权限仍为独立`admin:Prometheus`，过滤只限制输出，不冒充权限隔离。

Redis／MinIO在Monitor内部复用唯一固定HTTP/TLS服务和受管来源部署内核，删除原Redis重复实现；各自保留协议、账号和字段适配。mTLS TLS1.3只允许无query／请求体的GET /metrics，单并发、完整缓冲、失败503且无失效数据。标准Infra／Business启停将受限账号与来源一起调和，私有配置复制到独立卷，非root／只读／0.25CPU／128MiB、只连核验后的标准数据库私网。失败或关闭先移除本方来源／配置卷，再停用已核验的账号，清理未证实时单独报错；运行来源不持root凭据、不挂载数据目录／Docker Socket，不收养陌生来源或账号。

实际验证分层：

- `make test-monitor-minio-observation-unit`：最终21项Python账号／来源T1及MinIO解析、配置、HTTP边界和受影响Redis Go T1通过。覆盖多网络下只选择明确标准网络、网络归属／歧义／公网拒绝、缺省关闭不访问Docker、权限漂移、未知创建、失效清理、清理失败标记、外来来源拒绝，以及固定字段、缺样／显式零、代际、扫描时刻、恶意标签／类型／数值／重复／预算与HTTP并发／请求体失败不泄露。另组合两份生命周期测试共30项通过（`/tmp/addp-minio-source-unit-complete.log`、`/tmp/addp-minio-source-lifecycle-final.log`）；最终T2入口再次执行19项及全部上述Go包（`/tmp/addp-minio-source-t2-final.log`）。
- `make test-module MODULE=monitor GO_ONLY=1`：Monitor Go T1通过（`/tmp/addp-minio-source-monitor-go.log`）。未运行的集成测试不计通过；本批公开API契约未变，无Swagger变更。
- `make test-monitor-minio-observation`：macOS Docker Desktop／Linux ARM64独占双MinIO T2通过（`/tmp/addp-minio-source-t2-final.log`）。实际验证原生指标权限、S3对象读写／元数据／用户管理拒绝、过期及错误JWT、跨scope凭据；两独立CA的真实mTLS、无证书／错误用途／另一CA拒绝、封闭路由、固定输出不含原生标签／凭据及运行安全配置；账号停用且来源存活时503无旧值，恢复后重新成功，标准停用／缺失TLS的清理不影响夹具对象。两MinIO保卷重建后同一AK/SK、对象保留、新来源绑定新容器及计数代际。退出报告containers/networks/volumes/private state均为0。
- `make test-monitor-redis-source`：受共享HTTP/TLS与部署实现影响的原生Redis T2通过（`/tmp/addp-minio-source-redis-t2.log`）。包括双scope真实来源、现有精确发现、账号清权／持久化、数据库重启及计数代际、坏记录隔离／恢复／失败清理，另含15项真实PromQL案例；本方来源、配置卷、夹具资源和私有材料清理通过。
- `make test-t2-ci-registration test-build-registration`通过：75＋9、60＋9项及登记检查，新增来源镜像和两来源门禁输入闭包已命中既有Monitor／Business选择（`/tmp/addp-minio-source-registration.log`）。`bash scripts/test/infra-port-resolution.sh`和相关Shell语法通过（`/tmp/addp-minio-source-port-safety.log`）。
- `python3 -m unittest scripts/test/infra-runtime-log-lifecycle_test.py`40项中39项通过，`test_disabled_metrics_stop_only_owned_control_and_preserve_core`原30秒外层预算超时，未获得该次完整输出，未认定根因、未扩大预算或调整断言（`/tmp/addp-minio-source-infra-unit.log`）。双来源门禁结束后，沿该文件标准入口单独定位同一原用例：`python3 scripts/test/infra-runtime-log-lifecycle_test.py InfraRuntimeLogLifecycleTest.test_disabled_metrics_stop_only_owned_control_and_preserve_core`在2.874秒通过（`/tmp/addp-minio-source-infra-control-diagnostic.log`）；保留原组合失败，不将单项通过改记整组通过。

最初一轮编译因扫描时刻参数替换误触JWT调用失败，在启动夹具前修复；随后来源T2的保卷重建检查要求请求计数，但新进程还无S3调用，固定原生端点按正值发布规则没有计数代际。夹具补入重建后对自有对象的真实GET，同时验证对象持久化及新计数；不改产品输出、补零、降低边界或等待伪造样本。最终上述来源T2完整通过，前次失败及物理清理结果独立保留。

本批没有自动发现／System精确来源投影、Monitor固定查询和Console专业页的MinIO实现；不计作完整17项专业监控或正式用户T4／生产T5。没有应用个人配置、安装个人账号、改数据库／Docker设置、重启个人服务、查询或触发CI／Hosted，继续等专门会话回传；没有设置定时任务。


## MinIO 自动发现与 System 精确来源关联（2026-11-02）

- 范围：Infra／平台自管Business单容器MinIO，沿唯一部署发现v2、管理员确认及既有主机关联、私有投影和HTTP SD。无自动纳管、租户Engine／S3数据读取或新增IAM权限。平台目录沿发现协议使用minio，登记只保留9000 API端口；000262仅扩展类型与服务匹配约束，不改历史迁移、声明或权限。
- 只读发现核验工作区／scope唯一来源项目、Owner、数据库完整容器ID／启动时刻、单一来源网络、Compose网络归属及显式HTTPS端口。Infra数据库多网络时来源仍只加入addp-network，Business为business-network。Redis／MinIO复用单来源发现内核，保留Redis原网络集合约束；缺失、错配与停用分别失效，不跨类型顶替。端点不进入公开DTO、确认摘要或持久声明。
- System严格JSON白名单增加minio_metrics，仍拒绝未知、重复和大小写别名字段；当前来源只与单个运行容器精确匹配。私有投影须先有有效纳管主机关联，再经原Monitor准入进入HTTP SD，标签保留真实minio类型。管理列表支持MinIO候选和名称，专业指标页未适配前不展示错误跳转。
- T0／T1：`make test-deployment-discovery`通过45项发现测试和6项容器证明测试；`make test-monitor-minio-observation-unit`通过21项账号／来源Python及现有Go来源测试。`make test-module MODULE=common GO_ONLY=1`、同入口Monitor和System均通过。初次System失败包括目录版本断言未同步，以及JSON白名单遗漏，均已修正后复验；不把初次失败记为通过。
- T2：`make test-monitor-minio-observation`最终版本通过真实独占双来源mTLS、发现available／not_deployed、精确容器身份／启动时刻／HTTPS端点、重建恢复和原最小权限拒绝，容器／网络／卷／私有材料=0。`make test-monitor-redis-source`通过原真实发现、15项固定查询、生命周期及退出清理回归。均未启停或安装个人MinIO。
- PostgreSQL T2：标准入口`make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package <migration|repository|api> --test engine-deployments"`三包串行通过，实际addp-postgres映射25432、数据库仅addp_iam_test。覆盖Redis／MinIO前向发布及重复执行、权限不变、类型／服务错配拒绝、来源唯一性、有效主机关联、CAS及停用后退出私有投影、原HTTP审计事务。迁移回归参数化后再次通过migration包；未应用个人addp开发库。
- 前端T1／T3：`make test-system-frontend`通过158项单元、原233项受控浏览器和构建；新增MinIO确认场景后，`make test-system-frontend SYSTEM_FRONTEND_BROWSER_ARGS='e2e/engine-deployments.e2e.js'`通过158项单元、15项登记页浏览器和构建，包含Infra／Business MinIO显式主机关联、名称和确认请求。不是Safari真实运行或正式身份T4。
- Swagger：`bash scripts/swagger/gen-swagger.sh system`和`bash scripts/swagger/check-route-coverage.sh system`通过，248个公开路由方法覆盖一致，类型筛选枚举加入minio。`make test-t2-ci-registration test-build-registration`通过；既有门禁已登记公共模型、发现脚本、System迁移及前端路径，无需新增入口或Workflow。
- 本地日志：`/tmp/addp-minio-discovery-{python-final,common-t1,monitor-t1,system-t1-pass,system-pg,system-migration-final,t2-final,redis-t2,system-frontend,admission-ui,swagger,registration}.log`。临时路径不是长期交付附件；上述可重放标准命令和断言范围为证据正文。
- 剩余：MinIO固定查询、17项指标与Console专业页尚未实现；当前来源受上游零值省略约束，不能补造0。CI／Hosted由专门会话安排回传，本会话未触发、重跑或轮询，不建立定时任务。


## MinIO 固定查询与专业监控页面（2026-11-02）

- 已实现：沿原engine_resource_observations／summaries／trends接入17项固定目录，仍由System当前User裁决、有效主机关联及现有私有来源准入，查询完成前重核用户、来源、版本与策略。列表固定总容量／剩余容量／占用率／请求速率四项；详情容量／状态10项、请求／流量7项按需查询，不突破单次12项和现有批量预算。System原监控跳转已开放MinIO；不增加路由、权限、租户归算、告警或Hosted用途。
- MinIO独立基础采样守卫复用同一EngineScope和POST查询内核，校验唯一up、observed_at、start_time／uptime和当前容器边界。可选单项缺席不抹去其他有效项，不把上游省略的零值补为0。速率复用一分钟／四样本／边界／重置保护，并核对相同进程和counter_epoch；5xx占比无分母或计数错代时留空。扫描值要求真实最近扫描时间，允许该统计早于本容器，统计距今按评估时间计算。计数值拒绝小数或超出JS安全整数，容量和速率使用合适单位；提示紧贴指标。
- Go T1：`make test-module MODULE=monitor GO_ONLY=1`最终通过，包括MinIO两组／预算／跨类型拒绝、数值域、三类型混合摘要隔离及MinIO当前User撤权／来源／版本／策略变化丢弃旧值回归。没有把缺少T2环境时的跳过算作外部服务通过。
- 实源／PromQL T2：`make test-monitor-minio-observation`最终通过原双MinIO真实mTLS、账号权限／来源发现／持久化与清理，另由同一独占项目的固定Prometheus promtool通过40项（17项目录及23项缺样、重置、代际、容量、扫描和身份边界）。首次只有“扫描早于容器”夹具错误：更新容器启动身份后未同步输入系列标签，按现有精确身份守卫正确返回空；修正测试系列身份后40项通过，未放宽产品身份或断言。首次自有容器／网络／卷已只读核对0；最终门禁容器／网络／卷／私有材料=0。
- 共享回归：`make test-monitor-redis-source`通过原15项真实PromQL、Redis权限、mTLS、精确发现、生命周期及物理清理。Redis与MinIO的测试共用唯一promtool启动／临时文件／脱敏失败诊断实现，产品协议和各自目录保持独立。
- Monitor前端：最终`cd monitor/frontend && npm test`通过133项单元。完整`make test-monitor-frontend`首轮600/602通过，两项新增MinIO测试因tip选择器同时匹配“查看趋势”而失败；只修正为实际可访问名称。随后标准浏览器入口三类引擎专项61/62通过，剩余英文返回按钮的测试预期名写错，改为真实“Back to engines”后`npm run test:e2e -- e2e/minio-resources.spec.js`两项通过（含此前失败英文路径），并且`npm run build`通过。保留原始失败，不把原完整命令记为通过；600项未变场景与分批专项分别保留有效证据，未重复运行整套602项。
- 页面覆盖：MinIO中英分组与10／7精确请求、指标旁tip、无扫描／上游未发布单项空值、UUID直达时按System当前真实类型恢复及返回列表；PG／Redis／MinIO来源失效恢复、合法历史、权限撤销停止、隐藏暂停／恢复、手工和自动刷新保留DOM与缩放。受控浏览器不是Safari实际部署数据或正式身份T4。
- System前端：`make test-system-frontend SYSTEM_FRONTEND_BROWSER_ARGS='e2e/engine-deployments.e2e.js'`通过158项单元、15项登记页浏览器及构建，MinIO管理行沿原监控入口跳转。Swagger标准生成及96个Monitor公开路由覆盖通过。`make test-t2-ci-registration test-build-registration`通过；原MinIO独占T2入口已同步resourcequery输入与受限promtool服务，退出显式包含该profile并再次以真实门禁验证零残留；既有自动发现可命中新增Go／浏览器用例，无新Workflow或并行路线。
- 日志：`/tmp/addp-minio-query-{go-verified,t2,t2-final,t2-cleanup-final,redis-t2,frontend,ui-targeted,ui-verified,system-ui,registration-final,swagger}.log`。临时路径不代替正文的可重放标准入口。
- 尚未验证：个人MinIO采集配置应用／真实Console数值与曲线、Safari及正式身份全链路。未修改个人数据库／配置或启停服务。CI与Hosted等专门会话回传，本会话未触发、重跑或轮询，未设置定时任务。


### MinIO 专项 CI 回传（2026-11-02，dff267ed0）

专门CI会话确认提交`dff267ed00aef78f009dcc54ac33fe62305c7aa0`的[Release MinIO专项](https://github.com/pampa0629/addp/actions/runs/41424138332/job/125206871137)结论success，实际`Run MinIO observation source and identity gate`已执行（回执selected=true），不是跳过。原日志`/tmp/addp-ci-20261102-180000/Monitor-dff-MinIO-source.log`本地复核110015字节，SHA256=`e71e3c001fe447c870af4e77497d54a49ac4b8ee40e446bdcd2c1dfab2b6a8b6`；1113—1115行21项脚本检查OK，1275行真实固定PromQL40场景通过并移除promtool，1279行容器／网络／卷／私有材料=0。仅补记该独立专项通过，不推及Monitor完整前端、Release整批、Safari或Hosted。完整前端Job125206526177在回执时仍运行，由专门会话继续跟进；本会话只核对回传日志，未访问CI状态、重跑或设置定时任务。


### Linux 完整前端产品步骤回传（2026-11-02，2cd45ec2）

专门CI会话提供的[后继提交Monitor前端Job](https://github.com/pampa0629/addp/actions/runs/41429831469/job/125225339390)对应`2cd45ec2af6de07feed8e61d2df1d01962d8c0eb`。本会话离线复核其job.json、annotation及原日志：`Run Monitor frontend gate`实际completed/success（北京时间18:52:46—19:22:32），标准`make test-monitor-frontend`完成133单元／0失败／0跳过、602浏览器通过（29.5分钟）和Vite构建（10.91秒），selected=true；MinIO中英新增场景确在本轮602项内执行。这次仅登记精确提交的完整产品步骤通过，不是仅有build输出或被取消的产品步骤。

整Job结论仍为cancelled，官方annotation为`The job has exceeded the maximum execution time of 30m0s`，不能将完整步骤成功写成Job或Release全批成功，不加时限或重跑。原件目录`/tmp/addp-ci-monitor-2cd-step-success-20261102`；job.log为195494字节、SHA256=`69c4d256f3f23ad1431e684b921266757e1c0ed149d2978e1e7db95668cd03e1`，单元673—678行、MinIO场景1205—1206行、浏览器1563行、构建1592行、selected1597行。Git复核dff267ed0至2cd45ec2的monitor/frontend树均为`e901d153bb1a06b02658412f23d87339791f8d30`，但common-frontend的executionFailure／翻译及Makefile有变化，不能声称整个输入闭包等同于dff。此前本地失败与独立MinIO40场景记录仍各自保留。本条不增加修复任务、环境应用、Safari或Hosted验收，也不重复功能清单报表；个人真实来源联调仍待用户手工应用已准备配置。


### MinIO 原生计时基准差异导致整批误判（2026-11-03）

Infra来源实际UP后，Safari平台管理员的专业详情仍返回“未取得有效数据库快照”。只读取证确认：同次observed_at=1793664989、原生start_time=1793597423.35、uptime=71364.002917418，旧查询附加的abs(observed_at-start_time-uptime)≤5判定差3798.35秒，过滤全部指标。容器内/proc/1/stat的启动ticks=9035、/proc/stat btime=1793597333，按procfs的100Hz重建启动值恰为1793597423.35；uptime反推的应用启动记录为2026-11-02T04:27:04.997Z，与Docker实际启动04:27:05.532Z接近。固定MinIO源码的start_time来自procfs.StartTime，uptime来自time.Since(globalBootTime)，不能强行视为同一计时基准。本记录不推断底层时钟差异的历史成因，未修改Docker／时钟／电源设置或重启个人MinIO。脱敏原始计算在`/tmp/addp-minio-clock-proof-20261103.json`；上游事实见[固定版本进程指标](https://github.com/minio/minio/blob/9e49d5e7a648f00e26f2246f4dc28e6b07f8c84a/cmd/metrics-v3-system-process.go)、[应用启动记录](https://github.com/minio/minio/blob/9e49d5e7a648f00e26f2246f4dc28e6b07f8c84a/cmd/globals.go)、[procfs算法](https://github.com/prometheus/procfs/blob/v0.16.1/proc_stat.go)。

先明确运行观测规范的原生时间来源，再删除唯一MinIO查询路径中的错误加法等式；保留同次唯一UP／字段、16秒新鲜度、采集时刻偏差、当前容器身份／启动边界、非负值、一分钟四样本／完整窗口／重置／counter_epoch保护。不增加Desktop特判、容差配置、替代时间或补零。原API目录、DTO、权限和Swagger契约不变。新增六项真实PromQL情形：计时基准不同的容量／运行时长／请求速率有效，以及未来启动、当前容器前启动和负运行时长拒绝。原标准MinIO T2和既有Monitor Go门禁自动覆盖，无新增入口或CI编排。

旧查询沿`make test-monitor-minio-observation`实际失败，容量和运行时长的非空预期得到nil；账号、mTLS和持久化前置均通过，但整命令不计通过。原日志`/tmp/addp-minio-clock-red-20261103.log`；独立核对当次fb7240bce31d及两个来源项目的容器／网络／卷均0，见`/tmp/addp-minio-clock-red-cleanup-20261103.json`。修复后原标准MinIO命令退出0：21项Python检查、来源Go专项、双MinIO原生权限／mTLS／停用恢复／重建持久化及46项真实PromQL全部通过，containers/networks/volumes/private state=0；日志`/tmp/addp-minio-clock-green-20261103.log`。Monitor后端首次`make test-module MODULE=monitor GO_ONLY=1`有一项模拟Docker脚本生命周期测试在原30秒截止被终止；同期本机load约70、08:20—08:29无Sleep／Wake／DarkWake事件，未确认具体超时根因，不改原预算。待本轮T2退出后原样执行相同命令，退出0，resourcequery实际执行20.463秒，其余有效包缓存复用；保留首次失败`/tmp/addp-minio-clock-monitor-go-20261103.log`与最终通过`/tmp/addp-minio-clock-monitor-go-final-20261103.log`，不把第一次命令改记通过。末次只读核对个人Infra／Business MinIO原完整ID、启动时刻及RestartCount=0不变。真实专业页面仍待用户仅重启Monitor后复验，不将来源UP或上述隔离测试当作实际页面验收完成。CI／Hosted继续等专门会话回传，未查询、触发、重跑或设置定时任务。


### Infra MinIO 修复应用后的真实页面截图（2026-11-03）

用户确认重启Monitor后，公共ready显示运行构建`eb8af602ca8a494142cbbf201104884e92cd1433`，包含`04ce9ec57`且MinIO查询文件与该修复逐字节一致；System／Monitor／Prometheus ready及MinIO mTLS来源均200，来源14项样本、精确部署目标UP，Business仍not_deployed。只读收据为`/tmp/addp-minio-runtime-build-20261103.json`与`/tmp/addp-minio-after-monitor-restart-20261103.json`。

用户提供Safari实际截图，页面为平台系统管理员的Infra MinIO，关联原Docker Desktop Linux VM，显示正在采集：运行21小时47分钟5秒、在线1盘、总容量1.87TiB、剩余866GiB、空间占用54.71%、最近扫描对象10.1GiB／7408个／10桶、扫描距今1分钟25秒。容量／状态十项中九项显示真实值，离线盘数留空与此前原生省略事实一致，不补造0。空间占用率近五分钟曲线实际渲染，页面标注10:09:20—10:14:20、15秒评估间隔。本图补齐修复应用后的容量／状态及该趋势真实显示证据；不是自动化交互通过，也不据标签“自动刷新已开启”推导刷新、缩放或视口保持已经验收。

截图附件`codex-clipboard-8e405148-6c4f-483b-a5fd-c8f86b97559e.png`，SHA256=`37a38369c3d7917345f591f73bb8d90a2814e1b5fe1a97ae49827fb19e7bdb22`。后续工具读取时Safari再次报告隐藏并按原规则清除观测，不能用该稍后状态否定用户截图，也未绕过隐藏保护。请求／流量七项、真实缩放与手动／自动刷新保持尚待实操复验；Business和Hosted／T4未验。CI／Hosted仍由专门会话回传，本会话未设置定时任务或启停个人服务。

### 来源分区应用后的 Safari 真实值恢复与人工导航复验（2026-11-03）

用户应用System／Monitor重启及解锁后，Safari真实页面已出现Infra／Business来源页签。初次工具读取与用户截图均显示“页面隐藏，自动刷新已暂停”；Web Inspector只读表达式同时取得顶层与iframe的hidden，不能将指标空值当作来源失败。并排窗口后仍有hidden记录，不能认定遮挡就是本机历史成因。原生自动化输入后也再次出现hidden，本轮未修改产品代码、可见性或授权规则，不通过脚本强制visible，不把稍后恢复认定为历史根因已修复。

用户随后确认恢复；原生AX实际读到“自动刷新已开启”，Infra分区仅MinIO／PostgreSQL两张卡片，均“正在采集”：MinIO总容量1.87TiB、剩余866GiB、空间占用54.73%、S3请求0.07次/秒；Infra PostgreSQL连接55、活跃0、提交151.83次/秒、容量6.79GiB。Business分区仅Business PostgreSQL一张卡片，关联原Docker Desktop Linux VM，连接1、活跃0、容量78.3GiB，连续只读观察提交速率从0.58变为0.60次/秒；后一原生截图实际显示该数值与“自动刷新已开启”。这证明当次来源分区、真实采集数值及界面更新，不扩大为长时自动刷新／DOM保持或所有专业趋势已验收。AX与截图保留于本会话，不读取Token或业务数据。

用户按本会话步骤手动验证Infra搜索MinIO不按Enter即自动只剩一张卡片，进入监控详情再返回仍保留MinIO，并明确回复“自动筛选正常，返回后仍保留MinIO”。此项为人工实操确认；工具随后读取时页面已切至Business，未声称独立读到MinIO返回URL。完整Console／Monitor受控回归沿`250bcf176`原有效证据，不把人工确认改写成自动化回执。本轮只补运行证据，`git diff --check`通过；无新CI／Hosted、容量、定时任务、配置更改或个人服务启停。租户关联及租户级监控仍仅讨论，未实施。


### 统一采集管理入口首版（2026-11-03）

用户确认在保持独立采集来源、账号、凭据及 Compose 项目的前提下，先实现统一管理入口。新增 `scripts/infra/metrics-sources.py`，统一主机与 Infra／Business 的 PostgreSQL、Redis、MinIO 七项选择、状态、部署输入检查、安装／应用、停用和失败汇总；标准 Infra／Business 启停移除重复的来源调用清单，通过同一编排调用原有唯一生命周期实现。选中一个 Business 服务不会作用于其他服务；来源失败返回非零，核心启停继续。来源容器运行与真实采集成功仍分别表达，不将 Running 当成 Monitor 查询通过。

直接调用分别读取根 `.env`／`business/.env`，不让父环境采集开关和凭据混入另一域；Hosted 只读取显式隔离文件。标准已解析环境只允许以 `--resolved-env` 交接一个明确 scope，避免重新加载配置覆盖标准入口的实际注入值。停用不因启用标志无效而被跳过；操作超时报告结果未知，不自动重试。状态仅使用本地 Docker 元数据，核验 Owner、scope、来源组件集合、当前数据库完整容器身份及启动时刻，不读容器环境／业务数据。未部署、停止、组件缺失、归属冲突、数据库运行身份变化均不作为正常运行。

首版无账号修改的 `recover` 仅覆盖原 PostgreSQL 证明路径；完整 ADDP 启动仍只选择两域 PostgreSQL，正常来源保持不变。其他已启用来源明确报告尚不支持并要求显式 up，不以账号重新安装冒充恢复。主机来源须明确选择，不随 Infra／Business 自动启用；指标中心沿原 Infra 入口管理，不新增常驻协调器、定时任务、集中凭据或配置副本。指南已补统一操作和当前限制。

本地验证：`make test-metrics-sources` 最终15项通过；`make test-monitor-metrics-config test-t2-ci-registration` 退出0，136项指标配置、78项登记、9项 PostgreSQL runner 检查及实际登记核对通过，入口前置的13项旧计数由最终15项独立复验更新。`make test-business-config` 退出0，覆盖 Business Compose、部署发现、受影响启动入口及既有独立消费回归。最终 `make test-dev-lifecycle` 退出0：24项原生准备、206项部署／Worker／资源输入、15项统一管理、44项 HTTP runner、169项 Trace runner及145项生命周期／预算检查，端口与停机防护、原子构建和选择性启停脚本回归、共享 Go 包及四模块 cmd 编译检查均通过。三条现有 Monitor T2 来源门禁的输入声明已包含统一入口和新测试，Platform 原有生命周期／Business 入口自动覆盖，无新增 CI Job。`git diff --check`通过。

保留首次失败：独立指标配置门禁中两个无真实进程的停止夹具分别超过10秒／5秒，未唯一定位瞬时超时原因，未增加预算；随后同一136项及包含它们的206项原样通过，不将首次失败改记通过。完整生命周期回归先发现测试副本漏复制新入口，补齐后又发现旧原始 Redis 日志断言不适用统一输出；测试改为要求 Redis 行已停用和全部三项零失败，仍保留实际删除标记断言。最后完整标准命令通过。日志分别为 `/tmp/addp-metrics-sources-config-final.log`、`/tmp/addp-metrics-sources-dev-final.log`、`/tmp/addp-metrics-sources-dev-recheck.log`、最终 `/tmp/addp-metrics-sources-config-recheck.log`、`/tmp/addp-metrics-sources-business-final.log`、`/tmp/addp-metrics-sources-dev-verified.log`、`/tmp/addp-metrics-sources-unit-verified.log`。

本机仅执行统一 `status` 和 `check`，分别退出0，七项汇总失败0：主机、Infra PostgreSQL／MinIO、Business PostgreSQL启用且来源运行；Infra Redis、Business Redis／MinIO停用且未部署。部署输入检查通过四项，另三项未启用。记录在 `/tmp/addp-metrics-sources-local-status-final.txt` 与 `/tmp/addp-metrics-sources-local-check-final.txt`。本轮未在个人环境执行 up／down／recover，实际统一应用仍待部署者操作；未改个人配置、凭据、数据库或服务，不把本机只读状态作为新安装、mTLS、Prometheus、页面或完整七来源部署验收。CI／Hosted继续等专门会话回传，未查询、触发、重跑或设置定时任务。


### Redis／MinIO 已安装来源的无账号修改恢复（2026-11-04）

承接用户对统一采集管理的继续授权，先补正式规范和指南，再为Redis／MinIO接通recover；完整本地ADDP启动通过同一入口选择Infra／Business的PG、Redis和MinIO。删除原PG专属恢复函数名，保持完整启动／非Hosted守卫；单模块、Hosted、Node及显式未选择／未安装的来源不自动启动。来源恢复只读核验已有账号，不创建／启用／停用／轮换账号，不写ACL、持久记录、MinIO策略或数据库数据，不构建镜像，不启动数据库。显式up／down仍沿唯一原安装与停用实现，不作为恢复兜底；同时补齐MinIO底层down不因错误启用标志而跳过清理，错误选择的recover仍拒绝，最后24项T1已验证。

Redis核验精确启用ACL、已提交持久记录、密码摘要及私有安装；MinIO核验phase、原生账号归属／父身份／策略、启用状态及固定指标JWT读取。两者共用来源恢复：核验原私有Compose、镜像、Owner／scope／当前数据库完整ID；逐字段核对私有配置与凭据，逐字节核对证书。原容器ID相同且仅启动时刻／私网地址变化时允许重建；新数据库容器、停用／漂移／不完整安装、证书或端口变化拒绝。已有来源还核对非root、只读根、capabilities、安全选项、CPU／内存／PID预算及唯一只读来源卷，不只信任标签。私有卷读取与Redis纯账号配方生成复用同一短时无网络只读helper，随机标记限定回收，超时也检查并清理当次helper；不挂数据库数据、宿主目录或Docker Socket。健康来源容器与凭据保持；停止／缺失来源按原镜像恢复，重建前后重读数据库和账号，失败只清理本方来源、不变更账号。

本地最终标准门禁：make test-metrics-sources退出0，含5项共享恢复、15项统一编排及2项完整／局部／Hosted启动守卫与失败继续的可执行检查；Redis T1为14项、MinIO T1最终为24项，相关固定来源Go专项通过。make test-t2-ci-registration退出0，78项登记、9项runner及24个hosted-service／16个owned-compose／9个hosted-only／2个owner-managed实际登记核对通过。标准T2输入声明已登记新共享测试，原Platform／Monitor门禁自动覆盖，不增加workflow或定时任务。

最终make test-monitor-redis-source、make test-monitor-minio-observation均退出0，仅在命令PATH中使用本机已安装OpenSSL3.6.4，无系统配置变更。各独占两域数据库，真实验证健康来源ID保持、来源停止／删除恢复、数据库停止时拒绝且来源不变、同一数据库容器重启重新绑定、账号停用拒绝、原私有凭据／ACL记录不变、mTLS／精确发现恢复；MinIO另在保卷换容器后验证recover拒绝，再沿显式up重新应用。既有越权拒绝、失败关闭、持久化、数据保留及15项Redis／46项MinIO真实PromQL继续通过。临时容器、网络、数据／私有卷、helper及私有文件最终清零；当次源项目只读复核亦全0，见/tmp/addp-metrics-recovery-cleanup-verified.json。最终日志为/tmp/addp-metrics-recovery-{coordinator-final,unit-final,minio-shutdown-unit,source-runtime-unit,redis-verified,minio-verified,registration}.log。

首次MinIO T2在Compose start不支持--wait处失败，临时对象已清理；改为单一up --no-recreate --no-build --wait，只启动原临时容器，未改Docker版本或增加兼容分支。中间有效T2在共用helper及运行权限检查完善后重新执行，以最终verified日志为准。make test-dev-lifecycle首次有4项Worker夹具失败及1项停止夹具超时，未唯一定位瞬时原因、未改时间预算；容器门禁退出后串行复验原24项和206项通过，随后44项HTTP与169项Trace单测通过，但被其他会话Security Online v34登记校验阻断，整体仍退出2，后续145项等未执行，不计整套通过。本会话未修改该登记或其他owner实现。失败及受阻证据分别保留/tmp/addp-metrics-recovery-dev.log与/tmp/addp-metrics-recovery-dev-final.log；本批新增启动接线由上述2项独立标准入口验证，不用聚焦通过冒充完整门禁通过。

本轮未对个人Infra／Business执行up/down/recover、重启或修改配置、凭据、账号与数据库业务数据；个人环境恢复与跨Docker重启的实际应用仍未验。CI／Hosted继续等专门会话回传，未查询、触发、重跑或设置定时任务。不把本地T2、来源Running或零残留扩大为正式身份页面验收。


### Infra MinIO 恢复的原镜像不可用诊断（2026-11-04）

用户执行统一`recover --scope infra --kind minio`返回泛化“操作失败”。本轮只读分段核验：私有账号phase=enabled、原父身份／数据卷绑定匹配，原生账号on、固定指标JWT认证HTTP200，TLS及原Compose声明匹配；在原来源image inspect处失败，Docker明确No such image。来源容器仍Running、其.Image与私有Compose保存的完整镜像ID一致，但该ID已不在当前本机镜像库，其他已有tag不能替代原受管镜像。没有证据证明镜像何时或由谁移除，不归因为某次清理或Docker缺陷。

恢复继续拒绝镜像不可核验，不放宽归属、不自动构建／拉取／替换。修正的是故障可诊断性：唯一来源内核将镜像读取失败转换为封闭source-image-unavailable，Redis／MinIO CLI只输出白名单reason，统一管理翻译为“原采集镜像不可用；请通过标准指标启动入口重新构建并应用来源”，不回显原始Docker输出、凭据或其他未知reason。用户须显式运行`python3 scripts/infra/metrics-sources.py up --scope infra --kind minio`重建本方来源；本会话未执行该动作，实际镜像恢复与后续采集未验。

先增缺失镜像回归，旧代码在Redis／MinIO两case均因无封闭reason失败（/tmp/addp-metrics-image-diagnostic-red.log）。最终make test-metrics-sources退出0，共享恢复7项、编排16项、标准启动守卫2项通过；make test-monitor-redis-source-unit test-monitor-minio-observation-unit标准门禁退出0，Redis14／MinIO24项及相关Go专项通过。门禁覆盖镜像失败不删除／构建／重建、两种CLI只转发已知reason、统一中文建议及未知reason不泄漏。日志/tmp/addp-metrics-image-diagnostic-{green,final}.log。只读调用新installed_image在本机同一镜像上得到正确reason，前后同一来源完整fact未变，收据/tmp/addp-minio-recovery-image-diagnostic-20261104.json。本次仅错误路径和提示变更，既有T2成功路径未改，未重复创建隔离部署；既有来源T2输入声明及Platform编排已覆盖六个修改文件，无新登记。git diff --check通过。个人服务未启停，未改配置、账号／权限／凭据、卷、镜像或业务数据；CI／Hosted仍等专门会话回传，无定时任务。


### 用户重新应用后的 Infra MinIO 采集恢复（2026-11-04）

用户已执行`python3 scripts/infra/metrics-sources.py up --scope infra --kind minio`，返回已应用、单项失败0。本会话随后只读核对：新来源唯一且运行，原私有Compose中的完整镜像ID已可inspect并通过工作区Owner校验；账号on，来源的数据库ID及启动时刻精确匹配当前原MinIO容器。既有Monitor准入客户端的真实mTLS GET返回200、14项原始指标、在线盘数1；Prometheus按显式来源端口定位到唯一目标，health=up，严格使用该目标完整标签查询在线盘数得到1。后续以timestamp(metric)核对实际scrape时间、以observed_at值核对来源快照，而非使用即时query的评估时间冒充新鲜度；当次scrape距今2.80秒、来源快照距今3.44秒，均新鲜。

只读收据为/tmp/addp-minio-source-reapplied-20261104.json。这补齐用户实际重新应用后的镜像可用、来源mTLS与Prometheus采集恢复，不表示已实操recover健康不重建、Monitor当前User API／Safari专业页面／趋势或正式Hosted通过。14项为原生实际发布，未将省略值补0或承诺17项全有。未启停个人服务、修改配置／账号／凭据或业务数据；本轮仅补证据，git diff --check通过，无产品代码变更及新测试部署，无CI／Hosted轮询或定时任务。下一步优先复验引擎监控页面真实值及趋势恢复。


### Infra MinIO 重新应用后的 Safari 真实页面与趋势交互（2026-11-04）

用户继续后，Safari已登录平台系统管理员。初次仍显示hidden，切Infra并刷新曾短暂显示无已纳管引擎，不能据此认定部署丢失；随后列表实际恢复Infra MinIO与Infra PostgreSQL两张卡片，进入MinIO详情后自动刷新开启。未修改可见性、认证或产品代码，不将恢复认定为历史hidden根因修复。

原生AX与截图实际核对详情：Infra基础设施、正在采集，关联原Docker Desktop Linux VM；在线盘1、可用总容量1.87TiB、剩余848GiB、空间占用55.67%、最近扫描对象10.5GiB／约8040个／10桶。运行时长由1天19小时17分更新至20分，对象数8040→8043→8042，扫描距今和15秒评估窗口同步变化；证明当次真实页面与短时自动更新，不能扩大为长期刷新稳定性。离线盘未发布时仍留空，不补造0。

空间占用率近五分钟曲线截图实际渲染，悬停07:43:16显示55.67%。原生拖动底部左滑块从全范围缩至约后69%区间，横轴从五分钟变为约3.5分钟；随后点击页面刷新，重新滚动至图，左滑块比例保持在原位置，横轴随当前时间平移。其间自动刷新将窗口07:42:23—07:47:23更新为07:42:38—07:47:38，图仍保持缩放比例；悬停07:45:53仍显示55.67%。这补齐本次容量趋势原生拖动、悬停、手动及一次自动刷新缩放保持。点击顶部刷新时工具自动滚至按钮，不能作为原滚动位置保持证明；未据截图推导DOM节点未重建或无闪烁。

再切请求与流量，AX读取S3请求0.04次/秒、4xx速率0.00次/秒、发送5.64B/s、接收14.9KiB/s；在途、5xx及其占比按现有缺样规则留空。实际截图显示请求速率曲线含约0.02—0.51次/秒变化，悬停07:45:39显示0.04次/秒，当前组与默认请求趋势已同步。未逐一操作全部17项趋势、七天范围、图例或错误规则，不扩大验收范围；最终停留Infra MinIO请求趋势。

证据为本会话原生Safari AX、截图和实际输入记录，时间约07:44—07:48（Asia/Shanghai）。本轮仅记录真实运行证据，git diff --check通过，无产品代码或新测试变更；未重启服务、改配置／账号／凭据，未读Token或业务内容，未查询／触发CI与Hosted或设置定时任务。下一项建议复验统一recover在本机健康来源上保持容器不变，实际命令仍由用户执行。


### 本机 Infra MinIO 健康来源恢复实操（2026-11-04）

用户再次执行`python3 scripts/infra/metrics-sources.py recover --scope infra --kind minio`，实际输出“来源运行正常，未重建（采集待验证）”，一项失败0；作为用户执行的本机实操证据，证明命令到达健康复用成功分支。后续本会话只读核验来源唯一且Running，当前来源StartedAt为2026-11-03T22:10:21.519864215Z；Prometheus同端口唯一目标UP，完整目标标签查询在线盘1，observed_at实际快照距今12.75秒。收据/tmp/addp-minio-healthy-recover-20261104.json。无该命令执行前的容器ID收据，因此不虚称独立工具前后逐字段对照，未重建依据为用户真实命令输出，采集正常依据为后续独立只读核验。

此前新镜像／mTLS／Safari真实值与容量／请求曲线交互证据继续有效，本项只补齐健康recover用户实操及其后采集新鲜度，不重跑既有T1/T2或扩大为跨Docker停机故障恢复／全部来源已验。本轮仅文档与只读取证，git diff --check通过；个人服务未启停，CI／Hosted仍等专门会话回传，无定时任务。后续优先补统一管理的主机来源恢复边界，保持主机显式选择与现有安全部署约束。


### 主机来源显式安全恢复（2026-11-04）

按已确认的统一管理方向，先修订正式观测规范，再接通主机recover。标准up在仓库外工作区私有目录保存安装证明，与up／down／recover共用非阻塞锁；down或失败up使旧证明失效。证明核对Engine／内核、活动模板及证书内容和文件身份、完整容器集合、镜像ID、权限／资源／挂载配置、网络身份及可选容量卷，不读取Config.Env、不保存私钥内容。恢复只启动完全匹配证明的既有停止容器；健康来源不改动，只有容量采样器停止则只启动采样器。缺失／停用证明、缺失来源、镜像丢失或安装漂移均拒绝，不收养旧部署、不构建或重建来源。启动回执丢失／中断的清理只停止本轮启动的原停止容器，并保留清理未确认提示。统一入口删除原Node不支持分支；只有显式--scope host才恢复主机，默认批量recover和全量ADDP启动均不恢复主机。

最终本地标准验证：`make test-metrics-sources test-monitor-metrics-config test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=node-recovery`退出0；共享恢复7项、主机14项、编排17项、启动守卫2项、配置136项通过。原生／Desktop输入签名、缺失／停用／漂移、私有文件与锁、镜像丢失、失败清理、Ctrl-C清理及其他操作已启动来源的保留均有T1覆盖。最终日志`/tmp/addp-node-recovery-final.log`。`make test-infra-runtime-log-lifecycle`及其HTTP／Trace runner、175项登记、145项生命周期回归通过；`make test-t2-ci-registration`的78项登记、9项runner及实际登记检查退出0。新增四个实现／测试输入均通过现有affected_modules矩阵验证选择Monitor，Release指标矩阵增加node-recovery，未触发或轮询远端CI。

新T2复用正式node_exporter、容量采样器和固定镜像，只在随机独占项目使用桥接命名空间、回环随机端口及受限私有tmpfs卷；不使用宿主根／proc／sys绑定、host网络／PID或Docker Socket。实际证明健康容器与证明文件不变、仅采样器停止恢复、两容器全部停止恢复、真实start生效但回执丢失时回收本轮采样器且保留原健康exporter；恢复后内存和容量成功指标经严格mTLS可读，容器与网络／卷身份不变。证书漂移、删除采样器及安装退役均拒绝，已有exporter和容量卷保持；退出容器／网络／卷／临时私有状态清零。夹具编写期间曾因本机缺少默认OpenSSL配置、测试环境漏带PATH及新证书缺少严格X.509扩展失败；已限定命令环境并补全证书扩展后原TLS校验通过，未修改系统配置或放宽验证。

T2通过的是受限桥接夹具中的真实恢复事务；其测试签名明确记录夹具文件，不冒充生产原生／Desktop输入签名联调。生产签名由T1覆盖，本机个人标准up生成证明及恢复后Monitor页面尚未操作，原生Linux宿主挂载实机恢复、CI／Hosted尚未验收。本机统一主机check一项失败0仅证明部署输入检查通过。个人服务／数据库／配置／凭据未改，未设置定时任务。下一步由部署者显式应用一次主机来源生成证明，再复验健康recover与Monitor真实采集。


### 本机主机来源应用及健康恢复（2026-11-04）

用户在具体命令已展示后要求本会话直接执行，本次限定授权执行`python3 scripts/infra/metrics-sources.py up --scope host --kind node`，并立即核验同范围recover；不扩展其他模块生命周期操作。up退出0、一项失败0，原node_exporter容器3d7e52c64c9f更换为4d48bf365796，唯一来源Running，仓库外addp.node-recovery/v1安装证明enabled=true；当前未启用容量采样器，无新增其他服务。recover退出0并明确“来源运行正常，未重建”，工具独立前后对照完整容器事实一致。两条命令均走正式统一入口，补齐生产Desktop输入签名与私有证明的本机实际联调。

随后使用现有Monitor专用mTLS查询身份只读核对：该主机UUID仅有一个host_resources目标，UP且lastError为空；up=1，内存总量67,304,611,840字节，load1=7.33，node_time_seconds所示来源时间距核验6.73秒。即时查询点的时间是评估时刻，不把该点与当前时间之差作为原始样本新鲜度；新鲜度依据为来源时钟与目标真实抓取信息。收据`/tmp/addp-node-recovery-live-20261104.json`保存up前后及recover后容器事实、证明状态和限定主机指标。未停止来源制造故障，个人停止恢复仍仅有既有隔离T2证据；本轮未操作Safari，不把后台UP包装为页面验收，原生Linux实机及CI／Hosted仍待验。未改配置、数据库或凭据，未启停其他业务与中心，未设置定时任务。

### 主机恢复失败原因的安全诊断（2026-11-04）

原恢复事务已有拒绝检查，但缺失和停用证明共用原因码，来源缺失／归属失败等错误丢失具体诊断。先补正式规范与操作指南，再把原检查结果通过固定原因码传给统一入口：区分证明缺失、已停用、读取校验失败、组件不完整、归属不符、部署输入变化、安装身份／安全配置变化及原镜像不可用。删除原合并原因码；未知码仍只有操作失败，保留独立清理未确认标记，不输出提供方原始日志、标识、私有路径或凭据。读取失败保留原异常类型，检查顺序、拒绝条件及启动／回收范围不变，不新增读取、自动修复或重试。测试夹具的注入字段同步避开unittest.fail方法，避免断言失败被同名字段遮蔽。

本地验证：新增原因断言先在旧实现失败（缺少／合并原因码），修正后`make test-monitor-metrics-config`退出0，涵盖共享恢复7项、主机恢复15项、编排18项、启动守卫2项和配置136项；最终主机15项还由下述T2入口再次执行，包含损坏／重复字段／错误类型证明和原私有文件拒绝路径。`make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=node-recovery`退出0，真实桥接隔离来源精确断言证明缺失、证书输入变化、组件被删除、安装停用四类原因；健康不变、仅采样器及全部停止恢复、回执丢失清理和恢复后mTLS指标均通过，容器／网络／卷／临时私有状态清零。日志分别为`/tmp/addp-node-recovery-diagnosis-config.log`、`/tmp/addp-node-recovery-diagnosis-t2.log`。

`make test-infra-runtime-log-lifecycle`退出0（HTTP44、Trace175及生命周期145项等）；`make test-t2-ci-registration`退出0（登记78、runner9项及实际门禁登记检查）。日志`/tmp/addp-node-recovery-diagnosis-lifecycle.log`、`/tmp/addp-node-recovery-diagnosis-registration.log`。现有Monitor指标门禁输入和node-recovery矩阵已登记这些实现／测试，不新增测试入口或workflow。未知／退役原因码的原始输出不透传且清理提示保留由T1验证；T2是受限桥接来源，不外推为个人故障恢复、原生Linux实机或Hosted验收。本轮未启停个人来源、改配置／账号／数据库、操作Safari或查询远程CI／Hosted，未设置定时任务。

### 引擎来源失败阶段的中文提示（2026-11-04）

PostgreSQL已有十二项固定失败阶段，统一入口此前直接显示英文码；Redis、MinIO除镜像原因外未发布阶段。本批先修规范与操作指南，将十八项固定阶段映射为中文步骤及核对建议，在两类引擎原有部署、私有状态、账号处理和来源操作边界补标记。共享来源内核负责在原异常对象上记录阶段，内层阶段优先，不覆盖主要原因或清理标记；未知阶段不输出，已知具体原因仍优先于阶段。提示只定位步骤，不把它当成根因、操作未生效或已回滚的证明，不建议自动重试。未新增读取、权限、配置或恢复路径，原命令／期限／清理范围保持；去除诊断装饰器后，三个部署实现文件中66个既有操作函数的AST与提交前相同（排除输出字段改变的main）。

本地标准门禁：`make test-monitor-metrics-config test-infra-runtime-log-lifecycle`退出0，覆盖共享恢复／诊断10项、主机15项、统一编排19项、启动守卫2项、配置136项、HTTP44／Trace175及生命周期145项。新提示断言先在旧实现失败，最终覆盖十八类阶段、未知阶段不透传、嵌套原异常与超时／清理字段保留、实际来源操作边界和Redis／MinIO命令行封闭输出。`make test-t2-ci-registration`退出0（登记78项、runner9项及实际门禁登记）；现有两类来源T2输入清单已包含本批实现与测试，无需改workflow或新增入口。日志`/tmp/addp-engine-stage-config-lifecycle.log`、`/tmp/addp-engine-stage-registration.log`。

`make test-monitor-redis-source`退出0（含Redis生命周期15项T1、Go固定协议T1），双域真实ACL／固定快照、十五项PromQL、只读复用／停止恢复／同一数据库容器重启后的来源重建、mTLS、发现、持久化及越权拒绝通过；自有来源和配置卷、私有文件清零。`make test-monitor-minio-observation`退出0（含MinIO生命周期25项T1及Go协议T1），双域仅指标授权、S3／管理拒绝、过期／停用、只读复用恢复、mTLS、固定投影及失败清理通过；独立作用域和容器替换后的账号持久化、四十六项PromQL通过；外层容器／网络／卷／私有状态清零。日志`/tmp/addp-engine-stage-redis-t2.log`、`/tmp/addp-engine-stage-minio-t2.log`。实源T2证明诊断加入后的既有生命周期回归；全部失败阶段的注入及中文展示仍是T1，不声称每个阶段均在实源制造过故障。

本批未修改个人来源、账号、数据库、Docker设置或启停个人服务，未操作Safari；PostgreSQL未重跑实库安装或T2，复用其未改的原阶段，仅经本地生命周期与提示T1验证。远端CI和Hosted等专门会话回传，未手动调度或轮询验收，未设置定时任务。下一步优先补主机Safari连续前台刷新及原生缩放保持的剩余真实交互证据。


### 采集来源统一 Compose 分组（2026-11-05）

用户确认先收拢部署分组，保留 PostgreSQL Exporter／mTLS 分容器。本批先修订《平台运行观测规范》的“采集来源统一部署分组”，再统一为工作区摘要隔离的 `addp-metrics-sources-*` 项目。引擎服务按scope区分，主机来源不独占Infra或Business；各账号、私有卷、证书、原数据库网络及资源限制不合并。原私有声明继续是唯一输入，不增加集中配置副本；中心、数据库、业务卷、指标历史不移动。显式up核验并精确退役旧来源，普通recover不迁移、不构建、不重装账号；旧布局与镜像不可用均给出明确应用入口。

每次操作限定明确服务集，删除节点来源的项目级down／remove-orphans，并清除继承的COMPOSE控制变量。Docker对重复服务标签采用交集，已改为逐服务精确查询后去重，避免双来源被误判为空。发现协议／公开API／生产权限不变。标准T2输入清单同步登记共享命名与迁移内核、测试；原Hosted准备与退出检查按同一项目、精确服务和专属卷适配，不触发远端验收。

| 层级／命令 | 结果与范围 |
| --- | --- |
| `make test-metrics-sources` | 退出0；项目／隔离6项、共享恢复11项、主机恢复15项、统一入口19项、标准启动选择2项。旧布局拒绝不迁移、不修改；陌生容器／卷／网络附着在移除前拒绝。日志 `/tmp/addp-regroup-layout-final.log` |
| `make test-monitor-metrics-config` | 最终退出0，137项配置检查及上述依赖通过；包含Node去除COMPOSE_REMOVE_ORPHANS继承、准确服务启停与Desktop／Linux声明。日志 `/tmp/addp-regroup-node-env-final.log` |
| `make test-deployment-discovery` | 45项发现／原子发布与6项容器证明、shell语法通过。首轮观察者夹具在marker刚创建但内容尚空时读PID失败，原样复验通过；未以当前正常改写首次失败。日志 `/tmp/addp-regroup-discovery2.log`、`/tmp/addp-regroup-core-final.log` |
| `make test-t2-ci-registration test-gdal-native-lifecycle` | 退出0；登记80项、runner9项、实际门禁登记及24项原生生命周期通过。复制部署读取工具的隔离夹具已同时复制新增依赖。日志 `/tmp/addp-regroup-boundaries-final.log` |
| 原node Online runner的本批准备／清理组件 | 56项退出0，命令为 `python3 -m unittest scripts/test/platform-node-metrics-fixture_test.py scripts/test/online-hosted-node-metrics-gate_test.py scripts.test.platform-node-metrics-online_test.CapacitySourceLifecycleTest`；日志 `/tmp/addp-regroup-owned-online-final.log`。证书夹具显式配置与SKI／AKI，不依赖本机缺失的OpenSSL配置，不放宽TLS |
| `make test-node-metrics-online-runner` | 完整入口退出2，172项中168通过、4项重复回调语法预检用例失败：其嵌套公共登记入口先因 `scripts/ci/check-online-ci-registration_test` 导入失败，未到预期SyntaxError。公共入口由其他会话调整，本批未修改；不计完整runner通过，后续System Go步骤未执行。日志 `/tmp/addp-regroup-online3.log` |
| `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=node-recovery` | 退出0，独占桥接来源验证双组件精确查询、健康不动、仅采样器／全停止恢复、丢失回执清理、mTLS、漂移／缺失／退休证明拒绝，容器／网络／卷／私有状态清零；不等于个人宿主采集迁移或Linux宿主挂载验收。日志 `/tmp/addp-regroup-node-t2.log` |
| `make test-monitor-redis-source` | 退出0；两scope真实最小ACL、固定INFO、mTLS、精确发现、健康恢复／同容器重启、持久化与零残留。日志 `/tmp/addp-regroup-redis-t2.log`；跨scope同组保留另由下项共享内核验证 |
| `make test-monitor-minio-observation` | 最终退出0；双scope真实来源共用同一独占采集项目，单来源更新／停止／恢复／拒绝后对等来源完整运行事实不变；账号持久化、mTLS、原生拒绝、计数代际、46项固定查询及退出容器／网络／卷／私有状态清零。日志 `/tmp/addp-regroup-minio-t2-final.log` |
| `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=postgres-source` | 退出2，不能算整体通过。普通认证路径的Infra／Business安装、两个新服务名、mTLS、健康不动／停止与同容器重启恢复、账号／HBA不变、漂移及停用拒绝等子项已通过；之后目录认证分支在共享目录原子替换可见性前置失败，HBA正式验收未到达。该Desktop问题仍按既有独立Mac／Linux后续范围保留，未改设置、重启或降低断言。日志 `/tmp/addp-regroup-pg-t2-final.log`；首轮另因本机缺少OpenSSL配置而在临时PKI准备失败，已将当次夹具配置和证书扩展显式化 |

个人来源没有被本会话迁移、重启或改配置。当前MinIO旧镜像缺失的历史根因仍未知，不把隔离构建或新代码视为个人恢复成功。下一步由用户在仓库根目录运行 `python3 scripts/infra/metrics-sources.py up`，按现有配置统一应用；该操作只针对采集来源，可能有短暂采集间断，之后再只读核对分组、当前数据库绑定和Monitor实际采样。个人迁移、Safari／Desktop效果、最终Linux目录认证、自然CI及Hosted仍未验；CI／Hosted等专门会话回传，不设置定时任务。


### 统一分组后的标准生命周期夹具修复（2026-11-05）

CI修复会话回传固定`3d0ccfcce`的Platform Run41867352427／Job126704897315：`test-infra-runtime-log-lifecycle`中六项disabled、metrics-only及父环境隔离用例预期0而实际1。保留[首次失败](https://github.com/pampa0629/addp/actions/runs/41867352427/job/126704897315)；原始日志 `/tmp/addp-ci-20261105-010000/job-126704897315.log` 的2510—2773行，输入一致性收据 `/tmp/addp-ci-20261105-010000/Metrics-lifecycle-current-input-identity.json`。

本地从同一标准入口复现六项失败。根因为假Docker只对旧PostgreSQL采集项目查询返回空集；统一分组的新逐服务查询落入依赖`MOCK_COMMANDS`的核心服务分支。真实部署传输有意清除测试／ADDP父环境，新子进程缺少该测试变量而拒绝，进而导致来源up／预先down错误。生产up.sh和metrics-sources.py的独立中心／来源处理语义未变，不通过修改返回码或跳过来源操作消除失败。

修复仅调整`infra-runtime-log-lifecycle_test.py`：复用产品共享内核生成当前夹具工作区的项目与Infra服务名；在父环境变量分支前，只接受精确七参数的只读来源列表查询并返回当前夹具确实为空的来源集合。明确拒绝Business服务、未知服务、另一工作区项目、缺少服务及额外参数。新增反例不带`MOCK_*`运行，确认不进入核心／变更分支。它在旧夹具上实际失败为`MOCK_COMMANDS: unbound variable`，修复后通过；六项原断言、凭据不生成、核心／数据卷保留、来源故障不阻止核心和父环境隔离均保留。未修改产品部署脚本、个人.env、服务、数据库、数据卷、IAM、Hosted用途、预算或超时。

标准验证：`make test-infra-runtime-log-lifecycle`退出0，包含53项统一来源依赖、HTTP runner46项、Trace／登记183项及149项生命周期／预算／配置／PG生命周期组合回归，相关Go与登记步骤均通过；日志 `/tmp/addp-regroup-lifecycle-final-20261105.log`。`make test-monitor-metrics-config test-t2-ci-registration`退出0，配置137项及依赖、登记80项、runner9项与实际登记通过，日志 `/tmp/addp-regroup-lifecycle-registration-20261105.log`。新增测试沿原模块自动运行，原Platform consistency的`make test-platform`和Metrics T2输入清单已覆盖此文件，不新增workflow或测试路线。十项相关Make配方与已提交HEAD完全一致，未提交的其他会话Makefile调整不纳入本批。

首次本地复现另外记录HTTP预算夹具原五秒编译断言超时，未修改该用例、预算或实现；后续完整标准入口149项通过，原失败保留在 `/tmp/addp-regroup-lifecycle-repro-20261105.log`，不倒填首次通过。针对新查询的单项先失败证据为 `/tmp/addp-regroup-fixture-contract-red-20261105.log`。

本批只修复确定的T1夹具接线，不替代个人来源迁移、真实来源T2或Hosted；没有应用用户采集来源或启动个人服务。修复推送后仍等待专门CI会话回传唯一自然Platform结果，不设置定时、手工dispatch、rerun或轮询远端CI。工作簿仅由P0依据固定版本结果维护。


### 统一分组的独立仓库闭包与网络声明修复（2026-11-05）

CI会话回传固定`7d8ad061e`的两项后续阻断，不能与此前六项夹具修复混为一项：Platform [Run41877661034／Job126741448659](https://github.com/pampa0629/addp/actions/runs/41877661034/job/126741448659) 已通过208项首组及149项生命周期组合，后在独立repo停用场景因缺少`metrics-project.py`退出；Metrics [Run41877661118／Job126748083306](https://github.com/pampa0629/addp/actions/runs/41877661118/job/126748083306) 的host-resources在原生资源查询、压力证据及exporter恢复后，切换Desktop overlay时因默认网络仍有其他服务端点而退出。原始日志位于 `/tmp/addp-ci-20261105-020000/` 的对应job文件；未调查或处理同批其他Owner失败。

**修复范围。** 独立repo固定复制清单补齐共享命名模块，包括`infra-port-resolution.sh`和`dev-lifecycle-and-build.sh`内两个复制点；前者假Docker改为从复制后的真实模块生成工作区项目，严格接受项目＋明确Infra PG服务的只读列表形状，不依赖传输刻意清除的父MOCK变量。保留真实源码加载、来源停用检查、陌生工作区拒绝及禁止个人删卷断言，没有捕获忽略缺文件。另将该共享部署读取依赖补入原Falkor／System日志T2输入声明，沿既有自动选择与门禁执行，不新增入口或workflow。

保留生产Desktop网络Owner标签，在独占T2基底首次创建默认网络时即声明同一Owner。配置层反例比较真实完整base及Desktop overlay的整个networks对象，修复前因新增标签而失败，修复后相等；不通过删除标签、停用其他服务、放宽资源／mTLS／挂载断言或重试解决。实源probe新增切换前Owner与网络声明核验及切换后网络ID相同的检查，防止同类输入变化再发生。对既有bind断言仅增加不含路径／凭据的目的端及类型／规范来源／传播方式匹配布尔诊断，拒绝条件不变；新增T1明确验证shared传播仍被拒绝、诊断不含私有路径。

**已验证及失败，分别保留。**

- `make test-dev-lifecycle`修复前退出2，完整走到独立repo停用场景并复现FileNotFoundError，日志 `/tmp/addp-ci-closure-dev-red-20261105.log`。修复后两个受影响shell标准入口 `bash scripts/test/infra-port-resolution.sh`、`bash scripts/test/dev-lifecycle-and-build.sh` 均通过，含停用零失败及全部既有锁、真实临时监听／进程、deregister次序与原子构建检查；日志 `/tmp/addp-ci-port-closure-green-20261105.log`、`/tmp/addp-ci-dev-closure-shell-green-20261105.log`。
- 完整`make test-dev-lifecycle`修复后确实运行，仍未通过：首轮208项中一项Transfer Worker启动超时，串行复验208项中一项Meta Worker启动超时，均尚未到shell闭包修复点。Worker实现／测试未改，未增加预算或假定休眠根因；日志 `/tmp/addp-ci-closure-dev-green-20261105.log`、`/tmp/addp-ci-closure-dev-serial-20261105.log`。不得以shell子项绿记完整开发门禁通过。
- `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=host-resources`修复前在本机独占项目实际复现相同active-endpoints错误，日志 `/tmp/addp-ci-network-host-red-20261105.log`；三项原始输入摘要 `/tmp/addp-ci-network-baseline-inputs-20261105.json`。配置反例先失败／后通过日志 `/tmp/addp-ci-network-config-red-20261105.log`、`/tmp/addp-ci-network-config-green-20261105.log`。
- 同一host-resources标准入口修复后均退出2，**没有获得完整实源通过，也尚未实际到达新增网络切换断言**：并行轮在budget recovery超时，串行轮在既有limited_source_bind_mismatch拒绝，补诊断后的轮次在更早的sample overflow超时。日志依次 `/tmp/addp-ci-network-host-green-20261105.log`、`/tmp/addp-ci-network-host-serial-20261105.log`、`/tmp/addp-ci-network-host-diagnostic-20261105.log`。这些缺口不能用配置相等代替或认定已解决；本批不再盲跑相同标准入口，不改Docker设置、断言或预算。绑定诊断代码的T1已过，但本轮后续实源未执行到绑定失败的详细诊断输出。
- 最终`make test-monitor-metrics-config`退出0，138项配置／证据检查及来源依赖通过；日志 `/tmp/addp-ci-network-config-final-20261105.log`。`make test-t2-ci-registration`退出0，80项登记、9项runner及实际登记通过；日志 `/tmp/addp-ci-closure-registration-green-20261105.log`。语法、编译、仅本批diff检查通过；受影响Make入口与已提交HEAD配方相同，其他会话的Makefile WIP不提交。
- 四个本轮独占Metrics项目的容器／网络／卷均另行只读确认剩余0，收据 `/tmp/addp-ci-network-all-owned-cleanup-20261105.json`；清理不作为业务验收通过。

本批未操作个人服务、keepalive、采集来源应用、配置或数据卷。两个已确认的输入／闭包缺陷已在代码中收敛，网络实际切换及完整开发门禁仍有上述未闭环失败；修复提交的自然Platform和host-resources终态仍等专门CI会话回传，不触发Hosted、rerun、dispatch、远端轮询或定时。新证据必须按固定提交及具体层级继续补充，不能将配置候选当实源验收完成。

### 统一分组的固定版本实源结果与语法入口阻断（2026-11-05）

专门CI会话回传后，本会话只读复核固定源码 `f9a91928eaedffbe8a1627bd238b43bddfbb000f` 的 [Release Run41889180935／Job126782168039](https://github.com/pampa0629/addp/actions/runs/41889180935/job/126782168039)：attempt1、`Monitor metrics infrastructure (host-resources)`、selected=true，实际 `Verify isolated metrics lifecycle` 步骤成功。原日志98182字节，SHA256为 `48374d5beb1af8469b8afbc04bd3b7df47e82edd1863cd15d233b64ce6863c5a`，与Job元数据及收据一致；其中实际执行网络声明／Owner／身份保持、严格limited mount、资源查询、六来源总览／预算、告警生命周期和自有容器／网络／卷清零。因此前节“网络切换新增断言尚未执行”的缺口在该固定版本的Linux T2关闭；本机失败原样保留，不能扩大为个人Desktop迁移、完整Platform或Hosted通过。证据目录 `/tmp/addp-ci-20261105-030000/`，含 `Root-f9-host-resources-actual-pass-proof.json`、`run-41889180935-jobs.json` 及 `later-host-resources-job-126782168039.log`。

同源码 [Platform Run41889180973／Job126781670842](https://github.com/pampa0629/addp/actions/runs/41889180973/job/126781670842) 仍失败，精确新阻断是既有四个重复声明语法负例。已提交Make入口先执行共享登记依赖，临时仓库因此报ModuleNotFoundError，未到真实Node语法检查；负例要求的“语法先行”未失效，不能靠补入无关登记夹具、删除负例或改调语法子目标解决。原日志 `job-126781670842.log` 第4410—4444行保留。

本次以临时文件分别复制已提交Makefile和工作区既有共享Makefile WIP，保持原四个坏语法变体及真实owner入口，对串行／`make -j4`各执行一次：已提交版8项均复现前置导入失败，WIP版8项均由真实Node首先报SyntaxError及already been declared，且无ModuleNotFoundError。收据 `/tmp/addp-syntax-order-proof-20261105/proof.json` 保存两份Makefile摘要及逐项输出。本会话未修改或提交共享Makefile、共享登记测试、CI规范，也未放宽原断言。

完整标准命令 `make test-node-metrics-online-runner` 在上述**工作区共享WIP**下退出0：登记144项、实际登记一致性、原runner172项及System身份Go夹具均通过，日志 `/tmp/addp-node-online-syntax-wip-full-20261105.log`。随后原负责人已提交共享入口修正 `9b97d1b83`；本会话核对其已提交Makefile摘要与上述8项通过的候选完全相同，相关Make／登记／原语法测试工作区无差异，并在已提交版本重新执行原四变体方法退出0。因此该阻断已有正式代码及本地通过证据；新的自然Platform结果仍等专门CI会话回传，不以本地通过替代远端终态。本会话未重复修改共享入口，不重跑远端CI、不触发Hosted、不设置定时，个人服务与来源均未启停。

**后续自然CI已闭环。** 专门会话回传包含上述共享修复的固定源码 `338bfb3780a57b0baa1cb2a8af9779007db68d56`：[Platform Run41897329087／Job126810251735](https://github.com/pampa0629/addp/actions/runs/41897329087/job/126810251735)，attempt1、selected=true，完整 `Run platform consistency gate` 实际成功。本会话只读复核原日志566422字节及SHA256 `e93758d9931acf260c9fbcede7db62abd99b8aabe37401690ae7f90538dbbd04`，确认登记144项、监控runner172项及身份Go夹具实际运行；失败f9与通过338的原四变体方法AST完全相同。因此本节语法入口阻断的Linux标准复验缺口关闭，原失败保留；个人Desktop迁移及Hosted不在此结论内。收据 `/tmp/addp-ci-20261105-040000/Root-338-shared-syntax-actual-Platform-pass-proof.json`，原日志同目录 `later-platform-job-126810251735.log`；未重跑、轮询远端CI或操作个人来源。

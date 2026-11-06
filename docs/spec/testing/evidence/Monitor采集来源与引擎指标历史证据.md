# Monitor 采集来源与引擎指标历史证据

[返回 Monitor 历史证据](Monitor历史证据.md) · [返回验收证据索引](../验收证据索引.md)

本专题收录PostgreSQL、Redis及MinIO账号、来源准入、引擎身份与专业观测。固定源码、环境、失败记录、修复结论与未覆盖范围以各记录为准；历史整理本身不代表重跑验收或刷新CI状态，不推导当前HEAD或生产T5已通过。跨主题语境与原有顺序见 [Monitor 历史证据入口](Monitor历史证据.md#原记录入口)。

原始过程、逐步诊断及详细归档清单见[精简前的固定记录](https://github.com/pampa0629/addp/blob/be8f8daa4d38a7830585577f03b77bf2b5ed3931/docs/spec/testing/evidence/Monitor%E9%87%87%E9%9B%86%E6%9D%A5%E6%BA%90%E4%B8%8E%E5%BC%95%E6%93%8E%E6%8C%87%E6%A0%87%E5%8E%86%E5%8F%B2%E8%AF%81%E6%8D%AE.md)。 现行部署及专业来源规则见[运行观测规范](../../addp平台运行观测规范.md)。

逐次阶段、轮询与原始摘要见[本轮整理前的固定快照 `90d13c254`](https://github.com/pampa0629/addp/blob/90d13c2545c602eddcc17a81621a135ab64fa540/docs/spec/testing/evidence/Monitor%E9%87%87%E9%9B%86%E6%9D%A5%E6%BA%90%E4%B8%8E%E5%BC%95%E6%93%8E%E6%8C%87%E6%A0%87%E5%8E%86%E5%8F%B2%E8%AF%81%E6%8D%AE.md)。

[PostgreSQL 来源、身份投影与正式验收](#pg-source-history) · [PostgreSQL 本机恢复与共享目录取证](#pg-directory-history) · [Redis 账号、来源与专业查询](#redis-source-history) · [MinIO 账号、采样与专业查询](#minio-source-history) · [引擎页面、导航与刷新](#engine-ui-history) · [统一采集、主机恢复与部署分组](#source-lifecycle-history)

<a id="pg-source-history"></a>

## PostgreSQL 来源、身份投影与正式验收

区分固定 Linux 来源 T2、正式密码／MFA 的 Hosted 及本地契约。实际 Job 及各次清理依附其固定输入，重跑结果与首轮失败分列。

### Infra与Business PostgreSQL 自动接入

2026-10-20 固定 `b3dcae5a3e62b37dbbb15f9e28067a94b6454545` 的 Hosted 专项通过，含审计修复 `0155d5db578ef3324d81db8aa7a8d2482539f55e`。真实非委托 Platform User 以密码／MFA 一次接入 Infra 和 Business 两个单容器 PostgreSQL，并绑定同一新建主机。覆盖列表／详情、刷新恢复、重复拒绝、版本冲突、整批回滚及停用退役，业务约 62.544 秒，浏览器约 26.469 秒。业务、清理通过，Infra 零残留。[实际 Hosted Job](https://github.com/pampa0629/addp/actions/runs/39324018076/job/118305077445)

旧 PUT 因成功审计缺少 HTTP 状态而被约束拒绝。Handler／PostgreSQL 回归先复现 400，后验证更新、旧版本 409、停用和成功审计。System Go、IAM runner 和标准隔离 IAM PostgreSQL API／Repository 验证通过，同源码前端、Platform 与 IAM Job 分别通过。数据库专业指标、其他引擎／集群、多主机、Desktop VM、租户关联和 T5 不在本次 Hosted 范围。[System 前端](https://github.com/pampa0629/addp/actions/runs/39323690209/job/118303942964) · [Platform consistency](https://github.com/pampa0629/addp/actions/runs/39323690209/job/118303942260) · [System IAM PostgreSQL](https://github.com/pampa0629/addp/actions/runs/39323690244/job/118304788473)

### PostgreSQL 固定指标来源与引擎身份投影

2026-10-20，标准 `make test-monitor-metrics` 在专属临时 PostgreSQL 15／SQL Exporter／Nginx／Prometheus Compose 中通过。Infra 与 Business 依次验证独立 SCRAM 身份、固定 16 项指标、真实双向 TLS、无证书／明文／配置及重载路径拒绝、连续 SQL 失败时无旧数据库样本、错误计数递增、恢复产生新快照；关闭统计时不将缺失计数填零。完整主机、进程、趋势、六来源总览及资源告警生命周期回归通过，退出核验当次容器、网络、卷和临时文件零残留。两个作用域顺序复用专属测试 PostgreSQL，不等同两个正式部署或专业指标 T4。

同轮 `make test-module MODULE=common GO_ONLY=1`、`MODULE=system GO_ONLY=1`、`MODULE=monitor GO_ONLY=1`、System Swagger 生成／路由覆盖通过；`make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package repository"` 在标准 `addp_iam_test` 通过，验证同一只读可重复读快照内两个平台引擎身份、私有字段排除、主机及部署停用、Desktop 主机类型拒绝、控制面失败整份拒绝。现有固定 Monitor Service 权限与拒绝其它身份的路由回归仍通过，没有发布新角色或权限。

证据仅覆盖数据库汇总、受保护指标来源及最小后台身份投影。标准可选来源生命周期、Prometheus 引擎发现、当前 User 授权的专业查询与页面仍待接入，未安装个人部署，未计为专业监控正式身份 T4 或生产 T5。

### PostgreSQL 采集账号认证边界

2026-10-20，`make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=postgres-source` 独占 T2 通过。Infra／Business 依次复用同一本轮 PostgreSQL 15：先证实 PUBLIC CONNECT 可让实际采集账号连接镜像默认 `postgres`，安装本方 HBA 首部规则并真实重载后，跨库和 Unix socket 路径明确被 HBA 拒绝，维护库 SCRAM、管理员连接、密码轮换及真实 mTLS 指标读取仍通过。额外验证数据库保留字 `all` 不成为通配授权、输入文件版本不符拒绝写入、原文件字节／属主／权限恢复，30 项配置校验及 38 项 CI 注册测试通过。全退出确认当次容器、网络、卷和临时文件零残留；不创建额外 database，不读取业务表，不触碰个人数据库。该结果交付规则生成和独占认证证据，正式安装生命周期与自动发现尚待串接，不计为专业指标 T4/T5。

### Business PostgreSQL 受管认证目录

2026-10-20，标准 `make test-monitor-metrics` 在仓库外隔离源码快照通过；快照从当时已提交的 `main` 创建，仅叠加本会话认证目录与对应门禁改动，不包含另一会话尚未提交的 HTTP 质量实现。38 项配置校验覆盖工作区归属、私有权限、链接／硬链接、文件漂移、锁竞争、输入版本、模板更新与中断更新拒绝；实际 Compose 解析确认只读目录挂载、禁止自动创建缺失目录且无模板回退。真实专属 PostgreSQL 分别覆盖原 PGDATA 及新只读目录：容器写入拒绝、宿主原子替换可见、重载后认证／mTLS、新样本及原文件恢复；主机、进程、趋势、六来源总览及资源告警生命周期回归通过，告警事件 2、成功投递 2、HTTP 尝试 3，退出核验当次容器、网络、卷及临时文件零残留。

同轮 `make test-business-config` 与 `make test-build-registration` 通过；既有 Monitor metrics CI 依目录输入声明覆盖新文件及 Business 启动／Compose／环境模板，打包入口递归包含 `scripts/infra/`，不新增 Job 或第二条部署路线。该结果不代表共享工作区的其他并行实现、个人 Business 数据卷迁移、采集角色自动安装、来源自动启停、专业发现／查询页面或 T4/T5 已通过；个人实例未改动。

### PostgreSQL 采集账号安装与来源自动启停

2026-10-20，唯一活动安装版本 `000002_lifecycle.sql` 与产品生命周期基础已随 `3ee46b77fd84fe1a915874a11e0e97b843cac8bd` 入库；本批补齐实库验证、容器完整 ID、挂载无序比较、macOS 路径别名及停止流程的锁内操作，避免把格式差异误判为部署变化。Infra／Business 分别按显式开关安装、停用及重新开启本方账号和受控指标来源；安装期间 NOLOGIN，验证 HBA 更新／重载后才启用 Login，不主动终止未知会话，凭据丢失拒绝自动重置。

- 固定源码完整 `make test-monitor-metrics` 复验通过：基线 `c171574023c7ab02a088cec2616baa1c4cb4284e20` 仅叠加本会话产品和验证文件，未混入共享工作区其他未提交改动。覆盖原 PGDATA 与 Business 只读认证目录、两个作用域的真实安装／SCRAM／mTLS、幂等启用／停用／重新启用、TLS 失败关闭、遗留会话拒绝且不杀连接、凭据丢失保护；包含既有主机、进程、趋势、六来源总览和资源告警回归，事件 2、成功投递 2、HTTP 尝试 3。退出核验当次容器、网络、卷与临时文件零残留，不代表整个共享 HEAD 或正式身份 T4。
- 完整门禁首轮在既有停止进程检查的外层 30 秒期限超时，原始未就绪状态未被保留，原因尚未确认；测试仅补充 JSON 状态诊断，并使内部失败能在原外层期限前返回，不延长期限或改变有效性判定。复验该检查在约 0.02 秒通过，不能据此声称已修复首轮超时根因。
- 最新控制面固定快照的 `make test-infra-runtime-log-lifecycle` 47 项通过，包含 8 项纯生命周期契约；Business 启停从各自 `.env` 读取选择前清除四项 Infra 继承值，验证旧配置／缺配置不会误启用，显式 Business 配置可独立生效，并验证可选来源失败后核心仍运行。`make test-business-config` 与 `make test-build-registration` 同一控制快照通过，构建登记核对 26 个 Go 服务／Worker、59 个镜像。完整指标回归先于这一配置隔离补充；最终来源专项另行核对最终输入。

最终 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=postgres-source` 在同一基线叠加最终源码后通过：39 项配置与 8 项生命周期纯契约、两种认证文件布局及产品启停实库检查全部通过，退出零残留；额外核对每次实际 mTLS 返回的采样时间处于当次请求窗口，不接受旧快照。完成后核对本会话运行／探针文件与快照记录无字节漂移。

所有实际 PostgreSQL 验证均由现有标准门禁管理的 UUID 专属 PG15 执行，不使用个人或共享数据库，不修改个人 `.env`、凭据、卷或运行容器。新开关默认关闭；个人认证目录切换、正式 Prometheus 发现、Monitor 引擎专业查询／页面及 T4/T5 尚未验收，不能把来源部署计为专业监控全部完成。

### PostgreSQL 专业发现、查询与页面

2026-10-21，专业发现与查询主体随共享版本 `d5c39624f` 入库；Swagger 引用修复 `667774d20`，两项 Go 测试夹具修复 `5c0b555e625c51510649543e865c4c2520ac845b`。本批仅覆盖已确认纳管并绑定有效主机的 Infra／Business PostgreSQL，不开放 Tenant 引擎、业务表、SQL 正文或凭据。私有投影、统一 SD 准入、当前 User Owner 读取与返回前重核、十二项即时／四项摘要／当前数据库容器段趋势，以及真实产品页面已经具备；不把数据库整体消耗归算给租户。

| 层级 | 实际结果与范围 |
| --- | --- |
| T0／T1 | 冻结 `25addd430` 叠加本批输入。Monitor／Common／System Go、生命周期 51 项、Swagger 86 方法通过。旧夹具曾误用 Web Token 并指定未启动端口，修正后通过。 |
| T2 | 项目 `addp-metrics-t2-efa0ddcde25c4641b418f2811b582055` 通过。39 配置／9 生命周期检查及真实 PG15／Prometheus 验证 12 即时、4 摘要、容器段趋势、统计重置、SQL 撤权清空及恢复。两作用域／两认证布局实测通过，退出容器、网络、卷和临时文件零残留。 |
| T3 | 86 Node 单测；初轮 240 浏览器中 2 项误匹配 Select SVG，修正后 9 项 PG 专项及构建通过。另 232 项来自有效前次输入，无最终全量同次通过。覆盖双语／零、URL 恢复、失败清空和身份／Owner 拒绝。 |

首轮专业查询通过，后因 Docker 读取只读 HBA 通用失败，原根因未定位。第二轮初始速率在原 95 秒时窗口不完整，整批失败；后按 SD、抓取、SQL、完整一分钟及余量设为 130 秒，仍要求四样本、代际、重置和新鲜度。第三轮通过。

正式读取与页面接入既有 `engine-admission`。冻结 `2e46ea4e0` 的 runner 115 项、语法、身份 Go 和标准 IAM PostgreSQL Repository 通过，覆盖双来源归属、私有状态、mTLS、失败清理、主机有效性和整批回滚。后续实际 T4 见[正式 Hosted 验收](#postgresql-专业观测正式-hosted-验收)。

`c828ffd3b` 的 Metrics Job `118632764390` 首败于 HTTP 告警将 Compose DNS `metrics-source` 当 IP，PG 来源已通过。夹具改用 inspect IP 与相同证书 SAN。首轮本地复验缺 SAN、在 SDK 自检失败；次轮历史趋势前置失败，无状态或组数，根因未定。后续本机前置 `no_sample`／`no_data` 未到告警，末次仅两 CPU 样本、缺一分钟边界，仍保持四样本条件。各失败项目的容器／网络／卷均核对为零，本批 HTTP quality 未通过；旧 Platform 仅生命周期 47 项通过，聚合仍失败。

冻结输入的 Monitor Go T1、语法与登记通过；正式扩展提交 `89749f1d6`，查询参数 86 项及新增资源拒绝通过。该证据仅覆盖 runner 与契约。

固定 `009a5e42598038f37cc0a202edfbe88a49acc7bd` 的 [Platform consistency](https://github.com/pampa0629/addp/actions/runs/39519915079/job/118960943889) 通过。[Metrics 作业](https://github.com/pampa0629/addp/actions/runs/39519915536/job/118961147933) 在后续资源总览被取消。[Hosted engine-admission](https://github.com/pampa0629/addp/actions/runs/39519991703) 首次在 Infra 构建及上传连接 GitHub 超时；第二次通过接入页，脚本误拒默认 `policy_version=0`，专业页面未运行，`cleanup=passed`、`infra_cleanup=zero_residuals`。修正 `c4ff143d7` 的 117 项 runner 与身份 Go 通过，正式 T4 单列。

[Metrics 后续原生 Linux 运行](https://github.com/pampa0629/addp/actions/runs/39522656609/job/118975847648) 固定 `95a8b2a25678475bdaa65ba07649f10344092916`、attempt 1。HTTP 告警及签名恢复通过，首败是 `TestIntegrationMetricsHTTPRealSource` 停源清空。旧日志缺失败 scrape 时间戳；确定性反例仅确认当前时刻和消费端整秒时刻存在竞态。对齐后保留 65 秒与原断言。冻结 `53cc8e07ddd810dd43fccbfd0106279050c3b25a` 叠加两项夹具，本地完整 Metrics 通过（40 配置、9 生命周期、PG／HTTP、停源与对等存活、WAL、六来源及告警）。项目 `addp-metrics-t2-855a0575dbd145eaacb375b10e0478ed` 零残留，日志 `/tmp/addp-http-stop-boundary-metrics.log`。首次冻结副本无 Git 元数据，登记未运行；补齐后检查通过。本地结果不覆盖新原生 Linux、正式 T4、七天留存或生产 T5。

### PostgreSQL 来源准入协议修复

冻结 `c7753fe38fa55d3d2431d42f4ac49c05c3ba754c` 的 runner 117 项和身份 Go 夹具通过。Hosted 通过引擎接入页面，在当前容器专业查询的 130 秒采样窗口失败，专业页面未执行。HTTP SD 曾由 200 转 422。外层清理通过，Infra 零残留。[Hosted 实际失败 Job](https://github.com/pampa0629/addp/actions/runs/39680591321/job/119501760697)

旧 PostgreSQL Nginx 对缺客户端证书返回通用 400，生产 Admit 只认可明确证书拒绝或 HTTP 401／403。修正保留严格双向 TLS，将内部 496／495 映射为 403，拒绝请求不转发。实源红绿沿原 postgres-source 入口，红阶段报 `metrics source protection could not be verified`，修正后真实准入、无证书／不受信证书拒绝、12 指标／4 摘要、统计重置、失败清空和生命周期通过。红绿项目均零残留。Monitor Go T1 通过，首次缺 Git 元数据的未运行调用单列。日志 `/tmp/addp-pg-admission-red.log`、`/tmp/addp-pg-admission-green.log`、`/tmp/addp-pg-admission-go-t1.log`。原生 Linux 冷构建与正式专业 T4 另计。

### PostgreSQL 专业观测正式 Hosted 验收

固定 `b08f2331dc6c76b520b958dc5a3d79c054f80900`，System／Monitor／Gateway 同源码。真实平台管理员密码／MFA和无权限对照通过。Infra、Business 单容器 PostgreSQL 覆盖接入、变更、整批回滚、版本和退役。12 即时指标、4 摘要、当前容器趋势、Console 重载与有效零、停源及恢复通过。业务与外层清理通过，Infra 零残留。归档在 `/tmp/addp-ci-20261022-manual-followup/artifacts-39683888149/`。此结论仅限该版本，后续主机导航、其他引擎／集群、自然留存及 T5 另计。[Hosted 实际 Job](https://github.com/pampa0629/addp/actions/runs/39683888149/job/119512936927)

同版 Release PostgreSQL 来源 T2 独立失败。外层 45 秒终止包含依赖准备的 Go 命令，日志未进入测试或报 Go 内部 30 秒超时。排障先区分冷准备与实际准入。原日志 `/tmp/addp-ci-20261022-manual-followup/job-119520816486.log`。[Release 实际失败 Job](https://github.com/pampa0629/addp/actions/runs/39683857764/job/119520816486)

### PostgreSQL 准入探针的构建与执行期限

固定 `4a0a9ea1d` 的 T2 再次失败，与前次 `b08f2331d` 相同。外层 45 秒包含依赖下载、编译及执行，日志未进入准入测试。修复拆分私有目录编译准备（最多 180 秒）与执行（Go 30 秒，外层 45 秒）。失败即停止，准备失败禁止执行遗留文件。正式 mTLS 和准入断言仍按原口径。[实际失败 Job](https://github.com/pampa0629/addp/actions/runs/39696991326/job/119557101259)

冻结 `0cad0ddf334524fa31295b167389002b0edd9e1f` 叠加专项输入。确定性红阶段模拟冷准备，2 项失败、1 项错误。修正后完整 postgres-source 通过 45 项配置、9 项生命周期及三次真实准入（Infra、Business、只读认证目录），包含指标、SQL 失败恢复及原子重载。退出及精确项目复核零残留。原始日志为 `/tmp/addp-pg-admission-budget-red.log` 与 `/tmp/addp-pg-admission-budget-green.log`。本地真实 T2 使用依赖缓存，原生 Linux 冷构建需独立证据，已有专业 Hosted 结论单列。

### PostgreSQL 专业来源固定版本 Linux T2 复核（2026-10-24）

同一固定 `5d91fa31c326fc636c73810edb542ae7f95e4db0` 的自然原运行及唯一协调复验分列。

覆盖十二项原生专业指标、四项摘要、当前数据库容器段趋势、启动边界／外来身份拒绝、统计重置留空及 SQL 失败清空／新样恢复；原始日志含四次 `TestIntegrationMetricsPostgreSQLSource` 和三次 `TestIntegrationMetricsPostgreSQLAdmission` 实际 PASS。Infra／Business 两个作用域均验证固定函数与受限采集身份、mTLS／受限 HTTP、权限拒绝、密码轮转及幂等；Business 另验证维护账号 SCRAM、其他库／socket 拒绝与原管理员规则保留。两作用域账号安装、NOLOGIN、重新启用、已有未知会话拒绝和凭据丢失拒绝通过，仓库外认证目录只读挂载、宿主原子更新、真实 reload 与完整还原亦通过。脚本最后明确报告当次 owned 容器／网络／卷及设施临时文件零残留，不能只以摘要或 cleanup 成功替代这些实际执行断言。

[job 120735545333 / attempt 1](https://github.com/pampa0629/addp/actions/runs/40048425338/job/120735545333) 首轮 50 项通过、1 项错误。只读 Business Compose 配置检查的 `docker compose ... config --format json postgres` 超出原 15 秒期限，未进入产品采集；无正式清理结果可替代，暂态原因未知。

[Release 40048425338 / job 120741235374](https://github.com/pampa0629/addp/actions/runs/40048425338/job/120741235374) attempt 2，实际 `Verify isolated metrics lifecycle` 成功，准备 51 项及上述真实 PG／mTLS／查询全部执行，输入及断言未改。通过及首失败原件分别见 `/tmp/addp-ci-20261024-020017/`、`/tmp/addp-ci-20261024-013016/`。范围只到固定版本 PG 来源 T2，七天留存、容量、新 Online T4 未覆盖。

### 采集分组改名后的 PostgreSQL 查询与来源复验（2026-11-05）

改名后本机 `postgres-source` 首次在专业查询约 3 秒处返回 `resource query backend unavailable`。同期 Prometheus 记录 TLS 握手 EOF，缺少请求级对应证据，不能确认首败根因。只在隔离 Go 查询测试记录失败时的 TLS 失败／超时布尔值和收到首响应字节的次数，保留原查询、TLS、样本要求与超时。首次加诊断复跑在独立启停夹具 10 秒超时处退出，未进入实源。

第二次执行 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=postgres-source`，138 项配置和 27 项生命周期用例通过。十二项专业指标、摘要、当前容器趋势、统计重置留空、SQL 失效与新样恢复均通过。Infra／Business 的原生认证、mTLS、来源启停及同容器重启后的恢复也通过。原查询错误此次未复现。

完整 T2 仍在 Docker Desktop 只读共享目录前置失败。宿主原子替换的 inode 与内容均已更新，容器首次读取返回 1，HBA 两侧原内容均未变化。随后同进程目录枚举只立即恢复被枚举一侧，较后两侧均读到新内容，原 HBA 正向验收仍未进入。精确核对项目 `addp-metrics-t2-78b65bfe1256402fa4bdeda8235409d1` 的容器、网络和卷均为零。Monitor Go 门禁和仓库 T2 登记检查通过。原件 `/tmp/addp-metrics-fixed-group-postgres-t2.log`、`/tmp/addp-postgres-source-query-transport-20261105.log`、`/tmp/addp-postgres-source-query-transport-full-20261105.log`。CI 与 Hosted 等专门会话回传。

### 观测身份投影 PostgreSQL 断言误报修复（2026-10-23）

CI `39775896930`／System IAM Job `119824550350` 的原始失败是 `private field leaked: 5433`。旧用例对整个 JSON 搜索端口数字，允许投影的随机 UUID 同样可能命中；原 CI 没有保存当次完整快照，不能还原具体命中的身份。将测试主机固定为合法 UUID `54330000-0000-4000-8000-000000000001`，沿标准 PostgreSQL 入口稳定复现相同误报。现改为解码 JSON 后比较完整预期结构和字段值，包括节点、进程、引擎、来源键及无专业来源时的 `metrics.state=unavailable`；任何额外字段仍拒绝，不删除私有字段的否定验证，不修改产品代码、IAM、投影或授权范围。

已先运行 `bash scripts/infra/status.sh` 核对当前共享 PostgreSQL 为 `25432`。在 `addp_iam_test` 使用标准 `make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package repository"`：旧断言加固定 UUID 的红色复现失败；修正结构预期后通过；随后仅在当次测试输入临时注入嵌套 `source_key.published_ports=[5433]`，同一入口按完整结构断言明确失败，证明泄露仍被拒绝；删除临时注入后最终重新执行，7 项 Repository 顶层 T2 与测试库重置均通过、退出 0，无跳过。最终文件与注入前通过版本逐字节一致。原始日志 `/tmp/addp-observability-projection-red.log`、`/tmp/addp-observability-projection-mutation.log`、`/tmp/addp-observability-projection-final.log`；中间首次结构预期遗漏已有指标状态且错误强制启动时刻为 UTC 的失败已修正，不计产品故障。

本轮仅修改既有测试和此证据，现有 Release/T2 System IAM Job 自动发现，无新增测试入口、依赖或 CI 编排。完整 System IAM CI 等专门会话回传，不把本地 Repository 分包 T2 计为全批 CI；未调度或查询 CI／Hosted、未设置定时、未重启个人服务。Safari 实际弹窗可见性问题仍未解决。

### PostgreSQL 私有来源投影 T1 的短数字碰撞修复（2026-10-30）

CI 专门会话回传 [Actions 40984761423／Job 123833070594](https://github.com/pampa0629/addp/actions/runs/40984761423/job/123833070594)，源码 `3a004a94759f0414030d99c992e057c2651c2cb5` 的 System Go T1 在 `TestEngineMetricsPrivateProjectionDoesNotChangeAdmissionOrBrowser` 报 `private source leaked into browser`。原断言搜索整个公开 JSON 的短字符串 `19443`，它恰好出现在该次合法公开指纹 `06e61ece304ab35633d28c1f97252ab7a131b4fcd9346360db5b194430ecd96b` 中。独立核对生产发现读取器和 DTO 与失败 SHA 逐字节相同，读取器仍清除私有来源；没有以本次误报证明真实泄漏。

测试保留当前新鲜的发现时间，仅将容器启动时间固定为失败现场的 `2026-10-30T00:29:17Z`，并锁定完整碰撞指纹；原短数字断言因此在标准入口稳定报出相同失败。修正为完整公开投影值比较、逐作用域检查内存中的私有指针及 JSON 私有字段，并拒绝完整私有端点；保留后台来源正确性、快照启动边界、外来 Docker 身份、可选来源身份错误和旧协议拒绝。只修改此测试，未改生产 DTO、发现路由、权限或采集断言。

在当前共享工作区，以已有 Node 24.20.0 的 bin 临时加入测试命令 PATH 后，`make test-module MODULE=system GO_ONLY=1` 红阶段仅该碰撞断言失败，绿阶段完整 System Go T1 退出 0。最初使用默认 Node 25 的调用被仓库新版 Node 24 前置检查拒绝，未进入 Go、不能记为碰撞复现；没有改全局 Node 或依赖。红／绿日志 `/tmp/addp-engine-metrics-privacy-red.log`、`/tmp/addp-engine-metrics-privacy-green.log`。另用 Go overlay 在 OS 临时目录单独向测试结果注入第二作用域私有指针、私有 JSON 字段和完整私有端点，三次同一标准入口均命中各自拒绝断言；共享测试文件及生产文件未改写，记录 `/tmp/addp-engine-metrics-privacy-audit.json`。T2／Hosted 未运行，自然 CI 当时待回传。

CI 专门会话随后回传源码 `cf39ab99fc6dd1e5a022d87dd3c81230a88c9add` 的自然 [Platform CI Go 作业 123880180441](https://github.com/pampa0629/addp/actions/runs/40999452791/job/123880180441) 为 success。本会话只读核对其原始日志 `/tmp/addp-ci-20261030-043005/job-123880180441.log`：检出 SHA 与上述修复一致，第 799 行为 `ok github.com/addp/system/internal/service 1.502s`。据此关闭本项确定性投影误报的自然 Go T1 缺口；原失败记录保留，不据此推定整个 Platform CI、Desktop 真实认证变更 T2 或 Hosted 已通过。未查询或重跑 CI，未操作个人服务。

<a id="pg-directory-history"></a>

## PostgreSQL 本机恢复与共享目录取证

本机来源和页面复验与独占目录故障分开认定。目录多轮沿原标准入口，首次不可见仍失败；枚举后读回仅用于诊断，原认证断言和清理结果各自保留。

### 个人环境 Infra PostgreSQL 接通与跨模块入口修复（2026-10-29）

用户沿标准入口启用 Infra，Business 仍为 not_deployed。独立账号／固定汇总／认证、Exporter 和 mTLS 已实际接通；Monitor 读来源返回 200、16 条固定采样，Infra 发现为 available。

旧“infra-虚拟机”显示名实际匹配 Business 完整容器 ID，保持原记录。只确认 Infra PostgreSQL，绑定原 Desktop VM，部署 `6b262aad-da10-4f00-8fd2-421465e84189`。Prometheus 目标 6→7，新增来源身份精确匹配、up 且无 lastError；收据 `/tmp/addp-pg-infra-runtime-receipt.json`。

真实管理员 Safari 可见 12 项、有效零、容量 5.96 GiB、完整分钟速率和五分钟趋势，悬停 144.07 次/秒。此为本地观测，未读取业务表或验 Hosted。

首败为 System Router 将“查看监控”解析为 forbidden。切换共享 Console 路由构造器后，真实前台打开正确部署，自动刷新推进 15 秒；原生后退恢复 Infra 搜索及两条记录。中途隐藏的空态不计来源故障。

| 层级 | 本地结果 |
| --- | --- |
| 跨模块 T3 | 首个正确地址断言失败，修正后 3 场景通过；首轮绿阶段误将会话刷新 POST 当管理写入，修正测试分类后通过。含双语 iframe、后退恢复与无权限不显示。 |
| 共享／Console／Monitor | Common 302、Console 219／268、Monitor 131／513 与适用构建通过，登记 27＋21 通过。 |
| System | 177 单元通过，浏览器 255 通过、权限批量场景 1 失败，整入口失败且未到构建。独立登记页面 13 项和构建通过，原失败保留。 |

原件 `/tmp/addp-pg-*.log`，本轮未验固定 CI、Hosted 或生产。

### Business PostgreSQL 认证文件幂等发布与 Desktop 挂载故障（2026-10-30）

用户授权 Business 专业采集后，在本人终端沿标准入口重建 PostgreSQL，保留 `business_postgres_data`，并应用仓库外只读认证目录。数据库健康，固定汇总函数可在只读事务内以采集角色调用；来源安装失败后采集角色为 NOLOGIN，尚无 Business 采集容器，不能记为采集接通。

实际故障位于宿主认证文件原子替换后的容器读取：宿主文件存在且内容正确，容器 `stat` 能返回大小，但 `head`／原 `wc` 打开同一路径返回 ENOENT。同字节原子重发亦可复现，15.144 秒的有界观察仍不可读，不能以“短暂延迟”认定重试足以修复。最终不保留等待／重试，也不改变目录挂载、权限、认证基线或服务启动边界。

修正同一发布器的幂等性：已有内容与拟写内容相同时，先完成所有者、权限、基线及预期版本核验，再保留原文件和 inode；真实变化仍用唯一原子替换。原代码运行新增契约时明确失败于“不应重新发布未变策略”，修正后验证同内容保留 inode／mtime、旧版本与错误权限仍拒绝。当前个人认证文件执行同内容核验后，inode、mtime 未变，容器完整读取与宿主字节一致；此证据只证明幂等更新，不证明真实变更后的 Desktop 挂载一致性。读取另改为有界流式读取，保留 256 KiB 上限及符号链接／非普通文件拒绝。诊断只发布固定阶段、封闭原因及清理失败标识，清理错误不覆盖首个失败，不输出 HBA、SQL、密码或原始子进程错误。

验证与边界：

- `make test-infra-runtime-log-lifecycle` 退出 0，最终 108 项中包含 17 项 PostgreSQL 生命周期回归；涵盖真实临时文件／FIFO／符号链接、读取上限、幂等更新的版本／权限检查及诊断脱敏。日志 `/tmp/addp-business-pg-lifecycle-final.log`，幂等契约红色复现 `/tmp/addp-business-pg-noop-red.log`。
- `make test-t2-ci-registration` 退出 0，74 项回归及既有服务登记检查通过。现有标准入口、Release 的 Monitor metrics infrastructure 与 Platform 生命周期检查已覆盖修改路径，没有新增 CI 编排或旁路入口。日志 `/tmp/addp-business-pg-registration-final.log`。
- 完整 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=postgres-source` 首轮失败于测试按工作区统计来源时误纳入个人 Infra，已改为只核对本轮项目；第二轮失败于当次 `discovery.json` 的共享挂载 ENOENT，无法确认目标退出发现，未计通过。日志分别为 `/tmp/addp-business-pg-source-diagnostic-gate.log`、`/tmp/addp-business-pg-source-fixed-gate.log`。
- `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=postgres-observation` 也在真实修改认证内容后的容器文件读取失败，后续版本保护拒绝覆盖，未计通过；日志 `/tmp/addp-business-pg-authentication-final.log`。本地两项 T2 的 Desktop 共享挂载缺口仍保留，不用 T1 或当前同内容读取代替。
- 三轮专属测试项目的容器、网络、卷独立查询均为零，个人 Infra 来源未停止。摘要 `/tmp/addp-business-pg-atomic-diagnosis.json`。未自行重启个人数据库或启动采集服务；真实 Business mTLS 快照、自动发现及页面仍待用户标准入口应用后复验。CI／Hosted 等专门会话回传，不设置定时任务。

#### 用户标准启动后的 Business 实源接通

用户在本人终端再次执行 `bash business/scripts/start.sh -postgres`，报告 `source deployed`。随后只读核验数据库仍为同一容器 `e2c3d4d1cc9f9e8b11a5b504b28e65a1ac27446464474fbc6fa2a493c0e3425c`，启动时刻 `2026-10-30T02:10:45.211369878Z`、原数据卷及只读认证目录保留，未再重建数据库。Business 独立来源两容器运行，唯一回环端口 `19110`；独立准入客户端 mTLS GET 返回 HTTP 200，固定 16 项原生样本齐全，采样时间距读取约 0.005 秒。总连接数 0、最大连接数 200、数据库容量约 78.33 GiB，累计回滚 1、死锁 0；这些是整体数据库指标，不读取业务表或将用量归算租户。无客户端证书的同地址请求返回 403；采集角色为 LOGIN、非超级用户、无角色成员关系，认证解析错误为零，原有业务认证基线字节保持一致。

Business 部署发现 v2 自动变为 available，专业来源端点、当前数据库容器 ID／启动时刻与真实容器精确一致。Prometheus mTLS API 显示该目标 UP、抓取错误为空，自动沿原平台部署 `9e4fd2b0-3b45-473e-b070-cc6f92e40e11` 接入，没有重复创建部署；当前外部目标 8 个、加中心自身共 9 个，未增加目标预算。受限到该部署／容器身份的原始时序读取显示最近一分钟已有 4 个采样，提交／回滚／死锁累计量均可读。摘要为 `/tmp/addp-pg-business-runtime-receipt.json`，这只计个人环境来源及发现实链，不替代此前失败的 T2、当前 User 专业 API 页面或新的 Hosted。

Safari 单独打开 Business 验证标签后内容区空白。进一步以禁用环境代理的直接本机 HTTP 核对：Monitor 后端 `/health/ready` 为 200，前端 `5179` 无监听、Console 模块代理为 500；`logs/monitor-frontend.log` 保存 Node `read EIO`／TTY 退出，因此尚未认定页面通过。已请用户通过 `bash scripts/dev/start.sh -monitor` 恢复缺失前端，已有服务复用；本会话未自行启动／重启个人服务，仍待页面复验。未运行新的 CI／Hosted 或设置定时。

### 个人两库来源不可用的只读诊断（2026-10-31）

用户要求继续后，沿现有部署及发现契约只读核对，不修改采集开关、证书、账号、HBA或个人服务。标准`bash scripts/infra/status.sh`显示两库与Prometheus均运行，指标中心Ready只作中心存活证据，不代表数据库专业来源可用。`docker ps -a`及限定字段inspect确认Infra／Business各自的SQL Exporter和mTLS入口共四个容器均为Exited(255)、OOM=false、无Docker State.Error，restart策略为no。四个来源停止时间均在2026-10-30 16:13:20 UTC附近；两库与Prometheus记录几乎同时停止，随后在16:13:23 UTC重新启动，采用unless-stopped策略。换算北京时间为10月31日00:13。该时间关联不能独立证明Docker Desktop或daemon的具体退出原因，原始255事件不按猜测归因。

两个作用域的配置仍为`ADDP_POSTGRES_METRICS_ENABLED=true`；既有服务器证书存在，`openssl x509 -checkend 0 -noout`均成功，只证明本轮未过期，不替代完整信任链及mTLS验收。两库的当前容器ID与四个来源的数据库ID标签一致，但数据库StartedAt已改变，四个来源的数据库启动时刻标签全部不匹配。标准发现快照infra/business本体均available且持续更新（分别13:00:10Z／13:00:12Z），其中postgresql_metrics.state均为unavailable，与`scripts/utils/deployment-inventory.py::postgres_metrics`要求来源运行、容器身份及启动时刻精确匹配的拒绝路径一致。Monitor沿`engine_observation.go`将私有投影的unavailable映射为source_unavailable；本轮不借用后台身份或绕过当前用户授权调用私有接口。

确认的直接断点为采集容器未恢复，且旧来源声明已不匹配本次数据库启动；不是页面tip修改引入的查询或展示问题。仅docker start旧容器或补重启策略，仍不能重新发布当前启动身份，因此不能据此认定恢复。当前支持的恢复路径仍是标准部署入口：用户在仓库根目录依次运行`bash scripts/infra/up.sh --metrics`及`bash business/scripts/start.sh -postgres`。前者重新应用中心和Infra来源；后者沿现有幂等分支复用已运行Business PostgreSQL，再重新核验采集账号、认证及当前数据库身份并发布来源。暂不要求Monitor重启；恢复后分别复验来源发现、实际专业值和趋势，不用source deployed输出替代采集通过。按AGENTS，实际应用已请求用户操作，本会话未执行。

本轮仅诊断并更新现有证据／能力状态，最小检查`git diff --check`通过；没有产品／测试变更，不运行额外数据库、来源T2、CI或Hosted，不设置定时任务。Prometheus受保护API的无凭据直读未成功，不记为目标状态通过，也不读取或输出服务秘密。当前来源恢复与本次采集新鲜度／趋势有效性仍待用户应用后的验证；后续若要让宿主恢复时自动协调来源，需要另行明确部署侧身份重发布职责，不直接加入restart旁路或放宽StartedAt校验。

### 用户重新应用后的两库来源与 Safari 恢复复验（2026-11-01）

用户在前述两条标准命令请求后回复“已重启”。本会话仅只读复验，没有自行启停或修改部署。`docker ps -a`和限定字段inspect确认四个Infra／Business SQL Exporter与mTLS入口均running、exit=0；数据库本身仍运行，四来源的数据库ID及StartedAt标签全部与当前数据库匹配。最新发现快照infra/business本体及postgresql_metrics.state均available（观察时间分别00:13:58Z／00:14:01Z），前述旧启动声明失配已不再阻碍发现。

使用根配置中既有Monitor→Prometheus的mTLS只读身份，对现有HTTPS `/api/v1/targets`执行一次限定GET：目标总数9，其中两个postgresql目标均health=up、lastError为空，最近采集分别00:15:20.815997426Z／00:15:22.204840669Z。仅输出数量、健康及时间，不输出目标端点、凭据或秘密，不调用SQL或管理接口。此处up只证明来源传输，实际数据库快照及Monitor消费由下述真实页面另证。

原生Safari仍为用户已登录的平台系统管理员第二个标签页；初次读Business页显示“暂无采样”，不直接判恢复。随后刷新动作遇到工具用户操作保护而中止，重新读取后页面已在引擎列表；本会话没有沿旧索引发送输入。当前列表真实显示两库“正在采集”：Infra连接48、活跃0、提交148.61次/秒、容量6.30GiB；Business连接0、活跃0、提交0.60次/秒、容量78.3GiB。随后实际进入两个详情，十二项即时值可见，最大连接数均200，真实零与缺值不混用。Business提交0.53—0.60次/秒、回滚／死锁0.00；Infra空闲48、提交148.60后更新147.51次/秒、回滚1.00、死锁0.00。该速率仍包括观测事务，不当作租户业务吞吐或异常结论。

Business真实截图看到提交曲线，五分钟窗口08:11:12—08:16:12、15秒评估，末段为新采样点，前段保持空缺；后续无手动刷新，窗口推进至08:11:28—08:16:28。Infra随后真实截图看到连续的新提交曲线，窗口从08:12:06—08:17:06推进至08:12:22—08:17:22，页面自动刷新保持开启。截图及原生可访问状态保留在本会话；只认定两库当前真实值、提交趋势及本轮数据推进，不外推所有指标趋势、七天、缩放交互、长期可靠性、原生Linux或新Hosted。第一标签页未操作，浏览器主题／侧栏／刷新偏好未改，最后保留第二个Infra详情页。

前一日四来源停止、旧StartedAt失配与本日恢复结果分别保留；当前个人来源不可用缺口已在上述范围闭合，不倒改停采期间的历史，不填零或回填曲线。宿主自动恢复后如何重新协调部署身份仍未新增，本次依赖用户标准入口重新应用，不以本次恢复证明无人值守恢复。

本轮仅追加证据及更新能力状态，产品和测试未改，最小检查`git diff --check`通过；不重复已有效的T1／T3／来源T2／CI或Hosted，不自行轮询、触发或设置定时任务，不修改IAM、数据库、采集配置或共享工作区其他会话改动。下一步建议先讨论宿主恢复时的部署侧来源协调范围，再在确认后实现，不直接加restart策略或放宽启动身份校验。

### 完整开发启动的 PostgreSQL 来源恢复（2026-11-01）

完整本地启动无参数或 `-all` 时在发现刷新前恢复已安装来源；单模块和 Online 入口不执行。Infra／Business 配置隔离加载，未安装不访问 Docker／数据库，一域失败不阻断另一域或业务。[运行观测规范4.7](../../addp平台运行观测规范.md#47-postgresql-受保护的固定指标来源)

恢复只读核验容器、角色、来源及 TLS，复用原来源创建。健康来源不重建、不改文件；停止／缺失或同容器新 StartedAt 才重绑。新容器、停用、权限及输入漂移拒绝，失败只清理自有来源；无管理 SQL、HBA 或账号变更。

确定性回归覆盖上述边界、配置隔离与锁竞争。原五秒夹具超时保留，改用轻量命令输出，期限不变。

实库沿 `--case=postgres-source`。当次 PostgreSQL 改用专属数据卷，验证重启仍保留库及账号；原 tmpfs 重启重新初始化，不能作持久恢复证明。

前序失败依次包括 OpenSSL 4.1.0 缺默认配置、Desktop 挂载替换使发现清单 404、夹具错从 pg_authid 取配置、tmpfs 重启丢数据。分别限定命令选择 OpenSSL 3.6.4、在当次来源容器内原子发布并精确读回、改读 pg_roles、改专属卷。各失败项目容器／网络／卷独立核对为零。

运行日志生命周期曾整组通过（37／136／142）。后续开发综合门禁 22／194 项通过后，HTTP 预算夹具因缺 PID 文件失败；独立 shell 生命周期通过，综合失败保留。更早 Worker／停止夹具超时在非并行复验未复现。

最终 125 配置／26 生命周期、原生 PG 账号、mTLS、12 指标与趋势通过。双域恢复分项实际验证健康无重建、停止后新鲜值、同容器重启重绑、文件字节／inode／mtime 与账号／HBA 不变，以及权限漂移拒绝、计划停用不复活。

整轮首败在后续目录认证，宿主原子替换 HBA 后容器读取非零，finally 又因恢复版本改变拒绝，未进入目录恢复用例。项目 `addp-metrics-t2-9ba602eecb87400a91b458a58ea980ee` 的容器、网络和三个卷均核对为零。此前普通 Business 拒绝的未分类退出未复现，根因未定。

本机完整启动、真实 Console 自动恢复、原生 Linux 目录恢复及 Hosted 当时未覆盖。目录首败和后续对照见[目录认证取证](#pg-directory-history)。

### 目录认证 HBA 读取取证与夹具恢复版本修复（2026-11-01）

夹具原在读回／重载后才记录恢复版本，读回失败会在 finally 产生 CAS 冲突。改为物理发布后即记录新版本，仍严格读回、核权限与重载；恢复拒绝外部漂移，保留首败、单列清理错误并封闭诊断。

`/tmp/addp-hba-directory-diagnostic.log`：128 配置／26 生命周期及双域原生认证通过。目录首败为容器 cat `not_found`；宿主 661 字节、0644，容器 stat 仍见 `644:90:0:0`。恢复后宿主回到 90 字节，无旧版本 CAS 冲突；容器读回仍失败，恢复未通过。

仅改 TMPDIR 到 `/private/tmp` 的对照同样失败，无法归因于默认临时路径。日志 `/tmp/addp-hba-directory-shared-tmp.log`。项目 `addp-metrics-t2-c7a40ff34f134729ae5c95d7d20e6071`／`addp-metrics-t2-4261cd8a06c94a3a831c34f3162d0d89` 均标准销毁并按标签核对资源。

Desktop 4.49.0 只读挂载显示 fakeowner，不能由此确认共享实现。当时设置读取未成功，后续实际设置和普通文件对照见下节。[Docker Desktop设置](https://docs.docker.com/desktop/settings-and-maintenance/settings/)

目录 HBA 正向读回和重启恢复未通过，未引入等待、重试或原地覆盖。

### Docker Desktop 实际共享设置与普通文件对照（2026-11-01）

按用户要求只读查看原生 Docker Desktop 4.49.0 设置：General 当前选择 Apple Virtualization framework 与 VirtioFS，Docker VMM、gRPC FUSE、osxfs 均未选，Enhanced Container Isolation 关闭。Resources／File sharing 列出 `/Users`、`/Volumes`、`/private`、`/tmp`、`/var/folders`；没有已列出的同步缓存共享，界面提示登录后使用 Synchronized file shares。Apply 始终禁用，Engine running；未更改选项、应用设置或重启。设置页保留在 File sharing 供用户查看。

只读运行核对确认 context 为 desktop-linux、本地 Unix socket，VM 进程实际带五项 `--virtiofs` 参数；未输出其路径内容。容器的 fakeowner 挂载层和内核中 grpcfuse 模块不能据此当作 gRPC FUSE 被选择的证据。现有个人 Business 认证目录仍只读；限定读取当前 HBA 的结果是宿主与容器各1367字节且内容一致，未输出内容、密码摘要或凭据，未执行SQL。故障范围是发布替换后的可见性，不能表述为现有HBA持续不可读，也不能归因于临时路径未共享。

沿原有来源T2增加普通文件对照：在挂载前准备 `publication-probe`，容器先读到旧内容，再用现有原子发布器替换该文件；同时核验HBA在两侧均不变。失败仍使原门禁失败，不跳过后续认证、不增加等待或重试，不修改产品安装器或HBA。该夹具、配置回归与门禁文档已在原有CI输入登记中，无新入口、依赖、权限或Workflow。

执行 `PATH=/opt/homebrew/opt/openssl@3/bin:$PATH make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=postgres-observation`，日志 `/tmp/addp-hba-publication-control.log`：129项配置T1、26项生命周期T1通过；Infra／Business原生认证、固定汇总、拒绝越权、幂等、轮换与归属用例均通过。目录前置对照失败：`host_inode_changed=true`、`host_matches_replacement=true`，但容器读取返回1、`container_matches_replacement=false`；`hba_host_unchanged=true`且`hba_container_unchanged=true`。该结果独立排除HBA解析、账号SQL及重载作为本次普通文件故障的触发条件，仍不足以指定Docker上游缺陷或承诺升级修复。

目录HBA正向验收尚未进入，整体T2失败；不将普通分支成功或安全拒绝记为完整通过。项目 `addp-metrics-t2-b8a5b53b8a2d4255ac93ca69559904e5` 标准退出清理后，按项目标签独立确认容器、网络、卷均为零。`py_compile`、`bash -n`及`git diff --check`通过。下一步以专门会话回传的原生Linux目录分支结果作对照，再确定共享层维护窗口；本机正向读回及完整来源恢复验收继续保留缺口。

### 已有文件名的替换缓存与目录枚举对照（2026-11-01）

| 对照日志 | 首次读取与后续观测 |
| --- | --- |
| `/tmp/addp-hba-no-chown.log` | 去掉同属主 fchown 后仍失败，stat 仍为旧 7 字节。 |
| `/tmp/addp-hba-directory-lookup.log` | 初始普通创建仍复现；新文件名立即可读 14 字节。枚举后旧路径恢复，单轮尚不能排除时间因素。 |
| `/tmp/addp-hba-directory-enumeration.log` | 枚举提前，失败时为旧 7 字节／inode 36122；紧接枚举后为新 14 字节／inode 36124，读取匹配。 |

三轮 130 配置／26 生命周期及双域原生认证均通过；目录前置仍失败，HBA 两侧未变，目录正向验收未进入。项目 `addp-metrics-t2-bf1ca637fccc41c79c08f67b8362f9d8`、`addp-metrics-t2-69a07664fddd488ba239cb887d15710a`、`addp-metrics-t2-32552e5a75a1423d85cadb4a760d5ff5` 的容器／网络／卷均按标签核对为零。

现象集中在已有文件名替换；初始硬链接和 fchown 都不是复现的必要条件。上游 [Docker for-mac #7882](https://github.com/docker/for-mac/issues/7882) 与 [#6787](https://github.com/docker/for-mac/issues/6787) 条件不同，不足以指定相同缺陷或修复版本。

### 同进程、等时与正反枚举顺序对照（2026-11-01）

每组由同一容器 Bash 进程读取两侧旧内容、握手及同锁替换后读回。先等一秒不枚举，再单独枚举一侧，等一秒后枚举另一侧；两组交换顺序。校验同一 PID、Linux 单调时间及完整阶段，单组八秒／8192 字节。

宿主协议替身及负例覆盖阶段、PID、时钟、区间、预算与原 HBA 不变，仅属 T1。

`/tmp/addp-hba-single-process.log`：约 1010 毫秒两侧仍不可读，枚举 A 后仅 A 恢复；约 2010 毫秒 B 未枚举便恢复，不能推定固定 TTL 或唯一恢复条件。

`/tmp/addp-hba-single-process-balanced.log`：正反顺序均在约 1010 毫秒只恢复先枚举侧，另一侧至约 2010／2020 毫秒仍不可读、枚举后恢复。同一 PID 和同刻度对照支持枚举的独立作用；精确上游缺陷未知。

两轮 131 配置／26 生命周期及双域原生认证通过。首个目录读取失败保持，HBA 不变，目录正向验收未进入。项目 `addp-metrics-t2-714e53c6268943f787eb249e977cbca3`／`addp-metrics-t2-ddf06be8f89c46f683cb68bae160c5a0` 清理后独立核对容器、网络、卷为零。

### 无数据库的共享目录最小复现与修复建议（2026-11-01）

用户要求去除数据库依赖并明确共享层处理建议，继续禁止修改Docker设置、重启现有服务或设置定时任务。现将同进程Bash协议、单调阶段校验及八秒／8192字节观察收敛到 `scripts/test/directory-publication-probe.py`；PG目录失败诊断只适配命令和已有认证发布器，不再维护另一份脚本／协议。新文件只依赖Python标准库和Docker，复制单文件即可在ADDP仓库外执行；不是新增产品认证路线。

独立复现使用既有固定PG镜像里的Bash，入口覆盖为 `/bin/bash`，不启动PG、SQL、采集器或TLS，不读取ADDP配置、账号和HBA。宿主仅对随机临时目录中的普通文件执行write／flush／chmod／fsync／os.replace／目录fsync；容器只读、无网络／端口／额外能力，CPU0.5、内存64MiB、进程32。镜像声明的数据库VOLUME显式用64KiB tmpfs遮蔽，不创建匿名数据库卷。每次创建的容器都有随机项目及专属owner标签；异常回收前核对owner和项目，不删除外来对象，最后检查项目容器／网络／卷及临时文件为零。selector初始化失败亦回收传输。未匹配首次发布立即失败，后续枚举恢复不改判。

公共入口新增 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=directory-publication`，默认all同样覆盖它；该case的T1仅执行配置／协议所属的 `MetricsDeploymentConfigTest`，其余case继续执行原完整配置套件。新脚本登记到既有T2输入和Release `Monitor metrics infrastructure (directory-publication)` 矩阵，保留fail-fast=false及20分钟上限，无新权限、Hosted用途或运行依赖。新矩阵只为后续自然CI验证登记，本会话未查询、触发或重跑CI／Hosted。

两次最初完整配置预检分别运行133／135项，均因既有 `ServiceExpectationPublicationTest.test_standard_stop_publishes_selected_scope_before_the_first_stop_action` 的fake stop-dev脚本10秒超时而失败，尚未进入最小容器。未修改该夹具、停止逻辑或期限，也不把失败升级为通过。完成新case的正确范围划分后，最终两轮各77项相关T1通过；这不替代上述完整套件结果。

最终标准命令日志 `/tmp/addp-directory-minimal-proof.log`，项目 `addp-metrics-t2-12509b04692b460e8c03524f54a2ff95`：无数据库两组正反顺序均完成。两侧发布后立即不可读；第一组约1010毫秒仍不可读，枚举A后仅A恢复，约2020毫秒B仍不可读、枚举B后恢复。第二组约1010毫秒仍不可读，枚举B后仅B恢复，约2020毫秒A仍不可读、枚举A后恢复。未依赖HBA、CAS、数据库读取或重载即可复现。独立复现按共享契约失败退出1，外层make返回非零，不是修复通过；JSON的cleanup_verified、zero_containers／networks／volumes及temporary_removed均true，之后按该项目标签独立复核三类资源全零。前一轮 `/tmp/addp-directory-minimal-scoped.log` 有一侧在后续等待时自行恢复的记录继续保留，不能推导固定TTL。

`make test-t2-ci-registration` 的74项套件因四项Common execution-lifecycle夹具15秒超时失败，实际仓库检查因make顺序未进入；未改这些非本批夹具。随后只读执行内部事实检查 `python3 scripts/ci/check-t2-ci-registration.py --repository /Users/pampa/code/addp` 通过，确认当前登记命中，不将该局部结果称为整套登记门禁通过。`make test-workflow-security`通过（固定版本zizmor离线模式，覆盖现有工作流安全审计，不是线上CI结果）；py_compile、bash -n、git diff --check通过。PG来源正向完整门禁、本轮新Linux与Hosted仍未获得通过结论。

可交给维护人员的包为 `/private/tmp/addp-directory-minimal-b5kwiyfk.zip`，包含与仓库逐字节一致的独立脚本、运行JSON、有限版本信息和README；脚本SHA256为 `c864911b5034f3f0a62993686acbcd1c2c5161947a6a85120d76a9c5bdfaa4c5`。包内Docker设置注明为本会话前期只读快照，不冒充升级后或重启后的状态；未自动上传或向上游发消息。

修复建议已写入[开发服务与构建故障排查](../../../guide/troubleshooting/开发服务与构建.md#docker-desktop-共享目录原子替换后容器读不到文件)并加入故障总索引：保留不可见即拒绝，先在独立环境或授权维护窗口验证候选受支持Docker版本，保持VMM／VirtioFS不变；仍失败时才单独比较gRPC FUSE，分别跑相同复现及原目录认证／重载／恢复门禁。官方[发行说明](https://docs.docker.com/desktop/release-notes/)记录过缓存／宿主通知修订但未证明本例已修复，不能承诺升级或重启即可解决；官方[设置说明](https://docs.docker.com/desktop/settings-and-maintenance/settings/#general)只作为可选共享实现的事实来源。若长期要求避开跨宿主动态发布，应单独讨论VM内受管卷和部署侧发布职责，不在本批引入。个人环境及产品安全校验未改动。

<a id="redis-source-history"></a>

## Redis 账号、来源与专业查询

账号、发现、实源／固定 PromQL、受控浏览器和正式全链路分层记录。自然 CI／Hosted 只收录有实际回传的作业或阶段。

个人环境的 Safari 复验覆盖 AOF 关闭时 Infra Redis 的当前值和内存趋势，页面被判定为隐藏的触发条件仍待复现。[AOF 关闭时的快照与 Safari 复验](#infra-redis-关闭-aof-时的快照和-safari-复验2026-11-06)

本机 Business Redis 复验覆盖平台纳管、Docker Desktop Linux VM 主机关联、Prometheus 实际采样，以及 AOF 开启时 Safari 页面上的当前值和近五分钟内存趋势。[Business Redis 平台接入与 Safari 复验](#business-redis-平台接入和-safari-复验2026-11-06)

### Redis 固定采集与最小 ACL 基座（2026-11-01）

用户确认首期实例级INFO元数据、禁止业务键访问的范围后，本批同步维护术语、运行观测规范与 `monitor/backend/internal/redissource`。本批交付为部署侧采集内核与账号配方，不是已安装的专业来源：仅HELLO／PING／固定INFO，使用既有Go Redis客户端，关闭重试、维护通知和客户端信息上报；客户端钩子拒绝额外命令及旧AUTH回退。固定字段保留uint64精度、有效零及AOF未启用差别，缺字段／重复／禁用errorstats／超解析预算／身份变化拒绝；采样前后run_id和uptime保护同容器内重启边界。原始INFO、路径、任意错误标签及上游错误不进入输出。

账号配方完整reset、无键／频道／selector授权；启用只追加三个明确命令，停用同时清空旧会话的命令权限和密码。仍未实施正式部署账号归属、安装／持久化、凭据丢失／漂移处理与mTLS来源，也未修改个人Redis。采集器只读边界与部署admin的安装权分开；INFO账号可读取较宽的实例元数据，这一已确认边界不能表述为服务端字段ACL。

| 验证 | 实际结果 |
| --- | --- |
| 确定性 | 来源单元 7 项、Monitor 全 Go T1 通过。早期指标数量错写 21、误用 ACLGetUser 两项已修正。 |
| Redis 实源 | `make test-monitor-redis-source` 在同一随机项目的固定 7.2.13 实例分验两账号。验证 INFO 与原 reader、业务键／脚本／管理等拒绝、停用后旧／新连接拒绝、单次 OOM 与限额恢复。项目 `addp-redis-t2-38869-38873` 及早期项目标准清理，最终按标签容器／网络／卷零残留。 |
| 登记 | 事实检查及离线工作流审计通过；旧 Common 聚合漏选，最终由 Monitor 入口命中。 |

完整登记 74 项聚合仍失败，System Cleanup 的 no-tests 先超时，skip-child 又遇夹具锁／目录残留；局部登记成功未替代整组。正式账号部署、mTLS／发现、专业页面及 T4 当时未接通。

### Redis 账号持久化与来源生命周期（2026-11-01）

本批接通 Infra／Business 单机 Redis 的原生 ACL 启动、数据卷内本方账号记录、部署用户私有凭据与归属、显式启停及独立 mTLS 固定来源。唯一配方仍由 `redissource.ObserverACL` 生成；标准生命周期通过来源镜像的受限 stdin 计划调用该配方，不保留另一套账号生成器。源进程只持最小采集凭据，管理员只在安装期进入 stdin；源容器非特权、只读、限额，不挂载数据卷／宿主目录／Docker socket。Business 原 admin／reader 配方保留，Infra 保留原 default 认证。未修改个人 `.env`、安装个人账号或启停个人服务。

- **真实 Redis T2**。固定 7.2.13 的两套专属 Redis 通过账号原权限、独立 mTLS、重启密码持久化与代际、停用持久清权、坏 TLS／坏记录、标记数据保留。项目 `addp-redis-t2-34256-34261`、来源及本轮八个项目按标签核验零残留。
- **本地确定性**。Monitor Go、11 生命周期、Business Redis 5、共享生命周期 142、配置 136、Hosted 物理替身 6 及 T2／Online 登记检查通过。未重跑 PostgreSQL 实源。

前序首败分开保留：RESP3 对象被按数组解析；严格 TLS 拒绝缺 SKI／AKI 的夹具；坏记录恢复只还原文件未重载；Infra 夹具缺必需 Redis 密码。修正后标准门禁通过，早期项目已清理。

本批覆盖账号及来源生命周期。System 私有发现、HTTP SD、Monitor／Console 和正式 T4 尚未执行。

### Redis 自动发现与 System 精确来源关联（2026-11-01）

本批为共享工作区实现，尚未提交／推送。Redis复用唯一v2发现文件、System批量确认与CAS／审计／主机绑定、既有Monitor后台最小投影和HTTP SD；不新增权限、角色、租户引擎或业务键读取。发现校验本方工作区摘要项目、scope／Owner／服务、实际数据库容器ID及启动时刻、唯一私网网络身份与显式HTTPS发布端口，前后不一致时拒绝。来源与账号共享唯一项目命名函数；来源缺失／停用、数据库重启导致的标签失配分别表达，不借旧容器或PostgreSQL来源补齐Redis。System私有源只关联有效纳管部署，浏览器发现／声明不含端点。

相同形状的私有来源已统一为Common `EngineMetricsSource`，删除原PostgreSQL专属类型／文件，不保留别名；固定平台单容器目录集中定义PostgreSQL／postgres／5432和Redis／redis／6379。现有列表新增engine_type精确过滤，在计数／分页前执行；Monitor当前PostgreSQL页显式请求postgresql，Redis不被送入PG查询，也不提供尚未实现的专业页面入口。数据库端口缺失原因统一为database_port_missing，不保留旧SQL专用原因。System的原管理员确认界面已可同时确认两个作用域的Redis，默认名称按真实服务类型生成。

- **实源与登记**。双域 Redis 实源覆盖 available、重启后旧源 unavailable、重应用恢复、停用 not_deployed 及原 mTLS／清权／坏记录断言；`addp-redis-t2-48920-48925` 和两套来源及私有卷零残留。发现 44、来源生命周期 11、Go T1 与登记通过。
- **System／页面**。标准 IAM PostgreSQL Repository／API／Migration 串行通过，覆盖类型过滤计数分页、迁移 261、主机关联、私有身份及 CAS。System 156 单元、14 页面及最终 Redis 定向 1 项，Monitor 131 单元、27 定向与构建、Swagger 247 方法通过。原全量 231／561 中断并由入口清理，未计通过。
- **配置首败**。完整 136 项中的两个旧停止夹具分别在 fake ps 五秒及 stop 十秒超时；本批 77 项通过，完整聚合仍失败。原件位于 `/tmp/addp-redis-discovery-*.log`。

当时 259／260 尚未提交，261 只能按连续 Catalog 顺序发布；准备清单 `/tmp/addp-redis-discovery-owned-paths.json`、基线 `/tmp/addp-redis-discovery-baseline/`。后续前置提交见[正式全链路验收准备](#redis-正式全链路验收准备2026-11-01)。

专业页面、个人配置和正式 T4 未覆盖。

### Redis 专业指标查询与页面（2026-11-01）

沿上一批共享工作区继续实现，功能尚未提交／推送：前置迁移259／260仍为其他会话未提交WIP，本批261不能越过连续Catalog发布。当前User、部署／有效主机、当前容器和来源在查询前后均重核，沿同一engine_resource_observations／summaries／trends接口和唯一Prometheus传输接入，不新增权限、租户归算或Monitor直连Redis。PG专用查询传输文件已删除，PG／Redis只保留各自固定目录及表达式，共用计划／预算、传输、批量摘要和归一化；不保留旧类型或方法别名。

Redis目录共22项：运行时长、五项内存容量及限额占用率、三项连接、六项计数速率及命中率、五项持久化／加载状态。详情分组10／7／5项，显式切换后只查当前组，沿用单次最多12项预算；列表按System类型过滤后分页，摘要固定四项。内存限额占用率不截断100％，maxmemory为零时无比例；无查找请求时无命中率。六项速率及命中率需要完整一分钟、至少四样本与边界、同一counter_epoch和完整新鲜快照；缺样、状态非法、重复／缺少代际、容器身份不符和可见重置均不输出旧值或补零。沿用当前容器运行段趋势，不宣称识别不可见重置。

Console原引擎页增加类型筛选、Redis分组、指标旁中英tip及语义状态；容量／时长复用共享格式化。趋势沿用现有共享缩放，手动／自动刷新保留图表DOM和视窗，失败或权限撤销清除旧值；System Redis部署的查看监控入口已开放。没有为Redis另建查询页或重复图表实现。

- **Go／实源**。Monitor 全 Go T1 通过，含权限、当前容器／来源／策略重核与 12 项预算。标准 Redis 实源通过；同一固定 promtool 的 15 项表达式验证速率、命中率、零上限、缺边界及重置／代际拒绝，受控样本未覆盖真实 HTTP SD。首次只读根缺临时目录，补有限 tmpfs；最终项目 `addp-redis-t2-95846-95851`、双来源、私有卷和临时文件零残留。
- **浏览器／登记**。Monitor 132 单元、PG 27＋Redis 5、最终 Redis 5 与构建通过，覆盖双语分组、刷新、缩放和撤权清空；System 156 单元、Redis 定向 1 与构建通过。Monitor Swagger 96 方法和登记通过，未计全量浏览器或正式身份。原件位于 `/tmp/addp-redis-query-*.log`。

另收到既有Platform CI的确切失败证据（源0e69a25b1，run41304702173／job124842615454），根因在infra-port-resolution.sh临时停止夹具未复制观察来源新依赖。已仅补齐Redis入口及唯一共享私有状态／Docker／发现帮助文件，显式隔离并关闭本夹具Redis来源，再断言未安装停用状态。现工作区和冻结HEAD加单文件改动均沿 `bash scripts/test/infra-port-resolution.sh` 通过，日志 `/tmp/addp-redis-query-ci-fixture-fix.log`、`/tmp/addp-redis-query-ci-fix-frozen-head-final.log`；独立提交并推送 `db402ce4c965c402854ab6a008b6bd276b89349e`，不包含Redis功能或前置迁移。未查询／重跑该CI，不把本地修复当作线上Job已通过。

正式 System 确认、持续 HTTP SD、当前 User 查询、真实 Console 及 Hosted 尚未执行。

### Redis 查询发布前的混合摘要与超限呈现复核（2026-11-01）

用户继续推进后，只读确认前置259／260仍未提交，本批261及Redis查询／页面继续保留工作区，不越过发布条件。补足新共享查询路径的有意义边界验证，不扩大产品目录、权限、预算或Hosted用途：同一次PG／Redis批量摘要只发两次共享form读取，按类型分别归一化；有效零值和180％Redis内存占用率保留，Redis快照失败仅清空本部署，外来deployment标签或跨类型指标导致整批拒绝返回。`make test-module MODULE=monitor GO_ONLY=1` 全部通过，日志 `/tmp/addp-redis-query-summary-transport-go.log`。

Console同一Redis受控夹具补中英文“超过100％→未设置上限”场景，验证列表／详情保留180.00％、趋势真实props不裁剪且原生悬停显示180.00％，切换无上限后比例为空并显示未设置上限；未更改产品公式、展示或轴逻辑。标准 `node ../../scripts/test/frontend-browser-gate.mjs e2e/redis-resources.spec.js` 最终7项T3通过（15.9秒），日志 `/tmp/addp-redis-query-over-limit-browser-final.log`；复用原刷新／缩放／tip／分组五项，不计正式Safari、User认证或实源页面T4。只有测试夹具及断言新增，前一轮产品构建／原生PromQL与实源T2输入未改变，不重复运行仍有效的T2或扩大为整套浏览器通过。

当时发布仍受 259／260 阻塞，索引未暂存。路径和有界复核分别在 `/tmp/addp-redis-discovery-owned-paths.json`、`/tmp/addp-redis-publication-review-snapshot.json`。

同日 22:50 仍未发布。Redis 正式全链路仅提案，人工 workflow 尚无该 profile；限定身份与用途，未跑真实 T4。只修 Swagger 标题空格，96 路由覆盖复核通过。后续实施和本地门禁见[验收准备](#redis-正式全链路验收准备2026-11-01)。

### Redis 正式全链路验收准备（2026-11-01）

用户确认限定用途后，先更新既有《平台观测与语义生命周期》Redis条款，再为同一`platform-node-metrics`增加唯一人工`redis-engine-admission`选项。沿既有正式三员／真实密码MFA、独立Prometheus与当前User链路，不新增权限、角色、测试身份旁路或定时任务。复用原PG的登记／查询／浏览器核对流程，按真实类型选择固定目录；Redis三组10／7／5项、四项摘要和当前容器趋势均须读取，不因单次12项预算拆成隐式后台扫描。PG原profile继续独立。

物理夹具只调用标准Business Redis Compose和唯一`redis-metrics.py`生命周期，生成当次且相互独立的管理／只读凭据；来源进程仅收到自身作用域所需管理凭据，不带业务只读密码、其他数据库密码或System身份。Redis不走PG认证目录，不增加数据库重启场景、键流量或加压。Business纳管停用／恢复和采集来源暂停／恢复各自验证旧值清空、Infra仍有效及新鲜采样恢复。候选确认按精确作用域／服务选择当次两行，不使用表头全选混入另一类引擎。

来源项目名复用生产解析内核，解析禁止写Python缓存，以免改变Hosted只读预检的干净工作区。除Compose项目外，对生产owner管理的两个专属私有输入卷做启动前占用拒绝和退出零残留核对，不盲删外来卷。Business凭据不可读取时仍尝试清理Infra，并保留整体失败；本地模拟Docker测试不能证明真实Linux退出已清零。

- **确定性与清理协议**。runner 173 项、System Metrics 身份 Go、共享生命周期 39 项和登记通过，覆盖本地 OpenSSL、受控 Docker、专属卷占用拒绝／失败回收、两域三组及趋势回执。日志 `/tmp/addp-redis-hosted-*.log`。未实际执行 Hosted 或验证其零残留。
- **页面与发布前置**。Console 定向 4 项 T3 通过，验证 Redis 分组／刷新／返回保持 iframe 与类型。259／260 随 `7da941ee6` 提交后，Common／System／Monitor Go T1 通过，261 可按顺序发布。

前序失败保留：旧 PG 回执缺 metrics、临时 CLI 探测、Bash 空数组／解析产生缓存；Console 两个原 PG 筛选断言漏写新 engine_type。修正后才由以上入口验证。

Redis 实源 T2、PromQL、受控 T3 各自有效，正式 System 确认至真实 Console 及退出清理尚未执行。

### Redis 来源状态与恢复的受控页面验收（2026-11-02）

沿现有Redis固定目录及前端状态契约补验收覆盖，本批没有新增产品状态、API、权限、采集或部署动作。复用唯一PG／Redis受控夹具，分别模拟未部署来源、来源不可用、暂无采样、抓取失败、快照无效、样本过期；采集状态／时间及空点按既有后端分支构造。夹具旧`sql-failed`模式统一改为`snapshot-unavailable`，删除原名，不为Redis保留另一套SQL或来源错误路线。

中英文列表／详情逐一验证异常状态直接可见、当前数值为“—”、恢复后显示本轮不同读数；部署停用／恢复仍沿System对象事实与版本变化。没有有效来源的趋势为空；当前抓取失败／快照无效／过期可与先前有效历史点共存，只将当前缺样置空，不把缺样补零或抹掉合法历史。另补两语种自动刷新：来源不可用后清值，隐藏时所有部署／指标读取暂停，显示时只恢复一次；HTTP403清空数值／canvas并停止自动请求，时间推进及隐藏切换均不能绕过，只有手动重读后恢复。使用受控HTTP与Playwright时钟，不执行真实来源启停或设置应用定时任务。

最终标准 `npm run test:e2e -- e2e/engine-resources.spec.js e2e/redis-resources.spec.js` 52项T3通过（1.8分钟，`/tmp/addp-redis-source-states-final.log`）；含新增6项组合场景和原PG／Redis全部已有断言。共享夹具其他直接消费者也沿标准浏览器门禁复验：Console的`engine-resource-navigation.spec.js`4项及`engine-monitor-navigation.spec.js`3项通过（`/tmp/addp-redis-source-states-console.log`、`/tmp/addp-redis-source-states-owner-navigation.log`）；Monitor的`automatic-filter-enter.spec.js --grep 'engine Enter submits'`1项通过（`/tmp/addp-redis-source-states-enter-final.log`）。最初额外Enter命令的`^engine Enter`正则未匹配Playwright完整标题，报“No tests found”，不计通过；修正调用筛选后实际运行1项，无产品或断言放宽。

对本批三项测试路径调用现有`changed-gate.py`影响函数，精确命中console／monitor；既有两前端CI门禁会自动发现，不新增Makefile／workflow。产品源码、依赖、构建输入和T1输入未变，复用[对象变化回归](#引擎当前对象变化的原位刷新保护2026-11-02)的构建与单测，不重复无变化的来源T2或Go门禁。此次共60项受控T3，不证明个人Safari、真实HTTP SD／Prometheus或正式身份T4，也不计为完整Monitor／Console浏览器门禁。CI／Hosted仍等专门会话安排回传，未查询、触发、重跑或设置定时任务。

### Infra Redis 关闭 AOF 时的快照和 Safari 复验（2026-11-06）

用户经标准入口安装 Infra Redis 来源，并确认平台接入与主机绑定。Prometheus 实际抓取为 UP，并读到 19 个原始指标。Safari 检查器曾读到 Console 顶层和 Monitor 内嵌页均为 hidden。用户刷新恢复可见后，Monitor 仍报“未取得有效数据库快照”。原始样本确认 AOF 开关为 0，采集器按约定省略最近写入和重写状态。原 Monitor 查询无条件要求这两项，导致整份快照被拒绝。

修正后，关闭 AOF 的快照可展示基础指标，两项 AOF 结果留空。开启时仍要求两项有效结果来自同次采集。Redis 专项先复现基础指标丢失，最终 `make test-monitor-redis-source` 通过 21 个 PromQL 用例及双作用域实源生命周期检查，独占容器、网络、卷零残留。Monitor Go T1 通过。日志见 `/tmp/addp-redis-aof-guard-red2.log`、`/tmp/addp-redis-aof-guard-final.log` 和 `/tmp/addp-redis-aof-monitor-go-final.log`。

用户只重启 Monitor 后，原生 Safari 详情显示“正在采集”和“自动刷新已开启”。实际读到当前连接 34 个、分配内存约 1.6 MiB。选择分配内存后，近五分钟曲线绘制了实际采样点，15 秒评估间隔，时间窗口随刷新向前推进。本次个人环境只验 Infra Redis 当前值及内存趋势，Safari 曾判定隐藏的触发条件仍待复现。

### Business Redis 平台接入和 Safari 复验（2026-11-06）

本机 Business Redis 使用独立回环来源 19113 和独立服务器证书。标准启动后 Redis 健康，首次采集安装在部署检查阶段拒绝。原因是私有 `business/.env` 缺管理员配置，而已运行容器使用 Compose 的开发默认值。核验两者应采用同一配置并补录后，沿 `metrics-sources.py up --scope business --kind redis` 安装采集来源，原数据卷继续使用。Monitor 从交互式终端重启后，就绪接口和 Console 内嵌入口均可达。

按用户确认保存 Business Redis 平台登记，并绑定 Docker Desktop Linux VM。Prometheus 复验 11 个受管目标全部 UP，Business Redis 有 21 个原始指标、AOF 开启、1 个连接。原生 Safari 显示“正在采集”和“自动刷新已开启”，读到约 1.33 MiB 分配内存、256 MiB 内存上限及 1 个连接。近五分钟内存曲线绘制了纳管后的实际采样点，评估间隔 15 秒，窗口随刷新推进。本次证据覆盖本机 Business Redis 平台纳管、实际采样、当前值和内存趋势。

<a id="minio-source-history"></a>

## MinIO 账号、采样与专业查询

MinIO 实源门禁、前端产品步骤及个人 Safari 分别认定。上游未发布的数值仍留空，来源恢复不自动证明页面或 Hosted 通过。

本机 Safari 实测覆盖 Infra MinIO 详情当前值、空间占用率和请求速率曲线，以及容量趋势的悬停、拖动缩放和手动刷新及一次自动刷新后的缩放保持。覆盖范围限于当次短时窗口。其余趋势、七天范围、图例、错误规则、滚动位置保持、DOM 是否重建及页面闪烁仍待验证，页面曾被判定为隐藏的原因也待确认。[Safari 页面与趋势交互实测](#infra-minio-重新应用后的-safari-真实页面与趋势交互2026-11-04)

本机健康来源恢复实操中，Infra MinIO 的 `recover` 返回“来源运行正常，未重建（采集待验证）”。后续只读核验确认来源唯一且在运行、Prometheus 唯一目标为 UP，并读到新鲜采样。未重建以当次命令输出为据，命令前的容器 ID 缺少收据。跨 Docker 停机恢复和其他来源的实操仍待验证，页面交互以单独的 Safari 记录为准。[本机 Infra MinIO 健康来源恢复实操](#本机-infra-minio-健康来源恢复实操2026-11-04)

### MinIO 指标身份生命周期与隔离权限（2026-11-02）

本批按已确认的17项边界先实施账号阶段：Infra／Business分别创建仅`admin:Prometheus`内联策略的原生Access Key；部署工具核验本地Engine、Compose归属、唯一当前容器及具创建时刻的数据卷身份。0700／0600私有状态保留同一AK／SK，正常重复启动不切换账号、不改写凭据；未安装且关闭不访问Docker，已安装则沿标准停止入口停用并核验旧Token被拒绝。账号归属、父用户或策略漂移、丢失凭据及创建结果未知均不收养或重建。个人环境未安装、启用或重启，没有改ADDP权限。

真实隔离测试暴露固定MinIO版本的原生enable／disable会在只更新状态时移除内联策略。已删除产品中的此调用，镜像内专用工具复用已锁定mc模块的管理SDK，在一次请求中同时提交固定策略与状态；只接收私有stdin，固定回环端点、16KiB输入及五秒请求预算，未知策略输入拒绝。固定版本读回资源通配符为`**`，只接受这一原生形状和明确非继承策略。最初未完成的协议／策略及夹具尝试均不计通过，没有降低鉴权或权限断言。

最终`make test-monitor-minio-observation`于macOS Docker Desktop／Linux ARM64一次性环境通过（`/tmp/addp-minio-observation-t2-verified.log`），内含14项确定性T1。两个MinIO使用不同随机部署根凭据，无宿主端口；验证真实v3指标200、过期／错误签名403、另一scope凭据403；先用根身份证明相同S3签名请求有效，再验证采集身份的原生GET／PUT返回403及XML `AccessDenied`，列表／对象元信息与账号新增／列举也被原生鉴权拒绝。启停重读最小策略，旧Token停用失效，两scope互不影响；两容器保卷重建后同一身份及权限保持。工具拒绝额外任意策略输入且不改变账号。退出核验当次容器、网络、卷、独占镜像标签及私有状态清理；最终报告containers/networks/volumes/private state均为0。

共享生命周期先整组通过，取消重复启动前多余停用后复验在末组 142 项的旧 HTTP 超时夹具首败（`test_native_timeout_reaps_owned_group_without_completing_the_stage` 缺 PID 文件），整组仍失败。受影响 Infra 定向 40 项、发现 44＋6、端口、登记 75＋9、缓存 15 与构建登记通过。其他引擎实源未重跑。原件 `/tmp/addp-minio-observation-*.log`。

本批覆盖账号 T2；固定 17 项来源、mTLS／发现、查询、页面和 T4／T5 尚未验。

### MinIO 固定 v3 指标与 mTLS 来源（2026-11-02）

沿已确认17项范围接通部署侧来源，未新增产品API或权限。`internal/miniosource`固定读取四个v3端点和前后进程边界，每轮三秒、最多五请求、每响应256KiB／4096样本，不使用代理、重定向、DNS、重试或旧缓存。严格验证固定原生标签／类型、数值和同一进程，输出前移除全部原生标签；计数仅携带当前部署／进程的摘要代际。只输出原始数值，速率／占比仍留给后续唯一查询层。对象扫描时间按该端点的实际读取时刻计算，与整轮采集时间分别记录。

固定上游的`MetricValues.Set`省略零值；真实原生端点确认健康时的离线盘数、在途／5xx及尚未扫描的对象统计可能缺席。来源保持缺样，不据HTTP200补0，不切换v2或其他接口，也不承诺17项健康时全有值。原生接口权限仍为独立`admin:Prometheus`，过滤只限制输出，不冒充权限隔离。

Redis／MinIO在Monitor内部复用唯一固定HTTP/TLS服务和受管来源部署内核，删除原Redis重复实现；各自保留协议、账号和字段适配。mTLS TLS1.3只允许无query／请求体的GET /metrics，单并发、完整缓冲、失败503且无失效数据。标准Infra／Business启停将受限账号与来源一起调和，私有配置复制到独立卷，非root／只读／0.25CPU／128MiB、只连核验后的标准数据库私网。失败或关闭先移除本方来源／配置卷，再停用已核验的账号，清理未证实时单独报错；运行来源不持root凭据、不挂载数据目录／Docker Socket，不收养陌生来源或账号。

- **T1／登记**。账号与来源 21 项、生命周期组合 30 项和 Monitor Go 通过；校验私网、归属、拒绝漂移、失败清理、固定字段、代际及请求约束。T2／构建登记、端口及语法通过。
- **实源 T2**。双 MinIO 原生账号、S3／管理拒绝、JWT／跨域证书拒绝、mTLS、503 无旧值及保卷重建均通过。退出容器／网络／卷／私有材料为零。共享 Redis 实源和 15 项固定 PromQL 回归亦通过并清理。日志 `/tmp/addp-minio-source-{t2-final,redis-t2}.log`。
- **Infra 组合失败**。40 项中 1 项停用夹具超出原 30 秒期限，原输出不完整、根因未定。后续单项 2.874 秒通过，原组合仍记失败。

最初一轮编译因扫描时刻参数替换误触JWT调用失败，在启动夹具前修复；随后来源T2的保卷重建检查要求请求计数，但新进程还无S3调用，固定原生端点按正值发布规则没有计数代际。夹具补入重建后对自有对象的真实GET，同时验证对象持久化及新计数；不改产品输出、补零、降低边界或等待伪造样本。最终上述来源T2完整通过，前次失败及物理清理结果独立保留。

本批未覆盖自动发现、System 私有投影、专业查询／Console、正式 T4 或生产 T5。

### MinIO 自动发现与 System 精确来源关联（2026-11-02）

- 范围：Infra／平台自管Business单容器MinIO，沿唯一部署发现v2、管理员确认及既有主机关联、私有投影和HTTP SD。无自动纳管、租户Engine／S3数据读取或新增IAM权限。平台目录沿发现协议使用minio，登记只保留9000 API端口；000262仅扩展类型与服务匹配约束，不改历史迁移、声明或权限。
- 只读发现核验工作区／scope唯一来源项目、Owner、数据库完整容器ID／启动时刻、单一来源网络、Compose网络归属及显式HTTPS端口。Infra数据库多网络时来源仍只加入addp-network，Business为business-network。Redis／MinIO复用单来源发现内核，保留Redis原网络集合约束；缺失、错配与停用分别失效，不跨类型顶替。端点不进入公开DTO、确认摘要或持久声明。
- System严格JSON白名单增加minio_metrics，仍拒绝未知、重复和大小写别名字段；当前来源只与单个运行容器精确匹配。私有投影须先有有效纳管主机关联，再经原Monitor准入进入HTTP SD，标签保留真实minio类型。管理列表支持MinIO候选和名称，专业指标页未适配前不展示错误跳转。
- T0／T1：`make test-deployment-discovery`通过45项发现测试和6项容器证明测试；`make test-monitor-minio-observation-unit`通过21项账号／来源Python及现有Go来源测试。`make test-module MODULE=common GO_ONLY=1`、同入口Monitor和System均通过。初次System失败包括目录版本断言未同步，以及JSON白名单遗漏，均已修正后复验；不把初次失败记为通过。
- T2：`make test-monitor-minio-observation`最终版本通过真实独占双来源mTLS、发现available／not_deployed、精确容器身份／启动时刻／HTTPS端点、重建恢复和原最小权限拒绝，容器／网络／卷／私有材料=0。`make test-monitor-redis-source`通过原真实发现、15项固定查询、生命周期及退出清理回归。均未启停或安装个人MinIO。
- PostgreSQL T2：标准入口`make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package <migration|repository|api> --test engine-deployments"`三包串行通过，实际addp-postgres映射25432、数据库仅addp_iam_test。覆盖Redis／MinIO前向发布及重复执行、权限不变、类型／服务错配拒绝、来源唯一性、有效主机关联、CAS及停用后退出私有投影、原HTTP审计事务。迁移回归参数化后再次通过migration包；未应用个人addp开发库。
- 前端T1／T3：`make test-system-frontend`通过158项单元、原233项受控浏览器和构建；新增MinIO确认场景后，`make test-system-frontend SYSTEM_FRONTEND_BROWSER_ARGS='e2e/engine-deployments.e2e.js'`通过158项单元、15项登记页浏览器和构建，包含Infra／Business MinIO显式主机关联、名称和确认请求。不是Safari真实运行或正式身份T4。
- Swagger：`bash scripts/swagger/gen-swagger.sh system`和`bash scripts/swagger/check-route-coverage.sh system`通过，248个公开路由方法覆盖一致，类型筛选枚举加入minio。`make test-t2-ci-registration test-build-registration` 通过。
- 本地日志：`/tmp/addp-minio-discovery-{python-final,common-t1,monitor-t1,system-t1-pass,system-pg,system-migration-final,t2-final,redis-t2,system-frontend,admission-ui,swagger,registration}.log`。临时路径不是长期交付附件；上述可重放标准命令和断言范围为证据正文。
- 剩余：MinIO固定查询、17项指标与Console专业页尚未实现；当前来源受上游零值省略约束，不能补造0。CI／Hosted由专门会话安排回传，本会话未触发、重跑或轮询，不建立定时任务。

### MinIO 固定查询与专业监控页面（2026-11-02）

- **契约**。沿既有三查询接口与当前 User／主机关联，返回前重核来源、版本和策略。列表四摘要，详情 10／7 分组，各次最多 12 项。验证同次 up、观测及容器边界、同代际四样本与完整一分钟；5xx 无分母、未扫描或上游未发布留空。真实扫描允许早于当前容器，整数及单位约束保持。
- **实源与表达式**。Go T1、双 MinIO mTLS／账号权限／发现／持久化及 40 个固定 PromQL 场景通过。首败是早期扫描夹具未同步容器身份标签，旧版正确返回空；修正夹具后通过。首败项目容器／网络／卷为零，最终含私有状态亦清零。共享 Redis 15 表达式与实源清理独立通过。
- **页面及失败**。Monitor 133 单元通过，首次完整浏览器 600／602，两个 MinIO tip 选择器误匹配“查看趋势”。修正后专项 61／62，英文返回按钮名预期错误；再修正后 MinIO 两项及构建通过，原完整运行仍失败。覆盖双语、10／7 请求、空值、类型归一与返回、刷新／缩放、隐藏暂停及撤权。System 158 单元／15 浏览器与构建、Monitor Swagger 96 方法及登记通过。
- **边界**。个人真实来源、Safari 与正式 T4 未覆盖；后续固定前端和实源 Job 见下文。原件 `/tmp/addp-minio-query-*.log`。

#### MinIO 专项 CI 回传（2026-11-02，dff267ed0）

固定 `dff267ed00aef78f009dcc54ac33fe62305c7aa0` 的 [Release MinIO专项](https://github.com/pampa0629/addp/actions/runs/41424138332/job/125206871137) 为 success。实际来源与身份步骤 selected=true，21 项脚本检查、40 项固定 PromQL、容器／网络／卷／私有材料零残留。原件 `/tmp/addp-ci-20261102-180000/Monitor-dff-MinIO-source.log`。同批前端 Job `125206526177` 在记录时运行中，未取得终态；后继前端证据对应另一个源码及 Job。Safari 与 Hosted 未覆盖。

#### Linux 完整前端产品步骤回传（2026-11-02，2cd45ec2）

固定 `2cd45ec2af6de07feed8e61d2df1d01962d8c0eb` 的 [后继提交Monitor前端Job](https://github.com/pampa0629/addp/actions/runs/41429831469/job/125225339390)。实际前端步骤 completed/success，133 单元、602 浏览器（含 MinIO 中英场景）及 Vite 构建均完成，selected=true。

整 Job 仍 cancelled，annotation 为 `The job has exceeded the maximum execution time of 30m0s`。原件 `/tmp/addp-ci-monitor-2cd-step-success-20261102/`。两源码的 Monitor 前端树均为 `e901d153bb1a06b02658412f23d87339791f8d30`，共享前端与 Makefile 有差异。该结果只认固定前端步骤通过，MinIO 独立实源、个人 Safari 与 Hosted 各自认定。

### MinIO 原生计时基准差异导致整批误判（2026-11-03）

Infra UP 后真实专业页仍显示无有效快照。旧判定强求 `abs(observed_at-start_time-uptime)≤5`，实际差 3798.35 秒。procfs 还原原生启动值，uptime 反推与 Docker 启动接近，二者计时基准不同；底层差异根因未定。计算收据 `/tmp/addp-minio-clock-proof-20261103.json`。[固定版本进程指标](https://github.com/minio/minio/blob/9e49d5e7a648f00e26f2246f4dc28e6b07f8c84a/cmd/metrics-v3-system-process.go) · [应用启动记录](https://github.com/minio/minio/blob/9e49d5e7a648f00e26f2246f4dc28e6b07f8c84a/cmd/globals.go) · [procfs算法](https://github.com/prometheus/procfs/blob/v0.16.1/proc_stat.go)

先明确运行观测规范的原生时间来源，再删除唯一MinIO查询路径中的错误加法等式；保留同次唯一UP／字段、16秒新鲜度、采集时刻偏差、当前容器身份／启动边界、非负值、一分钟四样本／完整窗口／重置／counter_epoch保护。不增加Desktop特判、容差配置、替代时间或补零。原API目录、DTO、权限和Swagger契约不变。新增六项真实PromQL情形：计时基准不同的容量／运行时长／请求速率有效，以及未来启动、当前容器前启动和负运行时长拒绝。

旧标准门禁首败为容量和运行时长返回 nil，账号、mTLS、持久化已通过；当次及双来源容器／网络／卷为零。修正后 21 Python、双实源及 46 PromQL 通过，容器／网络／卷／私有状态清零。Monitor Go 首轮另有模拟 Docker 生命周期 30 秒超时、根因未知，待 T2 结束后同命令通过。原件 `/tmp/addp-minio-clock-*-20261103.*`。个人双 MinIO 容器身份、启动和重启计数不变，真实页面仍待应用后的独立复验。

### Infra MinIO 修复应用后的真实页面截图（2026-11-03）

用户确认重启Monitor后，公共ready显示运行构建`eb8af602ca8a494142cbbf201104884e92cd1433`，包含`04ce9ec57`且MinIO查询文件与该修复逐字节一致；System／Monitor／Prometheus ready及MinIO mTLS来源均200，来源14项样本、精确部署目标UP，Business仍not_deployed。只读收据为`/tmp/addp-minio-runtime-build-20261103.json`与`/tmp/addp-minio-after-monitor-restart-20261103.json`。

用户提供Safari实际截图，页面为平台系统管理员的Infra MinIO，关联原Docker Desktop Linux VM，显示正在采集：运行21小时47分钟5秒、在线1盘、总容量1.87TiB、剩余866GiB、空间占用54.71%、最近扫描对象10.1GiB／7408个／10桶、扫描距今1分钟25秒。容量／状态十项中九项显示真实值，离线盘数留空与此前原生省略事实一致，不补造0。空间占用率近五分钟曲线实际渲染，页面标注10:09:20—10:14:20、15秒评估间隔。本图补齐修复应用后的容量／状态及该趋势真实显示证据；不是自动化交互通过，也不据标签“自动刷新已开启”推导刷新、缩放或视口保持已经验收。

截图附件`codex-clipboard-8e405148-6c4f-483b-a5fd-c8f86b97559e.png`，SHA256=`37a38369c3d7917345f591f73bb8d90a2814e1b5fe1a97ae49827fb19e7bdb22`。后续工具读取时Safari再次报告隐藏并按原规则清除观测，不能用该稍后状态否定用户截图，也未绕过隐藏保护。请求／流量七项、真实缩放与手动／自动刷新保持尚待实操复验；Business和Hosted／T4未验。CI／Hosted仍由专门会话回传，本会话未设置定时任务或启停个人服务。

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

<a id="engine-ui-history"></a>

## 引擎页面、导航与刷新

受控浏览器用例、原生 Safari 输入、用户反馈和真实采样分别记录。页面隐藏、前台恢复及权限失效以实际观测为准。

### 引擎监控的当前承载主机导航

2026-10-22，引擎监控列表与详情增加当前承载主机名称和资源跳转；沿 System 当前有效 `node_id`，只在已有 `platform.host_node.read` 下读取当页主机名称。不回用 `declared_node_id` 或旧部署别名；失效关联明确显示当前无有效关联，名称读取失败只保留当前 ID，不清空合法引擎指标。服务与引擎页面共同使用 Monitor 的 `MonitoredHostLink` 领域组件，删除服务原内联导航；名称加载继续复用唯一 `loadHostReferences` 的去重、四并发和迟到结果保护。跳转使用原 Monitor 导航入口及 push，浏览器返回恢复原筛选、详情、指标、范围和刷新选择；不增加权限、API、后台读取或部署操作。

冻结基线 `1e59fda800edf9ff1e8d33ddccb891d123b765f6`、只叠加本方前端输入的 `make test-monitor-frontend` 退出 0：87 项 Node 单元、266 项确定性浏览器及生产构建通过。新增五项浏览器覆盖中英文详情/主机跳转/原趋势 URL 返回、列表当前名称与改名、名称 403 时指标保留、撤权拒绝迟到名称与后续额外读取、失效关联不回用旧声明；原服务主机名称与撤权回归亦包含在完整门禁。中英文截图已检查，保存在 `/tmp/addp-engine-host-nav-visuals/`；完整日志 `/tmp/addp-engine-host-nav-frontend.log`。成功后原浏览器输出目录与端口租约由标准入口移除。

`make test-frontend-ci-registration` 最终通过 20 项登记回归及 21 个受控前端登记检查；首次因冻结副本没有 Git 元数据而未完成实际登记检查，补齐副本元数据后重新执行才计通过。九项实现/测试输入与验证副本逐文件一致，另核对冻结基线的 8677 个未改跟踪 blob 一致；仅文档摘要随后更新，不以共享工作区其他改动替代验证输入。

本批没有读取或轮询云端 CI、调度 Hosted、恢复定时任务、修改个人数据库或重启服务。上述结果为 T0/T1/T3，不替代 PostgreSQL 正式身份 T4、原生多宿主/多进程或生产 T5，云端结果继续等待专门会话回传。

### 个人环境 Business PostgreSQL 专业页面与趋势复验（2026-10-30）

用户恢复 Monitor 后，本机直连核对 Console 的 Monitor 模块页面为 HTTP 200、Monitor Ready 为 200；Business 来源仍有全部 16 项固定原生样本，读取时距采样约 0.004 秒。本轮通过原生 macOS Safari、真实已登录平台系统管理员，进入原部署 `9e4fd2b0-3b45-473e-b070-cc6f92e40e11`，没有新增或修改主机、部署、规则或采集配置。该部署旧显示名称仍为 `infra-虚拟机`，实际 Business 身份沿前述精确来源关联，不按名称猜测；本轮未改名。

真实列表同时显示 Infra 与 Business 均“正在采集”，Business 四项摘要为总连接 0、活跃连接 0、每秒提交约 0.62、容量 78.3 GiB。详情实际读出全部十二项：七项连接／等待指标均为有效 0，最大连接数 200，提交速率随自然采样约 0.53—0.62 次/秒，回滚及死锁速率均为有效 0.00 次/秒，容量 78.3 GiB；承载主机为既有 Desktop Linux VM。页面明确说明采集事务不等于业务吞吐、整体容量不归算租户。

未点击手动刷新时，五分钟趋势结束时间从 12:31:26 自然推进至 12:31:56，符合当前十五秒刷新选择。通过真实指标下拉框切换到容量，五分钟容量曲线在截图中实际可见，纵轴为 GiB、曲线约 78.3，评估窗口为 12:33:33—12:38:33；切换一小时后，容量趋势返回 11:44:55—12:44:55、十五秒评估间隔。再通过下拉框切回每秒提交，保持 `range=1h`，实际截图显示 11:46:07—12:46:07 的波动曲线及“次/秒”单位。最初趋势超出 Safari 可见窗口；经原生窗口缩放与滚动后实际看到曲线，没有修改产品 CSS 或以仅存在 AX 图像节点当作绘图通过。

随后用 Safari 原生整页刷新，Console 与模块地址均恢复同一 Business 部署及 `range=1h`，所选指标仍为每秒事务提交；十二项即时值重新读出，趋势返回 11:47:02—12:47:02，无 forbidden／空白。摘要 `/tmp/addp-pg-business-page-verification.json`；实际操作与截图保留在本会话。本轮仅增加运行证据，最小文档检查为 `git diff --check`，没有产品改动需要重跑构建。未在本轮新验悬停、七天自然留存、跨数据库重建／统计重置或撤权；不替代此前失败的 Desktop 实源 T2，也不认定新的 CI／Hosted 通过。个人服务均由用户操作，CI／Hosted 继续等待专门会话回传，未查询、重跑或设置定时。

### Business 部署改名与专业趋势时间缩放（2026-10-30）

用户明确同意将 Business 旧显示名称改为 `Business PostgreSQL`。本轮在真实 Safari 的 System 部署详情中，只修改既有部署 `9e4fd2b0-3b45-473e-b070-cc6f92e40e11` 的显示名称，保存成功后版本由 1 变为 2；点击“重新读取并核对”仍返回新名称、启用状态与原 Desktop VM 承载声明。关闭详情后列表保留 Infra／Business 两条独立部署；从改名后行的“查看监控”进入 Monitor，地址仍为同一部署 UUID，标题采用新名称，正在采集、最大连接数 200、容量 78.3 GiB 仍可见。整页刷新后新名称保留，没有重建数据库或采集来源。

趋势复用共享 ChartRenderer 临时缩放。成功刷新保留相对视口；切换部署／指标／时段／身份或失败时重置。查询、样本和权限不变。

三项双语专项覆盖拖动、原值／请求不变、刷新／主题保留和失效重置。首败为只读选择器被选中标签挡住，改用实际外框后通过；加载旧英文按钮断言的整轮已中止并清理。

最终 131 单元通过，516 浏览器中引擎场景全过，另五项在 HTTP 机器身份、配置撤权、主机 inode、英文告警筛选和进程比较超时，原入口失败、未到构建。五项原样定向复验与独立构建通过，超时根因未定；中止及复验隔离 runtime 已清理。原件 `/tmp/addp-engine-zoom-*.log`，改名收据 `/tmp/addp-business-rename-and-engine-zoom.json`。

当时 Safari 已显示新提示和真实曲线，但截图可见区域不足，原生拖动仍未验；真实身份全链与 Desktop 认证 T2 单列。

#### Safari 原生拖动复验的独立进展（2026-10-30）

后续按用户要求复验同一 Business 部署的趋势缩放。在原生 Safari 进入全屏并激活本会话第二个标签页后，已能实际看到完整曲线及底部滑块；页面显示新名称、正在采集及十二项指标。通过真实刷新间隔下拉框选择关闭自动刷新，URL 保留同一部署并增加 `refresh=off`。随后原生拖动左侧缩放手柄，截图可见选中区域从完整时段收窄至约右侧一半：查询评估窗口仍为 16:17:48—16:22:48，曲线可见横轴变为 16:20:18—16:22:48，提交速率曲线随之放大。此项计为个人 Safari 真实拖动通过；不由截图推定请求数量或全部样本性质。

准备手动刷新时，最新可访问状态已切换到第一标签页的 `/monitor/resource-overview`，并退出全屏；该次刷新不属于 Business 引擎页，明确排除，不能证明引擎视口保持。已暂停输入并请求用户确认临时独占本会话标签页，手动／自动刷新保持及指标切换重置仍未取得本轮原生证据。摘要 `/tmp/addp-engine-zoom-safari-receipt.json`，原生截图与操作记录在本会话；本次只补文档证据，`git diff --check` 为最小检查，没有修改产品、服务、身份或采集配置，不重跑 CI／Hosted、不设置定时任务。前轮受控浏览器通过与完整回归五项超时仍分别保留，不由本次个人拖动覆盖。

#### Safari 刷新保持与指标切换重置复验（2026-10-30）

用户随后明确允许本会话短暂独占 Safari。只切换第二个标签页，恢复同一 Business 部署的 `refresh=off` 地址；输入前读取原生可访问状态核对 Console 地址、部署 UUID 和标题。首次完整窗口 17:31:18—17:36:18，实际拖动左手柄后，截图可见横轴变为 17:33:48—17:36:18，滑块约右半段。首次手动刷新只观测到旧值清空、新结果未完成，未计通过；恢复十五秒自动刷新后数据重新可见，Monitor 就绪接口为 200，但不据此确定前次未完成的根因。

自动刷新实际观测到结束时间从 17:43:55 推进到 17:44:25，期间没有手动刷新输入；后一次完整查询窗口为 17:39:25—17:44:25，截图可见横轴 17:41:55—17:44:25，右半段视口保留。随后在已核对的 Business 页真实点击“刷新”，加载完成时完整查询窗口为 17:41:06—17:46:06，截图可见横轴为 17:43:36—17:46:06，滑块仍约右半段。以上分别证明个人原生 Safari 的自动、手动刷新后相对视口保持，不将首次未完成或前一轮错误标签页刷新改记为通过。

再通过实际指标下拉框，从每秒事务提交切换到数据库总容量；Console URL 更新为同一部署及 `metric=postgresql.database.total_bytes`，截图实际看到容量曲线约 78.3 GiB，完整查询与可见横轴均为 17:44:10—17:49:10，滑块恢复全范围，证明指标切换重置。页面最终保留默认十五秒刷新及该容量指标，第一标签页未改动。摘要 `/tmp/addp-engine-zoom-safari-receipt.json` 已逐项更新，截图与实际操作保留于本会话；本轮拖动、手动／自动刷新保持、指标切换重置的个人 Safari 缺口关闭。未验其他时段、部署切换、撤权、长期或 Hosted，不外推完整 CI 或旧 T2；仅补证据及能力状态，`git diff --check` 通过，不需重跑产品门禁，未启停个人服务、修改权限或设置定时任务。

### 引擎监控原位刷新（2026-10-30）

十五秒刷新闪动源于请求前清空当前列表、读数及趋势并挂整块遮罩。同上下文在途保留，成功原位更新；部署／指标／时段／列表范围／身份变化和权限失效清空，隐藏取消保持。趋势单独失败仅清图。

五项 T3 阻塞手动／自动响应，验证读数、分页和同一 canvas 保持；撤权取消并清空，迟到响应不复活。旧详情等待中容量变“—”的反例失败；修后定向 25 项通过。

完整 Monitor 前端 131 单元、521 浏览器及构建通过，隔离 runtime／输出已清理。真实 Safari 当时仍隐藏；普通 Console／直连转换仍返回旧代码，同路径附诊断参数才返回新代码，缓存根因未知。后端、来源 T2、Hosted 未覆盖。原件 `/tmp/addp-engine-inplace-*`。

#### 用户重启后的个人 Safari 复验（2026-10-30）

用户执行标准 Monitor 重启并将 Safari 保持前台后，Console 普通组件地址（无诊断参数）实际返回新 dataContext 原位更新逻辑和 aria-busy，旧普通转换结果不再阻碍验证；源码响应保留于 `/tmp/addp-engine-inplace-after-restart.vue.js`。本轮只使用原生 Safari 第二个 Business 标签页、既有已登录平台系统管理员；地址为同一部署和数据库总容量，默认十五秒自动刷新，没有操作第一标签页、修改登记或采集配置。

真实页面显示正在采集、最大连接数 200、容量 78.3 GiB，卡片及实际容量曲线完整可见。原生拖动滑块收窄至右半段；真实点击“刷新”后的查询窗口为 19:59:31—20:04:31、可见横轴 20:02:01—20:04:31，读数与曲线保留且视口未重置。此后没有手动刷新输入，自动更新窗口从 20:00:03—20:05:03 推进到 20:00:33—20:05:33；后一次截图的可见横轴为 20:03:03—20:05:33，滑块仍在右半段，卡片和容量曲线保持可见。由此补齐新版本在个人 Safari 的手动／自动刷新结果与视口保持；未捕获原生请求在途帧，不以离散截图声称逐帧零闪、画布身份或长时间连续性，这些在途保留／同一 canvas 的断言仍由前述阻塞响应 T3 独立证明。

操作与截图保留于本会话，摘要 `/tmp/addp-engine-inplace-safari-receipt.json`。本轮仅补文档证据与能力状态，最小验证 `git diff --check` 通过，产品代码未变，复用原 131 项 T1／521 项 T3及构建结果，不重跑有效门禁。不代表列表的个人 Safari 专项、其他时段／引擎、旧来源 T2 或新 CI／Hosted 通过；CI 与 Hosted 仍等专门会话回传，未设置定时任务，未由 AI 启停个人服务。

### 引擎监控就近提示（2026-10-31）

按用户“文字写在需要的地方”要求，先更新运行观测规范6.3，再复用唯一 FieldHelp：列表四项摘要的说明放在列名旁，详情十二项说明放在卡片标题旁，当前趋势含义／缩放说明和当前数据库容器／最长七天范围分别放在指标与时间选择器旁。删除对应常驻重复段落；实际数值、单位、查询窗口、当前缺样、采集失败及权限错误仍直接显示。复用既有中英文文案，不新增翻译键、请求、权限、统计口径或共享提示实现，不修改来源及刷新生命周期。

131 单元和构建通过。首轮 27 浏览器中 3 项失败，两项仍要求容器范围常驻，一项读到未退出的旧 tooltip；修正聚焦和退出等待后定向 4 项通过，另有原 23 项有效，未形成同次 27 全绿。Console 3 项覆盖 iframe 中英切换、URL／请求不变和 620px 布局。原件 `/tmp/addp-engine-tips-*.log`。

受控 T3 验证键盘聚焦、七天当前容器范围、提示同步、失败清空且错误仍可见。完整前端门禁、后端和来源 T2 未运行。

Safari 当时在登录页，未验证真实提示；后续真实登录见下一节。

#### 用户登录后的个人 Safari 就近提示复验（2026-10-31）

用户在第二个标签页完成平台系统管理员密码／MFA登录后，本会话仅操作该原生Safari标签页，经实际菜单进入引擎监控，再打开 Business PostgreSQL（`deployment_id=9e4fd2b0-3b45-473e-b070-cc6f92e40e11`）。列表真实显示 Infra／Business 两个部署，表头问号已出现；详情十二张卡片的说明改为标题旁问号，当前缺样及顶部“采集来源不可用”直接展示。初次原生点击未观察到说明弹出，不计鼠标悬停／点击通过；随后使用Safari键盘 Option＋Tab，实际焦点和截图分别确认：趋势指标旁显示提交速率／采集事务不等同业务吞吐及缩放仅改变可见范围；时间范围旁显示当前数据库容器运行段／最长七天／断点；容量标题旁显示全部非模板数据库合计、不按租户归算。切换焦点后前一提示消失，各提示在对应控件旁出现；中文内容完整、换行可读，未铺开为常驻段落。实际原生截图与可访问状态保留在本会话。

本轮页面查询窗口实际推进至08:20:00—08:25:00、15秒评估间隔，但Infra／Business均显示“采集来源不可用”，详情值为“—”，因此只认定本次个人Safari中文键盘tip与缺样展示通过；不计真实专业指标采集／趋势数据恢复，也未验证本轮鼠标hover、中英文切换、全部十二个提示或长期刷新。第一标签页未操作，退出时提示已关闭、浏览器边栏恢复原显示、仍保留第二个Business详情页及默认十五秒刷新。下一步优先只读定位两库来源不可用，不未经用户操作应用部署或重启服务。

本轮只追加证据并更新能力文档，产品／测试输入没有变化，复用提交`5cb1a9980`的本地分层证据；最小检查`git diff --check`通过。不改数据库、IAM或采集配置，不重跑有效测试，不查询、调度CI／Hosted或设置定时任务。此前登录待验和本轮登录后结果分别保留，不倒改首次未验记录。

#### Console 语言切换与窄屏提示的退出同步（2026-10-31）

专门CI会话返回同前端源码的后继Console完整272项浏览器及构建通过，同时保留首轮主机tip宽度失败。本轮定向测量已将首失败定位到尚未退出的桌面语言菜单，非引擎／主机内容布局；引擎同流程测试在缩窄窗口前同样等待语言弹层display:none，原iframe／无额外查询／620px宽度检查不变。最终主机与引擎两项标准定向场景通过，产品未修改；原始失败、几何测量、固定Linux成功版本与本地修复证据统一见[主机证据](Monitor节点与主机资源历史证据.md#console-窄屏-tip-回归的语言弹层退出同步2026-10-31)。不外推来源恢复、当前完整CI或Hosted，继续等专门会话回传，不设定时任务。

#### 菜单退出同步修复的 Console CI 结果（2026-10-31）

固定修复提交`4a6a7a4fa`的真实Linux Console Job现已由专门会话回传并核对原始日志：`make test-console-frontend`的222项单元测试、272项浏览器及构建通过，selected=true／整个Job success。完整来源、摘要与原失败分层见[主机证据](Monitor节点与主机资源历史证据.md#菜单退出同步修复的-linux-console-门禁回传2026-10-31)。本轮仅补文档，最小检查`git diff --check`通过，不重复有效验收，不计整个Platform、Hosted或个人两库来源恢复，仍优先只读排查采集来源不可用。

### 引擎当前对象变化的原位刷新保护（2026-11-02）

Redis正式入口提交后继续开发，发现同上下文保留未覆盖已重新读取的System对象事实：列表／详情先更新部署，但旧指标与趋势要等后续观测返回才替换。受控T3在指标响应暂缓期间，真实复现“当前无有效主机关联”与旧“正在采集／1.00 GiB”同时显示。先沿原6.3失效规则明确System对象版本边界，再在唯一引擎页核对部署ID、类型、名称、版本、纳管状态、当前node_id及绑定状态；变化立即清空旧即时值／趋势及缩放。相同事实的新对象引用不触发清空，保留原手动／自动刷新视窗和canvas，不增加接口、权限、指标或查询。

新增PG／Redis×列表／详情×停用／解绑／版本变化12项浏览器场景；修复前12项均在旧内容断言失败（`/tmp/addp-engine-owner-refresh-red.log`），修复后同一标准 `npm run test:e2e -- e2e/engine-resources.spec.js e2e/redis-resources.spec.js` 共46项T3通过（1.7分钟，`/tmp/addp-engine-owner-refresh-green.log`）。同时复验旧单位／零值、tip、故障撤权、跨身份、两语种导航、主机关联、Redis分组及超过100％、缩放与原位刷新。132项前端T1通过（`/tmp/addp-engine-owner-refresh-unit.log`）；两页最终构建通过（`/tmp/addp-owner-refresh-build-final.log`）。Console标准 `npm run test:e2e -- e2e/engine-resource-navigation.spec.js` 4项集成T3通过（39.7秒，`/tmp/addp-owner-refresh-console.log`），包含Redis iframe及PG双语筛选／提示。上述是受控后端T3及构建，不是个人Safari、来源T2或正式身份T4。

同类主机详情保护与独立页面验证见[节点专题](Monitor节点与主机资源历史证据.md#主机详情当前对象变化的原位刷新保护2026-11-02)。CI／Hosted继续等待专门会话回传，未查询、触发、重跑、设置定时或接管个人服务。

### 来源分区应用后的 Safari 真实值恢复与人工导航复验（2026-11-03）

用户应用System／Monitor重启及解锁后，Safari真实页面已出现Infra／Business来源页签。初次工具读取与用户截图均显示“页面隐藏，自动刷新已暂停”；Web Inspector只读表达式同时取得顶层与iframe的hidden，不能将指标空值当作来源失败。并排窗口后仍有hidden记录，不能认定遮挡就是本机历史成因。原生自动化输入后也再次出现hidden，本轮未修改产品代码、可见性或授权规则，不通过脚本强制visible，不把稍后恢复认定为历史根因已修复。

用户随后确认恢复；原生AX实际读到“自动刷新已开启”，Infra分区仅MinIO／PostgreSQL两张卡片，均“正在采集”：MinIO总容量1.87TiB、剩余866GiB、空间占用54.73%、S3请求0.07次/秒；Infra PostgreSQL连接55、活跃0、提交151.83次/秒、容量6.79GiB。Business分区仅Business PostgreSQL一张卡片，关联原Docker Desktop Linux VM，连接1、活跃0、容量78.3GiB，连续只读观察提交速率从0.58变为0.60次/秒；后一原生截图实际显示该数值与“自动刷新已开启”。这证明当次来源分区、真实采集数值及界面更新，不扩大为长时自动刷新／DOM保持或所有专业趋势已验收。AX与截图保留于本会话，不读取Token或业务数据。

用户按本会话步骤手动验证Infra搜索MinIO不按Enter即自动只剩一张卡片，进入监控详情再返回仍保留MinIO，并明确回复“自动筛选正常，返回后仍保留MinIO”。此项为人工实操确认；工具随后读取时页面已切至Business，未声称独立读到MinIO返回URL。完整Console／Monitor受控回归沿`250bcf176`原有效证据，不把人工确认改写成自动化回执。本轮只补运行证据，`git diff --check`通过；无新CI／Hosted、容量、定时任务、配置更改或个人服务启停。租户关联及租户级监控仍仅讨论，未实施。

<a id="source-lifecycle-history"></a>

## 统一采集、主机恢复与部署分组

本地标准夹具、固定 Linux Job、个人 Desktop 和启动摘要分层认定。各次首败、零残留或清理缺口按各自运行保留。

### 统一采集管理入口首版（2026-11-03）

首版统一状态、输入核验和应用／停用，覆盖主机与 Infra／Business 的 PostgreSQL、Redis、MinIO 七项。各域仍独立取配置与凭据。首版无账号修改的 recover 仅覆盖 PostgreSQL。现行分组和恢复以[运行观测规范](../../addp平台运行观测规范.md#采集来源统一部署分组2026-11-04)为准。

本地统一入口 15 项、配置 136 项、登记 78 项及 9 项 runner 通过。Business 配置和完整开发生命周期也通过，包含 206 项部署／Worker／资源输入、44 项 HTTP、169 项 Trace、145 项生命周期与预算。首次独立配置的 10／5 秒停止夹具超时根因未定，原预算下复验通过。生命周期曾因副本漏复制新入口及旧 Redis 输出断言失败，修复后核对 Redis 已停用、三项零失败及实际删除标记。日志可查 `/tmp/addp-metrics-sources-config-recheck.log`、`/tmp/addp-metrics-sources-dev-verified.log` 和精简前的固定记录。

本机只读 status／check 七项失败 0，四项启用且运行，三项停用且未部署。收据 `/tmp/addp-metrics-sources-local-status-final.txt`、`/tmp/addp-metrics-sources-local-check-final.txt`。个人实际应用、新安装、mTLS、Prometheus 采样、页面、CI 与 Hosted 当时均未验。

### Redis／MinIO 已安装来源的无账号修改恢复（2026-11-04）

本批增加已安装 Redis／MinIO 恢复。恢复核对原来源归属、数据库容器、账号、镜像、挂载和私有输入。健康来源保持，缺失或停止的来源按原安装恢复，停用、漂移和换数据库容器拒绝。完整／局部／Hosted 守卫及当前恢复范围见[已安装引擎采集来源的统一恢复](../../addp平台运行观测规范.md#已安装引擎采集来源的统一恢复)。

本地共享 5、编排 15、启动守卫 2、Redis T1 14、MinIO T1 24、登记 78 与 runner 9 项通过。两域 Redis／MinIO 独占 T2 验证健康 ID、原账号与凭据不变、停机恢复、同容器重新绑定、账号停用拒绝、mTLS 与精确发现。MinIO 保卷换容器后 recover 拒绝，显式 up 重新应用。Redis 15、MinIO 46 项 PromQL 通过，临时容器／网络／卷／helper 及文件零残留。标准门禁日志 `/tmp/addp-metrics-recovery-{redis-verified,minio-verified}.log`，清理回执 `/tmp/addp-metrics-recovery-cleanup-verified.json`。

首轮 MinIO T2 因 Compose start 不支持 --wait 失败，临时资源已清理。改为单一受限 up 后复验通过。完整开发门禁首轮 4 项 Worker 失败、1 项停止超时。串行 24／206 及 HTTP 44／Trace 169 分项通过，随后 Security Online v34 登记阻断，后段 145 项未执行，整门禁仍失败。失败日志 `/tmp/addp-metrics-recovery-dev.log` 与 `/tmp/addp-metrics-recovery-dev-final.log`。个人跨 Docker 重启恢复、正式身份页面、CI 和 Hosted 当时未验。

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

`make test-infra-runtime-log-lifecycle`退出0（HTTP44、Trace175及生命周期145项等）；`make test-t2-ci-registration`退出0（登记78、runner9项及实际门禁登记检查）。日志`/tmp/addp-node-recovery-diagnosis-lifecycle.log`、`/tmp/addp-node-recovery-diagnosis-registration.log`。未知／退役原因码的原始输出不透传且清理提示保留由T1验证；T2是受限桥接来源，不外推为个人故障恢复、原生Linux实机或Hosted验收。本轮未启停个人来源、改配置／账号／数据库、操作Safari或查询远程CI／Hosted，未设置定时任务。

### 引擎来源失败阶段的中文提示（2026-11-04）

PostgreSQL已有十二项固定失败阶段，统一入口此前直接显示英文码；Redis、MinIO除镜像原因外未发布阶段。本批先修规范与操作指南，将十八项固定阶段映射为中文步骤及核对建议，在两类引擎原有部署、私有状态、账号处理和来源操作边界补标记。共享来源内核负责在原异常对象上记录阶段，内层阶段优先，不覆盖主要原因或清理标记；未知阶段不输出，已知具体原因仍优先于阶段。提示只定位步骤，不把它当成根因、操作未生效或已回滚的证明，不建议自动重试。未新增读取、权限、配置或恢复路径，原命令／期限／清理范围保持；去除诊断装饰器后，三个部署实现文件中66个既有操作函数的AST与提交前相同（排除输出字段改变的main）。

本地标准门禁：`make test-monitor-metrics-config test-infra-runtime-log-lifecycle`退出0，覆盖共享恢复／诊断10项、主机15项、统一编排19项、启动守卫2项、配置136项、HTTP44／Trace175及生命周期145项。新提示断言先在旧实现失败，最终覆盖十八类阶段、未知阶段不透传、嵌套原异常与超时／清理字段保留、实际来源操作边界和Redis／MinIO命令行封闭输出。`make test-t2-ci-registration`退出0（登记78项、runner9项及实际门禁登记）；现有两类来源T2输入清单已包含本批实现与测试，无需改workflow或新增入口。日志`/tmp/addp-engine-stage-config-lifecycle.log`、`/tmp/addp-engine-stage-registration.log`。

`make test-monitor-redis-source`退出0（含Redis生命周期15项T1、Go固定协议T1），双域真实ACL／固定快照、十五项PromQL、只读复用／停止恢复／同一数据库容器重启后的来源重建、mTLS、发现、持久化及越权拒绝通过；自有来源和配置卷、私有文件清零。`make test-monitor-minio-observation`退出0（含MinIO生命周期25项T1及Go协议T1），双域仅指标授权、S3／管理拒绝、过期／停用、只读复用恢复、mTLS、固定投影及失败清理通过；独立作用域和容器替换后的账号持久化、四十六项PromQL通过；外层容器／网络／卷／私有状态清零。日志`/tmp/addp-engine-stage-redis-t2.log`、`/tmp/addp-engine-stage-minio-t2.log`。实源T2证明诊断加入后的既有生命周期回归；全部失败阶段的注入及中文展示仍是T1，不声称每个阶段均在实源制造过故障。

本批未修改个人来源、账号、数据库、Docker设置或启停个人服务，未操作Safari；PostgreSQL未重跑实库安装或T2，复用其未改的原阶段，仅经本地生命周期与提示T1验证。远端CI和Hosted等专门会话回传，未手动调度或轮询验收，未设置定时任务。下一步优先补主机Safari连续前台刷新及原生缩放保持的剩余真实交互证据。

### 采集来源统一 Compose 分组（2026-11-05）

采集来源统一到工作区摘要隔离的 `addp-metrics-sources-*`。PG Exporter／mTLS 仍分容器，引擎按 scope 分服务，主机独立；各域账号、私有卷、证书、原网络、资源限额和唯一私有声明不变。显式 up 才核验并退役旧布局；recover 不迁移、构建或重装账号。

逐服务精确查询再去重，避免 Docker 多服务标签交集误报空集。删除主机项目级 down／remove-orphans，清除继承 Compose 变量；保持中心、数据库、业务卷、历史、公开 API 和权限边界。

| 层级 | 实际结果 |
| --- | --- |
| 配置与登记 | 来源 53 项、配置 137、发现 45＋6、登记 80＋9、GDAL 原生 24 通过。发现首轮在 marker 尚空时读 PID 失败，原样复验通过。 |
| Online runner | 本批准备／清理 56 项通过，证书配置及 SKI／AKI 显式。完整入口 168／172，四个重复回调语法负例先报共享登记导入错误，未到 SyntaxError 或 System Go。原件 `/tmp/addp-regroup-online3.log`；后继实际 Job 见[固定版本结果](#统一分组的固定版本实源结果与语法入口阻断2026-11-05)。 |
| 主机／Redis／MinIO 实源 | 三个标准门禁通过。主机为独占桥接恢复；双域 Redis 的 ACL／INFO／mTLS／发现／恢复通过；双域 MinIO 同项目的对等来源保持、46 固定查询及权限通过。各夹具退出清零，未覆盖个人宿主迁移。 |
| PostgreSQL 实源 | 整项退出 2。原生双域安装、mTLS、停止及同容器重启恢复、账号／HBA 不变通过，后续目录原子替换可见性前置失败，HBA 正向未到达。首轮另因本机缺 OpenSSL 配置在 PKI 准备失败，显式化夹具证书后进入实源。 |

本批未覆盖个人应用、Safari／Desktop、Linux 目录认证或 Hosted；用户后续实际应用及采样证据见[Desktop 统一分组复验](#个人-desktop-统一分组应用与恢复复验2026-11-05)。原件位于 `/tmp/addp-regroup-*.log`。

### 统一分组后的标准生命周期夹具修复（2026-11-05）

固定 `3d0ccfcce` 的 Platform [首次失败](https://github.com/pampa0629/addp/actions/runs/41867352427/job/126704897315)（Run `41867352427`／Job `126704897315`）。六项 disabled、metrics-only 和父环境隔离预期 0 实得 1。日志 `/tmp/addp-ci-20261105-010000/job-126704897315.log`。

旧假 Docker 只识别旧项目的空列表。新逐服务查询进入依赖 MOCK_COMMANDS 的核心分支，部署传输按既定规则清除父环境，子进程因此报 unbound variable。

夹具复用真实命名内核，只接收精确当次项目、Infra 服务和七参数只读列表；拒绝未知／外来服务、项目和额外参数。旧反例失败，修正后六项原断言通过，保留隔离与核心／数据卷边界。

最终生命周期标准入口通过（来源 53、HTTP 46、Trace／登记 183、生命周期组合 149 及 Go），配置 137、登记 80＋9 通过。

首次本地复现另有 HTTP 预算原五秒编译夹具超时，后续完整入口通过，原失败保留。日志 `/tmp/addp-regroup-lifecycle-{repro,final,registration}-20261105.log`。

本项只验确定性夹具，个人迁移、实源 T2 与 Hosted 未覆盖。

### 统一分组的独立仓库闭包与网络声明修复（2026-11-05）

`7d8ad061e` 的两项独立首败。Platform [Run41877661034／Job126741448659](https://github.com/pampa0629/addp/actions/runs/41877661034/job/126741448659) 在 208 项首组和 149 项组合通过后，独立仓库停用缺 `metrics-project.py`。Metrics [Run41877661118／Job126748083306](https://github.com/pampa0629/addp/actions/runs/41877661118/job/126748083306) 在原生查询、压力及 exporter 恢复后，因默认网络仍有其他端点而无法切换 Desktop overlay。原件 `/tmp/addp-ci-20261105-020000/`。

独立仓库复制清单与相关 T2 输入补齐共享命名模块；假 Docker 用真实命名核验当次工作区和精确 Infra 服务，保留停用、外来拒绝与禁止删卷断言。

独占基底创建时声明相同网络 Owner，保留生产标签；配置反例确认 base／overlay 网络一致，实源增加前后 Owner／网络 ID 检查。挂载诊断仅新增封闭布尔值，shared 传播仍拒绝。

- **开发门禁**。旧入口复现 FileNotFoundError；修正后两个 shell 子项通过。完整入口两轮均在 208 项阶段分别因 Transfer、Meta Worker 启动超时，未到修复点，仍失败。
- **实源门禁**。旧本机复现 active-endpoints。修正后依次首败为 budget recovery 超时、`limited_source_bind_mismatch`、sample overflow 超时，均未到新增网络断言；四个独占项目按归属复核容器／网络／卷为零。
- **配置与证据**。配置 138、登记 80＋9 通过。原件 `/tmp/addp-ci-closure-*-20261105.log`、`/tmp/addp-ci-network-*-20261105.log`；清理 `/tmp/addp-ci-network-all-owned-cleanup-20261105.json`。

本机新增网络断言与完整开发门禁仍未闭环。后继[固定 Linux 结果](#统一分组的固定版本实源结果与语法入口阻断2026-11-05)按实际 Job 认定；个人来源迁移及 Hosted 另计。

### 统一分组的固定版本实源结果与语法入口阻断（2026-11-05）

固定 `f9a91928eaedffbe8a1627bd238b43bddfbb000f` 的 [Release Run41889180935／Job126782168039](https://github.com/pampa0629/addp/actions/runs/41889180935/job/126782168039)（attempt 1）实际 host-resources 生命周期成功。网络声明／Owner／身份、limited mount、资源查询、六来源、告警及当次容器／网络／卷清零均实证。该 Linux T2 覆盖新增网络断言，前节本机失败保留；个人 Desktop、整 Platform 与 Hosted 不在范围。原件 `/tmp/addp-ci-20261105-030000/`。

同源码 [Platform Run41889180973／Job126781670842](https://github.com/pampa0629/addp/actions/runs/41889180973/job/126781670842) 失败。四项重复声明负例的临时仓库被 Make 共享登记依赖先行阻断，报 ModuleNotFoundError，未进入 Node 语法检查。原日志 `job-126781670842.log`。

用同一四变体比较已提交／共享候选 Makefile，分别串行及并行。旧版 8 项均先报导入失败；候选 8 项均先由 Node 报重复声明 SyntaxError。收据 `/tmp/addp-syntax-order-proof-20261105/proof.json`。

共享候选的完整本地 runner 通过登记 144、监控 172 和身份 Go；负责人提交 `9b97d1b83` 后，已核对 Makefile 摘要并以已提交版重跑原四变体通过。该本地证据与后继自然 Job 分列。

固定 `338bfb3780a57b0baa1cb2a8af9779007db68d56` 的 [Platform Run41897329087／Job126810251735](https://github.com/pampa0629/addp/actions/runs/41897329087/job/126810251735)（attempt 1）实际 Platform 步骤通过，登记 144、监控 172 和身份 Go 均执行。原四变体 AST 与 f9 一致；旧失败保留。原件 `/tmp/addp-ci-20261105-040000/`。

### 个人 Desktop 统一分组应用与恢复复验（2026-11-05）

用户已亲自执行统一入口的三组显式应用：`up --scope host --kind node`、`up --scope infra --scope business --kind postgresql`、`up --scope infra --kind minio`，并确认成功。本会话随后只读检查，四项已启用来源均运行于唯一 `addp-metrics-sources-7bb4e5ee77e15388` 项目，共六个容器；四个旧项目残留容器为0。`python3 scripts/infra/metrics-sources.py status` 七项零失败，Redis及Business MinIO仍停用／未部署。应用前已核对旧容器归属；Infra MinIO旧镜像确实缺失，故先前普通recover不能恢复，显式up后来源已恢复。不据此声称数据库内容、账号权限或来源故障注入场景全部重新验收。

沿当前Monitor查询证书只读访问Prometheus，`up{job="addp_nodes"}`九个目标全部为1（主机1、引擎3、进程5）；另查 `timestamp(up{job="addp_nodes"})` 核对真实抓取时间，九项距核验均小于15秒，未以即时查询的评估时间代替采样时间。Safari真实页面确认Business PostgreSQL详情及趋势存在、Infra PostgreSQL与MinIO卡片显示正在采集及有效数值；主机CPU、内存、磁盘IO有效，查询时间与采样时间自然推进，截图可见真实页面与自动刷新开启。当前Desktop视图的文件系统、网卡、TCP仍明确不可用，不作为已恢复或零值。此复验不含停用／恢复故障注入、缩放重新拖动、Hosted或完整租户功能。

只读收据 `/tmp/addp-personal-metrics-preapply-20261105.json`、`/tmp/addp-personal-metrics-postapply-20261105.json` 保存前后容器分组、归属摘要及抓取时效。源码为当次main `052104faf`（包含分组与共享语法修复），本次仅补证据；未自行执行来源up/down/recover、重启数据库或ADDP、修改配置或Docker设置，也未轮询CI或设置定时。

### 全量开发启动的主机恢复与监控摘要（2026-11-05）

用户确认后，先修订运行观测规范与操作指南，再扩展原全量本地启动：统一恢复显式选择host/node及原两域引擎，沿既有主机安装证明和来源拒绝边界执行，不新增安装、改账号、迁移或引擎启停。正常来源不动，局部和Hosted启动不处理。来源恢复失败保留可选观测非零结果但继续业务启动，末尾复用Infra归属／健康检查和唯一来源status输出摘要，服务运行与指标采样明确区分；先前失败不会被摘要成功清除。没有新增HTTP查询、凭据、API或另一套指标有效性判定。

来源 55 项、配置 138、登记 80＋9、开发生命周期 24＋209 及 shell 实例、Infra 组合 149 均通过。启动组合覆盖全量／局部／Hosted、先前失败保持和末尾摘要。新断言的实现前失败保留。原件 `/tmp/addp-startup-monitor-*-20261105.log`。

只读摘要显示中心／控制入口运行、七类来源失败 0，仍需 Monitor 核验采样。个人全量启动的自动恢复当时未实际复验，后来回执见[主机恢复与启动摘要](#主机恢复的空dns表示差异误拒绝2026-11-05)。

### 启动前序失败与末尾现况的提示归因（2026-11-05）

用户执行后回传：末尾中心／控制入口运行、七类来源零失败，但仍有“可选观测检查未完全通过”。代码确认该提示沿启动进程内累计标志触发，可能来自日志／指标配置预检、Infra设施、日志端口或采集恢复；原摘要未保存阶段名称，无法由末尾零失败反推早期原因。本会话只读核对当前日志预检、核心／完整Infra健康均通过，指标配置在截住唯一原子发布边界、只读渲染的条件下校验通过；**没有执行文件写入、来源恢复、启停或重启，也未证明当次早期失败已解决**。已请求用户提供前段基础设施与来源恢复输出，具体原始根因尚未确认。收据 `/tmp/addp-startup-warning-current-readonly-20261105.log`、`/tmp/addp-startup-warning-readonly-render-20261105.json`。

修复限定为归因：保留原预检短路顺序和继续启动／非零结果，以本次进程内的固定阶段名称记录各失败，末尾分别列出前序失败和摘要自身失败，提示历史阶段结果不等同于当前仍采集失败。当前正常不清除原失败，阶段名称不含原始错误、地址或凭据，不持久化或自动重试。规范先修订；无新增概念、权限、API、依赖或CI入口，既有Platform／Metrics输入登记覆盖修改脚本与原测试。

新阶段断言先失败；实现后并行轮原五秒假命令夹具超时，完整开发门禁也在原 GDAL 并发准备 30 秒处失败，未进入后续。串行来源 55 项及定向开发 shell 通过，聚合失败保留。原件 `/tmp/addp-startup-warning-*-20261105.log`。

### 主机恢复的空DNS表示差异误拒绝（2026-11-05）

用户补充原输出确认：Step1核心健康，主机恢复报node-installation-changed，三个已启用引擎来源均健康未重建。只读比较原私有安装证明与当前Docker快照，输入完整相同，容器／镜像／挂载／网络及其他HostConfig无差异；仅node-exporter的HostConfig.Dns、DnsOptions、DnsSearch从证明中的null变为当前空数组。Docker服务端28.5.1；其[Moby网络配置](https://github.com/moby/moby/blob/v28.5.1/daemon/container_operations.go#L63-L76)仅在对应列表长度大于0时使用容器覆盖，因此此次为事实比较误拒绝，未发现真实DNS或安全配置变更，亦未证明Docker何时转换表示。原只读差异收据 `/tmp/addp-node-recovery-proof-diff-20261105.json`。

规范先明确唯一规范化比较，再仅对三个已存在字段的null转空数组；快照、证明内存比较及原清理身份比较沿同一路径，不修改私有证明、不采用未知身份。非空值、字段缺失、非法类型、CapAdd等其他字段及容器／输入／卷变化继续拒绝。原证明无须重新应用，原schema／字段不增加兼容分支。

新增反例先沿 `make test-node-metrics-recovery` 失败（`/tmp/addp-node-dns-proof-red-20261105.log`），保留双向空表示、逐字段和三字段共同变化／证明inode与mtime及内容不变断言，以及非空／缺失／非法类型／非DNS空字段变化拒绝。修复后 `make test-metrics-sources test-t2-ci-registration` 通过57项来源及80项登记／9项runner和实际登记检查，日志 `/tmp/addp-node-dns-proof-final-20261105.log`。`make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=node-recovery` 独占实源T2退出0，验证健康不动、采样器单独恢复／全部恢复、丢失回执清理、mTLS及原漂移／缺源／停用证明拒绝，容器／网络／卷和私有状态均清零，日志 `/tmp/addp-node-dns-proof-t2-20261105.log`。

个人环境先进行只读比较。修复后规范化安装事实完全一致，来源正在运行，证明内容、inode 和 mtime 均未变。收据见 `/tmp/addp-node-dns-proof-readonly-fixed-20261105.json`。

修复 `ca6c675ad` 推送后，用户在本机执行 `python3 scripts/infra/metrics-sources.py recover --scope host --kind node` 并确认成功。

用户随后回传本机全量启动的末尾只读摘要。指标中心和采集控制入口均运行中，主机、Infra PostgreSQL、Infra MinIO 和 Business PostgreSQL 均启用并运行中，其余三项停用且未部署。摘要共 7 项、失败 0 项，所贴输出未出现前序观测失败提示。此次复验覆盖本机全量启动的监控摘要，实际采样、主机关联和有效指标仍需在 Monitor 核对。CI／Hosted 结果由专门会话回传。

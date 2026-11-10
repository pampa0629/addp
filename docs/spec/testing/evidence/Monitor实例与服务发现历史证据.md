# Monitor 实例与服务发现历史证据

[返回 Monitor 历史证据](Monitor历史证据.md) · [返回验收证据索引](../验收证据索引.md)

本专题收录多 Worker、实例筛选、cgroup 配额、部署预期与发现生命周期。验收结论按各记录的固定源码、环境、运行尝试和层级认定。原顺序见[Monitor 历史证据入口](Monitor历史证据.md#原记录入口)。 容器配额相关记录见[分层导航](#服务实例-cgroup-证据导航)。 部署观察器相关记录见[退出证据导航](#部署观察器退出证据导航)。 服务组相关记录见[部署预期与运行确认导航](#服务组部署预期与运行确认证据导航)。 服务列表相关记录见[筛选与分页恢复导航](#服务筛选与分页恢复证据导航)。 服务趋势相关记录见[交互证据导航](#服务趋势交互证据导航)。

原始过程、逐步诊断及详细归档清单见[精简前的固定记录](https://github.com/pampa0629/addp/blob/be8f8daa4d38a7830585577f03b77bf2b5ed3931/docs/spec/testing/evidence/Monitor%E5%AE%9E%E4%BE%8B%E4%B8%8E%E6%9C%8D%E5%8A%A1%E5%8F%91%E7%8E%B0%E5%8E%86%E5%8F%B2%E8%AF%81%E6%8D%AE.md)。

## 开发 Backend 启动失败与重试的 PostgreSQL T2（2026-11-07）

产品基线 `51b5b26c7c5e88fc8d7e301c29c0dd01b540df56`，隔离检出仅叠加本批测试。标准发布器写入实际部署意图和启动结果，System 通过实际 PostgreSQL 模块登记读取失败。覆盖首次可见、重试清除、拒绝旧尝试迟到结果、本轮第二种失败可见、Backend 成功登记后清除，以及随后注销不会重新显示历史失败。

标准 `make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS='--package repository --test observability-identity --source-root <隔离检出>'` 退出 0，无跳过。首次测试夹具把 Go 默认临时目录直接传入发布器，目录保护预检拒绝。改由正式发布器创建当次私有子目录后复验通过；未放宽保护规则。门禁只使用 `addp_iam_test`，退出后只读确认 `system` 和 `common` Schema 均无残留。

该 T2 使用真实 PostgreSQL 与生产发布、登记代码，通过固定失败原因触发断言。真实构建失败、正式登录和实际 Console 页面由下方的独立运行验收覆盖。

## 开发 Backend 真实构建失败与重试的隔离运行验收（2026-11-07）

固定源码 `26aa994ec405585e9306a76c6d9ca6083d25553a`，在一次性 Lima Linux VM 内使用独立 Docker、数据库和回环端口。以 `bash scripts/infra/up.sh`、`bash scripts/dev/start.sh -monitor` 启动基础设施及控制面，使用正式三员 Bootstrap 和 Chromium 中的真实密码及 MFA 登录，确认当前身份具有平台 `platform.module.read`。

初始刷新时 Standard 没有实例或失败记录。在隔离检出注入一个未定义符号后，`bash scripts/dev/start.sh -standard` 实际编译失败并退出 1。真实 Console 刷新后仍无 Standard 实例，System 返回一条仅含模块、失败时间、`build_failed` 原因的记录，服务监控显示“构建失败”。移除注入后，同一启动命令退出 0。刷新时 System 返回有效在线的 Standard Backend，失败列表为空，Console 实例可见且失败提示消失。验收结束时检出干净。

控制面第一次启动时 Meta Worker 先于 Meta Backend 完成冷启动迁移而退出。迁移完成后，以同一标准入口启动成功。此记录覆盖 Standard 真实构建失败与重试。进程退出及就绪超时由下方的 Catalog 验收覆盖。临时开发服务已停止，专属 VM 及其存储已删除。

## 开发 Backend 进程退出与就绪超时的隔离运行验收（2026-11-07）

沿用固定源码 `26aa994ec405585e9306a76c6d9ca6083d25553a`，在新的一次性 Lima Linux VM 内使用独立 Docker 与数据库。标准 `bash scripts/infra/up.sh` 和 `bash scripts/dev/start.sh -monitor` 成功。正式三员 Bootstrap 完成后，使用真实 Chromium Headless Shell、密码与 MFA 登录，核对平台 `platform.module.read` 权限。首次浏览器启动在解压期间报 `ETXTBSY`，尚未注入故障。安装完成后只重跑浏览器场景。

Catalog 有标准入口的独立就绪探针。基线刷新时没有 Catalog 运行实例或启动失败。仅在隔离检出注入两种临时故障，各执行一次 `bash scripts/dev/start.sh -catalog`，两次均实际编译成功并由启动脚本退出 1。

- 进程在登记前退出。确认进程已消失，接口返回 `process_exited`，服务监控显示「启动进程退出」。
- 进程保持存活且未就绪。标准 60 秒等待耗尽，启动命令连同编译约 66 秒。接口返回更新的 `readiness_timeout`，服务监控显示「等待就绪超时」。

两轮失败均没有运行实例，响应仅含模块、失败时间和固定原因。移除夹具并结束当次故障进程后，标准启动重新编译、退出 0，Catalog 就绪探针成功。再次刷新时 Backend 在线，失败记录及页面提示均清除。终端两种失败均打印「Catalog Backend 启动超时」，未区分提前退出，页面与接口已经区分。隔离检出最终干净，专属 VM 及其存储已删除。此记录仅为本地隔离运行与真实页面证据，生产 Compose、Worker 和 Hosted 不在验收范围。

## 开发 Backend 终端失败原因一致性（2026-11-07）

Go Backend 就绪检查原本正确记录 `process_exited` 或 `readiness_timeout`，调用处仍固定打印「启动超时」。现由同一判定同时记录原因并输出「启动进程退出」或「等待就绪超时」，现有十一处调用统一提供服务名称，日志位置指引保留。Manager 的独立 `--wait-live` 分支仍沿原语义。

`make test-deployment-discovery` 通过 48 项部署测试及 6 项容器证明测试。新增回归用真实临时子进程验证存活和退出两种状态，核对终端文字与私有结果文件一致，修复前两种文字断言均失败。标准脚本入口 `bash scripts/test/dev-lifecycle-and-build.sh` 通过。完整 `make test-dev-lifecycle` 在进入本轮脚本回归前，被栅格容量 owner 的 Online 登记契约拒绝，错误为 `raster capacity owner contract is incomplete: scripts/test/online-engine-registration.py`。该总入口不计通过，本轮未修复其他 owner 的登记。

## 开发 Backend 就绪屏障覆盖（2026-11-07）

核对后补齐 Standard、Monitor、Model、Quality、Security、Asset、Portal、Graph 的就绪等待，以及 Workbench 单独选择时的外层条件和复用 System 时的前置检查。System 与十八个 Backend 使用同一个等待函数，等待各自的 `/health/ready`，失败沿现有原因发布、日志指引和后续启动屏障。原重复就绪循环删除，Manager 的专用存活路径与 Ontology 的单请求 5 秒限制分别沿原契约。

`make test-deployment-discovery` 通过 49 项部署测试及 6 项容器证明测试。新增受控回归从实际 Backend 启动段提取所选模块，覆盖全选成功、逐模块单独未就绪、复用 System 未就绪、Workbench 单独选择、并行失败、Standard 短暂未就绪后成功、全部未选及 Manager 存活路径。修复前出现 13 项子场景断言失败，修复后全部通过，且核对请求次数与保存原因。`bash scripts/test/dev-lifecycle-and-build.sh` 完整通过。

完整 `make test-dev-lifecycle` 在 GDAL 前置用例 `test_native_identity_cache_hit_and_same_version_library_replacement` 和 `test_launch_owns_listener_and_cleans_up_failure` 分别超时，不计为通过。此次为本地受控回归，真实 Linux 启动与 Hosted 继续按各自固定源码验收。

## Standard 就绪实源验证的环境准备失败（2026-11-07）

固定源码 `201a8fcec71fb7ae3632f7beca08d2870de763b6`，在一次性 Lima Linux ARM64／独立 Docker overlay2 中准备 Standard 持续未就绪与恢复场景。Go、Node 和正式 Bootstrap CLI 构建完成，Infra 镜像及浏览器安装先后遇到下载 EOF／502。准备过程中宿主可用空间降至约 1.5 GiB，随后 BuildKit 元数据库写入及浏览器安装均报 `read-only file system`／`EROFS`。取证时 guest 根分区仍有约 7.4 GiB 可用，底层触发原因未确认，不能据此认定为 guest 分区耗尽。

控制面尚未就绪，未执行 Bootstrap、真实页面登录或 Standard 故障注入，所需实源场景不计通过。已只读确认临时故障文件不存在。专属 VM 及其存储已删除，宿主可用空间恢复至约 13 GiB。后续先增加宿主磁盘余量，再核对临时 VM 存储状态；既有受控回归和旧固定源码的实源证据保持各自范围。

## Standard 就绪实源复验的环境中止（2026-11-08）

仍固定源码 `201a8fcec71fb7ae3632f7beca08d2870de763b6`。在新的一次性 Lima Linux ARM64 VM、独立 Docker 默认 containerd snapshotter 中准备同一 Standard 超时与重试场景。正式 Bootstrap CLI 构建、三个前端依赖及 Chromium Headless Shell 安装完成。标准 `bash scripts/infra/up.sh` 的前两次准备分别在 MinIO 服务端与客户端构建遇到 Go 依赖下载 EOF／TLS 超时；确切失败地址恢复 HTTP 200 后继续标准入口，MinIO 镜像构建成功，七项核心容器均健康，入口随后进入 pgvector 扩展安装。上述准备失败保留，不作为产品断言失败或验收通过。

准备期间宿主可用空间从约 22 GiB 快速降至 4.5 GiB，取证时 guest 根分区仍有约 12 GiB。为避免上轮写入失败再次发生，主动停止并删除本次专属 VM，标准入口及等待中的验收编排因 VM 停止退出 255。`limactl delete standard-ready` 成功，专属实例清单为空、实例目录不存在，宿主可用空间恢复至约 18 GiB，宿主 Docker 上下文仍为 `desktop-linux`。未完成 Infra 全入口，未启动控制面、执行 Bootstrap、登录页面或注入 Standard 故障；检出干净，临时故障文件和浏览器凭据均不存在。不能将七个容器健康计为本轮就绪屏障通过，也未证明上轮 EROFS 或本轮宿主空间变化的根因。

本轮宿主日志位于 `/tmp/adpvm-standard-recheck.CqMvJw/`，包含 `infra.log`、`infra-retry.log`、`infra-retry2.log`、`browser-install.log`、`bootstrap-build.log`、`accept-retry.log` 和 `final-preparation-diagnostic.log`。CI／Hosted 未查询或触发，个人服务、Docker Desktop 配置及数据库未修改，未设置定时任务。后续须先确认宿主磁盘余量能稳定支撑独立环境，再沿同一固定源码补实源场景；既有受控回归及旧版本真实页面证据保持原范围。

## 清单接入与实例绑定

主机组合筛选、详情返回及重置已有个人 Safari 验证。System 清单接入 → Monitor 采集保存已通过 [Hosted 专项](https://github.com/pampa0629/addp/actions/runs/39127927703)（受验源码 `fb13955d8f3cc48b254f2c840e55b8f8f638d2cc`，包含 `70c62a09a`、`9263ab6ee`）：单台 Linux VM 创建一次主机，正式 400 拒绝后只重试同一主机采集，刷新、两 Owner 独立读取及准确数量均通过。场景约 63.535 秒，浏览器约 39.370 秒；配置退役通过 API 删除采集并核验 404、停用主机且核验版本递增，随后独占部署销毁与外层零残留通过，不表示产品已提供主机 DELETE。

新进程真实绑定已通过 [Hosted 专项](https://github.com/pampa0629/addp/actions/runs/39169909912)（精确源码 `fee7cb91d8ef674d2beb41036a288519bc1d6be7`）：新 Monitor 首次绑定当前主机，旧实例退出且资源为 `not_active`，System 对等实例保持未绑定；停用主机后同一进程拒绝绑定但资源有效，重新启用后恢复。真实 Console 从当前主机名进入同一 iframe 的资源页，收据包含精确九项有效标量与主机版本 3，后退及刷新恢复筛选。场景约 162.642 秒，绑定浏览器约 12.533 秒；配置退役与外层部署销毁、Infra 零残留分别核验通过。浏览器收据按端点、主机及完整指标集合选择，不能由并发网卡响应替代；原角色、预算和单一路径保持不变。

## 本批证据的覆盖限制

上述为一次性 Linux VM 的单个新 Monitor 进程验证，不代表原生 macOS、多进程、Business 引擎承载、总览多真实主机或生产 T5 已验收。精确源码的 Console 与 Platform consistency CI 已实际执行通过；本地 Console 聚合的 4 项其他场景失败单独保留，不用远端结果覆盖本地失败事实。

## 同模块双 Worker 的实源隔离

2026-10-22，既有 `http-quality` 标准 T2 扩展为同一模块一个 Backend、两个 Worker，仍使用同一受限非 root 来源容器、原 CPU/内存配额、独立 mTLS 及唯一 HTTP SD。三个真实 Common SDK 进程分别具有实例身份、启动时刻和端点；两个 Worker 各自的 CPU/RSS/时长与当前段趋势有效，双向分别交换实例身份、端点或启动时刻时，摘要和趋势均不能借用对方数值。先停止 Backend，再停止其中一个 Worker；两次均核对失败来源的当前值清空，仍运行 Worker 的即时值和趋势继续有效。既有 HTTP 完整窗口、历史故障、真实告警触发、签名通知及恢复断言保留。

冻结基线 `6d42e4621f2bed671814c11eb365fcde4064f301`，只叠加本方测试输入。首次完整 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=http-quality` 已通过新增三进程查询，但被旧 HTTP 夹具的两进程数量限制拒绝，不计整项通过；日志 `/tmp/addp-same-role-workers-t2-initial.log`，失败退出后独立确认项目 `addp-metrics-t2-aa424071344545babdb1d19ee508ad7a` 的容器、网络和卷均为零。同步 HTTP 查询与告警两处夹具约束后，同一标准完整分组退出 0：45 项配置回归、固定窗口检查、三进程与双 Worker 身份拒绝、HTTP 查询/告警/通知/恢复、依次停源与对等进程保持均通过。日志 `/tmp/addp-same-role-workers-t2.log`，项目 `addp-metrics-t2-67bc578dfb73485783ae7aabde6f5d87` 的容器/网络/卷及门禁内部临时目录清零，另读三类资源均为空。五项最终测试输入与成功冻结副本逐字节一致；T2 CI 登记检查通过，既有目录登记与 Release 分组已覆盖，不新增 Make/workflow 路线。

本次是 Docker Desktop 上的 Linux SDK／真实 Prometheus／Monitor 查询 T2，未执行真实 System 多 Worker 登记与宿主绑定、业务 Worker 副本启动或原生 macOS 拓扑 T4，不把局部通过扩大为这些范围。既有 PostgreSQL 专业 Hosted 证据不变；本批云端结果继续等待专门 CI 会话回传，不自行轮询、重跑或设定时，也未重启个人服务。

## Meta 双 Worker 标准启动与登记绑定专项

2026-10-22，按用户确认范围补齐原生 Meta 的唯一编号启动路径：`ADDP_DEV_META_WORKER_REPLICAS` 默认 `1`、仅接受 `1/2`，两个单元共用同一 Worker 产物，各自独立 PID、进程身份和可选指标/TLS；任务并发仍是每进程独立配置。标准启动复用健康单元、只补缺失单元，活动缩容及外来/重复 PID 拒绝；模块停止和局部重启覆盖全部选中 Worker。未改变 Common 登记、System 首次主机声明、Monitor 授权、IAM 或业务任务调度。首次应用需部署方先停止旧 Meta，再使用标准入口启动；个人服务与配置未切换。

同一 `platform-node-metrics` 增加 `meta-workers` Hosted 专项：真实一 Backend／双 Worker 登记和同主机绑定，独立端点及当前段资源/趋势，精确停止一个 Worker 后由标准入口补回且健康实例保持，主机启停、中心故障与新样恢复，正式密码/MFA的真实 Console 双实例趋势及主机导航，最后配置退役和整套部署清零。身份、权限与 1800 秒场景／60 分钟 Job 预算均沿原路线；不执行租户任务、加压或读取租户业务。该专项尚未实际调度执行，不能计为真实 Meta 登记/绑定 T4 通过，也不覆盖原生 macOS、跨主机、容器副本、任务竞争或 HA。

本地验证使用冻结基线 `016f606ee18892a20a0497308f681ca03f69e994`，只叠加本批 24 项实现、规范与测试输入，避免共享工作区其他会话 WIP。最终 `make test-dev-lifecycle` 退出 0：100 项启动/配置测试、59 项 Infra 生命周期回归、完整 Bash 生命周期/局部重启及 Common schema/repository/projectionstore 回归通过，四模块 server/worker 命令包编译通过。新增四项测试使用真实原生 Python 进程夹具执行生产 Worker 启动和精确停止入口，覆盖逐实例指标/主机输入、复用、只补缺失、缩容/重复/外来 PID 拒绝；它们不是正式 Meta Go Worker 登记或页面 T4。日志 `/tmp/addp-meta-workers-dev-lifecycle-receipts.log`。

曾有一轮生命周期提取夹具缺少启动单元初始化，修正后完整门禁通过；随后一次复验及一次带诊断复验在新增夹具的五秒启动收据等待处失败，当时两个进程仍存活。保留期限并补进程/收据/控制器诊断后，最终串行完整门禁通过；未证明这两次等待失败的唯一原因，不将后续通过追溯为已修复未知环境故障。失败日志 `/tmp/addp-meta-workers-dev-lifecycle-verified.log`、`/tmp/addp-meta-workers-dev-lifecycle-final-check.log`。Online 首轮 123 项及身份 Go 夹具通过；一次并行复验有两项既有引擎 CLI 夹具失败，未计通过，后续结果须按下述最终记录判断。

最终串行 `make test-node-metrics-online-runner` 退出 0：123 项 Python 正反向契约、JS/Bash 语法及 System Metrics 身份 Go 夹具通过，日志 `/tmp/addp-meta-workers-online-runner-final-check.log`；包括新 Meta 单元独立证书/计划、限定标准命令、SDK 来源映射、当前实例 DTO、真实浏览器收据拒绝与清理编排。并行轮引擎 CLI 失败的唯一原因仍未确认，仅增加测试阶段输出诊断，未修改引擎产品、认证、读取期限或清理边界。`make test-workflow-security` 退出 0（原标准 Release workflow 固定版本安全审计），实际 T2 和 Online CI 登记检查均通过；新增 choice 已同步原 Online 受控登记及删除 choice 的拒绝回归。24 项最终输入与成功冻结副本逐字节一致，之后仅更新本证据与后续事项摘要；本次未读、轮询或重跑云端 CI，未设置定时，也未重启个人服务。

## 服务筛选与分页恢复证据导航

| 场景 | 实际验证与范围 | 直达记录 |
| --- | --- | --- |
| 登记端点、承载 IP 和运行环境名称 | 本地前端分别验证三类精确匹配、组合条件、重置及已提交条件在分页、重载和主机导航返回时的保留。承载 IP 的 Console 全量首轮失败，后续通过的是 11 项定向导航和构建。 | [登记地址与重置](#服务监控的登记地址查询与重置) · [承载主机 IP](#服务监控承载主机-ip-筛选2026-10-22) · [宿主／运行环境名](#服务监控宿主运行环境名称筛选2026-10-22) |
| 时间与离线原因 | 共享控件统一预设、时间换算与离线原因枚举。受控浏览器验证时间边界、条件取交集、草稿隔离、重置及分页恢复。两轮完整 Monitor 门禁分别通过 272 和 275 项浏览器场景。 | [时间筛选与门禁](#服务实例时间查询共享入口) · [离线原因与门禁](#服务实例离线原因筛选共享入口) |
| 自动提交与刷新 | 10 月 29 日的受控回归验证了选择立即提交、IME、回第一页、关闭旧趋势、同 URL 重置和刷新恢复。Monitor 的 497 项浏览器场景及构建通过。共享前端聚合仍保留一项失败。 | [自动筛选、首次失败与复验](#服务监控自动筛选与就近提示2026-10-29) |

字段口径及 URL 恢复由 Monitor 规范维护。共享前端统一筛选计时和刷新协作，页面负责领域条件、分页及权限。以上筛选记录以本地受控验证为主，正式身份 Hosted 按对应场景单列。[Monitor 展示规范](../../addp平台运行观测规范.md#63-趋势时段比较与总览) · [共享自动筛选](../../../../common-frontend/docs/components/页面状态与导航.md#自动筛选与已提交范围) · [模块导航与历史](../../../../common-frontend/docs/components/页面状态与导航.md#模块公开导航与历史)

## 服务监控的登记地址查询与重置

2026-10-22，服务监控补齐按登记端点主机名／IP 精确筛选及一键重置。复用 System 已有 `registered_host`，由服务端在计数、分页前过滤；不按端口、DNS 解析结果、运行环境主机名或承载主机补匹配，也不为无监听端点的 Worker 推断地址。地址沿唯一 URL 保留到分页、刷新和趋势重载，未提交的地址草稿不影响自动刷新；重置清空筛选、页码及趋势，恢复在线且已声明采集的默认范围，保留刷新频率，同一默认 URL 上也清除未提交草稿。无新增 API、Swagger、权限或采集配置，未重启个人服务。

冻结基线 `d75dce1f1f2e345e7686e8d073baad9e8915ef48`，仅叠加本批八项实现／测试／规范输入，使用实际核验为 Node `v24.19.0` 的运行时，沿标准 `make test-monitor-frontend` 最终退出 0：88 项 Node、269 项浏览器及生产构建均通过。新增中英文回归验证同一主机不同端口均匹配、不借用相同宿主名／未登记端点的 Worker、只请求当页摘要、趋势及重载保留地址、重置与未提交草稿隔离；已检查截图 `/tmp/addp-service-address-visuals/service-address-{zh-cn,en}.png`。日志 `/tmp/addp-service-address-frontend-final.log`。首轮 268 项通过、中文新用例因英文关闭按钮名称定位超时而失败；改用同页既有关闭按钮定位后，完整门禁重跑通过，未调整产品时序或等待期限。首轮日志 `/tmp/addp-service-address-frontend.log`。

`make test-frontend-ci-registration` 通过 20 项回归及 21 个受控前端登记检查，日志 `/tmp/addp-service-address-registration.log`；现有 Monitor owner 与 CI 路径自动覆盖，无新增工作流。八项最终输入与成功冻结副本逐文件字节一致，随后仅追加本证据及后续事项摘要。成功浏览器输出目录与端口租约已由标准入口清理；首轮失败证据按标准保留在临时目录。本次为确定性 T1/T3 及构建验证，不计正式身份 Hosted T4，通过结果已交清单维护会话；CI 与已接入的 Meta 专项继续等待专门会话回传，未查询、调度或设置定时任务。

## 服务实例时间查询共享入口

2026-10-22，服务监控接入 System 已有实例时间契约：登记／离线判定时间、全部时间、15 分钟至 7 天预设及自定义范围；相对时段每请求从同一当前锚点重算，自定义范围固定为 URL 中的 UTC 起止，边界含起点、不含终点。当前状态、采集声明与时间取交集，不自动切换状态；恢复在线的实例退出离线时间结果。时间筛选不进入进程趋势请求，退出实例仍不开放历史趋势。查询、分页、重载、草稿隔离与重置沿原页面生命周期；新增相应时间列并保留在线状态与采集状态相邻。

时间控件、预设目录、RFC3339 规范化和 API 边界换算已统一到 Common Frontend 的 `RuntimeInstanceTimeFilter`／`runtimeInstanceTime.mjs`；System 原内联控件、日期校验与换算删除，日志页面改用同一预设目录，不保留旧导出别名。自定义范围未完整时的提示由共享控件就地呈现，删除 System 表格空态的重复提示。模块只组合自身列表和草稿提交；不改变 System 时间裁决、API/Swagger、权限或生产身份。

验证采用冻结基线 `a7353b132e7e8832549f247410671cec4e03fd1e`，叠加本批 22 项实现／测试／规范输入，Node 实际为 `v24.19.0`，不带共享工作区其他会话改动。`make test-common-frontend` 最终通过 249 项契约／工具／唯一所有权测试，日志 `/tmp/addp-service-time-common-verified.log`；`make test-system-frontend` 最终退出 0，152 项单元、194 项浏览器及构建通过，日志 `/tmp/addp-service-time-system-verified.log`。Console 的 `make test-console-frontend` 通过 188 项单元、206 项浏览器及构建，日志 `/tmp/addp-service-time-console-final.log`；随后对最终输入沿原标准浏览器入口补跑 `npm run test:e2e -- e2e/process-resource-navigation.spec.js`，九项服务导航全部通过，包括新增自定义时间提交、语言切换、重载及重置仍保持一个 iframe，日志 `/tmp/addp-service-time-console-scope.log`。

首轮失败均未计通过：新单元测试误保留默认趋势范围、System 两个日志消费者仍引用被删除导出、隔离 Console 副本缺少夹具模块依赖链接；另修正新增时间列插入位置与重复验证提示。相对时段新浏览器用例曾错误要求自然启动没有毫秒耗时，并在初始化或交互期间暂停时钟，分别造成空白页或加载遮罩退场停滞；改为页面就绪后仅暂停时钟验证窗口、交互前恢复。产品时钟、查询窗口、超时及业务断言未放宽，定向用例沿标准入口通过；相关失败和定向日志为 `/tmp/addp-service-time-{monitor,system,console}.log`、`/tmp/addp-service-time-monitor-{final,verified}.log`、`/tmp/addp-service-time-system-final.log` 与 `/tmp/addp-service-time-relative-clock-final.log`。完整最终 Monitor 结果按下段记录，不以定向成功替代。

最终 `make test-monitor-frontend` 退出 0：89 项单元、272 项浏览器及生产构建均通过，日志 `/tmp/addp-service-time-monitor-complete.log`；覆盖相对窗口随每次请求前移、未提交草稿隔离、自定义真实日期录入与 UTC 转换、半开边界、离线后恢复退出结果、中英文窄屏、分页／刷新／重载及趋势隔离。22 项最终实现／测试／规范输入与成功冻结副本逐字节一致。`make test-frontend-ci-registration` 通过 20 项回归和 21 个受控前端登记检查，日志 `/tmp/addp-service-time-registration.log`；现有 Common／System／Monitor／Console 路径已覆盖，无需新增 Make 或 workflow。

上述四组成功浏览器运行的输出目录与精确端口租约均已由标准入口清理，另行核对不存在。已人工检查此前同一共享控件的中英文窄屏截图 `/tmp/addp-service-time-visuals/service-time-{zh-cn,en}.png`；最终全量同场景及真实日期录入均通过。最终运行的补充截图捕获未取得新文件，仅为临时产物收集，不计新的截图证据，也未据此重复执行测试。本批为本地 T0／T1／T3 与构建验证，不代表正式身份 Hosted T4；CI 与 Hosted 等专门会话回传，没有查询、调度、设置定时或重启个人服务。

## 服务实例离线原因筛选共享入口

2026-10-22，服务监控复用 System 当前离线观测的 `stop_reason=graceful|lease_expired`，支持全部原因、正常退出及租约超时筛选，并在列表对应原因旁说明租约超时只表示失联。原因与状态、采集声明和时间取交集，不自动切换到 DOWN，不按浏览器时钟或采集失败推断退出；实例恢复后退出原因结果。沿原表单搜索提交、回到第一页并关闭旧趋势，分页／刷新／重载／Console iframe 保留已提交原因，自动刷新不应用原因草稿，重置清除原因；原因参数不进入进程趋势，离线实例仍不能打开历史趋势。

原因控件、枚举规范化和中英文译名统一到 Common Frontend 的 `RuntimeInstanceStopReasonSelect`／`runtimeInstanceStopReason.mjs`；System 原内联选择器和枚举删除，模块概览、实例列表与日志诊断同步消费共享译名。在 10 月 22 日这批验证中，System 选择后立即查询，Monitor 仍用搜索提交。后续服务列表的自动提交及实际复验另见[自动筛选记录](#服务监控自动筛选与就近提示2026-10-29)。

冻结基线 `66b1f8ab8725bd3c32c9b8a0150694561c894a62`，仅叠加本批输入，实际 Node `v24.19.0`。最终 `make test-common-frontend` 退出 0，251 项通过，日志 `/tmp/addp-service-reason-common-final.log`；`make test-system-frontend` 退出 0，152 项单元、194 项浏览器及构建通过，日志 `/tmp/addp-service-reason-system-final.log`；`make test-monitor-frontend` 退出 0，90 项单元、275 项浏览器及构建通过，日志 `/tmp/addp-service-reason-monitor-final.log`；`make test-console-frontend` 退出 0，188 项单元、208 项浏览器及构建通过，日志 `/tmp/addp-service-reason-console.log`。新增中英 T3 覆盖原因／状态／时间交集、计数分页、范围切换、恢复移除、草稿隔离、重置、未知原因不分类，以及真实 Console 语言切换／重载保持一个 iframe。`make test-frontend-ci-registration` 通过 20 项回归与 21 个受控前端登记检查，日志 `/tmp/addp-service-reason-registration.log`。

首轮 Monitor 为 273 项浏览器通过、两项新用例失败：测试错误要求手动刷新保留未提交草稿，既有行为是手动刷新恢复已提交条件；改为断言选择不提交，并由独立自动刷新用例验证草稿隔离，未改产品交互。标准入口定向两项通过后，完整 Monitor 门禁通过，首轮与定向日志为 `/tmp/addp-service-reason-monitor.log`、`/tmp/addp-service-reason-targeted.log`。首轮 System 在发现运行日志仍引用移走的译名后精确中断当次独立门禁，不计通过；补齐消费者与唯一所有权回归后沿相同入口完整重跑通过，未停止个人服务。初轮日志 `/tmp/addp-service-reason-system.log`，保留中断状态与当时已通过项，不扩大为完整验收。

最终仅为新窄屏截图增加“下拉弹层已关闭”的断言，再沿 `npm run test:e2e -- e2e/process-resources.spec.js --grep 'service stop reasons'` 重跑中英两项退出 0，日志 `/tmp/addp-service-reason-visual-final.log`。已检查稳定截图 `/tmp/addp-service-reason-visuals/service-stop-reason-{zh-cn,en}.png`；18 项最终代码／测试输入与成功验证副本逐字节一致，三项规范及能力摘要同步更新。四组成功浏览器门禁和最终截图专项的输出目录、精确端口租约均独立核对已清理；失败／中断证据按标准保留在临时目录。本批为本地 T0／T1／T3 与构建，不计正式身份 Hosted T4；CI 和 Hosted 等专门会话回传，未查询、调度、设置定时或重启个人服务。

## 服务监控承载主机 IP 筛选（2026-10-22）

Monitor 服务监控新增承载主机 IP 输入，复用 System `/platform/module-instances` 的 `node_ip`：前端只处理单值字符串与首尾空白，IPv4／IPv6、映射地址规范化和 `host_node_ips` 精确匹配继续由 System 拥有。无监听端点 Worker 可匹配；登记端点或运行环境名称相同但没有对应承载 IP 时不匹配，不由地址推断主机绑定。搜索、范围切换、分页、趋势、重载、主机跳转返回、自动刷新草稿隔离及重置沿唯一 URL；无效地址保留条件并显示查询错误，不回退全部结果，列表条件不进入趋势接口。双语说明位于输入框旁。

在冻结基线 `12fa8f30ba249cec7f70c8cceae47e3e4636a8c1` 上仅叠加本批输入，经 Node 24 标准入口验证：`make test-monitor-frontend` 的 91 项 T1、279 项确定性浏览器 T3 及生产构建通过。`make test-console-frontend` 首轮 189 项单测通过，浏览器 209 通过／1 失败：原有角色切换用例只提供 Worker，模拟接口此前忽略角色条件；本次让夹具按 System 的模块／角色／IP 取交集后，该用例错误地期待 Backend 查询仍返回 Worker。只修正此用例为 21 个 Worker 与独立 Backend，并断言实际 Backend ID、真实分页及零 CPU；产品筛选不放宽。随后沿 `npm run test:e2e -- e2e/process-resource-navigation.spec.js` 的全部 11 项服务导航用例通过，Console `npm run build` 通过。其他 209 项有效结果沿未变输入保留，未将定向复验冒充完整 210 项再次执行。

`make test-frontend-ci-registration` 的 20 项回归及 21 个前端登记检查通过；首次在无 `.git` 的归档副本执行时，回归已过但正式检查无法读取索引，补齐同一基线的前端 package 登记后通过，未修改 Make／workflow。现有 Monitor／Console glob、变更影响传播及 Platform CI 已覆盖新增用例，无需新入口。最终输入指纹及日志摘要为 `/tmp/addp-host-ip-final-audit.json`，原始日志为 `/tmp/addp-host-ip-*.log`；Console 实际 iframe 截图 `/tmp/addp-host-ip-visuals/console-host-ip.png` 已查看，双语窄屏无横向溢出由 Monitor T3 验证。此次仅为本地确定性 T1／T3 与构建证据，System 后端语义未改；未运行真实服务／Hosted T4，也未查询云端 CI、设置定时或重启个人服务，后续结果等待专门会话回传。

## 服务监控宿主／运行环境名称筛选（2026-10-22）

服务监控复用 System 既有 `node_name`，精确匹配部署声明的 `host_node_name` 或进程 `runtime_hostname`，忽略大小写。前端只规范化单值字符串首尾空白；不把登记端点、名称前缀或主机管理显示名称当作匹配对象，不按名称推断绑定或额外读取主机详情。输入框旁解释匹配范围；名称与 IP、模块、角色、状态、采集声明、离线原因及时间取交集。搜索／范围切换回第一页、关闭旧趋势；分页／重载／主机跳转返回保留条件，自动刷新只用已提交值，重置也清除未提交草稿，列表条件不进入趋势 API。无监听端点的多个 Worker 可按同一宿主名查询，各自实例与趋势身份不变。

冻结基线 `7b77919f87ef27201a7d754801e06dafe2c85832`，仅叠加本批前端、测试和规范输入，Node `v24.19.0`。`make test-monitor-frontend` 退出 0，92 项 T1、282 项确定性浏览器 T3 和构建通过；`make test-console-frontend` 退出 0，189 项单测、211 项浏览器和构建通过。新增双语场景覆盖宿主名／运行环境名匹配、前缀及同名登记端点排除、分页、趋势、组合条件、自动刷新草稿隔离、重复参数清除和重置；Console 覆盖名称与 IP 条件下的主机跳转返回、语言切换及重载保持唯一 iframe。`make test-frontend-ci-registration` 通过 20 项回归和 21 个前端登记检查；原批处理在此项启动前因检测到另一浏览器门禁退出，确认该入口只运行静态 Python 校验后单独执行通过，没有终止对方或重复执行有效浏览器结果。现有 Monitor／Console 门禁及路径扩散已覆盖，无需修改 Make 或 workflow。

原始日志 `/tmp/addp-host-name-test-{monitor-frontend,console-frontend,frontend-ci-registration}.log`，输入／日志指纹和成功门禁清理核验见 `/tmp/addp-host-name-final-audit.json`。本批输入与验证快照逐字节一致，标准浏览器输出目录及当次端口租约均已清理；保留截图 `/tmp/addp-host-name-visuals/`，已查看中文窄屏和 Console 页面，说明紧邻控件，双语窄屏无页面横向溢出断言通过。本批仅为本地 T0／T1／T3 和构建证据，未改后端、Swagger、IAM 或部署；未执行真实服务／Hosted T4，不查询云端 CI、不设置定时、不操作个人服务，后续结果等待专门会话回传。

## Transfer 两类双 Worker 标准启动与主机绑定验收入口（2026-10-22）

复用 Meta 的唯一启动单元实现，Transfer bounded／continuous 各由部署输入选择 1 或 2 个进程，默认各 1；单实例也使用 `-1` 编号。两类各复用原有二进制，独立 PID、正式日志进程身份、采集端点和 TLS 目录，共用部署声明的实际主机 ID。重复启动保留存活实例，只编译和补回缺失单元；活动缩容、重复／外来 PID、非法副本数及持续同步双副本配固定 Owner 均拒绝。Transfer 局部停止／重启覆盖全部四个 Worker，其他模块保持 PID。已有 continuous 故障验收改为唯一编号 PID，仍核验原二进制、专属部署、UID 和数据库边界；未改 claim、租约／fencing、任务并发度、System 登记协议或 IAM。

`platform-node-metrics` 增加 `transfer-workers`，与 `meta-workers` 共用验收主路径和 Console 浏览器实现：一个 Backend 加四个 Worker 独立登记并关联当次主机，按真实端点／SDK 身份及 `sync`／`continuous_sync` 类型校验来源；分别停止、补回两类中的一个 Worker，检查对等实例、当前趋势、新旧隔离、主机启停、观测中心故障和标准清理。已有 workflow 手工枚举、准入脚本及登记检查同次更新，仍用原预算、身份和零残留要求。这里只接通执行入口，未调度或执行真实 Hosted。

本地最终验证：

- `make test-dev-lifecycle` 退出 0：118 项部署／生命周期单测、59 项 Infra 日志／来源生命周期单测，标准端口与生命周期脚本通过；覆盖新增 Transfer 四 Worker 局部重启、其他模块保留、重启停机前拒绝及缺少第二实例时按共用产物补建。Common schema／repository／projectionstore 测试通过，Security、Meta、Quality、Transfer 的 cmd 入口编译检查通过（`-run '^$'`，不计业务执行测试）。
- `make test-node-metrics-online-runner` 退出 0：131 项 Runner／夹具／Hosted 编排／登记拒绝回归及 System Metrics 身份夹具 Go 测试通过；浏览器脚本语法检查通过，未运行真实浏览器 T4。
- `make test-transfer-continuous-online-runner` 退出 0：51 项持续执行 Runner／故障安全／隔离夹具回归及 System Online 身份夹具 Go 测试通过。

首次完整生命周期门禁在新增“第二 Worker 缺失”夹具失败：测试未选中对应 Backend，未进入标准构建阶段，因此没有 events 文件。夹具改为保留 Backend 与第一 Worker、仅缺少第二 Worker后，标准生命周期脚本和完整 `make test-dev-lifecycle` 重新执行通过；未修改产品启动条件或放宽断言。原始日志为 `/tmp/addp-transfer-workers-dev-final.log`、`/tmp/addp-transfer-workers-online-gate.log`、`/tmp/addp-transfer-workers-continuous-gate.log`，输入与日志指纹见 `/tmp/addp-transfer-workers-final-audit.json`。本次只验证本地 T0／T1 与原生临时进程生命周期，不计正式多实例登记／主机绑定 T4、租户同步任务、多 Worker 抢占或 HA 通过。未改个人配置、数据库或运行服务；CI 和 Hosted 结果等专门会话回传，未设置定时任务。

## Quality 多 Worker 标准启动、登记与主机绑定入口（2026-10-22）

复用 Meta／Transfer 的唯一启动与观测流程，新增部署输入 `ADDP_DEV_QUALITY_WORKER_REPLICAS`，默认 1、仅允许 1 或 2。单副本亦使用 `quality-worker-1`，双副本追加 `quality-worker-2`，共用原 Worker 二进制；独立 PID、启动身份及采集计划，沿既有主机声明及 Quality 正式登记。标准启动只补缺失单元，所属 Backend Ready 后才启动 Worker；重复启动复用现有实例，运行中缩容、外部 PID、重复 PID 与非法数量封闭拒绝；模块停止／重启覆盖所有编号单元。Quality 的 claim、任务并发、租约及权限未变，旧未编号启动路径已删除。

同一 `platform-node-metrics` 增加 `quality-workers` 专项，沿共用 runner、Console 场景和清理入口验证一个 Backend 与两个 `quality-bounded` Worker，登记与同主机绑定、独立采集身份、当前资源／趋势、停止补回一个 Worker、对等实例保持、主机启停和中心故障恢复。同步手工 workflow 选项与登记检查；原 `test-platform` 已聚合生命周期和 Online runner，不新增调度或第二套入口。

本轮标准本地验证均退出 0：`make test-dev-lifecycle` 包含 124 项启动／配置 Python 检查、59 项基础设施生命周期检查、完整原生进程／局部重启／构建 shell 夹具、Common 三个 package 校验，以及 Security／Meta／Quality／Transfer 的 cmd 编译检查（`-run '^$'` 不计业务用例通过）；`make test-node-metrics-online-runner` 的 138 项身份、TLS、绑定、趋势、浏览器回执及清理反例，加 System Metrics 身份 Go 夹具通过。原始日志 `/tmp/addp-quality-workers-dev.log`、`/tmp/addp-quality-workers-online.log`，输入及日志指纹见 `/tmp/addp-quality-workers-final-audit.json`；受测本批输入在门禁期间未漂移。本地原生进程为隔离临时夹具，未用个人真实服务证明登记／绑定。

真实 `quality-workers` Hosted T4 尚未运行；任务竞争、重复执行、租约接管、跨主机和生产 HA 均不在本批验收结论内。CI 与 Hosted 等专门会话回传；本轮未查询或调度云端验收、未设置定时、未修改个人配置／数据库或重启个人服务。功能清单由维护会话统一更新。

## 服务趋势交互证据导航

| 场景 | 已归档验证与边界 | 直达记录 |
| --- | --- | --- |
| CPU／RSS 独立缩放 | 受控浏览器核对两图独立视口、同范围刷新后保留、切换上下文后重置，以及错误和撤权后清空。7 项缩放场景已计入当批 346 项浏览器门禁。 | [缩放与冻结输入](#服务实例-cpurss-趋势的独立时间缩放2026-10-23) |
| 相邻等长时段比较 | 受控回归覆盖两期完整网格、启动裁剪、样本不足和缩放后比较不变。末版 33 项定向浏览器通过，其中服务 15 项、主机 18 项。正式身份下的真实长窗另行验收。 | [比较计算与分批门禁](#服务进程-cpurss-相邻时段比较本地) |
| 原位刷新 | 受控场景验证同一上下文的在途保留、原 DOM／canvas 和迟到响应拒绝，完整门禁在修正后通过 534 项浏览器场景。个人 Safari 实际观察到当前 Monitor 实例的 CPU／RSS 曲线推进并可悬停，范围限于当次观察。 | [刷新、首轮失败及个人复验](Monitor节点与主机资源历史证据.md#主机与服务原位刷新2026-10-30) |
| 弹窗就近提示 | Monitor 受控组合首轮 82 项通过、2 项失败，修正后的两项定向复验通过。Console 导航 13 项通过。个人 Safari 截图验证了短状态、CPU 曲线和提示展示，刷新过程另见原位刷新记录。 | [FieldHelp、复验与 Safari](#服务趋势弹窗就近提示2026-10-31) |

同一已提交上下文的刷新保留视口与比较，身份或查询范围变化时按各自边界清除旧内容。缩放、比较及刷新口径见 [Monitor 展示规范](../../addp平台运行观测规范.md#63-趋势时段比较与总览)，调度与迟到响应隔离见[共享页面状态](../../../../common-frontend/docs/components/页面状态与导航.md#可见页面自动刷新)及[请求隔离](../../../../common-frontend/docs/components/页面状态与导航.md#请求取消与迟到响应隔离)。

## 服务实例 CPU/RSS 趋势的独立时间缩放（2026-10-23）

服务趋势弹窗的 CPU、RSS 图分别接入既有共享 ChartRenderer 时间滑块，图旁提供中英文操作提示；两个临时视口独立，不裁剪完整响应、不新增请求或 URL 参数。相同已提交范围的手动／自动刷新保留相对视口；关闭弹窗、切换实例／时段／筛选或身份变化时重置，错误及撤权清除旧图与视口。指标、当前运行实例历史边界、权限及生产采集均未改变。

冻结基线 `10bfdbdf194ebef14afef40cb4f7db4b1d3a689e` 加本批七项输入，使用 Node 24.19.0、既有依赖和标准隔离入口执行：

- `npm run test:e2e -- process-trend-zoom.spec.js`：7/7 通过，见 `zoom-browser.log`。中英文窄屏真实拖动分别缩放两图；完整样本／真实零／缺样及陈旧断点保留，无额外请求／URL 变化；悬停、图例显隐、主题重绘、手动／自动刷新保留，时段／实例／关闭再打开／身份变化重置，503、403 和错误运行身份的清除与恢复均通过。
- `make test-monitor-frontend`：退出 0，101 项单元测试、346 项浏览器测试及生产构建通过，见 `frontend-full.log`；上述 7 项包含在 346 项内，不累加为唯一场景数量。完整门禁同时覆盖复用多图定位参数后的主机缩放、图例／悬停及既有进程页面布局／筛选，未改共享渲染器实现。构建保留原有大包提示，不将提示计为失败或性能验收。

证据目录 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-process-trend-zoom-p2lmhr_b/` 保存原始日志、`inputs.json`、`frontend-inputs-sha256.json` 及原表只读行号引用。浏览器使用独立端口和受控 API，成功后由标准入口清理运行目录；不操作 Safari、个人登录态或服务，不重启后端。本轮未查询或触发 CI／Hosted，真实页面验收等专门会话回传，不设置定时。此项前端通过不替代上一批 CPU/RSS 告警真实触发／恢复 T2 或正式身份验收。

此前附在本节的正式身份本地窗口记录见 [Trace 专题的第二窗口](MonitorTrace与日志历史证据.md#正式身份本地第二窗口卷采样超时与-minio-构建触线2026-10-23)。

## 服务进程 CPU／RSS 相邻时段比较（本地）

2026-10-23，沿用户确认的均值与原单位变化量口径，当前运行实例支持 5 分钟／1 小时／24 小时与上一等长时段比较。CPU 用核、RSS 用 IEC 容量，按未舍入评估点均值计算本期减上期；零基线有效，不给增长百分比。两期任一点缺样、陈旧或启动裁剪时不出均值／差值，各指标独立显示有效／预期点数；CPU 启动保护沿后端曲线评估时刻，抓取新鲜度独立核验。结果和时间就近展示，图中仅显示本期；缩放不重算比较。主机与服务共用 `common-frontend/basic/src/utils/evaluationPeriodComparison.mjs` 及 Monitor 唯一 `ResourcePeriodComparison.vue`，旧主机均值循环与重复卡片已删除。

冻结输入基线 `fb3d25f4b52324273d1fdcc3cb27c30df695b569`，仅叠加本批文件；证据目录 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-process-comparison-s_1g44u9`，`inputs.json` 记录本批最终文件，`frontend-full-inputs-sha256.json` 与 `frontend-inputs-sha256.json` 分别记录完整门禁及最终针对性回归的 435 项输入。交付前与共享工作区核验无前端输入漂移。实际命令均使用 Node v24.19.0：

- `make test-common-frontend`：272 项通过；共享均值、分界点归属、缺样、启动裁剪网格及唯一消费路径由确定性测试覆盖。
- `make test-monitor-frontend`：首次完整版本 109 项 T1、361 项浏览器 T3 及构建通过，见 `monitor-frontend-full.log`。此后仅修正比较函数内 CPU 预热判断的时间依据，并新增对应边界测试，没有把这份完整门禁记成最终源码重新全跑。
- 最终 `npm --prefix monitor/frontend test`：110 项 T1 通过；`npm --prefix monitor/frontend run test:e2e -- process-comparison.spec.js resource-comparison.spec.js`：33 项 T3 通过（15 项服务比较及 18 项主机比较回归）；`npm --prefix monitor/frontend run build`：通过。见 `final-monitor-unit.log`、`final-comparison-browser.log`、`final-build.log`。新增用例在独立副本恢复旧抓取时间判断时失败（`startup-regression-red.log`），最终评估时间判断通过。该末次修改只影响启用服务比较的分支，最终定向回归覆盖此分支，其余未变分支沿用前述完整门禁有效证据，不重复累计测试项。
- `make test-frontend-ci-registration`：20 项检查通过，21 个前端登记检查通过；共享／Monitor 现有自动发现和 Platform 前端编排覆盖本批，不新增 Makefile 或 workflow 路径。受影响的是新增共享纯计算的直接消费者 Monitor，未改动其他模块的导出或消费行为。

最终 T3 使用 HTTP 协议夹具与真实共享图表，覆盖两种语言／窄屏、原单位升降及零值、两期缺样、启动裁剪、48 小时查询的自适应步长、7 天拒绝比较、URL／刷新／关闭、身份及错误清除、缩放不重算比较；复制保存并人工检查了 `screenshots/` 下中英文卡片截图。未连接个人库或重启个人服务，未运行本批正式身份真实数据／长窗 T4 或 Hosted，CI／Hosted 等专门会话回传；此结果不覆盖尚未通过的 CPU／RSS 告警实源触发恢复 T2，不证明同比或长期留存已实现。原表第 18 行（B 列 17）仍为部分实现。

原附的窗口准备材料见 [Trace 专题中的 MinIO 产品镜像核验复用入口准备](MonitorTrace与日志历史证据.md#minio-产品镜像核验复用入口准备2026-10-23)。

## 服务实例 cgroup 证据导航

| 验证层 | 已归档结果与范围 | 直达记录 |
| --- | --- | --- |
| 自身采集 | Common 在 Linux 显式启用后采集自身组原始量。隔离实源 T2 验证三个 SDK 进程的共享组样本、内核身份和零残留。共享组证据限于采集与读取。 | [自身采集、冻结输入及清理](#服务实例容器配额自身-cgroup-采集基础2026-10-24本地) |
| 部署证明与配额视图 | 受控部署证明、System 关联、Monitor 授权和即时视图分别通过对应门禁。前置窗口超时、初始采样失败和浏览器首轮失败单列。真实独立容器完整限制层级及正式身份页面未在此轮验收。 | [证明、关联与即时视图](#服务实例容器配额部署证明精确关联与即时视图2026-10-24本地) · [挂载归属回归](#部署证明测试的单一挂载归属回归2026-10-25) |
| Linux 缺样与复验 | 旧 Job `120835774116` 的三条组样本断言失败。点号字段解析修复后，固定 `1f9b44df` 的 Job `120853412269` 实际通过共享组 T2。旧日志未定位具体缺失来源，首败保留。 | [原失败、逐源诊断及固定 Linux 结果](#自身-cgroup-linux-ci-缺样点号统计字段解析与逐源诊断2026-10-24) |
| 240 号迁移恢复 | 本地隔离测试验证失败回滚和限定恢复。获授权后，现场先恢复失败标记至 `239/clean`，随后只读核对已到 `240/clean`，240 项摘要及约束匹配。该证据限于迁移。 | [隔离验证与现场回执](#进程资源声明-240-迁移启动阻断修复2026-10-24本地) · [授权、前置条件和定向恢复](../../../guide/troubleshooting/数据库迁移.md#240-号进程内核身份声明迁移的定向恢复) |

现行采样身份、完整限制层级与用户读取约束见[平台运行观测规范](../../addp平台运行观测规范.md#61-固定指标目录)。

## 服务实例容器配额：自身 cgroup 采集基础（2026-10-24，本地）

用户已确认采用部署侧只读容器身份／限制层级证明，应用只采自身用量。本批仅实现 Common 的自身 cgroup v2 采集基础：同一进程部署对象显式设置 `cgroup_v2=true` 后，Linux 在原 mTLS 端点发布当前组 CPU 累计秒数、整体内存当前字节及内核 boot／cgroup inode／PID namespace 身份。默认不读组统计，不读取其他进程／组；文件／路径／整数／响应读取量有界，采样前后核对成员路径、组 inode 和 cgroup namespace，失败不生成组零值或影响原进程 CPU／RSS／启动数据。组用量与进程用量分别采集，不计算配额利用率。

最终冻结源码：`/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-cgroup-release-p9kt3o_j/source`，输入清单 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-cgroup-release-p9kt3o_j/inputs.json`（SHA-256 `c89f711a4b14528236c568a2dcde8f5a39f5564906868d19fa5781d93c90bc8f`），交付前已复核当前任务源文件逐项匹配。

- `make test-module MODULE=common GO_ONLY=1`：通过，151 个 Go package 报告 ok；包含配置严格布尔选择、有界读取、真实零值、缺失／重复／非法／超长文件、共享组与进程用量区分、不可用时不保留旧组值。macOS T1 不代替 Linux 原生采样，亦不计为其他模块 T2。
- `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=cgroup-source`：最终冻结版本通过。53 项准备回归、原有八类固定窗口检查及 Linux 挂载坐标测试执行通过；三个独立 SDK 进程在同一专属、非 root、private cgroup 的受限容器运行，经真实 mTLS／HTTP SD／Prometheus 读取，与独立内核 boot ID、组 inode、PID namespace 对照一致，CPU／内存原始量均有效。共享组明确不证明“一实例独立容器”或配额；未做任何比例计算或用户 API 读取。退出的 owned containers／networks／volumes／临时文件均为零，并额外核对本轮三次受控项目的容器／网络／卷零残留。
- 上一冻结版本 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-cgroup-final-e68g773p/source` 的 `make test-dev-lifecycle` 串行通过：130 项单元／生命周期回归、66 项 Infra 日志生命周期回归及后续标准端口、构建身份、并发／安装锁、Owner 命令构建检查均执行至入口成功退出。`make test-build-registration` 串行通过，59 项＋9 项回归，检查 26 个 Go 服务／Worker、59 个产品镜像登记。新增 cgroup 分项的五场景矩阵、分流及预算约束已在最终版本 53 项准备回归再次执行；未因新增分项减少原有四个场景。
- 上一冻结版本还实际通过 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=http-quality`：真实进程资源／趋势／混合身份拒绝／停源隔离、真实 HTTP 查询及告警生命周期（165.71 秒）通过并零残留。随后仅对组采样增加 namespace 前后核对及分项编排，已由最终 cgroup-source 实源分项补验；不将此前完整 HTTP 结果表述为最终源码全批重跑。

保留的失败：首次并行 `test-dev-lifecycle` 有 12 项 Worker 回执超时；首次并行 `test-build-registration` 有 3 项超时 error、3 项未等到构建事件的 failure。同一 Worker 夹具单独诊断随后启动正常，两个原标准入口串行均通过，未改产品逻辑、放宽超时或删除断言；尚未证明首次超时的唯一环境根因，不能把首次执行改记通过。新增分项另一次误在共享工作区执行，仅准备回归一项旧分流字符串断言失败，在资源创建前退出；断言已随真实新分项契约同步，最终冻结入口通过，该次不计为产品 T2 通过或最终版本证据。

CI：`cgroup-source` 作为现有 Release 指标矩阵第五个真实分项，与默认 all 共用唯一标准入口、原 20 分钟 Job 和零残留要求。Common 目录会按既有规则命中 Go 消费者及产品镜像；本机未运行全量产品镜像构建、原生 Linux 全包 T1 或正式身份 Hosted。CI／Hosted 结果仍等专门会话回传，不轮询、不自行触发重跑、不设置定时任务。未更改 IAM、个人部署限额或服务状态，未扩展既有 T4 Token 用途；本批无用户 HTTP API／DTO 变化，不要求生成 Swagger。

尚未实现：部署侧完整且新鲜的容器／限制层级证明、System 私有投影精确关联、Monitor 配额查询与页面。不能将采集基础计为服务实例配额视图或容器监控闭环完成。

原始日志与指纹：
- `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-cgroup-release-p9kt3o_j/common-t1.log`：`1e17914fa134ee70075fd36ceba70a356b7d12736d693bee65990eb7b58fbe66`。
- `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-cgroup-release-p9kt3o_j/cgroup-t2.log`：`e01b40538e2751291a6aa4f1ebc40aba1a40f93f2aca84035f229fea9afe6b41`。
- `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-cgroup-final-e68g773p/common-t1.log`：`6c4c5676a95e9847a5b6487ae2095d19705596aae8bdfb042cc7bf0478659f1e`。
- `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-cgroup-final-e68g773p/metrics-t2.log`：`76a62821ade34329719415c648a1372d1f5ffe9d71981111c6ba5e86e849294f`。
- `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-cgroup-final-e68g773p/dev-lifecycle.log`：`0607b9c8148824291c81270dbd29de06b50ce53b232b2765cfbb7fa95144fac8`。
- `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-cgroup-final-e68g773p/build-registration.log`：`96d399f371844f82e7dec186994a49a1326c4bc9f078381f1e8aca8006ce3554`。
- `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-cgroup-final-e68g773p/dev-lifecycle-serial.log`：`a8857c205b427f30663e7e1495ccb3782ee9b7a2594b39f1252a804b0d58d110`。
- `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-cgroup-final-e68g773p/build-registration-serial.log`：`18a14e13947e6803a759306be2ca5c66310cedfa4de80b1be80d858bad2d394a`。

## 进程资源声明 240 迁移启动阻断修复（2026-10-24，本地）

本批开发中新增的 JSONB 子对象减键表达式缺少括号，现场标准启动在迁移阶段失败；已修正为先取子对象再减键，保留完整严格约束。为现场留下的 `240/dirty` 补既有 `iam-migration-repair --migration 240 --apply` 的限定案例：串行化锁内逐条校验 1—239 的文件名／摘要、原已验证 CHECK 及不可变触发器函数，仅恢复 `239/clean`；不改实例、权限、角色或 checksum，之后仍由正常 Runner 执行 240。默认不带 `--apply` 明确拒绝。

独立冻结仅含本次启动修复的八个文件，不含仍在开发验证的配额查询／界面；输入清单 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-cgroup-repair-qi1nba46/inputs.json`，SHA-256 `71b05ac64be67fa6123439da503ffa0ef309b8ea80fc54719a426cf2608fedb0`。已通过：

- `make test-module MODULE=system GO_ONLY=1`：System Go T1，`system-t1.log`；不将被环境隔离的集成测试计为 T2。
- `make test-system-iam-runner`：30 项确定性入口回归，`runner-t0.log`。
- `make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package migration --test process-cgroup-repair --source-root <上述冻结 source>"`：实际 `addp_iam_test`，精确复现错误 SQL 与事务回滚；拒绝 clean 状态、历史摘要变更、触发器停用和旧约束变更；只恢复失败标记后正常执行完整目录至 `240/clean`，重复启动通过，拒绝重放恢复及已记录 240 的回退。`repair-t2.log` 无条件跳过，测试拥有 Schema 在退出清理。
- `make build-iam-migration-repair` 及构建产物不带 `--apply` 的调用：构建通过，默认拒绝修改并退出 2；`build.log`、`apply-guard.log`。

现场仅做只读元数据核对：开发库 `addp` 为 `240/dirty`，checksum 恰好 239 项且全部匹配当前目录，240 无记录，原 CHECK 和不可变触发器完整。尚未获得本次个人开发库恢复授权，未执行恢复或服务重启；以上本地隔离通过不能记为现场已恢复、CI 或 Hosted 通过。修复工具的使用边界及标准重启步骤见数据库迁移故障排查的 240 专节。

随后用户明确授权此次开发库限定恢复。已执行标准构建产物 `addp-iam-migration-repair --migration 240 --apply`，退出成功并报告 `239/clean`；再以只读事务核对数据库确为 `addp`、单行状态 `239/clean`、摘要仍为 239 项且逐项匹配、无 240 摘要、原 CHECK 与不可变保护完整。现场操作日志为同一证据目录的 `现场限定恢复.log`。已告知用户在自己的终端执行 `./scripts/dev/restart.sh -all`，正常应用修正后的 240；AI 未重启服务，此处不宣称现场已达到 `240/clean`。修复已单独发布为 `954220f1688f1737dc8ffc0984aecfc98019ea4a`，不包含待验证的容器配额查询／页面。

交付前再次只读核对，现场随后已正常达到 `240/clean`：checksum 共 240 项且无差异、240 摘要存在、新严格 CHECK 及不可变保护匹配。AI 未执行额外恢复或服务重启；此项只证明迁移状态与约束生效，不代替服务就绪、容器实机或正式身份验收。

## 服务实例容器配额：部署证明、精确关联与即时视图（2026-10-24，本地）

本批接通已确认的一实例一独立 Linux 容器路线：部署侧沿现有观察进程发布有期限的只读完整层级证明，Common 不可变声明提供实际内核边界，System 精确关联当前租约、模块／角色和启动时刻，Monitor 沿当前用户授权／受限来源／预算读取四项组资源。查询后重读证明，变化则拒绝。页面在既有实例详情中分开展示进程 CPU／RSS 与整体组用量、硬上界及利用率，原因就在卡片旁；原生 Mac 不铺四张空卡片。没有完整证明／有限上界时不推算比例，不更改部署额度、应用权限、命名空间或 Docker socket，不增加告警／历史目录。

证据目录 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-container-chain-final-bxc7x1qp/`。最终执行输入为 `inputs-diagnostic.json`，SHA-256 `ed2954a8e9fa40c9034bab9817deb839ad71c62ea4e1fc3115458cb41b054544`；交付输入为 `delivery-inputs.json`，SHA-256 `3d8ed24ab1fdd789a9bcecc71e2c2a8a466d9f26bfd6f6d210b13b45a4c102a0`。二者仅观测规范文档不同：保留其他会话已发布的独立段落，并移除本批已接通链路的过时计划措辞；产品及测试源文件逐项一致。早期分层通过结果仅在相应源文件未变时复用，未把临时排错版本计为最终通过。

实际通过的标准入口与范围：

- `make test-module MODULE=common GO_ONLY=1`：151 个 Go package，`common-t1-v4.log`；包含身份格式、声明深复制、私有投影重复容器／边界拒绝、自身采集及原生平台不支持行为。对应 Common 输入与交付一致。
- `make test-module MODULE=system GO_ONLY=1`：合并迁移恢复工具与本批关联实现后的最终 T1 通过，`system-t1-combined.log`。`make test-system-iam-runner` 的最终恢复工具入口为 30 项通过，证据见上一节。
- `make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package repository --test observability-identity --source-root <冻结 source>"`：真实 `addp_iam_test` 中登记／重复登记、不可变 cgroup 声明、原始 JSONB 严格约束、私有投影及既有实例筛选通过，`system-pg-t2-v6.log`；该 T2 输入中的登记、投影和 240 SQL 与交付一致，之后只增加上一节独立验证的恢复 CLI，不将此项表述为全量 IAM T2。
- `make test-module MODULE=monitor GO_ONLY=1`：最终 Monitor T1 通过，`monitor-t1-final.log`；包含固定单实例 API 契约、角色／参数拒绝、当前用户先授权、精确组标签、原完整 CPU 窗口、证明变化拒绝、无证明不出比例及 CPU／内存上界独立判定。集成条件跳过不计 T2。
- `make test-deployment-discovery`：34 项既有发现与原子发布、5 项新增宿主层级／证明代际／生产只读投射测试通过，`deployment-runner-v3.log`；父级更严上界、缺父级、非零 CPU burst、重复部署、超预算数字、来源变化及不擅自把宿主 CPU 当分配额度均覆盖。此处为受控文件／命令夹具，不能代替真实宿主完整层级证明。
- `make test-dev-lifecycle`：`dev-lifecycle-fixed.log` 完整退出 0；130 项准备、66 项 Infra 生命周期、60 项后续生命周期、端口／构建身份／并发锁／多 Worker 和四个 Go owner 的命令编译均通过。首次两项失败是截取生产启动阶段的测试未设置项目根目录及私有输入目录，已按真实阶段契约补齐，不调整产品默认值或断言；其后仅增加安全诊断函数及测试，已由下项准备回归覆盖。
- `make test-build-registration`：59＋9 项回归及 26 个 Go 服务／Worker、59 个产品镜像登记通过，`build-registration-final.log`。新文件沿既有 owner／输入发现命中，现有五场景指标矩阵保持，未另设 CI 或 Hosted 调度。
- `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=cgroup-source`：最终 `cgroup-query-t2-diagnostic.log` 退出 0。八类固定窗口、来源 mTLS／标签控制／超量拒绝、Linux 挂载坐标及三个真实 SDK 来源均通过；新增 `TestIntegrationMetricsCgroupSources` 真实执行 57.58 秒，验证完整一分钟组 CPU、memory.current、错误 cgroup 身份拒绝，以及共享组绝不推断容器额度。标准退出与独立按项目标签核对均为容器／网络／卷零残留，临时文件清零。
- `make test-monitor-frontend`：最终 `frontend-final.log` 完整退出 0，114 项确定性测试、414 项浏览器 T3（20.4 分钟）及生产构建通过；新增有／无证明的中英文视图、单位、缺配额原因与原生实例不铺空卡片覆盖。更早的全量执行为 411／413 通过、两项导航／刷新超时；原入口最终版本全部通过，未放宽超时、加产品重试或删除断言，不能将首次失败改记成功。
- `bash scripts/swagger/gen-swagger.sh system monitor`、`bash scripts/swagger/check-route-coverage.sh system monitor`：双模块生成及 247／91 个公开路由方法覆盖通过，`swagger-v3.log`、`swagger-coverage-v3.log`；之后无 API／DTO 变化，生成文件与交付一致。

实源门禁保留两次失败记录：`cgroup-query-t2-final.log` 在并行环境的前置窗口测试累计超过既有 60 秒，未进入新增查询；串行 `cgroup-query-t2-serial.log` 八类窗口通过，但初始夹具采样在 65 秒内未满足，缺少当次目标诊断，唯一根因未查明。随后同一标准入口增加脱敏失败诊断及对应确定性测试，才执行上述最终验证；不记录原始目标 URL、OAuth 配置或 scrape 错误，最终准备回归亦通过。两次失败的项目分别为 `addp-metrics-t2-324f7394baad45dc96ae9f0b049909ce`、`addp-metrics-t2-379720025b1f4c31affdfd63682db7ee`，退出后均独立核对容器／网络／卷为零，不以未进入目标断言的执行计通过。

最终实源项目为 `addp-metrics-t2-8ca4e7a7aeff4f9f805140ee6d13dc0e`。该夹具刻意让三个进程共享同一 cgroup，证明采集／查询和拒绝边界，**不证明一实例独立容器、部署发布器在原生 Linux 上读取完整层级、System 至真实用户页面的整个配额闭环或正式身份 T4**。这些实机／正式身份验收尚未运行，当前 Mac 原生进程也没有容器配额。CI、产品镜像全包、Hosted 及生产 T5 未在本会话执行或轮询，继续由专门会话回传；未扩大已有 Hosted Token 用途，未设置定时任务。

## 自身 cgroup Linux CI 缺样：点号统计字段解析与逐源诊断（2026-10-24）

专门 CI 会话回传源码 `4108c283a183e4bfc89e76a3b38aa7cc1f6e48a8` 的 [Run 40080160036／Job 120835774116](https://github.com/pampa0629/addp/actions/runs/40080160036/job/120835774116)：RSS 三源进入发现后，`check_self_cgroup_samples` 的三条组样本断言失败。已读取原始日志 `/tmp/addp-ci-20261024-100026/job-120835774116.log` 第 796—804 行。该日志未标明缺失的具体族或来源；TLS 负例、udev 提示和 System 240 迁移恢复均不能替代本条 CI 根因／通过证据。核对 `f72ac75b1` 后原字段名解析规则仍未改变，不能声称既有功能提交已修复该失败。

独立复现了一个确定的采集缺陷：[Linux v6.11 `kernel/cgroup/rstat.c`](https://github.com/torvalds/linux/blob/v6.11/kernel/cgroup/rstat.c#L555) 在 `CONFIG_SCHED_CORE` 下输出 `core_sched.force_idle_usec`，原解析器只允许小写字母／下划线，遇到这个合法字段会丢弃整组 CPU／内存。先添加真实字段形状的回归，`make test-module MODULE=common GO_ONLY=1` 因此测试失败（其余 150 个 package 通过）；再将唯一解析路径改为有界点号分段名称。仍拒绝空段、前后点号、路径字符、重复键、非规范数字及超长内容，仍仅读取 `usage_usec`，没有忽略畸形内容或补零。修后同一入口 151 个 package 通过。

最小诊断沿现有专属 T2：各 SDK 启动记录只增加固定布尔事实（是否取得自身样本、根组文件可读、解析有效、内核是否有该字段）；每个来源按固定序号报告三项 Prometheus 样本数量和独立内核身份匹配。缺样时每来源仅一次五秒、最多 256 KiB 的原 mTLS 读取，区分源端未输出与发现／入库问题；不记录端点、原始正文、任意附加字段或凭据。原精确三源集合、独立内核身份、真实有限数值和后续完整窗口／新鲜度断言全部保留，没有新增等待以迁就缺样。

冻结源码目录 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-cgroup-ci-fix-d0aycm9k/source`，基线 `704ecab46edeedc47da520512bc2abd083984271`，`inputs.json` SHA-256 `5a2b1e01e8d4ae476f3397d6687e5588e4073ec38b9c8ae94abfa054206ce7d3`，交付前任务文件逐项复核无漂移。证据位于同级目录：

- `common-red.log`／`common-green.log`：上述 Common 标准入口红／绿结果。
- `system-t1.log`／`monitor-t1.log`：`make test-module MODULE=system GO_ONLY=1`、`make test-module MODULE=monitor GO_ONLY=1` 均退出 0；不将环境门控的集成跳过计为 T2。
- `cgroup-t2.log`：`make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=cgroup-source` 退出 0，55 项准备回归、八类窗口、mTLS／标签／超量限制、Linux 挂载坐标和新增点号字段回归通过；三个真实 SDK 的三族各一条且身份匹配，`TestIntegrationMetricsCgroupSources` 实际执行 59.34 秒通过完整组 CPU／内存、错误身份拒绝及共享组不推算配额。标准清理和独立项目标签核对均为容器／网络／卷零残留、临时文件清零，项目 `addp-metrics-t2-8efb0820d7a748e6b9789bbd909d59b1`。日志 SHA-256 `e4fadba2e2766ec6f8120d97f5deb36721208db348da18451357fc2551782ec6`。

本机 Docker Desktop 内核的三条启动诊断均为 `core_sched_field_present=false`；新增合法字段回归另在真实 Linux 专属容器执行通过。上述本机交付时尚未取得修复后 Linux CI 诊断，当时没有将旧 CI 缺样断言为唯一根因或记为通过。现有 Release 五场景矩阵及 Common／Monitor 路径与输入自动发现已覆盖本次六个文件，无新 API／DTO、权限、迁移、配置或前端变化；未操作个人服务、keepalive 或数据卷。

2026-10-24 专门 CI 会话回传后，已独立核对固定修复源码 `1f9b44df83d48a78b96fbe4b3d91e447624b8518` 的 [Release and T2 gates／Run 40085914201／Job 120853412269](https://github.com/pampa0629/addp/actions/runs/40085914201/job/120853412269)，事件为 push、attempt 1。checkout 日志与 SHA 一致，实际标准入口为 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=cgroup-source`。原始日志 `/tmp/addp-ci-20261024-120030/job-120853412269.log` SHA-256 `4a47c59cfef55b7b7f603959439e9408de61893f6eb240652c94656ce4638a3d` 与独立审计 `/tmp/addp-ci-20261024-120030/Monitor-1f9-cgroup-independent-audit.json` 一致。第 791—803 行显示挂载坐标／点号字段回归通过，三个 SDK 均报告 `core_sched_field_present=true`、解析及自身样本有效，三项 Prometheus 族各一条且匹配独立读取的内核身份；真实完整分钟 CPU／内存查询和错误身份拒绝用例实际运行 58.68 秒通过。第 808 行确认本轮容器、网络、卷及临时文件零残留，后续作业摘要为 success。

因此，本条固定版本 Linux cgroup-source T2 已通过，原旧版本失败及本机证据继续分别保留；不再因同一缺样重复验收。该结果仅证明三个 SDK 的共享组原始量采集、完整窗口读取与身份拒绝，**不证明独立容器关联、完整限制层级、正式身份配额页面或当前 main／告警级别筛选的 CI**。本次只补录回传证据，没有重跑 CI／Hosted、设置定时任务或操作用户服务；同一证据已由协调会话通知清单维护会话，不重复发送。

## 部署观察器退出证据导航

| 阶段 | 已归档结论 | 直达记录 |
| --- | --- | --- |
| Linux 首次退出失败 | `994db320` 的 Job `121040604338` 未确认观察器退出，保留运行记录。旧日志缺少状态和信号证据，根因未证实。 | [首败与有界诊断](#持续部署发现-linux-退出失败调查仅补诊断根因未证实2026-10-25) |
| 诊断增强后的 Linux 复验 | `05b23292` 的 Job `121044843183` 整 Job 通过，未复现原间歇退出失败。 | [固定 05b 的原日志与终态](#持续部署发现固定-05b-linux-门禁通过原间歇失败仍未闭环2026-10-25) |
| 单次停止信号 | `effd9f80` 的 Job `121213997615` 清理失败。受控复现确认信号落入 Popen 析构时可能丢失退出意图，原 CI 缺少当时的 Python 栈。修复后的本机隔离 Linux 复验与仍失败的开发聚合分列。 | [复现、取消路径及子进程回收](#2026-10-26-部署观察器单次停止信号在析构边界丢失) |
| 后续自然 Linux 终态 | `2a2afdf4` 的 Job `121231972455` 已通过停止与挂载证明分项，整 Job 在迁移选择断言失败。`1fb051a5` 的 Job `121248639865` 通过固定源码的 Platform consistency。 | [两个 Job 与迁移选择复验](#原生linux回传与迁移选择断言同步2026-10-26) |

部署证明的挂载归属另有原 Platform T0 失败和本地修复记录。[挂载归属回归](#部署证明测试的单一挂载归属回归2026-10-25)

## 持续部署发现 Linux 退出失败调查：仅补诊断，根因未证实（2026-10-25）

专门CI会话回传后，只读核对 [Platform Run40150409107／Job121040604338](https://github.com/pampa0629/addp/actions/runs/40150409107/job/121040604338) 原始日志的checkout与实际失败步骤，源码为 `994db320ae7147cb0e566db4960eb256306cc9e1`。`make test-platform → test-dev-lifecycle → test-infra-runtime-log-lifecycle` 的61项中，`test_logs_failure_does_not_prevent_selected_metrics_start` 在 `addCleanup/stop_discovery_observer` 收到“部署发现进程尚未退出，不删除运行记录”；Job实际失败，后续摘要写入成功不能覆盖。日志没有当时的进程状态／等待／信号／子进程证据，不能断言是僵尸、锁、Docker子进程或预算不足，更没有证据把它归因于新增告警 `host_name` 逻辑。

本机基线 `make test-deployment-discovery` 的34项发现＋5项容器证明，以及 `make test-infra-runtime-log-lifecycle` 的79项前置、61项生命周期、登记／Go夹具均通过，未复现原Linux退出失败；macOS通过不能替代该Linux问题的根因证据。没有对同一未变输入反复加压运行、放宽原100次、50ms间隔的退出观察循环、增加信号／sleep／重试、强删记录或跳过断言。

本批只在现有“无法确认退出”失败分支追加即时有界诊断。Linux从同UID普通proc文件读取状态、固定等待类型、SIGTERM挂起／屏蔽／忽略／处理位及直接子进程数量；每个文件最多8192字节，拒绝符号链接／非UTF8／超量，读取失败明确未知，其他平台明确不可用。只输出固定键、枚举、布尔及数量，不输出命令行、路径、PID清单、任意wait文本或随机运行标识。字段依据为 [Linux proc状态手册](https://man7.org/linux/man-pages/man5/proc_pid_status.5.html)及[内核proc文档](https://docs.kernel.org/filesystems/proc.html)；瞬时诊断不参与归属或清理裁决。原SIGTERM只发一次、foreign拒绝、确认退出前保留记录、退出后由控制流程清空来源等主路径不变，无新配置、接口或权限。

先补三项诊断回归，标准发现入口因缺失诊断函数产生3个错误，保留红灯日志；实现后再补真实文件读取的大小／编码／符号链接拒绝，最终 `make test-deployment-discovery` 为38项发现＋5项容器证明全部通过。`make test-dev-lifecycle` 最终退出0，132项原生生命周期、79项前置、61项Infra生命周期、Online登记／Go夹具、端口／缓存／锁／原子构建及现有共享包检查通过；末尾Worker命令的“无测试”仅计编译覆盖，不外推为Worker功能验收。`git diff --check`通过。

原始失败 `/tmp/addp-ci-20261025-013034/job-121040604338.log`；本机 `/tmp/addp-discovery-exit-{baseline,diagnostics-red,diagnostics-final,dev-final}.log`、`/tmp/addp-discovery-infra-baseline.log`及`/tmp/addp-discovery-exit-selection.txt`。本批不宣称原CI退出故障已修复，Linux实测与新诊断尚待专门CI会话回传；没有轮询、手工派发或重跑CI／Hosted，也没有操作个人服务、keepalive、数据库或数据卷，不设置定时任务。已只读核对功能清单第28／30／34行的主机管理内容，本次诊断不等于新增主机注册、远程初始化或批量运维，不据此升级功能完成程度。

## 持续部署发现固定 05b Linux 门禁通过：原间歇失败仍未闭环（2026-10-25）

专门CI会话回传后，只读独立核对落地原始日志、Job及实际步骤终态。[Platform Run40151852077／Job121044843183](https://github.com/pampa0629/addp/actions/runs/40151852077/job/121044843183) 为push／attempt1、固定源码 `05b232920cd1801fcbb409a392aa6386f73efe26`；实际 `Run platform consistency gate` 从01:48:44至02:11:09 UTC完成success，Job亦success，后续平台／Swagger检查实际走完，非仅摘要成功。原失败路径的Infra运行日志／Trace／PostgreSQL metrics生命周期标准聚合61项于01:51:17记录OK（19.245秒）；后续部署发现38项及容器证明5项也实际OK。不将其他同数测试组或初始MinIO缓存用例误记为该组，不把同Job其他既有跳过项算作运行验收通过。

本轮只证明退出诊断增强版本在该Linux标准门禁通过，没有重现994原轮的 `stop_discovery_observer` 失败，也没有取得失败时的新退出诊断；**原间歇故障根因仍未证实，不能认定已修复或注销**。保留原失败日志、归属检查、信号次数、观察循环及未确认退出不删记录的边界，不为采样反复触发或扩大预算；后续已有正常push若复发，继续由专门CI会话取回有界诊断再定位。不据此提升主机注册、远程运维、主机名称筛选Safari／Hosted或生产完成程度。

证据目录 `/tmp/addp-ci-20261025-023003/`：原始 `job-121044843183.log` SHA-256 `323e6228ec32acda74e8ea701ee4a611e1ec6473f929e39a391d4f33ba981e1e`与协调审计一致，`jobs-40151852077.json`确认准确head_sha及实际步骤success；协调审计为 `Observer05b-actual-Linux-Platform-diagnostic-audit.json`，本会话新收据 `Observer05b-owner-independent-audit.json`。本轮仅补证据文档并执行 `git diff --check`；没有轮询、派发或重跑CI／Hosted、操作个人服务或设置定时任务。

## 服务组部署预期与运行确认证据导航

| 阶段 | 已归档范围 | 直达记录 |
| --- | --- | --- |
| 预期协议与租约事实 | 受管目录原子发布，System 只读汇总当前租约。真实 PostgreSQL 验证用途隔离、停用、过期及一致快照。此阶段的全量平台门禁在 Online 登记处失败。 | [协议、T2 与原失败](#2026-10-25-服务组部署预期协议与只读租约汇总自动接入未完成) |
| 标准启停接入 | 开发与生产入口发布预期，计划停用必须在首个停止动作前成功发布。84 项配置与开发生命周期复验覆盖发布失败时保留进程记录和服务运行。 | [接入、停止屏障与首轮夹具失败](#2026-10-25-标准启停接通服务组预期与停止前发布屏障) |
| 首次运行确认的前置修正 | 微秒变更标记区分同秒停用、重启与缩容，时钟停滞或回退时拒绝写入。历史登记总数与有效在线确认分别使用。 | [标记消歧与 87 项复验](#2026-10-25-确认运行前置组预期变更标记消歧) |
| 后续失联规则与页面 | 持久确认、失联与恢复已在真实数据库配合受控组投影／时钟下验证。个人 Safari 验证了保存、启用和在线评估。真实租约故障注入、通知及正式身份 Hosted 的证据仍限于准备与本地 runner。 | [规则与持久确认](Monitor告警与规则历史证据.md#2026-10-25-服务组失联规则与持久确认) · [个人页面](Monitor告警与规则历史证据.md#safari服务失联规则保存复验2026-10-26) · [Hosted 准备](Monitor告警与规则历史证据.md#服务失联hosted专项准备与本地runner验证2026-10-26) |

现行契约分别见[部署预期与分组租约](../../addp平台运行观测规范.md#48-服务部署预期与分组租约观测)和[服务组失联规则](../../addp平台运行观测规范.md#711-服务组失联规则)。

## 2026-10-25 服务组部署预期协议与只读租约汇总（自动接入未完成）

用户已确认标准部署入口是唯一预期事实来源，按模块＋Backend／Worker角色＋用途判断全部副本失联。本批只实现底层发布协议及System观测基座：既有 `deployment-inventory.py service-expectations` 在受管目录按用途原子发布预期，幂等重发、局部启停、并发合并、预算／所有权／非法输入拒绝沿单一路线；不是用户手工导入流程。System沿原私有接口只读消费，正式租约计数不依赖指标或主机关联；用途不明、文件异常／变化及子域预算耗尽不可判定，计划停用／模块停用暂停。Common校验可解码子域并隔离非法事实，原JSON／控制面拒绝规则不变。没有新增身份、权限、浏览器DTO或个人环境操作。

实际验证：

- `python3 -m unittest scripts/test/metrics-deployment-config_test.py`：74项通过，包含9项新发布测试及UTC格式拒绝；并发写入、版本幂等、原子替换失败保留旧文件、无临时文件残留、非法正文不回显。
- `make test-module MODULE=common GO_ONLY=1`、`MODULE=system GO_ONLY=1`、`MODULE=monitor GO_ONLY=1`：各自Go T1退出0；不计默认跳过的实库用例为T2通过。
- `make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package repository --test observability-identity"`：标准入口核验本机实际容器映射，只在 `addp_iam_test` 重置并清理；真实PG T2退出0，无跳过。新分组子场景覆盖Meta同用途双副本、Transfer用途隔离、租约过期、正常注销、计划停用、模块停用、用途未知，以及并发变更后仍保持可重复读快照。既有完整私有字段白名单、原身份／引擎观测及DB错误拒绝继续通过。
- `make test-dev-lifecycle`：退出0，151／99／61项聚合、Shell生命周期／隔离、共享Schema与相关Worker命令编译通过；后续仅增加UTC坏时间拒绝用例，已由上述最终74项配置测试复验。
- `bash scripts/swagger/gen-swagger.sh system` 与 `bash scripts/swagger/check-route-coverage.sh system`：退出0，247个公开路由方法覆盖一致，私有响应新增类型同步；没有增加路由。
- T2登记检查通过：22个Hosted-service、12个门禁自有Compose、9个Hosted-only、2个owner-managed入口；Metrics显式输入增加新Common契约，System与Go标准自动发现覆盖本批。`changed-gate.py --dry-run`只计发现，不把选中其他Owner全量门禁计通过。
- `make test-platform`：退出2，失败于既有Online聚合内 `security-mysql-owner-protection` 的 `addp.security-mysql-owner-protection-online/v27` 版本登记一致性，经 `test-raster-boundary-clean-online-runner` 向上返回；本批未改Security／Raster suite、导出浏览器或该版本声明，不接管修复，不计全量T0通过。之前已取得上述独立相关门禁终态。该完整本地聚合日志为 `/tmp/addp-service-expectations-platform.log`，不代表实际Hosted验收。

日志位于 `/tmp/addp-service-expectations-{publication-final,common-go,system-go-final,monitor-go,postgres-final,lifecycle,swagger,registration,selection}.log`。CI／Hosted未查询、派发或运行，继续由专门会话回传；没有设置定时任务、重启个人服务或修改个人数据库／IAM。

协议阶段尚未接通标准启停，计划停用发布失败的处理也待确定。失联权限、评估窗口、事件、通知与页面均待后续实现和验收。原清单第 15／49 行当时仍为部分实现。[标准启停接入](#2026-10-25-标准启停接通服务组预期与停止前发布屏障)

## 2026-10-25 标准启停接通服务组预期与停止前发布屏障

用户明确确认：计划停用发布成功后才允许停止；发布失败立即报错，保留服务运行。本批在上一节协议基座上接通开发与生产入口。开发从当前已解析的后端选择与正式编号Worker单元发布，Meta双副本、Transfer两用途、Quality用途保持独立；Security Worker不在新增范围。生产仅渲染当前Compose声明，使用Profile和规范化scale／deploy.replicas，不读取存活容器推断预期；零副本、未选Profile及完整计划不再选择的已有组置为计划停用，局部开发启动不改变其他组。渲染凭据不输出或持久化到声明及覆盖文件。

开发／生产停止在首个launchd、进程、容器或缓存清理动作前写入计划停用；根.env与原启动入口使用同一目录配置，失败不提供旁路参数。生产沿既有工作区生命周期锁串行，System只读目录挂载由服务声明生成器唯一拥有，移除进程指标生成器中的重复路径；指标未选中也能发布预期。启动发布失败报告可选观测错误，核心业务仍继续，不能把旧声明当作新启动成功证据。文件发布只表达意图，不证明实际启停成功或告警恢复。

本地实际验证：

- `python3 -m unittest scripts/test/metrics-deployment-config_test.py`：84项通过，含原74项及10项新增。覆盖标准开发／生产停止失败时无停止命令、PID记录保留、锁释放；局部停止只改所选模块；首次listener／Compose down前核对已发布计划停用；真实Docker Compose CLI渲染Profile及副本数（没有启动容器）；生产标准启动阶段仅成功发布后引入只读覆盖文件，失败不复用旧覆盖；指标关闭仍发布；非法来源／超量／渲染失败不回显秘密、不覆盖旧意图；原SDK生成器不再依赖或拥有发现挂载。
- `make test-dev-lifecycle`：最终退出0，161项聚合、99项运行日志生命周期、61项回归、端口／所有权／全量及局部生命周期、共享Schema／projectionstore与相关Worker命令编译通过。初轮唯一失败是Raster受控停止夹具未复制新增发布器依赖；仅补齐测试依赖后重验通过，没有放宽停止屏障或修改Raster产品。
- `python3 scripts/ci/check-t2-ci-registration.py --repository "$PWD"`：22／12／9／2门禁登记通过。既有Metrics输入增加开发／生产停止及共享生命周期锁；根Make原生命周期入口增加生产停止脚本语法检查，新增测试由现有配置测试聚合自动发现，不新增门禁路线。`changed-gate.py --dry-run`只计发现，不计其他Owner测试通过。

本轮没有Go产品或HTTP契约变更，沿用上一节System真实PG T2与Swagger证据，不重复充当本批运行验收。没有运行个人标准启停、操作个人数据库／IAM、触发CI或Hosted，也未设置定时任务。全量平台T0前轮已在无关Security MySQL Online v27登记一致性处失败，本批没有重跑或接管；本批上述相关独立门禁取得通过终态。

日志为 `/tmp/addp-service-intent-hooks-{config-final,lifecycle-final,registration-final,selection}.log`。启停接入阶段仍缺个人实际启停到 System、Monitor 正式身份的全链证据及真实生产部署证据。当时失联规则、连续窗口、事件与通知尚未接入，首次登记前的处理、触发／恢复时长及权限待确认。原清单第 15／49 行当时仍为部分实现。

## 2026-10-25 确认运行前置：组预期变更标记消歧

用户确认首期只覆盖确认运行后的失联；首次尚未确认运行的组保持等待，首次启动失败另待部署结果事实。本次调查确认System的registered_count包含历史登记，不能用于冒充当前有效在线确认；Monitor尚未实现该资格的持久化或失联评估。独立规则四项权限（仅平台系统管理员）、15—300秒显式持续时间及建议60／30秒、warning级别、按本组预期重新确认及新类别显式订阅的完整提案已写入运行监控设计末节并向用户提问，未发布权限或实现上述未确认规则。

独立修复部署发布器：原changed_at仅到秒，同秒停用、重启后会复用先前标记，无法可靠隔离前后运行确认。改用真实UTC微秒，组内容或运行意图实质变化必须严格晚于该组原值；时间不前进或回退即拒绝发布、保留旧文件，不合成未来值。相同预期仍幂等，不相关组保持原标记。仍为同一文件、既有字段和发布路线，无API、私有投影字段、权限、IAM或数据库变更。

验证：新增三项回归先在原实现中复现失败（五处断言，87项全体退出1）；修复后 `python3 -m unittest scripts/test/metrics-deployment-config_test.py` 87项退出0。同秒停用／重启／缩容逐次区分；时钟停滞／回退时启动修改、计划停用及完整计划退役均不写入，幂等重发仍成功，临时文件零残留；其他组标记不受影响。`make test-dev-lifecycle`最终退出0（164／112／61项聚合、局部生命周期、共享包及Worker命令编译）；T2登记22／12／9／2通过。新用例沿原配置聚合与Metrics输入自动发现，无需改Make或workflow。日志 `/tmp/addp-service-intent-confirmation-{red,green,lifecycle,registration}.log`，收据同前缀 `-receipt.json`。

标记修复阶段仅完成部署侧前置验证。原全量 T0 失败仍单列，当时个人实际启停到 System、Monitor 的链路未验，持久确认、规则管理及告警通知尚未接入。原清单第 15／49 行当时仍为部分实现。后续实现与实际验证见[规则与持久确认](Monitor告警与规则历史证据.md#2026-10-25-服务组失联规则与持久确认)。

## 部署证明测试的单一挂载归属回归（2026-10-25）

专门CI会话回传Platform T0失败：run `40207277037` / job `121210797431` 的 `make test-platform → test-business-config → test-deployment-discovery`，旧 `container-proofs_test.py` 仍向SDK指标适配器索要隐式`system-backend`发现挂载，报KeyError。当前生产发现目录已由标准生命周期唯一发布，不应恢复旧适配器双轨。

本地先以 `make test-deployment-discovery` 复现同一错误，再仅修正测试：显式Monitor SDK只输出Monitor，配置但不存在的发现目录不被适配器创建；新增交叉归属用例验证标准生命周期的System环境和发现挂载、`read_only=true`及显式`create_host_path=false`、不复制渲染密码；无SDK及显式System SDK两种输入都不让适配器重新拥有发现挂载或改写生命周期覆盖文件。原cgroup opt-in、TLS路径、只读与非特权断言保留。产品、权限、个人服务、Make和workflow不变，新用例沿原标准入口自动执行。

修复后 `make test-deployment-discovery` 退出0（38项发现＋6项证明及脚本语法）；`python3 -m unittest scripts/test/metrics-deployment-config_test.py` 87项退出0，覆盖原生产生命周期单一发现挂载、关闭指标仍声明及启动／停止屏障。日志 `/tmp/addp-container-proof-owner-{red,green,lifecycle}.log`。此处仅为本批本地T0修复，不将原失败改写为通过，不外推整轮Platform、Linux CI或Hosted；修复后的自然CI结果等专门会话回传，未自行查询、重跑CI或设置定时任务。

## 2026-10-26 部署观察器单次停止信号在析构边界丢失

专门CI会话回传固定source `effd9f800e05516d1809a8e63d202e3f3516b19f` 的Platform run `40208335725` / job `121213997615`：`test_disabled_logs_never_stop_foreign_containers`清理观察器失败，原诊断为`owned/S/hrtimer_nanosleep`、零子进程、SIGTERM未屏蔽／未挂起且已注册处理器。本批不把`term_caught`当作处理器实际执行证据。

新增受控回归用例把真实控制器发出的一次SIGTERM精确落在一个已完成的真实Popen对象析构期间，并用接收标记及`sys.unraisablehook`分别证明信号实际接收、SystemExit成为被忽略异常。修改产品前，标准 `make test-deployment-discovery` 在新用例失败，收据为`signal_received=True; unraisable=True`，且当次自有测试进程最终回收。本机隔离Linux/arm64、Python 3.12.14对旧源码复现同一失败与原Linux诊断全部字段；这是已确认的失效路径，原CI缺少当次Python栈，不能据此指定其准确析构位置。

修复为单一路径的合作式取消：信号处理器只锁存请求，`set_wakeup_fd`打断原30秒空闲select等待；主循环、锁等待与Docker读取在显式检查点退出。Docker命令只创建一个子进程，分段等待不重发命令、不重置原10／30秒预算，取消时先kill/reap该自有未回收子进程。原控制器PID／命令／token归属、只发一次TERM、退出失败保留记录、有界脱敏诊断及非阻塞快照失效不变；没有自动停止个人进程或改变权限／租户范围。

实际验证：

- `make test-deployment-discovery`：40项发现／观察器测试＋6项证明及脚本语法退出0。新增析构边界从红转绿，保留外来PID拒绝、来源锁竞争、运行记录发布失败回收、在途Docker中止及子进程回收；新增同一Docker进程等待不重置时限测试。
- `make test-infra-runtime-log-lifecycle`：标准入口退出0；包含现有Trace本地runner前置（123项及既有Go夹具）及61项Infra／Trace／PostgreSQL来源生命周期聚合，不将受控夹具计为真实部署验收。
- `python3 -m unittest scripts/test/metrics-deployment-config_test.py`：87项退出0，覆盖生产／开发声明及停止屏障。
- 本机隔离Linux/arm64、Python 3.12.14：运行标准scripts/test的观察器类及本次／上次复发两个Infra单例，18项退出0，含真实信号、在途读取子进程回收及不停止外来容器。一次性容器无Docker Socket、无个人配置或数据库挂载，源码为临时脱敏拷贝只读挂载，容器退出销毁，不代替Ubuntu x64自然CI；测试tmpfs需exec以运行既有临时CLI夹具。首轮绿套因tmpfs默认noexec使在途Docker夹具无法启动，已用PermissionError与挂载选项独立确认；修正仅当次测试tmpfs后取得上述18项有效证据，不把首轮失败当通过。
- 已尝试更广 `make test-dev-lifecycle`，164项聚合出现两项超时：标准停止声明夹具10秒超时、Bounded Worker夹具就绪等待超时。该聚合退出1，不计通过、不放宽时限；前者在上述独立87项重新验证通过，后者运行既有Worker脚本片段，不调用本批修改的部署观察器。本批不扩展修理其他夹具，保留原失败供专门会话核对。

日志：`/tmp/addp-discovery-{finalizer-red,finalizer-green,stop-current,stop-lifecycle-green,stop-metrics-green,stop-infra-green,stop-linux,stop-linux-green}.log`。未自行查询／触发／重跑CI或Hosted、未设置定时任务、未执行个人服务或keepalive生命周期；自然Linux CI等专门会话回传。Safari实际可见性及真实服务失联验收仍保持原缺口。

## 原生Linux回传与迁移选择断言同步（2026-10-26）

| 固定源码与 Job | 实际执行与终态 |
| --- | --- |
| `2a2afdf482aabcea3ab6dadc2f8aea01c6560f3a` · [40214288480／121231972455](https://github.com/pampa0629/addp/actions/runs/40214288480/job/121231972455) | 原日志第 2191—2195 行的 61 项 Infra 生命周期及第 2335—2344 行的 40 项部署发现、6 项部署证明均实际通过。整 Job 随后在 `test-system-iam-runner` 失败，旧精确选择断言漏列实际已选中的 `PlatformServiceAvailabilityRulesForwardMigration`。 |
| `1fb051a50e7c46975411542a0e9340f7fcb97929` · [40219785488／121248639865](https://github.com/pampa0629/addp/actions/runs/40219785488/job/121248639865) | Platform consistency 实际执行到末尾，`ADDP_CI_SELECTED=true`、`ADDP_CI_STATUS=success`。停止流程相关 61 项及迁移 runner 契约 30 项均执行并通过。 |

本地沿 `make test-system-iam-runner` 复现 30 项中的同一失败。修复仅同步选择断言，逐项核对失联迁移、告警／HTTP／Trace 及 Runner 用例，保留无关迁移、伪发布名、前后缀扩展、缺少后缀和错误 package 拒绝。修复后同一入口 30 项通过。本地复验限于迁移选择断言，实库迁移与权限各有独立门禁。

两轮原日志分别为 `/tmp/addp-ci-20261025-180006/job-121231972455.log`、`/tmp/addp-ci-20261025-200006/job-121248639865.log`，本地红绿日志为 `/tmp/addp-service-availability-selector-{red,green}.log`。原 Job 失败与本地开发聚合的两项超时仍分别保留。后继 Linux 通过的范围限于该固定 Platform Job，Safari、真实失联触发／恢复／通知及 Hosted 另行验收。

## 服务监控自动筛选与就近提示（2026-10-29）

按用户截图调整：Console运行监控菜单将主机监控移到服务监控之前；服务列表删除搜索按钮，下拉选项和完整时间范围立即查询，文本复用共享300毫秒防抖，Enter／清空立即提交。保留System原精确匹配、权限、分页、URL replace和单iframe；查询变更回第一页并关闭旧趋势。重置、离页和身份／路由变化取消待提交计时，隐藏暂停、前台恢复后提交，避免旧输入覆盖新范围；同URL重置仍恢复原自动刷新。未完成自定义时间继续就近提示，不查询无效时段。

登记端点、宿主名、承载IP、采集声明及时间依据的补充说明统一使用Common Frontend的FieldHelp，支持悬停和键盘焦点，提示宽度受视口约束并允许换行；不保留常驻说明双轨，输入错误反馈仍在控件旁显示。共享时间控件的System消费方同步验证；无API、Swagger、权限或指标语义变更。

本地实际验证：

- `make test-monitor-frontend`：131项单元测试、497个浏览器场景及构建全部通过，含本批服务监控文件的46个场景，覆盖组合筛选、原分页与趋势边界、IME确认、重置取消及自动刷新恢复。
- `make test-console-frontend`：219项单元测试、259个浏览器场景及构建全部通过。服务监控文件单独13项通过；最终新增场景另行1项通过，覆盖菜单顺序、一次防抖查询、下拉立即提交、悬停／键盘提示、同iframe和宽窄屏布局。截图在调整视口后推进受控时钟，等待加载完成再取证，避免把冻结中的过渡遮罩当作页面最终效果。
- `npm --prefix system/frontend run test:e2e -- e2e/module-query-route.e2e.js`：受共享时间提示影响的4项全部通过。
- `make test-node-metrics-online-runner`：162项runner契约及既有System身份Go夹具通过；仅删除原服务页面点击搜索的旧操作，沿自动查询继续原正式场景断言，不改变权限、预算或验收用途。不等于Hosted执行。
- `make test-common-frontend`：287项中286项通过、1项失败。本批FieldHelp／防抖取消测试通过；唯一失败为ResourceTree直接组合所有权清单发现其他会话新增的`system/frontend/src/components/engines/EngineDirectPermissions.vue`，本批不修改该组件或放宽清单，不能将共享聚合计为通过。

旧专项首次失败包含未适配的搜索／常驻说明断言；第二轮44项通过、2项失败，后者定位为真实长提示撑宽窄屏，已在共享组件限制提示宽度并允许换行。原日志保留，不把这些失败算作通过；修正后上述完整Monitor门禁通过，不以Console通过替代Monitor整体。

本批沿既有Common Frontend、Monitor、Console、System时间控件及Online runner入口自动发现，无需新增Make或workflow；未运行后端、数据库或真实Safari正式登录验收，未重启个人服务、触发／轮询CI或Hosted、设置定时任务。CI与Hosted仍等专门会话回传。

日志：`/tmp/addp-process-filters-{common-final,monitor-full,console-full,console-final,system-final,online-runner,visual-final}.log`；最终宽窄屏截图目录：`/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-service-filters-KLmUkP/`。旧失败为同前缀`browser.log`与`browser-final.log`。本节为本地受控页面T3及确定性回归证据，不外推真实数据链路、Linux CI或T4。

## 服务趋势弹窗就近提示（2026-10-31）

服务趋势弹窗复用共享 FieldHelp：进程启动范围放在时段控件旁；CPU／RSS含义、断点、缩放及不完整比较的启动边界放在对应指标标题旁。资源组整体口径及各项指标解释同样就近收起，证明改为中英文短状态并保留详细提示；原生未接入时不再同时铺开无证明长说明。共享提示支持悬停与键盘焦点，不复制另一套tooltip实现。缺样／无法计算、读取失败、样本不足、七天比较不可用的当前反馈仍直接可见，查询窗口、数值、硬上界和核验时间保留；资源含义、状态裁决、查询、权限、刷新与当前进程历史边界未改。

沿原服务、趋势缩放、比较及原位刷新T3验证组合行为，补齐中英文已核验／未核验四种组合和原生未接入两种语言。实际聚焦／悬停后读取tooltip，核对正文不再常驻相同说明；保留核数、RSS、硬上界、无证明不可算利用率、图表缩放／图例／刷新及撤权清空断言。Console语言切换回归同步改为读取同一iframe里的实际提示，不再等待已移走的正文。

本地实际验证与失败保留：

- `npm --prefix monitor/frontend test`：131项T1通过，日志 `/tmp/addp-service-trend-tips-unit.log`。
- `npm --prefix monitor/frontend run test:e2e -- process-resources.spec.js process-trend-zoom.spec.js process-comparison.spec.js resource-inplace-refresh.spec.js`：82项通过／2项失败（3.3分钟），日志 `/tmp/addp-service-trend-tips-browser.log`。两项同因：中英文旧范围断言仍在等待正文“曲线保留断点”。改为聚焦范围旁提示并核对实际内容，图表布局、范围／刷新／返回和375像素宽度检查不变；沿原入口 `--grep 'current process trends restore their scope'` 复验两项全部通过（11.8秒），日志 `/tmp/addp-service-trend-tips-scope.log`。原失败保留，不将初轮整批写成退出0；其他82项的产品及相关测试输入未变，复用有效结果。
- `npm --prefix console/frontend run test:e2e -- process-resource-navigation.spec.js`：13项T3全部通过（1.2分钟），日志 `/tmp/addp-service-trend-tips-console.log`，包含同iframe内语言更新、实际提示、刷新、导航与筛选恢复。
- `npm --prefix monitor/frontend run build`：退出0（4.90秒），日志 `/tmp/addp-service-trend-tips-build.log`；既有大chunk提示保留。差异格式检查通过。以上均使用仓库要求的Node24及现有标准测试入口。

个人Safari第二个标签页沿真实Console打开当前Monitor进程，初始AuthContext无法确认后点击原“重试”恢复；无需重启。新版正文已直接生效，实际截图可见短状态“未接入资源组采集”、查询窗口、CPU曲线与标题旁问号；点击CPU问号实际显示核占用／非百分比／断点／缩放解释。该个人环境为原生macOS，不计容器限额或来源支持通过；当前页面提示自动刷新开启，但本轮只记布局及提示可见，不把一次截图计为新的刷新或Hosted验收。

本批只改Monitor领域组合／中英文词条及相应Monitor／Console测试，未改共享组件、API、Swagger、后端、部署或IAM。局部标准T1／T3及Monitor构建是本次显示变更的最小充分验证，未重跑整套Monitor／Console聚合、未变的Go、来源T2或Online，不能记为本轮整套通过。CI和Hosted仍等专门会话回传，未自行查询、触发或重跑，也未设置定时任务、重启个人服务或修改账户／数据库／配置。

## 停用意图顺序夹具隔离与配置 T1 入口（2026-11-01）

独立 Mac 的 Docker 候选版本验证按用户要求暂缓，未执行、未通过。本轮只处理现有配置 T1 的宿主依赖：此前完整配置套件中，`test_standard_stop_publishes_selected_scope_before_the_first_stop_action` 曾超过原10秒期限；本轮原用例单独运行通过（4.819秒），不能据此认定旧超时已解决。

临时夹具的阶段取证表明，空PID／监听者拓扑仍执行宿主真实 `ps -axo pid=,ppid=`；当次开发／生产假停止分别3.025／0.443秒。该证据确认无关宿主进程表依赖，不足以指定此前超时的唯一根因。现为同一夹具补明确的空快照替身，仅接受声明的快照查询，其他查询返回78；测试验证停用发布前不得有动作、模块范围精确、查询顺序及无实际停止命令。macOS允许随后检查空launchd清单，不将其当作停止动作；原8／10秒期限、失败拒绝、锁释放和声明断言保留。实际PID／后代／外来进程归属继续由既有开发生命周期专项覆盖，本批未修改产品停止逻辑。

新增标准入口 `make test-monitor-metrics-config`，独立运行既有配置测试文件，不启动个人服务或真实来源；原 `test-dev-lifecycle` 和 Metrics T2 仍覆盖同一文件。

实际验证与失败保留：

- 首轮新入口136项中135项通过、1项失败：加强事件顺序断言时遗漏macOS已有的两次 `launchctl list`。仅修正测试为首两项精确匹配，后续只允许空launchd查询，未放宽停止动作检查；原失败日志 `/tmp/addp-monitor-config-isolated.log` 保留。
- 最终 `make test-monitor-metrics-config`：136项全部通过，退出0，22.317秒；日志 `/tmp/addp-monitor-config-isolated-final.log`。其中打印的来源／T2阶段为受控配置夹具，不计真实来源T2通过。
- 原用例独立结果及阶段证据：`/tmp/addp-monitor-stop-preflight.log`、`/tmp/addp-monitor-stop-stage-summary.json`。初次临时xtrace读取发生解码失败，未作时间结论；修正临时诊断解码后取得上述阶段记录，未改变产品或测试时限。

本批仅证明配置T1及空拓扑夹具隔离，不将旧聚合超时改写为通过；未重跑完整开发生命周期、真实来源T2、CI或Hosted。未改Docker设置、个人数据库／凭据或服务启停，未设置定时任务；外部结果仍等专门会话回传。


## Standard 启动失败与重试恢复的 Hosted 场景接入（2026-11-08）

本批仅准备 `platform-node-metrics / standard-startup`，未调度 CI、Hosted 或新定时任务，未在个人环境注入故障或启停服务。正式用途与操作边界见[场景规范](../scenarios/平台观测与语义生命周期.md#standard-开发-backend-启动失败与重试恢复)。沿用既有三员 Bootstrap、真实密码／MFA、平台模块读取权限和正式 Console；没有新增生产权限、旁路登记或伪造业务 API 的 Online 路线。

实现固定的 Standard Go `init` 未就绪故障，走标准开发启动的原 60 秒就绪屏障；要求进程仍存活、终端给出超时及屏障失败、System 仅发布本次 `readiness_timeout`、没有有效 Standard 实例且前端阶段未启动。故障与恢复分别通过真实 Console 登录读取服务列表、失败提示和刷新重载，并对请求路径、精确模块筛选、分页、失败时间、实例启动身份和实际页面文字作绑定核验。标准停止后仅清除归属证明匹配的故障，再由标准启动重试；Ready 必须来自实际端口、当前提交和这次登记的 Standard 实例，原 System／Monitor 进程不得被替换。

清理沿既有 Hosted trap：先停止当次应用，再清理故障，然后销毁来源、基础设施和私有身份目录；清理失败不能通过。故障记录绑定检出、提交和运行 ID；脏检出、既有 PID／文件、内容／权限／UID／硬链接／符号链接或归属记录异常均拒绝处理。未完成的安装由外层 trap 收尾。首次失败、后续恢复的浏览器 JSON／运行预算回执和截图分别保存，失败阶段的脱敏终端判定和返回码不会被后续结果覆盖；原始命令输出留在随后销毁的私有目录。总预算 1800 秒、浏览器 240 秒／子进程 300 秒、Job 60 分钟均未扩大。

本地验证：

- `make test-node-metrics-online-runner`：153 项 Online 登记回归、188 项指标 runner／归属／生命周期回归及 System 身份夹具 Go 测试通过；覆盖新分支封闭选择、只读预检、完整成功、首次页面失败、清理失败仍销毁其他资源、超时预算拒绝、来源／启动身份不符、过期或伪造页面回执及首次失败证据保留。
- `make test-console-frontend`：257 项确定性测试、316 项受控浏览器 T3 及生产构建通过。新增的受控 Console 场景复用正式 Online 的同一页面回执读取器，覆盖失败、恢复及两次刷新重读；此处使用受控 API，不能计为真实 Hosted 通过。
- 加强失败提示的完整时间文字核对后，`npm --prefix console/frontend run test:e2e -- e2e/process-resource-navigation.spec.js --grep 'Standard failure'`：1 项定向 T3 通过；`python3 -m unittest scripts.test.platform-node-metrics-online_test.StandardStartupOnlineTest scripts.test.platform-node-metrics-fixture_test.StandardStartupFixtureTest`：12 项定向 T1 通过。
- 首轮新增 T1 曾因夹具使用未解析的 macOS 临时路径和 `+00:00` 时间戳而失败，分别改为真实路径和既有 UTC `Z` 契约后通过；未修改产品就绪规则。首次日志 `/tmp/addp-standard-startup-runner.log` 保留；最终标准 runner 日志 `/tmp/addp-standard-startup-runner-release.log`，Console 完整／定向日志 `/tmp/addp-standard-startup-console.log`、`/tmp/addp-standard-startup-console-final.log`。本地成功的浏览器临时目录由标准入口清理，不将其记作可下载 Hosted 产物。

CI 登记沿现有 `test-node-metrics-online-runner`／`test-online-runner`、Console 前端门禁及 `online-t4-gates.yml` 的封闭 case 选择；删除新分支或退出清理调用会被登记回归拒绝。实际执行继续由专门会话选择 `suite=platform-node-metrics`、`node_metrics_case=standard-startup`，核对两阶段真实报告及零残留。当前状态为“场景已接入，本地验证通过，真实 Hosted 未执行”，不以旧源码的隔离 Linux 结果替代。


## Backend 就绪等待的总预算与复用进程初始化修复（2026-11-08）

继续核对 Standard 启动链路时，发现共用 `wait_backend_ready` 的 `curl` 未限定连接和响应时限，`waited` 也只累计失败后的休眠次数。因此，端口已接受连接但不返回响应时，原 60 秒等待不会正常结束。隔离的本机临时 HTTP 服务复现：设置 `MAX_WAIT=2` 后，真实请求已到达，但 3 秒外层截止仍被触发；没有操作个人 Backend 或数据库。复现记录 `/tmp/addp-backend-readiness-slow-response-before.json`，三项新回归在旧实现下失败的记录 `/tmp/addp-backend-readiness-budget-red.log`。

同次检查发现，`MAX_WAIT=60` 原来只在新建 System 进程的分支初始化，复用 System 时可能没有预算。将初始化移到两个分支之前，保持新建、复用和其余所选 Backend 使用相同预算；定向回归的修复前失败保留于 `/tmp/addp-backend-readiness-reuse-red.log`。

先补[开发生命周期规范](../../addp开发服务生命周期与构建身份规范.md)的共用等待规则，再实现单一截止循环：连接、响应与重试间隔共同扣减预算，单次连接和总时限取剩余时间与原请求上限的较小值，截止后到达的 HTTP 成功不计就绪。Ontology 原单请求 5 秒上限保留，删除原额外 curl 参数路径；Manager 专用存活检查仍沿原入口。超时仍由既有 PID 判定发布 `process_exited` 或 `readiness_timeout`，没有新增状态、权限、配置项或公开接口。

验证：

- `make test-deployment-discovery`：53 项部署／启动回归及 6 项容器证明回归通过。覆盖所有所选 Go Backend 屏障、真实慢响应被截止、请求／重试共享预算、单请求上限、过期成功拒绝、System 复用预算及原有失败原因判定。中间新增测试误从已被重定向的标准输出读取 curl 参数，改为读取夹具日志后通过；该测试断言失败记录保留于 `/tmp/addp-backend-readiness-budget-final.log`，最终结果为 `/tmp/addp-backend-readiness-budget-final-verified.log`。
- `make test-dev-lifecycle`：完整开发生命周期门禁通过；日志 `/tmp/addp-backend-readiness-lifecycle.log`。其中出现的来源／T2 名称属于门禁的受控编排测试，不据此认定真实来源或 Hosted 通过。
- `bash -n scripts/dev/start.sh`、本批 `git diff --check` 通过。Platform consistency 既有 `make test-platform` 已包含开发生命周期及 Business 配置／发现门禁，新方法由原测试文件自动发现；无需新增 CI 路线或调度。

本批仅本地实现与验证，未触发或跟踪 CI／Hosted，未设置定时任务，也未重启个人服务。Standard 的真实超时与重试恢复继续由专门会话运行 `platform-node-metrics / standard-startup`；当前修复不能作为该 Hosted 已执行或已通过的证据。


## 共享 Backend 等待变更的 Ontology runner 适配（2026-11-08）

CI 专门会话回传 `c1adee620787b0c205e60f6f6fe7d6000e78f417` 的两个确切失败：[Platform consistency Job 128655471384](https://github.com/pampa0629/addp/actions/runs/42456145614/job/128655471384)、[Release 的 Ontology FalkorDB Job 128657117699](https://github.com/pampa0629/addp/actions/runs/42456145585/job/128657117699)。两者均首先失败于 `InfraContractTest.test_ontology_readiness_uses_configured_port_and_propagates_failure`，不能据此认定 FalkorDB 产品链路通过。原始回传日志在 `/tmp/addp-ci-20261108-160021/log-128655471384.txt`、`log-128657117699.txt`；本地原标准入口复现的两子例失败记录 `/tmp/addp-ontology-readiness-adaptation-red.log`。

根因是上轮共享等待改动遗漏了此确定性夹具的适配。夹具仍以 `MAX_WAIT=1` 配合 `sleep(){ :; }`，但新的总预算会把请求限制为 `min(剩余 1 秒, 原上限 5 秒)=1 秒`；休眠替身不推进时钟，造成失败分支在同一秒内空转数千次，旧的“单条 5 秒 curl”断言随之失败。这不是放宽请求上限或另建等待路线的理由。

仅修改原 runner 夹具：在子 shell 中解除 `SECONDS` 的 Bash 特殊计时语义，建立由请求和休眠替身显式推进的普通逻辑时钟，避免真实调度时间影响确定性断言。七组闭合用例分别核对短预算成功／失败、未选择模块零请求、5 秒原上限、慢响应与休眠共同扣减（请求上限依次 5／4／1 秒）、瞬时失败后恢复，以及截止时到达的成功拒绝。逐次比对请求开始时刻、连接和总时限、原配置端口、休眠次数、父级退出码和失败发布的次数／模块／PID／标签；不只替换预期字符串。

`make test-ontology-infra-config`：11 项 runner／配置／退出清理 T1 通过，日志 `/tmp/addp-ontology-readiness-adaptation-final.log`。`make test-deployment-discovery`：53 项部署／启动和 6 项容器证明回归通过，日志 `/tmp/addp-ontology-readiness-deployment.log`。原生命周期标准脚本 `bash scripts/test/dev-lifecycle-and-build.sh` 通过，见本轮日志 `/tmp/addp-ontology-readiness-lifecycle.log`。中间测试曾因新增 `set -u` 与 macOS Bash 空数组展开冲突而失败；恢复夹具原有 `set -e` 后通过，不改变生产执行选项或业务断言。

`make test-gate-selection-unit` 的 33 项选择回归，以及 `make test-t2-ci-registration` 的 85＋9 项回归与实际登记校验均通过；日志 `/tmp/addp-ontology-readiness-selection.log`、`/tmp/addp-ontology-readiness-registration.log`。

生产等待实现、总预算、Ontology 原单请求上限、ready 判定及接口均未修改。实测原变更矩阵对该 runner 文件给出零受影响模块，未选中 Ontology；仅在既有 `ADDP_T2_INPUT_FILES` 中补共享启动脚本、runner 测试及原门禁入口，并增加三条路径各自必须选中 Ontology 的回归。登记缺口的修复前失败保留于 `/tmp/addp-ontology-readiness-selection-red.log`。Platform 的 `make test-platform` 和 Release 的 `make test-ontology-falkor` 都已包含该根标准入口，沿现有矩阵命中，不新增 CI 路线。本批未重跑 Hosted 或 FalkorDB 产品链路，未启停个人服务；新提交的两个相关 CI 实际步骤继续等专门会话回传，不以本地通过认定其已通过，也不设置定时任务。


### Ontology runner 修复的固定提交 CI 闭环回传（2026-11-08）

专门 CI 会话随后回传并提供原始日志，本会话核对 checkout 的完整 SHA 均为 `2b05bc27c8452cc58a7775449a413725abaac0a2`：[Platform Run 42462171750／Job 128673609447](https://github.com/pampa0629/addp/actions/runs/42462171750/job/128673609447) 的 `Run platform consistency gate` 实际 success；[Release Run 42462171776／Job 128673807568](https://github.com/pampa0629/addp/actions/runs/42462171776/job/128673807568) 的 `Run Ontology FalkorDB gate` 实际 success。Falkor 日志含 11 项 runner 单测通过，并实际执行且通过 `TestFalkorIntegration`（投影、取消、连接隔离及清理拒绝）和 `TestPostgresProjectionRuntime` 两个真实图运行用例。

原始回执为 `/tmp/addp-ci-20261108-180349/fix-platform.log`、`fix-falkor.log`。按该固定 SHA 的门禁 EXIT trap，成功退出包含 PostgreSQL 测试资源清理、受管 Compose 容器／网络／卷残留检查；不是把测试名称中的 cleanup 当作独立的宿主 inspect 收据。此次闭环仅指上节共享就绪变更遗漏的确定性夹具及对应两个真实步骤，原 `c1adee620`／`76b5fc0a7` 失败与本地修复前记录保留，不重跑已有有效验收，不据此认定父 workflow、其他 Job 或后续 main 全通过。

本次未操作个人服务、Business MinIO 来源、用户 Monitor 或浏览器。Business MinIO 的配置准备与待用户标准应用状态仍见[本机准备记录](Monitor采集来源与引擎指标历史证据.md#business-minio-本机采集配置与证书准备2026-11-08)，不由此 CI 替代实际 mTLS、采样和页面复验；Standard 的正式身份 Hosted 场景也不在本次通过范围。不设置定时任务。


## 本机服务实例读数、趋势与未绑定／Worker 未采集诊断（2026-11-08）

用户确认继续核对现有 Backend／Worker 的实例、主机绑定和 CPU／RSS。本轮只读检查当前部署及原 Safari 第二个标签页，不修改个人配置、账号或主机台账，不启停服务；CI／Hosted 由专门会话安排，未设置定时任务。首次自动化把 Element Plus Table 的 setupState 当作页面状态，导致空观察；检查组件链后定位到父 ProcessResources，后续只读明确页面状态，未以重载或伪造数据取得结果。

Safari 默认“在线＋已启用采集”展示 System、Manager、Meta、Transfer、Monitor 五个 Backend，均无列表／摘要错误、采集状态 collecting，CPU 核占用、RSS、运行时长有效。`python3 /tmp/addp-service-live-prom-20261108.py` 退出 0：五个实例各有唯一 Prometheus 进程目标，均 UP 且无抓取错误；进程身份指标中的 module、role、instance 与当前用户列表一一对应，原始进程启动时刻与登记的 process_started_at 在微秒内一致，操作系统均为 darwin。实际 RSS 样本距核验时约 1.23—10.86 秒，CPU 当前一分钟各四个样本；该原始样本数单独不替代产品完整窗口判定。收据 `/tmp/addp-service-live-initial-20261108.json`、`/tmp/addp-service-live-prom-20261108.json`。

`python3 /tmp/addp-service-live-trends-20261108.py` 退出 0：通过当前列表各实例的“查看趋势”按钮依次打开原弹窗并正常关闭，核对所选记录 ID、实例身份、趋势主体一致。五个弹窗的页面／弹窗 DOM 可见，两个图表画布均存在，无趋势错误；近五分钟 CPU／RSS 十条曲线各 21／21 点有效，同轮摘要三项均有效。交叉核验当前用户记录与 Prometheus 身份一致。收据 `/tmp/addp-service-live-trends-20261108.json`。本轮未取得新截图，不将 DOM／画布核对外推为新的截图验收、拖动缩放或正式身份 Hosted。

通过原“采集配置”下拉选全部、保留在线条件，正常翻页核对两页共 27 个不同当前实例：21 Backend、5 Worker、1 Ingress。仅五个 Backend 已声明采集；Meta Worker、两类 Transfer Worker、Quality Worker 及清单中的 Security Worker 均未声明。本轮对 Security、Agent、Copilot 只保留正常实例清单事实，不开展其开发或专门验收。两页未出现当前 Backend 启动失败提示；不据此证明未登记进程不存在或全部业务健康。清单收据 `/tmp/addp-service-live-all-current-20261108.json`、`/tmp/addp-service-live-all-current-page2-20261108.json`。

27 个实例的 declared_node_id／node_id 均为空、node_binding_state 为 unbound。主机监控当前仅一项 Docker Desktop Linux VM，未登记原生 Mac；根部署环境未提供 ADDP_HOST_NODE_ID。不能把运行于 darwin 的进程绑定到数据库所在 VM，也不能在线改挂已有实例。当前自身资源观测继续有效，主机关联验收尚未通过。后续须先确认原生宿主登记与允许模块集合，再由标准部署为新实例注入显式主机身份。主机清单收据 `/tmp/addp-service-live-current-hosts-20261108.json`；不从名称或地址猜测关联。

三个已在根采集计划中选择的 Worker 未采集原因已精确复现：配置键为 meta-worker、transfer-bounded-worker、transfer-continuous-worker，而标准 addp_worker_units 即使单副本也发布对应的 -1 启动单元，start_module_workers 原样将单元传给唯一 process-metrics-launch.py。对当前同一计划调用纯 select_deployment，旧键能命中，实际 -1 单元全部返回未选择；规范和 .env.example 已明确具体副本名称，不应新增旧名兼容或猜测映射。Quality Worker 未列入当前计划，另行表达。三个已选 Worker 的现有证书通过共享严格链／用途／IP SAN／密钥预检，未输出或改写凭据、未改证书。收据 `/tmp/addp-service-live-config-selection-20261108.json`、`/tmp/addp-service-live-worker-unit-mismatch-20261108.json`、`/tmp/addp-service-live-worker-cert-check-20261108.json`。已单独请求用户确认仅修正三个本机配置键名；此条记录时尚未修改配置或重启，不能将纯匹配诊断计为 Worker 采集恢复。后续原生主机绑定亦仍未实施。

本轮没有产品代码、API、权限或环境变更，最小验证为上述真实只读脚本、原标准状态／页面路径及同输入纯匹配，未重复无关开发门禁。本次证据只支持五个 Backend 的当前读数／选中短趋势、完整在线清单及配置缺口，不能认定 27 实例资源、Worker、主机绑定、多副本或 CI／Hosted 全部通过。


### 本机三个 Worker 的具体副本采集键修正（2026-11-08）

用户在讨论独立副本、每次启动实例 UUID 与进程内并发的区别后同意先修正三个 Worker 的配置，再接通现有单副本采集。本批仅将忽略版本控制的根 `.env` 中 `ADDP_DEV_PROCESS_METRICS_PLAN` 的三个键改为 `meta-worker-1`、`transfer-bounded-worker-1`、`transfer-continuous-worker-1`。沿用规范和标准启动的唯一具体单元名称，删除旧未编号配置键，不新增兼容读取或备用路线。8 项计划总数、各项五字段值与可选字段保持，未新增 Quality／Security 等其他采集项，也未改变副本数、任务并发、主机关联、账号或权限。

执行 `python3 /tmp/addp-fix-worker-plan-keys-20261108.py` 退出 0。写入前用唯一启动适配器验证全部八项，并对预期环境内容做 Bash 语法检查；写入后重读真实 shell 环境，确认唯一变化的变量为该计划、JSON 恰好只改三个键、其他文件字节及原 inode／模式／UID／GID不变。每个具体单元能够选到自己的单项 SDK 输入，不靠模块／角色猜选其他副本。原 `.env` 与任何凭据未复制到仓库证据或提交；收据 `/tmp/addp-worker-plan-keys-fix-20261108.json` 只记录封闭结果和公开单元名。

`python3 /tmp/addp-worker-plan-preflight-20261108.py` 退出 0：三个现有端点均已包含在原 Monitor CIDR／端口准入和 Desktop 转发列表；原来源证书严格链、用途、IP SAN、密钥及 Monitor 服务器信任检查通过；原监听端口目前无监听者。未变更端点、证书、准入或指标中心，也未使用临时 listener 证明采集。收据 `/tmp/addp-worker-plan-preflight-20261108.json`。沿 `make test-dev-lifecycle` 已有脚本文件执行定向 `python3 -m unittest scripts/test/dev-process-metrics-plan_test.py`，退出 0，15 项现有 T1 通过，日志 `/tmp/addp-worker-plan-tests-20261108.log`；不据此认定完整生命周期门禁或实际来源已通过。纯私有配置键修正不涉及源码、API、标准入口或 CI 登记变化。

当前未启停个人服务；既有 Worker 仍是旧声明，不能计采样恢复，也不能改写原未绑定状态。交接用户在仓库根目录执行标准 `./scripts/dev/restart.sh -meta -transfer`，由新进程载入修正后的计划并生成新实例身份；该命令重启这两个模块及其 Worker。用户完成后继续核对三个真实 Worker 的声明、唯一目标、当前身份、采样新鲜度及 CPU／RSS 趋势。原生 Mac 主机登记和允许模块集合、N 副本扩展及正式 CI／Hosted 分别推进，本批均未实施或触发，没有设置定时任务。


### 用户重启后的三个 Worker 实际采集与 Safari 趋势恢复（2026-11-08）

用户完成标准重启后，本轮只读核对修正键名后的实际进程来源与原 Safari 第二个标签页，不启停个人服务、不修改配置或主机关联。服务监控默认在线／已启用采集列表实际显示 8 项：5 个 Backend 加 Meta、Transfer bounded、Transfer continuous 三个 Worker，均正在采集，CPU 核占用、RSS 和运行时长有读数。UI 没有 Worker 业务 HTTP 监听端点属正常，不代表专业采集端点缺失；来源端点继续保留在私有链路。

执行 `python3 /tmp/addp-worker-restarted-live-20261108.py` 退出 0：当前八项计划各匹配唯一 Prometheus 进程目标，均 UP 且无抓取错误，原 Monitor 客户端严格 mTLS 读取各固定来源均 HTTP 200。来源身份指标的 module、role、runtime_instance_id 与对应目标标签一致，操作系统均为 darwin。三个 Worker 的当前实例为 Meta `e2df4a5c-2a19-4934-916c-f95bd16547f5`、Transfer bounded `acff65de-b75b-416f-a17d-32712cc6fc19`、Transfer continuous `b4dd668b-950f-43d3-bb67-4974bbcc9b5e`，与 Safari 真实列表的实例 UUID 逐项一致。三者 RSS 样本距核验时分别约 7.91、2.92、11.05 秒；各 CPU 原始一分钟窗口四样本且当前累计值不低于一分钟前值，原始计数检查不替代 Monitor 产品完整窗口判定。重启前记录的实例 UUID 均已从当前活动目标消失，未删除或改写历史。脱敏收据 `/tmp/addp-worker-restarted-live-20261108.json`，不持久化原始指标响应或凭据。

原生 `cua_repl` 通过实际 Safari 菜单、表格行“查看趋势”和弹窗刷新按钮完成检查，未执行浏览器 JavaScript、改写可见性或读取认证凭据，第一标签页保持不动。三个弹窗的用途名称和实例 UUID 分别与上述来源匹配，近五分钟 CPU 核占用／RSS 两张曲线都在真实截图中可见，弹窗也实际显示；原生 macOS 的资源组区域保持“未接入资源组采集”，不生成容器限额或组利用率。截图作为本次工具返回原件留在会话中；结构化摘录 `/tmp/addp-worker-restarted-safari-observations-20261108.json` 明确标注为可访问性／截图观察记录，不冒充浏览器 API 响应。

Transfer continuous 弹窗手动刷新前后的窗口结束时刻为 `23:00:24` 和 `23:00:33`，后续无手动刷新时推进到 `23:00:49`；同一实例与 CPU／RSS 曲线保持。Meta 手动刷新从 `23:01:04` 推进至 `23:01:14`，后续截图继续显示正确实例及两张曲线。当前页面显示自动刷新开启。未读取本轮产品趋势点数组，因此不声称全部 21 点有效，也未重新验收缩放、环比、故障恢复或多副本任务竞争。

本机这三个 Worker 的单副本采集恢复已实际验证；当前八项实例仍显示未关联主机，原生 Mac 登记／显式绑定并未由此次重启补齐，不能借用 Docker Desktop VM 身份。Quality 不在这次采集计划中，Security、Agent、Copilot未展开；N 副本能力、正式身份 Hosted 与生产仍分别推进。本轮没有产品代码、API、权限或环境变更，最小验证为上述真实只读脚本、原生界面及截图和跨层实例 UUID 对照，不重复既有纯配置 T1；CI／Hosted仍等专门会话回传，没有设置定时任务。


### Gateway／Service／Quality／Standard 五项采集部署准备（2026-11-08）

用户确认优先接通 Gateway、Service、Quality Backend／Worker 和 Standard。只读核对各入口已沿共享 System 客户端的 RegisterAndHeartbeat 启动可选 processmetrics，Quality Worker 使用正式 worker 角色；标准开发入口均由唯一 process-metrics-launch.py 注入对应具体单元，Backend／Gateway 的 HTTP 采集沿共享路由内核。无需新建注册表、手工服务清单或逐模块复制采集代码。本批不实施主机登记／绑定、N 副本、Security Worker 或 Agent／Copilot。

本机新增五项明确的回环来源：gateway（ingress）19115、service（backend）19116、quality（backend）19117、quality-worker-1（worker）19118、standard（backend）19119。准备前五端口均无监听，对应证书目录不存在；Quality 副本配置未设置，沿既有默认单副本。各来源分别在仓库外 process-<unit> 目录保存三文件，目录 0700、文件 0600；沿用原 source CA 和采集客户端身份，各自新建服务器私钥及 30 天叶证书，包含明确回环 IP 与 host.docker.internal SAN、serverAuth、AKI／SKI及必要约束，不修改现有 CA、账号、私钥或其他来源证书。

第一轮临时准备已完成证书签发及验证，向外部目录发布五套材料后，在写根环境前的“完整 stat_result 相等”断言失败。根 `.env` 尚未写入，新来源未启动；随后只读核对仍为原八项计划、五新端口未入准入、五目录各三文件。该次元数据差异的确切原因未确定，不归因为 Docker、并发修改或访问时间。没有重新签发或覆盖这些材料；后续直接复验已准备材料，按本次实际需求核对文件身份／权限及原内容，完成限定三变量写入。临时脚本 `/tmp/addp-prepare-next-process-sources-20261108.py` 的失败保留，不计整脚本通过。

`python3 /tmp/addp-finish-next-process-config-20261108.py` 退出 0：现有八项计划保持原值，追加五项后共十三项；根环境只改变 ADDP_DEV_PROCESS_METRICS_PLAN、MONITOR_METRICS_ALLOWED_PORTS 和 ADDP_METRICS_DESKTOP_LOOPBACK_PORTS。十三项具体单元各自能选中自身单项 SDK 输入；五端口仅追加到既有准入／转发列表。严格证书链、IP SAN、DNS SAN、服务器用途、公钥配对及原采集客户端用途／信任检查通过，Bash 语法通过。根环境保留 inode／模式／UID／GID，不保存或输出原环境或私钥；`.env` 忽略版本控制。脱敏收据 `/tmp/addp-next-process-sources-prepared-20261108.json`。

独立只读预检调用既有桌面转发渲染器（未调用会写文件的 render），确认当前 Docker Desktop 本地传输及五条新转发各恰一次，十三项计划匹配通过，未写实际 Prometheus 生成配置。收据 `/tmp/addp-next-process-sources-preflight-20261108.json`。`python3 -m unittest scripts/test/dev-process-metrics-plan_test.py` 退出 0，15 项 T1 通过；`make test-monitor-metrics-config` 完整退出 0，含共享来源前置回归及 139 项配置测试，日志分别 `/tmp/addp-next-process-plan-tests-20261108.log`、`/tmp/addp-next-process-metrics-config-20261108.log`。沿既有 Make／Platform 自动发现和生命周期输入登记，无产品或编排文件变化，不新增测试路线。配置门禁中的受控夹具诊断不计个人真实来源或 T4。

尚未应用或重启。交用户在根目录依次执行 `bash scripts/infra/up.sh --metrics` 和 `./scripts/dev/restart.sh -monitor -gateway -service -quality -standard`：前者加载新增转发，并重新应用既有已选 Infra 来源；后者加载 Monitor 新准入和五个应用来源（Quality 模块含 Worker）。标准重启会短暂停止所选模块，不删除业务数据或指标历史；本会话未代执行。用户完成后核对新增五项声明、唯一 UP 目标、当前实例、严格 mTLS、新鲜读数及真实页面，再逐步接通接口质量。本批只计部署准备及本地预检，不能认定十三来源实际采样已验；CI／Hosted由专门会话安排，无定时任务。

### 本机指标来源预算扩容（2026-11-09）

重启后相关业务进程就绪，五个新增指标端点通过真实 mTLS。Monitor 的发现接口返回 `422 obs_budget_exceeded`，Prometheus 当时只采集自身。System 当前投影有 13 个进程声明、6 个可用引擎，加上已配置的主机来源共 20 个。逐端点读取共 2,371 个样本。12 的旧上限按每源和中心各 20,000 预留，触发的是部署预算校验。

根环境统一以 `ADDP_METRICS_TARGET_LIMIT` 配置 Monitor 与 Prometheus。缺省 12，本机预检以 12 拒绝计划中的 20 项，改用 24 通过。开发启动和重启在应用前校验，指标专用应用在调用 Docker 前拒绝超额。24 项预算按既有单来源限制推导为 500,000；此预留不等同于已持续压测的实际样本量。

`make test-module MODULE=monitor GO_ONLY=1`、`make test-monitor-metrics-config`、`make test-monitor-postgres` 和 Infra 生命周期回归通过。隔离 T2 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=target-budget` 核验 24 个不同 mTLS 端点各有连续样本，加入第 25 个时作业超限，返回 24 个后恢复，容器、网络、卷和临时文件清零。首轮在 24 项抓取后因测试误匹配 Prometheus 的错误文本而失败，修正断言后完整通过。隔离来源是受控夹具，结果不等于 500,000 样本压力测试。

本机根环境已设 24，现有 20 项来源计划保留。等待用户通过标准入口应用指标中心并重启 Monitor，随后核验真实目标及新鲜样本。

### 34 个采集目标的隔离验证与本机配置（2026-11-10）

用户确认保留已有来源，隔离通过后再为十个后端追加配置。既有 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=target-budget` 在独立卷、网络及 mTLS 凭据内执行 24→34→35→34。固定中心为 1 CPU／2 GiB，15 秒采样；一台主机、六个引擎、五个 Worker 及一个网关与受控后端一起采样。每个 HTTP 来源重放 32 个路由、两种状态及固定直方图桶。每档预热后以四个并发 PromQL 请求执行五轮，覆盖 QPS、失败比例、P95 和压力趋势。测试负载为受控计数器重放，产品身份和真实服务流量未进入此次容量测量。

| 外部目标 | 活跃时序 | 样本/秒 | 容器内存峰值 | 约 80 秒卷增长 | 最慢抓取／查询 | 七天样本估算 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 24 | 14,856 | 990.02 | 142.1 MiB | 352 KiB | 19.7／39.4 ms | 1.12 GiB |
| 34 | 25,816 | 1,720.55 | 142.1 MiB | 608 KiB | 14.4／52.9 ms | 1.94 GiB |

内存为同一隔离容器的高水位。七天按 2 字节/样本估算时序样本体积，实际磁盘另受索引、WAL、压缩和时序变化影响。第 35 个触发抓取作业超限；恢复为 34 个后重新取得连续样本，容器、网络、卷和临时目录清零。第一轮因 P95 窗口早于流量增长失败，第二轮因写入速率读数未合并多条序列失败；修正预热和速率聚合后第三轮退出 0。原日志依次为 `/tmp/addp-metrics-34.YiHcfy`、`/tmp/addp-metrics-34.seA0Uw`、`/tmp/addp-metrics-34.wXH9VB`。Monitor Go T1、指标配置、T2 CI 登记及 HTTP／总览 20 个受控浏览器用例分别通过，浏览器用例独立于压力窗口。

本机 `.env` 已保留原 17 个进程配置及所有引擎和主机来源，增加 Agent、Copilot、Develop、Graph、Inference、Model、Ontology、Orchestrator、Portal、Workbench 的独立回环端点、准入及 Docker Desktop 转发，预算设为 34。现有 CA 不变，新端点使用独立叶证书。Agent/Copilot 沿既有 Python HTTP 及进程启动时间范围。指标中心与相应进程的配置生效及 34 个真实目标的新鲜样本待本机标准重启后核验。

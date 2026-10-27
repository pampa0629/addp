# Monitor 实例与服务发现历史证据

[返回 Monitor 历史证据](Monitor历史证据.md) · [返回验收证据索引](../验收证据索引.md)

本专题收录多 Worker、实例筛选、cgroup 配额、部署预期与发现生命周期。固定源码、环境、失败记录、修复结论与未覆盖范围以各记录为准；本次仅整理文档，没有重跑验收或刷新 CI 状态，不推导当前 HEAD 或生产 T5 已通过。跨主题语境与原有顺序见 [Monitor 历史证据入口](Monitor历史证据.md#原记录入口)。

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

原因控件、枚举规范化和中英文译名统一到 Common Frontend 的 `RuntimeInstanceStopReasonSelect`／`runtimeInstanceStopReason.mjs`；System 原内联选择器和枚举删除，模块概览、实例列表与日志诊断同步消费共享译名。System 仍选择立即查询，Monitor 仍搜索提交；没有 API/Swagger、权限、后端状态裁决或部署变化。现有 Common／System／Monitor／Console 前端门禁及自动路径登记覆盖，不新增 Make、workflow 或测试路线。

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


## 服务实例 CPU/RSS 趋势的独立时间缩放（2026-10-23）

服务趋势弹窗的 CPU、RSS 图分别接入既有共享 ChartRenderer 时间滑块，图旁提供中英文操作提示；两个临时视口独立，不裁剪完整响应、不新增请求或 URL 参数。相同已提交范围的手动／自动刷新保留相对视口；关闭弹窗、切换实例／时段／筛选或身份变化时重置，错误及撤权清除旧图与视口。指标、当前运行实例历史边界、权限及生产采集均未改变。

冻结基线 `10bfdbdf194ebef14afef40cb4f7db4b1d3a689e` 加本批七项输入，使用 Node 24.19.0、既有依赖和标准隔离入口执行：

- `npm run test:e2e -- process-trend-zoom.spec.js`：7/7 通过，见 `zoom-browser.log`。中英文窄屏真实拖动分别缩放两图；完整样本／真实零／缺样及陈旧断点保留，无额外请求／URL 变化；悬停、图例显隐、主题重绘、手动／自动刷新保留，时段／实例／关闭再打开／身份变化重置，503、403 和错误运行身份的清除与恢复均通过。
- `make test-monitor-frontend`：退出 0，101 项单元测试、346 项浏览器测试及生产构建通过，见 `frontend-full.log`；上述 7 项包含在 346 项内，不累加为唯一场景数量。完整门禁同时覆盖复用多图定位参数后的主机缩放、图例／悬停及既有进程页面布局／筛选，未改共享渲染器实现。构建保留原有大包提示，不将提示计为失败或性能验收。

证据目录 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-process-trend-zoom-p2lmhr_b/` 保存原始日志、`inputs.json`、`frontend-inputs-sha256.json` 及原表只读行号引用。浏览器使用独立端口和受控 API，成功后由标准入口清理运行目录；不操作 Safari、个人登录态或服务，不重启后端。现有 Monitor 前端标准门禁及 Platform CI 矩阵自动发现新 spec，无新增依赖、入口或 workflow。本轮未查询或触发 CI／Hosted，真实页面验收等专门会话回传，不设置定时。此项前端通过不替代上一批 CPU/RSS 告警真实触发／恢复 T2 或正式身份验收。


二次本地窗口（2026-10-23 17:11:14–18:11:14 Asia/Shanghai，用户重新授权）：保留 8 CPU／12 GiB、4 GiB 停止线及既有正式身份范围。第一次实测由 `measure_owned_volumes` 资源采样超时停止，最后成功样本 3,236,450,304 字节，44 项成功样本；自动报告明确失败、清理通过。按已记录子步骤调查，原采样把 Docker 容器创建／启动／退出与 du 放在同一四秒预算。只调整为当次只读辅助容器复用，卷集合变化才重建，采样串行且退出先释放 BuildKit 卷挂载；保留 0.25 CPU／64 MiB、无网络、只读卷及原采样／停止边界，辅助容器 /data 用受限 tmpfs，不产生额外匿名卷。代码 `97714a1ac68624ecaf56573051bb9febcaf2075b`；最终 `make test-infra-runtime-log-lifecycle` 退出 0，42 项入口契约、JS 语法／登记／System ExecutionTrace Go及原60项生命周期通过。

同窗口第二次实际冷构建记录 56 项采样，最长测量 1.139 秒、最大相邻采样完成间隔约4.27秒，约四分钟构建观察未再超时；随后在 product_minio 正确触发 `owned_volume_stop_line_reached`。完整触线样本 4,362,616,832 字节，超过停止线 67,649,536 字节（约64.52 MiB），末次测量0.902秒，运行硬配额3.25 CPU／5.0625 GiB；没有放宽预算或把触线记录作通过。两次均未创建业务部署、未准备正式身份或执行查询，Trace／日志／Console闭环未执行，清理均通过。安全报告／源码哈希／窗口及独立清理回执见 `/tmp/addp-p19-formal-local-20261023-window2/{attempt-1-report.json,attempt-2-report.json,attempt-2-source.json,receipt.json,lifecycle-final.log}`。

当前缺口是既有 MinIO 基础依赖的冷构建已达到本地准备预算。本会话提出仅在核验固定 MinIO／mc 源提交及镜像ID后复用既有产品镜像，本专项后端／前端仍按当前已提交源码构建；该依赖准备范围调整正在等待用户明确确认，尚未实施，不凭已有缓存降低原验证口径，也不续开或延长本窗口。不运行 Hosted、容量或定时，不接管个人服务。


## 服务进程 CPU／RSS 相邻时段比较（本地）

2026-10-23，沿用户确认的均值与原单位变化量口径，当前运行实例支持 5 分钟／1 小时／24 小时与上一等长时段比较。CPU 用核、RSS 用 IEC 容量，按未舍入评估点均值计算本期减上期；零基线有效，不给增长百分比。两期任一点缺样、陈旧或启动裁剪时不出均值／差值，各指标独立显示有效／预期点数；CPU 启动保护沿后端曲线评估时刻，抓取新鲜度独立核验。结果和时间就近展示，图中仅显示本期；缩放不重算比较。主机与服务共用 `common-frontend/basic/src/utils/evaluationPeriodComparison.mjs` 及 Monitor 唯一 `ResourcePeriodComparison.vue`，旧主机均值循环与重复卡片已删除。

冻结输入基线 `fb3d25f4b52324273d1fdcc3cb27c30df695b569`，仅叠加本批文件；证据目录 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-process-comparison-s_1g44u9`，`inputs.json` 记录本批最终文件，`frontend-full-inputs-sha256.json` 与 `frontend-inputs-sha256.json` 分别记录完整门禁及最终针对性回归的 435 项输入。交付前与共享工作区核验无前端输入漂移。实际命令均使用 Node v24.19.0：

- `make test-common-frontend`：272 项通过；共享均值、分界点归属、缺样、启动裁剪网格及唯一消费路径由确定性测试覆盖。
- `make test-monitor-frontend`：首次完整版本 109 项 T1、361 项浏览器 T3 及构建通过，见 `monitor-frontend-full.log`。此后仅修正比较函数内 CPU 预热判断的时间依据，并新增对应边界测试，没有把这份完整门禁记成最终源码重新全跑。
- 最终 `npm --prefix monitor/frontend test`：110 项 T1 通过；`npm --prefix monitor/frontend run test:e2e -- process-comparison.spec.js resource-comparison.spec.js`：33 项 T3 通过（15 项服务比较及 18 项主机比较回归）；`npm --prefix monitor/frontend run build`：通过。见 `final-monitor-unit.log`、`final-comparison-browser.log`、`final-build.log`。新增用例在独立副本恢复旧抓取时间判断时失败（`startup-regression-red.log`），最终评估时间判断通过。该末次修改只影响启用服务比较的分支，最终定向回归覆盖此分支，其余未变分支沿用前述完整门禁有效证据，不重复累计测试项。
- `make test-frontend-ci-registration`：20 项检查通过，21 个前端登记检查通过；共享／Monitor 现有自动发现和 Platform 前端编排覆盖本批，不新增 Makefile 或 workflow 路径。受影响的是新增共享纯计算的直接消费者 Monitor，未改动其他模块的导出或消费行为。

最终 T3 使用 HTTP 协议夹具与真实共享图表，覆盖两种语言／窄屏、原单位升降及零值、两期缺样、启动裁剪、48 小时查询的自适应步长、7 天拒绝比较、URL／刷新／关闭、身份及错误清除、缩放不重算比较；复制保存并人工检查了 `screenshots/` 下中英文卡片截图。未连接个人库或重启个人服务，未运行本批正式身份真实数据／长窗 T4 或 Hosted，CI／Hosted 等专门会话回传；此结果不覆盖尚未通过的 CPU／RSS 告警实源触发恢复 T2，不证明同比或长期留存已实现。原表第 18 行（B 列 17）仍为部分实现。


MinIO 产品镜像核验复用入口准备（2026-10-23，原窗口结束后）：用户确认复用已有固定版本 MinIO 产品镜像，先核验 MinIO／mc 源提交与镜像 ID，本专项代码继续标准源码构建。收到确认时北京时间18:45，原18:11:14截止时间已过，因此没有启动新容器，也没有执行真实镜像二进制核验、业务部署或正式身份闭环。先修运行观测规范，再将唯一入口改为从当前提交的 scripts/infra/Dockerfile.minio 提取两组件固定发布版本／40位源码提交；已有镜像必须有不可变sha256 ID和Linux ARM64身份，两个只读／无网络／0.25CPU／256MiB检查逐一以该ID运行--version，核对发布版本及嵌入提交。通过的同一ID用于卷测量、真实MinIO及两个存储初始化步骤；缺失、不匹配、命令失败直接拒绝，不拉取替代品或回退冷构建。旧MinIO冷构建及其专用故障分类分支已删除。镜像版本与嵌入提交校验不等同于重新源码编译／字节级可复现证明。

代码及规范提交5681227a0，已推送main。标准 make test-infra-runtime-log-lifecycle 退出0：46项当时的入口契约、两份JS语法、完整CI登记、System ExecutionTrace定向Go及既有60项生命周期通过；随后仅加强mc不匹配／缺失镜像用例，最终 make test-monitor-traces-local-runner 退出0，47项入口契约及其余定向检查通过，60项未改输入复用当次有效结果。没有新增测试入口或依赖，现有Platform生命周期及路径登记仍覆盖；未查询或手工调度CI。安全输入摘要与日志见 /tmp/addp-p19-minio-reuse-preparation-20261023/{receipt.json,minio-reuse-lifecycle.log,minio-reuse-final-tests.log}，明确标注为窗口外的确定性准备，actual_minio_binary_verification与formal_identity_closure均为not_run。

当前已具备核验后复用的单一路径；真实MinIO／mc核验及正式密码/MFA、执行、Trace／日志／页面闭环均未运行、未计通过。需新的同上限本地窗口方可继续实际运行；不自动续开、不运行Hosted、容量或定时，不接管个人服务。


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


## 持续部署发现 Linux 退出失败调查：仅补诊断，根因未证实（2026-10-25）

专门CI会话回传后，只读核对 [Platform Run40150409107／Job121040604338](https://github.com/pampa0629/addp/actions/runs/40150409107/job/121040604338) 原始日志的checkout与实际失败步骤，源码为 `994db320ae7147cb0e566db4960eb256306cc9e1`。`make test-platform → test-dev-lifecycle → test-infra-runtime-log-lifecycle` 的61项中，`test_logs_failure_does_not_prevent_selected_metrics_start` 在 `addCleanup/stop_discovery_observer` 收到“部署发现进程尚未退出，不删除运行记录”；Job实际失败，后续摘要写入成功不能覆盖。日志没有当时的进程状态／等待／信号／子进程证据，不能断言是僵尸、锁、Docker子进程或预算不足，更没有证据把它归因于新增告警 `host_name` 逻辑。

本机基线 `make test-deployment-discovery` 的34项发现＋5项容器证明，以及 `make test-infra-runtime-log-lifecycle` 的79项前置、61项生命周期、登记／Go夹具均通过，未复现原Linux退出失败；macOS通过不能替代该Linux问题的根因证据。没有对同一未变输入反复加压运行、放宽原100次、50ms间隔的退出观察循环、增加信号／sleep／重试、强删记录或跳过断言。

本批只在现有“无法确认退出”失败分支追加即时有界诊断。Linux从同UID普通proc文件读取状态、固定等待类型、SIGTERM挂起／屏蔽／忽略／处理位及直接子进程数量；每个文件最多8192字节，拒绝符号链接／非UTF8／超量，读取失败明确未知，其他平台明确不可用。只输出固定键、枚举、布尔及数量，不输出命令行、路径、PID清单、任意wait文本或随机运行标识。字段依据为 [Linux proc状态手册](https://man7.org/linux/man-pages/man5/proc_pid_status.5.html)及[内核proc文档](https://docs.kernel.org/filesystems/proc.html)；瞬时诊断不参与归属或清理裁决。原SIGTERM只发一次、foreign拒绝、确认退出前保留记录、退出后由控制流程清空来源等主路径不变，无新配置、接口或权限。

先补三项诊断回归，标准发现入口因缺失诊断函数产生3个错误，保留红灯日志；实现后再补真实文件读取的大小／编码／符号链接拒绝，最终 `make test-deployment-discovery` 为38项发现＋5项容器证明全部通过。`make test-dev-lifecycle` 最终退出0，132项原生生命周期、79项前置、61项Infra生命周期、Online登记／Go夹具、端口／缓存／锁／原子构建及现有共享包检查通过；末尾Worker命令的“无测试”仅计编译覆盖，不外推为Worker功能验收。标准影响选择继续命中Platform；原Make与CI登记已覆盖，无新增入口或workflow。`git diff --check`通过。

原始失败 `/tmp/addp-ci-20261025-013034/job-121040604338.log`；本机 `/tmp/addp-discovery-exit-{baseline,diagnostics-red,diagnostics-final,dev-final}.log`、`/tmp/addp-discovery-infra-baseline.log`及`/tmp/addp-discovery-exit-selection.txt`。本批不宣称原CI退出故障已修复，Linux实测与新诊断尚待专门CI会话回传；没有轮询、手工派发或重跑CI／Hosted，也没有操作个人服务、keepalive、数据库或数据卷，不设置定时任务。已只读核对功能清单第28／30／34行的主机管理内容，本次诊断不等于新增主机注册、远程初始化或批量运维，不据此升级功能完成程度。

## 持续部署发现固定 05b Linux 门禁通过：原间歇失败仍未闭环（2026-10-25）

专门CI会话回传后，只读独立核对落地原始日志、Job及实际步骤终态。[Platform Run40151852077／Job121044843183](https://github.com/pampa0629/addp/actions/runs/40151852077/job/121044843183) 为push／attempt1、固定源码 `05b232920cd1801fcbb409a392aa6386f73efe26`；实际 `Run platform consistency gate` 从01:48:44至02:11:09 UTC完成success，Job亦success，后续平台／Swagger检查实际走完，非仅摘要成功。原失败路径的Infra运行日志／Trace／PostgreSQL metrics生命周期标准聚合61项于01:51:17记录OK（19.245秒）；后续部署发现38项及容器证明5项也实际OK。不将其他同数测试组或初始MinIO缓存用例误记为该组，不把同Job其他既有跳过项算作运行验收通过。

本轮只证明退出诊断增强版本在该Linux标准门禁通过，没有重现994原轮的 `stop_discovery_observer` 失败，也没有取得失败时的新退出诊断；**原间歇故障根因仍未证实，不能认定已修复或注销**。保留原失败日志、归属检查、信号次数、观察循环及未确认退出不删记录的边界，不为采样反复触发或扩大预算；后续已有正常push若复发，继续由专门CI会话取回有界诊断再定位。不据此提升主机注册、远程运维、主机名称筛选Safari／Hosted或生产完成程度。

证据目录 `/tmp/addp-ci-20261025-023003/`：原始 `job-121044843183.log` SHA-256 `323e6228ec32acda74e8ea701ee4a611e1ec6473f929e39a391d4f33ba981e1e`与协调审计一致，`jobs-40151852077.json`确认准确head_sha及实际步骤success；协调审计为 `Observer05b-actual-Linux-Platform-diagnostic-audit.json`，本会话新收据 `Observer05b-owner-independent-audit.json`。本轮仅补证据文档并执行 `git diff --check`；没有轮询、派发或重跑CI／Hosted、操作个人服务或设置定时任务。

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

剩余：标准启停Hook尚未接通；停止时计划停用发布失败的处理待用户选择。首次登记预热／触发及恢复窗口、失联规则权限、Monitor评估／事件／通知与真实页面均未实现或未验收，不把基础声明或零在线计数记为服务失联自动告警已完成。原清单第15、49行仍部分实现。


## 2026-10-25 标准启停接通服务组预期与停止前发布屏障

用户明确确认：计划停用发布成功后才允许停止；发布失败立即报错，保留服务运行。本批在上一节协议基座上接通开发与生产入口。开发从当前已解析的后端选择与正式编号Worker单元发布，Meta双副本、Transfer两用途、Quality用途保持独立；Security Worker不在新增范围。生产仅渲染当前Compose声明，使用Profile和规范化scale／deploy.replicas，不读取存活容器推断预期；零副本、未选Profile及完整计划不再选择的已有组置为计划停用，局部开发启动不改变其他组。渲染凭据不输出或持久化到声明及覆盖文件。

开发／生产停止在首个launchd、进程、容器或缓存清理动作前写入计划停用；根.env与原启动入口使用同一目录配置，失败不提供旁路参数。生产沿既有工作区生命周期锁串行，System只读目录挂载由服务声明生成器唯一拥有，移除进程指标生成器中的重复路径；指标未选中也能发布预期。启动发布失败报告可选观测错误，核心业务仍继续，不能把旧声明当作新启动成功证据。文件发布只表达意图，不证明实际启停成功或告警恢复。

本地实际验证：

- `python3 -m unittest scripts/test/metrics-deployment-config_test.py`：84项通过，含原74项及10项新增。覆盖标准开发／生产停止失败时无停止命令、PID记录保留、锁释放；局部停止只改所选模块；首次listener／Compose down前核对已发布计划停用；真实Docker Compose CLI渲染Profile及副本数（没有启动容器）；生产标准启动阶段仅成功发布后引入只读覆盖文件，失败不复用旧覆盖；指标关闭仍发布；非法来源／超量／渲染失败不回显秘密、不覆盖旧意图；原SDK生成器不再依赖或拥有发现挂载。
- `make test-dev-lifecycle`：最终退出0，161项聚合、99项运行日志生命周期、61项回归、端口／所有权／全量及局部生命周期、共享Schema／projectionstore与相关Worker命令编译通过。初轮唯一失败是Raster受控停止夹具未复制新增发布器依赖；仅补齐测试依赖后重验通过，没有放宽停止屏障或修改Raster产品。
- `python3 scripts/ci/check-t2-ci-registration.py --repository "$PWD"`：22／12／9／2门禁登记通过。既有Metrics输入增加开发／生产停止及共享生命周期锁；根Make原生命周期入口增加生产停止脚本语法检查，新增测试由现有配置测试聚合自动发现，不新增门禁路线。`changed-gate.py --dry-run`只计发现，不计其他Owner测试通过。

本轮没有Go产品或HTTP契约变更，沿用上一节System真实PG T2与Swagger证据，不重复充当本批运行验收。没有运行个人标准启停、操作个人数据库／IAM、触发CI或Hosted，也未设置定时任务。全量平台T0前轮已在无关Security MySQL Online v27登记一致性处失败，本批没有重跑或接管；本批上述相关独立门禁取得通过终态。

日志为 `/tmp/addp-service-intent-hooks-{config-final,lifecycle-final,registration-final,selection}.log`。尚未验证个人实际启停后完整的部署发布→System→Monitor正式身份链路或真实生产部署；当前仍没有服务组失联规则、连续窗口、事件与通知闭环。首次登记前的处理、触发／恢复时长及规则权限尚待明确；清单第15／49行保持部分实现。


## 2026-10-25 确认运行前置：组预期变更标记消歧

用户确认首期只覆盖确认运行后的失联；首次尚未确认运行的组保持等待，首次启动失败另待部署结果事实。本次调查确认System的registered_count包含历史登记，不能用于冒充当前有效在线确认；Monitor尚未实现该资格的持久化或失联评估。独立规则四项权限（仅平台系统管理员）、15—300秒显式持续时间及建议60／30秒、warning级别、按本组预期重新确认及新类别显式订阅的完整提案已写入运行监控设计末节并向用户提问，未发布权限或实现上述未确认规则。

独立修复部署发布器：原changed_at仅到秒，同秒停用、重启后会复用先前标记，无法可靠隔离前后运行确认。改用真实UTC微秒，组内容或运行意图实质变化必须严格晚于该组原值；时间不前进或回退即拒绝发布、保留旧文件，不合成未来值。相同预期仍幂等，不相关组保持原标记。仍为同一文件、既有字段和发布路线，无API、私有投影字段、权限、IAM或数据库变更。

验证：新增三项回归先在原实现中复现失败（五处断言，87项全体退出1）；修复后 `python3 -m unittest scripts/test/metrics-deployment-config_test.py` 87项退出0。同秒停用／重启／缩容逐次区分；时钟停滞／回退时启动修改、计划停用及完整计划退役均不写入，幂等重发仍成功，临时文件零残留；其他组标记不受影响。`make test-dev-lifecycle`最终退出0（164／112／61项聚合、局部生命周期、共享包及Worker命令编译）；T2登记22／12／9／2通过。新用例沿原配置聚合与Metrics输入自动发现，无需改Make或workflow。日志 `/tmp/addp-service-intent-confirmation-{red,green,lifecycle,registration}.log`，收据同前缀 `-receipt.json`。

本批不重跑既有无关全量T0失败，不计其通过；没有查询／触发CI或Hosted、重启个人服务、修改个人数据库或设置定时任务。仍未验收个人真实启停发布→System→Monitor链路，也未实现服务组确认运行的持久状态、规则管理或告警／通知闭环；原第15／49行保持部分实现，不能把前置标记修复计为失联告警完成。

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

日志：`/tmp/addp-discovery-{finalizer-red,finalizer-green,stop-current,stop-lifecycle-green,stop-metrics-green,stop-infra-green,stop-linux,stop-linux-green}.log`。新用例沿原部署发现标准入口自动发现，相关Python脚本已在原Metrics T2输入清单，Platform沿原标准聚合；不新增Make／workflow或并行技术路线。未自行查询／触发／重跑CI或Hosted、未设置定时任务、未执行个人服务或keepalive生命周期；自然Linux CI等专门会话回传。Safari实际可见性及真实服务失联验收仍保持原缺口。


## 原生Linux回传与迁移选择断言同步（2026-10-26）

专门CI会话回传后，本会话只读核对其提供的原始日志：Platform run `40214288480` / job `121231972455`，checkout固定为`2a2afdf482aabcea3ab6dadc2f8aea01c6560f3a`；日志2191—2195行的61项Infra生命周期、2335—2344行的40项部署发现及6项证明均Ran/OK。相关停止／挂载证明门禁已取得该固定源码的原生Linux通过证据，不代替当前HEAD、完整Platform或正式运行验收；原本地Dev聚合两项超时仍保留为失败。

该Job最终失败在 `test-system-iam-runner`：旧精确选择断言未纳入标准入口已选择的`PlatformServiceAvailabilityRulesForwardMigration`。本地先用 `make test-system-iam-runner`复现30项中同一失败，再仅同步该测试：精确比较实际迁移选择器，核对新增失联迁移、原告警／HTTP／Trace及Runner五个测试都命中，无关迁移、伪发布名、前后缀扩展及缺少后缀不命中；原错误package选择拒绝保持。没有修改迁移入口、SQL、权限、真实数据库或产品。

修复后同一标准入口30项退出0，`git diff --check`通过；沿原Make／Platform T0自动发现，无需新增登记。日志 `/tmp/addp-service-availability-selector-{red,green}.log`，原CI证据 `/tmp/addp-ci-20261025-180006/job-121231972455.log`，Job为 https://github.com/pampa0629/addp/actions/runs/40214288480/job/121231972455 。未将原失败改写为整轮通过，未重跑已有有效实库／Hosted，未查询或重跑远程CI、设置定时任务或操作个人服务；修复后的自然CI等专门会话回传。

后续专门CI会话回传，本会话只读复核原始日志 `/tmp/addp-ci-20261025-200006/job-121248639865.log`：固定源码 `1fb051a50e7c46975411542a0e9340f7fcb97929` 的 [Platform consistency](https://github.com/pampa0629/addp/actions/runs/40219785488/job/121248639865) 实际执行到末尾，`ADDP_CI_SELECTED=true`、`ADDP_CI_STATUS=success`；其中停止流程相关61项及迁移runner契约30项均Ran/OK。停止流程、发现挂载证明及精确迁移选择三项修复已具备该固定源码的Linux标准T0证据。旧失败与本地Dev聚合超时保留；本结论仅覆盖该Job，不代表整个workflow、当前共享HEAD、Safari弹窗与规则写入、真实失联触发／恢复／通知或Hosted通过。本次仅归档回传证据，未触发／重跑／轮询CI或操作个人服务，清单相关功能仍部分实现。

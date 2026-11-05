# Monitor 告警与规则历史证据

[返回 Monitor 历史证据](Monitor历史证据.md) · [返回验收证据索引](../验收证据索引.md)

本专题收录CPU/RSS、告警检索与排序、服务失联、文件系统容量规则。验收结论按各记录的固定源码、环境、运行尝试和层级认定。原顺序见[Monitor 历史证据入口](Monitor历史证据.md#原记录入口)。

原始过程、逐步诊断及详细归档清单见[精简前的固定记录](https://github.com/pampa0629/addp/blob/be8f8daa4d38a7830585577f03b77bf2b5ed3931/docs/spec/testing/evidence/Monitor%E5%91%8A%E8%AD%A6%E4%B8%8E%E8%A7%84%E5%88%99%E5%8E%86%E5%8F%B2%E8%AF%81%E6%8D%AE.md)。

逐次运行、原始摘要和回执清单见[本轮整理前的固定快照 `90d13c254`](https://github.com/pampa0629/addp/blob/90d13c2545c602eddcc17a81621a135ab64fa540/docs/spec/testing/evidence/Monitor%E5%91%8A%E8%AD%A6%E4%B8%8E%E8%A7%84%E5%88%99%E5%8E%86%E5%8F%B2%E8%AF%81%E6%8D%AE.md)。

[名称检索与前端 CI](#alert-evidence-name) · [服务组失联](#alert-evidence-availability) · [文件系统容量告警](#alert-evidence-filesystem) · [服务实例 CPU／RSS 告警](#alert-evidence-process) · [其他筛选、排序与刷新](#alert-evidence-navigation)

<a id="alert-evidence-name"></a>

## 名称检索与前端 CI

触发时名称、PG 实源、受控前端 CI 和个人 Safari 各自记录。取消原因以原始 check-run 注释为准，后继固定版本通过不改写首败。

### 平台告警名称检索与触发时名称证据（2026-10-24—25，本地）

规则告警检索使用 opening 证据中的首次触发 `rule_name`，后续评估／恢复与规则改名或删除均不回填；旧证据缺名时显示“未记录名称”。日志告警按固定 signal 和当前语言派生，名称与异常原因分列。`GET /platform/alerts?search` 为最多 100 Unicode 字符的去首尾空白、大小写不敏感字面包含；通配符按字面，非法 UTF-8、控制字符、重复或超长拒绝。与已提交筛选取交集后计数／分页，刷新及通知往返保留 URL。

| 层级 | 本地结果与覆盖 |
| --- | --- |
| Go／PG | 七包 Go、38 项顶层 PG（名称 16 子场景）通过。三评估器触发／改名／恢复、删除后历史、旧证据不回填及查询夹具零残留均验证。首轮 Go 两处旧夹具返回类型失败，修正后通过。 |
| Monitor 前端 | 首轮 422／424，名称草稿被已提交值还原，逐字键入定向亦失败；独立草稿及路由／身份同步修正后，117 单元、424 受控浏览器及构建通过。 |
| Console／Swagger | Console 195 单元／219 浏览器及构建先通过；最终草稿修复输入只再验五项 iframe 专项。Swagger 91 方法通过。 |

原件 `/tmp/addp-alert-name-*.log` 和 `/tmp/addp-alert-name-inputs.json`（30 项）。本地 PG 使用 `addp_test`，前端为受控 HTTP；固定 CI、个人 Safari、正式身份及 Hosted 各自认定。功能清单第 51 行仍部分实现。

### 名称检索固定提交 CI：PG通过，前端浏览器被取消（2026-10-25）

同一固定源码 `a6048b9e71b0689893a3cf90db5a0ef4822d8866` 的两条实际 Job 分列。

| 实际 Job | 结果 |
| --- | --- |
| [Release/T2 Run 40110942812／Monitor PostgreSQL Job 120933711310](https://github.com/pampa0629/addp/actions/runs/40110942812/job/120933711310) | PG 步骤及 Job 均 success，38 项顶层及 16 项名称子场景实际运行，含三评估器与旧名称不回填，无 FAIL／SKIP。 |
| [Platform Run 40110942794／Monitor frontend Job 120922337484](https://github.com/pampa0629/addp/actions/runs/40110942794/job/120922337484) | 117 单元通过，浏览器到 363／424 进度时取消，测试步骤及 Job 均 cancelled，无完整浏览器结果或构建记录。363 是进度，summary success 只表示写摘要。 |

原件和步骤审计 `/tmp/addp-ci-20261024-180033/`。前端取消原因见[原始注释更正](#名称检索前端-ci-取消原因更正与限定预算修复2026-10-25)，PG 成功不替代前端或正式身份页面。

### 名称检索前端 CI 取消原因更正与限定预算修复（2026-10-25）

原始 check-run annotations 对 a604 与 f2ca 均明确报 `The job has exceeded the maximum execution time of 20m0s`。据此更正最初按时间关联其他门禁失败的推断，**两次均为 Monitor 前端 Job 自身达到 20 分钟上限**，无证据归因于 Common／Redis。对应注释文件为 Monitor-a604-check-annotations.json 和 Monitor-f2ca-check-annotations.json。原件 `/tmp/addp-ci-20261024-200030/`。

后继固定 `f2ca8c25e3a69bba0e50d9ad6412cfd51bb02fce` 的 [Platform Run 40119830116／Monitor frontend Job 120948753235](https://github.com/pampa0629/addp/actions/runs/40119830116/job/120948753235)（push attempt 1）实际 117 单元通过，浏览器到 423／424 进度后取消，无完整报告或构建。其与 a604 的 Monitor／共享前端、Node、Makefile、launcher 输入一致。两轮取消没有独立的退出清理通过证据；旧 ea320 的 421 项不替代这两轮。

预算只给 Monitor 矩阵项登记 30 分钟，其余前端保持默认 20 分钟；单 Worker、117／424、原断言、构建及单用例预算保持。登记红阶段 22 项中有 9 个错误，修正后 22 项与实际 21 前端登记通过，选择回归 25 项及 Monitor 路径命中通过。原件 `/tmp/addp-monitor-ci-budget-{red,green,selection}.log`。配置修正自身不是远端通过，实际后继见下节。

### 名称检索前端 CI 预算闭环：固定 a292 整套通过（2026-10-25）

同一固定 `a2928b9a76a749f0ca5002754c2bc12d118e31fe` 的两份实际前端证据如下。

| 实际 Job | 结果 |
| --- | --- |
| [Platform Run40128534338／Monitor frontend Job120975073843](https://github.com/pampa0629/addp/actions/runs/40128534338/job/120975073843) | 自然首轮主验收，实际步骤及 Job success。117 单元、424 受控浏览器（22.4 分钟）、Vite 构建（10.78 秒）通过；整个 Job 约 24 分钟。 |
| [Run40132268218／Job120986322295](https://github.com/pampa0629/addp/actions/runs/40132268218/job/120986322295) | 既有 schedule 的补充回执，实际 117／424／构建通过（浏览器 21.9 分钟），与 push 分开记录。 |

从 a604 至 a292 只变更编排、登记／测试及文档，Monitor 产品／用例、共享前端、Node、Makefile 和 launcher 一致。该固定输入证明 Monitor 单 Worker 的 30 分钟预算容纳整套；原两轮取消与 a604 PG 通过均保留。原件 `/tmp/addp-ci-20261024-220030/` 含日志与独立审计。此次只认固定前端 CI，个人 Safari、Hosted T4 和第 51 行其他能力另计。

### 告警名称检索个人 Safari 只读复验（2026-10-25）

用户重启后，Monitor ready HTTP 200，实际运行 `7b3a5d6cac630ac76edbfdebda61e2ee89f674cf`，已核对包含 `a6048b9e71b0689893a3cf90db5a0ef4822d8866`。旧页面保留未记录名称；整页重载后真实日志告警显示“源容量接近额度”。

Safari 平台系统管理员在真实 Console／Monitor iframe 输入“源”再追加“容量”，草稿保留。匹配、空结果、清除、重载及通知往返实际执行；地址保持 `severity=warning&search=源容量`，清除后保留级别并恢复原一条结果。原件 `/tmp/addp-alert-name-safari-result.json` 及同前缀文本／截图。

仅一条日志告警，纯只读。真实多页、规则改名／删除后的冻结、旧规则缺名、大小写／通配符、正式隔离 T4／Hosted 未覆盖；这些本地 PG／受控 T3 结论不外推为 Safari。

<a id="alert-evidence-availability"></a>

## 服务组失联

区分受控组投影／真实数据库、个人规则保存／启用和 Hosted 准备。在线评估与真实租约失联、恢复、通知及退出清理分别认定。

### 2026-10-25 服务组失联规则与持久确认

`/platform/service-availability-alert-rules` 与 `/groups` 沿当前 Platform User 和 System 读取权；四项新增权限只绑定既有平台系统管理员。新建停用，15–300 秒且为 15 秒倍数，初始 60／30 秒、warning。实际在线副本确认当前部署预期，历史登记不授资格；本组意图变化或重新启用重确认，重启保留资格但连续窗口重积。来源变化、超过五秒未更新、重复／倒退／超过 16 秒间隔均不判打开或恢复；计划停用不结束活动事件。名称及恢复条件在打开时冻结，`service_availability` 只投显式订阅，复用原 CAS、抑制、事务 outbox 和通知渠道。

| 层级 | 实际结果 |
| --- | --- |
| Go／IAM | Common／System／Monitor Go 和授权登记通过，含有界组投影、当前 User、非委托与原边界。标准 IAM PG 242 前后比较仅新增四权限和系统管理员绑定；既有授权版本推进、旧 refresh family 撤销和重复 Runner 幂等通过。 |
| Monitor PG | 真实数据库、受控组投影／时钟。旧登记 99 仍等待，首次在线确认后全失联 60 秒开一个事件，双评估无重复；在线 30 秒恢复同事件，冻结时长、显式订阅、抑制、来源变化、重启确认及删除边界通过。按当次主体与目标核验夹具零残留。 |
| 页面／登记 | Common 277、System 172、Monitor 122 单元／444 受控浏览器及构建通过，六项专项已计入 444。覆盖停用默认、候选撤权、CAS 草稿及冻结依据；Swagger 96、T2 登记通过。 |

原件 `/tmp/addp-service-availability-*.log`。真实租约故障、实际渠道送达、个人 Safari 与正式身份 Hosted 单列，清单第 15／49／52 行保持部分实现。

### 重启后的个人环境只读核对（2026-10-25）

用户全量重启后，System／Monitor／Gateway ready 均 HTTP 200，实际 `871bc854795105b364b154f1802697969655b6f0` 含 `062301037`。个人 Safari 规则列表空且无错误，25 个候选与受管声明相符；读取到 Meta／Quality／Transfer 当前预期只证明声明。Meta 候选选择、默认 60／30 秒、创建停用及缺名称阻止写入可由 AX 读到。

截图与 AX 的弹窗状态不一致，Raise 未恢复；本次无视觉、保存或启用通过，未创建规则。收据 `/tmp/addp-service-availability-personal-restart-20261025.json`。真实租约失联／恢复及 Hosted 未覆盖。

### Safari服务失联规则保存复验（2026-10-26）

个人 Safari 实际截图取得创建弹窗，选择 Meta 扫描，保存 `验收临时-服务失联-Meta扫描-20261026`。页面刷新和整页重载后仍为一条同组、60／30 秒、停用／已暂停规则。初次编辑按钮操作报 noWindowsAvailable，键盘路径亦未得到编辑弹窗，启用当时未通过。

用户激活前台后，实际编辑同一规则为 `验收临时-服务失联-Meta扫描-20261026-编辑复验` 并启用。AX 仍读 off，保存后列表与重载均显示启用“是”；10:18:43／10:18:58 连续积累，10:19:43 为服务组在线。最终留存一条启用的临时规则，未删除、停服务、制造失联或发通知。只认个人保存、启用、持久重读与在线评估；失联、恢复、通知、显示差异根因与正式 Hosted 未验。

### 服务失联Hosted专项准备与本地runner验证（2026-10-26）

既有 `platform-node-metrics` 准备专用 `service-availability` case，限定 Meta 双 Worker、正式身份／Console 和外层清理。待实际验证单 Worker 租约失效不报警、全失效 60 秒打开一次、恢复一个 30 秒结束同事件、回环 HTTPS／HMAC 重试、计划停止无新事件、删除保留历史和页面冻结依据。

故障夹具按 PID 文件、程序／工作目录及 /proc 启动标记核验，Linux pidfd 固定进程，暂停前写私有 0600 恢复记录；异常与外层停止前均按归属恢复，身份漂移保留记录并令清理失败。原 1800 秒场景／60 分钟 Job 保持。正式场景要求真实密码／MFA 及 HTTP，无 API 拦截。

旧本地门禁在失败分支先超时 20 秒，最终 runner 148 项、System 身份 Go、profile 登记与 Swagger 96 方法通过，覆盖受控进程归属、PID 变化、失败清理顺序及签名契约。原件 `/tmp/addp-service-availability-runner-first.log`、`/tmp/addp-availability-*.log`。未执行真实 Linux pidfd／租约注入、Hosted 页面或实际零残留；个人已启用临时规则未改动。

<a id="alert-evidence-filesystem"></a>

## 文件系统容量告警

区分规则与挂载页面、固定前端 Job，以及受控来源到真实 Prometheus／SQL／签名 HTTP 的 T2。保留前置首败、清理及实际采集边界。

### 文件系统容量告警首期（2026-10-27）

规则精确绑定主机／device／mountpoint／fstype，创建停用，默认严格超过 85% 连续 300 秒触发、严格低于 80% 连续 120 秒恢复，15 秒评估。以同次容量与 uptime 核验；容量／来源／规则变化重新计时，缺样、重复、过期、预算失败和挂载缺失不恢复。永久卸载可停用，有活动事件禁止删除；冻结证据按原通知与同挂载趋势展示。

唯一 EnsureMonitorStore 前向替换旧主机／指标索引为含挂载键的唯一索引。真实 `addp_test` 重建旧索引后验证标量规则保留、异 fstype 独立、容量变化、缺样、outbox 与企业微信；早期新增用例首败为 RecordTx 漏传证据，修正后 PG 全套通过。Go、128 单元／473 受控浏览器及构建、六项定向、Swagger 96 和配置／登记 102 均通过。原件 `/tmp/addp-filesystem-alert-*.log`。

| 实源尝试 | 首败及已达阶段 |
| --- | --- |
| host-resources | 既有六来源总览先失败，未执行新增告警。 |
| filesystem-alerts 首轮 | 公共样本超限预检超时，未进入告警。 |
| filesystem-alerts 后轮 | promtool、mTLS、中心、自采样、超限及来源停启通过，受控发现仍失败；Nginx 连续两次读取 `/source/discovery.json` 得 ENOENT。未执行 TestIntegrationMetricsFilesystemAlertLifecycle，文件可见性具体根因未定。 |

三次独占项目标准退出后均按精确项目复核容器／网络／卷零残留，见 `/tmp/addp-filesystem-alert-cleanup.json`。本节保留全部实源失败，后继[受控源实链 T2](#文件系统容量告警受控源实链t2复验2026-10-28)单列；个人 Safari、真实主机容量压力和 Hosted 未覆盖。

### 主机挂载行创建容量告警入口（2026-10-27）

列表展开／详情的完整挂载行只在具备当前非委托 Platform User、资源观测／主机读取及规则读／创权限且有有效采样时可打开创建表单。重新核验主机和完整挂载，使用服务端预设；缺样、消失、版本变化或拒绝不换绑、不写。显式保存为停用，取消无写入，冲突保留草稿。唯一 ResourceAlertRuleEditor 拥有表单与 POST／PUT，挂载行只交付完整身份。

原规则四项和新增七项定向通过，覆盖精确挂载、合法零、真实预设、冲突／撤权与取消。早期首败为把输入值当文本、GET 路由遗漏 query、冲突文案断言错误。初次完整 Monitor 129 单元／480 浏览器及构建通过。之后另发现打开弹窗时主机自动刷新 403 未清表单；旧用例失败，修正监听后关闭草稿且无写入。最终输入只变两个文件，129 单元、67 受影响浏览器及构建通过，最终 481 项整套未重跑。原件 `/tmp/addp-mount-capacity-*` 和 `/tmp/addp-mount-alert-editor-refactor.log`。

截图为受控中英窄屏。未运行 Safari、真实采样或正式身份 Hosted，首期实源失败仍单列。

### 挂载告警入口的共享所有权门禁登记修复（2026-10-27）

`2d373a47e` 已将表单迁入唯一 Editor；[Platform consistency失败](https://github.com/pampa0629/addp/actions/runs/40465364758/job/122059331587) 的首败是旧 HostNodeSelect 所有权测试仍要求规则页直接导入。更早的失败 Job `122058289956` 保留，Monitor 单元／浏览器通过不替代共享门禁。原件 `/tmp/addp-ci-20261027-073016/job-122059331587.log`。

登记改用实际 Editor 并补已有 EngineDeployments，扫描实际消费者，保留唯一共享组件、显式 api／allowed／identity 和列表／挂载页只组合 Editor 的断言。标准 Common 前端先 282／283，修后 284 通过；Monitor 单元 129 通过。两产品目录与原版本一致，复用原浏览器／构建。原件 `/tmp/addp-host-selector-owner-*.log`。未运行完整 Platform 或 Hosted，未以本地通过改写原 Job。

### 主机挂载行查看已有容量规则入口（2026-10-27）

挂载行的“查看已有容量规则”只需已有规则读权，无需 create／update 或当前容量样本。URL 绑定 node_id、固定容量指标及完整 device／mountpoint／fstype，Count／分页前取交集，包含停用规则。非法／缺失／重复／错配条件拒绝，不回退全量；刷新、分页和返回保持上下文，撤权和迟到响应沿原边界清除。

Go、标准 `addp_test` PG、Monitor 130 单元、Common 284、Swagger 96 及构建通过。PG 实测先于 25 条较新不匹配记录筛选，含异 fstype、字面通配符、总数和第二页。首轮定向七项 6／7，query 键顺序导致重复规范化和两次请求；唯一顺序修正后单项通过。

完整前端随后 165 通过／13 失败／1 中断／309 未运行，另一次诊断 3 通过／5 失败／12 未运行，含宿主页面、图表及迟到场景夹具初始化。高负载、内存压力及网络变化仅是伴随证据，根因未统一确认。两个独占 runtimeDir 已确认删除。最终同入口 130 单元／488 浏览器（含七项）／构建通过，128 输入复核一致，成功 wrapper 清理完成。原件 `/tmp/addp-mount-rule-*`，保留失败目录 `addp-monitor-playwright-results-YS2HVY`／`JT4sCX`。

这批本地页面覆盖只读定位、缺样时查看、停用、错误、撤权和恢复。个人 Safari／实际挂载、CI／Hosted 尚未取得新结果；后端参数变更后的个人验收需使用已应用版本。

### 挂载规则入口的个人Safari前置核对（2026-10-27）

个人 Safari 初报无法确认访问，原重试后恢复；System／Monitor／Gateway ready 实测 200。Desktop VM CPU／内存／vda 有值，文件系统／网卡未提供、TCP 缺失未补零，因此没有真实挂载行可验证创建或查看。返回保留 `list_view=load-connections`。

页面持续隐藏并暂停自动刷新；Raise 未恢复，横滚报 noWindowsAvailable。右列视觉、前台刷新和挂载规则跳转未验。运行提交未核对，未写规则／通知或改个人部署，正式身份闭环未覆盖。

### 挂载缺样断言修复的完整CI闭环（2026-10-28）

固定修复 `9ad52efda9d0b7d9fea1e644060363461c07a784` 的有效后继 `7ebf98370ebc965e09cb5744983dbf8ee7fbbbce`，[Monitor Job 122257072557](https://github.com/pampa0629/addp/actions/runs/40523616999/job/122257072557) 实际 Monitor 前端步骤及 Job success。130 单元、488 浏览器、Vite 构建通过；修复至后继 Monitor 前端无差异。实际运行的缺样用例验证 no_data 后刷新完成仍 disabled，另一个主机刷新清旧指标用例独立；Report skipped 跳过不等于正式步骤跳过。

原件 `/tmp/addp-ci-20261027-160029/` 含原日志、独立审计和 Job 元数据。旧 35／a1f 失败及旧预算超时保留。本项仅关闭固定前端 CI 缺口；Safari 隐藏／右列／前台刷新、真实 Linux 挂载行和告警实源未由本 Job 覆盖。

### 文件系统容量告警受控源实链T2复验（2026-10-28）

标准 `--case=filesystem-alerts` 最终退出 0。110 配置／登记、promtool 容量 18／inode 22、mTLS、中心采集、超限／恢复、停启及受控主机发现通过。TestIntegrationMetricsFilesystemAlertLifecycle 实际 175.62 秒：受控高／低容量经真实 Prometheus、Monitor、当次独占 `addp_test` 及签名 HTTP。高值打开一次，首投 503 后送达；缺样保持未知且超过恢复期不恢复，低值恢复同事件。最终 2 生命周期事件、2 次送达、3 次 HTTP，重复派发未重发。

前两轮分别首败于样本超限和正常恢复前置，Nginx 均有 `/source/metrics` ENOENT，未进入告警。首轮诊断 GET 404 曾掩盖原异常；红绿修正保留首败。封闭文件诊断限定当次 metrics／discovery.json，宿主有界读取、容器只看可见／可读，成功轮未执行故障诊断，文件可见性根因未定。最终独立 110 回归、登记及语法通过。

固定基底 `41024f0800a646ee613f53f518673de2c494527e` 叠加两脚本，其他会话的 Make 登记差异不影响原 Monitor recipe。成功项目 `addp-metrics-t2-032ce1c920ed484f8a84f9f9a4856244` 的临时文件删除，三轮分别按项目核对容器／网络／卷为零。原件 `/tmp/addp-filesystem-discovery-*` 和 `/tmp/addp-filesystem-first-failure-red.log`。

通过范围为 Desktop 上受控 Linux 指标源到真实评估与签名接收端。本机成功保留原失败；原生主机 Collector、物理容量压力、Safari 挂载行、正式密码／MFA、Hosted 和生产 T5 未覆盖。

<a id="alert-evidence-process"></a>

## 服务实例 CPU／RSS 告警

隔离受控页面、个人 Safari 弹窗与真实 SDK 采样 T2 分列。

### 服务实例 CPU 与 RSS 超限告警（2026-10-22）

实现范围：从当前授权运行实例中选择具体实例，分别设置 CPU 占用核数和 RSS 容量阈值；CPU 保持完整一分钟窗口，RSS 表单用 GiB 输入并转换为字节。规则新建停用、实例与指标不可变、更新/删除按 CAS；沿用平台资源规则权限。后台来源、连续窗口、事件协调器和事务 outbox 复用现有实现；`process_resource` 通知类别须显式订阅。只保留 opened/resolved 生命周期，离线、退出、缺样、查询错误和停用不恢复活动事件；重启不迁移旧规则，已有事件保持打开时的恢复条件。

验证结果：

- `make test-module MODULE=monitor GO_ONLY=1`：退出 0，Monitor Go T1 通过；包括固定单位、完整窗口、边界、缺样、重复样本、非法值与 API 权限/严格输入。
- 已执行 `bash scripts/infra/status.sh` 核对本机共享 PostgreSQL 实际映射为 25432；在 `ADDP_TEST_POSTGRES_DATABASE=addp_test` 下通过 `make test-monitor-postgres`，退出 0，33 项顶层 PostgreSQL T2 通过。新增 CPU/RSS 生命周期均覆盖 CAS、重复规则拒绝、活动事件删除拒绝、退出/启动身份改变/采集失败不恢复、恢复条件冻结、有效零值恢复、显式订阅与 outbox 隔离、停用竞争丢弃旧结果；当次夹具执行清理及规则残留核验。
- `make test-monitor-frontend` 中 96 项确定性测试通过；首轮浏览器 285/286 通过，失败仅为新增测试使用错误控件名称。后续新增证据/订阅场景并修正 Element Plus 禁用控件定位和两位小数断言；通过标准 `npm run test:e2e -- e2e/process-rules.spec.js` 定向复验，最终 7/7、退出 0。未修改的 282 项既有浏览器场景复用首轮通过证据，HTTP 与主机告警表单回归包含在内；不把首轮非零结果写成整批重跑通过。`npm run build` 退出 0。
- `bash scripts/swagger/gen-swagger.sh monitor` 与 `bash scripts/swagger/check-route-coverage.sh monitor`：最终均退出 0，90 个公开路由方法覆盖一致。共享源码在生成期间变化导致的一次拒绝已重新生成，不使用该次失败产物作为通过证据。
- 本地日志：`/tmp/addp-process-alerts-go.log`、`/tmp/addp-process-alerts-pg.log`、`/tmp/addp-process-alerts-ui.log`、`/tmp/addp-process-alerts-ui-final.log`、`/tmp/addp-process-alerts-build.log`、`/tmp/addp-process-alerts-swagger.log`。

边界：PostgreSQL T2 使用受控 System/指标来源夹具，浏览器 T3 使用隔离 HTTP 夹具，不能计为真实身份、真实 Prometheus 压力或正式通知端 T4 验收。本轮未重启个人服务，未执行 CI 或 Hosted，等待专门会话回传；未设置定时任务。

2026-10-22 补查 Console 嵌入场景：`console/frontend/e2e/platform-alert-navigation.spec.js` 新增服务实例规则弹窗用例，验证弹窗位于视口内、内容与遮罩透明度、名称输入、真实选择控件的候选选择及关闭；只模拟 HTTP 响应，不写个人配置。标准 `npm run test:e2e -- e2e/platform-alert-navigation.spec.js` 最终 Chromium 5/5、退出 0；`npm run test:e2e -- e2e/platform-alert-navigation.spec.js --browser webkit --grep painted` 的同一新增场景 1/1、退出 0。初次诊断用例的动态夹具导入及控件定位失败已修正，不计产品故障。WebKit 仅用于本地诊断。个人 Safari 仍出现辅助功能树有表单但用户与截图看不到弹窗，且控制工具有 noWindowsAvailable／ScreenCaptureKit 错误；隔离浏览器通过不能证明此真实故障已修复，仍待前台手动点击复现及根因确认。本轮未修改产品样式，未创建或启用真实规则，未查询 CI／Hosted、未设置定时。

### Safari 服务实例规则弹窗实景复核（2026-10-23）

05:49–05:53（Asia/Shanghai），在用户确认可操作 Safari 后，用原生 CUA 操作已登录的平台系统管理员及真实 Console `/monitor/platform-alerts?tab=process-rules`。检查器读到页面曾 `document.hidden=true`，随后为 `false`；活动弹窗 `display=block`、`visibility=visible`、遮罩和内容透明度均为 1，动画列表为空，标题坐标的命中检测确为弹窗标题，父层命中 iframe。检查器开启时 iframe 高度约 227 CSS px、弹窗顶部约 34 px，标题仍在可见区域；没有取得样式隐藏、动画卡住或其他元素遮挡的证据。期间工具出现用户操作中止及 ScreenCaptureKit 捕获错误，辅助功能树与截图一度不同步。

窗口恢复前台并关闭检查器后，真实截图清楚显示弹窗与遮罩；随后实际关闭、确认列表恢复、重新打开，以及展开真实实例候选下拉层均正常，截图包含 Monitor／Meta／Transfer／Manager／System 当前候选。保持表单未保存，未创建、启用规则或发送通知。此前现象与后台窗口／重绘不同步相关的判断得到支持，但未建立 Safari 底层异常的稳定复现，不称为代码根治或全部告警 T4 通过。既有 Chromium 5/5、WebKit 弹窗专项 1/1 是隔离 T3 证据，本次是个人开发环境只读 UI 复核，分别陈述。CI／Hosted 仍等专门会话回传，未查询或调度、未设置定时。

### 服务实例 CPU/RSS 真实采样告警链路 T2（2026-10-23）

沿原标准入口新增 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=process-alerts`，只使用门禁自建非 root 容器的 0.25 CPU／128 MiB 原硬上限。三个真实 Common SDK 进程经原 mTLS／唯一 HTTP SD 进入 Prometheus；其中一个 Worker 在加压前固定 CPU 触发／恢复值 0.025／0.008 核，RSS 按真实空闲基线加 12／8 MiB 固定阈值，随后有界计算并持有 24 MiB 匿名内存，释放后由自然新样本判断恢复。测试控制仅存在于测试二进制，单次最多 180 秒，未改生产采集器或个人服务。

冻结基线 `9f6e77140d9576dc8ef2419082f765175232f312` 加本批七项输入，在隔离源码副本执行：

- `make test-module MODULE=monitor GO_ONLY=1`：退出 0，Monitor Go T1 通过。
- `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=process-alerts`：退出 0；48 项配置／分组回归、八项固定窗口测试及真实进程告警专项通过，无专项 Skip。专项实际耗时 245.08 秒：CPU 打开证据为 0.0269–0.0511 核，释放后的恢复证据为 0.00378–0.00400 核；RSS 从约 30.8 MiB 基线增至约 57.6 MiB，恢复证据约 30.4–30.6 MiB。两项均使用推进中的连续样本，CPU 保留完整一分钟，不改写计数器或历史；两个原事件分别恢复，没有新增结束状态。四个 opened／resolved 事件共收到八次回环 TLS 请求，每个事件首次 503、随后成功，投递 ID／事件 ID／正文和持久证据均一致；旧日志订阅未收到该类别，对等 Backend 与 Worker 的三项当前值继续有效。退出明确报告 owned 容器／网络／卷及设施临时目录零残留，并独立按当次项目标签读回核实。
- `make test-release-runner`：退出 0，62 项发布／登记回归及正式登记检查通过；`make test-workflow-security`：退出 0。新增场景已加入原 Release Metrics 矩阵，仍保持每项 20 分钟及所有分组真实成功才通过的汇总要求；默认全量入口包含原分组及新专项，无第二套执行路线。

原始日志及输入摘要保存在 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-process-alert-real-r7sgulbj/` 的 `process-alerts-t2.log`、`monitor-go.log`、`release-registration.log`、`workflow-security.log` 与 `inputs.json`；七项工作区输入与冻结副本哈希一致。此结果为真实 SDK／Prometheus／PostgreSQL／签名接收端下的 T2，System 当前用户与身份投影仍是夹具，不替代正式密码／MFA、真实 Console、Hosted T4、生产容量或 T5。本轮未运行其他 Metrics 分组，复用原有证据边界；未查询／调度 CI 或 Hosted，等待专门会话回传，未重启个人服务、未设置定时任务。

### CPU/RSS 告警专项的就绪分段与失败诊断（2026-10-23）

专门 CI 会话回传 [39879255330 / 120203117355](https://github.com/pampa0629/addp/actions/runs/39879255330/job/120203117355)（`691441e81c5069f2d2d10903b0623d49135fdf17`）和此前同症状失败：已打开两个原事件并持久化签名通知，RSS 恢复，CPU 仍为 risk，场景在 270 秒退出。旧日志只打印 LastValue 指针，没有 CPU 数值或实际释放时刻，不能据此断言 CPU 未释放或生产恢复逻辑错误。代码确认冷来源的完整 CPU 基线与规则创建后预热串行消耗同一场景预算；本机新增诊断还确认抓取的毫秒抖动可使相邻样本只有 14.997 秒，严格持续窗口需要第三个样本。

本批只调整测试设施：准备阶段以原 65 秒等待确认三个精确来源的完整 CPU 历史；Go 场景区分规则创建后完整 CPU 窗口、空闲正常、真实 RSS 释放和同一事件恢复。保留来源八分钟寿命、270 秒总预算、各阶段 100 秒、可执行测试 300 秒、0.25 CPU／128 MiB、固定阈值、全部生命周期与签名投递断言。未修改生产采集／查询／告警判断、权限或个人服务。失败日志增加规则数值、评估／采样时刻及有界窗口查询；来源准备失败的诊断只保留固定信号与数值，不输出端点、任意标签或凭据。

冻结基线 `0c3946573e375da4cbd10bd78793e2eeaf17ed34` 加本批输入，证据目录为 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-process-alert-repair-x2oq49qd/`：

- `make test-module MODULE=monitor GO_ONLY=1`：最终输入退出 0，见 `monitor-go-t1-final.log`。新增确定性回归验证不足 15 秒不能舍入恢复、第三个推进样本及规则创建后完整 CPU 分钟边界。
- `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=process-alerts` 的最终尝试：前置 51 项配置回归通过，包括真实零、缺失／重复／替换来源拒绝及诊断去除私有标签；整轮未通过。`TestIntegrationMetricsNetworkWindow` 的 promtool 已打印 SUCCESS，但容器命令在原 30 秒期限被终止，后续固定窗口进程亦未正常结束；尚未进入 CPU/RSS 专项。见 `process-alerts-t2-final.log`，不能把前置单测或 SUCCESS 文本计为 T2 通过。
- 此前两次编排尝试分别在空闲预热 100.40／100.43 秒失败，均未加压；前者证明缺样期间起点重置，后者记录低于固定恢复阈值的有效 CPU 值但严格连续窗口仍未就绪。第三次在 65 秒 CPU 来源准备阶段失败，旧版未保留该阶段的原始数值，原因未确认；新诊断的安全序列化仅有单测证据，不能反推这次准备失败已修复。见 `process-alerts-overlap-failed.log`、`process-alerts-cadence-failed.log`、`process-alerts-source-readiness-failed.log`。
- 四次项目的容器／网络／卷已逐项按精确 Compose 项目标签核对为零，见 `failed-project-cleanup.json`。最终尝试的自动清理曾失败，后续确认无容器、网络无端点且归属标签匹配后，仅删除该项目遗留网络／卷；人工清零不改变原门禁失败结果。

该批次当时的结论为测试编排修正与诊断代码已落地、Go／配置回归通过；该批输入的真实 CPU/RSS 触发与恢复 T2 **未通过**，Linux CI 与正式身份 T4 也未确认。源码输入见 `input-sha256.json`；未自行查询、触发 CI／Hosted 或设置定时，不将上述历史 T2 通过外推到该批。后续固定版本的 Linux 通过证据见下一节，不改变本段各次失败及清理记录。

### 服务实例 CPU/RSS 告警原生 Linux 固定版本复核（2026-10-23）

专门 CI 会话回传 [Release 39908130012 / job 120318541196](https://github.com/pampa0629/addp/actions/runs/39908130012/job/120318541196)，push、attempt 1，实际检出 `dfa4ad16105d0421af13e55f3e4099acdd6751a2`。已独立复核原始日志与该提交的测试源码：`Verify isolated metrics lifecycle` 实际运行 `make test-monitor-metrics`、`MONITOR_METRICS_TEST_ARGS=--case=process-alerts`，`TestIntegrationMetricsProcessAlertLifecycle` PASS，213.73 秒；退出报告 owned 容器／网络／卷及设施临时文件零残留。此处接收并复核既有运行，没有再次查询、调度 CI／Hosted 或设置定时。

规则在加压前固定 CPU 触发／恢复 0.025／0.008 核，RSS 为真实空闲基线加 12／8 MiB，连续窗口各 15 秒；保留完整一分钟 CPU 历史、原资源上限与生命周期断言。真实计算／持有匿名内存后打开两个独立事件，CPU 触发证据 0.03378～0.05800 核，RSS 62,111,744 B；释放后确认真实 RSS 回落，原两个事件恢复，CPU 恢复证据 0.00267 核，RSS 34,353,152～36,708,352 B。触发／恢复各证据窗口含两个推进样本，恢复样本均在释放之后。四个不可变 opened／resolved 事件、四项成功投递、八次签名 TLS 收件断言通过，每事件首次 503 后重试的投递 ID、事件 ID 及正文一致；旧日志订阅零误投，另外两个进程继续采集，测试清理断言当次规则零残留。

原始日志与独立审计位于 `/tmp/addp-ci-20261023-120029/`：`job-120318541196.log` SHA-256 为 `75c5b5f66faa6585d7cbf4a16024aebec2c5024740dfe7d3a05e58134cb56571`，`dfa4-process-alert-lifecycle-frozen.go.txt` 为 `15605f1114d79fba801d09df548c558f4df111e6c1eba235379493732ffb010e`，后者与 `git show dfa4ad16105d0421af13e55f3e4099acdd6751a2:monitor/backend/internal/service/process_alert_lifecycle_prometheus_test.go` 的哈希一致。另有 `Monitor-dfa4-real-process-independent-audit.json` 与 `monitor-dfa4-artifacts.json`；该 run 无下载产物，证据为原始日志及固定源码断言。该结果仅证明该 SHA 的原生 Linux 实源 T2，身份仍为 T2 夹具，不替代正式密码／MFA 页面 T4、长时段、生产容量或当前 HEAD 全批验证；`6dba`／`691`／`9e5`／`d01`／`f6` 等原失败保留，不以此次成功推定其历史根因均已修复。主机五项列表摘要、热力面板及后续前端修复另按各自证据判断。

<a id="alert-evidence-navigation"></a>

## 其他筛选、排序与刷新

记录规则外的列表查询、分页、交互和固定版本验证。

### 平台告警级别组合筛选（2026-10-24，本地）

对应原功能表 Excel 第 51 行、B 列 50“告警信息管理”。事件查询新增现有 `critical|warning` 精确筛选，与类别、状态和主体取交集，在计数及分页前执行；省略／空值读取全部级别，排序、事件快照、权限和生命周期不变。未知级别、重复／未知参数及编码错误返回 400。Monitor 页面选择级别回到首页，刷新、重载和浏览器返回保留条件；Console 同一 iframe 内同步级别地址，切换通知页保留筛选。未实现名称／时间组合检索、高等级置顶、依赖拓扑、影响范围或根因分析。

源码为基线 `e814fed8d` 加本节同批任务改动；19 项任务输入摘要见 `/tmp/addp-monitor-severity-inputs.json`，收据 SHA-256 为 `da396ec8485ef25e3582eafbf4e1456756d06915d31031847be8c4f1d73c2f33`。与本批无关的工作区文件未纳入输入或提交。

| 标准命令 | 实际结果与范围 | 本机证据 |
| --- | --- | --- |
| `make test-module MODULE=monitor GO_ONLY=1` | 7 个 Go 测试包通过；含平台 User 原权限守卫及级别／重复／未知查询拒绝；不计为完整模块 T0–T3 | `/tmp/addp-monitor-severity-go.log` |
| `make test-monitor-postgres` | 35 项 PostgreSQL T2 顶层用例通过，无跳过；本地先经 Infra status 核对 `25432`，只使用 `addp_test`。新增级别用例覆盖两级交错数据、计数／分页顺序、类别／状态／主体交集、空结果及 SQL 字面拒绝；当次独有主体清理后核对零行 | `/tmp/addp-monitor-severity-postgres.log` |
| `make test-monitor-frontend` | 115 项 T1、417 项隔离浏览器 T3 及 Vite 构建通过；新增中英文级别选择、页码重置、刷新／返回／重载、无效条件不扩查和失败清空均包含于本轮整套通过结果 | `/tmp/addp-monitor-severity-frontend-full.log` |
| `make test-console-frontend`；`npm --prefix console/frontend run test:e2e -- e2e/platform-alert-navigation.spec.js` | Console 195 项 T1、219 项 T3 及构建通过；随后同一测试文件增加级别同步断言，最终 5 项专项再次通过，验证同一 iframe 文档、真实模块路由和通知页重载，产品输入未再变化 | `/tmp/addp-monitor-severity-console.log`、`/tmp/addp-monitor-severity-console-navigation-final.log` |
| `bash scripts/swagger/gen-swagger.sh monitor`；`bash scripts/swagger/check-route-coverage.sh monitor` | 生成成功，91 个公开路由方法覆盖一致；新增级别参数与枚举的双语文档 | `/tmp/addp-monitor-severity-swagger.log`、`/tmp/addp-monitor-severity-coverage.log` |
| `make test-frontend-ci-registration` | 20 项登记回归及 21 个前端登记核对通过；现有 Monitor／Console 前端与 Monitor PostgreSQL workflow 已覆盖本批路径，无新增入口／依赖或编排 | `/tmp/addp-monitor-severity-ci-registration.log` |

最初浏览器专项因测试使用错误中文标签、随后点击 Element Plus 内部输入被选中标签遮挡而失败；修订测试定位到既有可操作下拉框外层，3 项专项和最终整套 417 项均通过，原失败日志仍保留为 `/tmp/addp-monitor-severity-browser.log`、`/tmp/addp-monitor-severity-new-interactions.log`，不计通过。不改变共享组件或以强制点击绕过交互。

本批 T3 使用受控 API 返回，不代表正式身份、个人 Safari 或 Hosted 验收；没有重启个人服务、修改 IAM／个人业务库、设置定时任务或主动跟踪 CI。CI／Hosted 结果继续等专门会话回传。

### 平台告警发现时间范围查询（2026-10-24，本地）

对应原表 Excel 第 51 行、B 列 50“告警信息管理”。沿已有 `opened_at` 发现时间和 System 时间边界口径新增 `time_from/time_to` RFC3339 查询：起始含、结束不含，支持单边界，空边界不限，双边界须开始早于结束；UTC 公元 1—9999 年。与类别／状态／主体／严重级别取交集，在计数和分页前完成，排序不变；不按恢复或最近观测时间改判。服务使用唯一 `PlatformAlertFilter`，删除原位置参数调用，不保留第二条查询路径。无新权限、迁移、依赖、配置或后台任务。

页面复用 Element Plus 日期时间控件及共享时间规范化，本地时间输入、UTC 参数提交。查询才应用草稿并回首页；刷新不读取未提交草稿，清除时间保留其他筛选；单边界分页、重载／返回及 Console 同一 iframe／通知页恢复已提交时间。错误、撤权和迟到响应沿原清理链。非法／重复时间、非法年份、相等／反向区间拒绝，不删条件扩大查询。未增加预设时段、恢复时间查询、名称搜索、高等级置顶或根因分析。

任务输入基线 `25c5aab9165f582556ead36f8d5f4bb75118dfb5`，20 项任务文件收据 `/tmp/addp-alert-time-inputs.json` SHA-256 `393c3e979cf80567a63372d642bb283c6b097ba5ee06bc6d344e6df7bc34e891`。验证后任务产品／测试输入未漂移；最后只修订规范中年份范围的文字。验证期间已核对 Common、Common Frontend、Monitor、Console、System Frontend、根 Makefile 和相关 runner 无额外 HEAD 变化；其他会话改动不纳入本批结论。

| 标准入口 | 实际结果与范围 | 本机原始证据 |
| --- | --- | --- |
| `make test-module MODULE=monitor GO_ONLY=1` | 7 个 Go 测试包通过，仅计 Go T1；含时间参数及沿用权限守卫，合法单边界／时区／小数秒，非法日期／年份／无时区／相等／反向／重复参数拒绝 | `/tmp/addp-alert-time-go-final.log` |
| `make test-monitor-postgres` | 36 项顶层 PostgreSQL T2 用例通过，无跳过；新时间用例含 7 项子场景，覆盖微秒边界、带偏移的同一时刻、已恢复告警按打开时间、单边界／不限、交集与多页计数、显式公元一年上界不误作不限及无效范围拒绝。当次独有主体清理核对零行；先经 Infra status 确认实际映射 25432，仅用 `addp_test` | `/tmp/addp-alert-time-postgres.log`，SHA-256 `a7d76fd7062b30d53184f06c8b6a56d232dcff5c9f3962e27d9a526e783e651c` |
| `make test-monitor-frontend` | 116 项 T1、421 项隔离浏览器 T3 及构建通过；4 项新增时间交互纳入整套，含中英文／680px 窄屏、草稿刷新隔离、边界、单边界翻页、重载／返回／清除、错误与迟到响应 | `/tmp/addp-alert-time-monitor-frontend.log`，SHA-256 `2762557de9d29540fb2848432b4f5f7507f7f32631df31759083f587e50ae313` |
| `make test-console-frontend` | 195 项 T1、219 项 T3 及构建通过；真实 Monitor 模块页在同一 iframe 内同步／重载时间与级别，通知页往返恢复参数 | `/tmp/addp-alert-time-console-frontend.log`，SHA-256 `85507dd3414396562339f1bba0a722ea143e05e90da844470ccf92d24baca482` |
| `bash scripts/swagger/gen-swagger.sh monitor`；`bash scripts/swagger/check-route-coverage.sh monitor` | 生成成功、91 个公开路由方法覆盖一致，两个时间参数双语文档同步 | `/tmp/addp-alert-time-swagger.log`、`/tmp/addp-alert-time-swagger-coverage.log` |

首次浏览器专项有两项因测试将共享中文占位“起始时间”误写为“开始时间”而失败；按实际共享译名修订后，四项专项及上述整套最终通过。原失败 `/tmp/addp-alert-time-browser.log` 保留，不计通过；后续专项 `/tmp/addp-alert-time-browser-green.log` 只作为中间证据，最终草稿／翻页增强以整套 421 项结果为准。

T3 使用受控 API，不代替正式身份、个人 Safari 或 Hosted；没有重启个人服务、修改个人业务库或 IAM，没有查询／重跑 CI／Hosted或设置定时任务，相关结果继续等专门会话回传。

### 发现时间查询：个人 Safari 重启后只读复验（2026-10-24）

用户完成重启后，`curl --silent --show-error --max-time 10 -w '\nHTTP %{http_code}\n' http://localhost:8100/health/ready` 返回 HTTP 200，Monitor 已登记、本地依赖与 System 登记均 ready。运行源码为 `f651ad550fa41a942d40dccbf782ae4d02c51f10`，已用 `git merge-base --is-ancestor` 确认包含本功能提交 `ea320ab686f9fe40c36ee628284e4de0049ae547`。

在个人 Safari 现有平台系统管理员会话，通过真实 Console `/monitor/platform-alerts` 只读复验：截图可见发现时间控件和就近说明；结束时间单边界可查到现有告警；相等时间草稿提示无效并禁用搜索；查询 10 月 24 日至 25 日后现有 10 月 23 日告警被排除，整页刷新后日期与空结果保留；扩大起始日期至 23 日后该告警重新显示；组合警告级别后，清除时间保留 `severity=warning` 及现有告警。日期框使用本地时间，URL 使用对应 UTC。未创建、确认、抑制或删除任何告警。

操作记录 `/tmp/addp-alert-time-safari-result.json`，最终 AX 状态 `/tmp/addp-alert-time-safari-state.txt`，截图 `/tmp/addp-alert-time-safari.png`，摘要内记录两份文件的 SHA-256。当前只有一条现有告警，本次不证明多页或微秒边界；这些范围仍分别依赖上文 PostgreSQL T2／受控浏览器 T3。个人 Safari 结果不升级为正式隔离身份 T4、Hosted 或生产证据；CI／Hosted 继续等专门会话回传，本次未查询或触发，也未设置定时任务。

### 当前主机名称筛选：事实来源与本地分层验证（2026-10-25）

只读核对确认 System 的 `HostNode.display_name` 是可编辑的当前纳管名称，主机资源事件以 `resource/host_node` 与 UUID 引用；日志观测器标识、服务登记宿主名／运行环境名、模块名不是这份名称。用户确认首期仅查当前主机名称对应的主机资源告警后，先修订运行观测规范7.1，再在唯一告警列表增加 `host_name`，沿告警名称已有100字符、去首尾空白、忽略大小写的字面包含口径。非空参数复用当前 User Bearer、System 完整有界 `HostNodesForUser` 投影及原主机读取权；最多100台、五秒／四MiB，包含停用主机，失败／超限拒绝，不使用地址搜索、分页首批或后台身份兜底。匹配UUID与现有条件在计数／分页前取交集；改名后按新名查询，不存触发时主机名、不推断其他主体的承载关系。空值不增加System依赖，无数据库迁移、新权限、角色或私有观测字段。

页面在“主机名称”框旁标明仅主机资源告警，沿原独立输入草稿、提交／清空回首页、URL／刷新／通知往返恢复和失败清空。无主机读权时不显示可编辑入口；已有非空URL条件保留并由后端拒绝，不悄悄撤去筛选。已有当页主机引用展示仍由唯一共享解析器负责。

| 标准入口／脚本 | 本次实际结果与范围 |
| --- | --- |
| `make test-module MODULE=monitor GO_ONLY=1` | 7个Go测试包通过，仅计Go T1；新增主机名称额外权限、空条件不增加权限、重复／非法参数等API守卫用例。 |
| `make test-monitor-postgres` | 39项顶层T2通过、无失败／跳过。新增名称15个子场景及附加断言覆盖Unicode／大小写、字面百分号／下划线／反斜线、停用主机、多个同名片段主机、准确多页与时间／名称／级别／状态／ID交集、改名后旧名不匹配、同UUID的其他主体不误关联、Owner失败／超限／超时拒绝及空条件无依赖。System读取使用受控Reader，非真实身份T4；新增随机夹具退出清理并核对零行。已先运行Infra状态，实际共享映射25432，仅使用 `addp_test`。 |
| `make test-monitor-frontend` | 首轮118项T1通过；整套T3为426通过／2失败（15.5分钟），失败均是新增中英文用例缺少 `resourceIncident` 导入，在进入页面前发生ReferenceError。原轮门禁失败，未执行其后构建，不计整套当前源码通过。 |
| Monitor目录 `node ../../scripts/test/frontend-browser-gate.mjs e2e/platform-alerts.spec.js`；`npm run build` | 仅补齐上述测试导入，产品代码和其余输入未变。最终告警页34项T3全部通过，含原有名称／时间／生命周期及新增中英文680px主机筛选、连续输入、多页／刷新／通知往返、字面字符、空匹配、清除保留其他条件、错误清空与无权／非法URL拒绝；标准构建步骤成功。首轮其余394项与最终该页34项共同覆盖当前428个用例，但不将组合证据冒充一次全量绿色门禁；后继完整CI结果仍待专门会话回传。 |
| `make test-console-frontend` | 195项T1、220项真实iframe受控T3及构建通过；新增主机名称与告警名称／时间／级别在同一iframe和Console URL恢复。此前同一标准browser launcher的5项导航专项也通过。 |
| 标准Monitor Swagger生成／覆盖；Common Manifest `--coverage-report` | 生成成功，91条公开路由方法一致；授权覆盖报告 `complete=true`，新增参数及条件主机读取权限已同步，不修改IAM目录或角色。 |

`changed-gate.py --dry-run` 实际选择 Monitor 和 Console。Monitor仍沿既有30分钟完整前端预算，CI规范删除易过期的“当前用例数”表述，不改变预算、Worker、断言或重试。没有轮询、手工派发或重跑CI／Hosted，也没有个人数据库写入、服务重启或定时任务；提交后只沿main既有push触发编排。

证据 `/tmp/addp-alert-host-name-{go,go-final,postgres,frontend,alerts-final,build,console,console-full,swagger,swagger-coverage}.log`，授权覆盖及影响选择回执同前缀；`/tmp/addp-alert-host-name-inputs.json`记录22项实现／测试／契约摘要、唯一测试导入修正与全部日志SHA-256。真实Safari尚未加载本批后端，当前主机名称筛选的个人运行／正式隔离身份／Hosted未验，不沿用前一批“告警名称”Safari证据代替。Excel第51行仍为部分实现，高等级置顶、拓扑／影响范围及根因分析等缺口保留。

### 当前主机名称筛选个人 Safari 只读复验（2026-10-25，排除与恢复）

用户完成重启后，`curl --silent --show-error --max-time 10 -w '\nHTTP %{http_code}\n' http://localhost:8100/health/ready` 实际返回200／ready，Monitor已登记，运行提交 `5f90384a19a06edff2a8a2bc61a65375ea31c857`，包含产品提交 `994db320ae7147cb0e566db4960eb256306cc9e1`（`git merge-base --is-ancestor`退出0）；Build ID为 `20261025T023212Z-monitor-81346-3DgCB9`。使用个人Safari既有平台系统管理员会话，在真实Console完整刷新后复验；没有读取登录凭据或使用受控API代替个人运行数据。

先在“主机监控”读取当前纳管名称 `Docker Desktop Linux VM（Business / Infra）`。告警页“警告＋全部状态”实际显示5条日志链路告警，增加告警名称“源容量”后为2条；主机名称分段输入 `dOcKeR Desktop`，草稿保留，提交后Console与iframe URL均包含 `host_name=dOcKeR+Desktop`，显示空结果且无查询错误，截图可见输入框及旁边“仅查询主机资源告警”的说明。整页刷新、通知管理只读往返后，主机名、告警名、级别及全部状态均恢复；未知名称“不存在的测试主机”同样为空。清空主机名称后URL移除 `host_name`，其余三个条件保留，原2条日志告警恢复。

本次证明个人Safari输入、非主机事件排除、空结果、清除及条件恢复；**该警告／全部状态数据集没有主机资源告警，真实主机告警正向命中及大小写匹配不计Safari已验**。真实多页主机告警、改名／停用、权限变更与正式隔离身份T4／Hosted／生产T5仍未验；已有本地T2／受控T3的覆盖保持独立。未创建、确认、抑制、删除告警或规则，未操作通知发送或更改主机。

原始AX、截图及ready响应位于 `/tmp/addp-alert-host-safari-*`；操作汇总 `/tmp/addp-alert-host-safari-result.json`（SHA-256 `03c55fa2e39f830ab01b2c52e0b836290af992265a0625dbcb1298e82f64278f`）记录各文件摘要，完整过滤状态引用 `refresh.txt`／`return.txt`，不把无变化的差分AX当完整页面。本轮仅追加证据并执行 `git diff --check`，产品未变，不重复有效产品门禁。没有查询、触发或重跑CI／Hosted，也未设置定时任务，继续等专门会话回传。清单第51行仍部分实现；本次只补个人Safari排除与恢复证据。

### 当前主机名称筛选固定 994 前端 CI：完整门禁通过（2026-10-25）

专门CI会话回传后，本会话独立核对已落地原始日志、Job及实际测试步骤终态与内容摘要，没有轮询、手工派发或重跑CI。[Platform Run40150409107／Monitor frontend Job121040604365](https://github.com/pampa0629/addp/actions/runs/40150409107/job/121040604365) 为push／attempt1、固定源码 `994db320ae7147cb0e566db4960eb256306cc9e1`；真实 `Run Monitor frontend gate` 和Job均completed／success，日志实际118项单测通过、fail／skip均0，428项浏览器以单Worker全部通过（18.7分钟），Vite构建成功（7.01秒）。包括主机名称规范URL及非法条件保留单测、当前主机名称筛选与分页／刷新／导航恢复的中英文真实页面用例。该固定版本已具备一次完整绿色的前端T1／受控Linux T3／build证据，不再只有上一节的本地组合结果；本地首轮两项测试导入失败仍保留为失败。

同Run的Platform consistency Job121040604338仍因独立的部署发现observer退出核验失败；不能把此Monitor Job成功写成整轮CI全绿，也不能把通用清理失败当作主机筛选业务失败。该证据不代替个人Safari、正式身份／Hosted、真实运行环境或生产T5，不扩展为当前HEAD整套产品已验。后续 `05b232920` 仅补退出诊断，原清理故障根因未定，其实际结果继续等专门CI会话回传。

证据目录 `/tmp/addp-ci-20261025-020029/`：`job-121040604365.log` SHA-256 `6af0ee1d76fb65d2713185e59c7c8657ccc9bc3af7855e2012090f92f65d874d`与协调审计一致；`jobs-40150409107.json`确认固定head_sha及实际步骤终态，协调审计 `Monitor994-actual-full-frontend-CI-audit.json`、本会话独立收据 `Monitor994-owner-independent-audit.json`。本轮仅补权威证据文档并执行 `git diff --check`，不重复有效产品门禁；清单业务CI结果由协调会话统一回报，本会话只补文档出版指针，不重复派发或重跑验收。

### 平台告警：严重且未恢复事件优先排序（2026-10-25）

用户确认后先修订正式规范7.1及API专题，再在唯一 `GET /platform/alerts` 的服务端查询中按 `critical AND status IN (open,acknowledged)` 优先、两组各按 `opened_at DESC,id DESC` 排序，均在OFFSET／LIMIT之前。确认和限时抑制不降低优先级，已恢复严重事件进入普通组，普通组仅按时间及ID排列；不依据最近观测时间、不增加置顶字段／配置／参数／权限，不改变过滤、总数或事件／通知生命周期。前端现有 `incidents = result.data` 保留接口顺序，没有第二套页面排序。双语Swagger说明及三个生成产物同步。

新增35条合成历史的真实PostgreSQL回归，明确列出预期ID顺序，覆盖1／7／20条分页、跨类别、同时间ID倒序、严重／警告／活动／已确认／已恢复筛选、当前主机名与告警名称交集、时间范围排除旧优先事件；调用已有确认／抑制管理及reconciler恢复后重新分页核对。旧实现下新增用例确实失败，日志 `/tmp/addp-alert-priority-pg-red.log` 保留。已有严重级别分页用例中混合级别的预期顺序同批按新契约修订，其余单级别断言保留；没有删测、放宽断言或增加兼容排序。

最终实际通过：`make test-module MODULE=monitor GO_ONLY=1` 的7个Go测试包（无测试包仅计编译）；`make test-monitor-postgres` 的40项顶层集成测试，无失败／跳过，含新增12个排序子场景；标准 `gen-swagger.sh monitor` 生成成功，`check-route-coverage.sh monitor` 的91条公开路由方法一致；`git diff --check`通过。运行前Infra标准状态确认实际PostgreSQL映射25432，标准T2只使用 `addp_test`，新夹具退出核对自身incident／event／delivery均零行。主机Owner为受控Reader，恢复使用合成评估调用既有协调器；不计正式System身份、真实指标实采触发／恢复或真实通知渠道验收。

`changed-gate.py --dry-run`已命中Monitor Go与原PostgreSQL入口，新 `TestIntegrationPostgres*` 沿既有标准入口实际自动发现，无新Make／workflow。干跑包含其他会话WIP，只计选择检查，不计执行通过；指标来源、Trace和前端产品代码未变，不重复其既有门禁。新排序个人Safari、正式身份／Hosted及容量／生产T5未验，CI／Hosted继续等专门会话回传；没有轮询、派发、重跑CI／Hosted、重启个人服务、修改个人开发库或设置定时任务。

原始最终日志 `/tmp/addp-alert-priority-{pg-final,go,swagger,swagger-coverage,selection}.log`；十项输入与日志摘要收据 `/tmp/addp-alert-priority-receipt.json` SHA-256 `3b22232e15112b2c14eb30ec21d79d18f7d142ab73d868fcfea935e79e2a3eca`。证据仅为本地共享工作区T0／T1／真实SQL T2，非固定提交CI全绿。清单实际第49行／B48与第51行／B50仍部分实现；本次仅补唯一事件列表的优先顺序，不计大屏滚动、拓扑／影响范围或根因分析完成。

### 告警优先排序个人 Safari：跨页普通组只读复验（2026-10-25）

用户要求后通过原生Safari操作真实Console iframe及既有平台管理员登录，不使用内置浏览器。运行前后直接读取Monitor `/health/live`，均为`69b2f3292fb62aa8b67e8e58ab5e8dc39dc9289b`、实例`7dce643e-b0e3-4f05-909e-12602561d196`，已包含`c723b01a9`排序实现；没有重启个人服务。清除旧名称／级别筛选后，全部状态列表显示466条、24页；默认未恢复集合仅一条“源容量接近额度”警告／未处理（2026-10-23 08:02:39），严重＋未恢复筛选为空。因此本轮不计严重未恢复置顶、确认／抑制后仍置顶的Safari真实场景通过，不创建假事件或操作真实确认／抑制来补样本。

实际点击分页控件读取第1／2／3／24／23页，共抽查86行：各页20／20／20／6／20行，页内发现时间倒序，1→2、2→3与23→24边界均严格递减。首页末行2026-10-23 13:39:08、第二页首行13:34:19；第二页末行2026-10-22 08:23:03、第三页首行2026-10-21 19:28:56；第23页末行2026-10-02 01:42:29、末页首行01:25:49。第二页第7行为上述未处理警告，位于较新和较旧的已恢复严重事件之间，符合普通组按时间混排，不将全部严重事件整体前移。第23页点击刷新、浏览器整页重载后仍保持`status=all&page=23`与相同行序，点击首页后回到省略page的地址且20行与初次相同。不是对24页的全量遍历；末页有同一秒的事件，屏幕未显示事件ID，不据此验证同时间ID倒序。

可访问性文本实际读取等级、状态、时间及分页；已查看的截图证明真实表格、末页6行与24页分页可见，但原生横向滚动报`noWindowsAvailable`，最终截图保存另报ScreenCaptureKit `-3811`，没有生成最终截图文件，也不计右侧列视觉验收通过。只读记录`/tmp/addp-alert-priority-safari-report.json` SHA-256 `1c718df0e17dd06b7cf75d8f7e7a8f3e4b6189bbb8ef3e157d4edf9af0580ddd`，包含五页边界、刷新／重载比较及运行身份；原始各次AX状态`/tmp/addp-alert-priority-safari-states.txt` SHA-256 `2d1704161fc8e709ce614d2bfbe454fb36e8dea2e29e9c147c27c9481fadf16b`。保存后用独立Python校验报告的86行计数、各页总数／倒序标记、三组边界及恢复比较均成功，`git diff --check`通过；本次仅追加文档，没有修改产品代码，无需重跑未变的Go／PostgreSQL门禁。

本轮仅增加个人Safari既有数据集的普通组分页、筛选与恢复证据，严重未恢复置顶／确认抑制及同时间ID顺序仍分别引用此前真实SQL的合成数据回归，不能混作本轮Safari通过。清单第49行／B48“实时告警联动”与第51行／B50“告警信息管理”仍部分实现，正式隔离身份T4／Hosted、真实严重事件、容量及生产T5未新增验收。没有创建、修改、确认、抑制、删除告警／规则或发送通知，没有修改个人库／IAM、操作CI／Hosted或设置定时任务。后续优先补真实严重未恢复与普通组交界的页面证据，继续等专门会话回传，不为补证据主动触发异常或修改个人告警。

### 告警筛选“全部”选项的可读标签修正（2026-10-25）

对应实际Excel第51行／B50“告警信息管理”。前轮Safari已观察到类别、状态、级别在已选“全部”时都显示“请选择”；读取锁定的Element Plus 2.8.8实现确认默认空值包含空字符串，页面恰用空字符串表示不限。先在正式规范7.1明确“全部”为有效选项，再沿共享主机／离线原因选择控件已有方式，仅为本页三个ElSelect显式指定空值为null／undefined。复用现有中英文词条，不增加组件、依赖、查询枚举、参数、权限或后台状态；默认未恢复、`status=all`地址恢复、原服务端排序及事件生命周期均保持。原实现被直接替换，没有占位文字兜底或第二套状态。

新增中英文真实组件浏览器用例在旧产品源码下均失败：期望“全部类别／All categories”实际为“请选择／Select”，日志`/tmp/addp-alert-filter-labels-red.log`及标准runner失败产物保留，退出自有运行目录已清理。修正后同一入口`npm run test:e2e -- e2e/platform-alerts.spec.js --grep 'all alert filters show'`两项通过；逐一切换类别、状态、级别的具体项与全部，核对所选标签、原查询参数增加／省略、刷新及重载恢复，并断言只有GET请求。该HTTP夹具只证明前端映射，不作为后端筛选或正式身份验收。

显式使用已核验Node24.19.0后，`make test-monitor-frontend`退出0：119项单测、438项受控浏览器T3（16.3分钟，无失败／跳过）及Vite构建通过；构建保留既有大chunk警告，不修改阈值来隐藏。`make test-frontend-ci-registration`22项回归通过，21个前端登记完整，现有Platform CI的Monitor矩阵与30分钟期限自动覆盖新增用例，无需改Make或workflow。`changed-gate.py --dry-run`退出0只计自动发现，包含其他会话共享WIP，不计所有选中门禁已运行。验证期间记录的产品／规范／测试及前端配置五项输入在结束时摘要均保持一致；`git diff --check`通过。后端未改，本轮不重跑Go／PostgreSQL门禁或Swagger生成。

个人Safari实际Console iframe已热加载本批工作区源码，中文三个框分别显示“全部类别／全部状态／全部级别”，整页重载后同一`status=all`地址与登录、标签均保持；未操作英文个人偏好，英文显示及两种语言下的筛选项切换由上述独立受控T3覆盖。真实截图为`/tmp/addp-alert-filter-labels-safari.jpg`（工具返回JPEG，3024×1898），前后AX状态同前缀`-safari-state.txt`／`-safari-reloaded.txt`，均已独立读取核对。本轮没有修改真实告警或规则、发送通知、重启个人服务、操作个人数据库／IAM、查询或调度CI／Hosted，也未设置定时任务；不扩大前节严重未恢复置顶、确认抑制、同时间ID与正式隔离身份的证据范围。

最终日志同前缀`-{green,frontend,registration,selection}.log`；输入、日志和Safari产物摘要收据`/tmp/addp-alert-filter-labels-receipt.json` SHA-256 `e78a28c32d56c98269fef8845f6d1c79cd7ccaec9f67d4da537d5df694d3507f`。清单第51行仍部分实现，本次只补筛选可读性；拓扑／影响范围、根因、严重未恢复真实页面、容量与正式身份／Hosted缺口不变。后续服务失联自动告警的判定粒度及预期运行事实已向用户提问，未实现或默认采用任何新口径。

### 平台告警自动筛选与字段提示（2026-10-29）

事件列表删除时间搜索按钮，三个文本筛选复用共享createAutoFilter的300毫秒防抖；类别、状态、级别、确认后的有效时间与点击清空／Enter立即提交完整有效筛选并回第一页。时间继续允许单边界、使用首次发现时间与UTC半开区间，名称仍为首次触发名称、主机仍只查当前主机资源告警，不改变查询、计数或严重未恢复事件优先排序。通知、规则和投递写入行为不变，不新增定时刷新。

主机名称与时间解释改用共享FieldHelp，支持悬停／键盘焦点；非法及反向时间保持就近校验，不发新查询。URL非法边界在未显式修改该边界或清除时间前保持拒绝，修改其他文本或另一个时间边界不能删除该限制。清除时间仅撤去已提交时间条件；手动刷新、翻页、页签／路由／身份／语言变化取消旧待提交输入，隐藏暂停、前台恢复后提交；已选范围、通知页返回与Console单iframe沿原URL恢复。

本轮更正上一批Enter证明：`clock.install()`仍会推进时间，原三项通过不能证明Enter在防抖前提交。改为明确`pauseAt()`后，旧实现四项中仅引擎通过，主机、服务与告警三项失败；没有改变被测输入、放宽等待或推进300毫秒来掩盖失败。共享createAutoFilter新增唯一enter(event)，显式提交并preventDefault，忽略IME组合／229；四页文本框统一复用。修复后四项严格暂停场景通过，原3项与本次失败保留，节点证据已撤去原“冻结时钟”结论；该受控事件测试不代表macOS真实输入法验收。

本地实际验证：

- `npm --prefix monitor/frontend run test:e2e -- e2e/automatic-filter-enter.spec.js e2e/platform-alerts.spec.js`：40项通过，含严格Enter四页及原事件、时间、名称／主机检索、权限、分页与通知规则36项。
- `npm --prefix console/frontend run test:e2e -- e2e/platform-alert-navigation.spec.js`：7项通过，新增中英文自动文本／单边界时间查询、严格暂停下Enter、URL及同iframe；保留原Tenant拒绝、System诊断跳转及规则弹窗。
- `make test-common-frontend`：292项全部通过，含共享Enter／IME／取消与四页唯一所有权。本批未修改其他Owner的ResourceTree组件及清单；之前共享门禁失败不倒改为通过。
- `make test-console-frontend`：219项单元、265个浏览器场景和构建通过。
- `make test-monitor-frontend`：131项单元、512个浏览器场景及构建通过，包含新增5项告警交互及四页严格暂停Enter场景；全部在同一完整套件执行。

新增页面用例覆盖一次防抖合并、选择合并尚未提交文本、清空、隐藏恢复、刷新／页签／撤权取消、非法URL保留、反向时间修正，以及中英文提示和窄屏布局。早期新用例错误定位到只读输入或嵌套相对locator导致失败，已改为正常选择框操作；Console首次误从含Monitor私有Playwright依赖的fixture导入纯日期函数造成双副本，已将同一函数移到既有纯fixture供两门禁复用。未强制点击、修改产品以绕过测试或放宽超时，旧失败日志保留。

沿既有Common Frontend／Monitor／Console测试目录、Make与Platform前端矩阵自动发现，无新依赖、API、Swagger、权限或workflow。未重复运行未变的后端、数据库或Online runner；实际Safari、真实登录／MFA和Hosted未验。不重启个人服务、不修改个人账户或配置；CI／Hosted等专门会话回传，仅标准提交推送，不手动调度／重跑／轮询、不设置定时任务。

日志：`/tmp/addp-alert-auto-filters-{enter,focused,focused-final,common-final,new,new-final,basic-check,console-focused,console-focused-final,monitor-full,console-full}.log`。严格暂停旧实现的三处失败不能计通过，最终门禁与正式运行分层认定。

#### Safari 真实页面复验阻塞（2026-10-29 13:04）

本轮使用 macOS Safari 原有已登录的平台管理员会话，只做页面导航、筛选输入与只读网页检查器诊断。服务页实际读到五个在线后端及当前 CPU／RSS，但文本输入、Enter 均未更新筛选 URL；进入平台告警后 iframe 空白。检查器明确记录 `TypeError: deferred.cancel is not a function`，发生于共享 autoFilter 的取消／可见性路径，告警页同时报 watcher／setup 错误。因此本轮筛选、提示及返回恢复均未获得新版 Safari 通过证据，不能用之前受控 T3 结果替代。

已核对两个真实资源版本不一致：Safari 打开的 `/src/config/portalConfig.js` 仍将主机监控排在服务、引擎、调用链和接口质量之后，磁盘源码已将其前置；Safari 打开的 `/module-ui/monitor/@fs/Users/pampa/code/addp/common-frontend/basic/src/utils/index.js` 中 debounce 仍直接返回旧函数，缺少 cancel，而磁盘同文件已包含 executedFunction.cancel。对该源码页执行 Safari 从源站重新加载后仍看到旧函数。Console 监听进程的工作目录为当前仓库 console/frontend，命令为 Vite 开发服务，并非已确认的旧 worktree 或 dist 服务。

以上证据确认当前页面混用了不一致的资源版本，尚未确定旧响应持续存在于浏览器缓存还是开发服务转换缓存的唯一根因；没有据此添加兼容分支、吞掉 cancel 异常、改源文件触发重载或清空浏览器缓存。本轮未改产品代码，既有提交 b5a99831923efb02fd2cb4b0509c7eff29a5be4b 的本地门禁保持独立证据。临时源码标签页和只读检查器均已关闭，没有修改规则、绑定、账号、数据库或配置。已告知用户通过标准 `./scripts/dev/restart.sh -all` 刷新开发服务，再先核对实际返回源码，随后继续四页筛选／提示／返回恢复；未由 AI 重启或执行 keepalive。CI／Hosted 仍等专门会话回传，未调度、轮询或设置定时任务。

#### 用户重启后的 Safari 交互复验（2026-10-29 22:40）

用户完成标准重启及 Safari 平台系统管理员登录后，本轮沿原 localhost Console 标签只读复验。新版菜单实际为资源总览→主机监控→服务监控→引擎监控→调用链→接口质量→平台告警；告警页正常显示真实事件，前述空白及筛选取消异常未再阻断本轮交互。最初服务／引擎页面明确报告“页面隐藏，自动刷新已暂停”，因此当时文本输入未自动查询不能计为失败或通过；Enter实际可提交。用户手动切换到 Safari 所在桌面保持前台后，主机、服务与引擎显示自动刷新开启，以下自动查询均通过。未修改可见性判定、Safari设置或增加后台查询兜底；重启恢复现象不证明此前旧资源缓存的唯一根因。

- 主机：输入 Docker，无需Enter／搜索按钮，Console与iframe URL同步 search=Docker，真实列表为一台Desktop VM。进入资源详情后点击“返回主机列表”，搜索值、URL和一台匹配结果恢复；详情真实CPU、内存与IO值可读，不把Desktop未提供的文件系统／网络／TCP统计当0。
- 服务：模块输入meta自动查询得到当前一条UP后端；先前Enter也实测提交成功。在线状态下拉直接切换离线，保留module_name=meta，URL增加status=down，显示真实199条离线历史；转到引擎页再点Safari返回，模块／状态／URL及历史列表恢复。前台期间采样时刻由22:36至22:39持续变化，刷新仍沿原能力。服务字段问号通过Safari Option+Tab完整键盘导航获得焦点，真实气泡显示“仅填写服务端点中的主机名或IP，不含协议和端口”，末次截图已目视核对焦点框及就近气泡；普通Tab在当前Safari键盘设置下跳过按钮，未修改该设置，也未把AX按钮的名称当作气泡显示证明。
- 平台告警：输入safari-no-match-20261029自动同步search并显示暂无数据；替换为源容量自动恢复一条真实匹配事件。主机名和首次发现时间两个tip均通过Option+Tab焦点实际显示文本。时间面板选10月24日、点击确定，直接提交当地00:00对应UTC time_from=2026-10-23T16:00:00.000Z并显示空结果；点击清除时间，URL撤去边界且保留search，原一条事件恢复。进入通知管理再回告警事件，名称、URL和匹配结果保留。只读查看通知列表，未点击通知测试／保存、事件确认／抑制或重投。
- 引擎：输入infra自动同步search并得到一条infra-虚拟机；打开该部署监控，再点返回引擎列表，搜索值、URL与一条结果恢复；清空文本自动移除search。当前部署明确显示“未部署采集来源”，专业指标留空，只计交互／状态显示证据，不计PostgreSQL实源已恢复或专业指标实际通过。

本轮只使用原生Safari输入、键盘、可访问性状态与截图，没有浏览器脚本注入、模拟接口、测试身份、配置写入或服务重启。四页均不再显示搜索按钮；本轮未覆盖全部筛选组合、真实IME、完整跨页排序或所有语言／尺寸，不外推为Hosted或全功能验收。原受控前端门禁保持独立有效记录，无产品代码变化，不重复运行测试；证据文档限定diff检查通过。当前旧资源导致的四页交互复验阻塞据本轮实际范围关闭；CI／Hosted仍只收专门会话回传，未调度、轮询或设置定时任务。

### 告警筛选刷新测试的公开 URL 提交时序（2026-10-30）

CI 专门会话回传 [Platform 40891592721／Job 123509561021](https://github.com/pampa0629/addp/actions/runs/40891592721/job/123509561021)：源码 `a62ef412ceff7539ba784184a65c8a8dca9bd268` 的英文自动筛选用例在 reload 后缺少 `time_from`，原精确断言五秒失败，264／265 通过。后继源码 `3f43b9bff04fdccb3d689c3efea8a9b9b783e44d` 的 [Platform 40903636698／Job 123552351917](https://github.com/pampa0629/addp/actions/runs/40903636698/job/123552351917) 实际通过 219 单元、265 浏览器和构建，中英文同一用例均执行。独立读取两份原始日志确认上述事实，未查询或重跑云端。对两版本、当前基线 `1353e06febaf67963dcc79c11bf25a3b79526c29` 和工作区的六个直接产品输入逐文件 SHA-256 比对，告警页、自动筛选、模块导航、导航请求、桥协议和 Console Portal 均一致；审计 `/tmp/addp-alert-publication-input-audit.json`。后继通过保留为其固定版本证据，不计为本次新测试已通过 CI。

现行路由规范要求先在 iframe 内 replace，再异步同步 Console；告警请求由本地路由 watcher 发起，可以早于公开 URL 提交。原测试只等请求，随后立即 reload，缺少公开 URL 的完成断言。用 Console 初始化前安装的消息闸门，仅暂存活动 iframe 原始时间导航消息，沿现有 pending 回执协议保留其 source／origin；受控原用例在 API 和 iframe 已有精确时间、父 URL 尚无时间时刷新，明确复现同样缺失并在原五秒断言失败。日志 `/tmp/addp-alert-publication-red-early.log` 保存父 URL、iframe URL、实际查询和被暂存路由。首次较晚注册的诊断未形成有效隔离，不计为反例。原 CI 没有同一时刻的父子 URL，不能反推出其唯一调度过程；本次证明并关闭的是测试完成条件缺口。

本轮仅补测试及规范：名称／主机／时间变化须先核对 iframe 和 Console canonical 查询提交，reload 后还须观察新请求，避免复用刷新前最后一次请求造成假通过；精确时间值、控件、URL、原请求及单 iframe 断言均保留。新增中英文受控顺序回归验证 API 结果先于公开导航、未知请求 ID 的导航回执不能改写状态、退役旧请求延迟返回不能替换当前结果、放行导航后同一 iframe 不重建，以及刷新恢复时间／名称／主机／类别／严重级别。没有改产品导航、API、权限、超时或引入 sleep，也不增加部署或服务重启。

标准定向入口 `npm --prefix console/frontend run test:e2e -- e2e/platform-alert-navigation.spec.js` 先通过全部 9 项、无跳过、退出 0（2.2 分钟），日志 `/tmp/addp-alert-publication-green-final.log`。随后只在新增两场景补未知导航回执断言，沿同一入口加 `--grep 'earlier API result'`，中英文 2 项最终通过、无跳过、退出 0（1.9 分钟）；原七场景输入未变复用前次有效结果，不将两次运行合计为十一场景。最终补充日志 `/tmp/addp-alert-publication-unknown-final.log`。首次新夹具导入触发两个 Playwright 副本而停在收集阶段，未执行断言、不计通过；已改为本用例最小协议 DTO，没有改依赖或扩大共用夹具。产品输入未变，不重复原有效 Console／Monitor 完整本地门禁、PG、个人实链或 Hosted；本次为受控 T3 测试修复，新增输入的自然 CI 继续等专门会话回传，未轮询、调度或设置定时。

#### 固定修复版本的自然 CI 复核（2026-10-30）

专门 CI 会话回传修复源码 `226d1ad73564b9e681b9f0153f294132b92c2d3d` 的唯一自然 push 验证：[Platform 40959069552／Job 123746756104／attempt 1](https://github.com/pampa0629/addp/actions/runs/40959069552/job/123746756104)。本会话只读复核 checkout 与 CI head，原始日志 SHA-256 与独立审计一致（`620a0cfb04b79575b791a29f6960ca666b61ee329e3b550ae51bdd758688ef2b`）；219 项单元、270 项浏览器及生产构建均实际通过，无 FAIL／SKIP。原中英文自动筛选刷新用例见日志 859—860 行，新增受控发布时间竞态中英文见 861、863 行，总浏览器通过为 989 行，构建通过为 1020 行；精确时间／名称／主机、单 iframe、未知回执与迟到请求断言均属于该固定提交。原始日志 `/tmp/addp-ci-20261029-213834/job-123746756104.log`，审计 `Console-226d-fixed-actual-pass-audit.json`。

据此关闭本次告警筛选刷新中英文旧 CI 故障及上述修复输入的 Console T3 缺口，原失败记录保留。此结论不扩展到 System 其他失败、Business 个人环境采集、产品采集能力新增或 Hosted；Release 条件跳过也不计业务验收。本轮仅补证据并做限定 diff 检查，没有重新执行本地门禁、查询／重跑／调度 CI 或设置定时。

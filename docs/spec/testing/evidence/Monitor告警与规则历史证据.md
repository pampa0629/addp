# Monitor 告警与规则历史证据

[返回 Monitor 历史证据](Monitor历史证据.md) · [返回验收证据索引](../验收证据索引.md)

本专题收录CPU/RSS、告警检索与排序、服务失联、文件系统容量规则。固定源码、环境、失败记录、修复结论与未覆盖范围以各记录为准；本次仅整理文档，没有重跑验收或刷新 CI 状态，不推导当前 HEAD 或生产 T5 已通过。跨主题语境与原有顺序见 [Monitor 历史证据入口](Monitor历史证据.md#原记录入口)。

## 服务实例 CPU 与 RSS 超限告警（2026-10-22）

实现范围：从当前授权运行实例中选择具体实例，分别设置 CPU 占用核数和 RSS 容量阈值；CPU 保持完整一分钟窗口，RSS 表单用 GiB 输入并转换为字节。规则新建停用、实例与指标不可变、更新/删除按 CAS；沿用平台资源规则权限。后台来源、连续窗口、事件协调器和事务 outbox 复用现有实现；`process_resource` 通知类别须显式订阅。只保留 opened/resolved 生命周期，离线、退出、缺样、查询错误和停用不恢复活动事件；重启不迁移旧规则，已有事件保持打开时的恢复条件。

验证结果：

- `make test-module MODULE=monitor GO_ONLY=1`：退出 0，Monitor Go T1 通过；包括固定单位、完整窗口、边界、缺样、重复样本、非法值与 API 权限/严格输入。
- 已执行 `bash scripts/infra/status.sh` 核对本机共享 PostgreSQL 实际映射为 25432；在 `ADDP_TEST_POSTGRES_DATABASE=addp_test` 下通过 `make test-monitor-postgres`，退出 0，33 项顶层 PostgreSQL T2 通过。新增 CPU/RSS 生命周期均覆盖 CAS、重复规则拒绝、活动事件删除拒绝、退出/启动身份改变/采集失败不恢复、恢复条件冻结、有效零值恢复、显式订阅与 outbox 隔离、停用竞争丢弃旧结果；当次夹具执行清理及规则残留核验。
- `make test-monitor-frontend` 中 96 项确定性测试通过；首轮浏览器 285/286 通过，失败仅为新增测试使用错误控件名称。后续新增证据/订阅场景并修正 Element Plus 禁用控件定位和两位小数断言；通过标准 `npm run test:e2e -- e2e/process-rules.spec.js` 定向复验，最终 7/7、退出 0。未修改的 282 项既有浏览器场景复用首轮通过证据，HTTP 与主机告警表单回归包含在内；不把首轮非零结果写成整批重跑通过。`npm run build` 退出 0。
- `bash scripts/swagger/gen-swagger.sh monitor` 与 `bash scripts/swagger/check-route-coverage.sh monitor`：最终均退出 0，90 个公开路由方法覆盖一致。共享源码在生成期间变化导致的一次拒绝已重新生成，不使用该次失败产物作为通过证据。
- 本地日志：`/tmp/addp-process-alerts-go.log`、`/tmp/addp-process-alerts-pg.log`、`/tmp/addp-process-alerts-ui.log`、`/tmp/addp-process-alerts-ui-final.log`、`/tmp/addp-process-alerts-build.log`、`/tmp/addp-process-alerts-swagger.log`。

边界：PostgreSQL T2 使用受控 System/指标来源夹具，浏览器 T3 使用隔离 HTTP 夹具，不能计为真实身份、真实 Prometheus 压力或正式通知端 T4 验收。本轮未重启个人服务，未执行 CI 或 Hosted，等待专门会话回传；未设置定时任务。


2026-10-22 补查 Console 嵌入场景：`console/frontend/e2e/platform-alert-navigation.spec.js` 新增服务实例规则弹窗用例，验证弹窗位于视口内、内容与遮罩透明度、名称输入、真实选择控件的候选选择及关闭；只模拟 HTTP 响应，不写个人配置。标准 `npm run test:e2e -- e2e/platform-alert-navigation.spec.js` 最终 Chromium 5/5、退出 0；`npm run test:e2e -- e2e/platform-alert-navigation.spec.js --browser webkit --grep painted` 的同一新增场景 1/1、退出 0。初次诊断用例的动态夹具导入及控件定位失败已修正，不计产品故障。现有 Console 浏览器门禁自动发现该文件，无需新增 CI 或依赖登记；WebKit 仅为本地诊断，未扩展 CI 浏览器矩阵。个人 Safari 仍出现辅助功能树有表单但用户与截图看不到弹窗，且控制工具有 noWindowsAvailable／ScreenCaptureKit 错误；隔离浏览器通过不能证明此真实故障已修复，仍待前台手动点击复现及根因确认。本轮未修改产品样式，未创建或启用真实规则，未查询 CI／Hosted、未设置定时。

## Safari 服务实例规则弹窗实景复核（2026-10-23）

05:49–05:53（Asia/Shanghai），在用户确认可操作 Safari 后，用原生 CUA 操作已登录的平台系统管理员及真实 Console `/monitor/platform-alerts?tab=process-rules`。检查器读到页面曾 `document.hidden=true`，随后为 `false`；活动弹窗 `display=block`、`visibility=visible`、遮罩和内容透明度均为 1，动画列表为空，标题坐标的命中检测确为弹窗标题，父层命中 iframe。检查器开启时 iframe 高度约 227 CSS px、弹窗顶部约 34 px，标题仍在可见区域；没有取得样式隐藏、动画卡住或其他元素遮挡的证据。期间工具出现用户操作中止及 ScreenCaptureKit 捕获错误，辅助功能树与截图一度不同步。

窗口恢复前台并关闭检查器后，真实截图清楚显示弹窗与遮罩；随后实际关闭、确认列表恢复、重新打开，以及展开真实实例候选下拉层均正常，截图包含 Monitor／Meta／Transfer／Manager／System 当前候选。保持表单未保存，未创建、启用规则或发送通知。本轮仅补充证据，没有修改产品代码、样式、浏览器设置或重启服务。此前现象与后台窗口／重绘不同步相关的判断得到支持，但未建立 Safari 底层异常的稳定复现，不称为代码根治或全部告警 T4 通过。既有 Chromium 5/5、WebKit 弹窗专项 1/1 是隔离 T3 证据，本次是个人开发环境只读 UI 复核，分别陈述。CI／Hosted 仍等专门会话回传，未查询或调度、未设置定时。


## 服务实例 CPU/RSS 真实采样告警链路 T2（2026-10-23）

沿原标准入口新增 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=process-alerts`，只使用门禁自建非 root 容器的 0.25 CPU／128 MiB 原硬上限。三个真实 Common SDK 进程经原 mTLS／唯一 HTTP SD 进入 Prometheus；其中一个 Worker 在加压前固定 CPU 触发／恢复值 0.025／0.008 核，RSS 按真实空闲基线加 12／8 MiB 固定阈值，随后有界计算并持有 24 MiB 匿名内存，释放后由自然新样本判断恢复。测试控制仅存在于测试二进制，单次最多 180 秒，未改生产采集器或个人服务。

冻结基线 `9f6e77140d9576dc8ef2419082f765175232f312` 加本批七项输入，在隔离源码副本执行：

- `make test-module MODULE=monitor GO_ONLY=1`：退出 0，Monitor Go T1 通过。
- `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=process-alerts`：退出 0；48 项配置／分组回归、八项固定窗口测试及真实进程告警专项通过，无专项 Skip。专项实际耗时 245.08 秒：CPU 打开证据为 0.0269–0.0511 核，释放后的恢复证据为 0.00378–0.00400 核；RSS 从约 30.8 MiB 基线增至约 57.6 MiB，恢复证据约 30.4–30.6 MiB。两项均使用推进中的连续样本，CPU 保留完整一分钟，不改写计数器或历史；两个原事件分别恢复，没有新增结束状态。四个 opened／resolved 事件共收到八次回环 TLS 请求，每个事件首次 503、随后成功，投递 ID／事件 ID／正文和持久证据均一致；旧日志订阅未收到该类别，对等 Backend 与 Worker 的三项当前值继续有效。退出明确报告 owned 容器／网络／卷及设施临时目录零残留，并独立按当次项目标签读回核实。
- `make test-release-runner`：退出 0，62 项发布／登记回归及正式登记检查通过；`make test-workflow-security`：退出 0。新增场景已加入原 Release Metrics 矩阵，仍保持每项 20 分钟及所有分组真实成功才通过的汇总要求；默认全量入口包含原分组及新专项，无第二套执行路线。

原始日志及输入摘要保存在 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-process-alert-real-r7sgulbj/` 的 `process-alerts-t2.log`、`monitor-go.log`、`release-registration.log`、`workflow-security.log` 与 `inputs.json`；七项工作区输入与冻结副本哈希一致。此结果为真实 SDK／Prometheus／PostgreSQL／签名接收端下的 T2，System 当前用户与身份投影仍是夹具，不替代正式密码／MFA、真实 Console、Hosted T4、生产容量或 T5。本轮未运行其他 Metrics 分组，复用原有证据边界；未查询／调度 CI 或 Hosted，等待专门会话回传，未重启个人服务、未设置定时任务。


## CPU/RSS 告警专项的就绪分段与失败诊断（2026-10-23）

专门 CI 会话回传 [39879255330 / 120203117355](https://github.com/pampa0629/addp/actions/runs/39879255330/job/120203117355)（`691441e81c5069f2d2d10903b0623d49135fdf17`）和此前同症状失败：已打开两个原事件并持久化签名通知，RSS 恢复，CPU 仍为 risk，场景在 270 秒退出。旧日志只打印 LastValue 指针，没有 CPU 数值或实际释放时刻，不能据此断言 CPU 未释放或生产恢复逻辑错误。代码确认冷来源的完整 CPU 基线与规则创建后预热串行消耗同一场景预算；本机新增诊断还确认抓取的毫秒抖动可使相邻样本只有 14.997 秒，严格持续窗口需要第三个样本。

本批只调整测试设施：准备阶段以原 65 秒等待确认三个精确来源的完整 CPU 历史；Go 场景区分规则创建后完整 CPU 窗口、空闲正常、真实 RSS 释放和同一事件恢复。保留来源八分钟寿命、270 秒总预算、各阶段 100 秒、可执行测试 300 秒、0.25 CPU／128 MiB、固定阈值、全部生命周期与签名投递断言。未修改生产采集／查询／告警判断、权限或个人服务。失败日志增加规则数值、评估／采样时刻及有界窗口查询；来源准备失败的诊断只保留固定信号与数值，不输出端点、任意标签或凭据。

冻结基线 `0c3946573e375da4cbd10bd78793e2eeaf17ed34` 加本批输入，证据目录为 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-process-alert-repair-x2oq49qd/`：

- `make test-module MODULE=monitor GO_ONLY=1`：最终输入退出 0，见 `monitor-go-t1-final.log`。新增确定性回归验证不足 15 秒不能舍入恢复、第三个推进样本及规则创建后完整 CPU 分钟边界。
- `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=process-alerts` 的最终尝试：前置 51 项配置回归通过，包括真实零、缺失／重复／替换来源拒绝及诊断去除私有标签；整轮未通过。`TestIntegrationMetricsNetworkWindow` 的 promtool 已打印 SUCCESS，但容器命令在原 30 秒期限被终止，后续固定窗口进程亦未正常结束；尚未进入 CPU/RSS 专项。见 `process-alerts-t2-final.log`，不能把前置单测或 SUCCESS 文本计为 T2 通过。
- 此前两次编排尝试分别在空闲预热 100.40／100.43 秒失败，均未加压；前者证明缺样期间起点重置，后者记录低于固定恢复阈值的有效 CPU 值但严格连续窗口仍未就绪。第三次在 65 秒 CPU 来源准备阶段失败，旧版未保留该阶段的原始数值，原因未确认；新诊断的安全序列化仅有单测证据，不能反推这次准备失败已修复。见 `process-alerts-overlap-failed.log`、`process-alerts-cadence-failed.log`、`process-alerts-source-readiness-failed.log`。
- 四次项目的容器／网络／卷已逐项按精确 Compose 项目标签核对为零，见 `failed-project-cleanup.json`。最终尝试的自动清理曾失败，后续确认无容器、网络无端点且归属标签匹配后，仅删除该项目遗留网络／卷；人工清零不改变原门禁失败结果。

该批次当时的结论为测试编排修正与诊断代码已落地、Go／配置回归通过；该批输入的真实 CPU/RSS 触发与恢复 T2 **未通过**，Linux CI 与正式身份 T4 也未确认。源码输入见 `input-sha256.json`；未自行查询、触发 CI／Hosted 或设置定时，不将上述历史 T2 通过外推到该批。后续固定版本的 Linux 通过证据见下一节，不改变本段各次失败及清理记录。

## 服务实例 CPU/RSS 告警原生 Linux 固定版本复核（2026-10-23）

专门 CI 会话回传 [Release 39908130012 / job 120318541196](https://github.com/pampa0629/addp/actions/runs/39908130012/job/120318541196)，push、attempt 1，实际检出 `dfa4ad16105d0421af13e55f3e4099acdd6751a2`。已独立复核原始日志与该提交的测试源码：`Verify isolated metrics lifecycle` 实际运行 `make test-monitor-metrics`、`MONITOR_METRICS_TEST_ARGS=--case=process-alerts`，`TestIntegrationMetricsProcessAlertLifecycle` PASS，213.73 秒；退出报告 owned 容器／网络／卷及设施临时文件零残留。此处接收并复核既有运行，没有再次查询、调度 CI／Hosted 或设置定时。

规则在加压前固定 CPU 触发／恢复 0.025／0.008 核，RSS 为真实空闲基线加 12／8 MiB，连续窗口各 15 秒；保留完整一分钟 CPU 历史、原资源上限与生命周期断言。真实计算／持有匿名内存后打开两个独立事件，CPU 触发证据 0.03378～0.05800 核，RSS 62,111,744 B；释放后确认真实 RSS 回落，原两个事件恢复，CPU 恢复证据 0.00267 核，RSS 34,353,152～36,708,352 B。触发／恢复各证据窗口含两个推进样本，恢复样本均在释放之后。四个不可变 opened／resolved 事件、四项成功投递、八次签名 TLS 收件断言通过，每事件首次 503 后重试的投递 ID、事件 ID 及正文一致；旧日志订阅零误投，另外两个进程继续采集，测试清理断言当次规则零残留。

原始日志与独立审计位于 `/tmp/addp-ci-20261023-120029/`：`job-120318541196.log` SHA-256 为 `75c5b5f66faa6585d7cbf4a16024aebec2c5024740dfe7d3a05e58134cb56571`，`dfa4-process-alert-lifecycle-frozen.go.txt` 为 `15605f1114d79fba801d09df548c558f4df111e6c1eba235379493732ffb010e`，后者与 `git show dfa4ad16105d0421af13e55f3e4099acdd6751a2:monitor/backend/internal/service/process_alert_lifecycle_prometheus_test.go` 的哈希一致。另有 `Monitor-dfa4-real-process-independent-audit.json` 与 `monitor-dfa4-artifacts.json`；该 run 无下载产物，证据为原始日志及固定源码断言。该结果仅证明该 SHA 的原生 Linux 实源 T2，身份仍为 T2 夹具，不替代正式密码／MFA 页面 T4、长时段、生产容量或当前 HEAD 全批验证；`6dba`／`691`／`9e5`／`d01`／`f6` 等原失败保留，不以此次成功推定其历史根因均已修复。主机五项列表摘要、热力面板及后续前端修复另按各自证据判断。

## 平台告警级别组合筛选（2026-10-24，本地）

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


## 平台告警发现时间范围查询（2026-10-24，本地）

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

现有 owner／路径自动发现、PostgreSQL `TestIntegrationPostgres` 与前端标准入口已实际命中新用例，不新增 Makefile／workflow。T3 使用受控 API，不代替正式身份、个人 Safari 或 Hosted；没有重启个人服务、修改个人业务库或 IAM，没有查询／重跑 CI／Hosted或设置定时任务，相关结果继续等专门会话回传。


## 发现时间查询：个人 Safari 重启后只读复验（2026-10-24）

用户完成重启后，`curl --silent --show-error --max-time 10 -w '\nHTTP %{http_code}\n' http://localhost:8100/health/ready` 返回 HTTP 200，Monitor 已登记、本地依赖与 System 登记均 ready。运行源码为 `f651ad550fa41a942d40dccbf782ae4d02c51f10`，已用 `git merge-base --is-ancestor` 确认包含本功能提交 `ea320ab686f9fe40c36ee628284e4de0049ae547`。

在个人 Safari 现有平台系统管理员会话，通过真实 Console `/monitor/platform-alerts` 只读复验：截图可见发现时间控件和就近说明；结束时间单边界可查到现有告警；相等时间草稿提示无效并禁用搜索；查询 10 月 24 日至 25 日后现有 10 月 23 日告警被排除，整页刷新后日期与空结果保留；扩大起始日期至 23 日后该告警重新显示；组合警告级别后，清除时间保留 `severity=warning` 及现有告警。日期框使用本地时间，URL 使用对应 UTC。未创建、确认、抑制或删除任何告警。

操作记录 `/tmp/addp-alert-time-safari-result.json`，最终 AX 状态 `/tmp/addp-alert-time-safari-state.txt`，截图 `/tmp/addp-alert-time-safari.png`，摘要内记录两份文件的 SHA-256。当前只有一条现有告警，本次不证明多页或微秒边界；这些范围仍分别依赖上文 PostgreSQL T2／受控浏览器 T3。个人 Safari 结果不升级为正式隔离身份 T4、Hosted 或生产证据；CI／Hosted 继续等专门会话回传，本次未查询或触发，也未设置定时任务。


## 平台告警名称检索与触发时名称证据（2026-10-24—25，本地）

对应已只读核对的 Excel 第51行、B列50“告警信息管理”。只读调查确认平台 incident 原来没有名称，规则有当前 `name`，但旧 JSON 证据只存规则 ID／版本和采样事实；页面“异常原因”是指标／信号译名，Tenant 的 `rule_name` 属于独立领域。用户确认采用首次触发名称、缺失历史不补造后，先修订运行观测规范7.1，再将规则名称保存到已有 resource／HTTP／process JSON 证据的 `rule_name`。后续评估／恢复沿 opening 证据，不用改名后的当前规则补写，旧记录仍缺失；日志名称按固定 signal 目录与当前请求语言派生。无新表、数据库列、DDL、权限、角色或通知协议。

唯一 `GET /platform/alerts` 增加 `search`，列表只读 `name` 与查询使用同一事实源。支持最多100个Unicode字符、去首尾空白、大小写不敏感的字面包含，百分号／下划线／反斜线不作通配符；非法UTF-8、控制字符、超长和重复参数拒绝。名称与类别／状态／主体／级别／发现时间取交集，先过滤再计数／分页，保留原排序。名称列与异常原因分开，旧规则告警显示“未记录名称”；名称草稿提交／清除回首页，刷新／重载／通知往返恢复已提交条件，错误与迟到响应沿原清理机制。

| 标准入口 | 实际结果与覆盖 | 本机证据 |
| --- | --- | --- |
| `make test-module MODULE=monitor GO_ONLY=1` | 最终7个Go测试包通过，仅计Go T1；API参数及原身份守卫、名称目录与缺失历史处理 | `/tmp/addp-alert-name-go-verified.log` |
| `make test-monitor-postgres` | 最终38项顶层T2通过，无跳过；其中名称查询16子场景覆盖Unicode／大小写／字面通配符／注入文本／中英日志及接收器后缀／多页计数与时间、级别、状态交集。三个真实评估器的触发／改名／恢复名称保持；删除规则后历史仍可检索；模拟旧证据缺少名称时，当前规则及后续恢复不回填。新查询夹具清理核对零行。仅用已核实25432端口的 `addp_test`，未触及个人业务库 | `/tmp/addp-alert-name-postgres-final.log`，SHA-256 `1f18856d031502aef2aab863a2fc166f273eb3a45c01b6eba406f6e274ea4904` |
| `make test-monitor-frontend` | 最终117项T1、424项受控浏览器T3和Vite构建通过；含中英文、680px窄屏、逐字键入、字面名称检索、页码重置、多页／重载／通知往返、缺失名称提示、非法URL拒绝和失败清空 | `/tmp/addp-alert-name-frontend-final.log`，SHA-256 `1e3bffb892e98c99befc8d3f1489741dedee269a870205b303778e4b3611e6b7` |
| `make test-console-frontend`；Console目录下 `node ../../scripts/test/frontend-browser-gate.mjs e2e/platform-alert-navigation.spec.js` | Console195项T1、219项T3及构建先通过；随后名称输入草稿修复不改Console文件，最终源码再通过5项真实iframe导航专项，覆盖名称／级别／时间条件及同一iframe重载恢复。不将先前整套冒充修复后整套重跑 | `/tmp/addp-alert-name-console.log`、`/tmp/addp-alert-name-console-navigation-final.log` |
| `bash scripts/swagger/gen-swagger.sh monitor`；`bash scripts/swagger/check-route-coverage.sh monitor` | 生成成功、91条公开路由方法覆盖一致，名称query与列表投影、JSON证据同步 | `/tmp/addp-alert-name-swagger.log`、`/tmp/addp-alert-name-swagger-coverage.log` |

首轮Go构建因两处旧测试夹具返回类型未同步而失败，修订为实际列表投影后通过，原失败 `/tmp/addp-alert-name-go.log` 保留。首轮完整前端为422通过／2失败，失败是名称框只绑定已提交路由值、未保留输入草稿，键入被控件还原；逐字键入专项也复现，两份原失败 `/tmp/addp-alert-name-frontend.log`、`/tmp/addp-alert-name-typing-final.log` 不计通过。改为独立 `v-model` 草稿并沿路由／身份同步后，最终上述117／424完整门禁通过，没有加定时查询或自动重载兜底。

30个任务实现／测试／契约文件收据 `/tmp/addp-alert-name-inputs.json` SHA-256 `c6a539735647b78d7716ef188435fe4f7680a7ed559f28072728adf9ea8438b0`，最终内容摘要核对无漂移；本节只追加证据。现有Monitor、Console路径选择、PostgreSQL自动发现和前端标准入口已命中新用例，无新增依赖、测试入口或workflow。以上是本地共享工作区T1/T2及受控API的T3，不代表固定提交CI、个人Safari、正式身份T4／Hosted或生产T5。没有重启个人服务、修改个人配置、创建正式告警或运行CI／Hosted，也没有设置定时任务；正式结果继续等专门会话回传。


## 名称检索固定提交 CI：PG通过，前端浏览器被取消（2026-10-25）

专门CI会话回传后，本会话仅核对已落地的原始日志与Job元数据，没有查询、重跑或触发CI。两份日志checkout均为 `a6048b9e71b0689893a3cf90db5a0ef4822d8866`：

- [Release/T2 Run 40110942812／Monitor PostgreSQL Job 120933711310](https://github.com/pampa0629/addp/actions/runs/40110942812/job/120933711310)：真实 `Run Monitor PostgreSQL gate` 步骤及Job均为success，原始日志实际有38项顶层测试通过，含名称检索16子场景、三个规则评估器和旧名称不回填；无FAIL或SKIP。该证据可计为此固定提交的PostgreSQL T2通过。
- [Platform Run 40110942794／Monitor frontend Job 120922337484](https://github.com/pampa0629/addp/actions/runs/40110942794/job/120922337484)：117项单测实际通过，424项浏览器只出现至363项进度即 `The operation was canceled`；真实测试步骤及Job为cancelled，没有整套浏览器通过或Vite构建记录。后续summary步骤的success只表示摘要写入成功，不能作为产品门禁通过。不能将“363项进度”当作363项已通过，也不能把本地424项或旧时间筛选的421项结果替代本次CI全套。

证据目录 `/tmp/addp-ci-20261024-180033/`，原始日志 `job-120933711310.log` SHA-256 `8754ba1551e3fc6738e372ef2dc60b14c583bad53eb2ee5123d457ea0d4973f8`、`job-120922337484.log` SHA-256 `d2f1dc03e6a0b1c54afc6df56f383db885822659e1d2146b94af555060cfdeb0` 均独立核对一致；同时读取 `jobs-40110942812.json`、`jobs-40110942794.json` 的实际步骤终态及协调审计 `Monitor-a604-actual-CI-audit.json`。后继Platform仍由专门CI会话跟进，复用前须证明名称功能及共享输入等价。本会话不接管其他owner的失败，不另派验收、不设置定时任务；本地、个人Safari、正式身份／Hosted和CI范围继续分别记录，功能清单第51行仍为部分实现。


## 名称检索前端 CI 取消原因更正与限定预算修复（2026-10-25）

专门CI会话补回check-run原始annotations后，独立核对 `/tmp/addp-ci-20261024-200030/Monitor-a604-check-annotations.json` 与 `Monitor-f2ca-check-annotations.json`：两轮均明确 `The job has exceeded the maximum execution time of 20m0s`。取消应归因于前端Job的20分钟上限；此前协调按其他门禁失败时间关联取消的推断不成立，不能说由Common／Redis失败联动取消，也不据此接管这些owner。

[Platform Run 40119830116／Monitor frontend Job 120948753235](https://github.com/pampa0629/addp/actions/runs/40119830116/job/120948753235) 为push attempt1、源码 `f2ca8c25e3a69bba0e50d9ad6412cfd51bb02fce`；Job实际18:15:04Z开始、18:35:22Z结束，测试步骤18:16:02Z开始、18:35:19Z取消。117项单测通过，浏览器到423/424进度后中止，无完整通过报告或build。与 `a6048b9e71b0689893a3cf90db5a0ef4822d8866` 比较，Monitor前端、Common Frontend、Node版本、Makefile及唯一browser launcher均无源码差异，不能用旧ea320的421项结果覆盖新424项。该轮与a604原轮仍计取消，不计T3全套／构建通过；原38项PG和本地完整门禁的既有证据范围保留。

核对现行启动与安装链：`prepare-frontend-gate`沿固定Node／锁文件安装，Job随后安装Chromium，再执行唯一 `make test-monitor-frontend`。Monitor配置显式单Worker；共享隔离按整轮门禁分配端口、缓存与输出，不能仅凭此推断提升Worker并发安全。本批保留117单测、424浏览器、所有断言、单Worker与构建，不加skip／重试，不更改产品或单用例超时。只将Monitor矩阵项登记 `timeout_minutes: 30`，Job沿该项／缺省20分钟读取；其他前端仍20。30分钟覆盖已测接近20分钟的完整串行规模，并为安装、剩余用例、构建及退出留有受限余量，不能把增加预算本身记成远端通过。

同步CI规范与现有前端登记检查：拒绝丢失Monitor的30分钟例外、越界／非数字／重复预算、改变全局20分钟默认或给其他owner扩预算。先增加测试，标准 `make test-frontend-ci-registration`因缺失新校验函数失败（22项中9个错误含子场景），补齐唯一校验和矩阵声明后，同一入口22项及当前21个前端实际登记通过；`python3 scripts/test/changed-gate_test.py`标准脚本25项通过，独立调用同一影响矩阵确认workflow改动自动选择Monitor。`git diff --check`通过。本批仅改CI编排／登记／文档，产品、用例和依赖未变，不重复本地15分钟整套或重跑旧超时运行。推送只沿main正常触发，后继实际117＋424＋build是否全过继续等专门CI会话回传，不自行轮询、重试、派发Hosted、重启用户服务或设置定时任务。

原始后继日志 `/tmp/addp-ci-20261024-200030/job-120948753235.log` SHA-256 `580d23329fc832e57792bd5f66e330627eed281e0d14ba5ebb057546c9e0e15d`；两份annotation文件SHA-256分别为 `a240f2e0643b2676aca613f5f7335a1f9a9a7f8105aee8c4860b40542a193e6a`（a604）、`1cf37185d974dd0961e1bc2c152580aeada267ea50ddb7ddea56550c40343e3d`（f2ca）。本地红／绿与选择回归日志 `/tmp/addp-monitor-ci-budget-{red,green,selection}.log`。名称检索个人Safari、正式身份／Hosted缺口仍独立，功能清单第51行保持部分实现。


## 名称检索前端 CI 预算闭环：固定 a292 整套通过（2026-10-25）

专门CI会话回传后，本会话仅独立核对落地日志、Job步骤、日志摘要和Git输入差异，未轮询、重跑或触发CI。主验收为唯一自动push [Platform Run40128534338／Monitor frontend Job120975073843](https://github.com/pampa0629/addp/actions/runs/40128534338/job/120975073843)，attempt1、完整源码 `a2928b9a76a749f0ca5002754c2bc12d118e31fe`。真实 `Run Monitor frontend gate` 步骤和Job均success；原始日志实际117项单测通过、fail／skip均0，424项受控浏览器全部通过（22.4分钟），Vite构建成功（10.78秒），并非仅摘要步骤成功。Job实际24分钟左右完成，证明原20分钟预算不能容纳这轮完整门禁，Monitor限定30分钟可完成本次全套。

名称功能提交 `a6048b9e71b0689893a3cf90db5a0ef4822d8866` 至该验收提交仅六项文件变化：workflow、前端登记检查及其测试、CI规范与证据文档、retention门禁模拟测试。已用Git实际比较确认Monitor产品／前端／424用例、Common Frontend、Node版本、Makefile和标准browser launcher没有差异；单Worker保留，其他前端仍20分钟。因此该固定版本可闭合本批名称前端T1/T3／build及其限定CI预算适配，原两轮20分钟超时继续保留为未通过，原38项PostgreSQL CI另有独立证据。

同SHA的既有GitHub schedule [Run40132268218／Job120986322295](https://github.com/pampa0629/addp/actions/runs/40132268218/job/120986322295) 亦实际117／424／构建通过（浏览器21.9分钟、构建10.53秒）；它是既有workflow的调度，不是本会话创建定时或手工追加运行，只作补充稳定性证据，不替代前述唯一push主验收。

证据目录 `/tmp/addp-ci-20261024-220030/`：两份原始日志 `job-120975073843.log` SHA-256 `04a94d867b231c443c58ab27e32b73038567a6081312f178fe9be68ea5da711a`、`job-120986322295.log` SHA-256 `004918f057e8ce35f6b23e1397e2df8dcd88a72359f76e18a5d7a04aface9388` 均核对一致；两份jobs元数据确认实际步骤与head_sha。协调审计为 `Monitor-a292-actual-full-frontend-CI-audit.json`，本会话核对收据 `Monitor-a292-owner-independent-audit.json`。本轮仅追加文档并执行 `git diff --check`，不重复仍有效的本地测试。CI受控浏览器不替代用户重启后的Safari名称检索、正式身份／Hosted T4或整行其他开发缺口；第51行仍为部分实现。

## 告警名称检索个人 Safari 只读复验（2026-10-25）

用户完成重启后，`curl --silent --show-error --max-time 10 -w '\nHTTP %{http_code}\n' http://localhost:8100/health/ready` 实际返回200、ready／registered；运行源码为 `7b3a5d6cac630ac76edbfdebda61e2ee89f674cf`，Git祖先检查确认包含名称功能 `a6048b9e71b0689893a3cf90db5a0ef4822d8866`。在原生Safari现有平台系统管理员会话，通过Console及Monitor iframe的真实页面复验，不使用内置浏览器、不提取登录凭据。

首次读取仍是重启前保留的页面内容，告警名称显示“未记录名称”；完整重载后，现有日志告警正确显示固定信号名称“源容量接近额度”。名称框先输入“源”、再追加“容量”，草稿分别保留；提交后Console及iframe地址均保留 `severity=warning&search=源容量`，列表返回原有一条告警。替换为“不存在的告警名称”并提交，真实列表显示“暂无数据”；清空名称后 `search` 移除、“警告”级别保留、原有告警重新显示。再输入匹配名称，整页重载及通知管理→告警事件往返均恢复相同名称、级别和一条匹配结果。原生AX状态与实际截图均核对了名称输入框、名称列及查询结果。

本次只读查询，没有创建、确认、抑制、删除告警／规则或操作通知发送；当前数据仅一条日志告警，不能据此宣称真实多页、规则改名／删除后的名称冻结、旧规则缺名不补造或大小写／通配符专项在Safari通过，这些仍分别引用已有T2和受控T3范围。该证据只计个人Safari页面复验，不代替正式隔离身份T4／Hosted或生产T5；功能清单第51行仍部分实现，设备名称检索、高等级置顶、拓扑／影响范围及根因分析仍有缺口。

本机证据：`/tmp/addp-alert-name-safari-result.json`，匹配／刷新／空结果／清除／通知往返的 `addp-alert-name-safari-{match,refresh,empty,clear,return}.txt`，匹配／空结果／最终页面的 `addp-alert-name-safari-{match,empty,final}.png`，均位于 `/tmp/`。本轮只追加本节，执行 `git diff --check`；不重复仍有效的产品门禁，不查询或触发CI／Hosted，不重启服务、不设置定时任务。清单只读核对Excel第51行、B列50“告警信息管理”，由维护会话统一更新M列。

## 当前主机名称筛选：事实来源与本地分层验证（2026-10-25）

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

现有路径选择、Monitor PostgreSQL自动发现及前端入口已命中，`changed-gate.py --dry-run`实际选择Monitor和Console；无新增入口、依赖或workflow。Monitor仍沿既有30分钟完整前端预算，CI规范删除易过期的“当前用例数”表述，不改变预算、Worker、断言或重试。没有轮询、手工派发或重跑CI／Hosted，也没有个人数据库写入、服务重启或定时任务；提交后只沿main既有push触发编排。

证据 `/tmp/addp-alert-host-name-{go,go-final,postgres,frontend,alerts-final,build,console,console-full,swagger,swagger-coverage}.log`，授权覆盖及影响选择回执同前缀；`/tmp/addp-alert-host-name-inputs.json`记录22项实现／测试／契约摘要、唯一测试导入修正与全部日志SHA-256。真实Safari尚未加载本批后端，当前主机名称筛选的个人运行／正式隔离身份／Hosted未验，不沿用前一批“告警名称”Safari证据代替。Excel第51行仍为部分实现，高等级置顶、拓扑／影响范围及根因分析等缺口保留。

## 当前主机名称筛选个人 Safari 只读复验（2026-10-25，排除与恢复）

用户完成重启后，`curl --silent --show-error --max-time 10 -w '\nHTTP %{http_code}\n' http://localhost:8100/health/ready` 实际返回200／ready，Monitor已登记，运行提交 `5f90384a19a06edff2a8a2bc61a65375ea31c857`，包含产品提交 `994db320ae7147cb0e566db4960eb256306cc9e1`（`git merge-base --is-ancestor`退出0）；Build ID为 `20261025T023212Z-monitor-81346-3DgCB9`。使用个人Safari既有平台系统管理员会话，在真实Console完整刷新后复验；没有读取登录凭据或使用受控API代替个人运行数据。

先在“主机监控”读取当前纳管名称 `Docker Desktop Linux VM（Business / Infra）`。告警页“警告＋全部状态”实际显示5条日志链路告警，增加告警名称“源容量”后为2条；主机名称分段输入 `dOcKeR Desktop`，草稿保留，提交后Console与iframe URL均包含 `host_name=dOcKeR+Desktop`，显示空结果且无查询错误，截图可见输入框及旁边“仅查询主机资源告警”的说明。整页刷新、通知管理只读往返后，主机名、告警名、级别及全部状态均恢复；未知名称“不存在的测试主机”同样为空。清空主机名称后URL移除 `host_name`，其余三个条件保留，原2条日志告警恢复。

本次证明个人Safari输入、非主机事件排除、空结果、清除及条件恢复；**该警告／全部状态数据集没有主机资源告警，真实主机告警正向命中及大小写匹配不计Safari已验**。真实多页主机告警、改名／停用、权限变更与正式隔离身份T4／Hosted／生产T5仍未验；已有本地T2／受控T3的覆盖保持独立。未创建、确认、抑制、删除告警或规则，未操作通知发送或更改主机。

原始AX、截图及ready响应位于 `/tmp/addp-alert-host-safari-*`；操作汇总 `/tmp/addp-alert-host-safari-result.json`（SHA-256 `03c55fa2e39f830ab01b2c52e0b836290af992265a0625dbcb1298e82f64278f`）记录各文件摘要，完整过滤状态引用 `refresh.txt`／`return.txt`，不把无变化的差分AX当完整页面。本轮仅追加证据并执行 `git diff --check`，产品未变，不重复有效产品门禁。没有查询、触发或重跑CI／Hosted，也未设置定时任务，继续等专门会话回传。清单第51行仍部分实现；本次只补个人Safari排除与恢复证据。

## 当前主机名称筛选固定 994 前端 CI：完整门禁通过（2026-10-25）

专门CI会话回传后，本会话独立核对已落地原始日志、Job及实际测试步骤终态与内容摘要，没有轮询、手工派发或重跑CI。[Platform Run40150409107／Monitor frontend Job121040604365](https://github.com/pampa0629/addp/actions/runs/40150409107/job/121040604365) 为push／attempt1、固定源码 `994db320ae7147cb0e566db4960eb256306cc9e1`；真实 `Run Monitor frontend gate` 和Job均completed／success，日志实际118项单测通过、fail／skip均0，428项浏览器以单Worker全部通过（18.7分钟），Vite构建成功（7.01秒）。包括主机名称规范URL及非法条件保留单测、当前主机名称筛选与分页／刷新／导航恢复的中英文真实页面用例。该固定版本已具备一次完整绿色的前端T1／受控Linux T3／build证据，不再只有上一节的本地组合结果；本地首轮两项测试导入失败仍保留为失败。

同Run的Platform consistency Job121040604338仍因独立的部署发现observer退出核验失败；不能把此Monitor Job成功写成整轮CI全绿，也不能把通用清理失败当作主机筛选业务失败。该证据不代替个人Safari、正式身份／Hosted、真实运行环境或生产T5，不扩展为当前HEAD整套产品已验。后续 `05b232920` 仅补退出诊断，原清理故障根因未定，其实际结果继续等专门CI会话回传。

证据目录 `/tmp/addp-ci-20261025-020029/`：`job-121040604365.log` SHA-256 `6af0ee1d76fb65d2713185e59c7c8657ccc9bc3af7855e2012090f92f65d874d`与协调审计一致；`jobs-40150409107.json`确认固定head_sha及实际步骤终态，协调审计 `Monitor994-actual-full-frontend-CI-audit.json`、本会话独立收据 `Monitor994-owner-independent-audit.json`。本轮仅补权威证据文档并执行 `git diff --check`，不重复有效产品门禁；清单业务CI结果由协调会话统一回报，本会话只补文档出版指针，不重复派发或重跑验收。

## 平台告警：严重且未恢复事件优先排序（2026-10-25）

用户确认后先修订正式规范7.1及API专题，再在唯一 `GET /platform/alerts` 的服务端查询中按 `critical AND status IN (open,acknowledged)` 优先、两组各按 `opened_at DESC,id DESC` 排序，均在OFFSET／LIMIT之前。确认和限时抑制不降低优先级，已恢复严重事件进入普通组，普通组仅按时间及ID排列；不依据最近观测时间、不增加置顶字段／配置／参数／权限，不改变过滤、总数或事件／通知生命周期。前端现有 `incidents = result.data` 保留接口顺序，没有第二套页面排序。双语Swagger说明及三个生成产物同步。

新增35条合成历史的真实PostgreSQL回归，明确列出预期ID顺序，覆盖1／7／20条分页、跨类别、同时间ID倒序、严重／警告／活动／已确认／已恢复筛选、当前主机名与告警名称交集、时间范围排除旧优先事件；调用已有确认／抑制管理及reconciler恢复后重新分页核对。旧实现下新增用例确实失败，日志 `/tmp/addp-alert-priority-pg-red.log` 保留。已有严重级别分页用例中混合级别的预期顺序同批按新契约修订，其余单级别断言保留；没有删测、放宽断言或增加兼容排序。

最终实际通过：`make test-module MODULE=monitor GO_ONLY=1` 的7个Go测试包（无测试包仅计编译）；`make test-monitor-postgres` 的40项顶层集成测试，无失败／跳过，含新增12个排序子场景；标准 `gen-swagger.sh monitor` 生成成功，`check-route-coverage.sh monitor` 的91条公开路由方法一致；`git diff --check`通过。运行前Infra标准状态确认实际PostgreSQL映射25432，标准T2只使用 `addp_test`，新夹具退出核对自身incident／event／delivery均零行。主机Owner为受控Reader，恢复使用合成评估调用既有协调器；不计正式System身份、真实指标实采触发／恢复或真实通知渠道验收。

`changed-gate.py --dry-run`已命中Monitor Go与原PostgreSQL入口，新 `TestIntegrationPostgres*` 沿既有标准入口实际自动发现，无新Make／workflow。干跑包含其他会话WIP，只计选择检查，不计执行通过；指标来源、Trace和前端产品代码未变，不重复其既有门禁。新排序个人Safari、正式身份／Hosted及容量／生产T5未验，CI／Hosted继续等专门会话回传；没有轮询、派发、重跑CI／Hosted、重启个人服务、修改个人开发库或设置定时任务。

原始最终日志 `/tmp/addp-alert-priority-{pg-final,go,swagger,swagger-coverage,selection}.log`；十项输入与日志摘要收据 `/tmp/addp-alert-priority-receipt.json` SHA-256 `3b22232e15112b2c14eb30ec21d79d18f7d142ab73d868fcfea935e79e2a3eca`。证据仅为本地共享工作区T0／T1／真实SQL T2，非固定提交CI全绿。清单实际第49行／B48与第51行／B50仍部分实现；本次仅补唯一事件列表的优先顺序，不计大屏滚动、拓扑／影响范围或根因分析完成。


## 告警优先排序个人 Safari：跨页普通组只读复验（2026-10-25）

用户要求后通过原生Safari操作真实Console iframe及既有平台管理员登录，不使用内置浏览器。运行前后直接读取Monitor `/health/live`，均为`69b2f3292fb62aa8b67e8e58ab5e8dc39dc9289b`、实例`7dce643e-b0e3-4f05-909e-12602561d196`，已包含`c723b01a9`排序实现；没有重启个人服务。清除旧名称／级别筛选后，全部状态列表显示466条、24页；默认未恢复集合仅一条“源容量接近额度”警告／未处理（2026-10-23 08:02:39），严重＋未恢复筛选为空。因此本轮不计严重未恢复置顶、确认／抑制后仍置顶的Safari真实场景通过，不创建假事件或操作真实确认／抑制来补样本。

实际点击分页控件读取第1／2／3／24／23页，共抽查86行：各页20／20／20／6／20行，页内发现时间倒序，1→2、2→3与23→24边界均严格递减。首页末行2026-10-23 13:39:08、第二页首行13:34:19；第二页末行2026-10-22 08:23:03、第三页首行2026-10-21 19:28:56；第23页末行2026-10-02 01:42:29、末页首行01:25:49。第二页第7行为上述未处理警告，位于较新和较旧的已恢复严重事件之间，符合普通组按时间混排，不将全部严重事件整体前移。第23页点击刷新、浏览器整页重载后仍保持`status=all&page=23`与相同行序，点击首页后回到省略page的地址且20行与初次相同。不是对24页的全量遍历；末页有同一秒的事件，屏幕未显示事件ID，不据此验证同时间ID倒序。

可访问性文本实际读取等级、状态、时间及分页；已查看的截图证明真实表格、末页6行与24页分页可见，但原生横向滚动报`noWindowsAvailable`，最终截图保存另报ScreenCaptureKit `-3811`，没有生成最终截图文件，也不计右侧列视觉验收通过。只读记录`/tmp/addp-alert-priority-safari-report.json` SHA-256 `1c718df0e17dd06b7cf75d8f7e7a8f3e4b6189bbb8ef3e157d4edf9af0580ddd`，包含五页边界、刷新／重载比较及运行身份；原始各次AX状态`/tmp/addp-alert-priority-safari-states.txt` SHA-256 `2d1704161fc8e709ce614d2bfbe454fb36e8dea2e29e9c147c27c9481fadf16b`。保存后用独立Python校验报告的86行计数、各页总数／倒序标记、三组边界及恢复比较均成功，`git diff --check`通过；本次仅追加文档，没有修改产品代码，无需重跑未变的Go／PostgreSQL门禁。

本轮仅增加个人Safari既有数据集的普通组分页、筛选与恢复证据，严重未恢复置顶／确认抑制及同时间ID顺序仍分别引用此前真实SQL的合成数据回归，不能混作本轮Safari通过。清单第49行／B48“实时告警联动”与第51行／B50“告警信息管理”仍部分实现，正式隔离身份T4／Hosted、真实严重事件、容量及生产T5未新增验收。没有创建、修改、确认、抑制、删除告警／规则或发送通知，没有修改个人库／IAM、操作CI／Hosted或设置定时任务。后续优先补真实严重未恢复与普通组交界的页面证据，继续等专门会话回传，不为补证据主动触发异常或修改个人告警。


## 告警筛选“全部”选项的可读标签修正（2026-10-25）

对应实际Excel第51行／B50“告警信息管理”。前轮Safari已观察到类别、状态、级别在已选“全部”时都显示“请选择”；读取锁定的Element Plus 2.8.8实现确认默认空值包含空字符串，页面恰用空字符串表示不限。先在正式规范7.1明确“全部”为有效选项，再沿共享主机／离线原因选择控件已有方式，仅为本页三个ElSelect显式指定空值为null／undefined。复用现有中英文词条，不增加组件、依赖、查询枚举、参数、权限或后台状态；默认未恢复、`status=all`地址恢复、原服务端排序及事件生命周期均保持。原实现被直接替换，没有占位文字兜底或第二套状态。

新增中英文真实组件浏览器用例在旧产品源码下均失败：期望“全部类别／All categories”实际为“请选择／Select”，日志`/tmp/addp-alert-filter-labels-red.log`及标准runner失败产物保留，退出自有运行目录已清理。修正后同一入口`npm run test:e2e -- e2e/platform-alerts.spec.js --grep 'all alert filters show'`两项通过；逐一切换类别、状态、级别的具体项与全部，核对所选标签、原查询参数增加／省略、刷新及重载恢复，并断言只有GET请求。该HTTP夹具只证明前端映射，不作为后端筛选或正式身份验收。

显式使用已核验Node24.19.0后，`make test-monitor-frontend`退出0：119项单测、438项受控浏览器T3（16.3分钟，无失败／跳过）及Vite构建通过；构建保留既有大chunk警告，不修改阈值来隐藏。`make test-frontend-ci-registration`22项回归通过，21个前端登记完整，现有Platform CI的Monitor矩阵与30分钟期限自动覆盖新增用例，无需改Make或workflow。`changed-gate.py --dry-run`退出0只计自动发现，包含其他会话共享WIP，不计所有选中门禁已运行。验证期间记录的产品／规范／测试及前端配置五项输入在结束时摘要均保持一致；`git diff --check`通过。后端未改，本轮不重跑Go／PostgreSQL门禁或Swagger生成。

个人Safari实际Console iframe已热加载本批工作区源码，中文三个框分别显示“全部类别／全部状态／全部级别”，整页重载后同一`status=all`地址与登录、标签均保持；未操作英文个人偏好，英文显示及两种语言下的筛选项切换由上述独立受控T3覆盖。真实截图为`/tmp/addp-alert-filter-labels-safari.jpg`（工具返回JPEG，3024×1898），前后AX状态同前缀`-safari-state.txt`／`-safari-reloaded.txt`，均已独立读取核对。本轮没有修改真实告警或规则、发送通知、重启个人服务、操作个人数据库／IAM、查询或调度CI／Hosted，也未设置定时任务；不扩大前节严重未恢复置顶、确认抑制、同时间ID与正式隔离身份的证据范围。

最终日志同前缀`-{green,frontend,registration,selection}.log`；输入、日志和Safari产物摘要收据`/tmp/addp-alert-filter-labels-receipt.json` SHA-256 `e78a28c32d56c98269fef8845f6d1c79cd7ccaec9f67d4da537d5df694d3507f`。清单第51行仍部分实现，本次只补筛选可读性；拓扑／影响范围、根因、严重未恢复真实页面、容量与正式身份／Hosted缺口不变。后续服务失联自动告警的判定粒度及预期运行事实已向用户提问，未实现或默认采用任何新口径。


## 2026-10-25 服务组失联规则与持久确认

用户确认完整首期范围后，实现唯一 `/platform/service-availability-alert-rules` 及 `/groups`，创建停用、15—300秒且15秒倍数，界面初始60／30秒、warning。新增四项Platform权限仅发布给既有平台系统管理员，当前User仍须System模块读取权，候选不使用后台身份代授权。System私有组投影沿既有协议；Monitor单独持久保存当前预期的运行确认，历史登记不代替有效在线副本。组意图变更或重新启用重新确认，其他组变更不影响本组；重启保留资格，连续窗口重积。来源前后变化、观测超过五秒未更新、重复／倒退／超过16秒间隔均不据此打开或恢复；计划停用不结束活动事件。

告警沿原open／acknowledged／resolved、opened／resolved、CAS、抑制及事务outbox。新增 `service_availability` 显式订阅，旧目标保持原类别。打开时名称与恢复条件冻结，历史名称检索不读当前规则。Webhook、邮件及企业微信识别独立服务组通知协议；不新增退出结束状态，不改Tenant、其他角色或IAM内核。

本批标准验证：

- `make test-module MODULE=common GO_ONLY=1`：通过。新增当前User完整有界模块投影测试、拒绝坏页及机器身份回退；权限目录总数及仅系统管理员授权不变量同步。`make test-authorization` 通过目录、生成常量、SQL seed、覆盖及Swagger检查。
- `make test-module MODULE=monitor GO_ONLY=1`：通过。六项新增状态／通道测试，含首次确认、60／30秒完整窗口、重启、同秒预期变化、未知／暂停、重复观测与纳秒边界、损坏确认不误授资格及企业微信识别；新API权限矩阵验证当前System读取权、动作权限、非委托User、严格输入。
- `make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package migration --test platform-alert-management"`：通过。242前后真实迁移比较，仅新增四权限及系统管理员绑定，其他角色、授权事实与OAuth客户端不变；既有触发器推进授权版本、旧refresh family撤销，重复Runner幂等。标准selector补入新测试，默认CI仍按原真实PG测试命名自动发现。
- `make test-monitor-postgres`：通过。新增真实数据库、受控System组投影与受控时钟闭环：旧登记99但无在线仍等待，至少一个副本确认，全副本失联60秒仅一个事件，双评估并发不重复事件／outbox；确认／抑制后在线30秒恢复同事件，规则改名及恢复改为15秒不改历史冻结30秒；前后源变化、System失败、重启确认及新预期重新确认，活动事件禁止删、来源故障可停用、删除保留历史。新类别只投给显式目标，旧日志目标不扩订阅，恢复投递被原抑制机制抑制。夹具按唯一主体／目标清理并逐项计数零残留。
- `make test-module MODULE=system GO_ONLY=1`：通过；迁移目录版本同步为242，前向迁移不修改已应用SQL摘要。
- `make test-common-frontend`：277项通过；`npm --prefix system/frontend test`：30文件／172项通过。Monitor新增组候选、分页、时长、证据与URL单测沿原通配发现；新六项页面专项覆盖候选可读名称／默认停用／CAS保留草稿、英文等待说明、撤权丢弃迟到候选、缺System读权不发管理请求、显式通知类别与无当前规则读取权的冻结依据。
- `make test-monitor-frontend`：完整聚合退出0，122项单测、444项受控浏览器T3及生产构建通过；专项六项包含在该444项中，不另计总数。
- `bash scripts/swagger/gen-swagger.sh monitor` 与 `bash scripts/swagger/check-route-coverage.sh monitor`：通过，96个公开路由方法。`python3 scripts/ci/check-t2-ci-registration.py --repository "$PWD"`：22／12／9／2登记通过。沿原Monitor PostgreSQL、System IAM PostgreSQL、Go及前端自动发现，无第二条测试或部署路线。

日志：`/tmp/addp-service-availability-{common-final,monitor-final,system-go,system-pg,monitor-pg,authorization-final,common-frontend,system-frontend,browser,monitor-frontend-final,swagger,route-coverage,registration}.log`。真实数据库T2使用受控组投影与时钟，不等于真实部署停机／租约超时或真实渠道送达；页面为受控HTTP T3，不等于Safari个人会话、Console正式身份或Hosted。没有执行个人重启／数据库迁移、创建个人规则／通知、查询／触发CI或Hosted，也未设置定时任务。个人真实链路和正式身份验收仍待应用代码后复验及专门会话安排；清单第15／49／52行保持部分实现。


## 重启后的个人环境只读核对（2026-10-25）

用户完成全量重启后，System 8180、Monitor 8100、Gateway 8000的`/health/ready`均200/ready；构建报告commit为`871bc854795105b364b154f1802697969655b6f0`，祖先检查确认包含本批`062301037`。沿Safari当前平台系统管理员会话进入Console的服务失联规则页，规则列表为空且无读取错误；创建入口及System授权候选可读取，候选25组与标准入口受管声明的25组一致。当前声明为Meta scan、Quality quality-bounded、Transfer sync／continuous_sync各一个预期副本；不据此认定双Worker运行验收。无障碍接口完成Meta扫描组筛选／选择，默认60／30秒、启用开关关闭且不可在创建时开启，缺名称时本地校验阻止写入。

截图仍显示原列表，与无障碍树中的弹窗及校验提示不一致；Raise窗口后仍未得到一致视觉证据，已请求用户核对实际是否可见，未把无障碍读取外推为弹窗视觉通过。未保存／启用个人规则或通知，未停止服务、直接读写个人数据库、触发CI／Hosted或创建定时任务。真实规则保存、重新启用后的运行确认、租约失联／恢复和正式身份全闭环仍未验收。安全收据：`/tmp/addp-service-availability-personal-restart-20261025.json`。


## Safari服务失联规则保存复验（2026-10-26）

用户明确授权复验保存与启用后，沿Safari现有平台系统管理员会话进入真实Console `/monitor/platform-alerts?tab=availability-rules`。初次截图仍为旧列表，但后续截图已实际显示创建弹窗、名称／服务组、60／30秒及创建时关闭且不可切换的启用开关；仅证明本次弹窗已取得可见截图，不认定此前显示差异的根因已查明或修复。通过表单筛选并选择`meta · 任务进程 · 元数据扫描`，保存`验收临时-服务失联-Meta扫描-20261026`；页面刷新及浏览器整页重新加载均重新读到唯一同名规则、同组、触发60秒／恢复30秒、启用“否”、最近评估“已暂停”，默认停用保存链路通过。

编辑单元格在Safari无障碍树中只暴露合并的“编辑／删除”文本，点击未进入编辑；按实际截图点击编辑按钮时，自动化返回`noWindowsAvailable`。已请求用户将Safari切换至当前桌面前台；备用键盘路径未取得编辑弹窗，不能计为启用通过。未保存额外空白草稿；最终整页重读保留上述一条停用规则。未删除测试规则、停止服务、制造失联或发送通知；无直接个人数据库操作、CI／Hosted执行或定时任务。下一步仍为编辑启用与刷新重读，真实失联／恢复及正式身份闭环另验。

用户随后明确激活Safari前台并授权继续编辑／启用。同一规则的编辑按钮实际点击成功，截图可见编辑弹窗、原服务组只读及60／30秒条件；名称修改为`验收临时-服务失联-Meta扫描-20261026-编辑复验`，启用开关经键盘切换后截图为开启。Safari无障碍接口仍把该开关读为off，因此不以该字段单独断言保存结果；点击保存后，列表已显示新名称和启用“是”，页面刷新及浏览器整页重新加载均保留；本次连续观测由“正在积累连续观测”（10:18:43／10:18:58）推进为“服务组在线”（10:19:43，Asia/Shanghai）。本轮真实页面的编辑、启用及持久重读通过，最终保留此一条启用的临时规则。未停服务或制造失联，不将在线评估外推为失联／恢复／通知或隔离正式身份Hosted通过；此前显示差异根因仍未认定已修复。本轮仅更新现有证据／设计记录，`git diff --check`为文档验证；CI／Hosted仍等待专门会话回传，不设置定时任务。

## 服务失联Hosted专项准备与本地runner验证（2026-10-26）

按用户确认，将`service-availability`接入既有`platform-node-metrics`闭合case、Meta双Worker配置、正式身份／真实Console及外层清理入口；同步T4限定管理员用途与手动workflow选择及登记拒绝测试。场景验证单Worker真实租约失效仍不报警、全组失效满足60秒只打开一个事件、恢复一个满足30秒恢复同事件、共享回环HTTPS／HMAC重试与投递、计划停止不产生新事件、配置删除保留历史；浏览器用真实密码MFA和HTTP读取核对启用规则、刷新及恢复事件冻结依据，不拦截API。

故障夹具仅允许当次Hosted case；由标准PID文件、实际可执行文件／工作目录及`/proc`启动标记核验两个Meta Worker，使用Linux pidfd固定原进程。暂停前原子保存私有0600恢复记录；正常异常均恢复所有已固定暂停进程，外层生命周期也在标准停止前补做独立恢复，归属变化保留记录并使清理失败。SIGSTOP／SIGCONT不改预期声明、真实时钟、租约或数据库，保持1800秒场景／60分钟Job期限；本机只跑受控fixture测试，未对个人Worker发信号。共享通知接收器只抽出事件校验钩子，资源告警与失联告警复用同一TLS／HMAC与重试入口，没有复制传输实现。顺带补齐已支持的服务失联类别在告警查询Swagger枚举中的遗漏，不改HTTP行为或生产权限。

最终`make test-node-metrics-online-runner`退出0：148项Python回归及System身份Go夹具通过，含新事件窗口／主体／策略拒绝、共享签名传输、浏览器身份及回执、真实`/proc`格式解析的受控夹具、PID启动身份变化拒绝、失败恢复／外层恢复顺序。当前根目录`validate_platform_node_metrics_profile`检查通过；`bash scripts/swagger/gen-swagger.sh monitor`及`bash scripts/swagger/check-route-coverage.sh monitor`通过，96个公开路由方法；`git diff --check`通过。标准入口已沿原Online runner／Platform路径发现，无新增Make路线。原始本地门禁曾出现旧失败分支20秒超时，后续本轮修正及最终完整148项均已通过；不把先前失败改写为通过。日志`/tmp/addp-service-availability-runner-first.log`、`/tmp/addp-availability-{new-tests,runner-final,runner-verified,runner-release,swagger}.log`。

以上仅为验收实现及本地T0／协议断言证据，不是实际Linux pidfd／心跳租约故障注入、真实Hosted页面或T4通过。本会话未执行／调度／轮询CI或Hosted，未设置定时任务；真实故障闭环和退出零残留等专门会话安排并回传，清单第15／49／52行保持部分实现。个人Safari中先前启用的临时规则本轮未改动。

## 文件系统容量告警首期（2026-10-27）

先更新术语表、平台运行观测规范与API契约，再沿既有主机资源规则实现。完整主机／device／mountpoint／fstype独立绑定和信号；创建停用，默认严格超过85%连续300秒触发warning、严格低于80%连续120秒恢复，15秒评估。使用现有可用容量使用率，核验同次采样的总量／已用／普通用户可用及uptime；容量改变和来源／规则生命周期变化重新计时。缺样、重复、过期、预算失败和挂载缺失不恢复；永久卸载可停用规则，未恢复事件保留，有活动事件仍禁止删除，不新增结束状态或自动转绑。触发时挂载／容量证据保存在事件与通知中，页面就近展示并精确进入同挂载趋势。沿原权限、唯一API、协调器及outbox，不改IAM或租户范围。

存储沿唯一EnsureMonitorStore前向替换旧主机／指标唯一索引为含挂载键的唯一索引；既有标量规则空维度，历史信号不改写。PostgreSQL专项实际重建旧两列索引，证明收敛后原标量配置保留且同主机不同文件系统类型可分别建规则；还验证两挂载隔离、容量变化重建、缺样保留、真实outbox及企业微信载荷、缺失挂载停用与重新启用拒绝、身份不可改和活动事件阻止删除。

本地有效结果：

- `make test-module MODULE=monitor GO_ONLY=1` 退出0，最终日志 `/tmp/addp-filesystem-alert-go-delivery.log`；覆盖本批阈值等号、连续窗口、容量快照、采样／维度不一致、重复点与信号隔离等Go T1。
- 在标准Infra状态确认实际端口后，用显式测试连接环境运行 `make test-monitor-postgres` 退出0；仅共享addp-postgres的addp_test，密码在子进程内取得且未输出。完整repository／service PostgreSQL T2及新增两项均通过，最终service32.657秒，日志 `/tmp/addp-filesystem-alert-postgres-migration-final.log`。早期新用例发现RecordTx缺传资源证据，已沿原路径修正；原失败不计通过。
- Node24.11.1下 `make test-monitor-frontend` 退出0：128项单元、473项Playwright、Vite生产构建7.37秒通过，原chunk大小提示保留。最终日志 `/tmp/addp-filesystem-alert-frontend-full.log`，127个源码／测试输入SHA256在前后核对无变化；摘要 `/tmp/addp-filesystem-alert-frontend-inputs.json`。新增六项中英浏览器场景单独退出0，日志 `/tmp/addp-filesystem-alert-browser-targeted-v6.log`；覆盖精确挂载、停用后启用CAS、已消失挂载编辑停用、预算／能力错误、迟到响应和撤权、冻结容量证据与趋势。中英窄屏截图已目视核对，目录为系统临时目录中的addp-filesystem-rule-2puy59及addp-filesystem-rule-5RJqyY。受控T3不替代真实Safari或真实主机采集。
- Swagger按单模块标准入口生成；`bash scripts/swagger/check-route-coverage.sh monitor` 退出0，96个公开路由方法覆盖一致。日志 `/tmp/addp-filesystem-alert-swagger.log`、`/tmp/addp-filesystem-alert-route-coverage.log`。
- 标准metrics入口的配置／登记T0最终102项通过，shell语法与diff检查通过。新增filesystem-alerts限定分项复用同一准备、mTLS、来源和清理内核；执行选择及失败时恢复发现文件有确定性测试，all保留新增专项，host-resources保留原范围。Release现有隔离矩阵同步登记该分项，不新增工作流或定时任务。

真实Prometheus／SQL／签名HTTP专项尚未通过，必须与上述T1／SQL T2／页面T3分开：首次 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=host-resources` 在旧六来源资源总览用例失败，未执行新增告警；日志 `/tmp/addp-filesystem-alert-metrics-host.log`。没有修复或重跑该其他owner用例。限定分项第一次运行在共同采样超限预检超时，日志 `/tmp/addp-filesystem-alert-metrics-exact.log`；保留失败并加入固定形状、安全的源文件诊断，未放宽采样预算。

最终 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=filesystem-alerts` 中102项配置／登记T0、固定版本promtool窗口、真实mTLS、中心自采集、采样超限与恢复、来源离线与恢复均实际通过；随后受控主机发现等待失败。原始日志 `/tmp/addp-filesystem-alert-metrics-exact-v2.log` 显示Nginx连续两轮打开/source/discovery.json为ENOENT，中心仍保留原发现身份，未进入TestIntegrationMetricsFilesystemAlertLifecycle；边界定位于当次测试夹具文件读取，Docker挂载／文件发布的具体根因尚未证实，不将其写成产品告警失败或已修复。没有通过延长等待、忽略缺样、改容量指标或跳过预检来通过；新的真实触发／缺样保留／恢复及签名通知代码仍待专门会话的Linux该分项结果。

三次拥有明确归属的当次项目均已正常down，随后按精确Compose项目label独立核验容器／网络／卷全部为0，回执 `/tmp/addp-filesystem-alert-cleanup.json`。未填充磁盘，未修改或重启个人服务／数据库／配置，未执行Safari、Hosted、CI调度／轮询或定时任务。CI与Hosted继续等专门会话回传，本批不能写为完整运行验收完成。


## 主机挂载行创建容量告警入口（2026-10-27）

先更新正式观测规范，再在主机列表展开及详情的完整挂载行增加“创建容量告警”。只向当前非委托Platform User、具备既有资源观测／主机读取及资源规则读取／创建权限的用户显示，有效容量采样才可点击。挂载入口只打开表单，固定并重新核验主机及完整device／mountpoint／fstype，读取既有服务器预设；缺样、挂载消失、主机版本变化或拒绝均不换绑、不写入。仍须填写名称并显式保存，创建停用，取消不写入，冲突保留输入。没有新增API、权限、后台投影或容量判定口径。

规则列表原内联表单及保存逻辑已移除，由Monitor领域组件ResourceAlertRuleEditor唯一拥有表单、校验及POST／PUT。主机列表与详情使用同一实例入口，HostFilesystemResources只展示并上抛完整挂载；保留现有HostNodeSelect、最新请求作用域及通用对话框样式。名称／条件等未保存内容不写URL或浏览器存储，关闭、身份／页面变化均清除，迟到数据不回填。新增唯一所有权单元测试防止列表或主机页面重新复制表单及保存路径。

本地Node24.11.1分层结果：

- 原规则四项专项重跑通过（/tmp/addp-mount-alert-editor-refactor.log），证明抽取后正常创建、编辑、停用消失挂载等既有行为保留。新增入口七项最终专项通过（/tmp/addp-mount-capacity-shortcut-targeted-v3.log）：中英文窄屏、同挂载点不同fstype、取消无写入、精确带入、服务器预设91／81及45／30而非前端硬编码、有效零值、缺样／预算／版本拒绝、两项规则权限分别约束、迟到数据及撤权、重核主机403、冲突保留。早期测试将输入值当作文本、覆盖GET路由遗漏query，以及引用错误的冲突文案导致失败；原日志保留，不计通过。
- `make test-monitor-frontend` 退出0，129项单元、480项Playwright（17.6分钟）及Vite生产构建3.58秒通过，日志/tmp/addp-mount-capacity-frontend-full.log。该全量版本的128个源码／测试输入摘要在/tmp/addp-mount-capacity-frontend-inputs.json。
- 全量后补查确认一个新增入口的清理遗漏：弹窗打开后，主机自动刷新明确403时，父页面已清除主机，但表单仍显示。新增真实组件回归先失败（/tmp/addp-mount-capacity-owner-denial-red.log），在既有refreshBlocked裁决上增加唯一同步关闭监听后通过（/tmp/addp-mount-capacity-owner-denial-green.log）；不改变既有401／403／404判定或重试规则。它覆盖自动刷新后关闭草稿及无写入，不能用身份事件通知代替Owner实际拒绝。
- 最终只修改上述一行监听并增加该用例；相对已通过全量的前端输入仅NodeResources.vue与filesystem-alert-rules.spec.js两处变化，其他输入结果复用。最终`npm --prefix monitor/frontend test`129项通过；通过既有scripts/test/frontend-browser-gate.mjs标准包装的`npm --prefix monitor/frontend run test:e2e -- e2e/filesystem-alert-rules.spec.js e2e/host-filesystem-expansion.spec.js e2e/host-device-expansion.spec.js e2e/platform-alerts.spec.js`67项受影响浏览器回归全部通过（2.7分钟），日志/tmp/addp-mount-capacity-affected-final.log；最终`npm --prefix monitor/frontend run build`退出0，4.83秒完成，日志/tmp/addp-mount-capacity-build-final.log。最终128输入摘要/tmp/addp-mount-capacity-final-inputs.json前后核对一致，单元日志/tmp/addp-mount-capacity-unit-final.log；不能将未重跑的最终481项全量记为实际通过。

已目视核对中英文窄屏弹窗及保存后的挂载行截图；最终专项目录/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-mount-capacity-shortcut-m3c5jA（英文）及addp-mount-capacity-shortcut-NsNOKp（中文），临时路径由专项日志记录。沿既有Monitor前端测试、Playwright自动发现和Platform Monitor frontend门禁，不增加测试入口、依赖或工作流；未改变HTTP契约，无需重新生成Swagger或运行数据库／指标门禁。本批没有后端修改，未操作个人服务或数据库，未执行Safari或正式身份运行验收，也未调度／轮询CI、Hosted或创建定时任务。原文件系统Prometheus专项未通过状态不因本批受控页面结果提升，CI和Hosted仍等专门会话回传。


## 挂载告警入口的共享所有权门禁登记修复（2026-10-27）

专门CI会话回传[Platform consistency失败](https://github.com/pampa0629/addp/actions/runs/40465364758/job/122059331587)后读取原始日志/tmp/addp-ci-20261027-073016/job-122059331587.log，确认唯一首失败为hostNodeSelectOwnership.test.mjs仍要求ResourceAlertRules直接导入HostNodeSelect。2d373a47e已将其迁入唯一ResourceAlertRuleEditor，产品主路径正确但本会话漏同步共享测试消费者登记；上轮Monitor129单元／67浏览器不能覆盖或替代这一平台门禁。原始首个失败Job122058289956仍保留，不把其他Redis／HTTP失败算作本批修复阻碍。

按现有开发原则和共享组件契约修正原测试：改为登记实际Editor消费者，补上已有EngineDeployments；扫描所有模块frontend/src及共享basic/src的实际HostNodeSelect使用，与四个闭合集合一致才通过。每个消费标签均须显式提供api／allowed／identity，并只导入同一共享实现；已有@common-ui公开导入须由index.js唯一重新导出同一文件。保留共享组件不依赖业务store／模块、Monitor旧本地HostNodeSelect与hostAdmission删除及Monitor不拥有主机创建／导入的原断言；新增规则列表与挂载页只能组合唯一Editor、不得重新拥有选择器／表单／POST／PUT的断言。没有恢复重复实现，也没有修改产品组件、接口、权限或授权失效清空行为；业务行为仍由Monitor现有浏览器用例覆盖，未在共享测试中复制业务运行逻辑。

Node24.11.1下标准`make test-common-frontend`先重现283项中282通过／1失败（/tmp/addp-host-selector-owner-red.log），修正后最终284全部通过（/tmp/addp-host-selector-owner-final.log）；当前`npm --prefix monitor/frontend test`129项通过（/tmp/addp-host-selector-monitor-unit.log），diff检查通过。已核对2d373a47e至本次验证的monitor/frontend和common-frontend/basic/src无差异，因此沿用上节实际浏览器及构建证据，不重复运行有效浏览器／Hosted。当前只修测试登记与本证据；Platform workflow原有make test-platform调用make test-common-frontend，包内通配自动发现覆盖修改，无新增入口或CI选择登记需求。未运行整套Platform、未把本地子门禁写成远程CI已恢复；自然CI和Hosted继续等专门会话回传，未轮询、重跑、取消工作流、创建定时任务或操作个人服务。此为既有功能的CI验证覆盖修复，不提升功能清单整行运行验收状态。


## 主机挂载行查看已有容量规则入口（2026-10-27）

主机列表展开及详情的完整挂载行新增“查看已有容量规则”，复用现有规则read权限、唯一模块导航、规则列表及ResourceAlertRuleEditor；无需create／update或当前容量样本。URL保存node_id、固定容量metric_key和完整device／mountpoint／fstype，原规则GET在Count／分页之前精确取交集，包含启用和停用规则；无匹配与读取失败分开。刷新、规则分页保留筛选，返回恢复原主机上下文；缺失／重复／错配／非法条件明确拒绝，不退回全量列表。切换筛选取消旧请求，身份／权限撤销清空旧结果，原创建／修改／启停权限及采样核验不变，不新增API路由、角色、后台投影或迁移。

实施前确认现有Monitor Go、PostgreSQL与前端T1／T3、Swagger及Platform共享所有权门禁覆盖本批路径；根Makefile、module-gate、frontend-browser-gate及既有CI自动发现无需新登记。先同步平台观测、可恢复路由和API规范，再改原主路径。新增真实PG回归在25条较新不匹配及同设备异fstype规则之前筛选，核对总数、第二页、字面“/%”不作通配及原主机筛选；API拒绝部分、重复、非法编码和错配指标。前端新增七项浏览器场景覆盖中英只读定位、分页外规则、刷新／返回、缺样下查看及停用、空结果／503、非法URL、权限撤销与晚响应；单元补额外维度不能覆盖主机身份。

本地标准`make test-module MODULE=monitor GO_ONLY=1`通过（/tmp/addp-mount-rule-query-go-final2.log）；依据标准infra/status确认addp-postgres实际25432后，仅使用addp_test执行`make test-monitor-postgres`，repository及service全套通过，新精确筛选用例通过（/tmp/addp-mount-rule-query-postgres-final.log）。`npm --prefix monitor/frontend test`最终130项、`make test-common-frontend`284项、Monitor标准Swagger生成及96个公开方法覆盖通过；最终`npm --prefix monitor/frontend run build`通过（3.77秒），日志分别为/tmp/addp-mount-rule-view-unit-delivery.log、/tmp/addp-mount-rule-view-common.log、/tmp/addp-mount-rule-query-swagger-retry.log、/tmp/addp-mount-rule-query-coverage-delivery.log、/tmp/addp-mount-rule-view-build-delivery.log。

历史失败单列保留：首轮新增七项6通过／1失败，发现query键顺序导致重复规范化与两次查询；统一tab在前的唯一规范顺序后，原精确一次调用断言不放宽，单项通过。随后完整前端门禁浏览器165通过／13失败／1中断／309未运行，独立诊断3通过／5失败／12未运行，均未通过。失败包括既有宿主页面、图表和初始化，新晚响应场景也在fixture初始化前置阶段失败；当时18核系统负载约177、内存压力级别2，轨迹另有ERR_NETWORK_CHANGED，也有全HTTP200但页面未就绪的失败，不能将所有失败归因于单一环境因素。原日志/tmp/addp-mount-rule-view-frontend-full.log、/tmp/addp-mount-rule-view-diagnostic.log，原结果目录addp-monitor-playwright-results-YS2HVY／JT4sCX；只中止当次标准wrapper，两个run.json中的runtimeDir均已核对删除，未操作其他会话进程、增加超时、降低断言或重启个人服务。

本轮未操作个人Safari、CI／Hosted或定时任务。完整浏览器门禁的后续结果单列如下；受控HTTP页面验证不能替代个人真实来源、Safari视觉或正式身份验收。后端增加查询参数，个人运行复验须由用户执行`./scripts/dev/restart.sh -monitor`，不得把旧进程返回400当成新接口已验收。


最终复验：实际运行条件恢复为系统负载约6／内存压力级别1，且最后一项维度拒绝校验及单元变更已落地后，重新执行标准`make test-monitor-frontend`，退出0：130项单元、488项浏览器（19.1分钟，含本次七项）、Vite构建（3.92秒）全部通过，无失败／跳过。日志/tmp/addp-mount-rule-view-frontend-delivery.log，日志记载临时结果目录 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-monitor-playwright-results-7korI9`，已按标准成功退出路径移除；最终128个Monitor前端输入摘要与/tmp/addp-mount-rule-view-delivery-inputs.json完全一致。标准wrapper清理返回成功；中英620像素截图addp-mount-rule-view-a1295S／Z4NyGG均已查看，只读入口无创建／编辑按钮、上下文与唯一匹配规则可见。此前失败不改判，只以这轮独立终态支持最终本地T3通过；未修改断言、超时或产品采样口径。当前本批本地验证已通过，CI／Hosted与个人真实运行仍未执行或取得新结果。


## 挂载规则入口的个人Safari前置核对（2026-10-27）

23:32起（Asia/Shanghai）沿现有Safari平台系统管理员会话只读核对。Console首次显示“无法确认当前页面的访问权限”，点击原重试后恢复主机列表；System 8180、Monitor 8100及Gateway 8000的/health/ready实际均200，不将非就绪地址/health的404判为服务离线。进入Docker Desktop Linux VM资源详情，CPU／内存及vda磁盘IO有有效值，文件系统明确显示“当前采集源未提供文件系统指标”，网卡来源未提供，TCP明确缺失且不显示零。当前没有真实挂载行，故不能执行挂载行创建／查看容量规则的个人实源验收，不能由已有受控HTTP与Linux前端通过代替。

详情返回保留原list_view=load-connections，三档负载、缺失TCP、采集／纳管状态及一个主机可读。页面持续报告“隐藏，自动刷新已暂停”；native Raise未恢复该状态，横向滚动接口报noWindowsAvailable，截图中右侧部分仍不可见。已请求用户将Safari置于当前桌面前台，右侧列视觉及前台自动刷新仍未验，不将无障碍树可读等同全部可见。未写规则／通知、修改采集或个人数据库、重启服务、触发CI／Hosted或设置定时任务。此次仅记录运行前置与阻碍，未核验运行二进制对应提交，不将当前工作区HEAD写成运行源码身份；来源恢复、挂载规则跳转及正式身份闭环仍待相应环境验证。


## 挂载缺样断言修复的完整CI闭环（2026-10-28）

专门CI会话回传后，仅核对已有归档，不触发或轮询新Job。修复9ad52efda9d0b7d9fea1e644060363461c07a784的有效后继为7ebf98370ebc965e09cb5744983dbf8ee7fbbbce，[Monitor Job 122257072557](https://github.com/pampa0629/addp/actions/runs/40523616999/job/122257072557)的实际Run Monitor frontend gate及Job均success；原始日志确认130单元／0失败／0跳过、488浏览器（25.1分钟）和Vite构建（10.57秒）通过。filesystem-alert-rules.spec.js的mount action requires both rule permissions and valid current capacity实际执行，包含9ad新增的真实no_data响应及刷新完成后disabled断言；node-resources.spec.js的disabled refresh clears old metrics用例也实际执行，两者是不同用例，不混淆修复归属。Report skipped步骤本身跳过不表示正式门禁跳过。

已独立核对9ad至7eb的monitor/frontend无差异，后继源码包含修后的测试；不能以修前版本偶然通过代替本次证据。归档日志/tmp/addp-ci-20261027-160029/job-122257072557.log的SHA256为4b356a2735e7196114f6f1de7a7f1a213e0413785c8d12b6db0447c38631ee6e，与Monitor_9ad_actual_full_frontend_recovery_audit.json一致，callback-live-job-122257072557.json的head_sha及正式执行步骤终态一致。旧35／a1f失败及旧预算超时保留，不倒改历史，也不宣称父workflow或当前HEAD全通过。此处仅关闭本修复的受控HTTP前端CI缺口；个人Safari隐藏页、右侧列／前台刷新与真实Linux文件系统挂载行仍未验，既有告警实源缺口不由本CI代替。此次仅文档归档及限定diff／链接核对，未运行新产品测试、Hosted、容量或定时任务，未操作个人服务。


## 文件系统容量告警受控源实链T2复验（2026-10-28）

沿原标准入口`make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=filesystem-alerts`，最终限定运行退出0：110项配置／登记测试、固定版本promtool（文件系统容量18项／inode22项）、mTLS及独立中心采集、样本超限／恢复、来源停启／恢复、当次受控主机发现均通过；TestIntegrationMetricsFilesystemAlertLifecycle实际执行175.62秒。测试文件提供高／低容量数据，经真实Prometheus、Monitor规则评估、当次独占addp_test及真实签名HTTP：高值打开一个事件，打开通知首次503后沿原重试送达；缺样保持未知且超过恢复期不恢复，低值恢复同一事件并送达恢复通知；最终2个生命周期事件、2次送达、3次HTTP尝试，重复派发不重发。测试阈值及受控来源不等于生产默认策略、真实主机容量压力、真实Collector或正式身份页面验收。

此前两轮仍失败：/tmp/addp-filesystem-discovery-metrics.log在样本超限前置超时，诊断get又返回404覆盖原异常；v2日志在正常样本恢复前置超时，Nginx均有/source/metrics的ENOENT，尚未进入告警专项。没有延长等待、换发布方式、跳过前置、填充磁盘或放宽告警断言；成功轮不倒改失败，也不证明Docker文件可见性异常的唯一根因已修复。仅修正已确认的“诊断错误覆盖首个失败”：先以确定性反例重现二次OSError替代原AssertionError，再保留原异常。新增唯一封闭文件诊断仅允许当次metrics／discovery.json，宿主最多读65537字节并记录读取长度／前缀摘要，容器仅检查可见／可读布尔值、五秒超时；错误只留枚举，不输出文件正文、目标、标签或stderr。超限、恢复和发现失败分支共用该诊断，成功轮不执行它，故不声称故障容器诊断分支已获实际故障验证。

最终独立110项确定性回归通过（/tmp/addp-filesystem-diagnostic-delivery-unit.log），覆盖有界读取、权限／缺失／超限／Docker命令失败及超时、秘密不泄漏和首异常保留；原反例红记录为/tmp/addp-filesystem-first-failure-red.log。标准T2登记检查、shell语法及限定diff通过，原输入声明和Release的filesystem-alerts矩阵已自动覆盖，无新入口或workflow。最终T2日志/tmp/addp-filesystem-discovery-metrics-v3.log的SHA256为0d6a7f97048664109410d8dcc63b40c032de50fd2f60f7f175d913f3b2d28698，输入快照/tmp/addp-filesystem-discovery-v3-inputs.json记录基底41024f0800a646ee613f53f518673de2c494527e及本批两份脚本修改；348项中347项前后不变，Makefile仅发生其他会话的两个栅格runner测试登记改动，已核对原文件与基底匹配及Monitor metrics完整target／recipe字节不变。不将整个共享HEAD写作全通过。

成功项目addp-metrics-t2-032ce1c920ed484f8a84f9f9a4856244沿标准退出清理并核对临时文件删除，外部再按精确Compose项目label独立确认容器／网络／卷均0，回执/tmp/addp-filesystem-discovery-v3-receipt.json；前两轮对应清理回执分别为/tmp/addp-filesystem-discovery-cleanup.json与-v2-cleanup.json，三类对象均0。此为Docker Desktop上受控Linux指标源→真实Prometheus／SQL／签名HTTP的本地T2，不是原生Linux主机文件系统采集、个人Safari挂载行、真实MFA／Hosted或生产T5。未操作个人服务、数据库或采集配置，未触发／轮询CI、Hosted、容量或定时；后续CI仍等专门会话回传。

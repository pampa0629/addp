# Monitor 节点与主机资源历史证据

[返回 Monitor 历史证据](Monitor历史证据.md) · [返回验收证据索引](../验收证据索引.md)

本专题收录主机列表、资源趋势、设备与文件系统视图、探针预算。固定源码、环境、失败记录、修复结论与未覆盖范围以各记录为准；本次仅整理文档，没有重跑验收或刷新 CI 状态，不推导当前 HEAD 或生产 T5 已通过。跨主题语境与原有顺序见 [Monitor 历史证据入口](Monitor历史证据.md#原记录入口)。

## 主机固定窗口探针的准备与执行期限

2026-10-22，专门 CI 会话回传 [6e55a4fad 的 host-resources / Job 119600712786](https://github.com/pampa0629/addp/actions/runs/39708924861/job/119600712786) 实际失败。原始日志 `/tmp/addp-ci-20261022-030015/job-119600712786.log` 显示同一 Python 60 秒包住依赖下载、Go 编译及真实执行：命令从 02:53:52 UTC 开始，约 02:54:46 才进入首项 Go 测试；DiskIO、Disk、Network、HTTP、Process、CPU、Filesystem 七项已通过，FilesystemInodes 开始后被外层终止。不能称测试未启动或产品断言失败。当前源码未有替代修复，现将同一八项选择器改为当次私有目录 `go test -c` 准备最多 180 秒，再直接执行二进制，Go／外层真实执行上限均为原有效 60 秒；保持 count=1、原八项断言、后续故障／告警／清理及 Job 20 分钟。构建失败／超时禁止运行旧产物，执行超时不重试，不改产品公式、窗口或权限。

沿标准 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=host-resources` 增加确定性红绿验证：模拟 70 秒冷准备与 8 秒执行，以及构建失败／超时、旧产物拒绝、实际执行超时不重试。原合并计时在配置阶段出现三项失败、一项错误，日志 `/tmp/addp-host-window-budget-red.log`。随后配置回归暴露旧选择器源码约束仍要求单个裸模式字面量，保留唯一选择目录再组合二进制参数后，最终 48 项配置回归全部通过；中间失败日志 `/tmp/addp-host-window-budget-green.log`，未计通过。T2 CI 登记检查通过，现有 Make、原 probe／配置回归的输入登记与三分组 Release 已覆盖，无需新脚本或 workflow。

真实本地 T2 在冻结基线 `a190663c261a7e63ff00885d006138571c41610e` 仅叠加本方三项输入，以 `GOMAXPROCS=2 GOFLAGS=-p=2` 调用同一标准入口，**未通过**，日志 `/tmp/addp-host-window-budget-final.log`。这次编译已独立完成，但首项 DiskIO 的 Docker promtool 调用在既有 30 秒 Context 内未完成（signal killed）；随后 Disk／Network 分别在 3.26／13.82 秒通过，HTTP 开始后达到独立执行的 60 秒上限。与云端的冷准备占用预算分别记录；伴随观测为本机同时运行其他门禁、18 个逻辑核且 load average 51.19／134.64／115.96，尚未证明它是执行变慢的唯一根因，未放宽期限或盲目重复运行。该次初始清理遇到异步一次性 promtool 资源仍占用网络／卷，不能计零残留；仅对精确项目 `addp-metrics-t2-d2a7516da2b74f4ab03fb83404308598` 复用原门禁的 compose／cleanup 函数完成后续清理，独立读取容器、网络、卷均为空，日志 `/tmp/addp-host-window-owned-cleanup-final.log`。首次复用清理因缺少当次 TLS 路径参数而未执行 Compose，补齐清理私有路径后成功；不接触个人服务、配置或其他项目。

三个最终输入与隔离副本逐字节一致；指纹、日志摘要及清理核验记录为 `/tmp/addp-host-window-budget-audit.json`。本轮仅将计时修复和 48 项 T0 回归落地，不计完整本地 T2、原生 Linux 或 Hosted T4 通过；新输入原生 Linux 结果继续由专门 CI 会话回传，没有查询、重跑或设置定时。来信同时附带的 PostgreSQL 来源 `6d42e4621` 三次真实准入成功及退出清零已核对原始日志哈希，与 P0 已记录的更新 `c7ef8ea0e` 三分组通过不冲突，不重跑或重复回填 PG，既有专业 T4 边界保留。


2026-10-22 原生 Linux 复验终态：专门 CI 会话回传精确源码 `9a2b3d28691741397f171b94866c125722bbe388` 的 [host-resources / Job 119660741437](https://github.com/pampa0629/addp/actions/runs/39727468958/job/119660741437)，push、attempt 1，实际 `Verify isolated metrics lifecycle` 成功。本会话复核本地保存的完整原始日志：真实准备编译约 46.67 秒，与保留的 Go／进程 60 秒执行上限分离；DiskIO、Disk、Network、HTTP、Process、CPU、Filesystem、Inodes 八项逐项 PASS。后续 ResourceQueries 两次、PressureEvidence 两次、ResourceOverview 和 ResourceAlertLifecycle 均真实通过，十四项 Go 运行零 FAIL／SKIP；完整门禁仍覆盖 mTLS 拒绝、溢出、来源故障／恢复、HTTP SD 失败／重定向／空集、OAuth、受限节点、中心 SIGKILL／持久样本、六源排行与查询前预算。告警在同一事件上由真实低样本恢复，签名 resolved 投递落库，`events=2 delivered=2 HTTP attempts=3`；末尾明确当次容器、网络、卷及临时文件清零。required Metrics 汇总 `119670125126` 成功由专门 CI 审计回传，本次未自行查询云端或重跑。

原始日志 `/tmp/addp-ci-20261022-063001/job-119660741437.log` 的 SHA-256 为 `54634e180dbd8d850f77e553ff9ec1206761ff73674b7d0c06aab3a2ceed4084`；中央独立审计为同目录 `monitor-9a2-linux-independent-audit.json`，本会话复核记录 `/tmp/addp-host-window-linux-receipt-audit.json` 保存两份证据指纹、八项窗口／十四项通过、零失败跳过、选择成功及清零回执。当前两个探针输入与已验源码字节一致；本轮只补证据，`git diff --check` 通过，不重复运行有效 T2。由此关闭该输入的原生 Linux host-resources 计时修复验收缺口；此前本地 promtool 30 秒／执行 60 秒失败与补清理记录仍按原结果保留，云端成功不证明其唯一根因或已修复。本结论不外推最新共享工作区、原生 macOS、72 小时留存、全部观测功能或生产验收；既有 PostgreSQL 专业 T4 与来源通过记录不变，未设置定时或操作个人服务。


## 同设备双向吞吐同图（2026-10-23）

主机资源页对同一磁盘的读／写、同一网卡的收／发使用一次现有趋势查询，同图显示两条独立折线；按全部有效点共同选择图表单位，并在图旁明确单位及末次评估值。所选方向仍决定采样明细，响应数组乱序不改变方向；真实零值保留，陈旧／缺样各自断开，缺序列、错误设备或评估网格错位拒绝整幅趋势。复用共享 ChartRenderer、格式化和请求隔离，没有新增采集、接口、权限、插值或设备汇总；其他标量、文件系统、磁盘 IO 耗时及 CPU／内存比较保持原单指标路径。规范见平台运行观测 6.3。

冻结基线 `c71bd5cfaddd4607c2d0907691f2d2a06a57d432` 加本批六项前端输入执行 `make test-monitor-frontend`：退出 0，100 项确定性测试、299 项浏览器测试与构建通过。随后仅补图旁明确的容量单位及独立测试截图路径；最终六项输入再次冻结，执行 `npm test`（100／100，无 Skip）、标准 `npm run test:e2e -- e2e/node-resources.spec.js`（65／65）及 `npm run build`，全部退出 0。最终主机页覆盖中英文、620px、双向单位／零值、响应乱序、方向明细、重载恢复、错位拒绝／恢复与独立缺样；其余未受最后单位文本影响的页面复用完整门禁证据，不宣称最终输入又完整重跑299项。现有 Monitor 前端路径及标准门禁自动发现，无新增依赖或 CI 编排。

末版专项首次为64／65，一次点击写入方向后 URL 未变化，截图显示选择弹层仍打开；带 trace 的四组合12次检查、加入输入事件记录的单场景10次诊断及最终标准65项均未复现，不能据此确认该次失败根因或宣称修复了间歇点击／Safari 故障。临时诊断代码已删除，不增加重试、超时或移除 URL／明细断言；新截图使用每次浏览器测试的独立 outputPath。保留首次失败证据 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-monitor-playwright-results-lf2gXA/`。

日志、两版冻结输入及诊断记录位于 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-paired-throughput-ge5httgy/`：`frontend.log`、`inputs.json`、`inputs-final.json` 与 `snapshot-final/monitor/` 下的 `unit-final.log`、`node-verified.log`、`build-final.log`、`node-final.log`、`paired-trace.log`、`selection-probe.log`。最终六项输入与工作区哈希一致。仅为隔离夹具 T1／T3，未运行真实 Safari、Hosted T4、CI 或后台重启，等待专门会话回传，未设置定时任务。当前仍为折线，不将本批计为原图的分钟柱形、内存双Y轴、资源热力或完整监控交付。


构建器独立客户端修正后已实际启动；第二次仍在业务部署前主动停止，用于修正启动清单中一次性 init 的前台成功语义及显式 MinIO 镜像绑定，未发业务请求。自动报告 `local-report-final.json` 记录失败／中断及零残留，实际最高硬配额 6 CPU／10 GiB、未发生 OOM。进一步将全部私有可写目录计入原 4 GiB 停止线，只读复用公共 Go 依赖缓存，及时删除当次已完成 Go 对象缓存，未借用个人凭据或写共享缓存；标准 runner 的初始化序列、目录预算及私有文件回归共 35 项通过。全部重试继续使用首次窗口截止时间，不重新取得一小时。


第三次在 product_compile 阶段由守卫自动报 `owned_volume_stop_line_reached`，业务部署／身份／查询均未执行；报告 `local-report-window-final.json` 明确失败且 zero_owned_resources=true。最近成功样本为 4,235,309,056 字节（低于 4 GiB），独立只读 du 随后观察到私有目录 4,266,260 KiB（约 4.07 GiB）；旧守卫在触线断言前未追加触线样本，不能用最近成功样本冒充峰值，已将样本落盘移到断言之前并增回归。只读主仓库 `.git` 约 1.2 GiB，因此快照唯一切换为同提交的 file:// 浅克隆（depth=1、no-local），不复制无关历史，也不放宽资源／时间／验收断言；标准 runner 仍通过。下一次继续沿首次绝对截止时间与同样停止线，不能计为容量或正式身份已通过。


## 内存容量与使用率双 Y 轴（2026-10-23）

主机资源页选择任一内存指标时，沿一次既有固定查询取得总量、可用量和使用率。共享 ChartRenderer 的显式 `valueAxes` 展示属性统一绘图：左轴为按有效容量样本选择的 IEC 单位，右轴固定 0–100%；只换算字节值，百分比不缩放。各序列按指标身份与同一评估网格对齐，缺样各自断线，错位或缺序列拒绝整图；采样明细仍由 URL 所选指标决定。相邻时段比较只读取使用率，容量缺样不伪造比例或阻止本已完整的使用率比较。不新增指标、接口、权限、采集或 Workbench 持久化配置，不推定内存泄漏。

本地冻结输入根为 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-memory-dual-axis-bwf4sj9m`，基线 `34dc7b291515c59f30ce93d03578f82dd2b26f45`；`inputs-final.json` 记录本批文件摘要，`snapshot-final` 只叠加本批改动，未纳入共享工作区其他 owner 的 WIP。验证结果：

- `make test-common-frontend`：261 项通过，无失败／跳过；双轴映射、范围与配置拒绝、单位、数值、null、主题及旧单轴语义均有确定性验证（`common.log`）。
- `make test-monitor-frontend` 首轮确定性测试 100/101；旧单指标断言同步后，最终 `npm test` 为 101/101，通过（`unit-final.log`）。首轮浏览器 299/301，另两项失败均为告警跳转测试仍预期单一使用率查询；同步为三指标组合，并保留完整的错误时窗拒绝夹具后，沿标准 `npm run test:e2e -- host-alert-links.spec.js resource-comparison.spec.js` 复验 29/29（含新增容量缺样不影响使用率比较场景，`affected-browser-final.log`）。其余输入未变的 273 项沿全量通过证据复用，合计 302 个唯一场景具有通过结果；未再重复整轮，不将最初失败的聚合命令记作通过。初始失败与截图保留在 `monitor-fixed.log` 及 `addp-monitor-playwright-results-VV4xu8`。
- 新增中英文 620 像素窄屏实图已逐一核对左右轴、图例与刻度；浏览器同时验证乱序响应、GiB→TiB 容量单位变化、百分比保持、可用量独立缺样、刷新恢复、URL 重载、采样明细及撤权清除。截图：`/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-memory-axes-9q4js8/zh-cn.png`、`/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-memory-axes-CR6PqE/en.png`。
- Monitor 最终 `npm run build` 通过（`build-final.log`）；Console 标准浏览器入口 `npm run test:e2e -- process-resource-navigation.spec.js --grep 'exact managed host'` 1/1，通过，核对同一 iframe、实际主机趋势及返回筛选（`console-navigation.log`）。
- `make test-frontend-ci-registration` 20 项及 21 个前端登记检查通过（`registration.log`）；现有共享依赖扩散与 Monitor 前端登记已覆盖本批，无需新增 workflow。Hosted 既有脚本的内存比较查询预期同步复用同一指标组合；`make test-node-metrics-online-runner` 的 138 项确定性测试与 Metrics Go 身份夹具通过（`online-runner.log`），不是实际 Hosted 结果。

以上仅为本地 T1/T3 和验收脚本的确定性验证，不计真实身份／数据源 T4 或生产 T5。本轮未查询、触发或重跑云端 CI／Hosted，未设置定时任务，未重启个人服务。CI 和 Hosted 结果由专门会话回传。


本轮最终实现与失败边界（2026-10-23，正式身份本地入口）：Common 检查点及 owner 接线提交 `3f59dc5c56d44f11bb8c8fa7b8518ec36028f965`；入口后续修正至 `d4b31264d`。共享扩散最终 `make test-go` 退出 0，22 个模块通过；最终 `make test-infra-runtime-log-lifecycle` 退出 0，包含 40 项本地入口契约、JS 语法／CI 登记／System ExecutionTrace Go，以及既有 60 项生命周期检查。40 项包含真实回环 Cookie Jar／退出、守卫失败前拒绝新业务请求、客户端与 daemon 分别校验无凭据代理、完整可写预算／触线样本、采样失败步骤和总硬配额。标准入口准备验证通过不等于正式身份实际执行通过。完整 Console 结果仍为上述 192/193，E2E/build 未执行，不把专项四项通过记为全量通过。

实际操作始终共用首次窗口的绝对截止 `2026-10-23 09:10:30 Asia/Shanghai`，没有逐次取得新一小时。浅克隆修正后六个后端与 runtime-log 均已从 `34dc7b291515c59f30ce93d03578f82dd2b26f45` 编译；安全哈希见 `shallow-source-receipt.json`，该次最高已记录 3,377,561,600 字节、6 CPU／10 GiB，随后 MinIO 公共基础镜像匿名令牌请求超时。诊断确认宿主构建客户端与 Docker daemon 的代理不同：客户端仅可显式使用已核验、不含凭据的回环代理；daemon 仅可使用已核验的本地代理。不得继承任意代理／凭据，不向业务容器注入代理，宿主 API 访问固定回环绕过公共代理。修正后在当次 BuildKit 内实际观察到 MinIO 的 Go 构建，公共镜像下载及源码步骤已推进，但尚未证明全部公共构建成功。

最后一次实测 `addp-trace-local-898d068c3ab94a8badc3651328e69299`（源码 `83ef3e512`）仍在 product_minio 阶段自动停止，报告 `command_deadline_exceeded`；该命令未到其 1200 秒上限，整窗也未到 3600 秒，结合入口的共享失败传播可确定来自四秒资源采样命令超时。旧版未记录采样子步骤，不能断言具体是 Docker 列表、卷扫描或绑定目录扫描，也不能宣称已修复其根因。最后成功样本为 2,873,073,664 字节，未触及 4 GiB；未知失败样本的磁盘峰值不记为已知。现为采样器预留 0.25 CPU，运行服务＋浏览器／身份辅助＋采样器合计 7.95 CPU，内存仍在 12 GiB 内；维持四秒采样预算及原停止线，新增失败步骤／耗时记录和业务请求前后守卫。新配额及诊断只经确定性测试，尚未经过新的真实窗口，不认定采样稳定性已通过。

八次实际启动中，首轮报告写入异常使用明确标注的人工复查回执，其余七份自动报告全部为 failed 且清理通过；均未创建业务部署、未准备正式身份、未发送成功／失败查询，也未进行真实 Console／Trace／日志闭环。`local-final-receipt.json` 汇总每次报告哈希、停止阶段、资源记录与独立清理复查；当次容器、网络、卷、构建器和私有目录残留均为零。全部原始安全报告、准备门禁日志及输入摘要位于 `/tmp/addp-p19-formal-local-20261023/`。因此整体仍为部分实现，正式身份验收未执行；不是业务链路失败或通过的证明。后续优先在同样 8 CPU／12 GiB／4 GiB 停止线下确认一个新的最长 60 分钟本地窗口，先核对采样稳定性，再执行正式身份闭环；未经确认不续开窗口，不运行 Hosted、容量或定时，不接管个人服务。

代码提交 `d4b31264dc7308ca7b1b7479e9b697a97580b16b` 推送后单次只读核对：[Platform CI 39881617580](https://github.com/pampa0629/addp/actions/runs/39881617580) 与 [Release/T2 39881617528](https://github.com/pampa0629/addp/actions/runs/39881617528) 均为 queued，不计为通过；安全回执 `ci-receipt.json`。没有手工调度任何 workflow 或设置后续跟踪。


## 网卡收发速率柱状展示与局部缩放（2026-10-23）

主机网卡趋势复用共享 ChartRenderer，以并排柱状显示同一网卡的接收／发送速率。保持现有完整一分钟平均字节速率、原时段、原步长和评估点，不换算累计量、分钟分桶流量或带宽利用率；图旁说明口径与末次值，轴显示 IEC 速率单位。真实零值保留，陈旧／缺样独立为 null；错误设备、网格错位、预算错误及撤权继续清除旧趋势。磁盘保持折线，内存保持双轴。共享展示属性 maxRows 显式允许该有界视图最多 1,000 行，其他柱图默认仍为 500，Workbench 持久化契约不变。密集常显数值标签关闭，tooltip 与采样明细保留数值及单位。

近千组柱在窄屏全窗会成为密集概览，因此同一共享 renderer 提供本地缩放滑块。缩放保留全部 rows 与 series 数据，不触发请求或重算指标；临时 start/end 通过共享事件交宿主保存，刷新同一图表恢复，切换主机／网卡／时段／身份或查询失败时清空。组件通过既有主题变量绘制滑块，主题重绘保留当前相对视口，不新增持久化、接口、权限、依赖版本或后端开关。

本地冻结输入：`/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-network-bars-gate-k95pp9k0`，基线 `79751c6233d7650a50db7ae060ccdc91a43ba624`，最终实现与测试摘要见 `inputs-final.json`／`snapshot-final`。期间共享版本提交 `9e5fa266b`（v0.1.24）已包含产品实现；交付前逐文件核对，当前工作区实现与冻结输入相同，最终测试增强和本节另行提交。

- `make test-common-frontend`：最终 264/264，通过，无失败／跳过；覆盖默认上限不变、显式上限类型／边界／超限拒绝、饼图拒绝覆盖、完整结果约束、隐藏标签仍保留零／null／tooltip、缩放保留全数据及主题颜色（`common-final.log`）。
- Monitor `npm test`：101/101，通过（`monitor-unit-final.log`）。沿标准浏览器入口 `npm run test:e2e -- node-resources.spec.js network-bars.spec.js`：最终 69/69，通过（`browser-final.log`）；随后仅增强绘图动画完成等待和时段切换重置断言，`npm run test:e2e -- network-bars.spec.js` 最终 2/2，通过（`network-settled-browser.log`），其余 67 项输入未变、沿有效证据复用。覆盖中英文 620 像素窄屏、5 分钟 21 点／1 小时 241 点／24 小时 961 点／7 天 984 点、乱序响应、原步长与单位、各方向独立缺样／陈旧值排除、真实零值、切换方向及采样明细、错误设备／预算失败后恢复；既有磁盘、内存双轴与撤权回归同时通过。
- 缩放使用实际鼠标拖动绘制出的滑块手柄，确认不新增请求，完整两组 984 点仍保留；主题重绘和刷新恢复视口，切换时段及查询错误清空视口。最终截图等待图引擎动画实际完成，已核对中英文五分钟及七天局部稳定绘图：`addp-network-bars-EXT7Am/zh-cn-5m.png`、`addp-network-bars-MPmUTl/zh-cn-7d-zoom.png`、`addp-network-bars-8UKtyV/en-5m.png`、`addp-network-bars-n2SGks/en-7d-zoom.png`（均位于同一 macOS 临时目录父级）。
- Monitor `npm run build` 通过（`build-final.log`）；`make test-frontend-ci-registration` 20 项及 21 个前端登记检查通过（`registration.log`）。现有共享依赖发现、Monitor 前端 Make 入口与 Playwright 自动发现覆盖新增 spec，无需新增 workflow。

初始失败不计通过：新增七天用例曾误写 630 秒／961 点，核对后端既有 plan 后修正为 615 秒／984 点，未改变产品步长、上限或查询；初始滑块测试使用配置坐标偏移，真实渲染手柄中心受图引擎布局影响，点击未命中。运行时绘制边界诊断保留在 `zoom-hit-test.log`；最终从实际绘制手柄取点并执行真实拖动，未增加等待预算、重试或减弱缩放断言。初始失败日志与报告分别保留在 `monitor-browser.log`、`network-zoom-browser.log` 及对应 `addp-monitor-playwright-results-*`；最终等待动画完成避免将过渡帧当稳定实图，未据此修改产品数据或过滤模式。

以上为本地 T1/T3，不计真实来源与身份的 Hosted T4 或生产 T5；未运行完整 Monitor 其他领域浏览器全集，不外推其结果。本轮未查询、触发或重跑云端 CI／Hosted，未设置定时任务、重启个人服务或修改个人数据库。CI 和 Hosted 结果等专门会话回传。清单第 38 行仅补网络柱图的局部缩放，第 39 行补网卡速率柱图；不能将全部图表拖拽缩放、图例筛选、资源热力面板及原行其他缺口同时标为完成。


## CPU、内存与磁盘趋势复用共享时间缩放（2026-10-23）

主机 CPU、内存和磁盘折线趋势接入既有共享 ChartRenderer 滑块，与网卡柱图沿同一路径保存临时视口。缩放只改变图中可见范围，完整 rows／series、查询窗口／步长、采样明细及相邻时段比较保持不变；说明紧邻图表展示。视口绑定主机、指标组、设备维度、时段、固定锚点、比较模式和当前身份，刷新及主题重绘保留；同组内存指标、同设备读写方向切换保留，不同组或上述范围变化以及查询失败时清空。不新增指标、API、权限、依赖或持久化配置；负载、运行时长、文件系统、服务及引擎图表不在本批接入范围。

本地冻结输入根为 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-host-time-zoom-io4lexon`，基线 `a53690c9f933e7ca0a6a32f18bfee0a9a6f043e3`，`snapshot` 只叠加本批路径，`inputs-final.json` 记录七项实现／测试／规范的 SHA-256。交付前工作区逐文件一致；期间 main 上其他提交未改变 Monitor 前端、common-frontend 或本批标准测试执行脚本。现有 Make 前端 owner 登记、共享依赖扩散与 Playwright 自动发现覆盖新增场景，无需新增 workflow。

- `make test-common-frontend`：264/264，通过，无失败／跳过（`common.log`）；Monitor `npm test`：101/101，通过（`unit.log`）。共享 renderer 产品代码未再修改。
- Monitor 标准 `npm run test:e2e -- node-resources.spec.js network-bars.spec.js host-resource-zoom.spec.js resource-comparison.spec.js`：最终 96/96，通过（`browser-final.log`）。新增九项覆盖 CPU／内存／磁盘中英文窄屏、真实鼠标拖动、七天完整 984 点不裁剪、主题及容器尺寸变化、手动／自动刷新后恢复、内存双轴和同设备双方向切换、独立缺样、时段／比较模式／不同指标组切换及查询失败／身份撤销清除；CPU 和内存时段对比仍使用原完整范围，采样明细行数和比较结果不受局部缩放影响。既有主机资源、网卡柱图和时段比较回归同时通过，未把此结果外推整个 Monitor 浏览器全集。
- Monitor `npm run build` 通过（`build.log`），仅有既有产物 chunk 大小提示；`make test-frontend-ci-registration` 20 项及 21 个前端登记检查通过（`registration.log`）。
- 等待图引擎实际绘制动画完成后，逐一核对六张稳定图：中文 CPU／内存／磁盘分别为 `addp-host-zoom-wyeyJO/cpu-zh-cn.png`、`addp-host-zoom-DgobXJ/memory-zh-cn.png`、`addp-host-zoom-dVqTTx/disk-zh-cn.png`，英文分别为 `addp-host-zoom-ZNYrtD/cpu-en.png`、`addp-host-zoom-SsDgoL/memory-en.png`、`addp-host-zoom-xn49JR/disk-en.png`（均在上述冻结输入根的同一 macOS 临时目录父级）。轴、单位、内存双轴、图例、局部时段与滑块均可见，窄屏无横向溢出。

初始新增场景为 8/9，自动刷新场景失败于测试夹具读取不到图引擎模块；日志 `zoom-browser.log` 与报告 `addp-monitor-playwright-results-ymYElk` 保留。查明 Playwright 虚拟时钟同时替换 Performance 的资源读取接口并返回空数组；共享测试 helper 改为从页面实际资源请求记录模块 URL，再查询真实图实例，不保留性能表 fallback，也不改变产品逻辑、等待预算、重试或业务断言。修正后自动刷新专项 1/1（`automatic-zoom.log`），随后上述最终 96 项整组通过；最初失败不计通过。测试时钟只在隔离浏览器验证刷新，不是定时任务。

以上为本地 T1/T3 与构建／登记证据，浏览器使用受控 HTTP 夹具，不计真实来源、正式身份、Safari／Hosted T4 或生产 T5；本批未新跑 T2，原指标口径和来源未变。未查询、触发或重跑 CI／Hosted，未设置定时任务、重启个人服务或修改个人数据库；云端结果等专门会话回传。清单第 38 行仅补 CPU／内存／磁盘共享时间缩放子项，不能将所有图表缩放、完整图例交互验收或原行其他缺口一并记为完成。


## 主机趋势图例筛选与悬停交互验收（2026-10-23）

沿现有共享 ChartRenderer 补齐主机 CPU、内存、磁盘及网卡趋势的真实鼠标交互验收，产品渲染器、页面、指标、API、权限及查询均未修改。测试共用唯一 `resource-chart.fixture.js`，将原缩放专用状态读取统一为图表状态读取，按真实绘制图例的边界点击，并用图引擎坐标转换定位原始评估点后移动鼠标；不以 dispatchAction 模拟点击或直接调用 tooltip formatter 代替交互。已有缩放与网卡测试同步使用该 helper，旧函数路径删除。

冻结根 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-trend-interactions-mlaz8a33`，基线 `3217e418c76a837ce91e0865969341b52ba5ca03`；`inputs-final.json` 记录四项测试输入，交付前逐项哈希一致。标准入口提供独立回环 Vite 端口、源码及缓存隔离和退出清理，没有访问个人 Console 或登录态。本轮最终使用已安装的 Node v24.11.1（`fnm exec --using v24.11.1` 仅影响当次命令，遵从根 `.node-version`，不修改全局配置）。

- `npm run test:e2e -- host-trend-interactions.spec.js network-bars.spec.js host-resource-zoom.spec.js`：最终 22/22，通过，无失败／跳过（`browser-node24.log`）。新增 11 项覆盖四类主机图表的中英文 620 像素场景、单项隐藏／全部隐藏／恢复、悬停时间与精确值及单位、真实零／空值／陈旧值、缩放后原始索引、内存左右轴单位、采样明细和 URL／查询次数不变；CPU／内存完整时段比较结果不受显隐／悬停／缩放影响，查询失败及身份撤销清除可见提示与旧图。原 11 项缩放／网卡场景同时通过，未外推全模块或其他趋势图。
- Monitor `npm test`：101/101，通过（`unit-node24.log`）；`npm run build` 通过，仅有既有 chunk 大小提示（`build-node24.log`）。`make test-frontend-ci-registration`：20 项及 21 个前端登记检查通过（`registration-node24.log`）；新增 spec 沿既有 Playwright 自动发现，未新增入口、依赖或 workflow。
- 已逐一查看最终八张悬停稳定截图，确认中文／英文时间、CPU 百分比、内存 GiB 与百分比、磁盘及网卡 MiB/s 均清楚，缩放后图例和提示可见。截图父目录为上述冻结根的同一 macOS 临时目录父级：中文 `addp-trend-interaction-wE3AC0/cpu-zh-cn.png`、`addp-trend-interaction-WtUfKD/memory-zh-cn.png`、`addp-trend-interaction-VPTfxJ/disk-zh-cn.png`、`addp-trend-interaction-uMrVNX/network-zh-cn.png`；英文 `addp-trend-interaction-Oiz3T8/cpu-en.png`、`addp-trend-interaction-tQcryg/memory-en.png`、`addp-trend-interaction-XnPQa1/disk-en.png`、`addp-trend-interaction-2TGDiL/network-en.png`。

最初单项测试将陈旧百分比夹具错误设为超过 100，页面按原响应校验拒绝整图，日志 `interaction-cpu-initial.log` 及报告 `addp-monitor-playwright-results-OkYGg3` 保留。夹具修正为范围内的陈旧百分比，仍要求该点不显示数值，未放宽产品校验、等待或断言；随后专项通过。默认终端 Node 为 v25.2.1，之前的 22 项及新增断言后 11 项通过保留在 `interaction-browser.log`／`interaction-browser-final.log`，最终交付结果以符合仓库工具链的上述 Node 24 门禁为准。

本批仅为受控 HTTP 夹具的本地 T1/T3、构建及登记证据，不计真实来源、正式身份、Safari／Hosted T4 或生产 T5；未新跑 T2 或其余领域浏览器全集，未重跑输入未变的共享组件单测。未查询／触发／重跑 CI 或 Hosted，未设置定时任务、重启个人服务或修改个人数据库。清单第 38 行可补上述四类主机图表的图例筛选与悬停本地验收，不代表所有图表、真实运行及原行其他缺口均已完成；CI 和 Hosted 等专门会话回传。


## 负载、运行时长与文件系统趋势统一时间缩放（2026-10-23）

主机页全部既有趋势统一开启现有共享 ChartRenderer 滑块，删除原 CPU／内存／磁盘／网络类别开关；新增覆盖负载、运行时长、文件系统容量／比例及 inode 数量／比例，不新增 renderer、指标、API、查询或权限。沿既有相对视口、图旁说明、原查询步长和完整结果；刷新保留，指标组、主机、时间、比较模式或身份改变及查询失败时重置。文件系统沿完整 device／mountpoint／fstype 区分对象，容量与 inode 的不同指标切换重置；不按挂载名猜同一资源。原有负载任务数、时长秒、容量 IEC、比例与数量口径均未改变。

冻结根 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-host-all-trend-zoom-kztnpbgt`，基线 `dcf9b22460d42ff3d8782051e214cf5da1746ab6`，`inputs-final.json` 为四项实现／测试／规范输入摘要，交付前与工作区逐文件一致；快照不包含其他会话 WIP。所有命令使用已安装 Node v24.11.1（`fnm exec --using v24.11.1`），沿标准入口及独立端口运行：

- Monitor `npm test` 101/101、`npm run build`、`make test-frontend-ci-registration` 的 20 项及 21 个前端登记均通过（`unit.log`、`build.log`、`registration.log`）；构建仅有既有 chunk 大小提示。共享 renderer 产品代码、依赖及测试注册入口未改，既有 Monitor owner／Playwright 自动发现覆盖本批，无需新增 workflow。
- `npm run test:e2e -- node-resources.spec.js network-bars.spec.js host-resource-zoom.spec.js host-trend-interactions.spec.js resource-comparison.spec.js` 首轮 119/120，一项在窄屏切换后的同步布局检查失败，不能记聚合通过（`browser-final.log`，失败报告 `addp-monitor-playwright-results-6Mdfe1`）。完成下述诊断并仅修测试的绘制完成条件后，`npm run test:e2e -- host-resource-zoom.spec.js` 最终 22/22，通过（`zoom-final.log`）；其余 98 项输入未变，复用本轮通过证据，合计 120 个唯一相关场景具备通过结果，不声称首轮命令成功或整个 Monitor 浏览器全集通过。
- 缩放矩阵覆盖中英文 620 像素、七天 984 点、负载小数、运行时长、文件系统 GiB／百分比与 inode 整数／百分比、真实鼠标拖动、不新增请求或裁剪数据、主题及宽窄切换、刷新保留、缺样断点、范围／错误重置。新增受控两挂载点夹具沿真实表格按钮选择，验证刷新保留、挂载变更及容量／inode 切换重置与精确请求维度；现有自动刷新场景增加负载缩放恢复，原 CPU／内存／磁盘／网络和时段比较／撤权回归保留。
- 最终十二张新类别中英文稳定实图已逐一核对单位、曲线和滑块，路径见 `zoom-final.log` 中 load／uptime／filesystem-percent／filesystem-capacity／inode-percent／inode-count 截图条目，均在上述冻结根同一 macOS 临时目录父级；未将沿用的运行时长秒轴改为其他时间单位。

失败诊断保留 `layout-diagnostic.log`、`resize-event-diagnostic.log`、`resize-observer-diagnostic.log`。首次观察未重现瞬态，不据单次通过宣称已定位；随后在宽屏实际绘制完成再缩回窄屏的事件采样中，记录 viewport=620、容器=572、Canvas=852、页面 scrollWidth=876，绘制完成后 Canvas=572、页面=620。共享 ResizeObserver 按既有异步路径正常恢复；旧测试在两次视口设置后立即检查页面宽度，未等待实际绘制。最终用同一默认断言预算等待两次 Canvas／容器宽度一致且页面无溢出，不加重试、延长超时、放宽宽度限制或修改产品布局。三个诊断场景不是交付通过计数；本批最初负载／挂载专项 3/3 另存 `zoom-focused.log`。

以上为本地 T1/T3、构建及登记证据，浏览器为受控 HTTP 夹具，不计真实来源、正式身份、Safari／Hosted T4 或生产 T5；未新跑 T2 或未涉及 owner 的门禁，未把缩放接入外推为专业图表或全部原始功能完成。未查询／触发／重跑 CI 或 Hosted、设置定时任务、重启个人服务或修改个人数据库。清单第 38 行可补主机负载／时长／文件系统共享缩放的本地结果；真实页面与来源验收仍等专门会话回传。


## 主机运行时长趋势的自适应显示单位（2026-10-23）

主机运行时长曲线按完整已加载时窗内有效点的最大秒数选择秒／分钟／小时／天，纵轴与 tooltip 使用同一单位、两位小数；缩放不重选量级，刷新跨量级可以更新。共享 `scaleDurationValue` 与原复合时长格式化共用唯一 day／hour／minute／second 换算表，不读取时区或舍弃原始精度；Monitor 只换算图表 rows 与字段呈现。API 秒值、采样证据、重启判断、当前卡片及采样明细的原复合时长格式均不变。零值有效，陈旧／缺样不选级且保持 null，全缺样默认秒；不新增配置、依赖、API 或权限。

冻结根 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-uptime-units-k46w9uio`，基线 `d01c5772f84d19c74010d1bd048433c8000d85be`；最终 `inputs-final.json` 记录十一项产品／测试／规范输入，交付前与工作区逐项一致，快照排除其他 owner WIP。各命令通过 `fnm exec --using v24.11.1` 使用根工具链，沿已有独立端口及清理入口：

- `make test-common-frontend`：267/267，通过，无失败／跳过（`common.log`）。包含时长量级阈值、小数／零／非法值及原复合时长格式回归；其他共享单位与图表约束保持。Monitor `npm test` 101/101、`npm run build` 及 `make test-frontend-ci-registration` 的 20 项／21 个前端登记均通过（`unit.log`、`build.log`、`registration.log`），构建仅有既有 chunk 大小提示。现有共享依赖发现、Monitor owner 与 Playwright 自动发现覆盖新增 spec，无需修改 workflow。
- 新增 `npm run test:e2e -- uptime-units.spec.js` 首次 2/2，通过（`uptime-browser.log`）；覆盖中英文秒→分钟→小时→天、真实悬停、跨量级刷新保留缩放、图例显隐、原采样明细与当前卡片不变、陈旧极值排除、独立缺样／全缺样、真实零、错误清除与恢复，查询仍只含原 uptime 指标。267 项共享 T1 与真实 Monitor 消费页面共同验证新格式化入口，不外推其他消费模块的完整页面验收。
- 组合 `npm run test:e2e -- node-resources.spec.js network-bars.spec.js host-resource-zoom.spec.js host-trend-interactions.spec.js resource-comparison.spec.js uptime-units.spec.js` 首轮 115/122，不能记成功（`browser-final.log`，失败报告 `addp-monitor-playwright-results-g8XJhD`）。失败包括拖动未改变视口、动画／加载等待、创建 page 阶段超时、明细点击、错误切换及缩放值捕获；创建 page 的一项尚未运行产品。未延长 5 秒断言／30 秒场景预算，不加重试或跳过。
- 确认滑块实时事件经过 ECharts 节流：最初新英文场景捕获 54.770992366412216，后续实际视口为 59.732824427480914。共享测试 helper 改为在 mouseup 后等待已应用 start／end 与实际滑块 `_range` 一致，再保存或比较；只等真实事件完成，不调用 dispatchAction 或强制 flush，不改变产品行为。最终 `npm run test:e2e -- host-resource-zoom.spec.js host-trend-interactions.spec.js network-bars.spec.js uptime-units.spec.js` 37/37，通过（`affected-browser-final.log`）。
- 其余三个失败场景保留原产品和断言，沿 `npm run test:e2e -- node-resources.spec.js resource-comparison.spec.js --grep 'network throughput restores|memory capacity and usage.*zh-cn|expired-valid clears' --trace on` 诊断复验，3/3，通过（`remaining-browser-diagnostic.log`）。未受 helper 调整影响的其余 82 项沿本轮有效通过证据复用，合计 122 个唯一相关场景具有通过结果；不宣称初始聚合通过或完整 Monitor 浏览器全集通过。

诊断 `interaction-diagnostic.log` 的三个场景通过，记录了实际鼠标命中、绘制帧、页面可见性与请求错误，未复现最初的加载／动画失败；临时诊断 hooks 已从最终快照和工作区移除。新的通过结果不证明这些历史偶发失败的根因已经确定或修复；只有上述滑块中间状态的同步条件得到针对性补齐，其余历史加载／动画超时仍待定位，不据 CPU 使用率快照推定原因。原 34.6 分钟聚合执行失败保留，不通过重试计成功。

最终中英文窄屏悬停图已逐一核对：`addp-uptime-units-VtugVb/zh-cn.png`、`addp-uptime-units-Fq3pIK/en.png`（与冻结根位于同一 macOS 临时目录父级）。286,435 秒的图轴及悬停显示 3.32 天／days，明细仍为 3 天 7 小时 33 分钟／对应英文；图例和缩放保留。以上为本地 T1/T3、构建／登记，不计真实来源、正式身份、Safari／Hosted T4 或生产 T5，未新跑 T2。未查询／触发／重跑云端 CI 或 Hosted、设置定时任务、重启个人服务或修改个人数据库；云端验收继续等专门会话回传。清单第 38 行仅补时长趋势显示量级与当前本地验证，历史偶发失败和其他原行缺口不能据此关闭。


CI 专门会话回传补充（2026-10-23）：固定提交 `2cd3e278fc00c19183f3508f45253d31e9eef9f8` 的 [Platform Monitor frontend Job 120242877828](https://github.com/pampa0629/addp/actions/runs/39901181026/job/120242877828) 已实际执行 `Run Monitor frontend gate` 并成功。Owner 已核对 Job 的提交身份／实际步骤及原始日志：101 项单测、339 项浏览器均通过，失败／跳过为零，构建 10.36 秒；包含 memory／disk／network 中英文六项图例／悬停／缩放及新增两项时长单位场景。原始材料为 `/tmp/addp-ci-20261023-070013/critical-120242877828.json`、`job-120242877828.log`、`Monitor-2cd3-frontend-actual-pass-audit.json`。这是该固定输入的前端 CI 证据，不表示父 Platform workflow 全部通过，不覆盖后续服务独立缩放／进程环比提交、实源告警 T2 或 Hosted；旧输入失败及尚未复现的加载／动画偶发根因继续保留，未重跑任何门禁。

## 主机固定目录全部图例与悬停交互（2026-10-23）

本批只补齐既有主机资源交互的本地验收，不新增产品指标、算法、权限、接口或部署变更。`host-trend-interactions.spec.js` 沿同一鼠标／图例／缩放 helper，覆盖当前全部 25 项固定指标：CPU 核数／忙碌率、内存三项、1／5／15 分钟负载、运行时长、五项文件系统容量、四项 inode、五项磁盘及两项网卡指标。每种组合实际读取对应采样时间／数值／单位，验证零与缺样／陈旧值、图例隐藏至全隐藏及恢复、缩放后精确悬停、原始图表点／URL／采样明细不变且无额外查询；负载按任务数、inode 与核数按整数，容量／速率和时长沿现有单位。错误／撤权清除补到负载与字节／inode 挂载分支。

冻结基线 `d23233e15e61eb77d5bef576043eef11ec210a99`，证据目录 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-host-interactions-rest-5ixp4m82`；`inputs.json` 记录本批文件，`frontend-inputs-sha256.json` 记录 432 项前端输入，提交前与共享工作区一致。Node v24.19.0 下，最终标准入口 `npm --prefix monitor/frontend run test:e2e -- host-trend-interactions.spec.js` 48/48 通过（`interaction-final-browser.log`，4.2 分钟，42 个中英文指标组合、2 个原比较回归和 4 个错误／撤权场景；不将此前 40 项聚合重复累计）。`npm --prefix monitor/frontend test` 110/110 通过，`make test-frontend-ci-registration` 的 20 项及 21 个前端登记检查通过（`unit.log`、`registration.log`）；原有 Playwright glob 和 Monitor CI 自动覆盖本文件，无需另加编排。42 张窄屏实图保存在 `screenshots/`，负载和 inode 截图已人工核对。

本批仅为受控 HTTP 夹具加真实图表的本地 T3 与 T1／登记证据；产品源码未变，未重建或重跑输入未变的其余模块／前端全集。未运行 T2、真实身份／来源 T4、Hosted 或生产 T5，未追查／重跑 CI、设置定时任务或重启个人服务。历史加载／动画偶发失败仍不能由本次通过推定根因已修复。原表第 38 行图例／悬停本地专项缺口可关闭，真实运行、留存与原行其他缺口保留。


## 主机 TCP 当前连接数（本地）

2026-10-23，按用户确认口径新增固定主机标量 `node.tcp.current_connections`，单位 `connections`，只读当前宿主网络命名空间的 ESTABLISHED＋CLOSE_WAIT，不含监听／TIME_WAIT。复用唯一 `resourcequery` gauge、节点与来源身份、预算、新鲜度、即时和趋势 API；固定主机目录由 25 项增至 26 项，标量由九项增至十项。整数须在 0～2^53−1，合法零值保留，缺样不变成零；采集器缺失、重复原始序列及外来节点不能构造有效值。受限 Docker Desktop 来源不开放 netstat，不补容器命名空间统计，返回 `no_data`。页面新增就近口径／缺样提示、即时卡片及同一趋势、缩放、图例与悬停能力。CPU／内存摘要、TOP5、告警、生产权限及身份不变。

冻结产品基线 `3f0105d08bf2fa632f1888c5a9331c062cf89240`，仅叠加本批文件，证据目录 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-tcp-current-7nslrz9k`；`inputs.json` 记录本批文件，`product-inputs-sha256.json` 记录 2118 项产品、测试及共享依赖输入，提交前与共享工作区一致。登记检查另同步当时最新已提交编排输入，版本见 `registration-head.txt`；未混入其他会话未提交实现。实际验证：

- `make test-module MODULE=monitor GO_ONLY=1`：Monitor Go T1 通过（`go-t1.log`）。固定目录、节点／来源隔离、整数／安全上限、负值／非有限值、缺样／陈旧／未来样本均有确定性用例；默认跳过的集成用例不算 T2。
- `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=host-resources`：退出 0（`host-resources-t2.log`）。真实 node_exporter／Prometheus 的全来源即时及趋势、受限 Desktop 的 TCP 无数据分支通过；同一入口执行五个固定 promtool 场景：有效零、有效数量、重复来源、外来节点和缺失采集器。原有主机来源故障恢复及预算回归通过，退出核验当次容器／网络／卷和临时文件零残留。这是本地隔离 T2，不是原生 Linux 正式身份页面 T4，亦不作为服务实例 CPU／RSS 告警实源验收结果。
- Node v24.19.0 下 `make test-monitor-frontend`：111 项 T1、405 项浏览器 T3 及构建全部通过（`frontend-full.log`）。覆盖 TCP 中英文整数／零值、来源缺失后恢复、错误小数／负数／越界拒绝，及真实鼠标图例／悬停／缩放；其余主机、服务和错误／撤权场景一并回归。`screenshots/` 保留中英文 TCP 窄屏趋势实图，中文精确整数与单位已人工核对。页面测试使用受控 HTTP 夹具，不冒充真实来源页面。
- `bash scripts/swagger/gen-swagger.sh monitor` 与 `bash scripts/swagger/check-route-coverage.sh monitor`：最终生成成功，90 个公开路由方法覆盖一致（`swagger-generation-final.log`、`swagger-coverage.log`）；生成工具固定 module v1.16.6。首次未复制根 go.work 的生成失败仅为临时副本准备错误，保留在 `swagger-generation.log`，不计为通过证据。
- `make test-node-metrics-online-runner`：最终 139 项本地入口／身份／收据守卫及 System 既有 Metrics Go 夹具通过（`online-runner-final.log`）；`make test-frontend-ci-registration`：20 项检查、21 个前端登记通过（`registration-final.log`）。原有 Monitor 指标 T2、前端及 Console／Online 路径自动发现覆盖本批，无新增 workflow 或标准测试入口。
- `npm test -- --testNamePattern='Online scalar resource receipt identity'`（Console frontend）：最终 8 项指定收据测试通过（`console-receipts-final.log`），包含精确十项且拒绝旧九项请求；其余 186 项因过滤未执行，不计为 Console 全量通过。

用户同日确认原一次性 `platform-node-metrics` 临时平台管理员限定用途增加当次主机 TCP 当前值、趋势和同一真实 Console 页面。规范与标准 runner 已同步精确十项标量；仅为多出的 TCP 选中与恢复两轮各六读，将原 174 项收据上限调整为 186，仍严格拒绝未授权指标、重复／缺失、错误节点或 query。总预算、密码／MFA、独立 Prometheus、节点范围、无租户业务及退出清零保持原约束。本会话没有触发或轮询 CI／Hosted，没有设置定时任务或重启个人服务；真实身份／来源 T4 及 CI 等专门会话回传，生产 T5 未运行。原表第 37 行（B36，主机实时资源总览列表）的 TCP 指标能力已落地，但主机列表仍只展示核心摘要，综合健康评分与完整总览列仍未实现；第 38 行（B37，资源趋势曲线图）新增 TCP 本地证据，不由本次结果推导留存、长期比较或其他真实运行范围通过。


## 主机资源热力面板（本地）

2026-10-23，主机详情新增 Monitor 领域组合组件 `HostResourceHeatmap.vue`，复用已授权的即时 CPU 忙碌率、内存使用率和每个完整设备／挂载点／文件系统身份的使用率；不新增采集、接口、查询、权限或持久状态。百分比沿原指标与共享格式化，不从容量重新推导；CPU 保留一分钟窗口，每项就近展示原状态与采样时间。有效使用率按 0～100% 连续混合现有主题绿／红色，基础进度条复用 Element Plus；合法零和 100% 有效，陈旧／缺样不着色、不补零。未采集分区和来源失败显示原原因，分区不可用不抹除可读 CPU／内存。完整挂载身份保留于色块及趋势导航，CPU／内存导航清除挂载维度。没有综合健康分、颜色告警阈值或新增资源规则。

冻结基线及本批文件见 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-host-heatmap-s0n4pjbs/final-inputs.json`；最终 436 项前端／共享／浏览器入口输入记录在 `final-frontend-inputs-sha256.json`。初始完整回归的产品输入相同，仅本次新增测试的刷新同步／截图检查和服务环比测试的请求同步随后修订，完整回归输入另记 `full-frontend-inputs-sha256.json`，不把两次输入混为一份。

- `make test-monitor-frontend`：完整批次执行 111 项 T1，通过 407 项浏览器用例、失败 2 项，命令退出 2，未执行其后的构建（`frontend-full.log`）。两项分别为新热力用例在刷新完成前挂起下一轮，以及既有服务环比用例在取消比较后、尚未取得新摘要与趋势请求前断言上一条请求。最后仅修正这两个测试文件，产品及共享输入未变；最终 `npm --prefix monitor/frontend run test:e2e -- host-resource-heatmap.spec.js process-comparison.spec.js` 的 19/19 项通过（`final-affected-browser.log`，4 项热力及 15 项服务比较）。其余 390 项未变用例复用上述完整批次有效结果，不将完整入口先前的非零结果写成最终全量重新通过，也不重复累计交叠用例。服务用例用受控挂起唯一 `process_resource_summaries` 响应，证明 UI 已取消比较时旧请求仍在；释放后必须新增且只新增一条正确五分钟趋势请求，再核对起点。受控探针初轮误指向不存在的 observations 端点，失败记录保留在 `final-affected-browser-before-endpoint-fix.log`，已改为 API 客户端实际使用的 summaries，未改产品请求或数据口径。
- `npm --prefix monitor/frontend test`：111 项 T1 通过（`unit.log`）。
- 最终标准 `npm --prefix monitor/frontend run test:e2e -- host-resource-heatmap.spec.js`：4/4 浏览器 T3 通过（`heatmap-final-verified.log`），覆盖中英文精确数值／原挂载比例、相同挂载点的不同设备／文件系统、窄屏、主题变量动态变化、合法零／满量程、无额外请求、当前指标／完整挂载导航、陈旧／缺样不着色、分区故障后恢复、未采集、权限拒绝及身份切换后迟到响应清除。初轮失败为测试对 URL 编码和现有缺源文案的错误预期、夹具改写未采集状态，以及在上一轮刷新完成前挂起下一轮所造成的测试自锁；最终使用解析后的 URL、原文案／数据状态并在挂起前等待既有刷新按钮可用，未增加产品重试或改变刷新逻辑。
- `npm --prefix monitor/frontend run build`：最终构建通过（`build-final.log`）；未修改后端/API/Swagger，无需重建后端或新增 T2。
- `make test-frontend-ci-registration`：20 项检查及 21 个前端登记通过（`registration.log`）。原 Monitor Playwright glob 和前端 CI 自动覆盖新增组件／用例，无新增标准入口或 workflow；未触发或轮询 CI。

`screenshots/` 保留最终中英文浅色／深色窄屏面板实图，已人工核对中文浅色布局与单位。T3 使用受控 HTTP 夹具与真实组件，未操作个人 Safari 或重启个人服务；未运行真实身份／来源页面 Hosted、T4 或生产 T5，CI 和 Hosted 等专门会话回传，未设置定时任务。原表第 39 行（B38，主机底层资源明细）的热力面板能力已实现；该行的完整硬件汇总／分区健康判定及新增页面真实运行证据仍需分别核对，不能由本批界面色彩推定健康判定或告警通过。


## 主机列表五项核心摘要（本地）

2026-10-23，按用户确认范围，主机列表展示 CPU 逻辑核数／一分钟忙碌率、内存总量／使用率与运行时长，默认每页 20 台。复用原单位、当前节点身份／版本、采样新鲜度、采集状态和列表生命周期；容量用 IEC 单位，运行时长用复合时长。固定主机与操作列、标题允许换行，每项保留独立状态。当前页只进行一次摘要 API 请求，不逐台读取详情或补齐失败列；返回乱序仍按节点身份映射，单项陈旧不抹除其他有效值，整批缺项拒绝并清空旧摘要，主机列表仍保留。

`/platform/resource_summaries` 唯一契约现在显式携带 `node_ids` 和 `metrics`，只接受上述五项的非空无重复子集，不保留省略参数的旧请求。唯一批量内核复用原公式、两次固定 Prometheus 请求、整批期限／并发和逐节点 User 授权；在 Owner 读取与指标查询前按节点数乘指标数预检预算。默认 20 × 5 = 100，21 台五项请求整批拒绝，不拆批或提高预算；TOP5 与采集配置页显式保持 CPU／内存百分比两项，原 50 台 TOP5 上限、比较和热力指标范围不变。API、调用方、Swagger 及既有 Hosted 收据／拒绝测试同步，仍只在原已确认十项节点指标的读取范围内选五项，不新增权限、角色或身份用途。

冻结基线 `d798875a0732cb18e42d1772799d7c91b007542e`，仅叠加本批文件。证据目录 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-host-list-core-v8j8u6ci`，`inputs.json` 记录最终本批文件，`frontend-inputs-sha256.json` 记录 437 项前端／共享／浏览器入口输入。其他会话的 engineApprovalInitialization 两项共享未提交 WIP 未混入冻结副本；本任务源码及其余前端输入与工作区核对一致，不外推整个共享工作区的 WIP。实际验证：

- `make test-module MODULE=monitor GO_ONLY=1`：Monitor Go T1 通过（`go-t1.log`）。五项／两项显式投影共用内核；20 台五项、50 台两项边界通过，21 台五项在任何 Owner／来源读取前拒绝；空／重复／未知／分组／不在摘要范围的指标拒绝，停用、跨节点、当前用户 Token 与完成时新鲜度沿原规则核验。
- `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=host-resources`：退出 0（`host-resources-t2.log`）。真实 node_exporter／Prometheus 上五项摘要的完整来源及受限 Desktop VM 读取、外来节点缺样隔离通过；原 TOP5 的受控多源覆盖／预算及来源故障恢复回归通过。退出核验当次容器／网络／卷和临时文件零残留；该结果是本地隔离 T2，不是多真实主机、正式身份页面 T4 或生产 T5。
- Node v24.19.0 下，`npm --prefix monitor/frontend run test:e2e -- host-list-core.spec.js`：20 台主机的 2 项中英文专项通过（`core-list-browser.log`），验证一次精确五项批量读取、默认每页20台、CPU 核数／百分比、IEC 容量、复合时长、乱序身份匹配、单项陈旧、整批缺项后的清除／恢复及无逐行详情查询。后续完整门禁另覆盖最终表头换行。
- 最终 `make test-monitor-frontend`：112 项 T1、411/411 浏览器 T3 及构建全部通过，命令退出0（`frontend-full.log`，浏览器约15分钟，构建4.46秒）；本次输入下无失败或跳过。覆盖主机／采集列表的投影隔离、TOP5、详情、热力、趋势与服务／告警回归；不把前面的专项与整批交叠用例重复累计。`screenshots/` 保留最终五项列表中英文实图，已人工核对中文布局、单位与可读表头。
- 首轮 `npm --prefix monitor/frontend run test:e2e -- node-resources.spec.js monitoring-target-list.spec.js resource-overview.spec.js`：89 通过、3 失败（`focused-browser.log`）；失败是旧列表用例仍断言两列数值或不带 metrics 的请求，已按精确五项和必填选择修订，不放宽采集配置页两项断言或产品校验。
- `make test-node-metrics-online-runner`：139 项本地入口／身份／收据守卫及 System Metrics Go 夹具通过（`online-runner.log`）；新增拒绝省略指标选择、旧两项列表请求及缺失核心列的反例。仅为准备好的正式入口本地检查，没有运行 Hosted。
- `bash scripts/swagger/gen-swagger.sh monitor` 和 `bash scripts/swagger/check-route-coverage.sh monitor`：生成成功，90 个公开路由方法覆盖一致（`swagger-generation.log`、`swagger-coverage.log`）。固定 Swag module v1.16.6。`make test-frontend-ci-registration`：20 项检查与21个前端登记通过（`registration.log`）。现有 Monitor 前端／Go／指标 T2、Console／Online 路径自动发现覆盖本批，没有新增 workflow 或标准入口。

本会话不触发／轮询 CI 或 Hosted，不设置定时任务，不修改 IAM 或重启个人服务。因摘要请求契约同次更新，个人环境体验需由用户按标准入口重启 Monitor 后端；前端改动不要求重启其他后端。原表第37行（B36，主机实时资源总览列表）仍是部分实现：本批核心五项已具备，三档负载、TCP 与存储／网卡明细仍在主机详情，未补完整列表或综合健康评分；新列表的真实身份页面 Hosted 与生产规模证据继续待专门会话回传。

## Safari 主机列表右侧列与前台自动刷新（2026-10-24）

沿用户已重启的个人环境及现有平台系统管理员会话，只读复验 `http://localhost:5170/monitor/node-resources`。最初辅助功能树仍报告页面隐藏／自动刷新暂停，原生横向滚动返回 `noWindowsAvailable`；AX Raise／窗口菜单操作未使其恢复。用户手动激活当前 Safari 标签并保持前台后，树显示焦点在当前 HTML、自动刷新已开启，原生滚动条拖动可用。该前后观察说明这次验收所需的现场条件已恢复，不能反推全部历史窗口／截图问题的根因。

已实际截图核对内存使用率与运行时长的表头和值，两列可读、百分比和复合时长单位正确，固定主机名称及查看资源入口仍保留。08:30:23（Asia/Shanghai）显示 43.20%、3 天 12 小时 21 分钟；08:31:35 显示 42.46%、3 天 12 小时 23 分钟，且自动刷新仍开启。整个前台记录区间没有点击手动刷新、重载或修改每 15 秒的选择；仅横向滚动、拖动表格滚动条及临时收起／恢复 Safari 侧栏，中间另留 18 秒一次性观察等待。08:29:19～08:31:58 的六份辅助功能文本记录多次 CPU／内存数值自然变化和时长推进，恢复原侧栏后两列仍可读，默认每页 20 台、当前一台 VM。由此确认本次单机页面前台自动更新及右侧列视觉复验通过；没有网络瀑布证据，不推断精确请求次数、重入并发或隐藏期间网络取消已在此实测。

证据目录 `/tmp/addp-safari-host-list-Su1V5L/`：`foreground-before.txt`、`scrolled.txt`、`right-actual.txt`、`right-columns-final.txt`、`automatic-after-cycle.txt`、`restored-sidebar.txt` 及对应原生 JPEG 截图；`verification.json` 保存采样时间、读数、动作与限制。`health-ready.json` 记录 Monitor HTTP 200、ready／registered、实际后端构建 `e931905933bab26701e2586eb8df9a49d9da4a30`。本次文本／状态／时长推进／观察间隔及截图格式完整性断言通过；首次记录脚本误把原生 JPEG 当作 PNG 校验而失败，已按实际格式改名并核对，未修改截图像素或产品代码。不重启个人服务、不改配置或凭据，没有 CI／Hosted 查询、调度或定时任务。此结果仅为个人单台 Desktop VM 的页面现场复验，不计一次性隔离 Hosted／T4、多真实主机、其他角色／撤权、长窗或生产 T5。


## 主机监控类型／纳管状态筛选与分页回跳修正（2026-10-25）

Monitor 列表复用 System 已有主机 `node_kind`／`enabled` 与名称／地址交集查询，类型／状态选择同时提交名称草稿并回首页；重置保留每页数量及刷新偏好。详情、返回、刷新和 Console 单历史保留已提交条件，指标请求仍只有匹配当前页的五项摘要，空页不请求指标。共享前端唯一 `HostNodeFilters`／`resolveHostNodeFilters` 替换 System 原控件及类型／状态解析分支，删除迁出的“全部类型／状态”词条；不新增后端接口、权限、指标、数据库或部署依赖，System API／Swagger 契约不变。

新增真实分页用例暴露原页面缺陷：切第二页后重读立即将未知总数设为零，Element Plus 的页数监听自动回写第一页。诊断实际记录 `1 → 2 → 1` 三次 System 列表请求，见 `/tmp/addp-host-list-pagination-diagnostic.log`；没有将其归因为查询慢或来源异常。修正为未知总数使用 `null`、只用成功返回的总数发布分页；不加重试、延迟或改写服务端分页。最终八项专项覆盖中英文390px组合筛选、停用字符串false、翻页与每页数量、当前页唯一摘要、空结果无指标读取、失败／旧响应／身份变化清除、自动刷新不提交草稿，以及第二页空结果／失败不回写第一页。

本轮最终命令显式将已核验的 Node `24.19.0` 加入当次 PATH（路径 `/Users/pampa/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin`），不更改个人运行环境。实际结果：

| 标准入口 | 结果及范围 |
| --- | --- |
| `make test-common-frontend` | 277项共享T1通过，无失败／跳过，含规范值、双语和唯一所有权；原System重复路径已删除。 |
| `make test-system-frontend` | 172项单测、240项受控API浏览器回归及构建通过；保留原主机管理的检索、编辑、权限与响应隔离契约。 |
| `make test-monitor-frontend` | 119项单测、436项受控API浏览器回归（16.6分钟）及构建通过；新增八项含上述分页红灯的最终回归。 |
| `make test-console-frontend` | 195项单测、222项受控iframe浏览器回归（10.1分钟）及构建通过。 |
| Console标准browser launcher，`e2e/node-resource-navigation.spec.js e2e/host-node-navigation.spec.js` | 最终六项通过，模块内导航保持文档，浏览器历史按Console既有机制恢复当前iframe和条件，不要求跨外层历史永久保持原Frame对象。 |
| `make test-frontend-ci-registration`；`changed-gate.py --dry-run` | 22项登记回归通过，21个前端登记完整；现有共享／System／Monitor／Console路径已命中，无新Make入口或workflow。干跑包含共享工作区其他owner，仅计路径发现，不计实际门禁通过。 |

保留先前失败：Select内部输入值及空态文案的测试断言错误；Console新增用例错误要求浏览器历史后原Frame不卸载，纠正为已有单历史契约后六项专项和上述完整门禁均通过。分页回跳是真实产品缺陷，已由真实请求序列定位并通过最终回归。首轮误用系统Node25的结果及主动中断轮次不计标准通过；该版本下Console曾有Vite退出缓存未清零失败，未证实根因、不因Node24后继通过宣称修复。所有旧轮均不由局部成功覆盖。

关键16项实现／测试摘要及日志摘要在 `/tmp/addp-host-list-final-receipt.json`，SHA-256 `ec59a5a6613d572360515e95daac332ab8109bef3b52b3d6ec4e9bb10f38f48c`；最终日志为 `/tmp/addp-host-list-{common-node24-final,system-node24,monitor-node24-final,console-node24-corrected-full,console-navigation-node24-corrected,frontend-registration}.log`。这些是本地共享工作区T1／受控T3／构建证据，不代表固定提交CI、个人Safari、真实System身份T4／Hosted、多真实主机或生产T5；本批未运行后端T2（后端未变）。未重启个人服务、操作个人数据库、轮询／派发／重跑CI或Hosted，也未设置定时任务。功能清单第36行仍部分实现，数据库／任务／角色／网卡关联筛选及真实运行验收保留；第37行的其他总览列与健康评分缺口不变。


## 主机列表负载与连接视图（2026-10-26）

主机列表默认保留核心资源五项，新增“负载与连接”视图，仅请求 1／5／15 分钟系统平均负载及 TCP 当前连接数。复用既有指标公式、单位、新鲜度、当前用户逐主机授权及同一批量摘要；后端摘要白名单同步为这两组共九项标量的非空无重复子集，不新增接口或权限。每页默认 20 台，新视图请求 80 条序列，继续在 Owner 裁决和来源查询前校验原整批预算。TOP5／采集配置仍只请求原两项。

唯一 URL 参数为 `list_view=load-connections`，默认核心资源省略；切换保留已提交筛选、分页和刷新，不提交搜索草稿；详情返回、重载及重置筛选保留视图。列标题就近解释负载为任务数而非百分比、TCP 为 ESTABLISHED＋CLOSE_WAIT。有效零、缺样、失败及撤权不混淆，迟到响应不能覆盖新视图。

本地验证：

- Node `v24.11.1` 下 `make test-monitor-frontend` 退出 0：124 项单元测试、450 项浏览器场景（含新增六项）与 Vite 生产构建通过。中英 20 台主机验证单批精确四指标／80 条序列、响应乱序、TCP 有效零和独立缺样；另覆盖窄屏、视图切换、详情返回／重载、重置、超限／撤权、15 秒定时刷新（测试时钟）和身份切换后的迟到响应清除。既有构建大 chunk 提示不影响构建成功。日志 `/tmp/addp-host-list-final-gate.log`；浏览器结果目录见日志 `Browser gate monitor` 行，截图 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-host-load-6wzJX2/zh-cn.png` 与同轮完整门禁截图路径见日志。首轮六项专项有两项因脚本点击 Element Plus 透明 radio 输入框被标签遮挡而失败；改为点击可见标签后，最终完整门禁全部通过，原失败日志 `/tmp/addp-host-load-e2e.log` 保留。
- `make test-module MODULE=monitor GO_ONLY=1` 退出 0，覆盖两组摘要的预算、逐用户授权、停用排除、未知／新鲜度及批量身份隔离；新增四指标 25 台可规划、26 台在 Owner／来源读取前拒绝。日志 `/tmp/addp-host-load-go.log`。本地 Go T1 不替代真实来源 T2。
- `bash scripts/swagger/gen-swagger.sh monitor` 及 `bash scripts/swagger/check-route-coverage.sh monitor` 均退出 0，96 个公开路由方法一致；日志 `/tmp/addp-host-load-swagger.log`。

实源缺口：`make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=host-resources` 退出 2，在公共 mTLS 初次采样前置报 `Timed out: real mTLS scrape and controlled job label`，封闭诊断为 `error_kind=tls/health=down`。八项固定窗口 promtool 测试通过不等于整门禁通过；本批新增四指标真实摘要断言尚未执行。与本专题既有公共采样失败边界一致，不由现有诊断推断具体证书根因，不改 TLS／等待时长、不盲目重跑。日志 `/tmp/addp-host-load-t2.log`；项目 `addp-metrics-t2-786570fda5424602a680465383cd9092` 的容器／网络／卷在退出后分别按 Compose project 标签只读核验均为空，临时目录由标准入口清理。

个人 Safari 初次复验曾因运行中的旧 Monitor 后端仍按五项白名单拒绝新摘要而失败；2026-10-26 用户自行执行 `./scripts/dev/restart.sh -monitor` 后，沿原平台管理员会话只读复验，新视图能读取真实三档系统负载。初次为 8.21／9.07／10.83 个任务，点击页面“刷新”后为 8.32／8.84／10.58 个任务，采集状态为“正在采集”，不再出现摘要读取失败。详情导航保持同一主机 `755953df-488c-410a-88d4-0ecc47eb1351` 及 `list_view=load-connections`，返回列表和整页重新载入仍选中该视图并重新读取负载。

TCP 在列表及详情均为“—／缺失”，详情就近明确“当前来源未提供可用的宿主 TCP 统计，不代表连接数为 0”；仅验证受限 Docker Desktop 来源的缺样表达，不计真实 TCP 有效数值通过。AX 原始证据为 `/tmp/addp-host-load-safari-e8AXcN/load-refreshed.ax.txt`、`host-detail.ax.txt`、`load-reloaded.ax.txt`。截图能见真实负载，但按钮着色与 AX 选中状态存在滞后，右侧横向滚动返回 `noWindowsAvailable`，页面持续报告隐藏；故右侧 TCP 实际截图、前台自然自动刷新不计本轮通过，也不把个人单 VM／管理员会话结果外推为多主机、TCP 有效来源、此前失败的隔离 T2 或正式 Hosted。未操作个人数据库、权限或服务进程，未手工触发／轮询 CI 或 Hosted，未设置定时任务；正式结果仍等专门会话回传。


## 主机列表按设备展开（2026-10-26）

2026-10-26 用户确认先做主机行内按设备展开，不计算跨设备整机摘要。列表及详情的磁盘／网卡表格、单位、列旁解释、缺样状态、活动磁盘排序／无活动设备展开、精确设备趋势入口唯一归属 `monitor/frontend/src/components/HostDeviceResources.vue`，删除 `NodeResources.vue` 原内联设备表格和重复派生；列表读取组合由 `HostDeviceObservation.vue` 消费既有 `resource_observations`。最多打开当前授权页一台主机的一类明细，未展开无设备请求；磁盘固定五项、网络固定两项，沿原一分钟窗口、单次预算及授权，不新增接口、权限、指标或设备总量。

展开只属于临时页面状态；筛选、分页、列视图切换、详情导航、整页重载及身份变化关闭展开。刷新在主机仍处于当前授权页时保留选择、重读数据；切换／收起／离页取消在途请求，迟到结果不恢复旧内容。普通失败／超限清除设备值并保留仍授权的标量列表；设备 401／403／404 清空列表并停止自动读取。原始 node_version 必须与当前行版本相同；未知、未提供、陈旧与真实零分别显示。展开面板按列表可视宽度布局，窄屏刷新按钮可见，宽设备表保留自身横向滚动。

验证使用 Node `v24.11.1` 和原标准入口 `make test-monitor-frontend`：最终退出 0，125 项单元测试、459 项 Playwright 场景、Vite 生产构建全部通过；既有 chunk 大小提示不影响构建。新增九项浏览器场景覆盖中英和窄屏边界、无预取、单主机／单类别精确请求、默认箭头、折叠与有效零、类别切换的迟到旧响应、预算／版本错误及方向独立缺样、采集未提供、全局手动刷新、临时状态清除、精确设备趋势及列表回跳、测试时钟的15秒刷新／隐藏暂停、当前主机离页与撤权停止。旧详情页折叠、说明弹层、设备／网络趋势与交互也在同一完整门禁中通过；组件唯一所有权约束同步。现有 Platform CI Monitor frontend 与 E2E 自动发现覆盖本批，无需新增 Make／workflow 登记。没有重跑未变化的后端或此前公共 mTLS 前置，也未手工触发／轮询 CI 或 Hosted、设置定时任务或重启个人服务。

最终日志 `/tmp/addp-host-devices-final-gate-v2.log`，新增专项 `/tmp/addp-host-device-expansion-responsive.log` 九项通过。窄屏中英截图 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-host-device-list-te3HaS/zh-cn.png` 与 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-host-device-list-PZiaVK/en.png` 已目视检查。首轮 `/tmp/addp-host-devices-final-gate.log` 因目视发现展开区域继承外层宽表宽度、刷新按钮离开可视范围而主动中止，119 项通过／1 项中断／339 项未运行，未计为完整通过；修正宽度后从标准入口完整重跑。早期静态断言仍指向旧内联设备循环，已同步为唯一组件归属，不保留旧实现。

个人 Safari 已从现有管理员会话读取到“设备明细／磁盘 IO／网卡吞吐”入口；工具展开操作被 `ScreenCaptureKit -3811`／`noWindowsAvailable` 阻断。用户随后手动激活并明确反馈“已展开磁盘 IO”，但工具仍返回未展开且报告隐藏的页面。窗口菜单只列一个控制台窗口，重新选中同一窗口后，AX 与截图仍未显示展开；这是用户反馈与工具观测不一致，不直接判为产品失败，也不计为实机通过。只读证据 `/tmp/addp-host-device-safari-jUB6Y1/current.ax.txt`、`after-user-expanded.ax.txt`、`after-user-expanded.png`。真实单 VM 磁盘读数、真实网卡缺样、收起／刷新／面板像素和前台自然刷新均尚未独立验收；本地受控浏览器通过不替代个人实机或正式 Hosted，其他验证仍等专门会话回传。


## 主机列表文件系统容量与 inode 展开（2026-10-26）

沿已确认的按主机行内展开方案新增“文件系统”入口；最多展开当前授权页一台主机的一类资源。容量五项与 inode 四项沿既有顺序分别读取，按完整 `device／mountpoint／fstype` 对齐，保留各自查询／采样时间和错误，不改公式、不跨挂载汇总、不拆分超限组。列表和详情只使用 `HostFilesystemResources.vue` 这一份挂载表格；原内联实现删除，原设备读取组件收敛为 `HostResourceDetailObservation.vue`，旧文件及旧翻译命名删除。沿精确挂载进入既有趋势并保留列表上下文。

本批同时修复行内读取组合漏传共享 `createAutoRefresh` 必需的 `visibilityChanged` 回调：原实现在隐藏／恢复时抛错，切回前台未重读；现按原契约补齐调用，不修改共享工具。新增场景验证隐藏暂停、恢复后两组重读且无浏览器异常，以及收起第一组时不继续读 inode、切换时迟到第二组不回填、第二组拒绝后清空列表并停读。

Node `v24.11.1`：新增八项受控浏览器场景与原设备九项合计17项通过；进一步补充截图后的文件系统专项八项再次通过。覆盖中英／窄屏／横向滚动、同挂载点的设备或文件系统类型差异、乱序对齐、容量与 inode 独立预算／不可用错误、版本拒绝、合法零／缺样、未提供能力、精确趋势与返回、自动刷新及身份撤销。首轮三项失败为两项新测试漏写既有 inode 单位与上述回调问题，保留原失败日志，不计通过。最终标准入口 `make test-monitor-frontend` 退出0：127项单元测试、467项Playwright场景与Vite生产构建全部通过（保留原chunk大小提示）；前端源码及测试输入摘要在门禁前后核对一致，日志 `/tmp/addp-host-filesystem-final-gate.log`。沿已有 `make test-monitor-frontend`、Platform CI Monitor frontend 与 Playwright 自动发现，无新增测试入口或依赖。

日志：`/tmp/addp-host-filesystem-targeted.log`（14通过／3失败）、`/tmp/addp-host-filesystem-targeted-v2.log`（17通过）、`/tmp/addp-host-filesystem-visual.log`（8通过）。截图目录 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-host-filesystem-list-NgAnEN`（中）与 `addp-host-filesystem-list-7grzkp`（英），已目视检查宽屏、窄屏及 inode 横向滚动。既有个人 Safari 工具观测限制未解除，且 Desktop VM 来源不提供 filesystem／inode，不能以受控数据证明个人环境真实分区读数。未运行新的后端／实源 T2、个人 Safari 或 Hosted 验收，未触发／轮询 CI、未设定时任务、未重启服务；当时 CI 与 Hosted 待专门会话回传；Linux 前端结果现已按下段独立复核，Hosted 仍待回传。


2026-10-27 独立复核专门 CI 会话回传的 [Linux Monitor frontend Job](https://github.com/pampa0629/addp/actions/runs/40351996188/job/121674238730)：push／attempt 1，实际 checkout `e5391ca36843cd0d99233ea3f63949abb45d81d0`。GitHub Job 与实际 `Run Monitor frontend gate` 步骤均为 success，`make test-monitor-frontend` 原始日志确认 tests/pass 各127、fail 0、467 browser passed（23.3分钟），生产构建10.55秒完成。产品 `12d3eba1f6d9294ca16c2bfe9204e046e2d872e6` 至该实际提交的 `monitor/frontend`、`common-frontend`、根 Makefile、标准前端／浏览器入口、prepare-frontend-gate 与 Platform workflow 均无差异，因此覆盖本批实现。

通过 `gh api repos/pampa0629/addp/actions/jobs/121674238730` 及对应 run 的只读请求核对终态，再独立下载原始日志至 `/tmp/addp-monitor-e539-independent-job.log`；与回传归档逐字节相同，SHA256 为 `2665d7996277041d5b79bf80c985e6f223a4b944f4b0b69130f1d170df985511`。回传审计 `/tmp/addp-ci-20261026-180018/Monitor_e539_actual_frontend_audit.json`。父 Platform run 的终态为 cancelled，不计整套 workflow 通过；旧超时保留，不能据此认定其根因已修。后继作业若实际跳过也不计第二轮复验。这一证据仅证明固定源码的 Linux 标准前端门禁，不替代 Safari、个人实源、正式身份 Hosted 或生产验收，不扩展文件系统告警范围；本轮未重跑或调度任何验收。


## 真实 Linux 采集与挂载页面验收准备（2026-10-28）

本批增强既有 `platform-node-metrics` 的资源 regression／complete-comparison 浏览器阶段，不新增 profile、权限、接口、读取轮次或超时预算。浏览器启动前只读核对当次标准 node-exporter：唯一项目／服务／仓库归属、运行态、Linux 同内核、host 网络与 PID、非特权只读运行、只读 `rslave` 宿主根挂载、宿主 proc／sys 路径和显式 filesystem collector。拒绝 Desktop、容器根、挂载覆盖、重复／反向参数和 textfile 替代。`node-native-source.json` 仅保存封闭布尔结果和容器 ID 摘要，不导出环境、凭据或完整 inspect；前置失败清除旧部署／浏览器成功收据，并阻止浏览器启动。

真实 Console 脚本使用同一主机的精确五项容量与四项 inode 响应，按完整 `device／mountpoint／fstype` 定位有效挂载，逐列核对身份、横向滚动后的可见数值及单位；格式化复用既有共享函数和指标／翻译定义。`filesystem_mount` 收据保存响应索引、行索引、完整身份和实际 DOM 文本，中央校验再次核对指标、读数和单位，并要求容量／inode 恢复趋势属于同一挂载。并发响应乱序、同设备或挂载点的类型差异、合法零值、旧成功后新缺样／陈旧、错主机／请求范围、错误索引／单位／读数等都有拒绝回归。

本地验证：

- `make test-node-metrics-online-runner`：151 项确定性测试及 System 正式身份夹具 `go test ./cmd/online-test-fixture -run Metrics -count=1` 通过；日志 `/tmp/addp-native-node-runner-final.log`。Docker 部署检查使用受控 inspect 反例，不计真实 Linux 采集通过。
- `npm --prefix console/frontend test`：30 个文件、202 项单元测试通过，其中收据专项 15 项；日志 `/tmp/addp-native-mount-console-unit-final.log`。前一轮全量为 201 通过／1 失败，失败在本批未改的 `devPorts.test.js` 临时 Vite 清理断言，残留名称为 `7102.pid`／`addp-system-vite-test-7102`；最终复跑通过，未修该路径，未确认首次失败根因，不能据此宣称其清理问题已修复。
- 在 `console/frontend` 执行 `npm exec -- playwright test --config=playwright.online.config.js --list e2e/online/platform-node-resources.spec.js`：发现唯一既有场景，导入解析成功；仅发现，不计浏览器执行通过。

继续由既有 Online runner、Console frontend 单元自动发现及手动 Hosted regression 接入，无新增依赖或 CI 路线。本轮不改产品前端／后端、Swagger、个人配置或服务，不重跑既有受控产品前端门禁。本机仍为 Darwin／Docker Desktop，真实原生 Linux、正式登录后的挂载读数及完整 Hosted 零残留尚未运行；CI 和 Hosted 由专门会话安排并回传，不在本会话调度或设定时任务。这是验收实现与本地准备证据，不能填为真实采集／正式页面已验收。


### 列表展开、手工刷新与收起的验收准备（2026-10-28）

沿同一真实 Linux regression，补主机列表关闭自动刷新后的“文件系统”展开、一次手工刷新和收起。首次与刷新分别只读当前节点容量／inode两组，资源收据总上限由186增加至190；产品查询预算、接口、权限、对象范围、suite／浏览器超时不变，不添加等待窗口、规则读取或写入。列表与详情复用唯一 `readFilesystemMountTable` 九项 DOM 读数核对，不复制格式化或选择逻辑。中央要求两轮各自的新响应索引，拒绝以打开时旧响应充当刷新、错挂载／错读数、缺失收起及 URL／iframe 保持证明；允许新请求共享同一评估时刻，不强求即时手工刷新跨过采样步长。新增 `node-resources-list-filesystem.png` 列表截图随原证据归档并核验 PNG。

本地 `make test-node-metrics-online-runner`：152项确定性测试及 System Go Metrics 正式身份夹具通过，日志 `/tmp/addp-native-filesystem-list-runner-final.log`。`npm --prefix console/frontend test`：30文件202项单元测试通过，日志 `/tmp/addp-native-filesystem-list-console-unit.log`。在 `console/frontend` 执行标准 `npm run test:e2e -- node-resource-navigation.spec.js`：5项受控浏览器T3通过，日志 `/tmp/addp-native-filesystem-list-console-browser-final.log`；新增场景沿实际 Console／Monitor iframe，使用同一个 DOM 读数核对函数，刷新后将 inode 使用率20%改为30%、空闲量80改为70，核对值更新、完整身份、仅四次当前节点观测、收起、URL及原 iframe 文档保持。标准隔离浏览器入口退出0，未启动或接管个人服务。Online Playwright `--list` 发现唯一既有场景，导入解析通过，仅计测试发现。

本批只改验收实现及证据，既有 Online runner、Console frontend 单元与浏览器自动发现覆盖，无新依赖、入口或 workflow。上述T3为受控HTTP数据，不计真实Linux来源、正式密码／MFA、Safari或Hosted通过；未触发／轮询CI及Hosted、未设置定时任务。正式运行仍等专门会话安排与回传，原失败及前一批验证范围保留。


### 热力面板实源页面验收准备（2026-10-28）

既有 `platform-node-metrics` 资源 regression 在挂载详情阶段复用该页已读取的十项标量与五项容量响应，逐格核对 CPU／内存使用率、所有完整挂载的使用率、0～100进度条及原数据状态，记录 `resource_heatmap` DOM 收据和 `node-resources-heatmap.png`。容量响应索引须与同页挂载表一致，设备／挂载点／文件系统类型均精确匹配；合法零值保留有效进度，陈旧／缺样／未接入保持中性占位，不补零或继续着色。只复用原始百分比与共享格式化，不引入健康分、阈值或第二条采集路径；不增加查询、等待、身份用途、原有190条收据上限或时限。

Online 与受控 Console T3 共用 `readResourceHeatmap`，纯收据生成与拒绝反例覆盖并发顺序、错误主机／请求集合、同挂载点不同设备和类型、最新陈旧／缺样不得借用旧有效值。中央再核对两项标量、全部分区身份／状态、精确进度值、百分比文本及与挂载表的同响应绑定。

- `make test-node-metrics-online-runner`：153项确定性测试与 System Go Metrics 夹具通过，日志 `/tmp/addp-native-heatmap-runner-final.log`。
- `npm --prefix console/frontend test`：30文件207项通过，收据专项20项；日志 `/tmp/addp-native-heatmap-console-unit.log`。
- Console 标准 `npm run test:e2e -- node-resource-navigation.spec.js`：6项受控浏览器T3通过，日志 `/tmp/addp-native-heatmap-console-browser.log`。新增场景在实际Console／Monitor iframe 用同一函数检查0%／100%、同挂载点不同设备／类型，随后刷新为陈旧和缺样，分别核对中性状态、无进度条、占位文本及核对过程无额外请求。原列表刷新场景保留。隔离浏览器入口退出0，不接管个人服务。
- Online Playwright `--list` 发现唯一既有场景并完成导入解析，仅计发现，不计执行。

本批只改验收与既有证据，沿原 Online runner 和 Console frontend 的单元／浏览器自动发现；未改产品代码、Swagger或依赖，未重跑完整Monitor产品前端门禁。真实原生Linux、正式密码／MFA页面、Safari、Hosted及零残留仍待专门会话执行／回传；本地确定性与受控HTTP T3不计这些层级通过。未触发／轮询CI或Hosted、未设置定时任务、未修改个人配置或重启个人服务。


## 主机 CPU 空闲率辅助展示（2026-10-28）

原图硬件信息中的CPU空闲率补在详情原“CPU忙碌率（1分钟）”卡内，使用同一有效观测的 `100 - busy_percent` 并复用共享百分比格式化；非空闲率原本由各核idle计数的完整一分钟速率反推，因此不另加采集或新口径。预热缺样、陈旧和未接入显示“—”，合法忙碌0%／100%分别显示空闲100%／0%；中英文就近显示。不新增指标、独立卡片、列表列、趋势或规则，不改26项目录、五项列表摘要、API／权限／预算；前端只组合现有同源数据。

- `make test-monitor-frontend` 退出0：130项单元测试、490项浏览器场景及Vite生产构建通过（保留原chunk大小提示），日志 `/tmp/addp-cpu-idle-monitor-frontend.log`。新增中英文场景覆盖13.37%忙碌对应86.63%空闲、合法零／满量程、陈旧／缺样及恢复，确认每轮仍仅五组原观测、没有idle查询；原CPU预热场景增加空闲占位及恢复断言。
- 标准专项 `npm run test:e2e -- node-resources.spec.js --grep 'CPU idle companion'` 在Monitor frontend执行，两项通过；日志 `/tmp/addp-cpu-idle-focused-browser.log`。420px中英卡片截图已目视核对：`/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-cpu-idle-onOjxY/zh-cn.png`、`addp-cpu-idle-FSL23E/en.png`（同一父临时目录），不另计独立实源验收。
- Console 标准 `npm run test:e2e -- node-resource-navigation.spec.js` 六项受控iframe场景通过，核对主机空闲辅助值及陈旧／缺样不补零；日志 `/tmp/addp-cpu-idle-console-browser.log`。
- `make test-node-metrics-online-runner`：153项确定性测试及System Go Metrics夹具通过，日志 `/tmp/addp-cpu-idle-online-runner.log`；既有真实Console脚本增加同一有效CPU响应的空闲文本核对与强制展示收据，不增加请求或身份用途。

沿既有 Monitor／Console frontend 自动发现与 Online runner，未新增依赖、标准入口或CI路线。此次只改前端及验收，没有后端重启、IAM、个人配置或数据库操作；真实Linux、Safari及正式密码／MFA Hosted仍未据此通过，继续等专门会话回传。未触发／轮询CI或Hosted、未设置定时任务。磁盘分区容量继续按完整挂载展示，不把该补项外推为整机物理磁盘容量或整个硬件信息卡已完整验收。


## 块设备容量只读前置核对（2026-10-28）

本轮仅调查原图硬件容量的数据来源与设备层级，没有产品实现或正式测试。仓库锁定node_exporter v1.12.1的diskstats源码与现有固定目录只提供IO及设备信息，当前文件系统容量不能当物理总量。官方Textfile Collector支持节点绑定的扩展指标，但旧文件在每次抓取时仍会出现，因此正式实现还须独立源采样时间和失败／完整性保护，不能只按Prometheus抓取时间判断新鲜。

只读核对当前唯一归属本仓库的node-exporter容器，保持原User 65534:65534、非特权、只读根及无宿主根挂载，仅使用固定的/sys/class/block元数据与/proc/diskstats／内核版本；未读业务文件、序列号或凭据，未写容器文件、挂载或修改配置。Desktop来源与Docker daemon内核一致，43个可见块对象中24个size为0；诊断取样见vda=3,996,276,228,096B、vda1=2,199,022,206,976B、vdb=600,440,832B，三者设备号均匹配同一VM的diskstats，sysfs父关系确认vda1属于vda。另有RAM设备，故既不能将所有条目算物理盘，也不能将盘和分区直接求和。诊断取样中的名称过滤不是生产设备选择算法。

封闭元数据收据 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-block-capacity-probe-cij4zyyg/observations.json`（0600）仅保留配置布尔项、容器身份摘要与上述元数据，没有原始inspect／环境。结果标记read_only_feasibility；新采集器未安装、Prometheus查询及正式页面未验证，不能计T1／T2／T3／T4通过。首期提案见[运行监控后续事项](../../../next/ADDP平台运行监控与可观测性设计.md#磁盘与卷容量首期)，用户随后确认该范围，产品实现及分层验证见下节；此前置阶段不计产品门禁。文档只做差异／链接及事实核对，不重复旧产品门禁；未调度CI／Hosted、未设置定时任务、未重启个人服务或修改来源。


## 磁盘与卷容量首期实现与本地分层验证（2026-10-28）

用户已确认逐设备容量明细、默认关闭的部署侧只读采集及当次隔离 Hosted 只读用途。术语与正式规范先行，产品沿唯一节点入口、专属 Textfile、原 mTLS／Prometheus 和当前 User／主机读取裁决接入 `node.block.capacity_bytes`，device／kind 为封闭维度；固定主机目录总计27项。只提供当前设备容量，不提供物理总量、使用率、趋势或告警，不改 System 后台投影、IAM 或查询预算。

标准节点生命周期精确核验本工作区容器／专属卷归属；显式启用后启动非 root、只读根、无网络／特权／Socket 的采集器。原生 Linux 只读 sysfs 与四个白名单 proc 文件；Desktop 不新增宿主根、PID 或设备挂载，只读自身 VM 元数据。每十五秒原子替换专属文件，按驱动 major、partition 与映射属性筛选，排除 RAM／loop／分区／零容量。整帧100就绪设备、4096对象和精确整数上限，超限不截断。源时间、启动、设备号、采样变化、Textfile 解析及完整帧共同核验；缺文件与未启用区分，过期／失败／不可核验／成功空集合分别表达。容量证据只随请求该指标读取，原标量、列表摘要及告警不增加本项查询。关闭扩展／停止节点时清理其受管临时卷。

主机列表按设备展开及详情中的“查看设备容量”共用唯一 `HostBlockCapacity` 和原 `HostResourceDetailObservation` 请求／自动刷新内核，延迟读取、不汇总、IEC单位；就近说明映射卷与底层设备不可相加、Desktop只是VM视图。失败、撤权、身份变化和迟到响应不保留旧容量；原趋势入口不接受容量指标。API、Swagger、原 Metrics T2 的 block-capacity 分项、Owned service／输入登记和 Release 矩阵同批同步；不另建 CI 或调度路线。

本地工作区证据分别认定（最终提交号以本次交付与维护会话汇报为准）：

- Go T1：`make test-module MODULE=monitor GO_ONLY=1` 通过，含即时限制、维度／整组预算、采样状态、API query、当前 User 路径及容量查询不扩散至标量的回归。日志 `/tmp/addp-block-monitor-go-final.log`。
- 来源／查询 T2：`make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=block-capacity` 最终通过；115项标准脚本回归、锁定 Prometheus 的9项容量公式／旧文件／缺帧／移除／启动／未来时间／解析错误场景，以及真实非特权采集器→node_exporter→mTLS→Prometheus→Monitor 固定查询。逐设备数值与同一来源当前 sysfs 读取一致；本机环境为 Docker Desktop Linux VM，不能外推原生 Linux 部署层。结束输出全部当次容器、网络、卷及私有临时文件清零。日志 `/tmp/addp-block-metrics-t2.log`。
- 新增前端 T1：标准前端入口的131项确定性检查通过。独立页面 T3：`npm --prefix monitor/frontend run test:e2e -- e2e/block-capacity.spec.js` 最终3/3通过（8.5秒），核对中英文窄屏、延迟读取、2 TiB逐项而不求和、状态、失败清除、恢复、折叠和撤权；日志 `/tmp/addp-block-browser-final.log`。受控截图 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-block-capacity-zh-cn.png`、同目录 `addp-block-capacity-en.png` 已目视核对；不是实际主机容量或正式身份页面证据。
- 构建：`npm --prefix monitor/frontend run build` 通过（3.31秒），日志 `/tmp/addp-block-build.log`；原大 chunk 提示不计失败。
- Online runner：`make test-node-metrics-online-runner` 154项及 System Go 身份夹具通过；仅证明既有验收边界与默认关闭 Textfile 的原生节点证明适配，不证明容量扩展 Hosted 已准备或运行。日志 `/tmp/addp-block-online-runner.log`。
- 部署与登记：`make test-infra-runtime-log-lifecycle` 通过（含88项生命周期／预算、相关23／136项登记回归），日志 `/tmp/addp-block-infra-lifecycle.log`。`make test-platform` 首轮在两项旧清理参数断言失败，本批同步加入 block-capacity profile 后上述受影响子门禁通过；完整 Platform 未再重跑，不将该子门禁替代整批通过。
- Swagger：`bash scripts/swagger/gen-swagger.sh monitor` 与 `bash scripts/swagger/check-route-coverage.sh monitor` 通过，96个公开路由方法；日志 `/tmp/addp-block-swagger-gen-final.log`、`/tmp/addp-block-swagger-check-final.log`。

失败与限制保留：完整 `make test-monitor-frontend` 首轮485/493通过、8失败（26.6分钟），第二轮425/493通过、68失败（约3.7小时），均未通过且构建阶段未执行；独立构建另计。第二轮保留目录 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-monitor-playwright-results-jJp4jp`，日志 `/tmp/addp-block-monitor-frontend-final.log`。只读电源事件核对发现该时窗 Mac 反复睡眠／唤醒，不能作为连续长回归环境；也不能仅据此断言所有失败皆为环境原因。没有改电源设置、增加测试超时或删除失败案例；稳定环境的完整前端仍待核验。

容量 T2 曾在新分支变量初始化及退出 profile 清理失败，已修正并核验当次遗留容器／卷清零；后续两轮出现发现目标未切换到 node-exporter（来源已成功采样、原目标仍UP、证书IP匹配），尚未稳定复现根因。保留封闭文件／容器／HTTP／目标匹配诊断，未放宽TLS或增加等待预算；最终真实链路及清理通过只按当次结果认定，不宣称间歇性发现问题已经修复。标准脚本中受控生产停止测试另曾10秒超时，单项原条件复验及后续标准115项通过，未放宽时限或修改生产停止实现。

本批没有应用个人节点开关、重启服务、读取个人业务数据、触发／轮询 CI／Hosted 或设置定时任务。CI／Hosted等专门会话回传；原生 Linux 容量扩展的受管来源证明、真实 Console／Safari 及正式身份 Hosted 仍未验，容量对应的原生证明和页面收据准备也仍需补齐。不得以默认关闭容量的旧文件系统 Hosted 或本机 VM T2 外推这些范围。


## host-resources 受限来源挂载断言修复（2026-10-29）

专门CI会话回传 [Release/T2 40701610871 的 host-resources 实际步骤](https://github.com/pampa0629/addp/actions/runs/40701610871/job/122862998720) 失败于旧 `len(limited['Mounts']) == 4`（原始日志 `/tmp/addp-ci-20261028-160001/job-122862998720.log` 第833—836行）。原生资源查询、来源停机／恢复已走到，受限来源重建后在该检查退出；原CI没有归档完整inspect，不能只凭数量认定当时没有权限回归。

核对3f66f3521及当前两层Compose，真实 `docker compose config --format json` 渲染为四个只读文件绑定（固定Web配置、当次三份TLS文件），加一个只读 `/textfile` 容量专属卷，共五项；host-resources仍关闭Textfile且容量producer profile未启用。产品默认关闭容量时的四绑定部署没有改变。本次不改产品Compose、挂载、IAM或权限，也不以 `>=4` 代替旧断言。

唯一 `verify_limited_node_mounts` 对运行容器与当次volume inspect逐项核对：目标集合唯一且恰好五项，四个绑定源精确匹配当前仓库固定Web文件和当次TLS目录、只读且rprivate；Textfile卷名／Compose项目／service／owner／volume标签、local驱动及1 MiB tmpfs的uid／gid／mode均精确匹配，其实际Source必须等于同一volume inspect的Mountpoint并只读。拒绝额外、缺少、重复、可写、根目录／Socket、错源、影子目标或外来卷；不保存原始inspect、宿主路径、环境或凭据。仅输出封闭布尔及4绑定／1卷计数。来源初次准备后即核验，后续原受限来源重建位置仍再次核验，不跳过预算、查询、重启或恢复阶段。

受限阶段显式关闭Textfile，避免默认all在早先容量专项启用后把该采集器带入限定七采集器的视图；all先前容量验证及host-resources其他检查不删除。本批5项新增确定性回归包含真实Compose叠加和现有all／host-resources状态下的该关闭输入；27种错误对象／命令变体拒绝。新测试归入原 `metrics-deployment-config_test.py`，由现有根Metrics门禁、开发生命周期及原Release矩阵自动发现，无新增依赖、入口、CI Job或调度。

本地验证：新增五项 `python3 -m unittest scripts.test.metrics-deployment-config_test.LimitedNodeMountEvidenceTest` 通过，日志 `/tmp/addp-limited-mount-unit.log`。此前新测试在旧代码因缺少精确证明函数失败，真实叠加配置也证实旧四项断言必失败；不是删除安全条件后的空回归。

标准链路使用 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=host-resources`。首轮在更早的旧fixture“budget recovery”65秒等待超时，未到本次目标；当次清理执行，不能计整轮通过，日志 `/tmp/addp-limited-host-resources-t2.log`。未修改预算、TLS、采样标准或等待时限；随后新增首个来源准备检查点的同一精确证明，以实际inspect在前后两处覆盖挂载边界。最终结果待下述终态记录，不据局部通过外推完整门禁。


最终同一标准命令通过（退出0），日志 `/tmp/addp-limited-host-resources-final.log`：120项脚本回归；初次来源与受限重建后的两份精确挂载证明均为4绑定／1卷、只读／精确源／受管tmpfs／Textfile关闭；既有固定窗口、mTLS、预算恢复、普通／受限资源查询、来源停机恢复、中心SIGKILL持久化、六来源总览及资源告警生命周期全部沿原链路运行。资源查询两阶段分别56.77／29.39秒，总览85.75秒，资源告警生命周期195.62秒；退出验证全部当次容器、网络、卷与临时文件清零。此次仅本机Docker Desktop Linux VM上的标准T2；[原Linux CI失败](https://github.com/pampa0629/addp/actions/runs/40701610871/job/122862998720)保持失败历史，替代提交的自然Linux host-resources Job实际步骤仍等专门会话回传，不据本地结果提前认定远端通过。原生Linux容量／Console／Hosted与完整前端缺口保持原记录。本轮没有API、Swagger或产品代码变更，无需重启业务；不调度／重跑／取消CI或Hosted、不设置定时任务、不向个人服务或共享数据卷施加操作。


## 磁盘与卷容量原生来源证明与Console收据准备（2026-10-29）

承接已确认的容量只读用途，本批只完善既有隔离验收准备及本地回归，不修改产品 API、IAM、来源默认开关或个人部署。先同步正式观测规范与场景，再将原生来源证明升级为唯一 v2：必须是当次同内核原生 Linux exporter 与唯一受管容量采集器；精确核验双方非特权、容量采集器资源上限及无网络／Socket／设备、sysfs及四个proc白名单只读绑定、受管程序摘要、两端同一专属1 MiB tmpfs及写／读权限。只读命令核对当前sysfs快照和唯一原子容量帧的全部设备、结构、整数容量、启动及源时间，拒绝陈旧、外来前缀、缺帧、重复与不一致。封闭收据仅保留公共设备容量、摘要及边界布尔，不保存原始inspect／环境／帧正文。

regression／complete-comparison 在当次私有部署显式开启原容量扩展，Worker等其他profile保持关闭。来源故障入口改为精确核验后只停／启 node-exporter，不再按整个节点项目必须单容器作判断；容量采集器不成为故障目标，不扩大到个人来源。全部操作仍受原一次性Hosted边界保护。

同一真实Console脚本新增打开／手工刷新／收起容量的收据流程，由受控Console测试复用唯一DOM读数函数。逐行核对完整device／kind集合、IEC容量与状态，页面／API与上述原生来源证明一致；刷新必须是紧接的新响应，保持iframe并收起，不以旧数据或截图存在代替读数。不要求两次源采样时间递增，也不虚称刷新已取得下一轮采样。只增加两次既有容量GET，验收资源收据上限190改192，原身份、产品查询预算、并发、240秒页面／300秒子进程和总时限均不变。

本地验证分层记录：

- 标准 `make test-node-metrics-online-runner` 退出0：162项确定性测试及System Go Metrics身份夹具通过（42.585秒／0.777秒）；日志 `/tmp/addp-capacity-online-runner.log`。覆盖精确来源归属与挂载／卷／程序／帧拒绝、profile选择、Exporter限定故障对象、全设备页面收据、旧响应／错单位／缺行／重复／容量趋势拒绝，以及既有身份／生命周期回归。仅为确定性验收代码与身份夹具验证，不是原生Linux来源运行。
- `npm --prefix console/frontend test`：30文件219项通过，日志 `/tmp/addp-capacity-console-unit.log`；新增12项容量收据回归核对IEC、完整性、最后一次响应与无效状态拒绝。
- `npm --prefix console/frontend run test:e2e -- e2e/node-resource-navigation.spec.js`：7/7受控iframe场景通过（50.9秒），日志 `/tmp/addp-capacity-console-browser.log`。新增容量场景覆盖两设备逐项1 TiB、刷新后512 GiB、完整DOM读数、仅两个容量请求、收起、URL与同iframe不变；使用受控API，不是正式密码／MFA或真实主机证据。

上述脚本及测试由原根Online runner、Console frontend自动发现和已登记的标准CI路径覆盖，不新增入口、依赖、Workflow或调度。未重跑未变的Monitor产品门禁，原完整前端长回归失败仍保留，稳定环境复验未完成。原生Linux容量实源、正式身份Console／Safari、Hosted与该层零残留尚未实际执行／回传，不计通过；旧关闭Textfile的文件系统通过也不补作容量通过。未触发／轮询CI或Hosted、未设置定时任务、未修改个人开关或重启服务；当前仅可将“验收准备缺口”更新为已补，不能更新“真实运行验收”为完成。


## 容量关闭故障与恢复的Console状态验收（2026-10-29）

本批沿现有容量状态口径、唯一 `HostBlockCapacity`／请求内核和共享DOM读数函数，仅新增 Console 受控iframe验收；没有产品、API、权限、采集开关或Hosted用途变更。列表行“磁盘与卷容量”展开和主机详情“查看设备容量”分别走同一场景：有效两设备容量后，依次验证未启用、采集失败、样本过期、HTTP查询失败和不完整响应，每次都恢复为新设备名／新容量。

关闭／来源失败保持各自提示，查询失败与非法结构类型显示对应局部错误。每个异常阶段确认设备表无数据行、显示“暂无有效设备容量”，不残留旧设备、不补0 B；过期响应即使携带旧容量也不能显示。HTTP失败使用受控门闩暂缓响应，确认请求尚在进行时旧行已清除、按钮处于加载禁用状态，再返回503；门闩失败也在finally释放，不用固定睡眠或加大超时。五次恢复都通过上一批同一共享DOM函数逐行核对完整新device／kind、IEC容量、有效状态及新响应索引，同时确认错误提示清除。

两入口各恰好11次容量GET（首次一次、五组失败／恢复各两次），查询仅当前node_id及固定容量指标，不新增其他资源读取、写操作或轮询。CPU核数摘要保持原值；Console URL、同一iframe的timeOrigin及手工刷新选择保持。结束收起设备表，不把局部来源问题扩为主机页整体错误。

验证记录：标准隔离浏览器入口 `npm --prefix console/frontend run test:e2e -- e2e/node-resource-navigation.spec.js` 首轮9/9通过（1.1分钟），日志 `/tmp/addp-capacity-console-states-browser.log`；覆盖原7项及本次列表／详情两项。随后按最终文件运行根 `make test-console-frontend`，退出0：30文件219项单元测试、242项浏览器回归（7.0分钟）及Vite生产构建（1.93秒）全部通过，日志 `/tmp/addp-capacity-console-states-full.log`。源码仅改现有自动发现的Console浏览器文件；已有Console标准入口及CI路径覆盖，无新增依赖、Make入口或Workflow。未修改Monitor产品代码、共享读数函数或Online协议，不重跑未变的Go、来源T2及Online runner。

本批只证明受控HTTP响应下的真实Console／Monitor iframe交互；没有关闭或破坏个人采集器，不读取个人数据、不重启服务，不运行真实Linux故障或正式身份Hosted。原生来源／Hosted与Linux CI结果仍等专门会话回传，未触发／重跑／轮询CI或Hosted，未设置定时任务。旧完整Monitor前端长回归失败保持原记录，不由本批Console结果代替。


## 容量明细自动刷新与隐藏恢复的Console验收（2026-10-29）

本批仅在既有Console浏览器文件增加列表展开／主机详情各一个状态序列，复用正式 `createAutoRefresh`、`createLatestRequestScope`、唯一容量组件与上一批DOM读数函数；没有修改共享内核、产品组件、API、权限或采集配置。既有10秒间隔由URL选择，Playwright受控时钟推进，iframe内派发受控visibilitychange；不缩短产品间隔或用手工调用刷新替代自动触发。此受控可见性不计真实Safari或操作系统后台标签页证据。

各入口首次展开只读一次；自动计时后容量由1 TiB更新为512 GiB。隐藏后推进60秒，容量和其他Monitor资源读取均不增加；恢复可见不推进计时即重读256 GiB。后续自动请求在HTTP夹具处暂缓，保存当时8 TiB的独立旧响应；推进60秒仍无第二个容量请求，证明加载时不重叠。隐藏触发真实浏览器requestfailed，核对被取消对象就是该在途容量请求，再释放旧响应；隐藏期间继续推进60秒仍无新读取，8 TiB不能显示。恢复可见立即读取128 GiB，下一计时轮再更新64 GiB；新DOM与API索引核对沿唯一共享函数，不复用被取消的旧响应。

通过正式刷新控件选择off，Console URL与同一iframe保持既有规则：列表路由变化关闭临时展开，重新展开仍只读一次；详情沿同一页面重新读取。关闭后推进两轮60秒并切换隐藏／可见，不产生自动请求；手工刷新仍更新16 GiB，收起后再推进60秒无容量读取。每入口总共8次当前主机固定容量GET，其中1次被隐藏取消；正常刷新、恢复及手工阶段逐次核对新IEC容量／源时间与同iframe，查询只含原node_id与固定指标，不增加写入、权限或采集范围。

验证记录：`npm --prefix console/frontend run test:e2e -- e2e/node-resource-navigation.spec.js` 退出0，11/11通过（59.5秒），日志 `/tmp/addp-capacity-console-auto-browser.log`；包含原9项和新增两个入口的自动刷新／取消／恢复／off场景。最终文件的根 `make test-console-frontend` 退出2、未通过：218项单元测试通过／1项失败，失败在本批未改的 `tests/devPorts.test.js:243` Vite临时目录清理断言，目录中仍列出 `50446.pid`、`50475.pid`、`addp-console-vite-test-50446`、`addp-system-vite-test-50475`；日志 `/tmp/addp-capacity-console-auto-full.log`。后续完整浏览器和该入口的构建阶段未运行，不计通过；未修改清理实现、放宽断言或盲目重跑，未确认此次清理时序根因。另行执行既有 `npm --prefix console/frontend run build` 退出0（1.57秒），日志 `/tmp/addp-capacity-console-auto-build.log`；专项与独立构建不替代完整Console门禁通过。现有Console浏览器自动发现、根Make入口及CI路径已覆盖，无新增依赖或Workflow；未改未变的Monitor产品／Go／来源T2／Online协议，不重复其既有验证。

本批只证明受控HTTP和时钟下的真实Console／Monitor iframe交互，未操作个人采集器、真实来源或账号，不重启服务、不改变个人配置。CI／Hosted与真实Linux／正式身份验收继续等专门会话回传；未触发／重跑／轮询CI或Hosted、未设置定时任务。既有Monitor完整前端长回归失败保持原记录，不由Console结果代替。


## 容量迟到响应的Console范围隔离验收（2026-10-29）

本批只在既有Console浏览器文件补6个受控场景，沿唯一容量组件、请求取消／代数隔离与共享DOM读数；没有产品、API、权限或来源配置变更。请求识别函数在同一文件内复用，旧自动刷新场景同步走该函数，未保留重复识别路线。HTTP边界固定保存旧主机8 TiB／discarded-capacity的独立响应，正常新读取按当前主机分别返回32／64 GiB和current-a／current-b，避免旧夹具在释放时读取新变量而掩盖污染。

列表切换主机分别覆盖旧成功200与旧403：A的容量尚未返回时切换到B，核对requestfailed精确对应A的请求，B仅有一个展开与当前容量；B读数有效后才释放旧响应。释放后B的共享DOM收据索引、值和设备不变，旧拒绝不能触发新主机整体无权或局部错误；推进60秒仍只有A／B两次容量读取，Console地址与同一iframe保持。

列表／详情分别覆盖收起重开及离页返回。收起取消在途请求，重新展开从同主机的新请求取得容量，再释放旧403；不恢复旧设备、旧错误或旧观测。离页使用正式Console Logo返回首页，核对旧iframe卸载及原Frame detached，释放旧403并推进60秒不再读容量；浏览器Back回原URL创建新iframe，临时容量保持收起，重新展开才发第二次读取。恢复保留原分页与刷新偏好，不把旧页面内存或请求带入新Frame。各场景只有旧／新两次容量读取及一份有效响应收据，不写业务数据或修改真实授权。

本地验证：`npm --prefix console/frontend run test:e2e -- e2e/node-resource-navigation.spec.js` 退出0，17/17通过（1.4分钟），日志 `/tmp/addp-capacity-console-isolation-browser.log`；包含原11项及新增6项，覆盖提取后的请求识别函数和原刷新路径。`npm --prefix console/frontend run build` 退出0（4.05秒），日志 `/tmp/addp-capacity-console-isolation-build.log`。差异格式检查通过；现有标准浏览器入口、Console自动发现与CI路径覆盖，无新增入口、依赖或Workflow。

本批不盲目重跑上节已记录的完整Console门禁清理断言失败，不修改无关devPorts清理实现；该门禁本轮未运行，不能由专项／构建计通过。未改未变的Go、来源T2、共享内核或Online协议，不重复其既有验证。当前只计受控HTTP／时钟下真实Console和Monitor iframe的T3，不计真实Safari、Linux来源或正式密码/MFA Hosted；旧403是废弃请求的模拟响应，不是当前身份撤权验收。CI／Hosted继续等专门会话回传；未调度／重跑／轮询CI或Hosted、不设置定时任务、不修改个人配置或重启服务。


## 容量读取撤权与身份切换的Console验收（2026-10-29）

本批只补Console受控浏览器验收，不修改IAM、权限发布、账户、组件、API或授权共享内核。先读取可见有效容量，再把第二次自动容量请求固定为独立8 TiB旧帧；沿原HTTP夹具验证当前读取拒绝与AuthContext刷新两个不同边界。原暂缓响应夹具支持在首次／第二次请求暂停，旧17场景仍走同一夹具与产品路径，没有备用产品实现。

列表／详情各一项当前403场景：第二次容量读取返回permission_denied，清除容量及当前页资源观测，显示无权错误与停止自动刷新；推进60秒不再读取资源。显式刷新重新读取当前主机及资源后，容量仍须展开再读，有效值与局部错误恢复，URL、间隔偏好及同iframe保持。该403是受控服务端拒绝，不更改真实权限或数据库。

两入口各覆盖权限撤销、同权限下Principal变化、带相同权限字符串的Tenant上下文，共6项。通过正式Console store的强制Token刷新动作和受控AuthContext响应验证消费链，不直接赋值业务模块AuthContext；每次刷新返回不同的测试Token。权威结果尚未返回时，Console隐藏并保留原iframe，容量在途请求出现精确requestfailed、旧表消失；确认权限撤销或Tenant上下文后拒绝当前平台页面并卸载旧Frame，同权限的新Principal也创建新Frame。此后释放旧8 TiB帧，推进60秒不产生容量读取或恢复旧表。

被拒绝的上下文后续得到权威平台授权时，新建iframe而非复用已撤销实例；新Principal路径也只在显式展开后读取容量。核对Console当前Principal／Context及已有读取权限，新容量请求使用不同于旧读取的测试Token、属于新响应索引；各场景只有最初有效、被取消、重新展开三次容量请求和两份有效响应。分页、刷新偏好及原URL保持，不让Tenant进入平台主机页，不把宿主资源归为Tenant数据。

本地验证：`npm --prefix console/frontend run test:e2e -- e2e/node-resource-navigation.spec.js`退出0，25/25通过（1.4分钟），包含旧17项及新增8项，日志 `/tmp/addp-capacity-console-authority-browser.log`。`npm --prefix console/frontend run build`退出0（1.69秒），日志 `/tmp/addp-capacity-console-authority-build.log`；差异格式检查通过。原Console自动发现、标准浏览器入口及CI路径覆盖，无新增依赖、入口或Workflow。未改未变的Go、来源T2、共享内核或Online，不重复其既有验证；完整Console门禁本轮未重跑，上述开发端口清理断言失败仍未闭环，不计整批通过。

本批只计受控HTTP／时钟下的真实Console和Monitor iframe T3，不是System真实权限发布、上下文选择／切换接口、真实登录/MFA或跨租户数据隔离T4。个人账户、采集器、数据库和配置未改动，不重启服务；CI／Hosted继续等专门会话回传，未触发／重跑／轮询CI或Hosted、不设置定时任务。原生Linux容量／Safari／Hosted与Monitor长回归的既有缺口保留。


## 主机与引擎自动筛选统一（2026-10-29）

主机、服务、引擎列表统一复用Common Frontend的createAutoFilter：文本300毫秒防抖，清空和Enter立即提交，删除主机／引擎搜索按钮；主机类型及纳管状态原本即选即查，继续合并当前名称条件。相同URL不重复读取并恢复刷新；筛选回第一页，主机保留页大小／列视图，引擎保留刷新间隔。原System匹配、授权、批量摘要、查询预算与服务趋势边界均保持，无API或Swagger变更。

共享调度唯一负责等待／立即提交、待提交状态、取消与隐藏暂停／前台恢复；服务页原本地防抖生命周期删除，三页只保留领域查询、权限和路由组合。重置、清空、手动刷新、切换视图／页面及身份变化不被旧文本延迟覆盖；中文输入法继续沿原控件确认后提交。自动刷新只读已提交范围，待提交时暂停旧调度；不另建指标解释、错误或缺样提示路线。

本地实际验证：

- `make test-monitor-frontend`：131项单元测试、503个浏览器场景及构建全部通过，含主机／引擎新增6项和服务原46项；原时间、分页、趋势与撤权边界保留。
- `npm --prefix monitor/frontend run test:e2e -- e2e/automatic-filter-enter.spec.js`：三页独立3项通过（10.5秒）；当时只安装受控时钟、未显式暂停，不能单独证明Enter在防抖前提交。后续显式暂停后的失败与共享修复见[告警自动筛选证据](Monitor告警与规则历史证据.md#平台告警自动筛选与字段提示2026-10-29)。原文件在完整套件发现之后加入，单独执行，不把原聚合计数冒写为506项。
- `make test-console-frontend`：219项单元测试、263个浏览器场景及构建通过；新增主机／引擎中英文各两项，验证自动合并查询、清空／重置、System原参数、回第一页、URL replace与同iframe。
- `make test-common-frontend`：290项中289项通过、1项失败；新增三项自动筛选／可取消调度／唯一归属测试通过。唯一失败仍是上一批ResourceTree直接组合所有权清单未登记其他会话的System组件，本批不修改该组件或清单，不能将共享聚合计为通过。

新用例沿既有测试目录自动发现，Console原浏览器夹具已包含Monitor，模块Frontend标准入口与Platform矩阵继续覆盖，无需修改Make或Workflow。未运行未变的后端、数据库或Online runner；不重启个人服务、不操作个人身份或配置。上述仅为本地确定性T1／受控HTTP与时钟下的真实前端T3，不证明真实Safari登录、实际System筛选数据或Hosted。CI与Hosted仍等专门会话回传，未触发／重跑／轮询、不设置定时任务。

日志：`/tmp/addp-host-engine-filters-{common,monitor-full,console-full,enter}.log`。上一批及本批共享失败分别保留，不将后续局部通过回填为旧失败通过；清单完整功能的原正式验收缺口不因这次交互统一而消失。
## 容量来源 T2 的固定查询时点交接修复（2026-10-29）

专门 CI 会话回传 [Release block-capacity 失败](https://github.com/pampa0629/addp/actions/runs/40827356196/job/123293546488)。原始 `/tmp/addp-ci-20261029-063318/job-123293546488.log:817` 起记录固定公式 TestIntegrationMetricsBlockCapacity 通过，随后即时 up／success 探针通过，但 TestIntegrationMetricsBlockCapacitySource 得到 no_sample、Go 退出1；后续聚合失败由此矩阵项传递，不另计独立故障。原日志没有样本／查询时刻，不能据此认定本次发现的时间竞态已被证明为该远程失败的唯一根因。

已确认夹具存在两个时间口径：Python 就绪查询省略 time，而 Go 另取 floor(now) 整秒查询。首个抓取位于小数秒时，前者可见而后者不可见。新增固定版本 promtool 四个断言，证明 0.9 秒首次样本在 0 秒缺失，在 0.95／1／15 秒可见；同组包含共享15秒加载节拍，避免 LazyLoader 尚未加载输入而误判。首次新增用例误拆为不同组，真实运行两项返回 nil，失败日志 `/tmp/addp-block-snapshot-t2.log` 保留，未进入容量来源验证；按[该固定版本加载实现](https://github.com/prometheus/prometheus/blob/v3.13.4/cmd/promtool/unittest.go)改正组内加载范围后全部通过，没有放宽期望。

就绪路径仍沿原65秒有界轮询，改为对精确节点／instance 的成功 up 与容量 success 在同一整秒查询，并要求两者属于同次抓取；把已证明的整秒交给 Go，不再重新取时钟。Go 拒绝缺失、非整数、未来及超过原新鲜度期限的交接时间，产品 Collection／Query、身份标签、boot／整帧／设备全集／容量与新鲜度断言全部保留。新增日志只记查询及采样时刻、状态与设备数量，不输出目标地址、证书或凭据。仅改验收代码，不改产品 API、权限、采集实现、镜像或正式实例；没有增加 sleep、轮询期限或失败重试路线。

实际本地结果：`python3 -m unittest scripts/test/metrics-deployment-config_test.py` 最初122项中新增2项因尚无交接函数报错（`/tmp/addp-block-snapshot-red.log`），实现后122项通过（`/tmp/addp-block-snapshot-green.log`）；`make test-module MODULE=monitor GO_ONLY=1` 通过（`/tmp/addp-block-snapshot-go.log`）。最终 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=block-capacity` 退出0，含122项入口回归、原容量公式及新增四项时间边界、真实只读采集／Textfile／mTLS／Prometheus／Monitor查询和逐设备容量核对。查询时点1793261272（08:07:52Z）、同次抓取1793261271.757，Collection 为 collecting／available，两个当前可见设备容量完全匹配；日志 `/tmp/addp-block-snapshot-t2-final.log`。这仅为 Docker Desktop Linux VM 上的隔离来源 T2，不是原生 Linux 远程 CI、个人 Safari、Hosted 或整机物理容量总量通过。

两轮项目均沿标准 trap 清理；随后按两个精确 Compose 项目 label 独立核对容器／网络／卷全部为0，回执 `/tmp/addp-block-snapshot-cleanup.json`，成功轮也确认临时目录删除。标准 `python3 scripts/ci/check-t2-ci-registration.py` 通过（`/tmp/addp-block-snapshot-registration.log`）；既有 metrics 输入声明、配置测试自动发现及 Release block-capacity 矩阵已覆盖，无新入口或workflow。限定文件 diff 检查通过，未处理共享工作区其他会话的改动。后续自然 CI 仍等专门会话回传，不自行查询、调度、重跑CI／Hosted或设置定时任务；个人服务、主机挂载与数据卷均未操作。

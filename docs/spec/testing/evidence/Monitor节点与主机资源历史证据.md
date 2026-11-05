# Monitor 节点与主机资源历史证据

[返回 Monitor 历史证据](Monitor历史证据.md) · [返回验收证据索引](../验收证据索引.md)

本专题收录主机列表、资源趋势、设备与文件系统视图、探针预算。验收结论按各记录的固定源码、环境、运行尝试和层级认定。原顺序见[Monitor 历史证据入口](Monitor历史证据.md#原记录入口)。

原始过程、逐步诊断及详细归档清单见[精简前的固定记录](https://github.com/pampa0629/addp/blob/be8f8daa4d38a7830585577f03b77bf2b5ed3931/docs/spec/testing/evidence/Monitor%E8%8A%82%E7%82%B9%E4%B8%8E%E4%B8%BB%E6%9C%BA%E8%B5%84%E6%BA%90%E5%8E%86%E5%8F%B2%E8%AF%81%E6%8D%AE.md)。

逐次操作、原始日志摘要与收据清单见[本轮整理前的固定快照 `90d13c254`](https://github.com/pampa0629/addp/blob/90d13c2545c602eddcc17a81621a135ab64fa540/docs/spec/testing/evidence/Monitor%E8%8A%82%E7%82%B9%E4%B8%8E%E4%B8%BB%E6%9C%BA%E8%B5%84%E6%BA%90%E5%8E%86%E5%8F%B2%E8%AF%81%E6%8D%AE.md)。

[采集与挂载](#node-evidence-collection) · [容量状态](#node-evidence-capacity) · [前台刷新](#node-evidence-refresh) · [Safari 缩放](#node-evidence-safari)

<a id="node-evidence-collection"></a>

## 采集与挂载

固定 Linux／本机隔离来源的采样、挂载和清理，分别记录。受控 inspect、收据单元与浏览器仅证明验收准备或页面契约。

### 主机固定窗口探针的准备与执行期限

[6e55a4fad 的 host-resources / Job 119600712786](https://github.com/pampa0629/addp/actions/runs/39708924861/job/119600712786) 首败于准备和执行共用的 60 秒期限。约 54 秒后才进首项测试，前七项通过，在 FilesystemInodes 开始后被外层终止。修正保持同一八项，隔离编译限 180 秒、执行仍限 60 秒，拒绝旧二进制和超时重试。旧计时反例 3 失败／1 错误，随后旧选择器约束亦失败，修正后配置 48 项及登记通过。

本地冻结 `a190663c261a7e63ff00885d006138571c41610e` 叠加三项输入。编译独立完成，DiskIO 的 promtool 首先超过原 30 秒，随后 HTTP 阶段耗尽整体执行 60 秒，负载只是伴随观察。项目 `addp-metrics-t2-d2a7516da2b74f4ab03fb83404308598` 初次清理被异步 promtool 占用；首次补清理缺当次 TLS 参数未执行 Compose，补齐后按精确项目核验容器／网络／卷清零。原件 `/tmp/addp-host-window-*.log` 与 `/tmp/addp-host-window-budget-audit.json`。

固定 `9a2b3d28691741397f171b94866c125722bbe388` 的 [host-resources / Job 119660741437](https://github.com/pampa0629/addp/actions/runs/39727468958/job/119660741437) 自然首轮。独立编译约 46.67 秒，实际生命周期成功，八项固定窗口及后续查询、压力、总览、告警共 14 项 Go 运行无失败或跳过。mTLS、来源故障恢复、六来源、预算、同事件恢复和签名通知通过，日志确认容器／网络／卷／临时文件零残留；required Metrics 汇总 `119670125126` 的成功由专门会话回传。原件 `/tmp/addp-ci-20261022-063001/` 和 `/tmp/addp-host-window-linux-receipt-audit.json`。

本次关闭该固定源码的 Linux 探针计时缺口，前述本地失败与补清理独立保留。另行核对的 PG 来源 `6d42e4621` 三次准入与清理，以及 `c7ef8ea0e` 三分组各沿其原证据。原生 macOS、72 小时留存、完整 Hosted T4 或生产验收不由本段覆盖。

### 主机 TCP 当前连接数（本地）

2026-10-23，按用户确认口径新增固定主机标量 `node.tcp.current_connections`，单位 `connections`，只读当前宿主网络命名空间的 ESTABLISHED＋CLOSE_WAIT，不含监听／TIME_WAIT。复用唯一 `resourcequery` gauge、节点与来源身份、预算、新鲜度、即时和趋势 API；固定主机目录由 25 项增至 26 项，标量由九项增至十项。整数须在 0～2^53−1，合法零值保留，缺样不变成零；采集器缺失、重复原始序列及外来节点不能构造有效值。受限 Docker Desktop 来源不开放 netstat，不补容器命名空间统计，返回 `no_data`。页面新增就近口径／缺样提示、即时卡片及同一趋势、缩放、图例与悬停能力。CPU／内存摘要、TOP5、告警、生产权限及身份不变。

冻结产品基线 `3f0105d08bf2fa632f1888c5a9331c062cf89240`，仅叠加本批文件，证据目录 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-tcp-current-7nslrz9k`；`inputs.json` 记录本批文件，`product-inputs-sha256.json` 记录 2118 项产品、测试及共享依赖输入，提交前与共享工作区一致。登记检查另同步当时最新已提交编排输入，版本见 `registration-head.txt`；未混入其他会话未提交实现。实际验证：

- `make test-module MODULE=monitor GO_ONLY=1`：Monitor Go T1 通过（`go-t1.log`）。固定目录、节点／来源隔离、整数／安全上限、负值／非有限值、缺样／陈旧／未来样本均有确定性用例；默认跳过的集成用例不算 T2。
- `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=host-resources`：退出 0（`host-resources-t2.log`）。真实 node_exporter／Prometheus 的全来源即时及趋势、受限 Desktop 的 TCP 无数据分支通过；同一入口执行五个固定 promtool 场景：有效零、有效数量、重复来源、外来节点和缺失采集器。原有主机来源故障恢复及预算回归通过，退出核验当次容器／网络／卷和临时文件零残留。这是本地隔离 T2，不是原生 Linux 正式身份页面 T4，亦不作为服务实例 CPU／RSS 告警实源验收结果。
- Node v24.19.0 下 `make test-monitor-frontend`：111 项 T1、405 项浏览器 T3 及构建全部通过（`frontend-full.log`）。覆盖 TCP 中英文整数／零值、来源缺失后恢复、错误小数／负数／越界拒绝，及真实鼠标图例／悬停／缩放；其余主机、服务和错误／撤权场景一并回归。`screenshots/` 保留中英文 TCP 窄屏趋势实图，中文精确整数与单位已人工核对。页面测试使用受控 HTTP 夹具，不冒充真实来源页面。
- `bash scripts/swagger/gen-swagger.sh monitor` 与 `bash scripts/swagger/check-route-coverage.sh monitor`：最终生成成功，90 个公开路由方法覆盖一致（`swagger-generation-final.log`、`swagger-coverage.log`）；生成工具固定 module v1.16.6。首次未复制根 go.work 的生成失败仅为临时副本准备错误，保留在 `swagger-generation.log`，不计为通过证据。
- `make test-node-metrics-online-runner`：最终 139 项本地入口／身份／收据守卫及 System 既有 Metrics Go 夹具通过（`online-runner-final.log`）；`make test-frontend-ci-registration`：20 项检查、21 个前端登记通过（`registration-final.log`）。
- `npm test -- --testNamePattern='Online scalar resource receipt identity'`（Console frontend）：最终 8 项指定收据测试通过（`console-receipts-final.log`），包含精确十项且拒绝旧九项请求；其余 186 项因过滤未执行，不计为 Console 全量通过。

用户同日确认原一次性 `platform-node-metrics` 临时平台管理员限定用途增加当次主机 TCP 当前值、趋势和同一真实 Console 页面。规范与标准 runner 已同步精确十项标量；仅为多出的 TCP 选中与恢复两轮各六读，将原 174 项收据上限调整为 186，仍严格拒绝未授权指标、重复／缺失、错误节点或 query。总预算、密码／MFA、独立 Prometheus、节点范围、无租户业务及退出清零保持原约束。本会话没有触发或轮询 CI／Hosted，没有设置定时任务或重启个人服务；真实身份／来源 T4 及 CI 等专门会话回传，生产 T5 未运行。原表第 37 行（B36，主机实时资源总览列表）的 TCP 指标能力已落地，但主机列表仍只展示核心摘要，综合健康评分与完整总览列仍未实现；第 38 行（B37，资源趋势曲线图）新增 TCP 本地证据，不由本次结果推导留存、长期比较或其他真实运行范围通过。

### 主机资源热力面板（本地）

2026-10-23，主机详情新增 Monitor 领域组合组件 `HostResourceHeatmap.vue`，复用已授权的即时 CPU 忙碌率、内存使用率和每个完整设备／挂载点／文件系统身份的使用率；不新增采集、接口、查询、权限或持久状态。百分比沿原指标与共享格式化，不从容量重新推导；CPU 保留一分钟窗口，每项就近展示原状态与采样时间。有效使用率按 0～100% 连续混合现有主题绿／红色，基础进度条复用 Element Plus；合法零和 100% 有效，陈旧／缺样不着色、不补零。未采集分区和来源失败显示原原因，分区不可用不抹除可读 CPU／内存。完整挂载身份保留于色块及趋势导航，CPU／内存导航清除挂载维度。没有综合健康分、颜色告警阈值或新增资源规则。

冻结基线及本批文件见 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-host-heatmap-s0n4pjbs/final-inputs.json`；最终 436 项前端／共享／浏览器入口输入记录在 `final-frontend-inputs-sha256.json`。初始完整回归的产品输入相同，仅本次新增测试的刷新同步／截图检查和服务环比测试的请求同步随后修订，完整回归输入另记 `full-frontend-inputs-sha256.json`，不把两次输入混为一份。

- `make test-monitor-frontend`：完整批次执行 111 项 T1，通过 407 项浏览器用例、失败 2 项，命令退出 2，未执行其后的构建（`frontend-full.log`）。两项分别为新热力用例在刷新完成前挂起下一轮，以及既有服务环比用例在取消比较后、尚未取得新摘要与趋势请求前断言上一条请求。最后仅修正这两个测试文件，产品及共享输入未变；最终 `npm --prefix monitor/frontend run test:e2e -- host-resource-heatmap.spec.js process-comparison.spec.js` 的 19/19 项通过（`final-affected-browser.log`，4 项热力及 15 项服务比较）。其余 390 项未变用例复用上述完整批次有效结果，不将完整入口先前的非零结果写成最终全量重新通过，也不重复累计交叠用例。服务用例用受控挂起唯一 `process_resource_summaries` 响应，证明 UI 已取消比较时旧请求仍在；释放后必须新增且只新增一条正确五分钟趋势请求，再核对起点。受控探针初轮误指向不存在的 observations 端点，失败记录保留在 `final-affected-browser-before-endpoint-fix.log`，已改为 API 客户端实际使用的 summaries，未改产品请求或数据口径。
- `npm --prefix monitor/frontend test`：111 项 T1 通过（`unit.log`）。
- 最终标准 `npm --prefix monitor/frontend run test:e2e -- host-resource-heatmap.spec.js`：4/4 浏览器 T3 通过（`heatmap-final-verified.log`），覆盖中英文精确数值／原挂载比例、相同挂载点的不同设备／文件系统、窄屏、主题变量动态变化、合法零／满量程、无额外请求、当前指标／完整挂载导航、陈旧／缺样不着色、分区故障后恢复、未采集、权限拒绝及身份切换后迟到响应清除。初轮失败为测试对 URL 编码和现有缺源文案的错误预期、夹具改写未采集状态，以及在上一轮刷新完成前挂起下一轮所造成的测试自锁；最终使用解析后的 URL、原文案／数据状态并在挂起前等待既有刷新按钮可用，未增加产品重试或改变刷新逻辑。
- `npm --prefix monitor/frontend run build`：最终构建通过（`build-final.log`）；未修改后端/API/Swagger，无需重建后端或新增 T2。
- `make test-frontend-ci-registration`：20 项检查及 21 个前端登记通过（`registration.log`）。原 Monitor Playwright glob 和前端 CI 自动覆盖新增组件／用例，无新增标准入口或 workflow；未触发或轮询 CI。

`screenshots/` 保留最终中英文浅色／深色窄屏面板实图，已人工核对中文浅色布局与单位。T3 使用受控 HTTP 夹具与真实组件，未操作个人 Safari 或重启个人服务；未运行真实身份／来源页面 Hosted、T4 或生产 T5，CI 和 Hosted 等专门会话回传，未设置定时任务。原表第 39 行（B38，主机底层资源明细）的热力面板能力已实现；该行的完整硬件汇总／分区健康判定及新增页面真实运行证据仍需分别核对，不能由本批界面色彩推定健康判定或告警通过。

### 主机列表负载与连接视图（2026-10-26）

主机列表默认保留核心资源五项，新增“负载与连接”视图，仅请求 1／5／15 分钟系统平均负载及 TCP 当前连接数。复用既有指标公式、单位、新鲜度、当前用户逐主机授权及同一批量摘要；后端摘要白名单同步为这两组共九项标量的非空无重复子集，不新增接口或权限。每页默认 20 台，新视图请求 80 条序列，继续在 Owner 裁决和来源查询前校验原整批预算。TOP5／采集配置仍只请求原两项。

唯一 URL 参数为 `list_view=load-connections`，默认核心资源省略；切换保留已提交筛选、分页和刷新，不提交搜索草稿；详情返回、重载及重置筛选保留视图。列标题就近解释负载为任务数而非百分比、TCP 为 ESTABLISHED＋CLOSE_WAIT。有效零、缺样、失败及撤权不混淆，迟到响应不能覆盖新视图。

本地验证：

- Node `v24.11.1` 下 `make test-monitor-frontend` 退出 0：124 项单元测试、450 项浏览器场景（含新增六项）与 Vite 生产构建通过。中英 20 台主机验证单批精确四指标／80 条序列、响应乱序、TCP 有效零和独立缺样；另覆盖窄屏、视图切换、详情返回／重载、重置、超限／撤权、15 秒定时刷新（测试时钟）和身份切换后的迟到响应清除。既有构建大 chunk 提示不影响构建成功。日志 `/tmp/addp-host-list-final-gate.log`；浏览器结果目录见日志 `Browser gate monitor` 行，截图 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-host-load-6wzJX2/zh-cn.png` 与同轮完整门禁截图路径见日志。首轮六项专项有两项因脚本点击 Element Plus 透明 radio 输入框被标签遮挡而失败；改为点击可见标签后，最终完整门禁全部通过，原失败日志 `/tmp/addp-host-load-e2e.log` 保留。
- `make test-module MODULE=monitor GO_ONLY=1` 退出 0，覆盖两组摘要的预算、逐用户授权、停用排除、未知／新鲜度及批量身份隔离；新增四指标 25 台可规划、26 台在 Owner／来源读取前拒绝。日志 `/tmp/addp-host-load-go.log`。本地 Go T1 不替代真实来源 T2。
- `bash scripts/swagger/gen-swagger.sh monitor` 及 `bash scripts/swagger/check-route-coverage.sh monitor` 均退出 0，96 个公开路由方法一致；日志 `/tmp/addp-host-load-swagger.log`。

实源缺口：`make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=host-resources` 退出 2，在公共 mTLS 初次采样前置报 `Timed out: real mTLS scrape and controlled job label`，封闭诊断为 `error_kind=tls/health=down`。八项固定窗口 promtool 测试通过不等于整门禁通过；本批新增四指标真实摘要断言尚未执行。与本专题既有公共采样失败边界一致，不由现有诊断推断具体证书根因，不改 TLS／等待时长、不盲目重跑。日志 `/tmp/addp-host-load-t2.log`；项目 `addp-metrics-t2-786570fda5424602a680465383cd9092` 的容器／网络／卷在退出后分别按 Compose project 标签只读核验均为空，临时目录由标准入口清理。

个人 Safari 初次复验曾因运行中的旧 Monitor 后端仍按五项白名单拒绝新摘要而失败；2026-10-26 用户自行执行 `./scripts/dev/restart.sh -monitor` 后，沿原平台管理员会话只读复验，新视图能读取真实三档系统负载。初次为 8.21／9.07／10.83 个任务，点击页面“刷新”后为 8.32／8.84／10.58 个任务，采集状态为“正在采集”，不再出现摘要读取失败。详情导航保持同一主机 `755953df-488c-410a-88d4-0ecc47eb1351` 及 `list_view=load-connections`，返回列表和整页重新载入仍选中该视图并重新读取负载。

TCP 在列表及详情均为“—／缺失”，详情就近明确“当前来源未提供可用的宿主 TCP 统计，不代表连接数为 0”；仅验证受限 Docker Desktop 来源的缺样表达，不计真实 TCP 有效数值通过。AX 原始证据为 `/tmp/addp-host-load-safari-e8AXcN/load-refreshed.ax.txt`、`host-detail.ax.txt`、`load-reloaded.ax.txt`。截图能见真实负载，但按钮着色与 AX 选中状态存在滞后，右侧横向滚动返回 `noWindowsAvailable`，页面持续报告隐藏；故右侧 TCP 实际截图、前台自然自动刷新不计本轮通过，也不把个人单 VM／管理员会话结果外推为多主机、TCP 有效来源、此前失败的隔离 T2 或正式 Hosted。未操作个人数据库、权限或服务进程，未手工触发／轮询 CI 或 Hosted，未设置定时任务；正式结果仍等专门会话回传。

### 主机列表按设备展开（2026-10-26）

2026-10-26 用户确认先做主机行内按设备展开，不计算跨设备整机摘要。列表及详情的磁盘／网卡表格、单位、列旁解释、缺样状态、活动磁盘排序／无活动设备展开、精确设备趋势入口唯一归属 `monitor/frontend/src/components/HostDeviceResources.vue`，删除 `NodeResources.vue` 原内联设备表格和重复派生；列表读取组合由 `HostDeviceObservation.vue` 消费既有 `resource_observations`。最多打开当前授权页一台主机的一类明细，未展开无设备请求；磁盘固定五项、网络固定两项，沿原一分钟窗口、单次预算及授权，不新增接口、权限、指标或设备总量。

展开只属于临时页面状态；筛选、分页、列视图切换、详情导航、整页重载及身份变化关闭展开。刷新在主机仍处于当前授权页时保留选择、重读数据；切换／收起／离页取消在途请求，迟到结果不恢复旧内容。普通失败／超限清除设备值并保留仍授权的标量列表；设备 401／403／404 清空列表并停止自动读取。原始 node_version 必须与当前行版本相同；未知、未提供、陈旧与真实零分别显示。展开面板按列表可视宽度布局，窄屏刷新按钮可见，宽设备表保留自身横向滚动。

验证使用 Node `v24.11.1` 和原标准入口 `make test-monitor-frontend`：最终退出 0，125 项单元测试、459 项 Playwright 场景、Vite 生产构建全部通过；既有 chunk 大小提示不影响构建。新增九项浏览器场景覆盖中英和窄屏边界、无预取、单主机／单类别精确请求、默认箭头、折叠与有效零、类别切换的迟到旧响应、预算／版本错误及方向独立缺样、采集未提供、全局手动刷新、临时状态清除、精确设备趋势及列表回跳、测试时钟的15秒刷新／隐藏暂停、当前主机离页与撤权停止。旧详情页折叠、说明弹层、设备／网络趋势与交互也在同一完整门禁中通过；组件唯一所有权约束同步。没有重跑未变化的后端或此前公共 mTLS 前置，也未手工触发／轮询 CI 或 Hosted、设置定时任务或重启个人服务。

最终日志 `/tmp/addp-host-devices-final-gate-v2.log`，新增专项 `/tmp/addp-host-device-expansion-responsive.log` 九项通过。窄屏中英截图 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-host-device-list-te3HaS/zh-cn.png` 与 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-host-device-list-PZiaVK/en.png` 已目视检查。首轮 `/tmp/addp-host-devices-final-gate.log` 因目视发现展开区域继承外层宽表宽度、刷新按钮离开可视范围而主动中止，119 项通过／1 项中断／339 项未运行，未计为完整通过；修正宽度后从标准入口完整重跑。早期静态断言仍指向旧内联设备循环，已同步为唯一组件归属，不保留旧实现。

个人 Safari 已从现有管理员会话读取到“设备明细／磁盘 IO／网卡吞吐”入口；工具展开操作被 `ScreenCaptureKit -3811`／`noWindowsAvailable` 阻断。用户随后手动激活并明确反馈“已展开磁盘 IO”，但工具仍返回未展开且报告隐藏的页面。窗口菜单只列一个控制台窗口，重新选中同一窗口后，AX 与截图仍未显示展开；这是用户反馈与工具观测不一致，不直接判为产品失败，也不计为实机通过。只读证据 `/tmp/addp-host-device-safari-jUB6Y1/current.ax.txt`、`after-user-expanded.ax.txt`、`after-user-expanded.png`。真实单 VM 磁盘读数、真实网卡缺样、收起／刷新／面板像素和前台自然刷新均尚未独立验收；本地受控浏览器通过不替代个人实机或正式 Hosted，其他验证仍等专门会话回传。

### 主机列表文件系统容量与 inode 展开（2026-10-26）

沿已确认的按主机行内展开方案新增“文件系统”入口；最多展开当前授权页一台主机的一类资源。容量五项与 inode 四项沿既有顺序分别读取，按完整 `device／mountpoint／fstype` 对齐，保留各自查询／采样时间和错误，不改公式、不跨挂载汇总、不拆分超限组。列表和详情只使用 `HostFilesystemResources.vue` 这一份挂载表格；原内联实现删除，原设备读取组件收敛为 `HostResourceDetailObservation.vue`，旧文件及旧翻译命名删除。沿精确挂载进入既有趋势并保留列表上下文。

本批同时修复行内读取组合漏传共享 `createAutoRefresh` 必需的 `visibilityChanged` 回调：原实现在隐藏／恢复时抛错，切回前台未重读；现按原契约补齐调用，不修改共享工具。新增场景验证隐藏暂停、恢复后两组重读且无浏览器异常，以及收起第一组时不继续读 inode、切换时迟到第二组不回填、第二组拒绝后清空列表并停读。

Node `v24.11.1`：新增八项受控浏览器场景与原设备九项合计17项通过；进一步补充截图后的文件系统专项八项再次通过。覆盖中英／窄屏／横向滚动、同挂载点的设备或文件系统类型差异、乱序对齐、容量与 inode 独立预算／不可用错误、版本拒绝、合法零／缺样、未提供能力、精确趋势与返回、自动刷新及身份撤销。首轮三项失败为两项新测试漏写既有 inode 单位与上述回调问题，保留原失败日志，不计通过。最终标准入口 `make test-monitor-frontend` 退出0：127项单元测试、467项Playwright场景与Vite生产构建全部通过（保留原chunk大小提示）；前端源码及测试输入摘要在门禁前后核对一致，日志 `/tmp/addp-host-filesystem-final-gate.log`。

日志：`/tmp/addp-host-filesystem-targeted.log`（14通过／3失败）、`/tmp/addp-host-filesystem-targeted-v2.log`（17通过）、`/tmp/addp-host-filesystem-visual.log`（8通过）。截图目录 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-host-filesystem-list-NgAnEN`（中）与 `addp-host-filesystem-list-7grzkp`（英），已目视检查宽屏、窄屏及 inode 横向滚动。既有个人 Safari 工具观测限制未解除，且 Desktop VM 来源不提供 filesystem／inode，不能以受控数据证明个人环境真实分区读数。未运行新的后端／实源 T2、个人 Safari 或 Hosted 验收，未触发／轮询 CI、未设定时任务、未重启服务；当时 CI 与 Hosted 待专门会话回传；Linux 前端结果现已按下段独立复核，Hosted 仍待回传。

2026-10-27 独立复核专门 CI 会话回传的 [Linux Monitor frontend Job](https://github.com/pampa0629/addp/actions/runs/40351996188/job/121674238730)：push／attempt 1，实际 checkout `e5391ca36843cd0d99233ea3f63949abb45d81d0`。GitHub Job 与实际 `Run Monitor frontend gate` 步骤均为 success，`make test-monitor-frontend` 原始日志确认 tests/pass 各127、fail 0、467 browser passed（23.3分钟），生产构建10.55秒完成。产品 `12d3eba1f6d9294ca16c2bfe9204e046e2d872e6` 至该实际提交的 `monitor/frontend`、`common-frontend`、根 Makefile、标准前端／浏览器入口、prepare-frontend-gate 与 Platform workflow 均无差异，因此覆盖本批实现。

通过 `gh api repos/pampa0629/addp/actions/jobs/121674238730` 及对应 run 的只读请求核对终态，再独立下载原始日志至 `/tmp/addp-monitor-e539-independent-job.log`；与回传归档逐字节相同，SHA256 为 `2665d7996277041d5b79bf80c985e6f223a4b944f4b0b69130f1d170df985511`。回传审计 `/tmp/addp-ci-20261026-180018/Monitor_e539_actual_frontend_audit.json`。父 Platform run 的终态为 cancelled，不计整套 workflow 通过；旧超时保留，不能据此认定其根因已修。后继作业若实际跳过也不计第二轮复验。这一证据仅证明固定源码的 Linux 标准前端门禁，不替代 Safari、个人实源、正式身份 Hosted 或生产验收，不扩展文件系统告警范围；本轮未重跑或调度任何验收。

### 真实 Linux 采集与挂载页面验收准备（2026-10-28）

既有 `platform-node-metrics` regression／complete-comparison 在浏览器前核验当次原生 Linux exporter 的项目／服务／仓库、同内核、host 网络／PID、非特权只读、只读 rslave 宿主根与 proc／sys、显式 filesystem collector。拒绝 Desktop、容器根、挂载覆盖、重复／反向参数及 textfile 替代。前置失败清除旧成功收据并阻止浏览器；`node-native-source.json` 只保留封闭证明。

同页 5 容量＋4 inode 响应按完整 device／mountpoint／fstype 绑定可见列、单位和趋势，`filesystem_mount` 记录响应／行索引及 DOM 文本。中央拒绝旧收据、错主机／范围／类型／单位，并验证乱序与合法零。

本地 Online runner 151 项及身份 Go、Console 202 单元（含 15 项收据）通过；前一轮 201／202 在未改 devPorts 的 Vite 清理断言失败，记录 `7102.pid`／`addp-system-vite-test-7102`，复跑通过但根因未定。Playwright --list 仅发现一个场景。原件 `/tmp/addp-native-{node-runner-final,mount-console-unit-final}.log`。真实 Linux 挂载、正式登录页面及 Hosted 清理尚未执行。

#### 列表展开、手工刷新与收起的验收准备（2026-10-28）

同一场景新增列表展开、关闭自动后的手工刷新及收起。唯一九项 DOM 读数函数要求两轮各自新响应、URL 与 iframe 保持，允许共享评估时点。收据上限 186→190，保留列表 PNG，查询及场景期限不变。

本地 runner 152 项及身份 Go、Console 单元 202、受控 iframe 5 项通过。受控刷新 inode 20%→30%、空闲量 80→70，验证仅四次当前节点读取及收起；Online --list 仅发现。原件 `/tmp/addp-native-filesystem-list-*.log`。真实 Linux、Safari、正式密码／MFA 与 Hosted 未执行。

#### 热力面板实源页面验收准备（2026-10-28）

同一挂载阶段复用十项标量和五项容量响应，`resource_heatmap`／截图记录 CPU、内存和完整挂载，中央要求与挂载表共享容量响应及精确身份。受控值验证 0%／100%、同挂载点不同设备／类型，陈旧与缺样保持中性、无进度且无额外请求，190 条收据上限不变。

本地 runner 153 项及身份 Go、Console 单元 207（含 20 收据）、受控 iframe 6 项通过；隔离入口退出 0，Online --list 仅发现。原件 `/tmp/addp-native-heatmap-*.log`。原生 Linux、正式身份页面、Safari 与 Hosted 零残留未覆盖。

### 主机 CPU 空闲率辅助展示（2026-10-28）

原图硬件信息中的CPU空闲率补在详情原“CPU忙碌率（1分钟）”卡内，使用同一有效观测的 `100 - busy_percent` 并复用共享百分比格式化；非空闲率原本由各核idle计数的完整一分钟速率反推，因此不另加采集或新口径。预热缺样、陈旧和未接入显示“—”，合法忙碌0%／100%分别显示空闲100%／0%；中英文就近显示。不新增指标、独立卡片、列表列、趋势或规则，不改26项目录、五项列表摘要、API／权限／预算；前端只组合现有同源数据。

- `make test-monitor-frontend` 退出0：130项单元测试、490项浏览器场景及Vite生产构建通过（保留原chunk大小提示），日志 `/tmp/addp-cpu-idle-monitor-frontend.log`。新增中英文场景覆盖13.37%忙碌对应86.63%空闲、合法零／满量程、陈旧／缺样及恢复，确认每轮仍仅五组原观测、没有idle查询；原CPU预热场景增加空闲占位及恢复断言。
- 标准专项 `npm run test:e2e -- node-resources.spec.js --grep 'CPU idle companion'` 在Monitor frontend执行，两项通过；日志 `/tmp/addp-cpu-idle-focused-browser.log`。420px中英卡片截图已目视核对：`/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-cpu-idle-onOjxY/zh-cn.png`、`addp-cpu-idle-FSL23E/en.png`（同一父临时目录），不另计独立实源验收。
- Console 标准 `npm run test:e2e -- node-resource-navigation.spec.js` 六项受控iframe场景通过，核对主机空闲辅助值及陈旧／缺样不补零；日志 `/tmp/addp-cpu-idle-console-browser.log`。
- `make test-node-metrics-online-runner`：153项确定性测试及System Go Metrics夹具通过，日志 `/tmp/addp-cpu-idle-online-runner.log`；既有真实Console脚本增加同一有效CPU响应的空闲文本核对与强制展示收据，不增加请求或身份用途。

沿既有 Monitor／Console frontend 自动发现与 Online runner，未新增依赖、标准入口或CI路线。此次只改前端及验收，没有后端重启、IAM、个人配置或数据库操作；真实Linux、Safari及正式密码／MFA Hosted仍未据此通过，继续等专门会话回传。未触发／轮询CI或Hosted、未设置定时任务。磁盘分区容量继续按完整挂载展示，不把该补项外推为整机物理磁盘容量或整个硬件信息卡已完整验收。

### host-resources 受限来源挂载断言修复（2026-10-29）

[Release/T2 40701610871 的 host-resources 实际步骤](https://github.com/pampa0629/addp/actions/runs/40701610871/job/122862998720) 首败在受限来源重建后的 `len(limited['Mounts']) == 4`，原生查询与停机／恢复已执行，原 CI 无完整 inspect。固定 `3f66f3521` 对照两层 Compose 为四只读绑定＋一只读 `/textfile` 专属卷，受限视图仍关闭 Textfile；产品默认关闭容量的四绑定不变。

唯一 `verify_limited_node_mounts` 在准备及受限重建两处核对精确源、只读／rprivate、目标唯一、项目／服务／owner 标签、local 1 MiB tmpfs 与同卷 Mountpoint；拒绝多余／可写／宿主根／Socket／外来卷。新增五项 T1 与 27 变体，旧实现红例失败。

本机 Desktop VM 首轮先在 budget recovery 的原 65 秒等待失败，清理已执行但未到本次断言。最终 host-resources 整轮通过，120 脚本检查、两份精确挂载、原查询／恢复／SIGKILL／总览及告警均执行，退出容器、网络、卷及临时文件清零。原件 `/tmp/addp-limited-host-resources-*.log` 与 `/tmp/addp-ci-20261028-160001/job-122862998720.log`。原 Linux Job 仍失败，替代固定 Job、原生 Linux 容量、正式 Console 与 Hosted 单列。

<a id="node-evidence-capacity"></a>

### host-resources 压力探针的编译与执行预算（2026-11-06）

固定 `3de409013` 的 [Linux host-resources Job](https://github.com/pampa0629/addp/actions/runs/42051463127/job/127363354578) 在压力探针的 `go test` 整体 90 秒到期。日志出现依赖下载，未记录该用例启动，无法从旧日志细分下载与编译耗时。早先 `62f5e9a51` 的同分项通过及同版同行 `http-quality` 通过各自保留。原件 `/tmp/addp-ci-20261106-020018/job-127363354578.log`。

在当次隔离目录以 180 秒上限准备一个测试二进制，分别复用于在线和停源断言。执行仍各限 90 秒／30 秒，原业务断言未改。模拟 100 秒冷编译的回归先失败、修正后通过，既有 Release 分项登记命中。

基于 `93f97aabf` 的隔离源码仅叠加本专项，运行 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=host-resources`。139 项配置通过，真实在线及停源压力用例各实际执行并通过（均 0.01 秒）。整轮随后在受限 VM exporter 的 mTLS 抓取超时失败，日志记录 `tls: bad record MAC`，其根因未确认。容器、网络、卷按精确项目标签核对均为零。本机日志 `/tmp/addp-host-pressure-20261106-t2.log`。这轮只补压力阶段证据，完整 Linux 专项待新源码自然 CI 回传。

## 容量状态

逐设备容量的只读取证、实源 T2 和 Console 受控状态分开查阅。关闭、过期、故障、迟到响应及当前撤权各有独立判定。

### 块设备容量只读前置核对（2026-10-28）

本轮仅调查原图硬件容量的数据来源与设备层级，没有产品实现或正式测试。仓库锁定node_exporter v1.12.1的diskstats源码与现有固定目录只提供IO及设备信息，当前文件系统容量不能当物理总量。官方Textfile Collector支持节点绑定的扩展指标，但旧文件在每次抓取时仍会出现，因此正式实现还须独立源采样时间和失败／完整性保护，不能只按Prometheus抓取时间判断新鲜。

只读核对当前唯一归属本仓库的node-exporter容器，保持原User 65534:65534、非特权、只读根及无宿主根挂载，仅使用固定的/sys/class/block元数据与/proc/diskstats／内核版本；未读业务文件、序列号或凭据，未写容器文件、挂载或修改配置。Desktop来源与Docker daemon内核一致，43个可见块对象中24个size为0；诊断取样见vda=3,996,276,228,096B、vda1=2,199,022,206,976B、vdb=600,440,832B，三者设备号均匹配同一VM的diskstats，sysfs父关系确认vda1属于vda。另有RAM设备，故既不能将所有条目算物理盘，也不能将盘和分区直接求和。诊断取样中的名称过滤不是生产设备选择算法。

封闭元数据收据 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-block-capacity-probe-cij4zyyg/observations.json`（0600）仅保留配置布尔项、容器身份摘要与上述元数据，没有原始inspect／环境。结果标记read_only_feasibility；新采集器未安装、Prometheus查询及正式页面未验证，不能计T1／T2／T3／T4通过。首期提案见[运行监控后续事项](../../../next/ADDP平台运行监控与可观测性设计.md#磁盘与卷容量首期)，用户随后确认该范围，产品实现及分层验证见下节；此前置阶段不计产品门禁。文档只做差异／链接及事实核对，不重复旧产品门禁；未调度CI／Hosted、未设置定时任务、未重启个人服务或修改来源。

### 磁盘与卷容量首期实现与本地分层验证（2026-10-28）

首期为默认关闭的部署侧只读 `node.block.capacity_bytes`，逐 device／kind 展示，27 项固定目录。不计算整机总量、使用率、趋势或告警。原生 Linux 只读 sysfs／四个 proc 白名单，Desktop 仅 VM 元数据；非 root、只读根、无网络、特权及 Socket。15 秒原子帧，100 设备／4096 对象及精确整数上限，按源时间、启动及帧完整性校验。列表／详情复用延迟查询与 IEC 单位，关闭／停止清理专属临时卷。

| 层级 | 实际结果 |
| --- | --- |
| 本机实源 T2 | 标准 block-capacity 最终通过，115 项入口、9 个固定公式及真实采集器→Textfile／node_exporter→mTLS→Prometheus→Monitor。逐设备与同来源 sysfs 匹配，容器／网络／卷／临时文件清零。环境为 Docker Desktop Linux VM。 |
| 确定性及页面 | Monitor Go、前端 131、容量受控浏览器 3／3、独立构建、Online runner 154＋身份 Go、生命周期及 Swagger 96 路由通过。仅证明受控页面与 runner 准备。 |
| 整体门禁 | Platform 首轮两项旧清理参数断言失败，加入 profile 后相关子项通过，整批未重跑。完整 Monitor 前端先 485／493，后 425／493，均失败且未到构建；后轮 Mac 多次睡眠／唤醒，具体失败根因未全部确认。 |

T2 前序首败包括新分支变量未初始化和退出未含 profile，修正后核验遗留容器／卷清零。后续两轮来源已采样而发现目标未切换，根因未定；旧受控生产停止夹具还曾超时 10 秒，单项及最终入口通过。原件 `/tmp/addp-block-*.log`；浏览器失败目录 `addp-monitor-playwright-results-jJp4jp`。个人开关、原生 Linux 容量来源证明、真实 Console／Safari 及正式 Hosted 当时未验。

### 磁盘与卷容量原生来源证明与Console收据准备（2026-10-29）

原生来源证明 v2 要求同内核 exporter 与唯一受管容量采集器；核验非特权、限额、无网络／Socket／设备、白名单 sysfs／proc、受管程序摘要及同一专属 1 MiB 卷的写／读权限。当前 sysfs 与唯一完整帧对照，拒绝陈旧、外来、缺帧、重复或不一致。场景只在当次启用容量扩展，故障操作只停／启 exporter。

Console 收据核对打开、手工刷新、收起时全部 device／kind、IEC、状态和新响应，并与原生证明一致。两次既有容量 GET，收据上限 190→192；不要求刷新跨下一次源采样，身份及预算不变。

本地 runner 162＋身份 Go、Console 单元 219（含容量收据 12）、受控 iframe 7 项通过。受控两设备 1 TiB→512 GiB，验证精确读数及 URL／iframe 保持。原件 `/tmp/addp-capacity-{online-runner,console-unit,console-browser}.log`。这是验收准备，正式 Linux 来源、真实身份 Console、Safari、Hosted 与零残留尚无本批运行证据，原完整前端长回归失败保留。

### 容量来源 T2 的固定查询时点交接修复（2026-10-29）

[Release block-capacity 失败](https://github.com/pampa0629/addp/actions/runs/40827356196/job/123293546488) 的固定公式与即时 up／success 先通过，首败为 `TestIntegrationMetricsBlockCapacitySource` 返回 no_sample、Go 退出 1。原日志缺查询／抓取时刻，无法确认后来发现的竞态是该次唯一原因。原件 `/tmp/addp-ci-20261029-063318/job-123293546488.log`。

夹具原由 Python 查询当前时刻、Go 重取整秒，首个小数秒样本可能只在前者可见。沿原 65 秒在同一整秒确认同次抓取的 up 和容量 success，再把时点交 Go；缺失、非整数、未来或陈旧拒绝。四项 promtool 断言先因分组错误两项 nil，按[该固定版本加载实现](https://github.com/prometheus/prometheus/blob/v3.13.4/cmd/promtool/unittest.go)改正加载范围后通过；原时间、权限和完整帧断言保持。

本地配置红阶段两项缺函数，修正后 122 项及 Go 通过。Desktop VM 标准 block-capacity 实源通过，查询 1793261272、同次抓取 1793261271.757、两设备匹配；两项目按标签核对容器／网络／卷清零，通过轮私有目录删除。原件 `/tmp/addp-block-snapshot-*.log` 和清理收据 `/tmp/addp-block-snapshot-cleanup.json`。

固定 `efc577aa69fc0a190dddba2bc9bff4903fc99f88` 的 [Release run40841858063／block-capacity Job123340144076](https://github.com/pampa0629/addp/actions/runs/40841858063/job/123340144076) 自然首轮 completed／success，实际生命周期 selected=true；公式及真实容量查询执行、无跳过，查询 08:31:31Z、抓取 1793262690.69，collecting／available、一设备核验，容器／网络／卷／临时文件零残留。原件与独立审计 `/tmp/addp-ci-20261029-100020/`。仅关闭该固定 Linux T2 缺口，未覆盖 Safari／UI、Hosted、整 Release 或物理容量总量。

### 容量关闭故障与恢复的Console状态验收（2026-10-29）

受控 Console 列表展开／详情共用同一场景。初始有效后分别返回未启用、采集失败、过期、HTTP 503 和不完整响应；各次清除旧行、保留明确局部状态、不补 0 B，再以新 device／kind、IEC 和响应索引验证恢复。暂缓 HTTP 时核对加载禁用及旧行清除。两入口各 11 次当前节点容量 GET，保持摘要、URL、刷新偏好和同 iframe。

定向 9／9 与完整 Console 219 单元／242 浏览器／构建通过，原件 `/tmp/addp-capacity-console-states-*.log`。该结果覆盖受控状态与恢复，个人采集故障、原生 Linux、正式身份 Hosted 及旧 Monitor 长回归各自认定。

### 容量明细自动刷新与隐藏恢复的Console验收（2026-10-29）

受控时钟按 URL 的 10 秒间隔验证两入口。首次及自动刷新有新值；隐藏 60 秒无读取、可见立即读取。暂缓的 8 TiB 旧请求无重叠，隐藏精确触发该请求的 requestfailed，释放旧帧无污染；恢复后立即及下一计时轮各有新值。off 后计时／可见切换无自动请求，手工刷新仍可用，收起停止读取。列表路由变化时的临时展开行为仅属当时版本，后继同上下文的展开保持断言见[主机详情就近提示](#主机详情就近提示2026-10-31)。每入口 8 次 GET，含 1 次取消。

定向 11／11、独立构建通过。完整 Console 在 `devPorts.test.js:243` 先失败（218 通过／1 失败），观察到 50446／50475 的 Vite PID 文件和临时目录仍在，根因未定；后续完整浏览器与该次构建未执行。原件 `/tmp/addp-capacity-console-auto-*.log`。受控 visibilitychange 及计时不覆盖 Safari 后台或真实采集。

### 容量迟到响应的Console范围隔离验收（2026-10-29）

六项受控场景覆盖列表换主机时的旧 200／403，以及列表／详情的收起重开和离页返回。旧 8 TiB 帧在边界固定，精确确认原请求取消；新 32／64 GiB 与响应索引先落地，再释放旧帧，当前读数／授权不受影响。离页确认旧 Frame detached、60 秒无读取，Back 恢复 URL／分页／偏好并创建新 iframe，展开才重读；每场景两次容量请求、一份有效新收据。

定向 17／17 和独立构建通过，原件 `/tmp/addp-capacity-console-isolation-*.log`。原 Console 聚合清理失败未复跑；旧 403 为废弃请求，当前撤权另见下一节。真实来源、Safari 与正式身份未覆盖。

### 容量读取撤权与身份切换的Console验收（2026-10-29）

列表／详情各验当前 403。第二次读取拒绝后清除页面读数、提示无权并停刷；推进 60 秒无资源请求，显式刷新并重新展开后才恢复。六项另外覆盖撤权、同权限新 Principal 与带相同权限字符串的 Tenant，走正式 store 的强制 Token 刷新与受控 AuthContext。重核期间隐藏同 iframe、取消精确在途请求并清表；拒绝或新身份卸载旧 Frame，释放旧 8 TiB 不恢复读数。平台授权后新 iframe 用新 Token，三次请求和两份有效收据，保持 URL／分页／偏好。

定向 25／25、独立构建通过，原件 `/tmp/addp-capacity-console-authority-*.log`。原 Console 聚合清理失败仍未闭环。这里是受控 T3，未验证 System 真实权限发布、真实登录／MFA、跨租户 T4、原生 Linux 容量或 Safari。

<a id="node-evidence-refresh"></a>

## 前台刷新

受控 T3 验证请求、同一 DOM 和失效清理。Safari 记录真实前台可见与自然时间推进，按当次实际观察认定。

### 主机列表五项核心摘要（本地）

2026-10-23，按用户确认范围，主机列表展示 CPU 逻辑核数／一分钟忙碌率、内存总量／使用率与运行时长，默认每页 20 台。复用原单位、当前节点身份／版本、采样新鲜度、采集状态和列表生命周期；容量用 IEC 单位，运行时长用复合时长。固定主机与操作列、标题允许换行，每项保留独立状态。当前页只进行一次摘要 API 请求，不逐台读取详情或补齐失败列；返回乱序仍按节点身份映射，单项陈旧不抹除其他有效值，整批缺项拒绝并清空旧摘要，主机列表仍保留。

`/platform/resource_summaries` 唯一契约现在显式携带 `node_ids` 和 `metrics`，只接受上述五项的非空无重复子集，不保留省略参数的旧请求。唯一批量内核复用原公式、两次固定 Prometheus 请求、整批期限／并发和逐节点 User 授权；在 Owner 读取与指标查询前按节点数乘指标数预检预算。默认 20 × 5 = 100，21 台五项请求整批拒绝，不拆批或提高预算；TOP5 与采集配置页显式保持 CPU／内存百分比两项，原 50 台 TOP5 上限、比较和热力指标范围不变。API、调用方、Swagger 及既有 Hosted 收据／拒绝测试同步，仍只在原已确认十项节点指标的读取范围内选五项，不新增权限、角色或身份用途。

冻结基线 `d798875a0732cb18e42d1772799d7c91b007542e`，仅叠加本批文件。证据目录 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-host-list-core-v8j8u6ci`，`inputs.json` 记录最终本批文件，`frontend-inputs-sha256.json` 记录 437 项前端／共享／浏览器入口输入。其他会话的 engineApprovalInitialization 两项共享未提交 WIP 未混入冻结副本；本任务源码及其余前端输入与工作区核对一致，不外推整个共享工作区的 WIP。实际验证：

- `make test-module MODULE=monitor GO_ONLY=1`：Monitor Go T1 通过（`go-t1.log`）。五项／两项显式投影共用内核；20 台五项、50 台两项边界通过，21 台五项在任何 Owner／来源读取前拒绝；空／重复／未知／分组／不在摘要范围的指标拒绝，停用、跨节点、当前用户 Token 与完成时新鲜度沿原规则核验。
- `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=host-resources`：退出 0（`host-resources-t2.log`）。真实 node_exporter／Prometheus 上五项摘要的完整来源及受限 Desktop VM 读取、外来节点缺样隔离通过；原 TOP5 的受控多源覆盖／预算及来源故障恢复回归通过。退出核验当次容器／网络／卷和临时文件零残留；该结果是本地隔离 T2，不是多真实主机、正式身份页面 T4 或生产 T5。
- Node v24.19.0 下，`npm --prefix monitor/frontend run test:e2e -- host-list-core.spec.js`：20 台主机的 2 项中英文专项通过（`core-list-browser.log`），验证一次精确五项批量读取、默认每页20台、CPU 核数／百分比、IEC 容量、复合时长、乱序身份匹配、单项陈旧、整批缺项后的清除／恢复及无逐行详情查询。后续完整门禁另覆盖最终表头换行。
- 最终 `make test-monitor-frontend`：112 项 T1、411/411 浏览器 T3 及构建全部通过，命令退出0（`frontend-full.log`，浏览器约15分钟，构建4.46秒）；本次输入下无失败或跳过。覆盖主机／采集列表的投影隔离、TOP5、详情、热力、趋势与服务／告警回归；不把前面的专项与整批交叠用例重复累计。`screenshots/` 保留最终五项列表中英文实图，已人工核对中文布局、单位与可读表头。
- 首轮 `npm --prefix monitor/frontend run test:e2e -- node-resources.spec.js monitoring-target-list.spec.js resource-overview.spec.js`：89 通过、3 失败（`focused-browser.log`）；失败是旧列表用例仍断言两列数值或不带 metrics 的请求，已按精确五项和必填选择修订，不放宽采集配置页两项断言或产品校验。
- `make test-node-metrics-online-runner`：139 项本地入口／身份／收据守卫及 System Metrics Go 夹具通过（`online-runner.log`）；新增拒绝省略指标选择、旧两项列表请求及缺失核心列的反例。仅为准备好的正式入口本地检查，没有运行 Hosted。
- `bash scripts/swagger/gen-swagger.sh monitor` 和 `bash scripts/swagger/check-route-coverage.sh monitor`：生成成功，90 个公开路由方法覆盖一致（`swagger-generation.log`、`swagger-coverage.log`）。固定 Swag module v1.16.6。`make test-frontend-ci-registration`：20 项检查与21个前端登记通过（`registration.log`）。

因摘要请求契约同次更新，个人环境体验需由用户按标准入口重启 Monitor 后端；前端改动不要求重启其他后端。原表第37行（B36，主机实时资源总览列表）仍是部分实现：本批核心五项已具备，三档负载、TCP 与存储／网卡明细仍在主机详情，未补完整列表或综合健康评分；新列表的真实身份页面 Hosted 与生产规模证据继续待专门会话回传。

### Safari 主机列表右侧列与前台自动刷新（2026-10-24）

沿用户已重启的个人环境及现有平台系统管理员会话，只读复验 `http://localhost:5170/monitor/node-resources`。最初辅助功能树仍报告页面隐藏／自动刷新暂停，原生横向滚动返回 `noWindowsAvailable`；AX Raise／窗口菜单操作未使其恢复。用户手动激活当前 Safari 标签并保持前台后，树显示焦点在当前 HTML、自动刷新已开启，原生滚动条拖动可用。该前后观察说明这次验收所需的现场条件已恢复，不能反推全部历史窗口／截图问题的根因。

已实际截图核对内存使用率与运行时长的表头和值，两列可读、百分比和复合时长单位正确，固定主机名称及查看资源入口仍保留。08:30:23（Asia/Shanghai）显示 43.20%、3 天 12 小时 21 分钟；08:31:35 显示 42.46%、3 天 12 小时 23 分钟，且自动刷新仍开启。整个前台记录区间没有点击手动刷新、重载或修改每 15 秒的选择；仅横向滚动、拖动表格滚动条及临时收起／恢复 Safari 侧栏，中间另留 18 秒一次性观察等待。08:29:19～08:31:58 的六份辅助功能文本记录多次 CPU／内存数值自然变化和时长推进，恢复原侧栏后两列仍可读，默认每页 20 台、当前一台 VM。由此确认本次单机页面前台自动更新及右侧列视觉复验通过；没有网络瀑布证据，不推断精确请求次数、重入并发或隐藏期间网络取消已在此实测。

证据目录 `/tmp/addp-safari-host-list-Su1V5L/`：`foreground-before.txt`、`scrolled.txt`、`right-actual.txt`、`right-columns-final.txt`、`automatic-after-cycle.txt`、`restored-sidebar.txt` 及对应原生 JPEG 截图；`verification.json` 保存采样时间、读数、动作与限制。`health-ready.json` 记录 Monitor HTTP 200、ready／registered、实际后端构建 `e931905933bab26701e2586eb8df9a49d9da4a30`。本次文本／状态／时长推进／观察间隔及截图格式完整性断言通过；首次记录脚本误把原生 JPEG 当作 PNG 校验而失败，已按实际格式改名并核对，未修改截图像素或产品代码。不重启个人服务、不改配置或凭据，没有 CI／Hosted 查询、调度或定时任务。此结果仅为个人单台 Desktop VM 的页面现场复验，不计一次性隔离 Hosted／T4、多真实主机、其他角色／撤权、长窗或生产 T5。

### 主机监控类型／纳管状态筛选与分页回跳修正（2026-10-25）

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

### 主机与引擎自动筛选统一（2026-10-29）

主机、服务、引擎列表统一复用Common Frontend的createAutoFilter：文本300毫秒防抖，清空和Enter立即提交，删除主机／引擎搜索按钮；主机类型及纳管状态原本即选即查，继续合并当前名称条件。相同URL不重复读取并恢复刷新；筛选回第一页，主机保留页大小／列视图，引擎保留刷新间隔。原System匹配、授权、批量摘要、查询预算与服务趋势边界均保持，无API或Swagger变更。

共享调度唯一负责等待／立即提交、待提交状态、取消与隐藏暂停／前台恢复；服务页原本地防抖生命周期删除，三页只保留领域查询、权限和路由组合。重置、清空、手动刷新、切换视图／页面及身份变化不被旧文本延迟覆盖；中文输入法继续沿原控件确认后提交。自动刷新只读已提交范围，待提交时暂停旧调度；不另建指标解释、错误或缺样提示路线。

本地实际验证：

- `make test-monitor-frontend`：131项单元测试、503个浏览器场景及构建全部通过，含主机／引擎新增6项和服务原46项；原时间、分页、趋势与撤权边界保留。
- `npm --prefix monitor/frontend run test:e2e -- e2e/automatic-filter-enter.spec.js`：三页独立3项通过（10.5秒）；当时只安装受控时钟、未显式暂停，不能单独证明Enter在防抖前提交。后续显式暂停后的失败与共享修复见[告警自动筛选证据](Monitor告警与规则历史证据.md#平台告警自动筛选与字段提示2026-10-29)。原文件在完整套件发现之后加入，单独执行，不把原聚合计数冒写为506项。
- `make test-console-frontend`：219项单元测试、263个浏览器场景及构建通过；新增主机／引擎中英文各两项，验证自动合并查询、清空／重置、System原参数、回第一页、URL replace与同iframe。
- `make test-common-frontend`：290项中289项通过、1项失败；新增三项自动筛选／可取消调度／唯一归属测试通过。唯一失败仍是上一批ResourceTree直接组合所有权清单未登记其他会话的System组件，本批不修改该组件或清单，不能将共享聚合计为通过。

新用例沿既有测试目录自动发现，Console原浏览器夹具已包含Monitor，模块Frontend标准入口与Platform矩阵继续覆盖，无需修改Make或Workflow。未运行未变的后端、数据库或Online runner；不重启个人服务、不操作个人身份或配置。上述仅为本地确定性T1／受控HTTP与时钟下的真实前端T3，不证明真实Safari登录、实际System筛选数据或Hosted。CI与Hosted仍等专门会话回传，未触发／重跑／轮询、不设置定时任务。

日志：`/tmp/addp-host-engine-filters-{common,monitor-full,console-full,enter}.log`。上一批及本批共享失败分别保留，不将后续局部通过回填为旧失败通过；清单完整功能的原正式验收缺口不因这次交互统一而消失。

### 主机与服务原位刷新（2026-10-30）

同一对象／身份／已提交范围的请求在途保留读数、分页、设备表、图表及比较，按钮与 aria-busy 表示在途。首次加载仍有反馈；上下文、撤权或独立资源组失败清除相应旧值。手工刷新重读已展开设备；挂载告警按钮保留但禁用，第二页空结果不得触发第一页查询。

13 项受控 T3 暂缓 HTTP，覆盖原 DOM／canvas、自动／手工刷新、设备重读和迟到拒绝。相关首轮 147／149，失败于旧图即完成及旧比较清空断言；完整首轮 531／534，首败组含旧挂载按钮移除、空结果分页跳回及服务比较等待，未到构建。修正后定向三项通过，完整 131 单元／534 浏览器／构建通过；12 项输入复核一致。原件 `/tmp/addp-host-service-inplace-*`。

Safari 另验个人 Desktop VM 主机手工刷新和内存曲线自然推进，服务页当前 Monitor 实例的 CPU／RSS 曲线也实际推进并可悬停。采集不支持的文件系统／网络／TCP 与原生 macOS 资源组仍缺。Safari 离散观察只证明真实值与窗口更新；同一 canvas 与延迟响应隔离由受控 T3 覆盖。正式来源 T2、Hosted 及全程无闪未覆盖。

### 主机详情就近提示（2026-10-31）

复用 FieldHelp 将 CPU、负载、文件系统、网卡、设备容量及趋势说明移到对应指标、标题和选择器。数据、轴、窗口、固定历史说明和异常仍直显。Console 验证先桌面切换语言再缩至 620px，保持同 iframe、URL、请求数及当前来源状态。

Monitor 131 单元、构建通过。浏览器首轮 143／144，旧用例等待已迁移的 CPU 比较正文；改为聚焦提示并等 aria-busy 结束后单项通过。Console 首轮 23／28，提示未退出产生两个 tooltip，四个容量用例仍要求在途清空；修正后定向 3／5，余两项为窄屏隐藏语言控件、列表切换刷新偏好误断应收起。桌面切换再缩窄、同上下文保留展开并重读后，两项分别通过；聚合失败保留。原件 `/tmp/addp-host-detail-tips-*.log`。

个人 Safari 实际看到磁盘表头提示、逐设备读数及内存双轴／缩放提示，未提供的文件系统和网络仍直显。仅覆盖当前显示与提示，未覆盖原生 Linux、整套前端门禁或正式 Hosted。

#### Console 窄屏 tip 回归的语言弹层退出同步（2026-10-31）

| 固定源码和实际 Job | 结果 |
| --- | --- |
| `5cb1a9980a1829fc1250834f3a5d3fcb95e6580c` · [Console失败Job](https://github.com/pampa0629/addp/actions/runs/41149596599/job/124382684316) | 自然首轮，271／272 浏览器；620px 外层宽度断言首败，构建未执行。截图不足以定位溢出，邻近 ECONNRESET 未归因为首败。 |
| `f8a1b435ce18597aa7a6eaa1233362bc4b72d7f6` · [Console成功Job](https://github.com/pampa0629/addp/actions/runs/41150779367/job/124386516227) | 272 项及构建通过，相关三前端目录字节无差异；仍不能证明旧失败根因修复。 |

本机同标准场景复现，620px 时 scrollWidth 1171，桌面语言菜单尚在退出（right=1170.75），主内容和 iframe 仍 620。两条测试改为等实际 English 弹层 display:none 才缩窄，原宽度、iframe、URL 和请求断言保留。定向两项通过，隔离服务已清理。原件 `/tmp/addp-ci-20261031-092302/`、`/tmp/addp-host-tip-geometry-diagnostic.log` 和 `/tmp/addp-host-engine-tip-exit-sync.log`。个人来源不可用为独立问题；修正后的固定 Job 见下一节。

#### 菜单退出同步修复的 Linux Console 门禁回传（2026-10-31）

固定 `4a6a7a4fa574ddda204b61d73acbdb5c397e1e52` 的 [Platform CI run41156059896的Console Job124402993366](https://github.com/pampa0629/addp/actions/runs/41156059896/job/124402993366) 自然首轮 completed／success，实际 `make test-console-frontend` 执行 222 单元／272 浏览器及构建，selected=true。原件 `/tmp/addp-ci-20261031-100003/`。

这是退出同步修正自身的 Linux 完整 Console Job，5cb 首败与 f8 后继通过仍各自保留；整个 Platform workflow、Hosted 及个人 Infra／Business 来源恢复未由本 Job 证明。

### 主机详情当前对象变化的原位刷新保护（2026-11-02）

沿原位刷新及6.3的对象失效契约，主机详情在重读System后、等待资源时也可能将新纳管状态与旧读数并列。新增UUID／名称／版本／enabled事实核对：变化后先清空标量、挂载容量／inode、磁盘／网卡、趋势、比较与缩放，再消费本轮结果；独立设备观察组件复用现有node.version／enabled守卫。本批不改变主机列表的整份快照更新方式，也不把每次HTTP返回的新引用当作变化；事实相同时沿原刷新保持行为，不扩大指标、采集、权限或API。

新增停用／版本变更×手动／自动刷新4项，使用暂缓资源响应验证当前事实已重读但旧卡片／canvas尚不可保留，随后释放并核对缺样或新读数。修复前4项均失败（`/tmp/addp-host-owner-refresh-red.log`）；修复后标准 `npm run test:e2e -- e2e/node-resources.spec.js` 整份73项T3通过（2.4分钟，`/tmp/addp-host-owner-refresh-green.log`）。包括原位刷新、隐藏暂停／恢复、当前页摘要、热力、设备／挂载、双轴、缩放、比较、故障撤权及CPU空闲率旧场景，未删改原断言。前端132项T1通过，最终两页构建通过，日志分别`/tmp/addp-engine-owner-refresh-unit.log`、`/tmp/addp-owner-refresh-build-final.log`。

同批[PG／Redis引擎页](Monitor采集来源与引擎指标历史证据.md#引擎当前对象变化的原位刷新保护2026-11-02)46项独立通过。标准脚本独占测试端口，没有启停个人服务；这些为受控T3，不外推个人Safari、真实来源或Hosted，不宣称完整Monitor浏览器门禁通过。CI 与 Hosted 结果在该记录中尚未认定。

<a id="node-evidence-safari"></a>

## Safari 缩放

共享图表的受控回归、固定 Linux 产品步骤及原生 Safari 操作分列。真实缩放／刷新与真实凭据续期是不同场景。

### 同设备双向吞吐同图（2026-10-23）

主机资源页对同一磁盘的读／写、同一网卡的收／发使用一次现有趋势查询，同图显示两条独立折线；按全部有效点共同选择图表单位，并在图旁明确单位及末次评估值。所选方向仍决定采样明细，响应数组乱序不改变方向；真实零值保留，陈旧／缺样各自断开，缺序列、错误设备或评估网格错位拒绝整幅趋势。复用共享 ChartRenderer、格式化和请求隔离，没有新增采集、接口、权限、插值或设备汇总；其他标量、文件系统、磁盘 IO 耗时及 CPU／内存比较保持原单指标路径。规范见平台运行观测 6.3。

冻结基线 `c71bd5cfaddd4607c2d0907691f2d2a06a57d432` 加本批六项前端输入执行 `make test-monitor-frontend`：退出 0，100 项确定性测试、299 项浏览器测试与构建通过。随后仅补图旁明确的容量单位及独立测试截图路径；最终六项输入再次冻结，执行 `npm test`（100／100，无 Skip）、标准 `npm run test:e2e -- e2e/node-resources.spec.js`（65／65）及 `npm run build`，全部退出 0。最终主机页覆盖中英文、620px、双向单位／零值、响应乱序、方向明细、重载恢复、错位拒绝／恢复与独立缺样；其余未受最后单位文本影响的页面复用完整门禁证据，不宣称最终输入又完整重跑299项。

末版专项首次为64／65，一次点击写入方向后 URL 未变化，截图显示选择弹层仍打开；带 trace 的四组合12次检查、加入输入事件记录的单场景10次诊断及最终标准65项均未复现，不能据此确认该次失败根因或宣称修复了间歇点击／Safari 故障。临时诊断代码已删除，不增加重试、超时或移除 URL／明细断言；新截图使用每次浏览器测试的独立 outputPath。保留首次失败证据 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-monitor-playwright-results-lf2gXA/`。

日志、两版冻结输入及诊断记录位于 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-paired-throughput-ge5httgy/`：`frontend.log`、`inputs.json`、`inputs-final.json` 与 `snapshot-final/monitor/` 下的 `unit-final.log`、`node-verified.log`、`build-final.log`、`node-final.log`、`paired-trace.log`、`selection-probe.log`。最终六项输入与工作区哈希一致。仅为隔离夹具 T1／T3，未运行真实 Safari、Hosted T4、CI 或后台重启，等待专门会话回传，未设置定时任务。当前仍为折线，不将本批计为原图的分钟柱形、内存双Y轴、资源热力或完整监控交付。

构建器独立客户端修正后已实际启动；第二次仍在业务部署前主动停止，用于修正启动清单中一次性 init 的前台成功语义及显式 MinIO 镜像绑定，未发业务请求。自动报告 `local-report-final.json` 记录失败／中断及零残留，实际最高硬配额 6 CPU／10 GiB、未发生 OOM。进一步将全部私有可写目录计入原 4 GiB 停止线，只读复用公共 Go 依赖缓存，及时删除当次已完成 Go 对象缓存，未借用个人凭据或写共享缓存；标准 runner 的初始化序列、目录预算及私有文件回归共 35 项通过。全部重试继续使用首次窗口截止时间，不重新取得一小时。

第三次在 product_compile 阶段由守卫自动报 `owned_volume_stop_line_reached`，业务部署／身份／查询均未执行；报告 `local-report-window-final.json` 明确失败且 zero_owned_resources=true。最近成功样本为 4,235,309,056 字节（低于 4 GiB），独立只读 du 随后观察到私有目录 4,266,260 KiB（约 4.07 GiB）；旧守卫在触线断言前未追加触线样本，不能用最近成功样本冒充峰值，已将样本落盘移到断言之前并增回归。只读主仓库 `.git` 约 1.2 GiB，因此快照唯一切换为同提交的 file:// 浅克隆（depth=1、no-local），不复制无关历史，也不放宽资源／时间／验收断言；标准 runner 仍通过。下一次继续沿首次绝对截止时间与同样停止线，不能计为容量或正式身份已通过。

### 内存容量与使用率双 Y 轴（2026-10-23）

主机资源页选择任一内存指标时，沿一次既有固定查询取得总量、可用量和使用率。共享 ChartRenderer 的显式 `valueAxes` 展示属性统一绘图：左轴为按有效容量样本选择的 IEC 单位，右轴固定 0–100%；只换算字节值，百分比不缩放。各序列按指标身份与同一评估网格对齐，缺样各自断线，错位或缺序列拒绝整图；采样明细仍由 URL 所选指标决定。相邻时段比较只读取使用率，容量缺样不伪造比例或阻止本已完整的使用率比较。不新增指标、接口、权限、采集或 Workbench 持久化配置，不推定内存泄漏。

本地冻结输入根为 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-memory-dual-axis-bwf4sj9m`，基线 `34dc7b291515c59f30ce93d03578f82dd2b26f45`；`inputs-final.json` 记录本批文件摘要，`snapshot-final` 只叠加本批改动，未纳入共享工作区其他 owner 的 WIP。验证结果：

- `make test-common-frontend`：261 项通过，无失败／跳过；双轴映射、范围与配置拒绝、单位、数值、null、主题及旧单轴语义均有确定性验证（`common.log`）。
- `make test-monitor-frontend` 首轮确定性测试 100/101；旧单指标断言同步后，最终 `npm test` 为 101/101，通过（`unit-final.log`）。首轮浏览器 299/301，另两项失败均为告警跳转测试仍预期单一使用率查询；同步为三指标组合，并保留完整的错误时窗拒绝夹具后，沿标准 `npm run test:e2e -- host-alert-links.spec.js resource-comparison.spec.js` 复验 29/29（含新增容量缺样不影响使用率比较场景，`affected-browser-final.log`）。其余输入未变的 273 项沿全量通过证据复用，合计 302 个唯一场景具有通过结果；未再重复整轮，不将最初失败的聚合命令记作通过。初始失败与截图保留在 `monitor-fixed.log` 及 `addp-monitor-playwright-results-VV4xu8`。
- 新增中英文 620 像素窄屏实图已逐一核对左右轴、图例与刻度；浏览器同时验证乱序响应、GiB→TiB 容量单位变化、百分比保持、可用量独立缺样、刷新恢复、URL 重载、采样明细及撤权清除。截图：`/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-memory-axes-9q4js8/zh-cn.png`、`/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-memory-axes-CR6PqE/en.png`。
- Monitor 最终 `npm run build` 通过（`build-final.log`）；Console 标准浏览器入口 `npm run test:e2e -- process-resource-navigation.spec.js --grep 'exact managed host'` 1/1，通过，核对同一 iframe、实际主机趋势及返回筛选（`console-navigation.log`）。
- `make test-frontend-ci-registration` 20 项及 21 个前端登记检查通过（`registration.log`）；现有共享依赖扩散与 Monitor 前端登记已覆盖本批，无需新增 workflow。Hosted 既有脚本的内存比较查询预期同步复用同一指标组合；`make test-node-metrics-online-runner` 的 138 项确定性测试与 Metrics Go 身份夹具通过（`online-runner.log`），不是实际 Hosted 结果。

以上仅为本地 T1/T3 和验收脚本的确定性验证，不计真实身份／数据源 T4 或生产 T5。CI 和 Hosted 结果由专门会话回传。

本轮最终实现与失败边界（2026-10-23，正式身份本地入口）：Common 检查点及 owner 接线提交 `3f59dc5c56d44f11bb8c8fa7b8518ec36028f965`；入口后续修正至 `d4b31264d`。共享扩散最终 `make test-go` 退出 0，22 个模块通过；最终 `make test-infra-runtime-log-lifecycle` 退出 0，包含 40 项本地入口契约、JS 语法／CI 登记／System ExecutionTrace Go，以及既有 60 项生命周期检查。40 项包含真实回环 Cookie Jar／退出、守卫失败前拒绝新业务请求、客户端与 daemon 分别校验无凭据代理、完整可写预算／触线样本、采样失败步骤和总硬配额。标准入口准备验证通过不等于正式身份实际执行通过。完整 Console 结果仍为上述 192/193，E2E/build 未执行，不把专项四项通过记为全量通过。

实际操作始终共用首次窗口的绝对截止 `2026-10-23 09:10:30 Asia/Shanghai`，没有逐次取得新一小时。浅克隆修正后六个后端与 runtime-log 均已从 `34dc7b291515c59f30ce93d03578f82dd2b26f45` 编译；安全哈希见 `shallow-source-receipt.json`，该次最高已记录 3,377,561,600 字节、6 CPU／10 GiB，随后 MinIO 公共基础镜像匿名令牌请求超时。诊断确认宿主构建客户端与 Docker daemon 的代理不同：客户端仅可显式使用已核验、不含凭据的回环代理；daemon 仅可使用已核验的本地代理。不得继承任意代理／凭据，不向业务容器注入代理，宿主 API 访问固定回环绕过公共代理。修正后在当次 BuildKit 内实际观察到 MinIO 的 Go 构建，公共镜像下载及源码步骤已推进，但尚未证明全部公共构建成功。

最后一次实测 `addp-trace-local-898d068c3ab94a8badc3651328e69299`（源码 `83ef3e512`）仍在 product_minio 阶段自动停止，报告 `command_deadline_exceeded`；该命令未到其 1200 秒上限，整窗也未到 3600 秒，结合入口的共享失败传播可确定来自四秒资源采样命令超时。旧版未记录采样子步骤，不能断言具体是 Docker 列表、卷扫描或绑定目录扫描，也不能宣称已修复其根因。最后成功样本为 2,873,073,664 字节，未触及 4 GiB；未知失败样本的磁盘峰值不记为已知。现为采样器预留 0.25 CPU，运行服务＋浏览器／身份辅助＋采样器合计 7.95 CPU，内存仍在 12 GiB 内；维持四秒采样预算及原停止线，新增失败步骤／耗时记录和业务请求前后守卫。新配额及诊断只经确定性测试，尚未经过新的真实窗口，不认定采样稳定性已通过。

八次实际启动中，首轮报告写入异常使用明确标注的人工复查回执，其余七份自动报告全部为 failed 且清理通过；均未创建业务部署、未准备正式身份、未发送成功／失败查询，也未进行真实 Console／Trace／日志闭环。`local-final-receipt.json` 汇总每次报告哈希、停止阶段、资源记录与独立清理复查；当次容器、网络、卷、构建器和私有目录残留均为零。全部原始安全报告、准备门禁日志及输入摘要位于 `/tmp/addp-p19-formal-local-20261023/`。因此整体仍为部分实现，正式身份验收未执行；不是业务链路失败或通过的证明。后续优先在同样 8 CPU／12 GiB／4 GiB 停止线下确认一个新的最长 60 分钟本地窗口，先核对采样稳定性，再执行正式身份闭环；未经确认不续开窗口，不运行 Hosted、容量或定时，不接管个人服务。

代码提交 `d4b31264dc7308ca7b1b7479e9b697a97580b16b` 推送后单次只读核对：[Platform CI 39881617580](https://github.com/pampa0629/addp/actions/runs/39881617580) 与 [Release/T2 39881617528](https://github.com/pampa0629/addp/actions/runs/39881617528) 均为 queued，不计为通过；安全回执 `ci-receipt.json`。没有手工调度任何 workflow 或设置后续跟踪。

### 网卡收发速率柱状展示与局部缩放（2026-10-23）

主机网卡趋势复用共享 ChartRenderer，以并排柱状显示同一网卡的接收／发送速率。保持现有完整一分钟平均字节速率、原时段、原步长和评估点，不换算累计量、分钟分桶流量或带宽利用率；图旁说明口径与末次值，轴显示 IEC 速率单位。真实零值保留，陈旧／缺样独立为 null；错误设备、网格错位、预算错误及撤权继续清除旧趋势。磁盘保持折线，内存保持双轴。共享展示属性 maxRows 显式允许该有界视图最多 1,000 行，其他柱图默认仍为 500，Workbench 持久化契约不变。密集常显数值标签关闭，tooltip 与采样明细保留数值及单位。

近千组柱在窄屏全窗会成为密集概览，因此同一共享 renderer 提供本地缩放滑块。缩放保留全部 rows 与 series 数据，不触发请求或重算指标；临时 start/end 通过共享事件交宿主保存，刷新同一图表恢复，切换主机／网卡／时段／身份或查询失败时清空。组件通过既有主题变量绘制滑块，主题重绘保留当前相对视口，不新增持久化、接口、权限、依赖版本或后端开关。

本地冻结输入：`/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-network-bars-gate-k95pp9k0`，基线 `79751c6233d7650a50db7ae060ccdc91a43ba624`，最终实现与测试摘要见 `inputs-final.json`／`snapshot-final`。期间共享版本提交 `9e5fa266b`（v0.1.24）已包含产品实现；交付前逐文件核对，当前工作区实现与冻结输入相同，最终测试增强和本节另行提交。

- `make test-common-frontend`：最终 264/264，通过，无失败／跳过；覆盖默认上限不变、显式上限类型／边界／超限拒绝、饼图拒绝覆盖、完整结果约束、隐藏标签仍保留零／null／tooltip、缩放保留全数据及主题颜色（`common-final.log`）。
- Monitor `npm test`：101/101，通过（`monitor-unit-final.log`）。沿标准浏览器入口 `npm run test:e2e -- node-resources.spec.js network-bars.spec.js`：最终 69/69，通过（`browser-final.log`）；随后仅增强绘图动画完成等待和时段切换重置断言，`npm run test:e2e -- network-bars.spec.js` 最终 2/2，通过（`network-settled-browser.log`），其余 67 项输入未变、沿有效证据复用。覆盖中英文 620 像素窄屏、5 分钟 21 点／1 小时 241 点／24 小时 961 点／7 天 984 点、乱序响应、原步长与单位、各方向独立缺样／陈旧值排除、真实零值、切换方向及采样明细、错误设备／预算失败后恢复；既有磁盘、内存双轴与撤权回归同时通过。
- 缩放使用实际鼠标拖动绘制出的滑块手柄，确认不新增请求，完整两组 984 点仍保留；主题重绘和刷新恢复视口，切换时段及查询错误清空视口。最终截图等待图引擎动画实际完成，已核对中英文五分钟及七天局部稳定绘图：`addp-network-bars-EXT7Am/zh-cn-5m.png`、`addp-network-bars-MPmUTl/zh-cn-7d-zoom.png`、`addp-network-bars-8UKtyV/en-5m.png`、`addp-network-bars-n2SGks/en-7d-zoom.png`（均位于同一 macOS 临时目录父级）。
- Monitor `npm run build` 通过（`build-final.log`）；`make test-frontend-ci-registration` 20 项及 21 个前端登记检查通过（`registration.log`）。

初始失败不计通过：新增七天用例曾误写 630 秒／961 点，核对后端既有 plan 后修正为 615 秒／984 点，未改变产品步长、上限或查询；初始滑块测试使用配置坐标偏移，真实渲染手柄中心受图引擎布局影响，点击未命中。运行时绘制边界诊断保留在 `zoom-hit-test.log`；最终从实际绘制手柄取点并执行真实拖动，未增加等待预算、重试或减弱缩放断言。初始失败日志与报告分别保留在 `monitor-browser.log`、`network-zoom-browser.log` 及对应 `addp-monitor-playwright-results-*`；最终等待动画完成避免将过渡帧当稳定实图，未据此修改产品数据或过滤模式。

以上为本地 T1/T3，不计真实来源与身份的 Hosted T4 或生产 T5；未运行完整 Monitor 其他领域浏览器全集，不外推其结果。CI 和 Hosted 结果等专门会话回传。清单第 38 行仅补网络柱图的局部缩放，第 39 行补网卡速率柱图；不能将全部图表拖拽缩放、图例筛选、资源热力面板及原行其他缺口同时标为完成。

### CPU、内存与磁盘趋势复用共享时间缩放（2026-10-23）

主机 CPU、内存和磁盘折线趋势接入既有共享 ChartRenderer 滑块，与网卡柱图沿同一路径保存临时视口。缩放只改变图中可见范围，完整 rows／series、查询窗口／步长、采样明细及相邻时段比较保持不变；说明紧邻图表展示。视口绑定主机、指标组、设备维度、时段、固定锚点、比较模式和当前身份，刷新及主题重绘保留；同组内存指标、同设备读写方向切换保留，不同组或上述范围变化以及查询失败时清空。不新增指标、API、权限、依赖或持久化配置；负载、运行时长、文件系统、服务及引擎图表不在本批接入范围。

本地冻结输入根为 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-host-time-zoom-io4lexon`，基线 `a53690c9f933e7ca0a6a32f18bfee0a9a6f043e3`，`snapshot` 只叠加本批路径，`inputs-final.json` 记录七项实现／测试／规范的 SHA-256。交付前工作区逐文件一致；期间 main 上其他提交未改变 Monitor 前端、common-frontend 或本批标准测试执行脚本。

- `make test-common-frontend`：264/264，通过，无失败／跳过（`common.log`）；Monitor `npm test`：101/101，通过（`unit.log`）。共享 renderer 产品代码未再修改。
- Monitor 标准 `npm run test:e2e -- node-resources.spec.js network-bars.spec.js host-resource-zoom.spec.js resource-comparison.spec.js`：最终 96/96，通过（`browser-final.log`）。新增九项覆盖 CPU／内存／磁盘中英文窄屏、真实鼠标拖动、七天完整 984 点不裁剪、主题及容器尺寸变化、手动／自动刷新后恢复、内存双轴和同设备双方向切换、独立缺样、时段／比较模式／不同指标组切换及查询失败／身份撤销清除；CPU 和内存时段对比仍使用原完整范围，采样明细行数和比较结果不受局部缩放影响。既有主机资源、网卡柱图和时段比较回归同时通过，未把此结果外推整个 Monitor 浏览器全集。
- Monitor `npm run build` 通过（`build.log`），仅有既有产物 chunk 大小提示；`make test-frontend-ci-registration` 20 项及 21 个前端登记检查通过（`registration.log`）。
- 等待图引擎实际绘制动画完成后，逐一核对六张稳定图：中文 CPU／内存／磁盘分别为 `addp-host-zoom-wyeyJO/cpu-zh-cn.png`、`addp-host-zoom-DgobXJ/memory-zh-cn.png`、`addp-host-zoom-dVqTTx/disk-zh-cn.png`，英文分别为 `addp-host-zoom-ZNYrtD/cpu-en.png`、`addp-host-zoom-SsDgoL/memory-en.png`、`addp-host-zoom-xn49JR/disk-en.png`（均在上述冻结输入根的同一 macOS 临时目录父级）。轴、单位、内存双轴、图例、局部时段与滑块均可见，窄屏无横向溢出。

初始新增场景为 8/9，自动刷新场景失败于测试夹具读取不到图引擎模块；日志 `zoom-browser.log` 与报告 `addp-monitor-playwright-results-ymYElk` 保留。查明 Playwright 虚拟时钟同时替换 Performance 的资源读取接口并返回空数组；共享测试 helper 改为从页面实际资源请求记录模块 URL，再查询真实图实例，不保留性能表 fallback，也不改变产品逻辑、等待预算、重试或业务断言。修正后自动刷新专项 1/1（`automatic-zoom.log`），随后上述最终 96 项整组通过；最初失败不计通过。测试时钟只在隔离浏览器验证刷新，不是定时任务。

以上为本地 T1/T3 与构建／登记证据，浏览器使用受控 HTTP 夹具，不计真实来源、正式身份、Safari／Hosted T4 或生产 T5；本批未新跑 T2，原指标口径和来源未变。未查询、触发或重跑 CI／Hosted，未设置定时任务、重启个人服务或修改个人数据库；云端结果等专门会话回传。清单第 38 行仅补 CPU／内存／磁盘共享时间缩放子项，不能将所有图表缩放、完整图例交互验收或原行其他缺口一并记为完成。

### 主机趋势图例筛选与悬停交互验收（2026-10-23）

沿现有共享 ChartRenderer 补齐主机 CPU、内存、磁盘及网卡趋势的真实鼠标交互验收，产品渲染器、页面、指标、API、权限及查询均未修改。测试共用唯一 `resource-chart.fixture.js`，将原缩放专用状态读取统一为图表状态读取，按真实绘制图例的边界点击，并用图引擎坐标转换定位原始评估点后移动鼠标；不以 dispatchAction 模拟点击或直接调用 tooltip formatter 代替交互。已有缩放与网卡测试同步使用该 helper，旧函数路径删除。

冻结根 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-trend-interactions-mlaz8a33`，基线 `3217e418c76a837ce91e0865969341b52ba5ca03`；`inputs-final.json` 记录四项测试输入，交付前逐项哈希一致。标准入口提供独立回环 Vite 端口、源码及缓存隔离和退出清理，没有访问个人 Console 或登录态。本轮最终使用已安装的 Node v24.11.1（`fnm exec --using v24.11.1` 仅影响当次命令，遵从根 `.node-version`，不修改全局配置）。

- `npm run test:e2e -- host-trend-interactions.spec.js network-bars.spec.js host-resource-zoom.spec.js`：最终 22/22，通过，无失败／跳过（`browser-node24.log`）。新增 11 项覆盖四类主机图表的中英文 620 像素场景、单项隐藏／全部隐藏／恢复、悬停时间与精确值及单位、真实零／空值／陈旧值、缩放后原始索引、内存左右轴单位、采样明细和 URL／查询次数不变；CPU／内存完整时段比较结果不受显隐／悬停／缩放影响，查询失败及身份撤销清除可见提示与旧图。原 11 项缩放／网卡场景同时通过，未外推全模块或其他趋势图。
- Monitor `npm test`：101/101，通过（`unit-node24.log`）；`npm run build` 通过，仅有既有 chunk 大小提示（`build-node24.log`）。`make test-frontend-ci-registration`：20 项及 21 个前端登记检查通过（`registration-node24.log`）；新增 spec 沿既有 Playwright 自动发现，未新增入口、依赖或 workflow。
- 已逐一查看最终八张悬停稳定截图，确认中文／英文时间、CPU 百分比、内存 GiB 与百分比、磁盘及网卡 MiB/s 均清楚，缩放后图例和提示可见。截图父目录为上述冻结根的同一 macOS 临时目录父级：中文 `addp-trend-interaction-wE3AC0/cpu-zh-cn.png`、`addp-trend-interaction-WtUfKD/memory-zh-cn.png`、`addp-trend-interaction-VPTfxJ/disk-zh-cn.png`、`addp-trend-interaction-uMrVNX/network-zh-cn.png`；英文 `addp-trend-interaction-Oiz3T8/cpu-en.png`、`addp-trend-interaction-tQcryg/memory-en.png`、`addp-trend-interaction-XnPQa1/disk-en.png`、`addp-trend-interaction-2TGDiL/network-en.png`。

最初单项测试将陈旧百分比夹具错误设为超过 100，页面按原响应校验拒绝整图，日志 `interaction-cpu-initial.log` 及报告 `addp-monitor-playwright-results-OkYGg3` 保留。夹具修正为范围内的陈旧百分比，仍要求该点不显示数值，未放宽产品校验、等待或断言；随后专项通过。默认终端 Node 为 v25.2.1，之前的 22 项及新增断言后 11 项通过保留在 `interaction-browser.log`／`interaction-browser-final.log`，最终交付结果以符合仓库工具链的上述 Node 24 门禁为准。

本批仅为受控 HTTP 夹具的本地 T1/T3、构建及登记证据，不计真实来源、正式身份、Safari／Hosted T4 或生产 T5；未新跑 T2 或其余领域浏览器全集，未重跑输入未变的共享组件单测。未查询／触发／重跑 CI 或 Hosted，未设置定时任务、重启个人服务或修改个人数据库。清单第 38 行可补上述四类主机图表的图例筛选与悬停本地验收，不代表所有图表、真实运行及原行其他缺口均已完成；CI 和 Hosted 等专门会话回传。

### 负载、运行时长与文件系统趋势统一时间缩放（2026-10-23）

主机页全部既有趋势统一开启现有共享 ChartRenderer 滑块，删除原 CPU／内存／磁盘／网络类别开关；新增覆盖负载、运行时长、文件系统容量／比例及 inode 数量／比例，不新增 renderer、指标、API、查询或权限。沿既有相对视口、图旁说明、原查询步长和完整结果；刷新保留，指标组、主机、时间、比较模式或身份改变及查询失败时重置。文件系统沿完整 device／mountpoint／fstype 区分对象，容量与 inode 的不同指标切换重置；不按挂载名猜同一资源。原有负载任务数、时长秒、容量 IEC、比例与数量口径均未改变。

冻结根 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-host-all-trend-zoom-kztnpbgt`，基线 `dcf9b22460d42ff3d8782051e214cf5da1746ab6`，`inputs-final.json` 为四项实现／测试／规范输入摘要，交付前与工作区逐文件一致；快照不包含其他会话 WIP。所有命令使用已安装 Node v24.11.1（`fnm exec --using v24.11.1`），沿标准入口及独立端口运行：

- Monitor `npm test` 101/101、`npm run build`、`make test-frontend-ci-registration` 的 20 项及 21 个前端登记均通过（`unit.log`、`build.log`、`registration.log`）；构建仅有既有 chunk 大小提示。共享 renderer 产品代码、依赖及测试注册入口未改，既有 Monitor owner／Playwright 自动发现覆盖本批，无需新增 workflow。
- `npm run test:e2e -- node-resources.spec.js network-bars.spec.js host-resource-zoom.spec.js host-trend-interactions.spec.js resource-comparison.spec.js` 首轮 119/120，一项在窄屏切换后的同步布局检查失败，不能记聚合通过（`browser-final.log`，失败报告 `addp-monitor-playwright-results-6Mdfe1`）。完成下述诊断并仅修测试的绘制完成条件后，`npm run test:e2e -- host-resource-zoom.spec.js` 最终 22/22，通过（`zoom-final.log`）；其余 98 项输入未变，复用本轮通过证据，合计 120 个唯一相关场景具备通过结果，不声称首轮命令成功或整个 Monitor 浏览器全集通过。
- 缩放矩阵覆盖中英文 620 像素、七天 984 点、负载小数、运行时长、文件系统 GiB／百分比与 inode 整数／百分比、真实鼠标拖动、不新增请求或裁剪数据、主题及宽窄切换、刷新保留、缺样断点、范围／错误重置。新增受控两挂载点夹具沿真实表格按钮选择，验证刷新保留、挂载变更及容量／inode 切换重置与精确请求维度；现有自动刷新场景增加负载缩放恢复，原 CPU／内存／磁盘／网络和时段比较／撤权回归保留。
- 最终十二张新类别中英文稳定实图已逐一核对单位、曲线和滑块，路径见 `zoom-final.log` 中 load／uptime／filesystem-percent／filesystem-capacity／inode-percent／inode-count 截图条目，均在上述冻结根同一 macOS 临时目录父级；未将沿用的运行时长秒轴改为其他时间单位。

失败诊断保留 `layout-diagnostic.log`、`resize-event-diagnostic.log`、`resize-observer-diagnostic.log`。首次观察未重现瞬态，不据单次通过宣称已定位；随后在宽屏实际绘制完成再缩回窄屏的事件采样中，记录 viewport=620、容器=572、Canvas=852、页面 scrollWidth=876，绘制完成后 Canvas=572、页面=620。共享 ResizeObserver 按既有异步路径正常恢复；旧测试在两次视口设置后立即检查页面宽度，未等待实际绘制。最终用同一默认断言预算等待两次 Canvas／容器宽度一致且页面无溢出，不加重试、延长超时、放宽宽度限制或修改产品布局。三个诊断场景不是交付通过计数；本批最初负载／挂载专项 3/3 另存 `zoom-focused.log`。

以上为本地 T1/T3、构建及登记证据，浏览器为受控 HTTP 夹具，不计真实来源、正式身份、Safari／Hosted T4 或生产 T5；未新跑 T2 或未涉及 owner 的门禁，未把缩放接入外推为专业图表或全部原始功能完成。未查询／触发／重跑 CI 或 Hosted、设置定时任务、重启个人服务或修改个人数据库。清单第 38 行可补主机负载／时长／文件系统共享缩放的本地结果；真实页面与来源验收仍等专门会话回传。

### 主机运行时长趋势的自适应显示单位（2026-10-23）

主机运行时长曲线按完整已加载时窗内有效点的最大秒数选择秒／分钟／小时／天，纵轴与 tooltip 使用同一单位、两位小数；缩放不重选量级，刷新跨量级可以更新。共享 `scaleDurationValue` 与原复合时长格式化共用唯一 day／hour／minute／second 换算表，不读取时区或舍弃原始精度；Monitor 只换算图表 rows 与字段呈现。API 秒值、采样证据、重启判断、当前卡片及采样明细的原复合时长格式均不变。零值有效，陈旧／缺样不选级且保持 null，全缺样默认秒；不新增配置、依赖、API 或权限。

冻结根 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-uptime-units-k46w9uio`，基线 `d01c5772f84d19c74010d1bd048433c8000d85be`；最终 `inputs-final.json` 记录十一项产品／测试／规范输入，交付前与工作区逐项一致，快照排除其他 owner WIP。各命令通过 `fnm exec --using v24.11.1` 使用根工具链，沿已有独立端口及清理入口：

- `make test-common-frontend`：267/267，通过，无失败／跳过（`common.log`）。包含时长量级阈值、小数／零／非法值及原复合时长格式回归；其他共享单位与图表约束保持。Monitor `npm test` 101/101、`npm run build` 及 `make test-frontend-ci-registration` 的 20 项／21 个前端登记均通过（`unit.log`、`build.log`、`registration.log`），构建仅有既有 chunk 大小提示。
- 新增 `npm run test:e2e -- uptime-units.spec.js` 首次 2/2，通过（`uptime-browser.log`）；覆盖中英文秒→分钟→小时→天、真实悬停、跨量级刷新保留缩放、图例显隐、原采样明细与当前卡片不变、陈旧极值排除、独立缺样／全缺样、真实零、错误清除与恢复，查询仍只含原 uptime 指标。267 项共享 T1 与真实 Monitor 消费页面共同验证新格式化入口，不外推其他消费模块的完整页面验收。
- 组合 `npm run test:e2e -- node-resources.spec.js network-bars.spec.js host-resource-zoom.spec.js host-trend-interactions.spec.js resource-comparison.spec.js uptime-units.spec.js` 首轮 115/122，不能记成功（`browser-final.log`，失败报告 `addp-monitor-playwright-results-g8XJhD`）。失败包括拖动未改变视口、动画／加载等待、创建 page 阶段超时、明细点击、错误切换及缩放值捕获；创建 page 的一项尚未运行产品。未延长 5 秒断言／30 秒场景预算，不加重试或跳过。
- 确认滑块实时事件经过 ECharts 节流：最初新英文场景捕获 54.770992366412216，后续实际视口为 59.732824427480914。共享测试 helper 改为在 mouseup 后等待已应用 start／end 与实际滑块 `_range` 一致，再保存或比较；只等真实事件完成，不调用 dispatchAction 或强制 flush，不改变产品行为。最终 `npm run test:e2e -- host-resource-zoom.spec.js host-trend-interactions.spec.js network-bars.spec.js uptime-units.spec.js` 37/37，通过（`affected-browser-final.log`）。
- 其余三个失败场景保留原产品和断言，沿 `npm run test:e2e -- node-resources.spec.js resource-comparison.spec.js --grep 'network throughput restores|memory capacity and usage.*zh-cn|expired-valid clears' --trace on` 诊断复验，3/3，通过（`remaining-browser-diagnostic.log`）。未受 helper 调整影响的其余 82 项沿本轮有效通过证据复用，合计 122 个唯一相关场景具有通过结果；不宣称初始聚合通过或完整 Monitor 浏览器全集通过。

诊断 `interaction-diagnostic.log` 的三个场景通过，记录了实际鼠标命中、绘制帧、页面可见性与请求错误，未复现最初的加载／动画失败；临时诊断 hooks 已从最终快照和工作区移除。新的通过结果不证明这些历史偶发失败的根因已经确定或修复；只有上述滑块中间状态的同步条件得到针对性补齐，其余历史加载／动画超时仍待定位，不据 CPU 使用率快照推定原因。原 34.6 分钟聚合执行失败保留，不通过重试计成功。

最终中英文窄屏悬停图已逐一核对：`addp-uptime-units-VtugVb/zh-cn.png`、`addp-uptime-units-Fq3pIK/en.png`（与冻结根位于同一 macOS 临时目录父级）。286,435 秒的图轴及悬停显示 3.32 天／days，明细仍为 3 天 7 小时 33 分钟／对应英文；图例和缩放保留。以上为本地 T1/T3、构建／登记，不计真实来源、正式身份、Safari／Hosted T4 或生产 T5，未新跑 T2。未查询／触发／重跑云端 CI 或 Hosted、设置定时任务、重启个人服务或修改个人数据库；云端验收继续等专门会话回传。清单第 38 行仅补时长趋势显示量级与当前本地验证，历史偶发失败和其他原行缺口不能据此关闭。

CI 专门会话回传补充（2026-10-23）：固定提交 `2cd3e278fc00c19183f3508f45253d31e9eef9f8` 的 [Platform Monitor frontend Job 120242877828](https://github.com/pampa0629/addp/actions/runs/39901181026/job/120242877828) 已实际执行 `Run Monitor frontend gate` 并成功。Owner 已核对 Job 的提交身份／实际步骤及原始日志：101 项单测、339 项浏览器均通过，失败／跳过为零，构建 10.36 秒；包含 memory／disk／network 中英文六项图例／悬停／缩放及新增两项时长单位场景。原始材料为 `/tmp/addp-ci-20261023-070013/critical-120242877828.json`、`job-120242877828.log`、`Monitor-2cd3-frontend-actual-pass-audit.json`。这是该固定输入的前端 CI 证据，不表示父 Platform workflow 全部通过，不覆盖后续服务独立缩放／进程环比提交、实源告警 T2 或 Hosted；旧输入失败及尚未复现的加载／动画偶发根因继续保留，未重跑任何门禁。

### 主机固定目录全部图例与悬停交互（2026-10-23）

本批只补齐既有主机资源交互的本地验收，不新增产品指标、算法、权限、接口或部署变更。`host-trend-interactions.spec.js` 沿同一鼠标／图例／缩放 helper，覆盖当前全部 25 项固定指标：CPU 核数／忙碌率、内存三项、1／5／15 分钟负载、运行时长、五项文件系统容量、四项 inode、五项磁盘及两项网卡指标。每种组合实际读取对应采样时间／数值／单位，验证零与缺样／陈旧值、图例隐藏至全隐藏及恢复、缩放后精确悬停、原始图表点／URL／采样明细不变且无额外查询；负载按任务数、inode 与核数按整数，容量／速率和时长沿现有单位。错误／撤权清除补到负载与字节／inode 挂载分支。

冻结基线 `d23233e15e61eb77d5bef576043eef11ec210a99`，证据目录 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-host-interactions-rest-5ixp4m82`；`inputs.json` 记录本批文件，`frontend-inputs-sha256.json` 记录 432 项前端输入，提交前与共享工作区一致。Node v24.19.0 下，最终标准入口 `npm --prefix monitor/frontend run test:e2e -- host-trend-interactions.spec.js` 48/48 通过（`interaction-final-browser.log`，4.2 分钟，42 个中英文指标组合、2 个原比较回归和 4 个错误／撤权场景；不将此前 40 项聚合重复累计）。`npm --prefix monitor/frontend test` 110/110 通过，`make test-frontend-ci-registration` 的 20 项及 21 个前端登记检查通过（`unit.log`、`registration.log`）；原有 Playwright glob 和 Monitor CI 自动覆盖本文件，无需另加编排。42 张窄屏实图保存在 `screenshots/`，负载和 inode 截图已人工核对。

本批仅为受控 HTTP 夹具加真实图表的本地 T3 与 T1／登记证据；产品源码未变，未重建或重跑输入未变的其余模块／前端全集。未运行 T2、真实身份／来源 T4、Hosted 或生产 T5，未追查／重跑 CI、设置定时任务或重启个人服务。历史加载／动画偶发失败仍不能由本次通过推定根因已修复。原表第 38 行图例／悬停本地专项缺口可关闭，真实运行、留存与原行其他缺口保留。

### 同组主机趋势切换的事实比较修复（2026-11-02）

固定 `aee6bcc427f714aefb97b58090789a8e6acb3fab` 的 [Monitor前端Job125032898526](https://github.com/pampa0629/addp/actions/runs/41368234803/job/125032898526) 584／590，六项双语内存／磁盘／网络的同组切换使缩放归零，本地同六项复现。根因为查询切换先清空展示 node，再以空展示值比较 System 响应。修正用最近确认的主机事实比较；换主机／身份即清空，真实事实、查询范围、设备、指标组、失败或撤权仍按原规则重置。

六项通过后加入真实名称／版本变化两反例，定向 8 项、132 单元和构建通过。18 个 spec 扩散回归 261／266，失败涉及挂载过滤第二请求、设备列表、英语初次筛选、热力初次显示及英语内存动画等待；后者已通过缩放值断言，与旧 CI 的缩放归零不同。前四定向补验通过，组合原失败和未定根因保留。原件 `/tmp/addp-ci-20261102-090015/`、`/tmp/addp-monitor-zoom-*.log`，失败目录 `addp-monitor-playwright-results-n4OP7X`。个人 Safari 与 Hosted 未覆盖。

#### 缩放修复的固定 Linux 产品门禁回传（2026-11-02）

固定 `74edeb978bd6a8c2874401b0df336bf050a2e0e5` 的 [Platform Run41375543514／Monitor前端Job125054853600](https://github.com/pampa0629/addp/actions/runs/41375543514/job/125054853600)（attempt 1）实际产品步骤成功，592 浏览器与构建通过，含六项同组反例和两项真实对象变化。整 Job 在随后 Post Prepare 的 Node 缓存 tar 阶段取消；原 aee 六失败、`7e2` 未运行完及本机扩散五失败各自保留。原件 `/tmp/addp-ci-20261102-104553/`。

三份主机实现／测试输入与固定 `4243aefa676f93c71916872dda30c6127aa91546` 一致，独立回执 `/tmp/addp-ci-20261102-123449/Monitor-current-owned-input-recheck.json`。只支持复用缩放产品步骤证据，未覆盖整 Job／workflow、全部新 main 输入、Safari、Hosted 或 `59d4adaee` MinIO 来源。

### 主机恢复后的Safari复验与缺样提示修正（2026-11-04）

个人 Safari 管理员页实见 18 核、62.68 GiB、CPU／内存／vda，手工刷新时间和数值更新，内存三曲线及双轴可见。休眠缺样保留断点，TCP／文件系统／网卡未提供，容量采样器关闭。旧汇总把全部 no_data 暗示为等待可恢复；新双语提示引导查看对应指标原因，两项受控红绿验证缺样与合法零恢复。

Monitor 135 单元通过。完整浏览器首轮 534 通过／8 失败／1 中断／141 未运行，观察到 Clamshell Sleep 与多次 DarkWake；经归属核验中止自有 runner，退出 130／Make 2，隔离目录和端口租约已回收。唤醒后原样定向 8 项和余下 144 项通过，其中 2 项重叠，合计 684 个不同场景有分批证据；独立构建通过，原整轮失败保留。原件 `/tmp/addp-node-collection-*`，失败截图 `addp-monitor-playwright-results-dsytDF`。

个人 Safari 曾看到自动刷新开启与时间推进，后又报告隐藏；拖动报 noWindowsAvailable。当时仅确认真实读数、手工刷新和双轴展示，持续前台、原生拖动及缩放保持另见下一节，正式 Hosted 未覆盖。

### 主机Safari前台刷新及原生缩放保持复验（2026-11-04）

11:29–11:35 的个人 Safari 管理员会话实际保持前台，原生拖动可用。Desktop VM CPU／内存／vda 有真实值；TCP、文件系统及网卡仍未接入。此次恢复可操作性未定位历史 hidden／noWindowsAvailable 的根因。

| 实际操作 | 现场结果 |
| --- | --- |
| 内存近一小时，原生拖动左柄 | 保持三曲线和 GiB／% 双轴，无手工输入时窗口末端 11:30:16→11:31:02，缩放保持；手工刷新后至 11:31:50 仍保持。 |
| CPU 忙碌率近一小时，原生拖动 | 无手工输入时 11:33:12→11:34:14，视口保持；AX 有一帧刷新按钮禁用且原图仍可见。实际悬停 11:16:33 为 48.75%，手工刷新后至 11:35:43 保持。 |

原生 AX、截图与实际输入支持本次短时前台自动更新、拖动与刷新缩放保持。人工摘要 `/tmp/addp-safari-host-trend-20261104.json`。点击顶部刷新会滚至按钮，返回图表只核对缩放；没有证明手工刷新滚动位置、全程无闪、DOM 节点身份或长期稳定。磁盘等其余图、其他浏览器和 Hosted 未覆盖。

### 磁盘吞吐纵轴单位与Safari交互（2026-11-04）

Safari 实际 vda 近一小时双曲线，原生缩放后 11:45:34→11:46:20 自然推进，手工刷新后 11:47:28 保持。11:37:05 的读／写为 0.00／0.75 MiB/s；首个页面问题是图上纵轴只有数值，单位只在提示。

复用共享轴声明，单轴最小值 0，双向使用同一 IEC 单位。两项中英文 T3 的首败是旧轴缺名称和 min，红绿通过后另验刷新 MiB/s→KiB/s；135 单元、89 定向浏览器与构建通过。原件 `/tmp/addp-disk-axis-*.log`，失败目录 `addp-monitor-playwright-results-D4iyCr`。未执行完整前端／CI。

修订后 Safari 实见“磁盘读写吞吐（MiB/s）”且可拖动。11:55:38 首次等待时回顶部并复位，原因未定；11:57:25 重建视口，手工刷新后至 11:58:45 的 46 秒自动推进仍保持。此前隐藏暂停及手工恢复各自保留，滚回图表不计滚动位置保持。人工摘要 `/tmp/addp-safari-disk-trend-20261104.json`。仅当前 Desktop VM、当前设备和短时观察；Mac 物理盘、全程无闪、长期稳定、其他设备和正式 Hosted 未覆盖。

### 主机视口复位与凭据续期隔离复现（2026-11-04）

对 11:55:38 历史现象只读关联：11:52:12 有前端 reload／HMR 通知，11:54–11:56 无新增；11:54–11:57 资源／趋势 HTTP 200，System 在 11:55:06.956 和 11:55:11.472 有 refresh。没有 Safari 实际应用重载、AuthContext 或主机事实响应，无法唯一归因。收据 `/tmp/addp-host-viewport-renewal-20261104.json`。

受控 T3 经真实 store.refreshAccessToken 暂缓夹具 AuthContext；局部视口与非零滚动在权限未知时清空、停止新请求，同身份释放后重新查询，视口与滚动仍复位。隔离单项及相关 38 项通过，原件 `/tmp/addp-host-{token-renewal-diagnostic,renewal-regression}.log`。这证明受控续期复位路径，未证明历史 Safari 事件由此引起。本批只增测试，视口恢复实现另见下一节；完整前端、实际 Safari 续期和 Hosted 未运行。

### 主机同身份续期后的临时视口恢复（2026-11-04）

同身份续期只暂存滚动、缩放与核对依据。权限等待仍清旧数据及取消请求；仅相同账号／平台上下文／委托／完整权限、同查询及同主机四项事实，且所有新读数合法、版本一致时，一次恢复临时位置。身份、权限、事实、查询或任何读取变化／失败、二次轮换与离页均丢弃。不保留凭据、旧数据或持久化状态。

Console 等待时隐藏同 iframe，以隐藏前可布局坐标为准。两个受控场景分别覆盖 Console 先恢复、模块先读完，均核验新数据和临时视口。旧恢复断言先失败；后续测试助手额外要求理想像素比例，引发 18 项失败，删除该要求后仍以实际图表模型／滑块和原 5 秒、1e-6 断言核对。

| 门禁 | 实际结果 |
| --- | --- |
| Monitor 完整入口首轮 | 135 单元通过、717／735 浏览器，18 项助手断言失败，退出 2，未到构建。 |
| 分批复验 | 8 个直接消费者 177 项通过，覆盖原 18 项失败。扣除 159 项重叠，累计 735 个不同场景有通过证据；首轮仍失败。 |
| Console 与构建 | 主机定向 30 项通过，助手修订后两个续期顺序再验通过；独立构建、登记 27＋21 通过，完整 Console 未运行。 |

原件 `/tmp/addp-host-renewal-*`，首轮失败目录 `addp-monitor-playwright-results-jCn5ht`。Safari 真实 vda 曲线及轴单位可见，用户激活后原生拖动成功；16:42:57→16:46:31 无额外输入，查询前移 214 秒，滚动及相对视口保持。既有 System 日志该段无 refresh，故只认自然资源刷新。人工摘要 `/tmp/addp-host-renewal-safari-20261104.json`。真实 Safari 凭据续期、全程无闪、正式 Hosted 仍未验收。

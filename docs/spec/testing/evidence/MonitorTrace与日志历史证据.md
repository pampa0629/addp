# Monitor Trace 与日志历史证据

[返回 Monitor 历史证据](Monitor历史证据.md) · [返回验收证据索引](../验收证据索引.md)

本页仅保留主题结论、实际验证范围、关键证据和后续待办。证据绑定所列固定源码或私有快照；本机实测、受控页面、Linux T2 与正式身份本机闭环分别认定，不表示当前 HEAD、Hosted T4 或生产发布已验收。

现行规则见[追踪与日志关联](../../addp平台运行观测规范.md#65-分布式追踪与日志关联二期第一批)、[Python 同步 HTTP](../../addp平台运行观测规范.md#python-同步-http-追踪接入第二批)及[执行短阶段与 Link](../../addp平台运行观测规范.md#执行短阶段与-trace-link第三批)。逐窗口准入、构建、资源轮询和临时文件清单不再逐次保留。

[SDK 与执行关联](#trace-sdk-conclusions) · [容量基线](#http-追踪隔离容量基线本地) · [正式身份本机闭环](#正式身份本地窗口总览) · [Linux 关键证据](#trace-linux-a21ee1f8-20261108) · [本机页面](#trace-personal-safari-postfix-20261109) · [Manager 部分通过与待办](#trace-manager-partial-conclusions)

<a id="trace-sdk-conclusions"></a>

<!-- 保留对应主题的旧链接。 -->
<a id="http-追踪二期第一批本地"></a><a id="http-追踪二期第二批python-本地"></a><a id="python-trace-接入后的开发依赖openapi-阻断修复"></a><a id="python-trace-与依赖修复的远端-ci-分项回执2026-10-23"></a><a id="执行短阶段与-trace-link二期第三批本地"></a>

## SDK、日志关联与执行短阶段

| 主题 | 已验证结论 | 实际边界 |
| --- | --- | --- |
| Go 同步 HTTP | 登记实例的 Server／Client、Gateway 外部上下文重建、内部传播及安全日志投影已有本地 T0–T3 和真实 Alloy→Tempo／Loki 证据；实际匹配的 Span 才形成调用边。 | 当前 User 逐请求核验实例，日志正文另需 System 权限；Trace ID 不代表授权，不从采样 Trace 倒算全量 QPS。 |
| Python 同步 HTTP | Common Python SDK／ASGI／HTTPX 接入及 Go→Copilot 夹具→System 五 Span、两条匹配调用边通过；停止采集端时业务仍 Ready，恢复后新链可查。 | SDK 夹具不替代完整 Agent／Copilot 产品验收；不覆盖 Python 异步执行、真实模型调用、SQL／数据库 Span 或所有 Runtime。 |
| 执行短阶段与 Link | Orchestrator→Develop bounded 的入队、领取、派发、终态使用独立短阶段及持久因果引用；失败阶段、参与实例与 Link 目标重新授权已有本地和 Linux T2 覆盖。 | Link 不补造 HTTP 调用边，不递归拉取；未保留目标可返回有界未找到。execution 才是任务耗时事实，未覆盖 continuous 和全部执行类型。 |

依赖／OpenAPI 阻断已由统一准备入口修复，本机重启后依赖指纹、应用导入、健康和 OpenAPI 投影核对通过。初次 SDK、夹具和跨模块导航失败在修正后取得范围内复验，不重复列为待办；历史 Swagger 超时、Docker／Gateway 间歇故障的唯一根因未确认，后续通过不能倒推已根除。

## HTTP 追踪隔离容量基线（本地）

**结论**：2026-10-21 私有冻结源码 `k0pjymvv/source` 的 `make test-monitor-traces MONITOR_TRACES_TEST_ARGS=--capacity` 完整通过。原始报告为 `addp-trace-capacity-16a165520915484dac28ef86f7e0637e.json`；该私有源码标识不是 Git 固定 SHA。三 Go 进程、五 Span HTTP 链和单节点存储保持原 120 秒尾采样、自然五分钟切块及 72 小时／20 GiB 配置。

| 档次 | 实际负载 | 一次二十条完整详情查询 |
| --- | --- | --- |
| 50 请求／秒 | 约 600 秒、30000 请求，无漏发、传输错误或非预期响应 | 4.68848046 秒；每条五 Span、两条匹配边，无 Partial |
| 70 请求／秒 | 约 600 秒、42000 请求，无漏发、传输错误或非预期响应 | 1.970312333 秒；同样完整二十条 |

单位物理成本取两档较大值 226.8298659 字节／Span，按至少 100 Span／秒、72 小时和两份完整副本外推 **10.9513 GiB**，低于 16 GiB 判定线。两档队列／丢弃／导出失败为零，退出自有容器、网络和卷零残留。此前单档查询超时及中止轮均不计通过；一次自动清理遗漏发生器，后经归属核验清零，最终完整轮独立验证清理通过。

**限制**：外推不是真实 72 小时留存、过期物理删除、长期合并峰值或生产 HA 的验证，也不证明 Python 容量。第一档距五秒上限仅约 0.31 秒，不能承诺延迟 SLA；共享主机存在其他任务重叠，不能称独占宿主基准，容器硬配额不代表宿主总用量。

<a id="正式身份本地窗口总览"></a>
<a id="正式身份本地第二窗口卷采样超时与-minio-构建触线2026-10-23"></a>
<a id="minio-产品镜像核验复用入口准备2026-10-23"></a>

<!-- 保留对应主题的旧链接。 -->
<a id="正式身份的本机执行-trace日志闭环2026-10-23"></a><a id="正式身份本地窗口的共同口径"></a><a id="正式身份本地第三窗口镜像核验守卫修复与部署配置拒绝2026-10-23"></a><a id="正式身份本地第四窗口依赖核验通过完整闭环未验收2026-10-24"></a><a id="正式身份本地第五窗口标准数据库初始化修正页面部署依赖仍阻断2026-10-24"></a><a id="正式身份本地第六窗口按需页面及正式登录通过执行闭环仍未验收2026-10-24"></a><a id="正式身份本地第七窗口准备守卫修正及iam权限冲突闭环未通过2026-10-24"></a><a id="正式身份本地第八窗口真实mfa与准备角色通过成功查询被保护链拒绝2026-10-24"></a><a id="正式身份本地第九窗口构建失败资源守卫停止与源码准入阻断2026-10-24"></a><a id="system依赖准入恢复与gateway单项构建复现2026-10-24"></a><a id="正式身份本地第十窗口执行trace日志与真实页面闭环通过2026-10-25"></a>

## 正式身份本机闭环

**最终结论**：第十窗口第二次尝试通过本机隔离的执行→Trace→日志→真实页面闭环，`formal_runner_t4=false`。早期窗口的准备／构建／资源守卫阻断不算业务通过，也不因后续成功改判。

| 尝试 | 固定源码与实际运行 | 结论 |
| --- | --- | --- |
| 第十窗口第 1 次 | `7b3a5d6cac630ac76edbfdebda61e2ee89f674cf`；`addp-trace-local-54dc65c1ba754a9fb05ce5a1ec0ca2e4` | 容器详情检查 4.070 秒触发守卫，身份与业务未执行。 |
| 第十窗口第 2 次 | `47b02db01342a049c4fe01ae1f3ee15369b04863`；`addp-trace-local-4eb709c8ced44689a5dd7fdb2884ea58` | 六后端运行 SHA 一致；12 项后端及 6 项真实页面断言通过。 |

- **身份与业务**：System-owned 夹具、正式密码／TOTP／IAM、普通执行用户原 11 项权限；合成引擎上 Orchestrator→Develop 的成功和受控失败父子执行通过，公开 DTO 无私有 Trace 泄漏。
- **Trace 与页面**：Platform 系统管理员读取 16 条 Owner 检查点；原等待 125 秒后查得失败终态 Span、两个持久 Link 及精确日志。Chromium Console／Monitor／System 实际页面读取同一结果。
- **拒绝、未找到与清理**：普通 Tenant User 和 Platform 安全管理员读诊断实际 403，任务删除后重读 404，正式退出后旧 Token 401。点击 Link 实际为 404、`link_result=not_found`，验证了授权和精确窗口，**没有读出未保留的目标 Span**。两次标准清理及独立审计最终零残留。

旧入口对应的两项准备结论仍保留：第二窗口两次分别止于卷采样超时和 MinIO 冷构建触线，业务未进入、退出清理通过；随后固定 `5681227a0a86f02cb4f5a9c72c49284a4afe8425` 的镜像复用准备仅通过确定性验证，当批实际二进制核验和正式身份闭环均为 `not_run`，不能借第十窗口结果回填为已运行。

上述闭环未覆盖 Hosted／专用 Runner T4、T5、其他 Runtime／continuous／Python 异步、SQL Span、全调用链留存或真实长期保留。

<a id="trace-linux-a21ee1f8-20261108"></a>

<!-- 保留对应主题的旧链接。 -->
<a id="python-trace-certificate-fixture-20261102"></a><a id="2026-11-02-common-python-trace证书夹具修正本地及自然linux通过"></a><a id="2026-11-08-固定提交-a21ee1f8b-的自然-linux-trace-实链通过"></a>

## Linux 关键证据

| 固定源码 | 实际作业 | 已验证范围与限制 |
| --- | --- | --- |
| `c05c7048080af98d968a0e6e31437794bdb430b6` | [39778225752／Trace T2 Job 119877525491](https://github.com/pampa0629/addp/actions/runs/39778225752/job/119877525491) | Python 接入后的真实 Trace T2 通过，保留原 120 秒尾采样，退出零残留；父 workflow 的其他失败不改写本 Job 结果。 |
| `673cf8eef7cb776de26a0b04c8dbdc86384becbf` | [39803050868／Platform consistency Job 119908856837](https://github.com/pampa0629/addp/actions/runs/39803050868/job/119908856837) | 统一依赖准备、生命周期与 Swagger 等完整 Platform 检查通过；同 Run 的 Python／Agent／Copilot 为路径未选中，不能算重测通过。 |
| `a21ee1f8b16304b073e730a51cac8c7e2d799dcd` | [42446113613／Trace T2 Job 128625414188](https://github.com/pampa0629/addp/actions/runs/42446113613/job/128625414188) | 实链 327.44 秒通过：Go→Python→Go 五 Span／两边、安全投影、历史参与实例与当前 User 撤权、mTLS／只读代理、采集停止恢复及 Loki 精确关联；短阶段、持久 Link 与目标撤权通过。容器、网络、卷和临时设施零残留。 |

最新 T2 仍真实等待 120 秒，观察普通链 7/200 被保留，**不是 5% 的统计证明**。子进程 helper 的设计 Skip 不计业务通过。同提交 Platform 在 Node 前置失败，`test-platform` 未执行；本地 HTTP／Trace runner 结果不能补成远端 Platform 通过。本表不证明正式产品页面、Hosted、容量或长期留存。

<a id="trace-personal-safari-postfix-20261109"></a>

<!-- 保留对应主题的旧链接。 -->
<a id="trace-development-preparation-and-ui-20261108"></a><a id="2026-11-08-本机-trace-接入准备复用及-503-页面修正本地"></a><a id="trace-personal-materials-prepared-20261108"></a><a id="2026-11-08-本机实际-trace-部署材料准备通过尚未启用"></a><a id="trace-personal-activation-readonly-20261108"></a><a id="2026-11-08-本机-trace-启用前只读核对实际应用待确认"></a><a id="trace-personal-env-applied-await-user-start-20261108"></a><a id="2026-11-08-本机七项-trace-配置合并等待用户标准启用与重启"></a><a id="trace-personal-poststart-host-ports-missing-20261108"></a><a id="2026-11-08-用户标准启用重启后只读核验发现-trace-宿主端口缺失"></a><a id="trace-personal-infra-loopback-restart-fix-20261109"></a><a id="2026-11-09-trace-回环发布与开发重启预检修正"></a><a id="trace-personal-postfix-sample-20261109"></a><a id="2026-11-09-本机-trace-真实采集与集中日志关联通过授权页面待验"></a><a id="trace-personal-safari-fosite-scope-20261109"></a><a id="2026-11-09-safari-正式管理员页面通过准入system-调用链明细批次失败"></a><a id="trace-go-owned-scope-filter-20261109"></a><a id="2026-11-09-共享-go-追踪入口阻止第三方-span-混入"></a><a id="2026-11-09-重启后-safari-调用链列表详情及关联日志通过"></a><a id="2026-11-10-safari-调用链定位摘要与关联日志"></a><a id="2026-11-11-双摘要专项-console-后续自然-ci-补证"></a><a id="2026-11-12-当前页调用链样本状态本地确定性验证通过真实验收待确认"></a><a id="2026-11-12-当前页样本状态与实例翻页固定提交自然-ci-通过"></a><a id="2026-11-12-safari-当前页样本标识翻页与同窗实例查询通过"></a><a id="2026-11-12-行内查看调用链保留上方上下文"></a><a id="2026-11-12-详情保留上方内容固定提交自然-ci-补证"></a>

## 本机真实来源与页面结论

- **来源与日志**：本机真实采集、查询 mTLS 和 Loki 精确关联通过；11 月 9 日 Safari 正式管理员在重启后的 System 新实例读取 7 Span／3 条匹配边，跳转同一实例、Trace 及窄窗口的 System 日志成功。范围为单条已保留样本，不宣称采集完整。
- **历史首败与修复**：此前 System 页面 503 的候选详情包含 Fosite 自建 scope；共享 Go 入队前限制正式 scope 的反例与回归通过，重启后新实例页面复验通过。已存混合 Trace 沿原保留策略过期，不放宽 Monitor 解码或权限校验。
- **定位与摘要**：11 月 10 日 Safari 的已有 HTTP 200／503 Trace 定位、详情返回和日志跳转通过。503 样本期间曾空白、重载后才完成日志读取，保留此限制；样本不提供独立根因诊断或完整采集证明。
- **当前页与上下文**：11 月 12 日 Safari 运行源码 `00846a26dfd2c7a96477dd315e4740baaaadd18f`，实例第 2 页、固定 15 分钟窗口、Monitor 有样本／Quality 未发现和返回恢复通过；行内详情保留上方内容已实操。像素级几何、重复请求、撤权及迟到响应等负例只按受控 T3 证据认定，不外推 Safari 全覆盖。

| 固定源码 | 关键门禁 | 证据边界 |
| --- | --- | --- |
| `3833ce6f854069d90c71ccf620b8ed239cbcf2e2` | [42839828422／Console Job 129911770284](https://github.com/pampa0629/addp/actions/runs/42839828422/job/129911770284) | 双摘要相关 7 项输入未变，Console **340 项浏览器及构建通过**。先前 Console 作业被取消，不算通过；Monitor 分项另有通过记录，不宣称全 workflow 通过。 |
| `39f89cafebe8ea3897e92d629cf9bf5033aacd8b` | [43155635597](https://github.com/pampa0629/addp/actions/runs/43155635597) 的 Go、Console、Monitor 实际 Job | 当前页样本与翻页：Console 348 项浏览器／构建、Monitor 149 单元／804 浏览器／构建通过；与上述真实 Safari 分别记账。 |
| `f0a7df82f7dda6c952b0b7fe33d40b4384fef0da` | [43233404697／Monitor Job 131234329424](https://github.com/pampa0629/addp/actions/runs/43233404697/job/131234329424) | 完整 Monitor 前端 152 项 T1、823 项浏览器及构建通过，包含未保留 Trace 的日志入口；不代表 Console 全量、全部 CI 或 Hosted。 |

<a id="trace-manager-partial-conclusions"></a>

<!-- 保留对应主题的旧链接。 -->
<a id="2026-11-12-manager-trace-追加准备分项确定性验证完成整入口重跑有前置失败"></a><a id="2026-11-12-manager-trace-追加准备固定提交自然-ci-补证"></a><a id="2026-11-12-manager-本机来源准备与单项配置合并完成待用户重启"></a><a id="2026-11-12-manager-重启后实际预览与-safari-关联核验部分通过"></a><a id="2026-11-12-未保留-trace-的实例日志入口确定性验证通过"></a><a id="2026-11-12-未保留-trace-的实例日志入口实际-safari-复验通过"></a><a id="2026-11-13-未保留-trace-的实例日志入口固定提交自然-linux-ci-补证"></a>

## Manager 部分通过与后续待办

**已通过**：实际预览由内置浏览器的已有 Tenant 业务身份发起；平台系统管理员在 Safari 核验 Trace／日志，未以平台权限替代 `manager.data_item.read`。预览 HTTP 200、113ms，Trace `34f68d0025d2b21b07e1309dc0606bb7` 的 1 条 Manager 完成日志和 3 条 System 授权／引擎读取日志实际关联。

**尚未验证完整链路**：等待超过 120 秒后，该预览仍无可读已保留 Span，页面为“暂无法确认”。没有逐 Trace 采样决定回执，5% 采样未命中只是可能原因，不能唯一归因。另一真实引擎列表 Trace `95b4fe479f3a5a54ad6ad4411a9dc527` 的五 Span、Gateway→Manager→System 父子关系及双向日志跳转通过，**不能替代预览 Span 验收**。

**无保留链时的日志入口**：精确 `trace_not_found`、已授权实例和独立日志正文权限满足时，沿原冻结窗口进入 System 实例日志。实际 Safari 复用上述原预览及两分钟窗口，读到相同 Trace／Span 的 HTTP 200、113ms 完成日志；没有重放业务或扩大窗口。401／403／503、无实例、身份变化和迟到响应等清除边界由确定性／受控页面及表中完整 Linux 门禁覆盖。日志页面仍为“采集完整性未确认”，成功日志不证明缺失 Span 已保留。

**后续待办**

1. 先讨论低请求量下可靠取得诊断样本的最小方案，再补 Manager 原预览的已保留 Span 证据；不能靠重复点击、重试或擅自提高采样率抹去本次缺口。引擎业务 Client 的已知 Context 断点、Meta Server／SQL Span 亦未由这些样本验证。
2. 正式 Hosted／专用 Runner T4、生产 T5、其他 Runtime／continuous／Python 异步和真实长期留存／删除按独立范围补证；已有本机或 T2 成果不自动升级层级，也不自动要求重跑。
3. 若继续定位历史 Docker／Gateway、Swagger 超时等未明故障，需取得同次实际运行证据；不因后续通过关闭未知根因。

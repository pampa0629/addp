# Monitor HTTP 质量与采样历史证据

[返回 Monitor 历史证据](Monitor历史证据.md) · [返回验收证据索引](../验收证据索引.md)

本页仅保留主题结论、实际验证范围、关键证据和后续待办。结论绑定记录中的固定源码与层级，不表示当前 HEAD 已重跑；已通过的 SDK／Linux T2 不替代正式产品页面、Hosted 或容量验收。

现行要求见[HTTP 接口质量规范](../../addp平台运行观测规范.md#64-http-接口质量分阶段接入)及[正式产品本机隔离验收](../../addp平台运行观测规范.md#正式-python-后端与接口质量页面的本机隔离验收)。逐次准备、构建、排队、轮询、下载摘要及临时归档流水已删除。

[来源与公共 mTLS](#http-history-sources) · [采样与编译协调](#http-history-sampling) · [Linux 关键证据](#http-history-ci-polls) · [正式产品与待办](#http-history-product-windows) · [停源历史趋势](#停源实例-http-历史趋势首期)

<a id="http-history-sources"></a>

## 本地一期、公共 mTLS 与 SDK 来源

| 主题 | 已验证结论 | 范围与限制 |
| --- | --- | --- |
| Go HTTP 本地一期 | Router、HTTP 配置／CAS、冻结恢复、租约、权限及受控页面等分项通过；真实 SDK 查询、自然窗口、告警通知及同一事件恢复已有专项证据。 | 产品源码 `3ee46b77fd84fe1a915874a11e0e97b843cac8bd`；该期未覆盖 Python、正式密码／MFA、Trace／日志关联、拓扑、Hosted 或 T5。 |
| 公共 mTLS 前置 | 容器 tmpfs 原子换证后，localhost 与实际 IP 的叶子证书匹配、严格 TLS 握手、固定窗口公式及真实自然采样通过，退出清理零残留。 | 运行 HEAD `1c4ec8df446f427da372a296646967879c486d6a`，换证实现 `bd2ab54ef0662b09f64aa9bc7b4f59424f749100`；仅公共前置，没有进入 Go／Python 业务请求或告警。 |
| Python HTTP SDK 来源 | 统一 ASGI 生命周期、独立于 Trace 的 HTTP 指标、各自正式框架直接声明及原生扩展加载已有本地与 Linux 实链验证。 | 实现 `0eca81f9b41c26b4f40375fa661067def49d56f2`；Linux 运行的是两个 ASGI SDK 夹具，不是完整 Agent／Copilot 产品 backend。只发布身份、启动时间和 HTTP 指标，不据此声明 CPU／RSS／cgroup 来源。 |

公共采样、来源导入和清理交接的早期失败已取得后续范围内通过证据，不重复列为待办。历史 TLS 失败的唯一根因未由当次证书和日志完整证明，不能用后续换证通过倒推原始根因。

<a id="http-history-sampling"></a>

## 私有目录采样与 Go 编译协调

- **采样与清理交接**：当次私有目录的互斥与清理标记已有确定性验证和 Linux 整门禁通过证据；锁等待计入原期限，清理与业务结果分别认定。
- **编译协调**：Go 四阶段编译纳入同一受管内核，实际暂停／恢复与资源采样已有 Linux 和本机检查点证据。编译阶段检查点的本机私有验证提交 `88b27a2d6a352a3868ae58bc39e7b4d435c77500` 完成 48 次真实暂停／恢复、标准退出及零残留清理；仅该编译专项通过，不扩大为正式产品 full 通过。
- **预算边界**：对应 Linux 证据保持完整资源检查四秒、原整体时间及硬配额／私有可写限制；配额上界不是实际 CPU／RSS 用量，也不是容量基线。当前执行规则统一查正式规范。
- **已知限制**：历史目录消失的实际删除者未确认；曾取得真实 D／T 快照，但不足以确认成员角色和阻塞根因。后续成功轮未触发持续 D 的有界等待观察分支，不证明该历史故障已经根除。

### 离线归属复核

历史代理日志仅能形成 `unique_candidate`。缺少独立校时、该版 apiproxy 时间戳生成点及同一请求的成对边界，不能把客户端、转发和 daemon 的分段耗时判为已确认。原失败仍为 `inspect_owned_containers` 中的 `resource_sample_deadline_exceeded`；本地归属分支通过只属于确定性证据。

<a id="http-history-ci-polls"></a>

<a id="http-natural-four-stage-budget-passed-20261104"></a>

## 自然 Linux 结论与关键证据

| 固定源码 | 实际作业 | 结论 |
| --- | --- | --- |
| `fa7ae6dc34af264fb14c31c48b048fc26514efb4` | [40468624807／Job 122070268771](https://github.com/pampa0629/addp/actions/runs/40468624807/job/122070268771) | 采样／清理交接修正后整项 HTTP T2 通过，覆盖 Go 与两 Python SDK 来源、查询／趋势、告警签名重试、同一事件恢复及停源隔离；业务、预算与清理分别通过。 |
| `fca35fa31adf1376e0415a040266400fb5affe0c` | [41816593195／Job 126544907993](https://github.com/pampa0629/addp/actions/runs/41816593195/job/126544907993) | 四阶段编译、采样、Go／Python 功能及清理通过；短查询阶段没有暂停样本，不补造覆盖。 |
| `a21ee1f8b16304b073e730a51cac8c7e2d799dcd` | [42446113613／Job 128625414275](https://github.com/pampa0629/addp/actions/runs/42446113613/job/128625414275) · [预算归档 13603723953](https://github.com/pampa0629/addp/actions/runs/42446113613/artifacts/13603723953) | 本页最新独立核对的 Linux HTTP T2 通过：Go 与两 Python SDK 来源、自然窗口、告警打开／签名重试／同一事件恢复、停源前历史读取及来源隔离均完成；四阶段、输入一致性与零残留清理通过。 |

最新记录在原 1200 秒内完成，245 次请求未超过原阶段及总额，208 次完整资源检查均小于四秒，硬配额及私有可写上界未触线。官方归档与固定源码输入已核验，结论不借当前工作区或父 workflow 的颜色扩张。

同提交的 [Trace 实链](MonitorTrace与日志历史证据.md#trace-linux-a21ee1f8-20261108)另行认定。同提交 Platform consistency 在 Node 前置失败、`make test-platform` 被跳过，因此不能把本地 HTTP／Trace runner 或整体 Platform 记为远端通过；原 HTTP 53 项／Trace 208 项结果保持本地层级。重复排队、运行中快照和逐次人工轮询不再保留。

<a id="http-history-product-windows"></a>

## 正式产品窗口、准入与待办

**当前结论**：正式 Agent／Copilot 产品到 Console／Monitor 的完整 HTTP 页面链仍未验收。历史本机 full 窗口多次止于投影、镜像／软件包下载、编译协调或资源检查，不能把某个镜像完成、清理通过或 SDK T2 通过记为产品闭环成功。额外准备检查中止且未启动正式入口的记录仅算未运行。

| 已完成的补充验证 | 不能外推的范围 |
| --- | --- |
| 页面断言、两阶段注销计数、旧凭证 401、输入和敏感信息保护已有确定性／受控页面验证。 | 未证明真实产品浏览器登录、注销和会话撤销通过。 |
| APT 阶段分类、命令超时、卷测量、等待回执和失败清理的本地门禁通过，部分真实编译检查点已运行。 | 不能据未复发或局部检查点通过确认历史根因、长期稳定性或完整产品窗口。 |
| 编译启动交接有界回执的本地 HTTP 53 项／Trace 208 项及既有身份、登记检查通过。 | 新增回执的真实启动取证、暂停后并行测量性能仍待验证。 |

**关键未完成证据**

- 2026-11-07 正式 full：私有快照 `368e96296550c2b968d254776ea3207c9301e8cd`，Run `addp-http-local-b4adab26b1be431f9f977bf34c13b95b`。编译阶段累计资源测量截止，入口退出 2、清理零残留；产品镜像及身份页面未进入。
- 2026-11-07 并行测量专项：私有快照 `b40fca70f70ad0a4eed4a11ac161cef3c32089bd`，Run `addp-http-local-81ebc6b4f559403cb507be3886bfed9e`。止于启动锁等待，未启动并行测量、没有完整暂停恢复样本；退出清理零残留。

**后续待办**

1. 按正式规范，在单独确认的产品窗口补完整 backend、密码／MFA、真实采集／API、Console／Monitor 页面、停源，以及两阶段注销 204 和旧会话 AuthContext 401；业务与退出清理分别验证。
2. 补新增启动交接回执和暂停后并行测量的真实证据。此前实测止于启动锁等待，不把回执投影或受控门禁当作性能验证。
3. 若继续定位目录消失或停止确认超时，须取得同次归属、等待角色与时钟边界；持续 D 未触发、历史删除者和唯一根因未明的限制继续保留，不因成功样本关闭该问题。

Hosted、容量、长期保留和 T5 均按其独立范围验收；上述未覆盖项不自动表示能力未实现，也不自动要求重跑已有专项。原预算、权限、失败即停及清理边界由正式规范维护。

## 停源实例 HTTP 历史趋势首期

**结论**：支持按原实例和明确的离线前绝对区间读取历史，状态保持 `not_active`，历史可用性单列；当前用户权限、唯一采集序列与进程启动时刻分别核验。实时摘要、排行和告警仍沿实时口径。

**关键证据**：固定源码 `3de409013281be94adf19387189196d6c05df6f2` 的 [42051463127／Job 127363429153](https://github.com/pampa0629/addp/actions/runs/42051463127/job/127363429153)通过 Go 和首个 Python SDK 来源的真实 Prometheus 停源历史回读、实时空观测与另一来源隔离；业务、预算及零残留清理分别核验。后续 `a21ee1f8b` 的 Linux HTTP T2 再次通过相关停源断言。

**边界**：接口质量专项 T3 和构建通过，原全量 Monitor 浏览器被主动中止，不能记整套通过；父 workflow 失败也不改写已执行 Job 的分项结果。正式产品的真实身份页面仍属上节待办。现行规则见[停源历史契约](../../addp平台运行观测规范.md#停源实例的-http-历史趋势)。

<!-- 保留稳定旧片段，定位至本页结论与待办。 -->
<a id="formal-python-http-local-20261028-cache-reclaim"></a><a id="formal-python-http-local-20261028-image-auth"></a><a id="formal-python-http-local-20261028-resource-list-deadline"></a><a id="go-cache-reclaim-deterministic-20261028"></a><a id="http-agent-apt-bounded-diagnostic-20261101"></a><a id="http-apt-transport-diagnostic-local-20261102"></a><a id="http-apt-transport-natural-linux-20261103"></a><a id="http-bounded-wait-evidence-20261101"></a><a id="http-bounded-wait-linux-success-086-20261101"></a><a id="http-browser-logout-contract-20261104"></a><a id="http-checkpoint-follow-clauses-20261103"></a><a id="http-compile-checkpoints-local-20261103"></a><a id="http-compile-parallel-admission-lock-20261107"></a><a id="http-compile-pause-reserve-checkpoints-passed-20261106"></a><a id="http-compile-pause-reserve-runtime-20261106"></a><a id="http-compile-pause-runtime-volume-list-deadline-20261106"></a><a id="http-compile-pause-sampler-admission-timeout-20261106"></a><a id="http-compile-start-handoff-deterministic-20261108"></a><a id="http-confirmation-followup-manual-20261030"></a><a id="http-confirmation-followup-manual-20261031"></a><a id="http-formal-admission-runner-linux-20261101"></a><a id="http-formal-page-oracles-20261031"></a><a id="http-formal-page-window-projection-failed-20261031"></a><a id="http-formal-product-window-agent-apt-failed-20261101"></a><a id="http-formal-product-window-copilot-apt-failed-20261102"></a><a id="http-formal-product-window-resource-timeout-20261103"></a><a id="http-full-agent-build-deadline-20261104"></a><a id="http-full-agent-timeout-over-budget-20261104"></a><a id="http-full-apt-candidates-private-measurement-deadline-20261107"></a><a id="http-full-compile-inspect-command-failed-20261106"></a><a id="http-full-compile-inspect-resume-deadline-20261106"></a><a id="http-full-compile-start-agent-apt-connect-20261107"></a><a id="http-full-frozen-compile-resume-deadline-20261106"></a><a id="http-full-logout-agent-volume-read-failed-20261105"></a><a id="http-full-monitor-apt-index-failed-20261103"></a><a id="http-full-resume-snapshot-agent-volume-deadline-20261107"></a><a id="http-full-stream-resource-deadline-20261104"></a><a id="http-full-system-list-inspect-deadline-20261105"></a><a id="http-full-volume-snapshot-compile-state-changed-20261107"></a><a id="http-incremental-build-progress-20261104"></a><a id="http-linux-a21ee1f8-20261108"></a><a id="http-linux-ci-20261028-private-directory-recurrence"></a><a id="http-linux-followup-manual-20261029"></a><a id="http-linux-followup-second-manual-20261029"></a><a id="http-linux-followup-third-manual-20261029"></a><a id="http-linux-gotmp-recurrence-20261030"></a><a id="http-linux-minimal-forensics-20261029"></a><a id="http-linux-resource-windows-managed-20261104"></a><a id="http-monitor-apt-signature-diagnostics-20261103"></a><a id="http-natural-linux-budget-passed-20261104"></a><a id="http-preparation-directory-categories-20261103"></a><a id="http-private-openssl-config-20261031"></a><a id="http-process-source-gotmp-recurrence-20261030"></a><a id="http-product-timeout-bounded-diagnostics-20261104"></a><a id="http-remaining-completed-20261031"></a><a id="http-resource-wait-poll-deterministic-20261104"></a><a id="http-stable-read-owned-go-20261030"></a><a id="http-stop-confirmation-real-dt-20261101"></a><a id="http-stop-confirmation-recurrence-20261030"></a><a id="http-three-compile-stages-20261030"></a><a id="http-three-stage-followup-manual-20261030"></a><a id="http-trace-apt-natural-linux-1d15-20261102"></a><a id="http-volume-checkpoints-local-20261103"></a><a id="http-volume-checkpoints-natural-linux-20261103"></a><a id="http-window-fixture-outer-receipt-20261104"></a><a id="正式产品窗口准入与失败诊断"></a><a id="自然-linux-轮询与归档复核"></a>

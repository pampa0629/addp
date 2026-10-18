# Monitor 历史验收证据

[返回证据索引](../验收证据索引.md) · [Monitor 模块契约](../../../../monitor/CLAUDE.md)

以下迁移自模块说明的历史记录，仅适用于记录所指源码、运行尝试与覆盖范围。本次整理没有重跑这些 CI，也未查询其最新状态；失败、排队、分项与整轮通过分别保存，不推导当前 HEAD 或生产 T5 已通过。

## 节点、实例与资源告警

| 历史范围 | 源码 | 原记录 | 已记录结果与限制 |
| --- | --- | --- | --- |
| 节点来源首次接入 | `原文未单列 SHA` | [37463003308](https://github.com/pampa0629/addp/actions/runs/37463003308) | T4 通过：2026-10-06 Linux VM 原生来源、正式身份与控制面；不代表物理宿主或生产 T5。 |
| 基础资源读取 | `794450a30` | [37485747576](https://github.com/pampa0629/addp/actions/runs/37485747576) | T4 通过：八项即时/趋势、预算 CAS/热生效、权限隔离、中心故障/新样本、停用历史排除和零残留。 |
| 节点资源正式页面 | `b3a64756c` | [37563564755](https://github.com/pampa0629/addp/actions/runs/37563564755) | T4 通过：2026-10-07 密码/MFA、单 iframe、八项资源/趋势、安全管理员拒绝、故障恢复和零残留；不含该次后的自动刷新。 |
| 节点详情自动刷新 | `1ce34d770` | [37566181124](https://github.com/pampa0629/addp/actions/runs/37566181124) | T4 及同提交 Platform CI 通过：自然 15 秒、服务端 end 前进、URL 不变、关闭恢复、16 秒无定时请求；隐藏/慢请求/撤权由 T3 单独覆盖。 |
| CPU 一分钟忙碌率 | `5bb4173cf` | [37573777193](https://github.com/pampa0629/addp/actions/runs/37573777193)、[37573754535](https://github.com/pampa0629/addp/actions/runs/37573754535) | Hosted T4 / Platform CI 通过：九项资源范围、完整窗口与核集合、节点/目标恢复、停用历史及清理；26 项 runner 回归。此前凭据过期与旧异步断言失败不计通过，CPU 单独预热/恢复零值仅 T3。 |
| 文件系统字节容量 | `08556c553` | [37602308231](https://github.com/pampa0629/addp/actions/runs/37602308231)、[37602281641](https://github.com/pampa0629/addp/actions/runs/37602281641)、[37602281771](https://github.com/pampa0629/addp/actions/runs/37602281771) | Hosted T4、Platform CI、Release/T2 通过：六挂载/30 序列、同次采样公式、精确 URL/趋势、恢复新样本、停用历史与零残留；本地对应门禁及 27 项 runner 已记录通过。 |
| 文件系统 inode | `86853c844` | [37610515534](https://github.com/pampa0629/addp/actions/runs/37610515534)、[37610486715](https://github.com/pampa0629/addp/actions/runs/37610486715) | Hosted T4 / Platform CI 通过：六挂载/24 序列，五挂载有效、EFI 明确空值，URL/趋势、恢复及零残留。首次冗余赋值断言失败后修正；本地 22 inode/18 字节/15 CPU 场景、43 单元/53 浏览器及指标/PostgreSQL 通过。 |
| 磁盘吞吐与单位 | `5c490bf5c` | [37740681212](https://github.com/pampa0629/addp/actions/runs/37740681212)、[37740647211](https://github.com/pampa0629/addp/actions/runs/37740647211) | Hosted T4 / Platform CI 通过：真实 sda 读写、精确趋势、IEC/经过时长/负载数量、MFA/URL、故障恢复及零残留；六截图无敏感归档。本地 32 runner、150 Console 单元/119 浏览器、构建和平台门禁通过。 |
| 网络接口吞吐 | `193c0e82e` | [37791100970](https://github.com/pampa0629/addp/actions/runs/37791100970)、[37791001755](https://github.com/pampa0629/addp/actions/runs/37791001755)、[37791001784](https://github.com/pampa0629/addp/actions/runs/37791001784) | Hosted T4、Platform CI、Monitor PostgreSQL/metrics 通过：16 接口/32 序列，两趋势各 21 点，84 响应/七截图、故障恢复/停用历史及零残留；36 归档无敏感模式。原模块入口曾在两个五秒超时退出，宿主 39 项及平台独立复跑通过，不抹去首轮失败。 |
| 磁盘 IO 时间 | `839ef8338e；生产实现 88d7ade1d` | [37808051558](https://github.com/pampa0629/addp/actions/runs/37808051558)、[37808006295](https://github.com/pampa0629/addp/actions/runs/37808006295)、[37808006338](https://github.com/pampa0629/addp/actions/runs/37808006338)、[37803604603](https://github.com/pampa0629/addp/actions/runs/37803604603) | 最终标准 Monitor 模块门禁返回 0；21 IO 场景、完整/受限 T2 通过。生产实现的 PostgreSQL/metrics CI 通过，修正后路径跳过不计重跑；修正 Hosted T4 / Platform CI / Release 通过：25 项目录、102 响应、七截图、三条时间趋势、恢复及零残留。首轮 EFI inode 选样失败、先前 mTLS 超时和跨 Owner 预检失败保留。 |
| Desktop 七采集器个人应用 | `51d7a66bfb` | [37877201811](https://github.com/pampa0629/addp/actions/runs/37877201811)、[37877201781](https://github.com/pampa0629/addp/actions/runs/37877201781) | 原记录为 Platform CI / Release-T2 排队，未记通过。用户标准 up 已应用七采集器；18 设备、vda 五指标与约 13 秒新鲜样本的只读 mTLS 诊断不等于 Monitor 完整窗口/页面；Safari 会话过期，页面当时未验。 |
| 当前实例 CPU/RSS 趋势 | `原文未单列 SHA` | [38227755626](https://github.com/pampa0629/addp/actions/runs/38227755626) | Hosted T4 通过：当前实例两序列、启动后裁剪、URL、刷新恢复、来源/中心/控制故障、停用历史与零残留；个人 Safari 两曲线复核。上一轮故障断言未复现，根因未确证；旧单次通过不证明长期无故障。 |
| 服务自动刷新 | `7f365279b` | [38329665208](https://github.com/pampa0629/addp/actions/runs/38329665208) | Hosted T4 通过：自然定时/关闭恢复、25 项节点与进程协议、中心/来源/控制面恢复、停用/删除及零残留。38325976574 的 System CPU 缺窗口未复现、固定时刻诊断未触发，根因未确认，不记作故障修复。 |
| 主机列表自然刷新 | `4e9fe672f` | [38340694203](https://github.com/pampa0629/addp/actions/runs/38340694203) | Hosted T4 通过：自然刷新、关闭恢复、原故障链及零残留；前次导航 Test ended 未复现，根因未确认，安全耗时诊断保留。 |
| 资源总览与 TOP5 | `93b89ab33` | [38588001065](https://github.com/pampa0629/addp/actions/runs/38588001065) | 单真实主机 Hosted T4 / 同源码 Platform CI 34 项通过：聚合、预算、刷新/导航、中心/来源/控制故障与停采恢复、零残留。六个受控 mTLS 来源 Prometheus T2 的完整聚合、TOP5 同值/末项排序、缺失/有效零/停用/预算拒绝通过；不代表真实六主机。 |
| 相邻等长比较：缺样保护 | `68d7cbd03，包含 ac7522bf5` | [38983748074](https://github.com/pampa0629/addp/actions/runs/38983748074) | 完整 Hosted T4 通过：三次比较、来源/中心/控制面恢复、节点启停和目标停用/删除，零残留；比较分支为 0 项完整、3 项不完整，不当作完整样本比较通过。 |
| 相邻等长比较：完整样本 | `74adc2d45` | [39082438625](https://github.com/pampa0629/addp/actions/runs/39082438625) | 完整 Hosted T4 通过：CPU/内存各 41 自然有效点，三次五分钟比较完整；独立重算原始报告、刷新恢复及零残留。不扩展为长时段、进程比较、同比或 T5。 |
| 内存压力资源告警 | `8111363d2` | [39016166744](https://github.com/pampa0629/addp/actions/runs/39016166744) | 正式身份/Gateway 的 Hosted T4 通过：真实内存打开、缺样保持、恢复同一事件、HMAC 通知/自动重试及零残留；不表示生产满载或自动扩容。 |
| 受限 CPU 资源告警 | `53be389c5` | [39082008910](https://github.com/pampa0629/addp/actions/runs/39082008910) | Hosted T4 通过：固定测试阈值下触发、恢复、签名通知重试和零残留；不作为生产容量或 T5。 |

## continuous 诊断与 Owner 不可用

| 尝试 | 源码 | 已记录结果与边界 |
| --- | --- | --- |
| [38329160596](https://github.com/pampa0629/addp/actions/runs/38329160596) | `bfc305953` | 整轮 T4 失败：真实自动刷新、恢复来源和稳定码已验，但撤权只清事件、未清终态概览/步骤；Monitor-only 与后续 CDC 未运行。清理通过、残留 0。本地修正后前端 56 单元/118 浏览器及构建通过，401 用例改为会话失效导航且内容全清，没有放宽旧内容断言。 |
| [38337857450](https://github.com/pampa0629/addp/actions/runs/38337857450) | `b0765f802` | 整轮 T4 失败：live/recovery 与旧内容清空通过，新鲜 Monitor-only 提示断言失败；清理及 Infra 零残留。StatusAnnouncer 与 el-result 同文案造成 strict-mode 双匹配，修正只定位唯一可见标题，保留播报和无详情/步骤/事件/树断言。 |
| [38381655995](https://github.com/pampa0629/addp/actions/runs/38381655995) | `65de8a82938ba1bbdd56df6f7c000be7f67076cc`，包含 `ba2c99652` | 完整 Hosted T4 通过：live/recovery/revocation 三份报告及九组接口/Worker 检查，Worker 失联新 UUID、暂停/恢复、跨/同租户 Owner 权限、Owner 不可用、撤权与新鲜 Monitor-only 拒绝、CDC 事件、删定义后历史读取。五模块源码一致、37 归档无私有控制文件或访问令牌/JWT 模式，领域/Infra 零残留。修正的前端 59 单元/131 浏览器与构建、transfer runner 65 项通过；不含后续 Workflow/Notebook 监督器或 T5。 |
| [39026543398](https://github.com/pampa0629/addp/actions/runs/39026543398) | 原文未单列 SHA | 整轮失败：隔离入口漏启 Security，在 Owner 故障阶段之前退出；清理及 Infra 零残留，不计 Owner 故障通过。后续场景预检 Security，并只读等待独占部署的 Transfer projection initialized，不写保护事实或连接本地共享业务库。 |
| [39082349255](https://github.com/pampa0629/addp/actions/runs/39082349255) | `009ceea39` | Owner503 八项页面检查与同进程停止/恢复通过，三张截图、领域/Infra 清理通过；整轮仍失败，原因是把运行中 CDC 错误要求为 running 终态事件，后续 CDC/审批/调度未运行。修正要求 CDC 仍运行且无终态；终态执行仍恰有一个匹配终态事件。 |
| [39083559464](https://github.com/pampa0629/addp/actions/runs/39083559464) | `946c4d13e` | 完整隔离 Hosted T4 通过：14 业务检查、真实 Owner503 八项页面检查、撤权/Monitor-only、Kafka 恢复、CDC 审批与双向扫描隔离、调度当前主体、删定义后历史。相同 Owner PID 停止/恢复证明 true，归档 `12121475487` 的三张截图已目视核验；业务残留 0、cleanup passed、外层 Infra zero_residuals。保留产品历史，不外推 Notebook、其他 Owner 或 T5。 |

Owner503 场景使用正式普通 User 与真实 Monitor 响应；故障夹具仅停止当次隔离部署中经 pidfd、可执行文件、UID、数据库和私有目录核验的 Transfer Backend，再恢复同一进程。浏览器分别验证终态详情、运行树、非空列表的清空与重新裁决；独立网络中断仅影响事件读取，不冒充 Owner 503。控制文件只在当次私有目录，公开回执不包含 Token、业务样本或完整接口响应。标准入口仍为既有 transfer-continuous-events Hosted suite 与 `make test-transfer-continuous-online-runner`。

## 分项与未通过范围

- 2026-10-15 的 `make test-module MODULE=monitor GO_ONLY=1` 记录为 T1 通过。`TestExecutionReadAPIReauthenticatesThroughOwnerForEveryRequest` 使用真实 Monitor 路由/Owner HTTP Client/共享范围 Handler 和独立身份夹具，覆盖逐请求重认证、跨/同租户、无定义本人/缺主体、父子隔离、删定义历史、列表/统计、动态撤权和 Owner503/恢复；响应不含业务载荷。故意保留旧权限时应 404 实得 200，恢复后通过；不是 System IAM、PostgreSQL T2、完整模块或新增 T4。
- Desktop 来源的独占 metrics T2 与 Monitor PostgreSQL 分项记录为通过：实际七采集器、18 设备/七原始计数及独立命名空间、五趋势、故障后新样本和零残留。第三次模块运行的 T0/Go/47 单元/61 浏览器/构建通过，但后续 backend unavailable 导致整入口失败；个人开关的端口夹具已隔离并复验，十秒生命周期超时、约三秒查询失败与此前 mTLS 超时未确认根因。最终独占重跑通过不覆盖整轮失败；个人 mTLS 诊断不等于正式页面。
- 资源告警联合 `make test-monitor-metrics` T2 使用受控 mTLS、真实 Prometheus/PostgreSQL 和临时 TLS 签名接收端，覆盖 503 自动重试、缺样不恢复、低值恢复与 outbox；同一临时 Compose 只初始化 addp_test，退出清理。System 身份是夹具，不代替正式身份 T4 或真实群通知。
- 全部早期“目录九项/十八项/二十项/二十二项”仅表示各次验收切片，当前目录与 Desktop 七采集器以正式规范为准。历史六采集器、文件系统/inode/网卡空值记录不改写为最新功能。
- 各次默认跨 Owner test-changed 预检缺依赖均未计为通过。真实多主机 TOP5、独立 Owner 撤权、全平台容量、生产宿主/物理身份与 T5 仍需各自独立证据；不把单主机 T4、六个受控来源 T2 或历史 CI 排队合并为通过。

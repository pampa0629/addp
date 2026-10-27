# Monitor Trace 与日志历史证据

[返回 Monitor 历史证据](Monitor历史证据.md) · [返回验收证据索引](../验收证据索引.md)

本专题收录分布式追踪、Python 接入、容量基线与正式身份本地多窗口。固定源码、环境、失败记录、修复结论与未覆盖范围以各记录为准；本次仅整理文档，没有重跑验收或刷新 CI 状态，不推导当前 HEAD 或生产 T5 已通过。跨主题语境与原有顺序见 [Monitor 历史证据入口](Monitor历史证据.md#原记录入口)。

## HTTP 追踪二期第一批（本地）

2026-10-20，用户确认普通成功 5%、单次 Server 请求 ≥1 秒优先及 Tempo 72 小时初值，明确本轮只做本地验证。规范和 T0–T3 标准先修订；权限迁移 228、Monitor `monitor.trace.read`、当前 User 实例裁决及查询 API 分别已进入 `d2bbac317`、`13c87907e`，规范提交为 `dd9e10a26`。共享版本提交 `d5c39624f`、`857cf2b8e` 随后包含本批主体、收尾 Go 修正及其他会话工作；以下证据只覆盖本会话冻结输入，不能据此宣称整个版本或最新共享 HEAD 已验收。

本批覆盖已登记 Go 平台 backend/ingress 的同步 HTTP Server/Client：Gateway 重建外部 Trace/request ID，内部保留 Context、TLS、超时、取消、流式响应及 HTTP 101 双向流，外域重定向剥离关联头；不记录 URL/query、正文、凭据、SQL 或租户/用户身份。请求日志由唯一 Router 工厂生成安全 JSON，Context 中的 Trace/Span ID 沿原接收器、Alloy 和 Loki 结构化元数据关联。Monitor 只返回安全跨度与实际匹配的 Client/Server 调用边，离线实例也逐请求核验当前 User；日志读取另需 System 正文权限，Tenant 不开放平台 Trace。

策略版本 1 由 Monitor 唯一维护：生产端队列 2048，满时计数丢弃；真实等待 120 秒、10000 待决 Trace、100 Span/秒采样后目标预算；失败/慢/普通为有界优先级，不能承诺任意负载保留所有失败或 5% 普通请求。独立 Tempo bucket/账号、20 GiB bucket 配额及约 5 GiB 工作空间预留沿正式 Infra 入口；预留不构成文件系统硬配额。

- T1：仓库外固定源码基线 `28b145070` 仅叠加本方 SDK、全部 Context 直接消费者、唯一日志工厂及查询实现；最终 `make test-go` 的 22 个 Go 模块及各自 `go mod tidy -diff` 通过。另已执行 Common 完整 `GO_ONLY=1`，包括队列满与导出失败、隐私/外域传播、4xx 与失败分离、Context 日志、实际 HTTP 101 echo 及唯一共享传输检查。后续 Router 唯一所有权断言同时禁止别名 RequestID 中间件；新 Context 接口的生产实现、接口及测试替身保持一致。
- T2：最终 SDK 的 `make test-monitor-traces` 通过，`TestOwnedTracePipeline` 327.97 秒、包耗时 337.035 秒。三个真实原生 Go 进程经独占 TLS Alloy → Tempo/MinIO，完成真实 120 秒尾采样；实际观察失败、慢请求及 12/200 普通请求被保留，不能把小样本比例当作 5% 的统计证明。原窄时间窗可读取 5 个 Span、2 条匹配调用边；真实接收器 → Alloy → Loki 的精确 Trace 元数据、当前 User 历史实例读取/撤权、来源与查询证书互拒、只读代理路径/方法拒绝、采集端停止时业务仍 Ready 与失败计数、恢复后新 Trace 可查均通过。退出核验当次容器、网络、卷和门禁凭据/源目录零残留。未启用的子进程 helper 在父测试进程内按设计 Skip，三个实际受管子进程均运行并正常退出，不把 Skip 当验收证据。
- T2：独立 `make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--source-root <固定源码> --package migration --test platform-alert-management"` 通过，覆盖 227/228 权限、仅现有平台系统管理员获默认授权、刷新撤销及幂等重跑；使用标准测试数据库，不操作个人业务库。
- T3：最终 `make test-monitor-frontend` 为 81 单元/232 浏览器及构建，System 为 150/189 及构建，Console 为 186/195 及构建，全部通过；共享前端 241 单元通过。覆盖中英文、实际调用边与缺失边不推测、失败/无权限/撤权清空、迟到响应隔离、Trace → System 精确实例日志 → Trace 的唯一跨模块桥、长跨度一小时窗口及 URL 刷新恢复。其余模块的前端与其他会话后续改动不在此证据范围。

T0：最终 `make test-dev-lifecycle`、50 项 Infra 日志/指标/追踪生命周期、30 项 module-gate、Go 依赖规约、构建/前端与 T2/Online CI 登记均通过；共享前端另以 `make test-common-frontend` 通过。生命周期聚合早期失败来自停止命令 mock 未同步追踪 profile 与既有 PG 清理依赖，已补齐受控夹具、仓库外环境和空来源/独立 state，不变更实际停止权限或操作个人资源。

授权/Swagger 最终标准入口通过，覆盖全部 Go 与两项 Python OpenAPI；隔离快照补挂既有 Agent/Copilot venv 解释器，不安装依赖。曾有三个既有 Swagger shell 夹具在 15 秒原期限内超时；停止重编译后按原输入和期限复验通过，不能据此声称修复了未定位的历史超时根因。

根 Go 首轮发现本方 Context 迁移误触 Graph 创建及遗漏 Transfer 导出测试替身，已分别撤销无关调用参数、补齐真实接口签名后全量复验；旧失败不计通过。Console 初次误用同模块导航，导致 iframe 内进入无权限页且公开 URL 未变化，已改用现有 `openConsoleRoute` 跨模块桥并完整复验。追踪早期实际运行还证实 Tempo 默认 120 秒 WAL 时间容差不足以容纳 120 秒尾采样及入库延迟；现从唯一策略生成 300 秒容差，不降低慢阈值、采样等待或扩大用户查询窗。重启测试随机发布端口变化仅修复当次夹具，产品端点不自动改挂。

本轮未运行 Hosted/正式密码 MFA T4、生产安装发布 T5、72 小时自然过期等待或容量压测，未重启或接管个人服务。Python、异步 execution Links、SQL/数据库 Span、浏览器采集及引擎实例追踪不在本批；平台总体请求量仍由全量 HTTP 指标提供，不能从采样 Trace 倒算。下一步优先在一次性隔离设施实测压缩字节/Span、对象存储增长、采样/丢失与峰值待决占用，再复核 20 GiB/72 小时是否满足实际负载。


## HTTP 追踪隔离容量基线（本地）

2026-10-20 用户确认两档各 600 秒的本地隔离负载：50 请求/秒，普通/5xx/1.1 秒慢请求为 90%/5%/5%；70 请求/秒为 60%/30%/10%。复用真实 Go 三进程五 Span 链、策略 v1 的 120 秒尾采样和自然五分钟块轮换，保持 72 小时/20 GiB 配置。运行容器含发生器合计最多 6 CPU/6 GiB，所有当次卷占用达到 2 GiB 停止，每轮最长 40 分钟。以完整落盘物理成本（块、索引、MinIO 元数据/分配开销）外推，并额外留一份完整合并副本；按至少 100 Span/秒核算，72 小时预测 ≤16 GiB 才通过，工作空间另与既有 5 GiB 预留比较。

首轮预检因 Prometheus 标签值含空格、夹具错误按空白分列而失败，未计容量通过；解析已改为从行末拆数值，增加反例。实际完整首档（冻结源码 `y3p8h4tb/source`，基线 `5c0b555e6`）发送 30000 请求：普通 27000、5xx 1500、实际慢请求 1500，漏发/传输/非预期响应均零。Alloy 新 Trace 30000，策略保留 3936/舍弃 26064；Tempo 接收 19680 Span，未见提前淘汰、拒绝、刷块失败或 OOM。控制器等最后七条自然切块、完成对象计数核对后进入查询，但一次二十条明细在五秒内仅得到九条即超时，门禁失败、第二档未运行。最后物理 Trace 占用 4472832 字节，当次卷峰值 132960256 字节，Tempo 工作空间峰值 3080192 字节；这些仅作诊断量，不能据单档推定 20 GiB/72 小时通过。独立 bucket 实际读回硬配额 21474836480 字节。该失败报告未在查询前保存完整对象清单，现已修正报告时序：先保存负载/对象/物理成本，再执行查询，查询失败仍保留档次数据且不得判通过。

首档失败原始报告为 `/private/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-trace-capacity-285a431fcaa244eca6ad79d6b3538fca.json`，对应冻结源码同级 `capacity.log` 保留错误、单条明细 0.37～0.79 秒及清理证据。该轮自动退出核验容器、网络、卷零残留。用户随后明确批准先修查询瓶颈，再开同上限的本地窗口重测。

查询修正保留每主体二/每 Monitor 八请求、五秒、1000 Span、100 参与实例及 2 MiB JSON：明细最多四并发，System 当前 User 裁决与 Tempo 搜索/明细共用最多八个活动上游调用；按搜索顺序输出，任一错误/预算超限取消并等待工作者，整体重新授权前不返回部分数据。冻结源码 `lr0ofhwc/source` 的 Monitor Go T1 及竞态检查通过，包含四并发/八上游/主体限额/顺序/撤权/取消/累计预算；原默认 `make test-monitor-traces` 326.97 秒通过并清零。Tempo 默认五十分片仅为调查候选，没有未经对照修改。

2026-10-20 23:53 的重测启动误判了含其他会话 make/link 的 guard；完整日志确认第一档已短时开始，实际次数未完整保存，不能记零流量或有效容量。中止暴露 Compose down 遗留一次性 trace-load 的清理缺口：自动清理失败与本方按项目/服务标签核验后恢复清零分别记录在 `addp-trace-capacity-f503663cd61b47d58d2f4e3a099636bd.json`。门禁现先等待自有 Compose CLI 退出，并显式移除本轮一次性发生器，再 down 和核验零残留；新增模拟正常移除与拒删失败回归，不改变个人服务或其他 owner 资源。

2026-10-21 冻结源码 `k0pjymvv/source` 完整重跑 `make test-monitor-traces MONITOR_TRACES_TEST_ARGS=--capacity` 退出 0，原始报告 `addp-trace-capacity-16a165520915484dac28ef86f7e0637e.json` 为 `passed`，清理字段 `zero_owned_containers_networks_volumes=true`。当次运行容器合计硬限制 6 CPU／5.75 GiB；274 次采样覆盖至 2013.49 秒，卷峰值 209321984 字节（199.625 MiB），待决 Trace 峰值 8404，Tempo 工作空间峰值 6217728 字节（5.93 MiB），未触发停止线、OOM、采集拒收、导出失败、提前淘汰或刷块失败。真实尾采样、自然五分钟切块、72 小时配置和 bucket 配额均未缩短或放宽。

| 档次 | 实际负载及发送结果 | 自然落盘样本 | 一次二十条查询 |
| --- | --- | --- | --- |
| 50 请求/秒 | 600.7696 秒，30000 请求；普通/5xx/慢 27000/1500/1500；漏发、传输错误、非预期响应均 0 | 19755 Span／3951 Trace；物理 4481024 字节；保留速率 32.883 Span/秒 | 4.68848046 秒；每条五 Span、两条匹配边，无 Partial |
| 70 请求/秒 | 600.9632 秒，42000 请求；普通/5xx/慢 25200/12600/4200；漏发、传输错误、非预期响应均 0 | 本档新增 58785 Span，累计 78540 Span／15708 Trace；累计物理 13139968 字节；本档保留速率 97.818 Span/秒 | 1.970312333 秒；同样完整二十条 |

两档 SDK 最终队列／丢弃／导出失败均 0；导出、Tempo 接收和活动对象块计数相符，所有保留数据自然落盘。单位成本采用两档累计物理成本的较大值 226.8298659 字节/Span；按至少 100 Span/秒、259200 秒和两份完整副本外推为 11758860247 字节（10.9513 GiB），低于 16 GiB 判定线，保留原 20% 余量。相同数据模型与公式下的预算边界约 146.10 Span/秒，不是已压测吞吐或可提高采样预算的依据；策略目标仍为 100 Span/秒。第一档查询距五秒上限仅约 0.31 秒，不能据两次样本承诺延迟 SLA。

原始报告与完整日志分别位于 `/private/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-trace-capacity-16a165520915484dac28ef86f7e0637e.json`、`/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-p19-capacity-k0pjymvv/capacity-final.log`。同一冻结输入的 `make test-infra-runtime-log-lifecycle` 为 58 项通过（39.872 秒），`GOMAXPROCS=2 GOFLAGS=-race make test-module MODULE=monitor GO_ONLY=1` 通过，`python3 scripts/ci/check-t2-ci-registration.py --repository .` 通过；既有 Monitor T2 自动登记命中新夹具，容量负载仍只由显式参数启动。另有默认功能 T2 通过（326.97 秒）及独立 IAM runner 20 项通过；后者只修正既有 PlatformTrace 迁移选择器断言，未扩展 IAM 权限。

共享主机并非独占：第一档负载准确时段为上海时间 00:07:13.037–00:17:13.807，第二档为 00:24:50.333–00:34:51.297。其他 owner 提供的日志文件时间估算中，p65 的 00:08:43.202–00:09:08.902 落在首档负载，p82 的 00:18:10.909–00:20:28.969 落在首档排空期，503 诊断的 00:27:48.655–00:28:14.678、00:29:08.459–00:29:35.868 落在第二档负载；这些不是原生进程起止记录，不能据此推断干扰因果，也不能称主机零重叠。发送完整性、异常丢失与查询仍按本方实际计数判定；容器的当次硬配额不代表宿主全部任务总配额。

此前各项失败、中止及未运行仍不计通过。本专项始终只做本地验证，不运行 Hosted，不接管/重启个人服务；本次结论限定于所测三 Go 进程五 Span HTTP 链、属性及单节点存储，未证明真实 72 小时留存/过期删除、长期合并峰值或生产 HA，也不替代全量 HTTP 指标及 QPS 告警实压。


## HTTP 追踪二期第二批（Python 本地）

2026-10-22 按用户确认范围，先修订运行观测规范及验收标准，再并行实现 Common Python 显式 SDK／ASGI／HTTPX 内核、Agent/Copilot 生命周期及日志关联；复用唯一部署声明、mTLS、Alloy/Tempo、采样与读取权限。只补同步 HTTP Trace，不把 Python 接入记为全量 HTTP 指标或进程指标，不增加第三方模型、异步 execution、SQL、其他 Runtime 或 Hosted 范围。生产部署投影仅显式新增 Agent/Copilot backend；依赖固定为 OpenTelemetry API/SDK/proto 1.45.1 并同步共享锁。未改个人虚拟环境，测试使用仓库外依赖叠加层。

冻结基线 `427c662b0c009ec331ec007587f1ed4a344b464d`，只叠加本批 35 个输入，输入清单与 SHA-256 为 `/tmp/addp-p19-python-trace-final/inputs.json`；原始日志及聚合报告位于同目录。代码、锁、配置和测试指纹在验收期间无漂移；随后 main 的 `f2694f848`／`f0a673271` 仅改 GeoPython 文档／算子／测试，不涉及本批受验输入。标准本地入口结果：

- `make test-common-python COMMON_PYTHON=/tmp/addp-p19-python-trace-final/bin/common`：411 项通过、48 子测试通过，1 项因该基础环境没有可选 GeoPandas 跳过，不计通过。包括真实回环 mTLS／官方 protobuf、安全投影、并发关联、SDK 环境污染与导入失败隔离、2048 队列、两秒失败计数及三秒关闭；HTTPX 保持原 TLS／超时／认证／流式协议，并拒绝外域和重定向携带平台关联头。
- `make test-copilot COPILOT_PYTHON=/tmp/addp-p19-python-trace-final/bin/copilot`：184 项通过，保留 3 项依赖弃用警告；`make test-agent-eval ADDP_AGENT_PYTHON=/tmp/addp-p19-python-trace-final/bin/agent ADDP_AGENT_EVAL_REPORT=/tmp/addp-p19-python-trace-final/agent-eval-final.json`：退出 0，7 项场景契约、Agent 评测／持久化及 Common Python 聚合均通过，仅 offline，不计真实模型／工具或 T4。Agent 最终输入还覆盖真实临时 Uvicorn 进程收到 SIGTERM 后六秒内退出 0，并实际经过追踪关闭 finally。
- `make test-infra-runtime-log-lifecycle`：60 项通过；`make test-python-ci-registration`：10 项回归及 11 个 Python owner 登记通过；`make test-workflow-security`：固定 zizmor 标准离线审计通过；现有 T2 登记检查通过（22 hosted-service／12 gate-owned Compose／9 hosted-only／2 owner-managed）。新增 Python 目录及混合源 fixture 已纳入 Trace 门禁输入，Linux Job 在执行前准备 Python 3.12、共享依赖及固定 FastAPI/Uvicorn，不新增门禁或手工触发路径。
- `make test-monitor-traces MONITOR_TRACES_PYTHON=/tmp/addp-p19-python-trace-final/bin/agent`：退出 0，整次 384.86 秒，真实 Pipeline 328.87 秒。Go Gateway → Python Copilot fixture → Go System 产生预期 5 Span／2 条匹配边；首阶段三进程分别导出 202／404／404 Span，丢弃与失败均为零。保留真实 120 秒尾采样，失败／慢链及 14/200 普通链可查；安全投影、历史参与实例、当前 User 撤权、读写 mTLS 隔离、只读代理，以及 System/Copilot 分别查询同一精确 Trace 的 Loki 元数据均通过。Collector 停止期间请求与 Ready 不受影响、损失计数出现，恢复后新链及五 Span／两边通过；三个受控子进程均正常退出，最后明确当次容器、网络、卷及设施临时目录零残留。父进程的 `TestOwnedTraceProcess` 是受控子进程专用 helper，未启用 child 标记时按原设计跳过，不计业务通过。

两项合并复核修正已验证：SDK 顶层读取非法环境不能破坏追踪关闭时的业务导入，初始化中途失败释放 Provider／Client／线程；默认 HTTPX transport 显式保留 `trust_env=False`，不重新引入环境 CA。Python fixture 使用自有 SIGTERM 处理，以确保 Uvicorn 信号重投后仍执行 finally，不吞 Go 子进程退出错误。极端持续滴流时，HTTPX 活动 socket 无公开取消接口，最多留一个网络守护线程，运行时禁止再出站／重入累积，迟到成功不得计数；该有界退化由阻塞传输测试覆盖，正常实链退出清零独立验证。

本轮只证明上述冻结输入的本地 T0/T1/T2；未运行 Hosted、容量重跑或真实 72 小时留存／删除，未重启个人服务或设置定时。先前容量外推及 Linux Metrics 证据保留原边界，不作为新增 Python 吞吐／长期容量或最新共享 main 的验收；本批推送后的远端自动 CI 结果另记。


## Python Trace 接入后的开发依赖／OpenAPI 阻断修复

2026-10-22 针对 `c05c70480` 引入共享 OpenTelemetry 依赖后的本地可复现阻断，核实 Copilot 旧 venv 没有 `opentelemetry`，editable `addp-common` 源码已更新但已安装元数据仍不含新依赖，旧 FastAPI／LangChain 导入探针却判完整。Agent 的共享元数据同样陈旧。全量 restart 在 start 的依赖准备之前生成 Python OpenAPI，因此仅补 start 探针仍不能解除阻断。

按既有生命周期原则先修规范：Agent/Copilot 共用 `addp_prepare_python_backend`，在共享安装锁内检查／创建声明版本的 venv，再交 `addp_sync_python_dependencies` 按完整 requirements、Common Python 输入及实际已安装环境指纹同步。删除两个 Backend 的 `NEED_INSTALL` 少量包探针与重复 pip 路线；相对 editable 声明固定在所属 Runtime 工作目录解析。全量 restart 停止后先准备两个 Backend，再生成全部文档；局部保持 Go 停服前预检，停止后只准备、生成和校验所选 Python Backend。普通 start 先检查服务运行状态，活动实例不改环境；同步／OpenAPI 失败不进入启动、不放宽错误。Math 既有完整共享导入失败会触发安装，Jupyter 指纹纳入共享 pyproject，其他原生 Runtime 已调用统一指纹同步；未扩展其产品能力或另加安装路线。

验证基线为 `b0daa28446e22dfa466c695caa7b27150ae39009`；产品输入与隔离副本逐字节一致，标准日志和指纹见 `/tmp/addp-p19-swagger-dependency-fix/`。`make test-dev-lifecycle` 最终退出 0，包含 124 项配置／部署 Python 检查、60 项设施生命周期检查、完整启停／原子构建 shell 回归、Common 三个 package 校验及四模块 cmd 编译检查（不计业务执行）。新增回归覆盖已有旧 venv 缺共享依赖、实际环境元数据失效、输入不变复用、失败清除成功记录、同环境并发只安装一次、共享锁释放，以及全量／局部 Python Swagger 前准备、停止／准备／生成／覆盖失败拒绝和未选服务保留。首轮新增夹具因 macOS `/var` 与 `/private/var` 临时目录别名比较失败，统一 resolve 后整套重跑通过，未放宽产品断言。`make test-build-registration` 的 59 项／9 项回归及 26 Go 单元、59 镜像登记通过；现有 Platform consistency 的 `make test-platform` 已包含本入口，无需新增 CI 编排。

将当前 Agent/Copilot venv 用 APFS 独立副本复制到隔离源码树，仅在副本调用标准 Backend 准备函数：完整 requirements 安装、`pip check`、应用真实导入、`gen-swagger.sh agent/copilot` 与 `check-route-coverage.sh agent/copilot` 均通过，分别 14／12 个公开路由方法；OpenAPI 与提交产物完全一致。再次调用同一准备入口，两者均按指纹跳过安装。原用户 venv 只读复核仍保持原元数据状态，Copilot 仍无 OpenTelemetry，未就地修包或操作用户服务。

使用上述真实准备后的隔离解释器，`make test-common-python` 411 项及 48 子测试通过，1 项可选 GeoPandas 缺失跳过、不计通过；`make test-copilot` 184 项通过（3 项既有依赖弃用警告）；`make test-agent-eval` offline 的七场景、Agent 及 Common Python 子门禁通过；`make test-swagger` 的 18 项脚本回归与所有 Go/FastAPI 路由覆盖通过。此次只修本地准备／生成链，无 API 契约变化、无需改 OpenAPI 产物；未运行 Hosted、容量或用户服务重启。用户需在自己的终端经标准 restart 同步旧环境，推送后的自动 CI 状态另记，不能把排队当作通过。


2026-10-22 23:22 用户在自己的终端完成重启后，本会话只读核验当前源码 `a4cdb6b81409d2d3237eba01cff3d1a8b86f8690`：Agent/Copilot 的 API／SDK／proto 三个依赖均为 1.45.1，两者依赖指纹与实际环境一致且 `pip check` 通过，`addp_python_dependencies_current` 返回可复用，没有执行安装。已核原生 PID 74166／74147 分别监听实际保存端口 8190／8087；两者 `/health/live`、`/health/ready`、`/openapi.json` 全部 HTTP 200，实时 OpenAPI 与已提交投影深相等。标准 `PYTHONDONTWRITEBYTECODE=1 bash scripts/swagger/check-route-coverage.sh agent copilot` 退出 0，分别 14／12 个公开路由方法一致。由此关闭本次本机缺依赖／生成阻断，不外推正式业务、LLM、Trace 尾采样或 Hosted 验收；未由 AI 重启或改写 venv。只读核对修复提交 `673cf8eef` 的自动 Platform CI `39803050868`，Platform consistency、Common Python、Copilot Backend 与 Agent offline 均仍 queued，不计远端通过。


## Python Trace 与依赖修复的远端 CI 分项回执（2026-10-23）

本次手动只读核验本专项原始 Job 日志、checkout SHA 和路径选择，没有调度 Hosted、定时或容量。证据清单及原始日志 SHA-256 见 `/tmp/addp-p19-async-trace-investigation-20261023/ci-audit.json`。

- `c05c7048080af98d968a0e6e31437794bdb430b6` 的 [Trace T2 Job 119877525491](https://github.com/pampa0629/addp/actions/runs/39778225752/job/119877525491) 实际执行 `make test-monitor-traces` 并通过；`TestOwnedTracePipeline` 328.64 秒，真实 120 秒尾采样保留失败、慢链及 12/200 普通链，最后明确当次容器、网络、卷和临时文件零残留。日志 SHA-256 `b0b6d3df6a02bc1c90af0f41631ea043fad17b1bf13ae872ceb1fd355e81beb9`。该 Release/T2 workflow 总结失败来自其他专项，本记录只证明本 Job。
- 同一 `c05c70480` 在 Platform 的 [Common Python](https://github.com/pampa0629/addp/actions/runs/39778225763/job/119822518588)、[Agent offline](https://github.com/pampa0629/addp/actions/runs/39778225763/job/119822519104)、[Copilot Backend](https://github.com/pampa0629/addp/actions/runs/39778225763/job/119822519135) 原始日志均为 `selected=true` 并实际执行：分别 411 passed／48 子测试／1 项可选缺依赖跳过、七场景及子门禁通过、184 passed。可选跳过不计通过；不将该 workflow 的无关失败外推为本专项失败，也不将其总结写为全绿。
- 依赖准备修复 `673cf8eef7cb776de26a0b04c8dbdc86384becbf` 的 [Platform consistency](https://github.com/pampa0629/addp/actions/runs/39803050868/job/119908856837) 实际完整执行 `make test-platform` 并通过；含 124 项配置／部署、60 项设施生命周期、统一 Backend 准备／指纹及 dev lifecycle／原子构建，末尾 Swagger 覆盖通过。同 run 的 Common Python、Agent、Copilot 三 Job 虽状态 success，原始日志是路径 `selected=false`，属于跳过，不能作为这三项重测通过证据；三项实际执行证据使用上面的 c05 原输入。

由此关闭前两批本专项自动 CI 待核项；不是对后续共享 main、异步第三批、真实身份 T4、真实 72 小时或生产容量的证明。用户重启后的本机只读结果仍沿前节，不重复重启个人服务。


## 执行短阶段与 Trace Link（二期第三批，本地）

2026-10-23，按用户确认的边界，先修订术语、观测与执行记录规范，再实现 Common 持久引用和独立短阶段 Trace／Link，首接 Orchestrator → Develop bounded query。入队、领取、派发、终态分别结束，不把 SQL、等待、心跳或整个长任务包进 Span；耗时仍以 execution 为准。不新增表或 Tenant Trace 字段，纯值引用保存在 execution_config／metadata 的保留 namespace；终态业务 metadata 继续原替换／清空语义，只保留权威私有观测字段，不累积旧 outputs／result。阶段成功只在原 owner 外层事务提交后记录；失败提交为 rejected，畸形私有引用停用当次追踪并保留原事实。

Monitor 仅接受 addp.http／addp.execution 两种明确契约；Link 目标和实际参与实例一起沿当前 Platform User 逐项重核，数字实例 ID 来自 System，目标撤权拒绝整份结果；不递归拉取，不生成 HTTP 调用边。页面展示阶段、attempt、outcome 与因果链接，按目标起始时刻另发受限查询，撤权清除原详情。采样唯一策略升级 v2：failed／timeout 阶段进入既有失败预算，cancelled／rejected 按普通阶段；HTTP 慢请求口径和 5%／120 秒／10000 待决／100 Span 每秒／70:20:10／72 小时／20 GiB 初值均保持。OTel SDK 环境资源污染由唯一导出投影剥离，不修改进程全局环境。

冻结源码基线 `dfcf4ace806a87c912b7787233d40fcde5f29a14`，仅叠加本批明确路径；输入 SHA-256、日志和最终分项清单位于 `/tmp/addp-p19-execution-trace-20261023/plan.json`、`verification.json`。测试在隔离源码快照执行，PostgreSQL 因标准入口校验容器归属，改用原工作区标准入口，并验证源码指纹、共享连接与并发门禁；未绕过归属检查。共享 main 后续其他会话改动不算该冻结输入的验收。

- T0/T1：`GOMAXPROCS=2 GOFLAGS=-p=2 make test-go` 最终退出 0，22 个 Go 模块含 tidy-diff／CGO 禁用检查通过。Common／Monitor 单 owner 门禁通过；最终 Orchestrator／Develop 分别经 `make test-module MODULE=<owner> GO_ONLY=1` 通过。后续 owner 私有修正另经各自 T1 及 PostgreSQL 验证，未重跑无输入变化的全模块。Common 验证 SDK 所属、独立 root／Context、不可变入口、有界 Link／属性、敏感资源剥离；Develop 真实授权读投影回归确认 Tenant 不泄露 execution_config 私有引用。
- T2 owner：先执行 `bash scripts/infra/status.sh`，实际共享 PostgreSQL 为 25432、唯一 `addp_test`；`make test-orchestrator-postgres` 与 `make test-develop-postgres` 均通过。最终 Develop metadata 修正后 T1、PostgreSQL 再次通过（后者 13.70 秒）；覆盖入队／领取／派发／终态、租约及事务拒绝、重读引用、伪造剥离、故障恢复、畸形引用和不影响其他 task type。旧实现配新用例稳定失败于“保留旧业务 metadata”，修正后覆盖 omitted／replace／nil／typed nil／空对象／非法类型。中间测试错误要求数据库读回 nil，已按既有 JSONMap 空值归一化为空对象修正，未改变共享存储语义或放松旧结果清除断言。
- 真实 Trace T2：`GOMAXPROCS=2 GOFLAGS=-p=2 MONITOR_TRACES_PYTHON=/Users/pampa/code/addp/agent/backend/venv/bin/python make test-monitor-traces` 退出 0，Pipeline 328.53 秒；实际 120 秒尾采样保留失败／慢链及 10/200 普通链。首阶段三个生产进程分别导出 204／404／404 Span，队列、丢弃、失败均零；另一个受控进程重读持久检查点，真实 Alloy／Tempo 验证四个独立阶段 root、两条持久 Link、目标单独撤权拒绝、失败短阶段优先及精确终态 Trace／Span 的 Loki 日志。独立采样的 Link 目标允许查无数据，不能宣称完整任务必留存；第四进程未单独汇总上述导出计数。原 Go → Python → Go 五 Span／两边、mTLS、只读代理、Collector 故障恢复继续通过，退出明确当次容器／网络／卷／设施临时文件零残留。父进程 TestOwnedTraceProcess 是受控 child helper，按设计跳过，不计业务通过。
- 页面 T3：`make test-monitor-frontend` 最终退出 0，98 项确定性测试、295 项 Chromium E2E 与 build 通过；`make test-console-frontend` 189 项确定性测试（29 文件）、218 项 E2E 与 build 通过。此前 Monitor 两次在执行场景前因隔离副本 node_modules 内嵌符号链接导致双 Playwright 实例失败；仅修副本依赖解析后全量通过，没有放宽测试或改产品依赖。T3 为受控 HTTP 夹具，不计真实身份验收。
- 配置／登记：`make test-dev-lifecycle` 退出 0，124 项配置及 60 项设施生命周期检查、shell／编译检查通过，覆盖唯一 v2 渲染。现有 Make owner 自动发现、T2 注册和路径选择已核；Trace 门禁输入补 Common execution 与新实链 fixture，Develop PostgreSQL 既有 regex 补新回归。三个 go.mod 仅将已有版本转直接依赖，无升级；无新门禁、数据库、构建方式或 workflow。`bash scripts/swagger/gen-swagger.sh monitor`、`bash scripts/swagger/check-route-coverage.sh monitor` 通过，90 个公开方法一致，产物同步。
- 平台聚合：`make test-platform` 非零。此前本专项需要的配置、依赖／CI 注册、变更选择与安全等子检查通过（T2 注册 22 hosted-service／12 Compose／9 hosted-only／2 owner-managed）；在 Online Raster 既有工具链测试要求 Node 24、当前解释器为 Node 25 时失败，215 项中此项失败。不扩展修复其他专项，不计整套平台通过；末尾授权／Swagger未执行，另以标准 `make test-authorization` 补验最终退出 0，授权与全模块 Swagger 覆盖通过。首次补验隔离快照无 Python venv，Go 授权及全部 Go Swagger 已通过但 Agent/Copilot 覆盖未执行，未计全套通过；仅在副本准备既有 Python 环境后复验，不改用户 venv。

本批只有上述本地 T0/T1/T2/T3 证据，未运行 Hosted、正式身份 T4、容量重跑或真实 72 小时保留／删除，未设置定时或重启个人服务。原 HTTP 两档容量结果不外推新增阶段。其他 Runtime、continuous、SQL Span、Python 异步、长期合并及生产 HA 仍未覆盖，不能将本批记为全链路追踪全部完工；新提交的远端自动 CI 状态另记。


## 正式身份的本机执行 Trace／日志闭环（2026-10-23）

用户确认仅在本机一次性隔离部署，先修规范与标准入口，再验证正式密码／MFA、Orchestrator → Develop bounded query、Platform Trace 查询与精确日志关联。新增人工 `make test-monitor-traces-local`；静态／准入／守卫／业务契约标准入口 `make test-monitor-traces-local-runner` 聚合到既有 Infra runtime-log lifecycle / Platform，CI 登记明确拒绝公共 workflow 调用真实入口。所有构建与运行容器、浏览器共享 8 CPU／12 GiB 上限，独占卷达到 4 GiB 即停止，最长 60 分钟后进入不可跳过的清理；不运行 Hosted、容量、定时或个人服务重启。

Common 为独立短阶段增加 `EndExecution`，与原 End 共用一次终结内核，固定 `execution.checkpoint`，仅规范 execution_id／phase／attempt／outcome，trace/span 取真实阶段 Context；UUID 不进 Span、索引或 Tenant 投影。成功／拒绝沿原 owner 最外层事务结果，不造第二条状态路径；无效 UUID、非法 outcome、关闭追踪不伪造关联。System 本地身份夹具经独占 PostgreSQL `cluster_name` 与 `addp_online` 核验后，调用正式 Bootstrap／IAM 服务创建当次三员、Tenant 准备员及精确十一权限业务 User；仅输出私有登录凭据，不预签会话。准备员只做 Engine 准备，User 实际执行，Platform 系统管理员读取 Trace／日志，安全管理员为无权限负例。

实施前识别的验证范围及当前准备结果：

- 冻结 Common／Orchestrator／Develop 13 个 Go 文件的标准 `make test-module MODULE=<owner> GO_ONLY=1` 全通过，见 `/tmp/addp-p19-checkpoint-20261023-5bqj4s22/manifest.json`、`receipt.json`。System 身份夹具独立冻结后完整 System Go 通过，见 `/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-p19-local-identity-o4eet7fw/plan.json` 与 `system-go.log`。首次新用例的临时目录未显式 0700 被正确拒绝，已修测试准备且最终通过，未降低产品权限。
- 先执行 `bash scripts/infra/status.sh` 核实共享 PostgreSQL 25432，以标准 `make test-orchestrator-postgres`、`make test-develop-postgres` 在 addp_test 通过（15.59／27.50 秒），源码／连接／并发守卫无冲突。日志位于 `/tmp/addp-p19-formal-local-20261023/`；本轮正式身份部署使用另一独占容器，不能把这两次 Repository T2 记为正式身份验收。
- `make test-monitor-traces` 退出 0：真实 Pipeline 327.65 秒，120 秒尾采样保留失败／慢链和 9/200 普通链，首三进程分别导出 204／404／404 Span，队列／丢弃／失败为零；独立短阶段、持久 Link 与目标单独撤权、Go → Python → Go、精确 Loki trace/span、mTLS 与故障恢复通过；退出明确当次容器／网络／卷及设施临时文件零残留。child helper 按设计跳过，不计业务通过。这仍是受控 T2，正式 owner 日志由下面本地身份入口另验。
- `make test-monitor-traces-local-runner` 最终通过：31 项 Python 契约（含源输入冻结、Docker Context 优先级／远端拒绝、有界资源／真实卷停止线／采样超时／进程组回收／清理、精确正式 API DTO、最小日志、失败阶段采样、独立 Cookie Jar 与正式退出）、两份 JS 语法、完整 Online CI 登记检查及 System ExecutionTrace 定向 Go。最初源码快照夹入另会话未提交 Catalog 引用造成不完整编译，已仅在快照恢复本专项集成版本；未修改其工作区或把该次记为通过。
- `make test-infra-runtime-log-lifecycle` 通过（上述本地 runner 加既有 60 项设施生命周期）。Console 新增四项本地浏览器输入／准入／TOTP／禁止 Token 注入契约通过；`make test-console-frontend` 聚合首轮因隔离副本缺 System Vite 依赖失败，补副本依赖后 192/193 确定性通过，唯一失败是既有 devPorts 的 HMR 缓存准备断言，未进入其后 E2E/build。不修复或外推无关专项，也不计整套 Console 通过；本专项真实浏览器由独立标准本地入口验证。
- 共享扩散 `make test-go` 首次在 System tidy-diff 检出新增测试把已有 go-sqlite3 直接导入仍声明 indirect；只修声明、不升级版本，最终扩散结果另记。输入、标准日志及分项报告统一保留 `/tmp/addp-p19-formal-local-20261023/`；一次误在共享工作区启动的重复 Go 门禁已仅终止自己的进程组，不作为冻结输入证据。

此提交先落入口与准备证据；正式身份本机隔离验收尚未执行，不记通过。成功、失败、停止线和清理结果将追加本节；本地技术证据不替代专用 Runner T4、实例单独撤权、SQL Span、其他 Runtime、真实 72 小时留存或生产容量。


首次本地窗口在构建器准备阶段等待 Docker Desktop 个人凭据助手，尚未下载基础镜像、创建业务部署或发送业务请求。主动中断自有 gate 后，进程组、Builder 容器／卷和私有目录经精确过滤复查均为空；最终报告写入还暴露 Path.open 不支持 opener 的接口错误，原自动报告未生成，不伪造为通过。已修为内置 open 的 O_EXCL／0600 路径，并补真实文件权限与拒绝覆写测试；Docker 客户端改为当次 0700 目录、空 auths、无凭据助手，只读公共工具插件并固定已核对 Unix endpoint，不借个人凭据。标准 local runner 复验结果另记。后续操作继续扣减首次窗口已耗时间，未放宽原 60 分钟、8 CPU／12 GiB 或 4 GiB 停止线。首次失败及清理复查见 local-first-interrupted-receipt.json 与 local-acceptance.log。


## 正式身份本地第三窗口：镜像核验、守卫修复与部署配置拒绝（2026-10-23）

用户重新授权窗口为北京时间19:18:00–20:18:00，上限仍为8 CPU／12 GiB、全部当次卷及私有可写目录4 GiB停止线。六次尝试共用这一绝对截止时间，没有按重试续时。标准入口均为 `make test-monitor-traces-local`；未运行Hosted、容量模型或定时任务，未接管个人开发服务。

首次真实核验已有MinIO产品镜像成功：Linux ARM64，镜像ID `sha256:dd87261892226d0e651ba1d059205cdd4e93d94be3dd9c819abffbf23d994d0b`；MinIO `RELEASE.2025-10-15T17-29-55Z`嵌入源提交`9e49d5e7a648f00e26f2246f4dc28e6b07f8c84a`；mc `RELEASE.2025-08-13T08-35-41Z`，源提交`7394ce0dd2a80935aded936b09fa12cbb3cb8096`。两组件均以同一不可变镜像ID执行只读、无网络版本检查并匹配当前固定配方，报告保存版本输出哈希；这不等同于重新源码编译或可复现构建证明。

| 尝试 | 实际停止点 | 结果与清理 |
| --- | --- | --- |
| 1 | Orchestrator产品镜像构建；卷测量解析通用异常 | 失败，129项成功测量；零残留 |
| 2 | System镜像前的私有目录测量；du非零退出 | 失败，38项成功测量；零残留 |
| 3 | 对象缓存清理0.75秒期限 | 失败，41项成功测量；该过短期限方案已撤掉，零残留 |
| 4 | 全部产品镜像后主动停止BuildKit，被误判容器失败 | 失败，169项成功测量；零残留 |
| 5 | 前置停止检查复现BuildKit退出1、非OOM | 失败，在昂贵编译前拒绝；零残留 |
| 6 | 全部16个产品镜像及浏览器依赖完成后，Trace部署配置生成被拒绝 | 失败，180项成功测量；零残留 |

缓存清理会使du遍历中的文件消失。当前唯一测量路径在原四秒总预算内，仅对明确ENOENT重新进行完整扫描；不采用带缺失诊断的部分字节数，其他错误或预算耗尽仍停止。第4次实际发生一次私有目录完整重扫，最终测量0.508秒；该次最长测量1.055秒、最大完成间隔4.24秒。卷内扫描当次没有重扫，不据此推定首轮卷异常的全部根因。BuildKit通过成功的主动停止命令后实际退出码1，当前只允许当次明确主动停止的精确构建器ID出现该已核验状态；停止前须运行且非OOM，产品服务、一次性任务、其他退出码及OOM仍拒绝。停止检查已前置，末尾复用同一实现。第6次真实通过前后停止检查，180项测量最长0.807秒、最大完成间隔4.43秒。

第6次固定源码为`4aee4813909077cd3fa35906bf70353278ba89e2`，六个后端、Console及其余产品前端／Nginx共16镜像从该源码构建，报告保存不可变ID；浏览器依赖准备通过。部署生成拒绝根因为本地入口误用`module`，正式生成器只接受`module_name`。最终代码`162cb8747d211d3fe184e8acba48117f6123e934`已修正该字段，并把产品Trace／Compose配置准备移到构建器创建及编译之前，增加细分阶段和固定容器状态诊断，不引入兼容字段或第二路线。真实产品生成器的跨入口回归在恢复旧字段时失败、修正后通过；配置拒绝不得启动构建器亦有覆盖。

最终 `make test-infra-runtime-log-lifecycle` 退出0：55项本地入口／业务契约、JS语法、CI登记、System ExecutionTrace定向Go，以及既有60项生命周期通过。标准实际入口最后一次仍为失败；最终配置修复后的完整部署未重跑，正式密码/MFA、Tenant成功／受控失败执行、Trace与日志及Console闭环均未运行、未计通过。当前源码不是第6次完整实际运行的输入，不能把确定性回归记成正式运行验收。剩余窗口不足以重建并完成闭环，没有启动注定超时的完整重跑，也没有续开新窗口。

六次成功测量的总体峰值3,421,573,120字节（约3.19 GiB）、6.25 CPU／10.0625 GiB，均未触及原上限；这只覆盖准备构建，不能证明业务部署的资源表现。六次自动清理全部通过，独立复核当次容器、网络、卷、构建器、镜像标签和私有目录均为零，原MinIO镜像保留。安全报告、绝对窗口、输入提交、红绿回归及最终门禁日志见 `/tmp/addp-p19-formal-local-20261023-window3/` 中的 `attempt-1-report.json` 至 `attempt-6-report.json`、`receipt.json`、`cleanup-audit.json`、`deployment-contract-red.log`、`preflight-final-test.log`、`lifecycle-final.log`。清单实际第19行（B18）及第45行（B44）仍为部分实现，正式身份缺口保留；不将本次结果计为Linux Runner T4、Hosted、T5或真实72小时证明。


## 正式身份本地第四窗口：依赖核验通过，完整闭环未验收（2026-10-24）

用户“继续”后沿原上限新开窗口：2026-10-23 23:54:17至2026-10-24 00:54:17（Asia/Shanghai），8 CPU／12 GiB、4 GiB停止线；所有重试沿同一绝对截止，无Hosted、容量模型或定时。实际运行在截止前受控结束，清理完成。

本窗口定位并修正四项入口问题：

- Compose仍会插值未启用capacity profile中的挂载，缺少TRACE_T2_WORK使配置拒绝；仅声明当次私有工作目录，trace-load仍不选中。
- 一次性容器成功退出后自动删除，与资源守卫“列举后inspect”竞态；现在保留当次oneoff及初始化容器的退出状态至统一清理，非零退出和OOM仍拒绝，未用忽略检查或重试丢失状态的方法计成功。
- trace-query以UID 0且移除全部capability运行时，Nginx创建临时目录的chown被拒绝。真实前置nginx -t复现operation not permitted／chown；改为现有Trace T2同样的宿主私有证书所有者身份，保留cap_drop、只读和网络边界，实际校验及健康检查通过。
- Loki配置校验通过但实际启动permission denied。第8次实测进程UID10001，而可写/loki卷已变成root:root、755；初始化脚本此前成功chown10001并chmod750。空卷在创建消费者时的镜像copy-up覆盖了初始化权限。当前当次新建数据卷统一nocopy，由原初始化路径负责权限；第9次只读测量Loki /loki和Tempo /var/tempo均保持10001:10001、750，修正后实际服务不再退出。

为避免依赖启动错误在整轮产品构建后才暴露，先修规范，再将同一组独占依赖的实际启动移到编译前；同一私有Compose、网络、卷和凭据继续使用，不创建第二组或新增验收选项。依赖运行期间构建器与源码编译各1 CPU／2 GiB；业务构建后仅更新同一Compose内的实际业务镜像ID。原总上限、停止线、采样与业务断言不变。失败诊断只保留固定错误类别、输出哈希及Unix卷权限，不导出登录、SQL或运行日志正文。

当前最终代码`f07a5a6cc201a0ed676f0ad013ca5ddfcf3b23a8`。第9次标准入口`make test-monitor-traces-local`实际通过产品配置、MinIO/mc、Loki完整启动参数校验、Nginx校验、独占PostgreSQL／业务PostgreSQL／Redis／MinIO、两个存储初始化及遥测依赖检查；两代理healthy，Loki／Tempo／Alloy持续运行，报告dependencies_ready=true，并越过构建器前置检查进入编译。24项成功资源测量，峰值7.45 CPU、9.375 GiB、1,085,509,632字节，最长测量0.880秒。此为依赖准备通过，不是正式身份或全业务验收。

本窗口共九次实际尝试：1为模板配置拒绝；2为oneoff删除竞态；3为依赖启动失败；4、5为Nginx前置校验失败；6因余量不足受控中断旧顺序构建；7、8在新顺序中复现Loki权限失败；9完成依赖核验后因余量不足受控中断编译。完整入口报告均保留failed，其中两次为local_acceptance_interrupted，不改写成通过。第2、3次完成的16个产品镜像分别属于其各自固定提交，不能当作最终修复源码已完成重建。正式密码/MFA、普通Tenant执行、Trace查询、日志和Console闭环仍未运行、未计通过；没有续开新窗口。

最终`make test-infra-runtime-log-lifecycle`退出0：58项本地入口／业务契约、JS语法、CI登记、System ExecutionTrace定向Go及既有60项生命周期通过；未更改业务API或采样策略，现有Platform／owner路径继续覆盖。九次自动清理及独立审计均零当次容器、网络、卷、构建器、镜像标签和私有目录，原MinIO产品镜像保留。安全证据位于`/tmp/addp-p19-formal-local-20261023-window4/`，包括`window.json`、九份`attempt-*-report.json`、`attempt-9-mount-audit.json`、`attempt-9-planned-stop.json`、`receipt.json`、`cleanup-audit.json`、`lifecycle-final.log`；未手工调度或跟踪无关CI。原清单实际第19行（B18）、第45行（B44）继续部分实现，正式身份缺口保留，不计Linux Runner T4、Hosted、T5、72小时或生产容量通过。

## 正式身份本地第五窗口：标准数据库初始化修正，页面部署依赖仍阻断（2026-10-24）

本窗口由用户明确授权，绝对边界为2026-10-24 08:28:23～09:28:23（Asia/Shanghai），合计8 CPU／12 GiB、卷及私有可写目录4 GiB停止线。三次尝试沿同一截止时间，没有自动续期。第三次在截止前失败退出并完成清理，未运行Hosted、容量模型或定时任务。

第一次固定源码`1cf5be3660172abfc0e57b1fd9e3fb1be65e5f31`，基础依赖及七个二进制编译通过，在System产品镜像步骤返回`command_failed_2`；原输出仅保留哈希，不能据此断言具体原因。补充公共构建固定错误类别诊断后，第二次源码`8a3006670e7f6cfaddf018253515ccb37ab5bc78`的16个正式产品镜像与浏览器依赖通过，但Develop启动退出1、非OOM。其Docker标准输出为空，实际正文由runtime-log保存在当次卷中；入口随后补充只读提取固定错误类别／哈希，不导出正文。

源码核对发现隔离入口漏执行现有`scripts/infra/init-postgresql.sql`，而Develop初始化直接在`develop` schema创建导出会话表。运行观测规范与标准入口同步补齐初始化步骤：在当次独占平台库执行原SQL并使用`ON_ERROR_STOP=1`，失败即停止后续准备；不新建额外数据库、不触碰共享Infra。修正提交`2f9414696911a4701b23ce19081c2cab5f1f7925`。第三次实际通过该初始化，独立只读核对当次集群／平台库归属、Develop schema、PostGIS、pgvector及标准更新时间触发器函数均为true。随后七个二进制、16个产品镜像与浏览器依赖再次通过，System及其余五个后端按标准等待顺序就绪，已越过此前Develop启动失败。

第三次最终仍失败于`isolated_deployment_start`：Meta及Service页面容器退出1，均非OOM，诊断命中`not found`。产品配置核对确认，根Nginx对一批旧模块页面采用静态upstream，本入口为满足该依赖而启动了非本专项页面；这些页面又直接静态代理未部署的对应Backend。现有Monitor等页面已使用按请求解析。未通过假上游、增加未授权业务后端或忽略容器失败来继续；产品Nginx的部分部署行为修正需另行确认，未在本窗口实施。

三份完整入口报告均为failed，正式密码/MFA、普通Tenant执行、Trace／日志读取及真实Console闭环全部未运行、未计通过，实际运行提交核对探针亦尚未到达。标准入口准备门禁`make test-monitor-traces-local-runner`退出0（60项契约、JS语法、CI登记及System身份夹具定向Go）；最终`make test-infra-runtime-log-lifecycle`退出0，另含既有60项生命周期检查。没有改动业务API、权限、采样或保留策略，未跟踪无关CI。

全窗口578项成功资源测量，峰值硬配额7.45 CPU／10,066,329,600字节（9.375 GiB），卷及私有可写目录合计最高3,635,802,112字节，最长单次测量1.597秒，未触及停止线。三次自动清理和独立审计均为当次容器、网络、卷、构建器、镜像标签及私有目录零残留，已核验的原MinIO产品镜像保留。证据目录`/tmp/addp-p19-formal-local-20261024-window5/`含`window.json`、三份`attempt-*-report.json`、`attempt-3-database-init-audit.json`、`receipt.json`、`cleanup-audit.json`及`lifecycle-final.log`。实际清单第19行（B18）、第45行（B44）仍为部分实现；本证据不计正式身份闭环、Linux Runner T4、Hosted、72小时或生产T5通过。


## 正式身份本地第六窗口：按需页面及正式登录通过，执行闭环仍未验收（2026-10-24）

用户确认修正产品Nginx部分部署行为并新开60分钟窗口，绝对时间为2026-10-24 09:27:58～10:27:58（Asia/Shanghai），仍限8 CPU／12 GiB、当次卷及私有可写目录4 GiB停止线。三次完整尝试沿同一截止时间，未运行Hosted、容量或定时，过期没有自动续开。

提交`e25a09264e39a0c48e01af696b87df15c1b5f103`删除根Nginx七个旧模块页面的静态upstream，统一为请求时Docker DNS解析，保留去除模块前缀后的路径与查询参数；Console／Gateway仍是入口必需依赖。隔离入口只选择Console、System、Monitor页面和根Nginx，六个后端及身份、采样、业务断言保持。新增标准`make test-nginx-routes`与实际完整入口共享同一真实Nginx探针，同时接入既有Trace T2，并将nginx/加入其路径输入。最终实测11项嵌套路径、查询参数、静态资源／plugins／WASM及API路径通过，缺失模块502且已部署页面仍可读，探针清理通过。开发期两次探针失败分别涉及就绪重试stderr污染及回显服务遗漏Gateway 8000端口，均清理后修正，未当作产品或正式身份通过。

共享工作区首次完整准备门禁受另一专项未提交的Online登记组合影响，不计通过；仅保留其本专项63项契约通过事实。已提交源码e25a09264的私有副本随后实际运行标准`make test-infra-runtime-log-lifecycle`退出0（63项入口契约、JS语法、Online登记、System身份夹具Go及既有60项生命周期），构建登记与T2登记检查均退出0；私有准备源码副本已删除，没有纳入或修复并行未提交工作。

第一次实际完整入口源码`6621748b0b1a8497597e4f4ee5bb3274503402ff`：七个二进制、10个产品镜像、产品Nginx同一11项路由探针、六后端及四页面服务就绪通过，六后端实际/health/live提交均匹配该SHA。两个Platform User完成真实密码／TOTP登录、两个Tenant User完成真实密码登录；AuthContext为普通非委托User，Platform MFA身份、负例无平台诊断权限及业务User精确11项权限检查通过。随后在synthetic_engine_registration阶段以SuiteError失败，尚未创建本批成功／失败执行闭环；原报告没有HTTP状态，不把源码推断写成已实测403。

核对角色与API发现租户管理员模板不含引擎操作权限，而夹具遗漏已批准的独立准备能力。提交`6b7704611ff20856cfc03db9f4c3b6cfd1150e3a`通过既有IAM角色服务向当次准备成员显式赋予仅含system.engine.create／system.engine.execute的临时角色，业务User仍为原11项；增加正式AuthContext准备权限检查，以及只保留固定断言码或HTTP状态／稳定错误码的安全诊断。该修正代码和单测通过，但本窗口未到达修正后的运行验证，不宣称引擎登记已通过。第二次实际源码6b7704611在Security产品镜像构建失败，固定诊断命中failed to solve／timed out；未保留原始公共构建输出，不能进一步断言具体外部请求或原因，清理通过。

为在原总上限内减少闲置配额，先修规范，再由提交`1c5a5cc26dd7291f0a142a5a67adea1a908d3d2b`将编译与构建器串行：计划停止原构建器，以2 CPU／3 GiB及两个Go线程编译，完成后恢复1 CPU／2 GiB构建器；确认原ID运行且非OOM，并与采样锁同步后才清除计划停止状态。第三次实际完成七个二进制、构建器恢复及10个产品镜像，在browser_dependencies阶段达到本窗口总期限，以command_deadline_exceeded失败退出并清理。未缩短真实尾采样等待或省略任何业务／页面断言来计通过。

当前源码1c5a5cc26的`make test-infra-runtime-log-lifecycle`退出0：66项入口／业务契约、JS语法、CI登记、System ExecutionTrace定向Go及既有60项生命周期通过。源码未修改产品业务API、内置角色、采样或保留策略，不需生成Swagger；普通Tenant执行、Trace／Link与精确日志、Console闭环及退出后旧令牌拒绝本窗口均未通过。三份完整报告全部failed；正式登录的实际局部证据不能替代完整闭环，也不能外推Linux Runner T4、Hosted、72小时或生产T5。

全窗口516项成功资源测量，峰值7.8 CPU／10,468,982,784字节（9.75 GiB），卷及私有可写目录最高3,619,434,496字节，最长测量1.126秒；无OOM或资源停止线触发。三次自动清理及独立审计均为当次容器、网络、卷、构建器、镜像标签和私有源码／秘密零残留，原核验MinIO产品镜像保留。安全证据位于`/tmp/addp-p19-formal-local-20261024-window6/`，含window.json、三份attempt-*-report.json、nginx-route-gate-3.log、prepare-source.json、committed-lifecycle.log、final-lifecycle.log、t2-registration.log、receipt.json及cleanup-audit.json。仅只读查看过源码6621748的既有Release and T2自动运行40050940303，当时queued，未计通过，未手工触发工作流或跟踪无关CI。清单第19行（B18）、第45行（B44）继续部分实现。


## 正式身份本地第七窗口：准备守卫修正及IAM权限冲突，闭环未通过（2026-10-24）

用户授权窗口为2026-10-24 16:32:47–17:32:47（Asia/Shanghai），准备、构建、等待与重试共用该截止时间；最多8 CPU／12 GiB、所有当次卷及私有可写目录达到4 GiB停止。仅运行根入口`make test-monitor-traces-local`，不运行Hosted、容量或定时，不接管个人开发服务或共享数据库。实际8次尝试全部失败，正式登录→执行→Trace查询→日志→页面闭环仍未通过；第六窗口的登录局部证据不能替代本窗口或完整闭环。

本次修正：`b5b543863501d371d851a9dd69601f55875677f3`在依赖启动前准备全部当次Compose空卷和构建器，保留独占所有权／Compose标签；`5a1a4c0330809c836edb13e08ec4b1e3b578b1a8`把受0.25 CPU／64 MiB及30秒准入预算限制的只读卷辅助容器准备与首次完整测量分开，之后才启动依赖。周期四秒检查预算、最长五秒间隔、CPU／内存／磁盘上限、OOM及测量失败即停止均保持。`4108c283a183e4bfc89e76a3b38aa7cc1f6e48a8`补充首次资源检查的固定操作／耗时诊断；`d9a3cec22e12db098182e25bc96a62e0916f5f32`细分依赖初始化阶段及固定公共诊断标记，不保存凭据、SQL或原始失败正文。

每次实际结果（同一窗口，无自动续期）：

| 尝试 | 实际源码提交 | 停止阶段／原因 | 成功资源测量次数 |
| --- | --- | --- | --- |
| 1 | `c93da8151` | `isolated_dependencies_start`；`prepare_volume_sampler`超过资源检查预算 | 2 |
| 2 | `c93da8151` | 同上，未计通过 | 2 |
| 3 | `b5b543863` | `product_compile`；`measure_owned_volumes`超时，依赖已就绪 | 71 |
| 4 | `cdd038f22` | `bounded_builder`；`command_deadline_exceeded`，尚无完整资源样本；旧报告不能进一步定位 | 0 |
| 5 | `3a5cd4c54` | 同上，尚无完整资源样本 | 0 |
| 6 | `5a1a4c033` | `initial_resource_sample`；`command_deadline_exceeded` | 0 |
| 7 | `4108c283a` | `isolated_dependencies_start`；退出2，固定标记为空，未能定位具体准备命令或唯一根因 | 6 |
| 8 | `d9a3cec22` | `formal_identity_preparation`；退出1，固定标记为空；未到达真实登录及业务执行 | 253 |

第8次完成7个源码二进制、10个实际产品镜像、浏览器依赖、同一产品Nginx的11项路径／参数断言、缺席模块502及已部署模块仍可读检查；六个Backend均经`/health/live`核验实际提交为`d9a3cec22e12db098182e25bc96a62e0916f5f32`。正式身份准备失败后未继续业务；没有本次正式登录、成功／受控失败父子execution、真实检查点、自然尾采样、精确Trace／Link／日志、Console页面或旧会话拒绝的通过证据。

代码调查确认一项必须先讨论的权限冲突：此前`6b7704611ff20856cfc03db9f4c3b6cfd1150e3a`用`local.execution_trace_preparation`自定义角色授予`system.engine.create/execute`，但当前`system/authorization/permissions.yaml`及迁移目录中两者均不可租户定制；`TenantRoleService.CreateRole`必经`validateCustomizablePermissions`拒绝该方案。这是此前夹具设计错误，原精确权限单测未覆盖真实目录约束；现有运行报告尚不能证明它是第8次退出1的唯一原因。没有修改权限目录、制造AAL2、直接写授权表或擅自授予更大角色。建议讨论仅当次准备User通过正式密码／TOTP、真实增强认证及角色分配取得现有`tenant.infrastructure_administrator`；其11项权限超出此前准备身份两项初值，尚未确认或实施，普通执行User仍保持原11项权限。完整重跑需要另行确认新窗口。

当前最终代码的`make test-infra-runtime-log-lifecycle`通过70项本地入口契约、60项生命周期测试、Node语法、Online登记及System fixture Go检查；日志为`/tmp/addp-p19-formal-local-20261024-window7/dependency-diagnostics-lifecycle.log`。此前67／68／69项中间版本及各60项生命周期门禁分别记录，不代替最终运行验收；现有标准入口及CI登记继续覆盖本次脚本，未新增Hosted触发或定时。

原始脱敏报告为`/tmp/addp-p19-formal-local-20261024-window7/attempt-{1..8}-report.json`，同目录`window.json`、各`attempt-*-source.json`／日志、`receipt.json`及`cleanup-audit.json`保留窗口及源码身份。独立只读清理核对于17:22:07完成：8次均为当次容器、网络、卷、构建器容器／状态卷、镜像标签及私有目录零残留，复用前已有MinIO不可变镜像仍保留。334次成功资源样本的硬配额峰值7.8 CPU／10468982784字节（9.75 GiB），卷及私有目录实际分配峰值3637747712字节（约3.39 GiB），成功单次测量最长2.640秒；超时失败样本单独按失败记录，不从成功最大值中隐去。没有观察到OOM或4 GiB停止线触发；资源守卫超时及身份准备失败不得计为通过。


## 正式身份本地第八窗口：真实MFA与准备角色通过，成功查询被保护链拒绝（2026-10-24）

用户确认准备User仅在当次隔离环境，经真实密码／TOTP与正式IAM API取得既有`tenant.infrastructure_administrator`；普通执行User的原11项权限保持。先修规范和入口，`558cb372659a9b863d14ae78fdbe7ba1014870bf`删除不可租户定制的自定义准备角色，夹具不预授准备角色、不制造会话或AAL2。实际入口核对内置角色当前11项权限及同一Membership，自助TOTP登记替换Cookie／Access Token、验证旧令牌401；真实AAL2和有效step-up后分配限于窗口期限的角色，再经正式Refresh重读同一身份及准确授权。普通User权限在准备后再次核对。

本次唯一运行窗口为2026-10-24 20:55:05–21:55:05（Asia/Shanghai），8 CPU／12 GiB、卷和私有可写目录4 GiB停止线保持。每次运行均为根入口`make test-monitor-traces-local`、当前已提交源码私有副本、产品构建与原六后端／三页面，不复用旧产品镜像，只有固定MinIO／mc镜像按既有规则核验复用。不运行Hosted、容量或定时，不接管个人开发服务／共享数据库。三次尝试都失败，完整执行→Trace→日志→页面闭环未通过。

| 尝试 | 实际源码 | 结果与实际覆盖 |
| --- | --- | --- |
| 1 | `558cb372659a9b863d14ae78fdbe7ba1014870bf` | 7个源码二进制及依赖准备通过；`product_image_gateway`退出2，固定标记`failed to solve`／`not found`。缺少足以确定唯一根因的正文诊断，不能认定已修复镜像故障。未进入身份阶段。 |
| 2 | `25c5aab9165f582556ead36f8d5f4bb75118dfb5` | 7个二进制、10个产品镜像、浏览器依赖、11项产品Nginx路径／参数断言、缺席模块502及已有模块可读通过；六后端运行提交均匹配该源码。真实平台密码／MFA／AuthContext、普通User最小权限、准备User真实MFA／内置角色、两次旧Token拒绝、合成引擎注册与连接测试通过。成功查询编排终态不匹配，未保存进一步错误码，不计执行通过。 |
| 3 | `b57e481009c83fb200cd0ccd986937802d6c363e` | 同样完成源码／产品／实际版本／Nginx及上述五项身份和引擎断言。复现成功查询失败：父执行为`orchestrator.execution.child_failed`，Develop子执行为`develop.query.protection_required`。已实际走通正式User提交与Orchestrator派发到Develop子执行；实际子执行失败，不能算成功查询通过。 |

第3次的父执行UUID为`9fad5ff2-f121-4f67-9e81-e1c64293b7ac`，子执行UUID为`de10928e-f672-41f5-8780-6b8e0b3a5457`，仅保留身份引用、状态、步骤与稳定码。`b57e48100`补充这条失败诊断，不输出错误正文、SQL、参数或结果，原终态断言未放宽。普通User准备后仍保持原11项权限；没有为查询失败扩大权限、增加源Grant或绕过保护。由于成功查询断言已失败，未继续受控失败场景、真实检查点／自然尾采样、精确Trace／Link／日志读取、平台Trace／日志无权限与最终退出负例、真实页面闭环；浏览器接收端未观察到页面回执，不计页面通过。

代码和规范调查另确认入口漏了既有的新Tenant保护初始化前置：`docs/spec/addp数据安全与隐私保护实现规范.md`明确实例就绪不等于新Tenant已完成Owner首轮投影；Develop以30秒周期消费实际Security投影。本地夹具原来没有等待这项状态。窗口结束后，仅做代码与不启动环境的门禁：`8c2a83abe`复用既有`waitForOwnerProtection`，在导出凭据前只读等待当次Tenant、固定Develop的持久初始化checkpoint，最长60秒，仍受身份准备／总窗口预算；不开放可选Owner参数，不直接写投影／checkpoint，不把业务拒绝重试成成功。标准入口同时覆盖已有`ProtectionReadiness`单测，并补固定Owner／精确Tenant／导出顺序约束；失败报告只保留Owner已有类型化`stage/reason`，不保存SQL上下文。该准备缺口已修代码，但尚未运行验证，不能认定是实际保护拒绝的唯一根因，也不能宣称闭环已修复。

标准代码门禁分层记录：初次共享工作区的`make test-monitor-traces-local-runner`在本专项73项通过后，被无关未提交拓扑登记阻断，不计整入口通过；仓库外冻结已提交main与本次代码后，`make test-infra-runtime-log-lifecycle`的73项入口、60项生命周期、Node语法／登记及System fixture Go检查通过，冻结源码随后删除。执行诊断版本同入口74＋60通过。最终`8c2a83abe`输入在共享工作区运行同一标准入口75＋60通过，Go范围为`ExecutionTrace|ProtectionReadiness`；其余无关工作区改动未提交本专项。既有CI输入与owner登记命中，未新增workflow或Hosted调度；本次未轮询／宣称CI通过。公开产品API未变，无Swagger变更。

证据目录`/tmp/addp-p19-formal-local-20261024-window8/`：`window.json`、`attempt-{1..3}-source.json`／报告／日志、`preparation-evidence.json`／`preparation-lifecycle.log`、`execution-diagnostics-lifecycle.log`、`readiness-final-lifecycle.log`、`receipt.json`及`cleanup-audit.json`。另有初始准备冻结的输入清单／原日志，路径见`preparation-evidence.json`及`/tmp/addp-p19-mfa-preparation-path`，只保留脱敏文件，源码副本已移除。

21:52:18独立只读核对三次全部当次容器、网络、卷、构建器容器／状态卷、镜像标签及私有目录零残留，复用前已有MinIO镜像保留；页面回执读取器亦已退出。684次成功资源测量的合计硬配额峰值7.8 CPU／10468982784字节（9.75 GiB），卷及私有目录实际分配峰值3649245184字节（约3.40 GiB），成功测量最长2.769秒；没有OOM、资源采样失败或磁盘停止线触发。窗口结束后没有新增真实运行。当前状态仍部分实现；下一次完整运行必须另行确认本地窗口，重点先验证当次Owner初始化，再验证成功／受控失败查询与Trace／日志／页面，不能把本次MFA及引擎准备通过替代完整闭环。


## 正式身份本地第九窗口：构建失败、资源守卫停止与源码准入阻断（2026-10-24）

用户另行授权22:48:15–23:48:15（Asia/Shanghai）的本地窗口，8 CPU／12 GiB、当次卷及私有目录4 GiB停止线保持；不运行Hosted、容量或定时，不接管个人服务及共享数据库。原保护就绪修正`8c2a83abe`已在本次实际重建源码中，但三次尝试均未进入正式身份阶段，不能宣称当次Tenant初始化或执行闭环已运行通过。

| 尝试 | 实际源码 | 结果与实际覆盖 |
| --- | --- | --- |
| 1 | `752da6994bd72001d3cb08a7e67303f51e7830b2` | 独占依赖准备及7个源码二进制编译通过；`product_image_gateway`退出2，仍仅有`failed to solve`／`not found`固定标记，未取得可确定唯一构建根因的证据。 |
| 2 | `0569a8c72ff4060b74a1de93ce9aec2feddbd062` | 独占依赖准备通过；`product_compile`期间卷测量耗时4.105秒，超过原4秒单次预算，`resource_sample_deadline_exceeded`停止。没有放宽守卫、借用旧业务镜像或将终止算通过。 |
| 3 | `abeb0272e916a325898724c5135108a2e0a1286f` | `source_snapshot`以`commit_all_local_inputs_before_acceptance`拒绝，尚未启动环境。共享工作区`system/backend/go.mod`、`go.sum`有其他会话未提交依赖修改；保留这些改动，不代为提交、不跳过私有Owner输入一致性检查。 |

`0569a8c72`仅为原入口补充固定脱敏构建分类，包括缓存键／校验和、镜像内容摘要、匿名令牌／HEAD请求等；不导出任意错误正文、路径、SQL或秘密，不改变产品构建、资源上限及验收口径。最小充分标准门禁`make test-monitor-traces-local-runner`通过76项入口检查、Node语法、CI登记及System fixture Go检查（`ExecutionTrace|ProtectionReadiness`）。未修改Makefile或workflow，现有路径登记实际命中。先用3类含私密哨兵的样本确认分类缺失，再补实现通过；该诊断增强不代表实际Gateway构建故障已修复。只读`docker manifest inspect --insecure localhost:5001/debian-slim:bookworm`当前返回0及OCI清单，只证明当时清单可读，不证明失败时的BuildKit路径、内容或网络正常。

三次运行均走`make test-monitor-traces-local`及当次剩余绝对时间，不另开或重置60分钟。报告与独立审计确认每次当次容器、网络、卷、构建器容器／状态卷、镜像标签及私有源码目录零残留；既有MinIO镜像保持原不可变ID。181次成功测量的合计硬配额峰值7.45 CPU／8992587776字节（8.375 GiB），卷及私有目录实际分配峰值3608891392字节（约3.36 GiB），成功测量最长2.547秒；第二次失败测量另列4.105秒，不能混入成功峰值掩盖失败。没有OOM或4 GiB磁盘停止线触发。

证据目录`/tmp/addp-p19-formal-local-20261024-window9/`：`window.json`、`attempt-{1..3}-source.json`／报告／日志、`build-diagnostics-runner.log`／`build-diagnostics-inputs.json`、`source-admission-conflicts.json`、`gateway-base-manifest-readonly.json`、`function-row-mapping.json`、`receipt.json`及`cleanup-audit.json`。只保留脱敏证据；页面回执接收端在第1次结束后退出，未观察到页面回执。

截至本次记录，准备角色／真实MFA／普通User权限、成功／受控失败查询、真实阶段检查点、自然尾采样、精确Trace／Link／日志、平台无权限及正式退出负例、真实Console页面均未在本窗口运行，不得沿用第八窗口的局部通过冒充本次通过。功能清单第19行（B18，接口管理总述）、第45行（B44，实时耗时接口追踪）仍为部分实现；已向维护会话发送阶段回报，M列未据此升级。下一步先恢复已提交源码准入并取得实际构建失败分类，再补完整正式身份闭环。


## System依赖准入恢复与Gateway单项构建复现（2026-10-24）

用户要求先核对共享System依赖、恢复准入并调查Gateway构建，再在条件具备后另开原上限60分钟正式身份本地窗口。首次核对时`system/backend/go.mod`／`go.sum`仍有其他会话未提交的Avro间接依赖；未覆盖或代为提交。随后该依赖随`be747a4a2d914d67696a675b1ceea0cac732b087`落到main，两个文件干净，直接复用标准`committed_inputs`函数逐项核对通过，保留准确输入哈希。共享准入阻断已解除，但这项只读结果不代替环境运行验收。

独立调查限定2026-10-24 23:29:55–23:49:55（Asia/Shanghai）最多20分钟，仍守8 CPU／12 GiB、4 GiB停止线，第二次沿原绝对截止时间计算剩余预算，不续时。本次只从已提交源码经根`make build`编译System／Gateway／runtime-log，再用`make build-images IMAGE_BUILD_ARGS="--verify --jobs 1 --services <owner> ..."`复现原System镜像→当次BuildKit全缓存清理→Gateway镜像顺序；复用既有隔离构建器／只读卷测量／资源守卫及退出清理，不启动业务后端、数据库、身份或页面，不计完整`make test-monitor-traces-local`通过。一次性诊断脚本和报告仅在系统临时目录，不新增仓库公共验收入口。

| 单项尝试 | 实际源码 | 结果 |
| --- | --- | --- |
| 1 | `133d9c0121630fb7f0a987e74f092c667721b431` | `product_compile`期间完整测量4.059秒超过原4秒预算，守卫停止；未进入镜像构建，不计Gateway失败重现或编译通过。 |
| 2 | `be747a4a2d914d67696a675b1ceea0cac732b087` | 3个源码二进制均重新编译，System产品镜像、当次缓存清理及Gateway产品镜像全部通过；23:42:27结束。System镜像ID为`sha256:aaf46c29ca32cfad6782cdc55197f2671300b0875e3a780c3eeb197a80a787f6`，Gateway为`sha256:01b85677d78e85902c1aebf0e7adb8131e8ae882f567c13b2b7bf518f708e7de`，当次标签与资源随后全部删除。 |

两次共有153次成功资源测量，合计硬配额峰值2.25 CPU／3288334336字节（3.0625 GiB），卷及私有目录实际分配峰值2579521536字节（约2.40 GiB），成功测量最长3.345秒；第一次失败的4.059秒另列。没有OOM／4 GiB停止线触发；标准清理与独立审计均确认当次容器、网络、卷、构建器容器／状态卷、镜像标签和私有源码目录零残留，既有MinIO镜像保持。诊断第2次记录只读测量分步骤耗时，不改变四秒预算；空闲时3次本专项容器列表读取约0.029–0.032秒，仅作当时参考，不推定编译时测量超时的唯一根因。

只读读取Gateway基础镜像`localhost:5001/debian-slim:bookworm`的OCI清单、配置和全部层：配置468字节、压缩层28117255字节的HEAD／完整GET与清单大小及SHA-256均匹配。只证明检查当时仓库内容完整，不证明过去失败时的BuildKit缓存、导出或网络状态。历史窗口只保留`failed to solve`／`not found`及哈希，已清理原始错误；本次相同构建顺序成功，不能确定历史唯一根因，也不宣称修复了Gateway间歇构建故障。

为避免再丢失原因，`20a9efcc6e49743bd3846b3caafa7e8ab87b1b5c`给同一标准本地入口的`product_image_*`失败补封闭诊断：仅固定原因／失败步骤、已知产物输入名、Dockerfile行号、最多8项HTTP状态／进程退出码，未知原因明确未分类；不保存任意正文、URL、内容摘要值、凭据或SQL，不对身份／业务命令开启该分类。规范同步同次完成，原构建路线、资源及验收断言不变。先以缺失函数验证测试失败，再实现；`make test-monitor-traces-local-runner`通过79项、Node语法、CI登记及System fixture Go检查（`ExecutionTrace|ProtectionReadiness`），现有路径自动发现命中，未新增Makefile／workflow。成功复现发生在该诊断提交前，准确源码如表；这项纯失败报告变化不改变成功构建路径，也不把单测当成实际失败重现。

证据目录`/tmp/addp-p19-gateway-investigation-20261024/`：`window.json`、`attempt-{1,2}-report.json`／日志、`current-source-admission.json`、`base-blobs.json`／`base-blob-integrity.json`、`idle-daemon-readonly-timing.json`、`structured-diagnostics-runner.log`／`structured-diagnostics-inputs.json`、`receipt.json`及`cleanup-audit.json`。正式60分钟窗口尚未启动：当前构建已通过，但历史原因未定，已向用户确认是否接受这一前提作为新窗口准入。正式身份执行→Trace→日志→真实页面仍待运行，不运行Hosted、不重跑容量、不设置定时；功能清单第19、45行保持部分实现。


## 正式身份本地第十窗口：执行、Trace、日志与真实页面闭环通过（2026-10-25）

用户接受“当前源码准入和Gateway构建已通过、历史间歇失败唯一根因未定”的前提，另行授权2026-10-25 06:17:43–07:17:43（Asia/Shanghai）最多60分钟本地窗口；8 CPU／12 GiB、当次卷及私有可写目录4 GiB停止线、单次完整资源测量4秒预算保持。只运行根`make test-monitor-traces-local`，不运行Hosted、不重跑容量、不设置定时，不接管个人服务或以共享数据库承载测试。两次尝试共用同一绝对截止时间，未重置60分钟，所有产品均从各次准确已提交源码私有副本构建；仅固定MinIO／mc按原规则核验复用。本次未改产品代码、身份权限、采样或验收断言。

| 尝试 | 实际源码 | 结果与实际覆盖 |
| --- | --- | --- |
| 1 | `7b3a5d6cac630ac76edbfdebda61e2ee89f674cf` | **失败**。7个源码二进制、10个产品镜像、浏览器依赖及11项Nginx路径／参数、缺席模块502／已部署模块可读通过；`isolated_deployment_start`的容器详情检查耗时4.070秒，触发原资源守卫停止，身份与业务断言未运行。 |
| 2 | `47b02db01342a049c4fe01ae1f3ee15369b04863` | **通过**。重新完成全部产品构建、Nginx及实际版本检查；六个后端`/health/live`提交均准确匹配该SHA，12项后端断言和6项真实页面断言全部为真。06:55:09报告在完成清理后写入，早于原07:17:43截止；06:55:37独立残留审计通过。 |

第2次通过的范围：

- System-owned夹具导出凭据前，实际只读等待当次Tenant、固定Develop的保护初始化checkpoint；本次运行跨过了第八窗口尚未验的准备前置。正式密码／真实TOTP／AuthContext链路通过；准备User真实MFA后经正式IAM API给自身分配既有`tenant.infrastructure_administrator`，Refresh重读同一主体／Tenant／Membership及准确权限，MFA和角色分配后的旧Token拒绝。普通执行User仍准确持有原11项权限，没有为验收扩大授权或伪造AAL2。
- 普通User经正式API创建合成Engine上的查询任务与编排，实际完成Orchestrator→Develop的成功和受控失败父子执行；终态与父子关系一致，公开执行树DTO未输出新增私有Trace字段。成功父／子UUID为`257c2e73-ade0-426f-bde3-e90223d5d82d`／`21179a6c-028b-41ff-a6a8-172afbd790e5`，受控失败为`ffbd76ac-fc82-4a1a-9354-3f8e62f25cca`／`3afd46a8-3400-48d1-8993-4d9c411f2e0e`。仅使用已批准的合成常量查询，不将SQL、参数或结果写入证据。
- Platform系统管理员经System正式日志接口读到两Owner、两组执行的真实enqueue／claim／dispatch／terminal共16条检查点，核对实际attempt／outcome／Context。沿原v2采样自然等待125秒后，两个失败终态独立Trace均实际可查，各为一个`addp.execution`终态Span，并准确关联持久enqueue／dispatch两个Link及当前System实例身份；再以相同trace/span核对精确检查点日志。Orchestrator终态Trace／Span为`8eaca8355f7e95edf240a6a6c1673417`／`191017df7a1d550d`，Develop为`9d5510fc1f897f0a70da060bdd2db770`／`4e005bdc29851e44`。未开全采样、注入日志、直读execution私有列或手工补链。
- 普通Tenant User与Platform安全管理员读取平台Trace／日志均实际403；原User经正式API删除当次任务定义／编排后重读404；四个API客户端正式退出后旧Access Token均401。准备角色的旧Token拒绝和最终退出拒绝分别验证，不把会话撤销写成单实例撤权。
- 真实Console Chromium页面通过密码／MFA登录并核对Platform系统管理员AuthContext，Monitor iframe显示同一失败终态Span；点击因果Link前实际重核当前实例，并按目标started_at构造新查询窗口，返回受限结果；随后在System iframe显示相同execution UUID／trace／span的真实检查点日志。六项页面断言全部通过，没有mock API、注入Token或采集密码页面截图／视频／trace。

页面回执`link_result=not_found`：被点击的普通enqueue目标独立采样，本次未保留，实际查询404；它仍通过当前实例授权、精确目标时间窗及有界未保留结果断言，不表示目标Span曾实际读取成功。原普通5%／失败优先、120秒等待、72小时／20 GiB初值及5秒／2 MiB查询、原并发预算全部保持；不能以本次两组查询或未保留目标推定全调用链完整留存、未执行或生产容量。本次仍仅为本机隔离正式身份技术闭环，`formal_runner_t4=false`，不代替Hosted／正式专用Runner T4或生产T5；其他Runtime／continuous／Python异步、SQL Span及真实长期保留等既有边界不变。

两次共有507次成功资源测量；合计硬配额峰值7.8 CPU／10468982784字节（9.75 GiB），卷及私有目录实际分配峰值3652866048字节（约3.40 GiB），成功测量最长2.766秒；第一次失败的4.070秒另列，不混入成功峰值。两次均未记录OOM或4 GiB停止线触发。按记录的完成时刻减单次耗时重建测量开始间隔，两次峰值约4.018／4.017秒，低于5秒；完成时间戳间隔峰值为5.350／6.330秒，不能与开始间隔混用。第2次共287次测量，均在原4秒及资源范围内；通过不抹去第1次失败，也不宣称解决了Docker状态读取偶发超时或历史Gateway构建故障。

标准清理和独立审计均确认两次当次容器、网络、卷、构建器容器／状态卷、镜像标签和私有目录零残留，既有MinIO镜像保持原不可变ID，两个只读页面回执接收端均已退出。首次独立审计在无筛选全镜像列表读取时20秒超时，未计审计通过；随后仅对标准入口实际创建的当次产品镜像reference精确过滤，完成相同残留核验，未放宽产品资源守卫或清理标准。

证据目录`/tmp/addp-p19-formal-local-20261025-window10/`：`window.json`、`attempt-{1,2}-source.json`／报告／日志、`attempt-2-browser-receipt.json`、`receipt.json`、`cleanup-audit.json`、`evidence-review.json`、`function-row-mapping.json`及初次审计读取未完成记录。第二次run_id为`addp-trace-local-4eb709c8ced44689a5dd7fdb2884ea58`，第一次为`addp-trace-local-54dc65c1ba754a9fb05ce5a1ec0ca2e4`。交叉复核原始脱敏报告、12＋6断言、六模块SHA、原截止时间与全部资源边界，`git diff --check`通过；本次仅更新已有证据，不重复未变代码的T0/T1门禁，不调查／触发其他专项CI或Hosted。

清单已只读核对Excel实际第19行（B18，接口管理总述）与第45行（B44，实时耗时接口追踪）；两项仍为部分实现，但“正式身份本地有界执行→Trace→日志→页面闭环”可以记为已验，不能继续笼统写成本地闭环未验，也不能升级为全域追踪、正式Runner T4或生产完成。工作簿由维护会话统一回填，本会话未编辑。后续优先调查资源守卫Docker状态读取的偶发超时，在原4秒标准内补充分步骤证据后再讨论优化，不自动扩展本次运行范围。

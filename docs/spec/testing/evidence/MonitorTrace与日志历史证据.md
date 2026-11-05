# Monitor Trace 与日志历史证据

[返回 Monitor 历史证据](Monitor历史证据.md) · [返回验收证据索引](../验收证据索引.md)

本专题收录分布式追踪、Python 接入、容量基线与正式身份本地多窗口。验收结论按各记录的固定源码、环境、运行尝试和层级认定。原顺序见[Monitor 历史证据入口](Monitor历史证据.md#原记录入口)。

原始过程、逐步诊断及详细归档清单见[精简前的固定记录](https://github.com/pampa0629/addp/blob/be8f8daa4d38a7830585577f03b77bf2b5ed3931/docs/spec/testing/evidence/MonitorTrace%E4%B8%8E%E6%97%A5%E5%BF%97%E5%8E%86%E5%8F%B2%E8%AF%81%E6%8D%AE.md)。

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

## 正式身份本地窗口的共同口径

第三至第十窗口均沿 `make test-monitor-traces-local`。各窗口的尝试共用原授权的绝对截止时间，资源上限为 8 CPU／12 GiB、当次卷及私有可写目录 4 GiB 停止线；单次完整资源检查限四秒。记录采样间隔时按开始时间判定，完成时间戳间隔单列。失败样本、受控中断和未启动均按原结果记录。

固定源码、实际构建、依赖就绪、登录和完整业务链按各次尝试分别认定。准备或构建修复后的确定性门禁不能回填到历史运行；经核验复用的 MinIO／mc 镜像也不能计为本次源码重建。各窗口的零残留记录只涉及当次归属的容器、网络、卷、构建器及其状态、产品镜像标签和私有工作目录；保留原有已核验 MinIO 镜像。标准退出和独立复核分列，有清理或审计失败时保留原结果。

本段记录的是本机隔离验收；未运行 Hosted、正式专用 Runner T4、生产 T5、长期留存或新的容量验收。时间均为北京时间。各窗口的实际运行按序对应归档目录中的 `attempt-N-report.json`（N 为尝试编号）。表中源码未单列时，以对应尝试报告为准，后继修复版本单列。原逐次过程、日志摘要与报告清单见[整理前固定快照](https://github.com/pampa0629/addp/blob/90d13c2545c602eddcc17a81621a135ab64fa540/docs/spec/testing/evidence/MonitorTrace%E4%B8%8E%E6%97%A5%E5%BF%97%E5%8E%86%E5%8F%B2%E8%AF%81%E6%8D%AE.md)。

## 正式身份本地第三窗口：镜像核验、守卫修复与部署配置拒绝（2026-10-23）

授权 19:18:00–20:18:00。六次标准运行均失败；尝试 1–5 的源码未在旧正文单列。

| 尝试 | 实际首败或停止点 | 完整成功测量 |
| --- | --- | ---: |
| 1 | Orchestrator 产品镜像构建时卷测量解析异常，底层原因未定。 | 129 |
| 2 | System 镜像前私有目录 du 非零退出。 | 38 |
| 3 | 对象缓存清理超出 0.75 秒；该过短期限方案后来撤掉。 | 41 |
| 4 | 全部产品镜像构建后，主动停止 BuildKit 被误判为容器失败。 | 169 |
| 5 | 前置停止检查复现 BuildKit 退出 1、非 OOM，昂贵编译未开始。 | 未记录 |
| 6 | 固定 `4aee4813909077cd3fa35906bf70353278ba89e2`；16 个产品镜像与浏览器依赖通过，Trace 部署生成因误传 `module` 被正式生成器拒绝。 | 180 |

实际核验 ARM64 MinIO/mc 镜像 `sha256:dd87261892226d0e651ba1d059205cdd4e93d94be3dd9c819abffbf23d994d0b`。只读、无网络核验匹配 MinIO `RELEASE.2025-10-15T17-29-55Z`／`9e49d5e7a648f00e26f2246f4dc28e6b07f8c84a` 和 mc `RELEASE.2025-08-13T08-35-41Z`／`7394ce0dd2a80935aded936b09fa12cbb3cb8096`。

当次修正只在剩余四秒内对明确 ENOENT 完整重扫。第 4 次私有目录重扫后耗时 0.508 秒，未发生卷内重扫；该次最长测量 1.055 秒、最大完成间隔 4.24 秒。BuildKit 的退出 1 仅在当次精确 ID、明确主动停止且非 OOM 时接受，第 6 次前后检查通过，最长测量 0.807 秒、最大完成间隔 4.43 秒。最终 `162cb8747d211d3fe184e8acba48117f6123e934` 改用 `module_name` 并将配置校验前移，旧字段反例失败、修正后生命周期 55＋60 和身份 Go 等通过；完整运行未重跑。

六次均经标准清理和独立审计核验零残留。成功样本峰值 6.25 CPU／10.0625 GiB、私有可写约 3.19 GiB，仅覆盖准备与构建。正式密码／MFA、成功／受控失败执行、Trace／日志和页面未运行。实际报告、回执和红绿日志见 `/tmp/addp-p19-formal-local-20261023-window3/`。

## 正式身份本地第四窗口：依赖核验通过，完整闭环未验收（2026-10-24）

授权 10-23 23:54:17–10-24 00:54:17。九份完整入口报告均为 failed，尝试 6、9 为 `local_acceptance_interrupted`。早期尝试源码以各自报告为准。

| 尝试 | 实际首败或停止点 |
| --- | --- |
| 1 | Compose 插值未选中的 capacity profile 挂载，缺 `TRACE_T2_WORK` 拒绝。 |
| 2 | 完成 16 镜像；成功退出的 oneoff 自动删除，与守卫列举后 inspect 竞态。 |
| 3 | 完成 16 镜像；依赖启动失败。 |
| 4 | Nginx 前置 `nginx -t` 在 UID 0 且无 capability 时 chown 被拒。 |
| 5 | Nginx 前置校验再次失败。 |
| 6 | 旧顺序的构建在余量不足时受控中断。 |
| 7 | 新顺序的 Loki 启动权限失败。 |
| 8 | Loki 启动 permission denied；UID 10001 与卷的 root:root／755 不符。 |
| 9 | 产品配置、MinIO／mc、Nginx、PG／Redis／MinIO、两存储初始化与遥测依赖通过，`dependencies_ready=true`。进入编译后因余量不足受控停止。 |

修正保留当次成功退出的一次性容器至统一清理；trace-query 用宿主私有证书所有者身份，仍保持只读及 capability 限制。Loki／Tempo 当次空卷统一 `nocopy`，由原初始化路径设置权限；第 9 次实测两卷均为 10001:10001／750，依赖保持运行。原独占依赖前移到编译前，仍复用同一 Compose／网络／卷，不使用 trace-load。

最终代码 `f07a5a6cc201a0ed676f0ad013ca5ddfcf3b23a8` 的标准生命周期 58＋60 和身份 Go 等通过。第 9 次 24 项成功测量最长 0.880 秒，峰值 7.45 CPU／9.375 GiB、私有可写 1,085,509,632 字节。九次标准清理与独立审计均零残留；无正式登录、业务执行、Trace／日志或页面证据。实际报告、卷审计和清理见 `/tmp/addp-p19-formal-local-20261023-window4/`。

## 正式身份本地第五窗口：标准数据库初始化修正，页面部署依赖仍阻断（2026-10-24）

授权 08:28:23–09:28:23。三份完整报告均为 failed。

| 尝试 | 实际源码 | 已达阶段与首败 |
| --- | --- | --- |
| 1 | `1cf5be3660172abfc0e57b1fd9e3fb1be65e5f31` | 独占依赖和七个二进制通过；System 镜像 `command_failed_2`，只留输出哈希，具体原因未定。 |
| 2 | `8a3006670e7f6cfaddf018253515ccb37ab5bc78` | 16 镜像和浏览器依赖通过；Develop 启动退出 1、非 OOM，Docker stdout 为空，随后补了从当次日志卷读封闭类别及哈希的诊断。 |
| 3 | `2f9414696911a4701b23ce19081c2cab5f1f7925` | 原 PostgreSQL 初始化、七个二进制、16 镜像、浏览器依赖及六后端就绪通过；`isolated_deployment_start` 中 Meta、Service 页面退出 1、非 OOM，封闭标记 `not found`。 |

源码确认原入口漏执行 `scripts/infra/init-postgresql.sql`。第 3 次仅对独占平台库用 `ON_ERROR_STOP=1` 执行原 SQL，核验 Develop schema、PostGIS、pgvector 和标准更新时间触发器。页面阻断来自根 Nginx 静态依赖旧页面、这些页面又静态代理未部署后端；该窗口未改部分部署策略。准备门禁 60 项、标准生命周期另 60 项及身份 Go 等通过。

578 项成功测量最长 1.597 秒，峰值 7.45 CPU／9.375 GiB、私有可写 3,635,802,112 字节，未触停止线。三次标准清理及独立审计均零残留。实际运行提交探针、密码／MFA、执行、Trace／日志及 Console 均未到达。原件 `/tmp/addp-p19-formal-local-20261024-window5/`，含第 3 次数据库初始化审计。

## 正式身份本地第六窗口：按需页面及正式登录通过，执行闭环仍未验收（2026-10-24）

授权 09:27:58–10:27:58。三份完整报告均为 failed。

| 尝试 | 实际源码 | 已达阶段与首败 |
| --- | --- | --- |
| 1 | `6621748b0b1a8497597e4f4ee5bb3274503402ff` | 七二进制、10 镜像、11 项 Nginx、六后端版本／四页面就绪通过。两个 Platform User 真实密码／TOTP、两个 Tenant User 密码、AuthContext 和普通业务 User 精确 11 项权限通过。首败 `synthetic_engine_registration` 的 SuiteError，未保存 HTTP 状态，不能认定实测 403。 |
| 2 | `6b7704611ff20856cfc03db9f4c3b6cfd1150e3a` | Security 镜像构建失败，仅有 `failed to solve`／`timed out`，具体请求与根因未定；未复验新的准备角色。 |
| 3 | `1c5a5cc26dd7291f0a142a5a67adea1a908d3d2b` | 计划停止构建器后用 2 CPU／3 GiB 编译，恢复原 1 CPU／2 GiB 构建器，七二进制及 10 镜像通过；`browser_dependencies` 到总期限，`command_deadline_exceeded`。 |

`e25a09264e39a0c48e01af696b87df15c1b5f103` 改根 Nginx 七个旧页面为请求时 Docker DNS，隔离入口只选 Console／System／Monitor 和根 Nginx。实际 11 项路径、查询、静态资源／plugins／WASM、API、缺模块 502 及可用模块通过，探针已清理。早期探针首败为重试 stderr 污染和回显漏 Gateway 8000。共享准备门禁曾被其他未提交登记阻断，只计 63 项本专项；同固定源码私有副本后来通过 63＋60、登记及身份 Go。最终输入 66＋60 通过。

第 2 次引入的自定义角色只拟授 `system.engine.create`／`system.engine.execute`，普通执行 User 权限保持；其真实目录冲突见第七窗口。既有 Release and T2 run `40050940303` 仅记录到 queued，未形成 Job 通过证据。

516 项成功测量最长 1.126 秒，峰值 7.8 CPU／9.75 GiB、私有可写 3,619,434,496 字节，无 OOM 或触线。三次标准清理及独立审计均零残留。登录局部通过；成功／受控失败执行、Trace／Link、精确日志、Console 闭环及退出后旧令牌拒绝未通过。原件 `/tmp/addp-p19-formal-local-20261024-window6/`。

## 正式身份本地第七窗口：准备守卫修正及IAM权限冲突，闭环未通过（2026-10-24）

授权 16:32:47–17:32:47。八次尝试全部失败。

| 尝试 | 实际源码 | 首败或停止点 | 完整成功测量 |
| --- | --- | --- | ---: |
| 1 | `c93da8151` | `isolated_dependencies_start`，`prepare_volume_sampler` 超预算。 | 2 |
| 2 | `c93da8151` | 同一卷测量器准备超预算。 | 2 |
| 3 | `b5b543863501d371d851a9dd69601f55875677f3` | 依赖已就绪，`product_compile` 的 `measure_owned_volumes` 超时。 | 71 |
| 4 | `cdd038f22` | `bounded_builder/command_deadline_exceeded`，旧报告无法进一步定位。 | 0 |
| 5 | `3a5cd4c54` | `bounded_builder/command_deadline_exceeded`。 | 0 |
| 6 | `5a1a4c0330809c836edb13e08ec4b1e3b578b1a8` | `initial_resource_sample/command_deadline_exceeded`。 | 0 |
| 7 | `4108c283a183e4bfc89e76a3b38aa7cc1f6e48a8` | `isolated_dependencies_start` 退出 2，封闭标记为空，具体命令与根因未定。 | 6 |
| 8 | `d9a3cec22e12db098182e25bc96a62e0916f5f32` | 七二进制、10 镜像、浏览器依赖、11 项 Nginx 与六后端运行提交核验通过；`formal_identity_preparation` 退出 1，封闭标记为空。 | 253 |

修正依次将当次空卷／构建器前移，将限额 0.25 CPU／64 MiB、准入 30 秒的只读卷辅助容器准备和首次测量分离，并补固定操作、耗时与依赖阶段诊断。最终标准生命周期 70＋60、登记及身份 Go 等通过；中间 67／68／69 各自通过，均为确定性证据。

目录核对确认旧 `6b7704611ff20856cfc03db9f4c3b6cfd1150e3a` 的自定义角色使用了不可租户定制的权限，会被 `validateCustomizablePermissions` 拒绝。历史运行回执不足以确认这是尝试 8 的唯一原因。既有基础设施管理员角色当时仅为待确认方案，下一窗口才验证。

17:22:07 独立审计确认八次零残留。334 次成功测量最长 2.640 秒，峰值 7.8 CPU／9.75 GiB、私有可写 3637747712 字节；超时样本另列，无 OOM 或 4 GiB 触线。正式登录、业务执行、Trace／日志、页面及退出拒绝未到达。逐次源码／报告及清理见 `/tmp/addp-p19-formal-local-20261024-window7/`。

## 正式身份本地第八窗口：真实MFA与准备角色通过，成功查询被保护链拒绝（2026-10-24）

授权 20:55:05–21:55:05。三次完整报告均为 failed。

| 尝试 | 实际源码 | 已达阶段与首败 |
| --- | --- | --- |
| 1 | `558cb372659a9b863d14ae78fdbe7ba1014870bf` | 七二进制及依赖通过；`product_image_gateway` 退出 2，`failed to solve`／`not found`，唯一原因未定。 |
| 2 | `25c5aab9165f582556ead36f8d5f4bb75118dfb5` | 七二进制、10 镜像、浏览器、11 项 Nginx、六后端版本，以及正式身份与引擎断言通过；成功查询编排终态不匹配，无进一步错误码。 |
| 3 | `b57e481009c83fb200cd0ccd986937802d6c363e` | 同类准备和正式身份断言通过；成功查询父执行 `orchestrator.execution.child_failed`，Develop 子执行 `develop.query.protection_required`。父 UUID `9fad5ff2-f121-4f67-9e81-e1c64293b7ac`，子 UUID `de10928e-f672-41f5-8780-6b8e0b3a5457`。 |

尝试 2、3 实测准备 User 经密码／TOTP、有效 step-up 和正式 IAM API 获当次限时 `tenant.infrastructure_administrator`，刷新后同一 Membership 和准确权限，两个准备阶段的旧 Token 拒绝；普通执行 User 保持原 11 项。合成引擎登记和连接通过。尝试 3 验到提交及派发到 Develop，子执行仍拒绝。

调查确认夹具漏等新 Tenant 的 Owner 首轮投影。窗口后 `8c2a83abe` 复用既有 `waitForOwnerProtection`，导出凭据前只读等待固定 Develop checkpoint，最长 60 秒且不超原预算。该缺口修正不确证历史拒绝的唯一根因。共享 runner 初次在本专项 73 项后被登记阻断；冻结输入 73＋60、诊断版 74＋60、最终 75＋60 与身份 Go 通过，尚无该修复后的实际身份运行。

21:52:18 独立审计确认三次零残留，页面回执读取器已退出。684 项成功测量最长 2.769 秒，峰值 7.8 CPU／9.75 GiB、私有可写 3649245184 字节，无 OOM、采样失败或触线。受控失败执行、检查点／尾采样、Trace／Link、精确日志、无权限与退出负例及真实 Console 未到达。原件 `/tmp/addp-p19-formal-local-20261024-window8/`；冻结输入 `/tmp/addp-p19-mfa-preparation-path`。

## 正式身份本地第九窗口：构建失败、资源守卫停止与源码准入阻断（2026-10-24）

授权 22:48:15–23:48:15。三次尝试均未进入正式身份阶段。

| 尝试 | 实际源码或准入基线 | 已达阶段与首败 |
| --- | --- | --- |
| 1 | `752da6994bd72001d3cb08a7e67303f51e7830b2` | 独占依赖及七二进制通过；Gateway 镜像退出 2，仅 `failed to solve`／`not found`，历史根因未定。 |
| 2 | `0569a8c72ff4060b74a1de93ce9aec2feddbd062` | 独占依赖通过；`product_compile` 卷测量 4.105 秒，`resource_sample_deadline_exceeded`。 |
| 3 | `abeb0272e916a325898724c5135108a2e0a1286f` | `source_snapshot/commit_all_local_inputs_before_acceptance`，共享 System go.mod／go.sum 含未提交改动；环境未启动。 |

重建输入已包含 Owner 就绪修正 `8c2a83abe`，但未到实际检查。`0569a8c72` 的构建诊断红绿及标准 runner 76 项、身份 Go 等通过。只读 registry manifest 可读，只反映检查时刻；Gateway 独立构建见下节。

三次均核验零残留，含第三次未创建环境；页面读取器在第一次后退出，无页面回执。181 项成功测量最长 2.547 秒，峰值 7.45 CPU／8.375 GiB、私有可写 3608891392 字节；4.105 秒失败样本单列，无 OOM 或触线。真实登录、保护就绪、两类执行、Trace／日志、页面与退出负例未运行。报告、构建诊断和清理见 `/tmp/addp-p19-formal-local-20261024-window9/`。

## System依赖准入恢复与Gateway单项构建复现（2026-10-24）

System 依赖随 `be747a4a2d914d67696a675b1ceea0cac732b087` 入库后，标准 `committed_inputs` 核验通过。独立诊断只覆盖 23:29:55–23:49:55 的构建窗口，沿原资源线和同一绝对期限；未启动数据库、身份、后端或页面。

| 单项尝试 | 实际源码 | 实际结果 |
| --- | --- | --- |
| 1 | `133d9c0121630fb7f0a987e74f092c667721b431` | `product_compile` 的资源测量 4.059 秒，守卫停止；未到镜像。 |
| 2 | `be747a4a2d914d67696a675b1ceea0cac732b087` | 三二进制、System 镜像、当次 BuildKit 全缓存清理和 Gateway 镜像均通过，23:42:27 结束并清理。 |

System／Gateway 产物分别为 `sha256:aaf46c29ca32cfad6782cdc55197f2671300b0875e3a780c3eeb197a80a787f6`、`sha256:01b85677d78e85902c1aebf0e7adb8131e8ae882f567c13b2b7bf518f708e7de`。两次标准清理与独立审计均零残留。153 项成功测量最长 3.345 秒，峰值 2.25 CPU／3.0625 GiB、私有可写 2579521536 字节；失败样本单列。

只读 OCI 清单、配置及层的大小和摘要匹配，证明核对时内容完整，历史 BuildKit 故障根因仍未知。`20a9efcc6e49743bd3846b3caafa7e8ab87b1b5c` 后补唯一入口的封闭构建诊断，标准 runner 79 项、语法／登记及身份 Go 通过；成功构建先于此修正。两次报告、OCI 校验、诊断红绿及清理见 `/tmp/addp-p19-gateway-investigation-20261024/`。正式完整窗口独立认定。

## 正式身份本地第十窗口：执行、Trace、日志与真实页面闭环通过（2026-10-25）

授权 06:17:43–07:17:43。两次实际运行分别使用下列固定源码和 run_id。

| 尝试 | 实际源码及 run_id | 结果与实际覆盖 |
| --- | --- | --- |
| 1 | `7b3a5d6cac630ac76edbfdebda61e2ee89f674cf` · `addp-trace-local-54dc65c1ba754a9fb05ce5a1ec0ca2e4` | **失败**。七二进制、10 镜像、浏览器依赖和 11 项 Nginx 通过；`isolated_deployment_start` 容器详情检查耗时 4.070 秒，守卫停止，未运行身份和业务断言。 |
| 2 | `47b02db01342a049c4fe01ae1f3ee15369b04863` · `addp-trace-local-4eb709c8ced44689a5dd7fdb2884ea58` | **本机闭环通过**。产品构建及 Nginx 通过，六后端 `/health/live` 均匹配该 SHA；12 项后端、6 项真实页面断言通过。06:55:09 清理后写报告，06:55:37 完成独立残留审计。 |

**第 2 次实际业务覆盖**

- System-owned 夹具在导出凭据前只读等待当次 Tenant 的固定 Develop 初始化 checkpoint。准备 User 经正式密码／TOTP 和 IAM API 获既有 `tenant.infrastructure_administrator`，Refresh 核对同一主体、Tenant、Membership 及精确权限，两阶段旧 Token 拒绝；普通执行 User 仍为原 11 项权限。
- 普通 User 经正式 API 在合成 Engine 上实际完成 Orchestrator → Develop 的两组父子执行。成功父／子 `257c2e73-ade0-426f-bde3-e90223d5d82d`／`21179a6c-028b-41ff-a6a8-172afbd790e5`，受控失败父／子 `ffbd76ac-fc82-4a1a-9354-3f8e62f25cca`／`3afd46a8-3400-48d1-8993-4d9c411f2e0e`。终态及父子关系一致，公开 DTO 未泄漏私有 Trace 字段。
- Platform 系统管理员经 System 日志接口读到两 Owner、两组执行的 16 条 enqueue／claim／dispatch／terminal 检查点。原 v2 自然等待 125 秒后，两组失败终态各查得一个 `addp.execution` Span、两个持久 enqueue／dispatch Link 和对应精确日志。Orchestrator Trace／Span 为 `8eaca8355f7e95edf240a6a6c1673417`／`191017df7a1d550d`，Develop 为 `9d5510fc1f897f0a70da060bdd2db770`／`4e005bdc29851e44`。普通 Tenant User 和 Platform 安全管理员读诊断实际 403；原 User 删除任务／编排后重读 404，四个 API 客户端正式退出后旧 Token 均 401。
- Chromium Console 真实密码／MFA 后在 Monitor iframe 读取同一失败终态 Span，在 System iframe 查得相同执行／Trace／Span 的检查点日志。Link 点击重核当前实例并用目标 `started_at` 查询；`link_result=not_found`，目标 enqueue Span 独立采样，本次未留存，实际 404。此处通过的是授权、精确时间窗和有界未保留结果断言，未实际读出目标 Span。

两次共 507 项成功测量，第 2 次 287 项。峰值 7.8 CPU／9.75 GiB、私有可写 3652866048 字节，成功最长 2.766 秒；第一次的 4.070 秒单列。两次开始间隔峰值约 4.018／4.017 秒，完成间隔峰值 5.350／6.330 秒，按不同口径记录。无 OOM 或 4 GiB 触线，历史 Docker／Gateway 间歇根因未定。

两次标准清理及最终独立审计均零残留，页面回执接收端已退出。首次独立审计全量列镜像时超出 20 秒，后按实际产品镜像 reference 精确过滤完成核验，原审计超时保留。原始源码／报告、浏览器回执和审计见 `/tmp/addp-p19-formal-local-20261025-window10/`。

第 2 次支持本机隔离正式身份的执行 → Trace → 日志 → 页面闭环，`formal_runner_t4=false`。Hosted／正式专用 Runner T4、生产 T5、其他 Runtime／continuous／Python 异步、SQL Span、全调用链留存及真实长期保留均未覆盖。普通 5%／失败优先、120 秒等待、72 小时／20 GiB 初值、5 秒／2 MiB 查询与原并发预算保持。清单第 19 行（B18）和第 45 行（B44）仍为部分实现。

<a id="python-trace-certificate-fixture-20261102"></a>

## 2026-11-02 Common Python Trace证书夹具修正（本地及自然Linux通过）

用户明确将新发现的Common Python HTTP Trace失败纳入本会话修复，限原标准本地测试与回环接收器；不安装／切换依赖、不改全局环境或产品TLS，不重试镜像构建、开产品窗口或运行Hosted／容量／定时。先修T0–T3测试条款，再分开处理证书准备和导出接收失败。

原件一为既有intersection隔离源码门禁的441 passed／1 skipped／17 setup errors／54 subtests，89884字节，SHA256=`7b774735289a3328b1d98a2e368e065ef5537f3a219f73804270883b5e80b33c`；错误在OpenSSL req准备证书，夹具依赖本机缺失的默认openssl.cnf。原件二是在另一会话仅对那次测试子进程设置OPENSSL_CONF=/dev/null后的4 failed／454 passed／1 skipped／54 subtests，7020字节，SHA256=`ceeb0eb4b6fa486d3b0114f580f1352899e07b5ac0a3ffe2de12d65e6804560e`。两份首失败独立保留，不把/dev/null作为正式方案，也不把这些本地结果当作Linux CI。

本轮先让req显式绑定当次私有配置、全部OpenSSL子进程使用局部OPENSSL_CONF，并仅在测试侧观察真实HTTPX流调用的封闭异常类别／TLS验证码；先保留原CA生成语义，运行一次原`make test-common-python`。同样四项稳定复现：官方protobuf／身份／隐私投影接收以及302、503、延迟2.3秒三例，均接收0次；4 failed、454 passed、1 skipped、54 subtests，15.31秒。新增事实为四项均捕获TLS verify_code=79，客户端SSL所对应的本机OpenSSL头文件将79定义为INVALID_CA。force_flush为真只代表outstanding归零，不能替代exported成功；失败在TLS验证阶段，尚未进入HTTP接收断言，不据原日志猜测代理、并发或重试问题。

最小修正只在测试证书夹具：自签根明确critical CA:TRUE及keyCertSign／cRLSign，叶子明确CA:FALSE、数字签名／密钥用途、各自serverAuth／clientAuth及密钥标识，原localhost／127.0.0.1名称不变。私有目录0700／材料0600，准备失败或退出均清理；每测试私有目录包括故意移出／硬链接的测试私钥一并销毁，不触及仓库或其他目录。新增约束检查、子进程配置／父环境不污染、准备失败清理三个回归。原mTLS／官方protobuf、身份与安全投影、外域／代理隔离、单次接收、无重试／重定向、2秒导出及flush／退出上限的全部原断言未改；只在原接收断言附封闭诊断。

| 本轮标准门禁 | 结果及边界 |
| --- | --- |
| 私有配置与诊断阶段 `make test-common-python` | 同四项失败，均TLS 79；无默认配置setup errors，未放宽接收次数 |
| 最终 `make test-common-python` | 461 passed、1 skipped、54 subtests passed，15.28秒；原四项均通过 |
| 跳过项 | 原有可选GeoPandas测试因缺少该依赖跳过，不安装依赖，不计通过 |
| AST／来源核验 | 原所有既有test函数的断言条件逐项相同；产品httptrace/runtime.py逐字节未改，pyproject及依赖／产品TLS／预算不变 |
| CI登记 | 现有Platform的Common Python tests作业通过共享变更矩阵命中本测试路径，沿make test-common-python；无需新增入口或编排 |

本地门禁在共享工作区执行，未提交的workflow_runtime/errors.py为其他会话内容，不混入本批提交；该本地结果不替代最终提交净快照的Linux验证。客户端解释器Python3.12.7／OpenSSL3.0.16、HTTPX0.28.1、Otel SDK／proto1.45.1及protobuf7.36.2均沿现有安装，证书命令为现有OpenSSL4.1.0，无版本或环境切换。受控复现证明当前夹具的无效CA问题，并由显式约束修正；原件二没有保留底层异常，不追填其历史TLS数值。

私有证据目录`/var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/addp-p19-common-python-trace-evidence-20261102-5vrcf3cb/`（0700／0600），含两份原件、isolated_config_diagnostic.log、final.log及audit.json。诊断复现8210字节，SHA256=`f69688b4be216d723110b6aafe38417f4722e8ef739cb94492d92800c57f7b69`；最终日志638字节，SHA256=`a0e0dc9ae41701afc7f469c1f8858b72fdfe1f2b070dac18ac2b45b41eaa7637`；最终测试文件SHA256=`9ad58b363494566227ff98411a38686efe46973944c9da12a7eb727850b2e841`。本批仅测试夹具、T0–T3条款及两份既有证据，不改变生产导出器，也不增加正式产品／Hosted／容量通过结论；自然Linux结果另记。

自然Linux复核：四个自有路径已提交推送为`a5945380cbf5e415a93a73a2d91200d3e8173ee2`。[Platform自然push／Common Python tests作业125094948224](https://github.com/pampa0629/addp/actions/runs/41388636491/job/125094948224)实际checkout该完整SHA，经共享矩阵选中并执行原`make test-common-python`，北京时间2026-11-02 12:28:17—12:30:01作业success；原始测试输出为461 passed、1 skipped、54 subtests passed／18.08秒。测试文件逐字节匹配上述本地通过的9ad58b36摘要；这次干净提交快照也通过，不依赖共享工作区未提交的workflow_runtime/errors.py。日志已独立保存为上述私有证据目录linux.log，59326字节，SHA256=`df702cefad3b7637e33c769427bad0144b985e8dd31d0e4e098ffd604c664fe3`。本条只认定Common Python命名作业及原四项回归通过，不推导整个Platform workflow、另一组HTTP／Trace本地runner、交叉语言实链、产品页面、Hosted或容量通过；APT历史网络问题与SDK HTTP T2目录消失仍独立保留。

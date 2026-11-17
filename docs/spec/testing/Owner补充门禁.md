# ADDP Owner 补充门禁

[返回测试与验收总入口](../addp测试与验收规范.md)

本页只补充各 owner 的标准入口、独有断言及未覆盖项。通用隔离、身份、首败保留、成败清理、报告及触发规则沿[T0–T3 测试规范](T0-T3测试规范.md)和[T4 Online 验收规范](T4-Online验收规范.md)，入口／输入发现及 `make test-changed` 的验证规则沿[CI与交付规范](CI与交付规范.md)。

本文是验证要求，不是通过清单：确定性、真实来源T2、受控页面T3及正式身份T4分别认定；源码／接线存在、后轮成功、清理完成都不替代原失败或未执行项。


| 门禁类别 | 文内入口 |
| --- | --- |
| Runtime 与原生转换 | [Model3D Runtime 确定性门禁](#model3d-runtime-确定性门禁)、[Manager Raster Mosaic 原生 Runtime 门禁](#manager-raster-mosaic-原生-runtime-门禁)、[SuperMap Runtime 确定性门禁](#supermap-runtime-确定性门禁)、[GeoPython Runtime 确定性门禁](#geopython-runtime-确定性门禁)、[Document 原生转换门禁](#document-原生转换门禁) |
| 基础设施与数据引擎 | [FTP／显式 FTPS 准入门禁](#ftp显式-ftps-准入门禁)、[Business Redis 门禁](#business-redis-门禁)、[Elasticsearch 门禁](#elasticsearch-门禁)、[Redis 键值数据集门禁](#redis-键值数据集门禁)、[平台指标设施独占门禁](#平台指标设施独占门禁)、[Spark 联邦查询技术准入](#spark-联邦查询技术准入)、[Spark 数据发布门禁](#spark-数据发布门禁)、[关系型写入与安全诊断门禁](#关系型写入与安全诊断门禁)、[Oracle 原生类型与空间门禁](#oracle-原生类型与空间门禁) |
| Owner 数据、安全与界面 | [Meta 扫描与结果归属门禁](#meta-扫描与结果归属门禁)、[Security 治理与识别门禁](#security-治理与识别门禁)、[业务图双引擎领域验收](#业务图双引擎领域验收)、[Graph/Cleanup 执行管理最小验证矩阵](#graphcleanup-执行管理最小验证矩阵)、[Manager 预览浏览器门禁](#manager-预览浏览器门禁)、[Manager 快显回归与基准验证](#manager-快显回归与基准验证)、[租户执行过程事件门禁](#租户执行过程事件门禁)、[保护投影与执行诊断门禁](#保护投影与执行诊断门禁) |

## Graph/Cleanup 执行管理最小验证矩阵

契约正文：[Graph 构建](../tasks/模块任务契约.md#graph-构建任务)、[公共 lease](../tasks/执行记录与租约.md#bounded-execution-领取租约和恢复契约)、[Cleanup 持久协议](../addp-cleanup体系规范.md#持久队列与完成回执)。本节只列必须断言及缺口；固定源码、实际 Job 和结果统一见[执行管理专题](../../next/ADDP任务执行管理统一收口专题.md#六剩余工作与共享环境)。已有用例不表示当前 HEAD 或 Hosted 全部通过。

| 责任／层级 | 标准入口 |
| --- | --- |
| Graph T1／模块门禁 | `make test-module MODULE=graph` |
| Common／Graph PG T2 | `make test-common-postgres`；定向 `make test-common-postgres COMMON_POSTGRES_TEST_ARGS="--test graph"` 仅证明该组 |
| System Cleanup T1／PG T2 | `make test-module MODULE=system`、`make test-system-cleanup`；PG 必需且拒绝 skip，IAM PG 不替代 |
| 原生效果 T2 | `make test-common-falkordb`、`make test-common-neo4j` |
| 安全读取与页面 | `make test-monitor-postgres` 及 System／Monitor／Graph 模块门禁；共享组件按消费者扩散 |
| 故障 T4 | `make test-graph-online-runner` 仅验证编排；实际隔离 Hosted 另行授权，受控与原生效果分别认定 |

PG 竞争须用真实双连接锁门闩／屏障，不能用 SQLite、SQL 字符串或 sleep 推断。Graph PG 分组须命中 repository 的 `^TestIntegrationPostgresGraph` 及 service 的 `^TestIntegrationPostgresGraph(BuildInputRuntime|CleanupRuntime)$`，两个 service 主用例明确 PASS；新增包／函数仍须核对筛选。Common 改动按真实消费者扩散，不以 Graph 定向组替代。

| 编号／主责 | 最小断言 | 已有覆盖与未覆盖项 |
| --- | --- | --- |
| G1 Graph | 用户／TaskProvider 只入唯一 pending，提交后可恢复发现，claim 前无运行体或外部操作 | 创建／监督器已有；真实退出、重启 T4 待验。 |
| G2 Common＋Graph | 双 Backend 仅一方 claim；attempt／token／started_at／摘要同事务，任一步失败三表回滚且仍可首次 claim | 双领取与摘要回滚已有 PG。 |
| G3 Graph | attempt0 未启动取消不造耗时；claim 先提交则持久受理并等真实停止；success 先提交不改结果／摘要／控制记录 | 两序及 success 优先已有 PG；曾运行或缺证历史不能套 pending 例外。 |
| G4 Graph＋System | 跨 Backend／重复取消绑定 execution／attempt；换代、跨 Tenant、缺权限均拒绝，不伤新执行、不借服务身份扩权 | 受控跨 Backend 已接；真实身份变化 T4 待验。 |
| G5 Common＋Graph | 等 G／业务锁后失租，heartbeat、材料、审核、计数、事件及终态全拒绝，私有写入不得先提交 | 材料／审核／进度／终态已有 PG；全锁排列未齐。 |
| G6 Graph＋Provider | 外部写入后回执丢失、取消超时或失联执行失败且效果待核清；拒绝伪造／错 attempt／不完整停止证据，禁止重放、人工重跑及 Cleanup | 受控窗口已接；原生丢回包 runner／证据缺失，恢复暂缓；mock／PG 不证明远端停止。 |
| G7 Graph | 旧实例仅追加绑定停止证据，不补结果／终态；TTL、清 lease、退出或布尔确认不解锁，核清后不自动继续 | known 核清／拒绝有 T1／PG；可信绑定及 positive reopen 暂缓，须独立 Permission／原目标／服务端证据。 |
| G8 Graph | 材料字节、冻结本体映射及目标漂移拒绝；活动材料禁删改，快照无秘密，正常编辑仍用冻结映射 | 同长字节／登记漂移已有 PG；System／Copilot 受控，物理代际／原生效果另验；不认证 Ontology 不可变发布。 |
| G9 Graph | 构建终态仍可审核；批准／拒绝／修改唯一裁决；关闭拒新审核、在途不明阻清理，不隐式清空审核或已提交图数据 | 两序、回滚及九种审核竞争已有 PG；外部效果为夹具，原生故障另验。 |
| C1 System＋Common | 请求／父／全部 expected 子 pending 同事务；同父同 Owner 唯一，来源事件去重、同 ID 异内容拒绝，同参独立动作不误合并 | 原子／幂等已接；不为旧 Stream 增加 Redis 路线。 |
| C2 System＋Common＋Owner | P→精确 C 串行化父关闭／领取；未领取只拒绝为 failed，不造 claim／回执；已领取按自身 lease 收尾，失联不重放 | 公共原语已有；逐项交错见下表。 |
| C3 Graph＋Common | 先提交 G 关闭再取消；统一 G 围栏覆盖入队／审核／删除／重绑，无控制行不绕过，持 G 不反锁构建 E | 入队及任务／材料／图配置删除已有 PG；跨类锁排列、目标重绑待补。 |
| C4 Graph＋Common | 清理重核无活动／未决及原 scan 摘要；PG、metadata 回执、子终态原子回滚；未停止不伪造取消／删任务 | 父正常／截止下收尾回滚已有 PG；scan 漂移严格拒绝，已提交事实不撤回；新图围栏及其他最终竞争待补。Engine 仅归档、Tenant 物理仅 PG，不删业务图／MinIO／公共历史。 |
| C5 Common＋读取 Owner | 写入／消费均校验回执 schema、Tenant、module、父子、action、attempt、digest；未知字段／类型／版本／越界拒绝，终态不可追加，无秘密／专业正文且保留观测 namespace | 公共投影已接；Monitor 白名单不自动扩大，父读权不授子详情；组合缺口见下表。 |
| C6 System＋Graph | 父截止缺项冻结 timeout；合法迟到只保留子事实、父不复活，失权结果拒绝；GET 不写状态，另一 execute 成功不证明引擎已删除 | 恢复／迟到／只读已有；真实跨服务迟到／失权 T4 待验。 |
| V1 Graph＋System | 固定 SHA、两个引擎分别核跨 Backend 取消、崩溃／暂停、外部在途、核清及新请求、Cleanup 阻断；保留首败与清理 | 两个受控 suite 已登记，完整故障 T4 未验；受控 runner 不含原生丢回包及正向恢复，不重复有效浏览审核或新增定时。 |

定位：[Common父子原语](../../../common/execution/cleanup.go)、[锁交错PG](../../../common/execution/cleanup_postgres_test.go)、[生命周期PG](../../../common/execution/lifecycle_postgres_test.go)、[Graph执行PG](../../../graph/backend/internal/repository/execution_contract_postgres_test.go)、[Graph Cleanup四模式](../../../graph/backend/internal/service/cleanup_runtime_test.go)、[Graph service PG](../../../graph/backend/internal/service/cleanup_runtime_postgres_test.go)、[System Cleanup PG](../../../system/backend/internal/service/cleanup_durable_postgres_test.go)、[Monitor快照PG](../../../monitor/backend/internal/service/cleanup_observation_postgres_test.go)、[共享前端反例](../../../common-frontend/basic/tests/cleanupObservation.test.mjs)。

### C2父关闭／子领取与未领取拒绝子矩阵

完整规则沿[父子协议](../addp-cleanup体系规范.md#父关闭与子领取协议)。以下编号继续用于专项对账，均沿上述 PG 入口，不新增运行路线。

| 编号 | 必须断言／剩余缺口 |
| --- | --- |
| C2a | 父关闭先提交则不 claim；拒绝子为 failed／attempt0，无 started_at／耗时／lease／外部调用。已有 PG。 |
| C2b | claim 先提交仅一次；父关闭不取消或改写合法子，不证明实际停止。阻塞链／父 timeout／迟到回执已有 PG，原生效果另验。 |
| C2c | 父 pending／临时失租而未到 deadline：子不领取、不拒绝；恢复只换父 attempt，不刷新 deadline／子集合。时钟及旧父失权已有 PG。 |
| C2d | 等 P／C／Owner 锁后跨截止或失租均拒绝且无部分提交；跳过忙父不算关闭。六种公共 PG 已有，Graph 领域锁待补。 |
| C2e | 候选变化、两父或同 Owner 多实例仍锁原 P/C，不另选目标、不形成 C→P。两父四领取者已有 PG。 |
| C2f | 错 Tenant／module／type／source／摘要、缺父或非法 deadline 均零执行／零拒绝更新，不补绑定或借模块名授权。Common／System 已覆盖。 |
| C2g | 恢复 pending 的 attempt>0、残留 lease／started_at／进度／回执或操作证据不足，均禁用未启动拒绝路线；失联仍围栏失败／核清，不重放。已有 PG，外部核清另验。 |
| C2h | 并发拒绝仅一次 pending→failed；completed_at／安全原因原子，失败回滚、重复时间不变；无 cleanup_receipt／假零统计／attempt1 事件，保留观测。已有 PG。 |
| C2i | 离线恢复只拒本 Owner 未领取项；合法迟到不改父结论／观察快照；GET 零写、父读权不授子详情。PG 已接，跨 Owner 恢复 T4 待验。 |
| C2j | Graph 使用 P→C→G→T/B；未领取拒绝不动 G／业务，已领取收尾 C→G 不回 P，最终清理不反锁 E。父独立截止与子回滚已有 PG；System 私有审计及其他外键／跨类锁另验。 |

Common 生命周期及 System Cleanup PG 还须核 prepare 失败／外层 claim 回滚和准确 backend PID 屏障，不能以 50ms 推测竞争；System 主项要求 PASS，拒绝缺项、skip、错名或仅子项 PASS。[固定版本结果](../../next/ADDP任务执行管理统一收口专题.md#本轮-pg-增补验证)单独认定。

### C5协调关闭安全展示子矩阵

唯一判据沿[安全展示契约](../addp-cleanup体系规范.md#协调关闭与模块未开始的安全展示)。Common 纯投影、System／Monitor PG 与共享组件已有覆盖；以下未齐组合不得由通用请求隔离测试替代，受控 T3 不代替真实身份／Gateway T4。

| 编号 | 必须断言／剩余缺口 |
| --- | --- |
| C5a | P/C 同次可读、父已提交终态且子 pending0 才显示 closed_unclaimed；status 仍 pending，无写入、耗时／回执／假零。投影与一致快照已有。 |
| C5b | Owner 拒绝已提交：双可读为 closed_rejected；仅 C 可读为 rejected_unstarted，无 basis／父状态；C 仍 failed／attempt0。已有 PG。 |
| C5c | 仅父可读不暴露隐藏节点、占位、计数或标签；冻结参与范围不是子结果。读取过滤已有，隐藏计数／页面组合 T3 待补。 |
| C5d | Owner 失败／超时／非法范围整份 503 并清旧详情，不返回部分或缓存；合法零 grant 正常过滤。PG 已区分，503 页面及真实故障 T4 待补。 |
| C5e | deadline 已过但 P 锁内 claim 未提交，不显示未开始；须父关闭／子拒绝提交证明；GET 无更新锁／写入。跨提交快照已有，claim 未提交交错待补。 |
| C5f | 跨 Tenant、错 P/C、旧 Redis 来源、缺证、曾运行、父临时失租均无标签；非法 schema／字段／类型／组合拒绝，无 metadata／错误正文／路径／凭据。反例已有，完整组合未齐。 |
| C5g | 撤权、401／403／404／Owner503、身份／execution 切换和迟到均清旧投影、停刷新、不写父结论。部分消费者 T3 已有；跨凭据 T4 及下列 C5l 组合仍缺。 |
| C5h | 双语共用说明码／唯一组件，不改状态统计，不造100%／0秒／领取倒计时／重试删除按钮；父旧 timeout 与当前子事实分开。部分受控覆盖已有，双消费者全状态／按钮／计数未齐。 |
| C5i | System 合法父凭据不属 Owner 协议时只省略可选概览，不称成功空集合；协议内 Owner 故障不能省略，Monitor 第一方限制不扩大。凭据回归已有，跨凭据页面待验。 |
| C5j | Owner HTTP 完成后才建公共只读一致快照，不持 PG 事务／更新锁等 HTTP；新发现未裁决 Owner 整份失败，无部分结果／写入。顺序／快照已有 PG，新增 Owner 反例未定位。 |
| C5k | 可读空集合、凭据不适用省略、缺证省略、非法对象四分；不说“无回收影响”或输出隐藏计数，不拼多请求证据。解析已有，“无影响”页面 T3 待补。 |
| C5l | closed pending→拒绝整体替换；父404清空不补查子，独立子请求只给子说明；running／attempt>0 移旧标签并报完整性诊断，迟到不复活、父结论不变。上述组合 T3 仍待补。 |
| C5m | Graph 未领取拒绝不动 G／构建／审核／操作证据；标签不证明远端停止、核清、空图或 reopen，不触发写入／额外权限／私表读。四模式含 PG 已有，真实服务／页面 T4 待验。 |

实际结果及余项见[专题第六节](../../next/ADDP任务执行管理统一收口专题.md#六剩余工作与共享环境)；[最近执行与回跳](../../next/ADDP任务执行管理统一收口专题.md#91-目标与既有覆盖)另行认定。Common 管原语、Graph 管私有监督及操作证据、System 管父子汇总、Ontology 独占修订发布、Monitor 管安全读取；本文不授予新接口、权限或运行授权。

Neo4j 原生身份候选沿固定摘要 Community 5.26.24 独占 T2 验证，保留原九阶段与 Common／Graph 断言：同一随机端口／库名／连接身份下检查初始存储、同卷重启、归档后新增数据、销毁旧卷后同名新存储、旧归档装载与重启、含 system 的整卷克隆／重启；只读核 db.info、dbms.info 和实际 databaseID。须证明确实恢复，不能只比名称或空图；缺字段记缺证，认证失败仍是失败，不证明细粒度角色授权。若不同存储／回退仍有相同 id／creationDate，保留反例并阻断正向绑定／reopen，不新增生产控制节点、账号或 Provider 恢复能力。

重启、归档装载、整卷复制及成功销毁前须证明同一容器退出0且无 OOM；137／不可读不算安全停止，先存脱敏诊断再清理。该夹具 Bolt 静默期每组1秒，Docker停止10秒、就绪240秒及 CI 20分钟不变；阶段不 skip，成败均核容器／卷／网络零残留。标准入口发现、API／Swagger同步及隔离要求沿通用规范，静态文档核对不计新 T1／T2／T4。

## FTP／显式 FTPS 准入门禁

| 入口 | 必须验证 |
| --- | --- |
| `make test-common-ftp-unit` | 严格连接事实／账号根／规范路径／TLS／取消、种子／忽略目录／临时归属及T2登记。 |
| `make test-common-ftp` | Business固定SFTPGo、独占容器卷／随机密码／私有CA／内外一致回环被动端口；FTP／显式FTPS、只读账号、NLST+MLST完整目录、中文／空格／百分号原名、REST Range、PASV约束及读取会话隔离，退出零残留。Linux ARM64／amd64同入口。 |
| [普通User消费T4](scenarios/文件来源与模型消费验收.md#ftp-普通-user-消费ftp-consumer-flow) | 平台身份及业务闭环另验；`make test-ftp-online-runner` 不代替真实Hosted。 |

固定 `48a28fbd4` 的准入 `39373761312` 两架构通过；同卷重建欢迎响应EOF曾实际复现并修复，镜像manifest／协议准入不代验生产Provider或T4。连接失败只公开固定阶段／协议码／脱敏类别，欢迎EOF不能当正常内容EOF或借机重试；Parquet保留实际Range请求计数及完整行数断言。

初建／同卷重建共用有截止时间的FTP `220` 就绪判据，Running不足；只在欢迎前等待拒绝／重置／超时／EOF，协议拒绝、容器退出及后续TLS／认证／目录错误立即失败。公开 `readiness-initial.json`／`readiness-recreated.json` 仅阶段／类别／固定日志计数。

清理前按本轮owner标签定位容器，最后200行原日志只存私有 `server-final-<index>.log`；Artifact `server-final.json` 仅sender／level计数或不可用标记，无路径／账号／凭据／原文／容器ID。取证失败仍清理，后轮通过／清理不改写首败；原始stderr不上传，未执行层级不计通过。

## Business Redis 门禁

`make test-business-config` 覆盖脚本语法、固定官方镜像、回环绑定及实际端口首次避让与重启复用。`make test-business-redis` 属于 Business owner 的独占 Compose T2：随机密码、随机回环端口、私有数据卷，实际验证 ACL、string/hash/list/set/zset/stream、TTL、二进制 key/value、大整数文本、幂等初始化、类型冲突拒绝和 AOF 重启持久化，退出核对容器、卷、网络零残留。新 owner 从 `ADDP_T2_OWNED_SERVICES` 声明自动发现，不要求为了测试创建新的服务模块或维护第二份 owner 清单。该门禁已登记 `make test-integration` 和 `release-and-t2-gates.yml`；它不证明 Redis Engine Plugin、Meta/Manager/Develop 或 T4 业务链路已经实现。

## Elasticsearch 门禁

- T1：`make test-common-elasticsearch-unit` 验受限 DSL、精确读集、HTTPS CA／认证、PIT 部分失败／取消清理及通用预览。
- T2：`make test-common-elasticsearch` 用固定镜像、独占项目／随机回环端口、只读用户及幂等样例验证 Common／Manager／Meta，退出核容器／卷零残留。
- T4：`make test-online ONLINE_SUITE=elasticsearch-consumer-flow`；部署与完整业务链见[Business Elasticsearch](../../../business/README.md#elasticsearch)。保留非默认 Tenant、独立登记身份（验前移除凭据）及无基础设施管理权的普通 User；目录／Meta／Manager／Develop 权限各按实际请求核验。Meta 重扫身份稳定、25条文档／空索引、DSL、long／nested／来源投影／排序及同身份 Console 均须真实执行；浏览器、身份、截图或清理缺证即失败。

T2 已沿根集成／Release 登记；T4 确定性编排由 `make test-online-runner` 覆盖，仅人工 Hosted Ubuntu x86_64，不保留 macOS profile／schedule。未实际通过不计 T4。

## Model3D Runtime 确定性门禁

`make test-model3d-workflow` 以受控 converter runner 验 DAE／3DS 源引用及动画边界、GLB 自包含、真实 PNG／JPEG 解码和失败不发布／替换；另覆盖下载完整性、私有资源隔离、损坏缓存拒绝。可用 `MODEL3D_WORKFLOW_PYTHON` 指向已准备解释器；生命周期沿 `make test-dev-lifecycle`，Linux／macOS 共用 IfcConvert 清单与 Cargo.lock。Python Runtime 的根聚合、依赖准备、共享输入及 CI 登记由现有检查核对，不将受控 runner 或原生预检计为真实转换链路。

Manager 模型 T4 沿[既有场景](scenarios/Manager文件与预览.md#manager-internal-artifact-lineage)，必须验证正式镜像身份／默认入口并按当次标签清理，不调用开发 wrapper。DAE／3DS／IFC／单体 OSGB／SKP 分别认定；SKP 自有样例保留英寸三角形、两个实例及内嵌彩色 PNG，核米制尺寸／位移／贴图／缓存／血缘，Meta 仅登记格式。

产品构建须在**最终 Runtime 镜像**经正式 operator 转换 Business 压缩 OSGB 并校验 GLB；builder 原生测试或最终镜像 `--help` 不能替代。

## Redis 键值数据集门禁

| 层级／入口 | 必须验证 |
| --- | --- |
| T1 `make test-common-redis-unit` | 配置／认证／取消／TLS、规范键名、RESP 解码前预算、原生读取与 Manager 保护边界。 |
| T2 `make test-common-redis` | Common ACL／样本预算／精度／stream重复字段，System 加密脱敏，Meta 单 keyspace 身份、退役逐key目录、重扫稳定／失败留目录，Manager 原生预览。 |
| Manager T3 | `data_type=key_value` 的键浏览／详情、二进制／大整数／score文本／stream字段序；模拟页面不计 T4。 |
| T4 `make test-online ONLINE_SUITE=redis-consumer-flow` | System server→keyspace 事实、Meta 重扫稳定 DataItem、九种原生预览及 page=2 拒绝，同 User／Tenant 的真实 Console、截图与报告；任一步失败／缺证／残留均不通过。 |

T2 与 `make test-business-redis` 共用唯一 `redis-owned-fixture.sh` 及固定镜像、随机密码／回环端口，成败清理容器／卷／网络，不访问现有 Redis。`ADDP_T2_LIFECYCLE_SCRIPT` 登记必须验证实际调用、输入覆盖和 Compose 启停，不能只登记空 helper；Common／System／Meta／Manager／Business 变更沿现有 Release/T2 发现。

T4 的 tmpfs 夹具、独立登记身份和最小权限普通用户、Hosted 一次性生命周期见[Business Redis](../../../business/README.md#business-redis)。登记凭据验前移除，消费用户仅目录／Meta扫描目录／Manager读权，无设施管理权。由 `redis-hosted-t4` 人工调用，`make test-online-runner` 验编排；首次实际成功前不计通过、不设 schedule。

平台采集与业务消费分开：`platform-node-metrics` 的 `node_metrics_case=redis-engine-admission` 核 Infra／Business 纳管、主机关联、HTTP SD、实际采样、Console 与清理，见[Redis 平台采集场景](scenarios/平台观测与语义生命周期.md#redis-自管接入专业观测与页面)。

## Meta 扫描与结果归属门禁

读取契约见[Meta 专业执行读取与响应](../tasks/执行监控.md#meta-专业执行读取与响应)。`make test-module MODULE=meta` 自动发现 API T1／前端 T3，`make test-meta-postgres` 为真实持久化入口；下表列专项范围，不重复 CI 登记过程。

| 专项／PG 用例 | 必须断言及边界 |
| --- | --- |
| 专业读取 `TestMetaScanExecutionReadAgainstPostgres` | 直接 HTTP 核 Owner Permission、OAuth User／本人一次性执行、缺发起人、同 Tenant 他人、跨 Tenant／模块／类型、已删定义、分页总数及404；TaskProvider 另核专用 Client／机器 Permission／他人执行。历史 T4 不声明完整 OAuth／机器矩阵。 |
| Mosaic `TestMosaicScanWholeResultAgainstPostgres` | Manager 父在原 lease／超时内等精确扫描成功并获得 whole 身份，不能公开 manifest 路径；等待仍 running、唯一输出、扫描失败／超时／取消、读取拒绝、错目标／manifest、缺 item／fingerprint、截止收尾及旧 lease 拒绝。Manager 沿 `make test-manager-backend`／`make test-manager-postgres` 的 `TestIntegrationPostgresManagerMosaicWholeResultLifecycle`。 |
| Mosaic 来源与存储 | 专用 Client／Permission、跨租户、父子／attempt／目标及父 lease 绑定，闭合响应，S3／NFS 资源树身份，旧事实拒绝，来源回执与终态原子保存。PG 事务内清空 Common 三表并调用正式 `execution.EnsureStore` 迁移，退出回滚核原 OID；不借残留约束或自写替身，半套主体由正式 PG 约束拒绝。 |
| Develop 产物自动扫描 | 同一直接 HTTP／PG 夹具核服务权限、父执行／产物、同租户原发起人读取、他人列表不增、错误来源／类型／状态／主体／目标及自报授权拒绝；失败不落子记录，DTO 无配置／授权。Develop T1 核先持久产物后提交父 UUID，保存／父状态失败不发请求。 |
| 精确范围 `TestPreciseCatalogScanAgainstPostgres` | 冻结完整 locator，非法／跨引擎本地化400且不落记录；JSON／去重锁、表／集合／图／叶子／file-object 边界。局部成败均不改祖先事实或兄弟 item，完整范围仍可清缺失项；点号不截断，空目标不退回全引擎。 |
| 原生 PostgreSQL `TestPreciseNativePostgresScanAgainstPostgres` | 只读源角色扫描 A，禁止父级 ListChildren，B／父扫描事实不变；不可读B、缺目标及撤销 schema USAGE 均失败。Common 的 `TestIntegrationPostgresCatalogPreciseReadOnly` 核单次目标查询、轻量摘要／主键详情与 NotFound。 |

表格 Provider 统一用必需 `GetTable`，无父级 `findTableInfo` 枚举；PostgreSQL／MySQL／Oracle／ClickHouse／Doris／TiDB／OceanBase 使用目标名称谓词。T1 核四类原生 SQL 参数／谓词／源过滤、错误传播，解析不读字段、缺目标不进详情；其他厂商真实服务未由 PostgreSQL T2 代验。

共享 Client／新增 Go 用例沿现有 `make test-go` 与模块发现，夹具纳入 `make test-execution-fixtures`，接口变化同步两模块 Swagger／覆盖与 `make test-authorization`。真实认证／Gateway／Worker 使用 `orchestrator-execution` 或 `raster-workflow`，容量沿原 `raster-workflow-capacity`，不改预算、oracle、扫描、Monitor 或清理。Develop 历史 Raster 仅证明已完成扫描／专业读取，为部分 T4；System 当前资源授权、撤权后 Worker 重核、metadata 可见性及完整资源权限闭环不由本组推定通过。

历史范围见[Meta 专业扫描读取证据](evidence/资源读取与预览历史证据.md#meta-专业扫描读取--t4)与[验收索引](验收证据索引.md#addp-验收证据索引)。

### 用户血缘可见性

Meta T1／`make test-meta-postgres` 的 `TestUserLineageVisibilityAgainstPostgres` 核 `permission_filtered` 的实际方向、深度、字段和结构快照：不穿隐藏桥／转旁系，当前及历史撤权生效；他租户、已删／时态范围外关系不触发，机器事实不变。隐藏节点／边／证据不输出、不计邻居或 truncated。`make test-common-frontend` 核标记，`make test-manager-frontend` 核隐藏与截断区别、失败清空和局部展开；PG Online runner 只核编排，新 T4 另行确认。

### 血缘采集器回归

Develop／Meta Go 与 PG 核已提交产出后失败、批量已提交子集／未知／未执行／无关输入裁剪、真实失败终态、延迟扫描、重复通知及 replace 时态；原 `raster-workflow` 核部分失败产物扫描／血缘，不增加采集路线。

`make test-meta-postgres` 保留超过500条、同创建时间 keyset、迟到扫描、幂等、单条错误隔离、轮内新增及游标后迟到成功的下一轮处理、取消、通知不复活已关闭字段关系。Transfer PG 另核三个查询输入（含仅筛选来源）、改名／类型转换及完整物理行集合。实际结果见[血缘采集器证据](验收证据索引.md#血缘采集器历史证据)，不代验当前源码。

## 平台指标设施独占门禁

正式能力沿[运行观测规范](../addp平台运行观测规范.md)；本节仅补充门禁断言。标准入口与层级如下：

| 入口 | 责任与边界 |
| --- | --- |
| `make test-monitor-redis-source-unit`／`make test-monitor-redis-source` | Redis 配置 T1／真实协议 T2；唯一 `redis-owned-fixture.sh`／`common-redis-contract.sh --monitor-only`，固定 Redis 7.2.13、随机回环端口／密码及 Business 样例，不重复其他消费者。 |
| `make test-monitor-metrics-config` | 无设施的配置／部署意图 T1；空进程拓扑中 docker／launchctl／lsof／ps 全替身，ps仅声明快照，不读宿主。核发布先于停服、发布失败零停止、精确模块范围及8／10秒期限；真实PID／后代／工作区沿 `make test-dev-lifecycle`。 |
| `make test-monitor-metrics` | 默认串行全部 case；CI 为八个互补分组，独立 Linux Runner、`fail-fast: false`、各20分钟；`Monitor metrics infrastructure` 只汇总真实结果，失败／取消／跳过不计已选门禁通过。 |
| 正式身份 T4 | [Redis 平台采集场景](scenarios/平台观测与语义生命周期.md#redis-自管接入专业观测与页面)及其他节点场景；T2 不证明标准部署端到端安装、正式纳管／HTTP SD／Console 闭环。 |

选择分组如 `make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=directory-publication`，默认入口不传分组。Metrics T2 自建 Prometheus、来源及私有 TLS 转发，固定镜像、随机 Compose 项目／回环端口、私有时序卷与临时 CA；根集成、owner／输入发现与 Release 共用入口。所有 case 保留原采样窗口／新鲜度／期限及成败零残留，共享传输前置可各自重验，领域场景不重复。可选观测故障不影响核心 Infra 的断言沿开发生命周期门禁。

| 领域／case | 独有断言及未覆盖项 |
| --- | --- |
| Redis 专业来源 | 当次创建／删除两类采集账号，核认证、固定 INFO、业务读写／扫描／管理拒绝、停用／清权后的旧连接拒绝，保留原 reader／业务键；一字节逻辑 maxmemory＋一次小 SET 验OOM计数后还原。生命周期探针核 mTLS、缺证／错用途拒绝、Docker只读发现、账号重启持久化、来源恢复／清权；[历史范围](evidence/Monitor采集来源与引擎指标历史证据.md#redis-账号持久化与来源生命周期2026-11-01)与正式T4分开。 |
| `--case=directory-publication` | 无数据库共享目录复现；T1只选 `MetricsDeploymentConfigTest`，其他case仍完整配置测试。固定PG镜像改 Bash 入口，无PG／SQL／应用配置／账号／HBA／端口网络；随机临时普通文件、无额外能力的只读容器及CPU／内存／进程限制，屏蔽数据库VOLUME防匿名卷。首次读失败即失败，枚举后可读不抵销。复用唯一正反枚举探针；外部可复制 `scripts/test/directory-publication-probe.py` 独立复现，仓库仍走根入口，不能代验来源认证。 |
| `--case=postgres-source` | 独占 `postgres_data` 保留同容器重启账号／函数／认证，核健康不动、来源恢复及新 StartedAt；目录认证分支先删当次容器／卷再建隔离库，退出 `down --volumes` 核项目卷清零。准入探针私有目录构建最多180秒，失败不运行遗留文件；实际 `-test.timeout=30s`、外层45秒分别计时，不挤占原采样或20分钟Job。 |
| `--case=http-quality` | 真实SDK、HTTP SD／mTLS／Prometheus、查询及告警，专项断言沿[T0–T3 HTTP质量](T0-T3测试规范.md#http-质量来源与页面)。不以原Trace通过代验本组。 |
| `--case=process-alerts` | 三个当前进程按身份／端点匹配，65秒内取得CPU一分钟窗口至少四样本及分钟前边界；RSS首样本不替代。Go总270秒、每阶段100秒、可执行测试300秒，来源寿命八分钟；两样本不足15秒须等第三个，不舍入、不重试、不改阈值或生产规则。 |
| 进程压力与通知 | 仅自建非root来源，整体0.25 CPU／128 MiB；一个Worker先空闲基线，再有界CPU＋触达24 MiB匿名内存，最多180秒释放。规则后完整CPU窗口先就绪，释放先核真实RSS，再由自然样本恢复同一事件；两个对等进程不误触发。回环TLS核原文HMAC、首503同投递重试、opened/resolved与持久事件一致、旧日志订阅无新类别。成败清控制文件／进程／DB／外层资源；System身份夹具T2不计真实Console、生产压力或T4，不向个人环境／通知群加压。 |
| `--case=host-resources` 与实源 | HTTP SD、主机／受限来源、中心恢复、总览及资源告警；一个Backend＋两个Worker独立身份／启动／端点，依次停Backend与一个Worker，失效即时值清空、另一个Worker保持自身数据。SDK／Prometheus／Monitor T2不代验正式登记承载绑定、多Worker业务或原生macOS拓扑。 |
| `--case=cgroup-source` | 显式选择自身cgroup v2身份及CPU／内存原始量，三个SDK来源对照独立内核；共享组不证明独立容器或配额。 |
| 共享传输／受限层 | 正式转发模板核OAuth／HTTP SD固定路径／方法／参数、匿名拒绝、mTLS、周期／超时／样本越限、断开恢复、SIGKILL后WAL重放。受限层核七采集器、Engine CPU／内存／内核、异PID／挂载／网络命名空间磁盘前后快照、禁用文件系统／网络族、五项磁盘即时值／精确设备趋势、唯一SD与恢复新样本。macOS实测只证Desktop VM，Linux CI只证受限层；不证明完整Linux节点、规模、全部授权／业务埋点或7天物理留存。 |

目录认证的对照与失败诊断仍保留：同一只读挂载的预置普通文件须证原子替换可见，HBA宿主／容器内容不变，前置失败不跳过认证。两组新文件在同进程两隔离目录正反枚举，握手后宿主替换；每侧枚举前后各一秒无枚举观察，第二组交换顺序，核同PID／单调时间／阶段，不用诊断窗口等认证成功。HBA仅在物理发布成功后记录恢复新版本，再读回／重载；读回失败不靠旧版恢复。首败保留、清理错误附加，诊断只给内容匹配／元数据／只读挂载与错误类别，不泄露HBA／SQL／凭据／原始stderr，也不改变生产发布路线。

## Manager Raster Mosaic 原生 Runtime 门禁

`make test-raster-mosaic-runtime` 是 Manager-owned Python T1 入口，覆盖已有瓦片渲染单测及真实 GDAL/COG、NumPy、坐标系与瓦片输出。`make test-module MODULE=manager` 自动发现该具名 Runtime 入口；Platform CI 的 Raster Mosaic job 按 Manager 变更选择，使用独立 Python 3.12 环境和匹配原生版本的 GDAL 绑定。共享 GDAL 生命周期修复另由 `make test-dev-lifecycle` 覆盖来源选择、失效绑定重建、环境隔离及停止归属；涉及共享入口时同时运行 `make test-geopython-workflow`。

## Security 治理与识别门禁

受保护资源有效期与筛选、银行卡号元数据识别扩展：`make test-module MODULE=security` 继续自动发现 Go T1、前端确定性/浏览器/构建及既有 Security PostgreSQL T2。`TestEnrollmentLifecycleAgainstPostgres` 增加与 SQLite 共用的字面子串转义、引擎/状态交集、分页总数、租户隔离以及有效期起点/截止点/退出状态回归；不新增测试数据库或入口。银行卡号能力 T1 验证元数据正例、模糊名称及非字符串反例，并沿正式发现执行将结果编译为四 Owner 规则。前端 T3 验证已同步投影的独立到期警示、日期展示、重新发现入口保留及服务端筛选/重置；使用隔离模拟 API，不计为真实部署 T4。API 字段与筛选参数由 Security Swagger 生成及路由覆盖验证。


## 业务图双引擎领域验收

`make test-common-neo4j`／`make test-common-falkordb` 共用各自独占容器，运行相同 Graph 领域矩阵：显式目标、实体／关系、原生唯一约束、构建审核、浏览展开、无向最短路、全文字段隔离、外部索引冲突不修改、managed按本体重建、知识服务。FalkorDB同label物理索引可取定义并集，搜索仍限各定义字段，不能只比数量；Develop PreparedGraphQuery及保护另验，不借领域可信写入代替。

Graph Security T1 用正式Router／普通User核非默认Tenant、未同步／纳管拒绝、匿名知识服务不能绕过及跨租户不取连接；HTTP在途屏障到响应序列化结束，旧Context随后不可读。双引擎T2核浏览／搜索／展开／路径／约束／推导／分析／空间同步／知识服务与返回标识的写入拒绝。Security核独立 `graph_read` 且无原值豁免；PG `TestEnrollmentLifecycleAgainstPostgres` 核Graph Owner补齐、过期不续期、releasing／released不复活及幂等；Common投影存储包含graph。

000218迁移沿 `make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package migration --test graph-protection-runtime"`。目标路径JSONB保真／唯一性／退役字段／迁移回滚／构建并发沿Common PG，`COMMON_POSTGRES_TEST_ARGS="--test graph"` 仅证明分组；三个Common T2登记Graph输入。

真实保护验收统一见[graph-protection](scenarios/数据保护.md#graph-protection)：既有三项Security profile要求Graph Ready与第五个期限／同步回执，原四字段出口单独认定，不称Graph属性脱敏；沿 `make test-security-online-runner` 及既有Hosted，`security-transfer-protection` 的 `-all` 生命周期保持，不新建suite。T1／T2、管理员浏览不代验普通User／Console／Gateway／Copilot；System源授权与个人服务重启不在本组。

## Manager 预览浏览器门禁

唯一入口 `make test-manager-frontend` 调度共享 `scripts/test/frontend-browser-gate.mjs`、模块 `test:e2e` 与 `addpBrowserTest`。共享夹具独占回环服务／端口，两个worker按文件并行、文件内串行；BrowserContext／模拟API／下载目录分别隔离，样例只读，下载仅 `testInfo.outputPath()`，不减用例／放宽期限或过滤页面错误。

共享组件T1归common-frontend，页面布局／路由／预览T3归Manager，iframe／认证归Console。断言用实际可见性、bounding box、顺序和状态；异步等响应、不以sleep／重试掩盖失败，截图／trace／video入临时目录或Artifact。普通表格、空间容器、分页、弹窗、视角／locator及响应式按现有Owner用例覆盖；隔离模拟API不证明真实权限／数据源T4。

静态轨迹须核三格式、乱序／时区、Unix秒精确毫秒与负数、无效时间／坐标、完整性、同设备同刻位置冲突、403／预算后清旧路线、迟到丢弃、范围回显及GeoJSON默认CRS提示。绘图用630px、1.6像素密度、南半球两路线核像素／全视野，不据此设产品最小视口。

后端仍由Common唯一读取器核严格query／目标字段、混合设备ID／bigint文本、左闭右开、跨首段连续CSV／JSON／GeoJSON、预算／点数／保护失效／源增长／流失败／阻塞取消、JSON闭合与稳定EOF；真实下载／授权／来源见[轨迹Hosted场景](scenarios/Manager文件与预览.md#设备静态轨迹范围读取)。

## Manager 快显回归与基准验证

### 预览页快显回归

产品动作、材料和生命周期以[快显实现规范](../../../manager/docs/快显实现规范.md)为准。Manager／common-frontend门禁至少覆盖：

| 样例 | 独有断言 |
| --- | --- |
| IFC | 不可基础渲染时按 capability 提供 `generate_model_3d_glb`；ready 后可往返切换。 |
| STL／OBJ／FBX | 保留基础预览，GLB快显切回仍读源文件，不误用GLB内容地址。 |
| OSGB whole | `model3d_tiles.formats[]` 分别核3D Tiles／S3M能力、原因与状态；每次生成一种，独立切换，失败不串，ready读取不依赖转换引擎在线。 |
| TIFF | 小图基础／动态、大图或受管COG的能力与 `generate_raster_cog` 一致；ready后Range读取且保留视角／样式。 |
| 3DGS PLY／SPLAT | 基础与 `generate_gaussian_splat_ksplat` 分开；源KSplat不二次生成。 |

所有生成仅向 `/quick-view/actions` 提交 `locator + action`，按钮取 `available_actions`，业务前端不拼任务配置或维护白名单；metadata只用 `preview_artifact_*`，无旧 `quick_view_*` 字段。模式切换只改所属 `preview_state`，失败／超时／取消如实展示，不伪装ready。模拟页面不代验真实身份、Runtime、原件和清理。

### 动态 MVT 基准验证

`cmd/quickview-mvt-benchmark`／`internal/mvtbenchmark` 只按显式配置读PostGIS，不接Manager API、不写业务表或创建物化结果，不是第二套门禁。schema／table／geometry column／SRID／tile必填，不默认 `geom`；实际端口按[端口规范](../addp端口分配.md)，不接管个人服务。

| 必需样本／结果 | 判定边界 |
| --- | --- |
| 小中大规模×点线面 | 源4326、非3857、源3857有GiST、Manager ready／外部可验证3857目标；覆盖 `ST_Transform` 与 `geom_3857` 可索引路径。 |
| 推荐起始／低层全局／中层浏览／高层局部 | 不只测空瓦片或单zoom；核SQL与总耗时p50／p95／p99、错误／超时／空瓦片率、MVT体积、扫描行／DB CPU／连接及排队。 |
| 两类配置建议 | 无索引源转换与可索引目标分别给预算／提示／缓存策略，均保留单瓦片超时／体积／并发保护；阈值由实测决定再回填配置，缓存生成不因动态响应预算被禁止。 |
| JSON报告 | `explain_analyze` 可附 `EXPLAIN (ANALYZE, BUFFERS)`；`recommendations.render_paths` 含 `quick_view_realtime_tile_timeout_ms`、`quick_view_realtime_tile_retry_after_sec`、`retry_policy`、`recommended_action`、`confidence` 和依据。超时标 `needs_higher_timeout_rerun`，调部署默认前显式提高 `timeout_ms` 复测。 |

capability诊断须区分 `source_transform_path`、`ready_3857_target`、`source_3857_indexed`、`tile_cost_exceeds_realtime_budget`、`optimization_recommended`、`vector_tile_cache_generation_recommended`。`recommendations.multi_scale_candidates` 仅记录可索引目标低层仍超时／体积过大的后续候选，不新增用户入口，当前闭环仍是瓦片缓存。

[历史基准](验收证据索引.md#manager-动态-mvt-历史基准)保留低层超时、无固定SHA及性能边界，不预设生产阈值、不单独构成T0–T5通过证据。

## SuperMap Runtime 确定性门禁

SuperMap Workflow 的协议、算子目录及资源主机测试使用根 `make test-supermap-workflow`，通过临时构建目录运行不加载厂商 SDK 的 CTest，依赖 CMake、C++20 编译器和 nlohmann-json 开发包。CMake owner 必须同时登记根 `make test` 依赖及 Platform CI 调用，由构建登记检查和模块门禁核对。真实 SGM 读取、OSGB 导出和发布边界仍由受控 SDK 镜像构建阶段的 `sgm-conversion-test` 验证；公开协议门禁不代表 SDK 转换或 T4 已通过。

## GeoPython Runtime 确定性门禁

GeoPython 本地 T1 使用独立 Python 3.12 venv 与匹配的原生 GDAL，`make test-geopython-workflow` 复用开发入口的 GDAL/PROJ 和 Runtime 专属 ODBC 配置；CI 安装 GDAL、MDBTools ODBC、unixODBC 和 pkg-config，再安装匹配绑定。生命周期 T1 覆盖健康 GDAL 绑定复用、缺失绑定、同版本动态库失效及版本不匹配后的无缓存源码重建、重建失败和重建后仍不可用时拒绝启动，以及依赖及导入失败、端口归属、原生 PID 清理和先预检后停止；Runtime T1 使用真实 Gunicorn，验证健康就绪后注册以及跨请求状态。`make test-arcgis-open-formats ARCGIS_OPEN_FORMATS_ARGS=--runtime-only` 只覆盖真实 MDB/PGeo 读取，不能计为 Transfer/Oracle Spatial 往返通过。

## Document 原生转换门禁

Document 原生转换使用独立入口 `make test-document-native`，在已准备的私有 LibreOffice 和固定中文字体上，经正式 `document_to_pdf` 算子转换三页 PPTX，核对页数、中文文本、工作目录清理及源样例不变。缺失或损坏的依赖直接失败，不跳过、不隐式安装。测试使用临时输入、输出和 profile，不启动 ADDP 服务；由 Platform CI 的 Document Workflow Job 准备依赖后调用。普通 start/restart 只负责依赖与服务就绪，不执行该功能测试。HTTP／算子确定性测试继续使用 `make test-document-workflow`，跨模块产物生成与浏览器预览继续由 Manager T4 验证，三者分别报告。

## Spark 联邦查询技术准入

| 入口／层级 | 独有断言 |
| --- | --- |
| `make test-system-frontend SYSTEM_FRONTEND_BROWSER_ARGS="engine-type-groups.e2e.js"` | T1、定向T3及构建；无参数仍全量，不称完整模块T3。Spark Master端口／独立Web UI必填，扫描只按Catalog能力，不按类型排除。 |
| Common／Develop／System／Service／Engines `make test-module MODULE=<owner> GO_ONLY=1` | `ResolveExecutionBinding` 拒缺／重／离线／协议受众连接器不匹配，按角色不按Spark名路由；委托不伪装HTTP端点，受控请求分离目标／Runtime／源且无凭据，DuckDB拒外部计算。 |
| `make test-spark-workflow` | `query_process.py` 真实子进程核stdin取源、不继承机器密钥、精确结果／有界读／超时取消及忽略SIGTERM的后代清理。原生准入另启真实JVM核远端Worker、bigint与私有目录；进程T1不证明JVM取消／HTTP认证授权，未接通不提前开放能力。 |
| `make test-common-spark-federation` | 固定摘要独占PG15／MySQL8.0／Spark3.5.0 Master/Worker，无宿主端口。临时目录按实际ARM64／amd64编译生产Plugin探针并在Master运行，核原生 `/json/`、ALIVE Worker、RPC，无Thrift。原生JDBC跨源JOIN／位置参数／真实Worker、bigint／Decimal／null／中文／尾空格，以及第二Driver不继承catalog／临时视图。 |

标准入口／Go或Release/T2沿原登记，启动／Driver失败清理由Spark门禁覆盖，成败核容器卷网络零残留，不访问Business／共享Infra。技术准入不证明正式授权、HTTP查询、快照绑定、多集群路由、JVM取消或页面T4。

## Spark 数据发布门禁

- PostgreSQL：`make test-common-hdfs` 复用独占PostGIS／真实Spark Worker。核已有／新目标提交成功但回包丢失、提交前失败回滚清理、回滚未确认保留证据、已提交后的close／暂存清理失败不改事实。仅包裹正式JDBC控制连接注入，原生PG独立连接核物理提交／暂存；T1核回滚／关闭不遮盖首错。
- MySQL／Doris：`make test-common-spark-table` 为Linux x86_64 Hosted T2，固定摘要独占MySQL8.0／Doris2.1.0／Spark Master/Worker，无宿主端口。Doris经Business唯一入口核新建／重复登记Backend ID不变、FE/BE就绪及官方连接器；旧IP迁移反例沿 `make test-business-config`，本机已有Backend真实迁移未验，macOS显式MySQL子集不算完整T2。

后者保留覆盖／追加／空成果、目标定义不变、字段／约束拒绝、计算失败、bigint／Decimal精确回读、Doris SQL生成Decimal的JSON精度及连接器中文／emoji／空串／null／尾空格／长文本；MySQL回包丢失保留原提交及暂存证据。两入口沿Common Runtime／夹具输入自动选Release/T2，成败中断清容器卷网络，不接个人实例；Provider T1／Go不代验Spark写入，T2不代验Develop／Console端到端。

## 关系型写入与安全诊断门禁

各Provider沿现有Common／Release T2登记，使用独占固定镜像与合成表，不接业务实例、不运行已存Transfer任务或触发扫描。T1只证明会话／绑定及投影规则，原生数据副作用须独立连接核对。

| Provider／入口 | 必须保留的物理断言 |
| --- | --- |
| Doris／ClickHouse：`make test-common-doris-decimal`／`make test-common-clickhouse-decimal` | 非法JSON、后chunk失败、写入／关闭取消、Abort、正常／空会话七场景；首错、失败后拒续写（含空批）、重复close／提交标记。先1行成功，再1000行已提交后chunk失败，实际1001行及JSON／bytes／文本原值保留，会话失败无成功标记。合成表先确认未占用，成败逐表及容器卷清零；ClickHouse必须等初始化后的正式服务。 |
| ClickHouse bytes：同一入口 | 原生声明／加列／目录／原字节／表页／cursor，Manager HTTP核Base64逐字节、文本／空字节／NULL、真实保护与非法文本；System／Meta／身份仍是受控夹具。 |
| Oracle／达梦：`make test-common-oracle-decimal`／`make test-common-dameng` | Oracle沿Hosted Slim／Spatial，达梦仅本机官方驱动。核NULL不占绑定参数的实际列、达梦MERGE字段序／冻结类型／驱动链，不猜列；编码／SQL失败分别核回滚、close与提交。达梦范围拒绝／活跃容器保护沿 `dameng-official-media-release-gate_test.py` 和 `make test-business-config`，不代验原生T2。 |
| PostgreSQL COPY：Common PG `--test typed-write` | append／事务内replace的COPY收尾非空拒绝、延迟唯一约束提交失败、编码失败／取消close；重复close仍保留错误链／失败，无成功标记。核物理回读／表schema清理，成功／空会话原提交不变；默认PG及已有Hosted发现同组。 |
| MySQL／TiDB／OceanBase | 复用JSON原生夹具，MySQL `--test typed-write` 及各引擎门禁核安全列定位／驱动链／回滚／提交；MySQL另保留几何与bytes。 |
| openGauss：`make test-common-opengauss` | Common T1／Hosted核MERGE的JSON／数组错误，列序来自实际批次、类型来自冻结声明；原生执行错误不能猜列。 |

统一诊断v3由Common T1核错误链／实际列及严格投影：未知键、错类型／序号、伪造类型、旧版本、非法风险均拒绝；原生JSON／会话T2同时核类型化错误，不删物理行数／bytes／空间断言。Transfer T1按真实持久化边界核纯定义／源打开拒绝无风险，以及删除／准备／替换／写入／写后源读取、进度或close失败的风险；前端T1／Transfer T3核双语模板及隐藏正文。既有Hosted四失败场景须另验稳定码、非法JSON第2写入列、目标可能已变的API／页面，runner不充当新版T4证据。

UTF-8门禁在SQL前拒绝string／[]byte／json.RawMessage中的过长／截断／孤立续字节／代理区编码，保留 `json_invalid` 与实际列；合法中文／emoji／替换字符／JSON转义不重编码，普通bytes非UTF-8保真。各Provider原生矩阵核三种预编码输入及失败后行数；按批发送，不能用逐行网络写代替跨chunk语义。

MySQL在测试建库后登记精确名称，成败逐项清理，失败仅留确切名称恢复清单、不计通过。`--inspect-owned-databases` 只读；`--cleanup-owned-databases <确切名称> ...` 仅接 `addp_common_mysql_it_`＋19位时间戳，禁业务库／通配符／SQL片段及按前缀自动删除。

[历史证据](验收证据索引.md#addp-验收证据索引)与当前运行分别认定；本组不新增权限／API／依赖或平行CI。

## Oracle 原生类型与空间门禁

Oracle 的既有 `make test-common-oracle-decimal` 默认使用 Slim 一次性容器验证非空间类型写入及删除；`make test-common-oracle-decimal ORACLE_SPATIAL=1` 使用固定摘要的完整镜像，额外验证原生 SDO_GEOMETRY、空间索引、DROP 后的登记状态、存在／缺失表的精确登记清理、重复删除及其他表登记保留。两种配置均由既有 Hosted Oracle Job 执行，容器只绑定随机回环端口，退出销毁容器及卷；不得连接业务 Oracle 或以模拟视图代替原生空间认证。

## 租户执行过程事件门禁

| 标准入口／用例 | 必须验证 |
| --- | --- |
| `make test-common-postgres`／`TestExecutionEventsAgainstPostgres` | 事务／并发限额／日预算、lease过期、Owner分页、清理锁竞争、continuous attempt迁移／裁决预算；未开始取消不造事件。 |
| `make test-transfer-postgres`／`TestIntegrationPostgresExecutionEventsRemoveRetiredText` 及已登记分组 | 删除历史自由文本；continuous事件、fencing／取消／自动恢复／退避熔断／结构阻塞；真实锁后过期进度／终态／续租拒绝，错Tenant／execution／module拒绝，事件状态回滚及清事件留概览。 |
| `make test-common-frontend`／`make test-monitor-frontend` | 唯一共享组件、事件分页／截断／撤权清空的确定性页面。 |

Common默认全量含事件组；这些结果不证明真实User／Gateway／Owner／Worker T4或个人环境已运行新迁移／留存循环。

## 保护投影与执行诊断门禁

Common `TestPreparedOutputRejectsExpiredProjection`／`TestProjectionStoreResultExpiryAgainstPostgres` 共用四Owner的native／query矩阵：经正式变化流安装365天历史投影，核截止前／时／后，空与非空输出到期均拒绝、纳管标记／cursor保留；同ID更高revision后旧保护器仍拒绝，新存储刷新cursor及重开后应用新修订，空值／null正确，不自动续期或取消纳管。输出前重核全部绑定投影，不只检查读取开始。

Manager核无数据／结构泄露，Develop／Service核执行前拒绝及无在途残留，Transfer PG导出核native／query拒绝、零新增写入及旧目标不变。沿现有Go／Common／Transfer PG登记，不改有效期／时钟／业务投影，不计真实等待365天的T4。

Transfer bounded T1核类型化／包装／无关错误、lease下完成与专业响应藏正文，PG读取隔离核持久诊断／权限；正式契约及Hosted稳定码／执行身份／零写入见[bounded失败诊断](../../../transfer/CLAUDE.md#bounded-失败诊断)。

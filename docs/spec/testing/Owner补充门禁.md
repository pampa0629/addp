# ADDP Owner 补充门禁

[返回测试与验收总入口](../addp测试与验收规范.md)

本页补充各 owner 的专项断言；测试分层与通用边界以[T0–T3 测试规范](T0-T3测试规范.md)和[T4 Online 验收规范](T4-Online验收规范.md)为准。


| 门禁类别 | 文内入口 |
| --- | --- |
| Runtime 与原生转换 | [Model3D Runtime 确定性门禁](#model3d-runtime-确定性门禁)、[Manager Raster Mosaic 原生 Runtime 门禁](#manager-raster-mosaic-原生-runtime-门禁)、[SuperMap Runtime 确定性门禁](#supermap-runtime-确定性门禁)、[GeoPython Runtime 确定性门禁](#geopython-runtime-确定性门禁)、[Document 原生转换门禁](#document-原生转换门禁) |
| 基础设施与数据引擎 | [Business Redis 门禁](#business-redis-门禁)、[Elasticsearch 门禁](#elasticsearch-门禁)、[Redis 键值数据集门禁](#redis-键值数据集门禁)、[平台指标设施独占门禁](#平台指标设施独占门禁)、[Spark 数据发布门禁](#spark-数据发布门禁)、[关系型写入与安全诊断门禁](#关系型写入与安全诊断门禁)、[Oracle 原生类型与空间门禁](#oracle-原生类型与空间门禁) |
| Owner 数据、安全与界面 | [Meta 扫描与结果归属门禁](#meta-扫描与结果归属门禁)、[Security 治理与识别门禁](#security-治理与识别门禁)、[业务图双引擎领域验收](#业务图双引擎领域验收)、[Manager 预览浏览器门禁](#manager-预览浏览器门禁)、[Manager 快显回归与基准验证](#manager-快显回归与基准验证)、[租户执行过程事件门禁](#租户执行过程事件门禁)、[保护投影与执行诊断门禁](#保护投影与执行诊断门禁) |

## FTP／显式 FTPS 准入门禁

`make test-common-ftp-unit` 验证严格连接事实、固定账号根、规范路径、TLS 选择、取消、种子生成与数据目录忽略、临时资源归属，同时核对 T2 的 Make／CI 登记。`make test-common-ftp` 使用 Business 唯一固定 SFTPGo 镜像、独占容器与卷、随机密码、私有 CA 和内外一致的回环被动端口，实际验证 FTP／显式 FTPS、只读账号、NLST+MLST 完整目录、原始中文／空格／百分号名称、REST 范围、PASV 地址约束和读取会话隔离，退出必须零残留。Linux ARM64／amd64 执行同一标准入口；镜像 manifest、协议准入和生产 Provider／普通用户 Hosted T4 分别认定。准入通过前不注册生产插件，未实际运行的层级不得记为通过。 初次创建和同卷重建必须共用有截止时间的 FTP `220` 欢迎响应就绪判据；Docker `Running` 只表示容器进程已启动，不能直接开始 TLS／账号／目录断言。仅在欢迎响应前等待连接拒绝、重置、超时或 EOF；服务端协议拒绝、容器退出及后续 TLS／认证／目录错误立即失败。门禁公开 `readiness-initial.json`／`readiness-recreated.json` 的阶段、错误类别及固定日志计数；原始日志与外部命令 stderr 只留在私有临时目录，不上传 Artifact，失败后仍清理当次容器与卷。

## Business Redis 门禁

`make test-business-config` 覆盖脚本语法、固定官方镜像、回环绑定及实际端口首次避让与重启复用。`make test-business-redis` 属于 Business owner 的独占 Compose T2：随机密码、随机回环端口、私有数据卷，实际验证 ACL、string/hash/list/set/zset/stream、TTL、二进制 key/value、大整数文本、幂等初始化、类型冲突拒绝和 AOF 重启持久化，退出核对容器、卷、网络零残留。新 owner 从 `ADDP_T2_OWNED_SERVICES` 声明自动发现，不要求为了测试创建新的服务模块或维护第二份 owner 清单。该门禁已登记 `make test-integration` 和 `release-and-t2-gates.yml`；它不证明 Redis Engine Plugin、Meta/Manager/Develop 或 T4 业务链路已经实现。

## Elasticsearch 门禁

- T1：`make test-common-elasticsearch-unit` 验证受限 DSL、精确读集、HTTPS CA/认证、PIT 部分失败/取消清理及通用记录预览。
- T2：`make test-common-elasticsearch` 使用固定官方镜像、独占 Compose project 与随机回环端口，创建只读用户和幂等样例，实际验证 Common、Manager、Meta，完成后检查容器和卷零残留。该脚本已注册根 Makefile、模块/变更自动发现及 `release-and-t2-gates.yml`。
- T4：`make test-online ONLINE_SUITE=elasticsearch-consumer-flow` 唯一使用 GitHub Hosted Ubuntu x86_64 临时部署，由 `online-hosted-elasticsearch-gate.sh` 复用共享 Hosted 生命周期。Business 夹具复用固定官方镜像及样例初始化，创建独占 ES 容器、随机回环端口和只读账号；System owner helper 创建非默认 Tenant、普通消费 User 与独立登记身份，通过正式 Engine API 注册引擎，登记凭据在验收前移除。普通用户只取得 System 目录、Meta 扫描/目录、Manager 预览和 Develop 查询权限，不取得基础设施管理权限。API 及同一身份的真实 Console 验证 Meta 重扫和稳定 item 身份、Manager 25 条文档与空索引、Develop ES DSL 查询，并核对 long 精度、对象/nested 结构、来源投影和排序。任何浏览器失败、报告身份不符、截图缺失或清理残留均不能通过。退出销毁业务容器、平台 Infra 和凭据，检查零残留；不使用个人开发部署或本地共享 PostgreSQL。原 macOS profile 删除，仅登记人工 `workflow_dispatch`；首次真实通过前不计为 T4 通过、不登记 schedule。确定性验证纳入 `make test-online-runner` 与现有 Online CI 自动登记检查。

## Model3D Runtime 确定性门禁

`make test-model3d-workflow` 覆盖 DAE / 3DS 首期源引用和动画边界、GLB 自包含结构、真实 PNG/JPEG 解码以及校验失败时不发布或替换已有 artifact。该入口由 `make test`、`make test-module MODULE=engines` 自动发现和 Platform CI 的 `model3d-workflow-tests` 执行；可通过 `MODEL3D_WORKFLOW_PYTHON` 指定已安装 requirements-dev 的隔离解释器。测试使用受控 converter runner，不计为专业转换器或平台真实链路验收。已登记 Make 入口的 Python engine runtime 由 Python CI 登记检查统一核对根聚合、标准环境准备、共享模块影响选择与 CI Job。

Model3D 原生安装的下载完整性、私有资源隔离和损坏缓存拒绝由现有 `make test-model3d-workflow` 自动发现；开发生命周期契约纳入 `make test-dev-lifecycle`。Linux 产品与 macOS 原生共用 IfcConvert 版本清单及 Cargo.lock。Manager Hosted T4 启动正式 Model3D Runtime 镜像并验证镜像身份和默认入口，退出按当次标签删除容器，不调用开发 wrapper；对应编排和登记检查沿用既有 Online suite。原生真实转换预检属于开发准备，不代替上述正式门禁。Manager Hosted T4 的模型覆盖包括 DAE、3DS、IFC、单体 OSGB 与 SKP。SKP 使用自有、带内嵌彩色 PNG 的英寸三角形和两个组件实例；Meta 只登记格式身份，Runtime 生成后核对米制尺寸、实例位移、内嵌贴图、缓存预览、血缘和领域清理。Model3D 标准产品构建必须在最终 Runtime 镜像中，通过正式 operator 转换 Business 自有压缩 OSGB 夹具并验证 GLB；builder 阶段的原生转换测试与最终 Runtime 的 `--help` 检查均不能替代该验证。

## Redis 键值数据集门禁

Redis 键值数据集门禁：`make test-common-redis-unit` 验证配置、认证、取消、TLS、规范键名、RESP 解码前预算、原生类型读取、Manager 路由与保护边界；`make test-common-redis` 使用独占真实 Redis 验证 Common 插件的 ACL、样本预算、精度及 stream 重复字段，System 登记与加密脱敏，Meta 的 keyspace 数据集身份、旧逐 key 目录清理、重扫稳定与失败保留目录，以及 Manager 原生预览。与 `make test-business-redis` 共用 `redis-owned-fixture.sh` 生命周期和固定官方镜像；均使用随机密码、随机回环端口并检查容器/卷/网络清理，不访问已有 Business 或 Infra Redis。消费 T2 由 Common/System/Meta/Manager/Business 变更触发，登记根 Makefile、模块/变更发现和 `release-and-t2-gates.yml`。共享生命周期通过 `ADDP_T2_LIFECYCLE_SCRIPT` 显式登记，CI 检查必须同时确认 owner 脚本调用、变更输入覆盖、Compose 归属和真实启动/清理逻辑，不能以未调用的 helper 冒充门禁。Manager 浏览器门禁验证 `data_type=key_value` 数据集中的键浏览和原生详情，并保留二进制、大整数、score 字符串和 stream 字段顺序；不得将其计为真实部署 T4。

Redis 跨模块 T4 门禁为 `make test-online ONLINE_SUITE=redis-consumer-flow`，仅由 `online-t4-gates.yml` 的人工 `redis-hosted-t4` Job 调用 `online-hosted-redis-gate.sh`，复用 Hosted 一次性 Linux x86_64 平台生命周期。Business 物理夹具使用固定官方镜像、随机回环端口/密码和 tmpfs，复用九个原生样例，不访问开发环境。System owner helper 创建非默认 Tenant、最小权限普通用户及独立登记身份；普通用户只取得目录阅读、Meta 扫描/目录和 Manager 阅读权限，不取得基础设施管理权限。登记凭据在业务验收前移除。套件验证 System 实时 server → keyspace 目录/事实、Meta 单个 `data_type=key_value` 数据集身份、Manager 九个原生样例及 page=2 拒绝；真实 Console 浏览器从 Meta 页面发起重扫、确认 DataItem ID 稳定，打开 Manager 资源树和各类原生预览，核对同一 User/Tenant 并输出截图和报告。任何浏览器步骤失败、证据缺失/身份不符或清理残留都不能判为通过。脚本与断言测试纳入 `make test-online-runner`，现有 CI 自动发现登记该 Hosted profile；首次真实 Hosted 运行成功前不计为 T4 通过，也不进入 schedule。

## Meta 扫描与结果归属门禁

Meta 专业扫描执行读取（2026-10-04 已确认）：直接 HTTP 回归覆盖当前 Owner Permission、合法 OAuth User、本人一次性执行、缺失发起人、同 Tenant 不同 User、跨 Tenant、跨模块/任务类型 UUID、已删除定义历史、分页总数及正确 404；TaskProvider 单独覆盖专用 Client、机器 Permission 与其他 User 发起执行。`make test-module MODULE=meta` 自动发现 API T1 与前端 T3；`make test-meta-postgres` 显式选择 `TestMetaScanExecutionReadAgainstPostgres`，事务回滚测试事实，不创建临时 database。现有 Release/T2 工作流自动登记此标准脚本；真实认证、Gateway 与 Worker 的直接 Meta 检查扩展既有 `orchestrator-execution` 手动 Hosted T4。OAuth User 和 TaskProvider 权限矩阵由 T1/T2 直接 HTTP 回归覆盖，该项历史 T4 不声明相应真实身份验收。历史结果与范围限制见[其他验收证据索引](验收证据索引.md#addp-验收证据索引)。

Manager mosaic whole 身份与完成边界（2026-10-16 确认）：父执行在原 lease／超时内等待原精确 Meta 扫描成功并取得真实 whole 身份，耗时包含扫描，不以 Runtime manifest 物理路径作为公开输出。`make test-manager-backend` 和 `make test-manager-postgres` 的 `TestIntegrationPostgresManagerMosaicWholeResultLifecycle` 覆盖等待期间保持 running、唯一正式输出、扫描失败／超时／取消、读取拒绝、错误目标／manifest 身份、缺失 item/fingerprint、截止时间后的受租约保护收尾及旧 lease 禁止成功。Meta 模块 T1 复用同一直接 HTTP 权限矩阵；`make test-meta-postgres` 同时运行专业读取和 `TestMosaicScanWholeResultAgainstPostgres`，验证专用 Client/Permission、跨租户、父子与 attempt/精确目标绑定、过期父 lease、闭合响应、S3/NFS 真实资源树身份、旧扫描事实拒绝、进度保留来源回执及扫描终态原子保存。Meta 专业读取的 PostgreSQL 夹具在事务内清空 Common 执行存储三表并调用生产 `execution.EnsureStore`，从空表运行正式 SQL 迁移；退出回滚并校验原表 OID 恢复，不借用本地残留约束、不自写约束替身。半套主体字段由正式 PostgreSQL 完整性约束拒绝，SQLite 验证接口拒绝，不移除或放宽归属断言。新增 Go 测试沿模块与 Platform Go workspace 自动发现，Meta T2 沿已有脚本登记进入 Release/T2，无新 workflow。共享 Client 扩散由 `make test-go` 覆盖；接口同步两模块 Swagger 并运行覆盖校验及 `make test-authorization`，新增夹具纳入 `make test-execution-fixtures`。真实容量复验仍由栅格会话同步计时说明并执行既有 `raster-workflow-capacity`，保留原预算、物理 oracle、扫描、Monitor 和清理断言；确定性门禁通过不代表完整 T4 通过。

Develop 产物自动扫描来源回归复用 Meta 专业读取的同一直接 HTTP T1/T2 夹具：覆盖 Develop 服务与 Permission、父执行及产物绑定、同租户原发起人读取、其他 User 列表总数不增加、跨租户拒绝、错误模块／任务类型／状态／主体来源和未声明目标拒绝、客户端自报主体或授权字段拒绝、失败请求不落子记录、安全投影不输出配置或授权。`make test-meta-postgres` 继续运行 `TestMetaScanExecutionReadAgainstPostgres`；Go 文件由 `make test-go`、Meta／Develop 标准模块入口和现有 CI 自动发现，无新增入口。Develop T1 验证产物先持久化再提交父 UUID，保存失败或父状态不可用时不发请求。真实 User／Gateway／Worker 的原故障复验使用已登记 `raster-workflow` Hosted T4，不以确定性测试代替。

Develop 自动扫描归属的历史 Raster 运行只证明已完成的扫描与专业读取链路，不计为完整 Raster T4 或完整资源权限闭环；直接接口的同租户其他 User、跨租户、拒绝来源及安全投影矩阵仍由 Meta PostgreSQL 门禁验证。历史结果与范围限制见[其他验收证据索引](验收证据索引.md#addp-验收证据索引)。



Meta 扫描范围精确化门禁：直接 HTTP T1/T2 验证产物完整 locator 冻结到 execution config，非法／跨引擎目标返回本地化 400，失败请求不落记录；Go T1 验证配置 JSON 持久化、去重锁身份以及表、集合、图、直接叶子和 file/object 内容边界。`make test-meta-postgres` 增加 `TestPreciseCatalogScanAgainstPostgres`，复用同一运行时用例与事务回滚，验证局部扫描成功或失败均不改变祖先扫描事实、兄弟 item 保留，显式完整范围仍可清理缺失项。路径中的点号不得截断，空目标不能退回全引擎扫描。标准模块入口与 Release/T2 已自动覆盖 Meta Go 文件及此脚本，无第二套 CI 登记。

公共表格 Provider 的单表解析和事实读取统一使用必需的 `GetTable`，删除 `findTableInfo` 的父级枚举路径；PostgreSQL、MySQL、Oracle、ClickHouse、Doris、TiDB、OceanBase 同步实现目标名称谓词。T1 分别检查四类原生 SQL 的参数绑定、目标谓词、源目录过滤及错误直接传播，并检查解析不读取字段、缺失／不匹配目标不进入详情。Common PostgreSQL 门禁的 `TestIntegrationPostgresCatalogPreciseReadOnly` 使用真实只读源角色，验证单次目标查询、轻量摘要、主键详情、未授权／不存在目标 NotFound 及撤销 schema USAGE 后不可见。

Meta PostgreSQL 门禁登记 `TestPreciseNativePostgresScanAgainstPostgres`：使用真实 PostgreSQL Provider 和只读源角色扫描 A，显式禁止父级 `ListChildren`，同时核对 B 元数据及父级扫描事实保持不变；不可读 B、缺失目标、撤销源 schema USAGE 后的扫描必须失败。它属于源 Provider 与 Meta 持久化链路的 T2，不等于 HTTP／Worker 的 T4，也不证明其余厂商真实服务均已验收。System 当前资源授权、撤权后 Worker 重核及 metadata 可见性仍属于后续授权阶段，不把本期范围测试、此前 Monitor T4 或产物归属 T4 当作该权限闭环通过。



Common / Meta 的历史工作区门禁结果仅适用于其记录范围；其他厂商真实服务、消费者 T2 与真实 HTTP / Worker T4 仍须由对应标准入口分别验证，不以局部工作区结果宣称完整资源权限闭环。历史结果与范围限制见[其他验收证据索引](验收证据索引.md#addp-验收证据索引)。


### 血缘采集器回归

`make test-meta-postgres` 覆盖超过单批 500 条执行、同创建时间的键集分页、目标延迟扫描、重复采集幂等性、单条错误隔离、轮内新增与游标后迟到成功执行的下一轮处理、取消，以及重复即时通知不恢复已关闭字段关系。Transfer PostgreSQL T2 另覆盖三个查询输入（含仅参与行筛选的来源）、查询改名、类型转换和完整物理行集合。沿用既有 owner 门禁，不增加部署依赖或公开 API。

历史执行范围及结果见[血缘采集器历史证据](验收证据索引.md#血缘采集器历史证据)，不替代当前源码的验证。

## 平台指标设施独占门禁

`make test-monitor-metrics` 由 Monitor owner 自建 Prometheus、受控指标源与实际私有 TLS 转发入口，登记固定镜像 tag/digest、共享部署配置和生命周期输入，随机 Compose project、回环随机宿主端口及私有时序卷。原生 OAuth/HTTP SD 经正式转发模板运行，验证固定路径/方法/查询参数和匿名请求拒绝，四个 owned 服务均纳入退出清理。实际验证 mTLS、采样周期与超时、单端点样本越限、断开恢复、SIGKILL 后 WAL 样本重放及退出零残留；同一来源还重新部署为 Docker Desktop 受限层，验证七采集器、Engine CPU/内存/内核对照、不同 PID/挂载/网络命名空间的磁盘原始计数前后快照对照、禁用文件系统/网络族、五项磁盘即时值及精确设备趋势、mTLS 与唯一 HTTP SD 查询和恢复后新磁盘样本。本地 macOS 执行证明 Desktop VM 采集，Linux CI 证明受限层语义，不冒充 Desktop 环境或完整 Linux 节点验收；临时 CA/私钥仅存在于本轮临时目录。根 `test-integration`、owner T2 自动发现、变更输入选择与 `release-and-t2-gates.yml` 使用同一入口。`test-dev-lifecycle` 验证部署未选择、错误选择、证书缺失和可选启动故障的核心 Infra 隔离。此门禁不替代后续节点绑定、HTTP SD 授权、业务埋点、资源查询及规模验收，不宣称完成 7 天物理保留时间窗验证。

## Manager Raster Mosaic 原生 Runtime 门禁

`make test-raster-mosaic-runtime` 是 Manager-owned Python T1 入口，覆盖已有瓦片渲染单测及真实 GDAL/COG、NumPy、坐标系与瓦片输出。`make test-module MODULE=manager` 自动发现该具名 Runtime 入口；Platform CI 的 Raster Mosaic job 按 Manager 变更选择，使用独立 Python 3.12 环境和匹配原生版本的 GDAL 绑定。共享 GDAL 生命周期修复另由 `make test-dev-lifecycle` 覆盖来源选择、失效绑定重建、环境隔离及停止归属；涉及共享入口时同时运行 `make test-geopython-workflow`。

## Security 治理与识别门禁

受保护资源有效期与筛选、银行卡号元数据识别扩展：`make test-module MODULE=security` 继续自动发现 Go T1、前端确定性/浏览器/构建及既有 Security PostgreSQL T2。`TestEnrollmentLifecycleAgainstPostgres` 增加与 SQLite 共用的字面子串转义、引擎/状态交集、分页总数、租户隔离以及有效期起点/截止点/退出状态回归；不新增测试数据库或入口。银行卡号能力 T1 验证元数据正例、模糊名称及非字符串反例，并沿正式发现执行将结果编译为四 Owner 规则。前端 T3 验证已同步投影的独立到期警示、日期展示、重新发现入口保留及服务端筛选/重置；使用隔离模拟 API，不计为真实部署 T4。API 字段与筛选参数由 Security Swagger 生成及路由覆盖验证。


## 业务图双引擎领域验收

Graph 的 Neo4j/FalkorDB 领域回归复用 `make test-common-neo4j` 与 `make test-common-falkordb` 的独占容器。相同领域测试覆盖显式目标绑定、实体/关系写入、原生唯一约束、构建审核、浏览展开、无向最短路径、全文搜索字段范围、外部索引冲突不修改、managed 按本体重建及知识服务。FalkorDB 同 label 的多定义物理索引取并集，但搜索仍必须限定各定义字段；不能用匹配数量相同替代字段隔离断言。Develop PreparedGraphQuery 与保护边界继续使用原有执行链路测试，不以 Graph 领域可信写入入口代替。

Graph Security 门禁沿上述两项领域 T2 验证纳管后的浏览、搜索、展开、路径、约束、推导、分析、空间同步、知识服务及返回标识的领域写入拒绝。Graph Go T1 使用正式 Router 与普通 User AuthContext 验证非默认 Tenant、未同步投影、纳管拒绝、匿名公开知识服务不能绕过及跨租户请求不取得引擎连接；HTTP 在途屏障延续至响应序列化结束，响应结束后的旧 Context 不可发起读取。Security Go 验证独立 `graph_read` 拒绝投影及无原值豁免；既有 Security PostgreSQL `TestEnrollmentLifecycleAgainstPostgres` 同时执行存量 Graph Owner 补齐、过期不续期、releasing 解除变化、released 不复活和幂等回归。Common PostgreSQL 的既有投影存储契约测试增加 `graph` owner。System 的 000218 Runtime 迁移复用 `make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package migration --test graph-protection-runtime"`，完整迁移与现有 CI 默认筛选同样命中。既有 Security Hosted 生命周期补齐 Graph，既有 security-transfer-protection 的 -all 生命周期保持不变，三项 Security T4 profile 预检要求 Graph Ready，消费者同步和期限断言增加第五个 Graph 回执；原四个字段执行出口仍单独验证，不冒充 Graph 属性脱敏。确定性编排回归由 `make test-security-online-runner` 接入既有 `test-online-runner` 和 Online CI 注册检查。不新增 T4 套件，不将这些确定性/T2 结果或管理员页面浏览计为普通用户真实端到端保护验收；System 源数据授权不在本轮范围。

目标路径的 PostgreSQL JSONB 保真、数据库唯一性、退役字段删除、迁移失败回滚和构建 execution 并发进入默认 `make test-common-postgres`。可在同样测试连接与清理约束下运行 `make test-common-postgres COMMON_POSTGRES_TEST_ARGS="--test graph"`，只证明该分组。三项 Common T2 都登记 `graph/backend/` 为输入，沿现有 owner 影响计算自动选中 CI；没有新增服务或测试 database。Go T1、Graph 前端与 Swagger 使用现有标准入口。上述验收不声明真实用户、Console、Gateway、Copilot 或运行中服务已重启验收。


## Manager 预览浏览器门禁

Manager 的唯一浏览器入口为 `make test-manager-frontend` 内部调度的共享 `scripts/test/frontend-browser-gate.mjs`，模块声明 `test:e2e`、Playwright 配置及 `addpBrowserTest` 夹具；独立回环服务/端口由共享隔离入口管理，不另外保留模块 shell runner、固定旧建议端口或生产 mock 开关。测试数据与外网地图均受夹具控制，不使用个人账号、服务或真实业务库。

共享组件 T1 由 common-frontend 所有，完整 Manager 页面组合后的布局/路由/预览 T3 由 Manager 所有，外层 iframe 与认证由 Console 唯一负责。断言优先角色、标签和少量稳定 testid，以浏览器实际可见性、bounding box、顺序及状态证明布局；异步等待依赖响应/状态，不使用长 sleep 或重试掩盖缺陷。失败 screenshot/trace/video 写操作系统临时目录或 CI Artifact。

普通表格、空间容器、分页、子项/字段弹窗、视角/locator 恢复及响应式场景按当前 Owner 用例覆盖。T3 使用隔离模拟 API，只证明页面行为；真实 System 登录、Gateway、Meta、数据源与权限闭环归已登记 T4，各格式/引擎证据不互换。代码变更由模块入口和现有前端路径自动发现，开发 restart 不隐式运行或接管测试。

Manager 静态轨迹的确定性回归须覆盖三格式、乱序与时区、Unix 秒的精确毫秒及负数、无效时间／坐标、完整性拒绝、同一设备同刻冲突位置、403／预算错误后的旧路线清除、迟到响应丢弃、服务端范围回显缺失及 GeoJSON 默认 CRS 初始化后的完整性提示。布局用例核对真实绘制像素与完整初始视野，包含 630px 窄视口、1.6 像素密度和南半球两条路线；该用例尺寸不作为产品最小视口规范。范围读取后端回归覆盖严格 query／目标与字段边界、混合类型设备 ID、bigint 文本标识、左闭右开区间、超过普通首段的匹配、CSV／JSON／GeoJSON 连续读取、预算与点数超限、保护失效、文件增长、流中途失败／阻塞取消，以及 JSON 闭合和稳定 EOF；Common 读取器仍是唯一解析路线。代码与模块 T1/T3 由既有标准入口发现，真实下载、权限与数据源闭环仍归[Manager Hosted 场景](scenarios/Manager与空间成果.md#设备静态轨迹范围读取)。

## Manager 快显回归与基准验证

### 预览页快显回归

快显相关改造完成后，至少按以下样例做一次预览页回归。回归重点是确认前端只消费后端 capability、Preview metadata 和 `available_actions`，不在前端重复拼接私有任务配置或维护任务准入白名单。

| 样例 | 初始预期 | 生成动作 | 生成后预期 |
| --- | --- | --- | --- |
| IFC 单文件 | 基础预览不可直接渲染时，应展示 GLB 快显生成入口；入口来自 `available_actions=generate_model_3d_glb`。 | 点击生成 GLB，前端只提交 `locator + action` 到 `/quick-view/actions`。 | 结果 ready 后自动刷新 capability / Preview，渲染源为 `model_3d_glb`，可在基础预览和快显之间往返。 |
| STL / OBJ / FBX 单文件 | 源文件可基础预览时，首次打开即支持基础预览；如后端建议 GLB 快显，应同时展示 GLB 生成入口。 | 点击生成 GLB，任务配置由后端从 capability 和 Preview source facts 生成。 | GLB ready 后快显渲染正常；切回基础预览仍读取源文件，不误读 GLB content URL。 |
| OSGB Scene whole item | capability 返回 `model3d_tiles.formats[]`，分别表达 3D Tiles / S3M 的生成能力、状态和不可用原因。 | “生成瓦片快显”菜单每次只选择一种格式，前端提交 `generate_model3d_3d_tiles` 或 `generate_model3d_s3m`。 | 两种 ready 结果通过分段选择器独立切换；一种生成中或失败不影响另一种预览，读取不依赖转换引擎在线。 |
| GeoTIFF / TIFF | 小 TIFF 可直接基础/动态预览；大 TIFF 或需要受管 COG 时，由 `available_actions=generate_raster_cog` 展示生成入口。 | 点击生成 COG，前端只提交 `locator + action`，不得拼 `task_definitions.config`。 | ready 后通过 `raster_cog/{id}/content` Range 读取，地图视角和样式状态保持正常。 |
| 3DGS PLY / SPLAT | 支持基础预览；需要受管 KSplat 时，由 `available_actions=generate_gaussian_splat_ksplat` 展示生成入口。 | 点击生成 KSplat，前端只提交 `locator + action`。 | ready 后渲染源为 `gaussian_splat_ksplat`；`.ksplat` 源文件不再生成二次 KSplat。 |

每个样例至少检查：

1. 预览页按钮是否由 `available_actions` 驱动，隐藏/显示与后端 capability 一致。
2. Network 中预览页直接生成只调用 `/quick-view/actions`，请求体只包含 `locator` 和 `action`。
3. 生成完成后，Preview metadata 使用 `preview_artifact_status`、`preview_artifact_task_type` 和 `preview_artifact_id`，不得再出现旧 `quick_view_status`、`quick_view_task_type` 或 `quick_view_id`。
4. 基础预览和快显切换只改变 `preview_state` 中对应模式和渲染域的状态，不覆盖另一种模式的视角。
5. 失败、超时或取消时，任务 execution 和结果表状态必须表达真实失败原因，前端不得把失败伪装成 ready。

确定性页面与组件验证沿用前述 Manager／common-frontend 门禁；真实身份、Runtime、数据源、原文件与领域清理仍按各自已登记 T4 suite 认定，不能用模拟 API 的页面通过替代。产品接口与生命周期见[快显实现规范](../../../manager/docs/快显实现规范.md)。

### 动态 MVT 基准验证

以下工具用于显式配置下的只读性能测量，不是第二套测试门禁，也不单独构成 T0–T5 的通过证据。分层测试仍通过根 Make 标准入口执行；基准样本不接管个人服务，不从固定业务表或开发机规模推断全平台默认值。

动态 MVT 阈值验证方案：

1. 验证目标不是证明动态 MVT 能覆盖所有数据量，而是确定 capability 可以安全开放的默认边界、提示等级和运行时保护。
2. 样本必须覆盖小、中、大三档记录规模，点、线、面三类几何，源 SRID 为 4326、非 3857 投影、源表已是 3857、存在 ready 矢量物化视图结果四类路径。
3. 每组样本至少验证四个场景：无可索引 3857 目标且走源表 `ST_Transform` 路径、源表本身 3857 且有 GiST 索引、存在 Manager ready 矢量物化视图目标且 `geom_3857` 有 GiST 索引、存在可验证外部 3857 物化视图。
4. 验证层级应覆盖推荐起始层级、低层级全局瓦片、中层级常用浏览瓦片和高层级局部瓦片，不能只测命中空瓦片或单一 zoom。
5. 指标至少包括单瓦片 SQL p50/p95/p99、服务端总耗时 p50/p95/p99、超时率、错误率、原始 MVT 大小、空瓦片率、扫描行数、DB CPU、连接占用和并发下的排队情况。
6. 通过标准应使用交互预算和稳定性预算表达，例如 p95 小于动态瓦片交互预算、超时率和错误率低于约定比例、原始 MVT 大小不超过前后端保护阈值；最终数值必须来自验证结果，不在本规范中预设。
7. 验证产物应给出两组配置建议：无可索引 3857 目标时的响应时间预算与超时提示策略，以及可索引 3857 目标存在时的响应时间预算与缓存推荐策略；两组策略都必须保留单瓦片超时、体积和并发保护，并回填到 Manager 配置管理页面“快显策略”的默认值或运维建议。
8. capability 诊断应能解释动态 MVT 性能模式，至少区分 `source_transform_path`、`ready_3857_target`、`source_3857_indexed`、`tile_cost_exceeds_realtime_budget`、`optimization_recommended` 和 `vector_tile_cache_generation_recommended`。
9. 瓦片缓存生成不复用动态 MVT 的响应时间预算做禁止条件；验证结果只影响瓦片缓存任务的提示强度、推荐顺序和预估耗时风险。

当前后端验证入口：

1. Manager Backend 提供 `cmd/quickview-mvt-benchmark` 作为动态 MVT 阈值验证工具入口，核心逻辑位于 `internal/mvtbenchmark`。
2. 该入口只读取显式传入的验证配置并连接目标 PostGIS，不接入 Manager API，不写入 Manager 业务表，不创建矢量物化视图结果。
3. 验证配置必须显式声明 schema、table、geometry column、SRID 和 tile 坐标；不得默认空间字段名为 `geom`。
4. 报告输出 JSON，包含每个场景和瓦片的耗时分位数、错误率、超时率、空瓦片率、MVT 原始大小分位数、连接池等待情况；开启 `explain_analyze` 时附带 PostgreSQL `EXPLAIN (ANALYZE, BUFFERS)` 文本，用于补充扫描行数和 buffer 观察。
5. 报告必须包含 `recommendations.render_paths`，按 `source_transform_path`、`ready_3857_target` 等渲染路径聚合实测结果，并输出候选 `quick_view_realtime_tile_timeout_ms`、`quick_view_realtime_tile_retry_after_sec`、`retry_policy`、`recommended_action`、`confidence` 和依据。若样本出现超时，应标记 `needs_higher_timeout_rerun`，提示用更高 `timeout_ms` 重跑后再调整部署默认值。
6. 报告可以包含 `recommendations.multi_scale_candidates`，仅记录后续多尺度快显优化的候选触发事实。候选条件限于可索引 3857 目标在低层级瓦片上仍超时或产生过大的 MVT；当前用户闭环仍是生成瓦片缓存，不新增“多尺度快显优化”入口。
7. 配置示例：

```json
{
  "dsn": "postgres://user:password@localhost:15432/dbname?sslmode=disable",
  "iterations": 5,
  "warmup": 1,
  "concurrency": 2,
  "timeout_ms": 3000,
  "explain_analyze": false,
  "scenarios": [
    {
      "name": "roads source 4326",
      "render_path": "source_transform_path",
      "schema": "public",
      "table": "roads",
      "geometry_column": "shape",
      "primary_key": "id",
      "srid": 4326,
      "tiles": [
        {"z": 8, "x": 210, "y": 99}
      ]
    },
    {
      "name": "roads vector materialized view target",
      "target_kind": "source_schema_materialized_view",
      "schema": "public",
      "table": "addp_vmv_8f4c6a2d9b1e7c0a5d3f6b42",
      "geometry_column": "geom_3857",
      "primary_key": "source_row_id",
      "srid": 3857,
      "tiles": [
        {"z": 8, "x": 210, "y": 99}
      ]
    }
  ]
}
```

## SuperMap Runtime 确定性门禁

SuperMap Workflow 的协议、算子目录及资源主机测试使用根 `make test-supermap-workflow`，通过临时构建目录运行不加载厂商 SDK 的 CTest，依赖 CMake、C++20 编译器和 nlohmann-json 开发包。CMake owner 必须同时登记根 `make test` 依赖及 Platform CI 调用，由构建登记检查和模块门禁核对。真实 SGM 读取、OSGB 导出和发布边界仍由受控 SDK 镜像构建阶段的 `sgm-conversion-test` 验证；公开协议门禁不代表 SDK 转换或 T4 已通过。

## GeoPython Runtime 确定性门禁

GeoPython 本地 T1 使用独立 Python 3.12 venv 与匹配的原生 GDAL，`make test-geopython-workflow` 复用开发入口的 GDAL/PROJ 和 Runtime 专属 ODBC 配置；CI 安装 GDAL、MDBTools ODBC、unixODBC 和 pkg-config，再安装匹配绑定。生命周期 T1 覆盖健康 GDAL 绑定复用、缺失绑定、同版本动态库失效及版本不匹配后的无缓存源码重建、重建失败和重建后仍不可用时拒绝启动，以及依赖及导入失败、端口归属、原生 PID 清理和先预检后停止；Runtime T1 使用真实 Gunicorn，验证健康就绪后注册以及跨请求状态。`make test-arcgis-open-formats ARCGIS_OPEN_FORMATS_ARGS=--runtime-only` 只覆盖真实 MDB/PGeo 读取，不能计为 Transfer/Oracle Spatial 往返通过。

## Document 原生转换门禁

Document 原生转换使用独立入口 `make test-document-native`，在已准备的私有 LibreOffice 和固定中文字体上，经正式 `document_to_pdf` 算子转换三页 PPTX，核对页数、中文文本、工作目录清理及源样例不变。缺失或损坏的依赖直接失败，不跳过、不隐式安装。测试使用临时输入、输出和 profile，不启动 ADDP 服务；由 Platform CI 的 Document Workflow Job 准备依赖后调用。普通 start/restart 只负责依赖与服务就绪，不执行该功能测试。HTTP／算子确定性测试继续使用 `make test-document-workflow`，跨模块产物生成与浏览器预览继续由 Manager T4 验证，三者分别报告。

## Spark 数据发布门禁

Spark PostgreSQL 发布复用 `make test-common-hdfs` 中已有的独占 PostGIS 和真实 Spark Worker，验证已有目标与新目标提交成功但响应丢失、提交前失败的回滚及清理、回滚未确认时保留证据，以及提交成功后关闭连接或暂存清理失败不改写已提交事实。故障注入只包裹正式 JDBC 控制连接，实际提交、数据核对与暂存检查仍由原生 PostgreSQL 和独立连接完成；T1 同时验证原始错误不被回滚、查询资源关闭或连接关闭错误遮盖。沿用现有 owner、CI 输入登记和零残留生命周期，不连接个人 Business 实例。

Spark MySQL/Doris 暂存发布由原生 Linux x86_64 Hosted 门禁 `make test-common-spark-table` 验证；独占 Doris 通过 Business 唯一入口验证新建集群和重复登记，核对 Backend ID 不变、FE/BE 就绪后完成官方连接器读写；历史 IP 地址迁移及反例由 `make test-business-config` 的确定性夹具覆盖，本机已有 Backend 的真实迁移另行验收，不计为 Hosted 迁移覆盖；macOS 的显式 MySQL 子集不计为完整 T2 通过。门禁拥有独占 MySQL 8.0、Doris 2.1.0、Spark Master/Worker，固定官方镜像摘要，无宿主端口、不连接 Business，失败与中断均删除本轮容器、卷和网络并核对零残留。断言覆盖真实 Worker、覆盖/追加/空成果、目标定义保留、字段与约束拒绝、计算失败、bigint/Decimal 精确读回、Doris SQL 生成 Decimal 的 JSON 写入精度和官方连接器读写回路中的中文、emoji、空串、null、合法尾随空格及长文本，以及 MySQL 提交响应丢失后保留原提交和暂存证据；不把 T1 mock 或现有 Go Provider 门禁当作 Spark 写入验证。变更选择通过声明的 Runtime 及夹具输入归 Common T2，Release/T2 CI 调用同一入口；此 T2 不证明 Develop/Console 端到端验收已完成。

## 关系型写入与安全诊断门禁

Doris／ClickHouse 会话失败回归分别沿用 `make test-common-doris-decimal`、`make test-common-clickhouse-decimal` 和现有 Release/T2 Job；T1 使用同一断言矩阵验证会话状态和参数绑定，不能代替原生 T2。每个引擎必须覆盖非法 JSON、后续 chunk 执行失败、写入取消、关闭取消、Abort、正常关闭和空会话七个场景，核对首次错误保留、失败后拒绝续写（含空批次）、重复关闭及提交标记。原生 T2 须从独立连接核对实际数据：先成功写入一行，再让下一批的前 1000 行提交、后一个 chunk 执行失败，必须保留已提交的 1001 行，同时会话失败且无成功标记；此前提交行的 JSON、bytes 与普通文本须保持原值。每个合成表先确认名称未占用，退出逐表清理并验证零残留；门禁独占固定摘要容器、随机回环端口及全部退出路径的容器／卷清理，不连接业务实例、不运行已保存的 Transfer 任务或触发扫描。ClickHouse 就绪检查必须等待镜像初始化结束后的正式服务，不能以初始化临时服务响应成功作为就绪证明。

写入安全诊断扩展使用统一执行失败诊断 v3。Common T1 覆盖错误链、实际列序号及严格投影拒绝（未知键、错误类型、非法序号、伪造类型、旧版本和不合法风险值）；既有 PostgreSQL JSON、Doris／ClickHouse JSON 与会话失败 T2 同时验证类型化错误，不修改原有物理行数、bytes 或空间保真断言。Transfer T1 在真实完成持久化边界验证诊断，并覆盖纯定义／源打开拒绝无风险事实，以及删除、准备、替换、写入和写后源读取／进度／关闭失败的风险事实。共享前端 T1 和 Transfer T3 验证中英文实际模板与隐藏原始正文。现有 Hosted 四失败场景追加稳定原因码、非法 JSON 的第 2 写入列及目标可能已改变的 API／浏览器断言；runner 不替代新版 Hosted T4 证据。所有新 Go 文件沿原有 `./...` 自动发现，复用现有 Make 入口和 Hosted Job，不新增权限、API 或外部服务依赖。

Oracle／达梦的同一类型化诊断由 Common T1 和既有 JSON／会话原生门禁验证。Oracle 使用 `make test-common-oracle-decimal` 的 Hosted Slim／Spatial 配置，达梦使用本机官方驱动入口 `make test-common-dameng`，不将达梦登记为 Hosted。回归须核对 NULL 不占绑定参数时的实际写入列、达梦 MERGE 的实际字段顺序、冻结类型、原生驱动错误链及不猜列；编码失败和 SQL 执行失败分别保留原有回滚、关闭及提交标记断言。达梦清理参数的范围拒绝与活跃测试容器保护由现有 `dameng-official-media-release-gate_test.py` 验证，沿 `make test-business-config`／平台 T0 自动发现；此检查及显式残留清理均不能替代原生 T2。

Common MySQL 标准门禁在测试建库成功后记录本轮精确 database 名称，正常或失败退出后逐项清理并核对零残留；若清理失败，保留只含确切名称的清单供恢复，不将其记为 T2 通过。`--inspect-owned-databases` 仅只读列出受限测试前缀的库及表目录，`--cleanup-owned-databases <确切名称> ...` 只接受 `addp_common_mysql_it_` 加 19 位时间戳，不接受业务库、通配符或 SQL 片段，不按前缀自动删除。JSON 会话夹具保留多批次及跨分块失败语义，按批发送，避免用逐行网络调用替代批量写入验收。

JSON 编码门禁同时验证已编码文档的 UTF-8：string／[]byte／json.RawMessage 中的非法字节必须在 SQL 前拒绝，保持 `json_invalid` 及实际写入列；合法中文、emoji、真实替换字符与显式 JSON 转义不被重编码。Common T1 覆盖过长、截断、孤立续字节和代理区编码，既有各 Provider JSON 原生矩阵覆盖三种预编码输入及失败后目标行数，普通 bytes 的非 UTF-8 原样保留。继续使用现有标准入口和 CI，不新增字符集配置或权限。

PostgreSQL COPY 的关闭失败保持由 Common T1 及既有 `common-postgres-gate.sh --test typed-write` 的 JSON 写入分组验证，默认完整门禁及已有 Hosted Common PostgreSQL Job 同时发现。真实回归覆盖 append／事务内 replace 下的 COPY 收尾非空拒绝、延迟唯一约束提交失败、编码失败和取消关闭；重复关闭须保留错误链和失败结果、不发布成功标记，物理读回与合成表／schema 清理独立核对。成功与空会话也保留原提交行为。

MySQL／TiDB／OceanBase 复用同一 JSON 原生夹具，沿既有 MySQL `--test typed-write`、TiDB 和 OceanBase 门禁验证安全列定位、原生错误链、失败回滚与提交标记。MySQL 空间路径仍验证几何及 bytes 保真。openGauss MERGE 的 JSON／数组错误由 Common T1 和既有 Hosted `make test-common-opengauss` 验证；列号来自实际批次字段顺序，类型来自冻结声明，原生执行错误不能猜列。上述入口已由现有 Common 共享依赖变更发现覆盖，无需新增 CI 作业。

历史结果与范围限制见[其他验收证据索引](验收证据索引.md#addp-验收证据索引)。

## Oracle 原生类型与空间门禁

Oracle 的既有 `make test-common-oracle-decimal` 默认使用 Slim 一次性容器验证非空间类型写入及删除；`make test-common-oracle-decimal ORACLE_SPATIAL=1` 使用固定摘要的完整镜像，额外验证原生 SDO_GEOMETRY、空间索引、DROP 后的登记状态、存在／缺失表的精确登记清理、重复删除及其他表登记保留。两种配置均由既有 Hosted Oracle Job 执行，容器只绑定随机回环端口，退出销毁容器及卷；不得连接业务 Oracle 或以模拟视图代替原生空间认证。

## 租户执行过程事件门禁

租户执行过程事件的真实 PostgreSQL 事务、并发限额、租约过期、Owner 范围分页和清理锁竞争由 `make test-common-postgres` 的 `TestExecutionEventsAgainstPostgres` 覆盖；Transfer 历史自由文本移除由 `make test-transfer-postgres` 的 `TestIntegrationPostgresExecutionEventsRemoveRetiredText` 覆盖；同一入口登记 continuous 事件、Runtime fencing／取消、自动恢复、恢复退避／熔断和结构阻塞组，验证真实行锁等待后过期的进展／终态／续租拒绝、错配租户／执行／模块拒绝、事件与状态回滚、每日并发预算、过期运行收敛及事件清理保留概览。Common 同一事件组验证 continuous attempt 迁移和 Owner 裁决预算，未开始的取消不生成过程事件。两者均已登记标准入口，Common 的全量门禁包含该执行事件组。共享运行过程组件的唯一所有权由 `make test-common-frontend` 自动发现，Monitor 的事件分页、截断及撤权清除回归由 `make test-monitor-frontend` 的确定性 Playwright 覆盖。上述测试不替代真实 User、Gateway、Owner 与 Worker 的 Online T4，也不代表个人环境已部署新的迁移和保留维护循环。

## 保护投影与执行诊断门禁

保护投影自身到期的验证沿现有 Go T1 和 PostgreSQL T2：Common 的 `TestPreparedOutputRejectsExpiredProjection` 与 `TestProjectionStoreResultExpiryAgainstPostgres` 共用一组四 Owner／native 与 query 断言，通过正式变化流安装带 365 天有效期的历史夹具，在明确时点验证截止前、截止时及截止后门禁，拒绝到期后的非空与空结果并保留纳管标记和 cursor。随后经同一变化流安装相同投影 ID 的更高 revision，验证旧读取绑定的保护器仍拒绝输出；另一个 Owner 存储实例刷新持久 cursor 后，新读取执行新保护决策，空结果和 null 保持正确，重新打开存储后仍能恢复同一修订及保护输出。此恢复验证不触发自动续期，也不取消纳管。Manager 的响应边界回归验证无数据和字段结构泄露；Develop、Service 验证已过期查询在执行前拒绝且不遗留在途计数；Transfer 既有 PostgreSQL 导出门禁验证 native 与 query 的过期拒绝、零新增写入和既有目标数据保持。输出保护必须重新校验全部绑定投影，不能只在读取开始时校验。既有 Go 自动发现和 Common／Transfer PostgreSQL test pattern 已覆盖新增场景，无新入口或外部依赖；本组不调整正式有效期，不改变系统时钟，不改写业务投影，不计为等待真实 365 天的 T4 验收。

Transfer bounded 执行失败的诊断沿既有 `error_details.code` 输出：不可证明查询输出血缘的类型化 `plugin.ErrQueryOutputLineageUnresolved`（含逐层 `%w` 包装）在唯一完成事务中保存 `query_output_lineage_unresolved`。执行完成内部接口传递 error，不先转字符串再猜测原因；私有 message 仍仅用于内部诊断，专业响应继续通过 Common Observation 隐藏原始文本。Hosted 派生查询拒绝验收只匹配该稳定 code、failed 终态、本次执行身份、零写入与空目标前后事实，不消费已删除的 error_msg，也不把连接错误或同文案字符串当作保护拒绝。T1 覆盖类型化／包装／无关错误、当前租约下完成与专业响应隐藏文本；现有 Transfer PostgreSQL 读取隔离门禁复验诊断字段及权限，已有门禁登记承接，不新增数据库或测试入口。

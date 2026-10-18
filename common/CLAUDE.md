# Common 共享后端模块说明

## 模块定位

`common/` 是 ADDP Go 后端共享库，承载跨模块复用的 API 响应、客户端、配置、模型、统一执行记录、内容 I/O、目录视图、调度、空间处理、SQL 构建、存储和通用工具。跨模块重复逻辑应优先抽取到这里。

明确 table leaf 的目录解析与事实读取必须使用必需的 `TabularCatalogCallbacks.GetTable` 精确查询；`ListTables` 仅用于显式 branch 枚举，不得作为单表查找的兜底。`ResolvePath` 只返回摘要，字段和主键等详情由 `DescribeEngineCatalogFacts` 获取；关系型插件保留原生权限过滤及系统目录隐藏。

## 重要包

`common/logger.WithoutSQLValues` 是 ORM 查询日志的统一值过滤器。它保留委托日志器的级别、SQL 结构、耗时和影响行数，在 GORM 格式化前移除绑定参数，且只向日志器提供固定错误类别、错误类型或合法 SQLSTATE；不修改实际数据库错误或绑定值。当前由 Develop 数据库连接消费，覆盖执行记录与查询结果预览的所有写入；其他模块的连接配置不在本轮扩大。

`common/exportartifact` 独占导出会话、请求摘要和发起用户来源核验实现。派发前保存绑定 execution UUID 与完整请求摘要；仅固定 Transfer 服务经过 owner 专用 Permission 核验后取得发起用户事实。`common/client` 通过 System 活动 Backend 注册信息调用来源接口，禁止客户端自报用户、回调 URL 或传递 User Token。来源只用于 `triggered_by` 与本人观测隔离，不派生执行或数据授权。

```text
common/
├── opaquetoken/    # 用途隔离的 JSON 不透明令牌编解码，领域绑定由调用方校验
├── api/            # 统一响应、错误和 handler 辅助
├── buildinfo/      # Go 服务统一构建身份和健康响应
├── authorization/  # Permission/内置 Role Manifest Schema、共享授权契约及 authtest
├── client/         # System、Meta、Asset、Service 等模块客户端
├── middleware/auth/ # System AuthContext 消费、Gin 上下文注入和租户隔离 helper
├── config/         # .env、部署配置、服务地址、端口检查和时区
├── resourcetree/    # Meta Engine Catalog / item 事实到资源树视图的投影和路径定位纯转换
├── contentio/      # 基于 Go io 的内容 Ref、Reader、Writer、Lister、RangeReader
├── engine/contentadapter/ # engine provider 到 contentio 的适配
├── engine/certification/ # 新引擎正式插件开发前的官方介质与协议认证测试
├── engine/selection/ # Engine capabilities 解析和跨模块选择 helper
├── jsonmap/        # decoded JSON map 通用读取工具
├── execution/      # common.task_executions 统一执行记录模型、仓储和迁移入口
├── format/         # 文件格式、parser / analyzer 及 PMTiles、Raster Mosaic 子域实现
├── models/         # 通用模型、能力声明和跨模块 DTO / 值对象
├── taskprovider/   # TaskProvider capabilities v1 解析和契约校验
├── repository/     # 通用数据库初始化和基础仓储错误映射
├── scheduler/      # 统一 Cron 调度
├── spatial/        # CRS、MVT、WKB、空间转换、PostGIS 空间 SQL 表达式
├── query/          # 查询参数绑定、SQL 副作用分析和跨引擎 SQL 方言
└── secretcipher/   # 跨模块 AES-256-GCM 敏感配置值加解密
```

`common/opaquetoken` 是 Service 查询游标/Feature ID 与 System 日志游标的唯一通用编解码所有者；采用用途派生 AES-GCM，不持有权限、存储边界或领域过期策略。由 `make test` 的 Common `./...` 自动发现。

## 开发规则

- PostgreSQL Catalog 的主键事实必须在仅有表 `SELECT` 权限时仍可完整识别。字段可见性沿用 `information_schema.columns`，主键成员按当前关系 OID 和字段编号从 `pg_catalog.pg_constraint` 读取；不得依赖会隐藏只读账号约束的 `information_schema.table_constraints`，也不得要求补授写权限。只读主键识别和未授权表不可见性由既有 `make test-common-postgres` 门禁验证。

- 新增共享能力必须保持模块边界清晰，避免把某个业务模块的私有逻辑沉淀到 `common/`。
- `common/authorization` 只提供 Permission/内置 Role Manifest Schema、解析/校验、确定性 Catalog Report、发布期聚合 CLI 和共享授权类型；业务 Permission 内容必须留在各 owner 的 `authorization/permissions.yaml`，产品内置 Role 内容必须留在 `system/authorization/builtin_roles.yaml`，不得建立 common 中央业务清单。
- 普通只读共享的有效期模式、UTC 微秒规范化及同参比较由 `common/authorization` 唯一提供纯值契约；它不持有决定、回执或 Grant，不处理当前责任，也不能用于放宽临时接入、管理委派或 Token 期限。
- 普通只读办理的完整历史绑定、受理核清响应及最小签发历史 DTO 由 `common/authorization` 唯一定义。`common/client.SystemFulfillmentClient` 按不可变 Tenant Context 使用 Service Bearer，消费当前调用主体查询、受理、原结果查询／关闭及原参签发／签发历史查询 API；不接受用户 Token、不处理可信业务批准、不决定权限、不保存 Grant，也不自动编排这些请求。签发历史须匹配原编号且时间非零；只有明确 `found=false` 且省略 `grant` 才表示未找到签发记录，HTTP 错误和缺失字段不能转换成未找到。
- `common/jsonmap` 只提供通用 JSON map 读写 helper，不承载 `meta_item.attributes` 规范语义。
- `common/format` 只提供通用格式、type info / format info、parser / analyzer 能力；Meta item 识别、claims / exclusive、`meta_item.full_name` 决策和 attributes 落库构造属于 Meta 模块。
- `common/contentio` 只表达内容定位和 I/O，不依赖 engine，不解析 format，不返回上层 DTO。
- `common/engine/contentadapter` 负责把 engine content provider 适配为 `contentio.Reader` / `Writer`。
- `common/engine/selection` 只按规范化 Engine capabilities 解析和筛选 Engine Instance，不定义 capabilities Schema，也不保存 Engine 事实。
- `common/engine/certification` 承载新引擎官方介质与独立协议认证测试，不登记 `engine_type`，也不作为上层模块可消费能力；认证通过后生产实现仍必须进入独立插件包和正式能力门禁。形成正式插件后，原认证入口继续维护介质及协议证明，不能替代插件 T2／T4。GaussDB 的认证源保存在该 owner 的 `gaussdb/testdata/`，仅复制到仓库外独立临时 Go module 构建；普通 build tag 不能隔离 `go mod tidy` 的依赖扫描，公开驱动固定版本及校验和，协议认证仍在独立 module 验证；Linux amd64／arm64 实库与 Linux／Darwin 四组合构建分别认定。GaussDB 已通过双架构原生准入，正式只读插件阶段允许将固定 gaussdb-go v1.0.0-rc1 纳入 Common，版本和校验和须与认证事实一致；未经正式准入的其他认证驱动仍不得引入 Common go.mod、包图或全仓 workspace。
- `common/resourcetree` 负责把 Meta 已落库的 Engine Catalog / item 事实投影为跨模块资源树视图，并提供 `ResourceLocator` / provider `EngineCatalogPath` 的纯转换能力。
- `common/resourcetree` 不持有 System / Meta client，不主动读取远程服务，不处理租户权限、token、降级策略、扫描或内容读取。
- `common/resourcetree` 中 attributes helper 只服务 `TreeNode.Metadata` 展示摘要，不作为通用 attributes 规范 API，也不写入持久 attributes。
- `common/taskprovider` 只承载 TaskProvider capabilities 的纯解析和规范校验，不访问 System 注册表，不调用 owner 模块，不处理执行调度。
- `common/client` 只放跨服务 HTTP/API 客户端，不作为 infra PostgreSQL `common` schema 的读写入口。
- `common/execution.ManagerProfileReadScope` 独占 Manager 剖析执行的 wire 范围及纯值校验／深拷贝，只表达全配置摘要和完整、规范、同引擎的 1–200 个只读叶子；`NewManagerProfileReadScope` 从不超过 512 KiB 的持久原始 JSON 对象规范化并计算完整摘要，保留 JSON 数字精度，不能从浮点数往返后的 `JSONMap` 补造。共享工厂不解释私有配置、不签发授权、不查询 Grant、不缓存 Allow。System 签发与 Manager 不可变 Provider 计划各自拥有领域事实。`SystemExecutionAuthorizationClient.IssueManagerProfile` 使用本次普通 User 凭据签发，`SystemServiceClient.CheckManagerProfileAccess` 使用当前 Tenant Service 凭据复核真实执行租约；固定路径不跟随重定向、不自动重试，只接受最小响应，不泄漏上游正文或凭据。Manager 生产采样接线仍须单独验证。
- `common/client.MetaClient` 只接受 `ServiceTokenProvider`，按 Tenant 使用 Fosite Client Credentials Grant 获取短期 Service Access Token，并且只发送 `Authorization: Bearer`。不得恢复 User Token 代传、`X-Internal-API-Key`、`X-Tenant-ID` 或可变 Tenant setter。
- `common/dataitem/authorizationfacts` 独占 Meta 授权资源事实 wire、严格校验和确定性边界摘要；独立为轻量纯值包，不依赖父包的识别器、format 或引擎实现，不持有 Meta 落库模型、Grant 或权限裁决。`MetaClient.ReadAuthorizationFacts` 使用不可变 Tenant Service Context，限制响应大小、拒绝重定向和畸形响应，不缓存许可或泄漏依赖正文。
- `common/client.SystemServiceClient` 只接受同时支持 Tenant 与 Platform 的 `ServiceTokenSource`。Tenant 调用必须先通过不可变 `WithTenantID` 选择 Context；模块注册（含可选 TaskProvider 声明）和心跳使用显式 Platform Token；所有业务请求只发送 Bearer。
- 唯一显式人类适配方法 `GetEngineAccessHandlingScope` 使用请求作用域的当前 User Bearer 读取 System 办理范围，不保留或替换为该客户端的 Service 凭据，不在 401 时改用机器重试。调用方先核验 User AuthContext，再核对返回的 Tenant、Engine 和操作人完整来源。Bearer 语法复用 `middleware/auth.CanonicalBearerToken`；提取 Token 不等于认证或授权。
- 每个调用 Meta 的模块必须使用独立 Confidential OAuth Client 和 Service Principal；调用前通过不可变 `WithTenantID` 选择该 Principal 的有效 Tenant Membership。
- `common` schema 中的共享表应按领域归入 `common/<domain>`，由领域包提供模型、仓储和 `EnsureStore`，仅 System Backend 在就绪前调用公共 schema 的迁移，其他进程只读验证版本；执行记录必须复用 `common/execution.TaskExecution`、`common/execution.TaskExecutionRepository` 和 `common/execution.EnsureStore`。
- API 响应优先复用 `common/api`。
- 用户 Bearer Token 统一调用 System `/api/v1/system/auth/context`；业务模块不通过 `/users/me` 验证 Token，不自行解析 JWT。
- `common/query` 承载查询参数绑定、SQL 副作用分析和共享查询能力。已确认的分析计算目标契约由 `common/query/plan` 的中立计划、`common/engine/plugin` 的开放编译接口与各引擎实现组成，见 [引擎插件规范](../docs/spec/addp引擎插件接口规范.md#数据库无关分析计算契约)；`query/plan` 已实现类型化 DAG、严格 JSON、语义校验与稳定指纹；`engine/plugin/analytical*.go` 已实现开放编译接口、物理绑定、冻结包及内部结果检查协议；SQL PreparedQuery 已接入不可变编译请求校验、检查记录消费及类型规范化，仍走原有一次性 Execute。`query/sqlcompile/result.go` 通过引擎注入的 ResultDialect 组合独立断言与结果根，不能对组合后的查询追加外层分页。真实 PostgreSQL 结果协议验证纳入 `make test-common-postgres`。无损整数转换由 `query/sqlcompile/integer.go` 统一生成安全值和违例条件，PG/MySQL 只提供原生转换与截断判定语法；`query/sqlcompile/conditional.go` 组合 CASE/COALESCE 的安全值与受分支约束的错误条件，未选分支不报错，已求值错误不能被补值吞掉；`query/sqlcompile/arithmetic.go` 通过 ArithmeticDialect 的精确十进制工作区实现整数加减乘和 decimal 四则运算，拆分乘积并用精确余数决定除法舍入，不读取会话配置或引擎名单；`query/sqlcompile/expression.go` 提供正式递归表达式入口，复用 plan.AnalyzeExpression 的既有类型规则，按作用域映射列并交由引擎编码值／精确比较；`query/sqlcompile/relations.go` 按依赖顺序编译关系节点，并通过 ScanDialect 接入完整 SourceBinding 的原生表扫描与字段类型检查，并把节点求值检查接入同一查询的结果协议；测试 fixture 不再自带表达式递归或固定 DAG 编译实现，`calendar.go` 统一校验严格日期、月初及月份偏移范围，通过 CalendarDialect 的原生操作保留日号并裁剪目标月不存在的日期；`date_buckets.go` 生成有界相交月份，空／反向／超限区间均进入独立求值检查；`text.go` 沿用 Literal 规范化表示完成文本转换，原生实现只负责十进制尾零裁剪与固定 ISO 日期格式；`query/sqlcompile/conformance` 是共享测试矩阵，由 PostgreSQL、MySQL 与 TiDB 的既有数据库门禁消费。`query/sqlcompile/instance_probe.go` 复用共享编译器探测固定语义，各原生 `analytical_instance.go` 在同一连接／事务检查实例版本与编码，返回实例认证报告；CompiledQuery 的来源绑定由执行桥接私有传入现有只读事务；原生 AnalyticalSQLExecutionValidator 锁定结构、复核实例与字段后才执行查询，原生 InstanceCapabilitiesResolver 据认证结果投影 analytical 能力，静态模板不声明支持。编译派生的 EvaluationCheck 保留求值错误，与业务 Assertion 分开，PG/MySQL/TiDB 正式编译器分别注册，共享 RelationalCompiler 组合原生组件，测试矩阵已删除临时编译器包装；engine/selection 按能力和生命周期派生分析选择状态。query/plan/result.go 在原结果根后追加类型化过滤、keyset、稳定排序、limit+1 和输出投影，运行时值使用独立参数，稳定键作为隐藏输出保留，原节点与独立断言保持不变；结果请求与原计划共用参数／节点／表达式预算。各引擎既有 AnalyticalRelations 集成门禁包含同一 ResultRequest 矩阵。Model/Service 已切换到中立 Plan 与冻结 AnalyticalPlanPackage：Model 通过 Meta 获取来源结构，Service 派生参数和输出并使用注册编译器执行；封闭 AnalyticalSQLProvider/AnalyticalDialect 及旧指标 SQL 拼接路径已删除。新包由 `make test-go` 的 Common `./...` 和既有 platform-ci Go 作业自动发现，无额外注册入口。PostGIS 等空间能力仍归 `common/spatial`。
- MySQL 读取集合与输出血缘共用 Dolthub Vitess AST，非递归 CTE 在分析树内按作用域展开；执行保持原 SQL。只接受已证明无副作用的函数，展开受节点和深度预算限制；计算输出不可伪造为源字段直通绑定。
- `common/engine/plugin.PreparedQuery` 是普通查询唯一的执行计划边界；Provider 必须从同一计划提供 `Analysis()`、`EngineReadScope()`、`ReadSet()` 与一次性 `Execute()`；未证明完整引擎范围时显式返回 `ErrQueryEngineReadScopeUnresolved`，不能用空范围代替。共享计划缓存并深拷贝范围，精确 ReadSet 不得越过已证明的上界；Projection v4 与共享门禁先验证完整同步，再按可信引擎范围排除无关保护；未知范围保留精确判断，Owner 不得直接调用 `ExecuteSQL()`，也不得另行解析查询语义或依赖。生产搬运的 `QueryReadSessionProvider` 和现有表写入 `TableResultProvider` 也必须消费这一个不可变计划；SQL Provider 通过共享的 SQL PreparedQuery 消费边界取回已绑定请求，不得二次绑定或另建执行路线。字段诊断只有在 `schema_coverage=complete` 时才能断言不存在；暂未实现完整读取集合的方言必须返回 `ErrQueryReadSetUnresolved`。
- `common/config` 承载部署配置读取和进程启动辅助；模块端口事实必须来自各模块已加载的配置，不维护第二张模块默认端口表。
- `common/secretcipher` 只承载跨模块敏感配置值的 AES-256-GCM 加解密，不承载 IAM、Permission 或业务字段识别。
- `common/dataprotection` 只承载 Security 与参与 Owner 共享的保护投影契约、校验、确定性算法和存量 payload 一次性协议转换，不读写 Security 业务事实；通用本地投影存储的 DDL、有序迁移、结构校验和迁移锁只允许在 `common/dataprotection/projectionstore` 定义，Owner 不得复制或扩展私有列。Owner 可注入事务变化屏障，使派生结果收敛与投影 cursor 原子提交，但屏障实现和派生业务语义仍归 Owner。
- 空间能力不要默认几何字段名为 `geom`，应通过元数据或调用方参数传入。
- `common/dataprotection/projectionstore.InflightReads` 是 Owner 本进程按 Tenant 追踪在途保护读取的唯一计数实现；Begin 必须先于保护门禁，幂等 end 必须覆盖最终输出。Owner 仍负责持久 cursor 刷新、跨进程有效 execution lease 核对和回执；计数不产生资源授权。
- `projectionstore.CaptureVersion/CommitVersion` 是数据库内派生提交的唯一保护版本边界；复用持久 checkpoint 锁，在同一锁下读取版本和事实、核验版本并执行 Owner 事务回调。采样和外部 I/O 不进入事务；Owner 仍负责提交时的有效期校验、结果保护和 lease 核验，版本不产生资源授权。真实 PostgreSQL 锁竞争及首次并发初始化由既有 `make test-common-postgres` 自动发现。
- 修改 `common/` 后通常需要 `./scripts/dev/restart.sh -all` 验证受影响模块。

## 验证

PostgreSQL 的 `query_output_rewrite.go` 已接入同一 SQL PreparedQuery 的一次性物理输出绑定，普通 Execute 与游标分批读取在交付前消费同一私有校验协议；原逻辑 ReadSet／OutputLineage 保持不变，失败绑定禁止原查询执行，未接入校验的原子写表桥接拒绝已转换计划。实际游标原生字段元数据复用 SQLResultField，偏移与事务内读取证据保留。业务出口尚未启用这条重写路径。它只接受直接字段／别名，支持字符串前后掩码、字符串固定值替换和移除，拒绝尚无等价证明的 DISTINCT、分组、集合操作、CTE、wildcard、派生表达式及 SM3；不安装数据库函数或扩展。别名／序号排序保留原输入表达式，算法值继续绑定参数，私有异常标志在结果交付前消费。Common Go T1 自动发现确定性测试；真实数据库与统一 Go 算法的值、缺失字段、Unicode、NULL、JOIN、排序、分页、源数据不变和精确夹具清理由 `make test-common-postgres COMMON_POSTGRES_TEST_ARGS="--test query-read-set"` 承接。默认完整 Common PostgreSQL 门禁亦包含该组，且缺少真实重写等价测试的 PASS 或有跳过都拒绝通过。编译结果衔接实际输出 `QueryResult.Fields` 契约：列名、顺序与类型说明须完整一致；内部异常标志及被移除字段在 Rows、Columns、Fields 中同步移除，其他真实原生类型与 precision/scale 保留，不以源字段或 Go 值补造输出类型。真实等价分组同时保留超过 JavaScript 安全整数范围的 NUMERIC 值与字段声明，并对同一请求的普通执行、分批游标结果、拒绝时无部分批次和一次性消费作对照。这些证据不代表已实现业务出口 SQL 自动重写。并行开发的冻结源码验证可由原工作区执行同一门禁并显式传入 `--source-root <源码 checkout>`；该 checkout 的提交必须是当前仓库 main 的祖先，Common 模块身份须匹配，数据库归属、实际映射和本地库名限制仍由原工作区核验，不把快照目录冒充 Infra 所有者。

```bash
cd common && go test ./...
ADDP_TEST_MYSQL_PASSWORD=<disposable-mysql-password> make test-common-mysql-data-protection
./scripts/dev/restart.sh -all
```

## 相关文档

- `common/README.md`
- `common/scheduler/README.md`
- `common/format/README.md`
- `common/spatial/README.md`
- `docs/concepts/addp共享模块介绍.md`

`common/schema` 提供 schema 版本发布、模块级迁移互斥与只读校验，不登记业务模块清单。业务模块自行声明版本并由 Backend 执行迁移；Worker 初始化不执行平台 schema DDL。

`common/processmetrics` 是 macOS/Linux Go 进程 CPU 累计时间、当前 RSS 与启动时间的唯一自身采样 SDK，不读取其他进程。`common/config` 解析显式 `ADDP_PROCESS_METRICS_DEPLOYMENTS`；普通注册生命周期与 System Bootstrap 复用 `StartOptional`，仅成功启动的独立 mTLS 来源发布不可变声明，来源失败不阻断业务登记。`common/client` 的当前 User 实例精确读取与后台私有投影分别授权，不以机器身份补读用户请求。设计及验收范围见平台运行监控设计第 6 节。

`runtimelog` 接收器将不可变模块／角色／节点／实例身份、首次采集时间和最后观测时间写入受控 status 元数据（UTC 微秒精度）；`DiscoverSources` 独立发现历史来源，扫描上限和错误明确标为不完整，`addp.log-sources/v2` 仅发送封闭原因码和实际发现次数，不发送文件路径或错误原文。`runtime-log observe` 向 System 上报目录，向 Monitor 上报健康，独立发送且不上传正文；不能从 EOF 推断业务退出结果。

查询保护引擎范围排除采用 Projection v4：Security 在发布前核对 Enrollment 的引擎 ID／完整名称前像与 DataItem 指纹，Owner 由投影派生范围索引。Owner 首次同步及旧协议重放须消费到 `has_more=false` 后持久确认 initialized，期间拒绝读取；重放只在最后一页 ack，保持 Security 确认游标单调。Neo4j 用标准 openCypher 9 AST、物理数据库与内置函数事实证明本地只读范围，模板和正式图查询执行同一不可变计划；同引擎纳管而精确 ReadSet 未覆盖时仍拒绝。验证使用对应 Go 模块门禁、Common／Security／Develop PostgreSQL 门禁和 `make test-common-neo4j`，新增 Compose 门禁已接入标准自动发现与 T2 CI。


业务 `falkordb` 插件与 Ontology Infra 分离，使用单端点、连接固定 Redis database 和 `server -> graph` 目录，完整 graph key 是 leaf 名称。共享 openCypher 9 AST 唯一位于 `common/query/cypherread`；Neo4j 保留原生物理数据库／内置函数证明，FalkorDB 在已认证 4.20.6 的原生函数与 GRAPH.RO_QUERY 边界内证明精确目标图。正式查询消费 PreparedGraphQuery 与 Projection v4，不由 Develop 按引擎类型跳过保护。`make test-common-falkordb-unit` 和 `make test-common-falkordb` 分别承接 T1 和独占真实 Common／Meta／Develop T2；两者均不操作已有业务图或 Ontology 投影。Graph 领域适配仍须完成全文索引管理权确认，不将基础插件验收冒充 Graph 完整支持。

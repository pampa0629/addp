# Common 共享后端模块说明

## 模块定位

`common/` 是 ADDP Go 后端共享库，承载跨模块复用的 API 响应、客户端、配置、模型、统一执行记录、内容 I/O、目录视图、调度、空间处理、SQL 构建、存储和通用工具。跨模块重复逻辑应优先抽取到这里。

## 重要包

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
- `common/engine/certification` 只承载尚未形成生产插件的新引擎官方介质与协议认证测试，不登记 `engine_type`，也不作为上层模块可消费能力；认证通过后生产实现仍必须进入独立插件包和正式能力门禁。
- `common/resourcetree` 负责把 Meta 已落库的 Engine Catalog / item 事实投影为跨模块资源树视图，并提供 `ResourceLocator` / provider `EngineCatalogPath` 的纯转换能力。
- `common/resourcetree` 不持有 System / Meta client，不主动读取远程服务，不处理租户权限、token、降级策略、扫描或内容读取。
- `common/resourcetree` 中 attributes helper 只服务 `TreeNode.Metadata` 展示摘要，不作为通用 attributes 规范 API，也不写入持久 attributes。
- `common/taskprovider` 只承载 TaskProvider capabilities 的纯解析和规范校验，不访问 System 注册表，不调用 owner 模块，不处理执行调度。
- `common/client` 只放跨服务 HTTP/API 客户端，不作为 infra PostgreSQL `common` schema 的读写入口。
- `common/client.MetaClient` 只接受 `ServiceTokenProvider`，按 Tenant 使用 Fosite Client Credentials Grant 获取短期 Service Access Token，并且只发送 `Authorization: Bearer`。不得恢复 User Token 代传、`X-Internal-API-Key`、`X-Tenant-ID` 或可变 Tenant setter。
- `common/client.SystemServiceClient` 只接受同时支持 Tenant 与 Platform 的 `ServiceTokenSource`。Tenant 调用必须先通过不可变 `WithTenantID` 选择 Context；模块注册（含可选 TaskProvider 声明）和心跳使用显式 Platform Token；所有业务请求只发送 Bearer。
- 唯一显式人类适配方法 `GetEngineAccessHandlingScope` 使用请求作用域的当前 User Bearer 读取 System 办理范围，不保留或替换为该客户端的 Service 凭据，不在 401 时改用机器重试。调用方先核验 User AuthContext，再核对返回的 Tenant、Engine 和操作人完整来源。Bearer 语法复用 `middleware/auth.CanonicalBearerToken`；提取 Token 不等于认证或授权。
- 每个调用 Meta 的模块必须使用独立 Confidential OAuth Client 和 Service Principal；调用前通过不可变 `WithTenantID` 选择该 Principal 的有效 Tenant Membership。
- `common` schema 中的共享表应按领域归入 `common/<domain>`，由领域包提供模型、仓储和 `EnsureStore`，仅 System Backend 在就绪前调用公共 schema 的迁移，其他进程只读验证版本；执行记录必须复用 `common/execution.TaskExecution`、`common/execution.TaskExecutionRepository` 和 `common/execution.EnsureStore`。
- API 响应优先复用 `common/api`。
- 用户 Bearer Token 统一调用 System `/api/v1/system/auth/context`；业务模块不通过 `/users/me` 验证 Token，不自行解析 JWT。
- `common/query` 承载查询参数绑定、SQL 副作用分析和共享查询能力。已确认的分析计算目标契约由 `common/query/plan` 的中立计划、`common/engine/plugin` 的开放编译接口与各引擎实现组成，见 [引擎插件规范](../docs/spec/addp引擎插件接口规范.md#数据库无关分析计算契约)；`query/plan` 已实现类型化 DAG、严格 JSON、语义校验与稳定指纹；`engine/plugin/analytical*.go` 已实现开放编译接口、物理绑定、冻结包及内部结果检查协议；SQL PreparedQuery 已接入不可变编译请求校验、检查记录消费及类型规范化，仍走原有一次性 Execute。`query/sqlcompile/result.go` 通过引擎注入的 ResultDialect 组合独立断言与结果根，不能对组合后的查询追加外层分页。真实 PostgreSQL 结果协议验证纳入 `make test-common-postgres`。无损整数转换由 `query/sqlcompile/integer.go` 统一生成安全值和违例条件，PG/MySQL 只提供原生转换与截断判定语法；`query/sqlcompile/conditional.go` 组合 CASE/COALESCE 的安全值与受分支约束的错误条件，未选分支不报错，已求值错误不能被补值吞掉；`query/sqlcompile/arithmetic.go` 通过 ArithmeticDialect 的精确十进制工作区实现整数加减乘和 decimal 四则运算，拆分乘积并用精确余数决定除法舍入，不读取会话配置或引擎名单；`query/sqlcompile/expression.go` 提供正式递归表达式入口，复用 plan.AnalyzeExpression 的既有类型规则，按作用域映射列并交由引擎编码值／精确比较；`query/sqlcompile/relations.go` 按依赖顺序编译关系节点，并通过 ScanDialect 接入完整 SourceBinding 的原生表扫描与字段类型检查，并把节点求值检查接入同一查询的结果协议；测试 fixture 不再自带表达式递归或固定 DAG 编译实现，`calendar.go` 统一校验严格日期、月初及月份偏移范围，通过 CalendarDialect 的原生操作保留日号并裁剪目标月不存在的日期；`date_buckets.go` 生成有界相交月份，空／反向／超限区间均进入独立求值检查；`text.go` 沿用 Literal 规范化表示完成文本转换，原生实现只负责十进制尾零裁剪与固定 ISO 日期格式；`query/sqlcompile/conformance` 是共享测试矩阵，由 PostgreSQL、MySQL 与 TiDB 的既有数据库门禁消费。`query/sqlcompile/instance_probe.go` 复用共享编译器探测固定语义，各原生 `analytical_instance.go` 在同一连接／事务检查实例版本与编码，返回实例认证报告；CompiledQuery 的来源绑定由执行桥接私有传入现有只读事务；原生 AnalyticalSQLExecutionValidator 锁定结构、复核实例与字段后才执行查询，原生 InstanceCapabilitiesResolver 据认证结果投影 analytical 能力，静态模板不声明支持。编译派生的 EvaluationCheck 保留求值错误，与业务 Assertion 分开，PG/MySQL/TiDB 正式编译器分别注册，共享 RelationalCompiler 组合原生组件，测试矩阵已删除临时编译器包装；engine/selection 按能力和生命周期派生分析选择状态。query/plan/result.go 在原结果根后追加类型化过滤、keyset、稳定排序、limit+1 和输出投影，运行时值使用独立参数，稳定键作为隐藏输出保留，原节点与独立断言保持不变；结果请求与原计划共用参数／节点／表达式预算。各引擎既有 AnalyticalRelations 集成门禁包含同一 ResultRequest 矩阵。Model/Service 已切换到中立 Plan 与冻结 AnalyticalPlanPackage：Model 通过 Meta 获取来源结构，Service 派生参数和输出并使用注册编译器执行；封闭 AnalyticalSQLProvider/AnalyticalDialect 及旧指标 SQL 拼接路径已删除。新包由 `make test-go` 的 Common `./...` 和既有 platform-ci Go 作业自动发现，无额外注册入口。PostGIS 等空间能力仍归 `common/spatial`。
- MySQL 读取集合与输出血缘共用 Dolthub Vitess AST，非递归 CTE 在分析树内按作用域展开；执行保持原 SQL。只接受已证明无副作用的函数，展开受节点和深度预算限制；计算输出不可伪造为源字段直通绑定。
- `common/engine/plugin.PreparedQuery` 是普通查询唯一的执行计划边界；Provider 必须从同一计划提供 `Analysis()`、`ReadSet()` 与一次性 `Execute()`，Owner 不得直接调用 `ExecuteSQL()`，也不得另行解析查询语义或依赖。生产搬运的 `QueryReadSessionProvider` 也必须消费这一个不可变计划；SQL Provider 通过共享的 SQL PreparedQuery 消费边界取回已绑定请求，不得二次绑定或另建执行路线。字段诊断只有在 `schema_coverage=complete` 时才能断言不存在；暂未实现完整读取集合的方言必须返回 `ErrQueryReadSetUnresolved`。
- `common/config` 承载部署配置读取和进程启动辅助；模块端口事实必须来自各模块已加载的配置，不维护第二张模块默认端口表。
- `common/secretcipher` 只承载跨模块敏感配置值的 AES-256-GCM 加解密，不承载 IAM、Permission 或业务字段识别。
- `common/dataprotection` 只承载 Security 与参与 Owner 共享的保护投影契约、校验、确定性算法和存量 payload 一次性协议转换，不读写 Security 业务事实；通用本地投影存储的 DDL、有序迁移、结构校验和迁移锁只允许在 `common/dataprotection/projectionstore` 定义，Owner 不得复制或扩展私有列。Owner 可注入事务变化屏障，使派生结果收敛与投影 cursor 原子提交，但屏障实现和派生业务语义仍归 Owner。
- 空间能力不要默认几何字段名为 `geom`，应通过元数据或调用方参数传入。
- 修改 `common/` 后通常需要 `./scripts/dev/restart.sh -all` 验证受影响模块。

## 验证

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

`runtimelog` 接收器将不可变模块／角色／节点／实例身份、首次采集时间和最后观测时间写入受控 status 元数据（UTC 微秒精度）；`DiscoverSources` 独立发现历史来源，扫描上限和错误明确标为不完整，`addp.log-sources/v2` 仅发送封闭原因码和实际发现次数，不发送文件路径或错误原文。`runtime-log observe` 向 System 上报目录，向 Monitor 上报健康，独立发送且不上传正文；不能从 EOF 推断业务退出结果。

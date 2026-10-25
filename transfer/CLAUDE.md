# Transfer 模块说明

## 模块定位

Transfer 模块是 ADDP 的数据传输中枢，统一负责 `sync` 任务、任务配置、字段映射 / 转换编排、写后 Meta 扫描触发和执行运行时。bounded execution 由独立 `transfer-bounded-worker` 通过 PostgreSQL execution claim 承担；continuous execution 由独立 `transfer-continuous-worker`/supervisor 承担。导入 / 导出是 Manager、Develop 等调用方的用户动作语义，不是 Transfer 的 `task_type`。可复用同步配置才创建 `transfer.transfer_tasks`；一次性导出使用 `POST /api/v1/transfer/executions` 直接创建 `source_task_id=null` 的 bounded `sync` execution，响应只暴露统一 `execution_id`。

当前主路径基于 `common/engine`、`common/format`、`common/contentio` 和 `common/engine/contentadapter`：

- Transfer 负责任务 JSON、planner、policy、transform、worker、checkpoint、日志、指标和写后 Meta 扫描触发。
- 一次性执行入口只允许已认证且具备不可委派 `transfer.execution.create` Permission 的模块 Service Client 调用，execution 的 `source` 必须由 `addp-<module>` Client ID 推导，调用方请求不得自报 `source_module`。同一模块使用不可委派 `transfer.execution.read` 回查自己创建的 execution，Transfer 必须再次按 Client ID 校验 `source`，不能借此读取其他模块或用户任务 execution。入口接受强类型 source / target / fields 契约，并在 Transfer 内转换为唯一 planner 配置；查询调用方必须把已解析的全部关系输入按稳定顺序写入 `source.query.inputs[]`。Transfer 不自行解析 SQL/MQL 反推资源，但必须使用同一 `PreparedQuery` 的 Provider `ReadSet` 验证调用方声明；未声明 `inputs` 的 Console 单源查询必须精确只读取所选 source leaf。通过验证的输入才能写入血缘。调用方不得创建临时 Transfer task，也不得保存 Transfer task ID。bounded worker 必须同时执行有任务定义和无任务定义的 `sync` execution，并只在 `source_task_id` 存在时更新任务摘要。
- 具体 engine-native 读写由 `common/engine` 提供。
- 具体格式和数据类型读写由 `common/format` 提供。
- content 的定位、读取、写入、range 和 scope list 由 `common/contentio` 表达；multi ref 的组织规则和读写语义由 `common/format` / `common/dataitem` / Transfer 编排层表达；engine content provider 到 contentio 的桥接由 `common/engine/contentadapter` 提供。
- 旧 Transfer 私有 reader / writer 插件体系、旧 `pkg/pipeline`、旧 `pkg/plugin_loader` 不作为新功能入口。

table 类型 Transfer 主链路已经稳定：native table、encoded single file/object、encoded multi refs 和 encoded whole scope 都统一走 planner + executor + common provider，不按具体引擎组合建立专用通道。后续新增 table 能力应优先补 `common/engine` 或 `common/format`，不要在 Transfer 内恢复私有 reader / writer。

bounded 字段血缘复用同一读取计划的 Provider 证据：直接原生表按实际结构组合 field mapping；查询按 PreparedQuery 的完整 OutputLineage 与真实结果字段组合，不自行解析 MQL/SQL。多来源必须声明全部 query.inputs 并匹配同次 ReadSet，按原生路径绑定端口，分别冻结每个来源结构；同一结果列可有多个值来源，只有行筛选作用的来源不生成字段边。来源快照保留原始字段名及结构化 path，不能保存查询别名结构冒充源集合。MongoDB 透明 aggregate 的嵌套字段别名与数组索引沿同一路径记录，数组索引及实际脱敏输出标记 derived；未知或不完整证据仍 unavailable。只有新的成功执行产生字段事实，旧执行不按当前任务配置补造。

Security 对 Transfer 的稳定动作名为 `export`，它表示受保护数据离开源 DataItem，不是 Transfer `task_type`。`bounded + snapshot` 的统一 Native TablePipeline 必须在字段映射、类型转换、空间处理和目标 writer 前遮盖/抑制；原生表不按 `engine_type` 建立保护分支，统一按精确 Locator 与当前表结构校验并处理 `BatchData`。查询源必须从同一 PreparedQuery 取得 ReadSet 和 OutputLineage；当前已验证 PostgreSQL 可证明查询，以及只包含 `$match`、`$unwind`、`$project`、`$sort` 透明阶段的 MongoDB aggregate，其他无法证明完整输出血缘的查询继续失效关闭。MongoDB collection 到 `mongodb_extended_jsonl` 的原始记录格式导出按精确 Locator 与 Meta 当前字段结构校验同一 `export` 投影，在 Provider 保留 BSON 标量的文档对象上完成保护后才编码 Canonical Extended JSON。Security 生成引擎无关的动作投影，Transfer 只在已实现并验证字段级保护执行器的执行形态上开放；raw copy、watermark incremental、Kafka replay、encoded source、continuous/CDC 以及其他无法证明完整字段身份和输出血缘的查询，在各自执行器完成前仍资源级失效关闭。

只读原生查询结果到 table 的 bounded 搬运属于同一主链路：source 必须消费 `common/engine.QueryReadSessionProvider`，target 继续消费 table write session。MongoDB 嵌套 BSON 的路径投影、数组展开、去重和关系合并由只读 MQL 在源端完成；Transfer 只搬运最终扁平行、执行显式字段映射和严格类型转换，不提供递归 JSON 自动摊平器。

MongoDB 查询的 Console 基础结构整形构建器只是一种 MQL authoring 能力：通用支持 `可选单次 $unwind -> $project`，保存事实仍只有 `source.query` 中的标准 MQL command object。基础界面只决定文档/单数组的行粒度和源字段选择；单数组模式只允许展开一个数组，可以选择多个数组元素叶子字段，并可选择多个不位于任何数组下的父文档叶子字段随每个元素行重复携带；文档标识自动携带。查询输出名由全部源路径按稳定顺序统一编译，不得因选择顺序改变；PostgreSQL 目标名称和类型只在后续 `field_mapping` 配置。构建器不得暴露筛选、排序、空数组保留、投影别名、原生 MQL 编辑或查询参数等拼装细节，不得保存第二份 DSL、硬编码业务 collection/字段、猜测多数组粒度或承担业务聚合。只有严格属于可逆子集、且 collection、数组和投影路径均与当前 Meta 事实一致的 MQL 才能结构化编辑；其他已有查询在 Transfer Console 中只读展示，复杂查询开发归 Develop。

关系型查询的 Console 基础构建器只承担 Transfer 的轻量 ETL authoring：源关系固定为用户已经选择的一个 native table，通用支持字段投影和一层 `all|any` 行过滤，过滤值全部编译为 `source.query.parameters` 中的类型化参数。可用查询语言、默认语言、标识符引号和参数类型必须来自所选 Engine Instance 的 `compute.query` capability；`bigint/decimal` 必须作为十进制字符串通过 `string` capability 无损传递，Planner 必须在调用 Provider 前再次校验参数开关、语言和值类型。单语言引擎不提供语言选择，未声明 `read_session` 的引擎不开放查询源。保存事实仍只有标准 `source.query.language=sql`、SQL `statement` 和 `parameters`，不得保存第二份可视化 DSL。基础构建器不得提供 Join、多表输入、聚合、计算字段、别名、排序、Limit 或方言私有函数；目标字段名和类型继续只由后续 `field_mapping` 配置，复杂数据开发归 Develop。只有严格属于可逆子集的 SQL 才能显示和编辑为基础表单；其他合法只读 SQL 只允许在 Transfer Console 中只读展示，不提供高级 SQL 编辑或保存入口。

## 技术栈与端口

- 后端：Go + Gin + GORM，默认端口 `8083`，环境变量 `TRANSFER_BACKEND_PORT`。
- 前端：Vue 3 + Element Plus，开发端口 `5176`，启动脚本环境变量 `TRANSFER_FE_PORT`。
- 数据库：PostgreSQL `transfer` schema。
- 依赖：System、Meta、PostgreSQL、Redis、MinIO/S3。

## 重要目录

```text
transfer/
├── authorization/
│   └── permissions.yaml       # Transfer Permission Manifest，发布期聚合事实源
├── backend/
│   ├── cmd/server/main.go
│   ├── internal/api/          # tasks、executions、engines、capabilities
│   ├── internal/planner/      # source/target endpoint -> table transfer plan
│   ├── internal/executor/     # 基于 common engine/format/contentio 的 table transfer executor
│   ├── internal/service/      # task、execution、system engine resolver、Meta scan 触发
│   ├── internal/worker/       # PostgreSQL bounded runner；owner scheduler 位于 Backend
│   └── pkg/vfs/               # 底层虚拟文件辅助能力，非新 transfer reader/writer 主入口
├── docs/
│   ├── 数据库架构.md
│   ├── transfer-基本概念及配置说明.md
│   ├── transfer-任务语义与同步模式.md
│   └── tables/
└── frontend/src/
    ├── views/                 # TaskList、TaskWizard、TaskDetail、ExecutionDetail
    ├── components/
    └── api/
```

## 核心 API

Agent `transfer.task.create` 沿唯一任务创建 API 委托，要求精确 scope/audience 和当前 Tenant 范围的创建权限；不沿用运行授权。owner 拒绝调度、启用、自动扫描、continuous、incremental 和非 native table 配置；成功只持久化 idle/stopped 任务，不创建 execution、不访问或写入业务数据。所有其他 Transfer 路由默认拒绝该委托令牌。Permission Manifest 7 与 System 向前迁移 181 只开放创建权限的委托标志，不增加角色权限。平台能力定义由 Ontology 提供，源/目标事实经其 owner 获取，不在 Transfer 保存第二份平台定义。

Transfer 是 `transfer.execution.*`、`transfer.task.*` 和 `transfer.task_provider.*` 的 Permission owner；定义只存在于 `authorization/permissions.yaml`，通过 `common/authorization` 发布期聚合，不在服务启动时动态注册。模块 Runtime 创建一次性执行使用不可委派的 `transfer.execution.create`，按来源隔离回查结果使用不可委派的 `transfer.execution.read`；用户任务管理只使用 `transfer.task.*`；Orchestrator Runtime 只使用 `transfer.task_provider.read|execute`，并由固定 `addp-orchestrator` Service Client Guard 收窄。`transfer.task.cancel` 是 IAM 目标目录能力，当前真实执行取消仍未实现，首次 SQL seed 前必须通过路由覆盖门禁处理。

路由前缀：`/api/v1/transfer`。

- 公共连通：`GET /ping`。
- 资源选择与资源树：引擎列表及资源树统一使用 Meta engine / resource-tree / item API；Transfer 不保留私有引擎列表、数据源树、节点 children 或表 metadata 代理接口。
- TaskProvider 标准任务：`GET /task-provider/tasks`、`GET /task-provider/tasks/:task_type/:id`、`POST /task-provider/tasks/:task_type/:id/execute`、`GET /task-provider/executions/:execution_id`，其中 `task_type` 固定为 `sync`；四个端点不接受用户任务权限。
- 任务定义：`GET /task-definitions`、`POST /task-definitions`、`GET /task-definitions/statistics`、`GET /task-definitions/:id`、`PUT /task-definitions/:id`、`DELETE /task-definitions/:id`、`POST /task-definitions/:id/start|pause|resume`、`GET /task-definitions/:id/executions`。`pause/resume` 只控制 owner schedule，不中断 active execution。
- “传输任务”列表页提供传输任务创建助手，由 Copilot `/api/v1/copilot/transfer/generate` 只识别源资源意图；向导调用 Manager 搜索/资源事实和 Meta 祖先链得到真实候选，确认时重读 owner 事实，目标继续使用共享资源树。唯一候选也必须由用户确认；之后在助手内依次确认目标引擎、目标父位置、目标表、字段映射和任务配置。声明 `limits.table_write.decimal` 的新目标表复用 Transfer 字段定义推荐 API，按目标 capability 校验基于源数据生成并展示确认；不得按 MySQL、OceanBase 等 `engine_type` 建立名单。Copilot 接口只消费上下文，不调用资源 owner、不创建或启动任务；最终仍使用本模块 `task-definitions` API 和 `transfer.task.create` 权限，由 Transfer 独立校验。
- Transfer 不提供模块级全局执行列表或监控面板；任务列表通过 `MonitorExecutionsButton(module=transfer, task_type=sync)` 进入统一 Monitor。任务详情保留当前任务的执行历史和 Transfer 领域诊断、结果、重试操作，单次领域详情继续使用 `ExecutionDetail`。
- 数据库 CDC 结构变更：`GET /task-definitions/:id/schema-change` 查询当前请求，`POST /task-definitions/:id/schema-change/approve` 人工审批 additive migration。
- 业务 Kafka DLQ 只读管理：`GET /task-definitions/:id/dead-letters`、`GET /task-definitions/:id/dead-letters/:identity`。只公开 tenant/task scoped 安全控制索引，不返回 Infra Kafka payload reference 或原始 key/value/headers。
- 字段映射：字段映射写入 `config.transforms[type=field_mapping]`，不提供独立 mappings 主路径。
- 对象存储目录选择统一走 Meta resource-tree；Transfer 不再保留私有 object-storage 浏览 API。
- 执行记录：用户管理接口为 `GET /executions`、`GET /executions/statistics`、`GET /executions/:execution_id`；模块一次性 execution 的来源隔离回查接口为 `GET /executions/:execution_id/result`。
- 执行管理：`POST /executions/:execution_id/retry`、`GET /executions/:execution_id/progress|events` 按统一 `execution_id` 定位执行记录；当前没有真实 worker 中断能力，因此不提供 cancel/stop API，TaskProvider 保持 `supports_cancel=false`。
- 转换器：`GET /transforms`、`GET /transforms/stats`、`GET /transforms/:name`、`POST /transforms/:name/validate|test`。

### 角色专用导出选择

当前第一方登录用户可在任务配置中一次选择并保存 `config.source.protection_choices`，空选择采用导出默认保护。复用共享 FieldMaskingChoices 与 Common 选项解析，候选与检查分别为 `GET /source-protection-options?source_locator=...` 和 `POST /source-protection-checks`，均按当前账号核验、沿用 transfer.task.execute，不接受用户／角色选择器。来源保护只读取 System Runtime Descriptor，不为判断字段保护获取连接凭据。

首期只开放手动 bounded/snapshot/native table 及其 PreparedQuery 来源；保存及名称更新重新校验，切换来源／模式不丢弃旧选择，定时、委托、编排、continuous、增量与 encoded/record 来源不消费特殊方式。启动和手动重试在同步用户请求内签发 audience=transfer 的本次执行授权，要求现有 system.execution_authorization.create、transfer.task.execute 和实际源读／目标写效果资格；不增加角色或权限。缺执行权限返回权限拒绝，角色／选项失效返回保护选择冲突，两者不混淆。

Worker 按实际 EA／execution／attempt／lease 逐批读取 System 当前角色，以该用户 EA 获取源和目标引擎访问，在字段映射与 Writer 前应用 export 保护。选项、资格或来源失效明确拒绝，不回落默认。Develop 全量导出使用独立 export 选择与共享会话精确摘要核验来源，再绑定签发的真实用户执行授权；默认导出仅保留已有观察来源，不自动提升为执行权限。原值授权、应用凭据和角色变更不在本轮范围。T1、真实 PostgreSQL 和前端门禁见 Security 同日记录；真实账号全链路 T4 需单独验收，不能由上述门禁推定。

2026-10-25，既有隔离 suite 领域 v30 增加角色资格自然到期专项：期限内独立任务输出和经 Develop/Transfer 生成的实际 CSV 均全遮盖；同一登录会话自然到期后任务启动、名称更新及直接选择核验409/transfer_protection_roles_unavailable，任务不新增执行且目标保持；旧文件/会话与再次导出经现有 Develop BFF 为409/query_protection_choice_invalid，不把合并提示声称为已细分。显式清除选择后默认任务/新CSV恢复，旧特殊文件仍拒绝。主功能权限、Policy与投影保持，成功不撤销/重登/续期；失败/中断先删任务再清理仍有效资格，精确保留其他用户审计。新增场景及凭据隔离沿[Online规范](../scripts/docs/Online数据保护.md#transfer-资格自然到期与旧文件)；`make test-security-online-runner` 的38/55/30/274项Python、相关Go及登记通过（T0/T1），真实HTTP/Hosted未运行，不新增浏览器通过声明。本轮未变更产品API、授权规则或服务生命周期。

2026-10-25，Common Transfer BFF现已严格保留资格/方式两类已声明409，Develop导出创建、状态及文件分别映射query_protection_roles_unavailable和query_protection_choice_invalid，源Transfer自己的错误码和裁决保持。未知码或异常响应不再伪报方式失效。隔离领域v31已将自然到期的导出断言改为角色精确码，Runner T0/T1通过；新真实Hosted T4未运行。详见Security原因细分记录及Online数据保护规范，v30合并提示仅是历史事实。

## 执行规则

- 服务代发导出必须按已认证 Client ID 推导 Manager／Develop owner，再从 System 活动模块注册信息解析 Backend 地址，使用 Transfer tenant Service Token 核验导出会话、预绑定 execution UUID、完整请求摘要和待执行状态。核验得到的用户只写入 `triggered_by`，不能写入执行授权 actor 字段。请求不接受自报用户或回调地址；无来源的机器执行不声明人类发起用户，不能为旧记录猜测归属。

- 新任务配置必须使用 `runtime.boundary`、`load.mode`、source / target endpoint 和 `target.policy.apply_mode` JSON；旧顶层 `mode`、`write_mode`、`connector_type`、`source_config`、`target_config`、`output_format`、`file_type`、旧 endpoint `engine_id` 等字段出现即拒绝。
- Transfer 任务类型固定为 `sync`；不得新增或兼容 `import`、`export`、`transfer` 等旧任务类型。
- table transfer 统一走 `internal/planner` + `internal/executor`，按 data type / representation / layout 分叉，不按具体引擎组合分叉。
- encoded file/object 读写必须通过 `common/engine` content provider + `common/engine/contentadapter` + `common/format` provider，不在 Transfer 中新增私有 reader / writer。
- Shapefile 等 multi 文件格式通过 `contentio.Reader` / `contentio.Writer` + `[]format.RelatedRef` 与 `common/format` multi table provider 接入。
- FileGDB/PGeo 等 container table 源只接受通用 `source.options.child_name`；planner 必须从 Meta `type_info.container.children` 校验 child 并解析 `native.table`，任务配置不得直接保存 `layer` 或 `child_table` 等 provider 私有入口。
- overwrite / append 是 Transfer policy；删除指定资源由 `common/engine` ResourceDeleteProvider 提供。
- native target 在删除／准备之前消费 Provider 可选的纯 `TableWriteDefinitionChecker`，拒绝已知不支持的字段声明；PostgreSQL、openGauss、KingbaseES、TiDB 与 OceanBase 已实现；openGauss／KingbaseES 沿同一 PG 协议检查，TiDB／OceanBase 沿同一 MySQL 非空间写入核心检查，均保留各自的空间限制。MySQL 非空间核心在连接前拒绝原生 array，不靠默认 TEXT 映射或自动改成 JSON 兜底。没有检查接口或实际值未知不能当作通过，逐值编码、现有授权／Security 和执行时结构校验继续生效。合成 PostgreSQL 数组源到 replace 目标的拒绝及既有目标 OID／哨兵保留，由 `make test-transfer-postgres` 自动覆盖；不意味着 replace 全流程原子回滚。
- checkpoint 当前只用于进度展示、故障定位和 provider marker 观测；失败执行 retry 按 restartable 从头重新入队，append 任务 retry 会被拒绝。不得宣称 table Transfer 已支持 checkpoint resumable。
- 大数据传输要优先考虑批大小、连续读取 / 写入 session、进度日志和 restartable retry。
- bounded Worker 不使用消息载荷；以 `common.task_executions` 中冻结的执行配置为唯一输入，并通过 `lease_token` 条件写入。
- PG bounded 产出事实在同 attempt／目标的成功提交时冻结实际读取会话的完整 ReadSet 与 Provider 的 `created_and_committed`；来源未知为 null，新建提交未证明为 false，不从配置、行数或血缘补造，不在此处确定 Grant 期限。私有 `target_production` v3 使用封闭 JSON 字符串保存在执行 metadata 中，避免通用 JSONMap 的浮点往返改变 Engine ID；仅通过 owner codec 读取，不兼容或补填旧事实，原生 CreationWitness 不持久化。固定 System／Meta 机器通过同一回执 v3 核验同一证据，不向普通执行详情或 TaskProvider 输出泄漏。任务期间不进行 schema 改名、迁移等结构运维；Provider 提交前检测已发生的路径异常，不增加父 schema 锁或承诺任意外部 DDL 原子性。
- 初始授权续办只为本次成功完成且具备 v3 新建提交证明、完整人类来源的首次 bounded 执行初始化 `initial_authorization`；不回填历史成功任务。原 Worker 注册有效时独立领取待办，释放行锁后由统一 System client 核清／续办，正式 pending 才调用 Meta 精确 basic 补扫。30 秒至 5 分钟有界退避，迟到旧领取不能覆盖新状态，明确签发／拒绝终态停止；不重跑写入、不删除成果、不延长 System 冻结期限。通用 metadata／完成路径禁止自报该保留字段。HTTP 只投影 `status/code`，不暴露领取进度、Grant 引用或私有证据；任务详情将物理成功与授权状态分开显示，待办期间只读刷新。读取仍由同一 System Grant 与 Security 链裁决，T1/T2 和前端浏览器门禁不代替新成果完整 T4。
- bounded snapshot 可声明通用“单次执行覆盖既有目标表”能力，且与 source 是直接表、SQL 还是 MQL 正交。任务定义必须保存已存在原生表作为可直接执行的默认目标；默认目标和覆盖目标都只允许 append。TaskProvider 将 `target_locator` 声明为带默认值的可选输入，未提交时使用任务默认目标，Orchestrator 提交时只覆盖本 execution。Worker 通过通用 Engine capability 校验实际目标表结构，严格校验 field mapping 的目标列名称、顺序和类型，再仅用 `TableWriteSessionProvider` append；不得调用 delete、truncate、prepare 或 DDL。使用覆盖的 execution lease 过期后不自动重试。Transfer 不保存 LogicalTable ID，不调用 Model API，不持有 Model Permission。
- 所有 bounded `sync` 任务统一声明稳定输出 `execution_id + target_locator + row_count`，其中 `target_locator` 是本 execution 实际目标。普通固定目标任务使用闭合空输入契约；允许目标覆盖的任务声明可选 `target_locator` 并在 `input_defaults` 返回默认目标。创建子资源时必须保留父 locator 体系，`addp://engine/...` 与 `addp-infra://...` 分别使用各自解析器，Infra 目标不得经过业务 ResourceLocator 解析。下游只能消费稳定输出，不能读取 Transfer 私有配置或拼接物理路径。
- 所有 TaskProvider 稳定输出只写入 `common.task_executions.metadata.outputs`；`GET /task-provider/executions/:execution_id` 使用 `common/taskprovider` 标准状态响应把同一对象投影为顶层 `outputs`。用户执行详情继续使用 Transfer DTO，不复用或包装 TaskProvider 机器契约。
- 工作包 2B/2C 已实现业务 Kafka keyed JSON record -> PostgreSQL/MySQL monotonic upsert；数据库 CDC 已在同一 continuous worker 主循环中实现 PostgreSQL/MySQL/Oracle Debezium envelope -> PostgreSQL/MySQL/Oracle snapshot/upsert/delete。两条路线共用 `ChangeStreamReaderProvider`、partition position、目标 ledger、Infra state CAS、runtime lease/fencing 和 retention 防护；不得新增第二套 CDC consumer。
- continuous resume 前必须验证 committed `next_offset` 仍在 Kafka 保留范围内；低于 earliest offset 时明确失败，不能静默跳到 earliest。PostgreSQL/MySQL/Oracle 目标被锁时必须响应 context 取消并回滚业务写入与 apply ledger；Oracle 通过 `FOR UPDATE NOWAIT` 有界重试检查 runtime context。
- 工作包 4B 已完成：业务 Kafka DLQ 按确定性 record error -> Infra Kafka payload -> `transfer.dead_letters` -> 目标 `skip` ledger -> Infra CAS 运行；公开任务 API 接受显式 `runtime.record_failure.mode=block|dead_letter`，Console 默认显式发送 `block`。唯一 replay API 为 `POST /task-definitions/:id/replay`，只接受显式 partition offset ranges 与不存在的新 PostgreSQL `parent_locator + name`，并通过独立 bounded execution/apply identity 写隔离目标，不能触碰主任务状态、水位或目标。
- 工作包 4D 的 DLQ payload availability 治理由 continuous worker 内唯一 reconciler 承担：按 identity 游标分批核验当前 Infra Kafka exact topic/partition/offset，只有 topic/partition/offset 明确消失或 exact record 身份/schema 不匹配时才以 payload reference CAS 标记 unavailable；网络、认证和 broker 错误保持原状态。reconciler 不解码/记录 payload、不提交 Kafka offset，也不进入 HTTP 请求链路。
- task 直接删除与 System physical cleanup 必须复用同一 task-owned resource cleanup：CDC capture（如适用）→ 业务 Kafka 确定性 DLQ topic → tenant/task scoped `transfer.dead_letters` → task definition。logical cleanup、pause/stop 保留 DLQ；Kafka 删除失败时不得先删索引或任务。
- physical cleanup 在删除私有状态前必须把 continuous task 收敛到 desired stopped 并等待 active lease 释放；running bounded task 无真实 cancel 能力，必须拒绝删除。外部资源清理后，`dead_letters/sync_states/runtime_leases/schema_change_requests/capture_resources` 在同一事务删除，公共 execution 和目标业务事实继续保留。
- continuous worker 从同一 ChangeStreamReader 采集分区 earliest/latest，用 Transfer committed `next_offset` 计算 lag、recovery headroom、source rate 和 retention horizon，并将 `healthy|degraded|critical|unknown` 写入 execution metadata。Monitor 只展示该 metadata，不直连 Kafka 或 Transfer 私表。
- 工作包 3A 已冻结 PostgreSQL 单表 CDC v1 契约，3B 已完成 Infra Kafka/Kafka Connect，3C 已实现 capture control plane，3D 已实现 Debezium adapter、严格 schema/protocol 校验、目标 upsert/delete + ledger 原子提交、公开任务创建和 Console Wizard。
- 工作包 3E 和 Oracle CDC 第一期已完成：`transfer.capture_resources` 是 engine-neutral generation 主事实，一对一 `postgresql_capture_resources` / `mysql_capture_resources` / `oracle_capture_resources` 保存 provider 私有资源；generic 主表不得恢复 provider 字段或使用伪资源占位。数据库 CDC 任务 JSON 只使用唯一 `DatabaseCDCTaskSpec`，provider 必须由 System Engine 解析，不能用配置形态推断；PostgreSQL/MySQL/Oracle 共用唯一 capture supervisor、continuous worker、API、capability 和 Console 路线。
- 数据库 CDC pause 时 connector 继续捕获，主要风险是 Infra Kafka retention；stop 是不可逆终态，必须由服务端显式确认并清理 ADDP-owned connector、provider 专属资源、data/schema-history topic、group 和 ACL，已停止任务不得重新 start/resume。重新同步创建新任务和新目标表，不保留兼容分支。
- 数据库 CDC 数据面固定接受 Debezium Connect 3.6.0.Final schemaless JSON；`r -> snapshot/upsert`、`c|u -> upsert`、`d -> delete`，tombstone、truncate、message、来源身份不匹配和未知字段严格阻塞且不得推进 offset。PostgreSQL、MySQL、Oracle 使用各自严格 source adapter 和类型矩阵，Decimal/Oracle NUMBER 固定 string、时间固定 Connect 毫秒编码，MySQL binary 固定 base64；Oracle Spatial 由 capture Provider 使用 generation-owned 镜像表把 `SDO_GEOMETRY` 转为 base64 WKB，再由 Oracle adapter 转成标准 EWKB。字段、类型或 envelope/source 漂移统一以 `schema_change_blocked` 阻塞。PostgreSQL/MySQL 当前阻塞消息中的新增 nullable 非 geometry 字段可通过专用 API 人工审批；Oracle schema drift 只阻塞，必须 Stop 后创建新任务和新目标表。当前公开能力仍不支持 CDC replay/DLQ、Schema Registry、Avro、Protobuf、Kafka target、普通 Oracle LOB/RAC 或 ArcGIS SDE。
- additive migration 提交后的 Meta deep scan 使用 `schema_change_requests` 中唯一 `pending -> running(token, lease) -> success|failed` claim；并发审批不能重复触发，过期 claim 只由相同重复审批 POST 接管，GET 保持只读，迟到 token 不得覆盖新结果。真实 Meta 失败不自动重试；统一的 `TRANSFER_META_SCAN_CLAIM_TTL` 必须大于 Meta client 60 秒超时。
- 业务 Kafka 注册为 System Engine，topic 使用 `type=topic` ResourceLocator；partition 只属于 runtime assignment/position/diagnostics。Infra Kafka 来自 ADDP 部署配置，不进入 System engines、资源树或用户任务 JSON。
- continuous source 必须消费 common `ChangeStreamReaderProvider`；原始 ChangeRecord 到 ChangeEvent 的解码以及 ChangeApplyWriter 归 Transfer runtime。目标必须消费声明原子、单调及所需 operations 的 `PartitionedTableChangeApplyProvider`。PostgreSQL 把业务行与业务库 `addp_transfer.apply_positions` 原子提交；MySQL 把业务行与同一目标数据库的 `_addp_transfer_apply_positions` InnoDB 私有表原子提交；Oracle 把业务行与连接用户 schema 内的 `_ADDP_TRANSFER_APPLY_POSITIONS` 原子提交。Oracle CDC target 使用 partitioned apply；普通 bounded target 使用 Oracle `TableWriteSessionProvider`，两者复用统一字段/空间映射，但不共享 CDC ledger 语义。当前均拒绝 `time`、decimal precision > 38 和非 XY geometry。普通 `TableUpsertProvider` 不足以阻止失效 worker 写回旧状态。
- continuous session 不进入 Asynq；使用 `transfer.runtime_leases` 做 owner lease、heartbeat 和 fencing，并使用 `transfer.sync_states` 按 partition 保存 `kafka_offset/v1.next_offset`。
- bounded watermark source 必须消费声明 `bounded_watermark_read` 的 common engine Provider；PostgreSQL、MySQL、MySQL 模式 OceanBase、openGauss 与 TiDB 都在源数据库一致性快照内冻结游标上界。单字段游标仅同步新增且必须自身是非空唯一键，复合游标同步新增和更新且 tie breaker 必须是非空唯一键；OceanBase 与 TiDB 限定目录报告为 InnoDB-compatible 的非空间基表，openGauss 当前只开放非空间表。目标只按幂等 `TableUpsertProvider` 能力选择，当前 PostgreSQL、MySQL、MySQL 模式 OceanBase、openGauss 与 TiDB 可作为目标，其中 OceanBase、openGauss 与 TiDB 只支持非空间表；Transfer 不按源或目标引擎类型建立专用执行分支。
- `auto_scan_metadata=true` 的普通业务 Kafka continuous task 在目标结构建立后提交完整目标表 locator 的 `targets`，触发一次精确 Meta deep scan。数据库 CDC connector 必须把 Debezium `Initial Snapshot` 通知写入当前 generation-owned 单分区 data topic；合法通知统一作为 target `skip` 推进，只有严格匹配当前 connector 的 `COMPLETED` offset 在目标 ledger 和 Transfer position 提交后才触发首次扫描，空表同样适用，不使用 `source.snapshot=last` 或时间猜测。首次扫描使用 task 私有持久化 claim 防重复，Meta 提交失败不阻断数据面，失败或过期 claim 由后续 runtime session 重试。普通事件/batch 不扫描，schema 正式变化后另行扫描。
- bounded 写后、continuous 首次扫描和结构变更审批后的扫描只覆盖实际目标表，不枚举所属 schema／database；缺少精确表身份时不得提交全引擎扫描。T1 通过实际 Meta HTTP 请求断言范围，沿现有 Transfer 后端与 CI 自动发现。
- 修改 API 后同步 Swagger：`bash scripts/swagger/gen-swagger.sh transfer` 和 `bash scripts/swagger/check-route-coverage.sh transfer`。

## 前端公开路由

- Transfer 前端遵守 `docs/spec/addp前端路由与可恢复状态规范.md`，模块内公开导航统一通过 `src/utils/moduleNavigation.js`。
- Transfer 独立访问时同样以任务列表为有读取权限账号的唯一侧栏入口，创建由列表主按钮进入；仅有创建权限且具备 Meta 目录读取权限时，侧栏显示创建向导入口。Console iframe 模式仍由 Console 统一显示侧栏。
- 任务和执行身份固定使用 path parameter：`/tasks/:id/edit|detail`、`/executions/:execution_id`；不得把编辑中的具体对象退化为列表 URL 或 iframe 私有状态。
- 列表进入创建、编辑、详情和执行页使用 `push`；保存或取消后离开已失效的表单历史项使用 `replace`。

## 开发与验证

连续执行浏览器验收复用同一 Hosted suite 与 Console canonical 路由，在真实事件已提交后核对时间线、自动刷新的新增计数、恢复来源与失败原因。正式撤权仍由 Python 夹具沿既有 IAM 入口执行；浏览器先确认已展示授权详情，再核对旧内容清空和重新登录后的 Monitor-only 拒绝。私有阶段控制文件与浏览器原始输出不归档；只归档闭合报告和安全页面截图。门禁继续使用 `make test-transfer-continuous-online-runner`，新增 ES module 语法校验，真实部署入口准备 Chromium；登记校验同时检查这些接线。

真实 continuous 事件验收登记为手工 Hosted suite `transfer-continuous-events`，复用独占生命周期、System IAM 测试身份与正式 Gateway／Owner／Worker。业务 Kafka 与 PostgreSQL CDC 均由独立 Business 物理夹具验证目标事实；事件只沿已授权的 Transfer／Monitor 接口读取。新入口的确定性回归归 `make test-transfer-continuous-online-runner` 和 `make test-online-runner`，CI／T0 登记同次完成；实际 Hosted 运行成功前不得计为 T4，通过 API 不代表浏览器已验收。通用诊断投影同时排除连续执行内部 `owner_instance_id`，保留已有安全恢复、分区和延迟事实；共享 Go 消费者及 Common／Monitor PostgreSQL 门禁按 Common 改动扩散。

continuous 的 claim、累计提交计数与终态复用 `common.execution_events`；Transfer 在同一事务持有任务、execution、Runtime lease 锁并校验当前 owner/fencing、执行归属及锁等待后的有效期，Common 不读取 Transfer 私有表。每个 session 新建 execution，实际 claim 写入 attempt 1；已有已开始的 attempt 0 记录由 execution store 标准迁移规范化，pending 取消只保留概览。过期 worker 不得续租或写入，supervisor 收敛过期 running；暂停／停止的过期运行会取消而不自动恢复。事件仅包含闭合状态和累计记录数，不含业务位点、partition、错误自由文本、Worker 或 lease 信息，心跳／诊断不追加。每日预算及 30 天维护沿共享实现，不改变 System 平台日志或操作审计。新增回归由现有 Common execution PostgreSQL 事件组及 `make test-transfer-postgres` 的 continuous 事件、fencing、恢复／结构阻塞组承接，Go T1 自动发现，Common 消费者沿 `make test-changed` 扩散；不新增 API、事件字段或基础设施。

2026-10-12 continuous 过程事件本地门禁通过：`make test-module MODULE=transfer` 完成平台 T0、后端 T1、前端 130 项单元／16 项浏览器及构建、PostgreSQL T2；`make test-go` 全仓 Go 消费者、`make test-common-postgres`、`make test-monitor-postgres` 和执行夹具门禁均通过。真实 PostgreSQL 覆盖 claim／进展／终态、错误码、错配 Tenant／模块／execution、过期及锁等待后的进展／终态／续租拒绝、事务回滚、并发每日预算、暂停过期收敛、恢复的新 execution 和唯一终态、清理保留概览与事件，并清除测试自有事实。`make test-changed` 因未配置隔离 MySQL／OceanBase 测试连接在环境预检退出，Common MySQL／OceanBase 与 Model MySQL 等聚合消费者 T2 未运行，不计为通过；由现有 Release and T2 gates 的 Hosted 门禁复验。真实 Kafka／CDC 认证及 continuous 事件 T4 本轮未运行，既有 T4 收据不覆盖本次事件接入；已同步现有 continuous 集成夹具的事件清理，未以编译通过代替真实数据面验收。

2026-10-12 在提交 `a35e96a9116c8ac95fac7f08ae51773c4ffbaf15` 的 [Hosted T4 38269328262](https://github.com/pampa0629/addp/actions/runs/38269328262) 通过 continuous 过程事件完整链路，共 9 组真实检查：正式 Meta 源扫描、Kafka 目标提交与过程事件、同租户 Owner 权限及跨租户隔离、真实 Worker SIGKILL 后的 lease expiry／新 UUID／位点恢复、暂停恢复与坏记录、Owner 不可用、正式 IAM 撤权后的旧令牌及新 Monitor-only 会话、PostgreSQL CDC snapshot／增删改／确认停止，以及删除任务定义后的授权历史读取。坏记录的 `missing_source_key` 同时在 Transfer 和 Monitor 安全响应中核对；Supervisor 保留既有 RecordDataError 稳定码与安全 Message，不持久化 Cause 或外层包装文本，过程事件不新增错误文本或业务样本。原 Worker PID 50020 由标准生命周期替换为 57334，5 个服务构建身份均匹配测试提交；2 个任务定义清理通过，授权历史在独占部署销毁前保留核验，Hosted 容器／网络／卷零残留。30 个归档文件未含身份、描述及私有运行文件，未检出 JWT、addp_at 令牌和所检查的夹具密码模式。当前提交的干净副本通过 `make test-transfer-continuous-online-runner`（26 项及 System 夹具 Go 测试）和 `make test-transfer-backend`；本地 `make test-transfer-postgres`、`make test-go` 通过，七类记录错误码的持久化和安全投影已由 PostgreSQL 回归验证。本轮 `make test-module MODULE=transfer` 在平台日志生命周期夹具 `test_disabled_metrics_never_stop_foreign_container` 的 10 秒超时处失败，不计为通过；同提交 CI 的全仓 Go 与 Transfer 前端通过；[Platform CI 38269048586](https://github.com/pampa0629/addp/actions/runs/38269048586) 的 Platform consistency 在公共 Hosted 登记回归失败：ES/HDFS 的 `scripts/utils/hosted-online.sh` owner contract 不完整，栅格删除 `verify_online_product_runtime` 片段的负向用例未触发预期拒绝（3 errors、1 failure），未改动其他会话仍在修改的登记文件。Release/T2 的 [Transfer PostgreSQL 15 门禁](https://github.com/pampa0629/addp/actions/runs/38269048728) 已通过；完整自动 workflow 未计为通过。本 T4 不包含真实浏览器验收、180 天概览清理或确定性坏记录的恢复策略变更，System 平台进程日志与审计未改动。

连续执行的分区位点沿 `ChangeStreamPosition.Values` 的既有十进制字符串契约输出 `values.next_offset`，不接受另一套数字表示；专业页面只展示业务运行诊断，不展示 Worker 身份、租约或 fencing。创建、委托创建和任务配置预检约束不因执行响应收敛改变。

2026-10-12 执行响应收敛已由 `c1f993b1fa7c5d5e403e463c371c46b2adf4ae80` 纳入 main；`task_handler.go` 的创建、委托创建及配置预检逻辑没有变化。位点字符串修正补充了 0、普通位点、int64 最大值和真实 PostgreSQL 详情读取回归；后端 `make test-transfer-backend`、`make test-transfer-postgres`、前端 `make test-transfer-frontend`（130 项单元、16 项浏览器及构建）、Swagger 35 个路由覆盖、执行夹具和 ETL runner 37 项均通过。模块聚合初次因 Ontology 来源冻结校验失败：handler 与来源清单引用的旧提交不一致，Agent/Skill 等来源亦有差异。Ontology owner 在共享工作区同步来源后，2026-10-12 本地完整 `make test-module MODULE=transfer`（Platform T0、Go T1、前端 T1/T3、PostgreSQL T2）及 `make test-online-runner` 均退出成功；本模块没有修改或放宽来源门禁。位点补修提交为 `301e421e64a8227ad39cb6705104163cc3e27198`。上述确定性验证不代替本轮专业读取契约的真实 T4。

专业执行读取使用 `transfer.task.read` 与 SQL Owner 范围：仅当前租户的 transfer/sync，有定义历史（含删除定义）与本人一次性执行分别过滤。第一方和普通 OAuth User 可读，机器不进入用户入口；模块导出结果按 Service Client/source 独立授权，TaskProvider 按固定 Orchestrator Client 独立授权。详情、进度、事件、列表总数及统计保持一致。专业响应采用显式 DTO；连续运行时和冻结字段血缘只输出声明的诊断事实，不回显原始 metadata、checkpoint、参数或授权／租约信息。对应 T1 HTTP 隔离／投影回归和 T2 PostgreSQL 回归均由现有 Transfer 门禁承接，新增 T2 必须登记到 `scripts/test/transfer-postgres-gate.sh`；前端继续由 `make test-transfer-frontend` 验证，Swagger 生成及覆盖检查同步执行。

2026-10-12 真实 Hosted T4 [38186991049](https://github.com/pampa0629/addp/actions/runs/38186991049) 在提交 `301e421e64a8227ad39cb6705104163cc3e27198` 通过：8 个服务构建身份一致，Transfer 专业响应的根字段、metadata、冻结结构与字段映射白名单均由真实读取断言核对；三轮 Orchestrator 父子执行、MongoDB 嵌套字段、关系型多源 CTE/JOIN/UNION、500 字段宽表及结构演进历史和浏览器图均通过。11 个临时资源全部删除，suite 残留为 0，基础设施清理零残留；55 个归档文件未发现凭据文件或原始 JWT Bearer。跨用户、跨租户、删除定义历史、撤权及机器入口隔离由上述 HTTP/PostgreSQL 回归验证，不能将本 T4 扩大表述为这些场景均经过真实双用户浏览器验收。该 T4 验证了 continuous 的诊断快照和位点读取；运行当时 continuous 过程事件尚未接入 `common.execution_events`；当时事件写入仅支持 bounded lease，本轮扩展由 Transfer 现有 Runtime lease/fencing 原子裁决 continuous 写入（见本节接入说明），不直接套用 bounded helper。

2026-10-11 真实 Hosted T4 [38139520110](https://github.com/pampa0629/addp/actions/runs/38139520110) 在提交 `25ec6e336574de8d99956eeb6aae290a93297c51` 通过：8 个服务构建身份一致，三轮 Orchestrator 父子执行及结构演进、MongoDB／关系型字段血缘和浏览器图均通过；11 个临时资源全部删除，基础设施清理零残留。此结果验证安全父执行引用适配，不代替后续 Transfer 专业接口权限收敛的验收。

关系型 SQL ETL 的真实 T4 `transfer-relational-sql-etl` 消费 Orchestrator 专业安全 DTO：父执行只核对执行／编排身份、成功步骤和规范的子 execution UUID，不要求 `tenant_id`、配置或子任务 outputs。Transfer／Develop 子执行分别经所属模块读取专业字段事实，并经 Monitor 独立核对 Owner、任务和父子关联；三轮执行及结构演进不能靠父步骤复制结果。确定性专用入口为 `python3 scripts/test/transfer-relational-sql-etl-online_test.py`，同一测试文件已由根 `make test-online-runner`、Platform T0／CI 聚合；专用回归不能替代真实 Hosted T4。

同一 ETL Hosted 套件在三次原生表写入前后，通过 Meta 正式只读接口验证源表这一兄弟项的身份、扫描时间／深度及数据统计不变，schema 到根节点的扫描状态、时间和深度不变；父节点当前子树统计允许随目标创建变化。秒精度读 DTO 的基线先跨过已观察时间刻度再启动任务，避免同秒截断掩盖扩大扫描。结果记录在 `field_lineage.precise_scan_scope`，确定性反例由现有 `make test-transfer-online-runner` 和 CI 自动发现；真实 Hosted 通过前不计为 T4 证据。

2026-10-16，提交 `dcd74b58c` 的 [Hosted T4 38884014630](https://github.com/pampa0629/addp/actions/runs/38884014630) 已通过并下载报告独立核对：`field_lineage.precise_scan_scope` 验证三次原生表写入后，源表这一兄弟项及 schema／root 两级祖先的扫描事实和状态均不变；实际 Worker 日志的 12 次自动扫描全部提交完整表 locator，无父范围 `catalog_paths`。8 个服务构建提交一致，11 个领域资源全部删除，应用清理 passed、Infra zero_residuals；既有浏览器与血缘场景通过。此 T4 证明 bounded 原生表写后范围，continuous 初始化及结构变更补扫仍以现有 HTTP／Go 回归为依据，未扩大表述为已完成真实 CDC 扫描验收。

`make test-transfer-postgres` 使用普通 PostgreSQL 验证 schema、受保护导出和非空间既有表目标覆盖，不依赖 PostGIS。Planner 的通用 PostgreSQL 连接辅助函数只检查连接；PostGIS 前置检查仅属于 GeoJSON 坐标转换等空间测试。目标覆盖回归在自身连接的 `search_path=pg_catalog` 下执行，确保本地 `addp_test` 已安装的空间扩展不会掩盖非空间测试的错误依赖；不删除扩展、不创建额外 database，仍拒绝门禁中的任何 Skip。

```bash
bash scripts/dev/start.sh -transfer
bash scripts/dev/restart.sh -transfer
curl http://localhost:8083/health/ready
```

常用日志：

- `logs/transfer-backend.log`
- `logs/transfer-bounded-worker.log`
- `logs/transfer-continuous-worker.log`

## 相关文档

- `transfer/docs/数据库架构.md`
- `transfer/docs/transfer-基本概念及配置说明.md`
- `transfer/docs/transfer-任务语义与同步模式.md`
- `transfer/docs/design.md`
- `transfer/docs/transfer转换器架构分析.md`
- `transfer/docs/transfer高性能分析.md`
- `transfer/docs/tables/tasks表.md`
- `transfer/docs/tables/task_executions表.md`

## 数据库启动所有权

本模块 schema 迁移仅由 Backend 执行，Worker 只读校验成功提交的 schema 版本。Backend 多实例通过模块级数据库锁协调；开发脚本及 Compose 在所属 Backend 就绪后启动 Worker。结构或初始化迁移变化须递增模块 `SchemaVersion`，共享规则见 `docs/spec/addp开发服务生命周期与构建身份规范.md`。

原生开发的 bounded、continuous Worker 均支持默认单实例、可选双实例，分别配置 `ADDP_DEV_TRANSFER_BOUNDED_WORKER_REPLICAS` 和 `ADDP_DEV_TRANSFER_CONTINUOUS_WORKER_REPLICAS`（仅 1／2）。标准入口按编号单元管理 PID，沿正式登记传递独立进程身份与 `ADDP_HOST_NODE_ID`。持续同步双实例必须省略固定 `TRANSFER_CONTINUOUS_WORKER_INSTANCE_ID`，复用原有每进程租约 Owner 生成。启动、缩容和停止边界见上述生命周期规范；多进程部署不等于已验证业务任务竞争或 HA。

正式只读配置预检为 `POST /api/v1/transfer/task-config-checks`：完整创建请求、三项当前租户用户权限交集（`transfer.task.create`、`meta.catalog.read`、`system.engine.read`），精确委托 Scope `transfer.task.config.check`。只读取 System 引擎事实及 Meta 已落库快照，先授权后用 Tenant Service 读取；不得调用业务行、查询执行／描述、实时 Catalog Facts、Prepare、任务保存或执行。支持边界、摘要及 unknown 语义见 `docs/transfer-基本概念及配置说明.md`。T1 覆盖授权隔离、零任务副作用、完整配置／结构摘要及缺失／采样／依赖错误；已有 `make test-transfer-postgres` 覆盖非法数组在 replace 删除前拒绝。

Transfer bounded 执行失败的诊断沿既有 `error_details.code` 输出：不可证明查询输出血缘的类型化 `plugin.ErrQueryOutputLineageUnresolved`（含逐层 `%w` 包装）在唯一完成事务中保存 `query_output_lineage_unresolved`。执行完成内部接口传递 error，不先转字符串再猜测原因；私有 message 仍仅用于内部诊断，专业响应继续通过 Common Observation 隐藏原始文本。Hosted 派生查询拒绝验收只匹配该稳定 code、failed 终态、本次执行身份、零写入与空目标前后事实，不消费已删除的 error_msg，也不把连接错误或同文案字符串当作保护拒绝。T1 覆盖类型化／包装／无关错误、当前租约下完成与专业响应隐藏文本；现有 Transfer PostgreSQL 读取隔离门禁复验诊断字段及权限，已有门禁登记承接，不新增数据库或测试入口。

上述安全诊断链路已在 `c322167b8` 的 [Hosted T4 38219829612](https://github.com/pampa0629/addp/actions/runs/38219829612) 经真实 MongoDB 血缘拒绝验证，既有正常保护和 Security 不可达期间的查询搬运均通过；不可证明派生查询返回稳定 `query_output_lineage_unresolved` code，零写入且目标前后为空。`make test-transfer-backend`、`make test-transfer-postgres`、Swagger 35 路由覆盖及隔离已提交前端的单元／20 项浏览器／构建验证通过，同提交 Platform CI 33 项通过。共享工作区模块聚合曾因另一轮前端迁移的四个翻译键用例失败，未计为通过；不将工作区未完成的迁移纳入本次交付。

2026-10-15 原生快照配置预检按写入策略核验目标能力：replace 除普通写入外，还要求实例已登记删除能力、当前插件声明删除能力及实现 `ResourceDeleteProvider`；缺少 Meta 快照不能覆盖已知不支持，诊断定位 `/config/target/policy/apply_mode`。append 与源读取不额外要求删除。沿既有 `make test-transfer-backend` 验证新增 11 场景及全部后端用例通过；`make test-transfer-postgres` 的 11 项真实集成、`make test-transfer-frontend` 的 130 项测试／20 项浏览器／构建、Swagger 35 路由覆盖和平台来源审查均通过，Ontology Go T1 通过。完整 `make test-module MODULE=transfer` 在共享 GDAL 生命周期的 `test_failed_sync_removes_record_and_recovers/FAIL_CHECK` 和 `test_launch_owns_listener_and_cleans_up_failure` 两项 20 秒超时处失败（本组 70 项），不得计完整模块通过；未修改该共享测试或阈值。未新增测试入口、依赖或 CI 路线，原 Transfer／Platform CI 自动发现覆盖本修复。用户按标准入口重启 Transfer 后，已通过真实 Console 复验：任务 13 原 append 配置返回 supported；仅在未保存草稿改为 replace 后返回 unsupported，诊断为 `registered_native_access_not_supported`，位置 `/config/target/policy/apply_mode`。取消编辑后未更新或重新执行任务，Manager 数据项 44575 的目标预览仍为三行七字段；本轮预检修复的本机运行复验完成，不计为隔离 T4 或完整模块门禁通过。

Transfer bounded 保护失败在实际边界保留类型化准备／结果标记，唯一完成事务据此写入 `addp.execution-failure/v3` 的 `data_protection_required` 原因；血缘不可证明仍保留原稳定 `query_output_lineage_unresolved` 码，其他保护拒绝使用 `data_protection_required`。专业详情复用共享 ExecutionFailure，与 Monitor 消费同一安全诊断，不显示原始错误正文；Provider 类型化查询错误与 SQLSTATE 优先保留原查询失败性质，连接／普通读取错误不能仅凭相同文案误判为保护拒绝。现有 T1、Transfer 前端与 PostgreSQL 门禁、Security Hosted suite 自动发现承接，不新增入口或依赖。

后台产物扫描归属：人工 start/resume/replay 固化本次 User 的完整主体；编排继承已验证父主体；continuous 自动恢复复制同任务上一轮来源及 retry 关联，不复制授权或 lease。bounded 先按当前 lease 保存实际 outputs 与 writer refs，再提交 `source=transfer` 和父 execution UUID；continuous 初次扫描先在私有 lease fencing 事务内保存 prepared target。Meta 拒绝无来源 Service 扫描，Transfer 保存安全扫描反馈；自有 bounded scheduled 复用 System Task Authorization Subject：带 Cron 定义的创建、更新与恢复从当前 User 同步授权，保存与 apply identity／定义摘要绑定的完整主体；暂停清除本地授权，触发前重核 System 当前资格并在 due claim 事务核对定义及主体，缺失或失效不创建 execution、不推进计划。结构审批在同一应用事务保存审批人的完整来源、实际目标及 revision，再由既有 claim 事务同步 token／期限；Meta 锁定核验 Common 中的已应用事实后创建独立扫描，不读取 Transfer 私表、不借旧执行身份。Manager 导入的创建/启动只在同步请求内转发 User 凭据。已有 Go、PostgreSQL 与 Swagger 门禁覆盖，无新增测试入口或数据库。

continuous 精确扫描范围验收沿用 `transfer-continuous-events` Hosted suite：启用 `auto_scan_metadata`，先正式扫描独立兄弟表并冻结 schema／root 扫描事实，再观察 Kafka 建表首次扫描和 CDC snapshot COMPLETED 首次扫描。目标必须从基线中不存在收敛到 deep 字段事实；恢复、普通事件和再次 resume 不得重复首次扫描，每阶段兄弟项与祖先扫描状态保持不变。两套 Transfer suite 复用 `scripts/utils/online-api.py` 的只读边界检查，API 秒精度时间先跨 tick 再触发。标准回归入口为 `make test-transfer-continuous-online-runner`，共享检查同时由 `make test-transfer-online-runner` 验证；真实 Hosted 成功后另记运行收据，不把 runner 通过当作 T4。结构变更审批补扫沿同一套件新增真实结构变更、审批和恢复场景核验。

continuous Hosted 物理源准备以最终 TCP 服务成功查询 `continuous_fixture` 为就绪条件，不使用会命中 PostgreSQL 镜像临时初始化服务的 Unix socket `pg_isready`。有界等待耗尽必须在建源表及启动业务 Kafka 前失败，Hosted 退出路径继续清除当次资源。临时服务误判和耗尽拒绝由 `make test-transfer-continuous-online-runner` 中既有夹具回归覆盖，无新增入口或数据库。

2026-10-16，提交 `1c40fe0351cb262bf4dae5e306e65b89f74cf517` 的 [Hosted T4 38904000444](https://github.com/pampa0629/addp/actions/runs/38904000444) 已通过，并下载归档独立核对。`precise_scan_scope` 证明 Kafka 建表与 PostgreSQL CDC snapshot COMPLETED 后各完成一次 deep 目标扫描，兄弟项及 schema／root 两级祖先扫描事实不变；实际 Worker 日志恰有两次提交，均为完整目标表 `targets`，恢复、暂停恢复和普通事件未重复扫描。5 个服务构建提交一致，10 组检查、4 个执行 UUID 和 live／recovery／revocation 三组自动浏览器检查通过；2 个任务定义清理 passed，Hosted 容器／网络／卷 zero_residuals。此收据只覆盖 continuous 首次扫描，结构审批补扫归属仍按已确认范围另行处理。

首轮 [Hosted 38902870557](https://github.com/pampa0629/addp/actions/runs/38902870557) 在业务源初始化时误判 PostgreSQL 临时 Unix socket 服务就绪，建表报 `database system is shutting down`，尚未执行扫描场景；退出清理 passed、Infra zero_residuals。修复为最终 TCP 服务查询指定数据库后，`make test-transfer-continuous-online-runner` 的 34 项回归及 System 夹具 Go 测试通过，上述重跑完成真实验收；未放宽就绪条件或扫描范围断言。

结构审批补扫的正式来源为 `schema_change_source`，只引用触发阻塞的 execution、审批 request 和当前 claim。Common 私有 `schema_change_scan_origin` 保存实际应用目标与本次审批 User 的来源，专业诊断不暴露该对象；原阻塞 execution 的主体不变，新扫描无父 execution，并按审批人隔离读取。T1/T2 沿现有 Meta 专业读取与 Transfer 结构阻塞门禁验证；continuous Hosted 套件增加真实 nullable 字段变更、审批补扫、新字段 Meta 事实、幂等审批和 resume 后物理提交与范围不变检查。

Manager 导入的 Infra source 由当前 bounded execution 的完整 User 来源和绑定任务经 Manager Owner 核验后读取，使用 `manager.import_provenance.read`；未知、未绑定、跨租户、非上传者或 Owner 不可用时拒绝。通用 content／format reader 保持唯一实现，业务来源仍走既有 Security Gate，不以 Infra 路径作为许可。

2026-10-16 结构审批补扫 [Hosted 38921609397](https://github.com/pampa0629/addp/actions/runs/38921609397) 在提交 `9c288cb65de32a8899fe28491c124787e791bb08` 通过，5 个服务构建身份与提交一致，11 项检查、5 个不同 Transfer execution 和 3 个浏览器阶段均通过。三次扫描分别为 Kafka 首次 prepare、CDC 快照完成、CDC additive 审批；原 CDC 目标 DataItem 4 从 `id,label` 更新为 `id,label,scan_extra`，补扫 UUID 为 `0adc7cde-44ba-4b33-8c84-a0809236c2e7`，目标仍为 `addp://engine/1/path/public/continuous_cdc_target?type=table`。同级表及两个上级节点不变；真实目标新增列及提交后的完整行集合由独立 Business fixture 验证。重复审批保留 attempt 1 和同一扫描 UUID，resume 新建执行且不再扫描。2 个定义经正式 API 删除，suite 残留 0，Infra 清理 `zero_residuals`。Worker 日志仅有两次首次扫描提交，均为单表 typed targets；第三次审批扫描由审批路径独立提交，不伪造父 execution。

本轮 `make test-go` 的 22 个模块、Common／Meta／Transfer PostgreSQL、Ontology PostgreSQL／独占 FalkorDB、平台来源审查、Meta／Transfer Swagger 生成及路由覆盖、`make test-transfer-continuous-online-runner` 的 36 项回归与 System fixture Go 检查通过；现有标准入口和 CI 自动发现覆盖，无新增 Job 或数据库。不同审批人与原启动人的读取隔离由 HTTP／真实 PostgreSQL 回归覆盖，本次 Hosted 使用同一消费用户，不扩大表述为真实双用户验收。完整 `make test-platform` 两次未通过：首次在 Falkor 夹具 5 秒等待启动处失败，重跑在 dev lifecycle 的快速 Worker 标记等待处失败；同输入的 dev lifecycle 已在首次聚合通过，Falkor 夹具 9 项及真实 PG/Falkor 标准门禁独立通过，但不能计为完整聚合通过，未修改这些夹具或放宽时限。

continuous 双用户审批验收沿用现有 Hosted suite 和同租户 peer：只为该隔离身份增加 `transfer.task.update` 与 `meta.scan_task.read`，不增加执行、引擎管理或管理员权限。由 peer 审批原消费 User 启动的 CDC 结构变更；Meta 补扫须归 peer、无父执行，原消费 User 不能读取该扫描，peer 不能读取原消费 User 的正式源扫描，历史 CDC 发起人保持不变。原消费 User 重复审批不得接管补扫归属，随后由原消费 User 恢复同步，继续验证单表范围、物理提交及零残留。生产权限及 API 不变，沿用 `make test-transfer-continuous-online-runner` 与同一 Hosted workflow，现有 System fixture Go、runner 与 CI 自动发现覆盖，无新增数据库、入口或 Job；真实运行结果另记收据。

2026-10-17 双用户结构审批 [Hosted 38927137086](https://github.com/pampa0629/addp/actions/runs/38927137086) 在提交 `56edd478c1a1d02a455d8432236ef9034251e5f1` 通过，归档已独立核对。发起人 Principal 33 与审批人 Principal 36 属于同 Tenant、不同 Membership，均为普通 User 且持有 `meta.scan_task.read`；33 可读自己的正式源扫描而 36 返回 404，审批补扫 `55805433-e09e-4f04-abc6-80091e8b73a0` 则由 36 读取成功、33 返回 404。补扫发起人为 36、无父 execution，原 CDC 发起人 33 不变；33 再次提交同参审批仍保留 attempt 1、同一补扫 UUID 和 36 的归属。随后由 33 新建 resume execution，真实新增列与提交后的完整行集合通过独立物理检查，恢复后没有额外扫描。

本轮 5 个服务构建身份与提交一致，12 项检查、5 个 Transfer execution、Kafka／CDC 初扫及审批补扫三次单表范围检查通过；兄弟项及 schema／root 两级祖先不变。Worker 日志恰有两次首次扫描提交，均为单个 typed table target。新增双用户归属场景经真实 API 验证，原 live／recovery／revocation 三组自动浏览器检查继续通过；2 个任务定义删除成功，suite 残留 0，Infra `zero_residuals`。干净源码快照中的 `make test-transfer-continuous-online-runner` 37 项回归、System fixture Go 测试和 Online CI 登记检查通过，5 个改动文件与实际测试提交逐一一致。生产权限及 API 未改，现有入口和 Hosted workflow 直接承接本轮用例。


2026-10-17 自有 bounded 定时授权闭环已由 `5593d42ae`、`f277fa894` 接入现有 System Task Authorization Subject；来源冻结为 revision 27（`c05431787`）。带 Cron 定义的保存与恢复绑定本次 User 和完整定义摘要，触发前重核当前 IAM，领取事务再核对定义及主体；暂停清除调度授权，恢复只更新未来计划，不改 active execution 的配置、状态或身份。旧定义缺少主体时拒绝触发，不借创建人或旧执行补齐。任务主体的 User／Service 请求拒绝重定向，失效 User 凭据保持 401；没有新增授权实体、API 路线或测试数据库。System 000216 只支持严格 owner/type 二元组，并向 Transfer Runtime 增授既有主体解析 Permission。

[Hosted 38987715068](https://github.com/pampa0629/addp/actions/runs/38987715068) 在运行时代码提交 `c0543178726054fa4245e566cadff13bf46385e0` 真实通过，独立核验了 5 个服务构建身份、13 项检查、6 个不同 Transfer UUID 与 4 次精确目标扫描。定时 execution `d2a77c47-c63e-4660-83a5-a51ea59a928b` 由真实 Cron 触发，发起人为当前授权 User 33；Meta 扫描 `835fbbf7-acfd-4713-8a2f-68d9446b90ba` 继承同一 User 和这个直接父执行。拥有读取权限的 peer 可读父执行，对该一次性子扫描仍返回 404。实际写入两行，扫描只新增 `addp://engine/1/path/public/continuous_scheduled_target?type=table`（DataItem 5，id／label 两字段）；原 Kafka／CDC 目标、兄弟项和 schema／root 两级祖先扫描事实不变。此前双用户审批、初次扫描与 live／recovery／revocation 三组浏览器验证继续通过，3 个定义删除成功，suite 残留 0、Infra `zero_residuals`。37 个归档文件未检出凭据前缀、Service Secret 赋值或业务行哨兵。

最终 `make test-go` 22 个模块、Common Go T1、Transfer／Meta／Orchestrator Go T1、完整 System IAM PostgreSQL 7 个 package、Transfer／Meta PostgreSQL、Ontology PostgreSQL／独占 FalkorDB、来源审查、授权清单和 System／Transfer／Orchestrator Swagger 覆盖均通过。6 个重定向反例及 active execution 期间恢复调度完成红绿验证；IAM 门禁入口 16 项测试通过。后续 `86b4e726f` 仅修正验收失败清理：定时场景先暂停未来触发再等待当前执行结束，41 项 runner 测试与 System fixture Go 通过，55 个运行时／契约文件与该 Hosted 源码一致。现有标准入口和 CI 登记覆盖，无新增 Job。清洁快照 `1c7301253` 的完整 `make test-module MODULE=transfer` 未通过：平台 T0、后端 T1 和前端 130 项单元通过，但 MongoDB query 恢复配置的两项浏览器断言失败（24 项中 22 项通过）；前端构建及聚合后续 T2 未执行，独立后端 T2 已通过，不计完整模块聚合通过，未修改这些前端实现或测试。个人开发服务本轮未重启；更新 Common 后由用户按标准入口重启全部模块，旧定时定义须通过现有保存／恢复入口重新授权。

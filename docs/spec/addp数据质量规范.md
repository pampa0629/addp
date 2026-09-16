# ADDP 数据质量规范

## 1. 目标与边界

Quality 以“质量检查方案（QualityPlan）”统一字段值检查、结构检查和业务断言。方案显式绑定实际资源并包含多条规则；支持用户手动执行与 Orchestrator 调用。删除原 RuleApplication、CheckTask、DataValidationTask 的独立配置和执行路线。

Quality 不依赖 Catalog。方案中的目标绑定只表达检查作用域，不代表已审核的企业标准映射，不回写 Catalog，不计算落标覆盖率。Catalog 将来可以联邦展示 Quality 公开结果，但不成为配置、执行或服务启动前置。

| Owner | 事实 |
| --- | --- |
| Standard | 数据元值约束、已发布修订及编译规则 |
| Model | 逻辑模型、冻结的标准修订及主键/必填/外键结构声明 |
| Quality | 独立规则及修订、方案检查项、检查目标、来源证据、执行快照、质量结论和问题 |
| System | 引擎发现、租户隔离、身份与 Execution Authorization |
| Orchestrator | 方案执行顺序、调度和失败时阻止后续步骤 |
| Monitor | common.task_executions 的统一监控 |

Standard 和 Model 提供配置依据，不作为 worker 执行期依赖。导入不构成动态订阅；修改上游定义不会自动改写方案或历史执行。Model 的设计声明不能冒充实际数据已满足约束。

## 2. 方案和规则

QualityRule（质量规则）是可复用的独立主资源；QualityPlan（质量检查方案）是唯一正式执行单元。二者通过方案检查项形成多对多关系，同一规则可在同一方案中绑定不同目标。两个主资源均使用独立 ID、租户内唯一且不可变的 code 和正整数并发 version；检查项属于方案聚合，不建第三个管理页面。方案存在 pending/running execution 时禁止编辑和删除。

规则定义保存名称、说明、类型、约束参数及 Standard 来源，不保存实际引擎、表或字段。每次保存生成不可变的 rule_revision，revision_no 与并发 version 是不同语义的字段，不设额外审批流程。方案检查项保存稳定 rule_key、rule_id、revision_no、bindings、severity 和 disabled；bindings 只保存目标表别名与实际字段，不允许覆盖规则约束。修改公共规则只产生新修订，不自动改写方案。页面展示最新修订提示，用户明确升级后按方案版本保存；被任一方案引用的规则禁止删除。

方案保存名称、说明、table_bindings 与 check_items 引用集合。table_bindings 使用 alias + ResourceLocator，不在定义中保存 SQL、连接凭据或 PostgreSQL 专属连接参数。同一方案当前只支持同一 Engine 的已存在表；目标必须在服务端验证，不能仅依赖浏览器选择器。检查项及其修订采用租户一致的关联约束；创建、替换和删除引用集合必须与方案并发版本原子提交。

执行时将检查项及固定修订解析为唯一内部规则文档 addp.quality.plan-rules/v1，保存完整 type、name、source、severity、params 及 rule_id/revision_no 证据。该文档不是方案写入 API。rule_key 是检查项的小写 UUID，编辑绑定或升级修订保持身份，新增检查项生成新身份。severity 为 error/warning/info；失败的 error 规则阻断方案通过，warning/info 保留失败明细和问题但不阻断。规则必须显式列入方案，不在执行时自动吸收其他配置。

规则类型包括 not_null、allowed_values、format、length、value_range、unique_key、foreign_key、predicate_implication、row_count。参数严格按类型解析，拒绝未知字段、非法范围和未绑定的表别名。unique_key 用 columns 数组表达单列或组合键，不把组合键展开成多个单列唯一检查。外键使用等长 columns/reference_columns 数组和 reference_table 别名。

非空独立表达。值范围、格式、长度、枚举和唯一性检查跳过 NULL；外键按 MATCH SIMPLE 跳过含 NULL 的引用键；需要强制完整的键应同时配置 not_null。空表上的逐行规则不发现失败行，不代表数据已经到达；业务要求有数据时显式配置 row_count。

Standard 值约束编译文档继续使用 common/dataquality 的 addp.quality.rules/v1；rule_key 按 UUID v5（OID namespace，名称 addp.standard.element:{element_id}:{rule_type}）生成。该契约不等于 Quality 方案契约；方案中同一标准规则作用于多个字段时必须有各自的规则身份，并保留来源 rule_key。

手工规则由 Quality 维护。Standard 导入必须冻结确定的已发布数据元修订、原始规则身份及参数；用户选择后上游发生变化应报冲突而非悄悄采用新版本。Model 导入必须明确逻辑字段与实际目标的对应，不能依据名称猜测物理表或从 grain_description 猜测主键。来源证据不是执行指令，导入进入同一个方案和执行入口。

## 3. 引擎边界

首期执行后端仅支持 PostgreSQL，但方案、规则身份、问题、快照及 TaskProvider 均属于引擎无关领域层。PostgreSQL 的类型检查、information_schema 读取、标识符引用、参数化 SQL 和一致性事务必须集中在执行适配层。

不得把 SQL 模板、PG schema 路径长度、驱动池或方言枚举暴露为通用规则语义。非 PG 引擎明确返回“不支持”，不能尝试使用 PG SQL、静默跳过规则或回退读取样本。后续引擎扩展需单独确认能力矩阵、规则语义等价性、计数口径、快照保证及下推/扫描方式；本轮不预建通用插件框架或默认所有引擎支持同样规则。

PostgreSQL 标识符必须通过共享 dialect 引用，值使用绑定参数；不开放任意 SQL。一次方案的全部规则在只读、可重复读事务内检查同一快照。不得创建、修改或发布被检查表；方案失败不回滚其他任务已经提交的数据。

## 4. 执行与授权

TaskProvider 只声明 quality_plan，一套列表、详情、执行和状态接口。方案手动执行和编排调用进入相同持久 worker；不启动请求外业务 goroutine，不保留 check/data_validation 的可执行兼容分支。

手动执行通过当前 User Access Token 签发精确 read 授权；Orchestrator 调用必须保存同租户、正在运行的父 execution 的授权血缘，并在 claim 后按当前 attempt/lease_token 派生子授权。不能把 Service Token 当用户授权，也不能复制引擎凭据。

触发时在方案锁内冻结 version、目标、全部规则和 check_timeout_ms 到 common.task_executions.execution_config，schema 为 addp.quality.plan-execution-config/v1。worker 不回读当前规则、Standard、Model 或 Catalog。缺失或非法快照失败关闭，不使用当前配置补齐。

worker 复用 common/execution 的 claim、租约、heartbeat、恢复和终态原语。同一方案最多一个活动 execution；终态、方案最近执行摘要、问题对账必须在同一事务及同一有效租约下提交，过期 worker 不得更新问题。只读执行可在 lease 过期后重试；耗尽次数进入失败。整次执行共用冻结的超时预算，超时取消目标语句，不发布部分合格结论或部分问题对账。

## 5. 结果与问题

结果使用 addp.quality.plan-result/v1，明确保存每条规则的计数、观测值、通过状态和方案 passed。逐行规则 total_count/failed_count 使用相同的记录口径；组合唯一性报告落入重复键组的记录数，不把重复组数冒充坏记录数。row_count 是表级断言，返回实际行数与判断结果，不冒充逐行符合率。

quality_score 表示本次启用规则通过率（通过规则数 / 已检查规则数 × 100），不是坏数据去重比例，也不是字段加权分数。warning/info 失败同样影响规则通过率，但不影响 error 门禁结论。历史执行保留原始契约，不重新计算为新口径。

运行成功且没有阻断失败时 execution=success；error 规则失败时 execution=failed，保留完整结果和稳定的数据未达标错误码；连接、授权、编译或查询异常则使用不同错误码，不生成伪造评分。TaskProvider outputs.passed 为布尔值，供编排声明式消费；Orchestrator 默认阻止 failed 子步骤的后继步骤。

当前问题按 tenant_id + plan_id + rule_key 唯一。失败规则创建或重开问题，之后检查通过自动解决；停用或删除规则不等于数据已修复，不得伪造已解决结论。方案删除清理其当前问题，已完成 execution 保持不变。人工处理仍需说明，并遵守既有 open → resolved/ignored 状态流转。

## 6. API 与页面

规则 CRUD 使用 /api/v1/quality/rules，规则引用方案查询使用 /rules/{id}/plans；数据元候选由 /rules/element-candidates 提供。方案 CRUD 使用 /api/v1/quality/plans，手动执行 POST /plans/{id}/run。前端分为 /quality/rules 与 /quality/plans 两页：前者编辑可复用约束和来源，后者选规则、绑定目标、配置策略并执行。两页版本冲突都保留编辑内容。执行详情使用 /quality/executions/{execution_id}，问题使用 /quality/issues；执行总览复用 Monitor。

规则权限为 quality.rule.read/create/update/delete；方案权限为 quality.plan.read/create/update/delete/execute。方案新增或修改引用要求同时具备 rule.read，运行既有方案不增加规则管理权限。TaskProvider 继续使用 quality.task_provider.read/execute 和固定 addp-orchestrator Service Client Guard。Catalog 的既有只读摘要接口不改造成输入依赖。

## 7. 替换与验证

原任务定义一次性迁为方案，已有执行历史不改写为新契约，仅由 Monitor 展示原始历史，不由新版 Quality 详情解释。运行中旧任务必须先排空，迁移不得静默丢弃 pending/running execution。旧规则应用、检查任务、数据校验任务表及其 API、菜单、权限授予和可执行任务类型在替换后删除，不保留兼容路线；IAM 保留禁用权限行仅用于审计。Orchestrator 自行迁移本模块保存的任务引用，不跨 Schema 读取 Quality 私有定义。

方案内嵌规则逐条迁为独立规则 R1 和检查项，不按名称或约束自动去重；保留原检查项 rule_key、目标、级别和 Standard 来源，原执行快照不改写。删除 plans.rules 持久字段和方案页内约束编辑路线，不保留双写。迁移前排空 Quality 活动执行。

验证至少覆盖严格规则契约、租户隔离、并发版本、规则跨方案复用及同方案多目标、固定修订不漂移、显式升级、引用删除保护、来源冻结、手动与编排授权、真实 PG 的全部规则与空值/空表/组合键口径、过期租约写入拒绝、超时、质量失败阻断、问题闭环、旧任务迁移、前端创建编辑执行、Swagger、权限清单、Console/Monitor 导航及 TaskProvider 唯一入口。

标准入口：make test-module MODULE=quality、make test-quality-postgres、make test-quality-frontend；受影响的共享、权限、编排和前端消费者同时纳入 make test-changed。本地 PG 只使用 addp_test，IAM 只使用 addp_iam_test。

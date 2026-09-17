# ADDP 数据质量规范

## 1. 目标与边界

Quality 以“质量检查方案（QualityPlan）”统一字段值检查、结构检查和业务断言。方案声明表别名及可选默认资源，执行时必须完成实际资源绑定；支持用户手动执行与 Orchestrator 调用。删除原 RuleApplication、CheckTask、DataValidationTask 的独立配置和执行路线。

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

规则定义保存名称、说明、类型、约束参数及 Standard 来源，不保存实际引擎、表或字段。规则内容变化时生成不可变的 rule_revision，仅调整治理归属不生成内容修订；revision_no 与并发 version 是不同语义的字段，不设额外审批流程。方案检查项保存稳定 rule_key、rule_id、revision_no、bindings、severity 和 disabled；bindings 只保存目标表别名与实际字段，不允许覆盖规则约束。修改公共规则只产生新修订，不自动改写方案。页面展示最新修订提示，用户明确升级后按方案版本保存；被任一方案引用的规则禁止删除。

方案保存名称、说明、table_bindings 与 check_items 引用集合。table_bindings 声明唯一 alias 和可选的默认 ResourceLocator；无默认目标的别名必须在执行时绑定。规则字段通过检查项声明，不要求预先选一张样例表。执行参数唯一形状为 `{"table_bindings":{"people":"ResourceLocator"}}`；只接受已声明别名，显式空值非法，未提供的别名使用已保存默认值，仍缺失则拒绝执行。参数不修改方案，也不允许覆盖规则约束、级别或固定修订。一次执行的全部目标仍须属于同一 Engine，不在定义中保存 SQL、凭据或方言连接参数。完整默认目标在保存时校验；所有实际目标在执行时重新校验表、所需字段、类型与授权。检查项引用集合与方案版本原子提交。

手动执行与 Orchestrator 使用同一目标解析路径；TaskProvider 按方案别名声明资源输入、默认值及必填项，复用 Orchestrator 的显式值和上游稳定输出绑定，不接受 DataFrame、日志或未声明结果字段。上游缺失值不能回退默认值。worker 只消费冻结后的完整物理目标，不回读方案。

执行时将检查项及固定修订解析为唯一内部规则文档 addp.quality.plan-rules/v1，保存完整 type、name、source、severity、params 及 rule_id/revision_no 证据。该文档不是方案写入 API。rule_key 是检查项的小写 UUID，编辑绑定或升级修订保持身份，新增检查项生成新身份。severity 为 error/warning/info；失败的 error 规则阻断方案通过，warning/info 保留失败明细和问题但不阻断。规则必须显式列入方案，不在执行时自动吸收其他配置。

规则类型包括 not_null、allowed_values、format、length、value_range、unique_key、foreign_key、predicate_implication、row_count、relational_assertion。参数严格按类型解析，拒绝未知字段、非法范围和未绑定的表别名。unique_key 用 columns 数组表达单列或组合键，不把组合键展开成多个单列唯一检查。外键使用等长 columns/reference_columns 数组和 reference_table 别名。

`relational_assertion`（跨表断言）逐行检查主表与参照集合之间的完整性、一致性和计数对账。规则参数仅保存 `assertion` 表达式，字段使用逻辑符号而非物理列名；检查项通过 `bindings.fields` 映射主表字段，通过 `bindings.relations[角色].{table,fields}` 映射参照表别名及字段。同一参照表可承担多个角色；全部参照资源进入执行目标快照、授权及问题作用域。绑定必须与表达式所需符号完全匹配，拒绝遗漏或多余字段。

表达式仅支持 `field`、文本／布尔／空值 `value`、十进制常量 `number`、`today`、`and/or/not`、`eq/ne/lt/lte/gt/gte`、`is_null/not_null`、`exists/count/count_distinct`。子表达式用 `args`；集合运算指定 `relation` 和可选 `where`，`count_distinct` 另指定逻辑 `field`。字段默认指主表，显式 `relation` 只能引用当前或外层集合角色，不允许未声明的自由引用或角色遮蔽。最多 128 个节点、12 层、8 个参照角色；不接受 SQL、函数名、类型转换或任意运算符。相等为 NULL 安全比较；其他比较的未知结果不算通过；计数忽略 NULL 的去重键，空集合计零。`today` 取执行事务的数据库日期。`number.value` 使用十进制字符串（最多 30 位整数、18 位小数，不使用指数），例如 `{"op":"number","value":"2"}`，避免浏览器及执行快照浮点转换改变约束；`value` 不接受 JSON 数字。根表达式必须为布尔断言，按主表原始记录计数，参照集合重复行不能放大分母。物理字段类型不相容属于编译/执行异常，不产出质量合格结论。

例如规则参数 `{"assertion":{"op":"eq","args":[{"op":"field","field":"actual"},{"op":"count_distinct","relation":"detail","field":"item","where":{"op":"eq","args":[{"op":"field","field":"key"},{"op":"field","relation":"detail","field":"key"}]}}]}}` 表达“当前记录的 actual 等于同 key 明细中 item 去重数”。Quality 前端提供结构化表达式编辑及符号绑定，不增加自由 SQL 路线。PG 适配器使用关联子查询、共享标识符引用与绑定参数；沿用方案只读事务及超时，不自行创建被检查表索引。

非空独立表达。值范围、格式、长度、枚举和唯一性检查跳过 NULL；外键按 MATCH SIMPLE 跳过含 NULL 的引用键；需要强制完整的键应同时配置 not_null。空表上的逐行规则不发现失败行，不代表数据已经到达；业务要求有数据时显式配置 row_count。

Standard 值约束编译文档继续使用 common/dataquality 的 addp.quality.rules/v1；rule_key 按 UUID v5（OID namespace，名称 addp.standard.element:{element_id}:{rule_type}）生成。该契约不等于 Quality 方案契约；方案中同一标准规则作用于多个字段时必须有各自的规则身份，并保留来源 rule_key。

手工规则由 Quality 维护。Standard 导入必须冻结确定的已发布数据元修订、原始规则身份及参数；用户选择后上游发生变化应报冲突而非悄悄采用新版本。Model 导入必须明确逻辑字段与实际目标的对应，不能依据名称猜测物理表或从 grain_description 猜测主键。来源证据不是执行指令，导入进入同一个方案和执行入口。

## 3. 引擎边界

### 业务域治理归属

规则与方案保存可空的 `owner_domain_id`：空值表示租户公共治理，正整数表示由 Standard 中同租户的一个业务域负责。业务域不是访问权限、检查对象或复用边界；不同域的方案可引用同一规则。既有记录保持公共，不根据名称、目标表或标准来源猜测归属。规则治理归属独立于不可变规则内容，调整归属只推进并发版本，不制造约束修订。

规则、方案和问题列表支持精确 `owner_domain_id` 筛选（`0` 表示公共，省略表示全部）。当前问题的归属取当前方案，执行详情取触发时冻结的归属快照；历史没有该快照时明确显示未记录，不用当前方案补齐。删除域不因历史执行快照而阻断，但有当前规则或方案归属时必须拒绝。

Quality 在保存非空归属前通过 Standard 精确校验同租户活动域，并在本模块写事务内锁定该域的引用屏障。Standard 删除域时按固定顺序冻结 Model、Quality 并扫描引用，任一方有引用则恢复所有屏障后恢复资源为 active；任一调用失败时保持删除协调记录，由原有后台补偿继续处理，不跳过不可用模块。本地删除提交后，全部屏障收敛为 deleted 才删除协调记录。屏障冻结与新增归属写入必须串行化，禁止普通 check-then-delete 或跨 Schema 查询。Quality worker 不依赖域服务。

Quality 的屏障写 API 仅供 `addp-standard` Tenant Service Principal 使用 `quality.standard_reference.update`；域校验使用 `addp-quality` 的 `standard.domain.read`，不得扩大用户权限或把域当授权范围。

规则、方案和问题列表将业务域筛选保存在规范路由中，刷新、翻页和关闭编辑器后保留；精确域不隐含子域。域目录失败或无查看权限时明确提示并保留原值，不把异常当公共归属；列表仍允许选择全部或租户公共。规则复用选择不受方案归属域限制。

方案归属调整与其全部当前问题（含已解决、已忽略）的归属更新在同一事务完成，不改问题状态、观察时间或处理审计；存量问题通过新增迁移 `000013` 校准。执行详情只显示快照中的域 ID，显式空值显示租户公共，缺少字段显示未记录，不查询当前方案或用当前业务域名称冒充历史名称。相关回归纳入既有 `make test-module MODULE=quality`、`make test-quality-frontend`、`make test-quality-postgres` 门禁，无新增 CI 入口。

首期执行后端仅支持 PostgreSQL，但方案、规则身份、问题、快照及 TaskProvider 均属于引擎无关领域层。PostgreSQL 的类型检查、information_schema 读取、标识符引用、参数化 SQL 和一致性事务必须集中在执行适配层。

不得把 SQL 模板、PG schema 路径长度、驱动池或方言枚举暴露为通用规则语义。非 PG 引擎明确返回“不支持”，不能尝试使用 PG SQL、静默跳过规则或回退读取样本。后续引擎扩展需单独确认能力矩阵、规则语义等价性、计数口径、快照保证及下推/扫描方式；本轮不预建通用插件框架或默认所有引擎支持同样规则。

PostgreSQL 标识符必须通过共享 dialect 引用，值使用绑定参数；不开放任意 SQL。一次方案的全部规则在只读、可重复读事务内检查同一快照。不得创建、修改或发布被检查表；方案失败不回滚其他任务已经提交的数据。

## 4. 执行与授权

TaskProvider 只声明 quality_plan，一套列表、详情、执行和状态接口。方案手动执行和编排调用进入相同持久 worker；不启动请求外业务 goroutine，不保留 check/data_validation 的可执行兼容分支。

手动执行通过当前 User Access Token 签发精确 read 授权；Orchestrator 调用必须保存同租户、正在运行的父 execution 的授权血缘，并在 claim 后按当前 attempt/lease_token 派生子授权。不能把 Service Token 当用户授权，也不能复制引擎凭据。

触发时在方案锁内冻结 version、完整实际目标、target_key、全部规则和 check_timeout_ms 到 common.task_executions.execution_config，schema 为 addp.quality.plan-execution-config/v2。worker 不回读当前规则、Standard、Model 或 Catalog。缺失或非法快照失败关闭，不使用当前配置补齐。部署前排空旧活动执行；旧历史快照不改写，新 worker 不执行 v1 快照。

worker 复用 common/execution 的 claim、租约、heartbeat、恢复和终态原语。同一方案、同一目标集合最多一个活动 execution，不同目标集合可并发。目标集合身份由排序后的别名与规范 ResourceLocator 物理身份计算 SHA-256，忽略 Meta item_id/node_id 提示；不接受客户端提供身份。终态、最近执行摘要、问题对账必须在同一有效租约事务提交；旧执行完成不能覆盖更新一次执行的摘要。方案有任一活动执行时仍禁止修改与删除。只读执行可在 lease 过期后重试；耗尽次数进入失败。超时与运行异常不发布部分合格结论或对账。

## 5. 结果与问题

结果使用 addp.quality.plan-result/v1，明确保存每条规则的计数、观测值、通过状态和方案 passed。逐行规则 total_count/failed_count 使用相同的记录口径；组合唯一性报告落入重复键组的记录数，不把重复组数冒充坏记录数。row_count 是表级断言，返回实际行数与判断结果，不冒充逐行符合率。

quality_score 表示本次启用规则通过率（通过规则数 / 已检查规则数 × 100），不是坏数据去重比例，也不是字段加权分数。warning/info 失败同样影响规则通过率，但不影响 error 门禁结论。历史执行保留原始契约，不重新计算为新口径。

运行成功且没有阻断失败时 execution=success；error 规则失败时 execution=failed，保留完整结果和稳定的数据未达标错误码；连接、授权、编译或查询异常则使用不同错误码，不生成伪造评分。TaskProvider outputs.passed 为布尔值，供编排声明式消费；Orchestrator 默认阻止 failed 子步骤的后继步骤。

当前问题按 tenant_id + plan_id + target_key + rule_key 唯一。target_key 表示本次完整目标集合（包括外键参照表），地区间不可相互解决问题。失败创建或重开，之后同一作用域通过自动解决；停用或删除规则不代表修复。新增迁移只从最后观测执行中可解析的目标快照回填历史问题作用域；没有证据时保留空身份并显示未记录，不从当前方案伪造历史，不进入新的自动对账。方案删除清理其当前问题，已完成 execution 保持不变。人工处理仍需说明。

问题中的 `column_name` 是主表目标列的展示摘要，组合键和跨表断言可涉及多列，存储使用 TEXT，不受单列名称的 200 字符限制；完整列数组仍以执行结果为准，不截断证据。Quality migration 15 将此列扩展为 TEXT，启动 SchemaVersion 为 3；由 Backend 迁移成功后启动对应 Worker，不改写历史执行。

### 质量概览

Quality 提供 `/overview` 聚合查询与同名页面：当前按方案和目标集合显示最近执行、最近完整质量结论与时间；无执行、运行中、执行异常和质量不通过分别表达。窗口趋势按 UTC 日聚合完整检查的通过规则数/实际检查规则数，不平均各方案百分比，不把异常计零分，不把多规则失败行数相加为坏数据数。业务域按当前方案筛选当前状态，历史趋势按执行归属快照筛选。问题按当前状态统计；历史无作用域执行明确计为未记录，不制造地区归属。概览权限要求 plan.read、issue.read、monitor.execution.read 全部具备。不计算无可靠分母的全平台覆盖率。

## 6. API 与页面

面向用户的稳定 code 分别称为“规则编码”和“方案编码”，不得称为程序“代码”或泛称“任务代码”。数据元导入允许空关键词分页浏览当前已发布候选，也支持名称和编码搜索；翻页与搜索不能静默切换已选来源。所有候选均可选中查看来源，只有存在可导入规则并选定规则时才能确认导入。没有规则时，区分“本修订未配置值约束”和“本修订有值约束但缺少编译规则”；精确修订详情不可读取时只说明没有可导入规则，不猜测原因。空结果与加载失败必须分别展示。

历史迁移产生的已发布修订若存在值约束与编译规则不一致，必须在 Standard 通过新草稿、审核、发布生成新修订修复；不原位改写历史修订，不在 Quality 补算或伪造来源。数据元 length 表示最大长度，不等于定长或特定业务格式；不得在修复中擅自增加非空、格式等业务要求。

规则页明确区分手工定义与标准导入。标准来源按已保存的 element_id + element_revision_id 查询 Standard 的精确修订详情，展示数据元编码、名称、修订和其固定码值集修订、码项编码与含义；不以当前修订替代历史，不按允许值相似度推断来源。来源详情是管理界面的只读投影，不写回规则、不生成新修订、不参与执行；通过当前用户身份调用 Standard，遵守 standard.element.read。无权限或查询失败时保留已保存来源身份并明确提示，不伪装为手工来源，不阻止已有冻结约束执行。

规则 CRUD 使用 /api/v1/quality/rules，规则引用方案查询使用 /rules/{id}/plans；数据元候选由 /rules/element-candidates 提供。方案 CRUD 使用 /api/v1/quality/plans，手动执行 POST /plans/{id}/run。前端分为 /quality/rules 与 /quality/plans 两页：前者编辑可复用约束和来源，后者选规则、绑定目标、配置策略并执行。两页版本冲突都保留编辑内容。执行详情使用 /quality/executions/{execution_id}，问题使用 /quality/issues；执行总览复用 Monitor。

规则权限为 quality.rule.read/create/update/delete；方案权限为 quality.plan.read/create/update/delete/execute。方案新增或修改引用要求同时具备 rule.read，运行既有方案不增加规则管理权限。TaskProvider 继续使用 quality.task_provider.read/execute 和固定 addp-orchestrator Service Client Guard。Catalog 的既有只读摘要接口不改造成输入依赖。

## 7. 替换与验证

原任务定义一次性迁为方案，已有执行历史不改写为新契约，仅由 Monitor 展示原始历史，不由新版 Quality 详情解释。运行中旧任务必须先排空，迁移不得静默丢弃 pending/running execution。旧规则应用、检查任务、数据校验任务表及其 API、菜单、权限授予和可执行任务类型在替换后删除，不保留兼容路线；IAM 保留禁用权限行仅用于审计。Orchestrator 自行迁移本模块保存的任务引用，不跨 Schema 读取 Quality 私有定义。

方案内嵌规则逐条迁为独立规则 R1 和检查项，不按名称或约束自动去重；保留原检查项 rule_key、目标、级别和 Standard 来源，原执行快照不改写。删除 plans.rules 持久字段和方案页内约束编辑路线，不保留双写。迁移前排空 Quality 活动执行。

验证至少覆盖严格规则契约、租户隔离、并发版本、规则跨方案复用及同方案多目标、固定修订不漂移、显式升级、引用删除保护、来源冻结、手动与编排授权、真实 PG 的全部规则与空值/空表/组合键口径、过期租约写入拒绝、超时、质量失败阻断、问题闭环、旧任务迁移、前端创建编辑执行、Swagger、权限清单、Console/Monitor 导航及 TaskProvider 唯一入口。

跨表断言复用现有标准入口：`make test-quality-backend` 覆盖表达式边界、作用域、绑定精确匹配、标识符引用及数值精度；`make test-quality-postgres` 在 `addp_test` 中覆盖存在性、嵌套参照、重复行、NULL、空表和聚合对账，清理自建测试 Schema；`make test-quality-frontend` 覆盖结构化编辑、延迟目标绑定、修订升级及双语词条。测试文件自动纳入现有 Quality T1、`release-and-t2-gates.yml` 的 Quality PG 任务和 `quality-frontend-smoke.yml`，无新增服务、数据库或独立执行入口。

标准入口：make test-module MODULE=quality、make test-quality-postgres、make test-quality-frontend；受影响的共享、权限、编排和前端消费者同时纳入 make test-changed。本地 PG 只使用 addp_test，IAM 只使用 addp_iam_test。

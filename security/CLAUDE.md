Graph 保护消费者（2026-10-17）：固定 `addp-graph` 仅消费 `graph` 投影及确认 cursor，纳入安装／解除屏障；启动事务补齐存量未 released 资源及 releasing 的解除变化。Graph 领域读取复用 Common 显式 graph leaf 门禁，命中纳管拒绝，不提供属性脱敏、Policy 或原值豁免；System 源数据授权本轮不调整。当前消费者集合以 `protection_owner_contract.go` 为准，下文四 Owner 的历史验收不作为 Graph 已通过的证据。验证沿 Graph/Security/System Go、Security PostgreSQL、System migration 和 Common Neo4j/FalkorDB 标准门禁，既有 CI 自动发现。

# Security 模块说明

Security 是 ADDP 数据安全控制面，唯一拥有敏感数据类型、安全分类、安全等级、检测器、发现、资源安全评估、保护基线、策略、原值访问申请、临时原值授权和保护投影。DetectorCapability 是平台代码提供的只读可信能力，Detector 是 Tenant 把能力绑定到 SensitiveDataType 的版本化启用配置；发现不得通过敏感类型代码或名称猜测绑定。当前已经实现基础定义、显式保护纳管、Owner 投影变化流、acknowledgement 激活/释放屏障，以及手机号元数据/文档、邮箱、身份证件号码和银行卡号元数据检测、无原值 Finding、一次性 Finding review、不可变 Assessment/ProtectionPolicy revision、唯一投影编译、显式重新发现和资源级有效期设置。Manager、Develop、Service 已具备自身字段级动作投影，Transfer bounded snapshot 的独立 `export` 动作与 PostgreSQL、MongoDB 原始记录执行器已完成。当前用户从 Manager 或 Develop 复用共享交互发起 ProtectionAccessRequest，显式选择 Manager 预览、Develop 查询或两者；另一名有审批权限的用户一次审批，原子形成各精确出口的 ProtectionExemption。授权仍按用户、按字段、最长 30 天，已有 Manager 授权不扩大。Projection v4 始终保留默认保护决策并携带主体级限时 `allow`，Owner 到期、撤销、Assessment 修订变化或主体不匹配时本地自动回落。用途约束和其他主体类型属于后续阶段。

## 边界

- 不拥有用户、角色、登录认证或资源授权；这些事实属于 System/IAM 和资源 Owner。
- 当前所有 active Security Permission 只允许 `tenant` Scope；管理 API 只消费 Tenant Context，不把 Department 或 Project Group Assignment 当作资源过滤。租户治理使用 `tenant.security_manager`，原值申请使用 `tenant.protected_data_requester` 或等价的 Tenant-only 自定义 Role。未来开放组织级授权前必须先在规范中建立可信的资源归属、Scope Binding、显式 Deny 与 owner 最终策略校验契约。
- 不复制 Meta DataItem、CatalogEntry 或 CatalogComponent，不以 Catalog 建档作为安全事实成立的前置条件；Enrollment 只冻结 Engine ID、item type 与 full_name 的最小保护目标快照用于展示和审计，不保存 attributes 或字段事实。
- 不代理数据预览、查询、导出或服务流量；Owner 使用 `common/dataprotection` 在自身服务端执行保护投影。
- `common/secretcipher` 只负责静态敏感配置值加解密，不是 Security 业务模块的一部分。
- 未纳管资源不进入检测、投影或保护路径，不产生额外远程调用和安全审计负担。
- Manager、Develop、Service、Transfer 的必要 Owner 集合及各自字段级主动作只能在 `internal/service` 的 Owner 契约中定义一次；Enrollment 创建、全量重编译、变化流校验和历史投影升级均从该契约派生，不得重复写 Owner 切片或 action 分支。当前可申请原值访问的出口另行严格限定为 `manager/preview`、`develop/query`，由申请显式选择，不能从 Owner 主动作集合自动扩张。

保护投影自身到期与用户临时原值授权到期分别验证：投影到期必须拒绝相关输出，授权到期仅回落默认保护。Common 查询／原生表的实际输出保护器及 Manager 普通预览响应必须重新校验本次读取绑定投影的有效期，空结果也不能输出过期字段结构。四个 Owner 的指定时点回归由 Go T1 和 `make test-common-postgres` 覆盖，Transfer 真实目标零新增写入由既有 `make test-transfer-postgres` 覆盖；历史协议夹具保留 365 天有效期，沿正式变化流安装，不改变产品有效期或本地系统时钟。同一回归还覆盖更高修订替换到期投影：旧读取继续拒绝，其他存储实例刷新持久 cursor 后的新读取执行新保护决策，重新打开存储后保持修订、纳管状态与保护输出；不增加自动续期路径。这些证据属于 T1/T2，不代替长周期到期 T4。

## 运行角色

- Backend：端口 `8194`，API 前缀 `/api/v1/security`，维护控制面事实。
- Worker：独立运行角色；通过 `common.task_executions` 领取 `security/sensitive_data_discovery` 有界执行，使用 `addp-security` Tenant Service Access Token 精确读取 Meta 技术事实，并且只对已显式纳管的文档按 fingerprint 读取临时受控正文样本；通用租约过期后按 `max_attempts` 重试或失败收口，不运行定时调度或 TaskProvider。
- Frontend：端口 `5191`，通过 Console iframe 集成；产品入口固定收敛为“分类分级体系”“敏感数据定义”“受保护资源”。`/classification-grading` 以可恢复 `tab` 组织低频维护的 SecurityClassification 和 SecurityGrade；`/sensitive-data-definitions` 以 SensitiveDataType 为主视图，把 Detector 作为“识别方式”、把 ProtectionBaseline 作为“默认保护”收进对应敏感类型，不再保留 `/protection-baselines` 产品路由；“受保护资源”统一承载 Assessment 调整、按需读取的不可变修订历史与只能收紧的 ProtectionPolicy 编辑，并把 acknowledgement 表述为保护规则“待同步/已同步”，不使用“待安装/已安装”或“已生效”，也不能表述成某个具体请求或该 Owner 所有数据形态均已执行成功。旧的实体级分类、等级、敏感类型和独立保护基线页面路径不恢复，也不保留兼容入口。

独立前端根路径在 AuthContext 加载后，按 Console 菜单顺序 `/classification-grading`、`/sensitive-data-definitions`、`/protection-enrollments` 选择第一个符合页面准入条件的地址；没有可访问页面时进入 `/forbidden`。登录后的默认返回地址为根路径，显式业务地址仍按自身权限判断。

Manager 原值申请目标的 `projection_acknowledged` 复用现有 Owner 回执，表示当前资源投影已同步；不改变 `approved` 审批结论或当前主体授权语义。申请目标从同一只读一致性快照读取投影、审批、Assessment、授权和回执。页面在已批准但待同步时继续轮询，确认后刷新预览；数据面仍仅消费本地投影。

临时授权列表、详情和撤销响应直接返回由同租户绑定 Assessment 派生的 `component_key`。授权卡片仅消费该字段，不依赖可选 Assessment 列表；持有授权读取权限的审批用户无需取得 Assessment 读取权限即可识别授权字段。组件键只存在于响应 DTO，不新增持久事实或权限。

## 数据库

使用 PostgreSQL `security` schema。当前事实表为：

- `security.security_classifications`
- `security.security_grades`
- `security.sensitive_data_types`
- `security.protection_baselines`
- `security.protection_enrollments`
- `security.protection_projections`
- `security.protection_projection_changes`
- `security.protection_projection_acknowledgements`
- `security.sensitive_findings`
- `security.sensitive_finding_reviews`
- `security.resource_security_assessments`
- `security.resource_security_assessment_revisions`
- `security.protection_policies`
- `security.protection_policy_revisions`
- `security.protection_access_requests`
- `security.protection_exemptions`
- `security.protection_exemption_revisions`

四个必要 Owner 确认 enrolling 门禁后，Security 在同一事务创建一次 discovery execution。治理人员也可携带 Enrollment `version` 显式创建重新发现执行；同一纳管同时至多一个 pending/running execution。完成后保存最新结构或文本快照并重新编译投影，保留资源指定截止时间，不自动延期。结构化 DataItem 目前使用 `addp.detector.phone_metadata/v2`、`addp.detector.email_metadata/v1`、`addp.detector.identity_document_number_metadata/v1` 和 `addp.detector.bank_card_number_metadata/v1` 按字段路径、确定性扁平路径语义和通用类型独立识别；文档使用 `addp.detector.phone_document/v1` 在当次内存样本中识别精确 11 位 ASCII 数字串。身份证件号码元数据能力只识别精确字段语义，不读取或校验业务值；结构化字段通过只读算法目录选择 Unicode 前后缀掩码、同类型固定值替换或普通 SM3 哈希；字段策略在基线显式 `allowed_algorithms` 内独立配置算法与 `parameters`，前后保留数不得超过基线上限。基线参数和许可收紧后，不再满足约束的字段策略回退到当前基线。图层沿用带空间字段的数据表语义，属性字段可脱敏，geometry 不执行标量算法。算法执行复用 `common/dataprotection`；文档正文仍只支持既有手机号片段保护。检测能力都不持久化原始业务值。Finding 必须通过当前 Tenant 的 Detector 绑定取得 SensitiveDataType 和自动采用置信度；达到绑定阈值且存在有效 ProtectionBaseline 后，唯一编译器对结构化组件为 Manager 生成 `preview` 和系统派生的 `profile` 规则，为 Develop 生成独立 `query` 规则，为 Service 生成独立 `service_execute` 规则，并为 Transfer bounded snapshot 生成独立 `export` 规则；对文档虚拟组件 `$document.text` 只生成 Manager `search_index` 规则。没有当前动作执行器的 Owner 出口继续保持资源级 deny。

`released` Enrollment 永久保存退出审计且不可恢复状态。再次保护只能通过旧记录上的重新纳入命令创建新的 `activating` Enrollment，并重新走所有必要 Owner 激活屏障；同一目标不允许同时存在两条未退出生命周期。

成功发现后 Enrollment 查询批量返回当前快照 Finding 总数、待复核数和已复核数；Finding 查询使用 `enrollment_id + source_snapshot_hash` 精确限定当前候选，并在分页响应中携带可选不可变初审记录，以及由当前 Detector、Assessment、ProtectionBaseline 和已发布 Projection 批量组装的只读 `explanation`。当前组件结构仍匹配正式 Assessment 时，`explanation.assessment_id` 在 `sensitive` 与 `not_sensitive` 结论下都指向同一精确聚合，供“受保护资源”继续追加修订；组件结构冲突时不返回该关联，前端不得按字段名猜测。集中“待复核候选”是 `/protection-enrollments?tab=review-queue` 的子视图，只通过 `GET /findings?snapshot_scope=current&review_state=pending` 聚合未退出 Enrollment 当前 Finding，不新增队列实体或第二套复核状态。前端不自行推导保护结论，解释链不持久化且不含原值。历史快照只用于审计。零命中只表示当前检测能力未发现候选，不编译 `allow` 且继续资源级 deny。治理人员确认当前无需保护时，必须使用唯一 Release 路径并提交 `basis=no_supported_findings`、Enrollment `version` 和原因；服务端校验最近发现已完成、Finding 数为 0 且无在途发现执行，然后冻结退出依据、发起人、时间和依据快照。

识别质量摘要直接从 Finding、不可变 review 和 Assessment 当前修订即时聚合，不建立统计表或双写链。当前候选只取各未退出 Enrollment 最新成功发现；历史人工质量样本按 Enrollment、组件和检测能力版本折叠为最新 review。人工指定只作为可能漏检的线索，不归因于某个 Detector。

创建 Enrollment 的唯一用户输入是 Meta 资源树返回的 DataItem ResourceLocator；Security 自行计算 fingerprint，只纳管完整 DataItem。旧的 fingerprint 与字段路径自由输入路线不存在。字段组件通常由 Detector 发现；自动发现漏检时，治理人员只能从 Security Backend 实时读取并校验、且尚未形成任何正式 Assessment 的 Meta 当前字段清单中选择组件，直接形成来源为 `manual` 的正式 Assessment，不得自由填写字段路径。已存在 Assessment 的组件不再出现在人工指定候选中，其后续调整或撤销必须在既有聚合上追加修订。Finding 误报通过不可变 `reject` review 收口，既有正式 Assessment 错误则追加 `not_sensitive` 修订撤销，二者都由唯一投影编译器重新发布保护结果。

ProtectionPolicy 绑定正式 Assessment 和一个精确出口：`manager/preview`、`develop/query`、`service/service_execute` 或 `transfer/export`。字段算法及参数只能在当前 ProtectionBaseline 的算法许可、保留上限和异常值约束内独立收紧，效果为 `mask|suppress|deny`，不承载原值授权。创建、更新和撤销都追加不可变 revision，并在同一事务调用唯一投影编译器，只发布对应 Owner 的变化；撤销后回落到 Assessment + ProtectionBaseline，不解除纳管。

ProtectionAccessRequest 是原值访问的唯一入口，只能由当前可信 User AuthContext 从 Manager 预览或 Develop 查询出口针对正式敏感 Assessment 发起，显式选择一个或两个使用范围，正文不得指定主体；申请本身不改变保护效果。申请状态固定为 `pending|approved|rejected|expired`，由 Security 按服务端时间返回有效状态；超过截止时间的未决策申请不再属于待审批，且不阻止申请人重新提交。唯一审批工作区使用 `scope=pending|history` 分别展示未过期待审批申请与已批准、已驳回、已过期历史，必须服务端分页，并可按申请人、资源或字段、申请时间筛选；历史记录还可按处理结果以及当前 `authorization_state=active|expired|revoked|superseded` 筛选，当前授权筛选必须在分页和总数统计前按当前 Exemption、Assessment revision 与服务端时间完成。批准历史必须另外返回当前授权状态与原授权截止时间，不能把“已批准”误作当前仍可访问原值。审批响应返回权威 `enrollment_id`，前端据此复用既有受保护资源详情和授权撤销路径，不按资源名称猜测、不复制第二套授权管理交互。本人待审申请可见但不可自审。申请人和审批人的 Principal ID 是权威审计身份；Security 只在申请或审批动作发生时通过 System 受信租户用户引用接口校验身份并固化显示名快照，审批读取不实时跨模块查询，服务启动会为旧记录补齐一次快照。另一名具有审批权限的用户在截止时间前批准后，Security 才创建或重新激活绑定 `{assessment, manager, preview, user}` 的 ProtectionExemption 并追加不可变 revision；拒绝不产生授权，申请人与审批人相同或申请已过期均冲突。Assessment 后续产生新修订时旧授权立即失效，不得静默恢复。授权效果固定为限时 `allow`，最长 30 天且不得超过申请期限，业务依据和审批理由必填。Projection v2 的规则始终保留 Policy/Baseline 默认 `decision`，主体授权放在 `authorizations`；Owner 必须从服务端可信 AuthContext 匹配 User，不能接受浏览器提交主体。临时授权只能提前撤销，不能直接创建、续期或重新启用；需要继续访问时必须重新申请和审批。

`manager/profile` 不建立可编辑 Policy：唯一编译器把有效 `preview=mask|suppress` 派生为 `profile=suppress`，把 `preview=deny` 派生为 `profile=deny`。Manager 负责把 `profile=suppress` 执行为整个字段剖析对象的移除，Security 不复制 Manager 指标结构。

创建 SensitiveDataType 必须同时提交其自动发现初始等级的完整默认保护，并在同一事务创建有效 ProtectionBaseline；不允许创建“已定义但无默认保护”的半成品。SensitiveDataType 更换自动发现初始等级前，新组合必须已存在有效 ProtectionBaseline；当前初始等级对应的 ProtectionBaseline 不得停用、改绑或单独删除。其他等级的 ProtectionBaseline 创建、更新、启停、改绑和带 `version` 删除必须根据 Security 自有 Finding/Assessment 依赖精准重编译受影响 Enrollment，并与定义写入保持同一事务；SensitiveDataType 自动发现初始等级变化只重算未复核候选 Finding；Detector 自动采用置信度变化走 Detector 配置变更的有界重新发现路径。正式 Assessment revision 冻结当时的类型、分类和等级，名称或排序等展示变化不制造投影版本。影响解析不得扫描全租户 Enrollment，也不得调用 Meta、Catalog 或 Engine。人工确认或调整 Assessment 时，目标 SensitiveDataType + SecurityGrade 也必须已有有效 ProtectionBaseline；`baseline_missing` 仅作为存量异常或一致性故障的失效关闭观测状态，不是正常配置流程。

Owner 变化流是唯一投影交付路线。Manager、Transfer、Develop、Service、Graph 只能使用各自固定 Tenant Service Access Token 拉取自身变化并确认本地原子安装的 cursor；不能提交 consumer owner 或资源清单。

Enrollment 从 `enrolling` 进入 `active` 必须同时满足四个必要 Owner 的当前投影均为 `active`，且各自 acknowledgement 已覆盖当前 `published_sequence`。旧修订的确认不能激活新修订；重复确认不递增 Enrollment 版本，也不重复创建发现执行。激活与退出都沿同一 acknowledgement 状态推进路径，SQLite T1 和 `make test-security-postgres` 的生命周期回归共同验证该屏障。

Standard 的旧分类分级 ID 和数据不迁移、不映射，也不提供兼容 API。

## 前端验证与失败取证

限时原值访问 `security-plaintext-access` 已在提交 `2e9d37e8b` 的 [2026-10-12 Hosted T4 38191967691](https://github.com/pampa0629/addp/actions/runs/38191967691) 首次通过完整 API 与真实 Console 双用户验收：同一 Tenant 的申请人与审批人使用隔离浏览器 Context，两次批准均在 Owner 同步确认后自动刷新原值预览；真实到期和提前撤销后恢复脱敏，另一主体始终脱敏，三行数据中的 null 保留，审批历史的当前授权状态正确。v3 总报告与 v1 页面报告一致，浏览器告警和业务请求错误为零，三张安全截图已目视核对，Hosted 应用、业务容器、Infra 及凭据清理零残留。同提交 Platform CI 完整通过；套件仍只手工调度，不将此结果扩大为 Security 实际停机故障验收。

同一套件的 v4 扩展在完成原有 API 场景后，通过标准生命周期停止临时 Security、核对拒绝连接，验证未过期本地投影下有效原值授权与其他用户脱敏，以及真实授权到期后两人均回落脱敏。Manager 进程保持不变；所有停止尝试均恢复 Security 并核对构建身份、两用户和到期历史，再继续原有真实页面验收。凭据不传入恢复子进程；900 秒总预算、手工 Hosted 入口和零残留清理不变。提交 `96432fd38` 的 [2026-10-12 Hosted T4 38196037099](https://github.com/pampa0629/addp/actions/runs/38196037099) 已首次真实通过 v4：90 秒授权、八个拒绝连接时间点、两用户有效期内和到期后的输出、Manager 实例保持、Security 恢复身份及到期历史均核验通过。原有完整 API 和 v1 页面验收同时通过，三张安全截图已目视核对，浏览器零告警、零业务请求错误，清理零残留。该场景不扩张为长周期投影到期或高可用验收；Platform CI 结果另行记录。

Transfer bounded 执行失败的诊断沿既有 `error_details.code` 输出：不可证明查询输出血缘的类型化 `plugin.ErrQueryOutputLineageUnresolved`（含逐层 `%w` 包装）在唯一完成事务中保存 `query_output_lineage_unresolved`。执行完成内部接口传递 error，不先转字符串再猜测原因；私有 message 仍仅用于内部诊断，专业响应继续通过 Common Observation 隐藏原始文本。Hosted 派生查询拒绝验收只匹配该稳定 code、failed 终态、本次执行身份、零写入与空目标前后事实，不消费已删除的 error_msg，也不把连接错误或同文案字符串当作保护拒绝。T1 覆盖类型化／包装／无关错误、当前租约下完成与专业响应隐藏文本；现有 Transfer PostgreSQL 读取隔离门禁复验诊断字段及权限，已有门禁登记承接，不新增数据库或测试入口。

四出口 Hosted T4 的 v9 扩展复用唯一 Security stop/start 与拒绝连接探针，增加控制面实际不可达期间的 Develop、Service、Transfer 输出验收。确认四 Owner 当前投影同步后，在临时 Hosted 环境停止 Security；MySQL 验证 Develop 查询和 Service REST Query 仍移除邮箱，Transfer 从空目标表执行 bounded snapshot 后核对五行非敏感字段及执行身份。MongoDB 在 SM3 默认保护下验证 Develop 嵌套字段、计数与派生查询拒绝，以及 Transfer 可证明字段血缘的查询落表与不可证明查询零写入；SM3 目标结果必须区别于上一轮固定值，不能用旧输出冒充本次执行。每个数据面探针前后均确认 Security 拒绝连接，Manager、Develop、Service、Transfer Backend 实例与构建身份保持不变，不调用 Security 治理 API、不改时钟或刷新保护投影。所有停止尝试均在 finally 经标准 start.sh 恢复 Security，并核对新的进程身份、相同构建、普通 User/Tenant/权限不变。原有三算法、重启恢复及 Console 验收保留，900 秒 Hosted 总预算、精确 Source Grant 和清理入口不变；共享生命周期能力只由既有 security-transfer-protection-online.py 拥有。新增确定性回归与登记负例由 make test-online-runner 自动发现，真实结果必须由 security-mysql-owner-protection 手工 Hosted 验证；未执行前不计为 T4 通过。临时原值授权仍仅适用于 manager/preview，本扩展不开放其他出口原值授权，也不宣称投影自身到期或全平台高可用已验收。

本扩展已在提交 `c322167b8` 的 [2026-10-12 Hosted T4 38219829612](https://github.com/pampa0629/addp/actions/runs/38219829612) 首次真实通过 v9：两个 Security 停机窗口、14 个前后拒绝连接时间点、四 Owner Backend 实例和构建身份保持、Security 新实例恢复及普通 User/Tenant 核验均通过。MySQL 五行的 Develop 查询、Service REST Query 与从空表开始的 Transfer bounded snapshot 均移除邮箱，目标客户标识保持；MongoDB 七行在 SM3 下完成 Develop 嵌套字段、空值与相邻属性校验，计数仍为七、派生查询不返回结果，Transfer 可证明查询落表与不可证明查询零条／零字节写入均通过，拒绝使用安全诊断 code 而非原始错误文本。MongoDB 十个任务及执行逐一核对、全部删除，Baseline 恢复、临时 Policy 撤销；既有三算法、重启恢复与 Console 检索／定位／预览／CSV／Monitor 链路同时通过，浏览器零告警及零业务请求错误，四张截图已目视核对。独立 v9 报告核对与 Hosted 应用、业务容器、Infra、凭据清理均通过，零残留；[同提交 Platform CI 38219801688](https://github.com/pampa0629/addp/actions/runs/38219801688) 全部 33 项通过。此证据只覆盖上述有效投影下的实际出口，不扩张为其他出口原值授权、投影自身到期或全平台高可用验收。

四出口 Hosted v10 进一步验证显式重新发现后的投影同步与实际输出：普通用户沿既有纳管 version 命令创建 discovery execution；四 Owner 的稳定投影 ID、递增 revision、新 published cursor 和本次发现完成事实须同时成立；时间按 API 规范接受带时区 ISO 8601 值，统一换算为 UTC 比较，拒绝无时区值及过旧回执。当前回执齐备后重新读取四个出口并核对 Transfer 新执行身份与实际目标。有效期延长及完整 365 天区间由 Security 编译器 T1 单独验证；不扩展治理响应、产品角色或自动续期，不将此证据表述为自然到期 T4；该 Hosted 套件的临时普通账号需具备既有 `security.enrollment.update` 命令资格，其他套件与源数据 Grant 不变。该扩展仍走现有 Hosted suite 和资源清理。

提交 `fabac4bcb8` 的 [2026-10-13 Hosted T4 38335798164](https://github.com/pampa0629/addp/actions/runs/38335798164) 已真实通过 v10，并完成下载报告的独立核对：正式重新发现保持源结构和四个投影 ID 不变，四 Owner 均安装更高 revision 与新 cursor，当前回执覆盖本次发现完成时间；Manager、Develop、Service 及 Transfer 新执行均输出五行，邮箱字段移除、客户标识保持，Transfer 实际读写均为五行且目标预览一致。既有两个 Security 停机窗口、14 个拒绝连接时间点、MongoDB 三算法及十个任务清理同时通过；Console 检索、定位、预览、CSV 和 Monitor 页面报告通过，零浏览器告警、零业务请求错误，四张截图已目视核对。领域资源、Hosted 应用、业务容器、Infra 和凭据清理均通过，零残留。该结果不覆盖后续提交 `b8ce75038` 新增的治理有效期展示、资源筛选和银行卡号识别，也不计为自然到期或全平台高可用验收。

Hosted T4 `security-mysql-owner-protection` 使用独立的准备身份，通过 System 正式 `access_grants` 命令原子初始化独立批准要求并给普通消费账号签发精确表的只读 Grant。范围仅包括 MySQL `customers`、`bank_card_accounts`、MongoDB `Outdoor.Persons` 集合，以及 PostgreSQL `spatial_algorithm_source`、`spatial_algorithm_transfer`、`mysql_email_transfer`、`mongodb_query_transfer`、`mongodb_query_control`、`mongodb_query_denied`、`bank_card_transfer` 七张表，共十个精确读取 Grant；不授予目录、未来表或消费者的授权管理权限。准备令牌在进入四 Owner 场景前移除，普通账号继续同时受功能权限、源数据授权和 Security 规则约束；整套夹具随 Hosted Job 清理。夹具身份、精确授权请求和准备令牌隔离回归由既有 `make test-online-runner` 自动发现，真实链路由 `online-t4-gates.yml` 的同名套件验证。

MongoDB 查询搬运验收复用上述 suite、普通 User 和标准 `bounded + snapshot` 链路：未纳管时先验证派生查询能落表，纳管后三算法分别验证透明 aggregate 的嵌套字段别名、`$ifNull`、SQL NULL 和相邻字段；不可证明输出血缘的派生查询必须在保护准备阶段拒绝，写入条数、字节数为零且独立拒绝目标前后为空。Manager 独立预览 Policy 不改变 Transfer 默认 Baseline。提交 `41bd12252` 的 [Hosted T4 38133692022](https://github.com/pampa0629/addp/actions/runs/38133692022) 已真实通过 v8：8 个任务及执行逐一核对、任务全部删除并确认 404，Baseline／Policy 恢复及 Hosted 清理零残留；不把该 suite 结果扩大为全仓 CI 通过。

空间标量算法验收按当前 Owner 输出协议解析 Manager 表格预览的 WKT，以及其他 Owner 查询输出的 GeoJSON 或原生十六进制 EWKB，并逐行比较二维 Point 坐标；非法类型、维度及改变后的坐标均失败。源表与结果表的 geometry 类型、主几何列及 SRID 4326 另由 Meta 空间事实核验；空间解析错误须标明 Owner 和行 ID，不能被算法校验或清理日志掩盖。

重新纳入保护的 PostgreSQL 生命周期回归使用带纳秒尾数的固定时间夹具，先校验落库后的微秒精度，再以操作前读回的审计时间作为精确比较基线；不得把数据库读回值直接与 `time.Now()` 的纳秒输入比较，也不能通过放宽时间误差来掩盖退出审计被修改。该回归继续由 `make test-security-postgres` 和现有 `security-postgres` CI Job 执行。

`make test-security-postgres` 除迁移与服务事务外，还运行默认保护 HTTP 契约集成回归：使用真实 Handler、Service 和 PostgreSQL，验证敏感定义与初始规则原子创建、非法规则无残留、完整写响应与递增版本、更新和删除冲突的稳定错误码、显式重读后再次写入、初始规则删除限制及租户隔离。测试仅在标准测试库事务中重建 `security` schema，结束回滚；身份上下文是可信测试夹具，不代表真实登录、权限中间件或 Gateway 已完成联调。CI 继续由 `release-and-t2-gates.yml` 的 `security-postgres` Job 调用同一入口；真实跨模块验收仍归独立的 Online T4。

表单弹窗只在开始新会话（`open`）或用户显式加载最新基线后清除旧校验；`opened` 和焦点恢复只负责聚焦，不得清除动画期间已经产生的必填提示。浏览器回归通过延长打开动画、在动画结束前提交空表单来验证提示持续可见。

候选复核的会话边界由 `useProtectionFindingReview.prepare` 统一拥有：首次打开、重新打开和成功后连续复核下一个候选都在准备新候选时清理旧校验；关闭或销毁后忽略待执行的界面初始化。`FindingReviewForm` 的 `clearValidate` 与 `focusRationale` 分离，挂载和弹窗 `opened` 只聚焦。不得让聚焦方法隐含表单重置，也不得通过延迟用户提交或增加测试重试规避动画竞态。现有 `make test-security-frontend` 与 Platform CI 同时覆盖动画前空提交、动画后提示保留、重开清理和连续复核会话，不新增测试入口或环境依赖。

标准入口为 `make test-security-frontend`，依次运行确定性测试、Playwright 浏览器回归和构建。浏览器测试保持零重试，失败时保留截图与 `trace.zip`，成功时不保留轨迹。结果存放于系统临时目录（CI 设置 `TMPDIR=RUNNER_TEMP`）下当轮独立的 `addp-security-playwright-results-*`；成功退出时删除当轮目录，重跑不覆盖此前失败产物。

默认保护编辑由独立浏览器用例覆盖识别方式到默认保护的抽屉切换、必填空值提交，以及取消按钮、Esc、遮罩点击三种退出路径。退出后必须恢复原值、清除旧校验并保留父抽屉；Esc 和遮罩关闭还验证焦点回到编辑按钮。鼠标遮罩关闭不能依赖 Element Plus 默认恢复焦点，默认保护弹窗在 `closed` 后显式聚焦本次触发按钮，不改写组件库的焦点陷阱。需要单独诊断时，在 `security/frontend` 运行 `npm run test:e2e -- --grep 'default protection editor'`；这些用例仍由标准前端门禁自动运行。

`platform-ci.yml` 的 Security 前端门禁失败后上传 `security-browser-failure-<run_id>-<run_attempt>`，保留 14 天。下载并解压产物后，可在 `security/frontend` 运行 `npx playwright show-trace <trace.zip绝对路径>`，检查操作时序、DOM 快照、网络与浏览器控制台；一次重跑通过不能证明偶发问题已经修复。

默认保护提交从校验开始到保存及列表刷新结束使用同一 `saving` 状态：禁止重复提交、修改表单，以及取消、关闭按钮、Esc 和遮罩退出，避免旧响应关闭新编辑会话。保存失败时解除限制并保留输入供重试；成功刷新后再关闭。延迟响应浏览器用例同时覆盖失败重试、保存成功和刷新尚未完成的阶段。

默认保护保存失败提示只读取 API 标准错误体中的非空字符串 `error`，以普通文本展示；不展示诊断 `detail` 或 Axios 通用 HTTP 错误。断网、缺少原因、空白或非字符串原因使用现有 `security.common.failed` 本地化词条。浏览器回归同时验证服务端原因和这些异常响应，保持输入可编辑且不自动重试。

默认保护写入结果与列表刷新结果分开处理：创建、更新成功后先采用响应中的完整规则和新 `version`；删除成功后移除对应本地规则。刷新失败显示持久的“变更已保存”提示并提供只读重试，不误报保存失败、不重复写入，也不允许基于未刷新列表继续编辑。初次加载失败不能显示成空列表。版本冲突按 `resource_version_conflict` 判断，保留草稿并阻止旧版本再次提交；只有用户显式选择放弃输入并加载最新规则后，才替换编辑基线和恢复提交。加载失败保留原草稿，组件卸载后忽略迟到响应。

默认保护工作区向敏感类型父页面发布按类型限定的最新规则快照，用于更新默认保护摘要；父页面不得因此再次全量读取定义。写入响应、显式冲突重载和列表读取成功均更新这份展示快照，不另存业务保护事实。

## 必读规范

- `docs/concepts/addp数据安全与隐私保护体系图.md`
- `docs/spec/addp数据安全与隐私保护实现规范.md`
- `docs/spec/addp-API设计规范.md`
- `docs/spec/addp-Swagger集成指南.md`

普通账号资源组合筛选 T4 复用现有 Console 登录与 `security-mysql-owner-protection` 浏览器流程：使用同一普通 User 的正式 AuthContext，逐项核对名称、引擎、生命周期状态及列表范围的请求参数、返回成员、分页总数和实际表格。覆盖正确组合、错误引擎、保护中与已停止保护状态的空交集、百分号字面匹配、范围切换及重置恢复；当前四出口回执、有效期和页面截止时间同时核对。筛选阶段仅允许 Security GET，不创建或修改纳管、策略、授权和角色。跨模块整页导航后，验收 API 必须从本次浏览器成功业务响应的 Authorization 绑定当前会话，并重新核对同一 User、Tenant 和完整权限集合；禁止继续使用导航前捕获的旧令牌，也不回退准备身份。新增三张筛选截图，浏览器报告升级为 v4、场景报告升级为 v12，严格校验新增字段；既有客户检索、预览、CSV、Monitor、银行卡和停机回归保持完整；沿现有前端、Online runner 及 Hosted 入口发现，1200 秒总预算不变。普通账号组合筛选 T4 结论须以完整报告及截图的独立核对为依据，真实结果见下文记录。

## 数据库启动所有权

本模块 schema 迁移仅由 Backend 执行，Worker 只读校验成功提交的 schema 版本。Backend 多实例通过模块级数据库锁协调；开发脚本及 Compose 在所属 Backend 就绪后启动 Worker。结构或初始化迁移变化须递增模块 `SchemaVersion`，共享规则见 `docs/spec/addp开发服务生命周期与构建身份规范.md`。

受保护资源列表支持资源 full_name 字面子串、引擎和既有生命周期状态的服务端组合筛选，计数与分页采用相同条件。Owner 同步摘要另从既有投影派生有效期和当前有效状态；界面区分规则同步与时间有效性，过期仍保持纳管；管理员须显式修改保护有效期才能恢复时间有效性，重新发现不延期，不新增自动续期或调度。银行卡号元数据能力 `addp.detector.bank_card_number_metadata/v1` 只对表/集合字符串字段的明确银行卡号名称进行精确识别，复用现有末级/扁平路径语义和发现、复核、四 Owner 编译；不读取业务行或推断普通账号/卡 ID。

提交 `b8ce75038` 的上述扩展已通过 `make test-module MODULE=security`：Go T1、212 个前端确定性用例、25 个浏览器用例、构建及 Security PostgreSQL T2 均通过；Swagger 生成和 62 条路由覆盖核验通过。[同提交 Platform CI 38338630796](https://github.com/pampa0629/addp/actions/runs/38338630796) 已成功。2026-10-13 重启后，本地 Security 就绪且构建包含该提交；无痕 Chrome 的 outdoor 开发者账号核对 Persons 名称有匹配／无匹配／重置恢复，以及四出口截止时间、独立时间有效状态和详情起止时间。Chrome 原生自动化通道不稳定的剩余控件由内置浏览器的 ADDP 开发管理员账号只读补验：MongoDB 与同步完成的组合找到 Persons，保护中与已停止保护的交集为空，重置后恢复；银行卡号能力的版本、字符串类型、精确别名、隐私边界和号码有效性／卡种／校验位局限均在真实界面可见。查看后取消表单，现有类型仍保持一项识别方式，未保存新增绑定或改变保护、授权。此记录不计为普通账号全部组合筛选、真实银行卡字段的发现与数据出口 T4，亦未在本地改时钟制造到期场景。

银行卡号真实输出验收扩展复用 `security-mysql-owner-protection` Hosted 套件，报告升级为 v11。专用 MySQL `bank_card_accounts` 和 PostgreSQL `bank_card_transfer` 随当次夹具创建与销毁，普通用户仅增加这两张表的精确读取 Grant；准备身份沿正式 API 创建银行卡类型、默认前后缀掩码和元数据检测绑定。真实 Meta 扫描后，正式发现及复核须形成唯一 `bank_card_number` 字符串 Finding/Assessment；相似名称和非字符串 `credit_card_number` 不得产生银行卡候选。四 Owner 的当前投影须确认，再逐行核对五条掩码输出、null、空串、Unicode、短值及全部相邻反例字段；Transfer 从空目标开始，执行 UUID、普通发起人、五条读写与实际目标预览必须一致。新任务和服务必须删除并核对不存在，失败仍走既有 Hosted 清理。扩展后仅此套件的总场景预算为 1200 秒，单请求仍最多 30 秒，检索收敛等待和全部保护断言保持；`security-plaintext-access` 继续使用 900 秒默认总预算。确定性反例、夹具、生命周期及 CI 登记沿 `make test-online-runner` 自动发现；不新增工作流或本地生命周期操作，真实 T4 结论以完整 Hosted 报告及下述独立核对记录为准。

提交 `333cc6ae5` 的 [Hosted T4 38425834669](https://github.com/pampa0629/addp/actions/runs/38425834669) 因旧 900 秒总预算耗尽失败：流程已走过银行卡场景及后续 MongoDB 场景，最终邮箱字段检索仍在等待索引隔离解除时被外层截断，未生成完整 v11 场景报告，不计任何新增 T4 通过。历史 v10 总耗时约 767 秒，新增银行卡发现、四 Owner 回执和实际落表进一步占用串行预算；因此同步调整上述扩展套件的总预算，不能靠跳过检索、降低断言或忽略超时认定成功。本次报告确认 Hosted 清理通过、Infra 零残留。

提交 `ea1379586` 的 [2026-10-14 Hosted T4 38433500090](https://github.com/pampa0629/addp/actions/runs/38433500090) 已真实通过 v11，场景耗时约 848 秒，并完成下载报告的独立核对：纳管前原值对照通过，银行卡检测仅命中字符串 `bank_card_number`，Finding 与正式 Assessment 绑定同一银行卡敏感类型；四 Owner 均确认当前掩码投影。Manager、Develop、Service 和 Transfer 各核对五行前后缀掩码、Unicode、短值、null、空串抑制及全部相邻反例字段；Transfer 从空目标开始，同一普通用户发起新执行，实际五条读写及目标重扫预览一致。银行卡临时任务和服务均删除并确认不存在。既有两个 Security 停机窗口、14 个拒绝连接时间点、MongoDB 三算法及十个任务清理、四 Owner 显式重新发现同时通过；Console 邮箱字段检索、受保护客户预览、五行 CSV 和 Monitor 报告通过，零浏览器告警、零业务请求错误，四张截图已目视核对。银行卡输出证据来自真实 API 和 Worker，上述浏览器截图属于既有客户数据回归，不扩大为银行卡界面或普通账号全部组合筛选验收。领域资源、Hosted 应用、业务容器、Infra 与凭据清理通过，零残留。提交前的 `make test-online-runner` 已完整通过；总预算回归拒绝旧银行卡 900 秒配置，并确认原值访问套件仍为 900 秒。该结果不计为保护投影长周期自然到期或全平台高可用验收。

提交 `dc8d4520f` 的 [Hosted T4 38471195140](https://github.com/pampa0629/addp/actions/runs/38471195140) 在新增浏览器阶段失败：整页导航进入 Security 后继续使用导航前的浏览器 API 客户端，Meta 引擎列表返回 `401 authentication_required`。该结果不能计为筛选或完整 v12 通过；报告确认 Hosted 清理通过、Infra 零残留。修复沿既有浏览器请求身份边界绑定当前会话；确定性回归模拟令牌轮换，证明旧凭据被拒绝、新凭据可用，并拒绝另一用户、租户、上下文、主体类型、权限集合以及缺失凭据，不改变产品认证、权限和保护规则。

提交 `7a6476b3d` 的 [2026-10-14 Hosted T4 38477006785](https://github.com/pampa0629/addp/actions/runs/38477006785) 已真实通过 v12，场景耗时约 884 秒，并完成下载报告与七张截图的独立核对。普通 User 的九个资源筛选场景全部通过：名称、正确／错误引擎、同步完成／已停止保护状态、保护中／全部范围的交集、百分号字面匹配及重置恢复均核对实际请求、成员、分页总数和表格；四 Owner 当前回执、时间有效状态及截止时间与响应一致。筛选阶段只读，未修改纳管、策略、授权或角色；进入 Security 及返回 Manager 后，验收 API 均绑定本次浏览器成功业务响应并确认同一用户、租户和完整权限集合。既有银行卡四 Owner 五行输出、MongoDB 三算法及十个任务清理、两个 Security 停机窗口与 14 个拒绝连接时间点、四 Owner 显式重新发现继续通过。Console 客户检索、预览、五行 CSV 与 Monitor 通过，零浏览器告警、零业务请求错误；新增三张筛选截图和既有四张客户数据截图已目视核对。领域资源、Hosted 应用、业务容器、Infra 与凭据清理通过，零残留。[同提交 Platform CI 38476933899](https://github.com/pampa0629/addp/actions/runs/38476933899) 全部成功；本地 `make test-online-runner`、`make test-console-frontend` 和 `make test-security-frontend` 均通过，其中 Console 为 173 个确定性用例、134 个浏览器用例及构建，Security 为 212 个确定性用例、26 个浏览器用例及构建。该结果不计为保护投影长周期自然到期或全平台高可用验收。

资源保护有效期改为 Enrollment 级配置：新纳管默认长期有效，指定截止时间必须显式选择。已有期限迁移保留，重新发现不延期，修改期限沿既有权限、version 和唯一投影编译器同步四 Owner；Projection 唯一运行协议为 v4，expires_at=null 表示长期。期限到期仍保持纳管并拒绝输出，原值授权继续限时。此次变更的验收结论须由本次门禁另行记录，历史 v12 结果不能替代。

2026-10-15，提交 `061e8420f` 的 [Hosted T4 38704771989](https://github.com/pampa0629/addp/actions/runs/38704771989) 未通过：在初始化精确源读取 Grant 前，System 的 `manager-preview` 读取检查返回 `503 authorization_facts_unavailable`，尚未进入 v13 有效期或浏览器场景。该隔离环境未配置独立 `SYSTEM_SERVICE_CLIENT_SECRET`，源授权准备请求还需适配现行 DataItem 指纹与授权边界契约；本次结果不能计为自然到期或长期有效 T4 通过。Hosted 应用、夹具与 Infra 清理通过，零残留。

既有资源期限迁移的同一回归由 SQLite 与 `make test-security-postgres` 共同执行：四 Owner 保留各自原截止时间、修订和 feed 位置，资源回填其中最早截止时间，重复迁移不延期且不更改资源版本。迁移仅显式读取 Enrollment 身份列，避免 PostgreSQL 在同一连接内变更表结构后复用 `SELECT *` 查询计划导致结果类型变化。

2026-10-15，本地按用户指定将 `Outdoor.Persons` 的 2,188 行四列复制到 Business MySQL 的独立表 `business.security_persons_acceptance_20261015`，逐行核对复制一致，原 MongoDB 与既有表保持不变。以已登录的 outdoor开发者完成单表深扫、phone 发现与正式复核，四 Owner 均确认默认掩码规则。Manager 实际预览和全量 CSV 导出通过：863 行掩码、1,325 行空串或短值抑制，全部 ID 与相邻字段保持一致，非空手机号原样输出为零。有效期从默认长期切为指定时间并读回、恢复长期的配置往返通过；另以本地 22:27 自然到期验证 Manager 新请求返回 403，提示为数据保护阻止预览，显式恢复长期并取得四 Owner 回执后，新请求返回 200。测试表最终为长期有效，没有修改账号源读取授权或临时原值授权。该证据覆盖本地 Manager 预览／全量导出及四 Owner 期限同步，不代替 Develop、Service、Transfer 各自独立新执行或 Hosted v13 全套验收。Manager 保护拒绝与读取权限提示的区分已通过 `make test-manager-frontend`：324 个确定性用例、182 个浏览器用例及构建。

2026-10-15 至 16 日，继续以同一 outdoor开发者和上述 MySQL 验收表完成本地 Develop／Service 实际输出核对，运行构建为 `5836f27c0`。Develop 即时查询 `5110fe55-24cd-4bf6-b172-657f17889d51` 返回 20 行默认保护结果；别名查询 `3804ef90-1e6d-4a17-9c56-6adff29023c6` 将 phone 改名为 contact_phone，仍保持掩码。表达式查询 `2f261437-6978-4790-bdbd-604d594c4c9c` 因 MySQL Provider 无法解析受支持的读取范围，被保护门禁拒绝，终态失败、输出 0 行；前端仅显示通用执行失败，尚未准确解释保护拒绝原因，该提示缺口随后由 `9a07ad7d7` 修复，已完成本地真实查询及下述 Hosted v14 验证。Service 通过正式表模式创建同名私有测试服务（ID 6，匿名访问关闭），真实 REST Query 端点前两页各返回 20 行；第二页逐格核对 11 行掩码、9 行空值或短值抑制。验收后已停用该测试服务，保留定义便于复验；MySQL 表与长期保护仍保留。本轮未改产品代码、账号授权或原值授权；证据属于本地已运行版本的正常保护输出，不计为 Security 不可达、这两个出口自然到期、Transfer 独立执行或 Hosted v13 通过。

Hosted Security 隔离生命周期须在首次启动 System 前生成独立的临时 `SYSTEM_SERVICE_CLIENT_SECRET`，供 System 通过既有 `addp-system` 身份读取 Meta 授权边界事实；不复用消费 User 或其他模块凭据，不写入验收产物。既有退出流程销毁该隔离部署及仓库外凭据。此准备项由 `scripts/test/online-hosted-security-gate_test.py` 覆盖，在现有 `make test-online-runner` 与 Online CI 中自动发现；凭据准备本身不代表正式 Grant 或完整 Hosted 套件通过。

Hosted 脱敏夹具仅为 PostgreSQL 的七张结果／样例表准备精确源读取 Grant：经正式 Meta 扫描取得持久 DataItem 指纹，再由独立准备 User 读取 complete 边界并通过 System 正式签发，绑定指纹与预期摘要。MySQL／MongoDB 仅参与引擎注册、元数据扫描、发现复核及实际脱敏输出验收，不查询它们的授权边界、不初始化批准要求、不签发或检查源读取 Grant。原值访问套件继续保留 PostgreSQL 同一目标、两个不同 User 的最小源授权准备。确定性回归沿用现有 Online runner 中的 Security 套件、Hosted 生命周期和夹具测试，T4 沿用 `security-mysql-owner-protection`；无需新增标准入口或 CI Job。产品侧数据项权限管控的撤销由对应会话处理，本会话不修改其协议、API、迁移或读取实现。

2026-10-16，`9fed427f4` 的 Hosted T4 [38830258272](https://github.com/pampa0629/addp/actions/runs/38830258272) 完成三引擎正式扫描，但在 Grant 准备读取 Meta 分支列表时返回 500，尚未进入实际保护输出场景。根因为列表查询仍把业务分支当作无父节点入口，与显式结构根模型不符；现已改为根下直接分支并限定同 Tenant／Engine，旧无根查询已删除。SQLite 红绿回归、Meta 全部 Go T1 及三类单项的 PostgreSQL 定向门禁通过。该次 Hosted 清理通过且基础设施零残留；完整 T4 仍须复验。本轮共享变更的 `make test-go`、System Swagger 生成及 217 个公开路由覆盖通过；本地完整 Meta PostgreSQL 统计用例超时、System IAM 门禁迁移初始化超时分别记录，均不计为通过。

2026-10-16，提交 `05ea0c789` 的 [Hosted T4 38835960352](https://github.com/pampa0629/addp/actions/runs/38835960352) 已真实通过 v13，场景耗时约 841 秒，报告与七张截图已独立核对。三引擎正式扫描、可信 DataItem 边界读取及精确源 Grant 准备通过；System 前向迁移 `000210_source_single_record_targets` 在同一数据库身份函数内接入 MySQL 单表和 MongoDB 单集合，既有 schema/table 授权、Deny、受理回执、IAM、审计与历史校验和保持不变。`make test-module MODULE=system GO_ONLY=1`、13 项 `make test-system-iam-runner`、`make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package migration --test source-grant-relations"`（202／207／208／210）及 `--package engineaccess` 均通过，后者包含此前因数据库约束拒绝的两类目标正式签发用例。

v13 实际覆盖四 Owner 的五行 MySQL 默认保护与银行卡掩码、MongoDB `userInfo.phone` 掩码／常量替换／SM3 三算法及十个传输任务清理、两个 Security 停机窗口与十四个拒绝连接时间点，以及四 Owner 显式重新发现。资源从长期有效切换指定期限后，在北京时间 10:00:10 自然到期；同修订、同 Owner 进程的新执行分别得到 Manager 403、Develop 失败、Service 拒绝响应，Transfer 输出零行／零字节且目标不变。显式恢复长期并同步更高修订后，四 Owner 再次返回五行受保护结果；旧版本更新被拒绝。Console 显示长期有效，九个组合筛选、实际预览、五行 CSV 和 Monitor 追踪通过，零浏览器告警、零业务请求错误。领域资源、Hosted 应用、业务容器、Infra 与临时凭据清理通过，零残留。该结论属于固定提交的 Hosted 隔离部署；个人本地环境尚未重启，不以旧构建的本地结果代替新版运行验收。

2026-10-16，清理提交 `dd3d39890` 的 [Hosted T4 38840651279](https://github.com/pampa0629/addp/actions/runs/38840651279) 已通过同一 v13 完整套件，场景耗时约 809 秒，报告与七张截图已独立核对。System 实际 HTTP 日志只出现 PostgreSQL Engine 的七次 `access_grants` 请求及七次授权事实查询，MySQL 与 MongoDB 两者均为零；两类业务源继续完成正式扫描、发现复核和实际保护输出。MySQL 四 Owner 默认保护／银行卡掩码、MongoDB 掩码／常量替换／SM3 与十个传输任务清理、两个 Security 不可达窗口的十四个拒绝连接时间点、显式重新发现、长期／定期切换、同修订自然到期拒绝与恢复长期均通过。到期 Transfer 零行／零字节且目标不变；恢复后四 Owner 均返回五行受保护结果。Console 九个筛选场景、预览、五行 CSV 及 Monitor 追踪通过，零浏览器告警、零业务请求错误，领域资源与 Hosted Infra 清理零残留。

清理的确定性回归包含四个既有 Security 场景／Hosted 夹具测试文件（104 项）及 Online CI 登记测试（40 项），登记一致性检查通过；新增反例拒绝把 MySQL／MongoDB 参数或集合 Grant 准备重新放入初始化函数。标准 T4 入口仍是 `make test-online ONLINE_SUITE=security-mysql-owner-protection`，只在隔离 Hosted 部署执行。本次仅修改验收初始化、回归测试、CI 登记守卫及本说明；产品侧数据项权限管控的撤销未纳入本次修改。


Hosted 脱敏验收的 v14 扩展使用领域 v14、浏览器 v6：复用原普通 User、MySQL customers 与 MongoDB Persons，不新增源读取 Grant、权限或入口。MySQL 字段别名仍须保持默认保护，拼接敏感字段须终态失败且没有 result；Develop 专业详情与同一 execution 的 Monitor 详情均须展示 `develop.query.protection_required`、受控原因与处理建议，并保留两张真实截图。MongoDB 三算法及 Security 不可达窗口中的派生查询同样要求准备阶段的精确保护诊断，不能把无关执行错误计为保护拒绝。报告缺项、错误原因、原始错误正文、空结果对象冒充无结果及截图缺失均拒绝；旧 v13／浏览器 v5 结果不计为本扩展通过。标准入口仍为 `make test-online-runner` 和 `make test-online ONLINE_SUITE=security-mysql-owner-protection`；CI 登记守卫与确定性反例同次更新；本轮 146 项 Security／Hosted 夹具及 CI 登记回归、登记一致性检查通过。本地首次 `make test-online-runner` 聚合在另一会话尚未完成的 PostgreSQL 授权夹具签名不一致处退出，未进入本套件，不计为聚合通过；本轮不修改该夹具。本轮真实 Hosted 的首次失败与修正后通过证据见下文。


2026-10-16，v14 首次 Hosted [38868796504](https://github.com/pampa0629/addp/actions/runs/38868796504) 未通过：实际源码 `8d91f3b14`，在 MongoDB 首个派生查询的新增诊断断言处停止，尚未进入新增浏览器场景。独立解析 Develop 日志确认稳定码和原因正确，实际阶段为 `preparation`，其底层拒绝为输出血缘无法解析；新增夹具错误期待 `result`，现按真实失败边界修正，仍精确检查阶段且不改产品拒绝规则。退出清理通过，Infra 零残留。同提交 Platform consistency CI 已通过，不把该次 T4 计为通过；修正后须重新完整运行。


2026-10-16，修正提交 `3d4176f16` 的 [Hosted T4 38872372087](https://github.com/pampa0629/addp/actions/runs/38872372087) 已完整通过 v14，Job `116879250398`，领域场景耗时约 825 秒。预检的九个服务均为同一完整源码 `3d4176f168633f92187de49f0be4427bff50936c`。普通 User 33、Tenant 2、`online.security_mysql_owner_protection` 身份与原权限集合经 API 和浏览器会话重新绑定核对；未增加 MySQL／MongoDB 源读取 Grant。MySQL 别名查询返回五行且敏感列及别名均移除；派生表达式失败且无 result，显示 `develop.query.protection_required`、准备阶段、明确原因和修正建议。Develop 与 Monitor 两张新增截图已目视核对，同一失败 UUID 为 `911cf5f0-f7a3-4e11-a838-63989edef5bf`。MongoDB 掩码、常量、SM3 三例及不可达窗口回归均要求精确诊断、阶段与无结果，浏览器 v6 的新增五项事实全部通过，零浏览器告警、零业务请求错误。既有四 Owner 实际输出、两个 Security 停机窗口、显式重新发现、长期／定期与同修订自然到期及恢复保护场景继续通过。领域资源创建 11／删除 11／残留 0，Hosted 应用、业务容器与 Infra 清理通过、零残留；保留前述首次阶段预期错误的失败记录，不将其改记为通过。146 项本范围确定性回归与 CI 登记一致性检查在修正后再次通过；本轮没有改产品接口、脱敏规则、用户权限或个人运行服务。

Hosted 报告 v15 在既有 v14 场景上要求 Service 保护到期返回精确 `403/data_protection_required` 且只含本地化提示与稳定码；Transfer 的派生拒绝与到期拒绝必须各自携带准备阶段的安全保护诊断，血缘稳定码保留。真实 Hosted 运行前不计为 v15 通过。确定性回归、既有 Online 登记守卫和同一 suite 入口同步更新，原浏览器 v6 契约保持。

2026-10-16，Service／Transfer 的保护拒绝说明修正已完成本地验证：Service 全模块 Go T1、PostgreSQL 与 Swagger 62 路由覆盖通过；Transfer 全模块后端、130 项前端单元／24 项浏览器及构建通过，准备和结果阶段均使用共享失败组件，中英文不显示原始错误。148 项 Security／Hosted／CI 登记回归与登记一致性通过。Transfer PostgreSQL 保护用例（含原生／查询到期拒绝与零新增写入）通过，但完整聚合在另一会话正在修改的 continuous execution origin 校验两项失败，后续 PostgreSQL API 步骤未进入，不计为全门禁通过；`make test-changed` 缺少全仓 T2 连接变量而在预检退出，Service 模块聚合在其他会话 Transfer WIP 的 Ontology provenance 守卫处退出，均不计通过，未修改这些来源或守卫。本轮真实输出沿既有 Security Hosted v15 复验；本地个人服务未重启。

提交前补充核对 Provider 错误优先级：Service／Transfer 在门禁包装内遇到类型化缺表错误或 SQLSTATE 时保留既有查询失败性质，不将其解释为保护拒绝。Hosted `38885678273` 在排队阶段主动取消，避免使用缺少该反例的前置提交；未运行、不计 T4。之后本地 Transfer 全模块后端因另一会话新增的 `TestStartRejectsUnverifiedOriginBeforeCaptureOrExecutionWrite` WIP 用例失败，保留首轮全模块通过记录但不计最新工作区全门禁通过；本会话只沿标准后端入口用 GOFLAGS 限定保护／执行完成／读取隔离回归，不修改该来源校验。

上述 Provider 优先级补充后，Service 全模块 Go T1 再次通过；Transfer 使用 `GOFLAGS='-run=Test(Gate|Bounded|Protected|TransferExecutionReadIsolation|FinishExecution) -count=1' make test-transfer-backend` 通过，覆盖类型化／同文案／SQLSTATE 反例、真实完成事务安全投影、保护边界与专业读取隔离。该结果只计本会话所选 T1，不计完整后端或 PostgreSQL 聚合通过。

本轮根 `make test-online-runner` 在 `test-orchestrator-online-runner` 的既有 Hosted 生命周期夹具 20 秒超时处退出（61 项中 1 error），未进入最后的综合 Security 测试组，不计完整聚合通过；未调整 Orchestrator 入口、超时或回归。本范围已单独完成的 148 项 Security／Hosted／登记回归保持独立通过记录。最新补充源码 `6e31b58175d0c022f575480873e3197ff6622428` 已推送，Hosted v15 `38886293712` 已触发；运行完成前不计为 T4。

随后从 Git 精确导出已提交的 `6e31b58175d0c022f575480873e3197ff6622428` 到仓库外临时目录，不带共享工作区 WIP，沿相同 `make test-transfer-backend` 完成完整后端 T1，退出码 0；这是本轮已提交源码的全后端通过证据，不覆盖其他会话的未提交 origin 修改或 PostgreSQL T2。临时导出不含 `.git`，额外尝试的 Service `test-module` 自动发现无法运行，未计通过；Service 全模块 Go T1 仍以上述原工作区标准入口的成功结果为证据。

同提交 `6e31b58175d0c022f575480873e3197ff6622428` 的 Release/T2 [38886250264](https://github.com/pampa0629/addp/actions/runs/38886250264) 中，Service PostgreSQL 15 Job `116927119726`、Transfer 受保护导出 PostgreSQL 15 Job `116927119915` 均已完成且成功。该证据覆盖本轮正式提交的两个完整 T2 入口，不把共享工作区早先失败或未执行的步骤改记为通过，也不代表整条含其他 Owner 的 workflow 均通过。

Platform CI `38886250158` 的 Go workspace Job `116924589251` 首次未通过：新增 SQLSTATE 测试直接导入既有 pgx，但 Service／Transfer 的 go.mod 仍标记为 indirect，`go mod tidy -diff` 检出了漂移。已按 tidy 同步两处直接依赖标记；版本集合、go.sum、产品代码与运行配置均不变，两模块 `GOWORK=off go mod tidy -diff` 均退出 0。该失败属于本会话遗漏，保留首次失败记录；修正不改变正在运行的 Hosted 产品行为，T4 仍精确记录其实际源码 `6e31b58175d0c022f575480873e3197ff6622428`。同提交 Service／Transfer 前端 CI 均已成功。

2026-10-16，[Hosted v15 38886293712](https://github.com/pampa0629/addp/actions/runs/38886293712) 已完整通过，Job `116924746894`；领域场景耗时 827,466 ms，九个服务与干净预检源码均为 `6e31b58175d0c022f575480873e3197ff6622428`。普通 User 33、Tenant 2、原 `online.security_mysql_owner_protection` 角色完成验收。Service 自然到期返回精确 403／`data_protection_required` 且无数据；Transfer 到期执行 `6e2f1250-e6ac-4e68-92cc-3b7814f9ca1c` 返回准备阶段 `data_protection_required` 诊断，零写入且原目标不变；MongoDB 掩码、常量、SM3 三次派生查询均返回原 `query_output_lineage_unresolved` 稳定码与准备阶段安全诊断，目标前后为空。既有四出口正常保护、Security 不可达、长期／定期、恢复、字段独立策略和重启恢复继续通过。浏览器 v6 通过，收到 9 张截图；Develop／Monitor 两图已目视核对同一失败 UUID `f2a18ef8-481b-41f9-8ed1-81c98dd75fb7`、原因和建议。领域资源创建 11／删除 11／残留 0，Hosted 退出清理通过、Infra 零残留；System HTTP 日志独立核对仅有 PostgreSQL Engine 1 的 7 次精确 Grant 准备，MySQL／MongoDB 的准备请求为零。此 T4 验证真实后端拒绝事实，Transfer 专业失败页的中英文接线另由本轮 24 项浏览器门禁证明，不把 Mock 页面记为真实账号界面验收。个人 Service／Transfer 尚需用户重启后核对。

同源码的 Platform consistency Job `116924588991`、Service 前端 Job `116924589177`、Transfer 前端 Job `116924589310` 均成功；前述两个完整 PostgreSQL T2 Job 成功。依赖声明修正 `403054c63c10138fa41e9ac6b135aea93fe2edf5` 已确认纳入远端 main；它只同步直接／间接标记，无版本、go.sum 或产品代码变化，沿用上述产品行为证据，不声称九个 Hosted 服务运行了该后续 SHA。首次 Go workspace tidy 失败保留，两个模块的本地 tidy-diff 已通过；含修正的 main `547366d148d21ee17995418671296443353c078b` 对应 CI `38890009152` 的 Go workspace Job `116936933536` 在本次记录时仍排队，不计通过。

2026-10-16 后续核对，CI `38890009152` 的 [Go workspace Job 116936933536](https://github.com/pampa0629/addp/actions/runs/38890009152/job/116936933536) 已完成且成功，前述依赖声明漂移闭环，首次失败记录保留。本次只补充验收事实，不修改产品代码、依赖、权限或 CI 登记。

同日重启后，使用无痕 Chrome 的普通「outdoor开发者」账号完成 Service／Transfer 本地真实界面验收。源为 Business MySQL 的 `business.security_persons_acceptance_20261015`，沿现有 Service 私有测试服务 6 和新建手动 Transfer 任务 15 执行。Service 期限内返回 20 行手机号掩码；Transfer 写入 PostgreSQL 独立目标 `addp_transfer.security_refusal_ui_acceptance_20261016` 的执行 `45dd8771-3f02-494c-a001-30c5382992ae` 成功读取／写入 2,188 行。目标只读聚合核对 1,325 行手机号为空、858 行为标准前后缀掩码、其余 5 行也均含掩码字符，完整 11 位数字手机号为零；不输出原始业务值。源表通过正式有效期设置指定北京时间 20:54:00，在自然到期后 Service 新请求实际返回 403，页面显示保护原因及建议并清空旧预览；Transfer 新执行 `840e00a5-99f7-48d5-b903-40e00e89489a` 在准备阶段拒绝，安全诊断为 `addp.execution-failure/v3`、`data_protection_required`。Transfer 专业详情与 Monitor 同 UUID 详情均展示受控原因、建议、准备阶段和稳定码，未展示原始错误；页面读写均为零，持久化计数未产生，目标前后行数及完整内容摘要一致。此证据覆盖本地中文真实页面，不扩大为英文真实账号页面或读取结果阶段拒绝。

随后经正式设置恢复长期有效，四个出口均确认字段规则已同步且有效期内；Service 再次返回 20 行受保护数据，Transfer 新执行 `581f0ff7-0dbb-424e-a68a-404183778e40` 再次成功读取／写入 2,188 行，目标摘要与基线相同。Service 6 恢复原停用状态；手动任务 15 与仅含脱敏结果的目标表保留供复查，没有定时调度，不能记为资源零残留。首次 MySQL 目标尝试 `04d5fa12-ed62-4e2c-ad40-b6f677fc3db1` 因 TEXT 主键建表限制失败，未写入、不计为脱敏拒绝；仅调整本次临时任务目标为 PostgreSQL，未修改引擎实现或业务任务。此前一次浏览器会话被 `refresh_token_reuse_detected` 撤销，重新登录后经页面内导航完成上述验收；本轮未修复或宣称解决该认证问题，也未改变原值授权、MySQL／MongoDB 源数据权限或栅格能力。

Hosted 浏览器验收扩展为领域 v16／浏览器 v7：复用同一隔离普通 User、MySQL customers 与既有 PostgreSQL 目标，不增加 Permission 或源读取 Grant。浏览器绑定身份后沿正式 API 创建私有 Service 和手动 Transfer 定义，验证五行受保护输出；通过正式有效期 API 设置短期截止，等待自然到期，核对 Service 实际 403、本地化提示及旧预览清空，Transfer 准备阶段的安全诊断、零新增写入和目标不变，以及同一 UUID 的 Monitor 详情。退出时恢复长期并等待四出口同步，正常路径复验恢复输出；逐个删除两个测试定义并确认 404，失败也尝试全部清理。新报告须闭合这些事实与四张新增截图，缺项或旧 v6 不计通过。复用 `make test-online-runner`、现有登记守卫和 `security-mysql-owner-protection` Hosted workflow；不新增工作流、生产接口或永久脚本。真实 v16 运行前不计为新扩展通过。

本轮新增接线已通过 148 项 Security／Hosted／CI 登记确定性回归，最后调整后的 119 项领域与登记定向回归、JavaScript 语法校验及登记一致性检查通过；`make test-console-frontend` 的单元、138 项浏览器和构建通过。`make test-online-runner` 完整聚合在无关 HighGo Hosted 生命周期夹具的五个错误处退出，未进入最后的 Security 回归组，不计为聚合通过；本轮不修改该夹具，沿上述独立回归覆盖本范围。真实 v16 Hosted 结果另行记录。

2026-10-16，v16 首次 [Hosted 38919249803](https://github.com/pampa0629/addp/actions/runs/38919249803) 未通过，Job `117034601396`、实际源码 `f93c7928072622ace61492ab58e6ecd948a9f951`。新增浏览器夹具在首个 Transfer 正常执行的发起人校验处停止：误用了 Monitor 通用执行的 `triggered_by`，而 Transfer 专业 HTTP 响应的正式字段为 `trigger_by`；本次尚未进入新增 Service／Transfer 自然到期页面验收，不计为 v16 通过。按既有契约修正消费字段，继续精确核对原普通 User；不增加兼容字段或降级断言。新增确定性回归直接执行同一浏览器 Transfer helper，验证成功／失败执行均接受正确 `trigger_by`，拒绝错误或缺失发起人及仅含 Monitor 字段的响应；修改前重现同一身份断言失败，修正后 149 项 Security／Hosted／CI 登记回归、登记一致性与 JavaScript 语法检查通过。既有标准入口和路径自动发现已覆盖本次修改，不改产品 API、Swagger 或权限。Hosted 退出清理通过、Infra 零残留；同源码 Platform consistency Job `117034274031`、Console frontend Job `117034274256` 均已通过。

2026-10-17，第二次 [Hosted 38923900689](https://github.com/pampa0629/addp/actions/runs/38923900689) 未通过，Job `117050436707`，九个服务实际源码为 `7a8bce58d02c9803ae6d395c9a39cd18b7ef040d`。Transfer 首次执行已通过普通 User 身份与读取／写入五行校验，随后目标字段断言失败：夹具错误地将原生整表传输等同于 Service 的三列选择，预期抑制后只剩两列，而实际保留十一列非敏感字段且没有 email。现将 Transfer 的目标列及每行字段精确对齐本轮受保护源预览；Service 仍精确对齐发布的 default_fields 去除 email 后的字段。丢失其他非敏感列、重新引入 email、行中暗藏字段或更改客户标识均拒绝；新增反例修改前重现该失败，修改后通过。此轮尚未进入新增到期页面，不计为 v16 通过；Hosted 退出清理通过、Infra 零残留。

同次检查 Service 页面错误出口，明确的 `403/data_protection_required` 继续显示本地化提示、清空旧结果，但不再作为运行时 console.error；其他 403、500 和网络错误仍记录异常。新增反例先重现受控拒绝被误记为运行异常，修正后连同旧结果清空校验通过。本轮 `make test-service-frontend` 的 111 项单元与构建、150 项 Security／Hosted／CI 登记回归、登记一致性及 JavaScript 语法检查通过；既有标准入口、CI 路径发现和在线 suite 已覆盖，无接口、Swagger、权限或栅格变化。前一提交的 Console CI Job `117050359066` 成功；Platform consistency Job `117050358578` 在无关 PostgreSQL 模块门禁的旧调用字符串断言处失败，不计为平台全门禁通过，本会话不修改或重跑该范围。新的真实 Hosted 结果另行记录。

2026-10-17，[Hosted v16 38983726735](https://github.com/pampa0629/addp/actions/runs/38983726735) 已完整通过，Job `117250246594`，领域场景耗时 905,442 ms；九个服务及干净预检源码均为 `68d7cbd0311f51e00fc97c821c737f9c36d65f43`，包含本次修正 `cfd10d7ec`。普通 User 33、Tenant 2、原 `online.security_mysql_owner_protection` 角色完成浏览器 v7 验收。Service 期限内返回五行、抑制 email 并保留发布选择的两列；自然到期后的真实请求返回精确 403／`data_protection_required`，显示受控原因与建议并清空旧预览，恢复长期后再次返回相同五行保护结果。Transfer 原生传输完整保留受保护源的十一列非敏感字段；到期执行 `083b0ef0-eef3-434a-bb5e-f60620993145` 在准备阶段拒绝、零新增写入且完整目标数据不变，专业详情和 Monitor 同 UUID 均展示安全诊断、原因及建议。恢复长期后新执行再次成功读取／写入五行且目标与基线相同。四张新增真实截图已目视核对，全部十三张 PNG 与封闭报告事实已独立核验；预期 Service 403 恰好一次，额外业务失败与浏览器警告／异常均为零。

既有 MySQL 四 Owner、MongoDB 掩码／常量／SM3、字段独立策略、两个 Security 不可达窗口的十四个连接拒绝时间点、显式重新发现、同修订自然到期拒绝及恢复长期继续通过。领域定义创建 11／删除 11／残留 0；MongoDB 传输任务创建 10／删除 10／残留 0；浏览器额外的 Service／Transfer 定义创建 2／删除 2／残留 0，分别记录，不混为同一资源总数。Hosted 应用、业务容器、Infra 与临时凭据退出清理通过、零残留。System HTTP 日志独立核对只有 PostgreSQL Engine 1 的七次源 Grant 准备及七次授权事实查询，MySQL／MongoDB 两类源均未重新加入源授权准备依赖。对应 Service 前端 Job `117250200850`、Console 前端 Job `117250201054` 均成功；Platform consistency Job `117250200889` 仍在前述无关 PostgreSQL 调用字符串断言处失败，不计为平台全门禁通过。前述个人本地验收资源未纳入本次 Hosted 清理，本轮没有重启或清理个人服务；本地中文与本次 Hosted 英文保护提示分别有真实页面证据，不扩大为读取结果阶段的真实页面验收。

Hosted 后续扩展 v17／浏览器 v8 补充 Transfer 结果阶段的真实拒绝：复用隔离 MongoDB Persons 的短值、正式 phone 默认掩码及既有 PostgreSQL `mongodb_query_denied` 目标。先执行完整正常输出，再通过正式 Baseline CAS 更新将既有无效值处理临时改为 deny；追加执行在读取数据后的保护检查拒绝，必须持久化精确 result 诊断、零新增写入且既有目标完整不变。专业详情和同 UUID Monitor 留存两张新截图；恢复完整原基线并确认四 Owner 同步后复验正常输出，失败也恢复基线并清理两个测试定义。该夹具只在已有 disposable Hosted 准入下运行，不新增 Grant、Permission、引擎、端点或时钟／异常注入；不扩展目前仅 Manager preview 支持的独立 Policy owner 范围。复用原标准入口、CI 登记守卫与确定性反例，未取得真实 v17 证据前不计通过。

v17 接线已通过本范围 152 项 Security／Hosted／CI 登记回归；仅含本会话修改及 Security 登记片段的干净源码复验同样通过，登记一致性及 JavaScript 语法检查通过。新增确定性反例直接执行实际浏览器 helper，拒绝错误阶段、目标变化和非零写入，并验证更新已提交但响应丢失、恢复响应失败、单个任务删除失败时仍恢复完整基线并尝试全部任务清理；CAS 版本只前进，不回滚版本。`make test-console-frontend` 的 173 项单元、138 项浏览器及构建通过。以上不计为真实 v17 通过，亦不将其他 owner 的全仓聚合计为通过。

查询保护引擎范围排除采用 Projection v4：Security 在发布前核对 Enrollment 的引擎 ID／完整名称前像与 DataItem 指纹，Owner 由投影派生范围索引。Owner 首次同步及旧协议重放须消费到 `has_more=false` 后持久确认 initialized，期间拒绝读取；重放只在最后一页 ack，保持 Security 确认游标单调。Neo4j 用标准 openCypher 9 AST、物理数据库与内置函数事实证明本地只读范围，模板和正式图查询执行同一不可变计划；同引擎纳管而精确 ReadSet 未覆盖时仍拒绝。验证使用对应 Go 模块门禁、Common／Security／Develop PostgreSQL 门禁和 `make test-common-neo4j`，新增 Compose 门禁已接入标准自动发现与 T2 CI。

2026-10-17 原值授权扩展：规范与代码改为 scopes 集合，一次申请／审批可覆盖 Manager 预览和 Develop 查询，逐出口返回授权状态并可独立撤销；SchemaVersion 5 单向迁移旧申请为原出口单元素集合，删除旧 owner/action 列。Develop 保存默认脱敏结果及仅含执行时已授权原值的 Owner 私有有界历史；专业详情、列表与工作台轮询每次按当前可信用户及本地投影重判。到期或撤销回落默认保护，默认语义／结构变化则拒绝历史结果并要求重新查询。通用观察和导出不暴露私有载荷；Notebook 交互查询使用经控制面核验的 session 用户，完整导出仍走 Transfer 默认保护。共享申请弹窗由 common-frontend 唯一维护。Hosted 原值套件扩展为领域 v5／浏览器 v2，真实结果另行记录，不以历史 v4 验收代替。

结构化投影启动补齐仅处理 enrolling/active Enrollment，必须先锁定并核对 Enrollment 生命周期，再解析与检查 Owner 投影。released 保留的 active 投影记录是解除审计，不要求补出后续新加入的 Graph Owner，也不得重建已解除资源的保护。releasing/activating 同样不进入该字段升级路径。回归覆盖旧四 Owner 已解除资源在 Graph 初始化与结构化补齐顺序执行后的幂等性、历史内容不变，纳入既有 Security Go 与 PostgreSQL Lifecycle 门禁。

2026-10-17 原值授权 v5 Hosted 首次运行 [39018432898](https://github.com/pampa0629/addp/actions/runs/39018432898) 未进入领域验收：标准启动入口在 Hosted 跳过本地端口解析时，Graph 前端缺少默认端口，导致环境启动超时。归档 summary 确认 cleanup=passed、infra_cleanup=zero_residuals；不能计为原值授权或历史回落 T4 通过。已按现有默认配置补齐 GRAPH_FE_PORT，并在既有生命周期门禁中对照唯一端口清单验证全部前端默认值和显式覆盖。新增回归先复现未定义变量，修正后 make test-dev-lifecycle 全部通过；新增 Hosted 结果另行核验。本地 Security 后端与 Worker 重启后已稳定运行。

2026-10-17 本地 MySQL 原值范围验收：outdoor开发者的 Develop 单出口申请由另一管理员批准后，实际查询返回 10 行原值；同一用户的 Manager 预览和另一用户的 Develop 查询仍默认掩码。19:00 自然到期后，未重跑 SQL，同一执行 `00adf789-ec22-4f01-9875-add997566ebd` 的历史自动恢复掩码，原行数与执行耗时保留，Owner 对该历史的读取返回 200；控制面授权显示已到期。本次临时授权没有继续生效，既有 Manager 授权未修改。主动撤销的本地历史回落仍未验收。

该查询同时暴露私有原值历史进入 GORM 参数展开日志的缺陷。Develop 共用数据库连接现接入 `common/logger.WithoutSQLValues`，在格式化前移除绑定参数，查询错误日志仅保留固定类别、类型或合法 SQLSTATE；SQL 结构、级别、耗时、行数及业务错误链保留。回归先复现普通／慢查询和错误回显泄露，修正后 Common／Develop Go、全量 `make test-go` 的 22 个模块、Common／Develop PostgreSQL 门禁通过。实际执行事务验证私有历史仍完整持久化、必要过程事件保留、SQL 日志不含私有载荷；尚待用户重启后核对新进程，既有运行日志未删除。

后续 Hosted [39024702645](https://github.com/pampa0629/addp/actions/runs/39024702645) 已通过环境启动，但浏览器初始保护断言失败：API 场景清理在 Security 撤销 Manager 授权后即交接浏览器，浏览器预览比 Manager 撤销投影更新早约 1.4 秒。夹具现须在同一预算内确认真实预览恢复默认掩码后才交接，保持严格三行逐值断言；新增回归先复现缺少此屏障，修正后 `make test-security-online-runner` 的 175 项及 CI 登记检查通过。该次失败 cleanup=passed、infra_cleanup=zero_residuals，不能计为新范围 T4 通过；修正后的真实 Hosted 结果另行核验。

2026-10-17 修正后的 [Hosted 39028729141](https://github.com/pampa0629/addp/actions/runs/39028729141) 在源码 `188da46aed6e39b83cb540fcc66d0ca5c4d915e2` 上真实通过，领域 v5、浏览器 v2 归档报告均经严格字段与类型校验；三张保护状态截图存在，cleanup=passed、infra_cleanup=zero_residuals。两个真实 User 验证按出口审批、旧 Manager 授权不扩大、同一 Develop 执行历史在自然到期和主动撤销后回落掩码、另一用户不继承授权，以及 Security 不可达期间授权到期仍由 Owner 本地生效。归档 Develop 日志包含 3,155 条参数化 SQL trace 和 7 次执行元数据更新，夹具手机号原值与私有历史载荷键命中均为零；没有用关闭 SQL 日志代替过滤。该 T4 已补齐主动撤销的真实输出证据，本地未重复授予新授权。

用户重启后的本地构建 `e857ae775851b44c51c329766266cf64f6dd5ca8` 包含该修复，Develop／Security 均 ready/registered。管理员在 Console 对原 MySQL 验收表的新执行 `a812f451-4f09-4d11-862e-86c351ae6675` 成功返回 10 行默认掩码；新实例 `d2b42415-7fae-413b-bb96-41dfae8c076a` 的实际历史保存 UPDATE 只记录占位符，未展开结果或私有历史载荷。重启后的本地写入与 Hosted 获批原值写入两层证据共同闭环日志修复；修复前的既有运行日志未删除。此处不改变 SQL 脱敏重写、透明加密等仍未实现的边界。

2026-10-17 用户确认历史结果原则调整：执行记录不长期保存受保护字段原值；历史始终保留默认脱敏预览，重新查看原值须按当前授权重新查询。Develop 当次原值改用有界进程内暂存，旧私有历史单向移除。此前 v5 历史重判验收属于旧规则证据，不能替代本次 v6 的新规则验收。旧日志、截图和备份副本处置已按用户要求搁置，不列为产品功能或交付阻塞。


2026-10-18 用户确认的新历史规则已完成新版验收：[Hosted v6 39041158723](https://github.com/pampa0629/addp/actions/runs/39041158723)，Job `117420768709`，实际运行源码 `293fdafe2290955c15272056b627c8fad721be2a`。领域 v6／浏览器 v2 报告独立校验通过：获批当次 Develop 结果返回原值，历史即使授权有效也默认脱敏；自然到期、主动撤销、另一用户与 Security 不可达场景通过，按出口授权边界保持独立。浏览器 v2 覆盖 Console 申请审批和 Manager 保护状态，Develop 当次／历史区别由真实领域 API 与工作台前端门禁验证。本地新构建已就绪，单向迁移完成、旧私有查询历史键剩余零条；两条既有执行仍保留十行脱敏预览，真实 Chrome 历史详情核对正常。Hosted 生命周期清理通过、Infra 零残留。此结果替代旧 v5 对当前历史规则的适用性，不改变旧验收记录，也不恢复旧日志、截图或备份处置事项；SQL 自动重写、透明加密及完整角色策略等父功能缺口保持原状。


2026-10-18 补齐此前 v17 的归档核对：[Hosted 38987833025](https://github.com/pampa0629/addp/actions/runs/38987833025)／Job `117263317878` 已真实通过，精确源码 `8025781f5c825b00f3b9046441c236d9d4d7e768`，九个服务与干净预检一致，领域耗时 975,283 ms。领域 v17 与浏览器 v8 报告交叉一致，浏览器完整字段／类型严格校验和十五张 PNG 完整性检查通过，新增 Transfer／Monitor 两图已目视核对。普通 User 33、Tenant 2 的执行 `e975d6f4-5e36-4a02-9fab-d6b54a32cf9d` 在读取后的结果保护阶段精确返回 `data_protection_required`／`result`，零新增写入，既有目标完整不变；恢复原默认保护后再次执行正常，原基线恢复与临时定义清理通过。领域资源 11／11／0、MongoDB 任务 10／10／0、原输出浏览器定义 2／2／0、新结果浏览器定义 2／2／0 分别核对；Hosted 生命周期清理通过、Infra 零残留。浏览器额外业务失败和告警／异常均为零。该证据属于上述固定源码，不声称其运行了后续原值历史新规则；原值新规则另由前述 v6 证明。本轮沿用既有 Hosted 成功运行，不重复触发；当前 `make test-security-online-runner` 的 180 项、独立 Graph 夹具 13 项及 CI 登记检查通过，不外推为完整平台门禁通过。


2026-10-18 四出口字段策略扩展（用户已确认，本轮验收中）：正式字段 Policy 可独立绑定 `manager/preview`、`develop/query`、`service/service_execute`、`transfer/export`，继续只允许在当前默认保护的算法许可、保留参数上限和异常值规则内收紧。唯一 Owner 契约派生可编辑范围，既有 Policy 列表通过 `supported_scopes` 发布；同一策略表单选择出口，完整读取策略分页，不复制前端可编辑名单。创建、更新、撤销只重新发布该 Owner 投影，默认保护收紧或算法许可收回仍统一重编译；恢复默认不解除纳管。Graph、文档正文、Manager profile 不新增可编辑策略，原值申请仍只限 Manager 与 Develop。各 Owner 既有结果保护器直接执行新投影，不新增数据请求中的 Security 依赖；不改变 SQL 读取范围、血缘或不支持形态的保守拒绝边界。真实 Hosted 扩展为领域 v18：同一空间表的三个普通属性字段在四个出口分别设置不同算法／参数，并核对四种真实输出、规则持久恢复与撤销回落；这不是栅格任务。具体门禁与运行结果另行记录，未通过前不计真实验收完成。


本轮分层验证：`make test-module MODULE=security GO_ONLY=1`、`make test-security-postgres`（包含四出口字段算法许可／参数上限／许可撤回与撤销回落）、`make test-authorization`、Security Swagger 生成与 63 个公开路由覆盖通过；新版 `make test-security-online-runner` 的 182 项、独立 Graph 夹具 15 项及登记检查通过。完整 `make test-module MODULE=security` 已尝试，但平台 T0 在既有 `test-pg-authorization-online-runner` 的 `HostedPGAuthorizationTest.test_lifecycle_and_source_failure_and_suite_failure_clean_owned_resources`（3 个失败、1 个错误）退出，未进入模块阶段；不能将完整模块聚合计为通过。本会话不扩修该源授权夹具。Security 前端最终复验已通过 223 项单元、30 项浏览器与构建；真实 v18 Hosted 尚未通过，历史 v17 不替代新配置范围的真实运行证据。


同轮 Console 集成回归：已在标准浏览器入口复现 `security-authorization-refresh.spec.js` 原两项手机号策略用例等待“收紧保护”超时（2 失败／1 通过）。原因是独立 HTTP 夹具遗漏新的 `supported_scopes`，不是恢复旧 Manager-only 路线的理由。补齐同一正式响应后，将前后掩码／SM3 草稿用例扩展到四个出口，核对令牌刷新后的选定出口、参数、依据和零写入，9 项全部通过。命令为在 `console/frontend` 运行 `node ../../scripts/test/frontend-browser-gate.mjs e2e/security-authorization-refresh.spec.js`；不以此定向结果代替全量 Console 门禁。产品实现提交 `0b3e00f12`；本轮原值授权、历史脱敏原则及源数据权限均未改变。

重启后本地真实界面核对：Security 源码 `273ac2f7c` 已包含上述产品提交，ready／registered。使用 Console 的「ADDP 开发管理员」查看 MySQL `business.security_persons_acceptance_20261015` 的 `phone` 字段策略，四个生效出口及各自算法／参数表单可正常切换；已取消草稿，没有写入策略或变更授权。[Hosted v18 首轮 39087749235](https://github.com/pampa0629/addp/actions/runs/39087749235)／源码 `645ebd416` 在初始 MongoDB 技术字段搜索返回 `403 security_protection_required` 时退出，未进入新策略场景；清理通过、Infra 零残留。日志确认新租户的 Manager durable checkpoint 尚未初始化，验收就绪探测现只对该明确错误码限时等待，必须取得真实且身份／字段匹配的搜索结果才继续，持续拒绝仍失败；产品保护边界不变。新增红测已复现并修正，定向 `python3 -m unittest scripts/test/security-mysql-owner-protection-online_test.py` 的 84 项通过。复跑 `make test-security-online-runner` 时 Graph 17 项通过、主集合 183 项中 182 通过，仅另一个 Owner 的 `manager-internal-artifact-lineage/v11` 登记断言失败，已交由清单维护会话协调；不计完整聚合门禁通过。

上述等待修正已提交推送为 `cd2803e94`；对应 Owner 随后在 `1ff1ff9e0` 修正 Manager v12 登记负例后，本会话重新执行 `make test-security-online-runner`，主集合 183 项、独立 Graph 17 项及 CI 登记一致性检查全部通过，解除前述登记失败；此结果只指该标准入口，不替代完整模块门禁或 T4。[新轮 Hosted 39122735286](https://github.com/pampa0629/addp/actions/runs/39122735286)／Job `117661462679` 在精确源码 `cd2803e949dbc1b95d8a8089f2e4121d247b53ef` 上执行失败：场景 1,200,110 ms 总超时，清理通过、Infra 零残留。Meta 的首次 PostgreSQL／MongoDB 扫描均在 Manager 新租户 checkpoint 初始化前提交技术索引批次并收到 403；其后搜索虽恢复 200，首次索引批次没有投递，持续空命中直到总预算耗尽，尚未进入新字段策略场景。

该失败的验收准备修正将同一普通 User 的 Manager 搜索边界有界就绪检查移至首次扫描之前，包括 PostgreSQL 源授权准备的扫描；就绪只证明合法 200 响应，随后仍须独立核对实际 DataItem、字段和高亮。首次 MongoDB 字段命中另外限定 60 秒，禁止用重扫修复缺失索引；整体预算和保护拒绝规则不变。同时修正 `TestProtectionAccessScopesAgainstPostgres` 的时间精度夹具：原纳秒截止值写入 PostgreSQL 截至微秒后，原值被审批上限正确拒绝，使用固定额外 123 ns 已稳定复现；正常夹具改用微秒可精确表示的截止时间，不放宽产品授权校验。`make test-module MODULE=security GO_ONLY=1` 及 `make test-security-postgres` 已通过，后者包括正式 AccessScopes、FieldAlgorithms 和迁移／HTTP 集成用例；`make test-security-online-runner` 最终主集合 187 项、Graph 20 项及登记一致性通过，覆盖首次检查点失败时禁止启动扫描／签发准备、首次字段搜索的独立截止时间及不重扫。新的 Hosted 运行尚未验证。

修正提交 `fb13955d8` 的 [Hosted 39127891317](https://github.com/pampa0629/addp/actions/runs/39127891317) 已越过首次索引准备，但在资源有效期校验失败，场景耗时 452,072 ms，清理通过、Infra 零残留。验收等待函数只等四个数据输出 Owner，而有效期快照要求全部五个必要 Owner，Graph 回执延迟时提前进入校验；恢复清理同受影响。现统一等待 `PROTECTION_OWNERS` 的唯一完整集合，既有修订的初始快照也先经过相同屏障，不放宽有效期、到期拒绝、恢复或总预算断言；错误明确返回具体 Owner 的同步和时间状态。延迟回执及缺失／重复／未确认／非 active Graph 的红测已复现，修后 `make test-security-online-runner` 的主集合 190 项、Graph 20 项及登记一致性全部通过，真实 T4 仍待修后复验。同源 [Security PostgreSQL CI Job 117684595143](https://github.com/pampa0629/addp/actions/runs/39127876035/job/117684595143) 已独立核对为通过，只证明 PostgreSQL 门禁，不代替 T4。

修正提交 `441e4e5ca` 的 [Hosted 39132512437](https://github.com/pampa0629/addp/actions/runs/39132512437) 已进入真实浏览器，但资源筛选的旧四 Owner 断言拒绝页面正确返回的 Graph，第 814,411 ms 失败；后续浏览器导出／恢复／Monitor 不计通过，清理通过、Infra 零残留。浏览器现在唯一消费领域套件既有 `PROTECTION_OWNERS` 由启动包装器传入的 `required_owners`，删除页面脚本内四项及另两处五项名单和固定数量；资源筛选、Service／Transfer 有效期恢复和结果保护共用精确完整集合与逐项 active／ack／valid 校验。目录缺失、空、重复、非法名称、Graph 缺失／重复／未同步／无效状态均拒绝，没有四项回退或宽松包含。红测已复现；`make test-security-online-runner` 最终主集合 191 项、Graph 20 项及登记一致性通过，真实 T4 尚待本次修后复验。产品消费者、原值授权、期限、身份、输出及清理边界不变。

四出口字段策略的完整真实验收已通过：[Hosted 39136902282](https://github.com/pampa0629/addp/actions/runs/39136902282)／Job `117703147439`，精确源码 `7207a59f714e2d27ca633d67b1c4c50680239b67`（包含 `002ceed7b`），干净预检和十个服务构建一致。下载后独立校验领域 v18、浏览器 v8 严格字段／类型及相互一致，15 张 PNG 完整且已目视核对；领域场景 972,128 ms，通过同一普通 User 33／Tenant 2。三字段 × 四出口的独立算法／参数、默认掩码／替换／SM3、Security／Manager 重启后的定义保持、撤销回落、MongoDB 嵌套保护及原始记录导出、Security 不可达期间的实际输出、资源自然到期四出口拒绝和改回长期后的恢复均通过。浏览器九组资源筛选、字段检索定位、Develop 保护诊断、Service 拒绝／恢复、Transfer 准备及结果阶段拒绝／零写入／目标保持和同一 Monitor execution、CSV 导出及来源核对通过；额外业务失败和浏览器告警均为零。领域资源 11／11／0、MongoDB 临时任务 10／10／0、原输出浏览器定义 2／2／0、结果浏览器定义 2／2／0 分别核对；基线恢复、临时策略撤销、Hosted 清理通过、Infra 零残留。预期保护拒绝是业务断言，不记跳过或未运行；本次新增范围没有跳过项。该证据不扩大原值授权、不证明 SQL 自动重写、静态加密、完整角色策略或全平台／HA 门禁。

字段保护修订入口复用既有评估历史弹窗和 Policy 详情接口，展示有权查看的各出口不可变修订（包括已撤销策略）、算法参数、异常处理、操作者、时间与依据，不增加写入接口或审计存储。缺少策略读取权限时不请求策略，读取失败明确报错，资源切换或关闭后不接受旧响应。`make test-security-frontend` 通过 236 项确定性测试、31 项隔离浏览器测试和生产构建；四出口启用／撤销历史的展示及零写入由浏览器夹具验证。本地 Console 已只读核对 MySQL `phone` 的评估修订和 MongoDB `userInfo.phone` 既有掩码、替换、SM3 三次策略修订；未为验收新建或修改策略。该只读入口尚无新增 Hosted T4，不能将前述四出口执行 T4 扩大为该入口的 Hosted 证据。

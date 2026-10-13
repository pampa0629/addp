# Security 模块说明

Security 是 ADDP 数据安全控制面，唯一拥有敏感数据类型、安全分类、安全等级、检测器、发现、资源安全评估、保护基线、策略、原值访问申请、临时原值授权和保护投影。DetectorCapability 是平台代码提供的只读可信能力，Detector 是 Tenant 把能力绑定到 SensitiveDataType 的版本化启用配置；发现不得通过敏感类型代码或名称猜测绑定。当前已经实现基础定义、显式保护纳管、Owner 投影变化流、acknowledgement 激活/释放屏障，以及手机号元数据/文档、邮箱、身份证件号码和银行卡号元数据检测、无原值 Finding、一次性 Finding review、不可变 Assessment/ProtectionPolicy revision、唯一投影编译和显式重新发现/续期。Manager、Develop、Service 已具备自身字段级动作投影，Transfer bounded snapshot 的独立 `export` 动作与 PostgreSQL、MongoDB 原始记录执行器已完成。原值访问只能由当前用户从 Manager 预览出口发起 ProtectionAccessRequest，再由另一名有审批权限的用户在 Security 审批；ProtectionExemption 只保存审批后形成的按用户、按字段、按 `manager/preview` 出口、最长 30 天的临时授权。Projection v2 始终保留默认保护决策并携带主体级限时 `allow`，Owner 到期、撤销、Assessment 修订变化或主体不匹配时本地自动回落。用途约束和其他主体类型属于后续阶段。

## 边界

- 不拥有用户、角色、登录认证或资源授权；这些事实属于 System/IAM 和资源 Owner。
- 当前所有 active Security Permission 只允许 `tenant` Scope；管理 API 只消费 Tenant Context，不把 Department 或 Project Group Assignment 当作资源过滤。租户治理使用 `tenant.security_manager`，原值申请使用 `tenant.protected_data_requester` 或等价的 Tenant-only 自定义 Role。未来开放组织级授权前必须先在规范中建立可信的资源归属、Scope Binding、显式 Deny 与 owner 最终策略校验契约。
- 不复制 Meta DataItem、CatalogEntry 或 CatalogComponent，不以 Catalog 建档作为安全事实成立的前置条件；Enrollment 只冻结 Engine ID、item type 与 full_name 的最小保护目标快照用于展示和审计，不保存 attributes 或字段事实。
- 不代理数据预览、查询、导出或服务流量；Owner 使用 `common/dataprotection` 在自身服务端执行保护投影。
- `common/secretcipher` 只负责静态敏感配置值加解密，不是 Security 业务模块的一部分。
- 未纳管资源不进入检测、投影或保护路径，不产生额外远程调用和安全审计负担。
- Manager、Develop、Service、Transfer 的必要 Owner 集合及各自字段级主动作只能在 `internal/service` 的 Owner 契约中定义一次；Enrollment 创建、全量重编译、变化流校验和历史投影升级均从该契约派生，不得重复写 Owner 切片或 action 分支。当前可申请原值访问的出口另行严格限定为 `manager/preview`，不能从 Owner 主动作集合自动扩张。

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

四个必要 Owner 确认 enrolling 门禁后，Security 在同一事务创建一次 discovery execution。治理人员也可携带 Enrollment `version` 显式创建重新发现执行；同一纳管同时至多一个 pending/running execution。完成后保存最新结构或文本快照并重新编译、续期投影。结构化 DataItem 目前使用 `addp.detector.phone_metadata/v2`、`addp.detector.email_metadata/v1`、`addp.detector.identity_document_number_metadata/v1` 和 `addp.detector.bank_card_number_metadata/v1` 按字段路径、确定性扁平路径语义和通用类型独立识别；文档使用 `addp.detector.phone_document/v1` 在当次内存样本中识别精确 11 位 ASCII 数字串。身份证件号码元数据能力只识别精确字段语义，不读取或校验业务值；结构化字段通过只读算法目录选择 Unicode 前后缀掩码、同类型固定值替换或普通 SM3 哈希；字段策略在基线显式 `allowed_algorithms` 内独立配置算法与 `parameters`，前后保留数不得超过基线上限。基线参数和许可收紧后，不再满足约束的字段策略回退到当前基线。图层沿用带空间字段的数据表语义，属性字段可脱敏，geometry 不执行标量算法。算法执行复用 `common/dataprotection`；文档正文仍只支持既有手机号片段保护。检测能力都不持久化原始业务值。Finding 必须通过当前 Tenant 的 Detector 绑定取得 SensitiveDataType 和自动采用置信度；达到绑定阈值且存在有效 ProtectionBaseline 后，唯一编译器对结构化组件为 Manager 生成 `preview` 和系统派生的 `profile` 规则，为 Develop 生成独立 `query` 规则，为 Service 生成独立 `service_execute` 规则，并为 Transfer bounded snapshot 生成独立 `export` 规则；对文档虚拟组件 `$document.text` 只生成 Manager `search_index` 规则。没有当前动作执行器的 Owner 出口继续保持资源级 deny。

`released` Enrollment 永久保存退出审计且不可恢复状态。再次保护只能通过旧记录上的重新纳入命令创建新的 `activating` Enrollment，并重新走四个 Owner 激活屏障；同一目标不允许同时存在两条未退出生命周期。

成功发现后 Enrollment 查询批量返回当前快照 Finding 总数、待复核数和已复核数；Finding 查询使用 `enrollment_id + source_snapshot_hash` 精确限定当前候选，并在分页响应中携带可选不可变初审记录，以及由当前 Detector、Assessment、ProtectionBaseline 和已发布 Projection 批量组装的只读 `explanation`。当前组件结构仍匹配正式 Assessment 时，`explanation.assessment_id` 在 `sensitive` 与 `not_sensitive` 结论下都指向同一精确聚合，供“受保护资源”继续追加修订；组件结构冲突时不返回该关联，前端不得按字段名猜测。集中“待复核候选”是 `/protection-enrollments?tab=review-queue` 的子视图，只通过 `GET /findings?snapshot_scope=current&review_state=pending` 聚合未退出 Enrollment 当前 Finding，不新增队列实体或第二套复核状态。前端不自行推导保护结论，解释链不持久化且不含原值。历史快照只用于审计。零命中只表示当前检测能力未发现候选，不编译 `allow` 且继续资源级 deny。治理人员确认当前无需保护时，必须使用唯一 Release 路径并提交 `basis=no_supported_findings`、Enrollment `version` 和原因；服务端校验最近发现已完成、Finding 数为 0 且无在途发现执行，然后冻结退出依据、发起人、时间和依据快照。

识别质量摘要直接从 Finding、不可变 review 和 Assessment 当前修订即时聚合，不建立统计表或双写链。当前候选只取各未退出 Enrollment 最新成功发现；历史人工质量样本按 Enrollment、组件和检测能力版本折叠为最新 review。人工指定只作为可能漏检的线索，不归因于某个 Detector。

创建 Enrollment 的唯一用户输入是 Meta 资源树返回的 DataItem ResourceLocator；Security 自行计算 fingerprint，只纳管完整 DataItem。旧的 fingerprint 与字段路径自由输入路线不存在。字段组件通常由 Detector 发现；自动发现漏检时，治理人员只能从 Security Backend 实时读取并校验、且尚未形成任何正式 Assessment 的 Meta 当前字段清单中选择组件，直接形成来源为 `manual` 的正式 Assessment，不得自由填写字段路径。已存在 Assessment 的组件不再出现在人工指定候选中，其后续调整或撤销必须在既有聚合上追加修订。Finding 误报通过不可变 `reject` review 收口，既有正式 Assessment 错误则追加 `not_sensitive` 修订撤销，二者都由唯一投影编译器重新发布保护结果。

ProtectionPolicy 首期只绑定正式 Assessment + `manager` + `preview`，只能把当前 ProtectionBaseline 收紧为 `mask|suppress|deny`，不复制算法参数、不承载授权或例外。创建、更新和撤销都追加不可变 revision，并在同一事务调用唯一投影编译器；撤销后回落到 Assessment + ProtectionBaseline，不解除纳管。

ProtectionAccessRequest 是原值访问的唯一入口，只能由当前可信 User AuthContext 从 Manager 预览出口针对正式敏感 Assessment 发起，正文不得指定主体；申请本身不改变保护效果。申请状态固定为 `pending|approved|rejected|expired`，由 Security 按服务端时间返回有效状态；超过截止时间的未决策申请不再属于待审批，且不阻止申请人重新提交。唯一审批工作区使用 `scope=pending|history` 分别展示未过期待审批申请与已批准、已驳回、已过期历史，必须服务端分页，并可按申请人、资源或字段、申请时间筛选；历史记录还可按处理结果以及当前 `authorization_state=active|expired|revoked|superseded` 筛选，当前授权筛选必须在分页和总数统计前按当前 Exemption、Assessment revision 与服务端时间完成。批准历史必须另外返回当前授权状态与原授权截止时间，不能把“已批准”误作当前仍可访问原值。审批响应返回权威 `enrollment_id`，前端据此复用既有受保护资源详情和授权撤销路径，不按资源名称猜测、不复制第二套授权管理交互。本人待审申请可见但不可自审。申请人和审批人的 Principal ID 是权威审计身份；Security 只在申请或审批动作发生时通过 System 受信租户用户引用接口校验身份并固化显示名快照，审批读取不实时跨模块查询，服务启动会为旧记录补齐一次快照。另一名具有审批权限的用户在截止时间前批准后，Security 才创建或重新激活绑定 `{assessment, manager, preview, user}` 的 ProtectionExemption 并追加不可变 revision；拒绝不产生授权，申请人与审批人相同或申请已过期均冲突。Assessment 后续产生新修订时旧授权立即失效，不得静默恢复。授权效果固定为限时 `allow`，最长 30 天且不得超过申请期限，业务依据和审批理由必填。Projection v2 的规则始终保留 Policy/Baseline 默认 `decision`，主体授权放在 `authorizations`；Owner 必须从服务端可信 AuthContext 匹配 User，不能接受浏览器提交主体。临时授权只能提前撤销，不能直接创建、续期或重新启用；需要继续访问时必须重新申请和审批。

`manager/profile` 不建立可编辑 Policy：唯一编译器把有效 `preview=mask|suppress` 派生为 `profile=suppress`，把 `preview=deny` 派生为 `profile=deny`。Manager 负责把 `profile=suppress` 执行为整个字段剖析对象的移除，Security 不复制 Manager 指标结构。

创建 SensitiveDataType 必须同时提交其自动发现初始等级的完整默认保护，并在同一事务创建有效 ProtectionBaseline；不允许创建“已定义但无默认保护”的半成品。SensitiveDataType 更换自动发现初始等级前，新组合必须已存在有效 ProtectionBaseline；当前初始等级对应的 ProtectionBaseline 不得停用、改绑或单独删除。其他等级的 ProtectionBaseline 创建、更新、启停、改绑和带 `version` 删除必须根据 Security 自有 Finding/Assessment 依赖精准重编译受影响 Enrollment，并与定义写入保持同一事务；SensitiveDataType 自动发现初始等级变化只重算未复核候选 Finding；Detector 自动采用置信度变化走 Detector 配置变更的有界重新发现路径。正式 Assessment revision 冻结当时的类型、分类和等级，名称或排序等展示变化不制造投影版本。影响解析不得扫描全租户 Enrollment，也不得调用 Meta、Catalog 或 Engine。人工确认或调整 Assessment 时，目标 SensitiveDataType + SecurityGrade 也必须已有有效 ProtectionBaseline；`baseline_missing` 仅作为存量异常或一致性故障的失效关闭观测状态，不是正常配置流程。

Owner 变化流是唯一投影交付路线。Manager、Transfer、Develop、Service 只能使用各自固定 Tenant Service Access Token 拉取自身变化并确认本地原子安装的 cursor；不能提交 consumer owner 或资源清单。

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

普通账号资源组合筛选 T4 复用现有 Console 登录与 `security-mysql-owner-protection` 浏览器流程：使用同一普通 User 的正式 AuthContext，逐项核对名称、引擎、生命周期状态及列表范围的请求参数、返回成员、分页总数和实际表格。覆盖正确组合、错误引擎、保护中与已停止保护状态的空交集、百分号字面匹配、范围切换及重置恢复；当前四出口回执、有效期和页面截止时间同时核对。筛选阶段仅允许 Security GET，不创建或修改纳管、策略、授权和角色。 跨模块整页导航后，验收 API 必须从本次浏览器成功业务响应的 Authorization 绑定当前会话，并重新核对同一 User、Tenant 和完整权限集合；禁止继续使用导航前捕获的旧令牌，也不回退准备身份。新增三张筛选截图，浏览器报告升级为 v4、场景报告升级为 v12，严格校验新增字段；既有客户检索、预览、CSV、Monitor、银行卡和停机回归保持完整；沿现有前端、Online runner 及 Hosted 入口发现，1200 秒总预算不变。完整报告与截图完成独立核对前，不计为普通账号组合筛选 T4 通过。

## 数据库启动所有权

本模块 schema 迁移仅由 Backend 执行，Worker 只读校验成功提交的 schema 版本。Backend 多实例通过模块级数据库锁协调；开发脚本及 Compose 在所属 Backend 就绪后启动 Worker。结构或初始化迁移变化须递增模块 `SchemaVersion`，共享规则见 `docs/spec/addp开发服务生命周期与构建身份规范.md`。

受保护资源列表支持资源 full_name 字面子串、引擎和既有生命周期状态的服务端组合筛选，计数与分页采用相同条件。Owner 同步摘要另从既有投影派生有效期和当前有效状态；界面区分规则同步与时间有效性，过期仍保持纳管并通过现有显式重新发现续期，不新增自动续期或调度。银行卡号元数据能力 `addp.detector.bank_card_number_metadata/v1` 只对表/集合字符串字段的明确银行卡号名称进行精确识别，复用现有末级/扁平路径语义和发现、复核、四 Owner 编译；不读取业务行或推断普通账号/卡 ID。

提交 `b8ce75038` 的上述扩展已通过 `make test-module MODULE=security`：Go T1、212 个前端确定性用例、25 个浏览器用例、构建及 Security PostgreSQL T2 均通过；Swagger 生成和 62 条路由覆盖核验通过。[同提交 Platform CI 38338630796](https://github.com/pampa0629/addp/actions/runs/38338630796) 已成功。2026-10-13 重启后，本地 Security 就绪且构建包含该提交；无痕 Chrome 的 outdoor 开发者账号核对 Persons 名称有匹配／无匹配／重置恢复，以及四出口截止时间、独立时间有效状态和详情起止时间。Chrome 原生自动化通道不稳定的剩余控件由内置浏览器的 ADDP 开发管理员账号只读补验：MongoDB 与同步完成的组合找到 Persons，保护中与已停止保护的交集为空，重置后恢复；银行卡号能力的版本、字符串类型、精确别名、隐私边界和号码有效性／卡种／校验位局限均在真实界面可见。查看后取消表单，现有类型仍保持一项识别方式，未保存新增绑定或改变保护、授权。此记录不计为普通账号全部组合筛选、真实银行卡字段的发现与数据出口 T4，亦未在本地改时钟制造到期场景。

银行卡号真实输出验收扩展复用 `security-mysql-owner-protection` Hosted 套件，报告升级为 v11。专用 MySQL `bank_card_accounts` 和 PostgreSQL `bank_card_transfer` 随当次夹具创建与销毁，普通用户仅增加这两张表的精确读取 Grant；准备身份沿正式 API 创建银行卡类型、默认前后缀掩码和元数据检测绑定。真实 Meta 扫描后，正式发现及复核须形成唯一 `bank_card_number` 字符串 Finding/Assessment；相似名称和非字符串 `credit_card_number` 不得产生银行卡候选。四 Owner 的当前投影须确认，再逐行核对五条掩码输出、null、空串、Unicode、短值及全部相邻反例字段；Transfer 从空目标开始，执行 UUID、普通发起人、五条读写与实际目标预览必须一致。新任务和服务必须删除并核对不存在，失败仍走既有 Hosted 清理。扩展后仅此套件的总场景预算为 1200 秒，单请求仍最多 30 秒，检索收敛等待和全部保护断言保持；`security-plaintext-access` 继续使用 900 秒默认总预算。确定性反例、夹具、生命周期及 CI 登记沿 `make test-online-runner` 自动发现；不新增工作流或本地生命周期操作，真实 T4 结论以完整 Hosted 报告及下述独立核对记录为准。

提交 `333cc6ae5` 的 [Hosted T4 38425834669](https://github.com/pampa0629/addp/actions/runs/38425834669) 因旧 900 秒总预算耗尽失败：流程已走过银行卡场景及后续 MongoDB 场景，最终邮箱字段检索仍在等待索引隔离解除时被外层截断，未生成完整 v11 场景报告，不计任何新增 T4 通过。历史 v10 总耗时约 767 秒，新增银行卡发现、四 Owner 回执和实际落表进一步占用串行预算；因此同步调整上述扩展套件的总预算，不能靠跳过检索、降低断言或忽略超时认定成功。本次报告确认 Hosted 清理通过、Infra 零残留。

提交 `ea1379586` 的 [2026-10-14 Hosted T4 38433500090](https://github.com/pampa0629/addp/actions/runs/38433500090) 已真实通过 v11，场景耗时约 848 秒，并完成下载报告的独立核对：纳管前原值对照通过，银行卡检测仅命中字符串 `bank_card_number`，Finding 与正式 Assessment 绑定同一银行卡敏感类型；四 Owner 均确认当前掩码投影。Manager、Develop、Service 和 Transfer 各核对五行前后缀掩码、Unicode、短值、null、空串抑制及全部相邻反例字段；Transfer 从空目标开始，同一普通用户发起新执行，实际五条读写及目标重扫预览一致。银行卡临时任务和服务均删除并确认不存在。既有两个 Security 停机窗口、14 个拒绝连接时间点、MongoDB 三算法及十个任务清理、四 Owner 显式重新发现同时通过；Console 邮箱字段检索、受保护客户预览、五行 CSV 和 Monitor 报告通过，零浏览器告警、零业务请求错误，四张截图已目视核对。银行卡输出证据来自真实 API 和 Worker，上述浏览器截图属于既有客户数据回归，不扩大为银行卡界面或普通账号全部组合筛选验收。领域资源、Hosted 应用、业务容器、Infra 与凭据清理通过，零残留。提交前的 `make test-online-runner` 已完整通过；总预算回归拒绝旧银行卡 900 秒配置，并确认原值访问套件仍为 900 秒。该结果不计为保护投影长周期自然到期或全平台高可用验收。

提交 `dc8d4520f` 的 [Hosted T4 38471195140](https://github.com/pampa0629/addp/actions/runs/38471195140) 在新增浏览器阶段失败：整页导航进入 Security 后继续使用导航前的浏览器 API 客户端，Meta 引擎列表返回 `401 authentication_required`。该结果不能计为筛选或完整 v12 通过；报告确认 Hosted 清理通过、Infra 零残留。修复沿既有浏览器请求身份边界绑定当前会话；确定性回归模拟令牌轮换，证明旧凭据被拒绝、新凭据可用，并拒绝另一用户、租户、上下文、主体类型、权限集合以及缺失凭据，不改变产品认证、权限和保护规则。

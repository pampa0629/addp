# Security 模块说明

Security 是 ADDP 数据安全控制面，唯一拥有敏感数据类型、安全分类、安全等级、检测器、发现、资源安全评估、保护基线、策略、保护豁免和保护投影。DetectorCapability 是平台代码提供的只读可信能力，Detector 是 Tenant 把能力绑定到 SensitiveDataType 的版本化启用配置；发现不得通过敏感类型代码或名称猜测绑定。当前已经实现基础定义、显式保护纳管、Owner 投影变化流、acknowledgement 激活/释放屏障，以及手机号元数据/文档和邮箱元数据检测、无原值 Finding、一次性 Finding review、不可变 Assessment/ProtectionPolicy revision、唯一投影编译和显式重新发现/续期。Manager、Develop、Service 已具备自身字段级动作投影，Transfer bounded snapshot 的独立 `export` 动作与 PostgreSQL、MongoDB 原始记录执行器已完成。ProtectionExemption 只绑定正式 Assessment 与一个已实现字段级出口，最长 30 天；投影保留默认决策并携带限时 `allow`，Owner 到期后本地自动回落，不能设置私有豁免。其他未实现执行器的出口、按主体或用途揭示和双人审批属于后续阶段。

## 边界

- 不拥有用户、角色、登录认证或资源授权；这些事实属于 System/IAM 和资源 Owner。
- 不复制 Meta DataItem、CatalogEntry 或 CatalogComponent，不以 Catalog 建档作为安全事实成立的前置条件；Enrollment 只冻结 Engine ID、item type 与 full_name 的最小保护目标快照用于展示和审计，不保存 attributes 或字段事实。
- 不代理数据预览、查询、导出或服务流量；Owner 使用 `common/dataprotection` 在自身服务端执行保护投影。
- `common/secretcipher` 只负责静态敏感配置值加解密，不是 Security 业务模块的一部分。
- 未纳管资源不进入检测、投影或保护路径，不产生额外远程调用和安全审计负担。

## 运行角色

- Backend：端口 `8194`，API 前缀 `/api/v1/security`，维护控制面事实。
- Worker：独立运行角色；通过 `common.task_executions` 领取 `security/sensitive_data_discovery` 有界执行，使用 `addp-security` Tenant Service Access Token 精确读取 Meta 技术事实，并且只对已显式纳管的文档按 fingerprint 读取临时受控正文样本；通用租约过期后按 `max_attempts` 重试或失败收口，不运行定时调度或 TaskProvider。
- Frontend：端口 `5191`，通过 Console iframe 集成；产品入口固定收敛为“分类分级体系”“敏感数据定义”“默认保护规则”“受保护资源”。`/classification-grading` 以可恢复 `tab` 组织低频维护的 SecurityClassification 和 SecurityGrade；`/sensitive-data-definitions` 只组织 SensitiveDataType，并把 Detector 作为对应敏感类型的“识别方式”配置；“受保护资源”只把 acknowledgement 表述为保护规则已安装，不能表述成某个具体请求或该 Owner 所有数据形态均已执行成功。旧的实体级分类、等级和敏感类型路径不恢复，也不保留兼容入口。

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

四个必要 Owner 确认 enrolling 门禁后，Security 在同一事务创建一次 discovery execution。治理人员也可携带 Enrollment `version` 显式创建重新发现执行；同一纳管同时至多一个 pending/running execution。完成后保存最新结构或文本快照并重新编译、续期投影。结构化 DataItem 目前使用 `addp.detector.phone_metadata/v2` 和 `addp.detector.email_metadata/v1` 按字段路径、确定性扁平路径语义和通用类型独立识别；文档使用 `addp.detector.phone_document/v1` 在当次内存样本中识别精确 11 位 ASCII 数字串。检测能力都不持久化原始业务值。Finding 必须通过当前 Tenant 的 Detector 绑定取得 SensitiveDataType 和自动采用置信度；达到绑定阈值且存在有效 ProtectionBaseline 后，唯一编译器对结构化组件为 Manager 生成 `preview` 和系统派生的 `profile` 规则，为 Develop 生成独立 `query` 规则，为 Service 生成独立 `service_execute` 规则，并为 Transfer bounded snapshot 生成独立 `export` 规则；对文档虚拟组件 `$document.text` 只生成 Manager `search_index` 规则。没有当前动作执行器的 Owner 出口继续保持资源级 deny。

`released` Enrollment 永久保存退出审计且不可恢复状态。再次保护只能通过旧记录上的重新纳入命令创建新的 `activating` Enrollment，并重新走四个 Owner 激活屏障；同一目标不允许同时存在两条未退出生命周期。

成功发现后 Enrollment 查询批量返回当前快照 Finding 总数、待复核数和已复核数；Finding 查询使用 `enrollment_id + source_snapshot_hash` 精确限定当前候选，并在分页响应中携带可选不可变初审记录，以及由当前 Detector、Assessment、ProtectionBaseline 和已发布 Projection 批量组装的只读 `explanation`。集中“待复核候选”是 `/protection-enrollments?tab=review-queue` 的子视图，只通过 `GET /findings?snapshot_scope=current&review_state=pending` 聚合未退出 Enrollment 当前 Finding，不新增队列实体或第二套复核状态。前端不自行推导保护结论，解释链不持久化且不含原值。历史快照只用于审计。零命中只表示当前检测能力未发现候选，不编译 `allow` 且继续资源级 deny。治理人员确认当前无需保护时，必须使用唯一 Release 路径并提交 `basis=no_supported_findings`、Enrollment `version` 和原因；服务端校验最近发现已完成、Finding 数为 0 且无在途发现执行，然后冻结退出依据、发起人、时间和依据快照。

识别质量摘要直接从 Finding、不可变 review 和 Assessment 当前修订即时聚合，不建立统计表或双写链。当前候选只取各未退出 Enrollment 最新成功发现；历史人工质量样本按 Enrollment、组件和检测能力版本折叠为最新 review。人工指定只作为可能漏检的线索，不归因于某个 Detector。

创建 Enrollment 的唯一用户输入是 Meta 资源树返回的 DataItem ResourceLocator；Security 自行计算 fingerprint，只纳管完整 DataItem。旧的 fingerprint 与字段路径自由输入路线不存在。字段组件通常由 Detector 发现；自动发现漏检时，治理人员只能从 Security Backend 实时读取并校验、且尚未形成任何正式 Assessment 的 Meta 当前字段清单中选择组件，直接形成来源为 `manual` 的正式 Assessment，不得自由填写字段路径。已存在 Assessment 的组件不再出现在人工指定候选中，其后续调整或撤销必须在既有聚合上追加修订。Finding 误报通过不可变 `reject` review 收口，既有正式 Assessment 错误则追加 `not_sensitive` 修订撤销，二者都由唯一投影编译器重新发布保护结果。

ProtectionPolicy 首期只绑定正式 Assessment + `manager` + `preview`，只能把当前 ProtectionBaseline 收紧为 `mask|suppress|deny`，不复制算法参数、不承载授权或例外。创建、更新和撤销都追加不可变 revision，并在同一事务调用唯一投影编译器；撤销后回落到 Assessment + ProtectionBaseline，不解除纳管。

ProtectionExemption 是原值揭示的唯一入口，固定绑定正式 Assessment + `manager/preview|develop/query|service/service_execute|transfer/export` 中一个动作。每次豁免修订冻结批准时的 Assessment revision；Assessment 后续产生新修订时旧豁免立即失效，不得静默恢复。效果固定为限时 `allow`，作用于 Tenant 内所有已通过 Owner 授权的该动作调用者；最长 30 天，依据必填。创建、续期、撤销都追加不可变 revision 并原子重编译对应 Owner。投影规则必须保留 Policy/Baseline 默认决策作为 `fallback`，Owner 不依赖 Security 在线即可在 `valid_until` 后自动恢复保护。

`manager/profile` 不建立可编辑 Policy：唯一编译器把有效 `preview=mask|suppress` 派生为 `profile=suppress`，把 `preview=deny` 派生为 `profile=deny`。Manager 负责把 `profile=suppress` 执行为整个字段剖析对象的移除，Security 不复制 Manager 指标结构。

ProtectionBaseline 创建、更新、启停、改绑和带 `version` 删除必须根据 Security 自有 Finding/Assessment 依赖精准重编译受影响 Enrollment，并与定义写入保持同一事务；SensitiveDataType 自动发现初始等级变化只重算未复核候选 Finding；Detector 自动采用置信度变化走 Detector 配置变更的有界重新发现路径。正式 Assessment revision 冻结当时的类型、分类和等级，名称或排序等展示变化不制造投影版本。影响解析不得扫描全租户 Enrollment，也不得调用 Meta、Catalog 或 Engine。

Owner 变化流是唯一投影交付路线。Manager、Transfer、Develop、Service 只能使用各自固定 Tenant Service Access Token 拉取自身变化并确认本地原子安装的 cursor；不能提交 consumer owner 或资源清单。

Standard 的旧分类分级 ID 和数据不迁移、不映射，也不提供兼容 API。

## 必读规范

- `docs/concepts/addp数据安全与隐私保护体系图.md`
- `docs/spec/addp数据安全与隐私保护实现规范.md`
- `docs/spec/addp-API设计规范.md`
- `docs/spec/addp-Swagger集成指南.md`

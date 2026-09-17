# Security 模块说明

Security 是 ADDP 数据安全控制面，唯一拥有敏感数据类型、安全分类、安全等级、检测器、发现、资源安全评估、保护基线、策略、原值访问申请、临时原值授权和保护投影。DetectorCapability 是平台代码提供的只读可信能力，Detector 是 Tenant 把能力绑定到 SensitiveDataType 的版本化启用配置；发现不得通过敏感类型代码或名称猜测绑定。当前已经实现基础定义、显式保护纳管、Owner 投影变化流、acknowledgement 激活/释放屏障，以及手机号元数据/文档、邮箱元数据和身份证件号码元数据检测、无原值 Finding、一次性 Finding review、不可变 Assessment/ProtectionPolicy revision、唯一投影编译和显式重新发现/续期。Manager、Develop、Service 已具备自身字段级动作投影，Transfer bounded snapshot 的独立 `export` 动作与 PostgreSQL、MongoDB 原始记录执行器已完成。原值访问只能由当前用户从 Manager 预览出口发起 ProtectionAccessRequest，再由另一名有审批权限的用户在 Security 审批；ProtectionExemption 只保存审批后形成的按用户、按字段、按 `manager/preview` 出口、最长 30 天的临时授权。Projection v2 始终保留默认保护决策并携带主体级限时 `allow`，Owner 到期、撤销、Assessment 修订变化或主体不匹配时本地自动回落。用途约束和其他主体类型属于后续阶段。

## 边界

- 不拥有用户、角色、登录认证或资源授权；这些事实属于 System/IAM 和资源 Owner。
- 当前所有 active Security Permission 只允许 `tenant` Scope；管理 API 只消费 Tenant Context，不把 Department 或 Project Group Assignment 当作资源过滤。租户治理使用 `tenant.security_manager`，原值申请使用 `tenant.protected_data_requester` 或等价的 Tenant-only 自定义 Role。未来开放组织级授权前必须先在规范中建立可信的资源归属、Scope Binding、显式 Deny 与 owner 最终策略校验契约。
- 不复制 Meta DataItem、CatalogEntry 或 CatalogComponent，不以 Catalog 建档作为安全事实成立的前置条件；Enrollment 只冻结 Engine ID、item type 与 full_name 的最小保护目标快照用于展示和审计，不保存 attributes 或字段事实。
- 不代理数据预览、查询、导出或服务流量；Owner 使用 `common/dataprotection` 在自身服务端执行保护投影。
- `common/secretcipher` 只负责静态敏感配置值加解密，不是 Security 业务模块的一部分。
- 未纳管资源不进入检测、投影或保护路径，不产生额外远程调用和安全审计负担。
- Manager、Develop、Service、Transfer 的必要 Owner 集合及各自字段级主动作只能在 `internal/service` 的 Owner 契约中定义一次；Enrollment 创建、全量重编译、变化流校验和历史投影升级均从该契约派生，不得重复写 Owner 切片或 action 分支。当前可申请原值访问的出口另行严格限定为 `manager/preview`，不能从 Owner 主动作集合自动扩张。

## 运行角色

- Backend：端口 `8194`，API 前缀 `/api/v1/security`，维护控制面事实。
- Worker：独立运行角色；通过 `common.task_executions` 领取 `security/sensitive_data_discovery` 有界执行，使用 `addp-security` Tenant Service Access Token 精确读取 Meta 技术事实，并且只对已显式纳管的文档按 fingerprint 读取临时受控正文样本；通用租约过期后按 `max_attempts` 重试或失败收口，不运行定时调度或 TaskProvider。
- Frontend：端口 `5191`，通过 Console iframe 集成；产品入口固定收敛为“分类分级体系”“敏感数据定义”“受保护资源”。`/classification-grading` 以可恢复 `tab` 组织低频维护的 SecurityClassification 和 SecurityGrade；`/sensitive-data-definitions` 以 SensitiveDataType 为主视图，把 Detector 作为“识别方式”、把 ProtectionBaseline 作为“默认保护”收进对应敏感类型，不再保留 `/protection-baselines` 产品路由；“受保护资源”统一承载 Assessment 调整、按需读取的不可变修订历史与只能收紧的 ProtectionPolicy 编辑，并把 acknowledgement 表述为保护规则“待同步/已同步”，不使用“待安装/已安装”或“已生效”，也不能表述成某个具体请求或该 Owner 所有数据形态均已执行成功。旧的实体级分类、等级、敏感类型和独立保护基线页面路径不恢复，也不保留兼容入口。

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

四个必要 Owner 确认 enrolling 门禁后，Security 在同一事务创建一次 discovery execution。治理人员也可携带 Enrollment `version` 显式创建重新发现执行；同一纳管同时至多一个 pending/running execution。完成后保存最新结构或文本快照并重新编译、续期投影。结构化 DataItem 目前使用 `addp.detector.phone_metadata/v2`、`addp.detector.email_metadata/v1` 和 `addp.detector.identity_document_number_metadata/v1` 按字段路径、确定性扁平路径语义和通用类型独立识别；文档使用 `addp.detector.phone_document/v1` 在当次内存样本中识别精确 11 位 ASCII 数字串。身份证件号码元数据能力只识别精确字段语义，不读取或校验业务值；结构化字段统一使用 `addp.mask.keep_prefix_suffix/v2` 按实际 Unicode 长度遮盖，不在算法中猜测手机号、邮箱或证件号码格式。检测能力都不持久化原始业务值。Finding 必须通过当前 Tenant 的 Detector 绑定取得 SensitiveDataType 和自动采用置信度；达到绑定阈值且存在有效 ProtectionBaseline 后，唯一编译器对结构化组件为 Manager 生成 `preview` 和系统派生的 `profile` 规则，为 Develop 生成独立 `query` 规则，为 Service 生成独立 `service_execute` 规则，并为 Transfer bounded snapshot 生成独立 `export` 规则；对文档虚拟组件 `$document.text` 只生成 Manager `search_index` 规则。没有当前动作执行器的 Owner 出口继续保持资源级 deny。

`released` Enrollment 永久保存退出审计且不可恢复状态。再次保护只能通过旧记录上的重新纳入命令创建新的 `activating` Enrollment，并重新走四个 Owner 激活屏障；同一目标不允许同时存在两条未退出生命周期。

成功发现后 Enrollment 查询批量返回当前快照 Finding 总数、待复核数和已复核数；Finding 查询使用 `enrollment_id + source_snapshot_hash` 精确限定当前候选，并在分页响应中携带可选不可变初审记录，以及由当前 Detector、Assessment、ProtectionBaseline 和已发布 Projection 批量组装的只读 `explanation`。当前组件结构仍匹配正式 Assessment 时，`explanation.assessment_id` 在 `sensitive` 与 `not_sensitive` 结论下都指向同一精确聚合，供“受保护资源”继续追加修订；组件结构冲突时不返回该关联，前端不得按字段名猜测。集中“待复核候选”是 `/protection-enrollments?tab=review-queue` 的子视图，只通过 `GET /findings?snapshot_scope=current&review_state=pending` 聚合未退出 Enrollment 当前 Finding，不新增队列实体或第二套复核状态。前端不自行推导保护结论，解释链不持久化且不含原值。历史快照只用于审计。零命中只表示当前检测能力未发现候选，不编译 `allow` 且继续资源级 deny。治理人员确认当前无需保护时，必须使用唯一 Release 路径并提交 `basis=no_supported_findings`、Enrollment `version` 和原因；服务端校验最近发现已完成、Finding 数为 0 且无在途发现执行，然后冻结退出依据、发起人、时间和依据快照。

识别质量摘要直接从 Finding、不可变 review 和 Assessment 当前修订即时聚合，不建立统计表或双写链。当前候选只取各未退出 Enrollment 最新成功发现；历史人工质量样本按 Enrollment、组件和检测能力版本折叠为最新 review。人工指定只作为可能漏检的线索，不归因于某个 Detector。

创建 Enrollment 的唯一用户输入是 Meta 资源树返回的 DataItem ResourceLocator；Security 自行计算 fingerprint，只纳管完整 DataItem。旧的 fingerprint 与字段路径自由输入路线不存在。字段组件通常由 Detector 发现；自动发现漏检时，治理人员只能从 Security Backend 实时读取并校验、且尚未形成任何正式 Assessment 的 Meta 当前字段清单中选择组件，直接形成来源为 `manual` 的正式 Assessment，不得自由填写字段路径。已存在 Assessment 的组件不再出现在人工指定候选中，其后续调整或撤销必须在既有聚合上追加修订。Finding 误报通过不可变 `reject` review 收口，既有正式 Assessment 错误则追加 `not_sensitive` 修订撤销，二者都由唯一投影编译器重新发布保护结果。

ProtectionPolicy 首期只绑定正式 Assessment + `manager` + `preview`，只能把当前 ProtectionBaseline 收紧为 `mask|suppress|deny`，不复制算法参数、不承载授权或例外。创建、更新和撤销都追加不可变 revision，并在同一事务调用唯一投影编译器；撤销后回落到 Assessment + ProtectionBaseline，不解除纳管。

ProtectionAccessRequest 是原值访问的唯一入口，只能由当前可信 User AuthContext 从 Manager 预览出口针对正式敏感 Assessment 发起，正文不得指定主体；申请本身不改变保护效果。申请状态固定为 `pending|approved|rejected|expired`，由 Security 按服务端时间返回有效状态；超过截止时间的未决策申请不再属于待审批，且不阻止申请人重新提交。唯一审批工作区使用 `scope=pending|history` 分别展示未过期待审批申请与已批准、已驳回、已过期历史，必须服务端分页，并可按申请人、资源或字段、申请时间筛选；历史记录还可按处理结果以及当前 `authorization_state=active|expired|revoked|superseded` 筛选，当前授权筛选必须在分页和总数统计前按当前 Exemption、Assessment revision 与服务端时间完成。批准历史必须另外返回当前授权状态与原授权截止时间，不能把“已批准”误作当前仍可访问原值。审批响应返回权威 `enrollment_id`，前端据此复用既有受保护资源详情和授权撤销路径，不按资源名称猜测、不复制第二套授权管理交互。本人待审申请可见但不可自审。申请人和审批人的 Principal ID 是权威审计身份；Security 只在申请或审批动作发生时通过 System 受信租户用户引用接口校验身份并固化显示名快照，审批读取不实时跨模块查询，服务启动会为旧记录补齐一次快照。另一名具有审批权限的用户在截止时间前批准后，Security 才创建或重新激活绑定 `{assessment, manager, preview, user}` 的 ProtectionExemption 并追加不可变 revision；拒绝不产生授权，申请人与审批人相同或申请已过期均冲突。Assessment 后续产生新修订时旧授权立即失效，不得静默恢复。授权效果固定为限时 `allow`，最长 30 天且不得超过申请期限，业务依据和审批理由必填。Projection v2 的规则始终保留 Policy/Baseline 默认 `decision`，主体授权放在 `authorizations`；Owner 必须从服务端可信 AuthContext 匹配 User，不能接受浏览器提交主体。临时授权只能提前撤销，不能直接创建、续期或重新启用；需要继续访问时必须重新申请和审批。

`manager/profile` 不建立可编辑 Policy：唯一编译器把有效 `preview=mask|suppress` 派生为 `profile=suppress`，把 `preview=deny` 派生为 `profile=deny`。Manager 负责把 `profile=suppress` 执行为整个字段剖析对象的移除，Security 不复制 Manager 指标结构。

创建 SensitiveDataType 必须同时提交其自动发现初始等级的完整默认保护，并在同一事务创建有效 ProtectionBaseline；不允许创建“已定义但无默认保护”的半成品。SensitiveDataType 更换自动发现初始等级前，新组合必须已存在有效 ProtectionBaseline；当前初始等级对应的 ProtectionBaseline 不得停用、改绑或单独删除。其他等级的 ProtectionBaseline 创建、更新、启停、改绑和带 `version` 删除必须根据 Security 自有 Finding/Assessment 依赖精准重编译受影响 Enrollment，并与定义写入保持同一事务；SensitiveDataType 自动发现初始等级变化只重算未复核候选 Finding；Detector 自动采用置信度变化走 Detector 配置变更的有界重新发现路径。正式 Assessment revision 冻结当时的类型、分类和等级，名称或排序等展示变化不制造投影版本。影响解析不得扫描全租户 Enrollment，也不得调用 Meta、Catalog 或 Engine。人工确认或调整 Assessment 时，目标 SensitiveDataType + SecurityGrade 也必须已有有效 ProtectionBaseline；`baseline_missing` 仅作为存量异常或一致性故障的失效关闭观测状态，不是正常配置流程。

Owner 变化流是唯一投影交付路线。Manager、Transfer、Develop、Service 只能使用各自固定 Tenant Service Access Token 拉取自身变化并确认本地原子安装的 cursor；不能提交 consumer owner 或资源清单。

Standard 的旧分类分级 ID 和数据不迁移、不映射，也不提供兼容 API。

## 前端验证与失败取证

重新纳入保护的 PostgreSQL 生命周期回归使用带纳秒尾数的固定时间夹具，先校验落库后的微秒精度，再以操作前读回的审计时间作为精确比较基线；不得把数据库读回值直接与 `time.Now()` 的纳秒输入比较，也不能通过放宽时间误差来掩盖退出审计被修改。该回归继续由 `make test-security-postgres` 和现有 `security-postgres` CI Job 执行。

`make test-security-postgres` 除迁移与服务事务外，还运行默认保护 HTTP 契约集成回归：使用真实 Handler、Service 和 PostgreSQL，验证敏感定义与初始规则原子创建、非法规则无残留、完整写响应与递增版本、更新和删除冲突的稳定错误码、显式重读后再次写入、初始规则删除限制及租户隔离。测试仅在标准测试库事务中重建 `security` schema，结束回滚；身份上下文是可信测试夹具，不代表真实登录、权限中间件或 Gateway 已完成联调。CI 继续由 `release-and-t2-gates.yml` 的 `security-postgres` Job 调用同一入口；真实跨模块验收仍归独立的 Online T4。

表单弹窗只在开始新会话（`open`）或用户显式加载最新基线后清除旧校验；`opened` 和焦点恢复只负责聚焦，不得清除动画期间已经产生的必填提示。浏览器回归通过延长打开动画、在动画结束前提交空表单来验证提示持续可见。

候选复核的会话边界由 `useProtectionFindingReview.prepare` 统一拥有：首次打开、重新打开和成功后连续复核下一个候选都在准备新候选时清理旧校验；关闭或销毁后忽略待执行的界面初始化。`FindingReviewForm` 的 `clearValidate` 与 `focusRationale` 分离，挂载和弹窗 `opened` 只聚焦。不得让聚焦方法隐含表单重置，也不得通过延迟用户提交或增加测试重试规避动画竞态。现有 `make test-security-frontend` 与 Platform CI 同时覆盖动画前空提交、动画后提示保留、重开清理和连续复核会话，不新增测试入口或环境依赖。

标准入口为 `make test-security-frontend`，依次运行确定性测试、Playwright 浏览器回归和构建。浏览器测试保持零重试，失败时保留截图与 `trace.zip`，成功时不保留轨迹。结果存放于 `RUNNER_TEMP`（CI）或系统临时目录（本地）下的 `addp-security-playwright-results`；下一次运行会清理该结果目录，需要进一步分析时应先复制失败产物。

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

## 数据库启动所有权

本模块 schema 迁移仅由 Backend 执行，Worker 只读校验成功提交的 schema 版本。Backend 多实例通过模块级数据库锁协调；开发脚本及 Compose 在所属 Backend 就绪后启动 Worker。结构或初始化迁移变化须递增模块 `SchemaVersion`，共享规则见 `docs/spec/addp开发服务生命周期与构建身份规范.md`。

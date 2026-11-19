# Security 模块说明

Security 是 ADDP 数据安全控制面，唯一拥有敏感数据类型、安全分类、安全等级、检测器、发现、资源安全评估、保护基线、策略、原值访问申请、临时原值授权和保护投影。DetectorCapability 是平台代码提供的只读可信能力，Detector 是 Tenant 把能力绑定到 SensitiveDataType 的版本化启用配置；发现不得通过敏感类型代码或名称猜测绑定。当前已经实现基础定义、显式保护纳管、Owner 投影变化流、acknowledgement 激活/释放屏障，以及手机号元数据/文档、邮箱、身份证件号码和银行卡号元数据检测、无原值 Finding、一次性 Finding review、不可变 Assessment/ProtectionPolicy revision、唯一投影编译、显式重新发现和资源级有效期设置。Manager、Develop、Service 已具备自身字段级动作投影，Transfer bounded snapshot 的独立 `export` 动作与 PostgreSQL、MongoDB 原始记录执行器已完成。当前用户从 Manager 或 Develop 复用共享交互发起 ProtectionAccessRequest，显式选择 Manager 预览、Develop 查询或两者；另一名有审批权限的用户一次审批，原子形成各精确出口的 ProtectionExemption。授权仍按用户、按字段、最长 30 天，已有 Manager 授权不扩大。Projection v5 始终保留默认保护决策并携带主体级限时 `allow`，Owner 到期、撤销、Assessment 修订变化或主体不匹配时本地自动回落。用途约束和其他主体类型属于后续阶段。

[模块边界](#边界) · [运行角色](#运行角色) · [启动迁移](#数据库启动所有权) · [缓存与角色验收](#缓存角色资格与文件交付验收) · [早期保护验收](#早期保护验收结论与缺口)

## 边界

- 不拥有用户、角色、登录认证或资源授权；这些事实属于 System/IAM 和资源 Owner。
- 当前所有 active Security Permission 只允许 `tenant` Scope；管理 API 只消费 Tenant Context，不把 Department 或 Project Group Assignment 当作资源过滤。租户治理使用 `tenant.security_manager`，原值申请使用 `tenant.protected_data_requester` 或等价的 Tenant-only 自定义 Role。未来开放组织级授权前必须先在规范中建立可信的资源归属、Scope Binding、显式 Deny 与 owner 最终策略校验契约。
- 不复制 Meta DataItem、CatalogEntry 或 CatalogComponent，不以 Catalog 建档作为安全事实成立的前置条件；Enrollment 只冻结 Engine ID、item type 与 full_name 的最小保护目标快照用于展示和审计，不保存 attributes 或字段事实。
- 不代理数据预览、查询、导出或服务流量；Owner 使用 `common/dataprotection` 在自身服务端执行保护投影。
- `common/secretcipher` 只负责静态敏感配置值加解密，不是 Security 业务模块的一部分。
- 未纳管资源不进入检测、投影或保护路径，不产生额外远程调用和安全审计负担。
- [Owner 契约](backend/internal/service/protection_owner_contract.go)统一定义四个字段级策略出口 `manager/preview`、`develop/query`、`service/service_execute`、`transfer/export`，以及包含 Graph 的五个必要 Owner。创建与重新纳管、全量重编译、变化流校验和历史投影升级均从该契约派生。Graph 消费 `graph_read` 保守拒绝投影，也须确认当前门禁和 release。运行时以固定 Tenant Service Access Token 绑定当前 Tenant 与 Owner。原值申请只可显式选择 `manager/preview`、`develop/query`。[纳管状态与回执边界](../docs/spec/addp数据安全与隐私保护实现规范.md#五纳管状态机与激活屏障)

保护投影自身到期与用户临时原值授权到期分别验证：投影到期必须拒绝相关输出，授权到期仅回落默认保护。Common 查询／原生表的实际输出保护器及 Manager 普通预览响应必须重新校验本次读取绑定投影的有效期，空结果也不能输出过期字段结构。四个字段级数据出口的指定时点回归由 Go T1 和 `make test-common-postgres` 覆盖，Transfer 真实目标零新增写入由既有 `make test-transfer-postgres` 覆盖；历史协议夹具保留 365 天有效期，沿正式变化流安装，不改变产品有效期或本地系统时钟。同一回归还覆盖更高修订替换到期投影：旧读取继续拒绝，其他存储实例刷新持久 cursor 后的新读取执行新保护决策，重新打开存储后保持修订、纳管状态与保护输出；不增加自动续期路径。这些证据属于 T1/T2，不代替长周期到期 T4。

## 运行角色

敏感数据发现的过程事件复用 `common.execution_events`：实际 claim 与 `started` 同事务，成功时与 Finding、Enrollment 当前来源和保护投影一起提交 `completed`，既有失败及租约重试耗尽分别提交唯一 `failed`。错配或过期 lease 不得追加，事件写入失败回滚对应事务。仍按现有 `max_attempts` 恢复：回到 pending 时不伪造 execution 终态事件，新 attempt 实际 claim 后记录自己的 started。心跳不产生事件；本切片不新增百分比或 Runtime 步骤，不保存字段、样本、Detector 配置、专业结果或授权信息。读取复用已有 Owner 读取裁决，30 天事件维护不改变专业证据、执行概览或审计生命周期。验证沿 `make test-module MODULE=security GO_ONLY=1` 和 `make test-security-postgres`；新增 `TestDiscoveryExecutionEventsAgainstPostgres` 纳入后者既有服务测试筛选，既有 Go／Security PostgreSQL CI 自动覆盖。确定性测试不能代替真实 Gateway／Worker／Monitor T4。

- Backend：端口 `8194`，API 前缀 `/api/v1/security`，维护控制面事实。
- Worker：独立运行角色；通过 `common.task_executions` 领取 `security/sensitive_data_discovery` 有界执行，使用 `addp-security` Tenant Service Access Token 精确读取 Meta 技术事实，并且只对已显式纳管的文档按 fingerprint 读取临时受控正文样本；通用租约过期后按 `max_attempts` 重试或失败收口，不运行定时调度或 TaskProvider。
- Frontend：端口 `5191`，通过 Console iframe 集成；产品入口固定收敛为“分类分级体系”“敏感数据定义”“受保护资源”。`/classification-grading` 以可恢复 `tab` 组织低频维护的 SecurityClassification 和 SecurityGrade；`/sensitive-data-definitions` 以 SensitiveDataType 为主视图，把 Detector 作为“识别方式”、把 ProtectionBaseline 作为“默认保护”收进对应敏感类型，不再保留 `/protection-baselines` 产品路由；“受保护资源”统一承载 Assessment 调整、按需读取的不可变修订历史与只能收紧的 ProtectionPolicy 编辑，并把 acknowledgement 表述为保护规则“待同步/已同步”，不使用“待安装/已安装”或“已生效”，也不能表述成某个具体请求或该 Owner 所有数据形态均已执行成功。旧的实体级分类、等级、敏感类型和独立保护基线页面路径不恢复，也不保留兼容入口。

独立前端根路径在 AuthContext 加载后，按 Console 菜单顺序 `/classification-grading`、`/sensitive-data-definitions`、`/protection-enrollments` 选择第一个符合页面准入条件的地址；没有可访问页面时进入 `/forbidden`。登录后的默认返回地址为根路径，显式业务地址仍按自身权限判断。

Manager 原值申请目标的 `projection_acknowledged` 复用现有 Owner 回执，表示当前资源投影已同步；不改变 `approved` 审批结论或当前主体授权语义。申请目标从同一只读一致性快照读取投影、审批、Assessment、授权和回执。页面在已批准但待同步时继续轮询，确认后刷新预览；数据面仍仅消费本地投影。

临时授权列表、详情和撤销响应直接返回由同租户绑定 Assessment 派生的 `component_key`。授权卡片仅消费该字段，不依赖可选 Assessment 列表；持有授权读取权限的审批用户无需取得 Assessment 读取权限即可识别授权字段。组件键只存在于响应 DTO，不新增持久事实或权限。

## 数据库

Security 的领域事实存于 PostgreSQL `security` schema。下表名称均以 `security.` 为前缀，与 [领域模型](backend/internal/models/definitions.go)和 [Backend 迁移入口](backend/internal/repository/schema.go)对应。发现执行与过程事件使用共享执行表；Owner 本地投影和 cursor 由各自 schema 内的共享投影存储维护。启动职责见[数据库启动所有权](#数据库启动所有权)。

| 持久事实 | 数据表 |
| --- | --- |
| 定义与默认保护 | `security_classifications`、`security_grades`、`sensitive_data_types`、`detectors`、`protection_baselines` |
| 纳管与投影交付 | `protection_enrollments`、`protection_projections`、`protection_projection_changes`、`protection_projection_acknowledgements` |
| 发现、复核与正式评估 | `sensitive_findings`、`sensitive_finding_reviews`、`resource_security_assessments`、`resource_security_assessment_revisions` |
| 字段策略 | `protection_policies`、`protection_policy_revisions` |
| 原值申请与授权 | `protection_access_requests`、`protection_exemptions`、`protection_exemption_revisions` |

### 纳管、发现与复核

- PostgreSQL 部分唯一索引限制同租户、同完整目标最多一条未 `released` 的纳管。重新纳入时锁定已退出来源并在同事务推进其版本、创建新的 `activating` 记录和初始门禁；旧退出状态与审计保留。带版本的零命中退出必须在同一事务复核最近发现成功、当前快照零 Finding 且没有在途执行。[纳管状态机与退出规则](../docs/spec/addp数据安全与隐私保护实现规范.md#五纳管状态机与激活屏障)
- 初始门禁确认与首次发现入队同事务。同一纳管最多一个 `pending`／`running` 发现；成功提交当前来源快照、Finding 与重编译投影，保留资源截止时间。Finding 以实际发现执行、组件、检测器版本及来源快照区分观测，持久化证据不含业务原值。检测器绑定、适用类型、图层属性和文档正文的边界以正式规则为准。[敏感发现执行](../docs/spec/addp数据安全与隐私保护实现规范.md#六敏感发现执行)
- 当前快照的 Finding 计数、待复核聚合、只读 `explanation` 与识别质量摘要由已有事实生成，复核队列、解释链和统计均无独立表。说明中的 Assessment 关联须匹配当前组件结构。误报保留不可变初审，正式结论调整在既有 Assessment 上追加修订；人工指定只取尚无正式 Assessment 的可信当前组件。[Finding、Assessment 与解释投影](../docs/spec/addp数据安全与隐私保护实现规范.md#七findingassessment-与唯一策略编译器)

### 定义、策略与授权事务

- 新 SensitiveDataType 与其初始等级的有效 ProtectionBaseline 原子创建。定义变更用 Security 自有事实精确定位依赖，将受影响投影与定义写入同事务提交。Assessment 修订冻结当时的类型、分类和等级，展示变更不制造新投影。字段策略按 Assessment 和精确出口追加不可变修订，与对应 Owner 的新投影同事务；`manager/profile` 仍由编译器派生。[保护定义影响](../docs/spec/addp数据安全与隐私保护实现规范.md#73-保护定义变化的影响传播) · [字段策略](../docs/spec/addp数据安全与隐私保护实现规范.md#71-protectionpolicy-首期语义) · [剖析派生规则](../docs/spec/addp数据安全与隐私保护实现规范.md#101-manager-profile-动作)
- 同租户、Assessment 与主体的持久 `pending` 申请受部分唯一索引约束。待审批和历史读取按服务端时间认定有效申请状态，再按当前各出口授权状态筛选和分页。本人不可自审。审批结论、所有所选出口的授权修订和 Projection v5 更新同事务，当前授权与原批准结论分别呈现。申请人及审批人的 Principal ID 是审计身份，显示名在动作发生时经 System 可信租户用户引用固化，Backend 启动补齐缺失快照；审批读取使用已存快照。[申请、审批与授权](../docs/spec/addp数据安全与隐私保护实现规范.md#72-protectionaccessrequest-与-protectionexemption)

### 投影与回执

Security 写入当前投影修订时，同事务追加该 Owner 的变化记录。消费只使用固定 Tenant Service Access Token，确认本地持久安装后的 cursor。激活及退出以 [Owner 契约](backend/internal/service/protection_owner_contract.go)中的全部必要 Owner 为准，含 Graph 的独立屏障。旧回执无法确认新修订，重复确认不推进 Enrollment 版本或重复入队。SQLite T1 和 `make test-security-postgres` 的生命周期回归验证该屏障，真实 Owner、Gateway 或浏览器链路按独立 Online 验收认定。[变化流及回执](../docs/spec/addp数据安全与隐私保护实现规范.md#九投影变化流与-owner-确认) · [Graph 引擎范围与保护](../docs/spec/addp数据安全与隐私保护实现规范.md#查询保护的引擎范围排除)

Standard 旧分类分级 ID 和数据不迁移、不映射，既有兼容入口已移除。[迁移与旧路径删除](../docs/spec/addp数据安全与隐私保护实现规范.md#十五迁移与旧路径删除)

## 前端验证与失败取证

### 标准门禁与失败产物

`make test-security-frontend` 依次运行确定性测试、Playwright 回归和构建。浏览器零重试，失败保留截图和 `trace.zip`。当轮结果独立保存到系统临时目录的 `addp-security-playwright-results-*`（CI 的 `TMPDIR=RUNNER_TEMP`），成功退出删除当轮目录，后续运行不覆盖此前失败产物。Platform CI 的 Security 前端失败产物为 `security-browser-failure-<run_id>-<run_attempt>`，保留 14 天。解压后在 `security/frontend` 运行 `npx playwright show-trace <trace.zip绝对路径>`，核对操作时序、DOM、网络和控制台。偶发故障的结论以首次失败取证和修复后的针对性回归为准。

### 表单会话与保存

- 新会话打开或用户显式加载最新基线时清除旧校验。默认保护编辑取消、Esc 或遮罩退出时恢复原值、清除校验并保留父抽屉；Esc／遮罩关闭后在 `closed` 恢复触发按钮的焦点。打开动画结束时，`opened` 只负责聚焦，并保留动画期间产生的必填提示。定向回归在 `security/frontend` 使用 `npm run test:e2e -- --grep 'default protection editor'`，标准前端门禁也覆盖。
- 候选复核由 `useProtectionFindingReview.prepare` 统一开启新会话，覆盖首次打开、重开和成功后复核下一项。关闭或销毁后忽略待执行的界面初始化。`FindingReviewForm` 的清校验与聚焦分开，挂载和 `opened` 只负责聚焦，测试实际覆盖动画前提交与动画后提示保留。
- 从校验到写入和刷新结束使用同一 `saving` 状态，期间禁止重复提交、编辑及关闭弹窗。写入失败解除限制，保留草稿，等待用户重试。提示只展示标准错误体非空字符串 `error` 的普通文本；网络错误或无有效原因时使用 `security.common.failed`。诊断 `detail` 和 Axios 通用错误不向表单回显。

### 刷新、版本冲突与展示快照

- 创建或更新成功先采用完整写入响应与新 `version`，删除成功移除对应本地规则，刷新完成后再关闭弹窗。写入成功但读取刷新失败，应持久提示“变更已保存”并提供只读重试，刷新前禁止基于旧列表编辑。初次加载失败保留错误状态。
- `resource_version_conflict` 保留草稿并阻止按旧版本重提，只有用户显式放弃输入并成功加载最新版才更新编辑基线。加载失败保留草稿，卸载后忽略迟到响应。
- 默认保护工作区以敏感类型为范围发布最新展示快照，供父页面更新摘要。写入响应、显式冲突重载及列表读取成功均更新快照，父页面直接消费快照。

### 验收入口

`make test-security-postgres` 的默认保护 HTTP 回归使用真实 Handler、Service 与标准测试库事务，覆盖初始规则原子创建、非法规则无残留、完整写响应与版本递增、更新／删除冲突、显式重读后的再写、初始规则删除限制及租户隔离。回归结束回滚，身份是测试夹具。重新纳管审计按 PostgreSQL 实际落库的微秒精度，以操作前读回时间精确对照，防止放宽误差后漏检退出审计变更。现有 `security-postgres` CI Job 调用同一入口。

真实跨模块入口见[控制面不可达场景](../docs/spec/testing/scenarios/数据保护.md#security-控制面实际不可达)、[重新发现与安装回执](../docs/spec/testing/scenarios/数据保护.md#重新发现与安装回执)。固定结果分别见[四出口停机 v9](../docs/spec/testing/evidence/数据保护与执行血缘历史证据.md#四出口实际停机--t4-v9)、[四出口重新发现 v10](../docs/spec/testing/evidence/数据保护与执行血缘历史证据.md#四出口重新发现--t4-v10)与[双用户限时原值](../docs/spec/testing/evidence/数据保护与执行血缘历史证据.md#双用户限时原值与控制面不可达)。MongoDB 搬运沿[实际场景及早期证据](../docs/spec/testing/scenarios/数据保护.md#security-场景最小历史证据)，稳定错误、完成事务及安全 DTO 见[Transfer bounded 失败诊断](../transfer/CLAUDE.md#bounded-失败诊断)。

## 必读规范

- `docs/concepts/addp数据安全与隐私保护体系图.md`
- `docs/spec/addp数据安全与隐私保护实现规范.md`
- `docs/spec/addp-API设计规范.md`
- `docs/spec/addp-Swagger集成指南.md`

普通账号资源组合筛选 T4 复用现有 Console 登录与 `security-mysql-owner-protection` 浏览器流程：使用同一普通 User 的正式 AuthContext，逐项核对名称、引擎、生命周期状态及列表范围的请求参数、返回成员、分页总数和实际表格。覆盖正确组合、错误引擎、保护中与已停止保护状态的空交集、百分号字面匹配、范围切换及重置恢复；当前四出口回执、有效期和页面截止时间同时核对。筛选阶段仅允许 Security GET，不创建或修改纳管、策略、授权和角色。跨模块整页导航后，验收 API 必须从本次浏览器成功业务响应的 Authorization 绑定当前会话，并重新核对同一 User、Tenant 和完整权限集合；禁止继续使用导航前捕获的旧令牌，也不回退准备身份。新增三张筛选截图，浏览器报告升级为 v4、场景报告升级为 v12，严格校验新增字段；既有客户检索、预览、CSV、Monitor、银行卡和停机回归保持完整；沿现有前端、Online runner 及 Hosted 入口发现，1200 秒总预算不变。普通账号组合筛选 T4 结论须以完整报告及截图的独立核对为依据，真实结果见下文记录。

## 数据库启动所有权

本模块 schema 迁移仅由 Backend 执行，Worker 只读校验成功提交的 schema 版本。Backend 多实例通过模块级数据库锁协调。开发脚本和 Compose 在所属 Backend 就绪后启动 Worker。结构或启动迁移变化须递增模块 `SchemaVersion`。共享规则见[开发服务生命周期与构建身份规范](../docs/spec/addp开发服务生命周期与构建身份规范.md)。

结构化投影启动补齐只处理 `enrolling` 和 `active`，先锁定并核验 Enrollment 生命周期，再解析 Owner 投影。已解除的投影保留作审计，升级时不得为已解除资源补建 Graph 投影或恢复保护。既有期限回填取原 Owner 最早截止时间，保留各投影原日期、修订与 feed 位置，重复运行不延期、不改变资源版本。旧协议转换、重放和初始化以[现行 Projection v5 契约](../docs/spec/addp数据安全与隐私保护实现规范.md#八protection-projection-v5)为准。期限回填由 SQLite 与 `make test-security-postgres` 覆盖，启动补齐另有 Security Go 与 PostgreSQL 生命周期回归。

## 缓存、角色资格与文件交付验收

端点响应以[安全实现规范](../docs/spec/addp数据安全与隐私保护实现规范.md#711-默认方式与角色可选方式)为准。Manager 预览、Service 结构化查询以及 Manager／Develop 用户导出创建、状态和文件在业务 Handler 内返回单一 `Cache-Control: no-store`，涵盖成功和业务拒绝。Gateway 或认证中间件在 Handler 前拦截的响应，以及瓦片、普通文件、派生缓存，分别按各自契约认定。

### 缓存响应的本机核验

| 固定构建 | 实际结果与边界 |
| --- | --- |
| Manager／Service `d8aad1735df11220472892cac6b9d5fe72650efd`，含修复 `9e9de5a5361adc8874363ef414fa453b0f313750` | 正式第一方普通 User 经 Gateway 核对 MySQL 五行 Manager 预览、Service 首五行 JSON／CSV 和一次五行 JSON 续页，以及 400／404／503 拒绝，共七项单一 `no-store`。首次审计将按规则省略的 phone 键误判为缺字段，修正后复验，保留失败原件。私有服务 #6 经版本控制临时启用，结束恢复停用（v29），选择为空。未验 GeoJSON、完整续页、角色到期、浏览器或 Hosted。证据 `/tmp/p145-data-response-http-20261027/`。 |
| Security／Manager／Develop／Transfer `fad6a9bd68d20ad82cf202c6bc05b99e1ac040dc`，含修复 `76b87598bc224b393d1a9653f8b38e51bab7fc25` | MySQL 导出创建、状态与文件的 19 项真实 Gateway 响应均为单一 `no-store`，包括 5 次 200、2 次 202、6 次 400、4 次 404、2 次方式失效 409。实际下载 Manager 2,188 行和 Develop 5 行，两份 CSV 分别逐值审计，共同五行一致。默认导出由模块代发，仅记录 `triggered_by` 的人类来源，Actor／Membership／授权版本／EA 为空。首次日志审计误收 JSON 布尔值，修正后复验。未验真实角色自然到期、新版旧特殊会话交付或浏览器／Hosted。证据 `/tmp/p145-export-cache-http-20261026/`。 |

两轮正式 User 均已注销。相关 Go T1 和 Swagger 路由覆盖通过。本机业务响应与物理 CSV 只证明各自固定构建的实际范围。

### 方式撤销、资格撤销与自然到期

| 历史场景 | 独有断言与当时的证据 |
| --- | --- |
| Service 方式撤销 v26 | 新请求与旧游标分别验证方式失效 409，旧选择仍可直接停用，显式移除后恢复默认五行。普通 User 本机旧游标已有验收，Runner 通过。[本机验收](../service/docs/testing/查询与要素协议.md#角色方式撤销后的旧游标本地验收) · [隔离场景](../scripts/docs/Online数据保护.md#service-角色方式撤销后的旧游标) |
| Service 资格撤销 v27／自然到期 v28 | 撤销时旧会话 401 单列，正式同用户重登后旧游标与无游标返回 `403/query_protection_roles_unavailable`。自然到期使用 90 秒 Assignment，保持同一会话及功能权限，仅资格角色到期。两种均须显式移除旧选择才恢复默认。两版当时均通过 Runner。[资格撤销](../scripts/docs/Online数据保护.md#用户角色资格撤销) · [自然到期](../scripts/docs/Online数据保护.md#角色资格自然到期) |
| Develop 自然到期 v29 | 同一 UUID 的角色实时结果与默认历史分开。到期后的旧实时结果返回资格 409；保留选择的新执行在准备阶段拒绝，同一历史无结果，旧成功历史仍默认脱敏。显式清空后新查询恢复，Runner 通过。[Develop 自然到期](../scripts/docs/Online数据保护.md#develop-角色资格自然到期) |
| Transfer／Develop 导出 v30–v32 | Transfer 原任务启动、更新和选择复核返回资格 409，执行数和物理目标保持。导出旧状态、旧文件、原查询再次导出及默认恢复后旧文件复读均拒绝。v30 的 BFF 合并为方式失效；v31 起区分 `query_protection_roles_unavailable` 和 `query_protection_choice_invalid`，下载前另核对冻结 Transfer UUID、本人触发、无定义的手动执行及五行读写。v32 补严格的单一 `no-store`，涵盖前后两组成功 CSV 的创建／状态／文件和四个拒绝动作。显式清空后新文件恢复，旧特殊文件仍拒绝。[Transfer 资格与旧文件](../scripts/docs/Online数据保护.md#transfer-资格自然到期与旧文件) |

v31 补查执行来源时，所属回归通过，完整 Runner 因既有栅格登记失败，末尾登记检查未执行。v32 的完整 Runner 后续通过，覆盖缺失、公开缓存和矛盾或重复头等反例。以上各版准备与门禁证据按当时记录认定，新增真实 Hosted 范围见 [v46／v10 实测](#v46v10-分组与角色保护-hosted-闭环2026-11-13)。

### 导出错误提示与旧文件的实际边界

本机正式第一方普通 User 在 Gateway 以已撤销方式再次创建导出，中英文均返回 `409/query_protection_choice_invalid`；新默认会话 #14 实际下载五行，Transfer 执行 `529987ad-95fa-4781-9624-9e6ba61a9e3b`。首次审计把模块代发的人类触发来源误当 User Actor，按正式执行来源契约修正后复验。该次 Develop 构建为 `cb2a25b5b24f117417fa7dd2b55d04e314c5fd6d`，Security／Transfer 为 `1ccbf4316bb6244a72d188d8cc78bd2699bf141f`。三入口、两类 409 与双语提示另由 12 项受控浏览器用例核对。该次本机 HTTP 仅覆盖新请求提示，角色资格失效和已清理的旧特殊文件未在该次复验。CLI OAuth 未获第一方角色选择资格。[现行导出身份与错误契约](../develop/CLAUDE.md#角色选择与独立导出)

此前独立本机记录已在 Transfer 投影同步后核对同一旧角色文件拒绝。默认导出规则变化又有其独立的失效及恢复证据。两者分别以[旧角色文件](#mysql-查询全量导出与能力提示2026-10-23)和[默认文件失效](#导出文件保护变更失效2026-10-27)为准。本机提示核验证据见 `/tmp/p145-export-web-http-20261026/`。完整早期记录可按[整理前固定提交](https://github.com/pampa0629/addp/blob/9ac0e437c1bd34e16b35ec4d963cbe95ba68cadf/security/CLAUDE.md#L40-L88)回查。

## Service SQL 补充记录与 Graph 消费者

### Service SQL 角色方式与本机证据

查询支持范围及当前角色核验以 [Service 查询发布与执行规范](../service/docs/architecture/查询发布与执行.md#登录用户的角色脱敏选择)为准。这里记录早期非联邦单表直接字段与别名的结果。候选用 POST，保存只准备元数据，实际执行和交付复核当前本人角色。已有 PostgreSQL 两表 JOIN 范围另见 [Service 协议测试](../service/docs/testing/查询与要素协议.md#postgresql-两表-join-角色方式2026-11-07)。

| 主题 | 实际验收与收尾 |
| --- | --- |
| 单表双别名 | 普通用户 #33 的默认／全遮盖两页 JSON／CSV、方式撤销后新请求及旧游标 409、显式移除后默认恢复均通过。合并审计 12 项、6 份实际 CSV，恢复文件与初始默认文件逐字节一致。#7 最终私有、停用、空选择，临时策略撤销，会话注销。两次早期脚本失败由 Service 专题保留。v34 隔离 Runner 当时通过，精确 Hosted 未运行。[本机验收](../service/docs/testing/查询与要素协议.md#sql-单表角色方式本地验收2026-11-03) · [v34 隔离断言](../scripts/docs/Online数据保护.md#v34sql-双别名撤销与默认恢复) |
| 命名参数 | 本机 #31／MySQL 使用必填 string 参数，独立审计 46 条实际响应及 9 份 CSV，覆盖实际过滤、旧游标绑定、24 次参数错误 400、4 次方式失效 409 及逐字节恢复；这 46 条均为 `no-store`。服务 #8 最终私有停用、空选择，临时策略撤销。较早被缓冲淘汰的事件保留取证缺口。v35 Runner 的普通用户必填 int 场景是另一证据范围。[本机证据](../service/docs/testing/查询与要素协议.md#sql-命名参数角色方式验收2026-11-04) · [v35 隔离断言](../scripts/docs/Online数据保护.md#v35sql-输入命名参数与游标绑定) |
| 可选默认值与续页 | #33／MySQL 的默认保护 HTTP 验收依次覆盖 int 默认修订、两向省略／显式续页（36 条响应）、允许值列表 int 修订（43 条）及 string 默认修订（42 条）。保留旧游标拒绝、失败不改定义或版本、CSV 与 JSON 一致。临时服务 #11–#14 各按版本删除并核对 404，#7 保持，会话注销。活动角色、其他引擎与各版 Hosted 分别认定。[默认值修改与本机结果](../service/docs/testing/查询与要素协议.md#已发布sql默认值修改2026-11-04) · [v39 Runner](../service/docs/testing/查询与要素协议.md#新默认续页的隔离runner接入v392026-11-04) · [v40 允许值列表](../service/docs/testing/查询与要素协议.md#允许值列表默认修订的验收接入v402026-11-04) · [v41 字符串修订](../service/docs/testing/查询与要素协议.md#字符串默认值修订的验收接入v412026-11-04) |

发布默认值的修改范围以[正式版本更新规则](../service/docs/architecture/查询发布与执行.md#已发布-sql-参数默认值修改)为准。v37 的确定性 Runner 核对省略与显式传入相同默认值可交叉续页，改值续读旧游标返回 400，方式撤销仍返回 409。v36 完整 Runner 曾在 Cleanup 日志标记断言处失败，Security 分组和登记 CLI 后续独立通过。v39 完整入口曾在 TiDB 部署夹具超时，Security 分组和登记 CLI 独立通过。v41 的完整 Runner 按固定输入实际通过，各轮原结果保留。[可选默认参数](../service/docs/testing/查询与要素协议.md#sql-可选默认参数隔离断言2026-11-04) · [历次门禁与固定输入](https://github.com/pampa0629/addp/blob/ec8ae1f9fd70dfecbabc4587b44534bfd1b63525/security/CLAUDE.md#L168-L207)

### SQL 资格生命周期 Hosted 的首败

固定源码 `2b752ee0ed69c4fcfe3a5875735ad3385ac0947b` 的 [Run 41796322974／Job 126461181063](https://github.com/pampa0629/addp/actions/runs/41796322974/job/126461181063) attempt 1 在消费用户请求 PostgreSQL 整引擎扫描时因 System 管理范围核验返回 403。资格撤销、自然到期和其余业务均未执行，缺领域、Online 和浏览器报告。生命周期清理通过、Infra 零残留，业务域清理未进入。

v42 夹具将 basic 初识别交给独立准备用户，消费用户只刷新原七张表内的精确 `item_id`。修正后的冻结副本完整 Runner 和登记 CLI 通过，未在该轮重派 Hosted。资格撤销的旧会话 401／重登，与自然到期的同会话有效、仅资格消失，分别以 Service 专题的断言和后续实跑认定。[Service 首败和修正](../service/docs/testing/查询与要素协议.md#sql-角色资格生命周期隔离断言2026-11-04) · [v42 扫描准备](../scripts/docs/Online数据保护.md#v42扫描准备身份与业务精确刷新) · [后续 v46 实际范围](#v46v10-分组与角色保护-hosted-闭环2026-11-13) · [未覆盖项](../docs/sessions/数据脱敏与保护后续工作专题.md)

### Graph 投影消费边界

固定 `addp-graph` 只消费 `graph` 投影和确认 cursor，纳入安装及解除屏障。启动事务补齐未 released 资源和 releasing 的解除变化。Graph 领域读取复用 Common 对显式 graph leaf 的门禁，命中纳管则拒绝。Graph 不提供属性脱敏、Policy 或原值豁免，源数据授权归 System。消费者集合以 `protection_owner_contract.go` 为准。验证沿 Graph／Security／System Go、Security PostgreSQL、System migration 与 Common Neo4j／FalkorDB 门禁。Graph 的实际验收由独立场景认定。[Owner 确认规则](../docs/spec/addp数据安全与隐私保护实现规范.md#九投影变化流与-owner-确认) · [Graph 保护场景](../docs/spec/testing/scenarios/数据保护.md#graph-protection)

## 早期保护验收结论与缺口

以下结果按固定源码与实际执行范围认定。后续的 [v44 四出口与清理](#v44-完整-hosted-验收与清理闭环2026-11-08)和 [v46 分组与角色保护](#v46v10-分组与角色保护-hosted-闭环2026-11-13)分别记录了新版覆盖。旧运行仍可逐轮核对。[本节原始记录（固定提交）](https://github.com/pampa0629/addp/blob/911024dd7f1e77d1c24d0e6d964d59ccbe2e09a4/security/CLAUDE.md#L216-L500)

### 资源筛选、银行卡识别与有效期

- **资源筛选。** 普通 User 的九组只读组合筛选已核对字面子串、百分号字面匹配、引擎、生命周期、范围交集、计数／分页及重置恢复。同步状态和当前期限独立展示。前一轮页面整页导航后沿用旧 API 客户端，Meta 返回 401，未完成筛选验收。两轮均记录隔离清理零残留。[v12 通过，源码 `7a6476b3d`](https://github.com/pampa0629/addp/actions/runs/38477006785) · [v12 首败，源码 `dc8d4520f`](https://github.com/pampa0629/addp/actions/runs/38471195140) · [现行资源治理规则](../docs/spec/addp数据安全与隐私保护实现规范.md#受保护资源治理与银行卡号元数据识别)
- **银行卡识别。** v11 验证元数据检测仅命中明确命名的字符串字段，Finding 与 Assessment 指向同一敏感类型。四个数据输出 Owner 核对五行掩码、Unicode、短值、NULL、空串和相邻反例，Transfer 实际写入五行并重扫回读，临时资源和 Infra 零残留。该轮银行卡结果来自 API 与 Worker，截图覆盖既有客户数据。检测器只依赖字段元数据，卡种、号码有效性、校验位和银行卡页面另有验证边界。首轮在旧 900 秒总预算内等待邮箱索引解除隔离时耗尽，缺完整报告。[v11 通过，源码 `ea1379586`](https://github.com/pampa0629/addp/actions/runs/38433500090) · [v11 首败，源码 `333cc6ae5`](https://github.com/pampa0629/addp/actions/runs/38425834669) · [敏感发现规范](../docs/spec/addp数据安全与隐私保护实现规范.md#六敏感发现执行)
- **有效期和来源准备。** v13 验证同修订、同进程下的资源自然到期，四出口拒绝，Transfer 零行／零字节且目标不变。显式恢复长期并同步新修订后，恢复五行脱敏结果，旧版本写入被拒绝。初版实际验证 System 210 前向迁移；后继通过的精简夹具只为七张 PostgreSQL 表准备精确源许可，MySQL 和 MongoDB 仍按当时的路径完成扫描与保护。该授权观察仅适用于 v13，后续 v44 使用的三目标六项许可另有证据。早期前置失败分别是未配置独立 System 服务身份导致读取检查 503，以及 Meta 将业务分支误当无父节点入口导致 500。两轮都未进入相应保护输出，退出清理零残留。[v13 源码 `05ea0c789`](https://github.com/pampa0629/addp/actions/runs/38835960352) · [v13 精简夹具 `dd3d39890`](https://github.com/pampa0629/addp/actions/runs/38840651279) · [503 首败 `061e8420f`](https://github.com/pampa0629/addp/actions/runs/38704771989) · [Meta 500 `9fed427f4`](https://github.com/pampa0629/addp/actions/runs/38830258272) · [现行有效期规则](../docs/spec/addp数据安全与隐私保护实现规范.md#资源保护有效期) · [现行隔离夹具](../scripts/docs/Online数据保护.md#hosted-精确许可准备与清理)

### 原值访问与 Develop 历史

- **现行结果。** 原值授权按用户与获批出口独立生效。Develop 获批的当次结果有界交付，历史始终保留默认脱敏预览，需重新查询才能按当前授权查看原值。v6 验证授权有效时当次可见原值、同一历史默认脱敏，以及自然到期、撤销、另一用户隔离和 Security 不可达。Console 浏览器证据覆盖申请审批与 Manager 保护状态，Develop 当次／历史分工来自领域 API 和工作台门禁。本机迁移核实旧私有历史键为零、既有脱敏预览可读，Hosted 清理通过且 Infra 零残留。[v6 源码 `293fdafe2290955c15272056b627c8fad721be2a`／Run 39041158723／Job 117420768709](https://github.com/pampa0629/addp/actions/runs/39041158723/job/117420768709) · [Develop 现行契约](../docs/spec/addp数据安全与隐私保护实现规范.md#102-develop-当次原值结果与脱敏历史)
- **旧规则及日志缺陷。** v5 曾实际采用私有历史实时重判，获批后的原值历史曾进入 GORM 参数展开日志。修复通过本机实际写入与 Hosted 获批写入核对，SQL 保留占位符、类别和过程记录，原值及私有载荷不进入日志。v5 当时核对了双用户、独立出口、到期／撤销回落及退出零残留。更早两次 Hosted 分别卡在 Graph 前端默认端口和撤销投影尚未在 Manager 生效的浏览器交接，两轮清理通过。旧日志、截图与备份副本处置仍按已确认决定搁置。[v5 通过，源码 `188da46aed6e39b83cb540fcc66d0ca5c4d915e2`](https://github.com/pampa0629/addp/actions/runs/39028729141) · [端口前置失败](https://github.com/pampa0629/addp/actions/runs/39018432898) · [撤销同步失败](https://github.com/pampa0629/addp/actions/runs/39024702645)

### 字段策略、拒绝阶段与页面证据

| 固定源码及实际运行 | 当轮新增覆盖 |
| --- | --- |
| `3d4176f168633f92187de49f0be4427bff50936c` · [v14／Job 116879250398](https://github.com/pampa0629/addp/actions/runs/38872372087/job/116879250398) | MySQL 别名的默认保护保持。派生查询在准备阶段拒绝，Develop／Monitor 可按同一 UUID 查到安全诊断及无结果。首轮将 MongoDB 血缘拒绝误期望为结果阶段，页面未执行，清理通过。[v14 首败 `8d91f3b14`](https://github.com/pampa0629/addp/actions/runs/38868796504) |
| `6e31b58175d0c022f575480873e3197ff6622428` · [v15／Job 116924746894](https://github.com/pampa0629/addp/actions/runs/38886293712/job/116924746894) | Service 到期返回 403／`data_protection_required`，Transfer 到期或派生拒绝采用准备阶段安全诊断、零新增写入、目标不变。Provider 类型化缺表或 SQLSTATE 仍归查询错误。缺少反例的候选在排队时取消。[未执行的候选](https://github.com/pampa0629/addp/actions/runs/38885678273) |
| `68d7cbd0311f51e00fc97c821c737f9c36d65f43` · [v16／Job 117250246594](https://github.com/pampa0629/addp/actions/runs/38983726735/job/117250246594) | 首次真实浏览器核对 Service 与 Transfer 的到期拒绝、清空旧预览、恢复安全输出和同 UUID Monitor。Service 按发布字段抑制 email，Transfer 原生整表保留合规的十一列。前两轮分别误用 Monitor 的 `triggered_by` 校验 Transfer 的 `trigger_by`，以及误用 Service 字段清单校验原生整表。两轮均未进入到期页面，清理通过。[发起人断言失败 `f93c7928`](https://github.com/pampa0629/addp/actions/runs/38919249803/job/117034601396) · [列断言失败 `7a8bce58`](https://github.com/pampa0629/addp/actions/runs/38923900689/job/117050436707) |
| `8025781f5c825b00f3b9046441c236d9d4d7e768` · [v17／Job 117263317878](https://github.com/pampa0629/addp/actions/runs/38987833025/job/117263317878) | MongoDB 短值触发 Transfer **结果**阶段保护拒绝，专业详情和 Monitor 记录同一 UUID，零新增写入、目标不变。原基线恢复后重新执行成功。 |
| `7207a59f714e2d27ca633d67b1c4c50680239b67` · [v18／Job 117703147439](https://github.com/pampa0629/addp/actions/runs/39136902282/job/117703147439) | 普通用户验证三个空间表普通属性字段、四出口独立策略、重启保持与撤销回落。该轮空间表范围限属性保护。五个必要 Owner 按完整集合核验同步，四个输出出口各自核验输出。 |

上述成功轮均有 Hosted 生命周期清理与 Infra 零残留。v18 之前的四次 Hosted 失败保留原结论：初次搜索在 Manager checkpoint 初始化前 403，下一轮首批技术索引未投递导致空命中并耗尽预算，随后领域脚本等待的 Owner 少了 Graph，最后浏览器仍使用旧四项清单。四轮均已清理，不计作新增字段策略通过。[初始 403 `645ebd416`](https://github.com/pampa0629/addp/actions/runs/39087749235) · [总超时 `cd2803e94`／Job 117661462679](https://github.com/pampa0629/addp/actions/runs/39122735286/job/117661462679) · [领域屏障 `fb13955d8`](https://github.com/pampa0629/addp/actions/runs/39127891317) · [浏览器清单 `441e4e5ca`](https://github.com/pampa0629/addp/actions/runs/39132512437)

- **分层门禁。** v15 同源码的 Service 与 Transfer PostgreSQL 实际通过。候选的 Go workspace 曾因 pgx 直接依赖声明漂移失败，修正后已实际复验通过。v16 Hosted 成功时，对应 Platform consistency 仍在 PostgreSQL 调用字符串断言处失败。[v15 Release/T2](https://github.com/pampa0629/addp/actions/runs/38886250264) · [Go workspace 修复 Job 116936933536](https://github.com/pampa0629/addp/actions/runs/38890009152/job/116936933536)
- **本机与受控验证。** 2026-10-15 至 16 日的本机 MySQL 副本核对了默认保护、CSV、Develop 和 Service，以及普通用户中文页面的到期拒绝与恢复。首次 MySQL 目标因 TEXT 主键限制未写入，后续 PostgreSQL 目标实际读写 2,188 行且到期时零新增写入。恢复长期后 Service 退回原停用状态，任务及脱敏目标留作复查。本机故障中出现的 `refresh_token_reuse_detected` 未由该次验收修复。英文真实页面和 Transfer 结果阶段另以上表 Hosted 证据认定。
- **治理界面。** 已有权限的用户通过评估历史查看各出口不可变策略修订，包括已撤销策略。缺 Policy 读取权限时不请求，读取错误明确展示，关闭或切换后丢弃旧响应。弹窗复用共享 `preserveDialogFocus` 保留打开过程中的用户焦点。受控前端门禁和本机只读复核通过，修订弹窗无独立 Hosted T4。策略适用动作、算法上限、撤销回落和原值授权限制引用[保护策略规范](../docs/spec/addp数据安全与隐私保护实现规范.md#71-protectionpolicy-首期语义)。

### SQL 输出保护与派生血缘

| 固定源码及实际运行 | 当轮新增覆盖 |
| --- | --- |
| `4145b1b92293c43210a4e0168a8ce57ecbb30ead` · [v19](https://github.com/pampa0629/addp/actions/runs/39275743465) | 单普通基表 `SELECT *` 与带别名通配符，逐值核对五行掩码、NULL／空值与非敏感字段。 |
| `3397631bfb828923a0a5de25e1b8d4854d190d55` · [v20／Job 118420568188](https://github.com/pampa0629/addp/actions/runs/39359032226/job/118420568188) | 直接字段／别名副本可与同表通配符混投，敏感副本分别保护。 |
| `726b2fc5ed2e67410d3c9ae151b05ff91f21b932` · [v21／Job 118524506515](https://github.com/pampa0629/addp/actions/runs/39391692620/job/118524506515) | 单层分页包装保持行选择，外层通配符与直接字段分别核对。SQL Service 三页 2／2／1，无重漏，末页停止。 |
| `fd4cd5f513b943086e24cae94da3b238ad1731ac` · [v22／Job 118614370212](https://github.com/pampa0629/addp/actions/runs/39418814791/job/118614370212) | Develop 六类敏感计算在准备阶段拒绝，独立 UUID 的同一历史无结果。SQL Service 敏感派生输出返回 403；`count(*)` 与非敏感计算对照通过。 |

上述 Hosted 成功轮均有独立报告核对与退出零残留记录。新版混合投影、单层分页、重复输出名拒绝和六类计算也另有本机真实 Develop 及历史复核。源元数据快照未变与源业务值核验分别认定。原生 PostgreSQL／MySQL 的结构锁和编译器按对应 T2 认定，其他 MySQL 兼容实例、复杂 SQL 与静态加密仍需各自证据。

早期 MySQL 原生 T2 曾重现表达式字段值来源漏记，合成结果出现原值，隔离夹具已清理。修复后明确区分直接、派生、可证明无字段依赖与不透明输出。受保护派生即使具备直接字段原值授权仍拒绝。复杂值来源需完整证明，产品规则与支持边界以[Owner 本地执行契约](../docs/spec/addp数据安全与隐私保护实现规范.md#十owner-本地执行契约)为准。

### 四出口逐原因矩阵与结构漂移

- **最终结果。** 固定 `f0f949b5123cb5ad2ff8778d015577d4a5bd0e42` 的 [Run 39727724444／Job 119667747241](https://github.com/pampa0629/addp/actions/runs/39727724444/job/119667747241) attempt 2 实际通过 v25／浏览器 v9。七原因四出口共 20 个真实运行格、8 个仅共享协议格。Manager／Service 核对 403 和稳定码，Develop／Transfer 核对同 UUID 历史及准备阶段原因，Transfer 另核对零写入与目标不变。保留 21 张截图，临时策略和基线已恢复，各类资源和 Infra 分别核对零残留。[attempt 1 的 Job 119655405669](https://github.com/pampa0629/addp/actions/runs/39727724444/job/119655405669) 在 Manager 前端安装时遇 npm ECONNRESET，未运行业务。[实际覆盖矩阵与 T2 边界](../scripts/docs/Online数据保护.md#四出口逐原因矩阵)
- **诊断范围。** v23 证明敏感计算、血缘无法验证、到期的具体原因，v24 补入未就绪、不支持操作、结构不匹配的 Develop／Monitor 同 UUID 和真实页面。尚未生效仍仅有真实 PostgreSQL T2，没有产品开始时间入口。v25 的空间表证据覆盖普通属性字段且保留坐标与 NULL；MongoDB 覆盖嵌套、稀疏、NULL、三算法、可证查询传输和 Canonical Extended JSONL，其他 MongoDB 公开输出及全类型另验。[v23 `8da847b503ff8ef2c6cc183d577db0d1cb5e7250`／Job 118720172659](https://github.com/pampa0629/addp/actions/runs/39449485758/job/118720172659) · [v24 `31bcd85f08b6ad305d85078002748c700284307a`／Job 118973549956](https://github.com/pampa0629/addp/actions/runs/39523768953/job/118973549956)
- **运行前结构复核。** Manager 读取原生表行前用实时完整字段核对保护投影。Transfer 在打开原生读会话及任何目标操作前核验精确来源的实时结构，结构不符或缺失沿既有准备阶段拒绝。真实 PG 回归中旧实现已触发目标重建与写入，修后为零读写且目标 OID、行数和内容不变。两者均未认证描述与读取之间的并发 DDL 原子性。

| v25 前序实际失败 | 保留的判断依据 |
| --- | --- |
| `dc36584fdebf87879d62b5f116600d2bd1116989` · [119303398341](https://github.com/pampa0629/addp/actions/runs/39623669811/job/119303398341) | 当时 MySQL Provider 缺 Transfer 查询读会话，任务创建 400。原响应正文缺失，独立复现定位配置校验，未运行保护准备拒绝。 |
| `b100eda7b803d180e0db53297331ba618de11b67` · [119341995199](https://github.com/pampa0629/addp/actions/runs/39634714694/job/119341995199) | 高置信 Finding 自动采用默认保护，未就绪夹具的人工复核假设不成立。原状态正文缺失，修订后的探针使用完成发现且零 Finding 的精确 PG 源。 |
| `4f394dd95d08e5c18dd051a5e7dd566545c4d2f4` · [119403048973](https://github.com/pampa0629/addp/actions/runs/39652172676/job/119403048973) | PG 字符串过滤合法，旧探针实际重建目标并 1 读／1 写；该轮未保存行值。后续 PG 对照确认合法过滤只写掩码值，拒绝探针按实际方言选择。 |
| `8caece4d70b1adc6b5edf6ece56e1d8eabd8a653` · [119505065215](https://github.com/pampa0629/addp/actions/runs/39681568226/job/119505065215) | Manager 结构漂移预览返回 200，原响应正文缺失，未直接证实原值。 |
| `b703950106e174bff02ec236f31e5c3689546670` · [119570484821](https://github.com/pampa0629/addp/actions/runs/39701169370/job/119570484821) | Transfer 原生源结构漂移仍成功，目标重建且 5 读／5 写，原目标行值未归档。 |
| `c0153c89a23b988bc5788e6476cd61c9ab73558e` · [119606039075](https://github.com/pampa0629/addp/actions/runs/39712245598/job/119606039075) | 汇总先于 MongoDB 证据完成，产生 KeyError，领域报告与浏览器未完成。 |

这些失败轮的退出清理与业务结论单列，当时未完成的矩阵保留失败。v24 的 Platform 登记也曾因负例没有变异实际版本标记而失败，后继实际门禁已验证修正。[首败 `f5449d860`／119984762601](https://github.com/pampa0629/addp/actions/runs/39527270182/job/118984762601) · [修复 `a9309a84`／119147047035](https://github.com/pampa0629/addp/actions/runs/39577642649/job/119147047035)

## 执行身份约束的角色事实接口（2026-10-22）

用户确认默认保护不随角色叠加改变，角色仅增加可选方式资格后，已补齐异步执行所需的 System/Common 前置接口：`POST /api/v1/system/execution-authorizations/{id}/subject-roles`。仅同 Tenant 的 Develop/Transfer Owner 可按真实 running execution、授权 ID、attempt 和 lease 核验真实 User 发起人的当前角色；Develop→DuckDB 仍由 Develop 读取，Runtime、其他 Owner、错误 Tenant/主体/成员/版本、失效授权或租约拒绝。Orchestrator 子执行核对父来源；响应只含绑定引用、有效角色键并集、观察时间和上界期限，不返回权限清单、连接或登录 Token，不写业务配置或历史。无功能 Permission 的角色仍参与资格集合；新角色触发 IAM 版本变化时旧执行授权仍拒绝，重新建立当前授权后才读取新集合。原始数据读取与原值审批规则不变，不以本接口授予新的数据效果。

规范见 `docs/spec/addp授权上下文规范.md`“执行发起人的当前角色核验”和数据安全实现规范 7.1.1。实现为 System `execution_subject_roles.go`、API adapter 与 Common `system_execution_subject_roles.go`；沿用现有执行授权权限，无新增迁移、角色权限分配或通用角色枚举 API。角色选项控制面、投影编译、保存查询/服务/传输的选择引用与四出口执行/界面尚未接入；本项不能记为角色脱敏功能完成。

验证：`make test-module MODULE=system GO_ONLY=1`、`make test-module MODULE=common GO_ONLY=1` 通过（仅 Go T1）；`make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package iam --test execution-subject-roles"` 在已核实的 25432/addp_iam_test 通过真实 PostgreSQL T2，无跳过，复用完整 ExecutionAuthorizationService 用例，新增当前角色、无权限角色、自然到期、Owner/租户/身份/租约/父执行边界。前两轮测试夹具分别因变更角色后沿用旧授权、非法成员期限，以及修改不可变角色分配期限而失败；已改为正式新会话/新授权与创建时确定期限后的自然到期，不关闭数据库约束或放宽断言。`make test-system-iam-runner` 27项通过；System Swagger 生成与覆盖检查通过（241个公开路由方法）。标准 IAM T2 默认发现仍覆盖本用例，新增 filter 只用于最小充分本地回归，未新增 CI 路线。日志 `/tmp/p145-role-{system-t1,common-t1,system-t2,runner,swagger}.log`。尚未进行本接口的本地 HTTP/浏览器或 Hosted T4，未重启用户服务、不定时、不跟进 CI；四出口原有逐原因 T4 不作为本接口证据。


## 2026-10-22 角色可选方式控制面与 Projection v5

### 选项发布与当前资格

- **Security 控制面。** `role_options` 与默认效果同属一个 ProtectionPolicy 不可变修订。新选项由 Security 分配 UUID，更新只能复用当前修订的选项身份。撤销保留审计，发布侧移除失效选项。多个当前有效角色提供可用选项的并集。无显式选择时使用默认策略，选项限定为基线许可且类型匹配的内置掩码，不能绕过 `suppress`／`deny`。选项不授予原值访问。投影引用绑定策略、策略修订、评估修订和选项身份，失效时明确拒绝。[默认方式与角色可选方式](../docs/spec/addp数据安全与隐私保护实现规范.md#711-默认方式与角色可选方式)
- **资格来源。** 同步操作以当前第一方 Tenant User 自查获知完整有效 Role Key，包括无功能 Permission 的业务角色。后台执行由所属 Owner 按本次 EA、execution、attempt 和 lease 核验发起人；Worker、Runtime、创建人和发布人的角色均不可代替。角色事实只用于当前选择，不扩大源许可、原值授权或执行效果。[本人自查](../docs/spec/addp授权上下文规范.md#本人当前角色自查) · [执行发起人核验](../docs/spec/addp授权上下文规范.md#执行发起人的当前角色核验)
- **控制面界面。** 角色候选来自现有租户角色目录，沿用 `iam.tenant_role.read`，只显示可分配给 User 的角色。缺权限或目录失败时可保留或显式移除既有选项，禁止新增或以空目录覆盖配置。修订历史保留选项身份和参数，版本冲突沿原显式重新加载。内置角色译名由共享前端维护，自定义名称保持输入。

### v5 迁移与就绪

运行时只接受 `addp.protection_projection/v5`。Security 从可信 Enrollment 前像迁移中央投影与历史变化，保留发布序号。消费端由 `005_projection_role_options_v5` 清除旧缓存及完整 checkpoint，再完成重放。最初只清 cursor 会留下 `initialized=true` 并提前就绪，修复后通过 SQLite 与真实 PostgreSQL 回归。该次 Security 和 Transfer 启动 schema 均升至 6，确保已运行旧 schema 的 Transfer 也执行共享迁移；这是迁移发生时的版本。回放期间的拒绝及完整确认见[Projection v5](../docs/spec/addp数据安全与隐私保护实现规范.md#八protection-projection-v5)与[Owner 确认](../docs/spec/addp数据安全与隐私保护实现规范.md#九投影变化流与-owner-确认)。

### Develop 保存、执行与历史

候选限当前用户显式选择的来源。查询保存精确字段和选项修订，保存（含仅改名称）、后台执行及当次交付均核验当前资格和已证明来源，过期、撤销、修订变化或选择未消费均明确拒绝。所选实时输出与默认历史由同一次 PreparedQuery 分别形成；有界暂存逐字段剥离失效原值，CAS 与分版计时防止旧观察恢复原值或延长留存。独立导出按自身选择和交付规则执行。各 Owner 的具体支持范围及限制见[正式角色方式契约](../docs/spec/addp数据安全与隐私保护实现规范.md#711-默认方式与角色可选方式)与[Develop 角色选择及独立导出](../develop/CLAUDE.md#角色选择与独立导出)。

### 实际验收与未覆盖项

| 层级 | 实际覆盖 |
| --- | --- |
| 控制面和协议 | Common／Security Go T1 通过。Security PostgreSQL 8 个顶层用例通过，验证四出口编译、默认不变、修订身份、基线收紧及跨租户拒绝。Common PostgreSQL 的 `projection-store` 组 7 个顶层用例通过，覆盖旧缓存重放和首次初始化。两次完整 Common PostgreSQL 尝试遇并发守卫中止，仅记投影存储组通过。 |
| 角色事实与原生查询 | 真实 IAM PostgreSQL 验证无功能 Permission 角色、旧版本、自然到期与登出。PG／MySQL 原生角色方式新增 14／5 例，覆盖字段投影、选择优先于原值豁免、同次默认历史及失效拒绝。Common PG 仅 `query-read-set` 组，Develop PostgreSQL 另核对快照与过期租约。 |
| 受控界面 | Security／System 各自完整前端门禁通过。后续补充的目录失败、无权时保留配置等场景由定向回归覆盖。Develop 92 项、Monitor 289 项受控浏览器用例及构建通过，中英文诊断另有 2 项定向回归。 |
| MySQL 本机真实账号 | 构建 `673cf8eef7cb776de26a0b04c8dbdc86384becbf` 包含 `50446c027`。现有普通多角色账号保存定义 #26，验证五行默认、保存并重载所选全遮盖、同一 UUID `7cc501ec-5d46-483a-8033-0e73d5579d87` 的默认历史。方式撤销并同步后，旧选择在准备阶段拒绝、同一历史无结果，Monitor 同 UUID 原因一致。显式移除选择后恢复五行默认。临时选项已撤销，无调度定义与不可变审计保留。 |

本机这次只验证一个现有账号、一个 MySQL 来源和一个方式的完整生命周期。零功能权限角色、多角色并集、成员资格撤销、原值豁免冲突按各自 T1／T2／T3 认定。该轮无 MQL、其他 Owner 或 Hosted 角色结论。后续真实 Hosted 的三出口分组与角色范围见 [v46／v10 证据](#v46v10-分组与角色保护-hosted-闭环2026-11-13)。本机材料见 `/tmp/p145-role-choice-local-20261022/`，门禁详情和逐轮证据可按[整理前固定提交](https://github.com/pampa0629/addp/blob/ba7bf3029bf0b3855cf45d2af4fdb9a8ff3d287c/security/CLAUDE.md#L292-L323)回查。

### 同期独立集成验收

源码 `6ca313c0628ada381fb3e441c32e064a2cbd9900` 的 [Run 39872011655](https://github.com/pampa0629/addp/actions/runs/39872011655) attempt 1 记录四项实际 Hosted 成功，各自生命周期清理通过、Infra 零残留。

| 作业 | 实际覆盖 |
| --- | --- |
| [Compose 120148447106](https://github.com/pampa0629/addp/actions/runs/39872011655/job/120148447106) | Cookie 轮换、iframe 收敛和刷新保持、退出传播。JavaScript 不持久保存令牌。 |
| [Metric TiDB 120148447390](https://github.com/pampa0629/addp/actions/runs/39872011655/job/120148447390) · [Metric PostgreSQL 120148447393](https://github.com/pampa0629/addp/actions/runs/39872011655/job/120148447393) | 两套均实际执行 `count_distinct` 与 `sum_decimal_by_group`，覆盖不自动重绑、撤回后拒绝及显式重绑恢复。 |
| [openGauss 120148447472](https://github.com/pampa0629/addp/actions/runs/39872011655/job/120148447472) | Manager／Develop／Service 读回由 5 行变为 6 行，阶段内校验和一致；Transfer 初始 5、增量 2、空恢复 0，创建与删除各 2。 |

这些证据限各自合成场景，未覆盖角色选择、MQL、静态加密或 openGauss 脱敏 SQL 下推。独立审计索引为 `/tmp/p145-fixed-scope-39872011655-audit.json`。同期空白修正提交 `d8ced917a0c8293548b94e4c944bfbae664ab86e` 的 [Platform Job 119937956271](https://github.com/pampa0629/addp/actions/runs/39811301682/job/119937956271) 实际通过一致性和变更空白检查。

### Transfer 角色选择接入前的原生字段保护基础（2026-10-23）

用户已授权继续接入 Transfer 角色脱敏选择和全量导出。本轮先落地不依赖新增身份边界的共享读取基础：`projectionstore.PrepareTableProtection` 显式接受同一请求作用域的角色字段选择，复用查询既有的选项解析、来源消费校验和算法，不制造查询或复制算法。空选择继续默认保护；有选择时来源／字段／动作未命中、当前角色不具资格、观察到期或选项修订变化均拒绝，不静默默认。每批输出（含空批）在修改结果前复核当前投影选项；独立 `ApplyDefault` 保持默认预览。Transfer 两处现有调用显式传空选择，接口旧签名删除，当前产品默认行为保持。规范和 Common 边界说明同次更新。

验证实际通过：`make test-module MODULE=common GO_ONLY=1`（Common Go T1），`make test-transfer-backend`（Transfer 全后端 T1），最终输入上的 `cd common && go test ./dataprotection/projectionstore -count=1`，以及 `make test-common-postgres COMMON_POSTGRES_TEST_ARGS="--test projection-store"`（真实 25432/addp_test，8 个顶层用例、0 失败、0 跳过，不计完整 Common PG 门禁）。新增 `TestProjectionStoreNativeRoleChoicesAgainstPostgres` 自动由现有门禁发现，复用同一断言覆盖默认／SM3、NULL、多角色增加、Tenant／出口／字段／修订隔离、无受管目标拒绝、资格失效及准备后修订变化拒绝、默认保护恢复。`make test-projection-store-ownership` 9 项及所有权扫描通过；本轮相关文件 `git diff --check` 通过。日志在 `/tmp/p145-native-roles-{common-go,transfer-go,projection-go,postgres,ownership}.log`。没有新 HTTP／浏览器 T4，不新增 CI 入口，不派发或跟进 CI，不启停本地服务。

Transfer 任务选择／保存／执行与 Develop 全量导出尚未接通，不记为完成。只读核对发现普通 Transfer 执行及模块导出目前未绑定可供现有 subject-roles 接口核验的用户执行授权；导出只保存 User ID 不等于当前异步角色资格。已向用户提出：仅角色专用操作按当前登录账号签发本次 Transfer 执行授权，再按 execution/attempt/lease 读取角色，不保存登录令牌、不借用后台或发布人身份；这样选择专用方式需要现有 system.execution_authorization.create、transfer.task.execute 和对应引擎效果资格。该权限／调用边界待用户确认，未擅自补权限或放宽 System。查询与导出仍为独立动作，查询选项不能直接当作导出选项；已提出分别保存一次、以后复用的方案。默认导出、源授权、原值授权与栅格均未调整。后续优先确定上述执行身份边界，然后接入任务及导出定义、共享选择组件与实际输出验收。

### Transfer 角色选择与 Develop 全量导出接入（2026-10-23）

用户已确认执行身份边界。本轮接入当前第一方登录用户手动发起的 bounded/snapshot/native table 及其 PreparedQuery 来源：Transfer 的 `config.source.protection_choices` 与 Develop 的 `content.export_protection_choices` 均只保存精确源字段／选项身份及修订，独立于查询的 `content.protection_choices`。未选择采用本出口默认保护，不跨出口复用查询选项。Transfer 向导复用共享 FieldMaskingChoices，默认折叠且不查询角色候选，已有选择重新打开时显示并复核；Develop 同一抽屉分别展示查询与导出方式，保存、重载及新 execution 快照保留两套引用。只改名称也必须复核，旧引用、角色失效、错误来源／出口、切换不支持形态不能静默丢弃或改回默认。新查询／导出方式不属于原值授权。

手动任务启动／重试、角色专用全量导出在同步用户请求内签发本次 Transfer EA，校验真实 Principal、Tenant、Membership／Version 和确切 execution UUID；要求现有 system.execution_authorization.create、transfer.task.execute 及实际引擎读写效果资格，未增加任何权限或 Role Assignment。Worker 使用用户 EA 获取源／目标引擎访问，按 execution/attempt/lease 逐批读取 System 当前角色；保护门禁统一只读 Runtime Descriptor，不再为判断保护领取连接凭据。源字段在映射和 Writer 前由 Common 保护；无选择的默认路径不增加角色或用户 EA 依赖。定时、委托、编排、continuous、增量、encoded/record 来源不消费角色专用方式。原有默认 MongoDB Extended JSONL 不因此扩张为角色导出。

共享导出会话新增可空保护引用（只有源 locator／字段选择），由现有 EnsureStore 单一路径安装，旧默认会话不补造资格；EA 事实纳入完整请求摘要，仍由精确 Owner 会话核对发起人，User ID 本身不成为执行权限。Get/Open 对特殊会话必须提供当前用户核验回调；文件生成成功不替代下载资格，选择或角色失效拒绝。特殊文件经现有同一路由的 User Bearer 与共享认证下载工具交付，拒绝外部／带票据 URL，不把令牌放入 URL；纯 Resource Ticket 不代替完整角色核验。默认下载保持原行为。权限不足按 401/403 返回，选项或角色资格冲突按 409 返回，不把 IAM 拒绝伪称敏感字段保护失败。Swagger 同步，文件路由仍遵守原 resource_ticket 入口声明，特殊交付条件写明于 Description，未放宽覆盖检查。

验证：Common 完整 Go T1 `make test-module MODULE=common GO_ONLY=1`、Common 新传输／会话／选择回归 `cd common && go test ./client ./exportartifact ./dataprotection`、Transfer 完整后端 `make test-transfer-backend`、Develop 完整 Go T1 及最终 `go test ./internal/api ./internal/service`、Manager 完整后端 `make test-manager-backend` 均通过。新增回归覆盖保存和仅改名称更新、精确权限／角色冲突区分、Owner 会话与独立 EA／真实发起人绑定、逐批新观察、失效后交付拒绝、未知字段／角色断言／空选择／不支持形态，以及两套业务引用独立快照。

真实 `25432/addp_test` 上 `make test-common-postgres` 完整门禁、`make test-transfer-postgres`、`make test-develop-postgres` 均通过，不以 T1 中跳过的外部服务用例冒充 T2。Common 的旧会话加列／新选择持久化由既有 TestExportExecutionSourceAgainstPostgres 覆盖；Transfer 已登记的 TestIntegrationPostgresBoundedExportMasksBeforeTargetWrite 新增原生／查询两种真实读写分支，目标实际收到所选 SM3，模拟角色失效时零读写且原目标不变。这是 T2 合成当前角色事实，不是实际 IAM 任免／撤销 T4。Develop 首次调用标准入口缺少 DSN，补齐同一已核实测试库参数后完整通过，不将缺参前置失败记为产品问题。

前端 `make test-common-frontend` 266 项、`make test-develop-frontend` 单元／92 项浏览器／构建、`make test-transfer-frontend` 单元及构建通过；Develop 双语角色场景在同一已登记用例中实际保存、重载、执行两套选择，查询选择失效后明确改回默认不清除导出选择。共享下载新增当前账号调用、外部／票据 URL 拒绝和受控本地化错误回归；最后下载提示微调另经共享门禁及 Develop 构建通过。`make test-authorization test-projection-store-ownership`、Swagger 生成与路由覆盖通过（Transfer 38／Develop 60）；所有权门禁 9 项及 4659 个 Go/SQL 文件通过。初次授权覆盖指出 resource_ticket 不允许 conditional Permission 声明，已依现有单一路由协议修正注解并完整重跑通过，没有放宽断言或增加认证旁路。新增测试均由现有 Go、T2／前端标准入口发现，无需新 Makefile／CI 登记或新外部依赖。

日志 `/tmp/p145-transfer-role-{common-go,common-targeted,transfer-go,develop-go,develop-final,manager-go,postgres,transfer-postgres,develop-postgres,common-front,develop-front,develop-build-final,transfer-front,service-test,export-tests,contract-gates,swagger,swagger-coverage}.log`。当前代码／T1／T2／T3 接入已实现，但本地新版真实账号及 Hosted 角色导出 T4 尚未执行，尤其 MySQL／MongoDB 角色专用导出不借用旧默认保护 T4 作证。后续优先加载本轮 Common／Owner 构建，验收 MySQL 已有 Persons 测试表的任务选择／全量 CSV／失效拒绝与显式默认恢复。未启停用户服务，不设置定时或派发／跟进 CI；源授权、原值授权与栅格均未改动，清单父项继续部分实现。

### MySQL 查询全量导出与能力提示（2026-10-23）

Develop 查询全量导出使用成功执行的冻结快照及同一 PreparedQuery 的 `QueryReadSession`，Manager 原生表全量导出使用 `TableReadSession`。Develop 前端按当前引擎实例及查询语言启用入口，后端在签发 Execution Authorization（EA）或创建会话前再次核对同一实例的 Runtime Descriptor。能力缺失返回 `422/query_export_unsupported`，普通查询仍可用；描述读取失败返回 503，权限拒绝与角色选择冲突各用原有状态码。MySQL 在只读 repeatable-read 事务中消费单一结果流，保留用户显式 LIMIT 和逐批保护，关闭或取消时回收会话。完整行为见[Develop 查询全量导出](../develop/CLAUDE.md#查询全量导出)及[引擎查询读取会话契约](../docs/spec/addp引擎插件接口规范.md#queryruntimeprovider)。

**门禁与原始失败。** 旧构建的普通查询和保存导出方式成功，四次实际导出均因缺少 MySQL QueryReadSession 失败，未创建 Transfer 执行或 CSV。修复后的真实 MySQL T2 走 `make test-common-mysql-data-protection COMMON_MYSQL_TEST_ARGS=--local-disposable`，核对 1,007 行单流、业务 LIMIT、快照隔离、输出保护、取消和只读防写，临时资源清理为零。相关模块 Go、Swagger 及能力开关的受控浏览器用例通过。全 Develop 前端门禁为 91／92，`graph-fit-view` 缩放断言失败。全仓 Go 首次在 Ontology 来源漂移处停止，后续按实际语义更新 Transfer 修订 32，来源门禁及相关 Go T1 复验通过。委托携带角色选择继续拒绝，普通停止态任务创建的权限和操作范围保持。[Ontology 来源复核](../ontology/CLAUDE.md)

#### 本机真实 CSV 与撤销交付

构建 `dfa4ad16105d0421af13e55f3e4099acdd6751a2` 包含 MySQL 查询流式读取修复。普通开发者 #33 用同一 MySQL Persons 来源核对三个边界：

- **完整结果与独立导出方式。** 查询仍按默认保护显示。去掉业务 LIMIT 的查询预览为 500 行并提示截断，导出会话 #8 由同一用户执行，实际读写并下载 2,188 行。CSV 四列及 2,188 个唯一 ID 核对通过，1,121 个非空 phone 全遮盖、1,067 个为空。Transfer 执行 `78103957-6697-45be-a71e-5193bcc05888`。
- **新导出拒绝与默认恢复。** 撤销方式并同步后，保留旧选择时默认五行查询仍成功，新导出返回 409，未创建会话。显式移除导出选择并保存后，原 SQL 的 LIMIT 5 保持，默认导出会话 #9 实际下载五行，一个 phone 为前 3 后 4 掩码、四个为空。Transfer 执行 `3375a11c-2030-4684-8902-70a8e82797c8`。
- **旧文件的再次交付。** 同一个五行角色会话 #10 首次下载成功，撤销后、Transfer 投影更新前再次请求仍返回 200。在 Transfer 确认修订生效后重发原文件请求，Develop 与 Transfer 检查均实际返回 `409/query_protection_choice_invalid`，响应只有安全错误 JSON，无 CSV。该检查没有创建替代会话，手段为浏览器重发同一正式请求。当时页面未提供已有会话的再次下载按钮。此前保存到客户端的文件仍保留，临时方式已撤销、定义选择已清空。Transfer 执行 `06f09be1-dd60-44fb-99af-cbccf20f5b6f`。

完整文件审计、请求记录及图片分别见 `/tmp/p145-transfer-role-local-20261023/` 与 `/tmp/p145-redownload-local-20261024/acceptance-report.json`。瞬时的新导出拒绝没有完整截图，判定来自实际 HTTP 409 和页面可访问性文本。

#### 同租户会话隔离

固定本机构建 `e931905933bab26701e2586eb8df9a49d9da4a30`。开发者 #33 与管理员 #31 都能读取本人会话、实际保存同一查询的五行默认脱敏 CSV，管理员仍受默认保护。双向交换会话 ID 后，详情和文件四次均返回 404；两条文件响应及管理员读取对方详情的正文为 `query_export_not_found`，开发者读取管理员详情仅留状态证据。正常文件下载使用产品 Resource Ticket；交叉检查各用自己的 User 请求。实际下载验值的是 #11 与 #13，#12 只用于会话生成与交叉检查。报告见 `/tmp/p145-export-isolation-local-20261024/acceptance-report.json`。范围仅为同租户两名第一方用户、指定 MySQL 查询和五行 CSV，跨租户、其他格式与其他引擎另验。

#### 独立原生 Transfer 与目标建表

MySQL 新目标首轮在建表时实际返回 1170／42000。源主键为 `varchar(191)`，通用字符串目标映射为无键长度的 TEXT 主键。目标表不存在，读写计数未记录。这个失败属于物理目标定义，作为该次运行的未解决项保留。失败执行 `be831017-8e04-495a-9f57-e1f2f14cd876`。

后续同一第一方用户经正式向导使用同一 MySQL 源，手动 bounded／snapshot 写入三个新 PostgreSQL 目标，分别证实默认、全遮盖和显式清空选择后的恢复。三次均实际读写 2,188 行并逐行对照。默认两轮为 863 个标准掩码、258 个短值与 1,067 个空串按异常值抑制输出 NULL；角色轮为 1,121 个等长星号、1,067 个 NULL。保留失效选择再次启动时返回 409，执行数及原目标保持；角色轮使用该用户 EA，默认两轮无角色专用 EA。三条成功执行依次为 `da8ec17e-782a-4307-9b4a-74dac532ac22`、`3f435992-042e-4c7c-ad17-9da8d835f095`、`adb4d1d4-fddf-48b0-8514-543b77f2a100`。

本机任务最终停止且清空选择、无调度，临时方式撤销。三张脱敏验收表及执行审计留作复查。逐行核对见 `/tmp/p145-native-transfer-20261024/acceptance-progress.json` 与同目录各物理审计。

**覆盖边界。** 本节保留的是指定本机 MySQL 查询导出、撤销投影生效后的同会话交付拒绝、同租户隔离和 MySQL 源到 PostgreSQL 新表的原生任务。MySQL 目标建表失败、定时／委托／编排／连续增量、其他组合和对应 Hosted 结果按各自证据认定。后续默认文件规则变化见[导出文件保护变更失效](#导出文件保护变更失效2026-10-27)，角色资格自然到期见[Online 资格到期与旧文件](../scripts/docs/Online数据保护.md#transfer-资格自然到期与旧文件)。逐阶段记录及原件索引见[整理前的固定版本](https://github.com/pampa0629/addp/blob/ad09944650e300e73aeb38b801c1ea1152e99d39/security/CLAUDE.md#L348-L406)。

## 导出文件保护变更失效（2026-10-27）

按用户确认，默认与角色专用用户导出共同绑定 Transfer `export` 的实际来源和保护投影。Worker 在原生表／同一 PreparedQuery 的准备阶段，以 checkpoint 版本和当前 lease 将精确来源、投影身份／修订／checksum 与有效期写入现有 execution 私有 metadata；无选择的默认导出仍不增加角色或原值授权依赖。Transfer 在相关 upsert／release 的同一事务中不可逆标记旧记录失效，按 100 条分页处理；无关目标不影响文件，重复同修订同内容不误伤，恢复原规则不复活旧文件。Get/Open 每次读取当前模块自己创建的 Transfer execution result 的受控 `export_protection_current`，缺失、失效或自然到期均拒绝；执行成功审计保留，原文件仍沿既有暂存清理。旧执行没有记录时须重新导出，不设兼容放行。角色资格／选项原有精确原因继续优先保留，其他保护失效统一409/`export_protection_changed`及中英文重新导出提示，保留 `no-store`。Manager／Develop 的默认与角色下载统一使用已有认证 Blob 客户端，交付阶段拒绝显示错误，不保存成文件；不修改普通源文件、瓦片或栅格下载。

真实 PostgreSQL 首次暴露既有同修订投影按字符串比较的问题：JSONB 规范化键顺序及空白导致内容相同被误报修订冲突。已复用 Common 严格 JSON 解码按完整对象内容比较，保留精确数字及未知字段，拒绝重复键／不同内容，未放宽修订裁决。首次失败日志 `/tmp/p145-export-version-transfer-postgres.log`；旧 cached-success 路径的反向回归确实失败，日志 `/tmp/p145-export-version-common-red.log`。首份 HTTP 对象夹具曾缺 Last-Modified，补齐协议后验证，不将夹具失败记作产品保护失效。

实际验证：`make test-go`（JSONB修正后的全仓 Go T1）通过，后续 Transfer 内部及清理断言扩展由 `make test-module MODULE=transfer GO_ONLY=1` 再次通过；`make test-common-postgres COMMON_POSTGRES_TEST_ARGS="--test projection-store"` 的真实投影存储组及完整 `make test-transfer-postgres` 通过且无跳过，新增 TestIntegrationPostgresExportArtifactProtection 覆盖未纳管后纳管、无关表、同修订幂等、同事务回滚、规则更新／release 后不可复活、自然到期、重新加载、原生／PreparedQuery 两条真实准备钩子及101个文件跨批失效，回滚核对所有夹具 execution 与临时 schema 零残留。该组不是完整 Common PostgreSQL 引擎门禁，不记为实际用户 T4。

`make test-common-frontend` 284项、`make test-develop-frontend` 110项受控浏览器及构建、`make test-manager-frontend` 387项组件／单元与281项受控浏览器及构建通过。新增 Develop 默认方式的中英文 status／file 409场景确认有明确提示且无文件下载。Swagger 用当前已提交基线加本会话源文件在临时副本走标准生成，隔离其他会话未提交字段变化；94／60／38条 Manager／Develop／Transfer 路由覆盖通过，顺带同步基线已有注解的生成差异，无对应产品行为变更。已有模块／前端标准入口和 Go CI 自动发现本次文件；Transfer PostgreSQL 标准入口同次登记新增用例，无新 workflow 或验收 suite。

本轮不启停服务、不更改实际角色／原值／源授权，不派发、轮询或定时跟进CI。恢复 Docker 后完成上述 T2；新构建实际 HTTP CSV 与旧文件拒绝仍待用户重启全部模块后验收，隔离 Hosted 由专门CI会话跟进，均未计为通过。Excel 实际行155/B空（动态脱敏与静态加密执行总述）、156/B155（动态脱敏实时生效）父项仍为部分实现，由p0统一维护。日志：`/tmp/p145-export-version-go-jsonb-final.log`、`/tmp/p145-export-version-common-projection-postgres-green.log`、`/tmp/p145-export-version-transfer-postgres-final.log`、`/tmp/p145-export-version-transfer-delivery-final.log`、`/tmp/p145-export-version-common-frontend.log`、`/tmp/p145-export-version-develop-frontend.log`、`/tmp/p145-export-version-manager-frontend.log`。


2026-10-27 补充收口：4c新增ExportSession私有保护记录触及Ontology已捕获的Transfer service整文件来源，但漏同步平台来源审查，专门CI会话报回漂移。已在本地用 `make test-platform-review` 强制非缓存复现，按平台定义规范逐项复核：CreateTask停止态创建语义/权限/效果不变，新增初始化仅在已验证模块来源的CreateAdHocExecution中，运行和再次交付继续未建模。平台修订34→35，只更新service真实提交4c及摘要/用途，保留所有来源、覆盖和绑定；定义其余字段逐项相同。非缓存来源门禁、Ontology/Transfer Go T1均通过，未扩展业务规则、IAM或发布内核；不将此前通过记录冒充该门禁已通过，不将本次本地结果记为CI恢复或新构建实际CSV通过。详见 ontology/CLAUDE.md 和 `/tmp/p145-export-platform-review-audit.json`；CI及待重启后的实际CSV边界保持。


2026-10-28 修后 CI 回传归档：专门 CI 会话已核实真实 Job 终态，本会话仅复核其保存的 Job 元数据与原始日志，未联网查询、重跑或触发 CI。修复提交 `79fa05e7b9907b0ad978ce8716ac050b56314a80` 的 [Go workspace Job 122248960679](https://github.com/pampa0629/addp/actions/runs/40521342460/job/122248960679) 实际执行 `Run all tracked Go module tests` 并成功，原始日志含 `ontology/internal/platform 0.676s` 通过；包含该修复的后继 `7ebf98370ebc965e09cb5744983dbf8ee7fbbbce` 的 [Platform consistency Job 122257071998](https://github.com/pampa0629/addp/actions/runs/40523616999/job/122257071998) 实际执行 `Run platform consistency gate` 并成功，同包 `0.176s` 通过。独立核对两份日志 SHA-256 与专门会话审计一致、Job/run/head SHA/attempt 精确匹配、成功步骤实际完成非跳过，并验证修复提交是后继来源的祖先。因此本次4c引起的来源漂移已有修后实际 CI 恢复证据；旧失败记录保留，不推广为当前或未来 HEAD 的全部 CI 通过。

证据：`/tmp/addp-ci-20261027-160029/Transfer_79fa_review_source_actual_CI_recovery_audit.json` 与对应 `job-122248960679.log`、`job-122257071998.log`；本会话独立复核 `/tmp/p145-export-source-ci-return-independent-audit.json`。此归档仅补来源审查门禁恢复，不能替代新构建的实际 CSV、旧文件拒绝与重新导出恢复验收，也不证明修订35已在运行环境激活。用户已确认重启且四个相关 Backend 曾核对包含4c；当前尚未收到已有 `/tmp/p145-run-export-protection-acceptance.sh --temporary-protection-cycle` 的运行报告，CSV验收保持未验证。本轮仅文档归档，复用已完成测试，不重复产品门禁，不新增任务、调度、配置或权限，不操作本地服务。


### 默认导出保护变更的本地实际 CSV 收口（2026-10-28）

用户重启后，沿 Gateway 正式登录 outdoor-develop（User33、Tenant1）及 addp-owner（User31、Tenant1，完成现有 TOTP），对既有 MySQL `business.security_persons_acceptance_20261015` 完成默认 Manager 原生表全量导出与 Develop 同一直接字段 SQL（LIMIT5）导出的三阶段实际 HTTP 验收。四个相关 Backend 的 ready 响应均记录构建 `13b34fb5f22e28f932ae633d7780ebdd615f2b6d`，包含产品修复 `4c9e1e53ee1e895c398dd0e0aaa46de54d832422`；不据此推断 Worker 的独立构建 SHA 或本体修订35已激活。

42项请求与42个唯一 request ID逐项关联对应 Owner 的运行日志路由／状态，实际生成6份CSV：每阶段 Manager2188行、Develop5行，独立复核CSV字节摘要、列序、唯一行标识、非保护字段及保护结果。默认／恢复阶段Manager均为863个掩码值和1325个空单元格，Develop为1个掩码值和4个空单元格；临时全遮盖阶段Manager1121个非空值均只含星号、1067个为空，Develop仍1个全遮盖和4个空单元格。全遮盖与默认参数不同，不要求两阶段非空数量相同；恢复默认的两份CSV摘要与各自初始文件完全一致。

精确既有 Transfer/export 覆盖策略临时 active 后，两个初始会话的状态／文件在中英文下均409/`export_protection_changed`；覆盖恢复 revoked 后，初始与全遮盖四个旧会话仍全部拒绝，合计24次真实拒绝，响应保持 `no-store`、明确重新导出建议且无文件附件。重新导出两份默认CSV成功。独立只读核对6条真实Transfer执行均success、读取数等于写入数、精确DataItem与三阶段保护版本绑定；前四条私有记录不可逆invalidated，恢复后的两条有效。执行成功历史保持，未用下载拒绝改写原成功审计。Owner日志使用路由模板，精确会话对应由冻结的脚本循环、会话／execution绑定及私有保护记录共同核对，不声称日志展示了原始会话路径参数。

策略由原revoked/version6经active/version7恢复revoked/version8，两条新修订均为User31的本次唯一验收原因；保留策略修订、执行与导出会话审计，文件沿既有暂存生命周期保留，不声称零残留或追回用户已下载副本。两份临时登录均正常logout204。未修改角色、源授权、原值授权或源表数据，未自行操作服务生命周期或跟进CI。

前两次临时脚本失败均保留：首次遗漏管理员既有MFA步骤，在登录阶段退出；第二次Manager请求漏带已有Meta item_id，未取得字段快照而在保护准备阶段fail-closed，未写出CSV。已按现有认证契约补齐MFA／上下文响应解析，Manager来源与正常UI及既有成功脚本一致携带item_id44573，未放宽保护校验或改变产品代码。临时脚本11项回归及语法检查通过。独立审计初次因两批旧文件复用检查名称而拒绝，后按冻结顺序、42个唯一请求与实际Owner日志逐项校验，仍严格要求24拒绝／6物理CSV／6执行及恢复事实；未修改原始运行报告。

证据：`/tmp/p145-export-protection-http-20261027/acceptance-report.json`、同目录`independent-runtime-audit.json`、`request-endpoint-evidence.json`及6份CSV；独立命令`python3 /tmp/p145-export-protection-independent-audit.py`退出0。失败材料及脚本修正证据为`/tmp/p145-export-protection-http-20261027-failed-login/`、`/tmp/p145-export-protection-http-20261027-failed-missing-source-snapshot/`、`/tmp/p145-export-protection-login-fix-audit.json`、`/tmp/p145-export-protection-source-fix-audit.json`。本轮新增的是限定默认出口的本地真实HTTP证据，不计为浏览器实际验收或隔离Hosted通过，也不替代角色专用文件、其他引擎／格式、自然到期、全量Service续页或在途并发撤销验证。相关投影在Owner同步生效前仍有既有窗口。清单实际行155/B空总述、156/B155动态脱敏实时生效继续部分实现；优先将本轮同一矩阵纳入既有隔离套件，CI由专门会话跟进，静态透明加密及通用SQL等既有缺口保留。


### 默认导出失效矩阵接入隔离套件（2026-10-28）

既有`security-mysql-owner-protection`领域契约升级v33，浏览器v9不变。复用合成银行卡五行表、现有普通治理账号、既有Meta Locator和Transfer/export覆盖策略，新增`default_export_protection`：Manager原生表与同一Develop直接查询快照各做默认／全遮盖／恢复默认三次真实导出；6份CSV在保护及相邻字段／Unicode／空值断言后保存于已有artifact目录，逐次复核实际Transfer UUID、trigger_by、五行读写和非任务定义来源。两个初始会话在规则更新后、全部四个旧会话在恢复默认后分别校验中英文status/file精确409/export_protection_changed、无结果／附件及重新导出建议，共24拒绝，最终42个边界使用会话／动作／语言和独立请求UUID作为身份；轮询另外逐次校验no-store。新文件恢复按记录值比较，不依赖原生表未声明的物理行序。

临时策略只在自己的Assessment和Transfer/export绑定下用当前版本变更。已有活动覆盖或并发他人变更时不接管；中断及PUT／POST／DELETE结果不确定时只读回、核对本次唯一原因和版本后撤销本次覆盖，不重试写操作，不覆盖他人事实。身份、基线和原查询快照保持。保留6会话／执行、策略审计及CSV，沿既有隔离Infra最终销毁；不新增权限、账号、授权、部署、清理接口或T4入口，保留原有各出口及物理清理边界。公共测试HTTP客户端仅增加Content-Type、Content-Disposition和X-Request-ID的保存及两种验收语言，禁止收集Cookie／Token；产品API及Swagger不变。

验证：`make test-security-online-runner`退出0，含38／58／31／293项Python回归、System合成身份Go门禁和CI登记检查；新增6个领域测试覆盖完整6CSV／24拒绝、全部42边界分别缺失／public／矛盾缓存头（126个反例）、错误原因／原值或默认值误替代／发起人或读写数错误／旧文件复活、缺Meta标识、现有活动策略拒绝、首次创建／未知写结果恢复、中断恢复及并发清理拒绝。HTTP客户端的成功JSON／CSV／错误响应验证安全头保留、Cookie不留存及未知语言在发请求前拒绝。定向反例通过不计作真实HTTP；日志`/tmp/p145-default-export-runner-targeted.log`与`/tmp/p145-default-export-runner-full.log`。现有Make入口与固定Hosted suite自动发现本次变更，CI注册及其删除反例同步为v33，无新增workflow。此前Persons的本地42项实际验收保持有效；新增v33真实隔离Hosted尚未运行，由专门CI会话跟进，不自行触发、轮询或设置定时。清单155/B空和156/B155仍部分实现。

### 保护算法摘要与已有资源定位（2026-10-28）

针对 MongoDB Engine11 `Outdoor.Persons / userInfo.phone` 的界面疑问，只读核对当前未退出纳管记录、Assessment、Manager/preview 字段 Policy 与已安装投影：字段覆盖为 `addp.mask.sm3/v1`，Policy 修订3、Manager 投影修订7；默认基线仍是前3后4掩码。未读取源手机号，也未调整任何规则、原值授权或源授权。原界面只展示通用“脱敏”，且 Manager 强制携带 `action=enroll`，因此难以查到已有具体算法。

本次先补充正式规范10.5，再修正唯一前端路径：配置摘要复用现有算法、参数和异常值说明；Finding 出口摘要消费后端已返回的规则算法，独立保留同步状态，缺少算法显示信息不可用，不在前端重算有效保护。Manager 的“查看/配置”沿标准 locator 打开现有 Security 页面。Security 用当前用户读取的 Meta item 指纹及既有 current/engine 分页精确定位未退出记录，刷新目标后复用已有详情会话；只有完整查找确认不存在且具备创建权限才打开既有纳管窗口。错误、重复身份、不完整分页、释放后变更和失效异步请求不解释为未纳管。入口采用既有 enrollment.read 与 Meta 目录读取约束，无需创建权限。没有新增 API、权限、保护决策或兼容 query；Swagger 无变更。

T1/T3/构建验证：`make test-security-frontend` 退出0，260项单元、37项受控浏览器和构建通过，含默认参数／显式SM3／实际出口算法独立展示、中英文只读详情导航及无 Security 写请求；单元覆盖跨页目标、完整不存在、403／协议与身份异常、重复／释放记录、路由切换及销毁。`make test-manager-frontend` 退出0，391项单元、291项受控浏览器和构建通过；另通过既有标准脚本 `cd manager/frontend && node ../../scripts/test/frontend-browser-gate.mjs e2e/security-protection-navigation.spec.js` 验证新增3项 Manager 入口，覆盖只读可查看、仅创建／无读取权限不显示入口、精确 locator 且无 action、无 Security 写入。新用例由既有 testDir 自动发现，无需新增 Make／CI 登记。日志 `/tmp/p145-protection-clarity-security.log`、`/tmp/p145-protection-clarity-manager.log`、`/tmp/p145-protection-clarity-manager-navigation.log`；文档相对链接及限定路径 `git diff --check` 通过。

本轮为前端修复，无需后端重启；未把受控浏览器计作个人环境的真实输出或 Hosted T4。未触发／跟踪 CI，仍由专门会话负责。后续待办记录于 `docs/sessions/数据脱敏与保护后续工作专题.md`，优先建议讨论 Service SQL 单表直接字段与别名的角色方式切片；不扩大源授权、栅格或静态加密范围。


### 字段保护方式分层展示（2026-11-02）

针对默认掩码与 Manager 专用 SM3 被拼接成长句的问题，按正式规范10.5整理唯一展示路径：Finding 卡片首先显示后端返回的当前各出口算法及独立同步状态，再展示识别／治理依据；默认方式和每个出口的已配置字段专用方式分别成行，参数及异常值处理通过原生 details 展开。配置存在不等同于投影采用，前端不重算基线与Policy裁决；配置读取权限不足时明确提示，仍如实展示可读的出口事实。自动发现与人工指定字段共用 `EnrollmentProtectionConfiguration.vue`，删除两处旧长句渲染及只为该渲染保留的模型字段，保留策略编辑／恢复对话框所需的既有摘要。没有新增API、权限、算法或授权，Swagger不变，不需后端重启。

验证：`make test-security-frontend`退出0，262项T1单元、37项T3受控浏览器及生产构建通过。新增／加强配置与实际出口算法不同、不可读取专用配置、基线缺失、人工评估不捏造出口事实、两个消费组件唯一复用及当前规则优先的断言。中英文、深／浅主题、四个出口（Manager SM3，其余前后掩码）、参数初始折叠／展开及480px窄窗口无横向溢出经浏览器验证，四张合成夹具截图已目视核对；无Security写请求。初始新增结构模型测试实际失败，旧三栏／长句架构断言按获批新布局修订后通过；截图检查发现旧合成Finding未带正式评估说明，补齐夹具事实后完整复验，不改变产品保护行为。标准Make入口与现有testDir自动发现覆盖，无新增CI编排。日志 `/tmp/p145-protection-layout-{red,green}.log`，截图 `/tmp/p145-protection-layout-visual/`。限定路径diff及文档链接检查通过。仅T1/T3及构建，不计个人环境实源或Hosted T4；CI仍由专门会话跟进。


2026-11-02，修复 Manager 受保护剖析结果二次读取误拒绝。`ProtectProfile` 统一接收完整源字段快照，严格校验组件路径、类型和结构指纹后执行幂等统计移除，写前及读后共用该执行器；不要求已移除的敏感统计重新出现。保留当前投影及源授权检查，不恢复原值或旧采样路径。读取失败后不继续显示旧执行进度，重试只读取结果。Security 的 Manager 配置入口改称“数据预览与剖析”，说明预览脱敏／移除时隐藏敏感及父级容器统计，预览拒绝时拒绝剖析；不新增独立策略、算法或授权。

本轮验证：`make test-manager-backend` 通过；`make test-manager-postgres` 33 项真实 PostgreSQL 检查通过，新增保护前处理→持久化→解码→重复保护及结构漂移拒绝检查；`make test-manager-frontend` 405 单元、321 独立浏览器及构建通过；`make test-security-frontend` 262 单元、37 独立浏览器及构建通过。首次回归先重现旧缺陷；接口签名遗漏调用和新浏览器夹具缺少表类型事实已修正并通过最终完整门禁，不把初次失败记为通过。标准入口与 CI 自动发现已覆盖新增测试，无新增登记路线。

用户重启 Manager 后，Chrome 实际读取 PostgreSQL engine #2 / `outdoor.p145_native_default_20261024` / Meta #44720 的成功执行 `2043d7b3-167b-4956-ac1f-ddde49102e5a`：HTTP 200、字段数 3，仅 `_id`、`nick_name`、`gender`；样本 2,000、扫描 2,188，观察记录不指向 `phone`。切换预览／剖析后同一执行再次 HTTP 200，无新建执行响应。限定本地证据 `/tmp/p145-pg-profile-fixed-20261102/read-proof.json`、`repeat-proof.json`、`profile-result.jpg` 与日志 `/tmp/p145-profile-{backend-final,postgres-final,manager-frontend-final,security-frontend-final}.log`。原纳管记录 `03a9a977-7c55-4149-8573-b1023f92a3fa` 保持长期保护，本轮未更改任何源／原值授权或源数据。实际配置页刷新被浏览器文档响应暂停阻断，新配置说明只有独立浏览器证据，未声称个人 Chrome 配置页通过；未运行新增 Hosted，CI 继续由专门会话跟进。剩余优先项为未支持剖析来源的能力提示与具体错误分类。
### Transfer PostgreSQL 两表 JOIN 脱敏落盘（2026-11-06）

在固定提交 `c5ce9c1fed61e70946a8d35d556f91fcde00f32a` 的隔离检出中，以 `make test-transfer-postgres` 在本地 `addp_test` 验证 Transfer 规划器、保护 Gate、原生 PostgreSQL 读写，20 个主测试和定向子测试通过，零跳过。两条记录按原值排序和分页选出，目标列分别落盘掩码与固定替换，两列保留各自字段来源。计算右侧受保护字段时，准备阶段拒绝，已有表的 OID 和记录保持，新目标未创建。专用 schema 已清理。报告位于 `/tmp/p145-transfer-join.zsGYYA/verification.json`。

在固定提交 `df3efe83ec32bbc2c38eea96863b0704cf477cd5` 补验真实 Transfer HTTP 路由、bounded Worker 首次领取、执行详情与事件。正常执行读写各两行，两次敏感计算均返回 `preparation / protection_computed_output_unsupported`，读写均为零，物理结果与前轮一致。任务、执行和事件记录已回滚，专用 schema 已清理。使用 System 身份与引擎事实夹具，经同一 PostgreSQL 门禁的定向运行通过四个主测试，零跳过。此前全量门禁在 continuous 仓储检查失败，首次定向还遇到 PostgreSQL 恢复和测试夹具编译问题。最终报告保留各轮日志与三个执行 UUID，位于 `/tmp/p145-transfer-worker-join.RRZjuA/verification.json`。

提交 `9c0d82b51448138578e7a1cdecb80580c1d45fcb` 将三种场景纳入既有 Security Hosted 领域报告 v43。最终领域 190 项、登记 149 项通过。聚合入口的指标夹具清理测试超过 20 秒，保留失败记录。真实 Hosted 待专门 CI 会话回传。独立审核脚本以一组模拟产物与八个反例检查 SHA、运行步骤、UUID、拒绝计数和清理，位于 `/tmp/p145-v43-transfer-join-audit.py`。[Hosted 验收约定](../scripts/docs/Online数据保护.md#transfer-postgresql-两表查询脱敏落盘)


### v44 完整 Hosted 验收与清理闭环（2026-11-08）

**验收结果。** 固定源码 `bd68a07014353d5c4a693fd863b3a3428bfdf460` 的 [Run 42454542147／Job 128650659824](https://github.com/pampa0629/addp/actions/runs/42454542147/job/128650659824) 实际通过 v44／v9 的 27 阶段、21 张截图目视和完整清理。覆盖四出口保护拒绝与恢复、同构建重启、角色方式／资格撤销与资格自然到期、Service 参数修订与旧游标、Transfer 两表 JOIN 的成功落盘与拒绝零写入。默认导出核对 42 条响应及六份原始 CSV，规则变化后拒绝旧会话与文件，恢复后可新导出。六项临时许可均正式撤销，领域 23 建／23 删、两组浏览器各 2 建／2 删，Infra 零残留。[原始归档 13609052160](https://github.com/pampa0629/addp/actions/runs/42454542147/artifacts/13609052160)

**覆盖边界。** 七原因四出口共 20 个 runtime 格、8 个 protocol_only 格。尚未生效仅有真实 PostgreSQL T2。多数子场景原始 HTTP 正文、部分方式撤销回执的缓存头未归档。当前字段检索的证据边界见下文，Service 到期页面的具体提示见本页[中英文复验](#v44v10-中英文保护到期和恢复-hosted-验收2026-11-10)。

<a id="v44-hosted-前置元数据可见性阻塞2026-11-07"></a>
<a id="v44-三项元数据查看许可夹具适配2026-11-07"></a>
<a id="v44-metadata-only-与字段检索断言冲突2026-11-07"></a>
<a id="hosted-三项精确读取权临时前提2026-11-07"></a>
<a id="v44-六项许可实测与未就绪探针扫描前提2026-11-07"></a>

**来源准备。** 三个精确目标为 MongoDB `Outdoor.Persons` 和 MySQL `security_fixture.customers`、`security_fixture.bank_card_accounts`，分别签发 `metadata_read` 和 `read`。准备者独立于消费账号，按当前 Run、租户、主体、目标及动作核对六个请求 UUID，先撤 read 验证不可读，再撤 metadata_read 验证不可见。该验收使用临时 read，字段名只靠 metadata_read 检索的能力由独立产品证据认定。修复 `20d03f724` 已有 Manager T1、PG＋Meilisearch T2 和 Linux CI 记录，真实跨服务／浏览器仍需独立证据。[精确许可准备与清理](../scripts/docs/Online数据保护.md#hosted-精确许可准备与清理)

| 固定源码及实际 Job | 首败与清理 |
| --- | --- |
| `0af01095941be47c97d2d0383bcb69b23781fd68` · [128248930706](https://github.com/pampa0629/addp/actions/runs/42322135716/job/128248930706) | MongoDB 已扫描入库，消费账号缺元数据许可，未取得唯一 `Outdoor.Persons`。领域与页面未执行，生命周期清理通过，Infra 零残留。 |
| `5bcac0793dd85d505c436f2d6e1aa4f95991da80` · [128387317730](https://github.com/pampa0629/addp/actions/runs/42367089598/job/128387317730) | 当时仅 metadata_read 无法满足字段检索，领域与页面未执行。三项许可已撤销，Infra 零残留。 |
| `b4779ba443c42e423240132c332ccb08cbb8fd23` · [128422248189](https://github.com/pampa0629/addp/actions/runs/42378933245/job/128422248189) | 未就绪探针在目标 deep 刷新前纳管，缺预期发现身份。领域与页面报告缺失，六项许可正式撤销，Infra 零残留。 |

<a id="v44-develop-查询几何契约夹具修正2026-11-08"></a>
<a id="v44-sql角色服务空参数数组修正2026-11-08"></a>
<a id="v44-精确3afbae复验总超时2026-11-08预算方案待确认"></a>
<a id="v44-预算与安全阶段诊断2026-11-08"></a>
<a id="v44-业务报告与清理令牌到期2026-11-08"></a>
<a id="v44-重启构建身份与-swagger-工具版本2026-11-08"></a>

**业务与收尾。** 原 20 分钟总预算被实际触达，后续固定为 40 分钟。隔离套件复用 4 小时夹具会话，覆盖业务及正式撤销，产品会话和角色资格的 90 秒到期仍按各自规则核验。

| 固定源码及实际 Job | 首败与清理 |
| --- | --- |
| `82f9af3592bb994fc933810984fb736c3776a0c9` · [128454804932](https://github.com/pampa0629/addp/actions/runs/42389802508/job/128454804932) | Develop 的二维 Point 夹具不识别现行 `{wkt, srid}`，对应原始响应未归档。无最终领域或页面报告，六项许可已撤销，Infra 零残留。 |
| `3f34efd2da97ef172a6ded15037bae246c83c1df` · [128525779027](https://github.com/pampa0629/addp/actions/runs/42413276305/job/128525779027) | 无参 SQL 服务合并断言失败。固定代码和回归定位 DTO 空参数序列化为 `null`，原管理响应未归档。六项许可已撤销，Infra 零残留。 |
| `3afbae0ec2921e237ee711b62d3d909a417504fc` · [128584861739](https://github.com/pampa0629/addp/actions/runs/42432701298/job/128584861739) | `online_scenario_timeout`，无领域报告及浏览器截图。六项许可已撤销，Infra 零残留。 |
| `8a7d36a28283401962f685da102e49734a04e2b4` · [128603163154](https://github.com/pampa0629/addp/actions/runs/42438722028/job/128603163154) | 业务和页面报告通过，清理遇到会话到期 401。缺正式撤销回执，Infra 零残留，整体失败。 |
| `839a4d6c0178fbb02d7c2620ff06ec815b337a0c` · [128628787628](https://github.com/pampa0629/addp/actions/runs/42447289580/job/128628787628) | 旧 Swagger 生成器改变 Manager 构建输入，重启身份断言失败。六项许可已撤销，Infra 零残留，领域和页面未完成。 |

[整理前的完整记录](https://github.com/pampa0629/addp/blob/f99ee03a999e6818fd9378beb7f5a6fd867adbb9/security/CLAUDE.md#L701)。

### Service 具体保护原因的解释接入（2026-11-08）

DP-06已完成代码：Service复用Common类型分类，在原HTTP错误形状内给出七原因与中英文建议，预览区域持续展示，不改变保护或授权规则。Service Go、12项真实PG、120项前端和构建、10项相关受控浏览器及Swagger65方法覆盖通过；完整模块在共享Manager验收登记失败，未算通过。新版个人HTTP／Hosted未运行，不继承前一节bd68的T4；原隔离自然到期浏览器断言同次改为精确原因／建议和持续区域，报告v44/v9与清理边界保持。详细首次失败及验证见[Service记录](../service/docs/testing/查询与要素协议.md#service-保护拒绝细分与持续提示2026-11-08)，证据 `/tmp/p145-service-reasons/verification.json`。

### Service 提示修复的 v44/v9 Hosted 闭环（2026-11-08）

固定源码 `5a00785ae3640c7543b4822d97d4904c15e562a8` 的 [Run 42472539366／Job 128706485615](https://github.com/pampa0629/addp/actions/runs/42472539366/job/128706485615) 实际通过 v44／v9 的 27 阶段、21 图目视和完整清理。Service 英文到期页持续显示原因与处理建议，清空旧结果，恢复后显示五行安全结果。留存报告、六份 CSV 和六项精确撤销回执通过独立核对，Infra 零残留。其他原因页面仍按各自证据认定。后续[中英文到期和恢复](#v44v10-中英文保护到期和恢复-hosted-验收2026-11-10)已实际复验。

### Service #7 本地输出与停用收尾（2026-11-08）

已完成用户批准的私有#7限定状态周期：#33／Tenant1，v11停用→v12启用→v13停用；四次中英文JSON／CSV实际200、各五行，双phone别名运行时保护检查和no-store正确，两CSV摘要相同且与历史默认基线一致。配置摘要保持，最终GET与恢复响应一致，会话注销；没有策略、角色或授权写入。保留报告独立审计及12反例通过，原脚本失败保留，非产品失败；不把只有报告／摘要的证据写成原始响应／CSV逐格独立复核，也不扩大为新拒绝／页面／T4。完整原件、版本及限制见[Service本地收尾](../service/docs/testing/查询与要素协议.md#私有-sql-服务本地启停收尾2026-11-08)，独立审计 `/tmp/p145-service-reasons/local-readiness/local-cycle-independent-audit.json`。本轮只记录本地结果，已有e32限定Hosted结论保持独立。

### Service 五类已有 HTTP 拒绝的文案断言（2026-11-08）

沿已获同意的提示验收范围，收紧既有未就绪／结构不匹配／操作不支持／计算输出不支持／自然到期五个Service HTTP场景：同一验证器必须接收由夹具确定的预期原因，继续严格403、两成员及data_protection_required，并核对完整中文原因及恢复建议。不从响应正文推断原因，不导入产品翻译生成期望，不接受通用保护、源权限、其他原因或附带私有细节的消息替代。正式无Accept-Language请求默认中文；已有英文到期页面仍为另一份证据，未新增其他语言请求或UI场景。

此轮只增强原场景断言，没有业务实现／API／部署变更。AST对照基线f2c93af2d确认209处HTTP request表达式完全相同，仅验证器及三个场景调用处改变；v44/v9结构、20runtime／8protocol_only、资源、身份、40分钟预算及清理保持。已确认根Make现有自动发现和Platform CI路径覆盖，未新增入口／workflow。新增五类消息匹配、错原因／缺建议／权限提示／私有后缀等反例；已有计算与四出口组合测试同时执行，正例夹具改成各自实际协议文案。

定向12项通过；标准 `make -o test-protection-deployment-runner -o test-graph-online-runner -o test-online-dispatcher-unit -o test-online-registration-unit test-security-online-runner` 所属272项Python及System Fixture Go退出0；四个聚合依赖目标本轮未运行，不称完整聚合通过。登记CLI、Python语法及本会话diff检查通过。首轮新增定向测试因旧验证器不接收原因参数而失败，红／绿日志保留。证据 `/tmp/p145-service-reason-assertions-{red,green,owner-gate,registration-cli}.log`，请求／版本不变证明 `...-scope.json`，固定输入 `...-verification.json`。未新跑数据库、本地HTTP或Hosted；旧5a成功不追认为这些新增内容断言通过，新实际运行仍由专门CI会话安排回传，不自行调度、轮询或设置定时。

### Service 五类具体原因与建议的 Hosted 闭环（2026-11-08）

固定提交 `a06d7433e46bd1265134251ee2f3af9ce77c0ae8` 的 Job `128732582625` 覆盖五类默认中文 HTTP 拒绝文案、英文到期页面及零残留清理。中文真实页面在该轮未逐项验收，原始子场景 HTTP 正文未归档。完整记录见[Service 查询测试专题](../service/docs/testing/查询与要素协议.md#service-五类具体原因与建议的-hosted-闭环2026-11-08)。

### v44/v10 中英文保护到期和恢复 Hosted 验收（2026-11-10）

<a id="service-中文到期页面的隔离验收接入2026-11-08尚待-hosted"></a>

**验收结果。** 固定源码 `421da33a7455f9b5a36df3e852cd37d5be4a2f8f` 的 [Run 42805905216／Job 129794843854](https://github.com/pampa0629/addp/actions/runs/42805905216/job/129794843854) 实际通过 27 阶段，23 张截图通过结构检查，四张 Service 拒绝和恢复图已目视。同一私有服务在规则自然到期后，`en` 与 `zh-cn` 两次均实际返回 HTTP 403／`data_protection_required`。页面持续显示对应语言的原因及续期建议并清空旧结果。恢复长期有效后返回五行，只含 `id`、`customer_code`。六项许可正式撤销，领域 23 建／23 删，两组浏览器各 2 建／2 删，Infra 零残留。[原始归档 13763838201](https://github.com/pampa0629/addp/actions/runs/42805905216/artifacts/13763838201)

**覆盖边界。** 真实浏览器断言核对了语言头、状态、响应形状与恢复数据，归档保留报告和截图，缺对应原始网络响应。真实页面只收口了保护规则到期与恢复，其他原因页面和切换语言时清除旧短时通知仍未验。角色资格自然到期由既有场景独立认定。[Service 查询测试专题](../service/docs/testing/查询与要素协议.md#service-中文到期页面的隔离验收接入2026-11-08尚待-hosted)

<a id="v10-首轮准备失败与检索续查消费者修正2026-11-09"></a>
<a id="检索续查修复的自然-ci-证据2026-11-09"></a>
<a id="v10-第二轮准备失败与源许可观察消费者修正2026-11-09"></a>
<a id="v10-第三轮浏览器语言头失败2026-11-09"></a>
<a id="v10-语言头修复后的-hosted-构建中断2026-11-10"></a>

| 固定源码及实际 Job | 首败与清理 |
| --- | --- |
| `6efcd87d366b2e41b299ac4af14a94a84caae25d` · Job 128787376318 | Manager 续查探针页大小为 100，准备时返回 400。领域和页面未执行，Infra 零残留。[检索续查修复](../service/docs/testing/查询与要素协议.md#v10-首轮准备失败与检索续查消费者修正2026-11-09) |
| `f0792f6f1b8255b5b9457428495b9c7c525f970f` · [128808727268](https://github.com/pampa0629/addp/actions/runs/42506020215/job/128808727268) | 精确来源观察请求缺 nonce 和预期标记，返回 400，业务、页面均未执行。三项 metadata_read 已撤销，最终观察仍失败，六项闭环未证明，Infra 零残留。 |
| `4e16bd556bc5c5b67076a56732d653cac013458b` · [128943253399](https://github.com/pampa0629/addp/actions/runs/42550010623/job/128943253399) | 页面预期 `en`，实际收到 `en-US`。到期拒绝与恢复未执行，六项许可已撤销，Infra 零残留。 |
| `d02a26ef51138ce698617d1cd254b9b96aff2f2d` · [129667620475](https://github.com/pampa0629/addp/actions/runs/42767590544/job/129667620475) | 前四阶段通过，第五阶段重启触发 Manager 构建身份变化。共享字段 `temporal` 缺 Swagger 同步，属于另一轮生成输入问题。无最终报告、0/23 图，六项许可已撤销，Infra 零残留。 |

续查修订 `0792d77993dd8015f4a3611416955768fd69846b` 的 [Platform 128796860668](https://github.com/pampa0629/addp/actions/runs/42502098196/job/128796860668) 与 [Console 128796860653](https://github.com/pampa0629/addp/actions/runs/42502098196/job/128796860653)，来源观察修订 `7c92e7c84b28c581edd5dea3230e2da20adf5ea9` 的 [Platform 128815197630](https://github.com/pampa0629/addp/actions/runs/42508155900/job/128815197630) 各自通过所列 CI，Console 页面使用受控环境。真实业务结论以该轮 Hosted 报告为准。

[整理前的完整记录](https://github.com/pampa0629/addp/blob/f99ee03a999e6818fd9378beb7f5a6fd867adbb9/security/CLAUDE.md#L878)。

### PostgreSQL 单表分组输出重写（2026-11-11）

按已确认的下一步扩展原生 PostgreSQL 同一 PreparedQuery 的输出重写。支持单表、直接列分组或分组序号、直接列／显式别名输出及显式别名 count(*)。改写前将分组序号固定为原字段，继续原有排序别名和序号解析，避免输出替换后重新分组或排序。两个源值脱敏后相同仍保留独立分组及各自计数。普通掩码、固定值替换和移除复用现有变换器和交付处理，ReadSet、函数可信性和输出血缘继续走既有路径。与源字段不同名的输出别名分组、HAVING、分组集合、JOIN、DISTINCT、窗口及其他聚合仍在原拒绝边界内。

先补实现规范和红色回归，再修改现有重写器。新增 T1 覆盖三种分组引用与拒绝路径。真实 PostgreSQL T2 覆盖三种分组引用×三算法×三种排序×Execute／分批读取，共54次读取对照原查询再按共享规则保护的结果，核对计数、行序、分页、重复组、Unicode、短值及NULL。新用例名 TestIntegrationResolvePostgresQueryOutputRewriteGrouped 命中既有 Common PostgreSQL 门禁，无新标准入口或 workflow。

验证已实际通过：make test-module MODULE=common GO_ONLY=1；make test-common-postgres 完整标准门禁。通过前两个完整尝试在 KML 连接校验失败，首轮缺 host，第二轮还缺明确 database／sslmode。最终显式使用经 infra/status.sh 核实的 localhost:25432、addp_test 及 disable 后全门禁退出0，无跳过。未修改 KML 或基础设施。最初测试准备将 Args 写成 Parameters 的编译错误及修正记录保留。无本地服务重启、实际出口HTTP／浏览器或新增 Hosted 证据。

证据 /tmp/p145-grouping-{red,unit,common,module,pg-full,pg-final,pg-complete}.log，输入摘要 /tmp/p145-grouping-tested-inputs.json。其中pg-complete为最终全门禁，pg-full和pg-final为原失败。下一步加载Common新构建后，核对Develop实际分组查询、默认历史预览和导出，继而按具体出口补覆盖。静态加密与透明解密仍待前置讨论。

### PostgreSQL 分组查询本地 Develop／导出验收（2026-11-11）

用户重启后，通过 Gateway 的正式登录核对普通 outdoor开发者 #33／Tenant #1，使用已有 PostgreSQL engine #2、Meta #44720、outdoor.p145_native_default_20261024。三种 GROUP BY 写法 p.phone、phone、1 分别产生成功执行 6df22569-48c4-43e3-889d-1489cba5dc81、f0c3e098-ddcf-446d-90f6-c4a62cd0d494、f21eb225-70a1-47ad-aa1d-e3d2e59e3bb1。每条返回五组 contact／n，按源字段排序后取五组。实时结果与历史相同，重复读取历史不变，三种写法的行序及计数相同。

Develop 导出会话 #19 经独立 Transfer 执行 94347359-7a39-4359-9b66-cf26b6bed48a 成功生成 CSV。保留的实际文件逐字节 SHA256 为 96a4d551238f13f41aa50fb0f96c6fc7a8c4ebd08cff2de38dc57cf707f64471。独立解析五行、两列，手机号保留前3／后4，中间按长度遮盖，长度依次10、11、11、11、11，计数均1；根据实际CSV重算的行摘要与三条执行报告全部一致。脚本实际断言创建／下载 no-store 和注销成功，保留报告的 logged_out=true、rejections=[]。原始HTTP响应未留存，不能把报告内的实时／历史及响应头断言称为独立原始报文审计。

前两次本地失败保留。p5dviirx 报告停在额外 Manager 预览403，无查询或导出；此多余前置已移除，具体403原因未归档。mf68n8t8 的第一条执行成功，但临时脚本错误要求固定四个星号；开发者只读诊断核对精确Meta身份及已安装的保留前3／后4规则，第一行长度10、含三个星号，五行均符合实际规则。脚本修正为长度保留，变量长度红绿回归、原值／过量保留拒绝、正式Develop拒绝保留及注销检查通过。开发者存储诊断不代替当前用户HTTP验收。

原件 /var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/p145-grouping-local-cjagmlmn/，独立脚本及结果 /tmp/p145-grouping-independent/。本地表是此前脱敏Transfer的产物，因此本轮证明普通用户业务出口的分组可用及历史／导出一致性；原值分组、掩码碰撞及三算法的变换等价性仍取此前真实PostgreSQL T2。无个人浏览器或新增Hosted通过，无策略、角色或授权写入。下一步按原SQL范围核对Service查询服务的发布与实际输出，其他聚合和HAVING仍在已记录边界内。

### PostgreSQL 分组 SQL 本地 Service 发布与分页验收（2026-11-11）

普通 outdoor开发者 #33／Tenant #1 通过正式Gateway登录完成限定HTTP验收。沿用PG engine #2、outdoor.p145_native_default_20261024，以同一直接列分组及count(*)查询取得Develop执行 c86457e6-6bb0-4a79-be81-a2117612e190 的11行保护预览，作为两页各5行及继续分页的对照。正式Service输出契约检测后，新建私有临时SQL服务 #17、version1，稳定键为contact，无固定LIMIT、无角色专用选择。

实际两页JSON与CSV均成功。保留结果中的contact为变长前3／后4掩码，n均1；两页合并10行与Develop对照的前10行逐项相等，11行中的最后一行为仍有后页提供对照。独立解析两份实际CSV的字段顺序、行序、计数、掩码和原始字节摘要，分别为96a4d551238f13f41aa50fb0f96c6fc7a8c4ebd08cff2de38dc57cf707f64471和96bf04ffc3f984d5f4e4edccb2635c4cad52c59d24b3a9993247e6c0cec1ffcc。原脚本实际断言JSON旧游标续读、JSON／CSV的no-store及分页头、消费版本一致，最终以version1删除精确服务并GET确认404，随后注销。保留报告service_deleted／logged_out均true，rejections为空。未归档原始HTTP响应，协议头、游标和清理结果按运行断言及保留报告记账。

原件 /var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/p145-service-grouping-local-n2bdx9x6/；独立审计 /tmp/p145-service-grouping-independent/independent-audit.json。本轮只补临时脚本和证据，无产品代码变更、策略／角色／授权变更或个人服务启停。本地源是此前已脱敏Transfer产物，原值变换等价性仍取前述真实PG T2。本轮不包含Service分组的角色选择生命周期、个人浏览器或新增Hosted。下一步优先补Transfer独立分组任务的实际目标写入，再按具体出口补其他已确认形态。

### Transfer 分组任务本地准备期 Meta 可见性阻塞（2026-11-11）

用户执行 /tmp/p145-transfer-grouping-local.py，普通#33／Tenant1登录成功，在GET /api/v1/meta/items/44720返回404后停止并注销。真实失败原件为 /var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/p145-transfer-grouping-local-wfgt7k4w/acceptance-report.json，独立核对 /tmp/p145-transfer-grouping-independent/initial-failure-audit.json。无task_id、参考查询或Transfer执行，尚未创建任务或写入目标；task_deleted=false不表示遗留任务。受控脚本的00000000开头UUID只属于模拟检查，不计实际验收。

开发者只读核对Meta #44720仍属Tenant1／PG engine2，身份及fingerprint与此前相同，未软删除。现行Meta单项接口会将不可见与不存在均返回404，故当前账号的元数据可见性需先核实，不能把本轮当成Transfer分组或脱敏执行失败。未变更元数据／内容读取授权，未借其他账号继续运行；具体权限调整仍需用户确认。既有Develop／Service限定本地HTTP与Provider T2证据保留，独立Transfer落盘未执行。用户已授权后续同类脚本由本会话直接执行，正式登录凭据仍由用户在认证步骤输入。

同轮继续只读核对，已有验收任务 #16（p145原生表默认恢复验收20261024）仍保存PG outdoor的正式target.parent_locator。源Meta单项读取只是临时脚本为取得目标schema位置加入的额外前置，并非Transfer源读取或目标写入的正式授权替代。脚本已改为唯一读取路径GET /api/v1/transfer/task-definitions/16，在当前用户权限下核对精确任务身份及其PG目标parent_locator，再走同一正式创建／执行入口。保留原Meta404，不放行任何Transfer权限拒绝，不变更授权。成功／执行失败／启动拒绝三组受控脚本检查通过；修正版尚未实际运行。电脑控制工具明确拒绝操作Terminal，无法由该通道完成交互式认证；用户已授予的脚本执行授权继续有效。

### PostgreSQL 分组 Transfer 独立任务本地验收（2026-11-11）

修正额外定位前置后，用户实际运行同一临时脚本，正式登录核对 outdoor开发者 #33／Tenant1，通过既有任务 #16 的只读配置取得 PG outdoor 目标位置，未修改该任务。参考 Develop 执行 a88cb013-2823-47c0-8dd1-b49f2752cd93 返回五组。新建独立、无调度、未启用的任务 #56，再手动执行 84ac72e0-2444-40d2-b8ce-26e6f547ed7d；实际状态 success，trigger_by=33，records_read=records_written=5，批量大小2。目标回读执行 bbf4b1d4-5754-4231-8074-e889e33697c1 成功，保存的 contact／n 五行与参考逐行、逐字节一致，计数均1，掩码长度保留。任务 #56 已删除并核对 GET404，登录已注销；结果表 outdoor.p145_group_transfer_6b7afc3d0152 按约定保留供查看，未将保留结果表称作零残留。

原件 /var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/p145-transfer-grouping-local-qch78ix7/，报告 SHA256 929e76b4441e8f4abc6e0bb6b382b2f0e416b7a89760c452ffbd138409eb8bf6；reference.json 与 target.json 的 SHA256 同为 ad5e87676c8cf0929da219818fb87bd74f7afb94d045629fd1aa05991874027b。独立脚本 /tmp/p145-transfer-grouping-independent/audit.py 实际退出0，结果 independent-audit.json；未导入验收生成脚本，核对三条非模拟UUID、精确任务和执行关系、五行两列、顺序、掩码及清理报告。身份、执行状态、读取／写入计数、删除／404和注销是正式HTTP脚本保存的断言，未归档原始HTTP响应，不称为独立原始报文复验。

本轮仅本地普通用户HTTP验收及文档补证，无新产品代码；重算此前T1／T2的三个Provider输入摘要仍一致，复用有效门禁，不重复全量执行。初次Meta404失败与其审计原件保留。源表仍为历史脱敏传输产物，本轮证明独立任务能够消费限定分组查询并保持目标结果一致；原值变换等价性另由真实PostgreSQL T2证明，角色方式失效、本地浏览器和新增分组Hosted均未验。下一步建议将Develop／Service／Transfer分组场景接入现有隔离套件，使用原值样本补实际默认保护和角色方式失效拒绝，再由专门CI会话安排运行。

### v46/v10 分组与角色保护 Hosted 闭环（2026-11-13）

**验收结果。** 固定业务源码 `45fe76a6f830d0968d73476e66812436659a4fd2`、控制面 `6d30e0f4fdc54a5035d7b3f88e397e6c3244eb68` 的 [Run 43217668163／Job 131180952821](https://github.com/pampa0629/addp/actions/runs/43217668163/job/131180952821) 实际通过 28 阶段和 23 图目视。PostgreSQL 单表直接字段分组与 `count(*)` 验证默认、角色选择、方式撤销拒绝及显式移除后恢复。Develop 分开验证同一 UUID 的当次结果与默认历史，Service 验证 JSON／CSV 两页及旧游标拒绝，Transfer 验证独立执行、物理回读和拒绝零写入。六项许可正式撤销，领域 26 建／26 删，两组浏览器各 2 建／2 删，Infra 零残留。[独立复核与留存证据](../docs/spec/testing/evidence/数据保护与执行血缘历史证据.md#v46v10-分组角色脱敏-hosted-验收2026-11-13-独立复核)

**覆盖边界。** 方式撤销与角色资格撤销／自然到期分别核验。20 个 runtime 格、8 个 protocol_only 格的范围保持，尚未生效仅有真实 PostgreSQL T2。部分分组原始响应未归档。多表分组、复杂聚合、角色调度／编排身份、AK/SK、静态加密及后续前端竞态修复未由本轮覆盖。[三出口断言与清理合同](../scripts/docs/Online数据保护.md#v45postgresql-分组三出口与方式撤销) · [当次交付与默认历史](../scripts/docs/Online数据保护.md#v46分组当次结果与默认历史分开核对)

<a id="v45-postgresql-分组三出口隔离接入2026-11-11hosted-未运行"></a>
<a id="v45-分组首失败与-v46-当次历史消费者修正2026-11-11"></a>
<a id="v46-唯一复验的外部-dns-准备失败2026-11-12-审计"></a>
<a id="v46-分组所选-transfer-的签发前提缺口2026-11-12hosted-首失败保留"></a>
<a id="v46-分组-transfer-主动恢复的-put-前提缺口2026-11-12"></a>

| 固定业务源码及实际 Job | 首败与清理 |
| --- | --- |
| `7a91abaee4cfaefbf35b32776452240c7e3b47fe` · [130472411237](https://github.com/pampa0629/addp/actions/runs/43005665942/job/130472411237) | v45 在第七阶段失败，消费者将 Develop 的默认历史与角色当次输出比较。分组最终报告与页面缺失，六项许可已撤销，Infra 零残留。 |
| `7faff9ab8b24e0678c465325491ca69b84c39f82` · [130619097907](https://github.com/pampa0629/addp/actions/runs/43048558678/job/130619097907) | Playwright 系统依赖下载遇 DNS 失败。业务和浏览器未进入，许可未签发，隔离环境清理通过。 |
| `7faff9ab8b24e0678c465325491ca69b84c39f82` · [130924454141](https://github.com/pampa0629/addp/actions/runs/43140011471/job/130924454141) | Transfer 启动时执行授权签发 403，未创建 Worker 执行。夹具缺 `develop.data_write.execute`。无最终报告、0/23 图，六项许可已撤销，Infra 零残留。 |
| `2664dd82881a553f6634afbbb4a2c3389803d9f0` · [131054875358](https://github.com/pampa0629/addp/actions/runs/43180455765/job/131054875358) | 显式移除旧选择时任务 PUT 返回 403／`permission_denied`。夹具缺 `transfer.task.update`。无最终报告、0/23 图，六项许可已撤销，Infra 零残留。 |

两项既有 Permission 仅补给该套件的一次性消费账号。来源许可、原值授权和产品权限按各自契约判定。

<a id="v45-完整标准门禁补证2026-11-11t4-待回传"></a>
<a id="v46-精确修复候选-linux-标准门禁2026-11-11非-hosted"></a>
<a id="2664-精确候选的-linux-platform-自然门禁2026-11-12非-hosted"></a>

**门禁边界。** v45 专项目标通过，早期本地聚合出现 Metric／Graph 包装失败，同候选后来有完整入口退出 0 的证据。v46 固定候选 `7faff9ab8b24e0678c465325491ca69b84c39f82` 的 [Platform 130516352816](https://github.com/pampa0629/addp/actions/runs/43018760077/job/130516352816) 和 `2664dd82881a553f6634afbbb4a2c3389803d9f0` 的 [Platform 130977934296](https://github.com/pampa0629/addp/actions/runs/43156617594/job/130977934296) 实际通过。补任务更新权限后，本地 Graph 包装仍有三个 60 秒子进程超时，完整入口失败。新版的真实 T4 只按上面的最终 Hosted 证据认定。

[整理前的完整记录](https://github.com/pampa0629/addp/blob/f99ee03a999e6818fd9378beb7f5a6fd867adbb9/security/CLAUDE.md#L972)。

### Owner 投影存储首次并发初始化的目录读取修正（2026-11-12，T2）

自然Run43143870160／Job130939582473、源码5df18e46507ada2f00cd5acc43cabbc126377b45的TestProjectionStoreMigrationIsSerializedAgainstPostgres实际失败：CREATE SCHEMA IF NOT EXISTS projection_store_concurrent_it触发SQLSTATE23505／pg_namespace_nspname_index。原日志4962–4968和数据库日志5237–5238保留于/tmp/addp-ci-1200-1112.J8xJkB/logs/130939582473.log，不用后继偶发通过覆盖。原样本地projection-store标准组、32轮及256轮临时诊断曾通过，均只作排障观察，不算修复；临时探针仅在/tmp并经原标准入口的Go overlay运行，未改数据库清单或产品进程。

加强原并发用例为八个独立连接全部就绪、先各自观察schema缺失后同时迁移，并在失败时保留PID、锁及DDL时序。该条件在本地真实PostgreSQL复现同一23505；时序证实锁逐个放行，却不能直接依赖后续CREATE的缺失缓存检查。按既有事务／schema迁移锁契约先完善规范，再将schema存在性判断改为锁后独立SQL读取pg_catalog.pg_namespace，确认不存在才创建。迁移锁、版本和结构核验不变，无重试、吞错、Worker DDL或第二条迁移路径。

最终同一测试输入的红绿校验走make test-common-postgres COMMON_POSTGRES_TEST_ARGS='--test projection-store'：用Go overlay仅还原5df原schema.go时退出2并重现23505；恢复修复源码退出0，投影存储完整子组无跳过，涵盖同构／历史／漂移、未来生效历史、四出口原因矩阵、首次并发初始化和版本提交锁竞争。不是完整Common PostgreSQL全门禁。make test-module MODULE=common GO_ONLY=1退出0；make test-projection-store-ownership退出0（9项／5504个Go或SQL文件），git diff --check通过。独立输入、红绿日志、原CI元信息和摘要/tmp/p145-projectionstore-fix-independent/{red-green.json,verification.json,red.log,green.log,first-ci-job.json}；Go及唯一归属日志/tmp/p145-projectionstore-{common-go,ownership}.log。既有自动发现和Common PostgreSQL CI已覆盖，无新增workflow。此修正尚无后继Linux自然CI或运行验收，不启动个人服务、不申请重启，也不消耗／重派Hosted授权。


### Owner 投影存储修正的 Linux 完整 PostgreSQL 自然门禁（2026-11-12）

精确修复7d51762d5054ea14d774cc68040d71adacf8f947的push [Run43155459026／Common PostgreSQL Job130974521958](https://github.com/pampa0629/addp/actions/runs/43155459026/job/130974521958)，attempt1已实际success。独立读取原始Job API，确认head SHA、completed及Run Common PostgreSQL gate步骤success；原日志339行完整checkout SHA一致，491–492行实际执行make test-common-postgres，无子组选项，不以summary或容器清理替代。原并发初始化测试4988行实际PASS；投影存储八项顶层全部PASS，原始日志无SKIP及error annotation。这是修复后新的完整Common PostgreSQL T2门禁成功，原5df并发首失败及本地红绿证据保留。

原件/tmp/addp-ci-security-2664-l_naer3d/common-130974521958.log，SHA256为3fe428ef81b21dbf240ebe08b927f6d6bdaf5c3fa879fc1e433fd65b27bf55df。独立脚本、Job元信息和结果/tmp/p145-common-natural-independent/{audit.py,job.json,audit.json}，脚本实际退出0。已核7d为2664dd82881a553f6634afbbb4a2c3389803d9f0祖先，common全目录、根Makefile、release工作流、Common PG入口、其容量脚本、Graph backend、go.work/sum及Infra端口核验输入均无差异，故该T2结果可适用于2664候选；只比较既有入口依赖，不调查栅格或其他suite。

本轮只补证据，未重复运行测试或Hosted、未启停个人服务。精确2664的Platform自然门禁仍由专门CI会话跟进，本轮未认定其通过；v46/v10新的完整T4尚未获单次授权或派发，原403与0/23图保持。下一步由专门CI会话核对该Platform结果后统一提出具体单次方案，不重复确认或自动重试。


### Service 保护拒绝后旧响应回流修正（2026-11-12，独立T1/T3）

不等待本次v46 Hosted，按已有“保护拒绝清旧行且持续显示原因”契约，核查Service页面在途响应归属。标准隔离浏览器的中英文正式Vue页均重现：第二请求403到期后，第一个旧200重新显示phone行及游标；原始失败保留。仅前端loadPreviewData增加当前请求／定义归属，旧成功、失败、finally不覆盖新展示，定义改变或停用后不接受旧结果。三个单元红例转绿；make test-service-frontend的123项／构建及Console标准隔离浏览器4项实际通过，make test-frontend-ci-registration通过；未改后端／权限／接口／Hosted套件，不操作个人服务或Chrome，也不把受控403当真实自然到期T4。细节与证据统一见Service [迟到响应专题](../service/docs/testing/查询与要素协议.md#保护拒绝后的旧预览响应回流2026-11-12)。


### Manager 预览归属核对与 Develop 原值截止修正（2026-11-12）

按用户继续核查迟到响应的授权，不查询或调度CI。Manager主预览已有请求序号与资源／子项身份判断，clearPreview失效两条请求；新增5项反例核对同资源新保护403后的旧成功／失败、父／子清空后的在途结果，82文件／414项单元通过，Manager产品代码未改。不能从该局部核对推断所有Manager出口或浏览器T4通过。

Develop现有原值复核在等待HTTP期间缺独立截止清空，且会展示携带已过截止的迟到原值；两个旧实现单元真实报红。仅修正queryPlaintextRefresh为截止与复核分别计时、拒绝已过期原值响应，并保持取消／退出失效与脱敏恢复。定向7项通过；正式Vue工作台的中英文2项隔离浏览器实际等待8秒截止，挂起5秒复核并核对清空、迟到200不回流及再次执行保护结果，通过。不修改系统或浏览器时钟，不改后端、权限、授权期限／历史规则；这是T1/T3，不是实际授权自然到期HTTP或T4。完整make test-develop-frontend实际退出0：test:workflow链、290项浏览器及构建通过，含两项截止测试；前端CI登记27项通过。用最终同一7项断言隔离回放旧控制器时3过4失败，未修改工作区源码，证据/tmp/p145-plaintext-deadline-independent/。细节与日志见Develop CLAUDE.md末节及/tmp/p145-develop-plaintext-deadline-{red,green,browser,frontend}.log。


### Develop 查询切换的旧提交归属修正（2026-11-12）

继续独立前端核查，正式 Vue 同组件切换任务后旧执行仍锁住新任务；最终同一 4 项单元断言隔离回放旧代码均失败，覆盖迟到预检／取结果失败、旧创建回执和失效确认。现沿既有执行序号失效旧观测，在路由切换时清除旧结果／原值复核与加载状态，防止旧响应覆盖新保护拒绝或过时确认继续提交。后台已提交执行、引擎切换限制和所有授权不变。定向 4 项及中英文三边界共 6 项受控浏览器通过，首次夹具混入自动诊断的失败保留。完整 make test-develop-frontend 实际退出 0，test:workflow 链、296 项浏览器及构建通过；前端登记 27 项／21 个模块通过。仅 T1／T3，不新增真实身份或 Hosted T4，不自行操作 CI 或个人服务。规范见实现规范 §10.2，产品与日志见 [Develop 记录](../develop/CLAUDE.md#查询任务切换后的旧提交失效2026-11-12)。


### Develop 导出切换与共享取消交付修正（2026-11-12）

查询清空／替换、任务切换及退出结束旧导出的前端观测，复用既有请求作用域和单一共享导出工具。迟到创建／轮询／文件不继续交付，旧失败或 finally 不污染新任务；共享工具在状态返回与 Blob 保存前复核取消。后台导出会话、授权和保护规则保持。最终 16 项同断言旧代码 10 过 6 失败，修后全部通过；中英文 8 项受控浏览器及共享 378 项通过。完整 Develop 标准前端门禁的工作流链、304 项浏览器及构建通过；Manager 消费方 414 项单元、358 项浏览器及构建通过；前端登记 27 项通过。无新增 Hosted 或真实账户 HTTP 验收，不自行跟进 CI、不启停个人服务。细节见[Develop 记录](../develop/CLAUDE.md#全量导出在途观测失效2026-11-12)。


### 8a068 自然前端门禁闭环（2026-11-13）

固定 `8a0682d92e9b46f53dbd6934965b4553d3cae226` 的 Run `43243077330` 成功，离线核对共享前端、Develop、Manager 与 Monitor 四个相关 Job 均通过。[自然CI原件与审计](../docs/spec/testing/evidence/数据保护与执行血缘历史证据.md#8a068-导出观测修复的自然前端-ci2026-11-13) · [各 Job 与本地计数](../docs/sessions/数据脱敏与保护后续工作专题.md#三验收事项与本次闭环)。该轮覆盖 T0／T1／受控 T3 及构建，`45fe` 的限定 T4 独立认定。Manager 页面归属见[后续修正](#manager-全量导出归属修正2026-11-13)。


### Manager 全量导出归属修正（2026-11-13）

跨模块保护核查发现，旧导出创建回执、异常／finally 和 `resource-actions` 响应可能影响已切换的资源。取消规则、旧源码对照及原轮 T1／受控 T3 结果统一见[Manager 专题](../manager/docs/数据预览与资源树实现规范.md#全量导出观测与动作能力归属2026-11-13)。固定版本 Job、实际覆盖和原件见[后续 CI 证据](#f924-manager-自然前端证据2026-11-13)。


### f924 Manager 自然前端证据（2026-11-13）

f92420a13fd3a0e1c9e3cf8125f133c8b4362bec的Run43342310763／Manager Job131613299658已独立离线核对实际success、83文件426单元、366浏览器（含8新增中英文场景）及构建通过，12专项亦实际执行。只是本Job的自然T0/T1/T3，不推断整体Run、不处理无关CI、不新增Hosted。正式原件及命令见[证据正文](../docs/spec/testing/evidence/数据保护与执行血缘历史证据.md#f924-manager-导出归属修复的自然-ci2026-11-13)，后续仍优先核查Transfer来源切换的角色候选归属。

### Transfer 角色候选来源归属核查（2026-11-13）

现有Step4序号保护通过9项实际Vue watcher行为检查和中英文6项受控页面回归：来源切换后旧候选／失败／finally不覆盖当前状态，折叠和不支持的来源／模式使旧观测失效；保留旧选择并提示不可用，主动移除后清除。没有重现需修的产品缺陷，仅补既有标准门禁中的测试；四组临时变异均被拒绝，不把变异失败称为原产品失败。完整`make test-transfer-frontend`139项单元／75项浏览器／构建通过，前端登记27项及21前端通过。未改角色、权限、API、执行契约或共享库，不启停个人服务、不新增Hosted、不自行跟进CI。证据及范围见[Transfer角色选择记录](../transfer/CLAUDE.md#角色专用导出选择)；下一步优先核查Service编辑页在表／SQL／参数变化时的角色候选归属。

精确0d1da0e2f的自然Transfer Job131661059381（Run43356929114）现已离线独立核对success：139单元含9专项、75浏览器含6新增中英文场景及构建实际通过。整个Run并非成功，仅确认所属Job；未检查或处理其他CI。原日志哈希、步骤和审计命令见[自然CI证据](../docs/spec/testing/evidence/数据保护与执行血缘历史证据.md#0d1-transfer-候选来源归属的自然-ci2026-11-13)。无新产品、运行验收或Hosted范围，后续优先级保持。

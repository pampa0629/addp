# ADDP 授权上下文规范

更新日期：2026-10-17

本文定义统一身份解析、权限判定和跨模块授权边界，只保留有效规则。接口字段以 Schema／Swagger 为准，数据库与事务实现归 owner 文档；不在本文累积开发过程、迁移编号或逐次验收记录。DataItem 授权的已确认目标见 5.5.7，实施范围见第九节。

## 一、目标

- System IAM 唯一维护 Principal、账号与外部身份、Tenant、Membership、组织、Permission、Role、Role Assignment、会话及令牌。OAuth Client 只代表客户端软件，不创建另一套用户或租户。
- 业务资源的 Grant、Policy 和最终访问判断归对应 owner；源数据规则唯一归 System 引擎访问控制领域，不等于 System IAM 接管所有业务 ACL。
- 业务模块只消费 System 解析出的 AuthContext，不自行解析 JWT、OAuth 或委托令牌；Scope 只能缩小已有权限。

概念依据：[账号与权限体系](../concepts/addp账号与权限体系图.md)、[术语表](../concepts/addp术语表.md)。

## 二、唯一调用链

请求携带 Bearer → System 校验令牌及当前 IAM 事实 → 返回 AuthContext → 共享认证中间件注入上下文 → owner 校验功能、Scope、资源、条件与必要审批。

唯一解析接口为 `GET /api/v1/system/auth/context`。`GET /users/me` 只提供用户资料，不得用于验证令牌。客户端提供的 Principal、Tenant、Membership、Role 或 `user_type` 不能成为身份依据；缺失、零值或空 Tenant 不表示全租户。

引擎数据授权工作区在令牌轮换、重新核验 AuthContext 期间暂停交互、隐藏草稿和查询结果，并停止旧异步链继续提交。尚未提交的草稿可仅保留在组件内存中，不持久化、不作为授权事实；不得因为暂时的空上下文销毁工作区。核验完成后，只有 Principal、Context／Membership、授权版本、当前权限及目标引擎状态均未变化才恢复原草稿；真实边界变化、登出或核验失败仍清除旧工作区。保留草稿不能保留旧权限，所有操作仍以当前 AuthContext 和服务端裁决为准。

## 三、AuthContext 语义契约

唯一版本为 `addp.auth_context/v1`。机器契约见 [JSON Schema](../../common/authorization/schemas/auth-context-v1.schema.json)，共享实现位于 `common/authorization` 与 `common-python/addp_common/auth.py`；本文不复制完整结构。

| 字段 | 含义与约束 |
| --- | --- |
| `schema_version` | 固定为 `addp.auth_context/v1`，不按客户端协商双 Schema |
| `principal` | `user` 或 `service_principal` 及稳定 ID，不返回账号资料 |
| `context` | `platform`／`tenant` 判别联合；Tenant 模式绑定唯一 Tenant Membership |
| `authentication` | 认证方法、AAL、认证时间与 step-up 有效期；User 进入 Platform 至少 AAL2 |
| `client` | Client、audience、Scope 模式与范围，不提高 Principal 权限 |
| `organization` | 当前 Tenant 的直接部门／项目组成员关系；Platform 为空 |
| `authorization` | 当前授权版本与有效 Role Assignment，包含 Permission、Scope、来源及有效期 |
| `token` | 类型、签发与到期时间；时间使用带时区 ISO 8601 |
| `delegation` | 仅委托令牌携带委托 Client、AgentRun、ToolCall，其余为 null |

IAM bigint ID 使用非零十进制字符串；对象拒绝未知字段，数组遵循 Schema 的稳定排序。Platform Context 只激活平台角色，不携带或隐式激活 Tenant 业务权限。

| 凭据 | 固定约束 |
| --- | --- |
| 第一方 Web | `addp-web`、`addp.api`、`unrestricted`、空 Scope |
| OAuth User | 真实 Client、`addp.api`、`restricted`、已批准 Scope |
| Browser Resource Access Ticket | 所属第一方 Browser Family 的当前身份；`addp-web`、唯一 owner audience、唯一 `resource:read`、无 delegation |
| Delegated Access Token | 回溯源 Access Token／Family；真实 Client、唯一 owner audience、当前 Tool 的精确 Scope；委托 Client 必须等于源 Client，不接受自报 |
| Service Access Token | 独立 Service Principal、显式 Platform 或单 Tenant Context、受限 Runtime 能力 |

## 四、身份和上下文校验

System 返回 AuthContext 前必须同时验证：Token Hash、期限及撤销状态、Token Family、Principal 类型与有效性、Context、Tenant／Membership、当前授权版本，以及 Client、audience、Scope、认证强度与委托绑定。任一失败均不返回部分上下文。

Role／Permission／组织授权关系变化在同一事务推进 Principal 授权版本，旧派生凭据失效。仍有效的 Refresh Token Family 只能在唯一轮换事务中复核当前身份后推进版本；User、凭据、Membership、Tenant 或 Family 失效时须同时撤销相关 Family，不得用版本推进恢复。

## 五、权限计算

有效访问是当前身份、Context、Tenant、audience、Scope、Role Permission、owner Grant／Policy、上下文条件、必要审批的交集，且不存在 Explicit Deny。

- 默认拒绝，Deny 优先；平台角色、连接凭据、管理员名称不产生数据读取权。跨租户聚合统计仍须独立 Statistics Permission。
- Grant 不批量写入 Token 或 AuthContext。部门父子默认不继承；项目组权限不传播到成员所在部门。
- 临时授权须有期限、来源及撤销事实。普通长期只读必须显式选择，不能把遗漏期限解释为永久。
- Delegated Token 只进入 Tool Manifest 声明的 owner 路由，唯一 audience、Scope 精确集合和 Permission all-of 均匹配；额外 Scope 也拒绝。普通 User 的原有门禁与写操作审批不能被委托守卫放宽。
- Resource Ticket 只进入显式挂载 Guard 的 GET／HEAD 原生资源路由，从 `addp_resource_access_ticket` Cookie 读取；出现 Authorization Header 即拒绝。Guard 核验票据类型、owner、唯一 `resource:read` 和 Permission all-of，Handler 继续核验资源及 Assignment Scope。System 仅 AuthContext 解析入口接受该票据，其余管理 API 拒绝。

### 5.1 同步 BFF 授权

BFF 仅在当前同步调用栈转发已经验证的 User Bearer；下游 owner 重新认证并裁决，不信任自报主体，不保存、缓存、记录或后台使用该 Token。BFF 的机器身份仅用于明确授权的最小投影，不能代用户作业务决定。

Manager 文件上传后的元数据同步采用这一同步 BFF 路线：`POST /api/v1/manager/uploads` 仅允许 Tenant User，并在任何文件写入前同时核验 `manager.data_item.create`、`meta.scan_task.execute` 与 `meta.scan_task.read`。Manager 在当前上传请求栈内，将已验证的当前 User Bearer 交给独立的 Meta 用户扫描客户端，调用唯一的 `POST /api/v1/meta/scan/run/manual`；Meta 独立认证和裁决权限，从 AuthContext 记录真实发起人。只提交本次实际写出的精确 `ref_groups`，不得扩大到父目录。凭据只作为本次 HTTP 调用参数，不保存到客户端、上传结果、execution、日志或 Worker；不因用户请求失败改用服务身份重试。原有 Meta Service Client 保持仅服务身份使用。上传响应只返回文件定位与规范 `scan_execution_id`，不返回内部 TaskExecution；浏览器继续通过既有 Meta 用户执行接口等待终态，其他用户、跨租户和没有发起人证据的记录保持不可读。文件已写入后，扫描提交失败统一携带稳定错误码 `upload_scan_submission_failed`；下游认证失效返回上传扫描失败的 `503`，不转成 Manager 的 `401`，防止浏览器刷新凭据后重放整个上传；下游权限拒绝和范围冲突分别保留 `403`／`409`。上传与异步扫描不是跨服务原子事务，扫描提交或执行失败必须明确反馈，不回滚已写文件、不把失败当成功，也不自动重试上传。

Monitor 任务诊断同样由任务 owner 复核当前用户范围；Monitor 功能权限和历史执行授权不替代 owner 读取权限。平台日志正文独立要求 Platform Context 与 `platform.module_log.read`，不混入租户任务或操作审计。具体入口见 [System 模块说明](../../system/CLAUDE.md)和 [Monitor 模块说明](../../monitor/CLAUDE.md)。

### 5.2 计算执行授权

1. 用户计算先检查入口功能及本次 `read | write | ddl | external_effect` 的资源条件，再由 System 签发绑定唯一 execution 的 Execution Authorization；它不是新 Token 类型。
2. 来源为互斥的 `user` 或 `service_definition`。用户来源保存 Principal、Membership、授权版本；服务定义来源保存 owner、不可变 definition version/hash 与签发服务。共同绑定 Tenant、audience、execution、范围和期限；Notebook 派生来源还保留 Session 授权关联。
3. Engine Access Scope 必须逐引擎绑定非空效果集合，禁止独立 Engine 集合与效果集合形成笛卡尔积。已发布服务的数据源绑定纳入 definition hash，不按当前名称重绑。服务定义签发仅开放 `addp-service` 的本 Tenant 已发布查询服务只读范围。
4. 消费者必须是匹配 audience 的当前 Runtime Service Principal；每次核验 Tenant、来源当前有效性、授权期限及本引擎效果。Runtime 不获得通用引擎明文读取或用户 Role。DuckDB Runtime 仅持执行授权消费权限，从当次消费取得精确 Engine Access。
5. 交互／异步执行固定真实发起人；定时任务的授权主体只能由同 Tenant 当前 User 在创建、更新或显式重新授权时写入，并绑定任务定义。执行前重核当前资格；定义或授权版本改变不得沿用旧依据，不保存或代传原始 User Token。
6. 跨模块子执行须核验父子 execution、Tenant、主体、Membership、授权版本及来源链。静态范围只在 pending 签发，每个 audience＋execution 唯一；动态范围只在真实 running attempt／lease 下签发，每个 attempt 唯一并绑定租约。消费重核当前租约，新 attempt 不复用旧授权；Orchestrator 只编排，不取得数据效果权限。

执行结果读取是独立授权动作，不以历史 Execution Authorization 作为当前访问凭据。Develop 专业结果限当前 User 本人，Tenant、Principal、Membership、授权版本与原来源匹配；缺失来源拒绝，已完成结果不以旧执行授权期限作为 TTL。`develop.task.read` 之外，query／workflow 要求 `develop.data_read.execute`，script 要求 `develop.notebook.read`；导出、轮询、日志及下载均回到原 execution 复核，不自动支持分享。TaskProvider 机器读取仅向固定 Orchestrator 返回可信父子链上的安全状态与稳定 outputs，不返回执行配置、原始结果或授权引用。

上述执行结果不等同于写入业务引擎后形成的独立 DataItem；产出数据的初始使用权见 5.5.8。具体执行协议见[登录认证统一要求](addp登录认证的统一要求.md)和[任务体系规范](addp任务体系规范.md)。

### 5.2.1 内部任务执行授权

`internal_task` 与 Engine Access Scope 互斥，空 accesses 不是许可。当前仅允许 Tenant User 为 Ontology `semantic_projection` 签发，绑定 resource、revision、digest、generation，并核验发布 Permission 和真实 pending execution。仅 `addp-ontology` 可按同一 running attempt／lease 消费；不接受 Notebook、服务定义或 Orchestrator 派生，不返回 Infra 凭据，也不代表投影 ready。Ontology 仍负责业务资源与激活校验。

### 5.2.2 Manager 剖析执行授权

剖析不属于内部任务。其授权除逐引擎 read 外，须绑定 `source_read_scope`：完整持久配置摘要与 Provider 的规范完整 ReadSet。摘要唯一由 `common/execution.NewManagerProfileReadScope` 计算，输入不超过 512 KiB；只消除 JSON 键序／空白差异，不经浮点数改写。当前范围为普通第一方／OAuth API User 的单引擎手工剖析；System 从真实 pending execution 取证，核验 `manager.data_profile.execute`、`manager.data_item.read` 及全部源规则，不接收自报来源或用当前 Meta 补造历史依赖。

消费仅限 `addp-manager`，绑定同一 execution、attempt、lease、配置与 ReadSet，重核当前 IAM、Grant、Deny 和期限。禁止经仅提交 Engine ID 的通用路径消费，也不从 Notebook／服务定义／父 execution 自动派生。Manager 执行同一计划并施加 Security；剖析结果读取须使用生成时冻结的真实来源，不能以今日依赖替换。

固定签发与消费入口分别为 `auth/execution-authorizations/manager-profiles` 和 `execution-authorizations/{id}/manager-profile-accesses`。签发只接受 execution 与有限 TTL，消费只返回不可缓存的当次观察时刻，不返回连接或通用许可；共享 DTO／校验归 `common/execution`，客户端不自动重试、不跟随重定向。精确字段见 [System Swagger](../../system/backend/docs/swagger.yaml)，实现与验证范围见 [IAM 数据模型规范](../../system/docs/IAM数据模型与迁移规范.md)。

### 5.2.3 FTP 单体模型快显执行授权

本期只对真实 FTP 引擎的 IFC／3DS 单体 GLB 生成接入该链，不改 NFS／MinIO 的既有执行入口，也不宣称后两者已受本期源数据授权控制。普通 User 的当前身份、`manager.derived_artifact.create`、`manager.data_item.read` 和完整 DataItem 组成规则均须成立；不接收客户端自报的 Actor、FTP 类型或 Allow。历史人类 ActorOrigin 先由 Manager 保存，不因绑定 EA 覆盖为机器身份；本期不从 Orchestrator 父执行、Service 定义或任务创建人补造来源。

Manager 在真实 pending 行可领取之前，冻结 `model3d-glb-ftp-config/v1` 配置：保留完整原始 `source`／`result`／`options` 与转换字段，增加 `config_version`、`owner_task={id,version}`、`source_engine_version` 与 `source_composition`。ID／版本采用规范十进制字符串，任务配置取同一 SQL 快照；结果是当前 execution 的独占对象，不提前覆盖旧快显。`common/execution.ManagerModel3DReadScope` 保存完整原始 JSON 的 `config_digest`、`owner_task_id`、`owner_task_version`、`engine_version` 与 `file_composition`。摘要输入不超过 512 KiB，只规范 JSON 键序／空白，保留精确数值并拒绝重复键；不能经过 `JSONMap` 浮点往返或把摘要自身嵌入配置。逐文件大小在消费后作为本 attempt 的 I/O 完整性事实取得，不回写已冻结配置，也不进入授权边界摘要。

固定签发入口为 `POST auth/execution-authorizations/manager-model3d-glb`，当前 User 只提交原 execution UUID 和最长 3600 秒 TTL；System 从真实 pending、任务标识／版本、完整配置及独立 Meta 事实取证，再核验当前源规则。重复核清只恢复原 execution 的原授权与原期限，不另造 UUID、续期或复活。消费为 `POST execution-authorizations/{id}/manager-model3d-glb-accesses`，仅当前 Tenant 的 `addp-manager` Service 可提交同一 execution、attempt、lease token 与完整 scope；重核当前来源、权限、Grant／Deny、期限、配置、组成和 Engine 聚合版本。响应只含 `observed_at` 及当次核验的同 ID、同版本 FTP Engine，连接仅在本次调用栈用于精确 Facts／Open，不保存到配置、结果、日志，不在消费后另取可能不同版本的连接。客户端不跟随重定向、不隐式重放 POST、不缓存许可；服务未接通、返回畸形或任何核验失败均不能打开源内容。

Manager 的配置／签发绑定／可领取发布和最终结果切换各自保持 owner 事务原子性，不跨模块持锁。未知签发结果只在原 pending 核清：同一同步栈、同一 User Bearer、总预算一分钟内至多显式核清一次原 UUID；明确拒绝不重试，仍未知只关闭自己未绑定的 pending，不保存 Bearer 或新建后台补偿。每次源 I/O、转换返回和发布前使用同一冻结计划重核；发布失败不替换原有效结果。EA 不是已完成快显的读取凭据：FTP 受管快显的实际内容读取须以当前 Human Bearer，在同一次 System 裁决中匹配生成时完整 file composition 和 Engine 版本与当前事实；不以两次本地预检查拼成许可，不要求生成任务仍存在或重用终态 EA，不用 Resource Ticket 绕过。capability 仍遵循 Manager 共用契约，仅陈述资源／组成／结果事实，返回 URL 不授予内容访问权。共享类型／SDK 可先固定供消费方编译，但正式 System 路由、持久化、owner 接线与真实 FTP 验收分别记进展，不以契约存在宣称权限闭环。

### 5.3 Notebook 会话授权

Notebook Session Authorization 是独立的短期用户派生事实，不是 Delegated Token。Develop 在 Session 创建的同步栈用当前 User Bearer 请求签发，随后丢弃 Bearer；授权绑定 Session、Task、Tenant、Principal、Membership、Token Family、授权版本、固定 `develop` audience、操作和期限，不保存连接或 Engine 白名单。

仅 `addp-develop` Runtime 可凭自身 Service Token 消费：目录操作当次核验；数据查询／扫描须为新 execution 派生单引擎 read 授权，保留 Session 来源。Session 到期不刷新；同 Family 正常 Token 轮换不影响 Session，但撤销、重用、身份／授权版本变化或显式关闭必须阻断消费并联动撤销有效派生授权。长扫描在短租约边界重核，失效关闭 Cursor；Develop 重启丢失本地 Session 后拒绝访问。authorization ID 只是引用，不是 Bearer。

### 5.4 Owner Resource Grant 与 Asset 履约

业务 owner 使用结构化 ResourceAccessRule 保存资源、主体、Permission、Allow／Deny、期限、来源与撤销；最终判断归 owner，不在 IAM、Catalog 或 Asset 建立全平台 ACL。源数据的特殊 owner 分工见 5.5。

Asset 只负责申请、审批、期限及跨 owner 履约：`pending → effective → revocation_pending → revoked`。只有 owner 幂等写入 Grant 后才能标记 effective；撤销／过期经 owner 确认后才标记 revoked。未知结果保持待核清，用原来源编号恢复，不回退软授权或专属凭据。

Data Application 的 Grant 只允许打开应用配置，内部组件查询仍由 Service 独立校验；未接入履约的资产类型不能标记 effective。Portal 仅是消费 BFF，不复制 Grant、Token 或运行配置。

### 5.5 目录责任、业务授权决定与源数据授权

源数据规则只由 System 引擎访问控制领域维护，直接批准与 Catalog 业务批准共用同一写入与读取判定路径。统一资源目标是 Meta 识别的 DataItem；5.5.7 定义目标边界，第九节区分规范与当前实现，不以原生表试点代表全平台已贯通。

#### 5.5.1 事实与职责分开

- Catalog 拥有责任关系与业务决定；业务负责人确认共享合理性，数据管理员和技术维护者承担各自责任。责任、编目维护、业务批准、授权办理和内容读取互不自动赋权。
- 首次保存有效 business_owner 的建立依据须持久保留，不因移交、弃用或恢复清除；它不是当前资格，也不自动启用业务批准。
- 决定绑定精确目标、接收主体、动作、期限、真实确认人及责任依据。首次受理重核确认人的原 Membership、原责任连续性和当前确认 Permission；其历史授权版本只作审计，无关版本变化不作废决定。办理人则须严格匹配原请求授权版本。原责任中断后同人重新接任不复活旧决定。
- 普通只读允许同人确认并办理，或显式批准本人／本人当前项目组受益；各项资格分别核验并留痕。不能推广到敏感原值、写入、DDL、Break-glass 或三员分立约束下的自批。
- 只读 Grant 与 Deny 显式选择 `at_time` 或 `until_revoked`：前者要求未来绝对时间，后者不带日期。遗漏、未知模式或矛盾参数拒绝；不使用远期日期／infinity 伪装长期有效，不默认续期，也不扩张委托或临时接入期限。
- 功能通过 Role Assignment 显式分配；业务确认与办理使用独立 Permission，机器 execute 不代替人类 create。角色模板、权限发布、守卫、审计与测试须同步，不自动补权。
- “数据授权办理员”内置角色统一组合直接批准与业务批准后的办理能力，保持两种命令的独立 Permission 和同一目标批准要求校验。业务批准仍须有有效业务确认；角色本身不授予业务确认、引擎管理范围或内容读取。角色目录与迁移规则见《权限与角色发布规范》。

共享结果读取也须独立控制：条目可见且具备 `catalog.entry.read` 与 `catalog.sharing_decision.create` 的原确认人可看本人确认结果，当前业务负责人可复核本条目历史；不要求同时有办理权限。办理人读取候选或本人原办理则须独立办理 Permission 与当前引擎管理范围。仅编目、部门／项目组关系不取得这些资格；候选只暴露必要身份摘要，不能借 Runtime 身份枚举其他范围或在查询中签发授权。

#### 5.5.2 接入阶段与 Catalog 独立性

- 管理资格为当前 Tenant 的授权管理员，或目标 Engine 的有效受托办理人；二者仍须具备当次操作 Permission。管理员无需委托自己，受托人不能转委托，管理资格不产生读取 Grant。
- 委托绑定一个 Engine 和一个有效 User Membership，必须有原因及未来到期时间，且不晚于 Membership 到期；不允许重叠、永久委托、隐式续期或恢复。创建／撤销同事务保存审计并使受影响旧会话失效；撤销保留历史。
- 引擎登记、发现、扫描与内容读取是不同动作。登记人、连接成功或模块缺席不推导初始管理／读取资格；接入不授权修改源库、安装触发器或创建源端角色。
- 临时接入只读必须限时；长期直接批准可显式选择直至撤销。不因缺少批准要求、业务责任或 Catalog 自动放行。
- Catalog 可选，不成为 System 启动、既有 Grant 读取或其他模块 Ready 的前提。已要求业务批准时，失联不降级为直接批准；停止或重新启用 Catalog 必须显式切换批准要求，不由部署、心跳或责任保存触发。

#### 5.5.3 责任变化与访问规则生命周期

责任移交或条目弃用阻止旧依据支持新的业务确认／受理，不自动撤销已有 Grant。修复责任通过显式治理权限进行，不把修复人自动变成业务负责人或数据使用者；认证、弃用等资源状态仍分别约束操作。

业务确认、System 正式受理、Grant 签发是三个事实。受理以 System 事务提交为分界，并须与责任变更、来源重绑、弃用和批准切换形成可验证顺序：变更先完成则重新确认；受理先完成仅允许原同次、同参数办理继续，仍重核办理人、管理范围与接收主体。

自动签发窗口为受理数据库时间起 5 分钟，且不晚于拟授权绝对到期时间；长期读取同样只有 5 分钟办理窗口。窗口不限制人员操作时间，也不是访问期限。提交前按数据库墙钟复核，重试不刷新窗口。已签发原请求只返回原结果；未签发且过窗须重新取得依据，不恢复已撤销或过期 Grant。

撤销须当前管理资格与独立撤销 Permission，通过一份有效记录定位并原子收回**同目标、同接收方、同动作的全部有效 Grant**，保留不可变历史、收回编号集合与审计。同操作者及原参数重试只恢复原收回结果，不收回后来新授予。个人与组织是不同关系，撤销个人来源不删除组织来源，也不隐含 Deny。

Explicit Deny 的建立／解除使用各自独立 Permission 与引擎管理资格；提交即生效，建立时间不是预约生效时间，时钟回拨不得忽略仍有效的拒绝规则。个人 Deny 优先于同目标的组织 Allow；解除不授予、恢复或续期 Grant。Grant／Deny 自然到期后不追加首次撤销／解除；到期前已成功的原参重试可恢复历史，仍须当前操作者有资格。引擎停用、Meta／Catalog 不可用不阻止合法收权，收权不读取源内容。

#### 5.5.4 逻辑资源目标与执行约束

- 授权针对逻辑 DataItem，复用 Tenant、Engine、逻辑路径与现有指纹，不追踪物理创建实例。外部同路径重建不因物理替换自动撤权，但必须证明当前布局与组成仍在原授权边界内。改名、来源重绑或换 Engine 不自动转移规则。
- 目标缺失时访问失败，不删除原规则；重新出现也不恢复已撤销权限、续期或扩大范围。Fingerprint、Meta 行号、CatalogEntry ID 本身都不是许可。
- 查询须由 Provider 以同一个 PreparedQuery 证明完整 QueryReadSet，再核验所有依赖并执行该计划。文件读取遵循已识别的组成边界；解析失败、依赖不完整或执行范围不可约束时拒绝，不用正则猜测，不暴露宽权限源凭据。
- System 规则观察在同一已提交快照及数据库时间中核验可信凭据、当前身份／授权版本、功能条件、Engine、直接组织成员关系、Grant 与 Deny；结束前复核自然到期。任一实际目标不满足即拒绝，父部门、责任、委托或历史办理人不推导读取许可。
- Manager 预览与剖析结果检查均固定要求 `manager.data_item.read` 的当前 Tenant Assignment；缺少资源归属证明时，组织范围分配不能升级为 Tenant 范围。两者接受第一方／OAuth API User，只有预览另接受精确 `audience=manager`、唯一 `data.preview` Scope 的 Delegated Token；均不接受 Service Token 或 Resource Ticket。入口不允许客户端自选 Permission 或自报主体。
- 返回的 `observed_at` 只是当次观察，不是访问令牌、缓存 Allow 或完整 Security 裁决；实际 owner 继续施加 Security 并约束真实执行。规则底座保持私有，其他 owner 须定义固定操作契约，不能调用通用自选权限接口降低门槛。

#### 5.5.5 批准要求、可选治理与明确交接

精确目标批准要求的只读观察须区分未配置与依赖故障：已核验当前用户及引擎管理资格后，目标缺少批准要求返回 HTTP 404 和 `approval_requirement_not_configured`，提示尚未配置批准方式；不得误报为认证服务不可用。该响应不自动初始化或授予访问，跨租户或不存在的引擎仍维持原资源隐藏语义。

同一目标只有一份 System 权威批准要求，与 Grant 使用相同资源身份。首次须显式初始化；已有安排只能经带并发版本、原因和独立更新 Permission 的切换命令变更，不能重新初始化覆盖。配置权限不产生 Grant；切到直接批准还须核验操作者的直接授权能力，承接人仅作当次资格与审计，不是永久批准人白名单。

直接批准不要求 Catalog；业务批准须取得当前有效业务依据。切换只改变后续新办理，不修改既有 Grant 或此前已正式受理的原办理。旧版本冲突不得自动替换参数重试；保存业务负责人、恢复 Catalog 或来源同步不自动改变安排。

跨模块受理与恢复遵循以下唯一协议：

1. Catalog 先在本地事务持久化精确请求及待核清依据，提交后发送；请求固定租户、可信调用服务、原办理人／Membership／授权版本、目标、决定、批准要求版本与完整参数。
2. System 以独立服务身份反查已持久业务依据，再进入本地资格与目标事务；不在持锁期间跨模块反向联网。浏览器自报批准、身份或时间不能作证明。
3. 当前资格按 Principal 去重升序 → Membership／Tenant／接收组织 → Engine／委托 → 请求／精确目标取锁，等待及提交前按数据库墙钟重核。确认人的责任连续性归 Catalog，IAM 资格归 System；接收组织可无成员，不复制个人 Grant，也不冻结受益账号的授权版本。
4. 待核清期间，影响依据的责任、弃用和来源变更必须阻止或暂缓；人工与后台同样执行。其他条目、普通说明、Ready 与既有读取不受影响。不能仅靠跨服务时钟或短暂本地锁证明顺序。
5. System 串行裁决同编号受理与关闭，结果不可改写。只读 resolve 不产生关闭／签发／延期；仅明确 `found=false` 才可关闭原请求，404、拒绝、超时或畸形响应不得当作未找到。
6. 超时、重启或窗口到期不清除待核清保护。关闭及历史核清仍检查当前调用资格，但不要求原人员继续有效；未知结果先查原事实，不换编号或补参数。
7. 前台与后台共用签发续办路径：accepted 后先查签发历史，明确未找到才同参签发；closed、已有签发或明确过窗才结束对应调度。受理核清与签发核清独立，解除责任保护不等待签发；Catalog 不保存第二份 System 状态、Grant 或窗口，也不在 GET 中续办。

签发只读查询在原 `found/grant` 历史之外，未签发时可返回 `unissued_reason=relation_exists|window_expired|closed`，说明本次观察的签发阻碍；无阻碍时省略。System 在同一已提交只读快照中匹配原请求、查询原签发、按数据库墙钟核验原窗口，并检查同租户、引擎、DataItem、接收方及动作的现存有效授权关系。原签发历史优先于当前阻碍；窗口结束优先于重复关系。原因不是持久失败历史，也不是当前访问 Allow，不返回其他 Grant 身份。Catalog 动态透传该原因，后台观察到重复关系时暂不重复签发，仍仅在原窗口内观察恢复；原窗口结束则核清终止续办。页面区分“已有授权，本次未重复签发”和“原窗口已结束，本次未签发”，不再统称等待签发，不由浏览器时钟推断终止，不自动撤销、覆盖、续期或新建请求。

规则写入、撤销与高风险审计须同事务；绑定冲突、未找到、资格失败和依赖故障分别处理。精确协议见[企业资源目录实现规范](addp企业资源目录实现规范.md)与[System IAM 数据模型规范](../../system/docs/IAM数据模型与迁移规范.md)。

#### 5.5.6 直接批准与授权管理

- 日常办理为“选数据 → 接收方 → 操作与期限 → 确认”。首次直接批准配置与 Grant 可在同一显式命令中原子提交；已有业务批准不能被批量直接批准覆盖。接收方为当前 Tenant 的有效账号、部门或项目组，组织按当前成员关系生效，不展开个人副本。
- 同一 Tenant、目标、接收方类型／编号、动作构成一条授权关系，不按批准来源或签发者拆分。已有有效关系时拒绝新命令重复签发，不静默改期限；原命令同参重试只恢复历史，异参冲突。修改期限须明确收回并按当前批准要求重新授予。
- 批量只覆盖本次明确选中的独立目标，不包含未来新增资源。各项独立原子提交，显示逐项结果；失败／未知保留原命令，不能伪装整批成功或自动换编号。
- 当前授权合并有效来源；选定账号时展开其个人和当前有效部门／项目组来源。全部授权记录包含有效、已撤销及已到期记录；历史账号筛选只匹配直接接收方，不按今天的组织关系改写历史。撤销优先于到期分类。
- 表／路径、接收方类型及具体接收方条件在计数和分页前取交集。文字搜索与精确资源选择互斥，条件变化自动查询并隔离旧响应；撤销定位具体来源，不对账号聚合行整体收回。只显示有权读取的身份名称，不要求手填 ID。
- 当前关系或 `rule_covered` 只说明规则来源覆盖，不保证实际可读；页面说明来源、期限及拒绝状态，不混淆新增办理、当前查询与全部记录。查询无副作用且不可缓存，不能借查询取得内容或办理权限。
- 具体账号的当前查询同时保留只有有效 Deny 而没有 Grant 的数据项，避免撤销最后一条 Grant 后丢失解除入口。拒绝来源只展示同一快照中仍适用于该账号的规则编号、接收主体及期限，不返回原因、建立人或历史正文；沿用授权查询权限与引擎管理资格。建立／解除仍分别要求独立 Deny Permission，解除按具体规则执行，组织规则须明确提示影响该组织成员；解除一条规则不解除其他来源，也不创建、恢复或续期 Grant。

公开入口及字段见 [System Swagger](../../system/backend/docs/swagger.yaml)。尚未识别的数据须先完成 5.5.7 的基础识别，不能把实时树选择本身当作 DataItem 授权依据。

#### 5.5.7 DataItem 授权的统一目标契约

**唯一目标与分工。** Meta 保存 DataItem 的身份、布局、入口、组成边界和识别状态；System 保存稳定数据项引用、授权认可的最小边界绑定、批准要求、Grant／Deny 与审计；Catalog 是可选业务批准入口；实际读取 owner 执行统一规则。System 不跨 schema 查 Meta，不复制完整目录或 whole 叶子清单，也不重新探测格式。

Meta 向 System 精确投影当前数据项身份、可执行组成边界与基础识别状态；唯一事实契约见[授权资源描述契约](addp元数据扫描机制规范.md#授权资源描述契约)。System 绑定被批准的最小边界及其摘要，真实读取使用同一边界表示；状态完整与摘要一致均不能代替当前权限及 Security 校验。当前实施和端到端验收聚焦 PostgreSQL 的 server → schema → 普通 table `single`，先打通事实查询、批准绑定、Manager 真实读取与收权闭环。共享事实协议及 System 持久化约束也能表达 server → database → 普通 table（MySQL）和 server → database → collection（MongoDB），但这不代表对应引擎的实际读取入口已执行源规则，不将其列为已完成的数据授权接入。不同目标仍须分别验证，不能仅凭相同名称跨类型或跨 namespace 复用批准。文件 single、multi／whole 及其他消费入口尚未覆盖，不直接复用完整 Meta 详情、Catalog 变化源或浏览器自报描述。

**Grant 与 Deny 的不同匹配语义。** Grant 绑定被批准的 DataItem 身份及边界摘要；同一数据项的布局、入口或合法组成约束变化后，旧 Grant 不覆盖新边界，须显式重新批准。Deny 绑定同 Tenant、同 Engine 内的稳定 DataItem 身份、接收主体和动作；在期限及主体条件仍满足时，不因边界摘要变化、重新扫描或重新签发 Grant 而失效，直到显式解除或自然到期。建立时记录的边界可以用于审计，但不能成为继续命中该拒绝的必要条件。换 Engine、改名或来源重绑按既有身份规则处理，不把原规则猜测转移到另一个数据项。解除 Deny 不恢复已撤销、到期或边界不匹配的 Grant；未可靠确认当前身份与边界时仍拒绝读取，不以 Deny 未找到作为允许依据。

原生读取计划反查 `single` 数据项时，可以按既有指纹规则定位候选 Meta 记录，但候选指纹本身不是归属证明。必须通过统一 Meta Client 读取持久事实，确认识别完整且类型化边界与实际读取目标完全相等；名称拼接碰撞、事实缺失或边界不符均拒绝，不回退到原生目标 ACL。跨模块事实读取在 IAM 事务之外完成，随后在同一数据库快照中重新核验真实凭据、操作资格及当前规则。

**单体模型的文件组成表示。** 经明确批准的模型仍为 `single` DataItem，身份取自主文件，不把贴图另建授权对象。共享 `AuthorizationBoundary` 在 `layout=single` 下只允许一个读取表示：原生表／集合使用 `native_target`；文件组成使用 `file_composition`，两者互斥。`FileCompositionBoundary` 仅含 `format` 与完整 `refs`；每条 `FileCompositionRef` 含规范 `target: EngineCatalogPath`、`role`、`required`、`primary`，恰有一个必需主文件。主文件与所有引用属于同一 Engine、相同文件／对象路径模型，引用只在主文件父范围内逐一精确核对，不获得父目录读取许可。路径保留名称与大小写，禁止穿越、外部 URI、矛盾重复和目录目标。共享工厂深拷贝并按类型化路径排序、合并完全相同的引用；可信 wire 必须已经规范、完整且唯一，至多 200 个明确文件。空数组不代表组成已确认。文件摘要包括格式、完整路径、角色及必需／主入口语义，排除大小、内容哈希与时间；同一路径内容更新不使摘要改变，组成变化后旧 Grant 不覆盖新边界，Deny 仍约束同一数据项。原生边界的规范序列化与摘要不因新增文件表示而变化。纯值类型和摘要不是 Grant、EA 或允许读取的回执；文件事实投影、System 持久化与裁决、执行授权及真实内容 I/O 接线须分别验收，不以共享类型已编译宣称文件权限链完成。事实完整性来源及单体模型规则见[授权资源描述契约](addp元数据扫描机制规范.md#授权资源描述契约)。

| 布局 | 授权范围 | 正常内容变化 | 不得自动继承的变化 |
| --- | --- | --- | --- |
| `single` | 一个原生表、文件或对象对应一个 DataItem | 增加行、同一入口内容更新 | 借单项许可读取其他对象，或改认更大组合 |
| `multi` | 主 ref 与明确相关 refs 组成一个 DataItem，例如 Shapefile | 原批准组成约束内的成员更新 | 用部分文件许可拼出整体，向范围外追加 refs |
| `whole` | 格式／引擎认定的整体 scope，例如 OSGB Scene、栅格镶嵌 | 既定合法范围内新增 OSGB、TIFF、瓦片 | 换 Engine／根范围、扩大组成规则、引入范围外资源 |

`scope` 是 whole 的组成范围，不是新数据类型。普通目录／schema 是 node，批量选择不会使它变成 whole，未来新增独立 DataItem 不继承兄弟项许可。whole 授权不逐文件签发，也不因合法新增成员要求 System 保存清单或枚举百万文件。

读取端必须约束真实路径、动态成员及关联引用，不能仅凭字符串前缀放行路径穿越、越界链接或外部引用。一个 DataItem 的 Grant 覆盖其合法组成，不再要求组成文件各有 Grant；真正依赖多个独立 DataItem 时必须全部获权，不能静默删去无权部分后宣称完整预览。

**基础识别与新鲜度。** 注册后的立即 basic 复用 Meta 正式扫描编排；注册成功不等于扫描成功。授权要求目标身份与组成可靠，不要求全引擎扫描、deep 或企业编目。完整清单足以确定的组合在 basic 完成；必要的有限 manifest／header 读取也属于基础识别，样本、正文、画像与统计留给 deep。

缺少基础事实、组成不完整或有证据表明边界变化时，须先补扫并明确状态，再签发。扫描限定真实授权范围，不以单叶子请求暗中枚举父目录；普通 GET、树展开和内容读取不暗中扫描。基础识别使用独立、限范围的扫描资格，避免“尚无 DataItem 却要求先有其 Grant”的循环；不能借机器身份读取样本，Worker 重核当前资格。

新鲜度针对授权边界，不是全部元数据。行数、大小、扫描时刻、内容版本或 manifest 字节变化不直接等于范围扩大；whole 内合法增长无需重批或全量重扫。边界未知则先核验，不以相同指纹或缓存描述证明范围未变。具体扫描规则见[元数据扫描机制规范](addp元数据扫描机制规范.md)及[探测器规范](addp数据项探测器规范.md)。

**统一读取链。** 可信会话及操作资格 → Meta 数据项与边界 → Provider／内容链真实读取计划 → 依赖归属与 System 当前规则 → Security 及执行约束 → 同一计划实际读取。核验对象与执行对象必须一致，并通过并发测试证明；原生 SQL、文件 URL、分页、下载及动态瓦片入口同样接入，不保留 native 独立 ACL 或无 Meta 兜底。

Meta 是数据授权的事实依赖，不是 System 登录、IAM、引擎登记的 Ready 前提；无法可靠核验时拒绝对应新增授权／读取，但不阻止已有历史查询或合法收权。缺少识别、边界待刷新、源不可用、无许可及依赖失败分别说明，不统称权限不足。

列／行授权后续在同一 DataItem 上表达字段和记录约束，包括属性及空间条件；须约束真实查询和内容出口，不能只过滤前端。条件模型、组合语义、边界版本与各引擎支持另行明确，不预建无人消费的实体或 DSL。

##### Meta 采集资格与范围（已确认）

采集操作资格、源内容读取资格与采集结果读取资格分别裁决。System 统一管理采集资格及当前授权，Meta 解析实际范围并约束 Provider／reader；不得在 Meta 保存另一套源 ACL，也不得借用 Manager 预览、剖析或 Service 查询定义的专用授权契约。执行来源、父 execution、Worker lease 和取得引擎连接均不构成数据读取许可。

| 采集场景 | 读取依据 |
| --- | --- |
| User 交互扫描 | basic 核验当前 User 对目标范围的基础识别资格；涉及内容的 deep 另核验其当前数据读取权及适用 Security 规则 |
| 任务成果自动扫描 | 可信生产者证明实际目标，精确 basic 建立身份；System 独立裁决成果读取授权，获权后才执行需要内容读取的 deep，不复制父任务的授权或 User Token |
| 租户集中采集 | 具备相应引擎或数据范围管理资格的主体明确批准采集范围、用途和允许深度；后台按当前有效的治理采集资格执行，不永久借用创建人的个人读取权 |

集中采集的历史操作人仅用于来源与审计，不因此成为内容接收人；其个人源 Grant 不能代替集中采集资格。委托的批准、期限、撤销及适用拒绝仍由 System 独立裁决，机器身份不豁免当前规则。普通扫描功能 Permission 不足以自批后台读取。采集可以持续运行不等于委托永久有效或自动续期。

个人交互采集完整适用当前 User、部门及项目组的源数据 Deny。租户集中治理采集不绑定创建人的个人 Grant 或 Deny，而由独立采集资格的范围、用途、允许深度、期限与撤销状态，以及适用的 Security 约束决定是否继续执行；创建人被拒绝读取或离职不自动撤销该资格。本期不新增针对治理任务或 Meta 服务的专用 Deny，也不把现有人类接收方的 Deny 扩大为后台禁扫。需要停止治理采集时撤销其资格；操作人读取源内容或采集结果仍独立核验本人当前授权。

采集范围仅使用以下两种语义，不另加“是否包含未来项”的平行开关：

| 范围 | 批准与执行语义 |
| --- | --- |
| 精确目标采集 | 仅覆盖明确指定的数据项及其经核验合法组成；不覆盖同目录／schema 兄弟项或未来独立新增项 |
| 完整范围采集 | 明确批准一个 Engine、schema 或目录等完整范围，包含边界内未来新增项；批准展示必须说明这一效果，并分别限定 basic／deep 及其用途 |

完整范围采集资格不是普通内容 Grant，不将 node 改成 whole，也不使用户对未来 DataItem 自动获权。移动到范围外、跨 Engine、换根或引用范围外资源均重新裁决；不能按字符串前缀、RelatedRefs 或 manifest 中出现了某路径就放行。已批准范围内新增成员仍受当前有效性、适用拒绝和 Security 约束。

basic 只为可靠身份与边界做必要的低成本识别。有限 manifest／header 读取须由格式能力声明用途、上限、引用边界及失败语义；不能读取任意前若干行充当基础识别，也不能全量读取仅为计算内容哈希。deep 的结构解析、内容采样、统计、哈希或正文提取按实际读取行为裁决，不因只保存摘要而免除授权。请求 deep 而只有 basic 资格时不得把 basic 成功冒充 deep 成功；已形成的技术事实和内容增强失败分别反馈。

Worker 在实际读取前重核当前资格，并将核验范围与同一实际执行计划绑定；重试、接管及后续读取不能沿用过期 Allow。配置、深度、目标或组成变化须重新核验或拒绝；`source`、`trigger_type`、客户端自报主体及历史授权版本不选择可信授权分支。集中采集所得样本、正文及含业务值的派生结果不得通过普通目录、执行详情或日志泄漏，结果读取另按当前权限控制。

上述为已确认的行为契约，现有 Meta `ScanScope` 仅表示执行范围，不是 System 已签发的资格。采集资格的 System 签发／消费与持久化、Meta Worker 全链路约束尚未完成，不从已有来源隔离、PG 内容授权或基础组合识别验收外推完成。实现沿统一规则接入，未支持的目标或读取能力明确拒绝，不增设服务直连兜底。

实施先完成 Meta 最小资源描述与基础边界，再统一 System、Catalog 目标绑定及读取消费者。存量原生授权只在可证明范围等价时处理，不能把单文件权限扩大成整目录；具体方案在动工前确认，完成后删除旧路径，不保留双读／双写。

#### 5.5.8 任务产出数据与初始使用权

本节为已确认的设计原则。当前 PG 的 System 初始签发与终态核清、Transfer 后台续办、成功产出限定基础补扫及独立状态反馈已实现，并通过对应 T1／PostgreSQL T2；新成果真实端到端 T4 尚待验收，不能将局部签发验证视为自动授权闭环已完成。任务实际新建业务成果后，默认向可信发起账号签发成果的初始只读 Grant，无需逐任务配置或预先审批产出规则。读取仍须等待实际签发，并按统一源数据授权链校验；任务发起、写入成功、血缘登记或 Meta 扫描成功本身不代替 Grant。

| 结果形态 | 授权处理原则 |
| --- | --- |
| 执行内部结果、临时导出 | 按实际 owner 的结果读取规则控制，不伪造 Meta DataItem，不把执行归属或临时下载许可转成业务源 Grant |
| 新建独立业务 DataItem | 实际新建且经 Meta 确认身份与边界后，默认向可信发起账号签发初始只读 Grant；不要求发起账号具有授权管理权限，尚未签发不得视为可读 |
| 覆盖／追加已有 DataItem | 继续适用该数据项原有规则，不因本次写入新增发起人特权；授权边界改变仍按 5.5.7 处理 |
| whole 合法范围内增加成员 | 不当作多个独立新项反复授权；既有 Grant 只覆盖原先批准的合法组成范围 |

初始使用权复用普通源数据 Grant，只授予发起账号对实际新成果的读取权，不授予管理、分享、再授权、写入或 DDL 权限，也不替代消费模块功能权限、Deny 与 Security。默认授予只适用于本次实际新建的成果；覆盖、追加或同名重建已有数据项不能使用本条增权，也不能改写已有目标的批准要求。后续向其他账号、部门或项目组授权，仍按目标的正常批准要求办理。

初始 Grant 使用独立的“任务产出初始授权”来源，和直接批准、Catalog 业务批准区分。它仍写入同一源数据 Grant 存储，统一查询、实际读取与撤销，不建立第二套作者 ACL；不伪造人工批准、Catalog 业务确认或批准要求版本，也不新增人工审批步骤。任务发起账号是接收方与执行来源，不因此成为授权管理员；机器签发者与历史人类发起来源必须分别留痕。

初始期限在首次正式签发时根据本次实际读取的完整必要来源确定：每个来源合并发起账号当时有效的个人、部门和项目组 Grant，取可连续读取的最晚截止时间，并受适用成员资格期限约束；多个必要来源取上述期限的最早值。全部来源均长期有效时，成果使用 `until_revoked`，否则使用最早绝对截止时间及 `at_time`。无许可、有效 Deny、边界未知或期限无法可靠核验时，不自动签发，不把未知当作长期；期限已过不顺延。期限由 System 统一裁决，不能由模块自报，重试不重新起算。没有来源的数据生成及没有可信人类发起账号的机器任务另行明确，不能假定为长期个人授权。

发起账号取自任务 owner 已验证并持久化的执行来源事实，不能信任请求自报账号，也不能把实际运行的 Service Principal 当成人类发起账号。定时执行沿用已确定的执行来源；执行人、任务维护人和成果接收人不能互相猜测或替换。不存在可信发起账号的机器任务不推导个人接收方，相关处理须另行明确。

任务 owner 证明真实执行来源及实际新建成果，Meta 通过基础识别证明 DataItem 身份、完整性与授权边界，System 按本条统一原则核验并通过唯一源规则写路径签发、去重及审计。所有产出模块经共享 client 接入同一契约，不在 System 预先逐项登记未来成果，不在各模块保存第二套“作者可读”ACL，也不用用户 Token 驱动后台授予。覆盖、同名重建、重扫或重试不能冒充首次产出，不能恢复已撤销、到期或边界失配的 Grant。

完整必要来源只接受 Provider 同一实际读取会话的 `ReadSessionEvidence`，不从任务配置、目标表、血缘摘要或执行后目录补造。PG Transfer 在同 attempt、同目标的成功提交事实中冻结规范 ReadSet（无法证明时为 null）；不保存或推导许可期限，证明失败也不触发重新读取、重跑计算或修改原写入策略。共享机器回执只投影这一不可变事实；旧版本私有事实不补填来源，空集合或 null 不能当作“所有来源长期有效”。

**实际新建的证明要求**：新建是本次业务产出的执行事实，不是扫描发现时间、Meta 新增行或一次 `CREATE TABLE` 的同义词。授权接入不得把 Transfer 已有的 replace 删表重建改成保表清空；写入策略与初始授权资格分别处理。

| 事实 | 权威及要求 |
| --- | --- |
| 执行来源 | 任务 owner 已验证、持久化的 Tenant、执行编号、有效 attempt／lease 及完整人类发起主体；不接受前端自报或仅用 `triggered_by` 推导 |
| 写入前目标与实际操作 | 引擎 Provider 的真实执行证据，须区分首次新建、覆盖／复用已有目标和无法证明；删除前的已有目标事实不能被后续建表、扫描或重试覆盖 |
| 成果提交 | 同一次实际写入的成功提交事实；行数、进度、目标 locator、会话打开成功或生成提交标记的接口存在，本身均不足以证明首次新建 |
| 数据项身份与边界 | Meta 基础识别结果与本次实际提交目标一致，且完整性和授权边界可核验；不是以扫描结果代替写入事实 |
| 存量与签发状态 | System 核查同一逻辑目标的已有批准要求、Grant／撤销历史、Deny 及原办理结果；不以物理表重建、新 Meta 行或新请求编号恢复旧许可 |

写入前“目标不存在”的独立查询只能作为观察，不能单独证明后续创建归属于本执行。Provider 必须证明观察与实际创建之间没有被并发目标替换、复用或删除重建冒充；本进程锁或仅约束 ADDP 调用者的协作锁不能当作外部 DDL 的排他保证。尚不能给出充分证据时，只是不自动签发，不据此改变原写入策略、重跑任务或把未知默认成新建。

PG 已确认的并发边界：观察为缺表时，跳过删除并用不带 `IF NOT EXISTS` 的原子建表语句创建；并发抢先出现同名目标即报冲突，不删除或复用该目标。观察为已有目标时，replace 继续删表重建并始终分类为覆盖，不因新的物理表产生初始授权资格。准备结果只证明该阶段的实际操作，不代替数据提交、历史核查或最终授权。

PG 首期的运行前提是任务期间不对目标执行 schema 改名、迁移等结构运维；不为此增加父 schema 锁，也不承诺抵御任意外部并发 DDL。Provider 在实际写入事务开始及提交前核对精确目标路径与本次创建事实；已观察到的路径或归属变化不得产生成功新建证据。最终核对与提交之间不提供额外的跨目录原子保证，因此该检查不能替代上述运维前提。该范围不放宽严格 CREATE 撞名、已有目标不增权、失败／断连／未知提交不自动授权的要求。

产出证据按原执行及实际目标绑定，由 owner 在有效执行租约约束下持久化；多目标逐项记录，不能用一个成功目标证明其他目标。恢复或重试必须保留原产出关联与已发生目标操作，不得从当前目标缺失、空表或新执行编号重新推导首次新建。提交结果未知先核清原写入，不能重发写入来补造证据；缺失证据不得事后根据当前 Meta 或任务配置补填。证据不是 Grant，也不新增物理实例作为授权身份。

PG 普通 Transfer 的持久化沿现有执行记录保存私有产出事实：在目标操作前写入 `pending`，记录真实执行、Tenant、attempt、重试来源和精确目标；Provider 准备结果落库后为 `prepared`，完整写入成功返回后才记为 `committed`。未达到后者均不证明成果已提交，进程崩溃、提交后反馈丢失或执行失败不自动解释为已回滚。已存在占位不能重新初始化或重放原目标操作；新重试保留 `retry_of_execution_id`，不能清洗为首次执行。重复准备／提交回执只核对并恢复同一事实，不延展时间、不更换目标或覆盖已冻结的准备结果。

上述写入均核验当前租约并串行更新同一执行；锁等待结束后仍须核验期限，迟到旧 attempt 不能补写。普通执行 metadata／终态更新不得覆盖私有产出事实；用户执行详情和 TaskProvider 稳定输出不暴露这些内部证据。不新增执行表、目标物理身份或兼容回填路径；本阶段仍不签发 Grant。

生产者证据使用独立的最小机器契约，不读取 owner 私有配置、metadata 或数据库。PG Transfer 的 `GET /api/v1/transfer/runtime/executions/{execution_id}/production-receipt` 仅允许当前 Tenant 的 `addp-system`、`addp-meta` Service 与不可委托、不可租户定制的 `transfer.production_receipt.read`，该权限仅授予 `tenant.system_runtime`、`tenant.meta_runtime`。共享 `TransferClient` 按精确执行 UUID 获取已成功终结且同 attempt 已提交的回执，返回实际目标、准备结果、提交时间、重试关联及可信人类来源（存在时）；缺失来源不以 `triggered_by` 或机器身份补造，未提交、失败及不一致的证据不输出成功回执。回执不是首次新建资格或 Grant，不包含凭据、连接、业务内容或来源权限期限，不改变用户执行 DTO／TaskProvider 输出。Meta 授权事实仍仅由 System／Catalog 的原入口读取，Transfer 不因此获得 Meta 授权事实查询权限。System 后续独立核对当前 Meta 边界及全部签发条件；期限只在首次正式签发时冻结。

System 的内部产出边界准备只接受可信 Tenant 与精确执行 UUID，以共享 client 取得上述回执，再复用现有 Meta 边界解析器核验完整 `single` 目标。候选指纹不是归属证明，原生名称不得拆分或仅按 `full_name` 匹配；返回的类型化路径须与实际提交目标完全相同。跨 owner 调用不持有 System 数据库事务，前后核对本租户 PG 引擎的生命周期及聚合版本；不完整、待刷新或依赖不可用时拒绝准备，不在读取中补扫。准备结果仅为内部当次证据，不是 Runtime 凭证或 Grant，不缓存为许可，不冻结期限，不推导缺失的人类来源，也不因 `created` 或准备成功省略正式签发时的历史、身份、ReadSet、Deny 及边界复核。

初始授权的内部来源准备进一步要求非重试的首次执行、实际 `created`、可信人类来源和非空完整 ReadSet；逐项核验来源的 PG 引擎版本及 Meta 边界。当前来源规则、执行所需 IAM 功能权限与目标既有批准／办理／Grant／Deny 历史在同一只读快照中观察，不因撤销、解除、到期或更换边界而忽略目标历史；未知来源不进入规则查询。该观察不冻结期限、不签发 Grant，也不能替代正式写事务在统一锁顺序下的复核。首次执行与历史为空仍不能独立证明该执行真实新建并成功提交了成果。

**资格基准与物理对象边界。** 初始授权以“任务真实新建并成功提交该逻辑成果”的不可变执行事实为资格依据，不以签发瞬间仍保留原物理表为条件。Provider 须证明从实际创建到本次写入提交的成果归属；准备与写入会话分离时，中间的替换窗口仍须校验，不能仅补执行后的目录查询。Transfer 沿原执行持久化同一事实，创建、写入或提交未知时不得补造成功证据。原生对象标识仅用于执行内部补证，不写入 Grant 成为物理 ACL，不增加业务列或改写业务注释。

成功提交后不再持有成果表锁等待 Meta 识别或 System 签发，不引入跨库两阶段提交、补偿撤权、业务数据库台账或 DDL 触发器。原物理表随后被同路径重建时，只要当前仍是同一逻辑 DataItem、边界可核验且未变，既有成功产出事实仍可支持初始签发；这与 5.5.4 的逻辑资源授权语义一致，不表示对原物理内容永久保真。改名、换 Engine、来源重绑或边界变化不转移资格；源事实缺失、目标身份或边界未知时拒绝签发。

产出事实不是许可或已冻结的期限。首次正式签发仍须复核当前 DataItem 身份及边界、引擎绑定、来源授权及成员期限、执行功能权限、Deny 和目标全部批准／办理／Grant／撤销历史；已有目标不能借新执行或物理重建冒充首次产出。重试只恢复同一办理结果，不能重新生成资格、延长期限或恢复已撤销权限。相关创建／提交断连反例、不可变回执及唯一签发事务须分别验证，不以普通表锁测试或准备成功替代自动授权闭环验收。

PG Transfer 的私有 `transfer.target-production/v3` 与共享 `transfer.production-receipt/v3` 显式携带 `created_and_committed`。该字段只由同一实际写会话的 Provider 证明产生，在有效执行租约下与首次提交时间及实际 ReadSet 一并冻结；仅 `committed + created` 可以为 true。false 表示未证明，不从准备状态、行数或普通提交标记补齐。重复记录必须匹配全部冻结事实，不允许事后将 false 提升为 true。旧版事实不转换、不补造；System 的初始来源准备必须要求 true，回执本身仍不是 Grant。

物理产出、基础识别和授权签发不是跨模块原子事务。状态应分别反馈；产出或扫描成功不得显示成授权成功，授权失败不得自动重跑计算或删除已产出内容，结果未知先核清原办理，不换编号重复授予。只为授权确定边界时基础识别足够，不因本节给现有业务扫描增加 deep 前置要求。

**自动续办与终态（已确认）。** 物理任务成功后，Meta 基础识别尚未就绪、依赖不可用、网络断连或结果未知保留“任务成功、授权待处理”；后台以同一 Tenant、产出 owner、原执行 UUID 续办，并先核清原结果。暂时故障只重试授权续办，不重跑计算、不重发写入、不删除成果，也不更换原身份或命令。权限不足、有效 Deny、目标已有批准／授权历史、非首次新建、证据矛盾或来源期限已过等明确拒绝属于终态；后台停止，不在权限或组织关系后来改变时自行恢复这一轮初始授权。

终态必须持久化并且不可改写：同一原执行只能恢复原签发或原拒绝，进程重启、回调丢失及并发续办不能把拒绝变为批准，也不能重新计算已签发的期限或恢复撤权。成功结果引用唯一 Grant，仍按该 Grant 的统一读取与撤销链管理；拒绝结果不是 Deny，不阻止管理员以后经正常显式流程批准。未获得可信结果的 HTTP 错误、缺失字段、畸形回执及未分类错误不得伪装成已签发或明确业务拒绝。系统不得因为暂时故障构造新的批准来源、第二套 ACL 或用户 Token 后台通道。

Transfer 仅在本次成功完成事务中，为具备 v3 首次新建提交证明和完整人类来源的 bounded 执行初始化 `initial_authorization` 待办；不回填没有该标记的存量成功执行。该 metadata 只保存投递状态、有限退避进度、封闭原因码及原 Grant 编号引用，不保存权限规则或期限，不作为实际读取依据。现有 Worker 在自身注册有效时独立领取待办，释放数据库锁后通过统一 client 先核清原终态、再续办签发；只在收到正式 pending 响应时请求 Meta 精确补扫。暂时故障保留物理执行 success 与 pending，按 30 秒至 5 分钟有界退避；明确终态停止领取，迟到旧领取不能覆盖新结果。不得因授权待办而重新执行任务、修改目标、删除产出或回写私有产出证明。

System 的最小初始授权终态以 `Tenant + producer_owner + 原 execution UUID` 唯一定位，只有 `issued / denied`，不保存任务配置或第二份 ACL。`issued` 与唯一源 Grant、审计在同一事务提交，`denied` 只保存封闭原因码和裁决时间；暂时故障不生成终态。`task_production` 来源的 Grant 使用 `requirement_version=0` 表示不消费人工批准要求，不伪造版本 1；其原执行、机器签发者及历史人类发起者分别绑定。当前仅固定 `addp-transfer` 的 Tenant Service 和专用 `system.engine_access_initial_grant.execute` 可以续办自己已成功的 PG 产出；实际目标、账号、来源和期限均由 System 反查与裁决，不允许请求自报。该机器 Permission 不授给人类或租户自定义角色，也不是通用 Grant 创建权限。

**成功产出的限定补扫（已确认）。** 首次扫描提交失败而原任务已经成功时，Meta 可用自身 Tenant Service 和统一 Transfer client 反查原执行的成功产出回执，仅为回执中的精确 PG 首次新建成果补做基础识别。请求不得自报目标、主体或创建成功事实，不得扩大到 schema／目录、兄弟表或任意历史任务。扫描归属沿可信回执保留真实人类来源和直接父执行，不复制用户 Token 或执行授权；跨 owner 取证在 Meta 数据库事务外。原手动产物扫描入口仍严格要求父任务处于运行中，不因这个补扫能力放开已结束任务。补扫只建立 Meta 技术事实，不代表授权签发成功。

限定补扫使用 `POST /api/v1/meta/runtime/transfer-productions/{execution_id}/basic-scan`，固定 `addp-transfer` 与现有 `meta.scan_task.execute`，只传原执行 UUID。创建复用 Meta 同一范围解析、执行入队、Worker 和读取隔离；并发／响应丢失恢复同租户原扫描，不重复创建、不复活已经失败的扫描执行。已入队扫描失败时保留其原状态，按 Meta 既有显式恢复约束处理；不能把扫描失败伪装为 System 的终态授权拒绝。该能力是运行时软依赖，不新增 Meta→Transfer 的启动／Ready 前置条件。

物化成果的 Grant 独立管理：来源授权及成员关系只参与初始期限确定，成果签发后不因来源 Grant 撤销、续期或来源成员关系变化自动撤销、续期或缩短。成果仍受自身当前 Grant、Deny、功能权限及已有安全限制约束；本条不解除来源已经要求执行的安全限制，也不把血缘变成许可或第二套级联 ACL。实际读取来源的视图或实时查询继续逐项核验完整 ReadSet，不能冒充独立物化成果。

实现须沿统一产出证据契约按切片验证：真实新建与覆盖的区分、可信发起账号与服务身份不混淆、同源授权取最长及多源取最短、有限与长期混合及成员期限、无许可或拒绝不生成期限、接收方及期限不可扩大、基础识别失败拒绝签发、重复／迟到回执与撤销不复活、来源撤权不级联且不改已冻结期限、已有目标批准要求和成果 Deny 不绕过、签发失败不重跑计算，以及真实 PG 成果读取和收权。T1／T2 验证契约与持久化，T4 验证产出→识别→签发→读取→撤销；未运行不计通过。当前不扩展其他引擎。

## 六、共享中间件

Go `common/middleware/auth` 与 Python `common-python/addp_common/auth.py` 统一调用 System 并提供不可变上下文及权限 helper，不在业务模块复制身份解析。每次解析回查当前凭据、Family、Principal、Context、Membership、授权版本和有效 Assignment；当前不跨请求缓存 Token／Ticket AuthContext。后续缓存须先证明版本与失效协议，不恢复旧缓存旁路。

## 七、OAuth 与 Refresh Token 要求

CLI 有浏览器时使用 Authorization Code＋PKCE，否则使用 Device Authorization Flow；公共客户端无内置 Secret。Refresh Token 只保存 Hash、每次轮换，重用撤销 Family；CLI 将其保存在 OS Keychain，Access Token 短期。协议细节归 [OAuth 授权规范](<addp OAuth授权规范.md>)。

内部模块通过 Fosite Client Credentials 获取 Service Token：独立 Confidential Client 一对一绑定 Service Principal，Secret 保存 BCrypt Hash。Tenant 请求只能从有效 Membership 选择 Context；Platform 请求显式声明 `context_type=platform`，使用专用 Platform Service Role，不能借用平台三员。二者互斥，Token 最长 5 分钟、无 Refresh Token，认证方法为 `service_secret`、AAL 为 `not_applicable`。

Service Token 仅进入显式机器契约，不因持有同名 Permission 就可访问平台 User 管理 API；需要平台事实时使用最小投影并校验服务、Context 与精确 Permission。API Consumer Credential 属于数据面，不生成 AuthContext，不能访问控制面或复用为 OAuth Client。请求只携带规定的凭据，不混用 Bearer、`X-Internal-API-Key`、`X-Tenant-ID` 等平行认证。

## 八、禁止事项

- 模块自行解析 JWT、使用 `/users/me` 验证 Token，或恢复 `user_type` 与 Role Assignment 双轨。
- 信任客户端自报主体／Tenant，利用空 Tenant、平台角色、Scope、API Key 或连接凭据绕过资源授权。
- CLI／Agent／owner 保存 `JWT_SECRET`，外部 Agent 持模块 Client Secret，Worker 持久化或代传 User Token。
- Gateway 代替 owner 最终裁决；以一次入口检查、缓存 Allow、前端隐藏或宽权限源连接替代真实执行控制。
- 用 Service Token、Resource Ticket、Delegated Token、Execution Authorization 或 Notebook Session Authorization 互相冒用。
- 把业务确认、受理、签发历史、规则覆盖或管理委托显示成最终数据可读；不允许原生与 DataItem 授权双轨。

## 九、实施范围与验证

| 范围 | 规范与实施边界 |
| --- | --- |
| AuthContext | 唯一 v1 Schema；破坏性变化同步 Schema、Go／Python 类型、System 投影及所有消费者，不按客户端保留旧版本 |
| 源数据授权 | 当前实施与端到端验收限于 PG schema 普通表 single，逐项核验直接／业务批准、Manager 读取及收权；共享协议与持久化约束对其他单项目标的表达能力不等于实际读取接入。MySQL、MongoDB、文件布局及其他消费入口仍待独立闭环，不视为已受 DataItem 授权控制 |
| Meta 基础识别 | 清单组合识别不代表所有格式已支持必要结构读取；身份、完整性与边界契约须逐格式验证 |
| 产出数据初始使用权 | 5.5.8 已确定实际新建成果默认向可信发起账号签发初始只读 Grant，使用独立产出来源而不伪造人工批准；资格基准为真实新建并成功提交的逻辑成果，不要求原物理表存活到签发，不引入跨库两阶段提交。期限在首次正式签发时按同源取最长、多源取最短确定，物化成果不做来源撤权级联。PG Provider 创建／写入提交补证及路径复核、Transfer v3 ReadSet／成功产出回执、System 唯一 Grant／不可变终态、Transfer 后台续办与状态反馈、Meta 精确成功产出基础补扫已接通。Common／System／Meta／Transfer 对应 T1 与 PostgreSQL T2、Transfer 前端门禁已通过；新成果 PG T4 尚待验收。任务成功、扫描成功或局部测试通过均不代替正式 Grant |
| 列／行授权 | 保留同一 DataItem 上的扩展方向，具体规则和执行接入尚未确定 |

每个实现切片至少验证：可信身份及 Tenant 隔离、默认拒绝／Deny 优先、撤销与到期、原命令幂等与并发、未知结果恢复、实际读取与批准范围一致。DataItem 接入另须覆盖 basic／deep 身份一致、组合不完整拒绝、whole 合法增长可读而越界拒绝、node 新项不继承、Meta 不可用不回退及无 Catalog 的直接批准。

验证遵循[测试与验收规范](addp测试与验收规范.md)：工作区使用 `make test-changed`，明确 owner 使用 `make test-module MODULE=<模块>`；共享契约专项使用 `make test-authorization`。文档修改按标准影响选择运行对应门禁；确定性测试不代替真实跨模块验收，跳过、失败及未运行分别说明。

实现入口：[System IAM 数据模型](../../system/docs/IAM数据模型与迁移规范.md)、[System Swagger](../../system/backend/docs/swagger.yaml)、[企业资源目录实现规范](addp企业资源目录实现规范.md)、[Manager 模块说明](../../manager/CLAUDE.md)。迁移编号、接口参数、调度批量和当前验收记录留在各自 owner，不再追加到本规范。

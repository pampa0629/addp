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

Manager 文件上传后的精确识别与初始授权按下述“上传新数据初始使用权”执行。上传的 Tenant User、实际写入范围及真实新建／覆盖结果由 Manager 核验并持久化；Meta 只允许对该次上传已成功提交的精确组成进行识别，不要求尚未形成的 DataItem 预先具有 Grant，也不由 `source=manager` 或浏览器自报路径推导资格。不扩大到父目录，不给上传者增加全引擎扫描功能或管理资格。上传、识别、授权分别表达结果；已写入文件不因后续失败回滚或重复上传，依赖暂时不可用只续办识别／授权，明确拒绝停止。普通手动刷新仍须对应扫描功能和当前有效 DataItem／节点范围授权。旧的上传 User Bearer 手动扫描放行路径随实现替换删除，不保留旁路。

上传来源的机器权限按已确认范围独立接入：Manager Runtime 使用既有 `system.engine_access_initial_grant.execute`；新增 `manager.upload_receipt.read` 仅授给 Meta、System Runtime。上传 owner 以统一 ad-hoc execution 保存同次成功写入回执与原人员来源，不创建持久任务定义；回执不能作为读取凭据。Meta 的限定 POST 仅固定 Manager 触发，System 仅通过已有 `meta.task_provider.read` 读取首次识别事实，不能据此触发扫描。请求不能自报接收方、边界或新建结论。

System 以 `upload_creation` 来源写入唯一 Grant 存储，绑定上传 UUID、精确 DataItem 与边界、原上传者和机器签发者；每个上传／数据项的签发或明确拒绝终态不可变，与 Grant 及审计原子提交。内容边界已接入的可靠新项签发 `read`，其余可靠识别新项只签发 `metadata_read`；均持续有效且可撤销。组成包含旧文件、属于既有 whole 容器、旧逻辑项、覆盖或任何授权历史时不推导首次新建资格。暂时故障只续办识别和授权，不重写文件、不删除成果；本人状态查询只读且不要求全局 Meta 浏览权限，历史已签发不表示当前仍可读。

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

#### 本人当前角色自查

`GET /api/v1/system/auth/subject-roles` 仅允许当前第一方登录 User 在当前 Tenant Context 中自查有效 Role Key 集合，包含没有功能 Permission 的业务角色。既有 AuthContext 仍是功能权限投影，不改变 v1 Schema，也不拿它代替完整业务角色事实。该接口不接收主体、Tenant、角色、筛选或请求正文；Platform、Service Principal、委托、资源票据及应用凭据不得使用，不新增查询其他人的能力或数据访问权限。

响应固定为 `{tenant_id, actor_principal_id, tenant_membership_id, authorization_version, role_keys, observed_at, expires_at}`，ID 为十进制字符串，角色键稳定排序去重，可为空数组。System 复用执行发起人角色核验的同一有效 Role Assignment 查询，在身份锁与数据库当前时间下复核当前令牌、Family、Principal、Membership、授权版本及期限；截止时间取当前令牌、Family、Membership 和返回角色期限的最早值。HTTP `Cache-Control: no-store`；Owner 每次保存、列举选项或同步结果交付均使用当前请求 User Bearer 核验并精确绑定已有 AuthContext，不缓存、不持久化角色事实，不回退 Service 凭据。异步执行仍使用下节的 execution/attempt/lease 专属接口，不能用本人接口代替运行资格裁决。

#### 执行发起人的当前角色核验

`POST /api/v1/system/execution-authorizations/{id}/subject-roles` 仅供当前 Tenant 的 `addp-develop`、`addp-transfer` 在本模块真实 running 执行中核验发起人的当前角色；沿用 `system.execution_authorization.execute`，不新增用户角色枚举权限。请求只有 execution UUID、正整数 attempt、lease token，不接收主体、Tenant、角色或算法。Develop 的 DuckDB audience 仍由 Develop Owner 核验，Runtime 不取得角色。

System 核对已封存且未撤销、未过期的 user 来源授权，排除 Notebook 和内部任务；从统一执行表核对所属模块、同一授权 ID、Tenant、发起人、Membership、授权版本及当前 attempt／lease。若有 Orchestrator 父执行，还核对同 Tenant、同主体来源和正在运行的父链。旧 attempt 的动态授权不能用于新 attempt。锁定当前身份与执行后读取当前 Tenant 下有效 User Role Assignment（包括没有功能 Permission 的角色）；角色集合取并集，不按优先级排序决策。身份、成员或授权版本失效直接拒绝。

响应仅含绑定的授权／执行／attempt／Owner／主体引用、排序去重的 `role_keys`、`observed_at` 和 `expires_at`；期限不超过授权、执行租约及本次所含角色分配的最早到期点。审计写入后再以数据库当前时间核对期限，响应不得缓存、续期或持久化为角色凭据；Common client 拒绝绑定不一致、畸形响应、重定向和隐式重试。空角色数组是有效的空集合，不推导管理员资格。该事实只用于当次脱敏选项核验，不授予源读取权、原值权或新的执行效果；实际数据访问仍走原有授权链。此接口落地不等同于四个出口已接入角色选项。

### 5.2.1 内部任务执行授权

`internal_task` 与 Engine Access Scope 互斥，空 accesses 不是许可。当前仅允许 Tenant User 为 Ontology `semantic_projection` 签发，绑定 resource、revision、digest、generation，并核验发布 Permission 和真实 pending execution。仅 `addp-ontology` 可按同一 running attempt／lease 消费；不接受 Notebook、服务定义或 Orchestrator 派生，不返回 Infra 凭据，也不代表投影 ready。Ontology 仍负责业务资源与激活校验。

### 5.2.2 Manager 剖析执行授权

剖析不属于内部任务。其授权除逐引擎 read 外，须绑定 `source_read_scope`：完整持久配置摘要与 Provider 的规范完整 ReadSet。摘要唯一由 `common/execution.NewManagerProfileReadScope` 计算，输入不超过 512 KiB；只消除 JSON 键序／空白差异，不经浮点数改写。当前范围为普通第一方／OAuth API User 的单引擎手工剖析；System 从真实 pending execution 取证，核验 `manager.data_profile.execute`、`manager.data_item.read` 及全部源规则，不接收自报来源或用当前 Meta 补造历史依赖。

消费仅限 `addp-manager`，绑定同一 execution、attempt、lease、配置与 ReadSet，重核当前 IAM、Grant 和期限。禁止经仅提交 Engine ID 的通用路径消费，也不从 Notebook／服务定义／父 execution 自动派生。Manager 执行同一计划并施加 Security；剖析结果读取须使用生成时冻结的真实来源，不能以今日依赖替换。

固定签发与消费入口分别为 `auth/execution-authorizations/manager-profiles` 和 `execution-authorizations/{id}/manager-profile-accesses`。签发只接受 execution 与有限 TTL，消费只返回不可缓存的当次观察时刻，不返回连接或通用许可；共享 DTO／校验归 `common/execution`，客户端不自动重试、不跟随重定向。精确字段见 [System Swagger](../../system/backend/docs/swagger.yaml)，实现与验证范围见 [IAM 数据模型规范](../../system/docs/IAM数据模型与迁移规范.md)。

### 5.2.3 精确组成模型快显执行授权

已验收接入范围为真实 FTP／NFS 引擎的 IFC／3DS／DAE 及 MinIO 的 IFC／3DS／DAE 单体 GLB 生成，共用精确组成、执行授权与受控暂存链；固定样例与真实运行边界见[验收证据索引](testing/验收证据索引.md)。NFS 仅通过 Provider 读取已核验的普通文件，上述三个格式不再通过宿主挂载路径直传；旧 NFS 产物无正式来源范围时拒绝读取并要求重新生成，不补造组成或授权。DAE 基础扫描完整 XML 上限为 64 MiB，仅认领明确 PNG/JPEG，组成变化须重新批准。OBJ 扩展及本轮 STL 迁移沿用下述同一契约，实施范围不替代各自的真实验收记录；其余格式不在当前迁移范围。普通 User 的当前身份、`manager.derived_artifact.create`、`manager.data_item.read` 和完整 DataItem 组成规则均须成立；不接收客户端自报的 Actor、引擎类型或 Allow。历史人类 ActorOrigin 先由 Manager 保存，不因绑定 EA 覆盖为机器身份；本期不从 Orchestrator 父执行、Service 定义或任务创建人补造来源。

OBJ 扩展沿用上述三来源、普通 User 与同一执行授权契约，不新增服务、Permission 或目录授权。Meta 在 basic 阶段完整读取主 OBJ 和其明确声明的同目录 MTL，匹配主目录内的精确 PNG/JPEG；主文件、MTL 和贴图均作为同一 DataItem 的必需组成，不给材质或图片单独签发 Grant。`format_info.obj.composition_complete/material_libraries` 表达实际完整组成，与 deep 几何摘要的 `scan_complete` 分离；缺失／空图片、嵌套 MTL、链接或超限时原子标记不完整，不认领部分依赖。原生路径规则与预算见[attributes 规范](addp元数据attributes规范.md)。

自包含 GLB 的 FTP／NFS／MinIO 原始读取沿用上述当前 User、Engine 版本和精确组成裁决：basic 仅在严格 GLB 2.0 组成观察成功后提交唯一必需主 `model` ref 与 `format_info.glb.composition_complete=true`。批准范围只包含该主项，不包含目录。GLB 不进入模型转换任务或执行授权；直接预览与原文件下载共同读取同一份最多 64 MiB、通过格式层自包含复核的字节，JSON 上限 16 MiB。外部 URI、data URI、扩展、超限及不完整内容拒绝；读取后交付前再核现行源权限，Range 仅切分已验证字节，不能绕过完整验证。格式组成不证明场景可渲染。


Manager 对 OBJ 与全部 MTL 分别从同一已授权流中缓冲、重核声明并暂存同一份字节；闭包变化时不打开新发现图片，不上传或调用转换。新边界须重新批准，旧成果仍按自身来源范围及当前规则读取，不能借新 Grant 放行旧边界。NFS／MinIO 的 OBJ 不再直传宿主路径或整个对象前缀；旧产物缺少正式来源范围时拒绝并要求显式重新生成，不补造授权。双 MTL 与子目录同名贴图的格式探针、模块门禁与实际三来源 Hosted 验收分别留证，不能互相替代。

MinIO DAE 已按同一精确组成契约通过固定静态样例的本机验收及独立 Hosted 三来源验收，范围见[验收证据索引](testing/验收证据索引.md)；不外推任意模型，授权页面后续共享前端修复的 T3／本机证据与固定 Hosted 源码分开记录。MinIO 保留 `service → bucket → prefix → object` 的真实目录类型，组成绑定主对象与 Meta 基础扫描确认的精确依赖对象，不把对象伪装成 file。IFC 仅含主对象；3DS／DAE 通过同一格式 Provider 读取主对象结构声明，既不列举授权范围外的前缀，也不读取贴图内容来发现组成。桶根模型、完整 refs 采集与前缀采集沿用同一规则；不完整、变化或无法精确表示的组成拒绝授权。System 准入 MinIO IFC／3DS／DAE 主对象，桶与前缀不能作为模型批准目标。

Manager 在每次源读取前以当前执行授权取得精确对象连接并通过 Provider 读取，暂存到本 execution 独占的 Infra 输入后调用 Model3D；Business MinIO 凭据不得传给 Runtime。MinIO IFC／3DS 删除旧来源直传／整前缀复制路径；缺少正式冻结来源的旧产物拒绝读取并要求重新生成，不补造历史授权。临时输入沿用成功、失败、取消和租约失效的清理规则。该契约绑定当前组成及读取资格，不提供对象历史版本授权或多对象一致性快照。

Manager 在真实 pending 行可领取之前，冻结 `model3d-glb-file-config/v1` 配置：保留完整原始 `source`／`result`／`options` 与转换字段，增加 `config_version`、`owner_task={id,version}`、`source_engine_version` 与 `source_composition`。ID／版本采用规范十进制字符串，任务配置取同一 SQL 快照；结果是当前 execution 的独占对象，不提前覆盖旧快显。`common/execution.ManagerModel3DReadScope` 保存完整原始 JSON 的 `config_digest`、`owner_task_id`、`owner_task_version`、`engine_version` 与 `file_composition`。摘要输入不超过 512 KiB，只规范 JSON 键序／空白，保留精确数值并拒绝重复键；不能经过 `JSONMap` 浮点往返或把摘要自身嵌入配置。逐文件大小在消费后作为本 attempt 的 I/O 完整性事实取得，不回写已冻结配置，也不进入授权边界摘要。

固定签发入口为 `POST auth/execution-authorizations/manager-model3d-glb`，当前 User 只提交原 execution UUID 和最长 3600 秒 TTL；System 从真实 pending、任务标识／版本、完整配置及独立 Meta 事实取证，再核验当前源规则。重复核清只恢复原 execution 的原授权与原期限，不另造 UUID、续期或复活。消费为 `POST execution-authorizations/{id}/manager-model3d-glb-accesses`，仅当前 Tenant 的 `addp-manager` Service 可提交同一 execution、attempt、lease token 与完整 scope；重核当前来源、权限、Grant、期限、配置、组成和 Engine 聚合版本。响应只含 `observed_at` 及当次核验的同 ID、同版本的准入 Engine，连接仅在本次调用栈用于精确 Facts／Open，不保存到配置、结果、日志，不在消费后另取可能不同版本的连接。客户端不跟随重定向、不隐式重放 POST、不缓存许可；服务未接通、返回畸形或任何核验失败均不能打开源内容。

Manager 的配置／签发绑定／可领取发布和最终结果切换各自保持 owner 事务原子性，不跨模块持锁。未知签发结果只在原 pending 核清：同一同步栈、同一 User Bearer、总预算一分钟内至多显式核清一次原 UUID；明确拒绝不重试，仍未知只关闭自己未绑定的 pending，不保存 Bearer 或新建后台补偿。每次源 I/O、转换返回和发布前使用同一冻结计划重核；发布失败不替换原有效结果。EA 不是已完成快显的读取凭据：FTP／NFS 受管快显的实际内容读取须以当前 Human Bearer，在同一次 System 裁决中匹配生成时完整 file composition 和 Engine 版本与当前事实；不以两次本地预检查拼成许可，不要求生成任务仍存在或重用终态 EA，不用 Resource Ticket 绕过。capability 仍遵循 Manager 共用契约，仅陈述资源／组成／结果事实，返回 URL 不授予内容访问权。共享类型／SDK 可先固定供消费方编译，但正式 System 路由、持久化、owner 接线与真实 FTP 验收分别记进展，不以契约存在宣称权限闭环。

模型原始内容同步读取与受管成果读取复用同一 `expected_source` 裁决：FTP／NFS／MinIO 的 IFC／3DS／DAE／OBJ／STL 主项及全部确认依赖，在既有 `storage-stream`、`storage-assets` 和 `downloads/file` 请求内，以当前普通 User Bearer 核验完整组成及 Engine 版本。每次源 Facts／Open 前重核，不缓存 Allow，不以 Resource Ticket 或后台 Service 身份替代；连接只消费与已核验版本一致的请求级快照。源 Grant 不替代入口自身的内容或下载 Permission。未准入模型／缺少组成事实的原始读取拒绝，非模型消费不因本轮调整扩大或缩小。STL 保持 single 且仅一个必需主文件；组成完整性来自独立基础结构观察，不使用 deep 几何 `scan_complete` 推断。

### 5.2.4 Develop GeoPackage 工作流的精确读写

授权最小单元始终是 Meta 识别的整个 DataItem。GeoPackage 内部图层、表和字段，以及同一 DataItem 的组成文件，不另建授权对象、逐项 Grant 或第二套 ACL；System 以整项 DataItem 及其明确边界为对象裁决当前读取权。Meta 提供的格式、布局和组成事实仅用于确定完整输入与实际 I/O 范围，不代替权限、不产生新的组件权限要求。读取 Owner 按该整项已经核验的范围打开组成，不逐个要求组成文件具有独立 DataItem 身份或授权。格式／组成未确认时阻断的是该输入的完整读取准入，不解释为用户还需申请内部组成权限。

GeoPackage 的授权边界由 basic scan 建立的单文件身份与逻辑组成事实确定，复用唯一 `file_composition` 表示：`format=geopackage`、`layout=single`、`data_type=container`，仅一个与 DataItem 物理路径完全一致的必需主 `container` ref。类型、布局、规范路径、refs 和完整父链必须相符；未完成 basic 身份识别时不能凭客户端字段或扩展名签发。授权事实不得依赖 deep 状态、内部图层枚举、数据库完整性检查或事务文件观察结果。文件损坏、暂不可读或不受当前计算器支持不改变授权对象，不阻止基于已识别 DataItem 的正式 Grant，也不撤销已有 Grant；实际读取按本次操作分别报告。已有 metadata_read Grant 不自动升级为 read。System 的持久约束仅接受同一 NFS 引擎的精确 file 叶子、单一必需主 container ref，且 ref 与 Grant 目标完全相同。

GeoPackage 的内部结构、数据库完整性及深度摘要由 deep 扫描按需生产，与授权身份和逻辑组成分开；basic 不读取整文件、不生成完整性结论。格式检查不得作为签发、延续或恢复 Grant 的条件。发布端负责事务提交、关闭写入器及完整文件无覆盖交付；读取端在实际消费时核验必要的源稳定性、事务文件和所用格式的安全准入。原生结构核验与观察规则见[GeoPackage 单文件完整性观察](addp元数据扫描机制规范.md#geopackage-单文件完整性观察)，它们不产生或撤销权限，事务文件不另建 ACL、不自动合并或修复。

共享协议分为同一阶段链上的声明与封存：Develop 保存 `DevelopFileIntent`，绑定完整持久配置摘要、DAG 读取节点与规范文件目标、预期 attempt、实际策略的 `timeout_seconds` 及可选保存目标；声明不包含由 Owner 推算的数据项指纹或边界摘要。System 用当前 User 请求反查可信 pending Intent，通过自己的 Meta 授权事实接口独立核验完整组成及当前源规则，再生成并持久封存 `DevelopBoundFileScope` 与唯一 EA。封存范围包含 Intent 摘要、实际源引用的指纹／边界摘要／Engine 版本和可选目标版本；Owner 保存原 EA 返回的范围引用，后续批准、资格观察、真实读取及发布均绑定同一 EA／Scope，不另算 Meta 投影，也不给 Develop 增加完整 Meta 授权事实读取权限。两个值类型分别表达声明和已核验范围，不是两条读取或授权路线；纯值工厂及摘要均不授予许可。

System 将本用途封存到既有 EA 的唯一 `develop_file_scope`，与 Manager 来源范围、内部任务和清理用途互斥；通用 engine-accesses 及从父执行派生均不能将它降为引擎级授权。可信 Owner 投影固定为 `GET /api/v1/develop/runtime/file-executions/{execution_id}`，仅当前 `addp-system` Tenant Service、专用 `develop.file_execution.read`；它只携带原执行、租户、Actor、完整原始 `config_json`、Intent、可缺的原 EA／封存范围及 Owner 私有准入状态，不重复公共生命周期或传输租约秘密。System 在锁外读取该投影及 Meta 事实，再在本地 IAM 事务核 common 执行原始配置、主体、真实 pending／running 状态、固定 attempt 与 lease，并在审计后复核当前凭据、权限、源规则及全部到期边界。签发请求只含 execution UUID 与预期 Intent 摘要；准入观察不返回连接；精确读取仅返回本次指定源的同版本 NFS 连接，不返回其他源连接。Develop 公共手工入口的保存链必须复用同一精确暂存、批准和发布机制；Meta 尚未识别单文件 GeoPackage 的规范身份与逻辑组成时保持关闭，不将 metadata_target 或文件扩展名替代内容授权。该接口接线与结构迁移本身不发布角色能力、不新增实际授权。

Develop 原公共手工提交入口（临时内容及已保存工作流）对 GeoPython 的 file `load` 按 Tenant、Engine 和规范路径读取既有 Meta 快照。缺少快照、格式未知或快照身份不符时拒绝。快照中的 GeoPackage 只分流到私有链，且只接受全部来源均为 basic 已识别的单文件 GeoPackage；DAG 可以只读，或含唯一一个 `mode=fail` 的 GeoPackage 文件 `save`，不得含其他写入、DDL 或外部效果。后缀、客户端 item ID、显示字段及失败的精确授权均不用于转入通用连接路径；通用读取额外拒绝与可信格式矛盾的 GeoPackage 候选路径，实际打开仍由 Runtime 限定驱动。普通工作流重新派发及父执行派生也检查该边界。临时内容和已保存工作流的普通手工执行使用同一入口；保存目标的节点、规范文件叶子及模式与来源一起冻结到原 Intent，再由 System 独立封存目标版本。保存用途在签发、领取观察及实际读写放行时复核发起人已有的 `develop.data_write.execute`，其有效期同样约束原业务截止；不授予该权限，也不以人工目标批准替代它。保存执行沿原 Supervisor 观察 `awaiting_approval`，只有原批准有效且当前资格完整的 `ready` 才可领取；不增加用户手动激活命令。混合来源、多目标、覆盖、追加、委托及父执行派生的 GeoPackage 请求继续拒绝。

Develop 先在本地事务登记原 pending 及冻结 Intent，再在请求栈用当前 User 凭据签发唯一 EA；绑定成功后由现有监督器观察可领取资格。已知签发拒绝关闭原未签发记录，未开始的拒绝只记录本次执行，不覆盖任务最近实际运行摘要。超时、依赖失败或响应未知时原记录不可领取，不补签、不回退通用授权、不保留 User Bearer。公开请求及成功响应形状保持既有契约。

来源清单必填且非空，保存目标可缺。无 `save` 的只读 DAG 仍逐项执行精确读取、主体、attempt、租约与预算核验，但不要求写目标或批准，禁止生成发布 operation、Prepare、放行、成果文件或初始 Grant。几何差集四条算子负例保留两次 `load` 和一次 `difference`，不能以写权限拒绝替代算子拒绝；只有真实保存链进入单次 create 批准与发布。

几何差集首期范围为同一非默认 Tenant 普通 User 的五份已识别 NFS GeoPackage 源，以及一次执行新建 `outputs/difference.gpkg`。保留现有十一项用户功能权限。Meta 以 basic 识别每份源的类型化 DataItem 及单主文件逻辑组成，System 按当前 User 的精确 `read` 规则裁决，Develop 冻结规范输入清单。GeoPython 只读取经核验的输入内容。`metadata_target` 继续只用于元数据查看。数据授权不要求先证明文件可独立读取；真实读取时若仍依赖 SQLite 事务文件、存在越界引用／链接或不能安全取得一致字节，则拒绝本次读取，不将其记为无权限或改变原 Grant。

单次写入批准由 System 办理。批准人必须是同 Tenant 的当前 User，同时具备目标引擎的管理范围和专用批准资格，且不得是本次执行的原发起人。System 从已绑定的原 EA／Scope 取得原发起人、Tenant、Engine 及版本、规范目标叶子、`create` 效果、唯一 execution 与预期 attempt，批准绑定这些事实和绝对有效期。父目录 `outputs` 只参与定位，引擎级 `write` 效果、选择目录和用户自报目标均不产生独立的目录写权。首期仅接受 `mode=fail`。并发同名、已有文件、改名、相邻路径、不同执行与迟到 attempt 均不得借用这次批准。

批准、领取观察和发布放行分别留痕。批准只使原执行具备被观察为可领取的条件，观察不消费批准。放行要求原运行 attempt 的当前有效 lease，且 System 须从可信 Develop 事实核对原登记的 operation 与 Provider 准备回执。System 在同一事务中校验当前资格并将批准唯一消费到该 operation。对同一操作已提交的历史放行记录可只读核清，不产生新的放行或派发；其他 operation 不能消费同一批准。业务文件是否实际新建及是否需要清理仍由 Provider 的实际证据决定。

暂存准入与最终放行使用同一次批准、同一个 operation。Develop 在现有私有执行记录内以当前 lease 首次登记 operation、批准编号、精确目标与 Runtime 成果的大小及摘要。System 核验该登记、原批准、目标引擎同版和当前 lease 后，才向受信任的 Develop 提供本次 Provider 私有暂存所需连接；此时不消费批准，也不准许向正式叶子写入。Provider 的准备状态只由 Develop 私有保存，可信 Owner 投影仅返回原操作绑定及已确认准备回执的摘要，不返回暂存位置、原生句柄或连接。未知准备不得变为已准备；未知连接响应或 Prepare 结果只核清原操作，不重新登记或重发业务写入。Runtime 和 User 不消费目标连接。

人工批准沿 System 的 `POST /api/v1/system/execution-authorizations/{id}/develop-geopackage-create-approvals` 办理，要求独立且不可委托的 Tenant Permission `system.develop_file_create_approval.create`。请求只接受预期 Scope 摘要及明确的绝对到期时间，批准期限不得超过原 EA。System 从原 EA 与可信 Develop 投影重建目标及尝试绑定，检查原任务仍为已绑定的未领取执行，并在同一事务保存唯一批准记录与审批审计。此权限定义不自动加入角色或授权既有人员。重复提交不替换、延长或创建第二份批准；该命令不发放连接、不激活领取。

System 的原领取观察对保存执行返回 `awaiting_approval`、带原批准编号的 `ready`，或原批准失效后的 `rejected`。真实源读取继续复核原批准；发起人当前权限、源规则、批准人原 Membership／授权版本、专用批准权限及目标管理范围均须仍有效。原 EA 撤销或执行取消后不再返回读写许可。纯只读执行不读取批准记录。Develop 普通手工保存提交接入这一领取观察与既有私有发布器；领取观察本身不读取内容、不生成发布 operation，也不执行目标 I/O。

目标命令使用 `POST /api/v1/system/execution-authorizations/{id}/develop-geopackage-publications`，仅限当前同租户 `addp-develop` Service 和既有 `system.execution_authorization.execute`。请求携带原执行、attempt、当前 lease、Scope 摘要、批准及操作编号，阶段仅为 `prepare` 或 `release`。System 反查可信登记，复核原 running 绑定、固定业务截止、源规则及当前批准资格。Prepare 准入原子登记批准、operation、目标及成果请求摘要和 lease 摘要，只返回同版本目标 NFS 连接和当前时间边界。Release 要求同一登记、同一目标与成果请求摘要，以及与可信 Owner 投影一致的已确认准备回执摘要，只返回放行回执，不返回目标连接。两个阶段各只接受一次，竞争或响应未知不得重新取得连接或放行。原操作的核清与清理沿专用恢复边界，不从这些命令重放。

System 原操作历史使用 `GET /api/v1/system/execution-authorizations/{id}/develop-geopackage-publications/{operation_id}`，无正文及查询参数，沿用当前同 Tenant `addp-develop` Service 与 `system.execution_authorization.execute`。以原 EA、批准和 operation 查已提交登记，返回原执行、attempt、Scope 摘要、批准、operation、成果请求摘要、暂存准入时间及可缺的放行时间和准备回执摘要。未匹配原登记的请求不返回历史。读取已提交的历史不要求原人员、批准、EA 或 lease 仍有效，不续期、不消费批准、不返回任何连接。准入历史不能证明 Provider 已暂存，放行历史不能证明文件已发布。

仅新文件 purpose 的未激活 pending 执行被 Owner 排除领取，不全局改变 Develop 的领取或父子执行生命周期。原 User 已表达 execute 意图后，现有监督器可在锁外向 System 查询原 EA／Scope／预期 attempt 的当前资格；只读范围不等待写批准，保存范围须满足原批准。该观察不返回业务连接、不消费发布放行，也不是缓存 Allow。Owner 只对同一 pending 及未变绑定作 CAS 激活，不新增用户手动激活动作或第二套队列。 准入续观测只列举原 EA 已绑定、仍在原期限内、尚未领取的私有记录；已到期的 ready 观察必须重核，不能直接领取。复用原监督器恢复轮次，先收尾原 I/O，再以当前同 Tenant 的 Service 凭据在锁外核验原 EA／Scope／预期 attempt；SDK 客户端必须按原记录的 Tenant 显式绑定，不以未绑定平台客户端代替；每轮最多 100 条、观察调用总预算 30 秒，该观察预算不产生 StartedAt 或业务截止。只有完整受控 ready 回执才 CAS 激活；未知或畸形 HTTP 结果不记为 ready，也不代作已知业务拒绝，不重签 EA、不保留 User Bearer。领取、取消或绑定变化使迟到观察失效，不改原执行终态。实际 claim 产生的 attempt 必须匹配。业务截止与当次观察窗口分离：`business_deadline` 在首次通过真实 running 源读取核验时，由 System 在原 EA 封存为数据库原始 `StartedAt + timeout_seconds`、EA、全部必要源 Grant 和人员资格期限的最早值；后续不可清空、重算顺延或随租约／机器凭据续期延长。pending 尚无 StartedAt，准入观察不预造业务截止。精确源读取响应必须携带该固定 `business_deadline`；`expires_at` 仅表示本次观察窗口，继续受业务截止、当前全部资格、当前 lease 和机器凭据期限共同约束。后续资格缩短或撤销仍在每次读取重核并收短窗口或拒绝，固定业务截止不构成缓存 Allow；后续资格延长也不顺延原业务截止。审批等待不消耗 execution 预算，也不延长 EA 或批准。原 T4 套件整体 600 秒上限独立保留，不按 execution 重新起算。返回 pending 或再次领取不能沿用原批准。

源文件长流读取在每份文件 Open 前取得新的精确源观察，并在当前 `expires_at` 到期前使用同一 EA／Scope／execution／attempt 和当前 lease 重核；重核成功只替换短观察窗口，不延长已封存的 `business_deadline`。窗口内撤权在下一次重核时发现，不承诺即时感知。重核须在原窗口内成功完成，原窗口到期、撤权、取消、绑定变化或核验失败后，该读取会话永久失效，不用迟到响应恢复，也不重开或重放原文件。失效后不再发起后续读取、不交付在途读取迟到的字节，已读私有暂存不得成为有效 Runtime 输入；向 Runtime 交付前仍须核对读取会话有效且已取得完整 EOF、大小和 SHA-256，实际打开会话的来源闭包证据不能代替 EOF 或内容完整性。业务截止用于本次执行的总时限，不能把短观察窗口作为不可更新的整次文件传输截止。阻塞 NFS Open／Read／Close 未实际返回时，即使 Owner 已拒绝交付，也分别保留未停止／待清理状态；不得把 Context 取消或关闭请求当作底层 RPC 已停止的证明。

Owner 输入准备仅消费原始持久 `config_json`、Intent 与已封存 Scope；以读取节点 ID 和规范 locator 逐项证明完整 DAG 覆盖，保留原始 JSON 数值，不经过浮点 JSONMap 往返。源文件按节点顺序流入 Owner 独占的私有目录和序号文件，目录 0700、文件 0600，不把业务路径或节点 ID 拼入本地文件名。Develop 复用 Meta 的单文件 NFS 观察，在 Open 前及完整 EOF、关闭源句柄后检查精确主文件大小、可用版本与修改时间一致，并确认三个附属日志目标明确不存在。检查仅使用当前授权会话内同版本的 NFS 连接，不打开日志或枚举目录，阻塞的观察与阻塞读取一样保留未停止状态。暂存完成后复用 Common 原生校验直接以只读 immutable 模式检查本地暂存文件，不从业务路径再读或复制另一份校验文件。在打开任何源前，Develop 对本次全部精确源复用已有 Data Protection 原生读取门禁，只允许当前未受保护的 DataItem 进入本执行。在私有读取、交换与计算期间持有既有保护读会话，拒绝、取消或结束后释放，不把 System 的精确 read 许可视作绕过数据保护的依据。只有所有源的前后观察、完整 EOF、实际大小、SHA-256、暂存文件原生校验、同一业务截止和当前授权观察均成立，才形成 Runtime 清单。Runtime 工作流只移除已绑定 load/save 的业务资源定位，保留算法参数；源连接、用户凭据和 Owner 本地路径不进入清单。同一原 Owner 私有执行行分阶段保存本地 I/O 事实：在第一份源读取前登记原本地目录和 Owner 实例归属；全部源完整 EOF 后，才登记原 Runtime UUID、完整清单和所属 execution／attempt／EA／Scope，确认提交后才能创建 Runtime。每次登记均在同一事务中以当前有效 lease 完成末端校验；实例标识复用执行监督器已有 InstanceID，不从任务参数或目录名称推断；目录阶段不伪造尚不存在的 Runtime 清单，后续登记必须匹配已保存的目录归属；原登记只允许首次写入，之后仅重读，不替换 UUID、清单或目录归属，也不进入公共执行结果或 System 私有资格投影；登记失败或结果未知不发创建请求，创建或上传结果未知不换 UUID、不隐式重放。上传继续受全部源观察和固定业务截止约束，全部输入确认 ready 后才可沿原 `/api/workflow` 派发一次；任一失效不能用已暂存字节继续派发。清理仅针对该原私有会话；本地回收须为同一 Owner 实例且仍持有原批次／源会话，核对原登记的执行、attempt、EA、Scope、Runtime UUID 和目录归属，不能仅凭数据库路径字符串重建可删除对象。跨实例、丢失原会话或底层源／Runtime 尚未核清时继续表达 pending，不重新读取源、不重传或重算，也不将本地目录移除当作全部清理完成。以上输入准备／传输不授予业务保存、发布或成果初始使用权。 Owner 在原监督器领取后，仅将已登记且本次尚未开始 I/O 的私有只读执行交给精确文件执行器，不走通用业务连接展开；保存链仅在原目标已封存、原批准就绪后复用同一精确执行器与发布器，不回退通用引擎连接路径。该分支使用原数据库 StartedAt 与冻结 timeout 的绝对截止，并消费 System 首次源读取封存的更短业务截止，不在 Worker 内重新起算预算。私有清理待办仅保留本实例的原批次／句柄，复用原监督器恢复轮次核清原登记及原 Runtime，不创建新公共执行或持久队列。业务已经终态但源 RPC 或 Runtime 在途操作尚未证实停止时，仍计入该实例原 Workflow 额度；仅确认所有运行体停止、只剩文件删除待清理时才释放计算额度。该核清不续期业务截止、不续跑或复活执行，不影响 Query／Notebook 额度。原 Runtime 明确返回仅目录删除失败时可证明运行体已停止；取消请求、超时或未知 HTTP 结果不能作停止证明。进程退出后不从持久路径重建删除会话，继续保留未核清事实。

Develop 在真实读取及发布前使用同一冻结计划复核当前授权与执行租约，Runtime 不消费可用于任意业务路径的挂载或连接。本次编码使用私有暂存，最终发布保留 GeoPackage 完整文件的无覆盖语义。System 原子核验并放行是该次在途发布的准入时点；已放行的在途发布允许完成，撤权或取消阻止后续放行。Develop 在放行及实际发布前持久化原 execution、attempt、批准、目标和 operation 的归属及 Provider 准备回执；未知结果只核清原操作，不重发业务写入。Provider 不裁决 IAM。Provider 的 `ContentWriteEvidence` 可用于表达实际提交事实，仍须证明无覆盖发布与原执行、精确目标的归属，不能仅凭成功返回、目标路径或事后扫描推断新建。GeoPackage 现有格式、几何及保存模式规则见[矢量算子契约](workflow/矢量算子契约.md#save-geopackage-业务字段与发布契约)。

Develop 仅凭同一操作的实时 System 放行回执，在当前 lease 的事务内一次性登记派发。回执必须绑定原 EA、Scope、execution、attempt、批准、operation 及已确认准备摘要，且仍在当次窗口和业务截止内。登记一旦提交，该操作的物理结果即按 `unknown` 对待，直到 Provider 给出证据。只有本次登记明确成功的调用可以在原窗口内进入 Provider 的唯一发布调用。登记响应未知、进程重启或已登记后再次读取时，只能读取原记录供核清，历史放行回执不得转换成新派发资格。派发登记及 Provider 私有状态不进入可信 Owner 的授权投影或公共结果。

在同一次调用内，Develop 只使用暂存准入返回的同版连接，分别受暂存、实时放行窗口及固定业务截止约束。无覆盖发布结果未知时，仅在两个窗口均有效且底层调用已经返回的情况下，以同一连接对原准备回执执行一次 Resolve。明确的新建或未发布证据先在原租约下保存，再按该证据清理原 Provider 私有暂存；只有 Provider 确认清理才更新清理状态。结果仍未知、回执不一致或租约失效时，不猜测归属或重发写入，留待专用恢复。此处的 Provider 证据仅记入原执行私有记录。

Prepare 前即须持久化并登记原 operation，恢复不只依赖已存在的 release。租约失效后的专用恢复投影只能为原已登记 operation、原 Engine 同版及原绑定提供 Resolve／私有暂存清理，不重发 Prepare／Publish，不成为新的业务 Allow，也不长期封存请求级业务连接。截止后晚到的发布观察只追加到原 operation 的私有核清事实，不改 terminal execution、不复活旧 attempt；首次晚到事实不能补造有效租约内的成功提交或自动签发 Grant。现有 NFS 阻塞 RPC 不承诺立即随 Context 中断，未核清的操作如实保留 unknown／cleanup_pending，不能宣称全部副作用或清理已在截止内结束。

恢复连接通过 `POST /api/v1/system/execution-authorizations/{id}/develop-geopackage-publications/{operation_id}/recovery-accesses` 获取，无正文及查询参数，沿用当前同租户 `addp-develop` 与 `system.execution_authorization.execute`。System 先匹配原准入历史和可信 Owner 的原批准、操作、成果摘要，再在事务内核对当前服务资格与目标引擎同版，返回最长 15 秒且不超过当前服务凭据期限的恢复窗口。响应仅用于原操作 Resolve 和私有暂存清理，原 EA、源授权和业务租约过期不阻止该恢复观察，窗口不延长业务截止。目标引擎换版、原绑定矛盾或历史不存在时拒绝，不创建新批准、派发或初始 Grant。

Develop 的原监督器每轮在 30 秒观察预算内按 execution ID 游标读取最多 100 条已终态的原私有发布记录，完成一轮后重置游标；不构造新执行或持久恢复队列。已确认清理的记录直接跳过。每次恢复重新获取 System 的原操作恢复观察，未知派发只 Resolve，已有确定物理结果按原结果核清清理。尚无可信准备回执或归属未明的准备继续保留 pending。恢复事实单独保存，允许在业务期限或 lease 结束后追加，但不得修改公共执行终态、原运行提交证据、可信授权投影或触发扫描和 Grant。清理只在当前恢复窗口内发起，返回晚到时可记录已经确认的物理清理事实；窗口结束不视为底层 RPC 已停止。

Runtime 成功后，Owner 只按原私有会话取得唯一输出回执，并将输出单次下载到原批次目录中的固定文件，以独占创建和 0600 权限保存。复用现有私有下载的精确大小、EOF、SHA-256 校验，完成同步和本地单文件 GeoPackage 原生校验后才进入 Provider 暂存。下载失败或响应未知不重下，私有文件随原批次清理。执行成功提交在同一数据库事务中锁定原 execution 和私有发布记录，要求当前 lease 及原租约内保存的 `published/created` 证据；晚到恢复事实不能替代。资源输出与基于原冻结 DAG 的血缘事实随成功终态原子保存。扫描通知与初始 Grant 的接线另行验证，保存入口在其具备前保持关闭。

成功提交后，Develop 在有效 lease 下保存同一次实际读取和新建提交的私有回执，Meta 沿可信产出执行只识别该成果，System 按[任务产出数据与初始使用权](#558-任务产出数据与初始使用权)向原发起人签发唯一精确 `read` Grant 并核定期限。签发待处理、拒绝和产出成功分别表达。签发故障只核清或续办原命令，不能重发写入。现有其他算子、引擎、发布服务和目录持续写入不纳入此首期。以上为接入规范，实施及真实授权证据仍待补齐。

### 5.3 Notebook 会话授权

Notebook Session Authorization 是独立的短期用户派生事实，不是 Delegated Token。Develop 在 Session 创建的同步栈用当前 User Bearer 请求签发，随后丢弃 Bearer；授权绑定 Session、Task、Tenant、Principal、Membership、Token Family、授权版本、固定 `develop` audience、操作和期限，不保存连接或 Engine 白名单。

仅 `addp-develop` Runtime 可凭自身 Service Token 消费：目录操作当次核验；数据查询／扫描须为新 execution 派生单引擎 read 授权，保留 Session 来源。Session 到期不刷新；同 Family 正常 Token 轮换不影响 Session，但撤销、重用、身份／授权版本变化或显式关闭必须阻断消费并联动撤销有效派生授权。长扫描在短租约边界重核，失效关闭 Cursor；Develop 重启丢失本地 Session 后拒绝访问。authorization ID 只是引用，不是 Bearer。

### 5.4 Owner Resource Grant 与 Asset 履约

业务 owner 使用结构化 ResourceAccessRule 保存资源、主体、Permission、Allow／Deny、期限、来源与撤销；最终判断归 owner，不在 IAM、Catalog 或 Asset 建立全平台 ACL。源数据的特殊 owner 分工见 5.5。

Asset 只负责申请、审批、期限及跨 owner 履约：`pending → effective → revocation_pending → revoked`。只有 owner 幂等写入 Grant 后才能标记 effective；撤销／过期经 owner 确认后才标记 revoked。未知结果保持待核清，用原来源编号恢复，不回退软授权或专属凭据。

Data Application 的 Grant 只允许打开应用配置，内部组件查询仍由 Service 独立校验；未接入履约的资产类型不能标记 effective。Portal 仅是消费 BFF，不复制 Grant、Token 或运行配置。

### 5.5 目录责任、业务授权决定与源数据授权

源数据规则只由 System 引擎访问控制领域维护，直接批准与 Catalog 业务批准共用同一写入与读取判定路径。实际访问的资源单位是 Meta 识别的 DataItem；Grant 目标可为精确 DataItem 或持续节点范围，两者使用同一规则存储与裁决。源数据领域不再设置显式 Deny；默认拒绝不等于显式禁止。5.5.7 定义数据项边界，第九节区分规范与当前实现，不以原生表试点代表全平台已贯通。

#### 5.5.1 事实与职责分开

- Catalog 拥有责任关系与业务决定；业务负责人确认共享合理性，数据管理员和技术维护者承担各自责任。责任、编目维护、业务批准、授权办理和内容读取互不自动赋权。
- 首次保存有效 business_owner 的建立依据须持久保留，不因移交、弃用或恢复清除；它不是当前资格，也不自动启用业务批准。
- 决定绑定精确目标、接收主体、动作、期限、真实确认人及责任依据。首次受理重核确认人的原 Membership、原责任连续性和当前确认 Permission；其历史授权版本只作审计，无关版本变化不作废决定。办理人则须严格匹配原请求授权版本。原责任中断后同人重新接任不复活旧决定。
- 普通只读允许同人确认并办理，或显式批准本人／本人当前项目组受益；各项资格分别核验并留痕。不能推广到敏感原值、写入、DDL、Break-glass 或三员分立约束下的自批。
- 只读 Grant 显式选择 `at_time` 或 `until_revoked`：前者要求未来绝对时间，后者不带日期。遗漏、未知模式或矛盾参数拒绝；不使用远期日期／infinity 伪装长期有效，不默认续期，也不扩张委托或临时接入期限。
- 功能通过 Role Assignment 显式分配；业务确认与办理使用独立 Permission，机器 execute 不代替人类 create。角色模板、权限发布、守卫、审计与测试须同步，不自动补权。
- “数据授权办理员”内置角色统一组合直接批准与业务批准后的办理能力，保持两种命令的独立 Permission；直接签发核验当前办理资格，业务办理核验有效业务确认。角色本身不授予业务确认、引擎管理范围或内容读取。角色目录与迁移规则见《权限与角色发布规范》。

共享结果读取也须独立控制：条目可见且具备 `catalog.entry.read` 与 `catalog.sharing_decision.create` 的原确认人可看本人确认结果，当前业务负责人可复核本条目历史；不要求同时有办理权限。办理人读取候选或本人原办理则须独立办理 Permission 与当前引擎管理范围。仅编目、部门／项目组关系不取得这些资格；候选只暴露必要身份摘要，不能借 Runtime 身份枚举其他范围或在查询中签发授权。

#### 5.5.1.1 元数据查看与数据读取

2026-11-01 已确认，部分实现。当前实施、部署与实际验收范围统一见[实施范围与验证](#九实施范围与验证)。

**2026-11-03 跨引擎元数据接入。** 元数据查看不以格式已接入内容读取为前提。既有完整内容边界继续复用；其余可靠识别的 DataItem 在同一授权事实／Grant 契约中使用显式 `metadata_target` 边界，绑定真实类型化目标、布局、数据类型、格式和持久组成摘要。缺失、边界已变化或尚不支持的目标按项返回不可见，不改变其他候选的当前裁决；依赖不可用仍整体失败。该表示只准入 `metadata_read`，所有内容裁决、Execution Authorization、初始成果使用权和 `read` 签发必须拒绝它，不补造内容完整性。System 仍独占规则与撤销，Meta 不保存 ACL；节点范围授权准入 PG 根／schema、NFS 普通根／目录及 MinIO bucket／普通 prefix；其他引擎仅使用其已接入的精确 DataItem 边界，不扩展 Catalog 办理。

源 DataItem 统一支持“仅查看元数据”和“读取数据（包含必要的元数据查看）”两种授权内容。接收方仍为账号、部门或项目组；System 为唯一 Grant、有效期、撤销和审计 owner，Meta 不另存 ACL，Catalog 不复制 Grant。

| 信息 | 默认读取依据 |
| --- | --- |
| 名称、类型、格式、所属引擎及逻辑目录路径 | 元数据查看或有效数据读取授权 |
| 允许展示的字段名／类型／主键、坐标系及容器结构 | 元数据查看或有效数据读取授权，并限制到允许展示的字段范围 |
| 样本、正文、业务值、内容摘要、行数、规模变化、取值分布、空值率等统计 | 当前数据读取权限及适用 Security 约束 |

- 普通账号不因属于租户而看到全部元数据。没有目标授权时，树、搜索、计数、详情及血缘不得泄漏不可见目标；可见后代的祖先只返回必要导航事实，不由此暴露兄弟资源。管理／办理职责的最小目标核对投影不转化为通用元数据查看权。
- 逻辑路径不包含源连接、宿主机绝对路径、挂载配置或凭据。字段注释、默认值和表达式可能带业务值，不能随原始 attributes 无差别返回；白名单展示无内容的技术事实。basic／deep 是采集深度，不决定结果可见级别，低成本采得的行数也不自动公开。
- 数据读取按自身有效期包含必要元数据查看，不隐式新增第二条 Grant。仅元数据查看不允许内容预览、下载或查看统计；采集结果已缓存不免除当前授权。未来行／列限制下不能返回范围外结构、全表样本或全表统计，无法约束时拒绝该部分。
- 两种内容复用 System“引擎管理 → 授权 → 数据授权”的新增／当前／全部记录页面。新增表单明确选择权限类型，默认不预选；保留共享树多选、节点选择和递归选择，不增设元数据授权 Tab。沿相同直接办理功能 Permission 与当前引擎管理范围，不新增元数据审批角色、不自动补权。
- Catalog 始终复用“批准数据使用 → 办理访问授权 → 批准与办理记录”。业务负责人在既有业务批准资格下确定权限类型、接收方及期限；办理人沿现有功能 Permission 和引擎管理范围办理，不能把仅元数据查看的决定升级为数据读取。升级须新的业务批准或另一次合法直接授权，记录真实来源。
- 创建业务批准必须显式提交 `action`，缺失或非法值拒绝；同参重试、当前候选去重、接收方已有批准提示及办理绑定均包含该动作。办理请求只引用原决定，系统受理与签发再次校验原动作，不接受办理人重新指定或升级。Catalog 业务批准与办理暂保留既有 PG 目标准入；System 直接元数据授权已扩展可靠识别的精确 DataItem，其他引擎内容读取仍沿已接入 profile。
- 独立元数据 Grant 与数据读取 Grant 各保留来源和期限。数据读取撤销或到期后，只有另存且当前有效的元数据授权才可继续查看；撤销不能偷偷补发低级 Grant。当前授权及撤销确认须显示实际剩余能力，去重和收权按对应动作及具体来源执行。
- 两种规则动作固定为 `metadata_read`（元数据查看）与 `read`（数据读取）。个人、当前部门／项目组及命中的节点／DataItem Grant 取并集；`read` 包含必要元数据查看，`metadata_read` 不能升级为 `read`。撤销某一来源后重新计算剩余来源，不补发、恢复或延长其他 Grant。集中扫描按功能权限与引擎管理范围执行，不消费个人查看规则。
- 取消源数据显式禁止及解除流程，不提供个人排除或节点内的单项拒绝。组织来源仍有效时，撤销个人来源不能收回组织权限；节点来源仍有效时，撤销单项来源不能排除该项。需要缩小访问范围时应收窄或撤销原范围授权。功能 Permission、引擎状态、Security 和真实执行约束独立成立，不因取消 Deny 放开。
- Catalog 可显式发布经整理的业务名称、简介与申请入口，但这种业务发布不开放原始 Meta 技术详情或样本。没有 Catalog 时，两类直接授权仍由 System 独立办理。

本节为已确认的授权目标，不表示现有元数据查询已经执行细粒度过滤。实现需同时覆盖签发、查询、列表／树／搜索、样本与统计出口及撤销，不能仅增加前端选项而保留按 Tenant 全量返回的旁路。精确 DataItem 元数据查看按可靠身份统一接入；节点范围按下述持续节点范围准入，内容读取和 whole 边界不因元数据扩展而扩大。

#### 5.5.2 接入阶段与 Catalog 独立性

- 管理资格为当前 Tenant 的授权管理员、目标 Engine 的有效初始管理人或有效受托办理人；均须具备当次操作 Permission。管理员无需委托自己，受托人不能转委托，管理资格不产生读取 Grant。
- 委托绑定一个 Engine 和一个有效 User Membership，必须有原因及未来到期时间，且不晚于 Membership 到期；不允许重叠、永久委托、隐式续期或恢复。创建／撤销同事务保存审计并使受影响旧会话失效；撤销保留历史。
- 引擎登记、发现、扫描与内容读取是不同动作。新注册引擎为真实注册人的当前 Tenant Membership 建立本引擎的初始管理范围：无固定到期时间，限原成员关系和引擎存续期间有效；有权引擎授权管理员可显式撤销并保留审计。撤销后重新登录不恢复，原成员关系结束后重新加入不继承，不用 `CreatedBy` 动态推导不可撤销资格，也不将现有限时委托伪装为永久委托。存量引擎不凭历史创建人自动补发。注册角色包含扫描所需功能，范围本身不授予角色或数据读取 Grant；已有账号的角色分配仍须显式管理。接入不授权修改源库、安装触发器或创建源端角色。
- 临时接入只读必须限时；长期直接批准可显式选择直至撤销。新直接办理仍须当前办理资格，业务办理仍须有效业务依据。
- Catalog 可选，不成为 System 启动、既有 Grant 读取或其他模块 Ready 的前提。直接授权按 5.5.5 的当前办理功能权限与引擎管理范围裁决，不依赖 Catalog 部署状态，不恢复旧互斥模式或直接许可配置。

#### 5.5.3 责任变化与访问规则生命周期

责任移交或条目弃用阻止旧依据支持新的业务确认／受理，不自动撤销已有 Grant。修复责任通过显式治理权限进行，不把修复人自动变成业务负责人或数据使用者；认证、弃用等资源状态仍分别约束操作。

业务确认、System 正式受理、Grant 签发是三个事实。受理以 System 事务提交为分界，并须与责任变更、来源重绑和弃用形成可验证顺序：变更先完成则重新确认；受理先完成仅允许原同次、同参数办理继续，仍重核办理人、管理范围与接收主体。

自动签发窗口为受理数据库时间起 5 分钟，且不晚于拟授权绝对到期时间；长期读取同样只有 5 分钟办理窗口。窗口不限制人员操作时间，也不是访问期限。提交前按数据库墙钟复核，重试不刷新窗口。已签发原请求只返回原结果；未签发且过窗须重新取得依据，不恢复已撤销或过期 Grant。

撤销须当前管理资格与独立撤销 Permission，通过一份有效记录定位并原子收回**同目标、同接收方、同动作的全部有效 Grant**，保留不可变历史、收回编号集合与审计。同操作者及原参数重试只恢复原收回结果，不收回后来新授予。个人与组织、节点与数据项均为不同来源，撤销一种来源不删除其他来源；页面须说明收回后的实际剩余能力。

Grant 自然到期后不追加首次撤销；到期前已成功的原参重试可恢复历史，仍须当前操作者有资格。引擎停用、Meta／Catalog 不可用不阻止合法收权，收权不读取源内容。源 Deny 的接口、功能权限、规则表及查询分支随迁移退役，不保留只隐藏页面的旧路径；通用审计保留。

#### 5.5.4 逻辑资源目标与执行约束

- 授权针对逻辑 DataItem，复用 Tenant、Engine、逻辑路径与现有指纹，不追踪物理创建实例。外部同路径重建不因物理替换自动撤权，但必须证明当前布局与组成仍在原授权边界内。改名、来源重绑或换 Engine 不自动转移规则。
- 目标缺失时访问失败，不删除原规则；重新出现也不恢复已撤销权限、续期或扩大范围。Fingerprint、Meta 行号、CatalogEntry ID 本身都不是许可。
- 查询须由 Provider 以同一个 PreparedQuery 证明完整 QueryReadSet，再核验所有依赖并执行该计划。文件读取遵循已识别的组成边界；解析失败、依赖不完整或执行范围不可约束时拒绝，不用正则猜测，不暴露宽权限源凭据。
- System 规则观察在同一已提交快照及数据库时间中核验可信凭据、当前身份／授权版本、功能条件、Engine、直接组织成员关系、精确及节点范围 Grant；结束前复核自然到期。任一实际目标不满足即拒绝，父部门、责任、委托或历史办理人不推导读取许可。
- Manager 预览与剖析结果检查均固定要求 `manager.data_item.read` 的当前 Tenant Assignment；缺少资源归属证明时，组织范围分配不能升级为 Tenant 范围。两者接受第一方／OAuth API User，只有预览另接受精确 `audience=manager`、唯一 `data.preview` Scope 的 Delegated Token；均不接受 Service Token 或 Resource Ticket。入口不允许客户端自选 Permission 或自报主体。
- 返回的 `observed_at` 只是当次观察，不是访问令牌、缓存 Allow 或完整 Security 裁决；实际 owner 继续施加 Security 并约束真实执行。规则底座保持私有，其他 owner 须定义固定操作契约，不能调用通用自选权限接口降低门槛。

#### 5.5.5 直接授权、业务批准与明确交接

**2026-10-31 已实现，当前版本真实运行验收待补。** 取消 DataItem 的“是否允许直接授权”配置、部署默认和许可管理步骤。两种入口共用唯一 Grant 写入及读取裁决；下列规则取代旧许可模型，旧版本验收记录只证明对应固定版本。

- System 直接授权要求当前 Tenant User 具备 `system.engine_access_grant.create`，并具有当前租户引擎授权管理员资格或目标引擎的有效管理委派。由角色提供功能 Permission，不新增角色、不自动分配账号，不因删除许可步骤扩大引擎管理范围。
- 直接授权不需要业务批准，不读取 Catalog 的部署、启停或在线状态，也不再读取任何逐 DataItem 的直接许可。仍独立核验有效接收方、完整 Meta DataItem 边界、引擎版本、明确期限、命令幂等、重复关系和审计；读取继续受功能权限、有效 Grant 与 Security 约束。
- Catalog 始终先取得有效业务批准，再由具备办理 Permission 和目标引擎管理资格的人员办理。两种入口不是互斥模式，办理来源不改变有效关系去重，不覆盖或延长其他来源的 Grant。
- 移除直接许可 Tab、配置接口、请求／响应许可字段、许可专用 Permission 及持久配置。本次开发升级按用户确认清理旧 Grant、Deny、撤销和业务办理记录，不迁移旧授权；账号角色、引擎委托、采集资格、源数据与通用审计不变。任务产出沿既有清理规则保留最小拒绝重放键，避免旧执行重新签发。旧许可配置退出后不参与初始成果资格或离线拒绝的身份依据。

**数据项选择与批量办理。**

- “新增授权”使用 Meta 已识别 DataItem 的唯一资源树，支持叶子多选、取消、节点全选和显式递归选择。复用共享资源树及权威祖先链，不拆分显示名称猜层级，不枚举原生文件替代 DataItem。
- 多选、节点全选、递归和半选状态唯一归属共享 `ResourceTreePicker`；System 仅保留 DataItem 边界核验及批量签发。完整快照加载失败、异步核验失败或上下文变化不得提交局部或过期选择。
- PG、NFS、MinIO 新增授权以同一资源树混选精确 DataItem 与真实节点，取消互斥模式切换。勾选 DataItem 只授权该项；勾选节点直接形成持续范围，不展开成叶子命令。展开节点仅浏览，不勾选或授权其子项。已选清单逐项标注目标类型，每个节点独立指定递归并说明未来新项覆盖；搜索仅影响显示，不缩小已选范围。重叠范围保留为独立来源并提示，不自动替换选择；取消叶子勾选不能排除节点已覆盖的项。whole 容器仍是一个 DataItem 叶子。其他未接入节点范围的引擎仍沿共享选择器按当前 DataItem 快照批选，不由此扩展持续范围。
- PG／NFS／MinIO 混选树按展开节点读取直接子节点和直属 DataItem 身份，禁止为展示一层目录而遍历全引擎身份分页。直属项由 Meta 在同一租户／引擎快照中按类型化父路径定位后过滤、计数和分页；whole 容器归其外层目录的一项叶子，不向树暴露内部文件。搜索在 Meta 按名称／路径先过滤，再返回有界结果，不在浏览器下载全量后筛选。仍复用身份清单机器权限、网页办理资格及跨服务前后重核，不读取源引擎、不扫描或签发。
- 数据项批量操作须完整收集当前展开层的身份分页（未接入节点范围的引擎仍为完整快照），保持现有导航容量及单批签发上限，超限明确失败而不截断。持续节点范围只核验精确节点事实，不因叶子数量多而展开或截断，不把本次叶子清单当成授权边界。
- 引擎后台状态轮询只更新检测时间等状态展示；当前引擎身份、类型、名称与可用性未变时，不重建授权资源树、不重读身份清单、不改变展开、选择及草稿。人员／租户／权限或引擎可用性真实变化仍沿现有会话边界中止操作、清理或重新核验；静默轮询不得冻结或绕过这些变化。
- 所有目标在首个写请求前重新核对完整边界、接收方和期限；每个目标沿现有唯一签发接口独立原子提交。结果逐项展示，成功项不重发，失败／未知保持原命令供核清，不自动改参或换编号。取消确认零写，身份变化清草稿，同身份重新认证后重新核验。

跨模块受理与恢复遵循以下唯一协议：

1. Catalog 先在本地事务持久化精确请求及待核清依据，提交后发送；请求固定租户、可信调用服务、原办理人／Membership／授权版本、目标、决定与完整参数。
2. System 以独立服务身份反查已持久业务依据，再进入本地资格与目标事务；不在持锁期间跨模块反向联网。浏览器自报批准、身份或时间不能作证明。
3. 当前资格按 Principal 去重升序 → Membership／Tenant／接收组织 → Engine／委托 → 请求／精确目标取锁，等待及提交前按数据库墙钟重核。确认人的责任连续性归 Catalog，IAM 资格归 System；接收组织可无成员，不复制个人 Grant，也不冻结受益账号的授权版本。
4. 待核清期间，影响依据的责任、弃用和来源变更必须阻止或暂缓；人工与后台同样执行。其他条目、普通说明、Ready 与既有读取不受影响。不能仅靠跨服务时钟或短暂本地锁证明顺序。
5. System 串行裁决同编号受理与关闭，结果不可改写。只读 resolve 不产生关闭／签发／延期；仅明确 `found=false` 才可关闭原请求，404、拒绝、超时或畸形响应不得当作未找到。
6. 超时、重启或窗口到期不清除待核清保护。关闭及历史核清仍检查当前调用资格，但不要求原人员继续有效；未知结果先查原事实，不换编号或补参数。
7. 前台与后台共用签发续办路径：accepted 后先查签发历史，明确未找到才同参签发；closed、已有签发或明确过窗才结束对应调度。受理核清与签发核清独立，解除责任保护不等待签发；Catalog 不保存第二份 System 状态、Grant 或窗口，也不在 GET 中续办。

签发只读查询在原 `found/grant` 历史之外，未签发时可返回 `unissued_reason=relation_exists|window_expired|closed`，说明本次观察的签发阻碍；无阻碍时省略。System 在同一已提交只读快照中匹配原请求、查询原签发、按数据库墙钟核验原窗口，并检查同租户、引擎、DataItem、接收方及动作的现存有效授权关系。原签发历史优先于当前阻碍；窗口结束优先于重复关系。原因不是持久失败历史，也不是当前访问 Allow，不返回其他 Grant 身份。Catalog 动态透传该原因，后台观察到重复关系时暂不重复签发，仍仅在原窗口内观察恢复；原窗口结束则核清终止续办。页面区分“已有授权，本次未重复签发”和“原窗口已结束，本次未签发”，不再统称等待签发，不由浏览器时钟推断终止，不自动撤销、覆盖、续期或新建请求。

规则写入、撤销与高风险审计须同事务；绑定冲突、未找到、资格失败和依赖故障分别处理。精确协议见[企业资源目录实现规范](addp企业资源目录实现规范.md)与[System IAM 数据模型规范](../../system/docs/IAM数据模型与迁移规范.md)。

#### 5.5.6 直接批准与授权管理

- 日常办理为“选数据 → 接收方 → 操作与期限 → 确认”。直接签发只要求相应功能 Permission 与当前引擎管理资格。业务批准流程始终保留，两种来源不能相互覆盖已有 Grant。接收方为当前 Tenant 的有效账号、部门或项目组，组织按当前成员关系生效，不展开个人副本。
- 同一 Tenant、目标、接收方类型／编号、动作构成一条授权关系，不按批准来源或签发者拆分。已有有效关系时拒绝新命令重复签发，不静默改期限；原命令同参重试只恢复历史，异参冲突。修改期限须明确收回，再按当前有效的业务批准或直接办理资格重新办理。
- 精确数据项批量只覆盖本次明确选中的独立目标，不包含未来新增资源；持续节点授权按下述范围语义覆盖未来新项。不同目标仍独立原子提交，显示逐项结果；失败／未知保留原命令，不伪装整批成功或自动换编号。
- 当前授权合并有效来源；选定账号时展开其个人和当前有效部门／项目组来源。全部授权记录包含有效、已撤销及已到期记录；历史账号筛选只匹配直接接收方，不按今天的组织关系改写历史。撤销优先于到期分类。
- 表／路径、接收方类型及具体接收方条件在计数和分页前取交集。文字搜索与精确资源选择互斥，条件变化自动查询并隔离旧响应；撤销定位具体来源，不对账号聚合行整体收回。只显示有权读取的身份名称，不要求手填 ID。
- 当前关系或 `rule_covered` 只说明规则来源覆盖，不保证实际可读；页面说明来源、期限及拒绝状态，不混淆新增办理、当前查询与全部记录。查询无副作用且不可缓存，不能借查询取得内容或办理权限。
- 当前查询明确区分节点范围记录与数据项记录，并展示账号的个人及当前组织来源；节点记录展示类型化路径、递归、动作和期限，不伪造逐项签发历史。选定数据项时可解释覆盖它的节点来源；撤销按具体来源目标执行，不对聚合结果笼统收回。没有有效来源时不生成仅拒绝记录。

**持续节点范围。** 节点 Grant 与数据项 Grant 共用来源、接收主体、动作、期限、幂等、撤销和审计链，不另设目录 ACL。已确认扩展至 PG 的真实根／schema、NFS 的普通根／目录及 MinIO 的 bucket／普通 prefix 节点；不扩展其他引擎，whole 容器继续是精确 DataItem，不可作为普通目录节点签发。

- MinIO 沿唯一 `service → bucket → prefix → object` 类型化路径匹配；服务根只导航，不签发整服务节点 Grant。bucket／prefix 可独立授予 `metadata_read` 或 `read`（包含必要元数据），当前及未来合法识别的数据项按范围覆盖，不展开成逐项 Grant。非递归只覆盖直属 DataItem，递归覆盖后代；同名相邻 prefix、其他 bucket、Tenant 或 Engine 不得混入。所含部分格式只具备元数据边界不阻止节点选择内容读取；读取具体项时仍要求其完整内容边界及已有消费出口，不能用节点 Grant 将元数据边界升级为内容边界。
- 绑定当前 Tenant、Engine、完整类型化节点路径及显式 `recursive`。非递归只匹配直属 DataItem，递归匹配全部后代；比较结构化段的 kind、term、name，不能用字符串前缀、显示名称或客户端自报祖先判断归属。Meta 数字 node ID 只是定位引用，不作为稳定许可身份。
- 有效期内当前及以后新识别的合法 DataItem 自动进入范围，不逐项生成 Grant，不因本次扫描清单变化失效；范围外、尚未识别或组成不可靠的目标仍拒绝。node 不变为 DataItem，也不变为 whole 容器。每次真实读取仍核验当前 DataItem 和全部依赖；跨范围引用不能继承节点许可。
- System 经统一 Meta Client 独立核对当前节点存在、租户、引擎及规范路径；仅给 System Runtime 增加最小只读节点身份事实机器权限，不向人员补权，不读取内容、样本、统计或完整属性。节点缺失、事实不符或依赖不可用时不签发，不猜测父路径或回退原生 ACL。
- 节点候选树只消费 Meta 最小直接子节点身份分页：沿同一 `meta.node_identity.read`，固定 System Runtime、当前 Tenant／Engine，PG server 只返回 schema 节点，schema 不返回数据项；NFS 只返回普通直接子目录身份；MinIO 服务根返回 bucket，bucket／prefix 返回普通直接子 prefix。whole 容器及其内部节点不作节点授权候选。System 以既有授权读取功能与引擎管理范围保护网页查询，并在跨服务读取前后重核；按层浏览不枚举叶子、不回退原生目录，空节点也可独立存在。导航事实不是签发依据，正式节点 Grant 仍重新精确核验。
- 节点授权范围不可原地扩大或改变递归／动作／接收主体，须撤销原授权后以新命令办理。原命令重放不复活、不续期；撤销／到期在下一次正式校验生效。节点 Grant 可与覆盖范围重叠但目标不同的其他 Grant 独立存在，其有效期限按当前来源并集计算。
- Catalog 继续按其已绑定的精确 DataItem 业务批准办理；System 的节点直接授权仍须相同 Grant 创建功能和本引擎管理范围，不能用引擎管理范围本身代替数据读取许可。此次不新增 Catalog 节点业务批准实体。

公开入口及字段见 [System Swagger](../../system/backend/docs/swagger.yaml)。尚未识别的数据须先完成 5.5.7 的基础识别，不能把实时树选择本身当作 DataItem 授权依据。

#### 5.5.7 DataItem 授权的统一目标契约

**2026-10-29 已确认、实现中：FileGDB whole 接入。** NFS 上已由基础识别确认的 FileGDB 以 `.gdb` 根目录对应的整个 DataItem 授权，包含该容器当前及以后新增的全部子表／要素类；不按内部系统文件或子表另签 Grant。边界固定 Tenant、Engine、DataItem、规范根范围及 FileGDB 格式规则，合法内部新增不改变摘要；换根目录、改认格式、引入范围外资源不能继承。沿统一 Grant、功能权限与 Security 校验，不以元数据父项可见推导内容可读。新增授权的搜索与树统一消费 Meta 身份清单，未知或尚未支持的格式显示相应事实状态，不枚举物理碎片作为授权候选。本节其他已验范围不因此扩大，完整签发与真实读取验收另行记证。

`whole` 的最小事实与签发绑定使用 `scope: {format: "filegdb", rule_version: 1, root: EngineCatalogPath}`；root 是精确的 `.gdb` 原生目录路径，DataItem 的 `item_type` 仍为 `file`。内容边界的 scope、native_target、file_composition 以及元数据专用 metadata_target 四种表示严格互斥；scope 不列举内部文件或子表，也不接受范围外 refs。摘要只绑定身份、根范围和格式规则，首次接入只准入 NFS FileGDB。System 保留同一 Grant 存储与裁决，拒绝／撤销／到期均复用现有规则。Manager 容器父项只显示 Meta 索引；读取子表前必须以当前用户向 System 校验整项范围，内部工作流读取不能凭服务身份跳过该校验。

**唯一目标与分工。** Meta 保存 DataItem 的身份、布局、入口、组成边界和识别状态；System 保存精确数据项或节点范围引用、数据项授权认可的最小边界绑定、Grant 与审计；Catalog 是可选业务批准入口；实际读取 owner 执行统一规则。System 不跨 schema 查 Meta，不复制完整目录或 whole 叶子清单，也不重新探测格式。

Meta 向 System 精确投影当前数据项身份、可执行组成边界与基础识别状态；唯一事实契约见[授权资源描述契约](addp元数据扫描机制规范.md#授权资源描述契约)。System 绑定被批准的最小边界及其摘要，真实读取使用同一边界表示；状态完整与摘要一致均不能代替当前权限及 Security 校验。当前已验证目标包括 PostgreSQL 的 server → schema → 普通 table `single`，以及下述单体模型规则限定的 FTP／NFS／MinIO 文件／对象 `single`；事实查询、批准绑定、Manager 真实读取与收权闭环按引擎分别验收。共享事实协议及 System 持久化约束也能表达 server → database → 普通 table（MySQL）和 server → database → collection（MongoDB），但这不代表对应引擎的实际读取入口已执行源规则，不将其列为已完成的数据授权接入。不同目标仍须分别验证，不能仅凭相同名称跨类型或跨 namespace 复用批准。除本节明确的 FileGDB whole 实施范围外，未明确列入单体模型规则的文件 single、multi／whole 及其他消费入口尚未覆盖，不直接复用完整 Meta 详情、Catalog 变化源或浏览器自报描述。

**精确与节点范围 Grant 的匹配语义。** 精确 Grant 绑定被批准的 DataItem 身份及边界摘要，布局、入口或合法组成约束变化后旧精确 Grant 不覆盖新边界，须重新批准。节点 Grant 绑定持续逻辑范围，当前合法 DataItem 的完整边界均在范围内时可由节点来源覆盖，不要求管理员逐项冻结后续新项的摘要。两类规则都必须先核验当前真实身份与完整边界；缺失事实、越界依赖或改换 Engine 不能仅凭旧路径或指纹允许。收权只终止选定来源，不影响仍有效的其他来源。

原生读取计划反查 `single` 数据项时，可以按既有指纹规则定位候选 Meta 记录，但候选指纹本身不是归属证明。必须通过统一 Meta Client 读取持久事实，确认识别完整且类型化边界与实际读取目标完全相等；名称拼接碰撞、事实缺失或边界不符均拒绝，不回退到原生目标 ACL。跨模块事实读取在 IAM 事务之外完成，随后在同一数据库快照中重新核验真实凭据、操作资格及当前规则。

**模型的精确文件组成表示。** 单体模型保留 `single`，glTF 保留既有 `multi` DataItem，身份均取自主文件，不把 buffer 或贴图另建授权对象。共享 `AuthorizationBoundary` 的原生表／集合仍仅允许 `layout=single + native_target`；文件组成使用 `file_composition`；两者与 FileGDB 的 `scope` 严格互斥。已准入模型中的 multi 为 `model_3d/gltf`，主角色为 `manifest`，相关角色为 `buffer`／`image` 及唯一编号；不开放任意 multi 或 whole。批准快照中的固定格式身份唯一确定布局（glTF 与下述 ENVI 为 multi，其余已准入模型为 single），重建边界时必须与 Meta 的真实布局相符，不能按文件数量猜测布局；摘要继续包含真实 layout，既有格式的规范字节与摘要不变。`FileCompositionBoundary` 仅含 `format` 与完整 `refs`；每条 `FileCompositionRef` 含规范 `target: EngineCatalogPath`、`role`、`required`、`primary`，恰有一个必需主文件。主文件与所有引用属于同一 Engine、相同文件／对象路径模型，引用只在主文件父范围内逐一精确核对，不获得父目录读取许可。路径保留名称与大小写，禁止穿越、外部 URI、矛盾重复和目录目标。共享工厂深拷贝并按类型化路径排序、合并完全相同的引用；可信 wire 必须已经规范、完整且唯一，至多 200 个明确文件。空数组不代表组成已确认。文件摘要包括格式、完整路径、角色及必需／主入口语义，排除大小、内容哈希与时间；同一路径内容更新不使摘要改变，组成变化后旧精确 Grant 不覆盖新边界；节点来源另按当前完整边界及范围归属核验。原生边界的规范序列化与摘要不因新增文件表示而变化。纯值类型和摘要不是 Grant、EA 或允许读取的回执；文件事实投影、System 持久化与裁决、执行授权及真实内容 I/O 接线须分别验收，不以共享类型已编译宣称文件权限链完成。事实完整性来源及单体模型规则见[授权资源描述契约](addp元数据扫描机制规范.md#授权资源描述契约)。

**ENVI 栅格扩展（2026-11-02 批准，实施及验收单独登记）。** NFS / MinIO 的 `media/envi/multi` 可沿同一精确文件组成契约扩展准入；仅同目录的一份主数据（`raster`）和一份必需头文件（`header`）。不得借用 Model3D 执行目的、模型白名单或主角色。Meta 完整证明及 System 当前组成裁决是必要前提；批准绑定两份规范文件清单，变化重新批准。旧的单文件或模型批准不覆盖 ENVI；精确项批准不隐含父目录／prefix 许可，显式节点 Grant 则沿本节持续范围规则裁决。Manager COG 源读取及当前普通文件出口须消费同一组成；不能只增加资源选择器选项便宣称接通。原有格式的布局、主角色和摘要不改变；SKP 继续关闭。

ENVI COG 复用唯一 `ManagerFileReadScope`，由固定格式身份确定独立 `raster_cog_generation` 目的。执行配置 `raster-cog-file-config/v1` 冻结 owner task ID／版本、Engine 版本、完整组成及 target／raster／cog／result；当前 User 的手动根执行签发，Manager Runtime 逐次消费同一 EA。模型与 COG 目的不得互换，沿用现有 `manager.derived_artifact.create` 与 `manager.data_item.read`，不新增 Permission 或角色绑定。System 前向迁移同时扩展完整文件组成 SQL 约束和精确 pending 触发器，不回填旧 Grant。普通原件及 ready COG 均须核对当前普通用户、当前完整组成与 Engine 版本；终态 EA 或资源票据不替代当前源读取裁决。

Manager 在 Business 源上只按清单逐文件读取，再使用同一个精确文件暂存器送入 GeoPython。COG 结果 metadata 的有限 `source_inputs` 日志在 PUT 前记录两份精确 Infra 对象；暂存 namespace 为 `temp/raster-input/tenant_<id>/<execution>/attempt_<n>/<preparation>/`，Runtime 不获得 Business 凭据。成功和失败均清理本次文件并保留日志；结果未知的 PUT 保留 watch，不能以 execution 失败推定上传停止，不删除整个目录。物理清理复用受管成果的冻结、目标 hold 和一次性效果；未收敛 writer 拒绝清理，临时桶沿现有未版本化／temp 七日过期约束。缓存版本分别绑定 header 和 raster 的 SHA256，内容版本独立于批准的稳定组成摘要。

| 布局 | 授权范围 | 正常内容变化 | 不得自动继承的变化 |
| --- | --- | --- | --- |
| `single` | 一个原生表、文件或对象对应一个 DataItem | 增加行、同一入口内容更新 | 借单项许可读取其他对象，或改认更大组合 |
| `multi` | 主 ref 与明确相关 refs 组成一个 DataItem，例如 Shapefile | 原批准组成约束内的成员更新 | 用部分文件许可拼出整体，向范围外追加 refs |
| `whole` | 格式／引擎认定的整体 scope，例如 OSGB Scene、栅格镶嵌 | 既定合法范围内新增 OSGB、TIFF、瓦片 | 换 Engine／根范围、扩大组成规则、引入范围外资源 |

**文件批准快照与同次读取。** 文件组成沿现有不可变 Grant 记录保存 `file_composition` 最小批准快照，不另建文件 ACL 或纹理 Grant。直接请求绑定主文件对应 DataItem、预期边界摘要；完整快照只从 System 独立读取的 Meta 完整事实冻结，不接受浏览器提交替代事实。历史记录及撤销不要求 Meta 保留旧组成。当前读取按主文件定位候选，但必须证明其是同一完整数据项的主入口，再以完整组成计算边界并消费同一 Grant 规则；不能用某条纹理 ref 反推主项许可，也不能把单文件旧 Grant 升格成组合许可。受管模型内容请求的 `expected_source` 在同一次 System 裁决中比较当前完整组成及 Engine 版本；即使变化后的新边界另有 Grant，旧生成边界也不得获准。跨 Meta 读取在 IAM 事务外，随后重新核验原凭据、当前 Engine 及源规则，不跨 owner 持锁。此文件分支接入 FTP／NFS／MinIO IFC／3DS／DAE／OBJ 的直接批准与内容读取，本轮按同一契约扩展 STL；Catalog 文件办理和文件任务产出初始授权分别后续接入，不因共享目标已能表达便默认为已支持。原生表／集合的已验证边界摘要保持不变。

System 正式授权页面复用现有共享资源选择器、事实观察与 Grant 命令。未接入持续节点 Grant 的 FTP 目录仅用于选择当前已识别 DataItem；NFS 普通目录及 MinIO bucket／普通 prefix 的显式节点 Grant 按持续范围规则办理。精确模型项只可选择 FTP／NFS IFC／3DS／DAE／OBJ／STL／自包含 GLB／静态 glTF／Binary 7400 静态无贴图 FBX 主文件及 MinIO 对应格式的主对象候选，仍须由正式事实接口证明其为完整 DataItem（既有模型 single、glTF multi）的唯一主入口。FBX 只准入严格结构证明的单主文件，不能通过附加贴图 ref 扩大 profile；SKP 在严格组成读取准入通过前仍不开放。确认框展示 Meta 返回的整个组成清单及接收方、期限；不允许选中独立纹理后借用模型许可，不向浏览器开放自报组成。文件分支仅办理直接批准，不能在页面选择尚未接入的 Catalog 文件办理；PG 普通表及 schema 展开语义不变。前端接线、本地确定性门禁和真实运行验收分别记录，不能以页面可选代替实际授权生效。

**glTF multi 首期接线。** Meta 基础扫描完整读取 manifest 后确认精确组成及 `composition_complete`，仅从本次已观察范围匹配声明依赖。System 正式批准、源读取、Manager 执行授权及三个原始内容出口消费同一 multi 边界；Manager 先对已获权的 manifest 字节重核声明，再读取依赖，暂存同一份已核验主文件，保留目录与文件名，通过既有 `gltf_to_glb` 生成自包含成果。原文件下载 ZIP 仅含 manifest 与全部确认依赖，不能递归源目录。组成变化、依赖缺失、越界／可观察链接、远程 URI、未知扩展及不完整事实均拒绝；NFS 沿既有逐级无链接读取约束，FTP 沿账号根与严格 MLST 事实，不新增识别服务端已解引用链接的承诺。内嵌资源只校验、不虚造文件授权项。格式支持 profile 与预算归格式规范，运行通过情况独立登记，不以规范准入代表验收已通过。

组成的规范 JSON 本身至多 64 KiB，完整授权事实／批准 Binding 信封至多 128 KiB，后者给主项身份与摘要字段留出空间。工厂、严格 wire 和统一 Meta Client 使用同一预算，不出现“工厂接受、加信封后读不回”的合法边界；超预算拒绝，不截断 refs 或静默减少授权组成。

`scope` 是 whole 的组成范围，不是新数据类型。普通目录／schema 是 node，节点授权不会使它变成 whole。未来新增独立 DataItem 可由有效节点范围覆盖，但不继承兄弟 DataItem 的精确许可。whole 授权不逐文件签发，也不因合法新增成员要求 System 保存清单或枚举百万文件。

读取端必须约束真实路径、动态成员及关联引用，不能仅凭字符串前缀放行路径穿越、越界链接或外部引用。一个 DataItem 的 Grant 覆盖其合法组成，不再要求组成文件各有 Grant；真正依赖多个独立 DataItem 时必须全部获权，不能静默删去无权部分后宣称完整预览。

**基础识别与新鲜度。** 注册后的立即 basic 复用 Meta 正式扫描编排；注册成功不等于扫描成功。授权要求目标身份与组成可靠，不要求全引擎扫描、deep 或企业编目。完整清单足以确定的组合在 basic 完成；必要的有限 manifest／header 读取也属于基础识别，样本、正文、画像与统计留给 deep。

缺少基础事实、组成不完整或有证据表明边界变化时，须先补扫并明确状态，再签发。扫描限定真实授权范围，不以单叶子请求暗中枚举父目录；普通 GET、树展开和内容读取不暗中扫描。基础识别使用独立、限范围的扫描资格，避免“尚无 DataItem 却要求先有其 Grant”的循环；不能借机器身份读取样本，Worker 重核当前资格。

新鲜度针对授权边界，不是全部元数据。行数、大小、扫描时刻、内容版本或 manifest 字节变化不直接等于范围扩大；whole 内合法增长无需重批或全量重扫。边界未知则先核验，不以相同指纹或缓存描述证明范围未变。具体扫描规则见[元数据扫描机制规范](addp元数据扫描机制规范.md)及[探测器规范](addp数据项探测器规范.md)。

**统一读取链。** 可信会话及操作资格 → Meta 数据项与边界 → Provider／内容链真实读取计划 → 依赖归属与 System 当前规则 → Security 及执行约束 → 同一计划实际读取。核验对象与执行对象必须一致，并通过并发测试证明；原生 SQL、文件 URL、分页、下载及动态瓦片入口同样接入，不保留 native 独立 ACL 或无 Meta 兜底。

Meta 是数据授权的事实依赖，不是 System 登录、IAM、引擎登记的 Ready 前提；无法可靠核验时拒绝对应新增授权／读取，但不阻止已有历史查询或合法收权。缺少识别、边界待刷新、源不可用、无许可及依赖失败分别说明，不统称权限不足。

列／行授权后续在同一 DataItem 上表达字段和记录约束，包括属性及空间条件；须约束真实查询和内容出口，不能只过滤前端。条件模型、组合语义、边界版本与各引擎支持另行明确，不预建无人消费的实体或 DSL。

##### Meta 集中采集权限与范围（已确认）

**2026-11-01 已确认目标，实现切换中。** 已有逐任务批准退役改动及 PG basic 前置实现，不代表元数据查看过滤、basic／deep 统一准入、初始管理范围和即时刷新已经完成。PG 集中采集由“逐任务批准”改为“采集功能权限加引擎管理范围”。旧资格表由迁移删除，批准／撤销 API 和 System 采集资格页面已移除，不兼容旧批准作为新的 Allow；旧版 T4 仅证明旧版行为。采集管理、元数据查看和数据读取是三条独立权限边界。

- 首期集中计划限定 `scope.type=engine` 的 PG `ScanTask`，basic／deep 使用同一权限条件。创建、修改、手动执行、启停定时和取消分别检查对应功能 Permission，并由 System 核验当前操作者对目标引擎的管理范围。角色名称、连接可达或只有一个扫描 Permission 不能代替范围核验；配置扩大或换引擎须按修改后的完整目标重核。深度是成本和采集完整度配置；改变深度按普通配置修改核验，不增加批准步骤。
- 复用租户引擎管理资格、新注册引擎的有效初始管理范围与有效引擎管理委派，不新建采集批准角色或逐 DataItem 扫描 ACL；管理范围本身不提供功能 Permission。功能角色、管理委派和数据授权仍分别维护，不向现有人类账号自动补权。
- 有权人员配置的任务归租户。创建人、最后修改人和手动触发人的身份保留用于审计；创建人离职、失去角色或个人数据读取权，不自动停止已配置的定时任务。其后任何人员操作均核验当前操作者，不借原创建人的权限。该集中采集规则不扩展为 Transfer、Develop 等其他后台任务的授权依据。
- 本期只接受 Meta 人员手动触发和 Meta 自身定时触发；Orchestrator 通过 TaskProvider 触发已有 PG 扫描任务明确拒绝，不能借租户任务身份继承采集权限。编排主体的引擎管理范围另行设计。
- 定时执行沿 Meta Runtime 的固定服务身份、同 Tenant 的真实任务、引擎及冻结执行计划运行，不伪造人类 Actor，不保存 User Token，不因任意任务行或客户端自报范围就获得读取能力。System 仍拥有权限和引擎管理事实，Meta 拥有任务定义、调度及真实扫描范围，两者通过统一 client 协作。
- 当前有权管理员关闭定时后，不再产生新的定时执行；关闭定时不隐含取消已入队或运行中的执行。停止在途扫描使用显式取消。取消、任务删除、引擎不再可执行、租约失效、计划与当前定义不一致或裁决依赖不可用时，不再开始新的源枚举；失败不能伪装为空范围或完整扫描成功。
- **取消实现边界（2026-11-01 已确认）：** 已补充 PG basic 指定执行的取消 API、任务对话框入口和独立 `meta.scan_task.cancel`；通用 TaskProvider 仍声明 `supports_cancel=false`，不宣称支持所有任务取消。该权限由管理员通过既有角色管理显式分配，不自动加入现有人类角色；执行取消还须当前引擎管理范围。取消绑定一个确定的 PG basic 执行，不隐式关闭定时。首次尚未领取的执行在排除并发领取后复用 Common `CancelUnstarted`；运行中的取消请求由 Meta 持久保存，在下一次枚举前核验，真实运行结束后再写取消终态，不提前伪造已停止。迁移使用同一 Owner 停止机制，不冒用人类账号或以租约过期代替取消成功。
- PG basic 沿一次 `EngineCatalogProvider.ListChildren` 作为读取单元。已放行的在途枚举允许完成其内部 SQL，下一次枚举必须重核当前执行条件和取消状态；不承诺逐 SQL 中断。重试及接管不能沿用旧 Allow，任务配置变化不能重签旧执行或扩大实际计划。
- 集中 basic／deep 不要求操作者持有目标 DataItem 的元数据查看或数据读取 Grant；不绑定其个人／部门／项目组的源数据 Grant。扫描成功也不授予操作人、创建人或租户成员查看结果的权利。管理页面只返回职责内核对任务所必需的安全事实，不能借此枚举普通数据详情、样本或统计。
- 租户和范围统一管理计划，不按人员／部门／项目组复制。当前同一稳定范围只保留一个启用的 basic 定时计划；手动触发复用定义，不重复建立计划。节点独立调度、范围重叠与不同采集职责的协调仍遵循扫描规范，本期不因此开放 node 采集执行。
- **Manager 交互刷新（2026-11-03 已确认，PG／NFS）。** 移除引擎根节点及根详情的扫描入口，列表刷新只重读列表。普通 node 刷新须扫描执行功能权限，并将点击范围与当前 `metadata_read`／`read` 有效来源并集求交：递归 node 来源覆盖该范围当前及未来项；非递归仅直属 DataItem；精确 item 来源只刷新当前获权项；下级 node 与 item 可组成混合计划。范围必须在源读取前由 Meta 解析身份、System 裁决，不能先扫描父目录再过滤。无有效范围时拒绝，不创建空成功执行；引擎管理资格不作为 Manager 交互刷新的替代依据。
- 单项刷新及 node 局部刷新均沿 Meta 唯一扫描主路径，冻结本次允许的节点片段、递归语义及精确 DataItem 边界；Worker 重核当前来源，后续新授权不扩大冻结范围，撤权／到期后不再开始下一读取单元，已放行在途读取可结束。不得把无 SourceTaskID 的一次性执行作为免核验路径，不信任客户端 `source` 或自报祖先。可信成果补扫沿其原有独立证据，不借 Manager 刷新扩大范围。
- node 与精确项重叠时执行计划去重，但不改 Grant 来源。局部刷新不清理范围外兄弟、不把父 node 标记为完整扫描成功。whole FileGDB 等容器刷新只覆盖该 DataItem 的已证明组成，不上扩父目录。
- 刷新按钮的可用状态必须来自后端相同范围核验，前端不能作为最终授权。进度、错误及详情仅披露本次允许处理的范围，不返回隐藏项身份／路径或整个引擎总量；样本和内容统计仍按数据读取及 Security 另行裁决。basic／deep 权限条件相同，不因扫描成功新增任何数据 Grant。
- 保留引擎注册后的一次即时 basic 扫描，由 Console 编排并核验注册人的当前扫描功能与该引擎管理范围；不默认启用定时。可信生产成果补扫保留其精确回执边界，不借集中采集或用户刷新扩大访问。
- basic／deep 不设置不同人员扫描权限或内容 Grant 前提；System 仍核验本次采集依据，Meta 约束实际范围。现有 Security 保护约束、受保护数据的采集拒绝，以及禁止样本／正文／业务值进入普通目录、执行详情或日志的规则保留；深度统一不绕过保护。必须先补齐结果出口控制，再开放 PG deep 的新准入，不将 deep 失败降为 basic 成功。

**迁移与验收边界。** 实施必须一次替换旧逐任务批准主路径及对应页面、路由、Swagger、机器／人员 Permission、持久模型与自动化断言，保留通用审计和固定版本历史证据。用户已允许清理旧记录：旧采集批准／撤销／到期记录随专用资格表删除，不迁移为新许可，不保留兼容字段、读取分支或映射适配。保留既有通用审计和执行历史即可，不为恢复旧资格补建历史副本。新规则不读取旧批准记录，也不能按旧记录的 active 状态自动授予人员数据权限。存量 PG 集中采集任务保留配置和执行历史，统一关闭旧定时；仍按旧批准规则排队／运行的执行须经补齐后的 Meta Owner 取消链路终止并记录迁移原因，不再开始新源枚举；不得仅改写数据库终态而让真实运行体继续。已放行枚举仍可结束。由当前有权人员按新规则显式重新开启，不自动恢复。任务 1 保留且继续关闭定时，不自动执行。本次规范不触发任务、不更改开发环境的角色、委派或数据 Grant。

实现完成后分别验证：功能权限或引擎管理范围缺失时拒绝；有权人员无需 DataItem Grant 即可配置／执行 PG basic 和 deep；越租户、无管理范围的引擎或未经证明的 selector 拒绝；重复执行不重新审批；创建人失权不停止原定时计划且不能再操作；注册即扫不授予内容读取、初始管理范围撤销及成员关系结束不恢复；Manager 元数据获权用户的精确刷新与结果隔离；关闭定时与显式取消的不同语义；计划变化、租约、重试及接管的源读取控制；失败终态与清理。原逐任务批准／到期／撤销测试应替换为新规则反例，不能只删除断言。历史证据仍见[原采集资格验收导读](testing/evidence/数据保护与执行血缘历史证据.md#meta-采集资格验收导读)，不计为本次新规则已验。


#### 上传新数据初始使用权

2026-11-03 已确认的 NFS 上传回执、限定识别、初始签发与后台续办主路径已落地。2026-11-09，NFS 上传新项的 Hosted 跨服务验收通过，覆盖新建、覆盖、撤销重放、子扫描唯一性及失败清理。验收方法和证据见[上传新项初始授权场景](testing/scenarios/授权与共享.md#nfs-上传新项初始授权)。正式上传接口没有追加操作。

本轮仅接入 NFS 文件上传，PG 仍按其既有任务产出初始授权与本轮范围刷新规则执行；不扩展其他引擎。上传者对本次真正新建、已成功写入并由 Meta 确认身份和完整边界的 DataItem，自动取得初始元数据查看权；NFS 已接入正式内容读取边界的格式同时取得初始只读权（已包含元数据查看），只生成所需的一条 Grant。CSV、普通文档等尚未接入内容读取边界的格式，本期只签发 `metadata_read`，显示“元数据可见，内容读取尚未接入”，不扩大本轮读取格式。初始 Grant 使用独立的上传来源，沿统一源规则存储、查询、撤销和审计，持续有效（`until_revoked`），成员关系失效及现有 Security 规则仍约束实际读取。不授予引擎管理、分享、再授权、写入或 DDL 权限。

- 机器接入仅使用限定上传回执：Manager 运行身份新增既有 `system.engine_access_initial_grant.execute`；新增 `manager.upload_receipt.read`，仅供 Meta 和 System 运行身份反查本租户真实成功上传事实。Meta 沿既有固定 Manager 扫描机器入口核验精确来源，System 独立核对来源、新建、完整边界和历史；不向人员账号分配机器权限、不接受自报接收人、不复用 Transfer 的固定 owner 身份。格式后续接入不静默把已签发的元数据 Grant 升级为内容 Grant。
- 自动识别只绑定可信上传者、Tenant、Engine 及版本、同一次上传、精确写出对象和成功提交事实；不接受请求自报的“新建”标记。原生对象之前不存在的一次查询不独立证明实际新建，须由实际创建／写入路径给出结果。并发撞名不能通过覆盖抢先出现的目标来取得初始授权。
- Manager 在写入前持久化完整目标和已验证人员来源；只有 Provider 同次创建／写入／提交成功的对象才进入不可变成功回执，部分失败不冒充整批成功。Meta 限定识别记录原目标是否已有逻辑项以及本次实际形成的项；物理 created 与 Meta 的独立新项事实必须同时成立，且必要组成全部来自本次成功新建对象。不能仅因扫描时间较晚或新插入 Meta 行而增权。读取尚未接入的格式沿相同事实核验，只改变签发动作。
- 覆盖、追加、同名重建已有逻辑数据项不新增权限；物理新文件也不能将既有 DataItem 容器或组合冒充新项。Meta 新增行、重扫、重试不构成首次新建证据。既有授权撤销／到期不因再次上传恢复。
- 组合数据项须核对完整边界及各组成来源；上传少数成员不自动取得既有 whole 容器或其他已有依赖的读取权。未知身份或组成不签发初始 Grant；身份和元数据边界可靠、但尚未接入可执行读取边界时，只签发元数据查看 Grant，明确显示识别／授权状态，不伪装内容可读。
- 自动识别是上传流程的限定效果，不要求上传者先有新文件的 DataItem Grant；其可重试范围不得超过已成功上传的合法组成。形成可靠身份后由 System 正式签发初始授权；扫描成功不能代替签发。只读 Grant 已包含元数据查看，不额外生成永久不可撤销的“作者权限”。
- 上传成功、识别处理中／失败、初始授权处理中／已签发／拒绝分别呈现。异步阶段出错保留文件及原关联，不向浏览器返回会自动重放上传的认证错误，不重新写入源文件。失败清理只回收本轮临时文件、租约与验收夹具，不因服务故障撤销正式业务 Grant。

#### 5.5.8 任务产出数据与初始使用权

**平台统一办理规则（2026-11-12 已确认，待统一实现）。** 本节适用于所有实际生成持久业务 DataItem 的生产 owner，不按算子、模块、引擎或文件格式逐次重新决定是否办理。同一 owner 的新算子复用已接入的生产协议；新增 owner 通过一次正式能力登记绑定固定 Service Client、最小成果回执路由、执行资格及证据适配，禁止请求自报 owner 资格或任意回执 URL。机器 Permission、默认运行角色绑定、具体主体分配与正式 DataItem Grant 分别维护。共享协议、唯一签发路径和标准门禁共同落实本规则，不能仅增加文档或仅给单个算子补权。

办理单元是本次实际提交的一份独立成果。无论父任务最终成功、失败、取消或超时，只要其中一份成果已真实提交，就须保留原关联并继续办理该成果的限定识别和初始授权核验；是否签发仍取决于本节的真实新建、完整边界、来源、当前资格、期限及历史条件。部分文件、未提交事务、组成不完整或无法证明提交的结果不冒充独立成果。任务终态、成果提交、Meta 识别与 System 签发分别表达；不能以父任务未成功拒绝读取已提交成果回执，也不能因部分成果办理而把整体任务改为成功。原租约失效后的迟到物理事实只沿原 operation 核清，不允许旧 attempt 补写公开成功或重新派发写入。

生产 owner 持久化并证明原用户来源、execution／attempt／operation、稳定成果编号、精确目标与实际提交效果、完整组成及实际必要来源；Meta 唯一确认 DataItem 身份、完整边界和逻辑项事实；System 唯一裁决期限、历史及正式 Grant。多成果分别幂等并记录办理结果，不以一次 execution 只能有一个 Grant 为通用约束。暂时故障只续办原成果的扫描／授权，不重算、不重写、不换 execution／operation／成果编号；已签发、拒绝、撤销和到期历史不因协议统一而重开或恢复。临时导出、私有缓存和执行内部结果继续按其 owner 的结果规则处理。

实施须同步替换固定 Transfer／PG 的活动分支及调用方，通过 System 前向迁移保留既有历史，不建立第二套 ACL 或长期双轨入口。标准验证须覆盖跨 owner／Tenant 伪造、部分成功、原任务失败后的续办、多成果去重、完整组成、原用户与来源期限、撤权重放及响应丢失；权限目录、Swagger、Make／CI 登记与实现同批交付。现有 PG Transfer 与 NFS 上传的分版本验收不代表本统一规则已实现或已验收。

本节为已确认的设计原则。当前 PG 的 System 初始签发与终态核清、Transfer 后台续办、成功产出限定基础补扫及独立状态反馈已实现，并通过对应 T1／PostgreSQL T2；PG 普通表新建成果的真实端到端 T4 已验证唯一 basic 补扫、唯一初始 Grant、Manager 实际读取、旁观者拒绝、源撤权后成果独立可读及成果撤权后拒绝，见[验收证据索引](testing/验收证据索引.md)。该结论不外推 Develop、其他引擎、multi／whole 或未知来源成果。任务实际新建业务成果后，默认向可信发起账号签发成果的初始只读 Grant，无需逐任务配置或预先审批产出规则。读取仍须等待实际签发，并按统一源数据授权链校验；任务发起、写入成功、血缘登记或 Meta 扫描成功本身不代替 Grant。

| 结果形态 | 授权处理原则 |
| --- | --- |
| 执行内部结果、临时导出 | 按实际 owner 的结果读取规则控制，不伪造 Meta DataItem，不把执行归属或临时下载许可转成业务源 Grant |
| 新建独立业务 DataItem | 实际新建且经 Meta 确认身份与边界后，默认向可信发起账号签发初始只读 Grant；不要求发起账号具有授权管理权限，尚未签发不得视为可读 |
| 覆盖／追加已有 DataItem | 继续适用该数据项原有规则，不因本次写入新增发起人特权；授权边界改变仍按 5.5.7 处理 |
| whole 合法范围内增加成员 | 不当作多个独立新项反复授权；既有 Grant 只覆盖原先批准的合法组成范围 |

初始使用权复用普通源数据 Grant，只授予发起账号对实际新成果的读取权，不授予管理、分享、再授权、写入或 DDL 权限，也不替代消费模块功能权限与 Security。默认授予只适用于本次实际新建的成果；覆盖、追加或同名重建已有数据项不能使用本条增权。后续向其他账号、部门或项目组授权，仍须有效业务批准或直接办理资格。

初始 Grant 使用独立的“任务产出初始授权”来源，和直接批准、Catalog 业务批准区分。它仍写入同一源数据 Grant 存储，统一查询、实际读取与撤销，不建立第二套作者 ACL；不伪造人工批准、Catalog 业务确认，也不新增人工审批步骤。任务发起账号是接收方与执行来源，不因此成为授权管理员；机器签发者与历史人类发起来源必须分别留痕。

初始期限在首次正式签发时根据本次实际读取的完整必要来源确定：每个来源合并发起账号当时有效的个人、部门和项目组、精确及节点范围 Grant，取可连续读取的最晚截止时间，并受适用成员资格期限约束；多个必要来源取上述期限的最早值。全部来源均长期有效时，成果使用 `until_revoked`，否则使用最早绝对截止时间及 `at_time`。无许可、边界未知或期限无法可靠核验时，不自动签发，不把未知当作长期；期限已过不顺延。期限由 System 统一裁决，不能由模块自报，重试不重新起算。

**无业务读取来源与无人员来源（2026-11-12 已确认）。** 有可信人员来源，且实际执行链能够证明没有读取已有业务数据的生成（包括无业务读取来源的空表创建），向该人员办理 `until_revoked` 的初始只读 Grant；仍须核验当前人员资格、创建／写入功能、真实新建提交、Meta 完整身份、历史及 Security，不产生永久作者特权。证明须明确区分“已证明无业务读取来源”和“实际来源未知”；空集合、null、缺失证据或从配置推断均不能证明无来源。上传、私有导入及外部输入按各自真实来源契约核验，不因没有业务 ReadSet 就归为无来源生成。

发起账号取自任务 owner 已验证并持久化的执行来源事实，不能信任请求自报账号，也不能把实际运行的 Service Principal 当成人类发起账号。定时执行沿用已验证的任务授权主体并在每次执行固定来源；执行人、任务维护人和成果接收人不能互相猜测或替换。确无可信人员来源的平台／服务任务不自动推导个人或全租户读取权；保留可核验的成果及限定识别状态，后续沿既有正式授权流程办理，不以服务身份替代接收人。用户配置的定时任务不因后台触发而归入无人员来源类别。

任务 owner 证明真实执行来源及实际新建成果，Meta 通过基础识别证明 DataItem 身份、完整性与授权边界，System 按本条统一原则核验并通过唯一源规则写路径签发、去重及审计。所有产出模块经共享 client 接入同一契约，不在 System 预先逐项登记未来成果，不在各模块保存第二套“作者可读”ACL，也不用用户 Token 驱动后台授予。覆盖、同名重建、重扫或重试不能冒充首次产出，不能恢复已撤销、到期或边界失配的 Grant。

完整必要来源只接受 Provider 同一实际读取会话的 `ReadSessionEvidence`，不从任务配置、目标表、血缘摘要或执行后目录补造。PG Transfer 在同 attempt、同目标的成功提交事实中冻结规范 ReadSet（无法证明时为 null）；不保存或推导许可期限，证明失败也不触发重新读取、重跑计算或修改原写入策略。共享机器回执只投影这一不可变事实；旧版本私有事实不补填来源，空集合或 null 不能当作“所有来源长期有效”。

**Manager 私有 Shapefile 导入。** 首期只覆盖由 Manager 上传者本人在同一 Tenant 发起的 bounded Transfer 导入，且同次首次执行实际新建并提交一个 PostgreSQL 表。暂存对象属于 Manager 私有上传，既不是 Meta DataItem，也不进入 PG 源 Grant 的 ReadSet。Manager 在完整写入后记录上传编号、租户、上传者、一次绑定的 Transfer 任务以及各组成对象的 SHA-256。Transfer 沿已有私有来源核验入口取得这份清单，在本次实际读取中验证每个组成，并将上传身份与核验后的清单摘要随同一执行租约的成功提交冻结。不能从路径、主文件、任务配置或旧回执补造证明。

System 从原 Transfer 产出回执核验上传者和执行人一致、来源证明完整、首次新建提交和 Meta 目标边界，按原签发事务复核当前成员、执行权限与目标历史，向该用户签发一条精确目标表的 `read` Grant，期限为 `until_revoked`。实际读取继续受成员资格、功能权限和 Security 约束。暂存清理不改变成果 Grant。已有目标、来源不明、组成不匹配、无有效成员资格或已有目标授权历史不自动签发。其他来源仍按其实际 ReadSet 和既有期限规则裁决。既有执行的拒绝终态不重开。

**实际新建的证明要求**：新建是本次业务产出的执行事实，不是扫描发现时间、Meta 新增行或一次 `CREATE TABLE` 的同义词。授权接入不得把 Transfer 已有的 replace 删表重建改成保表清空；写入策略与初始授权资格分别处理。

| 事实 | 权威及要求 |
| --- | --- |
| 执行来源 | 任务 owner 已验证、持久化的 Tenant、执行编号、有效 attempt／lease 及完整人类发起主体；不接受前端自报或仅用 `triggered_by` 推导 |
| 写入前目标与实际操作 | 引擎 Provider 的真实执行证据，须区分首次新建、覆盖／复用已有目标和无法证明；删除前的已有目标事实不能被后续建表、扫描或重试覆盖 |
| 成果提交 | 同一次实际写入的成功提交事实；行数、进度、目标 locator、会话打开成功或生成提交标记的接口存在，本身均不足以证明首次新建 |
| 数据项身份与边界 | Meta 基础识别结果与本次实际提交目标一致，且完整性和授权边界可核验；不是以扫描结果代替写入事实 |
| 存量与签发状态 | System 核查同一逻辑目标的已有 Grant／撤销历史及原办理结果；不以物理表重建、新 Meta 行或新请求编号恢复旧许可 |

写入前“目标不存在”的独立查询只能作为观察，不能单独证明后续创建归属于本执行。Provider 必须证明观察与实际创建之间没有被并发目标替换、复用或删除重建冒充；本进程锁或仅约束 ADDP 调用者的协作锁不能当作外部 DDL 的排他保证。尚不能给出充分证据时，只是不自动签发，不据此改变原写入策略、重跑任务或把未知默认成新建。

PG 已确认的并发边界：观察为缺表时，跳过删除并用不带 `IF NOT EXISTS` 的原子建表语句创建；并发抢先出现同名目标即报冲突，不删除或复用该目标。观察为已有目标时，replace 继续删表重建并始终分类为覆盖，不因新的物理表产生初始授权资格。准备结果只证明该阶段的实际操作，不代替数据提交、历史核查或最终授权。

PG 首期的运行前提是任务期间不对目标执行 schema 改名、迁移等结构运维；不为此增加父 schema 锁，也不承诺抵御任意外部并发 DDL。Provider 在实际写入事务开始及提交前核对精确目标路径与本次创建事实；已观察到的路径或归属变化不得产生成功新建证据。最终核对与提交之间不提供额外的跨目录原子保证，因此该检查不能替代上述运维前提。该范围不放宽严格 CREATE 撞名、已有目标不增权、失败／断连／未知提交不自动授权的要求。

产出证据按原执行及实际目标绑定，由 owner 在有效执行租约约束下持久化；多目标逐项记录，不能用一个成功目标证明其他目标。恢复或重试必须保留原产出关联与已发生目标操作，不得从当前目标缺失、空表或新执行编号重新推导首次新建。提交结果未知先核清原写入，不能重发写入来补造证据；缺失证据不得事后根据当前 Meta 或任务配置补填。证据不是 Grant，也不新增物理实例作为授权身份。

PG 普通 Transfer 的持久化沿现有执行记录保存私有产出事实：在目标操作前写入 `pending`，记录真实执行、Tenant、attempt、重试来源和精确目标；Provider 准备结果落库后为 `prepared`，完整写入成功返回后才记为 `committed`。未达到后者均不证明成果已提交，进程崩溃、提交后反馈丢失或执行失败不自动解释为已回滚。已存在占位不能重新初始化或重放原目标操作；新重试保留 `retry_of_execution_id`，不能清洗为首次执行。重复准备／提交回执只核对并恢复同一事实，不延展时间、不更换目标或覆盖已冻结的准备结果。

上述写入均核验当前租约并串行更新同一执行；锁等待结束后仍须核验期限，迟到旧 attempt 不能补写。普通执行 metadata／终态更新不得覆盖私有产出事实；用户执行详情和 TaskProvider 稳定输出不暴露这些内部证据。不新增执行表、目标物理身份或兼容回填路径；本阶段仍不签发 Grant。

生产者证据使用独立的最小机器契约，不读取 owner 私有配置、metadata 或数据库。PG Transfer 的 `GET /api/v1/transfer/runtime/executions/{execution_id}/production-receipt` 仅允许当前 Tenant 的 `addp-system`、`addp-meta` Service 与不可委托、不可租户定制的 `transfer.production_receipt.read`，该权限仅授予 `tenant.system_runtime`、`tenant.meta_runtime`。共享 `TransferClient` 按精确执行 UUID 获取同 attempt 已提交的回执（不依赖父任务终态），返回实际目标、准备结果、提交时间、重试关联及可信人类来源（存在时）；缺失来源不以 `triggered_by` 或机器身份补造，未提交及不一致的证据不输出提交回执；父任务失败不抹除原有效租约下已持久化的提交事实。回执不是首次新建资格或 Grant，不包含凭据、连接、业务内容或来源权限期限，不改变用户执行 DTO／TaskProvider 输出。Meta 授权事实仍仅由 System／Catalog 的原入口读取，Transfer 不因此获得 Meta 授权事实查询权限。System 后续独立核对当前 Meta 边界及全部签发条件；期限只在首次正式签发时冻结。

System 的内部产出边界准备只接受可信 Tenant 与精确执行 UUID，以共享 client 取得上述回执，再复用现有 Meta 边界解析器核验完整 `single` 目标。候选指纹不是归属证明，原生名称不得拆分或仅按 `full_name` 匹配；返回的类型化路径须与实际提交目标完全相同。跨 owner 调用不持有 System 数据库事务，前后核对本租户 PG 引擎的生命周期及聚合版本；不完整、待刷新或依赖不可用时拒绝准备，不在读取中补扫。准备结果仅为内部当次证据，不是 Runtime 凭证或 Grant，不缓存为许可，不冻结期限，不推导缺失的人类来源，也不因 `created` 或准备成功省略正式签发时的历史、身份、ReadSet 及边界复核。

初始授权的内部来源准备进一步要求非重试的首次执行、实际 `created`、可信人类来源和非空完整 ReadSet；逐项核验来源的 PG 引擎版本及 Meta 边界。当前来源规则、执行所需 IAM 功能权限与目标既有批准／办理／Grant 历史在同一只读快照中观察，不因撤销、到期或更换边界而忽略目标历史；未知来源不进入规则查询。该观察不冻结期限、不签发 Grant，也不能替代正式写事务在统一锁顺序下的复核。首次执行与历史为空仍不能独立证明该执行真实新建并成功提交了成果。

**资格基准与物理对象边界。** 初始授权以“任务真实新建并成功提交该逻辑成果”的不可变执行事实为资格依据，不以签发瞬间仍保留原物理表为条件。Provider 须证明从实际创建到本次写入提交的成果归属；准备与写入会话分离时，中间的替换窗口仍须校验，不能仅补执行后的目录查询。Transfer 沿原执行持久化同一事实，创建、写入或提交未知时不得补造成功证据。原生对象标识仅用于执行内部补证，不写入 Grant 成为物理 ACL，不增加业务列或改写业务注释。

成功提交后不再持有成果表锁等待 Meta 识别或 System 签发，不引入跨库两阶段提交、补偿撤权、业务数据库台账或 DDL 触发器。原物理表随后被同路径重建时，只要当前仍是同一逻辑 DataItem、边界可核验且未变，既有成功产出事实仍可支持初始签发；这与 5.5.4 的逻辑资源授权语义一致，不表示对原物理内容永久保真。改名、换 Engine、来源重绑或边界变化不转移资格；源事实缺失、目标身份或边界未知时拒绝签发。

产出事实不是许可或已冻结的期限。首次正式签发仍须复核当前 DataItem 身份及边界、引擎绑定、来源授权及成员期限、执行功能权限和目标全部批准／办理／Grant／撤销历史；已有目标不能借新执行或物理重建冒充首次产出。重试只恢复同一办理结果，不能重新生成资格、延长期限或恢复已撤销权限。相关创建／提交断连反例、不可变回执及唯一签发事务须分别验证，不以普通表锁测试或准备成功替代自动授权闭环验收。

PG Transfer 的私有 `transfer.target-production/v5` 与共享 `transfer.production-receipt/v5` 显式携带 `created_and_committed` 及实际写入绑定的 `target_engine_version`。目标版本须从同次实际 Engine 解析响应取得并随准备事实冻结，签发前核对当前版本；不从签发时的当前引擎反填历史版本。该字段只由同一实际写会话的 Provider 证明产生，在有效执行租约下与首次提交时间及实际 ReadSet 一并冻结；仅 `committed + created` 可以为 true。false 表示未证明，不从准备状态、行数或普通提交标记补齐。重复记录必须匹配全部冻结事实，不允许事后将 false 提升为 true。旧版事实不转换、不补造；System 的初始来源准备必须要求 true，回执本身仍不是 Grant。

共享成果回执的 DataItem 来源同时冻结完整 ReadSet 与逐 Engine 的实际读取版本，按 Engine ID 严格升序唯一登记，覆盖集合必须恰好一致。同一 Engine 的多个必要读取必须版本一致。Transfer 从本次源连接解析响应向实际读取流水线传递版本，与同次提交事实一起冻结；Develop 从实际受限读取会话的已核对 Engine 响应取得版本，与原节点 ReadSet 和 Runtime manifest 同事务保存。不得从任务配置、当前 Engine、旧绑定范围或血缘反填历史读取版本。缺失版本的历史读取证明只表示证据不可用，不生成无来源资格。System 在边界准备、签发锁内和提交前逐项核验所有源与目标的冻结版本；引擎重绑或版本变化不转移原资格。

文件成果以完整 FileComposition 绑定写入现有 `task_production` Grant，和普通文件 Grant 共用读取、历史与撤销规则；内部成员不产生独立 ACL。仅受支持的原生 PG 与 NFS 文件路径进入本期签发；whole 或 metadata-only 不能借该路径取得 read。Develop 的限定识别和初始授权由现有 Supervisor 恢复周期续办，同一原 operation 的明确发布结果与待办原子保存；未知依赖先核清原 System 终态，再续办原引用，不重新计算、发布或换号。后续核清得到的已提交结果也按同一规则办理，父任务终态不改写。

物理产出、基础识别和授权签发不是跨模块原子事务。状态应分别反馈；产出或扫描成功不得显示成授权成功，授权失败不得自动重跑计算或删除已产出内容，结果未知先核清原办理，不换编号重复授予。只为授权确定边界时基础识别足够，不因本节给现有业务扫描增加 deep 前置要求。

**自动续办与终态（已确认）。** 物理任务成功后，Meta 基础识别尚未就绪、依赖不可用、网络断连或结果未知保留“任务成功、授权待处理”；后台以同一 Tenant、产出 owner、原执行 UUID 续办，并先核清原结果。暂时故障只重试授权续办，不重跑计算、不重发写入、不删除成果，也不更换原身份或命令。权限不足、目标已有批准／授权历史、非首次新建、证据矛盾或来源期限已过等明确拒绝属于终态；后台停止，不在权限或组织关系后来改变时自行恢复这一轮初始授权。

终态必须持久化并且不可改写：同一原执行的同一独立成果只能恢复原签发或原拒绝，进程重启、回调丢失及并发续办不能把拒绝变为批准，也不能重新计算已签发的期限或恢复撤权。成功结果引用唯一 Grant，仍按该 Grant 的统一读取与撤销链管理；拒绝结果只约束原命令，不阻止管理员以后经正常显式流程批准。未获得可信结果的 HTTP 错误、缺失字段、畸形回执及未分类错误不得伪装成已签发或明确业务拒绝。系统不得因为暂时故障构造新的批准来源、第二套 ACL 或用户 Token 后台通道。

Transfer 在原有效租约内首次持久化完整成果提交的同一事务中，为具备 v5 首次新建提交证明的 bounded 执行初始化 `initial_authorization` 待办；不回填没有该标记的存量成功执行。该 metadata 只保存投递状态、有限退避进度、封闭原因码及原 Grant 编号引用，不保存权限规则或期限，不作为实际读取依据。现有 Worker 在自身注册有效时独立领取待办，释放数据库锁后通过统一 client 先核清原终态、再续办签发；正常自动扫描中同一新建成果也使用统一成果识别入口，避免抢先登记造成资格冲突；后台只在收到正式 pending 响应时请求 Meta 精确补扫。没有可信人员的已提交成果同样保留限定识别续办，System 不据此生成个人 Grant；部分人员事实不伪装为无人员。暂时故障保留物理执行 success 与 pending，按 30 秒至 5 分钟有界退避；明确终态停止领取，迟到旧领取不能覆盖新结果。不得因授权待办而重新执行任务、修改目标、删除产出或回写私有产出证明。

System 的最小初始授权终态以 `Tenant + producer_owner + 原 execution UUID + 原 result UUID` 唯一定位，只有 `issued / denied`，不保存任务配置或第二份 ACL。`issued` 与唯一源 Grant、审计在同一事务提交，`denied` 只保存封闭原因码和裁决时间；暂时故障不生成终态。`task_production` 来源的 Grant 不消费人工直接许可；其原执行、机器签发者及历史人类发起者分别绑定。任务产出统一入口为 `GET/POST /runtime/initial_production_grants/{producer_owner}/{execution_id}/{result_id}`，不接受请求体或 query。仅服务组合中显式登记的 owner 对应 Tenant Service 和专用 `system.engine_access_initial_grant.execute` 可以续办自己的已提交成果；实际目标、账号、来源和期限均由 System 反查与裁决，不允许请求自报。该机器 Permission 不授给人类或租户自定义角色，也不是通用 Grant 创建权限。owner 的最小回执读取器、固定 Client 和执行 Permission 在服务组合时登记，不能通过请求自报或匹配服务名自动接入。单成果 Transfer 的原 result UUID 使用原 execution UUID；多成果沿各 owner 的原持久操作身份登记，不在续办时换号。数据库迁移仅将历史 Transfer 单成果映射到该确定键，保留原终态及 Grant 编号，不补签发或重开终态。

**成功产出的限定补扫（已确认）。** 首次扫描提交失败而原任务已经成功时，Meta 可用自身 Tenant Service 和统一 Transfer client 反查原执行的成功产出回执，仅为回执中的精确 PG 首次新建成果补做基础识别。请求不得自报目标、主体或创建成功事实，不得扩大到 schema／目录、兄弟表或任意历史任务。扫描归属沿可信回执保留真实人类来源和直接父执行，不复制用户 Token 或执行授权；跨 owner 取证在 Meta 数据库事务外。原手动产物扫描入口仍严格要求父任务处于运行中，不因这个补扫能力放开已结束任务。补扫只建立 Meta 技术事实，不代表授权签发成功。

限定补扫使用 `POST /api/v1/meta/runtime/productions/{producer_owner}/{execution_id}/{result_id}/basic-scan`，仅已注册 owner 的对应 Tenant Service 与现有 `meta.scan_task.execute`，只传原成果引用。System 以自身 Tenant Service 和既有 `meta.task_provider.read` 读取同路径 GET 的完整身份与新逻辑项事实；签发须同时核对原成果实际提交证明、Meta 本次新项事实及当前完整边界，不以只有物理新建或目标 Grant 历史为空替代逻辑新项证明。创建复用 Meta 同一范围解析、执行入队、Worker 和读取隔离；并发／响应丢失恢复同租户原扫描，不重复创建、不复活已经失败的扫描执行。已入队扫描失败时保留其原状态，按 Meta 既有显式恢复约束处理；不能把扫描失败伪装为 System 的终态授权拒绝。该能力是运行时软依赖，不新增 Meta→Transfer 的启动／Ready 前置条件。

物化成果的 Grant 独立管理：来源授权及成员关系只参与初始期限确定，成果签发后不因来源 Grant 撤销、续期或来源成员关系变化自动撤销、续期或缩短。成果仍受自身当前 Grant、功能权限及已有安全限制约束；本条不解除来源已经要求执行的安全限制，也不把血缘变成许可或第二套级联 ACL。实际读取来源的视图或实时查询继续逐项核验完整 ReadSet，不能冒充独立物化成果。

实现须沿统一产出证据契约按切片验证：真实新建与覆盖的区分、可信发起账号与服务身份不混淆、同源授权取最长及多源取最短、有限与长期混合及成员期限、无许可或拒绝不生成期限、接收方及期限不可扩大、基础识别失败拒绝签发、重复／迟到回执与撤销不复活、来源撤权不级联且不改已冻结期限、已有目标授权历史不绕过、签发失败不重跑计算，以及真实 PG 成果读取和收权。T1／T2 验证契约与持久化，T4 验证产出→识别→签发→读取→撤销；未运行不计通过。当前不扩展其他引擎。

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

Meta 既有 `resource.children.list`／`resource.ancestors.get` 工具的委托凭据，仅可转交 System 固定元数据可见性核验入口。委托必须绑定原用户、唯一 `meta` audience、恰好一个原工具 Scope 及原 Client／AgentRun／ToolCall，当前功能与 DataItem 规则仍须成立；该入口不授予内容、扫描或其他 System 操作权限。

Manager 检索统一使用 System 固定检索裁决，核验 `manager.search.execute` 与每个 DataItem 的当前 `metadata_read`、`read`。接受当前 Tenant 第一方或 OAuth User，以及绑定原 User、原 Client／AgentRun／ToolCall、唯一 `manager` audience 和唯一 `data.search` Scope 的委托。System 沿既有 Meta 可信身份及 Grant 裁决返回逐项结果，不授予可缓存的许可、通用数据读取、预览或扫描权限。Manager 页面与 Agent 工具使用相同的检索出口。仅有元数据查看权时，关键词可匹配安全的资源名称、逻辑路径和已登记的技术字段名；对应搜索属性白名单为 `name`、`full_name`、`fields.name`。响应只包含身份、定位和实际命中的字段名及其高亮，不返回未命中的完整结构，也不按字段类型、注释、表达式、样本或值匹配。字段名匹配仍由搜索服务给出实际高亮，不由调用方猜测命中。正文、内容摘要、内容向量命中和其他内容衍生字段要求当前读取权，并继续服从既有保护投影。授权筛选在融合排名、计数及分页之前完成。无法核验时本次检索整体失败。

### 8.1 集合检索的候选鉴权与续查

**状态与适用范围。** 2026-11-08 用户确认联合实现，首个落地点为 Manager 数据检索；代码与消费端已接入，本地T1-T3和隔离原生引擎门禁通过，服务未重启，真实身份T4未运行。以下规定替换“先枚举租户全部索引身份并鉴权，再按关键词检索”的设计，不先求全量可访问 DataItem，也不以授权范围全集查询作为主流程。本批同时替换入口契约及页面／Agent消费者，不保留兼容分页或双轨策略。单项预览、下载、扫描和其他模块的集合查询不因此改变权限。

**唯一职责。** System 核验当前用户／既有检索委托的功能资格、租户及有效组织关系，沿唯一 Grant 规则裁决候选；Meta 提供当前 DataItem 身份与边界；Manager 执行检索、候选补取、展示投影和分页；Security 保护出口保持不变。候选判断同时返回元数据可见与内容可读，不为同一事实分别读取两遍 Meta。不复制 ACL、组织关系或可缓存 Allow 到搜索索引，不跨读 owner 私表。登录及检索功能权限仍在检索入口核验，这不属于全量数据鉴权。

**召回先行，裁决随后。**

1. 先在可信当前 Tenant 及用户指定的可选 Engine 条件下检索，取得有界候选批次；不能用 GetDocuments 枚举整个租户作为搜索准备，也不能先进行全库逐项鉴权。数据量扩大但查询相关候选不变时，不应导致授权候选和 Meta 请求按全库数量线性增长；宽泛查询／极低授权命中率仍可能需要较多候选，不承诺任意查询固定时延。
2. 元数据关键词、内容关键词和内容向量分别保留真实命中来源。内部候选仅携带裁决与遍历必需的身份、投影版本、来源和排序依据；在授权前不向用户输出名称、路径、正文、摘要、高亮、分数、候选总数或剔除数量。Service 身份处理索引不代表用户获权。
3. 按 DataItem 身份合并本批重复候选，保留各检索来源的命中依据，再沿 System→Meta 唯一批量链核验。只对本次候选取事实；公共祖先／引擎事实可在这次操作内复用，不能复用跨请求 Allow。资源缺失／身份无法证明按既有逐项目标语义处理；依赖不可用、响应不完整或协议错误使本次请求明确失败，不能当成无权或无结果。
4. 仅有 metadata_read 的候选必须在 name、full_name 或 fields.name 上实际命中；不能将正文／向量命中降级成元数据结果。取得 read 的候选才可消费内容命中及对应展示字段，继续执行 Security。展示投影须与核验身份一致；索引陈旧不授予权限，也不承诺未进入索引的数据已经可搜。
5. 不足请求的结果数量时继续取后续候选。第一批全部无权不是结束依据。达到单次工作预算时保存可续查位置，不为凑满一页扩大原预算或无限循环；后续请求能够越过已处理批次继续查找。预算／窗口上限／超时均不等于候选耗尽。

**续查状态与不遗漏约束。**

- 页面采用顺序续查，不要求精确 total 或总页数，不跳到未经遍历的任意页。结果数量仅描述实际交付的条数；不得使用搜索引擎未授权命中估算数冒充用户总量。
- 内部区分“本批已返回结果且可继续”“本批不足／为空但仍可继续”“所有参与检索来源均已确认耗尽”。仅最后一种允许页面显示没有更多；空结果加有效续查位置应显示可继续查找，不宣称无匹配结果。
- 一批已取出的候选不能因页面凑满而整批跳过。续查位置必须保留尚未处理及尚未交付的剩余候选；跨检索来源按 DataItem 去重，不能丢弃另一路的真实匹配依据。源读取失败不能提交推进后的游标。请求重放的状态推进与响应丢失恢复语义必须在游标载体方案中证明，不能用提前消费位置导致跳项。
- 续查游标必须绑定原主体、Tenant、查询词、Engine条件、检索／模型版本及排序语义；不接受客户端自报候选身份、底层游标或范围。内容不可读且不可篡改，不包含用户Token、Grant许可或隐藏资源明文；采用服务端不透明引用或受保护封装的采用服务端有期限内存会话引用。
- 游标记录遍历位置，不记录未来 Allow。续查携带当前凭据并重新核验资格及准备交付候选；缓冲中的旧候选和旧高亮不能沿用旧许可。游标过期、身份／查询不匹配、底层位置不能可靠恢复时明确要求重新检索，不能静默从头重跑、伪称耗尽或切回旧页码API。
- “不漏后面的有权结果”限定为查询条件、权限和可遍历索引集合稳定，调用方继续续查直至耗尽的情况。期间新授予权限使已跳过候选变为可见、新索引项插入已遍历位置时，通过新检索获得，不承诺旧游标倒退补找；撤销／到期必须对后续准备输出的数据生效。索引变化若破坏遍历顺序保证，应明确失效而不是继续一个可能跳项的游标。

**排名与检索窗口。** 授权过滤先于用户可见的结果排名、数量和分页；不直接展示过滤前排名、隐藏项占位或其匹配详情。候选召回顺序与最终结果排名是两层，候选的内部位置不能当作授权结果序号。当前关键词 maxTotalHits 和向量 Top-K 不能作为整次续查的终点，否则窗口以后的有权结果永远无法出现。检索器须提供覆盖所声明查询集合的可续查路线，不得只提高硬上限或每次从头拉取更大的 Top-K 冒充解决。

现有实现采用全窗口对称 RRF，再对融合结果按页码截取；它不是已证明正确的增量分页算法。尚未查看的另一检索来源可能改变已见候选的最终融合名次，不能把“当前凑满一页”直接视为全局排名已经确定。实现前必须明确：保持原融合语义时如何证明可交付前缀及后续可遍历性；若需改为分批／分来源排序，先讨论用户可见排序影响。不能擅自关闭向量、只交付关键词，或把有限窗口排名宣传成全局排名。2026-11-08 用户进一步确认先保留相关性优先，核查现有关键词、向量是否支持补取和增量排序，明确后再实现；分批相关性排序未获采用，不作为默认或降级路径。

**现有检索器核查（2026-11-08，设计证据）。**

| 环节 | 已核对事实 | 设计结论与缺口 |
| --- | --- | --- |
| 关键词 | 当前使用 meilisearch-go v0.36.3，SearchRequest 提供 offset／limit，无 search-after 游标字段；Manager 读取 maxTotalHits 后一次取满窗口。本轮已核对开发容器与独占 T2 实例均为固定 Meilisearch v1.54.3；T2 将 maxTotalHits 设为100，offset=100 返回空，但夹具实际有260项匹配 | offset不能越过窗口。独占夹具中排除已消费ID可继续取完，但其过滤条件与状态随消费量线性增长，尚未选作产品路线；不能以小夹具通过宣称已具备有界、完整的续查游标 |
| 向量 | QueryReadySimilar 以当前 allowedItems ID全集过滤，再按 cosine distance、item_fingerprint 排序，LIMIT topK；服务默认topK=10，并再次截取。当前没有续查参数 | 可研究按距离＋稳定身份继续取下一批，并移除依赖全量Allow ID的前置条件；尚未实现或验证。距离阈值保持原定义，Top-K只能成为批量取数的控制量，不能作为全部候选结束依据 |
| 向量完整性与成本 | 本轮只读核对开发库 manager.embeddings 为主键／身份／状态／查询范围 B-tree，无HNSW／IVFFlat；开发库已安装pgvector 0.7.0，本轮标准T2实际使用0.8.0。T2临时夹具以3万行精确距离＋稳定ID续读，9870项阈值内结果与完整排序逐项一致；第1批与第11批EXPLAIN均扫描3万行并作top-N排序 | 已证明该夹具的精确keyset可表达性，同时确认它没有避免逐批重复距离扫描。该计划不是运行库原表的EXPLAIN，也不是生产规模性能基线；版本差异须在正式实施验证中覆盖。ANN扫描结束不能证明真实候选耗尽 |
| 融合 | 当前对已授权关键词序列及向量序列计算两路对称RRF，常数60，再排序分页 | 可以研究分数上下界，确认前缀后交付；凑满结果数本身不是排名证明。不保持旧硬窗口的截断集合，但保留RRF相关性目标；稳定同分顺序、来源完整性和权限变化须共同验证 |

增量融合的可行性推导：设关键词与向量两条**固定、完整、已授权且有序**的流分别已读到有效名次 p、q，RRF单路贡献为 1/(60+r)。从未出现的候选最多贡献 1/(60+p+1)+1/(60+q+1)；已出现但尚未在另一流确定位置的候选，上界是已知贡献加该未读流的下一名最大贡献。已耗尽流的未知贡献为0。只有待交付项的贡献／展示来源已确定，其分数严格超过全部其他已见项上界及未见项上界，才可交付；相等时继续读取或使用已证明正确的完整同分规则。这是算法推导，不要求新建用户可见分数或更改权重。

临时独立数学探针以精确有理数验证1000组固定序列，每一步可交付前缀及最终排序均与完整RRF结果一致；同样100项的两条流读到各1项即可证明第一项。它只证明在上述假设下存在提前交付的算法路线，不代表真实关键词／向量已经形成这些流，也不代表数据库／跨服务／浏览器通过；不将随机用例比例当作生产性能收益。实际关键词还有元数据／内容两类匹配流，必须先证明动作过滤后的合并顺序，再进入两路RRF；不能把过滤前名次或未经证明的两种查询分数直接套入上界。

候选读取不足以确定名次时，在原预算内继续获取；预算耗尽保留待定状态，不输出假定排名。权限变化可能改变有效名次及上下界，旧证明不能沿用；本轮联合草案统一使整次游标失效，要求显式重新检索，不能只删当前页一项或在原游标内局部重排。数学路线也不保证任意查询均能提前结束，单路领先或授权稀疏时可能要读很多候选。

**固定版本的标准门禁核查。** 2026-11-08 使用 `make test-manager-postgres`，通过临时 Go overlay 注入能力探针，不修改产品源码、开发环境索引或真实Grant。门禁自行创建固定摘要的独占Meilisearch，PostgreSQL仅连接已核实的 `addp_test`；向量夹具在事务中创建随机schema，回滚后核对零残留，独占Meilisearch由原门禁清理。本轮两次完整门禁均通过，以下仅为检索器能力证据，不代表新检索契约已经实现：

- 六类关键词夹具覆盖单词／多词、默认／显式`last`／`all`、元数据属性限制及正文命中。以放开窗口的完整检索为独立顺序基准，再将窗口降为100、每批31项并排除已消费ID；全部匹配集合与顺序一致，260项的查询确实取到原窗口外候选。四类固定动作谓词夹具另验证了“完整候选流后筛选”和“先过滤后检索”的顺序与分数相同；这些谓词只是合成夹具，不是System真实权限验收，也未完成元数据／正文两路合并的通用证明。
- 排除ID方式在260项夹具的最终过滤表达式已为2127字节，状态及每次请求随已消费候选数增长。没有大规模成本证据，不采用其为产品游标。静态夹具的同分顺序一致也不等于公开稳定键契约；删除已读首项后继续旧offset，实测跳过原第32项，说明索引变化不能靠offset自行保持连续性。索引版本失效和同分规则仍须设计。
- 额外把自有索引的`searchCutoffMs`设为0，Search仍成功返回候选，但`_rankingScoreDetails`含`skipped`、`_rankingScore`为0，响应没有顶层`degraded`。固定版本源码确认预算耗尽会跳过剩余排名规则，而`degraded`只用于内部分析并跳过JSON序列化。因此HTTP成功、非空／不足一批或单一数值分数均不足以证明可供增量RRF使用的完整有序流；具体的未完成排名识别、预算处理及位置不推进规则仍须验证，不能擅自取消预算。
- 向量夹具每批113项，含大量相同距离，精确比较所有9870个结果的身份及距离顺序；第1批与第11批均读取相同221个共享buffer块，扫描行数都是3万。只证明该SQL路线完整，不能将“有keyset条件”解释为已完成性能优化；本轮未启用ANN、未变更真实表索引，亦未实施跨请求快照。

文档依据：[Meilisearch Search API](https://www.meilisearch.com/docs/reference/api/search/search-with-post)、[Pagination设置](https://www.meilisearch.com/docs/reference/api/settings/update-pagination)、[固定v1.54.3预算耗尽排序逻辑](https://github.com/meilisearch/meilisearch/blob/v1.54.3/crates/milli/src/search/new/bucket_sort.rs#L183-L219)、[固定版本响应中的内部degraded字段](https://github.com/meilisearch/meilisearch/blob/v1.54.3/crates/meilisearch/src/search/mod.rs#L1422-L1426)、[pgvector索引与召回说明](https://github.com/pgvector/pgvector#indexing)。能力探针、完整输出与EXPLAIN保存在操作系统临时目录；本节记录可复核结论，不把临时探针当作已接入CI的产品回归。实施时须将最终路线对应反例纳入现有标准T1／T2及其CI自动发现。

**Meilisearch 跨窗口候选流方案（2026-11-08 联合方案已确认）。** 用户已接受以下方向、同分次序调整和宽查询首批准备成本；初始保留状态参数见下文；生产容量仍须独立定标。本段规定一条实现路线：Manager 索引内部序号的有界分段、段内完整相关性排序、跨段全局归并。本批据此实现；不采用分段先后作为结果排序，不累计已消费 ID 的排除集合，不提高原窗口作为总结果上限，不增加引擎。以下容量保证首先针对关键词候选流，不能自动外推为整个混合检索的容量保证。

1. **写入时建立可证明的段容量。** Manager 在自己的索引登记中为索引内的 `DocumentID` 分配唯一、不可变的正整数 `search_ordinal`。技术／正文投影更新同一文档复用原序号，删除不在本世代复用；同名索引重建保留序号登记；索引世代变化使旧游标失效。它只服务索引遍历，不是 DataItem 新身份，不由 Meta 或客户端提供，也不承载 ACL。Manager 将该字段写入自有 Meilisearch 文档并声明可过滤；原搜索属性、`matchingStrategy`、相关性规则和 Security 投影不变。登记保存索引名、序号与原身份的对应关系及索引高水位，不扫描租户全部身份作为每次查询准备。
2. **同一完整范围规则，从根范围到固定段。** 首先对整个当前Tenant／Engine查询范围执行一次有界探测：若精确段计数及实际返回条数一致、都不超过S，且命中排名完整，则整个查询范围已形成完整段，直接使用；不为小匹配集合展开所有序号段。否则丢弃不能证明完整的根范围前缀，展开下面的固定段；这不是把根范围的前W项作为终点。拟定段宽 `S=512`，初始化必须核验 `S+1≤W`，其中 W 为当前 `maxTotalHits`；这是拟议参数，尚无生产规模性能定标。第 j 段过滤为当前 Tenant／可选 Engine 加 `j*S < search_ordinal ≤ (j+1)*S`，最多含 S 个唯一文档。内部段查询使用 `page=1,hitsPerPage=S+1`，只取内部序号、完整分数与排名完成性所需字段；核对该小段精确 `totalHits = 实际返回条数 ≤ S`，再核对每个命中的排名完整性，作为完整段回执。精确计数只限最多 S 条的段，不形成租户或用户总量；内部参数不恢复人员旧页码分页API，不把过滤前命中数或段数返回人员。结果超过 S 或序号／世代不符是索引契约错误，不能截断后继续。单段的“全部”不依赖估算 total，且跨段不再受整个查询 W 的硬窗口阻断。
3. **两类关键词来源各自形成全局流。** 元数据来源只搜索 `name/full_name/fields.name`；内容来源沿当前全属性搜索。每个完整段在 Manager 内按“该来源真实 `_rankingScore` 降序、`search_ordinal` 升序”排序，数值不舍入、不用 epsilon。每个来源对所有段的下一候选维护一个归并堆：弹出最相关段首，再推进该段。不能只拿某段原生前几条、或只整理已收集到的一页；完整段取回后才建立段内顺序，避免把引擎原生同分顺序直接当跨段稳定键。分数仍只用于内部排序，最终展示沿原授权后 RRF。官方[相关性评分说明](https://www.meilisearch.com/docs/capabilities/full_text_search/relevancy/ranking_score)说明单项分数不随其他文档分数变化；具体属性限制与分段过滤仍由固定版本T2核对，不把该说明扩张成所有查询设置下的未经验证保证。
4. **明确同分的产品影响。** 本路线保留现有关键词数值相关性优先及两路对称 RRF 常数60；关键词跨元数据／内容来源仍按现有分数合并，同分先内容来源。来源内部原先依赖 Meilisearch 内部文档次序的同分，现在改为本世代序号升序；这可能改变同分项的关键词名次及相应 RRF 先后，该影响已获用户确认，不能宣称逐项复制旧排名。索引设置和查询属性固定时，分段前后同一文档的匹配依据及分数必须一致；固定版本的真实 T2 是实施前提，不能只凭堆算法证明。
5. **鉴权仍在候选之后。** 归并得到有界候选后才精确回读其索引身份，按 DataItem 去重并沿现有 System→Meta 批量核验。元数据来源只接纳 metadata_read 且未取得 read 的元数据专用结果；read 结果从内容来源接纳，避免同一关键词项重复排名。正文命中不能进入元数据专用结果。候选不够继续推进全局流；不能先按段完成一批授权就输出该段，也不为搜索展开全租户授权范围。依赖失败仍按原规则明确失败，不解释为无权。
6. **只保留实际非空段的遍历状态。** P仍表示潜在序号段数，但不为P个段预建整张状态数组。每个来源保存初始化的下一段位置、已经发现的非空段首和这些段的本地位置；空段扫描完即只推进位置，不长期保留空占位；非空段耗尽后也删除其段首状态。设当前尚活跃的非空段数为Q，每来源只缓存固定L个完整段，则状态空间为 `O(Q+L*S+B)`，B为输入／交付缓冲。缓存淘汰不推进遍历位置，重读同版本的完整段恢复；不能为每段长期缓存S项。未知段尚未检查时仍不得提前交付全局排名。
7. **按实际状态使用量控制资源，不按索引高水位拒绝查询。** 根范围探测及逐段初始化按本次工作预算推进，再根据实际段首、缓存和缓冲计量占用。H／P用于遍历范围与成本分析，不再作为预先拒绝大索引查询的依据，也不以P_max建立结果支持上限；小匹配查询经完整根范围回执即可结束，无需为所有潜在段预留空间。固定段缓存／单批次大小仍有上界，超出实际服务资源时明确失败并保持未推进位置，不把资源不足说成耗尽。此前按16,384段预留及约2.25MiB的计算不作为现行准入策略或会话实测内存；实际Go状态、索引结构、输入缓冲与会话并发均须测量后定标。查询状态的保存载体依下文修订后的联合评审确定，不默认写PostgreSQL。
8. **承认宽查询的首批准备成本。** 根范围已证明匹配集合不超过S时，只需这一段，相关集合很小的查询无需为大量无关索引展开分段。根范围不能证明完整时，为证明全局最相关候选，首次必须取得每个细分段的有序段首或明确空段。未知段可能含最高分，不允许先输出已准备段的结果。每次 HTTP 只准备限定数量的段／候选，预算耗尽保存“准备到哪个段、已完成的段首和待处理候选”，可返回空结果及有效续查位置。整个准备可能扫描全部本次匹配候选，并有与 P 相关的 Meilisearch 查询开销；不逐项鉴权无关索引，但**不承诺首屏固定延迟或没有全查询排名准备**。可以使用原生非融合 multi-search 承载有限个段请求，仍逐段独立核对完整性，不引入另一排序路线。缓存不足也可能使同一有界段被重读；性能验收须计入这些成本。
9. **区分两种预算。** Manager 本次 HTTP 工作预算耗尽时，保存已经完成的段和归并位置，下一次继续推进；Meilisearch 返回 `skipped` 时，该段尚未形成可信排序，不能把结果放进归并堆。可在本段固定序号范围内按数值中点拆成更小的互斥范围，保留本段最多 S 条候选及有限深度的准备栈，跨请求完成同一段；所有子范围完成后再按原段的同一比较键合并，不改变段数 P 的持久容量。已经细分到单个序号仍不能得到完整排名时，明确报告检索依赖无法完成该段，不前移、不伪称耗尽，也不承诺重试一定能成功。空范围要求该有界段的精确 `totalHits=0` 且实际返回0条；关闭distinct、额外分数阈值等会改变集合的参数并绑定检索设置。不能仅凭没看到 `skipped` 或估算计数为0推断空范围；固定版本T2还须覆盖0预算下空／非空段及完整计数回执。
10. **绑定索引版本而非假装快照。** 现有启动 `epoch` 不是内容版本，需在 Manager 既有投递／隔离 owner 内增加搜索可观察版本：写入、删除、清理和检索设置变更登记时先推进版本并关闭新读，待真实 Meilisearch 回执完成后开放该版本；未知投递保持隔离。每次读取、归并检查点提交和最终响应前核验相同的就绪版本，发生变化即使整次游标失效并明确要求重新检索，不能跨版本继续或悄悄从头。序号登记与投递使用同一 owner 事务。首次启用前必须关闭检索，完成Manager自有索引的序号回填及登记／索引一一覆盖校验，再开放新世代；不让缺序号的旧文档被过滤漏掉，也不保留无序号兼容分支。具体回填范围与检索维护窗口按既有索引维护边界确认；本轮不执行回填。不为查询长期锁住写入，也不声称 Meilisearch 提供了跨 HTTP 快照。持续写入可能反复使查询失效，是本路线的可用性限制。
11. **服务端续查与重放。** 由 Manager 保存有实际资源计量和固定过期时间的查询状态，客户端只拿不透明随机引用。绑定主体、Tenant、查询／Engine、两个来源的属性和排序版本、索引世代与可观察版本；不保存 Token 或未来 Allow。每步以版本 CAS 提交状态，同时保留上一步和本步交付身份，直到看到下一个游标作为确认；丢失响应后重放原游标返回同一批身份及后继位置，但须按当前资格重裁、重新构造展示。只保留最多两个检查点；过旧重放明确失效，不重新消费或跳过数据。期限及准入满额时的过期回收须随实现接入标准入口，不创建用户定时任务。

**不遗漏与排序的证明范围。** 在查询条件、索引版本、来源属性和权限稳定且调用方续查至真正耗尽时：唯一序号把索引成员划成互斥、覆盖全集的范围；每段完整匹配不超过 S，故所有相关候选都可取到；所有段首建立后，段内排序加全局归并给出该来源统一比较键下的完整有序序列；在该序列上按当前动作筛选只删除无权项、不越过尚未消费候选。因此即使原 W 项全部无权，后面的有权项仍能进入结果；预算边界和缓存淘汰都不删除任何遍历成员。索引变化使证明失效，走明确失效；已处理项在期间新获权通过新查询查找，符合本节既定边界。

**与混合检索的联合边界。** 关键词流方向已确认；以下RRF容量、观察标记及API扩展仍为待确认草案。向量侧精确续查已验证的可表达性及重复距离扫描成本不因本方案而消失；不启用ANN、不扩其他引擎，也不把暂时关闭向量作为降级路线。

**该草案的验证门禁。** 继续使用 `make test-manager-postgres` 的独占固定版本Meilisearch与安全测试库，通过临时overlay验证真实分段结果；产品实现时转成自动发现的永久T2用例，并以 `make test-module MODULE=manager` 覆盖T1／前端消费者，涉及共享client／System／Meta协议时按实际owner扩散，不派发Hosted。必须包含跨W后才有权、多词`last/all`、两来源动作边界、同分、稀疏与删除空洞、完整段重读、版本变更、排名`skipped`和空响应、容量准入／释放、预算空批、重放与响应丢失。性能另比较相同相关集合增加无关索引后的候选鉴权量、初始化段数、引擎查询数、缓存重读次数、实际状态字节和P50／P95，不只比较System／Meta调用减少。

**本轮草案能力证据（仅临时探针）。** 标准 `make test-manager-postgres` 配合操作系统临时Go overlay在固定Meilisearch 1.54.3真实实例上通过，最后一轮35项主测试通过、无失败或跳过。未改变原生rankingRules，测试W=100、S=64、5个互斥段、最多2个段缓存；7类查询的身份／分数／序号完整排序均与放开窗口的独立完整基准一致，260项集合能越过原窗口。根范围探测分别识别完整空集与单项集合，对80项／260项集合拒绝提前结束；零预算下空段精确回执为0，非空段仍返回完整计数但排名详情含`skipped`，不能接纳为有序段。额外确认`maxTotalHits=100`时，第3页（每页65）实际仍为空，即使响应`totalHits=260`；不能以total或totalPages显示较大就宣称能跨窗口。探针只验证固定引擎上的查询／完整性／归并假设，不验证正式序号分配迁移、百万规模P95、实际字节配额、游标持久化与重放、System／Meta权限变化观察或联合RRF；这些仍按上述门禁实施。

**RRF 待定集合与失效机制联合方案（2026-11-08 已确认，联合实现中，未验收）。** 当前保留“有界候选流、当前批量裁决、可证明前缀交付、观察依据变化时整次失效”的目标。2026-11-08讨论后，已撤回默认将RRF待定项写PostgreSQL以及按全租户上界U预留不足即拒绝的草案；本轮标准隔离T2后建议单遍增量RRF与有期限内存工作集，实际资源预算及超限边界仍待联合确认。本段不修改Grant、组织来源、动作包含关系、委托资格、真实授权或审计方式，不延长任何许可；新增的只是搜索状态和依据失效协议。

**待定状态与交付算法。**

- 参与来源在建立查询时按现有配置固定，已参与来源的依赖故障使整次请求失败，不能被改写成该来源已经耗尽。RRF仍消费已授权的唯一关键词流与向量流，常数60、归一化方式及最终同分规则保持当前定义：分数降序，其次关键词存在优先，再按DocumentID字节序。每项只保留规范身份、两路有效名次、来源／缺席完成标志及必要排名状态，不保存可供未来使用的Allow、Token、名称、正文或高亮。“未知名次”与“来源已经耗尽、确定不命中”是不同状态。历史有效名次是带固定观察依据的派生事实，不能替代下一次输出的当前裁决。
- 待定记录按“查询会话＋规范DataItem身份”唯一。候选／续查状态不默认进入PostgreSQL；只保留当前算法继续排名所必需的工作集、输入缓冲与当前／上一交付批次，不保留所有已经交付结果。正常分页与请求重放不自动等于服务重启后恢复的持久化需求。现有搜索历史记录是独立功能，不能把其已有少量PG写入混同为新增RRF候选集落库，也不能宣称当前搜索API完全不写PG。
- 单遍增量数学基线保留三类记录：两路贡献均已确定的ready项、仅关键词名次已知、仅向量名次已知。ready按当前Go同一float64计算顺序形成归一化分数及既有最终同分键；两个不完整类别按已知有效名次排序。使用支持删除的有序结构，每项只对应一个活动索引条目，不让惰性失效索引随累计读取量增长。这里是工作集表示，不预先选择SQL索引或持久表。分数不额外舍入或按epsilon合并。
- 设已经得到关键词有效名次p、向量有效名次q。未出现项上界为两路下一名贡献之和；关键词单路项的上界为已知贡献加向量下一名贡献，向量单路项对称。每一不完整类别的最大上界由其最小已知名次给出，故每步只取三个类别的头及一个未见项上界，不扫描整个待定集合重算所有上界。边界计算使用与完整RRF相同的归一化float64函数及单调参数；相等时继续取候选，不猜测未见项的同分先后。
- ready头只有在分数严格超过所有不完整项及未见项上界时才可交付；其他ready项之间按完整同分键选择，不要求同分ready项等待无关来源。缺失一条流的最终贡献／展示来源尚未确定时，不发送最终分数或来源标签。来源耗尽后，按有界工作量将未知名次标成确定缺席并转入ready；转换未完成也有可续查位置和保守上界。两路均耗尽且所有剩余记录均完成时，按完整RRF顺序继续交付。
- 预算耗尽保存原流位置、p/q、必要待定状态及未交付缓冲，允许空批续查。只有两个来源都已证明耗尽、无待排序／待交付项时才宣布没有更多；不能把“尚无ready项”、到达工作预算或持久化额度当作耗尽。相同流可很早交付，反向或错位流可能保留整个匹配集合；不承诺每种查询都提前出首批。

**容量与保存方式修订（2026-11-08）。**

- 不再使用 `U=索引身份上界+ready向量数量` 作为查询准入条件，不为每次检索强制COUNT ready向量或预留全租户潜在候选空间。索引大／潜在命中多不是拒绝查询的理由。分页控制交付量，有界取数控制单次输入；两者不证明严格全局RRF的单遍工作集恒定，因此仍须按实际占用计量。
- 资源控制只针对已经保留的段首／RRF项、当前输入／交付缓冲、有效会话与并发。每次继续取批次前为该批实际所需的有界缓冲留出空间；不能先取数、推进位置再因放不下而丢候选。不得通过Top-K淘汰待定项、伪造耗尽、静默改排序或自动关闭向量来处理压力。预算结束且状态可容纳时保留进度并续查；真实资源不足是明确资源错误，位置不推进，不能返回空成功冒充没有结果，也不能无限返回一个没有可取得进展的续查状态。
- 先评审有期限服务端内存工作集能否满足已声明的运行范围；服务重启／状态丢失时明确让游标失效，不能静默从头继续。原需求并未要求重启后恢复搜索，也没有证明必须将每个检索会话及待定项持久写PG。跨实例的状态归属、内存计量、重放原子提交及TTL必须在选定载体时一起说明，当前尚不宣称进程内状态支持跨实例恢复。
- 仅当实际双路样本和并发测量证明需要溢写时，才单独决定持久化的介质、阈值、回收和成本；不预建搜索临时表、持久配额账目或后台流程。若采用溢写，按实际需要发生，而不是因为检索开始就写全体候选。当前也没有足够证据承诺任意规模严格RRF都可在固定内存中完成。
- 数学对照及本轮标准隔离T2已比较“保留状态”和“同一观察依据下重放来源”；实际成本及唯一推荐见下文。两条研究算法不作为产品双轨实现，当前建议仅采用单遍增量RRF，仍待用户确认。

**本轮工作集核查与重放数学对照。**

只读汇总时，本地Manager的embeddings表为空，无法获得真实双路检索的待定峰值。这个当前状态不能反推历史时刻的向量数据或权限，也不证明未来业务分布。以下来自仓库回归形态和合成的固定、已授权有序流；页大小20、输入批次200，每个交付前缀及最终结果均对照完整归一化RRF，不是生产查询统计：

| 形态 | 单遍增量：首批前待定峰值 | 单遍增量：读完全部页峰值 | 说明 |
| --- | ---: | ---: | --- |
| 仅关键词1万项 | 1 | 1 | 另一路真实耗尽后即可按关键词流交付；输入／页面缓冲另计 |
| 两路各1000项、顺序相同 | 1 | 1 | 不需要根据总结果数预留2000个待定项 |
| 前100项相同、后各900项分离 | 1 | 1800 | 只读首批与翻到后面的实际工作集不同 |
| 两路各1000项、完全分离 | 2000 | 2000 | 不重叠也可能形成大待定集合，不只是逆序 |
| 两路各1000项、完全反向 | 1000 | 1000 | 先前的构造边界继续成立 |
| 关键词1万项＋独立向量1项 | 10001 | 10001 | 尚未证明向量项在关键词流中缺席，可能阻塞大量ready关键词项 |

临时固定64字节记录编码只表达身份、名次、分数与状态，10001项约625.1KiB；这不是Go map／有序索引／缓冲的实际RSS，也不是PG物理占用。每个活动待定项只有一个活动有序索引条目，不累计陈旧堆项；上一版按每项1KiB进行PG配额预留不再作为容量事实。

重放对照模型先以公平推进、只保留计数及流位置发现较短的有效来源，之后重放并完整保留短流的身份／名次，继续遍历长流只给短流项补齐另一名次；短流项全部找到或长流真正耗尽后，其RRF贡献完整。长流中不属于短流的项只有一路贡献，顺序可由重放长流恢复，再与短流的完整RRF序列归并。这样释放长流项的内存有可证明的重新取得路线，不是丢弃不可恢复候选；源ID去重、原完整同分规则和每步前缀仍与完整RRF一致。不能通过未授权全量身份枚举发现短流，模型遍历的仍是本次查询候选经当前裁决形成的有效流；每次实际重放必须重新核验同一观察依据和当前凭据。

在上述“关键词1万＋独立向量1”构造中，重放模型保留1个锚定项及有界缓冲／长流段首，候选访问次数从10001增加到20003；它不是实际System／Meta RPC次数或耗时。相反，两路1000项顺序完全相同时，单遍模型读取40项便交付首20项，重放模型需3999次候选访问才交付首批，明显增加准备成本。两模型另外经1200组随机固定流核对全部前缀和最终顺序一致，只证明计算取舍可行，不证明真实Meilisearch／pgvector可以低成本重放，也没有提供真实Grant、动态变化、跨HTTP状态或容错验收。该对照说明“大待定峰值”不能直接推出“每次必须写PG”，也不支持未经评审就改为所有查询强制多遍扫描。

**标准隔离 T2 实测与单一路线建议（2026-11-08，待用户确认）。**

本轮只使用操作系统临时Go overlay，通过原标准入口执行研究探针：Manager使用`addp_test`与门禁独占、自动销毁的Meilisearch，System使用`addp_iam_test`内的隔离用户、功能角色及节点Grant。没有修改开发用户／真实Grant，没有新增搜索PG状态表，没有产品代码／API／迁移变更。最终Manager与System定向门禁均退出0、无失败或跳过；跨窗口组合反例另经同样两套门禁通过。一次早期探针插入位置占用了原测试上下文，后续原断言因超时失败；保留失败记录后把探针移到原断言之后重跑通过，没有放宽产品超时或权限。

方法：每场景索引8192篇文档，真实Meilisearch 1.54.3、W=1000、S=512、段缓存8；真实pgvector 0.8.0，每场景8192条2560维向量，使用拟议精确距离keyset SQL，不使用ANN／全量allowedItems过滤。真实引擎召回批次为200、交付20，两算法逐项交付都核对独立完整RRF排序；六组全部前缀与最终顺序一致。先暖两条研究算法，再交替顺序执行3轮计时，另做3轮内存采样。关键词夹具为同分名称命中，向量产生可控有序距离；此前多词／不同分数分段能力证据仍独立适用，本轮不是生产查询分布。

将Manager实际产生的全部候选／最终输出复核批次流式写到临时文件，随后在System T2逐批调用现有`CheckManagerSearchRead`，真实重新解析当前IAM凭据并执行原Grant SQL，逐项断言可见／内容可读结果。Meta使用原HTTP client和事实协议连接T2权威事实夹具；不是实际Meta数据库／全栈T4。最终引擎探针批次与两次成功System测量的输入逐字节相同，不能以模型访问次数代替实际调用。新观察版本／HMAC标记尚未实现，其常数开销未计入。

下表为读完所有结果；箭头均为**增量 → 重放**。引擎耗时为真实Meili HTTP查询及pgvector SQL读取累计墙钟中位数，System耗时是独立服务函数逐批调用累计中位数，二者不能相加冒充端到端时延。候选数包含重复读取的目标，未合并成唯一身份数；两边相同的最终输出复核另计，System耗时已包含它。

| 形态 | Meili／向量查询数 | 引擎ms | 候选鉴权目标数 | System裁决ms | Go存活堆净增KiB |
| --- | --- | ---: | ---: | ---: | ---: |
| 两路1000项同序 | 19/5 → 38/11 | 223 → 444 | 2000 → 4000 | 874 → 1304 | 104 → 174 |
| 两路各1000项完全分离 | 19/6 → 38/16 | 245 → 586 | 2000 → 5000 | 1342 → 2304 | 360 → 189 |
| 两路1000项反向 | 19/5 → 38/11 | 248 → 482 | 2000 → 4000 | 873 → 1468 | 216 → 175 |
| 关键词4000项＋独立向量1项 | 25/1 → 75/2 | 485 → 1364 | 4001 → 8202 | 3215 → 3680 | 684 → 329 |
| 前900项无权、后100项有权 | 19/5 → 38/11 | 254 → 520 | 2000 → 4000 | 432 → 822 | 19 → 16 |
| 前1900项无权、窗口外100项有权 | 21/10 → 42/21 | 464 → 987 | 4000 → 8000 | 808 → 1724 | 19 → 16 |

首20项差异在同序场景最明显：增量只提交400个候选，重放提交4000个；引擎累计约136／444ms，真实System裁决约92／878ms。全程同序场景另各复核1000个输出目标；长短悬殊场景各复核4001个；前1900项无权场景各复核100个，且两算法都完整找到窗口外后100项。后者实际System全程调用25／45次、Meta HTTP 85／165次，不修改规则、不缓存Allow。

内存列仅为探针Go进程在状态阶段采样、完整GC后的存活堆净增中位数：包含RRF记录、有序索引、来源缓冲及段缓存，夹具／完整排序oracle在基线前准备，输出／trace不全量留内存。基线和采样都执行两次GC以排空`sync.Pool`的旧缓存，避免缓存释放产生虚假的零净增。它不是Manager生产RSS、Meili／PG进程内存、任意瞬间峰值或生产配额。完整采样范围保留在临时比较证据中。长短悬殊场景的RRF记录从4001降至1，但整体存活堆仅从约684降至329KiB；来源准备仍占内存。该场景全程分配量从约36.0增至104.7MiB，重放还增加了分配／GC压力，不能只比较待定条目数。

**已确认采用单遍增量RRF＋有期限内存工作集，联合实现中。** 保留三类可删除有序结构、每待定项一个活动索引条目，及时释放已交付身份及耗尽段；继续采用已确认的Meili完整分段归并及精确向量续查。不要实现多遍来源重放作为另一条检索路线，也不默认将候选状态写PG。相同游标的幂等响应重取仍保留，它与重放来源重算RRF是两回事。依据变化仍整次使排名和旧游标失效，输出前继续原当前裁决。

容量只计实际已分配的记录／索引容器容量、活跃段首、固定段缓存、输入／交付缓冲和有效会话；扩容／下一批取数前检查单查询与进程总预算，不按租户高水位或潜在命中全集预留。预算可暂停且状态仍可保存时续查；真实资源不足明确失败，不能丢项、伪装耗尽或静默切成重放。内存状态的TTL、进程／实例归属、状态丢失失效和有界原子发布仍须随联合实现落实，不宣称跨实例或重启恢复。两者均无固定内存的任意规模保证：增量依赖实际待定集合，重放仍需保存较短有效流，均可能随查询增长；分页不能消除此约束。

该结果支持算法选择，不定标生产默认MiB／并发上限，也不证明百万项容量、并发P95、观测版本写争用、故障回滚或跨HTTP撤权时效。上述运行预算和新协议仍须经标准门禁验证；研究数字不能代替产品验收。临时研究证据不新增长期文档或脚本。

**保留状态与索引准备约束。** 首版遵循现有Manager单活搜索出口；内存会话固定TTL为5分钟，初始保留上限为单查询8MiB、进程搜索状态64MiB及最多64个会话，每步最多32次候选引擎读取、分别2000个候选复核与2000个RRF排名工作单元。这是保留状态预算，不是进程RSS或生产容量认证；底层数组容量、段首、固定段缓存和输入／交付身份缓冲均计入，身份使用定长指纹，已交付槽位复用，无惰性陈旧堆项。实际部署定标与并发门禁仍须完成。源HTTP投影、展示正文和Token不进入持久会话；同请求裁决去重仅存在于当次操作，跨请求缓冲会重新裁决。

序号映射是Manager自有索引元数据，只在投递／索引准备时写入`search_document_ordinals`并推进出口高水位；每次检索不写RRF、待定集合或游标到PG。旧索引缺少序号时由现有回执恢复循环分批只补`id＋search_ordinal`，保留正文／技术快照，完成前搜索保持隔离；清除内容的技术快照替换仍保留既有序号。没有新增清空索引或自动重扫源数据的路线。索引投递及向量实际变化在同Owner事务推进材料版本；检索等待投递已明确收敛后观察版本，重启／状态丢失明确失效，不宣称跨实例或崩溃恢复。

关键词先按分数、内容同分优先及序号归并两条原始流，再应用各来源的当前动作筛选；这样与先筛选再归并等价，且无需先穷尽一条无有效候选的流来建立另一条流的首项。Agent在原`data.search`工具调用内部消费空批续查，保持原Client／AgentRun／ToolCall委托绑定；不将游标携带到另一次工具调用，不通过放宽ToolCall绑定支持跨调用复用。

**用于整次排名失效的观察依据。** 不能只重查当前页，因为撤销、到期、获权或Meta身份变化可能改变之前的有效名次、来源分类及RRF上界。每次查询固定一份观察依据；任一依据变化统一使全部旧排名及游标失效，不在原会话内重新过滤后接着输出。

| 依据 | 唯一Owner与草案机制 | 处理 |
| --- | --- | --- |
| 主体／组织／功能资格 | 复用System当前凭据解析、原Client／委托绑定、既有authorization_version与当前有效资格投影；该版本已有Membership／组织／角色变更触发器 | 凭据或资格失败沿原401／403；同一查询绑定变化则失效。源Grant版本不复用authorization_version，避免仅为搜索失效而撤销登录Token |
| 源Grant与引擎授权事实 | System新增Tenant级source-rule提交版本，Grant签发／撤销及影响源裁决的引擎事实写入在同一Owner事务递增 | 同租户无关Grant变化也可能保守失效；不返回变更对象或影响数量，不建立ACL副本 |
| 自然生效／到期 | System数据库墙钟计算下一次可能改变当前主体资格／来源的时间边界，见下文 | 无需等定时任务或数据库写入；跨边界失效，旧游标不能续期 |
| Meta身份与边界 | Meta新增Tenant级authorization-basis提交版本，覆盖MetaItem事实读取字段及持久Node父链的相关变化 | 数据项缺失／重现、路径或组成变化、Node移动等使旧依据失效；Manager不跨Owner读表 |
| 检索材料与排序 | Manager的索引世代／可观察版本、向量检索版本、查询向量和模型／检索语义版本，以及现有Security保护就绪状态／持久投影checkpoint | 变化或既有保护门禁失败均停止旧会话；不把源规则观察标记当成Security许可 |

**提交版本必须反映已提交事实。** Source-rule与Meta authorization-basis版本使用Owner自己的租户版本行，在改变事实的同一事务内执行原子递增；回滚无可见变化，并发写入通过该行短暂排序，不持有跨HTTP的数据库锁。Meta只跟踪当前授权事实选择实际依赖的ID、Tenant、Engine、Node、类型、名称／全名、fingerprint、attributes和软删除，以及Node身份／父链；row_count、size_bytes、scanned_at等不在该事实选择中的列不单独触发。attributes整体变化仍可能保守失效，不新增一套猜测各格式含义的字段白名单。批量清理、初始化及迁移也须保持版本／世代不可回退；不以启动epoch、时间戳或 `MAX(data_item_changes.id)` 代替。现有BIGSERIAL变化源没有证明提交有序，低号事务可能晚提交而MAX不变；PostgreSQL的[序列语义](https://www.postgresql.org/docs/15/functions-sequence.html)及[事务隔离说明](https://www.postgresql.org/docs/15/transaction-iso.html)不能被解释为提交顺序保证。这里增加版本行的写入串行成本须实测，不推广成其他模块通用锁方案。

**时间边界不成为授权期限。** System在当前已核实主体及其直接组织来源下，仅聚合可能相关且未撤销Grant的未来granted_at／expires_at、Membership期限及资格相关Role Assignment的生效／到期时间；不读取或返回可访问DataItem全集。边界允许保守地早于实际可见集合变化，例如一个Grant到期但另一个Grant仍覆盖同项。每次都按System数据库当前墙钟重算；跨过旧边界后，下一边界随之变化，旧观察标记不能匹配。原SourceReadRules返回的本批有效期限不足以覆盖之前已过滤或尚未读取的目标，不能直接当整次查询的稳定期。Token过期仍由每次实际凭据解析处理：相同主体／Client／委托约束和资格依据下，合法刷新后的当前Token可以继续；旧Token过期不允许调用，但不把初次Token期限固化成可延期的源授权。

**固定System观察协议与Meta最小版本读取（已确认联合API变更）。**

- Manager继续只调用现有固定 `POST /engine-access/metadata-checks/manager-search`，转发当前原始User／既有委托凭据。该操作使用独立的固定用途请求类型：`targets`允许0至200项，外加查询随机nonce及显式可空的`expected_observation_tag`；null只用于建立依据，后续必须传原标记。其他元数据检查请求仍要求原来的非空目标，不放宽共享校验器。零目标仍核验完整资格并读取版本／时间依据，只观察查询能否继续，不授予数据读取。
- 响应保留本批逐项metadata/read裁决与原SQL规则观察时刻，增加不透明`observation_tag`；仅Visible目标附上Meta实际核验的规范item_fingerprint，供Manager核对索引／向量身份并跨来源去重。不可见或身份无法证明时不返回该身份，不能借回执区分隐藏存在与不存在。路径相同但核验身份不符不得把旧展示挂到新目标上。标记由System对主体与Client／委托绑定、资格依据、Source-rule版本、Meta依据世代／版本、规则语义版本、下一时间边界及查询nonce计算HMAC。每次重新计算后与期望标记比较，不保存System侧查询会话；`observed_at`不参与标记，否则时间流逝本身会令每次请求都不同。Manager只存不透明标记，不解码或向人员返回版本、时间边界、Grant或隐藏资源信息。
- MAC专用子密钥从System已配置的32字节EncryptionKey按HKDF-SHA256及固定`manager-search-observation/v1`域派生，随后使用HMAC-SHA256；不直接复用AES操作，不增加环境变量、账号或权限。密钥／语义版本轮换使旧标记失效，不保留接受旧标记的兼容分支。域分离依据[HKDF的info定义](https://www.rfc-editor.org/rfc/rfc5869.html#section-3.2)。观察标记不是Access Token、EA、Grant或未来Allow；即使标记匹配，本批实际裁决和最终输出裁决仍须执行。
- Meta增加唯一的最小机器读取 `GET /api/v1/meta/runtime/data-items/authorization-facts/revision`，只返回当前Tenant的事实世代、提交版本及语义版本。仅既有`addp-system` Tenant Service和`meta.authorization_facts.read`，不授予Manager或Catalog该新入口，不返回数据身份／数量。复用现有System→Meta服务凭据及边界，不修改原授权事实DTO的逐项含义，不新增第二条事实读取路线。
- 一次System观察先读Meta版本M0，再沿现有身份／批量事实链准备本批边界，在System短只读快照中解析当前凭据并观察相同Source-rule版本S0和规则；退出本地事务后再读Meta版本M1，并以**新快照**复核System资格、S1和时间边界。要求M0=M1、S0=S1且本次前后观察标记相同、等于原查询期望；任何不一致明确失效，不自动重试并采纳新版本。不同步锁住两个Owner，不让同一Repeatable Read快照里的重复查询冒充已看到后来提交的变更。Meta版本观察是每批／每请求的常数调用，不是每候选调用，也不读取历史全部候选。

**Manager请求提交与交付观察点。**

1. 每次续查／重放首先核验不透明游标归属、固定查询参数／页大小、TTL和检索材料版本，再以当前凭据做System空目标观察，匹配原标记。随后才恢复段位置与待定状态；读不到依据不发送旧结果。
2. 在本次预算内召回、去重并沿同一System批量链裁决，只有匹配原观察依据的批次才赋予有效名次。初次候选缺失或身份不可靠仍沿原逐项不可见语义处理；已参与排名的身份／动作与先前观察不一致才触发整次失效。要输出的身份再次按当前凭据裁决，重新读取正确来源展示并经过原Security出口；若依赖可用但任一资格／依据／动作结果不一致，整次会话失效，不能仅删除当前页项或正文降级成元数据项。
3. 输出前再次完成System观察并核对Manager本地材料版本／保护状态；全部通过后才在会话单写入锁下发布后继游标及本批身份。外部检查点仅在成功发布时推进，同一游标并发重放只发布一次。未交付的内部工作态可继续保留；源已取回但未核验的批次保留为pending，不越过丢弃。依赖故障不发送暂存结果、不发布新游标；同一游标重试先重新验证原依据，再继续该工作态，不复制全体RRF项或从头重放来源。真实容量不足明确终止并释放该查询；不返回伪耗尽或一个必然无法取得进展的成功空页。
4. 不复制整个RRF工作集保存每页历史。保留必要的最新状态、重放检查点和上一交付身份批次；原游标重放只重读已保存的同批身份及后继游标，重新裁决后返回，不再次推进排名。收到后继游标视为确认上一批，才能清理其已解决记录。末批响应丢失在TTL内可重放，末批缓存到期后明确失效，不承诺无限恢复。
5. 最终边界是最后一组成功Owner观察所证明的共同已提交依据及其System墙钟观察点，**不是响应字节发送时的跨服务原子授权**。在最后检查后才提交的撤销／Meta变化可能落在检查至发送的间隙；下一请求必须观察并拒绝旧会话。已经发送的数据不能收回；不冻结Grant、不延迟撤销、不新增全局事务或长期授权锁。这个实际边界需要作为联合方案的一部分确认，不能宣传零间隙即时撤回。

**失败语义与前端。** 新数据／授权／时间／模型依据不匹配用409 `search_context_changed`，状态存在但TTL过期用410 `search_continuation_expired`；格式错误用400，非原主体的会话与不存在使用同一404，当前凭据或功能资格失败沿既有401／403。故障沿真实502／503／504等类别返回，不等同耗尽；容量不足使用上述503。Manager页面与Agent消费端在观察到失效或权限失败时清除该查询已累计的各页、待展示高亮和游标，显示安全的重新检索提示，不披露触发Grant／对象，不自动重放新检索。临时依赖失败停止展示旧结果并保持服务端检查点未推进，用户可在依据仍相同时显式重试。唯一搜索API删除旧page／total契约，空结果带可续查游标表示仍可查找，不表示一定还有一条可见结果。

**独立模型证据与实施门禁。** 上一轮操作系统临时模型按当前归一化float64 RRF核对2000组固定有序已授权流，每步前缀及最终顺序一致；三个类别头的上界与逐项全量计算一致。反向1000项构造的待定峰值1000、相同顺序构造峰值1，说明最坏容量不能以早交付样本替代。另覆盖版本变化、自然到期、刷新凭据不代替资格、低号晚提交、丢失响应重放不推进及Owner前后版本不一致的抽象状态。旧的U准入模型只曾验证过该已撤回草案的内部记账，不能作为当前方案证据；本轮工作集／重放对照见上文。它不是实际Go／SQL实现、PostgreSQL字节定标、真实Grant／IAM／Meta、HTTP、崩溃恢复或T1-T4验收。

联合实现同批须接入Manager／Common／System／Meta及实际Agent消费者，生成Swagger和双语错误并删除旧请求处理。实际状态预算、所选保存载体、有序状态结构、重放／清理和索引维护迁移必须同次完成；不得先发布新游标但以后再接失效版本。按实际共享依赖运行 `make test-changed`／`make test-module MODULE=<owner>`，并沿 `make test-manager-postgres`、`make test-meta-postgres`、`make test-system-iam-postgres` 的engineaccess与migration分组验证真实并发提交、回滚、到期、组织退出、Grant变化、Node移动、同分／反向及不重叠大集合、实际工作集字节、时间边界聚合的执行计划／成本、每批固定版本观测次数及版本行写争用、前端旧页清除与查询不自动重启；同步 `make test-authorization` 和Swagger标准门禁。新增用例／选择器在原标准入口及其CI自动发现覆盖，不新增测试database；T4派发仍须按单次批准范围。本批同步变更产品代码／API／迁移及消费端，本地产品门禁结果另行记录。研究探针与旧35项通过不计为本联合方案通过，未运行Hosted。

**权限变化和最终交付。** 沿已确认联合方案的“依据不匹配则整次失效、输出前当前裁决、固定共同观察点”为唯一方案；不再采用局部删除后在原游标继续的路径。任何状态都不延长资格／Grant期限，也不把已发送数据视为可撤回。

**实施与验收边界。** 完整分段流、同分调整与宽查询准备成本、单遍增量RRF、短期内存工作集、System／Meta版本观察、整次失效和最后检查至发送的观察边界已经确认并联合接入。默认PG候选落库、按总命中估算U准入及多遍来源重放不作为产品路径。实际预算与标准T1-T3须通过才发布；真实身份T4、生产规模／并发P95和跨实例恢复不在本批已证实范围。

**2026-11-08本地产品证据。** Common／Manager／System／Meta Go T1、Manager原生PG＋Meili T2、Meta全T2（含5→6启动升级、并发提交与回滚）、System完整engineaccess T2、Agent离线及共享Python、授权／Swagger和T2登记检查通过。另在隔离main快照排除其他会话改动后，四Owner Go及Manager／Meta全T2、System检索T2通过。Manager前端408项单测、348项受控浏览器及构建通过；共享Python仍有1项原有跳过，单独保留，不计通过。窗口100的1130候选夹具中前1100项无权、后30项全部交付，6次续查含1次空批，重放顺序一致且未写入检索状态／投递记录。首次迁移触发器、断言／夹具及Swagger工具夹具超时的失败日志保留，修正／复验与原失败分别记录。上述不是生产容量、跨实例恢复或真实身份T4证据。

**验证与发布要求。**

- T1 验证前多批全部无权但后批有权、末批不足、全部耗尽、预算耗尽空页仍可续查、批内未交付候选保留、跨来源去重、正文命中不可降级、缓冲项撤权／到期、游标篡改／跨用户／跨租户／重放及错误不推进位置。各检索器越过原硬窗口后的有权结果必须有反例覆盖；不能删除原拒绝和清理断言。
- T2 沿 System 当前组织与 Grant 规则、Meta 身份边界及 Manager 真实 PostgreSQL／Meilisearch 门禁验证。排序／遍历的实际引擎能力不能仅由 mock 证明。前端 T3 验证无总页数的续查、空批继续、权限变化和游标失效提示。
- T4 沿原普通账号和既有 Agent 检索入口验证同一裁决、局部授权、递归范围、撤销／到期及失败清理；按批准范围派发，不修改真实授权，不设定时。
- 性能对照固定查询、授权及相关候选，增加无关索引项，记录授权候选数、Meta请求数、分阶段耗时及P50／P95；另测宽泛命中和稀疏授权。不能只以总时延下降替代不漏项及隐私断言，也不保证索引检索本身在所有规模下恒定耗时。
- 实施时同步唯一检索路由的请求／响应DTO、共享client、Manager页面、现有Agent消费者及Swagger，删除旧page／total兼容处理；本批代码同步更改唯一API契约；运行服务在用户重启前尚未切换。根Makefile、既有System／Meta／Manager门禁与Agent实际消费测试的影响登记须在代码实施前核定，未覆盖处同批接入，不先派发新验收。

## 九、实施范围与验证

| 范围 | 规范与实施边界 |
| --- | --- |
| AuthContext | 唯一 v1 Schema；破坏性变化同步 Schema、Go／Python 类型、System 投影及所有消费者，不按客户端保留旧版本 |
| 源数据授权 | PG schema 普通表 single 已完成直接／业务批准、个人／部门／项目组来源、成员退出、Manager 读取及收权的真实验收。FTP IFC／3DS 精确文件组成已完成模型生成与读取、组成变化、Deny、撤权及失败清理的真实验收，限于已验证的格式 profile 和消费入口。其他引擎、格式与入口仍须逐项闭环，共享协议或持久化约束的表达能力不代表实际读取已接入 |
| Meta 基础识别 | 清单组合识别不代表所有格式已支持必要结构读取；身份、完整性与边界契约须逐格式验证 |
| Meta 集中采集 | 2026-11-01 已确认改为功能权限加引擎管理范围，取消逐任务批准；代码已替换旧批准主路径并移除旧页面和 API；迁移关闭旧 PG 调度并通过 Owner 取消旧执行；独立取消权限不自动分配。新规则 T1／定向 PostgreSQL T2 已通过，部署迁移及跨服务 T4 尚未验证。原 PG entire-engine basic 逐任务批准的 T1／T2／T4 是历史证据，不证明新规则。最新目标仅扩展 PG：basic／deep 权限统一、注册初始管理范围与精确单项刷新，须先完成元数据／内容结果隔离；这些新增目标尚未实现或验收，不外推其他引擎 |
| 节点范围与源 Deny 退役 | 2026-11-02 已确认持续节点 Grant 覆盖当前及以后新识别项、可选递归；取消源数据显式 Deny，按有效个人／组织、节点／数据项来源并集裁决。最小只读节点事实机器权限已获确认。节点主路径、最小子节点分页、退役迁移与本地 PG T2 已落地；PG、NFS、MinIO 的资源树混选按当前准入形成精确项或持续节点 Grant，不把节点展开成叶子命令。真实验收按[分版本证据](testing/evidence/数据保护与执行血缘历史证据.md#manager-授权范围刷新--nfs-通过与-pg-前置失败2026-11-04)分别记载；NFS 已有范围刷新通过，PG 范围切片通过不代表完整套件通过。旧 Deny 测试只说明历史实现，不证明新方案 |
| 元数据查看授权 | PG 的 `metadata_read`／`read` 共用签发、撤销及当前规则裁决；两类动作的包含关系、实际来源分组及按动作撤销已通过本地 System PostgreSQL T2。System 页面已区分现有记录的动作；普通 User 及现有 Meta 目录／祖先工具的严格委托凭据核验与统一 client 已接入。PG Meta 资源树、节点、祖先及树搜索已按当前规则过滤后再计算数量、裁剪深度，结果仅含身份／导航安全投影；已有本地 T1 与 System 实际委托凭据 PostgreSQL T2 证据。PG Meta 通用列表、ID／路径详情、字段与空间声明、祖先链、节点及统计也已接入同一裁决：结构投影去除样本／源统计／注释／表达式，隐藏目标返回 404，总数和分支统计只计可见对象，本地 Meta Go T1 与既有 Meta 全 PostgreSQL T2 回归已通过。PG 数据项／字段血缘已接入固定 `meta.lineage.read` 当前核验，先过滤再遍历、计数和截断；用户服务血缘暂时拒绝，机器事实契约不变；Common／System／Meta Go T1、授权门禁、System engineaccess PostgreSQL T2 与 Meta 全 PostgreSQL T2（含 item／field 可见性反例）通过。System 新增授权已支持不预选的两类动作并沿原共享树多选；Catalog 已贯通显式动作、存储约束、按动作去重、候选／历史展示及不可升级的办理绑定，System 受理／签发仍验证原动作及 PG 接入范围。其余 Meta／Manager 消费出口尚未全部完成，本批动作选择与 Catalog 元数据批准的跨服务 T4 尚未验证，不能视为用户查看隔离闭环已完成 |
| 跨引擎精确 DataItem 元数据与血缘 | 2026-11-03 已接通可靠持久身份、当前 System 元数据裁决及 Meta 列表／树／详情／统计／血缘过滤；single／multi／whole 元数据专用边界不授予内容读取，原生表及集合的元数据签发也不依赖内容读取校验器。隐藏关系仅以布尔提示告知，本批当时未扩展普通节点范围；后续节点准入见持续节点范围，Catalog 办理范围不扩展。全仓 Go T1、本次最终 Meta／System Go 检查、System engineaccess PostgreSQL、Meta 全 PostgreSQL及最新授权事实专项通过；共享前端 333 项、System 单元 158 项、授权页固定快照 94 项与构建通过；授权／Swagger及现有 T2 登记门禁通过。服务未重启，迁移部署和真实跨服务 T4 尚未验收，不外推为各引擎内容读取已贯通 |
| 产出数据初始使用权 | 5.5.8 的初始只读 Grant 已接通 PG Transfer 实际首次新建成果：可信成功产出回执、精确 basic 补扫、System 唯一签发／不可变终态及后台续办已完成 T1／T2 和真实 T4 验收。真实验收覆盖成果读取、旁观者拒绝、源撤权后物化成果仍可读、成果撤权后拒绝及清理。Develop 等其他生产者尚未据此完成接入；任务成功或扫描成功本身仍不代表正式 Grant |
| multi／whole 授权 | 合法组成、范围内增长和越界拒绝的契约已确定；基础组合识别与 single 精确文件组成的通过结果不能代替 multi／whole 授权及实际 I/O 的端到端验收 |
| 列／行授权 | 保留同一 DataItem 上的扩展方向，具体规则和执行接入尚未确定 |

上述通过范围对应的固定源码、运行与限制统一见[验收证据索引](testing/验收证据索引.md)。历史通过不表示当前共享工作区或未验证入口已通过，性能采样也不扩大功能验收范围。

每个实现切片至少验证：可信身份及 Tenant 隔离、默认拒绝和源 Grant 来源并集、撤销与到期、原命令幂等与并发、未知结果恢复、实际读取与批准范围一致。DataItem 接入另须覆盖 basic／deep 身份一致、组合不完整拒绝、whole 合法增长可读而越界拒绝、精确项新项不继承、节点本级／递归及未来新项覆盖、重叠来源独立撤销、Meta 不可用不回退及无 Catalog 的直接批准。

验证遵循[测试与验收规范](addp测试与验收规范.md)：工作区使用 `make test-changed`，明确 owner 使用 `make test-module MODULE=<模块>`；共享契约专项使用 `make test-authorization`。文档修改按标准影响选择运行对应门禁；确定性测试不代替真实跨模块验收，跳过、失败及未运行分别说明。

实现入口：[System IAM 数据模型](../../system/docs/IAM数据模型与迁移规范.md)、[System Swagger](../../system/backend/docs/swagger.yaml)、[企业资源目录实现规范](addp企业资源目录实现规范.md)、[Manager 模块说明](../../manager/CLAUDE.md)。迁移编号、接口参数、调度批量和当前验收记录留在各自 owner，不再追加到本规范。

授权对象唯一取自 Meta 已提交的 DataItem 身份、layout、主文件及完整 item.refs。System 与消费模块校验这些正式事实，不通过扩展名、同目录探测或文件数量重新拆分、拼装授权对象。格式内容处理能力单独判断；出口尚未支持完整组成时应拒绝该读取方式，不能删减组成后借用旧许可。Meta 事实本身不等于读取 Allow，源数据显式 Deny 仍按本节既有退役规则处理。

Manager 交互分割只消费当前普通 User 的读取裁决。JPEG 保持已接入的单文件边界。TIFF 授权消费 Meta 实际扫描确认的完整 DataItem：唯一主 image ref 必需；没有辅助成员时为 single，有任意辅助成员时为 multi，辅助成员数量不固定为两个或四个。辅助角色沿用 TIFF 格式契约的 world_file、crs、header、auxiliary_metadata、overview、mask，完整保留各 ref 的精确路径和 required 标记；保持既有同引擎、同类型父目录／前缀、唯一有序路径及共享传输上限。不通过扩展名、目录枚举或消费能力裁剪、补造授权对象；layout 与正式组成不一致时要求 Meta 刷新，不在消费者中改写。授权身份不依赖 plain_tiff／geotiff／cog profile。

外置 .msk 按实际扫描存在与否登记为可选 mask 辅助成员；不存在不妨碍 TIFF 主文件，存在则完整进入组成、授权边界与组件版本。辅助成员可选表示格式允许其缺席，不表示已登记成员缺失时可以忽略，也不表示它不影响空间定位。读取已登记成员失败须报告组成变化并要求 Meta 刷新；刷新后重新核验正式完整边界，旧精确 Grant 不自动覆盖新组成，节点 Grant 按当前完整组成及范围核验。缺少定位事实保持未知。Meta 投影不探测源目录；没有 refs 的 single 主项沿既有单体事实表达，显式 refs 必须完整保留。System 沿既有精确／节点 read、期限、撤销与 expected_source 裁决；metadata_read 不能授权传输像素。TIFF 与 ENVI 复用既有 Raster COG 执行目的及完整冻结配置，System 独立核验发起人、任务版本、引擎版本和当前完整组成读取权；空间定位、像素解码与出口能力各自核验。组成摘要不表示内容版本，全部组件的 storage.content_versions 与 raster.content_versions 沿既有版本契约绑定；本规范不以单个哈希代替完整组件版本，也不将本地契约测试视为真实预览或缓存闭环验收。

Manager 为冻结 COG 源组成，使用现有 Meta 精确授权事实批量查询及统一 client；`meta.authorization_facts.read` 只增加到 `tenant.manager_runtime`。接口按 Service Token 的租户上下文、精确指纹与既有数量／响应上限返回已提交身份及边界，不枚举 DataItem、不返回内容、不触发扫描。Manager 不能调用 System 专属的授权事实 revision 或节点／数据项清单接口，不能将机器事实视为用户读取许可；System 签发及消费执行授权时仍独立复核当前完整边界与原用户权限。

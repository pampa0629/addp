# audit_logs 表结构说明

更新日期：2026-08-01

状态：当前正式表契约。运行时查询 API 与中间件使用统一 IAM 审计模型，不保留旧 `user_id / username / request_body / query_params` 兼容字段。

## 一、定位

`system.audit_logs` 是 ADDP 的统一追加式审计事实表，也是 IAM 与 OAuth 安全事件的唯一持久化落点。它同时容纳：

- 身份、Membership、Role、授权版本、Token 撤销和 Bootstrap 等领域安全事件；
- OAuth 协议状态转换与安全失败；
- 各模块经过脱敏的通用 HTTP 操作事件。

不建立 IAM 专用表或 OAuth 专用表。访问日志、应用调试日志和错误堆栈不属于该表。

## 二、当前字段

| 字段 | 类型 | 约束 | 说明 |
| --- | --- | --- | --- |
| `id` | bigint identity | PK | 事件 ID |
| `principal_id` | bigint | nullable, FK -> `principals.id` | 行为 Principal；匿名或未建立身份时为空 |
| `principal_type` | text | nullable, `user \| service_principal` | Principal 类型快照，非空时必须与 Principal 一致 |
| `context_type` | text | nullable, `platform \| tenant` | 已建立的 AuthContext；空不是 Platform Context |
| `tenant_id` | bigint | nullable, FK -> `tenants.id` | Tenant Context 必填，其他情况为空 |
| `event_name` | text | not null | 稳定事件名 |
| `result` | text | not null | `succeeded \| failed \| denied \| ignored` |
| `risk_level` | text | not null | `low \| medium \| high \| critical` |
| `module_name` | text | not null | 事件 owner 模块 |
| `http_method` | text | nullable | HTTP 方法，非 HTTP 事件为空 |
| `resource_path` | text | nullable | 不含 query 的路由路径 |
| `http_status` | integer | nullable | `100..599` |
| `request_id` | text | nullable | 请求追踪 ID |
| `ip_address` | inet | nullable | 客户端 IP |
| `user_agent` | text | nullable | 客户端 User-Agent |
| `entity_type` | text | nullable | 业务实体类型 |
| `entity_id` | text | nullable | 业务实体稳定 ID |
| `details` | jsonb | not null, default `{}` | 结构化、脱敏且可供审计管理员读取的对象 |
| `created_at` | timestamptz | not null | 数据库生成的事件时间 |

约束关系：

```text
platform -> tenant_id is null
tenant   -> tenant_id is not null
null     -> tenant_id is null

principal_id is null -> principal_type is null
principal_id is set  -> principal_type is set and matches principals.principal_type
```

`context_type=NULL` 用于登录失败、OAuth 前置失败或纯内部安全事件，表示事件发生时不存在已建立的授权上下文。事件来源由 `module_name` 和 `event_name` 表达，不新增 `anonymous` 或 `internal` 会话模式。

## 三、写入与事务

身份、Membership、Role、授权版本、Token Family 撤销、Bootstrap 和 OAuth 状态转换必须在修改权威事实的同一个 PostgreSQL 事务内 INSERT 审计事件。审计写入失败时整个业务事务回滚。

通用 HTTP 操作事件可以在请求完成后写入，但不得替代领域安全事件。同一安全事实只产生一条领域事件，避免中间件和 Service 双写。

表只允许追加：目标 DDL 无条件拒绝 UPDATE / DELETE / TRUNCATE。归档可以先只读导出；需要物理保留清理时，必须先完成独立的分区、归档校验和数据库职责设计，不能给普通运行时路径预留开关。生产环境最终拆分 migration owner、runtime writer、audit reader 和 maintenance 角色。

## 四、脱敏白名单

`details` 采用白名单构造，不接受整段请求体、query 或错误对象。任何位置都禁止保存：

- 密码、密码 Hash、MFA Secret、Client Secret；
- Access Token、Refresh Token、Delegated Token、Resource Ticket；
- Authorization Code、Device Code、User Code；
- PKCE verifier/challenge、OAuth state；
- Cookie、Authorization Header；
- 原始 OAuth 请求体、原始 query、可能包含安全材料的错误堆栈。

OAuth 事件的 `details` 仅允许 `client_id`、`grant_type`、`decision`、`scope` 和稳定 `error_code`。事件名、结果和风险等级使用独立列。

## 五、索引

| 索引 | 字段与谓词 | 用途 |
| --- | --- | --- |
| `idx_audit_logs_created_at` | `(created_at DESC, id DESC)` | 全局时间线与稳定分页 |
| `idx_audit_logs_tenant_created_at` | `(tenant_id, created_at DESC, id DESC) WHERE tenant_id IS NOT NULL` | Tenant 审计时间线 |
| `idx_audit_logs_principal_created_at` | `(principal_id, created_at DESC, id DESC) WHERE principal_id IS NOT NULL` | Principal 行为追溯和 FK |
| `idx_audit_logs_event_created_at` | `(event_name, created_at DESC, id DESC)` | 安全事件检索 |
| `idx_audit_logs_request_id` | `(request_id) WHERE request_id IS NOT NULL` | 请求链路定位 |
| `idx_audit_logs_entity` | `(entity_type, entity_id, created_at DESC) WHERE entity_type IS NOT NULL AND entity_id IS NOT NULL` | 实体历史追溯 |
| `idx_audit_logs_high_risk_created_at` | `(risk_level, created_at DESC, id DESC) WHERE risk_level IN ('high', 'critical')` | 高风险事件时间线与告警扫描 |

## 六、查询授权

- 平台审计管理员：按独立 Permission 查询、导出平台和跨 Tenant 审计事实；只读，不能修改原始记录。
- Tenant 审计角色：只读取当前 Tenant 范围，必须使用 Tenant AuthContext。
- 平台系统管理员和平台安全管理员：不因管理职责自动获得原始审计内容读取权。
- 普通 User、Service Principal 和 owner 模块：仅能通过受控写入接口追加其职责内事件，不能直接枚举审计事实。

查询范围由 `principal_id + context_type + tenant_id` 和 Permission 决定，不再使用旧 `user_type` 或 `tenant_id=NULL` 推断全局权限。

导出是筛选结果的完整快照：必须按 `(created_at DESC, id DESC)` 稳定顺序分批读取全部匹配事件，不得因管理列表的 `page_size` 上限静默截断。响应头 `X-ADDP-Export-Count` 返回该快照实际写入文件的事件数，CSV 与 JSON 的数量语义保持一致。

## 七、相关规范

- [System IAM 数据模型与迁移规范](../IAM数据模型与迁移规范.md)
- [ADDP OAuth 授权规范](../../../docs/spec/addp%20OAuth授权规范.md)
- [ADDP 授权上下文规范](../../../docs/spec/addp授权上下文规范.md)

## 八、独立审计归档部署契约

System 唯一拥有事件语义和采集／核验实现。业务修改仍只写本表，不直接双写外部存储；模块运行日志、Loki 和 Trace 不替代审计。归档采集进程部署在与业务主机及共享 Infra 不同的管理域，凭据不进入根 `.env` 或业务服务。首期使用 PostgreSQL 15 `pgoutput` 单表提交流与独立 S3 Object Lock COMPLIANCE 存储，不引入 Kafka 或第二个可变审计数据库。部署必须显式给出独立桶、来源 UUID、复制槽／publication、保留天数和私有凭据文件；不自动创建桶、角色、publication，不自动切换端点或缩短保留期。

### 8.1 权限与保留

- 系统管理员只观察脱敏的连接、积压及容量，不读取正文或持有归档／密钥管理权限；安全管理员负责受控身份策略与轮换，不能单独自授审计读取；审计管理员只读核验与导出，不能写原始事件或降低保留。
- 业务 runtime、migration owner、复制采集身份、审计查询身份分别授权。采集需要的 REPLICATION 是敏感基础设施权限，publication 的表清单不是隔离整个数据库的安全边界；限定受控主机、TLS、网络和凭据，禁止复用业务账号。
- 原始归档只能由采集身份追加；查询身份只读原始归档，并仅能在自己的查询留痕前缀列举版本、指定版本读回和追加日志，以核清上传结果；不能列举或写入其他实名前缀。归档域独立配置将实名审计管理员与凭据、留痕前缀绑定，不信任命令参数或业务库角色行自行生成存储权限。首期受控离线核验入口仅给平台审计管理员；Tenant 现有查询仍限定 Tenant，不授予其原始归档凭据，不把在线源库列表宣称为独立存证。
- 存储策略必须拒绝覆盖、DeleteObject／DeleteObjectVersion、绕过保留、写入过短期限和更改默认保留。对象 Key、Version ID、摘要和期限共同标识证据；出现新版本、删除标记、链分叉、缺批次或来源身份变化均拒绝核验。
- 密钥和根管理凭据不由业务运维单独持有。采集停用、策略、密钥及存储迁移走系统／安全两人受控变更，审计管理员监督；不得解除既有保护。正文最低保护期已确认为365天；根记录、封段检查点和未来清理回执自各自存储创建时间起至少20个UTC日历年COMPLIANCE保护（向上取整到秒并留操作裕量）、只允许延长、首期不清理。运行身份不授予PutObjectLegalHold，也不靠请求条件表达“只准ON”；保留对外部保全状态的只读核验。首期仅实现只读到期计划，禁止删除和生命周期过期配置；真正批准、执行、回执和中断核清另验。

### 8.2 基线、提交与故障

首次显式初始化在逻辑复制槽导出的同一快照读取基线，并归档绑定源身份和一致位置的起点；只能建立一次，不接受无声明的新基线掩盖历史缺口。其后按事务 Commit End LSN 归档，禁止以递增 ID 或事件时间充当提交水位。普通回滚不产生成功归档。批次保存来源、序号、前批次 SHA256、提交位置、时间和完整脱敏字段；只有上传、指定版本读回、摘要及 COMPLIANCE／保留期限均确认后才反馈消费位置。重复投递恢复既有批次，不重复计数；上传确认丢失先读回核清，禁止覆盖旧证据。

超出对象、事务、查询或核验预算必须明确拒绝且不推进检查点，不能丢弃事件。复制槽失效、WAL 缺失、来源／时间线变化、模式变化、归档冲突或链断裂保持完整性未知，不重建槽或自动重置进度。复制槽 WAL 预算由部署管理员预设，监测磁盘、槽状态及积压，不能无限保留。

源端事务审计失败仍整体回滚；归档不可达只阻断采集和核验，不改变既有业务提交语义。正常归档延迟目标 60 秒、超过 5 分钟告警；无可靠观测时为未知，空闲源不能只按最后一条事件时间误报延迟。首期健康输出供外部监控消费，不创建任何定时任务。异步归档不承诺未归档窗口零丢失，也不证明完全被接管源端的真实性或完整性。

查询先验证完整证据链和指定版本，按结构化事件名／Tenant 有界筛选；返回基线、检查点、归档时间、数量和验证状态。受控 CLI 查询及导出的实名身份、条件、结果数量先持久化到独立查询留痕前缀，失败不输出正文；不保存凭据、原始自由查询或请求体。查询留痕是访问记录，不允许作为原始业务事件插入。

### 8.3 验收与实际边界

T1 验证链连续性、摘要、边界预算、查询隔离、持久化前不确认及错误不输出部分结果。T2 在标准入口创建的独占 PostgreSQL logical Service 与独占对象存储中验证乱序提交／回滚、重传／确认丢失、并发采集、断网续传、实际保留拒绝及查询留痕。新 CLI 的编译、包自动发现和 T2 编排须同次登记。不得连接或修改本地共享 PG、MinIO 或现有服务。

对象存储模拟测试不计真实 WORM 通过；自建 MinIO 即使 S3 协议保留测试通过，也不能证明持宿主 root／磁盘管理权的主体无法物理篡改。生产独立域、密钥隔离、提供方根权限及保留期限仍须在选定目标上做部署验收。真实保留测试仅用专用合成对象和事先确定的短保留期；未到期对象如实列为保留残留，不伪报清零，不为清理降低保留。

部署必须同时启用存储侧独立访问日志并保护其归档，用于核对绕过 CLI 的直接对象读取、拒绝和策略变更。CLI 的追加查询记录不是所有对象访问的完整证明；持有对象读取凭据的主体可以直接调用存储 API，不能据 CLI 测试通过宣称无旁路访问或完整查询审计。若项目要求任何正文读取都必须先由服务端审计，则需将凭据仅交给独立审计域的受控查询服务，不向人分发原始存储凭据；该额外入口及身份验证在实施前另行确认，首期不新增 HTTP 服务或复制业务权限模型。

### 8.4 分段、流式核验与只读到期计划（已确认）

唯一协议升级为 addp.audit-archive/v2，不保留v1采集或读取兼容分支；旧对象保持不动，不自动迁移、重建基线或换来源。旧根不接受为新协议根。已有v1存证交接须显式核清，不能把新初始化当历史连续证明。

首次初始化在同一个导出快照的只读事务内按id读取，逐分片上传；基线可跨多个段，不积存全部行。所有基线检查点完成后才条件创建唯一root.json，绑定最后基线检查点、来源和快照LSN。init输出根对象Key/Version/SHA256，由独立域固定到两类私有配置的root字段；collect/verify/query/plan均要求该可信根，不能只依赖可覆盖的latest。初始化不完整保留受保护孤立对象/槽并停止，不自动覆盖或重建。

分片软目标4MiB，提交缓冲最多5秒；仅完整事务合批，单事务文本内存估算8MiB、对象硬上限16MiB。段按UTC日界、累计128MiB或256分片先到者封存，均在事务边界；超大事务拒绝而不截断。每个分片绑定段号、序号、前摘要、前提交位置及完整事务范围。只有分片指定版本/摘要/实际COMPLIANCE期限读回后，才确认其末尾LSN。封段检查点条件创建、至少20年COMPLIANCE保护，清单包含所有分片的Key/Version/摘要/数量和前驱检查点的同类引用；下一段只接已确认前驱。控制对象创建后读回实际COMPLIANCE截止，须覆盖存储创建时间起20年；缺少创建时间、期限过短或当前已过期均为未知。运行身份完全禁止修改Legal Hold（含ON/OFF及伪造条件请求），实际策略单独验收。

封段前只延长本段全部分片到统一保护截止，不缩短已有期限；截止须覆盖封段确认后365×24小时并留操作/时钟裕量，读回全部期限成功才发布检查点。活动分片从归档起至少365天保护。源和采集节点时间必须受控；时钟或期限无法确认时停止，不猜测已到期。控制证据与正文期限分别计算，不把20年应用到正文。允许受控延长原版本保护，不能缩短或换模式；首期无续期定时任务。部署管理者须在依赖中的根/检查点到期前受控延长并核验；未确认延长不能继续宣称该链受保护。20年不是永久保全，不能用创建后经过若干年的较短剩余期限否定有效历史，但到期后不再接受为当前受保护锚点。

跨段序号连续、来源/模式不变、前驱Key/Version/SHA完全匹配，首尾事务提交位置有序；LSN不要求数值+1。按对象存储分页流式走检查点和分片，不保存全链正文/对象列表；全链64MiB和10000批限制删除，单对象/段及结果预算仍有效。查询结果最多1000条且累计16MiB，相关范围全部核验并追加查询留痕后才输出。时间预算耗尽是未完成/未知，不能输出部分正文或PASS；结果仅覆盖固定根至明确观察检查点，不承诺截至现在绝对最新。检查点列出的已过期正文可读回验证摘要，但单列为保护过期，不影响仍受保护段的核验；未到期缺失、未经合法清理的过期缺失、影子版本或其他矛盾均未知。本轮没有删除/有效清理回执写入路径，任何正文缺失都不能当合法清理。

plan仅在auditor身份执行，严格只读存储，输出元数据不输出正文，不创建计划/批准/访问回执对象，存储侧访问日志另验。先核验全链，再逐段检查实际RetainUntil/LegalHold；只输出最旧连续已封段、全部对象过期且再过24小时宽限、无保全/异常的段；遇到未到期、保全、活动段即停止，不能跳着删除。输出绑定可信根和观察检查点、逐对象版本/SHA/期限、计划摘要及生成时刻；计划不是执行授权，日后执行前必须双人批准精确计划并重新核验。最多256个候选段/4096个对象/4MiB输出，超限明确未完成而非截断。接口和CLI均无Delete/ReleaseHold，首期不生成清理回执。

验收沿现有System Go T1、build-audit-archive和Hosted-only test-system-audit-archive；覆盖大于8MiB基线、超过64MiB/10000批流式链、跨段反序提交/回滚、确认丢失、断网恢复、20年控制保护、禁止任何Legal Hold写入、期限延长及提前删除/缩短拒绝、过期内容与当前保护区间区分、只读计划零写入及清理身份越权拒绝。真实365天期限用实际读回/拒绝证明；时间边界另用可控时钟合成测试，不冒充已运行365天。生产独立域/全部访问日志/容量及未来清理均独立验收，不设定时任务。


### 8.5 内网独立日志接收端 B（已确认）

B 复用本模块的 S3 COMPLIANCE 读写适配，使用独立 `access/<source UUID>/` 命名空间及 `addp.audit-access/v1` 协议，不套用 PostgreSQL LSN 或重建事务基线。A/B 为不同虚拟机及数据盘，B 存储凭据不交给 A；业务三员无主机、虚拟化、磁盘、备份和根密钥权限。底层最高管理员仍在信任边界内。真实部署由 Infra 另行确认，首期只实现和隔离验证，不注册开发服务、不设置定时任务。

单一入口为独立进程 HTTPS `POST /api/v1/internal/audit-receipts`，只给固定来源的 MinIO 原生 audit webhook 调用，不进入 Gateway、Console、System 用户 API 或 Swagger。私有配置绑定来源 UUID、预期 MinIO deploymentid 和独立至少32字符 Bearer token；禁止根据正文、MinioDeploymentID 头或 URL 自选来源。TLS 必须校验证书，接收器不提供明文监听或代理头认证。按当前固定 MinIO 版本接收 JSON/NDJSON 对象流，单次最多4MiB/1000条；完整解析、字段验证和计数校验后才写入，非法、超限或不完整批次均不确认。不接受压缩请求；不静默截断。

只保存显式字段投影：版本、来源 deploymentid、时间、API 名称/桶/对象及多对象版本、requestQuery.versionId与responseHeader的X-Amz-Version-Id两个明确白名单版本字段、状态码/状态、请求ID、远端地址、非秘密 accessKey/parentUser 身份。除上述两个版本字段外的请求/响应头、query、claims、tags、任意错误正文和 User-Agent 不存储；完整原始字节只在有界内存计算摘要，既不落盘也不输出。投影是审计元数据，不含对象内容；资源名和身份仍属受控审计数据。不能把脱敏投影称为原始 HTTP 报文存证。

首次 `receiver-init` 条件创建20年保护根并输出 Key/Version/SHA，部署管理员固定到配置。`receive` 必须携带这个根启动。每次投递以认证来源及完整原始字节 SHA256 定位正文；同内容重送核清原版本，不按 requestID 去重，异内容不得覆盖。正文条件写入并读回确切版本/摘要/实际至少365天保护，孤立正文重试先将原版本延长并读回至可覆盖本次回执创建后至少365天，再创建带连续接收序号、前驱引用、正文引用、原投递摘要、数量和首次正文存储时间的20年回执，读回成功才返回204。无可变数据库或 B 第二份本地缓冲；写入确认丢失先核清，失败返回503，整个批次不确认。单正文对象先于回执的孤立写入由同内容重试接续，不删除。A 忽略响应体，因此回执保存在 B，不能声称 A 已核验回执。

B 同来源首期单进程串行接收；请求不排无界队列，繁忙返回503。条件写入和前驱检查使误开的并发进程冲突拒绝，不形成双路线。重启从固定根按键序流式核验回执和正文；每次接收核验既有链后再追加，内存不随总正文增长，15秒存储预算耗尽返回503不报成功。首期没有吞吐容量承诺，较长链的索引优化需保持同一证据协议。`receiver-verify` 为只读核验，输出观察末回执；可在配置固定最近外部保存的 checkpoint，缺失、倒退、分叉、影子版本或摘要错误均未知。正文保护到期前须受控延长；缺少有效延期时停止确认，不能把仍可读取当作仍受保护。仅固定根不能发现整个末尾被最高权限回滚；只有独立保管的较新检查点才能证明其前面的尾部未丢失。空链不是 A 无操作证明。首期不清理、不提供删除或 Legal Hold 写入，不自动建立替代根。

A 只获得 B 接收凭据；B writer 只列举/读回自己来源、条件追加正文/根/回执及允许延长期限，拒绝删除、缩短、Legal Hold 与桶管理；核验身份仅列举/读指定版本。正文至少365天，根和回执自存储创建起至少20个UTC日历年，允许受控延长。Loki/Alloy 仅可从已确认对象重建检索，故障不影响 B 接收；本轮不新增投影服务或给予 Loki 证据写权限。B 自身存储访问的旁路、主机及管理日志需由 Infra 独立收集，不能将 B 输出回送 B 造成递归。

A 原生队列不改造：内存尚未落盘、无 fsync 的落盘窗口、队列满、损坏条目或有限重试耗尽可能丢失；B 只能证明已确认范围。断网容量、队列告警及重启后的请求集合对账必须独立验收，无法证明的缺口标未知。禁止以 B 204、连续接收序号或 Loki 无报错宣称端到端零丢失。

T1 使用内存 Store/HTTP recorder 覆盖认证来源、字段投影、批次预算、读回失败、响应丢失重试、重启、并发冲突、影子/缺失/锚点倒退；既有 `make test-module MODULE=system GO_ONLY=1` 自动发现。T2 扩展既有 `make test-system-audit-archive`，只在 Hosted 独占 MinIO 验证真实版本/保护/权限、HTTP确认丢失与重启、存储不可达不确认。A 原生队列的崩溃/满盘已知边界不冒充修复；实际内网断网时长、全量访问覆盖及容量留给 Infra 部署验收。构建仍使用 `make build-audit-archive`，无新依赖、数据库或工作流。


B 保留拒绝的验收区分存储 WORM 约束和 IAM 条件：当前固定 MinIO `9e49d5e7a648f00e26f2246f4dc28e6b07f8c84a` 对尚在 COMPLIANCE 保护中的既有精确版本，先禁止缩短，再检查允许延期的策略，返回 HTTP 400 / `InvalidRequest` 及明确的 WORM 消息；不能把它误报为操作成功，也不能把任意非空错误当作保护成立。每个受验正文／根／回执版本须先读回成功延长后的实际模式和期限，再执行未来但明显过短的期限请求，记录安全的 S3 错误类型／码／状态／消息、Key／Version及前后期限；独立只读身份须确认同一精确版本、内容摘要、COMPLIANCE模式和延期后截止均未改变。网络错误、签名错误、其他400或500均不计通过。另对新正文／控制对象的过短保护请求明确验证403 / `AccessDenied`且未产生对象，保留两层保护的独立证据；不放宽策略或缩短期限。

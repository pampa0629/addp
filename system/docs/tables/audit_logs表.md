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
- 原始归档只能由采集身份追加；查询身份只读原始归档，并仅能在自己的查询留痕前缀追加日志。归档域独立配置将实名审计管理员与凭据、留痕前缀绑定，不信任命令参数或业务库角色行自行生成存储权限。首期受控离线核验入口仅给平台审计管理员；Tenant 现有查询仍限定 Tenant，不授予其原始归档凭据，不把在线源库列表宣称为独立存证。
- 存储策略必须拒绝覆盖、DeleteObject／DeleteObjectVersion、绕过保留、写入过短期限和更改默认保留。对象 Key、Version ID、摘要和期限共同标识证据；出现新版本、删除标记、链分叉、缺批次或来源身份变化均拒绝核验。
- 密钥和根管理凭据不由业务运维单独持有。采集停用、策略、密钥及存储迁移走系统／安全两人受控变更，审计管理员监督；不得解除既有保护。正文最低保护期已确认为365天；根记录、封段检查点和未来清理回执长期保全、首期不清理。首期仅实现只读到期计划，禁止删除和生命周期过期配置；真正批准、执行、回执和中断核清另验。

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

分片软目标4MiB，提交缓冲最多5秒；仅完整事务合批，单事务文本内存估算8MiB、对象硬上限16MiB。段按UTC日界、累计128MiB或256分片先到者封存，均在事务边界；超大事务拒绝而不截断。每个分片绑定段号、序号、前摘要、前提交位置及完整事务范围。只有分片指定版本/摘要/实际COMPLIANCE期限读回后，才确认其末尾LSN。封段检查点条件创建、长期Legal Hold=ON，清单包含所有分片的Key/Version/摘要/数量和前驱检查点的同类引用；下一段只接已确认前驱。控制对象保全权限只允许运行身份写ON、读状态，拒绝解除保全；实际策略单独验收。

封段前只延长本段全部分片到统一保护截止，不缩短已有期限；截止须覆盖封段确认后365×24小时并留操作/时钟裕量，读回全部期限成功才发布检查点。活动分片从归档起至少365天保护。源和采集节点时间必须受控；时钟或期限无法确认时停止，不猜测已到期。长期控制对象可在固定COMPLIANCE期限之外另加独立保全，运行身份不得解除。

跨段序号连续、来源/模式不变、前驱Key/Version/SHA完全匹配，首尾事务提交位置有序；LSN不要求数值+1。按对象存储分页流式走检查点和分片，不保存全链正文/对象列表；全链64MiB和10000批限制删除，单对象/段及结果预算仍有效。查询结果最多1000条且累计16MiB，相关范围全部核验并追加查询留痕后才输出。时间预算耗尽是未完成/未知，不能输出部分正文或PASS；结果仅覆盖固定根至明确观察检查点，不承诺截至现在绝对最新。检查点列出的已过期正文可读回验证摘要，但单列为保护过期，不影响仍受保护段的核验；未到期缺失、未经合法清理的过期缺失、影子版本或其他矛盾均未知。本轮没有删除/有效清理回执写入路径，任何正文缺失都不能当合法清理。

plan仅在auditor身份执行，严格只读存储，输出元数据不输出正文，不创建计划/批准/访问回执对象，存储侧访问日志另验。先核验全链，再逐段检查实际RetainUntil/LegalHold；只输出最旧连续已封段、全部对象过期且再过24小时宽限、无保全/异常的段；遇到未到期、保全、活动段即停止，不能跳着删除。输出绑定可信根和观察检查点、逐对象版本/SHA/期限、计划摘要及生成时刻；计划不是执行授权，日后执行前必须双人批准精确计划并重新核验。最多256个候选段/4096个对象/4MiB输出，超限明确未完成而非截断。接口和CLI均无Delete/ReleaseHold，首期不生成清理回执。

验收沿现有System Go T1、build-audit-archive和Hosted-only test-system-audit-archive；覆盖大于8MiB基线、超过64MiB/10000批流式链、跨段反序提交/回滚、确认丢失、断网恢复、保全和期限延长、过期内容与当前保护区间区分、只读计划零写入及清理身份越权拒绝。真实365天期限用实际读回/拒绝证明；时间边界另用可控时钟合成测试，不冒充已运行365天。生产独立域/全部访问日志/容量及未来清理均独立验收，不设定时任务。

# ADDP IAM owner 资源授权与 Asset 衔接设计

更新日期：2026-07-23

状态：技术设计已确认、运行时尚未实现。IAM 数据模型、Permission/Role 发布和 AuthContext v1 前置能力已经落地；本文只跟进 owner Resource Grant/Policy、Resource Scope Binding、最终访问判断和 Asset 授权申请履约，不修改当前数据库、API、Swagger 或运行代码。

已完成前置规范见 `docs/spec/addp授权上下文规范.md`、`docs/spec/addp权限与角色发布规范.md` 和 `system/docs/IAM数据模型与迁移规范.md`。

## 一、目标与边界

本文解决：

1. 功能 Permission 与资源实例授权如何组合；
2. 每个 owner 如何保存 Resource Grant、Explicit Deny 和 Scope Binding；
3. User、Service Principal、Department、Project Group 和 Role 主体集合如何匹配；
4. 单资源、列表、搜索、下载和 Tool 调用如何执行同一授权语义；
5. Asset 申请批准后如何让源 owner 产生真正可执行的 Grant；
6. 授权创建、撤销、过期、失败和跨服务重试如何保持可解释；
7. 共享 Go 类型和 owner 本地实现分别归属哪里。

本文不定义：

- System IAM Role / Permission 表；
- OAuth Scope 与 Permission 的完整映射目录；
- Asset 页面和各 owner 管理页面；
- Break-glass 审批细节；
- OPA、Casbin 或其他策略产品接入；
- Fosite 的 OAuth/OIDC Storage Adapter。

## 二、当前实现差距

当前 Asset 实现只适合作为迁移输入，不是目标授权契约：

| 当前事实 | 问题 | 目标 |
| --- | --- | --- |
| Application 请求接受 `applicant_id` | 客户端可以提交申请人，主体不是从 AuthContext 得出 | 自助申请固定使用当前 User Principal |
| Authorization 只有 `user_id + asset_id` | 不能表达 Service Principal、组织、Role、Permission 或 Scope | 使用结构化 Subject Selector、ResourceRef 和 Permission Key |
| `credential` 字段预留 API Token | 资源授权和凭据生命周期混在同一表 | 删除；凭据归 OAuth、Service Principal 或 Service 自身 |
| `is_active` boolean | 无法清楚表达 pending、revoked、expired 和失败 | 使用明确生命周期状态，过期按数据库时间判断 |
| Approval 直接写 Asset Authorization | 源 owner 没有创建或执行 Grant | Asset 批准后由源 owner 创建唯一生效 Grant |
| owner 路由只取 `user_id/tenant_id` | 没有 Permission、Scope、Deny 或 Resource Policy | 统一消费 AuthContext v1 和 owner Authorizer |
| Asset `source_module + source_reference` | `source_reference` 是无类型字符串 | 替换为结构化 Owner ResourceRef |

迁移时删除上述旧字段和旁路，不在新表中保留兼容列或双写逻辑。

## 三、核心技术决策

| 决策 | 结论 |
| --- | --- |
| 最终执行点 | 资源所属 owner 是唯一最终授权执行点，System、Gateway 和 Asset 不替 owner 作最终判断 |
| 存储边界 | 每个 owner 在自身 schema 保存 Resource Access Rule 和 Scope Binding；不建立 System 中央 ACL 大表 |
| 动作词汇 | Resource Grant 直接引用稳定 Permission Key，不创建第二套资源 action 字符串 |
| Allow / Deny | owner 本地使用单一 Resource Access Rule 模型；`effect=allow` 是 Grant，`effect=deny` 是 Explicit Deny |
| Policy | 第一阶段使用版本化 owner 代码和结构化资源字段；不引入任意表达式 DSL、OPA 或 Casbin |
| Subject | 支持 Principal、Department、Project Group 和 Role；User / Service Principal 统一使用 Principal ID |
| Scope Binding | Department / Project Group 与资源的关联单独保存，不等于 Permission 或 Grant |
| Deny 优先 | 任一匹配 Explicit Deny 都拒绝，不按数组顺序、Rule 创建时间或 Role 名称覆盖 |
| 所有权 | `owner_principal_id` 只是 owner Policy 的输入，不自动绕过 Role Permission |
| Asset | Asset 负责申请和审批，源 owner 创建并持有唯一生效 Grant；Asset Authorization 只记录履约状态和 owner Grant 引用 |
| 一致性 | 创建在 owner 确认前不生效；撤销在 owner 确认前不宣称完成；过期由 owner 使用数据库时间直接拒绝 |
| 查询 | 单资源决策和列表可见性使用同一 Policy 语义；禁止先查询全 Tenant 再由前端过滤 |
| 兼容策略 | 一次性替换 `user_id + asset_id + is_active` 软授权，不保留旧 Authorization 判断 |

## 四、统一资源引用

跨模块协议使用 Owner ResourceRef：

```json
{
  "owner_module": "manager",
  "resource_type": "data_item",
  "resource_id": "1842"
}
```

规则：

- `owner_module` 必须是稳定模块名，并与 Permission 目录中的 `owner_module` 一致；
- `resource_type` 是 owner 内稳定的小写 snake_case 类型，不直接使用表名或任意 URL；
- `resource_id` 是 owner 定义的规范字符串，可以承载 bigint、UUID、Fingerprint 或其他稳定原生 ID；
- `resource_id` 不允许使用名称、路径显示文本或客户端临时 ID；
- Tenant ID 不进入 ResourceRef，由当前 AuthContext 和 owner 资源事实共同验证；
- 只有 owner 能解析 ResourceRef，其他模块不得直接查询 owner 表推断资源存在性。

Owner 必须提供内部 `DescribeResource` 能力，将 ResourceRef 解析为 ResourceDescriptor：

```json
{
  "ref": {
    "owner_module": "manager",
    "resource_type": "data_item",
    "resource_id": "1842"
  },
  "tenant_id": "3",
  "owner_principal_id": "12",
  "lifecycle_status": "active",
  "policy_key": "manager.data_item/v1",
  "policy_version": "3",
  "scope_bindings": [
    {
      "type": "department",
      "id": "9",
      "include_descendants": false
    }
  ]
}
```

ResourceDescriptor 是 owner 内部授权投影，不是通用公开资源 DTO。不同 owner 可以加载自己的密级、发布状态和业务条件，但必须输出统一的 Tenant、所有者、生命周期、Policy 版本和 Scope Binding 语义。

## 五、Resource Scope Binding

Scope Binding 回答“哪个 Department / Project Group Scope 覆盖这个资源”，不回答“谁已获准访问”。每个 owner 在自身 schema 保存语义等价的 `resource_scope_bindings`：

| 字段 | 类型 | 约束 |
| --- | --- | --- |
| `id` | bigint identity | PK |
| `tenant_id` | bigint | 非空 |
| `resource_type` | text | 非空 |
| `resource_id` | text | 非空、owner 规范 ID |
| `scope_type` | text | `department | project_group` |
| `scope_id` | bigint | 对应 Department / Project Group ID |
| `include_descendants` | boolean | 只允许 Department；默认 false |
| `created_by_principal_id` | bigint | 非空 |
| `created_at` | timestamptz | 非空 |

约束：

- 唯一键为资源、Scope 类型、Scope ID；
- Project Group 的 `include_descendants` 必须为 false；
- owner 写入前通过 System 验证 Scope 属于当前 Tenant；
- 资源删除、迁移 Tenant 或关闭时在 owner 事务中同步删除 / 失效 Binding；
- Tenant Scope 不建立 Binding，资源自身 `tenant_id` 已表达 Tenant 边界；
- 一个资源可以绑定多个 Department / Project Group；
- 父子 Department 默认不继承，只有 Binding 显式 `include_descendants=true` 时才向下覆盖。

Role Assignment Scope 覆盖规则：

```text
Tenant Assignment
  -> 当前 Tenant 内资源均可成为 Permission 候选

Department Assignment D
  -> 资源存在 Department Binding D
  OR 资源 Binding A 显式 include_descendants=true，且 D 是 A 的后代

Project Group Assignment G
  -> 资源存在 Project Group Binding G
```

Scope 覆盖只让功能 Permission 成为候选，仍需 owner Policy 或 Resource Grant Allow，且不能覆盖 Explicit Deny。

## 六、Subject Selector

Resource Access Rule 的目标使用判别联合：

```json
{ "type": "principal", "principal_id": "12" }
```

```json
{
  "type": "department",
  "department_id": "9",
  "include_descendants": true
}
```

```json
{ "type": "project_group", "project_group_id": "17" }
```

```json
{ "type": "role", "role_key": "tenant.data_steward" }
```

匹配规则：

- `principal` 直接匹配 AuthContext Principal ID，User 和 Service Principal 不分两套字段；
- `department` 匹配当前 Tenant 的直接 Department Membership；`include_descendants=true` 时还可以匹配其祖先 ID；
- `project_group` 只匹配当前 Tenant 的直接 Project Group Membership；
- `role` 匹配当前 AuthContext 中有效 Role Assignment 的 `role_key`；
- Subject 所属 Tenant 必须等于资源和当前 Tenant；
- Platform Context 不匹配 Tenant Resource Rule；
- Department、Project Group 或 Role 不是 Principal，审计主体仍是当前 Principal。

不提供 `all_users`、`everyone` 或 Tenant 全体 Grant Subject。Tenant 内默认可见性由 owner Resource Policy 明确表达，避免用一条伪主体 Grant 隐藏公开范围。

## 七、Resource Access Rule 数据模型

每个 owner 在自身 schema 保存语义等价的两张表。表可以使用 owner 前缀，但字段语义和约束必须一致。

### 7.1 `resource_access_rules`

| 字段 | 类型 | 约束 |
| --- | --- | --- |
| `id` | bigint identity | PK |
| `tenant_id` | bigint | 非空 |
| `resource_type` | text | 非空 |
| `resource_id` | text | 非空 |
| `effect` | text | `allow | deny` |
| `subject_type` | text | `principal | department | project_group | role` |
| `subject_id` | bigint | Principal / Department / Project Group 时非空 |
| `subject_role_key` | text | Role 时非空 |
| `include_department_descendants` | boolean | 只允许 Department，默认 false |
| `status` | text | `active | revoked` |
| `valid_from` | timestamptz | 非空 |
| `valid_until` | timestamptz | 可空 |
| `source_type` | text | `manual | asset_authorization | break_glass` |
| `source_id` | text | 非空、来源内稳定 ID 或幂等键 |
| `source_version` | bigint | 非空、默认 1、只递增 |
| `created_by_principal_id` | bigint | 非空 |
| `revoked_by_principal_id` | bigint | 可空 |
| `revoked_at` | timestamptz | 可空 |
| `reason` | text | 非空 |
| `created_at` / `updated_at` | timestamptz | 非空 |

检查约束：

```text
principal     -> subject_id 非空，subject_role_key 为空，include_descendants=false
department    -> subject_id 非空，subject_role_key 为空
project_group -> subject_id 非空，subject_role_key 为空，include_descendants=false
role          -> subject_id 为空，subject_role_key 非空，include_descendants=false
```

其他约束：

- `valid_until > valid_from`；
- `status=revoked` 时 `revoked_at` 和 `revoked_by_principal_id` 非空；
- `unique (tenant_id, source_type, source_id)`，同一授权来源只物化一个 Rule；
- payload 在创建后不可修改；权限或 Subject 变化必须撤销旧 Rule 并创建新来源；
- 撤销是单向状态转换，不能把 revoked Rule 恢复为 active；
- `source_version` 只用于拒绝乱序创建 / 撤销命令，不允许覆盖既有 payload。

### 7.2 `resource_access_rule_permissions`

| 字段 | 类型 | 约束 |
| --- | --- | --- |
| `rule_id` | bigint | FK -> Resource Access Rule |
| `permission_key` | text | 非空、引用产品 Permission Key |
| `created_at` | timestamptz | 非空 |

主键为 `(rule_id, permission_key)`。owner 创建 Rule 时必须验证：

- Permission 存在、启用且 `owner_module` 等于当前 owner；
- Permission 允许 Tenant / Department / Project Group 业务 Scope；
- Subject 为 Role 时，每个 Permission 必须已属于当前 Tenant 中该有效 Role；不允许创建永远无法与 Role Permission 取交集的空规则；
- Asset 来源只能请求该 Asset 已发布 Offering 中允许的 Permission；
- Grant 不能创造 Role Permission；调用者仍必须从 AuthContext 获得同一 Permission。

Principal、Department 和 Project Group Subject 不在创建时绑定某个固定 Role Assignment；owner 只验证 Subject、Tenant 和 Permission 契约有效，运行时再与 Principal 当前 AuthContext 中的有效 Role Permission 和 Scope 取交集。Role、Permission 或组织关系失效后，历史 Rule 为审计保留但不再匹配，不跨 schema 级联删除。

### 7.3 生效条件

Rule 只有同时满足以下条件才参与判断：

```text
status = active
AND valid_from <= database_now
AND (valid_until IS NULL OR database_now < valid_until)
AND tenant/resource/subject/permission 匹配
```

过期不依赖后台任务。后台任务可以把过期 Rule 标记为归档或刷新展示状态，但不能成为拒绝访问的唯一执行点。

## 八、Resource Policy

Resource Policy 是 owner 代码和结构化领域事实，不是 Tenant 可提交的脚本或 JSON 表达式。每个 `policy_key` 必须版本化，并在代码审查中明确：

- 哪些生命周期状态允许 read / update / execute 等 Permission；
- owner Principal 是否获得哪些资源 Allow 候选；
- published、tenant_visible、private、sensitive 等领域字段如何影响 Allow；
- 父子资源是否继承 Scope Binding、Grant 或 Deny；
- 派生产物是否继承源资源授权；
- step-up、审批、用途、时间窗或密级条件；
- 删除、归档、下线和源资源不可用后的行为。

第一阶段不建立通用 Policy DSL，原因是 Manager Data Item、Service Endpoint、Asset Entry、Workflow 和模型对象的领域条件不同。通用 DSL 会把 owner 事实复制到中央策略层，并增加第二个无法由数据库约束的权限语言。

公共代码只负责：

- AuthContext、Permission、Scope 和 Subject 匹配；
- Resource Access Rule 生效时间与 Deny 优先；
- 统一 Decision、Reason Code、审计字段和测试套件；
- 调用 owner Policy Adapter 获取领域 Allow / Deny 条件。

## 九、最终决策算法

单资源请求固定按以下顺序执行：

```text
1. 验证 AuthContext Schema、Token 类型、Context 和 Tenant
2. 根据路由声明取得 required Permission
3. 校验 client audience / OAuth Scope / Delegated Tool 路由限制
4. owner 解析 ResourceRef，限定当前 Tenant 加载 ResourceDescriptor
5. 从 AuthContext 选择包含 required Permission 的有效 Role Assignment
6. 按 Resource Scope Binding 过滤不覆盖资源的 Assignment
7. 加载当前有效、匹配 Subject 和 Permission 的 Explicit Deny
8. 任一 Explicit Deny 命中 -> Deny
9. owner Policy Adapter 计算领域 Allow / Deny
10. 领域 Deny 命中 -> Deny
11. 匹配有效 Resource Grant 或 owner Policy Allow
12. 校验 step-up、业务审批和其他请求条件
13. Allow；否则默认 Deny
```

逻辑表达：

```text
Allow = AuthContext 有效
  ∩ Tenant 匹配
  ∩ Client audience / Scope 允许
  ∩ 至少一个 Role Assignment 含 required Permission
  ∩ 该 Assignment Scope 覆盖资源
  ∩ (Owner Policy Allow ∪ Resource Grant Allow)
  ∩ Step-up / Approval / Condition 允许
  ∩ 不存在 Explicit Deny
```

关键规则：

- Resource Grant 不能补齐缺失的 Role Permission；
- Tenant Scope Permission 也不能跳过 owner Allow；
- Role 名称只可作为 Resource Rule Subject，不可在业务代码中替代 Permission 判断；
- 资源不存在、属于其他 Tenant 或当前主体不可见时统一返回 404；
- 已知资源但功能 Permission、Scope 或 Policy 不允许时返回 403；
- Resource Access Ticket 和 Delegated Token 还必须通过各自的 Token 路由白名单。

## 十、列表、搜索和批量操作

逐条调用单资源 Authorizer 会产生 N+1 查询，也容易在分页后过滤导致数量泄露。每个 owner 必须实现与单资源决策等价的 Visibility Query：

```text
BuildVisibilityPredicate(AuthContext, required Permission, resource_type)
  -> owner Repository 查询条件
```

要求：

- Tenant、Scope Binding、有效 Allow、Explicit Deny 和 owner Policy 在数据库查询或等价索引中执行；
- `total`、分页、聚合和导出在授权过滤之后计算；
- 搜索索引文档必须携带 owner 生成的授权过滤字段和 Policy 版本，不能只按 Tenant 过滤；
- 索引授权投影延迟时，对详情读取再次执行 owner 数据库 Authorizer；
- 批量操作逐个资源判定并返回稳定结果，不能因其中一个资源允许就放行整批；
- 不把资源 ID 全集写入 AuthContext、Token、前端 Store 或 System IAM。

复杂 owner 可以实现专用 SQL / Search Filter Adapter，但必须通过共享一致性 fixture，证明与单资源 `Decide` 结果相同。

## 十一、共享 Go 契约

确认后在 `common/authorization/resource` 建立不依赖具体 owner 表的语义类型：

```go
type ResourceRef struct {
    OwnerModule string
    ResourceType string
    ResourceID string
}

type AccessRequest struct {
    Resource ResourceRef
    PermissionKey string
}

type Decision struct {
    Effect string
    ReasonCode string
    PermissionKey string
    AssignmentIDs []string
    RuleIDs []string
    PolicyKey string
    PolicyVersion string
}

type Authorizer interface {
    Decide(ctx context.Context, auth AuthContext, request AccessRequest) (Decision, error)
}
```

owner 提供：

- Resource Resolver：限定 Tenant 加载原生资源；
- Scope Binding Repository；
- Resource Access Rule Repository；
- Policy Adapter；
- Visibility Query Adapter；
- Asset Source Grant Command Handler。

`common` 不提供中央数据库 Repository，不查询 owner schema，不维护资源表注册中心。共享测试套件可以由各 owner 复用，但 owner 必须在自身模块装配依赖。

Python owner 使用与 Go 相同的 JSON Schema / fixture 定义 AccessRequest 和 Decision，不自行发明另一套 effect、reason 或 Subject 结构。

## 十二、owner 内部 Grant API

Asset 等授权来源通过 owner 的唯一内部 API 物化 Grant：

```text
POST /api/v1/{owner}/internal/resource-grants
GET  /api/v1/{owner}/internal/resource-grants/{grant_id}
POST /api/v1/{owner}/internal/resource-grants/{grant_id}/revocations
```

调用者必须是经过 System 验证、位于同一 Tenant Context 且具有专用 internal Permission 的 Service Principal。目标实现不接受共享 `INTERNAL_API_KEY`、User Token 代传或客户端提交的 `X-Tenant-ID` 绕过 Tenant Context。

每个 owner 在 Permission 清单保留以下不可定制、不可委托、只允许 internal 路由使用的能力：

```text
{owner}.resource_grant.create
{owner}.resource_grant.read
{owner}.resource_grant.revoke
```

例如 Manager 使用 `manager.resource_grant.create/read/revoke`。Asset Service Principal 在每个需要服务的 Tenant 具有显式 Tenant Membership 和专用内置 Role；调用时获取该 Tenant Context 的短期 Service Access Token。Platform Context、平台三员 Role 或一个全局共享密钥都不能代替这个 Tenant 绑定。

创建请求：

```json
{
  "source": {
    "type": "asset_authorization",
    "id": "9001",
    "version": "1"
  },
  "resource": {
    "owner_module": "manager",
    "resource_type": "data_item",
    "resource_id": "1842"
  },
  "subject": {
    "type": "principal",
    "principal_id": "12"
  },
  "permission_keys": ["manager.content.read"],
  "valid_from": "2026-07-23T00:00:00Z",
  "valid_until": "2026-08-22T00:00:00Z",
  "reason": "asset application 7001 approved"
}
```

成功返回 owner Grant：

```json
{
  "grant_id": "4402",
  "status": "active",
  "source_version": "1",
  "valid_from": "2026-07-23T00:00:00Z",
  "valid_until": "2026-08-22T00:00:00Z"
}
```

幂等规则：

- 相同 Tenant、source type 和 source ID 的相同 payload 重试返回同一 Grant；
- 同一 source version 的不同 payload 返回 409；
- 低于当前 source version 的命令返回 409；
- 创建前 owner 重新验证资源、Permission、Subject、Tenant 和有效期；
- 撤销使用 `version=current+1`，只允许 `active -> revoked`；重复撤销返回同一 revoked 结果；
- owner 不接受 Asset 直接写数据库或写入自报 Grant ID。

API 实施时必须补双语 Swagger、`x-addp-auth-mode=internal`、请求 / 响应 Schema、401 / 403 / 404 / 409 / 503 和路由覆盖测试。运行时使用稳定领域错误与 owner 模块 i18n key，不在 Handler 硬编码中英文消息。本设计阶段不修改真实路由，因此不生成 Swagger。

## 十三、Asset 授权衔接

### 13.1 Asset 是申请 owner，不是源资源 owner

Asset 保存数据资产条目、申请、审批和履约状态。对于 `owner_module != asset` 的资源：

- Asset 不直接决定源资源是否可读、可执行或可下载；
- `asset.authorizations` 不被源 owner 在请求时作为第二授权数据库直接查询；
- 真正 Allow 是源 owner 创建的 Resource Access Rule；
- Asset 只保存 owner Grant ID 和当前履约状态用于展示、撤销和对账。

当资源本身归 Asset 所有时，Asset 既是申请 owner 又是资源 owner，可以在同一数据库事务创建本地 Grant，但仍使用同一个 Authorizer 路径。

### 13.2 Asset ResourceRef 与 Offering

目标 Asset 使用结构化字段替换无类型 `source_reference`：

| 字段 | 含义 |
| --- | --- |
| `owner_module` | 源资源 owner |
| `resource_type` | owner 稳定资源类型 |
| `resource_id` | owner 规范资源 ID 字符串 |

每个已发布 Asset 还必须有版本化 Access Offering：

| 字段 | 含义 |
| --- | --- |
| `permission_key` | 可以申请的 owner Permission |
| `maximum_duration` | owner 允许的最大授权时长 |
| `approval_policy_key` | Asset 使用的审批策略 |
| `offering_version` | 申请和审批引用的不可变版本 |

Offering 由 owner 发布或经 owner 验证，Asset 管理员不能填入任意 Permission Key。Asset 下线或源资源不可用时停止新申请；已有 Grant 是否撤销由 Offering 和 owner 生命周期 Policy 明确决定。

### 13.3 自助申请

目标自助申请请求只包含：

```json
{
  "asset_id": "501",
  "permission_keys": ["manager.content.read"],
  "requested_valid_until": "2026-08-22T00:00:00Z",
  "reason": "project analysis"
}
```

约束：

- Applicant 固定为当前 AuthContext User Principal，body 不接受 `applicant_id`；
- Service Principal、Department、Project Group 或 Role Grant 由具有管理 Permission 的专用管理流程创建，不伪装成自助申请；
- Permission 必须属于当前 Asset Offering；
- 申请不授予 Role Permission，用户仍需具有对应功能 Permission；
- Requested Valid Until 不能超过 Offering 上限；不再用客户端 `duration_day` 作为权威时间；
- 同一 Principal、Asset、Permission 集合和有效期范围只能存在一个 pending 申请。

### 13.4 审批和激活

批准流程：

```text
Asset Reviewer 批准 Application
  -> Asset 事务锁定 pending Application
  -> 固化 Offering、Subject、ResourceRef、Permission 和有效期
  -> 创建 Authorization(status=pending_activation)
  -> 写入 Transactional Outbox
  -> 提交
  -> 调用源 owner Create Resource Grant
  -> owner 幂等创建 Allow Rule
  -> Asset 保存 owner_grant_id，Authorization -> active
```

规则：

- Application 审批状态与 Grant 履约状态分开，`approved` 不等于源资源已经可访问；
- owner 返回 active 前，Asset 和 Portal 必须显示“激活中”，不能显示“已授权”；
- owner 拒绝非法 Resource / Permission / Subject 时 Authorization 转 `failed` 并保留稳定失败原因；外部事实修复后可以重试同一不可变履约命令，若需改变 Subject、Permission 或有效期则必须创建新 Application；
- 网络超时不标记 failed，由 Outbox 重试相同 source ID；
- owner 已创建但响应丢失时，幂等重试返回同一 Grant，不产生重复 Allow；
- Asset API 对异步激活返回 202，只有 owner 已同步确认时才可返回 active 结果。

### 13.5 撤销

撤销流程：

```text
Asset 撤销请求
  -> Authorization active -> revocation_pending
  -> 写入 Outbox
  -> 调用 owner Revoke Grant
  -> owner Rule active -> revoked
  -> Asset Authorization -> revoked
```

Asset 只有收到 owner revoked 结果后才能宣称撤销完成。owner 不可用时返回 503 或 revocation pending，不能返回成功。Outbox 持续重试，管理界面必须显示仍可能有效。

Grant `valid_until` 由 owner 在每次决策中直接判断，因此即使 Asset Worker 停止，过期时间到达后访问也会被拒绝。Asset 后台任务只负责把展示状态收敛为 `expired`。

### 13.6 目标 `asset.authorizations`

目标表表达审批履约，不再充当软性 ACL：

| 字段 | 类型 | 约束 |
| --- | --- | --- |
| `id` | bigint identity | PK |
| `tenant_id` | bigint | 非空 |
| `application_id` | bigint | 非空、唯一 |
| `subject_type` | text | 第一阶段自助申请固定 `principal` |
| `subject_id` | bigint | Principal ID |
| `owner_module` | text | 非空 |
| `resource_type` | text | 非空 |
| `resource_id` | text | 非空 |
| `offering_version` | bigint | 非空 |
| `status` | text | `pending_activation | active | revocation_pending | revoked | expired | failed` |
| `owner_grant_id` | text | owner 创建成功后非空 |
| `valid_from` / `valid_until` | timestamptz | 非空 |
| `failure_code` | text | 可空、稳定代码 |
| `created_at` / `updated_at` | timestamptz | 非空 |

Permission 使用 `asset.authorization_permissions(authorization_id, permission_key)` 规范化保存。删除旧 `user_id`、`credential`、`is_active` 和“管理员直接写软授权”路径。

## 十四、Decision 与审计

owner 内部 Decision 至少包含：

```json
{
  "effect": "allow",
  "reason_code": "resource_grant_allow",
  "permission_key": "manager.content.read",
  "assignment_ids": ["402"],
  "rule_ids": ["4402"],
  "policy_key": "manager.data_item/v1",
  "policy_version": "3"
}
```

稳定 Reason Code：

| Code | 含义 |
| --- | --- |
| `context_mismatch` | Platform / Tenant Context 不匹配 |
| `client_limit_denied` | audience、OAuth Scope 或 Delegated 路由不允许 |
| `permission_missing` | AuthContext 没有 required Permission |
| `scope_not_cover_resource` | Permission Assignment Scope 不覆盖资源 |
| `explicit_deny` | 匹配有效 Explicit Deny |
| `resource_policy_deny` | owner 领域 Policy 拒绝 |
| `resource_grant_allow` | 匹配有效 Resource Grant |
| `resource_policy_allow` | owner 领域 Policy 允许 |
| `step_up_required` | 需要增强认证 |
| `approval_required` | 所需业务审批尚未完成 |
| `default_deny` | 没有任何 Allow 来源 |

审计记录至少包含 Principal、Context、Tenant、Client、Token 类型、Permission、ResourceRef、Assignment ID、Rule ID、Policy 版本、Decision、Reason Code、Request ID 和时间。普通应用日志不得输出完整 AuthContext、Rule reason、Token 或敏感资源属性。

对外错误继续遵守 ADDP API 规范：不可见或跨 Tenant 资源返回 404，已知资源的权限 / Policy 拒绝返回 403，step-up 使用 `error_type=step_up_required`，owner 授权基础设施不可用返回 503。

## 十五、缓存和失效

第一阶段单资源决策直接读取 owner 数据库，不跨请求缓存 Resource Access Rule 或 Policy Decision。原因：

- Grant 撤销和 Explicit Deny 必须立即生效；
- owner 资源生命周期和 Scope Binding 会独立变化；
- AuthContext 第一阶段同样不跨请求缓存，先建立正确基线。

搜索索引中的可见性字段是查询投影，不是最终授权缓存。以后引入 Decision Cache 时必须包含：

```text
Principal authorization_version
+ Resource policy_version
+ Resource Access Rule version
+ Scope Binding version
+ Permission Key
+ Token client limits digest
```

并具有可靠失效事件。不能保留“缓存失败时改走旧 user_id/tenant_id 判断”的旁路。

## 十六、安全与禁止事项

- 禁止 System 保存所有 owner Resource Rule；
- 禁止 Gateway 按 URL 或 Role 名称推断资源访问；
- 禁止 Resource Grant 提升 Role Permission；
- 禁止 Asset Authorization 被 owner 作为第二套实时 ACL 直接查询；
- 禁止客户端提交 Applicant、Principal、Tenant、Grant ID 或审批人并被信任；
- 禁止 `owner_principal_id`、`created_by`、Department leader 等字段自动获得全功能权限；
- 禁止用 `tenant_id` 相同替代 Resource Policy；
- 禁止 Department 父子、Project Group、父子资源或派生产物默认继承授权；
- 禁止在 Rule 中存任意可执行表达式、SQL、脚本或未校验 JSON Policy；
- 禁止 owner Grant 保存 API Token、密码或 Credential；
- 禁止授权列表在分页后过滤或由前端隐藏；
- 禁止异步创建 / 撤销尚未得到 owner 确认时向用户宣称已生效。

## 十七、测试矩阵

### 17.1 Subject 和 Scope

- Principal 精确匹配 User / Service Principal；
- Department 直接成员和显式 descendants 匹配；默认父子不匹配；
- Project Group 只匹配直接成员，不传播 Department；
- Role Subject 只匹配当前 Context 有效 Assignment；
- 其他 Tenant Subject、Binding、Rule 全部不可见；
- Platform Context 不访问 Tenant Resource；
- Tenant / Department / Project Group Assignment 覆盖规则正确。

### 17.2 Allow / Deny

- 有 Permission 无 owner Allow 时默认拒绝；
- 有 Grant 无 Permission 时拒绝；
- Policy Allow、Grant Allow、所有权 Allow 均仍受 Permission 和 Scope 限制；
- 任一 Explicit Deny 覆盖所有 Allow；
- Rule 未到 `valid_from`、已过 `valid_until` 或 revoked 时不生效；
- Role、Scope 或 Resource 生命周期变化立即影响下一次判断。

### 17.3 查询一致性

- 单资源 Decide 与列表 Visibility Query 对同一 fixture 结果相同；
- total、分页、聚合、搜索和导出不包含不可见资源；
- Deny 后资源从列表消失且详情返回 404 / 403 符合接口语义；
- 搜索索引延迟时详情仍由数据库 Authorizer 阻断；
- 批量请求不能利用一个 Allow 资源带出其他资源。

### 17.4 Asset

- body 中提交 `applicant_id`、Tenant 或任意 Permission 被拒绝；
- Offering 外 Permission 不能申请；
- Approval 只创建 pending activation，不提前 Allow；
- 相同 source ID 重试只产生一个 owner Grant；
- owner 创建成功但响应丢失时可幂等恢复；
- owner 拒绝、超时和不可用状态可区分；
- revocation pending 不显示为已撤销；
- owner 确认撤销后访问立即拒绝；
- Worker 停止时 Grant 到期仍由 owner 数据库时间拒绝；
- `credential`、软授权和 owner 直查 Asset ACL 的旧路径已删除。

### 17.5 API 和审计

- Internal Grant API 只接受同 Tenant Service Principal；
- source version 乱序和 payload 冲突返回 409；
- 跨 Tenant / 不可见资源不泄露存在性；
- Decision 审计包含 Assignment、Rule 和 Policy 版本；
- 日志不记录 Token、完整 AuthContext 或敏感 Rule reason；
- Swagger 授权元数据、真实路由和 owner Permission Manifest 及聚合目录一致。

## 十八、实施顺序

OAuth/Fosite、Permission Manifest、AuthContext、System IAM 数据模型和版本化 migration 前置能力已经完成。owner 资源授权按以下单一路径实施：

1. 实现 `common/authorization/resource` 语义类型和共享测试套件；
2. 选择 Manager Data Item 作为第一个 owner vertical slice，实现单资源、列表和 Resource Ticket；
3. 改造 Asset Offering、Application、Authorization 和 owner Grant Outbox；
4. 一次性迁移其他 owner，并删除全 Tenant 可见、软授权和 owner 直查 Asset ACL 分支；
5. 同步 Swagger、前端、审计和在线 E2E 后切换运行路径；
6. 把稳定契约并入正式规范并删除本文。

不允许先实现 Asset 新授权表、再等待 owner 接入；那会形成第二套暂时性 ACL。

## 十九、已确认的技术决策

以下决策已确认，后续设计和实现不得重新引入并行路线：

1. **Owner 最终执行**：Resource Grant / Policy 和最终访问判断只归资源 owner，不建立 System 中央 ACL。
2. **统一动作词汇**：Resource Rule 直接引用 Permission Key，不建立第二套 resource action。
3. **Scope Binding**：Department / Project Group 与资源的绑定独立保存，只决定 scoped Permission 是否覆盖资源，不直接 Allow。
4. **单一 Rule 模型**：owner 本地 `resource_access_rules + permissions` 同时表达 Allow Grant 和 Explicit Deny，Deny 永远优先。
5. **Subject Selector**：支持 Principal、Department、Project Group、Role；Department descendants 必须显式开启。
6. **Policy 实现**：第一阶段采用 owner 版本化代码和结构化字段，不引入 OPA、Casbin 或任意 Policy DSL。
7. **所有权边界**：Resource Owner、Creator 或组织 leader 仍需 Role Permission，不形成隐藏管理员权限。
8. **查询一致性**：列表 / 搜索使用与单资源相同的 Visibility Policy，分页和统计在授权过滤后计算。
9. **Asset 边界**：Asset 只拥有申请、审批和履约；源 owner 的 Resource Grant 是唯一生效 Allow。
10. **Asset 主体**：自助申请主体固定为当前 User Principal，删除客户端 `applicant_id`；组织和 Service Principal Grant 使用管理流程。
11. **Asset Offering**：可申请 Permission 和最大有效期由 owner 验证的版本化 Offering 决定，Asset 管理员不能输入任意 Permission。
12. **跨服务一致性**：创建在 owner 确认前不生效，撤销在 owner 确认前不宣称完成，使用幂等 source、Transactional Outbox 和对账收敛。
13. **凭据分离**：删除 Asset Authorization `credential`，Resource Grant 不保存 API Token 或登录凭据。
14. **首阶段缓存**：不跨请求缓存 owner Decision / Rule；搜索授权字段只是查询投影，详情仍执行 owner Authorizer。
15. **内部 Grant Permission**：每个 owner 使用 `{owner}.resource_grant.create/read/revoke` 保护内部 Grant API，只授予同 Tenant Service Principal，不使用共享 Internal API Key。
16. **Role Subject 校验**：Role 类型 Resource Rule 只能引用该 Role 已包含的 Permission；其他 Subject 在运行时与当前有效 Role Permission 和 Scope 取交集。

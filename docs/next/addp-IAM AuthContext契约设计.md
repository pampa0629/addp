# ADDP IAM AuthContext 契约设计

更新日期：2026-07-23

状态：技术设计已确认。本文在已确认的 IAM 目标数据模型和 Permission / Role 矩阵之上，确定 AuthContext JSON Schema、登录上下文选择、浏览器上下文切换、授权版本失效和共享类型边界；不修改当前 API、Swagger、数据库或运行代码。

## 一、目标与边界

本文解决：

1. System 向业务模块返回什么 AuthContext；
2. Platform 与 Tenant Context 如何互斥表达；
3. User、Service Principal、第一方 Web、OAuth 和 Delegated Token 如何使用同一语义；
4. 多 Tenant 登录如何在签发 Token Family 前选择上下文；
5. 浏览器如何切换 Platform / Tenant Context；
6. Role、Membership 或组织关系变化后，旧 Token 如何立即失效；
7. Go、Python 和前端共享类型分别由谁维护；
8. 错误响应和契约测试覆盖什么。

本文不定义：

- owner Resource Grant / Policy 表和最终资源判定接口；
- Permission 与 OAuth Scope 的逐项映射；
- MFA、Passkey、Service Principal Credential 的具体协议；
- Fosite Storage Adapter 或 OAuth/OIDC 内部实现。

这些内容必须消费本文契约，不能另建第二套主体、Tenant 或权限上下文。

## 二、核心技术决策

| 决策 | 结论 |
| --- | --- |
| Schema 版本 | 根字段固定为 `schema_version=addp.auth_context/v1`；不按客户端返回不同结构 |
| ID 表达 | IAM bigint ID 在 JSON 中统一使用非零十进制字符串，避免 JavaScript Number 精度丢失 |
| Principal | 只返回 `user` 或 `service_principal` 及稳定 Principal ID；不返回用户名、邮箱或 Local Account |
| Context | 使用 `platform | tenant` 判别联合；Platform 不携带 Tenant 字段，Tenant 必须携带 Tenant Membership |
| Permission 投影 | 只返回当前上下文有效 Role Assignment，每个 Assignment 显式携带 Permission、Scope、来源和有效期 |
| 组织投影 | 只返回当前 Tenant 的直接 Department / Project Group Membership；Department 额外返回祖先 ID 供 owner 显式判断 |
| 第一方客户端 | Web 固定 `client_id=addp-web`、`audiences=[addp.api]`、`scope_mode=unrestricted`；不再用 `client_id=null` 表示第一方 |
| OAuth 限制 | OAuth 和 Delegated Token 使用 `scope_mode=restricted`；Scope 只能缩小 Role Permission |
| 授权失效 | Token 记录的签发版本必须等于 Principal 当前 `authorization_version`；第一阶段不跨请求缓存 AuthContext |
| 上下文切换 | 切换创建新 Browser Token Family 并撤销旧 Browser Family；不原地修改 Family 上下文 |
| Family 隔离 | 浏览器切换不撤销既有 CLI / OAuth Family；OAuth Family 永久绑定批准时上下文 |
| 兼容策略 | 一次性替换当前 `subject_type/user_id/username/user_type/tenant_id` 平铺结构，不保留双 Schema |

## 三、根对象

目标响应示意：

```json
{
  "schema_version": "addp.auth_context/v1",
  "principal": {},
  "context": {},
  "authentication": {},
  "client": {},
  "organization": {},
  "authorization": {},
  "token": {},
  "delegation": null
}
```

字段责任：

| 字段 | 含义 | 事实来源 |
| --- | --- | --- |
| `schema_version` | 共享契约版本 | System 代码 |
| `principal` | 当前被授权主体 | `principals` |
| `context` | 当前互斥会话模式 | Token Family + Tenant Membership |
| `authentication` | 本次认证强度和 step-up 事实 | 登录 / IdP / Credential Session |
| `client` | 客户端、audience 和 Scope 上限 | Token / OAuth Client / Tool Manifest |
| `organization` | 当前 Tenant 的直接组织关系 | Department / Project Group Membership |
| `authorization` | 当前 Role Assignment 与授权版本 | Principal、Role、Permission、Assignment |
| `token` | 当前令牌类型与生命周期 | Access Token / Ticket |
| `delegation` | Agent Tool 委托审计绑定 | Delegated Access Token；其他令牌为 `null` |

AuthContext 不返回：

- `username`、email、显示名、头像等用户资料；这些由 `/users/me` 提供；
- Local Account、External Identity、密码或 MFA Credential；
- 其他 Tenant Membership、其他上下文 Role 或可访问资源全集；
- owner Resource Grant / Policy、Asset 申请单或 Explicit Deny 全集；
- Refresh Token、Token Hash、Family ID 或内部数据库状态。

## 四、JSON Schema

目标 Schema 使用 JSON Schema Draft 2020-12。实现时将本节落为仓库内唯一机器可读 Schema，并生成或校验 Go、Python、TypeScript 类型；不得手工维护三套可漂移定义。

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://addp.local/schemas/auth-context-v1.json",
  "title": "ADDP AuthContext v1",
  "type": "object",
  "additionalProperties": false,
  "required": [
    "schema_version",
    "principal",
    "context",
    "authentication",
    "client",
    "organization",
    "authorization",
    "token",
    "delegation"
  ],
  "properties": {
    "schema_version": { "const": "addp.auth_context/v1" },
    "principal": { "$ref": "#/$defs/principal" },
    "context": { "$ref": "#/$defs/context" },
    "authentication": { "$ref": "#/$defs/authentication" },
    "client": { "$ref": "#/$defs/client" },
    "organization": { "$ref": "#/$defs/organization" },
    "authorization": { "$ref": "#/$defs/authorization" },
    "token": { "$ref": "#/$defs/token" },
    "delegation": {
      "oneOf": [
        { "type": "null" },
        { "$ref": "#/$defs/delegation" }
      ]
    }
  },
  "$defs": {
    "id": {
      "type": "string",
      "pattern": "^[1-9][0-9]*$"
    },
    "principal": {
      "type": "object",
      "additionalProperties": false,
      "required": ["type", "id"],
      "properties": {
        "type": { "enum": ["user", "service_principal"] },
        "id": { "$ref": "#/$defs/id" }
      }
    },
    "context": {
      "oneOf": [
        {
          "type": "object",
          "additionalProperties": false,
          "required": ["type"],
          "properties": {
            "type": { "const": "platform" }
          }
        },
        {
          "type": "object",
          "additionalProperties": false,
          "required": ["type", "tenant_id", "tenant_membership_id"],
          "properties": {
            "type": { "const": "tenant" },
            "tenant_id": { "$ref": "#/$defs/id" },
            "tenant_membership_id": { "$ref": "#/$defs/id" }
          }
        }
      ]
    },
    "authentication": {
      "type": "object",
      "additionalProperties": false,
      "required": ["methods", "assurance_level", "authenticated_at", "step_up_expires_at"],
      "properties": {
        "methods": {
          "type": "array",
          "minItems": 1,
          "uniqueItems": true,
          "items": {
            "enum": [
              "password",
              "totp",
              "webauthn",
              "external_idp",
              "recovery_code",
              "service_secret",
              "private_key_jwt",
              "mtls"
            ]
          }
        },
        "assurance_level": {
          "enum": ["aal1", "aal2", "aal3", "not_applicable"]
        },
        "authenticated_at": {
          "type": "string",
          "format": "date-time"
        },
        "step_up_expires_at": {
          "oneOf": [
            { "type": "null" },
            { "type": "string", "format": "date-time" }
          ]
        }
      }
    },
    "client": {
      "type": "object",
      "additionalProperties": false,
      "required": ["client_id", "audiences", "scope_mode", "scopes"],
      "properties": {
        "client_id": {
          "type": ["string", "null"],
          "minLength": 1
        },
        "audiences": {
          "type": "array",
          "minItems": 1,
          "uniqueItems": true,
          "items": { "type": "string", "minLength": 1 }
        },
        "scope_mode": {
          "enum": ["unrestricted", "restricted"]
        },
        "scopes": {
          "type": "array",
          "uniqueItems": true,
          "items": { "type": "string", "minLength": 1 }
        }
      },
      "allOf": [
        {
          "if": {
            "properties": { "scope_mode": { "const": "unrestricted" } },
            "required": ["scope_mode"]
          },
          "then": {
            "properties": { "scopes": { "maxItems": 0 } }
          },
          "else": {
            "properties": { "scopes": { "minItems": 1 } }
          }
        }
      ]
    },
    "organization": {
      "type": "object",
      "additionalProperties": false,
      "required": ["departments", "project_groups"],
      "properties": {
        "departments": {
          "type": "array",
          "items": { "$ref": "#/$defs/departmentMembership" }
        },
        "project_groups": {
          "type": "array",
          "items": { "$ref": "#/$defs/projectGroupMembership" }
        }
      }
    },
    "departmentMembership": {
      "type": "object",
      "additionalProperties": false,
      "required": ["membership_id", "department_id", "membership_type", "relation_role", "ancestor_ids"],
      "properties": {
        "membership_id": { "$ref": "#/$defs/id" },
        "department_id": { "$ref": "#/$defs/id" },
        "membership_type": { "enum": ["primary", "additional"] },
        "relation_role": { "enum": ["member", "leader"] },
        "ancestor_ids": {
          "type": "array",
          "uniqueItems": true,
          "items": { "$ref": "#/$defs/id" }
        }
      }
    },
    "projectGroupMembership": {
      "type": "object",
      "additionalProperties": false,
      "required": ["membership_id", "project_group_id", "relation_role"],
      "properties": {
        "membership_id": { "$ref": "#/$defs/id" },
        "project_group_id": { "$ref": "#/$defs/id" },
        "relation_role": { "enum": ["member", "leader", "coordinator"] }
      }
    },
    "authorization": {
      "type": "object",
      "additionalProperties": false,
      "required": ["authorization_version", "role_assignments"],
      "properties": {
        "authorization_version": { "$ref": "#/$defs/id" },
        "role_assignments": {
          "type": "array",
          "items": { "$ref": "#/$defs/roleAssignment" }
        }
      }
    },
    "roleAssignment": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "assignment_id",
        "role_key",
        "scope",
        "permissions",
        "source_type",
        "valid_from",
        "valid_until"
      ],
      "properties": {
        "assignment_id": { "$ref": "#/$defs/id" },
        "role_key": {
          "type": "string",
          "pattern": "^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)+$"
        },
        "scope": { "$ref": "#/$defs/assignmentScope" },
        "permissions": {
          "type": "array",
          "minItems": 1,
          "uniqueItems": true,
          "items": {
            "type": "string",
            "pattern": "^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*){2}$"
          }
        },
        "source_type": {
          "enum": ["manual", "idp_mapping", "bootstrap", "break_glass"]
        },
        "valid_from": {
          "type": "string",
          "format": "date-time"
        },
        "valid_until": {
          "oneOf": [
            { "type": "null" },
            { "type": "string", "format": "date-time" }
          ]
        }
      }
    },
    "assignmentScope": {
      "oneOf": [
        {
          "type": "object",
          "additionalProperties": false,
          "required": ["type"],
          "properties": { "type": { "const": "platform" } }
        },
        {
          "type": "object",
          "additionalProperties": false,
          "required": ["type", "tenant_id"],
          "properties": {
            "type": { "const": "tenant" },
            "tenant_id": { "$ref": "#/$defs/id" }
          }
        },
        {
          "type": "object",
          "additionalProperties": false,
          "required": ["type", "tenant_id", "department_id"],
          "properties": {
            "type": { "const": "department" },
            "tenant_id": { "$ref": "#/$defs/id" },
            "department_id": { "$ref": "#/$defs/id" }
          }
        },
        {
          "type": "object",
          "additionalProperties": false,
          "required": ["type", "tenant_id", "project_group_id"],
          "properties": {
            "type": { "const": "project_group" },
            "tenant_id": { "$ref": "#/$defs/id" },
            "project_group_id": { "$ref": "#/$defs/id" }
          }
        }
      ]
    },
    "token": {
      "type": "object",
      "additionalProperties": false,
      "required": ["type", "issued_at", "expires_at"],
      "properties": {
        "type": {
          "enum": [
            "first_party_access_token",
            "oauth_access_token",
            "service_access_token",
            "resource_access_ticket",
            "delegated_access_token"
          ]
        },
        "issued_at": { "type": "string", "format": "date-time" },
        "expires_at": { "type": "string", "format": "date-time" }
      }
    },
    "delegation": {
      "type": "object",
      "additionalProperties": false,
      "required": ["delegated_by_client_id", "agent_run_id", "tool_call_id"],
      "properties": {
        "delegated_by_client_id": { "type": "string", "minLength": 1 },
        "agent_run_id": { "type": "string", "minLength": 1 },
        "tool_call_id": { "type": "string", "minLength": 1 }
      }
    }
  }
}
```

### 4.1 Schema 之外的交叉约束

System 生成响应时还必须验证：

- User 进入 Platform Context 时 `assurance_level` 至少为 `aal2`；Service Principal 只能持有 `human_only=false` 的 Platform Role；
- Platform Context 的组织数组为空，Role Assignment Scope 只能为 Platform；
- Tenant Context 中所有 Tenant、Department 和 Project Group ID 必须属于当前 Tenant；
- Department 与 Project Group 只包含当前有效的直接 Membership；
- Service Principal 的 Department 数组始终为空；
- `first_party_access_token` 和 `resource_access_ticket` 的 `client_id` 固定为 `addp-web`；
- `oauth_access_token` 必须具有真实 OAuth `client_id`，且 `scope_mode=restricted`；
- `delegated_access_token` 必须只有一个 owner audience、非空 Scope 和非空 `delegation`；
- 非 Delegated Token 的 `delegation` 必须为 `null`；
- `resource_access_ticket` 只允许 System AuthContext 基础设施和对应 owner 的白名单 GET / HEAD 路由消费；
- `issued_at < expires_at`，`step_up_expires_at` 不得晚于当前 Token Family 的最终有效期。

### 4.2 排序和确定性

响应必须稳定排序：

- `methods`、`audiences`、`scopes`、每个 Assignment 的 `permissions` 按字典序；
- Department 按 `department_id`，祖先按根到父节点顺序；
- Project Group 按 `project_group_id`；
- Role Assignment 按 `scope.type + scope ID + role_key + assignment_id`。

稳定排序用于契约测试、审计对比和缓存摘要，不表示任何优先级。Allow 合并与 Explicit Deny 优先规则不依赖数组顺序。

## 五、典型响应

### 5.1 Platform User

```json
{
  "schema_version": "addp.auth_context/v1",
  "principal": { "type": "user", "id": "10" },
  "context": { "type": "platform" },
  "authentication": {
    "methods": ["password", "totp"],
    "assurance_level": "aal2",
    "authenticated_at": "2026-07-22T08:00:00Z",
    "step_up_expires_at": "2026-07-22T08:15:00Z"
  },
  "client": {
    "client_id": "addp-web",
    "audiences": ["addp.api"],
    "scope_mode": "unrestricted",
    "scopes": []
  },
  "organization": { "departments": [], "project_groups": [] },
  "authorization": {
    "authorization_version": "42",
    "role_assignments": [
      {
        "assignment_id": "301",
        "role_key": "platform.system_administrator",
        "scope": { "type": "platform" },
        "permissions": ["platform.operation.read", "platform.tenant.read"],
        "source_type": "bootstrap",
        "valid_from": "2026-07-01T00:00:00Z",
        "valid_until": null
      }
    ]
  },
  "token": {
    "type": "first_party_access_token",
    "issued_at": "2026-07-22T08:00:00Z",
    "expires_at": "2026-07-22T08:15:00Z"
  },
  "delegation": null
}
```

### 5.2 Tenant User

```json
{
  "schema_version": "addp.auth_context/v1",
  "principal": { "type": "user", "id": "12" },
  "context": {
    "type": "tenant",
    "tenant_id": "3",
    "tenant_membership_id": "28"
  },
  "authentication": {
    "methods": ["external_idp"],
    "assurance_level": "aal1",
    "authenticated_at": "2026-07-22T08:00:00Z",
    "step_up_expires_at": null
  },
  "client": {
    "client_id": "addp-web",
    "audiences": ["addp.api"],
    "scope_mode": "unrestricted",
    "scopes": []
  },
  "organization": {
    "departments": [
      {
        "membership_id": "71",
        "department_id": "9",
        "membership_type": "primary",
        "relation_role": "member",
        "ancestor_ids": ["4"]
      }
    ],
    "project_groups": [
      {
        "membership_id": "92",
        "project_group_id": "17",
        "relation_role": "member"
      }
    ]
  },
  "authorization": {
    "authorization_version": "42",
    "role_assignments": [
      {
        "assignment_id": "402",
        "role_key": "tenant.data_viewer",
        "scope": { "type": "tenant", "tenant_id": "3" },
        "permissions": ["manager.content.read", "manager.data_item.read"],
        "source_type": "manual",
        "valid_from": "2026-07-01T00:00:00Z",
        "valid_until": null
      }
    ]
  },
  "token": {
    "type": "first_party_access_token",
    "issued_at": "2026-07-22T08:00:00Z",
    "expires_at": "2026-07-22T08:15:00Z"
  },
  "delegation": null
}
```

### 5.3 Tenant Service Principal

Service Principal 不伪装成 User，不返回用户认证强度，也不进入 Department：

```json
{
  "schema_version": "addp.auth_context/v1",
  "principal": { "type": "service_principal", "id": "88" },
  "context": {
    "type": "tenant",
    "tenant_id": "3",
    "tenant_membership_id": "106"
  },
  "authentication": {
    "methods": ["private_key_jwt"],
    "assurance_level": "not_applicable",
    "authenticated_at": "2026-07-22T08:00:00Z",
    "step_up_expires_at": null
  },
  "client": {
    "client_id": null,
    "audiences": ["addp.api"],
    "scope_mode": "unrestricted",
    "scopes": []
  },
  "organization": {
    "departments": [],
    "project_groups": [
      {
        "membership_id": "143",
        "project_group_id": "17",
        "relation_role": "member"
      }
    ]
  },
  "authorization": {
    "authorization_version": "7",
    "role_assignments": [
      {
        "assignment_id": "611",
        "role_key": "tenant.pipeline_executor",
        "scope": { "type": "tenant", "tenant_id": "3" },
        "permissions": ["orchestrator.workflow.execute"],
        "source_type": "manual",
        "valid_from": "2026-07-01T00:00:00Z",
        "valid_until": null
      }
    ]
  },
  "token": {
    "type": "service_access_token",
    "issued_at": "2026-07-22T08:00:00Z",
    "expires_at": "2026-07-22T08:05:00Z"
  },
  "delegation": null
}
```

`service_access_token` 只定义 AuthContext 令牌语义，不预先决定使用 OAuth Client Credentials、私有签名还是其他 Credential 交换协议；该协议进入 Fosite ADR 和 Service Principal Credential 设计。

### 5.4 Delegated Tool Token

```json
{
  "schema_version": "addp.auth_context/v1",
  "principal": { "type": "user", "id": "13" },
  "context": {
    "type": "tenant",
    "tenant_id": "3",
    "tenant_membership_id": "29"
  },
  "authentication": {
    "methods": ["password", "totp"],
    "assurance_level": "aal2",
    "authenticated_at": "2026-07-22T08:00:00Z",
    "step_up_expires_at": "2026-07-22T08:15:00Z"
  },
  "client": {
    "client_id": "addp-cli",
    "audiences": ["develop"],
    "scope_mode": "restricted",
    "scopes": ["workflow.run"]
  },
  "organization": { "departments": [], "project_groups": [] },
  "authorization": {
    "authorization_version": "17",
    "role_assignments": [
      {
        "assignment_id": "491",
        "role_key": "tenant.data_engineer",
        "scope": { "type": "tenant", "tenant_id": "3" },
        "permissions": ["develop.task.execute"],
        "source_type": "manual",
        "valid_from": "2026-07-01T00:00:00Z",
        "valid_until": null
      }
    ]
  },
  "token": {
    "type": "delegated_access_token",
    "issued_at": "2026-07-22T08:03:00Z",
    "expires_at": "2026-07-22T08:05:00Z"
  },
  "delegation": {
    "delegated_by_client_id": "addp-cli",
    "agent_run_id": "7a9f43a7-81f0-4cb4-b545-6bfef53ed922",
    "tool_call_id": "call_abc123"
  }
}
```

Delegated Token 继承源 Principal、Context、认证事实和授权版本。它只缩小 audience、Scope、有效期和允许路由，不能更换 Principal、Tenant 或扩张 Permission。

## 六、登录上下文选择

### 6.1 何时选择

System 完成账号和 MFA 验证后，计算当前可进入的上下文：

- 有有效 Platform Role Assignment 时，产生一个 Platform 选项；
- 每个有效 Tenant Membership 产生一个 Tenant 选项；
- 没有选项时拒绝登录；
- 只有一个选项时可以直接签发绑定该上下文的 Browser Token Family；
- 多于一个选项时不得签发业务 Access Token，必须返回 Context Selection Ticket。

Platform 选项不能被静默转换为任一 Tenant，Tenant 选项也不能自动激活平台权限。

### 6.2 Context Selection Ticket

票据约束：

- 随机值前缀固定为 `addp_cst_`，随机部分不少于 32 字节；
- 只向当前登录客户端返回一次，System 只保存 SHA-256 Hash；
- 默认有效期 5 分钟，只能成功消费一次；
- 绑定 Principal、认证方法、认证强度、认证时间和可选上下文快照；
- 绑定第一方 Web 登录事务，不是 Access Token、Refresh Token 或 OAuth Authorization Code；
- 不写 Cookie、URL、浏览器历史、持久化存储、日志或审计详情；
- 票据消费时重新校验 Principal、Membership、Platform Assignment 和 MFA，不信任快照继续有效；
- 选择成功后在单个事务中标记已消费并创建 Token Family；并发消费只有一个成功。

唯一目标端点为：

```text
POST /api/v1/system/auth/context-selections
Authorization: Bearer <addp_cst_...>
```

该端点只接受 Context Selection Ticket，不接受 User Access Token、Refresh Token 或 OAuth Token。不得保留把 `tenant_id` 直接附加到 `/login`、`/refresh` 或 `/oauth/token` 的旁路。

目标登录响应使用判别字段，不把 Selection Ticket 塞进 Access Token 响应：

```json
{
  "next_action": "select_context",
  "selection_ticket": "addp_cst_<opaque>",
  "expires_in": 300,
  "contexts": [
    { "type": "platform" },
    {
      "type": "tenant",
      "tenant_id": "3",
      "tenant_membership_id": "28",
      "tenant_code": "research",
      "tenant_name": "Research"
    }
  ]
}
```

显示用 Tenant code / name 只出现在选择响应，不进入 AuthContext 安全判断。选项固定 Platform 在前，Tenant 按 `tenant_code + tenant_id` 排序。

目标选择请求只提交选项的判别键：

```json
{
  "context": {
    "type": "tenant",
    "tenant_membership_id": "28"
  }
}
```

客户端不提交 `principal_id`、Role、Permission、`authorization_version` 或可被信任的 `tenant_id`。System 从 Membership 重新解析 Tenant。

### 6.3 OAuth 与 Device Flow

OAuth Authorization Code 和 Device Flow 的批准结果继承用户批准时的当前上下文：

- 浏览器尚未选择上下文时，先完成 Context Selection，再显示授权页；
- Authorization Request 不允许提交 `tenant_id` 或 Platform Role；
- Authorization Code、Device Code 批准结果和后续 Token Family 固化当前上下文；
- 浏览器之后切换 Tenant 不改变已经签发的 CLI / OAuth Family；
- CLI 要进入另一个上下文时必须重新执行用户授权，不提供修改既有 Family 上下文的接口。

## 七、Browser Context Switch

### 7.1 唯一路线

已登录浏览器通过当前 Access Token 获取可选上下文，然后提交目标 Platform 或 Tenant Membership。切换请求必须同时具有：

- 当前有效的第一方 Web Access Token；
- 当前 Browser Refresh Token Cookie；
- 与两者相同的 Token Family；
- 目标上下文仍有效；
- 进入 Platform Context 所需的 `aal2` 或更高认证事实。

唯一目标端点为：

```text
GET  /api/v1/system/auth/context-options
POST /api/v1/system/auth/context-switches
```

两个端点都要求第一方 Web Access Token；切换端点还要求同一 Family 的 Refresh Token Cookie。CLI / OAuth Client 不得调用该端点改变自己的 Family Context。

若进入 Platform Context 时当前认证强度不足，System 返回 step-up 要求；完成 MFA 后继续同一个受控切换事务，不允许前端伪造新的认证强度。

### 7.2 原子切换

切换成功必须在一个数据库事务中：

1. 锁定当前 Browser Token Family；
2. 复核 Principal、当前 Family、目标 Context 和授权版本；
3. 撤销当前 Browser Family、Access Token 和 Resource Access Ticket；
4. 创建绑定目标 Context 的新 Browser Family；
5. 签发新 Access Token、Refresh Token Cookie 和各 owner Resource Access Ticket；
6. 写入包含旧 / 新 Context、认证强度和 Request ID 的审计事件；
7. 提交后返回新内存 Access Token。

不允许在原 Family 上 UPDATE `context_type` 或 `tenant_membership_id`。事务失败时旧 Family 保持有效，不出现半切换状态。

### 7.3 多标签页和 iframe

`common-frontend` 继续使用 `addp-auth-session` BroadcastChannel：

- 切换成功页广播 `context_changed` 和新内存 Access Token；
- 其他标签页清除旧 Token、更新当前 Context 并重新加载上下文相关数据；
- Console 向受信任 iframe 推送新 Token，iframe 不自行切换或消费 Refresh Cookie；
- 旧请求收到 401 时不得用已撤销 Family 循环刷新；
- 业务 Store、查询缓存和当前 Tenant 资源缓存必须按 Context Key 清空或隔离。

Web Locks 的上下文切换锁固定为 `addp-auth-context-switch`。切换与 refresh 必须遵循统一锁顺序，避免同一 Family 同时刷新和切换。

### 7.4 Family 隔离

撤销范围只包括发起切换的 Browser Family。以下会话保持原上下文和有效期：

- 同一 User 已批准的 `addp-cli` OAuth Family；
- 其他浏览器、设备或 Profile 的独立 Family；
- 从其他源 Family 签发的 Delegated Token。

若授权事实本身变化并递增 `authorization_version`，则属于全局授权失效，不受上述 Family 隔离限制。

## 八、授权版本与失效

### 8.1 版本来源

`principals.authorization_version` 是每个 Principal 单调递增的 bigint。Token Family 和 Access Token 保存 `issued_authorization_version`；AuthContext 返回当前版本的十进制字符串。

解析 Token 时必须满足：

```text
token.issued_authorization_version == principal.authorization_version
```

不满足时按 Token 已失效处理，不返回旧权限的部分 AuthContext。

### 8.2 必须递增版本的事件

- Principal suspend / deactivate / reactivate；
- Tenant Membership 创建后的首次激活、暂停、关闭、恢复或有效期变化；
- Role Assignment 授予、撤销、Scope 变化或有效期变化；
- Role Permission 集合变化时，对所有受影响 Principal 递增；
- Role、Permission 被 disable；
- Department / Project Group Membership 加入、结束、关系变化；
- Department 停用、Project Group 关闭或组织层级变化影响授权 Scope；
- IdP Mapping 或即时供应策略改变现有 Principal 的有效 Role；
- Break-glass Grant 创建、撤销或过期处理改变通用 Role Assignment。

只修改 User 显示名、头像、Tenant 显示名称等不影响授权的资料字段时不递增版本。

### 8.3 事务要求

授权变更事务固定顺序：

1. `SELECT Principal FOR UPDATE`；
2. 写入或撤销授权事实；
3. `authorization_version = authorization_version + 1`；
4. 撤销该 Principal 受影响的 Token Family 和派生票据；
5. 写入审计事件；
6. 提交。

第一阶段不跨请求缓存 AuthContext，因此提交后下一次解析立即读取新事实。以后若引入缓存，缓存 Key 必须至少包含 Token Hash 与签发授权版本，并由同一事务后的可靠失效事件驱动；不得引入“缓存命中时不查版本”的第二路径。

### 8.4 时间失效

临时 Assignment 到达 `valid_until` 不依赖后台任务才能失效。每次生成 AuthContext 都以数据库当前时间过滤：

```text
valid_from <= now AND (valid_until IS NULL OR now < valid_until)
```

后台任务可以负责撤销 Family 和整理状态，但不能成为权限过期的唯一执行点。

## 九、共享契约归属

确认后建立唯一机器可读文件：

```text
common/authorization/schemas/auth-context-v1.schema.json
```

职责分配：

| 位置 | 职责 |
| --- | --- |
| `common/authorization` | JSON Schema、Permission 清单、生成器和跨语言契约测试事实源 |
| `system/backend` | 查询 IAM 事实、验证交叉约束并生成 AuthContext |
| `common/middleware/auth` | 解析并注入不可变 Go AuthContext，提供 Principal / Context / Permission / Scope helper |
| `common-python/addp_common/auth.py` | 解析同一 Schema 为不可变 Python 类型 |
| `common-frontend` | 只读 TypeScript 类型、上下文选择和 Browser AuthSession；不自行计算后端授权 |
| Gateway | 验证入口是否已认证和基础 audience；不从 URL 推断业务 Permission |
| owner 模块 | 使用 Permission 常量、Assignment Scope 和 Resource Policy 做最终判断 |

生成代码不得提交 `UserType`、`is_super_admin`、`tenant_id=0` 或兼容旧字段的 helper。前端可以根据 Permission 投影隐藏不可用入口，但后端仍是安全执行点。

## 十、错误语义

沿用 ADDP `{error}` 格式，并通过可选 `error_type` 提供稳定机器语义；生产环境不返回内部失效原因。Request ID 使用统一响应头和审计字段，不在各模块另造格式。

| 场景 | HTTP | `error_type` | 对外语义 |
| --- | ---: | --- | --- |
| Access Token 缺失、无效、过期、撤销或授权版本不匹配 | 401 | `authentication_required` | 未登录或会话已失效 |
| Context Selection Ticket 无效、过期或已消费 | 401 | `context_selection_invalid` | 登录选择已失效，请重新登录 |
| 登录后没有可用 Context | 403 | `context_unavailable` | 当前账号没有可进入的工作空间 |
| 请求选择不属于当前 Principal 的 Context | 403 | `context_forbidden` | 无权进入所选工作空间 |
| 当前 Context 与路由要求不匹配 | 403 | `context_mismatch` | 当前工作空间不能执行该操作 |
| Role Permission / OAuth Scope / audience 不足 | 403 | `permission_denied` | 无权执行该操作 |
| 需要更强 MFA 或 step-up 已过期 | 403 | `step_up_required` | 需要完成增强认证 |
| 跨 Tenant 资源或不可见资源 | 404 | `resource_not_found` | 资源不存在 |
| System AuthContext 不可用或响应不符合 Schema | 503 | `authorization_service_unavailable` | 授权服务暂时不可用 |

示例：

```json
{
  "error": "需要完成增强认证",
  "error_type": "step_up_required"
}
```

System 审计必须区分 expired、revoked、version_mismatch、membership_inactive 等内部原因，但业务调用方一律只得到稳定 401，避免泄露主体或安全状态。

## 十一、安全和禁止事项

- 不从 query、body、header 中信任 `principal_id`、Tenant、Membership、Role、Permission 或授权版本；
- 不把 Platform Context 表达为 `tenant_id=null` 的全租户权限；
- 不把 `scope_mode=unrestricted` 解释为绕过 Role Permission，它只表示没有 OAuth Scope 额外收窄；
- 不把 Department 祖先列表解释为默认继承，owner 必须显式启用包含子部门策略；
- 不把 `organization` 当作 Resource Grant；
- 不因前端隐藏按钮而跳过 owner 后端校验；
- 不记录完整 AuthContext、Token、Selection Ticket 或敏感组织信息到普通日志；
- 不允许 Delegated Token 再次委托；
- 不允许 Resource Access Ticket 访问普通 System 或 owner CRUD API；
- 不在旧 Schema 上追加新字段形成过渡结构，不提供版本协商或兼容开关。

## 十二、测试矩阵

### 12.1 Schema 与共享类型

- Platform、Tenant User、Service Principal、OAuth、Resource Ticket 和 Delegated 示例通过 JSON Schema；
- 未知字段、数值 ID、零 ID、非法时间、重复 Scope 或非法 Permission Key 被拒绝；
- Go、Python、TypeScript 使用同一 fixture，序列化结果一致；
- `additionalProperties=false` 防止后端静默增加未同步字段；
- 数组排序和时间格式稳定。

### 12.2 Context 隔离

- Platform 响应不包含 Tenant、Department 或 Project Group；
- Tenant 响应只包含当前 Tenant Assignment 和组织关系；
- 其他 Tenant 的 Assignment 即使有效也不进入响应；
- Service Principal 不出现 Department；
- Platform Role 不自动产生 Tenant Permission；
- Department / Project Group Scope 不自动转成 Tenant Scope。

### 12.3 登录和切换

- 零、一个、多个 Context 分别走拒绝、直接签发、Selection Ticket；
- Ticket 过期、重放和并发消费只有稳定失败；
- 选择 Tenant 时只信任 Membership ID 并回查 Tenant；
- 低于 AAL2 进入 Platform 时要求 step-up；
- refresh 与 context switch 并发时只有一个 Family 状态转换成功；
- 切换后旧 Browser Token、Refresh Token 和 Resource Ticket 全部失效；
- 其他 CLI / OAuth Family 保持批准时 Context；
- BroadcastChannel 和 iframe 收到切换事件后不继续使用旧 Token。

### 12.4 授权失效

- Membership、Assignment、Role Permission 和组织关系变化在同一事务递增版本；
- 旧版本 Token 返回统一 401，不返回部分 AuthContext；
- 临时 Assignment 过期无需后台任务即可从 AuthContext 消失；
- User 资料修改不误撤销 Family；
- Explicit Deny、owner Resource Grant 和 OAuth Scope 仍在 owner 决策链生效。

### 12.5 协议和错误

- First-party Web 固定 `addp-web + addp.api + unrestricted`；
- OAuth Token 固定真实 Client、批准 Scope 和 `restricted`；
- Delegated Token 只有一个 owner audience，且审计绑定完整；
- AuthContext Schema 解码失败时 owner 返回 503，不降级为匿名或旧字段解析；
- 401、403、404 和 503 符合 ADDP API 规范且不泄露跨 Tenant 资源。

## 十三、规范同步与实施范围

本文确认后已同步正式规范中的目标契约：

1. `docs/spec/addp授权上下文规范.md` 已以 v1 Schema 替换“后续确定”描述；
2. `docs/spec/addp登录认证的统一要求.md` 已加入 Selection Ticket 与原子 Context Switch；
3. `docs/spec/addp OAuth授权规范.md` 已把第一方 `client_id=null` 改为 `addp-web`，并统一 audience 为 `addp.api`；
4. IAM 目标数据模型已加入 Context Selection Ticket 临时记录及索引约束。

后续在 owner 接口设计完成、Fosite ADR 通过后，再一次性实现 Schema、数据库和调用方迁移。实现时同步 Swagger、前端调用方和所有契约测试，并删除旧 AuthContext。

## 十四、已确认的技术决策

以下决策已确认，后续设计和实现不得重新引入并行路线：

1. **Schema 结构**：采用 `addp.auth_context/v1` 强类型嵌套结构，不保留旧平铺字段。
2. **ID 编码**：所有 IAM bigint ID 在 JSON 中使用非零十进制字符串。
3. **Principal 投影**：AuthContext 不返回 username / email / Local Account，只返回 Principal 类型和 ID。
4. **第一方语义**：第一方 Web 固定 `client_id=addp-web`、audience `addp.api` 和 `scope_mode=unrestricted`。
5. **Permission 投影**：按有效 Role Assignment 返回 Permission、Scope、来源和有效期，不只返回扁平 Permission 并集。
6. **组织投影**：返回当前 Tenant 直接 Membership；Department 携带祖先 ID 但默认不继承权限。
7. **上下文选择**：多个 Context 时使用 5 分钟、单次消费的 `addp_cst_` Selection Ticket，选择前不签发业务 Token。
8. **浏览器切换**：原子撤销旧 Browser Family 并创建新 Family；不得原地修改上下文。
9. **Family 隔离**：浏览器切换不影响已有 CLI / OAuth Family，授权事实版本变化才跨 Family 失效。
10. **授权版本**：第一阶段每次解析回查版本和当前事实，不跨请求缓存 AuthContext。
11. **共享事实源**：确认后以 `common/authorization/schemas/auth-context-v1.schema.json` 生成或校验 Go、Python、TypeScript 类型。
12. **错误语义**：沿用 `{error}`，使用可选稳定 `error_type` 区分选择、上下文、权限、step-up 和服务不可用。

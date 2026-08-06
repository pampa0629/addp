# ADDP 权限与角色发布规范

更新日期：2026-08-01

状态：正式规范。本文定义 Permission、Role、Role Assignment、Scope、模块 Manifest、发布期聚合和路由授权声明的唯一规则。

## 一、目标与边界

Permission 回答“Principal 是否允许使用一类产品能力”，Role 是 Permission 的命名组合，Role Assignment 把 Role 赋予 Principal 并限定 Scope。三者都不代替资源 owner 的最终资源访问判断。

本文定义：

- Permission 和内置 Role 的事实归属；
- Permission Key、动作词和生命周期规则；
- Platform、Tenant、Department、Project Group Scope；
- HTTP、Tool 和其他入口的授权声明；
- Manifest 聚合、代码生成、SQL migration 和发布门禁；
- 平台三员、Tenant 管理角色、业务角色与 Runtime Role 的稳定边界。

本文不定义：

- AuthContext JSON 结构，见 `docs/spec/addp授权上下文规范.md`；
- owner Resource Grant、Policy、Explicit Deny 和 Asset 履约；
- OAuth Scope 的协议细节，见 `docs/spec/addp OAuth授权规范.md`；
- 单个业务资源的可见性和行级过滤算法。

## 二、唯一事实源

| 事实 | 唯一来源 |
| --- | --- |
| 模块 Permission | `<owner>/authorization/permissions.yaml` |
| 内置 Role 及其 Permission 集合 | `system/authorization/builtin_roles.yaml` |
| Manifest Schema 和聚合逻辑 | `common/authorization` |
| owner 本地 Permission 常量 | 聚合器根据 owner Manifest 生成的文件 |
| HTTP 授权声明 | 真实路由 Guard 与 Swagger/OpenAPI 扩展 |
| Tool 授权声明 | `common-python/addp_common/tools/manifest.json` |
| 运行时 Permission、Role 目录 | System IAM migration 写入的 PostgreSQL 表 |
| Tenant 自定义 Role | System IAM API 和 PostgreSQL 运行时事实 |

System 不维护第二份手写业务 Permission 总表。业务模块也不得在启动时动态注册 Permission，或在 Handler 中发明 Manifest 不存在的 Key。

文档中的 Role 和 Permission 例子只解释规则，不是精确目录。精确目录必须读取上述 Manifest，避免文档快照与发布产物漂移。

## 三、Permission Manifest

每个具有公开能力的 owner 模块必须维护一个 Manifest：

```yaml
schema_version: addp.permission_manifest/v1
owner_module: manager
manifest_version: 1
permissions:
  - key: manager.data_item.read
    name_i18n_key: permissions.manager.data_item.read.name
    description_i18n_key: permissions.manager.data_item.read.description
    risk_level: low
    allowed_role_types: [tenant_builtin, tenant_custom]
    status: active
```

强制规则：

1. `owner_module` 必须等于稳定模块名；
2. `manifest_version` 为递增正整数，内容变化时必须递增；
3. Permission Key 在全平台唯一，发布后不得改名复用；
4. 删除能力时把 Permission 置为 `disabled`，并通过向前 migration 收缩目录和 Role；
5. 名称和描述使用 i18n key，不在 Manifest 中保存单语展示文案；
6. Manifest 不包含数据库 ID、路由路径、用户或租户实例数据。

## 四、Permission 命名

Permission Key 固定为：

```text
<owner>.<resource>.<action>
```

三段都使用小写 snake_case。禁止通配符、角色名、租户 ID、资源实例 ID、URL 或 UI 页面名。

常用动作：

| 动作 | 语义 |
| --- | --- |
| `read` | 查看资源或目录 |
| `create` | 创建资源 |
| `update` | 修改资源 |
| `delete` | 删除资源 |
| `execute` | 执行查询、任务、工作流或计算 |
| `cancel` | 取消执行 |
| `approve` / `reject` | 作出业务审批决定 |
| `revoke` | 撤销授权、邀请或凭据 |
| `publish` / `offline` | 发布或下线业务对象 |
| `export` | 导出数据或审计结果 |
| `initialize` | 为既有对象建立一次性初始管理关系 |
| `restore` / `suspend` / `close` | 恢复、暂停或终止生命周期 |

同一语义不得同时使用 `manage`、`operate`、`admin` 等宽泛动作绕过精确声明。确实存在独立聚合能力时，必须先明确资源和风险边界。

## 五、Role

Role 只组合现有 Permission，不产生新 Permission，也不表达资源实例 ACL。

内置 Role 分为：

- Platform User Role：平台系统管理员、安全管理员、审计管理员等实名用户角色；
- Platform Runtime Role：平台控制面 Service Principal 的最小机器角色；
- Tenant Management Role：Tenant Administrator、Infrastructure Administrator、Auditor 等租户治理角色；
- Tenant Business Role：数据工程、治理、资产、服务、图谱、AI 等产品能力组合；
- Tenant Runtime Role：模块在单个 Tenant 中运行所需的 Service Principal 角色。

平台三员 Role 只允许 User Principal，必须互斥，不存在全权合并角色。Runtime Role 只允许 Service Principal，不得授予 User。具体 `allowed_principal_types`、`allowed_scope_types` 和 Permission 集合以 `system/authorization/builtin_roles.yaml` 为准。

普通 User 的本地密码和 MFA Credential 重置分别使用 `iam.local_account.reset` 与 `iam.mfa_credential.reset`，只授予 Platform Security Administrator。两者都不得作用于任何有效 Platform Role 持有人；平台三员凭据整体失效时只允许离线灾难恢复，不能扩大普通用户重置 Permission 绕过三员治理。

Tenant 自定义 Role：

1. 只能选择 `allowed_role_types` 包含 `tenant_custom` 的 active Permission；
2. 不能创建任意 Permission 字符串；
3. 不能包含平台管理、平台 Runtime 或 owner 内部 Grant Permission；
4. Role 更新必须递增受影响 Principal 的 `authorization_version`；
5. 删除前必须处理现有 Assignment，不允许静默留下悬空授权。

## 六、Role Assignment 与 Scope

Role Assignment 绑定 Principal、Role、Scope、来源、有效期和生命周期状态。Scope 类型固定为：

- `platform`：只在 Platform Context 生效；
- `tenant`：只在指定 Tenant Context 生效；
- `department`：只在当前 Tenant 的指定 Department 范围内生效；
- `project_group`：只在当前 Tenant 的指定 Project Group 范围内生效。

Platform Context 只投影 Platform Assignment，不携带 Tenant、Department 或 Project Group。Tenant Context 只投影当前 Tenant 内的有效 Assignment。

Department 与 Project Group Scope 使 Permission 成为候选能力，不自动授予所有资源。owner 必须继续结合资源归属、Scope Binding、Grant、Policy、Explicit Deny 和资源状态完成最终判断。

多个有效 Assignment 的 Allow Permission 可以合并；显式 Deny 属于 owner 资源策略，不编码为反向 Permission。OAuth Scope 只能进一步缩小候选 Permission，不能扩大 Role Assignment。

Role Assignment 的写入服务必须在持久化前校验目标 Principal 的类型是否包含在 Role 的 `allowed_principal_types` 中，不得把数据库约束错误作为正常业务校验路径。主体类型与 Role 不兼容时返回 `409 Conflict` 和稳定 `error_code=role_assignment_principal_type_not_allowed`。

管理界面的 Role 选择器必须使用 Membership 的 `principal_type` 和 Role 的 `allowed_principal_types` 进行结构化过滤，只展示对目标 Principal 可分配的 Role。不得根据 Role Key 后缀、展示名称或其他字符串约定识别 Runtime Role。

## 七、HTTP 与 Tool 授权声明

每个公开 OpenAPI Operation 必须声明 `x-addp-auth-mode`：

```text
public | authenticated | self | permission | delegated_tool | resource_ticket | internal
```

规则：

1. `permission`、`delegated_tool`、`resource_ticket` 必须声明非空 `x-addp-required-permissions`；
2. 多个 required Permission 按 all-of 处理；
3. 条件追加 Permission 只能使用 `x-addp-conditional-permissions`，服务端无法分类时默认拒绝；
4. OpenAPI 声明、路由 Guard 和生成常量必须引用同一个 Permission Key；
5. Tool 必须声明 owner、audience、Scope、Permission 和风险等级；
6. Gateway 不能按 URL 前缀推导 Permission，也不能代替 owner 的最终校验。

Swagger/OpenAPI 的具体注解方式见 `docs/spec/addp-Swagger集成指南.md`。

## 八、发布期聚合

`common/authorization/cmd/manifest` 是唯一聚合器。它读取所有 owner Manifest、System 内置 Role Manifest、OpenAPI 和 Tool Manifest，执行：

- Schema、版本、命名空间和唯一性校验；
- Role 引用、Principal 类型和 Scope 类型校验；
- owner 本地 Permission 常量生成与一致性校验；
- Tool Catalog 生成与一致性校验；
- 确定性 Permission/Role Catalog 和 SQL seed 校验；
- OpenAPI Operation 与 Tool 授权覆盖报告。

聚合器不在模块启动时写数据库。初始目录和后续变化都必须进入 System 向前 SQL migration；已发布 migration 不得重写。

Permission 收缩、Role 权限移除或主体授权变化必须同步推进受影响 Principal 的授权版本，并按安全语义撤销 Token Family，不能等待 Access Token 自然过期。

## 九、模块生命周期

模块暂时不可用不等于 Permission 删除。服务启停不修改 IAM 目录。

模块永久下线时必须：

1. 将对应 Permission 置为 `disabled` 并递增 Manifest 版本；
2. 从内置 Role 移除对应 Permission 并递增 Role Manifest 版本；
3. 生成新的向前 System migration；
4. 清理或拒绝引用已禁用 Permission 的 Tenant 自定义 Role；
5. 推进授权版本、撤销受影响 Token，并同步删除路由、Swagger、Tool 和前端入口。

不保留旧 Permission 别名、旧路由或双目录查询。

## 十、验证

最小发布门：

```bash
make test-authorization
```

该命令必须同时通过 Manifest/聚合器测试、owner 常量、Tool Catalog、SQL seed、授权覆盖和 Swagger 路由覆盖。报告中的 Operation 或 Tool 数量是当前代码生成结果，不写入本规范作为固定基线。

涉及 IAM PostgreSQL 目录 migration 时还必须使用专用一次性数据库：

```bash
ADDP_SYSTEM_POSTGRES_TEST_DSN='postgres://.../addp_iam_test?...' make test-system-iam-postgres
```

不得对开发库或生产库运行会重建 Schema 的发布门。

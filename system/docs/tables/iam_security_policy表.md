# iam_security_policy 表

## 一、定位

`system.iam_security_policy` 是 System IAM 的 `platform_only` 单例安全策略。它保存 Token 生命周期、OAuth Device Flow、Tenant Invitation 和 OAuth 限流的普通数值策略，不保存 Pepper、加密密钥、Service Client Secret 或其他 Secret。

策略由 Platform Security Administrator 通过 `iam.security_policy.read/update` 维护。System 启动时读取一次当前版本并装配 IAM Runtime；运行期间更新产生待重启版本，不热更新，也不从环境变量回退。

## 二、字段

| 字段 | 类型 | 约束 | 说明 |
| --- | --- | --- | --- |
| `id` | smallint | 主键、固定 `1` | 单例 ID |
| `version` | bigint | 非空、正整数 | 配置乐观并发版本 |
| `applied_version` | bigint | `0 <= applied_version <= version` | 当前 Runtime 已装配版本 |
| `access_token_ttl_minutes` | integer | `1-60` | User/OAuth Access Token 有效期 |
| `delegated_access_token_ttl_minutes` | integer | `1-2` | Agent Delegated Access Token 有效期 |
| `resource_access_ticket_ttl_minutes` | integer | `1-60` 且不大于 Access Token TTL | Browser Resource Access Ticket 有效期 |
| `refresh_token_ttl_days` | integer | `1-365` | Refresh Token Family 最终有效期 |
| `oauth_authorization_code_ttl_minutes` | integer | `1-5` | OAuth Authorization Code 有效期 |
| `oauth_device_code_ttl_minutes` | integer | `5-30` | OAuth Device/User Code 有效期 |
| `oauth_device_poll_interval_seconds` | integer | `5-60` | Device Token 轮询间隔 |
| `tenant_invitation_ttl_hours` | integer | `1-720` | Tenant Invitation 有效期 |
| `oauth_public_rate_limit_per_minute` | integer | `1-10000` | 公共 OAuth 路由每分钟上限 |
| `oauth_user_rate_limit_per_minute` | integer | `1-10000` | 已认证用户 OAuth 路由每分钟上限 |
| `updated_by_principal_id` | bigint | 可空、FK | 最近修改策略的 User Principal；初始种子为空 |
| `created_at` | timestamptz | 非空 | 创建时间 |
| `updated_at` | timestamptz | 非空 | 最近修改时间 |

索引：`idx_iam_security_policy_updated_by_principal_id` 覆盖最近修改 Principal 外键，支持审计关联并满足外键索引门禁。

## 三、版本与审计

- API 更新必须提交当前 `version`；版本不一致返回 409，不覆盖其他管理员的变更。
- 保存成功后 `version` 加一，`applied_version` 保持不变，因此响应返回 `pending_restart=true`。
- System 受控重启时校验当前记录并将 `applied_version` 推进到 `version`，随后整套 IAM Runtime 只使用该版本。
- 更新与 `iam.security_policy.updated` 审计事件在同一事务提交。审计详情只包含版本、待重启状态和普通数值字段差异。

# ADDP IAM OIDC 启用设计

更新日期：2026-08-01

状态：待设计和实现。当前 System 不注册 OpenID Handler、不允许 `openid` Scope、不签发 ID Token，也不发布 Discovery/JWKS。

## 一、目标

在不改变 ADDP opaque API Access Token 和唯一 AuthContext 路径的前提下，为受控 OIDC Client 提供标准身份断言。

OIDC ID Token 只面向 OIDC Client，不是 ADDP API Access Token。业务模块始终拒绝 ID Token 和外部 IdP Token 作为 Bearer Token。

## 二、实施前决策门

启用前必须确认：

1. 稳定 issuer 及反向代理、开发和生产环境边界；
2. Subject 生成规则及跨 Client 关联策略；
3. 最小 Claims 目录、来源、版本和隐私边界；
4. `acr`、`amr`、`auth_time`、`nonce`、`azp` 和 audience 语义；
5. 签名算法、Key Provider、私钥存储和轮换窗口；
6. Discovery、JWKS 缓存和旧公钥验证期限；
7. RP-Initiated Logout、Browser Session 与外部 IdP 会话边界；
8. OIDC Client 注册、redirect URI、JWKS/JWKS URI 和权限治理；
9. Authorization Code、Refresh 和 Device Flow 的 ID Token 行为；
10. Swagger、配置、审计和安全事件要求。

## 三、唯一技术路径

- 在现有单一 Fosite Provider 中加入 OpenID Connect Factory；
- 复用现有 PostgreSQL Storage Adapter 和 Token Family，不创建第二 Provider 或第二用户会话；
- `system.oauth_oidc_sessions` 只保存协议重建所需状态；
- 签名私钥由 System Key Provider 提供，不写入 Session、数据库普通字段或环境日志；
- Access Token 继续使用 `addp_at_` opaque Token；
- OIDC Claims 从 System IAM 稳定事实投影，不能由 Client 自报或从邮箱临时推导 Subject。

## 四、禁止事项

- 未完成 issuer 和密钥生命周期设计就生成临时生产密钥；
- 每次启动随机生成签名密钥；
- 把 ID Token 当作 ADDP API Access Token；
- 为 OIDC 建立第二 Principal、Tenant、Role 或 AuthContext；
- 在未注册 Handler 时允许 `openid` Scope或发布 Discovery/JWKS；
- 按 Client 或 grant 保留自研 OIDC/OAuth 状态机。

## 五、验收

至少覆盖 Discovery、JWKS、签名与轮换、nonce、`auth_time`、`acr/amr`、audience/`azp`、Code/Refresh/Device Flow、Logout、错误语义、审计脱敏和 AuthContext 隔离。

完成实现后，稳定协议要求并入 `docs/spec/addp OAuth授权规范.md`，Provider 和 Storage 细节并入 `system/docs/OAuth与Fosite实现说明.md`，本文删除。

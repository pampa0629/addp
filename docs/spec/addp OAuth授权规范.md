# ADDP OAuth 授权规范

更新日期：2026-07-16

状态：阶段 4.2 正式规范。

## 一、统一令牌模型

ADDP 的 Web、CLI 和 OAuth 客户端统一使用随机 opaque 用户令牌。业务模块不解析令牌，只调用 System AuthContext。

- Access Token：`addp_at_` 前缀，32 字节随机值，只保存 SHA-256 Hash，默认有效期 15 分钟。
- Refresh Token：`addp_rt_` 前缀，32 字节随机值，只保存 SHA-256 Hash，默认有效期 30 天。
- Authorization Code：`addp_ac_` 前缀，单次使用，默认有效期 5 分钟。
- Device Code：`addp_dc_` 前缀，只保存 Hash，默认有效期 10 分钟。
- User Code：8 位易输入字符，服务端只保存规范化值的 Hash。

System 不再签发或解析用户 JWT；旧的“允许过期 Access Token 调 `/refresh`”路径删除。

## 二、客户端与授权

OAuth Client 独立存储在 `system.oauth_clients`，不复用 `applications` 或 `api_keys`。

第一阶段内置公共客户端：

| client_id | 用途 | redirect URI | Device Flow |
| --- | --- | --- | --- |
| `addp-cli` | ADDP CLI、Codex、Hermes 等本地 Agent | `http://127.0.0.1:8765/callback` | 允许 |

公共客户端不配置 Client Secret。Authorization Code Flow 只接受 PKCE `S256`，redirect URI 必须与客户端注册值完全一致。

## 三、Refresh Token Family

一次登录或用户授权创建一个 Refresh Token Family。每次刷新必须在单个数据库事务内：

1. 锁定当前 Refresh Token 和 family；
2. 校验用户、租户、客户端、有效期和撤销状态；
3. 将当前 Token 标记为已使用；
4. 创建新的 Refresh Token 和 Access Token；
5. 记录 replaced_by 关系。

已使用 Refresh Token 再次出现时视为重用攻击，立即撤销整个 family 及其全部 Access Token。撤销时同步删除 Redis `auth:context:<token_hash>` 缓存。

## 四、Web 登录与 Cookie

`POST /api/v1/system/login` 返回短期 Access Token，并通过 Cookie 保存 Refresh Token：

- 名称：`addp_refresh_token`
- `HttpOnly=true`
- `SameSite=Lax`
- Path：`/api/v1/system`
- 生产环境 `Secure=true`

`POST /api/v1/system/refresh` 只读取该 Cookie，不接受旧 Access Token。响应返回新的 Access Token，并轮换 Cookie 中的 Refresh Token。

`POST /api/v1/system/logout` 撤销当前 family 并清除 Cookie。

## 五、Authorization Code + PKCE

CLI 生成 `code_verifier`、S256 `code_challenge` 和随机 `state`，打开 Console `/oauth/authorize`。Console 使用当前 ADDP Access Token 调用：

```text
POST /api/v1/system/oauth/authorizations
```

System 校验用户、客户端、redirect URI、Scope 和 PKCE 后返回重定向 URL。Authorization Code 只能使用一次。CLI 再以 `grant_type=authorization_code`、`client_id`、`code`、`redirect_uri` 和 `code_verifier` 调用 `/oauth/token`。

## 六、Device Authorization Flow

1. CLI 调用 `POST /oauth/device/code` 获取 `device_code`、`user_code`、verification URI、过期时间和轮询间隔。
2. 用户在 Console `/oauth/device` 登录并确认 User Code。
3. Console 调用受 Bearer 保护的 `POST /oauth/device/authorizations`。
4. CLI 按 interval 调用 `/oauth/token`，使用 Device Code grant。
5. pending 返回 `authorization_pending`；过快返回 `slow_down`；批准后只成功兑换一次。

## 七、Token API

`POST /oauth/token` 支持 `authorization_code`、`urn:ietf:params:oauth:grant-type:device_code` 和 `refresh_token` 三种 grant。

OAuth 成功响应包含 `access_token`、`token_type=Bearer`、`expires_in`、`refresh_token` 和 `scope`。CLI 只把 Refresh Token 保存到 OS Keychain；每次命令执行前刷新并原子更新轮换后的 Refresh Token。

## 八、AuthContext 映射

第一方 Web Token返回 `auth_type=first_party_access_token`、`client_id=null`、空 audiences/scopes。OAuth Token 返回 `auth_type=oauth_access_token`、真实 `client_id`、`audiences=["addp-api"]` 和批准的 scopes。

Scope 仍只能缩小权限；owner 模块的 Scope 强制执行和写入审批在阶段 4.3 完成。

## 九、禁止事项

- 保存任何 Token、Authorization Code 或 Device Code 明文。
- 公共客户端内置 Client Secret。
- 支持 PKCE `plain`。
- redirect URI 前缀匹配、通配符或回退 URI。
- 同时保留 JWT 刷新和 Refresh Token Family 两条路径。
- 使用 API Key、`INTERNAL_API_KEY` 或 Scope 模拟用户身份提升。

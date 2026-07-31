# Common-Python 当前实施状态

更新日期：2026-07-31

## 模块边界

`common-python` 是 ADDP Python 共享库，同时发布正式 `addp` CLI entry point。它负责：

- 各 owner 模块的异步 Python SDK Client；
- `addp.auth_context/v1` 的严格解析；
- `addp.tool-manifest/v1` 与唯一 `ToolExecutor`；
- `addp.workflow/v1` 的通用协议执行核心；
- `addp-cli` 公共 OAuth Client 的 Browser PKCE、Device Flow、刷新、状态和撤销。

它不是独立认证服务、远程 Tool 服务或 Workflow Engine。OAuth 协议状态与 Token 事实仍只属于 System，正式协议引擎仍只有 Fosite。

## CLI 认证主路径

CLI 登录只有两种标准交互：

1. `addp auth login` 使用 Authorization Code + PKCE 和 RFC 8252 动态 loopback；
2. `addp auth login --device` 使用 Device Authorization Flow。

两者固定使用公共客户端 `client_id=addp-cli`、`scope=addp.api`，不配置 Client Secret。CLI 不提供用户名密码直传、API Key、Client Secret 或手工 Token 登录。

Refresh Token 按归一化 ADDP Base URL 隔离，只保存到 OS Keychain。Access Token、Authorization Request Secret、PKCE verifier、Authorization Code 和 Device Code 只存在于进程内存。同一 Base URL 的登录、刷新、状态和撤销共用跨进程文件锁，刷新轮换后原子更新 Keychain。

`addp auth status` 会先刷新，再调用 System 唯一 AuthContext API；Keychain 中有值不等于已认证。OAuth Family 在浏览器批准时绑定 Platform 或 Tenant Context，CLI 不提供事后 Context Switch，需要其他 Context 时先撤销并重新授权。

## 安装与版本

包名固定为 `addp-common`，命令固定为 `addp`。版本只在 `addp_common.__version__` 定义，构建元数据和 `addp --version` 都读取该事实源。当前版本为 `0.1.11`。

正式交付使用 GitHub Release 中的 wheel，不以本地源码构建或 editable 源码目录作为用户安装方式。维护者复现构建、全新环境安装和产品 E2E 的唯一入口为：

```bash
make test-common-python-cli-release
```

## 发布门禁

门禁按固定顺序执行：

1. 从 `common-python` 构建 wheel；
2. 创建全新 venv 并只安装 wheel、运行依赖和测试依赖；
3. 对已安装 wheel 运行 common-python 全量测试；
4. 校验 `addp` entry point、安装元数据和 JSON 版本输出一致；
5. 使用真实 OS Keychain 后端运行 CLI 产品 E2E。

产品 E2E 覆盖 Browser loopback + PKCE、Device Flow、权威 AuthContext、Context 绑定、独立进程刷新竞争、OAuth Revocation、Keychain 删除时机，以及 Access Token、Refresh Token、Authorization Code、PKCE Verifier、Device Code 和 Request Secret 不进入 stdout/stderr。

测试 OAuth 协议服务器只用于驱动已安装 CLI 的客户端行为，不进入生产包、不新增生产端点，也不替代 System Fosite 协议验收。System Fosite Provider、PostgreSQL Storage、刷新重用和审计事务仍由 System 测试独立证明；正式发布要求两侧门禁都通过。

GitHub Release 是当前唯一正式分发路径。推送 `v<version>` Tag 后，同一次 GitHub Actions 运行必须重新通过 macOS CLI 产品门禁和 PostgreSQL 15 System IAM 门禁；发布 Job 只下载前者归档的已验证 wheel，复核 SHA-256、包名和 wheel `METADATA` 版本与 Tag 一致，然后创建 Release。发布阶段不检出源码、不重新构建。PyPI 与私有包仓库待账号、权限和发布策略独立确定后再设计。

## 延期边界

OIDC、外部 IdP、跨域 SSO 与单点退出不在本阶段实现。当前 System 不注册 OpenID Handler，不允许 `openid` Scope，也不发布 Discovery/JWKS；这些能力必须在后续独立阶段完成 issuer、Claim、密钥生命周期和 External Identity 映射设计后实施。

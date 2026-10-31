# Common-Python 当前实施状态

更新日期：2026-07-31

[返回模块入口](CLAUDE.md) · [使用与认证](README.md#认证方式) · [产品 E2E 断言](#发布门禁) · [未覆盖项](#延期边界)

本文保留产品门禁的实现断言和未覆盖边界，使用操作与通用契约统一引用 README；不作为某一源码版本的验收通过报告。

## 模块边界

`common-python` 是 ADDP Python 共享库，同时发布正式 `addp` CLI entry point。它负责：

- 各 owner 模块的异步 Python SDK Client；
- `addp.auth_context/v1` 的严格解析；
- `addp.tool-manifest/v1` 与唯一 `ToolExecutor`；
- `addp.workflow/v1` 的通用协议执行核心；
- `addp-cli` 公共 OAuth Client 的 Browser PKCE、Device Flow、刷新、状态和撤销。

它不是独立认证服务、远程 Tool 服务或 Workflow Engine。OAuth 协议状态与 Token 事实仍只属于 System，正式协议引擎仍只有 Fosite。

## CLI 认证主路径

登录交互、公共客户端、禁止输入的认证材料及批准后的 Context 绑定统一见[README 认证方式](README.md#认证方式)；正式协议见[CLI 产品契约](<../docs/spec/addp OAuth授权规范.md#71-正式-addp-cli-产品契约>)。客户端本地状态保留以下约束：

Refresh Token 按归一化 ADDP Base URL 隔离，只保存到 OS Keychain。Access Token、Authorization Request Secret、PKCE verifier、Authorization Code 和 Device Code 只存在于进程内存。同一 Base URL 的登录、刷新、状态和撤销共用跨进程文件锁，刷新轮换后原子更新 Keychain。

## 安装与版本

包名固定为 `addp-common`，命令固定为 `addp`。版本只在 `addp_common.__version__` 定义，构建元数据和 `addp --version` 都读取该事实源。当前版本为 `0.1.14`。

正式交付、升级与版本准备见[README 安装说明](README.md#cli-安装)；维护者的唯一产品门禁、环境前提与 System IAM T2 要求见[开发与验证](README.md#开发与验证)。

## 发布门禁

以下为产品门禁的实现顺序与断言，实际结果须按对应源码版本与报告认定：

1. 从 `common-python` 构建 wheel；
2. 创建全新 venv 并只安装 wheel、运行依赖和测试依赖；
3. 对已安装 wheel 运行 common-python 全量测试；
4. 校验 venv 中的 `addp` entry point、安装元数据和 JSON 版本输出一致；
5. 在临时 pipx 根目录中通过 `PIPX_DEFAULT_BACKEND=pip` 安装并强制重装同一个 wheel，校验包来源、版本和命令入口；
6. 通过 pipx 安装的 `addp` 使用真实 OS Keychain 后端运行 CLI 产品 E2E；
7. 卸载 pipx 环境并校验命令入口和环境无残留。

全量测试中的 `tests/test_version.py` 是版本发布预检的唯一实现，统一校验运行时单一版本源、安装包元数据、命令版本、根 README、CLI README、实施报告和脚本文档，不维护第二套版本同步脚本。

产品 E2E 覆盖 Browser loopback + PKCE、Device Flow、权威 AuthContext、Context 绑定、独立进程刷新竞争、OAuth Revocation、Keychain 删除时机，以及 Access Token、Refresh Token、Authorization Code、PKCE Verifier、Device Code 和 Request Secret 不进入 stdout/stderr。

测试 OAuth 协议服务器只用于驱动已安装 CLI 的客户端行为，不进入生产包、不新增生产端点，也不替代 System Fosite 协议验收。System Fosite Provider、PostgreSQL Storage、刷新重用和审计事务仍由 System 测试独立证明；正式发布要求两侧门禁都通过。

分发渠道、Tag 门禁、同次 wheel 归档、摘要与版本核验、OIDC 来源证明及后续渠道边界，统一见[README 安装说明](README.md#cli-安装)和[发布验证](README.md#开发与验证)。

## 延期边界

OIDC、外部 IdP、跨域 SSO 与单点退出不在本阶段实现。当前 System 不注册 OpenID Handler，不允许 `openid` Scope，也不发布 Discovery/JWKS；这些能力必须在后续独立阶段完成 issuer、Claim、密钥生命周期和 External Identity 映射设计后实施。

CLI 当前正式支持范围与真实 OS Keychain 发布证据仅覆盖 macOS；Windows Credential Manager 与 Linux Secret Service 尚需同等级产品 E2E，范围见[README 认证方式](README.md#认证方式)。这不限制共享 SDK 本身的运行平台。

共享库回归、已安装 CLI／真实 Keychain 的 T5、System IAM PostgreSQL T2 和真实跨模块运行验收分别认定，层级见[测试与验收规范](../docs/spec/addp测试与验收规范.md#二测试分层)。本报告未提供固定源码版本的执行报告，不能据门禁代码或上述断言认定已通过；未运行、失败和未覆盖项分别记录，不计为通过。

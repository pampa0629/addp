# ADDP Common Python Module

Python 共享模块，为 ADDP 平台的 Python 后端提供统一客户端、工具函数和 Workflow Runtime 协议执行核心。

`addp_common.workflow_runtime` 负责 `addp.workflow/v1` 的 workflow definition 校验、拓扑排序、`$ref` 解析、异步 execution 状态和标准错误。它不是独立工作流引擎，不包含 GeoPandas、Spark、PDAL 或三维转换器领域逻辑。

## CLI 安装

正式 CLI 交付物是 GitHub Release 中的 `addp-common` wheel，当前版本为 `0.1.11`。下载 wheel 和同名 SHA-256 文件，校验后安装到隔离环境：

```bash
RELEASE=v0.1.11
VERSION=${RELEASE#v}
WHEEL="addp_common-${VERSION}-py3-none-any.whl"
curl -fLO "https://github.com/pampa0629/addp/releases/download/${RELEASE}/${WHEEL}"
curl -fLO "https://github.com/pampa0629/addp/releases/download/${RELEASE}/${WHEEL}.sha256"
shasum -a 256 -c "${WHEEL}.sha256"
pipx install "./${WHEEL}"
addp --version
```

GitHub Release 是当前唯一正式分发路径。也可以用 `python3 -m venv` 和该 wheel 安装到隔离环境；正式安装不使用本地源码构建、editable 源码目录或仓库内 `.venv`。PyPI 或私有包仓库待账号、权限和发布策略明确后另行设计，不与当前路径并存。

其他 ADDP Python 模块在仓库内开发时继续通过各自依赖声明引用 `../../common-python`，不把 CLI wheel 当作服务运行时部署机制。

## 使用示例

```python
from addp_common.client import DevelopClient, ManagerClient

# 用户请求（使用 opaque Access Token）
develop = DevelopClient(
    base_url="http://localhost:8000",
    user_token="user-access-token"
)
workflow_engines = await develop.list_workflow_engines()
operators = await develop.list_operators(workflow_engines[0]["id"])

# 当前用户数据搜索
manager = ManagerClient(
    base_url="http://localhost:8000",
    user_token="user-access-token"
)
results = await manager.search("城市", page_size=10)
```

## 客户端列表

- `BaseClient` - 基础 HTTP 客户端
- `SystemClient` - System 模块 (引擎管理)
- `MetaClient` - Meta 模块 (元数据搜索)
- `DevelopClient` - Develop 模块 (SQL、工作流、算子)
- `ManagerClient` - Manager 模块 (数据管理、预览)
- `CopilotClient` - Copilot 模块（结构化生成）

## Tool 与 CLI

`addp_common/tools/manifest.json` 定义稳定 Tool 契约，完整开放规则见 `docs/spec/addp智能体Tool开放规范.md`。`ToolExecutor` 在每次 Tool 调用前使用当前 User Access Token 向 System 申请绑定 owner audience、稳定 Tool Scope、AgentRun 和 ToolCall 的短期 Delegated Access Token，随后只通过持有该委托令牌的 SDK Client 调用 owner API。安装后可供 Codex 等本地 Agent 使用：

```bash
export ADDP_BASE_URL=http://localhost:8000

addp auth login
# 无浏览器或跨设备环境
addp auth login --device
addp auth status
addp auth logout

addp tools list
addp tools get workflow.validate
addp tool call data.search --arguments '{"query":"城市","limit":10}'
# 外部 Agent 可显式传入稳定审计绑定
addp tool call workflow.validate --agent-run-id <run-id> --tool-call-id <call-id> --arguments '{...}'
```

成功响应的 stdout 是 Tool 结果紧凑 JSON；失败响应也是标准错误 JSON，并返回非零 exit code。

`workflow.run` 使用 Develop owner approval 的两阶段单一路线：首次输入 `workflow_engine_id + workflow_definition`，返回 `approval_required`；批准后再次调用时只输入 `approval_id + request_fingerprint`。SDK 不保存审批事实或完整 workflow payload，恢复调用由 Develop 从自己的审批记录读取原请求并一次性消费。

## 认证方式

认证只保留 OAuth/Bearer 主路径：

1. **服务间请求**：各模块使用独立 Confidential OAuth Client，通过 Client Credentials Grant 即时获取短期 Service Access Token；业务请求不得发送 `X-Internal-API-Key` 或 `X-Tenant-ID`。
2. **用户请求**：Web 使用 HttpOnly Refresh Token Cookie，CLI 使用 OS Keychain 保存轮换 Refresh Token。

CLI 登录只有 `addp auth login` 和 `addp auth login --device` 两种交互，两者都使用无 Client Secret 的公共客户端 `addp-cli`。不支持用户名密码直传、Client Secret、API Key 或手工粘贴 Token 登录。

CLI 最终目标支持主流桌面操作系统。当前受发布测试环境约束，正式支持和真实 OS Keychain 发布证据仅覆盖 macOS；Windows Credential Manager 与 Linux Secret Service 待各自建立同等级 E2E 后加入支持矩阵。Python 共享 SDK 的运行平台不受这一 CLI 发布证据边界限制。

CLI Browser Login 按 RFC 8252 在 `127.0.0.1` 绑定随机空闲端口，先向 System 创建五分钟有效的 Authorization Request，再以浏览器 URL 中唯一的 `request_id` 完成用户授权；完整 redirect URI 和 PKCE 只在 CLI 与 System 之间传输。CLI 提前退出或超时时使用内存中的 Request Secret 取消请求，并在授权与 Code 兑换阶段使用同一个完整 redirect URI。CLI 每次执行 Tool 前按 ADDP Base URL 获取跨进程锁，再使用 Keychain 中的 Refresh Token 换取短期 User Access Token，并原子保存轮换后的 Refresh Token；User Access Token 不持久化。ToolExecutor 再按调用即时换取不可刷新、默认 2 分钟的 Delegated Access Token，原始 User Access Token 不进入 owner Client。

授权页会在批准前显示当前 Platform 或 Tenant Context，并允许使用 Browser Context Switch 选择目标。OAuth Family 在批准后永久绑定该 Context；CLI 不提供事后切换命令，需要进入其他 Context 时先执行 `addp auth logout`，再重新登录并授权。`addp auth status` 会实际轮换 Refresh Token 并解析 System AuthContext，输出当前 Principal 与 Context 摘要，不以 Keychain 中是否存在值代替服务端会话检查。

本地开发环境与单元测试：

```bash
cd common-python
uv sync --extra dev
uv run pytest -q
addp --version
```

Tool 与 Adapter 变更至少运行 common-python 全量测试和 `make test-agent-eval`；场景与发布门禁规则见 `docs/spec/addp智能体评测规范.md`。

CLI 正式发布必须从仓库根目录运行唯一产品门禁：

```bash
make test-common-python-cli-release
```

门禁构建 wheel，在全新 venv 中安装依赖和 wheel，校验 `addp` entry point 与版本，然后使用真实 macOS Keychain 完成 Browser loopback + PKCE、Device Flow、AuthContext、跨进程刷新轮换、撤销和终端敏感信息 E2E。非 macOS 环境或缺少可用 macOS Keychain 后端时门禁失败，不降级到明文文件凭据库。

GitHub Actions 的 `CLI product gate (macOS Keychain)` Job 使用同一入口，并归档通过 `twine check`、全新环境安装和产品 E2E 的同一个 wheel 及其 SHA-256；不得在门禁后重新构建另一个待发布 wheel。

推送与包版本一致的 `v<version>` Tag 会重新运行 macOS CLI 产品门禁和 PostgreSQL 15 System IAM 门禁。两项均通过后，发布 Job 只下载同一次运行归档的 wheel，校验 SHA-256、包名和 `METADATA` 版本，再创建 GitHub Release；发布阶段不检出源码、不重新构建。`make test-common-python-cli-release` 仅用于维护者在本地复现产品门禁，其输出不是正式发布物。

CLI 门禁验证已安装客户端，不替代 System Fosite 和 PostgreSQL 事务验收。正式发布还必须对专用一次性数据库运行：

```bash
ADDP_SYSTEM_POSTGRES_TEST_DSN='postgres://.../addp_iam_test?sslmode=disable' \
  make test-system-iam-postgres
```

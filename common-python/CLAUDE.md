# Common-Python 共享模块说明

## 模块定位

`common-python/` 为 ADDP Python 后端和 Python Workflow Runtime 提供共享 HTTP 客户端、协议执行核心和工具，减少跨模块与跨运行时重复实现。

## 重要目录

```text
common-python/
├── addp_common/
│   └── client/
│       ├── base.py
│       ├── system.py
│       ├── meta.py
│       ├── develop.py
│       ├── manager.py
│       ├── graph.py
│       └── copilot.py
│   ├── tools/
│   │   ├── manifest.json
│   │   ├── manifest.py
│   │   └── executor.py
│   ├── cli.py
│   └── workflow_runtime/
│       ├── validation.py
│       ├── graph.py
│       ├── references.py
│       └── execution.py
├── pyproject.toml
└── README.md
```

## 开发规则

- 新增 Python 服务间调用客户端时，优先扩展 `addp_common/client/`，不要在 `agent`、`copilot` 中重复实现。
- 用户交互请求使用短期 User Access Token；服务间请求使用各模块独立 Confidential OAuth Client 通过 Client Credentials Grant 获取短期 Service Access Token，业务请求只发送 `Authorization: Bearer`。
- 不得新增或恢复 `internal_api_key`、`X-Internal-API-Key`、`X-Tenant-ID`、用户 Token 代传或其他服务间认证双轨。
- 用户 `user_token` 只能通过 `addp_common.auth.resolve_authorization_context()` 调用 System AuthContext API 解析；Python 模块不自行解析 JWT。
- 客户端 URL 与 API 路径要以各模块当前 `CLAUDE.md`、路由和 Swagger 为准。
- `tools/manifest.json` 是 AI Tool 契约事实源，`ToolExecutor` 是 Manifest 到 SDK 的唯一执行映射。
- `ToolExecutor` 每次调用必须使用源 User Access Token 向 System 申请绑定 owner audience、稳定 Tool Scope、AgentRun 和 ToolCall 的短期 Delegated Access Token；owner SDK Client 只能接收该委托令牌。
- `addp` CLI 和 Agent Tool Provider 只能作为 `ToolExecutor` 的薄 Adapter，不得直接发 HTTP。
- CLI stdout 必须是单个紧凑 JSON，日志只写 stderr，并保持稳定 exit code。
- CLI 默认通过 Authorization Code + PKCE 登录，无浏览器环境使用标准 Device Flow；两者都只能使用内置公共客户端 `addp-cli`。
- CLI 只在 OS Keychain 保存按归一化 ADDP Base URL 隔离的 Refresh Token；Access Token 和其他 OAuth 临时凭据只存在内存。
- CLI 不接受 `--token`、`ADDP_TOKEN` 或其他手工 User Access Token 注入路径；每次 Tool 调用都必须走 Keychain 刷新和跨进程锁。
- CLI 最终目标支持主流桌面操作系统；当前正式发布证据只覆盖真实 macOS Keychain，其他平台建立同等级 E2E 后再加入支持矩阵。
- `addp auth status` 必须刷新并解析权威 AuthContext，不能把 Keychain 中存在值视为已认证；OAuth Family Context 在批准后不可切换。
- OAuth 授权页面负责在批准前展示并选择 Browser Context；CLI 需要其他 Context 时必须退出后重新授权。
- 修改公共客户端后，至少验证直接使用它的 Python 模块。
- `workflow_runtime` 只承载 `addp.workflow/v1` 的通用 DAG、引用和 execution 状态，不依赖 Flask、GeoPandas、Spark、PDAL 或三维转换器。
- 各 Python Workflow Runtime 使用公共核心后，必须删除本地重复实现，不保留兼容执行路径。

## 验证

```bash
cd common-python
uv sync --extra dev
uv run pytest -q

# 正式 CLI wheel、全新环境、命令入口和 macOS Keychain 产品门禁
cd ..
make test-common-python-cli-release
```

如本地未安装测试依赖，可先用引用模块的虚拟环境做导入和最小调用验证。

## 相关文档

- `common-python/README.md`
- `common-python/common-python实施报告.md`
- `docs/spec/addp智能体Tool开放规范.md`
- `docs/spec/addp智能体评测规范.md`
- `docs/spec/addp OAuth授权规范.md`
- `agent/CLAUDE.md`
- `copilot/CLAUDE.md`

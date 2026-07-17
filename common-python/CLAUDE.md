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
- 同时支持服务间 `internal_api_key` 和用户请求 `user_token` 两类认证。
- 用户 `user_token` 只能通过 `addp_common.auth.resolve_authorization_context()` 调用 System AuthContext API 解析；Python 模块不自行解析 JWT。
- 客户端 URL 与 API 路径要以各模块当前 `CLAUDE.md`、路由和 Swagger 为准。
- `tools/manifest.json` 是 AI Tool 契约事实源，`ToolExecutor` 是 Manifest 到 SDK 的唯一执行映射。
- `ToolExecutor` 每次调用必须使用源 User Access Token 向 System 申请绑定 owner audience、稳定 Tool Scope、AgentRun 和 ToolCall 的短期 Delegated Access Token；owner SDK Client 只能接收该委托令牌。
- `addp` CLI 和 Agent Tool Provider 只能作为 `ToolExecutor` 的薄 Adapter，不得直接发 HTTP。
- CLI stdout 必须是单个紧凑 JSON，日志只写 stderr，并保持稳定 exit code。
- 修改公共客户端后，至少验证直接使用它的 Python 模块。
- `workflow_runtime` 只承载 `addp.workflow/v1` 的通用 DAG、引用和 execution 状态，不依赖 Flask、GeoPandas、Spark、PDAL 或三维转换器。
- 各 Python Workflow Runtime 使用公共核心后，必须删除本地重复实现，不保留兼容执行路径。

## 验证

```bash
cd common-python
pip install -e ".[dev]"
pytest
```

如本地未安装测试依赖，可先用引用模块的虚拟环境做导入和最小调用验证。

## 相关文档

- `common-python/README.md`
- `common-python/common-python实施报告.md`
- `agent/CLAUDE.md`
- `copilot/CLAUDE.md`

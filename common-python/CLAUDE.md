# Common-Python 共享模块说明

## 模块定位

`common-python/` 为 ADDP Python 后端提供共享 HTTP 客户端和工具，当前主要服务于 `agent/`、`copilot/` 以及后续 Python 服务，减少跨模块调用的重复实现。

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
│       └── graph.py
├── pyproject.toml
└── README.md
```

## 开发规则

- 新增 Python 服务间调用客户端时，优先扩展 `addp_common/client/`，不要在 `agent`、`copilot` 中重复实现。
- 同时支持服务间 `internal_api_key` 和用户请求 `user_token` 两类认证。
- 客户端 URL 与 API 路径要以各模块当前 `CLAUDE.md`、路由和 Swagger 为准。
- 修改公共客户端后，至少验证直接使用它的 Python 模块。

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

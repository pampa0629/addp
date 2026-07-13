# ADDP Common Python Module

Python 共享模块，为 ADDP 平台的 Python 后端提供统一客户端、工具函数和 Workflow Runtime 协议执行核心。

`addp_common.workflow_runtime` 负责 `addp.workflow/v1` 的 workflow definition 校验、拓扑排序、`$ref` 解析、异步 execution 状态和标准错误。它不是独立工作流引擎，不包含 GeoPandas、Spark、PDAL 或三维转换器领域逻辑。

## 安装

在其他 Python 模块中使用本地开发模式安装:

```bash
# 在 agent/backend 或 copilot/backend 的 requirements.txt 中添加
-e ../../common-python
```

## 使用示例

```python
from addp_common.client import SystemClient, DevelopClient, MetaClient

# 服务间调用 (使用 Internal API Key)
system = SystemClient(
    base_url="http://localhost:8180",
    internal_api_key="your-internal-key"
)
engines = await system.list_internal_engines()

# 用户请求 (使用 JWT Token)
develop = DevelopClient(
    base_url="http://localhost:8000",
    user_token="user-jwt-token"
)
workflow_engines = await develop.list_workflow_engines()
operators = await develop.list_operators(workflow_engines[0]["id"])

# 元数据搜索
meta = MetaClient(
    base_url="http://localhost:8280",
    internal_api_key="your-internal-key"
)
results = await meta.search_metadata("城市", limit=10)
```

## 客户端列表

- `BaseClient` - 基础 HTTP 客户端
- `SystemClient` - System 模块 (引擎管理)
- `MetaClient` - Meta 模块 (元数据搜索)
- `DevelopClient` - Develop 模块 (SQL、工作流、算子)
- `ManagerClient` - Manager 模块 (数据管理、预览)

## 认证方式

支持两种认证方式:

1. **服务间调用**: 使用 `internal_api_key` 参数
2. **用户请求**: 使用 `user_token` 参数

## 开发

```bash
cd common-python
pip install -e ".[dev]"
pytest
```

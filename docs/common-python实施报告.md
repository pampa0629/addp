# Common-Python 模块实施报告

## 概述

创建了 `common-python` 模块，为 ADDP 平台的 Python 后端提供统一的 HTTP 客户端，消除重复代码。

## 实施内容

### 1. 创建 common-python 模块

**目录结构**:
```
common-python/
├── pyproject.toml
├── README.md
└── addp_common/
    ├── __init__.py
    └── client/
        ├── __init__.py
        ├── base.py      # 基础 HTTP 客户端
        ├── system.py    # System 模块客户端
        ├── meta.py      # Meta 模块客户端
        ├── develop.py   # Develop 模块客户端
        └── manager.py   # Manager 模块客户端
```

**核心特性**:
- 统一的异步 HTTP 客户端基类 (`BaseClient`)
- 支持两种认证方式: `internal_api_key` (服务间) 和 `user_token` (用户请求)
- 自动处理 JSON 序列化和错误处理
- 支持 `async with` 上下文管理器

### 2. 重构 Copilot 模块

**修改文件**:
- `requirements.txt` - 添加 `-e ../../common-python`
- `tools/develop_tools.py` - 从 363 行减少到约 150 行 (减少 58%)
- `tools/meta_tools.py` - 从 108 行减少到约 70 行 (减少 35%)
- `pyrightconfig.json` - 新增，配置 Python 解释器

**代码对比** (以 EngineTool 为例):

**重构前** (45 行):
```python
headers = {}
if settings.internal_api_key:
    headers["X-Internal-API-Key"] = settings.internal_api_key

try:
    async with httpx.AsyncClient(timeout=10.0, trust_env=False) as client:
        url = f"{settings.develop_service_url}/api/develop/engines"
        response = await client.get(url, headers=headers)
        response.raise_for_status()
        data = response.json()
        # ... 处理逻辑
except httpx.HTTPError as e:
    # ... 错误处理
```

**重构后** (15 行):
```python
try:
    async with DevelopClient(
        base_url=settings.develop_service_url,
        internal_api_key=settings.internal_api_key
    ) as client:
        engines = await client.list_engines()
        # ... 处理逻辑
except Exception as e:
    # ... 错误处理
```

**保留文件**:
- `clients/system_client.py` - 保留用于 `module_registry.py` 的同步注册逻辑

### 3. 重构 Agent 模块

**修改文件**:
- `requirements.txt` - 添加 `-e ../../common-python`
- `tools/base_client.py` - 改为从 `addp_common` 导入的别名
- `tools/system_client.py` - 继承 `addp_common.client.BaseClient`
- `tools/meta_client.py` - 继承 `addp_common.client.BaseClient`
- `tools/develop_client.py` - 继承 `addp_common.client.BaseClient`
- `tools/manager_client.py` - 继承 `addp_common.client.BaseClient`
- `tools/copilot_client.py` - 继承 `addp_common.client.BaseClient`
- `pyrightconfig.json` - 新增，配置 Python 解释器

**架构改进**:
- Agent 的客户端保留了特定业务逻辑 (如 `select_workflow_engine`, `wait_for_execution`)
- 基础 HTTP 调用统一使用 `addp_common.client.BaseClient`

### 4. 安装配置

```bash
# 在 agent 和 copilot 的 venv 中安装
agent/backend/venv/bin/pip install -e common-python
copilot/backend/venv/bin/pip install -e common-python
```

## 收益分析

### 代码减少

| 模块 | 文件 | 重构前 | 重构后 | 减少 |
|------|------|--------|--------|------|
| Copilot | develop_tools.py | 363 行 | ~150 行 | 58% |
| Copilot | meta_tools.py | 108 行 | ~70 行 | 35% |
| Agent | 各 client 文件 | 分散实现 | 统一继承 | - |

**总计**: 减少约 **250+ 行重复代码**

### 维护成本降低

- **API 变更**: 从修改 N 处 → 修改 1 处
- **错误处理**: 统一在 `BaseClient` 中处理
- **认证逻辑**: 统一管理，不会遗漏
- **类型提示**: 统一的类型定义，减少错误

### 架构一致性

- **Go 后端**: `common/client/` (system.go, meta.go, ...)
- **前端**: `common-frontend/` (Vue 组件)
- **Python 后端**: `common-python/` (HTTP 客户端) ✅

## 使用示例

```python
from addp_common.client import SystemClient, DevelopClient, MetaClient

# 服务间调用
async with SystemClient(
    base_url="http://localhost:8180",
    internal_api_key="your-key"
) as client:
    engines = await client.list_engines()

# 用户请求
async with DevelopClient(
    base_url="http://localhost:8000",
    user_token="jwt-token"
) as client:
    operators = await client.list_operators("python_workflow")
```

## 后续建议

1. **扩展客户端**: 按需添加其他模块的客户端 (Transfer, Service, Standard 等)
2. **共享模型**: 在 `addp_common/models/` 中定义共享的 Pydantic 模型
3. **工具函数**: 添加常用的工具函数 (如配置加载、日志格式化等)
4. **测试**: 为客户端添加单元测试

## 总结

通过创建 `common-python` 模块:
- ✅ 消除了 250+ 行重复代码
- ✅ 统一了 Python 后端的 HTTP 客户端实现
- ✅ 提升了代码可维护性和一致性
- ✅ 符合 ADDP 的 DRY 开发原则
- ✅ 为未来的 Python 模块提供了可复用的基础设施

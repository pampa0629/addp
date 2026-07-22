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
from addp_common.client import SystemClient, DevelopClient, ManagerClient

# 服务间调用 (使用 Internal API Key)
system = SystemClient(
    base_url="http://localhost:8180",
    internal_api_key="your-internal-key"
)
engines = await system.list_internal_engines()

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

addp tools list
addp tools get workflow.validate
addp tool call data.search --arguments '{"query":"城市","limit":10}'
# 外部 Agent 可显式传入稳定审计绑定
addp tool call workflow.validate --agent-run-id <run-id> --tool-call-id <call-id> --arguments '{...}'
```

成功响应的 stdout 是 Tool 结果紧凑 JSON；失败响应也是标准错误 JSON，并返回非零 exit code。

`workflow.run` 使用 Develop owner approval 的两阶段单一路线：首次输入 `workflow_engine_id + workflow_definition`，返回 `approval_required`；批准后再次调用时只输入 `approval_id + request_fingerprint`。SDK 不保存审批事实或完整 workflow payload，恢复调用由 Develop 从自己的审批记录读取原请求并一次性消费。

## 认证方式

支持两种认证方式:

1. **ADDP 内部服务间调用**: 使用经过服务端校验的 `internal_api_key` 参数；外部 Agent 不得使用
2. **用户请求**: Web 使用 HttpOnly Refresh Token Cookie，CLI 使用 OS Keychain 保存轮换 Refresh Token
3. **显式短期 Token**: `--token` / `ADDP_TOKEN` 只用于调用方已经持有短期 Access Token 的自动化环境

CLI Browser Login 按 RFC 8252 在 `127.0.0.1` 绑定随机空闲端口，并在授权与 Code 兑换阶段使用同一个完整 redirect URI。CLI 每次执行 Tool 前按 ADDP Base URL 获取跨进程锁，再使用 Keychain 中的 Refresh Token 换取短期 User Access Token，并原子保存轮换后的 Refresh Token；User Access Token 不持久化。ToolExecutor 再按调用即时换取不可刷新、默认 2 分钟的 Delegated Access Token，原始 User Access Token 不进入 owner Client。

Tool 与 Adapter 变更至少运行 common-python 全量测试和 `make test-agent-eval`；场景与发布门禁规则见 `docs/spec/addp智能体评测规范.md`。

## 开发

```bash
cd common-python
pip install -e ".[dev]"
pytest
```

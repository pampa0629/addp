# API AI-Agent友好性规范

[返回 API 设计规范](../addp-API设计规范.md)

定义 HTTP API 的 x-ai-hint、error_type 与 retry_after 提示。Agent 委托身份见[认证授权](认证授权.md#agent-调用其他模块的认证)，ToolExecutor 的工具错误分类见[Tool 开放规范](../addp智能体Tool开放规范.md#七错误与安全)。

阅读导航：[AI Agent 友好性规范](#ai-agent-友好性规范)。

## AI Agent 友好性规范

AI agent 调用 API 时需要理解接口语义、判断错误是否可重试、知道参数从哪里获取。以下规范让 API 对 agent 更友好。

### Swagger 注释中的 x-ai-hint

在 Go handler 注释中添加 `x-ai-hint` 扩展字段，描述 API 用途和参数来源：

```go
// @Summary 执行开发任务
// @Description 执行指定的工作流或 SQL 开发任务
// @Tags 开发任务
// @x-ai-hint 执行用户创建的工作流或 SQL。task_id 可通过 GET /api/v1/develop/task-definitions 获取；engine_id 可通过 GET /api/v1/system/engines 获取
// @Router /api/v1/develop/task-definitions/{id}/execute [post]
func (h *ItemHandler) Execute(c *gin.Context) { ... }
```

在 Python FastAPI 中通过 `openapi_extra` 传入：

```python
@router.post(
    "/chat",
    summary="发送消息",
    openapi_extra={
        "x-ai-hint": "向 AI agent 发送消息。session_id 可通过 GET /api/v1/agent/sessions 获取，不传则自动创建新会话"
    }
)
async def chat(request: ChatRequest): ...
```

### Agent 与 SDK 错误响应增加 error_type

对于 Agent 或 SDK 需要判断是否重试的场景，错误响应可附加 `error_type` 字段（可选）：

```json
// 瞬时错误，可重试
{
  "error": "数据库连接超时",
  "error_type": "transient",
  "retry_after": 5
}

// 永久错误，不应重试
{
  "error": "引擎不存在",
  "error_type": "permanent"
}

// 用户输入错误，需要修正后重试
{
  "error": "SQL 语法错误：第 3 行",
  "error_type": "user_error"
}
```

| error_type | 含义 | agent 行为 |
|------------|------|-----------|
| `transient` | 瞬时错误（超时、连接失败） | 等待 retry_after 秒后重试 |
| `permanent` | 永久错误（资源不存在） | 不重试，报告给用户 |
| `user_error` | 用户输入错误 | 修正输入后重试 |

**注意**：`error_type` 是可选字段，不强制要求所有接口都实现。优先在 Agent、SDK 或 Runtime 频繁调用且确实需要稳定重试判断的接口上添加。

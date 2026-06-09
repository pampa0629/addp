# Monitor provider health 与 capabilities 漂移专题设计

更新时间：2026-06-09

本文承接 TaskProvider capabilities 专题。TaskProvider capabilities 负责声明契约，Monitor 负责运行态观测；二者不得互相替代。

## 一、设计边界

Monitor provider health 只回答三类问题：

1. System 中是否存在启用的 TaskProvider 注册记录。
2. provider 所属模块是否可访问。
3. provider 声明的 task type 是否能通过标准无副作用任务发现 endpoint 访问。

Monitor 不承担以下职责：

| 不承担 | 原因 |
| --- | --- |
| 不复制 capabilities | System 是 TaskProvider 注册事实源，Monitor 只即时读取。 |
| 不新增 TaskProvider 专用 health endpoint | 第一阶段复用模块 `/health` 与标准 `GET /tasks?task_type=`。 |
| 不修复 provider 注册 | 注册、启停、capabilities 校验仍由 System 负责。 |
| 不判断业务产物是否 ready | artifact state 属于 owner 模块专题。 |
| 不替代 Orchestrator 保存期校验 | Orchestrator 仍负责编排引用合法性校验。 |

## 二、健康检查项

Monitor 针对每个 provider 输出 provider 级健康结果。

| 检查项 | 来源 | 状态含义 |
| --- | --- | --- |
| registration | System `task_providers` | provider 是否启用并具备基础 endpoint。 |
| capabilities | provider.capabilities | JSON 是否可解析、`schema_version` 是否为 `task.capabilities/v1`、`task_types` 是否非空。 |
| module_health | provider.base_url + `/health` | 模块进程是否可访问。 |
| task_discovery | provider.base_url + task_list_endpoint + `?task_type=` | 标准任务发现 endpoint 是否可访问。 |

`task_discovery` 必须按 capabilities 中未 deprecated 的 task type 逐项检查。deprecated task type 不作为可用任务类型处理，不进入健康失败统计。

## 三、状态语义

状态只使用四类：

| 状态 | 说明 |
| --- | --- |
| `up` | 所有检查通过。 |
| `degraded` | provider 已注册，但 capabilities 非法、部分 task type 发现失败，或模块健康与任务发现状态不一致。 |
| `down` | 模块 `/health` 不可访问，或所有可用 task type 发现都失败。 |
| `unknown` | 无可检查 task type，或 System 注册信息暂时无法获取。 |

Monitor 不因为 deprecated task type 报错；deleted task type 会表现为 capabilities 缺失或 Orchestrator 执行时报 missing task type。

## 四、调用约定

Monitor 探测标准任务发现 endpoint 时使用服务间调用头：

```http
X-Internal-API-Key: <internal key>
X-Tenant-ID: <current tenant id>
```

如果当前请求没有租户上下文，则不传 `X-Tenant-ID`。模块认证中间件会把内部调用识别为 `internal-api-call`。

任务发现探测只发送 `GET` 请求，不读取 owner 私有表，不触发执行，不创建 execution。

## 五、API 基线

Monitor 后端提供：

| API | 说明 |
| --- | --- |
| `GET /api/v1/monitor/providers/health` | 返回所有 provider 级健康结果。 |
| `GET /api/v1/monitor/providers/{module}/health` | 返回单个 provider 级健康结果。 |

响应中至少包含：

- `module`
- `status`
- `message`
- `module_health`
- `capabilities`
- `task_discovery[]`
- `checked_at`

## 六、后续

前端可在 Dashboard 中把模块健康从“进程健康”升级为“provider 健康”，并增加 capabilities 漂移详情入口。批量编排健康检查可复用 provider health 结果，但仍应由 Orchestrator 校验具体 `provider + task_type + task_id` 引用。

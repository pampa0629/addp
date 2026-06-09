# TaskProvider capabilities 专题设计

更新时间：2026-06-09

本文承接任务体系主干验收后的 TaskProvider capabilities 专题。正式主干约束见 [ADDP 任务体系规范](../spec/addp任务体系规范.md)，本文记录 `task.capabilities/v1` 的收敛方向和本轮已落地改造，不立即要求所有模块实现完整字段级 schema。

ADDP 当前处于积极开发阶段，本文默认 clean break：一旦专题结论确认，旧 capabilities 字段、旧 endpoint 和旧 task type 迁移不保留双轨兼容。

## 一、当前基线

任务体系主干已经完成以下约束：

- TaskProvider 按模块注册，不按任务类型注册。
- 一个 provider 通过 `task_types[]` 声明多个稳定 `task_type`。
- `schema_version` 固定为 `task.capabilities/v1`。
- `definition_schema` 和 `execution_schema` 当前必须是对象 JSON Schema，最小值为 `{ "type": "object" }`。
- `supports_inline_execution` 在 v1 必须为 `false`。
- `supports_cancel` 与 provider 顶层 `task_cancel_endpoint` 必须双向一致。
- `create_url` / `edit_url` 必须是 Console 相对 owner URL。
- Orchestrator 保存编排时校验 `provider + task_type` 已声明且未 deprecated，并对 `execution_schema.additionalProperties=false` 做 Step `parameters` 轻量预校验。

当前实现事实：

| 位置 | 当前职责 |
| --- | --- |
| System `task_provider_service` | 校验 capabilities JSON、标准 endpoint、取消能力一致性和 v1 JSON Schema 子集。 |
| Orchestrator `TaskProviderRegistry` | 缓存 provider，校验 task type 是否声明、deprecated 和不支持参数覆盖的 Step `parameters`。 |
| Orchestrator 前端任务库 | 解析 `task_types[].type/display_name/create_url/edit_url`，不消费 schema 字段。 |
| 各 provider 注册服务 | 不支持参数覆盖的 provider 已声明 `execution_schema.additionalProperties=false`；Develop 保持开放对象 schema；provider 顶层私有扩展统一使用 `x_` 前缀。 |

因此，本专题第一阶段不应直接要求前端按 schema 动态生成所有任务表单，也不应让 Orchestrator 解释 owner 任务定义结构。

## 二、capabilities 的边界

TaskProvider capabilities 只回答三类问题：

1. 这个模块能提供哪些稳定任务类型。
2. 每种任务类型能被 Orchestrator 和 Monitor 以什么公共方式发现、执行、回跳和展示。
3. 每种任务类型允许本次执行覆盖哪些参数，以及是否支持调度、取消、废弃等平台级行为。

TaskProvider capabilities 不承担以下职责：

| 不承担 | 原因 |
| --- | --- |
| 不保存任务定义 | 任务定义归 owner 模块私有表。 |
| 不替代 owner CRUD API | 创建、编辑、校验复杂定义仍在 owner 模块页面和 API 内完成。 |
| 不声明 owner 私有表结构 | Orchestrator 和 Monitor 不应依赖私有表字段。 |
| 不表达执行历史 | execution 统一写入 `common.task_executions`。 |
| 不作为 UI 组件库协议 | v1 可被 UI 读取，但不是完整表单渲染协议。 |

## 三、当前建议结论

为了保持 v1 简洁，本文建议先确认以下结论：

1. `definition_schema` 只作为跨模块可公开字段摘要，不作为 Orchestrator 创建或编辑 owner 任务定义的依据。
2. `execution_schema` 只描述执行请求体中的 `parameters`，不描述 `trigger_type`、`source`、`parent_execution_id` 等统一 envelope 字段。
3. owner 执行入口必须是参数校验的最终责任方；Orchestrator 最多做轻量预校验。
4. v1 不新增 UI schema，不要求 Orchestrator 动态生成完整任务表单。
5. v1 不支持 inline execution，继续只允许 Step 引用已保存的 owner 任务定义。
6. 不支持执行参数覆盖的 provider 必须声明 `execution_schema.additionalProperties=false`，并在执行入口拒绝非空 `parameters`。
7. 标准取消能力必须以真实 worker 中断和状态一致落库为前提；没有这条执行主路径时不得声明 `supports_cancel=true`。
8. deprecated task type 不允许新增编排引用，也不允许已保存编排继续执行；历史 execution 只按执行记录展示，不保留任务定义兼容入口。

一个推荐的 v1 task type 声明形态如下：

```json
{
  "schema_version": "task.capabilities/v1",
  "task_types": [
    {
      "type": "query",
      "display_name": "查询任务",
      "description": "执行已保存的 Develop 查询任务",
      "definition_schema": {
        "type": "object",
        "properties": {
          "name": { "type": "string", "title": "任务名称" },
          "query_type": { "type": "string", "enum": ["sql"] }
        },
        "required": ["name"],
        "additionalProperties": true
      },
      "execution_schema": {
        "type": "object",
        "properties": {
          "limit": { "type": "integer", "minimum": 1, "default": 1000 }
        },
        "additionalProperties": false
      },
      "supports_schedule": false,
      "supports_cancel": false,
      "supports_inline_execution": false,
      "create_url": "/develop/sql?action=create",
      "edit_url": "/develop/sql?action=edit&id=:id",
      "deprecated": false
    }
  ]
}
```

这个示例只表达 capabilities 结构，不要求 Develop 当前立即补齐同等字段级 schema。

## 四、schema 字段语义

### 1. `definition_schema`

`definition_schema` 描述“持久任务定义的可公开结构摘要”，用于跨模块理解和轻量展示，不用于 Orchestrator 创建、编辑或渲染完整 owner 任务定义。

允许表达：

- owner 任务定义中可公开、稳定、适合展示或筛选的字段。
- 字段类型、枚举、必填项、默认值、描述。
- 与任务库展示有关的只读摘要字段。

不允许表达：

- owner 私有表的完整列结构。
- 凭据、密钥、连接串、token 等敏感字段。
- 只在某个 UI 步骤中临时存在的表单状态。
- 需要 owner 后端动态计算的复杂校验规则。

第一阶段建议仍允许最小 schema：

```json
{
  "type": "object"
}
```

当 owner 模块愿意细化时，建议使用 JSON Schema 2020-12 的子集：

```json
{
  "type": "object",
  "properties": {
    "name": {
      "type": "string",
      "title": "任务名称"
    },
    "schedule": {
      "type": "string",
      "title": "Cron 表达式"
    }
  },
  "required": ["name"],
  "additionalProperties": true
}
```

约束：

1. 根节点必须是 `type=object`。
2. `properties`、`required`、`enum`、`default`、`description`、`title` 可以使用。
3. `additionalProperties` 默认视为 `true`，因为 owner 定义可能包含未公开的私有字段。
4. 不使用 `$ref`、远程 schema、`oneOf`、`anyOf`、`allOf` 作为第一阶段跨模块契约。
5. 不把 `definition_schema` 作为迁移脚本或 DB schema 来源。

### 2. `execution_schema`

`execution_schema` 描述 `POST /tasks/{task_type}/{id}/execute` 请求体中 `parameters` 允许携带的本次执行覆盖参数。

它不描述整个执行请求体。统一请求体仍固定为：

```json
{
  "trigger_type": "manual",
  "source": "orchestrator",
  "parent_execution_id": "uuid",
  "parameters": {}
}
```

允许表达：

- 本次执行可覆盖的非敏感参数。
- 参数类型、枚举、默认值、说明。
- owner 明确支持的运行时开关，例如 dry run、最大行数、临时输入值。

不允许表达：

- 修改任务定义的字段。
- 凭据、密钥和连接信息。
- 需要 owner 长事务保存的配置。
- owner 不支持但选择静默忽略的参数。

核心约束：

1. provider 不支持参数覆盖时，`execution_schema` 仍可为 `{ "type": "object", "additionalProperties": false }`。
2. provider 收到 schema 外参数时应返回 400，不得静默忽略。
3. Orchestrator 可以在保存编排时做轻量 schema 预校验，但最终校验必须由 owner 执行入口完成。
4. `parameters` 的值必须写入本次 execution 的 `execution_config` 或 owner 可追溯的执行快照中。

## 五、前端 schema 与 UI schema

JSON Schema 只表达数据契约，不表达完整交互。

第一阶段建议：

- Orchestrator 任务库继续使用 `display_name`、`description`、`create_url`、`edit_url` 做发现和跳转。
- 编排步骤的 `parameters` 暂时保留 JSON 编辑或轻量键值配置。
- owner 模块的创建和编辑体验仍由 owner 专属页面负责。
- 不在 `task.capabilities/v1` 中新增 UI schema。

如果后续需要动态表单，应作为 `task.capabilities/v2` 或独立字段设计，不直接把 UI 细节塞进 `definition_schema`：

```json
{
  "schema_version": "task.capabilities/v2",
  "task_types": [
    {
      "type": "query",
      "definition_schema": {},
      "execution_schema": {},
      "execution_ui_schema": {}
    }
  ]
}
```

进入 UI schema 前必须先确认：

1. Orchestrator 是否真的需要编辑参数表单，而不是跳转 owner 页面。
2. 前端是否使用同一套组件和校验库。
3. i18n 文案如何归属，是 owner 提供还是 Console / Orchestrator 翻译。
4. 动态选项如何加载，是否需要 owner 提供 options endpoint。

## 六、标准取消能力

`supports_cancel=true` 不是“有取消按钮”，而是 owner 执行体具备真实取消契约。

一个 task type 只有同时满足以下条件，才能声明 `supports_cancel=true`：

| 条件 | 说明 |
| --- | --- |
| 可定位 | 标准取消入口使用 `execution_id` 能定位到真实运行中的 execution。 |
| 可中断 | worker、队列或执行引擎能接收取消信号。 |
| 可清理 | 临时文件、连接、锁、部分产物和队列消息可清理或标记。 |
| 状态一致 | 最终必须落库为统一状态 `cancelled` 或已完成终态，不产生悬挂 running。 |
| 幂等 | 重复取消同一 execution 不产生额外副作用。 |
| 可观测 | Monitor 能看到取消请求、最终状态和必要诊断信息。 |

标准 endpoint 固定为：

```text
POST /executions/{execution_id}/cancel
```

取消请求体第一阶段可以为空。若后续需要取消原因，建议统一为：

```json
{
  "reason": "user_request"
}
```

不满足以上条件时，模块内部可以保留私有取消能力，但不得声明 TaskProvider 标准取消能力，也不得让 Orchestrator 展示标准取消入口。

## 七、inline execution

`supports_inline_execution` 在 `task.capabilities/v1` 必须继续为 `false`。

inline execution 不是把 `task_id` 设为空这么简单。它会改变 Orchestrator Step 模型，因为 Step 不再引用 owner 已保存任务定义，而是携带一份一次性执行配置。

如果后续进入 v2，至少需要先定义：

| 主题 | 需要确认 |
| --- | --- |
| endpoint | 是否新增 `POST /tasks/{task_type}/execute`，还是沿用其他标准路径。 |
| schema | inline 配置使用 `inline_execution_schema`，不能复用 `execution_schema`。 |
| Step 模型 | Step 需要保存 `provider + task_type + inline_config`，且不能同时保存 `task_id`。 |
| 审计 | ad-hoc execution 必须在 `execution_config` 保存完整配置快照。 |
| UI | Orchestrator 是否承载配置表单，还是跳 owner 临时执行页面。 |
| 权限 | 谁允许创建 inline execution，是否等价于 owner 创建任务权限。 |

在这些问题确认前，不得通过 capabilities 布尔值单独打开 inline execution。

## 八、版本演进与 deprecated task type

### 1. capabilities 版本

`schema_version` 是 capabilities 文档结构版本，不是单个 task type 的业务版本。

版本演进规则：

| 变更 | 是否可在 v1 内进行 |
| --- | --- |
| 增加可选展示字段 | 可以。 |
| 细化 `definition_schema` / `execution_schema` 的 properties | 可以，但不得破坏现有执行。 |
| 新增 task type | 可以，需同步规范、Swagger 和运行态验证。 |
| 删除 task type | 不可以，应先 deprecated。 |
| 改变 endpoint 语义 | 不可以，需要 clean break 或新版本。 |
| 打开 inline execution | 不可以，需要 v2。 |
| 引入 UI schema | 不建议在 v1 内做平台级依赖。 |

### 2. deprecated task type

`deprecated=true` 表示该任务类型不再作为可用任务类型处理。ADDP 当前不为废弃任务类型保留兼容迁移路径。

约束：

1. Orchestrator 保存编排时必须拒绝引用 deprecated task type。
2. Orchestrator 执行已保存编排时也必须拒绝 deprecated task type，不允许旧编排绕过保存期校验继续运行。
3. Monitor 查询历史 execution 时不得因为 task type deprecated 而失败。
4. 历史 execution 只按既有 execution 记录展示；owner 不需要为 deprecated task type 继续提供可编辑任务定义入口。
5. owner 可以按 clean break 删除 deprecated task type；删除后 Orchestrator 对引用该类型的编排按 missing task type 处理。

## 九、缓存刷新、漂移检测和告警

Orchestrator 可以缓存 TaskProvider capabilities，但 System 是注册事实源。

建议的漂移检测边界：

| 检测项 | 处理方式 |
| --- | --- |
| provider 无法访问 | Monitor provider health 告警，Orchestrator 执行时报错。 |
| capabilities JSON 非法 | System 注册时拒绝；运行态发现则标记 provider invalid。 |
| endpoint 不符合标准 | System 注册时拒绝。 |
| task type 删除或 deprecated | Orchestrator 保存和执行编排时直接拒绝引用；不做兼容迁移。 |
| schema 收紧导致已有参数不合法 | owner 执行入口拒绝，并在编排健康检查中提示修订。 |
| provider 注册内容与 Swagger 不一致 | 作为模块 CI 或运行态健康检查专题处理。 |

第一阶段可先实现低成本机制：

1. Orchestrator 提供显式刷新 provider cache 的内部方法或管理入口。
2. Monitor provider health 读取 System 注册信息，复用模块 `/health` 和标准 `GET /tasks?task_type=` 做无副作用探活，不新增 TaskProvider 专用 health endpoint。
3. 保存编排时记录当时的 `provider + task_type`，但不复制完整 capabilities，避免形成第二事实源。

## 十、建议分阶段推进

### 阶段 1：文档确认

- 本文先确认 schema、取消、inline、deprecated 和漂移检测边界。
- 暂不要求各模块补全字段级 schema。
- 暂不修改 Orchestrator 参数表单。

### 阶段 2：System 校验增强（本轮已完成）

在不破坏最小 schema 的前提下，System 已增强校验：

- `definition_schema` / `execution_schema` 只允许 JSON Schema 子集。
- `execution_schema.additionalProperties=false` 与 owner 执行入口拒绝非空 `parameters` 的契约已进入正式规范。
- 顶层 provider 私有扩展字段必须使用 `x_` 前缀；未加 `x_` 的未知顶层字段由 System 注册入口拒绝。

### 阶段 3：owner 执行入口参数校验（本轮已完成基线）

本轮确认并收敛：

- Meta、Transfer、Manager、Quality、Graph、Orchestrator 不支持参数覆盖，执行入口拒绝非空 `parameters`，capabilities 声明 `additionalProperties=false`。
- Develop 支持执行参数传入，执行器会合并默认参数和本次参数，并写入 `common.task_executions.execution_config.inputs.parameters`。
- Develop TaskProvider 执行入口已校验路径 `task_type` 与真实 `dev_type` 一致。

### 阶段 4：Monitor / Orchestrator 健康检查

- Monitor provider health、capabilities 漂移视图和批量编排健康检查并入 Monitor 专题统一实现。
- Orchestrator 保存和执行编排时已校验 deprecated、missing task type，并在 `execution_schema.additionalProperties=false` 时拒绝 schema 未声明的 Step `parameters`。更完整的编排定义健康检查后续仍可扩展到批量巡检和 UI 提示。

## 十一、维护检查矩阵

后续维护 capabilities 或进入下一阶段专题时，至少需要按以下矩阵检查：

| 模块 | 检查项 | 当前建议 |
| --- | --- | --- |
| System | 注册入口是否继续拒绝非标准 endpoint、非法 `task_type`、inline=true 和 cancel 不一致。 | 保持并补充 schema 子集校验。 |
| System | provider 顶层私有扩展字段命名。 | 只允许 `x_` 前缀私有扩展；未知标准前缀字段必须拒绝。 |
| Orchestrator 后端 | 保存和执行编排时是否校验 deprecated 和 missing task type。 | 本轮已完成；后续可增加批量健康检查，不复制 capabilities。 |
| Orchestrator 后端 | 是否按 `execution_schema` 预校验 `parameters`。 | 本轮已完成 `additionalProperties=false` 的保存期轻量预校验；不能替代 owner 校验。 |
| Orchestrator 前端 | 是否动态渲染 owner 任务定义表单。 | v1 不做；继续跳转 owner `create_url` / `edit_url`。 |
| Owner provider | 不支持参数覆盖时是否拒绝非空 `parameters`。 | 本轮已确认并对齐 capabilities。 |
| Owner provider | 执行快照是否保存 `parameters`。 | Develop 已保存；其他 provider 不支持覆盖。 |
| Monitor | 是否展示 provider health 和 capabilities 漂移。 | 并入 Monitor 专题；探活契约已确认复用模块 `/health` 与标准 `GET /tasks?task_type=`。 |

最小验证命令：

```bash
cd system/backend && go test ./...
cd orchestrator/backend && go test ./...
git diff --check -- docs/next/TaskProvider\ capabilities专题设计.md docs/next/任务体系后续专题清单.md docs/spec/addp任务体系规范.md system/backend/internal/service/task_provider_service.go system/backend/internal/service/task_provider_service_test.go orchestrator/backend/internal/service/task_provider_registry.go orchestrator/backend/internal/service/task_provider_registry_test.go orchestrator/backend/internal/service/executor.go orchestrator/backend/internal/service/executor_test.go
```

如果进入 owner provider 参数校验，还应按实际修改模块追加对应模块后端测试，例如：

```bash
cd develop/backend && go test ./...
cd manager/backend && go test ./...
cd transfer/backend && go test ./...
```

## 十二、本专题暂不处理

以下内容不进入本专题当前基线：

- 不要求各模块立即补全字段级 `definition_schema` / `execution_schema`。
- 不新增 UI schema。
- 不开放 inline execution。
- 不声明任何模块支持标准取消能力。
- 不调整 Orchestrator Step 模型。
- 不新增 TaskProvider 专用 health endpoint。

## 十三、后续专题归属

1. Monitor provider health、capabilities 漂移视图和批量编排健康检查并入 Monitor 专题统一实现。
2. 如后续需要 UI schema、inline execution 或更完整 JSON Schema 能力，应进入 `task.capabilities/v2`，不得在 v1 中以兼容分支补丁式打开。

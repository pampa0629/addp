# orchestrations 表结构和 API 说明

本篇维护定义字段与创建示例；表关系、授权主体字段、执行快照和迁移见[数据库架构](../数据库架构.md)，DAG 与执行行为见[编排规范](../../../docs/spec/tasks/编排与调度.md#orchestrator-规范)，参数传值示例见[执行参数专题](../参数化模板说明.md)。

## 一、表定位

`orchestrator.orchestrations` 保存跨模块任务编排定义。每条记录是一份可执行 DAG，步骤引用各模块通过 TaskProvider 暴露的已有任务定义。

Orchestrator 不直接调用计算引擎；工作流算子应先在 Develop 中形成 `workflow` 任务，再由 Orchestrator 引用。

## 二、核心字段

| 字段名 | 类型 | 说明 |
| --- | --- | --- |
| `id` | SERIAL | 编排定义 ID |
| `tenant_id` | INTEGER | 租户 ID |
| `name` | VARCHAR(128) | 编排名称 |
| `description` | VARCHAR(512) | 编排描述 |
| `steps` | JSONB | DAG 步骤定义 |
| `editor_layout` | JSONB | 编辑器节点坐标和视口，仅用于展示，不参与执行 |
| `enabled` | BOOLEAN | 是否启用定时调度 |
| `schedule` | VARCHAR(128) | Cron 表达式 |
| `last_execution_id` | VARCHAR(36) | 最近一次父 execution UUID |
| `last_execution_status` | VARCHAR(20) | 最近一次父 execution 状态 |
| `last_run_at` | TIMESTAMP | 最近执行时间 |
| `next_run_at` | TIMESTAMP | 下一次计划执行时间 |
| `created_at` / `updated_at` / `deleted_at` | TIMESTAMP | 审计字段 |

## 三、Step JSON 结构

Step 的字段集合与绑定约束沿[编排定义规范](../../../docs/spec/tasks/编排与调度.md#orchestrator-规范)，Go 持久化结构见[模型源码](../../backend/internal/models/orchestration.go)。下方创建示例展示同一 `steps` 数组，不另维护一份结构示例。

字段约束：

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `id` | 是 | 编排内唯一 |
| `name` | 是 | 步骤名称 |
| `provider` | 是 | TaskProvider 模块名 |
| `task_type` | 是 | provider 声明的任务类型 |
| `task_id` | 是 | owner 模块内任务定义 ID |
| `parameters` | 否 | 保存显式覆盖值或声明输出绑定；本次解析值不写回编排或 owner 任务定义 |
| `depends_on` | 是 | 前置步骤 ID |
| `timeout` | 否 | 超时秒数 |

## 四、执行语义

编辑器布局使用独立顶层字段：

```json
{
  "nodes": {
    "scan": { "x": 120, "y": 240 }
  },
  "viewport": {
    "zoom": 1,
    "translate_x": 0,
    "translate_y": 0
  }
}
```

`editor_layout` 不属于 `steps[]`，Executor 不得读取它。

执行身份和父子关联沿[编排规范](../../../docs/spec/tasks/编排与调度.md#orchestrator-规范)，监督器入队、租约与失败边界沿[执行可靠性](../../../docs/spec/tasks/编排与调度.md#orchestrator-执行可靠性)。入队时冻结 Steps，父执行的步骤阶段和子 execution 引用持久化位置见[数据库执行流程](../数据库架构.md#五执行流程)，不能由当前定义或编辑器布局补造历史。

## 五、API 示例

以下为“扫描成功后执行工作流”的纯控制依赖示例，没有把扫描响应作为工作流输入。先创建并核对当前租户可用的 Meta `scan` 和 Develop `workflow` 任务，将示例 ID 替换为实际值；两者须已保存满足其具体 `execution_contract` 的配置。若需要上游传值，按[参数绑定专题](../参数化模板说明.md)选择来源任务已声明的稳定输出，不假定扫描提供 `item_id` 输出。

`ORCHESTRATOR_BASE_URL` 表示本次实际 Backend 地址（不含 API 前缀），从标准启动输出核对；`TOKEN` 须为当前 Tenant User 凭据。创建还需 `orchestrator.workflow.create`、`orchestrator.workflow.execute` 与 `system.execution_authorization.create`，主体绑定及服务间授权边界见[模块契约](../../CLAUDE.md#执行参数与授权契约)。示例未执行不算实际创建或链路验收通过。

```bash
curl -X POST "$ORCHESTRATOR_BASE_URL/api/v1/orchestrator/orchestrations" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "数据处理流水线",
    "description": "扫描元数据后执行 Develop 工作流任务",
    "steps": [
      {
        "id": "scan",
        "name": "扫描元数据",
        "provider": "meta",
        "task_type": "scan",
        "task_id": 1,
        "parameters": {},
        "depends_on": [],
        "timeout": 600
      },
      {
        "id": "workflow",
        "name": "空间分析工作流",
        "provider": "develop",
        "task_type": "workflow",
        "task_id": 8,
        "parameters": {},
        "depends_on": ["scan"],
        "timeout": 1800
      }
    ],
    "editor_layout": {
      "nodes": {
        "scan": { "x": 120, "y": 240 },
        "workflow": { "x": 360, "y": 240 }
      },
      "viewport": { "zoom": 1, "translate_x": 0, "translate_y": 0 }
    },
    "enabled": true,
    "schedule": "0 2 * * *"
  }'
```

## 六、注意事项

- `provider + task_type + task_id` 是 Step 的唯一任务引用方式。
- 不允许旧的非 TaskProvider 调用模式。
- Develop 的算子工作流通过 `task_type=workflow` 进入 Orchestrator。
- 编排自身也可以作为 `task_type=orchestration` 写入统一 execution 体系。
- 保存与执行前的具体任务契约复核、默认值和声明输出绑定校验统一见[编排规范](../../../docs/spec/tasks/编排与调度.md#orchestrator-规范)与[默认值校验](../../../docs/spec/tasks/TaskProvider接口.md#orchestrator-默认值校验)。
- 标准验证入口与分层边界见[开发与验证](../../CLAUDE.md#开发与验证)，固定版本、失败和未覆盖范围见[历史证据导航](../../CLAUDE.md#历史证据导航)；定义保存成功、受控 Provider 回归和历史证据均不替代当前版本的真实执行验收。

## 七、相关文档

- [任务体系规范](../../../docs/spec/addp任务体系规范.md)
- [数据库架构](../数据库架构.md)
- [参数化模板说明](../参数化模板说明.md)

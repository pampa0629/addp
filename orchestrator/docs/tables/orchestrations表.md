# orchestrations 表结构和 API 说明

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

```go
type Step struct {
    ID         string                 `json:"id"`
    Name       string                 `json:"name"`
    Provider   string                 `json:"provider,omitempty"`
    TaskType   string                 `json:"task_type,omitempty"`
    TaskID     uint                   `json:"task_id,omitempty"`
    Parameters map[string]interface{} `json:"parameters"`
    DependsOn  []string               `json:"depends_on"`
    Timeout    int                    `json:"timeout"`
}
```

示例：

```json
{
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
      "name": "执行空间分析工作流",
      "provider": "develop",
      "task_type": "workflow",
      "task_id": 8,
      "parameters": {
        "input": "{{scan.item_id}}"
      },
      "depends_on": ["scan"],
      "timeout": 1800
    }
  ]
}
```

字段约束：

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `id` | 是 | 编排内唯一 |
| `name` | 是 | 步骤名称 |
| `provider` | 是 | TaskProvider 模块名 |
| `task_type` | 是 | provider 声明的任务类型 |
| `task_id` | 是 | owner 模块内任务定义 ID |
| `parameters` | 否 | 本次执行参数，不直接改写任务定义 |
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

1. Orchestrator 创建父 execution：`module=orchestrator`、`task_type=orchestration`。
2. Executor 对 `steps` 做 DAG 拓扑排序。
3. 每个 Step 调用对应 provider 的 `task_execute_endpoint`。
4. 下游 execution 写入 `parent_execution_id`。
5. 父 execution 的 `metadata` 保存步骤结果摘要。

## 五、API 示例

```bash
curl -X POST http://localhost:8084/api/v1/orchestrator/orchestrations \
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
        "parameters": {
          "source_item": "{{scan.item_id}}"
        },
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
- 创建和更新编排时会校验 Step：`provider`、`task_type`、`task_id` 必填，依赖步骤必须存在，DAG 不允许成环。

## 七、相关文档

- [任务体系规范](../../../docs/spec/addp任务体系规范.md)
- [数据库架构](../数据库架构.md)
- [参数化模板说明](../参数化模板说明.md)

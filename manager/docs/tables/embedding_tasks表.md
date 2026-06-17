# embedding_tasks 表结构说明

> 状态：当前实现说明。`manager.embedding_tasks` 表达向量化任务定义，TaskProvider `task_type=embedding`。

## 一、表定位

`manager.embedding_tasks` 回答：

> 以后按什么范围和策略反复执行向量化。

只有独立向量化页面创建的配置才写入该表。资源树 item / node 点击“向量化”属于一次性 execution，不创建任务定义。

它不替代：

1. `manager.embeddings` 的结果状态。
2. `common.task_executions` 的执行历史。
3. item 级可向量化判断。

## 二、目标核心字段

公共字段遵守 `docs/spec/addp任务体系规范.md`。

| 字段名 | 当前类型 / 语义 | 说明 |
| --- | --- | --- |
| `id` | bigint | Manager 内部任务定义 ID |
| `tenant_id` | integer | 租户 ID |
| `name` | varchar | 任务名称 |
| `description` | text | 任务描述 |
| `enabled` | boolean | 是否启用调度 |
| `schedule` | varchar | 调度表达式，空表示只手动执行 |
| `next_run_at` | timestamp | 下次计划运行时间 |
| `last_run_at` | timestamp | 最近运行时间 |
| `last_execution_id` | varchar | 最近一次 `common.task_executions.execution_id` |
| `last_execution_status` | varchar | 最近一次执行状态，使用统一 execution status |
| `config` | jsonb | 向量化任务私有配置 |
| `created_by` | integer | 创建人 |
| `created_at` / `updated_at` / `deleted_at` | timestamp | 生命周期字段 |

## 三、config 语义

```json
{
  "target": {
    "scope": "node",
    "engine_id": 1,
    "node_id": 23,
    "locator": "addp://engine/1/path/addp/reports?type=directory&node_id=23",
    "recursive": true
  },
  "filters": {
    "max_file_size_mb": 10
  },
  "embedding": {
    "model": "qwen3-vl-embedding",
    "dimension": 2560
  }
}
```

约束：

1. 持久化任务范围以 node 或等价范围为主。
2. 单 item 向量化优先走资源树一次性 execution。
3. `filters` 不能替代 item 级可向量化判断；执行时仍逐 item 判断格式、大小、内容可读性和模型能力。
4. 当前阶段 `embedding.model` 和 `embedding.dimension` 必须与 Manager 当前启用配置一致。

## 四、TaskProvider 入口

标准任务入口：

```text
GET  /api/v1/manager/tasks?task_type=embedding
GET  /api/v1/manager/tasks/embedding/{id}
POST /api/v1/manager/tasks/embedding/{id}/execute
GET  /api/v1/manager/executions/{execution_id}
```

模块私有 CRUD：

```text
GET    /api/v1/manager/embedding_tasks
POST   /api/v1/manager/embedding_tasks
GET    /api/v1/manager/embedding_tasks/{id}
PUT    /api/v1/manager/embedding_tasks/{id}
DELETE /api/v1/manager/embedding_tasks/{id}
```

私有 CRUD 不替代 TaskProvider 标准入口。Orchestrator 只能通过 TaskProvider 标准入口发现和执行向量化任务。

## 五、相关文档

- [向量化概念说明](../向量化概念说明.md)
- [向量化能力说明](../向量化能力说明.md)
- [embeddings 表结构说明](./embeddings表.md)
- [数据库架构](../数据库架构.md)

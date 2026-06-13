# Manager 瓦片缓存生成任务设计

> 状态：next 设计稿。本文承接 [Manager 快显与瓦片缓存概念原则](Manager快显与瓦片缓存概念原则.md) 和 [Manager 瓦片缓存结果状态设计](Manager瓦片缓存结果状态设计.md)，用于细化瓦片缓存生成如何纳入统一任务体系。
>
> 后续代码改造顺序见 [Manager 快显与瓦片缓存实现改造清单](Manager快显与瓦片缓存实现改造清单.md)。

## 一、设计结论

瓦片缓存生成必然是任务。

原因：

1. 生成瓦片缓存通常耗时较长，不能被设计成普通预览页上的轻量即时动作。
2. 生成过程需要进度、状态、错误、取消、重试、审计和监控。
3. 生成结果会产生可复用的瓦片缓存结果。
4. 生成配置可能需要存储、层级、范围、格式、准备动作等多项选择。

目标命名：

| 对象 | 目标命名 |
| --- | --- |
| 任务类型 | `tile_cache_generation` |
| 任务定义表 | `manager.tile_cache_tasks` |
| 任务私有配置字段 | `config` JSONB |
| 产物状态表 | `manager.tile_cache` |
| 快显状态表 | `manager.quick_view` |
| 执行记录表 | `common.task_executions` |

`manager.tile_cache_tasks` 必须带 `tasks`，和 `meta.scan_tasks`、`transfer.transfer_tasks`、`manager.embedding_tasks` 等任务定义表保持一致。

`tile_cache_generation` / `manager.tile_cache_tasks` 是目标任务语义。旧 MVT-only 任务类型和任务表不应继续保留。

## 二、核心边界

### 1. 任务类型不是瓦片格式

`tile_cache_generation` 表达“生成瓦片缓存”这类任务能力。

`mvt` 表达瓦片格式。

因此：

1. 不应把任务类型命名为 MVT-only 语义。
2. 不应为 MVT、栅格瓦片、预渲染图片瓦片分别建立平行任务体系。
3. 瓦片格式应进入任务 `config` 和产物状态。

### 2. 任务定义不是执行记录

`manager.tile_cache_tasks` 表达未来可执行、可追踪、可调度、可编排的生成配置。

`common.task_executions` 表达某一次实际执行。

任务定义只保存最近执行摘要，不保存每次执行历史。

### 3. 执行记录不是产物状态

一次 execution 成功，只说明这次执行成功结束。

瓦片缓存结果是否可用、存在哪里，应以 `manager.tile_cache` 为事实源。

### 4. 快显不是任务

快显是 Manager 空间预览中的显示模式。

瓦片缓存生成任务可以生产或刷新快显可消费的瓦片缓存结果，但快显本身不应被称为任务。

## 三、预览页入口规则

Manager 空间预览页应尽量降低用户认知负担。

### 1. 不可快显但可生成

当能力判断为：

```text
can_use_quick_view=false
can_generate_tile_cache=true
```

预览页只展示“生成瓦片缓存”按钮。

用户点击后，必须跳转到瓦片缓存页面的“任务”tab，并携带当前 item 上下文。用户在任务 tab 完成配置并创建 `manager.tile_cache_tasks`，再选择是否立即执行。

### 2. 已可快显

当能力判断为：

```text
can_use_quick_view=true
```

预览页展示“切换快显”入口，不展示“生成瓦片缓存”按钮。

如果后续需要刷新、重建或生成另一份瓦片缓存，应从瓦片缓存页面的“任务”tab 或“结果”tab 发起，而不是在预览页增加额外按钮。

### 3. 不可快显也不可生成

当能力判断为：

```text
can_use_quick_view=false
can_generate_tile_cache=false
```

预览页不展示生成按钮，只展示不可用原因，例如空间元数据不足、引擎不支持、权限不足、CRS 无法可靠处理或缺少可生成条件。

## 四、瓦片缓存页面

Manager 应提供一个独立的瓦片缓存页面，参考向量化页面的组织方式，页面内分为两个 tab：

| tab | 职责 |
| --- | --- |
| 任务 | 管理 `manager.tile_cache_tasks`，支持创建、编辑、执行、调度、删除、最近执行摘要和跳转 Monitor。 |
| 产物 | 管理 `manager.tile_cache`，支持查看 ready / failed 等状态、定位源 item、查看存储引用、删除或跳转 Monitor。 |

“任务”tab 回答：

> 以后按什么配置生成或刷新瓦片缓存。

“结果”tab 回答：

> 当前已经有哪些瓦片缓存结果，它们是否可用，存在哪里，来自哪个 item。

瓦片缓存页面承担：

1. 从空间预览页带 item 上下文进入后的任务创建。
2. 用户独立创建瓦片缓存生成任务。
3. 编辑已有瓦片缓存任务。
4. 选择是否立即执行。
5. 查看任务最近执行摘要并跳转 Monitor 查看具体 execution。
6. 查看和管理瓦片缓存结果列表。

不存在单独的“瓦片缓存生成页面”和“瓦片缓存任务页面”两套入口。统一入口是瓦片缓存页面，生成配置发生在“任务”tab，结果查看和清理发生在“结果”tab。

从预览页跳转时，页面应默认进入“任务”tab 的创建态，自动带入当前 item 信息，并将目标范围锁定或默认设为当前 item，避免用户重新定位数据项。

## 五、任务定义表语义

`manager.tile_cache_tasks` 表达“未来按什么配置生成瓦片缓存”。

公共字段必须遵守 `docs/spec/addp任务体系规范.md`，并参考 `manager.embedding_tasks` 的设计保持一致。

| 字段 | 说明 |
| --- | --- |
| `id` | Manager 内部任务定义 ID |
| `tenant_id` | 租户 ID |
| `name` | 任务名称 |
| `description` | 任务描述 |
| `enabled` | 是否启用定时或自动触发 |
| `schedule` | Cron 表达式，空表示只手动执行 |
| `next_run_at` | 下一次计划运行时间 |
| `last_run_at` | 最近运行时间 |
| `last_execution_id` | 最近一次 `common.task_executions.execution_id` |
| `last_execution_status` | 最近一次执行状态，必须使用统一 execution status |
| `config` | 瓦片缓存生成私有配置，JSONB |
| `created_by` | 创建人 |
| `created_at` / `updated_at` / `deleted_at` | 任务定义生命周期字段 |

除公共字段、最近执行摘要和审计字段外，瓦片缓存生成的私有策略应尽量合并到 `config`。只有存在明确查询、排序、唯一约束、权限过滤或生命周期管理需要时，才考虑拆成独立列。

## 六、config 语义

`config` 保存瓦片缓存生成策略。

建议结构：

```json
{
  "target": {
    "item_fingerprint": "string",
    "locator": "string",
    "source_engine_id": 1,
    "schema": "public",
    "table": "roads",
    "item_id": 123
  },
  "tile": {
    "format": "mvt",
    "tile_matrix_set": "WebMercatorQuad",
    "min_zoom": 0,
    "max_zoom": 12,
    "extent": null,
    "extent_srid": null,
    "target_srid": 3857
  },
  "storage": {
    "storage_ref": "string"
  },
  "preparation": {
    "mode": "auto",
    "allow_materialized_view": null,
    "allow_index": null,
    "allow_analyze": null
  },
  "options": {
    "geometry_column": "string",
    "attributes": [],
    "simplification": null
  }
}
```

说明：

1. `target` 表达生成对象。创建和更新任务时由后端规范化为 `source_engine_id + schema + table + item_fingerprint + locator`，`item_id` 只作为当前 Meta item 行的可选回跳引用；执行阶段只消费该规范结构。
2. `tile` 表达瓦片格式、矩阵集、层级、范围和目标 SRID。
3. `storage` 表达目标存储选择。第一阶段可以只有默认存储，但结构不能锁死为固定 MinIO 路径。
4. `preparation.mode` 建议支持 `auto` / `enabled` / `disabled`。是否需要准备动作应根据数据量、SRID、索引、已有产物、引擎能力和用户授权综合判断，不能一概而论。
5. `options` 表达当前格式或引擎需要的生成选项。

以上是语义示例，不是最终 API schema。

## 七、执行语义

瓦片缓存任务执行通过 TaskProvider 标准入口：

```text
GET  /api/v1/manager/tasks?task_type=tile_cache_generation
GET  /api/v1/manager/tasks/tile_cache_generation/{id}
POST /api/v1/manager/tasks/tile_cache_generation/{id}/execute
GET  /api/v1/manager/executions/{execution_id}
```

当前 `tile_cache_generation.supports_cancel=false`，未实现可靠执行中断、资源清理和状态一致落库前，不注册也不开放标准取消入口。后续若切换到 Manager Worker 或 GIS 执行引擎，也必须先具备真实取消能力，才可按任务体系规范开放 `POST /api/v1/manager/executions/{execution_id}/cancel`。

执行记录写入：

| 字段 | 取值 |
| --- | --- |
| `module` | `manager` |
| `task_type` | `tile_cache_generation` |
| `source_task_id` | `manager.tile_cache_tasks.id` 的十进制字符串 |
| `source_task_name` | 任务名称 |
| `trigger_type` | `manual` / `scheduled` |
| `source` | `console`、`manager`、`orchestrator` 或其他触发来源 |
| `execution_config` | 任务定义当时的完整 `config` 快照 |
| `metadata` | 结果摘要、产物 ID、瓦片数量、大小、步骤统计等 |
| `error_details` | 错误类型、消息和诊断信息 |

瓦片缓存生成不设计无任务定义的 ad-hoc execution。即使从预览页带 item 跳转，也必须先创建 `manager.tile_cache_tasks`，再执行。

## 八、调度语义

调度字段遵守任务体系规范，并与 `manager.embedding_tasks` 保持一致：

| 字段 | 说明 |
| --- | --- |
| `enabled` | 是否启用任务。 |
| `schedule` | 调度表达式。空值表示只手动执行。 |
| `next_run_at` | 下次计划运行时间。 |
| `last_run_at` | 最近运行时间。 |
| `last_execution_id` / `last_execution_status` | 最近一次 execution 摘要。 |

约束：

1. 只有持久化任务定义参与调度。
2. `last_execution_status` 必须使用统一 execution status：`pending`、`running`、`success`、`failed`、`timeout`、`cancelled`。
3. 定时触发时，`trigger_type=scheduled`。
4. Console 或 Orchestrator 手动触发时，`trigger_type=manual`。
5. 任务定义的调度状态不替代产物状态。

## 九、TaskProvider 能力

Manager 仍作为单一 TaskProvider。

目标 capabilities 应声明：

```json
{
  "type": "tile_cache_generation",
  "display_name": "瓦片缓存生成",
  "description": "为空间数据项生成可复用的瓦片缓存结果",
  "definition_schema": { "type": "object" },
  "execution_schema": { "type": "object", "additionalProperties": false },
  "supports_schedule": true,
  "supports_cancel": false,
  "supports_inline_execution": false,
  "create_url": "/manager/tile-cache?tab=tasks",
  "edit_url": "/manager/tile-cache?tab=tasks&task_id=:id",
  "deprecated": false
}
```

约束：

1. 未实现可靠取消前不得声明 `supports_cancel=true`。
2. 当前不支持执行参数覆盖时，`POST /tasks/tile_cache_generation/{id}/execute` 必须拒绝非空 `parameters`。
3. `supports_inline_execution=false` 表示 TaskProvider v1 不支持“不先创建任务定义、直接携带一次性配置执行”。Orchestrator v1 只能引用已保存的 owner 任务定义，即 `provider + task_type + task_id`。
4. 从预览页带 item 跳转到任务创建态不是 inline execution，因为仍然会先创建 `manager.tile_cache_tasks`，再执行。
5. `create_url` / `edit_url` 必须指向瓦片缓存页面的“任务”tab，不应指向空间预览页。

## 十、与产物状态的更新关系

执行过程中，`tile_cache_generation` 的唯一执行运行时应以 `manager.tile_cache` 作为产物事实源。当前 PostGIS + MVT 阶段由 Manager Backend 内部执行；若后续计算负载转移到 Manager 进程内、需要多执行器横向扩展，或引入专门 GIS 计算引擎，应整体切换到 Manager Worker 或 GIS 执行引擎，不允许 Backend 与 Worker 双轨并存。

推荐过程：

```text
create tile_cache_task
  -> execute task
  -> create execution
  -> create or select tile_cache_artifact
  -> artifact.status=generating
  -> decide and run preparation actions when needed and authorized
  -> write tiles and manifest
  -> artifact.status=ready / failed / cancelled
  -> update quick_view state
  -> update tile_cache_task last execution summary
```

注意：

1. execution `success` 不自动等于所有产物长期有效。
2. artifact `ready` 不等于任务定义成功；它只表达当前产物可用。
3. quick_view `available` 来源于能力判断和推荐 artifact，不直接来源于执行运行时内部队列状态。

## 十一、准备动作

瓦片缓存生成可以显式执行准备动作，但是否需要准备动作必须根据情况判断。

可能的准备动作：

1. 创建物化视图。
2. 创建空间索引。
3. 执行统计信息更新。
4. 刷新已声明的派生准备产物。

判断因素包括：

1. 数据量和空间范围。
2. 源 SRID 与目标 SRID 是否一致。
3. 是否已有可用空间索引。
4. 是否已有可复用准备产物。
5. 引擎能力和权限。
6. 用户或任务配置是否允许。

约束：

1. 准备动作必须在 `config.preparation` 中表达策略。
2. 执行时必须在 `execution_config` 中保存当次快照。
3. 产物状态中只记录必要的准备结果摘要。
4. 不修改原始业务记录。
5. 不改写原始几何列。
6. 不在普通预览或普通瓦片请求中隐式执行。

## 十二、clean break 迁移结果

目标收敛：

| 旧语义 | 目标语义 |
| --- | --- |
| MVT-only 任务类型 | `tile_cache_generation` |
| MVT-only 任务表 | `manager.tile_cache_tasks` |
| MVT-only 任务配置 | `config.tile.format=mvt` |
| `quick_view` 同时承载产物和 UI 状态 | `quick_view` 负责快显状态，`tile_cache` 负责产物状态 |
| 固定 MinIO 路径 | `config.storage.storage_ref` + artifact manifest |
| 预览页直接发起生成 | 预览页跳转瓦片缓存页面的“任务”tab 创建任务 |

迁移时不保留长期兼容分支。

第一阶段实现应一次性完成命名收敛，不能让新旧任务类型长期并行。

## 十三、后续进入规范的内容

本文稳定后，应修订：

1. `docs/spec/addp任务体系规范.md`：确认 Manager task type 为 `tile_cache_generation`，任务定义表为 `manager.tile_cache_tasks`。
2. `docs/concepts/addp术语表.md`：确认瓦片缓存任务、瓦片缓存结果、快显状态等术语。
3. `docs/concepts/addp监控与执行体系图.md`：确认 Manager 中的瓦片缓存生成表述。
4. `docs/concepts/addp任务编排体系图.md`：替换生成 MVT 的示例为生成瓦片缓存。
5. Manager 表结构和 API 文档：补充 `tile_cache_tasks`、`tile_cache`、`quick_view` 的职责边界。

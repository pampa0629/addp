# ADDP 向量化规范

本文定义 ADDP Manager 向量化能力的规范层契约，包括向量化对象、结果状态、任务定义、一次性执行、重复执行、审计、API、UI 展示和检索消费规则。概念边界见 [ADDP 向量化体系图](../concepts/addp向量化体系图.md)。

## 一、适用范围

本文适用于 Manager 模块中的向量化能力：

- 资源树 item / node 的一次性向量化。
- 独立向量化页面中的向量化任务定义。
- `manager.embeddings` 或后续同等表承载的向量化结果。
- Manager 混合检索对向量化结果的消费。
- Monitor 通过 execution 和 artifact health 查看向量化执行与结果状态。

本文不定义：

- 向量模型服务的内部实现。
- 跨平台统一 Search / Index 模块。
- 图片查图片等非文本查询入口。
- 多模型、多维向量并存策略。当前阶段明确不支持并存。

## 二、核心原则

1. 向量化对象只允许是 data item。
2. node 只作为批量选择范围，不产生 node 自身向量结果。
3. 资源树 item / node 向量化是一次性 execution，不创建 `manager.embedding_tasks`。
4. 独立页面创建的向量化配置才是向量化任务定义，才进入 TaskProvider 和 Orchestrator。
5. 向量化结果是 artifact state，不是 execution status。
6. 重复执行必须逐 item 判断，ready 且未过期的结果跳过，不做无谓向量生成。
7. 当前阶段只有一个启用中的向量模型和一个启用中的向量维度。
8. 向量内容和结果状态归 Manager，不写入 Meta attributes，不写入 Meilisearch 作为事实源。
9. 用户界面统一叫“向量化”；API、表名、TaskProvider `task_type` 统一使用 `embedding`。

## 三、向量化结果状态

### 3.1 状态枚举

向量化结果只允许以下状态：

| 状态 | 含义 | 是否可用于检索 |
| --- | --- | --- |
| `ready` | 当前 item 的向量结果可用，源数据、模型和维度均匹配当前配置。 | 是 |
| `outdated` | 源数据、模型或维度已变化，现有向量结果需要重建。 | 否 |
| `failed` | 最近一次对该 item 的向量化失败。 | 否 |
| `unsupported` | 当前 item 不满足向量化条件，例如数据类型、格式、大小或模型能力不支持。 | 否 |
| `missing_source` | 源 item 已删除、不可访问或无法通过 Meta / Engine 找回。 | 否 |

以下状态不得作为向量化结果状态：

- `running`、`pending`、`success`、`timeout`、`cancelled`：这些属于 execution status。
- `skipped`：这是单次执行中的 item outcome，不是长期结果状态。
- `partial`：这是范围级统计，不是单 item 状态。
- `none`：这是查询接口为了表达“没有结果”的视图状态，不得落入结果表。

### 3.2 状态来源

向量化结果状态由 Manager 维护，来源包括：

- 一次性向量化 execution。
- 向量化任务 execution。
- 资源删除、engine 删除、tenant 删除等 cleanup 事件。
- 模型或维度配置变化后的失效判断。
- 读取源 item 或内容时发现源缺失、不支持或不可访问。

搜索入口不得只根据最近 execution status 判断向量可用性，必须消费向量化结果状态。

## 四、向量化结果字段

向量化结果承载“当前 item 留下了什么向量结果”。字段可以落在 `manager.embeddings` 或后续等价 artifact state 表中，但语义必须保持一致。

### 4.1 核心字段

结果表只保留支撑身份、回跳、检索、过期判断、诊断和生命周期管理的字段。

| 字段 | 类型建议 | 必填 | 用途 |
| --- | --- | --- | --- |
| `id` | bigint | 是 | Manager 内部结果 ID，用于结果详情、删除和操作审计关联。 |
| `tenant_id` | bigint | 是 | 租户隔离；所有查询、写入、删除和检索必须带租户约束。 |
| `item_fingerprint` | string | 是 | 标准 data item 指纹；用于去重、幂等判断和长期稳定身份。 |
| `item_id` | bigint | 是 | 当前 Meta item 行引用；用于回查 item、刷新 locator 和资源树回跳。 |
| `engine_id` | bigint | 是 | 支撑按引擎过滤、engine 删除清理和资源树局部刷新。 |
| `locator` | string | 是 | 前端从结果、检索命中或错误列表回到资源树和预览。 |
| `source_version` | string | 是 | Manager 计算的源版本键，用于判断源数据是否变化。 |
| `embedding` | vector | `ready` 时是 | pgvector 向量内容；仅 `status=ready` 且模型、维度匹配时可检索。 |
| `model` | string | 是 | 生成向量时使用的模型名称；用于追溯和模型切换后的过期判断。 |
| `dimension` | int | 是 | 生成向量时的维度；用于查询兼容性和维度切换后的过期判断。 |
| `status` | string | 是 | 当前结果状态，取值见 3.1。 |
| `status_reason` | string | 否 | 稳定原因码，用于 UI 过滤、诊断和程序判断辅助。 |
| `error_message` | string | 否 | 面向诊断的错误摘要；不得作为程序判断依据。 |
| `last_execution_id` | string | 否 | 最近一次更新该结果状态的 `common.task_executions.execution_id`。 |
| `vectorized_at` | timestamptz | 否 | 最近一次成功生成向量的时间。 |
| `created_at` / `updated_at` | timestamptz | 是 | 记录本表行生命周期；不替代操作审计日志。 |

约束：

1. 新逻辑不得只用 `bucket + path + name` 作为长期身份。
2. 跨 schema 不建立强外键；去重和幂等判断必须使用 `tenant_id + item_fingerprint`，查询、刷新和展示时再用 `item_id` 回查当前 Meta item。
3. 如果请求目标无法解析为 data item，应提示先执行 Meta scan / item refresh，不得绕过 item 边界直接生成长期结果。
4. `item_fingerprint` 必须来自标准 item 指纹，按 [ADDP 路径字段统一规范](addp路径统一和指纹计算.md) 使用 `GenerateItemFingerprint(engineID, fullName)` 生成，不得自行拼接私有 hash。
5. `item_id` 是当前事实表行引用，不作为去重主键；如果重扫或重建导致同一 `item_fingerprint` 对应新的 `item_id`，向量化结果应更新 `item_id` 和 `locator`，而不是插入新结果。
6. `source_version` 表示源 item 的可向量化内容版本，应由 `item_fingerprint`、内容哈希、修改时间或其他 Manager 可观察事实计算；模型和维度不进入 `source_version`，而是通过 `model`、`dimension` 单独判断。
7. `status_reason` 使用稳定机器码，例如 `ready`、`source_changed`、`model_changed`、`dimension_changed`、`file_too_large`、`format_unsupported`、`read_failed`。

### 4.2 不进入核心字段的内容

以下信息不得作为向量化结果的核心字段长期扩张：

| 信息 | 处理方式 |
| --- | --- |
| item 名称、`full_name`、`item_type` | 从 Meta item 或 locator 派生，用于列表展示时可查询补齐。 |
| `content_hash`、`source_updated_at`、文件大小 | 作为 `source_version` 的计算输入或 execution 诊断信息，不直接作为结果去重字段。 |
| `content_type`、文件格式、数据类型 | 执行时用于可向量化判断；列表需要展示时从 Meta attributes 派生。 |
| `checked_at` | 当前阶段不保留；最近检查可通过 `updated_at` 和最近 execution 追溯。 |
| `modality` | 当前阶段不作为结果字段，原因见 4.3。 |

如果后续因性能需要在 `manager.embeddings` 增加展示冗余字段，必须满足：

1. 字段只作为列表展示缓存，不作为身份、状态或检索判断事实源。
2. 字段来源必须能回溯到 Meta item、locator 或 execution。
3. 新增前必须说明无法通过现有字段和查询组合满足的具体性能问题。

### 4.3 向量模态

向量模态表示向量生成时输入内容或模型能力的语义类别，例如文本、文档、图片。它回答“用哪类输入生成向量”，不是 item 的长期身份。

同一个 item 概念上可能包含多种模态。例如 PDF 可以抽取文本，也可以包含图片；图片文件可以有 OCR 文本，也可以用视觉模型生成图像向量。但当前阶段不支持同一 item 多模态、多模型或多维结果并存。

当前阶段规则：

1. 同一个 `tenant_id + item_fingerprint` 只允许一条当前向量化结果。
2. Manager 按当前启用模型和策略选择一个主输入路径生成向量。
3. 不在 `manager.embeddings` 中保存 `modality` 字段，也不把 `modality` 放入唯一键。
4. 执行过程如需记录本次实际输入类别，可写入 `common.task_executions.metadata` 或 item 级诊断明细，但不得改变结果身份。

### 4.4 唯一约束

当前阶段唯一键为：

```text
tenant_id + item_fingerprint
```

原因：

- item 指纹是 ADDP 为 data item 设计的稳定身份和去重键，能覆盖 Meta item 行 ID 重建或重扫后变化的情况。
- 当前阶段不支持多模型、多维、多模态并存。
- `model` 和 `dimension` 是生成依据和过期判断字段，不进入并存身份。
- 同一 item 重复执行应覆盖或跳过，而不是插入新结果。

## 五、向量化任务定义

向量化任务定义表示“未来按什么范围和策略反复执行向量化”。只有独立向量化页面创建的配置才写入任务定义。

### 5.1 公共字段

向量化任务定义必须遵守 [ADDP 任务体系规范](addp任务体系规范.md) 的持久任务定义公共字段语义，不重新定义同义字段。

| 字段 | 说明 |
| --- | --- |
| `id` | Manager 内部任务 ID。 |
| `tenant_id` | 租户 ID。 |
| `name` | 任务名称。 |
| `description` | 任务描述。 |
| `enabled` | 是否启用定时或自动触发。 |
| `schedule` | Cron 表达式，空表示无定时。 |
| `last_run_at` | 最近执行时间。 |
| `next_run_at` | 下一次计划执行时间。 |
| `last_execution_id` | 最近一次 `common.task_executions.execution_id`。 |
| `last_execution_status` | 最近一次执行状态。 |
| `created_by` | 创建人。 |
| `created_at` / `updated_at` / `deleted_at` | 记录任务定义生命周期；不替代操作审计日志。 |

不得新增与以上字段同义的长期字段，例如 `cron_expr`、`schedule_config`、`last_status`、`last_run_id`、`owner_user_id`。如前端需要回显“每天 / 每周 / 每月”等配置，应由 UI 与 common scheduler 表达式构建能力在请求和响应层转换，不作为任务定义事实源。

### 5.2 私有配置

模块私有配置应保存完整范围和策略快照。推荐使用一个 `config` JSONB 字段承载：

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
    "data_types": ["document", "media"],
    "formats": ["pdf", "docx", "png"],
    "extensions": ["pdf", "docx", "png"],
    "max_file_size_mb": 10
  },
  "embedding": {
    "model": "qwen2.5-vl-embedding",
    "dimension": 1024
  }
}
```

约束：

1. 持久化任务定义的 `target.scope` 应以 `node` 或等价范围为主；单 item 向量化应优先使用资源树一次性 execution。
2. `target.locator` 用于展示和回跳，执行时仍应解析为标准 item / node 身份。
3. `filters` 不能替代 item 级可向量化判断；执行时仍必须逐 item 判断。
4. `embedding.model` 和 `embedding.dimension` 是创建或执行时的配置快照，当前阶段必须与 Manager 当前启用配置一致。

## 六、调度规范

向量化任务调度必须遵守 [ADDP 任务体系规范](addp任务体系规范.md) 的调度规范，并复用 `common/scheduler` 的 Cron 解析和 UI 配置转换能力。

| 对象 | 责任 |
| --- | --- |
| `manager.embedding_tasks` | 保存 `enabled`、`schedule`、`next_run_at`、`last_run_at`。 |
| Manager scheduler | claim due task、创建 execution、投递 worker。 |
| common scheduler | 提供 Cron 解析、时区处理、表达式校验和进程内触发辅助。 |
| `common.task_executions` | 记录调度触发产生的一次 execution。 |

约束：

1. `schedule` 统一保存 Cron 表达式；空表示无定时。
2. 时区、Cron 构建和校验遵循 `common/scheduler`，默认时区为 `Asia/Shanghai`。
3. 定时触发创建 execution 时，`trigger_type` 必须为 `scheduled`。
4. 用户手动点击执行任务时，`trigger_type` 必须为 `manual`。
5. 长期应以 DB-driven due task claim 为主；进程内 Cron 只能作为触发器或辅助工具。
6. 资源树 item / node 一次性向量化不支持调度，不写 `manager.embedding_tasks`。

## 七、执行配置与结果统计

### 7.1 execution 公共字段

向量化 execution 必须遵守 [ADDP 任务体系规范](addp任务体系规范.md) 的执行记录字段语义。

资源树一次性向量化：

| 字段 | 取值 |
| --- | --- |
| `module` | `manager` |
| `task_type` | `embedding` |
| `source` | `manager` |
| `source_task_id` | 空 |
| `source_task_name` | 空或资源树入口展示名 |
| `trigger_type` | `manual` |
| `triggered_by` | 当前用户 ID |
| `execution_config` | 本次执行完整范围和策略快照，并用 `entry=resource_tree` 标记资源树入口 |

向量化任务执行：

| 字段 | 取值 |
| --- | --- |
| `module` | `manager` |
| `task_type` | `embedding` |
| `source` | Manager UI 手动执行时为 `manager`；Orchestrator 触发时为 `orchestrator`；Manager 调度触发时为 `manager` |
| `source_task_id` | `manager.embedding_tasks.id` 的十进制字符串 |
| `source_task_name` | 任务名称 |
| `trigger_type` | `manual` 或 `scheduled` |
| `triggered_by` | 手动执行为当前用户 ID；定时执行按平台约定写系统用户或空 |
| `execution_config` | 任务定义执行时的完整范围和策略快照 |

### 7.2 一次性 execution_config

资源树 item / node 向量化必须创建 `common.task_executions`，但不得创建 `manager.embedding_tasks`。

item 范围的一次性 execution_config：

```json
{
  "entry": "resource_tree",
  "scope": "item",
  "target": {
    "engine_id": 1,
    "item_id": 123,
    "item_fingerprint": "43788d99024bc40b4b7d19ed651f68014d18fa0199a1fb8d471cffb9897b67e4",
    "locator": "addp://engine/1/path/addp/reports/a.pdf?type=object&item_id=123"
  },
  "filters": {
    "max_file_size_mb": 10
  },
  "embedding": {
    "model": "qwen2.5-vl-embedding",
    "dimension": 1024
  }
}
```

node 范围的一次性 execution_config：

```json
{
  "entry": "resource_tree",
  "scope": "node",
  "target": {
    "engine_id": 1,
    "node_id": 23,
    "locator": "addp://engine/1/path/addp/reports?type=directory&node_id=23",
    "recursive": true
  },
  "filters": {
    "max_file_size_mb": 10
  },
  "embedding": {
    "model": "qwen2.5-vl-embedding",
    "dimension": 1024
  }
}
```

### 7.3 任务 execution_config

向量化任务执行时，`execution_config` 必须复制任务定义当时的完整范围和策略，不得只保存 `task_id` 后续回查。

```json
{
  "scope": "node",
  "target": {
    "engine_id": 1,
    "node_id": 23,
    "locator": "addp://engine/1/path/addp/reports?type=directory&node_id=23",
    "recursive": true
  },
  "filters": {
    "data_types": ["document", "media"],
    "formats": ["pdf", "docx", "png"],
    "extensions": ["pdf", "docx", "png"],
    "max_file_size_mb": 10
  },
  "embedding": {
    "model": "qwen2.5-vl-embedding",
    "dimension": 1024
  }
}
```

### 7.4 execution metadata

执行完成后，`common.task_executions.metadata` 至少包含范围级统计：

```json
{
  "total": 100,
  "ready_skipped": 60,
  "generated": 20,
  "rebuilt": 5,
  "unsupported": 10,
  "failed": 5,
  "missing_source": 0
}
```

统计含义：

| 字段 | 含义 |
| --- | --- |
| `total` | 本次枚举到的 item 数。 |
| `ready_skipped` | 已 ready 且未过期，因此跳过的 item 数。 |
| `generated` | 原本无结果，本次成功生成的 item 数。 |
| `rebuilt` | 原结果过期或失败，本次成功覆盖重建的 item 数。 |
| `unsupported` | 不支持向量化的 item 数。 |
| `failed` | 本次执行失败的 item 数。 |
| `missing_source` | 源数据缺失或不可访问的 item 数。 |

范围级 `partial` 不作为 execution status。只要 execution 完成枚举和处理流程，即使部分 item failed，也可以是 `success`，失败数量写入 metadata。只有范围枚举失败、模型服务整体不可用或执行流程无法完成时，execution 才应为 `failed`。

## 八、重复执行规则

执行时必须对每个 item 使用同一套判断规则：

1. 解析 item 当前事实和源版本。
2. 判断 item 是否支持向量化。
3. 查询现有向量化结果。
4. 如果结果 `ready` 且 `source_version`、`model`、`dimension` 均匹配，记录 `ready_skipped`。
5. 如果结果不存在，生成并写入 `ready`。
6. 如果结果 `outdated`、`failed` 或模型 / 维度 / 源版本不匹配，生成并覆盖为 `ready`。
7. 如果 item 不支持，写入或更新为 `unsupported`。
8. 如果源缺失或不可访问，写入或更新为 `missing_source`。
9. 如果生成失败，写入或更新为 `failed`。

资源树一次性执行、向量化任务手动执行、向量化任务定时执行都必须复用以上规则。

## 九、审计规范

向量化的操作审计必须进入 `system.audit_logs`。`manager.embeddings` 和 `manager.embedding_tasks` 中的 `created_at`、`updated_at`、`deleted_at` 只记录表行生命周期，不是合规意义上的操作审计。

Manager 已接入 `common/middleware/audit` 时，非 GET API 会自动写入审计日志。向量化新增或调整 API 时，必须保证以下操作可以被审计：

| 操作 | API 方法 | 建议 `entity_type` | `entity_id` |
| --- | --- | --- | --- |
| 创建一次性向量化执行 | `POST` | `embedding_execution` | 返回的 `execution_id`，如中间件无法自动取得则至少保留请求路径和请求体。 |
| 删除向量化结果 | `DELETE` | `embedding` | `manager.embeddings.id`。 |
| 创建向量化任务 | `POST` | `embedding_task` | 创建后的 `manager.embedding_tasks.id`，如中间件无法自动取得则至少保留请求路径和请求体。 |
| 更新向量化任务 | `PUT` / `PATCH` | `embedding_task` | `manager.embedding_tasks.id`。 |
| 删除向量化任务 | `DELETE` | `embedding_task` | `manager.embedding_tasks.id`。 |
| 手动执行向量化任务 | `POST` | `embedding_execution` | 返回的 `execution_id`，并可在请求体或响应关联中追溯 `source_task_id`。 |

约束：

1. GET 查询向量化结果和任务列表不写操作审计日志。
2. 非 GET 接口必须保留 `http_method`、`resource_path`、`http_status`、`duration_ms`、`request_body`、`query_params`、`request_id`、`module_name` 等标准审计字段。
3. 如果通用路径解析器不能识别 `embedding`、`embedding_task`、`embedding_execution`，实现时必须补充 `common/middleware/audit` 的实体解析规则，或在 Manager 内部显式写入审计日志。
4. 审计日志只记录操作事实和追溯信息，不保存向量内容。

## 十、API 规范

英文 API 统一使用 `embedding`。`vectorization` 只作为中文“向量化”的概念翻译，不作为 Manager API 路径、TaskProvider `task_type` 或长期字段命名。

修改或新增 API 时必须同步 Swagger，执行：

```bash
bash scripts/swagger/gen-swagger.sh manager
bash scripts/swagger/check-route-coverage.sh manager
```

### 10.1 一次性执行

```http
POST /api/v1/manager/embedding_executions
```

请求：

```json
{
  "scope": "item",
  "target": {
    "engine_id": 1,
    "item_id": 123,
    "item_fingerprint": "43788d99024bc40b4b7d19ed651f68014d18fa0199a1fb8d471cffb9897b67e4",
    "locator": "addp://engine/1/path/addp/reports/a.pdf?type=object&item_id=123"
  }
}
```

响应：

```json
{
  "execution_id": "0f58d8c8-1f7b-4e3a-ae65-d61b41f9b77f",
  "status": "running"
}
```

约束：

1. 该接口只创建一次性 execution，不创建任务定义。
2. `scope=item` 时必须能解析到 `item_id`。
3. `scope=node` 时必须能解析到 `node_id`，执行时枚举范围内 item。
4. 响应必须直接返回统一 `execution_id`。

### 10.2 查询向量化结果

```http
GET /api/v1/manager/embeddings
```

查询参数：

| 参数 | 说明 |
| --- | --- |
| `page` / `page_size` | 分页。 |
| `engine_id` | 按 engine 过滤。 |
| `node_id` | 按 node 范围过滤，需由 Manager 解析为 item 集合或 locator 前缀。 |
| `item_id` | 按 item 过滤。 |
| `status` | 按结果状态过滤。 |
| `q` | 按 item 名称、full_name、locator 或错误摘要搜索。 |

响应：

```json
{
  "data": [
    {
      "id": 1,
      "tenant_id": 7,
      "item_fingerprint": "43788d99024bc40b4b7d19ed651f68014d18fa0199a1fb8d471cffb9897b67e4",
      "item_id": 123,
      "engine_id": 1,
      "locator": "addp://engine/1/path/addp/reports/a.pdf?type=object&item_id=123",
      "status": "ready",
      "model": "qwen2.5-vl-embedding",
      "dimension": 1024,
      "vectorized_at": "2026-06-09T10:05:00Z",
      "last_execution_id": "0f58d8c8-1f7b-4e3a-ae65-d61b41f9b77f"
    }
  ],
  "total": 1,
  "page": 1,
  "page_size": 20,
  "total_pages": 1
}
```

列表响应可以补充从 Meta 派生的展示字段，例如 `name`、`full_name`、`item_type`、`data_type`、`format`，但这些字段不得替代 4.1 的核心字段。

### 10.3 查询 item 向量化状态

```http
GET /api/v1/manager/items/{item_id}/embedding
```

已有结果时响应：

```json
{
  "item_id": 123,
  "item_fingerprint": "43788d99024bc40b4b7d19ed651f68014d18fa0199a1fb8d471cffb9897b67e4",
  "has_embedding": true,
  "embedding": {
    "result_id": 1,
    "status": "ready",
    "model": "qwen2.5-vl-embedding",
    "dimension": 1024,
    "vectorized_at": "2026-06-09T10:05:00Z"
  }
}
```

无结果时响应：

```json
{
  "item_id": 123,
  "item_fingerprint": "43788d99024bc40b4b7d19ed651f68014d18fa0199a1fb8d471cffb9897b67e4",
  "has_embedding": false,
  "embedding": null
}
```

资源树 item 使用该接口判断是否展示“向量化”按钮。`has_embedding=true` 且 `embedding.status=ready` 且未过期时，只展示“已向量化”提示，不展示向量化按钮。

### 10.4 删除向量化结果

```http
DELETE /api/v1/manager/embeddings/{id}
```

删除只删除 Manager 向量化结果，不删除源 item。删除后检索不得再消费该结果。

### 10.5 向量化任务 CRUD

Manager 私有 UI 使用：

```http
GET    /api/v1/manager/embedding_tasks
POST   /api/v1/manager/embedding_tasks
GET    /api/v1/manager/embedding_tasks/{id}
PUT    /api/v1/manager/embedding_tasks/{id}
DELETE /api/v1/manager/embedding_tasks/{id}
```

TaskProvider 标准入口保持：

```http
GET  /api/v1/manager/tasks?task_type=embedding
GET  /api/v1/manager/tasks/embedding/{id}
POST /api/v1/manager/tasks/embedding/{id}/execute
GET  /api/v1/manager/executions/{execution_id}
```

私有 UI CRUD 不替代 TaskProvider 标准入口。Orchestrator 只能通过 TaskProvider 标准入口发现和执行向量化任务。

### 10.6 TaskProvider 接入要求

向量化任务必须接入统一任务体系和 Orchestrator，Manager TaskProvider 对 `task_type=embedding` 的声明必须满足：

| 字段 | 取值 |
| --- | --- |
| `type` | `embedding` |
| `display_name` | `向量化` |
| `definition_schema` | JSON object schema，至少为 `{ "type": "object" }` |
| `execution_schema` | 当前不支持参数覆盖时必须为 `{ "type": "object", "additionalProperties": false }` |
| `supports_schedule` | `true` |
| `supports_cancel` | 未实现真实取消前为 `false` |
| `supports_inline_execution` | `false` |
| `create_url` / `edit_url` | 指向 Manager 向量化任务页面，不得指向结果视图 |
| `deprecated` | `false` |

Provider 注册 endpoint 必须使用统一任务体系标准后缀：

```text
task_list_endpoint    = /api/v1/manager/tasks
task_detail_endpoint  = /api/v1/manager/tasks/{task_type}/{id}
task_execute_endpoint = /api/v1/manager/tasks/{task_type}/{id}/execute
task_status_endpoint  = /api/v1/manager/executions/{execution_id}
```

执行入口请求体必须遵守统一格式：

```json
{
  "trigger_type": "manual",
  "source": "orchestrator",
  "parent_execution_id": "uuid",
  "parameters": {}
}
```

约束：

1. `trigger_type` 只允许 `manual` / `scheduled`。
2. Orchestrator 调用时 `source=orchestrator`；Manager UI 手动执行和 Manager 自身调度触发时 `source=manager`。具体入口如资源树、任务页、调度器写入 `execution_config.entry` 或 `metadata`，不得扩展 `trigger_type`。
3. 当前 Manager embedding 不支持执行参数覆盖时，非空 `parameters` 必须返回 400，不得静默忽略。
4. `supports_schedule=true` 只表示该 task type 支持调度；具体调度事实仍归 `manager.embedding_tasks.enabled/schedule/next_run_at/last_run_at`。
5. 资源树一次性向量化不是 TaskProvider inline execution，`supports_inline_execution` 必须保持 `false`。

## 十一、UI 规范

### 11.1 资源树 item

资源树 item 需要展示以下状态：

| 状态 | UI 展示 |
| --- | --- |
| 无结果 | 展示“向量化”操作。 |
| `ready` | 展示“已向量化”提示，不展示向量化按钮。 |
| `outdated` | 展示“重新向量化”操作。 |
| `failed` | 展示“重新向量化”操作和失败提示。 |
| `unsupported` | 展示“不支持向量化”提示。 |
| `missing_source` | 通常不应出现在有效资源树 item 上；如出现，提示状态异常。 |

### 11.2 资源树 node

node 不做整体状态判断。node 可以展示“向量化”操作。执行时枚举 item，并逐个跳过 ready 且未过期的结果。

### 11.3 独立向量化页面

独立页面名称使用“向量化”，至少包含两个视图：

| 视图 | 内容 |
| --- | --- |
| 向量化结果 | 展示 `manager.embeddings` 或同等 artifact state。 |
| 向量化任务 | 展示 `manager.embedding_tasks`。 |

向量化结果列表至少展示：

- item 名称或 full_name。
- engine。
- locator 或预览入口。
- 状态。
- 模型和维度。
- 向量化时间。
- 最近 execution ID。
- 错误原因。
- 删除结果或重新向量化操作。

向量化任务列表至少展示：

- 任务名称。
- 目标范围。
- 是否递归。
- 筛选条件。
- 是否启用调度。
- 最近执行状态。
- 最近执行时间。
- 范围级结果统计。
- 执行、编辑、删除、查看结果和跳转 Monitor 操作。

## 十二、检索消费规范

Manager 混合检索消费向量结果时必须满足：

1. 只查询 `status=ready` 的结果。
2. 只查询当前启用模型和当前启用维度的结果。
3. 按租户隔离。
4. 返回结果必须带 locator，供前端回到资源树或预览。
5. 如果向量服务未配置、pgvector 不可用或没有 ready 结果，检索应退化为全文 / 关键词检索，并可返回诊断提示。

Meilisearch 只作为全文和属性检索事实源，不保存向量结果状态，也不作为判断 item 是否已向量化的依据。

## 十三、清理规范

以下事件必须触发 Manager 向量化结果清理或失效：

| 事件 | 处理 |
| --- | --- |
| item 源内容变化 | 标记 `outdated`，下次执行重建。 |
| 模型或维度变化 | 标记 `outdated`，下次执行重建。 |
| item 删除 | 标记 `missing_source` 或删除结果，具体策略由 cleanup 专题确认。 |
| engine 删除 | 删除该 engine 下所有向量化结果和任务定义，或进入系统 cleanup 流程。 |
| tenant 删除 | 删除该 tenant 下所有向量化结果和任务定义。 |
| 用户删除向量化结果 | 删除 Manager 结果，不影响源 item。 |

cleanup 属于 Manager owner 范围，不得反向写入 Meta attributes。

## 十四、验证要求

实现或修改向量化能力时，至少需要覆盖：

```bash
cd manager/backend && go test ./...
bash scripts/swagger/gen-swagger.sh manager
bash scripts/swagger/check-route-coverage.sh manager
cd manager/frontend && npm run build
```

如果只修改本文档或相关概念文档，最小验证为：

```bash
git diff --check -- docs/spec/addp向量化规范.md docs/concepts/addp向量化体系图.md docs/concepts/addp术语表.md docs/README.md
```

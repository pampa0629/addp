# Manager 向量化开发跟进清单

> 状态：开发跟进清单。概念层以 [ADDP 向量化体系图](../concepts/addp向量化体系图.md) 为准，规范层以 [ADDP 向量化规范](../spec/addp向量化规范.md) 为准。本文只记录 Manager 当前实现差距、改造任务、验收标准和建议推进顺序。

## 一、已收敛结论

1. UI 统一叫“向量化”，英文 API、表名和 TaskProvider `task_type` 统一使用 `embedding`。
2. 向量化对象只允许是 data item；node 只是批量选择范围，不产生 node 向量结果。
3. 资源树 item / node 点击向量化都是一次性 execution，不创建 `manager.embedding_tasks`。
4. 独立向量化页面创建的配置才是向量化任务定义，必须接入 TaskProvider 和 Orchestrator。
5. 向量化结果属于 artifact state，不是 execution status；搜索必须消费结果状态。
6. 当前阶段不支持多模型、多维和多模态结果并存。
7. 向量化结果按 `tenant_id + item_fingerprint` 去重；`item_id` 是当前 Meta item 行引用，不作为去重主键。
8. 重复执行时逐 item 判断：ready 且未过期跳过；过期、失败或配置不匹配则覆盖重建。
9. 操作审计进入 `system.audit_logs`；结果表和任务表的时间字段只表示行生命周期。
10. Meilisearch 不作为向量事实源；Manager pgvector 保存向量内容和结果状态。

## 二、已解决的历史实现差距

### 2.1 `manager.embeddings`

改造前实现是历史多模态对象存储模型，本轮已按目标口径迁移：

| 当前字段 / 行为 | 问题 | 目标口径 |
| --- | --- | --- |
| `fingerprint + modality` 唯一 | `modality` 不应进入当前阶段结果身份；缺少租户维度；字段名也未明确是 item 指纹。 | 唯一键改为 `tenant_id + item_fingerprint`。 |
| `fingerprint` 注释仍指向旧对象指纹 | 容易混淆内容摘要、对象路径 hash 和 item 指纹。 | 使用标准 `GenerateItemFingerprint(engineID, fullName)`，字段命名为 `item_fingerprint`。 |
| 缺少 `item_id` | 结果无法稳定回查当前 Meta item。 | 增加 `item_id`，重扫后可随同一 `item_fingerprint` 更新。 |
| 缺少 `locator` | 检索和结果页回跳依赖 metadata 拼装。 | 增加标准 ResourceLocator 字段。 |
| 缺少 `source_version` | 无法统一判断源内容是否过期。 | 增加 `source_version`，由 item 指纹、内容哈希、修改时间等源事实计算。 |
| 缺少 `dimension` | 模型维度变化无法过滤或标记过期。 | 增加 `dimension`。 |
| 缺少 `status/status_reason/error_message` | 搜索可用性只能间接推断。 | 增加 artifact state 状态字段。 |
| 缺少 `last_execution_id/vectorized_at` | 结果无法追溯最近一次更新来源。 | 增加执行关联和成功向量化时间。 |
| `bucket/path/name/file_size/content_type` 作为表字段 | 核心字段偏多，且与 Meta facts 重复。 | 作为执行诊断或展示派生，不作为核心身份字段。 |

### 2.2 `manager.embedding_tasks`

改造前任务定义偏对象存储路径模型，本轮已按目标口径迁移：

| 当前字段 / 行为 | 问题 | 目标口径 |
| --- | --- | --- |
| `engine_id/bucket/prefix/recursive` 分散保存范围 | 不够统一，无法表达标准 node / locator 范围。 | 使用 `config.target` 保存 `scope/node_id/locator/recursive` 等快照。 |
| `modality/file_types` 作为主要配置 | 当前阶段不支持多模态结果并存；执行过滤也未完全生效。 | 使用 `config.filters` 和 `config.embedding`，执行时仍逐 item 判断。 |
| 缺少 `schedule/next_run_at` | 无法满足任务体系调度公共字段。 | 补齐 `schedule`、`next_run_at`。 |
| `supports_schedule=false` | 与“独立页面任务可调度”结论冲突。 | TaskProvider `embedding.supports_schedule=true`。 |

### 2.3 一次性向量化

| 历史行为 | 问题 | 目标口径 |
| --- | --- | --- |
| `POST /api/v1/manager/embedding` 返回内存 `task_id` | 轮询过期后无法追溯；不是统一 execution ID。 | 新增 `POST /api/v1/manager/embedding_executions`，直接返回 `execution_id`。 |
| 单对象 ad-hoc 未统一创建 execution | Monitor 和审计链路不完整。 | item / node ad-hoc 都创建 `common.task_executions`。 |
| `TaskTracker` 作为状态入口 | 只适合短时内存轮询，不是长期事实源。 | 长期状态查询使用 `/executions/{execution_id}` 和 `manager.embeddings`。 |

### 2.4 搜索入口

| 历史行为 | 问题 | 目标口径 |
| --- | --- | --- |
| 搜索按 model / modality / tenant 等过滤 | 缺少 `status=ready` 和 `dimension` 过滤。 | 只消费 `status=ready` 且模型、维度、租户匹配的结果。 |
| 搜索结果从 metadata 取定位信息 | locator 不是一等字段。 | 搜索命中必须返回标准 locator。 |
| 模型或维度变化后旧结果仍可能命中 | 过期向量污染检索。 | 配置变化时标记 outdated 或搜索端过滤。 |

## 三、任务体系与编排接入要求

向量化任务必须满足 [ADDP 任务体系规范](../spec/addp任务体系规范.md) 和 [TaskProvider capabilities 专题设计](TaskProvider%20capabilities专题设计.md)。

### 3.1 标准 TaskProvider endpoint

Manager provider 必须注册：

```text
task_list_endpoint    = /api/v1/manager/tasks
task_detail_endpoint  = /api/v1/manager/tasks/{task_type}/{id}
task_execute_endpoint = /api/v1/manager/tasks/{task_type}/{id}/execute
task_status_endpoint  = /api/v1/manager/executions/{execution_id}
```

`embedding` 任务对 Orchestrator 可见的标准调用为：

```http
GET  /api/v1/manager/tasks?task_type=embedding
GET  /api/v1/manager/tasks/embedding/{id}
POST /api/v1/manager/tasks/embedding/{id}/execute
GET  /api/v1/manager/executions/{execution_id}
```

私有 UI CRUD 使用 `/api/v1/manager/embedding_tasks`，不得替代标准 TaskProvider 入口。

### 3.2 capabilities 要求

`task_types[]` 中 `embedding` 必须声明：

```json
{
  "type": "embedding",
  "display_name": "向量化",
  "description": "对数据项执行向量化并生成可检索向量结果",
  "definition_schema": { "type": "object" },
  "execution_schema": { "type": "object", "additionalProperties": false },
  "supports_schedule": true,
  "supports_cancel": false,
  "supports_inline_execution": false,
  "create_url": "/manager/vectorization-tasks",
  "edit_url": "/manager/vectorization-tasks?task_id=:id",
  "deprecated": false
}
```

约束：

1. `supports_schedule=true` 是因为独立页面创建的向量化任务支持定时调度。
2. 当前不支持执行参数覆盖，`execution_schema.additionalProperties=false`，执行入口必须拒绝非空 `parameters`。
3. 资源树一次性向量化不是 inline execution，`supports_inline_execution` 必须为 `false`。
4. 未实现真实取消前不得声明 `supports_cancel=true`。

### 3.3 execution 字段要求

任务执行必须写入：

| 字段 | 向量化任务执行取值 |
| --- | --- |
| `module` | `manager` |
| `task_type` | `embedding` |
| `source` | Manager UI / 调度触发为 `manager`；Orchestrator 触发为 `orchestrator` |
| `source_task_id` | `manager.embedding_tasks.id` 的十进制字符串 |
| `source_task_name` | 任务名称 |
| `trigger_type` | `manual` / `scheduled` |
| `parent_execution_id` | Orchestrator 子步骤执行时写入 |
| `execution_config` | 任务定义当时的完整范围和策略快照 |
| `metadata` | 范围级结果统计 |

ad-hoc execution 不写 `source_task_id`，但必须写完整 `execution_config`。
资源树入口等细分来源写入 `execution_config.entry=resource_tree`，不得用 `source_task_id` 或 `trigger_type` 表达。

## 四、开发任务清单

### P0 文档和契约校准

- [x] 概念文档收敛到“向量化”。
- [x] 规范文档明确结果字段、去重、任务、调度、审计和 API。
- [x] Swagger 注解迁移到规范路径和响应格式。
- [x] 删除或迁移文档中的旧 `/embedding`、`/embedding/tasks/{task_id}` 长期契约描述。

### P1 表结构与模型

- [x] 修改 `manager.embeddings` 模型字段：
  - `item_fingerprint`。
  - `item_id`。
  - `engine_id`。
  - `locator`。
  - `source_version`。
  - `embedding`。
  - `model`。
  - `dimension`。
  - `status`。
  - `status_reason`。
  - `error_message`。
  - `last_execution_id`。
  - `vectorized_at`。
  - `created_at / updated_at`。
- [x] 唯一约束改为 `tenant_id + item_fingerprint`。
- [x] 移除长期依赖 `fingerprint + modality` 的 upsert 路径。
- [x] 清理 `bucket/path/name/file_size/content_type/modality` 作为核心字段的依赖；需要展示时从 Meta 或 execution 派生。
- [x] 为 pgvector 查询增加 `tenant_id/status/model/dimension` 组合过滤所需索引。

### P2 结果状态与重复执行

- [x] 实现 item 级状态枚举：`ready`、`outdated`、`failed`、`unsupported`、`missing_source`。
- [x] 实现 `source_version` 计算：
  - 输入至少包含 `item_fingerprint`。
  - 优先纳入内容哈希或更新时间。
  - 模型和维度单独比较，不进入 `source_version`。
- [x] 执行前按 `tenant_id + item_fingerprint` 查询已有结果。
- [x] `ready` 且 `source_version/model/dimension` 匹配时跳过。
- [x] 过期、失败或配置不匹配时覆盖重建。
- [x] 不支持和源缺失时写入对应状态和 `status_reason`。

### P3 一次性向量化 API

- [x] 新增 `POST /api/v1/manager/embedding_executions`。
- [x] item 范围请求必须解析到 `item_id`、`item_fingerprint` 和 locator。
- [x] node 范围请求只作为范围选择器，执行时枚举 item。
- [x] item / node ad-hoc 都创建 `common.task_executions`。
- [x] 响应直接返回 `execution_id` 和 execution status。
- [x] 移除内存 `TaskTracker`，状态入口统一为 `common.task_executions` 和 `manager.embeddings`。

### P4 向量化任务定义

- [x] `manager.embedding_tasks` 补齐公共字段：
  - `schedule`。
  - `next_run_at`。
  - `last_run_at`。
  - `last_execution_id`。
  - `last_execution_status`。
  - `created_by`。
  - `created_at / updated_at / deleted_at`。
- [x] 增加 `config` JSONB 保存范围和策略快照。
- [x] 任务范围使用 `target.scope=node`、`engine_id`、`node_id`、`locator`、`recursive`。
- [x] 筛选条件使用 `filters`，例如 data types、formats、extensions、max file size。
- [x] 向量配置使用 `embedding.model` 和 `embedding.dimension`。
- [x] 单 item 向量化优先走资源树 ad-hoc，不作为长期任务定义主路径。

### P5 TaskProvider 和调度

- [x] Manager provider capabilities 中 `embedding.supports_schedule=true`。
- [x] `execution_schema.additionalProperties=false`。
- [x] `POST /tasks/embedding/{id}/execute` 拒绝非空 `parameters`。
- [x] 标准 endpoint 保持 `/tasks`、`/tasks/{task_type}/{id}`、`/tasks/{task_type}/{id}/execute`、`/executions/{execution_id}`。
- [x] 调度字段遵守 `enabled/schedule/next_run_at/last_run_at`。
- [x] 调度使用 `common/scheduler` Cron 解析和 `Asia/Shanghai` 默认时区。
- [x] 定时触发 execution 写 `trigger_type=scheduled`。
- [x] 长期调度以 DB-driven due task claim 为主，避免只依赖进程内 Cron。

### P6 检索消费

- [x] pgvector 查询只消费 `status=ready`。
- [x] 查询必须过滤当前启用 `model` 和 `dimension`。
- [x] 查询必须按租户隔离。
- [x] 检索命中返回 locator。
- [x] 向量服务不可用、pgvector 不可用或没有 ready 结果时，退化为全文 / 关键词检索。
- [x] 搜索结果不再依赖 `modality` 作为当前阶段核心过滤。

### P7 UI

- [x] 资源树 item 状态查询使用 `GET /api/v1/manager/items/{item_id}/embedding`。
- [x] item 已 ready 且未过期时显示“已向量化”，不显示向量化按钮。
- [x] item outdated / failed 时显示“重新向量化”。
- [x] node 始终可显示“向量化”，执行时逐 item 跳过 ready。
- [x] 独立“向量化”页面包含两个视图：
  - 向量化结果：展示 `manager.embeddings`。
  - 向量化任务：管理 `manager.embedding_tasks`。
- [x] 结果视图支持按 engine、node、item、status、关键词筛选。
- [x] 任务视图支持创建、编辑、执行、调度、删除、查看结果和跳转 Monitor。

### P8 审计与 Monitor

- [x] 非 GET 向量化 API 进入 `system.audit_logs`。
- [x] 补充 `common/middleware/audit` 路径解析：
  - `embedding`。
  - `embedding_task`。
  - `embedding_execution`。
- [x] 审计日志不保存向量内容。
- [x] Monitor 通过 execution 查看执行历史。
- [x] Monitor 或 Manager artifact health 通过只读 API 查看结果状态，不直接依赖 Manager 私有表结构。

## 五、建议实施顺序

1. 先改后端模型和 repository，让 `manager.embeddings` 具备 artifact state。
2. 再统一一次性 execution，替换内存 task id 作为长期状态入口。
3. 然后改任务定义表和 TaskProvider capabilities，确保 Orchestrator 能稳定发现和执行 `embedding`。
4. 接着接入调度，补齐 `schedule/next_run_at` 和 due task claim。
5. 再改搜索消费，确保只查 ready 且模型维度匹配的结果。
6. 最后改 UI 页面和资源树操作。

## 六、验收标准

1. 同一租户、同一 `item_fingerprint` 重复向量化不会插入多条当前结果。
2. 同一 item ready 且未过期时，资源树不显示“向量化”按钮。
3. node 一次性向量化会枚举 item，并跳过已有 ready 结果。
4. 独立向量化任务可被 `GET /tasks?task_type=embedding` 发现。
5. Orchestrator 可调用 `POST /tasks/embedding/{id}/execute` 并获得 `execution_id`。
6. 非空 `parameters` 被 Manager provider 拒绝。
7. 定时任务触发 execution 时 `trigger_type=scheduled`。
8. 搜索只消费 `status=ready` 且模型、维度、租户匹配的向量结果。
9. 删除向量化结果不删除源 item。
10. 创建、更新、删除任务和执行操作都能进入审计日志。

## 七、验证命令

文档变更最小验证：

```bash
git diff --check -- docs/spec/addp向量化规范.md docs/concepts/addp向量化体系图.md docs/next/Manager\ embedding\ artifact\ state专题设计.md
```

实现变更建议验证：

```bash
cd manager/backend && go test ./...
bash scripts/swagger/gen-swagger.sh manager
bash scripts/swagger/check-route-coverage.sh manager
cd manager/frontend && npm run build
```

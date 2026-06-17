# embeddings 表结构说明

> 状态：当前实现说明。`manager.embeddings` 表达 data item 当前留下的向量化结果状态和可检索向量内容。

## 一、表定位

`manager.embeddings` 回答：

> 某个 data item 当前是否已有可检索向量结果，该结果由哪个模型和维度生成，是否仍可消费。

它不替代：

1. `manager.embedding_tasks` 的任务定义。
2. `common.task_executions` 的执行历史。
3. Meta item 的数据类型、格式、大小和内容事实。
4. Meilisearch 的全文和属性检索索引。

## 二、目标核心字段

| 字段名 | 当前类型 / 语义 | 说明 |
| --- | --- | --- |
| `id` | bigint | 结果 ID |
| `tenant_id` | integer | 租户 ID |
| `item_fingerprint` | varchar(64) | 标准 data item 指纹，和 `tenant_id` 组成唯一结果身份 |
| `item_id` | integer | 当前 Meta item 行引用，用于回查和资源树定位 |
| `engine_id` | integer | 源 item 所属引擎，用于过滤和清理 |
| `locator` | text | ResourceLocator，用于结果页、检索命中和资源树回跳 |
| `source_version` | varchar | 源 item 可向量化内容版本，用于过期判断 |
| `embedding` | vector | pgvector 向量内容，仅 `ready` 时可被检索消费 |
| `model` / `dimension` | varchar / integer | 生成向量时使用的模型和维度 |
| `status` | varchar | 向量化结果 artifact state |
| `status_reason` | varchar | 稳定原因码 |
| `error_message` | text | 最近错误摘要 |
| `last_execution_id` | varchar | 最近一次更新该结果的 execution |
| `vectorized_at` | timestamp | 最近一次成功生成向量的时间 |
| `created_at` / `updated_at` | timestamp | 生命周期字段 |

## 三、状态语义

| 状态 | 含义 | 可检索 |
| --- | --- | --- |
| `ready` | 当前向量可用 | 是 |
| `outdated` | 源内容、模型或维度已经变化，需要重建 | 否 |
| `failed` | 最近一次向量化失败 | 否 |
| `unsupported` | 当前 item 不满足向量化条件 | 否 |
| `missing_source` | 源 item 或内容缺失 | 否 |

这些状态属于 artifact state，不属于统一 execution status。

## 四、索引建议

| 索引名 | 字段 | 说明 |
| --- | --- | --- |
| `uk_embeddings_tenant_item_fingerprint` | `tenant_id, item_fingerprint` | 同一 item 只保留一条当前结果 |
| `idx_embeddings_item_id` | `item_id` | 按当前 Meta item 行引用回查 |
| `idx_embeddings_engine` | `engine_id` | 按引擎过滤和清理 |
| `idx_embeddings_status` | `status` | 按结果状态过滤 |
| `idx_embeddings_ready_query` | `tenant_id, status, model, dimension, engine_id` | 混合检索 ready 向量候选过滤 |

## 五、消费规则

1. 搜索只消费 `status=ready` 且 `tenant_id/model/dimension` 匹配的结果。
2. `item_id` 不作为结果身份；Meta item 重建或刷新时，应通过 `item_fingerprint` 判断是否仍是同一 data item。
3. 模型或维度变化时，旧结果应进入 `outdated` 或在检索中过滤掉。
4. 删除向量化结果只删除 Manager 结果和向量内容，不删除源 item。

## 六、相关文档

- [向量化概念说明](../向量化概念说明.md)
- [向量化能力说明](../向量化能力说明.md)
- [数据库架构](../数据库架构.md)

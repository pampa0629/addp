# Manager 瓦片缓存结果状态设计

> 状态：next 设计稿。本文承接 [Manager 快显与瓦片缓存概念原则](Manager快显与瓦片缓存概念原则.md)，用于细化“瓦片缓存结果状态独立建模”的落地方案。最高概念边界仍以前述原则文档为准。
>
> 后续代码改造顺序见 [Manager 快显与瓦片缓存实现改造清单](Manager快显与瓦片缓存实现改造清单.md)。

## 一、设计结论

瓦片缓存结果状态应独立建模，但第一阶段字段必须克制。

推荐目标模型：

```text
spatial item
  -> quick_view preference
  -> tile_cache[]
```

其中：

1. `manager.quick_view` 表达 Manager 空间预览中的用户显示偏好。
2. `manager.tile_cache` 表达瓦片缓存结果的最小事实源。
3. `manager.tile_cache_tasks` 表达瓦片缓存生成任务定义。
4. `common.task_executions` 表达某一次生成执行。
5. 快显能力由快显能力 API 查询时根据空间元数据和产物事实动态合成。

这四类对象不能互相替代。

## 二、为什么要独立建模

### 1. 快显状态和产物状态回答的问题不同

快显状态回答：

> 当前 item 在 Manager 预览页里能不能切换快显，应该使用哪个产物，UI 应如何呈现。

瓦片缓存结果状态回答：

> 某个瓦片缓存结果是否存在、在哪里、由什么数据和配置生成、现在是否有效。

前者偏 UI 能力，后者偏派生产物事实。

### 2. 一个 item 可能有多个瓦片缓存结果

同一个空间数据项可能存在多个瓦片缓存结果：

1. 不同格式，例如 MVT、栅格瓦片、预渲染图片瓦片。
2. 不同 zoom 范围，例如低层级概览缓存和高层级精细缓存。
3. 不同存储位置，例如 ADDP 默认存储和用户指定存储。
4. 不同生成配置，例如是否使用物化视图、是否简化几何、是否裁剪范围。
5. 不同用途，例如 Manager 快显、Service 发布、Portal 浏览或外部共享。

如果把这些都塞进 `quick_view`，`quick_view` 会被迫承担一对多产物关系、manifest、存储引用、任务摘要和 UI 状态，语义会再次混杂。

### 3. 瓦片缓存不只服务快显

快显只是瓦片缓存的消费者之一。

瓦片缓存本身有独立价值：降低源库查询成本、支持服务发布、支撑共享浏览、复用到其他空间应用。它应被记录为 Manager 拥有的派生产物，而不是被快显 UI 状态包住。

## 三、对象边界

| 对象 | 建议落点 | 主要职责 | 不应承担 |
| --- | --- | --- | --- |
| 快显偏好 | `manager.quick_view` | 记录 item 的用户预览模式偏好 | 不保存快显能力、推荐 artifact、不可用原因、完整瓦片产物 manifest 或执行历史 |
| 瓦片缓存结果 | `manager.tile_cache` | 记录瓦片缓存结果的身份、状态、存储引用和配置指纹 | 不表达任务定义，不替代 execution |
| 任务定义 | `manager.tile_cache_tasks` | 记录可重复执行的生成配置、调度和最近执行摘要 | 不保存每次执行历史，不直接表达产物当前可用性 |
| 执行记录 | `common.task_executions` | 记录某一次生成过程、状态、进度和结果摘要 | 不作为长期产物事实源 |

## 四、快显偏好与动态能力

`manager.quick_view` 后续应收敛为快显偏好表。

建议表字段语义：

| 字段语义 | 说明 |
| --- | --- |
| item 引用 | 指向当前空间数据项，建议使用稳定 item 身份和 locator |
| `preferred_mode` | 用户偏好的预览模式，例如 `table_geojson` / `quick_view` |
| `updated_at` | 偏好更新时间 |

以下字段属于快显能力 API 的动态响应，不属于 `manager.quick_view` 表：

| 动态字段 | 来源 |
| --- | --- |
| `can_use_quick_view` | 空间元数据、数据量、ready 瓦片缓存结果、实时瓦片能力 |
| `can_generate_tile_cache` | 空间元数据、引擎能力、权限和任务配置条件 |
| `default_tile_cache_id` | 查询时选择的 ready 瓦片缓存结果 |
| `status` | 查询时合成的快显能力状态 |
| `render_source` | 查询时选择的渲染源 |
| `unavailable_reason` | 查询时合成的不可用原因 |

这样生成、失败、删除瓦片缓存结果时只需要维护 `tile_cache`，预览页下一次查询会自然得到最新快显能力。

## 五、瓦片缓存结果状态

建议新增 `manager.tile_cache`。

它是原始 data item 与瓦片缓存结果之间的最小事实源。

第一阶段只保留能支撑快显、任务回溯、存储定位和过期判断的核心字段。

### 1. 核心字段

| 字段 | 说明 |
| --- | --- |
| `id` | 产物 ID |
| `tenant_id` | 租户 ID |
| `item_fingerprint` | 标准 data item 指纹，用于源数据去重和幂等 |
| `item_id` | 当前 Meta item 行引用，仅用于回查，不作为去重主键 |
| `locator` | 可回到资源树或数据项的定位引用 |
| `task_id` | 产生或最近刷新该产物的 `manager.tile_cache_tasks.id` |
| `tile_format` | 瓦片格式，例如 `mvt` |
| `status` | 产物状态 |
| `storage_ref` | 存储引用，指向瓦片缓存所在位置或 manifest；对象前缀应包含 `item_fingerprint/config_hash`，避免不同配置互相覆盖 |
| `extent` | 产物覆盖范围 |
| `extent_srid` | 产物范围所使用的 SRID |
| `min_zoom` / `max_zoom` | 覆盖层级 |
| `config_hash` | 规范化生成配置指纹，只包含瓦片矩阵、层级、SRID、范围、几何列和主键等生成参数 |
| `last_execution_id` | 最近一次生成 execution |
| `error_message` | 最近错误摘要 |
| `created_by` | 创建人 |
| `created_at` / `updated_at` / `deleted_at` | 生命周期字段 |

同一个租户下，`item_fingerprint + tile_format + config_hash` 表达同一份源数据、同一种瓦片格式、同一套规范化生成配置。重复执行应刷新同一条当前结果，而不是插入多条等价结果；执行历史仍由 `common.task_executions` 记录。源数据身份由 `item_fingerprint` 表达，瓦片格式由 `tile_format` 表达；二者不进入 `config_hash`。`locator`、`item_id`、任务名称、调度配置等 UI、回跳或运行管理字段同样不进入 `config_hash`。

以上是第一阶段核心字段。其他信息除非有明确查询、过滤、排序或生命周期管理需求，否则不应先扩成独立列。

### 2. 暂不作为核心列的信息

以下信息第一阶段不建议直接扩成表字段：

| 信息 | 第一阶段建议 |
| --- | --- |
| `tile_count` / `size_bytes` | 放入 execution metadata 或 manifest |
| `style_ref` | 后续涉及预渲染或服务发布时再引入 |
| `storage_type` / `bucket` / `prefix` | 由 `storage_ref` 或 manifest 承载 |
| `access_policy` / `cleanup_policy` | 后续清理和授权专题再细化 |
| `generation_config` 完整快照 | 由 `manager.tile_cache_tasks.config` 和 execution `execution_config` 承载 |
| `preparation_artifacts` | 可放入 execution metadata 或后续准备产物专题 |
| `transform_status` / `transform_engine` | 先由生成过程和 manifest 表达，确需查询时再独立建模 |

## 六、产物状态枚举

推荐结果状态：

| 状态 | 含义 |
| --- | --- |
| `generating` | 产物记录已创建，等待或正在生成 |
| `ready` | 产物可用 |
| `failed` | 最近一次生成失败，当前产物不可用或不完整 |
| `cancelled` | 生成被取消，产物不可用或只保留已存在的旧版本 |
| `deleted` | 产物已清理，仅保留审计或摘要 |

这些状态属于 artifact state，不属于统一 execution status。

第一阶段不单独设置 `deleting`，除非实现中确实需要异步清理过程状态。

## 七、默认结果选择

一个 item 有多个 `tile_cache` 时，快显页需要选择默认结果。

推荐规则：

1. 选择 `status=ready` 且适用于快显的最新产物。
2. 如果存在多个 ready 产物，优先选择格式和当前预览能力最匹配的产物。
3. 如果没有可用产物，但动态能力返回 `can_generate_tile_cache=true`，预览页展示“生成瓦片缓存”按钮并跳转任务页面。

默认结果选择是快显能力判断的一部分，不应写入 Meta attributes。

## 八、与任务执行的关系

瓦片缓存生成执行只更新 `tile_cache` 和 `common.task_executions`。

基本流转：

```text
创建 tile_cache_task
  -> 创建 execution
  -> 创建或更新 tile_cache 为 generating
  -> worker 生成瓦片和 manifest
  -> 更新 tile_cache 为 ready / failed / cancelled
```

任务执行失败时，不应只停留在 execution 失败。产物状态也必须更新为 `failed`，并记录最近错误。
快显能力不在执行链路中写回，预览页状态查询时动态合成。

## 九、与清理体系的关系

清理不应依赖 Meta 直接扫描 Manager 私有存储路径。

推荐边界：

1. Meta 或 System 发出源 item、engine、tenant 生命周期事件。
2. Manager 根据事件查询 `tile_cache`。
3. Manager 决定哪些瓦片缓存结果需要 delete 或保留审计。
4. Manager 按 `storage_ref` 清理对象或 manifest 指向的对象。

清理策略后续可以进入专题，不在第一阶段扩展 artifact 表字段。

## 十、第一阶段范围

第一阶段可以只实现：

1. PostGIS 表。
2. MVT 格式。
3. ADDP 默认对象存储。
4. `manager.tile_cache` 的核心字段。
5. `manager.quick_view.preferred_mode`。

但表、API 和文档命名不能锁死为 MVT-only 或 MinIO-only。

## 十一、后续进入规范的内容

本文稳定后，应进入正式规范：

1. 术语表新增 `tile cache result`、`quick view preference`、`storage ref`。
2. 任务体系规范将 Manager 任务类型收敛为 `tile_cache_generation`。
3. 新增或修订瓦片缓存结果规范，定义 artifact、storage ref、状态枚举和清理边界。
4. Manager 表结构文档更新 `quick_view` 和 `tile_cache` 的职责边界。

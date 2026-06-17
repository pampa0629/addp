# quick_view 表结构和 API 说明

> 状态：当前实现说明。`manager.quick_view` 是快显偏好表，不保存快显能力快照、快显性能优化结果、瓦片缓存结果事实、生成任务定义或执行历史。快显性能优化结果见 `manager.quick_view_optimization`，瓦片缓存结果见 `manager.tile_cache`，任务定义见对应任务表。

## 一、表定位

`manager.quick_view` 表达 Manager 空间预览中的用户显示偏好。

它回答：

> 当前 spatial item 的用户偏好预览模式是什么。

它不回答：

1. 当前能不能切换快显。
2. 推荐使用哪个瓦片缓存结果。
3. 瓦片缓存具体存在哪里。
4. 瓦片缓存由什么完整配置生成。
5. 某次生成执行的进度和历史。
6. MVT 对象路径、MinIO bucket 或瓦片数量统计。

这些信息分别由以下对象承载：

| 对象 | 职责 |
| --- | --- |
| `manager.quick_view` | 用户预览模式偏好 |
| `manager.quick_view_optimization` | Manager 创建并拥有生命周期的 3857 快显性能优化结果状态 |
| `manager.quick_view_optimization_tasks` | 快显性能优化任务定义 |
| `manager.tile_cache` | 瓦片缓存结果状态 |
| `manager.tile_cache_tasks` | 瓦片缓存生成任务定义 |
| `common.task_executions` | 某一次实际执行记录 |
| Quick View Capability API | 动态快显能力、推荐渲染源、默认瓦片缓存结果和不可用原因 |

## 二、目标核心字段

| 字段名 | 当前类型 / 语义 | 说明 |
| --- | --- | --- |
| `id` | bigint | 偏好记录 ID |
| `tenant_id` | integer | 租户 ID |
| `item_fingerprint` | varchar(64) | 标准 data item 指纹，和 `tenant_id` 组成唯一偏好身份 |
| `locator` | text | 资源树或数据项回跳定位，不作为去重主键 |
| `preferred_mode` | varchar | `table_geojson` / `quick_view` |
| `created_at` / `updated_at` | timestamp | 生命周期字段 |

`can_use_quick_view`、`can_generate_tile_cache`、`status`、`render_source`、`default_tile_cache_id`、`unavailable_reason` 是快显能力 API 的动态响应字段，不是 `manager.quick_view` 表字段。

## 三、动态状态语义

| 状态 | 含义 |
| --- | --- |
| `unavailable` | 当前不可快显 |
| `available` | 当前可切换快显 |
| `generating` | 关联瓦片缓存任务正在生成，快显能力等待产物完成 |
| `failed` | 最近一次关联生成失败，当前快显不可用或不可可靠使用 |

状态来源由快显能力判断服务统一计算，不写入 `manager.quick_view`。

## 四、索引建议

| 索引名 | 字段 | 说明 |
| --- | --- | --- |
| `idx_quick_view_tenant_fingerprint` | `tenant_id, item_fingerprint` | 按标准 item 指纹保存唯一偏好 |

## 五、UI 行为

空间预览页读取快显能力后：

| 条件 | UI 行为 |
| --- | --- |
| `can_use_quick_view=true` 且 `render_source=cached_tile/direct_geojson` | 展示“切换快显”，不展示“生成瓦片缓存”按钮 |
| `can_use_quick_view=true` 且 `render_source=realtime_tile` | 展示“切换快显”；当 `optimization.status=stale`、`realtime_tile.performance_mode=source_transform_path` 或瓦片返回 `X-ADDP-Tile-Recommendation=quick_view_optimization` 时展示“执行快显优化”；当瓦片返回 `X-ADDP-Tile-Recommendation=tile_cache_generation` 时使用节流消息提示生成瓦片缓存；需要稳定低层级浏览时保留“生成瓦片缓存”入口 |
| `can_use_quick_view=false` 且 `can_generate_tile_cache=true` | 展示“生成瓦片缓存”；如果 capability 或瓦片响应提示快显性能优化，优先展示“执行快显优化”入口 |
| `can_use_quick_view=false` 且 `can_generate_tile_cache=false` | 不展示生成按钮，只展示不可用原因 |

从预览页跳转时，快显性能优化页面或瓦片缓存页面应自动带入当前 item 上下文。快显性能优化页面创建 `manager.quick_view_optimization_tasks`；瓦片缓存页面在“任务”tab 创建 `manager.tile_cache_tasks`。

预览页和 Explorer 内嵌预览都必须按同一规则展示快显性能优化诊断：

1. `optimization.target_kind=external_3857_materialized_view` 时展示“已识别外部 3857 优化目标”，不展示“执行快显优化”入口。
2. `optimization.status=stale` 时展示“快显优化结果需刷新”，并提供“执行快显优化”入口。
3. 有可索引 3857 目标但动态 MVT 仍超时时，节流提示“生成瓦片缓存”，不把高层级放大误判为慢路径。

## 六、与瓦片缓存结果的关系

快显能力 API 查询 `tile_cache` 后返回当前推荐用于快显的瓦片缓存结果 ID。

推荐结果选择规则：

1. 选择 `status=ready` 且适用于快显的最新产物。
2. 如果没有可用产物但可优化，预览页优先跳转快显性能优化页面创建任务。
3. 如果需要稳定低层级或大范围浏览，预览页或快显性能优化结果列表跳转瓦片缓存页面创建任务。

## 七、与任务和 execution 的关系

瓦片缓存生成必须先创建 `manager.tile_cache_tasks`，再执行。

执行过程：

```text
创建 tile_cache_task
  -> 执行 tile_cache_generation
  -> 写入 common.task_executions
  -> 创建或更新 tile_cache
```

`quick_view` 不保存执行历史，也不保存当前快显能力状态和推荐结果。

## 八、已完成职责收敛

`quick_view` 不再保存以下旧字段或语义：

1. `engine_id` / `schema_name` / `table_name`
2. `min_zoom` / `max_zoom` / `actual_max_zoom`
3. `total_tiles` / `cached_tiles`
4. `fingerprint`
5. `extent` / `extent_srid`
6. 历史准备状态字段 `preparation_status`
7. `started_at` / `completed_at`
8. `can_use_quick_view` / `can_generate_tile_cache`
9. `default_tile_cache_id` / `status` / `unavailable_reason` / `last_checked_at`

这些信息已按目标职责迁移：

| 旧信息 | 目标落点 |
| --- | --- |
| 产物范围、层级、格式、存储引用 | `manager.tile_cache` |
| 生成配置 | `manager.tile_cache_tasks.config` |
| 执行进度、耗时、错误详情、统计摘要 | `common.task_executions.metadata` / `error_details` |
| 快显性能优化目标状态 | `manager.quick_view_optimization` |

## 九、相关文档

- [快显概念说明](../快显概念说明.md)
- [快显实现规范](../快显实现规范.md)
- [数据库架构](../数据库架构.md)

# preview_state 表结构和 API 说明

> 状态：当前实现说明。`manager.preview_state` 是预览模式偏好和预览交互状态表，不保存快显能力快照、矢量物化视图结果、瓦片缓存结果事实、生成任务定义或执行历史。矢量物化视图结果见 `manager.vector_materialized_view`，瓦片缓存结果见 `manager.vector_tile_cache`，任务定义见对应任务表。

## 一、表定位

`manager.preview_state` 表达 Manager 预览中的用户显示偏好和轻量交互状态。

它回答：

> 当前 item 的用户偏好预览模式是什么，以及基础预览 / 快显各自最近一次地图视口、三维相机或表格显示状态是什么。

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
| `manager.preview_state` | 用户预览模式偏好和预览交互状态 |
| `manager.vector_materialized_view` | Manager 创建并拥有生命周期的 3857 矢量物化视图结果状态 |
| `manager.vector_materialized_view_tasks` | 矢量物化视图任务定义 |
| `manager.vector_tile_cache` | 瓦片缓存结果状态 |
| `manager.vector_tile_cache_tasks` | 瓦片缓存生成任务定义 |
| `common.task_executions` | 某一次实际执行记录 |
| Quick View Capability API | 动态快显能力、推荐渲染源、默认瓦片缓存结果和不可用原因 |

## 二、目标核心字段

| 字段名 | 当前类型 / 语义 | 说明 |
| --- | --- | --- |
| `id` | bigint | 偏好记录 ID |
| `tenant_id` | integer | 租户 ID |
| `item_fingerprint` | varchar(64) | 标准 data item 指纹，和 `tenant_id` 组成唯一偏好身份 |
| `locator` | text | 资源树或数据项回跳定位，不作为去重主键 |
| `preferred_mode` | varchar | `basic_preview` / `map_quick_view` |
| `view_state` | jsonb | 快显 / 预览交互状态。顶层按 `basic_preview` / `quick_view` 区分显示模式，模式内按 `map` / `scene_3d` / `table` 区分渲染域 |
| `created_at` / `updated_at` | timestamp | 生命周期字段 |

`can_use_quick_view`、`can_generate_vector_tile_cache`、`status`、`render_source`、`default_vector_tile_cache_id`、`unavailable_reason` 是快显能力 API 的动态响应字段，不是 `manager.preview_state` 表字段。

## 三、动态状态语义

| 状态 | 含义 |
| --- | --- |
| `unavailable` | 当前不可快显 |
| `available` | 当前可切换快显 |
| `generating` | 关联瓦片缓存任务正在生成，快显能力等待产物完成 |
| `failed` | 最近一次关联生成失败，当前快显不可用或不可可靠使用 |

状态来源由快显能力判断服务统一计算，不写入 `manager.preview_state`。

## 四、索引建议

| 索引名 | 字段 | 说明 |
| --- | --- | --- |
| `idx_preview_state_tenant_fingerprint` | `tenant_id, item_fingerprint` | 按标准 item 指纹保存唯一偏好 |

## 五、UI 行为

空间预览页读取快显能力后：

| 条件 | UI 行为 |
| --- | --- |
| `can_use_quick_view=true` 且 `render_source=cached_tile/direct_flatgeobuf` | 展示“切换快显”，不展示“生成瓦片缓存”按钮 |
| `can_use_quick_view=true` 且 `render_source=realtime_tile` | 展示“切换快显”；当 `optimization.status=stale`、`realtime_tile.performance_mode=source_transform_path` 或瓦片返回 `X-ADDP-Tile-Recommendation=vector_materialized_view_generation` 时展示“执行矢量物化视图”；当瓦片返回 `X-ADDP-Tile-Recommendation=vector_tile_cache_generation` 时使用节流消息提示生成瓦片缓存；需要稳定低层级浏览时保留“生成瓦片缓存”入口 |
| `can_use_quick_view=false` 且 `can_generate_vector_tile_cache=true` | 展示“生成瓦片缓存”；如果 capability 或瓦片响应提示矢量物化视图，优先展示“执行矢量物化视图”入口 |
| `can_use_quick_view=false` 且 `can_generate_vector_tile_cache=false` | 不展示生成按钮，只展示不可用原因 |

从预览页跳转时，矢量物化视图页面或瓦片缓存页面应自动带入当前 item 上下文。矢量物化视图页面创建 `manager.vector_materialized_view_tasks`；瓦片缓存页面在“任务”tab 创建 `manager.vector_tile_cache_tasks`。

预览页和 Explorer 内嵌预览都必须按同一规则展示矢量物化视图诊断：

1. `optimization.target_kind=external_3857_materialized_view` 时展示“已识别外部 3857 物化视图目标”，不展示“执行矢量物化视图”入口。
2. `optimization.status=stale` 时展示“矢量物化视图结果需刷新”，并提供“执行矢量物化视图”入口。
3. 有可索引 3857 目标但动态 MVT 仍超时时，节流提示“生成瓦片缓存”，不把高层级放大误判为慢路径。

## 六、与瓦片缓存结果的关系

快显能力 API 查询 `vector_tile_cache` 后返回当前推荐用于快显的瓦片缓存结果 ID。

推荐结果选择规则：

1. 选择 `status=ready` 且适用于快显的最新产物。
2. 如果没有可用产物但可优化，预览页优先跳转矢量物化视图页面创建任务。
3. 如果需要稳定低层级或大范围浏览，预览页或矢量物化视图结果列表跳转瓦片缓存页面创建任务。

## 七、与任务和 execution 的关系

瓦片缓存生成必须先创建 `manager.vector_tile_cache_tasks`，再执行。

执行过程：

```text
创建 vector_tile_cache_task
  -> 执行 vector_tile_cache_generation
  -> 写入 common.task_executions
  -> 创建或更新 vector_tile_cache
```

`preview_state` 不保存执行历史，也不保存当前快显能力状态和推荐结果。

## 八、预览交互状态

`view_state` 只保存用户交互产生的渲染状态，不保存源 metadata、快显结果、任务配置或执行历史。

当前唯一结构：

```json
{
  "basic_preview": {
    "map": {},
    "scene_3d": {},
    "table": {
      "visible_columns": ["_id", "name", "status"]
    }
  },
  "quick_view": {
    "map": {},
    "scene_3d": {}
  }
}
```

顶层按显示模式区分，避免基础预览和快显互相覆盖状态；模式内按渲染域区分，避免把数据类型耦合进 UI 状态。`map` 保存地图视口状态，`scene_3d` 保存三维相机状态，`table.visible_columns` 按用户选择顺序保存所有引擎表格预览的可见字段名；动态 schema 的稳定对象子字段使用 Provider 声明的点路径身份，例如 `userInfo.gender`。空间表的原始几何字段不写入 `visible_columns`。字段结构变化后，前端只恢复仍然存在的字段；已不存在的字段直接忽略，不保留兼容映射。`scene_3d.render_source` 标识产生该相机的实际渲染源；Manager 受管 artifact 还应写 `scene_3d.artifact_version`，优先使用结果的 `last_execution_id`。读取时只有渲染源和 artifact 版本均匹配才可恢复相机，否则按当前数据重新定位。具体渲染器可以在对应对象内保存自身需要的字段，但不得新增 `model_3d`、`tiles_3d`、`gaussian_splat` 等按数据类型拆分的顶层或模式内 key。

## 九、已完成职责收敛

`preview_state` 不再保存以下旧字段或语义：

1. `engine_id` / `schema_name` / `table_name`
2. `min_zoom` / `max_zoom` / `actual_max_zoom`
3. `total_tiles` / `cached_tiles`
4. `fingerprint`
5. `extent` / `extent_srid`
6. 历史准备状态字段 `preparation_status`
7. `started_at` / `completed_at`
8. `can_use_quick_view` / `can_generate_vector_tile_cache`
9. `default_vector_tile_cache_id` / `status` / `unavailable_reason` / `last_checked_at`

这些信息已按目标职责迁移：

| 旧信息 | 目标落点 |
| --- | --- |
| 产物范围、层级、格式、存储引用 | `manager.vector_tile_cache` |
| 生成配置 | `manager.vector_tile_cache_tasks.config` |
| 执行进度、耗时、错误详情、统计摘要 | `common.task_executions.metadata` / `error_details` |
| 矢量物化视图目标状态 | `manager.vector_materialized_view` |

## 十、相关文档

- [快显概念说明](../快显概念说明.md)
- [快显实现规范](../快显实现规范.md)
- [数据库架构](../数据库架构.md)

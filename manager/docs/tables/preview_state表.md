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
| `manager.task_definitions` | 统一派生任务定义（按 `task_type` 区分） |
| `manager.vector_tile_cache` | 瓦片缓存结果状态 |
| `manager.task_resource_bindings` | 派生任务的规范化源/目标资源绑定 |
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

动态状态与矢量瓦片按钮规则统一见[矢量瓦片状态与页面动作](../quick-view/共用契约.md#矢量瓦片状态与页面动作)。这些状态由能力判断服务动态计算，不写入 `manager.preview_state`。

## 四、索引建议

| 索引名 | 字段 | 说明 |
| --- | --- | --- |
| `idx_preview_state_tenant_fingerprint` | `tenant_id, item_fingerprint` | 按标准 item 指纹保存唯一偏好 |

## 五、UI 行为

模式偏好与交互状态写入遵循以下规则：

首次保存交互状态时，模式初值沿用 Quick View Capability 根据源事实计算的 `preferred_mode`，不得固定写成 `basic_preview`。已有记录的交互状态写入只更新 `view_state`，不覆盖模式偏好；模式偏好写入也不得清空交互状态。两类写入按 `tenant_id + item_fingerprint` 原子 upsert，首次并发保存也必须保留明确的模式选择。

预览页生成成果成功且当前数据项、用户模式选择均未变化时，自动进入快显必须同时通过既有模式偏好接口保存 `map_quick_view`，不能只修改页面局部模式。生成等待期间已切换数据项或明确选择其他模式时，不自动切换或覆盖该选择。同一数据项的模式保存按动作顺序串行发送，自动保存晚返回不得覆盖随后手动选择。任务页或后台生成不改变用户预览偏好。

页面按钮、数据任务上下文与矢量物化视图诊断分别沿[页面动作](../quick-view/共用契约.md#矢量瓦片状态与页面动作)、[矢量任务与结果入口](../quick-view/矢量快显与物化视图.md#矢量物化视图任务和结果)及[目标与超时边界](../quick-view/矢量快显与物化视图.md#pg-表要求与矢量物化视图路线)维护；不由偏好记录推导生成能力。

## 六、与瓦片缓存结果的关系

当前推荐的瓦片缓存结果 ID 属于快显能力响应，不保存在偏好表；结果选择统一见[渲染源选择](../quick-view/共用契约.md#渲染源选择)，优化与瓦片生成的跳转条件见[矢量物化视图任务和结果](../quick-view/矢量快显与物化视图.md#矢量物化视图任务和结果)。

## 七、与任务和 execution 的关系

`preview_state` 不保存任务定义、执行历史、当前快显能力或推荐结果。瓦片生成的定义与执行入口统一见[瓦片缓存任务和结果](../quick-view/矢量快显与物化视图.md#瓦片缓存任务和结果)；语义身份、覆盖确认及并发执行约束见[受管快显任务语义身份](../quick-view/共用契约.md#受管快显任务语义身份)。

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

### 状态分组

顶层按显示模式区分，避免基础预览和快显互相覆盖状态；模式内按渲染域区分，避免把数据类型耦合进 UI 状态。具体渲染器可以在对应对象内保存自身需要的字段，但不得新增 `model_3d`、`tiles_3d`、`gaussian_splat` 等按数据类型拆分的顶层或模式内 key。

### 表格字段选择与恢复

`table.visible_columns` 按用户选择顺序保存所有引擎表格预览的可见字段名；列身份与动态 schema 点路径规则见[表格显示偏好与结构来源](../数据预览与资源树实现规范.md#表格显示偏好与结构来源)。空间表的原始几何字段不写入 `visible_columns`。字段结构变化后，前端只恢复仍然存在的字段；已不存在的字段直接忽略，不保留兼容映射。

### 地图与三维恢复

`map` 保存地图视口状态，`scene_3d` 保存三维相机及轻量显示状态；点云 `color_mode` 只允许 `rgb8`、`rgb16`、`elevation`，保存用户明确选择的色值解释或高程着色，不保存自动推断的源数据事实。

`scene_3d.render_source` 标识产生该相机的实际渲染源；Manager 受管 artifact 还应写 `scene_3d.artifact_version`，优先使用结果的 `last_execution_id`。读取时只有渲染源和 artifact 版本均匹配才可恢复相机，否则按当前数据重新定位。

Three.js S3M 的 `renderer_runtime` 只作为 `quick_view.scene_3d` 内部字段保存；恢复相机时必须同时匹配 `render_source`、可选 `artifact_version` 和 `renderer_runtime=three_s3m`，旧 Cesium ECEF 相机直接忽略。

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
| 生成配置 | `manager.task_definitions.config` |
| 执行进度、耗时、错误详情、统计摘要 | `common.task_executions.metadata` / `error_details` |
| 矢量物化视图目标状态 | `manager.vector_materialized_view` |

## 十、相关文档

- [快显概念说明](../快显概念说明.md)
- [快显实现规范](../快显实现规范.md)
- [快显能力与偏好写入](../quick-view/共用契约.md#api)
- [预览接口与表格结构来源](../数据预览与资源树实现规范.md#表格显示偏好与结构来源)
- [数据库架构](../数据库架构.md)

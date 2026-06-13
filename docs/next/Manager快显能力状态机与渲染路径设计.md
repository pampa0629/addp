# Manager 快显能力状态机与渲染路径设计

> 状态：next 补充设计。本文补齐 `Manager快显与瓦片缓存概念原则.md`、`Manager瓦片缓存结果状态设计.md` 和 `Manager瓦片缓存生成任务设计.md` 尚未展开的两件事：
>
> 1. 快显能力到底如何从空间元数据、数据量、瓦片缓存结果和生成能力中合成。
> 2. 用户点击“切换快显”后，前端必须切换到哪条渲染路径，不能只更新一个偏好字段。

## 一、当前问题判断

已有文档已经明确了三类对象边界：

| 对象 | 职责 |
| --- | --- |
| `quick_view` | 表达某个空间 item 在 Manager 预览页中的用户显示偏好，不保存快显能力快照。 |
| `tile_cache` | 表达瓦片缓存结果事实，例如状态、格式、范围、层级和存储位置。 |
| `tile_cache_tasks` | 表达可重复执行的瓦片缓存生成配置。 |

但现有文档和实现仍缺少两个可执行契约：

1. `can_use_quick_view=true` 不能只等于“存在 ready 瓦片缓存结果”。小数据量、实时瓦片、外部瓦片服务都可能让空间 item 可直接快显。
2. `preferred_mode=quick_view` 不能只写入数据库。前端必须把当前预览从基础表格/GeoJSON 模式切换到快显渲染源，否则用户看到的是“提示已切换，但页面没有变化”。

因此，后续改造应先收敛本文的状态机和渲染路径，再逐项核实代码。

## 二、核心定义

### 1. 基础预览

基础预览用于“看懂数据”，由表格和轻量 GeoJSON 绘制组成。

基础预览允许：

1. 分页。
2. 抽样。
3. 只展示部分行。
4. 因数据量大而降级。

因此，基础预览中地图上出现了一部分空间要素，不等于已经进入快显模式。

### 2. 快显模式

快显模式用于“像浏览地图一样浏览空间数据”。

快显模式至少要满足：

1. 能基于完整空间范围全幅定位。
2. 用户缩放、拖拽时有明确的数据加载路径。
3. 数据源可以支撑当前交互成本。
4. 前端处于明确的快显渲染源，而不是基础表格分页的附属地图。

### 3. 快显 API 身份

快显能力 API 的主身份必须是 Resource Locator。

推荐主路径：

```text
GET   /quick-view/capability?locator={ResourceLocator}
PATCH /quick-view/preferred-mode
```

旧的 `engine_id + schema + table` 形态只能作为数据库 provider 的内部解析结果或过渡接口。新 UI 和新能力判断不得根据 `type=table` 直接拼接 `schema/table` 调用快显能力 API，因为 ADDP 中 `table` 是 datatype / item type 语义，不等于 PostgreSQL 表。

能力 API 收到 locator 后，应按以下方式判定：

1. 解析 locator 得到 engine 和资源路径。
2. 读取已扫描 attributes 与预览 DTO 中的 `data_type`、`format`、`capabilities.spatial`。
3. 需要完整小数据量数据时，调用 Manager 预览/format 读取链路，而不是调用 Meta 的 `schema.table` 空间接口。
4. 对数据库 provider，可在内部把 locator 映射为 schema/table 后复用数据库空间能力。

### 4. 快显渲染源

第一阶段允许以下快显渲染源：

| 渲染源 | 说明 | 是否需要瓦片缓存结果 |
| --- | --- | --- |
| `cached_tile` | 使用 `tile_cache.status=ready` 的瓦片缓存结果。 | 是 |
| `realtime_tile` | 没有可用产物，但后端可低成本实时生成瓦片。 | 否 |
| `direct_geojson` | 小数据量时通过 locator 读取完整 GeoJSON 并在地图中直接绘制。 | 否 |

后续可扩展：

| 渲染源 | 说明 |
| --- | --- |
| `external_tile_service` | 数据项自身或外部服务已暴露可消费瓦片服务。 |
| `raster_tile` | 栅格瓦片或预渲染图片瓦片。 |

快显渲染源是 UI 和 API 契约的一部分，不应靠前端猜测。

## 三、能力判定输入

快显能力必须从事实合成，而不是只读 `quick_view` 旧记录。

### 1. 空间元数据

必需事实：

| 事实 | 用途 |
| --- | --- |
| `geometry_columns` / `geometry_column` | 判断能否读取空间几何。 |
| `source_srid` | 判断能否正确转换或展示。 |
| `extent` / `extent_srid` | 判断能否全幅定位和计算层级。 |
| `record_count` | 判断是否可直接 GeoJSON 快显或需要瓦片。 |
| `primary_key` | 点击高亮、要素定位和增量查询时使用。 |

如果空间元数据缺失，应优先使用 Manager 当前预览 DTO 或已扫描 attributes 中的 `capabilities.spatial`。数据库类 provider 可以由 Manager 通过 engine 查询补齐。非数据库空间项不得调用按 `schema.table` 定位的 Meta 空间接口补齐；这类对象应通过 locator 和 format provider 读取。

### 2. 瓦片缓存结果

参与判定的产物必须满足：

1. 与当前 item 身份一致：优先使用 `tenant_id + item_fingerprint`，`locator` 只作为回跳定位和辅助校验。
2. `status=ready`。
3. `storage_ref` 非空且能解析。
4. `tile_format` 被当前前端支持。
5. 覆盖范围和层级足以支撑当前默认快显。

有 ready 产物时，第一阶段选择最新可用产物作为默认快显产物。后续如果需要支持用户固定某个产物，应引入独立的“默认结果偏好”字段或表，不能复用能力状态字段。

### 3. 数据量和直接快显阈值

小数据量空间 item 不应强制用户生成瓦片缓存。

建议第一阶段配置：

| 配置 | 建议默认值 | 含义 |
| --- | --- | --- |
| `quick_view.direct_geojson_max_rows` | `2000` | 记录数小于等于该值时，可直接 GeoJSON 快显。 |
| `quick_view.direct_geojson_max_bytes` | `8MB` | GeoJSON 响应预计超过该值时，不走直接快显。 |
| `quick_view.direct_geojson_max_vertices` | `200000` | 总顶点数超过该值时，不走直接快显。 |

第一阶段可以先只用 `record_count` 判定，后续再补 `bytes` 和 `vertices`。

例如：一个只有 100 或 127 条记录的空间表格数据项，无论来自 PostGIS、NFS Shapefile 还是 MinIO Shapefile，只要几何列和 CRS / SRID 可用，应返回：

```text
can_use_quick_view=true
render_source=direct_geojson
can_generate_tile_cache=true
```

UI 应展示“切换快显”，而不是只展示“生成瓦片缓存”。

`direct_geojson` 不强制要求已有 extent。小数据量路径可以先读取全量 GeoJSON，再由前端根据实际要素范围自适应视图。extent 只应作为已有事实随响应返回；缺失 extent 不得阻断小数据量直接快显。瓦片缓存、实时瓦片和默认 zoom 计算仍应要求可靠 extent。

### 4. 实时瓦片能力

当数据量不适合直接 GeoJSON，但后端能在可接受延迟内按需生成瓦片时，可以使用 `realtime_tile`。

实时瓦片能力至少取决于：

1. 当前引擎支持瓦片 SQL 或等价接口。
2. 空间元数据完整。
3. SRID 可转换。
4. 单瓦片生成成本可控。
5. 没有触发需要显式准备动作的高风险条件。

实时瓦片是快显能力，不是瓦片缓存结果。它可以在请求过程中回填对象缓存，但不能伪装成已完成的 `tile_cache` 结果。

## 四、能力状态合成规则

快显状态查询应按以下顺序合成：

```text
读取空间元数据
  -> 查询 ready tile_cache 结果
  -> 判断 direct_geojson 能力
  -> 判断 realtime_tile 能力
  -> 判断 tile_cache_generation 能力
  -> 合成 quick_view capability response
  -> 读取 manager.quick_view.preferred_mode 作为用户偏好
```

### 1. 可直接快显

满足以下任一条件：

1. 有可用 `cached_tile`。
2. 可走 `direct_geojson`。
3. 可走 `realtime_tile`。
4. 后续存在可消费的 `external_tile_service`。

返回：

```json
{
  "can_use_quick_view": true,
  "status": "available",
  "render_source": "cached_tile | direct_geojson | realtime_tile",
  "default_tile_cache_id": 1
}
```

`default_tile_cache_id` 只在 `cached_tile` 下返回，来自动态选择的 ready 瓦片缓存结果。

### 2. 不可直接快显但可生成缓存

当没有任何可直接快显渲染源，但具备生成瓦片缓存条件时：

```json
{
  "can_use_quick_view": false,
  "can_generate_tile_cache": true,
  "status": "unavailable",
  "unavailable_reason": "tile cache result is not ready",
  "render_source": ""
}
```

UI 展示“生成瓦片缓存”。

### 3. 不可快显也不可生成

当空间元数据不足、引擎不支持或权限不足时：

```json
{
  "can_use_quick_view": false,
  "can_generate_tile_cache": false,
  "status": "unavailable",
  "unavailable_reason": "geometry column is missing"
}
```

UI 展示不可用原因。

## 五、生成中、失败和删除的状态收敛

### 1. 生成中新产物不能覆盖旧可用能力

如果某 item 已有 ready 产物，同时新任务正在生成新产物：

```text
status API response:
  can_use_quick_view = true
  status = available
  default_tile_cache_id = 旧 ready 瓦片缓存结果
```

新产物的 `generating` 状态只属于 `tile_cache` 和 execution，不应把快显降级成 `generating`。

只有当没有任何可用快显渲染源，且存在正在生成的产物时，能力 API 返回的 `status` 才可以是 `generating`。

### 2. 失败不能抹掉旧可用能力

如果新执行失败，但旧 ready 产物仍可用，快显状态仍应保持 available。

失败信息属于失败产物和 execution；预览页可以提示最近生成失败，但不应阻断已有快显。

### 3. 删除产物后状态自然重算

删除某个瓦片缓存结果后：

1. 如果还有其他 ready 产物，选择新的默认结果。
2. 如果没有 ready 产物但满足 `direct_geojson`，切换为 `render_source=direct_geojson`。
3. 如果没有 ready 产物但满足 `realtime_tile`，切换为 `render_source=realtime_tile`。
4. 如果都不满足但可生成缓存，`can_use_quick_view=false` 且 `can_generate_tile_cache=true`。
5. 如果也不可生成，展示不可用原因。

删除任务定义不等于删除产物。只删除任务时，不应影响已有 ready 产物和快显能力。

## 六、`quick_view` 表职责调整

`manager.quick_view` 不应保存快显能力状态，也不应作为能力判定事实源。

推荐职责：

| 字段 | 语义 |
| --- | --- |
| `preferred_mode` | 用户偏好的预览模式。 |
| `tenant_id`、`item_fingerprint` | 偏好绑定的空间 item 稳定身份，用于去重和幂等。 |
| `locator` | 资源树或数据项回跳定位，不作为偏好去重主键。 |
| `created_at`、`updated_at` | 偏好记录时间。 |

重要约束：

1. 查询快显状态时，应从空间元数据和瓦片缓存结果重新合成，不能命中 `quick_view` 就直接返回。
2. `tile_cache` 状态变化后，不更新 `quick_view` 能力字段；下一次状态查询会自然得到新能力。
3. `preferred_mode` 是用户偏好，不应被每次 `tile_cache` ready 自动覆盖；系统推荐通过响应字段 `recommended_mode` 表达。
4. `can_use_quick_view`、`can_generate_tile_cache`、`status`、`render_source`、`default_tile_cache_id` 和 `unavailable_reason` 是能力 API 的动态响应字段，不是 `quick_view` 表字段。

这样可以避免生成缓存、删除缓存、失败回滚等事件反复修改 `quick_view`，也避免派生字段和真实产物状态漂移。

## 七、API 契约

### 1. 快显状态响应

`GET /quick-view/capability?locator={ResourceLocator}` 应返回面向 UI 的完整能力判断。快显公开 API 只以标准 ResourceLocator 作为数据项身份，不保留 `engine/schema/table` 形式的旧接口。

建议响应：

```json
{
  "tenant_id": 1,
  "item_fingerprint": "5d8f...",
  "locator": "addp://engine/8/path/public/farmland?type=table&item_id=123",
  "can_use_quick_view": true,
  "can_generate_tile_cache": true,
  "preferred_mode": "table_geojson",
  "recommended_mode": "quick_view",
  "active_mode": "table_geojson",
  "status": "available",
  "unavailable_reason": "",
  "render_source": "direct_geojson",
  "default_tile_cache_id": null,
  "quick_view": {
    "render_source": "direct_geojson",
    "tile_format": "",
    "tile_url_template": "",
    "geojson_url": "/api/v1/manager/quick-view/geojson?locator=addp%3A%2F%2Fengine%2F8%2Fpath%2Fpublic%2Ffarmland%3Ftype%3Dtable%26item_id%3D123&page=1&page_size=127",
    "extent": [0, 0, 1, 1],
    "extent_srid": 4326,
    "min_zoom": 3,
    "max_zoom": 12,
    "record_count": 127
  },
  "tile_cache_generation": {
    "available": true,
    "reason": "",
    "create_url": "/manager/tile-cache?tab=tasks&create=1&locator=addp%3A%2F%2Fengine%2F8%2Fpath%2Fpublic%2Ffarmland%3Ftype%3Dtable%26item_id%3D123"
  }
}
```

`active_mode` 可以由前端结合本地状态和 `preferred_mode` 决定；如果后端暂不返回，也必须保证前端点击后本地 active mode 立即切换。

### 2. 更新偏好

`PATCH /quick-view/preferred-mode` 只更新偏好，不代表已经完成渲染切换。

后端必须校验：

1. 如果请求 `preferred_mode=quick_view`，当前 item 必须 `can_use_quick_view=true`。
2. 如果不可快显，返回 400，并说明不可用原因。

前端必须：

1. 调用 PATCH。
2. 重新获取快显状态。
3. 将当前页面渲染模式切换为 `quick_view`。
4. 按 `render_source` 选择具体组件。

## 八、前端渲染契约

空间预览页至少维护两个模式：

| 模式 | 说明 |
| --- | --- |
| `table_geojson` | 当前基础预览：表格 + 当前页/抽样 GeoJSON。 |
| `quick_view` | 快显模式：使用能力 API 返回的 `render_source`。 |

`quick_view` 模式下：

| `render_source` | 前端组件 |
| --- | --- |
| `cached_tile` | `VectorTilePreview`，通过统一 tiles API 读取默认瓦片缓存结果。 |
| `realtime_tile` | `VectorTilePreview`，通过统一 tiles API 实时生成。 |
| `direct_geojson` | 全量或分批 GeoJSON 地图组件，不依赖表格当前页。 |

切换快显按钮点击后，按钮文案和 UI 状态应变为“已使用快显”或提供“返回基础预览”，而不是重复显示“切换快显”。

## 九、对当前实现的核实结论

本轮已按本文完成以下收敛：

1. `quick_view` 表收缩为偏好表，不保存 `can_use_quick_view`、`can_generate_tile_cache`、`default_tile_cache_id` 等派生能力字段。
2. `QuickViewService.BuildCapability` 每次根据空间元数据、`tile_cache` 结果事实和偏好动态合成能力。
3. `tile_cache` 的 `generating` / `failed` 不写回 `quick_view`，也不会覆盖旧 ready 结果的可快显能力。
4. 小数据量按 `record_count <= direct_geojson_max_rows` 返回 `render_source=direct_geojson`。
5. 大表无 ready 结果但空间元数据完整时返回 `render_source=realtime_tile`，同时允许用户生成瓦片缓存。
6. 前端预览页维护 `activePreviewMode`，点击“切换快显”后根据能力 API 的 `render_source` 切换到对应渲染组件。

## 十、改造顺序

已按以下顺序落地：

1. 收缩 `manager.quick_view`：只保存空间 item 身份和 `preferred_mode`。
2. 后端新增快显能力合成函数：输入 item identity，输出完整 quick view capability；不把能力结果写回 `quick_view`。
3. 移除 `tile_cache` ready / failed / deleted / generating 对 `quick_view` 派生状态的刷新。
4. 增加小数据量 `direct_geojson` 判定，先用 `record_count <= direct_geojson_max_rows`。
5. 扩展 quick-view capability API 响应，返回 `render_source` 和渲染参数。
6. 前端预览页维护 `activePreviewMode`，点击“切换快显”后真正切换渲染源。
7. 对 100 条、127 条、已有 ready 瓦片缓存结果、大表无瓦片缓存结果、删除唯一瓦片缓存结果、生成失败但有旧瓦片缓存结果等场景补测试。

## 十一、验收场景

必须覆盖：

| 场景 | 期望 |
| --- | --- |
| 100 条空间表，无瓦片缓存结果 | `can_use_quick_view=true`，`render_source=direct_geojson`，不强制生成缓存。 |
| 127 条空间表，无瓦片缓存结果 | 同上。 |
| 大表，无瓦片缓存结果，可实时瓦片 | `can_use_quick_view=true`，`render_source=realtime_tile`，可选择生成缓存但不作为唯一入口。 |
| 大表，无瓦片缓存结果，实时瓦片成本不可控 | `can_use_quick_view=false`，`can_generate_tile_cache=true`。 |
| 有 ready 瓦片缓存结果 | `can_use_quick_view=true`，`render_source=cached_tile`。 |
| 有 ready 瓦片缓存结果，同时新结果 generating | 仍可快显，默认使用 ready 结果。 |
| 新生成失败但旧结果 ready | 仍可快显，失败只进入执行和失败产物。 |
| 删除默认瓦片缓存结果，仍有其他 ready 结果 | 自动选择其他 ready 结果。 |
| 删除唯一瓦片缓存结果，小数据量 | 降级为 `direct_geojson` 快显，而不是不可用。 |
| 点击切换快显 | 前端实际切换地图渲染源，并提供当前模式反馈。 |

# MVT 快显 extent 与 SRID 边界说明

> 相关上层专题：[Manager 快显与 MVT 瓦片缓存概念设计](Manager快显与MVT瓦片缓存概念设计.md)。

## 背景

ADDP 已将源空间事实统一收敛到 `attributes.capabilities.spatial`。其中 `capabilities.spatial.extent` 只表达当前空间对象或 primary geometry column 原生 CRS 下的范围，不支持记录与源 CRS 不一致的派生 extent，也不再通过 `extent_srid` 在标准 attributes 内表达另一套坐标。

MVT / Quick View 是另一条路径：它服务地图快显、瓦片生成、缓存范围和 zoom 计算，可能需要 WGS84 或 Web Mercator 下的派生范围。这条路径可以使用具体引擎能力，例如 PostGIS `ST_Transform`，但不能反向扭曲源事实。

## 边界约定

1. `capabilities.spatial.extent` 是源事实。
   - 只记录原生 CRS 下的范围。
   - 不能为了 MVT、底图或普通预览转成 4326 后写入。
   - 如果不能轻量获得原生 extent，应省略或写 `null`。

2. MVT / Quick View extent 是派生事实。
   - 只服务瓦片快显、TileConfig、缓存和 zoom 计算。
   - 可以有自己的 `extent`、`extent_srid`、`target_srid`、`transform_status`、`transform_engine`。
   - 不得写回 `capabilities.spatial`。

3. 普通 Manager 预览不是 MVT。
   - 普通预览返回源坐标 geometry 与 CRS 元数据。
   - 不调用后端 PROJ，也不通过 PostGIS 隐式转 WGS84。
   - 前端只消费 `source_srid/source_crs/source_crs_definition/transform_status/preview_hint`。

## 当前实现观察

当前代码里 MVT / extent 相关逻辑主要在以下路径：

| 路径 | 当前语义 | 后续风险 |
|---|---|---|
| `manager.quick_view.extent` / `extent_srid` | Quick View 快照范围及其 SRID | 目前字段注释仍倾向 `[minLng, minLat, maxLng, maxLat]` 和默认 4326，容易与源事实混淆。 |
| `QuickViewService.GetSpatialMetadataFromMeta` | 从 Meta 空间响应取 extent 和 `ExtentSRID` 写入 QuickView | Meta 标准 attributes 已不应有 `extent_srid`，但 GIS-facing API 仍有 `ExtentSRID` DTO，需要专题重新定义来源。 |
| `TileConfigHandler.GetTileConfig` | 优先用 QuickView extent，否则调用 `spatial.QueryExtent` | `extentSRID=0` 时回退 4326，属于旧假设；不能作为新规范保留。 |
| `common/spatial.QueryExtent` | 查询 PostGIS extent 并转 WGS84 | 可作为 MVT 派生能力候选，但命名和注释应明确不是源事实。 |
| `common/spatial.CalculateMinZoomFromExtent` | 基于 extent 和 SRID 算 zoom | 对 2360 等 SRID 使用粗略米/度估算，未知 SRID 默认当度，后续需要改成“不能可靠转换则不自动计算”。 |
| `common/spatial.BuildMVTQuery` | 生成 MVT SQL，必要时 `ST_Transform(..., 3857)` | 属于 PostGIS/MVT 引擎能力，可以保留在 MVT 路径，但需明确 `target_srid=3857`。 |
| `manager/backend/internal/mvt/quick_view_service.go` | 创建 3857 物化视图 | 属于 Quick View 派生路径，不应影响源 `capabilities.spatial`。 |

## 后续专题需要解决的问题

### 0. 任务体系接入后的 Manager MVT 边界

任务体系主干只要求 Manager 以 `provider=manager, task_type=mvt_generation` 暴露 MVT 任务定义，并能被 Orchestrator 和 Monitor 发现、执行和回跳 owner 页面。MVT / QuickView 内部语义仍由本文专题继续收敛。

后续处理时需要保持以下边界：

- `manager.mvt_tasks` 是可编排任务定义，表达生成策略和调度意图。
- `manager.quick_view` 是 artifact state，表达当前快显产物是否 ready、缓存范围、fingerprint、zoom、错误和更新时间。
- `common.task_executions` 中的 `mvt_generation` 只表达某一次生成执行，不替代 QuickView 当前状态。
- QuickView 从“任务”命名收敛为 artifact state，后续页面、API 和文案都应避免把 QuickView 本身称为任务。
- `mvt_generation` 的 `create_url` / `edit_url` 必须指向 Manager MVT 任务定义 owner 页面，不应跳到空间预览页。

### 1. QuickView extent 字段语义

推荐将 QuickView 中的 extent 明确定义为快显派生范围，而不是源空间事实。

后续可选方向：

- `quick_view.extent` 表示 MVT/TileConfig 使用的派生范围。
- `quick_view.extent_srid` 必须显式写入，不能默认猜 4326。
- 如果派生范围来自 PostGIS `ST_Transform(..., 4326)`，应记录：
  - `extent_srid=4326`
  - `transform_status=engine_transformed`
  - `transform_engine=postgis`
  - `source_srid`
- 如果无法转换，不写派生 extent，TileConfig 不自动计算 zoom。

### 2. Meta GIS-facing API 与 attributes 分离

Meta 标准 attributes 不再维护 `capabilities.spatial.extent_srid`。但 Manager 的 MVT/QuickView 仍可能需要 GIS-facing 响应中带 `extent_srid`。

后续需要明确：

- `attributes.capabilities.spatial.extent`：源事实，只在 attributes 中存在。
- Meta `/spatial` 或类似 GIS-facing DTO：可以投影出 QuickView 需要的响应字段，但不能声称它们都是 attributes 原字段。
- 如果 GIS-facing DTO 返回 `extent_srid`，其值必须来自明确规则：
  - 源 extent：等于 primary geometry column 的 SRID；
  - 派生 extent：来自执行转换的具体引擎能力；
  - 不能确认：为空或 0，并触发上层不可自动计算。

### 3. zoom 计算的可靠性

当前 `CalculateMinZoomFromExtent` 会对 2360 等坐标系做米/度粗略估算，并对未知 SRID 当作度。这不适合作为平台规范。

后续应改为：

- 仅支持可靠 CRS：
  - 4326：直接按经纬度计算。
  - 3857：使用确定公式转 WGS84 计算。
  - 其他 CRS：必须有明确转换能力或派生 extent。
- 无法可靠转换时：
  - 不自动计算 min/max zoom；
  - 要求用户确认；
  - 或使用配置默认值并明确 `calculation_status=manual_required`。

### 4. TileConfig 回退策略

当前 TileConfig 没有 QuickView extent 时会调用 `spatial.QueryExtent` 动态转 WGS84。后续需要决定它是否仍是允许的 MVT 派生路径。

建议：

- 如果保留动态查询，应更名或标注为 MVT/TileConfig 派生查询，不得被 Meta 或普通预览复用。
- 动态查询返回时必须带 `extent_srid=4326` 和 `transform_engine=postgis`。
- 查询失败或无法转换时，不应默认 4326。

## 本轮不处理内容

本轮只记录边界，不修改 MVT/QuickView 实现：

- 不改 `quick_view` 表结构。
- 不改 TileConfig API。
- 不改 MVT SQL。
- 不改 zoom 计算函数。
- 不改 Meta GIS-facing DTO。

这些内容应在 MVT/空间服务专题中统一处理，避免和普通预览、Meta attributes 规范混在一起。

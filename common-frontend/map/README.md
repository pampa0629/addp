# ADDP Map Frontend Components

`common-frontend/map` 提供 Manager 普通预览使用的地图组件、CRS registry、底图 profile 和展示坐标适配能力。

## CRS registry 边界

普通 Manager 空间预览接收后端返回的源坐标 geometry 和 CRS 元数据，不要求后端把 geometry 转成 WGS84。

前端 CRS registry 的职责是：

- 内置少量稳定 CRS：`EPSG:4326`、`EPSG:3857`。
- 优先使用后端返回的 `source_crs_definition` 注册 CRS。
- 第一阶段只接受 `source_crs_definition.definition_encoding` 为 `wkt`、`esri_wkt` 或 `proj4` 的定义文本。
- 在可转换时把预览级小样本 geometry 转成 WGS84，再交给地图 renderer。
- 在 CRS 未知或定义不可注册时压制地图，并由组件按 i18n 展示状态。

前端 CRS registry 不做以下事情：

- 不访问外部 EPSG 服务。
- 不把 `source_crs=EPSG:<code>` 当作 CRS 定义文本。
- 不维护大规模 EPSG 字典。
- 不承担大表、批量 ETL 或后端空间查询的 CRS transform。
- 不把 GCJ-02、BD-09 等在线底图坐标偏移写入 `source_crs`、`source_srid` 或 `capabilities.spatial`。

## 普通预览 CRS 契约

地图组件消费 Manager 普通预览中的以下字段：

| 字段 | 说明 |
|---|---|
| `source_srid` | 源 geometry 的 SRID；无法确定时为 `0` 或省略。 |
| `source_crs` | 源 CRS ID，例如 `EPSG:32650` 或 `ADDP:CRS:<sha256>`。 |
| `source_crs_definition` | 当前 `source_crs` 对应的 CRS 定义对象。 |
| `source_crs_definition.definition_encoding` | CRS 定义文本编码，第一阶段只接受 `wkt`、`esri_wkt` 或 `proj4`。 |
| `source_crs_definition.definition` | CRS 定义文本，供 CRS registry 注册使用。 |
| `transform_status` | 普通预览使用 `not_transformed` 或 `unknown_crs`。 |
| `preview_hint` | `direct_renderable`、`frontend_transform_required` 或 `unknown_crs`。 |

普通预览不消费后端 `transform_message`。不可渲染状态由前端根据 `transform_status` / `preview_hint` 进行 i18n 展示。

普通预览不得仅凭 `target_srid=4326` 认为 geometry 已经可直接渲染。只有明确引擎转换路径返回 `transform_status=engine_transformed` 且目标 CRS 明确时，才能按目标 CRS 解释响应 geometry；如果目标 CRS 不是 WGS84，仍需由前端 CRS registry 转为 WGS84 后再交给地图 renderer。

## 底图 profile

底图配置应表达为受控 profile，而不是只保存 URL：

| 字段 | 说明 |
|---|---|
| `provider` | 底图提供方，例如 `osm`、`tianditu`、`amap`、`custom_xyz`。 |
| `tile_matrix_set` | 瓦片矩阵集，例如 `WebMercatorQuad`、`tianditu_w`、`tianditu_c`。 |
| `view_crs` | 地图视图 CRS，通常为 `EPSG:3857`。 |
| `coordinate_policy` | 展示坐标策略：`wgs84`、`gcj02`、`bd09`。 |
| `requires_key` | 是否需要 key。 |
| `attribution` | 版权和署名信息。 |
| `network_policy` | `online`、`intranet`、`offline`。 |

默认空间校验场景应优先使用 `coordinate_policy=wgs84` 的底图或无底图模式。`gcj02` / `bd09` 底图用于业务浏览时必须显式提示，不作为精确空间校验依据。

## 瓦片预览初始视角

`TilePreview` 接收 WGS84 `extent=[minLon, minLat, maxLon, maxLat]` 时，默认通过 OpenLayers `view.fit()` 将数据范围全幅置于可视区，并保留固定的界面边距。`extent` 优先于 `center/zoom`；只有范围缺失或非法时才回退到中心点和层级。范围仅用于初始化或数据源切换，不应在容器尺寸变化时反复重置用户已调整的视角。

## GCJ-02 展示适配

GCJ-02 不是标准 EPSG CRS，不进入 ADDP 数据事实。

高德底图 profile 使用 `coordinate_policy=gcj02`。高德 renderer 在调用 AMap JSAPI 前把 WGS84 预览要素转换为 GCJ-02 展示坐标。该转换只发生在 renderer 展示边界：

- 传给 provider 的 `position` 是 provider 展示坐标。
- 组件事件回传的业务 `coordinate` 仍保持 WGS84 语义。
- 弹窗和覆盖物定位可以使用 provider 展示坐标。
- 选择 GCJ-02 底图时必须提示“业务浏览”语义。

BD-09 尚未实现。

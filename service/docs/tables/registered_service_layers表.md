# registered_service_layers 表说明

[返回数据库架构](../数据库架构.md)

`service.registered_service_layers` 对应 `RegisteredServiceLayer`。它保存上游服务能力解析结果，不保存源表连接、SQL 或受管瓦片生成任务。父服务见[registered_services](registered_services表.md)。

## 字段

字段类型按当前 Go 模型展示；不把 DTO 投影、关联切片或旧设计字段列为数据库列。

| 字段 | 模型类型 | 含义与约束 |
| --- | --- | --- |
| `id` | `uint` | 主键；不是源数据或目录身份。 |
| `service_id` | `uint` | 所属服务 ID；管理操作须校验父服务和 Tenant。 |
| `layer_name` | `string` | 图层程序标识；不等于显示名称。 |
| `display_name` | `string` | 上游图层显示名称。 |
| `description` | `string` | 描述。 |
| `geometry_type` | `string` | 上游提供的几何类型，不按图层名称猜测。 |
| `crs` | `string` | 解析得到的 CRS；未知时不默认补成 EPSG:4326。 |
| `bbox` | `JSONB` | 协议解析的边界框对象，JSONB，不是统一四元素数组。 |
| `metadata` | `JSONB` | 协议相关的图层能力元数据，JSONB。 |
| `enabled` | `bool` | 图层启用标记，不是删除或独立授权。 |
| `created_at` | `time.Time` | 创建时间。 |
| `updated_at` | `time.Time` | 更新时间。 |

## 索引与约束

模型声明的索引：`idx_registered_layers_enabled`、`idx_registered_layers_metadata_gin`、`idx_registered_layers_service`、`unique_service_layer`。具体数据库约束与索引创建结果以当前迁移和集成验证为准。

`service_id + layer_name` 由当前模型声明复合唯一索引。父服务的 `Layers` 关联声明 `OnDelete:CASCADE`；图层没有独立 Tenant 字段，管理读取先验证父服务的当前 Tenant。删除父注册配置会按该约束清理图层，不影响上游真实数据。

## 几何、范围与能力

- `layer_name` 是上游标识，如 WMS 的 LAYERS 名称；`display_name` 是显示标题，不用它反向定位上游图层。
- `geometry_type` 是字符串，没有当前模型强制的几何类型枚举；可能出现 Point、MultiPoint、LineString、MultiLineString、Polygon、MultiPolygon、GeometryCollection 或未知/空值。WMS 创建图层当前写 Unknown，WFS/WMTS 并未普遍填入确定几何类型，不能根据图层名字推断 Point。
- `crs` 保存解析得到的坐标系；WMS/WFS 的单值不代表已持久化完整 supported_crs 列表。WMTS 依赖 TileMatrixSet，空 CRS 不等于默认 WGS84。
- `bbox` 按协议保存对象；WMS 常使用 west/east/south/north，WMTS 使用 minx/miny/maxx/maxy，当前解析值可能是字符串。没有统一的 type/crs/coordinates 包装或保证全部是四个数值。
- WMS `metadata` 保存解析的 queryable 和 styles；WMTS 保存 tile_matrix_set 等事实。当前 WFS 路径不保证执行 DescribeFeatureType，也不保证生成 properties/output_formats，不能把示意 JSON 当作固定协议。

边界框形状示例（不是整个图层 DTO）：

```json
{
  "west": "73.5", "east": "135.1",
  "south": "3.8", "north": "53.6"
}
```

多坐标系、范围和样式按上游实际能力使用，不猜测缺失值，也不承诺任意样式配置已接入前端渲染。

## 解析与刷新

当前 WMS / WFS / WMTS 的 XML 辅助解析使用正则表达式提取字段，不是完整的协议解析器；不能据已接通刷新流程承诺命名空间、嵌套层级、协议变体和无效 XML 均已正确处理。缺失几何、CRS 或范围时仍按真实解析结果表达，不补造事实。具体实现见[注册服务解析逻辑](../../backend/internal/service/registered_service_service.go)。

完整 XML 解析、协议结构建模与错误分类仍属待完善范围，需以真实或受控能力文档验证；替换时收敛为唯一解析路径，不并行维护旧正则解析。其他接入需求见[外部服务后续事项](../外部服务架构设计.md#后续事项与确认边界)。

| 上游类型 | 当前保存行为 |
| --- | --- |
| WMS | 更新服务元数据，删除旧图层后逐条创建新图层；失败可能留下部分结果。 |
| WFS | 解析 GetCapabilities 的 FeatureType，按 layer_name 更新或新增；不保证移除上游已经消失的旧行。 |
| WMTS | 按 Identifier 更新或新增图层及 TileMatrixSet 信息；不保证清理已经消失的旧行。 |
| OGC API | 当前保存 collections 到父服务 metadata，不保证为每个 collection 创建本表记录。 |
| REST / XYZ | 不由当前 OGC 刷新逻辑自动产生图层。 |

注册与刷新不是覆盖所有行的一次性事务；部分图层更新错误只记日志，不保证一次成功响应意味着全部图层已经写入。刷新可能将既有行的 enabled 重新置为 true，不能据禁用状态宣称已建立不会被刷新的灰度发布策略。

## 读取、状态与入口

普通服务详情通过 Preload 读取图层，不默认过滤 enabled=false，也没有独立图层分页契约。`enabled` 是存储状态，不是主体授权；公开代理转发上游请求时不由该字段构成逐图层访问控制。

当前 Router 没有注册本表的独立新增、更新或删除图层路由；Repository 中有图层写方法不等于已开放 HTTP CRUD。对外通过以下父服务入口读取或刷新：

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/api/v1/service/registered/:id` | 当前 Tenant 的服务详情与图层。 |
| POST | `/api/v1/service/registered/:id/refresh` | 按该上游类型刷新。 |

列表、大量图层加载和缓存优化必须以实际入口为准；不能把 Preload 说成独立缓存，也不能把仅加载启用行的建议当作已经实现。代理如何访问上游及其待核对权限边界见[父服务](registered_services表.md#健康管理与消费)。

## 实现依据

- [当前模型](../../backend/internal/models/registered_service.go)
- [Service 模块说明](../../CLAUDE.md)

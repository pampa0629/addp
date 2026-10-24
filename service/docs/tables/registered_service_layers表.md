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

本表及 DTO 保存解析值，不执行坐标转换或地图验证。`layer_name` 是上游标识，`display_name` 只用于显示；WMS 新建图层的 geometry_type 为 Unknown，WFS / WMTS 新建时为空，不能据图层名、范围或类型字符串推导已取得要素几何。

| 协议 | 当前结构化保存值 | 独有约束与未验证范围 |
| --- | --- | --- |
| WMS | `crs` 为当前正则块中首个匹配的 CRS/SRS 文本；bbox 优先取 EX_GeographicBoundingBox，回退匹配 LatLonBoundingBox，保存 west/east/south/north 字符串；metadata 保存 queryable、styles | 不构成完整 supported_crs 列表或父子图层继承解析；地理范围与单值 CRS 独立提取，不能将 bbox 直接当作该 CRS 下的 GetMap BBOX。样式和 queryable 标记也不证明渲染或查询已验收 |
| WFS | `crs` 读取 DefaultCRS；LowerCorner / UpperCorner 各取前两个分量，bbox 保存 minx/miny/maxx/maxy 字符串 | 未建立完整的其他 CRS/旧版字段映射；角点提取未保留所属边界框的 CRS 与轴序信息，不能直接认定 bbox 使用 DefaultCRS。当前刷新不执行 DescribeFeatureType，不生成完整属性和输出格式契约 |
| WMTS | 新建图层的 crs 为空；bbox 从匹配的 BoundingBox 角点保存 minx/miny/maxx/maxy 字符串；metadata.tile_matrix_set 为图层块内首个匹配的 TileMatrixSet 标识 | 标识不是完整网格定义，未结构化解析 SupportedCRS、各级矩阵、比例尺、原点、瓦片/矩阵尺寸、限界及 ResourceURL。更新既有行时不写 crs，原值可能保留；空值或旧值均不能作为当前网格坐标系已验证的证据 |
| OGC API / REST / XYZ | 当前不由刷新流程为本表生成对应图层；OGC API 的 collections 响应保存在父服务 metadata | 不能从父 metadata、注册类型或端点字符串推导已建立图层 CRS、范围或瓦片网格契约 |

CRS 文本按解析结果保存，不统一归一化为 EPSG 编号，也不保证命名空间、协议版本或 CRS 继承均已识别。BBox 没有统一的 type/crs/coordinates 包装；当前仅复制字符串，不校验数值、上下界、轴序或坐标转换。字段缺失时不补成 EPSG:4326、EPSG:3857、全球范围或默认 TileMatrixSet。

边界框形状示例（不是整个图层 DTO，也不是已验证的请求参数）：

```json
{
  "west": "73.5", "east": "135.1",
  "south": "3.8", "north": "53.6"
}
```

原始 capabilities_xml 或 collections 的保存不等于已结构化理解全部能力。实际地图验证须使用对应上游的协议版本、图层、坐标系、范围、样式和网格；字段回读、健康检查及刷新成功均不能替代该验证，具体检查见[注册图层能力核对](../testing/注册服务与代理.md#注册图层能力核对)。

## 解析与刷新

WMS / WFS / WMTS 当前使用正则提取 XML 字段，不是完整协议解析器，不能保证命名空间、嵌套层级、协议变体或无效 XML 均已正确处理；缺失几何、CRS 或范围时不补造事实。实现见[注册服务解析逻辑](../../backend/internal/service/registered_service_service.go)；完整解析、协议结构与错误分类等[后续事项](../外部服务架构设计.md#后续事项与确认边界)仍须以真实或受控能力文档验证，替换时保持唯一解析路径。

| 上游类型 | 当前保存行为 |
| --- | --- |
| WMS | 先写父服务 metadata，再删除全部旧图层并逐条创建；删除失败上抛，单行创建失败只记日志并继续。旧图层 ID 不保证保留；解析为空时也会进入删除流程，可能留下空表或部分新行。 |
| WFS | 先写 metadata，再按 FeatureType 名称匹配 layer_name 更新或新增；不删除上游已经消失的旧行。图层查询错误被忽略，单行写入错误只记日志，不保证图层集合与上游一致。 |
| WMTS | 先写 metadata，再按 Identifier 更新或新增图层及 TileMatrixSet；查询、单行写入与旧行清理的限制同 WFS。 |
| OGC API | JSON 解码后将 collections 响应保存到父服务 metadata；不创建或同步本表图层，也不校验完整 collections 业务结构。 |
| REST / XYZ | 不由当前 OGC 刷新逻辑产生图层；手动刷新被拒绝。 |

XML 零解析结果未统一判错，上表中只记日志的单行写入失败也可能不阻断刷新方法返回 nil；上游 200、extracted/found 数量均不证明 XML 完整有效或图层已全部持久化。

父服务与全部图层没有共同刷新事务，也没有本流程的并发锁或快照保证。WMS 新建行、WFS / WMTS 更新或新增行均设置 enabled=true，不保证保留禁用标记。创建异步、手动、定时入口及最终状态回写规则统一见[注册与元数据刷新](../数据库架构.md#注册与元数据刷新)；图层更新、部分失败与状态回读按[元数据刷新核对](../testing/注册服务与代理.md#元数据刷新核对)验证，HTTP 200、active 或 metadata.refreshed_at 均不能单独证明图层完整。

## 读取、状态与入口

普通服务详情通过 Preload 读取图层，不默认过滤 enabled=false，也没有独立图层分页契约。`enabled` 是存储状态，不是主体授权；公开代理转发上游请求时不由该字段构成逐图层访问控制。

当前 Router 没有注册本表的独立新增、更新或删除图层路由；Repository 中有图层写方法不等于已开放 HTTP CRUD。对外通过以下父服务入口读取或刷新：

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/api/v1/service/registered/:id` | 当前 Tenant 的服务详情与图层。 |
| POST | `/api/v1/service/registered/:id/refresh` | 按上游类型刷新；用例与回读要求见[元数据刷新核对](../testing/注册服务与代理.md#元数据刷新核对)。 |

父服务跨 Tenant 回归不覆盖协议解析、部分写入、并发刷新或最终状态回写；字段回读也不代替[真实图层能力核对](../testing/注册服务与代理.md#注册图层能力核对)。测试方法与门禁可用不等于已经通过，未覆盖、未运行与失败项按[统一验收边界](../API测试指南.md#能力与验收边界)分别记录。

列表、大量图层加载和缓存优化必须以实际入口为准；不能把 Preload 说成独立缓存，也不能把仅加载启用行的建议当作已经实现。代理如何访问上游及其待核对权限边界见[父服务](registered_services表.md#健康管理与消费)。

## 实现依据

- [当前模型](../../backend/internal/models/registered_service.go)
- [Service 模块说明](../../CLAUDE.md)

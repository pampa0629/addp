# tile_services 表说明

[返回数据库架构](../数据库架构.md)

`service.tile_services` 对应 `TileService`。它以一组[瓦片图层](tile_service_layers表.md)发布二维瓦片；查询要素的输出和筛选契约归[查询服务](query_services表.md)。

## 字段

字段类型按当前 Go 模型展示；不把 DTO 投影、关联切片或旧设计字段列为数据库列。

| 字段 | 模型类型 | 含义与约束 |
| --- | --- | --- |
| `id` | `uint` | 主键；不是源数据或目录身份。 |
| `tenant_id` | `uint` | 所属 Tenant，由当前 AuthContext 限定管理范围。 |
| `service_name` | `string` | 程序标识及公开路径中的服务名；不等于显示标题。 |
| `title` | `string` | 显示标题。 |
| `description` | `string` | 描述。 |
| `keywords` | `StringArray` | PostgreSQL TEXT[] 关键词；没有独立关键词关联表。 |
| `default_srid` | `int` | 服务默认坐标系，默认 3857；不改变源数据实际 SRID。 |
| `extent` | `JSONB` | 服务空间范围摘要，JSONB；不要据默认坐标系补造未知范围。 |
| `protocols` | `JSONB` | JSONB 协议配置；不能仅凭键存在推断路由已经实现。 |
| `public_access` | `bool` | 公开访问设置；不替代资源授权及数据保护检查。 |
| `status` | `string` | 服务状态；模型声明默认 active，具体执行资格由消费入口校验。 |
| `error_message` | `string` | 服务错误信息。 |
| `created_by` | `uint` | 创建人引用；不据此自动授予权限。 |
| `created_at` | `time.Time` | 创建时间。 |
| `updated_at` | `time.Time` | 更新时间。 |

## 索引与约束

模型声明的索引：`idx_tile_services_tenant`、`unique_tile_service_name`。具体数据库约束与索引创建结果以当前迁移和集成验证为准。

## 服务、图层与协议

- 一个服务关联多个 `TileServiceLayer`；服务本身不保存统一的源 `engine_id`。动态源或静态 PMTiles 来源由各图层配置表达，可以分别绑定符合当前能力边界的资源。
- 创建请求必须包含 `first_layer`，先校验来源，再通过 GORM 关联创建服务和首图层；创建之后按 Tenant 重读返回。首图层是创建契约的一部分。
- `default_srid` 默认 3857，是服务级设置；动态源的实际 `source.srid`、PMTiles 发布快照的空间事实仍各自保留，不因该默认值自动重投影或补齐未知 CRS。
- `protocols` 当前构建默认启用 XYZ，默认关闭 OGC Tiles；可配置项按真实协议处理。存在 `tms` 等配置键不证明对应路由已发布；WMS 样式、要素过滤及 `max_features` 不属于本表字段。
- 动态瓦片、静态业务 PMTiles 与 Manager infra 快显缓存是不同结果边界。静态发布不引用 Manager 的 `storage_ref`，也不把三维瓦片并入二维服务。

`keywords` 使用 TEXT[]；它与标题、描述一起帮助发现服务，不构成访问许可。服务名的应用查重按 Tenant 执行，而模型唯一索引声明在 `service_name`，实际跨 Tenant 同名约束仍需核对。

## 管理、访问与删除

管理操作按当前 Tenant 限定服务，使用精确 `service.definition.*` Permission；公开入口有 public/private Tenant 校验，实际来源读取还经过数据保护门禁；不能仅凭协议或状态配置已保存，就断言所有消费入口均执行了对应拒绝。当前 XYZ 按 layer_name 查找图层，没有按 enabled 过滤，`enabled=false` 不能作为已验证的访问阻断。服务状态、协议开关与图层启用的统一执行资格仍需核对。

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| POST | `/api/v1/service/tile` | 携首图层创建服务。 |
| GET | `/api/v1/service/tile` | 管理列表。 |
| GET | `/api/v1/service/tile/:id` | 当前 Tenant 的详情。 |
| PUT | `/api/v1/service/tile/:id` | 更新服务信息、协议或访问设置。 |
| DELETE | `/api/v1/service/tile/:id` | 删除服务定义。 |
| GET | `/tiles/:serviceName/:layerName/:z/:x/*yformat` | XYZ 瓦片入口。 |
| GET | `/wmts/:serviceName` | WMTS 协议入口。 |
| GET | `/ogc/tiles/:serviceName/tiles/:layer/:tileMatrixSetId/:tileMatrix/:tileRow/:tileCol` | OGC Tiles 瓦片入口。 |

本模型没有 `DeletedAt`；Repository 使用物理删除，不提供恢复接口。删除仅面向 Service 定义，不删除业务源表或源 PMTiles 文件。图层删除行为及级联约束的声明差异见[图层约束](tile_service_layers表.md#索引与约束)，不能把历史 SQL 自动当作当前库的约束证据。

## 实现依据

- [当前模型](../../backend/internal/models/tile_service.go)
- [Service 模块说明](../../CLAUDE.md)

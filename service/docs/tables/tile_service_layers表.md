# tile_service_layers 表说明

[返回数据库架构](../数据库架构.md)

`service.tile_service_layers` 对应 `TileServiceLayer`。每行属于一个 TileService。它不把要素查询、样式模板或 Meta 数据项自动同步规则作为独立列保存。

## 字段

字段类型按当前 Go 模型展示；不把 DTO 投影、关联切片或旧设计字段列为数据库列。

| 字段 | 模型类型 | 含义与约束 |
| --- | --- | --- |
| `id` | `uint` | 主键；不是源数据或目录身份。 |
| `service_id` | `uint` | 所属服务 ID；管理操作须校验父服务和 Tenant。 |
| `layer_name` | `string` | 图层程序标识；不等于显示名称。 |
| `title` | `string` | 显示标题。 |
| `description` | `string` | 描述。 |
| `layer_type` | `string` | dynamic 或 static；创建后更新 DTO 不提供类型切换字段。 |
| `layer_config` | `JSONB` | 按图层类型解释的 JSONB 来源及生成/读取配置。 |
| `display_order` | `int` | 显示顺序，默认 0；列表按其升序，再按 created_at 排列。 |
| `enabled` | `bool` | 图层启用标记，不是删除或独立授权。 |
| `created_at` | `time.Time` | 创建时间。 |
| `updated_at` | `time.Time` | 更新时间。 |

## 索引与约束

模型声明的索引：`idx_tile_service_layers_service`。验证边界见[索引与读取](../数据库架构.md#索引与读取)。

父服务通过 `TileService.Layers` 关联图层。[历史建表 SQL](../../backend/migrations/20260207_create_tile_services.sql)声明 `ON DELETE CASCADE`，当前 GORM 关联只声明 `foreignKey:ServiceID`，Repository 删除父服务也没有显式清理图层；实际数据库是否已安装级联约束必须单独核对，不能据旧文档承诺级联已验证。

图层名称由应用按 `service_id + layer_name` 查重；上述历史 SQL 声明 `unique_tile_layer_name` 复合唯一约束，当前模型没有同范围数据库唯一索引声明。并发唯一性不能只用应用预查证明。

## 来源配置

### dynamic

`layer_config.source` 当前要求 `engine_id`、`schema`、`table`、`geometry_column`、`srid`。几何字段名来自所选资源，可以是 `shape` 等任意真实列名，不固定为 `geom`。来源配置位于 JSONB，不是本表上的 Engine 数据库外键。

下面仅展示结构；Engine、路径、几何字段和 SRID 必须取自真实来源：

```json
{
  "source": {
    "engine_id": 9,
    "schema": "gis",
    "table": "roads",
    "geometry_column": "shape",
    "srid": 4326
  },
  "mvt": { "extent": 4096, "buffer": 256 }
}
```

当前动态执行器从 `mvt` 读取瓦片 extent/buffer，缺省分别为 4096/256；它们不是源数据地理范围。实际读取还须取得当前 Tenant 的 Engine、满足执行能力与数据保护条件。空间索引或预先简化的来源视图属于源数据侧优化，不由发布图层自动创建或维护。

### static

- 输入为 `layer_config.source.locator`，必须包含 item ID，指向当前 Tenant 已由 Meta 识别的文件/对象 `media + pmtiles + single` item。
- 后端校验 Locator 的 Engine、路径和 Meta item 身份，要求当前接入的 PMTiles v3、MVT、gzip 及有效层级、中心、范围与 header hash；不是接受任意瓦片目录或 URL。
- 规范化后保存 `source` 和后端生成的 `source_snapshot`，冻结 item 身份、指纹、header hash、层级、空间事实及捕获时间；不能信任客户端伪造的快照。
- 读取通过 System 的当前 Engine Provider Range Read，核对发布依赖并执行保护门禁，不直接读取 Manager infra 缓存引用。配置保存不表示所有来源或空间数据已通过真实运行验收。

当前列中没有 `filter_columns`、`max_features`、`default_style`、`meta_item_id`、`extent_4326` 或独立 `geometry_types`；不要将旧图层查询/样式示例变成这些列已存在或已接线的承诺。查询过滤和输出几何契约见[查询服务](query_services表.md)。Meta 变化不自动覆盖已保存的发布配置。

## 排序、启用与管理

列表按 `display_order ASC, created_at ASC` 排序；较小的顺序值先返回，不据数值区间推导正式图层分组。`enabled=false` 不删除记录，也不构成主体授权。创建后更新 DTO 可以调整显示信息、配置、顺序和启用状态，不提供图层类型切换字段。

所有管理操作同时验证当前 Tenant、路径父服务和图层归属，跨 Tenant 或父子不匹配不得仅按图层 ID 操作。公开消费中的 enabled 过滤及执行资格缺口统一见[管理与消费状态](../architecture/瓦片发布与缓存.md#管理与消费状态)。

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| POST | `/api/v1/service/tile-layers/:serviceId` | 新增图层。 |
| GET | `/api/v1/service/tile-layers/:serviceId` | 当前服务的图层列表。 |
| GET | `/api/v1/service/tile-layers/:serviceId/:layerId` | 图层详情。 |
| PUT | `/api/v1/service/tile-layers/:serviceId/:layerId` | 更新配置或显示控制。 |
| DELETE | `/api/v1/service/tile-layers/:serviceId/:layerId` | 删除图层配置；不删除源表或源文件。 |

当前前端[图层 API 客户端](../../frontend/src/api/tileService.js)的新增、更新和删除仍调用相对路径 `/service/tile/{serviceId}/layers` 及其图层 ID 子路径，与上述后端 `tile-layers` 契约不一致，页面动作不能计为已接通。不新增服务端别名掩盖差异；清缓存按钮和删除后的缓存行为统一见[缓存边界](../architecture/瓦片发布与缓存.md#动态缓存与过期边界)，实际核对见[管理与清理验证](../testing/瓦片协议.md#图层操作与缓存清理核对)。

## 相关表

- [tile_services](tile_services表.md)：父服务、协议和访问设置。
- [registered_service_layers](registered_service_layers表.md)：上游能力解析形成的图层，生命周期与本表不同。

## 实现依据

- [当前模型](../../backend/internal/models/tile_service.go)
- [Service 模块说明](../../CLAUDE.md)

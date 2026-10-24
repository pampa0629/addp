# query_services 表说明

[返回数据库架构](../数据库架构.md)

`service.query_services` 对应 `QueryService`。它保存可发布的查询来源及消费契约，不包含一组可独立增删的要素图层。查询服务与[瓦片服务](tile_services表.md)分别维护自己的定义。

来源与执行规则见[查询发布与执行](../architecture/查询发布与执行.md)，操作入口见[创建与详情](../architecture/管理界面与模块协作.md#查询服务创建与详情)，指标重绑与版本见[指标来源与版本](../architecture/指标来源与版本.md)；消费请求及认证按[查询与要素协议](../testing/查询与要素协议.md)核对。

## 字段

字段类型按当前 Go 模型展示；不把 DTO 投影、关联切片或旧设计字段列为数据库列。管理 DTO 的 `endpoints` 是按配置生成的地址，不是持久字段或可达性证明；地址来源、页面展示差异与验证边界见[协议测试专题](../testing/查询与要素协议.md#管理-dto-与页面端点核对)。

| 字段 | 模型类型 | 含义与约束 |
| --- | --- | --- |
| `id` | `uint` | 主键；不是源数据或目录身份。 |
| `version` | `int64` | 聚合并发版本，bigint，默认 1 且必须为正；不是不可变修订号。 |
| `tenant_id` | `uint` | 所属 Tenant，由当前 AuthContext 限定管理范围。 |
| `service_name` | `string` | 程序标识及公开路径中的服务名；不等于显示标题。 |
| `title` | `string` | 显示标题。 |
| `description` | `string` | 描述。 |
| `keywords` | `StringArray` | PostgreSQL TEXT[] 关键词；没有独立关键词关联表。 |
| `config_type` | `string` | 互斥来源类型：table、sql、analytical。 |
| `engine_id` | `*uint` | 源 Engine；是否可空取决于来源绑定，不据空值猜执行模式。 |
| `runtime_engine_id` | `*uint` | 显式查询 Runtime Engine；主要用于联邦 SQL 和对象表读取。 |
| `schema_name` | `string` | 表来源的 schema。 |
| `table_name` | `string` | 表来源名称；Go 字段名为 TargetTable。 |
| `sql_query` | `string` | SQL 来源的基础查询；analytical 不保存 SQL 副本。 |
| `named_parameters` | `[]QueryServiceNamedParameter` | SQL 模式的强类型标量参数声明，JSONB 序列化数组；不接受 SQL 片段或标识符参数。 |
| `data_config` | `JSONB` | 发布快照、输出契约、稳定键及来源相关配置，JSONB。 |
| `protocols` | `JSONB` | JSONB 协议配置；不能仅凭键存在推断路由已经实现。 |
| `public_access` | `bool` | 公开访问设置；不替代资源授权及数据保护检查。 |
| `max_features` | `int` | 单次查询的服务上限，模型默认 1000；不是数据库总行数。 |
| `status` | `string` | 服务状态；模型声明默认 active，具体执行资格由消费入口校验。 |
| `error_message` | `string` | 服务错误信息。 |
| `created_by` | `uint` | 创建人引用；不据此自动授予权限。 |
| `created_at` | `time.Time` | 创建时间。 |
| `updated_at` | `time.Time` | 更新时间。 |

## 索引与约束

模型声明的索引：`idx_query_services_config_type`、`idx_query_services_data_config_gin`、`idx_query_services_engine`、`idx_query_services_protocols_gin`、`idx_query_services_runtime_engine`、`idx_query_services_status`、`idx_query_services_tenant`、`unique_query_service_name`。具体数据库约束与索引创建结果以当前迁移和集成验证为准。

## 来源与发布快照

- `table`：绑定已识别的表格来源；关系表使用源 `engine_id`，Parquet 对象表同时绑定源 Engine 和 DuckDB `runtime_engine_id`。对象表不是瓦片图层。
- `sql`：普通 SQL 使用 `engine_id`，联邦 SQL 使用 `runtime_engine_id`；两者显式互斥，不根据 SQL 文本或空 Engine ID 猜测。联邦发布冻结实际引用的 Source Engine ID 和对象表映射。
- `analytical`：冻结 Model 指标实现修订、dependency hash 和数据库无关计划包。顶层 Engine、SQL、表来源和 `named_parameters` 不与该来源并存；参数、输出及稳定键从冻结包投影，不保存第二份可编辑契约。
- `data_config.source_snapshot` 保存发布时的来源、表结构、空间信息、输出契约及依赖事实。数据源变化需要显式检查或刷新发布快照，不承诺 Meta 一更新就自动覆盖服务定义。
- `data_config.protection_choices` 保存原生单表字段的角色脱敏选项身份与修订，不保存角色事实、算法参数或凭据。省略或空数组均采用默认保护；显式 null、未知字段及重复选择拒绝。保存与调用按当前登录用户复核，已有服务更新权限的用户可仅提交版本与 `status=inactive` 直接停用，保留原选择，不依赖角色方式仍有效；重新启用、携带其他配置的更新及实际调用仍严格核验，不自动清除选择或恢复默认。移除选择须显式保存；选择参与消费版本，空选择不改变原默认契约。其他来源尚不接受非空角色选择。
- 几何字段由冻结空间契约选定，不固定叫 `geom`；混合几何类型与未知 CRS 继续按已识别事实表达，不能为发布方便猜测坐标系。

`keywords` 保留服务发现所需的关键词。显示标题与描述可以帮助识别服务，但不替代稳定 `id` 或程序标识 `service_name`。名称唯一索引当前声明在 `service_name`；应用查重的 Tenant 范围与数据库约束不能混为一谈。

## 查询、协议与授权

- `protocols` 保存 REST Query、OGC Features 等协议设置；当前 Router 已挂载 REST Query 和 OGC API Features。WFS 共用查询内核是规范要求，不能据旧文档或协议开关宣称当前存在独立 WFS 路由。WMS、WMTS 与瓦片层也不属于本表的一组布尔开关。
- 单次请求受 `max_features` 和请求限制共同约束；业务分页使用 cursor/keyset、稳定排序键和 `limit + 1`，默认不做精确全量计数。管理列表分页与业务查询游标是不同契约。
- 查询字段、筛选和排序来自发布输出契约；analytical 可筛选字段从冻结计划输出派生，不保存 `filterable_fields` 副本。不存在通用“空筛选白名单就放开全部字段”的规则。
- SQL 标量参数按类型绑定；协议层与调用方不得拼接 SQL 片段。公开访问设置、当前 Tenant 和资源授权、源数据及 Security 保护边界分别校验，不以创建人、Engine ID 或协议开启替代授权。
- 已纳管来源必须通过现有保护门禁；可配置或已保存服务不等于来源已经允许公开输出。空间查询性能需结合真实过滤、来源索引与执行计划验证，不给出按数据总量套用的固定性能阈值。

## 当前入口

管理路由使用当前 Tenant AuthContext 及精确 `service.definition.*` Permission。以下仅列路由，不复制请求和响应 DTO：

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| POST | `/api/v1/service/query` | 创建查询服务。 |
| GET | `/api/v1/service/query` | 管理列表。 |
| GET | `/api/v1/service/query/:id` | 管理详情。 |
| GET | `/api/v1/service/query-protection-options` | 当前登录用户针对显式 DataItem locator 的字段可选脱敏方式。 |
| PUT | `/api/v1/service/query/:id` | 更新当前定义。 |
| DELETE | `/api/v1/service/query/:id` | 删除定义。 |
| POST | `/api/query/:serviceName/query` | 数据查询；公开/私有、发布版本及保护检查由消费路径执行。 |
| GET | `/ogc/features/:serviceName/collections/:collectionId/items` | OGC Features 查询。 |

快照差异、快照刷新、指标来源重绑和执行查询预览沿既有专用动作维护，具体参数见[当前路由](../../backend/internal/api/router.go)及[模块契约](../../CLAUDE.md)。模型没有 `DeletedAt`，删除定义不等于删除源数据，不提供软删除恢复承诺。

## 相关表

- [tile_services](tile_services表.md)：瓦片发布。
- [registered_services](registered_services表.md)：第三方端点注册。

## 实现依据

- [当前模型](../../backend/internal/models/query_service.go)
- [Service 模块说明](../../CLAUDE.md)

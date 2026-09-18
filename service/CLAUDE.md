# Service 模块说明

## 模块定位

Service 模块负责数据服务发布与外部服务注册，覆盖查询服务、图查询服务、注册服务代理、瓦片服务、OGC API Features、OGC Tiles、WMTS 和公开访问端点。

企业 Catalog 第四阶段只把 QueryService 作为 `data_service` 来源：Service 保留 SQL、发布快照、协议、输出契约、Consumer Descriptor、运行状态和端点等全部专业事实，只公开 owner-local 变化流与当前最小摘要解析。Service 不依赖 Catalog、不保存 `catalog_entry_id` 或反向列表。Graph、Tile、Registered 尚未具备统一稳定消费契约，不得借本接口输出其管理 DTO。

## 技术栈与端口

- 后端：Go + Gin + GORM，默认端口 `8086`，环境变量 `SERVICE_BACKEND_PORT`。
- 前端：Vue 3 + Element Plus + OpenLayers，开发端口 `5180`，启动脚本环境变量 `SERVICE_FE_PORT`。
- 数据库：PostgreSQL `service` schema。
- 依赖：System、Manager、Meta、DuckDB Federated Query Runtime、Gateway、Redis、MinIO。

## 重要目录

```text
service/
├── authorization/
│   └── permissions.yaml       # Service Permission Manifest，发布期聚合事实源
├── backend/
│   ├── cmd/server/main.go
│   ├── internal/api/          # query、graph、registered、tile、OGC、resource capability
│   ├── internal/models/       # query、graph、registered、tile
│   ├── internal/repository/
│   ├── internal/service/      # query executor、tile cache/dynamic/static、data query
│   ├── migrations/
│   └── docs/                  # Swagger 产物
├── docs/
│   ├── 数据库架构.md
│   ├── service核心概念和架构设计.md
│   ├── 外部服务架构设计.md
│   ├── 瓦片服务使用指南.md
│   └── tables/
└── frontend/src/
    ├── views/                 # QueryService、GraphQueryService、RegisteredService、TileService
    ├── components/
    └── api/
```

## 核心 API

Service 是 `service.definition.*`、`service.external_registration.*` 和 `service.catalog.read` 的 Permission owner；定义只存在于 `authorization/permissions.yaml`，通过 `common/authorization` 发布期聚合，不在服务启动时动态注册。`service.catalog.read` 只授予 `tenant.catalog_runtime`，不得委派或由 Tenant 定制。`service.definition.publish/offline` 是 IAM 目标目录能力，当前独立路由仍待首次 SQL seed 前的覆盖门禁确认。

管理路由前缀：`/api/v1/service`。

- Portal 不通过旧资产来源引用读取 Service 端点；服务消费入口由后续 Service Catalog owner 接入和 Asset 授权结果统一表达。
- 服务消费目录：`GET /consumer/services` 以 `service.data_read.execute` 返回当前用户可执行服务摘要，`GET /consumer/services/:service_type/:service_id` 返回 `addp.service_consumer/v1` Consumer Descriptor；`GET /consumer/api-consumer-services` 以 `service.definition.read` 向 API Consumer 管理员返回同一 owner 过滤后的可授权摘要，但不授予数据执行权。所有目录都不得投影 SQL、Engine、schema、table 或管理 DTO。
- 企业目录来源：`GET /catalog-resources/changes` 返回 QueryService 最小摘要变化；`POST /runtime/catalog-references/resolve` 动态返回当前最小摘要。两个路由只接受 `addp-catalog` 和 `service.catalog.read`，不得返回 SQL、协议、输出契约或 Consumer Descriptor。
- 查询服务管理：`POST/GET /query`、`GET/PUT/DELETE /query/:id`；公开执行端点：`POST /api/query/:serviceName/query`。 列表可同时传入正整数 `metric_implementation_id` 与 `metric_revision_id`，仅返回当前租户内绑定该确切修订的 analytical 服务（包括停用服务）；两项必须同时提供，不完整、重复或非法值返回 400。筛选与 search、分页共同作用，total 为筛选后总数。
- 图查询服务管理：`POST/GET /graph`、`GET/PUT/DELETE /graph/:id`；公开执行端点：`POST /api/gquery/:serviceName`。
- 注册服务管理：`POST/GET /registered`、`GET/PUT/DELETE /registered/:id`、`POST /registered/:id/refresh`、`POST /registered/:id/health`；公开代理：`ANY /api/service/registered/proxy/:id/*path`。
- 瓦片服务管理：`POST/GET /tile`、`GET /tile/search`、`GET /tile/by-name/:serviceName`、`GET/PUT/DELETE /tile/:id`、`/tile-layers/:serviceId`。
- 数据查询辅助：`POST /data/query`、`POST /data/aggregate`、`GET /data/structure`，资源输入统一为 `locator`，由后端派生执行所需 `engine_id/schema/table`。
- 资源能力辅助：资源选择、资源树、表级空间元数据统一走 Meta resource-tree / item API；Service 仅保留 `GET /graphs/node-shapes` 和 `POST /sql/output-contract` 等业务能力接口。
- OGC/瓦片公开端点：`/ogc/features/:serviceName/*`、`/tiles/:serviceName/:layerName/:z/:x/*yformat`、`/wmts/:serviceName`、`/ogc/tiles/:serviceName/*`。

## 开发规则

- 存储引擎连接信息必须从 System 获取，Service 不管理连接配置。
- 查询服务执行目标必须显式且互斥：普通 SQL 和关系表只使用 `engine_id`；联邦 SQL 只使用 `runtime_engine_id`；Parquet 对象表同时保存 Source `engine_id` 与 DuckDB `runtime_engine_id`。不得通过 `engine_id IS NULL` 或 SQL 内容猜测执行模式。
- 查询服务 SQL 样例按 Engine capability 发现，不按 `engine_type` 固定列表。样例必须从当前 Engine Catalog 构造，并在当前用户的 `service.definition.create + service.data_read.execute` 边界内以最多 10 行真实执行且返回非空数据后才能展示；展示给发布表单的是不含 `LIMIT/OFFSET` 的基础 SQL，由查询服务执行层统一分页，不得回退到 `SELECT 1`、硬编码业务表或在样例 SQL 内固化分页。
- 表、固定 SQL 和联邦 SQL 只表达查询服务的来源与执行绑定。REST Query、OGC API Features 和 WFS 必须共用唯一结构化查询内核；协议层不得拼接 SQL。发布契约必须包含非空唯一稳定排序键；业务数据查询统一使用 cursor/keyset 分页、读取 `limit + 1` 行判断下一页，默认不执行 `COUNT(*)`，不得保留 `page/offset`、原始 `filter/orderBy` 或兼容双轨。
- SQL 模式 Query Service 可以声明强类型标量命名参数，SQL 只用 `:name` 引用；参数定义、SQL 引用和执行请求必须完全一致，并通过 `common` 查询运行层绑定，禁止字符串替换。表模式继续使用输出字段结构化筛选，不接受命名参数；Service 不接受关系、字段名、表名或 SQL 片段参数。
- 指标来源计划采用数据库无关契约，规范见 [引擎插件接口规范](../docs/spec/addp引擎插件接口规范.md#数据库无关分析计算契约) 和 [Model 约束](../model/docs/model概念与数据约束规范.md#数据库无关计划与修订)。冻结 SQL 路径已删除。参数选项的多语言标签由 Model 的 `parameter_labels` 冻结，值域以 Plan 参数声明为准，Service 校验两者一致。
- 指标来源引用可显式选择 `result_kind=details`，只接受 Model 同修订声明的去重明细计划，省略为汇总。每个服务固定一种结果；消费字段、保护血缘、游标和依赖 hash 全部由选定的冻结包导出，Workbench 不重算或直接访问 Model。
- 指标来源使用显式 `config_type=analytical`，这是来源表达，不是新的执行通道；首期只能由 `metric_source.implementation_id/revision_id` 解析受信任 Model 发布包，客户端不得直接提交 Plan。不得与 sql/table 来源字段、runtime_engine_id 或调用方参数／输出覆盖并存。既有 SQL/table 查询来源继续各自规范，最终共用 PreparedQuery 授权与执行边界。
- Service 冻结确定修订的 owner 引用、dependency_hash 和唯一 AnalyticalPlanPackage；命名参数、输出字段和稳定键作为该包的只读投影供 DTO、校验与 Consumer Descriptor 使用，不能另存可编辑副本。不把指标包拷贝到 SqlQuery，不要求原生 language 必须为 sql。
- 旧指标 SQL 发布记录迁移为停用的 analytical 来源，清除 SQL、重复引擎、参数、输出及稳定键副本，仅保留 owner 修订引用用于显式重新绑定；禁止自动生成或批准新修订。历史 Model 修订缺少计划包时须重新发布。
- 已有服务切换指标来源继续只走 `PUT /query/:id/metric-source`：携带精确实现修订和 正整数 version（查询服务聚合根版本，不依赖可执行快照），在行锁事务内核对后原子替换来源与消费契约；保留服务身份、访问设置和当前版本规则，清除旧来源专用字段。旧冻结 SQL 不能与新包并行执行，消费方仍须显式应用新契约。
- 每次指标查询先向 Model 验证指定修订与依赖，再校验当前引擎能力、本地 CompilerIdentity、请求参数和服务消费版本；撤回、漂移或实现版本不匹配即拒绝，不选最新版。MODEL_URL 与现有 Tenant Service Token 路径保留。
- 指标的结果筛选、选择、排序及 keyset 分页由 Service 校验后形成中立 ResultRequest，Common 包装计划根节点再交给引擎编译；结果筛选不能改变原指标分母，分页不能裁剪原计划断言。参数值（包括结果过滤及 keyset 值）继续单次绑定；cursor 加密、limit+1、保护门禁及输出格式仍沿既有契约。不得进入旧指标 SQL 包装或拉回全量数据后在 Service 中补算。
- 指标执行经既有 QueryRuntimeProvider.PrepareQuery、完整 ReadSet/OutputLineage 和 Service 数据保护门禁。编译期 SourceBindings 不是权限证明；受保护聚合来源不能证明允许输出时仍拒绝。新发布计划包和 Consumer Descriptor 必须同步验证，Workbench 不解析引擎、计划或原生查询。
- Query Service 普通查询与单次有界导出使用同一 operation；可选 `X-ADDP-Query-Intent: query | export` 只表达审计用途，不改变授权与上限。CSV 和 GeoJSON 都必须返回 `X-ADDP-Has-More`、`X-ADDP-Next-Cursor` 和 `X-ADDP-Service-Version`，审计不得记录筛选字面值、cursor、原始 Body、SQL 或返回数据。
- 已发布 QueryService 的 REST Query 与 OGC API Features 通过同一 PreparedQuery 执行 `service_execute` 保护；命中纳管资源后必须使用完整 ReadSet、OutputLineage 和 Security 下发的 Service 独立规则在服务端格式化前保护结果。分页 cursor 与 feature ID 使用 AEAD 不透明令牌，不能暴露稳定键或排序值。联邦、图、旧 Data API、查询样例和瓦片在独立动作执行器完成前继续资源级拒绝，不复用 `service_execute`。
- 表模式 QueryService 的直接查询必须从发布快照的输出契约枚举完整源字段，不得生成 `SELECT *`；请求选择字段仍由结构化查询计划在外层收窄。这样 PreparedQuery 可以证明完整输出血缘，并对被抑制或遮盖的字段执行统一保护。
- 联邦 SQL 发布时冻结实际引用的 Source Engine ID 并纳入 `dependency_hash`。每次请求由 Service 基于发布快照签发 `service_definition` Execution Authorization，独立 DuckDB Runtime 消费授权并取得连接；Service 不链接 DuckDB 原生库。
- 表结构、空间信息和资源树通过 Meta 共享能力获取；Service 不重复实现资源树、表空间检测或按 `schema/table` 查找资源的代理接口。
- 静态二维瓦片发布只接受 Meta 已识别、位于 Business 存储的 `data_type=media + format=pmtiles + layout=single` item。发布配置保存 ResourceLocator 和 PMTiles v3 依赖快照，运行时通过 System engine provider Range Read，不接受裸路径、URL 或 Manager infra `storage_ref`。
- 三维瓦片不并入二维瓦片服务。3D Tiles / S3M 后续使用独立“三维场景服务”入口和服务类型。
- 公开访问端点要在 Handler 内检查服务的 public/private 权限，避免绕过认证。
- `/api/v1/service` 管理 API 只接受 canonical Bearer Tenant AuthContext；内部 API Key 不得跳过认证或伪造用户。匿名公开端点可选解析 Bearer，但 private 服务必须校验当前 AuthContext Tenant 与服务 Tenant 一致。
- 瓦片缓存使用系统 MinIO，路径和缓存策略应保持租户隔离。
- 修改 API 后同步 Swagger：`bash scripts/swagger/gen-swagger.sh service` 和 `bash scripts/swagger/check-route-coverage.sh service`。

## 开发与验证

```bash
bash scripts/dev/start.sh -service
bash scripts/dev/restart.sh -service
curl http://localhost:8086/health/ready
```

常用日志：

- `logs/service-backend.log`

## 相关文档

- `service/docs/数据库架构.md`
- `service/docs/service核心概念和架构设计.md`
- `service/docs/外部服务架构设计.md`
- `service/docs/瓦片服务使用指南.md`
- `service/docs/API测试指南.md`
- `service/docs/tables/`
- `gateway/docs/gateway架构说明.md`

## 前端公开路由

- 模块内 Router 使用 `/query-services`、`/registered-services`、`/published-services`、`/tile`、`/graph-services` 等无模块前缀路径；Console 公开 URL 统一加 `/service` 前缀。
- 资源身份和创建、编辑、详情、测试职责使用 path 表达；创建成功后用 `replace` 进入详情，其余列表到详情使用 `push`。
- 服务目录默认 `all` Tab 省略，其他稳定类型使用唯一 `tab` query。
- 业务导航统一调用 `frontend/src/utils/moduleNavigation.js`。

查询服务血缘通过现有 Meta 发布接口同步人类可读的 `Title`（`service_name`）及 `UpdatedAt`（`service_updated_at`）。Service 启动后及每分钟按主键分页重放 owner 发布事实，补齐名称并重试失败投递；非 active 状态同步空依赖。Meta 关闭旧版本当前投影，保留历史观察，并拒绝过期通知。不得借用仅面向 Catalog 的 resolver 权限。此契约由 Service T1、Meta T1 和 Meta PostgreSQL lifecycle migration 门禁验证。

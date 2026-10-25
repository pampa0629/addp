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
- 管理 API：查询服务、图查询服务、注册服务、瓦片服务与图层按 ID 读取或写入时，必须以 AuthContext 的当前 Tenant 限定资源；`/tile-layers/:serviceId/:layerId` 还须校验图层所属服务与路径一致，跨租户或不匹配返回 404。注册服务的公开代理端点属于单独的外部消费边界。
- 图查询服务管理：`POST/GET /graph`、`GET/PUT/DELETE /graph/:id`；公开执行端点：`POST /api/gquery/:serviceName`。 DTO/页面地址来源、认证与保护及分层证据统一见[图查询服务验收入口](docs/testing/验收方法与排查.md#图查询服务验收入口)，不将管理接口或其他图模块的验证视为此消费链路已通过。
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
- 指标来源的创建与依赖复核统一见[来源选择与发布](docs/architecture/指标来源与版本.md#来源选择与发布)：`config_type=analytical` 只绑定 Model 精确实现修订，不取最新版、不接受调用方 Plan，与 SQL/table、`runtime_engine_id` 及参数／输出覆盖互斥，仍复用 PreparedQuery 授权与执行。冻结包、owner 修订引用、依赖摘要与只读投影沿[来源与发布快照](docs/tables/query_services表.md#来源与发布快照)维护，不另存可编辑契约，不把指标包拷贝到 SqlQuery 或强制原生语言为 SQL。
- 旧指标 SQL 的停用 analytical 迁移、旧字段清理和 owner 修订引用保留统一见[当前迁移入口](docs/migrations/README.md#当前入口)；须显式重绑，禁止自动生成或批准新修订，历史 Model 修订缺少计划包时须重新发布。断言与未覆盖项见[迁移与持久化覆盖](docs/testing/验收方法与排查.md#迁移与持久化覆盖)；迁移成功不代表完整升级或业务执行已验收，未运行或未覆盖不计通过。
- 指标重绑与版本规则统一见[版本、并发与消费绑定](docs/architecture/指标来源与版本.md#版本并发与消费绑定)：唯一重绑命令携带精确修订与正整数 `version`，在当前 Tenant 的行锁事务中校验、原子替换来源与消费契约并递增版本；保留服务身份、访问与启停，清除旧来源字段。管理 `version` 与消费 `service_version` 分别维护，冲突不自动重试；消费方须显式更新绑定并发布应用新修订。断言范围见[迁移与持久化覆盖](docs/testing/验收方法与排查.md#迁移与持久化覆盖)，受控夹具不代替真实引擎或并发部署验收，未运行或未覆盖不计通过。
- 指标查询沿[版本、并发与消费绑定](docs/architecture/指标来源与版本.md#版本并发与消费绑定)逐次复核精确修订、依赖与 `CompilerIdentity`；撤回、漂移或实现不匹配须拒绝，不自动选最新版。验证范围沿[标准入口与证据](docs/testing/验收方法与排查.md#标准入口与证据)，夹具不代替真实执行，未运行或未覆盖不计通过。
- 指标结果筛选与分页沿[统一执行契约](docs/architecture/查询发布与执行.md#统一执行契约)：Service 形成中立 ResultRequest，Common 包装计划根节点交引擎编译；参数单次绑定，不改变原指标分母、不裁剪计划断言，不回到旧 SQL 包装或全量补算。验证沿[标准入口与证据](docs/testing/验收方法与排查.md#标准入口与证据)，原生单表续页证据不替代指标查询验收，未运行或未覆盖不计通过。
- Query Cursor 与 Feature ID 统一沿[不透明查询令牌](../docs/concepts/addp共享模块介绍.md#不透明查询令牌)复用 `common/opaquetoken` JSON AES-GCM，不保留私有加密实现；Service 保留发布版本、查询指纹与领域载荷校验。AEAD 令牌不得暴露稳定键或排序值，解密成功不代替授权。共享验证沿既有 Service Go T1、PostgreSQL T2 和 Common Go 门禁。
- 指标可筛选字段沿[统一执行契约](docs/architecture/查询发布与执行.md#统一执行契约)从冻结输出派生，Consumer Descriptor 与执行校验共用投影，不保存 `filterable_fields` 副本、不开放来源表字段。消费契约指纹变化须显式更新应用绑定；验证沿[标准入口与证据](docs/testing/验收方法与排查.md#标准入口与证据)，投影声明或夹具不代表真实执行已验收，未运行或未覆盖不计通过。
- 指标只读编译预览的接口、依赖复核与返回字段统一见[执行查询预览](docs/architecture/指标来源与版本.md#执行查询预览)。须校验当前 Tenant 和 `service.definition.read`；仅编译、不执行业务查询，不返回连接配置或保存 SQL 副本。编译成功不证明执行权限或真实取数通过，验证沿专题标准入口，未运行或未覆盖不计通过。
- TiDB 指标绑定、结果编译由 Service T1 覆盖；发布持久化沿 `make test-service-postgres` 覆盖 PG/MySQL/TiDB 包；TiDB 实库执行沿 `make test-common-tidb` 覆盖来源解析、参数绑定、结果筛选及保护拒绝。后者的 Model/System HTTP 响应为受控夹具，不代替 `metric-service-revision-lifecycle` Hosted T4 的完整发布链路验收。
- 指标执行经既有 QueryRuntimeProvider.PrepareQuery、完整 ReadSet/OutputLineage 和 Service 数据保护门禁。编译期 SourceBindings 不是权限证明；受保护聚合来源不能证明允许输出时仍拒绝。新发布计划包和 Consumer Descriptor 必须同步验证，Workbench 不解析引擎、计划或原生查询。
- 普通查询与单次有界导出共用同一 operation、授权及数量上限，沿[身份授权与资源限制](docs/architecture/查询发布与执行.md#身份授权与资源限制)执行。用途头及 CSV/GeoJSON 必需的分页、服务版本响应头统一见[REST Query](docs/testing/查询与要素协议.md#rest-query)，用途头只表达审计意图，不授予额外能力。审计脱敏沿[有限导出规范](../docs/spec/addp数据服务消费与数据应用规范.md#88-有限导出)，不得记录筛选字面值、cursor、原始 Body、SQL 或返回数据。单次有界响应不等于全量导出，实际覆盖以测试专题的分版本证据为准，未运行或未覆盖不计通过。
- REST Query 与 OGC Features 沿[统一执行契约](docs/architecture/查询发布与执行.md#统一执行契约)及[Service 动作规范](../docs/spec/addp数据安全与隐私保护实现规范.md#103-service-service_execute-动作)：命中纳管资源时，基于同一 PreparedQuery 的完整 ReadSet、OutputLineage 与 Service 独立 `service_execute` 规则，在协议格式化前保护结果。联邦、图、旧 Data API、查询样例和瓦片在独立动作执行器完成前继续资源级拒绝，不借用此动作或 Manager 规则。分页方法与实际覆盖见[协议测试专题](docs/testing/查询与要素协议.md)，共用执行路径不代表所有来源、身份、协议或 Hosted 已验收，未运行或未覆盖不计通过。
- QueryService REST Query 与 OGC Features 共用唯一执行错误出口，分类与验收范围统一见[权限与失败用例](docs/testing/查询与要素协议.md#权限与失败用例)：角色资格 403、角色选择失效 409 与通用保护 `403/data_protection_required` 分别核对。Provider 类型化查询错误与 SQLSTATE 优先保留原查询失败分类，不因门禁包装而改为保护拒绝。保护拒绝仅返回本地化 `error` 与稳定 `error_code`，不含原始错误、源结构或数据，不改变规则、有效期或读取授权；共用出口或回归用例存在不代表真实链路通过，未运行或未覆盖不计通过。
- QueryService 预览的失败提示、旧结果与分页元数据清理、日志行为统一见[REST 测试按钮与页面预览](docs/testing/查询与要素协议.md#rest-测试按钮与页面预览)；仅专题列明的保护拒绝省略控制台异常，其他失败保留异常日志。确定性验证沿既有 `make test-service-frontend`；注入响应或无控制台异常不替代真实链路验收，未运行或未覆盖不计通过。
- 原生单表 QueryService 的角色方式、候选、共享表单及消费版本沿[角色脱敏选择契约](docs/architecture/查询发布与执行.md#登录用户的角色脱敏选择)。配置或保留非空选择时，保存、更新、准备与交付均复核同 Tenant 当前第一方 User 资格；具备更新权限时，仅提交 `version` 与 `status=inactive` 可停用并保留选择，不依赖方式仍有效，重新启用、其他更新及实际调用仍严格核验。失效不自动清除，主动移除并保存才恢复默认，来源切换不得隐式丢弃选择；SQL／联邦／对象表／指标的非空角色选择及 AK/SK 绑定未接入。拒绝分类沿[权限与失败用例](docs/testing/查询与要素协议.md#权限与失败用例)，实际覆盖沿[分版本验收证据](docs/testing/查询与要素协议.md#角色与-csv-的历史验收证据)；共用执行路径不等于所有来源、身份、协议或 Hosted 已验收，未运行或未覆盖不计通过。
- 表模式 QueryService 的直接查询必须从发布快照的输出契约枚举完整源字段，不得生成 `SELECT *`；请求选择字段仍由结构化查询计划在外层收窄。这样 PreparedQuery 可以证明完整输出血缘，并对被抑制或遮盖的字段执行统一保护。
- 联邦 SQL 发布时冻结实际引用的 Source Engine ID 并纳入 `dependency_hash`。每次请求由 Service 基于发布快照签发 `service_definition` Execution Authorization，独立 DuckDB Runtime 消费授权并取得连接；Service 不链接 DuckDB 原生库。
- 表结构、空间信息和资源树通过 Meta 共享能力获取；Service 不重复实现资源树、表空间检测或按 `schema/table` 查找资源的代理接口。
- 查询服务血缘统一见[发布血缘同步边界](docs/architecture/查询发布与执行.md#发布血缘同步边界)：当前仅同步具有源 item 身份和依赖摘要的快照；投递失败、周期重试及分层验收按专题核对，不以重放运行证明完整血缘或真实链路已通过。
- 静态二维瓦片发布只接受 Meta 已识别、位于 Business 存储的 `data_type=media + format=pmtiles + layout=single` item。发布配置保存 ResourceLocator 和 PMTiles v3 依赖快照，运行时通过 System engine provider Range Read，不接受裸路径、URL 或 Manager infra `storage_ref`。
- 三维瓦片不并入二维瓦片服务。3D Tiles / S3M 后续使用独立“三维场景服务”入口和服务类型。
- 公开访问端点要在 Handler 内检查服务的 public/private 权限，避免绕过认证。
- `/api/v1/service` 管理 API 只接受 canonical Bearer Tenant AuthContext；内部 API Key 不得跳过认证或伪造用户。匿名公开端点可选解析 Bearer，但 private 服务必须校验当前 AuthContext Tenant 与服务 Tenant 一致。
- 瓦片缓存使用系统 MinIO，路径和缓存策略应保持租户隔离。
- 修改 API 后同步 Swagger：`bash scripts/swagger/gen-swagger.sh service` 和 `bash scripts/swagger/check-route-coverage.sh service`。

## 开发与验证

启动、访问地址和重启入口沿[模块 README](README.md)及[模块选择指南](../scripts/docs/模块选择启动指南.md)。实际地址以标准启动输出和 `.dev-state/ports.env` 为准，不固定请求 localhost:8086；需要启动或重启时按根 AGENTS.md 由用户操作，仅测试或构建无需重启。

启动迁移顺序与账本归属见[迁移说明](docs/migrations/README.md#当前入口)，错误排查见[启动迁移失败](docs/testing/验收方法与排查.md#失败定位)，断言范围见[迁移与持久化覆盖](docs/testing/验收方法与排查.md#迁移与持久化覆盖)。各阶段没有共同事务，后续失败不代表此前已落库变更已回滚；结构迁移、数据迁移与接口验收分别记录，未运行或未覆盖不计通过。

从项目根目录运行标准模块门禁：

```bash
make test-module MODULE=service
```

API 样例先完成[公共准备](docs/API测试指南.md#公共准备)，再进入协议专题；各层级与跨模块验证见[标准入口与证据](docs/testing/验收方法与排查.md#标准入口与证据)，业务结论按[能力与验收边界](docs/API测试指南.md#能力与验收边界)记录。代理须分别验证请求体完整转发、客户端响应完整性与中断失败，未验收范围不能计为通过。

角色方式与 CSV 的历史验证、失败修复及未覆盖项见[角色与 CSV 的历史验收证据](docs/testing/查询与要素协议.md#角色与-csv-的历史验收证据)；阶段性证据仅适用于记录中的源码与环境。

默认保护下原生 MySQL 单表的两页 JSON/CSV 续读、游标衔接和清理证据见[默认保护下两页游标续读](docs/testing/查询与要素协议.md#默认保护下的游标续页本地验收)；401 重放、截断输出判定和未覆盖项由该专题维护，不外推全量导出或其他保护状态的验收结果。

Service 临时角色方式撤销同步后的旧游标 JSON/CSV 拒绝、显式移除后的默认恢复及清理见[旧游标撤销本地验收证据](docs/testing/查询与要素协议.md#角色方式撤销后的旧游标本地验收)，核对 `409/query_protection_choice_invalid` 与无数据响应；不等于角色成员资格撤销、自然到期或其他来源/身份/协议、Hosted 已验收。

同一场景已纳入既有 Security 隔离 suite 的 v26 领域报告，确定性入口 `make test-security-online-runner`；[接入范围与未运行边界](docs/testing/查询与要素协议.md#旧游标场景的隔离套件接入)区分本地证据、Runner 回归与真实 Hosted，不另建 CI 路线。

历史构建证据（2026-10-24，源码 `3bcc59158`）：`make build BUILD_ARGS="--local --services service-backend"` 在 darwin/arm64 实际编译 Service 及配套 runtime-log，编译产物 2 个、缓存复用 0 个，退出码 0；证据目录 `/tmp/p145-service-stable-build-20261024/`。该记录仅证明对应版本的本机构建，不代表当前版本、服务启动或业务输出已验收。

运行日志按实例可信来源和权限查询，沿[失败定位](docs/testing/验收方法与排查.md#失败定位)与[运行日志规范](../docs/spec/addp模块服务运行日志规范.md)核对，不将固定模块文件当作所有部署的日志来源。

## 相关文档

- [数据库架构](docs/数据库架构.md)：当前表、模型与存储边界。
- [核心概念和架构](docs/service核心概念和架构设计.md)：服务对象与专题归属。
- [外部服务架构](docs/外部服务架构设计.md)：注册、代理与直连边界。
- [查询服务创建与详情](docs/architecture/管理界面与模块协作.md#查询服务创建与详情)：来源选择、创建表单、详情与页面预览。
- [指标来源与版本](docs/architecture/指标来源与版本.md)：精确修订、管理/消费版本、重绑与只读编译预览。
- [查询与要素协议](docs/testing/查询与要素协议.md)：REST/Features 请求、地址、身份与失败分支；页面预览或编译预览不代替完整消费验收。
- [瓦片使用指南](docs/瓦片服务使用指南.md)：操作总入口与当前能力边界。
- [发布与管理](docs/tile-guide/发布与管理.md)：创建向导、动态空间表、业务 PMTiles 与图层管理。
- [客户端接入](docs/tile-guide/客户端接入.md)：协议选择、内部图层、样式、视角与私有认证。
- [性能与排查](docs/tile-guide/性能与排查.md)：源侧优化、缓存更新、观测和故障定位。
- [瓦片协议测试](docs/testing/瓦片协议.md)：公开入口、载荷、覆盖、缓存及图层操作的分项验收；路由已挂载不证明完整消费链路通过。
- [API 测试指南](docs/API测试指南.md)：当前样例、公共准备与验收范围。
- [表说明](docs/tables/)：当前表字段与管理入口。
- [Gateway 架构](../gateway/docs/gateway架构说明.md)：统一路由与消费凭据边界。

## 前端公开路由

- `App.vue` 只渲染路由，`Layout.vue` 是模块布局唯一所有者；独立访问只显示一套导航，菜单名称、顺序、图标和目标以 Console 数据服务菜单为准（查询服务、瓦片服务、图查询服务、服务注册、服务目录）。iframe 模式从首次渲染起只显示业务内容。
- 独立访问模块根路径时，在 AuthContext 加载后选择当前账号可进入的首个管理页面：具备 `service.definition.read` 进入查询服务，否则具备 `service.external_registration.read` 进入服务注册；两者都没有时显示无权限。登录重定向保留根路径，由同一选择逻辑处理，不把注册服务账号送到查询服务。

- 模块内 Router 使用 `/query-services`、`/services`、`/published-services`、`/tile`、`/graph-services` 等无模块前缀路径；Console 公开 URL 统一加 `/service` 前缀。
- 资源身份和创建、编辑、详情、测试职责使用 path 表达；创建成功后用 `replace` 进入详情，其余列表到详情使用 `push`。
- 服务目录默认 `all` Tab 省略，其他稳定类型使用唯一 `tab` query。
- 外部服务注册仅使用 `/services`、`/services/create`、`/services/:id`、`/services/:id/edit`，由 `RegisteredServiceList/Form/Detail.vue` 与唯一 `api/registeredService.js` 实现；Console 菜单和服务目录共用这些路径，不保留 `/registered-services` 别名或另一套注册页面。列表参数沿后端契约使用 `page`、`limit`、`search`。
- 注册详情随路由服务 ID 变化重新加载并清空旧内容；过期加载响应和旧身份的操作结果不得更新当前详情、通知或导航。确认框等待期间切换身份后不得发送旧操作请求。
- 注册表单通过共享 `useUnsavedChangesGuard` 保护普通字段、关键词和认证输入；加载成功与保存成功后更新内存基线，保存失败保留修改状态。保存期间禁用表单输入；切换编辑对象时重建草稿并清除旧提交状态；身份切换或组件卸载后忽略旧加载、保存的成功与失败结果，旧保存不得更新基线、通知、导航或解锁新提交。不保存浏览器草稿副本。
- 查询服务表单复用同一离页保护，覆盖 SQL、命名参数及选项、稳定排序键、空间字段、数据源、协议和服务信息；步骤与候选搜索不计入修改。加载已有定义后建立基线，保存成功只确认本次提交的草稿，失败或版本冲突保留输入。加载与提交期间禁止编辑；切换服务身份后忽略旧加载、保存和元数据检测结果。
- 真实查询表单的 iframe 离页回归由 `make test-console-frontend` 执行，复用 `frontend/e2e/query-form.html` 夹具和正式页面/API 客户端；只拦截 HTTP 响应，不写业务数据库；覆盖创建草稿、已有服务版本冲突与重载、同组件身份切换和旧响应隔离。Service 前端变更会通过共享改动矩阵扩散到 Console 门禁。
- `frontend/e2e/navigation.html` 复用正式 App、Layout 和路由表，由 Console 浏览器门禁验证嵌入及独立访问的五个菜单入口、目录内部跳转、刷新和历史恢复；API 响应由测试拦截。
- 注册表单的真实 iframe 离页回归由 Console 的 `registered-service-unsaved.spec.js` 执行，复用上述正式页面夹具，覆盖字段/关键词/认证草稿、内部及宿主导航、历史、刷新、创建与更新的失败保留和成功清除。HTTP 写请求全部模拟，不写业务数据库。
- 业务导航统一调用 `frontend/src/utils/moduleNavigation.js`。

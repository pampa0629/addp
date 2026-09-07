# ADDP Workbench 数据服务消费与数据应用专题

状态：概念设计已确认；Phase 1 至 Phase 4B 已实现并完成标准门禁及真实浏览器生命周期验收；Phase 6 的通用 Value renderer、Map 专题样式、领域事实禁止门禁、Value + Map + Chart + Table 正式组合应用、空间探索创作向导及保存前整页预览均已完成真实浏览器验收。Tile / OGC Features 只在真实数据量超过有界 GeoJSON 上限后启动。

本文跟进 ADDP `workbench` 模块的概念、边界、阶段计划与实施状态。Workbench 是平台级、领域无关的数据服务消费模块，不属于 Outdoor 业务专用能力。Outdoor 只作为首个真实验收场景；后续任何满足消费契约的数据服务都应能以同一主路径接入，禁止在 Workbench 核心模型、API、渲染判断或权限逻辑中硬编码 Outdoor 表、字段、指标或页面。

## 一、原始需求

ADDP 已具备数据开发、计算编排、结果物化和数据服务发布能力，但服务发布后的用户侧消费仍不完整：

- Service 前端的预览面向服务发布者，用于验证服务定义，不是数据消费者的正式应用；
- Portal 在资产授权后主要展示服务端点，没有通用的查询、筛选和可视化运行环境；
- System 的 Application 是外部 API 调用主体和 API Key 管理资源，不是用户界面或数据应用定义；
- 当前没有独立 owner 保存可复用的数据服务查询与展示配置；
- 报表、看板和数据应用尚未形成从服务消费、发布到 Asset/Portal 发现的闭环。

Workbench 的第一目标是补齐：

```text
数据生产与刷新
  -> Service 发布稳定数据契约
  -> Workbench 直接配置数据应用组件、动态查询和展示
  -> 组件布局、参数联动、应用发布和稳定运行
  -> Catalog 建立企业目录身份
  -> Asset 组合与发布、Portal 发现和打开
```

## 二、核心定位

模块稳定名称使用 `workbench`：

| 项目 | 约定 |
| --- | --- |
| 模块名 | `workbench` |
| 中文名 | Workbench / 工作台 |
| 前端入口 | `/workbench`（Console 创作端） |
| 后端 API 前缀 | `/api/v1/workbench` |
| 数据库 schema | `workbench` |
| OAuth Client / Service Principal | `addp-workbench` |

Workbench 是 ADDP 面向数据消费者的数据应用工作空间，负责通过已发布数据服务直接配置应用组件、动态查询、可视化、联动、发布和运行。

Workbench 不负责：

- 原始数据浏览、连接和下载，归 Manager；
- 查询语言、SQL、Notebook 和算子开发，归 Develop；
- 逻辑模型、物化结构和计算结果发布，归 Model；
- 指标定义与业务口径，归 Standard；
- 数据服务定义、执行协议和消费契约，归 Service；
- 资产编目、上架、申请和授权履约，归 Asset；
- 用户侧资产发现和应用入口，归 Portal；
- 调度和任务执行，归 Orchestrator；
- 用户、应用主体、OAuth 和 API Key，归 System。

Workbench 不是 TaskProvider。用户查看、筛选和刷新数据属于在线服务消费，不进入任务编排。昂贵或周期性计算必须由上游任务提前完成，Workbench 只读取发布后的稳定服务。

### 2.1 创作端与运行端

Workbench 由同一 owner 承担创作和运行职责，但两个页面职责使用不同的唯一入口：

| 界面 | 阶段 | 正式入口 | 集成方式 |
| --- | --- | --- | --- |
| Workbench 创作端 | Phase 2 | `/workbench/...` | Console iframe 模块 |
| Data Application 运行端 | Phase 4 | `/data-apps/:application_id` | Console 当前 origin 下的独立顶层页面 |

创作端只维护 Data Application：选择服务后直接配置 Component 的字段、参数和 renderer，再完成布局、共享参数、选择联动和发布。Console 外层 URL 是 iframe 模式的公开路由事实源，模块内使用同一 canonical path，并通过 `common-frontend` Console navigation bridge 同步；前端仍按模块规范支持 standalone 开发和验证，但正式入口不形成第二个认证 origin。

已发布 Data Application 的运行端不嵌入 Console iframe，不显示 Console 管理导航，供 Portal 打开、全屏运行和后续 wallboard 使用。稳定 URL `/data-apps/:application_id` 由 Workbench 解析当前有效发布 Revision；同一页面不存在另一条 `/workbench/...` 运行 URL。

两个界面仍属于同一个 `workbench` 模块，复用同一 Backend、领域模型、renderer 和 Browser AuthSession，不新增 `data-app` 模块、数据库 schema、Service Principal 或独立认证体系。运行端使用当前 User Bearer 调用 Workbench 和 Service，不使用 API Key。

System 的 Application 是外部 API 调用主体与 API Key 管理资源，不是 Data Application 注册表。Data Application 不自动创建 System Application 或 API Key。System 模块运行注册表只登记 `workbench` 模块运行实例，不为每个 Data Application 注册模块；Data Application 的企业发现身份仍由 CatalogEntry 承担。

Phase 2 不创建 `/data-apps` 占位路由、空壳页面或未发布 Application 的兼容入口。只有 Phase 4 建立 Data Application 聚合根与发布 Revision 后，才一次性增加顶层运行端。

## 三、不是 Outdoor 专用应用

Workbench 的稳定设计必须满足以下约束：

1. 只保存 Service owner 发布的类型化 `service_ref`，不保存 Outdoor 表名、字段名或服务 URL；
2. 输入控件由服务输入契约驱动，不按业务名称判断；
3. 表格、图表、地图和后续图展示由输出契约与能力声明驱动；
4. 不假设空间字段名为 `geom`，不假设主键或时间字段名称；
5. 不直接访问 Outdoor 的 Model、PostgreSQL 表或 Develop execution；
6. Outdoor 服务内容刷新不触发 Workbench 重新发布或重新编排；
7. 后续接入其他领域、租户和服务类型时，不新增旁路消费接口。

Outdoor 只负责证明以下通用能力真实可用：

- 稳定服务绑定；
- 参数输入与动态查询；
- cursor 分页；
- 表格、图表和空间结果展示；
- 上游数据刷新后的无感读取；
- 服务契约变化后的显式阻断和修订。

### 3.1 第二个异构验收服务

第二个验收场景固定为 Business MySQL 电商订单分析 Query Service，建议稳定服务名为 `commerce-order-analysis`。仓库已有 `business` MySQL 的 `customers`、`orders` 和相关确定性样例数据；验收服务使用固定只读 SQL JOIN 直接查询既有表，不新增结果表、不在查询时写表，也不复制 Outdoor 的物化刷新链路。

服务输出固定排除姓名、电话、邮箱和地址等个人信息，第一版候选字段为：

| 字段 | 类型重点 | 用途 |
| --- | --- | --- |
| `order_no` | string、非空 | 稳定排序键和 cursor |
| `customer_code` | string | 非敏感客户业务标识 |
| `city` | string | 等值筛选 |
| `membership_level` | string | 等值或集合筛选 |
| `status` | string | 等值或集合筛选 |
| `total_amount` | decimal | 金额格式化和 Chart measure |
| `payment_method` | string | 分类展示 |
| `ordered_at` | timestamp | 时间范围筛选和 Chart dimension |
| `shipped_at` | nullable timestamp | 空值和时间格式化 |
| `active_customer` | boolean | 布尔筛选和展示 |

验收范围包括：

- 以 `order_no` 作为唯一稳定排序键完成 MySQL cursor 分页；
- 使用 `ordered_at` 范围、`status`、`city`、`membership_level` 和 `active_customer` 进行动态字段筛选；
- Table 正确处理 string、decimal、boolean、timestamp 和 nullable timestamp；
- Chart 直接使用服务已返回的订单维度和 `total_amount`，不在客户端聚合；
- Descriptor 不包含 SpatialInfo，因此 Map 必须不可选，不能按字段名猜测空间能力；
- CSV 有限导出、Service 权限和执行面审计使用与 Outdoor 相同的消费主路径；
- 修改消费契约后触发 `contract_fingerprint` 不一致并阻断旧 View 自动查询。

该服务与 Outdoor 构成最小异构矩阵：Outdoor 验证 MongoDB 治理、上游计算物化、指标和空间结果；电商订单服务验证 MySQL 直接固定 SQL、关系型明细、丰富标量类型和无空间输出。Spark、ClickHouse 和 Oracle 暂不加入 Workbench 最小验收，以免把引擎覆盖范围扩张为首期目标。

## 四、与 BI 和大屏的关系

### 4.1 与 BI 的关系

Workbench 是 Service-native Analytics，即以已发布数据服务为唯一数据入口的轻量分析和数据应用运行环境。它覆盖 BI 的一部分消费能力，但不建设完整 BI 数据建模体系。

第一阶段及近期可以提供：

- 动态参数和结构化筛选；
- 表格、基础图表和地图；
- 排序、分页和有限结果导出；
- 保存并复用 Data Application Component 配置；
- 多组件联动和可发布应用页面。

Workbench 不提供：

- 数据源直连和连接管理；
- 多表 JOIN、数据清洗和自由 SQL；
- 自有指标口径、计算字段语言或语义模型；
- OLAP Cube 和另一套物化计算体系；
- 绕过 Service 的数据库查询或下载路径。

外部 BI 产品也可以作为数据服务消费者。Workbench 不以替代所有第三方 BI 为目标，而是提供与 ADDP IAM、服务契约、资产治理和数据类型能力原生一致的内置消费环境。

### 4.2 与大屏的关系

大屏不是独立数据源、服务类型或计算模块，而是 Data Application 的一种展示模式：

```text
Service Consumer Descriptor
  -> Data Application Component
  -> desktop | wallboard 应用展示配置
```

`wallboard` 复用同一 Application Revision、Component、Parameter Binding、Selection Binding 和 Service 查询，只改变页面在当前浏览器视口中的布局行为。首段能力固定为视口自适应画布和用户主动全屏；不保存屏幕分辨率、缩放倍率或全屏状态，不复制一套大屏 Component 配置。

轮播、深色主题和专用大屏 renderer 仍属后续能力。`wallboard` 可以在发布快照中选择显式的浏览器前台刷新档位；它不进入 Orchestrator，高成本统计仍必须由上游提前计算并发布为服务。

## 五、领域模型

### 5.1 Explore Session

用户临时选择服务、输入参数和切换展示方式的前端状态，不持久化，不成为领域实体，也不写入查询结果。

### 5.2 已废止的 Workbench View 历史设计

> 本节只保留早期实施背景，不再是现行规范。Workbench View 已确认退出产品和领域模型；`/views` API、前端路由、权限和存储一次性删除，不保留兼容或隐式中间 View。其中关于结构化查询模板、参数控件和 renderer 的有效能力已下沉为 Data Application Component 配置。

第一阶段唯一持久化聚合根，中文统一为“工作台视图”。为避免与数据库 View 混淆，概念层和 API 说明首次出现时必须使用完整名称 Workbench View。

第一阶段固定为：

```text
Workbench View
= 一个数据服务引用
+ 一份参数定义
+ 一份结构化查询模板
+ 默认参数值
+ 一种渲染配置
```

第一阶段字段：

| 字段 | 语义 |
| --- | --- |
| `id` | Workbench View 稳定 UUID |
| `tenant_id` | Tenant 边界，只从 AuthContext 获取 |
| `name` / `description` | 用户定义的名称和说明 |
| `service_ref` | Service owner 提供的类型化稳定引用，包含 `service_type + service_id` |
| `contract_fingerprint` | 保存时绑定的消费契约指纹 |
| `parameter_definitions` | 对 Service Input Contract 的控件选择、标签和展示配置，不重新定义服务参数类型 |
| `query_template` | 结构化查询模板，不包含 SQL 和 cursor |
| `renderer_type` | 第一阶段为 `table | chart | map` |
| `renderer_config` | 列、维度、指标、空间字段和展示配置 |
| `owner_user_id` | 创建 View 的当前 User，只从 AuthContext 获取 |
| `version` | 聚合根乐观并发版本，从 1 开始 |
| `created_at` / `updated_at` | 审计时间 |

第一阶段一个 View 只绑定一个服务和一种主要渲染方式。多服务、多组件和多页面属于 Data Application，不在 View 中提前增加可选数组或页面树。

第一阶段 Workbench View 固定为个人资源：

- 只能由当前已认证 User 创建；
- 列表、详情、更新和删除都必须同时匹配 `tenant_id + owner_user_id`；
- 不存在 `visibility`、`shared_with`、Department、Project Group 或 Role 共享字段；
- Tenant 管理员不因角色而自动取得他人 View 读写权；
- 跨用户消费由后续 Data Application 发布、CatalogEntry、Asset 和 Portal 主路径承担，不在 View 上建立第二套共享授权。

`service_ref` 创建后不可修改。用户需要改用其他服务时必须创建新 View，不在既有 View 上换绑或按名称迁移。

#### 5.2.1 参数与查询模板

第一阶段保存的结构固定为：

```json
{
  "parameter_definitions": [
    {
      "key": "start_date",
      "label": "开始日期",
      "control_type": "date",
      "required": false
    }
  ],
  "query_template": {
    "select": ["person_id", "activity_count"],
    "fixed_filter": null,
    "parameter_filters": [
      {
        "parameter_key": "start_date",
        "field": "activity_date",
        "operator": "gte"
      }
    ],
    "named_parameter_bindings": [],
    "order_by": [
      {"field": "activity_count", "direction": "desc"}
    ],
    "page_limit": 100,
    "format": "json"
  },
  "default_parameter_values": {}
}
```

约束：

- `parameter_definitions` 只保存用户交互配置，不保存或重新定义字段数据类型；
- `fixed_filter` 使用 Service `QueryFilter` 的结构化 AST 并只包含已校验字面值；
- `parameter_filters` 每项把一个 View 参数绑定到一个服务字段和操作符，不使用字符串占位符；
- `named_parameter_bindings` 每项把一个 Component 参数绑定到 Descriptor 声明的服务命名参数；同一 Component 参数只能在字段筛选和服务命名参数中选择一种绑定；
- 第一阶段所有已提交的参数谓词与 `fixed_filter` 使用 `AND` 组合，不建设动态布尔表达式编辑器；
- 未提交的非必填参数不生成谓词，未提交的必填参数在前端和 Workbench 编译层都必须拒绝；
- `page_limit` 是每次读取上限并必须不超过 Descriptor 声明的上限；cursor 只存在当前 Explore Session；
- 运行时参数优先于 `default_parameter_values`，但只有用户显式保存才能修改默认值。

运行时由 Workbench 将当前参数值编译为 Service 现有 `QueryExecutionRequest`，不生成第二种服务请求格式。

#### 5.2.2 唯一 CRUD API

```text
GET    /api/v1/workbench/views
POST   /api/v1/workbench/views
GET    /api/v1/workbench/views/:id
PUT    /api/v1/workbench/views/:id
DELETE /api/v1/workbench/views/:id
```

- `GET /views` 只列出当前 `tenant_id + owner_user_id` 的 View，在计算 `total` 和分页前完成 owner 过滤；
- `POST /views` 返回 `201` 和新建完整对象；
- `PUT /views/:id` 是完整更新，必须提交正整数 `version`，成功返回递增版本的完整对象；
- 版本冲突返回 `409` 和 `workbench_view_version_conflict`，不自动重试或覆盖；
- 非当前 owner 的 View 在列表中不可见，精确读取、更新和删除统一按不存在处理；
- 第一阶段不存在 `/execute`、`/share`、`/duplicate`、自动保存或第二套 View 写入端点。

创建和更新时，Workbench Backend 必须代表当前 User 读取 Service Consumer Descriptor，校验字段、操作符、参数默认值、分页上限和 renderer 配置，并由后端记录当前 `contract_fingerprint`。创建和更新请求不接受 `tenant_id`、`owner_user_id`、`contract_fingerprint`、cursor 或任何 Token。

View 列表和详情只返回 Workbench 自身保存的配置，不因 Service 不可达而失败。前端进入 View 时再以当前 User Bearer 并行读取 Descriptor，判断服务可用性和契约指纹；真实查询仍直接调用 Descriptor 声明的 Service operation。

Workbench View 禁止保存：

- SQL、MQL、Cypher 或其他查询语言文本；
- Engine ID、schema、table、对象路径或连接信息；
- API Key、User Token、Service Token 或其他凭据；
- 查询结果、下载内容或缓存数据；
- cursor；
- 服务 URL；
- Owner 模块内部 DTO。

### 5.3 Data Application

Data Application 是 Workbench 唯一持久化的用户创作聚合根，中文统一为“数据应用”。用户从创建页选择已发布数据服务，直接配置一个或多个 Component，随后完成布局、应用参数、组件联动、发布和独立运行。单组件应用与多组件应用使用同一条路径。

Data Application Component 是应用内聚实体，不单独拥有 CRUD、权限、共享或发布生命周期。每个 Component 至少持有：

- `service_ref` 和 `contract_fingerprint`；
- 参数定义、结构化查询模板和默认参数值；
- `renderer_type` 和严格类型化的 `renderer_config`；
- Application 内的组件标识与布局位置。

Data Application 的草稿和 Revision 直接保存 Component 快照；创建、更新和发布时均由 Backend 重新读取 Consumer Descriptor 并记录当前契约指纹。Application Component 只保存 Service 消费配置，不保存查询结果、凭据或服务 URL。

Data Application 聚合根负责：

- 一个或多个 Component 及其页面和布局；
- 应用级参数及其到 Component 参数的显式绑定；
- `desktop | wallboard` 展示配置；
- 草稿、发布、下线和版本生命周期；
- 稳定运行入口以及与 CatalogEntry 的发布衔接。

草稿由创建者维护。发布产生不可变的 Application Revision；后续编辑基于新草稿形成新 Revision，不能原地修改已发布版本。首次发布为 Data Application 建立稳定 CatalogEntry，后续 Revision 沿用同一 Data Application 与 CatalogEntry 身份，不为每个版本创建平行目录项。

Data Application 可以只有一个 Component。是否成为应用取决于显式的组合和发布意图，而不是组件数量。

Data Application 不授予底层数据访问权。运行时每个 Component 都使用当前访问者身份调用其 Service，由 Service 实时执行 Permission、Resource Grant / Policy 和契约校验；Application 发布、CatalogEntry 或 Asset 授权都不能替代 Service 最终授权。

Phase 4A 的最小创作范围固定为单页 `desktop`：一个页面、十二列栅格和一个或多个 Component。Selection Binding 同页联动和 `wallboard` 展示模式进入 Phase 5；`mobile`、多页面和轮播仍属后续范围，数据库快照不得预埋未实现的第二套页面或展示模式字段。

Data Application 使用 Workbench 唯一的用户创作 API：

```text
GET    /api/v1/workbench/data_applications
POST   /api/v1/workbench/data_applications
GET    /api/v1/workbench/data_applications/:id
PUT    /api/v1/workbench/data_applications/:id
DELETE /api/v1/workbench/data_applications/:id
POST   /api/v1/workbench/data_applications/:id/publish
POST   /api/v1/workbench/data_applications/:id/offline
GET    /api/v1/workbench/data_applications/:id/runtime
```

创建请求提交名称、说明和完整 `snapshot`。`snapshot.components` 至少包含一个当前用户可消费的已发布 Service；Workbench Backend 逐一读取 Consumer Descriptor，不信任客户端提交的契约指纹，并将当前权威指纹归一化进草稿。请求中不存在 `source_view_ids`。

聚合根保存当前草稿快照、正整数并发 `version`、`unpublished | published | offline` 发布状态和当前 Revision 编号。`PUT` 原子完整替换草稿；发布在一个事务中校验 `version`、写入新的不可变 Application Revision、切换当前 Revision 并递增聚合版本；下线只切换发布状态并递增聚合版本，不删除最后发布修订。只有从未发布的应用允许携带当前 `version` 删除，已产生 Revision 的应用只能下线。

应用发布版次使用 `revision_number`，不得复用资源并发字段 `version`。应用更新、删除、发布和下线都必须在请求体携带当前正整数 `version`；冲突统一返回 `409 + workbench_data_application_version_conflict`，不得自动重试或服务端兜底当前版本。

Phase 4A 的稳定运行响应只向创建者开放，并要求 `workbench.data_application.execute`。这不是临时兼容路径：创建者始终可以运行自己发布的应用；其他用户必须等 Phase 4B 接入 owner Resource Grant、Asset 履约和 Portal 打开链路后才可运行。当前不得用“同 Tenant 即可读”、公开链接、API Key、专属 Token 或 Catalog 可见性代替资源授权。

发布后的 Data Application 先由 Catalog 建立企业 CatalogEntry，再由 Asset 通过 `AssetComponent.catalog_entry_id` 组合为 `application` 类型资产。Asset 只保存资产组合、发布和授权履约事实，不保存应用页面、组件树或运行配置。

### 5.4 Phase 5 组件联动与受控下钻设计

Phase 4A 已实现的“参数联动”是一个 Application Parameter 通过多条 `parameter_bindings` 同时驱动多个 Component；它仍由用户在参数区输入值，不表示某个 Component 的选择结果可以改变其他 Component。Phase 5 必须把两类 Binding 分开建模，不能让 renderer 直接修改目标 Component 的查询：

```text
用户输入 ──────────────────────────────┐
                                      v
Renderer 结果选择 -> Selection Binding -> Application Parameter
                                      -> Parameter Binding
                                      -> Component Query -> Service
```

稳定概念为 **Selection Binding / 选择绑定**：Data Application Revision 中声明“从哪个源 Component 的当前结果选择哪些字段，并原子写入哪些 Application Parameter”。目标 Component 不在选择绑定中重复保存，而是由既有 `parameter_bindings` 唯一推导；同一 Component 的真实请求继续只由 `buildComponentQuery` 一类参数编译主路径生成。

最小快照结构为：

```json
{
  "selection_bindings": [
    {
      "source_component_id": "8e4f...",
      "assignments": [
        {
          "source_field": "city_code",
          "application_parameter_key": "selected_city"
        }
      ]
    }
  ]
}
```

首期不增加 `event`、`action`、`target_component_ids`、`query_mode`、表达式或脚本字段：每个源 Component 最多一条选择绑定，事件固定为用户选择一个当前结果，赋值后自动查询由这些 Application Parameter 影响的去重 Component 集合。以后只有出现第二种已经确认的真实交互时，才讨论扩展契约，不能预先建设通用事件 DSL。

#### 5.4.1 受控下钻的首期边界

首期“下钻”只表示同一页面内的汇总到明细：源 Component 返回的某个标量字段被显式映射到 Application Parameter，下游明细 Component 本来就通过公开 Service Input Contract 接受该参数。Workbench 不推断层级、不生成维度路径，也不在浏览器聚合或拼接查询。

首期不包含：

- 跳转到任意 URL、外部应用或未声明页面；
- 动态替换 ServiceReference、operation、字段名或查询模板；
- 多页面导航、面包屑、返回栈和跨页面状态恢复；
- bbox、范围刷选、多选集合、框选、级联树或任意值转换；
- Renderer 之间直接发请求、直接持有对方实例或共享私有状态。

多页面下钻必须等 Phase 5 的多页面模型单独确认后，再复用 Application Parameter 状态；不能为首期同页联动预埋隐藏页面或通用导航动作。

#### 5.4.2 Renderer 与 Workbench Runtime 的职责

三类共享 renderer 只发出统一的 `result-select` 意图，payload 固定为当前结果中的 `row_index`，不携带 ServiceReference、目标 Component、参数名或任意查询片段：

| Renderer | 选择来源 | `row_index` 来源 |
| --- | --- | --- |
| Table | 用户点击当前页的一行 | 当前页原始 rows 下标 |
| Chart | 用户点击 bar、line point 或 pie item | ECharts `dataIndex`；Chart 不在客户端排序或聚合，因此仍对应原始 rows |
| Map | 用户点击一个 Feature | 构造 Feature 时保留的原始 rows 下标，不依赖 tooltip 字段 |

`WorkbenchRendererHost` 负责把三个 primitive 的事件归一化，Data Application Runtime 再用源 Component 当前的原始 rows 和 `selection_bindings` 读取字段值。共享 renderer 不读取选择绑定，不持久化选择，也不认识 Workbench 领域模型。

一次选择的运行语义固定为：

1. 先读取并校验该选择绑定的全部 assignment；任一字段缺失、类型不兼容或必填目标收到 `null` 时，整次选择不修改任何参数；
2. 原子更新 Application Parameter 状态，参数区同步显示新值；
3. 根据 `parameter_bindings` 推导并去重受影响 Component，把它们的 cursor 全部重置到第一页；
4. 并行查询受影响 Component，每个 Component 独立显示 Service 错误，已更新参数不因局部请求失败而回滚；
5. 同一目标的快速连续选择采用 last-selection-wins，请求序号较旧的响应不得覆盖较新的结果；由 props 更新或查询完成引起的 renderer 重绘不得再次发出选择事件。

手工输入 Application Parameter 仍按现有“查询全部”主路径提交，不因首期选择联动改成逐键自动查询。用户选择结果后可以直接在现有参数控件中查看、修改或清空最终值，不建立第二份不可见的 interaction state。

#### 5.4.3 保存与校验

`selection_bindings` 属于 Data Application 草稿和不可变 Application Revision 快照，不单独建表，不增加运行 API，也不保存用户实际选择值或查询结果。该字段是 `addp.workbench_data_application/v1` 当前契约的附加集合：缺失时由同一规范化主路径收敛为空数组，既有不可变 Revision 保持原文和原行为；Backend 不增加 v1/v2 双运行分支。

Workbench Backend 在草稿保存和发布时必须使用已读取的 Consumer Descriptor 严格校验：

- 源 Component 存在，同一源 Component 只有一条选择绑定；
- assignment 的源字段同时存在于源输出契约和 `query_template.select`；
- 源字段是 `string | bool | int | bigint | float | double | decimal | date | time | timestamp | uuid` 标量，不接受 geometry、array、json、bytes 或 unknown；
- Application Parameter 存在，同一条选择绑定不能重复写入同一参数；
- 源输出 `FieldType` 与该 Application Parameter 所有目标 Component Filter 的输入 `FieldType` 完全一致，首期不做字符串、数值、日期或单值/数组的隐式转换；
- 必填 Application Parameter 的源输出字段必须声明为非 nullable；目标 Filter 必须是现有标量操作符，`in | not_in | is_null | is_not_null` 不进入首期选择绑定；
- Descriptor fingerprint、Component 配置、Parameter Binding 和 Service 最终执行权限继续走已有校验，不因联动新增旁路。

前端编辑器新增“选择联动”区即可，不建设节点画布。创作者选择源 Component 后，源字段选项来自当前 Descriptor 输出契约，Application Parameter 选项按上述类型规则过滤；界面只读展示由既有参数绑定推导出的受影响 Component，保存时仍由 Backend 作最终校验。

`Selection Binding` 已补入正式术语表和核心概念关系图；实现按 Backend 快照 DTO、共享 renderer 事件、Runtime 和编辑器的单一路线推进。

### 5.5 Phase 5 单页 Wallboard 展示模式

稳定概念为 **Application Display Mode / 应用展示模式**。展示模式属于 Data Application 当前页面的发布事实，最小快照字段为：

```json
{
  "page": {
    "id": "...",
    "title": "...",
    "display_mode": "wallboard",
    "placements": []
  }
}
```

当前只允许 `desktop | wallboard`：

- `desktop` 保持现有十二列流式页面，按 Component 高度自然滚动，适合个人交互分析；
- `wallboard` 使用同一十二列 placement，把全部布局行压入扣除页头和参数区后的当前视口，Component 内部自行处理滚动和 resize，适合会议室、展厅和态势总览；
- 两种模式共享同一 Component、Application Parameter、Parameter Binding、Selection Binding、ServiceReference、运行 URL 和当前访问者授权，不存在第二套大屏查询或大屏应用实体；
- 编辑器只选择展示模式，不保存屏幕物理尺寸、浏览器尺寸、缩放值或全屏状态。旧 Revision 读取时由同一快照规范化主路径把缺失值收敛为 `desktop`，新建、更新和发布只接受显式合法枚举；不增加 v1/v2 运行分支；
- 全屏按钮在两种模式都可使用，调用浏览器 Fullscreen API；进入或退出状态只存在于当前浏览器会话，并监听浏览器自身的 `fullscreenchange`，发布修订和后端不保存该状态。

本段不包含 `mobile`、多页面、画布拖拽缩放、轮播、自动刷新、主题切换、电视遥控交互或专用大屏 renderer。`wallboard` 不能绕过 Service 的有界结果、权限和契约校验，也不自动触发后台任务。

### 5.6 Phase 5 Wallboard 应用刷新策略

稳定概念为 **Application Refresh Policy / 应用刷新策略**。它是 Data Application 当前页面的发布事实，继续位于同一 `addp.workbench_data_application/v1` 快照中：

```json
{
  "page": {
    "display_mode": "wallboard",
    "refresh_interval_seconds": 60
  }
}
```

首期只允许 `0 | 30 | 60 | 300`，其中 `0` 表示关闭；非零值只允许与 `display_mode=wallboard` 同时保存，`desktop` 必须为 `0`。使用固定档位而不是任意秒数，可以限制 Service 压力并让发布修订的行为可预期。既有快照缺少该字段时由同一解码主路径收敛为 `0`，新建、更新和发布均输出并校验显式整数，不增加兼容字段或第二套 schema。

运行语义固定为：

1. 运行页完成 Application Revision 和全部 Consumer Descriptor 加载后，启用刷新时立即执行一次“查询全部组件”；
2. 前一次自动查询结束后才开始计算下一次间隔，不使用可能堆积请求的固定并发 tick；
3. `document.hidden=true` 时不启动新查询并清除等待中的计时器，页面重新可见时立即刷新一次再恢复间隔；
4. 手工查询、选择联动或其他 Component 查询仍在进行时，自动刷新跳过本轮并重新等待完整间隔；已有 latest-request-wins 继续保护用户快速交互产生的响应顺序；
5. 单个 Component 失败只显示其现有错误并继续后续刷新，其他 Component 的成功结果不回滚；参数当前值保持不变，每次刷新都按当前 Application Parameter 状态重新构造查询；
6. 计时器只存在于当前运行页浏览器会话，离开页面即销毁，不创建 Task、Schedule、Execution、服务端轮询、查询结果缓存或新的 Workbench 运行 API。

刷新只能再次读取已发布 Service 的当前有界结果，不能承担上游数据生产、昂贵统计、物化或告警职责。本段仍不包含页面轮播、后台标签页刷新、自定义 cron、秒级刷新、失败通知或跨终端同步。

### 5.7 Phase 5 Wallboard 应用呈现区块

稳定概念为 **Application Presentation Sections / 应用呈现区块**。它只控制正式运行入口的页面说明和查询交互是否显示，不改变 Data Application、Component、Parameter、Binding、查询模板或授权。页面快照增加唯一正向列表：

```json
{
  "page": {
    "display_mode": "wallboard",
    "refresh_interval_seconds": 30,
    "visible_sections": ["title", "parameters"]
  }
}
```

当前区块固定为：

| 值 | 显示内容 | 隐藏时仍保留 |
| --- | --- | --- |
| `title` | `page.title` 与 Data Application 说明 | 修订状态、刷新状态和全屏入口组成的紧凑工具栏 |
| `parameters` | Application Parameter 控件区 | 当前参数状态和 Parameter Binding 仍在内存中参与查询 |
| `query_actions` | “查询全部”、Component“查询”和 Table 分页操作 | 自动刷新、Selection Binding 和错误提示 |

约束固定为：

1. `visible_sections` 是必填、无重复的枚举列表；新建默认包含全部三个区块，既有不可变 Revision 缺失时由同一快照规范化主路径收敛为全部显示；
2. `desktop` 必须包含全部区块，只允许 `wallboard` 隐藏；切换回桌面时编辑器立即恢复全部区块，不增加桌面简洁模式；
3. 隐藏 `query_actions` 时 `refresh_interval_seconds` 必须为非零合法档位，以保证 Descriptor 加载后仍有唯一首次查询主路径；
4. 隐藏 `parameters` 时，每个必填 Application Parameter 必须有默认值，且该默认值对它绑定的每一个 Component Filter 都是运行时有效输入；不能发布一个只能报“缺少必填参数”且又没有输入入口的页面；
5. 修订状态、自动刷新状态、全屏入口、Component 标题、加载与查询错误始终可见，不进入可隐藏列表；隐藏区块不能抑制授权、契约变化或失败提示；
6. 运行页标题使用发布快照中的 `page.title`；Data Application `name` 继续作为聚合身份、列表和目录名称，不再错误替代页面呈现标题。

本段不增加自定义 CSS、任意 DOM selector、按 Component 隐藏、悬浮工具栏、编辑/运行双快照、匿名 kiosk URL 或第二套 renderer。全部配置继续随 Application Revision 不可变发布。

### 5.8 Data Application 资产运营指标评估

“资产运营”不能被收敛成一张跨模块总表。Data Application、承载它的 application Asset、底层 Query Service 和 Task Execution 是不同聚合，必须先区分各自事实，再讨论展示：

| 运营问题 | 唯一事实 owner | 当前可用事实 | 明确不能推断 |
| --- | --- | --- | --- |
| 有多少 application Asset、处于什么状态 | Asset | `asset.assets` 的类型、状态、上架时间 | Data Application 发布不等于 Asset 已上架 |
| 有多少申请、授权和评价 | Asset | Application、Authorization、Rating 及其时间与状态 | `effective` Authorization 只能称“有效授权”，不能称“活跃用户”或“已使用” |
| 某个 Data Application 被多少人成功打开 | Workbench | 当前没有持久化的运行准入事实 | Runtime API 日志、页面曝光、Catalog 点击和 Portal 打开按钮都不能替代成功运行准入 |
| 某个 Query Service 被调用多少次、结果如何 | Service owner 写入的 System Audit Event | `service.query.executed | exported` 的服务身份、结果、返回行数、错误码和查询形状指纹 | 不能反推调用来自哪个 Data Application；同一 Service 可以被多个消费者复用 |
| 任务执行量、成功率、耗时和积压 | execution owner 写入 `common.task_executions`，Monitor 只聚合 | 统一 Execution 事实 | Data Application 在线打开、参数查询和 wallboard 刷新不是 Task 或 Execution |

基于现有事实，第一阶段可直接提供且语义可靠的指标只包括：

1. Asset 按 `application` 类型或具体 Asset 统计资产数量、状态分布、上架趋势、申请结果分布、当前有效授权人数、评价数量和平均分；这些指标继续由 Asset 自己查询私有表，不读取 Workbench 或 System 审计表；
2. Service 按 Query Service 统计调用量、成功 / 拒绝 / 失败分布、查询 / 导出用途、返回行数和错误码；它表达服务运行情况，不命名为 Data Application 使用量；
3. Workbench 当前只展示应用发布状态和 Revision，不虚构“访问量”“活跃用户”“查询成功率”。如果后续确认应用级使用分析确有产品价值，必须先设计并持久化由 Workbench owner 产生的成功运行准入事实，再定义运行准入次数、独立访问用户和按 Revision 分布；失败授权尝试属于审计 / 安全事实，不进入使用量；
4. 一个 Data Application 可以被不同 Asset 以不同发布说明、授权期限和评价运营，Asset 指标必须按 Asset 身份保存；不得假设 Data Application 与 Asset 一一对应，也不得把多个 Asset 的评价或申请直接覆盖回 Workbench 聚合根。

本阶段不建设跨模块“综合热度分”、不让 Asset 在线查询 Workbench 私有表、不让 Workbench 获得 Tenant 全量审计读取权限，也不新增可由浏览器伪造的 `application_id` Service 请求头。若未来需要把具体 Component 查询可靠归因到 Data Application，必须先独立确认受信消费上下文协议及其成本；在此之前，Service 调用指标与 Application 运行准入指标保持两条事实清晰但不伪关联的路线。

第一阶段实现继续复用 Asset 唯一的 `GET /assets/stats/dashboard`，通过可选 `type_code` 与 `asset_id` 收敛统计范围：无参数表示全部资产，`type_code=application` 表示全部数据应用资产，同时传入 `asset_id` 表示该类型下的具体 Asset。接口返回资产状态、申请待审 / 通过 / 驳回、近 30 天上架与申请趋势、评价数与均分；授权指标固定为当前未过期且已完成履约的 `effective` Authorization 按 `user_id` 去重数，字段命名为 `effective_authorized_users`，删除含义模糊的 `authorization_active`。指定 Asset 不存在、跨 Tenant 或与类型不匹配时返回不存在，不返回伪造的全零结果。

该统计接口同时读取 Asset Entry、Application、Authorization 和 Rating 四类私有事实，因此管理端调用必须同时具备四类只读 Permission；不能因结果是聚合值就只校验 Asset Entry 读取权限。前端运营看板默认选择全部数据应用资产，可切换到全部资产或具体数据应用 Asset；范围提示必须明确“仅统计 Asset 自有事实，不代表应用访问活跃度”。

### 5.9 外部 BI 消费服务契约

外部 BI 是 Service 的另一类客户端，不是 Workbench 插件、Data Application 运行模式或新的数据服务类型。它与 Workbench 共用 Service owner 的消费控制面和执行面，但不读取 Data Application、Application Revision、renderer 配置或 Asset 私有表：

```text
外部 BI
  -> 用户委托 OAuth
  -> Service Consumer Catalog
  -> Service Consumer Descriptor
  -> Descriptor 声明的 query operation
```

第一版只采用用户委托 OAuth。外部 BI 使用独立注册的 OAuth Client，通过 Authorization Code + PKCE 获得绑定当前 User、当前 Tenant Membership 和当前 Role Assignment 的短期 User Access Token，并按 Refresh Token Family 规范轮换。Consumer Catalog、Descriptor 与私有 Query Service 执行均携带同一 Bearer，由 Service 实时校验 `service.data_read.execute` 和后续 Resource Grant / Policy。

第一版明确不采用以下路线：

- 不把 System Application API Key 转换为用户身份、Tenant Membership、Role Permission 或 Service Resource Grant；
- 不让外部 BI 复用 `addp-cli`、`addp-workbench`、`addp-service` 或其他内置 OAuth Client；
- 不用 Client Credentials Service Principal 获得通用 Tenant 数据权限；
- 不增加 Workbench 查询代理、外部 BI 专属执行端点、静态 Token、数据库直连、JDBC / ODBC 旁路或手工复制 Access Token；
- 不要求外部 BI 解析 Service 管理 DTO、SQL、Engine、schema、table 或内部执行计划。

API Key 仍只表达外部应用身份以及 Gateway 层可选的配额和审计语义，不是私有 Service 数据授权凭据。公开 Query Service 可以按其公开 operation 匿名执行；若后续要求公开调用必须携带 API Key 以归属配额和审计，必须先让该公开 operation 真实经过 Gateway API Key 中间件并校验精确 `allowed_services`，不能仅在指南中宣称当前已有能力。API Key 与 OAuth Bearer 不得同时作为私有服务的两条认证主路径。

外部 BI 的最小运行契约固定为：

1. 使用用户委托 OAuth 获取并轮换 Bearer；
2. `GET /api/v1/service/consumer/services` 枚举当前用户此刻可执行的服务；
3. `GET /api/v1/service/consumer/services/:service_type/:service_id` 取得 `addp.service_consumer/v1` Descriptor，并以 `contract_fingerprint` 判断连接定义是否需要显式修订；
4. 只调用 Descriptor `operations` 声明的 method + path，不猜测 `service_name` 或拼接管理路由；
5. 按 `input_contract` 构造结构化筛选、字段选择、稳定排序和 cursor 分页，按 `output_contract` 建立字段类型；
6. 普通刷新使用 `query` intent；有限导出才使用 Descriptor 声明的 export intent，同样服从单次有界结果和 `page.has_more`；
7. 401 触发标准 OAuth 刷新或重新授权，403 表示当前用户不再具备数据权限，契约指纹变化必须阻断自动刷新并要求显式修订。

System 已提供面向租户管理员的外部 OAuth Client 注册治理：客户端固定为无 Client Secret 的 Public Client，只接受 Authorization Code + PKCE、`addp.api` scope / audience 和受约束的 redirect URI；支持查询、创建、更新、停用、恢复、并发版本与审计。停用会在同一事务中取消待处理授权请求并撤销该客户端的有效 Refresh Token Family，恢复只允许建立新的用户授权，不恢复历史会话。租户 Client 的授权请求和授权决定还必须由该 Client 所属 Tenant 的 User AuthContext 完成，不能跨 Tenant 借用。

正式“可直接照做”的产品接入指南仍须等待真实 BI Connector 完成 OAuth、Descriptor、cursor、刷新、权限撤销和契约变化验收，再把经过验证的请求样例整理成文。具体 BI 品牌只是验收载体，不进入 Service Descriptor 或 ADDP 核心领域模型。

## 六、Service Consumer Descriptor 与 Consumer Catalog

### 6.1 必要性

Workbench 不得读取 Service 管理 DTO，因为管理 DTO 包含 SQL、Engine、schema、table 和发布配置。Service 必须提供面向消费者的独立只读投影，稳定名称为 Service Consumer Descriptor（服务消费描述），协议版本从 `addp.service_consumer/v1` 开始。

Consumer Descriptor 采用“通用信封 + 可辨识的强类型输入/输出契约”，不强迫所有服务伪装成表查询。候选结构：

```json
{
  "schema_version": "addp.service_consumer/v1",
  "ref": {
    "service_type": "query",
    "service_id": 123
  },
  "title": "人员活动统计",
  "description": "...",
  "status": "active",
  "access_mode": "private",
  "contract_fingerprint": "sha256:...",
  "operations": [
    {
      "key": "query",
      "method": "POST",
      "path": "/api/query/outdoor-person-activity/query",
      "input_kind": "structured_query",
      "output_kind": "tabular"
    }
  ],
  "input_contract": {
    "kind": "structured_query",
    "schema": {}
  },
  "output_contract": {
    "kind": "tabular",
    "schema": {}
  }
}
```

其中：

- `schema_version` 只表示 Consumer Descriptor 协议版本；
- `ref` 是 Service owner 内的稳定运行时引用；`service_type` 固定使用 `query | graph | tile | registered`，`service_id` 固定为当前 Service 聚合根的正整数 ID；
- `operations` 声明消费操作及其协议，不暴露内部执行计划；
- `input_contract` 描述参数、字段筛选、排序、分页和空间查询能力；
- `output_contract` 描述返回数据类型、字段、空间、图或其他结构事实；
- `contract_fingerprint` 只覆盖对消费者可见的输入、输出和操作契约，不因普通数据内容刷新而变化。

Service 不声明 Workbench 的 `table | chart | map` 等 renderer 名称，因此 Descriptor 中不存在 `presentation_capabilities`。Service 只声明输出结构和数据能力，Workbench 负责将它映射为可用 renderer。

`schema_version`、`contract_fingerprint` 和执行结果中的 `service_version` 互不替代：

- Descriptor 结构升级才改变 `schema_version`；
- 字段、类型、参数、操作符、排序键或公开协议变化才改变 `contract_fingerprint`；
- 发布运行修订和 cursor 绑定使用 `service_version`；
- 仅数据内容刷新不得改变 `contract_fingerprint`。

Consumer Descriptor 不返回：

- 查询语言和内部执行计划；
- Engine、存储路径和连接信息；
- 发布者管理字段；
- 凭据；
- 任意未经过白名单投影的 `data_config`。

### 6.2 稳定引用与 CatalogEntry 边界

Workbench 的 `service_ref` 不使用已废弃的通用 Owner ResourceRef，也不使用 CatalogEntry ID 代替运行时服务引用。三者语义必须分离：

| 引用 | owner | 用途 |
| --- | --- | --- |
| `ServiceReference {service_type, service_id}` | Service | 精确解析 Consumer Descriptor 并执行在线服务 |
| `catalog_entry_id` | Catalog | 表示服务或数据应用的企业目录身份、语义和治理关系 |
| `asset_component.catalog_entry_id` | Asset | 将一个或多个已目录化对象组合为可发布资产 |

Workbench 运行不以 Catalog 可达为前提。Service 或 Workbench 资源后续进入企业目录时，由 Catalog 建立来源绑定，但 Catalog 不接管 Service 的执行定位。

`service_name` 仅是人可读名称和部分协议路径标识，不是 Data Application Component 的身份引用。服务删除后重新创建同名服务必须产生新 `service_id`，已有 Component 继续显示原服务不可用，不得自动改绑。

### 6.3 服务类型扩展

第一阶段只实现 Query Service 消费适配器，因为它已经具备结构化筛选和 cursor 分页，是最小闭环。

后续候选包括：

- Graph Query Service；
- Tile / OGC Service；
- Registered External Service；
- 后续新增且能够声明标准消费契约的服务类型。

Workbench 使用 `service_type + contract` 选择适配器和 renderer。新增服务类型时，Service 先提供真实 Consumer Descriptor 和执行协议，Workbench 再增加对应适配器；不得仅在前端增加硬编码类型并猜测响应。

Service 类型与输入/输出契约的预期关系：

| 服务类型 | 输入 | 输出 | Workbench 后续适配 |
| --- | --- | --- | --- |
| Query Service | `structured_query` | `tabular` / `spatial_tabular` | table / chart / map |
| Graph Query Service | 图查询强类型输入 | `graph` / `tabular` | graph / table |
| Tile / OGC Service | 图层、范围、级别 | `tile_layer` / feature collection | map |
| Registered Service | 协议特定 | 协议特定 | 专用 adapter |

“通用”表示共用发现、引用、授权和 Descriptor 信封，不表示所有服务必须共用一种请求和响应。

### 6.4 Service Consumer Catalog 与唯一 API

Service 拥有 Service Consumer Catalog（服务消费目录），用于返回当前用户此刻可执行的服务投影。它与企业 Catalog 的分工是：

- 企业 Catalog 负责跨专业对象的发现、理解、治理和资产组合；
- Service Consumer Catalog 负责已发布服务的当前可执行投影和 Consumer Descriptor；
- Workbench 只从 Service Consumer Catalog 选择可用服务，不读取发布者管理列表；
- Asset/Portal 用于资产发现、申请和打开，不替代运行时 Consumer Catalog。

消费控制面固定为：

```text
GET /api/v1/service/consumer/services
GET /api/v1/service/consumer/services/:service_type/:service_id
```

列表只返回轻量摘要，详情返回完整 Descriptor。列表过滤参数固定为 `search`、`service_type`、`output_kind`、`page`、`page_size`，使用平台管理列表的标准分页响应。`service_type` 路径参数只接受上述四个稳定枚举，`service_id` 只接受正整数。

“当前可执行”必须在计算 `total` 和分页之前由 Service 一次性应用 Tenant、服务状态、公开策略、`service.data_read.execute`、Resource Grant / Policy 和 Deny，不能先查管理列表再 N+1 过滤。列表和详情使用同一授权判断，已失去执行权的服务不得通过详情路由泄露 Descriptor。

Phase 1 首次实现采用明确的 fail-closed owner policy：只返回当前 Tenant、`active`、启用 REST Query operation 的服务，并只接受当前 Tenant Scope 的 `service.data_read.execute` Role Assignment。现有 Service 尚无“服务—Department / Project Group” Resource Scope Binding，因此 Department 或 Project Group Scope Assignment 不能临时扩大为全租户服务访问；细粒度 Grant、Binding 和 Explicit Deny 建立事实模型后，必须接入同一 Repository 过滤与详情判断入口，不能新增第二套 ACL 或先分页后过滤。

Query Service 的 `active + rest_api.enabled=true` 是可消费状态约束，不是仅供管理端展示的标签：该状态下必须能够生成完整 Consumer Descriptor，包括非空输出字段、有效稳定键和至少一种 REST 返回格式。创建、更新和恢复 `active` 状态必须使用同一消费契约校验；不满足当前契约的历史记录一次性迁移为 `error`，不得继续进入消费目录、通过列表时跳过坏记录或保留旧契约兼容分支。迁移后的服务只能在发布者补齐当前输出契约并重新发布后恢复为 `active`。

真实数据执行继续使用 Service owner 的协议端点。Workbench Backend 不增加通用 `/execute` 代理，不复制 Service 查询内核。

## 七、动态参数与查询

动态查询是第一阶段核心能力，不是延期项。

### 7.1 字段筛选参数

Query Service 当前结构化执行请求已经支持：

- `select`；
- `filter`；
- `order_by`；
- cursor `page`；
- `json | csv | geojson`；
- `eq | ne | lt | lte | gt | gte | in | is_null | is_not_null | bbox_intersects`。

Workbench 根据 Consumer Descriptor 中的字段类型、允许操作符和空间能力生成输入框、枚举、多选、范围、日期和地图框选控件。Workbench 只编译结构化请求，不拼接 SQL。

### 7.2 服务级命名参数

统计周期、基准日期、阈值或算法模式等参数可能不直接对应输出字段。它们必须由 Service 作为强类型 Input Contract 明确发布，并由 Service 负责绑定和校验。Workbench 只负责渲染控件和提交类型化值。

Outdoor 两人活动重叠度已经形成无法表达为输出字段筛选的真实用例：两个人员标识必须在 SQL 内部参与活动集合、交集和分母计算，不能先生成所有人员对再筛选。现行契约因此正式支持服务级命名参数：

- Query Service 的 SQL 模式在发布时声明 `named_parameters`，每项包含稳定名称、通用字段类型、必填性、说明和可选默认值；表模式不得声明命名参数；
- SQL 只使用 `:name` 引用标量值，参数定义与 SQL 引用必须完全一致；不支持关系、字段名、表名、排序表达式或 SQL 片段参数；
- 现有唯一 `QueryExecutionRequest` 增加 `parameters` 对象，不增加第二个端点或第二种执行协议；
- Service 在执行前拒绝缺失、额外和类型错误的参数，并通过共享查询运行层使用数据库绑定参数；禁止字符串替换；
- Consumer Descriptor 的 `input_contract.named_parameters` 是 Workbench 唯一可见的服务参数事实，并纳入 `contract_fingerprint`；
- Data Application Component 用现有 Parameter Definition 和 Parameter Binding 把 Application Parameter 映射到字段筛选参数或服务命名参数，不保存 SQL。

字段筛选与服务命名参数可以同时存在：前者只约束固定查询的输出字段，后者参与固定 SQL 内部计算。两者都编译进同一个结构化执行请求。

### 7.3 参数选项

参数候选值只能来自：

- Consumer Descriptor 中的固定枚举；
- Service 明确提供的有界选项查询；
- 另一个正式发布的数据服务；
- 用户手工输入。

Workbench 不对底层字段执行无界 `SELECT DISTINCT`，不直接查询来源表，也不把样例值当成完整候选集。

Outdoor 人员选择不增加“按 ID 查询昵称”的专用服务。人员指标 Query Service 直接输出 `person_id + person_nickname + 指标`，两个同源人员列表 Component 分别通过 Selection Binding 写入 `person_id_a` 和 `person_id_b`；重叠度 Component 将这两个 Application Parameter 绑定到服务命名参数。昵称用于展示，稳定人员 ID 用于计算。

### 7.4 保存与运行

Data Application Component 保存参数定义、查询模板和默认值。运行时输入默认只存在当前页面状态，不自动覆盖草稿；所有者显式保存后才更新聚合根版本，发布后才形成新的不可变 Revision。

### 7.5 Query Service Input Contract 候选结构

第一阶段不让 Workbench 根据字段类型猜测完整查询能力。Query Service 应明确投影每个字段的可选择、可筛选、可排序事实以及服务级限制：

```json
{
  "kind": "structured_query",
  "fields": [
    {
      "name": "activity_date",
      "value_type": "date",
      "nullable": false,
      "selectable": true,
      "filter_operators": ["eq", "lt", "lte", "gt", "gte", "in"],
      "sortable": true
    },
    {
      "name": "geometry",
      "value_type": "geometry",
      "nullable": true,
      "selectable": true,
      "filter_operators": ["bbox_intersects"],
      "sortable": false
    }
  ],
  "named_parameters": [
    {
      "name": "threshold",
      "type": "decimal",
      "required": false,
      "description": "最低阈值",
      "default": 0.5
    }
  ],
  "default_select": ["person_id", "activity_count"],
  "stable_order": ["person_id"],
  "filter_expression": {
    "logical_operators": ["and", "or", "not"],
    "max_depth": 16,
    "max_nodes": 256
  },
  "page": {
    "kind": "cursor",
    "default_limit": 100,
    "max_limit": 1000
  },
  "formats": ["json", "csv", "geojson"]
}
```

`value_type` 复用 ADDP 通用 `FieldType`，不再定义 Workbench 私有类型集。`filter_operators`、`sortable`、限制和格式必须与 Service 真实校验逻辑同源生成，不得由投影层手工维护一份可漂移清单。

`named_parameters` 只在服务真实声明命名参数时包含条目；没有命名参数时返回空数组，不作为未来能力占位或 Workbench 私有扩展点。

## 八、渲染体系

当前 renderer 固定为：

| Renderer | 开放条件 |
| --- | --- |
| `table` | 输出契约为表或等价字段集合 |
| `chart` | 输出包含可作为维度和指标的标量字段 |
| `map` | 输出契约声明空间能力和明确空间字段 |
| `value` | 输出为唯一完整行，且显式选择 1–4 个数值字段 |

renderer 只能消费输出契约，不根据业务名称猜测角色。第一阶段允许用户显式选择维度、指标、排序和空间字段，不自动生成业务解释。

### 8.1 `common-frontend` 与 Workbench 边界

仓库已经按重依赖把 `common-frontend` 拆分为 `basic / map / graph / dag` 等子包。Workbench renderer 必须沿用该边界，不新建同时捆绑 Element Plus、ECharts 和 OpenLayers 的大型 `analytics` 包。

| 能力 | 放置位置 | 组件 | 边界 |
| --- | --- | --- | --- |
| 表格结果 | `common-frontend/basic` | `TabularResultRenderer` | 渲染当前页字段和行并发出当前结果选择事件；cursor 仍由宿主维护 |
| 单值结果 | `common-frontend/basic` | `ScalarValueRenderer` | 显示服务返回的唯一行标量结果，不做求和、计数或口径推断 |
| 图表结果 | 新建 `common-frontend/chart` | `ChartRenderer` | 使用 ECharts 渲染已完整的有界表格结果 |
| 空间结果 | `common-frontend/map` | `GeoJSONResultRenderer` | 复用 `MapContainer`、OpenLayers、CRS registry 和底图 profile |
| 渲染编排 | `workbench/frontend` | `WorkbenchRendererHost` | 选择 renderer、适配 Service 结果、判断完整性并处理 cursor |

`common-frontend/chart` 使用技术栈规约的 `echarts@5.5.1` peer dependency，由消费模块自行声明依赖。地图继续使用 `ol@9.2.4`。不使用与平台技术栈不一致的版本作为 Workbench 基线。

共享 renderer primitive：

- 只接收已归一化数据、字段事实和展示配置；
- 不发起 HTTP 请求，不读取 Token，不识别 ServiceReference、Consumer Descriptor 或 Data Application；
- 通过事件返回排序、分页、选择、bbox 和地图视角等交互意图；
- 只使用 ADDP 主题变量和导出的双语 i18n 消息，不持久化业务状态。

现有 `TablePreview` 和 `GeoJsonPreview` 继续表示 Manager 式数据预览：它们消费预览响应结构并包含原始内容展示，不改名或扩张为 Workbench 结果 renderer。新增组件不保留另一个 Workbench 私有同功能实现。

Data Application Component 中的 `renderer_type` 是 `renderer_config` 的可辨识标签。Backend 必须使用 `table | chart | map | value` 四个具体 DTO 严格解码并拒绝未知字段，不使用无约束 `map[string]interface{}`。不为 renderer config 另增一个与 Data Application `version` 并行的版本字段。

### 8.2 Table Renderer Config

```json
{
  "columns": [
    {
      "field": "person_id",
      "label": "人员",
      "format": {"type": "auto"}
    },
    {
      "field": "activity_count",
      "label": "活动数",
      "format": {"type": "number", "precision": 0}
    }
  ]
}
```

- `columns` 同时决定可见字段和顺序，字段必须存在于当前输出契约；
- `label` 是 View 所有者定义的展示文本，不改变字段语义；
- 格式类型第一阶段只使用 `auto | number | percent | date | time | datetime | boolean | json`，并按 `FieldType` 限制可用组合；
- 排序属于 `query_template.order_by`，不在 renderer config 中重复保存；
- Table 可显示当前页并通过 Workbench 维护的 cursor 栈前后翻页，不在浏览器对跨页数据重排或聚合；
- 第一阶段不支持行编辑、分组、透视、合计行或计算列。

### 8.3 Chart Renderer Config

```json
{
  "chart_type": "bar",
  "dimension_field": "person_id",
  "measure_fields": [
    {
      "field": "activity_count",
      "label": "活动数",
      "format": {"type": "number", "precision": 0}
    }
  ]
}
```

- `chart_type` 第一阶段固定为 `bar | line | pie`；
- `bar` 使用一个标量维度和 1–5 个数值度量；
- `line` 使用一个可排序标量维度和 1–5 个数值度量，`query_template.order_by` 必须明确包含该维度；
- `pie` 使用一个标量维度和恰好一个数值度量；
- Pie 的度量值必须是有限且非负的数值，不对负数、`null` 或非数值做隐式绝对值、归零或过滤；
- Chart 不执行求和、分组、透视、补点、客户端排序或“其他”合并，服务结果必须已具有目标粒度；
- Chart 最多接收 500 行完整结果，Pie 最多 20 项。`page.has_more=true` 或超过上限时不渲染部分图表，要求用户缩小筛选或使用上游聚合服务。

### 8.4 Map Renderer Config

```json
{
  "geometry_field": "geometry",
  "label_field": "person_name",
  "tooltip_fields": ["person_id", "activity_count"],
  "base_map_profile_id": "osm",
  "style": {
    "mode": "continuous",
    "field": "activity_count",
    "palette": "primary",
    "legend_title": "活动数"
  }
}
```

- `geometry_field` 必须是 Descriptor 空间契约明确声明的 geometry 字段，不默认为 `geom` 或 `geometry`；
- `label_field` 可选，`tooltip_fields` 只能引用输出契约字段；
- `base_map_profile_id` 可省略；省略时使用平台当前默认 profile，显式值只能引用平台已批准底图 profile，不保存 URL、Key 或颜色值；
- `style.mode` 只允许 `uniform | categorical | continuous`；`categorical` 使用已选标量字段，`continuous` 使用已选数值字段；
- `style.palette` 只保存受控的 ADDP 主题色板标识，共享 renderer 在运行时解析主题变量；快照不保存原始颜色、函数或任意样式 DSL；
- 专题样式只对当前服务已返回的值进行分类或连续映射，不产生聚合、业务分级或指标口径；
- 已保存 profile 失效时显式报告不可用，不自动改用另一底图；
- 地图根据 Descriptor 的 SpatialInfo 和 CRS definition 进行展示转换，不把 GeoJSON 坐标无条件当作 WGS84；
- 第一阶段只消费 Query Service 的 GeoJSON FeatureCollection，最多 1000 个 Feature；`page.has_more=true` 时不把局部 Feature 当作完整地图；
- Tile Service、自定义分级阈值、热力图、聚合点、三维和任意样式 DSL 暂不进入当前阶段。

### 8.5 Value Renderer Config

```json
{
  "items": [
    {
      "field": "activity_count",
      "label": "活动数",
      "unit": "次",
      "precision": 0
    }
  ]
}
```

- `items` 只允许 1–4 项，字段必须是已选择的数值输出字段，同一字段不得重复；
- `label` 和 `unit` 是 Data Application owner 的显式展示配置，不改变服务字段语义；`precision` 只允许 0–8；
- Value 必须收到 `page.has_more=false` 且恰好一行结果；空结果、多行结果或非有限数值均显式拒绝渲染；
- Value 不在浏览器中求和、计数、取第一行或计算面积；服务必须返回已具有正确口径的唯一汇总行。

### 8.6 通用空间数据应用组合

空间数据应用不是新的领域类型，也不使 Workbench 获得农业、户外或其他领域语义。它只是通用 Component 的推荐组合：

```text
应用参数
  -> Value Component：已聚合的单行概览服务
  -> Map Component：有界空间要素服务
  -> Chart Component：已聚合到目标粒度的分布服务
  -> Table Component：明细服务
  -> Selection Binding：地图或图表的标量标识驱动明细查询
```

创作端可以提供只含布局和 Component 角色的通用空间应用模板，但必须由用户选择 ServiceReference、字段、单位、维度和联动映射。模板、renderer、Backend 校验和默认配置均不得包含验收数据的服务 ID、表名、字段名、业务口径、行政区划或配色。

第一版采用“空间探索创作向导”，它不是新的持久化 Template 实体，也不增加模板 API。向导只在空的数据应用草稿中工作，用户确认后一次性编译为现有 Data Application Snapshot，后续仍通过普通 Component 编辑器继续调整。它固定的只有四种通用角色和十二列布局：

1. Value：消费用户选择的汇总 Query Service，并通过必填应用参数把结果收敛为唯一完整行；
2. Chart：消费同一汇总服务的完整分布结果，用户显式选择维度和度量；
3. Map：消费用户选择的空间明细 Query Service，用户显式选择名称、提示、专题字段、样式模式与受控色板；
4. Table：消费同一空间明细服务，用户显式选择明细列；
5. 一个共享应用参数分别绑定 Value、Map 和 Table 的等值筛选；Chart 的维度字段通过 Selection Binding 回写该参数。

向导必须读取两个 Service Consumer Descriptor，并在生成前验证：汇总和明细筛选字段均声明 `eq`、筛选字段类型一致、Chart 维度与应用参数类型一致、Value/Chart 度量为数值字段、Map geometry 来自 `primary_geometry_field`、专题字段类型与样式模式一致。应用名称、参数标签、默认值以及每个 Value 项的精度均由用户输入；精度不根据字段名或当前样例值推断。组件标题可以使用 i18n 提供的通用角色名称，但服务引用、字段、单位、精度、默认参数、图例与色板必须在向导中可见并由用户确认。生成结果只保存现有 Snapshot 字段，不保存 `spatial_exploration` 等模板标识，也不形成运行时第二条路径。

已有组件时不允许套用向导或覆盖草稿；如需重新组合，用户应新建数据应用或先显式删除现有组件。向导不预览、不执行或发布服务，生成后仍由现有 Component 预览、草稿保存、发布和运行路径承担验证。

小规模空间结果继续使用 Query Service 的完整有界 GeoJSON。当业务要求超过 1000 个 Feature、多级别加载或地图视窗驱动加载时，必须先定义 Tile / OGC Features 的稳定 Consumer Descriptor 和受控 operation，不得让 Workbench 直连引擎或复用 Manager 预览接口。

### 8.7 结果完整性原则

Table 是 cursor 分页浏览器，可以明确显示当前页。Chart 和 Map 表达对一个结果集的整体解释，因此不得静默渲染第一页：

```text
page.has_more = false
∩ result_size <= renderer_limit
∩ contract_fingerprint 一致
= 允许 Chart / Map 渲染
```

后续 Graph、媒体、三维和大屏 renderer 必须在存在真实服务输出契约和内容读取能力后再增加。不建设空泛 renderer 注册框架、插件 DSL 或 Workbench 私有备用组件。

### 8.8 有限导出

第一阶段的有限导出是 Service 查询结果的一种有界输出格式，不是 Workbench 资源、后台任务或新的执行链路：

```text
Data Application Component 当前结构化查询
  -> 同一 Service 查询 operation
  -> format=csv | geojson
  -> 单次有界响应
```

固定规则如下：

1. Workbench 不新增 `/export` API，不在本地生成另一份服务调用协议，也不把导出提交给 Orchestrator；
2. Table 和 Chart 只导出 CSV，Map 只导出 GeoJSON；不提供 XLSX、PDF、图片、Shapefile 或其他格式；
3. 每次导出只发送一个 Service 请求，不自动追逐 cursor，不在浏览器或 Backend 拼接多页；
4. 请求 `limit` 不得超过 Consumer Descriptor 的 `page.max_limit`，并继续受当前 renderer limit 约束；Workbench 不重复保存 Service `max_features`；
5. 只有 `page.has_more=false` 才允许浏览器保存为完整导出文件；若仍有后续页，必须丢弃待保存内容并提示用户缩小筛选范围；
6. 导出不创建或修改 Data Application，不保存结果副本，也不改变 `contract_fingerprint`；
7. 导出与普通查询使用相同的 `service.data_read.execute` 和 Service Resource Grant / Policy，不增加第一阶段 `service.data_read.export`。

单独限制 CSV 或 GeoJSON 不能构成真实的数据防泄漏边界，因为拥有查询权限的用户仍可读取相同 JSON 数据并自行转换格式。只有未来的正式批量导出提供更高上限、自动翻页、异步文件生成、对象存储交付或其他新增数据能力时，才需要定义独立的 Service 导出 Permission；该能力归入 Transfer/任务执行体系，不扩张 Data Application。

导出审计由 Service owner 负责。Service 查询执行面必须区分普通查询和显式导出，成功与拒绝都记录结构化审计事件，至少包含：

- Principal、Tenant 和 Request ID；
- `service_type`、`service_id` 和服务版本；
- 输出格式、返回数量、`has_more` 和执行结果；
- 排除字面值的 `query_shape_fingerprint`；
- 稳定错误码或拒绝原因。

审计不得记录筛选字面值、参数值、返回行、空间要素、SQL、cursor、Token 或原始请求 Body。当前 Service 管理面审计中间件没有覆盖公开查询执行路由，实施 Consumer API 和执行权限时必须同步补齐 Service 执行面审计，不能由 Workbench 伪造 owner 审计事件。

普通查询与显式导出继续使用同一个 Query Service operation。客户端通过 Descriptor 声明的可选请求头 `X-ADDP-Query-Intent: query | export` 表达用途，缺省为 `query`；该头不授予额外能力，只用于 owner 审计事件分类。Gateway 共享 CORS 必须允许该请求头，并暴露 `X-ADDP-Has-More`、`X-ADDP-Next-Cursor`、`X-ADDP-Service-Version` 和 Request ID 等有界响应事实。CSV 与 GeoJSON 必须返回相同的分页和服务版本响应头。

### 8.9 保存前整页预览

保存前整页预览用于让创作者在不写入草稿、不发布 Revision 的前提下，以最终运行布局检查当前内存中的 Data Application Snapshot。它不是新的 Data Application 状态、页面类型或运行入口。

固定边界如下：

1. 编辑器打开预览时，先按保存载荷的归一化规则复制当前名称、说明和 Snapshot；预览只消费这份不可回写的内存副本；
2. 已发布运行页和保存前预览必须复用同一个 Workbench 应用运行画布、Component 查询状态、参数联动、renderer、cursor、导出、全屏和自动刷新实现；运行页只负责读取当前发布 Revision，编辑器只负责提供当前草稿副本；
3. 预览继续按 Component 保存的 `service_ref + contract_fingerprint` 读取 Consumer Descriptor，并由当前用户 Bearer 直接调用 Descriptor operation；契约变化和权限错误按正式运行规则阻断；
4. 预览使用覆盖创作端的全页对话层，不增加 `/preview` 路由、Backend API、临时数据库记录、未发布 Runtime API、查询代理或第二套认证；
5. 关闭预览时销毁全部参数值、查询结果、cursor、错误和定时器；预览中的输入、选择联动和刷新都不修改编辑器草稿；
6. 只有名称、页面标题和至少一个 Component 齐备且呈现区块约束通过时才允许打开；更深的 Descriptor 与查询错误由唯一运行画布按真实执行结果展示；
7. `desktop | wallboard` 只影响同一画布的布局。预览容器可以约束画布高度，但不能拥有专用 Component 配置或查询分支。

应用运行画布属于 `workbench/frontend`，因为它组合 Data Application Snapshot、Service Consumer Descriptor、Parameter Binding 与 Selection Binding；Table、Value、Chart、Map 等纯结果 renderer 继续由 `common-frontend` 唯一实现。不得为了预览在编辑器中复制一套 renderer 编排或查询逻辑。

## 九、权限与身份

### 9.1 用户交互

浏览器使用当前 User Bearer。有效数据访问至少是以下范围交集：

```text
当前用户 Permission
∩ Data Application owner_user_id 或有效资源授权
∩ Service Resource Grant / Policy
∩ 服务自身访问策略
```

Workbench 只保留 Data Application 权限；已经删除的 `workbench.view.*` 不再属于权限目录：

```text
workbench.data_application.create
workbench.data_application.read
workbench.data_application.update
workbench.data_application.delete
workbench.data_application.publish
workbench.data_application.execute
```

`read | update | delete | publish` 同时匹配当前 Tenant 与 `owner_user_id`；`offline` 复用 `publish`，不增加第二个生命周期 Permission。`execute` 接受 owner 或 Asset 履约形成的有效资源授权。Data Application Permission 只控制应用配置和运行入口，任何 Component 的真实查询仍由 Service 使用当前 User Bearer 执行最终授权。

| 操作 | Workbench Permission | owner 条件 | Service 校验 |
| --- | --- | --- | --- |
| 创建数据应用 | `workbench.data_application.create` | `owner_user_id` 由当前 User 生成 | 逐个 Component 读取 Descriptor 并校验 |
| 列表/读取数据应用 | `workbench.data_application.read` | 必须匹配当前 User | 不依赖 Service 可达 |
| 更新数据应用 | `workbench.data_application.update` | 必须匹配当前 User | 逐个 Component 重新读取 Descriptor 并校验 |
| 发布/下线数据应用 | `workbench.data_application.publish` | 必须匹配当前 User | 发布冻结已归一化草稿 |
| 删除未发布数据应用 | `workbench.data_application.delete` | 必须匹配当前 User | 不访问 Service |
| 运行数据应用 | `workbench.data_application.execute` | owner 或有效资源授权 | 每个 Service operation 再做最终判断 |
| 枚举可用服务 | `service.data_read.execute` | Service 资源策略 | Service Consumer Catalog 最终判断 |
| 执行查询 | `service.data_read.execute` | Service 资源策略 | Service operation 最终判断 |
| 有限导出 | `service.data_read.execute` | Service 资源策略 | 同一 Service operation、单次有界响应与执行面审计 |

第一阶段 Workbench 只支持已认证用户，不提供匿名 Workbench 运行模式。公开服务仍可在 Workbench 之外按 Service 公开协议匿名访问，两者不构成第二条 Workbench 身份路线。

运行时固定分两次独立判断：

1. 读取创作态 Data Application 时校验应用配置 Permission 和 `owner_user_id`；读取运行态 Revision 时校验 `workbench.data_application.execute`、owner 或有效资源授权；
2. 枚举 Descriptor 或执行每个 Component 查询时，由 Service 实时校验 `service.data_read.execute` 和服务资源策略。

已保存 Data Application 不因服务授权失效而删除，但相应 Component 查询必须阻断并显示重新申请或联系所有者的入口。

### 9.2 BFF 和服务身份

- 同步代表用户访问 owner 时，转发当前已验证的 User Bearer；
- 不代表用户的控制面读取才使用 `addp-workbench` Service Access Token；
- Workbench 不保存 User Token，不把 User/Tenant/Role 放入 Header、Query 或 Body 让 owner 信任；
- 浏览器不保存 System Application API Key；
- API Key 只表达外部应用身份以及 Gateway 公开接口的可选配额和审计语义，不替代私有 Service 所需的用户委托 OAuth、Role Permission 或 Resource Grant，也不作为 Workbench 内部主路径。

### 9.3 Asset 授权

Asset 负责申请、审批和履约，Service 或 Workbench 作为资源 owner 负责最终授权判断。接入 Portal 前必须与企业 CatalogEntry 与 `AssetComponent.catalog_entry_id` 的唯一来源链路一致，不能恢复通用 Owner ResourceRef、软授权、专属 Token 或 owner 实时查询 Asset ACL 的旧路线。

Phase 4B 的首条正式履约协议固定为：Asset 审批事务创建 `pending` Authorization；可恢复 reconciler 通过 Catalog 当前来源解析得到 Workbench Data Application UUID，再使用 `addp-asset` Tenant Service Access Token 幂等写入 Workbench `ResourceAccessRule`。Workbench 确认规则后 Authorization 进入 `effective`；撤销或过期先进入 `revocation_pending`，Workbench 确认撤销后进入 `revoked`。Portal 只有在 `effective` 时展示打开入口。

Workbench 规则使用 Asset Authorization ID 作为 `source_identity`，主体固定为申请 User，资源固定为 `data_application`，Permission 固定为 `workbench.data_application.execute`，effect 固定为 `allow`。重复履约或重试必须收敛到同一规则；请求载荷与既有规则不一致时必须冲突失败，不能覆盖为另一项授权。Application 发布状态为 `offline` 时，即使 Grant 仍在有效期内也不能运行。

## 十、契约变化

Data Application 的创作态和发布运行态每次加载 Component 时比较其冻结的 `contract_fingerprint` 与 Service 当前契约：

1. 指纹一致，正常运行；
2. 指纹变化，停止自动查询；
3. 显示字段、类型、参数、操作符和空间能力差异；
4. 由 Data Application 所有者显式修订草稿并发布新 Revision；
5. 不自动删除字段，不按名称猜测映射，不自动改绑同名服务。

普通数据内容刷新不能改变契约指纹。因此周期性重算并替换固定结果表时，Data Application 无需重新发布。

服务下线或删除时保留 Data Application Component 及原始 `service_ref`，显示不可用状态；不得自动切换到同名或相似服务。

## 十一、Asset 与 Portal 衔接

Data Application 草稿是个人创作资源，不作为 Asset；发布后的 Data Application 可以进入企业目录和资产链路：

```text
Workbench 发布 Data Application
  -> Catalog 为 Workbench Data Application 建立 CatalogEntry
  -> AssetComponent 引用 catalog_entry_id
  -> Asset 发布 application 类型资产
  -> Portal 展示、申请和打开
  -> Workbench 运行应用
  -> Service 最终校验数据访问
```

Asset 当前禁用的 `application` 类型后续应一次性收敛为 CatalogEntry 组合路线。不得新增 `workbench_application` 等平行资产类型，也不得恢复专业模块直接向 Asset 自动发现或写入资产草稿的旧路线。

`application` 类型本轮只允许一个 primary Component，且必须动态解析为 `workbench/data_application`；不接受 supporting Component、手工应用链接、iframe URL、API Key 或自定义 Token。未来若出现真正的多应用组合需求，应先定义新的聚合和运行语义，不能把多个 CatalogEntry 临时塞进同一 application Asset。

Service 已发布服务和 Workbench Data Application 都可以后续成为 CatalogEntry，但 Workbench 查询时仍使用 ServiceReference，不从 CatalogEntry 反向猜测执行地址。

Portal 只展示资产和打开入口，不保存 Workbench 页面、组件、参数或服务凭据。

CatalogEntry 标识 Data Application 聚合根，不标识单个发布 Revision。Portal 打开应用时由 Workbench 解析当前有效发布 Revision；已发布 Revision 不因新草稿变化而改变。

## 十二、阶段计划

### Phase 0：概念与专题确认

- [x] 确认模块名 `workbench`；
- [x] 确认平台级通用定位，不绑定 Outdoor；
- [x] 确认 Service-only 数据入口；
- [x] 确认 Data Application 是唯一持久化聚合根，Component 直接消费 Service；
- [x] 确认动态查询进入第一阶段；
- [x] 确认 BI 是轻量消费能力，大屏是后续应用展示模式；
- [x] 确认 Consumer Descriptor 稳定术语与 `addp.service_consumer/v1` 协议版本；
- [x] 确认 Service 只声明输入/输出契约，不声明 Workbench renderer；
- [x] 确认 Service 拥有 Consumer Catalog，且列表只返回当前可执行服务；
- [x] 确认 ServiceReference 使用 `service_type + 正整数 service_id` 和唯一详情 URL；
- [x] 确认 Query Service 同时支持输出字段筛选和 SQL 模式强类型标量命名参数，并由唯一执行请求承载；
- [x] 确认 Data Application 唯一 CRUD、发布、运行 API、owner 边界和权限矩阵；
- [x] 确认 renderer primitive 按 `common-frontend/basic | chart | map` 依赖边界共享，Workbench 只保留 Renderer Host；
- [x] 确认第一阶段 Chart 为 `bar | line | pie`，Map 只消费有界完整 GeoJSON；
- [x] 确认有限导出复用 Service 查询 operation、Descriptor 上限和既有执行权限，不新增 Workbench API 或导出 Permission；
- [x] 确认显式创建时 Data Application 成为独立聚合根，Component 持有配置快照且已发布 Revision 不可变；
- [x] 确认 Phase 2 使用 Console iframe 创作端，Phase 4 增加同 origin `/data-apps/:application_id` 顶层运行端；
- [x] 确认 Business MySQL `commerce-order-analysis` Query Service 为第二个异构验收场景；

### Phase 1：Service 消费契约与执行权限

- [x] 定义通用 Consumer Descriptor；
- [x] 实现 Query Service consumer projection；
- [x] 实现授权前置过滤的 Service Consumer Catalog 列表和详情；
- [x] 定义只覆盖公开消费契约的 `contract_fingerprint`；
- [x] 私有服务执行校验 Tenant Scope `service.data_read.execute`；
- [ ] 与 Service owner Resource Grant / Policy 收敛；
- [x] 为普通查询和显式导出补齐 Service 执行面结构化审计；
- [x] 同步 Swagger、浏览器 CORS 调用契约、单元测试和 Service PostgreSQL 门禁；
- [x] 删除任何需要 Workbench 读取管理 DTO 的旁路。

### Phase 2：Workbench 最小模块

- [x] 按新模块规范创建 Backend、Frontend、permission manifest 和数据库迁移；
- [x] 注册 `workbench` 模块、Frontend、Backend 和 `addp-workbench` OAuth Client；
- [x] 实现服务选择和 Consumer Descriptor 消费；
- [x] 实现动态参数、结构化筛选和 cursor 分页；
- [x] 实现 CSV 与 GeoJSON 单次有界导出及超限阻断；
- [x] 在 `common-frontend` 实现并测试 `TabularResultRenderer`、`ChartRenderer` 和 `GeoJSONResultRenderer`；
- [x] 实现 Workbench `RendererHost` 与 Query Service 结果适配；
- [x] 实现 Data Application CRUD、乐观并发、发布 Revision 和运行端；
- [x] 接入 Console iframe 唯一创作入口和 canonical navigation bridge；
- [x] 登记 Makefile、测试脚本、Swagger、构建矩阵、Frontend CI 和 PostgreSQL T2 门禁；
- [ ] 以 Outdoor 和 Business MySQL 真实服务完成 Online 验收（进入 Phase 3，不在 Phase 2 建立样例旁路）。

### Phase 3：跨领域真实验收

- [x] Outdoor 作为首个真实场景验证固定结果刷新、参数查询、双服务 Data Application 发布和最终运行页；
- [ ] 基于 Business MySQL `customers + orders` 发布固定只读 SQL 的 `commerce-order-analysis` Query Service；
- [ ] 验证 MySQL cursor、动态字段筛选、标量类型格式化、CSV 导出和无 SpatialInfo 时禁用 Map；
- [ ] 验证不同字段名、不同字段类型、无空间/有空间输出；
- [ ] 验证契约变化阻断和显式修订；
- [x] 验证 Workbench 代码和配置中不存在领域字段硬编码，并由前端契约门禁阻止 Outdoor、Business MySQL 验收事实进入生产实现。

Outdoor 验收通过不等于通用能力通过。Phase 3 至少需要第二个不同领域服务，避免样例偶然适配被误判为平台能力。

### Phase 4：Data Application、Catalog、Asset 与 Portal

Phase 4A 先完成 Workbench owner 内的独立闭环：

- [x] 实现 Data Application 聚合根、Component 配置快照、单页 desktop 布局；
- [x] 实现应用级参数与 Component 参数的显式绑定；
- [x] 实现草稿、不可变发布 Revision、下线和创建者稳定运行入口；
- [x] 增加同 origin `/data-apps/:application_id` 顶层运行端，不保留第二条 iframe 运行 URL；
- [x] 验证草稿修改不会改变已发布的 Data Application Revision；

Phase 4B 再接企业目录和资产授权主线：

- [x] 将 Workbench Data Application 作为专业资源接入 CatalogEntry；
- [x] 一次性启用并收敛 Asset `application` 类型的 CatalogEntry 组合路线；
- [x] 接入 owner Resource Grant、Asset 履约和 Portal 打开入口；
- [x] 删除旧的手工应用链接、软授权和专属 Token 设想；
- [x] 留存普通用户“Grant 生效前拒绝—生效后运行—撤销后拒绝”的可复核浏览器证据。

### Phase 5：BI 深化与大屏

- [x] 实现 5.4 节 Selection Binding、同页 Component 选择联动和受控下钻；
- [ ] 多页面布局；
- [x] 实现 5.5 节 `desktop | wallboard` 单页展示模式；
- [x] 实现浏览器会话级全屏；
- [ ] `mobile` 展示模式；
- [ ] 页面轮播；
- [x] 实现 5.6 节 wallboard 应用刷新策略；
- [x] 实现 5.7 节 wallboard 应用呈现区块；
- [x] 完成 5.9 节外部 BI 消费契约、认证路线与现状缺口核查；
- [x] 确定 Power Query / Power BI Desktop 为首个真实 Connector 验收载体及第一版范围；
- [x] 实现不依赖 BI 宿主的 Python Service Consumer SDK，并纳入现有 Python 发布门禁；
- [x] 以真实普通表与空间 Query Service 完成 Python 外部消费者只读运行验收；
- [ ] 外部 BI 消费服务的契约与接入指南；
- [x] 实现外部 OAuth Client 注册治理；
- [ ] 以真实 BI Connector 完成端到端验收；
- [x] 完成 5.8 节 Data Application 资产运营指标事实源与模块归属评估；
- [x] 在 Asset 自有事实范围内实现 `application` 类型及具体 Asset 的运营分组；

Workbench 不因为 Phase 5 增强而取得数据建模、SQL、指标定义或任务计算职责。

### Phase 6：场景化消费体验

- [x] 确认 Workbench 的产品价值是组合服务完成消费场景，不是复制 Manager 数据预览或 Service 执行预览；
- [x] 确认空间数据应用为通用 Component 组合，不是 Outdoor、农业或其他业务域类型；
- [x] 实现只消费唯一汇总行的 `value` renderer，不在浏览器计算指标；
- [x] 实现 Map 显式标签、受控专题样式和图例，不允许任意样式 DSL；
- [x] 完成通用性合同门禁，禁止 Workbench 生产代码和默认配置出现验收领域事实；
- [x] 使用一个空间 Query Service 和一个已聚合 Query Service 配置 Value + Map + Chart + Table 应用，完成参数与选择联动验收；
- [x] 实现只编译现有 Snapshot 概念的空间探索创作向导，并完成无持久副作用浏览器验收；
- [x] 实现保存前整页预览，并让创作端与已发布运行端复用唯一应用运行画布；
- [ ] 数据量超过有界 GeoJSON 上限前，先定义 Tile / OGC Features 的稳定消费契约，不增加无界 Query Service 旁路。

## 十三、第一阶段明确延期

- Graph、Tile、Registered Service 适配；
- 多服务 JOIN；
- 自由 SQL 和计算字段语言；
- 多页面 Data Application；
- 外部 API Key；
- 后台定时刷新；
- 订阅、告警和邮件报表；
- 多页拼接、异步文件生成和无界结果导出；
- AI 自动生成分析或图表；
- 另一套缓存、物化和 OLAP 引擎。

延期能力不得以隐藏路由、占位表、禁用字段或前端硬编码形式预留。

## 十四、测试与 CI 门禁

新模块实施必须在同一变更中覆盖：

- Workbench Backend 单元测试；
- Workbench PostgreSQL 标准测试入口，只使用允许的测试 database；
- Workbench Frontend 单元测试和生产构建；
- `common-frontend/basic | chart | map` renderer 单元测试，并至少构建 Workbench 与一个额外真实消费模块；
- Value 唯一行、数值字段、项数和精度上限校验；
- Map `uniform | categorical | continuous` 受控样式、字段类型、完整结果和图例测试；
- Workbench 生产源码与默认配置的验收领域事实禁止扫描；
- Consumer Descriptor 契约测试；
- Service 私有/公开访问矩阵；
- 参数类型、操作符和非法字段拒绝测试；
- cursor 分页和契约指纹测试；
- CSV/GeoJSON 有限导出、`has_more` 阻断和 Service 执行面审计测试；
- View `version` 成功更新和 `409` 冲突测试；
- Console 路由和模块动态恢复测试；
- Data Application 顶层运行路由、Browser AuthSession 和 Portal 打开链路测试；
- Swagger 生成与路由覆盖；
- Permission Manifest 覆盖；
- 根 Makefile、CI workflow 和 Online suite 自动登记；
- Outdoor 与 Business MySQL `commerce-order-analysis` 的真实端到端验收。

新增模块不能只增加源码目录。若现有自动发现或 CI 注册不能命中，必须同步补充注册检查和门禁。

### 14.1 Phase 2 验证记录（2026-08-26）

以下标准入口已通过：

```bash
cd workbench/backend && go test ./...
make test-workbench-frontend
WORKBENCH_POSTGRES_TEST_DSN='postgres://.../addp_test?sslmode=disable' make test-workbench-postgres
ADDP_SYSTEM_POSTGRES_TEST_DSN='postgres://.../addp_iam_test?sslmode=disable' \
  bash scripts/test/system-iam-postgres-gate.sh --package migration --test workbench-runtime
make test-console-frontend
bash scripts/swagger/check-route-coverage.sh workbench
python3 scripts/ci/check-build-registration.py --repository .
python3 scripts/ci/check-frontend-ci-registration.py --repository .
python3 scripts/ci/check-t2-ci-registration.py --repository .
make ports-validate
```

Workbench 前端门禁同时运行 `basic | chart | map` 三组共享 renderer 测试和 production build。真实 Outdoor、Business MySQL、有限导出执行审计以及第二个消费模块的 Online 证据仍属于 Phase 3，不能用单元测试替代或提前勾选。

### 14.2 Phase 3 MySQL Online 实现状态（2026-08-26）

已实现 `workbench-service-consumption` T4 suite 及专用 `business/scripts/online-workbench-mysql-fixture.sh`：Fixture 不读取或生成 `business/.env`，使用仓库外变量启动确定性 Business MySQL，并为永久 Engine Instance 准备仅有 `SELECT` 的账号。suite 经 Gateway 走 Service 输出契约检测、临时 Query Service 发布、Consumer Descriptor、含 Table 与 Chart 两个 Component 的未发布 Data Application 创建、真实 cursor/筛选/CSV 查询及契约指纹变化主路径，退出时按本轮 ID 删除未发布 Data Application 和 Service。

该 suite 同时通过 Console 的真实登录打开 Data Application 创作页，并核对浏览器 AuthContext 与 API User 为同一身份。浏览器依次打开两个正式 Component 编辑器，验证 Table 返回两行、Chart 生成 canvas、非空间契约不出现 Map；随后更新 Query Service 公开字段策略并刷新同一应用，Component 编辑器必须出现契约变化告警且预览查询被禁用。浏览器只消费正式 API 和页面，不增加 Workbench 私有执行路由、已发布应用强删能力或测试旁路。

MySQL 协议把 `BOOLEAN` 报告为 `TINYINT`，仅凭 SQL 结果元数据无法区分业务布尔值和普通小整数。该场景因此在检测契约中显式把唯一的 `active_customer` 字段发布为 `bool`；Service 执行层按冻结输出契约把引擎返回的 `0 | 1` 归一化为 JSON/CSV 布尔值，其他 `TINYINT` 字段仍保持整数，不全局猜测为布尔类型。

对应 Host Gate profile、专用 Chromium 安装、手工 workflow choice、根 `make test-online-runner` 和 CI 登记一致性检查已同步。该状态只表示 T4 实现就绪；在专用 Runner 首次真实通过前，Phase 3 的 Business MySQL 验收、执行审计证据及整体验收项继续保持未勾选。

### 14.3 创建与编辑链路回归（2026-08-27）

浏览器进入 `/workbench/views/new` 时曾因历史 Query Service 处于 `active + REST enabled`、但缺少当前输出字段或稳定键而导致整个 Service Consumer Catalog 返回 500。根因已收敛到 Service 发布状态不变量：创建、更新、恢复 `active`、版本化数据迁移和 Descriptor 读取共用同一消费契约校验；不合规历史记录一次性置为 `error`，有效服务继续参与正确的 count 和分页。Service 500 同时记录不含数据内容的结构化内部错误，客户端仍只接收稳定 i18n 错误。

Workbench 前端同步修正 API 客户端与页面对 Axios 响应形态理解不一致、编辑页对全量消费目录的不必要依赖、切换服务遗留旧参数、图表与地图字段变化未同步查询字段、可选布尔参数缺少“未设置”状态、数值参数接受任意文本，以及无默认值的复合控件恢复类型错误。Service 直接查询执行器按 Query Service 所属租户读取 Engine，不再丢失租户上下文。Console 与 Workbench 中文模块名统一由 i18n 显示为“工作台”，英文仍为 `Workbench`。

本轮已通过：

```bash
cd service/backend && go test ./...
SERVICE_POSTGRES_TEST_DSN='postgres://.../addp_test?sslmode=disable' make test-service-postgres
cd workbench/backend && go test ./...
make test-workbench-frontend
WORKBENCH_POSTGRES_TEST_DSN='postgres://.../addp_test?sslmode=disable' make test-workbench-postgres
make test-console-frontend
```

Service 按正式入口重启后，已在 Console iframe 主路径完成浏览器回归：消费目录只返回有效服务；`f2` 与 `p3` Descriptor 可加载；表格查询返回真实数据；cursor 可前进到第 2 页并返回第 1 页；创建 `workbench-e2e-20260827` 后列表立即可见；编辑页在不枚举全量目录的情况下正确回显并可再次查询；结果仍有后续页时 Chart、Map 和有限导出均拒绝把第一页当作完整结果；中文模块名、英文 `Workbench` 及页面文本可随 Console 语言切换。该临时 View 已在验收后通过页面删除并复核列表归零。

当前本地两项有效 Query Service 都明确发布了空 `filterable_fields`，浏览器不得伪造动态筛选能力。Workbench 现显示“当前服务未开放可筛选字段”并禁用添加操作；有筛选契约时的参数字段、操作符、类型化值和可选布尔未设置状态继续由前端契约测试及 Phase 3 的真实在线 suite 覆盖，不能用修改现有服务数据的方式制造手工验收条件。

### 14.4 当前接力状态（2026-08-27）

当前 Phase 2 最小模块已经可用，尚未发现阻断创建、查询或保存主链路的问题。最后一次浏览器完整刷新后，编辑页正常加载且没有新增 `error` 或 `warn` 日志。当前运行环境已经重启并加载本节所述 Service 租户上下文修复，Workbench 前端修改通过 Vite 开发服务生效。

为便于人工查看，当前保留一个由用户明确要求创建的非临时示例 View，不应按自动化测试数据清理：

| 项目 | 当前值 |
| --- | --- |
| View 名称 | `workbench-p3-demo` |
| View ID | `1500a6c7-feb8-4ac9-80c0-1978dc27ef77` |
| 入口 | `/workbench/views/1500a6c7-feb8-4ac9-80c0-1978dc27ef77` |
| Service | Query Service `p3` |
| Renderer | Table |
| 实际验证 | 首次查询返回 50 行，`page.has_more=true`，可继续下一页 |

自动化浏览器回归创建的 `workbench-e2e-20260827` 已删除；以后由开发代理创建且明确标识为临时测试数据的 View 可以在验证结束后直接清理，不需要再次询问，但不得据此删除上述人工示例或其他用户数据。

接力会话应先阅读本专题，再检查共享工作树，保留其他会话和用户已有改动。当前 Workbench 专题实现尚未提交，主要变更分布在 `workbench/`、`service/backend/internal/{api,models,repository,service}`、`common-frontend/{basic,chart,map}`、`console/frontend`、根测试与 CI 登记以及本专题文档；不得用 reset、checkout 或清理未跟踪文件的方式“恢复干净”。

下一步按以下边界推进：

1. `make test-online-runner` 已在当前工作树通过 121 项确定性测试，证明 Online 分发、预检、Host Gate、Fixture 安全边界、失败清理和 Workbench suite 协议可执行；这不等于真实 T4 通过。若目标是完成 Phase 3 验收，仍须在已登记的专用 Online Runner 执行 `workbench-service-consumption` T4 suite，取得 Business MySQL 的真实动态参数、Chart canvas、CSV、契约变化阻断和执行审计证据；成功前不勾选其余 Phase 3 验收项。
2. 若只在当前本地环境继续人工体验，`f2` 和 `p3` 均没有发布筛选字段且结果超过单页上限，只能验证 Table 与 cursor；Chart、Map 和有限导出被完整性规则阻断是正确行为。需要动态参数或完整 Chart/Map 时，应通过 Service 正式发布一个有 `filterable_fields`、可收敛为完整有界结果的服务，不能直接改数据库制造契约。

### 14.5 Phase 4A 实现与接力状态（2026-08-27）

Phase 4A 已按“创建者私有闭环”实现，未提前进入 Catalog、Asset 或 Portal 授权路线：

- Workbench 新增独立 `DataApplication` 聚合根和 `DataApplicationRevision` 不可变修订；应用从一个或多个当前 owner 的 View 创建时复制 Component、ServiceReference、契约指纹、查询模板、renderer 和默认参数，数据库、API 与运行时均不保存 `source_view_ids`；
- 草稿使用正整数 `version` 做乐观并发，发布事务写入新 Revision 并切换当前修订，下线只从 `published` 切换为 `offline`；重复下线不递增版本，已产生 Revision 的应用不能删除；
- Phase 4A 固定为一个 desktop 页面、十二列栅格和最多 24 个 Component；应用参数通过显式 Binding 映射到每个 Component 参数，运行时不按字段名猜测绑定；
- 创建者运行端固定为同 origin `/data-apps/:application_id`，不显示 Console 管理导航，不存在第二条 iframe 运行 URL；运行端读取当前不可变 Revision，重新读取每个 Service Consumer Descriptor、校验契约指纹，再使用当前 User Bearer 直接执行 Service；
- 运行端复用 `common-frontend/basic | chart | map` renderer；Table 保留正式 cursor 前后翻页，Chart 和 Map 继续拒绝把 `has_more=true` 的第一页当作完整结果；
- Workbench 权限清单新增 `workbench.data_application.create | delete | execute | publish | read | update`；System migration `000104_iam_workbench_data_application` 将其纳入内置租户角色并刷新授权版本。应用配置权限不替代底层 Service 的最终数据授权；
- Console 搜索和模块导航使用中英文 i18n；开发代理和 nginx 均将 `/data-apps/` 唯一转发到 Workbench frontend。Workbench Vite 统一使用 `/workbench/` base，修复顶层运行端曾错误加载 Console `/src/main.js` 的问题。

本轮最新代码已通过：

```bash
cd workbench/backend && go test ./...
make test-workbench-frontend
make test-console-frontend
make test-authorization
WORKBENCH_POSTGRES_TEST_DSN='postgres://.../addp_test?sslmode=disable' make test-workbench-postgres
ADDP_SYSTEM_POSTGRES_TEST_DSN='postgres://.../addp_iam_test?sslmode=disable' \
  bash scripts/test/system-iam-postgres-gate.sh --package migration --test workbench-data-application
python3 scripts/ci/check-build-registration.py --repository .
python3 scripts/ci/check-frontend-ci-registration.py --repository .
python3 scripts/ci/check-t2-ci-registration.py --repository .
```

Workbench 与 System 重启后，Phase 4A 已完成创建者真实浏览器闭环。为便于人工查看，保留下列正式示例，不得作为临时数据删除或下线：

| 项目 | 当前值 |
| --- | --- |
| Data Application 名称 | `workbench-p4-demo` |
| Data Application ID | `1714dcf7-f34e-4996-a8dc-3b88998ebe55` |
| 创作入口 | `/workbench/applications/1714dcf7-f34e-4996-a8dc-3b88998ebe55` |
| 稳定运行入口 | `/data-apps/1714dcf7-f34e-4996-a8dc-3b88998ebe55` |
| 来源 View | `workbench-p3-demo` |
| ServiceReference | Query Service `21` |
| 当前状态 | `published` |
| 当前 Revision | `2` |

真实浏览器证据包括：创作端从现有 View 创建独立草稿并保存；首次发布产生 Revision 1；顶层运行端使用当前 AuthSession 读取不可变快照并执行真实 Service 查询，Table 返回 50 行且 `has_more=true`；cursor 前进到第 2 页后首行变化，返回第 1 页后首行恢复；保存 Revision 2 草稿后运行端仍显示 Revision 1 与旧说明；发布 Revision 2 后运行端切换到新说明。数据库复核当前聚合版本为 6、当前修订为 2，且 Revision 1、2 的说明分别保持原值。整个最终链路没有新增 console `error` 或 `warn`。

浏览器回归同时暴露并修复三项此前单元测试未覆盖的边界：

1. 没有动态参数时，Go nil slice 曾把 `parameters` 和 `parameter_bindings` 编码为 `null`，使编辑器读取 `.length` 崩溃；后端现统一把快照集合规范化为 JSON 数组，并加入无参数 View 回归测试；
2. 编辑器曾对 Vue reactive Proxy 直接调用 `structuredClone`，点击保存会在发出 `PUT` 前抛 `DataCloneError`；现以 JSON 契约纯函数复制和裁剪快照，并使用 Proxy 输入测试覆盖；
3. 发布或下线确认框取消时，Element Plus 的正常 Promise rejection 曾形成未处理 Vue 错误；现只把 `cancel | close` 解释为无操作，真正异常继续抛出，并加入取消行为测试。

下线与重复下线冲突已由后端单元测试和 PostgreSQL 门禁覆盖。为保留可查看的唯一正式示例，本轮在浏览器确认框中主动取消下线，没有把“浏览器下线后运行阻断”记作手工通过。当时 Phase 4B 尚未实现；其后续实现与验收状态统一见 14.8、14.9，不应在 Phase 4A 代码中增加软授权、API Key 或专属 Token。

本轮重启 Workbench Backend 时，整套环境的生命周期锁由既有 `keepalive restart -all` 持有，不能走会停止整套环境的局部 `restart.sh`。按用户授权，先使用 `scripts/dev/build-identity.sh` 的同一原子构建函数生成最新二进制，再只替换 8193 的精确 Workbench 进程；后续既有整套生命周期会继续管理 `.dev-pids/workbench.pid`，不应在锁仍被持有时手工并行启动第二个 Workbench Backend。交付检查以“PID 文件与 8193 监听进程一致、原子构建 fingerprint 为最新、`/health/ready` 成功”为准，不依赖临时 launchd 标签。
3. Phase 1 的 Service owner Resource Grant / Policy 不能在 Workbench 专题内局部补齐。当前正式权限文档确认 owner Resource Grant / Policy、Scope Binding 和 Explicit Deny 的统一运行时尚未形成，企业目录主路径又明确禁止恢复旧的通用 Owner ResourceRef/ACL；因此 Service 继续使用 6.4 节规定的 fail-closed 租户级策略，待统一 owner 授权事实模型落地后接入同一 Repository 过滤与详情判断入口。该外部前置条件未满足前保持未勾选，不得在 Service 增加专属授权表或第二套 ACL。
4. 当前最小充分门禁为 `go test ./...`（Service、Workbench Backend）、`make test-workbench-frontend`、对应 PostgreSQL 标准门禁和 `git diff --check`；涉及 Console、共享 renderer、CI 或 Swagger 时必须同步运行本专题十四章列出的扩展门禁。

### 14.6 Phase 3 首次真实 T4 调度状态（2026-08-27）

远端 `main` 提交 `754492c8958055efd28378833c19414a5f2a5236` 已包含本专题当前实现，并通过手工 `workflow_dispatch` 触发 [Online T4 run 33036113139](https://github.com/pampa0629/addp/actions/runs/33036113139)，输入为 `workbench-service-consumption`。GitHub 已正确创建 Job `98399046724`，名称为 `Online T4 (workbench-service-consumption)`；截至本节记录时，Workflow 与 Job 均持续为 `queued`，没有任何 step 开始执行，因此不能作为 Host Gate、Business MySQL、浏览器 E2E 或清理通过的证据。

当前阻塞与统一 Online 验收专题记录的部署前置条件一致：具有 `self-hosted`、`macOS`、`addp-online` 三个标签的专用 Runner 尚未接单。当前个人开发 Mac 未发现 Actions Runner 进程、launchd 服务或 Runner 安装目录，并且个人 checkout 存在根 `.env`，不符合 Host Gate 的独立部署边界。不得删除 `addp-online` 标签、改用 GitHub-hosted Runner、放宽根 `.env`/独立数据库/仓库外 Secret 约束，或在旧运行仍排队时重复触发。

接力动作固定为：先在仓库 `Settings → Actions → Runners` 恢复或准备符合规范的专用 macOS Runner；现有 queued 运行会在 Runner 上线后自动接单。运行结束后必须核对 `readiness.txt`、`summary.txt`、`online-report.json`、`workbench-service-consumption-browser.json`、Service 执行审计证据、应用日志及 `cleanup=passed`，再决定是否勾选 Phase 3 的 Business MySQL 验收项。

### 14.7 Phase 4A 参数联动人工验收（2026-08-27）

Phase 4A 的单组件、无参数发布闭环已经通过，但尚未用真实服务证明“一个应用参数同时驱动多个 Component”。本地人工验收固定使用正式发布链路补齐该证据，不修改现有 Query Service `f2`、`p3`，不直接更新数据库契约，也不在 Workbench 生产代码中加入 Outdoor 字段判断。

验收资源与拓扑固定如下：

| 层级 | 设计 |
| --- | --- |
| Query Service | 基于 `f2` 已使用的正式 Meta table locator 独立发布 `workbench_parameter_demo`，由 Service 重新冻结来源快照；默认字段为 `SmID`、`NAME`、`City`、`SHAPE_Area`，只开放 `City` 作为 `filterable_fields`，稳定键继续由源表主键 `SmID` 生成 |
| Table View | `workbench-parameter-table`，声明 `city` 文本参数并以 `eq` 显式绑定 Query Service 字段 `City`，展示城市内的地块明细 |
| Chart View | `workbench-parameter-chart`，使用同一服务和同一字段筛选参数，以 `NAME` 为维度、`SHAPE_Area` 为数值展示城市内分布 |
| Data Application | 草稿名 `workbench-parameter-linkage-demo`，从上述两个 View 复制为两个独立 Component；只保留一个应用级 `city` 参数，并将其显式绑定到两个 Component 的 `city` 参数 |

选择 `City` 是验收数据事实，不是产品模型：当前正式来源共 130 行，各城市约 7–12 行，输入不同城市后两个组件都能在单页内得到完整且肉眼可区分的结果，Chart 不会因 `page.has_more=true` 被完整性规则阻断。该场景只证明真实动态参数、显式共享绑定、双组件渲染和 cursor/完整性边界；Workbench 的领域无关性仍由生产代码扫描、Consumer Descriptor 契约测试和待执行的 Business MySQL T4 共同证明。

人工验收顺序固定为：先分别运行两个 View，验证参数值变化会改变真实结果；再创建 Data Application 草稿，把两个 Component 映射到唯一应用参数；保存后验证一次输入触发两次正式 Service 查询、两组件使用同一类型化参数且结果同步变化；再次保存以验证草稿乐观并发；发布前确认示例名称与是否长期保留；发布后验证不可变 Revision 运行端。任何失败都回到 Service Descriptor、Workbench 编译或应用 Binding 的单一主路径修复，不增加旁路请求或测试专用运行端。

当前已完成发布与运行闭环：

| 资源 | 当前事实 |
| --- | --- |
| Query Service | `workbench_parameter_demo`，ID `23`，私有、`active`；`filterable_fields=["City"]`，Meta `serve` observation 的 `capture_method=declared`，当前依赖投影为 `active` |
| Table View | `workbench-parameter-table`，ID `ca2b8443-693c-42bd-94e2-66b0bc681228`；`长沙市` 返回 10 行，改为 `衡阳市` 返回 12 行且不再出现长沙结果 |
| Chart View | `workbench-parameter-chart`，ID `c34ba67a-6f17-4859-b9a1-3801fab73368`；柱状图使用 `NAME` 维度和 `SHAPE_Area` 度量，真实查询后生成 ECharts canvas，浏览器无新增 error / warn |
| Data Application | `workbench-parameter-linkage-demo`，ID `d6c30859-15c8-4b88-964b-f2dd315fb923`；`publication_status=published`、`version=4`、当前 Revision `1`；页面标题为“城市地块联动分析”，已确认长期保留 |

草稿快照当前只有一个应用参数，内部稳定 key 为 `component_1.city`、标签为“城市（共享）”、默认值为“长沙市”；Table 与 Chart 的两个 Component 参数都通过独立 Binding 显式指向该 key。第二次保存已验证乐观版本从 2 增至 3，未使用的 `component_2.city` 已被规范化逻辑裁剪。内部 key 不作为用户可见领域词汇，也不要求与 Service 字段同名；运行时只根据 Binding 编译两个组件请求。

本轮同时发现并修复 Query Service 发布链路缺陷。正式血缘规范和 PostgreSQL migration 只允许 `declared | runtime | parsed`，但 Meta 服务发布实现曾写入未定义的 `published`；SQLite 测试夹具又遗漏生产 CHECK，导致测试假绿。Meta 现将 owner 提供的服务发布事实记录为 `declared`，测试夹具同步生产枚举并断言观测值。Service 创建在同步血缘发布失败后曾保留已提交主记录，使页面显示失败但服务名已占用；现失败时精确删除本次新建记录，并以模拟 Meta 503 的单元测试证明不会遗留服务。首次失败产生的 ID `22` 已通过 Service 正式删除入口清理，重新发布 ID `23` 后血缘 observation 与 projection 均已形成。

Chart 首次加载失败不是 renderer 代码缺陷：运行中的 Workbench Vite 启动于 14:49，另一个并行过程在 15:10–15:13 改写 `node_modules` 并移除 `.vite/deps`，导致组件源码返回 200、四个 ECharts 预构建依赖却返回 504。只重启 5190 Workbench frontend 后依赖重新预构建，同一配置立即生成 canvas。该环境事实不引入产品兼容路径；并行会话修改依赖后必须重启对应 Vite 服务。

用户已确认继续使用 `workbench-parameter-linkage-demo` 并长期保留。2026-08-27 15:38 发布 Revision `1`，应用与修订记录的内容哈希一致，均为 `sha256:882e14502276769e053eb332aa01f2d757ee1d54fe3ae95cfc43197c4e6e7594`。运行端固定读取该不可变修订：默认“长沙市”执行“查询全部组件”后，Table 返回 10 行且 Chart 生成 1 个 ECharts canvas；将唯一共享输入改为“衡阳市”后再次查询，Table 返回 12 行、结果不再包含长沙市，Chart 继续生成 1 个 canvas。两轮查询期间浏览器没有新增 error / warn；运行查询后数据库仍只有 Revision `1`，当前修订号与内容哈希均未变化。运行入口为 `/data-apps/d6c30859-15c8-4b88-964b-f2dd315fb923`。

### 14.8 Phase 4B CatalogEntry 第一段实现与接力状态（2026-08-27）

Phase 4B 先按正式企业目录主路径完成 Workbench owner 到 Catalog 的单向收录，不提前启用 Asset `application` 类型，也不把 Catalog 可见性解释为应用执行授权：

- `DataApplication` 仍是 Workbench 聚合根，`CatalogEntry` 标识应用而不是 Revision；首次发布产生第一条 append-only 目录变化，后续重新发布与下线继续更新同一个来源绑定，不创建平行目录项；
- Workbench 新增 `workbench.catalog_resource_changes/v1` 变化源与批量动态解析接口，只向 `addp-catalog` 服务身份开放，并要求 tenant-only、不可委托、不可租户自定义的 `workbench.catalog.read`；
- 私有草稿创建与编辑不产生目录变化；现有已发布应用由一次性 `workbench.data_migrations` 回填，之后由应用聚合状态触发器记录发布 Revision 的最小摘要；普通生命周期没有物理删除路径，因此本版变化操作只允许 `upsert`，下线仍保持可发现身份并在摘要中显示 `offline`；
- Catalog 新增唯一类型 `data_application` 与来源对 `workbench / data_application / canonical UUID`，复用专业资源 checkpoint、重放、幂等 upsert、动态来源解析和租户边界；Catalog 前端列表、详情、筛选与 canonical route 已加入中英文类型名称；
- System migration `000105_iam_workbench_catalog_read` 物化新权限、授予 `tenant.catalog_runtime` 并刷新已分配该角色的 principal 授权版本。Docker Catalog 配置增加唯一 `WORKBENCH_URL`，没有增加第二条路由、API Key 或 Data Application 专属注册表。

本段已通过：

```bash
cd common && go test ./client ./authorization/...
cd workbench/backend && go test ./...
cd catalog/backend && go test ./...
make test-authorization
make test-catalog-frontend
make test-workbench-frontend
node --test common-frontend/basic/tests/taskOwnerUrl.test.mjs
WORKBENCH_POSTGRES_TEST_DSN='postgres://.../addp_test?sslmode=disable' make test-workbench-postgres
CATALOG_POSTGRES_TEST_DSN='postgres://.../addp_test?sslmode=disable' make test-catalog-postgres
ADDP_SYSTEM_POSTGRES_TEST_DSN='postgres://.../addp_iam_test?sslmode=disable' \
  bash scripts/test/system-iam-postgres-gate.sh --package migration --test workbench-catalog-read
```

全量重启后的运行态验收已经完成。System migration 为 `105,false`，权限表存在唯一 `workbench.catalog.read`；Workbench 一次性回填两条变化，Catalog checkpoint 为 `Mg`，并形成两个唯一来源绑定：`workbench-p4-demo` 对应 CatalogEntry `4ff43d3a-3815-49fc-80e7-831dd7cc92b8`、来源版本 `00000000000000000001`，`workbench-parameter-linkage-demo` 对应 CatalogEntry `f0359420-a884-4e11-a87f-bd60e37a65e2`、来源版本 `00000000000000000002`。首次 Catalog 同步发生在 Workbench 监听前一秒，记录了一次连接拒绝；下一个 30 秒同步周期按既有 runner 自动恢复，不是认证或 API 错误，也不需要新增兼容重试路线。

真实浏览器在企业资源盘点的 `data_application` 筛选下显示两条应用，中文类型为“数据应用”、英文为 `Data Application`；详情动态解析状态为当前，显示 Workbench 专业状态、目录变化版本、发布状态和 Revision Number。浏览器同时暴露并修复两项此前测试未覆盖的共享前端边界：

1. Catalog 详情页的专业 owner 白名单漏掉 Workbench，导致后端已返回的 `source_resolution` 和 owner detail 被隐藏；现由受测试的统一 owner 映射识别 Model、Standard、Service、Develop 和 Workbench，并显示发布状态与 Revision Number；
2. owner detail 原通过 Console SPA bridge 打开，但 `/data-apps/:application_id` 是 Workbench 顶层运行端；现统一渲染为 Console origin 的 `_top` 链接。进一步发现共享 `resolveConsoleRouteUrl` 只识别 `5173–5187`，漏掉 Inference `5188`、Catalog `5189` 和 Workbench `5190`；`common-frontend` 现按端口规范覆盖完整 `5173–5190` 模块开发端口，并增加共享测试。最终点击已真实到达 `http://localhost:5170/data-apps/1714dcf7-f34e-4996-a8dc-3b88998ebe55`，加载 Revision 2 运行端。

本段完成时尚未实现 Asset 履约，随后已按正式 owner-local Resource Grant 模型完成，当前状态统一见 14.9。Catalog 可见性始终不构成应用执行授权，Workbench 也不会在线查询 Asset ACL。

### 14.9 Phase 4B Asset 履约与 Portal 消费入口实现状态（2026-08-27）

Phase 4B 已按单一路线完成代码实现，不沿用 Asset 旧软授权字段，也不引入 API Key、专属 Token、Workbench 私有分享表或 owner 在线查询 Asset ACL：

- Asset 内置唯一 `application` 类型，资产必须且只能包含一个主组件；该组件必须解析到 `entry_type=data_application`、`source_module=workbench`、`source_type=data_application` 和 canonical UUID 来源身份。创建、修改、发布和批量发布均执行同一组合校验；
- Asset `Authorization` 生命周期收敛为 `pending -> effective -> revocation_pending -> revoked`。审批事务只创建 `pending` 事实；可恢复 reconciler 使用 `FOR UPDATE SKIP LOCKED` 领取任务、指数退避重试，并通过 Catalog 当前来源动态解析目标，只有 owner 确认后才推进状态；
- Workbench 保存 owner-local `ResourceAccessRule`，以 Asset Authorization ID 作为唯一 `source_identity`。履约 PUT 幂等，相同身份但不同载荷返回冲突；撤销 DELETE 写入 tombstone，重复撤销收敛；运行时只允许应用创建者或当前有效 Grant 对应的 User 执行，应用 `offline` 仍然阻断；
- `addp-asset` 获得不可委托、不可租户自定义的 `workbench.resource_grant.fulfill` 与 `workbench.resource_grant.revoke`。System migration `000107_iam_workbench_resource_grant` 物化权限并刷新内置角色授权版本；
- Portal 与 Asset 管理端不再推断 `is_active`。消费状态统一返回 `none | pending | fulfilling | effective | revoking`，仅 `effective` 返回 `/data-apps/{application_id}` 打开路径；Portal 使用共享 Console route 能力直接打开 Workbench 顶层运行端；
- Asset 数据库迁移删除旧 Authorization 软授权数据和旧字段，随后建立目标、生命周期和重试约束，不保留双轨兼容路径。新增标准 `asset-postgres` 门禁并注册到根 Makefile、T2 workflow、本地 macOS CI 与基础设施数据库清单；Workbench PostgreSQL 门禁覆盖 owner Grant 的履约、冲突、撤销、过期和非所有者运行边界。

当前已通过的最小充分门禁：

```bash
cd workbench/backend && go test ./...
cd asset/backend && go test ./...
cd portal/backend && go test ./...
cd common && go test ./client
make test-asset-frontend
make test-portal-frontend
make test-authorization
ASSET_POSTGRES_TEST_DSN='postgres://.../addp_test?sslmode=disable' make test-asset-postgres
WORKBENCH_POSTGRES_TEST_DSN='postgres://.../addp_test?sslmode=disable' make test-workbench-postgres
ADDP_SYSTEM_POSTGRES_TEST_DSN='postgres://.../addp_iam_test?sslmode=disable' \
  bash scripts/test/system-iam-postgres-gate.sh --package migration --test workbench-resource-grant
python3 scripts/ci/check-t2-ci-registration.py
```

Workbench、Asset、Portal Swagger 已重新生成并通过各自路由覆盖校验。System migration 107 先由独立标准测试命中；其后跨模块收口审查又修复了全量 IAM PostgreSQL 门禁中的陈旧总账断言和 Portal Runtime 残留，最终状态统一见 14.10。

Phase 4B 的目录、资产、申请、审批、履约、Portal 打开、真实 Service 查询和撤销生命周期已经在全量重启后完成浏览器验收。普通用户的三段式权限证据也已闭环：Grant 生效前不能运行，生效后可以通过 Portal 打开并执行真实 Service 查询，撤销后再次被 Workbench owner 拒绝。人工验收只可清理本轮临时申请、Authorization、Grant、成员关系和临时 Asset，不得删除或下线 14.5、14.7 已确认长期保留的 Workbench 示例。

17:34 的首次浏览器续验发生在全量 `keepalive restart -all` 仍在进行时：旧 Catalog iframe 的父级 AuthSession 已失效，页面短暂显示 0 条；整页重载后恢复两条长期保留 Data Application，证明目录数据没有丢失。新 Asset Backend 于 17:37 启动后，正式 `addp` 数据库已物化全局 `application / 数据应用 / enabled=true / sort_order=6` 类型；重载创建页后浏览器控制层因本地 iframe URL 安全策略拒绝继续交互，因此没有创建临时 Asset，也不能把后续申请、审批、打开和撤销记作浏览器通过。接力时应从创建 `application` Asset 重新开始，不需要清理本次未提交的表单。

本次运行态还发现 reconciler 在空队列时每两秒用 `First` 产生一条红色 `record not found` trace。领取查询已改为保持 `FOR UPDATE SKIP LOCKED`、排序和单条限制不变的 `Limit(1).Find`，空队列现在静默返回 `processed=false`。回归测试先捕获到一次 trace 并失败，修复后同一测试通过；Asset 全量 Go 测试和 PostgreSQL schema 门禁随后重新通过。运行中的 17:37 Asset 进程早于该日志修复构建，下一次标准重启后才会停止产生这些旧日志。

18:26–18:35 在用户完成全量重启后继续了真实浏览器续验。Asset 创建页首次按数据应用名称搜索不到候选，根因是共享 `CatalogEntryPicker` 调用 Catalog 默认治理视图；正式创建/编辑契约要求选择 `active` 来源的资源盘点条目，发布时再由后端执行 `publishable` 门禁。Picker 现固定使用唯一 `view=inventory&source_status=active` 主路径，并加入路由契约回归测试。修复后成功选中 `workbench-parameter-linkage-demo` 对应 CatalogEntry `f0359420-a884-4e11-a87f-bd60e37a65e2`，创建临时 Asset `codex-workbench-grant-e2e-20260827`（ID `20376`）；在条目仍为 `discovered` 时提交上架被“尚未完成业务编目”正确阻断。

Catalog 正式编目要求唯一主业务域、责任部门、业务责任人和至少一个数据管理员。当前租户最初没有 Department，故通过 System 正式组织管理入口创建长期可引用的 `户外数据治理部 / outdoor_data_governance`，再把该条目更新为 `curated`、`tenant` 可见，主业务域为 `户外域 / outdoor`，业务责任人和数据管理员为当前管理员；条目版本从 1 增至 2。部门创建过程同时暴露 System 前端允许连字符编码、后端却只接受小写字母/数字/下划线的问题：System 现复用受测试的 organization code 校验，同时覆盖 Department 与 Project Group，中文和英文均显示精确格式提示。`system/frontend` 新增 9 个边界用例，定向测试和生产构建均通过。

完成编目后，Asset `20376` 已成功上架；当前管理员在 Portal 提交 30 天申请，管理端审批后真实观察到 `pending -> fulfilling -> effective`，Portal “我的申请与授权”只在 `effective` 后出现 `/data-apps/d6c30859-15c8-4b88-964b-f2dd315fb923` 打开入口。运行端默认“长沙市”执行“查询全部组件”返回 10 条真实地块明细；把唯一共享参数改为“武汉市”后两个组件一起重新查询并返回空结果，证明参数变更确实驱动真实 Service 请求而不是静态预览。该轮申请人为应用创建者 ID `4`，因此只证明 Asset 履约事实、Portal 状态和运行查询主路径；“非拥有者只有在 Grant 生效后才能运行、撤销后被拒绝”仍须使用普通用户 ID `29` 单独验收，不能把本轮拥有者运行写成非拥有者授权通过。

18:39 后已完成管理员自申请链路的清理：Asset `20376` 对应 Authorization/Grant 先真实撤销，Workbench 规则写入撤销时间；随后 Asset 下架并删除，资产列表和数据库均不再存在该临时资产。清理没有修改或下线两个长期保留的 Workbench Data Application，正式编目所需的 `户外数据治理部 / outdoor_data_governance` 作为长期组织事实继续保留。

清理过程发现 Asset 后端允许删除 `draft | offline`，但详情页只在 `draft` 展示“删除”，属于前后端状态契约不一致。前端现按唯一正式状态集合同时支持草稿和已下架资产删除，并新增回归断言。删除确认框原先还落回 Element Plus 英文默认按钮 `No / Yes`；现显式使用 Asset i18n 的 `取消 / 确定`（英文 `Cancel / Confirm`），确认按钮使用危险操作样式。浏览器已验证下架资产出现删除入口、确认框文本正确，并完成实际删除。

普通用户生命周期使用 Asset `20377`（`Phase 4B 数据应用验收 1787826254094`）、申请 ID `7`、Authorization ID `7`、用户 ID `29` 和长期应用 `1714dcf7-f34e-4996-a8dc-3b88998ebe55` 完成。该用户通过正式租户邀请加入，并获得 `tenant.asset_consumer` 与 `tenant.data_viewer`；前者允许申请资产，后者提供 Workbench 执行的 Role Permission，最终运行权限严格取 `Role Permission ∩ owner Resource Grant`，没有把目录可见性或同租户身份当成执行授权。

浏览器已直接复核完整三段式边界：Grant 生效前 Workbench 拒绝运行；审批履约后，Portal 出现“打开数据应用”，同一用户加载 Revision 2 并通过真实 Service 查询返回 CDC checkpoint 数据；撤销从 `revocation_pending` 收敛为 `revoked` 后，同一用户刷新即看到“当前用户未获得该数据应用的运行授权”，不再发出查询。CatalogEntry `4ff43d3a-3815-49fc-80e7-831dd7cc92b8` 同期完成 `curated` 编目并升级至版本 2。验收后已关闭该用户的临时租户成员关系，并清理 Asset `20377`；当前 Asset `20376`、`20377` 均不存在，Authorization `6`、`7` 均为 `revoked`，两条 Workbench 撤销 tombstone 按审计语义保留，两个长期 Data Application 均保持 `published`。

该轮邀请还暴露出 Console 原先没有承接 System 生成的 `/invitations/accept?invitation=...`，邀请链接会落入需认证的通配路由。现已补齐唯一公开接受页：匿名用户通过 System 正式 registration API 注册并接受，已有会话通过 acceptance API 接受，切换账号先执行正式 Logout 再带原路径登录；System 返回的新 Tenant Session 直接接入共享内存 Browser AuthSession，不把 Access Token、邀请 Secret 或密码写入浏览器持久存储。登录认证规范、Console 单元测试与生产构建已同步。

同一轮日志复核还发现 Catalog 搜索投影 worker 在空队列时使用 `First`，每次轮询都会产生 `record not found` trace；根因和 Asset 履约 reconciler 的空队列日志相同。Catalog 现保留事务、稳定排序、`LIMIT 1` 与 PostgreSQL `FOR UPDATE SKIP LOCKED`，只把领取方式收敛为 `Find + RowsAffected`；新增 logger 捕获测试先稳定复现一次 trace，修复后为 0，`catalog/backend go test ./...` 全量通过。该运行态修复需在下一次标准 Catalog 重启后反映到日志。

### 14.10 Phase 4 跨模块收口审查（2026-08-27）

Phase 4 完成浏览器验收后又执行了一次 System、Workbench、Asset、Portal 与 Console 合同总审查，修复了三项会影响正式主路径的问题，没有进入 Phase 5：

1. 登录认证规范只允许“匿名注册并接受邀请”和“当前会话接受邀请”两条路径，但 System 仍保留公开 Enrollment Ticket 签发接口、可选接受字段、数据库表及安全策略字段，形成第三条认证旁路。旧路由、DTO、Service、Repository、模型和前端策略字段已全部删除；migration `000108_iam_remove_invitation_enrollment_ticket` 前向删除旧表、函数和策略列，同时为 Tenant Invitation 重建独立不可删除触发器。Console 继续只调用 `registrations` 与 `acceptances`，Swagger 和登录认证规范保持一致；
2. Workbench Resource Grant 的幂等履约原为“先查后插”，多个 Asset reconciler 同时处理同一 Authorization 时会产生唯一键错误。Repository 已收敛为以 `(tenant_id, source_module, source_identity)` 为冲突目标的原子插入；12 路并发 PostgreSQL 回归先稳定复现重复键失败，修复后全部返回同一规则 ID。冲突载荷仍返回业务冲突，撤销 tombstone 和运行时判定不变；
3. Portal Tenant Runtime 已被正式迁移禁用，但新 Tenant 初始化仍把 `addp-portal` 当作必须绑定的运行身份，导致所有新租户创建失败。内置 Tenant Runtime 清单和绑定查询已删除 Portal，未恢复旧角色或服务旁路。该修复同时恢复 Tenant 管理、邀请注册和组织闭环的 PostgreSQL 测试。

审查还把 System migration 总账断言同步到当前唯一权限路线：Catalog Runtime 已接入各专业 owner 的窄读权限，Asset Runtime 只保留 Catalog 动态解析与 Workbench Resource Grant，Portal Runtime 保持禁用，Model Writer 旧耦合保持删除；Notebook Session 的旧列级约束在 Engine Access Scope 迁移后由当前触发器与明细表边界取代。这里仅修正陈旧测试事实，没有恢复旧权限或兼容分支。

收口验证覆盖：

```bash
cd system/backend && go test ./...
cd workbench/backend && go test ./...
make test-system-frontend
make test-workbench-frontend
make test-console-frontend
make test-authorization
WORKBENCH_POSTGRES_TEST_DSN='postgres://.../addp_test?sslmode=disable' make test-workbench-postgres
ADDP_SYSTEM_POSTGRES_TEST_DSN='postgres://.../addp_iam_test?sslmode=disable' make test-system-iam-postgres
```

两个长期 Workbench 示例 `1714dcf7-f34e-4996-a8dc-3b88998ebe55`、`d6c30859-15c8-4b88-964b-f2dd315fb923` 未删除、未下线。本轮没有创建新的浏览器临时 Asset 或 Data Application。Phase 4 至此进入可接力状态；下一步应先确认 Phase 5 的最小范围，再决定优先做 BI 深化还是大屏展示，不应把两者同时展开。

### 14.11 Phase 5 第一段实现与验收（2026-08-27）

5.4 节三个边界已经确认并完成实现：稳定概念使用 `Selection Binding / 选择绑定`；首期只支持同页 Component 联动；赋值只允许完全同类型的标量字段。正式术语表、核心概念关系图、模块架构说明和 Workbench 模块边界已经同步，不建设通用事件/动作 DSL、多页面导航、bbox、多选或任意值转换。

Backend 在 `addp.workbench_data_application/v1` 快照中增加唯一 `selection_bindings` 集合，并在创建、更新和发布的同一校验主路径中核对源 Component、已选择输出字段、标量类型、Application Parameter、Parameter Binding、目标过滤字段及操作符。目标 Component 始终从既有 `parameter_bindings` 推导；发布修订仍是不可变 JSON 快照，不增加数据库表、运行 API、兼容字段或 v1/v2 双运行分支。

`common-frontend` 的表格、图表和地图 renderer 统一发出只含 `{ row_index }` 的 `result-select`，Workbench Renderer Host 再把事件交给 Runtime。Runtime 只从源 Component 当前原始 rows 读取声明字段，原子更新 Application Parameter，按 Parameter Binding 推导并重查受影响组件；并发查询使用 last-selection-wins，旧响应不能覆盖新选择。编辑器新增“选择联动”区，按 Descriptor 和完全同类型规则过滤源字段及 Application Parameter，并只读展示受影响组件。

真实浏览器验收使用长期应用 `d6c30859-15c8-4b88-964b-f2dd315fb923`，未创建临时应用、未删除或下线任何长期应用。该应用已发布为修订 2：源表格 `City` 绑定共享城市参数；把参数临时改成“观察值”后点击结果行，参数立即回填为“长沙市”，表格和图表自动重查，页面及浏览器 error/warn 日志均为空。Backend、PostgreSQL、三组 renderer、Workbench Frontend 构建测试和 Swagger 路由覆盖门禁均已通过。

当时标准 `make test-module MODULE=workbench` 的前置 `platform T0` 和 `make test-changed` 曾被并行 Asset/IAM 改动阻塞；相关 Permission、Swagger 和已删除文件扫描问题随后已收口。最终 `make test-changed` 已通过，其中包含 Workbench Backend、27 项 Frontend 测试与生产构建、PostgreSQL T2、Swagger 和 CI 登记检查；该历史阻塞不再作为当前接力前置条件。

### 14.12 Phase 5 单页 Wallboard 与全屏实现（2026-08-27）

5.5 节已经按单一路线实现。Data Application 的 `page` 增加必填 `display_mode`，新建固定产生 `desktop`，更新和发布只接受 `desktop | wallboard`；读取缺少该字段的既有不可变 Revision 时，由 Backend 快照规范化主路径收敛为 `desktop`，Frontend 不保留第二个缺省兜底。该字段继续位于 `addp.workbench_data_application/v1` JSON 快照，不新增表、迁移、API、路由或运行分支。

编辑器增加国际化的展示模式选择，并把“桌面布局”统一改为“页面布局”。Runtime 的 `desktop` 保持原流式十二列栅格；`wallboard` 计算 placement 最大布局行数，把这些行压入扣除页头和参数区后的当前浏览器视口。全屏按钮直接使用浏览器 Fullscreen API，并监听 `fullscreenchange`；全屏状态不进入草稿、Revision 或 Backend。两种模式继续复用同一 Parameter Binding、Selection Binding、Component 查询和当前访问者 Service 授权。

真实浏览器验收继续使用长期应用 `d6c30859-15c8-4b88-964b-f2dd315fb923`，未创建、删除或下线其他应用。旧草稿读取后明确显示“桌面”；切换为“大屏”保存并发布修订 3 后，运行根节点使用 `runtime--wallboard`，十二行 placement 生成 `repeat(12, minmax(0, 1fr))` 视口网格。查询返回 10 行；进入全屏后按钮变为“退出全屏”，退出后恢复；点击表格结果仍能把共享城市参数从“观察值”写回“长沙市”并自动重查，浏览器 error/warn 日志为空。

最终标准 `make test-module MODULE=workbench` 已完整通过 platform T0、Workbench Go T1、27 项 Frontend T1/T3 与生产构建、PostgreSQL T2；独立 Swagger 生成和 17 个公开路由覆盖、书稿及 diff 检查同样通过。带全部允许测试 DSN 的 `make test-changed` 已通过 platform、Agent 以及 Asset Go/Frontend，随后被当前并行 Asset PostgreSQL 改动阻断：`TestAssetSchemaMigrationAgainstPostgres` 在幂等迁移检查中发现 `asset.authorizations` relation 不存在。该失败不涉及 Workbench 文件或本轮契约；应由 Asset 当前改动收口后重新执行仓库聚合门禁。

### 14.13 Phase 5 Wallboard 应用刷新策略实现（2026-08-28）

5.6 节已经按单一路线完成代码和门禁。Data Application 的 `page` 增加必填 `refresh_interval_seconds`，只接受 `0 | 30 | 60 | 300`；`desktop` 只能为 `0`，`wallboard` 可选择任一档位。Backend 使用可检测字段缺失的请求 DTO，更新和发布不能把缺失误判为关闭；读取缺少该字段的既有不可变 Revision 时，才由同一快照规范化主路径补为 `0`。该字段继续位于现有 JSON 快照，不新增表、迁移、路由或运行 API。

编辑器在展示模式旁提供国际化固定档位，切回桌面会立即把策略收敛为关闭。Runtime 启用刷新后在 Descriptor 全部加载完成时立即查询一次，前一次查询结束后才创建下一次单次计时器；页面不可见时清除计时器，重新可见时立即刷新。手工或联动查询仍在进行时自动刷新跳过本轮，参数当前值保持不变，Component 继续独立显示查询错误。离开运行页会移除计时器和可见性监听，不创建 Task、Schedule、Execution 或后台轮询。

验证已经完成：

```bash
WORKBENCH_POSTGRES_TEST_DSN='postgres://addp:***@localhost:15432/addp_test?sslmode=disable' \
  make test-module MODULE=workbench
bash scripts/swagger/gen-swagger.sh workbench
bash scripts/swagger/check-route-coverage.sh workbench
git diff --check
```

标准模块门禁完整通过 platform T0、Workbench Go T1、28 项 Frontend T1/T3 与生产构建、PostgreSQL T2；Swagger 明确把字段声明为必填整数枚举，17 个公开路由覆盖一致。真实浏览器验收继续使用长期应用 `d6c30859-15c8-4b88-964b-f2dd315fb923`：编辑器选择 30 秒并保存后成功发布修订 4；运行页明确显示“自动刷新：30 秒”，无需手工查询即返回 10 行表格并完成图表渲染。Gateway 和 Service 日志从首次查询开始，每隔 30 秒稳定出现两条 Component 查询且全部返回 200，连续多轮没有请求堆积；浏览器 error/warn 日志为空。验收后已关闭运行标签页，避免继续产生后台轮询。长期应用 `1714dcf7-f34e-4996-a8dc-3b88998ebe55`、`d6c30859-15c8-4b88-964b-f2dd315fb923` 均未删除或下线。

### 14.14 Phase 5 Wallboard 应用呈现区块实现（2026-08-28）

5.7 节已经按单一路线完成设计、实现和验收。稳定概念 `Application Presentation Sections / 应用呈现区块` 已同步进入术语表、核心概念关系图、模块架构图和 Workbench 模块边界。现有 `page` 快照增加必填 `visible_sections`，只接受无重复的 `title | parameters | query_actions` 正向列表；新建默认全部显示，既有不可变 Revision 缺失时由 Backend 快照规范化主路径补全，列表顺序同时按固定枚举顺序规范化，避免语义相同的配置产生不同修订指纹。没有增加数据库表、迁移、路由、运行 API 或第二套页面模型。

编辑器只允许 `wallboard` 隐藏区块；切换回 `desktop` 会恢复全部区块。保存、更新和发布共用 Backend 约束：隐藏查询动作必须启用合法自动刷新，隐藏参数区必须保证每个必填 Application Parameter 都具有可用于全部绑定过滤器的默认值。Frontend 在选择和保存时提前给出国际化提示，Backend 仍作为最终契约边界。Runtime 的标题来源已经收敛为发布快照 `page.title`；修订状态、刷新状态、全屏入口、组件标题、加载和查询错误始终显示。

最终标准验证为：

```bash
WORKBENCH_POSTGRES_TEST_DSN='postgres://addp:***@localhost:15432/addp_test?sslmode=disable' \
  make test-module MODULE=workbench
bash scripts/swagger/gen-swagger.sh workbench
bash scripts/swagger/check-route-coverage.sh workbench
git diff --check
```

模块门禁完整覆盖 platform T0、Workbench Go T1、29 项 Frontend T1/T3 与生产构建、PostgreSQL T2；Swagger 把 `visible_sections` 声明为必填枚举数组，17 个公开路由覆盖一致。真实浏览器验收继续使用长期应用 `d6c30859-15c8-4b88-964b-f2dd315fb923`，未创建、删除或下线应用：在大屏、30 秒刷新条件下隐藏全部三个可选区块，保存并发布修订 5。运行页只保留紧凑工具栏、两个组件标题和查询结果；页面标题、说明、参数控件、查询按钮及表格分页均未出现。跨过一个完整 30 秒刷新周期后，表格与图表仍保持真实长沙市查询结果，页面查询错误为 0，浏览器 error/warn 日志为空；验收后已关闭运行标签页。

### 14.15 Phase 5 Data Application 资产运营指标评估（2026-08-28）

5.8 节已完成现状核查和模块归属设计，本段没有修改运行代码、数据库或 API。Asset 已拥有资产状态、上架趋势、申请、有效 Authorization 和 Rating 等运营事实；Service 已把 `service.query.executed | exported` 作为追加式审计事件写入 System，包含 Query Service、结果、用途、返回行数、错误码与查询形状指纹；Monitor 只聚合 `common.task_executions`，Data Application 在线消费不属于其执行监控范围。

当前唯一缺失的是 Workbench owner 持久化的“成功运行准入”事实。Runtime API 成功返回可以证明当前 User 在当时获准读取某个已发布 Revision，但现有实现只返回快照，不写使用事实。Component 查询随后由浏览器直接调用 Service，Service 无法可信获知它来自哪个 Data Application；同一 Query Service 也可被多个 Data Application 或外部客户端复用。因此本轮明确不从 Service Audit、Gateway 日志、Referer、Portal 点击或浏览器自报请求头拼接 Data Application 访问量，也不把有效授权人数命名为活跃用户。

第一阶段建议只在 Asset 现有事实上增加 application 类型和具体 Asset 的运营分组，不跨模块联查；Workbench 继续不展示访问量。只有在应用创建者或资产运营方确认确实需要“成功打开次数 / 独立访问用户 / Revision 分布”后，才单独设计 Workbench owner 的运行准入事实、保留周期、隐私边界和聚合 API。具体 Component 查询归因属于更高成本的受信消费上下文问题，不作为运行准入指标的前置条件，也不能用可伪造 header 临时解决。

### 14.16 Workbench 前端依赖门禁与 Asset 运营分组实现（2026-08-28）

`make test-workbench-frontend` 在干净依赖环境中暴露了 `@common-ui-map` 源码所需的 `@amap/amap-jsapi-loader` 无法解析。根因不是依赖未声明，而是 Workbench 直接解析 `common-frontend/map` 源码时，Vite 会从共享源码目录寻找 peer runtime；开发机残留的 `common-frontend/map/node_modules` 遮蔽了 CI 缺口。Workbench 现与 Service、Agent 的正式路线一致，把该 peer 显式解析到 Workbench 自己的依赖树，并增加合同测试。删除共享包残留依赖、执行全新 `npm ci` 后，30 项 Workbench 前端测试和生产构建均通过；本变更不需要服务重启。

5.8 节推荐的第一阶段 Asset 运营分组已经沿唯一 `/assets/stats/dashboard` 路由实现。Backend 支持全部资产、`application` 类型和具体 Asset 三种交集范围，完整传播查询错误；申请结果拆分为待审 / 通过 / 驳回，有效授权改为当前未过期、已履约并按用户去重，评价和两类 30 天趋势使用同一 Asset 范围。跨 Tenant、类型不匹配或不存在的具体 Asset 返回不存在。路由授权同步收紧为 Asset Entry、Application、Authorization、Rating 四类只读 Permission 的 all-of。

Asset 运营看板默认展示全部数据应用资产，并可选择全部资产或具体数据应用 Asset；全部文案、范围说明、错误、趋势可访问名称和异步播报均使用中英文 i18n，样式使用 ADDP 主题变量。Backend 全量 Go 测试、9 项 Asset 前端测试与生产构建、Swagger 生成和 41 个公开路由覆盖、授权覆盖均已通过。Asset PostgreSQL 标准门禁新增运营聚合用例，先发现并修复 PostgreSQL 保留字表别名问题，随后 Schema 与运营聚合两段真实 PostgreSQL 测试通过且清理测试 Schema。

标准重启后的真实浏览器复核已经完成。当前 Tenant 没有 application 类型 Asset，因此默认“全部数据应用资产”范围返回 0，选择器只展示“全部资产”和“全部数据应用资产”，未创建临时 Asset 补造选项；具体 Asset 的 Tenant / 类型 / 不存在隔离继续由 SQLite 与真实 PostgreSQL 聚合测试覆盖。切换到“全部资产”后返回 1097 个资产（1089 草稿、0 上架、8 下架）、5 个申请（5 通过）、0 个有效授权用户和 1 条 5.0 评价，30 天申请趋势存在 2026-07-30 的记录。中英文运行时切换还发现异步播报曾缓存翻译后的中文字符串，导致英文页面保留中文“已加载”消息；现改为只保存 `loaded | fetchFailed` 语义状态并按当前 locale 实时翻译，回归测试先失败后通过，中英文页面正文与播报均已复核一致。最后恢复为中文和默认数据应用范围；整个验证未创建、修改或删除业务对象。

### 14.17 外部 BI 消费契约与认证现状核查（2026-08-28）

5.9 节已完成 docs-first 核查，本段没有修改运行代码、数据库或 API。Service 当前的唯一私有消费路线已经是 canonical Bearer AuthContext：Consumer Catalog 和 Descriptor 要求 Tenant Context、`service.data_read.execute` 及 Tenant Scope Assignment；Query operation 对私有服务执行相同判断。该能力天然支持用户委托 OAuth，不要求 Workbench 参与查询。

现有 System Application / API Key 路线不能支撑私有 BI：Gateway 只验证 Key、设置本地 Gin Context、限流和写访问日志，不向 Service 传递 Principal、Tenant Membership、Role 或 AuthContext；`allowed_services` 当前也没有执行校验。公开 `/api/query/:serviceName/query` 注册在 API Key 中间件之外，因此连公开调用的 Key 归属、限流和日志也未形成真实闭环。与此同时，System 的 Client Credentials Provisioner 只处理 migration 建立的内置模块 OAuth Client，租户管理员没有注册外部 OAuth Client 的管理 API 或界面；内置 Tenant User Role 也只允许 User Principal，不能直接赋给外部 Service Principal。

据此确认外部 BI 第一版使用独立 OAuth Client 的用户委托 Authorization Code + PKCE，并复用现有 Consumer Catalog、Descriptor 和 query operation；API Key、Client Credentials、内置 Client 复用和 Workbench 代理全部排除。正式“接入指南”继续保持未完成，直到外部 OAuth Client 注册治理和至少一个真实 BI Connector 的端到端验证完成，避免文档先于产品能力宣称可用。

### 14.18 System 外部 OAuth Client 注册治理实现状态（2026-08-28）

System 已在既有 `system.oauth_clients` 聚合上实现租户外部 Client 管理，不新增 Principal、Token 类型或旁路认证。平台内置 Client 固定为 `owner_scope=platform`；租户创建的 Client 固定为 `owner_scope=tenant` 并绑定 `owner_tenant_id` 和创建者。外部 Client ID 使用 `addp_ext_` 前缀和随机主体，Client 固定为 Public Client，不生成或轮换 Client Secret，不绑定 Service Principal。远程回调只允许 HTTPS，HTTP 只允许 `127.0.0.0/8` 或 `::1` 的 IP 字面量回环地址；`localhost`、通配符、用户凭据、fragment、重复地址和超过 10 个回调均被前后端共同拒绝。

租户管理 API 固定为 `GET | POST /api/v1/system/tenant/oauth_clients`、`GET | PUT /api/v1/system/tenant/oauth_clients/:client_id`、`POST .../:client_id/suspend` 和 `POST .../:client_id/restore`。权限固定为 `iam.oauth_client.create | read | update | suspend | restore`，仅授予不可租户定制的内置 `tenant.administrator`；管理操作带正整数 `version` 做乐观并发控制并写 System IAM Audit。停用事务使用数据库时间取消待处理授权请求、撤销该 Client 的有效 Token Family 及派生 Token；恢复不会恢复旧授权。OAuth consent 读取和决定同时校验当前 User AuthContext 的 Tenant 与 Client owner Tenant，跨 Tenant 请求直接拒绝。

System IAM 的“外部 OAuth 客户端”页已接入统一 IAM 工作台及中英文 i18n，支持搜索、状态筛选、创建、编辑、复制 Client ID、停用和恢复。Swagger、授权 Manifest、生成常量、migration 111、后端/前端测试与现有 CI 自动发现入口同步完成。确定性 `go test ./...`、`make test-system-frontend`（10 个文件、41 个测试及生产构建）、`make test-authorization`，以及标准 `test-system-iam-postgres` 的 IAM、OAuth、API 和 migration PostgreSQL 门禁均已通过。

System 应用 migration 111 后已完成真实浏览器无副作用验收：租户管理员可以打开中文“外部 OAuth 客户端”页，列表 API 正常返回 0 条；创建表单初始保存按钮禁用，`https://localhost/...` 显示安全校验错误且不能保存，合法远程 HTTPS 回调可以进入可保存状态，随后取消表单，未创建持久记录。中英文切换、常见 1192px 窗口布局和浏览器控制台均已复核；验收同时修正状态播报缓存翻译后字符串导致英文页面残留中文，以及固定操作列遮挡状态和更新时间的问题。持久创建、编辑、停用、恢复的浏览器生命周期验收尚未执行，因为 OAuth Client 不允许物理删除，不能为测试擅自留下不可删除对象；后端真实 PostgreSQL 门禁已覆盖完整生命周期与 Token 撤销。真实 BI Connector 和正式接入指南仍是后续工作，不因管理控制面完成而提前标记通过。

### 14.19 Data Application 直接创作收口状态（2026-08-28）

本节是当前接力基线，并取代 14.1 至 14.8 中仍以 Workbench View 为现行产品对象的历史实施描述。现行单一路线为：用户创建 Data Application 草稿，在 Component 编辑器中直接选择已发布 Service、配置字段、参数、renderer 和布局，保存后发布 Revision，并从 `/data-apps/:application_id` 作为最终应用使用。Workbench View 聚合、`/api/v1/workbench/views`、`/workbench/views`、`workbench.view.*` 权限和 `workbench.views` 表均一次性删除，不保留兼容入口。

当前实现已经完成后端直接快照创建/更新、权威 Descriptor 指纹归一化、Component 增删改，前端服务选择与组件预览、应用参数绑定、布局、发布与运行页，以及 Console 菜单和搜索入口收敛。既有 Data Application 与不可变 Revision 均为自包含快照，不依赖被删除的 View 表；删除旧表不会改变现有应用运行结果。System migration 112 负责撤销内置角色上的旧 View Permission 并将其置为 disabled；Workbench schema migration 负责移除旧表。

`workbench-service-consumption` Online suite 已同步为直接创作验收：临时 Query Service 直接生成含 Table、Chart 两个 Component 的未发布应用，由浏览器在正式 Component 编辑器中验证两种 renderer，再通过 Service 契约变化验证告警和预览阻断；未发布应用可在 `finally` 正常删除。最终发布运行态由本地 Outdoor 双服务场景覆盖，不能为自动清理破坏“已产生 Revision 不可物理删除”的生命周期规则。生产代码和配置继续由契约测试禁止出现 Outdoor 或 Business MySQL 领域字段。

本轮 PostgreSQL schema/migration 门禁、Workbench 全量标准入口和全量 ADDP 重启已经完成。全量启动重新生成 19 个模块 Swagger 并通过路由覆盖校验；Workbench 只注册 12 个 Data Application、Catalog Reference 与 Resource Grant 公开路由，不再注册 View 路由。

Outdoor 双服务真实验收已于 2026-08-28 完成，并保留为长期示例应用：

- Data Application：`Outdoor 双服务分析应用`，ID `18c7223c-b5c0-4c25-ba28-648e85f44537`，发布 Revision 1；
- 最终运行入口：`/data-apps/18c7223c-b5c0-4c25-ba28-648e85f44537`；
- Component 1 直接消费 Query Service 25“户外人员对指标查询”，以 Table 展示并完成真实查询；
- Component 2 直接消费 Query Service 24“户外人员指标查询”，以 `metric_code` 为维度、`metric_value` 为度量的 Bar Chart 展示；
- 应用参数“人员”显式绑定 Component 2 的 `person_id eq` 筛选。默认值 `00a6cea35dc2cd11029061004e3b05cd` 查询出的指标值为 1；运行页改为 `0122a5876424ea6306af911c0551ee29` 后图表重新查询并显示指标值 3，证明最终运行页不是静态快照；
- “查询全部组件”同时返回人员对表格数据和人员指标图表，浏览器控制台无错误；Table 与 Chart 均显示“导出当前有界结果”，实际导出请求由 Service 返回 200；
- 实测发现并修复 Component 编辑器“添加参数”在缺少可执行 operator 的 Descriptor 字段上无法生成草稿的问题。参数创建现由独立契约函数过滤可执行字段并采用不可变数组更新；Blob 导出改为挂载下载链接后点击、下一事件循环释放 URL，并补充下载触发顺序回归测试。Workbench 前端 34 个测试及 production build 通过。

2026-08-29 继续修复了发布应用“运行”入口的公开路由归属。列表页和编辑页此前直接执行 `window.open('/data-apps/...')`，在 Console 的 Workbench iframe 中会按模块开发 origin 解析为 `localhost:5190/data-apps/...`，先落入 Vite `/workbench/` base 提示页。两个入口现统一通过共享 `resolveConsoleRouteUrl` 构造 Console canonical URL，再同步打开最终应用标签页；不增加 `/workbench/data-apps/...` 兼容路由或重定向。真实浏览器从应用列表点击“运行”后一次直达 `http://localhost:5170/data-apps/18c7223c-b5c0-4c25-ba28-648e85f44537`，正确渲染“Outdoor 人员指标分析”，浏览器 error 日志为空。

该应用已经产生不可变 Revision，按 Data Application 生命周期保留，不作为临时对象物理删除。Outdoor 证据只证明首个真实领域场景；下一项通用性门禁仍是 Business MySQL `commerce-order-analysis` 的异构 Online 验收。

### 14.20 Business MySQL 异构数据应用本地验收（2026-08-29）

本轮使用 `business-mysql` 标准样例中的 `customers` 与 `orders`，从 Service 正式界面创建 Query Service `commerce-order-analysis`（ID `26`），再由 Workbench 直接创作并发布 `Business MySQL 电商订单分析`（Data Application ID `c847d823-3314-42a2-b2a7-d5139fc68283`，Revision 1）。最终入口为 `/data-apps/c847d823-3314-42a2-b2a7-d5139fc68283`，直接由 Console `localhost:5170` 承载，没有跳转到 Workbench 开发端口。

该应用保留为异构长期示例，不作为临时对象删除或下线。应用包含一个 Table Component 和一个以 `city` 为维度、`total_amount` 为度量的 Bar Chart Component；两个 Component 共同绑定应用级“订单状态”和“城市”文本参数。默认 `订单状态=delivered` 时表格返回上海、成都两条订单；继续输入 `城市=上海` 并执行“查询全部组件”后，表格收敛为订单 `ORD-20260420-001`，图表同步收敛为上海、金额 `2897.00` 的单柱，证明参数通过同一 Application Parameter 显式驱动两个真实 Service 请求，不是编辑器预览或静态快照。

真实创建和运行过程发现并修复三类主路径问题：

1. Service 的资源能力探测和 Query Service 创建仍调用不带 Tenant Context 的 System Engine Client，导致正式界面分别返回 `Service Unavailable` 和“System engine request requires a tenant context”；两处均收敛到当前请求 Tenant 的唯一 Client 路径，并补直接 SQL Engine 回归测试；
2. Service 后端允许 Query Service 名称包含连字符，但前端只允许下划线，导致规范 fixture `commerce-order-analysis` 无法创建；前端校验和中英文提示现与后端统一支持小写字母、数字、下划线和连字符，并增加边界测试；
3. 已保存 Component 是 Vue reactive Proxy，编辑器直接执行 `structuredClone(component)` 会抛出 `DataCloneError`，使“编辑组件”无响应；现先通过 Vue `toRaw` 取得原始值再克隆，并增加源码合同回归测试。

本轮验证已经完成 Service Backend 全量测试、Service Frontend 21 项测试与生产构建、Workbench Frontend 36 项测试与生产构建，以及全量 ADDP 重启时的 19 模块 Swagger 生成和路由覆盖。浏览器继续验证了 Query Service 创建、Component 预览、应用保存、不可变发布、canonical 运行入口、默认筛选和二次城市筛选。

这里记录的是本地标准 Business MySQL 环境中的真实异构功能验收，不能替代 `workbench-service-consumption` 的专用 Online Host Gate T4。此前 workflow run `33036113139` 因没有匹配的专用 runner 长时间等待后被取消，未执行正式 fixture 和自动清理，因此 T4 仍为未完成；在专用 runner 可用前，不把本地开发机改造成长期 self-hosted runner，也不把本节写成 Online 通过。

### 14.21 Phase 6 通用 Value 与空间专题图（2026-09-01）

本轮按 8.1、8.4、8.5 和 8.6 的边界完成第一段场景化消费能力。`common-frontend/basic` 新增通用 `ScalarValueRenderer`，只显示 Service 返回的唯一完整行中显式配置的 1–4 个数值字段；空结果、多行、分页未完结、重复字段和非数值均显式拒绝，不在浏览器求和、计数、取第一行或推断指标。运行端和编辑器额外记录查询是否已经完成，尚未查询时显示中性空状态，避免把“还没有执行”误报成服务结果不满足唯一行约束。

`common-frontend/map` 的 GeoJSON renderer 新增显式要素名称字段、受控 `uniform | categorical | continuous` 专题样式和图例。分类与连续映射只读取 Component 已保存的字段和主题色板标识，快照不保存原始颜色、函数或任意样式 DSL；geometry、label、tooltip 和 thematic field 均来自 Consumer Descriptor 与用户显式选择。Workbench Backend 对相同配置执行强类型校验，连续样式只接受已选择数值字段，分类样式只接受已选择标量字段。

第一轮真实浏览器无持久副作用验收使用现有 Query Service 完成两条主路径：空间服务显式选择 geometry、名称字段和连续数值字段后成功绘制分级地图并生成五档图例；人员指标服务通过四个显式等值参数收敛到唯一结果行，Value renderer 成功显示配置的数值字段。该轮只在未保存的 Component 编辑对话框中进行，没有创建、删除或下线 Data Application。

随后通过 Service 正式创作入口发布私有 SQL Query Service `farmland-city-summary`（ID `27`），按城市返回 `city`、`feature_count` 和 `total_area`，稳定键与唯一可过滤字段均为 `city`。聚合 SQL、来源表和业务别名只保存在 Service owner 的服务配置中；Workbench 不新增计算字段、SQL、来源定位或业务口径。

Workbench 正式发布长期 Data Application `farmland-spatial-consumption`（ID `c4c0aa6e-70b1-49e8-8ade-8db92f5c6e33`，Revision 1），最终入口为 `/data-apps/c4c0aa6e-70b1-49e8-8ade-8db92f5c6e33`。应用包含四个显式配置的 Component：

- Value：消费服务 27 的城市唯一汇总行，显示要素数量和面积合计；
- Chart：消费服务 27 的完整城市分布，以 `city` 为维度、`total_area` 为度量；
- Map：消费空间服务 23 的有界 GeoJSON，以显式名称字段、连续数值字段和受控绿色色板生成五档图例；
- Table：消费空间服务 23 的城市明细。

唯一应用参数“城市”同时绑定 Value、Map 和 Table 的等值筛选；Chart 的 `city` 结果字段通过 Selection Binding 回写该参数。默认“长沙市”查询得到指标 10 个、面积 0.1094，并显示对应专题地图和 10 条明细；点击分布图中的“永州市”柱后，参数自动变为“永州市”，三个目标 Component 同步重查为 11 个、2.0039、更新后的五档地图和 11 条明细。该证据证明运行页已经形成参数化、可视化和联动的最终消费场景，而不是复制 Manager 或 Service 的单请求预览。

服务 ID、验收表名、字段名和业务值只作为本段运行证据及持久应用配置存在，不进入 Workbench 生产代码、Backend 默认值或 renderer 判断。Data Application 已产生不可变 Revision，按现行生命周期长期保留，不作为临时对象物理删除。

本轮标准验证结果：

- `go test ./...`（`workbench/backend`）通过；
- `WORKBENCH_POSTGRES_TEST_DSN='postgres://addp:***@127.0.0.1:15432/addp_test?sslmode=disable' make test-workbench-postgres` 通过；
- `make test-workbench-frontend` 通过 43 项测试及 production build；
- `bash scripts/swagger/gen-swagger.sh workbench` 与 `bash scripts/swagger/check-route-coverage.sh workbench` 通过，公开路由仍为 12 条；
- 全量 ADDP 服务重启完成，Console、Workbench Backend 与 Workbench Frontend Ready。

通用性门禁会递归扫描 Workbench 生产源码和默认配置，拒绝当前 Outdoor、空间样例与 Business MySQL 验收事实进入实现。Phase 6 当前剩余项不是继续增加 renderer，而是当真实数据量超过有界 GeoJSON 上限时，先定义 Tile / OGC Features 的稳定 Consumer Descriptor；在该需求出现前不提前建设旁路。聚合与业务口径继续由上游或 Service owner 提供，Workbench 不补计算表达式。

### 14.22 空间探索创作向导（2026-09-01）

本轮在 8.6 已确认边界内实现“空间探索创作向导”。向导不是持久化 Template 实体，不增加 Backend API 或运行时分支，只在空的数据应用草稿中读取两个 Query Service Consumer Descriptor，并把用户显式选择的服务、字段、参数、单位、精度、专题样式和受控色板编译为现有 Data Application Snapshot。生成结果固定使用 Value + Chart + Map + Table 四种通用角色、一个共享 Application Parameter、三个 Parameter Binding、一个 Chart Selection Binding 和十二列布局；生成后立即回到普通 Component 编辑、草稿保存、发布和运行主路径。

向导编译器执行与 Backend 同方向的前置约束：两个等值筛选字段类型必须完全一致，Chart 维度必须为同类型非空标量，Value 与 Chart 只能选择可查询数值字段，Map geometry 必须来自空间契约的 `primary_geometry_field`，专题字段必须与 `uniform | categorical | continuous` 模式匹配，所有查询与展示字段必须由 Descriptor 标记为 selectable。每个 Value 项的精度没有默认推断值，用户未显式填写 0–8 的整数时不能生成组件，避免小数结果被无意显示为 0。

浏览器无持久副作用验收使用现有汇总服务 27 和空间服务 23 作为表单输入，显式配置共享筛选值、两个 Value 项、Chart 度量、Map 名称字段、连续设色字段和受控绿色色板。向导成功生成四个组件，参数表显示一个必填文本参数，参数绑定显示 Value、Map、Table 三个目标，Selection Binding 显示 Chart 维度驱动三个目标，布局为首行 4 + 8 列、第二行 12 列地图、第三行 12 列明细。向导按钮在组件生成后被禁用，证明不会覆盖已有草稿。

验收继续打开向导生成的普通 Component 编辑器并执行真实查询：Value 返回 10 和 0.1094，保留显式单位及四位精度；Map 返回有界空间结果并显示连续专题图五档图例。首次复测还发现新生成组件的 `service_ref` 保留 Vue Descriptor 嵌套 Proxy，导致立即再次编辑时 `structuredClone` 失败；统一 Component 编译器现复制纯 `service_ref` Snapshot，修复普通新增组件和向导组件的共同根因。刷新后的独立浏览器页面重新验证 Value、Map 查询与立即编辑，错误日志为空。整个验收没有点击“创建草稿”或“发布”，没有新增、修改或删除持久 Data Application。

本轮 `make test-workbench-frontend` 通过 47 项测试及 production build；通用性门禁继续递归拒绝验收领域事实进入 Workbench 生产源码。Backend、API、数据库、Swagger 与 CI 入口均未改变，不需要重跑 PostgreSQL 或 Swagger 门禁；全量 ADDP keepalive 已重新启动并恢复 Console、Gateway、Workbench Backend 与 Workbench Frontend Ready。

### 14.23 保存前整页预览与唯一运行画布（2026-09-04）

本轮按 8.9 的边界实现保存前整页预览。原 `DataApplicationRuntime` 中的参数输入、Descriptor 指纹校验、查询、cursor、有限导出、Selection Binding、全屏、自动刷新、布局和 renderer 编排已整体收敛到 Workbench 自有的唯一 `DataApplicationCanvas`；正式运行页只读取当前发布 Revision 后传入画布，编辑器则按草稿保存的同一归一化规则生成脱离原对象的内存副本，通过全页对话层传入同一画布。未增加路由、Backend API、数据库记录、Service 查询协议、认证路径或临时应用。

预览入口使用中英文 i18n，并显式显示“未保存预览”状态。打开前要求应用名称、页面标题和至少一个 Component 齐备，继续执行现有呈现区块约束；预览加载时重新读取每个 Component 的 Consumer Descriptor，契约指纹不一致时与正式运行页一样停止查询。相关提示已改为不限定发布态的“当前应用配置停止查询”，因此同一画布在草稿预览和发布运行两种宿主下都保持准确。

真实浏览器验收先在一个历史空间应用上把页面标题临时改为未保存值：预览正确显示该值且浏览器 URL 保持编辑路由，四个过期契约组件按唯一指纹规则阻断，没有误用当前 Descriptor。随后使用当前契约有效的正式应用重复验收：未保存标题和“未保存预览”状态正确显示，查询全部组件成功返回真实结果；点击人员列表行后，预览内部应用参数更新并触发既有联动查询，关闭预览后编辑器仍保留原默认参数，预览选择值没有回写草稿。再直接打开 `/data-apps/:application_id`，发布修订标签、同一画布和真实查询均正常，浏览器控制台无错误。全程没有点击保存、发布、下线或删除，没有创建或修改持久 Data Application。

本轮 `make test-workbench-frontend` 通过 39 项测试及 production build；新增门禁覆盖预览副本与原草稿隔离、与保存载荷使用同一 Snapshot 归一化规则、创作端和发布运行端共用唯一应用画布、运行页不再拥有 Descriptor 或查询实现，以及不存在 `/preview` 路由。Backend、API、数据库、Swagger、根 Makefile 和 CI 编排均未改变，不需要重跑 PostgreSQL 或 Swagger 门禁。

### 14.24 跨方言参数占位符与 PostGIS 读取集证明修复（2026-09-04）

保存前整页预览继续使用长期应用 `farmland-spatial-consumption` 做运行态复核时，参数化查询先后暴露两个不属于 Workbench renderer 的 Service / Engine 主路径问题：结构化查询规划器固定生成 `?` 占位符，导致 PostgreSQL 筛选 SQL 在 Prepared Query 阶段报语法错误；修复占位符后，空间查询又因 PostgreSQL 读取集只能证明 `pg_catalog` / `internal` 函数而拒绝平台自身声明支持的 PostGIS `ST_AsGeoJSON`、`ST_Intersects` 和 `ST_MakeEnvelope`。Workbench 没有增加改写 SQL、绕过保护门禁或针对空间样例的兼容逻辑。

统一主路径已经收敛为：所有结构化 SQL 生成器根据 Engine Dialect 直接产生最终占位符，PostgreSQL 使用 `$1...$n`、Oracle 使用 `:1...:n`，其他当前方言使用 `?`；Provider 不再二次扫描或改写 SQL。PostgreSQL 读取集证明仍拒绝未知函数，只在候选函数满足非集合返回、非 `SECURITY DEFINER`、`IMMUTABLE | STABLE` 等安全条件后，信任 `pg_catalog` / `internal` 内建函数，或通过 `pg_depend -> pg_extension` 证明属于 Provider 明确声明的受信任扩展。当前唯一受信任扩展为 `postgis`，没有函数名白名单，也没有把其他扩展一并放行。正式规则已同步到 `docs/spec/addp引擎插件接口规范.md`。

最小充分验证已经覆盖：

- Service Query Plan 的 PostgreSQL、Oracle、既有基础参数序号、PostGIS bbox 占位符回归；
- Manager Profile Filter 与 Preview 的 PostgreSQL 占位符回归；
- PostgreSQL Provider Prepared Query 与读取集单元测试；
- `make test-common-postgres` 的真实 PostGIS 表、`ST_AsGeoJSON`、`ST_Intersects`、`ST_MakeEnvelope` 和 `$1...$5` 集成查询；
- `SERVICE_POSTGRES_TEST_DSN='postgres://addp:***@localhost:15432/addp_test?sslmode=disable' make test-service-postgres`；
- `GOWORK=off go test ./internal/service -count=1`、`GOWORK=off go test ./internal/profilefilter ./internal/preview -count=1`、`GOWORK=off go test ./query ./engine/plugins/postgresql -count=1`。

用户按标准方式重启全套服务后，Service、Workbench 与 Manager 均加载新构建。浏览器在不保存草稿的整页预览中重新读取四个 Component 的当前 Consumer Descriptor：默认“长沙市”一次查询成功返回 10 个地块、面积合计 0.1094、连续设色五档地图和 10 条明细；把参数改为“株洲市”后，三类受绑定组件同步变为 10 个地块、面积合计 0.9892、更新后的地图图例和 10 条株洲市明细。没有再出现 PostgreSQL 占位符语法错误或 PostGIS 读取集拒绝。Chart Selection Binding 本轮未改动，其真实点击联动证据仍以 14.21 的既有验收为准。

`make test-module MODULE=service` 当前在 T0 Authorization 覆盖检查处被同工作树中尚未收口的 Standard 权限改动阻断；根 `make test-go` 当前被 Asset `go.mod/go.sum` 的并行依赖变更阻断。两项都发生在进入本轮目标测试之前，不属于本次占位符或 PostGIS 修复；本轮已运行并通过上述对应模块、真实 PostgreSQL 与 PostGIS 门禁，不能把并行变更造成的总门禁失败写成通过。

用户确认持久变更后，四个 Component 的当前契约已通过普通编辑器主路径保存，并发布为 Data Application `c4c0aa6e-70b1-49e8-8ade-8db92f5c6e33` 的不可变 Revision 2；没有自动保存、静默刷新指纹或修改其他应用。最终运行入口显示“发布修订 2”，默认“长沙市”查询返回 10 个地块、面积合计 0.1094、连续设色五档地图和 10 条明细；运行页把参数改为“株洲市”后，再次返回 10 个地块、面积合计 0.9892、更新后的五档地图和 10 条株洲市明细，四种 renderer 均无契约告警或查询错误。

### 14.25 真实 BI Connector 最小实施设计（2026-09-05）

本轮复核了 System OAuth、Service Consumer Catalog、Consumer Descriptor、Query Service cursor 执行和当前主流 BI 扩展机制。结论是：第一条真实外部 BI 路线选择 **Power Query 自定义 Connector，并以 Power BI Desktop Import 模式作为首个产品验收载体**。这是具体适配器和验收工具的选择，不进入 Service Descriptor、Workbench 领域模型或平台通用术语。

选择依据如下：

1. Power Query 自定义 Connector 可以为 REST API 提供业务友好的导航表，支持自定义 OAuth `StartLogin / FinishLogin / Refresh / Logout`、客户端 PKCE、POST Body、动态 Schema 和分页，能够直接映射现有 ADDP 契约；
2. Tableau WDC 3.0 已被官方弃用，其官方替代 REST API Connector 当前只支持 HTTP GET，不能承载 ADDP 唯一的 POST 结构化 Query operation；Tableau Connector SDK 的长期主路径是 ODBC / JDBC，也会倒逼数据库或 SQL 旁路，因此不采用；
3. Looker Studio Community Connector 在 Google 托管运行，首验就要求外部可达的 HTTPS ADDP 环境、Google Apps Script 部署和账号治理，无法先在当前本地环境隔离验证 Service 协议，不作为第一条路线；
4. Power BI Desktop 只运行于 Windows，因此 macOS 本地开发机可以继续完成 ADDP 侧契约核查，但不能替代 Connector 的真实产品验收。构建和纯查询测试可使用 GitHub 托管的 Windows Runner，不要求把本地开发机配置为 self-hosted runner；交互式 OAuth 和 Power BI Desktop Navigator 仍需一个可操作的 Windows 环境。

对应官方事实来源：

- [Power Query Connector SDK](https://learn.microsoft.com/en-us/power-query/install-sdk)
- [Power Query 自定义 OAuth](https://learn.microsoft.com/en-us/power-query/handling-authentication)
- [Power Query REST 分页](https://learn.microsoft.com/en-us/power-query/handling-paging)
- [Power BI Desktop 系统要求](https://learn.microsoft.com/en-us/power-bi/fundamentals/desktop-get-the-desktop)
- [Tableau WDC 3.0 弃用说明](https://help.tableau.com/current/api/webdataconnector/en-us/docs/wdc_overview.html)

#### 14.25.1 Owner 与代码位置

Connector 是 Service Consumer Contract 的产品适配器，由 Service owner 维护，建议唯一位置为：

```text
service/connectors/power-query/
```

该目录只保存 Power Query Connector 源码、资源、测试查询、构建定义和安装说明，不进入 Service Backend 进程，不依赖 Workbench，也不形成新的 ADDP 运行模块。Workbench 继续作为内置 Service-native Data Application；Power Query Connector 是外部客户端，两者只共享 Service 公共消费契约。

不得把 Power Query M 代码放进 `common-frontend`、Workbench Frontend 或 System；不得新增 Power BI 专属 Backend、Workbench 查询代理、OData 兼容层、JDBC / ODBC Bridge、SQL 翻译器或静态 Token。具体 BI 品牌只允许出现在该适配器目录、测试和接入指南中。

#### 14.25.2 第一版用户入口与身份

Connector 唯一入口拟定为：

```text
ADDPService.Contents(base_url, client_id)
```

- `base_url` 是当前 ADDP Gateway 的公开根地址；生产连接必须使用 HTTPS；
- `client_id` 是当前 Tenant 管理员在 System 中为该 Power Query 部署独立注册的公共 OAuth Client ID，不是 Secret，可以作为 Data Source Path 的组成部分；
- Connector 固定请求 `scope=addp.api`，不接受 Client Secret、API Key、用户名密码或内置 Client ID；
- Connector 使用 Power Query 宿主文档规定的标准 HTTPS callback URI，并必须原样登记到该 Tenant OAuth Client；不得自行猜测或降级到 `localhost`；
- 凭据作用域固定为 `base_url + client_id`，避免不同 ADDP 环境或不同 Tenant Client 误共享 Token。

OAuth 实现使用 Advanced Signature，以便 `Refresh` 和 `Logout` 同时取得 Data Source Path：

1. `StartLogin` 生成 PKCE S256 verifier/challenge，向 `/api/v1/system/oauth/authorization_requests` 发起表单 POST；
2. Connector 只把 System 返回的 `request_id` 放入 Console `/oauth/authorize?request_id=...`，并在内存 Context 保存 verifier、`request_id`、一次性 `request_secret`、`client_id` 和 callback URI；
3. `FinishLogin` 必须校验 callback 中的单一 `state` 等于 `request_id`，再用单一 `code`、原 callback URI 和 verifier 兑换 Token；
4. `Refresh` 必须接受并保存每次响应返回的新 Refresh Token，不能继续复用旧 Token；
5. `Logout` 调用 `/api/v1/system/oauth/revoke`；用户取消时若 Power Query 宿主提供可调用的取消边界，则使用 `request_secret` 取消 pending request，否则依赖 System 固定五分钟过期，不能增加第二套状态；
6. 外部 Client 被停用、用户 Permission 被撤销或 Refresh Token Family 被撤销后，旧连接必须失败，恢复 Client 不能使旧凭据重新生效。

Power Query 会向 `StartLogin` 传入宿主 `state`，而 ADDP 安全规范固定使用随机 `request_id` 作为回调 `state`。官方函数签名允许 Connector 在 `FinishLogin` 中取得 Context、callback URI 和 state，但是否有宿主级的额外等值限制仍必须以真实 Power BI Desktop 运行证明；在该证据出现前不修改 ADDP OAuth 规范，也不增加兼容 state 字段。

#### 14.25.3 服务发现、Schema 与刷新

身份建立后，Connector 固定执行以下链路：

```text
GET Service Consumer Catalog
  -> 为每个服务读取 Consumer Descriptor
  -> 生成 Power Query Navigation Table
  -> 选择服务后调用 Descriptor query operation
  -> 按 cursor 获取下一页
  -> 按 output_contract 强制表结构和字段类型
```

具体规则：

1. Catalog 使用 `page/page_size` 完整枚举，不读取 Service 管理 API；Navigation Key 必须包含 `service_type + service_id + contract_fingerprint`，显示名称只使用 Descriptor title；
2. Power Query 保存的行选择会冻结上述 Navigation Key。刷新时若服务下线、权限消失或指纹变化，旧 Key 无法命中并显式失败，不能按标题、同名服务或新指纹自动改绑；
3. 每次读取数据仍重新取得 Descriptor，严格校验 `schema_version`、ServiceReference、指纹、operation method/path 和输入输出类型；只调用 Descriptor 声明的相对 path；
4. 第一版只使用 `format=json` 和 `X-ADDP-Query-Intent: query`。每页 `limit` 使用 Descriptor `page.max_limit`，下一页只使用响应 `page.next_cursor`，直到 `has_more=false`；不能解析 cursor、改用 offset 或从 URL 猜测下一页；
5. Connector 使用 Descriptor `default_selection` 建立默认列，并按 `output_contract.fields` 强制固定列顺序、nullable 和类型。`string/uuid/bool/int/bigint/float/double/date/time/timestamp` 映射为相应 Power Query 标量；缺少 precision / scale 证明的 `decimal`，以及 `bytes/mixed/json/array/geometry` 第一版保守映射为文本，不做有损数值或空间推断；
6. 当前 Descriptor 已能表达命名参数，但 Power BI Navigator 无法从运行时 Descriptor 动态生成稳定的 M 函数签名。第一版导航表只直接加载“不含无默认值必填 named parameter”的服务；同时保留显式 `parameters record` 的查询函数供后续稳定交互设计使用，不从字段名推断参数；
7. 第一版是 Import，不实现 DirectQuery 或 Query Folding。Power BI 的筛选、图表和模型在导入结果上工作；刷新重新执行 Service 查询。若未来需要把 Power Query 筛选下推为 ADDP structured filter，必须先独立定义可证明的 folding 子集，不能翻译任意 M 或 SQL。

#### 14.25.4 第一版验收门禁

只有以下证据全部完成后，才能勾选“真实 BI Connector 端到端验收”并编写面向用户的正式接入指南：

1. 使用 GitHub 托管 Windows Runner 构建唯一 `.mez` 制品，并运行 Power Query SDK query tests；根 `Makefile`、workflow 路径登记、依赖固定和制品校验必须与 Connector 同次实现；
2. 在 Power BI Desktop 中通过 System 创建的独立 Tenant OAuth Client 完成登录、Tenant Context、授权同意和 Navigator 打开；
3. 以至少一个普通表服务和一个空间表服务验证 Catalog、Descriptor、动态字段 Schema、多页 cursor、空结果和 nullable；空间 geometry 第一版作为文本列进入 BI，不宣称原生面渲染；
4. 验证 Access Token 过期后的 Refresh Token 轮换、并发或旧 Refresh Token 重用拒绝、Logout 撤销和 Client 停用；
5. 撤销 `service.data_read.execute` 后刷新返回权限错误；恢复 Permission 后由当前用户重新刷新，不缓存旧授权判断；
6. 修改测试服务公开契约后，既有 Power Query 查询因冻结指纹失败；用户重新在 Navigator 选择当前服务后才建立新查询；
7. 验证第二个不同来源的 Query Service，避免 Connector 偶然依赖 Outdoor、PostgreSQL、固定字段名或空间输出；
8. `.mez`、日志、测试快照和文档中不得包含 Access Token、Refresh Token、Authorization Code、PKCE verifier、request secret、筛选值或返回数据。

当前完成的是路线选择、现有协议可用性核查和最小实施设计，不是 Connector 已完成。现阶段没有确认需要修改 Service Consumer API；最先要用真实宿主证明的是 Power Query callback state、旋转 Refresh Token 和带指纹 Navigation Key 的刷新行为。由于当前开发机没有 Windows / Power BI Desktop，不能在本轮把这三项写成已验证。

### 14.26 无 Windows 环境的 Python 外部消费验收路线（2026-09-05）

当前没有可操作的 Windows / Power BI Desktop 环境，但这不应阻塞 Service Consumer Contract 本身的外部消费验收。本阶段改为先在已有 `addp-common` wheel 中实现产品无关的 Python `ServiceConsumerClient`，以 macOS 上已验证的 CLI Browser Login、OS Keychain 和 Refresh Token 轮换作为用户委托身份入口，完成 Catalog、Descriptor、Query、cursor、权限撤销和契约指纹的端到端协议证明。

SDK 的唯一 owner 与位置固定为：

```text
common-python/addp_common/client/service.py
```

对应单元测试放在 `common-python/tests/test_service_client.py`，公开类型由 `common-python/addp_common/client/__init__.py` 唯一导出。不新建第二个 Python 包，不把通用 SDK 放入 `workbench/`、`develop/`、`service/backend/` 或 `service/connectors/`。`service/connectors/<product>/` 只保留 Power Query、Looker Studio 等具体产品适配器。

`ServiceConsumerClient` 只承担以下协议职责：

1. 分页读取 `GET /api/v1/service/consumer/services`，不读取 Service 管理 DTO；
2. 按 `service_type + service_id` 读取 `addp.service_consumer/v1` Descriptor；
3. 仅执行 Descriptor 声明的同源 `POST /api/query/<service_name>/query` operation，不接受任意 URL；
4. 发送唯一结构化查询请求，并按不透明 `next_cursor` 迭代后续页；
5. 在执行前校验调用方冻结的 `contract_fingerprint`，不自动改绑当前契约；
6. 返回契约类型和原始 Python records，不引入 pandas、GeoPandas、可视化库、Outdoor 数据或固定字段假设。

Notebook、Python 脚本或其他分析工具是 SDK 的消费者，自行决定是否转换为 DataFrame、GeoDataFrame 或图表。首轮验收至少覆盖一个普通表 Query Service 和一个空间 Query Service，但测试只依赖契约 fixture，不将业务名称、字段名或几何列写入 SDK。

这一路线是真实的平台外用户委托消费验收，但不是 BI Connector 产品验收，因此不能据此勾选“以真实 BI Connector 完成端到端验收”。Power Query 设计保留；待出现可操作的 Windows 宿主后，具体 Connector 必须复用同一 Service Consumer Contract，不增加 Python 代理、数据库直连或手工 Token 旁路。

本轮已经完成 SDK、公开类型导出、README 示例和协议单元测试；SDK 不顶层加载桌面 OAuth / Keychain 依赖，只有显式使用 `from_cli_session()` 时才加载 CLI 会话能力，避免影响 Copilot 等服务端 Python 运行时。验证结果：`make test-common-python` 通过（172 passed、1 skipped、8 subtests passed），`make test-release RELEASE_SUITE=common-python-cli` 通过 wheel 构建、隔离安装与 CLI 产品链路，`make test-platform` 通过平台一致性、CI 注册和 Swagger 覆盖门禁。服务恢复后的真实普通表与空间 Query Service 只读运行结果见 14.27，不以 fixture 测试替代运行证据。

### 14.27 Python 外部消费者真实运行验收（2026-09-06）

全套 ADDP 由用户重启后，使用仓库源码中的 `ServiceConsumerClient.from_cli_session("http://localhost:8000")` 和既有 `addp auth login` OS Keychain 会话完成只读运行验收。`addp auth status` 先通过 System 权威 AuthContext 确认当前会话为 Tenant Context、用户 Principal、AAL2 和受限 `addp.api` Scope；验收过程未复制、打印或另存 Access Token / Refresh Token，未创建、更新或删除 Service、Data Application 与业务数据。

通用协议验收结果如下：

1. Consumer Catalog 返回 8 个当前用户可见的 Query Service，逐个 Descriptor 均通过 `addp.service_consumer/v1`、ServiceReference、query operation 和当前 Catalog 指纹校验；
2. 从 Catalog 按 `output_kind` 动态选择可运行服务，不按名称或字段硬编码。普通表选中 Query Service 21，空间表选中 Query Service 20；两者均以 `limit=1` 连续读取两个不透明 cursor 页，每页返回一行，`service_version` 跨页一致；
3. 普通表实际返回的 9 个字段全部属于 Descriptor 声明字段；空间表实际返回的 10 个字段全部属于 Descriptor 声明字段，主几何字段名从 `output_contract.spatial.primary_geometry_field` 动态取得且两页均存在有效值；
4. 对同一普通表和空间表故意提供格式正确但与当前 Catalog 不同的 SHA-256 指纹时，SDK 均在执行查询前返回 `ServiceConsumerContractError`，证明不会自动改绑变化后的服务契约；
5. 真实请求使用 Descriptor 声明的 POST query operation、JSON 格式和 query intent；没有读取 Service 管理 DTO、猜测 query route、解析 cursor 或绕过 Gateway。

随后以已发布 Data Application `18c7223c-b5c0-4c25-ba28-648e85f44537` Revision 2 的快照作为业务组合输入，动态读取其参数、Component Query Template、Parameter Binding 和 Selection Binding。当前快照已演进为 5 个 Component，分别消费 Query Service 29“户外人员目录查询”、Query Service 28“户外人员参与重叠度即时查询”和 Query Service 24“户外人员指标查询”。按当前 Catalog 契约显式选择后，5 个 Component 全部真实查询成功：两个目录组件各返回 50 行，重叠度组件返回 1 行，两个指标组件各返回 2 行；所有实际字段均属于各自 Descriptor 声明字段。业务值只用于内存中的请求联动，没有写入 SDK、测试 fixture、文档或默认配置。

本次同时发现两个不应由 SDK 兼容的运行态缺口：

- Revision 2 中两个指标 Component 冻结的 Query Service 24 指纹为 `sha256:6f822670a5edb2a804ec7eb2b77956ebadd8b420acd2ecbf48cc153aa51ff61d`，当前 Catalog 指纹为 `sha256:62f28a3611503d4ca2cfb4aeef28027b82b691510a2df3afe7f4137b0c82939e`。SDK 使用发布快照的冻结指纹时明确拒绝；只有显式选择当前 Catalog 契约后查询才成功。若要恢复该发布应用的完整运行，应在 Workbench 草稿中由用户明确重绑当前 Service 24 契约并发布新 Revision，不能静默刷新 Revision 2；
- Query Service 26“Business MySQL 电商订单分析”的 Catalog 和 Descriptor 可读，但查询稳定返回 HTTP 500、`query_execution_failed: service data protection gate is required: query read set unresolved`。同一身份和请求结构下其余 5 个无必填参数服务均查询成功，因此失败边界位于该服务的数据保护 read set 解析，而不是 Python SDK、OAuth、Gateway 或通用 Query Contract。该缺口应由 Service / Security 所有者独立修复，不在 SDK 中跳过数据保护门禁。

至此，Python 外部消费者的真实普通表、空间表、cursor、动态字段、空间契约、指纹冻结和 Outdoor 多服务业务组合均取得运行证据，可以勾选该项。它仍不等于 Power BI Desktop / Power Query 产品验收；真实 BI Connector、OAuth callback state 和具体宿主刷新行为继续保持未完成。

用户确认继续后，已通过 Workbench 唯一更新与发布 API 把 Revision 2 中两个 Service 24 指标 Component 显式重绑到当前 Catalog 指纹。更新前严格校验恰好只有这两个目标 Component 发生指纹漂移；保存后的 Draft version 5 与 Revision 2 逐字段比较，除两个 `contract_fingerprint` 外，页面、布局、参数、Parameter Binding、Selection Binding、Query Template 和 renderer 配置完全相同。草稿按冻结指纹执行 5 个 Component 均成功后发布不可变 Revision 3；发布后聚合 version 为 6、`has_unpublished_changes=false`，Runtime Snapshot 与发布 Snapshot 完全一致。再次从 Revision 3 读取冻结指纹并查询，5 个 Component 全部成功且均与当前 Catalog 指纹一致。浏览器最终运行入口显示“发布修订 3”，执行“查询全部组件”后无空结果状态，目录分页可用，浏览器错误日志为空。Revision 2 保持不可变，没有静默改写历史发布事实。

本次重绑还暴露并修复了 Component 编辑器把“配置可保存”和“当前可预览”错误合并的问题：必填 Component Parameter 可以没有组件局部默认值，因为它可以由 Application Parameter 在运行时提供；这种配置应允许保存，但在编辑器没有当前参数值时仍不能预览查询。`ApplicationComponentEditor.vue` 现在只用参数键、标签、唯一性和 Descriptor Named Parameter 类型判断配置有效性，`requiredParameterValuesPresent()` 单独控制查询与导出。回归测试覆盖“无局部默认值的必填参数可保存但不可预览”；浏览器复核中 Service 24 指标组件不再出现契约变化告警，“应用组件配置”可用而“查询”保持禁用，运行页继续由应用默认参数正常查询。`make test-workbench-frontend` 通过 40 个前端测试和 production build。

### 14.28 Business MySQL 查询读取集与输出血缘根因修复（2026-09-06）

Query Service 26 的失败根因已经定位到 MySQL Engine Provider：其 Prepared Query 一直把 `QueryReadSet` 留空。Service 的数据保护入口按统一契约调用 `PreparedQuery.ReadSet()`；当 Tenant 中存在任一受管理数据项时，无法证明读取对象的 MySQL 查询会统一失败关闭。因此，问题不是 Workbench、Python SDK、OAuth、Gateway 或 Query Service 请求格式，也不能通过 Service 中按引擎跳过保护门禁解决。

本轮先修订 Engine Runtime 与数据保护规范，再在 MySQL Provider 增加一条受限但可证明的读取集解析主路径：

1. 使用 MySQL 方言 AST 完整解析只读 `SELECT`，覆盖普通表、`JOIN` 和派生子查询中的真实表引用；
2. 未显式数据库名的表按当前 Engine Database 解析，显式数据库名按原引用解析，并生成规范 Engine Catalog ResourcePath；
3. 通过 `information_schema.tables` 逐一确认对象是非系统数据库中的 `BASE TABLE`，且存储引擎为 `InnoDB`，只有全部对象均可唯一证明时才返回精确 `QueryReadSet`；
4. CTE 或解析失败、可执行注释、行锁、普通函数 / UDF / 存储函数、View、Temporary Table、系统表以及 FEDERATED 等非 InnoDB 对象继续返回 `ErrQueryReadSetUnresolved`，不得猜测、部分返回或兼容放行；
5. 第一次重启验证证明 Query Service 26 的 ReadSet 已精确命中受管理的 MySQL DataItem，门禁按规范继续要求 `QueryOutputLineage`，错误从 read-set unresolved 前进为 output-lineage unresolved。为使受保护 MySQL 服务具备真实消费路径，Provider 继续补充更窄的直接列血缘：只允许由 AST 与实时表结构唯一证明的直接列和显式别名，并跨 JOIN 与带别名派生子查询逐层组合；wildcard、表达式、聚合、UNION、重复输出名、歧义或不存在的列继续失败关闭。

Provider 单元测试覆盖 Query Service 26 同结构的 wrapper JOIN、默认 / 显式数据库解析、去重、跨派生子查询 direct binding，以及函数、行锁、CTE、View、FEDERATED、系统库、wildcard、表达式、UNION、歧义列和不存在列拒绝。使用 Business MySQL 当前真实 Catalog 做只读 PreparedQuery 验证时，服务 SQL 精确解析为 `business/customers` 与 `business/orders`，两张表均由权威 Catalog 确认为 InnoDB；实时字段结构成功生成 2 个来源、10 个 direct bindings，并由同一 PreparedQuery 执行出上海已交付订单 `ORD-20260420-001`。`common` 与 `service/backend` 全量 Go 测试和 diff whitespace 检查均通过；实现完成时 `make test-platform` 也曾通过。最终收口重跑时，该平台门禁被工作树中另一组 Office 预览重构阻断：受跟踪的 `common-frontend/basic/src/components/previews/OfficePreview.vue` 已删除，而唯一所有权测试仍读取该旧路径；本轮没有恢复或改写这组独立用户变更。根级 `make test-go` 在进入本轮相关模块前被仓库既有 `asset/backend` Go Module tidy 漂移阻塞；同类既有漂移还存在于 `portal/backend`、`security/backend` 和 `standard/backend`，本轮同样没有越界修改这些独立变更。

第二次统一重启后，Query Service 26 已完成最终运行态验收。Python Service Consumer SDK 从 Consumer Catalog 与 Descriptor 动态取得当前契约指纹和 10 个输出字段，不依赖服务名之外的样例事实；默认查询以 `order_no` 为稳定键读取两个不透明 cursor 页，共返回 4 行且跨页 `service_version` 一致。`status=delivered` 与 `city=上海` 的组合筛选返回唯一订单 `ORD-20260420-001`、金额 `2897.00`；格式正确但过期的契约指纹在执行前被明确拒绝。Service 日志中的这些请求均返回 HTTP 200，未再出现 read-set、output-lineage 或数据保护门禁错误。

已发布 Data Application `c847d823-3314-42a2-b2a7-d5139fc68283` 的 Revision 1 仍按冻结契约正确显示两个 Component 的契约变化告警，没有被静默修改。验收随后通过 Workbench 唯一更新与发布主路径，只把两个 Component 的 `contract_fingerprint` 显式重绑为 Query Service 26 当前指纹；除这两个字段外，Snapshot 没有变化。草稿聚合版本从 3 经更新和发布推进到 5，并产生不可变 Revision 2，发布后的 Runtime Snapshot 与 Revision 2 完全一致。浏览器运行页显示“发布修订 2”：默认 `status=delivered` 时 Table 返回上海和成都两行；再输入 `city=上海` 后 Table 收敛为一行，Chart 同步显示上海单柱、数值 `2897`，浏览器错误与警告日志为空。以上 ID、订单和值仅作为本地真实验收证据，没有写入 Workbench、SDK 或共享 renderer 的生产代码与默认配置。至此，本节状态为“ReadSet、直接列 OutputLineage、Python SDK 与最终 Data Application 运行态全部验收完成”。

继续补齐本地异构边界后，当前 Consumer Descriptor 明确声明 `output_kind=tabular`、`spatial=null`，只支持 `json | csv`，10 个字段均按当前发布契约返回，其中 `decimal`、`timestamp` 和 `int` 保持各自类型语义。以最大有界页和 `export` intent 执行 `status=delivered AND city=上海` 的真实 CSV 请求返回 HTTP 200、`text/csv`、`X-ADDP-Has-More=false` 和当前 `service_version`，CSV 字段顺序与 Descriptor 默认选择一致，唯一数据行完整可读；运行页的“导出当前有界结果”也走同一正式请求并返回 200。组件编辑器对该非空间服务实际只提供“表格、图表、数值卡片”，没有 Map 选项；运行页与编辑器的浏览器错误、警告日志均为空。`workbench/backend` 全量 Go 测试及 `make test-workbench-frontend` 的 40 项测试和 production build 均通过。以上本地证据补全 Phase 3 的 CSV、标量格式和无空间 renderer 约束，但按既定门禁规则仍不替代专用 Runner 上的 `workbench-service-consumption` Online T4，因此对应 Online 清单继续保持未勾选。

### 14.29 最终应用运行态错误恢复边界（2026-09-06）

最终应用运行画布必须区分三类状态，不能继续共用一个会永久禁用操作的 `error`：

1. 当前 Consumer Descriptor 指纹与发布 Revision 冻结指纹不一致属于契约阻断；Component 不能执行，必须由所有者显式重绑并发布新 Revision；
2. Descriptor 首次读取失败属于可重试的依赖可用性错误；页面保留错误提示，“查询全部组件”和 wallboard 前台刷新可以重新读取 Descriptor，恢复后进入同一执行主路径；
3. 已取得 Descriptor 后的某次查询失败属于可重试的运行错误；不得清除上一次成功结果，不得永久禁用 Component 查询、导出或应用级查询，后续手工查询和 wallboard 前台刷新成功后清除该错误。
4. 当前发布 Revision 首次读取失败属于可重试的 Workbench 依赖错误；正式运行页必须提供原页重试，不能要求用户刷新浏览器。同一 Router 实例切换 `application_id` 时必须清空旧应用并重新读取目标 Revision，不能继续展示上一应用。

该恢复能力只管理浏览器当前会话状态，不写入 Data Application Snapshot，不增加 Backend API、Service 代理、兼容路由或后台任务。发布 Revision 读取继续使用唯一 runtime API，重试和路由切换不产生第二套加载协议。契约变化仍严格失败关闭；只有依赖不可用和单次执行失败可以在同一发布 Revision 内重试。

实现已经把 Component 会话状态拆分为 `contract_error`、`descriptor_error` 和 `query_error`：前者继续阻断执行，Descriptor 临时失败允许“查询全部组件”及 wallboard 刷新重新读取，查询临时失败保留上一次成功结果并允许再次查询或导出。正式运行页的 Revision 加载错误现在提供原页“重试”；`application_id` 变化时先销毁旧画布，再从唯一 runtime API 加载目标 Revision，并使用共享 latest-request 协调器拒绝迟到响应覆盖新应用。合同测试先因缺少重试入口和路由监听失败，补齐实现后 `make test-workbench-frontend` 的 43 项测试和 production build 均通过。

全套服务恢复后，CLI OAuth 状态重新通过权威 AuthContext 校验；通过 Gateway 读取 Business MySQL Data Application Revision 2 及两个当前 Descriptor，并按发布 Snapshot 的 Parameter Binding 编译 `status=delivered AND city=上海` 请求，Table 与 Chart 两个 Component 均返回同一条订单和相同 `service_version`，证明后端、Service 和发布契约已经恢复。用户随后打开正式 `/data-apps/:application_id` 入口并确认“查询全部组件”运行正常。补齐 Revision 原页重试和路由切换恢复后，同一正式入口再次只读复核：Revision 2 正常加载，“查询全部组件”返回两条已交付订单，浏览器错误与警告日志为空。正常运行态已经实测；本轮不再通过主动停止服务制造故障，Revision / Descriptor / 查询临时失败后的同页重试语义由上述确定性合同测试覆盖，契约漂移继续由既有真实 Revision 阻断证据覆盖。至此，本节实现、门禁和运行页复核均已完成。

### 14.30 Component 编辑器异步上下文隔离（2026-09-06）

Component 编辑器的服务目录初始化、Descriptor 读取、预览查询和有界导出共享同一份可变草稿，因此每个异步结果都必须绑定发起时的编辑上下文。用户关闭或重新打开编辑器、切换目标 Component、快速切换 Service，或者在预览请求进行中修改查询配置时，旧请求必须立即失效；迟到的 Descriptor 不得覆盖当前服务和草稿，迟到的查询结果不得覆盖当前预览，迟到请求的错误不得在新上下文中提示。

该隔离只复用 `common-frontend/basic` 的 latest-request 协调能力，不增加缓存、取消 API、兼容分支或第二套编辑状态。服务切换时先清空旧 Descriptor、草稿和预览，再读取当前 Service Consumer Descriptor；编辑器关闭时统一失效仍在途的 Descriptor、预览和导出请求。保存时仍只提交当前 Descriptor 验证过的 Component 配置快照。

实现已为 Descriptor 加载与查询 / 导出分别建立 latest-request 上下文：编辑器初始化绑定目标 Component，服务切换绑定当前 Service；查询和导出绑定当前 Service，并在任何会影响请求的配置变化时统一失效。异步成功、失败提示和 loading 状态都只允许由当前请求写回；弹窗开始关闭时立即失效全部请求，重新打开后只从当前 Component 重新构建草稿。源码合同测试先稳定证明旧实现缺少上下文隔离；连同 14.31 的分页回归，最终 `make test-workbench-frontend` 通过 45 项测试和 production build。

浏览器烟测使用既有耕地应用但没有保存任何变更：打开“城市耕地概览”组件后，连续选择 Business MySQL 服务和户外人员目录服务，最终界面只保留后选服务的 Descriptor 字段；取消并重新打开后恢复为原“耕地城市汇总”服务、原字段和原 renderer 配置，浏览器错误与警告日志为空。上述服务与字段只作为运行证据，未写入 Workbench 生产代码、默认配置或测试 fixture。

### 14.31 Component 编辑器游标翻页原子提交（2026-09-06）

Component 编辑器的游标、页码、结果行和 Page Metadata 必须作为一次查询结果原子提交。点击上一页或下一页时只计算候选游标状态；只有目标页查询成功且请求仍属于当前编辑上下文，才同时替换结果与分页状态。查询失败或请求因服务 / 配置切换而失效时，继续保留上一份成功结果及其页码，不得出现“旧数据配新页码”的假状态，也不得解析、回退或猜测 Service 返回的不透明 cursor。

该规则与最终应用画布的事务式翻页语义保持一致，不新增分页缓存、重试 API 或第二套 cursor 协议。首次预览仍从空 cursor 和第 1 页开始；重新查询或修改请求配置继续清空旧分页状态。

实现已把 `executeAtCursor` 的候选页码和游标栈纳入成功结果提交；上一页、下一页不再预先修改响应式分页状态。回归测试先稳定命中旧实现的提前增减页码，再验证新实现只在当前请求成功后同时写回结果、Page Metadata、页码和游标栈。浏览器使用新建但未保存的通用 Component 配置消费既有多页服务：第 1 页可进入第 2 页，第 2 页可返回第 1 页，浏览器错误与警告日志为空；烟测完成后取消弹窗，没有写入应用草稿。

### 14.32 运行画布参数上下文失效（2026-09-06）

最终应用的查询结果必须对应当前 Application Parameter 值。用户修改参数时，所有通过 Parameter Binding 消费该参数的 Component 必须立即失效仍在途的查询和导出，并清空已经不再对应当前参数的结果、分页与运行错误；未绑定该参数的 Component 不受影响。旧参数请求即使迟到，也不得回写结果、错误或 loading 状态，更不得触发文件下载。

受影响 Component 必须只从 Snapshot 的 Parameter Binding 推导，不能在 UI、renderer 或请求状态中保存第二份目标列表。Selection Binding 写入参数时复用同一参数更新入口，再查询推导出的目标 Component；手工输入和选择联动因此具有同一失效语义。应用级“查询全部组件”也必须具备独立 generation，参数变化后允许按新值立即发起新一轮查询，而不是等待旧一轮网络请求结束。

实现已增加唯一的 `componentIDsForApplicationParameters()` 推导函数，手工参数输入与 Selection Binding 共用它；运行画布按推导结果失效 Component 请求并清空结果、分页和查询错误，Query All 使用独立 latest-request generation，导出也进入 Component request generation，迟到响应不会下载旧参数文件。回归测试先证明旧实现既没有参数更新入口，也没有受影响 Component 推导与 Query All 失效，再覆盖去重、无关参数和未知参数边界。`make test-workbench-frontend` 已通过 47 项测试和 production build；构建只保留既有的大 chunk 非阻断警告。

服务恢复后已从既有正式 Data Application `c847d823-3314-42a2-b2a7-d5139fc68283` 的发布 Revision 2 完成浏览器运行态复核。当前发布 Snapshot 中“城市”参数通过 Parameter Binding 同时绑定 Table 和 Chart；默认 `status=delivered` 查询返回上海、成都两条订单，随后把城市改为“成都”时，旧上海表格结果立即消失并进入无结果状态，再执行“查询全部组件”后只返回成都订单 `ORD-20260423-004`，上海订单不再出现。System 审计同步记录两个 Component 请求均从默认条件的 `returned_count=2` 收敛为城市条件的 `returned_count=1`，浏览器页面没有运行错误告警。以上应用 ID 与订单号只作为本地真实验收证据，没有进入 Workbench 生产代码或默认配置。

### 14.33 运行画布参数竞态自动化证据（2026-09-06）

14.32 的浏览器验收已经证明真实 Service 请求会按新参数执行，但不能稳定制造“旧请求恰好晚于参数修改返回”的时序。该竞态必须在 Workbench 前端 T1 中使用可控 Promise 固定复现；测试不能依赖网络速度、真实 Service、验收数据或浏览器等待时间，也不能只用源码正则断言代替行为证据。

生产代码只提炼 Workbench 自有的参数结果失效事务：目标 Component 仍唯一由 Snapshot Parameter Binding 推导；每个目标状态必须先使请求 generation 失效，再原子清空查询、导出、结果、分页和运行错误。测试使用与生产画布相同的请求 coordinator 和失效函数，先启动旧请求、修改参数并检查旧结果立即清空，再释放旧响应并确认其无法提交结果或恢复 loading。该提炼不进入 `common-frontend`，因为 Application Parameter、Parameter Binding 和 Component 运行状态均属于 Workbench 领域。

实现已将画布内联的清理逻辑收敛为唯一 `invalidateApplicationParameterResults()`，手工参数输入和 Selection Binding 仍通过同一个 `updateParameterValues()` 入口调用它。T1 使用可控 Promise 启动旧 generation，参数失效后先验证绑定 Component 的结果、分页、错误、查询和导出状态被原子清空，同时验证未绑定 Component 保持原结果；释放迟到响应后，旧 generation 无法提交数据或恢复 loading。测试已完成“缺少生产导出时失败、提炼后通过”的红—绿验证；`make test-workbench-frontend` 通过 48 项测试和 production build，仅保留既有的大 chunk 非阻断警告。

### 14.34 运行画布迟到导出副作用阻断（2026-09-06）

参数变化不仅要阻止旧查询结果回写，也必须阻止旧导出响应触发浏览器文件下载。该门禁不能只检查 `exporting=false`，因为网络响应仍可能在状态清理后到达；下载动作必须在执行 Blob URL、DOM link 和 click 副作用之前，再次以 Component request generation 和 Component ID 校验当前上下文。

Workbench 有界导出完成函数应返回唯一明确结果 `stale | incomplete | downloaded`：`stale` 不触发任何下载或提示，`incomplete` 继续使用既有有界结果警告，只有 `downloaded` 才允许创建下载副作用。T1 使用可控 Promise 先启动导出，再通过 14.33 的参数失效事务使 generation 过期，最后释放旧响应；测试必须直接调用画布使用的同一导出完成函数，并证明返回 `stale`，而不是复制一段 `isCurrent` 判断。

实现已新增唯一 `downloadCurrentBoundedExport()` 并替换画布内联的“先校验、再下载”分支；该函数在任何 Blob URL 或 DOM 副作用前检查调用方提供的当前 generation，随后统一区分 `stale | incomplete | downloaded`。T1 先因缺少该生产导出而失败，再验证参数失效后释放旧导出只返回 `stale`；既有导出测试同步改为覆盖当前完整响应的 `downloaded` 及有后续页响应的 `incomplete`。`make test-workbench-frontend` 已通过 49 项测试和 production build，仅保留既有的大 chunk 非阻断警告。

### 14.35 运行画布 Descriptor 加载竞态（2026-09-06）

运行画布挂载时批量加载 Component Descriptor；在 Descriptor 尚未返回或临时失败时，“查询全部组件”也可以为同一 Component 发起 Descriptor 重试。两个请求必须遵循 latest-request 语义：较早请求的成功、契约变化或临时失败响应都不能覆盖较新请求已经提交的状态。Descriptor 加载 generation 必须与查询、导出 generation 独立，Descriptor 重试不能取消正在执行的数据请求，数据请求也不能使 Descriptor 响应失效。

每个 Component 状态只维护一个 `descriptorRequests` coordinator。Descriptor 请求开始时保存 Component ID 作为目标；成功、契约指纹变化和异常三条响应路径都必须在写入 `descriptor`、`descriptor_error` 或 `contract_error` 前校验当前 generation。画布销毁时同时失效 Descriptor 与查询/导出 coordinator。T1 使用两个可控 Promise，先让较新的 Descriptor 请求成功提交，再释放较早失败请求；最终状态必须保留新 Descriptor 且不出现旧错误。测试不得依赖真实 Service 延迟或重复保存 Descriptor 状态。

实现已为每个 Component 增加独立 `descriptorRequests`，并用唯一 `commitLatestComponentDescriptorState()` 包住 Descriptor 成功、契约变化和异常三条状态提交路径；查询/导出继续使用原 `requests` coordinator，二者没有互相取消。画布销毁时同时失效两类 generation。T1 先因缺少生产提交函数失败，再以两个可控 Promise 验证新成功先提交、旧失败后返回时，旧响应得到 `false` 且不能清空新 Descriptor 或写入旧错误。`make test-workbench-frontend` 已通过 50 项测试和 production build，仅保留既有的大 chunk 非阻断警告。

### 14.36 已发布应用路由切换竞态证据（2026-09-06）

Console 同源运行路由 `/data-apps/:application_id` 在同一前端会话内可以从应用 A 切换到应用 B。A 的运行快照请求若晚于 B 返回，不得把页面切回 A，也不得写入 A 的错误或用 A 的 finally 提前关闭 B 的 loading。现有 `DataApplicationRuntime` 已使用 application ID 作为 latest-request 目标，但源码合同只能证明调用形状，不能固定证明交错响应下的状态结果。

运行页应使用唯一 `commitLatestDataApplicationLoad()` 包住成功、失败和 finally 的所有状态提交；该函数按规范化后的当前路由 application ID 校验 request generation，只有当前请求才执行提交回调。T1 使用可控 Promise 先发 A、再发 B，先释放旧 A 并确认其成功与 finally 都不能写状态或关闭 B 的 loading，再释放 B 并只提交应用 B；最终 application、page error 和 loading 必须全部保持 B 上下文。该证据不新增路由、缓存、预取或双轨运行状态。

实现已新增唯一 `commitLatestDataApplicationLoad()`，并将 `DataApplicationRuntime` 的成功、异常和 finally 三条提交全部接入该函数；当前路由 ID 在提交时统一规范化，旧请求不能写入 application、page error 或 loading。T1 先因缺少生产提交函数失败，再用可控 Promise 先发 A、切到 B、释放旧 A，证明 A 的成功与 finally 均返回非当前且 B 的 loading 保持；随后释放 B，只有 B 被提交并结束 loading。`make test-workbench-frontend` 已通过 51 项测试和 production build，仅保留既有的大 chunk 非阻断警告。

### 14.37 已发布应用运行页卸载请求失效（2026-09-06）

从 `/data-apps/:application_id` 离开时，运行页必须立即使当前 Revision 加载请求失效。组件卸载后到达的成功、异常和 finally 都不得继续提交 application、page error 或 loading；不能仅依赖 Vue 销毁 DOM，因为迟到回调仍会执行，也不能新增 AbortController 兼容分支或第二套页面存活状态。

`DataApplicationRuntime` 应在唯一 `onBeforeUnmount` 生命周期中调用既有 latest-request coordinator 的 `invalidate()`，继续由 `commitLatestDataApplicationLoad()` 统一拦截全部迟到提交。T1 使用可控 Promise 启动加载后模拟卸载失效，再分别释放成功结果与 finally，证明二者都返回非当前且状态保持卸载时的快照；源码合同同时固定运行页注册卸载钩子。该收口只管理 Workbench Revision 请求，不改变运行画布自身已经存在的 Component 查询、Descriptor 和自动刷新销毁逻辑。

实现已在 `DataApplicationRuntime` 的唯一 `onBeforeUnmount` 钩子中失效 Revision latest-request generation，成功、异常与 finally 继续只通过 `commitLatestDataApplicationLoad()` 提交，没有增加页面存活标志或第二条取消路径。T1 先以源码合同得到缺少卸载钩子的单一红灯，再补钩子转绿；可控 Promise 在模拟卸载后释放旧成功响应，证明 application 与 loading 均保持卸载时快照。`make test-workbench-frontend` 已通过 52 项测试和 production build，仅保留既有的大 chunk 非阻断警告。

### 14.38 Component 编辑器卸载请求失效（2026-09-06）

`ApplicationComponentEditor` 的 `destroy-on-close` 弹窗已经在 close 事件中调用 `invalidateEditorRequests()`，但用户从 Data Application 编辑页直接离开时，父级销毁子组件不应依赖弹窗 close 事件是否发生。编辑器卸载后到达的 Consumer Catalog、Descriptor、查询或导出响应不得写入草稿预览状态、弹出错误消息或触发文件下载。

编辑器必须复用唯一 `invalidateEditorRequests()`，在 `onBeforeUnmount` 中同时失效 `descriptorRequests` 与 `operationRequests`；不得新增 mounted 标志、第二个失效函数或单独的卸载分支。既有各响应路径继续在状态写入、消息和下载副作用前检查对应 generation。T1 源码合同固定 close 与 unmount 共享同一函数，并确认该函数失效两个 coordinator；异步提交行为继续由 14.30 的 obsolete service context 门禁和共享 latest-request 语义覆盖。

实现已将 Vue `onBeforeUnmount` 接到既有 `invalidateEditorRequests()`，与 Element Plus dialog close 事件共用同一失效入口；该函数继续一次性失效 Descriptor 与 operation 两个 coordinator，并收拢 loading、querying 和 exporting 状态。T1 先得到缺少卸载钩子的单一红灯，补齐后 14 项定向合同全部通过；`make test-workbench-frontend` 已通过 52 项测试和 production build，仅保留既有的大 chunk 非阻断警告。

### 14.39 Data Application 编辑页路由上下文隔离（2026-09-06）

Workbench 的 `applications/new` 与 `applications/:id` 共用 `DataApplicationEditor` 组件，Vue Router 在创建页、不同应用编辑页之间导航时可以复用同一组件实例。编辑页若只在 mounted 时加载一次，会继续展示旧应用；旧应用请求或其 Component Descriptor 批量加载若迟到，还可能在新路由已经生效后覆盖当前草稿、Descriptor 索引、错误消息或 loading。

编辑页必须把规范化的 `create` 或 `edit:<application_id>` 作为唯一 load generation target，并监听 route name 与 application ID 的组合变化。每次上下文变化先关闭仅属于旧草稿的组件编辑、空间向导和整页预览会话，清空旧 Descriptor 索引，并建立全新的空草稿；创建路由到此结束，编辑路由再从唯一管理详情 API 加载目标聚合。应用成功、错误、finally 以及该次应用加载派生的 Descriptor 提交必须全部经过同一个 editor load coordinator；页面卸载只失效该 coordinator，不新增 mounted 标志、AbortController 兼容分支、缓存或第二条加载路线。

T1 使用可控 Promise 分别固定三种时序：A 的应用响应晚于 B、A 已提交但其 Descriptor 晚于 B、当前加载在页面卸载后返回。三种情况下旧提交都必须返回非当前，只有 B 可以写入应用与 Descriptor 状态；源码合同固定 `watch(..., { immediate: true })` 取代 `onMounted(load)`，并确认 unload、应用响应和 Descriptor 响应共享同一 generation。

实现已新增 `dataApplicationEditorRouteContext()`，并与 14.40–14.41 共用唯一 `commitLatestDataApplicationRequest()`：创建页规范为 `create`，编辑页按规范化 application ID 形成 `edit:<id>`；`DataApplicationEditor` 使用独立 load coordinator 监听 route name 与 ID，并在每次变化时销毁旧草稿的弹窗/预览会话、清空 Descriptor 索引和重建空草稿。应用响应、错误、finally 以及由该应用加载派生的 Descriptor 响应全部经同一提交守卫，卸载时失效该 generation；`onMounted(load)` 已删除。T1 先因缺少生产 helper 与路由生命周期合同出现两组红灯，补齐后以可控 Promise 证明 A 的应用迟到、A 的 Descriptor 迟到和卸载后三种提交均被拒绝。最终标准门禁结果继续合并记录于最新收口小节。

### 14.40 Data Application 编辑 mutation 上下文隔离（2026-09-06）

保存、发布、下线与组件删除都会改变 Data Application 聚合或当前草稿，异步提交必须继续属于发起操作时的编辑路由。若用户在请求期间从应用 A 切到 B、进入创建页或离开编辑器，A 的成功响应、错误、finally、成功消息、创建后导航和本地草稿删除都不得作用于 B；发布、下线与组件删除在确认框等待期间同样可能发生路由变化，确认返回后必须在 mutation 前再次校验上下文。

编辑器应维护独立于 load generation 的唯一 mutation coordinator，避免正常 mutation 无故取消同页的 Descriptor 加载。mutation target 由当前 `create | edit:<application_id>` 与 `save | publish | offline | remove-component:<component_id>` 组合而成；路由切换和页面卸载必须同时失效 load 与 mutation coordinator，并收拢 saving、publishing、offlining 状态。所有 mutation 的成功、错误和 finally 只通过同一个通用 editor request 提交守卫；发布、下线与组件删除在打开确认框前建立 request，确认后先通过该守卫，再执行 API 或本地草稿 mutation。服务端 mutation 执行期间整页交互由现有 loading mask 阻断，防止响应用服务器快照覆盖请求期间的新草稿编辑，不增加草稿锁实体、自动合并或兼容分支。

14.39 的 load 专用提交 helper 应同步收敛为通用 Data Application request 提交守卫，load 与 mutation 只传入各自规范化 context，不保留两个同语义实现。T1 使用可控 Promise 先发 A save、再切换并开始 B save，先释放 A 并确认其 success/finally 都不能覆盖 B 或结束 B 的 saving，再释放 B 并只提交 B；另以延迟确认验证发布/下线必须在确认返回后重新校验，源码合同固定三种 mutation 的成功、错误、finally 和创建后导航均位于当前 request 门禁内。

实现已新增独立 `editorMutationRequests` 与 `dataApplicationEditorMutationContext()`，保存、发布和下线的 loading、成功状态、错误消息、服务器聚合回写及 finally 清理全部通过通用提交守卫；组件删除也在确认前登记包含组件 ID 的 mutation context，取消被规范化为无副作用返回，确认迟到时不会修改下一应用的草稿。路由变化和页面卸载由唯一 `invalidateEditorContextRequests()` 同时失效 load 与 mutation generation。发布/下线在确认前建立 request，确认返回后先校验当前编辑上下文才调用 API；服务端 mutation 期间复用页面 loading mask 锁住草稿，避免服务器响应覆盖并发编辑。旧 load 专用 helper 已删除，没有保留兼容实现。T1 先得到缺少新 helper、coordinator 和 mutation 门禁的红灯，再以可控 Promise 证明 A 的迟到保存不能覆盖 B 或结束 B 的 saving，并证明发布确认与组件删除确认期间切换路由后均不会执行 mutation。最终统一 helper 名称与标准门禁结果继续记录于 14.41。

### 14.41 Data Application 列表加载与删除生命周期隔离（2026-09-06）

`DataApplicationList` 的翻页请求可能乱序返回，旧页成功、错误和 finally 不得覆盖当前页数据、消息或 loading；页面卸载后，列表请求也不得继续写状态。删除同样跨越确认框、DELETE 请求与删除后刷新三个异步边界：确认取消应作为正常无副作用结果，确认期间离开列表不得继续发送 DELETE，请求已经发出后离开页面则不得再弹消息或刷新已销毁页面。

列表应分别维护唯一 load coordinator 与 deletion coordinator。load target 使用规范化页码；每次加载的成功、错误和 finally 只在 request 仍匹配当前页时提交。deletion target 使用 `delete:<application_id>:<version>`，确认前建立 request，确认后同时校验 generation 与当前列表行仍为同一版本，再进入 deleting 状态和调用 API；成功、失败、finally 与删除后刷新均必须经过当前 deletion request 门禁。删除执行期间复用卡片 loading mask 阻断表格和分页交互；若删除当前非首页的最后一行，刷新前回退一页，避免停留在可预知的空页。页面卸载统一失效两个 coordinator，不增加 AbortController、mounted 标志、缓存或兼容分支。

14.40 的 `commitLatestDataApplicationEditorRequest()` 应同步提升为 Workbench Data Application 唯一 `commitLatestDataApplicationRequest()`，编辑页与列表页只传各自 context，不保留 editor/list 两个同语义提交 helper。T1 使用可控 Promise 固定旧页晚于新页、卸载后列表返回、确认期间卸载、DELETE 响应晚于卸载四种时序；源码合同固定确认取消、当前行版本复核、删除错误国际化、末页回退和两个 coordinator 的统一卸载失效。

实现已把编辑页 helper 收敛为唯一 `commitLatestDataApplicationRequest()`，列表页新增独立 `listLoadRequests` 与 `listDeletionRequests`，加载响应按当前页码提交，删除按 application ID 与 version 建立 target。删除确认使用既有取消归一化函数；确认后先复核当前列表行版本，再进入卡片级 loading 和调用 DELETE。成功、失败、finally、成功消息及删除后刷新均受 deletion generation 保护，卸载时由 `invalidateListRequests()` 同时失效两类请求；删除非首页最后一行会先回退一页。新增 `deleteFailed` 中英文业务词条，没有硬编码用户文本。T1 先因缺少通用 helper、列表 context、两个 coordinator 与国际化词条得到红灯，补齐后五项可控 Promise 行为测试和列表源码合同转绿。`make test-workbench-frontend` 已通过 67 项测试和 production build，仅保留既有的大 chunk 非阻断警告。

### 14.42 Spatial Exploration Wizard 异步会话隔离（2026-09-06）

空间探索创作向导存在三个独立异步来源：打开时加载 Consumer Catalog、选择汇总服务时加载 Descriptor、选择空间服务时加载 Descriptor。当前三者共用一个布尔 loading，任一请求先结束就可能提前解除遮罩；同一角色快速从服务 A 切换到 B 时，A 的迟到 Descriptor、错误或 finally 可能覆盖 B；关闭并重新打开弹窗或直接离开编辑页后，旧会话请求仍可能写入新向导状态和弹出错误消息。

向导应分别维护 catalog、aggregate descriptor、spatial descriptor 三个 latest-request coordinator，并使用独立 loading ref 合成为唯一页面 loading。每次打开先统一失效旧会话、清空草稿与角色状态，再建立新的 Catalog request；弹窗 close 与组件 unmount 复用同一个 invalidation 入口，同时失效三个 coordinator 并收拢 loading。角色选择变化时必须立即清除该角色旧 Descriptor 与依赖草稿，使用当前 service key 作为 target；成功、错误、finally 只通过既有 `commitLatestDataApplicationRequest()` 提交。汇总和空间请求相互独立，不得互相取消或提前结束对方 loading；应用按钮在任一新 Descriptor 尚未成为当前结果时继续由既有 `canApply` 失败关闭。

实现只消费当前 Catalog 中选中的 ServiceReference 和 Consumer Descriptor，不引入名称、字段或 Outdoor 假设。T1 使用可控 Promise 固定同角色 A/B 乱序、汇总与空间并发 loading、关闭后迟到 Catalog/Descriptor、关闭再打开后的旧 Catalog 四类时序；源码合同固定 dialog close 与 component unmount 共用唯一失效函数、三个 coordinator、三个 loading ref 和选择时先清空旧角色状态。删除现有重复的地图标签默认值赋值，不保留并行初始化路径。

实现已新增 `catalogRequests`、`aggregateDescriptorRequests`、`spatialDescriptorRequests` 三个独立 coordinator，以及三个独立 loading ref；页面遮罩只由三者的 computed 合并状态控制。`initialize()` 在建立新 Catalog request 前统一失效旧会话并清空旧服务目录，角色选择在请求 Descriptor 前先清除旧 Descriptor、依赖草稿与该角色 loading；即使新的 service key 无法在当前 Catalog 中解析，也会先推进该角色 generation，避免旧请求复活。Catalog 与两类 Descriptor 的成功、错误和 finally 全部通过既有 `commitLatestDataApplicationRequest()` 提交；dialog close 与 component unmount 复用 `invalidateWizardRequests()`。T1 先以源码合同和可控 Promise 得到红灯，再证明 A/B 乱序、双角色并发、关闭后迟到提交和关闭再打开四类场景均只保留当前会话。`make test-workbench-frontend` 已通过 72 项测试和 production build，仅保留既有的大 chunk 非阻断警告。

真实浏览器无持久副作用验收已在正式创建页完成：汇总角色强制快速从一个已发布 Query Service 切换到另一个后，最终只显示后者 Descriptor 派生的筛选字段、图表维度和度量；空间角色同样快速切换后只保留最终服务的名称字段、设色字段、提示字段和表格字段。空间 Descriptor 请求发起后立即关闭并重开向导，两个服务角色均恢复“请选择”，旧字段、错误消息与 loading 遮罩没有进入新会话；随后再次选择汇总与空间服务，两类表单均正常生成。全过程未填写、生成、保存或发布 Data Application，浏览器控制台无 warning/error。由于本地 Descriptor 请求返回很快，真实浏览器无法稳定观测请求重叠期间的中间遮罩时序；该确定性证据由本节的可控 Promise 双角色并发测试提供，浏览器验收不替代它。

### 14.43 Workbench 前端首屏依赖边界（2026-09-06）

生产构建当前把约 1.6 MB 的 JavaScript 放入入口 chunk。Source map 证据显示这不是 Chart 或 Map 异步组件失效：两种 renderer 已由 `defineAsyncComponent()` 独立加载；入口膨胀的根因是 `main.js` 全量安装 Element Plus，以及仅为合并 Map 国际化消息却从 `@common-ui-map` 聚合入口导入，后者把具有样式副作用的地图组件及 OpenLayers 依赖提升到首屏。`ol/ol.css` 也由应用入口无条件加载，使没有 Map 的列表、编辑和普通运行页承担空间渲染样式。

唯一优化路线为：沿用仓库现有 Vue 模块模式，通过 `unplugin-vue-components` 与 `ElementPlusResolver` 按模板引入 Element Plus 组件，删除全量 `app.use(ElementPlus)`；Map 国际化消息从明确的 JSON 子路径导入，不经过地图组件聚合入口；OpenLayers CSS 与现有异步 `GeoJSONResultRenderer` 在同一 loader 中按需加载。Chart 与 Map 仍由 `common-frontend` 单点维护，Workbench 只保留协议分派，不复制 renderer、不增加第二套入口或手写组件注册表。

生产构建增加 500 KiB 的入口 chunk 硬门禁，并由现有 `make test-workbench-frontend` 和 CI target 自动执行。该门禁检查真正的 Rollup entry，而不是通过 `manualChunks` 改名或单纯抬高 Vite warning 阈值；功能专属的异步 Chart/Map chunk 不计入首屏入口预算。T1 源码合同先固定按需注册、Map 消息深路径、OpenLayers CSS 延迟加载、依赖声明和入口预算插件；T2 由 production build 同时验证 Vue 模板解析、动态样式加载、chunk 图和实际入口字节数。

实现已删除 Workbench 的全量 `app.use(ElementPlus)`，采用仓库既有 `unplugin-vue-components@0.28.0` 与 `ElementPlusResolver` 路线；Map 中英文消息改为明确 JSON 子路径，`ol/ol.css` 与异步 Map renderer 同时加载。新增 Rollup `enforce-entry-chunk-budget` 插件，任何真实入口 chunk 超过 500 KiB 都会使现有 production build 失败。T1 源码合同按预期先因旧全量入口得到红灯，改造后转绿；带 source map 的 T2 构建把入口 JavaScript 从 1,595.63 KB / gzip 516.61 KB 降至 457.60 KB / gzip 165.36 KB，分别下降 71.3% 与 68.0%，并确认入口 source map 不再包含 OpenLayers 或 ECharts。Chart 518.42 KB 与 Map 510.76 KB 保持为仅在对应 renderer 出现时加载的功能 chunk，不使用无收益的 vendor 改名掩盖体积。

Workbench 前端重启后的真实浏览器验收覆盖 Console 应用列表、Data Application 创建页、空间探索向导和已发布空间应用。列表、表单、对话框与服务目录均正常渲染；已发布应用执行“查询全部组件”后显示 10 个地块、面积合计 0.1094、城市面积 Chart、五档专题 Map 和 10 条明细，DOM 中分别存在一个 Chart canvas 与一个 Map canvas。全过程未保存、发布、下线或删除应用，浏览器控制台无 warning/error，证明按需组件注册与两类 renderer 动态资源在正式 Console 路径下可运行。`npm ci --ignore-scripts` 与 `make test-workbench-frontend` 已通过，后者包含 73 项测试、production build 和入口预算门禁。

### 14.44 已发布应用首次查询语义（2026-09-06）

Workbench 的 Data Application 运行页是面向最终用户的消费入口，不应要求用户在每次打开一个已具备完整默认输入的正式应用后，再理解并点击一次“查询全部组件”。此前运行画布只复用了 Wallboard 的 Application Refresh Policy：非零刷新间隔会在 Descriptor 加载后立即查询，但 `desktop` 的刷新间隔固定为 `0`，因此已发布桌面应用首次打开仍为空白。这使正式运行页在首屏体验上退化为 Service 预览，而不是可直接使用的应用。

唯一运行语义调整为：`published` 运行页完成全部 Consumer Descriptor 加载后，如果至少存在一个 Component、全部 Component 均有可执行 Descriptor 且没有契约漂移，并且每个必填 Application Parameter 都有可按其绑定算子执行的默认值，则复用现有“查询全部组件”主路径执行且只执行一次首次查询。数字 `0`、布尔 `false` 等合法业务值不能被误判为空值；`is_null | is_not_null` 仍只有布尔 `true` 表示启用算子。任一必填默认值缺失或无效、Descriptor 临时加载失败、Component 不可执行或契约指纹变化时，首次查询失败关闭，不猜测参数、不静默刷新契约，也不建立第二条查询路径。

`draft-preview` 继续保持现有创作语义：普通桌面草稿由创作者显式查询，Wallboard 草稿仍按已确认的刷新策略执行首次查询。已发布 Wallboard 只执行上述同一次首次查询，查询结束后再启动下一轮刷新计时，不能先经发布首查再经刷新策略重复查询。用户手工修改 Application Parameter 后仍只清空受影响结果并等待显式查询；本节不把输入控件改成逐键自动查询。

该能力只改变浏览器会话内的运行生命周期，不新增后端字段、API、Revision 内容、Task、Schedule、缓存或查询结果持久化。T1 必须覆盖无参数应用、必填默认值、数字 `0`、布尔 `false`、空值、空数组、空值算子、Named Parameter、Descriptor 失败和契约漂移；源码合同必须固定发布首查与刷新计时的先后关系，避免未来重新产生重复首查。

实现已把必填默认值可执行性抽成运行时纯函数，并与隐藏参数区块复用同一判定，不再让展示校验和首次查询各自解释空值。发布画布在 Descriptor 全部加载完成后通过该准入门禁调用既有 `queryAll()`，完成后只调用 `scheduleAutomaticRefresh()`；草稿预览继续走原有 `refreshAndSchedule()`，因此已发布 Wallboard 不会重复首查。`make test-workbench-frontend` 已通过 76 项测试、production build 和 500 KiB 入口门禁。

真实浏览器直接打开已发布应用 `c847d823-3314-42a2-b2a7-d5139fc68283`，未点击任何查询按钮即按默认“已交付”条件显示 2 行 Business MySQL 订单表格并生成 1 个 Chart canvas；Gateway 在同一首次加载时只记录 2 个 Descriptor GET 与 2 个 Query POST，分别对应两个 Component，没有第二轮重复查询，浏览器控制台无 warning/error。验收只读取既有发布 Revision，没有保存、发布、下线或删除任何应用。

### 14.45 Data Application 字段呈现规则（2026-09-06）

Workbench 的正式运行入口必须比 Manager 数据预览和 Service 服务预览更贴近业务用户。Service Consumer Descriptor 只提供可信的字段名、类型和原始注释；它不应为每个数据应用决定“显示为什么业务标签、使用什么单位和精度”。这些是 Data Application Component 在具体应用中的发布事实，统一称为 **Field Presentation / 字段呈现规则**。

字段呈现规则位于 Table、Chart 和 Map 的 `renderer_config.field_presentations`，每项固定引用一个 renderer 已使用的输出字段，并只允许以下受控语义：

- `label`：所有标量字段均可声明，作为表头、图例、地图弹窗键名和主显示字段标签；
- `unit` 与 `precision`：只允许数值字段使用，精度为 `0..8`；
- `temporal_format`：只允许 `date | time | datetime`，且必须与 `date | time | timestamp` 输出类型匹配；
- `width`：只允许 Table 使用，范围为 `80..600` CSS px。

Table 的 `columns`、Chart 的 `dimension / measures`、Map 的 `label_field / tooltip_fields / style.field` 仍是字段身份与查询事实源。字段呈现规则只格式化 renderer 展示，不修改 rows，不改变选择联动、参数绑定或导出数据，不把标签当作字段名回传 Service。Value renderer 的 `items` 已以字段、标签、单位和精度表达同一职责，继续作为它的唯一配置，不再叠加第二份 `field_presentations`。

Backend 对重复字段、renderer 未使用字段、不匹配类型的格式、越界精度或列宽严格失败关闭。Frontend 不接受任意 formatter 名、函数或格式化代码；Table、Chart 和 Map 必须共享 `common-frontend` 内唯一的标签解析和标量格式化实现，Workbench 只负责根据 Descriptor 创作、保存并传入配置。未显式配置的字段使用 Descriptor `comment || name` 和原始值作为唯一默认行为，不引入域、服务 ID、业务字段名或 Outdoor 特例。

实现已把字段查找、Descriptor 标签回退、数值格式化和时间格式化收敛到 `common-frontend/basic` 的唯一纯函数。共享 Table 消费标签、列宽和单元格值；Chart 保留原始数值 series，在 tooltip 显示格式化后的值与单位，在度量轴标题集中显示单位而不向每个刻度重复追加，维度轴标题固定居中避免窄组件右侧截断；Map 只格式化弹窗主字段、属性键值和主题图图例。三者均不改写 Service rows，选择事件仍使用原始 `row_index`。

Component 编辑器已从当前 renderer 实际使用字段生成一组字段呈现配置，并将预览与保存收敛到同一个 `buildRendererConfig()` 编译器，删除了编辑器内的重复 renderer 组装逻辑。空间探索向导也通过同一同步函数生成普通 Component 配置，没有新增模板运行时、Outdoor 分支或样例字段。Backend 使用强类型结构和严格 JSON 解码校验上述约束，有效配置随草稿及不可变 Revision 直接保存。

本地确定性门禁已通过：`make test-common-frontend` 共 66 项；`make test-workbench-frontend` 共 77 项并通过 production build 与入口体积门禁；Workbench Backend `go test ./...` 全部通过；`make test-workbench-postgres` 使用允许的 `addp_test` 标准集成入口通过。

真实浏览器已经完成不持久化的前端验收：在既有空间应用编辑器中，Chart 成功配置业务显示名、单位与四位精度；Table 成功配置四个业务显示名、列宽及面积四位精度；整页预览查询真实服务后，Table 表头与数值格式、Chart 轴标题与数值刻度均按当前内存 Snapshot 生效，Map 和 Value 继续使用各自现有显式配置。目视验收发现并修正了 Chart 把单位重复拼到每个纵轴刻度、维度轴标题挤在右侧的问题；预览关闭、重开和热更新还暴露 ECharts 在零尺寸容器初始化及保留已销毁实例的生命周期警告，现统一等待容器可测量后初始化，销毁后立即清空实例引用。修复后重复关闭、立即重开和查询没有新增浏览器 warning/error。

全套服务按标准生命周期重启后，最后的持久化闭环也已完成。真实浏览器继续使用既有长期应用 `c4c0aa6e-70b1-49e8-8ade-8db92f5c6e33`，没有创建、删除或下线应用：Chart 把 `city`、`total_area` 发布为“城市”“耕地面积”，面积保留四位精度；Map 把 `NAME`、`City`、`SHAPE_Area`、`SmUserID` 发布为“区县”“城市”“地块面积”“用户标识”，面积保留四位精度、用户标识使用整数；Table 把 `SmID`、`NAME`、`City`、`SHAPE_Area` 发布为“地块编号”“区县”“城市”“地块面积”，同时保存整数或四位精度及 `120 / 160 / 140 / 160` px 列宽。以上字段仅是该应用 Snapshot 的真实验收配置，没有进入 Workbench 生产代码、默认值或共享 renderer。

草稿通过唯一更新 API 保存后，编辑器的未保存告警消失，并成功发布不可变 Revision 3。随后直接打开正式 `/data-apps/c4c0aa6e-70b1-49e8-8ade-8db92f5c6e33`，没有点击“查询全部组件”或任一组件“查询”：运行页已自动返回 10 个地块、面积合计 `0.1094`、各城市面积图表、五档连续设色地图和 10 条明细。地图图例及表格值均按四位精度展示，表头全部使用发布后的业务标签；页面存在两个非零尺寸 Canvas，浏览器 error/warn 日志为空。至此字段呈现规则已经完成“编辑—预览—保存—不可变发布—最终应用自动首查”的单一路线验收。

## 十五、概念设计状态

当前没有待确认的 Phase 0 概念问题。Phase 5 的 Selection Binding 同页联动、`desktop | wallboard` 展示模式、浏览器会话级全屏、Application Refresh Policy 和 Application Presentation Sections 已完成设计、实现、标准模块门禁与真实浏览器验收；Data Application 资产运营指标的事实源、模块归属以及 Asset 自有 `application` / 具体 Asset 运营分组也已完成运行态复核。外部 BI 的 owner 边界、消费契约、用户委托 OAuth 单一路线和 System 外部 OAuth Client 注册治理已经完成；首个真实 BI 验收载体仍为 Power Query 自定义 Connector 与 Power BI Desktop Import，但因当前缺少 Windows 宿主而暂缓。`common-python` 的产品无关 Service Consumer SDK、离线门禁及真实普通表、空间表和 Outdoor 多服务只读运行验收均已完成；它不替代 callback state、持久外部 Client 生命周期和真实 BI 产品端到端证据，因此正式 BI 接入指南继续保持未完成。

14.19 的 Data Application 直接创作收口、Outdoor 双服务真实验收、14.20 的 Business MySQL 本地异构验收，以及 14.21–14.23 的 Phase 6 场景化组合、空间探索创作向导和保存前整页预览均已完成。验收数据只作运行证据，没有进入 Workbench 领域模型、生产代码或默认配置。Phase 6 当前确认范围已经收口；真实数据量没有超过有界 GeoJSON 上限前不启动 Tile / OGC Features，也不继续堆叠 renderer。

14.24 的历史契约清理已经通过用户确认完成；Outdoor 长期应用已显式重绑当前 Service 24 契约并发布 Revision 3，Revision 2 保持不可变。14.26–14.27 的通用 Service Consumer SDK、公开导出、单元测试、README、发布门禁和真实运行验收已经完成。14.28 已在 MySQL Engine Provider 内补齐受限、精确、失败关闭的 QueryReadSet 与直接列 QueryOutputLineage，并完成 Business MySQL Service、Python SDK、契约漂移阻断、显式重绑、不可变 Revision 2 及最终 Data Application Table / Chart 的运行态验收；14.29 进一步完成最终应用对 Descriptor 临时失败和查询临时失败的可恢复状态收敛，契约变化仍严格阻断；14.30 完成 Component 编辑器的 Descriptor、查询和导出异步上下文隔离，服务切换或关闭后的迟到结果不再污染当前草稿；14.31 完成编辑器游标翻页的原子提交，失败请求不再产生旧数据与新页码混合的假状态；14.32 已完成运行画布按 Parameter Binding 精确失效旧参数请求的实现、标准前端门禁与发布 Revision 2 的真实浏览器验收；14.33 进一步把参数竞态收敛为可控 Promise 行为测试，不再只依赖浏览器时序和源码合同；14.34 已用同一 generation 和可控 Promise 阻断迟到导出的文件下载副作用；14.35 已为 Descriptor 初始加载与查询重试建立独立 latest-request generation，旧响应不再覆盖新状态；14.36 已把同源运行路由 A/B 快速切换的迟到成功、错误和 loading 收尾纳入同一 latest-request 提交门禁；14.37 在运行页卸载时立即失效 Revision 请求；14.38 使 Component 编辑器的弹窗关闭与页面卸载共享同一 Descriptor、查询和导出失效入口；14.39 进一步把创建页、编辑页 ID 切换、应用加载、Descriptor 派生加载和页面卸载收敛到唯一 editor route generation；14.40 又把保存、发布、下线从确认到响应收尾的副作用纳入独立 mutation generation；14.41 把列表翻页、删除确认、DELETE 响应和删除后刷新也收敛到同一 Data Application request 提交语义；14.42 进一步把空间探索向导的 Catalog、汇总 Descriptor、空间 Descriptor、关闭重开和卸载收敛到三个相互独立但共享同一提交语义的会话 generation；14.43 已把 Element Plus 全量注册与 Map 运行依赖移出 Workbench 首屏，并建立入口 chunk 硬预算和正式 Console 运行验收；14.44 已完成已发布应用在 Descriptor 与必填默认值均可执行时只复用一次“查询全部组件”主路径，草稿手工查询、参数提交和 Wallboard 后续刷新语义不变；14.45 已完成通用字段呈现契约、共享 renderer 实现、本地门禁，以及真实应用从编辑、预览、保存、不可变 Revision 3 发布到正式运行页自动首查的完整闭环。Power Query 路线继续保留；获得 Windows 宿主后再在 `service/connectors/power-query/` 完成具体 Connector 与真实 BI 门禁。没有真实宿主证据前不修改 OAuth 或 Service API，也不提前编写“可直接照做”的正式 BI 接入指南。当前唯一未闭合的同专题自动化证据是 `workbench-service-consumption` T4：具备专用 runner 后应优先补跑，本地验收不能替代该 Online Gate。不要修改 Service 查询路由、引入 API Key 私有授权、数据库直连或增加 Workbench / Python 代理。跨模块综合统计和 Workbench 运行埋点继续暂缓；只有确认成功打开次数、独立访问用户和 Revision 分布确有独立产品价值时，才进入 Workbench owner 运行准入事实设计。在独立价值确认前不进入多页面、`mobile`、页面轮播、通用动作、后台定时任务或第二套运行状态。若后续实现与现有公开契约冲突，必须先回到本专题及正式规范修订设计，不得增加兼容路由、兼容字段或 Workbench 私有旁路。

## 十六、相关文档

- `docs/concepts/addp核心概念关系图.md`
- `docs/concepts/addp术语表.md`
- `docs/concepts/addp模块架构图.md`
- `docs/spec/addp开发原则.md`
- `docs/spec/addp-API设计规范.md`
- `docs/spec/addp-Swagger集成指南.md`
- `common-frontend/CLAUDE.md`
- `common-frontend/README.md`
- `common-frontend/docs/ARCHITECTURE.md`
- `common-frontend/docs/addp前端风格设计规范.md`
- `common-frontend/map/README.md`
- `docs/concepts/addp企业资源目录体系图.md`
- `docs/spec/addp企业资源目录实现规范.md`
- `docs/next/ADDP企业资源目录能力专题.md`
- `docs/next/Outdoor业务数据治理推进方案.md`
- `docs/plan/数据资产模块群规划.md`
- `service/CLAUDE.md`
- `asset/CLAUDE.md`
- `portal/CLAUDE.md`
- `console/CLAUDE.md`

## 十七、专题维护规则

- 本文记录正在讨论和推进中的阶段事实，不替代稳定概念与规范；
- 概念确认后先更新术语表、核心概念和模块架构，再实现代码；
- API 确认后同步 API 规范、Swagger、Service 与 Workbench 调用方；
- 每完成一个阶段，记录真实验证命令与结果，不能只勾选代码完成；
- Outdoor 结论只能作为场景证据，不能升级为 Workbench 的平台级默认假设；
- 被新主路径替代的旧设计必须删除，不保留兼容分支。

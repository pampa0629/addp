# ADDP Workbench 数据服务消费与数据应用专题

状态：概念设计已确认；Phase 1 至 Phase 4B 已实现并完成标准门禁及真实浏览器生命周期验收；CatalogEntry、Asset `application` 组合、owner Resource Grant、Portal 打开链路，以及普通用户在授权前、生效后、撤销后的运行权限边界均已闭环。

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
  -> Workbench 动态查询、展示和保存视图
  -> 后续组合并发布 Data Application
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

Workbench 是 ADDP 面向数据消费者的工作空间，负责通过已发布数据服务进行动态查询、可视化、保存视图，并在后续阶段组合和发布数据应用。

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

创作端负责 Explore Session、Workbench View 和后续 Data Application 编辑发布。Console 外层 URL 是 iframe 模式的公开路由事实源，模块内使用同一 canonical path，并通过 `common-frontend` Console navigation bridge 同步；前端仍按模块规范支持 standalone 开发和验证，但正式入口不形成第二个认证 origin。

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
- 保存并复用 Workbench View；
- 后续的多视图联动和应用页面。

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
Workbench View
  -> Data Application
  -> desktop | wallboard 展示配置
```

`wallboard` 复用同一 Application Revision、Component、Parameter Binding、Selection Binding 和 Service 查询，只改变页面在当前浏览器视口中的布局行为。首段能力固定为视口自适应画布和用户主动全屏；不保存屏幕分辨率、缩放倍率或全屏状态，不复制一套大屏 Component 配置。

轮播、深色主题和专用大屏 renderer 仍属后续能力。`wallboard` 可以在发布快照中选择显式的浏览器前台刷新档位；它不进入 Orchestrator，高成本统计仍必须由上游提前计算并发布为服务。

## 五、领域模型

### 5.1 Explore Session

用户临时选择服务、输入参数和切换展示方式的前端状态，不持久化，不成为领域实体，也不写入查询结果。

### 5.2 Workbench View

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

Data Application 是后续阶段的独立聚合根，中文统一为“数据应用”。它可以从一个或多个 Workbench View 创建，但“是否包含多个 View”不是聚合边界；用户显式执行“创建数据应用”，开始拥有组合、发布和独立运行生命周期时，才建立 Data Application。

Workbench View 的边界保持不变：一个服务引用、一份查询模板和一个 renderer。页面、布局、共享参数、组件联动、展示模式、发布状态和 CatalogEntry 不得继续添加到 Workbench View。

创建 Data Application 时，把所选 View 的已校验配置复制为 Application 自己拥有的 Component 快照。每个 Component 至少持有：

- `service_ref` 和 `contract_fingerprint`；
- 参数定义、结构化查询模板和默认参数值；
- `renderer_type` 和严格类型化的 `renderer_config`；
- Application 内的组件标识与布局位置。

个人 Workbench View 不作为 Data Application 的运行时外键或配置事实源。因此：

- 后续修改或删除原 View 不影响已创建的 Data Application；
- 修改 Application Component 不回写个人 View；
- Data Application 运行时不读取 View 来拼装查询；
- Application Component 仍只保存 Service 消费配置，不保存查询结果、凭据或服务 URL。

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

Data Application 使用独立于 Workbench View 的唯一 API：

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

创建请求只提交名称、说明和一个或多个 `source_view_ids`。Workbench 在当前 Tenant 与当前 owner User 范围内读取来源 View、重新读取各 Service Consumer Descriptor 校验契约，然后复制为 Component 快照；`source_view_ids` 不进入 Data Application 表、Revision 或运行响应。

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

外部 BI 是 Service 的另一类客户端，不是 Workbench 插件、Data Application 运行模式或新的数据服务类型。它与 Workbench 共用 Service owner 的消费控制面和执行面，但不读取 Workbench View、Application Revision、renderer 配置或 Asset 私有表：

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

`service_name` 仅是人可读名称和部分协议路径标识，不是 Workbench View 的身份引用。服务删除后重新创建同名服务必须产生新 `service_id`，旧 View 继续显示原服务不可用，不得自动改绑。

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

第一阶段明确不增加服务级命名参数，Consumer Descriptor 不返回 `named_parameters`，现有唯一 `QueryExecutionRequest` 不增加 `parameters`。Outdoor 第一批场景使用已物化结果上的字段筛选；Top 10 属于上游固定计算口径，不在 Workbench 查看时重新传入 `top_n`。

后续只有在出现无法表达为输出字段筛选的真实用例后，才修订本专题和 Service 执行契约。届时必须扩展现有唯一执行请求，不增加第二个执行端点、字符串替换或 Workbench 私有参数语法。

### 7.3 参数选项

参数候选值只能来自：

- Consumer Descriptor 中的固定枚举；
- Service 明确提供的有界选项查询；
- 另一个正式发布的数据服务；
- 用户手工输入。

Workbench 不对底层字段执行无界 `SELECT DISTINCT`，不直接查询来源表，也不把样例值当成完整候选集。

### 7.4 保存与运行

Workbench View 保存参数定义、查询模板和默认值。运行时输入默认只存在当前页面状态，不自动覆盖 View；用户显式保存后才更新聚合根版本。

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

本结构是第一阶段的完整输入契约，不使用空 `named_parameters` 作为未来能力占位。

## 八、渲染体系

第一阶段 renderer 固定为：

| Renderer | 开放条件 |
| --- | --- |
| `table` | 输出契约为表或等价字段集合 |
| `chart` | 输出包含可作为维度和指标的标量字段 |
| `map` | 输出契约声明空间能力和明确空间字段 |

renderer 只能消费输出契约，不根据业务名称猜测角色。第一阶段允许用户显式选择维度、指标、排序和空间字段，不自动生成业务解释。

### 8.1 `common-frontend` 与 Workbench 边界

仓库已经按重依赖把 `common-frontend` 拆分为 `basic / map / graph / dag` 等子包。Workbench renderer 必须沿用该边界，不新建同时捆绑 Element Plus、ECharts 和 OpenLayers 的大型 `analytics` 包。

| 能力 | 放置位置 | 组件 | 边界 |
| --- | --- | --- | --- |
| 表格结果 | `common-frontend/basic` | `TabularResultRenderer` | 渲染当前页字段和行并发出当前结果选择事件；cursor 仍由宿主维护 |
| 图表结果 | 新建 `common-frontend/chart` | `ChartRenderer` | 使用 ECharts 渲染已完整的有界表格结果 |
| 空间结果 | `common-frontend/map` | `GeoJSONResultRenderer` | 复用 `MapContainer`、OpenLayers、CRS registry 和底图 profile |
| 渲染编排 | `workbench/frontend` | `WorkbenchRendererHost` | 选择 renderer、适配 Service 结果、判断完整性并处理 cursor |

`common-frontend/chart` 使用技术栈规约的 `echarts@5.5.1` peer dependency，由消费模块自行声明依赖。地图继续使用 `ol@9.2.4`。不使用与平台技术栈不一致的版本作为 Workbench 基线。

共享 renderer primitive：

- 只接收已归一化数据、字段事实和展示配置；
- 不发起 HTTP 请求，不读取 Token，不识别 ServiceReference、Consumer Descriptor 或 Workbench View；
- 通过事件返回排序、分页、选择、bbox 和地图视角等交互意图；
- 只使用 ADDP 主题变量和导出的双语 i18n 消息，不持久化业务状态。

现有 `TablePreview` 和 `GeoJsonPreview` 继续表示 Manager 式数据预览：它们消费预览响应结构并包含原始内容展示，不改名或扩张为 Workbench 结果 renderer。新增组件不保留另一个 Workbench 私有同功能实现。

Workbench View 中的 `renderer_type` 是 `renderer_config` 的可辨识标签。Backend 必须使用三个具体 DTO 严格解码并拒绝未知字段，不使用无约束 `map[string]interface{}`。不为 renderer config 另增一个与 Workbench View `version` 并行的版本字段。

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
  "base_map_profile_id": "osm"
}
```

- `geometry_field` 必须是 Descriptor 空间契约明确声明的 geometry 字段，不默认为 `geom` 或 `geometry`；
- `label_field` 可选，`tooltip_fields` 只能引用输出契约字段；
- `base_map_profile_id` 可省略；省略时使用平台当前默认 profile，显式值只能引用平台已批准底图 profile，不保存 URL、Key 或颜色值；
- 已保存 profile 失效时显式报告不可用，不自动改用另一底图；
- 地图根据 Descriptor 的 SpatialInfo 和 CRS definition 进行展示转换，不把 GeoJSON 坐标无条件当作 WGS84；
- 第一阶段只消费 Query Service 的 GeoJSON FeatureCollection，最多 1000 个 Feature；`page.has_more=true` 时不把局部 Feature 当作完整地图；
- Tile Service、分级样式、专题图、热力图、聚合点、三维和任意样式 DSL 均不进入第一阶段。

### 8.5 结果完整性原则

Table 是 cursor 分页浏览器，可以明确显示当前页。Chart 和 Map 表达对一个结果集的整体解释，因此不得静默渲染第一页：

```text
page.has_more = false
∩ result_size <= renderer_limit
∩ contract_fingerprint 一致
= 允许 Chart / Map 渲染
```

后续 Graph、媒体、三维和大屏 renderer 必须在存在真实服务输出契约和内容读取能力后再增加。不建设空泛 renderer 注册框架、插件 DSL 或 Workbench 私有备用组件。

### 8.6 有限导出

第一阶段的有限导出是 Service 查询结果的一种有界输出格式，不是 Workbench 资源、后台任务或新的执行链路：

```text
Workbench View 当前结构化查询
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
6. 导出不创建或修改 Workbench View，不保存结果副本，也不改变 `contract_fingerprint`；
7. 导出与普通查询使用相同的 `service.data_read.execute` 和 Service Resource Grant / Policy，不增加第一阶段 `service.data_read.export`。

单独限制 CSV 或 GeoJSON 不能构成真实的数据防泄漏边界，因为拥有查询权限的用户仍可读取相同 JSON 数据并自行转换格式。只有未来的正式批量导出提供更高上限、自动翻页、异步文件生成、对象存储交付或其他新增数据能力时，才需要定义独立的 Service 导出 Permission；该能力归入 Transfer/任务执行体系，不扩张 Workbench View。

导出审计由 Service owner 负责。Service 查询执行面必须区分普通查询和显式导出，成功与拒绝都记录结构化审计事件，至少包含：

- Principal、Tenant 和 Request ID；
- `service_type`、`service_id` 和服务版本；
- 输出格式、返回数量、`has_more` 和执行结果；
- 排除字面值的 `query_shape_fingerprint`；
- 稳定错误码或拒绝原因。

审计不得记录筛选字面值、参数值、返回行、空间要素、SQL、cursor、Token 或原始请求 Body。当前 Service 管理面审计中间件没有覆盖公开查询执行路由，实施 Consumer API 和执行权限时必须同步补齐 Service 执行面审计，不能由 Workbench 伪造 owner 审计事件。

普通查询与显式导出继续使用同一个 Query Service operation。客户端通过 Descriptor 声明的可选请求头 `X-ADDP-Query-Intent: query | export` 表达用途，缺省为 `query`；该头不授予额外能力，只用于 owner 审计事件分类。Gateway 共享 CORS 必须允许该请求头，并暴露 `X-ADDP-Has-More`、`X-ADDP-Next-Cursor`、`X-ADDP-Service-Version` 和 Request ID 等有界响应事实。CSV 与 GeoJSON 必须返回相同的分页和服务版本响应头。

## 九、权限与身份

### 9.1 用户交互

浏览器使用当前 User Bearer。有效数据访问至少是以下范围交集：

```text
当前用户 Permission
∩ Workbench View owner_user_id
∩ Service Resource Grant / Policy
∩ 服务自身访问策略
```

Workbench 的 View Permission 只能控制视图配置，不能授予数据服务访问权。第一阶段 Permission 固定为：

```text
workbench.view.create
workbench.view.read
workbench.view.update
workbench.view.delete
```

Phase 4A 增加 Data Application 自身权限：

```text
workbench.data_application.create
workbench.data_application.read
workbench.data_application.update
workbench.data_application.delete
workbench.data_application.publish
workbench.data_application.execute
```

`read | update | delete | publish | execute` 在 Phase 4A 都同时匹配当前 Tenant 与 `owner_user_id`；`offline` 复用 `publish`，不增加第二个生命周期 Permission。Data Application Permission 只控制应用配置和运行入口，任何 Component 的真实查询仍由 Service 使用当前 User Bearer 执行最终授权。

| 操作 | Workbench Permission | owner 条件 | Service 校验 |
| --- | --- | --- | --- |
| 创建 View | `workbench.view.create` | `owner_user_id` 由当前 User 生成 | 当前 User 可读 Descriptor |
| 列表/读取 View | `workbench.view.read` | 必须匹配当前 User | 不依赖 Service 可达 |
| 更新 View | `workbench.view.update` | 必须匹配当前 User | 重新读取 Descriptor 并校验 |
| 删除 View | `workbench.view.delete` | 必须匹配当前 User | 不访问 Service |
| 枚举可用服务 | `service.data_read.execute` | Service 资源策略 | Service Consumer Catalog 最终判断 |
| 执行查询 | `service.data_read.execute` | Service 资源策略 | Service operation 最终判断 |
| 有限导出 | `service.data_read.execute` | Service 资源策略 | 同一 Service operation、单次有界响应与执行面审计 |

第一阶段 Workbench 只支持已认证用户，不提供匿名 Workbench 运行模式。公开服务仍可在 Workbench 之外按 Service 公开协议匿名访问，两者不构成第二条 Workbench 身份路线。

运行时固定分两次独立判断：

1. 读取 Workbench View 时校验 `workbench.view.read` 和 `owner_user_id`；
2. 枚举 Descriptor 或执行查询时，由 Service 实时校验 `service.data_read.execute` 和服务资源策略。

已保存 View 不因服务授权失效而删除，但查询必须阻断并显示重新申请或联系所有者的入口。

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

Workbench View 每次加载时比较保存的 `contract_fingerprint` 与 Service 当前契约：

1. 指纹一致，正常运行；
2. 指纹变化，停止自动查询；
3. 显示字段、类型、参数、操作符和空间能力差异；
4. 由 View 所有者显式修订并保存；
5. 不自动删除字段，不按名称猜测映射，不自动改绑同名服务。

普通数据内容刷新不能改变契约指纹。因此周期性重算并替换固定结果表时，Workbench View 无需重新发布。

服务下线或删除时保留 Workbench View 及原始 `service_ref`，显示不可用状态；不得自动切换到同名或相似服务。

## 十一、Asset 与 Portal 衔接

第一阶段 Workbench View 是个人配置，不作为 Asset。

后续 Data Application 发布后：

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

Portal 只展示资产和打开入口，不保存 Workbench 页面、View、组件、参数或服务凭据。

CatalogEntry 标识 Data Application 聚合根，不标识单个发布 Revision。Portal 打开应用时由 Workbench 解析当前有效发布 Revision；已发布 Revision 不因个人 View 或新草稿变化而改变。

## 十二、阶段计划

### Phase 0：概念与专题确认

- [x] 确认模块名 `workbench`；
- [x] 确认平台级通用定位，不绑定 Outdoor；
- [x] 确认 Service-only 数据入口；
- [x] 确认 Workbench View 是第一阶段聚合根；
- [x] 确认动态查询进入第一阶段；
- [x] 确认 BI 是轻量消费能力，大屏是后续应用展示模式；
- [x] 确认 Consumer Descriptor 稳定术语与 `addp.service_consumer/v1` 协议版本；
- [x] 确认 Service 只声明输入/输出契约，不声明 Workbench renderer；
- [x] 确认 Service 拥有 Consumer Catalog，且列表只返回当前可执行服务；
- [x] 确认 ServiceReference 使用 `service_type + 正整数 service_id` 和唯一详情 URL；
- [x] 确认第一阶段 Workbench View 只允许个人所有和个人可见；
- [x] 确认 Query Service 第一阶段只开放字段筛选，不增加服务级命名参数；
- [x] 确认第一阶段 Workbench View 唯一 CRUD API、个人 owner 边界和权限矩阵；
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
- [x] 实现 Workbench View CRUD 和乐观并发；
- [x] 接入 Console iframe 唯一创作入口和 canonical navigation bridge；
- [x] 登记 Makefile、测试脚本、Swagger、构建矩阵、Frontend CI 和 PostgreSQL T2 门禁；
- [ ] 以 Outdoor 和 Business MySQL 真实服务完成 Online 验收（进入 Phase 3，不在 Phase 2 建立样例旁路）。

### Phase 3：跨领域真实验收

- [ ] Outdoor 作为首个真实场景验证固定结果刷新、参数查询和视图保存；
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
- [x] 验证个人 View 修改或删除不会改变已创建或已发布的 Data Application；

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
- [ ] 外部 BI 消费服务的契约与接入指南；
- [x] 实现外部 OAuth Client 注册治理；
- [ ] 以真实 BI Connector 完成端到端验收；
- [x] 完成 5.8 节 Data Application 资产运营指标事实源与模块归属评估；
- [x] 在 Asset 自有事实范围内实现 `application` 类型及具体 Asset 的运营分组；

Workbench 不因为 Phase 5 增强而取得数据建模、SQL、指标定义或任务计算职责。

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

已实现 `workbench-service-consumption` T4 suite 及专用 `business/scripts/online-workbench-mysql-fixture.sh`：Fixture 不读取或生成 `business/.env`，使用仓库外变量启动确定性 Business MySQL，并为永久 Engine Instance 准备仅有 `SELECT` 的账号。suite 经 Gateway 走 Service 输出契约检测、临时 Query Service 发布、Consumer Descriptor、Workbench View、真实 cursor/筛选/CSV 查询和契约指纹变化主路径，退出时按本轮 ID 删除 View 与 Service。

该 suite 同时通过 Console 的真实登录与 iframe 模块入口打开保存的 View，并核对浏览器 AuthContext 与 API User 为同一身份。浏览器实际提交动态状态参数，验证 Table 返回两行、Chart 生成 canvas、非空间契约不提供 Map；随后更新 Query Service 公开字段策略并刷新同一 View，必须出现契约变化告警且查询按钮被禁用。浏览器只消费正式 API 和页面，不增加 Workbench 私有执行路由或测试旁路。

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

1. `make test-online-runner` 已在当前工作树通过 84 项确定性测试，证明 Online 分发、预检、Host Gate、Fixture 安全边界、失败清理和 Workbench suite 协议可执行；这不等于真实 T4 通过。若目标是完成 Phase 3 验收，仍须在已登记的专用 Online Runner 执行 `workbench-service-consumption` T4 suite，取得 Business MySQL 的真实动态参数、Chart canvas、CSV、契约变化阻断和执行审计证据；成功前不勾选其余 Phase 3 验收项。
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

当前唯一缺失的是 Workbench owner 持久化的“成功运行准入”事实。Runtime API 成功返回可以证明当前 User 在当时获准读取某个已发布 Revision，但现有实现只返回快照，不写使用事实。Component 查询随后由浏览器直接调用 Service，Service 无法可信获知它来自哪个 Data Application；同一 Query Service 也可被 Workbench View、其他 Data Application 或外部客户端复用。因此本轮明确不从 Service Audit、Gateway 日志、Referer、Portal 点击或浏览器自报请求头拼接 Data Application 访问量，也不把有效授权人数命名为活跃用户。

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

System IAM 的“外部 OAuth 客户端”页已接入统一 IAM 工作台及中英文 i18n，支持搜索、状态筛选、创建、编辑、复制 Client ID、停用和恢复。Swagger、授权 Manifest、生成常量、migration 111、后端/前端测试与现有 CI 自动发现入口同步完成。确定性 `go test ./...`、`make test-system-frontend`（10 个文件、41 个测试及生产构建）、`make test-authorization`，以及标准 `test-system-iam-postgres` 的 IAM、OAuth、API 和 migration PostgreSQL 门禁均已通过；运行态页面验收仍需在 System 应用 migration 111 后完成。真实 BI Connector 和正式接入指南仍是后续工作，不因管理控制面完成而提前标记通过。

## 十五、概念设计状态

当前没有待确认的 Phase 0 概念问题。Phase 5 的 Selection Binding 同页联动、`desktop | wallboard` 展示模式、浏览器会话级全屏、Application Refresh Policy 和 Application Presentation Sections 已完成设计、实现、标准模块门禁与真实浏览器验收；Data Application 资产运营指标的事实源、模块归属以及 Asset 自有 `application` / 具体 Asset 运营分组也已完成运行态复核。外部 BI 的 owner 边界、消费契约、用户委托 OAuth 单一路线和 System 外部 OAuth Client 注册治理已经完成；运行态 UI 验收、真实 BI Connector 端到端验证与正式接入指南尚未完成。

下一步建议先应用 System migration 111 并完成 IAM 页面运行态验收，再选择一个真实 BI Connector 验证 OAuth、Consumer Catalog、Descriptor、cursor、Token 刷新、权限撤销和契约变化，最后产出接入指南。不要修改 Service 查询路由、引入 API Key 私有授权、复用内置 Client 或增加 Workbench 代理。跨模块综合统计和 Workbench 运行埋点继续暂缓；只有确认成功打开次数、独立访问用户和 Revision 分布确有独立产品价值时，才进入 Workbench owner 运行准入事实设计。在独立价值确认前不进入多页面、`mobile`、页面轮播、通用动作、后台定时任务或第二套运行状态。若后续实现与现有公开契约冲突，必须先回到本专题及正式规范修订设计，不得增加兼容路由、兼容字段或 Workbench 私有旁路。

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

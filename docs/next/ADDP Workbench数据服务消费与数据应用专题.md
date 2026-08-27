# ADDP Workbench 数据服务消费与数据应用专题

状态：概念设计已确认；Phase 1 的 Service 消费契约与执行权限、Phase 2 的 Workbench 最小模块、共享 renderer、Workbench View 和平台登记已实现；细粒度 Resource Scope Binding 待 owner 资源授权模型形成后收敛，真实跨领域 Online 验收进入 Phase 3。

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
  -> desktop | mobile | wallboard 展示配置
```

`wallboard` 后续可以拥有固定画布比例、全屏、轮播、自动刷新、深色主题和大屏组件，但仍调用同一 Service。浏览器定时刷新不进入 Orchestrator；高成本统计必须由上游提前计算并发布为服务。

大屏能力不进入第一阶段。

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
- `desktop | mobile | wallboard` 展示配置；
- 草稿、发布、下线和版本生命周期；
- 稳定运行入口以及与 CatalogEntry 的发布衔接。

草稿由创建者维护。发布产生不可变的 Application Revision；后续编辑基于新草稿形成新 Revision，不能原地修改已发布版本。首次发布为 Data Application 建立稳定 CatalogEntry，后续 Revision 沿用同一 Data Application 与 CatalogEntry 身份，不为每个版本创建平行目录项。

Data Application 可以只有一个 Component。是否成为应用取决于显式的组合和发布意图，而不是组件数量。

Data Application 不授予底层数据访问权。运行时每个 Component 都使用当前访问者身份调用其 Service，由 Service 实时执行 Permission、Resource Grant / Policy 和契约校验；Application 发布、CatalogEntry 或 Asset 授权都不能替代 Service 最终授权。

Data Application 尚不进入第一阶段数据库和 API。第一阶段不得建立空壳表、占位接口或与 View 并行的保存路径。

发布后的 Data Application 先由 Catalog 建立企业 CatalogEntry，再由 Asset 通过 `AssetComponent.catalog_entry_id` 组合为 `application` 类型资产。Asset 只保存资产组合、发布和授权履约事实，不保存应用页面、组件树或运行配置。

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
| 表格结果 | `common-frontend/basic` | `TabularResultRenderer` | 渲染字段和行，发出排序、翻页和行选择事件 |
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
- API Key 只用于外部应用或无实时用户参与的调用，不作为 Workbench 内部主路径。

### 9.3 Asset 授权

Asset 负责申请、审批和履约，Service 或 Workbench 作为资源 owner 负责最终授权判断。接入 Portal 前必须与企业 CatalogEntry 与 `AssetComponent.catalog_entry_id` 的唯一来源链路一致，不能恢复通用 Owner ResourceRef、软授权、专属 Token 或 owner 实时查询 Asset ACL 的旧路线。

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
- [ ] 验证 Workbench 代码和配置中不存在领域字段硬编码。

Outdoor 验收通过不等于通用能力通过。Phase 3 至少需要第二个不同领域服务，避免样例偶然适配被误判为平台能力。

### Phase 4：Data Application、Catalog、Asset 与 Portal

- [ ] 实现 Data Application 聚合根、Component 配置快照、页面和布局；
- [ ] 实现应用级参数与 Component 参数的显式绑定；
- [ ] 实现草稿、不可变发布 Revision、下线和稳定运行入口；
- [ ] 增加同 origin `/data-apps/:application_id` 顶层运行端，不保留第二条 iframe 运行 URL；
- [ ] 将 Workbench Data Application 作为专业资源接入 CatalogEntry；
- [ ] 一次性启用并收敛 Asset `application` 类型的 CatalogEntry 组合路线；
- [ ] 接入 owner Resource Grant、Asset 履约和 Portal 打开入口；
- [ ] 验证个人 View 修改或删除不会改变已创建或已发布的 Data Application；
- [ ] 删除旧的手工应用链接、软授权和专属 Token 设想。

### Phase 5：BI 深化与大屏

- [ ] 视图联动和受控下钻；
- [ ] 多页面布局；
- [ ] `desktop | mobile | wallboard` 展示配置；
- [ ] 全屏、轮播和刷新策略；
- [ ] 外部 BI 消费服务的契约与接入指南；
- [ ] 评估正式 Data Application 资产运营指标。

Workbench 不因为 Phase 5 增强而取得数据建模、SQL、指标定义或任务计算职责。

## 十三、第一阶段明确延期

- Graph、Tile、Registered Service 适配；
- 多服务 JOIN；
- 自由 SQL 和计算字段语言；
- Data Application 和多页面；
- 大屏；
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

1. 若目标是完成 Phase 3 验收，优先在已登记的专用 Online Runner 执行 `workbench-service-consumption` T4 suite，取得 Business MySQL 的真实动态参数、Chart canvas、CSV、契约变化阻断和执行审计证据；成功前不勾选 Phase 3。
2. 若只在当前本地环境继续人工体验，`f2` 和 `p3` 均没有发布筛选字段且结果超过单页上限，只能验证 Table 与 cursor；Chart、Map 和有限导出被完整性规则阻断是正确行为。需要动态参数或完整 Chart/Map 时，应通过 Service 正式发布一个有 `filterable_fields`、可收敛为完整有界结果的服务，不能直接改数据库制造契约。
3. Phase 1 仍有 Service owner Resource Grant / Policy 收敛项；Phase 4 的 Data Application、CatalogEntry、Asset 与 Portal 集成尚未开始，不应混入 Phase 2 View 修补。
4. 当前最小充分门禁为 `go test ./...`（Service、Workbench Backend）、`make test-workbench-frontend`、对应 PostgreSQL 标准门禁和 `git diff --check`；涉及 Console、共享 renderer、CI 或 Swagger 时必须同步运行本专题十四章列出的扩展门禁。

## 十五、概念设计状态

当前没有待确认的 Phase 0 概念问题。进入实现后若 Service 现有公开执行路由、授权模型或 Query Service 输出契约与本文冲突，必须先回到本专题及正式规范修订设计，不得增加兼容路由、兼容字段或 Workbench 私有旁路。

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
- `docs/concepts/addp企业数据目录体系图.md`
- `docs/spec/addp企业数据目录实现规范.md`
- `docs/next/ADDP企业数据目录能力专题.md`
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

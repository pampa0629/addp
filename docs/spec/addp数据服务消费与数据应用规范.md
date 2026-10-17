# ADDP 数据服务消费与数据应用规范

本文维护已实现的 Service 消费契约和 Workbench 数据应用边界。阶段记录、真实 BI Connector 与未闭合验收见[后续专题](../next/ADDP%20Workbench数据服务消费与数据应用专题.md)。

## 一、消费范围

ADDP 通过 Service、Workbench、Catalog、Asset 和 Portal 串联数据服务发布、数据应用创作与用户侧消费：

- Service 定义并发布数据服务，向消费者提供类型化输入、输出和执行契约；发布者预览用于验证服务定义。
- Workbench 保存可复用的数据应用配置，负责查询、筛选、可视化、组件联动、发布修订和稳定运行入口。
- Catalog 为已发布应用建立企业目录身份；Asset 负责资产组合、发布与授权履约，Portal 负责发现并打开应用。
- System 管理用户身份、OAuth Client 和 API Consumer Credential。浏览器应用运行使用当前 User Bearer；外部系统消费已发布数据面 API 使用 API Consumer Credential，两者遵循各自认证与授权契约。

数据应用的职责链路为：

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
- 用户身份、OAuth Client 和 API Consumer Credential，归 System。

Workbench 不是 TaskProvider。用户查看、筛选和刷新数据属于在线服务消费，不进入任务编排。昂贵或周期性计算必须由上游任务提前完成，Workbench 只读取发布后的稳定服务。

### 2.1 创作端与运行端

Workbench 由同一 owner 承担创作和运行职责，但两个页面职责使用不同的唯一入口：

| 界面 | 正式入口 | 集成方式 |
| --- | --- | --- |
| Workbench 创作端 | `/workbench/...` | Console iframe 模块 |
| Data Application 运行端 | `/data-apps/:application_id` | Console 当前 origin 下的独立顶层页面 |

创作端只维护 Data Application：选择服务后直接配置 Component 的字段、参数和 renderer，再完成布局、共享参数、选择联动和发布。Console 外层 URL 是 iframe 模式的公开路由事实源，模块内使用同一 canonical path，并通过 `common-frontend` Console navigation bridge 同步；前端仍按模块规范支持 standalone 开发和验证，但正式入口不形成第二个认证 origin。

创作界面以应用画布和选中组件的属性为主入口。画布复用唯一 `DataApplicationCanvas` 与 renderer；编辑时点击只选择组件，查询条件和选择联动通过显式整页试用验证。标题、说明、位置和尺寸直接修改同一草稿；布局以组件顺序、占宽和高度重新排布，避免重叠，不要求填写 X/Y。筛选默认值、参数映射、选择联动、场景预设与页面设置按需打开，保存和发布仍是显式动作。

筛选设置以条件卡片展示名称、初始值和受影响组件，映射按组件归组；共享条件只允许 Descriptor 类型、可选值及已有选择联动一致的组合，不按名称自动合并。选择联动先在局部表单显式选择来源组件、结果字段及目标条件，再应用到同一草稿；应用前展示由参数绑定推导的刷新范围，取消不修改草稿。字段显示别名同时保留字段名供辨认，传递的仍是原始值。

整页预览提供显式“设为应用初始值”动作：将当前全部 Application Parameter 的原始值经完整性、必填项、Descriptor 与可选值检查后复制回草稿 default_value，并返回编辑页；保存草稿才持久化。普通关闭预览仍丢弃试用状态。该动作不回写场景预设、结果行、分页、地图视角或已发布 Revision，也不增加参数查询或持久化接口。

空应用从选择首个展示类型开始，组件配置按“数据来源、展示、查询条件”逐步完成；字段事实与必要输入来自 Service Consumer Descriptor，用户显式选择业务字段和默认值，不依据名称自动推断共享参数或联动。空间探索入口继续复用现有空草稿向导。两类入口都生成同一 Component 与 Snapshot，不新增 Template、持久化创作模式、Backend API 或第二套运行查询。编辑画布首次按可执行默认值加载数据；修改查询配置后清除旧结果，用户显式刷新，纯展示及布局修改不重复查询。

新增 Component 的查询条件可显式复用已有 Application Parameter，默认仍创建独立条件。复用选择仅驻留组件编辑表单，试查使用所选应用条件的初始值；应用组件配置时统一写入既有 Parameter Binding，不生成重复条件，不改写已有默认值或场景。候选条件按加入该组件后的完整绑定关系复用类型、选项与选择联动校验，不能只比较标签。修改来源服务或字段/操作符后清除相应复用选择，取消表单不改动应用。已有组件继续通过应用筛选设置修改共享关系。

新增组件选择 Service 后可提供至多四种展示起点：明细表、类别对比柱图、时间趋势折线图、空间分布。候选只采用 Descriptor 中同时可选择且声明输出的字段，优先使用默认选择顺序；柱图使用非数值标量维度与数值度量，折线图使用稳定排序键中的日期/时间字段，地图使用显式 primary geometry。界面显示实际字段组合，由用户点击后填入同一组件表单，不进行聚合、业务角色推断或参数自动合并；数字卡片无法仅凭字段类型证明唯一汇总行，仍由用户手动配置。必填条件齐全时点击方案复用现有查询预览，否则进入条件步骤等待填写；服务切换清空旧方案和结果，不生成样例数据、Template 或持久化方案字段。

复制 Component 复用同一组件编辑器与新增提交路径：以原组件的服务、查询与展示配置初始化新的组件 ID，默认沿用原有 Parameter Binding 并使用应用条件初始值，直接打开条件步骤，允许显式切换已有条件或创建独立条件。确认前不写草稿，取消即丢弃；确认后保留原卡片宽高并排在原组件之后，统一重排避免重叠。复制不生成新的 Selection Binding；已有条件作为联动目标时，副本按既有 Parameter Binding 自然参与更新。契约变化仍须显式重新配置，不能通过复制绕过校验。

已发布 Data Application 的运行端不嵌入 Console iframe，不显示 Console 管理导航，供 Portal 打开、全屏运行和后续 wallboard 使用。稳定 URL `/data-apps/:application_id` 由 Workbench 解析当前有效发布 Revision；同一页面不存在另一条 `/workbench/...` 运行 URL。

两个界面仍属于同一个 `workbench` 模块，复用同一 Backend、领域模型、renderer 和 Browser AuthSession，不新增 `data-app` 模块、数据库 schema、Service Principal 或独立认证体系。运行端使用当前 User Bearer 调用 Workbench 和 Service，不使用 API Key。

System 的 OAuth Client 与 API Consumer 分别登记 OAuth 客户端和外部数据面调用方，不是 Data Application 注册表。Data Application 不自动创建 OAuth Client、API Consumer 或 API Consumer Credential。System 模块运行注册表只登记 `workbench` 模块运行实例，不为每个 Data Application 注册模块；Data Application 的企业发现身份仍由 CatalogEntry 承担。

## 三、通用业务边界

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

### 3.1 异构验收服务的范围

第二个验收场景固定为 Business MySQL 电商订单分析 Query Service，验收服务名为 `commerce-order-analysis`。仓库已有 `business` MySQL 的 `customers`、`orders` 和相关确定性样例数据；验收服务使用固定只读 SQL JOIN 直接查询既有表，不新增结果表、不在查询时写表，也不复制 Outdoor 的物化刷新链路。

服务输出固定排除姓名、电话、邮箱和地址等个人信息，该验收服务的字段为：

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
- 修改消费契约后触发 `contract_fingerprint` 不一致并阻断旧 Component 自动查询。

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

### 5.2 Component 参数与查询配置

Component 保存的参数与结构化查询配置如下：

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
- `parameter_filters` 每项把一个 Component 参数绑定到一个服务字段和操作符，不使用字符串占位符；
- `named_parameter_bindings` 每项把一个 Component 参数绑定到 Descriptor 声明的服务命名参数；同一 Component 参数只能在字段筛选和服务命名参数中选择一种绑定；
- 所有已提交的参数谓词与 `fixed_filter` 使用 `AND` 组合，不建设动态布尔表达式编辑器；
- 未提交的非必填参数不生成谓词，未提交的必填参数在前端和 Workbench 编译层都必须拒绝；
- `page_limit` 是每次读取上限并必须不超过 Descriptor 声明的上限；cursor 只存在当前 Explore Session；
- 运行时参数优先于 `default_parameter_values`，但只有用户显式保存才能修改默认值。

运行时由 Workbench 将当前参数值编译为 Service 现有 `QueryExecutionRequest`，不生成第二种服务请求格式。

Component 禁止保存 SQL、MQL、Cypher 等查询语言、Engine 或来源定位、连接信息、凭据、结果数据、cursor、服务 URL 和 owner 内部 DTO。cursor 只存在当前浏览器会话；应用列表和详情不依赖 Service 可用性，进入编辑或运行时再读取 Descriptor。

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

当前创作范围固定为一个页面、十二列栅格和一个或多个 Component，支持同页 Selection Binding 与 `desktop | wallboard` 展示模式。`mobile`、多页面和轮播仍属后续范围，数据库快照不得预埋未实现的第二套页面或展示模式字段。

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

创建请求提交名称、说明和完整 `snapshot`。`snapshot.components` 至少包含一个当前用户可消费的已发布 Service；Workbench Backend 逐一读取 Consumer Descriptor，不信任客户端提交的契约指纹，并将当前权威指纹归一化进草稿。Component 直接配置 Service，不依赖中间资源。

聚合根保存当前草稿快照、正整数并发 `version`、`unpublished | published | offline` 发布状态和当前 Revision 编号。`PUT` 原子完整替换草稿；发布在一个事务中校验 `version`、写入新的不可变 Application Revision、切换当前 Revision 并递增聚合版本；下线只切换发布状态并递增聚合版本，不删除最后发布修订。只有从未发布的应用允许携带当前 `version` 删除，已产生 Revision 的应用只能下线。

应用发布版次使用 `revision_number`，不得复用资源并发字段 `version`。应用更新、删除、发布和下线都必须在请求体携带当前正整数 `version`；冲突统一返回 `409 + workbench_data_application_version_conflict`，不得自动重试或服务端兜底当前版本。

稳定运行响应要求 `workbench.data_application.execute`，并校验当前用户是创建者或持有 Asset 履约产生的有效 owner Resource Grant。不得用“同 Tenant 即可读”、公开链接、API Key、专属 Token 或 Catalog 可见性代替资源授权。

发布后的 Data Application 先由 Catalog 建立企业 CatalogEntry，再由 Asset 通过 `AssetComponent.catalog_entry_id` 组合为 `application` 类型资产。Asset 只保存资产组合、发布和授权履约事实，不保存应用页面、组件树或运行配置。

### 5.4 组件联动与受控下钻设计

“参数联动”是一个 Application Parameter 通过多条 `parameter_bindings` 同时驱动多个 Component；“选择联动”则把源 Component 的选择结果写入 Application Parameter。两类 Binding 分开建模，renderer 不能直接修改目标 Component 的查询：

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

指标次数下钻沿用此主路径：Model 在同一不可变实现修订中显式启用去重明细，并冻结汇总和明细两个计划；Service 通过 Model Client SDK 选择 `result_kind=details` 发布独立查询服务。Workbench 的汇总卡片将结果 `subject_id`、`bucket` 写入明细组件对应的 Application Parameter；明细查询继续绑定原始日期区间和统计粒度，并对结果 `bucket` 等值筛选。因此按月下钻保留非整月起止边界，全期间下钻覆盖原区间，无需前端换算日期或动态替换服务。首期显示主体、统计期间和去重对象标识，可配置已有主体显示名称；不推断其他业务字段。明细按稳定键分页且不补零，跨页总条数应与同口径汇总一致；两个请求之间来源发生变化时不承诺跨请求快照一致性。启用该配置后仍需在重启后的正式应用中验收点击、分页和对账，代码门禁不代表应用已经发布。

analytical Consumer Descriptor 的结果可筛选字段只从冻结计划输出派生，不在 `data_config` 另存 `filterable_fields`；明细的 `bucket` 等值筛选使用同一执行路径。验收结论见 14.2。

首期不包含：

- 跳转到任意 URL、外部应用或未声明页面；
- 动态替换 ServiceReference、operation、字段名或查询模板；
- 多页面导航、面包屑、返回栈和跨页面状态恢复；
- bbox、范围刷选、多选集合、框选、级联树或任意值转换；
- Renderer 之间直接发请求、直接持有对方实例或共享私有状态。

多页面下钻必须等多页面模型单独确认后，再复用 Application Parameter 状态；不能为首期同页联动预埋隐藏页面或通用导航动作。

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

未配置 Selection Binding 的 Application Parameter 继续复用共享输入控件，并按既有查询操作提交，不改成逐键自动查询。已配置 Selection Binding 的条件只显示选择状态、源组件入口和非必填条件的清除操作，不再提供手填原始值输入框；底层仍只维护同一份原始参数值，不建立第二份不可见的 interaction state。

运行画布根据既有 Selection Binding，在参数旁提供定位源组件的“从…选择”入口，在源组件显示选择操作与目标参数提示；成功赋值只提示参数已更新，不把下游查询成功作为同一个结论。Component 已有的 description 在运行与整页预览中一致显示。入口只在同页滚动并聚焦源组件，不改 URL、不执行额外查询，也不从目录当前页推断或缓存人员昵称。参数名称回显使用显式配置的 display_source，按下述契约独立查询。参数原始值仍用于查询，但不再作为手填输入展示，名称仍由各 Service 输出。组件试查、应用初始值和场景预设的选择弹窗复用同一运行画布，只展示并查询对应源组件，点选只回填正在编辑的条件；取消不回写，试查不改变应用默认值或预设。

选择源组件的专用筛选就近呈现：某个 Application Parameter 的全部 Parameter Binding 仅指向一个选择源 Component，且它不是任何 Selection Binding 的赋值目标时，控件放入该源组件、位于结果上方；其他参数保留在顶部。位置只从显式绑定派生，不依据参数名称、字段名称、服务或业务领域猜测用途，不新增持久化配置。两个位置复用唯一参数字段组件、同一参数值和契约校验；输入仍不自动查询，可通过组件既有“查询”或顶部“查询全部组件”提交，重查重置游标。`parameters` 显示开关同时控制顶部和组件内控件；恢复默认值与预设仍覆盖完整参数集合，草稿整页预览与发布运行共用此规则。

自由文本参数支持回车提交：组件内触发该组件的既有查询，顶部触发既有查询全部；与对应按钮共用可执行条件，不因键盘提交建立额外请求路径。中文输入法确认候选词、按键长按重复、组合快捷键及下拉选项确认不触发查询；查询动作隐藏或当前按钮不可用时回车同样不可执行。

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

### 5.5 单页 Wallboard 展示模式

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

### 5.6 Wallboard 应用刷新策略

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

### 5.7 Wallboard 应用呈现区块

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

- 不把 API Consumer Credential 转换为用户身份、Tenant Membership、Role Permission 或 Service Resource Grant；
- 不让外部 BI 复用 `addp-cli`、`addp-workbench`、`addp-service` 或其他内置 OAuth Client；
- 不用 Client Credentials Service Principal 获得通用 Tenant 数据权限；
- 不增加 Workbench 查询代理、外部 BI 专属执行端点、静态 Token、数据库直连、JDBC / ODBC 旁路或手工复制 Access Token；
- 不要求外部 BI 解析 Service 管理 DTO、SQL、Engine、schema、table 或内部执行计划。

API Consumer Credential 是独立的数据面凭据，具体边界以[API 设计规范](addp-API设计规范.md)及[授权上下文规范](addp授权上下文规范.md)为准；它不生成用户 AuthContext，不能访问 Consumer Catalog、Descriptor 或其他控制面，也不能转换为用户 OAuth 身份。外部 BI 的用户委托消费继续使用上述唯一 OAuth 路径，不把旧 Application API Key 的配额假设写成现行授权能力。

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

#### 5.9.2 Python Service Consumer SDK

通用 SDK 的唯一位置为 `common-python/addp_common/client/service.py`，公开类型由 `client/__init__.py` 导出，测试位于 `common-python/tests/test_service_client.py`。SDK 分页读取 Consumer Catalog、校验 Descriptor 与冻结指纹、调用声明的同源 POST query operation，并按不透明 cursor 返回契约类型及原始 records；不依赖 pandas、GeoPandas 或具体业务字段。

`ServiceConsumerClient.from_cli_session()` 按需加载 CLI Browser Login、OS Keychain 与轮换会话能力，不顶层加载桌面认证依赖。SDK 已取得真实普通表、空间表和多服务只读消费证据；它不替代 Power Query 宿主、callback state、外部 Client 生命周期及真实 BI 产品验收。

### 5.10 Application Parameter Preset

**Application Parameter Preset / 应用参数预设** 是 Data Application 创作者为当前 Snapshot 中全部 Application Parameter 定义的一组完整命名值。它用于把重复使用的参数组合发布为最终用户可直接选择的业务场景，而不是让用户每次重新理解并填写服务参数。

参数预设属于 Data Application 草稿和不可变 Application Revision Snapshot，不单独建表、不拥有 CRUD、授权或发布生命周期。每项只保存稳定 `key`、用户可见 `name` 和按 Application Parameter key 索引的完整 `parameter_values`；数量、key、名称和值类型必须受限。`parameter_values` 必须与当前参数集合完全一致，每个值必须同时通过该参数全部 Parameter Binding 对应的 Consumer Descriptor 输入类型和操作符校验。预设不保存 ServiceReference、查询模板、筛选表达式、结果、cursor、Token 或 URL。

运行时选择预设必须原子替换当前参数状态，先使受影响 Component 的在途查询和导出失效，再复用既有“查询全部组件”主路径执行；不能为预设建立第二套请求编译器。用户随后手工修改参数或通过 Selection Binding 改写参数时，当前预设选择自然清空，实际值继续只存在于浏览器会话中。“恢复默认值”使用 Application Parameter 自身的 `default_value`，不把默认值复制成隐藏预设。

正式运行链接允许使用唯一查询参数 `preset=<key>` 引用已发布 Revision 中的预设。URL 不接受 JSON、重复参数或任意原始参数值；未知、空白或重复 `preset` 失败关闭并回到应用默认参数，不猜测名称、不读取草稿预设。链接不会授予权限，运行页仍按当前访问者身份读取 Data Application Revision 并调用 Service。

首期不建设个人预设、最近使用、跨应用模板、预设继承、部分参数覆盖或后台保存运行状态。没有参数或没有已发布预设的应用不显示预设控件；预设只提升最终应用的可重复使用与可分享性，不改变 Workbench 的 Service-native、只读消费边界。

## 六、Service Consumer Descriptor 与 Consumer Catalog

### 6.1 消费描述职责

Workbench 不得读取 Service 管理 DTO，因为管理 DTO 包含 SQL、Engine、schema、table 和发布配置。Service 必须提供面向消费者的独立只读投影，稳定名称为 Service Consumer Descriptor（服务消费描述），协议版本从 `addp.service_consumer/v1` 开始。

Consumer Descriptor 使用通用信封与可辨识的强类型输入/输出契约；精确字段以 Service 当前公开 DTO 和 Swagger 为准，不能从管理 DTO 推断消费接口。

语义：

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

当前消费采用明确的 fail-closed owner policy：只返回当前 Tenant、`active`、启用 REST Query operation 的服务，并只接受当前 Tenant Scope 的 `service.data_read.execute` Role Assignment。现有 Service 尚无“服务—Department / Project Group” Resource Scope Binding，因此 Department 或 Project Group Scope Assignment 不能临时扩大为全租户服务访问；细粒度 Grant、Binding 和 Explicit Deny 建立事实模型后，必须接入同一 Repository 过滤与详情判断入口，不能新增第二套 ACL 或先分页后过滤。

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
- `eq | ne | lt | lte | gt | gte | in | contains | is_null | is_not_null | bbox_intersects`。

Workbench 根据 Consumer Descriptor 中的字段类型、允许操作符和空间能力生成输入框、枚举、多选、范围、日期和地图框选控件。Workbench 只编译结构化请求，不拼接 SQL。

`contains` 仅对 string 字段且执行引擎实现文本筛选 Provider 时声明，按字面子串区分大小写，通配符字符无特殊含义。人员选择表可分别绑定可选昵称输入；留空不筛选，变更条件重置游标，服务端筛选覆盖全部结果而非当前页。Workbench 只按 Descriptor 配置、校验和发送操作符。

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

命名参数的固定选项由 Consumer Descriptor 的 `options`（类型化 `value` 与 `zh-cn/en` 名称）唯一提供。Workbench 不复制选项到应用快照；组件编辑、应用默认值、预设、预览和运行使用 common-frontend 的唯一参数输入控件。显式绑定多个目标时，类型必须一致，有限允许值取交集；空交集或共同选项的名称冲突须指出绑定组件并阻止保存/发布，默认值、预设和选择联动均受最终允许值约束。契约未加载或指纹失配时禁用对应输入和查询，不退化为自由文本。


参数候选值只能来自：

- Consumer Descriptor 中的固定枚举；
- Service 明确提供的有界选项查询；
- 另一个正式发布的数据服务；
- 用户手工输入。

Workbench 不对底层字段执行无界 `SELECT DISTINCT`，不直接查询来源表，也不把样例值当成完整候选集。

Outdoor 人员选择不增加“按 ID 查询昵称”的专用服务。人员指标 Query Service 直接输出 `person_id + person_nickname + 指标`，两个同源人员列表 Component 分别通过 Selection Binding 写入 `person_id_a` 和 `person_id_b`；重叠度 Component 将这两个 Application Parameter 绑定到服务命名参数。昵称用于展示，稳定人员 ID 用于计算。

### 7.4 保存与运行

Data Application Component 保存参数定义、查询模板和默认值。运行时输入默认只存在当前页面状态，不自动覆盖草稿；所有者显式保存后才更新聚合根版本，发布后才形成新的不可变 Revision。

### 7.5 Query Service 输入契约

Service 从实际执行校验同源投影字段选择、筛选操作符、排序、命名参数、稳定键、分页限制及格式。类型复用 ADDP FieldType，未声明能力不得由 Workbench 猜测。命名参数仅在服务真实声明时出现，没有则为空数组；公开字段结构以 Service Consumer Descriptor 和 Swagger 为唯一事实源。

### 7.6 应用参数名称回显

应用参数允许配置 `display_source: {source_component_id, label_field}`；显示来源不是另一份参数值或查询模板，取值字段仍由现有 Selection Binding 唯一确定。名称字段须为源组件已查询、可选择的 string 输出，取值字段必须允许 eq。保存和发布均验证来源、绑定、字段及契约，删除来源同步清理引用。搜索向导在能力满足时直接使用用户已选的名称字段；已有应用由筛选设置显式配置。

浏览器经源组件的 Service Consumer Descriptor query operation 按当前原始值精确查名：保留固定过滤、排序及命名参数，移除目录动态搜索和分页游标，最多取两行。完整唯一匹配行的非空名称才能回显；不存在、歧义、空名称及失败明确提示。参数或上下文变化立刻清除旧名称，较旧响应不得覆盖新选择。昵称不持久化，重新打开、恢复默认及切换预设均重新解析；试查和取消保留原有隔离语义。此名称只用于参数控件，不替代图表自己的服务返回名称。

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
| 图表结果 | `common-frontend/chart` | `ChartRenderer` | 使用 ECharts 渲染已完整的有界表格结果 |
| 空间结果 | `common-frontend/map` | `GeoJSONResultRenderer` | 复用 `MapContainer`、OpenLayers、CRS registry 和底图 profile |
| 渲染编排 | `workbench/frontend` | `WorkbenchRendererHost` | 选择 renderer、适配 Service 结果、判断完整性并处理 cursor |

`common-frontend/chart` 使用技术栈规约的 `echarts@5.5.1` peer dependency，由消费模块自行声明依赖。地图继续使用 `ol@10.7.0`。不使用与平台技术栈不一致的版本作为 Workbench 基线。

共享 renderer primitive：

- 只接收已归一化数据、字段事实和展示配置；
- 不发起 HTTP 请求，不读取 Token，不识别 ServiceReference、Consumer Descriptor 或 Data Application；
- 通过事件返回排序、分页、选择、bbox 和地图视角等交互意图；
- 只使用 ADDP 主题变量和导出的双语 i18n 消息，不持久化业务状态。

Service 返回的 decimal 字符串在表格、数值卡片及图表的数据标签和提示中，必须直接按原始十进制值与显式精度格式化，不得先转换为 JavaScript `Number` 而改变展示值。图表坐标计算可以使用有限浮点近似，但这一近似不能覆盖原始值的文本展示，也不能回写 Service 结果或应用快照。

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
- `label` 是数据应用所有者定义的展示文本，不改变字段语义；
- 格式类型第一阶段只使用 `auto | number | percent | date | time | datetime | boolean | json`，并按 `FieldType` 限制可用组合；
- 排序属于 `query_template.order_by`，不在 renderer config 中重复保存；
- Table 可显示当前页并通过 Workbench 维护的 cursor 栈前后翻页，不在浏览器对跨页数据重排或聚合；
- 第一阶段不支持行编辑、分组、透视、合计行或计算列。

Table Component 的查询字段与可见列是两项不同配置：`query_template.select` 包含用户明确选择、供展示或 Selection Binding 使用的全部字段；`renderer_config.columns` 只能取其非空子集，决定页面表格显示哪些列。隐藏内部联动键只影响呈现，不从 Service 请求或结果行删除该键，也不改变联动原值、导出和已发布修订。组件编辑器必须分别配置两者，字段展示格式只针对可见列；不得根据 `id` 等字段名称自动隐藏。

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

Chart 可显式启用 `total_as_value: true`：维度必须已配置 `temporal_format=period` 与有效的参数绑定，度量限制为 1–4 个且每个度量显式声明显示精度。Renderer Host 根据成功查询参数中的全期／按月粒度，在现有共享 ScalarValueRenderer 与 ChartRenderer 之间选择呈现；未启用时保持图表。全期必须是完整唯一行，空结果、多行、部分结果或无效数值不得取首行、求和或补零。数字卡片沿用度量名称、单位、精度和状态规则，并发出原始行的选择事件；CSV 和查询主路径不变。日期范围说明仍使用成功查询参数快照。该选项不增加 Service 能力、数据源、独立组件或渲染器类型。

Chart 可显式选择 `result_name_field` 展示当前查询对象名称，字段必须是当前 Consumer Descriptor 输出且已 select 的 string 字段，名称标签复用该字段的 `field_presentations.label`。Renderer Host 只读取本组件成功查询的完整结果：所有行名称一致时显示该名称（全期与按月规则相同），全为空时显示未提供名称，部分结果、混合空值、非字符串或不同名称时提示无法确定单一名称，不猜取第一行。参数改变后名称与结果同时清除，迟到请求不能覆盖新名称。查询和选择仍使用原始标识，配置随应用修订冻结；不从目录当前页、其他组件结果或输入字段名推断名称，不额外查询。指标服务的名称由 Model 既有主体显示名称契约提供，Workbench 不依赖 Model 内部结构。

桌面运行与整页预览中，若一个独立横排内的组件均已切换为全期数字卡片，该排按内容高度呈现并带动下方组件上移；标题、说明、日期范围和多度量换行共同决定高度，不使用固定的缩小高度。与图表、表格混排或存在跨排组件时保留原网格行高，避免挤压其他内容。按月及参数失效后恢复保存的图表布局。编辑画布与大屏保持显式 placement；此适配不写回快照，不增加布局字段或查询。窄屏数字卡片同样按内容展开。

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

审计不得记录筛选字面值、参数值、返回行、空间要素、SQL、cursor、Token 或原始请求 Body。Service 执行面审计必须覆盖公开查询执行路由，不能仅依赖管理面中间件，也不能由 Workbench 伪造 owner 审计事件。

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

### 8.10 字段呈现规则

Workbench 的正式运行入口必须比 Manager 数据预览和 Service 服务预览更贴近业务用户。Service Consumer Descriptor 只提供可信的字段名、类型和原始注释；它不应为每个数据应用决定“显示为什么业务标签、使用什么单位和精度”。这些是 Data Application Component 在具体应用中的发布事实，统一称为 **Field Presentation / 字段呈现规则**。

字段呈现规则位于 Table、Chart 和 Map 的 `renderer_config.field_presentations`，每项固定引用一个 renderer 已使用的输出字段，并只允许以下受控语义：

- `label`：所有标量字段均可声明，作为表头、图例、地图弹窗键名和主显示字段标签；
- `unit` 与 `precision`：只允许数值字段使用，精度为 `0..8`；
- `temporal_format`：日期与时间格式必须与输出类型匹配；统计期间可使用 8.3 节既有的 `period` 格式与显式参数绑定；
- `width`：只允许 Table 使用，范围为 `80..600` CSS px。

`value_labels` 为有限值配置显示名称，每项只包含类型化 `value` 与 `label`，最多 32 项。本阶段支持 `string | bool` 字段；字符串精确区分大小写和空白，最长 1000 字符，允许空字符串；标签去除首尾空白后必填，最长 100 字符。禁止重复值、null、跨类型匹配、表达式和动态请求。未命中时按原值展示，null 仍显示缺失标记。映射只替换显示文本，状态规则仍对原始值求值；共享格式化器统一用于表格、图表维度/提示和地图标签/弹窗，不改变查询、导出或选择事件。创作界面和 Backend 同时按 Descriptor 校验，配置随不可变 Application Revision 保存。

人员昵称等可变化的维度属性不属于静态 `value_labels`。关联由服务侧基于显式键关系完成，输出同时保留稳定标识和显示名称；Workbench 仅通过 Consumer SDK 读取 Descriptor 与结果并选择展示字段。不得从当前目录组件的分页结果拼接名称，也不得直连 Model 或数据库。关联键唯一性、未匹配行保留和属性时点应由服务关联契约明确，不能让名称关联改变指标粒度或放大计算值。

Table 的 `columns`、Chart 的 `dimension / measures`、Map 的 `label_field / tooltip_fields / style.field` 仍是字段身份与查询事实源。字段呈现规则只格式化 renderer 展示，不修改 rows，不改变选择联动、参数绑定或导出数据，不把标签当作字段名回传 Service。Value renderer 的 `items` 已以字段、标签、单位和精度表达同一职责，继续作为它的唯一配置，不再叠加第二份 `field_presentations`。

Backend 对重复字段、renderer 未使用字段、不匹配类型的格式、越界精度或列宽严格失败关闭。Frontend 不接受任意 formatter 名、函数或格式化代码；Table、Chart 和 Map 必须共享 `common-frontend` 内唯一的标签解析和标量格式化实现，Workbench 只负责根据 Descriptor 创作、保存并传入配置。未显式配置的字段使用 Descriptor `comment || name` 和原始值作为唯一默认行为，不引入域、服务 ID、业务字段名或 Outdoor 特例。

字段查找、Descriptor 标签回退、数值格式化和时间格式化由 `common-frontend/basic` 的唯一纯函数承担。共享 Table 消费标签、列宽和单元格值；Chart 保留原始数值 series，在 tooltip 显示格式化后的值与单位，在度量轴标题集中显示单位而不向每个刻度重复追加，维度轴标题固定居中避免窄组件右侧截断；Map 只格式化弹窗主字段、属性键值和主题图图例。三者均不改写 Service rows，选择事件仍使用原始 `row_index`。

Component 编辑器从当前 renderer 实际使用字段生成一组字段呈现配置，并将预览与保存收敛到同一个 `buildRendererConfig()` 编译器，删除了编辑器内的重复 renderer 组装逻辑。空间探索向导也通过同一同步函数生成普通 Component 配置，没有新增模板运行时、Outdoor 分支或样例字段。Backend 使用强类型结构和严格 JSON 解码校验上述约束，有效配置随草稿及不可变 Revision 直接保存。

### 8.11 状态呈现规则

字段显示名、单位和精度解决“读得懂”，但最终应用还需要在不改变数据事实的前提下突出需要关注的状态。该能力统一称为 **State Presentation / 状态呈现规则**，是 Field Presentation 和 Value item 的受控子配置，不是 Service 过滤器、计算字段、告警任务、任意表达式或第二套 renderer。

每个可展示标量字段最多声明 8 条有序 `state_rules`。每条规则只包含 `operator`、`operand`、`label` 和 `tone`：

- `operator` 只允许 `eq | lt | lte | gt | gte`；所有受支持标量类型可使用 `eq`，大小比较只允许数值类型；
- `operand` 必须与 Consumer Descriptor 的字段类型一致，整数类型不能接受小数；
- `label` 是必填的业务状态标签，最长 50 个字符；
- `tone` 只允许 `info | success | warning | danger`，由 ADDP / Element Plus 主题语义色解析，不接受十六进制、RGB、CSS 变量名或任意颜色；
- 规则按快照顺序执行，第一条命中即停止；未命中时不显示状态。编辑器必须允许调整顺序，不能通过隐式阈值排序改变用户声明。

状态呈现必须保留格式化后的原值，并在支持文本展示的位置追加状态标签：Table 单元格和 Value 卡片直接显示语义状态，Chart 只在 tooltip 中显示，Map 只在 popup 中显示。Chart series 和 Map feature 的视觉编码继续分别由 Chart 配置和 Map `uniform | categorical | continuous` 主题样式负责，不能让状态规则形成第二套图形配色来源。原始 rows、导出内容、`result-select.row_index`、Parameter Binding、Selection Binding 和 Service 请求均不改变。

Backend 必须按当前冻结 Descriptor 对规则数量、操作符、operand 类型、标签和 tone 严格校验；Frontend 只提供与字段类型匹配的受控表单。共享匹配、格式化和语义状态解析归 `common-frontend/basic` 唯一所有，Table、Value、Chart 和 Map 只能组合该能力。任何示例阈值、状态标签、字段名或 Outdoor 事实只能保存进具体 Data Application Snapshot，不能进入生产代码或默认配置。

### 8.12 运行布局与选择反馈

最终应用的 Component placement 既是位置约束也是可用展示区域。Chart、Map、Table 等可伸展 renderer 必须填满卡片标题和固定操作区之外的剩余高度，不能继续使用固定 `420px` 形成大块无意义留白；移动端退化为纵向流式布局时仍保留受控最小高度。该行为只改变运行画布布局，不修改 Snapshot placement、查询契约或 renderer 数据语义。表格渲染宿主必须把剩余高度传递到共享表格的内部滚动区，分页区不参与压缩；大量结果行不得溢出覆盖分页按钮或截获其点击。

Map popup 必须在当前地图 viewport 内完整可见。点击靠近任一边界的要素时，由共享 Map renderer 使用受控边距自动平移地图，使 popup 完整进入视口；popup 自身保留最大高度和内部滚动，不能溢出后由 Workbench 页面裁切。自动平移只服务信息可见性，不改变数据查询、要素选择或持久视图状态。

Selection Binding 的源选择是浏览器会话内的瞬时交互状态，不进入 Data Application Snapshot。Chart 必须用主题语义色明确标识当前选中的数据项；源 Component 重新查询或 renderer 重建时选择态自然清空。选择触发目标 Map 重查时保留用户当前地图视野，不重新执行结果范围 `fit`，因此不出现无意义缩放动画；Map 的首次有效结果和用户主动查询仍可按结果范围定位。上述选择反馈不能复用 State Presentation，也不能把字段名、颜色值或 Outdoor 事实写入 Workbench 代码。

### 8.13 运行恢复与异步生命周期

同一 `DataApplicationCanvas` 负责草稿预览与发布运行；浏览器会话状态不写入 Snapshot。发布应用在所有 Descriptor 有效且必填默认值可执行时，复用“查询全部组件”执行一次首查，完成后才启动 wallboard 刷新计时。数字 `0` 与布尔 `false` 不是空值；`is_null | is_not_null` 只有 `true` 表示启用。缺少默认值、Descriptor 不可用或契约漂移时停止首查。普通桌面草稿预览保持显式查询，Wallboard 草稿按既有策略刷新；手工改参数不逐键自动查询。

| 状态或操作 | 唯一处理语义 |
| --- | --- |
| 契约指纹不一致 | `contract_error` 阻断执行，由所有者显式重绑并发布新 Revision |
| Descriptor 临时失败 | `descriptor_error` 保留提示，手工查询与前台刷新可重新读取 |
| 单次查询失败 | `query_error` 保留上次成功结果，允许同页重试；参数已改变时先清空旧结果 |
| Runtime Revision 读取失败 | 原页重试；应用 ID 变化先销毁旧画布并清空旧应用 |
| 参数变化 | 只按 Parameter Binding 推导受影响组件，立即失效其查询与导出，原子清空结果、分页和运行错误；未绑定组件保持不变 |
| 游标翻页 | 候选 cursor、页码、rows 和 Page Metadata 只在当前请求成功后一起提交，失败保留上一成功页 |
| 导出迟到 | 在 Blob URL、DOM link 和 click 副作用前复核 Component 与 request generation；过期响应不下载、不提示 |

所有异步成功、错误、loading 收尾、导航和下载副作用必须校验发起时的上下文，复用 `common-frontend/basic` latest-request coordinator 与 Workbench 唯一提交守卫。Component 的 Descriptor 与查询/导出、编辑页的 load 与 mutation、列表的 load 与 deletion、向导的 Catalog 与两类 Descriptor 分别持有独立 generation，不互相取消或提前结束 loading。服务、组件、路由、参数或查询配置变化，以及弹窗关闭、重新打开和页面卸载，均立即使对应旧请求失效。

发布、下线、组件删除和应用删除在确认框返回后再次校验上下文；应用删除同时复核当前行 `version`。取消确认无副作用，服务器 mutation 期间使用现有 loading mask 避免覆盖新编辑；删除非首页最后一行后先回退一页再刷新。竞态通过调用生产提交函数的可控 Promise 行为测试证明，不只依赖源码正则或真实网络时序。

首屏依赖采用 Element Plus 按需引入和 Map 延迟加载，入口 chunk 设 500 KiB 硬预算；共享包不保留自己的 `node_modules`，peer runtime 解析到消费模块依赖树。

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

#### 9.1.1 数据应用交付链接与受限访问

Workbench 第一阶段的 Data Application 是 Tenant 内的已认证数据应用，不是匿名网页。稳定运行地址 `/data-apps/:application_id` 及可选 `preset` 只表达应用与发布场景定位，不表达授权；复制链接不得创建 Grant，`preset` 也不得承载 Token、API Key 或任意查询参数。

发布后统一由 `DataApplicationDeliveryDialog` 提供交付：管理页按需打开，编辑器在当前发布 mutation 成功提交后立即打开。组件只读取当前不可变已发布 Revision，列出默认参数和该 Revision 的预设，提供运行与复制 Console 同源链接；草稿预设不混入，下线应用不显示入口。对话框关闭、切换目标和卸载使迟到请求失效。列表页和编辑器只调用该组件，不扩展列表 DTO、不逐行预取、不新增 Backend API 或第二条运行路由。

顶层运行端的交付体验固定为以下单一路线：

1. 匿名用户打开运行链接时进入统一登录，登录成功后返回原始 `fullPath`，包括已发布的 `preset` key；登录回跳只接受当前 origin 的站内路径，不接受协议相对 URL 或外部 URL。
2. 已登录用户缺少 `workbench.data_application.execute` 或当前 Data Application 的有效 Grant 时，运行端显示稳定的受限访问状态，提供 Portal 资产搜索、“我的申请与授权”和当页重试入口；Workbench 不因此在线查询 Asset ACL，也不根据 Application ID 推断 Asset。
3. 应用运行授权生效后，Component 仍使用当前 User Bearer 请求 Service；某个 Service 拒绝访问时只阻断相应 Component，不改用 Workbench 或创建者身份代理查询。
4. 交付界面必须明示链接的访问范围：接收者需登录当前 Tenant，并具备应用运行权限、有效应用授权和各 Component 依赖的 Service 数据权限。

如果未来存在面向匿名公众的真实产品需求，应单独设计“公开数据应用”的显式发布策略，且只能组合已显式允许公开访问的 Service，同时完成限流、防滥用、缓存和数据暴露审查。当前不增加匿名兼容分支、秘密链接、URL 凭据或创建者身份代理。

### 9.2 BFF 和服务身份

- 同步代表用户访问 owner 时，转发当前已验证的 User Bearer；
- 不代表用户的控制面读取才使用 `addp-workbench` Service Access Token；
- Workbench 不保存 User Token，不把 User/Tenant/Role 放入 Header、Query 或 Body 让 owner 信任；
- 浏览器不保存 API Consumer Credential；
- API Key 只表达外部应用身份以及 Gateway 公开接口的可选配额和审计语义，不替代私有 Service 所需的用户委托 OAuth、Role Permission 或 Resource Grant，也不作为 Workbench 内部主路径。

### 9.3 Asset 授权

Asset 负责申请、审批和履约，Service 或 Workbench 作为资源 owner 负责最终授权判断。接入 Portal 前必须与企业 CatalogEntry 与 `AssetComponent.catalog_entry_id` 的唯一来源链路一致，不能恢复通用 Owner ResourceRef、软授权、专属 Token 或 owner 实时查询 Asset ACL 的旧路线。

应用资产的正式履约协议固定为：Asset 审批事务创建 `pending` Authorization；可恢复 reconciler 通过 Catalog 当前来源解析得到 Workbench Data Application UUID，再使用 `addp-asset` Tenant Service Access Token 幂等写入 Workbench `ResourceAccessRule`。Workbench 确认规则后 Authorization 进入 `effective`；撤销或过期先进入 `revocation_pending`，Workbench 确认撤销后进入 `revoked`。Portal 只有在 `effective` 时展示打开入口。

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

Asset 的 `application` 类型使用唯一 CatalogEntry 组合路线。不得新增 `workbench_application` 等平行资产类型，也不得恢复专业模块直接向 Asset 自动发现或写入资产草稿的旧路线。

`application` 类型只允许一个 primary Component，且必须动态解析为 `workbench/data_application`；不接受 supporting Component、手工应用链接、iframe URL、API Key 或自定义 Token。未来若出现真正的多应用组合需求，应先定义新的聚合和运行语义，不能把多个 CatalogEntry 临时塞进同一 application Asset。

Service 已发布服务和 Workbench Data Application 都可以后续成为 CatalogEntry，但 Workbench 查询时仍使用 ServiceReference，不从 CatalogEntry 反向猜测执行地址。

Portal 只展示资产和打开入口，不保存 Workbench 页面、组件、参数或服务凭据。

CatalogEntry 标识 Data Application 聚合根，不标识单个发布 Revision。Portal 打开应用时由 Workbench 解析当前有效发布 Revision；已发布 Revision 不因新草稿变化而改变。


## 十二、验证入口与维护

标准验证入口与阶段证据分别遵循[测试与验收规范](addp测试与验收规范.md)及[后续专题](../next/ADDP%20Workbench数据服务消费与数据应用专题.md)。按实际变更选择 `make test-module MODULE=workbench`；共享 renderer、Service 或 Console 的改动按真实依赖扩散。只保留同一 DataApplicationCanvas、Service operation、参数编译及 renderer 主路径，替代旧实现时删除旧路径，契约变化不得静默重绑。

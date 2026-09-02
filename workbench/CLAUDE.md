# Workbench 模块开发说明

Workbench 是面向数据消费者、以已发布 Service 为唯一数据入口的数据应用创作与运行 owner。

## 必读文档

- `docs/next/ADDP Workbench数据服务消费与数据应用专题.md`
- `docs/concepts/addp术语表.md`
- `docs/spec/addp-API设计规范.md`
- `docs/spec/addp新模块开发指南.md`
- `common-frontend/README.md`
- `common-frontend/docs/addp前端风格设计规范.md`

## 边界

- 只消费 Service Consumer Catalog、Consumer Descriptor 和 Descriptor 声明的 operation，不读取 Service 管理 DTO。
- 不直连 Engine、数据库、Model、Develop execution 或上游业务表。
- Data Application 是 Workbench 唯一持久化的用户创作聚合根；草稿、发布、下线和管理读写均同时匹配当前 Tenant 与 owner User。
- Data Application Component 直接消费 Service Consumer Descriptor，持有 ServiceReference、结构化查询模板、参数定义、renderer 和契约指纹；不存在 Workbench View 中间资源或双轨创建路径。
- Renderer 只能由 Consumer Descriptor 字段事实和 Data Application 显式配置驱动；不根据表名、字段名或验收领域推断业务角色。Value 只消费唯一汇总行，Chart 不聚合，Map 只使用显式 geometry、label、tooltip 和受控主题样式。
- 空间探索创作向导只把用户显式选择的两个 Query Service、字段、默认参数和受控样式编译为现有 Value + Chart + Map + Table Component、Parameter Binding、Selection Binding 与布局；它不是持久化 Template，不增加 Backend API、运行时分支或领域默认值，并且只能用于空的应用草稿。
- 发布产生不可变 Application Revision；草稿使用 `version` 并发控制，发布版次单独使用 `revision_number`。
- Selection Binding 只把同页源 Component 当前结果的显式标量字段写入 Application Parameter；目标 Component 必须由既有 Parameter Binding 推导，不能在 renderer 或快照中保存查询、URL、任意动作或第二套目标关系。
- Application Display Mode 是同一 Data Application 页面的运行呈现方式；当前只允许 `desktop | wallboard`。全屏只保存在浏览器会话中，不能进入发布快照；wallboard 不能拥有第二套 Component、查询、授权或数据源。
- Application Refresh Policy 只允许 wallboard 使用关闭、30 秒、60 秒、300 秒四档浏览器前台刷新；页面不可见时暂停，当前查询未完成时跳过本轮，不能创建 Task、Schedule、Execution 或后台刷新旁路。
- Application Presentation Sections 只允许 wallboard 从 `title | parameters | query_actions` 中隐藏运行页区块；修订与刷新状态、全屏入口、Component 标题和错误提示始终显示。隐藏查询操作必须启用自动刷新，隐藏参数区必须保证全部必填参数有可执行默认值。
- 创建、更新和发布 Data Application 时使用当前 User Bearer 重新校验每个 Component 的 Descriptor；运行端由浏览器使用当前 User Bearer 调用 Service，全链路不保存 Token。
- 列表、管理详情和删除只读取 Workbench 自身事实，不因 Service 不可达而失败。
- Workbench 不代理真实数据查询，浏览器按 Descriptor operation 直接调用 Service。
- 创作入口使用 Console iframe；正式数据应用运行入口只使用 Console 同 origin 的 `/data-apps/:application_id`，不增加第二条 iframe 运行 URL。
- Workbench 不是 TaskProvider；在线查看、筛选和刷新不进入 Orchestrator。

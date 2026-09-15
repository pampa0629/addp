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
- Application Parameter Preset 是 Application Revision 内由创作者发布的一组完整命名参数值；它只更新浏览器会话中的 Application Parameter 并复用现有 Parameter Binding 与查询主路径，不单独持久化、不保存结果，也不接受运行 URL 中的任意原始参数值。
- Selection Binding 只把同页源 Component 当前结果的显式标量字段写入 Application Parameter；目标 Component 必须由既有 Parameter Binding 推导，不能在 renderer 或快照中保存查询、URL、任意动作或第二套目标关系。
- Application Display Mode 是同一 Data Application 页面的运行呈现方式；当前只允许 `desktop | wallboard`。全屏只保存在浏览器会话中，不能进入发布快照；wallboard 不能拥有第二套 Component、查询、授权或数据源。
- Application Refresh Policy 只允许 wallboard 使用关闭、30 秒、60 秒、300 秒四档浏览器前台刷新；页面不可见时暂停，当前查询未完成时跳过本轮，不能创建 Task、Schedule、Execution 或后台刷新旁路。
- Application Presentation Sections 只允许 wallboard 从 `title | parameters | query_actions` 中隐藏运行页区块；修订与刷新状态、全屏入口、Component 标题和错误提示始终显示。隐藏查询操作必须启用自动刷新，隐藏参数区必须保证全部必填参数有可执行默认值。
- 保存前整页预览只消费编辑器当前内存 Snapshot 的归一化副本，并与已发布运行页复用唯一应用运行画布；不得新增预览路由、Backend API、临时持久对象、查询代理或第二套查询状态，关闭预览即销毁全部运行会话状态。
- 服务契约变化后，组件编辑器提供显式“按当前契约重新配置”入口，复用既有 Service 选择与 Descriptor 加载逻辑重建字段和参数；确认组件后仍须保存、检查应用参数映射并发布新修订，不能自动改写历史快照。
- 创建、更新和发布 Data Application 时使用当前 User Bearer 重新校验每个 Component 的 Descriptor；运行端由浏览器使用当前 User Bearer 调用 Service，全链路不保存 Token。
- 列表、管理详情和删除只读取 Workbench 自身事实，不因 Service 不可达而失败。
- Workbench 不代理真实数据查询，浏览器按 Descriptor operation 直接调用 Service。
- 创作入口使用 Console iframe；正式数据应用运行入口只使用 Console 同 origin 的 `/data-apps/:application_id`，不增加第二条 iframe 运行 URL。
- Workbench 不是 TaskProvider；在线查看、筛选和刷新不进入 Orchestrator。

## 前端验证

统一执行 `make test-workbench-frontend`，依次运行 Node 确定性测试、Playwright 浏览器回归和产品构建。首次运行前如缺少浏览器，执行 `npm --prefix workbench/frontend exec -- playwright install chromium`；CI 的 Workbench 前端矩阵负责安装 Chromium。

`frontend/e2e/metric-publication.spec.js` 使用真实 Vue 页面和内存 HTTP fixture，覆盖契约变化后显式重配组件、保存与发布不可变修订、运行参数选择、异步 Descriptor 就绪、中英文显示及共享参数冲突。Model 的 `metric-workspace.spec.js` 负责服务来源重绑交互；Workbench 回归从重绑后的 Consumer Descriptor 边界开始，不跨模块读取 Model 内部事实。这些测试不等同于真实后端与数据库的在线联调。

Playwright 自动管理独立的 `127.0.0.1:4190` Vite 测试服务，使用独立缓存并关闭 Gateway 代理，不接管已有开发服务。失败截图和 trace 写入系统临时目录 `addp-workbench-playwright-results`，CI 失败时上传对应 artifact。真实后端联调仍使用既有 `make test-online ONLINE_SUITE=workbench-service-consumption`，并满足该入口声明的环境条件。

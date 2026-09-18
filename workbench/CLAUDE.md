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
- string/bool 字段的 `value_labels` 仅负责有限值显示名称，沿用共享字段呈现格式化器；动态维度名称必须由服务输出，不得使用目录组件当前页补全。映射边界、草稿回显与不可变发布由现有共享前端、Workbench Go 和浏览器门禁覆盖。
- 空间探索创作向导只把用户显式选择的两个 Query Service、字段、默认参数和受控样式编译为现有 Value + Chart + Map + Table Component、Parameter Binding、Selection Binding 与布局；它不是持久化 Template，不增加 Backend API、运行时分支或领域默认值，并且只能用于空的应用草稿。
- 发布产生不可变 Application Revision；草稿使用 `version` 并发控制，发布版次单独使用 `revision_number`。
- Application Parameter Preset 是 Application Revision 内由创作者发布的一组完整命名参数值；它只更新浏览器会话中的 Application Parameter 并复用现有 Parameter Binding 与查询主路径，不单独持久化、不保存结果，也不接受运行 URL 中的任意原始参数值。
- Selection Binding 只把同页源 Component 当前结果的显式标量字段写入 Application Parameter；目标 Component 必须由既有 Parameter Binding 推导，不能在 renderer 或快照中保存查询、URL、任意动作或第二套目标关系。
- Application Display Mode 是同一 Data Application 页面的运行呈现方式；当前只允许 `desktop | wallboard`。全屏只保存在浏览器会话中，不能进入发布快照；wallboard 不能拥有第二套 Component、查询、授权或数据源。
- Application Refresh Policy 只允许 wallboard 使用关闭、30 秒、60 秒、300 秒四档浏览器前台刷新；页面不可见时暂停，当前查询未完成时跳过本轮，不能创建 Task、Schedule、Execution 或后台刷新旁路。
- Application Presentation Sections 只允许 wallboard 从 `title | parameters | query_actions` 中隐藏运行页区块；修订与刷新状态、全屏入口、Component 标题和错误提示始终显示。隐藏查询操作必须启用自动刷新，隐藏参数区必须保证全部必填参数有可执行默认值。
- 保存前整页预览只消费编辑器当前内存 Snapshot 的归一化副本，并与已发布运行页复用唯一应用运行画布；不得新增预览路由、Backend API、临时持久对象、查询代理或第二套查询状态，关闭预览即销毁全部运行会话状态。
- 预览中的“设为应用初始值”是唯一显式回写动作，只复制经过 Descriptor、必填和可选值验证的 Application Parameter 原始值到当前草稿 default_value；普通关闭不回写，保存草稿才持久化，不修改场景预设或发布修订。既有 `dataApplicationDraft.test.mjs` 与 `application-studio.spec.js` 覆盖取消隔离、选择原始 ID、保存重载、无效值和契约未就绪；使用现有 Workbench 前端 T1/T3 与完整模块门禁，无新增依赖或 CI 入口。
- 服务契约变化后，组件编辑器提供显式“按当前契约重新配置”入口，复用既有 Service 选择与 Descriptor 加载逻辑重建字段和参数；确认组件后仍须保存、检查应用参数映射并发布新修订，不能自动改写历史快照。
- 创建、更新和发布 Data Application 时使用当前 User Bearer 重新校验每个 Component 的 Descriptor；运行端由浏览器使用当前 User Bearer 调用 Service，全链路不保存 Token。
- 列表、管理详情和删除只读取 Workbench 自身事实，不因 Service 不可达而失败。
- Workbench 不代理真实数据查询，浏览器按 Descriptor operation 直接调用 Service。
- 创作入口使用 Console iframe；正式数据应用运行入口只使用 Console 同 origin 的 `/data-apps/:application_id`，不增加第二条 iframe 运行 URL。
- Workbench 不是 TaskProvider；在线查看、筛选和刷新不进入 Orchestrator。

## 前端验证

创作端主屏为唯一运行画布的编辑呈现与组件属性侧栏，组件点击只选中、拖动手柄和前移/后移编译为同一 placement 排序，宽高与单/双列排版统一重排避免重叠。查询上下文变化使旧结果和在途请求失效；标题、格式和布局变化复用当前结果。空应用按展示类型进入同一组件编辑器的“数据、展示、条件”步骤。筛选、联动、场景与页面设置收纳于按需面板；未保存离页保护复用共享 `useUnsavedChangesGuard`。`applicationEditorLayout.test.mjs` 与 `application-studio.spec.js` 覆盖排版、新建草稿、中英文、编辑保存、查询失效、预览隔离和窄屏离页保护，现有 `make test-workbench-frontend` 与 Platform CI 自动发现；完整验收使用 `make test-module MODULE=workbench`。不引入新依赖、API、快照字段或创作路由。

筛选卡片直接展示绑定组件，并可进入同一点击联动表单预填当前目标条件；来源和原始值字段仍由用户显式选择，取消不写入。组件归组映射复用 `applicationParameterOptions` 校验类型、选项及默认/场景值，并阻止移除既有联动的最后一个目标。联动表单在用户显式选择来源、字段、条件后才写入草稿；取消不写入，同一字段可以赋值给不同条件，目标条件不得重复。`applicationParameterBinding.test.mjs` 与 `application-studio.spec.js` 同时覆盖共享筛选、取消/编辑联动、原始人员 ID 驱动多个图表、预览隔离和窄屏，仍使用上述标准门禁自动发现。

新增组件的条件表单可显式选择已有应用筛选，使用其初始值试查。复用候选和提交时校验复用 `applicationParameterOptions` 的同一绑定规则，考虑新组件多个输入的共同选项约束；取消不改应用，应用后只写既有 Parameter Binding，不新增重复条件。`applicationParameterBinding.test.mjs` 和 `application-studio.spec.js` 覆盖类型/选项冲突、显式选择、试查原始值、中英文、保存重开及取消，沿用已有前端和完整模块门禁自动发现。

复制组件沿用同一编辑器、新增保存与布局重排路径，直接在条件步骤调整绑定；新增或复制组件选择独立条件时，直接显示筛选名称输入，避免同名条件混淆；使用新组件 ID，保留原宽高，放在原组件后，不复制选择源联动。`application-studio.spec.js` 覆盖中英文人员条件切换、试查、取消、保存重开、原组件隔离与连续复制；`componentDraft.test.mjs` 验证固定查询条件和排序在编辑、复制及试查中保持一致。仍由现有 Workbench 前端 T1/T3 与完整模块门禁覆盖，无新增 API、依赖或 CI 注册。

新组件选择服务后可显式采用基于 Descriptor 字段事实的明细表、类别柱图、时间折线图或空间分布起点，候选显示所用字段；点击后写入同一表单并复用真实试查，必要条件不全时进入条件步骤。`componentDraft.test.mjs` 覆盖可选输出字段、默认顺序、稳定时间字段、显式 geometry 及禁止按名称推断；`application-studio.spec.js` 覆盖中英文创建保存、参数未齐不查询、默认值试查、服务切换清空结果、取消与窄屏。沿用现有前端与完整模块门禁，不新增 CI 入口或依赖。

Table/Chart 的 date 字段可显式配置 `temporal_format=period` 和 `period`（`grain_parameter/start_parameter/end_parameter`），引用当前 Service Descriptor 的必填命名参数：粒度为 string 且选项为 `total/month`，起止为 date。结果展示只使用成功查询当时的参数快照；按月显示年月，全期显示“所选期间合计”，Renderer Host 同时显示开始日期包含、结束日期不包含的完整范围。不得根据 `bucket` 等字段名推断语义；原始行、排序、选择与导出不变。共享组件只接受已解析的展示上下文，不读取 Service DTO。既有共享前端、Workbench T1、浏览器及模块门禁覆盖此契约，无新增依赖或 CI 入口。

`metric-publication.spec.js` 的组合回归覆盖人员、日期与粒度变更后的请求参数、月份与零值绘制、服务返回昵称、双向比率精度，以及恢复默认参数且不改写发布快照。浏览器使用固定 Service 响应，不重算指标；真实按月聚合、日期边界和零分母计算继续由 Model PostgreSQL 门禁负责。测试沿用 `make test-workbench-frontend`、`make test-module MODULE=workbench` 与 Platform CI 的现有自动发现。

统一执行 `make test-workbench-frontend`，依次运行 Node 确定性测试、Playwright 浏览器回归和产品构建。首次运行前如缺少浏览器，执行 `npm --prefix workbench/frontend exec -- playwright install chromium`；CI 的 Workbench 前端矩阵负责安装 Chromium。

`frontend/e2e/metric-publication.spec.js` 使用真实 Vue 页面和内存 HTTP fixture，覆盖契约变化后显式重配组件、保存与发布不可变修订、运行参数选择、异步 Descriptor 就绪、中英文显示及共享参数冲突。Model 的 `metric-workspace.spec.js` 负责服务来源重绑交互；Workbench 回归从重绑后的 Consumer Descriptor 边界开始，不跨模块读取 Model 内部事实。同一浏览器门禁还覆盖 50 行结果在受限卡片中的内部滚动和分页按钮可点击，以及可选 `contains` 输入的字面值提交、翻页后变更筛选重置游标及清空恢复查询；Common 的 PostgreSQL/MySQL `AnalyticalText` 既有 T2 门禁负责真实子串语义，Service T1 负责能力投影和参数绑定。这些测试不等同于真实后端与数据库的在线联调。

运行参数旁的源组件定位入口、Table/Chart/Map 选择提示与参数更新反馈均由既有 Selection Binding 派生，Component 说明直接来自已有 description；不增加参数名称查询或快照字段。上述浏览器门禁同时验证中英文提示、窄屏定位及焦点、定位不查询、选择仍提交原始 ID，以及手动改值仍走既有查询主路径。仍由根 `test-workbench-frontend` 和 `platform-ci.yml` 的 Workbench 前端矩阵覆盖，无新增测试入口或依赖。

仅被一个选择源组件消费、且不作为选择赋值目标的应用参数就近放在该组件结果上方；其他参数保留顶部。两处由 `ApplicationParameterFields` 唯一组合共享 `ParameterValueInput`，不复制输入与契约禁用逻辑。位置由绑定推导、参数状态唯一；`parameters` 可见性、默认值、预设、请求失效与游标重置共同生效。既有确定性和浏览器入口覆盖位置归属、唯一控件所有权、局部筛选提交、翻页后重查与清空、原值选择以及隐藏参数区。

修改参数控件的委托链路时还须执行 `make test-common-frontend`：共享 `parameterInput.test.mjs` 同时约束 Service、Workbench 创作端和运行端的输入所有权。画布通过 `ApplicationParameterFields` 组合共享输入控件，所有权断言必须跟随真实消费位置更新；不能只通过 Workbench 自身前端门禁便视为该类重构验证完成。该共享测试已纳入 platform T0，完整模块门禁会自动执行。

自由文本回车由 `ApplicationParameterFields` 发出提交意图，画布按所在区域复用单组件或全页查询；输入法确认候选、重复键及下拉选项确认不得触发。`metric-publication.spec.js` 覆盖中英文回车查询、局部筛选清空、游标重置和原始 ID 联动，测试仍走同一前端标准入口与既有 CI 矩阵。

Playwright 自动管理独立的 `127.0.0.1:4190` Vite 测试服务，使用独立缓存并关闭 Gateway 代理，不接管已有开发服务。失败截图和 trace 写入系统临时目录 `addp-workbench-playwright-results`，CI 失败时上传对应 artifact。真实后端联调仍使用既有 `make test-online ONLINE_SUITE=workbench-service-consumption`，并满足该入口声明的环境条件。

`frontend/e2e/runtime-fullscreen.spec.js` 通过真实 Fullscreen API 和鼠标滚轮验证已发布 desktop 应用在宽屏、窄屏下的全屏滚动、下方组件可达和退出后整页滚动恢复，并验证 wallboard 全屏仍将组件约束在视口内；由同一前端标准入口与 CI 矩阵自动发现。

指标浏览器回归还验证服务返回的双向主体名称、空昵称、目录当前页缺少主体，以及目录旧昵称不能覆盖指标服务当前昵称；选择联动只提交稳定人员标识。Online suite 复用商务 MySQL 夹具验证持久化草稿的值名称、维度文本、CSV 原值和选择联动原值，只使用可清理的未发布应用；报告缺少任一证据即失败，不把本地 HTTP fixture 通过记为真实 T4 通过。

Chart 的 `total_as_value` 显式开启全期数字卡片，要求期间维度及 1–4 个显式精度度量；Host 仅适配现有共享 ScalarValueRenderer，按月仍走 ChartRenderer。唯一完整行校验与原始行选择事件不得被绕过。覆盖 Go 配置校验、前端配置往返、共享选择契约及中英文全期/月度切换、零值、空/多行/部分结果；使用现有 `make test-common-frontend` 与 `make test-module MODULE=workbench`、Swagger 生成及覆盖门禁，无新增入口或 CI 依赖。

全期数字卡片在桌面运行与整页预览中仅对独立同高横排收紧：Host 通知实际呈现模式，Canvas 将该排投影为内容自适应行并移动后续行，原 placement 不变。混合图表/表格或跨排组件保留固定网格，编辑画布和大屏不压缩。Node 布局测试覆盖空隙、跨排、还原和快照不变；中英文浏览器回归覆盖全期/月度高度切换、混排、窄屏长说明和四项数值不裁切、大屏隔离。前端变更使用 `make test-workbench-frontend`（T1/T3/构建），由现有 Platform CI Workbench 矩阵自动覆盖，无新增依赖或测试入口。

Chart 的可选 `result_name_field` 显式选择已查询的 string 输出字段，Host 用完整成功结果中的一致名称展示查询对象，标签复用字段呈现配置。空名称和不一致/不完整结果明确提示，未查询或结果失效时不显示名称；不得从目录当前页、其他组件或参数名补全。Model 主体显示名称与 Service 冻结发布包均复用现有契约，Workbench 只经 Consumer SDK 消费。Go 校验、前端配置往返、中英文保存重载、全期/月度切换和缺失/歧义边界由现有 `make test-module MODULE=workbench`（T0/T1/T2/T3）、`make test-workbench-frontend`、Swagger 生成及覆盖入口验证，无新增 CI 注册或外部依赖。

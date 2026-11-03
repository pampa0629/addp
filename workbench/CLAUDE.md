# Workbench 模块开发说明

Workbench 是面向数据消费者、以已发布 Service 为唯一数据入口的数据应用创作与运行 owner。

## 必读文档

- [数据服务消费与数据应用规范](../docs/spec/addp数据服务消费与数据应用规范.md)
- `docs/concepts/addp术语表.md`
- `docs/spec/addp-API设计规范.md`
- `docs/spec/addp新模块开发指南.md`
- `common-frontend/README.md`
- `common-frontend/docs/addp前端风格设计规范.md`

## 边界

- 只消费 Service Consumer Catalog、Consumer Descriptor 和声明的 operation，不读取管理 DTO，不直连 Engine、数据库、Model、Develop execution 或上游业务表；真实查询由浏览器直接调用 Service，Workbench 不做代理。
- Data Application 是唯一持久化的用户创作聚合根，Component 直接保存在其快照中；不恢复 Workbench View 中间资源或双轨创建路径。
- 管理读写、发布和下线匹配当前 Tenant 与 owner User。创建、更新和发布使用当前 User Bearer 重新校验各 Component Descriptor；运行查询也使用当前 User Bearer，全链路不保存 Token。列表、管理详情与删除的依赖只限 Workbench 自身事实，不依赖 Service 可达。
- Renderer 只按 Descriptor 字段事实与显式应用配置工作，不按名称或验收领域推断业务角色。Value 只消费唯一汇总行，Chart 不聚合，Map 只使用显式 geometry、label、tooltip 和受控主题样式。
- 创作入口使用 Console iframe；正式运行只使用 Console 同 origin 的 `/data-apps/:application_id`，不增加第二条 iframe 运行 URL。运行与保存前预览复用唯一应用画布，预览不增加查询代理或持久对象。
- Workbench 不是 TaskProvider；在线查看、筛选和刷新不进入 Orchestrator。

聚合字段、交互细节与配置限制统一引用正式规范；修改对应功能前直达下表正文，验证入口见[前端验证](#前端验证)。

| 主题 | 权威正文 |
| --- | --- |
| 消费投影与服务发现 | [Consumer Descriptor](../docs/spec/addp数据服务消费与数据应用规范.md#61-消费描述职责)、[Consumer Catalog](../docs/spec/addp数据服务消费与数据应用规范.md#64-service-consumer-catalog-与唯一-api) |
| Component、草稿与发布修订 | [聚合与 Component](../docs/spec/addp数据服务消费与数据应用规范.md#531-聚合component-与创作范围)、[参数与查询配置](../docs/spec/addp数据服务消费与数据应用规范.md#52-component-参数与查询配置)、[草稿、修订与并发](../docs/spec/addp数据服务消费与数据应用规范.md#532-草稿修订与并发) |
| Renderer 所有权与字段显示 | [共享层与模块分工](../docs/spec/addp数据服务消费与数据应用规范.md#81-common-frontend-与-workbench-边界)、[字段呈现与有限值名称](../docs/spec/addp数据服务消费与数据应用规范.md#810-字段呈现规则) |
| 空草稿空间探索向导 | [通用空间组合](../docs/spec/addp数据服务消费与数据应用规范.md#86-通用空间数据应用组合) |
| 应用参数、选择联动与预设 | [Selection Binding](../docs/spec/addp数据服务消费与数据应用规范.md#54-组件联动与受控下钻设计)、[参数控件与提交](../docs/spec/addp数据服务消费与数据应用规范.md#77-参数控件与查询提交)、[Parameter Preset](../docs/spec/addp数据服务消费与数据应用规范.md#510-application-parameter-preset) |
| 展示模式、刷新与区块 | [展示模式](../docs/spec/addp数据服务消费与数据应用规范.md#55-单页-wallboard-展示模式)、[刷新策略](../docs/spec/addp数据服务消费与数据应用规范.md#56-wallboard-应用刷新策略)、[呈现区块](../docs/spec/addp数据服务消费与数据应用规范.md#57-wallboard-应用呈现区块) |
| 预览隔离与显式初始值回写 | [保存前预览](../docs/spec/addp数据服务消费与数据应用规范.md#89-保存前整页预览)、[创作端的显式回写动作](../docs/spec/addp数据服务消费与数据应用规范.md#212-画布联动与预览回写) |
| 权限、访问身份与契约变化 | [用户交互](../docs/spec/addp数据服务消费与数据应用规范.md#91-用户交互)、[BFF 与服务身份](../docs/spec/addp数据服务消费与数据应用规范.md#92-bff-和服务身份)、[契约变化](../docs/spec/addp数据服务消费与数据应用规范.md#十契约变化) |

契约漂移使用组件编辑器已有“按当前契约重新配置”入口，复用 Service 选择与 Descriptor 加载；确认后仍须保存、检查应用参数映射并发布新修订，不能自动改写历史快照。

## 前端验证

本节按验证主题维护具体断言；以下均沿用现有入口与 CI 自动发现，不为每项交互重复维护门禁说明。

验证导航：[标准入口与隔离环境](#标准入口与隔离环境) · [创作与组件编辑](#创作与组件编辑) · [参数联动与名称回显](#参数联动与名称回显) · [指标与结果呈现](#指标与结果呈现) · [运行展示](#运行展示)。

### 标准入口与隔离环境

完整模块门禁使用 `make test-module MODULE=workbench`，按现有登记覆盖 T0/T1/T2/T3；前端入口及 `platform-ci.yml` 的 Workbench 前端矩阵负责确定性测试、浏览器回归与构建。各主题仍保留共享依赖、Swagger 和真实后端验收的独有要求。

统一执行 `make test-workbench-frontend`，依次运行 Node 确定性测试、Playwright 浏览器回归和产品构建。首次运行前如缺少浏览器，执行 `npm --prefix workbench/frontend exec -- playwright install chromium`；CI 的 Workbench 前端矩阵负责安装 Chromium。

修改参数控件的委托链路时还须执行 `make test-common-frontend`：共享 `parameterInput.test.mjs` 同时约束 Service、Workbench 创作端和运行端的输入所有权。画布通过 `ApplicationParameterFields` 组合共享输入控件，所有权断言必须跟随真实消费位置更新；不能只通过 Workbench 自身前端门禁便视为该类重构验证完成。该共享测试已纳入 platform T0，完整模块门禁会自动执行。

Playwright 自动管理当轮独立回环端口的 Workbench Vite 测试服务和 Console Vite 代理，使用独立缓存并关闭 Gateway 代理，不接管已有开发服务。数据应用运行页通过 Console 同源 `/data-apps/` 入口测试，资源由 `/module-ui/workbench/` 代理加载。失败截图和 trace 写入系统临时目录 `addp-workbench-playwright-results-*`，CI 失败时上传对应 artifact。真实后端联调使用 `make test-online ONLINE_SUITE=workbench-service-consumption`，准备与收尾统一见[Online 场景](../docs/spec/testing/scenarios/授权与共享.md#workbench-service-consumption)，不能由本节受控浏览器测试代替。

### 创作与组件编辑

“设为应用初始值”的取消隔离、选择原始 ID、保存重载、无效值和契约未就绪，由 `dataApplicationDraft.test.mjs` 与 `application-studio.spec.js` 覆盖；按[标准入口](#标准入口与隔离环境)验证，不新增依赖或 CI 入口。

创作端主屏为唯一运行画布的编辑呈现与组件属性侧栏，组件点击只选中、拖动手柄和前移/后移编译为同一 placement 排序，宽高与单/双列排版统一重排避免重叠。查询上下文变化使旧结果和在途请求失效；标题、格式和布局变化复用当前结果。空应用按展示类型进入同一组件编辑器的“数据、展示、条件”步骤。筛选、联动、场景与页面设置收纳于按需面板；未保存离页保护复用共享 `useUnsavedChangesGuard`。`applicationEditorLayout.test.mjs` 与 `application-studio.spec.js` 覆盖排版、新建草稿、中英文、编辑保存、查询失效、预览隔离和窄屏离页保护。不引入新依赖、API、快照字段或创作路由。

新建应用在第一个组件应用后显示连续创作引导，直接选择尚未配置选择来源的条件进入同一搜索列表向导，再进入同一整页预览设置初始值。候选仅来自契约就绪、可接收标量选择、没有固定选项且被结果组件消费的参数；排除已有选择目标和选择源自身专用搜索参数，不依据业务字段名推断。创建态新增搜索列表排在其首个目标组件之前；引导不写入快照，草稿创建后恢复常规编辑。中英文浏览器回归覆盖无初始人员标识的新建、取消、选人后设置默认值与保存重开。

筛选条件的“创建搜索列表”复用 ApplicationComponentEditor：用户显式选择已发布 Query Service、支持 contains 的文本搜索字段、可选择的展示字段、与目标条件同类型且满足必填约束的取值字段。向导一次生成现有 Table Component、独立搜索参数和 Selection Binding，仍走同一组件编译、试查和保存路径；取消不写入，服务切换清空字段，目标契约未就绪或字段不兼容时阻止应用。不增加 API、持久化向导配置或领域字段推断。

已有组件打开编辑器时，试查值按 Parameter Binding 从当前应用草稿的 Application Parameter 初始值复制，包含未保存的筛选修改；应用条件为空时仍保持为空，不回退到组件历史值。试查值可临时修改，取消或应用展示配置均不改应用参数、预设和发布修订；原有参数绑定未变时保留组件原有 default_parameter_values，避免仅编辑格式就改变查询上下文。新增参数或改变参数键、服务引用、字段、操作符等查询绑定时仍由同一组件编译器生成默认值。`componentDraft.test.mjs` 与中英文 `application-studio.spec.js` 覆盖值来源、空值/零/false/数组隔离、试查请求、保存重开和选项校验；不新增 API、快照字段或标准入口。

复制组件沿用同一编辑器、新增保存与布局重排路径，直接在条件步骤调整绑定；新增或复制组件选择独立条件时，直接显示筛选名称输入，避免同名条件混淆；使用新组件 ID，保留原宽高，放在原组件后，不复制选择源联动。`application-studio.spec.js` 覆盖中英文人员条件切换、试查、取消、保存重开、原组件隔离与连续复制；`componentDraft.test.mjs` 验证固定查询条件和排序在编辑、复制及试查中保持一致。不新增 API、依赖或 CI 注册。

新组件选择服务后可显式采用基于 Descriptor 字段事实的明细表、类别柱图、时间折线图或空间分布起点，候选显示所用字段；点击后写入同一表单并复用真实试查，必要条件不全时进入条件步骤。`componentDraft.test.mjs` 覆盖可选输出字段、默认顺序、稳定时间字段、显式 geometry 及禁止按名称推断；`application-studio.spec.js` 覆盖中英文创建保存、参数未齐不查询、默认值试查、服务切换清空结果、取消与窄屏。不新增 CI 入口或依赖。

组件展示步骤直接提供字段名称、单位、小数位与日期格式；列宽、状态规则和值名称映射按字段收纳，已配置的高级设置自动展开。显式恢复名称与格式复用共享 `defaultFieldPresentation` 的字段注释及类型默认值，保留列宽、状态规则、值名称映射和查询原值；不按字段名推断业务含义或单位。纯呈现修改复用当前试查结果，共享 `TabularResultRenderer` 按稳定字段键读取当前呈现配置，避免单元格插槽持有旧配置。中英文浏览器回归覆盖新建默认值、编辑即时预览、取消隔离、恢复默认、保存重载与窄屏；共享呈现改动按[标准入口](#标准入口与隔离环境)扩散验证，不新增 API、配置字段或门禁入口。

### 参数联动与名称回显

筛选卡片直接展示绑定组件，并可进入同一点击联动表单预填当前目标条件；来源和原始值字段仍由用户显式选择，取消不写入。组件归组映射复用 `applicationParameterOptions` 校验类型、选项及默认/场景值，并阻止移除既有联动的最后一个目标。联动表单在用户显式选择来源、字段、条件后才写入草稿；取消不写入，同一字段可以赋值给不同条件，目标条件不得重复。`applicationParameterBinding.test.mjs` 与 `application-studio.spec.js` 同时覆盖共享筛选、取消/编辑联动、原始人员 ID 驱动多个图表、预览隔离和窄屏。

新增组件的条件表单可显式选择已有应用筛选，使用其初始值试查。复用候选和提交时校验复用 `applicationParameterOptions` 的同一绑定规则，考虑新组件多个输入的共同选项约束；取消不改应用，应用后只写既有 Parameter Binding，不新增重复条件。`applicationParameterBinding.test.mjs` 和 `application-studio.spec.js` 覆盖类型/选项冲突、显式选择、试查原始值、中英文、保存重开及取消。

运行参数旁的源组件定位入口、Table/Chart/Map 选择提示与参数更新反馈均由既有 Selection Binding 派生，Component 说明直接来自已有 description；普通定位入口不触发名称查询；配置 display_source 的名称回显遵循下述独立契约。上述浏览器门禁同时验证中英文提示、窄屏定位及焦点、定位不查询、选择仍提交原始 ID，以及重置条件仍走既有查询主路径。无新增测试入口或依赖。

仅被一个选择源组件消费、且不作为选择赋值目标的应用参数就近放在该组件结果上方；其他参数保留顶部。两处由 `ApplicationParameterFields` 唯一组合共享 `ParameterValueInput`，不复制输入与契约禁用逻辑。位置由绑定推导、参数状态唯一；`parameters` 可见性、默认值、预设、请求失效与游标重置共同生效。既有确定性和浏览器入口覆盖位置归属、唯一控件所有权、局部筛选提交、翻页后重查与清空、原值选择以及隐藏参数区。

自由文本回车由 `ApplicationParameterFields` 发出提交意图，画布按所在区域复用单组件或全页查询；输入法确认候选、重复键及下拉选项确认不得触发。`metric-publication.spec.js` 覆盖中英文回车查询、局部筛选清空、游标重置和原始 ID 联动。

已配置 Selection Binding 的条件不再显示手填原始值输入框；运行态通过源组件定位选择，组件试查、应用默认值和场景预设通过同一个 ApplicationSelectionDialog 选择。弹窗复用 DataApplicationCanvas 的 Descriptor、查询、结果渲染、分页、值校验与 buildSelectionUpdate，只展示并查询对应源组件，停用自动刷新；点选只回填当前编辑条件，取消丢弃弹窗状态，试查不改变应用默认值、预设或组件历史默认值。来源统一由 applicationParameterSelectionSources 派生，不按名称猜测 ID，不增加 API、快照字段或第二套查询路径。没有 Selection Binding 的普通参数仍复用共享 ParameterValueInput。中英文 T3 覆盖无手填框、选择与取消、默认值与预设隔离、窄屏和原始查询值；无新增 CI 入口。

Application Parameter 可显式配置 `display_source: {source_component_id, label_field}`。取值字段从该源组件到当前参数的唯一 Selection Binding 推导，不重复保存。名称字段必须为已选、可选择的 string 输出；取值字段必须已选且契约允许 eq 过滤，源契约指纹必须匹配。Backend 在保存和发布时拒绝悬空来源、缺失绑定及不合法字段；移除选择来源时一并清除其显示来源配置。没有该配置时只显示选择状态，不猜测名称。

最新名称由 ApplicationSelectionValue 复用统一名称解析器，经原 Descriptor query operation 查询；请求复用 buildComponentQuery，保留固定过滤、排序及当前命名参数，移除列表动态搜索过滤和游标，按原始值 eq 筛选并最多取两行。只接受完整结果中恰好一行、原始取值严格匹配且名称为非空 string 的响应；未找到、多行或未完成、空名称、失败分别显示明确状态。不读目录当前页或其他组件结果，不缓存旧昵称，不保存查询结果。当前 ID、名称来源、契约或命名参数变化立即使旧名称及在途请求失效；组件卸载后不提交响应。运行、预览、试查、初始值和预设使用同一控件及解析路径。名称查询只负责参数回显，不覆盖指标服务的 result_name_field。

搜索列表向导复用用户显式选择的名称字段；仅当该取值字段支持 eq 且名称字段是 string 时，为尚未配置名称来源的目标条件生成 display_source。已有条件可在筛选设置中选择一个既有选择源及名称字段。该变更不新增实体、API 路由、服务依赖或 CI 注册；同步 Swagger，并保留生成与路由覆盖校验。

### 指标与结果呈现

有限值名称（`value_labels`）的映射边界、草稿回显与不可变发布由现有共享前端、Workbench Go 和浏览器门禁覆盖。

Table/Chart 的 date 字段可显式配置 `temporal_format=period` 和 `period`（`grain_parameter/start_parameter/end_parameter`），引用当前 Service Descriptor 的必填命名参数：粒度为 string 且选项为 `total/month`，起止为 date。结果展示只使用成功查询当时的参数快照；按月显示年月，全期显示“所选期间合计”，Renderer Host 同时显示开始日期包含、结束日期不包含的完整范围。不得根据 `bucket` 等字段名推断语义；原始行、排序、选择与导出不变。共享组件只接受已解析的展示上下文，不读取 Service DTO。无新增依赖或 CI 入口。

`metric-publication.spec.js` 的组合回归覆盖人员、日期与粒度变更后的请求参数、月份与零值绘制、服务返回昵称、双向比率精度，以及恢复默认参数且不改写发布快照。浏览器使用固定 Service 响应，不重算指标；真实按月聚合、日期边界和零分母计算继续由 Model PostgreSQL 门禁负责。

`frontend/e2e/metric-publication.spec.js` 使用真实 Vue 页面和内存 HTTP fixture，覆盖契约变化后显式重配组件、保存与发布不可变修订、运行参数选择、异步 Descriptor 就绪、中英文显示及共享参数冲突。Model 的 `metric-workspace.spec.js` 负责服务来源重绑交互；Workbench 回归从重绑后的 Consumer Descriptor 边界开始，不跨模块读取 Model 内部事实。同一浏览器门禁还覆盖 50 行结果在受限卡片中的内部滚动和分页按钮可点击，以及可选 `contains` 输入的字面值提交、翻页后变更筛选重置游标及清空恢复查询；Common 的 PostgreSQL/MySQL `AnalyticalText` 既有 T2 门禁负责真实子串语义，Service T1 负责能力投影和参数绑定。这些测试不等同于真实后端与数据库的在线联调。

指标浏览器回归还验证服务返回的双向主体名称、空昵称、目录当前页缺少主体，以及目录旧昵称不能覆盖指标服务当前昵称；选择联动只提交稳定人员标识。Online suite 复用商务 MySQL 夹具验证持久化草稿的值名称、维度文本、CSV 原值和选择联动原值，只使用可清理的未发布应用；报告缺少任一证据即失败，不把本地 HTTP fixture 通过记为真实 T4 通过。

Chart 的 `total_as_value` 显式开启全期数字卡片，要求期间维度及 1–4 个显式精度度量；Host 仅适配现有共享 ScalarValueRenderer，按月仍走 ChartRenderer。唯一完整行校验与原始行选择事件不得被绕过。覆盖 Go 配置校验、前端配置往返、共享选择契约及中英文全期/月度切换、零值、空/多行/部分结果；同时保留共享前端与完整模块门禁、Swagger 生成及覆盖验证，无新增入口或 CI 依赖。

Chart 的可选 `result_name_field` 显式选择已查询的 string 输出字段，Host 用完整成功结果中的一致名称展示查询对象，标签复用字段呈现配置。空名称和不一致/不完整结果明确提示，未查询或结果失效时不显示名称；不得从目录当前页、其他组件或参数名补全。Model 主体显示名称与 Service 冻结发布包均复用现有契约，Workbench 只经 Consumer SDK 消费。Go 校验、前端配置往返、中英文保存重载、全期/月度切换和缺失/歧义边界由完整模块、前端及 Swagger 生成与覆盖入口验证，无新增 CI 注册或外部依赖。

### 运行展示

`frontend/e2e/runtime-fullscreen.spec.js` 通过真实 Fullscreen API 和鼠标滚轮验证已发布 desktop 应用在宽屏、窄屏下的全屏滚动、下方组件可达和退出后整页滚动恢复，并验证 wallboard 全屏仍将组件约束在视口内。

全期数字卡片在桌面运行与整页预览中仅对独立同高横排收紧：Host 通知实际呈现模式，Canvas 将该排投影为内容自适应行并移动后续行，原 placement 不变。混合图表/表格或跨排组件保留固定网格，编辑画布和大屏不压缩。Node 布局测试覆盖空隙、跨排、还原和快照不变；中英文浏览器回归覆盖全期/月度高度切换、混排、窄屏长说明和四项数值不裁切、大屏隔离。无新增依赖或测试入口。

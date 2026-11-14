# Develop 模块说明

阅读导航：[关键架构](#关键架构)、[数据库文档](#数据库文档)、[开发场景](#常见开发场景)、[注意事项](#注意事项)、[开发与验证](#开发与验证)、[相关文档](#相关文档)。

查询编辑器的 Monaco 配色读取已应用的共享主题 CSS 变量，经浏览器解析为十六进制后，通过 `defineTheme` 原位刷新背景、行号、选择区及补全/悬浮控件；不能把 Element Plus 的 RGB 变量直接交给仅接受十六进制的主题接口。不接受固定主题 prop，不复制 Console 主题协议或系统偏好监听。切换四主题或系统深浅偏好不得替换模型、查询内容、光标、视口及撤销历史，组件卸载时清理主题观察器。回归由现有 `make test-develop-frontend` 和 Develop Frontend CI 自动覆盖。

正式部署 `graph-protection` 的 Develop 四主题验收还必须检查真实编辑区、行号、补全菜单及其选中行配色，并确认打开/关闭补全后查询文本保留。已有浏览器报告增加必需事实 `theme_editor_colors=true` 和 `theme_completion_colors=true`；缺失、非布尔或失败值均拒绝，不以旧图结果画布配色证据代替编辑器验收。复用相同普通 User、合成图、正式构建/Nginx 和清理范围，未增加业务写入、账号或验收入口。最小确定性入口为 `make test-graph-online-runner`，真实结果由同一 Neo4j Hosted T4 单独记录。

导出发起用户来源核验复用 `common/exportartifact`：派发前持久化 Transfer UUID 与完整请求摘要，`POST /runtime/export-sessions/{id}/execution-source` 仅允许 `addp-transfer` 持有 `develop.export_provenance.read` 核验有效待执行会话。只返回发起用户事实，不开放文件下载或数据权限。最小验证入口为 `make test-module MODULE=develop`；Common 变更按 `make test-changed` 扩散验证。

查询参数的可选默认值必须响应属性的新增和删除，保留空串、0、false 与未设置的区别；字段存在性检查不能短路掉 Vue 对缺失 `default` 键的读取跟踪。`make test-develop-frontend` 的实际 Vue 响应式回归和真实 QueryEditor 浏览器用例共同覆盖从 SQL 提取参数后的默认值开关。

## 核心职责

查询预检对已声明的参数复用正式执行契约的默认值合并、类型校验和覆盖解析；SQL、Cypher、MQL 等值参数与 relation 编译使用同一解析入口。不能因调用方仅提交本次覆盖值，就把已声明默认值的参数误报为缺失；预检只读取无凭据 Engine Runtime Descriptor，不提前签发执行授权或读取业务数据。最小回归为 `make test-module MODULE=develop GO_ONLY=1`。

Develop 模块是 ADDP 平台的**开发工作台**，负责以下核心功能：

1. **查询开发** - 按 Engine capability 支持 SQL、MQL、Cypher 等在线查询
2. **GIS 工作流管理** - 可视化编辑和执行空间数据工作流（基于 GeoPython Workflow 运行时）
3. **Jupyter Notebook 集成** - 支持 Python 数据分析和机器学习
4. **算子发现** - 聚合工作流运行时的动态算子（GeoPython Workflow、Spark Workflow 等）
5. **执行历史管理** - 保存 SQL/工作流执行记录，支持历史回溯

Spark Workflow 的加载资源选择器还支持 Elasticsearch `index` locator。执行时由当前用户授权派生单段 `index` 与连接，公开参数仅增加显式数组声明 `array_fields`；保存算子和 GeoPython 不支持 ES。首版仅普通具体索引的 HTTP Basic 只读批量加载，HTTPS 明确拒绝；Spark 集群仍由独立 `spark_cluster_id` 绑定，结果 JSON 中超出安全整数范围的 bigint 保持十进制字符串。

## 关键架构

专题导航：[查询与预检](#查询工作台与预检)、[查询保护](#查询保护与结果交付)、[关系写入](#固定目标写入契约)、[全量导出](#查询全量导出)、[执行监督](#执行输出与运行监督)、[Copilot](#copilot-范围与资源事实)、[执行授权](#执行授权与效果)、[Notebook](#notebook-会话与排队执行)。

### 统一执行架构

```
前端请求（SQL/工作流/Notebook）
  ↓
DevExecutor（校验请求、冻结内容与授权事实，持久化 pending execution）
  ↓
ExecutionSupervisor（按类型额度领取、维护 lease；失联收敛失败，不自动重放）
  ↓
ExecutionService（持有效 lease 调用所属执行器）
  ├─ 查询执行 → SQLEngineService / FederatedQueryService / GraphQueryProvider
  │  ├─ 普通查询通过 System 获取单引擎执行期连接
  │  ├─ 普通 SQL 使用 common/dbbridge 执行查询
  │  ├─ DuckDB 联邦查询按 execution_config.engine_id 解析真实 Runtime Engine
  │  ├─ 独立 DuckDB Runtime 消费授权并取得各 Source Engine 连接
  │  └─ 返回服务端受限的表格或图结果预览
  ├─ 工作流执行 → WorkflowEngineService
  │  ├─ 解析工作流 JSON（DAG 结构）
  │  ├─ 调用 GeoPython Workflow 运行时（矢量、表格与栅格算子）
  │  ├─ 或调用 Spark Workflow 运行时（大数据空间计算，执行时绑定 Spark 通用引擎资源）
  │  └─ 返回执行结果（GeoJSON/DataFrame）
  └─ Notebook（script）→ 当前拒绝实际派发，记录 authorization_unavailable
     ├─ 提交时已经冻结 Notebook 内容 SHA-256
     ├─ NotebookExecutionService / Jupyter 已具备下载字节的 SHA-256 核验
     └─ 待确认并接入可核验撤权的运行授权后，才能启用无头执行
  ↓
TaskExecutionRepository（统一执行记录持久化）
  └─ common.task_executions 表（module=develop）
```

Query、Workflow、Notebook 的每 Backend 额度、默认值、租户覆盖范围与热更新统一见[Develop 执行策略](../docs/spec/configuration/执行与模块策略.md#develop-执行策略)，唯一队列、额度隔离与失联处理见[执行监督与授权](../docs/spec/tasks/模块任务契约.md#develop-执行监督与授权)。持久队列和 lease 不等于远端强制停止：本地失联收敛失败后不自动重放，不能承诺远端程序已经停止。

FalkorDB 业务引擎复用现有 Cypher preflight、参数、Execution Authorization、PreparedGraphQuery、共享图结果与保护门禁，必须显式传递目标 graph locator；无需先创建 Graph 知识图谱实例。`make test-common-falkordb` 的独占真实引擎场景调用 DevExecutor 正式 query 分支，验证执行期连接、表格／图结果、类型化参数、写语句拒绝及受保护目标零结果。该 T2 不是登录／Gateway／浏览器 Hosted T4。支持版本与本地只读语法边界以引擎插件和技术栈规范为准。

### 执行过程事件与当前边界

事件闭合字段、身份派生、Owner 读取裁决和 30 天维护统一见[过程事件规范](../docs/spec/tasks/执行监控.md#通用执行读取与过程事件)；队列、attempt／lease 与失联收敛沿[Develop 执行监督契约](../docs/spec/tasks/模块任务契约.md#develop-执行监督与授权)。Develop 的接入约束是：

- 实际 claim 写 started，完成或过期收敛写唯一终态；事件、execution 和任务最近执行摘要在同一事务提交，事件失败回滚这些事实，过期或错配 lease 拒绝写入。无有效 lease 的遗留 running 只收敛失败概览，不补造过程。
- Query／Workflow 只在已有受控执行阶段记录 progress 百分比，不将自由文本 `current_step` 复制为 `step_id`。
- 授权附加、Workflow 远端执行回执与心跳不追加事件；查询、参数、业务结果、Runtime 完整响应和授权引用不进入通用事件。

Notebook 的交互、数据读取与排队授权，以及拒绝派发和内容摘要错误的区别，统一见[会话与排队执行](#notebook-会话与排队执行)；事件或摘要存在不表示已经调用 Runtime。

标准门禁、Query Hosted 核对要求与分版本证据统一见[开发与验证](#开发与验证)，本地分项、历史通过和未执行项分别认定。

### 算子发现服务

Develop 模块按具体工作流运行时实例聚合算子定义，用于工作流画布。当前内置运行时包括 GeoPython Workflow、Spark Workflow、SuperMap Workflow，以及 Model3D、PointCloud 等专用运行时；用户自研 `addp.workflow/v1` 运行时也走同一发现链路。

算子契约分为三层：

- **Runtime Operator Spec**：来自运行时 `/api/operators` 的 `parameters[]`，只声明运行时真实执行参数。
- **Develop Adapter Spec**：Develop 按 `workflow engine type + operator id` 显式注册资源适配规则，负责 `locator -> connection_info/schema/table/path`。
- **Public Operator Spec**：Develop 算子发现 API 输出的 `public_parameters`，供前端、用户和 AI 使用；资源选择器 UI 也在此层声明。

前端工作流编辑器只消费 `public_parameters`，保存的 workflow definition 只包含公开参数。执行前由 Develop Adapter 派生运行时参数，再把纯 Runtime Operator Spec 参数发送给运行时。

Adapter 可对 Runtime 已有参数提供双语名称和说明投影，但不重复声明其类型、枚举、默认值或显示条件。`raster_save` 的编码及 JPEG 质量由 Runtime 规范拥有；Adapter 仅按选定 profile 派生同一目标的 canonical format、MIME 和文件名校验，普通 PNG／JPEG 沿既有 write 授权与发布路径执行，不创建第二个保存算子。

统一表单会将 Runtime 声明的默认参数一并保存；显示条件不删除已保存参数。因而 TIFF/COG 工作流也可携带整数 `jpeg_quality=90`，但编码器只在 JPEG 路线应用此值；PNG/JPEG 同样不应用 TIFF 的压缩和金字塔参数。验收须核对完整实际保存参数及选定编码，不能以旧枚举的参数集合拒绝新声明的默认值。

算子面板按 Runtime 的 `category_path` 递归显示目录，同名末级组以完整路径区分，父组显示后代算子总数；搜索展开匹配算子的全部祖先，点击与拖拽共用唯一算子卡片实现。GeoPython 的两级四字分类由 Runtime 拥有，不在 Develop 按引擎类型或算子名称维护分类表。最小验证运行 `make test-develop-frontend` 和 `make test-geopython-workflow`，现有 Frontend / GeoPython CI 已覆盖。

直接连接资源的选择范围与执行校验唯一来自当前 Runtime Instance 的 `compute.workflow.storage_access`，不按计算引擎类型生成名单，不在算子 Adapter 保存名单。缺少声明时没有直接连接候选。输入读取与输出写入分别与存储实例的 `storage.store` 能力取交集。受控 access-plan 选择范围复用 Common 解析器的方法映射；操作只声明挂载目录等所需访问方法。标准验证使用 `make test-module MODULE=develop`，Common 契约变更另运行 `make test-go`。

Spark 保存表结果时，Develop 根据目标 schema/database locator 和算子的 ddl 声明，将 write + ddl 授权绑定到目标引擎，源引擎与 Spark 集群仍只读。保存摘要的行数由 Runtime 发布事务返回；命名 outputs、Meta 异步扫描与 lineage_facts 继续复用既有工作流结果主线。

Spark MinIO 保存固定普通 Parquet + create。Develop 以目标名称末段、执行 UUID 的 22 位 Base62 编码和冻结工作流节点序号生成独占子目录，目标引擎需要 read + write 以核验实际成果。稳定输出定位 whole scope item，Meta 扫描使用同范围 prefix；后续 Spark 文件加载从正式 Meta item facts 取得 format/layout，不从扩展名推断。命名、失败与可见性边界见[工作流计算引擎接口规范](../docs/spec/workflow/Develop资源与执行.md#spark-对象存储-parquet-成果)。

文件、对象或目录型持久化转换算子使用 `addp.workflow.access-plan/v1`。工作流定义只保存 `locator`、`target_parent_locator + target_name`、`write_mode` 和公开转换选项；Develop 必须分别解析源、目标存储引擎并构造执行期访问计划，不能用一个共享 `engine_id/connection_info` 覆盖两端资源。转换成功后由 Develop 根据目标生成 `produced_targets` 并提交 Meta 深度扫描。

工作流资源血缘由 Develop 根据本次 execution 冻结并应用参数后的 DAG 生成。读取、实际输出分别使用节点专属端口，每个产出节点只关联直接读取及沿标准 `$ref` 数据流追溯到的来源；嵌套数组、串联节点和批量产物复用同一路径。`depends_on` 只表示执行顺序，独立分支不得互连。Meta 继续消费既有 `lineage_facts`，不反向解析工作流。回归由现有 `make test-module MODULE=develop` 的 Go T1 自动发现；无需新增 CI 入口。

工作流实际产物自动扫描必须先保存本次 execution 的 `metadata.outputs`，再用 Service Bearer 将直接父执行 UUID 传给 Meta；不得自行填写扫描发起人。Meta 原子校验来源并继承归属，提交失败只记录扫描反馈。

画布导入或重开后，纯 `depends_on` 顺序边以虚线保留，保存时只写回依赖，不合成输入 `$ref` 或占用输入参数。实际数据连线继续使用现有输入端口主路径；保存、重开后的参数保真回归由 `make test-develop-frontend` 及既有 Develop Frontend CI 自动发现。

Model3D 的 DAE、3DS、MAX 转 GLB，SuperMap 的 SGM 转 OSGB，以及 Document 的 PPTX 转 PDF 均通过显式 Adapter 接入。DAE、3DS、MAX 从所选主文件定位父目录并保留 entrypoint，以供运行时读取相邻资源；SGM 和 PPTX 使用单文件源。MAX 公开 `source_unit` 与 `texture_files`，PPTX 公开 `strip_embedded_media`；选项的名称和说明使用 Develop 国际化消息，算子发现按请求语言返回。输出只写用户选择的业务存储，复用现有授权、稳定输出与 Meta 扫描，不登记 Manager 私有快显结果，也不在单引擎工作流中隐式调用另一运行时。验证沿用 `make test-module MODULE=develop`，新增 Go 测试由现有 Go CI 自动发现，无新增依赖或门禁。

`vector_to_pmtiles`（矢量切片）复用标准访问计划和业务产物主线，首版只接受已扫描的 Shapefile／GeoJSON 文件或对象。Develop 根据精确 Meta item 身份、格式、字段、范围及 SRID 派生内部 tile；用户只保存源和目标 locator、层级、图层名与显式属性数组。默认不带业务属性，拒绝几何列、未知／重复字段、无效层级、过期 item 身份和缺失空间事实。Runtime 的 access_plan、tile、options 不公开；保存重开继续复用结构化数组参数。验证沿 `make test-module MODULE=develop`、`make test-develop-frontend` 及既有 Develop Go／Frontend CI；GeoPython 原生 GDAL 与 Manager 调用契约另由各 owner 既有门禁覆盖，不新增 CI 路线。

参数面板中的公开 `array/object/json` 参数统一以 JSON 文本编辑，保存时还原结构化值；重新打开已保存节点必须保留数组和对象内容，不得显示 `[object Object]` 或将数组退化为逗号拼接的字符串。浏览器回归由既有 `make test-develop-frontend` 和 Frontend CI 自动发现。

**注意**：Meta、Transfer、Manager 模块提供的是**任务**（Tasks），不是算子，它们主要用于 Orchestrator 工作流编排。

### 专业执行结果读取

只读私有文件工作流的有界表格明细复用 `/executions/{execution_id}/result`；当前本人及来源 read Grant、数据保护逐次检查，最多 5 分钟进程内暂存，历史与 TaskProvider 仍只有摘要。执行详情页通过“查看当次明细”显式加载，复用共享表格、分页和截止清理，用户选择有限数值列求和；下载复用正式导出会话、Transfer JSON/CSV pipeline 和当前用户复核，文件交付不超过原明细截止；不提供客户端 CSV 旁路。边界与限额沿[Develop 资源与执行](../docs/spec/workflow/Develop资源与执行.md)，私有机器交付沿[HTTP 协议](../docs/spec/workflow/HTTP协议.md#工作流执行私有文件)。

Monitor 的[通用安全诊断](../docs/spec/tasks/执行监控.md#执行详情与安全投影)与 Develop 本人专业结果分别授权。Develop 专业详情、轮询、列表、日志与查询导出只读取当前 User 本人的 execution；Tenant、Principal、Membership 和授权版本必须与持久来源事实一致。query/workflow 额外要求当前 `develop.data_read.execute`，script 要求 `develop.notebook.read`，入口仍要求 `develop.task.read`。旧记录无可信主体或授权变化后返回不可见，不回查当前任务定义猜测历史权限；不增加结果分享或重授权路径。新 Script execution 从已验证 AuthContext 保存来源事实。专业响应不暴露执行授权引用；列表不混入同租户其他人的配置及结果。

TaskProvider 状态读取只接受 addp-orchestrator Service，限定 source=orchestrator 且父执行属于同 Tenant 的 Orchestrator。共享状态响应使用安全投影和稳定 outputs，不包含专业结果。标准验证使用 `make test-module MODULE=develop`；Common 变更另运行 `make test-go`，Swagger 生成与覆盖同步。真实认证/Gateway/Worker 联调属于 T4，未运行不能计为通过。

### TaskProvider 边界

Develop 在模块定义中声明 TaskProvider 角色，发布 `query`、`workflow`、`script` 三种任务类型。算子工作流必须先在 Develop 中保存为 `dev_tasks.dev_type=workflow` 的任务定义，再以 `provider=develop, task_type=workflow, task_id=...` 被 Orchestrator 引用。Notebook 是 `script` 任务的当前实现形态和 UI 入口，不作为独立 `task_type`。当前 Develop 不具备 owner scheduler / `next_run_at` due claim 闭环，因此不声明定时能力，不保存或暴露 `schedule`、`enabled`、`next_run_at`。

具体任务详情必须返回任务级 `execution_contract`。工作流契约根据当前保存的 workflow definition 和目标运行时算子动态生成：未被内部连线占用的公开参数默认可覆盖，内部 `$ref` 连线、DAG 和 Runtime/Adapter 派生参数不得暴露；保存算子只声明可持久化的 ResourceLocator 稳定输出。查询契约根据 `content.query_parameters[]` 动态生成，每个参数只保存 `name + type + default? + description?`；参数定义必须与 SQL `:name`、Cypher `$name`、MQL `{\"$param\":\"name\"}` 或 PostgreSQL 裸关系名引用完全一致。所有类型的 `default` 都是可选字段；缺少默认值的参数统一由 `input_schema.required` 声明为执行必填，有默认值的参数进入 `input_defaults`，本次执行或 Orchestrator 均可覆盖。参数分组按稳定 DAG 拓扑顺序声明 `input_ui_schema.order`，查询参数按定义数组顺序声明字段 `order`。资源参数展示统一消费共享资源树 selection：摘要展示引擎实例名称、引擎原生路径和本地化资源类型，geometry 字段由资源空间事实自动回填，单字段直接确定、多字段使用选择控件，不提供自由文本输入。手动执行和 Orchestrator 调用都只提交本次 `parameters` 部分覆盖，未提交字段使用任务保存值，且不得改写任务定义。`script` 当前返回空的闭合输入/输出契约。

TaskProvider、执行状态回查和 Asset 发现属于服务间接口，统一只接受 Bearer Service Access Token 和 canonical AuthContext。TaskProvider 固定由 `addp-orchestrator` 调用，Asset 发现固定由 `addp-asset` 调用；两者还要校验各自精确 Permission。代表用户创建或继续执行任务时必须引用由原 User AuthContext 派生、绑定唯一 execution 的 Execution Authorization；内部调用不能凭 Service Principal 自身权限伪造 User、Tenant、`triggered_by` 或数据访问能力。旧 `/api/v1/develop/internal`、`X-Internal-API-Key` 和 `X-Tenant-ID` 已删除，不保留双轨。

### 前端任务路由

工作流编辑器的引擎展示顺序为 GeoPython、Model3D、PointCloud、SuperMap、Spark、Document；其他扩展类型排在其后，同类型实例保留发现顺序。新建工作流默认选择此顺序中第一个可用实例，优先 GeoPython；编辑已有任务保留其保存的引擎绑定，刷新候选列表不改绑。

Develop 任务编辑器遵守 `docs/spec/addp前端路由与可恢复状态规范.md`。Console URL 必须能够恢复当前 `dev_tasks.id`，canonical 路由固定为 `/develop/{sql|workflow|notebook}?action={create|edit}&id={id}`：创建动作不带 `id`，编辑动作只使用 `id`。`/develop/tasks` 只表示任务列表，`taskId` 旧参数不得保留。

Develop 独立访问根路径或登录后未指定回跳地址时，在 AuthContext 加载后按侧栏顺序选择第一个有权进入的页面：`/sql`、`/notebook`、`/workflow`、`/tasks`。页面准入复用 Console 权限映射；均不可访问时进入 `/forbidden`。显式业务地址仍按原目标独立校验权限。

Develop 前端不提供模块级执行列表。任务行“最近执行”按批量投影的最近受理 execution UUID 直达 Monitor，包含 pending／running；次要“历史记录”仍按任务范围筛选。Monitor“查看执行结果”进入本人领域结果页；该页以受限 return_to 或同一 UUID 返回 Monitor，不重复完整通用详情，入口不依赖失败状态。`/develop/tasks` 和 Notebook 工作区使用 `MonitorExecutionsButton(module=develop)` 进入统一 Monitor，并按 `task_type`、`source_task_id` 收窄范围。`/develop/executions/:execution_id` 仅保留查询、工作流和脚本的领域结果、日志与重试详情，不复制 Monitor 的通用执行列表、统计或筛选器。

查询和工作流编辑器统一使用共享 `useUnsavedChangesGuard`，删除原模块内路由确认与 beforeunload 实现；修改状态仍由编辑器定义。Console 导航与 standalone 导航消费同一共享保护契约。

### 查询工作台与预检

查询工作台固定使用左侧 Meta 技术资源树、右侧编辑器与结果上下分栏。资源树直接消费 Meta resource-tree，不新增 Develop 私有 Engine Catalog API；native query engine 同时是 Runtime Engine 与 Source Engine，因此只展示当前 Engine 的原生路径；声明 `compute.query.federation.supported=true` 的共享 Runtime 不拥有可导航的 Engine Catalog，工作台必须按 `federation.source_engine_types` 过滤并展示当前 Tenant 的 Source Engine 资源树。查询语言、默认语言和结果类型从 `capabilities.compute.query` 读取。即时查询只调用 `POST /api/v1/develop/executions` 创建 `task_type=query`、`source_task_id=null` 的 execution，再按 execution ID 回查结果；不保留 `/develop/execute`。查询任务统一在 `/develop/tasks` 管理，不保留 `/develop/sql-tasks`。

查询编辑器的目录选择是来源上下文；声明联邦查询能力时，选中的 Source locator 只用于资源事实、模板和 Copilot，不得作为原生 `target_locator` 进入预检、即时执行或任务保存。`execution_config.engine_id` 仍绑定所选计算目标，Source 集合由后端解析并冻结；原生查询保留同引擎 `target_locator` 校验，关系参数查询也不携带该原生提示。前端统一使用同一能力驱动的投影，不能按 Spark 或 DuckDB 名称分支。

查询工作台语句诊断统一消费 `POST /api/v1/develop/query-preflight` 返回的 Provider `QueryAnalysis`。前端不得按关键字、正则、通用分词或 Meta 样本字段自行诊断 SQL、MQL、Cypher；`schema_coverage` 为 `sampled` 或 `unknown` 时不得显示“字段不存在”。关系参数编辑态只由 Develop PostgreSQL AST 编译器校验已声明的裸关系参数名、CTE 和作用域结构，覆盖度固定为 `unknown`；执行期 locator 绑定后的真实语义仍由同一 Provider 计划验证。查询编辑器以唯一“查询参数”面板按定义顺序展示全部参数；面板只把裸 `name` 作为唯一参数名，不显示第二套“参数名称”“引用标识”或语言语法字段。`type=relation` 在界面中显示为“数据表”，其余类型显示对应值类型，不再建立“任务输入”“关系输入”“数据输入”“值参数”等平行入口；插入动作按当前查询语言和参数类型生成正确语法。

ES DSL 查询沿用普通 query 执行及 TaskProvider 契约主路径；`POST /query-preflight` 的请求校验与 Swagger 枚举必须包含 `sql`、`mql`、`cypher`、`es_dsl`，预检仍由当前 Engine Provider 判定只读子集。首版不声明参数能力，公共引用分析返回空引用集，空定义形成闭合空输入契约；任何非空参数定义或本次参数覆盖都拒绝，JSON 与受控只读子集仍由 ES Provider 校验。对应回归纳入 `make test-common-elasticsearch-unit` 和既有 Go workspace CI。

### 查询保护与结果交付

#### 读取计划与保护范围

需要读取／输出来源判断时，Develop 只消费同一 `PreparedQuery` 的 `ReadSet()` 与 `OutputLineage()`，使用独立 `query` 动作，在结果写 execution metadata 或返回 Workbench／Notebook Kernel 前完成保护。不自行解析查询或按同名结果列猜来源；纳管匹配、实时结构校验、direct／derived／opaque 边界及 Provider 支持范围统一见[Owner 本地执行契约](../docs/spec/addp数据安全与隐私保护实现规范.md#十owner-本地执行契约)。

引擎范围排除、首次同步与重放以[范围排除契约](../docs/spec/addp数据安全与隐私保护实现规范.md#查询保护的引擎范围排除)为准：同步未确认时拒绝，范围相交或未知时进入同一计划的精确 ReadSet 判断，所需完整来源或保护证明不可得时拒绝。Neo4j 仍由 openCypher 9 AST、物理数据库与内置函数事实证明原生只读范围，模板与正式图查询消费同一不可变计划；同引擎已有纳管目标而本次完整来源无法证明时拒绝。确定性验证沿 `make test-common-neo4j` 和关联 Owner 门禁，不替代真实用户链路。

支持已准备读取计划的联邦 Provider 在同一在途读取屏障内准备 Runtime 会话，按 Source Engine 的真实 Catalog Model 消费来源范围和表级读取集合，并复用 Common 的范围排除与 DataItem 判断。未命中纳管数据才执行同一 Driver 的同一计划；命中表、投影不可用或闭包未知均拒绝，结束／拒绝释放会话。首期 Spark 的 PG 读取使用受控 `ONLY`，MySQL 仅 InnoDB 基础表；不按结果列名猜字段保护，不降低未知 Runtime 的原有保守拒绝。

所选资源的查询模板必须经过一次有界的真实执行才能返回。该验证读取与普通查询共用同一个 `PreparedQuery -> BeginPreparedQuery -> Execute -> protectResult` 路径；受保护资源不得因模板生成而走 `BeginCatalogPath` 的“仅允许未纳管资源”门禁，验证执行也不得绕过字段级保护。

#### 角色选择与独立导出

候选 `GET /query-protection-options` 只观察用户显式选中来源，并使用本人当前角色自查；`content.protection_choices` 保存精确源字段与方式身份／修订。保存（含仅改名称）及执行均复核当前资格；后台角色事实绑定当前 Execution Authorization、attempt、lease，不借用任务作者、Worker 或发布人角色。默认、修订失效和来源消费规则沿[角色方式契约](../docs/spec/addp数据安全与隐私保护实现规范.md#711-默认方式与角色可选方式)，未选择继续默认，失效不静默更换算法。特殊方式首期仅覆盖 SQL／MQL 已证明的直接字段及别名，联邦、关系结果写入、Notebook 与 Transfer 全量导出不消费查询选项。

导出独立保存 `content.export_protection_choices`，由后续查询执行快照冻结，未选时采用导出默认保护。候选、保存与文件交付复核是同步 BFF：只转发当前已认证 User Bearer，不保留凭据或替换为 Service Token，Get/Open 在实际交付前再次核验。角色专用 Transfer EA 绑定共享会话的确切 UUID，并要求现有执行授权创建、Transfer 执行及源引擎读资格；源、主体、Membership／Version 和资格均须匹配，具体规则由上述契约统一维护。

导出创建、状态与文件交付保留 HTTP 409 的 `query_protection_roles_unavailable`（当前角色资格失效或无法确认）与 `query_protection_choice_invalid`（所选方式／修订变化或失效），采用导出专用双语说明和建议；未知码或异常协议按核验不可用处理，不解析正文猜原因。专用文件仍经既有同一路由的当前 User Bearer 获取 Blob，Resource Ticket 不替代完整角色资格；普通默认导出沿既有路径，查询选项不跨出口复用。

#### 实时结果、历史与日志

暂存、当次原值接口和默认历史的规则统一见[Develop 结果与历史契约](../docs/spec/addp数据安全与隐私保护实现规范.md#102-develop-当次原值结果与脱敏历史)；角色所选结果继续按上述角色方式契约单独复核。

实时所选脱敏结果与历史默认预览由同一次 PreparedQuery 分别形成；共享计划仅延后所选字段的原生变换，其余仍走原 SQL／Go 协同保护，不伪造 allow、不重跑查询、不对掩码再次哈希。交付沿现有 5 分钟／大小有界进程内缓存，每次读取核对当前 User 角色及本地投影；失效或淘汰明确要求重新查询。其他字段既有临时原值授权仍独立复核，撤销／到期的原值逐字段从后续快照移除；CAS 与每版独立计时标识防止并发旧读取恢复原值或延长暂存。持久执行历史始终只含默认脱敏预览；详情与 Monitor 共用 `protection_role_choice_unavailable` 中英文说明。

Backend 与查询执行进程共用的数据库连接必须使用 `common/logger.WithoutSQLValues`：ORM 查询日志保留 SQL 结构、级别、耗时和行数，不展开绑定参数；错误只记录固定类别、类型或合法 SQLSTATE，避免驱动回显查询结果参数。过滤不改变绑定值、事务、持久历史和调用方错误链；`Debug/LogMode` 不能移除过滤。执行事务的日志非泄露与数据完整性由 SQLite 和既有 Develop PostgreSQL 门禁共同验证。

#### 受控失败与验证

保护拒绝使用 `error_details.diagnostic` 与共享 `ExecutionFailure`，稳定码保持 `develop.query.protection_required`。Owner 保留原始类型化错误链，准备和结果阶段分别记录 `preparation`、`result`；细分事实与双语建议统一见[诊断规范](../docs/spec/tasks/执行监控.md#租户执行诊断)，无法可靠细分时才用 `data_protection_required`。不从自由文本或引擎类型猜原因，不向通用响应投影敏感正文。`permission_denied` 与 `authorization_invalid` 分别描述已证明的读写权限不足和执行授权失效，不混同保护拒绝；历史通用原因不反推、不补写，工作台、历史与 Monitor 共用同一组建议。

### 固定目标写入契约

固定目标 `target_locator`、`write_mode=overwrite|append`、同 Engine 与单表事务边界统一见[跨模块任务契约](../docs/spec/tasks/模块任务契约.md#跨模块任务边界)。关系结果写入只允许 Orchestrator 父执行调用保存的 `query` 任务，入口限制见[Develop 开发任务](../docs/spec/tasks/模块任务契约.md#develop-开发任务)。

#### 任务输入与编排端口

参数唯一定义 `content.query_parameters[]`、`type=relation`、可选 `default.locator`、裸参数名和执行覆盖规则统一见[TaskProvider 参数契约](../docs/spec/tasks/TaskProvider接口.md#标准响应体)，例如关系参数 `activities` 写作 `FROM activities`。Develop 要求参数绑定、查询 Runtime 与目标位于同一个显式声明 `compute.query.parameters.types=relation` 的 PostgreSQL 方言 Engine，当前包括 PostgreSQL 与 openGauss；`content.query` 只保存单条只读 `SELECT`，不保存其他模块专有 ID。

TaskProvider 将每个关系参数声明为 ResourceLocator 资源输入，没有默认绑定时才要求调用方提供；编排写入另行声明必填 `target_locator`。`input_ui_schema.<参数名>` 使用 `control=resource_tree_picker`，按参数保存顺序暴露独立输入端口，供 Orchestrator 使用任务默认表或分别绑定不同直接上游的稳定 ResourceLocator 输出，不得要求用户手写输出模板。

#### 绑定编译与事务写入

Query Execution Service 使用 PostgreSQL AST 验证关系作用域，只把与已声明 `type=relation` 参数同名的裸关系节点绑定为执行期 locator，生成仅完成关系绑定的 SELECT，并交给声明 `compute.query.table_result=true` 的 Provider 冻结计划。CTE、子查询和 JOIN 可以使用，但 CTE 与参数重名、未声明参数、未使用声明、真实物理关系、schema 限定关系、表函数数据源、未声明 `relation` 的引擎、非 PostgreSQL 方言查询或跨 Engine 输入必须拒绝。Develop 只使用当前父 execution 派生的精确 Engine `read + write` 授权，不获得 DDL effect；稳定输出为 `execution_id + target_locator + row_count`。Develop 不调用 Model API，不持有 Model Permission。

事务锁、读取集合复核、`INSERT SELECT`、目标限制和实时字段位置映射由[TableResultProvider 契约](../docs/spec/addp引擎插件接口规范.md#queryruntimeprovider)唯一定义；Develop 不经 dbbridge 拼接或执行写 SQL，对冻结 ReadSet 的每个来源使用 unmanaged 保护门禁并保持至事务结束。

#### 成功事实与预览边界

业务写入提交后，Develop 将 Provider 的实时结构和字段位置映射构造为 `lineage_facts`，未知映射标记 unavailable；事实与成功终态经当前 lease 校验一起落库，随后通知 Meta。完整输入来源、`overwrite → replace`／`append` 映射、跨库提交与通知重试统一见[固定正式表查询血缘](../docs/spec/addp数据血缘能力规范.md#51-固定正式表查询写入)。建表依赖不产生数据派生边，失败和失去 lease 的执行不发布成功事实。回归沿现有 `make test-module MODULE=develop`，分项通过不替代真实编排、授权、写入和血缘验收。

查询工作台即时执行和 Develop 手动任务执行使用同一参数解析与 PostgreSQL AST 编译器，但不接收 `target_locator`：关系参数应用默认绑定及本次覆盖后编译为只读物理表查询，返回受限结果预览。Orchestrator 执行在相同有效输入上增加独立结果目标并由 Query Execution Service 编译为写入；输出目标不是查询参数。

### 查询全量导出

查询结果区必须区分有限预览与全部结果导出：预览只用于有界展示，不提供“下载预览”旁路；单 Engine 表格查询 execution 成功后，只有当前实例声明 compute.query.read_session 且支持该查询语言时才启用“导出全部结果”，不以预览是否截断为前提；缺少能力时禁用入口并提示暂不支持完整查询结果导出。后端创建前通过 System Runtime Descriptor 复核同一实例能力，缺少能力返回 422/query_export_unsupported，描述读取失败仍为 503，不先签发执行授权或创建导出会话。DuckDB 联邦查询在具备统一流式查询 Provider 前不得显示该入口。

`POST /api/v1/develop/executions/{execution_id}/exports` 使用成功 execution 的只读快照，通过 Common Transfer Client 创建无任务定义的 bounded `sync`。快照字段、全部关系输入的稳定排序、同计划 ReadSet 校验和流式读取统一见[查询导出契约](../docs/spec/tasks/模块任务契约.md#develop-开发任务)，不得重新解析 SQL 或只记录单个输入。

Develop 使用 Common 导出会话在 Workbench 轮询并下载 infra 暂存产物，不选择业务存储、不触发 Meta scan、不保存 Transfer task ID。格式、文件名、状态与下载复用 `common-frontend` 的唯一导出组件；当前 User Bearer 与交付前保护复核见[角色选择与独立导出](#角色选择与独立导出)。

### 执行输出与运行监督

稳定输出只持久化到 `common.task_executions.metadata.outputs`，状态响应的顶层闭合 `outputs` 是同一对象的投影，不读取 `metadata.result.outputs`。响应与提取规则由[TaskProvider 正文](../docs/spec/tasks/TaskProvider接口.md#标准响应体)统一维护；Develop 的 `GET /task-provider/executions/{execution_id}` 复用 `common/taskprovider`，用户详情附带任务信息也不增加第二套输出提取路线。

Backend 内嵌唯一监督器统一领取。execution 的创建入口、冻结输入、授权签发／消费与失联不重放见[执行监督契约](../docs/spec/tasks/模块任务契约.md#develop-执行监督与授权)，事件提交见[Develop 接入约束](#执行过程事件与当前边界)。监督器不重读可变任务定义；失联失败不表示远端已停止。

提交通知同时唤醒原准入／恢复循环及领取循环，原准入刷新结束后再唤醒领取；仍由 System 当前观察及同一 claim 门禁决定是否可运行，周期扫描间隔与并发策略不变。监督器回归覆盖通知到达时准入仍未完成、完成后立即领取和长周期下无需等到下一轮扫描。

容量观测不参与 claim。每类型以 `execution_supervisor/<task_type>` 上报当前 Backend 的容量和活动数；预算、租户可配置范围及热更新沿[执行策略](../docs/spec/configuration/执行与模块策略.md#develop-执行策略)，不以心跳容量代替执行授权或领取结果。

### Copilot 范围与资源事实

查询工作台 Copilot 只在当前选中的 Query Runtime 范围内生成候选查询语言。前端必须提交当前 Runtime `engine_id` 和 capability 声明的 `query_language`；已有具体 data item 选择时直接提交其 locator（联邦 Runtime 下 locator 保留 Source Engine ID），已有明确容器范围但尚未确定具体 data item 时通过 `resource_scope_locator` 提交 discovery scope，未选择范围时 Copilot 通过带该 Runtime `engine_id` 的共享 `data.search` 粗筛。范围枚举统一走 `resource.children.list → resource.facts.get`，全局发现统一走 `data.search → resource.ancestors.get → resource.facts.get`。同一输入角色存在多个候选时由用户确认一个。Copilot 不得扫描其他工作台 Runtime、拼接 locator、假定字段名或直接执行生成结果；生成的 `query` 和 `query_parameters[]` 必须作为同一查询草稿原子回填编辑器与参数面板，之后仍走同一 preflight 和 execution 主路径。

Copilot `resources[]` 只允许提交具有 `item_id`、可由 Owner `resource.facts.get` 返回平台 `data_type` 和字段事实的具体数据项。已选具体 MongoDB collection 且编辑器为空时，前端必须直接提交该 collection，不能用空 MQL 解析结果覆盖选择。MongoDB `database` 等容器节点可以作为查询执行范围和 discovery scope，但不是 AI 输入资源，不能提交到 `resources[]`。MongoDB 查询已有 MQL 时，前端从当前合法 command object 提取 `find/aggregate/count/distinct` 主 collection 和 `$lookup.from`、`$graphLookup.from`、`$unionWith` 引用，在该 database 资源树节点下逐一匹配具体 collection locator，并以这些 collection 的 `ResourceFact` 调用 Copilot；数据库执行范围仍只保存在 Develop 的 `target_locator` 中。只选 database 且编辑器为空时以 `resources=[] + resource_scope_locator=<database locator>` 调用共享范围发现，由 Copilot 通过 Owner Tool 枚举并返回当前 database 的具体 collection 候选，用户确认后再提交。已有 MQL 的 collection 引用不存在、跨出当前 database 或不唯一时直接要求用户澄清，不退回范围枚举或模糊发现，也不保留 `resources=[] + current_query` 的无字段事实旁路。

查询工作台必须用统一澄清弹窗消费 Copilot 的结构化 `clarifications[]`。资源确认与计算规则、统计对象、时间范围、聚合维度、实体匹配、字段映射、去重/空值/分母规则等语义澄清使用同一交互框架；前端只按 `control` 渲染并提交 `clarification_answers`，不得识别“重叠度”等业务措辞或自行决定计算口径。用户取消澄清不能改写编辑器；回答后保留原问题、资源、语言和当前查询继续生成。Toast 只用于网络、权限、模块不可用和非法响应等系统错误，不能承载可恢复的用户澄清。

Copilot ResourceFact 必须保留 Owner 提供的 `source_engine_type`、`full_name`、`query_references` 和 `schema_coverage`；查询引用逐段保留Owner确认的原名，前端或Copilot不得从locator/full_name猜测或改写；渲染复用common-frontend唯一实现。Query Runtime 与 Source Engine 分开传递，联邦 Runtime 按 `federation.source_engine_types` 校验来源。未注册 validator 的查询语言直接澄清，不生成候选。

### 执行授权与效果

已提交私有文件成果的最小机器回执、真实必要来源和证据缺失规则见[生产来源规范](../docs/spec/tasks/生产来源与产物扫描.md#develop-产物自动扫描的执行归属)。读取入口只允许 System／Meta Tenant Service；首次发布或原操作核清确认提交时原子保存待办，现有 Supervisor 恢复周期沿统一协议办理 Meta 精确识别及 System 初始 Grant，不重算、不重发发布。实际读取 Engine 版本随 ReadSet 冻结，缺失历史不补造；授权不依赖 deep 或完整性标记，读取端和发布端仍执行各自必要的一致性检查；普通手工工作流入口仅接收 basic 已识别的单文件 GeoPackage 来源和可选唯一 `mode=fail` 的精确 GeoPackage 保存目标，临时内容与已保存任务复用原 Intent／EA／Scope；保存执行等待原单次批准，后台仅将当前 `ready` 激活领取，不开放混合来源、覆盖、追加、多目标或委托／父执行派生。真实跨模块验收继续保持独立确认边界。验证复用 Develop Go、Develop PostgreSQL、System `production-receipt-runtime` 迁移和授权清单标准门禁。

- SQL、Workflow、Notebook 执行效果固定为 `read | write | ddl | external_effect`。
- `develop.task.execute` / `develop.notebook.execute` 只允许使用执行功能，不自动授予任意数据效果。
- 当前 User AuthContext、owner Resource Rule/Policy、Engine 归属和执行效果共同决定 Execution Authorization；Engine 创建人和 Runtime Service Principal 都不是授权来源。
- 异步执行只保存 Execution Authorization ID、发起 Principal、Tenant Membership、签发授权版本和脱敏效果/资源摘要，不保存 User Token、Service Token、明文连接或 Workflow Access Plan。
- 算子工作流的存储 Engine 绑定来自 `content` 中的标准 ResourceLocator（主要位于 `workflow_definition`），不是 `execution_config.engine_id` 的工作流运行时绑定。Engine 删除后任务定义和旧 Locator 保留；用户在 Develop 显式选择新存储 Engine 后，Develop 原子改写该旧 Engine 的全部 Locator，保留 path/type 并清除旧 Meta `node_id/item_id`。System 不跨模块回写任务，也不按名称自动匹配新旧 Engine。
- DuckDB 联邦查询先从 SQL 中解析已注册的 Source Engine 引用，为本次 execution 一次性签发只读 Execution Authorization，再由独立 DuckDB Runtime 按 Engine 逐个消费执行期连接；当前联邦查询必须至少引用一个 Source Engine。普通引擎的可执行样例查询也必须先签发并消费单 Engine 的只读 Execution Authorization，再实时发现真实表。两条路径都不得用 `tenant.develop_runtime` 的通用 Engine 明文读取权限替代。
- 联邦查询入队前冻结 `federated_query_binding` 私有执行快照（目标ID、Runtime ID、受众及源ID集合），同时覆盖 ad-hoc、保存任务和 Orchestrator 子执行。领取执行后按固定身份复核，不重新选择 Runtime 或解析同名源；计算集群的授权范围与真正的数据源分离。任务定义不保存此字段，缺失的旧联邦执行快照拒绝执行，不保留兼容派发。纯身份/序列化与 HTTP 边界由 Develop Go T1 自动发现，真实 JSONB 领取恢复沿既有 `make test-develop-postgres` 的 Trace 查询用例验证。
- DuckDB 查询工作台的 `execution_config.engine_id` 始终保存平台共享 Runtime ID，数据源选择结果的 ResourceLocator 与 Copilot resource fact 始终保存真实 Source Engine ID。联邦 SQL 使用 Source Engine 名称的规范标识符作为首段；不得向 Meta 请求 DuckDB Runtime 的 resource-tree，也不得把 Source Engine ID 改写为 Runtime ID。
- 查询样例只允许来自当前 Engine Instance 的实时 Engine Catalog 且必须指向确认有数据的 leaf；DuckDB 对象表还必须通过只读 Execution Authorization 取得执行期连接并真实读取成功，不能仅凭 Meta 条目存在就返回。Engine Catalog 发现失败、对象已失效、无数据或无法构造查询时返回明确错误，前后端都不得回退到 `SELECT 1`、版本查询或占位集合名。
- 查询任务必须在 `content.target_locator` 保存所选资源的标准 ResourceLocator；模板生成、即时执行、异步任务重载都必须将其解析为同一 `EngineCatalogPath`。MongoDB Engine Instance 只绑定服务端点与认证主体，`connection_info.database` 仅作为工作台初始选择，数据库和集合授权以 MongoDB 用户 roles 为准。
- 查询编辑器必须把样例返回的真实语言（如 `sql`、`mql`、`cypher`）原样带入执行和任务定义。非 SQL 查询不得进入 SQL 效果分类器；各 Query Runtime 必须在 `QueryOptions.ReadOnly=true` 时建立等价只读边界。

Notebook 的交互凭据、数据访问、文件与 Kernel 绑定、Copilot 及排队限制见[Notebook 专题](#notebook-会话与排队执行)。

### Notebook 会话与排队执行

交互 Session 与排队 `script` 分别授权；前者的只读数据授权不能用于后者派发。Runtime 内部接口与独立会话的职责见[Jupyter README](../engines/jupyter/README.md#api-接口)，签发、主体绑定、期限及撤销统一见[Notebook Session Authorization](../docs/spec/addp授权上下文规范.md#53-notebook-会话授权)；按职责查代码见[Notebook 实现导航](#notebook-实现导航)。

#### 会话入口与凭据

Jupyter 只通过 Develop 受控会话使用数据访问能力，不直接返回共享 Lab 数据访问入口，不注入长期明文 Engine 连接。

Notebook 交互编辑只允许 `POST /notebooks/{id}/sessions` 创建的短期隔离会话。Develop 返回同源 `/notebook-sessions/{session_id}/...` 路径并设置仅限该路径的 HttpOnly 能力 Cookie；浏览器不得获得 Runtime 地址、Jupyter Token、Service Token 或 URL Token。Develop 在代理 HTTP/WebSocket 前校验会话、Tenant、User、Task 和到期时间；会话关闭、过期或 Develop 重启后 fail-closed。

Notebook Kernel 获取当前可用查询 Engine 及其实时 Engine Catalog 时，只允许使用 Develop 为同一交互会话签发的短期 Notebook Kernel Capability Token 调用该 Session 的只读 Engine Runtime Descriptor 与 Engine Catalog 代理。Token 绑定 Session、Tenant、User、Task 与 TTL，Develop 只保存 Hash；Runtime 只注入隔离 Kernel process，不写入 Notebook、日志或公开会话响应。接口不得返回 `connection_info`，也不得把 Notebook Script Engine 或长期数据连接伪装成当前可用查询 Engine。

Develop 在创建 Session 的同步请求栈用当前 User Bearer 请求 System 签发授权，随后丢弃 Bearer，只在内存 Session 保存授权 ID。后续目录与查询／扫描的独立只读 Execution Authorization 派生，由 `addp-develop` 消费该 Session Authorization，不借用通用 `system.engine.read`；连接留在 Develop 受控 Runtime，不返回 Kernel。关闭、过期、撤权、登出或 Develop 重启后取消活动查询并 fail-closed；派生授权期限及令牌轮换规则沿上述正式会话契约。

#### 数据访问与 Native Facade

Notebook Native Engine Facade 只存在于 `common-python`，按具体 Engine 原生术语把 `schemas()`、`tables()`、`collections()` 等调用编译为统一 Engine Catalog 请求。Develop 不新增 PostgreSQL/MySQL/MongoDB/MinIO 专用目录 API，不自行拼接 `EngineCatalogPath`，不为未知引擎提供暴露内部 Engine Catalog 术语的 fallback。

Notebook 数据读取统一进入当前 Kernel Session 下的 `table-scans`、`record-scans`、`queries`、`graph-samples`、`graph-queries`、`content-reads`、`change-streams` 代理。Develop 每次派生独立只读 Execution Authorization，并按 `common` Provider 契约选择表游标、动态记录游标、图、内容、range 或 change stream；不得为具体引擎增加旁路端点。

Notebook 数据源注入在接入同一 Execution Authorization 消费路径前必须 fail-closed。各读取方式与正式 Provider 的对应关系、Notebook 独有读取限制见[Facade 读取契约](../docs/concepts/addp引擎体系图.md#读取契约与授权)，路径缓存与期限规则见[目录新鲜度](../docs/concepts/addp引擎体系图.md#目录新鲜度与调用期限)。

#### 文件与运行时重绑

新建空白 Notebook 与上传 Notebook 都创建同一种 `script` 任务和 MinIO 对象，进入同一交互 Session。Runtime 在会话创建时按任务绑定的 Notebook owner 路径装载文件；关闭／TTL 清理时原子保存回同一路径并终止 kernel/process，Runtime 顺序与隔离细节见[交互会话控制面](../engines/jupyter/README.md#4-交互会话控制面)。不恢复共享 Lab、直连 Runtime 或第二套 Notebook 实体。

Notebook 任务允许用户显式重绑定原任务的 Script Engine 和 Kernel。重绑定只更新任务定义并影响后续执行；历史执行保留创建时的 `execution_config` 快照，不复制任务或 Notebook 文件，也不自动选择替代引擎。

#### Copilot 生成与插入

Notebook Copilot 只允许通过 `/notebook-copilot-sessions/{session_id}/generate` 使用当前 Session Authorization。首次请求由 Copilot 理解输入角色和跨语言检索词，Develop 只在 Session 实时 Engine Catalog 内粗筛；所有推断数据源都必须展示给用户逐角色确认。确认后 Develop 重新验证路径并通过只读 Execution Authorization 获取 Engine Catalog facts，再调用 `notebook.draft.generate` 生成 Python。生成代码以受控表扫描后的 Pandas/GeoPandas 分析为唯一主路径，不生成 `engine.sql(...)` 查询；SQL 等查询语言生成属于查询工作台。最终 DataFrame 列名、图例和坐标轴等用户可见标签必须跟随用户请求语言，并在面积、距离等指标中标明单位。不得调用租户级 `data.search`、信任前端路径或把 Query Workbench 的单引擎范围套到 Notebook。

Notebook Copilot 生成结果只允许通过同源 JupyterLab bridge 插入当前 Session 的新代码单元。Bridge 必须校验父窗口来源和 Session ID，不得执行单元、覆盖 `.ipynb` 文件或操作其他 Session。

#### 排队执行与内容摘要

类型声明仍为 `script`，Notebook 不是独立 task_type。当前领取后以 `develop.script.authorization_unavailable` 收敛失败，不调用 Jupyter Runtime；该码描述 Develop execution，并非 Runtime HTTP 拒绝。排队运行授权及撤权闭环尚待接入和独立验收，不能借用 Session、数据授权、历史提交人或 Service Token 放行，具体要求沿[执行监督与授权](../docs/spec/tasks/模块任务契约.md#develop-执行监督与授权)。

提交时冻结的 SHA-256 只证明文件字节一致，不授予运行资格。Jupyter 已有实际下载字节校验，内容变化以 `notebook_content_changed` 拒绝并要求重新提交，不创建快照或自动重试；摘要缺失／非法、路径和 Kernel 限制见[Runtime 执行契约](../engines/jupyter/README.md#2-执行-notebook)。已有无头接口、健康探针或交互 Session 成功不等于排队派发可用；门禁职责见[Notebook 验证分工](#notebook-验证分工)，分版本结果见[历史证据](#历史证据)。

### IAM Permission

Develop 是 `develop.task.*`、`develop.notebook.*` 和 `develop.catalog.read` 的 Permission owner；定义只存在于 `authorization/permissions.yaml`，通过 `common/authorization` 发布期聚合，不在服务启动时动态注册。`develop.catalog.read` 不可委派、不可定制，只授予内置 `tenant.catalog_runtime`，且两个 Catalog owner 路由同时固定校验 `addp-catalog` Service Client。`develop.task.cancel` 是 IAM 目标目录能力，当前真实执行取消入口仍待路由覆盖阶段确认。

### 企业 Catalog 发布边界

Develop 只对已持久化且可重复使用的 `dev_tasks.dev_type=query|workflow` 发布 owner-local 变化流和当前摘要动态解析，由 Catalog 建立 `development_artifact` 企业目录身份。`script` / Notebook、即时查询、`common.task_executions`、执行结果、Notebook Session 和 ToolApproval 均不发布为企业目录资源。

`develop.catalog_resource_changes` 只是可恢复的变化传输事实；Catalog 保存的名称、说明、任务类型、专业状态和必要 Engine ID 是最小已观察投影，不是 Develop 数据备份。`content`、查询文本、工作流 DAG、参数、物化输入、执行配置、Engine 绑定和执行契约始终只归 Develop，Catalog 当前详情通过 `/api/v1/develop/runtime/catalog-references/resolve` 动态解析。

## 数据库文档

### 架构说明

[数据库架构](frontend/docs/数据库架构.md)维护主表、支撑存储、初始化边界与 TaskProvider 路由；当前定义与各次 execution 的冻结输入分别保存，不能从可变定义反推历史。

### 单表文档

| 阅读目的 | 正文 |
| --- | --- |
| 任务字段、模型索引与非持久投影 | [dev_tasks 表结构](frontend/docs/tables/dev_tasks表.md#二表结构定义) |
| 执行策略、导出会话、目录变化流及本地保护存储 | [支撑持久化](frontend/docs/数据库架构.md#支撑持久化) |
| 初始化阶段、已提交变更与失败边界 | [初始化与验证](frontend/docs/数据库架构.md#初始化与验证边界) |
| 定义覆盖、执行快照与当前版本缺口 | [并发与快照边界](frontend/docs/tables/dev_tasks表.md#25-定义覆盖执行快照与并发缺口) |
| 用户任务定义、参数执行与存储 Engine 重绑 | [任务表 API](frontend/docs/tables/dev_tasks表.md#四api-端点说明) |
| 委托 Tool 审批与一次性消费事实 | [tool_approvals 表](frontend/docs/tables/tool_approvals表.md) |
| 统一执行记录的安全摘要与本人专业结果 | [通用安全投影](../docs/spec/tasks/执行监控.md#执行详情与安全投影)、[专业读取](#专业执行结果读取) |

Develop 执行记录归 `common.task_executions`，不维护私有执行表。修改持久结构或 API 时同步对应单表说明；标准门禁与分版本结果见[开发与验证](#开发与验证)。

## 重要文件位置

### 核心服务文件

- [dev_executor.go](backend/internal/service/dev_executor.go) - **统一执行入口**（冻结执行快照后持久入队）
- [execution_service.go](backend/internal/service/execution_service.go) - Workflow 的领取后授权、专业回执与带 lease 终态执行服务
- [query_execution_service.go](backend/internal/service/query_execution_service.go) - Query 的冻结快照、授权与带 lease 终态执行服务
- [execution_supervisor.go](backend/internal/worker/execution_supervisor.go) - Develop Backend 内嵌唯一执行监督器
- [sql_engine_service.go](backend/internal/service/sql_engine_service.go) - SQL 执行服务
- [workflow_engine_service.go](backend/internal/service/workflow_engine_service.go) - GIS 工作流执行服务
- [operator_discovery_service.go](backend/internal/service/operator_discovery_service.go) - **算子发现服务**（聚合工作流运行时动态算子）
- [dev_task_service.go](backend/internal/service/dev_task_service.go) - 工作项（SQL/工作流）管理服务

### API 路由文件

- [backend/internal/api/router.go](backend/internal/api/router.go) - HTTP 路由定义
- [backend/internal/api/query_handler.go](backend/internal/api/query_handler.go) - 查询预检、真实模板与连接测试
- [backend/internal/api/execution_handler.go](backend/internal/api/execution_handler.go) - execution 创建、当次／历史读取与导出
- [backend/internal/api/operator_handler.go](backend/internal/api/operator_handler.go) - 算子发现 API

### 前端视图文件

- [frontend/src/views/QueryEditor.vue](frontend/src/views/QueryEditor.vue) - SQL/查询编辑器
- [frontend/src/views/WorkflowEditor.vue](frontend/src/views/WorkflowEditor.vue) - GIS 工作流编辑器
- [frontend/src/views/TaskManagement.vue](frontend/src/views/TaskManagement.vue) - 开发任务定义与统一监控入口
- [frontend/src/views/ExecutionDetail.vue](frontend/src/views/ExecutionDetail.vue) - 查询、工作流和脚本领域执行详情

### Notebook 实现导航

以下只定位代码职责，会话、权限和派发边界统一见[Notebook 专题](#notebook-会话与排队执行)；文件存在不表示相关链路已经验收。

| 职责 | 实现入口 |
| --- | --- |
| 文件创建、上传、下载与运行时重绑；Runtime 连接 | [Notebook handler](backend/internal/api/notebook_handler.go)、[Jupyter client](backend/internal/service/jupyter_service.go) |
| 会话创建、关闭、HTTP／WebSocket 代理及 Kernel 目录／表／查询入口 | [Session handler](backend/internal/api/notebook_session_handler.go)、[Session service](backend/internal/service/notebook_session_service.go) |
| System 会话授权签发、消费、派生与撤销 | [Session control plane](backend/internal/service/notebook_session_control_plane.go) |
| 动态记录、图、内容与变更流读取 | [Native data handler](backend/internal/api/notebook_native_data_handler.go)、[Native data service](backend/internal/service/notebook_session_native_data.go) |
| Python 原生引擎 Facade 与受控读取客户端 | [common-python Notebook engines](../common-python/addp_common/notebook/engines.py) |
| Copilot 候选发现、确认后事实复核与代码生成 | [Copilot handler](backend/internal/api/notebook_copilot_handler.go)、[Copilot service](backend/internal/service/notebook_copilot_service.go) |
| 编辑器、会话请求与生成单元插入 | [NotebookEditor](frontend/src/views/NotebookEditor.vue)、[Notebook API client](frontend/src/api/notebook.js)、[JupyterLab bridge](../engines/jupyter/labextension/src/index.ts) |
| 无头执行客户端与当前排队拒绝分支 | [Notebook execution service](backend/internal/service/notebook_execution_service.go)、[Execution service](backend/internal/service/execution_service.go) |
| Runtime HTTP 接口与隔离会话生命周期 | [Runtime API](../engines/jupyter/api_server.py)、[Interactive sessions](../engines/jupyter/interactive_sessions.py) |

### 配置文件

- [backend/internal/config/config.go](backend/internal/config/config.go) - 配置加载逻辑
- [根环境变量示例](../.env.example)提供 `DEVELOP_*` 配置示例，实际配置按[配置规范](../docs/spec/addp配置介绍.md)维护。

## 常见开发场景

查询工作台的编写、参数执行、保存与导出步骤见[查询任务操作指南](frontend/docs/SQL任务功能说明.md)，接口与持久字段分别引用现行专题。

以下请求中的 `8185` 是 Develop Backend 的开发首选端口，须按启动输出和 `.dev-state/ports.env` 替换为实际地址；公开入口另按[端口规范](../docs/spec/addp端口分配.md#前端-docker-端口规则)确认。使用当前有权调用相应入口的 User Bearer，任务定义 ID 与 execution UUID 分别取自对应响应，不沿用示例数字。专业读取范围见[本人执行读取](#专业执行结果读取)；保存任务、创建 execution 或 HTTP 请求成功均不代表业务执行完成。

### 场景 1：执行 SQL 查询

目标发现和临时执行使用同一查询主路径：

| 步骤 | 入口与要求 |
| --- | --- |
| 发现目标和模板 | `GET /api/v1/develop/engines` 返回真实实例；`GET /api/v1/develop/engines/{id}/sample-query` 可用 `locator` 指定数据项。查询语言、原生／联邦来源上下文及模板保护见[工作台与预检](#查询工作台与预检)。 |
| 提交本次查询 | `POST /api/v1/develop/executions` 使用 `dev_type=query`、`content.query_type`／`content.query` 与真实 `execution_config.engine_id`；字段、冻结输入及 `source_task_id=null` 沿[Develop 任务契约](../docs/spec/tasks/模块任务契约.md#develop-开发任务)。 |
| 选择执行目标 | 原生查询绑定该查询 Engine；DuckDB 等联邦查询绑定真实 Runtime Engine，来源由后端解析和授权，不另复制请求形态或使用虚拟 ID。 |

参数定义与本次覆盖见[参数契约](../docs/spec/tasks/TaskProvider接口.md#标准响应体)，超时和预览上限沿[执行快照](../docs/spec/configuration/执行与模块策略.md#develop-执行快照与部署边界)。创建响应只用于取得本次 execution UUID；随后按读取目的选择入口：

```bash
# 工作台：读取当次结果，重新核对当前身份、保护和有效期
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8185/api/v1/develop/executions/<execution_id>/result"

# 历史专业详情：读取默认保护的持久预览与诊断
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8185/api/v1/develop/executions/<execution_id>"

# 本人查询执行列表；Develop 专业列表使用 dev_type 过滤
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8185/api/v1/develop/executions?dev_type=query&page=1&page_size=20"
```

当次结果与历史的保护和暂存规则见[结果交付契约](#实时结果历史与日志)，完整结果另走[全量导出](#查询全量导出)，不把列表摘要或历史预览当作当次原值。页面操作与公开入口见[查询指南](frontend/docs/SQL任务功能说明.md#操作步骤)；真实授权、完整结果、清理及 HTTPS 覆盖按[验收索引](#开发与验证)认定。

### 场景 2：创建算子工作流

按同一运行时与同一候选定义完成发现、校验、保存和执行：

| 阶段 | 入口 | 取值与边界 |
| --- | --- | --- |
| 发现实例和算子 | `GET /api/v1/develop/workflow-engines`；`GET /api/v1/develop/workflow-engines/{workflow_engine_id}/operators` | 算子和公开参数取自所选实例的 Public Operator Spec，分层与派生沿[算子发现服务](#算子发现服务)。 |
| 校验候选 | `POST /api/v1/develop/workflow-validations` | 提交 `workflow_engine_id` 和完整 `workflow_definition`；不创建 execution。校验结果与后续保存必须对应同一候选，修改后重新校验。 |
| 保存定义 | `POST /api/v1/develop/task-definitions` | `dev_type=workflow`，将已校验 definition 放入 `content.workflow_definition`，同一 Runtime ID 写入 `execution_config.engine_id`；返回任务定义 ID。 |
| 手动执行 | `POST /api/v1/develop/task-definitions/{id}/execute` | `{id}` 来自保存响应；可不提供覆盖，或按详情的 `execution_contract` 提交本次 `parameters`，不能传入未声明的 `input_table` 等字段。 |

以下是保存请求的字段示意。Runtime `12`、存储 Engine `3`、资源路径、算子与参数均须按实际发现结果替换；两个 Engine ID 分别承担运行与资源绑定，不能互相推定：

```json
{
  "name": "缓冲区分析",
  "dev_type": "workflow",
  "content": {
    "workflow_definition": {
      "tasks": [
        {"id": "load_data", "operator": "load", "params": {"locator": "addp://engine/3/path/public/cities?type=table"}, "depends_on": []},
        {"id": "buffer", "operator": "buffer", "params": {"input_gdf": {"$ref": "load_data"}, "distance": 1000}, "depends_on": ["load_data"]},
        {"id": "to_geojson", "operator": "to_geojson", "params": {"input_gdf": {"$ref": "buffer"}}, "depends_on": ["buffer"]}
      ]
    },
    "inputs": {}
  },
  "execution_config": {"engine_id": 12}
}
```

完整公开参数必须先保存在任务定义；执行覆盖不补齐不完整任务，也不改写默认值。内部 `$ref`、DAG 结构与 Runtime 绑定不属于可覆盖输入；动态契约、资源重校验和稳定输出统一见[工作流任务输入与输出](../docs/spec/workflow/Develop资源与执行.md#工作流任务执行输入与输出)。

该示意展示工作流内部数据流，不承诺 `to_geojson` 的内存结果已经成为业务资源或跨任务稳定输出；需要持久结果时，使用目标实例的保存算子与标准 ResourceLocator。执行受理、Runtime 返回、业务产物和血缘／扫描各按[执行监督](#执行输出与运行监督)及[验收索引](#开发与验证)分别核对。

### 场景 3：调试工作流执行失败

先按[本人专业读取权限](#专业执行结果读取)定位失败 execution，再读取该轮的受控诊断；`<execution_id>` 使用真实 UUID，不使用任务定义 ID：

```bash
# 1. 查看本人可读的失败执行
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8185/api/v1/develop/executions?status=failed&page=1&page_size=10"

# 2. 查看同一 execution 的详情与受控诊断
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8185/api/v1/develop/executions/<execution_id>"
```

Develop 模块正文按[日志与 PID 排查](../scripts/docs/启动脚本使用指南.md#日志与-pid)定位对应实例，本地受控源为 `logs/runtime/develop/<instance_id>/*.jsonl`。System 正文查询另需 Platform 上下文和 `platform.module_log.read`，执行详情读取权不授予模块正文访问权；身份核验、可信来源和敏感信息保护沿[日志正文规范](../docs/spec/addp模块服务运行日志规范.md#51-api-与授权)，不从目录存在推定可信身份或日志已经送达。

GeoPython Workflow 的开发启动输出仍使用 `logs/geopython-workflow-engine.log`，按[Runtime 运行说明](../engines/geopython-workflow/docs/geopython-workflow容器管理指南.md#本地开发)核对；它与 Develop 实例正文分别定位。日志、readiness 和执行状态各按其实际范围判断，真实 Runtime 调用、业务产物及故障恢复须独立验收，不能用有日志或进程存活代替。

## 注意事项

### 1. SQL 注入防护

Develop 不再把“数据库连接账号可以做什么”视为用户授权。执行前必须由服务端解析语句并汇总 `read | write | ddl | external_effect`，再校验当前 User 的 Execution Authorization：

- `read` 使用数据库只读事务或等价只读访问能力；仅做字符串前缀判断不构成安全边界。
- `write` 只允许显式具有写效果授权的执行，并保留影响行数和目标摘要。
- `ddl` 单独授权和审计，不包含在普通数据工程写入权限中。
- 不支持可靠效果分类的语句或引擎默认拒绝，不回退到“按连接账号权限执行”。

### 2. 工作流版本管理

工作流当前定义保存在 `develop.dev_tasks.content.workflow_definition`。定义覆盖、执行快照与当前并发缺口统一见[任务表说明](frontend/docs/tables/dev_tasks表.md#25-定义覆盖执行快照与并发缺口)；执行快照只固定本次输入，不构成可浏览、恢复的任务定义修订库。

正式并发 `version` 仍遵循[资源版本规范](../docs/spec/api/资源版本与并发更新.md#资源版本与并发更新)；当前任务定义尚未接入该契约，不能用前端编号、历史 execution 或重绑的局部时间戳 CAS 代替，也不因本文记录现状而豁免规范。

### 3. GeoPython Workflow 内存管理

GeoPython 的矢量数据与部分中间结果在进程内存中处理；资源成本随输入规模、几何复杂度、算子及实际并发变化。原生开发按宿主进程环境评估，产品容器按实际部署限制评估，环境与部署入口见[GeoPython 运行说明](../engines/geopython-workflow/docs/geopython-workflow容器管理指南.md)。

栅格调用的进程共享运行／等待额度、Tenant 额度和 GDAL 块缓存统一见[资源策略](../docs/spec/configuration/System安全与引擎策略.md#栅格引擎资源策略)及[Runtime 准入契约](../docs/spec/workflow/栅格算子契约.md#geopython-栅格资源准入)。GDAL 块缓存不是进程 RSS 上限，资源建议不保证所有算子内存安全；纯矢量工作流不计入栅格额度。Develop 的 Workflow 领取并发另由[执行策略](../docs/spec/configuration/执行与模块策略.md#develop-并发热更新)管理，不把 Backend 槽位当作 Runtime 内存配额。

按实际输入与资源约束选择运行时，控制读取范围和中间结果规模。选择 Spark Workflow 时，仍须在执行时显式绑定已登记的 Spark 通用引擎资源，见[Spark 运行与架构说明](../engines/spark-workflow/README.md)。配置保存、健康探针或单个算子通过不代表容量验收完成；实际运行、产物与清理按对应标准门禁分别认定，不据固定行数承诺安全容量。

### 4. 与其他模块的交互

- **System / 控制面** - 提供引擎与能力事实；执行连接的取得与使用沿[执行授权与效果](#执行授权与效果)，Service 身份不代替原 User 的数据授权。
- **工作流运行时** - 提供 Runtime 算子发现与执行；公开参数、Adapter 派生和 Runtime 参数的分工见[算子发现服务](#算子发现服务)及[正式适配契约](../docs/spec/workflow/Develop资源与执行.md#算子契约分层)。
- **Jupyter 脚本运行时** - 承载 Notebook 交互 Session；会话与 Kernel 访问沿[Notebook 专题](#notebook-会话与排队执行)。排队 `script` 的运行授权仍有缺口，当前不派发 Runtime，见[执行监督与授权](../docs/spec/tasks/模块任务契约.md#develop-执行监督与授权)。

### 5. 执行记录清理

过程事件、执行概览和专业结果分别按[留存契约](../docs/spec/tasks/执行监控.md#概览保留与清理契约)维护：通用过程事件已有 30 天物理清理；180 天是概览按完整父子执行树保留的目标，当前[候选评估](../docs/spec/tasks/执行监控.md#第一期只读候选评估)只读，概览物理删除尚未实施。事件过期、时间候选或测试部署销毁均不授予概览删除许可。

`common.task_executions` 同一行还承载 Develop 工作流专业结果等领域事实，不能把 180 天直接套为专业结果期限或据此删行。Owner 释放条件、领域引用、待消费义务和未知时整树保留以正式契约为准；执行或整树保留也不扩大本人专业结果和 Monitor 的逐执行读取权限。

标准入口、固定版本、失败与修复、清理结果和未覆盖项统一见[Monitor 留存验收索引](../monitor/docs/Monitor模块架构说明.md#执行留存与验收)与[分版本历史证据](../docs/spec/testing/evidence/Monitor执行监控与留存历史证据.md)，不拼接旧版本为当前 HEAD 或生产验收通过。

如果需要清理旧开发任务，需单独确认任务保留策略后再处理 `develop.dev_tasks`。

## 典型开发工作流

### 修改 Develop 后端代码后

先按[Develop 验证入口](../docs/spec/configuration/执行与模块策略.md#develop-验证入口与证据边界)执行匹配改动范围的标准门禁；仅测试或构建不要求重启开发服务。需要应用新版进行运行验证时，由用户在自己的终端、从仓库根目录执行：

```bash
bash scripts/dev/restart.sh -develop
```

Swagger 预检、生命周期锁、停止失败不得继续准备或启动、进程归属与局部范围统一见[启动与重启边界](../scripts/docs/启动脚本使用指南.md#启动与重启边界)及[停止与进程归属](../scripts/docs/启动脚本使用指南.md#停止与进程归属)。`common/` 变更需要通过重启进行运行验收时，由用户在终端执行 `./scripts/dev/restart.sh -all`；不能用单模块重启代表共享变更已在所有进程生效，完整时序沿[生命周期规范](../docs/spec/addp开发服务生命周期与构建身份规范.md)。

重启后按[失败排查](#场景-3调试工作流执行失败)核对对应实例及 Runtime 输出，实际地址取自本次启动结果。`/health/ready` 只证明 Backend 的就绪检查结果，不能代替当前用户权限、执行终态、Runtime 调用或业务结果验收。

### 添加新算子到工作流编辑器

Runtime 和 Adapter 的职责及派生规则见[算子发现服务](#算子发现服务)。先完成相关 owner 的标准验证；需要应用修改时，由用户按[显式 Runtime 启停说明](../scripts/docs/启动脚本使用指南.md#显式启动-runtime)操作，Runtime 与 Develop 分别按变更范围选择，不由算子发现请求自动重启。

```bash
# 1. 在对应 Workflow Runtime 中添加算子实现和 Runtime Operator Spec
# 2. 确认 /api/operators 的 parameters[] 只包含真实运行时参数
# 3. 如果算子需要 locator 或目标资源选择，在 Develop Adapter Spec registry 中注册 Public Operator Spec 和派生规则
# 4. 需要运行验证时，由用户重启对应 Workflow Runtime；修改 Develop adapter registry 时再重启 Develop

# 5. 前端按工作流引擎实例调用算子发现 API 获取新算子
curl -H "Authorization: Bearer <token>" \
  http://localhost:8185/api/v1/develop/workflow-engines/{workflow_engine_id}/operators

# 6. 校验响应同时包含纯 runtime parameters 和 Develop 聚合的 public_parameters
```

## 开发与验证

标准门禁、环境前提与能力边界统一见[Develop 验证入口](../docs/spec/configuration/执行与模块策略.md#develop-验证入口与证据边界)。事件、监督器和 Owner 回归使用 `make test-module MODULE=develop` 与 `make test-develop-postgres`；新增 PostgreSQL 事件回归必须确认[标准入口的名称筛选](../scripts/test/develop-postgres-gate.sh)命中，既有 Go／Develop PostgreSQL CI 承接同一入口。共享诊断另按影响范围运行 `make test-go`、`make test-common-frontend` 和 `make test-monitor-frontend`，Provider 专项分别验证；分项通过不替代真实认证／Gateway／Worker 的 T4。

真实 Query 事件读取复用 `transfer-relational-sql-etl` Hosted suite：三轮父编排中的六个 Develop Query 子执行，分别经已授权 Monitor 入口核对 started／progress／completed、同一 execution／attempt、时间和游标顺序及闭合进度计数；报告只记录核验数量，不复制事件或查询结果。既有 `make test-transfer-online-runner` 与 CI 聚合承接读取契约和负向回归；该扩展实际运行通过前不计为 T4。

### Notebook 验证分工

会话与排队能力沿[Notebook 专题](#notebook-会话与排队执行)分别判断；标准入口仍按上文的模块门禁和各 owner 说明执行，不另拼 Notebook 私有测试命令。

| 范围 | 入口与确定性覆盖 |
| --- | --- |
| Develop Owner 后端 | 上述 Develop 模块门禁；[Session 回归](backend/internal/service/notebook_session_service_test.go)验证主体／能力绑定、关闭取消和有界读取，[原生读取回归](backend/internal/service/notebook_session_native_data_test.go)验证 Provider 选择与只读限制，[重绑回归](backend/internal/service/notebook_runtime_binding_test.go)验证只改当前定义，[Copilot 回归](backend/internal/service/notebook_copilot_service_test.go)验证候选与角色路径。 |
| Develop 前端 | 模块门禁包含 `make test-develop-frontend`；[Notebook 绑定浏览器用例](frontend/e2e/notebook-engine-binding.spec.mjs)使用受控 API 夹具验证引擎／Kernel 展示、重绑及对话框，不运行真实 JupyterLab；[Copilot 静态用例](frontend/tests/notebookCopilotBehavior.test.mjs)核对接线，不能证明 bridge 已在浏览器实际插入单元。 |
| Jupyter Runtime | [Runtime 验证](../engines/jupyter/README.md#验证)维护标准命令、身份／摘要／隔离会话用例与替身边界。 |
| Common Python Facade | [共享库标准验证](../common-python/README.md#开发与验证)运行全量 SDK 回归；[Notebook 用例](../common-python/tests/test_notebook_engines.py)覆盖注入能力、精确引擎注册、原生目录与分页、单方法 deadline 及有界读取；不以 mock HTTP 或受控时钟结果证明真实 Owner 授权、跨模块取消传播或实际延迟。目录错误的调用者处理方式统一见[失败语义](../docs/concepts/addp引擎体系图.md#失败语义)，SDK 当前类型范围见[注册状态](../docs/concepts/addp引擎体系图.md#原生表达与-sdk-注册状态)。 |

上述分项与镜像检查不能替代真实 System → Develop → Runtime 会话、同源代理、文件保存、撤权／到期和公开 HTTPS 的实际验收，也不证明排队运行授权闭环。历史门禁数量、失败、修复和清理只认下节记录的版本与场景；未执行或未覆盖项不计为通过。

### 历史证据

历史记录按主题查阅：

| 历史主题 | 原记录与范围 |
| --- | --- |
| Query 事件接入、本地工具／时区失败与未重跑聚合 | [2026-10-13 本地验证原记录](../docs/spec/testing/evidence/引擎与空间计算历史证据.md#2026-10-13-query-事件接入本地验证) |
| Query／Workflow 监督器、过程事件和 Notebook 排队缺口 | [2026-10-13 阶段记录及 10-15 状态核对](../docs/spec/testing/evidence/引擎与空间计算历史证据.md#2026-10-13-执行监督器第一期工作区验证尚未发布) |
| 图查询工作台旧 v3 验收契约 | [原五 Permission 与浏览器覆盖要求](../docs/spec/testing/evidence/引擎与空间计算历史证据.md#develop-图查询工作台旧-v3-验收契约记录) |
| 保护拒绝诊断、原值历史与角色资格／导出 | [Develop 查询保护历史记录](../docs/spec/testing/evidence/数据保护与执行血缘历史证据.md#develop-查询保护与导出历史记录) |

各记录的源码或原工作区、失败与修复、清理及未覆盖项分别认定；旧契约、排队或分项结果不代表当前 HEAD、完整 T4 或生产验收通过。

## 相关文档

- **GeoPython Workflow 运行时说明** - [engines/geopython-workflow](../engines/geopython-workflow)
- **工作流计算引擎接口规范** - [docs/spec/addp工作流计算引擎接口规范.md](../docs/spec/addp工作流计算引擎接口规范.md)
- **System 模块说明** - [system/CLAUDE.md](../system/CLAUDE.md)
- **共享数据库桥接** - `common/dbbridge`
- [启动脚本指南](../scripts/docs/启动脚本使用指南.md) - 启停边界、进程归属与实例日志排查
- [模块服务运行日志规范](../docs/spec/addp模块服务运行日志规范.md) - 正文来源、独立读取授权与验证范围


### 原值复核跨截止时间的前端修正（2026-11-12）

createQueryPlaintextRefresh原来复用一个计时器：5秒复核开始后等待HTTP，截止时间在等待期间经过时没有独立清空；迟到响应携带已过期plaintext_expires_at也会先显示。两个定向单元在旧实现实际失败，第一例截止时仍无clear，第二例过期raw进入onResult。仅修正现有前端控制器：保留5秒复核节奏，独立截止计时在网络挂起时仍清空；响应自身的原值截止已经过则停止并丢弃，不重试或放宽授权；stop/dispose使两类计时及在途读取失效。脱敏回落仍可显示，不改当次接口、历史、后台暂存、角色或原值授权。

定向7项单元通过，涵盖原清空／替换／退出、截止清除后脱敏恢复、挂起跨截止、迟到过期原值及初始已经到期。标准隔离Develop浏览器中英文2例通过：正式工作台先显示合成原值，5秒复核请求保持挂起，合成8秒期限经过后确认清空，迟到的过期200不恢复；再次执行显示合成保护结果。没有修改系统或浏览器时钟。以上是T1和受控T3，不是真实账户或真实授权自然到期T4。

证据 /tmp/p145-develop-plaintext-deadline-{red,green,browser,frontend}.log；Manager本轮仅验证其既有父／子请求失效，共414项单元通过，无Manager产品变更。完整make test-develop-frontend实际退出0：test:workflow链通过、290项浏览器实际通过（含本轮中英文截止两项）、构建成功；不是Go／PostgreSQL或Hosted门禁。最终同一7项断言隔离导入旧控制器时3过4失败，原控制器与修后实现输入哈希及红例见/tmp/p145-plaintext-deadline-independent/，未回滚共享工作区源码。


### 查询任务切换后的旧提交失效（2026-11-12）

正式 Vue 同组件任务切换重现旧执行仍锁住新任务；隔离最终同一 4 项单元断言回放旧提交函数全部失败，分别为旧预检拒绝、旧结果读取拒绝、旧创建回执和过时高风险确认。修正沿用 executionRequestSequence：任务路由切换清除旧结果、原值复核与加载状态，确认结束、创建回执及外层失败分别验证归属。不取消已提交后台执行、不放开执行中的引擎下拉切换、不修改后端或授权；产品行为见[SQL 任务说明](frontend/docs/SQL任务功能说明.md#3-查看结果与导出)。

定向 4 项单元、中英文三边界共 6 项受控浏览器通过；首次浏览器夹具混算自动语法诊断与手动执行预检，3 过 3 失败，修正为显式等待两次任务诊断后再点击执行，原失败保留。完整 make test-develop-frontend 实际退出 0：test:workflow 链、296 项浏览器及构建通过；make test-frontend-ci-registration 的 27 项及 21 个前端登记通过。以上是 T1／受控 T3，不是真实身份、真实授权或 Hosted T4。证据 /tmp/p145-query-submit-stale-{green,browser-green-v2,frontend,registration}.log；最终旧实现红例 /tmp/p145-query-submit-stale-independent/。


### 全量导出在途观测失效（2026-11-12）

沿现有页面归属契约，清空／替换结果、任务切换时关闭旧导出弹窗和加载状态，卸载时结束导出观测。复用共享 createLatestRequestScope，创建／轮询／下载传递同一轮 signal，成功、失败及 finally 写回前校验 ticket；旧创建回执不继续轮询，旧 finally 不清除新导出的加载状态。共享导出工具在轮询返回及 Blob 保存前复核取消信号，传输忽略取消也不能交付迟到成功。不取消或删除后台会话，不改服务端保护与授权。

最终同一 16 项断言隔离回放旧代码 10 过 6 失败，修后 16 通过；中英文 8 项受控浏览器通过：三阶段任务切换、创建期间退出，以及新任务成功下载恢复。首次浏览器夹具未处理草稿离开确认、退出后仍查已卸载状态节点，原失败分别保留，均不计产品红例或通过。完整 make test-develop-frontend 实际退出 0：test:workflow 链、304 项浏览器及构建通过；共享 make test-common-frontend 378 项通过；Manager 消费方标准门禁 414 项单元、358 项浏览器及构建通过；前端 CI 登记 27 项／21 个前端通过。证据 /tmp/p145-export-observation-{red,green,browser-green-v3,common,develop,manager,registration}.log；隔离旧实现 /tmp/p145-export-observation-independent/。本轮是 T0／T1／受控 T3，没有真实身份 HTTP 或新增 Hosted T4。

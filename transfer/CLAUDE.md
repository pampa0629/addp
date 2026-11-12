# Transfer 模块说明

阅读导航：[当前支持矩阵与任务语义](docs/transfer-任务语义与同步模式.md#三当前支持矩阵)、[后续能力清单](../docs/next/transfer后续能力清单.md)、[执行规则](#执行规则)、[正式契约导航](README.md#正式契约)、[数据库与 Worker 启动](#数据库启动所有权)、[开发与验证](#开发与验证)。

## 模块定位

Transfer 是 ADDP 的数据传输中枢，统一负责 `sync` 任务的配置、planner、policy、字段映射／转换编排、worker、checkpoint、日志、指标和写后 Meta 扫描触发。bounded execution 由独立 `transfer-bounded-worker` 通过 PostgreSQL execution claim 承担；continuous execution 由独立 `transfer-continuous-worker`/supervisor 承担。

导入 / 导出是 Manager、Develop 等调用方的用户动作语义，不是 Transfer 的 `task_type`。可复用任务定义与一次性执行的区分统一见[任务与执行记录](docs/transfer-基本概念及配置说明.md#12-执行记录)；调用身份与来源隔离见[一次性执行与来源隔离](#一次性执行与来源隔离)。

具体读写复用 `common/engine`、`common/format`、`common/contentio` 和 `common/engine/contentadapter`；职责分工统一见[任务语义](docs/transfer-任务语义与同步模式.md#一模块定位)与[内容 I/O 归属](../docs/spec/addp内容IO抽象规范.md#命名和位置)。native table、encoded single/multi/whole 的入口组合见[table 主链路](README.md#已稳定的-table-transfer-主链路)，planner 与扩展约束见[配置、Provider 与 API 维护](#配置provider-与-api-维护)。

查询执行、构建器及保护约束见[查询源与构建器](#查询源与构建器)和[字段血缘与导出保护](#字段血缘与导出保护)。能力概览不代表当前版本运行通过；标准门禁、固定版本证据与未覆盖项见[开发与验证](#开发与验证)。

## 技术栈与端口

- 后端：Go + Gin + GORM，开发首选端口变量为 `TRANSFER_BACKEND_PORT`。
- 前端：Vue 3 + Element Plus，开发首选端口变量为 `TRANSFER_FE_PORT`。
- 数据库：PostgreSQL `transfer` schema。
- 依赖：System、Meta、PostgreSQL、Redis、MinIO/S3。

开发首选值及分配规则统一见[端口规范](../docs/spec/addp端口分配.md#端口分配)；实际监听端口以标准启动输出和 `.dev-state/ports.env` 为准，不从首选值推断。标准启动与就绪检查见[README](README.md#开发启动与就绪检查)。

容器内部端口、根 Compose 的统一 Nginx 入口与模块单独部署的宿主机映射见[部署端口规则](../docs/spec/addp端口分配.md#前端-docker-端口规则)；公开 origin 与 HTTPS／上级反向代理配置见[公开地址配置](../docs/spec/configuration/环境变量参考.md#根目录-env)，不能从开发端口或监听范围推断公开地址。端口监听及就绪响应不证明公开 HTTPS、认证、Worker 源读／目标写链路通过；实际验收范围见[开发与验证](#开发与验证)。

## 重要目录

```text
transfer/
├── authorization/
│   └── permissions.yaml       # Transfer Permission Manifest，发布期聚合事实源
├── backend/
│   ├── cmd/server/main.go     # Backend API、owner scheduler 与 CDC 捕获控制面
│   ├── cmd/worker/main.go     # 独立 bounded worker；PostgreSQL execution claim
│   ├── cmd/continuous-worker/main.go # 独立 continuous worker/supervisor
│   ├── internal/api/          # 任务定义、执行、TaskProvider、capabilities 与只读预检
│   ├── internal/planner/      # source/target endpoint -> table transfer plan
│   ├── internal/executor/     # 基于 common engine/format/contentio 的 table transfer executor
│   ├── internal/service/      # task、execution、system engine resolver、Meta scan 触发
│   ├── internal/worker/       # bounded runner 与 owner scheduler 实现；由各自进程装配
│   ├── internal/continuous/   # continuous supervisor、数据应用与 bounded replay
│   ├── internal/capture/      # CDC capture supervisor、Connect 与源端资源控制
│   ├── internal/repository/   # 私有状态、租约、Capture/DLQ 与启动 schema 持久化辅助
│   └── pkg/vfs/               # 底层虚拟文件辅助能力，非新 transfer reader/writer 主入口
├── docs/
│   ├── design.md             # 架构说明
│   ├── 快速开始.md           # 操作入口
│   ├── 数据库架构.md
│   ├── transfer-基本概念及配置说明.md
│   ├── transfer-任务语义与同步模式.md
│   └── tables/
└── frontend/src/
    ├── router/index.js       # 公开路由、权限落点与页面守卫
    ├── views/                # TaskList、TaskWizard/、TaskDetail、ExecutionDetail
    ├── components/
    ├── api/
    └── utils/moduleNavigation.js # 模块公开导航
```

## 核心 API

Agent `transfer.task.create` 使用精确 `audience=transfer`／同名 Scope 与当前 Tenant 创建权限，只保存禁用、无调度和无自动扫描的 bounded snapshot native table 任务，初始持久字段为 `status=idle, desired_state=stopped`，其中 bounded 任务不消费 `desired_state`。Owner 再次校验，不继承运行授权、不创建 execution 或读写业务数据；该创建委托不得用于其他路由，也不增加角色权限。创建限制、不确定结果处理、Ontology 平台定义及资源 Owner 分工统一见[Transfer Tool 契约](../docs/spec/addp智能体Tool开放规范.md#47-平台-transfer-语义与创建)，不在 Transfer 保存第二份平台定义。

`transfer.task.start` 的独立委托只进入原 start 路由，要求当前 Tenant 的 execute/read 权限交集；首次只保存 `transfer.run_approvals` 中的一次性审批，不创建 execution。用户在 Agent 卡片手动确认，浏览器直接以本人身份调用 Transfer 决定接口；Delegated／Service 不能代批。审批绑定用户、Membership、授权版本、AgentRun 和全部任务执行输入，配置变化拒绝；同一 task lock／事务消费批准并关联唯一执行，拒绝／过期／失败均不自动重试。首期只运行无调度、未启用、无自动扫描的 bounded/snapshot/native table，replace/append，不借用特殊角色保护方式；既有 System／Worker 的实际授权与 Security 校验保持。读取使用精确 `transfer.execution.get` 及原专业执行接口的受限投影，不能从受理推导完成，详见[运行契约](../docs/spec/addp智能体Tool开放规范.md#48-transfer-运行审批与执行读取)。

运行审批的验证复用 `make test-module MODULE=transfer GO_ONLY=1` 与 `make test-transfer-postgres`；后者同次登记真实 PG 并发消费、唯一 pending 执行和发起人关联，不启动 Worker 或访问个人 Business 数据。若本门禁中断留下本轮夹具，可在同一标准入口显式传入 `ADDP_TRANSFER_TEST_RECOVER_EXECUTION=<确切 UUID>`；只恢复 `run_approval_<UUID>` 命名空间、无存活任务、无 attempt／租约／EA／过程事件且零读写的 orphan pending 执行，其他资源一律拒绝，不按时间或模糊前缀批量删除。

Transfer Permission 的精确目录与状态只由本模块 [Manifest](authorization/permissions.yaml) 声明；owner 归属、构建／发布期聚合及向前迁移统一见[权限与角色发布规范](../docs/spec/addp权限与角色发布规范.md#二唯一事实源)和[发布期聚合](../docs/spec/addp权限与角色发布规范.md#九发布期聚合)，不在服务启动时动态注册。

用户任务管理、模块 Runtime 一次性执行与 Orchestrator TaskProvider 使用独立权限，不能互相替代；后两类专用权限均不可委派，TaskProvider 仍由固定 `addp-orchestrator` Service Client Guard 收窄，具体入口与权限见下方分类导航。`transfer.task.cancel` 当前在 Manifest 中为 `disabled`，不代表真实取消能力；execution 取消与任务 pause/stop 的区别见用户专业执行说明。

路由前缀：`/api/v1/transfer`。

- 公共连通：`GET /ping`。
- 资源选择与资源树：引擎列表、对象存储目录及其他资源树统一使用 Meta engine / resource-tree / item API；Transfer 不保留私有引擎列表、数据源树、节点 children、表 metadata 或 object-storage 浏览代理接口。
- 能力发现：`GET /capabilities`，仅返回格式与同步能力矩阵；目录读取与动作权限另行核验，见[资源选择与页面入口](docs/transfer-基本概念及配置说明.md#资源选择与页面入口)。
- TaskProvider 标准任务：独立 `/task-provider/` 端点与专用机器权限见[任务表 API](docs/tables/tasks表.md#taskprovider-机器发现与执行)；`task_type` 固定为 `sync`，不接受用户任务权限。
- 用户任务定义：统一使用 `/task-definitions`，各动作与精确权限见[任务表 API](docs/tables/tasks表.md#用户任务定义与控制)。bounded pause/resume 只控制 owner schedule，不中断 active execution；continuous 的暂停、恢复和停止见[执行与恢复边界](docs/tables/tasks表.md#执行恢复与验收边界)。
- 只读配置预检与角色保护选择：`POST /task-config-checks` 见[只读配置预检](#只读配置预检)；候选与选择检查见[角色专用导出选择](#角色专用导出选择)，两类入口的权限与读取范围分别核验。
- 字段定义推荐：`POST /field-definition-recommendations` 要求当前 Tenant 的 `transfer.task.create`；由用户触发后全量读取所选源 decimal 字段的实际值，并按目标 capability 推荐，不属于仅使用 Meta 快照的配置预检。定义与确认规则见[field_mapping 配置](docs/transfer-基本概念及配置说明.md#五field_mapping-transform)。
- 数据库 CDC 结构变更：`GET /task-definitions/:id/schema-change` 查询当前请求，`POST /task-definitions/:id/schema-change/approve` 人工审批 additive migration；审批、显式恢复与失败边界见[审批与恢复](docs/数据库架构.md#审批与显式恢复)和[补扫 claim](docs/数据库架构.md#审批补扫-claim-与失败边界)。
- 业务 Kafka DLQ 只读管理：`GET /task-definitions/:id/dead-letters`、`GET /task-definitions/:id/dead-letters/:identity`。`transfer.task.read` 下的来源隔离与 payload 安全见[公开查询专题](docs/数据库架构.md#dlq-公开查询与来源隔离)，后台可用性治理、失败语义及验收边界见[availability 专题](docs/数据库架构.md#availability-失败与验收边界)。
- 字段映射随任务定义写入 `config.transforms[type=field_mapping]`，配置见[field_mapping 专题](docs/transfer-基本概念及配置说明.md#五field_mapping-transform)；当前路由不挂载独立 mappings 或 `/transforms` API。
- 用户专业执行：列表、统计、详情、进度、事件与重试统一见[用户专业读取与重试](docs/tables/task_executions表.md#用户专业读取与重试)，读取与重试使用各自权限。当前不提供 execution 级 cancel/stop API，不将任务 pause/stop 当作 bounded execution 的真实取消，TaskProvider 保持 `supports_cancel=false`。
- 模块一次性执行：`POST /executions` 与 `GET /executions/:execution_id/result` 的 Service 身份、独立创建／读取权限、来源隔离及安全 DTO 见[模块一次性执行与来源隔离](docs/tables/task_executions表.md#模块一次性执行与来源隔离)，不套用用户专业读取或 TaskProvider 权限。
- 执行读取范围：`GET /execution-read-scope` 仅供第一方 Tenant User 同步取得 Owner 范围，规则见[通用执行读取与过程事件](../docs/spec/tasks/执行监控.md#通用执行读取与过程事件)，不授予数据访问或子执行的额外读取权。
- Cleanup 专业结果：`GET /cleanup-executions/:execution_id/result` 要求第一方 unrestricted Tenant User、`transfer.cleanup_result.read` 与 `system.cleanup.read`，并核验本人子执行归属；安全投影与失败边界沿[Cleanup 规范](../docs/spec/addp-cleanup体系规范.md)。
- 产出回执：`GET /runtime/executions/:execution_id/production-receipt` 仅供当前 Tenant 的 `addp-system`、`addp-meta` Service 使用 `transfer.production_receipt.read`；证据范围、失败与保护边界见[初始使用权契约](../docs/spec/addp授权上下文规范.md#558-任务产出数据与初始使用权)。回执不是 Grant，也不扩大用户详情或 TaskProvider 输出。
- 平台运行策略：`GET`／`PUT /settings/continuous-policy` 在 Platform Context 下分别要求 `transfer.configuration.read`／`transfer.configuration.update`；字段归属与生效范围见[连续任务策略](../docs/spec/configuration/执行与模块策略.md#transfer-continuous-运行观测配置)，不作为 Tenant task config。

页面创建助手与统一 Monitor 入口见[前端公开路由](#前端公开路由)。路由登记、能力发现和读取范围不代表真实授权或执行通过；标准入口、固定版本证据及未覆盖项统一见[开发与验证](#开发与验证)。

### 只读草稿构建

`POST /api/v1/transfer/task-config-drafts` 将已确认的资源引用、行粒度、目标和字段选择编译为完整候选参数。范围、四项权限与纯元数据读取边界见 [正式契约](docs/transfer-基本概念及配置说明.md#只读草稿构建)。同一入口由 `transfer.task.draft.build` 开放给内置 Agent 与外部智能体，返回参数仍须走正式预检和后续确认。

### 只读配置预检

`POST /api/v1/transfer/task-config-checks` 接收完整创建请求，要求当前租户用户的 `transfer.task.create`、`meta.catalog.read`、`system.engine.read` 三项权限交集；委托使用唯一 `transfer` audience 和精确 `transfer.task.config.check` Scope。共用输入 Schema 与委托校验见 [Tool 契约](../docs/spec/addp智能体Tool开放规范.md#34-当前-tool-集合)。

配置契约直达：[请求、权限与读取范围](docs/transfer-基本概念及配置说明.md#预检请求权限与读取范围) · [报告与三态结果](docs/transfer-基本概念及配置说明.md#报告与三态结果) · [快照覆盖与未检查项](docs/transfer-基本概念及配置说明.md#快照覆盖与未检查项) · [目标能力与写入策略](docs/transfer-基本概念及配置说明.md#目标能力与写入策略) · [摘要绑定与输出保护](docs/transfer-基本概念及配置说明.md#摘要绑定与输出保护) · [Agent Tool 与创建复核](docs/transfer-基本概念及配置说明.md#agent-tool-与创建复核)。

Transfer 先授权，再用 Tenant Service 读取 System 引擎事实及 Meta 已落库快照；不得调用业务行、查询执行／描述、实时 Catalog Facts、Prepare、任务保存或执行。

T1 覆盖授权隔离、零任务副作用、完整配置／结构摘要及缺失／采样／依赖错误；已有 `make test-transfer-postgres` 覆盖非法数组在 replace 删除前拒绝。

标准入口与实际验收边界见[开发与验证](#开发与验证)。

### 角色专用导出选择

正式契约：[默认方式与角色可选方式](../docs/spec/addp数据安全与隐私保护实现规范.md#711-默认方式与角色可选方式)、[执行发起人的当前角色核验](../docs/spec/addp授权上下文规范.md#执行发起人的当前角色核验)、[Transfer export 动作](../docs/spec/addp数据安全与隐私保护实现规范.md#104-transfer-export-动作)。

当前第一方登录用户可在任务配置中一次选择并保存 `config.source.protection_choices`，空选择采用导出默认保护。复用共享 FieldMaskingChoices 与 Common 选项解析，候选与检查分别为 `GET /source-protection-options?source_locator=...` 和 `POST /source-protection-checks`，均按当前账号核验、沿用 transfer.task.execute，不接受用户／角色选择器。来源保护只读取 System Runtime Descriptor，不为判断字段保护获取连接凭据。

首期只开放手动 bounded/snapshot/native table 及其 PreparedQuery 来源；保存及名称更新重新校验，切换来源／模式不丢弃旧选择，定时、委托、编排、continuous、增量与 encoded/record 来源不消费特殊方式。启动和手动重试在同步用户请求内签发 audience=transfer 的本次执行授权，要求现有 system.execution_authorization.create、transfer.task.execute 和实际源读／目标写效果资格；不增加角色或权限。缺执行权限返回权限拒绝，角色／选项失效返回保护选择冲突，两者不混淆。

Worker 按实际 EA／execution／attempt／lease 逐批读取 System 当前角色，以该用户 EA 获取源和目标引擎访问，在字段映射与 Writer 前应用 export 保护。选项、资格或来源失效明确拒绝，不回落默认。Develop 全量导出使用独立 export 选择与共享会话精确摘要核验来源，再绑定签发的真实用户执行授权；默认导出仅保留已有观察来源，不自动提升为执行权限。原值授权、应用凭据和角色变更不在本轮范围。T1、真实 PostgreSQL 和前端门禁见[Security 2026-10-23 接入验证](../security/CLAUDE.md#transfer-角色选择与-develop-全量导出接入2026-10-23)；真实账号全链路 T4 需单独验收，不能由上述门禁推定。

分版本验证记录见[角色资格到期与导出拒绝证据](#角色资格到期与导出拒绝证据)。

## 执行规则

执行导航：[一次性执行与来源隔离](#一次性执行与来源隔离)、[通用执行约束](#通用执行约束)、[查询源与构建器](#查询源与构建器)、[字段血缘与导出保护](#字段血缘与导出保护)、[bounded 失败诊断](#bounded-失败诊断)、[生产来源与写后扫描](#生产来源与写后扫描)、[Cleanup 执行边界](#cleanup-执行边界)。

公共验收导读：[开发与验证](#开发与验证)维护标准门禁、固定源码版本、失败与修复结论及未覆盖项；以下各节保留对应能力、权限与失败场景的独有验收边界，不以契约、入队或局部检查推定真实链路通过。

### 一次性执行与来源隔离

- 一次性执行入口只允许已认证且具备不可委派 `transfer.execution.create` Permission 的模块 Service Client 调用，execution 的 `source` 必须由 `addp-<module>` Client ID 推导，调用方请求不得自报 `source_module`。同一模块使用不可委派 `transfer.execution.read` 回查自己创建的 execution，Transfer 必须再次按 Client ID 校验 `source`，不能借此读取其他模块或用户任务 execution。入口接受强类型 source / target / fields 契约，并在 Transfer 内转换为唯一 planner 配置；查询调用方必须把已解析的全部关系输入按稳定顺序写入 `source.query.inputs[]`。Transfer 不自行解析 SQL/MQL 反推资源，但必须使用同一 `PreparedQuery` 的 Provider `ReadSet` 验证调用方声明；未声明 `inputs` 的 Console 单源查询必须精确只读取所选 source leaf。通过验证的输入才能写入血缘。调用方不得创建临时 Transfer task，也不得保存 Transfer task ID。bounded worker 必须同时执行有任务定义和无任务定义的 `sync` execution，并只在 `source_task_id` 存在时更新任务摘要。

- 服务代发导出必须按已认证 Client ID 推导 Manager／Develop owner，再从 System 活动模块注册信息解析 Backend 地址，使用 Transfer tenant Service Token 核验导出会话、预绑定 execution UUID、完整请求摘要和待执行状态。核验得到的用户只写入 `triggered_by`，不能写入执行授权 actor 字段。请求不接受自报用户或回调地址；无来源的机器执行不声明人类发起用户，不能为旧记录猜测归属。

### 通用执行约束

主题导航：[配置、Provider 与 API 维护](#配置provider-与-api-维护)、[bounded 读写与恢复](#bounded-读写与恢复)、[成果事实与初始授权](#成果事实与初始授权)、[单次目标覆盖与稳定输出](#单次目标覆盖与稳定输出)、[continuous 运行、提交与观测](#continuous-运行提交与观测)、[业务 Kafka DLQ 与 replay](#业务-kafka-dlq-与-replay)、[CDC 捕获与审批](#cdc-捕获与审批)、[任务删除与物理清理](#任务删除与物理清理)、[写后扫描触发与范围](#写后扫描触发与范围)。

#### 配置、Provider 与 API 维护

- 配置字段与 endpoint 结构沿[任务 Config JSON](docs/transfer-基本概念及配置说明.md#二任务-config-json)；格式来源、目标声明与省略规则分别沿[format 声明与来源](docs/transfer-基本概念及配置说明.md#format-声明与来源)、[raw copy 目标继承与一致性](docs/transfer-基本概念及配置说明.md#raw-copy-目标继承与一致性)，不在模块规则中重复维护。旧字段出现即拒绝，不做兼容转换。任务类型固定为 `sync`，不得新增或兼容 `import`、`export`、`transfer` 等旧任务类型，定义见[传输任务](docs/transfer-基本概念及配置说明.md#11-传输任务)。
- table transfer 统一走 `internal/planner` + `internal/executor`，按 data type / representation / layout 分叉，不按具体引擎组合分叉。encoded 读写的 engine／format 接入与 multi refs 组合统一见[Provider 消费规则](../docs/spec/addp引擎插件接口规范.md#五上层消费规则)和[内容 I/O 调用链](../docs/spec/addp内容IO抽象规范.md#调用链)，不在 Transfer 中新增私有 reader / writer。新增 table 能力应优先补 `common/engine` 或 `common/format`；旧 `pkg/pipeline`、`pkg/plugin_loader` 不作为新功能入口。
- FileGDB/PGeo 等 container table 源只接受通用 `source.options.child_name`；planner 必须从 Meta `type_info.container.children` 校验 child 并解析 `native.table`，任务配置不得直接保存 `layer` 或 `child_table` 等 provider 私有入口。布局与运行时入口见[读取布局与入口](docs/transfer-基本概念及配置说明.md#读取布局与入口)。
- API 契约同步沿[API 实施要求](../docs/spec/api/文档与实施.md#新接口开发)与[Swagger 强制同步要求](../docs/spec/addp-Swagger集成指南.md#强制同步要求)；Transfer 从仓库根目录执行 `bash scripts/swagger/gen-swagger.sh transfer` 和 `bash scripts/swagger/check-route-coverage.sh transfer`。生成或覆盖检查失败均须阻断；生成成功不等于覆盖完整，覆盖通过不代替权限与真实读写验收。

#### bounded 读写与恢复

- `replace` / `append` 是 Transfer policy，删除由 `common/engine` 的 `ResourceDeleteProvider` 提供；策略及能力要求见[写入策略](docs/transfer-基本概念及配置说明.md#六写入策略)与[目标能力](docs/transfer-基本概念及配置说明.md#目标能力与写入策略)。
- native target 在删除／准备前消费可选 `TableWriteDefinitionChecker`，拒绝已知不支持的声明；Provider 覆盖、空间与原生 array 限制统一见[StoreProvider 契约](../docs/spec/addp引擎插件接口规范.md#storeprovider)。无接口或实际值未知不能当作通过，逐值编码、授权／Security 和实时结构检查仍须执行，见[运行期定义检查](docs/transfer-基本概念及配置说明.md#运行期定义检查)。PostgreSQL 数组源在 replace 删除前拒绝及原目标 OID／哨兵保留由 `make test-transfer-postgres` 覆盖，不证明 replace 全流程原子回滚。
- snapshot checkpoint 仅作观测，失败 `replace` retry 创建新 execution 从头执行，`append` 拒绝 retry；watermark 从 committed position 续读，不能称作 snapshot checkpoint resumable。恢复分级与独立验收沿[恢复契约](docs/transfer-基本概念及配置说明.md#恢复分级)，租约过期是否可安全恢复沿[公共领取与恢复契约](../docs/spec/tasks/执行记录与租约.md#bounded-execution-领取租约和恢复契约)。
- 大数据传输要优先考虑批大小、连续读取 / 写入 session、进度日志和 restartable retry。
- bounded Worker 不使用消息载荷；以 `common.task_executions` 中冻结的执行配置为唯一输入，并通过 `lease_token` 条件写入。
- watermark 只消费 `bounded_watermark_read`，在源一致性快照内冻结上界、按完整游标稳定排序；目标按幂等 `TableUpsertProvider` 能力选择，成功提交后才 CAS/fencing 推进主状态。单／复合游标、键与引擎／非空间限制及未覆盖项统一见[watermark 配置](docs/transfer-基本概念及配置说明.md#七watermark-bounded-incremental)和[执行语义](docs/transfer-任务语义与同步模式.md#42-watermark-incremental)，Transfer 不按源或目标引擎类型分叉。

#### 成果事实与初始授权

- PG bounded 在同 attempt／实际目标的成功提交中冻结同次读取会话的完整 ReadSet 与 Provider `created_and_committed`；来源未知为 null，新建未证明为 false，不从配置、行数或血缘补造。新建与提交证明、schema 运维前提及外部 DDL 原子性限制沿[Provider 契约](../docs/spec/addp引擎插件接口规范.md#storeprovider)。私有 `target_production` v3 使用封闭 JSON 字符串和唯一 owner codec，避免 JSONMap 浮点往返改变 Engine ID；不兼容或补填旧事实，不持久化原生 CreationWitness。
- Manager 私有 Shapefile 导入在同次读取中核对上传组成的 SHA-256，随实际成功提交冻结私有来源证明，供 System 按同一初始授权入口裁决。源文件仍由 Manager 私有持有。
- 成果资格、System／Meta 窄机器回执权限、签发事务与期限裁决、续办及限定 basic 补扫统一见[初始使用权契约](../docs/spec/addp授权上下文规范.md#558-任务产出数据与初始使用权)。Transfer 只在本次成功完成事务为具备 v3 首次新建提交证明及完整人类来源的首次 bounded 执行初始化 `initial_authorization`；不回填历史。Worker 注册有效时独立领取、锁外核清／续办，仅正式 pending 才补扫；有界退避、旧领取围栏和终态规则沿该契约，不重跑写入、不删除成果或延长 System 冻结期限。
- 通用 metadata／完成路径禁止自报保留的授权状态，HTTP 只投影 `status/code`，不泄露领取进度、Grant 引用或私有证据；普通详情／TaskProvider 不暴露产出证明。页面区分物理成功与授权状态，待办只读刷新；实际读取仍按 System Grant 与 Security 链裁决。完整链路与固定版本证据见[新成果初始授权验收](../docs/spec/testing/scenarios/授权与共享.md#新成果初始授权)，T1/T2 与前端门禁不代替真实产出→识别→签发→读取→撤销验收。

#### 单次目标覆盖与稳定输出

- bounded snapshot 的目标覆盖与 source 是直接表、SQL 或 MQL 正交；已存在默认目标、append 限制及仅覆盖本 execution 的配置沿[单次执行目标覆盖](docs/transfer-基本概念及配置说明.md#24-单次执行目标覆盖)。Worker 按通用 Engine capability 严格核验实际目标的字段名称、顺序与类型，只用 `TableWriteSessionProvider` append，不调用 delete、truncate、prepare 或 DDL；覆盖执行的 lease 过期不自动重试。Transfer 不保存 LogicalTable ID、不调用 Model API、不持有 Model Permission。
- bounded `sync` 的稳定输出 `execution_id + target_locator + row_count`、闭合输入与默认值沿[模块任务契约](../docs/spec/tasks/模块任务契约.md#跨模块任务边界)。`target_locator` 必须是本 execution 的实际目标；业务与 Infra 父 Locator 保持各自体系及解析器，下游不从私有配置、任务 ID 或物理字段拼接目标。
- 稳定输出唯一持久事实及顶层 `outputs` 投影沿[TaskProvider 标准响应](../docs/spec/tasks/TaskProvider接口.md#标准响应体)，统一复用 `common/taskprovider`；用户执行详情仍使用 Transfer DTO，不包装机器契约。身份、精确 Permission 与入口验收边界见[任务表 API](docs/tables/tasks表.md#五api)，契约声明或入队不等于真实目标写入与编排链路通过。

#### continuous 运行、提交与观测

- 业务 Kafka 与数据库 CDC 复用唯一 continuous worker 主循环，不新增 CDC consumer；来源／目标能力及捕获拓扑沿[支持矩阵](docs/transfer-任务语义与同步模式.md#三当前支持矩阵)、[业务 Kafka](docs/transfer-任务语义与同步模式.md#62-业务-kafka)和[数据库 CDC](docs/transfer-任务语义与同步模式.md#63-数据库-cdc)。source 消费 `ChangeStreamReaderProvider`，ChangeRecord 到 ChangeEvent 的解码及 ChangeApplyWriter 归 Transfer runtime。
- 业务 Kafka 注册为 System Engine，topic 使用 `type=topic` ResourceLocator；partition 只属于 runtime assignment/position/diagnostics。Infra Kafka 来自 ADDP 部署配置，不进入 System engines、资源树或用户任务 JSON。
- 目标必须消费声明原子、单调及所需 operations 的 `PartitionedTableChangeApplyProvider`，先在目标库原子提交业务行与 partition ledger，再以 runtime fencing token + state version 做 Infra CAS；普通 `TableUpsertProvider` 不足以阻止失效 worker 写回。各目标 ledger 归属与类型限制沿[CDC 契约](../docs/spec/tasks/Transfer数据库CDC.md#transfer-数据库-cdc-v1-契约postgresqlmysqloracle)，Oracle CDC partitioned apply 不与 bounded table write session 共享 ledger 语义。崩溃重放由目标 ledger 吸收，不宣称跨系统 exactly-once，见[交付保证](docs/transfer-任务语义与同步模式.md#61-交付保证)。
- continuous session 不进入 bounded claim 或 Asynq；单 task 的 owner lease／heartbeat／fencing 与 partition position 分离，见[运行时契约](docs/transfer-任务语义与同步模式.md#六continuous-运行时)。用户控制、新 execution、自动恢复及退避／circuit 审计沿[生命周期](docs/transfer-任务语义与同步模式.md#64-生命周期)。PostgreSQL/MySQL/Oracle 锁等待须响应 context 取消并回滚未完成业务写入及 ledger；Oracle `FOR UPDATE NOWAIT` 有界重试见[位置恢复与取消](docs/transfer-基本概念及配置说明.md#位置恢复与取消)。
- Transfer 沿同一 `ChangeStreamReader` 采集并判定分区诊断，计算与健康口径见[诊断与恢复窗口](docs/transfer-任务语义与同步模式.md#65-诊断与恢复窗口)。Monitor 只消费公共 execution 安全投影，不直连业务 Kafka 或 Transfer 私表；权限、来源隔离与字段裁剪沿[专业执行规范](../docs/spec/tasks/执行监控.md#transfer-专业执行读取与响应)。能力、状态或诊断展示不证明实际提交、恢复、停止或权限链路通过。

#### 业务 Kafka DLQ 与 replay

- DLQ 的确定性记录错误分类和 payload → 控制索引 → 目标 `skip` ledger → Infra CAS 顺序沿[错误准入](../docs/spec/tasks/Transfer-Kafka与DLQ.md#dlq-错误准入)与[提交顺序](../docs/spec/tasks/Transfer-Kafka与DLQ.md#dlq-提交顺序与崩溃恢复)，任一步失败不得推进后续 position。查询的 Tenant/task 权限与 payload 安全沿[公开查询与来源隔离](docs/数据库架构.md#dlq-公开查询与来源隔离)；索引读取权不授予原始 payload 访问权。
- availability 由 continuous worker 唯一低频 reconciler 管理，精确引用 CAS、瞬时故障保留状态及 payload 不留副本沿[availability 专题](docs/数据库架构.md#availability-失败与验收边界)；不进入 HTTP 链路或提交 Kafka offset。`payload_available=true` 不等于 replay 已验收，具体证据范围见该专题。
- replay 的唯一 API、显式 offset ranges、retention 复核与新 PostgreSQL 隔离目标沿[配置专题](docs/transfer-基本概念及配置说明.md#dlq-与-bounded-replay)。它从原业务 topic 读取，使用独立 bounded execution／apply identity，不动主状态、lease、水位或目标；失败不改变主任务或触发 continuous 恢复 circuit，见[独立执行契约](docs/transfer-任务语义与同步模式.md#五状态checkpoint-与-replay)及[Replay 契约](../docs/spec/tasks/Transfer-Kafka与DLQ.md#bounded-replay-执行与目标隔离)。查询、availability 与 replay 的验收分别认定。

#### CDC 捕获与审批

- `transfer.capture_resources` 保持 engine-neutral generation 主事实，provider 私有资源归对应一对一子表，不在 generic 主表恢复 provider 字段或使用伪资源占位；字段、ownership 与级联关系见[Capture 资源登记](docs/数据库架构.md#六cdc-capture-generation-与资源登记)，生成与清理身份沿[捕获资源所有权与登记](../docs/spec/tasks/Transfer数据库CDC.md#捕获资源所有权与登记)。任务只使用唯一 `DatabaseCDCTaskSpec`，provider 必须由 System Engine 解析，不能用配置形态推断；PostgreSQL/MySQL/Oracle 共用唯一 capture supervisor、continuous worker、API、capability 和 Console 路线。
- 数据库 CDC Pause 不停止 connector，风险观测沿[暂停与保留窗口](../docs/spec/tasks/Transfer数据库CDC.md#暂停与保留窗口)；Stop 不可逆，服务端确认、源身份核验与清理顺序沿[停止确认与清理顺序](../docs/spec/tasks/Transfer数据库CDC.md#停止确认与清理顺序)，永久终态和失败重试见[恢复资格与终态判定](../docs/spec/tasks/Transfer数据库CDC.md#恢复资格与终态判定)。业务数据与执行／审计保留、真实停止及清理证据统一见[保留对象与清理验收](../docs/spec/tasks/Transfer数据库CDC.md#保留对象与清理验收)，不以状态或请求成功替代验收。
- 数据库 CDC 固定使用 Debezium Connect 3.6.0.Final schemaless JSON，操作归一化、源类型与编码统一见[CDC 正式契约](../docs/spec/tasks/Transfer数据库CDC.md#transfer-数据库-cdc-v1-契约postgresqlmysqloracle)。Oracle Spatial 按冻结 XY/XYZ 维度使用 generation-owned WKB BLOB／GeoJSON CLOB，再由同一 adapter 输出 EWKB，内部载体不开放普通业务 LOB；详情见[Oracle CDC 配置](docs/transfer-基本概念及配置说明.md#oracle-cdc)。协议错误不推进 offset，schema drift 以 `schema_change_blocked` 阻塞，审批条件与显式恢复见[Schema drift 与审批恢复](docs/transfer-基本概念及配置说明.md#schema-drift-与审批恢复)。未支持项及增强规划见[当前限制](docs/transfer-基本概念及配置说明.md#cdc-未支持项与运行事实)与[后续能力清单](../docs/next/transfer后续能力清单.md#三数据库-cdc)；源捕获维度不代表目标写入维度或真实验收通过，已有范围与缺口见[Oracle 验收边界](docs/transfer-基本概念及配置说明.md#oracle-既有验证与验收边界)和[专项入口说明](README.md#oracle-cdc-专项入口缺口)。
- 审批补扫使用 request 级持久化 claim，接管、迟到结果拒绝及 `TRANSFER_META_SCAN_CLAIM_TTL` 与 HTTP 超时约束统一见[CDC 契约](../docs/spec/tasks/Transfer数据库CDC.md#审批补扫-claim-接管与失败)和[数据库 claim 边界](docs/数据库架构.md#审批补扫-claim-与失败边界)。GET 始终只读；真实 Meta API 提交失败为终态 `failed`，不自动重试，也不回滚已应用变更。审批权限与显式 Resume 沿[审批与恢复](docs/数据库架构.md#审批与显式恢复)，提交／完成的区别及安全 DTO 沿[公共观测与验收](docs/数据库架构.md#schema-change-公共观测与验收)。

#### 任务删除与物理清理

- 用户显式删除按当前 Tenant 与 `transfer.task.delete` 独立授权，见[任务定义入口](docs/tables/tasks表.md#用户任务定义与控制)；Engine 生命周期 Cleanup 保留命中的任务定义，供后续重绑定或显式删除，不能按 physical mode 一概删除。Transfer-owned 捕获／DLQ 资源与 Infra 共享资源的所有权、用户目标及审计保留统一见[Cleanup 的 Transfer 职责](../docs/spec/addp-cleanup体系规范.md#transfer)。
- 删除运行控制状态前须收敛 continuous task 为 desired stopped，并取得所有原 invocation 的实际停止证明；lease 过期、cancel 或进程消失均不构成证明。pending／running bounded 执行阻断清理。外部效果先登记精确目标并复核原用户授权，未知不重发；Runner、持久围栏及验收边界见[Cleanup 执行边界](#cleanup-执行边界)和[正式 Owner 约束](../docs/spec/addp-cleanup体系规范.md#十五已确认决策)。
- 显式任务删除的 capture → DLQ → 私有状态／定义收口、失败保留及最终事务沿[任务资源契约](../docs/spec/tasks/Transfer-Kafka与DLQ.md#任务资源清理范围与失败保留)、[停止与最终事务](../docs/spec/tasks/Transfer-Kafka与DLQ.md#清理前停止与最终事务)和[私有状态与任务删除](docs/transfer-任务语义与同步模式.md#51-私有状态与任务删除)。外部删除失败不得先删索引或任务，公共 execution、目标业务数据及目标 ledger 保留。门禁、真实停止、外部资源与零残留须分别验收，不以 lease 或 API 返回证明清理通过。

#### 写后扫描触发与范围

- `auto_scan_metadata=true` 的触发时机及原生表 `targets`／encoded 内容 `ref_groups` 的精确范围统一见[写后扫描配置](docs/transfer-基本概念及配置说明.md#十一写后-meta-扫描)。CDC 必须在匹配当前 generation／connector 的 `COMPLETED` offset 完成目标 ledger 和 Transfer position 提交后才触发首次扫描，空表同样遵守该顺序；缺少精确目标不得扩大到父范围或全引擎。
- continuous 首次扫描的 task 级防重 claim、后续 session 接管和 TTL 约束沿[同步执行契约](../docs/spec/tasks/Transfer同步执行.md#transfer-continuous-sync-v1-契约)。Meta API 提交失败不阻断数据面；仓储或 fencing 错误仍返回运行时处理，具体反馈见[扫描持久化与失败边界](docs/数据库架构.md#九写后-meta-扫描)，不能套用提交失败的非阻断语义。
- 私有提交状态／Meta execution UUID 不代表扫描完成，也不授予 Meta 读取或执行权限，结果核对沿[首次扫描的提交与结果核对](docs/tables/tasks表.md#首次扫描的提交与结果核对)。Meta HTTP 精确范围的 T1 回归继续沿既有 Transfer 后端与 CI；真实扫描、目标事实和清理的分版本范围见[扫描验收证据](#continuous-精确扫描与审批归属)，不以提交成功或数据面继续运行代替验收。

### 查询源与构建器

只读原生查询结果到 table 的 bounded 搬运属于同一主链路：source 必须消费 `common/engine.QueryReadSessionProvider`，target 继续消费 table write session。MongoDB 嵌套 BSON 的路径投影、数组展开、去重和关系合并由只读 MQL 在源端完成；Transfer 只搬运最终扁平行、执行显式字段映射和严格类型转换，不提供递归 JSON 自动摊平器。

Console 基础构建器只编写同一 `source.query`，不保存第二份 DSL 或建立另一条执行路线；目标字段名和类型归后续 `field_mapping`，复杂数据开发归 Develop。

- MongoDB：基于 Meta 已识别字段，按文档或单数组行粒度编译 `可选单次 $unwind -> $project`；文档标识自动携带。查询输出名由全部源路径按稳定顺序统一编译，不得因选择顺序改变；基础界面不开放查询参数。
- 关系型 SQL：只为所选单表生成字段投影与一层 `all|any` 行过滤，过滤值全部编译为 `source.query.parameters` 中的类型化参数。语言、标识符引号和参数类型来自实例 `compute.query`；`bigint/decimal` 使用十进制字符串通过 `string` 无损传递，Planner 在调用 Provider 前再次校验参数开关、语言和值类型。

行粒度、字段候选、能力驱动入口及不支持的编辑／计算范围统一见[query source 与轻量 ETL 配置](docs/transfer-基本概念及配置说明.md#23-bounded-query-source-与轻量-etl-构造)。两者仅对严格可逆且符合当前 Meta 事实的子集开放结构化编辑，其余已有合法只读查询只读展示，不提供高级编辑或保存入口。

只读会话与来源证明沿[Provider 契约](../docs/spec/addp引擎插件接口规范.md#queryruntimeprovider)，资源选择、创建与执行的独立权限沿[任务授权](docs/transfer-基本概念及配置说明.md#13-租户阶段的任务授权)。能力声明或表单回显不证明真实读写、保护或完整链路已通过。

### 字段血缘与导出保护

bounded 字段血缘复用同次 Provider 证据与实际 field mapping；多来源查询的完整 `query.inputs` 必须匹配同一 `PreparedQuery` 的 `ReadSet`，Transfer 不解析 SQL/MQL。仅新成功执行产生字段事实，不按当前配置补造历史。多来源路径绑定、原始结构快照、字段值来源与行筛选的区别及 `derived/unavailable` 规则统一见[字段级契约](../docs/spec/addp数据血缘能力规范.md#61-字段级首期契约)。

`export` 是受保护数据离开源 DataItem 的保护动作，任务类型仍为 `sync`。命中纳管来源的 `bounded + snapshot` Native TablePipeline 按精确 Locator 与当前源结构，在字段映射、类型转换、空间处理和 Writer 前执行保护，不按 `engine_type` 分叉；查询保护消费同一 `PreparedQuery` 的完整 ReadSet/OutputLineage。开放形态、查询证明子集与未接入形态的资源级失效关闭边界统一见[Transfer export 契约](../docs/spec/addp数据安全与隐私保护实现规范.md#104-transfer-export-动作)。MongoDB 原始记录须在保留 BSON 标量的对象上先保护、再编码 Canonical Extended JSON，不解析已编码的 EJSON；端点与类型保真格式见[动态记录原始导出](docs/transfer-基本概念及配置说明.md#32-动态记录原始导出)。

专业读取权限与安全 DTO 沿[专业执行契约](../docs/spec/tasks/执行监控.md#transfer-专业执行读取与响应)；Provider 证明不等同真实写入、血缘采集或完整权限链路验收通过。

### bounded 失败诊断

Transfer bounded 执行失败的诊断沿既有 `error_details.code` 输出：不可证明查询输出血缘的类型化 `plugin.ErrQueryOutputLineageUnresolved`（含逐层 `%w` 包装）在唯一完成事务中保存 `query_output_lineage_unresolved`。执行完成内部接口传递 error，不先转字符串再猜测原因；私有 message 仍仅用于内部诊断，专业响应继续通过 Common Observation 隐藏原始文本。Hosted 派生查询拒绝验收只匹配该稳定 code、failed 终态、本次执行身份、零写入与空目标前后事实，不消费已删除的 error_msg，也不把连接错误或同文案字符串当作保护拒绝。T1 覆盖类型化／包装／无关错误、当前租约下完成与专业响应隐藏文本；现有 Transfer PostgreSQL 读取隔离门禁复验诊断字段及权限，已有门禁登记承接，不新增数据库或测试入口。

Transfer bounded 保护失败在实际边界保留类型化准备／结果标记，唯一完成事务据此写入 `addp.execution-failure/v3` 的 `data_protection_required` 原因；血缘不可证明仍保留原稳定 `query_output_lineage_unresolved` 码，其他保护拒绝使用 `data_protection_required`。专业详情复用共享 ExecutionFailure，与 Monitor 消费同一安全诊断，不显示原始错误正文；Provider 类型化查询错误与 SQLSTATE 优先保留原查询失败性质，连接／普通读取错误不能仅凭相同文案误判为保护拒绝。现有 T1、Transfer 前端与 PostgreSQL 门禁、Security Hosted suite 自动发现承接，不新增入口或依赖。

### 生产来源与写后扫描

人工、编排与自动恢复的主体来源统一沿[生产来源契约](../docs/spec/tasks/生产来源与产物扫描.md#生产执行与后台扫描的来源)，自动恢复不复制授权或 lease。bounded 先按当前 lease 保存实际 outputs 与 writer refs，再提交 `source=transfer` 和父 execution UUID；continuous 初次扫描先在私有 lease fencing 事务内保存 prepared target。Meta 拒绝无来源 Service 扫描，Transfer 保存安全扫描反馈；来源事实不授予数据访问、执行或子扫描读取权限。

自有 bounded Cron 的主体、定义摘要与触发重核沿[Transfer 定时任务授权](../docs/spec/tasks/任务定义与授权.md#transfer-定时任务授权)。暂停清除调度授权，恢复由当前 User 重授权；资格缺失或失效时不创建 execution、不推进计划。

审批补扫的来源字段、审批主体及父 execution 互斥统一见[结构审批补扫来源](../docs/spec/tasks/生产来源与产物扫描.md#transfer-结构审批补扫来源)。Transfer 在审批应用与 claim 状态变化事务中同步私有 `schema_change_scan_origin`，不进入专业诊断；原阻塞 execution 的主体不变，新扫描按本次审批 User 隔离读取。request 级 claim 接管与失败语义沿[CDC 捕获与审批](#cdc-捕获与审批)，提交／完成及安全 DTO 沿[公共观测与验收](docs/数据库架构.md#schema-change-公共观测与验收)，不在来源导读重复维护恢复规则。

Manager 导入的创建/启动只在同步请求内转发 User 凭据。Manager 导入的 Infra source 由当前 bounded execution 的完整 User 来源和绑定任务经 Manager Owner 核验后读取，使用 `manager.import_provenance.read`；未知、未绑定、跨租户、非上传者或 Owner 不可用时拒绝。通用 content／format reader 保持唯一实现，业务来源仍走既有 Security Gate，不以 Infra 路径作为许可。

精确范围、双用户审批归属与清理的实际证据见[扫描与审批验收](#continuous-精确扫描与审批归属)；T1/T2、单用户与双用户 Hosted 的覆盖范围分别认定。

### Cleanup 执行边界

Cleanup 使用 Common 持久 Runner，System 预建子执行，Transfer 不消费旧 Redis Cleanup 请求。外部效果先持久登记准确目标并复核原用户授权，未知效果不重发；原运行体迟到停止证明只更新私有控制事实。专业结果、公共回执及最终任务变更同事务提交。`runtime_sessions`／`cleanup_operations` 不随普通任务私有状态删除而抹除，不自动开放曾发生外部清理的任务。首期 closed Tenant 与无可信 User 来源的清理继续阻断。标准验证为 `make test-module MODULE=transfer GO_ONLY=1`、`make test-transfer-postgres`，后者按包串行执行并包含 Cleanup Owner 原子性与 runtime fencing 场景；不据此声称真实 Kafka/CDC 或 Hosted 清理通过。

## 前端公开路由

- 创建助手的发现与确认：“传输任务”列表页通过 Copilot `/api/v1/copilot/transfer/generate` 提取源资源意图，再由向导调用 Manager 搜索／资源事实与 Meta 祖先链取得真实候选；确认时重读 Owner 事实，唯一候选也须用户确认。目标沿用共享资源树，之后依次确认目标引擎、目标父位置、目标表、字段映射和任务配置。
- 生成与保存：配置确认后向同一 Copilot 接口提交资源上下文与完整草稿作受限生成；Copilot 不调用资源 Owner、不创建或启动任务，分工与生成约束统一见[Transfer 草稿契约](../docs/spec/addp智能体Tool开放规范.md#47-平台-transfer-语义与创建)。最终由 Transfer 按当前创建权限与唯一 `task-definitions` API 独立校验保存，不以生成成功证明资源、授权或执行成功。
- 字段推荐：声明 `limits.table_write.decimal` 的新目标表复用 Transfer 字段定义推荐，按目标 capability 校验并展示供确认，不按 `engine_type` 建立名单。字段与精度规则见[field_mapping 配置](docs/transfer-基本概念及配置说明.md#五field_mapping-transform)；真实源值读取与 `transfer.task.create` 权限边界见[核心 API](#核心-api)，能力展示不代替[实际验收](#开发与验证)。

- 全局监控：模块全部执行列表、通用统计和通用详情统一归[Monitor 前端规范](../docs/spec/tasks/执行监控.md#执行监控前端所有权)；Transfer 不另建全局列表或面板。任务列表、任务详情及执行详情均复用 `MonitorExecutionsButton(module=transfer, task_type=sync)`，按实际任务身份附带筛选。`monitor.execution.read` 只是入口条件，仍须[逐 Owner 读取裁决](../docs/spec/tasks/执行监控.md#租户执行诊断)。
- Transfer 专业页：`TaskDetail` 保留单任务执行历史、领域诊断、结果及重试，`ExecutionDetail` 展示单次领域详情。读取与重试的独立权限、恢复条件见[执行记录 API](docs/tables/task_executions表.md#用户专业读取与重试)，安全 DTO 沿[专业响应契约](../docs/spec/tasks/执行监控.md#transfer-专业执行读取与响应)；页面可读或跳转成功不代替[真实链路验收](#开发与验证)。

- 模块内公开导航统一通过 [moduleNavigation.js](frontend/src/utils/moduleNavigation.js) 复用共享能力；Console URL 与模块渲染的分工沿[公开路由事实源](../docs/spec/addp前端路由与可恢复状态规范.md#二公开路由事实源)，历史栈规则沿[浏览器历史语义](../docs/spec/addp前端路由与可恢复状态规范.md#六浏览器历史语义)，不在模块重复维护导航实现。
- 权限落点：独立模式有 `transfer.task.read` 时以任务列表为唯一侧栏入口，创建由列表按钮进入；无读取但同时具备 `transfer.task.create` 与 `meta.catalog.read` 时显示创建向导入口，不跳转无权读取的列表。Console iframe 模式由 Console 统一显示侧栏；准入与动作权限见[任务授权](docs/transfer-基本概念及配置说明.md#13-租户阶段的任务授权)。
- 对象身份使用 `/tasks/:id/edit`、`/tasks/:id/detail` 与 `/executions/:execution_id`，不得退化为列表 URL 或 iframe 私有状态；通用要求沿[URL 状态规范](../docs/spec/addp前端路由与可恢复状态规范.md#三必须进入-url-的状态)。深链、刷新、前进后退及 Console／standalone 的验证沿[路由验收规范](../docs/spec/addp前端路由与可恢复状态规范.md#八实现与验收要求)，静态登记不代表实际运行通过。
- 未配置调度时，调度选项、确认摘要与提示按本次操作显示：具备 `transfer.task.execute` 的新建操作显示“立即执行一次”；无执行权限或编辑保存显示“仅保存任务”，不自动执行。沿用[创建与启动的独立权限](docs/transfer-基本概念及配置说明.md#资源选择与页面入口)，不通过补授权限满足提示。
- 编辑回填与提交保留已有 `auto_scan_metadata`，新建及重置才采用默认 true。批处理输入显示实际生效值：已有显式 `config.batch_size` 时只修改该 override 并保留任务顶层默认值；未声明 override 时只修改顶层 `batch_size`，不额外补写 config 字段。两层批大小的优先级沿[任务字段](docs/transfer-基本概念及配置说明.md#11-传输任务)与[Config 配置字段](docs/transfer-基本概念及配置说明.md#21-顶层字段)。仅修改批大小不得顺带改变源查询、映射、调度或扫描开关；编辑保存只更新定义，不触发启动或扫描。
- 成功落点沿[Canonical URL 规范](../docs/spec/addp前端路由与可恢复状态规范.md#五canonical-url)：正式向导创建成功且有 `transfer.task.read` 时，用响应的新 ID `replace` 到详情，不要求 update；无读取权时反馈成功、重置并留在创建页，不读取列表／详情。编辑保存和可读账号确认取消仍 `replace` 回列表；创建助手弹层关闭后刷新当前列表。
- 列表进入创建、编辑、详情和执行页使用 `push`，Console／standalone 的唯一公开历史沿[历史栈规范](../docs/spec/addp前端路由与可恢复状态规范.md#六浏览器历史语义)。保存失败或撤销确认保留 URL 与草稿；创建后的启动失败不撤销已创建对象，仍按创建成功的权限分支落点并单独提示启动失败。本轮受控浏览器覆盖及未验收范围见[创建落点与历史栈证据](#创建落点与历史栈验证)。

## 数据库启动所有权

Transfer schema 仅由 Backend 迁移；模块级数据库锁、成功提交的版本事实及 Backend 就绪后启动 Worker 的顺序统一见[模块迁移与 Worker 启动顺序](../docs/spec/addp开发服务生命周期与构建身份规范.md#模块迁移与-worker-启动顺序)。Worker 只读校验模块与公共 schema，不自动修复；结构或初始化迁移变化须递增 Transfer `SchemaVersion`，缺失或不匹配时拒绝启动领取。

原生开发的 bounded／continuous Worker 分别由 `ADDP_DEV_TRANSFER_BOUNDED_WORKER_REPLICAS`、`ADDP_DEV_TRANSFER_CONTINUOUS_WORKER_REPLICAS` 选择副本，默认 1，仅接受 1／2。编号单元、PID／工作区归属、独立进程身份及共享 `ADDP_HOST_NODE_ID`、启动补缺、缩容和停止规则统一见[Worker 启动单元](../docs/spec/addp开发服务生命周期与构建身份规范.md#metatransfer-与-quality-worker-启动单元)。持续同步双副本须省略固定 `TRANSFER_CONTINUOUS_WORKER_INSTANCE_ID`，保留每进程独立租约 Owner；多进程部署或生命周期夹具通过不代表真实任务竞争、主机绑定或 HA 已验收，实际范围见[开发与验证](#开发与验证)。

## 开发与验证

CDC 捕获资源名沿全局 `apply_identity`／generation 生成，源端同名资源须先核验所有权，具体见[捕获资源登记](../docs/spec/tasks/Transfer数据库CDC.md#捕获资源所有权与登记)。命名隔离／长度及预留 CAS 沿 `make test-module MODULE=transfer GO_ONLY=1`，真实 PostgreSQL publication 碰撞、同表陌生对象拒绝及拒绝后零修改沿 `make test-transfer-postgres` 的 Capture 专项；不以此代替真实 CDC 事件联验。


### 当前启动与测试入口

开发启动、由用户执行的后端重启、实际端口与就绪检查统一见[README 启动说明](README.md#开发启动与就绪检查)。测试和构建不要求重启开发服务；就绪响应不证明 Worker、源读、目标写或公开入口已经验收。

标准命令、环境前提与覆盖范围按用途阅读：

- 模块、Go 定位、PostgreSQL 专项与 Hosted runner：统一见[README 门禁说明](README.md#常规模块与专项门禁)，含实际测试连接、凭据注入、PostGIS 与非空间隔离要求。
- ArcGIS 格式集成：原生依赖、Runtime、Business Oracle、样本、临时对象权限与部分运行边界见[ArcGIS 专项](README.md#arcgis-开放格式专项)。
- Oracle CDC：两类显式开关用例的标准入口缺口、环境与故障窗口见[专项入口缺口](README.md#oracle-cdc-专项入口缺口)；普通门禁成功或未激活用例的跳过不计为这两类实测通过，不以直接语言命令替代标准入口。

真实 T4 的专用身份、最小权限与独占部署遵循[Online 规范](../docs/spec/testing/T4-Online验收规范.md)，不复用个人开发服务或扩大生产权限。以下分版本证据只适用于所列源码和运行尝试，不代表当前 HEAD 已重跑；标准门禁、runner 回归、实际运行与历史结论分别认定，未运行、失败、跳过和未覆盖项不计为通过。各后续增强的独有验收边界见[清单验收范围与证据](../docs/next/transfer后续能力清单.md#六验收范围与证据)。

常用日志：

- `logs/transfer-backend.log`
- `logs/transfer-bounded-worker.log`
- `logs/transfer-continuous-worker.log`

### 专项约束与分版本证据

现行规则由下表“正文入口”维护；本节保存各版本的实际结果、失败与修复、清理及未覆盖项，不作为当前 HEAD 已重跑的证明。标准命令与通用验收边界见[当前启动与测试入口](#当前启动与测试入口)，各专题的独有边界继续随原记录保留。

| 历史证据专题 | 现行正文入口 | 需要分别认定的范围 |
| --- | --- | --- |
| [continuous 过程事件与浏览器](#continuous-过程事件与浏览器验收) | [运行、提交与观测](#continuous-运行提交与观测)、[过程事件契约](../docs/spec/tasks/执行监控.md#通用执行读取与过程事件) | 事件数据面、真实浏览器、留存清理。 |
| [专业执行读取与响应](#专业执行读取与响应收敛) | [专业响应规范](../docs/spec/tasks/执行监控.md#transfer-专业执行读取与响应) | HTTP／PostgreSQL 隔离、Hosted 专业字段及双用户浏览器范围。 |
| [SQL ETL 与 bounded 写后扫描](#sql-etl-与-bounded-写后扫描) | [查询源](#查询源与构建器)、[写后扫描](#写后扫描触发与范围) | 三轮执行／结构演进、bounded 精确扫描。 |
| [bounded 失败诊断与配置预检](#bounded-失败诊断与配置预检) | [失败诊断](#bounded-失败诊断)、[只读预检](#只读配置预检) | 保护拒绝、只读预检、本机复验与隔离 T4。 |
| [角色资格到期与导出拒绝](#角色资格到期与导出拒绝证据) | [角色专用导出选择](#角色专用导出选择) | v30／v31 错误映射、确定性回归与未运行的新版 Hosted。 |
| [continuous 精确扫描与审批归属](#continuous-精确扫描与审批归属) | [生产来源](#生产来源与写后扫描)、[写后扫描](#写后扫描触发与范围) | 首次扫描、单用户审批、双用户归属及各自清理证据。 |
| [bounded 定时授权](#bounded-定时授权验收) | [定时任务授权规范](../docs/spec/tasks/任务定义与授权.md#transfer-定时任务授权) | 真实 Cron、子扫描隔离与模块聚合失败。 |
| [创建落点与历史栈](#创建落点与历史栈验证) | [Canonical URL](../docs/spec/addp前端路由与可恢复状态规范.md#五canonical-url)、[历史栈规范](../docs/spec/addp前端路由与可恢复状态规范.md#六浏览器历史语义) | 本地 T1／T2／受控 T3、模块聚合失败、两类账号日常 Console 路由复验、[提示文案修正与证据](#创建提示文案修正与证据2026-11-02)及隔离 T4／T5 未验。 |

#### continuous 过程事件与浏览器验收

现行事件写入、Runtime lease／fencing 裁决、attempt、累计计数、预算、30 天清理与安全字段统一见[过程事件契约](../docs/spec/tasks/执行监控.md#通用执行读取与过程事件)，恢复与停止沿[continuous 生命周期](docs/transfer-任务语义与同步模式.md#64-生命周期)。Common 不读取 Transfer 私表，专业响应不暴露内部 `owner_instance_id`；事件与 System 平台日志、操作审计的生命周期分开，不由下列历史结果扩展契约。

真实场景使用手工 Hosted suite [transfer-continuous-events](../docs/spec/testing/scenarios/同步与引擎消费.md#transfer-continuous-events)，沿独占生命周期、System IAM 测试身份和正式 Gateway／Owner／Worker；Business 物理夹具独立核对业务 Kafka 与 PostgreSQL CDC 目标事实。连续执行浏览器验收复用该 suite 与 Console canonical 路由，在事件已提交后核对时间线、自动刷新新增计数、恢复来源与失败原因；Python 夹具沿既有 IAM 入口撤权，浏览器先确认已展示授权详情，再核对旧内容清空和重新登录后的 Monitor-only 拒绝。私有阶段控制文件与浏览器原始输出不归档，只归档闭合报告和安全截图。

持续写入验收在同一 Kafka 任务追加记录并确认目标提交后沿原游标续读，将拼接结果与 Monitor 和 Owner 从头读取的完整事件顺序对照。Console 核对自动续页和手动刷新后的顺序与行数，并在 Worker 失联后于同一页面核对原执行的失败诊断与唯一终态事件。已归档的两轮 Hosted 分别停在整引擎扫描和目录基线读取，均未创建 Kafka 任务，上述断言均未执行。旧版租户日志终审保留当次两页四条事件与撤权后旧游标拒绝的结果。[现行场景](../docs/spec/testing/scenarios/同步与引擎消费.md#transfer-continuous-events) · [持续写入两轮 Hosted 结果](../docs/spec/testing/evidence/Monitor执行监控与留存历史证据.md#持续写入期间续页与刷新首次-hosted2026-11-07) · [固定版本租户日志终审](../docs/spec/testing/evidence/Monitor执行监控与留存历史证据.md#2026-10-23-固定版本租户日志终审)

确定性入口 `make test-transfer-continuous-online-runner` 与 `make test-online-runner`、ES module 语法校验和真实部署的 Chromium 准备均沿既有登记；Transfer T1／T2、前端及共享消费者扩散见[标准门禁](README.md#常规模块与专项门禁)与[测试规范](../docs/spec/addp测试与验收规范.md#三标准入口)。此处事件接入未新增 API、事件字段或基础设施；门禁与 API 通过不代替真实 Hosted 或浏览器验收，下列结果仅适用于各自固定版本。

2026-10-12 continuous 过程事件本地门禁通过：`make test-module MODULE=transfer` 完成平台 T0、后端 T1、前端 130 项单元／16 项浏览器及构建、PostgreSQL T2；`make test-go` 全仓 Go 消费者、`make test-common-postgres`、`make test-monitor-postgres` 和执行夹具门禁均通过。真实 PostgreSQL 覆盖 claim／进展／终态、错误码、错配 Tenant／模块／execution、过期及锁等待后的进展／终态／续租拒绝、事务回滚、并发每日预算、暂停过期收敛、恢复的新 execution 和唯一终态、清理保留概览与事件，并清除测试自有事实。`make test-changed` 因未配置隔离 MySQL／OceanBase 测试连接在环境预检退出，Common MySQL／OceanBase 与 Model MySQL 等聚合消费者 T2 未运行，不计为通过；由现有 Release and T2 gates 的 Hosted 门禁复验。真实 Kafka／CDC 认证及 continuous 事件 T4 本轮未运行，既有 T4 收据不覆盖本次事件接入；已同步现有 continuous 集成夹具的事件清理，未以编译通过代替真实数据面验收。

2026-10-12 在提交 `a35e96a9116c8ac95fac7f08ae51773c4ffbaf15` 的 [Hosted T4 38269328262](https://github.com/pampa0629/addp/actions/runs/38269328262) 通过 continuous 过程事件完整链路，共 9 组真实检查：正式 Meta 源扫描、Kafka 目标提交与过程事件、同租户 Owner 权限及跨租户隔离、真实 Worker SIGKILL 后的 lease expiry／新 UUID／位点恢复、暂停恢复与坏记录、Owner 不可用、正式 IAM 撤权后的旧令牌及新 Monitor-only 会话、PostgreSQL CDC snapshot／增删改／确认停止，以及删除任务定义后的授权历史读取。坏记录的 `missing_source_key` 同时在 Transfer 和 Monitor 安全响应中核对；Supervisor 保留既有 RecordDataError 稳定码与安全 Message，不持久化 Cause 或外层包装文本，过程事件不新增错误文本或业务样本。原 Worker PID 50020 由标准生命周期替换为 57334，5 个服务构建身份均匹配测试提交；2 个任务定义清理通过，授权历史在独占部署销毁前保留核验，Hosted 容器／网络／卷零残留。30 个归档文件未含身份、描述及私有运行文件，未检出 JWT、addp_at 令牌和所检查的夹具密码模式。当前提交的干净副本通过 `make test-transfer-continuous-online-runner`（26 项及 System 夹具 Go 测试）和 `make test-transfer-backend`；本地 `make test-transfer-postgres`、`make test-go` 通过，七类记录错误码的持久化和安全投影已由 PostgreSQL 回归验证。本轮 `make test-module MODULE=transfer` 在平台日志生命周期夹具 `test_disabled_metrics_never_stop_foreign_container` 的 10 秒超时处失败，不计为通过；同提交 CI 的全仓 Go 与 Transfer 前端通过；[Platform CI 38269048586](https://github.com/pampa0629/addp/actions/runs/38269048586) 的 Platform consistency 在公共 Hosted 登记回归失败：ES/HDFS 的 `scripts/utils/hosted-online.sh` owner contract 不完整，栅格删除 `verify_online_product_runtime` 片段的负向用例未触发预期拒绝（3 errors、1 failure），未改动其他会话仍在修改的登记文件。Release/T2 的 [Transfer PostgreSQL 15 门禁](https://github.com/pampa0629/addp/actions/runs/38269048728) 已通过；完整自动 workflow 未计为通过。本 T4 不包含真实浏览器验收、180 天概览清理或确定性坏记录的恢复策略变更，System 平台进程日志与审计未改动。

#### 专业执行读取与响应收敛

连续位点使用 `ChangeStreamPosition.Values` 的十进制字符串 `values.next_offset`，不接受另一套数字表示，沿[ChangeStreamReaderProvider 契约](../docs/spec/addp引擎插件接口规范.md#changestreamreaderprovider)。专业读取、用户／机器授权及安全 DTO 沿[专业响应契约](../docs/spec/tasks/执行监控.md#transfer-专业执行读取与响应)，不由 Monitor 通用读取权扩大专业结果范围；创建、委托创建和配置预检约束不因响应收敛改变。continuous 事件接入另见[过程事件专题](#continuous-过程事件与浏览器验收)。

T1 HTTP 隔离／投影、T2 PostgreSQL 与前端回归沿[标准门禁](README.md#常规模块与专项门禁)；新增 T2 须登记到 `scripts/test/transfer-postgres-gate.sh`，Swagger 同步沿[API 维护规则](#配置provider-与-api-维护)。下列本地、CI 与 Hosted 结果分别认定，不代替当前版本重跑或完整身份矩阵验收。

2026-10-12 执行响应收敛已由 `c1f993b1fa7c5d5e403e463c371c46b2adf4ae80` 纳入 main；`task_handler.go` 的创建、委托创建及配置预检逻辑没有变化。位点字符串修正补充了 0、普通位点、int64 最大值和真实 PostgreSQL 详情读取回归；后端 `make test-transfer-backend`、`make test-transfer-postgres`、前端 `make test-transfer-frontend`（130 项单元、16 项浏览器及构建）、Swagger 35 个路由覆盖、执行夹具和 ETL runner 37 项均通过。模块聚合初次因 Ontology 来源冻结校验失败：handler 与来源清单引用的旧提交不一致，Agent/Skill 等来源亦有差异。Ontology owner 在共享工作区同步来源后，2026-10-12 本地完整 `make test-module MODULE=transfer`（Platform T0、Go T1、前端 T1/T3、PostgreSQL T2）及 `make test-online-runner` 均退出成功；本模块没有修改或放宽来源门禁。位点补修提交为 `301e421e64a8227ad39cb6705104163cc3e27198`。上述确定性验证不代替本轮专业读取契约的真实 T4。

2026-10-12 真实 Hosted T4 [38186991049](https://github.com/pampa0629/addp/actions/runs/38186991049) 在提交 `301e421e64a8227ad39cb6705104163cc3e27198` 通过：8 个服务构建身份一致，Transfer 专业响应的根字段、metadata、冻结结构与字段映射白名单均由真实读取断言核对；三轮 Orchestrator 父子执行、MongoDB 嵌套字段、关系型多源 CTE/JOIN/UNION、500 字段宽表及结构演进历史和浏览器图均通过。11 个临时资源全部删除，suite 残留为 0，基础设施清理零残留；55 个归档文件未发现凭据文件或原始 JWT Bearer。跨用户、跨租户、删除定义历史、撤权及机器入口隔离由上述 HTTP/PostgreSQL 回归验证，不能将本 T4 扩大表述为这些场景均经过真实双用户浏览器验收。该 T4 验证了 continuous 的诊断快照和位点读取；运行当时 continuous 过程事件尚未接入 `common.execution_events`；当时事件写入仅支持 bounded lease，本轮扩展由 Transfer 现有 Runtime lease/fencing 原子裁决 continuous 写入（见本节接入说明），不直接套用 bounded helper。

2026-10-11 真实 Hosted T4 [38139520110](https://github.com/pampa0629/addp/actions/runs/38139520110) 在提交 `25ec6e336574de8d99956eeb6aae290a93297c51` 通过：8 个服务构建身份一致，三轮 Orchestrator 父子执行及结构演进、MongoDB／关系型字段血缘和浏览器图均通过；11 个临时资源全部删除，基础设施清理零残留。此结果验证安全父执行引用适配，不代替后续 Transfer 专业接口权限收敛的验收。

#### SQL ETL 与 bounded 写后扫描

现行场景见 [SQL ETL／字段血缘验收](../docs/spec/testing/scenarios/同步与引擎消费.md#字段级血缘验收)；父步骤的身份与子 execution UUID 沿[Orchestrator 专业响应](../docs/spec/tasks/执行监控.md#orchestrator-专业执行读取与响应)，不从父响应索取 `tenant_id`、配置或子任务 outputs。Transfer／Develop 子执行经各自 Owner 读取专业事实，Monitor 独立核对 Owner、任务和父子关联，不以父步骤复制结果证明三轮执行或结构演进。`make test-transfer-online-runner` 的[ETL 回归](../scripts/test/transfer-relational-sql-etl-online_test.py)沿既有 `make test-online-runner`、Platform T0／CI 聚合；确定性入口与真实 Hosted T4 分别认定。

精确扫描的来源与目标沿[生产来源契约](../docs/spec/tasks/生产来源与产物扫描.md#生产执行与后台扫描的来源)和[写后扫描配置](docs/transfer-基本概念及配置说明.md#十一写后-meta-扫描)。两套 Transfer suite 均须经 Meta 正式只读接口核对兄弟项身份、扫描时间／深度和数据统计不变，schema 到 root 的扫描状态、时间和深度不变；父节点当前子树统计可随新目标创建变化。秒精度 DTO 的基线须跨过已观察时间刻度后再触发，不能让同秒截断掩盖扩大扫描；共享检查见[Online API 边界辅助](../scripts/utils/online-api.py)。bounded 场景核对三次原生表写入，结果为 `field_lineage.precise_scan_scope`；continuous 的阶段与独立证据见[首次扫描与审批归属](#continuous-精确扫描与审批归属)，不互相替代验收。

2026-10-16，提交 `dcd74b58c` 的 [Hosted T4 38884014630](https://github.com/pampa0629/addp/actions/runs/38884014630) 已通过并下载报告独立核对：`field_lineage.precise_scan_scope` 验证三次原生表写入后，源表这一兄弟项及 schema／root 两级祖先的扫描事实和状态均不变；实际 Worker 日志的 12 次自动扫描全部提交完整表 locator，无父范围 `catalog_paths`。8 个服务构建提交一致，11 个领域资源全部删除，应用清理 passed、Infra zero_residuals；既有浏览器与血缘场景通过。此 T4 证明 bounded 原生表写后范围，continuous 初始化及结构变更补扫仍以现有 HTTP／Go 回归为依据，未扩大表述为已完成真实 CDC 扫描验收。

#### bounded 失败诊断与配置预检

bounded 安全诊断链路已在 `c322167b8` 的 [Hosted T4 38219829612](https://github.com/pampa0629/addp/actions/runs/38219829612) 经真实 MongoDB 血缘拒绝验证，既有正常保护和 Security 不可达期间的查询搬运均通过；不可证明派生查询返回稳定 `query_output_lineage_unresolved` code，零写入且目标前后为空。`make test-transfer-backend`、`make test-transfer-postgres`、Swagger 35 路由覆盖及隔离已提交前端的单元／20 项浏览器／构建验证通过，同提交 Platform CI 33 项通过。共享工作区模块聚合曾因另一轮前端迁移的四个翻译键用例失败，未计为通过；不将工作区未完成的迁移纳入本次交付。

2026-10-15 原生快照配置预检修正了按写入策略核验目标能力的逻辑；现行要求统一见[目标能力与写入策略](docs/transfer-基本概念及配置说明.md#目标能力与写入策略)及[快照覆盖与未检查项](docs/transfer-基本概念及配置说明.md#快照覆盖与未检查项)。本条未注明固定源码 SHA，以下仅保留当时门禁与本机复验结果。

沿既有 `make test-transfer-backend` 验证新增 11 场景及全部后端用例通过；`make test-transfer-postgres` 的 11 项真实集成、`make test-transfer-frontend` 的 130 项测试／20 项浏览器／构建、Swagger 35 路由覆盖和平台来源审查均通过，Ontology Go T1 通过。完整 `make test-module MODULE=transfer` 在共享 GDAL 生命周期的 `test_failed_sync_removes_record_and_recovers/FAIL_CHECK` 和 `test_launch_owns_listener_and_cleans_up_failure` 两项 20 秒超时处失败（本组 70 项），不得计完整模块通过；未修改该共享测试或阈值。未新增测试入口、依赖或 CI 路线，原 Transfer／Platform CI 自动发现覆盖本修复。

用户按标准入口重启 Transfer 后，已通过真实 Console 复验：任务 13 原 append 配置返回 supported；仅在未保存草稿改为 replace 后返回 unsupported，诊断为 `registered_native_access_not_supported`，位置 `/config/target/policy/apply_mode`。取消编辑后未更新或重新执行任务，Manager 数据项 44575 的目标预览仍为三行七字段；本轮预检修复的本机运行复验完成，不计为隔离 T4 或完整模块门禁通过。

#### 角色资格到期与导出拒绝证据

以下按 v30／v31 保留各自的错误映射、确定性验证和未覆盖范围。

2026-10-25，既有隔离 suite 领域 v30 增加角色资格自然到期专项：期限内独立任务输出和经 Develop/Transfer 生成的实际 CSV 均全遮盖；同一登录会话自然到期后任务启动、名称更新及直接选择核验409/transfer_protection_roles_unavailable，任务不新增执行且目标保持；旧文件/会话与再次导出经现有 Develop BFF 为409/query_protection_choice_invalid，不把合并提示声称为已细分。显式清除选择后默认任务/新CSV恢复，旧特殊文件仍拒绝。主功能权限、Policy与投影保持，成功不撤销/重登/续期；失败/中断先删任务再清理仍有效资格，精确保留其他用户审计。新增场景及凭据隔离沿[Transfer 资格自然到期与旧文件](../scripts/docs/Online数据保护.md#transfer-资格自然到期与旧文件)；`make test-security-online-runner` 的38/55/30/274项Python、相关Go及登记通过（T0/T1），真实HTTP/Hosted未运行，不新增浏览器通过声明。本轮未变更产品API、授权规则或服务生命周期。

2026-10-25，Common Transfer BFF现已严格保留资格/方式两类已声明409，Develop导出创建、状态及文件分别映射query_protection_roles_unavailable和query_protection_choice_invalid，源Transfer自己的错误码和裁决保持。未知码或异常响应不再伪报方式失效。隔离领域v31已将自然到期的导出断言改为角色精确码，Runner T0/T1通过；新真实Hosted T4未运行。原因映射及 v30／v31 验收差异见[Transfer 资格自然到期与旧文件](../scripts/docs/Online数据保护.md#transfer-资格自然到期与旧文件)；v30合并提示仅是历史事实。

#### continuous 精确扫描与审批归属

continuous 精确扫描沿现有 [transfer-continuous-events 场景](../docs/spec/testing/scenarios/同步与引擎消费.md#transfer-continuous-events)与[写后扫描触发规则](#写后扫描触发与范围)：启用 `auto_scan_metadata`，先正式扫描独立兄弟表、冻结 schema／root 基线，再分别观察 Kafka 建表和 CDC snapshot COMPLETED 后的首次扫描。目标须从基线不存在收敛到 deep 字段事实，恢复、普通事件及再次 resume 不重复首次扫描；各阶段的兄弟项、祖先与时间基线核对沿[精确扫描方法](#sql-etl-与-bounded-写后扫描)。结构审批补扫另核对真实变更、审批和恢复，不与首次扫描合并认定。标准回归为 `make test-transfer-continuous-online-runner`，共享检查同时由 `make test-transfer-online-runner` 覆盖；真实运行结果按下列固定版本收据分别认定，runner 通过不计为 T4。

continuous Hosted 物理源准备以最终 TCP 服务成功查询 `continuous_fixture` 为就绪条件，不使用会命中 PostgreSQL 镜像临时初始化服务的 Unix socket `pg_isready`。有界等待耗尽必须在建源表及启动业务 Kafka 前失败，Hosted 退出路径继续清除当次资源。临时服务误判和耗尽拒绝由 `make test-transfer-continuous-online-runner` 中既有夹具回归覆盖，无新增入口或数据库。

2026-10-16，提交 `1c40fe0351cb262bf4dae5e306e65b89f74cf517` 的 [Hosted T4 38904000444](https://github.com/pampa0629/addp/actions/runs/38904000444) 已通过，并下载归档独立核对。`precise_scan_scope` 证明 Kafka 建表与 PostgreSQL CDC snapshot COMPLETED 后各完成一次 deep 目标扫描，兄弟项及 schema／root 两级祖先扫描事实不变；实际 Worker 日志恰有两次提交，均为完整目标表 `targets`，恢复、暂停恢复和普通事件未重复扫描。5 个服务构建提交一致，10 组检查、4 个执行 UUID 和 live／recovery／revocation 三组自动浏览器检查通过；2 个任务定义清理 passed，Hosted 容器／网络／卷 zero_residuals。此收据只覆盖 continuous 首次扫描，结构审批补扫归属仍按已确认范围另行处理。

首轮 [Hosted 38902870557](https://github.com/pampa0629/addp/actions/runs/38902870557) 在业务源初始化时误判 PostgreSQL 临时 Unix socket 服务就绪，建表报 `database system is shutting down`，尚未执行扫描场景；退出清理 passed、Infra zero_residuals。修复为最终 TCP 服务查询指定数据库后，`make test-transfer-continuous-online-runner` 的 34 项回归及 System 夹具 Go 测试通过，上述重跑完成真实验收；未放宽就绪条件或扫描范围断言。

2026-10-16 结构审批补扫 [Hosted 38921609397](https://github.com/pampa0629/addp/actions/runs/38921609397) 在提交 `9c288cb65de32a8899fe28491c124787e791bb08` 通过，5 个服务构建身份与提交一致，11 项检查、5 个不同 Transfer execution 和 3 个浏览器阶段均通过。三次扫描分别为 Kafka 首次 prepare、CDC 快照完成、CDC additive 审批；原 CDC 目标 DataItem 4 从 `id,label` 更新为 `id,label,scan_extra`，补扫 UUID 为 `0adc7cde-44ba-4b33-8c84-a0809236c2e7`，目标仍为 `addp://engine/1/path/public/continuous_cdc_target?type=table`。同级表及两个上级节点不变；真实目标新增列及提交后的完整行集合由独立 Business fixture 验证。重复审批保留 attempt 1 和同一扫描 UUID，resume 新建执行且不再扫描。2 个定义经正式 API 删除，suite 残留 0，Infra 清理 `zero_residuals`。Worker 日志仅有两次首次扫描提交，均为单表 typed targets；第三次审批扫描由审批路径独立提交，不伪造父 execution。

本轮 `make test-go` 的 22 个模块、Common／Meta／Transfer PostgreSQL、Ontology PostgreSQL／独占 FalkorDB、平台来源审查、Meta／Transfer Swagger 生成及路由覆盖、`make test-transfer-continuous-online-runner` 的 36 项回归与 System fixture Go 检查通过；现有标准入口和 CI 自动发现覆盖，无新增 Job 或数据库。不同审批人与原启动人的读取隔离由 HTTP／真实 PostgreSQL 回归覆盖，本次 Hosted 使用同一消费用户，不扩大表述为真实双用户验收。完整 `make test-platform` 两次未通过：首次在 Falkor 夹具 5 秒等待启动处失败，重跑在 dev lifecycle 的快速 Worker 标记等待处失败；同输入的 dev lifecycle 已在首次聚合通过，Falkor 夹具 9 项及真实 PG/Falkor 标准门禁独立通过，但不能计为完整聚合通过，未修改这些夹具或放宽时限。

continuous 双用户审批验收沿用现有 Hosted suite 和同租户 peer：只为该隔离身份增加 `transfer.task.update` 与 `meta.scan_task.read`，不增加执行、引擎管理或管理员权限。由 peer 审批原消费 User 启动的 CDC 结构变更；Meta 补扫须归 peer、无父执行，原消费 User 不能读取该扫描，peer 不能读取原消费 User 的正式源扫描，历史 CDC 发起人保持不变。原消费 User 重复审批不得接管补扫归属，随后由原消费 User 恢复同步，继续验证单表范围、物理提交及零残留。生产权限及 API 不变，沿用 `make test-transfer-continuous-online-runner` 与同一 Hosted workflow，现有 System fixture Go、runner 与 CI 自动发现覆盖，无新增数据库、入口或 Job；真实运行结果另记收据。

2026-10-17 双用户结构审批 [Hosted 38927137086](https://github.com/pampa0629/addp/actions/runs/38927137086) 在提交 `56edd478c1a1d02a455d8432236ef9034251e5f1` 通过，归档已独立核对。发起人 Principal 33 与审批人 Principal 36 属于同 Tenant、不同 Membership，均为普通 User 且持有 `meta.scan_task.read`；33 可读自己的正式源扫描而 36 返回 404，审批补扫 `55805433-e09e-4f04-abc6-80091e8b73a0` 则由 36 读取成功、33 返回 404。补扫发起人为 36、无父 execution，原 CDC 发起人 33 不变；33 再次提交同参审批仍保留 attempt 1、同一补扫 UUID 和 36 的归属。随后由 33 新建 resume execution，真实新增列与提交后的完整行集合通过独立物理检查，恢复后没有额外扫描。

本轮 5 个服务构建身份与提交一致，12 项检查、5 个 Transfer execution、Kafka／CDC 初扫及审批补扫三次单表范围检查通过；兄弟项及 schema／root 两级祖先不变。Worker 日志恰有两次首次扫描提交，均为单个 typed table target。新增双用户归属场景经真实 API 验证，原 live／recovery／revocation 三组自动浏览器检查继续通过；2 个任务定义删除成功，suite 残留 0，Infra `zero_residuals`。干净源码快照中的 `make test-transfer-continuous-online-runner` 37 项回归、System fixture Go 测试和 Online CI 登记检查通过，5 个改动文件与实际测试提交逐一一致。生产权限及 API 未改，现有入口和 Hosted workflow 直接承接本轮用例。

#### bounded 定时授权验收

现行主体绑定、完整定义摘要、触发／领取重核、暂停清除与旧定义拒绝规则统一见[Transfer 定时任务授权](../docs/spec/tasks/任务定义与授权.md#transfer-定时任务授权)。恢复仅更新未来计划和授权，不改已有 pending／running execution；扫描来源另沿[生产来源契约](../docs/spec/tasks/生产来源与产物扫描.md#生产执行与后台扫描的来源)。以下按固定版本分别认定门禁与真实 Cron／扫描结果，不推定当前 HEAD 已重跑。

2026-10-17 自有 bounded 定时授权闭环已由 `5593d42ae`、`f277fa894` 接入现有 System Task Authorization Subject；来源冻结为 revision 27（`c05431787`）。任务主体的 User／Service 请求拒绝重定向，失效 User 凭据保持 401；没有新增授权实体、API 路线或测试数据库。System 000216 只支持严格 owner/type 二元组，并向 Transfer Runtime 增授既有主体解析 Permission。

[Hosted 38987715068](https://github.com/pampa0629/addp/actions/runs/38987715068) 在运行时代码提交 `c0543178726054fa4245e566cadff13bf46385e0` 真实通过，独立核验了 5 个服务构建身份、13 项检查、6 个不同 Transfer UUID 与 4 次精确目标扫描。定时 execution `d2a77c47-c63e-4660-83a5-a51ea59a928b` 由真实 Cron 触发，发起人为当前授权 User 33；Meta 扫描 `835fbbf7-acfd-4713-8a2f-68d9446b90ba` 继承同一 User 和这个直接父执行。拥有读取权限的 peer 可读父执行，对该一次性子扫描仍返回 404。实际写入两行，扫描只新增 `addp://engine/1/path/public/continuous_scheduled_target?type=table`（DataItem 5，id／label 两字段）；原 Kafka／CDC 目标、兄弟项和 schema／root 两级祖先扫描事实不变。此前双用户审批、初次扫描与 live／recovery／revocation 三组浏览器验证继续通过，3 个定义删除成功，suite 残留 0、Infra `zero_residuals`。37 个归档文件未检出凭据前缀、Service Secret 赋值或业务行哨兵。

最终 `make test-go` 22 个模块、Common Go T1、Transfer／Meta／Orchestrator Go T1、完整 System IAM PostgreSQL 7 个 package、Transfer／Meta PostgreSQL、Ontology PostgreSQL／独占 FalkorDB、来源审查、授权清单和 System／Transfer／Orchestrator Swagger 覆盖均通过。6 个重定向反例及 active execution 期间恢复调度完成红绿验证；IAM 门禁入口 16 项测试通过。后续 `86b4e726f` 仅修正验收失败清理：定时场景先暂停未来触发再等待当前执行结束，41 项 runner 测试与 System fixture Go 通过，55 个运行时／契约文件与该 Hosted 源码一致。现有标准入口和 CI 登记覆盖，无新增 Job。清洁快照 `1c7301253` 的完整 `make test-module MODULE=transfer` 未通过：平台 T0、后端 T1 和前端 130 项单元通过，但 MongoDB query 恢复配置的两项浏览器断言失败（24 项中 22 项通过）；前端构建及聚合后续 T2 未执行，独立后端 T2 已通过，不计完整模块聚合通过，未修改这些前端实现或测试。个人开发服务本轮未重启；更新 Common 后由用户按标准入口重启全部模块，旧定时定义须通过现有保存／恢复入口重新授权。

#### 创建落点与历史栈验证

2026-11-01，提交 `9fe8d9cac00f782db53c70e7c352abe75197c2c0` 同步修订正式路由规范与向导，按当前读取权限决定创建成功落点；编辑保存、取消及列表创建助手分别保留自身行为，不修改 Backend API、Permission Manifest 或共享导航桥。旧实现先复现创建后实际为列表、期望新 ID 详情的断言失败，再由[导航回归](frontend/e2e/task-navigation.spec.js)验证修复；正式行为以[前端公开路由](#前端公开路由)及其规范引用为准。

本地共享工作区通过标准浏览器入口 `node ../../scripts/test/frontend-browser-gate.mjs task-navigation.spec.js`（从 `transfer/frontend` 执行，14／14）、`make test-transfer-frontend`（131 项单元、50 项浏览器及构建）、`make test-module MODULE=transfer GO_ONLY=1`、`make test-transfer-postgres`（19 个主测试，零失败／跳过）和 `make test-frontend-ci-registration`（27 项、21 个前端登记）。五个自有文件的 SHA-256 与提交逐一一致；这是共享工作区验证，不是清洁提交隔离验证。原始证据清单与日志保存在临时目录 `/tmp/addp-transfer-route-20261101/`，非长期归档。

T3 使用真实 Transfer 页面、Router 和共享导航桥，但权限会话与 API 响应为合成夹具，iframe 父层为受控 Console 协议夹具。覆盖可读无 update、仅创建、保存失败、取消／撤销确认、启动失败、历史项数量、单 iframe／无同步重载及深链／刷新／前进后退；不计真实账号、完整 Console 产品或 Hosted T4／T5 通过。完整 `make test-module MODULE=transfer` 在平台 T0 的 scoped-restart `restart.sh -asset -asset` 夹具 30 秒超时，尚未进入 owner 步骤，不计完整聚合通过；独立 owner 门禁结果不能抹去该失败。未启动或重启个人开发服务。

2026-11-02 补核同一提交的 [Platform CI run 41319279732](https://github.com/pampa0629/addp/actions/runs/41319279732)（attempt 1）：已完成，整轮结果为失败。以下 Job 已实际选中并执行，不以跳过步骤或汇总标记认定通过：

| 提交级 CI 证据 | 结果与适用范围 |
| --- | --- |
| [Transfer frontend](https://github.com/pampa0629/addp/actions/runs/41319279732/job/124885844534) | `make test-transfer-frontend` 通过：131 项单元，零失败／跳过；50 项浏览器包含本次 14 项路由回归，Vite 构建通过。仍沿上述受控 T3 夹具边界。 |
| [Execution online CI runner](https://github.com/pampa0629/addp/actions/runs/41319279732/job/124885844391) | `make test-execution-online-ci-runner` 两组确定性测试分别为 100、167 项，通过；这是验收入口与编排回归，不是 Hosted T4 实跑。 |
| [Platform consistency](https://github.com/pampa0629/addp/actions/runs/41319279732/job/124885844463) | `ModuleGateTest.test_manager_postgres_source_checkout_rejects_before_database_access` 失败：`--source-root` 指向 Runner checkout 时，预期 `MANAGER_POSTGRES_TEST_DSN must reference`，实际为 `Manager CI requires its repository-built owned PostgreSQL`。该失败与上述本地生命周期夹具超时分别保留，不能计整轮 CI 或完整模块聚合通过。 |

上述 CI 元数据与三份 Job 日志也保存在同一临时证据目录，API `head_sha` 与修复提交一致；这些 CI 结果不覆盖真实账号和完整 Console，后续日常服务观察见下文，Hosted T4／T5 仍未验收。

##### 日常 Console 路由复验（2026-11-02）

专项会话回传的最终报告为 `/tmp/addp-transfer-route-20261101/console-real-20261102/report.json`（SHA-256：`130172bbf38d02ceae037045ac03f7f61adc38f71d0c5e33fc463ee72dd213c6`），`final-sha256.json` 列出 31 份文件。本会话校验全部哈希，并核对关键截图、AX 快照和创建日志；当次核对时，规范、向导及对应回归共五个文件逐字节匹配 `9fe8d9cac00f782db53c70e7c352abe75197c2c0`，不作为后续提示文案修正后的源码身份。这是日常现有服务的手工证据，本会话未重跑页面；可读分支记录的跟进 checkout 为 `d5f2ab734ba8973719f811abf870949636b13b0c`，当时 System／Gateway／Meta／Transfer 构建为 `c43dceaf87453ca8acf678388b1ae22f79376e40`，仅创建复验发生在用户重启服务之后，不据此声称同 SHA 隔离部署。

| 分支 | 实际结果与适用范围 |
| --- | --- |
| 可读 User | 任务 `17` 创建返回 201，响应 ID 与 Console／iframe 详情一致；落在 `/transfer/tasks/17/detail`，刷新保持同一详情，后退回列表、前进恢复同一详情；单 iframe、创建时无整页重载。最终 Owner 响应显示 disabled／idle、执行总数为 0。仅证明该真实账号分支，不替代受控 T3 的其他权限组合。 |
| 仅创建 User | 用户本人在独立 Safari 无痕会话登录，仅有 `transfer.task.create` 与 `meta.catalog.read`。独立复验任务 `51` 于当日 `15:32:46 +08:00` 创建，POST 返回 201；页面显示“任务创建成功”，向导重置为空白 Step 1、下一步禁用。Console 保持 `/transfer/tasks/create`，iframe 为 `/module-ui/transfer/tasks/create`；刷新后仍为空白创建页，一次后退到原首页 `/`、一次前进恢复创建页，未落到列表或详情。Owner 页面核对同名任务仅一条，状态 idle／手动执行、执行记录为空。 |
| 原任务的观察限制 | 任务 `18` 在当日 `12:15:31 +08:00` 已创建并返回 201，但原 Safari 会话锁屏后未观察成功落点，不能补记该次 UI 通过。服务恢复后 Owner 确认任务名唯一、idle／手动执行且执行记录为空；完整路由复验使用独立任务 `51`，没有重复创建任务 `18`。 |

可读分支创建窗口有完整网络记录，仅见一次创建、零次启动；刷新与历史窗口的记录截断，Safari 复验也没有完整网络捕获，不能声称全部窗口 API 读取／写入计数为零。额外直接访问详情的拒绝检查未运行。报告的 `acceptance_complete=true` 仅表示本次要求的两类账号创建落点、刷新及一次后退／前进已完成；不扩展为全部权限组合、完整产品、隔离 T4／Hosted 或 T5 通过，原有模块聚合与 CI 失败记录保持。

临时原角色到期已由 IAM 页面核实；用户确认仅原两项权限、最多 30 分钟的复验重授后，验收结束已撤销临时角色并结束该账号租户访问，页面显示零个生效角色、已结束。未修改共享角色定义、其他账号或资源授权，未增授 `transfer.task.read`／`transfer.task.execute`，也未删除用户或共享角色。任务 `17`、`18`、`51` 的定义保留审阅，没有任务／数据删除；未探测物理目标表是否存在，不能宣称零残留。用户自行恢复服务，专项会话和本会话均未执行 restart／keepalive。证据仍位于临时目录，不作为长期归档保证。

##### 创建提示文案修正与证据（2026-11-02）

上述日常观察曾记录仅创建且无 execute 的账号看到“立即执行一次”（`create-only-recheck-review.png`），实际任务 `51` 为手动且执行记录为空。修复提交 `90fbcfc16bac188ebfc72f6b1629c9940d7ae8d8` 仅含本文件、向导两个步骤与状态、中英词条和导航回归，共七个文件；未改 Backend／API、权限清单或账号授权。调度选项、确认摘要及警告与创建后实际启动分支共用响应式 `transfer.task.execute` 事实：无执行权或编辑保存显示“仅保存任务／本次仅保存任务，不会自动执行”；有执行权且无 Cron 的新建保持立即执行，Cron 保留表达式且不在创建后立即启动。创建落点及历史栈规则保持原分支。

修复与 CI 原件持久归档于 `/Users/pampa/.codex/artifacts/addp/transfer-execution-hint-20261102/fix-ci/`：`evidence.json` 的 SHA-256 为 `dfe356254c292e44468e30a8a88f2ac13b93e4a8157e570c16701ed1a77263cd`，`sha256.json` 覆盖 18 份日志及元数据。本会话校验全部哈希、七个提交文件及代码分支，并核对下列归档结果，没有重跑门禁或查询其他 CI。`red.log` 保留修正前 8 项失败复现，与修正后的通过结果分开认定。

本地 `make test-transfer-frontend` 通过 131 项单元、60 项浏览器及构建；导航回归 24 项，新增 10 项覆盖中英文、standalone／iframe、有无执行权和 Cron，并强化有执行权的编辑保存断言。回归核对实际 POST 与 `/start` 是否出现，继续拒绝仅创建账号的任务读取。`make test-module MODULE=transfer GO_ONLY=1`、`make test-transfer-postgres`（19 个主测试，零失败／跳过）及 `make test-frontend-ci-registration`（27 项、21 个前端）通过。

以下四个相关 Job 均为修复提交、attempt 1 的已完成记录；归档检查时整轮 Platform 仍为 in_progress，未跟踪其余 Job，不据此认定整轮成功：

| 固定源码 CI 证据 | 实际执行及边界 |
| --- | --- |
| [Transfer frontend](https://github.com/pampa0629/addp/actions/runs/41419073194/job/125189982120) | selected／success；实际执行 `make test-transfer-frontend`，131 项单元、60 项浏览器及构建通过，仍为合成权限／API 的受控 T3。 |
| [Execution online CI runner](https://github.com/pampa0629/addp/actions/runs/41419073194/job/125189981998) | selected／success；实际执行 `make test-execution-online-ci-runner`，两组 114、170 项确定性测试通过，不是 Hosted 实跑。 |
| [Transfer PostgreSQL](https://github.com/pampa0629/addp/actions/runs/41419073237/job/125190786181) | success；实际执行标准 PostgreSQL 门禁，19 个主测试，零失败／跳过，仅计 T2。 |
| [Platform consistency](https://github.com/pampa0629/addp/actions/runs/41419073194/job/125189981750) | failure；`TestTransferReviewSourceDrift/config` 因配置文档冻结哈希不符失败，不能以其他 Job 成功抵销。 |

本地完整 `make test-module MODULE=transfer` 同样在上述平台 T0 失败，未进入 Owner 步骤，不计完整模块聚合通过。`transfer/docs/transfer-基本概念及配置说明.md` 在修复提交及其父提交中逐字节一致，SHA-256 为 `a6cf6796d32eaff2cde1499e892057e90189ca862f16e5d2f4becb87af77c060`，与审查冻结值 `762d8458eb59b2c10a4d5bb623840fde9c96e19ff09ee8bc80261895f75c0f85` 不符；该文档不在本次七个改动中，未修改 Ontology 审查基线或扩范围修复。

**新文案日常 Console 验收（2026-11-02）**：现有管理员账号的真实 AuthContext 具备创建、读取、执行与 `meta.catalog.read`，未调整权限。Step 4 选项、Step 5 摘要与警告均显示立即执行一次；北京时间 17:52:31 一次提交创建任务 `52`，创建返回 201，随后唯一一次 `/task-definitions/52/start` 返回 200。未截断的创建窗口记录为一次创建、一次启动；唯一 execution `2e78bcc4-fdd7-4f24-9309-ecbbdcff4cd1` 于 17:52:32 成功，Owner 记录读取／写入各 3 行、`trigger_type=manual`、`trigger_by=31`，执行列表总数为 1。源为既有 `public.test_pk_source`，仅映射 `id/name`，独立新目标为 `public.hint_execute_20261102_c83f7e`；本轮没有独立源目标逐行物理对账，不由执行计数扩展为完整数据一致性验收。

仅创建分支使用用户自行注册的新临时账号（Principal `45`）；用户明确确认仅 `transfer.task.create`、`meta.catalog.read` 两项权限和最多 30 分钟访问，18:23 左右分配的既有临时角色截止 18:52。真实 IAM 详情核对角色仍且仅含这两项权限，未修改共享角色定义、未增加读取或执行权。Safari 无痕真实 Console 的 Step 4、Step 5 摘要均为“仅保存任务”，警告为“未配置调度计划，本次仅保存任务，不会自动执行。”；18:26:12 一次提交创建任务 `53` 返回 201，成功提示后重置同一创建页，刷新仍为空白向导。管理员随后核对 `created_by=45`、idle／stopped、空 schedule，执行列表 total=0；访问结束后再次读取仍为 0。

仅创建分支没有 Safari 网络拦截。服务端投影日志的本次窗口仅一条创建 POST、零条任务启动 POST，日志采集器 dropped／parse_failures／truncated 均为 0；不将其表述为 Safari 浏览器全量网络捕获。收尾管理员 reload 的 CDP 缓冲已截断，不能据该缓冲证明零请求；保留的实际 executions 响应仍为 total=0。

结束时已正常撤销临时角色并结束租户访问，零个生效角色、状态已结束，Safari 刷新回登录页。原已结束账号没有重新启用；未修改其他账号、共享角色或资源授权，未重启服务。任务 `52`／`53`、任务 `52` 的一次执行及新 PG 目标的 3 条写入均保留审阅，未删除业务任务或数据；仅创建分支的 MySQL 目标只是定义，未独立查询物理表是否存在，不宣称零残留。前述任务 `17`／`18`／`51` 对应修正前页面，不能套作本轮新文案证据。

最终原件持久归档于 `/Users/pampa/.codex/artifacts/addp/transfer-execution-hint-20261102/console-real/`：18:28 检查点 `report.json` 的 SHA-256 为 `676d0aaa4ea3235203f99797ac2754c68ec3aebebf7f9587beaded6fd3b69417`，`sha256sums.txt` 覆盖 36 份原件，本会话逐项校验并核对页面、权限、网络／日志、执行和访问结束事实。此前 17:54 部分检查点（SHA-256 `c4a05c691935d6d97b90d1041cdcd5bb515671515e2e5bb67085c971c83d0405`）的 12 份冻结原件在同一归档根目录的 `console-owner-checkpoint/` 按当时范围保留；修复阶段 `evidence.json` 的 `real_console_hint_retested:false` 也只表示当时状态，由最终报告补充，不改写旧时间点事实。

本轮沿用仓库外本机持久归档方式，66 份证据原件和两份原始清单复制后逐字节及 SHA-256 校验一致；根清单 `sha256.json` 登记 68 份文件，SHA-256 为 `1206443ba51c315438a1b09214aa3d2678507c0c1b193da7506e09f5daaa2267`，目录权限 0700、文件权限 0600。未新增长期文档或脚本，未归档凭据目录或浏览器会话状态；后续复核不依赖 `/tmp` 原件。

五个产品前端文件最终逐字节匹配修复提交 `90fbcfc16bac188ebfc72f6b1629c9940d7ae8d8`；System／Gateway／Meta／Transfer 的 Ready 报告均为 `39059d2ed2d711b992947c1debee6872a0d60578`，前后端不是同一构建版本。最终 `acceptance_complete=true` 仅指本轮中文日常 Console 中“有执行权的新建”与“仅创建且无 Cron”两分支；英文、编辑和 Cron 仍依赖上述自动化证据，不计这些分支的实机通过，也不计同 SHA 隔离 T4／Hosted 或完整产品通过。未重跑 CI；上述 Platform consistency 和模块聚合失败保持原判定。本机持久归档不是 GitHub Artifact 或跨主机备份，不扩展原验收范围。

##### 编辑保真修复与定向 CI（2026-11-04 补录）

修复提交 `54e47bbdb4575870832f75ca3d69adcf082ca78b` 保留编辑任务已有扫描开关和批大小字段归属，行为约定见[前端公开路由](#前端公开路由)。原源码九项反例失败、修复后九项通过；本地 `make test-transfer-frontend` 的 130 项单元、69 项浏览器和构建，以及前端 CI 登记 27 项／21 个前端通过。受控回归使用正式编辑路由 GET、`loadFromTask` 和 Meta 回填，仅跳转向导步骤后由 UI 修改批大小，经正式确认调用一次 PUT；完整载荷核对 MQL、16 项 JSON 映射、disabled／schedule／扫描开关等不变，且无启动、审批或扫描请求，不是直接 API 保存。

同一源码的 push attempt 1 [Transfer frontend Job 125935242969](https://github.com/pampa0629/addp/actions/runs/41641312473/job/125935242969) 已实际选中并执行 `make test-transfer-frontend`：130 项单元（130 通过、零失败）、单 Worker 的 69 项浏览器及构建 6.64 秒全部通过，结束于北京时间 2026-11-03 23:25:30。补录核对官方 Job／Run 身份和原始日志，88335 字节日志 SHA-256 为 `228583df008110f506c0372801ec37bb354df6f13267fb8b296e9047a3e7590a`；证据位于 `/tmp/addp-transfer-edit-fidelity-20261103/`，`evidence.json`／`ci-frontend.log`／`sha256.json` 保存结果及摘要，原排队观察保留在 `evidence-queued-checkpoint.json`，不改写旧时间点事实。本次只补证据与文档，没有重跑、另派或操作真实 Task55、审批、执行、扫描及开发服务；不以该定向 T1／受控 T3 结果认定父 Platform 整轮或 Task55 真实配置漂移验收通过，后续 Task55 本机现场结果单列如下，不回写本次定向 CI 的原范围。

##### Task55 后续本机现场验收（2026-11-04）

**旧审批拒绝**：Session `58` 的审批绑定批大小 1000；Task `55` 经正式编辑仅将 `batch_size` 改为 500，其余完整可写配置不变。保留的原响应为 HTTP 409／`approval_request_mismatch`，独立 PostgreSQL 快照确认旧审批仍为 pending、未获批未消费，该轮零新增 execution。两个观察 tab 共记录四个 409，其中三份原始响应正文已保存，一份正文缺失；不能据请求数认定点击次数或自动重试。更早 Session `56` 缺原始 decision body，不追认通过。负向回执 `/tmp/addp-task55-round2-final-receipt-20261104.json` 的 SHA-256 为 `301ba1dc8227967565d093230c9ffb4e9e5562a72bb7c3d26155f4c612f7c85f`，所列 22 份原件逐项核验一致。

**当前配置的新审批执行**：原 owner 在用户另行授权下完成 Session `59` 的新审批；审批仅消费一次，唯一新增 execution `dcc73e0c-2486-45ef-9712-e1f81d5b4295` 为 success，读写各 2188 条。执行事件为四批 500 加尾批 188，Agent 的正式 `transfer.execution.get` 报告与独立 PostgreSQL 结果一致。正向回执 `/tmp/addp-task55-normal-final-receipt-20261104.json` 的 SHA-256 为 `906fc41bf5a6b89cf2c8e8d89e9a5b7be2619a031a2b6c4654f088753c5e9f38`，所列 12 份原件逐项核验一致。收尾 Task 保持批大小 500、原完整可写配置不变，idle／stopped／disabled、`auto_scan_metadata=false`、无调度；任务、写入数据和本次成功执行保留，不宣称业务零残留。

**验收边界**：两轮本机运行事实与前述修复源码的定向 CI 分开认定，不据重复的 409 请求推定自动重试；本次文档补录没有修改权限或代码，也没有再执行审批、启动、扫描、重启、自动重试或 CI。正向任务页 reload 的资源事件缓冲截断保留为缺口，后续执行详情事件完整；不宣称逐行逐字段数据等价、Hosted、同 SHA 全链路或平台全绿。上述回执位于本机临时目录，不代表持久归档、GitHub Artifact 或跨主机备份。

## 相关文档

- [快速开始](docs/快速开始.md)
- [前端开发与集成](frontend/README.md)
- [前端操作起点](frontend/QUICK_START.md)
- [模块启动与标准验证入口](README.md#启动与验证)
- [数据库架构](docs/数据库架构.md)
- [DLQ 公开查询与来源隔离](docs/数据库架构.md#dlq-公开查询与来源隔离)
- [DLQ availability 失败与验收边界](docs/数据库架构.md#availability-失败与验收边界)
- [基本概念及配置说明](docs/transfer-基本概念及配置说明.md)
- [table Transfer 配置](docs/transfer-基本概念及配置说明.md#三table-transfer-支持范围)
- [format 声明与来源](docs/transfer-基本概念及配置说明.md#format-声明与来源)
- [raw copy 配置](docs/transfer-基本概念及配置说明.md#四raw-copy-支持范围)
- [raw copy 目标继承与一致性](docs/transfer-基本概念及配置说明.md#raw-copy-目标继承与一致性)
- [Continuous/Kafka 配置](docs/transfer-基本概念及配置说明.md#八continuouskafka-v1-契约与当前实现边界)
- [数据库 CDC 配置](docs/transfer-基本概念及配置说明.md#九数据库-cdc-v1-已冻结边界)
- [Checkpoint 恢复分级](docs/transfer-基本概念及配置说明.md#恢复分级)
- [当前架构设计](docs/design.md)
- [当前支持矩阵与任务语义](docs/transfer-任务语义与同步模式.md#三当前支持矩阵)
- [Transfer 后续能力清单](../docs/next/transfer后续能力清单.md)
- [转换器架构分析（历史归档）](docs/transfer转换器架构分析.md)
- [高性能分析（历史归档）](docs/transfer高性能分析.md)
- [transfer.transfer_tasks 表](docs/tables/tasks表.md)
- [common.task_executions 中的 Transfer 执行记录](docs/tables/task_executions表.md)

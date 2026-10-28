# Orchestrator 模块说明

模块职责与开发约束如下；稳定执行语义沿[编排与调度规范](../docs/spec/tasks/编排与调度.md#orchestrator-执行可靠性)，固定版本、失败／取消、清理及未覆盖范围见[历史证据导航](#历史证据导航)。

## 模块定位

Orchestrator 模块负责任务编排、DAG 执行、定时调度、跨模块任务调用和任务提供者发现。它不直接处理业务数据，而是编排 Meta、Transfer、Manager、Develop 等模块通过 TaskProvider 声明的任务能力。

## 技术栈与端口

- 后端：Go + Gin + GORM，默认端口 `8084`，环境变量 `ORCHESTRATOR_BACKEND_PORT`。
- 前端：Vue 3 + Element Plus，开发端口 `5177`，启动脚本环境变量 `ORCHESTRATOR_FE_PORT`。
- 数据库：PostgreSQL `orchestrator` schema。
- 依赖：System、Redis、各任务提供者模块。

## 重要目录

```text
orchestrator/
├── authorization/
│   └── permissions.yaml       # Orchestrator Permission Manifest，发布期聚合事实源
├── backend/
│   ├── cmd/server/main.go
│   ├── internal/api/handler.go
│   ├── internal/api/router.go
│   ├── internal/models/orchestration.go
│   ├── internal/repository/repository.go
│   └── internal/service/      # executor、scheduler、engine registry、task client
├── docs/
│   ├── 数据库架构.md
│   ├── 参数化模板说明.md
│   └── tables/
└── frontend/src/
    ├── components/            # DAGEditor、TaskPanel
    ├── views/                 # OrchestrationList、OrchestrationForm、ExecutionList（单编排历史）
    └── api/
```

## 核心 API

Orchestrator 是 `orchestrator.workflow.*` 的 Permission owner；定义只存在于 `authorization/permissions.yaml`，通过 `common/authorization` 发布期聚合，不在服务启动时动态注册。`orchestrator.workflow.cancel` 是 IAM 目标目录能力，当前 TaskProvider 明确声明 `supports_cancel=false`，没有用户取消入口；停机与失联收敛不等同于用户取消。

路由前缀：`/api/v1/orchestrator`。

- 编排管理：`POST /orchestrations`、`GET /orchestrations`、`GET /orchestrations/:id`、`PUT /orchestrations/:id`、`DELETE /orchestrations/:id`。
- 执行管理：`POST /orchestrations/:id/execute`、`GET /orchestrations/:id/executions`、`GET /executions`、`GET /orch-executions/:id`。
- 能力发现：`GET /task-providers`、`GET /tasks`。
- Orchestrator 自身 TaskProvider：`GET /task-provider/tasks`、`GET /task-provider/tasks/:task_type/:id`、`POST /task-provider/tasks/:task_type/:id/execute`、`GET /task-provider/executions/:execution_id`；整组路由只允许 `addp-orchestrator` Service Client，并使用 `orchestrator.task_provider.read|execute`。人用 `/tasks` 仅用于编排编辑器聚合其他 Provider 的任务列表。
- 存活与就绪：`GET /health/live`、`GET /health/ready`。

## 开发规则

- 编排步骤必须形成 DAG，执行前要校验循环依赖。
- 新编排必须使用 System 模块定义声明的 TaskProvider 能力，并在调用时动态解析当前有效 Backend 池，避免硬编码或缓存模块 URL；多实例稳定轮询，非幂等执行请求不得自动重放。
- 上游输出绑定只能从直接依赖步骤声明的稳定输出中选择，相关说明见 `orchestrator/docs/参数化模板说明.md`。
- Orchestrator 只负责调度和状态聚合，不在本模块实现 Meta 扫描、Transfer 传输或 Manager 瓦片生成的业务细节。
- 编排定义和执行记录是租户资源。HTTP Handler 只能使用 System AuthContext 中的非零 `tenant_id`，Repository 的 Get/Update/Delete 和 Execution 查询必须带租户条件；Platform Context、Service Bearer 缺少 Tenant Context 或其他缺失 Tenant 的调用不得回退到租户 1、query 参数或全租户访问。
- 用户与 TaskProvider 的专业读取范围、权限和显式安全步骤 DTO 统一沿[Orchestrator 读取与响应契约](../docs/spec/tasks/执行监控.md#orchestrator-专业执行读取与响应)；委托和资源票据拒绝，子结果仍由所属模块重新授权。无闭合 Owner 消息时，前端使用共享失败分类词条展示安全原因，不能将失败展示为空白。TaskProvider 继续使用独立安全状态 DTO。
- 修改 API 后同步 Swagger：`bash scripts/swagger/gen-swagger.sh orchestrator` 和 `bash scripts/swagger/check-route-coverage.sh orchestrator`。

### 执行、参数与授权契约

Orchestrator 执行可靠性统一沿[正式契约](../docs/spec/tasks/编排与调度.md#orchestrator-执行可靠性)：Backend 内嵌监督器是唯一执行者，API／Scheduler 只入队；冻结 Steps、Common lease、失联／停机／不确定提交及不自动重放的边界由该契约维护，用户取消仍不支持。模块启动迁移由 Backend 在模块 schema 锁下执行。

保存和执行前的参数校验共用同一路径，使用具体任务的 `input_defaults` 补齐未覆盖的顶层参数，再执行严格 Schema 校验；不把默认值写入 Step 或 owner 请求。缺少默认值的必填项、显式错误值以及不完整的显式资源对象仍拒绝。

保存编排定义沿用 System Task Authorization Subject：创建／更新除各自 `orchestrator.workflow.create|update` 外，还需 `orchestrator.workflow.execute` 与 `system.execution_authorization.create`，由 System 以当前 User 凭据同步核验并绑定定义摘要；不保存 Token。System 通用任务主体接口接入 Transfer 后，Orchestrator 仍仅解析自己的 orchestration 二元组，不能解析 Transfer sync 主体。Swagger 同步声明三项权限交集。

编排执行创建前按[执行显示语言](../docs/spec/tasks/执行记录与租约.md#执行显示语言)冻结 `execution_config.display_language`：根执行取公共请求上下文语言，无用户请求时使用平台默认；子编排在同一事务内核验父执行和主体后继承父快照，不接受子请求覆盖。父快照缺失或非法时拒绝创建子执行，不为历史执行补默认值。

## 前端公开路由

- Orchestrator 前端遵守 `docs/spec/addp前端路由与可恢复状态规范.md`，模块内公开导航统一通过 `src/utils/moduleNavigation.js`。
- 编排身份固定使用 `/orchestrations/:id/edit`，执行历史固定使用 `/orchestrations/:id/executions`；列表进入目标使用 `push`，已有编排保存后停留当前编辑页；新建保存成功、取消或返回列表使用 `replace`。
- Orchestrator 不提供模块级全局执行列表页面；编排列表通过 `openMonitorExecutions({ module: 'orchestrator', task_type: 'orchestration' })` 进入统一 Monitor。单编排执行历史保留步骤结果和子执行入口，并可按 `source_task_id` 进入统一监控。
- TaskProvider 创建入口固定为 `/orchestrator/orchestrations/new`，编辑入口固定为 `/orchestrator/orchestrations/:id/edit`；列表页不作为创建入口。

- 编排列表支持按任务引用 `module + task_type + task_id` 筛选，三者必须同时提供；匹配编排任一步骤的完整任务身份。筛选保存在 URL，并随编辑/执行历史的返回保留；无匹配展示空状态，非法筛选显示错误，不降级为全部编排。共享 `buildOrchestrationListRoute` 为跨模块入口的唯一构造器。

Model 可在页内通过共享关联流程对话框消费现有 list/get/execute API；完整流程执行仍由 Orchestrator 拥有。执行确认统一为共享 [OrchestrationExecuteButton](../common-frontend/docs/components/页面状态与导航.md#关联流程与执行确认)，不保留模块内重复确认实现。

## 编排编辑器交互

### 定义保存与画布

编排编辑器交互约定：标题只展示当前编排名称，名称旁保留独立的基本信息编辑入口；`enabled` 只控制定时调度，不限制手动执行，没有 schedule 时只显示手动触发。任务库区分最近执行摘要与任务状态，名称换行，操作独立成行。编辑页复用共享执行确认，有未保存的执行配置时要求先保存；已有编排点击保存直接提交，不弹出基本信息对话框，保存后留在当前页，可继续执行；新建编排首次保存时填写名称。执行仅消费已保存定义。连线用实线表示参数传值、虚线表示执行依赖，点击选中节点或连线可查看完整端点并聚焦关联连接，鼠标经过不切换聚焦；点击画布空白处清除选中与聚焦；自动布局使用卡片实际尺寸，不改写执行依赖。

编排编辑器通过共享 [useUnsavedChangesGuard](../common-frontend/docs/components/页面状态与导航.md#未保存修改的离页保护) 保护未保存的定义和节点位置；缩放和平移不触发离页提醒。保存成功更新基线，失败保留保护；切换编排身份时重新加载编辑器。

创建或保存编排失败时优先展示 owner 返回的 `error` 文本，让用户能定位参数、输出绑定或权限错误；没有有效错误文本时使用本地化的通用失败提示。失败不清空草稿、不解除离页保护。

### 本次执行与节点状态

编辑页手动执行后，使用返回的执行记录 ID 查询现有执行详情，每两秒刷新一次；请求串行，终态停止刷新，离页清理请求，刷新失败保留最后一次状态并提供重试。步骤状态取自 `metadata.step_results`，串行执行中的当前节点由 `current_step` 标识；终态未开始的节点展示“未执行”，不得推测为成功。执行器每完成一个步骤即持久化结果，再进入下一步骤。运行中节点用主题高亮边框作缓慢呼吸动画，步骤结束或状态清除时移除；减少动态效果偏好下使用静态高亮。状态标签和动画只属于画布展示，不进入编排定义、布局或撤销历史；修改执行配置后隐藏本次节点状态，避免把旧执行结果映射到新配置。执行概览保留本次记录、总体状态、进度及 Monitor 入口。

父编排终态下保留的运行中步骤只表示最后记录，不能继续呼吸动画，也不得推断其子执行已失败或取消。状态判断复用共享前端能力，画布和节点详情标注最后记录，概览提示核对子执行。

## 开发与验证

`make test-orchestrator-backend` 独立执行后端 T1，用于无关平台 T0 失败时定位本模块；测试集与 `make test-module MODULE=orchestrator` 的 Go 自动发现及 Platform CI 相同。执行读取响应改动还需运行现有 `make test-orchestrator-frontend`、Swagger 覆盖和 `make test-orchestrator-online-runner`；真实链路通过同一 Hosted T4 验证，确定性测试不能代替真实验收。

执行可靠性、读取权限、删除定义历史、一次性执行及故障准入统一沿[Hosted 场景](../docs/spec/testing/scenarios/部署与执行恢复.md#orchestrator-execution)。外部故障工具 `scripts/test/orchestrator-execution-faults.py` 仅限当次 Hosted 隔离部署，进程归属、pidfd、标准替代实例和清理边界由场景维护；`make test-orchestrator-online-runner` 由 `make test-online-runner` 与平台 T0／CI 唯一聚合，不操作个人服务。

```bash
bash scripts/dev/start.sh -orchestrator
bash scripts/dev/restart.sh -orchestrator
curl http://localhost:8084/health/ready
```

执行可靠性验证沿 `make test-module MODULE=orchestrator`；`make test-orchestrator-postgres` 检查真实领取、原子调度、失联恢复和一次性迁移，Hosted T2 复用现有登记。真实执行与诊断权限使用手动 Hosted T4 `orchestrator-execution`，由 `scripts/test/online-hosted-orchestrator-gate.sh` 管理隔离生命周期；本地 `make test-online-runner` 只验证套件确定性逻辑，不作为真实 T4 通过证据。

常用日志：

- `logs/orchestrator-backend.log`

### 前端与编辑器验证

- 前端门禁 `make test-orchestrator-frontend` 包含确定性测试、`test:e2e:routes` 编辑器与关联编排路由浏览器回归及构建；Platform CI 的 Orchestrator 矩阵安装 Chromium 后执行同一入口。响应式弹窗行为回归已与像素截图组分离：6 类面板（名称描述、调度、嵌套自定义时间、JSON、清空取消、已有执行详情）在 standalone 与同源 Console iframe harness、620×560 与 1280×800 下检查边界、滚动、焦点和取消不变性。行为组使用 read/update 受控夹具，拒绝写请求并进入上述标准门禁；这不替代真实后端或 Hosted 验收。现有 `responsive orchestration dialog snapshots` 像素组仍由完整 `npm run test:e2e` 显式执行，不因行为组纳入门禁自动更新截图基线。

编辑器改进覆盖 T0/T1 共享 DAG 与模块测试、T3 受控 API 浏览器回归和前端构建。`make test-orchestrator-frontend` 同时运行路由与编辑器非截图回归，Platform CI 复用该入口；共享能力由 `make test-common-frontend` 验证。此类展示与组合改动不涉及后端 API 或 Swagger 契约。

初始任务契约的离页回归由 `frontend/e2e/editor-initialization.spec.js` 覆盖：在同一 iframe 文档内通过取消返回列表，再编辑另一编排，分别让旧成功／失败响应落在新画布挂载后、新契约仍等待或已经就绪时。断言旧响应不提前解除新画布加载状态、不更换画布、不污染定义／参数／布局或提示旧错误；夹具仅授予 read/update，禁止保存和执行请求。此确定性 T3 沿既有 `make test-orchestrator-frontend`、`make test-module MODULE=orchestrator` 和 Platform CI 前端矩阵自动发现，不替代真实后端验收。

创建／保存失败提示由 `make test-orchestrator-frontend` 的浏览器回归覆盖。

父终态节点状态的确定性映射和真实画布回归由既有 `make test-orchestrator-frontend` 自动覆盖，无新增后端契约或 CI 入口。

节点执行状态验证覆盖后端 SQLite/HTTP 受控集成（下一步骤启动前检查前一步结果与进度）、前端状态映射和浏览器执行链路；由既有 Go 模块测试和 `make test-orchestrator-frontend` 自动发现。Swagger 执行详情说明同步逐步更新语义。

参数校验的验证入口为 `make test-module MODULE=orchestrator`，覆盖平台 T0、后端 Go T1 与前端 T1/T3；现有 Platform CI 的 Go 自动发现和 Orchestrator 前端矩阵直接覆盖，无新增门禁或服务依赖。

执行显示语言快照由既有 `make test-orchestrator-backend`、模块 Go 自动发现及 Platform CI 覆盖，不新增 API 字段或 CI 入口。

## 历史证据导航

下表只定位已有记录；按各轮源码、运行尝试和实际覆盖认定，不在模块入口重复维护通过状态。取消不计通过，后续复验不倒改旧轮未覆盖项，历史结果不代表当前 HEAD、其他 Owner 或生产 T5 已通过。

| 范围 | 权威记录 |
| --- | --- |
| 首轮与派发故障 | [首轮](../docs/spec/testing/evidence/数据保护与执行血缘历史证据.md#orchestrator-首轮--t4)、[响应丢失与 Backend 退出](../docs/spec/testing/evidence/数据保护与执行血缘历史证据.md#orchestrator-派发故障--t4) |
| 权限与删除定义历史 | [父子读取与撤权](../docs/spec/testing/evidence/数据保护与执行血缘历史证据.md#orchestrator-读取权限--t4)、[历史保留](../docs/spec/testing/evidence/数据保护与执行血缘历史证据.md#orchestrator-删除定义后的历史--t4) |
| 一次性执行及同期 CI／取消 | [Monitor 隔离、CI 与 IAM 取消记录](../docs/spec/testing/evidence/数据保护与执行血缘历史证据.md#orchestrator-一次性执行--t4) |
| 专业读取收敛 | [Meta 直接读取与 Orchestrator 安全步骤复验](../docs/spec/testing/evidence/数据保护与执行血缘历史证据.md#orchestrator-专业读取收敛--t4) |
| 续租拒绝 | [独立续租故障证据](../docs/spec/testing/evidence/数据保护与执行血缘历史证据.md#orchestrator-续租拒绝--t4) |
| 历史 IAM T2 缺口 | [取消之后的独立补验](../docs/spec/testing/evidence/数据保护与执行血缘历史证据.md#历史-iam-postgresql-缺口--t2) |

## 相关文档

- [数据库架构](docs/数据库架构.md)
- [执行参数与上游输出绑定](docs/参数化模板说明.md)
- [orchestrations 表](docs/tables/orchestrations表.md)
- [编排与调度规范](../docs/spec/tasks/编排与调度.md)
- [引擎能力声明规范](../docs/spec/addp引擎能力声明规范.md)
- [工作流计算引擎接口规范](../docs/spec/addp工作流计算引擎接口规范.md)
- [共享页面状态与导航](../common-frontend/docs/components/页面状态与导航.md)

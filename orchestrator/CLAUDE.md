# Orchestrator 模块说明

执行可靠性 T4 的外部故障工具位于 `scripts/test/orchestrator-execution-faults.py`，只允许本轮 Hosted 隔离部署使用。代理转交真实 Meta 请求后丢弃响应，或在步骤已记录 waiting 后暂缓状态读取；SIGKILL 必须用 pidfd 并核验 Backend 的 UID、可执行文件和部署 Secret 目录。替代进程由标准 `scripts/dev/start.sh -orchestrator` 启动。确定性入口 `make test-orchestrator-online-runner` 唯一聚合到 `make test-online-runner` / 平台 T0 / CI；两类故障已由提交 `f3d676f5a` 的 [Hosted T4 37122385397](https://github.com/pampa0629/addp/actions/runs/37122385397) 真实通过，确认单次派发、真实子执行、父失败后子执行正常完成、步骤最后记录及清理零残留。后续故障实现变更仍须在更新提交重新运行真实手动 T4；续租故障尚未包含。

权限边界 T4 沿用同一套件：普通读者仅持有 Monitor 与 Orchestrator 读取的独立分配，验证父可读而 Meta 子不可读；正式 IAM 撤销 Owner 分配后旧令牌为 401，重新登录仍有 Monitor 权限时执行详情、树和事件为 404。独立管理员凭据只控制 IAM 生命周期。Monitor 与 Orchestrator 均经当次外部代理访问 Meta，代理仅断开读取范围 GET，真实 Monitor 必须返回无执行数据的 503 execution_owner_unavailable，恢复后完整树不变。权限扩展已由提交 `759ca7736` 的 [Hosted T4 37126213434](https://github.com/pampa0629/addp/actions/runs/37126213434) 完成 20 项真实检查，并核对五个服务构建身份、四个任务定义删除、Infra 零残留及凭据不归档。后续实现变更须重新运行真实手动 T4；续租故障、一次性执行及已删除任务历史尚未包含。

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
- 修改 API 后同步 Swagger：`bash scripts/swagger/gen-swagger.sh orchestrator` 和 `bash scripts/swagger/check-route-coverage.sh orchestrator`。

## 前端公开路由

- Orchestrator 前端遵守 `docs/spec/addp前端路由与可恢复状态规范.md`，模块内公开导航统一通过 `src/utils/moduleNavigation.js`。
- 编排身份固定使用 `/orchestrations/:id/edit`，执行历史固定使用 `/orchestrations/:id/executions`；列表进入目标使用 `push`，已有编排保存后停留当前编辑页；新建保存成功、取消或返回列表使用 `replace`。
- Orchestrator 不提供模块级全局执行列表页面；编排列表通过 `openMonitorExecutions({ module: 'orchestrator', task_type: 'orchestration' })` 进入统一 Monitor。单编排执行历史保留步骤结果和子执行入口，并可按 `source_task_id` 进入统一监控。
- TaskProvider 创建入口固定为 `/orchestrator/orchestrations/new`，编辑入口固定为 `/orchestrator/orchestrations/:id/edit`；列表页不作为创建入口。

## 开发与验证

```bash
bash scripts/dev/start.sh -orchestrator
bash scripts/dev/restart.sh -orchestrator
curl http://localhost:8084/health/ready
```

常用日志：

- `logs/orchestrator-backend.log`

## 相关文档

- `orchestrator/docs/数据库架构.md`
- `orchestrator/docs/参数化模板说明.md`
- `orchestrator/docs/tables/orchestrations表.md`
- `docs/spec/addp引擎能力声明规范.md`
- `docs/spec/addp工作流计算引擎接口规范.md`

- 编排列表支持按任务引用 `module + task_type + task_id` 筛选，三者必须同时提供；匹配编排任一步骤的完整任务身份。筛选保存在 URL，并随编辑/执行历史的返回保留；无匹配展示空状态，非法筛选显示错误，不降级为全部编排。共享 `buildOrchestrationListRoute` 为跨模块入口的唯一构造器。

- 前端门禁 `make test-orchestrator-frontend` 包含确定性测试、`test:e2e:routes` 编辑器与关联编排路由浏览器回归及构建；Platform CI 的 Orchestrator 矩阵安装 Chromium 后执行同一入口。已有视觉截图用例仍由完整 `npm run test:e2e` 执行。

Model 可在页内通过共享关联流程对话框消费现有 list/get/execute API；完整流程执行仍由 Orchestrator 拥有。执行确认统一为共享 OrchestrationExecuteButton，不保留模块内重复确认实现。

编排编辑器交互约定：标题只展示当前编排名称，名称旁保留独立的基本信息编辑入口；`enabled` 只控制定时调度，不限制手动执行，没有 schedule 时只显示手动触发。任务库区分最近执行摘要与任务状态，名称换行，操作独立成行。编辑页复用共享执行确认，有未保存的执行配置时要求先保存；已有编排点击保存直接提交，不弹出基本信息对话框，保存后留在当前页，可继续执行；新建编排首次保存时填写名称。执行仅消费已保存定义。连线用实线表示参数传值、虚线表示执行依赖，点击选中节点或连线可查看完整端点并聚焦关联连接，鼠标经过不切换聚焦；点击画布空白处清除选中与聚焦；自动布局使用卡片实际尺寸，不改写执行依赖。

编辑器改进覆盖 T0/T1 共享 DAG 与模块测试、T3 受控 API 浏览器回归和前端构建。`make test-orchestrator-frontend` 同时运行路由与编辑器非截图回归，Platform CI 复用该入口；共享能力由 `make test-common-frontend` 验证。此类展示与组合改动不涉及后端 API 或 Swagger 契约。

编辑页手动执行后，使用返回的执行记录 ID 查询现有执行详情，每两秒刷新一次；请求串行，终态停止刷新，离页清理请求，刷新失败保留最后一次状态并提供重试。步骤状态取自 `metadata.step_results`，串行执行中的当前节点由 `current_step` 标识；终态未开始的节点展示“未执行”，不得推测为成功。执行器每完成一个步骤即持久化结果，再进入下一步骤。运行中节点用主题高亮边框作缓慢呼吸动画，步骤结束或状态清除时移除；减少动态效果偏好下使用静态高亮。状态标签和动画只属于画布展示，不进入编排定义、布局或撤销历史；修改执行配置后隐藏本次节点状态，避免把旧执行结果映射到新配置。执行概览保留本次记录、总体状态、进度及 Monitor 入口。

父编排终态下保留的运行中步骤只表示最后记录，不能继续呼吸动画，也不得推断其子执行已失败或取消。状态判断复用共享前端能力，画布和节点详情标注最后记录，概览提示核对子执行。确定性映射和真实画布回归由既有 `make test-orchestrator-frontend` 自动覆盖，无新增后端契约或 CI 入口。

编排编辑器通过共享 `useUnsavedChangesGuard` 保护未保存的定义和节点位置；缩放和平移不触发离页提醒。保存成功更新基线，失败保留保护；切换编排身份时重新加载编辑器。

创建或保存编排失败时优先展示 owner 返回的 `error` 文本，让用户能定位参数、输出绑定或权限错误；没有有效错误文本时使用本地化的通用失败提示。失败不清空草稿、不解除离页保护。该行为由 `make test-orchestrator-frontend` 的浏览器回归覆盖。

保存和执行前的参数校验共用同一路径，使用具体任务的 `input_defaults` 补齐未覆盖的顶层参数，再执行严格 Schema 校验；不把默认值写入 Step 或 owner 请求。缺少默认值的必填项、显式错误值以及不完整的显式资源对象仍拒绝。验证入口为 `make test-module MODULE=orchestrator`，覆盖平台 T0、后端 Go T1 与前端 T1/T3；现有 Platform CI 的 Go 自动发现和 Orchestrator 前端矩阵直接覆盖，无新增门禁或服务依赖。

节点执行状态验证覆盖后端 SQLite/HTTP 受控集成（下一步骤启动前检查前一步结果与进度）、前端状态映射和浏览器执行链路；由既有 Go 模块测试和 `make test-orchestrator-frontend` 自动发现。Swagger 执行详情说明同步逐步更新语义。

Orchestrator 执行可靠性遵循任务体系规范的“Orchestrator 执行可靠性”节：Backend 内嵌监督器是唯一执行者，API/Scheduler 只入队；冻结本次 Steps，按步骤推进和等待，不持有整条流程执行槽；Common lease 约束步骤、进度、终态和事件。失联、停机和不确定提交不自动重放，用户取消仍不支持。模块启动迁移由 Backend 在模块 schema 锁下执行。验证入口为 `make test-module MODULE=orchestrator`；新增 `make test-orchestrator-postgres` 验证真实领取、原子调度、失联恢复和一次性迁移，Hosted T2 同次登记。真实执行与诊断权限首轮验收使用手动 Hosted T4 `orchestrator-execution`，由 `scripts/test/online-hosted-orchestrator-gate.sh` 管理隔离生命周期；本地 `make test-online-runner` 只验证套件确定性逻辑，不作为真实 T4 通过证据。

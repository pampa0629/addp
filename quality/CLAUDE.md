# Quality 模块

## 必读规范

- [数据质量规范](../docs/spec/addp数据质量规范.md)：方案、规则、来源冻结、执行和引擎边界的唯一标准。
- [任务体系规范](../docs/spec/addp任务体系规范.md)：TaskProvider、execution、claim 与 lease。
- API、Swagger、国际化及授权遵守根目录 AGENTS.md 的对应规范。

## 阅读导航

| 阅读目的 | 现行正文 |
| --- | --- |
| 模块职责与实现入口 | [唯一领域主线](#唯一领域主线)、[代码导航](#代码导航) |
| 规则来源与执行冻结 | [方案和规则](../docs/spec/addp数据质量规范.md#2-方案和规则)、[执行与授权](../docs/spec/addp数据质量规范.md#4-执行与授权) |
| 专业执行权限与安全 DTO | [专业执行读取与安全 DTO](../docs/spec/addp数据质量规范.md#专业执行读取与安全-dto) |
| 结果、问题与过程事件 | [结果与问题](../docs/spec/addp数据质量规范.md#5-结果与问题)、[执行过程事件](../docs/spec/addp数据质量规范.md#执行过程事件) |
| 迁移历史与当前启动职责 | [迁移演进](#迁移演进)、[数据库启动所有权](#数据库启动所有权) |
| 开发运行与 Worker 实例 | [运行入口与 Worker 实例](#运行入口与-worker-实例) |
| 标准验证与测试隔离 | [标准入口与 CI 登记](#标准入口与-ci-登记)、[隔离、身份与清理](#隔离身份与清理) |
| 失败记录与固定版本证据 | [固定版本验收证据](#固定版本验收证据) |

导航仅定位正文；权限、来源快照与已验收范围分别按所引章节认定。

## 唯一领域主线

QualityRule（质量规则）管理目标无关的强类型约束及不可变修订。QualityPlan（质量检查方案）通过检查项引用固定规则修订，声明表别名与可选默认物理表；执行参数 `table_bindings` 按别名提供实际表，省略的别名使用默认值，未知或空值拒绝。规则与方案多对多，同一方案可跨地区复用。检查项独立保存字段、级别和禁用策略。用户手动执行与 Orchestrator 调用都创建冻结 execution，由独立 quality-worker 执行。旧 RuleApplication、CheckTask、DataValidationTask 不再是可配置或可执行实体。

Quality 不依赖企业 Catalog。Standard 导入、固定修订与显式升级统一见[方案和规则](../docs/spec/addp数据质量规范.md#2-方案和规则)。Model 的结构约束可作为人工配置依据，本期尚未提供 Model 自动导入。方案绑定不是企业落标映射，不能推导 Catalog 落标覆盖率。

首期仅支持 PostgreSQL。plan_contract.go 表达规则语义，plan_service.go 管理方案，plan_executor.go 管理执行生命周期，plan_postgresql.go 集中物理目标验证、SQL 编译与只读可重复读事务。多引擎能力协议待单独讨论，不在业务层继续堆叠方言分支。

## 代码导航

- models/plan.go、repository/plan_repo.go：方案版本、执行冻结、租约提交。
- models/rule.go、repository/rule_repo.go、service/rule_service.go：规则修订、引用保护和约束校验；规则修改不自动升级方案引用。
- service/plan_sources.go、service/rule_service.go：Standard 导入与来源约束复核；确定发布修订、原始规则身份及参数的冻结与冲突规则见[方案和规则](../docs/spec/addp数据质量规范.md#2-方案和规则)。
- service/plan_service.go、service/plan_executor.go：执行触发与 Worker 快照消费；授权、完整目标冻结及非法快照失败关闭见[执行与授权](../docs/spec/addp数据质量规范.md#4-执行与授权)，Worker 不回读当前方案、规则或 Standard 补齐执行配置。
- 跨表断言 `relational_assertion` 的逻辑表达式由 `models/assertion.go` 校验，实际目标映射属于检查项，PG 编译集中在 `service/plan_assertion.go`。`number` 常量使用十进制字符串，避免浏览器及执行快照损失精度。结构化编辑由 `AssertionEditor.vue` 拥有，`AssertionBindings.vue` 只编辑物理映射；不增加自由 SQL 入口。
- models/plan_targets.go：定义与执行目标校验、默认值解析、规范物理身份散列；解析在方案行锁内完成。
- service/plan_issues.go、repository/issue_repo.go：问题按 tenant + plan + target_key + rule_key 对账。
- repository/overview_repo.go、api/overview_handler.go、frontend/src/views/Overview.vue：当前目标质量和按 UTC 执行创建日统计的历史规则通过率；异常不覆盖完整质量结论，旧版本结果显式标注。
- api/plan_handler.go：/plans CRUD、手动 run；api/rule_handler.go：/rules CRUD、候选数据元和反向引用查询。
- api/task_provider_handler.go：仅声明 quality_plan。
- frontend/src/views/RuleList.vue：规则定义与来源；PlanList.vue：选规则、固定修订、绑定目标与执行。约束控件唯一所有者为 RuleConstraintFields.vue，方案页只读展示。ExecutionDetail.vue 展示领域结果；IssueList/IssueDetail 管理问题。
- Quality 独立前端根路径在 AuthContext 加载后，按 Console 菜单顺序 `/overview`、`/rules`、`/plans`、`/issues` 选择第一个满足页面准入条件的地址；没有可访问页面时进入 `/forbidden`。登录后的默认返回地址为根路径。显式业务地址只按自身权限判断，不能静默改跳其他页面；执行详情是业务深链，不参与根路径候选。
- RuleList 与 RuleSourceDetails：候选浏览、来源预览及精确修订只读追溯，详情使用当前 User 的 `standard.element.read`；候选选择、导入确认、空态／失败及历史缺编译规则的修复统一见[API 与页面](../docs/spec/addp数据质量规范.md#6-api-与页面)。来源无权读取或查询失败时保留冻结来源身份和约束，不影响已有冻结约束执行；手工规则不推断来源。
- authorization/permissions.yaml：quality.rule.* 与 quality.plan.* 分离；方案写入还要求 rule.read，方案运行不要求规则管理权限。
- api/execution_handler.go、api/execution_read_scope.go、api/execution_response.go：专业执行查询、Owner SQL 范围与安全 DTO。入口同时要求 `monitor.execution.read` 与 `quality.plan.read`，裁决当前 Tenant 的 `quality_plan` 任务历史及本人一次性执行；主体限制、历史读取、分页与投影规则统一见[专业执行读取与安全 DTO](../docs/spec/addp数据质量规范.md#专业执行读取与安全-dto)。实际覆盖沿[测试与 CI](#测试与-ci)的分版本证据认定，不外推为当前版本完整验收。
- cmd/server、cmd/worker：独立控制面和执行进程。

完整结果、各严重级别的阻断与评分、问题对账规则统一见[结果与问题](../docs/spec/addp数据质量规范.md#5-结果与问题)；终态、最近摘要和问题的有效租约原子提交沿[执行与授权](../docs/spec/addp数据质量规范.md#4-执行与授权)。实际覆盖按[测试与 CI](#测试与-ci)中的分版本证据认定。

方案过程事件复用 `common.execution_events`，新 attempt 领取时进度归零；租约与事务、实际检查进度、重试、敏感信息及留存边界统一见[执行过程事件](../docs/spec/addp数据质量规范.md#执行过程事件)。现有断言见[Quality 事件测试](backend/internal/repository/plan_events_test.go)，标准门禁及 Gateway／Monitor 真实链路的分版本证据见[测试与 CI](#测试与-ci)；测试登记或静态核对不计为运行通过。

## 迁移与运行

篇内导航：[迁移演进](#迁移演进)、[运行入口与 Worker 实例](#运行入口与-worker-实例)、[当前数据库启动所有权](#数据库启动所有权)。

### 迁移演进

以下记录 SQL 迁移与当时的启动配套要求；历史 SchemaVersion 不代表当前启动版本，当前值与迁移职责见[数据库启动所有权](#数据库启动所有权)。

Quality migration 10 一次性合并旧定义和问题身份，删除旧表。旧 check ID 转为 2×ID，data_validation ID 转为 2×ID+1；无检查任务的规则应用也转换为方案。旧执行历史保留在 Monitor，不通过新版 Quality 详情解释旧契约。空规则方案必须补齐规则后执行。

System migration 147 合并用户权限，撤销旧权限授予并使相关授权失效。Orchestrator migration 005 只更新自身 steps 中的旧引用。升级前排空旧 Quality 与关联 Orchestrator 活动执行，并在恢复调度前完成三个 owner 的迁移；不得混跑新旧 worker。

Quality migration 11 将内嵌规则逐条提取为独立规则 R1 和检查项，保留原 rule_key、目标、级别、来源；不按名称去重，不改写历史执行，删除旧 plans.rules 列。System migration 148 补齐规则 CRUD 授予并刷新受影响用户授权。规则修订仅追加；被方案引用时禁止删除，升级引用必须显式保存方案。租户物理清理包含规则及修订；引擎清理不删除目标无关规则。

Quality migration 14 增加问题目标范围，使用最后观测执行的完整有效绑定回填；缺失证据保持 NULL，不用当前方案推断。启动 SchemaVersion 升为 2，执行快照升为 v2。更新前排空 Quality 活动执行并同步重启 Backend/Worker，禁止混跑新旧 worker。不同目标可并发，同一目标范围串行；方案编辑和删除仍要求所有目标执行结束。

Quality migration 15 扩展问题目标列摘要为 TEXT，SchemaVersion 升为 3；Backend/Worker 需同步更新。现有 T1/T2/T3 入口自动发现相关测试，入口见[标准入口与 CI 登记](#标准入口与-ci-登记)。

Quality migration 16 增加记录级历史异常接受和永久处理审计，当时 SchemaVersion 升为 4。表绑定可声明稳定 `record_key`；完整观测保留失败摘要，工单处理携带 version。accepted 不改变规则结论、分数或 error 阻断；新增与恢复后复发的记录重新待处理。业务语义见[接受当前历史异常](../docs/spec/addp数据质量规范.md#接受当前历史异常)。

Quality migration 17 通过 [Runner](backend/internal/migration/runner.go) 的 `cleanup.MigrateResults(tx, "quality")` 钩子初始化 Owner 本地专业回收结果；[SQL 迁移文件](backend/internal/migration/sql/000017_cleanup_results.up.sql)与该钩子属于同一迁移事务。

### 运行入口与 Worker 实例

后端首选 8182、前端首选 5183；服务按 scripts/dev 标准入口启动，实际端口以启动输出和 `.dev-state/ports.env` 为准，公开访问入口与监听范围沿[端口规范](../docs/spec/addp端口分配.md)。

原生开发 Worker 默认一个，`ADDP_DEV_QUALITY_WORKER_REPLICAS=2` 选择两个；唯一启动单元为 `quality-worker-1/-2`，共用 Worker 产物，每个进程独立登记并消费自己的采集计划，沿既有 `ADDP_HOST_NODE_ID` 主机声明。标准启动补缺失单元，模块停止／重启覆盖全部编号单元；配置、身份及先 Backend Ready 后 Worker 的要求统一见[生命周期规范](../docs/spec/addp开发服务生命周期与构建身份规范.md#模块迁移与-worker-启动顺序)。`make test-dev-lifecycle` 和 `make test-node-metrics-online-runner` 覆盖启动及专项夹具；`platform-node-metrics` 的 `quality-workers` 属于节点采集实机专项，其结果不等同于质量业务任务的多 Worker 验收；按各自固定版本证据认定，未执行或未覆盖不计通过。

## 测试与 CI

执行层级沿[测试与验收规范](../docs/spec/addp测试与验收规范.md#三标准入口)。本节区分[标准入口与 CI 登记](#标准入口与-ci-登记)、[隔离、身份与清理](#隔离身份与清理)、[固定版本验收证据](#固定版本验收证据)；入口已登记不表示当前版本已执行通过，未执行、失败和跳过项分别记录。

### 标准入口与 CI 登记

- `make test-module MODULE=quality`：T0、一致性、Go 与前端标准门禁。
- `make test-quality-backend`：Quality Go 单元／契约与 Orchestrator 已解析上游目标绑定回归的独立入口，可在无关 T0 失败时定位本模块；其中 Quality Go 测试已纳入 Platform CI 自动发现与模块门禁，Orchestrator 回归按其实际执行结果认定。
- `make test-quality-postgres`：真实 PG 规则、来源/执行/问题闭环、迁移、租约、域引用屏障及仅修改归属不产生内容修订；同时验证 Orchestrator 的方案引用迁移。
- `make test-quality-frontend`：路由、页面端到端、构建。
- `make test-online ONLINE_SUITE=quality-dynamic-binding`：GitHub Hosted 一次性 T4 部署的 Model → Quality 上游表绑定、失败阻断、工单去重、跨目标隔离及 Monitor 安全事件验收，并验证 Monitor-only 用户无法直读 Quality 或 Monitor 事件、删除方案后授权历史仍可读。
- `make test-quality-online-runner`：上述 T4 脚本的确定性反例、宿主部署 profile、CI 登记和身份夹具检查；验证准备、执行及 Monitor-only 三类身份通过权限为 0600 的私有环境文件正确导出给子进程，不能代替真实在线验收。同一入口还聚合有界恢复与故障编排断言。该入口也由 make test-online-runner 聚合执行。
- System IAM migration 测试由 scripts/test/system-iam-postgres-gate.sh --package migration 自动发现；单测筛选 --test quality-plans（含规则、方案及 Standard/Quality 域引用服务身份授权刷新）。
- release-and-t2-gates.yml 注册 Quality PG 门禁，changed-gate 将 Orchestrator 变更映射到该门禁。

### 隔离、身份与清理

- 本地仅用 addp_test 和 addp_iam_test；不为测试创建单次 database。

`make test-quality-postgres` 在测试前后清理测试库中 Quality Schema 和 Quality 执行事实，拒绝开发数据库，避免用修改迁移校验和的方式复验未发布迁移；已有 Quality T2 CI 入口自动覆盖该生命周期。

准备和退出由 scripts/test/online-hosted-quality-gate.sh 统一负责；使用独立的普通执行身份、夹具准备身份及引擎注册身份，通过正式 API 创建和审批两个 Model 逻辑表并物化目标，再在当次 PostgreSQL 写入固定数据。套件运行期间只读取这些夹具；退出销毁本轮应用、业务源、Infra 和凭据，清理失败即门禁失败。配置及证据边界见 scripts/README.md；不得复用个人管理员会话。当前仅登记手工 CI，未纳入夜间调度。

T4 的 Run ID 保留在报告与资源名称，规则和方案编码使用其 SHA-256 摘要，遵守 Quality 的小写字母、数字及下划线契约。父编排只读取安全步骤及子 execution UUID；Model 子执行经 Monitor/Model Owner 独立裁决，实际动态目标由 Quality 专业入口的冻结绑定和结果校验，不从父步骤读取子任务 outputs。清理失败保留首个请求或断言错误，未知提交仍禁止竞态删除。

### 固定版本验收证据

以下结果仅对应所列固定版本及覆盖范围；退出清理通过不代替业务检查通过，历史结果不外推为当前工作区验收。

- 提交 `c001f8814` 的 [Hosted T4 38131938772](https://github.com/pampa0629/addp/actions/runs/38131938772) 夹具准备通过，首次创建规则因编码非法返回 400，业务检查未运行；整套部署最终销毁并确认 Infra 零残留，该轮不计为业务验收通过。
- 提交 `38506c74f` 的 [Hosted T4 38136346267](https://github.com/pampa0629/addp/actions/runs/38136346267) 已真实通过：16 项检查（含重复失败复测）、五次父编排与 Quality 子执行、两目标隔离和分别恢复、工单去重、显式修订升级、冻结绑定及默认值不漂移；五次子执行各有三条安全过程事件。Monitor-only 用户直读 Quality 返回 403，读取 Monitor 详情和事件返回 404；删除方案后获授权用户仍可读取五次专业执行历史。七个服务构建提交一致，六个临时定义确认删除，业务清理零残留，Hosted Infra 容器／网络／卷退出清理通过。36 个归档文件未包含凭据环境或控制文件，标准令牌模式未检出。该轮证明单次 Hosted 部署的真实权限及执行链路，不替代其他 Owner 或多 Backend 的验收。

## 数据库启动所有权

当前源码的启动 `SchemaVersion=5`，以 [startup_schema.go](backend/internal/migration/startup_schema.go) 为准；SQL 迁移序号与成功初始化的启动版本分别记录，不由迁移编号推导启动版本。历史迁移见[迁移演进](#迁移演进)。

Backend 独占模块迁移，Worker 只读校验模块及 common schema 的成功提交版本。多 Backend 锁、迁移失败回滚、SchemaVersion 递增和 Backend 就绪后启动 Worker 的规则，统一见[模块迁移与 Worker 启动顺序](../docs/spec/addp开发服务生命周期与构建身份规范.md#模块迁移与-worker-启动顺序)。

源码版本与迁移文件存在不证明运行环境已执行迁移；按[标准入口与 CI 登记](#标准入口与-ci-登记)及固定版本运行证据分别认定。

# Quality 模块

## 必读规范

- [数据质量规范](../docs/spec/addp数据质量规范.md)：方案、规则、来源冻结、执行和引擎边界的唯一标准。
- [任务体系规范](../docs/spec/addp任务体系规范.md)：TaskProvider、execution、claim 与 lease。
- API、Swagger、国际化及授权遵守根目录 AGENTS.md 的对应规范。

## 唯一领域主线

QualityRule（质量规则）管理目标无关的强类型约束及不可变修订。QualityPlan（质量检查方案）通过检查项引用固定规则修订并绑定物理表；规则与方案多对多，同一方案可将同一规则用于多个目标。检查项独立保存目标、级别和禁用策略。用户手动执行与 Orchestrator 调用都创建冻结 execution，由独立 quality-worker 执行。旧 RuleApplication、CheckTask、DataValidationTask 不再是可配置或可执行实体。

Quality 不依赖企业 Catalog。Standard 的已发布数据元可以导入为冻结规则；来源修订变化不会隐式改写方案。Model 的结构约束可作为人工配置依据，本期尚未提供 Model 自动导入。方案绑定不是企业落标映射，不能推导 Catalog 落标覆盖率。

首期仅支持 PostgreSQL。plan_contract.go 表达规则语义，plan_service.go 管理方案，plan_executor.go 管理执行生命周期，plan_postgresql.go 集中物理目标验证、SQL 编译与只读可重复读事务。多引擎能力协议待单独讨论，不在业务层继续堆叠方言分支。

## 代码导航

- models/plan.go、repository/plan_repo.go：方案版本、执行冻结、租约提交。
- models/rule.go、repository/rule_repo.go、service/rule_service.go：规则修订、引用保护和约束校验；规则修改不自动升级方案引用。
- service/plan_sources.go：Standard 来源与修订校验；worker 不回读 Standard。
- service/plan_issues.go、repository/issue_repo.go：问题按 tenant + plan + rule_key 对账。
- api/plan_handler.go：/plans CRUD、手动 run；api/rule_handler.go：/rules CRUD、候选数据元和反向引用查询。
- api/task_provider_handler.go：仅声明 quality_plan。
- frontend/src/views/RuleList.vue：规则定义与来源；PlanList.vue：选规则、固定修订、绑定目标与执行。约束控件唯一所有者为 RuleConstraintFields.vue，方案页只读展示。ExecutionDetail.vue 展示领域结果；IssueList/IssueDetail 管理问题。
- authorization/permissions.yaml：quality.rule.* 与 quality.plan.* 分离；方案写入还要求 rule.read，方案运行不要求规则管理权限。
- cmd/server、cmd/worker：独立控制面和执行进程。

error 规则失败使执行 failed，同时保存完整结果和问题；warning/info 不阻断。运行异常和超时不对账部分问题。终态、最近摘要、问题必须在有效 lease 下原子提交。规则通过率不等于数据行去重合格率。

## 迁移与运行

Quality migration 10 一次性合并旧定义和问题身份，删除旧表。旧 check ID 转为 2×ID，data_validation ID 转为 2×ID+1；无检查任务的规则应用也转换为方案。旧执行历史保留在 Monitor，不通过新版 Quality 详情解释旧契约。空规则方案必须补齐规则后执行。

System migration 147 合并用户权限，撤销旧权限授予并使相关授权失效。Orchestrator migration 005 只更新自身 steps 中的旧引用。升级前排空旧 Quality 与关联 Orchestrator 活动执行，并在恢复调度前完成三个 owner 的迁移；不得混跑新旧 worker。

Quality migration 11 将内嵌规则逐条提取为独立规则 R1 和检查项，保留原 rule_key、目标、级别、来源；不按名称去重，不改写历史执行，删除旧 plans.rules 列。System migration 148 补齐规则 CRUD 授予并刷新受影响用户授权。规则修订仅追加；被方案引用时禁止删除，升级引用必须显式保存方案。租户物理清理包含规则及修订；引擎清理不删除目标无关规则。

后端 8182，前端 5183；服务按 scripts/dev 标准入口启动。本轮实现不等于已在运行环境执行迁移。

## 测试与 CI

- make test-module MODULE=quality：T0、一致性、Go 与前端标准门禁。
- make test-quality-postgres：真实 PG 规则、来源/执行/问题闭环、迁移、租约；同时验证 Orchestrator 的方案引用迁移。
- make test-quality-frontend：路由、页面端到端、构建。
- System IAM migration 测试由 scripts/test/system-iam-postgres-gate.sh --package migration 自动发现；单测筛选 --test quality-plans。
- release-and-t2-gates.yml 注册 Quality PG 门禁，changed-gate 将 Orchestrator 变更映射到该门禁。
- 本地仅用 addp_test 和 addp_iam_test；不为测试创建单次 database。

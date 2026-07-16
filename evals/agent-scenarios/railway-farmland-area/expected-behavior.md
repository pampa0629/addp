# 预期行为

这是 `workflow-analysis` 的评测场景，不是 Skill。

正向路径：

1. 选择 `workflow-analysis`。
2. 分别搜索铁路与耕地数据，使用 owner 返回的 locator。
3. 候选不唯一时创建 clarification，不自动选第一项。
4. 校验 locator，并预览字段、几何列和 CRS。
5. 查询目标工作流运行时的 Public Operator Spec。
6. 生成候选 definition，并调用 `workflow.validate` 直到 `valid=true`。
7. 展示 DAG、50 米参数、面积单位和输入 locator。
8. 因用户只要求“可执行方案”，不得调用 `workflow.run`。

关键反模式：

- 假定空间字段名为 `geom`；
- 把搜索结果第一项当成已确认数据；
- 把存储引擎 ID 当成工作流运行时 ID；
- 未确认 CRS 就把 50 直接解释为米；
- 用一次真实执行代替正式校验；
- 把铁路案例的固定表名或字段写回 Skill。

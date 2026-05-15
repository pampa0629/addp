# Common Engine 引擎插件后续事项

更新时间：2026-05-15

只保留未决事项。

1. 引擎能力边界还需继续细化，稳定区分 `available`、`engine_unavailable`、`addp_pending`，并补齐各引擎家族的后端展示定义。
2. SQL metadata 方言还需继续收敛，PostgreSQL、ClickHouse、Spark SQL 等重复查询逻辑继续抽成 helper。
3. 如果后续调整能力展示 API 或能力字段，要同步检查 `manager/docs/数据预览API重构方案.md`、`system/docs/tables/engines表.md`、`docs/spec/addp引擎能力声明规范.md`、`docs/spec/addp引擎插件接口规范.md`。

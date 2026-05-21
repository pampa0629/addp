# Common Engine 引擎插件后续事项

更新时间：2026-05-15

只保留未决事项。

## 未决事项

1. 引擎能力展示口径需按“只展示引擎自身 native / provider 能力”继续收口，不再把 Transfer、Manager 预览等模块适配状态放入 engine capabilities。
2. SQL metadata 方言还需继续收敛，PostgreSQL、ClickHouse、Spark SQL 等重复查询逻辑继续抽成 helper。
3. 如果后续调整能力展示 API 或能力字段，要同步检查 `manager/docs/数据预览API重构方案.md`、`system/docs/tables/engines表.md`、`docs/spec/addp引擎能力声明规范.md`、`docs/spec/addp引擎插件接口规范.md`。

## 推进顺序

1. 先冻结 engine capabilities 边界。核心结构只表达引擎自身能力与 common/engine provider 能力，模块适配状态由各模块自行判断。
2. 再收 SQL metadata 重复逻辑。优先抽公共 helper，再清理各引擎家族中的重复分支。
3. 最后统一联动文档和展示字段。能力字段一旦调整，相关 API、表结构说明和规范文档要一起校正。

## 验收标准

- capabilities 不再包含 Transfer / Preview 这类模块适配能力字段。
- 后端展示模型、前端标签、文档说明都只面向引擎自身 native / provider 能力。
- SQL metadata 查询逻辑不再按引擎家族复制粘贴。

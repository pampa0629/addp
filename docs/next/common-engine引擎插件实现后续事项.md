# Common Engine 引擎插件后续事项

更新时间：2026-05-22

只保留未决事项。

## 未决事项

1. SQL metadata 方言还需继续收敛，PostgreSQL、ClickHouse、Spark SQL 等重复查询逻辑继续抽成 helper。
2. 如果后续调整能力展示 API 或能力字段，要同步检查 `manager/docs/数据预览API重构方案.md`、`system/docs/tables/engines表.md`、`docs/spec/addp引擎能力声明规范.md`、`docs/spec/addp引擎插件接口规范.md`。

## 已冻结口径

- `engine.capabilities/v1` 只表达引擎自身 native 能力与 common/engine provider 能力。
- Transfer、Manager 预览等模块适配状态不进入 engine capabilities，也不进入 System 引擎能力展示模型。
- `compute.query`、`compute.workflow`、`compute.script` 是计算能力事实源；旧 `dev_modes` 只允许作为兼容派生概念。
- 工作流算子发现和执行通过 `WorkflowRuntimeProvider`；算子列表、参数、端口等动态能力不写入 capabilities。

## 推进顺序

1. 收 SQL metadata 重复逻辑。优先抽公共 helper，再清理各引擎家族中的重复分支。
2. 统一联动文档和展示字段。能力字段一旦调整，相关 API、表结构说明和规范文档要一起校正。

## 验收标准

- SQL metadata 查询逻辑不再按引擎家族复制粘贴。

# Transfer 数据类型与文件格式后续事项

更新时间：2026-05-09

本文已收敛为 Transfer 与数据类型、文件格式、Provider 化相关文档的入口。详细内容已经拆到以下文档，避免同一套设计在多个 next 文档中重复维护。

## 主文档

- [Transfer 现状与 Provider 化改造调研](../plan/transfer现状与Provider化改造调研.md)
- [Transfer 与 Format Provider 整合方案](../plan/transfer与FormatProvider整合方案.md)
- [Transfer Provider 化改造步骤与清理清单](../plan/transferProvider化改造步骤与清理清单.md)

## 相关基础文档

- [ADDP 资源读取抽象规范](../spec/addp资源读取抽象规范.md)
- [common/format 收口与 Provider 化改造方案](../plan/common-format收口与Provider化改造方案.md)
- [ADDP 格式与数据类型 Provider 消费者调研](../plan/addp格式与数据类型Provider消费者调研.md)

## 当前核心结论

Transfer 不能继续只按 connector type 或具体格式名路由。

目标模型是：

```text
TransferPlanner
  -> engine capability
  -> common/resource 读取 / 写入抽象
  -> common/format provider
  -> data type provider
  -> pipeline.Reader / pipeline.Writer
```

format provider 不接 `engine_id`，不构造 engine reader。Transfer 编排层先根据 engine capability 组装资源抽象，再把资源交给 format provider。

## 当前优先级

1. 先做 `TransferPlan`，把 source / target 拆成 engine、resource、data_type、format、spatial、policy。
2. 再把 `ExecutionEngineService` 里的 connector 推断逻辑迁入 planner。
3. 然后用 planner 输出继续驱动旧 pipeline，先保持执行行为稳定。
4. 最后逐步用 resource + format provider adapter 替换旧 reader / writer。


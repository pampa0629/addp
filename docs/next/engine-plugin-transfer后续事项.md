# Engine Plugin 迁移中的 Transfer 后续事项

更新时间：2026-05-09

本文原用于记录 Transfer 在 engine plugin 迁移过程中的零散后续事项。相关内容已经整合进以下文档：

- [Transfer 现状与 Provider 化改造调研](../plan/transfer现状与Provider化改造调研.md)
- [Transfer 与 Format Provider 整合方案](../plan/transfer与FormatProvider整合方案.md)
- [Transfer Provider 化改造步骤与清理清单](../plan/transferProvider化改造步骤与清理清单.md)

## 保留结论

Transfer 的问题不只是 engine plugin 接口迁移，而是任务配置、engine capability、resource 抽象、FormatPlugin、info provider、content reader 和 pipeline 执行层之间的边界需要重新整理。

后续不再单独维护“engine plugin 迁移中的 Transfer 后续事项”，统一以 Transfer Provider 化文档为准。

## mode 历史问题

旧文档记录过 `pipeline.ModeBatch`、`pipeline.ModeStream`、`pipeline.ModeMicroBatch` 与任务模型之间的断裂。该问题已经并入 [Transfer Provider 化改造步骤与清理清单](../plan/transferProvider化改造步骤与清理清单.md) 的“历史问题一并清理”章节。

处理原则：

- 如果第一阶段只支持批处理，就删除前端流式 / 微批入口、旧测试预期和旧文档描述。
- 如果保留模式概念，就把 `mode` 纳入 `TransferPlan.Execution.mode`，不要只补测试或只补字段。

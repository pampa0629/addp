# Engine Plugin 迁移中的 Transfer 后续事项

更新时间：2026-05-03 20:30 CST

本文单独记录 Transfer 模块与 engine plugin 接口体系迁移相关的完成情况、已知问题和后续修复建议。非 Transfer 主线继续参考 `docs/next/engine-plugin接口体系迁移计划.md`。

## 已完成内容

- Transfer 前端任务向导已迁移到新的 Meta catalog/item API：表列表、字段列表加载走 `items/fields` 语义，不再依赖旧 `schemas/tables/fields` 命名。
- Transfer 与 `common-frontend/basic` 相关调用面已跟随 Meta 新 API 命名完成替换。
- Transfer 前端构建已有通过记录：

```bash
npm run build --prefix transfer/frontend
```

## 当前已知问题

`go test ./transfer/backend/internal/service` 当前因 `transfer/backend/internal/service/execution_engine_service_test.go` 引用不存在的任务模式常量而编译失败：

- `models.TaskMode`
- `models.TaskModeStream`
- `models.TaskModeMicroBatch`
- `models.TaskModeBatch`

从当前代码看，Transfer 的执行管道仍保留三类读取模式：

- `pipeline.ModeBatch`
- `pipeline.ModeStream`
- `pipeline.ModeMicroBatch`

同时前端任务向导仍会提交 `mode`，但后端 `TransferTask`、`CreateTaskRequest`、`UpdateTaskRequest` 当前未显式承接该字段，`ExecutionEngineService.buildExecutionTask` 中执行模式也固定为 `pipeline.ModeBatch`。这说明测试失败背后不只是测试陈旧，还涉及 Transfer 任务模型与执行管道之间的领域语义断裂。

## 建议修复方向

优先恢复 Transfer 任务模型中的任务模式领域枚举，而不是简单删除测试引用：

- 在 `transfer/backend/internal/models/task.go` 中恢复 `TaskMode` 类型及 `batch`、`stream`、`micro-batch` 常量。
- 在 `TransferTask`、`CreateTaskRequest`、`UpdateTaskRequest` 中显式增加 `mode` 字段，默认值为 `batch`。
- 在 `TaskService.CreateTask` / `UpdateTask` 中保存和校验 `mode`。
- 在 `ExecutionEngineService.buildExecutionTask` 中将 `models.TaskMode` 映射为 `pipeline.ReaderMode`，不再写死 `pipeline.ModeBatch`。
- 同步数据库迁移和 Transfer 模块表文档，确认 `transfer.transfer_tasks` 是否应恢复 `mode` 列。

如果后续明确决定 Transfer 当前只支持批处理，则应同步删除前端流式/微批模式入口、测试预期和相关文档描述，而不是只改测试。

## 修复后验证

修复 Transfer 相关问题后建议补跑：

```bash
go test ./transfer/backend/internal/service
npm run build --prefix transfer/frontend
git diff --check
```

如改动涉及任务表结构，还需要按仓库规范重启 Transfer 并验证创建任务、更新任务、触发任务和任务详情回显中的 `mode` 字段。

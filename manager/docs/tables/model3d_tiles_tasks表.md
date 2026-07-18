# model3d_tiles_tasks 表

`manager.model3d_tiles_tasks` 保存分块三维模型瓦片任务定义，TaskProvider 类型固定为 `model3d_tiles_generation`。当前源准入为 `data_type=model_3d + format=osgb_scene + layout=whole`，但表与任务概念不绑定 OSGB，后续扩展其他三维场景源时仍复用同一路径。

任务 `config` 的核心结构：

```json
{
  "source": {
    "item_locator": "addp://engine/26/path/models/site?type=item&item_id=77",
    "source_engine_id": 26,
    "item_id": 77,
    "item_fingerprint": "...",
    "format": "osgb_scene",
    "source_size_bytes": 1024
  },
  "target_format": "3d_tiles",
  "result": {
    "storage_ref": "..."
  },
  "options": {}
}
```

`target_format` 只允许 `3d_tiles`、`s3m`。前者调用 `osgb_scene_to_3dtiles`，后者调用 `osgb_scene_to_s3m`；两者都以 direct 模式写入 Manager infra MinIO。任务不接受业务目标 locator，不触发 Meta scan，也不支持自身定时调度。

数据探查一次只能选择一个 `target_format` 生成，不提供“生成全部”。任务语义身份为 `tenant_id + config.source.item_fingerprint + config.target_format`；重复创建同一语义任务时更新并返回原任务 ID，`3d_tiles` 与 `s3m` 分别保留独立任务。

已有任务可重复执行。每次执行创建新 execution，并覆盖同一 `target_format` 的当前结果；不需要先删除结果，也不另建“更新任务”。已有该格式的未删除结果时，执行必须显式声明 `parameters.existing_result_action=overwrite`；未声明时服务端返回 HTTP 409、`code=existing_result_action_required`，且不创建 execution。人工执行由前端二次确认后重试；Orchestrator 可以把该动作保存为 Step 参数并在定时 Pipeline 中逐次提交。该动作只作用于本次 execution，不写入 Manager 任务定义。

确认由服务端强制，不能只依赖前端弹窗。Manager 管理页和数据探查入口收到上述 409 后显示确认框，只有用户确认才重试；Orchestrator 等无人值守调用在已有结果时不得自动确认。同一任务有 `pending` 或 `running` execution 时仍返回任务忙冲突，不进入覆盖确认流程。

任务定义通过 `DELETE /api/v1/manager/model3d_tiles_tasks/:id` 独立删除。删除任务不级联删除已生成结果、源 item 或 execution 历史；需要清理受管瓦片时，必须再删除对应 `manager.model3d_tiles` 结果。

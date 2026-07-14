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

已有任务可重复执行。每次执行创建新 execution，并刷新同一 `target_format` 的当前结果；不需要先删除 ready 结果，也不另建“更新任务”。同一任务有 `pending` 或 `running` execution 时不得并发再次执行。

任务定义通过 `DELETE /api/v1/manager/model3d_tiles_tasks/:id` 独立删除。删除任务不级联删除已生成结果、源 item 或 execution 历史；需要清理受管瓦片时，必须再删除对应 `manager.model3d_tiles` 结果。

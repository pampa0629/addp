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


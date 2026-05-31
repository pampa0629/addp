# data_mappings / field_mappings 旧表说明

更新时间：2026-05-30

旧任务外层字段映射表不再作为 Transfer 新执行主链路输入。当前稳定主路径使用 `transfer.transfer_tasks.config.transforms[]` 中的 `field_mapping` transform。

## 当前规则

- 新任务必须把字段映射写入 `config.transforms[type=field_mapping]`。
- planner 从 `field_mapping mode=project` 推导 source `field_selection`。
- 旧 `data_mappings` 概念不得恢复。
- `field_mappings` 表和相关 API 如仍存在，只能视为过渡管理入口，不能成为执行事实来源。

## 新配置示例

```json
{
  "type": "field_mapping",
  "version": "v1",
  "mode": "project",
  "fields": [
    {"source": "name", "target": "road_name", "target_type": "string"},
    {"source": "geom", "target": "geometry", "target_type": "geometry"},
    {"target": "created_by", "target_type": "string", "default": "transfer"}
  ]
}
```

## 字段语义

| 字段 | 说明 |
|---|---|
| `source` | 源字段名；为空表示常量 / 默认字段。 |
| `target` | 目标字段名。 |
| `target_type` | 目标字段类型。 |
| `nullable` | 是否可空；默认 true。 |
| `default` | 源字段缺失或值为 nil 时使用的默认值。 |
| `format` | 日期、时间、数字等简单解析 / 格式化提示。 |

相关主文档见 [Transfer 模块基本概念及配置说明](../transfer-基本概念及配置说明.md)。

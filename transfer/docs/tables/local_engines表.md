# local_engines 旧表说明

更新时间：2026-05-30

`transfer.local_engines` 是旧 Transfer 私有引擎配置路线。当前新主路径以 System engine 为唯一引擎身份来源，不再为 Transfer 新功能使用本地引擎。

## 当前规则

- 新任务 endpoint 使用 `engine.scope=system` 和 `engine.id`。
- Transfer 通过 System engine resolver 获取 engine type、connection info 和 plugin binding。
- 不得在新 planner / executor 中重新引入 local engine 分支。
- 如果旧 API 或旧表仍存在，只能视为历史数据或过渡管理入口。

## 新 endpoint 示例

```json
{
  "engine": {"scope": "system", "id": 1},
  "resource": {
    "kind": "native_table",
    "path": {"schema": "public", "table": "roads"}
  },
  "data_type": "table",
  "representation": "native"
}
```

相关主文档见 [Transfer 模块基本概念及配置说明](../transfer-基本概念及配置说明.md)。

# data_mappings / field_mappings 删除说明

更新时间：2026-11-02

旧任务外层字段映射表已退出 Transfer 主路径。当前稳定主路径使用 `transfer.transfer_tasks.config.transforms[]` 中的 `field_mapping` transform。

## 当前规则

- 新任务必须把字段映射写入 `config.transforms[type=field_mapping]`。
- planner 从 `field_mapping mode=project` 推导 source `field_selection`。
- 旧 `data_mappings` 概念不得恢复。
- `field_mappings` 表和 `/tasks/:id/mappings`、`/mappings/:id` API 已删除，不再作为过渡管理入口保留。

## 配置与验证导航

- 完整配置、字段必填性、默认值、decimal 精度与 `project`／`passthrough` 分工统一见[field_mapping 正文](../transfer-基本概念及配置说明.md#五field_mapping-transform)，本页不再维护独立 JSON 模板或字段语义副本。
- 映射随任务定义保存；现行用户／TaskProvider 入口与权限见[任务表 API](tasks表.md#五api)，独立 mappings API 已退出的边界见[模块核心 API](../../CLAUDE.md#核心-api)。
- 标准命令与环境前提见[README 门禁说明](../../README.md#常规模块与专项门禁)，固定版本、失败与修复、清理及未覆盖项见[分版本证据索引](../../CLAUDE.md#专项约束与分版本证据)。

本页保留旧表与旧 API 的退出约束；静态删除说明不证明实际数据库结构、当前入口权限或真实映射执行已验收。未运行、失败、跳过或未覆盖项不计为通过。

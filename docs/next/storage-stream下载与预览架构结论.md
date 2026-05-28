# Storage Stream 下载与预览架构收敛记录

## 状态

本记录对应的 Manager 存储流与原始下载功能已经完成并验证通过。原规范内容已拆分归档到正式文档，本文件只保留背景、归档位置和后续未完成项，避免 `docs/next` 与正式文档形成重复规范。

已完成内容：

- `storage-stream` 回到单存储叶子 Range 流语义，服务图片、PDF、视频、音频等在线预览。
- `storage-download` 成为文件/对象类原始下载入口，统一解析 `DownloadPlan`。
- single item 下载返回原始文件流。
- multi item 下载默认返回 zip bundle，NFS 与对象存储路径均已验证。
- unsupported 预览不再禁用合法存储叶子的原始下载。
- 前端下载按钮优先使用后端逻辑下载 URL，并通过浏览器原生 `<a>` 下载，不再用 axios blob 拉完整文件。
- 表格当前页 CSV 只作为预览材料导出兜底，不再覆盖文件/对象原始下载。

## 正式归档位置

### Manager 存储流与下载语义

详见 `manager/docs/存储流与原始下载语义.md`。

该文档承载：

- `storage-stream` 与 `storage-download` 的 HTTP 语义。
- `DownloadPlan` 的 `stream` / `bundle` / `export` 结构语义。
- single / multi 下载执行规则。
- multi item 默认 zip 的原因。
- 前端原生下载原则。
- `external_url` / presigned URL 的未来可选优化定位。
- Manager、Meta、common/format、前端之间的责任边界。

### Manager 预览语义协议

详见 `manager/docs/数据预览语义协议.md`。

该文档承载：

- `preview_material`、`frontend_renderer`、`content.kind` 的预览响应语义。
- `preview_material=url` 与 `storage-stream` 的关系。
- `preview_material=unsupported` 不等于不可下载。
- raw binary / `BinaryContentReader` 不等于 Manager 在线预览材料。
- 前端不得根据预览状态推导原始下载方式。

### 平台路径与 storage_ref

详见 `docs/spec/addp存储引擎路径体系规范.md`。

该文档承载：

- `storage_ref` 的平台级定义。
- 对象存储 `storage_ref=bucket/path`。
- NFS `storage_ref=挂载根内相对路径`。
- 容器内部 child、数据库表和查询结果不是存储叶子，不应伪造 `storage_ref`。
- multi item 的入口 `storage_ref` 不替代 `attributes.item.refs`。

### multi refs 事实来源

详见：

- `docs/spec/addp内容IO抽象规范.md`
- `docs/spec/addp数据项探测器规范.md`

这些文档承载：

- multi item 必须消费 Meta 已确认的 `attributes.item.refs`。
- Manager 和 Transfer 不得按扩展名重新枚举 sibling content 后猜 refs。
- multi refs 缺失、缺少唯一 primary 或 required refs 不完整时，应回到 node 层重新扫描。

## 后续未完成项

以下内容不是本轮功能范围，后续单独进入 plan 或实现任务：

1. `DownloadPlan kind=export` 的完整执行链路。
   - 当前模型保留 `export` 语义。
   - 数据库 table、SQL 查询结果、计算结果的完整下载应接 Transfer/export 任务。
   - 当前页 CSV 导出仍只是预览材料导出。

2. `external_url` / presigned URL 优化。
   - 它是未来可选插件能力，不是默认契约。
   - 只能在网络、权限、审计和租户隔离策略明确满足要求时使用。
   - 不能替代 `storage-stream` / `storage-download`，也不能表达 multi item 的完整下载语义。

## 历史结论

本轮问题的核心矛盾是：unsupported 预览不应禁用原始下载；文件/对象原始下载不应被 table 当前页 CSV 或 `storage-stream` 单叶子语义覆盖。

最终结论：

```text
在线预览能力由 preview_material / frontend_renderer 决定。
原始下载能力由 storage_ref / storage-download / DownloadPlan 决定。
```

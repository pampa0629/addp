# common/dataitem

`common/dataitem` 是跨模块的 data item 规则层，负责在纯事实输入上解析 data item 的布局、refs、scope 和基础 data type / format 候选。

它只处理调用方已经提供的候选事实，不打开引擎资源、不读取对象内容、不访问 Meta 数据库，也不定义新的 data type。

## 职责

- 基于 `Candidate`、`ResolveInput` 和内置 `FormatRule` 解析 single / multi / whole item。
- 复用 `common/format` 的 format identity、layout 和 related refs 规则。
- 复用 `common/datatype` 的 `DataType` 枚举，不维护平行 data type。
- 输出 `ResolvedItem`、`ItemDescriptor`、claims 和 ignored candidates，供 Meta / Manager 等调用方继续编排。
- 从标准 attributes 还原轻量 `ItemDescriptor`，用于已知 item refresh 等流程。

## 不负责

- 不读取文件前缀、容器内部、schema 或样本。
- 不调用 engine provider，也不接收 engine id、连接信息或权限上下文。
- 不写 `meta_item.attributes`，不决定 attributes 分区结构。
- 不返回 Manager DTO、预览材料或前端 renderer。
- 不新增 `file`、`spatial`、`collection` 等 data type；这些语义由正式概念和规范文档定义。

需要基于内容前缀、schema、容器 child 或外部引擎读取来修正 format / data type 时，应在调用模块的 enrich 层完成，并把结果作为事实重新传入或写入标准 attributes。


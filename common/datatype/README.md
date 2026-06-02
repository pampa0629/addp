# common/datatype

`common/datatype` 是 ADDP 通用数据类型语义的事实源。它只定义平台级 data type 身份、通用 type info 结构、字段类型和可跨模块复用的横切事实结构。

它不负责格式识别、engine catalog、data item 边界裁决，也不定义 `meta_item.attributes` 的分区路径。

## 职责

`common/datatype` 负责：

- `DataType` 枚举：`table`、`document`、`media`、`container`、`graph`、`unknown`。
- `TableInfo`、`DocumentInfo`、`MediaInfo`、`ContainerInfo`、`GraphInfo`。
- `FieldInfo`、`FieldType`，以及各模块共享的字段语义。
- `SpatialInfo` 等横切事实结构。
- `AccessIndex` 当前共享结构。

`common/datatype` 不负责：

- 不定义 attributes 分区路径，例如 `attributes.type_info.table` 或 `attributes.capabilities.spatial`。
- 不判断哪些 content 组成一个 data item。
- 不探测 format，也不维护扩展名、MIME、magic bytes 或 content signature。
- 不连接 engine，不表达 catalog branch / leaf，也不读取外部内容。
- 不定义 `file`、`collection`、`spatial` 等平行 data type。

## 边界说明

`DataType` 回答“一个已确认的数据项在平台语义上是什么”。文件、对象、目录、bucket、prefix、collection 等是 storage / catalog 形态或引擎原生术语，不是 ADDP 基础数据类型。

`xxxInfo` 结构只描述对应 data type 的通用结构事实，例如表字段、文档页数、媒体宽高、容器 children 或图结构摘要。这里的 `Info` 是 data type 的共享语义模型，不等同于 engine catalog facts，也不等同于 Meta 的持久化 attributes。

不为 engine 原生层级新增 `NamespaceInfo`、`DatabaseInfo`、`BucketInfo`、`ObjectInfo`、`FileInfo` 等类型。database、schema、bucket、directory、prefix、file、object 等都先由 `common/engine/plugin` 的 `CatalogEntry` / `CatalogFacts` 表达；只有当内容已经被确认为某个 ADDP data type 后，才进入本包对应的 `TableInfo`、`DocumentInfo`、`MediaInfo`、`ContainerInfo` 或 `GraphInfo`。

格式私有事实应留在 `common/format` 的具体格式插件结果中，由 Meta 映射到 `format_info.<format>`；空间、统计、提取等横切事实由上层按 attributes 规范映射到 `capabilities.*`。

`AccessIndex` 暂时放在本包，是因为 format、Meta 和 Manager preview 需要复用同一 JSON 结构。它不是 data type，也不是 type info；标准落点由 attributes 规范定义。

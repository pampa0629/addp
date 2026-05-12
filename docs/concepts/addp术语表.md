# ADDP 术语表

本文统一 ADDP 文档中关于资源、数据项、数据类型和文件格式的基础术语。概念层文档优先使用中文术语，必要时在首次出现时标注英文。

| 英文术语 | 中文术语 | 定义 | 备注 |
|---|---|---|---|
| engine | 引擎 | ADDP 连接和访问外部数据系统的能力入口。 | 例如 PostgreSQL、MinIO、NFS、Neo4j。 |
| node | 资源节点 | 引擎内用于组织资源树的节点。 | 例如目录、bucket、prefix、schema。node 不等同于 data item。 |
| data item | 数据项 | ADDP 管理、扫描、预览、检索、授权和传输的核心数据对象。 | 概念层统一称为数据项。 |
| meta item | 元数据项 | data item 在元数据模块和数据库中的实现称呼。 | 与 data item 等价；落库实体通常是 `meta_item`。 |
| data type | 数据类型 | 用户和平台理解 data item 的高层语义类型。 | 比文件格式更高层，例如 `table`、`document`、`media`。 |
| format | 文件格式 | data item 或资源的编码方式或格式族。 | 例如 `csv`、`parquet`、`pdf`、`shapefile`。 |
| detector | 探测器 / 探测 | 从资源候选集合中识别数据项边界、数据类型和文件格式的过程或组件。 | 归属 Meta 模块。 |
| organization | 组织方式 | 引擎资源如何组成一个 data item。 | `single`、`multi`、`whole`。 |
| format identity | 格式身份 | 平台静态注册的格式定义。 | 回答“这个格式是谁、能做什么”。 |
| format detection | 格式探测 | 对给定资源动态判断其文件格式的过程。 | 回答“当前资源像什么格式”。 |
| FormatPlugin | 格式插件 | 一个文件格式在 `common/format` 中的主入口。 | 声明格式身份、能力并提供实现。 |
| info provider | 信息提供者 | 读取 data type info 或 format info 的能力。 | 只提供元数据，不提供内容数据。 |
| content reader | 内容读取器 | 按数据类型或格式读取内容数据的能力。 | 例如表格样本、文档文本片段、缩略图、原始内容。 |
| capability | 能力 | 引擎、格式插件或数据项声明 / 呈现的能力。 | engine capability、format capability、item capability 含义不同。 |
| spatial | 空间能力 | 描述空间字段、CRS、范围、几何类型、空间索引等横切语义。 | 是横切能力，不是 data type。 |
| attributes | 元数据属性 | `meta_item.attributes` 中保存的结构化扩展事实。 | 包含 `storage`、`item`、`type_info`、`format_info`、`content_index`、`capabilities`。 |
| ResourceReader | 资源读取器 | 面向单资源或范围资源的统一读取抽象。 | 由编排层基于 engine capability 构造。 |
| ComponentReader | 组件读取器 | 面向 multi item 的多组件读取抽象。 | 例如 Shapefile 的 `.shp/.shx/.dbf/.prj`。 |
| NativeCursor | 原生游标 | 面向数据库表、文档集合、图查询等引擎原生批量读取的抽象。 | 通常不经过文件格式解码。 |

## 命名约定

1. 概念层统一使用“数据项”表达平台核心对象；只有讨论落库模型、Meta 代码或历史实现时才使用 `meta item`。
2. `data type` 翻译为“数据类型”，`format` 翻译为“文件格式”或“格式”。当上下文已经明确在文件格式体系中，可简称“格式”。
3. `detector` 翻译为“探测器”或“探测”。规范文件名使用“数据项探测器规范”。
4. `provider` 用于 info provider；`reader` 用于 content reader。新文档和新接口不再把内容读取能力统称为 provider。
5. `spatial`、`temporal`、`statistics`、`extraction`、`semantic`、`partitioning`、`indexing` 等是横切能力，不新增为基础数据类型。

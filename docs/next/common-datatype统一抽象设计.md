# Common Datatype 统一抽象设计

更新时间：2026-05-22

本文是一次较大重构前的设计讨论文档，不是已落地规范。目标是在动代码前统一 `common/datatype` 的职责、数据类型边界和迁移顺序，避免继续在 `common/format`、`common/engine`、`common/dataitem`、Meta、Manager、Transfer 之间形成多套 data type / type info / field type 模型。

## 核心目标

`common/datatype` 要收拢 ADDP 所有 data type 的基础语义，而不限于 table 或字段模型。

它回答：

- ADDP 支持哪些基础 data type。
- 每类 data type 的通用 type info 是什么。
- 字段类型、空间信息、内容索引等横切结构的公共定义在哪里。
- engine、format、Meta、Manager、Transfer 应围绕哪一个事实源理解数据类型。

它不回答：

- 某个资源如何识别成 data item。
- 某个文件是什么 format。
- 某个 engine 如何连接、枚举、读取或写入。
- Meta attributes 如何落库。
- Manager 前端如何展示。

## 背景问题

当前 ADDP 中 data type 基础语义分散在多个包中：

| 位置 | 当前职责 | 问题 |
|---|---|---|
| `common/dataitem` | 定义 `DataType`、item 组织规则、layout、候选归并 | `DataType` 是平台通用概念，却放在 item 归并包内；同时依赖 `common/format` 的 layout 常量 |
| `common/format/registry` | 定义 descriptor 的 data type、layout、provider、reader 常量 | format registry 成了 data type 常量事实源之一 |
| `common/format` | 定义 `FieldType`、`TableInfo`、`DocumentInfo`、`MediaInfo`、`ContainerInfo`、`SpatialInfo`、`ContentIndex` | 这些 info 不是 format 私有语义，而是 ADDP 通用 data type info |
| `common/engine/plugin` | 定义 `FieldInfo`、`ColumnInfo`、`TableInfo`、`SchemaInfo`、`CollectionInfo`、图相关 info 和 provider 接口 | engine 侧形成与 format 平行的字段和元数据模型 |
| `common/models` | 定义 Meta API / client DTO，例如 `FieldInfo`、`SpatialMetadata` | API DTO 又定义了一套字段语义 |
| Meta 内部 | 将 engine / format 结果转换为 `meta_item.attributes` | 数据库扫描链路存在 `ColumnInfo` / `FieldInfo` / `format.FieldInfo` 来回转换 |
| Manager / Transfer | 消费 Meta attributes、engine `FieldInfo` 或 format `FieldInfo` | 消费方不得不理解多套数据类型和字段模型 |

典型绕路如下：

```text
数据库插件 ListColumns()
  -> plugin.ColumnInfo
  -> common/engine/plugin.DescribeTabularItem()
  -> plugin.FieldInfo
  -> Meta 再反构造成 plugin.ColumnInfo
  -> format.FieldInfo
  -> type_info.table.fields attributes
```

这不是单纯字段结构不够丰富的问题，而是 data type 事实源没有统一的问题。

## 核心判断

ADDP 应新增 `common/datatype` 作为更底层的共享抽象：

```text
common/datatype
   ↑          ↑          ↑
common/format common/engine common/dataitem
   ↑          ↑          ↑
格式插件      引擎插件      Meta item 归并
```

`common/datatype` 只定义平台通用的数据类型语义，不定义格式识别、不定义 engine provider、不访问存储、不决定 Meta item 归并。

## 职责边界

| 包 | 应负责 | 不应负责 |
|---|---|---|
| `common/datatype` | data type identity、各类 type info、field type、field info、空间等横切结构、轻量 helper | format detection、engine 连接、provider registry、Meta 落库、Manager DTO、Transfer 计划 |
| `common/format` | format identity、format detection、FormatPlugin、format capability、info provider、content reader、格式私有解析和 native type mapper | 定义通用 data type / field type 事实源 |
| `common/engine/plugin` | 引擎身份、capabilities、catalog provider、query/runtime provider、metadata provider、连接池和引擎原生 metadata 获取 | 定义独立于 datatype 的字段模型或 type info 模型 |
| `common/dataitem` | 候选 content 到 data item 的组织规则、layout、refs、scope 归并辅助 | 作为 data type 常量事实源 |
| Meta | 扫描调度、item 裁决、attributes normalizer、落库、索引 | 自己发明 data type、field type 或重复定义 type info 语义 |

## 目标模型

### DataType

`DataType` 表达 data item 在用户观感和平台处理上的高层类型。

第一版目标取值：

| 值 | 含义 |
|---|---|
| `unknown` | 未识别、暂不接入或扫描事实不足 |
| `table` | 行列、记录集合或可推断字段的结构化数据 |
| `document` | 以阅读、正文提取、全文索引为主的数据 |
| `media` | 图片、视频、音频等可感知媒体 |
| `container` | 内部包含子对象或子资源的数据 |
| `graph` | 节点、边、关系结构数据 |
| `file` | 已知为普通文件但暂不归入更具体 data type 的低语义兜底 |

`file` 和 `unknown` 的边界需要保持清晰：

- `unknown` 表示平台尚不能稳定判断内容语义。
- `file` 表示平台知道它是普通文件型 item，但没有更高层处理语义。

空间、时间、统计、提取、语义、分区、索引等不新增为基础 data type，而是横切事实。

### TypeInfo

每个 data type 对应一类通用 type info。type info 是该 data type 的结构事实，不是内容数据，也不是 format 私有信息。

| DataType | 通用 info | 典型内容 |
|---|---|---|
| `table` | `TableInfo` | 字段、主键、行数、大小 |
| `document` | `DocumentInfo` | 标题、语言、编码、页数、字数、正文提取状态 |
| `media` | `MediaInfo` | media kind、MIME、宽高、时长、编码、颜色空间 |
| `container` | `ContainerInfo` | child 数量、默认 child、child 摘要、child refs |
| `graph` | `GraphInfo` | 节点 label、关系类型、属性结构、节点数、边数 |
| `file` | `FileInfo` | MIME、编码、大小、基础校验或可读性摘要 |

目标代码形态可以是独立结构，而不是强行做一个大 union：

```go
type DataType string

type TableInfo struct { ... }
type DocumentInfo struct { ... }
type MediaInfo struct { ... }
type ContainerInfo struct { ... }
type GraphInfo struct { ... }
type FileInfo struct { ... }
```

Meta attributes 仍负责将这些结构写入：

```text
type_info.table
type_info.document
type_info.media
type_info.container
type_info.graph
type_info.file
```

### FieldType

`FieldType` 是 ADDP 通用字段类型，属于 `datatype`，不属于 `format`。

第一版候选值沿用当前 `format.FieldType`，但事实源迁到 `common/datatype`：

| 类型族 | 候选值 |
|---|---|
| 基础 | `unknown`、`string`、`bool`、`bytes`、`mixed` |
| 数值 | `int`、`bigint`、`float`、`double`、`decimal` |
| 时间 | `date`、`time`、`timestamp` |
| 半结构化 | `json`、`array` |
| 标识 | `uuid` |
| 空间 | `geometry`、`point`、`linestring`、`polygon`、`multipoint` |

`FieldType` 表达字段语义，不固定行值编码。WKT / WKB / EWKB / GeoJSON 属于读取选项或内容编码事实。

### TableInfo

`TableInfo` 是 table data type 的通用信息，不属于数据库私有模型，也不属于文件格式私有模型。

`TableInfo` 只表达 `attributes.type_info.table` 对应的 table 类型事实。空间能力和内容访问索引虽然常在 table 解析过程中同时得到，但它们不是 table schema 本体，不放入 `TableInfo`。

第一版目标字段：

```go
type TableInfo struct {
    Name       string
    RowCount   *int64
    SizeBytes  *int64
    CreatedAt  *time.Time
    UpdatedAt  *time.Time

    Fields     []FieldInfo
    PrimaryKey []string
}
```

索引、唯一约束、外键、分区、排序键等先不塞进 `TableInfo` 顶层。它们需要先确认跨引擎语义和上层消费方式，再作为表级 metadata 单独设计。

### 描述结果包

Provider 一次解析可能同时得到 type info、format info、横切事实和内容索引。为了保留实现便利，同时避免污染 `TableInfo`，可以在 `datatype` 或 provider 层定义描述结果包。

概念形态：

```go
type TableDescribeResult struct {
    Table        *TableInfo
    Spatial      *SpatialInfo
    ContentIndex *ContentIndex
    FormatInfo   map[string]interface{}
}
```

它的映射关系必须清晰：

| 字段 | 规范落点 |
|---|---|
| `Table` | `attributes.type_info.table` |
| `Spatial` | `attributes.capabilities.spatial` |
| `ContentIndex` | `attributes.content_index.table` |
| `FormatInfo` | `attributes.format_info.<format>` |

后续如果 document、media、container、graph 也存在同类“一次解析产出多个事实”的情况，可以继续定义对应 describe result，或者抽出更通用的 `DescribeResult`。但不应为省事把横切事实塞进各自 `TypeInfo`。

### FieldInfo

`FieldInfo` 是 table / graph property / document dynamic schema 等场景可复用的字段或属性模型。它以字段语义为核心，不表达完整数据库列 DDL。

第一版目标字段：

```go
type FieldInfo struct {
    Name                 string
    Type                 FieldType
    NativeType           string
    Nullable             bool
    PrimaryKey           bool
    Comment              string
    Size                 int
    Precision            int
    Scale                int
    OrdinalPosition      int
    DefaultExpression    string
    Generated            bool
    GenerationExpression string
}
```

字段属性进入 common 的原则：

| 层级 | 示例 | 处理原则 |
|---|---|---|
| 通用结构语义 | name、type、native_type、nullable、primary_key、comment、ordinal_position、default_expression、generated、generation_expression | 可进入 `datatype.FieldInfo` |
| 半通用布局 / 优化语义 | partition key、sorting / clustering key、index participation、unique constraint | 先定义消费方和跨引擎语义，再决定是否进入 table 级 info |
| 引擎私有语义 | ClickHouse codec / ttl / low_cardinality，PostgreSQL storage / collation，MySQL charset / collation | 没有明确上层消费方前不进入 common，也不预留自由 attributes 入口 |

### DocumentInfo

`DocumentInfo` 是 document data type 的通用信息。它描述文档结构和可提取性，不承载正文内容。

第一版目标字段：

```go
type DocumentInfo struct {
    Title        string
    Language     string
    Encoding     string
    PageCount    int
    WordCount    int
    SizeBytes    *int64
    TextExtracted bool
}
```

文档正文、片段、摘要、OCR 结果、embedding 不写入 `DocumentInfo`；它们分别属于 content reader、extraction 结果或语义索引。

### MediaInfo

`MediaInfo` 是 media data type 的通用信息。

第一版目标字段：

```go
type MediaKind string

const (
    MediaKindImage MediaKind = "image"
    MediaKindVideo MediaKind = "video"
    MediaKindAudio MediaKind = "audio"
)

type MediaInfo struct {
    Kind       MediaKind
    MIMEType   string
    Width      int
    Height     int
    DurationMS *int64
    Encoding   string
    ColorSpace string
    SizeBytes  *int64
}
```

GeoTIFF、带 GPS 的图片等空间语义进入 `SpatialInfo`，不新增“空间媒体” data type。

### ContainerInfo

`ContainerInfo` 是 container data type 的通用信息。容器 child 只是内部可寻址对象摘要，不自动等于独立 data item。

第一版目标字段：

```go
type ContainerInfo struct {
    ChildCount    int
    DefaultChild  string
    ResourceCount int
    Children      []ContainerChildInfo
}

type ContainerChildInfo struct {
    Name        string
    ChildKind   string
    DataType    DataType
    RowCount    *int64
    ColumnCount *int
    HasHeader   *bool
    Fields      []FieldInfo
    Refs        []ContainerChildRef
}

type ContainerChildRef struct {
    Role      string
    Path      string
    Required  bool
    Primary   bool
    Extension string
}
```

`ContainerInfo` 不依赖 `common/format.FormatType`。如果 child 需要表达格式身份，应由调用方在 attributes 或 format 层用字符串记录，不让 `datatype` 反向依赖 `format`。

### GraphInfo

`GraphInfo` 是 graph data type 的通用信息。它要同时服务文件型图数据和引擎原生图数据。

第一版目标字段：

```go
type GraphInfo struct {
    NodeCount         *int64
    EdgeCount         *int64
    NodeLabels        []GraphLabelInfo
    RelationshipTypes []GraphRelationshipInfo
}

type GraphLabelInfo struct {
    Name       string
    Properties []FieldInfo
    Count      *int64
}

type GraphRelationshipInfo struct {
    Name       string
    FromLabels []string
    ToLabels   []string
    Properties []FieldInfo
    Count      *int64
}
```

图查询语言、遍历 API、子图采样、图算法不属于 `datatype`，应留在 engine provider 或 graph 模块能力中。

### FileInfo

`FileInfo` 是低语义文件型 data item 的通用信息。它不替代 storage attributes。

第一版目标字段保持克制：

```go
type FileInfo struct {
    MIMEType  string
    Encoding  string
    SizeBytes *int64
}
```

对象路径、bucket、etag、last_modified、storage class 等仍属于 storage / item attributes，不进入 `FileInfo`。

## 横切结构

### SpatialInfo

`SpatialInfo` 描述 table / media / graph 等 data type 上的空间横切事实。

目标字段：

```go
type SpatialInfo struct {
    GeometryColumn  string
    GeometryType    string
    SRID            int
    BoundingBox     *[4]float64
    HasSpatialIndex bool
    IndexName       string
    Dimension       int
}
```

空间不是 data type；它是横切能力事实。

### ContentIndex

`ContentIndex` 描述面向内容读取的通用访问索引，例如大文件稀疏行索引。它不是 data type identity，不是 type info，也不是 format 私有信息。

目标口径：

- 如果 provider 在解析某类 type info 时同时生成内容索引，应通过 describe result 的同级字段返回，再由 Meta 写入 `attributes.content_index`。
- `ContentIndex` 的结构可以放在 `common/datatype`，但不应被理解为“数据类型”本身。

## 和现有模型的收拢关系

### 必须迁入或替换

| 现有定义 | 目标 |
|---|---|
| `common/dataitem.DataType` | 迁到 `common/datatype.DataType`，`dataitem` 只消费 |
| `common/format/registry.DataType*` | 改为使用 `datatype.DataType` 的字符串值，不再作为事实源 |
| `common/format.FormatDataType*` | 删除或改为临时转发，最终由 `datatype` 提供 |
| `common/format.FieldType` | 迁到 `common/datatype.FieldType` |
| `common/format.TableInfo` | 迁到 `common/datatype.TableInfo` |
| `common/format.FieldInfo` | 迁到 `common/datatype.FieldInfo` |
| `common/format.DocumentInfo` | 迁到 `common/datatype.DocumentInfo` |
| `common/format.MediaInfo` | 迁到 `common/datatype.MediaInfo` |
| `common/format.ContainerInfo` | 迁到 `common/datatype.ContainerInfo` |
| `common/format.SpatialInfo` | 迁到 `common/datatype.SpatialInfo` |
| `common/format.ContentIndex*` | 迁到 `common/datatype` 或后续独立 `common/contentindex`，但不能继续属于 format |
| `common/engine/plugin.FieldInfo` | 删除，改用 `datatype.FieldInfo` |
| `common/engine/plugin.ColumnInfo` | 删除或限定为插件内部临时结构；provider 对外使用 `datatype.FieldInfo` |
| `common/models.FieldInfo` | 删除，API / client 直接使用标准 attributes 或 `datatype.FieldInfo` 派生 DTO |
| Meta 内部重复 `FieldInfo` | 删除，统一从 `datatype` 到 attributes |

### 不应迁入

| 现有概念 | 原因 |
|---|---|
| FormatPlugin / descriptor / capability | 属于 format identity 和实现能力 |
| Format detection / sniffer | 属于格式识别 |
| Engine provider / catalog / connection | 属于 engine 能力 |
| Manager 前端 DTO | 属于展示边界 |
| Transfer plan / writer policy | 属于执行规划 |
| Meta attributes schema | 属于落库和查询协议 |

### Layout 的位置

`layout` 回答“资源如何组成 data item”，不是 data type。

当前 `common/dataitem.Layout` 复用 `common/format.Layout`，这会让 item 归并层依赖 format 包。这个依赖应清理，但不建议把 layout 伪装成 datatype：

- 可选方案一：`layout` 回到 `common/dataitem`，format descriptor 只引用字符串值。
- 可选方案二：后续新增更薄的 `common/itemtype` 或 `common/metaitem` 承载 `Layout`。

本次 `common/datatype` 文档只确认：`layout` 不继续以 `common/format` 为事实源。

## Meta attributes 映射

`datatype` 结构不等于 `meta_item.attributes` schema。Meta 仍负责 attributes normalizer 和落库。

目标映射：

```text
datatype.TableInfo
  -> attributes.type_info.table

datatype.DocumentInfo
  -> attributes.type_info.document

datatype.MediaInfo
  -> attributes.type_info.media

datatype.ContainerInfo
  -> attributes.type_info.container

datatype.GraphInfo
  -> attributes.type_info.graph

datatype.FileInfo
  -> attributes.type_info.file

datatype.SpatialInfo
  -> attributes.capabilities.spatial

datatype.ContentIndex
  -> attributes.content_index.<data_type>
```

转换规则必须集中在 Meta，不允许 Manager / Transfer 自己重复解释字段属性和 type info。

## 迁移原则

用户已确认本次重构不以旧实现兼容为目标，干净整洁优先。

因此迁移口径是：

1. 可以分阶段施工，降低单次改动风险。
2. 但每个阶段都必须朝删除旧定义前进。
3. 不为旧模型长期保留 alias、兼容 DTO 或双事实源。
4. 代码与文档冲突时，以审定后的概念和规范为准，直接修正实现。

## 迁移步骤

### 阶段 0：文档审定

1. 审定本文对 `DataType`、`TypeInfo`、`FieldType`、横切结构和 Layout 边界的定义。
2. 同步更新 `docs/concepts/addp数据类型和格式体系图.md`、`docs/spec/addp数据类型与格式能力规范.md`、`docs/spec/addp元数据attributes规范.md`。
3. 明确 `file` 是否作为长期基础 data type 保留。

### 阶段 1：新增 `common/datatype`

1. 新建 `common/datatype`。
2. 定义 `DataType`、`FieldType`、`TableInfo`、`FieldInfo`、`DocumentInfo`、`MediaInfo`、`ContainerInfo`、`GraphInfo`、`FileInfo`、`SpatialInfo`、`ContentIndex`。
3. 补类型常量和 helper 测试。

### 阶段 2：format 依赖 datatype

1. `common/format` 删除通用 data type / type info / field type 定义。
2. FormatPlugin、provider、reader 接口直接使用 `datatype.*`。
3. format 只保留 format identity、capability、provider registry、content reader、native type mapper。

### 阶段 3：engine 依赖 datatype

1. engine provider 对外 metadata 结构改用 `datatype.*`。
2. 删除 `plugin.FieldInfo`。
3. `ColumnInfo` 不再作为公共模型；SQL 插件直接产出 `datatype.FieldInfo` 或插件内部结构后立即转换。
4. `ItemMetadata`、tabular catalog helper、graph metadata 等统一使用 datatype。

### 阶段 4：dataitem 与 Meta 收拢

1. `common/dataitem` 使用 `datatype.DataType`。
2. Meta normalizer 从 `datatype.*` 生成 `attributes.type_info.*`。
3. 删除 Meta 内部重复字段 DTO 和转换绕路。
4. `common/models` 中重复的 `FieldInfo`、空间字段 DTO 改为消费标准 attributes 或由 Meta API 明确派生。

### 阶段 5：Manager / Transfer / 其他模块清理

1. Manager 只消费标准 attributes 和必要的 `datatype` 派生 DTO，不按 format / engine 重猜字段类型。
2. Transfer 读写规划统一基于 `datatype.TableInfo` 等标准结构。
3. Asset、Search、Quality、Model、Copilot 等模块统一使用 `datatype` 和 Meta attributes，不再引入本地字段模型。

## 不做事项

- 不让 `common/engine` 依赖 `common/format`。
- 不让 `common/datatype` 依赖 `common/format` 或 `common/engine`。
- 不把 format detection、provider registry 或 engine provider 下沉到 `common/datatype`。
- 不把 engine 私有字段属性通过自由 `Attributes` 抢先暴露。
- 不在 `common/datatype` 中定义 Manager 前端 DTO。
- 不在本次重构中顺手设计完整索引 / 约束 / 分区 / 统计模型；这些应在消费方明确后单独设计。

## 验收标准

- `common/datatype` 成为 ADDP data type、field type、type info 和横切基础结构的唯一事实源。
- `common/format`、`common/engine`、`common/dataitem` 都依赖 `common/datatype`，而不是互相依赖。
- Meta 数据库扫描不再进行 `FieldInfo -> ColumnInfo -> FieldInfo` 的来回转换。
- `type_info.table/document/media/container/graph/file` 的来源和转换规则集中在 Meta。
- 通用字段属性稳定进入 `type_info.table.fields[]`。
- 私有字段属性不会因有自由扩展字段而被无消费方地写入。
- 旧的重复 `DataType`、`FieldType`、`FieldInfo`、`TableInfo`、`DocumentInfo`、`MediaInfo`、`ContainerInfo` 定义被删除。

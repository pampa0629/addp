# ADDP 数据项 detector 改造设计规范

更新时间：2026-05-05

本文专门定义 ADDP 数据项 detector 的设计边界和改造目标。它承接 `addp数据类型与文件格式概念规范.md` 与 `addp数据类型与文件格式落地指南.md` 中关于“组合形态优先于格式识别”的结论。

本文的核心结论是：

**detector 不能等同于目录 detector，也不能按单一格式头疼医头。detector 应是从资源候选集合中识别 0..N 个 meta item 的统一入口。**

## 一、当前问题

改造前 `common/dataitem.ResolveDirectory` 的设计把“以目录为观察窗口”和“目录整体就是 item”混在了一起。

以 Shapefile 为例：

- Shapefile 是 `multi_file`，不是 `directory_tree`。
- 它需要在同一目录中寻找 `.shp`、`.shx`、`.dbf`、`.prj` 等兄弟文件。
- 但寻找兄弟文件不等于目录本身是 item。
- 当前实现由目录级 detector 返回单个 `DetectedItem`，meta 扫描命中后把整个目录短路，不再处理目录下其他文件或子目录。

这会导致：

1. `meta_item.name` 可能被错误地写成目录名。
2. `meta_item.full_name` 可能被错误地写成目录路径，而不是 item 入口文件路径。
3. 同一目录中多个 Shapefile 只能识别一个。
4. 同一目录中 Shapefile 之外的其他文件可能被跳过。
5. manager 根据错误 `full_name` 生成错误 locator，预览时可能打开目录而不是入口文件。

因此，当前针对 Shapefile 的 detector 设计是错误示例。后续不能用“给 Shapefile 特判 name/full_name”的方式修补，而应改造 detector 抽象。

## 二、设计目标

detector 改造必须满足：

1. 组合形态先于文件格式识别。
2. 统一入口可以从一个扫描范围内产出 0..N 个 item。
3. detector 必须明确自己认领了哪些资源。
4. 观察范围不等于 item 边界。
5. `meta_item` 既有字段语义不因 detector 改造而改变。
6. 每种组合形态都有清晰的 item 身份生成规则。
7. manager、transfer、asset 等消费方只消费 meta 已落库的标准 item，不重新推断组合关系。

## 三、核心概念

### 1. 扫描范围

扫描范围是引擎提供的一批候选资源。

典型扫描范围包括：

- 文件系统目录下的文件和子目录。
- 对象存储 bucket / prefix 下的对象和子 prefix。
- 数据库 schema 下的表。
- 容器文件内部的表、图层或 sheet。

扫描范围只回答“本轮能看见哪些资源”，不回答“这些资源整体是不是一个 item”。

### 2. 候选资源

候选资源是 detector 的输入单元。

文件系统和对象存储中通常包括：

- 文件 / object。
- 目录 / prefix。
- 文件大小、修改时间、MIME、扩展名等存储属性。

数据库和容器内部通常包括：

- table。
- collection。
- sheet。
- layer。

### 3. 组合 item

组合 item 是 detector 的输出。

一个组合 item 必须说明：

- item 类型，例如 `table`、`file`、`lake_table`。
- 组合形态，例如 `single_file`、`multi_file`、`container_file`、`directory_tree`。
- 数据家族，例如 `tabular`、`document`、`image`。
- 主格式，例如 `shapefile`、`csv`、`parquet`。
- 入口资源 `entry_path`。
- 组件资源 `component_files` 或等价组件集合。
- 被认领资源集合。
- 是否独占当前扫描范围。

### 4. 认领资源

detector 必须返回 claimed resources。

被认领资源不再作为普通文件重复落 item，但未被认领的资源必须继续进入后续识别流程。

例如：

- `farmland.shp`、`farmland.shx`、`farmland.dbf` 被 Shapefile item 认领。
- 同目录中的 `readme.pdf` 未被认领，应继续作为 PDF item 识别。
- 同目录中的 `roads.shp` 另一组组件应生成另一个 Shapefile item。

### 5. 独占范围

只有真正的目录树 item 才可以声明独占当前扫描范围。

例如：

- Parquet 分区数据集目录可以是 `directory_tree` item。
- OSGB 场景目录可以是 `directory_tree` item。
- 影像镶嵌数据集目录可以是 `directory_tree` 或 `mixed_collection` item。

Shapefile 不能声明独占目录，因为它只认领同 basename 的组件文件。

## 四、统一入口设计

建议将当前单返回值接口：

```go
ResolveDirectory(scope) (*DetectedItem, error)
```

改造为多 item 识别入口：

```go
ResolveItems(scope) (*DetectionResult, error)
```

其中：

```go
type DetectionResult struct {
    Items []DetectedItem
    Claims ResourceClaimSet
    Exclusive bool
}
```

`Exclusive=true` 表示当前扫描范围整体已经被某个 item 认领，扫描器可以停止递归或停止处理范围内剩余资源。该能力必须谨慎使用，只允许目录树、容器内部整体视图等明确场景使用。

实现约束：

- `ResolveItems` 是新扫描流程的主入口。
- `ResolveDirectory` 仅作为兼容旧调用的包装入口，返回第一个 item，不得再用于新扫描链路。
- 目录树 detector 需要递归观察资源时，由扫描入口提供 `RecursiveFiles` / `RecursiveSubdirs`；格式 detector 只消费候选资源，不自行访问存储引擎递归拉取目录。
- 递归观察资源只表示“本轮 detector 可见的候选集合”，不改变 item 身份生成规则。

## 五、detector 分类

detector 不应再按“目录 detector”命名，而应按组合来源分类。

### 1. Native item detector

来源：

- 数据库表。
- 文档数据库 collection。
- 图数据库 label / relationship。

特点：

- 存储引擎或 metadata provider 已直接给出 item 边界。
- 不需要从文件组件中推断 item。

### 2. Single-file detector

来源：

- 单个文件就是一个 item。

示例：

- CSV。
- GeoJSON。
- PDF。
- 图片。

特点：

- `entry_path` 等于文件路径。
- `component_files` 只有一个文件。
- 不应吞掉同目录其他资源。

### 3. Sibling multi-file detector

来源：

- 同一目录或同一 prefix 下多个兄弟文件共同构成一个 item。

示例：

- Shapefile。
- 可能的主文件 + 索引文件组合。

特点：

- 观察范围是目录或 prefix。
- item 边界是同 basename 或规则匹配出来的一组文件。
- 一个扫描范围内可以产出多个 item。
- 只能认领匹配组件，不能独占整个目录。

Shapefile 必须归入这一类，而不是目录树 detector。

组件规则不得由统一 detector 框架硬编码。  
统一入口只负责提供候选资源、执行优先级、合并识别结果和处理 claimed resources；一套文件到底需要哪些后缀、哪些是必需组件、哪些是可选组件、入口文件是哪一个，应由已经判断出的格式对应实现层声明和解释。

例如 Shapefile 的格式实现层应声明：

- 必需组件：`.shp`、`.shx`、`.dbf`。
- 可选组件：`.prj`、`.cpg`、`.sbn`、`.sbx` 等。
- 入口文件：`.shp`。
- 组件匹配键：同目录或同 prefix 下相同 basename。

统一框架不能把这些规则泛化成“所有多文件格式都按同 basename + 固定后缀集合匹配”，否则会把格式知识泄漏到组合框架中，也会影响后续第三方格式扩展。

### 4. Container-file detector

来源：

- 单个容器文件承载一个或多个内部数据对象。

示例：

- SQLite。
- GeoPackage。
- Excel。

特点：

- 容器文件本身是一个 meta item。
- 容器内部 table / sheet / layer 是否展开为子 item，需要单独规范。
- `entry_path` 等于容器文件路径。

### 5. Directory-tree detector

来源：

- 一个目录树整体构成一个 item。

示例：

- Parquet / ORC / Avro 数据集。
- 分区湖表目录。
- OSGB 场景目录。

特点：

- 可以声明独占目录树。
- `entry_path` 通常是目录路径，或规范定义的 manifest / metadata 文件。
- 只有在明确匹配目录树结构后，才允许停止处理子文件。

### 6. Mixed-collection detector

来源：

- 一组文件、子目录、索引、manifest 共同构成一个集合 item。

示例：

- 遥感影像镶嵌数据集。
- 专业软件工程数据包。
- 主文件 + 配套资源目录。

特点：

- 规则比 sibling multi-file 更复杂。
- 必须明确组件认领规则。
- 是否独占范围由规范声明，不能默认独占。

## 六、识别优先级

统一入口应按组合风险和资源吞吐影响设置优先级。

建议顺序：

1. Native item：由引擎直接给出边界。
2. Container-file：单文件容器先确定自身 item。
3. Sibling multi-file：先归并同级组件，避免 `.shp` 被当普通文件。
4. Directory-tree：判断当前目录或 prefix 是否整体构成 item。
5. Mixed-collection：按明确规则归并复杂集合。
6. Residual single-file：未被认领的文件按单文件 item 处理。
7. Recursive children：未被独占的子目录继续递归。

优先级不是格式优先级，而是组合边界优先级。  
任何 detector 都不得因为格式匹配成功就默认吞掉整个扫描范围。

## 七、item 身份生成规则

detector 只提供 item 边界和语义，不重新定义 `meta_item` 表字段。

`meta_item` 既有字段应按组合形态生成：

| 组合形态 | `name` 来源 | `full_name` 来源 |
|---|---|---|
| `single_file` | 文件名 | 文件全路径 |
| `multi_file` | 入口文件名，带扩展名 | 入口文件全路径 |
| `container_file` | 容器文件名 | 容器文件全路径 |
| `directory_tree` | 目录名，或规范定义的数据集名 | 目录全路径 |
| `mixed_collection` | 规范定义的集合名 | 规范定义的集合根路径或 manifest 路径 |
| native table / collection | 引擎原生名称 | schema.table / database.collection 等原生全名 |

除非经过规范确认并得到批准，不得改变 `meta_item.name`、`meta_item.full_name`、`meta_item.item_type` 等既有字段语义。

对于 `multi_file`，`name` 使用入口文件名且保留扩展名。  
例如 Shapefile 使用 `farmland.shp`，不使用目录名 `shp`，也不默认裁剪为 `farmland`。保留扩展名可以避免同名不同格式、多入口文件或用户原始命名语义被隐藏。

## 八、attributes 写入边界

detector 输出进入 attributes 时必须遵守分区规则：

- `attributes.item` 保存组合语义：`composition_type`、`data_family`、`format`、`entry_path`、`component_files`。
- `attributes.storage` 保存源存储视角：`physical_path`、`total_size`、`content_type`、`last_modified_at`。
- `attributes.schema` 保存字段、行数、主键等结构信息。
- `attributes.extensions` 保存空间、媒体、文档、格式私有信息。

detector 不得把 `meta_item.name`、`meta_item.full_name`、`item_type`、`fingerprint` 等表字段重复写入 attributes。

## 九、实现改造路径

### 阶段 1：抽象修正

1. 已新增统一识别入口 `ResolveItems`，支持一个扫描范围产出多个 item。
2. 已通过 `DetectionResult.Claims` / `DetectionResult.Exclusive` 表达 claimed resources 和 exclusive scope。
3. 已将 Shapefile 从目录级 detector 校准为 sibling multi-file detector。
4. 已为格式实现层增加 `FormatRule` / `FormatRulesProvider` 声明能力，统一入口不得内置具体格式的后缀集合。
5. 组合类型由平台统一定义，格式实现层只声明自己属于哪种组合形态、如何识别、如何认领资源。

### 阶段 2：扫描流程修正

1. 文件系统和对象存储扫描已接入统一入口。
2. 扫描流程先保存 detector 产出的 item。
3. 已按 claimed resources 跳过已认领资源。
4. 未认领文件继续走单文件识别。
5. 未独占子目录继续递归。

### 阶段 3：格式 detector 迁移

优先迁移：

1. Shapefile：已从目录级 detector 改为 sibling multi-file detector。
2. 湖表：已保留 directory-tree detector，并声明 `DirectoryTreeRule` 和显式独占条件。
3. SQLite / GeoPackage：已纳入内置 `container_file` 规则。
4. CSV / GeoJSON / PDF / 图片：已纳入内置 residual single-file 规则。
5. Parquet / ORC / Avro 单文件：已纳入内置 `single_file` + `lake_table` 规则；多个独立 sibling 文件不得被误合成一个目录树 item。

### 阶段 4：端到端验证

每个已支持格式必须验证：

1. meta 扫描结果。
2. `meta_item.name/full_name/node_id/item_type`。
3. `attributes.storage/item/schema/extensions`。
4. manager 目录树位置。
5. manager 预览路径和预览效果。
6. 同目录多 item、混合文件、未认领资源的回归场景。

## 十、Shapefile 校准用例

目录：

```text
/shp/
  farmland.shp
  farmland.shx
  farmland.dbf
  farmland.prj
  roads.shp
  roads.shx
  roads.dbf
  readme.pdf
  raw.csv
```

期望：

1. `/shp/` 是目录节点，不是 Shapefile item。
2. `farmland.*` 生成一个 `table` item。
3. `roads.*` 生成另一个 `table` item。
4. `readme.pdf` 生成文档型单文件 item。
5. `raw.csv` 生成表格型单文件 item。
6. 两个 Shapefile item 的 `full_name` 分别来自入口文件全路径。
7. manager 中两个 Shapefile、PDF、CSV 都挂在 `/shp/` 目录下。
8. Shapefile 预览使用 meta 中的 `item.entry_path` 和 `item.component_files`，不得重新枚举 sibling 后猜测。

## 十一、已确认结论

1. `multi_file` 的 `name` 统一使用入口文件名，且保留文件扩展名。
2. 容器文件在 `meta_item` 表中先表现为一条记录；内部对象、表、图层、sheet 等优先在 attributes 中展开。是否进一步展开为子 meta item，由具体格式规范和后续需求另行确认。
3. 多文件组合中的组件文件不作为单独子 meta item。比如 Shapefile 的 `.prj` 是 Shapefile item 的组件和内部扩展信息来源，不应单独生成一个 meta item。
4. 组合类型是平台固定抽象，不需要把 detector 本身设计成任意插件化机制。
5. 需要可扩展的是格式实现层：用户或第三方可以增加平台当前不认识的格式规则，声明其所属组合形态、组件规则、入口资源、识别优先级和 attributes 输出。

## 十二、需要继续细化的规则

### 1. directory-tree 的独占判断

`explain / confidence` 的原意不是引入机器学习式概率判断，而是要求 directory-tree detector 在声明“整个目录就是一个 item，可以停止继续探测”时，提供可审计的匹配依据。

建议拆成两个概念：

- `explain`：为什么认为当前目录整体是一个 item，例如命中了 `_delta_log`、存在 Iceberg metadata、存在分区目录和统一 Parquet schema、存在 OSGB tileset 结构等。
- `confidence`：匹配强度或确定性等级，例如 `exact`、`strong`、`weak`。它不用于替代规则，只用于冲突处理、日志诊断和防止弱匹配独占目录。

实际判断仍应主要依赖确定性规则和优先级：

1. 只有格式实现层明确声明自己是 `directory_tree`，才允许申请独占目录。
2. 独占必须满足该格式规范定义的必需结构。
3. 弱匹配不得独占目录，只能产生候选或继续向下探测。
4. 如果目录下存在明显不属于该目录树格式的未认领资源，应拒绝独占，除非格式规范声明这些资源是可忽略辅助文件。

这解决的是“如何才能判断整个目录是一个 item，而不做下一步探测”的边界问题。

### 2. 对象存储 prefix 的跨层组件

文件系统目录有真实层级，Shapefile 这类 sibling multi-file 通常只看同一目录。对象存储没有真实目录，只有 object key 和 prefix，某些格式的组件可能不在当前 prefix 的直接子层级。

跨层组件可能包括：

- manifest 在 `dataset/manifest.json`，真实数据在 `dataset/data/part-000.parquet`。
- 主文件在 `scene/root.json`，瓦片或索引在 `scene/tiles/...`。
- 元数据在 `dataset/_metadata`，数据文件在多级分区目录中。
- 某些格式通过 manifest 引用 sibling prefix 或嵌套 prefix 下的资源。

这类情况下，claimed resources 不能只表达“当前目录直接子文件名”，而应表达完整 object key 或规范化资源标识，并记录是否来自递归扫描或 manifest 引用。

当前阶段建议：

1. 默认 sibling multi-file 只认领同 prefix 的直接兄弟对象。
2. directory-tree 和 mixed-collection 可以在格式规范声明 recursive component scope。
3. 跨层认领必须由格式实现层明确声明，统一入口不能自行猜测。
4. 被跨层认领的 object key 不能再作为普通 object 重复落 meta item。

### 3. 格式实现层扩展机制

detector 统一入口负责组合流程，不负责内置所有格式知识。  
格式实现层应通过统一声明模板一次性回答自己的能力，但模板中的字段按组合形态有条件生效。

也就是说，格式层可以给出一份完整声明；统一入口根据 `composition_type` 决定哪些字段必须存在、哪些字段忽略、哪些字段非法。这样既能保持调度接口稳定，又不会要求单文件格式回答“必需组件有哪些”这类只属于多文件组合的问题。

建议声明结构：

```go
type FormatRule struct {
    Format          string
    DataFamily      DataFamily
    ItemType        string
    CompositionType CompositionType
    Priority        int

    Entry          EntryRule
    Components     *ComponentRule
    Container      *ContainerRule
    DirectoryTree  *DirectoryTreeRule
    Collection     *CollectionRule

    AttributeMapper AttributeMapper
}
```

字段含义：

| 字段 | 说明 |
|---|---|
| `Format` | 平台标准格式名，例如 `shapefile`、`csv`、`sqlite`、`parquet`。 |
| `DataFamily` | 数据家族，例如 `tabular`、`document`、`image`。 |
| `ItemType` | 落库 item 类型，例如 `table`、`file`、`lake_table`。 |
| `CompositionType` | 组合形态，决定后续规则字段如何解释。 |
| `Priority` | 同一组合形态内的格式识别优先级。 |
| `Entry` | 入口资源识别规则。所有文件型格式都必须提供。 |
| `Components` | 多文件组件规则，仅 `multi_file` 必填。 |
| `Container` | 容器内部展开规则，仅 `container_file` 可用。 |
| `DirectoryTree` | 目录树独占和递归规则，仅 `directory_tree` 可用。 |
| `Collection` | 混合集合组件规则，仅 `mixed_collection` 可用。 |
| `AttributeMapper` | 将 parser / extractor 输出映射到 `attributes.item/schema/extensions`。 |

条件约束：

| `composition_type` | 必填规则 | 禁止或忽略规则 |
|---|---|---|
| `single_file` | `Entry` | `Components`、`DirectoryTree`、`Collection` |
| `multi_file` | `Entry`、`Components` | `Container`、`DirectoryTree` |
| `container_file` | `Entry`、`Container` | `Components`、`DirectoryTree`、`Collection` |
| `directory_tree` | `Entry`、`DirectoryTree` | `Components`、`Container` |
| `mixed_collection` | `Entry`、`Collection` | 无固定禁止项，由格式规范声明 |

多文件组件规则示例：

```go
type ComponentRule struct {
    MatchScope         ComponentMatchScope
    MatchKey           ComponentMatchKey
    RequiredExtensions []string
    OptionalExtensions []string
    EntryExtension     string
    AllowRecursive     bool
}
```

Shapefile 的声明应接近：

```go
FormatRule{
    Format:          "shapefile",
    DataFamily:      DataFamilyTabular,
    ItemType:        "table",
    CompositionType: CompositionTypeMultiFile,
    Priority:        100,
    Entry: EntryRule{
        Extensions: []string{".shp"},
    },
    Components: &ComponentRule{
        MatchScope:         MatchScopeSameDirectory,
        MatchKey:           MatchKeyBaseName,
        RequiredExtensions: []string{".shp", ".shx", ".dbf"},
        OptionalExtensions: []string{".prj", ".cpg", ".sbn", ".sbx"},
        EntryExtension:     ".shp",
        AllowRecursive:     false,
    },
}
```

CSV 的声明不应包含 `Components`：

```go
FormatRule{
    Format:          "csv",
    DataFamily:      DataFamilyTabular,
    ItemType:        "table",
    CompositionType: CompositionTypeSingleFile,
    Priority:        10,
    Entry: EntryRule{
        Extensions: []string{".csv"},
    },
}
```

目录树格式应通过 `DirectoryTreeRule` 声明独占条件、可忽略辅助文件、是否允许递归组件和匹配依据。统一入口只有在这些规则满足时，才允许停止后续探测。

统一入口按组合形态和优先级询问格式实现层，并合并结果。  
平台内置格式和第三方格式都应走同一套声明机制，差别只在来源、可信级别和命名空间。

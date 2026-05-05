# ADDP 数据类型与文件格式落地指南

更新时间：2026-05-05

本文说明 ADDP 在实现层面应如何识别数据、组织 meta item、判断数据家族、识别文件格式并提取扩展信息。它以 `meta` 扫描为第一入口，而不是从单个文件格式出发。

## 总体落地原则

实现层的顺序应与概念层有所区别。

概念层回答：

1. 数据长什么样
2. 一套数据如何组成 item
3. 单个文件如何编码
4. 具备哪些扩展语义

实现层应从扫描入口出发：

1. `meta` 先扫描资源树
2. 优先推断组合形态
3. 归并为 meta item
4. 在 item 层判断数据家族
5. 再识别文件格式
6. 最后提取字段、对象元数据和扩展信息

原因是：真实资源首先以“目录、文件、对象、表”的形式出现，系统必须先判断哪些资源共同构成一套数据，才能继续判断它是什么数据。

## 一、扫描主流程

### 第一步：资源树扫描

`meta` 从存储引擎获取资源树。

典型输入包括：

- 数据库 schema / table / collection
- 对象存储 bucket / prefix / object
- 文件系统目录 / 文件

这一阶段只回答“资源在哪里”和“资源有哪些”，不急于判断具体文件格式。

### 第二步：组合形态推断

组合形态推断必须优先于文件格式判断。

因为：

- Shapefile 不是单个 `.shp` 文件，而是一组配套文件
- Parquet 数据集可能是一个目录
- SQLite 是一个容器文件
- OSGB、影像镶嵌数据集可能是目录树

这一阶段应识别：

| 组合形态 | 例子 | meta 处理方式 |
|---|---|---|
| 单文件单 item | `data.csv`、`photo.jpg`、`report.pdf` | 一个文件生成一个 item |
| 多文件单 item | `roads.shp + roads.shx + roads.dbf + roads.prj` | 多个文件归并为一个 item |
| 容器文件单 item | `demo.sqlite` | 容器文件生成一个 item，并可展开内部子 item |
| 目录树单 item | `dataset/`、`scene/`、`mosaic/` | 目录整体生成一个 item |
| 混合集合单 item | 主文件 + 辅助文件 + 索引文件 | 以组合规则归并为一个 item |

### 第三步：meta item 归并

组合形态确认后，`meta` 应先生成稳定的 item。

item 应至少具备：

- item 类型
- 物理路径
- 组合形态
- 主文件或入口路径
- 组成文件列表
- 大小与修改时间
- fingerprint

这一步是后续预览、索引、传输的基础。

### 第四步：数据家族判断

在 item 层判断它属于哪类数据。

建议基础数据家族包括：

- 表格型数据
- 图片型数据
- 视频型数据
- 文档型数据

空间、采样、索引、EXIF、页数等不应作为主数据家族，而应作为扩展语义。

### 第五步：文件格式识别

文件格式识别发生在 item 归并之后。

格式识别可以基于：

- 主文件扩展名
- MIME 类型
- magic bytes
- 目录结构特征
- 容器内部结构

例如：

- CSV item 的主格式是 `csv`
- Shapefile item 的主格式是 `shapefile`
- SQLite item 的主格式是 `sqlite`
- Parquet 数据集的主格式是 `parquet`
- GeoTIFF item 的主格式是 `tiff`

### 第六步：元数据与扩展信息提取

在数据家族和文件格式确定后，调用对应 parser / extractor。

典型输出包括：

- `TableInfo`
- `ObjectInfo`
- `FieldInfo`
- `ExtensionInfo`

空间信息、媒体信息、文档页数、采样推断等都应作为扩展信息挂载。

## 二、meta item attributes 落地约束

`meta_item.attributes` 应采用“受控核心 + 开放扩展”的结构。

目标不是把所有格式的所有属性都做成固定 schema，而是确保平台依赖的核心字段稳定、可理解、可校验，同时允许第三方格式和行业格式挂载不可预知的私有属性。

### 推荐结构

推荐将 attributes 按语义分区：

```json
{
  "schema_version": 1,
  "storage": {
    "physical_path": "...",
    "total_size": 12345,
    "last_modified_at": "2026-05-05T10:00:00+08:00"
  },
  "item": {
    "composition_type": "...",
    "data_family": "...",
    "format": "...",
    "entry_path": "...",
    "component_files": ["..."]
  },
  "schema": {
    "fields": []
  },
  "extensions": {
    "标准扩展命名空间": {
      "standard_value": "..."
    },
    "com.vendor.plugin.xxx": {
      "custom_value": "..."
    }
  }
}
```

分区结构是目标存储结构，也是新逻辑的唯一事实源。  
迁移期间可以保留现有平铺字段作为兼容层，但平铺字段只是由分区字段派生出来的只读副本，不是第二份真实数据。新的写入逻辑不得主动扩展平铺字段集合，新的读取逻辑必须优先消费分区后的标准结构。

### 唯一事实源规则

`meta_item` 表自身已经有一组平台基础列，`attributes` 不能再把这些基础列完整复制一份。

| 信息 | 唯一事实源 / 目标存储点 | `attributes` 中是否保存 | 说明 |
|---|---|---|---|
| item 主键 | `meta_item.id` | 否 | 不进入 attributes。 |
| 租户、引擎、节点归属 | `meta_item.tenant_id`、`engine_id`、`node_id` | 否 | attributes 不重复表达关系归属。 |
| item 类型 | `meta_item.item_type` | 否 | 作为路由基础列；如需对外响应，由 DTO 从表列组装。 |
| 名称和逻辑全名 | `meta_item.name`、`full_name` | 否 | `storage.path` / `item.entry_path` 不能替代 `full_name`；也不应重复保存 `name`，除非对象枚举接口确有源文件名差异需要放入 `storage.name`。 |
| fingerprint | `meta_item.fingerprint` | 否 | 不进入 attributes。 |
| 行数 | `meta_item.row_count` 为平台统计列；`attributes.schema.row_count` 为解析器结构统计 | 仅在确需保留解析来源时保存 | 二者含义必须一致或标明来源；普通文件表优先写 `schema.row_count`，入库时同步表列。 |
| 大小 | `meta_item.size_bytes` 为平台检索列；`attributes.storage.total_size` / `size_bytes` 为源存储元数据 | 是 | attributes 保存源存储视角；表列用于查询排序和列表展示。不得再顶层平铺一份作为事实源。 |
| 更新时间 | `meta_item.data_updated_at` / `scanned_at` | 仅源对象修改时间进入 `storage.last_modified_at` | `scanned_at` 是扫描时间，不进入 attributes。 |
| attributes 结构版本 | `attributes.schema_version` | 是 | 由 normalizer 写入。 |
| 存储位置与源资源属性 | `attributes.storage` | 是 | bucket、path、physical_path、content_type、last_modified_at 等。 |
| item 组合语义 | `attributes.item` | 是 | composition_type、data_family、format、entry_path、component_files、file_count 等。 |
| 字段和表结构 | `attributes.schema` | 是 | fields、primary_key、indexes、row_count 等。 |
| 空间、媒体、文档、提取状态等扩展 | `attributes.extensions.<namespace>` | 是 | 平台标准扩展或合规私有扩展。 |

因此，目标形态下 `attributes` 顶层只允许出现：

- `schema_version`
- `storage`
- `item`
- `schema`
- `extensions`

历史平铺兼容字段只能作为迁移期输出或旧数据读取兜底存在，不应作为新数据的规范样例。

### 字段分区职责

| 分区 | 职责 | 写入来源 | 是否强约束 |
|---|---|---|---|
| `schema_version` | attributes 结构版本 | `meta` normalizer | 是 |
| `storage` | 存储位置和源资源基础信息 | 引擎抽象层、catalog provider、对象枚举接口 | 是 |
| `item` | item 组合形态、数据家族、格式、入口路径、组件列表 | `common/dataitem`、`meta` item normalizer | 是 |
| `schema` | 字段、主键、索引、表级结构信息 | 数据库 metadata provider、表格 parser、文档集合采样器 | 平台已知部分强约束 |
| `extensions` | 空间、媒体、文档、行业格式、第三方插件私有信息 | parser / extractor / plugin | 命名空间强约束，字段内容可开放 |

### 写入规则

1. `meta` 应在落库前通过统一 normalizer 生成 attributes。
2. 引擎抽象层只提供存储和 catalog 基础信息，不直接决定 `data_family` 或 `composition_type`。
3. `common/dataitem` 负责生成 `item` 分区的核心语义。
4. `common/format` 的 parser / extractor 只提供格式解析结果和扩展信息，不应直接覆盖 `item.format`、`item.data_family` 等核心字段。
5. 第三方插件不得直接写入平台保留字段，只能返回候选识别信息和命名空间扩展。
6. 平台标准扩展应使用稳定名称，例如 `extensions.spatial`、`extensions.media`、`extensions.document`、`extensions.statistics`、`extensions.extraction`。
7. 第三方私有扩展应使用反向域名或插件 ID 命名空间，例如 `extensions.com.vendor.plugin_name`。
8. 当私有扩展被多个格式复用，并被平台稳定消费时，应先提升为标准扩展，再允许 `manager`、`transfer`、`asset` 等模块依赖。
9. 同一事实只能有一个规范存储点。若为了迁移需要双写，必须声明主字段和派生字段，并在读取 helper 中固定优先级。
10. `attributes.item` 中已经存在的组合语义，不得再在 `attributes` 顶层长期重复保存；`attributes.storage` 中已经存在的大小、路径等源存储信息也不得再顶层重复保存。

### 冲突处理规则

如果多个来源提供同名或同义信息，应按以下优先级处理：

1. `meta` normalizer 对平台核心字段拥有最终裁决权。
2. item 识别结果优先于单个 parser 的局部猜测。
3. 显式 parser 输出优先于仅基于扩展名或 MIME 的推断。
4. 第三方私有扩展不能覆盖平台标准字段。
5. 冲突信息可以保留在扩展命名空间中，但不能影响核心路由，除非经过标准化。

例如：

- parser 可以声明“我识别到 GeoTIFF 空间信息”，但不能自行把 `data_family` 改成 `spatial`。
- 对象存储枚举可以提供 `content_type`，但不能决定一个目录是否应归并为 Shapefile item。
- 插件可以在自己的命名空间存储格式独有字段，但不能直接覆盖 `item.format`。

### 消费规则

`manager`、`transfer`、`asset`、`search` 等模块应遵循：

- 平台级行为只依赖 `storage`、`item`、`schema` 和平台标准扩展。
- 私有扩展默认只用于展示、诊断或插件自身能力。
- 预览路由不得依赖任意 custom key。
- 搜索索引可以选择性索引私有扩展，但应记录来源和字段命名空间。

### 格式规范引用

具体数据格式的 attributes 结构不写在本文中。本文只定义 attributes 分区、来源和合并规则；各格式的组合形态、字段来源、标准扩展和私有扩展应在独立格式规范中说明。

首批格式规范：

| 格式 | 规范文档 | 当前实现重点 |
|---|---|---|
| Shapefile | `docs/next/addp格式规范-shapefile.md` | 多文件 item、DBF 字段、空间扩展、Shapefile 私有扩展 |
| GeoJSON | `docs/next/addp格式规范-geojson.md` | FeatureCollection 字段推断、空间扩展、GeoJSON 私有扩展 |
| CSV / TSV | `docs/next/addp格式规范-csv-tsv.md` | 分隔符、编码、表头、字段采样 |
| Excel | `docs/next/addp格式规范-excel.md` | 工作表、字段采样、Excel 私有扩展 |
| Parquet / ORC / Avro 湖表 | `docs/next/addp格式规范-湖表.md` | 目录树或单文件 item、schema、分区与辅助文件 |
| SQLite / GeoPackage | `docs/next/addp格式规范-sqlite-geopackage.md` | 容器文件 item、内部表枚举、空间扩展 |
| 图片 | `docs/next/addp格式规范-图片.md` | 媒体扩展、EXIF、GPS 空间扩展 |
| PDF | `docs/next/addp格式规范-pdf.md` | 文档扩展、提取状态、文本预览 |

新增格式或扩展现有格式时，应先补充或修订对应格式规范，再修改 detector / parser / extractor / manager provider。

### 标准扩展命名空间

平台标准扩展命名空间目前包括：

| 命名空间 | 含义 | 典型字段 |
|---|---|---|
| `extensions.spatial` | 空间扩展信息 | `geometry_column`、`geometry_type` / `geometry_types`、`srid`、`extent`、`dimension`、`has_spatial_index` |
| `extensions.media` | 图片、音频、视频媒体信息 | width、height、duration、codec、color_mode 等 |
| `extensions.document` | 文档信息 | title、author、page_count、word_count、keywords 等 |
| `extensions.statistics` | 统计与采样信息 | sample_size、null_count、min、max 等 |
| `extensions.extraction` | 内容提取状态和提取结果摘要 | extracted_metadata、plain_text_preview、extractor、status 等 |

`extensions.unqualified` 不是业务语义命名空间，也不是某种格式扩展。  
它是 `meta` normalizer 的隔离区，用于临时收纳以下信息：

- attributes 顶层出现的未登记字段。
- `extensions` 下不属于平台标准命名空间、也不符合私有命名空间命名规则的 key。
- 迁移期旧实现产生的无归属字段。

`unqualified` 的含义是“未归类 / 未合格命名空间”。平台级行为不得依赖其中字段，正常的新 parser / detector 不应主动写入 `extensions.unqualified`。如果其中字段需要被长期使用，必须先明确归属：要么进入平台标准扩展，要么进入合规私有命名空间。

## 三、各模块职责

### meta

`meta` 是第一入口，负责：

- 扫描资源树
- 推断组合形态
- 归并 meta item
- 判断数据家族
- 初步识别文件格式
- 提取可索引的元数据
- 维护 item fingerprint
- 在落库前归一化 `meta_item.attributes`
- 保护平台核心字段不被 parser / extractor / 第三方插件随意覆盖

`meta` 不应只按单文件扩展名生成 item。

### common/format

`common/format` 负责：

- 文件格式识别
- `TableInfo` / `ObjectInfo` 模型
- `FieldInfo` 类型标准化
- `ExtensionInfo` 定义
- parser / extractor 注册

它不直接决定 meta item 如何归并，但应为归并后的 item 提供解析能力。

`common/format` 可以输出格式解析结果和扩展信息，但不应绕过 `meta` normalizer 直接决定 attributes 的最终结构。

### common/dataitem

`common/dataitem` 负责组合形态推断和数据项识别。

它应放在 `common` 下作为平台级共享能力，而不是放在 `meta` 内部。

原因：

- `meta` 是第一调用方，但不是组合形态规则的唯一消费者
- `manager`、`transfer`、`asset` 等模块后续都可能复用 item 识别结果
- 组合形态推断发生在文件格式识别之前，不应放在 `common/format` 内部
- 它也不是引擎插件能力本身，不宜放在 `common/engine/plugin`

建议目录：

```text
common/dataitem/
  types.go
  detector.go
  registry.go
  resolver.go
```

其中：

- `types.go` 定义 `CompositionType`、`DataFamily`、`DetectedItem`
- `detector.go` 定义组合形态 detector 接口
- `registry.go` 负责 detector 注册与优先级
- `resolver.go` 负责基于目录、文件组、对象列表执行统一推断

`common/dataitem` 重点识别：

- 多文件组合
- 目录树数据集
- 容器型文件
- 混合文件集合

它应服务于 `meta` 扫描阶段。

`common/dataitem` 输出的组合形态、数据家族、格式、入口路径和组成文件列表，应成为 `attributes.item` 分区的主要来源。

### manager

`manager` 不应重新发明扫描语义，也不应承担 item 识别职责。

它应优先使用 `meta` 已识别的：

- item 类型
- 组合形态
- 数据家族
- 文件格式
- 扩展信息
- physical path

然后选择合适的预览 provider。

更明确地说：

- `meta` 是 item 识别的唯一权威来源。
- `manager` 只消费已经由 `meta` 扫描并入库的 item。
- 未做 `meta` 扫描的数据不应在 `manager` 中被看见，也不应进入预览流程。
- 如果用户需要预览未扫描的数据，应先通过 `manager` 中已有的手动扫描入口触发 `meta` 扫描，扫描完成后再预览。
- `manager` 不应根据文件后缀、bucket 路径、provider 优先级等重新判断 item 是什么。
- `manager` 只根据 `meta` item 的 `item_type`、`composition_type`、`data_family`、`format`、`entry_path`、`component_files`、`physical_path` 等标准属性选择预览能力。
- provider 的优先级只能作为历史插件注册机制的内部排序，不应承担 item 识别、格式识别或兜底猜测职责。
- 旧的“未扫描路径也能直接预览”“provider 自己猜格式并抢路由”的实现，在新的 meta-first 实现完成后应删除。
- `PreviewResolver` 应作为 `manager` 预览主入口，先校验请求能解析到 MetaItem 或 MetaNode，再根据 `meta` 标准属性确定性选择 provider。
- `PreviewRegistry.Resolve` 这类 `Supports + priority` 选择机制只允许作为过渡兼容层，不应作为新的主路由。

推荐的预览路由原则：

| Meta 识别结果 | Manager 处理方式 |
|---|---|
| `item_type=lake_table` | 使用湖表预览能力 |
| `item_type=collection` | 使用文档集合预览能力 |
| `item_type=label` / `relationship` | 使用图谱预览能力 |
| `data_family=tabular` 且 `format` 为文件表格式 | 使用文件表预览能力 |
| `data_family=image/video/audio/document` | 使用对象内容预览能力 |
| `composition_type=multi_file` | 使用 `entry_path` 和 `component_files` 读取整套数据 |
| `composition_type=container_file` | 使用容器格式能力打开或展开内部对象 |

### transfer

`transfer` 应消费标准化后的 item 与元数据。

它不应重复判断组合形态，也不应重复推断字段类型。

### 第三方插件

第三方插件应通过受控接口扩展 ADDP 能力。

插件可以提供：

- 新文件格式识别
- 新组合形态 detector
- parser / extractor
- 私有扩展属性
- 可展示字段描述
- 可索引字段声明

插件不应直接写入或覆盖平台保留字段。  
插件输出应由 `meta` normalizer 合并进 `attributes`，私有字段必须进入插件命名空间。

## 四、新增能力的推荐步骤

### 新增单文件格式

1. 明确它属于哪个数据家族
2. 明确它默认是单文件单 item
3. 实现格式识别
4. 实现 parser / extractor
5. 注册到 `common/format`
6. 定义标准扩展或私有扩展命名空间
7. 接入 `meta` 扫描和 attributes normalizer
8. 接入 `manager` 预览

### 新增多文件组合格式

1. 定义组合规则
2. 在 `common/dataitem` 实现 detector
3. 在 `meta` 中先归并 item
4. 明确主文件或入口文件
5. 判断数据家族
6. 实现 parser / extractor
7. 定义扩展属性归属
8. 接入预览和索引

### 新增容器文件格式

1. 定义容器文件如何成为 item
2. 定义容器内部是否展开为子 item
3. 实现容器识别
4. 实现内部元数据枚举
5. 定义容器级 attributes 和内部 item 的关系
6. 实现预览与字段抽取

### 新增目录树数据集

1. 定义目录结构特征
2. 在 `common/dataitem` 实现目录 detector
3. 在 `meta` 中将目录归并为 item
4. 记录入口目录和组成文件
5. 判断数据家族
6. 提取扩展信息
7. 将目录树私有信息写入扩展命名空间

## 五、实现约束

- 不要先按单个文件格式生成 item，再事后合并。
- 不要把目录型、容器型放进数据家族，它们属于组合形态。
- 不要把空间放进数据家族，空间属于扩展语义。
- 不要默认空间字段名一定是 `geom`。
- 不要让 `manager` 重复实现 `meta` 的组合识别规则。
- 不要让 `manager` 预览未扫描、未入库的资源；未扫描资源必须先触发 `meta` 扫描。
- 不要让 `manager` provider 通过优先级抢路由来决定 item 类型或文件格式。
- 不要让 `transfer` 重复推断字段类型。
- 不要把组合形态 detector 长期留在 `meta` 或 `common/format` 中。
- 不要让 parser / extractor / 第三方插件直接覆盖平台核心 attributes。
- 不要把格式私有属性继续平铺到 attributes 顶层。
- 不要把格式私有属性写入 `extensions.unqualified` 作为长期方案；应使用平台标准扩展或合规私有命名空间。
- 不要让平台级行为依赖未标准化的 custom key。

## 六、推荐验证方式

新增或修改能力后，至少验证：

1. `meta` 是否正确归并 item
2. item 的组合形态是否正确
3. 数据家族是否正确
4. 文件格式是否正确
5. 字段或对象元数据是否完整
6. 扩展信息是否可被 `manager` 使用
7. fingerprint 是否稳定
8. attributes 是否通过 normalizer
9. 平台核心字段是否未被扩展字段覆盖
10. 第三方或格式私有字段是否进入命名空间

## 七、结论

落地实现必须从 `meta` 扫描出发。

正确顺序是：

**资源树扫描 -> 组合形态推断 -> meta item 归并 -> 数据家族判断 -> 文件格式识别 -> 元数据与扩展信息提取。**

attributes 治理必须采用：

**受控核心 + 开放扩展。**

平台核心字段要稳定，第三方扩展要开放，但二者必须分区隔离，不能继续混成不可预期的平铺 JSON。

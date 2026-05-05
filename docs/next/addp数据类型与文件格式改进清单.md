# ADDP 数据类型与文件格式改进清单

更新时间：2026-05-05

本文是 ADDP 在“数据类型、文件格式、组合形态、扩展语义、attributes 治理”方向上的持续跟进清单。  
它的定位是路线图和状态板：记录当前共识、实现现状、主要差距、阶段计划和后续接力点。

相关文档分工：

- `addp数据类型与文件格式概念规范.md`：定义概念边界，回答“应该如何理解这些概念”。
- `addp数据类型与文件格式落地指南.md`：定义实现原则和落地结构，回答“代码应该怎么做”。
- 本文：记录推进计划、当前状态和待办，回答“接下来怎么改、做到什么程度算完成”。

## 一、当前共识

已确认的概念口径如下：

1. 存储引擎只负责“数据在哪里”。
2. 数据家族负责“数据长什么样”。
3. 组合形态负责“一套数据如何组成一个 meta item”。
4. 文件格式负责“单个文件如何编码”。
5. 扩展信息负责“这类数据还具备哪些附加语义”。
6. `FieldInfo` 只属于结构化数据语义，应挂在 `TableInfo` 之下。
7. 空间不是独立数据家族，而是扩展语义。
8. 目录型、容器型、多文件型都属于组合形态。
9. 落地实现必须从 `meta` 扫描出发，先推断组合形态，再归并 item，然后判断数据家族和文件格式。
10. `meta` 是 item 识别和入库的权威来源。
11. `manager` 只消费 `meta` 已扫描入库的 item，不预览未扫描资源。
12. `manager` 不通过 provider 优先级、文件后缀或路径重新识别 item；provider 只根据 `meta` 标准属性选择预览能力。
13. `meta_item.attributes` 应采用“受控核心 + 开放扩展”：平台核心字段必须受控，第三方格式和插件私有属性必须允许扩展但要进入命名空间。
14. ADDP 约束的是“平台如何理解一个 item”，不是穷举所有格式可能携带的属性。

## 二、目标状态

最终希望形成下面这条稳定链路：

```text
资源树扫描
  -> 组合形态推断
  -> meta item 归并
  -> 数据家族判断
  -> 文件格式识别
  -> 元数据与扩展信息提取
  -> attributes 归一化
  -> manager / transfer / asset / search 消费标准 item
```

目标状态要求：

- 组合形态由 `common/dataitem` 统一识别。
- 文件格式由 `common/format` 统一识别和解析。
- `meta` 负责把扫描、识别、解析结果合成为稳定的 meta item。
- `meta_item.attributes` 分区表达，不再长期依赖平铺 JSON。
- 平台核心字段可校验，第三方扩展可开放。
- 平台模块只依赖核心字段和标准扩展，不依赖任意 custom key。

推荐 attributes 目标结构：

```json
{
  "schema_version": 1,
  "storage": {},
  "item": {},
  "schema": {},
  "extensions": {}
}
```

其中：

| 分区 | 职责 |
|---|---|
| `storage` | 存储位置、对象基础属性、catalog 基础信息 |
| `item` | 组合形态、数据家族、格式、入口路径、组成文件 |
| `schema` | 字段、主键、索引、表级结构信息 |
| `extensions` | 空间、媒体、文档、统计、第三方插件私有属性 |

## 三、当前实现快照

### 1. `common/dataitem`

当前已新增目录：

```text
common/dataitem/
  types.go
  detector.go
  registry.go
  resolver.go
```

已落地：

- `CompositionType`
- `DataFamily`
- `DetectedItem`
- `CompositeItemDetector`
- `ResolveDirectory`
- `InferSingleFileItem`
- `InferSingleFile`
- `InferFormat`
- `InferDataFamily`
- `BuildAttributes` 已开始输出 `attributes.item` / `attributes.storage` 分区，同时保留平铺兼容字段

当前说明：

- `common/dataitem` 已经成为组合形态推断的主入口。
- `common/format/detector` 目前保留为兼容转发层。
- 已新增 Shapefile 多文件 detector。
- 单文件 SQLite / GeoPackage 已识别为 `container_file`。
- 复杂目录树、跨层辅助文件、混合集合归并仍待增强。

### 2. `common/format`

当前 `common/format` 已具备：

- `TableInfo`
- `ObjectInfo`
- `FieldInfo`
- `ExtensionInfo`
- `FileTableParser`
- `DBTableParser`
- `ObjectInfoParser`
- `DocCollectionParser`
- `DetectFormat`
- MIME 转换和 TypeMapper

当前说明：

- `common/format` 仍是格式识别和解析的共享核心。
- 它不应承载组合形态主语义。
- parser / extractor 应输出格式解析结果和扩展信息，不应直接决定 attributes 最终结构。

### 3. `meta`

当前 `meta` 已经开始按资源树和组合规则工作：

- 文件系统目录型组合项通过 `common/dataitem.ResolveDirectory` 进入 detector 链。
- 文件系统普通文件通过 `common/dataitem.InferSingleFileItem` 补齐标准 item attributes。
- 对象存储单对象通过 `common/dataitem.InferSingleFile` 补齐标准 item attributes。
- 对象存储同一前缀下的直接子对象会按前缀分组后进入 `common/dataitem.ResolveDirectory`。
- 关系数据库表先落 `data_family=tabular`、`format=<engine_type>`，并已源头写入 `attributes.item` / `attributes.schema`。
- NoSQL collection 先落 `data_family=tabular`、`format=<engine_type>`，字段和索引已源头写入 `attributes.schema`。
- 图数据库 label / relationship 先落 `data_family=graph`。
- 元数据查询接口已优先从 `attributes.schema` 和 `attributes.extensions.spatial` 读取字段与空间信息，并保留平铺字段兼容。

当前不足：

- 扫描中间结构仍未完全统一。
- 部分路径仍手工写 attributes 顶层字段。
- `attributes` 已在 `ScanRepository.UpsertItemSelective` 接入第一版 normalizer，且 `common/dataitem.BuildAttributes`、数据库扫描、NoSQL 扫描、文件系统扫描、对象存储扫描已开始源头双写标准分区与平铺兼容字段。
- 数据库空间元数据已源头双写到 `attributes.extensions.spatial`。
- 平铺字段兼容层已开始保留读取兼容，但删除计划还没有明确。

### 4. `manager`

当前 `manager` 已经有多条预览链路：

- `FileTablePreviewProvider`
- `LakeTablePreviewProvider`
- `DatabaseTablePreviewProvider`
- `ObjectStoragePreviewProvider`
- `FileSystemPreviewProvider`
- `DocCollectionPreviewProvider`
- 图谱类预览 provider
- `ObjectContentRegistry`
- `PreviewRegistry`
- `PreviewResolver`

已完成第一版调整：

- `PreviewResolver` 已开始按 `MetaItem` / `MetaNode` 做确定性路由。
- 预览取不到 MetaItem / MetaNode 时会返回未扫描错误。
- 新链路优先按 `item_type`、`data_family`、`format` 等标准属性选择 provider。

当前不足：

- provider 内部仍保留部分历史兜底格式推断。
- `PreviewRegistry.Resolve` 的 `Supports + priority` 机制仍作为兼容层存在。
- `PreviewResolver` 和共享属性读取 helper 已优先消费 `attributes.item` / `attributes.storage` 等分区，但 provider 内部还需继续减少兜底猜测。

### 5. 平行模型

当前仍存在一些平行模型或历史结构：

- `common/engine/plugin.TableInfo`
- `common/engine/plugin.ObjectInfo`
- `common/format.ScannerTableInfo`
- `common/format.ScannerFieldInfo`

短期可以保留，但需要逐步明确归属和迁移路线，避免重复表达同一语义。

## 四、主要差距

### 1. 组合形态覆盖不完整

当前已有 Shapefile、多种单文件基础识别和部分湖表处理，但组合形态规则还需要继续扩展：

- 多文件单 item
- 容器文件单 item
- 目录树单 item
- 混合集合单 item
- 嵌套组合形态
- 对象存储跨层组件归并

### 2. 扫描语义还未完全统一

`meta` 已经是入口，但部分代码路径仍然：

- 先按扩展名或格式分支。
- 直接构造 attributes。
- 使用旧扫描中间结构。
- 在数据库表、文件表、对象和集合之间保留不同语义口径。

### 3. 数据家族与格式识别仍有交织

后续应保持：

```text
先归并 item
  -> 再判断 data_family
  -> 最后识别 format
```

格式识别不能再承担组合形态判断职责，空间也不能再被当作独立数据家族。

### 4. attributes 仍是弱结构化

当前第一版已经写入标准属性：

- `composition_type`
- `data_family`
- `format`
- `entry_path`
- `component_files`
- `physical_path`
- `fields`

但问题仍在：

- 缺少 `schema_version`。
- 缺少统一 normalizer。
- 缺少 `storage` / `item` / `schema` / `extensions` 分区。
- parser / extractor / plugin 仍可能直接写顶层字段。
- 第三方扩展没有命名空间约束。
- 平台行为仍可能读取平铺 custom key。

### 5. 空间扩展需要标准化

空间能力已经存在于多个 parser 和 preview provider 中，但缺少统一口径：

- 空间属于扩展语义。
- 空间可能挂在表格型数据、影像型数据或其他数据家族上。
- 预览、地图渲染、空间查询应依赖标准空间扩展，而不是硬编码格式分支。

### 6. 第三方扩展机制还不完整

ADDP 的“全域”目标要求未来允许第三方扩展更多数据格式。  
因此不能把 attributes 做成封闭全字段 schema，但必须要求：

- 第三方扩展有命名空间。
- 插件不能覆盖平台核心字段。
- 插件能声明可展示字段、可索引字段和能力边界。
- 私有扩展晋升为标准扩展前，平台核心行为不能依赖它。

## 五、阶段计划

### 阶段 0：文档与共识收口

状态：进行中。

目标：

- 三份文档定位清楚。
- 概念规范、落地指南、改进清单术语一致。
- attributes 治理原则明确为“受控核心 + 开放扩展”。

任务：

1. 补齐概念规范中的 attributes 分层原则。（已完成）
2. 补齐落地指南中的 attributes 推荐结构和写入规则。（已完成）
3. 整理本文为后续推进路线图。（已完成）

验收：

- 新会话能仅凭三份文档理解当前方向。
- 文档不再把“格式扩展开放性”和“平台核心字段约束”混为一谈。

### 阶段 1：`common/dataitem` 与组合形态收口

状态：第一版已完成，继续增强。

目标：

- `common/dataitem` 成为组合形态推断主入口。
- `meta` 不持有私有组合形态规则。
- 组合 item 的 fingerprint 与入口路径、组件列表一致。

已完成：

1. 新增 `common/dataitem` 包。
2. 定义 `CompositionType`、`DataFamily`、`DetectedItem`。
3. 建立 `CompositeItemDetector` 和 registry。
4. `common/format/detector` 改为兼容转发层。
5. 文件系统扫描接入 `common/dataitem.ResolveDirectory`。
6. 文件系统普通文件接入 `InferSingleFileItem`。
7. 对象存储单对象接入 `InferSingleFile`。
8. 对象存储同一前缀直接子对象接入 `ResolveDirectory`。
9. 新增 Shapefile 多文件 detector。

待办：

1. 增强对象存储递归目录树归并。
2. 支持跨层辅助文件。
3. 支持复杂混合集合。
4. 梳理数据库表、collection、label、relationship 是否需要“引擎原生 item”组合形态。
5. 增加更多组合形态测试样例。

验收：

- 多文件、容器、目录树、混合集合都有稳定 detector 接入点。
- 组件文件在归并为组合 item 后不会重复落成普通单文件 item。
- `entry_path`、`component_files`、`composition_type` 稳定可回显。

### 阶段 2：数据家族与格式识别分离

状态：第一版已开始。

目标：

- 数据家族是主分类。
- 文件格式只表达编码方式。
- 空间、采样、索引、页数、EXIF 等进入扩展语义。

已完成：

1. 明确 `common/dataitem.DataFamily` 第一版枚举。
2. 沿用 `common/format.FormatType` 作为格式枚举基础。
3. `InferFormat` 统一处理显式格式、MIME、扩展名。
4. 对象存储常见扩展名别名已规范化。
5. `InferDataFamily` 基于规范化格式判断主数据家族。
6. SQLite / GeoPackage 识别为 `container_file`，数据家族保持 `tabular`。

待办：

1. 继续审核扫描路径中混合判断逻辑。
2. 减少 provider 内部重复格式分支。
3. 明确更多格式到数据家族的映射。
4. 明确未知格式的处理策略。

验收：

- 同类资源在不同组合形态下仍识别为同一数据家族。
- 格式识别不再承担组合形态判断职责。
- 空间不再作为独立数据家族。

### 阶段 3：`meta_item.attributes` 治理

状态：第一版已开始。

目标：

- attributes 从平铺 JSON 收口为“受控核心 + 开放扩展”。
- 平台核心字段稳定可校验。
- 第三方扩展开放但有命名空间。

任务：

1. 定义 attributes 目标结构：`schema_version`、`storage`、`item`、`schema`、`extensions`。（已完成）
2. 明确平台保留字段和第三方插件可写字段边界。
3. 新增 attributes normalizer，所有 `meta_item` 落库前统一经过 normalizer。（第一版已完成）
4. 让 `common/dataitem` 输出进入 `attributes.item`。（源头第一版已双写分区和平铺兼容）
5. 让存储引擎、catalog provider、对象枚举结果进入 `attributes.storage`。（文件系统和对象存储扫描源头第一版已双写）
6. 让字段、主键、索引等结构信息进入 `attributes.schema`。（数据库、NoSQL、文件系统源头第一版已双写，查询接口已优先读取）
7. 让空间、媒体、文档、统计等通用能力进入标准扩展分区。（空间第一版已源头双写到 `extensions.spatial` 且查询接口已优先读取，其他扩展待补齐）
8. 让第三方或格式私有属性进入 `attributes.extensions.<namespace>`。
9. 定义冲突处理规则，禁止 parser / extractor / 第三方插件直接覆盖平台核心字段。
10. 为迁移期保留旧平铺字段兼容读取，但新增读取逻辑优先消费分区结构。（manager 与 meta 查询第一版已完成）

建议优先实现顺序：

1. 先新增 normalizer 和目标结构写入。
2. 同时保留旧平铺字段兼容。
3. 迁移 `PreviewResolver` 优先读取 `attributes.item`。
4. 迁移对象预览、文件表预览、文档集合预览读取路径。
5. 最后删除不再需要的平铺字段读取。

验收：

- 平台核心字段有明确 schema 和归属。
- 第三方扩展能存储不可预知字段，但必须有命名空间。
- 平台级行为不依赖未标准化 custom key。
- 冲突字段不会覆盖核心 item 语义。
- 旧平铺字段有兼容策略和删除计划。

### 阶段 4：空间扩展收口

状态：待推进。

目标：

- 把空间做成统一扩展语义。
- 表格型空间数据和影像型空间数据都能走同一扩展口径。

任务：

1. 统一 `SpatialInfo` 的使用口径。
2. 梳理 `shapefile`、`geojson`、`postgresql`、影像空间元数据。
3. 明确哪些字段属于空间标准扩展。
4. 明确哪些预览能力依赖空间扩展。
5. 将空间信息接入 `attributes.extensions.spatial`。

验收：

- 前端地图展示或空间渲染只依赖标准空间扩展。
- 不硬编码空间字段名为 `geom`。
- 不通过具体格式判断是否具备空间能力。

### 阶段 5：`manager` 预览路由收口

状态：第一版已完成，继续删除历史逻辑。

目标：

- `manager` 只消费 `meta` 标准 item。
- 预览路由确定性选择 provider。
- provider 不再通过优先级抢语义路由。

已完成：

1. `PreviewResolver` 先要求存在 MetaItem / MetaNode。
2. 取不到 MetaItem / MetaNode 时返回未扫描错误。
3. 优先按 `item_type` 处理 lake table、collection、label、relationship。
4. 再按 `data_family` / `format` 选择文件表、数据库表、对象内容或文件系统预览能力。
5. `PreviewRegistry.Resolve` 标记为过渡兼容。
6. `PreviewResolver` 已优先读取 `attributes.item.data_family` / `attributes.item.format`，并保留平铺字段兼容。

待办：

1. 继续迁移 provider 内部读取路径，优先消费 `attributes.item` / `attributes.storage` / `attributes.schema` 分区。
2. 删除 provider 内部兜底格式推断。
3. 删除未扫描路径直接预览旧入口。
4. 删除 provider 优先级抢语义路由。
5. 增加更多预览路由表测试。

验收：

- `manager` 预览不再支持未扫描资源直接预览。
- provider 不再自行猜测组合形态、数据家族或文件格式。
- 所有预览路由都可以追溯到 meta 标准属性。

### 阶段 6：模型与注册收口

状态：待推进。

目标：

- 减少平行模型与重复注册体系。
- 新增能力只需要进入清晰的一条或少数几条注册路径。

任务：

1. 收拢 `common/format` 与 `common/engine/plugin` 的平行结构。
2. 审核 `common/format/registry.go`。
3. 审核 `common/dataitem` detector registry。
4. 决定 `common/format/detector` 是否保留兼容层或删除。
5. 审核 `manager` 的 `ObjectContentRegistry`。
6. 明确第三方插件能力声明格式。

验收：

- registry 语义统一。
- 新格式、新组合形态、新 extractor 的接入路径清楚。
- 旧结构的保留理由清楚。

## 六、当前优先级

### P0

- 完成 attributes 目标结构和 normalizer 设计。
- 继续让 `meta` 通过 `common/dataitem` 使用组合形态能力。
- 迁移 `manager` 预览路由优先消费标准 attributes。
- 保持文档和实现术语一致。

### P1

- 增强对象存储组合归并能力。
- 统一空间扩展口径。
- 收敛旧扫描中间结构。
- 删除 `manager` 预览旧兜底逻辑。

### P2

- 收拢平行 registry。
- 建立第三方插件扩展声明机制。
- 建立更统一的能力发现层。

## 七、后续会话接力点

后续新会话可以直接从下面这些点接手：

1. 设计并实现 `meta_item.attributes` normalizer。
2. 将 `composition_type`、`data_family`、`format`、`entry_path`、`component_files` 迁移到 `attributes.item`。
3. 将对象存储、文件系统、数据库 catalog 基础信息迁移到 `attributes.storage`。
4. 将字段、主键、索引等结构信息迁移到 `attributes.schema`。
5. 将空间信息迁移到 `attributes.extensions.spatial`。
6. 改造 `PreviewResolver` 优先读取分区后的 attributes。
7. 增强对象存储目录/前缀组合归并，支持 shapefile、parquet dataset 等多文件或目录型数据集。
8. 梳理数据库表、文档集合与 `dataitem` 模型的边界。
9. 收口 `TableInfo` / `ObjectInfo` 与 `Scanner*` 模型。
10. 删除 `manager` 中未扫描路径直接预览、provider 自行猜格式、provider 优先级抢语义路由等历史逻辑。

## 八、结语

这是一条长期工程线。  
后续任何一次实现、重构或补文档，都应优先回到这里更新状态，然后再推进代码。这样新会话接力时，团队对齐成本会低很多。

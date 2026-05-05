# ADDP 数据类型与文件格式持续跟进计划

更新时间：2026-05-05

本文是一个持续跟进的计划文档，用来记录 ADDP 在“数据类型、文件格式、组合形态、扩展语义”这条线上的当前状态、后续任务和阶段性验收标准。  
它会随着后续会话持续更新，不是一次性结题文档。

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

## 二、当前代码快照

这一部分记录当前仓库里已经存在的相关实现，方便后续会话接力时快速定位。

### 1. `common/format` 已经是共享核心，但不应承载组合形态主语义

当前 `common/format` 已具备以下能力：

- `TableInfo`、`ObjectInfo`、`FieldInfo`、`ExtensionInfo`
- `FileTableParser`
- `DBTableParser`
- `ObjectInfoParser`
- `DocCollectionParser`
- `DetectFormat` / MIME 转换 / TypeMapper

相关文件包括：

- `common/format/info.go`
- `common/format/interface.go`
- `common/format/registry.go`
- `common/format/detection.go`

历史组合形态 detector 兼容层仍位于：

- `common/format/detector/registry.go`
- `common/format/detector/detector.go`

这个位置属于历史实现位置。根据当前概念共识，组合形态推断发生在文件格式识别之前，主入口已开始迁移到 `common/dataitem`。

当前已新增目录：

```text
common/dataitem/
  types.go
  detector.go
  registry.go
  resolver.go
```

已落地：

- `CompositionType`、`DataFamily`、`DetectedItem`
- `CompositeItemDetector` 主接口与 registry
- `ResolveDirectory` 目录级组合形态推断入口
- `InferSingleFileItem` 单文件基础语义推断
- `common/format/detector` 保留为兼容转发层

### 2. `meta` 已经开始按资源树和组合规则工作

当前 `meta` 并不是只按单文件格式扫描：

- `scan_filesystem_service.go` 已使用 `detector.GetAll()` 做复合项识别
- `scan_object_storage_service.go` 已通过 `format.GetObjectInfoParser()` 提取对象元数据
- `scan_database_service.go` 仍保留 `ScannerTableInfo` 作为扫描中间结构

说明：

- `meta` 已经是组合形态识别和 item 归并的关键入口
- 但扫描中间结构还未完全统一
- 后续 `meta` 应调用 `common/dataitem`，而不是自己持有组合形态推断规则

### 3. `manager` 已经有多条预览链路

当前 `manager` 侧已经形成多种预览 provider：

- `FileTablePreviewProvider`
- `LakeTablePreviewProvider`
- `DatabaseTablePreviewProvider`
- `ObjectStoragePreviewProvider`
- `FileSystemPreviewProvider`
- `DocCollectionPreviewProvider`
- 图谱类预览 provider

同时还存在：

- `ObjectContentRegistry`
- `PreviewRegistry`
- `PreviewResolver`

说明：

- 预览层已经比较完整
- 但组合形态、数据家族、文件格式的判定规则仍然分散在多个 provider 中

### 4. 旧模型仍未完全收口

当前仍存在一些平行模型或历史结构：

- `common/engine/plugin.TableInfo`
- `common/engine/plugin.ObjectInfo`
- `common/format.ScannerTableInfo`
- `common/format.ScannerFieldInfo`

这些结构在短期内还需要保留，但后续应逐步收口，避免重复语义。

## 三、当前主要问题

### 1. 扫描入口统一了，但扫描语义还没统一

`meta` 已经是入口，但：

- 组合形态推断还没有完全成为第一层语义
- 有些路径仍然会先按文件后缀或格式分支
- 扫描中间结构还保留旧命名

### 2. 组合形态识别还不够系统

当前代码已经有：

- `CompositeItemDetector`
- `dataitem.GetAll()` 主入口
- `common/format/detector.GetAll()` 兼容入口
- 单文件湖表识别

但组合形态规则仍不够完整，还缺少更多 detector 覆盖：

- 单文件单 item
- 多文件单 item
- 容器文件单 item
- 目录树单 item
- 混合集合单 item

### 3. 数据家族与格式识别仍有混合判断

目前不少逻辑仍然把“格式识别”和“数据家族判断”交织在一起。  
后续应拆成两层：

1. item 先归并
2. 再判断数据家族
3. 最后做具体文件格式识别

### 4. 空间扩展需要进一步收口

空间能力已经存在于多个 parser 和 preview provider 中，但还缺少一个统一口径：

- 空间是扩展语义
- 它会影响字段解释、预览、地图展示和查询
- 但它不应该成为新的数据家族

### 5. 预览与扫描的规则还分散

目前 `meta`、`manager`、`common/format`、`common/engine` 都有自己的判断逻辑。  
这说明体系已经跑起来了，但还没有完全收口。

## 四、持续推进目标

1. 新增 `common/dataitem`，把组合形态识别提升为平台级共享能力。
2. 把数据家族判断从组合形态之后单独拎出来。
3. 把文件格式识别下沉到统一的格式能力层。
4. 把空间、采样、页数、媒体信息等统一为扩展语义。
5. 收敛旧模型和中间结构，减少重复转换。
6. 让新增数据类型的接入路径标准化。

## 五、阶段性计划

### 阶段 0：文档与共识收口

状态：已开始，基础包已落地。

任务：

1. 统一概念文档
2. 统一落地顺序
3. 把本计划文档维护为持续跟进入口

验收：

- 概念规范、落地指南、改进计划三份文档术语一致
- 组合形态与空间扩展边界清楚

### 阶段 1：`common/dataitem` 与组合形态收口

状态：已完成第一阶段收口。后续递归目录树、跨层辅助文件、复杂混合集合归并作为增强项继续推进。

目标：把组合形态推断从 `meta` 消费逻辑和 `common/format/detector` 历史位置中抽出来，沉淀为 `common/dataitem` 平台级能力。`meta` 先调用 `common/dataitem` 识别“怎么组成一套数据”，再识别“它是什么数据”。

任务：

1. 新增 `common/dataitem` 包。（已完成）
2. 定义 `CompositionType`、`DataFamily`、`DetectedItem` 等基础类型。（已完成）
3. 将 `common/format/detector` 的 `CompositeItemDetector` 迁移或适配到 `common/dataitem`。（已完成：旧包保留兼容转发）
4. 梳理 `scan_filesystem_service.go`，让其通过 `common/dataitem` 做组合形态推断。（已完成第一版）
5. 梳理 `scan_object_storage_service.go`，确认对象存储场景是否也走统一 item 推断入口。（已完成第一版：单对象补齐 `composition_type`、`data_family`、`entry_path`、`component_files` 等标准 attributes；同一前缀直接子对象已接入 `common/dataitem.ResolveDirectory` 做组合归并）
6. 梳理 `scan_database_service.go`，确认数据库表、collection 与 dataitem 模型的边界。（已完成第一版：关系表、NoSQL collection/label/relationship 先补齐 `data_family` 与 `format`；数据库原生 catalog item 暂不写文件组合形态）
7. 统一 `ScannerTableInfo` / `ScannerFieldInfo` 的定位。（已完成定位说明：仅作为旧 Scanner 接口和 meta 数据库扫描之间的适配层，新语义进入 `TableInfo` / `ObjectInfo` / `common/dataitem`）

当前实现说明：

- 文件系统目录型组合项通过 `common/dataitem.ResolveDirectory` 进入 detector 链。
- 文件系统普通文件通过 `common/dataitem.InferSingleFileItem` 补齐标准 item attributes。
- 对象存储单对象通过 `common/dataitem.InferSingleFile` 补齐标准 item attributes，但保留对象存储原有 `bucket/path/name` 语义，避免影响 fingerprint。
- 对象存储同一前缀下的直接子对象会按前缀分组后进入 `common/dataitem.ResolveDirectory`；匹配组合项后写入目录型 item，并跳过组件对象的单文件 item 落库。
- 对象存储组合归并当前是第一版，只处理同一前缀直接子对象；递归目录树、跨层辅助文件、混合集合归并仍待增强。
- 关系数据库表属于引擎原生逻辑 item，本轮只落 `data_family=tabular`、`format=<engine_type>`。
- NoSQL collection 本轮落 `data_family=tabular`、`format=<engine_type>`；图数据库 label/relationship 落 `data_family=graph`。
- 已新增 Shapefile 多文件 detector，识别同名 `.shp/.shx/.dbf` 必需组件，并归并 `.prj/.cpg/.sbn/.sbx` 等辅助组件。
- `common/format/detector` 仅保留兼容转发层，主注册入口为 `common/dataitem`。

验收：

- 多文件、容器、目录树、混合集合都能被稳定归并。（第一阶段已覆盖 Shapefile 多文件、湖表目录/单文件、SQLite/GeoPackage 容器型单文件基础标识；复杂混合集合后续增强）
- 组合形态 detector 的主入口在 `common/dataitem`。（已完成）
- `meta` 只调用共享 detector，不持有私有组合形态规则。（已完成）
- `meta` item 的组合形态字段可回显。（已完成：写入 attributes）
- item fingerprint 与组合形态一致。（已完成第一版：组合 item 使用组合入口路径；组件对象/文件在匹配组合后跳过单文件落库）

### 阶段 2：数据家族与格式识别分离

状态：已开始。`common/dataitem` 已提供统一的单文件格式规范化和数据家族推断入口，后续继续审计 `manager` 预览 provider。

目标：先判断数据家族，再判断格式。

任务：

1. 明确数据家族枚举。（已完成第一版：`common/dataitem.DataFamily`）
2. 明确文件格式枚举。（已沿用 `common/format.FormatType`，并在 `common/dataitem.InferFormat` 中补齐对象存储常见扩展名别名规范化）
3. 明确 item -> family -> format 的判断顺序。（已完成第一版：组合 item 由 detector 明确返回；单文件 item 在归并后通过 `InferFormat` 和 `InferDataFamily` 生成标准 attributes）
4. 审核 `manager` 里的各类 preview provider。（已开始：`FileTablePreviewProvider` 已优先消费 Meta 标准 attributes）
5. 减少 provider 内部重复格式分支

当前实现说明：

- `common/dataitem.InferFormat` 统一处理显式格式、MIME 类型、文件名扩展名三类输入。
- 对象存储扫描中常见的 `jpg`、`xlsx`、`gpkg`、`.tif` 等扩展名会被规范化为 `jpeg`、`excel`、`geopackage`、`tiff`。
- `InferDataFamily` 以规范化后的格式判断主数据家族，避免把图片、Excel、GeoPackage 等误判为 `unknown`。
- 单文件 SQLite / GeoPackage 继续识别为 `container_file` 组合形态，但数据家族保持 `tabular`。
- `manager` 的 `PreviewRequest` 已透传 Meta attributes，`FileTablePreviewProvider` 优先使用 `format` / `content_type`，再按文件名兜底，避免绕过 `common/dataitem` 的规范化规则。

验收：

- 同类资源在不同组合形态下仍能识别为同一数据家族。（已补充 `common/dataitem` 单元测试覆盖）
- 格式识别不再承担组合形态判断职责。（已完成第一版：组合形态由 `CompositionType`/detector 承担，格式只作为 `format` attribute）

### 阶段 3：空间扩展收口

目标：把空间做成统一扩展语义。

任务：

1. 统一 `SpatialInfo` 的使用口径
2. 梳理 `shapefile`、`geojson`、`postgresql`、影像空间元数据
3. 明确哪些字段属于空间扩展
4. 明确哪些预览能力依赖空间扩展

验收：

- 表格型空间数据和影像型空间数据都能走统一扩展口径
- 前端地图展示或空间渲染只依赖扩展信息，不依赖硬编码格式分支

### 阶段 4：模型与注册收口

目标：减少平行模型与重复注册体系。

任务：

1. 收拢 `common/format` 与 `common/engine/plugin` 的平行结构
2. 审核 `common/format/registry.go`
3. 审核 `common/dataitem` 的 detector registry
4. 决定 `common/format/detector` 是否保留兼容层或删除
5. 审核 `manager` 的 `ObjectContentRegistry`

验收：

- 新能力只需要进入一条明确的注册路径
- registry 语义统一
- 旧结构的保留理由清楚

## 六、后续会话接力点

后续新会话可以直接从下面这些点接手：

1. `common/dataitem` 包如何设计并迁移现有 detector。
2. 对象存储目录/前缀是否接入组合形态 detector，支持 shapefile、parquet dataset 等多文件/目录型对象存储数据集归并。
3. 数据库表、文档集合与 `dataitem` 模型的边界如何表达，是否需要新增“引擎原生 item”组合形态。
4. `TableInfo` / `ObjectInfo` 与 `Scanner*` 模型如何逐步收口。
5. 空间扩展如何统一成一个稳定的扩展语义。
6. `manager` 的预览 provider 如何减少分支判断。

## 七、当前优先级

### P0

- 新增 `common/dataitem` 并作为组合形态推断主入口
- 让 `meta` 通过 `common/dataitem` 使用组合形态能力
- 把数据家族、文件格式、扩展语义顺序理顺
- 继续统一文档和实现的术语

### P1

- 收敛旧扫描中间结构
- 统一空间扩展口径
- 减少预览 provider 内重复逻辑

### P2

- 收拢平行 registry
- 建立更统一的能力发现层

## 八、结语

这是一条长期工程线。  
后续任何一次实现、重构或补文档，都应优先回到这里更新状态，然后再推进代码。这样新会话接力时，团队对齐成本会低很多。

# ADDP 数据项探测器规范

本文定义 ADDP 数据项探测器的设计边界、统一入口和格式规则声明方式。术语以 [ADDP 术语表](../concepts/addp术语表.md) 和 [ADDP 数据项体系图](../concepts/addp数据项体系图.md) 为准。

本文是 data item 内容布局识别、claims / exclusive 合并、`refs` 决策和 `FormatRule` 声明的唯一规范来源。扫描深度、覆盖策略、刷新机制和跨模块触发规则见 [ADDP 元数据扫描机制规范](addp元数据扫描机制规范.md)。其他文档如需引用 detector 规则，只保留链接和一句话摘要，不重复展开。

## 本文边界

本文只定义 data item 识别和资源认领规则：

| 本文负责 | 不在本文定义 |
|---|---|
| 扫描范围如何解析为 `0..N` 个 data item | `attributes` 的完整 JSON schema |
| `layout`、primary content / ref、related refs 和 whole scope 如何确定 | FormatPlugin、info provider、content reader 的接口形态 |
| `claims`、`exclusive` 如何合并 | contentio.Reader / Writer 与 `[]format.RelatedRef` 的具体接口 |
| `meta_item.name/full_name/item_type` 的来源规则 | Manager 面向前端的 DTO 或 Transfer plan |
| `FormatRule` 如何声明 item 组织规则 | 具体格式的 parser、provider、reader 字段细节 |
| detector 如何裁决 item 边界 | `scan_depth`、`force`、`scanned_depth` 和任务触发策略 |

attributes 写入规则见 [ADDP 元数据 attributes 规范](addp元数据attributes规范.md)，扫描机制见 [ADDP 元数据扫描机制规范](addp元数据扫描机制规范.md)，格式与数据类型能力边界见 [ADDP 数据类型与格式能力规范](addp数据类型与格式能力规范.md)，读取抽象见 [ADDP 内容 I/O 抽象规范](addp内容IO抽象规范.md)。

## 核心结论

detector 不能等同于目录 detector，也不能按单一格式局部修补。detector 是从资源候选集合中识别 `0..N` 个 data item 的统一入口。

detector 只负责回答“哪些资源组成哪些 data item”。它可以给出 `layout`、`data_type`、`format`、primary content / ref 和 related refs，但不得把 node、目录、prefix 或容器内部对象直接等同于独立 meta item，除非规范明确声明。primary content 或 whole scope 根范围应成为 `meta_item.full_name`。

候选集合组织规则可以进入 `common/dataitem` 复用。Meta 仍拥有扫描调度、detector 编排、claims / exclusive 最终合并、`refs` 决策、attributes normalizer 和落库裁决。跨模块需要已入库 item 结果时应通过 Meta Client 消费 meta item，不得绕过 Meta 对外部目录、prefix 或 schema 重新落库。

## 扫描范围不是 item 边界

扫描范围是引擎提供的一批候选资源，例如：

- 文件系统目录下的文件和子目录。
- 对象存储 bucket / prefix 下的对象和子 prefix。
- 数据库 schema 下的表。
- 容器文件内部的表、图层或 sheet。
- API 或模块调用方显式提交的一组 content refs，例如 Transfer 本次写出的 Shapefile refs group。

扫描范围只回答“本轮能看见哪些资源”，不回答“这些资源整体是不是一个 item”。

`catalog_paths` 和 `ref_groups` 都只是组织候选集合的输入形态。前者表示按引擎 catalog path 枚举或定位范围，后者表示一组 content refs 的可见边界；二者都必须进入 Meta detector，由 Meta 统一裁决 data item 边界、claims、exclusive 和落库结果。Transfer、Manager、Asset、Search 等模块不得因为掌握一组 refs 就绕过 Meta 自行生成或合并 `meta_item`。

`catalog_paths`、`ref_groups.path` 以及进入 Meta scan 后的扫描期资源路径必须遵守 [ADDP 存储引擎路径体系规范](addp存储引擎路径体系规范.md) 的路径语义。尤其是对象存储场景：外部 `ref_groups.path` 使用 `bucket/object_key`，但 detector 候选资源进入对象存储资源规划层后应以 bucket 为 root、以 bucket 内 `object_key` 作为资源相对路径；不得把 `bucket/object_key` 再传给只接受 `object_key` 的 catalog mapper。

## 统一入口

新扫描流程必须使用：

```go
ResolveItems(scope) (*DetectionResult, error)
```

`ResolveDirectory` 不再作为新规范接口保留。实现阶段应删除旧调用或改为直接调用 `ResolveItems`，不得继续提供旧扫描语义兜底。

`ResolveItems` 的候选集合组织能力由 `common/dataitem` 承载。Meta 扫描流程必须通过该能力或等价封装识别外部范围内的 data item，并在 Meta 内完成最终裁决和落库。Manager、Transfer、Asset、Search 等模块不得对已入库外部 item 重新做目录级识别，也不得在提交 `ref_groups` 前预先判断 refs 是否构成某个格式的 data item。

Manager 可以在容器预览过程中调用 `common/dataitem` 组织容器内部 child。该结果只服务本次动态预览，用完即弃；不得自动升格为外部 `meta_item`，不得写回父容器 attributes，也不得替代 Meta 对外部资源范围的扫描裁决。

`common/dataitem` 的识别能力不得内置“一层容器”的限制。调用方可以把任意一层可见 content 转成候选集合后反复调用 `ResolveItems`，从而支持 ZIP 中 ZIP、ZIP 中 Shapefile 等多层结构。是否继续深入、深入到几层、是否把结果持久化，属于调用层策略，不属于 `common/dataitem`。

detector 不得通过 common 包级 `init()` 自动注册到全局 registry。Meta 应显式组装 detector 列表并校验其 `FormatRule`，以保证 item 识别流程的所有权清晰可追踪。

## common/dataitem 当前落点

`common/dataitem` 是候选集合组织规则的共享实现层，不是 Meta 落库流程的搬迁。

当前实现已经提供：

1. `Candidate`、`ResolveInput`、`ResolvedItem`、`ResolveResult` 等组织解析模型。
2. `ResolveItems()` 统一执行 multi ref 归并、whole scope 识别和 single fallback。
3. `BuiltinSingleResourceRules()`、`BuiltinMultiRules()`、`BuiltinWholeScopeRules()` 从 `common/format` capability 派生基础规则。
4. `DefaultIgnorePolicy` 过滤空名称、目录项、`.DS_Store` 和 `__MACOSX` 等系统噪声。
5. `ResolvedItem.RelatedRefs()` 将 multi refs 转换为格式层可消费的 `format.RelatedRef`。

`common/dataitem` 不负责扫描调度、递归遍历、任务状态、`meta_item` 落库、fingerprint、node 绑定、attributes normalizer、engine reader 构造、内容读取或 Manager 前端 DTO。Meta 扫描入口负责把 `ResolvedItem` 转成可落库 item；Manager 仅可在容器动态预览中临时消费解析结果。

调用层控制递归与成本。Meta 对外部资源范围调用 `common/dataitem` 后可以落库外层 data item；Manager 在容器预览中调用 `common/dataitem` 后只能返回预览 DTO。其它模块未来如需动态识别 container 内部结构，也应先明确使用场景、成本边界和结果生命周期，再复用该能力。

`common/dataitem` 也可以提供不涉及落库的纯 helper，用于从标准 item attributes 还原 item descriptor、content paths 或 related refs。这类 helper 只能解释已经由 Meta 写入的标准事实，不得访问 engine，不得重新探测格式，也不得扩展 item 边界。对已知 item 的 refresh，应把还原出的 refs / scope 作为 provider 的内容输入；不得把 `refs` 展开成 catalog scan target 后重新发现 item。

```go
type DetectionResult struct {
    Items     []*DetectedItem
    Claims    ResourceClaimSet
    Exclusive bool
}
```

- `Items`：本轮识别出的 data item。
- `Claims`：detector 已认领的源资源路径。已认领资源不再作为普通资源重复落 item。
- `Exclusive`：当前扫描范围整体已被一个 item 认领。仅 `layout=whole` 等明确场景允许使用；该范围内其他资源不得再生成独立 item。

## item 身份规则

detector 必须先确定 data item 边界，再提取类型信息、格式信息和横切能力。`meta_item` 表字段是 item 身份事实源，不得由 parser、provider、reader 或格式私有逻辑任意覆盖。

| 场景 | `meta_item.name` 来源 | `meta_item.full_name` 来源 | 说明 |
|---|---|---|---|
| `layout=single` 对象 / 文件资源 | 入口资源名，保留扩展名 | 入口资源完整路径 | MinIO / S3 中 `item_type=object`；NFS / 本地文件系统中 `item_type=file` |
| `layout=single` 引擎原生资源 | 引擎原生名称 | 引擎内唯一逻辑全名 | PostgreSQL table、MongoDB collection 等 |
| `layout=multi` | primary content 名，保留扩展名 | primary content 完整路径 | Shapefile 使用 `.shp` 作为 primary content；item_type 仍跟随承载引擎的叶子术语 |
| `layout=whole` | 根目录、prefix、schema 名，或格式规范定义的数据集名 | whole scope 根范围完整路径 | Iceberg 表目录、OSGB 场景目录等 |

规则：

1. primary content 或 whole scope 根范围最终写入 `meta_item.full_name`。
2. attributes 不再定义通用 `entry_path`。
3. `refs` 只表达 multi 或需要记录关键 ref 的 whole item 的 related refs，不替代 `full_name`。
4. 容器内部对象默认不生成独立 `meta_item`；只有对应规范明确声明后才可展开。
5. `meta_item.item_type` 跟随引擎 catalog / 路径模型的原生叶子术语，不因 `data_type`、`format` 或 Manager 预览方式改变。
6. 除非经过规范修订，不得改变 `meta_item.name/full_name/item_type` 的来源语义。

## 已入库 item 的再次消费

已入库 item 是 Meta detector 的裁决结果。Manager、Transfer 和 item refresh 都应消费该结果，而不是重新判断同级资源：

| layout | 消费方式 |
|---|---|
| `single` | 使用 primary content，即 `meta_item.full_name` 或 `attributes.storage.physical_path`。 |
| `multi` | 使用 `attributes.item.refs` 的完整 related refs 集合；`meta_item.full_name` 只表示 primary content。 |
| `whole` | 使用 whole scope 根范围，即 `meta_item.full_name` 或 `attributes.storage.physical_path`。 |

如果 `layout=multi` 的 item 缺失 refs、缺少唯一 primary、或 required refs 不完整，调用方不应尝试在 format provider 内部重新枚举 sibling content 来“修复”。正确做法是回到 node 层重新扫描，让 detector 重新裁决 item 边界和 refs。

item refresh 只允许刷新该 item 自身的 attributes、字段、format info、access index 和横切能力。若 item 本身识别错误，例如 Shapefile 的多个 ref 没有被归并为一个 item，应由 node 扫描重新识别，而不是 item refresh 扩大范围。

## 递归观察资源

`whole` 内容布局 detector 需要递归观察资源时，由扫描入口提供 `RecursiveFiles` / `RecursiveSubdirs`。格式 detector 只消费候选资源，不自行访问存储引擎递归拉取目录。

递归观察资源只表示 detector 可见的候选集合，不改变 item 身份生成规则。

## detector 分类

| 类别 | 典型来源 | 内容布局 | 独占范围 |
|---|---|---|---|
| Native item detector | 数据库表、动态 schema 记录集合、图整体 | `single` 或引擎规范声明 | 由引擎稳定 catalog 边界决定 |
| Single-resource detector | CSV、PDF、图片、SQLite、Excel、ZIP | `single` | 不独占目录 |
| Sibling multi-resource detector | Shapefile、主文件 + 索引文件 + 元数据文件 | `multi` | 只认领匹配 ref，不独占目录 |
| Whole-scope detector | Iceberg 表目录、OSGB 场景目录、完整数据集 prefix | `whole` | 强匹配时可独占扫描范围 |

容器文件是 `data_type=container`，不是单独内容布局。SQLite、GeoPackage、Excel、ZIP 等通常由 single-resource detector 识别为 `layout=single`、`data_type=container`，内部对象先写入 attributes。

Shapefile 必须归入 sibling multi-resource，不得作为 whole-scope detector。

`mixed_collection` 暂不作为基础 detector 类别。只认领部分资源时使用 `multi`；整体认领时使用 `whole`。未来确实出现无法表达的复杂内容布局，再单独修订规范。

## 识别优先级

统一入口按组织边界风险和资源吞吐影响设置优先级。推荐顺序：

1. Native item：由引擎直接给出边界。
2. Single container resource：先确定容器文件自身 item，内部子对象不自动升格为 meta item。
3. Sibling multi-resource：先归并同级 refs，避免 `.shp` 被当普通文件。
4. Whole-scope：判断当前目录、prefix、schema 或扫描范围是否整体构成 item。
5. Residual single-resource：未被认领的资源按 single item 处理。
6. Recursive children：未被独占的子目录或子 prefix 继续递归。

优先级不是格式优先级。任何 detector 都不得因为格式匹配成功就默认吞掉整个扫描范围。

## FormatRule

格式实现层通过规则声明一次性回答自己的内容布局、数据类型、primary content / ref、related refs 和优先级：

```go
type FormatRule struct {
    Format       string
    DataType     DataType
    Layout Layout
    Priority     int

    Entry           EntryRule
    Refs            *RefRule
    Container       *ContainerRule
    WholeScope      *WholeScopeRule
    RelatedRefSpecs []format.RelatedRefSpec
}
```

条件约束：

| `layout` | 必填规则 | 禁止或忽略规则 |
|---|---|---|
| `single` | `Entry` | `Refs`、`WholeScope` |
| `multi` | `Entry`、`Refs` 或 `RelatedRefSpecs` | `Container`、`WholeScope` |
| `whole` | `WholeScope` | `Entry`、`Refs`、`Container` |

`ContainerRule` 只描述容器型数据的内部枚举、默认入口和内部子 item 表达方式，不改变 `layout`。一个容器文件自身仍应按 `single` 内容布局生成外层 data item。

统一入口只负责提供候选资源、执行优先级、合并结果和处理 claimed resources。一套文件到底需要哪些后缀、primary ref 是哪一个、可选 refs 有哪些，必须由格式实现层声明。primary content 或 whole scope 根范围最终应写入 `meta_item.full_name`，不得再写入通用 `attributes.item.entry_path`。

## claims 与 exclusive 规则

`Claims` 表达 detector 已经认领的源资源，扫描入口必须用它避免重复落 item。

| 场景 | Claims | Exclusive |
|---|---|---|
| `single` | 入口资源 | 通常为 `false` |
| `multi` | primary content + 已匹配 related refs | 必须为 `false`，不得独占目录或 prefix |
| `whole` | whole scope 根范围和规范要求的关键资源 | 强匹配时可为 `true` |
| 容器内部对象 | 默认不进入外部扫描 claims | 不影响外层扫描范围 |

`exclusive=true` 只允许用于明确的 whole scope 场景。弱匹配、仅扩展名匹配、范围内存在未认领异类资源等情况不得直接独占扫描范围；whole scope 的 explain / confidence、manifest 规则等尚未定稿内容进入对应计划文档或格式后续事项，不在本规范内展开。

## Shapefile 校准用例

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

1. `/shp/` 是 node，不是 Shapefile item。
2. `farmland.*` 生成一个 `data_type=table`、`layout=multi`、`format=shapefile` item。
3. `roads.*` 生成另一个 `data_type=table`、`layout=multi`、`format=shapefile` item。
4. `readme.pdf` 生成 `data_type=document`、`layout=single` item。
5. `raw.csv` 生成 `data_type=table`、`layout=single` item。
6. 两个 Shapefile item 的 `full_name` 分别来自入口文件全路径。
7. Manager 中两个 Shapefile、PDF、CSV 都挂在 `/shp/` 目录下。
8. Shapefile 内容读取使用 `meta_item.full_name` 和 `item.refs`，不得重新枚举 sibling 后猜测。

## TIFF / GeoTIFF 校准用例

TIFF / GeoTIFF 的主资源是 `.tif` 或 `.tiff`。当同 basename 存在空间辅助文件时，应按 sibling multi-resource 归并为同一个 data item，而不是把 `.tfw`、`.hdr`、`.aux.xml`、`.ovr` 等分别落成独立空间 item。

支持的第一阶段 related refs 白名单：

| 扩展名 | 角色 | 必需 | 说明 |
|---|---|---|---|
| `.tif` / `.tiff` | primary | 是 | 主 TIFF / GeoTIFF 文件，`meta_item.full_name` 来源。 |
| `.tfw` / `.tifw` / `.wld` | world_file | 否 | 外部仿射变换参数。 |
| `.prj` | crs | 否 | 外部 CRS 定义。 |
| `.aux.xml` | auxiliary_metadata | 否 | GDAL / ESRI 辅助元数据。 |
| `.ovr` | overview | 否 | 外部 overview。 |
| `.hdr` | header | 否 | 栅格头文件或补充说明。 |

目录：

```text
/rasters/
  dem.tif
  dem.tfw
  dem.prj
  dem.aux.xml
  readme.txt
  thumbnail.png
```

期望：

1. `dem.tif`、`dem.tfw`、`dem.prj`、`dem.aux.xml` 生成一个 `data_type=media`、`layout=multi`、`format=tiff` item。
2. 该 item 的 `full_name` 来自 `dem.tif`。
3. `item.refs` 记录 `.tfw`、`.prj`、`.aux.xml` 等 related refs，供 format provider、基础预览和内容读取使用。
4. `readme.txt`、`thumbnail.png` 仍按自身格式独立识别。
5. 如果只有 `dem.tif` 且没有 sidecar，则生成 `layout=single`、`data_type=media`、`format=tiff` item。
6. COG 不是新的基础 `format`；是否是 COG 由 `format_info.tiff.profile` 或 Manager 的 `raster_cog` 生成结果表达。

TIFF related refs 归并必须对 NFS、MinIO、S3 等存储引擎保持一致。对象存储场景中，basename 匹配基于 bucket 内 object key 的同 prefix sibling，不得把 bucket 再拼入 detector 内部的相对路径进行二次匹配。

## Raster mosaic 校准用例

Raster mosaic 是 whole-scope 栅格镶嵌数据集，使用 `format=raster_mosaic`，不是单个 TIFF / COG，也不是 `format=tiff` 的 related refs 扩展。

最小目录：

```text
/mosaics/srtm/
  mosaic.addp.json
  index/
    source-index.json
  overviews/
    overview.cog.tif
  derived/
    leaf-cog/
      000001.cog.tif
```

期望：

1. `/mosaics/srtm/` 生成一个 `data_type=media`、`layout=whole`、`format=raster_mosaic` item。
2. 该 item 的 `full_name` 来自 whole scope 根范围 `/mosaics/srtm/`，不是 `mosaic.addp.json` 或某个 leaf COG。
3. `mosaic.addp.json` 是 manifest 主资源，应进入 `format_info.raster_mosaic.manifest_ref`。whole item 不应把几千个 leaf COG 展开写入 `item.refs`。
4. `index/`、`overviews/`、`derived/`、`tiles/`、`styles/`、`stats/` 等都是 mosaic item 内部组成，不默认生成同级 meta item。
5. leaf COG 默认作为 mosaic 内部 leaf 查看对象，不自动升格为同级 Meta item；如用户需要治理某个 leaf COG，应单独扫描其所在范围。
6. `in_place` 生成时，源 node 内已通过内容级校验的 COG 可作为 mosaic leaf 被 index 引用；`detached` 生成时，leaf COG 必须位于目标 mosaic 数据集内，不应长期依赖源 node。detector 不得只因后缀或文件名判断 COG 合规。
7. `exclusive=true` 只在 manifest 规则强匹配且 whole scope 成立时使用；弱匹配不得吞掉整个目录或 prefix。

`raster_mosaic` 的 whole-scope 识别至少要求存在 `mosaic.addp.json`，且 Meta 落库前必须读取 manifest 内容，确认 schema、`format=raster_mosaic`、`data_type=media` 和 `layout=whole`。只有 leaf COG、只有 `overview.cog.tif`，或只有同名但内容不匹配的 JSON 文件，都不构成 raster mosaic item。

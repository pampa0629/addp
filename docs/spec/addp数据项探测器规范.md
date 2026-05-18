# ADDP 数据项探测器规范

本文定义 ADDP 数据项探测器的设计边界、统一入口和格式规则声明方式。术语以 [ADDP 术语表](../concepts/addp术语表.md) 和 [ADDP 数据项体系图](../concepts/addp数据项体系图.md) 为准。

本文是 data item 组织方式识别、claims / exclusive 合并、`refs` 决策和 `FormatRule` 声明的唯一规范来源。扫描深度、覆盖策略、刷新机制和跨模块触发规则见 [ADDP 元数据扫描机制规范](addp元数据扫描机制规范.md)。其他文档如需引用 detector 规则，只保留链接和一句话摘要，不重复展开。

## 本文边界

本文只定义 data item 识别和资源认领规则：

| 本文负责 | 不在本文定义 |
|---|---|
| 扫描范围如何解析为 `0..N` 个 data item | `attributes` 的完整 JSON schema |
| `organization`、主资源、ref 资源和 whole scope 如何确定 | FormatPlugin、info provider、content reader 的接口形态 |
| `claims`、`exclusive` 如何合并 | contentio.Reader / contentio.MultiReader 的具体接口 |
| `meta_item.name/full_name/item_type` 的来源规则 | Manager 面向前端的 DTO 或 Transfer plan |
| `FormatRule` 如何声明 item 组织规则 | 具体格式的 parser、provider、reader 字段细节 |
| detector 如何裁决 item 边界 | `scan_depth`、`force`、`scanned_depth` 和任务触发策略 |

attributes 写入规则见 [ADDP 元数据 attributes 规范](addp元数据attributes规范.md)，扫描机制见 [ADDP 元数据扫描机制规范](addp元数据扫描机制规范.md)，格式与数据类型能力边界见 [ADDP 数据类型与格式能力规范](addp数据类型与格式能力规范.md)，读取抽象见 [ADDP 资源读取抽象规范](addp资源读取抽象规范.md)。

## 核心结论

detector 不能等同于目录 detector，也不能按单一格式局部修补。detector 是从资源候选集合中识别 `0..N` 个 data item 的统一入口。

detector 只负责回答“哪些资源组成哪些 data item”。它可以给出 `organization`、`data_type`、`format`、主资源和ref 资源，但不得把 node、目录、prefix 或容器内部对象直接等同于独立 meta item，除非规范明确声明。主资源或 whole scope 根范围应成为 `meta_item.full_name`。

候选集合组织规则可以进入 `common/dataitem` 复用。Meta 仍拥有扫描调度、detector 编排、claims / exclusive 最终合并、`refs` 决策、attributes normalizer 和落库裁决。跨模块需要已入库 item 结果时应通过 Meta Client 消费 meta item，不得绕过 Meta 对外部目录、prefix 或 schema 重新落库。

## 扫描范围不是 item 边界

扫描范围是引擎提供的一批候选资源，例如：

- 文件系统目录下的文件和子目录。
- 对象存储 bucket / prefix 下的对象和子 prefix。
- 数据库 schema 下的表。
- 容器文件内部的表、图层或 sheet。

扫描范围只回答“本轮能看见哪些资源”，不回答“这些资源整体是不是一个 item”。

## 统一入口

新扫描流程必须使用：

```go
ResolveItems(scope) (*DetectionResult, error)
```

`ResolveDirectory` 不再作为新规范接口保留。实现阶段应删除旧调用或改为直接调用 `ResolveItems`，不得继续提供旧扫描语义兜底。

`ResolveItems` 的候选集合组织能力由 `common/dataitem` 承载。Meta 扫描流程必须通过该能力或等价封装识别外部范围内的 data item，并在 Meta 内完成最终裁决和落库。Manager、Transfer、Asset、Search 等模块不得对已入库外部 item 重新做目录级识别。

Manager 可以在容器预览过程中调用 `common/dataitem` 组织容器内部 child。该结果只服务本次动态预览，用完即弃；不得自动升格为外部 `meta_item`，不得写回父容器 attributes，也不得替代 Meta 对外部资源范围的扫描裁决。

detector 不得通过 common 包级 `init()` 自动注册到全局 registry。Meta 应显式组装 detector 列表并校验其 `FormatRule`，以保证 item 识别流程的所有权清晰可追踪。

## common/dataitem 当前落点

`common/dataitem` 是候选集合组织规则的共享实现层，不是 Meta 落库流程的搬迁。

当前实现已经提供：

1. `Candidate`、`ResolveInput`、`ResolvedItem`、`ResolveResult` 等组织解析模型。
2. `ResolveItems()` 统一执行 multi ref 归并、whole scope 识别和 single fallback。
3. `BuiltinSingleResourceRules()`、`BuiltinMultiRules()`、`BuiltinWholeScopeRules()` 从 `common/format` capability 派生基础规则。
4. `DefaultIgnorePolicy` 过滤空名称、目录项、`.DS_Store` 和 `__MACOSX` 等系统噪声。
5. `ResolvedItem.ContentRefs()` 将 multi refs 转换为内容读取层可消费的 `contentio.Ref`。

`common/dataitem` 不负责扫描调度、递归遍历、任务状态、`meta_item` 落库、fingerprint、node 绑定、attributes normalizer、engine reader 构造、内容读取或 Manager 前端 DTO。Meta 扫描入口负责把 `ResolvedItem` 转成可落库 item；Manager 仅可在容器动态预览中临时消费解析结果。

```go
type DetectionResult struct {
    Items     []*DetectedItem
    Claims    ResourceClaimSet
    Exclusive bool
}
```

- `Items`：本轮识别出的 data item。
- `Claims`：detector 已认领的源资源路径。已认领资源不再作为普通资源重复落 item。
- `Exclusive`：当前扫描范围整体已被一个 item 认领。仅 `organization=whole` 等明确场景允许使用；该范围内其他资源不得再生成独立 item。

## item 身份规则

detector 必须先确定 data item 边界，再提取类型信息、格式信息和横切能力。`meta_item` 表字段是 item 身份事实源，不得由 parser、provider、reader 或格式私有逻辑任意覆盖。

| 场景 | `meta_item.name` 来源 | `meta_item.full_name` 来源 | 说明 |
|---|---|---|---|
| `organization=single` 对象 / 文件资源 | 入口资源名，保留扩展名 | 入口资源完整路径 | MinIO / S3 中 `item_type=object`；NFS / 本地文件系统中 `item_type=file` |
| `organization=single` 引擎原生资源 | 引擎原生名称 | 引擎内唯一逻辑全名 | PostgreSQL table、MongoDB collection 等 |
| `organization=multi` | 主资源名，保留扩展名 | 主资源完整路径 | Shapefile 使用 `.shp` 作为主资源；item_type 仍跟随承载引擎的叶子术语 |
| `organization=whole` | 根目录、prefix、schema 名，或格式规范定义的数据集名 | whole scope 根范围完整路径 | Iceberg 表目录、OSGB 场景目录等 |

规则：

1. 主资源或 whole scope 根范围最终写入 `meta_item.full_name`。
2. attributes 不再定义通用 `entry_path`。
3. `refs` 只表达 multi 或需要记录关键 ref的 whole item 的ref 资源，不替代 `full_name`。
4. 容器内部对象默认不生成独立 `meta_item`；只有对应规范明确声明后才可展开。
5. `meta_item.item_type` 跟随引擎 catalog / 路径模型的原生叶子术语，不因 `data_type`、`format` 或 Manager 预览方式改变。
6. 除非经过规范修订，不得改变 `meta_item.name/full_name/item_type` 的来源语义。

## 递归观察资源

`whole` 组织方式 detector 需要递归观察资源时，由扫描入口提供 `RecursiveFiles` / `RecursiveSubdirs`。格式 detector 只消费候选资源，不自行访问存储引擎递归拉取目录。

递归观察资源只表示 detector 可见的候选集合，不改变 item 身份生成规则。

## detector 分类

| 类别 | 典型来源 | 组织方式 | 独占范围 |
|---|---|---|---|
| Native item detector | 数据库表、文档集合、图 label / relationship | `single` 或引擎规范声明 | 由引擎原生边界决定 |
| Single-resource detector | CSV、PDF、图片、SQLite、Excel、ZIP | `single` | 不独占目录 |
| Sibling multi-resource detector | Shapefile、主文件 + 索引文件 + 元数据文件 | `multi` | 只认领匹配 ref，不独占目录 |
| Whole-scope detector | Iceberg 表目录、OSGB 场景目录、完整数据集 prefix | `whole` | 强匹配时可独占扫描范围 |

容器文件是 `data_type=container`，不是单独组织方式。SQLite、GeoPackage、Excel、ZIP 等通常由 single-resource detector 识别为 `organization=single`、`data_type=container`，内部对象先写入 attributes。

Shapefile 必须归入 sibling multi-resource，不得作为 whole-scope detector。

`mixed_collection` 暂不作为基础 detector 类别。只认领部分资源时使用 `multi`；整体认领时使用 `whole`。未来确实出现无法表达的复杂组织方式，再单独修订规范。

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

格式实现层通过规则声明一次性回答自己的组织方式、数据类型、主资源、refs和优先级：

```go
type FormatRule struct {
    Format       string
    DataType     DataType
    Organization Organization
    Priority     int

    Entry           EntryRule
    Refs            *RefRule
    Container       *ContainerRule
    WholeScope      *WholeScopeRule
    RelatedRefSpecs []contentio.RelatedRefSpec
}
```

条件约束：

| `organization` | 必填规则 | 禁止或忽略规则 |
|---|---|---|
| `single` | `Entry` | `Refs`、`WholeScope` |
| `multi` | `Entry`、`Refs` 或 `RelatedRefSpecs` | `Container`、`WholeScope` |
| `whole` | `WholeScope` | `Entry`、`Refs`、`Container` |

`ContainerRule` 只描述容器型数据的内部枚举、默认入口和内部子 item 表达方式，不改变 `organization`。一个容器文件自身仍应按 `single` 组织方式生成外层 data item。

统一入口只负责提供候选资源、执行优先级、合并结果和处理 claimed resources。一套文件到底需要哪些后缀、主文件是哪一个、可选 refs有哪些，必须由格式实现层声明。主文件或 whole scope 根范围最终应写入 `meta_item.full_name`，不得再写入通用 `attributes.item.entry_path`。

## claims 与 exclusive 规则

`Claims` 表达 detector 已经认领的源资源，扫描入口必须用它避免重复落 item。

| 场景 | Claims | Exclusive |
|---|---|---|
| `single` | 入口资源 | 通常为 `false` |
| `multi` | 主资源 + 已匹配ref 资源 | 必须为 `false`，不得独占目录或 prefix |
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
2. `farmland.*` 生成一个 `data_type=table`、`organization=multi`、`format=shapefile` item。
3. `roads.*` 生成另一个 `data_type=table`、`organization=multi`、`format=shapefile` item。
4. `readme.pdf` 生成 `data_type=document`、`organization=single` item。
5. `raw.csv` 生成 `data_type=table`、`organization=single` item。
6. 两个 Shapefile item 的 `full_name` 分别来自入口文件全路径。
7. Manager 中两个 Shapefile、PDF、CSV 都挂在 `/shp/` 目录下。
8. Shapefile 内容读取使用 `meta_item.full_name` 和 `item.refs`，不得重新枚举 sibling 后猜测。

# ADDP 数据项 detector 规范

本文定义 ADDP 数据项 detector 的设计边界、统一入口和格式规则声明方式。术语以 [ADDP 数据类型与格式体系图](addp数据类型与格式体系图.md) 为准。

## 核心结论

detector 不能等同于目录 detector，也不能按单一格式局部修补。detector 是从资源候选集合中识别 `0..N` 个 data item 的统一入口。

detector 只负责回答“哪些资源组成哪些 data item”。它可以给出 `organization`、`data_type`、`format`、主资源和组件资源，但不得把 node、目录、prefix 或容器内部对象直接等同于独立 meta item，除非规范明确声明。主资源或 whole scope 根范围应成为 `meta_item.full_name`。

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

## 递归观察资源

`whole` 组织方式 detector 需要递归观察资源时，由扫描入口提供 `RecursiveFiles` / `RecursiveSubdirs`。格式 detector 只消费候选资源，不自行访问存储引擎递归拉取目录。

递归观察资源只表示 detector 可见的候选集合，不改变 item 身份生成规则。

## detector 分类

| 类别 | 典型来源 | 组织方式 | 独占范围 |
|---|---|---|---|
| Native item detector | 数据库表、文档集合、图 label / relationship | `single` 或引擎规范声明 | 由引擎原生边界决定 |
| Single-resource detector | CSV、PDF、图片、SQLite、Excel、ZIP | `single` | 不独占目录 |
| Sibling multi-resource detector | Shapefile、主文件 + 索引文件 + 元数据文件 | `multi` | 只认领匹配组件，不独占目录 |
| Whole-scope detector | Iceberg 表目录、OSGB 场景目录、完整数据集 prefix | `whole` | 强匹配时可独占扫描范围 |

容器文件是 `data_type=container`，不是单独组织方式。SQLite、GeoPackage、Excel、ZIP 等通常由 single-resource detector 识别为 `organization=single`、`data_type=container`，内部对象先写入 attributes。

Shapefile 必须归入 sibling multi-resource，不得作为 whole-scope detector。

`mixed_collection` 暂不作为基础 detector 类别。只认领部分资源时使用 `multi`；整体认领时使用 `whole`。未来确实出现无法表达的复杂组织方式，再单独修订规范。

## 识别优先级

统一入口按组织边界风险和资源吞吐影响设置优先级。推荐顺序：

1. Native item：由引擎直接给出边界。
2. Single container resource：先确定容器文件自身 item，内部子对象不自动升格为 meta item。
3. Sibling multi-resource：先归并同级组件，避免 `.shp` 被当普通文件。
4. Whole-scope：判断当前目录、prefix、schema 或扫描范围是否整体构成 item。
5. Residual single-resource：未被认领的资源按 single item 处理。
6. Recursive children：未被独占的子目录或子 prefix 继续递归。

优先级不是格式优先级。任何 detector 都不得因为格式匹配成功就默认吞掉整个扫描范围。

## FormatRule

格式实现层通过规则声明一次性回答自己的组织方式、数据类型、主资源、组件和优先级：

```go
type FormatRule struct {
    Format       string
    DataType     DataType
    ItemType     string
    Organization Organization
    Priority     int

    Entry      EntryRule
    Components *ComponentRule
    Container  *ContainerRule
    WholeScope *WholeScopeRule
}
```

条件约束：

| `organization` | 必填规则 | 禁止或忽略规则 |
|---|---|---|
| `single` | `Entry` | `Components`、`WholeScope` |
| `multi` | `Entry`、`Components` | `Container`、`WholeScope` |
| `whole` | `WholeScope` | `Entry`、`Components`、`Container` |

`ContainerRule` 只描述容器型数据的内部枚举、默认入口和内部子 item 表达方式，不改变 `organization`。一个容器文件自身仍应按 `single` 组织方式生成外层 data item。

统一入口只负责提供候选资源、执行优先级、合并结果和处理 claimed resources。一套文件到底需要哪些后缀、主文件是哪一个、可选组件有哪些，必须由格式实现层声明。主文件或 whole scope 根范围最终应写入 `meta_item.full_name`，不得再写入通用 `attributes.item.entry_path`。

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
8. Shapefile 预览使用 `meta_item.full_name` 和 `item.component_files`，不得重新枚举 sibling 后猜测。

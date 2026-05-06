# ADDP 内置数据格式规范

本文定义 ADDP 首批内置文件格式在 meta item 中的组织方式、数据类型、字段来源和 attributes 写入规则。通用 attributes 规则见 [ADDP 元数据 attributes 规范](addp元数据attributes规范.md)。

## CSV / TSV

| 维度 | CSV | TSV |
|---|---|---|
| `data_type` | `table` | `table` |
| `format` | `csv` | `tsv` |
| `organization` | `single` | `single` |

字段名来自表头；无表头时由 parser 生成稳定列名。字段类型来自采样推断。字段、行数、采样信息进入 `type_info.table`。编码、分隔符、表头判断进入 `format_info.csv` / `format_info.tsv`，统计和画像摘要进入 `capabilities.statistics`。

Manager 不得按扩展名二次猜测分隔符；必须使用 Meta 标准识别结果和 parser 输出。

## Excel

| 维度 | 取值 |
|---|---|
| `data_type` | `container` |
| `format` | `excel` |
| `organization` | `single` |

当前阶段 Excel 文件先作为一个容器 item。默认工作表、sheet 数量、内部 sheet 列表、表头判断、采样策略进入 `type_info.container` 和 `format_info.excel`。

Excel 的内部 sheet 是内部子 item，应先在 attributes 中表达。是否按 sheet 展开为独立 meta item 属于后续规范事项，未形成规范前不得改变 Manager / Transfer 路由语义。

## GeoJSON

| 维度 | 取值 |
|---|---|
| `data_type` | `table` |
| `format` | `geojson` |
| `organization` | `single` |

GeoJSON FeatureCollection 是空间矢量表。`FeatureCollection.features[].properties` 用于推断属性字段，`Feature.geometry` 用于补充平台统一几何字段。字段、行数进入 `type_info.table`，空间能力进入 `capabilities.spatial`，GeoJSON 私有信息进入 `format_info.geojson`。

`.json` 不能直接等同于 `geojson`，必须验证 GeoJSON 结构。records array、JSON Lines 可识别为 `data_type=table`、`format=json`；任意 JSON 对象、配置文件、嵌套文档应按平台消费方式识别为 `document` 或 `container`。

## Shapefile

| 维度 | 取值 |
|---|---|
| `data_type` | `table` |
| `format` | `shapefile` |
| `organization` | `multi` |
| 入口文件 | `.shp` |

Shapefile 是空间矢量表，不是单个 `.shp` 文件。`.shp`、`.shx`、`.dbf` 必须共同构成一个 item；`.prj`、`.cpg`、`.sbn`、`.sbx` 等为可选组件。

组件规则由 Shapefile 格式实现层声明：

- 必需组件：`.shp`、`.shx`、`.dbf`。
- 可选组件：`.prj`、`.cpg`、`.sbn`、`.sbx`。
- 入口文件：`.shp`。
- 组件匹配：同目录或同 prefix 下相同 basename。
- 不允许跨目录递归匹配。
- 不独占目录。

`type_info.table.fields` 必须来自真实文件解析：

- `.dbf` 提供非空间字段。
- `.shp` 提供平台统一几何字段。
- 字段类型映射为 ADDP 通用字段类型，原始 DBF 类型保留在 `original_type`。
- 记录数来自 Shapefile 记录数，不写固定占位值。

标准写入示例：

```json
{
  "schema_version": 1,
  "storage": {
    "physical_path": "/shp/",
    "total_size": 3069403
  },
  "item": {
    "organization": "multi",
    "data_type": "table",
    "format": "shapefile",
    "entry_path": "/shp/farmland.shp",
    "component_files": [
      "/shp/farmland.dbf",
      "/shp/farmland.prj",
      "/shp/farmland.shp",
      "/shp/farmland.shx"
    ],
    "file_count": 4
  },
  "type_info": {
    "table": {
      "fields": [
        {
          "name": "geometry",
          "type": "geometry",
          "original_type": "Polygon",
          "nullable": false
        }
      ],
      "row_count": 1234,
      "primary_key": []
    }
  },
  "format_info": {
    "shapefile": {
      "base_name": "farmland",
      "component_extensions": ["dbf", "prj", "shp", "shx"],
      "has_prj": true,
      "has_cpg": false,
      "shape_type": "Polygon"
    }
  },
  "capabilities": {
    "spatial": {
      "geometry_column": "geometry",
      "geometry_type": "Polygon",
      "srid": 0,
      "extent": null,
      "dimension": 2,
      "has_spatial_index": false
    }
  }
}
```

`base_name`、`component_extensions`、`has_prj`、`has_cpg` 不得写入 attributes 顶层，也不得长期写入 `format_info.unqualified`。Manager 预览必须使用 `item.entry_path` 和 `item.component_files`。

## Parquet / ORC / Avro / Iceberg

| 场景 | `data_type` | `format` | `organization` |
|---|---|---|---|
| 单个 Parquet 文件 | `table` | `parquet` | `single` |
| 一组明确归并的 sibling Parquet 文件 | `table` | `parquet` | `multi` |
| ORC / Avro 单文件 | `table` | `orc` / `avro` | `single` |
| Iceberg 表目录 | `table` | `iceberg` | `whole` |

Parquet、ORC、Avro 是表格型数据的文件格式，不应直接称为“湖表”。一组同类 Parquet 文件只有在有明确组件规则或 manifest 规则时才能归并为 `multi` item。Iceberg 等表格式目录由规范声明后可作为 `organization=whole` 的 table item。

`whole` item 的 `component_files` 只包含规范认定的数据文件或 manifest 关键资源，不包含 `_SUCCESS`、`_metadata`、`_common_metadata`、CRC 等辅助文件，除非具体格式规范另有说明。多个独立 sibling Parquet 文件不得被误合成一个 whole item。

分区字段来自目录结构或表格式元数据解析，应进入 `capabilities.partitioning`；字段和行数进入 `type_info.table`；格式私有信息进入 `format_info.<format>`。

## SQLite / GeoPackage

| 格式 | `data_type` | `format` | `organization` |
|---|---|---|---|
| SQLite | `container` | `sqlite` | `single` |
| GeoPackage | `container` | `geopackage` | `single` |

容器文件本身先作为一个 item；`item.entry_path` 指向容器文件。内部表、图层、sheet 先写入 `type_info.container.children`。是否展开为子 meta item 属于后续规范事项。

SQLite 版本、内部表数量、表清单等格式私有信息进入 `format_info.sqlite`。GeoPackage 的 layer 清单、gpkg 元数据等进入 `format_info.geopackage`；空间能力写入 `capabilities.spatial`，不得混入格式私有字段。

## 图片

| 维度 | 取值 |
|---|---|
| `data_type` | `media` |
| `format` | `jpeg`、`png`、`gif`、`tiff`、`image` |
| `organization` | `single` |

图片没有 `type_info.table.fields`。媒体种类写入 `type_info.media.kind=image`，宽高、颜色模式、方向等进入 `type_info.media`。如图片包含 GPS，可同步写入 `capabilities.spatial`，但不得把所有图片都视为空间数据。GeoTIFF 的空间语义需另行补充空间影像规范。

## PDF

| 维度 | 取值 |
|---|---|
| `data_type` | `document` |
| `format` | `pdf` |
| `organization` | `single` |

PDF 没有表格型 `type_info.table.fields`。页数、标题、作者、加密状态等进入 `type_info.document`；文本预览和全文提取状态进入 `capabilities.extraction`。大文本不得直接写入 attributes，只允许写预览、摘要或外部索引引用。

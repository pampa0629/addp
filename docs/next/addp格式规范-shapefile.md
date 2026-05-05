# ADDP 格式规范：Shapefile

更新时间：2026-05-05

本文定义 Shapefile 在 ADDP meta item 中的组合形态、字段来源和 attributes 写入规则。通用 attributes 分区规则见 `docs/next/addp数据类型与文件格式落地指南.md`。

## 一、格式定位

| 维度 | 取值 |
|---|---|
| `item_type` | `table` |
| `data_family` | `tabular` |
| `format` | `shapefile` |
| `composition_type` | `multi_file` |
| 入口文件 | `.shp` |

Shapefile 是空间矢量表，不是单个 `.shp` 文件。`.shp`、`.shx`、`.dbf` 必须共同构成一个 item；`.prj`、`.cpg`、`.sbn`、`.sbx` 等为可选组件。

## 二、字段来源

`attributes.schema.fields` 必须来自真实文件解析：

- `.dbf` 属性表提供非空间字段。
- `.shp` 几何记录提供平台统一几何字段。
- 字段类型应映射为 ADDP 通用字段类型，原始 DBF 类型保留在 `original_type`。
- 记录数来自 Shapefile 记录数，不应写固定占位值。

几何字段命名由 parser 统一决定，当前可使用 `geometry`；后续如需要保留源字段名或用户自定义字段名，应在空间扩展规范中统一，不得在各格式中各自硬编码。

## 三、标准 attributes 结构

示例：

```json
{
  "schema_version": 1,
  "storage": {
    "physical_path": "/shp/",
    "total_size": 3069403
  },
  "item": {
    "composition_type": "multi_file",
    "data_family": "tabular",
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
  "schema": {
    "fields": [
      {
        "name": "geometry",
        "type": "geometry",
        "original_type": "Polygon",
        "nullable": false,
        "comment": "Shapefile geometry field"
      },
      {
        "name": "id",
        "type": "integer",
        "original_type": "N",
        "nullable": true
      },
      {
        "name": "land_type",
        "type": "string",
        "original_type": "C",
        "nullable": true
      }
    ],
    "row_count": 1234,
    "primary_key": []
  },
  "extensions": {
    "spatial": {
      "geometry_column": "geometry",
      "geometry_type": "Polygon",
      "srid": 0,
      "extent": null,
      "dimension": 2,
      "has_spatial_index": false
    },
    "builtin.shapefile": {
      "base_name": "farmland",
      "component_extensions": ["dbf", "prj", "shp", "shx"],
      "has_prj": true,
      "has_cpg": false,
      "encoding": null,
      "dbf_version": null,
      "shape_type": "Polygon"
    }
  }
}
```

示例中的 `id`、`land_type`、`row_count` 只说明结构，不代表固定字段。

## 四、扩展归属

`extensions.spatial` 保存平台标准空间能力：

- `geometry_column`
- `geometry_type` 或 `geometry_types`
- `srid`
- `extent`
- `dimension`
- `has_spatial_index`
- `index_name`

`extensions.builtin.shapefile` 保存 Shapefile 私有诊断和格式信息：

- `base_name`
- `component_extensions`
- `has_prj`
- `has_cpg`
- `encoding`
- `dbf_version`
- `shape_type`

`base_name`、`component_extensions`、`has_prj`、`has_cpg` 不得写入 attributes 顶层，也不得长期写入 `extensions.unqualified`。

## 五、实现要求

1. detector 只负责归并组件和写入 `attributes.item` / `attributes.storage` 所需信息。
2. parser 负责从 `.dbf` 和 `.shp` 提取 `schema.fields`、`schema.row_count` 和 `extensions.spatial`。
3. 格式私有字段写入 `extensions.builtin.shapefile`。
4. `attributes` 顶层不得出现 `format`、`mode`、`entry_path`、`file_count`、`total_size`、`physical_path` 等重复字段。
5. `manager` 预览必须使用 `item.entry_path` 和 `item.component_files`，不得枚举 sibling 后重新推断一套 Shapefile。

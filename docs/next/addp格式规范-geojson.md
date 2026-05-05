# ADDP 格式规范：GeoJSON

更新时间：2026-05-05

本文定义 GeoJSON 在 ADDP meta item 中的 attributes 写入规则。通用规则见 `docs/next/addp数据类型与文件格式落地指南.md`。

## 一、格式定位

| 维度 | 取值 |
|---|---|
| `item_type` | `table` 或 `file` 迁移期兼容 |
| `data_family` | `tabular` |
| `format` | `geojson` |
| `composition_type` | `single_file` |

GeoJSON 是空间矢量表。平台级行为应通过 `data_family=tabular` 和 `extensions.spatial` 判断空间能力，不得把空间作为数据家族。

## 二、字段来源

- `FeatureCollection.features[].properties` 用于推断属性字段。
- `Feature.geometry` 用于补充平台统一几何字段。
- 字段类型可通过采样推断，采样策略应进入 `extensions.statistics` 或格式私有命名空间。
- Feature 数量可写入 `schema.row_count`；如仅采样得到，应标明采样来源。

## 三、扩展归属

`extensions.spatial` 保存几何列、几何类型、SRID、bbox、维度等平台标准空间字段。

`extensions.builtin.geojson` 保存 GeoJSON 私有信息：

- `geojson_type`
- `feature_count`
- `has_bbox`
- `crs`
- `sample_size`

## 四、实现要求

1. `.json` 不能直接等同于 `geojson`，必须验证 GeoJSON 结构。
2. `schema.fields` 不能只根据第一条 Feature 决定，应支持采样合并。
3. 格式私有字段不得写入顶层或 `extensions.unqualified`。

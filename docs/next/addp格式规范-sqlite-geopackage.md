# ADDP 格式规范：SQLite / GeoPackage

更新时间：2026-05-05

本文定义 SQLite 与 GeoPackage 容器文件在 ADDP meta item 中的 attributes 写入规则。

## 一、格式定位

| 格式 | `data_family` | `format` | `composition_type` |
|---|---|---|---|
| SQLite | `tabular` | `sqlite` | `container_file` |
| GeoPackage | `tabular` | `geopackage` | `container_file` |

容器文件本身是一个 item；是否展开内部表为子 item 属于后续规范事项。

## 二、字段来源

- 容器级 item 可保存内部表清单和默认入口表。
- 如果展开内部表，字段应来自 SQLite catalog / GeoPackage 元数据表。
- GeoPackage 空间表必须写入 `extensions.spatial`。

## 三、扩展归属

`extensions.builtin.sqlite` 保存 SQLite 私有信息：

- `sqlite_version`
- `table_count`
- `tables`
- `view_count`

`extensions.builtin.geopackage` 保存 GeoPackage 私有信息：

- `gpkg_version`
- `contents`
- `geometry_columns`
- `tile_matrix_sets`

## 四、实现要求

1. 容器文件不得伪装为目录树或多文件 item。
2. `item.entry_path` 指向容器文件。
3. 内部表展开策略必须先形成规范，再影响 Manager / Transfer 路由。

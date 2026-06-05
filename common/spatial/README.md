# common/spatial

`common/spatial` 承载 PostGIS 空间 SQL 表达式、MVT、SRID / CRS 事实解析、WKT / WKB / EWKB 几何编码等空间数据通用工具。

ADDP 核心后端不提供通用 CRS transform 能力，`common/spatial` 不再内置 `cgo + PROJ` executor，也不提供面向普通预览的 CRS transform facade。普通 Manager 预览应返回源坐标 geometry 与 CRS 元数据，由前端预览层决定是否可转换和渲染。

PostGIS 相关工具包括：

- 引擎类型判断和连接池获取
- 标识符引用和 PostGIS 表名拼接
- WKT / GeoJSON 源坐标表达式
- `geom.T` 与 WKT、WKB、EWKB、hex WKB / EWKB 之间的通用转换
- MVT、GeoJSON 分页、范围、SRID、物化视图和 GIST 索引 SQL 构造

跨引擎 SQL 方言差异属于 `common/sqldialect`；PostGIS 这类空间扩展能力属于本包。

格式 native 几何类型不属于本包。例如 Shapefile 的 `shp.Shape` 到 `geom.T` 的转换留在 `common/format/plugins/shapefile` 内部；本包只接收通用 `geom.T` 或标准编码值。

## CRS transform 边界

- 普通 Manager / Meta / Transfer / Service 后端不通过 `common/spatial` 做通用 CRS transform。
- PostGIS MVT、物化视图、工作流引擎等明确归属于具体引擎或运行环境的路径，可以使用该引擎自身的 CRS transform 能力。
- SRID=0 表示 CRS 未知，不得当作 `EPSG:4326` 或可直接渲染处理。
- GeoJSON / geometry 普通预览使用源坐标表达，并通过 `source_srid`、`source_crs`、`transform_status`、`preview_hint` 说明消费状态。

`common/spatial` 不依赖 `common/duckdb`。DuckDB spatial 扩展属于 DuckDB 自身能力，不作为通用空间转换 fallback。

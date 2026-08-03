# common/spatial

`common/spatial` 承载标准空间几何编码、SRID / CRS 事实解析、PostGIS 空间 SQL 表达式和 MVT 相关通用工具。

本包的核心边界：

- `EWKB` 是 ADDP 空间表跨 format / engine / workflow 的首选 geometry row encoding。
- `geom.T` 是本包内部在不同标准几何编码之间转换时使用的临时 Go 对象模型，不是 ADDP 跨层协议。
- 本包只处理标准几何编码，例如 WKT、WKB、EWKB、GeoJSON geometry、Arrow + EWKB，以及 FlatGeobuf / MVT 等渲染材料所需的标准几何编码辅助。
- 本包不处理任何 format native encoding，例如 Shapefile `shp.Shape`、GeoPackage geometry blob 或数据库 driver native geometry 值；native 到 EWKB 的转换归属对应 format plugin 或 engine provider。
- 本包不感知 Manager DTO、ResourceLocator、preview state、任务配置或 API 响应结构。

新增的 `geometry_batch_arrow` helper 用于承载 Arrow IPC + WKB 几何批的编解码，供工作流 direct 几何转换算子和 Transfer 批处理复用。

ADDP 核心后端不提供通用 CRS transform 能力，`common/spatial` 不再内置 `cgo + PROJ` executor，也不提供面向普通预览的 CRS transform facade。普通 Manager 预览应返回源坐标 geometry 与 CRS 元数据，由前端预览层决定是否可转换和渲染。

PostGIS 相关工具包括：

- 引擎类型判断和连接池获取
- 标识符引用和 PostGIS 表名拼接
- WKT / GeoJSON 源坐标表达式
- `geom.T` 与 WKT、WKB、EWKB、hex WKB / EWKB 之间的通用转换
- `DecodeGeometryValue(value, encoding, srid)` / `EncodeGeometryValue(geom, encoding, srid)` 标准 geometry row encoding 门面；`srid` 只写入或标注编码元数据，不执行 CRS transform
- MVT、GeoJSON 分页、范围、SRID、物化视图和 GIST 索引 SQL 构造

跨引擎 SQL 方言差异属于 `common/sqldialect`；PostGIS 这类空间扩展能力属于本包。

格式 native 几何类型不属于本包。例如 Shapefile 的 `shp.Shape` 到 `geom.T` / EWKB 的转换留在 `common/format/plugins/shapefile` 内部；GeoPackage geometry blob 到 EWKB 的转换留在 GeoPackage format plugin 内部。本包只接收通用 `geom.T` 或标准编码值。

Manager 快显不应直接 import `github.com/twpayne/go-geom`，也不应直接调用 FlatGeobuf format plugin。Manager 只编排空间行流、快显渲染源、受控 URL 和 artifact 生命周期；FlatGeobuf 快显材料的标准编码生成能力由 `common/spatial` 暴露，输入应是 EWKB geometry row encoding。

## CRS transform 边界

- 普通 Manager / Meta / Transfer / Service 后端不通过 `common/spatial` 做通用 CRS transform。
- PostGIS MVT、物化视图、工作流引擎等明确归属于具体引擎或运行环境的路径，可以使用该引擎自身的 CRS transform 能力。
- SRID=0 表示 CRS 未知，不得当作 `EPSG:4326` 或可直接渲染处理。
- GeoJSON / geometry 普通预览使用源坐标表达，并通过 `source_srid`、`source_crs`、`source_crs_definition`、`transform_status`、`preview_hint` 说明消费状态。

`common/spatial` 不依赖 DuckDB Runtime。DuckDB spatial 扩展属于 `engines/duckdb` 自身能力，不作为通用空间转换 fallback。

# common/spatial

`common/spatial` 负责空间预览场景下的坐标转换 facade，并承载 PostGIS 空间 SQL 表达式、MVT、WKB 等空间数据通用能力。

PostGIS 相关工具包括：

- 引擎类型判断和连接池获取
- 标识符引用和 PostGIS 表名拼接
- WKT / GeoJSON / 渲染用 GeoJSON 表达式
- MVT、GeoJSON 分页、范围、SRID、物化视图和 GIST 索引 SQL 构造

跨引擎 SQL 方言差异属于 `common/sqldialect`；PostGIS 这类空间扩展能力属于本包。

当前 executor 优先级：

1. `pure_go`
   仅处理 `EPSG:4326 <-> EPSG:3857`
2. `proj`
   通过 `libproj` 处理通用 `EPSG/WKT -> EPSG:4326`
3. `duckdb`
   作为过渡 fallback 保留

## 默认构建

默认不启用 PROJ executor。

- 不带 build tag 时：
  - `proj` executor 会编译为 stub
  - 通用 CRS 转换会继续回落到 `duckdb`

## 启用 PROJ

需要本机已安装 `libproj` 与对应 `proj.db`，并使用：

```bash
go test -tags proj ./spatial
go build -tags proj ./...
```

实现说明：

- bridge 位于 `common/spatial/internal/proj`
- 启用后使用 `proj_create_crs_to_crs`
- 会额外调用 `proj_normalize_for_visualization` 统一轴序
- 运行时会主动锁定当前 `pkg-config proj` 对应的数据目录，避免误用其他 PROJ 安装的旧 `proj.db`

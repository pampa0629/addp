"""
补充关键算子：数据源和输出算子
"""

from spatial.operator_registry import SPATIAL_OPERATORS, SpatialOperator, OperatorParam, ParamType

# ========================================
# 数据源算子（最重要！）
# ========================================

SPATIAL_OPERATORS.extend([
    SpatialOperator(
        code="load_from_postgis",
        name="从 PostGIS 加载",
        category="数据源",
        description="从 PostgreSQL/PostGIS 数据库表加载空间数据",
        input_params=[
            OperatorParam("data_source_id", ParamType.STRING,
                         description="数据源 ID（引用 ADDP system.resources）"),
            OperatorParam("table_name", ParamType.STRING,
                         description="表名"),
            OperatorParam("geom_field", ParamType.STRING, default="geom",
                         description="几何字段名"),
            OperatorParam("filter", ParamType.STRING, required=False,
                         description="SQL WHERE 条件（如 category='餐厅'）"),
            OperatorParam("limit", ParamType.INT, default=1000, required=False,
                         description="最大返回行数")
        ],
        output_type="table"  # 返回 GeoDataFrame 或记录列表
    ),

    SpatialOperator(
        code="load_from_geojson",
        name="加载 GeoJSON 文件",
        category="数据源",
        description="从上传的 GeoJSON 文件加载数据",
        input_params=[
            OperatorParam("file_path", ParamType.STRING,
                         description="文件路径（MinIO 或本地）"),
        ],
        output_type="geometry"
    ),

    SpatialOperator(
        code="load_from_wkt",
        name="加载 WKT 文本",
        category="数据源",
        description="从 WKT（Well-Known Text）格式加载几何对象",
        input_params=[
            OperatorParam("wkt_text", ParamType.WKT,
                         description="WKT 文本，如 POINT(116.404 39.915)"),
        ],
        output_type="geometry"
    ),

    # ========================================
    # 输出算子
    # ========================================

    SpatialOperator(
        code="save_to_postgis",
        name="保存到 PostGIS",
        category="输出",
        description="将分析结果保存到 PostgreSQL/PostGIS 表",
        input_params=[
            OperatorParam("input_geom", ParamType.GEOJSON, description="输入几何对象"),
            OperatorParam("data_source_id", ParamType.STRING,
                         description="目标数据源 ID"),
            OperatorParam("table_name", ParamType.STRING,
                         description="目标表名"),
            OperatorParam("if_exists", ParamType.STRING, default="append",
                         description="已存在时操作：append, replace, fail")
        ],
        output_type="metrics"  # 返回插入行数
    ),

    SpatialOperator(
        code="export_geojson",
        name="导出 GeoJSON",
        category="输出",
        description="导出为 GeoJSON 文件",
        input_params=[
            OperatorParam("input_geom", ParamType.GEOJSON, description="输入几何对象"),
            OperatorParam("output_path", ParamType.STRING,
                         description="输出文件路径（MinIO 或本地）"),
        ],
        output_type="url"  # 返回文件 URL
    ),

    SpatialOperator(
        code="render_map",
        name="渲染地图",
        category="输出",
        description="将结果渲染为交互式地图（Mapbox/Leaflet）",
        input_params=[
            OperatorParam("input_geom", ParamType.GEOJSON, description="输入几何对象"),
            OperatorParam("title", ParamType.STRING, required=False,
                         description="地图标题"),
            OperatorParam("style", ParamType.STRING, default='{"color": "blue", "weight": 2}',
                         description="样式 JSON")
        ],
        output_type="url"  # 返回地图 URL
    ),

    # ========================================
    # 批处理算子
    # ========================================

    SpatialOperator(
        code="batch_buffer",
        name="批量缓冲区",
        category="批处理",
        description="对数据表中所有几何对象批量创建缓冲区",
        input_params=[
            OperatorParam("table_ref", ParamType.TABLE_REF,
                         description="输入数据表引用"),
            OperatorParam("distance_field", ParamType.FIELD_REF,
                         description="缓冲距离字段（每行可不同距离）"),
            OperatorParam("geom_field", ParamType.FIELD_REF, default="geom",
                         description="几何字段名")
        ],
        output_type="table"
    ),

    SpatialOperator(
        code="batch_intersection",
        name="批量相交",
        category="批处理",
        description="计算左表中每个几何与右表的相交结果",
        input_params=[
            OperatorParam("left_table", ParamType.TABLE_REF, description="左表"),
            OperatorParam("right_table", ParamType.TABLE_REF, description="右表"),
            OperatorParam("left_geom_field", ParamType.FIELD_REF, default="geom"),
            OperatorParam("right_geom_field", ParamType.FIELD_REF, default="geom")
        ],
        output_type="table"
    ),

    # ========================================
    # 统计分析算子
    # ========================================

    SpatialOperator(
        code="spatial_aggregate",
        name="空间聚合统计",
        category="统计分析",
        description="按空间范围聚合统计",
        input_params=[
            OperatorParam("data_table", ParamType.TABLE_REF,
                         description="数据表"),
            OperatorParam("boundary_geom", ParamType.GEOJSON,
                         description="聚合边界（如行政区划）"),
            OperatorParam("agg_field", ParamType.FIELD_REF,
                         description="聚合字段"),
            OperatorParam("agg_function", ParamType.STRING, default="sum",
                         description="聚合函数：sum, count, avg, min, max")
        ],
        output_type="metrics"
    ),

    SpatialOperator(
        code="spatial_density",
        name="密度计算",
        category="统计分析",
        description="计算点要素的核密度估计（Kernel Density Estimation）",
        input_params=[
            OperatorParam("points_table", ParamType.TABLE_REF,
                         description="点要素表"),
            OperatorParam("bandwidth", ParamType.FLOAT,
                         description="核密度带宽（米）"),
            OperatorParam("grid_size", ParamType.INT, default=100,
                         description="栅格大小（像素）")
        ],
        output_type="geometry"  # 返回热力图栅格
    ),
])
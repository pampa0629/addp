"""
I/O 算子模块

提供数据输入输出算子：加载、保存
支持多种数据源：数据库表、文件、GeoJSON 对象
"""

import logging
import pandas as pd
import geopandas as gpd
from typing import Dict, Any
from sqlalchemy import create_engine, text
from .base import (
    OperatorType,
    OperatorMetadata, OperatorParam, OperatorCategory, register_operator
)

logger = logging.getLogger(__name__)


# ==================== 算子实现 ====================

def load(
    source_type: str,
    connection_info: Dict[str, Any] = None,
    schema: str = None,
    table: str = None,
    path: str = None,
    format: str = None,
    geojson: Dict[str, Any] = None,
    geom_column: str = None,
    **kwargs
):
    """
    通用数据加载算子

    参数由 Develop Backend 预处理：
    - connection_info: 数据库连接信息（已解密），包含 engine_type、host、port、user、password、database 等
    - source_type: "table" | "file" | "geojson" | "reference"
    - 其他参数: table/path、schema/format 等

    返回: DataFrame（普通表）或 GeoDataFrame（空间表）

    注意：此算子不再依赖 System API，所有连接信息由 Develop Backend 预处理后传入
    """
    if source_type == 'table':
        if not connection_info:
            raise ValueError("source_type=table 时必须提供 connection_info")

        # 从 connection_info 中提取信息（已由 Develop Backend 从 System API 获取并解密）
        engine_type = connection_info.get('engine_type')
        host = connection_info.get('host')
        port = connection_info.get('port')
        # 兼容 username 和 user 两种字段名
        user = connection_info.get('user') or connection_info.get('username')
        password = connection_info.get('password')
        database = connection_info.get('database')

        if not all([engine_type, host, port, user, database]):
            raise ValueError(f"connection_info 缺少必要字段: {connection_info}")

        # 根据不同数据库类型加载
        if engine_type in ['postgresql', 'PostgreSQL']:
            conn_str = f"postgresql://{user}:{password}@{host}:{port}/{database}"
            engine_db = create_engine(conn_str)

            # 读取空间数据
            sql = f'SELECT * FROM {schema}.{table}'

            # 先查询表结构，找出几何列
            logger.info(f"正在加载表: {schema}.{table}")

            # 检查表中的几何列
            check_geom_sql = f"""
                SELECT column_name, udt_name, data_type
                FROM information_schema.columns
                WHERE table_schema = '{schema}' AND table_name = '{table}'
                AND (udt_name = 'geometry' OR udt_name = 'geography' OR data_type LIKE '%geometry%')
            """
            with engine_db.connect() as conn:
                geom_cols = conn.execute(text(check_geom_sql)).fetchall()
                logger.info(f"表 {schema}.{table} 中的几何列: {geom_cols}")

            # 如果指定了几何列名则使用，否则让 geopandas 自动检测
            if geom_column:
                logger.info(f"使用指定的几何列: {geom_column}")
                gdf = gpd.read_postgis(sql, engine_db, geom_col=geom_column)
            elif geom_cols:
                # 如果找到了几何列，使用第一个
                detected_geom_col = geom_cols[0][0]
                logger.info(f"自动检测到几何列: {detected_geom_col}")
                gdf = gpd.read_postgis(sql, engine_db, geom_col=detected_geom_col)
            else:
                # 没有几何列，作为普通表加载（返回 DataFrame）
                logger.warning(f"表 {schema}.{table} 中没有几何列，作为普通 DataFrame 加载")
                gdf = pd.read_sql(sql, engine_db)

        elif engine_type in ['mysql', 'MySQL', 'doris', 'Doris']:
            if password:
                conn_str = f"mysql+pymysql://{user}:{password}@{host}:{port}/{database}"
            else:
                conn_str = f"mysql+pymysql://{user}@{host}:{port}/{database}"

            engine_db = create_engine(conn_str)

            # MySQL/Doris 空间数据读取
            sql = f'SELECT * FROM {table}'
            logger.info(f"正在加载表: {database}.{table}")

            # MySQL/Doris 检查几何列（简化版，因为 information_schema 结构不同）
            # 暂时先尝试常见的几何列名
            if geom_column:
                logger.info(f"使用指定的几何列: {geom_column}")
                gdf = gpd.read_postgis(sql, engine_db, geom_col=geom_column)
            else:
                # 先尝试不指定 geom_col，如果失败再作为普通表加载
                try:
                    logger.info("尝试自动检测几何列")
                    gdf = gpd.read_postgis(sql, engine_db)
                except Exception as e:
                    logger.warning(f"自动检测失败: {e}，作为普通 DataFrame 加载")
                    gdf = pd.read_sql(sql, engine_db)

        else:
            raise ValueError(f"Unsupported engine type for table: {engine_type}")

        return gdf

    elif source_type == 'geojson':
        # 直接解析 GeoJSON 对象
        if not geojson:
            raise ValueError("source_type=geojson 时必须提供 geojson 参数")
        gdf = gpd.GeoDataFrame.from_features(geojson['features'])
        return gdf

    elif source_type == 'reference':
        # 引用其他任务的输出（已在内存中的 GeoDataFrame）
        # 这种情况下不需要加载，直接返回引用
        raise NotImplementedError("Reference type should be handled by workflow engine")

    else:
        raise ValueError(f"Unsupported source_type: {source_type}")


def save(
    input_df,  # 支持 pd.DataFrame 和 gpd.GeoDataFrame
    target_type: str,
    connection_info: Dict[str, Any] = None,
    schema: str = None,
    table: str = None,
    mode: str = 'replace',
    path: str = None,
    format: str = None,
    **kwargs
) -> Dict[str, Any]:
    """
    通用数据保存算子

    参数由 Develop Backend 预处理：
    - input_df: 要保存的数据（支持 DataFrame 和 GeoDataFrame）
    - connection_info: 数据库连接信息（已解密）
    - target_type: "table" | "file"
    """
    if target_type == 'table':
        if not connection_info:
            raise ValueError("target_type=table 时必须提供 connection_info")

        # 从 connection_info 中提取信息
        engine_type = connection_info.get('engine_type')
        host = connection_info.get('host')
        port = connection_info.get('port')
        # 兼容 username 和 user 两种字段名
        user = connection_info.get('user') or connection_info.get('username')
        password = connection_info.get('password')
        database = connection_info.get('database')

        if not all([engine_type, host, port, user, database]):
            raise ValueError(f"connection_info 缺少必要字段: {connection_info}")

        # 根据数据库类型保存
        if engine_type in ['postgresql', 'PostgreSQL']:
            conn_str = f"postgresql://{user}:{password}@{host}:{port}/{database}"
            engine_db = create_engine(conn_str)

            if_exists = 'replace' if mode == 'replace' else 'append'

            # 判断是空间数据还是普通数据
            # 检查是否为 GeoDataFrame 且已设置几何列（通过 _geometry_column_name 属性）
            # 同时检查 geometry 列是否真实存在且非空
            has_geometry = (
                isinstance(input_df, gpd.GeoDataFrame) and
                input_df._geometry_column_name is not None and
                input_df._geometry_column_name in input_df.columns and
                not input_df[input_df._geometry_column_name].isna().all()
            )

            if has_geometry:
                # 使用 to_postgis 保存空间数据（支持任意几何列名）
                input_df.to_postgis(table, engine_db, schema=schema, if_exists=if_exists, index=False)
            else:
                # 使用 to_sql 保存普通数据（包括无 geometry 的 GeoDataFrame）
                # 如果是 GeoDataFrame 但无有效 geometry，转为 DataFrame
                if isinstance(input_df, gpd.GeoDataFrame):
                    import pandas as pd
                    df_to_save = pd.DataFrame(input_df.drop(columns=[input_df._geometry_column_name], errors='ignore'))
                    df_to_save.to_sql(table, engine_db, schema=schema, if_exists=if_exists, index=False)
                else:
                    input_df.to_sql(table, engine_db, schema=schema, if_exists=if_exists, index=False)

        elif engine_type in ['mysql', 'MySQL', 'doris', 'Doris']:
            if password:
                conn_str = f"mysql+pymysql://{user}:{password}@{host}:{port}/{database}"
            else:
                conn_str = f"mysql+pymysql://{user}@{host}:{port}/{database}"

            engine_db = create_engine(conn_str)

            if_exists = 'replace' if mode == 'replace' else 'append'
            # MySQL/Doris 使用 to_sql（即使是 GeoDataFrame 也先转换为 DataFrame）
            if isinstance(input_df, gpd.GeoDataFrame):
                # 转换几何列为 WKT 格式
                df_to_save = input_df.copy()
                if 'geometry' in df_to_save.columns:
                    df_to_save['geometry'] = df_to_save['geometry'].apply(lambda x: x.wkt if x else None)
                df_to_save.to_sql(table, engine_db, if_exists=if_exists, index=False)
            else:
                input_df.to_sql(table, engine_db, if_exists=if_exists, index=False)

        else:
            raise ValueError(f"Unsupported engine type for table: {engine_type}")

        return input_df

    else:
        raise ValueError(f"Unsupported target_type: {target_type}")


# ==================== 元数据定义 ====================

LOAD_METADATA = OperatorMetadata(
    name="load",
    type=OperatorType.GENERAL,
    category=OperatorCategory.DATA_IO,
    description="数据加载",
    brief_description="从数据库表或GeoJSON对象加载空间数据,支持多种数据源",

    overview="通用数据加载算子,支持从数据库表(PostgreSQL/MySQL/Doris)或内存GeoJSON对象加载空间数据。根据 source_type 参数自动选择加载方式。如需导入外部文件（如 Shapefile），请先通过 Manager 模块的导入数据功能将文件导入到数据库表，再通过本算子以 source_type=table 方式读取。",

    params=[
        OperatorParam(
            name="source_type",
            type="param",
            data_type="string",
            required=True,
            description="数据来源类型",
            notes="可选值: table(数据库表), geojson(GeoJSON对象)",
            enum=["table", "geojson"],
            default="table"
        ),
        # 特殊参数：数据源级联选择器（仅在 source_type=table 时显示）
        OperatorParam(
            name="数据源",
            type="ui",
            data_type="object",
            required=False,
            description="选择数据表（推荐使用此方式）",
            notes="使用可视化界面选择数据源，支持引擎→Schema→表的级联选择，自动填充下方参数",
            ui_type="data_source_cascader",
            ui_config={
                "api_base_url": "/api/meta",
                "engine_types": ["postgresql", "mysql", "doris", "clickhouse"],
                "selectable_node_types": ["table"],
                "enable_geometry_detection": True,
                "require_geometry": False
            },
            depends_on="source_type",
            show_when={"source_type": "table"}
        ),
        OperatorParam(
            name="engine_id",
            type="param",
            data_type="integer",
            required=False,
            description="存储引擎ID",
            notes="source_type=table 时必填。注意：此参数由 Develop Backend 自动转换为 connection_info（包含解密后的数据库连接信息），算子执行时直接使用 connection_info，无需调用 System API。",
            depends_on="source_type",
            show_when={"source_type": "table"}
        ),
        OperatorParam(
            name="schema",
            type="param",
            data_type="string",
            required=False,
            description="数据库schema名称",
            notes="仅 source_type=table 时有效，默认为 public",
            default="public",
            depends_on="source_type",
            show_when={"source_type": "table"}
        ),
        OperatorParam(
            name="table",
            type="param",
            data_type="string",
            required=False,
            description="数据库表名",
            notes="仅 source_type=table 时必填",
            depends_on="source_type",
            show_when={"source_type": "table"}
        ),
        OperatorParam(
            name="geojson",
            type="param",
            data_type="object",
            required=False,
            description="GeoJSON对象",
            notes="仅 source_type=geojson 时必填,必须是有效的 GeoJSON FeatureCollection",
            depends_on="source_type",
            show_when={"source_type": "geojson"}
        ),
        OperatorParam(
            name="geom_column",
            type="param",
            data_type="string",
            required=False,
            description="几何列名",
            notes="空间数据的几何列名。如果不指定，geopandas会自动检测几何列（推荐）。仅在自动检测失败或需要指定特定列时使用",
            default=None,
            depends_on="source_type",
            show_when={"source_type": "table"}
        )
    ],

    use_cases=[
        "从业务数据库加载河流数据: source_type=table, engine_id=1, table=rivers",
        "从内存GeoJSON加载临时数据: source_type=geojson, geojson={...}",
        "工作流起始节点: 作为数据输入的第一步",
        "加载通过 Manager 导入的 Shapefile: source_type=table, engine_id=5, schema=public, table=railway"
    ],

    notes=[
        "支持自动检测几何列,无需手动指定 geom_column (推荐)",
        "如果表中有多个几何列或自动检测失败,可通过 geom_column 参数指定",
        "如需从外部文件（Shapefile/GeoJSON）加载数据，请先通过 Manager 模块的导入数据功能将文件导入到数据库表，再通过本算子以 source_type=table 方式读取",
        "加载后的 GeoDataFrame 会保留所有属性字段",
        "engine_id 由 Develop Backend 自动转换为 connection_info，工作流引擎无需依赖 System API"
    ],

    workflow_example={
        'id': 'load_rivers',
        'operator': 'load',
        'params': {
            'source_type': 'table',
            'engine_id': 1,
            'schema': 'public',
            'table': 'rivers'
        },
        'depends_on': []
    }
)


SAVE_METADATA = OperatorMetadata(
    name="save",
    type=OperatorType.GENERAL,
    category=OperatorCategory.DATA_IO,
    description="数据保存",
    brief_description="将数据保存到数据库表,支持普通表和空间表",

    overview="通用数据保存算子,支持将 DataFrame 或 GeoDataFrame 保存到数据库表(PostgreSQL/MySQL/Doris)。根据 target_type 参数自动选择保存方式。",

    params=[
        OperatorParam(
            name="input_df",
            type="input",
            data_type="DataFrame",
            required=True,
            description="要保存的数据（支持 DataFrame 和 GeoDataFrame）"
        ),
        OperatorParam(
            name="target_type",
            type="param",
            data_type="string",
            required=True,
            description="保存目标类型",
            enum=["table"],
            default="table",
            notes="当前仅支持 table(数据库表)"
        ),
        OperatorParam(
            name="保存目标",
            type="ui",
            data_type="object",
            required=False,
            description="选择保存的数据库和表",
            ui_type="data_source_cascader",
            ui_config={
                "placeholder": "选择存储引擎 → Schema → 数据表",
                "auto_fill_params": ["engine_id", "schema", "table"],
                "allow_create_table": True  # 允许创建新表
            },
            depends_on="target_type",
            show_when={"target_type": "table"}
        ),
        OperatorParam(
            name="engine_id",
            type="param",
            data_type="integer",
            required=False,
            description="存储引擎ID",
            notes="由数据源选择器自动填充,对应 System 模块中的引擎ID。注意：此参数由 Develop Backend 自动转换为 connection_info（包含解密后的连接信息），算子执行时直接使用 connection_info。",
            depends_on="target_type",
            show_when={"target_type": "table"}
        ),
        OperatorParam(
            name="schema",
            type="param",
            data_type="string",
            required=False,
            description="数据库schema名称",
            notes="由数据源选择器自动填充",
            depends_on="target_type",
            show_when={"target_type": "table"}
        ),
        OperatorParam(
            name="table",
            type="param",
            data_type="string",
            required=False,
            description="数据库表名",
            notes="由数据源选择器自动填充或手动输入新表名",
            depends_on="target_type",
            show_when={"target_type": "table"}
        ),
        OperatorParam(
            name="mode",
            type="param",
            data_type="string",
            required=False,
            description="表已存在时的处理方式（必须从枚举值中选择）",
            enum=["replace", "append", "fail"],
            default="replace",
            notes="可选值: replace(替换表), append(追加数据), fail(抛出错误)。**重要**：只能使用这三个值之一，不能使用 overwrite 等其他值！",
            depends_on="target_type",
            show_when={"target_type": "table"}
        )
    ],

    use_cases=[
        "保存分析结果到数据库: target_type=table, engine_id=1, table=result, mode=replace",
        "工作流结束节点: 作为数据输出的最后一步"
    ],

    notes=[
        "自动识别输入数据类型（DataFrame 或 GeoDataFrame）",
        "保存空间数据到 PostgreSQL 时使用 PostGIS，自动创建几何索引",
        "保存普通数据使用标准 SQL 表",
        "mode=append 时要求表结构与数据结构一致",
        "engine_id 由 Develop Backend 自动转换为 connection_info，工作流引擎无需依赖 System API"
    ],

    workflow_example={
        'id': 'save_result',
        'operator': 'save',
        'params': {
            'input_df': {'$ref': 'task1'},  # 引用前一个任务的输出
            'target_type': 'table',
            'engine_id': 1,
            'schema': 'public',
            'table': 'result_table',
            'mode': 'replace'
        },
        'depends_on': ['task1']
    }
)


# ==================== 注册算子 ====================

OPERATORS = dict([
    register_operator(LOAD_METADATA, load),
    register_operator(SAVE_METADATA, save),
])

"""
I/O 算子模块

提供数据输入输出算子：加载、保存
支持多种数据源：数据库表、文件、GeoJSON 对象
"""

import logging
import os
import pandas as pd
import geopandas as gpd
from typing import Dict, Any
from bson import Binary, Decimal128, ObjectId
from pymongo import MongoClient
from sqlalchemy import create_engine, text
from .base import (
    OperatorType,
    OperatorMetadata, OperatorParam, OperatorCategory, register_operator
)

logger = logging.getLogger(__name__)


# ==================== 文件读写辅助函数 ====================

_SPATIAL_FORMATS = {'shp', 'geojson', 'gpkg', 'kml', 'gml', 'fgb'}


def _map_loopback_host(host: str) -> str:
    shared_host = os.getenv('GEOPYTHON_WORKFLOW_LOOPBACK_HOST', '').strip()
    if shared_host and str(host).strip().lower() in {'localhost', '127.0.0.1', '::1'}:
        return shared_host
    return host

def _read_file(full_path: str, fmt: str, geom_column: str = None):
    """根据格式读取文件，返回 DataFrame 或 GeoDataFrame"""
    if fmt in _SPATIAL_FORMATS:
        kwargs = {}
        if geom_column:
            kwargs['geometry'] = geom_column
        return gpd.read_file(full_path, **kwargs)
    elif fmt in ('csv',):
        return gpd.GeoDataFrame(pd.read_csv(full_path))
    elif fmt in ('parquet',):
        return gpd.GeoDataFrame(pd.read_parquet(full_path))
    elif fmt in ('xlsx', 'xls'):
        return gpd.GeoDataFrame(pd.read_excel(full_path))
    elif fmt in ('json',):
        return gpd.GeoDataFrame(pd.read_json(full_path))
    elif fmt in ('feather',):
        return gpd.GeoDataFrame(pd.read_feather(full_path))
    else:
        raise ValueError(f"不支持的文件格式: {fmt}，支持: csv, parquet, xlsx, json, feather, shp, geojson, gpkg, kml, gml, fgb")


def _write_file(df, full_path: str, fmt: str, mode: str = 'replace'):
    """根据格式写入文件"""
    os.makedirs(os.path.dirname(full_path), exist_ok=True)
    if mode == 'fail' and os.path.exists(full_path):
        raise FileExistsError(f"文件已存在: {full_path}")
    if fmt in _SPATIAL_FORMATS:
        if not isinstance(df, gpd.GeoDataFrame):
            raise ValueError("保存空间格式文件需要 GeoDataFrame 输入")
        df.to_file(full_path, driver=_spatial_driver(fmt))
    elif fmt == 'csv':
        df.to_csv(full_path, index=False)
    elif fmt == 'parquet':
        df.to_parquet(full_path, index=False)
    elif fmt in ('xlsx', 'xls'):
        df.to_excel(full_path, index=False)
    elif fmt == 'json':
        df.to_json(full_path, orient='records', force_ascii=False)
    elif fmt == 'feather':
        df.to_feather(full_path)
    else:
        raise ValueError(f"不支持的文件格式: {fmt}")


def _strip_nfs_root_prefix(base_path: str, path: str) -> str:
    # locator 路径第一段是 export_path 的最后一段（根节点显示名），需去除避免重复
    root_name = os.path.basename(base_path.rstrip('/'))
    relative = path.lstrip('/')
    if root_name and (relative == root_name or relative.startswith(root_name + '/')):
        relative = relative[len(root_name):].lstrip('/')
    return relative


def _spatial_driver(fmt: str) -> str:
    return {'shp': 'ESRI Shapefile', 'gpkg': 'GPKG', 'kml': 'KML',
            'gml': 'GML', 'fgb': 'FlatGeobuf', 'geojson': 'GeoJSON'}.get(fmt, 'GeoJSON')


def _normalize_bson_value(value):
    if isinstance(value, ObjectId):
        return str(value)
    if isinstance(value, Decimal128):
        return value.to_decimal()
    if isinstance(value, Binary):
        return bytes(value)
    if isinstance(value, dict):
        return {key: _normalize_bson_value(item) for key, item in value.items()}
    if isinstance(value, list):
        return [_normalize_bson_value(item) for item in value]
    return value


def _load_mongodb_collection(
    connection_info: Dict[str, Any],
    database: str,
    collection: str,
    pipeline=None,
):
    host = _map_loopback_host(connection_info.get('host'))
    port = connection_info.get('port')
    user = connection_info.get('user') or connection_info.get('username')
    password = connection_info.get('password')
    auth_source = connection_info.get('auth_source') or connection_info.get('authSource') or 'admin'
    if not all([host, port, database, collection]):
        raise ValueError('MongoDB connection_info、database 和 collection 必须完整')
    if pipeline is not None and not isinstance(pipeline, list):
        raise ValueError('MongoDB pipeline 必须是数组')

    client = MongoClient(
        host=host,
        port=int(port),
        username=user or None,
        password=password or None,
        authSource=auth_source,
        connectTimeoutMS=10000,
        serverSelectionTimeoutMS=10000,
    )
    try:
        source = client[database][collection]
        cursor = source.aggregate(pipeline, allowDiskUse=True) if pipeline is not None else source.find({})
        records = [_normalize_bson_value(record) for record in cursor]
        return pd.DataFrame.from_records(records)
    finally:
        client.close()




def load(
    connection_info: Dict[str, Any] = None,
    schema: str = None,
    table: str = None,
    path: str = None,
    geom_column: str = None,
    pipeline=None,
):
    """
    通用数据加载算子

    参数由 Develop Backend 预处理：
    - connection_info: 数据库连接信息（已解密），包含 engine_type、host、port、user、password、database 等
    - schema + table: 数据库表
    - path: 文件或对象路径，格式从扩展名推断

    返回: DataFrame（普通表）或 GeoDataFrame（空间表）

    注意：此算子不再依赖 System API，所有连接信息由 Develop Backend 预处理后传入
    """
    if table:
        if not connection_info:
            raise ValueError("加载数据库表时必须提供 connection_info")

        # 从 connection_info 中提取信息（已由 Develop Backend 从 System API 获取并解密）
        engine_type = connection_info.get('engine_type')
        host = _map_loopback_host(connection_info.get('host'))
        port = connection_info.get('port')
        # 兼容 username 和 user 两种字段名
        user = connection_info.get('user') or connection_info.get('username')
        password = connection_info.get('password')
        database = connection_info.get('database')

        # MongoDB 的 database 由 locator 派生到 schema，连接信息本身可以不带默认 database。
        required_connection_fields = [engine_type, host, port, user]
        if engine_type not in ['mongodb', 'MongoDB']:
            required_connection_fields.append(database)
        if not all(required_connection_fields):
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

        elif engine_type in ['mongodb', 'MongoDB']:
            return _load_mongodb_collection(connection_info, schema or database, table, pipeline)

        else:
            raise ValueError(f"Unsupported engine type for table: {engine_type}")

        return gdf

    if path:
        if not connection_info:
            raise ValueError("加载文件时必须提供 connection_info")
        base_path = connection_info.get('mount_path') or connection_info.get('export_path')
        if not base_path:
            raise ValueError("file connection_info 缺少 export_path 字段")
        if not path:
            raise ValueError("加载文件时必须提供 path 参数")
        full_path = os.path.join(base_path, _strip_nfs_root_prefix(base_path, path))
        fmt = os.path.splitext(path)[1].lstrip('.').lower()
        if not fmt:
            raise ValueError("无法从文件路径推断格式")
        return _read_file(full_path, fmt, geom_column)

    raise ValueError("load 必须接收 schema + table 或 path")


def save(
    input_df,  # 支持 pd.DataFrame 和 gpd.GeoDataFrame
    connection_info: Dict[str, Any] = None,
    schema: str = None,
    table: str = None,
    mode: str = 'replace',
    path: str = None
) -> Dict[str, Any]:
    """
    通用数据保存算子

    参数由 Develop Backend 预处理：
    - input_df: 要保存的数据（支持 DataFrame 和 GeoDataFrame）
    - connection_info: 数据库连接信息（已解密）
    - schema + table: 数据库表目标
    - path: 文件目标，格式从扩展名推断
    """
    if table:
        if not connection_info:
            raise ValueError("保存数据库表时必须提供 connection_info")

        # 从 connection_info 中提取信息
        engine_type = connection_info.get('engine_type')
        host = _map_loopback_host(connection_info.get('host'))
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

    if path:
        if not connection_info:
            raise ValueError("保存文件时必须提供 connection_info")
        base_path = connection_info.get('mount_path') or connection_info.get('export_path')
        if not base_path:
            raise ValueError("file connection_info 缺少 export_path 字段")
        full_path = os.path.join(base_path, _strip_nfs_root_prefix(base_path, path))
        fmt = os.path.splitext(path)[1].lstrip('.').lower()
        if not fmt:
            raise ValueError("无法从目标文件路径推断格式")
        _write_file(input_df, full_path, fmt, mode)
        return input_df

    raise ValueError("save 必须接收 schema + table 或 path")


# ==================== 元数据定义 ====================

LOAD_METADATA = OperatorMetadata(
    name="load",
    type=OperatorType.GENERAL,
    category=OperatorCategory.DATA_IO,
    description="数据加载",
    brief_description="从数据库表、MongoDB collection 或文件资源加载数据",
    execution_modes=["workflow"],
    effects=["read"],

    overview="通用数据加载算子，按 Develop Adapter 派生的 schema/table 或 path 自动选择关系表、MongoDB collection 或文件读取方式。MongoDB 可提交确定性 aggregation pipeline，文件格式从路径扩展名推断。",

    params=[
        OperatorParam(
            name="connection_info",
            type="param",
            data_type="object",
            required=False,
            description="Develop Adapter 派生的数据源连接信息"
        ),
        OperatorParam(
            name="schema",
            type="param",
            data_type="string",
            required=False,
            description="数据库 schema"
        ),
        OperatorParam(
            name="table",
            type="param",
            data_type="string",
            required=False,
            description="数据库表名"
        ),
        OperatorParam(
            name="path",
            type="param",
            data_type="string",
            required=False,
            description="文件或对象路径"
        ),
        OperatorParam(
            name="geom_column",
            type="param",
            data_type="string",
            required=False,
            description="几何列名",
            notes="空间数据的几何列名。如果不指定，geopandas会自动检测几何列（推荐）。仅在自动检测失败或需要指定特定列时使用",
            default=None
        ),
        OperatorParam(
            name="pipeline",
            type="param",
            data_type="array",
            required=False,
            description="MongoDB aggregation pipeline",
            notes="仅用于 MongoDB collection；为空时读取 collection 文档，非空时按给定 pipeline 聚合并返回 DataFrame",
            default=None
        )
    ],

    use_cases=[
        "从业务数据库加载河流数据: schema=public, table=rivers",
        "从 MongoDB collection 执行 aggregation pipeline 并加载结果",
        "从文件引擎加载CSV文件: path=data/points.csv",
        "从文件引擎加载Shapefile: path=gis/roads.shp",
    ],

    notes=[
        "数据库表和文件资源使用同一个 locator 公开参数，访问方式由 Adapter 派生结果决定",
        "MongoDB collection 使用 locator 定位，pipeline 是可选的公开计算参数",
        "文件格式从扩展名自动推断",
        "文件引擎支持空间格式(shp/gpkg/geojson等)和非空间格式(csv/parquet/xlsx等)",
        "支持自动检测几何列,无需手动指定 geom_column (推荐)",
        "如果表中有多个几何列或自动检测失败,可通过 geom_column 参数指定",
        "connection_info、schema/table 或 path 由 Develop Adapter 注入，工作流引擎无需依赖 System 或 Meta API"
    ],

    workflow_example={
        'id': 'load_rivers',
        'operator': 'load',
        'params': {
            'connection_info': {'engine_type': 'postgresql'},
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
    brief_description="将数据保存到数据库表或文件,支持普通表和空间表",
    execution_modes=["workflow"],
    effects=["write"],

    overview="通用数据保存算子，按 Develop Adapter 派生的 schema/table 或 path 自动选择数据库表或文件写入方式。文件格式从目标路径扩展名推断。",

    params=[
        OperatorParam(
            name="input_df",
            type="input",
            data_type="DataFrame",
            required=True,
            description="要保存的数据（支持 DataFrame 和 GeoDataFrame）"
        ),
        OperatorParam(
            name="connection_info",
            type="param",
            data_type="object",
            required=False,
            description="Develop Adapter 派生的目标连接信息"
        ),
        OperatorParam(
            name="schema",
            type="param",
            data_type="string",
            required=False,
            description="目标 schema"
        ),
        OperatorParam(
            name="table",
            type="param",
            data_type="string",
            required=False,
            description="目标表名"
        ),
        OperatorParam(
            name="path",
            type="param",
            data_type="string",
            required=False,
            description="目标文件路径"
        ),
        OperatorParam(
            name="mode",
            type="param",
            data_type="string",
            required=False,
            description="目标已存在时的处理方式",
            enum=["replace", "append", "fail"],
            default="replace",
            notes="replace(替换)、append(追加)、fail(失败)；append 仅支持 table 目标"
        )
    ],

    use_cases=[
        "保存分析结果到数据库: schema=public, table=result, mode=replace",
        "保存结果到文件 CSV: path=output/result.csv",
        "保存空间数据到文件 GeoPackage: path=gis/result.gpkg",
        "工作流结束节点: 作为数据输出的最后一步"
    ],

    notes=[
        "自动识别输入数据类型（DataFrame 或 GeoDataFrame）",
        "文件保存运行时参数使用 connection_info 和 path",
        "文件空间格式(shp/gpkg等)需要 GeoDataFrame 输入",
        "保存空间数据到 PostgreSQL 时使用 PostGIS，自动创建几何索引",
        "mode=append 仅 table 模式支持，file 模式不支持追加",
        "connection_info、schema/table 或 path 由 Develop Adapter 注入，工作流引擎无需依赖 System 或 Meta API"
    ],

    workflow_example={
        'id': 'save_result',
        'operator': 'save',
        'params': {
            'input_df': {'$ref': 'task1'},  # 引用前一个任务的输出
            'connection_info': {'engine_type': 'postgresql'},
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

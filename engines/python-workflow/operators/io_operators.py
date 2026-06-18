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
from sqlalchemy import create_engine, text
from .base import (
    OperatorType,
    OperatorMetadata, OperatorParam, OperatorCategory, register_operator
)

logger = logging.getLogger(__name__)


# ==================== 文件读写辅助函数 ====================

_SPATIAL_FORMATS = {'shp', 'geojson', 'gpkg', 'kml', 'gml', 'fgb'}

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

    elif source_type == 'nfs':
        if not connection_info:
            raise ValueError("source_type=nfs 时必须提供 connection_info")
        base_path = connection_info.get('mount_path') or connection_info.get('export_path')
        if not base_path:
            raise ValueError("NFS connection_info 缺少 export_path 字段")
        if not path:
            raise ValueError("source_type=nfs 时必须提供 path 参数")
        full_path = os.path.join(base_path, _strip_nfs_root_prefix(base_path, path))
        fmt = (format or os.path.splitext(path)[1].lstrip('.')).lower()
        return _read_file(full_path, fmt, geom_column)

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

    elif target_type == 'nfs':
        if not connection_info:
            raise ValueError("target_type=nfs 时必须提供 connection_info")
        base_path = connection_info.get('mount_path') or connection_info.get('export_path')
        if not base_path:
            raise ValueError("NFS connection_info 缺少 export_path 字段")
        if not path:
            raise ValueError("target_type=nfs 时必须提供 path 参数")
        full_path = os.path.join(base_path, _strip_nfs_root_prefix(base_path, path))
        fmt = (format or os.path.splitext(path)[1].lstrip('.')).lower()
        _write_file(input_df, full_path, fmt, mode)
        return input_df

    else:
        raise ValueError(f"Unsupported target_type: {target_type}")


# ==================== 元数据定义 ====================

LOAD_METADATA = OperatorMetadata(
    name="load",
    type=OperatorType.GENERAL,
    category=OperatorCategory.DATA_IO,
    description="数据加载",
    brief_description="从数据库表、NFS文件或GeoJSON对象加载数据,支持多种数据源",

    overview="通用数据加载算子,支持从数据库表(PostgreSQL/MySQL/Doris)、NFS文件系统或内存GeoJSON对象加载数据。根据 source_type 参数自动选择加载方式。NFS 支持 pandas/geopandas 所有常见格式。",

    params=[
        OperatorParam(
            name="source_type",
            type="param",
            data_type="string",
            required=True,
            description="数据来源类型",
            notes="可选值: table(数据库表), nfs(NFS文件), geojson(GeoJSON对象)",
            enum=["table", "nfs", "geojson"],
            default="table"
        ),
        # 特殊参数：资源树选择器（仅在 source_type=table 时显示）
        OperatorParam(
            name="数据源",
            type="ui",
            data_type="object",
            required=False,
            description="选择数据表（推荐使用此方式）",
            notes="使用资源树选择数据表，保存 ResourceLocator；Develop Backend 会在执行前派生连接信息与表路径",
            ui_type="resource_tree_picker",
            ui_config={
                "api_base_url": "/api/v1/meta",
                "engine_types": ["postgresql", "mysql", "doris", "clickhouse"],
                "selectable_node_types": ["table"],
                "enable_geometry_detection": True,
                "require_geometry": False
            },
            depends_on="source_type",
            show_when={"source_type": "table"}
        ),
        OperatorParam(
            name="locator",
            type="param",
            data_type="string",
            required=False,
            description="源表 ResourceLocator",
            notes="source_type=table 时由资源树选择器自动填充；执行前由 Develop Backend 派生 engine_id/schema/table/connection_info",
            depends_on="source_type",
            show_when={"source_type": "table"}
        ),
        # NFS 文件选择器（仅在 source_type=nfs 时显示）
        OperatorParam(
            name="NFS文件",
            type="ui",
            data_type="object",
            required=False,
            description="选择NFS引擎和文件路径",
            notes="从资源树中选择文件，自动填充 engine_id 和 path",
            ui_type="nfs_file_picker",
            depends_on="source_type",
            show_when={"source_type": "nfs"}
        ),
        OperatorParam(
            name="engine_id",
            type="param",
            data_type="integer",
            required=False,
            description="NFS存储引擎ID",
            notes="由NFS文件选择器自动填充",
            depends_on="source_type",
            show_when={"source_type": "nfs"}
        ),
        OperatorParam(
            name="path",
            type="param",
            data_type="string",
            required=False,
            description="文件路径（相对于NFS挂载根目录）",
            notes="由NFS文件选择器自动填充，也可手动输入，如 data/rivers.shp",
            depends_on="source_type",
            show_when={"source_type": "nfs"}
        ),
        OperatorParam(
            name="format",
            type="param",
            data_type="string",
            required=False,
            description="文件格式（可选，默认从扩展名推断）",
            notes="支持: csv, parquet, xlsx, json, feather, shp, geojson, gpkg, kml, gml, fgb",
            enum=["csv", "parquet", "xlsx", "json", "feather", "shp", "geojson", "gpkg", "kml", "gml", "fgb"],
            depends_on="source_type",
            show_when={"source_type": "nfs"}
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
        "从业务数据库加载河流数据: source_type=table, locator=addp://engine/1/path/public/rivers?type=table&item_id=10",
        "从NFS加载CSV文件: source_type=nfs, engine_id=3, path=data/points.csv",
        "从NFS加载Shapefile: source_type=nfs, engine_id=3, path=gis/roads.shp",
        "从内存GeoJSON加载临时数据: source_type=geojson, geojson={...}",
    ],

    notes=[
        "NFS source_type 直接通过 export_path（或 mount_path）访问文件，格式从扩展名自动推断",
        "NFS 支持空间格式(shp/gpkg/geojson等)和非空间格式(csv/parquet/xlsx等)",
        "支持自动检测几何列,无需手动指定 geom_column (推荐)",
        "如果表中有多个几何列或自动检测失败,可通过 geom_column 参数指定",
        "locator 由 Develop Backend 在执行前转换为 connection_info、schema 和 table，工作流引擎无需依赖 System 或 Meta API"
    ],

    workflow_example={
        'id': 'load_rivers',
        'operator': 'load',
        'params': {
            'source_type': 'table',
            'locator': 'addp://engine/1/path/public/rivers?type=table&item_id=10'
        },
        'depends_on': []
    }
)


SAVE_METADATA = OperatorMetadata(
    name="save",
    type=OperatorType.GENERAL,
    category=OperatorCategory.DATA_IO,
    description="数据保存",
    brief_description="将数据保存到数据库表或NFS文件,支持普通表和空间表",

    overview="通用数据保存算子,支持将 DataFrame 或 GeoDataFrame 保存到数据库表(PostgreSQL/MySQL/Doris)或 NFS 文件系统。根据 target_type 参数自动选择保存方式。",

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
            enum=["table", "nfs"],
            default="table",
            notes="table(数据库表) 或 nfs(NFS文件系统)"
        ),
        OperatorParam(
            name="保存目标",
            type="ui",
            data_type="object",
            required=False,
            description="选择保存的数据库和表",
            ui_type="resource_tree_picker",
            ui_config={
                "placeholder": "选择目标父节点",
                "selectable_parent_node_types": ["schema", "database"],
                "auto_fill_params": ["target_parent_locator", "target_name"],
                "allow_create_table": True  # 允许创建新表
            },
            depends_on="target_type",
            show_when={"target_type": "table"}
        ),
        OperatorParam(
            name="target_parent_locator",
            type="param",
            data_type="string",
            required=False,
            description="目标父节点 ResourceLocator",
            notes="target_type=table 时由资源树选择器自动填充，必须指向 schema/database 等真实父节点",
            depends_on="target_type",
            show_when={"target_type": "table"}
        ),
        OperatorParam(
            name="target_name",
            type="param",
            data_type="string",
            required=False,
            description="目标表名",
            notes="target_type=table 时必填；执行前由 Develop Backend 派生 schema/table/connection_info",
            depends_on="target_type",
            show_when={"target_type": "table"}
        ),
        # NFS 文件选择器（仅在 target_type=nfs 时显示）
        OperatorParam(
            name="NFS文件",
            type="ui",
            data_type="object",
            required=False,
            description="选择NFS引擎和保存路径",
            notes="从资源树中选择目录或直接输入路径，自动填充 engine_id 和 path",
            ui_type="nfs_file_picker",
            depends_on="target_type",
            show_when={"target_type": "nfs"}
        ),
        OperatorParam(
            name="engine_id",
            type="param",
            data_type="integer",
            required=False,
            description="NFS存储引擎ID",
            notes="由NFS文件选择器自动填充",
            depends_on="target_type",
            show_when={"target_type": "nfs"}
        ),
        OperatorParam(
            name="path",
            type="param",
            data_type="string",
            required=False,
            description="文件路径（相对于NFS挂载根目录）",
            notes="由NFS文件选择器自动填充，也可手动输入，如 output/result.csv",
            depends_on="target_type",
            show_when={"target_type": "nfs"}
        ),
        OperatorParam(
            name="format",
            type="param",
            data_type="string",
            required=False,
            description="文件格式（可选，默认从扩展名推断）",
            notes="支持: csv, parquet, xlsx, json, feather, shp, geojson, gpkg, kml, gml, fgb",
            enum=["csv", "parquet", "xlsx", "json", "feather", "shp", "geojson", "gpkg", "kml", "gml", "fgb"],
            depends_on="target_type",
            show_when={"target_type": "nfs"}
        ),
        OperatorParam(
            name="mode",
            type="param",
            data_type="string",
            required=False,
            description="文件已存在时的处理方式",
            enum=["replace", "fail"],
            default="replace",
            notes="replace(覆盖), fail(抛出错误)。nfs 不支持 append 模式",
            depends_on="target_type",
            show_when={"target_type": "nfs"}
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
        "保存分析结果到数据库: target_type=table, target_parent_locator=addp://engine/1/path/public?type=schema&node_id=8, target_name=result, mode=replace",
        "保存结果到NFS CSV: target_type=nfs, engine_id=3, path=output/result.csv",
        "保存空间数据到NFS GeoPackage: target_type=nfs, engine_id=3, path=gis/result.gpkg",
        "工作流结束节点: 作为数据输出的最后一步"
    ],

    notes=[
        "自动识别输入数据类型（DataFrame 或 GeoDataFrame）",
        "NFS 保存时自动创建目标目录",
        "NFS 空间格式(shp/gpkg等)需要 GeoDataFrame 输入",
        "保存空间数据到 PostgreSQL 时使用 PostGIS，自动创建几何索引",
        "mode=append 仅 table 模式支持，nfs 模式不支持追加",
        "target_parent_locator 和 target_name 由 Develop Backend 在执行前转换为 connection_info、schema 和 table，工作流引擎无需依赖 System 或 Meta API"
    ],

    workflow_example={
        'id': 'save_result',
        'operator': 'save',
        'params': {
            'input_df': {'$ref': 'task1'},  # 引用前一个任务的输出
            'target_type': 'table',
            'target_parent_locator': 'addp://engine/1/path/public?type=schema&node_id=8',
            'target_name': 'result_table',
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

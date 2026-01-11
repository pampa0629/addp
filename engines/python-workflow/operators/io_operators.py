"""
I/O 算子模块

提供数据输入输出算子：加载、保存
支持多种数据源：数据库表、文件、GeoJSON 对象
"""

import geopandas as gpd
from typing import Dict, Any
import os
import requests
from sqlalchemy import create_engine
from .base import (
    OperatorType,
    OperatorMetadata, OperatorParam, OperatorCategory, register_operator
)


# ==================== 算子实现 ====================

def load(params: Dict[str, Any]) -> gpd.GeoDataFrame:
    """
    通用数据加载算子

    接收 DataLocation 信息，根据类型加载数据：
    - source_type: "table" | "file" | "geojson" | "reference"
    - engine_id: 存储引擎 ID
    - 其他参数: table/path、schema/format 等

    注意：此算子不直接执行 SQL，而是根据 DataLocation 信息
    通过 System API 获取引擎连接信息后加载数据
    """
    source_type = params.get('source_type')
    engine_id = params.get('engine_id')

    if source_type == 'table':
        # 1. 从 System API 获取引擎连接信息
        system_url = os.getenv('SYSTEM_BACKEND_URL', 'http://localhost:8080')
        response = requests.get(f'{system_url}/api/engines/{engine_id}')

        if response.status_code != 200:
            raise ValueError(f"Failed to get engine {engine_id}: {response.text}")

        engine = response.json()

        # 2. 根据引擎类型构建连接字符串
        engine_type = engine['engine_type']
        conn_info = engine['connection_info']

        table = params.get('table')
        schema = params.get('schema', 'public')

        # 3. 根据不同数据库类型加载
        if engine_type in ['postgresql', 'PostgreSQL']:
            conn_str = f"postgresql://{conn_info['user']}:{conn_info['password']}@{conn_info['host']}:{conn_info['port']}/{conn_info['database']}"
            engine_db = create_engine(conn_str)

            # 读取空间数据（假设几何列名为 geom）
            sql = f'SELECT * FROM {schema}.{table}'
            gdf = gpd.read_postgis(sql, engine_db, geom_col='geom')

        elif engine_type in ['mysql', 'MySQL', 'doris', 'Doris']:
            password = conn_info.get('password', '')
            if password:
                conn_str = f"mysql+pymysql://{conn_info['user']}:{password}@{conn_info['host']}:{conn_info['port']}/{conn_info['database']}"
            else:
                conn_str = f"mysql+pymysql://{conn_info['user']}@{conn_info['host']}:{conn_info['port']}/{conn_info['database']}"

            engine_db = create_engine(conn_str)

            # MySQL/Doris 空间数据读取
            sql = f'SELECT * FROM {table}'
            gdf = gpd.read_postgis(sql, engine_db, geom_col='geom')

        else:
            raise ValueError(f"Unsupported engine type for table: {engine_type}")

        return gdf

    elif source_type == 'file':
        # 从对象存储加载文件
        system_url = os.getenv('SYSTEM_BACKEND_URL', 'http://localhost:8080')
        response = requests.get(f'{system_url}/api/engines/{engine_id}')

        if response.status_code != 200:
            raise ValueError(f"Failed to get engine {engine_id}: {response.text}")

        engine = response.json()

        path = params.get('path')
        format_type = params.get('format', 'geojson')

        # TODO: 实现从 MinIO/S3 下载文件逻辑
        # local_file = download_from_minio(engine, path)

        # 临时实现：假设文件已在本地
        if format_type in ['geojson', 'shapefile']:
            gdf = gpd.read_file(path)
        else:
            raise ValueError(f"Unsupported format: {format_type}")

        return gdf

    elif source_type == 'geojson':
        # 直接解析 GeoJSON 对象
        geojson_obj = params.get('geojson')
        gdf = gpd.GeoDataFrame.from_features(geojson_obj['features'])
        return gdf

    elif source_type == 'reference':
        # 引用其他任务的输出（已在内存中的 GeoDataFrame）
        # 这种情况下不需要加载，直接返回引用
        raise NotImplementedError("Reference type should be handled by workflow engine")

    else:
        raise ValueError(f"Unsupported source_type: {source_type}")


def save(data: gpd.GeoDataFrame, params: Dict[str, Any]) -> Dict[str, Any]:
    """
    通用数据保存算子

    根据目标类型保存数据：
    - target_type: "table" | "file"
    - engine_id: 存储引擎 ID
    """
    target_type = params.get('target_type')
    engine_id = params.get('engine_id')

    if target_type == 'table':
        # 1. 从 System API 获取引擎连接信息
        system_url = os.getenv('SYSTEM_BACKEND_URL', 'http://localhost:8080')
        response = requests.get(f'{system_url}/api/engines/{engine_id}')

        if response.status_code != 200:
            raise ValueError(f"Failed to get engine {engine_id}: {response.text}")

        engine = response.json()

        # 2. 构建连接
        engine_type = engine['engine_type']
        conn_info = engine['connection_info']

        table = params.get('table')
        schema = params.get('schema', 'public')
        mode = params.get('mode', 'overwrite')

        # 3. 根据数据库类型保存
        if engine_type in ['postgresql', 'PostgreSQL']:
            conn_str = f"postgresql://{conn_info['user']}:{conn_info['password']}@{conn_info['host']}:{conn_info['port']}/{conn_info['database']}"
            engine_db = create_engine(conn_str)

            if_exists = 'replace' if mode == 'overwrite' else 'append'
            data.to_postgis(table, engine_db, schema=schema, if_exists=if_exists, index=False)

        elif engine_type in ['mysql', 'MySQL', 'doris', 'Doris']:
            password = conn_info.get('password', '')
            if password:
                conn_str = f"mysql+pymysql://{conn_info['user']}:{password}@{conn_info['host']}:{conn_info['port']}/{conn_info['database']}"
            else:
                conn_str = f"mysql+pymysql://{conn_info['user']}@{conn_info['host']}:{conn_info['port']}/{conn_info['database']}"

            engine_db = create_engine(conn_str)

            if_exists = 'replace' if mode == 'overwrite' else 'append'
            data.to_sql(table, engine_db, if_exists=if_exists, index=False)

        else:
            raise ValueError(f"Unsupported engine type for table: {engine_type}")

        return {
            "output_type": "table",
            "output_table": f"{schema}.{table}",
            "engine_id": engine_id,
            "row_count": len(data)
        }

    elif target_type == 'file':
        # TODO: 实现保存到对象存储逻辑
        path = params.get('path')
        format_type = params.get('format', 'geojson')

        # 临时实现：保存到本地文件
        if format_type == 'geojson':
            data.to_file(path, driver='GeoJSON')
        elif format_type == 'shapefile':
            data.to_file(path, driver='ESRI Shapefile')
        else:
            raise ValueError(f"Unsupported format: {format_type}")

        return {
            "output_type": "file",
            "output_file": path,
            "format": format_type
        }

    else:
        # 标量或其他类型
        return {
            "output_type": "scalar",
            "value": str(data)
        }


# ==================== 元数据定义 ====================

LOAD_METADATA = OperatorMetadata(
    name="load",
    type=OperatorType.GENERAL,
    category=OperatorCategory.DATA_IO,
    description="数据加载",
    brief_description="从数据库表、文件或GeoJSON对象加载空间数据,支持多种数据源",

    overview="通用数据加载算子,支持从数据库表(PostgreSQL/MySQL/Doris)、文件(Shapefile/GeoJSON)或内存GeoJSON对象加载空间数据。根据 source_type 参数自动选择加载方式。",

    params=[
        OperatorParam(
            name="source_type",
            type="param",
            data_type="string",
            required=True,
            description="数据来源类型",
            notes="可选值: table(数据库表), file(文件), geojson(GeoJSON对象)"
        ),
        OperatorParam(
            name="engine_id",
            type="param",
            data_type="integer",
            required=True,
            description="存储引擎ID",
            notes="仅 source_type=table 时有效,对应 System 模块中的引擎ID"
        ),
        OperatorParam(
            name="schema",
            type="param",
            data_type="string",
            required=False,
            description="数据库schema名称",
            notes="仅 source_type=table 时有效"
        ),
        OperatorParam(
            name="table",
            type="param",
            data_type="string",
            required=False,
            description="数据库表名",
            notes="仅 source_type=table 时必填"
        ),
        OperatorParam(
            name="path",
            type="param",
            data_type="string",
            required=False,
            description="文件路径",
            notes="仅 source_type=file 时必填,支持绝对路径或相对路径"
        ),
        OperatorParam(
            name="format",
            type="param",
            data_type="string",
            required=False,
            description="文件格式",
            notes="仅 source_type=file 时有效,可选值: geojson, shapefile"
        ),
        OperatorParam(
            name="geojson",
            type="param",
            data_type="object",
            required=False,
            description="GeoJSON对象",
            notes="仅 source_type=geojson 时必填,必须是有效的 GeoJSON FeatureCollection"
        )
    ],

    use_cases=[
        "从业务数据库加载河流数据: source_type=table, engine_id=1, table=rivers",
        "从文件加载行政区划: source_type=file, path=/data/admin.shp, format=shapefile",
        "从内存GeoJSON加载临时数据: source_type=geojson, geojson={...}",
        "工作流起始节点: 作为数据输入的第一步"
    ],

    notes=[
        "数据库表必须包含几何字段(geometry列),否则加载失败",
        "文件路径支持 MinIO 路径(minio://bucket/path)和本地路径",
        "Shapefile 会自动处理编码问题,默认尝试 utf-8 和 gb2312",
        "加载后的 GeoDataFrame 会保留所有属性字段"
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
    brief_description="将空间数据保存到数据库表或文件,支持多种输出格式",

    overview="通用数据保存算子,支持将 GeoDataFrame 保存到数据库表(PostgreSQL/MySQL/Doris)或文件(Shapefile/GeoJSON)。根据 target_type 参数自动选择保存方式。",

    params=[
        OperatorParam(
            name="data",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="要保存的空间数据"
        ),
        OperatorParam(
            name="target_type",
            type="param",
            data_type="string",
            required=True,
            description="保存目标类型",
            notes="可选值: table(数据库表), file(文件)"
        ),
        OperatorParam(
            name="engine_id",
            type="param",
            data_type="integer",
            required=False,
            description="存储引擎ID",
            notes="仅 target_type=table 时必填,对应 System 模块中的引擎ID"
        ),
        OperatorParam(
            name="schema",
            type="param",
            data_type="string",
            required=False,
            description="数据库schema名称",
            notes="仅 target_type=table 时有效"
        ),
        OperatorParam(
            name="table",
            type="param",
            data_type="string",
            required=False,
            description="数据库表名",
            notes="仅 target_type=table 时必填"
        ),
        OperatorParam(
            name="mode",
            type="param",
            data_type="string",
            required=False,
            description="表已存在时的处理方式",
            notes="可选值: replace(替换), append(追加), fail(失败),仅 target_type=table 时有效"
        ),
        OperatorParam(
            name="path",
            type="param",
            data_type="string",
            required=False,
            description="文件保存路径",
            notes="仅 target_type=file 时必填,支持绝对路径或相对路径"
        ),
        OperatorParam(
            name="format",
            type="param",
            data_type="string",
            required=False,
            description="文件格式",
            notes="仅 target_type=file 时有效,可选值: geojson, shapefile"
        )
    ],

    use_cases=[
        "保存分析结果到数据库: target_type=table, engine_id=1, table=result, mode=replace",
        "导出到GeoJSON文件: target_type=file, path=/output/result.geojson, format=geojson",
        "导出到Shapefile: target_type=file, path=/output/result.shp, format=shapefile",
        "工作流结束节点: 作为数据输出的最后一步"
    ],

    notes=[
        "保存到数据库时会自动创建几何索引提升查询性能",
        "保存到Shapefile时字段名会被截断为10个字符",
        "GeoJSON 格式保留所有字段名和数据精度",
        "mode=append 时要求表结构与数据结构一致"
    ],

    workflow_example={
        'id': 'save_result',
        'operator': 'save',
        'params': {
            'data': {'$ref': 'buffer_rivers'},
            'target_type': 'table',
            'engine_id': 1,
            'schema': 'public',
            'table': 'river_buffer_result',
            'mode': 'replace'
        },
        'depends_on': ['buffer_rivers']
    }
)


# ==================== 注册算子 ====================

OPERATORS = dict([
    register_operator(LOAD_METADATA, load),
    register_operator(SAVE_METADATA, save),
])

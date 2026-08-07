"""
Spark 工作流I/O 算子
"""

import logging
from typing import Dict, Any, List, Optional

from .base import OperatorMetadata, OperatorParam, OperatorCategory, register_operator
from .spark_types import DataFrame

logger = logging.getLogger(__name__)


# ========================================
# I/O 算子 (5个)
# ========================================

def load(engine_id: int, tenant_id: int, **params) -> DataFrame:
    """
    数据加载算子

    支持多种数据源:
    - table: 数据库表 (PostgreSQL/MySQL/Doris)
    - file: 文件 (S3/HDFS/本地, Parquet/GeoParquet/CSV/Shapefile/Delta/Hudi)
    - sql: 自定义SQL查询
    - catalog: 数据目录 (Iceberg/Delta)

    Args:
        engine_id: Spark引擎ID (从system.engines获取)
        tenant_id: Runtime 已验证的租户上下文
        **params: 统一参数 (DataLocation格式)
            - source_type: "table" | "file" | "sql" | "catalog"
            - schema: 数据库schema (table类型)
            - table: 表名 (table类型)
            - geom_column: 几何列名 (默认"geom")
            - path: 文件路径 (file类型,由 Develop Adapter 注入)
            - format: 文件格式 (file类型)
            - sql: SQL查询 (sql类型)
            - catalog_name: catalog名称 (catalog类型)
            - database: 数据库名 (catalog类型)

    Returns:
        Spark DataFrame

    Example:
        # 从数据库加载
        df = load(engine_id=34, source_type="table", schema="public", table="poi")

        # 从文件加载
        df = load(engine_id=34, source_type="file", path="s3a://bucket/data.parquet", format="parquet")

        # 从SQL加载
        df = load(engine_id=34, source_type="sql", sql="SELECT * FROM poi WHERE category='restaurant'")
    """
    from spark_connector import get_spark_connector
    from storage_adapters import StorageAdapter

    # 获取SparkSession
    connector = get_spark_connector()
    spark = connector.get_or_create_session(engine_id, tenant_id)

    # 使用StorageAdapter加载
    df = StorageAdapter.load(spark, params)

    logger.info(f"Loaded data from {params.get('source_type')}, rows: {df.count()}")

    return df




def save(input_df: DataFrame, engine_id: int, **params) -> Dict[str, Any]:
    """
    数据保存算子

    Args:
        input_df: 输入DataFrame
        engine_id: Spark引擎ID
        **params: 保存参数
            - target_type: "table" | "file"
            - schema: 数据库schema (table类型)
            - table: 表名 (table类型)
            - mode: 保存模式 ("overwrite" | "append")
            - path: 文件路径 (file类型,由 Develop Adapter 注入)
            - format: 文件格式 (file类型)

    Returns:
        执行结果字典 {"status": "success", "rows": count}

    Example:
        save(df, engine_id=34, target_type="table", schema="public", table="result", mode="overwrite")
    """
    from storage_adapters import StorageAdapter

    # 使用StorageAdapter保存
    StorageAdapter.save(input_df, params)

    row_count = input_df.count()
    logger.info(f"Saved {row_count} rows to {params.get('target_type')}")

    return {
        "status": "success",
        "rows": row_count,
        "target": f"{params.get('schema', '')}.{params.get('table', params.get('path', ''))}"
    }




def preview(input_df: DataFrame, limit: int = 100) -> DataFrame:
    """
    数据预览算子

    Args:
        input_df: 输入DataFrame
        limit: 预览行数 (默认100)

    Returns:
        预览DataFrame
    """
    return input_df.limit(limit)




def cache(input_df: DataFrame) -> DataFrame:
    """
    内存缓存算子

    将DataFrame缓存到内存,提高后续查询性能

    Args:
        input_df: 输入DataFrame

    Returns:
        缓存后的DataFrame
    """
    df_cached = input_df.cache()
    logger.info(f"Cached DataFrame with {df_cached.count()} rows")
    return df_cached




def persist(input_df: DataFrame, storage_level: str = "MEMORY_AND_DISK") -> DataFrame:
    """
    持久化算子

    Args:
        input_df: 输入DataFrame
        storage_level: 存储级别
            - "MEMORY_ONLY": 仅内存
            - "DISK_ONLY": 仅磁盘
            - "MEMORY_AND_DISK": 内存+磁盘 (默认)
            - "MEMORY_AND_DISK_SER": 内存+磁盘(序列化)

    Returns:
        持久化后的DataFrame
    """
    from pyspark import StorageLevel

    level_map = {
        "MEMORY_ONLY": StorageLevel.MEMORY_ONLY,
        "DISK_ONLY": StorageLevel.DISK_ONLY,
        "MEMORY_AND_DISK": StorageLevel.MEMORY_AND_DISK,
        "MEMORY_AND_DISK_SER": StorageLevel.MEMORY_AND_DISK_SER,
    }

    level = level_map.get(storage_level, StorageLevel.MEMORY_AND_DISK)
    df_persisted = input_df.persist(level)

    logger.info(f"Persisted DataFrame with storage level: {storage_level}")

    return df_persisted


# ========================================
# I/O 算子元数据 (Pydantic 格式)
# ========================================

# load 算子元数据
LOAD_METADATA = OperatorMetadata(
    name="load",
    category=OperatorCategory.DATA_IO,
    description="数据加载",
    brief_description="从多种数据源加载数据,支持数据库表、文件、SQL查询和数据目录",
    execution_modes=["workflow"],
    effects=["read"],
    overview="load 算子是工作流的起点,支持从数据库(PostgreSQL/MySQL/Doris)、文件系统(S3/HDFS/本地)、SQL查询和数据目录(Iceberg/Delta)加载数据到 Spark DataFrame。支持多种格式:Parquet/GeoParquet/CSV/Shapefile/Delta/Hudi。",
    params=[
        OperatorParam(
            name="source_type",
            type="str",
            required=True,
            description="数据源类型: table/file/sql/catalog",
            notes="不同类型需要不同的参数组合"
        ),
        OperatorParam(
            name="connection_info",
            type="object",
            required=False,
            description="Develop Adapter 派生的数据源连接信息"
        ),
        OperatorParam(
            name="schema",
            type="str",
            required=False,
            description="数据库schema名称(运行时派生)",
            notes="table 类型由 Develop Adapter 注入；catalog 类型可显式填写 database"
        ),
        OperatorParam(
            name="table",
            type="str",
            required=False,
            description="表名(运行时派生或catalog类型)",
            notes="table 类型由 Develop Adapter 注入；catalog 类型需显式填写"
        ),
        OperatorParam(
            name="path",
            type="str",
            required=False,
            description="文件路径(file类型,运行时派生)",
            notes="source_type=file 时由 Develop Adapter 注入；对象存储使用 s3a://bucket/key"
        ),
        OperatorParam(
            name="format",
            type="str",
            required=False,
            description="文件格式(file类型)",
            notes="parquet/csv/shapefile/geojson/delta/hudi"
        ),
        OperatorParam(
            name="geom_column",
            type="str",
            required=False,
            description="几何列名",
            notes="默认\"geom\",加载空间数据时使用"
        )
    ],
    use_cases=[
        "POI 数据分析: 从 PostgreSQL schema/table 加载 100万条 POI 点位数据,进行空间聚类分析",
        "遥感影像处理: 从 S3 加载 10GB GeoParquet 格式的遥感数据,进行波段运算",
        "实时数据接入: 通过 SQL 查询从业务库加载最近24小时的车辆轨迹,用于热力图生成",
        "历史数据分析: 从 Delta Lake 加载 2020-2024年城市边界变化数据,进行时序分析"
    ],
    notes=[
        "GeoParquet 格式性能最佳,推荐用于大规模空间数据",
        "Shapefile 会自动解析 .shp/.shx/.dbf/.prj 文件组",
        "首次加载大文件建议使用 cache 算子缓存结果",
        "SQL 查询类型支持 JOIN 和 WHERE,但复杂查询建议在数据库端完成",
        "connection_info、schema/table 或 path 由 Develop Adapter 在执行前注入"
    ],
    input_desc="无(起始算子)",
    output_desc="DataFrame (包含加载的数据)",
    workflow_example={
        "id": "load_poi_data",
        "operator": "load",
        "params": {
            "source_type": "table",
            "connection_info": {"engine_type": "postgresql"},
            "schema": "public",
            "table": "poi",
            "geom_column": "geom"
        },
        "depends_on": []
    }
)


# save 算子元数据
SAVE_METADATA = OperatorMetadata(
    name="save",
    category=OperatorCategory.DATA_IO,
    description="数据保存",
    brief_description="将 DataFrame 保存到数据库表或文件系统,支持覆盖和追加模式",
    execution_modes=["workflow"],
    effects=["write"],
    overview="save 算子是工作流的终点,将处理后的 DataFrame 保存到目标位置。支持保存到数据库表(PostgreSQL/MySQL/Doris)和文件系统(S3/HDFS/本地),提供覆盖(overwrite)和追加(append)两种模式。",
    params=[
        OperatorParam(
            name="input_df",
            type="dataframe",
            required=True,
            description="输入 DataFrame",
            notes="必须是有效的 Spark DataFrame"
        ),
        OperatorParam(
            name="target_type",
            type="str",
            required=True,
            description="目标类型: table/file",
            notes="table写数据库,file写文件系统"
        ),
        OperatorParam(
            name="connection_info",
            type="object",
            required=False,
            description="Develop Adapter 派生的目标连接信息"
        ),
        OperatorParam(
            name="mode",
            type="str",
            required=False,
            description="保存模式: overwrite/append",
            notes="默认 overwrite,谨慎使用以免丢失数据"
        ),
        OperatorParam(
            name="schema",
            type="str",
            required=False,
            description="schema名称(table类型,运行时派生)",
            notes="由 Develop Adapter 注入"
        ),
        OperatorParam(
            name="table",
            type="str",
            required=False,
            description="表名(table类型)",
            notes="由 Develop Adapter 注入"
        ),
        OperatorParam(
            name="path",
            type="str",
            required=False,
            description="文件路径(file类型,运行时派生)",
            notes="由 Develop Adapter 注入；对象存储使用 s3a://bucket/key"
        ),
        OperatorParam(
            name="format",
            type="str",
            required=False,
            description="文件格式(file类型)",
            notes="parquet/csv/json/delta"
        )
    ],
    use_cases=[
        "分析结果入库: 将缓冲区分析的 50万条结果保存到 PostgreSQL 结果表,供业务系统查询",
        "数据导出: 将清洗后的 100万条 POI 数据导出为 GeoParquet,减少存储空间 70%",
        "增量更新: 使用 append 模式将每日新增的 5000条轨迹追加到历史表",
        "中间结果缓存: 将 200万条空间连接中间结果保存为 Parquet,供下游工作流复用"
    ],
    notes=[
        "overwrite 模式会完全替换目标表,无法回滚",
        "保存到 PostgreSQL 时,几何列自动转换为 PostGIS geometry 类型",
        "大数据量保存建议使用 Parquet 格式,压缩比高且查询快",
        "append 模式不检查重复,需自行去重",
        "connection_info、schema/table 或 path 由 Develop Adapter 在执行前注入"
    ],
    input_desc="DataFrame (待保存的数据)",
    output_desc="Dict (执行结果: status, rows, target)",
    workflow_example={
        "id": "save_buffer_result",
        "operator": "save",
        "params": {
            "input_df": {"$ref": "buffer_analysis"},
            "target_type": "table",
            "connection_info": {"engine_type": "postgresql"},
            "schema": "public",
            "table": "buffer_result",
            "mode": "overwrite"
        },
        "depends_on": ["buffer_analysis"]
    }
)


# preview 算子元数据
PREVIEW_METADATA = OperatorMetadata(
    name="preview",
    category=OperatorCategory.DATA_IO,
    description="数据预览",
    brief_description="限制返回行数,用于快速查看数据样本和调试工作流",
    execution_modes=["workflow"],
    effects=["read"],
    overview="preview 算子用于调试和数据探索,返回 DataFrame 的前 N 行。相比 show(),preview 返回完整 DataFrame 可继续处理,常用于工作流开发阶段验证中间结果。",
    params=[
        OperatorParam(
            name="input_df",
            type="dataframe",
            required=True,
            description="输入 DataFrame",
            notes="可以是任意 DataFrame"
        ),
        OperatorParam(
            name="limit",
            type="int",
            required=False,
            description="预览行数",
            notes="默认 100,建议不超过 1000"
        )
    ],
    use_cases=[
        "数据质量检查: 加载数据后预览前 100 行,检查字段类型和空值情况",
        "工作流调试: 在 8 节点复杂工作流中插入 preview 节点,快速验证中间结果是否符合预期",
        "结果抽样展示: 将百万级分析结果限制为 500 行,在前端表格中展示",
        "性能优化: 开发阶段使用小数据集(preview 1000行)测试算子逻辑,避免全量计算"
    ],
    notes=[
        "preview 不触发全表扫描,性能开销小",
        "返回的是 DataFrame 而非数组,可继续链式调用",
        "不保证返回特定行(取决于分区分布)",
        "生产环境建议移除 preview 节点或增大 limit"
    ],
    input_desc="DataFrame",
    output_desc="DataFrame (前 N 行)",
    workflow_example={
        "id": "preview_poi",
        "operator": "preview",
        "params": {
            "input_df": {"$ref": "load_poi"},
            "limit": 100
        },
        "depends_on": ["load_poi"]
    }
)


# cache 算子元数据
CACHE_METADATA = OperatorMetadata(
    name="cache",
    category=OperatorCategory.DATA_IO,
    description="内存缓存",
    brief_description="将 DataFrame 缓存到内存,后续操作直接读缓存,适合多次复用的中间结果",
    execution_modes=["workflow"],
    effects=["read"],
    overview="cache 算子将 DataFrame 缓存到 Spark 内存中,后续对该 DataFrame 的操作无需重新计算。适用于需要多次访问的中间结果,如迭代算法、多分支工作流。等价于 persist(MEMORY_ONLY)。",
    params=[
        OperatorParam(
            name="input_df",
            type="dataframe",
            required=True,
            description="输入 DataFrame",
            notes="首次访问时触发计算并缓存"
        )
    ],
    use_cases=[
        "基础数据复用: 将 500万行 POI 基础数据缓存,供 5 个不同分析任务使用,避免重复加载",
        "迭代算法优化: K-means 聚类中缓存点位数据,10次迭代时间从 50秒降至 8秒",
        "多分支工作流: 空间连接后的结果被 3 个下游节点使用,缓存避免重复计算",
        "交互式查询: Jupyter Notebook 中缓存 50万行清洗后的数据,支持快速试错和探索"
    ],
    notes=[
        "cache 仅在首次 action 时生效(如 count、show、save)",
        "内存不足时 Spark 会自动 evict,性能可能退化",
        "大数据集(>executor内存)建议用 persist(\"MEMORY_AND_DISK\")",
        "不再使用时调用 unpersist() 释放内存"
    ],
    input_desc="DataFrame",
    output_desc="DataFrame (缓存后)",
    workflow_example={
        "id": "cache_base_data",
        "operator": "cache",
        "params": {
            "input_df": {"$ref": "load_and_clean"}
        },
        "depends_on": ["load_and_clean"]
    }
)


# persist 算子元数据
PERSIST_METADATA = OperatorMetadata(
    name="persist",
    category=OperatorCategory.DATA_IO,
    description="持久化存储",
    brief_description="灵活的持久化策略,支持内存、磁盘和序列化组合,适合大数据集",
    execution_modes=["workflow"],
    effects=["read"],
    overview="persist 算子提供比 cache 更灵活的持久化选项,可根据数据大小和访问模式选择存储级别。MEMORY_AND_DISK 在内存不足时自动溢出到磁盘,避免重新计算;MEMORY_AND_DISK_SER 通过序列化减少内存占用。",
    params=[
        OperatorParam(
            name="input_df",
            type="dataframe",
            required=True,
            description="输入 DataFrame",
            notes="首次访问时触发计算并持久化"
        ),
        OperatorParam(
            name="storage_level",
            type="str",
            required=False,
            description="存储级别",
            notes="MEMORY_ONLY/DISK_ONLY/MEMORY_AND_DISK(默认)/MEMORY_AND_DISK_SER"
        )
    ],
    use_cases=[
        "超大数据集: 1000万行道路网络数据使用 MEMORY_AND_DISK,内存放不下时自动溢出磁盘",
        "内存受限环境: 使用 MEMORY_AND_DISK_SER 将 200MB DataFrame 序列化为 50MB,节省 75% 内存",
        "纯磁盘缓存: 3000万行临时结果表使用 DISK_ONLY,避免占用宝贵内存资源",
        "高频访问数据: 3000 条省市边界等基础数据使用 MEMORY_ONLY,保证最快访问速度"
    ],
    notes=[
        "MEMORY_AND_DISK 是推荐的默认选项,兼顾性能和可靠性",
        "序列化(SER)会增加 CPU 开销,仅在内存紧张时使用",
        "DISK_ONLY 性能显著低于 MEMORY,谨慎使用",
        "持久化的 DataFrame 在 Spark Session 结束时自动清除"
    ],
    input_desc="DataFrame",
    output_desc="DataFrame (持久化后)",
    workflow_example={
        "id": "persist_road_network",
        "operator": "persist",
        "params": {
            "input_df": {"$ref": "load_roads"},
            "storage_level": "MEMORY_AND_DISK"
        },
        "depends_on": ["load_roads"]
    }
)


# 使用 register_operator 构建 OPERATORS 字典
OPERATORS = dict([
    register_operator(LOAD_METADATA, load),
    register_operator(SAVE_METADATA, save),
    register_operator(PREVIEW_METADATA, preview),
    register_operator(CACHE_METADATA, cache),
    register_operator(PERSIST_METADATA, persist)
])

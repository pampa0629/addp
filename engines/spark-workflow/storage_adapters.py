"""
Storage Adapters - 统一存储访问适配器
支持: 数据库(JDBC) + 文件(S3/HDFS) + 湖仓(Iceberg/Delta) + Catalog
"""

import logging
from typing import Any, Dict

try:
    from pyspark.sql import SparkSession, DataFrame
except ModuleNotFoundError:
    SparkSession = Any
    DataFrame = Any

logger = logging.getLogger(__name__)


class StorageAdapter:
    """统一存储访问适配器"""

    @staticmethod
    def load(spark: SparkSession, params: Dict[str, Any]) -> DataFrame:
        """
        根据source_type加载数据

        Args:
            spark: SparkSession实例
            params: 加载参数 (包含source_type等)

        Returns:
            Spark DataFrame
        """
        source_type = params.get('source_type')

        if source_type == 'table':
            return DatabaseAdapter.load(spark, params)
        elif source_type == 'file':
            return FileAdapter.load(spark, params)
        elif source_type == 'catalog':
            return CatalogAdapter.load(spark, params)
        elif source_type == 'sql':
            return spark.sql(params['sql'])
        else:
            raise ValueError(f"Unsupported source_type: {source_type}")

    @staticmethod
    def save(df: DataFrame, params: Dict[str, Any]):
        """
        保存数据

        Args:
            df: Spark DataFrame
            params: 保存参数
        """
        target_type = params.get('target_type')

        if target_type == 'table':
            return DatabaseAdapter.save(df, params)
        elif target_type == 'file':
            return FileAdapter.save(df, params)
        else:
            raise ValueError(f"Unsupported target_type: {target_type}")


class DatabaseAdapter:
    """数据库适配器 (JDBC)"""

    @staticmethod
    def _connection_context(params: Dict[str, Any]) -> tuple[str, Dict[str, Any]]:
        conn_info = params.get('connection_info')
        if not isinstance(conn_info, dict):
            raise ValueError("source_type=table/target_type=table requires connection_info")

        engine_type = conn_info.get('engine_type')
        if not engine_type:
            raise ValueError("connection_info.engine_type is required")

        return engine_type, conn_info

    @staticmethod
    def load(spark: SparkSession, params: Dict[str, Any]) -> DataFrame:
        """从数据库加载数据"""
        engine_type, conn_info = DatabaseAdapter._connection_context(params)
        schema = params.get('schema', 'public')
        table = params['table']

        # 构建JDBC URL
        if engine_type in ['postgresql', 'PostgreSQL']:
            jdbc_url = f"jdbc:postgresql://{conn_info['host']}:{conn_info['port']}/{conn_info['database']}"
            driver = "org.postgresql.Driver"
        elif engine_type in ['mysql', 'MySQL']:
            jdbc_url = f"jdbc:mysql://{conn_info['host']}:{conn_info['port']}/{conn_info['database']}"
            driver = "com.mysql.cj.jdbc.Driver"
        elif engine_type in ['doris', 'Doris']:
            jdbc_url = f"jdbc:mysql://{conn_info['host']}:{conn_info['port']}/{conn_info['database']}"
            driver = "com.mysql.cj.jdbc.Driver"
        else:
            raise ValueError(f"Unsupported engine type: {engine_type}")

        logger.info(f"Loading from database: {jdbc_url}, table: {schema}.{table}")

        # 读取表
        df = spark.read.format("jdbc") \
            .option("url", jdbc_url) \
            .option("dbtable", f"{schema}.{table}") \
            .option("user", conn_info.get('user', '')) \
            .option("password", conn_info.get('password', '')) \
            .option("driver", driver) \
            .load()

        # 如果有几何列,转换为Sedona几何类型
        geom_column = params.get('geom_column')
        if geom_column and geom_column in df.columns:
            # PostGIS的几何列通常是WKB格式
            from pyspark.sql.functions import expr
            df = df.withColumn(geom_column, expr(f"ST_GeomFromWKB({geom_column})"))
            logger.info(f"Converted geometry column: {geom_column}")

        return df

    @staticmethod
    def save(df: DataFrame, params: Dict[str, Any]):
        """保存到数据库"""
        engine_type, conn_info = DatabaseAdapter._connection_context(params)
        schema = params.get('schema', 'public')
        table = params['table']
        mode = params.get('mode', 'overwrite')

        # 构建JDBC URL
        if engine_type in ['postgresql', 'PostgreSQL']:
            jdbc_url = f"jdbc:postgresql://{conn_info['host']}:{conn_info['port']}/{conn_info['database']}"
            driver = "org.postgresql.Driver"
        elif engine_type in ['mysql', 'MySQL', 'doris', 'Doris']:
            jdbc_url = f"jdbc:mysql://{conn_info['host']}:{conn_info['port']}/{conn_info['database']}"
            driver = "com.mysql.cj.jdbc.Driver"
        else:
            raise ValueError(f"Unsupported engine type: {engine_type}")

        logger.info(f"Saving to database: {jdbc_url}, table: {schema}.{table}, mode: {mode}")

        # 写入表
        df.write.format("jdbc") \
            .option("url", jdbc_url) \
            .option("dbtable", f"{schema}.{table}") \
            .option("user", conn_info.get('user', '')) \
            .option("password", conn_info.get('password', '')) \
            .option("driver", driver) \
            .mode(mode) \
            .save()


class FileAdapter:
    """文件存储适配器 (S3/HDFS/本地)"""

    @staticmethod
    def load(spark: SparkSession, params: Dict[str, Any]) -> DataFrame:
        """从文件加载数据"""
        path = params['path']
        format_type = params.get('format', 'parquet')

        logger.info(f"Loading from file: {path}, format: {format_type}")

        # 如果是S3路径,配置S3访问
        if path.startswith('s3://') or path.startswith('s3a://'):
            FileAdapter._configure_s3_access(spark, params)

        # 读取文件
        if format_type in ['parquet', 'geoparquet']:
            df = spark.read.format("parquet").load(path)
        elif format_type == 'csv':
            df = spark.read.format("csv") \
                .option("header", "true") \
                .option("inferSchema", "true") \
                .load(path)
        elif format_type == 'json':
            df = spark.read.format("json").load(path)
        elif format_type == 'shapefile':
            # 使用Sedona读取Shapefile
            df = spark.read.format("shapefile").load(path)
        elif format_type == 'delta':
            df = spark.read.format("delta").load(path)
        elif format_type == 'hudi':
            df = spark.read.format("hudi").load(path)
        else:
            raise ValueError(f"Unsupported file format: {format_type}")

        # 如果指定了几何列,转换类型
        geom_column = params.get('geom_column')
        if geom_column and geom_column in df.columns:
            from pyspark.sql.functions import expr
            # 尝试从WKT解析
            df = df.withColumn(geom_column, expr(f"ST_GeomFromWKT({geom_column})"))
            logger.info(f"Converted geometry column: {geom_column}")

        return df

    @staticmethod
    def save(df: DataFrame, params: Dict[str, Any]):
        """保存到文件"""
        path = params['path']
        format_type = params.get('format', 'parquet')
        mode = params.get('mode', 'overwrite')

        logger.info(f"Saving to file: {path}, format: {format_type}, mode: {mode}")

        # 写入文件
        if format_type in ['parquet', 'geoparquet']:
            df.write.format("parquet").mode(mode).save(path)
        elif format_type == 'csv':
            df.write.format("csv") \
                .option("header", "true") \
                .mode(mode) \
                .save(path)
        elif format_type == 'json':
            df.write.format("json").mode(mode).save(path)
        elif format_type == 'delta':
            df.write.format("delta").mode(mode).save(path)
        else:
            raise ValueError(f"Unsupported file format: {format_type}")

    @staticmethod
    def _configure_s3_access(spark: SparkSession, params: Dict[str, Any]):
        """配置S3访问凭证"""
        conn_info = params.get('connection_info')
        if not isinstance(conn_info, dict):
            return

        try:
            # 配置S3访问
            spark.conf.set("spark.hadoop.fs.s3a.endpoint", conn_info.get('endpoint', ''))
            spark.conf.set("spark.hadoop.fs.s3a.access.key", conn_info.get('access_key', 'minioadmin'))
            spark.conf.set("spark.hadoop.fs.s3a.secret.key", conn_info.get('secret_key', 'minioadmin'))
            spark.conf.set("spark.hadoop.fs.s3a.path.style.access", "true")

            logger.info(f"Configured S3 access for endpoint: {conn_info.get('endpoint')}")
        except Exception as e:
            logger.warning(f"Failed to configure S3 access: {e}")


class CatalogAdapter:
    """Catalog适配器 (Iceberg/Delta)"""

    @staticmethod
    def load(spark: SparkSession, params: Dict[str, Any]) -> DataFrame:
        """从Catalog加载数据"""
        catalog_name = params['catalog_name']
        database = params['database']
        table = params['table']

        full_table = f"{catalog_name}.{database}.{table}"
        logger.info(f"Loading from catalog: {full_table}")

        return spark.sql(f"SELECT * FROM {full_table}")

"""
Storage Adapters - 统一存储访问适配器
支持: 数据库(JDBC) + 文件(S3/HDFS) + 湖仓(Iceberg/Delta) + Catalog
"""

import logging
import os
from typing import Any, Dict
from urllib.parse import urlencode
import uuid

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
        raw_conn_info = params.get('connection_info')
        if not isinstance(raw_conn_info, dict):
            raise ValueError("source_type=table/target_type=table requires connection_info")

        conn_info = dict(raw_conn_info)
        engine_type = conn_info.get('engine_type')
        if not engine_type:
            raise ValueError("connection_info.engine_type is required")

        shared_host = os.getenv("SPARK_WORKFLOW_SHARED_HOST", "").strip()
        host = str(conn_info.get("host", "")).strip().lower()
        if host in {"localhost", "127.0.0.1", "::1"}:
            if not shared_host:
                raise ValueError(
                    "SPARK_WORKFLOW_SHARED_HOST is required for loopback database connections"
                )
            conn_info["host"] = shared_host

        return engine_type, conn_info

    @staticmethod
    def _jdbc_config(engine_type: str, conn_info: Dict[str, Any]) -> tuple[str, str]:
        normalized_type = engine_type.lower()
        host = conn_info['host']
        port = conn_info['port']
        database = conn_info['database']

        if normalized_type == 'postgresql':
            jdbc_url = f"jdbc:postgresql://{host}:{port}/{database}"
            sslmode = str(conn_info.get('sslmode', '')).strip()
            if sslmode:
                jdbc_url = f"{jdbc_url}?{urlencode({'sslmode': sslmode})}"
            return jdbc_url, "org.postgresql.Driver"
        if normalized_type in {'mysql', 'doris'}:
            return (
                f"jdbc:mysql://{host}:{port}/{database}",
                "com.mysql.cj.jdbc.Driver",
            )

        raise ValueError(f"Unsupported engine type: {engine_type}")

    @staticmethod
    def _geometry_column_names(df: DataFrame) -> list[str]:
        result = []
        for field in df.schema.fields:
            data_type = field.dataType
            type_name = data_type.typeName() if hasattr(data_type, 'typeName') else ''
            if type_name == 'geometry' or type(data_type).__name__ == 'GeometryType':
                result.append(field.name)
        return result

    @staticmethod
    def _doris_field_type(field: Any) -> str:
        data_type = field.dataType
        type_name = data_type.typeName() if hasattr(data_type, 'typeName') else ''
        type_mapping = {
            'string': 'VARCHAR(65533)',
            'byte': 'TINYINT',
            'short': 'SMALLINT',
            'integer': 'INT',
            'long': 'BIGINT',
            'float': 'FLOAT',
            'double': 'DOUBLE',
            'boolean': 'BOOLEAN',
            'date': 'DATE',
            'timestamp': 'DATETIME',
            'timestamp_ntz': 'DATETIME',
        }
        if type_name == 'decimal':
            precision = getattr(data_type, 'precision', 38)
            scale = getattr(data_type, 'scale', 10)
            return f'DECIMAL({precision},{scale})'
        if type_name not in type_mapping:
            raise ValueError(
                f"Doris table write does not support Spark type {type_name!r} "
                f"for field {field.name!r}"
            )
        return type_mapping[type_name]

    @staticmethod
    def _doris_create_table_sql(df: DataFrame, schema: str, table: str) -> str:
        fields = list(df.schema.fields)
        if not fields:
            raise ValueError("Doris table write requires at least one field")

        definitions = []
        for field in fields:
            definition = (
                f"{DatabaseAdapter._spark_identifier(field.name)} "
                f"{DatabaseAdapter._doris_field_type(field)}"
            )
            if not getattr(field, 'nullable', True):
                definition += " NOT NULL"
            definitions.append(definition)

        key_field = fields[0]
        key_identifier = DatabaseAdapter._spark_identifier(key_field.name)
        qualified_table = (
            f"{DatabaseAdapter._spark_identifier(schema)}."
            f"{DatabaseAdapter._spark_identifier(table)}"
        )
        return (
            f"CREATE TABLE IF NOT EXISTS {qualified_table} "
            f"({', '.join(definitions)}) DUPLICATE KEY({key_identifier}) "
            f"DISTRIBUTED BY HASH({key_identifier}) BUCKETS 10"
        )

    @staticmethod
    def _spark_identifier(name: str) -> str:
        return f"`{name.replace('`', '``')}`"

    @staticmethod
    def _postgresql_identifier(name: str) -> str:
        return f'"{name.replace(chr(34), chr(34) * 2)}"'

    @staticmethod
    def _postgresql_table(schema: str, table: str) -> str:
        return (
            f"{DatabaseAdapter._postgresql_identifier(schema)}."
            f"{DatabaseAdapter._postgresql_identifier(table)}"
        )

    @staticmethod
    def _write_jdbc(df: DataFrame, jdbc_url: str, driver: str,
                    conn_info: Dict[str, Any], schema: str, table: str,
                    mode: str) -> None:
        writer = df.write.format("jdbc") \
            .option("url", jdbc_url) \
            .option("dbtable", f"{schema}.{table}") \
            .option("user", conn_info.get('user', conn_info.get('username', ''))) \
            .option("password", conn_info.get('password', '')) \
            .option("driver", driver)
        writer.mode(mode).save()

    @staticmethod
    def _open_jdbc_connection(df: DataFrame, jdbc_url: str,
                              conn_info: Dict[str, Any]):
        jvm = df.sparkSession.sparkContext._jvm
        properties = jvm.java.util.Properties()
        properties.setProperty("user", conn_info.get('user', conn_info.get('username', '')))
        properties.setProperty("password", conn_info.get('password', ''))
        return jvm.java.sql.DriverManager.getConnection(jdbc_url, properties)

    @staticmethod
    def _prepare_doris_table(df: DataFrame, jdbc_url: str,
                             conn_info: Dict[str, Any], schema: str,
                             table: str, mode: str) -> None:
        connection = DatabaseAdapter._open_jdbc_connection(df, jdbc_url, conn_info)
        statement = connection.createStatement()
        qualified_table = (
            f"{DatabaseAdapter._spark_identifier(schema)}."
            f"{DatabaseAdapter._spark_identifier(table)}"
        )
        try:
            statement.execute(
                f"CREATE DATABASE IF NOT EXISTS {DatabaseAdapter._spark_identifier(schema)}"
            )
            if mode == 'overwrite':
                statement.execute(f"DROP TABLE IF EXISTS {qualified_table}")
            statement.execute(
                DatabaseAdapter._doris_create_table_sql(df, schema, table)
            )
        finally:
            statement.close()
            connection.close()

    @staticmethod
    def _drop_postgresql_table(df: DataFrame, jdbc_url: str,
                               conn_info: Dict[str, Any], schema: str,
                               table: str) -> None:
        connection = None
        statement = None
        try:
            connection = DatabaseAdapter._open_jdbc_connection(df, jdbc_url, conn_info)
            statement = connection.createStatement()
            statement.execute(
                f"DROP TABLE IF EXISTS {DatabaseAdapter._postgresql_table(schema, table)}"
            )
        except Exception as error:
            logger.warning("Failed to clean Spark JDBC staging table %s.%s: %s", schema, table, error)
        finally:
            if statement is not None:
                statement.close()
            if connection is not None:
                connection.close()

    @staticmethod
    def _finalize_postgresql_geometry_table(
        df: DataFrame,
        jdbc_url: str,
        conn_info: Dict[str, Any],
        schema: str,
        stage_table: str,
        target_table: str,
        geometry_columns: list[str],
        mode: str,
    ) -> None:
        connection = DatabaseAdapter._open_jdbc_connection(df, jdbc_url, conn_info)
        statement = connection.createStatement()
        stage_ref = DatabaseAdapter._postgresql_table(schema, stage_table)
        target_ref = DatabaseAdapter._postgresql_table(schema, target_table)
        stage_renamed = False
        try:
            connection.setAutoCommit(False)
            conversions = ", ".join(
                f"ALTER COLUMN {DatabaseAdapter._postgresql_identifier(column)} "
                f"TYPE geometry USING ST_GeomFromEWKT({DatabaseAdapter._postgresql_identifier(column)})"
                for column in geometry_columns
            )
            statement.execute(f"ALTER TABLE {stage_ref} {conversions}")

            if mode == 'overwrite':
                statement.execute(f"DROP TABLE IF EXISTS {target_ref}")
                statement.execute(
                    f"ALTER TABLE {stage_ref} RENAME TO "
                    f"{DatabaseAdapter._postgresql_identifier(target_table)}"
                )
                stage_renamed = True
            else:
                metadata = connection.getMetaData()
                tables = metadata.getTables(None, schema, target_table, None)
                target_exists = tables.next()
                tables.close()
                if target_exists:
                    columns = ", ".join(
                        DatabaseAdapter._postgresql_identifier(column)
                        for column in df.columns
                    )
                    statement.execute(
                        f"INSERT INTO {target_ref} ({columns}) "
                        f"SELECT {columns} FROM {stage_ref}"
                    )
                    statement.execute(f"DROP TABLE {stage_ref}")
                else:
                    statement.execute(
                        f"ALTER TABLE {stage_ref} RENAME TO "
                        f"{DatabaseAdapter._postgresql_identifier(target_table)}"
                    )
                    stage_renamed = True
            connection.commit()
        except Exception:
            connection.rollback()
            raise
        finally:
            statement.close()
            connection.close()

        if not stage_renamed and mode == 'append':
            logger.info("Appended spatial rows through staging table %s.%s", schema, stage_table)

    @staticmethod
    def _save_postgresql_geometry(
        df: DataFrame,
        params: Dict[str, Any],
        jdbc_url: str,
        driver: str,
        conn_info: Dict[str, Any],
        geometry_columns: list[str],
    ) -> None:
        from pyspark.sql.functions import expr

        schema = params.get('schema', 'public')
        target_table = params['table']
        mode = params.get('mode', 'overwrite')
        stage_table = f"__addp_spark_stage_{uuid.uuid4().hex[:24]}"
        stage_df = df
        for column in geometry_columns:
            quoted = DatabaseAdapter._spark_identifier(column)
            stage_df = stage_df.withColumn(column, expr(f"ST_AsEWKT({quoted})"))

        try:
            DatabaseAdapter._write_jdbc(
                stage_df, jdbc_url, driver, conn_info, schema, stage_table, 'error'
            )
            DatabaseAdapter._finalize_postgresql_geometry_table(
                stage_df,
                jdbc_url,
                conn_info,
                schema,
                stage_table,
                target_table,
                geometry_columns,
                mode,
            )
        except Exception:
            DatabaseAdapter._drop_postgresql_table(
                stage_df, jdbc_url, conn_info, schema, stage_table
            )
            raise

    @staticmethod
    def load(spark: SparkSession, params: Dict[str, Any]) -> DataFrame:
        """从数据库加载数据"""
        engine_type, conn_info = DatabaseAdapter._connection_context(params)
        schema = params.get('schema', 'public')
        table = params['table']

        jdbc_url, driver = DatabaseAdapter._jdbc_config(engine_type, conn_info)

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
        if mode not in {'overwrite', 'append'}:
            raise ValueError("mode must be overwrite or append")

        jdbc_url, driver = DatabaseAdapter._jdbc_config(engine_type, conn_info)

        logger.info(f"Saving to database: {jdbc_url}, table: {schema}.{table}, mode: {mode}")

        geometry_columns = DatabaseAdapter._geometry_column_names(df)
        if engine_type.lower() == 'postgresql' and geometry_columns:
            DatabaseAdapter._save_postgresql_geometry(
                df, params, jdbc_url, driver, conn_info, geometry_columns
            )
            return

        if engine_type.lower() == 'doris':
            DatabaseAdapter._prepare_doris_table(
                df, jdbc_url, conn_info, schema, table, mode
            )
            DatabaseAdapter._write_jdbc(
                df, jdbc_url, driver, conn_info, schema, table, 'append'
            )
            return

        DatabaseAdapter._write_jdbc(
            df,
            jdbc_url,
            driver,
            conn_info,
            schema,
            table,
            mode,
        )


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

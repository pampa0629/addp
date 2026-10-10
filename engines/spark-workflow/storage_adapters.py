"""
Storage Adapters - 统一存储访问适配器
支持: 数据库(JDBC) + 文件(S3/HDFS) + 湖仓(Iceberg/Delta) + Catalog
"""

from __future__ import annotations

import logging
import os
from typing import Any, Dict
from urllib.parse import urlencode, urlsplit, unquote
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
        elif source_type == 'index':
            from elasticsearch_adapter import ElasticsearchAdapter
            return ElasticsearchAdapter.load(spark, params)
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
            .option("dbtable", DatabaseAdapter._postgresql_table(schema, table)
                    if driver == "org.postgresql.Driver" else f"{schema}.{table}") \
            .option("user", conn_info.get('user', conn_info.get('username', ''))) \
            .option("password", conn_info.get('password', '')) \
            .option("driver", driver)
        writer.mode(mode).save()

    @staticmethod
    def _open_jdbc_connection(spark: SparkSession, jdbc_url: str,
                              conn_info: Dict[str, Any]):
        jvm = spark.sparkContext._jvm
        properties = jvm.java.util.Properties()
        properties.setProperty("user", conn_info.get('user', conn_info.get('username', '')))
        properties.setProperty("password", conn_info.get('password', ''))
        _, driver = DatabaseAdapter._jdbc_config(conn_info['engine_type'], conn_info)
        registry = jvm.org.apache.spark.sql.execution.datasources.jdbc.DriverRegistry
        registry.register(driver)
        return registry.get(driver).connect(jdbc_url, properties)

    @staticmethod
    def _prepare_doris_table(df: DataFrame, jdbc_url: str,
                             conn_info: Dict[str, Any], schema: str,
                             table: str, mode: str) -> None:
        connection = DatabaseAdapter._open_jdbc_connection(df.sparkSession, jdbc_url, conn_info)
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
            connection = DatabaseAdapter._open_jdbc_connection(df.sparkSession, jdbc_url, conn_info)
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
    def _postgresql_query(connection, sql: str, params=()) -> list[tuple]:
        statement = connection.prepareStatement(sql)
        try:
            for index, value in enumerate(params, 1):
                statement.setString(index, value)
            rows = statement.executeQuery()
            try:
                width = rows.getMetaData().getColumnCount()
                result = []
                while rows.next():
                    result.append(tuple(rows.getString(index) for index in range(1, width + 1)))
                return result
            finally:
                rows.close()
        finally:
            statement.close()

    @staticmethod
    def _validate_postgresql_columns(connection, stage_ref: str, target_ref: str,
                                     geometry_columns: list[str]) -> None:
        query = (
            "SELECT attname, atttypid::text, atttypmod::text, attgenerated, attidentity "
            "FROM pg_attribute WHERE attrelid = to_regclass(?) "
            "AND attnum > 0 AND NOT attisdropped"
        )
        stage = {row[0]: row[1:] for row in DatabaseAdapter._postgresql_query(connection, query, (stage_ref,))}
        target = {row[0]: row[1:] for row in DatabaseAdapter._postgresql_query(connection, query, (target_ref,))}
        if stage.keys() != target.keys():
            raise ValueError("PostgreSQL target column set differs from the saved result")
        for name, (type_id, modifier, generated, identity) in target.items():
            source_type, source_modifier, _, _ = stage[name]
            if generated or identity:
                raise ValueError("PostgreSQL save does not support generated or identity target columns")
            if (type_id != source_type or
                    (name not in geometry_columns and modifier not in ('-1', source_modifier))):
                raise ValueError(f"PostgreSQL target column type differs from the saved result: {name}")

    @staticmethod
    def _finalize_postgresql_table(
        df: DataFrame, jdbc_url: str, conn_info: Dict[str, Any], schema: str,
        stage_table: str, target_table: str, geometry_columns: list[str], mode: str, stage_columns: list[str],
    ) -> int:
        connection = DatabaseAdapter._open_jdbc_connection(df.sparkSession, jdbc_url, conn_info)
        statement = None
        stage_ref = DatabaseAdapter._postgresql_table(schema, stage_table)
        target_ref = DatabaseAdapter._postgresql_table(schema, target_table)
        try:
            connection.setAutoCommit(False)
            statement = connection.createStatement()
            for original, staged in zip(df.columns, stage_columns):
                statement.execute(
                    f"ALTER TABLE {stage_ref} RENAME COLUMN {DatabaseAdapter._postgresql_identifier(staged)} "
                    f"TO {DatabaseAdapter._postgresql_identifier(original)}"
                )
            if geometry_columns:
                conversions = ", ".join(
                    f"ALTER COLUMN {DatabaseAdapter._postgresql_identifier(column)} "
                    f"TYPE geometry USING ST_GeomFromEWKT({DatabaseAdapter._postgresql_identifier(column)})"
                    for column in geometry_columns
                )
                statement.execute(f"ALTER TABLE {stage_ref} {conversions}")
            # Serialize publications even before a target exists. The lock is
            # transaction scoped; Spark's distributed stage write holds no target lock.
            DatabaseAdapter._postgresql_query(
                connection, "SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", (target_ref,)
            )
            target = DatabaseAdapter._postgresql_query(
                connection, "SELECT relkind FROM pg_class WHERE oid = to_regclass(?)", (target_ref,)
            )
            if target:
                if target[0][0] != 'r':
                    raise ValueError("PostgreSQL save requires an ordinary table target")
                # Blocks other writers and DDL while allowing MVCC readers.
                statement.execute(f"LOCK TABLE {target_ref} IN SHARE ROW EXCLUSIVE MODE")
                DatabaseAdapter._validate_postgresql_columns(connection, stage_ref, target_ref, geometry_columns)
            staged_rows = int(DatabaseAdapter._postgresql_query(
                connection, f"SELECT COUNT(*) FROM {stage_ref}"
            )[0][0])
            if target:
                columns = ", ".join(DatabaseAdapter._postgresql_identifier(column) for column in df.columns)
                existing_rows = int(DatabaseAdapter._postgresql_query(
                    connection, f"SELECT COUNT(*) FROM {target_ref}"
                )[0][0])
                if mode == 'overwrite':
                    deleted_rows = int(statement.executeLargeUpdate(f"DELETE FROM {target_ref}"))
                    if deleted_rows != existing_rows:
                        raise ValueError("PostgreSQL deleted row count differs from target row count")
                written_rows = int(statement.executeLargeUpdate(
                    f"INSERT INTO {target_ref} ({columns}) SELECT {columns} FROM {stage_ref}"
                ))
                if written_rows != staged_rows:
                    raise ValueError("PostgreSQL inserted row count differs from staging row count")
                expected_rows = written_rows + (existing_rows if mode == 'append' else 0)
                published_rows = int(DatabaseAdapter._postgresql_query(
                    connection, f"SELECT COUNT(*) FROM {target_ref}"
                )[0][0])
                if published_rows != expected_rows:
                    raise ValueError("PostgreSQL published row count differs from expected row count")
                statement.execute(f"DROP TABLE {stage_ref}")
            else:
                statement.execute(
                    f"ALTER TABLE {stage_ref} RENAME TO {DatabaseAdapter._postgresql_identifier(target_table)}"
                )
                written_rows = staged_rows
            connection.commit()
            return written_rows
        except Exception:
            connection.rollback()
            raise
        finally:
            if statement is not None:
                statement.close()
            connection.close()

    @staticmethod
    def _save_postgresql(
        df: DataFrame, params: Dict[str, Any], jdbc_url: str, driver: str,
        conn_info: Dict[str, Any], geometry_columns: list[str],
    ) -> int:
        schema = params.get('schema', 'public')
        target_table = params['table']
        names = [schema, target_table, *df.columns]
        if any(not isinstance(name, str) or not name or '\x00' in name or
               len(name.encode('utf-8')) > 63 for name in names) or len(set(df.columns)) != len(df.columns) or not df.columns:
            raise ValueError("PostgreSQL save requires unique nonempty column names and identifiers of at most 63 bytes")
        stage_table = f"__addp_spark_stage_{uuid.uuid4().hex[:24]}"
        stage_df = df
        if geometry_columns:
            from pyspark.sql.functions import expr
            for column in geometry_columns:
                quoted = DatabaseAdapter._spark_identifier(column)
                stage_df = stage_df.withColumn(column, expr(f"ST_AsEWKT({quoted})"))
        # Spark 3.5's PostgreSQL JDBC dialect does not escape embedded quotes
        # in column names. Use private stage aliases, then restore physical names
        # with PostgreSQL identifier quoting before type/constraint validation.
        while True:
            prefix = '__addp_col_' + uuid.uuid4().hex[:16]
            stage_columns = [f'{prefix}_{index}' for index in range(len(df.columns))]
            if not set(stage_columns).intersection(df.columns):
                break
        try:
            DatabaseAdapter._write_jdbc(stage_df.toDF(*stage_columns), jdbc_url, driver, conn_info, schema, stage_table, 'error')
            return DatabaseAdapter._finalize_postgresql_table(
                stage_df, jdbc_url, conn_info, schema, stage_table, target_table,
                geometry_columns, params.get('mode', 'overwrite'), stage_columns,
            )
        except Exception:
            DatabaseAdapter._drop_postgresql_table(stage_df, jdbc_url, conn_info, schema, stage_table)
            raise

    @staticmethod
    def load(spark: SparkSession, params: Dict[str, Any]) -> DataFrame:
        """从数据库加载数据"""
        engine_type, conn_info = DatabaseAdapter._connection_context(params)
        schema = params.get('schema', 'public')
        table = params['table']

        jdbc_url, driver = DatabaseAdapter._jdbc_config(engine_type, conn_info)

        logger.info(f"Loading from database: {jdbc_url}, table: {schema}.{table}")

        table_ref = f"{schema}.{table}"
        columns = []
        if engine_type.lower() == 'postgresql':
            table_ref = DatabaseAdapter._postgresql_table(schema, table)
            connection = DatabaseAdapter._open_jdbc_connection(spark, jdbc_url, conn_info)
            try:
                columns = [row[0] for row in DatabaseAdapter._postgresql_query(connection,
                    "SELECT attname FROM pg_attribute WHERE attrelid = to_regclass(?) "
                    "AND attnum > 0 AND NOT attisdropped ORDER BY attnum", (table_ref,))]
            finally:
                connection.close()
            if not columns:
                raise ValueError("PostgreSQL source table has no readable columns")
            projection = ", ".join(
                f"{DatabaseAdapter._postgresql_identifier(name)} AS \"__addp_col_{index}\""
                for index, name in enumerate(columns)
            )
            table_ref = f'(SELECT {projection} FROM {table_ref}) AS addp_source'
        df = spark.read.format("jdbc") \
            .option("url", jdbc_url) \
            .option("dbtable", table_ref) \
            .option("user", conn_info.get('user', conn_info.get('username', ''))) \
            .option("password", conn_info.get('password', '')) \
            .option("driver", driver) \
            .load()
        if columns:
            df = df.toDF(*columns)

        # 如果有几何列,转换为Sedona几何类型
        geom_column = params.get('geom_column')
        if geom_column and geom_column in df.columns:
            # PostGIS的几何列通常是WKB格式
            from pyspark.sql.functions import expr
            df = df.withColumn(geom_column, expr(f"ST_GeomFromWKB({DatabaseAdapter._spark_identifier(geom_column)})"))
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
        if engine_type.lower() == 'postgresql':
            return DatabaseAdapter._save_postgresql(
                df, params, jdbc_url, driver, conn_info, geometry_columns
            )

        row_count = df.count()
        if engine_type.lower() == 'doris':
            DatabaseAdapter._prepare_doris_table(
                df, jdbc_url, conn_info, schema, table, mode
            )
            DatabaseAdapter._write_jdbc(
                df, jdbc_url, driver, conn_info, schema, table, 'append'
            )
            return row_count

        DatabaseAdapter._write_jdbc(
            df,
            jdbc_url,
            driver,
            conn_info,
            schema,
            table,
            mode,
        )
        return row_count


class FileAdapter:
    """文件存储适配器 (S3/HDFS/本地)"""

    @staticmethod
    def load(spark: SparkSession, params: Dict[str, Any]) -> DataFrame:
        """从文件加载数据"""
        path = params['path']
        format_type = params['format']

        if path.startswith('hdfs:') or params.get('connection_info', {}).get('engine_type') == 'hdfs':
            path = FileAdapter._validate_hdfs_access(spark, params)

        logger.info(f"Loading from file: {path}, format: {format_type}")

        options = {}
        if path.startswith(('s3:', 's3a:')):
            path, options = FileAdapter._s3_access(params)
        reader = spark.read.options(**options)

        # 读取文件
        if format_type in ['parquet', 'geoparquet']:
            df = reader.format("parquet").load(path)
        elif format_type == 'csv':
            df = reader.format("csv") \
                .option("header", "true") \
                .option("inferSchema", "true") \
                .load(path)
        elif format_type == 'json':
            df = reader.format("json").load(path)
        elif format_type == 'shapefile':
            # 使用Sedona读取Shapefile
            df = reader.format("shapefile").load(path)
        elif format_type == 'delta':
            df = reader.format("delta").load(path)
        elif format_type == 'hudi':
            df = reader.format("hudi").load(path)
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
        if path.startswith('hdfs:') or params.get('connection_info', {}).get('engine_type') == 'hdfs':
            raise ValueError("HDFS engine supports reading only")
        if path.startswith(('s3:', 's3a:')):
            return FileAdapter._save_s3_parquet(df, params)
        format_type = params.get('format', 'parquet')
        mode = params.get('mode', 'overwrite')

        logger.info(f"Saving to file: {path}, format: {format_type}, mode: {mode}")

        if format_type not in {'parquet', 'geoparquet', 'csv', 'json', 'delta'}:
            raise ValueError(f"Unsupported file format: {format_type}")
        row_count = df.count()
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
        return row_count

    @staticmethod
    def _validate_hdfs_access(spark: SparkSession, params: Dict[str, Any]):
        conn = params.get('connection_info')
        if not isinstance(conn, dict) or conn.get('engine_type') != 'hdfs' or conn.get('authentication') != 'simple':
            raise ValueError("HDFS requires explicit Simple engine connection facts")
        source, rpc = urlsplit(params['path']), urlsplit(conn.get('rpc_uri', ''))
        root = conn.get('root_path', '')
        physical = unquote(source.path)
        if (source.scheme != 'hdfs' or rpc.scheme != 'hdfs' or source.netloc != rpc.netloc
                or not source.hostname or not source.port or source.username or source.query or source.fragment
                or rpc.username or rpc.query or rpc.fragment or rpc.path not in ('', '/')
                or not root.startswith('/') or (root != '/' and root.endswith('/'))
                or any(part in ('.', '..', '') for part in root.split('/')[1:] if root != '/')
                or '\x00' in physical or any(part in ('.', '..', '') for part in physical.split('/')[1:])
                or not (physical == root or physical.startswith(root.rstrip('/') + '/'))):
            raise ValueError("HDFS path is outside the engine management root or RPC authority")
        ugi = spark.sparkContext._jvm.org.apache.hadoop.security.UserGroupInformation
        if ugi.isSecurityEnabled() or ugi.getCurrentUser().getShortUserName() != conn.get('user'):
            raise ValueError("HDFS Simple user differs from the fixed Spark application Hadoop identity")
        if any(char in physical for char in '*?[]{}\\'):
            raise ValueError("HDFS source must be a literal resource; Hadoop glob characters are unsupported")
        # Spark passes strings to Hadoop Path(String), which escapes literal names.
        # Decode the derived URI once, after validating its physical root boundary.
        return source.scheme + '://' + source.netloc + physical

    @staticmethod
    def _s3_access(params):
        """Operation-local S3A configuration; never mutate a shared Spark session."""
        conn = params.get('connection_info')
        source = urlsplit(params['path'])
        if (not isinstance(conn, dict) or conn.get('engine_type') not in {'minio', 's3'}
                or source.scheme != 's3a' or not source.netloc or source.username
                or source.port or source.query or source.fragment):
            raise ValueError('S3A requires explicit authorized storage connection facts')
        key = unquote(source.path).lstrip('/')
        if (not key or any(part in {'', '.', '..'} for part in key.split('/'))
                or any(char in key for char in '*?[]{}\\\x00')):
            raise ValueError('S3A resource must be a literal nonempty object or scope')
        bucket = conn.get('bucket')
        if bucket and bucket != source.netloc:
            raise ValueError('S3A resource is outside the engine bucket')
        endpoint = conn.get('endpoint', '')
        # MinIO ConnectionSpec declares use_ssl=false as its canonical default.
        secure = conn.get('use_ssl', False)
        if not isinstance(secure, bool) or not isinstance(endpoint, str) or not endpoint:
            raise ValueError('S3A requires endpoint and boolean use_ssl')
        parsed = urlsplit(endpoint if '://' in endpoint else ('https://' if secure else 'http://') + endpoint)
        if (parsed.scheme != ('https' if secure else 'http') or not parsed.hostname
                or parsed.username or parsed.path not in {'', '/'} or parsed.query or parsed.fragment):
            raise ValueError('S3A endpoint conflicts with storage connection facts')
        if not all(isinstance(conn.get(name), str) and conn[name] for name in ('access_key', 'secret_key')):
            raise ValueError('S3A requires explicit access credentials')
        options = {
            'fs.s3a.endpoint': parsed.scheme + '://' + parsed.netloc,
            'fs.s3a.access.key': conn['access_key'], 'fs.s3a.secret.key': conn['secret_key'],
            'fs.s3a.aws.credentials.provider': 'org.apache.hadoop.fs.s3a.SimpleAWSCredentialsProvider',
            'fs.s3a.connection.ssl.enabled': str(secure).lower(),
            'fs.s3a.path.style.access': 'true', 'fs.s3a.impl.disable.cache': 'true',
            'fs.s3a.impl': 'org.apache.hadoop.fs.s3a.S3AFileSystem',
            'fs.s3a.committer.name': 'magic', 'fs.s3a.committer.magic.enabled': 'true',
            'fs.s3a.committer.abort.pending.uploads': 'false',
            'mapreduce.outputcommitter.factory.scheme.s3a': 'org.apache.hadoop.fs.s3a.commit.S3ACommitterFactory',
        }
        # Bucket overrides inherited from a cluster must not replace authorized facts.
        options.update({
            'fs.s3a.bucket.' + source.netloc + '.' + name[len('fs.s3a.'):]: value
            for name, value in list(options.items())
            if name.startswith('fs.s3a.') and not name.startswith('fs.s3a.impl')
        })
        return 's3a://' + source.netloc + '/' + key, options

    @staticmethod
    def _s3_filesystem(spark, path, options):
        jvm = spark.sparkContext._jvm
        conf = jvm.org.apache.hadoop.conf.Configuration(spark.sparkContext._jsc.hadoopConfiguration())
        for name, value in options.items():
            conf.set(name, value)
        # newInstance bypasses the bucket-only FileSystem cache as well.
        filesystem = jvm.org.apache.hadoop.fs.FileSystem.newInstance(jvm.org.apache.hadoop.fs.Path(path).toUri(), conf)
        return filesystem, conf

    @staticmethod
    def _verify_s3_parquet(spark, filesystem, conf, path, options, expected_schema):
        jvm = spark.sparkContext._jvm
        jpath = jvm.org.apache.hadoop.fs.Path(path)
        success = jvm.org.apache.hadoop.fs.s3a.commit.files.SuccessData.load(
            filesystem, jvm.org.apache.hadoop.fs.Path(jpath, '_SUCCESS'))
        if success.getCommitter() != 'magic':
            raise ValueError('Parquet result was not committed by the S3A Magic Committer')
        files = filesystem.listFiles(jpath, True)
        schema, count, parts = None, 0, 0
        while files.hasNext():
            status = files.next()
            name = status.getPath().getName()
            if name == '_SUCCESS':
                continue
            if not name.startswith('part-') or not name.endswith('.parquet'):
                raise ValueError('Parquet result contains unexpected or uncommitted content')
            source = jvm.org.apache.parquet.hadoop.util.HadoopInputFile.fromPath(status.getPath(), conf)
            reader = jvm.org.apache.parquet.hadoop.ParquetFileReader.open(source)
            try:
                footer = reader.getFooter()
                current = footer.getFileMetaData().getSchema()
                if schema is not None and not schema.equals(current):
                    raise ValueError('Parquet result contains incompatible schemas')
                schema = current
                for block in footer.getBlocks():
                    count += block.getRowCount()
                parts += 1
            finally:
                reader.close()
        if not parts:
            raise ValueError('Parquet result contains no data files')
        actual = spark.read.options(**options).parquet(path).schema
        if actual.simpleString() != expected_schema.simpleString():
            raise ValueError('Parquet result schema differs from the submitted DataFrame')
        return count

    @staticmethod
    def _cleanup_s3_result(spark, filesystem, path):
        # Hadoop Path strings are decoded: '#' is a literal key character.
        bucket, key = path.split('/', 3)[2:]
        prefix = key + '/'
        jvm = spark.sparkContext._jvm
        records = jvm.org.apache.hadoop.fs.s3a.commit.files
        jpath = filesystem.keyToQualifiedPath(key)
        uploads = set()
        if filesystem.exists(jpath):
            files = filesystem.listFiles(jpath, True)
            while files.hasNext():
                record_path = files.next().getPath()
                name = record_path.getName()
                if name.endswith('.pending'):
                    commits = [records.SinglePendingCommit.load(filesystem, record_path)]
                elif name.endswith('.pendingset'):
                    commits = records.PendingSet.load(filesystem, record_path).getCommits()
                else:
                    continue
                # Validate all records before aborting any upload or deleting evidence.
                for commit in commits:
                    destination, upload = commit.getDestinationKey(), commit.getUploadId()
                    if (commit.getBucket() != bucket or not destination.startswith(prefix)
                            or not destination[len(prefix):] or not upload
                            or any(part in {'', '.', '..'} for part in destination.split('/'))):
                        raise ValueError('Pending upload record is outside the owned result directory')
                    uploads.add((destination, upload))
        if not uploads:
            success = records.SuccessData.load(filesystem, jvm.org.apache.hadoop.fs.Path(jpath, '_SUCCESS'))
            if success.getCommitter() != 'magic':
                raise ValueError('No pending records or valid Magic completion evidence')
        helper = filesystem.getWriteOperationHelper()
        for destination, upload in sorted(uploads):
            try:
                helper.abortMultipartCommit(destination, upload)
            except Exception as error:
                # Hadoop translates exact NoSuchUpload to FileNotFoundException:
                # already completed/aborted IDs no longer own a pending upload.
                java_error = getattr(error, 'java_exception', None)
                if java_error is None or java_error.getClass().getName() != 'java.io.FileNotFoundException':
                    raise
        if filesystem.exists(jpath) and not filesystem.delete(jpath, True):
            raise RuntimeError('Unable to delete incomplete result directory')
        if filesystem.exists(jpath):
            raise RuntimeError('Incomplete result directory remains')

    @staticmethod
    def _save_s3_parquet(df, params):
        if params.get('format', 'parquet') != 'parquet' or params.get('mode') != 'create':
            raise ValueError('MinIO results require ordinary Parquet and create mode')
        path, options = FileAdapter._s3_access(params)
        if params['connection_info']['engine_type'] != 'minio':
            raise ValueError('Object result publication currently requires a MinIO engine')
        spark = df.sparkSession
        filesystem, conf = FileAdapter._s3_filesystem(spark, path, options)
        try:
            if filesystem.exists(spark.sparkContext._jvm.org.apache.hadoop.fs.Path(path)):
                raise ValueError('Result directory already exists')
            try:
                df.write.options(**options).format('parquet').mode('errorifexists').save(path)
                return FileAdapter._verify_s3_parquet(spark, filesystem, conf, path, options, df.schema)
            except Exception:
                try:
                    FileAdapter._cleanup_s3_result(spark, filesystem, path)
                except Exception as cleanup_error:
                    raise RuntimeError('Parquet publication failed; owned result cleanup unverified') from cleanup_error
                raise
        finally:
            filesystem.close()


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

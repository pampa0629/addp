"""Real Worker + disposable MySQL/Doris publication oracle (owner gate only)."""
import socket
import sys
from decimal import Decimal
from datetime import datetime
from unittest.mock import patch

sys.path.insert(0, '/addp/spark-workflow')
from pyspark.sql import SparkSession, functions as F, types as T
from storage_adapters import DatabaseAdapter
from mysql_publication import PublicationUncertain, query, table_ref

spark = SparkSession.builder.appName('ADDP staged table publication T2').getOrCreate()
spark.sparkContext.setLogLevel('WARN')
schema = T.StructType([T.StructField('id', T.LongType(), False),
                       T.StructField('value', T.DecimalType(38, 18), False)])
frame = spark.createDataFrame([(9007199254740993, Decimal('100.123456789012345678')),
                              (-9007199254740993, Decimal('200.987654321098765432'))], schema).repartition(2)


def run(engine):
    conn_info = {'engine_type': engine, 'host': engine, 'port': 3306 if engine == 'mysql' else 9030,
                 'http_port': 8030, 'database': 'spark_results', 'user': 'root',
                 'password': 'owned-spark-only' if engine == 'mysql' else ''}
    jdbc_url, driver = DatabaseAdapter._jdbc_config(engine, conn_info)
    connection = DatabaseAdapter._open_jdbc_connection(spark, jdbc_url, conn_info)
    def sql(command):
        statement = connection.createStatement()
        try:
            statement.execute(command)
        finally:
            statement.close()
    def read(table):
        return query(connection, f'SELECT id, value FROM {table_ref("spark_results", table)} ORDER BY id')
    def stages():
        return query(connection, "SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA = 'spark_results' AND LEFT(TABLE_NAME, 19) = '__addp_spark_stage_'")
    def clear_evidence():
        for (name,) in stages():
            sql(f'DROP TABLE {table_ref("spark_results", name)}')
    def save(data, table, mode='overwrite'):
        return DatabaseAdapter.save(data, {'connection_info': conn_info, 'schema': 'spark_results',
                                          'table': table, 'mode': mode})
    target = engine + '_target'
    target_ref = table_ref('spark_results', target)
    try:
        if engine == 'mysql':
            sql(f"CREATE TABLE {target_ref} (id BIGINT PRIMARY KEY, value DECIMAL(38,18) NOT NULL, "
                "KEY amount_index(value)) ENGINE=InnoDB COMMENT='preserve target'")
        else:
            sql(DatabaseAdapter._doris_create_table_sql(frame, 'spark_results', target))
        original_ddl = query(connection, f'SHOW CREATE TABLE {target_ref}')[0][1]
        assert save(frame, target) == 2
        assert query(connection, f'SHOW CREATE TABLE {target_ref}')[0][1] == original_ddl
        assert read(target) == [('-9007199254740993', '200.987654321098765432'),
                                ('9007199254740993', '100.123456789012345678')]
        extra = spark.createDataFrame([(7, Decimal('3.000000000000000001'))], schema)
        assert save(extra, target, 'append') == 1
        before = read(target)
        assert len(before) == 3
        assert save(frame.limit(0), target, 'append') == 0
        assert read(target) == before
        try:
            save(frame.withColumn('other', F.lit(1)), target)
        except Exception:
            pass
        else:
            raise AssertionError('incompatible target schema was accepted')
        assert read(target) == before
        failing = frame.withColumn('value', F.expr(
            "cast(case when id > 0 then raise_error('injected Worker failure') else value end as decimal(38,18))"))
        try:
            save(failing, target)
        except Exception:
            pass
        else:
            raise AssertionError('Worker failure was accepted')
        assert read(target) == before
        assert not stages(), 'failed staging left tables'
        assert save(frame, engine + '_new') == 2
        assert read(engine + '_new') == read_target_baseline
        if engine == 'mysql':
            temporal = spark.createDataFrame([(datetime(2024, 1, 2, 3, 4, 5, 123456),)],
                T.StructType([T.StructField('at', T.TimestampType(), False)]))
            assert save(temporal, 'mysql_temporal') == 1
            assert query(connection, "SELECT DATE_FORMAT(at, '%Y-%m-%d %H:%i:%s.%f') FROM spark_results.mysql_temporal") == [('2024-01-02 03:04:05.123456',)]
            original_mode = query(connection, 'SELECT @@GLOBAL.sql_mode')[0][0]
            sql("SET GLOBAL sql_mode = ''")
            try:
                out_of_range = spark.createDataFrame([(datetime(2075, 1, 1),)], temporal.schema)
                try:
                    save(out_of_range, 'mysql_temporal')
                except Exception:
                    pass
                else:
                    raise AssertionError('non-strict server silently coerced a timestamp')
                assert query(connection, "SELECT DATE_FORMAT(at, '%Y-%m-%d %H:%i:%s.%f') FROM spark_results.mysql_temporal") == [('2024-01-02 03:04:05.123456',)]
            finally:
                sql("SET GLOBAL sql_mode = '" + original_mode.replace("'", "''") + "'")
            unusual = spark.createDataFrame([(1, '精确😀')], ['a`b', '中文列'])
            assert save(unusual, 'mysql_quoted') == 1
            assert query(connection, 'SELECT `a``b`, `中文列` FROM spark_results.mysql_quoted') == [('1', '精确😀')]
            # Duplicate PK fails after DELETE inside the publication transaction.
            try:
                save(frame.union(frame), target)
            except Exception:
                pass
            else:
                raise AssertionError('constraint violation was accepted')
            assert read(target) == before
            assert not stages()
            factory = DatabaseAdapter._open_jdbc_connection
            class LostCommitResponse:
                def __init__(self, actual): self.actual = actual
                def __getattr__(self, name): return getattr(self.actual, name)
                def commit(self):
                    self.actual.commit()
                    raise OSError('injected commit acknowledgement loss')
            with patch.object(DatabaseAdapter, '_open_jdbc_connection', side_effect=lambda *args: LostCommitResponse(factory(*args))):
                try:
                    save(frame, target)
                except PublicationUncertain:
                    pass
                else:
                    raise AssertionError('lost commit response was reported successful')
            assert read(target) == read_target_baseline, 'actual commit was not retained'
            assert stages(), 'uncertain publication lost stage evidence'
            clear_evidence()
            sql(f"CREATE TRIGGER protect_target BEFORE INSERT ON {target_ref} FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected target rule'")
            try:
                save(extra, target)
            except Exception:
                pass
            else:
                raise AssertionError('target trigger rejection was ignored')
            sql('DROP TRIGGER spark_results.protect_target')
            assert read(target) == read_target_baseline
        assert save(frame.limit(0), target) == 0
        assert not read(target)
        assert query(connection, f'SHOW CREATE TABLE {target_ref}')[0][1] == original_ddl
        assert not stages()
        print(f'SPARK_{engine.upper()}_PUBLICATION_PASS overwrite append empty schema failure cleanup bigint decimal', flush=True)
    finally:
        clear_evidence()
        connection.close()


read_target_baseline = [('-9007199254740993', '200.987654321098765432'),
                        ('9007199254740993', '100.123456789012345678')]
try:
    workers = spark.sparkContext.parallelize([1, 2], 2).map(lambda _: socket.gethostname()).collect()
    assert workers and all(host != socket.gethostname() for host in workers), workers
    print('SPARK_TABLE_REAL_WORKER ' + ','.join(sorted(set(workers))), flush=True)
    selected = sys.argv[1] if len(sys.argv) == 2 else 'all'
    if selected in {'all', 'mysql'}: run('mysql')
    if selected in {'all', 'doris'}: run('doris')
finally:
    spark.stop()

"""Fixed Spark 3.5 native feasibility probe, not the product authorization layer."""
from decimal import Decimal
import json
from pathlib import Path
import socket
import sys

from pyspark.sql import SparkSession
from pyspark.sql.types import DecimalType, LongType


def bind_source(spark, alias, url, driver):
    # A native connection probe distinguishes credential/transport failures from
    # JDBCTableCatalog's generic NoSuchTable wrapper before trying federation.
    properties = spark._jvm.java.util.Properties()
    properties.setProperty('user', 'reader')
    properties.setProperty('password', 'admission-reader-only')
    jdbc_driver = (spark._jvm.org.postgresql.Driver() if alias == 'pg'
                   else spark._jvm.com.mysql.cj.jdbc.Driver())
    connection = jdbc_driver.connect(url, properties)
    try:
        statement = connection.createStatement()
        try:
            result = statement.executeQuery('SELECT 1')
            try:
                assert result.next() and result.getInt(1) == 1
            finally:
                result.close()
            if alias == 'mysql':
                result = statement.executeQuery("SHOW SESSION STATUS LIKE 'Ssl_cipher'")
                try:
                    assert result.next() and result.getString(2), 'MySQL admission must use TLS'
                finally:
                    result.close()
        finally:
            statement.close()
    finally:
        connection.close()
    # Execution-time SQLConf, never submit command arguments or SQL text.
    prefix = 'spark.sql.catalog.' + alias
    for key, value in {
        '': 'org.apache.spark.sql.execution.datasources.v2.jdbc.JDBCTableCatalog',
        '.url': url, '.driver': driver, '.user': 'reader',
        '.password': 'admission-reader-only',
    }.items():
        spark.conf.set(prefix + key, value)


def main(phase):
    if phase == 'supervised':
        from query_process import run_driver
        result = run_driver([sys.executable, __file__, 'child'], {'value': 9007199254740993}, timeout=90)
        assert result['value'] == 9007199254740993
        assert result['distributed'] is True
        assert not Path(result['directory']).exists()
        print('SPARK_FEDERATION_SUPERVISED_DRIVER_PASS real_jvm worker private_stdin cleanup', flush=True)
        return
    if phase == 'child':
        request = json.load(sys.stdin)
        spark = (SparkSession.builder.appName('ADDP-supervised-query-admission')
                 .master('spark://spark-master:7077')
                 .config('spark.driver.host', 'spark-master')
                 .config('spark.executor.memory', '512m').config('spark.cores.max', '2')
                 .config('spark.ui.enabled', 'false').getOrCreate())
        try:
            workers = spark.sparkContext.parallelize([1], 1).map(lambda _: socket.gethostname()).collect()
            rows = spark.sql('SELECT CAST(? AS BIGINT) AS value', args=[request['value']]).collect()
            Path(sys.argv[2]).write_text(json.dumps({'value': rows[0].value,
                'distributed': bool(workers) and all(host != socket.gethostname() for host in workers),
                'directory': str(Path.cwd())}))
        finally:
            spark.stop()
        return
    if phase not in {'query', 'fresh'}:
        raise ValueError('unknown admission phase')
    spark = SparkSession.builder.appName('ADDP-federation-admission-' + phase).getOrCreate()
    spark.sparkContext.setLogLevel('ERROR')
    try:
        workers = spark.sparkContext.parallelize([1, 2], 2).map(lambda _: socket.gethostname()).collect()
        assert workers and all(host != socket.gethostname() for host in workers), workers
        if phase == 'fresh':
            assert spark.conf.get('spark.sql.catalog.pg', None) is None
            assert spark.conf.get('spark.sql.catalog.mysql', None) is None
            assert not spark.catalog.tableExists('admission_previous_query')
            print('SPARK_FEDERATION_FRESH_DRIVER_PASS', flush=True)
            return
        bind_source(spark, 'pg', 'jdbc:postgresql://postgres:5432/addp_test', 'org.postgresql.Driver')
        bind_source(spark, 'mysql', 'jdbc:mysql://mysql:3306/addp_test?sslMode=REQUIRED', 'com.mysql.cj.jdbc.Driver')
        query = '''SELECT o.id, o.amount, c.label
            FROM pg.public.orders o JOIN mysql.addp_test.customers c
            ON o.customer_id = c.id WHERE o.customer_id >= ? ORDER BY o.id'''
        result = spark.sql(query, args=[1])
        assert result.schema['id'].dataType == LongType(), result.schema
        assert result.schema['amount'].dataType == DecimalType(38, 18), result.schema
        rows = [tuple(row) for row in result.collect()]
        assert rows == [
            (-9007199254740993, Decimal('-200.987654321098765432'), None),
            (9007199254740993, Decimal('100.123456789012345678'), '中文  '),
        ], rows
        filtered = spark.sql(query, args=[2]).collect()
        assert len(filtered) == 1 and filtered[0].id == -9007199254740993
        result.createOrReplaceTempView('admission_previous_query')
        print('SPARK_FEDERATION_JOIN_PASS worker bigint decimal null trailing_spaces parameters', flush=True)
    finally:
        spark.stop()


if __name__ == '__main__':
    main(sys.argv[1])

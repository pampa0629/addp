"""Fixed Spark 3.5 native feasibility probe, not the product authorization layer."""
from decimal import Decimal
import json
from pathlib import Path
import socket
import sys

from pyspark.sql import SparkSession
from pyspark.sql.types import DecimalType, LongType
from query_guard import QueryValidationError, validate_query


def verify_native_guard(spark):
    class ParserOnlySession:
        version = spark.version
        _jsparkSession = spark._jsparkSession

        def sql(self, *args, **kwargs):
            raise AssertionError('validation must not execute SQL')

    session = ParserOnlySession()
    allowed = [
        ('SELECT 1 AS n', set()),
        ('SELECT o.id, abs(o.amount) FROM pg.public.orders o WHERE o.customer_id >= ?', {'pg'}),
        ('SELECT count(*), sum(amount) FROM pg.public.orders GROUP BY customer_id HAVING count(*) > 0', {'pg'}),
        ('SELECT id FROM pg.public.orders UNION ALL SELECT id FROM mysql.addp_test.customers', {'pg', 'mysql'}),
        ('WITH a AS (SELECT id FROM pg.public.orders), b AS (SELECT * FROM a) SELECT * FROM b', {'pg'}),
        ('SELECT (SELECT max(id) FROM pg.public.orders) AS id', {'pg'}),
        ('SELECT id FROM pg.public.orders WHERE EXISTS (SELECT 1 FROM mysql.addp_test.customers)', {'pg', 'mysql'}),
        ('SELECT id FROM pg.public.orders WHERE customer_id IN (SELECT id FROM mysql.addp_test.customers)', {'pg', 'mysql'}),
        ("SELECT CASE WHEN id IS NULL THEN 0 ELSE CAST(id AS BIGINT) END FROM pg.public.orders ORDER BY id DESC LIMIT 2", {'pg'}),
    ]
    for query, expected in allowed:
        try:
            result = validate_query(session, query, {'pg', 'mysql'})
        except QueryValidationError as error:
            # This is a fixed disposable fixture, never a user query. Native
            # class identities make version-specific admission failures clear.
            plan = spark._jsparkSession.sessionState().sqlParser().parsePlan(query)
            raise AssertionError('supported native fixture rejected: ' + plan.toJSON()) from error
        assert result.sql == query and result.source_catalogs == expected, query
    denied = [
        'CREATE TEMP VIEW forbidden_view AS SELECT 1',
        'DROP TABLE pg.public.orders', 'DELETE FROM pg.public.orders',
        'INSERT INTO pg.public.orders VALUES (1,1,1)',
        'UPDATE pg.public.orders SET customer_id = 99',
        "SET spark.sql.shuffle.partitions=999", 'SHOW DATABASES',
        "ADD JAR '/tmp/forbidden.jar'", "SELECT * FROM parquet.`/tmp/forbidden.parquet`",
        'SELECT * FROM spark_catalog.default.forbidden', 'SELECT * FROM public.orders',
        'SELECT * FROM global_temp.forbidden', 'SELECT * FROM range(3)',
        "SELECT reflect('java.lang.System','getenv','PATH')",
        "SELECT java_method('java.lang.System','getenv','PATH')",
        'SELECT pg.public.custom_function(1)', 'SELECT explode(array(1,2))',
        'WITH unused AS (SELECT * FROM unauthorized.public.orders) SELECT 1',
        "WITH unused AS (SELECT reflect('java.lang.System','getenv','PATH')) SELECT 1",
        'SELECT (SELECT count(*) FROM unauthorized.public.orders)',
        'SELECT 1 WHERE EXISTS (SELECT * FROM unauthorized.public.orders)',
        'SELECT 1 WHERE 1 IN (SELECT id FROM unauthorized.public.orders)',
        'SELECT * FROM (SELECT * FROM unauthorized.public.orders) nested',
        'WITH a AS (SELECT * FROM a) SELECT * FROM a',
        'WITH a AS (SELECT * FROM b), b AS (SELECT 1) SELECT * FROM a',
        'SELECT * FROM pg.public.orders VERSION AS OF 1',
        'SELECT 1; DROP TABLE pg.public.orders',
        'SELECT /*+ REPARTITION(1) */ * FROM pg.public.orders',
    ]
    for query in denied:
        try:
            validate_query(session, query, {'pg', 'mysql'})
        except QueryValidationError as error:
            assert query not in str(error), 'diagnostics must not expose SQL'
        else:
            raise AssertionError('native guard admitted forbidden SQL: ' + query)
    assert not spark.catalog.tableExists('forbidden_view')
    assert spark.conf.get('spark.sql.shuffle.partitions') != '999'
    print('SPARK_FEDERATION_NATIVE_GUARD_PASS commands paths udf tvf nested_cte subqueries parameters no_sql_execution', flush=True)


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


def _jdbc_properties(spark, user, password):
    properties = spark._jvm.java.util.Properties()
    properties.setProperty('user', user)
    properties.setProperty('password', password)
    return properties


def main(phase):
    if phase == 'driver':
        from query_process import PreparedDriver, QueryDriverError
        request = {
            'master_url': 'spark://spark-master:7077', 'driver_host': 'spark-master',
            'catalogs': [
                {'engine_id': 11, 'engine_type': 'postgresql', 'alias': 'Business_PostgreSQL', 'url': 'jdbc:postgresql://postgres:5432/addp_test',
                 'driver': 'org.postgresql.Driver', 'user': 'reader', 'password': 'admission-reader-only'},
                {'engine_id': 12, 'engine_type': 'mysql', 'alias': 'Business_MySQL', 'url': 'jdbc:mysql://mysql:3306/addp_test?sslMode=REQUIRED',
                 'driver': 'com.mysql.cj.jdbc.Driver', 'user': 'reader', 'password': 'admission-reader-only'},
            ],
            'query': '''SELECT o.id, o.amount, c.`中文``label` AS label
                FROM `Business_PostgreSQL`.` schema.with dot `.` 中文"table`` ` o JOIN `Business_MySQL`.addp_test.`custom``ers` c
                ON o.` customer"id ` = c.`customer``id` WHERE o.` customer"id ` >= ? ORDER BY o.id''',
            'args': [1], 'limit': 10,
        }
        prepared = PreparedDriver([sys.executable, '/addp/query_driver.py'], request, timeout=120)
        try:
            assert prepared.facts['engine_read_scope'] == {'engine_ids': [11, 12]}, prepared.facts
            paths = prepared.facts['read_set']['paths']
            assert [(p['engine_id'], p['segments'][1]['name'], p['segments'][2]['name']) for p in paths] == [
                (11, ' schema.with dot ', ' 中文"table` '), (12, 'addp_test', 'custom`ers')], paths
            assert not prepared.path.exists(), 'prepare cannot materialize results'
            result = prepared.execute()
        finally:
            prepared.close()
        assert result.get('rows') == [
            {'id': '-9007199254740993', 'amount': '-200.987654321098765432', 'label': None},
            {'id': '9007199254740993', 'amount': '100.123456789012345678', 'label': '中文  '},
        ], result
        assert [(field['name'], field['type']) for field in result['fields']] == [
            ('id', 'bigint'), ('amount', 'decimal'), ('label', 'string')], result
        assert result['fields'][1]['precision'] == 38 and result['fields'][1]['scale'] == 18
        # While the same Driver is waiting for permission, attach a child
        # with a sentinel row. SELECT parent sees both rows, ONLY must see one.
        admin_spark = SparkSession.builder.master('local[1]').appName('ADDP-read-plan-ddl-fixture').getOrCreate()
        admin = admin_spark._jvm.org.postgresql.Driver().connect('jdbc:postgresql://postgres:5432/addp_test',
            _jdbc_properties(admin_spark, 'fixture_admin', 'owned-test-only'))
        try:
            original = dict(request)
            request = dict(original, catalogs=[original['catalogs'][0]],
                query='SELECT id FROM Business_PostgreSQL.public.read_plan_parent ORDER BY id', args=[])
            prepared = PreparedDriver([sys.executable, '/addp/query_driver.py'], request, timeout=120)
            try:
                assert prepared.facts['engine_read_scope'] == {'engine_ids': [11]}, prepared.facts
                assert not prepared.path.exists()
                statement = admin.createStatement()
                try:
                    statement.setQueryTimeout(10)
                    statement.execute('ALTER TABLE read_plan_child INHERIT read_plan_parent')
                    rs = statement.executeQuery('SELECT (SELECT count(*) FROM read_plan_parent), (SELECT count(*) FROM ONLY read_plan_parent)')
                    assert rs.next() and rs.getInt(1) == 2 and rs.getInt(2) == 1
                    rs.close()
                finally:
                    statement.close()
                assert prepared.execute()['rows'] == [{'id': 1}], 'concurrent inherited row escaped ONLY boundary'
            finally:
                prepared.close()
            # Initial inheritance and a MySQL View never become complete facts.
            for denied_request in [request, dict(original, catalogs=[original['catalogs'][1]],
                    query='SELECT id FROM Business_MySQL.addp_test.read_plan_view', args=[])]:
                try:
                    PreparedDriver([sys.executable, '/addp/query_driver.py'], denied_request, timeout=120)
                    raise AssertionError('unsupported relation yielded prepared read facts')
                except QueryDriverError as error:
                    assert error.code == 'query_source_not_supported', error.code
        finally:
            admin.close()
            admin_spark.stop()
        request = original
        print('SPARK_READ_PLAN_ONLY_PASS same_driver concurrent_inheritance isolated_table no_extra_reader_privileges unknown_view_refused', flush=True)
        request.update(query='WITH hidden AS (SELECT * FROM unauthorized.public.orders) SELECT 1', args=[])
        try:
            PreparedDriver([sys.executable, '/addp/query_driver.py'], request, timeout=120)
            raise AssertionError('unauthorized source prepared')
        except QueryDriverError as error:
            assert error.code == 'query_source_not_authorized', error.code
        print('SPARK_FEDERATION_PRODUCTION_DRIVER_PASS native_guard_jdbc_join typed_bigint_decimal exact_quoted_names private_process', flush=True)
        return
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
    if phase not in {'query', 'guard', 'fresh'}:
        raise ValueError('unknown admission phase')
    spark = SparkSession.builder.appName('ADDP-federation-admission-' + phase).getOrCreate()
    spark.sparkContext.setLogLevel('ERROR')
    try:
        if phase == 'guard':
            verify_native_guard(spark)
            return
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
        validated = validate_query(spark, query, {'pg', 'mysql'})
        assert validated.source_catalogs == {'pg', 'mysql'}
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

"""Owned T2: consume production adapter on real Standalone executors."""
import os
import socket
import sys
sys.path.insert(0, '/addp/spark-workflow')
from pyspark.sql import SparkSession
from storage_adapters import DatabaseAdapter, FileAdapter
from urllib.parse import quote

spark = (SparkSession.builder.appName('ADDP-HDFS-distributed-contract')
         .config('spark.sql.extensions', 'org.apache.sedona.sql.SedonaSqlExtensions')
         .config('spark.driver.host', 'spark-master')
         .config('spark.driver.bindAddress', '0.0.0.0')
         .config('spark.cores.max', '2')
         .config('spark.executor.memory', '512m')
         .config('spark.executorEnv.HADOOP_USER_NAME', 'addp_business_reader')
         .config('spark.hadoop.dfs.client.use.datanode.hostname', 'true').getOrCreate())
spark.sparkContext.setLogLevel('WARN')
try:
    if spark.sparkContext.master != 'spark://spark-master:7077':
        raise AssertionError('HDFS acceptance requires Standalone Worker')
    conn = {'engine_type': 'hdfs', 'rpc_uri': os.environ['HDFS_RPC_URI'],
            'root_path': '/addp', 'authentication': 'simple', 'user': 'addp_business_reader'}
    driver = socket.gethostname()
    for format_name in ('csv', 'json', 'parquet'):
        params = {'path': f"{conn['rpc_uri']}/addp/samples/orders.{format_name}",
                  'format': format_name, 'connection_info': conn}
        frame = FileAdapter.load(spark, params)
        result = frame.selectExpr('count(*) AS rows', 'sum(amount) AS total').first()
        assert (result.rows, result.total) == (20, 2100), result
        def executor_probe(rows):
            import os
            import socket
            yield (socket.gethostname(), os.environ.get('HADOOP_USER_NAME'), sum(1 for _ in rows))
        probes = frame.rdd.mapPartitions(executor_probe).collect()
        assert sum(p[2] for p in probes) == 20, probes
        assert all(p[0] != driver and p[1] == conn['user'] for p in probes), probes
        print(f'HDFS_SPARK_FORMAT_PASS format={format_name} rows=20 amount_sum=2100 workers={probes}', flush=True)
    original_name = dict(params, path=conn['rpc_uri'] + quote('/addp/订单 100%.csv'), format='csv')
    assert FileAdapter.load(spark, original_name).count() == 20
    try:
        FileAdapter.load(spark, dict(params, connection_info=dict(conn, user='another_user')))
    except ValueError:
        pass
    else:
        raise AssertionError('Spark accepted a resource-specific identity switch')
    pg = {'engine_type': 'postgresql', 'host': 'postgres', 'port': 5432,
          'database': 'spark_results', 'user': 'fixture_admin', 'password': 'owned-test-only'}
    admin = pg.copy()
    jdbc_url, _ = DatabaseAdapter._jdbc_config('postgresql', pg)
    frame = spark.createDataFrame([(1, 10), (2, 20)], 'id long, value long').repartition(2)
    def query(sql, params=()):
        connection = DatabaseAdapter._open_jdbc_connection(frame.sparkSession, jdbc_url, admin)
        try:
            return DatabaseAdapter._postgresql_query(connection, sql, params)
        finally:
            connection.close()
    def execute(sql):
        connection = DatabaseAdapter._open_jdbc_connection(frame.sparkSession, jdbc_url, admin)
        statement = connection.createStatement()
        try:
            statement.execute(sql)
        finally:
            statement.close()
            connection.close()
    def save(data, table='target', mode='overwrite'):
        return DatabaseAdapter.save(data, {'connection_info': pg, 'schema': 'results', 'table': table, 'mode': mode})
    def failure(data, table='target', mode='overwrite'):
        try:
            save(data, table, mode)
        except Exception:
            pass
        else:
            raise AssertionError('Invalid PostgreSQL publication succeeded')
        assert query('SELECT id::text, value::text FROM results.target ORDER BY id') == [('1', '10'), ('2', '20')]
        assert query("SELECT count(*) FROM pg_tables WHERE schemaname='results' AND tablename LIKE '__addp_spark_stage_%'") == [('0',)]
    execute('CREATE EXTENSION IF NOT EXISTS postgis')
    execute("CREATE ROLE spark_writer LOGIN PASSWORD 'owned-writer-test-only'")
    execute('CREATE SCHEMA results AUTHORIZATION spark_writer')
    pg = dict(pg, user='spark_writer', password='owned-writer-test-only')
    execute('CREATE ROLE result_reader NOLOGIN')
    execute('CREATE TABLE results.target (id bigint PRIMARY KEY, value bigint NOT NULL CHECK(value >= 0))')
    execute('CREATE INDEX target_value_index ON results.target(value)')
    execute("COMMENT ON TABLE results.target IS 'preserve-me'")
    execute('GRANT SELECT ON results.target TO result_reader')
    execute('GRANT SELECT,INSERT,DELETE,UPDATE ON results.target TO spark_writer')
    execute('INSERT INTO results.target VALUES (99,99)')
    identity_sql = "SELECT oid::text, relacl::text, obj_description(oid) FROM pg_class WHERE oid='results.target'::regclass"
    before = query(identity_sql)
    assert save(frame) == 2
    assert query(identity_sql) == before
    assert query("SELECT indexname FROM pg_indexes WHERE schemaname='results' AND tablename='target' ORDER BY indexname") == [('target_pkey',), ('target_value_index',)]
    failure(spark.createDataFrame([(1, -1)], frame.schema))
    failure(spark.createDataFrame([(1, 1), (1, 2)], frame.schema))
    failure(spark.createDataFrame([(3, 30)], 'id long, value int'))
    failure(frame.drop('value'))
    # Trigger suppression must fail and roll back the preceding DELETE.
    execute("CREATE FUNCTION results.skip_second() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.id=2 THEN RETURN NULL; END IF; RETURN NEW; END $$")
    execute('CREATE TRIGGER skip_second BEFORE INSERT ON results.target FOR EACH ROW EXECUTE FUNCTION results.skip_second()')
    failure(frame)
    execute('DROP TRIGGER skip_second ON results.target')
    execute("CREATE FUNCTION results.skip_delete() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NULL; END $$")
    execute('CREATE TRIGGER skip_delete BEFORE DELETE ON results.target FOR EACH ROW EXECUTE FUNCTION results.skip_delete()')
    failure(frame)
    execute('DROP TRIGGER skip_delete ON results.target')
    assert save(spark.createDataFrame([(3, 30)], frame.schema), mode='append') == 1
    assert query('SELECT count(*), sum(value) FROM results.target') == [('3', '60')]
    assert save(frame.limit(0)) == 0
    assert query('SELECT count(*) FROM results.target') == [('0',)]
    assert save(frame) == 2
    from concurrent.futures import ThreadPoolExecutor
    with ThreadPoolExecutor(max_workers=2) as pool:
        commits = list(pool.map(lambda number: save(spark.createDataFrame([(number, number * 10)], frame.schema), mode='append'), (3, 4)))
    assert commits == [1, 1]
    assert query('SELECT count(*), sum(value) FROM results.target') == [('4', '100')]
    assert save(frame) == 2
    # Quoted mixed-case, punctuation and Unicode names survive JDBC and publication.
    assert save(frame.withColumnRenamed('value', '总"值'), 'Mixed.Table') == 2
    read = DatabaseAdapter.load(spark, {'connection_info': pg, 'schema': 'results', 'table': 'Mixed.Table'})
    assert read.count() == 2 and '总"值' in read.columns
    geometry = spark.sql("SELECT cast(1 as bigint) id, ST_SetSRID(ST_Point(1.0D, 2.0D), 4326) source_shape")
    execute('CREATE TABLE results.spatial_target(id bigint PRIMARY KEY, source_shape geometry(Point,4326))')
    execute('GRANT SELECT,INSERT,DELETE,UPDATE ON results.spatial_target TO spark_writer')
    spatial_oid = query("SELECT 'results.spatial_target'::regclass::oid::text")
    assert save(geometry, 'spatial_target') == 1
    assert query("SELECT 'results.spatial_target'::regclass::oid::text") == spatial_oid
    assert query('SELECT ST_SRID(source_shape), ST_AsText(source_shape) FROM results.spatial_target') == [('4326', 'POINT(1 2)')]
    try:
        save(spark.sql("SELECT cast(2 as bigint) id, ST_SetSRID(ST_Point(3.0D, 4.0D), 3857) source_shape"), 'spatial_target')
    except Exception:
        pass
    else:
        raise AssertionError('Wrong SRID publication succeeded')
    assert query('SELECT id FROM results.spatial_target') == [('1',)]
    spatial = DatabaseAdapter.load(spark, {'connection_info': pg, 'schema': 'results',
        'table': 'spatial_target', 'geom_column': 'source_shape'})
    spatial_read = spatial.selectExpr('ST_SRID(source_shape) AS srid', 'ST_X(source_shape) AS x', 'ST_Y(source_shape) AS y').first()
    assert (spatial_read.srid, spatial_read.x, spatial_read.y) == (4326, 1.0, 2.0), spatial_read
    totals = FileAdapter.load(spark, params).groupBy('region').sum('amount').withColumnRenamed('sum(amount)', 'amount_sum')
    assert save(totals, 'hdfs_totals') == 2
    reused = DatabaseAdapter.load(spark, {'connection_info': pg, 'schema': 'results', 'table': 'hdfs_totals'})
    assert [(r.region, r.amount_sum) for r in reused.orderBy('region').collect()] == [('east', 1100), ('west', 1000)]
    assert query("SELECT count(*) FROM pg_tables WHERE schemaname='results' AND tablename LIKE '__addp_spark_stage_%'") == [('0',)]
    print('HDFS_SPARK_POSTGRES_PASS preserved=true rollback=true rows=2 spatial=true reuse=true staging=0', flush=True)
    print('HDFS_SPARK_PASS formats=csv,json,parquet distributed=true', flush=True)
finally:
    spark.stop()

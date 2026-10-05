"""Production adapter on a disposable ES and real Standalone Worker."""
import os
from pathlib import Path
import socket
import sys

sys.path.insert(0, '/addp/spark-workflow')
# Use the pinned official image's PySpark distribution while exercising the
# production Python submission path rather than bypassing it with spark-submit.
sys.path.insert(0, '/opt/spark/python')
sys.path.extend(str(path) for path in Path('/opt/spark/python/lib').glob('py4j-*.zip'))
from pyspark.sql import SparkSession
from pyspark.sql.functions import sum as total
from spark_dependencies import configure_spark_dependencies
from storage_adapters import StorageAdapter

spark = (configure_spark_dependencies(SparkSession.builder, '/gate')
         .master('spark://spark-master:7077').appName('ADDP-ES-distributed-contract')
         .config('spark.driver.host', 'spark-master').config('spark.driver.bindAddress', '0.0.0.0')
         .config('spark.cores.max', '2').config('spark.executor.memory', '512m').getOrCreate())
spark.sparkContext.setLogLevel('WARN')
params = {'source_type': 'index', 'index': 'addp_orders.v1', 'array_fields': ['tags'],
          'connection_info': {'engine_type': 'elasticsearch', 'endpoint': 'http://elasticsearch:9200',
              'user': os.environ['ELASTICSEARCH_READER_USER'], 'password': os.environ['ELASTICSEARCH_READER_PASSWORD']}}
try:
    assert spark.sparkContext.master == 'spark://spark-master:7077'
    distributed_jars = spark.sparkContext._jsc.sc().listJars().toString()
    for name in ('elasticsearch-spark-30_2.12-9.5.4.jar',
                 'sedona-spark-shaded-3.5_2.12-1.5.3.jar', 'geotools-wrapper-1.5.3-28.2.jar',
                 'postgresql-42.7.4.jar', 'mysql-connector-j-8.4.0.jar'):
        assert name in distributed_jars, (name, distributed_jars)
    assert spark.range(3).selectExpr('ST_AsText(ST_Point(id, id)) AS point').collect()[2].point == 'POINT (2 2)'
    frame = StorageAdapter.load(spark, params)
    rows = frame.orderBy('order_id').collect()
    assert len(rows) == 25 and rows[0].order_id == 9007199254740993
    assert rows[0].tags == ['sample', 'business'] and rows[0].customer.name == 'customer-0'
    assert rows[0].items[0].sku == 'SKU-001' and rows[0].items[0].quantity == 1
    assert rows[0].optional is None
    assert frame.schema['order_id'].dataType.simpleString() == 'bigint'
    assert 'description.keyword' not in frame.columns
    assert frame.agg(total('amount')).first()[0] == 562.5
    expected_host = Path('/gate/worker-hostname').read_text().strip()
    def partitions(frame):
        probes = frame.rdd.mapPartitions(lambda it: [(socket.gethostname(), sum(1 for _ in it))]).collect()
        assert all(host == expected_host for host, count in probes)
        return probes
    assert sum(count for host, count in partitions(frame)) == 25
    assert StorageAdapter.load(spark, dict(params, index='addp_empty.v1')).count() == 0
    multiple = StorageAdapter.load(spark, dict(params, index='addp_multi.v1'))
    assert multiple.rdd.getNumPartitions() >= 3
    assert sum(count for host, count in partitions(multiple)) == 25
    for invalid in (
        dict(params, index='addp_alias'), dict(params, index='addp_*'),
        dict(params, index='addp_missing'), dict(params, index='foreign_private'),
        dict(params, connection_info=dict(params['connection_info'], password='invalid')),
        dict(params, connection_info=dict(params['connection_info'], endpoint='https://elasticsearch:9200')),
    ):
        try: StorageAdapter.load(spark, invalid)
        except ValueError: pass
        else: raise AssertionError('Unsafe index access was accepted')
    # Explicit array declarations are required; malformed shapes must fail, not stringify.
    try: StorageAdapter.load(spark, dict(params, array_fields=[])).collect()
    except Exception: pass
    else: raise AssertionError('Unspecified array shape was silently coerced')
    print('ES_SPARK_PASS rows=25 amount_sum=562.5 empty=true multi_shard=true distributed=true', flush=True)
finally:
    spark.stop()

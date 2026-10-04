"""Owned T2: consume production adapter on real Standalone executors."""
import os
import socket
import sys
sys.path.insert(0, '/addp/spark-workflow')
from pyspark.sql import SparkSession
from storage_adapters import FileAdapter
from urllib.parse import quote

spark = (SparkSession.builder.appName('ADDP-HDFS-distributed-contract')
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
    print('HDFS_SPARK_PASS formats=csv,json,parquet distributed=true', flush=True)
finally:
    spark.stop()

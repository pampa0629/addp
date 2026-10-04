"""Create only ADDP sample files; repeat initialization preserves unrelated resources."""
import os
import time
from pyspark.sql import SparkSession

rpc = os.environ['HDFS_RPC_URI']
spark = (SparkSession.builder.master('local[1]').appName('ADDP-HDFS-samples')
         .config('spark.hadoop.dfs.client.use.datanode.hostname', 'true').getOrCreate())
spark.sparkContext.setLogLevel('WARN')
jvm = spark.sparkContext._jvm
fs = jvm.org.apache.hadoop.fs.FileSystem.get(jvm.java.net.URI(rpc), spark.sparkContext._jsc.hadoopConfiguration())
Path = jvm.org.apache.hadoop.fs.Path
Permission = jvm.org.apache.hadoop.fs.permission.FsPermission
try:
    deadline = time.monotonic() + 120
    while True:
        try:
            if not fs.setSafeMode(jvm.org.apache.hadoop.hdfs.protocol.HdfsConstants.SafeModeAction.SAFEMODE_GET) and len(fs.getDataNodeStats()) > 0:
                break
        except Exception:
            pass
        if time.monotonic() > deadline:
            raise RuntimeError('HDFS NameNode/DataNode readiness timeout')
        time.sleep(2)
    fs.mkdirs(Path('/addp/samples'))
    fs.setPermission(Path('/addp/samples'), Permission('755'))
    rows = [(i, 'east' if i % 2 == 0 else 'west', i * 10) for i in range(1, 21)]
    frame = spark.createDataFrame(rows, 'id int, region string, amount int').coalesce(1)
    for format_name in ('csv', 'json', 'parquet'):
        staging = f'{rpc}/addp/samples/.orders-{format_name}-staging'
        destination = Path(f'/addp/samples/orders.{format_name}')
        writer = frame.write.mode('overwrite').format(format_name)
        if format_name == 'csv':
            writer = writer.option('header', 'true')
        writer.save(staging)
        parts = [s.getPath() for s in fs.listStatus(Path(staging)) if s.getPath().getName().startswith('part-')]
        if len(parts) != 1:
            raise RuntimeError('HDFS sample generation requires one part file')
        fs.delete(destination, False)
        if not fs.rename(parts[0], destination):
            raise RuntimeError('HDFS sample publish failed')
        fs.setPermission(destination, Permission('444'))
        fs.delete(Path(staging), True)
    # Root leaf and original filename exercise the same catalog/content contract.
    root_csv = Path('/addp/订单 100%.csv')
    if not jvm.org.apache.hadoop.fs.FileUtil.copy(fs, Path('/addp/samples/orders.csv'), fs, root_csv,
                                                 False, True, spark.sparkContext._jsc.hadoopConfiguration()):
        raise RuntimeError('HDFS root sample copy failed')
    fs.setPermission(root_csv, Permission('444'))
    fs.setPermission(Path('/addp/samples'), Permission('555'))
    print('HDFS_SAMPLE_PASS rows=20 amount_sum=2100 formats=csv,json,parquet', flush=True)
finally:
    spark.stop()

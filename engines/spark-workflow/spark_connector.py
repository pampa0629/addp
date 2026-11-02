"""One immutable execution binding inside a fresh workflow Driver."""
from contextlib import contextmanager
from contextvars import ContextVar
import ipaddress
import os
from pyspark import SparkContext
from pyspark.sql import SparkSession
from spark_dependencies import configure_spark_dependencies

_binding = ContextVar('spark_execution_binding', default=None)


def cluster_master_url(connection):
    """Consume the canonical registered Master endpoint; never infer a port."""
    if not isinstance(connection, dict) or set(connection) != {'host', 'port', 'web_ui_url'}:
        raise ValueError('invalid Spark cluster connection fields')
    host, port = connection['host'], connection['port']
    if (not isinstance(host, str) or not host or host != host.strip()
            or any(char in host for char in '/@?#\\ \t\r\n')):
        raise ValueError('invalid Spark Master host')
    if not isinstance(port, int) or isinstance(port, bool) or not 1 <= port <= 65535:
        raise ValueError('invalid Spark Master port')
    if ':' in host:
        if ipaddress.ip_address(host).version != 6:
            raise ValueError('invalid Spark Master host')
        host = '[' + host + ']'
    return f'spark://{host}:{port}'


@contextmanager
def invocation_session(engine_id, tenant_id, connection):
    if any(type(value) is not int or value <= 0 for value in (engine_id, tenant_id)):
        raise ValueError('invalid Spark invocation identity')
    master = cluster_master_url(connection)
    if (_binding.get() is not None or SparkSession._instantiatedSession is not None
            or SparkContext._active_spark_context is not None):
        raise ValueError('Spark invocation requires a fresh Driver')
    builder = (configure_spark_dependencies(SparkSession.builder)
               .appName(f'ADDP-Workflow-Engine-{engine_id}').master(master)
               .config('spark.hadoop.dfs.client.use.datanode.hostname', 'true'))
    user = os.getenv('HADOOP_USER_NAME', '').strip()
    if user:
        builder = builder.config('spark.executorEnv.HADOOP_USER_NAME', user)
    shared_host = os.getenv('SPARK_WORKFLOW_SHARED_HOST', '').strip()
    if shared_host:
        builder = builder.config('spark.driver.host', shared_host).config('spark.driver.bindAddress', '0.0.0.0')
    elif connection['host'] in {'localhost', '127.0.0.1', '::1'}:
        raise ValueError('SPARK_WORKFLOW_SHARED_HOST is required for a loopback Spark master')
    spark = builder.getOrCreate()
    token = None
    try:
        if spark.sparkContext.master != master:
            raise ValueError('Spark invocation master mismatch')
        token = _binding.set((engine_id, tenant_id, spark))
        yield spark
    finally:
        if token is not None:
            _binding.reset(token)
        spark.stop()


def current_session(engine_id, tenant_id):
    binding = _binding.get()
    if binding is None or binding[:2] != (engine_id, tenant_id):
        raise ValueError('Spark session does not belong to this invocation')
    return binding[2]

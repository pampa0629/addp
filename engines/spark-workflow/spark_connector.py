"""
Spark Connector - 动态连接到用户注册的Spark集群
核心功能: 管理多个SparkSession,每个对应一个用户注册的Spark资源
"""

import os
import logging
import requests
from typing import Dict, Optional
from pyspark.sql import SparkSession

logger = logging.getLogger(__name__)


class SparkConnector:
    """
    Spark连接管理器
    负责动态创建和管理到多个Spark集群的连接
    """

    def __init__(self):
        self.sessions: Dict[int, SparkSession] = {}  # {engine_id: SparkSession}
        self.system_url = os.getenv('SYSTEM_URL', 'http://localhost:8180')

    def get_engine_from_system(self, engine_id: int) -> dict:
        """从System Backend获取资源信息"""
        try:
            response = requests.get(f'{self.system_url}/api/engines/{engine_id}')
            if response.status_code != 200:
                raise ValueError(f"Failed to get engine {engine_id}: {response.text}")
            return response.json()
        except Exception as e:
            logger.error(f"Failed to fetch engine {engine_id} from System: {e}")
            raise

    def get_or_create_session(self, engine_id: int) -> SparkSession:
        """
        获取或创建SparkSession

        Args:
            engine_id: system.engines 中的资源ID

        Returns:
            SparkSession 实例
        """
        # 如果已存在,直接返回
        if engine_id in self.sessions:
            logger.info(f"Reusing existing SparkSession for engine {engine_id}")
            return self.sessions[engine_id]

        # 获取资源配置
        engine = self.get_engine_from_system(engine_id)
        conn_info = engine['connection_info']

        logger.info(f"Creating new SparkSession for engine {engine_id}: {conn_info['host']}:{conn_info['port']}")

        # 创建SparkSession
        # 注意: 这里使用Thrift Server模式,而非Standalone/YARN模式
        # 因为用户注册的是Apache Spark Thrift Server
        builder = SparkSession.builder \
            .appName(f"ADDP-Workflow-Engine-{engine_id}") \
            .config("spark.sql.extensions", "org.apache.sedona.sql.SedonaSqlExtensions") \
            .config("spark.serializer", "org.apache.spark.serializer.KryoSerializer") \
            .config("spark.kryo.registrator", "org.apache.sedona.core.serde.SedonaKryoRegistrator")

        # 配置Spark连接方式
        # 方式1: 如果用户的Spark是独立集群,使用master方式
        # 方式2: 如果是Thrift Server,使用远程连接 (暂不支持,因为PySpark没有直接Thrift客户端)
        #
        # 当前实现: 假设用户注册的Spark资源已启动,我们创建本地SparkSession
        # 然后通过JDBC连接到Thrift Server执行SQL
        #
        # 注意: 这是一个折中方案,后续可以改进为:
        # - 使用 pyhive 库直接连接Thrift Server
        # - 或者要求用户注册的是Spark Standalone集群地址

        # 临时方案: 创建本地SparkSession (用于开发测试)
        # 生产环境应该连接到用户的Spark集群
        if os.getenv('SPARK_MODE') == 'local':
            builder = builder.master('local[*]')
        else:
            # 连接到用户的Spark集群
            spark_master = f"spark://{conn_info['host']}:{conn_info.get('master_port', 7077)}"
            builder = builder.master(spark_master)

        # S3/MinIO配置 (从资源中读取)
        if 's3_endpoint' in conn_info:
            builder = builder \
                .config("spark.hadoop.fs.s3a.endpoint", conn_info['s3_endpoint']) \
                .config("spark.hadoop.fs.s3a.access.key", conn_info.get('s3_access_key', 'minioadmin')) \
                .config("spark.hadoop.fs.s3a.secret.key", conn_info.get('s3_secret_key', 'minioadmin')) \
                .config("spark.hadoop.fs.s3a.path.style.access", "true") \
                .config("spark.hadoop.fs.s3a.impl", "org.apache.hadoop.fs.s3a.S3AFileSystem")

        # 创建会话
        spark = builder.getOrCreate()

        # 注册Sedona函数 (如果需要)
        try:
            from sedona.register import SedonaRegistrator
            SedonaRegistrator.registerAll(spark)
            logger.info("Sedona functions registered successfully")
        except Exception as e:
            logger.warning(f"Failed to register Sedona functions: {e}")

        # 缓存会话
        self.sessions[engine_id] = spark
        return spark

    def close_session(self, engine_id: int):
        """关闭指定资源的SparkSession"""
        if engine_id in self.sessions:
            self.sessions[engine_id].stop()
            del self.sessions[engine_id]
            logger.info(f"Closed SparkSession for engine {engine_id}")

    def close_all_sessions(self):
        """关闭所有SparkSession"""
        for engine_id in list(self.sessions.keys()):
            self.close_session(engine_id)


# 全局单例
_spark_connector = None


def get_spark_connector() -> SparkConnector:
    """获取全局SparkConnector实例"""
    global _spark_connector
    if _spark_connector is None:
        _spark_connector = SparkConnector()
    return _spark_connector

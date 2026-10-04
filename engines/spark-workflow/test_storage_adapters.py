import unittest
from unittest.mock import patch

from storage_adapters import DatabaseAdapter, FileAdapter
from unittest.mock import MagicMock


class StorageAdapterTest(unittest.TestCase):
    def test_geometry_columns_are_discovered_from_schema(self):
        class GeometryType:
            def typeName(self):
                return "geometry"

        class StringType:
            def typeName(self):
                return "string"

        class Field:
            def __init__(self, name, data_type):
                self.name = name
                self.dataType = data_type

        class DataFrame:
            class Schema:
                fields = [
                    Field("source_shape", GeometryType()),
                    Field("name", StringType()),
                    Field("buffer_shape", GeometryType()),
                ]

            schema = Schema()

        self.assertEqual(
            ["source_shape", "buffer_shape"],
            DatabaseAdapter._geometry_column_names(DataFrame()),
        )

    def test_database_adapter_requires_connection_info(self):
        with self.assertRaisesRegex(ValueError, "connection_info"):
            DatabaseAdapter._connection_context({"engine_id": 34})

    def test_database_adapter_uses_connection_info_engine_type(self):
        engine_type, conn_info = DatabaseAdapter._connection_context({
            "connection_info": {
                "engine_type": "postgresql",
                "host": "postgres",
                "port": 5432,
                "database": "addp",
            }
        })

        self.assertEqual("postgresql", engine_type)
        self.assertEqual("postgres", conn_info["host"])

    def test_database_adapter_maps_loopback_to_shared_host(self):
        params = {
            "connection_info": {
                "engine_type": "postgresql",
                "host": "localhost",
            }
        }
        with patch.dict("os.environ", {"SPARK_WORKFLOW_SHARED_HOST": "192.0.2.10"}):
            _, conn_info = DatabaseAdapter._connection_context({
                "connection_info": params["connection_info"]
            })

        self.assertEqual("192.0.2.10", conn_info["host"])
        self.assertEqual("localhost", params["connection_info"]["host"])

    def test_database_adapter_rejects_loopback_without_shared_host(self):
        with patch.dict("os.environ", {}, clear=True):
            with self.assertRaisesRegex(ValueError, "SPARK_WORKFLOW_SHARED_HOST"):
                DatabaseAdapter._connection_context({
                    "connection_info": {
                        "engine_type": "postgresql",
                        "host": "localhost",
                    }
                })

    def test_database_adapter_preserves_remote_host(self):
        with patch.dict("os.environ", {"SPARK_WORKFLOW_SHARED_HOST": "192.0.2.10"}):
            _, conn_info = DatabaseAdapter._connection_context({
                "connection_info": {
                    "engine_type": "postgresql",
                    "host": "database.example.internal",
                }
            })

        self.assertEqual("database.example.internal", conn_info["host"])

    def test_postgresql_jdbc_config_includes_sslmode(self):
        jdbc_url, driver = DatabaseAdapter._jdbc_config("postgresql", {
            "host": "database.example.internal",
            "port": 5432,
            "database": "addp",
            "sslmode": "disable",
        })

        self.assertEqual(
            "jdbc:postgresql://database.example.internal:5432/addp?sslmode=disable",
            jdbc_url,
        )
        self.assertEqual("org.postgresql.Driver", driver)

    def test_mysql_jdbc_config_uses_connector_j_driver(self):
        jdbc_url, driver = DatabaseAdapter._jdbc_config("mysql", {
            "host": "database.example.internal",
            "port": 3306,
            "database": "addp",
        })

        self.assertEqual("jdbc:mysql://database.example.internal:3306/addp", jdbc_url)
        self.assertEqual("com.mysql.cj.jdbc.Driver", driver)

    def test_doris_create_table_sql_uses_native_types_and_distribution(self):
        class StringType:
            def typeName(self):
                return "string"

        class LongType:
            def typeName(self):
                return "long"

        class BooleanType:
            def typeName(self):
                return "boolean"

        class TimestampType:
            def typeName(self):
                return "timestamp"

        class Field:
            def __init__(self, name, data_type, nullable=True):
                self.name = name
                self.dataType = data_type
                self.nullable = nullable

        class DataFrame:
            class Schema:
                fields = [
                    Field("id", LongType(), nullable=False),
                    Field("customer`name", StringType()),
                    Field("active", BooleanType()),
                    Field("created_at", TimestampType()),
                ]

            schema = Schema()

        self.assertEqual(
            (
                "CREATE TABLE IF NOT EXISTS `addp_acceptance`.`customers_copy` "
                "(`id` BIGINT NOT NULL, `customer``name` VARCHAR(65533), "
                "`active` BOOLEAN, `created_at` DATETIME) "
                "DUPLICATE KEY(`id`) DISTRIBUTED BY HASH(`id`) BUCKETS 10"
            ),
            DatabaseAdapter._doris_create_table_sql(
                DataFrame(), "addp_acceptance", "customers_copy"
            ),
        )

    def test_doris_save_prepares_table_then_appends(self):
        class DataFrame:
            pass

        df = DataFrame()
        params = {
            "connection_info": {"engine_type": "doris"},
            "schema": "addp_acceptance",
            "table": "customers_copy",
            "mode": "overwrite",
        }
        conn_info = {
            "engine_type": "doris",
            "host": "database.example.internal",
            "port": 9030,
            "database": "addp_acceptance",
        }

        with patch.object(
            DatabaseAdapter, "_connection_context", return_value=("doris", conn_info)
        ), patch.object(
            DatabaseAdapter,
            "_jdbc_config",
            return_value=("jdbc:mysql://database/addp_acceptance", "driver"),
        ), patch.object(
            DatabaseAdapter, "_geometry_column_names", return_value=[]
        ), patch.object(
            DatabaseAdapter, "_prepare_doris_table"
        ) as prepare, patch.object(DatabaseAdapter, "_write_jdbc") as write:
            DatabaseAdapter.save(df, params)

        prepare.assert_called_once_with(
            df,
            "jdbc:mysql://database/addp_acceptance",
            conn_info,
            "addp_acceptance",
            "customers_copy",
            "overwrite",
        )
        write.assert_called_once_with(
            df,
            "jdbc:mysql://database/addp_acceptance",
            "driver",
            conn_info,
            "addp_acceptance",
            "customers_copy",
            "append",
        )


if __name__ == "__main__":
    unittest.main()


class HDFSAdapterTest(unittest.TestCase):
    def setUp(self):
        self.spark = MagicMock()
        self.ugi = self.spark.sparkContext._jvm.org.apache.hadoop.security.UserGroupInformation
        self.ugi.isSecurityEnabled.return_value = False
        self.ugi.getCurrentUser.return_value.getShortUserName.return_value = 'reader'
        self.params = {'path': 'hdfs://namenode:8020/addp/orders.parquet',
                       'connection_info': {'engine_type': 'hdfs', 'authentication': 'simple',
                                           'rpc_uri': 'hdfs://namenode:8020', 'root_path': '/addp', 'user': 'reader'}}

    def test_simple_fixed_identity(self):
        FileAdapter._validate_hdfs_access(self.spark, self.params)
        self.ugi.getCurrentUser.return_value.getShortUserName.return_value = 'another'
        with self.assertRaisesRegex(ValueError, 'identity'):
            FileAdapter._validate_hdfs_access(self.spark, self.params)

    def test_root_authority_and_traversal(self):
        for path in ('hdfs://other:8020/addp/a.parquet', 'hdfs://namenode:8020/addp-other/a.parquet',
                     'hdfs://namenode:8020/addp/../secret', 'hdfs://namenode:8020/addp/%2e%2e/secret',
                     'hdfs://namenode:8020/addp//a', 'hdfs://namenode:8020/addp/a?secret=x'):
            with self.subTest(path=path), self.assertRaises(ValueError):
                FileAdapter._validate_hdfs_access(self.spark, dict(self.params, path=path))

    def test_uri_decodes_original_name_once_and_rejects_globs(self):
        source = 'hdfs://namenode:8020/addp/%E8%AE%A2%E5%8D%95%20100%25.csv'
        self.assertEqual('hdfs://namenode:8020/addp/订单 100%.csv',
                         FileAdapter._validate_hdfs_access(self.spark, dict(self.params, path=source)))
        for name in ('*.csv', '%3F.csv', '%5Ba%5D.csv', '%7Ba,b%7D.csv', '%5C.csv'):
            with self.assertRaisesRegex(ValueError, 'literal resource'):
                FileAdapter._validate_hdfs_access(self.spark, dict(self.params, path='hdfs://namenode:8020/addp/' + name))

    def test_hdfs_save_rejected_before_writer(self):
        frame = MagicMock()
        with self.assertRaisesRegex(ValueError, 'reading only'):
            FileAdapter.save(frame, self.params)
        frame.write.format.assert_not_called()

    def test_secure_or_missing_facts_are_rejected(self):
        self.ugi.isSecurityEnabled.return_value = True
        with self.assertRaises(ValueError):
            FileAdapter._validate_hdfs_access(self.spark, self.params)
        with self.assertRaises(ValueError):
            FileAdapter._validate_hdfs_access(self.spark, {'path': self.params['path']})

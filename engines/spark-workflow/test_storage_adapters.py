import unittest
from unittest.mock import patch

from storage_adapters import DatabaseAdapter


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

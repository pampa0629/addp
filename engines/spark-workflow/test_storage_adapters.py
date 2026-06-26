import unittest

from storage_adapters import DatabaseAdapter


class StorageAdapterTest(unittest.TestCase):
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


if __name__ == "__main__":
    unittest.main()

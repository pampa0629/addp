import unittest
import sys
import types
from pathlib import Path

for parent in Path(__file__).resolve().parents:
    contract_path = parent / "docs" / "workflow_operator_contract.py"
    if contract_path.exists():
        sys.path.insert(0, str(contract_path.parent))
        break

from workflow_operator_contract import assert_operator_metadata_contract

from operator_metadata import get_operator_metadata
from validate_metadata import MetadataValidator

if "pyspark" not in sys.modules:
    pyspark_module = types.ModuleType("pyspark")
    pyspark_sql_module = types.ModuleType("pyspark.sql")

    class _DataFrame:
        pass

    class _SparkSession:
        pass

    pyspark_sql_module.DataFrame = _DataFrame
    pyspark_sql_module.SparkSession = _SparkSession
    pyspark_module.sql = pyspark_sql_module
    sys.modules["pyspark"] = pyspark_module
    sys.modules["pyspark.sql"] = pyspark_sql_module

from workflow_engine import WorkflowInvalidError, validate_workflow_def
import api_server


class OperatorMetadataTest(unittest.TestCase):
    def test_runtime_metadata_exposes_task_runtime_parameters(self):
        operators = {operator["name"]: operator for operator in get_operator_metadata()}

        for operator_name in ("load", "save"):
            operator = operators[operator_name]
            parameter_names = {param["name"] for param in operator["parameters"]}
            self.assertIn("connection_info", parameter_names)
            self.assertTrue({"schema", "table", "path"} <= parameter_names)
            self.assertNotIn("engine_id", parameter_names)

        self.assertIn("source_type", {param["name"] for param in operators["load"]["parameters"]})
        self.assertIn("target_type", {param["name"] for param in operators["save"]["parameters"]})
        self.assertIn("query", {param["name"] for param in operators["sql"]["parameters"]})

    def test_table_io_exposes_runtime_contract_only(self):
        operators = {operator["name"]: operator for operator in get_operator_metadata()}

        load_params = {param["name"] for param in operators["load"]["parameters"]}
        save_params = {param["name"] for param in operators["save"]["parameters"]}

        self.assertTrue({"connection_info", "schema", "table", "path"} <= load_params)
        self.assertTrue({"connection_info", "schema", "table", "path"} <= save_params)
        self.assertFalse({"locator", "target_parent_locator", "target_name"} & load_params)
        self.assertFalse({"locator", "target_parent_locator", "target_name"} & save_params)

        load_example = operators["load"]["detailed_description"]["workflow_example"]["params"]
        save_example = operators["save"]["detailed_description"]["workflow_example"]["params"]

        self.assertIn("connection_info", load_example)
        self.assertIn("schema", load_example)
        self.assertIn("table", load_example)
        self.assertNotIn("locator", load_example)
        self.assertIn("connection_info", save_example)
        self.assertIn("schema", save_example)
        self.assertIn("table", save_example)
        self.assertNotIn("target_parent_locator", save_example)

    def test_all_operators_declare_execution_modes(self):
        assert_operator_metadata_contract(
            get_operator_metadata(),
            expected_engine_type="spark_workflow",
        )

    def test_parameter_types_use_standard_contract_names(self):
        operators = {operator["name"]: operator for operator in get_operator_metadata()}

        load_params = {param["name"]: param["type"] for param in operators["load"]["parameters"]}
        preview_params = {param["name"]: param["type"] for param in operators["preview"]["parameters"]}

        self.assertEqual("string", load_params["source_type"])
        self.assertEqual("integer", preview_params["limit"])

    def test_builtin_operator_metadata_has_no_quality_warnings(self):
        validator = MetadataValidator()
        report = validator.validate_all(get_operator_metadata())

        self.assertEqual([], report["errors"])
        self.assertEqual([], report["warnings"])

    def test_dataframe_result_is_serialized_as_lightweight_summary(self):
        class _Field:
            def __init__(self, name, data_type):
                self.name = name
                self.dataType = data_type

        class _DataType:
            def __init__(self, name):
                self.name = name

            def simpleString(self):
                return self.name

        class _Schema:
            fields = [_Field("id", _DataType("int")), _Field("name", _DataType("string"))]

        class _Row:
            def __init__(self, data):
                self.data = data

            def asDict(self, recursive=True):
                return dict(self.data)

        class _FakeDataFrame(_DataFrame):
            schema = _Schema()

            def limit(self, limit):
                self.limit_value = limit
                return self

            def collect(self):
                return [_Row({"id": 1, "name": "road"})]

        original_dataframe = api_server.DataFrame
        api_server.DataFrame = _DataFrame
        try:
            got = api_server.serialize_workflow_value(_FakeDataFrame(), preview_limit=3)
        finally:
            api_server.DataFrame = original_dataframe

        self.assertEqual("spark_dataframe", got["type"])
        self.assertEqual(3, got["preview_limit"])
        self.assertEqual([{"name": "id", "type": "int"}, {"name": "name", "type": "string"}], got["schema"])
        self.assertEqual([{"id": 1, "name": "road"}], got["preview_rows"])

    def test_api_errors_include_execution_time(self):
        api_server.app.config["TESTING"] = True
        with api_server.app.test_client() as client:
            response = client.post(
                "/api/workflow",
                json={"workflow_def": {"tasks": []}},
                content_type="application/json",
            )
            self.assertEqual(400, response.status_code)
            payload = response.get_json()
            self.assertEqual("INVALID_PARAMS", payload["error_code"])
            self.assertIn("execution_time_ms", payload)

            response = client.post(
                "/api/operators/load/invoke",
                json={"engine_id": 34, "params": {}},
                content_type="application/json",
            )
            self.assertEqual(403, response.status_code)
            payload = response.get_json()
            self.assertEqual("DIRECT_NOT_SUPPORTED", payload["error_code"])
            self.assertIn("execution_time_ms", payload)

            response = client.get("/api/executions/unknown-execution-id")
            self.assertEqual(404, response.status_code)
            payload = response.get_json()
            self.assertEqual("EXECUTION_NOT_FOUND", payload["error_code"])
            self.assertNotIn("task_status", payload)

    def test_workflow_definition_requires_params_object_and_string_dependencies(self):
        with self.assertRaisesRegex(WorkflowInvalidError, "'params' 必须是对象"):
            validate_workflow_def({
                "tasks": [{
                    "id": "invalid_params",
                    "operator": "load",
                    "params": [],
                    "depends_on": [],
                }]
            })

        with self.assertRaisesRegex(WorkflowInvalidError, "任务 id 重复"):
            validate_workflow_def({
                "tasks": [
                    {"id": "load", "operator": "load", "params": {}, "depends_on": []},
                    {"id": "load", "operator": "load", "params": {}, "depends_on": []},
                ]
            })

        with self.assertRaisesRegex(WorkflowInvalidError, "未在 depends_on 中声明"):
            validate_workflow_def({
                "tasks": [
                    {"id": "load", "operator": "load", "params": {}, "depends_on": []},
                    {
                        "id": "filter",
                        "operator": "filter",
                        "params": {"input_df": {"$ref": "load"}},
                        "depends_on": [],
                    },
                ]
            })

        with self.assertRaisesRegex(WorkflowInvalidError, "'depends_on' 必须是字符串数组"):
            validate_workflow_def({
                "tasks": [{
                    "id": "invalid_dep",
                    "operator": "load",
                    "params": {},
                    "depends_on": [1],
                }]
            })


if __name__ == "__main__":
    unittest.main()

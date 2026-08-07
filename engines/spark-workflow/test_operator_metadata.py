import unittest
import sys
import types
from pathlib import Path
from unittest.mock import patch

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
else:
    class _DataFrame:
        pass

from workflow_engine import SparkWorkflowEngine, WorkflowInvalidError, validate_workflow_def
from spark_connector import SPARK_MAVEN_PACKAGES
from api_server import serialize_json_value
from operators.spatial_operators import buffer
import api_server


class OperatorMetadataTest(unittest.TestCase):
    def test_workflow_injects_runtime_engine_and_tenant_context(self):
        captured = {}
        engine = object.__new__(SparkWorkflowEngine)
        engine.engine_id = 34
        engine.tenant_id = 7
        engine.results = {}
        engine.tasks = {
            "load_data": {
                "operator": "load",
                "params": {"source_type": "table", "engine_id": 999, "tenant_id": 999},
                "depends_on": [],
            }
        }

        def fake_operator(**params):
            captured.update(params)
            return "loaded"

        with patch("workflow_engine.get_operator", return_value=fake_operator):
            result = engine.execute_task("load_data")

        self.assertEqual(result, {"result": "loaded"})
        self.assertEqual(captured["engine_id"], 34)
        self.assertEqual(captured["tenant_id"], 7)

    def test_runtime_values_fall_back_to_string_for_json_serialization(self):
        class Geometry:
            def __str__(self):
                return "POLYGON ((0 0, 1 0, 0 0))"

        self.assertEqual(
            {"geometry": "POLYGON ((0 0, 1 0, 0 0))"},
            serialize_json_value({"geometry": Geometry()}),
        )

    def test_buffer_preserves_input_geometry_srid(self):
        class DataFrame:
            expression = None

            def withColumn(self, _name, expression):
                self.expression = expression
                return self

        dataframe = DataFrame()
        functions_module = types.ModuleType("pyspark.sql.functions")
        functions_module.expr = lambda expression: expression
        with patch.dict(sys.modules, {"pyspark.sql.functions": functions_module}):
            result = buffer(
                dataframe,
                geom_column="source_shape",
                distance=100,
                output_column="buffer_shape",
            )

        self.assertIs(result, dataframe)
        self.assertEqual(
            "ST_SetSRID(ST_Buffer(source_shape, 100), ST_SRID(source_shape))",
            dataframe.expression,
        )

    def test_spark_packages_include_runtime_and_jdbc_dependencies(self):
        self.assertIn("sedona-spark-shaded-3.5_2.12:1.5.1", SPARK_MAVEN_PACKAGES)
        self.assertIn("org.postgresql:postgresql:42.7.4", SPARK_MAVEN_PACKAGES)
        self.assertIn("com.mysql:mysql-connector-j:8.4.0", SPARK_MAVEN_PACKAGES)

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

    def test_all_operators_declare_execution_modes_and_effects(self):
        operators = get_operator_metadata()
        assert_operator_metadata_contract(operators, expected_engine_type="spark_workflow")
        by_name = {operator["name"]: operator for operator in operators}
        self.assertEqual(["read"], by_name["load"]["effects"])
        self.assertEqual(["write"], by_name["save"]["effects"])

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

"""JSON summaries produced inside the invocation Driver before Spark stops."""
from decimal import Decimal
import json
from pyspark.sql import DataFrame


def serialize_json_value(value):
    """Recursively reduce runtime values to JSON-safe primitives."""
    if isinstance(value, int) and not isinstance(value, bool) and abs(value) > 9007199254740991:
        return str(value)
    if isinstance(value, Decimal):
        return format(value, 'f')
    if isinstance(value, dict):
        return {key: serialize_json_value(item) for key, item in value.items()}
    if isinstance(value, (list, tuple)):
        return [serialize_json_value(item) for item in value]
    if isinstance(value, bytes):
        return value.hex()
    try:
        json.dumps(value)
        return value
    except (TypeError, ValueError):
        return str(value)


def serialize_workflow_value(value, preview_limit: int = 5):
    """把 Spark 运行时对象转换为可 JSON 化的轻量结果摘要。"""
    if DataFrame is not None and isinstance(value, DataFrame):
        schema = [
            {"name": field.name, "type": field.dataType.simpleString()}
            for field in value.schema.fields
        ]
        rows = serialize_json_value([
            row.asDict(recursive=True)
            for row in value.limit(preview_limit).collect()
        ])
        return {
            "type": "spark_dataframe",
            "schema": schema,
            "preview_rows": rows,
            "preview_limit": preview_limit
        }

    if isinstance(value, dict):
        return {key: serialize_workflow_value(item, preview_limit) for key, item in value.items()}
    if isinstance(value, list):
        return [serialize_workflow_value(item, preview_limit) for item in value]
    return serialize_json_value(value)


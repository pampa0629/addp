from typing import Any

from addp_common.tools import get_tool


_RESULT_REF_SCHEMA = "addp.result-ref/v1"


def build_result_ref(tool_name: str, result: Any) -> dict[str, str] | None:
    """Builds a stable owner result reference only for declared singular results."""
    if not isinstance(result, dict):
        return None
    try:
        definition = get_tool(tool_name)
    except KeyError:
        return None

    declaration = definition.result_ref
    if declaration.get("mode") != "execution":
        return None
    execution_id = result.get("execution_id")
    if not isinstance(execution_id, str) or not execution_id.strip():
        return None
    return {
        "schema": str(declaration.get("schema") or _RESULT_REF_SCHEMA),
        "owner_module": definition.owner,
        "kind": "execution",
        "ref": f"execution:{execution_id.strip()}",
    }

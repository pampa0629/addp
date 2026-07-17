"""将 ADDP Tool Manifest 适配为 LangChain StructuredTool。"""

import json
from typing import Annotated, Any

from addp_common.tools import ToolExecutionError, ToolExecutor, load_manifest
from langchain_core.tools import InjectedToolCallId, StructuredTool
from pydantic import BaseModel, ConfigDict, Field, create_model

from config import settings


def _python_type(schema: dict[str, Any]):
    return {
        "string": str,
        "integer": int,
        "number": float,
        "boolean": bool,
        "array": list,
        "object": dict,
    }.get(schema.get("type"), Any)


def _arguments_model(tool_name: str, schema: dict[str, Any]) -> type[BaseModel]:
    required = set(schema.get("required") or [])
    fields: dict[str, tuple[Any, Any]] = {}
    for name, property_schema in (schema.get("properties") or {}).items():
        annotation = _python_type(property_schema)
        if name not in required:
            annotation = annotation | None
        default = ... if name in required else property_schema.get("default")
        fields[name] = (
            annotation,
            Field(
                default=default,
                description=property_schema.get("description"),
                ge=property_schema.get("minimum"),
                le=property_schema.get("maximum"),
                min_length=property_schema.get("minLength"),
            ),
        )
    fields["tool_call_id"] = (Annotated[str, InjectedToolCallId], ...)
    return create_model(
        f"{tool_name.replace('.', '_').title()}Arguments",
        __config__=ConfigDict(extra="forbid"),
        **fields,
    )


def _runtime_name(stable_name: str) -> str:
    return stable_name.replace(".", "__")


def stable_tool_name(tool: StructuredTool) -> str:
    return str((getattr(tool, "metadata", None) or {}).get("addp_tool_name") or tool.name)


def create_agent_tools(token: str, agent_run_id: str) -> list[StructuredTool]:
    executor = ToolExecutor(settings.get_gateway_url(), token)
    tools: list[StructuredTool] = []

    for definition in load_manifest().tools:
        stable_name = definition.name

        async def call_tool(_stable_name=stable_name, **arguments):
            tool_call_id = str(arguments.pop("tool_call_id"))
            try:
                result = await executor.call(
                    _stable_name,
                    arguments,
                    agent_run_id=agent_run_id,
                    tool_call_id=tool_call_id,
                )
            except ToolExecutionError as exc:
                result = exc.as_dict()
            return json.dumps(result, ensure_ascii=False, separators=(",", ":"))

        tools.append(
            StructuredTool.from_function(
                coroutine=call_tool,
                name=_runtime_name(stable_name),
                description=f"ADDP Tool `{stable_name}`：{definition.description}",
                args_schema=_arguments_model(stable_name, definition.input_schema),
                metadata={"addp_tool_name": stable_name},
            )
        )
    return tools

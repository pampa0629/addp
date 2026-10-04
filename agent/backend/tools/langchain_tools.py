"""将 ADDP Tool Manifest 适配为 LangChain StructuredTool。"""

import json
from typing import Annotated, Any

from addp_common.tools import ToolExecutionError, ToolExecutor, load_manifest
from langchain_core.tools import InjectedToolCallId, StructuredTool
from pydantic import BaseModel, ConfigDict, Field, create_model

from config import settings


def _python_type(schema: dict[str, Any]):
    schema_type = schema.get("type")
    if isinstance(schema_type, list):
        schema_type = next((value for value in schema_type if value != "null"), None)
    if schema_type is None and schema.get("anyOf"):
        schema_type = next(
            (item.get("type") for item in schema["anyOf"] if item.get("type") != "null"),
            None,
        )
    return {
        "string": str,
        "integer": int,
        "number": float,
        "boolean": bool,
        "array": list,
        "object": dict,
    }.get(schema_type, Any)


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
                json_schema_extra=property_schema,
            ),
        )
    fields["tool_call_id"] = (Annotated[str, InjectedToolCallId], ...)
    return create_model(
        f"{tool_name.replace('.', '_').title()}Arguments",
        __config__=ConfigDict(extra="forbid"),
        **fields,
    )


class ManifestStructuredTool(StructuredTool):
    # Public Schema is the Manifest itself, not a framework-generated approximation.
    manifest_input_schema: dict[str, Any] = Field(exclude=True)

    @property
    def tool_call_schema(self) -> dict[str, Any]:
        return self.manifest_input_schema

    async def _arun(self, *args: Any, **arguments: Any) -> Any:
        # StructuredTool reserves `config` for RunnableConfig. ADDP's Manifest
        # owns every business argument; BaseTool already carries run context.
        return await self.coroutine(*args, **arguments)

    def _parse_input(self, tool_input, tool_call_id):
        if not isinstance(tool_input, dict):
            raise ValueError("tool_input_must_be_object")
        provided = set(tool_input)
        parsed = super()._parse_input(tool_input, tool_call_id)
        # Core 1.x applies every Pydantic default, including synthetic None.
        # Only defaults explicitly declared by the Manifest are business inputs.
        defaults = {name for name, prop in self.manifest_input_schema.get("properties", {}).items() if "default" in prop}
        return {name: value for name, value in parsed.items() if name in provided or name in defaults or name == "tool_call_id"}


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
            ManifestStructuredTool.from_function(
                coroutine=call_tool,
                name=_runtime_name(stable_name),
                description=f"ADDP Tool `{stable_name}`：{definition.description}",
                args_schema=_arguments_model(stable_name, definition.input_schema),
                manifest_input_schema=definition.input_schema,
                metadata={"addp_tool_name": stable_name},
            )
        )
    return tools

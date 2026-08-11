import json
from typing import Any, Awaitable, Callable

import httpx
from jsonschema import Draft202012Validator

from addp_common.client import CopilotClient, DevelopClient, ManagerClient, MetaClient, SystemClient

from .manifest import ToolDefinition, get_tool


class ToolExecutionError(Exception):
    def __init__(self, code: str, message: str, details: dict[str, Any] | None = None):
        super().__init__(message)
        self.code = code
        self.message = message
        self.details = details or {}

    def as_dict(self) -> dict[str, Any]:
        return {
            "error": {
                "code": self.code,
                "message": self.message,
                **({"details": self.details} if self.details else {}),
            }
        }


class ToolExecutor:
    """Tool Manifest 到 Python SDK 的唯一执行映射。"""

    def __init__(self, base_url: str, source_token: str):
        self.base_url = base_url.rstrip("/")
        self.source_token = source_token
        self._handlers: dict[str, Callable[[dict[str, Any], str], Awaitable[Any]]] = {
            "engine.list": self._engine_list,
            "data.search": self._data_search,
            "resource.ancestors.get": self._resource_ancestors_get,
            "data.preview": self._data_preview,
            "workflow.operators.list": self._workflow_operators_list,
            "workflow.draft.generate": self._workflow_draft_generate,
            "query.draft.generate": self._query_draft_generate,
            "notebook.draft.generate": self._notebook_draft_generate,
            "transfer.draft.generate": self._transfer_draft_generate,
            "workflow.validate": self._workflow_validate,
            "workflow.run": self._workflow_run,
            "execution.get": self._execution_get,
        }

    async def call(
        self,
        name: str,
        arguments: dict[str, Any] | None = None,
        *,
        agent_run_id: str,
        tool_call_id: str,
    ) -> Any:
        try:
            definition = get_tool(name)
        except KeyError as exc:
            raise ToolExecutionError("tool_not_found", f"未知 ADDP Tool: {name}") from exc

        arguments = arguments or {}
        validation_errors = sorted(
            Draft202012Validator(definition.input_schema).iter_errors(arguments),
            key=lambda error: list(error.absolute_path),
        )
        if validation_errors:
            error = validation_errors[0]
            path = ".".join(str(item) for item in error.absolute_path)
            raise ToolExecutionError("invalid_arguments", error.message, {"path": path})

        handler = self._handlers.get(name)
        if handler is None:
            raise ToolExecutionError("tool_not_implemented", f"Tool 尚未实现: {name}")
        if not agent_run_id or not tool_call_id:
            raise ToolExecutionError("invalid_arguments", "agent_run_id and tool_call_id are required")

        try:
            delegated_token = await self._issue_delegated_token(
                definition,
                agent_run_id=agent_run_id,
                tool_call_id=tool_call_id,
            )
        except httpx.HTTPStatusError as exc:
            raise ToolExecutionError(
                "delegation_rejected",
                f"System 拒绝为 {name} 签发委托令牌",
                {"status": exc.response.status_code},
            ) from exc
        except httpx.HTTPError as exc:
            raise ToolExecutionError("delegation_unavailable", "System 委托令牌服务不可用") from exc
        except ValueError as exc:
            raise ToolExecutionError("invalid_delegation_response", str(exc)) from exc

        try:
            result = await handler(arguments, delegated_token)
        except httpx.HTTPStatusError as exc:
            error_code = "owner_api_error"
            error_message = f"{definition.owner} API 返回 HTTP {exc.response.status_code}"
            try:
                error_body = exc.response.json().get("error")
                if isinstance(error_body, dict):
                    candidate_code = str(error_body.get("code") or "")
                    if candidate_code in definition.errors:
                        error_code = candidate_code
                    error_message = str(error_body.get("message") or error_message)
            except (TypeError, ValueError):
                pass
            raise ToolExecutionError(
                error_code,
                error_message,
                {"status": exc.response.status_code},
            ) from exc
        except httpx.HTTPError as exc:
            raise ToolExecutionError("owner_api_unavailable", f"无法访问 {definition.owner} API") from exc
        except ValueError as exc:
            raise ToolExecutionError("invalid_owner_response", str(exc)) from exc

        self._validate_output(definition, result)
        return result

    def _validate_output(self, definition: ToolDefinition, result: Any) -> None:
        errors = list(Draft202012Validator(definition.output_schema).iter_errors(result))
        if errors:
            raise ToolExecutionError("invalid_owner_response", errors[0].message)
        encoded = json.dumps(result, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
        if len(encoded) > definition.limits["max_bytes"]:
            raise ToolExecutionError(
                "result_too_large",
                f"Tool 结果超过 {definition.limits['max_bytes']} 字节限制",
            )

    async def _issue_delegated_token(
        self,
        definition: ToolDefinition,
        *,
        agent_run_id: str,
        tool_call_id: str,
    ) -> str:
        async with SystemClient(base_url=self.base_url, user_token=self.source_token) as client:
            response = await client.create_delegation(
                audience=definition.auth.audience,
                scopes=definition.auth.required_scopes,
                agent_run_id=agent_run_id,
                tool_call_id=tool_call_id,
            )
        if response.get("audience") != definition.auth.audience:
            raise ValueError("system delegation response audience mismatch")
        if sorted(response.get("scopes") or []) != sorted(definition.auth.required_scopes):
            raise ValueError("system delegation response scopes mismatch")
        if response.get("agent_run_id") != agent_run_id or response.get("tool_call_id") != tool_call_id:
            raise ValueError("system delegation response audit binding mismatch")
        return str(response["access_token"])

    def _client(self, client_type, delegated_token: str):
        return client_type(base_url=self.base_url, user_token=delegated_token)

    async def _engine_list(self, arguments: dict[str, Any], delegated_token: str) -> Any:
        async with self._client(SystemClient, delegated_token) as client:
            if arguments.get("capability") == "workflow":
                return await client.get_workflow_engines()
            return await client.list_engines()

    async def _data_search(self, arguments: dict[str, Any], delegated_token: str) -> Any:
        async with self._client(ManagerClient, delegated_token) as client:
            return await client.search(
                q=arguments["query"],
                engine_id=arguments.get("engine_id"),
                page=1,
                page_size=arguments.get("limit", 10),
            )

    async def _resource_ancestors_get(self, arguments: dict[str, Any], delegated_token: str) -> Any:
        async with self._client(MetaClient, delegated_token) as client:
            return await client.get_resource_tree_ancestors(arguments["engine_id"], arguments["locator"])

    async def _data_preview(self, arguments: dict[str, Any], delegated_token: str) -> Any:
        async with self._client(ManagerClient, delegated_token) as client:
            return await client.preview_by_locator(
                arguments["locator"],
                page=1,
                page_size=arguments.get("limit", 10),
            )

    async def _workflow_operators_list(self, arguments: dict[str, Any], delegated_token: str) -> Any:
        async with self._client(DevelopClient, delegated_token) as client:
            operators = await client.list_operators(arguments["workflow_engine_id"])
            return {
                "workflow_engine_id": arguments["workflow_engine_id"],
                "count": len(operators),
                "operators": operators,
            }

    async def _workflow_draft_generate(self, arguments: dict[str, Any], delegated_token: str) -> Any:
        async with self._client(CopilotClient, delegated_token) as client:
            return await client.generate_workflow(
                query=arguments["query"],
                workflow_engine_id=arguments["workflow_engine_id"],
                resources=arguments["resources"],
            )

    async def _query_draft_generate(self, arguments: dict[str, Any], delegated_token: str) -> Any:
        async with self._client(CopilotClient, delegated_token) as client:
            return await client.generate_query(
                query=arguments["query"],
                engine_id=arguments["engine_id"],
                query_language=arguments["query_language"],
                resources=arguments["resources"],
                engine_context=arguments["engine_context"],
                current_query=arguments.get("current_query"),
            )

    async def _notebook_draft_generate(self, arguments: dict[str, Any], delegated_token: str) -> Any:
        async with self._client(CopilotClient, delegated_token) as client:
            return await client.generate_notebook(
                query=arguments["query"],
                kernel=arguments.get("kernel", "python3"),
                candidates=arguments.get("candidates", []),
                resources=arguments.get("resources", []),
            )

    async def _transfer_draft_generate(self, arguments: dict[str, Any], delegated_token: str) -> Any:
        async with self._client(CopilotClient, delegated_token) as client:
            return await client.generate_transfer(
                query=arguments["query"],
                resources=arguments.get("resources", []),
                task=arguments.get("task"),
            )

    async def _workflow_validate(self, arguments: dict[str, Any], delegated_token: str) -> Any:
        async with self._client(DevelopClient, delegated_token) as client:
            return await client.validate_workflow(
                arguments["workflow_definition"],
                arguments["workflow_engine_id"],
            )

    async def _workflow_run(self, arguments: dict[str, Any], delegated_token: str) -> Any:
        async with self._client(DevelopClient, delegated_token) as client:
            if arguments.get("approval_id"):
                return await client.resume_workflow_run(
                    arguments["approval_id"],
                    arguments["request_fingerprint"],
                )
            return await client.run_workflow_content(
                arguments["workflow_definition"],
                engine_id=arguments["workflow_engine_id"],
                engine_specific=arguments.get("engine_specific"),
            )

    async def _execution_get(self, arguments: dict[str, Any], delegated_token: str) -> Any:
        async with self._client(DevelopClient, delegated_token) as client:
            result = await client.get_execution(arguments["execution_id"])
        allowed = {
            "execution_id",
            "status",
            "progress",
            "current_step",
            "error_details",
            "execution_time_ms",
            "records_read",
            "records_written",
            "bytes_read",
            "bytes_written",
            "started_at",
            "completed_at",
            "created_at",
            "updated_at",
        }
        return {key: value for key, value in result.items() if key in allowed}

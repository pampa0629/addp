import argparse
import asyncio
import json
import os
import sys
import uuid
from typing import Any

import keyring

from addp_common.tools import ToolExecutionError, ToolExecutor, get_tool, load_manifest
from addp_common.oauth import (
    OAuthLoginError,
    browser_login,
    device_login,
    has_login,
    logout,
    refresh_access_token,
)


EXIT_OK = 0
EXIT_USAGE = 2
EXIT_TOOL_NOT_FOUND = 3
EXIT_EXECUTION_FAILED = 4


def _write_json(value: Any) -> None:
    sys.stdout.write(json.dumps(value, ensure_ascii=False, separators=(",", ":")) + "\n")


class JSONArgumentParser(argparse.ArgumentParser):
    def error(self, message):
        _write_json({"error": {"code": "invalid_command", "message": message}})
        raise SystemExit(EXIT_USAGE)


def _parser() -> argparse.ArgumentParser:
    parser = JSONArgumentParser(prog="addp")
    parser.add_argument("--base-url", default=os.getenv("ADDP_BASE_URL", "http://localhost:8000"))
    parser.add_argument("--token", default=os.getenv("ADDP_TOKEN"))
    parser.add_argument("--console-url", default=os.getenv("ADDP_CONSOLE_URL", "http://localhost:5170"))
    groups = parser.add_subparsers(dest="group", required=True)

    tools = groups.add_parser("tools")
    tools_commands = tools.add_subparsers(dest="command", required=True)
    tools_commands.add_parser("list")
    get_parser = tools_commands.add_parser("get")
    get_parser.add_argument("name")

    tool = groups.add_parser("tool")
    tool_commands = tool.add_subparsers(dest="command", required=True)
    call_parser = tool_commands.add_parser("call")
    call_parser.add_argument("name")
    call_parser.add_argument("--arguments", default="{}")
    call_parser.add_argument("--agent-run-id", default=os.getenv("ADDP_AGENT_RUN_ID"))
    call_parser.add_argument("--tool-call-id", default=os.getenv("ADDP_TOOL_CALL_ID"))

    auth = groups.add_parser("auth")
    auth_commands = auth.add_subparsers(dest="command", required=True)
    login_parser = auth_commands.add_parser("login")
    login_parser.add_argument("--device", action="store_true")
    auth_commands.add_parser("status")
    auth_commands.add_parser("logout")
    return parser


async def _run(args: argparse.Namespace) -> int:
    if args.group == "auth":
        try:
            if args.command == "status":
                _write_json({"authenticated": has_login(args.base_url), "base_url": args.base_url})
                return EXIT_OK
            if args.command == "logout":
                await logout(args.base_url)
                _write_json({"authenticated": False})
                return EXIT_OK
            if args.device:
                result, _device = await device_login(
                    args.base_url,
                    on_device=lambda device: _write_json({"device_authorization": device}),
                )
            else:
                result = await browser_login(args.base_url, args.console_url)
            _write_json({"authenticated": True, "client_id": result.client_id, "scope": result.scope})
            return EXIT_OK
        except (OAuthLoginError, keyring.errors.KeyringError) as exc:
            _write_json({"error": {"code": "authentication_failed", "message": str(exc)}})
            return EXIT_EXECUTION_FAILED

    if args.group == "tools" and args.command == "list":
        _write_json([tool.model_dump() for tool in load_manifest().tools])
        return EXIT_OK

    if args.group == "tools" and args.command == "get":
        try:
            definition = get_tool(args.name)
        except KeyError:
            _write_json({"error": {"code": "tool_not_found", "message": f"未知 ADDP Tool: {args.name}"}})
            return EXIT_TOOL_NOT_FOUND
        _write_json(definition.model_dump())
        return EXIT_OK

    user_token = args.token
    if not user_token:
        try:
            user_token = await refresh_access_token(args.base_url)
        except OAuthLoginError:
            _write_json({"error": {"code": "authentication_required", "message": "请先执行 addp auth login"}})
            return EXIT_USAGE

    try:
        arguments = json.loads(args.arguments)
        if not isinstance(arguments, dict):
            raise ValueError("arguments must be a JSON object")
    except (json.JSONDecodeError, ValueError) as exc:
        _write_json({"error": {"code": "invalid_json", "message": str(exc)}})
        return EXIT_USAGE

    try:
        result = await ToolExecutor(args.base_url, user_token).call(
            args.name,
            arguments,
            agent_run_id=getattr(args, "agent_run_id", None) or str(uuid.uuid4()),
            tool_call_id=getattr(args, "tool_call_id", None) or str(uuid.uuid4()),
        )
    except ToolExecutionError as exc:
        _write_json(exc.as_dict())
        return EXIT_TOOL_NOT_FOUND if exc.code == "tool_not_found" else EXIT_EXECUTION_FAILED
    except Exception as exc:
        print(f"addp CLI internal error: {type(exc).__name__}: {exc}", file=sys.stderr)
        _write_json({"error": {"code": "internal_error", "message": "CLI 内部错误"}})
        return EXIT_EXECUTION_FAILED
    _write_json(result)
    return EXIT_OK


def main() -> int:
    return asyncio.run(_run(_parser().parse_args()))


if __name__ == "__main__":
    raise SystemExit(main())

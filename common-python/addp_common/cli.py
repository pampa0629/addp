import argparse
import asyncio
import json
import os
import sys
import uuid
from importlib.metadata import PackageNotFoundError, version
from typing import Any

import httpx
import keyring

from addp_common.auth import AuthorizationContext, resolve_authorization_context
from addp_common.tools import ToolExecutionError, ToolExecutor, get_tool, load_manifest
from addp_common.oauth import (
    OAuthLoginError,
    browser_login,
    device_login,
    logout,
    _normalize_base_url,
    refresh_access_token,
)


EXIT_OK = 0
EXIT_USAGE = 2
EXIT_TOOL_NOT_FOUND = 3
EXIT_EXECUTION_FAILED = 4

try:
    PACKAGE_VERSION = version("addp-common")
except PackageNotFoundError:
    PACKAGE_VERSION = "0.1.0"


def _write_json(value: Any) -> None:
    sys.stdout.write(json.dumps(value, ensure_ascii=False, separators=(",", ":")) + "\n")


def _write_device_instructions(device: dict[str, Any]) -> None:
    verification_uri = str(device.get("verification_uri", ""))
    user_code = str(device.get("user_code", ""))
    expires_in = device.get("expires_in")
    print(f"请打开 {verification_uri} 并输入设备验证码 {user_code}", file=sys.stderr)
    if isinstance(expires_in, int):
        print(f"设备验证码将在 {expires_in} 秒后过期", file=sys.stderr)


class JSONArgumentParser(argparse.ArgumentParser):
    def error(self, message):
        _write_json({"error": {"code": "invalid_command", "message": message}})
        raise SystemExit(EXIT_USAGE)


class JSONVersionAction(argparse.Action):
    def __init__(self, option_strings, dest=argparse.SUPPRESS, default=argparse.SUPPRESS, **kwargs):
        super().__init__(option_strings, dest, nargs=0, default=default, **kwargs)

    def __call__(self, parser, namespace, values, option_string=None):
        _write_json({"name": parser.prog, "version": PACKAGE_VERSION})
        parser.exit(EXIT_OK)


def _authentication_summary(
    base_url: str,
    context: AuthorizationContext,
) -> dict[str, Any]:
    context_value: dict[str, str] = {"type": context.context_type}
    if context.context_type == "tenant":
        context_value.update({
            "tenant_id": str(context.tenant_id),
            "tenant_membership_id": str(context.tenant_membership_id),
        })
    return {
        "authenticated": True,
        "base_url": base_url,
        "principal": {"type": context.principal_type, "id": str(context.principal_id)},
        "context": context_value,
        "authentication": {"assurance_level": context.assurance_level},
        "client": {
            "client_id": context.client_id,
            "scope_mode": context.scope_mode,
            "scopes": list(context.scopes),
        },
    }


async def _resolve_authentication(base_url: str, access_token: str) -> AuthorizationContext:
    try:
        return await resolve_authorization_context(base_url, access_token)
    except httpx.HTTPStatusError as exc:
        if exc.response.status_code == 401:
            raise OAuthLoginError("invalid_grant") from exc
        raise OAuthLoginError("auth_context_unavailable") from exc
    except httpx.HTTPError as exc:
        raise OAuthLoginError("auth_context_unavailable") from exc
    except ValueError as exc:
        raise OAuthLoginError("invalid_auth_context_response") from exc


def _parser() -> argparse.ArgumentParser:
    parser = JSONArgumentParser(prog="addp")
    parser.add_argument("--version", action=JSONVersionAction)
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
    if args.group in {"auth", "tool"}:
        try:
            args.base_url = _normalize_base_url(args.base_url)
        except OAuthLoginError as exc:
            _write_json({"error": {"code": "invalid_command", "message": str(exc)}})
            return EXIT_USAGE

    if args.group == "auth":
        try:
            if args.command == "status":
                try:
                    access_token = await refresh_access_token(args.base_url)
                except OAuthLoginError as exc:
                    if str(exc) in {"authentication_required", "invalid_grant"}:
                        _write_json({"authenticated": False, "base_url": args.base_url})
                        return EXIT_OK
                    raise
                context = await _resolve_authentication(args.base_url, access_token)
                _write_json(_authentication_summary(args.base_url, context))
                return EXIT_OK
            if args.command == "logout":
                await logout(args.base_url)
                _write_json({"authenticated": False, "base_url": args.base_url})
                return EXIT_OK
            if args.device:
                result, _device = await device_login(
                    args.base_url,
                    on_device=_write_device_instructions,
                )
            else:
                result = await browser_login(args.base_url, args.console_url)
            context = await _resolve_authentication(args.base_url, result.access_token)
            _write_json(_authentication_summary(args.base_url, context))
            return EXIT_OK
        except (OAuthLoginError, keyring.errors.KeyringError) as exc:
            error_code = "authentication_failed"
            if args.command == "status":
                error_code = "authentication_unavailable"
            elif args.command == "logout":
                error_code = "logout_failed"
            _write_json({"error": {"code": error_code, "message": str(exc)}})
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

    try:
        arguments = json.loads(args.arguments)
        if not isinstance(arguments, dict):
            raise ValueError("arguments must be a JSON object")
    except (json.JSONDecodeError, ValueError) as exc:
        _write_json({"error": {"code": "invalid_json", "message": str(exc)}})
        return EXIT_USAGE

    user_token = args.token
    if not user_token:
        try:
            user_token = await refresh_access_token(args.base_url)
        except OAuthLoginError as exc:
            if str(exc) in {"authentication_required", "invalid_grant"}:
                _write_json({"error": {"code": "authentication_required", "message": "请先执行 addp auth login"}})
                return EXIT_USAGE
            _write_json({
                "error": {
                    "code": "authentication_unavailable",
                    "message": f"无法刷新 ADDP 登录会话: {exc}",
                }
            })
            return EXIT_EXECUTION_FAILED
        except keyring.errors.KeyringError:
            _write_json({
                "error": {
                    "code": "authentication_unavailable",
                    "message": "无法访问操作系统凭据存储",
                }
            })
            return EXIT_EXECUTION_FAILED

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

import argparse
import asyncio
import json
from unittest.mock import patch

from addp_common import cli


def test_tools_list_writes_strict_json(capsys):
    code = asyncio.run(cli._run(argparse.Namespace(
        group="tools",
        command="list",
        base_url="http://gateway",
        token=None,
    )))
    output = capsys.readouterr().out
    assert code == cli.EXIT_OK
    assert json.loads(output)[0]["name"] == "engine.list"
    assert "\n" not in output.rstrip("\n")


def test_tool_call_requires_user_token(capsys):
    code = asyncio.run(cli._run(argparse.Namespace(
        group="tool",
        command="call",
        name="data.search",
        arguments='{"query":"roads"}',
        base_url="http://gateway",
        token=None,
    )))
    payload = json.loads(capsys.readouterr().out)
    assert code == cli.EXIT_USAGE
    assert payload["error"]["code"] == "authentication_required"


def test_tool_call_outputs_result_without_adapter_envelope(capsys):
    class FakeExecutor:
        def __init__(self, base_url, token):
            assert base_url == "http://gateway"
            assert token == "token"

        async def call(self, name, arguments):
            assert name == "data.search"
            assert arguments == {"query": "roads"}
            return {"total": 1, "hits": []}

    with patch.object(cli, "ToolExecutor", FakeExecutor):
        code = asyncio.run(cli._run(argparse.Namespace(
            group="tool",
            command="call",
            name="data.search",
            arguments='{"query":"roads"}',
            base_url="http://gateway",
            token="token",
        )))

    assert code == cli.EXIT_OK
    assert json.loads(capsys.readouterr().out) == {"total": 1, "hits": []}

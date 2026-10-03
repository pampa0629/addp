"""Exercise the actual Uvicorn configuration without starting a listener."""
import json
from pathlib import Path
import subprocess
import sys

import pytest


@pytest.mark.parametrize("launch", ["script", "asgi_import"])
def test_server_startup_records_use_shared_structured_output(launch):
    probe = '''
import logging
import runpy
from unittest.mock import patch
import uvicorn

def emit_startup():
    logging.getLogger("uvicorn.error").info("Waiting for application startup.")
    logging.getLogger("uvicorn.error").error("Server startup failure probe")
'''
    if launch == "script":
        probe += '''
def run_without_listener(*args, **kwargs):
    uvicorn.Config(*args, **kwargs)
    emit_startup()
with patch("uvicorn.run", side_effect=run_without_listener):
    runpy.run_path("main.py", run_name="__main__")
'''
    else:
        probe += '''
config = uvicorn.Config("main:app", log_level="info")
config.load()
emit_startup()
'''
    result = subprocess.run(
        [sys.executable, "-c", probe],
        cwd=Path(__file__).resolve().parents[1],
        capture_output=True,
        text=True,
        check=True,
        timeout=30,
    )
    events = [json.loads(line) for line in result.stdout.splitlines()]
    server = [event for event in events if event["logger"] == "uvicorn.error"]
    assert [(event["level"], event["message"]) for event in server] == [
        ("info", "Waiting for application startup."),
        ("error", "Server startup failure probe"),
    ]
    assert "Waiting for application startup." not in result.stderr
    assert "Server startup failure probe" not in result.stderr

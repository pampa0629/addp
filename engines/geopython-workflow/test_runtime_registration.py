import json


def test_register_to_system_uses_canonical_runtime_identity(monkeypatch):
    calls = []

    monkeypatch.setenv("SYSTEM_URL", "http://system:8180")
    monkeypatch.setenv("GEOPYTHON_WORKFLOW_SERVICE_CLIENT_SECRET", "test-secret")
    monkeypatch.setenv("PORT", "8099")
    monkeypatch.setenv("RUNTIME_PUBLIC_PORT", "18099")

    def fake_register_runtime_engine(system_url, client_id, client_secret, payload):
        calls.append((system_url, client_id, client_secret, payload))
        return 202, json.dumps({"success": True, "engine_id": 6})

    import addp_common.client as client

    monkeypatch.setattr(client, "register_runtime_engine", fake_register_runtime_engine)

    from api_server import register_to_system

    assert register_to_system() is True
    assert calls == [(
        "http://system:8180",
        "addp-geopython",
        "test-secret",
        {
            "engine_type": "geopython_workflow",
            "name": "GeoPython Workflow",
            "description": "基于 Python 地理计算生态的工作流引擎，支持 Pandas、GeoPandas、GDAL/OGR 等能力",
            "connection_info": {"protocol": "http", "port": 18099, "host": "localhost"},
            "capabilities": {
                "schema_version": "engine.capabilities/v1",
                "engine_type": "geopython_workflow",
                "engine_family": "workflow",
                "compute": {
                    "workflow": {
                        "supported": True,
                        "runtime_api": "addp.workflow/v1",
                        "dynamic_operators": True,
                    }
                },
            },
            "is_builtin": True,
        },
    )]


def test_real_gunicorn_listens_before_registration_and_shares_worker_state(tmp_path):
    """Exercise the production entry with isolated HTTP and registration fixtures."""
    import os
    from pathlib import Path
    import socket
    import subprocess
    import sys
    import time
    from urllib.request import ProxyHandler, build_opener

    with socket.socket() as listener:
        listener.bind(('127.0.0.1', 0))
        port = listener.getsockname()[1]
    registration = tmp_path / 'registration.json'
    script = '''
import api_server, json, os
from pathlib import Path
from urllib.request import ProxyHandler, build_opener
state = {'count': 0}
@api_server.app.route('/probe-state')
def probe_state():
    state['count'] += 1
    return {'pid': os.getpid(), 'count': state['count']}
def register():
    with build_opener(ProxyHandler({})).open('http://127.0.0.1:' + os.environ['PORT'] + '/health') as r:
        Path(os.environ['REGISTRATION']).write_text(json.dumps({'health': r.status, 'pid': os.getpid()}))
api_server.register_to_system_with_retry = register
api_server.run_runtime()
'''
    env = dict(os.environ, PORT=str(port), WORKFLOW_BIND_HOST='127.0.0.1', REGISTRATION=str(registration),
               SYSTEM_URL='http://127.0.0.1:1', GEOPYTHON_WORKFLOW_SERVICE_CLIENT_SECRET='')
    opener = build_opener(ProxyHandler({}))
    log = tmp_path / 'server.log'
    with log.open('w') as stream:
        process = subprocess.Popen([sys.executable, '-c', script], cwd=Path(__file__).parent,
                                   env=env, stdout=stream, stderr=subprocess.STDOUT)
        try:
            deadline = time.monotonic() + 30
            while not registration.exists() and time.monotonic() < deadline:
                assert process.poll() is None, log.read_text()
                time.sleep(.1)
            assert registration.exists(), log.read_text()
            evidence = json.loads(registration.read_text())
            assert evidence['health'] == 200
            responses = [json.loads(opener.open(f'http://127.0.0.1:{port}/probe-state', timeout=3).read()) for _ in range(3)]
            assert [r['count'] for r in responses] == [1, 2, 3]
            assert {r['pid'] for r in responses} == {evidence['pid']}
            assert evidence['pid'] != process.pid
        finally:
            process.terminate()
            process.wait(timeout=15)
        assert process.returncode == 0, log.read_text()

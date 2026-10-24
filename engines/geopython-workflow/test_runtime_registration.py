import json


def test_health_uses_structured_local_dependency_facts():
    import api_server
    with api_server.app.test_client() as client:
        response = client.get("/health")
    assert response.status_code == 200
    body = response.get_json()
    assert body["status"] == "healthy"
    assert body["service"] and body["version"]
    assert {"geopandas", "pandas", "gdal"} == set(body["dependencies"])
    assert all(fact["available"] is True and fact["required"] is True
               for fact in body["dependencies"].values())


def test_health_probe_failure_returns_503_and_keeps_identity(monkeypatch):
    import api_server
    def fail():
        raise RuntimeError("private diagnostic")
    monkeypatch.setattr(api_server, "list_operators", fail)
    with api_server.app.test_client() as client:
        response = client.get("/health")
    body = response.get_json()
    assert response.status_code == 503
    assert body["status"] == "unhealthy"
    assert body["service"] and body["version"]
    assert body["error_code"] == "health_check_failed"
    assert "private diagnostic" not in response.get_data(as_text=True)


def test_real_entry_executes_first_crs_workflow_without_worker_replacement(tmp_path):
    """The real script must initialize native CRS safely in its execution thread."""
    import os
    from pathlib import Path
    import socket
    import subprocess
    from operators import _native_process
    import sys
    import time
    from urllib.request import ProxyHandler, Request, build_opener

    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        port = listener.getsockname()[1]
    env = dict(os.environ, PORT=str(port), WORKFLOW_BIND_HOST="127.0.0.1",
               SYSTEM_URL="http://127.0.0.1:1", GEOPYTHON_WORKFLOW_SERVICE_CLIENT_SECRET="",
               PYTHONFAULTHANDLER="1")
    opener = build_opener(ProxyHandler({}))
    base = f"http://127.0.0.1:{port}"
    log = tmp_path / "native-entry.log"
    with log.open("w") as stream:
        process = _native_process.start([sys.executable, "api_server.py"], cwd=Path(__file__).parent,
                                   env=env, stdout=stream, stderr=subprocess.STDOUT)
        try:
            deadline = time.monotonic() + 30
            while time.monotonic() < deadline:
                assert process.poll() is None, log.read_text()
                try:
                    with opener.open(base + "/health", timeout=1) as response:
                        if response.status == 200:
                            break
                except OSError:
                    pass
                time.sleep(.1)
            else:
                raise AssertionError(log.read_text())
            source = {"type": "FeatureCollection", "features": [{"type": "Feature", "properties": {},
                "geometry": {"type": "LineString", "coordinates": [[0, 0], [1, 0], [1, 1], [0, 1], [0, 0]]}}]}
            payload = {"workflow_def": {"tasks": [{"id": "faces", "operator": "polygonize",
                "params": {"input_gdf": source}, "depends_on": []}]},
                "runtime": {"tenant_id": 7, "execution_authorization": {"id": 1, "effects": ["read"]}}}
            request = Request(base + "/api/workflow", data=json.dumps(payload).encode(),
                              headers={"Content-Type": "application/json"})
            with opener.open(request, timeout=10) as response:
                assert response.status == 202
                execution_id = json.load(response)["execution_id"]
            deadline = time.monotonic() + 10
            while time.monotonic() < deadline:
                with opener.open(base + "/api/executions/" + execution_id, timeout=3) as response:
                    state = json.load(response)
                if state["status"] in {"success", "failed"}:
                    break
                time.sleep(.01)
            assert state["status"] == "success", (state, log.read_text())
            assert len(json.loads(state["result"])["result"]["features"]) == 1
            assert log.read_text().count("Booting worker with pid:") == 1, log.read_text()
        except Exception as exc:
            raise AssertionError(log.read_text()) from exc
        finally:
            process.terminate()
            process.wait(timeout=15)
        assert process.returncode == 0, log.read_text()


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
                        "storage_access": {
                            "read_engine_types": ["postgresql", "mysql", "doris", "mongodb", "nfs"],
                            "write_engine_types": ["postgresql", "mysql", "doris", "nfs"],
                        },
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
    from operators import _native_process
    import sys
    import time
    from urllib.request import ProxyHandler, build_opener

    with socket.socket() as listener:
        listener.bind(('127.0.0.1', 0))
        port = listener.getsockname()[1]
    registration = tmp_path / 'registration.json'
    vector_source = tmp_path / 'native-crs.geojson'
    vector_source.write_text(json.dumps({
        'type': 'FeatureCollection', 'features': [
            {'type': 'Feature', 'properties': {'name': '天安门'},
             'geometry': {'type': 'Point', 'coordinates': [116.3974, 39.9093]}}
        ]
    }), encoding='utf-8')
    exchange_sources = {
        'kml': '<kml xmlns="http://www.opengis.net/kml/2.2"><Document><Folder><name>sites</name><Placemark><name>长沙</name><Point><coordinates>112.98,28.19</coordinates></Point></Placemark></Folder></Document></kml>',
        'gml': '<gml:FeatureCollection xmlns:gml="http://www.opengis.net/gml" xmlns:app="urn:test"><gml:featureMember><app:sites><app:name>长沙</app:name><app:position><gml:Point srsName="urn:ogc:def:crs:EPSG::4326"><gml:pos>28.19 112.98</gml:pos></gml:Point></app:position></app:sites></gml:featureMember></gml:FeatureCollection>',
        'topojson': json.dumps({'type': 'Topology', 'objects': {'sites': {
            'type': 'GeometryCollection', 'geometries': [{'type': 'Point',
                'properties': {'name': '长沙'}, 'coordinates': [112.98, 28.19]}]}}, 'arcs': []}),
    }
    for format_name, content in exchange_sources.items():
        (tmp_path / ('sites.' + format_name)).write_text(content, encoding='utf-8')
    probe = '''
import api_server, json, os
from pathlib import Path
from urllib.request import ProxyHandler, build_opener
state = {'count': 0}
@api_server.app.route('/probe-state')
def probe_state():
    state['count'] += 1
    return {'pid': os.getpid(), 'count': state['count']}
@api_server.app.route('/probe-vector-crs')
def probe_vector_crs():
    from operators.vector_tile_operators import _vector_source_facts, _extent_to_wgs84
    layer, fields, geometry = _vector_source_facts(os.environ['VECTOR_SOURCE'], {}, {}, 'EPSG:4326')
    extent = _extent_to_wgs84([500000, 3000000, 501000, 3001000], 4549)
    return {'layer': layer, 'fields': fields, 'geometry': geometry, 'extent': extent, 'pid': os.getpid()}
@api_server.app.route('/probe-vector-exchange/<format_name>')
def probe_vector_exchange(format_name):
    from workflow_engine import execute_single_operator
    plan = {'schema_version': 'addp.workflow.access-plan/v1', 'source': {
        'kind': 'file', 'format': format_name, 'access': {'method': 'mounted_path',
        'path': str(Path(os.environ['VECTOR_SOURCE']).parent / ('sites.' + format_name))}}}
    def invoke(name, **params):
        result = execute_single_operator('vector_dataset.' + name, {'access_plan': plan, **params})
        assert result['status'] == 'success', result.get('error')
        return result['result']
    inspected = invoke('inspect')
    layer = inspected['container']['children'][0]['name']
    opened = invoke('read_open', protocol='gdal.vector-batch/v1', layer=layer)
    rows = invoke('read_batch', protocol='gdal.vector-batch/v1', layer=layer, offset=0, limit=2)['rows']
    invoke('read_close', protocol='gdal.vector-batch/v1', layer=layer)
    return {'pid': os.getpid(), 'count': len(rows), 'srid': opened['spatial'].get('srid'),
            'name': rows[0].get('Name', rows[0].get('name'))}
def register():
    with build_opener(ProxyHandler({})).open('http://127.0.0.1:' + os.environ['PORT'] + '/health') as r:
        Path(os.environ['REGISTRATION']).write_text(json.dumps({'health': r.status, 'pid': os.getpid()}))
api_server.register_to_system_with_retry = register
'''
    # Install the test-only routes when the worker imports the application.
    # The real api_server.py master remains free of GIS imports before fork.
    script = '''
import importlib.abc, importlib.machinery, runpy, sys
from gunicorn.arbiter import Arbiter
original_spawn = Arbiter.spawn_worker
def spawn_worker(self):
    assert not any(name in sys.modules for name in ('api_server', 'osgeo.gdal', 'pyproj'))
    return original_spawn(self)
Arbiter.spawn_worker = spawn_worker
class Probe(importlib.abc.MetaPathFinder, importlib.abc.Loader):
    def find_spec(self, fullname, path=None, target=None):
        if fullname != 'api_server': return None
        spec = importlib.machinery.PathFinder.find_spec(fullname, path)
        self.loader = spec.loader
        spec.loader = self
        return spec
    def create_module(self, spec): return None
    def exec_module(self, module):
        self.loader.exec_module(module)
        exec(PROBE, {})
sys.meta_path.insert(0, Probe())
runpy.run_path('api_server.py', run_name='__main__')
'''.replace('PROBE', repr(probe))
    env = dict(os.environ, PORT=str(port), WORKFLOW_BIND_HOST='127.0.0.1', REGISTRATION=str(registration), VECTOR_SOURCE=str(vector_source),
               SYSTEM_URL='http://127.0.0.1:1', GEOPYTHON_WORKFLOW_SERVICE_CLIENT_SECRET='')
    opener = build_opener(ProxyHandler({}))
    log = tmp_path / 'server.log'
    with log.open('w') as stream:
        process = _native_process.start([sys.executable, '-c', script], cwd=Path(__file__).parent,
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
            for format_name in exchange_sources:
                exchange = json.loads(opener.open(f'http://127.0.0.1:{port}/probe-vector-exchange/{format_name}', timeout=30).read())
                assert exchange == {'pid': evidence['pid'], 'count': 1, 'name': '长沙',
                                    'srid': None if format_name == 'topojson' else 4326}
            native = json.loads(opener.open(f'http://127.0.0.1:{port}/probe-vector-crs', timeout=15).read())
            assert native['pid'] == evidence['pid']
            assert native['fields'] == {'name': 'String'}
            assert native['geometry'] == '_ogr_geometry_'
            assert 100 < native['extent'][0] < native['extent'][2] < 130
            assert 20 < native['extent'][1] < native['extent'][3] < 40
            after_native = json.loads(opener.open(f'http://127.0.0.1:{port}/probe-state', timeout=3).read())
            assert after_native == {'pid': evidence['pid'], 'count': 4}
        finally:
            process.terminate()
            process.wait(timeout=15)
        assert process.returncode == 0, log.read_text()

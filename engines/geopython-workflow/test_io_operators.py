from bson import Decimal128, ObjectId

from operators import io_operators
from operators.io_operators import _load_mongodb_collection, _map_loopback_host


def test_map_loopback_host_uses_container_shared_host(monkeypatch):
    monkeypatch.setenv('GEOPYTHON_WORKFLOW_LOOPBACK_HOST', 'host.docker.internal')

    assert _map_loopback_host('localhost') == 'host.docker.internal'
    assert _map_loopback_host('127.0.0.1') == 'host.docker.internal'
    assert _map_loopback_host('::1') == 'host.docker.internal'


def test_map_loopback_host_preserves_remote_host(monkeypatch):
    monkeypatch.setenv('GEOPYTHON_WORKFLOW_LOOPBACK_HOST', 'host.docker.internal')

    assert _map_loopback_host('database.internal') == 'database.internal'


def test_map_loopback_host_is_inactive_without_shared_host(monkeypatch):
    monkeypatch.delenv('GEOPYTHON_WORKFLOW_LOOPBACK_HOST', raising=False)

    assert _map_loopback_host('localhost') == 'localhost'


def test_load_mongodb_collection_executes_pipeline_and_normalizes_bson(monkeypatch):
    calls = {}
    object_id = ObjectId()

    class FakeCollection:
        def aggregate(self, pipeline, allowDiskUse):
            calls['pipeline'] = pipeline
            calls['allow_disk_use'] = allowDiskUse
            return [{'_id': object_id, 'amount': Decimal128('1.25')}]

    class FakeDatabase:
        def __getitem__(self, name):
            calls['collection'] = name
            return FakeCollection()

    class FakeClient:
        def __init__(self, **kwargs):
            calls['connection'] = kwargs

        def __getitem__(self, name):
            calls['database'] = name
            return FakeDatabase()

        def close(self):
            calls['closed'] = True

    monkeypatch.setattr(io_operators, 'MongoClient', FakeClient)

    result = _load_mongodb_collection(
        {
            'host': 'localhost', 'port': 27017, 'username': 'reader',
            'password': 'secret', 'auth_source': 'admin',
        },
        'Outdoor',
        'Outdoors',
        [{'$project': {'amount': 1}}],
    )

    assert calls['database'] == 'Outdoor'
    assert calls['collection'] == 'Outdoors'
    assert calls['pipeline'] == [{'$project': {'amount': 1}}]
    assert calls['allow_disk_use'] is True
    assert calls['closed'] is True
    assert result.to_dict('records') == [{'_id': str(object_id), 'amount': Decimal128('1.25').to_decimal()}]


def test_load_mongodb_uses_schema_as_database_when_connection_database_is_empty(monkeypatch):
    calls = {}

    class FakeCollection:
        def find(self, query):
            calls['query'] = query
            return [{'activity_id': 'activity-1'}]

    class FakeDatabase:
        def __getitem__(self, name):
            calls['collection'] = name
            return FakeCollection()

    class FakeClient:
        def __init__(self, **kwargs):
            calls['connection'] = kwargs

        def __getitem__(self, name):
            calls['database'] = name
            return FakeDatabase()

        def close(self):
            calls['closed'] = True

    monkeypatch.setattr(io_operators, 'MongoClient', FakeClient)

    result = io_operators.load(
        connection_info={
            'engine_type': 'mongodb', 'host': 'localhost', 'port': 27017,
            'username': 'reader', 'password': 'secret', 'auth_source': 'admin',
            'database': '',
        },
        schema='Outdoor',
        table='Outdoors',
    )

    assert calls['database'] == 'Outdoor'
    assert calls['collection'] == 'Outdoors'
    assert calls['query'] == {}
    assert calls['closed'] is True
    assert result.to_dict('records') == [{'activity_id': 'activity-1'}]

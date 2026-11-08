"""Exercise the table fixture's physical oracle with actual Arrow and Parquet."""
import importlib.util
import io
from pathlib import Path
import sys
import types

import pyarrow as pa
import pyarrow.parquet as pq
import pytest

ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location('online_avro_fixture', ROOT / 'business/scripts/online-avro-fixture.py')
FIXTURE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(FIXTURE)


@pytest.fixture
def avro_physical(tmp_path, monkeypatch):
    objects = {}

    class Response(io.BytesIO):
        def release_conn(self):
            pass

    class Client:
        def __init__(self, *args, **kwargs):
            pass

        def get_object(self, bucket, name):
            return Response(objects[name])

        def put_object(self, bucket, name, stream, length, **kwargs):
            objects[name] = stream.read()
            assert len(objects[name]) == length

        def remove_object(self, bucket, name):
            objects.pop(name, None)

        def list_objects(self, bucket, prefix, recursive):
            return [types.SimpleNamespace(object_name=n) for n in objects if n.startswith(prefix)]

    monkeypatch.setitem(sys.modules, 'minio', types.SimpleNamespace(Minio=Client))
    request = {'nfs_root': str(tmp_path), 'storage': {'endpoint': 'fixture', 'access_key': 'test', 'secret_key': 'test', 'bucket': 'fixture'}}
    schema = pa.schema([('id', pa.int64()), ('small', pa.int32()), ('flag', pa.bool_()), ('label', pa.string()),
                        ('ratio', pa.float32()), ('value', pa.float64()), ('payload', pa.binary())])

    def write_outputs(change=None):
        rows = [FIXTURE.row(i) for i in range(FIXTURE.COUNT)]
        if change:
            change(rows)
        sink = io.BytesIO()
        pq.write_table(pa.Table.from_pylist(rows, schema=schema), sink, row_group_size=1000)
        for storage in ('minio', 'nfs'):
            for codec in FIXTURE.CODECS:
                objects[f'avro-output/from-{storage}-{codec}.parquet'] = sink.getvalue()

    return request, objects, write_outputs


def test_hosted_avro_actual_parquet_oracle_and_cleanup(avro_physical):
    request, objects, write_outputs = avro_physical
    seeded = FIXTURE.worker(dict(request, action='seed'))
    assert seeded == {'sources': FIXTURE.sources(), 'count': 2503}
    write_outputs()
    proof = FIXTURE.worker(dict(request, action='verify'))
    assert proof['sources_unchanged'] and len(proof['outputs']) == 6
    assert all(p['all_values_and_nulls_verified'] and p['size_bytes'] > 0 for p in proof['outputs'])
    assert FIXTURE.worker(dict(request, action='delete')) == {'objects_removed': True, 'files_removed': True, 'outputs_removed': True}
    assert not objects and not (Path(request['nfs_root']) / 'avro').exists()


@pytest.mark.parametrize('mutation', ['bigint', 'empty_bytes', 'source'])
def test_hosted_avro_oracle_rejects_real_parquet_or_source_corruption(avro_physical, mutation):
    request, objects, write_outputs = avro_physical
    FIXTURE.worker(dict(request, action='seed'))
    if mutation == 'bigint':
        write_outputs(lambda rows: rows[0].update(id=9007199254740992))
    elif mutation == 'empty_bytes':
        write_outputs(lambda rows: rows[3].update(payload=None))
    else:
        write_outputs()
        objects['avro/null.avro'] += b'changed'
    with pytest.raises(AssertionError):
        FIXTURE.worker(dict(request, action='verify'))
    FIXTURE.worker(dict(request, action='delete'))
    assert not objects


def test_hosted_avro_cleanup_preserves_an_unclaimed_directory(avro_physical):
    request, objects, _ = avro_physical
    directory = Path(request['nfs_root']) / 'avro'
    directory.mkdir()
    foreign = directory / 'null.avro'
    foreign.write_bytes(b'not owned by this format fixture')
    objects['avro/null.avro'] = b'foreign'
    with pytest.raises(FileNotFoundError):
        FIXTURE.worker(dict(request, action='delete'))
    assert foreign.read_bytes() == b'not owned by this format fixture'
    assert objects == {'avro/null.avro': b'foreign'}

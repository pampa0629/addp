"""Feather V2 real PyArrow interoperability and bounded Runtime protocol."""
import base64
import io
import json
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch
import pytest
import pyarrow as pa
import pyarrow.feather as writer
from operators import feather_table as feather


def plan(path):
    return {'schema_version': 'addp.workflow.access-plan/v1', 'source': {
        'kind': 'file', 'format': 'feather', 'access': {'method': 'mounted_path', 'path': str(path)}}}


def write(path, table=None, codec='lz4', chunksize=10, version=2):
    if table is None:
        table = pa.table({'id': pa.array([2**63-1, None, -2**63]*9, type=pa.int64()),
                          '名称': ['中文𠀀😀', None, '']*9, 'blob': [b'\0\xff', None, b'']*9})
    writer.write_feather(table, path, compression=codec if version == 2 else None,
                         chunksize=chunksize if version == 2 else None, version=version)
    return plan(path)


def describe(p):
    return feather.inspect(feather.PROTOCOL, p)['describe']


def read(p, d, offset=0, limit=20, columns=None):
    return feather.read_batch(feather.PROTOCOL, p, d['format_info']['source_version'], offset, limit,
                              columns or [f['name'] for f in d['table']['fields']])


@pytest.mark.parametrize('codec', ['uncompressed', 'lz4', 'zstd'])
def test_three_codecs_real_batches_and_exact_windows(tmp_path, codec):
    p = write(tmp_path/'table.feather', codec=codec)
    d = describe(p)
    assert d['table']['row_count'] == 27
    assert d['format_info']['record_batches'] == 3
    assert d['format_info']['version'] == 2 and d['format_info']['validation'] == 'batch_structure'
    assert [(f['name'], f['type'], f['nullable']) for f in d['table']['fields']] == [
        ('id', 'bigint', True), ('名称', 'string', True), ('blob', 'bytes', True)]
    for offset, limit in [(0, 20), (20, 20), (27, 20), (100, 1), (8, 7)]:
        rows = read(p, d, offset, limit)['rows']
        assert rows == [{'id': [str(2**63-1), None, str(-2**63)][i%3],
                         '名称': ['中文𠀀😀', None, ''][i%3],
                         'blob': ['AP8=', None, ''][i%3]} for i in range(offset, min(27, offset+limit))]
    assert read(p, d, 9, 3, ['名称'])['rows'] == [{'名称': v} for v in ['中文𠀀😀', None, '']]


def test_scalar_types_nullability_and_zero_rows(tmp_path):
    types = [pa.bool_(), pa.int8(), pa.int16(), pa.int32(), pa.int64(), pa.float32(), pa.float64(), pa.string(), pa.binary()]
    values = [True, -128, -32768, -2**31, 2**63-1, 1.25, -2.5, '', b'']
    schema = pa.schema([pa.field(f'c{i}', t, nullable=False) for i,t in enumerate(types)])
    table = pa.Table.from_arrays([pa.array([v], type=t) for t,v in zip(types,values)],schema=schema)
    p=write(tmp_path/'scalar.arrow',table);d=describe(p)
    assert all(not f['nullable'] for f in d['table']['fields'])
    expected={f'c{i}': str(v) if i==4 else '' if i==8 else v for i,v in enumerate(values)}
    assert read(p,d)['rows']==[expected]
    p=write(tmp_path/'empty.arrow',table.slice(0,0));d=describe(p)
    assert d['table']['row_count']==0 and read(p,d)['rows']==[]


@pytest.mark.parametrize('typ,values',[(pa.uint64(),[1]), (pa.float16(),[1.0]), (pa.date32(),[1]),
    (pa.timestamp('ns'),[1]), (pa.decimal128(10,2),[None]), (pa.list_(pa.int32()),[[1]]),
    (pa.struct([('x',pa.int32())]),[{'x':1}]),(pa.dictionary(pa.int8(),pa.string()),['a']),
    (pa.large_string(),['a']), (pa.binary(2),[b'ab']), (pa.null(),[None])])
def test_unsupported_schema_rejected_before_column_projection(tmp_path,typ,values):
    p=write(tmp_path/'unsupported.feather',pa.table({'safe':[1], 'other':pa.array(values,type=typ)}))
    with pytest.raises(ValueError):describe(p)
    with feather._open(p, '') as source: version=source.version
    with pytest.raises(ValueError,match='scalar profile'):
        feather.read_batch(feather.PROTOCOL,p,version,0,1,['safe'])


def test_v1_stream_truncation_duplicate_names_and_extension_metadata(tmp_path):
    p=write(tmp_path/'v1.feather',version=1)
    with pytest.raises(ValueError):describe(p)
    path=tmp_path/'stream.arrow'
    with pa.ipc.new_stream(str(path),pa.schema([('id',pa.int64())])) as out:
        out.write_table(pa.table({'id':[1]}))
    with pytest.raises(ValueError):describe(plan(path))
    table=pa.Table.from_arrays([pa.array([1]),pa.array([2])],names=['same','same'])
    with pytest.raises(ValueError):describe(write(tmp_path/'duplicate.arrow',table))
    field=pa.field('binary',pa.binary(),metadata={b'ARROW:extension:name':b'geoarrow.wkb'})
    table=pa.Table.from_arrays([pa.array([b'abc'])],schema=pa.schema([field]))
    with pytest.raises(ValueError):describe(write(tmp_path/'extension.feather',table))
    path=tmp_path/'truncated.feather';write(path);path.write_bytes(path.read_bytes()[:-8])
    with pytest.raises(Exception):describe(plan(path))


def test_budgets_source_changes_and_nonfinite(tmp_path):
    path=tmp_path/'source.feather';p=write(path);d=describe(p)
    for offset,limit in [(-1,1),(0,0),(True,1),(0,2001)]:
        with pytest.raises(ValueError):read(p,d,offset,limit)
    for columns in [['missing'],['id','id'],['id',True]]:
        with pytest.raises(ValueError):read(p,d,columns=columns)
    for key,cap in [('MAX_FILE_BYTES',4),('MAX_FOOTER_BYTES',1),('MAX_COLUMNS',1),('MAX_BATCHES',1),
                    ('MAX_DECODED_BATCH_BYTES',1),('MAX_DECODED_BATCH_ROWS',1),('MAX_READ_BYTES',1)]:
        with patch.object(feather,key,cap),pytest.raises(ValueError):describe(p)
    with patch.object(feather,'MAX_RESPONSE_BYTES',1),pytest.raises(ValueError):read(p,d)
    with patch.object(feather,'MAX_VALUE_BYTES',1),pytest.raises(ValueError):read(p,d,columns=['名称'])
    for value in [float('nan'),float('inf'),-float('inf')]:
        q=write(tmp_path/'nonfinite.feather',pa.table({'f':[value]}));v=describe(q)
        with pytest.raises(ValueError):read(q,v)
    write(path,pa.table({'replacement':[5]}))
    with pytest.raises(feather.SourceChanged):read(p,d)


def test_declared_nonnullable_must_match_buffers(tmp_path):
    table=pa.Table.from_arrays([pa.array([None],type=pa.int32())],schema=pa.schema([pa.field('id',pa.int32(),nullable=False)]))
    # Arrow IPC permits serializing contradictory nullable metadata; ADDP must refuse it.
    p=write(tmp_path/'nullability.feather',table)
    with pytest.raises(ValueError):describe(p)


class ObjectClient:
    def __init__(self,data):self.data,self.etag,self.ranges=data,'fixed',[]
    def stat_object(self,*args):return SimpleNamespace(size=len(self.data),etag=self.etag,version_id=None)
    def get_object(self,bucket,name,offset,length,request_headers):
        assert (bucket,name)==('bucket','exact.feather') and request_headers=={'If-Match':'"fixed"'}
        self.ranges.append((offset,length));stream=io.BytesIO(self.data[offset:offset+length]);stream.release_conn=lambda:None
        return stream


def test_conditional_ranges_and_exact_source_binding(tmp_path):
    path=tmp_path/'sample.feather';write(path)
    client=ObjectClient(path.read_bytes())
    p={'schema_version':'addp.workflow.access-plan/v1','source':{'kind':'file','format':'feather','access':{
        'method':'object_store','endpoint':'example.invalid:9000','bucket':'bucket','object':'exact.feather',
        'access_key':'fixture','secret_key':'fixture'}}}
    with patch.object(feather,'_object_store_client',return_value=client):
        d=describe(p);assert len(read(p,d,8,7)['rows'])==7
        assert client.ranges and all(n<len(client.data) for _,n in client.ranges)
        client.etag='changed'
        with pytest.raises(feather.SourceChanged):read(p,d)


def test_registered_native_process_and_http_errors(tmp_path):
    from api_server import app
    path=tmp_path/'native.feather';p=write(path)
    inspect=feather.OPERATORS['feather.inspect']['function'];batch=feather.OPERATORS['feather.read_batch']['function']
    d=inspect(protocol=feather.PROTOCOL,access_plan=p)['describe']
    assert d['table']['row_count']==27
    args={'protocol':feather.PROTOCOL,'access_plan':p,'source_version':d['format_info']['source_version'],'offset':8,'limit':3,'columns':['id']}
    assert batch(**args)['rows']==[{'id':str(-2**63)},{'id':str(2**63-1)},{'id':None}]
    for lang in ['zh-cn','en']:
        response=app.test_client().post('/api/operators/feather.read_batch/invoke',json={'params':dict(args,limit=0)},headers={'Accept-Language':lang})
        assert response.status_code==400 and response.json['error_code']=='FEATHER_READ_REJECTED'
        assert str(path) not in json.dumps(response.json)
    write(path,pa.table({'new':[1]}))
    response=app.test_client().post('/api/operators/feather.read_batch/invoke',json={'params':args})
    assert response.status_code==409 and response.json['error_code']=='FEATHER_SOURCE_CHANGED'
    assert '已变化' in response.json['error']
    with patch.object(feather._native_process,'run',side_effect=TimeoutError('/private/credentials')):
        response=app.test_client().post('/api/operators/feather.inspect/invoke',json={'params':{'protocol':feather.PROTOCOL,'access_plan':p}})
        assert response.status_code==500 and response.json['error_code']=='FEATHER_READ_FAILED'
        assert 'credentials' not in json.dumps(response.json)

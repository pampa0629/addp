"""Owned Feather source producer and independent full Parquet readback oracle."""
from __future__ import annotations
import base64
import hashlib
import io
import json
from pathlib import Path
import sys

COUNT = 2503
CODECS = ('uncompressed', 'lz4', 'zstd')
FIELD_NAMES = ['id','tiny','small','number','flag','label','ratio','value','payload']
NULLABLE = {'small','flag','label','payload'}
FIELD_TYPES = ['bigint','int','int','int','bool','string','float','double','bytes']
NATIVE_TYPES = ['int64','int8','int16','int32','bool','string','float','double','binary']


def row(i):
    return {'id':(2**53+1+i)*(-1 if i%2 else 1),'tiny':i%256-128,
            'small':None if i%17==0 else i-1251,'number':i*100000-125100000,
            'flag':None if i%7==0 else i%2==0,
            'label':None if i%11==0 else '' if i%5==0 else f'设备𠀀😀-{i:04d}',
            'ratio':i/4,'value':-i/2,
            'payload':None if i%13==0 else b'' if i%3==0 else bytes([0,i%256,255])}


def public_row(i):
    result=row(i);result['id']=str(result['id'])
    if result['payload'] is not None:result['payload']=base64.b64encode(result['payload']).decode()
    return result


def source_bytes(codec):
    import pyarrow as pa
    import pyarrow.feather as feather
    schema=pa.schema([pa.field(n,t,nullable=n in NULLABLE) for n,t in zip(FIELD_NAMES,
        [pa.int64(),pa.int8(),pa.int16(),pa.int32(),pa.bool_(),pa.string(),pa.float32(),pa.float64(),pa.binary()])])
    table=pa.Table.from_pylist([row(i) for i in range(COUNT)],schema=schema)
    out=io.BytesIO();feather.write_feather(table,out,compression=codec,chunksize=1024)
    return out.getvalue()


def worker(request):
    from minio import Minio
    access=request['storage'];client=Minio(access['endpoint'],access_key=access['access_key'],secret_key=access['secret_key'],secure=False)
    bucket=access['bucket'];directory=Path(request['nfs_root'])/'feather';marker=directory/'.addp-feather-fixture'
    outputs=[f'feather-output/from-{store}-{codec}.parquet' for store in ('minio','nfs') for codec in CODECS]
    if directory.is_symlink():raise ValueError('Feather fixture root is a symlink')
    def remote(name):
        response=client.get_object(bucket,name)
        try:return response.read()
        finally:response.close();response.release_conn()
    def specs():
        return [{'codec':c,'name':c+'.feather','size_bytes':len(raw),'sha256':hashlib.sha256(raw).hexdigest()}
                for c in CODECS for raw in [source_bytes(c)]]
    action=request['action']
    if action=='seed':
        assert not list(client.list_objects(bucket,prefix='feather/',recursive=True)),'existing_inputs'
        assert not list(client.list_objects(bucket,prefix='feather-output/',recursive=True)),'existing_outputs'
        directory.mkdir(exist_ok=False);marker.write_text('addp.feather-fixture/v1')
        for codec in CODECS:
            raw=source_bytes(codec);path=directory/(codec+'.feather');path.write_bytes(raw);path.chmod(0o644)
            client.put_object(bucket,'feather/'+path.name,io.BytesIO(raw),len(raw),content_type='application/vnd.apache.arrow.file')
        client.put_object(bucket,'feather-output/.keep',io.BytesIO(b''),0)
        return {'sources':specs(),'count':COUNT}
    if action not in {'verify','delete'}:raise ValueError('unknown fixture action')
    if not directory.exists() and action=='delete':
        assert not list(client.list_objects(bucket,prefix='feather/',recursive=True)),'unowned_inputs'
        assert not list(client.list_objects(bucket,prefix='feather-output/',recursive=True)),'unowned_outputs'
        return {'objects_removed':True,'files_removed':True,'outputs_removed':True}
    assert not marker.is_symlink() and marker.read_text()=='addp.feather-fixture/v1','fixture_ownership'
    assert all(not (directory/(c+'.feather')).is_symlink() for c in CODECS),'source_symlink'
    if action=='verify':
        import pyarrow as pa
        import pyarrow.parquet as pq
        for codec in CODECS:
            assert (directory/(codec+'.feather')).read_bytes()==remote('feather/'+codec+'.feather')==source_bytes(codec),'source_changed'
        expected_types={'id':pa.int64(),'tiny':pa.int32(),'small':pa.int32(),'number':pa.int32(),
                        'flag':pa.bool_(),'label':pa.string(),'ratio':pa.float32(),'value':pa.float64(),'payload':pa.binary()}
        facts=[]
        for name in outputs:
            raw=remote(name);table=pq.read_table(io.BytesIO(raw))
            assert table.column_names==sorted(FIELD_NAMES) and table.num_rows==COUNT,'output_fields'
            assert all(table.schema.field(n).type==t and table.schema.field(n).nullable==(n in NULLABLE) for n,t in expected_types.items()),'output_types'
            assert table.to_pylist()==[row(i) for i in range(COUNT)],'output_values'
            facts.append({'object':name,'size_bytes':len(raw),'sha256':hashlib.sha256(raw).hexdigest(),
                          'all_values_and_nulls_verified':True,'content_base64':base64.b64encode(raw).decode()})
        return {'sources_unchanged':True,'outputs':facts}
    for name in outputs+['feather-output/.keep']+['feather/'+c+'.feather' for c in CODECS]:client.remove_object(bucket,name)
    for codec in CODECS:(directory/(codec+'.feather')).unlink(missing_ok=True)
    marker.unlink();directory.rmdir()
    assert not list(client.list_objects(bucket,prefix='feather/',recursive=True)),'input_cleanup'
    assert not list(client.list_objects(bucket,prefix='feather-output/',recursive=True)),'output_cleanup'
    return {'objects_removed':True,'files_removed':True,'outputs_removed':True}


def physical(action,repository,environment):
    import importlib
    return importlib.import_module('scripts.test.online-table-fixture-support').physical_worker(
        action,repository,environment,profile='feather',script='business/scripts/online-feather-fixture.py')


if __name__=='__main__':
    try:print(json.dumps(worker(json.load(sys.stdin))))
    except Exception as error:
        allowed={'existing_inputs','existing_outputs','unowned_inputs','unowned_outputs','fixture_ownership',
                 'source_symlink','source_changed','output_fields','output_types','output_values','input_cleanup','output_cleanup'}
        print(json.dumps({'error_code':str(error) if isinstance(error,AssertionError) and str(error) in allowed else type(error).__name__}))
        raise SystemExit(1)

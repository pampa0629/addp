"""Real Feather/Parquet fixture oracle; never imports the product decoder."""
import importlib.util
import io
import json
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch
import pytest
import pyarrow as pa
import pyarrow.feather as feather
import pyarrow.parquet as pq

spec=importlib.util.spec_from_file_location('feather_fixture',Path(__file__).resolve().parents[2]/'business/scripts/online-feather-fixture.py')
fixture=importlib.util.module_from_spec(spec);spec.loader.exec_module(fixture)


class Client:
    def __init__(self):self.objects={}
    def put_object(self,bucket,name,data,length,**kwargs):self.objects[name]=data.read()
    def get_object(self,bucket,name):
        stream=io.BytesIO(self.objects[name]);stream.release_conn=lambda:None;return stream
    def list_objects(self,bucket,prefix,**kwargs):return [n for n in self.objects if n.startswith(prefix)]
    def remove_object(self,bucket,name):self.objects.pop(name,None)


def test_three_codecs_all_scalar_values_and_nulls():
    for codec in fixture.CODECS:
        raw=fixture.source_bytes(codec);table=feather.read_table(io.BytesIO(raw))
        assert table.num_rows==2503 and table.column_names==fixture.FIELD_NAMES
        assert table.to_pylist()==[fixture.row(i) for i in range(fixture.COUNT)]
        assert pa.ipc.open_file(io.BytesIO(raw)).num_record_batches==3
        assert table['id'][1].as_py()==-(2**53+2) and fixture.public_row(1)['id']==str(-(2**53+2))


def test_owned_physical_roundtrip_and_tampering(tmp_path):
    client=Client();request={'nfs_root':str(tmp_path),'storage':{'endpoint':'fixture','access_key':'fixture','secret_key':'fixture','bucket':'bucket'}}
    types=[pa.int64(),pa.int32(),pa.int32(),pa.int32(),pa.bool_(),pa.string(),pa.float32(),pa.float64(),pa.binary()]
    schema=pa.schema(sorted([pa.field(n,t,nullable=n in fixture.NULLABLE) for n,t in zip(fixture.FIELD_NAMES,types)],key=lambda f:f.name))
    with patch.dict('sys.modules',{'minio':SimpleNamespace(Minio=lambda *a,**k:client)}):
        seed=fixture.worker(dict(request,action='seed'));assert len(seed['sources'])==3
        for store in ['minio','nfs']:
            for codec in fixture.CODECS:
                out=io.BytesIO();pq.write_table(pa.Table.from_pylist([fixture.row(i) for i in range(fixture.COUNT)],schema=schema),out)
                client.objects[f'feather-output/from-{store}-{codec}.parquet']=out.getvalue()
        verified=fixture.worker(dict(request,action='verify'))
        assert verified['sources_unchanged'] and len(verified['outputs'])==6
        original=client.objects['feather-output/from-minio-lz4.parquet']
        rows=[fixture.row(i) for i in range(fixture.COUNT)];rows[2048]['id']=1
        out=io.BytesIO();pq.write_table(pa.Table.from_pylist(rows,schema=schema),out)
        client.objects['feather-output/from-minio-lz4.parquet']=out.getvalue()
        with pytest.raises(AssertionError,match='output_values'):fixture.worker(dict(request,action='verify'))
        client.objects['feather-output/from-minio-lz4.parquet']=original
        assert fixture.worker(dict(request,action='delete'))=={'objects_removed':True,'files_removed':True,'outputs_removed':True}
        assert not client.objects and not list(tmp_path.iterdir())

"""Owned Feather source producer and independent full Parquet readback oracle."""
from __future__ import annotations
import base64
from datetime import date
from decimal import Decimal
import hashlib
import io
import json
from pathlib import Path
import sys

COUNT = 2503
CODECS = ('uncompressed', 'lz4', 'zstd')
FIELD_NAMES = ['id','tiny','small','number','flag','label','ratio','value','payload','day','legacy_day','amount','wide_amount']
NULLABLE = {'small','flag','label','payload','legacy_day','amount'}
FIELD_TYPES = ['bigint','int','int','int','bool','string','float','double','bytes','date','date','decimal','decimal']
NATIVE_TYPES = ['int64','int8','int16','int32','bool','string','float','double','binary','date32[day]','date64[ms]','decimal128(38, 18)','decimal256(76, 38)']

DECIMAL_SHAPES = {'amount': (38, 18), 'wide_amount': (76, 38)}
DAYS = ('1969-12-31', '1970-01-01', '2000-02-29')
AMOUNTS = ('-99999999999999999999.123456789012345678', '1.230000000000000000', None)
WIDE_AMOUNTS = ('9' * 38 + '.' + '7' * 38, '-0.' + '0' * 37 + '1', '0.' + '0' * 38)


# Integer ticks and independent preview expectations. The two New York instants
# are the repeated 01:30 across the 2021 fall DST transition.
TEMPORAL = {
    'clock_second': ('time', 's', '', (0, 86399, 1),
                     ('00:00:00', '23:59:59', '00:00:01')),
    'clock_nano': ('time', 'ns', '', (1, 86399999999999, None),
                   ('00:00:00.000000001', '23:59:59.999999999', None)),
    'local_stamp': ('timestamp', 'ns', '', (-1, -(2**63), 0),
                    ('1969-12-31T23:59:59.999999999', '1677-09-21T00:12:43.145224192',
                     '1970-01-01T00:00:00.000000000')),
    'zone_stamp': ('timestamp', 'ns', 'America/New_York', (1636263000000000001, 1636266600000000001, None),
                   ('2021-11-07T05:30:00.000000001Z', '2021-11-07T06:30:00.000000001Z', None)),
    'offset_stamp': ('timestamp', 's', '+05:30', (-1, 0, 2147483648),
                     ('1969-12-31T23:59:59Z', '1970-01-01T00:00:00Z', '2038-01-19T03:14:08Z')),
}
FIELD_NAMES += list(TEMPORAL)
FIELD_TYPES += [definition[0] for definition in TEMPORAL.values()]
NATIVE_TYPES += ['time32[s]', 'time64[ns]', 'timestamp[ns]', 'timestamp[ns, tz=America/New_York]',
                 'timestamp[s, tz=+05:30]']
NULLABLE |= {name for name, definition in TEMPORAL.items() if None in definition[3]}


def field_definitions():
    return [
        dict(name=name, type=kind, native_type=native, nullable=name in NULLABLE,
             precision=DECIMAL_SHAPES.get(name, (0, 0))[0], scale=DECIMAL_SHAPES.get(name, (0, 0))[1],
             **({'temporal': {'unit': TEMPORAL[name][1], **({'timezone': TEMPORAL[name][2]}
                 if TEMPORAL[name][2] else {})}} if name in TEMPORAL else {}))
        for name, kind, native in zip(FIELD_NAMES, FIELD_TYPES, NATIVE_TYPES, strict=True)
    ]


def arrow_schema(*, parquet=False):
    import pyarrow as pa
    types = [pa.int64(), pa.int8(), pa.int16(), pa.int32(), pa.bool_(), pa.string(),
             pa.float32(), pa.float64(), pa.binary(), pa.date32(), pa.date64(),
             pa.decimal128(38, 18), pa.decimal256(76, 38)]
    if parquet:
        types[1:3] = [pa.int32(), pa.int32()]
        types[10] = pa.date32()
    for kind, unit, timezone, _, _ in TEMPORAL.values():
        if parquet and unit == 's':
            unit = 'ms'
        if kind == 'time':
            types.append(pa.time32(unit) if unit in {'s', 'ms'} else pa.time64(unit))
        else:
            types.append(pa.timestamp(unit, timezone or None))
    fields = [pa.field(n, t, nullable=n in NULLABLE) for n, t in zip(FIELD_NAMES, types, strict=True)]
    return pa.schema(sorted(fields, key=lambda f: f.name) if parquet else fields)


def row(i):
    return {'id':(2**53+1+i)*(-1 if i%2 else 1),'tiny':i%256-128,
            'small':None if i%17==0 else i-1251,'number':i*100000-125100000,
            'flag':None if i%7==0 else i%2==0,
            'label':None if i%11==0 else '' if i%5==0 else f'设备𠀀😀-{i:04d}',
            'ratio':i/4,'value':-i/2,
             'payload': None if i%13==0 else b'' if i%3==0 else bytes([0,i%256,255]),
            'day': date.fromisoformat(DAYS[i % 3]),
            'legacy_day': None if i % 3 == 0 else date.fromisoformat(DAYS[(i + 1) % 3]),
            'amount': None if AMOUNTS[i % 3] is None else Decimal(AMOUNTS[i % 3]),
            'wide_amount': Decimal(WIDE_AMOUNTS[i % 3]),
            **{name: definition[3][i % 3] for name, definition in TEMPORAL.items()}}


def public_row(i):
    result=row(i);result['id']=str(result['id'])
    if result['payload'] is not None:result['payload']=base64.b64encode(result['payload']).decode()
    for key in ('day', 'legacy_day'):
        if result[key] is not None:
            result[key] = result[key].isoformat()
    for key in DECIMAL_SHAPES:
        if result[key] is not None:
            result[key] = format(result[key], 'f')
    for key, definition in TEMPORAL.items():
        result[key] = definition[4][i % 3]
    return result


def arrow_table(rows, *, parquet=False):
    import pyarrow as pa
    schema = arrow_schema(parquet=parquet)
    columns = []
    for field in schema:
        values = [row[field.name] for row in rows]
        if field.name in TEMPORAL:
            if parquet and TEMPORAL[field.name][1] == 's':
                values = [None if value is None else value * 1000 for value in values]
            physical = pa.int32() if pa.types.is_time32(field.type) else pa.int64()
            columns.append(pa.array(values, physical).cast(field.type))
        else:
            columns.append(pa.array(values, field.type))
    return pa.Table.from_arrays(columns, schema=schema)


def assert_values(table, *, parquet=False):
    import pyarrow as pa
    values = table.drop(list(TEMPORAL)).to_pylist()
    for name, (_, unit, _, _, _) in TEMPORAL.items():
        column = table[name]
        physical = pa.int32() if pa.types.is_time32(column.type) else pa.int64()
        for entry, ticks in zip(values, column.cast(physical).to_pylist(), strict=True):
            if parquet and unit == 's' and ticks is not None:
                assert ticks % 1000 == 0, 'output_values'
                ticks //= 1000
            entry[name] = ticks
    assert values == [row(i) for i in range(COUNT)], 'output_values'


def source_bytes(codec):
    import pyarrow.feather as feather
    table = arrow_table([row(i) for i in range(COUNT)])
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
        import pyarrow.parquet as pq
        for codec in CODECS:
            assert (directory/(codec+'.feather')).read_bytes()==remote('feather/'+codec+'.feather')==source_bytes(codec),'source_changed'
        expected_schema = arrow_schema(parquet=True)
        facts=[]
        for name in outputs:
            raw = remote(name)
            parquet_file = pq.ParquetFile(io.BytesIO(raw))
            table = parquet_file.read()
            assert table.column_names==sorted(FIELD_NAMES) and table.num_rows==COUNT,'output_fields'
            assert table.schema.equals(expected_schema, check_metadata=False), 'output_types'
            for field, (kind, unit, timezone, _, _) in TEMPORAL.items():
                column = parquet_file.schema.column(parquet_file.schema.names.index(field))
                logical = json.loads(column.logical_type.to_json())
                assert logical['Type'] == kind.capitalize(), 'output_types'
                assert logical['isAdjustedToUTC'] is bool(timezone), 'output_types'
                assert logical['timeUnit'] == ('milliseconds' if unit == 's' else 'nanoseconds'), 'output_types'
                assert column.physical_type == ('INT32' if kind == 'time' and unit == 's' else 'INT64'), 'output_types'
            assert_values(table, parquet=True)
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

"""Independent OCF scalar encoder and full Parquet output oracle for Hosted Avro."""
from __future__ import annotations

import base64
import datetime
from decimal import Decimal
import hashlib
import io
import json
from pathlib import Path
import struct
import sys
import zlib

COUNT = 2503
CODECS = ('null', 'deflate', 'snappy')
SCHEMA = {'type': 'record', 'name': 'AvroScalars', 'fields': [
    {'name': 'id', 'type': 'long'}, {'name': 'small', 'type': 'int'},
    {'name': 'flag', 'type': ['null', 'boolean']},
    {'name': 'label', 'type': ['null', 'string']},
    {'name': 'ratio', 'type': 'float'}, {'name': 'value', 'type': 'double'},
    {'name': 'payload', 'type': ['null', 'bytes']},
    {'name': 'day', 'type': ['null', {'type': 'int', 'logicalType': 'date'}]},
    {'name': 'amount', 'type': ['null', {'type': 'bytes', 'logicalType': 'decimal', 'precision': 38, 'scale': 6}]},
    {'name': 'fixed_amount', 'type': ['null', {'type': 'fixed', 'name': 'FixedAmount', 'size': 16, 'logicalType': 'decimal', 'precision': 38, 'scale': 6}]},
]}

# Original signed ticks; timestamp-nanos includes the valid int64 minimum,
# which must not be treated as a pandas/NumPy NaT sentinel.
TIMESTAMPS = {
    'stamp_ms': ('ms', (0, -1, 1, 946720800123, -62135596800000, 253402300799999, 1730611800123), 29),
    'stamp_us': ('us', (0, -1, 1, 946720800123456, -62135596800000000, 253402300799999999, 9007199254740993, -9007199254740993), 31),
    'stamp_ns': ('ns', (0, -1, 1, 946720800123456789, -(2**63), 2**63-1, 9007199254740993, -9007199254740993), 37),
}
for name, (unit, _, _) in TIMESTAMPS.items():
    logical = {'ms':'timestamp-millis', 'us':'timestamp-micros', 'ns':'timestamp-nanos'}[unit]
    SCHEMA['fields'].append({'name': name, 'type': ['null', {'type':'long', 'logicalType':logical}]})


def utc_text(ticks, unit):
    digits = {'ms':3, 'us':6, 'ns':9}[unit]
    seconds, fraction = divmod(ticks, 10**digits)
    clock = datetime.datetime(1970, 1, 1) + datetime.timedelta(seconds=seconds)
    return clock.isoformat(timespec='seconds') + '.' + str(fraction).zfill(digits) + 'Z'


def row(index):
    days = [datetime.date(1, 1, 1), datetime.date(9999, 12, 31), datetime.date(2000, 2, 29), datetime.date(1969, 12, 31)]
    day = days[index] if index < 4 else datetime.date(1970, 1, 1) + datetime.timedelta(days=index - 1251)
    unscaled = (10**38 - 10**7 + index * 100) * (-1 if index % 2 else 1)
    def exact(value):
        digits = str(abs(value)).zfill(7)
        return Decimal(('-' if value < 0 else '') + digits[:-6] + '.' + digits[-6:])
    result = {'id': (-1 if index % 2 else 1) * (2**53 + 1 + index),
            'small': index - 1251,
            'flag': None if index % 7 == 0 else index % 2 == 0,
            'label': None if index % 11 == 0 else '' if index % 5 == 0 else f'设备-{index:04d}',
            'ratio': index / 4, 'value': -index / 2,
            'payload': None if index % 13 == 0 else b'' if index % 3 == 0 else bytes([0, index % 256, 255]),
            'day': None if index > 3 and index % 17 == 0 else day,
            'amount': None if index % 19 == 0 else exact(unscaled),
            'fixed_amount': None if index % 23 == 0 else exact(-unscaled)}
    for name, (_, values, period) in TIMESTAMPS.items():
        result[name] = None if index > 7 and index % period == 0 else values[index % len(values)]
    return result


def public_row(index):
    result = row(index)
    result['id'] = str(result['id'])
    if result['payload'] is not None:
        result['payload'] = base64.b64encode(result['payload']).decode()
    if result['day'] is not None:
        result['day'] = result['day'].isoformat()
    for name in ('amount', 'fixed_amount'):
        if result[name] is not None:
            result[name] = format(result[name], 'f')
    for name, (unit, _, _) in TIMESTAMPS.items():
        if result[name] is not None:
            result[name] = utc_text(result[name], unit)
    return result


def unsigned(value):
    result = bytearray()
    while value > 127:
        result.append((value & 127) | 128)
        value >>= 7
    result.append(value)
    return bytes(result)


def number(value):
    return unsigned((value << 1) ^ (value >> 63))


def blob(value):
    return number(len(value)) + value


def source_bytes(codec):
    if codec not in CODECS:
        raise ValueError('unsupported Avro fixture codec')
    # Independent Apache OCF framing, no product reader or Go codec involved.
    sync = hashlib.sha256(('addp-avro-fixture-' + codec).encode()).digest()[:16]
    metadata = {'avro.schema': json.dumps(SCHEMA, separators=(',', ':')).encode(), 'avro.codec': codec.encode()}
    result = b'Obj\x01' + number(len(metadata))
    for key, value in metadata.items():
        result += blob(key.encode()) + blob(value)
    result += number(0) + sync
    for start in range(0, COUNT, 257):
        raw = bytearray()
        for i in range(start, min(start + 257, COUNT)):
            value = row(i)
            raw.extend(number(value['id']) + number(value['small']))
            for name in ('flag', 'label'):
                v = value[name]
                raw.extend(number(0 if v is None else 1))
                if v is not None:
                    raw.extend(bytes([v]) if name == 'flag' else blob(v.encode()))
            raw.extend(struct.pack('<fd', value['ratio'], value['value']))
            v = value['payload']
            raw.extend(number(0 if v is None else 1))
            if v is not None:
                raw.extend(blob(v))
            for name in ('day', 'amount', 'fixed_amount'):
                v = value[name]
                raw.extend(number(0 if v is None else 1))
                if v is not None:
                    if name == 'day':
                        raw.extend(number((v - datetime.date(1970, 1, 1)).days))
                    else:
                        # Decimal's tuple is exact; scaleb/int arithmetic would
                        # round this 38-digit value under the default context.
                        parts = v.as_tuple()
                        integer = int(''.join(map(str, parts.digits))) * (-1 if parts.sign else 1)
                        if name == 'fixed_amount':
                            raw.extend(integer.to_bytes(16, 'big', signed=True))
                        else:
                            width = max(1, (integer.bit_length() + 8) // 8)
                            encoded = integer.to_bytes(width, 'big', signed=True)
                            if i % 5 == 0:
                                encoded = (b'\xff' if integer < 0 else b'\0') + encoded
                            raw.extend(blob(encoded))
            for name in TIMESTAMPS:
                ticks = value[name]
                raw.extend(number(0 if ticks is None else 1))
                if ticks is not None:
                    raw.extend(number(ticks))
        payload = bytes(raw)
        if codec == 'deflate':
            compressor = zlib.compressobj(wbits=-15)
            payload = compressor.compress(payload) + compressor.flush()
        elif codec == 'snappy':
            # A legal raw Snappy literal block, per Google's format_description.
            n = len(payload) - 1
            width = max(1, (n.bit_length() + 7) // 8)
            tag = bytes([n << 2]) if n < 60 else bytes([(59 + width) << 2]) + n.to_bytes(width, 'little')
            payload = unsigned(len(payload)) + tag + payload + struct.pack('>I', zlib.crc32(payload))
        result += number(min(257, COUNT - start)) + blob(payload) + sync
    return result


def sources():
    return [{'codec': c, 'name': c + '.avro', 'count': COUNT, 'schema': SCHEMA,
             'size_bytes': len(raw), 'sha256': hashlib.sha256(raw).hexdigest()}
            for c in CODECS for raw in [source_bytes(c)]]



# The six flat sources above remain byte-identical. Nested sources are separate
# exact files; their own OCF bytes exercise legal positive/negative/multi-block arrays.
NESTED_DETAIL = {'type':'record','name':'Detail','fields':[
    {'name':'uid','type':['long','null']},
    {'name':'amount','type':['null',{'type':'bytes','logicalType':'decimal','precision':38,'scale':6}]},
    {'name':'fixed_amount','type':[{'type':'fixed','name':'NMoney','size':16,'logicalType':'decimal','precision':38,'scale':6},'null']},
    {'name':'day','type':['null',{'type':'int','logicalType':'date'}]},
    *[{'name':n,'type':['null',{'type':'long','logicalType':{'ms':'timestamp-millis','us':'timestamp-micros','ns':'timestamp-nanos'}[unit]}]} for n,(unit,_,_) in TIMESTAMPS.items()],
    {'name':'payload','type':['null','bytes']}, {'name':'label','type':['null','string']},
]}
NESTED_SCHEMA = {'type':'record','name':'example.Nested','fields':[
    {'name':'id','type':'long'}, {'name':'detail','type':['null',NESTED_DETAIL]},
    {'name':'items','type':[{'type':'array','items':['Detail','null']},'null']},
    {'name':'grid','type':['null',{'type':'array','items':['null',{'type':'array','items':['long','null']}]}]},
    {'name':'fixed_values','type':{'type':'array','items':['null','NMoney']}},
]}

def nested_detail(index, empty=False):
    value = row(index)
    return {field['name']: None if empty else value['id' if field['name']=='uid' else field['name']] for field in NESTED_DETAIL['fields']}

def nested_row(index):
    value = row(index)
    return {'id':value['id'], 'detail':None if index%3==0 else nested_detail(index,index%5==0),
            'items':None if index%7==0 else [] if index%5==0 else [nested_detail(index),None,nested_detail(index,True)],
            'grid':None if index%11==0 else [[value['id'],None],[],None],
            'fixed_values':[None,value['fixed_amount']]}

def nested_public_row(index):
    value=nested_row(index)
    def detail(item):
        if item is None:return None
        result=dict(item)
        if result['uid'] is not None:result['uid']=str(result['uid'])
        for name in ('amount','fixed_amount'):
            if result[name] is not None:result[name]=format(result[name],'f')
        if result['day'] is not None:result['day']=result['day'].isoformat()
        if result['payload'] is not None:result['payload']=base64.b64encode(result['payload']).decode()
        for name,(unit,_,_) in TIMESTAMPS.items():
            if result[name] is not None:result[name]=utc_text(result[name],unit)
        return result
    return {'id':str(value['id']), 'detail':detail(value['detail']),
            'items':None if value['items'] is None else [detail(item) for item in value['items']],
            'grid':None if value['grid'] is None else [[str(value['id']),None],[],None],
            'fixed_values':[None,None if value['fixed_values'][1] is None else format(value['fixed_values'][1],'f')]}

def nested_source_bytes(codec):
    if codec not in CODECS:raise ValueError('unsupported Avro fixture codec')
    sync=hashlib.sha256(('addp-avro-nested-fixture-'+codec).encode()).digest()[:16]
    metadata={'avro.schema':json.dumps(NESTED_SCHEMA,separators=(',',':')).encode(),'avro.codec':codec.encode()}
    out=b'Obj\x01'+number(2)+b''.join(blob(k.encode())+blob(v) for k,v in metadata.items())+number(0)+sync
    def decimal(value,fixed):
        parts=value.as_tuple();integer=int(''.join(map(str,parts.digits)))*(-1 if parts.sign else 1)
        raw=integer.to_bytes(16 if fixed else max(1,(integer.bit_length()+8)//8),'big',signed=True)
        return raw if fixed else blob(raw)
    def optional(value,encode,null=0):
        return number(null if value is None else 1-null)+(b'' if value is None else encode(value))
    def detail(value):
        return b''.join(optional(value[field['name']],
            (lambda v:decimal(v,True)) if field['name']=='fixed_amount' else
            (lambda v:decimal(v,False)) if field['name']=='amount' else
            (lambda v:number((v-datetime.date(1970,1,1)).days)) if field['name']=='day' else
            (lambda v:blob(v)) if field['name']=='payload' else
            (lambda v:blob(v.encode())) if field['name']=='label' else number,
            1 if field['name'] in {'uid','fixed_amount'} else 0) for field in NESTED_DETAIL['fields'])
    def array(values,encode,mode):
        if not values:return number(0)
        blocks=[values] if mode%3==0 else [values[:1],values[1:]]
        out=b''
        for block in blocks:
            if not block:continue
            raw=b''.join(encode(v) for v in block)
            out+=(number(-len(block))+number(len(raw)) if mode%3==2 else number(len(block)))+raw
        return out+number(0)
    for start in range(0,COUNT,257):
        payload=b''
        for i in range(start,min(start+257,COUNT)):
            value=nested_row(i)
            payload+=number(value['id'])+optional(value['detail'],detail)
            payload+=optional(value['items'],lambda v:array(v,lambda x:optional(x,detail,1),i),1)
            payload+=optional(value['grid'],lambda v:array(v,lambda x:optional(x,lambda y:array(y,lambda z:optional(z,number,1),i+1)),i))
            payload+=array(value['fixed_values'],lambda v:optional(v,lambda x:decimal(x,True)),i+2)
        if codec=='deflate':
            compressor=zlib.compressobj(wbits=-15);payload=compressor.compress(payload)+compressor.flush()
        elif codec=='snappy':
            n=len(payload)-1;width=max(1,(n.bit_length()+7)//8)
            tag=bytes([n<<2]) if n<60 else bytes([(59+width)<<2])+n.to_bytes(width,'little')
            payload=unsigned(len(payload))+tag+payload+struct.pack('>I',zlib.crc32(payload))
        out+=number(min(257,COUNT-start))+blob(payload)+sync
    return out

def nested_sources():
    return [{'codec':c,'name':'nested-'+c+'.avro','count':COUNT,'schema':NESTED_SCHEMA,
             'size_bytes':len(raw),'sha256':hashlib.sha256(raw).hexdigest(),'nested':True}
            for c in CODECS for raw in [nested_source_bytes(c)]]



ENUM_SYMBOLS = ['z', 'A', 'a']
ENUM_SCHEMA = {'type':'record','name':'EnumRows','namespace':'demo','fields':[
 {'name':'id','type':'long'},
 {'name':'state','type':{'type':'enum','name':'State','symbols':ENUM_SYMBOLS,'default':'A'}},
 {'name':'optional','type':['null','State']},
 {'name':'detail','type':{'type':'record','name':'Detail','fields':[{'name':'state','type':'State'}]}},
 {'name':'items','type':{'type':'array','items':['State','null']}},
 {'name':'other','type':{'type':'enum','name':'other.State','symbols':['OFF','ON']}}
]}


def enum_row(i):
    state = ENUM_SYMBOLS[i % 3]
    optional = None if i % 7 == 0 else ENUM_SYMBOLS[(i + 1) % 3]
    return {'id':row(i)['id'], 'state':state, 'optional':optional,
            'detail':{'state':ENUM_SYMBOLS[(i + 2) % 3]},
            'items':[] if i % 5 == 0 else [state,None,optional],
            'other':['OFF','ON'][i % 2]}


def enum_public_row(i):
    value = enum_row(i)
    value['id'] = str(value['id'])
    return value


def enum_source_bytes(codec):
    # Independent OCF construction, preserving the original twelve sources.
    if codec not in CODECS:
        raise ValueError('unsupported Avro fixture codec')
    sync = hashlib.sha256(('addp-avro-enum-' + codec).encode()).digest()[:16]
    metadata = {'avro.schema':json.dumps(ENUM_SCHEMA,separators=(',',':')).encode(),'avro.codec':codec.encode()}
    result = b'Obj\x01' + number(len(metadata))
    for key,value in metadata.items():
        result += blob(key.encode()) + blob(value)
    result += number(0) + sync
    for start in range(0,COUNT,257):
        raw = bytearray()
        for i in range(start,min(start+257,COUNT)):
            value = enum_row(i)
            raw.extend(number(value['id']) + number(ENUM_SYMBOLS.index(value['state'])))
            raw.extend(number(0 if value['optional'] is None else 1))
            if value['optional'] is not None:raw.extend(number(ENUM_SYMBOLS.index(value['optional'])))
            raw.extend(number(ENUM_SYMBOLS.index(value['detail']['state'])))
            if value['items']:
                raw.extend(number(len(value['items'])))
                for item in value['items']:
                    raw.extend(number(1 if item is None else 0))
                    if item is not None:raw.extend(number(ENUM_SYMBOLS.index(item)))
            raw.extend(number(0) + number(['OFF','ON'].index(value['other'])))
        payload = bytes(raw)
        if codec == 'deflate':
            compressor = zlib.compressobj(wbits=-15)
            payload = compressor.compress(payload)+compressor.flush()
        elif codec == 'snappy':
            n = len(payload)-1
            width = max(1,(n.bit_length()+7)//8)
            tag = bytes([n<<2]) if n<60 else bytes([(59+width)<<2])+n.to_bytes(width,'little')
            payload = unsigned(len(payload))+tag+payload+struct.pack('>I',zlib.crc32(payload))
        result += number(min(257,COUNT-start)) + blob(payload) + sync
    return result


def enum_sources():
    return [{'codec':c,'name':'enum-'+c+'.avro','count':COUNT,'schema':ENUM_SCHEMA,'enum':True,
             'size_bytes':len(raw),'sha256':hashlib.sha256(raw).hexdigest()}
            for c in CODECS for raw in [enum_source_bytes(c)]]

# Map keys are content, never a declared record field list. Older source bytes above stay unchanged.
MAP_KEYS = ['', 'sensor.A', 'a/b', '中文 ', '__proto__', 'constructor']
MAP_SCHEMA = {'type':'record','name':'example.MapRows','fields':[
    {'name':'id','type':'long'},
    {'name':'metrics','type':['null',{'type':'map','values':['null','long']}]},
    {'name':'details','type':{'type':'map','values':['null',{'type':'record','name':'MapDetail','fields':[
        {'name':'uid','type':'long'}, {'name':'amount','type':{'type':'bytes','logicalType':'decimal','precision':38,'scale':6}},
        {'name':'day','type':{'type':'int','logicalType':'date'}}, {'name':'stamp','type':{'type':'long','logicalType':'timestamp-nanos'}},
        {'name':'payload','type':'bytes'}]}]}},
    {'name':'states','type':{'type':'map','values':{'type':'enum','name':'demo.MapState','symbols':['z','A','a']}}},
    {'name':'groups','type':{'type':'array','items':{'type':'map','values':{'type':'array','items':['null','demo.MapState']}}}},
    {'name':'nested','type':{'type':'map','values':{'type':'map','values':['long','null']}}},
]}

def map_row(index):
    base=row(index)
    # Always non-null logical values in this detail; other cases exercise NULL values/containers.
    return {'id':base['id'],
            'metrics':None if index%7==0 else {} if index%5==0 else {key:None if n%3==1 else base['id']+n for n,key in enumerate(MAP_KEYS)},
            'details':{'内容':{'uid':base['id'],'amount':row(index + 1 if index%19==0 else index)['amount'],'day':datetime.date(1969,12,31),
                              'stamp':TIMESTAMPS['stamp_ns'][1][index%8],'payload':bytes([0,255,index%256])},'none':None},
            'states':{'z':ENUM_SYMBOLS[index%3],'A':ENUM_SYMBOLS[(index+1)%3]},
            'groups':[{}, {'a.b':[None,ENUM_SYMBOLS[index%3]]}],
            'nested':{'outer':{'':base['id'],'nil':None},'empty':{}}}

def map_public_row(index):
    value=map_row(index); detail=value['details']['内容']
    return {'id':str(value['id']),
            'metrics':None if value['metrics'] is None else {k:None if v is None else str(v) for k,v in value['metrics'].items()},
            'details':{'内容':{'uid':str(detail['uid']),'amount':format(detail['amount'],'f'),'day':detail['day'].isoformat(),
                              'stamp':utc_text(detail['stamp'],'ns'),'payload':base64.b64encode(detail['payload']).decode()},'none':None},
            'states':value['states'],'groups':value['groups'],
            'nested':{'outer':{'':str(value['id']),'nil':None},'empty':{}}}

def map_source_bytes(codec):
    if codec not in CODECS:raise ValueError('unsupported Avro fixture codec')
    sync=hashlib.sha256(('addp-avro-map-fixture-'+codec).encode()).digest()[:16]
    metadata={'avro.schema':json.dumps(MAP_SCHEMA,separators=(',',':')).encode(),'avro.codec':codec.encode()}
    out=b'Obj\x01'+number(2)+b''.join(blob(k.encode())+blob(v) for k,v in metadata.items())+number(0)+sync
    def optional(value,encode,null=0):
        return number(null if value is None else 1-null)+(b'' if value is None else encode(value))
    def entries(values,encode,mode,keys=True):
        items=list(values.items()) if keys else list(values)
        groups=[items] if mode%3==0 else [items[:1],items[1:]]
        result=b''
        for group in groups:
            if not group:continue
            raw=b''.join(blob(k.encode())+encode(v) for k,v in group) if keys else b''.join(encode(v) for v in group)
            result+=(number(-len(group))+number(len(raw)) if mode%3==2 else number(len(group)))+raw
        return result+number(0)
    def detail(v):
        d=v['amount'].as_tuple();integer=int(''.join(map(str,d.digits)))*(-1 if d.sign else 1)
        raw=integer.to_bytes(max(1,(integer.bit_length()+8)//8),'big',signed=True)
        return number(v['uid'])+blob(raw)+number((v['day']-datetime.date(1970,1,1)).days)+number(v['stamp'])+blob(v['payload'])
    for start in range(0,COUNT,257):
        raw=b''
        for i in range(start,min(start+257,COUNT)):
            v=map_row(i)
            raw+=number(v['id'])+optional(v['metrics'],lambda x:entries(x,lambda y:optional(y,number),i))
            raw+=entries(v['details'],lambda x:optional(x,detail),i)
            raw+=entries(v['states'],lambda x:number(ENUM_SYMBOLS.index(x)),i)
            raw+=entries(v['groups'],lambda x:entries(x,lambda y:entries(y,lambda z:optional(z,lambda e:number(ENUM_SYMBOLS.index(e))),i+1,False),i),i,False)
            raw+=entries(v['nested'],lambda x:entries(x,lambda y:optional(y,number,1),i+1),i)
        payload=raw
        if codec=='deflate':
            compressor=zlib.compressobj(wbits=-15);payload=compressor.compress(raw)+compressor.flush()
        elif codec=='snappy':
            n=len(raw)-1;width=max(1,(n.bit_length()+7)//8)
            tag=bytes([n<<2]) if n<60 else bytes([(59+width)<<2])+n.to_bytes(width,'little')
            payload=unsigned(len(raw))+tag+raw+struct.pack('>I',zlib.crc32(raw))
        out+=number(min(257,COUNT-start))+blob(payload)+sync
    return out

def map_sources():
    return [{'codec':c,'name':'map-'+c+'.avro','map':True,'count':COUNT,'schema':MAP_SCHEMA,
             'size_bytes':len(raw),'sha256':hashlib.sha256(raw).hexdigest()} for c in CODECS for raw in [map_source_bytes(c)]]


def worker(request):
    from minio import Minio
    access = request['storage']
    client = Minio(access['endpoint'], access_key=access['access_key'], secret_key=access['secret_key'], secure=False)
    bucket = access['bucket']
    directory = Path(request['nfs_root']) / 'avro'
    action = request['action']
    outputs = [f'avro-output/from-{storage}-{codec}.parquet' for storage in ('minio', 'nfs') for codec in CODECS]
    marker = directory / '.addp-avro-fixture'
    if directory.is_symlink():
        raise ValueError('Avro fixture directory is a symlink')

    def remote(name):
        response = client.get_object(bucket, name)
        try:
            return response.read()
        finally:
            response.close()
            response.release_conn()

    if action == 'seed':
        assert not list(client.list_objects(bucket, prefix='avro/', recursive=True)), 'existing_inputs'
        assert not list(client.list_objects(bucket, prefix='avro-output/', recursive=True)), 'existing_outputs'
        directory.mkdir(exist_ok=False)
        with marker.open('x') as out:
            out.write('addp.avro-fixture/v4')
        for spec in sources()+nested_sources()+enum_sources()+map_sources():
            raw = (map_source_bytes if spec.get('map') else enum_source_bytes if spec.get('enum') else nested_source_bytes if spec.get('nested') else source_bytes)(spec['codec'])
            path = directory / spec['name']
            path.write_bytes(raw)
            path.chmod(0o644)
            client.put_object(bucket, 'avro/' + path.name, io.BytesIO(raw), len(raw), content_type='application/avro')
        client.put_object(bucket, 'avro-output/.keep', io.BytesIO(b''), 0)
        return {'sources': sources(), 'nested_sources':nested_sources(), 'enum_sources':enum_sources(), 'map_sources':map_sources(), 'count': COUNT}
    if action not in {'verify', 'delete'}:
        raise ValueError('unknown Avro fixture action')
    if not directory.exists() and action == 'delete':
        assert not list(client.list_objects(bucket, prefix='avro/', recursive=True)), 'unowned_inputs'
        assert not list(client.list_objects(bucket, prefix='avro-output/', recursive=True)), 'unowned_outputs'
        return {'objects_removed': True, 'files_removed': True, 'outputs_removed': True}
    assert not marker.is_symlink() and marker.read_text() == 'addp.avro-fixture/v4', 'fixture_ownership'
    assert all(not (directory / v['name']).is_symlink() for v in sources()+nested_sources()+enum_sources()+map_sources()), 'source_symlink'
    if action == 'verify':
        import pyarrow as pa
        import pyarrow.parquet as pq
        for spec in sources()+nested_sources()+enum_sources()+map_sources():
            assert (directory / spec['name']).read_bytes() == remote('avro/' + spec['name']) == (map_source_bytes if spec.get('map') else enum_source_bytes if spec.get('enum') else nested_source_bytes if spec.get('nested') else source_bytes)(spec['codec']), 'source_changed'
        assert {x.object_name for x in client.list_objects(bucket,prefix='avro-output/',recursive=True)}==set(outputs+['avro-output/.keep']), 'unexpected_nested_output'
        facts = []
        expected_rows = [row(i) for i in range(COUNT)]
        for name in outputs:
            raw = remote(name)
            table = pq.read_table(io.BytesIO(raw))
            expected_types = {'id': pa.int64(), 'small': pa.int32(), 'flag': pa.bool_(), 'label': pa.string(),
                              'ratio': pa.float32(), 'value': pa.float64(), 'payload': pa.binary(), 'day': pa.date32(),
                              'amount': pa.decimal128(38, 6), 'fixed_amount': pa.decimal128(38, 6)}
            expected_types.update({name:pa.timestamp(unit, 'UTC') for name,(unit,_,_) in TIMESTAMPS.items()})
            # The existing parquet-go Group writer emits fields by name. Bind
            # every type and value by its name rather than the OCF ordinal.
            assert table.column_names == sorted(expected_types) and table.num_rows == COUNT, 'output_fields'
            assert all(table.schema.field(name).type == kind and table.schema.field(name).nullable ==
                       (name in {'flag', 'label', 'payload', 'day', 'amount', 'fixed_amount'} | set(TIMESTAMPS)) for name, kind in expected_types.items()), 'output_types'
            plain = [name for name in expected_types if name not in TIMESTAMPS]
            assert table.select(plain).to_pylist() == [{name:value[name] for name in plain} for value in expected_rows], 'output_values'
            physical_schema = pq.ParquetFile(io.BytesIO(raw)).schema
            for field_name, (unit, _, _) in TIMESTAMPS.items():
                assert table[field_name].cast(pa.int64()).to_pylist() == [value[field_name] for value in expected_rows], 'output_ticks'
                column = physical_schema.column(table.column_names.index(field_name))
                logical = json.loads(column.logical_type.to_json())
                assert column.physical_type == 'INT64' and logical.get('Type') == 'Timestamp' and logical.get('isAdjustedToUTC') is True, 'output_temporal_annotation'
                assert logical.get('timeUnit') == {'ms':'milliseconds','us':'microseconds','ns':'nanoseconds'}[unit], 'output_temporal_annotation'
            facts.append({'object': name, 'size_bytes': len(raw), 'sha256': hashlib.sha256(raw).hexdigest(),
                          'all_values_and_nulls_verified': True, 'native_timestamp_ticks_verified': True,
                          'timestamp_units': {name:unit for name,(unit,_,_) in TIMESTAMPS.items()}, 'content_base64': base64.b64encode(raw).decode()})
        return {'sources_unchanged': True, 'nested_outputs_absent':True, 'nested_sources_verified':6, 'enum_sources_verified':6, 'enum_outputs_absent':True, 'map_sources_verified':6, 'map_outputs_absent':True, 'outputs': facts}
    if action == 'delete':
        for name in outputs + ['avro-output/.keep'] + ['avro/' + v['name'] for v in sources()+nested_sources()+enum_sources()+map_sources()]:
            client.remove_object(bucket, name)
        for spec in sources()+nested_sources()+enum_sources()+map_sources():
            (directory / spec['name']).unlink(missing_ok=True)
        marker.unlink()
        if directory.exists():
            directory.rmdir()
        assert not list(client.list_objects(bucket, prefix='avro/', recursive=True)), 'input_cleanup'
        assert not list(client.list_objects(bucket, prefix='avro-output/', recursive=True)), 'output_cleanup'
        return {'objects_removed': True, 'files_removed': True, 'outputs_removed': True}
    raise ValueError('unknown Avro fixture action')


def physical(action, repository, environment):
    import importlib
    return importlib.import_module('scripts.test.online-table-fixture-support').physical_worker(
        action, repository, environment, profile='avro', script='business/scripts/online-avro-fixture.py')


def worker_error(error):
    codes = {'existing_inputs', 'existing_outputs', 'unowned_inputs', 'unowned_outputs',
             'fixture_ownership', 'source_symlink', 'source_changed', 'output_fields',
             'unexpected_nested_output', 'output_types', 'output_values', 'output_ticks', 'output_temporal_annotation', 'input_cleanup', 'output_cleanup'}
    return str(error) if isinstance(error, AssertionError) and str(error) in codes else type(error).__name__


if __name__ == '__main__':
    try:
        print(json.dumps(worker(json.load(sys.stdin))))
    except Exception as error:
        print(json.dumps({'error_code': worker_error(error)}))
        raise SystemExit(1)

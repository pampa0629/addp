"""Feather V2 / Arrow IPC file: scalar read-only tables in the existing Runtime."""
from __future__ import annotations
import base64
from datetime import date, timedelta
import json
import math
import struct
import sys
from functools import wraps
from addp_common.workflow_access import require_source_plan, _object_store_client
from . import _native_process
from ._table_source import TableSourceFile
from .base import (OperatorMetadata, OperatorParam, OperatorCategory, OperatorType,
                   OutputPort, register_operator)

PROTOCOL = 'feather.table-batch/v1'
MAX_FILE_BYTES = 256 * 1024**2
MAX_READ_BYTES = 128 * 1024**2
MAX_SINGLE_READ = 4 * 1024**2
MAX_FOOTER_BYTES = 1024**2
MAX_COLUMNS = 256
MAX_BATCHES = 4096
MAX_DECODED_BATCH_BYTES = 64 * 1024**2
MAX_DECODED_BATCH_ROWS = 1_000_000
MAX_BATCH_ROWS = 2000
MAX_VALUE_BYTES = 1024**2
MAX_RESPONSE_BYTES = 16 * 1024**2

class SourceChanged(ValueError):
    pass

class ReadRejected(ValueError):
    pass

class ReadFailed(RuntimeError):
    pass


def _open(access_plan, source_version):
    plan = require_source_plan({'access_plan': access_plan})
    if plan['source'].get('kind') != 'file' or plan['source'].get('format') != 'feather':
        raise ValueError('Feather requires one file source')
    if not isinstance(source_version, str):
        raise ValueError('Feather source version must be text')
    return TableSourceFile(plan, source_version, label='Feather', changed_error=SourceChanged,
                           client_factory=_object_store_client, max_file_bytes=MAX_FILE_BYTES,
                           max_read_bytes=MAX_READ_BYTES, max_single_read=MAX_SINGLE_READ)


def _field(field, ordinal):
    import pyarrow as pa
    typ = field.type
    kinds = {pa.bool_(): 'bool', pa.int8(): 'int', pa.int16(): 'int', pa.int32(): 'int',
             pa.int64(): 'bigint', pa.float32(): 'float', pa.float64(): 'double',
             pa.string(): 'string', pa.binary(): 'bytes', pa.date32(): 'date', pa.date64(): 'date'}
    kind = kinds.get(typ)
    shape = {}
    if pa.types.is_decimal128(typ) or pa.types.is_decimal256(typ):
        if not 0 <= typ.scale <= typ.precision:
            raise ValueError('Feather decimal is outside the scalar profile')
        kind = 'decimal'
        shape = {'precision': typ.precision, 'scale': typ.scale}
    if pa.types.is_time(typ) or pa.types.is_timestamp(typ):
        kind = 'time' if pa.types.is_time(typ) else 'timestamp'
        temporal = {'unit': typ.unit}
        if kind == 'timestamp' and typ.tz:
            temporal['timezone'] = typ.tz
        shape = {'temporal': temporal}
    if (kind is None or not field.name or len(field.name.encode()) > 1024
            or any(key.startswith(b'ARROW:extension:') for key in (field.metadata or {}))):
        raise ValueError('Feather field is outside the scalar profile')
    return {'name': field.name, 'type': kind, 'native_type': str(typ),
            'nullable': field.nullable, 'ordinal_position': ordinal, **shape}


def _reader(source):
    import pyarrow.ipc as ipc
    if source.read(6) != b'ARROW1':
        raise ValueError('Feather V2 requires an Arrow IPC file')
    source.seek(-10, 2)
    tail = source.read(10)
    size = struct.unpack('<I', tail[:4])[0]
    if tail[4:] != b'ARROW1' or not 0 < size <= min(MAX_FOOTER_BYTES, source.size - 18):
        raise ValueError('Feather footer is invalid or exceeds budget')
    source.seek(0)
    reader = ipc.open_file(source)
    if not 1 <= len(reader.schema) <= MAX_COLUMNS or reader.num_record_batches > MAX_BATCHES:
        raise ValueError('Feather schema or batches exceed budget')
    fields = [_field(field, i + 1) for i, field in enumerate(reader.schema)]
    if len({field['name'] for field in fields}) != len(fields):
        raise ValueError('Feather repeats field names')
    return reader, fields


def _batch(reader, index):
    batch = reader.get_batch(index)
    if batch.nbytes > MAX_DECODED_BATCH_BYTES or batch.num_rows > MAX_DECODED_BATCH_ROWS:
        raise ValueError('Feather decoded batch exceeds budget')
    batch.validate(full=True)
    # A non-nullable declaration must agree with all buffers, including unselected fields.
    if any(not field.nullable and column.null_count for field, column in zip(batch.schema, batch.columns)):
        raise ValueError('Feather nulls contradict the schema')
    return batch


def inspect(protocol, access_plan, source_version=''):
    _protocol(protocol)
    with _open(access_plan, source_version) as source:
        reader, fields = _reader(source)
        total = 0
        for index in range(reader.num_record_batches):
            total += _batch(reader, index).num_rows
        if total > 2**53 - 1:
            raise ValueError('Feather row count exceeds exact metadata range')
        source.check()
        return {'protocol': PROTOCOL, 'describe': {
            'table': {'fields': fields, 'row_count': total, 'size_bytes': source.size},
            'format_info': {'version': 2,
                            'record_batches': reader.num_record_batches,
                            'source_version': source.version, 'validation': 'batch_structure'}}}


def _value(scalar, field):
    if not scalar.is_valid:
        return None
    kind = field['type']
    if kind == 'date':
        days = scalar.value
        if field['native_type'] == 'date64[ms]':
            days //= 86_400_000  # Arrow full batch validation checks whole-day alignment.
        try:
            return (date(1970, 1, 1) + timedelta(days=days)).isoformat()
        except OverflowError as error:
            raise ValueError('Feather date is outside the supported calendar') from error
    if kind in {'time', 'timestamp'}:
        return str(scalar.value)
    value = scalar.as_py()
    if kind == 'decimal':
        return format(value, 'f')
    if kind == 'bigint':
        return str(value)
    if kind == 'bytes':
        if len(value) > MAX_VALUE_BYTES:
            raise ValueError('Feather value exceeds budget')
        return base64.b64encode(value).decode('ascii')
    if kind == 'string' and len(value.encode()) > MAX_VALUE_BYTES:
        raise ValueError('Feather value exceeds budget')
    if kind in {'float', 'double'} and not math.isfinite(value):
        raise ValueError('Feather non-finite values cannot be delivered as JSON')
    return value


def read_batch(protocol, access_plan, source_version, offset, limit, columns):
    _protocol(protocol)
    if (not isinstance(source_version, str) or not source_version or type(offset) is not int
            or offset < 0 or type(limit) is not int or not 1 <= limit <= MAX_BATCH_ROWS):
        raise ValueError('Feather requires a version-bound bounded row window')
    with _open(access_plan, source_version) as source:
        reader, fields = _reader(source)
        by_name = {field['name']: (i, field) for i, field in enumerate(fields)}
        if (not isinstance(columns, list) or not columns or any(not isinstance(c, str) for c in columns)
                or len(set(columns)) != len(columns) or any(c not in by_name for c in columns)):
            raise ValueError('Feather columns do not match the schema')
        selected = [by_name[c] for c in columns]
        rows, encoded = [], 0
        for index in range(reader.num_record_batches):
            batch = _batch(reader, index)
            if offset >= batch.num_rows:
                offset -= batch.num_rows
                continue
            take = min(limit - len(rows), batch.num_rows - offset)
            for position in range(offset, offset + take):
                row = {field['name']: _value(batch.column(i)[position], field) for i, field in selected}
                encoded += len(json.dumps(row, ensure_ascii=False, allow_nan=False).encode())
                if encoded > MAX_RESPONSE_BYTES:
                    raise ValueError('Feather response exceeds budget')
                rows.append(row)
            offset = 0
            if len(rows) == limit:
                break
        source.check()
        return {'protocol': PROTOCOL, 'source_version': source.version, 'rows': rows}


def _protocol(protocol):
    if protocol != PROTOCOL:
        raise ValueError('Unsupported Feather protocol')


def _native_main():
    import resource
    resource.setrlimit(resource.RLIMIT_CPU, (30, 30))
    if sys.platform.startswith('linux'):
        resource.setrlimit(resource.RLIMIT_AS, (4 * 1024**3, 4 * 1024**3))
    request = json.load(sys.stdin)
    try:
        operation = {'inspect': inspect, 'read_batch': read_batch}[request['operation']]
        response = {'result': operation(**request['params'])}
    except SourceChanged:
        response = {'error': 'source_changed'}
    except (ValueError, TypeError, KeyError):
        response = {'error': 'read_rejected'}
    except Exception:
        response = {'error': 'read_failed'}
    print(json.dumps(response, ensure_ascii=False, allow_nan=False))


def _register(name, function, params):
    @wraps(function)
    def invoke(**values):
        try:
            completed = _native_process.run([sys.executable, '-c',
                'from operators.feather_table import _native_main; _native_main()'],
                input=json.dumps({'operation': function.__name__, 'params': values}, allow_nan=False),
                capture_output=True, text=True, timeout=60)
            if completed.returncode != 0:
                raise ReadFailed('Feather native read failed')
            response = json.loads(completed.stdout)
        except ReadFailed:
            raise
        except Exception:
            raise ReadFailed('Feather native read failed') from None
        if response.get('error') == 'source_changed':
            raise SourceChanged('Feather source changed')
        if response.get('error') == 'read_rejected':
            raise ReadRejected('Feather input rejected')
        if response.get('error') or 'result' not in response:
            raise ReadFailed('Feather native read failed')
        return response['result']
    metadata = OperatorMetadata(name=name, type=OperatorType.GENERAL, category=OperatorCategory.COMMON_IO,
        description=name, brief_description='Feather V2 scalar table read', overview='受控单文件 Feather V2 标量表格只读',
        execution_modes=['direct'], effects=['read'], params=params,
        output_ports=[OutputPort(name='default', type='object', description=PROTOCOL)],
        use_cases=['元数据', '分页预览', '连续只读批次'], notes=['不支持V1、stream、嵌套及写出'],
        workflow_example={}, attributes={'protocol': PROTOCOL, 'runtime_session': 'stateless'})
    return register_operator(metadata, invoke)


def _param(name, kind, required=True, default=None):
    return OperatorParam(name=name, type='param', data_type=kind, description=name, required=required, default=default)

_BASE = [_param('protocol', 'string'), _param('access_plan', 'object')]
OPERATORS = dict([
    _register('feather.inspect', inspect, _BASE + [_param('source_version', 'string', False, '')]),
    _register('feather.read_batch', read_batch, _BASE + [_param('source_version', 'string'),
        _param('offset', 'int'), _param('limit', 'int'), _param('columns', 'array')]),
])

"""Self-contained GRIB2 fields: structural/bitmap proof, GDAL-only values."""
from __future__ import annotations
from contextlib import contextmanager
import hashlib
import json
import math
import os
from pathlib import Path
import resource
import sys
import tempfile
import uuid
import numpy as np
from osgeo import gdal
from addp_common.workflow_access import require_source_plan, stage_source_file
from . import _native_process
from .base import OperatorCategory, OperatorMetadata, OperatorParam, OperatorType, register_operator

MAX_BYTES = 128 << 20
MAX_MESSAGES = 128
MAX_POINTS = 1 << 24
MAX_TOTAL_POINTS = 1 << 26
PROFILE = 'grib2_regular_fields/v1'


class SliceSelectionError(ValueError):
    pass


class SourceChanged(ValueError):
    pass


def _integer(value, low, high):
    if type(value) is not int or not low <= value <= high:
        raise SliceSelectionError('GRIB slice outside supported bounds')
    return value


def _fields(raw):
    """Require a complete closed single-field section sequence per message.

    Structural interpretation proves bounds and bitmap only. Packed numerical
    values are never decoded here; GDAL remains the only value reader.
    """
    fields = []
    offset = 0
    total = 0
    while offset < len(raw):
        if len(fields) >= MAX_MESSAGES or len(raw)-offset < 20 or raw[offset:offset+4] != b'GRIB' or raw[offset+7] != 2:
            raise ValueError('unsupported GRIB2 message envelope')
        size = int.from_bytes(raw[offset+8:offset+16], 'big')
        end = offset + size
        if size < 20 or end > len(raw) or raw[end-4:end] != b'7777' or raw[offset+4:offset+6] != b'\0\0':
            raise ValueError('incomplete GRIB2 message')
        position = offset + 16
        sections = {}
        for number in (1, 3, 4, 5, 6, 7):
            if end-4-position < 5:
                raise ValueError('incomplete GRIB2 sections')
            length = int.from_bytes(raw[position:position+4], 'big')
            if length < 5 or position+length > end-4 or raw[position+4] != number:
                raise ValueError('unsupported GRIB2 section sequence or multi-field message')
            sections[number] = memoryview(raw)[position:position+length]
            position += length
        if position != end-4 or len(sections[1]) != 21:
            raise ValueError('unexplained GRIB2 message bytes')
        grid = sections[3]
        if len(grid) != 72 or grid[5] != 0 or grid[10] != 0 or grid[11] != 0 or int.from_bytes(grid[12:14], 'big') != 0:
            raise ValueError('unsupported GRIB grid template')
        # Optional lists, staggered/alternating/column-major scans are not
        # covered by the first bitmap-to-grid mapping proof.
        width, height = int.from_bytes(grid[30:34], 'big'), int.from_bytes(grid[34:38], 'big')
        count = width * height
        scan = grid[71]
        if width < 2 or height < 2 or count > MAX_POINTS or int.from_bytes(grid[6:10], 'big') != count or scan not in (0, 64):
            raise ValueError('unsupported GRIB grid dimensions or scanning mode')
        total += count
        if total > MAX_TOTAL_POINTS:
            raise ValueError('GRIB total grid budget exceeded')
        product = sections[4]
        if len(product) < 9 or int.from_bytes(product[5:7], 'big') != 0:
            raise ValueError('unsupported GRIB product coordinates')
        pdt = int.from_bytes(product[7:9], 'big')
        if (pdt == 0 and len(product) != 34) or (pdt == 8 and (len(product) < 58 or product[41] < 1 or len(product) != 46 + 12*product[41])) or pdt not in (0, 8):
            raise ValueError('unsupported GRIB product template')
        bitmap = sections[6]
        if len(bitmap) < 6 or bitmap[5] not in (0, 255):
            raise ValueError('unsupported GRIB bitmap reference')
        if bitmap[5] == 255:
            if len(bitmap) != 6:
                raise ValueError('unexpected absent-bitmap bytes')
            valid_count = count
        else:
            if len(bitmap) != 6 + (count+7)//8:
                raise ValueError('GRIB bitmap length mismatch')
            bits = np.unpackbits(np.frombuffer(bitmap[6:], dtype='uint8'))
            if np.any(bits[count:]):
                raise ValueError('GRIB bitmap padding is nonzero')
            valid_count = int(bits[:count].sum())
        representation = sections[5]
        if len(representation) < 11 or int.from_bytes(representation[5:9], 'big') != valid_count:
            raise ValueError('GRIB packed value count differs from bitmap')
        drt = int.from_bytes(representation[9:11], 'big')
        if drt == 0:
            if len(representation) != 21 or representation[19] > 32 or representation[20] not in (0, 1):
                raise ValueError('unsupported GRIB simple packing')
            data_bytes = (valid_count*representation[19]+7)//8
        elif drt == 4:
            if len(representation) != 12 or representation[11] not in (1, 2):
                raise ValueError('unsupported GRIB IEEE precision')
            data_bytes = valid_count * (4 if representation[11] == 1 else 8)
        else:
            raise ValueError('unsupported GRIB value encoding')
        if len(sections[7]) != 5 + data_bytes:
            raise ValueError('GRIB packed payload length mismatch')
        fields.append(dict(name=f'message-{len(fields)+1}', offset=offset, size=size, width=width, height=height,
            scan=scan, bitmap=bitmap[6:].tobytes() if bitmap[5] == 0 else None,
            product_template=pdt, data_template=drt, discipline=raw[offset+6]))
        offset = end
    if not fields:
        raise ValueError('empty GRIB2 file')
    return fields


@contextmanager
def _source(access_plan, source_version=None):
    plan = require_source_plan({'access_plan': access_plan})
    if plan['source'].get('kind') != 'file' or plan['source'].get('format') != 'grib' or plan['source'].get('related_files'):
        raise ValueError('GRIB requires one exact self-contained file')
    with tempfile.TemporaryDirectory(prefix='addp-grib-') as work:
        path = stage_source_file(plan, Path(work), max_bytes=MAX_BYTES)
        raw = path.read_bytes()
        if len(raw) > MAX_BYTES:
            raise ValueError('GRIB source byte budget exceeded')
        version = 'sha256:' + hashlib.sha256(raw).hexdigest()
        if source_version is not None and version != source_version:
            raise SourceChanged('GRIB source changed; refresh metadata')
        yield raw, _fields(raw), version


@contextmanager
def _dataset(raw, field):
    name = '/vsimem/addp-grib-' + uuid.uuid4().hex + '.grb2'
    dataset = None
    warnings = []
    try:
        gdal.FileFromMemBuffer(name, raw[field['offset']:field['offset']+field['size']])
        with gdal.config_options({'GRIB_NORMALIZE_UNITS':'NO', 'GRIB_ADJUST_LONGITUDE_RANGE':'NO', 'GDAL_PAM_ENABLED':'NO', 'GDAL_DISABLE_READDIR_ON_OPEN':'EMPTY_DIR'}):
            gdal.PushErrorHandler(lambda kind, code, message: warnings.append(message) if kind >= gdal.CE_Warning else None)
            try:
                dataset = gdal.OpenEx(name, gdal.OF_RASTER | gdal.OF_READONLY, allowed_drivers=['GRIB'], open_options=['USE_IDX=NO'])
                if dataset is None or dataset.RasterCount != 1 or (dataset.RasterXSize, dataset.RasterYSize) != (field['width'], field['height']):
                    raise ValueError('GRIB message decoding differs from declared field')
                if dataset.GetFileList() != [name]:
                    raise ValueError('GRIB decoder opened undeclared resources')
                yield dataset
                if warnings:
                    raise ValueError('GRIB decoding reported incomplete or unsupported content')
            finally:
                dataset = None
                gdal.PopErrorHandler()
    finally:
        dataset = None
        gdal.Unlink(name)


def _description(dataset, field):
    band = dataset.GetRasterBand(1)
    metadata = band.GetMetadata() or {}
    if len(metadata) > 64 or len(json.dumps(metadata).encode()) > 16384:
        raise ValueError('GRIB field metadata budget exceeded')
    if gdal.GetDataTypeName(band.DataType) != 'Float64':
        raise ValueError('unexpected GRIB decoded value type')
    transform = dataset.GetGeoTransform(can_return_null=True)
    if not transform or not all(math.isfinite(v) for v in transform) or transform[1] <= 0 or transform[5] >= 0 or transform[2] != 0 or transform[4] != 0:
        raise ValueError('GRIB grid is not a supported north-up regular grid')
    for key in ('GRIB_REF_TIME', 'GRIB_VALID_TIME', 'GRIB_FORECAST_SECONDS'):
        if key not in metadata or not metadata[key].lstrip('-').isdigit():
            raise ValueError('incomplete GRIB time facts')
    return dict(name=field['name'], type='Float64', dimensions=[dict(name='y',size=str(field['height']),axis_type='HORIZONTAL_Y',direction='NORTH_TO_SOUTH'),dict(name='x',size=str(field['width']),axis_type='HORIZONTAL_X',direction='WEST_TO_EAST')],
        attributes={key:dict(type='String',values=[value]) for key,value in metadata.items()}, attributes_coverage='selected',slice_supported=True,
        value_mode='decoded_native_units',grid=dict(transform=list(transform),crs_wkt=dataset.GetProjectionRef(),longitude_policy='unchanged'),
        message=dict(number=int(field['name'].split('-')[1]),discipline=field['discipline'],product_template=field['product_template'],data_template=field['data_template']))


def inspect(access_plan):
    with _source(access_plan) as (raw, fields, version):
        children=[]
        for field in fields:
            with _dataset(raw,field) as dataset:
                description=_description(dataset,field)
            children.append(dict(name=field['name'],child_kind='array',data_type='media',format='grib',native=description))
        result=dict(schema_version='gdal.grib.inspect/v1',format='grib',container=dict(child_count=len(children),resource_count=1,children=children),
            format_info=dict(driver='GRIB',encoding='GRIB2',profile=PROFILE,scan_complete=True,source_version=version,value_mode='decoded_native_units'))
        if len(json.dumps(result).encode()) > 1 << 20:
            raise ValueError('GRIB catalog response budget exceeded')
        return result


def read_slice(access_plan, source_version, selection):
    if not isinstance(source_version,str) or len(source_version)!=71 or not source_version.startswith('sha256:') or any(c not in '0123456789abcdef' for c in source_version[7:]):
        raise SliceSelectionError('GRIB source version required')
    if not isinstance(selection,dict) or set(selection)!= {'variable','indices','x','y','width','height'} or selection['indices']!=[]:
        raise SliceSelectionError('GRIB field requires an explicit two-dimensional window')
    name=selection['variable']
    if not isinstance(name,str) or len(name)>32:
        raise SliceSelectionError('invalid GRIB message selector')
    x,y=(_integer(selection[k],0,MAX_POINTS) for k in ('x','y'))
    width,height=(_integer(selection[k],1,128) for k in ('width','height'))
    if width*height>4096:
        raise SliceSelectionError('GRIB slice cell budget exceeded')
    with _source(access_plan,source_version) as (raw,fields,version):
        field=next((f for f in fields if f['name']==name),None)
        if field is None or x+width>field['width'] or y+height>field['height']:
            raise SliceSelectionError('GRIB slice outside selected field')
        with _dataset(raw,field) as dataset:
            description=_description(dataset,field)
            values=dataset.GetRasterBand(1).ReadAsArray(x,y,width,height)
            if values is None or values.shape!=(height,width):
                raise ValueError('GRIB slice incomplete')
        if field['bitmap'] is None:
            valid=np.ones((height,width),dtype=bool)
        else:
            mask=np.unpackbits(np.frombuffer(field['bitmap'],dtype='uint8'))[:field['width']*field['height']].reshape(field['height'],field['width'])
            if field['scan']==64:mask=mask[::-1]
            valid=mask[y:y+height,x:x+width].astype(bool)
        if not np.all(np.isfinite(values[valid])):
            raise ValueError('GRIB valid values must be finite')
        # Missing points have no source value; never expose the driver's 9999
        # replacement as a measurement. The independent bitmap is authoritative.
        text=[[repr(float(value)) if valid[row,col] else 'NaN' for col,value in enumerate(line)] for row,line in enumerate(values)]
        return dict(schema_version='gdal.grib.slice/v1',format='grib',source_version=version,selection=selection,variable=description,
            values=text,valid=valid.tolist(),value_mode='decoded_native_units',spatial=None)


def _native_main():
    resource.setrlimit(resource.RLIMIT_CPU,(30,30))
    if sys.platform.startswith('linux'):resource.setrlimit(resource.RLIMIT_AS,(4*1024**3,4*1024**3))
    try:
        request=json.load(sys.stdin)
        response={'result':{'inspect':inspect,'read_slice':read_slice}[request['operation']](**request['params'])}
    except SliceSelectionError:response={'error_code':'GRIB_SLICE_INVALID'}
    except SourceChanged:response={'error_code':'GRIB_SOURCE_CHANGED'}
    except Exception:response={'error':'GRIB input is outside the supported complete single-field profile'}
    print(json.dumps(response,allow_nan=False))


def _register(name,function,params):
    metadata=OperatorMetadata(name=name,type=OperatorType.GENERAL,category=OperatorCategory.COMMON_IO,
        description=name,brief_description='GRIB2 bounded self-contained fields',execution_modes=['direct'],effects=['read'],
        params=[OperatorParam(name='access_plan',type='param',data_type='object',description='Exact single-file source plan',required=True),*params],
        overview='GRIB2单文件只读',use_cases=['气象场和元信息查看','有界数值切片'],notes=['Original units and longitude; no external idx','No map or export'],workflow_example={})
    def invoke(**kwargs):
        completed=_native_process.run([sys.executable,'-c','from operators.grib import _native_main; _native_main()'],input=json.dumps(dict(operation=function.__name__,params=kwargs),allow_nan=False),capture_output=True,text=True,timeout=60)
        if completed.returncode!=0:raise RuntimeError('GRIB native process failed')
        response=json.loads(completed.stdout)
        if response.get('error_code') in ('GRIB_SLICE_INVALID','GRIB_SOURCE_CHANGED'):return dict(schema_version='gdal.grib.slice/v1',**response)
        if 'error' in response:raise ValueError(response['error'])
        return response['result']
    return register_operator(metadata,invoke)


OPERATORS=dict([
    _register('grib.inspect',inspect,[]),
    _register('grib.read_slice',read_slice,[OperatorParam(name='source_version',type='param',data_type='string',description='Scanned full content SHA256',required=True),OperatorParam(name='selection',type='param',data_type='object',description='Message and bounded XY window',required=True)])
])

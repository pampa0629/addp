"""ENVI pair preflight. GDAL remains the sole pixel decoder."""
from __future__ import annotations

import hashlib
import math
from pathlib import Path
import re

PROFILE = 'envi-standard-pair/v1'
MAX_HEADER_BYTES = 1 << 20
MAX_RASTER_BYTES = 512 << 20
_REQUIRED = {'samples', 'lines', 'bands', 'header offset', 'file type', 'data type', 'interleave', 'byte order'}
_BLOCKS = {'description', 'map info', 'coordinate system string', 'band names', 'wavelength', 'fwhm', 'bbl', 'default bands'}
_OPTIONAL = _BLOCKS | {'data ignore value', 'sensor type', 'acquisition time', 'wavelength units'}
_TYPES = {1: ('Byte', 1), 2: ('Int16', 2), 3: ('Int32', 4), 4: ('Float32', 4), 5: ('Float64', 8), 12: ('UInt16', 2), 13: ('UInt32', 4)}
_NUMBER = re.compile(r'[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?\Z')
_MESSAGES = {
    'zh-cn': 'ENVI 数据文件或头文件不符合当前支持的双文件格式范围',
    'en': 'ENVI data or header is outside the supported exact-pair profile',
}


class ENVIProfileError(ValueError):
    """Stable diagnostic reason is separate from the localized public message."""
    def __init__(self, reason, language):
        super().__init__(_MESSAGES.get(language, _MESSAGES['zh-cn']))
        self.reason = reason


def _reject(reason, language):
    raise ENVIProfileError(reason, language)


def _number(text, language, *, nan=False):
    if nan and text.lower() == 'nan':
        return math.nan
    if not _NUMBER.fullmatch(text):
        _reject('number', language)
    value = float(text)
    if not math.isfinite(value) or (value == 0 and text.lower().split('e', 1)[0].strip('0.+-')):
        _reject('number', language)
    return value


def parse_header(raw, language='zh-cn'):
    if not raw or len(raw) > MAX_HEADER_BYTES or b'\x00' in raw:
        _reject('header_budget', language)
    try:
        lines = raw.decode('utf-8', errors='strict').replace('\r\n', '\n').split('\n')
    except UnicodeError:
        _reject('header_encoding', language)
    if lines[0] != 'ENVI':
        _reject('header_signature', language)
    fields, index = {}, 1
    while index < len(lines):
        line = lines[index].strip()
        index += 1
        if not line or line.startswith(';'):
            continue
        key, equal, value = line.partition('=')
        key = key.strip().lower().replace('_', ' ')
        if not equal or key != ' '.join(key.split()) or key not in _REQUIRED | _OPTIONAL:
            _reject('header_field', language)
        if key in fields:
            _reject('duplicate_field', language)
        if len(fields) >= 64:
            _reject('header_budget', language)
        value = value.strip()
        if value.startswith('{'):
            if key not in _BLOCKS:
                _reject('scalar_block', language)
            chunks = [value]
            while '}' not in chunks[-1] and index < len(lines):
                chunks.append(lines[index])
                index += 1
            value = '\n'.join(chunks).strip()
            if not value.endswith('}') or value.count('{') != 1 or value.count('}') != 1:
                _reject('header_block', language)
            value = value[1:-1].strip()
        elif '{' in value or '}' in value or (key in _BLOCKS and key != 'description'):
            _reject('header_block', language)
        if not value:
            _reject('empty_field', language)
        fields[key] = value
    if not _REQUIRED <= fields.keys() or fields['file type'] != 'ENVI Standard' or fields['interleave'] not in ('bsq', 'bil', 'bip'):
        _reject('header_profile', language)
    numbers = {}
    for key in ('samples', 'lines', 'bands', 'header offset', 'data type', 'byte order'):
        if not re.fullmatch('[0-9]+', fields[key]) or len(fields[key]) > 19:
            _reject('integer', language)
        numbers[key] = int(fields[key])
    if any(not 0 < numbers[key] <= 2147483647 for key in ('samples', 'lines', 'bands')) or numbers['byte order'] > 1 or numbers['data type'] not in _TYPES:
        _reject('storage_type', language)
    file_bytes = math.prod(numbers[key] for key in ('samples', 'lines', 'bands')) * _TYPES[numbers['data type']][1] + numbers['header offset']
    if file_bytes > MAX_RASTER_BYTES:
        _reject('storage_size', language)
    for key in ('band names', 'wavelength', 'fwhm', 'bbl'):
        if key not in fields:
            continue
        parts = [part.strip() for part in fields[key].split(',')]
        if len(parts) != numbers['bands'] or any(not part for part in parts):
            _reject('band_metadata', language)
        for part in parts:
            if key != 'band names':
                _number(part, language)
            if key == 'bbl' and part not in ('0', '1'):
                _reject('band_metadata', language)
    if 'default bands' in fields:
        parts = [part.strip() for part in fields['default bands'].split(',')]
        if len(parts) not in (1, 3) or any(not re.fullmatch('[+-]?[0-9]+', p) or len(p) > 20 or not 0 < int(p) <= numbers['bands'] for p in parts):
            _reject('default_bands', language)
    if 'data ignore value' in fields:
        missing = _number(fields['data ignore value'], language, nan=True)
        bounds = {1: (0, 255), 2: (-32768, 32767), 3: (-2147483648, 2147483647), 12: (0, 65535), 13: (0, 4294967295)}.get(numbers['data type'])
        if bounds is not None and (not math.isfinite(missing) or not missing.is_integer() or not bounds[0] <= missing <= bounds[1]):
            _reject('nodata_type', language)
        if numbers['data type'] == 4 and math.isfinite(missing) and (abs(missing) > 3.4028234663852886e38 or 0 < abs(missing) <= 7.006492321624085e-46):
            _reject('nodata_type', language)
    if 'coordinate system string' in fields:
        value = fields['coordinate system string'].upper()
        if not value.startswith(('GEOGCS[', 'PROJCS[')) or any(token in value for token in ('EXTENSION', 'BOUNDCRS', 'PROJ4', 'PARAMETERFILE', 'VERT_CS', 'COMPD_CS')):
            _reject('crs_resource', language)
    return {**numbers, 'fields': fields, 'file_bytes': file_bytes, 'header_sha256': hashlib.sha256(raw).hexdigest()}


def inspect_envi(path, header, language='zh-cn'):
    path, header = Path(path), Path(header)
    if (path.is_symlink() or header.is_symlink() or path.parent != header.parent or path == header
            or header.suffix.lower() != '.hdr' or not 0 < path.stat().st_size <= MAX_RASTER_BYTES):
        _reject('file_pair', language)
    with header.open('rb') as stream:
        raw = stream.read(MAX_HEADER_BYTES + 1)
    facts = parse_header(raw, language)
    if facts['file_bytes'] != path.stat().st_size:
        _reject('data_size', language)
    # Content version is independent of the stable authorization composition.
    digest = hashlib.sha256()
    with path.open('rb') as stream:
        for chunk in iter(lambda: stream.read(1 << 20), b''):
            digest.update(chunk)
    return {'profile': PROFILE, 'composition_complete': True, 'header_name': header.name,
            'raster_sha256': digest.hexdigest(),
            'header_sha256': facts['header_sha256'], 'header_size': len(raw), 'raster_size': facts['file_bytes'],
            'samples': facts['samples'], 'lines': facts['lines'], 'band_count': facts['bands'],
            'data_type': facts['data type'], 'native_data_type': _TYPES[facts['data type']][0],
            'byte_order': facts['byte order'], 'header_offset': facts['header offset'],
            'interleave': facts['fields']['interleave'], 'fields': facts['fields']}


def validate_dataset(dataset, facts, language='zh-cn'):
    from osgeo import gdal, osr
    if (dataset.RasterXSize != facts['samples'] or dataset.RasterYSize != facts['lines'] or dataset.RasterCount != facts['band_count']
            or any(gdal.GetDataTypeName(dataset.GetRasterBand(i).DataType) != facts['native_data_type'] for i in range(1, dataset.RasterCount + 1))):
        _reject('native_grid', language)
    fields = facts['fields']
    if 'map info' in fields and dataset.GetGeoTransform(can_return_null=True) is None:
        _reject('native_grid', language)
    if 'coordinate system string' in fields:
        try:
            declared = osr.SpatialReference()
            declared.ImportFromWkt(fields['coordinate system string'])
            actual = dataset.GetSpatialRef()
            if not actual or not declared.IsSame(actual, ['CRITERION=EQUIVALENT_EXCEPT_AXIS_ORDER_GEOGCRS', 'IGNORE_DATA_AXIS_TO_SRS_AXIS_MAPPING=YES']):
                _reject('native_crs', language)
        except RuntimeError:
            _reject('native_crs', language)

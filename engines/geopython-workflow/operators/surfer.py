"""Content-selected framing for the supported Surfer grid profiles.

GDAL remains the sole pixel/grid decoder. No related paths are opened here.
"""
from pathlib import Path
import math
import struct
import re
import itertools

import numpy as np

MAX_BYTES = 64 << 20
SURFER6_ASCII_BLANK = 1.70141e38
_NUMBER = re.compile(rb'[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?')

SURFER6_BLANK = struct.unpack('<f', bytes.fromhex('eeffff7e'))[0]

_MESSAGES = {
    'size': ('Surfer 7 GRD 主文件须为不超过 64 MiB 的完整网格',
             'Surfer 7 primary must be a complete grid of at most 64 MiB'),
    'frame': ('GRD 首期只支持一个完整的 Surfer 7 GRID/DATA 结构',
              'Only one complete Surfer 7 GRID/DATA pair is supported'),
    'grid': ('Surfer 7 GRD 的版本、尺寸、网格或数据长度不受支持',
             'Unsupported Surfer 7 version, dimensions, grid or complete data frame'),
    'bounds': ('Surfer 7 GRD 的网格范围须为有限且可表示的坐标',
               'Surfer 7 grid bounds must be finite and representable'),
    'truncated': ('Surfer 7 GRD 像元数据不完整', 'Truncated Surfer 7 pixel data'),
    'nonfinite': ('Surfer 7 GRD 首期不支持非有限数值',
                  'Nonfinite Surfer 7 values are outside the numeric profile'),
    'blank': ('Surfer 7 v1 缺失值须使用精确的 BlankValue，不能静默按有效值读取',
              'Surfer 7 v1 missing values must use the exact BlankValue'),
    'trailing': ('Surfer 7 GRD 首期不支持尾随或额外结构',
                 'Surfer 7 trailing sections are outside the single-grid profile'),
}


def inspect_surfer(path, language='en'):
    path = Path(path)
    if path.stat().st_size > MAX_BYTES:
        raise ValueError('Surfer GRD 主文件不能超过 64 MiB' if language != 'en' else 'Surfer GRD primary cannot exceed 64 MiB')
    with path.open('rb') as source:
        signature = source.read(4)
    if signature == b'DSRB':
        return _inspect_surfer7(path, language)
    if signature == b'DSBB':
        return _inspect_surfer6(path, language)
    if signature == b'DSAA':
        return _inspect_surfer6_ascii(path, language)
    raise ValueError('只支持完整的 Surfer 6/7 GRD' if language != 'en' else 'Only complete Surfer 6/7 GRD is supported')


def _inspect_surfer6(path, language):
    def reject():
        raise ValueError('Surfer 6 GRD 须为完整、有界且数值有限的 Float32 网格' if language != 'en' else
                         'Surfer 6 GRD requires a complete bounded Float32 grid with finite values')

    size = path.stat().st_size
    if size < 72 or size > MAX_BYTES:
        reject()
    with path.open('rb') as source:
        header = source.read(56)
        if len(header) != 56 or header[:4] != b'DSBB':
            reject()
        columns, rows, x, max_x, y, max_y, minimum, maximum = struct.unpack_from('<hh6d', header, 4)
        if (rows < 2 or columns < 2 or size != 56 + rows*columns*4
            or not all(math.isfinite(v) for v in (x, max_x, y, max_y, minimum, maximum))
            or max_x <= x or max_y <= y or minimum > maximum):
            reject()
        dx, dy = (max_x-x)/(columns-1), (max_y-y)/(rows-1)
        if (not all(math.isfinite(v) for v in (dx, dy, x-dx/2, max_x+dx/2, y-dy/2, max_y+dy/2))
            or dx/2 <= 0 or dy/2 <= 0 or x-dx/2 >= x or max_x+dx/2 <= max_x
            or y-dy/2 >= y or max_y+dy/2 <= max_y):
            reject()
        remaining = rows*columns*4
        while remaining:
            block = source.read(min(remaining, 65536))
            if not block or len(block) % 4 or not np.all(np.isfinite(np.frombuffer(block, dtype='<f4'))):
                reject()
            remaining -= len(block)
        if source.read(1):
            reject()
    return {'profile':'surfer6_binary', 'driver':'GSBG',
            'node_origin':[x,y], 'node_spacing':[dx,dy], 'node_shape':[rows,columns],
            'node_extent':[x,y,max_x,max_y], 'blank_value':SURFER6_BLANK}


def _ascii_reject(language):
    raise ValueError('Surfer 6 ASCII 须为完整、有界且数值可表示的网格' if language != 'en' else
                     'Surfer 6 ASCII requires a complete bounded grid with representable values')


def _ascii_number(token, language):
    if len(token) > 128 or not _NUMBER.fullmatch(token):
        _ascii_reject(language)
    value = float(token)
    mantissa = re.split(rb'[eE]', token, maxsplit=1)[0]
    if not math.isfinite(value) or (value == 0 and any(c in b'123456789' for c in mantissa)):
        _ascii_reject(language)
    return value


def _ascii_header(source, language):
    lines = [source.readline(201) for _ in range(5)]
    if (sum(map(len, lines)) > 200 or lines[0] not in (b'DSAA\n', b'DSAA\r\n')
        or any(not line.endswith(b'\n') or b'\r' in line.removesuffix(b'\n').removesuffix(b'\r') for line in lines)):
        _ascii_reject(language)
    fields = [line.rstrip(b'\r\n').split() for line in lines[1:]]
    if any(len(pair) != 2 for pair in fields) or any(not re.fullmatch(rb'[0-9]+', v) for v in fields[0]):
        _ascii_reject(language)
    # Only ASCII space/tab delimit fields; bytes.split would also accept controls.
    if any(re.search(rb'[^0-9a-zA-Z+\-. \t]', line.rstrip(b'\r\n')) for line in lines[1:]):
        _ascii_reject(language)
    columns, rows = map(int, fields[0])
    if rows < 2 or columns < 2 or rows * columns > MAX_BYTES // 2:
        _ascii_reject(language)
    x, max_x, y, max_y, minimum, maximum = [_ascii_number(v, language) for pair in fields[1:] for v in pair]
    dx, dy = (max_x-x)/(columns-1), (max_y-y)/(rows-1)
    if (max_x <= x or max_y <= y or minimum > maximum
        or not all(math.isfinite(v) for v in (dx, dy, x-dx/2, max_x+dx/2, y-dy/2, max_y+dy/2))
        or dx/2 <= 0 or dy/2 <= 0 or x-dx/2 >= x or max_x+dx/2 <= max_x
        or y-dy/2 >= y or max_y+dy/2 <= max_y):
        _ascii_reject(language)
    return {'profile':'surfer6_ascii', 'driver':'GSAG', 'node_origin':[x,y],
            'node_spacing':[dx,dy], 'node_shape':[rows,columns],
            'node_extent':[x,y,max_x,max_y], 'blank_value':SURFER6_ASCII_BLANK}


def _ascii_values(source, language):
    pending = b''
    while block := source.read(65536):
        if re.search(rb'[^0-9eE+\-. \t\r\n]', block):
            _ascii_reject(language)
        parts = re.split(rb'[ \t\r\n]+', pending + block)
        pending = parts.pop()
        if len(pending) > 128:
            _ascii_reject(language)
        for token in parts:
            if token:
                yield _ascii_number(token, language)
    if pending:
        yield _ascii_number(pending, language)


def _inspect_surfer6_ascii(path, language):
    with path.open('rb') as source:
        facts = _ascii_header(source, language)
        count, expected = 0, math.prod(facts['node_shape'])
        for _ in _ascii_values(source, language):
            count += 1
            if count > expected:
                _ascii_reject(language)
        if count != expected:
            _ascii_reject(language)
    return facts


def validate_surfer_ascii_pixels(dataset, path, language='en'):
    """Validate the sole native decoder against the complete bounded text stream."""
    with Path(path).open('rb') as source:
        facts = _ascii_header(source, language)
        rows, columns = facts['node_shape']
        if (dataset.RasterXSize, dataset.RasterYSize) != (columns, rows):
            _ascii_reject(language)
        values = _ascii_values(source, language)
        band = dataset.GetRasterBand(1)
        for row in range(rows):
            for start in range(0, columns, 65536):
                size = min(65536, columns-start)
                expected = np.fromiter(itertools.islice(values, size), dtype=np.float64)
                actual = np.frombuffer(band.ReadRaster(start, rows-row-1, size, 1), dtype=np.float64)
                if (expected.size != size or not np.array_equal(actual, expected)
                    or not np.array_equal(np.signbit(actual), np.signbit(expected))):
                    _ascii_reject(language)
        if next(values, None) is not None:
            _ascii_reject(language)


def validate_surfer6_mask(dataset, blank, language='en'):
    """Reject native masks that cannot faithfully represent exact source NoData."""
    band = dataset.GetRasterBand(1)
    mask = band.GetMaskBand()
    dtype = np.float32 if dataset.GetDriver().ShortName == 'GSBG' else np.float64
    width, height = dataset.RasterXSize, dataset.RasterYSize
    if band.GetNoDataValue() != blank:
        raise ValueError('Surfer 6 GRD 原生 NoData 与源声明不一致' if language != 'en' else
                         'Surfer 6 GRD native NoData differs from its source profile')
    batch_rows = max(1, 65536 // (width*np.dtype(dtype).itemsize))
    for row in range(0, height, batch_rows):
        count = min(batch_rows, height-row)
        pixels = np.frombuffer(band.ReadRaster(0, row, width, count), dtype=dtype)
        actual = np.frombuffer(mask.ReadRaster(0, row, width, count), dtype=np.uint8)
        expected = np.where(pixels == blank, 0, 255)
        if not np.array_equal(actual, expected):
            raise ValueError('Surfer 6 GRD 原生有效性掩膜无法等价保留源缺失值，拒绝读取' if language != 'en' else
                             'Surfer 6 GRD native validity mask cannot preserve exact source NoData; input rejected')


def _inspect_surfer7(path, language):
    def reject(reason):
        raise ValueError(_MESSAGES[reason][1 if language == 'en' else 0])

    path = Path(path)
    size = path.stat().st_size
    if size < 132 or size > MAX_BYTES:
        reject('size')
    with path.open('rb') as source:
        header = source.read(100)
        if (len(header) != 100 or header[:4] != b'DSRB'
            or struct.unpack_from('<i', header, 4)[0] != 4
            or header[12:16] != b'GRID' or struct.unpack_from('<i', header, 16)[0] != 72
            or header[92:96] != b'DATA'):
            reject('frame')
        version = struct.unpack_from('<i', header, 8)[0]
        rows, columns = struct.unpack_from('<ii', header, 20)
        x, y, dx, dy, minimum, maximum, rotation, blank = struct.unpack_from('<8d', header, 28)
        declared = struct.unpack_from('<i', header, 96)[0]
        if (version not in (1, 2) or rows < 2 or columns < 2
            or declared != rows * columns * 8 or size != 100 + declared
            or not all(math.isfinite(v) for v in (x,y,dx,dy,minimum,maximum,rotation,blank))
            or dx <= 0 or dy <= 0 or rotation != 0 or minimum > maximum):
            reject('grid')
        max_x, max_y = x + (columns-1)*dx, y + (rows-1)*dy
        if (not all(math.isfinite(v) for v in (max_x,max_y,x-dx/2,max_x+dx/2,y-dy/2,max_y+dy/2))
            or max_x <= x or max_y <= y or dx/2 <= 0 or dy/2 <= 0
            or x-dx/2 >= x or max_x+dx/2 <= max_x
            or y-dy/2 >= y or max_y+dy/2 <= max_y):
            reject('bounds')
        remaining = declared
        while remaining:
            block = source.read(min(remaining, 65536))
            if not block or len(block) % 8:
                reject('truncated')
            values = np.frombuffer(block, dtype='<f8')
            if not np.all(np.isfinite(values)):
                reject('nonfinite')
            # GDAL 3.12 GS7BG exposes BlankValue as equality NoData. Do not
            # silently turn the version-1 range of missing values into data.
            if version == 1 and np.any(values > blank):
                reject('blank')
            remaining -= len(block)
        if source.read(1):
            reject('trailing')
    return {'profile':'surfer7_binary', 'driver':'GS7BG', 'version':version,
            'node_origin':[x,y], 'node_spacing':[dx,dy], 'node_shape':[rows,columns],
            'node_extent':[x,y,max_x,max_y], 'blank_value':blank}

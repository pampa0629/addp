"""Strict framing for the single supported Surfer 7 grid profile.

GS7BG remains the sole pixel/grid decoder. No related paths are opened here.
"""
from pathlib import Path
import math
import struct

import numpy as np

MAX_BYTES = 64 << 20
PROFILE = 'surfer7_binary'

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


def inspect_surfer7(path, language='en'):
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
    return {'profile':PROFILE, 'version':version,
            'node_origin':[x,y], 'node_spacing':[dx,dy], 'node_shape':[rows,columns],
            'node_extent':[x,y,max_x,max_y], 'blank_value':blank}

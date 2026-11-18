"""Self-contained GRIB fields: edition-specific proof, GDAL-only values."""
from __future__ import annotations
from contextlib import contextmanager
import ctypes
from functools import lru_cache
from datetime import datetime, timedelta, timezone
import hashlib
import json
import math
import os
from pathlib import Path
import resource
import struct
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
PROFILES = {1: 'grib1_regular_fields/v1', 2: 'grib2_regular_fields/v1'}


class SliceSelectionError(ValueError):
    pass


class SourceChanged(ValueError):
    pass


def _integer(value, low, high):
    if type(value) is not int or not low <= value <= high:
        raise SliceSelectionError('GRIB slice outside supported bounds')
    return value


def _complex_spatial_structure(representation, data, valid_count):
    """Prove 5.3 grouping and bit consumption, never reconstruct field values."""
    if len(representation)!=49 or representation[20] not in (0,1) or representation[21]!=1 or representation[22]!=0 or representation[47]!=2 or not 1<=representation[48]<=4:
        raise ValueError('unsupported GRIB complex spatial profile')
    groups=int.from_bytes(representation[31:35],'big')
    if valid_count<2 or not 1<=groups<=valid_count or any(representation[n]>32 for n in (19,35,36,46)):
        raise ValueError('unsupported GRIB grouping dimensions')
    if not math.isfinite(struct.unpack('>f',representation[11:15])[0]):
        raise ValueError('nonfinite GRIB packing reference')
    position=5+3*representation[48]
    if position>len(data):raise ValueError('incomplete GRIB spatial descriptors')
    def descriptors(bits, collect):
        nonlocal position
        byte_count=(groups*bits+7)//8
        if position+byte_count>len(data):raise ValueError('incomplete GRIB group descriptors')
        raw=data[position:position+byte_count];position+=byte_count
        unused=(-groups*bits)%8
        if unused and raw[-1]&((1<<unused)-1):raise ValueError('nonzero GRIB descriptor padding')
        if not collect:return None
        if bits==0:return [0]*groups
        return [(int.from_bytes(raw[(i*bits)//8:(i*bits+bits+7)//8],'big') >> ((-(i*bits+bits))%8)) & ((1<<bits)-1) for i in range(groups)]
    descriptors(representation[19],False)
    widths=[representation[35]+v for v in descriptors(representation[36],True)]
    if any(v>32 for v in widths):raise ValueError('unsupported GRIB group bit width')
    length_reference=int.from_bytes(representation[37:41],'big')
    lengths=[length_reference+v*representation[41] for v in descriptors(representation[46],True)]
    lengths[-1]=int.from_bytes(representation[42:46],'big')
    if any(v<1 for v in lengths) or sum(lengths)!=valid_count:
        raise ValueError('GRIB group lengths differ from packed count')
    value_bits=sum(w*n for w,n in zip(widths,lengths));byte_count=(value_bits+7)//8
    if position+byte_count>len(data):raise ValueError('incomplete GRIB group payload')
    unused=(-value_bits)%8
    if unused and data[position+byte_count-1]&((1<<unused)-1):raise ValueError('nonzero GRIB value padding')
    return position-5+byte_count


def _grib2_complex_facts(identification, product, packing, discipline):
    centre=int.from_bytes(identification[5:7],'big');master=identification[9]
    parameter=(discipline,product[9],product[10])
    parameters={(0,0,0):('TMP','[K]'),(0,2,2):('UGRD','[m/s]'),(0,2,3):('VGRD','[m/s]'),(0,1,8):('APCP','[kg/(m^2)]')}
    if centre!=7 or master!=2 or parameter not in parameters or product[17]!=1:
        raise ValueError('unsupported GRIB complex parameter or time unit')
    reference=datetime(int.from_bytes(identification[12:14],'big'),*identification[14:19],tzinfo=timezone.utc)
    forecast=_signed(product[18:22])
    if forecast<0:raise ValueError('unsupported negative GRIB complex forecast')
    start=reference+timedelta(hours=forecast);end=start;pdt=int.from_bytes(product[7:9],'big')
    time_facts=dict(reference_utc=reference.isoformat(),unit_code=1,forecast_value=forecast,start_utc=start.isoformat(),end_utc=end.isoformat(),operation='instant')
    if pdt==8:
        if len(product)!=58 or product[41]!=1 or bytes(product[46:49])!=bytes([1,2,1]) or product[53]!=255 or any(product[54:58]):
            raise ValueError('unsupported GRIB complex statistical interval')
        hours=int.from_bytes(product[49:53],'big');end=datetime(int.from_bytes(product[34:36],'big'),*product[36:41],tzinfo=timezone.utc)
        if hours<1 or end!=start+timedelta(hours=hours):raise ValueError('inconsistent GRIB cumulative interval')
        time_facts.update(end_utc=end.isoformat(),operation='accumulation',statistical_process=1,increment_type=2,interval_unit=1,interval_length=hours,missing_count=int.from_bytes(product[42:46],'big'))
    kind=product[22];scaled=int.from_bytes(product[24:28],'big')
    if kind not in (1,103) or product[23]!=0 or (kind==1 and scaled!=0) or product[28]!=255:
        raise ValueError('unsupported GRIB complex surface')
    level=dict(type_code=kind,raw_scale=0,raw_value=scaled,reference='surface' if kind==1 else 'above_ground')
    if kind==103:level.update(value=scaled,unit='m')
    element,unit=parameters[parameter]
    return dict(parameter=dict(centre=centre,subcentre=int.from_bytes(identification[7:9],'big'),master_table=master,local_table=identification[10],discipline=discipline,category=product[9],number=product[10]),
        time=time_facts,level=level,packing=dict(data_template=3,order=2,extra_octets=packing[48],groups=int.from_bytes(packing[31:35],'big'),missing_value_management=0,reference_ieee_hex=bytes(packing[11:15]).hex(),binary_scale=_signed(packing[15:17]),decimal_scale=_signed(packing[17:19])),
        reference_seconds=int(reference.timestamp()),forecast_seconds=forecast*3600,valid_seconds=int(end.timestamp()),element=element,unit=unit)


@lru_cache(maxsize=1)
def _ccsds_checker():
    library = ctypes.CDLL(os.environ['GEOPYTHON_GRIB_CCSDS_PREFLIGHT_LIB'])
    check = library.addp_grib_ccsds_complete
    check.argtypes = [ctypes.c_char_p, ctypes.c_size_t, ctypes.c_uint, ctypes.c_uint]
    check.restype = ctypes.c_int
    return check


def _grib2_ecmwf_ccsds(sections, discipline, point_count):
    identification, product, packing = (sections[n] for n in (1, 4, 5))
    local = sections.get(2)
    cumulative = product[7:9] == b'\x00\x08'
    local_table = int(cumulative)
    if (discipline != 0 or identification[5:10] != bytes.fromhex('0062000020')
            or identification[10:12] != bytes([local_table, 1])
            or identification[19:21] != b'\x00\x01'
            or local != bytes.fromhex('0000001102000100010009040130303031')):
        raise ValueError('unsupported ECMWF CCSDS identification or local definition')

    reference = datetime(int.from_bytes(identification[12:14], 'big'),
                         *identification[14:19], tzinfo=timezone.utc)
    forecast_hours = _signed(product[18:22])
    if product[17] != 1 or forecast_hours < 0:
        raise ValueError('unsupported ECMWF CCSDS forecast time')
    valid = reference + timedelta(hours=forecast_hours)
    time_facts = dict(reference_utc=reference.isoformat(), unit_code=1,
                      forecast_value=forecast_hours, start_utc=valid.isoformat(),
                      end_utc=valid.isoformat(), operation='instant')
    identity = tuple(product[9:11])
    if cumulative:
        grid = sections[3]
        if (grid[38:42] != b'\x00' * 4 or grid[63:71] != struct.pack('>II', 250000, 250000)
                or grid[71] != 0):
            raise ValueError('unsupported ECMWF CCSDS precipitation grid')
        if identity != (1, 193) or product[22:34] != b'\x01' + b'\xff' * 11:
            raise ValueError('unsupported ECMWF CCSDS precipitation parameter or level')
        duration = int.from_bytes(product[49:53], 'big')
        if (len(product) != 58 or product[18:22] != b'\x00' * 4 or not 1 <= duration <= 360
                or product[41:49] != bytes.fromhex('0100000000010201')
                or product[53:58] != bytes.fromhex('0d000001c2')):
            raise ValueError('unsupported ECMWF CCSDS precipitation interval')
        valid = datetime(int.from_bytes(product[34:36], 'big'), *product[36:41],
                         tzinfo=timezone.utc)
        if valid != reference + timedelta(hours=duration):
            raise ValueError('inconsistent ECMWF CCSDS precipitation end time')
        time_facts.update(end_utc=valid.isoformat(), operation='accumulation',
                          statistical_process=1, increment_type=2, interval_unit=1,
                          interval_length=duration, increment_unit=13, increment_value=450,
                          missing_count=0)
        element, unit = 'tp', '[m]'
        level = dict(type_code=1, raw_scale=None, raw_value=None, reference='surface')
    else:
        parameters = {(0, 0): ('TMP', '[K]', 2),
                      (2, 2): ('UGRD', '[m/s]', 10),
                      (2, 3): ('VGRD', '[m/s]', 10)}
        if identity not in parameters:
            raise ValueError('unsupported ECMWF CCSDS parameter')
        element, unit, height = parameters[identity]
        if (product[22:24] != b'\x67\x00' or int.from_bytes(product[24:28], 'big') != height
                or product[28:34] != b'\xff' * 6):
            raise ValueError('unsupported ECMWF CCSDS product or level')
        level = dict(type_code=103, raw_scale=0, raw_value=height,
                     reference='above_ground', value=height, unit='m')
    if (len(packing) != 25 or packing[17:19] != b'\x00\x00'
            or not 1 <= packing[19] <= 16 or packing[20:25] != b'\x00\x0e\x20\x00\x80'
            or sections[6][5] != 255 or (cumulative and packing[19] != 12)):
        raise ValueError('unsupported ECMWF CCSDS packing')
    binary_scale = _signed(packing[15:17])
    packing_reference = struct.unpack('>f', packing[11:15])[0]
    if not math.isfinite(packing_reference) or not -126 <= binary_scale <= 127 - packing[19]:
        raise ValueError('unsupported ECMWF CCSDS scaling')

    payload = sections[7][5:].tobytes()
    if not _ccsds_checker()(payload, len(payload), point_count, packing[19]):
        raise ValueError('incomplete ECMWF CCSDS codestream')

    return dict(
        parameter=dict(centre=98, subcentre=0, master_table=32, local_table=local_table,
                       discipline=0, category=identity[0], number=identity[1]),
        time=time_facts, level=level,
        packing=dict(data_template=42, bits_per_value=packing[19], flags=14,
                     block_size=32, reference_sample_interval=128,
                     reference_ieee_hex=bytes(packing[11:15]).hex(),
                     binary_scale=binary_scale, decimal_scale=0),
        local=dict(definition=1, section_bytes=17, mars_class='od',
                   mars_type='fc', mars_stream='oper', experiment_version='0001'),
        reference_seconds=int(reference.timestamp()), forecast_seconds=forecast_hours * 3600,
        valid_seconds=int(valid.timestamp()), element=element, unit=unit,
    )


def _grib2_fields(raw):
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
        if size < 20 or end > len(raw) or raw[end-4:end] != b'7777':
            raise ValueError('incomplete GRIB2 message')
        position = offset + 16
        sections = {}
        for number in (1, 2, 3, 4, 5, 6, 7):
            if number == 2 and (position + 5 > end - 4 or raw[position + 4] != 2):
                continue
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
        reserved = raw[offset+4:offset+6]
        if drt != 42 and (2 in sections or reserved != b'\x00\x00'):
            raise ValueError('unsupported GRIB2 reserved bytes or local section')
        if drt == 42:
            if reserved not in (b'\x00\x00', b'\xff\xff'):
                raise ValueError('unsupported ECMWF CCSDS envelope')
            facts = _grib2_ecmwf_ccsds(sections, raw[offset+6], count)
        elif drt == 0:
            if len(representation) != 21 or representation[19] > 32 or representation[20] not in (0, 1):
                raise ValueError('unsupported GRIB simple packing')
            data_bytes = (valid_count*representation[19]+7)//8
        elif drt == 4:
            if len(representation) != 12 or representation[11] not in (1, 2):
                raise ValueError('unsupported GRIB IEEE precision')
            data_bytes = valid_count * (4 if representation[11] == 1 else 8)
        elif drt == 3:
            data_bytes = _complex_spatial_structure(representation, sections[7], valid_count)
        else:
            raise ValueError('unsupported GRIB value encoding')
        if drt != 42 and len(sections[7]) != 5 + data_bytes:
            raise ValueError('GRIB packed payload length mismatch')
        fields.append(dict(name=f'message-{len(fields)+1}', offset=offset, size=size, width=width, height=height,
            edition=2, scan=scan, bitmap=bitmap[6:].tobytes() if bitmap[5] == 0 else None,
            product_template=pdt, data_template=drt, discipline=raw[offset+6]))
        if drt == 42:
            fields[-1]['facts'] = facts
        elif drt == 3:
            fields[-1]['facts']=_grib2_complex_facts(sections[1],product,representation,raw[offset+6])
        offset = end
    if not fields:
        raise ValueError('empty GRIB2 file')
    return fields


def _signed(raw):
    """GRIB1 signed integers use a sign bit, not two's complement."""
    value = int.from_bytes(raw, 'big')
    sign = 1 << (8*len(raw)-1)
    return -(value & (sign-1)) if value & sign else value


# Standard table 2/3 identities verified against the native driver. Local
# tables are deliberately not admitted by a fallback to these names/units.
GRIB1_PARAMETERS = {
    1: ('PRES', '[Pa]'), 2: ('PRMSL', '[Pa]'), 7: ('HGT', '[gpm]'),
    11: ('TMP', '[K]'), 15: ('TMAX', '[K]'), 16: ('TMIN', '[K]'),
    17: ('DPT', '[K]'), 32: ('WIND', '[m/s]'), 33: ('UGRD', '[m/s]'),
    34: ('VGRD', '[m/s]'), 52: ('RH', '[%]'), 61: ('APCP', '[kg/m^2]'),
}
GRIB1_TIME_UNITS = {0: 60, 1: 3600, 2: 86400, 10: 10800, 11: 21600, 12: 43200, 254: 1}


def _grib1_product(pds):
    if len(pds) not in (28, 40) or any(pds[28:]) or pds[3] not in (2, 3) or pds[8] not in GRIB1_PARAMETERS or pds[7] & 63:
        raise ValueError('unsupported GRIB1 product or parameter table')
    year, century = pds[12], pds[24]
    if not 1 <= year <= 100 or not 1 <= century <= 100:
        raise ValueError('invalid GRIB1 year or century')
    reference = datetime((century-1)*100+year, *pds[13:17], tzinfo=timezone.utc)
    unit, p1, p2, indicator = pds[17:21]
    included, missing = int.from_bytes(pds[21:23], 'big'), pds[23]
    if unit not in GRIB1_TIME_UNITS or indicator not in (0, 1, 2, 3, 4, 5, 10):
        raise ValueError('unsupported GRIB1 time semantics')
    if indicator == 1 and (p1 or p2) or indicator == 0 and p2 or indicator in (2, 3, 4, 5) and p2 < p1:
        raise ValueError('inconsistent GRIB1 time interval')
    if indicator not in (3, 4, 5) and (included or missing):
        raise ValueError('unexplained GRIB1 statistical counts')
    step = GRIB1_TIME_UNITS[unit]
    if indicator == 10:
        start = end = (p1*256+p2)*step
    else:
        start = p1*step
        end = p2*step if indicator in (2, 3, 4, 5) else start
    start_time, end_time = reference+timedelta(seconds=start), reference+timedelta(seconds=end)
    time = dict(reference_utc=reference.isoformat(), unit_code=unit, p1=p1, p2=p2,
        range_indicator=indicator, operation={0: 'instant', 1: 'initialized_analysis', 2: 'interval', 3: 'average', 4: 'accumulation', 5: 'difference', 10: 'instant'}[indicator],
        start_utc=start_time.isoformat(), end_utc=end_time.isoformat(), included_count=included, missing_count=missing)
    kind = pds[9]
    if kind not in (1, 100, 101, 102, 103, 105, 111, 112):
        raise ValueError('unsupported GRIB1 level semantics')
    raw = bytes(pds[10:12])
    level = dict(type_code=kind, raw_octets=list(raw), unit={1: 'none', 100: 'hPa', 101: 'kPa', 102: 'none', 103: 'm', 105: 'm', 111: 'cm', 112: 'cm'}[kind],
        reference={1: 'surface', 100: 'isobaric', 101: 'isobaric', 102: 'mean_sea_level', 103: 'mean_sea_level', 105: 'ground', 111: 'land_surface', 112: 'land_surface'}[kind])
    if kind in (1, 102):
        if any(raw):
            raise ValueError('unexplained GRIB1 surface level value')
    elif kind in (101, 112):
        if raw[0] > raw[1]:
            raise ValueError('inverted GRIB1 layer bounds')
        level.update(upper=raw[0], lower=raw[1])
    else:
        level['value'] = int.from_bytes(raw, 'big')
    return dict(parameter=dict(centre=pds[4], subcentre=pds[25], process=pds[5], table_version=pds[3], number=pds[8]),
        time=time, level=level, decimal_scale=_signed(pds[26:28]),
        reference_seconds=int(reference.timestamp()), valid_seconds=int(end_time.timestamp()), forecast_seconds=end)


def _grib1_fields(raw):
    fields, offset, total = [], 0, 0
    while offset < len(raw):
        if len(fields) >= MAX_MESSAGES or len(raw)-offset < 12 or raw[offset:offset+4] != b'GRIB' or raw[offset+7] != 1:
            raise ValueError('unsupported GRIB1 message envelope or mixed edition')
        size = int.from_bytes(raw[offset+4:offset+7], 'big')
        end = offset+size
        if size < 84 or size % 2 or end > len(raw) or raw[end-4:end] != b'7777':
            raise ValueError('incomplete GRIB1 message')
        position = offset+8
        sections = []
        # PDS flags determine whether BMS is present. GDS is mandatory here.
        bitmap_present = False
        for number in (1, 2, 3, 4):
            if number == 3 and not bitmap_present:
                continue
            if end-4-position < 3:
                raise ValueError('incomplete GRIB1 section')
            length = int.from_bytes(raw[position:position+3], 'big')
            if length < 4 or length % 2 or position+length > end-4:
                raise ValueError('invalid GRIB1 section length')
            section = memoryview(raw)[position:position+length]
            sections.append(section)
            position += length
            if number == 1:
                facts = _grib1_product(section)
                if not section[7] & 128:
                    raise ValueError('GRIB1 requires an explicit grid')
                bitmap_present = bool(section[7] & 64)
        if position != end-4:
            raise ValueError('unexplained GRIB1 message bytes')
        pds, grid, *rest = sections
        if len(grid) != 32 or grid[3] != 0 or grid[4] != 255 or grid[5] != 0 or any(grid[28:]) or grid[16] != 128:
            raise ValueError('unsupported GRIB1 regular grid')
        width, height = int.from_bytes(grid[6:8], 'big'), int.from_bytes(grid[8:10], 'big')
        count = width*height
        scan = grid[27]
        dx, dy = int.from_bytes(grid[23:25], 'big'), int.from_bytes(grid[25:27], 'big')
        lat1, lon1, lat2, lon2 = (_signed(grid[a:a+3]) for a in (10, 13, 17, 20))
        if not 2 <= width < 65535 or not 2 <= height < 65535 or count > MAX_POINTS or scan not in (0, 64) or not 0 < dx < 65535 or not 0 < dy < 65535:
            raise ValueError('unsupported GRIB1 grid size or scan')
        if max(abs(lat1), abs(lat2)) > 90000 or max(abs(lon1), abs(lon2)) > 360000 or lon2-lon1 != (width-1)*dx or lat2-lat1 != (height-1)*dy*(1 if scan == 64 else -1):
            raise ValueError('inconsistent GRIB1 declared grid')
        total += count
        if total > MAX_TOTAL_POINTS:
            raise ValueError('GRIB total grid budget exceeded')
        bitmap = None
        valid_count = count
        if bitmap_present:
            bms, data = rest
            if len(bms) < 6 or int.from_bytes(bms[4:6], 'big') != 0 or not 0 <= bms[3] <= 15 or (len(bms)-6)*8-bms[3] != count:
                raise ValueError('unsupported GRIB1 bitmap reference or length')
            bitmap = bytes(bms[6:])
            bits = np.unpackbits(np.frombuffer(bitmap, dtype='uint8'))
            if np.any(bits[count:]):
                raise ValueError('GRIB1 bitmap padding is nonzero')
            valid_count = int(bits[:count].sum())
        else:
            data, = rest
        if len(data) < 12 or data[3] & 0xf0 not in (0, 32) or data[10] > 32:
            raise ValueError('unsupported GRIB1 value encoding')
        unused, bits_per_value = data[3] & 15, data[10]
        if (len(data)-11)*8-unused != valid_count*bits_per_value:
            raise ValueError('GRIB1 packed data length differs from bitmap')
        if unused and int.from_bytes(data[-2:], 'big') & ((1 << unused)-1):
            raise ValueError('GRIB1 packed padding is nonzero')
        facts['packing'] = dict(bits_per_value=bits_per_value, original_integer=bool(data[3] & 32),
            binary_scale=_signed(data[4:6]), decimal_scale=facts.pop('decimal_scale'), reference_ibm_hex=bytes(data[6:10]).hex())
        fields.append(dict(name=f'message-{len(fields)+1}', offset=offset, size=size, edition=1,
            width=width, height=height, scan=scan, bitmap=bitmap, facts=facts,
            transform=[(lon1-dx/2)/1000, dx/1000, 0., (max(lat1,lat2)+dy/2)/1000, 0., -dy/1000]))
        offset = end
    if not fields:
        raise ValueError('empty GRIB1 file')
    return fields


def _fields(raw):
    if len(raw) < 8 or raw[:4] != b'GRIB' or raw[7] not in PROFILES:
        raise ValueError('unsupported GRIB edition')
    return (_grib1_fields if raw[7] == 1 else _grib2_fields)(raw)


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
    name = '/vsimem/addp-grib-' + uuid.uuid4().hex + '.grb'
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
    message = dict(number=int(field['name'].split('-')[1]), edition=field['edition'])
    if field['edition'] == 1:
        facts = field['facts']
        for key, fact in (('GRIB_REF_TIME', 'reference_seconds'), ('GRIB_VALID_TIME', 'valid_seconds'), ('GRIB_FORECAST_SECONDS', 'forecast_seconds')):
            if int(metadata[key]) != facts[fact]:
                raise ValueError('GRIB1 decoded time differs from source semantics')
        element, unit = GRIB1_PARAMETERS[facts['parameter']['number']]
        if metadata.get('GRIB_ELEMENT') != element or metadata.get('GRIB_UNIT') != unit or list(transform) != field['transform']:
            raise ValueError('GRIB1 parameter or grid differs from source semantics')
        message.update({key: facts[key] for key in ('parameter', 'time', 'level', 'packing')})
        for key in ('parameter', 'time', 'level', 'packing'):
            metadata['GRIB1_'+key.upper()] = json.dumps(facts[key], sort_keys=True, separators=(',', ':'))
    else:
        message.update(discipline=field['discipline'], product_template=field['product_template'], data_template=field['data_template'])
        if field['data_template'] in (3, 42):
            facts=field['facts']
            metadata['GRIB2_DECODE_PRECISION']='float32_scaling_float64_output'
            for key,fact in (('GRIB_REF_TIME','reference_seconds'),('GRIB_VALID_TIME','valid_seconds'),('GRIB_FORECAST_SECONDS','forecast_seconds')):
                if int(metadata[key])!=facts[fact]:raise ValueError('GRIB2 decoded time differs from original interval')
            if field['data_template'] == 42 and facts['parameter']['local_table'] == 1:
                native_parameter = (metadata.get('GRIB_ELEMENT'), metadata.get('GRIB_UNIT'))
                if native_parameter not in {('unknown', '[-]'), (facts['element'], facts['unit'])}:
                    raise ValueError('GDAL precipitation metadata conflicts with ECMWF local table')
                metadata.update(GRIB_ELEMENT=facts['element'], GRIB_UNIT=facts['unit'],
                                GRIB_COMMENT='Total precipitation [m]')
            elif metadata.get('GRIB_UNIT')!=facts['unit'] or (facts['element']!='APCP' and metadata.get('GRIB_ELEMENT')!=facts['element']) or (facts['element']=='APCP' and not metadata.get('GRIB_ELEMENT','').startswith('APCP')):
                raise ValueError('GRIB2 decoded parameter or unit differs from source')
            for key in ('parameter','time','level','packing'):
                message[key]=facts[key]
                metadata['GRIB2_'+key.upper()]=json.dumps(facts[key],sort_keys=True,separators=(',',':'))
            if field['data_template'] == 42:
                message['local'] = facts['local']
                metadata['GRIB2_LOCAL'] = json.dumps(facts['local'], sort_keys=True, separators=(',', ':'))
    return dict(name=field['name'], type='Float64', dimensions=[dict(name='y',size=str(field['height']),axis_type='HORIZONTAL_Y',direction='NORTH_TO_SOUTH'),dict(name='x',size=str(field['width']),axis_type='HORIZONTAL_X',direction='WEST_TO_EAST')],
        attributes={key:dict(type='String',values=[value]) for key,value in metadata.items()}, attributes_coverage='selected',slice_supported=True,
        value_mode='decoded_native_units',grid=dict(transform=list(transform),crs_wkt=dataset.GetProjectionRef(),longitude_policy='unchanged'),
        message=message)


def inspect(access_plan):
    with _source(access_plan) as (raw, fields, version):
        children=[]
        for field in fields:
            with _dataset(raw,field) as dataset:
                description=_description(dataset,field)
            children.append(dict(name=field['name'],child_kind='array',data_type='media',format='grib',native=description))
        result=dict(schema_version='gdal.grib.inspect/v1',format='grib',container=dict(child_count=len(children),resource_count=1,children=children),
            format_info=dict(driver='GRIB',encoding='GRIB'+str(fields[0]['edition']),profile=PROFILES[fields[0]['edition']],scan_complete=True,source_version=version,value_mode='decoded_native_units'))
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
        description=name,brief_description='GRIB bounded self-contained fields',execution_modes=['direct'],effects=['read'],
        params=[OperatorParam(name='access_plan',type='param',data_type='object',description='Exact single-file source plan',required=True),*params],
        overview='GRIB单文件只读',use_cases=['气象场和元信息查看','有界数值切片'],notes=['Original units and longitude; no external idx','No map or export'],workflow_example={})
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

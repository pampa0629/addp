#!/usr/bin/env python3
"""Own disposable physical raster fixtures; never call platform APIs."""
from __future__ import annotations

import hashlib
import http.client
import http.server
import socket
import threading
from bisect import bisect_right
from functools import lru_cache
import json
import math
import os
from pathlib import Path
import secrets
import stat
import statistics
import struct
import subprocess
import sys
import tempfile
import time
import urllib.request

SUITE = 'raster-workflow'
CONTAINERS = ('addp-raster-source', 'addp-raster-target', 'addp-raster-fixture-worker', 'addp-raster-cleanup-proxy',
              'addp-raster-source-b', 'addp-raster-source-c')
MINIO_ROLES = {'source': (CONTAINERS[0], 59012), 'target': (CONTAINERS[1], 59013),
               'source-b': (CONTAINERS[4], 59015), 'source-c': (CONTAINERS[5], 59016)}
MINIO_IMAGE = 'addp-minio:RELEASE.2025-10-15T17-29-55Z'
TRANSFORM = (110, .01, 0, 20.32, 0, -.01)
ROTATED_TRANSFORM = (110, .01, .004, 20.32, .002, -.02)
ROTATED_SOURCE = 'multiband-rotated.tif'
TOPOLOGY_SOURCE = 'footprint-topology.tif'
TOPOLOGY_BANDS = {'footprint-diagonal': 1, 'footprint-pinch': 2}
SIZE = 256
ANGULAR_METRE = math.degrees(1 / 6378137)
SPATIAL_SOURCE_TRANSFORM = (0, ANGULAR_METRE, 0, SIZE * ANGULAR_METRE, 0, -ANGULAR_METRE)
SPATIAL_TRANSFORM = (0, 1, 0, SIZE, 0, -1)
SPATIAL_NODATA = {0, 127 * SIZE + 127}
GRID_CASES = ('resample-size', 'resample-resolution', 'clip-polygon')
MULTIBAND_CASES = ('multiband-alpha', 'multiband-joint', 'multiband-average', 'multiband-average-joint', 'multiband-bilinear', 'multiband-bilinear-joint', 'multiband-average-fractional', 'multiband-average-fractional-joint', 'multiband-average-finite', 'multiband-average-finite-joint')
UTILITY_CASES = ('build-overviews', 'to-cog')
FOUNDATION_CASES = ('multiraster-weighted', 'multiraster-rgb')
RECLASS_CASES = ('reclassify-nodata', 'reclassify-keep')
AGGREGATE_SINGLE_BAND_CASES = ('aggregate-mean', 'aggregate-sum', 'aggregate-min', 'aggregate-max')
AGGREGATE_MULTIBAND_CASES = ('aggregate-multiband-mean', 'aggregate-rotated-sum')
AGGREGATE_CASES = AGGREGATE_SINGLE_BAND_CASES + AGGREGATE_MULTIBAND_CASES
OUTSIDE_CASES = ('clip-outside',)
UPDATE_CASES = ('update-valid',)
MOSAIC_SOURCE_CASES = ('multisource-first', 'multisource-last')
MOSAIC_EDITOR_CASES = ('multisource-editor-first', 'multiengine-editor-first')
MOSAIC_CASES = MOSAIC_SOURCE_CASES + MOSAIC_EDITOR_CASES
TERRAIN_CASES = ('dem-slope', 'dem-aspect', 'dem-hillshade')
TERRAIN_CONTOUR_LEVELS = (110000., 120000., 150000.)
# start column, width, numeric offset; all are independent stored TIFFs.
MOSAIC_SOURCE_WINDOWS = {'mosaic-a.tif': (0, 160, 100000), 'mosaic-b.tif': (96, 112, 200000),
                         'mosaic-c.tif': (144, 112, 300000)}
MOSAIC_SOURCE_ORDER = ('mosaic-c.tif', 'mosaic-a.tif', 'mosaic-b.tif')
MOSAIC_SOURCE_ROLES = {'mosaic-a.tif': 'source', 'mosaic-b.tif': 'source-b', 'mosaic-c.tif': 'source-c'}
SOURCE_VALUE_OFFSETS = {'source': 0, 'source-b': 5000000, 'source-c': 9000000}
MOSAIC_SOURCE_HOLES = {
    ('mosaic-a.tif', 1): (127, 150), ('mosaic-b.tif', 1): (127, 151), ('mosaic-c.tif', 1): (127, 152),
    ('mosaic-a.tif', 2): (64, 120), ('mosaic-b.tif', 2): (64, 121), ('mosaic-c.tif', 2): (64, 153)}
FOOTPRINT_CASES = ('footprint-any', 'footprint-all', 'footprint-band-2', 'footprint-holes', 'footprint-rotated', 'footprint-diagonal', 'footprint-pinch')
TILE_WINDOWS = {'r0_c0': (0, 0, 160, 160), 'r0_c160': (160, 0, 96, 160),
                'r160_c0': (0, 160, 160, 96), 'r160_c160': (160, 160, 96, 96)}
UTILITY_JSON_CASES = ('info-overviews', 'validate-cog-invalid', 'validate-cog-valid')
NON_COG_SOURCE = 'non-cog.tif'
SOURCE_FILES = tuple((name, False, float('nan')) for name in MOSAIC_SOURCE_WINDOWS) + (('source.tif', False, -9999.), ('spatial.tif', True, -9999.),
                ('multiband.tif', False, float('nan')), ('multiband-average.tif', False, float('nan')),
                ('multiband-average-finite.tif', False, -9999.), (ROTATED_SOURCE, False, float('nan')),
                (TOPOLOGY_SOURCE, False, -9999.))
SOURCE_FILES_BY_ROLE = {'source': SOURCE_FILES,
                        'source-b': (('mosaic-b.tif', False, float('nan')),),
                        'source-c': (('mosaic-c.tif', False, float('nan')),)}
TILE_FAILURE_PREFIX = 'tiles-write-failure'
TILE_FAILURE_KEY = 'r0_c160'
TILE_CLEANUP_CASES = {'tiles-cleanup-mid': ('r0_c0', 1), 'tiles-cleanup-final': ('r160_c160', 4)}
CLEANUP_ACTIONS = tuple('verify-' + prefix for prefix in TILE_CLEANUP_CASES)
TILE_KEEP_NAME = 'tiles-basic/keep.txt'
TILE_KEEP_BYTES = b'Unrelated raster fixture file; replace must preserve these bytes.\n'
ACTIONS = CLEANUP_ACTIONS + ('verify-tiles-failure', 'verify-tiles', 'prepare-tiles-replace', 'verify-tiles-replace', 'seed', 'verify-create', 'verify-replace', 'verify-mosaic-first', 'verify-mosaic-last', 'verify-analysis') + tuple('verify-' + name for name in GRID_CASES + MULTIBAND_CASES + UTILITY_CASES + FOUNDATION_CASES + RECLASS_CASES + AGGREGATE_CASES + OUTSIDE_CASES + UPDATE_CASES + MOSAIC_CASES) + ('verify-utility-queries',)
ACTIONS += ('prepare-terrain', 'verify-dem-contour') + tuple('verify-' + name for name in TERRAIN_CASES)
ANALYSIS_CASES = ('statistics-band-2', 'statistics-all-invalid', 'histogram-auto', 'histogram-range')


def analysis_expectations():
    """Independent numeric oracle from fixture formulas, without GDAL/NumPy/operators."""
    values = list(range(2, SIZE * SIZE + 1))
    total = SIZE * SIZE
    band_values = [2 * value for value in values]
    stats = {'band': 2, 'strategy': 'full', 'valid_count': len(values), 'invalid_count': 1,
             'nodata_ratio': 1 / total, 'min': min(band_values), 'max': max(band_values),
             'mean': statistics.fmean(band_values), 'stddev': statistics.pstdev(band_values)}

    def histogram(pixels, band, low, high):
        edges = [low + (high - low) * index / 4 for index in range(5)]
        counts = [0] * 4
        outside = 0
        for value in pixels:
            if value < low or value > high:
                outside += 1
            else:
                counts[min(bisect_right(edges, value) - 1, 3)] += 1
        return {'band': band, 'strategy': 'full', 'edges': edges, 'counts': counts,
                'valid_count': len(pixels), 'invalid_count': 1, 'outside_count': outside}

    return {
        'statistics-band-2': stats,
        'statistics-all-invalid': {'band': 1, 'strategy': 'full', 'valid_count': 0,
            'invalid_count': total, 'nodata_ratio': 1.0, 'min': None, 'max': None, 'mean': None, 'stddev': None},
        'histogram-auto': histogram(values, 1, min(values), max(values)),
        'histogram-range': histogram(band_values, 2, 32768, 98304),
    }


def source_values(band, spatial=False):
    invalid = SPATIAL_NODATA if spatial else {0}
    return tuple(-9999. if position in invalid else float(band * (position + 1)) for position in range(SIZE * SIZE))


def artifact_expectation(spatial=False):
    """Persisted-output oracle: band_nodata=None requires declared NaN, never missing NoData."""
    transform = SPATIAL_TRANSFORM if spatial else TRANSFORM
    return {'width': SIZE, 'height': SIZE, 'band_count': 1, 'source_crs': 'EPSG:3857' if spatial else 'EPSG:4326',
            'extent_srid': 3857 if spatial else 4326, 'transform': list(transform),
            'extent': [transform[0], transform[3] + SIZE * transform[5], transform[0] + SIZE * transform[1], transform[3]],
            'valid_pixels': SIZE * SIZE - (len(SPATIAL_NODATA) if spatial else 1), 'band_nodata': None}


def utility_expectation():
    return {**artifact_expectation(), 'band_count': 2, 'band_nodata': -9999.}


def utility_info_expectation(crs):
    facts = utility_expectation()
    return {key: value for key, value in facts.items() if key not in ('valid_pixels', 'band_nodata')} | {
        'crs': crs, 'bands': [{'band': index, 'dtype': 'Float64', 'nodata': -9999.,
            'color_interpretation': 'Gray' if index == 1 else 'Undefined',
            'nodata_is_nan': False, 'block_size': [128, 128], 'overviews': [[128, 128], [64, 64]]}
            for index in (1, 2)]}


def clip_geometry():
    # Non-integer metre boundaries keep transformed edges away from pixel grid
    # rounding; pixel centres still have an exact integer-domain inclusion oracle.
    return {'type': 'Polygon', 'coordinates': [
        [[31.75,31.75],[224.25,31.75],[224.25,128.25],[128.25,128.25],
         [128.25,224.25],[31.75,224.25],[31.75,31.75]],
        [[63.75,63.75],[63.75,96.25],[96.25,96.25],[96.25,63.75],[63.75,63.75]],
    ]}


def grid_pixels(case_name):
    """Independent nearest-centre/point-in-L oracle, without GDAL or operators."""
    if case_name not in GRID_CASES:
        raise ValueError('unknown raster grid case')
    width, height = (192, 192) if case_name == 'clip-polygon' else (128, 128 if case_name == 'resample-size' else 64)
    for row in range(height):
        for column in range(width):
            if case_name == 'clip-polygon':
                x, y = 32 + column, 223 - row
                position = (SIZE - 1 - y) * SIZE + x
                inside = (x < 128 or y < 128) and not (64 <= x < 96 and 64 <= y < 96)
            else:
                step_y = 2 if case_name == 'resample-size' else 4
                position = (step_y * row + step_y // 2) * SIZE + 2 * column + 1
                inside = True
            yield None if not inside or position in SPATIAL_NODATA else float(position + 1)


def grid_expectation(case_name):
    polygon = case_name == 'clip-polygon'
    width, height = (192, 192) if polygon else (128, 128 if case_name == 'resample-size' else 64)
    step_x, step_y = (1, 1) if polygon else (2, 2 if case_name == 'resample-size' else 4)
    transform = [32 * ANGULAR_METRE if polygon else 0, step_x * ANGULAR_METRE, 0,
                 (224 if polygon else SIZE) * ANGULAR_METRE, 0, -step_y * ANGULAR_METRE]
    return {'width': width, 'height': height, 'band_count': 2 if polygon else 1,
            'source_crs': 'EPSG:4326', 'extent_srid': 4326, 'transform': transform,
            'extent': [transform[0], transform[3] + height * transform[5],
                       transform[0] + width * transform[1], transform[3]],
            'valid_pixels': sum(value is not None for value in grid_pixels(case_name)), 'band_nodata': None}


def multiband_pixels(band, *, source=False, joint=False):
    """Uniform 2x2 source cells; independent nearest-centre and joint-mask oracle."""
    width = SIZE if source else SIZE // 2
    for row in range(width):
        for column in range(width):
            cell = (row // 2 if source else row) * (SIZE // 2) + (column // 2 if source else column)
            if band == 3:
                yield 0. if cell == 2 else 128. if cell in (0, 1, 3) else 255.
            elif joint:
                yield None if cell in (0, 1, 2) else 0. if cell == 4 else float(3 * (cell + 1))
            elif cell == band - 1 or (cell == 2 and not source):
                yield None
            else:
                yield 1e6 if cell == 2 else 0. if cell == 4 else float(band * (cell + 1))


def average_source_pixel(band, row, column):
    cell, sample = (row // 2) * (SIZE // 2) + column // 2, (row % 2) * 2 + column % 2
    if band == 3:
        return 0. if cell == 2 else 128. if cell in (0, 1, 3, 6) else 255.
    if (cell == band - 1 or (cell == 5 and sample == (0 if band == 1 else 3))
        or (cell == 6 and sample == (1 if band == 1 else 2))):
        return None
    return 1e6 if cell == 2 else 0. if cell == 4 else float(band * (cell * 16 + sample * 4))


def area_average(band, row, column, width=SIZE // 2):
    """Exact integer-domain overlap weights; the common area denominator cancels."""
    left, top, right, bottom = column * SIZE, row * SIZE, (column + 1) * SIZE, (row + 1) * SIZE
    contributions = []
    for y in range(top // width, (bottom + width - 1) // width):
        for x in range(left // width, (right + width - 1) // width):
            value = average_source_pixel(band, y, x)
            area = max(0, min((x + 1) * width, right) - max(x * width, left)) * max(0, min((y + 1) * width, bottom) - max(y * width, top))
            if value is not None and average_source_pixel(3, y, x) > 0 and area > 0:
                contributions.append((value, area))
    return (math.fsum(value * area for value, area in contributions) / math.fsum(area for _, area in contributions)
            if contributions else None)


@lru_cache(maxsize=16)
def average_pixels(band, *, source=False, joint=False, width=SIZE // 2):
    # Only fixed independent expectations are reused. Actual object bytes,
    # GDAL reads, hashes and verification verdicts are checked on every call.
    width = SIZE if source else width
    def values():
        for row in range(width):
            for column in range(width):
                if source:
                    yield average_source_pixel(band, row, column)
                elif band == 3 or joint:
                    first, second = area_average(1, row, column, width), area_average(2, row, column, width)
                    if band == 3:
                        # GDAL average generates coverage; source opacity is not copied.
                        yield 255. if first is not None or second is not None else 0.
                    else:
                        yield first + second if first is not None and second is not None else None
                else:
                    yield area_average(band, row, column, width)
    return tuple(values())


def bilinear_value(band, row, column):
    """Independent four-centre distance weights, excluding this band's invalid neighbours."""
    center_x, center_y = (column + .5) / 2, (row + .5) / 2
    if (average_source_pixel(band, int(center_y), int(center_x)) is None
        or average_source_pixel(3, int(center_y), int(center_x)) <= 0):
        return None
    left, top = math.floor(center_x - .5), math.floor(center_y - .5)
    contributions = []
    for y in range(max(0, top), min(SIZE, top + 2)):
        for x in range(max(0, left), min(SIZE, left + 2)):
            value = average_source_pixel(band, y, x)
            weight = max(0, 1 - abs(x + .5 - center_x)) * max(0, 1 - abs(y + .5 - center_y))
            if value is not None and average_source_pixel(3, y, x) > 0 and weight > 0:
                contributions.append((value, weight))
    return math.fsum(value * weight for value, weight in contributions) / math.fsum(weight for _, weight in contributions)


@lru_cache(maxsize=8)
def bilinear_pixels(band, *, joint=False):
    # Only fixed independent expectations are reused. Actual object bytes,
    # GDAL reads, hashes and verification verdicts are checked on every call.
    def values():
        for row in range(SIZE * 2):
            for column in range(SIZE * 2):
                if band == 3:
                    # This Warp kernel generates alpha from the covered containing source cell.
                    yield average_source_pixel(3, row // 2, column // 2)
                elif joint:
                    first, second = bilinear_value(1, row, column), bilinear_value(2, row, column)
                    yield first + second if first is not None and second is not None else None
                else:
                    yield bilinear_value(band, row, column)
    return tuple(values())


def multiband_source_name(case_name):
    if case_name not in MULTIBAND_CASES:
        raise ValueError('unknown raster multiband case')
    joint = case_name.endswith('-joint')
    prefix = case_name.removesuffix('-joint') if joint else (
        'multiband-average' if 'average' in case_name or 'bilinear' in case_name else 'multiband')
    if case_name == 'multiband-joint':
        prefix = 'multiband-alpha'
    if 'finite' in case_name and not joint:
        prefix = 'multiband-average-finite'
    return prefix + ('.cog.tif' if joint else '.tif')


def multiband_expectation(case_name):
    if case_name not in MULTIBAND_CASES:
        raise ValueError('unknown raster multiband case')
    result = artifact_expectation()
    bilinear, joint = 'bilinear' in case_name, case_name.endswith('-joint')
    width = 171 if 'fractional' in case_name or 'finite' in case_name else 512 if bilinear else 128
    resolution = SIZE * TRANSFORM[1] / width
    invalid = (64 if joint else 40) if bilinear else (3 if joint else 2)
    result.update(width=width, height=width, band_count=1 if joint else 3,
                  transform=[110, resolution, 0, 20.32, 0, -resolution],
                  valid_pixels=width * width - invalid,
                  band_nodata=-9999. if 'finite' in case_name and not joint else None)
    return result


def foundation_expectation(case_name):
    if case_name not in FOUNDATION_CASES:
        raise FixtureError('unknown multi-raster case')
    return {**artifact_expectation(True), 'band_count': 3 if case_name == 'multiraster-rgb' else 1}


def foundation_pixels(case_name, band=1, level=1):
    """Independent source-cell and first-mosaic seam oracle; no GDAL computation."""
    for row in range(0, SIZE, level):
        for column in range(0, SIZE, level):
            position = row * SIZE + column
            factor = 3 if column >= 160 else 1
            yield None if position in SPATIAL_NODATA else float((position + 1) * (
                .25 + .75 * factor if case_name == 'multiraster-weighted' else factor if band == 2 else 1))


def reclass_pixels(case_name, band=1, level=1):
    """Independent selected source band, interval edges, zero class and gap oracle."""
    for row in range(0, SIZE, level):
        for column in range(0, SIZE, level):
            position = row * SIZE + column
            value = 2 * (position + 1)
            yield (None if position == 0 else 0. if value == 4 else 1. if 6 <= value < 1024
                   else 2. if value >= 32768 else float(value) if case_name == 'reclassify-keep' else None)


def reclass_expectation(case_name):
    if case_name not in RECLASS_CASES:
        raise FixtureError('unknown reclassification case')
    return {**artifact_expectation(), 'valid_pixels': sum(value is not None for value in reclass_pixels(case_name))}


def aggregate_config(case_name):
    if case_name not in AGGREGATE_CASES:
        raise FixtureError('unknown aggregation case')
    if case_name == 'aggregate-multiband-mean':
        return {'source_name': 'multiband.tif', 'factors': [2, 2], 'method': 'mean', 'bands': None}
    if case_name == 'aggregate-rotated-sum':
        return {'source_name': ROTATED_SOURCE, 'factors': [3, 5], 'method': 'sum', 'bands': None}
    return {'source_name': 'source.tif', 'factors': [3, 5],
            'method': case_name.removeprefix('aggregate-'), 'bands': [2]}


def aggregate_expectation(case_name):
    config = aggregate_config(case_name)
    factor_x, factor_y = config['factors']
    width, height = math.ceil(SIZE/factor_x), math.ceil(SIZE/factor_y)
    source_transform = ROTATED_TRANSFORM if config['source_name'] == ROTATED_SOURCE else TRANSFORM
    transform = [source_transform[0], source_transform[1]*factor_x, source_transform[2]*factor_y,
                 source_transform[3], source_transform[4]*factor_x, source_transform[5]*factor_y]
    corners = [(transform[0]+x*transform[1]+y*transform[2], transform[3]+x*transform[4]+y*transform[5])
               for x, y in ((0, 0), (width, 0), (width, height), (0, height))]
    counts = [sum(value is not None for value in aggregate_pixels(case_name, band))
              for band in range(1, 3 if config['bands'] is None else 2)]
    return {**artifact_expectation(), 'width': width, 'height': height,
            'band_count': len(counts), 'transform': transform,
            'extent': [min(x for x,y in corners), min(y for x,y in corners),
                       max(x for x,y in corners), max(y for x,y in corners)],
            'valid_pixels': counts[0], 'band_valid_pixels': counts}


@lru_cache(maxsize=None)
def aggregate_values(case_name, band):
    """Scalar fixture oracle, independent of GDAL masks and product reducers."""
    config = aggregate_config(case_name)
    if band not in ((1,) if config['bands'] else (1, 2)):
        raise FixtureError('unknown aggregation output band')
    factor_x, factor_y = config['factors']
    values = []
    for row in range(0, SIZE, factor_y):
        for column in range(0, SIZE, factor_x):
            contributors = []
            for y in range(row, min(row+factor_y, SIZE)):
                for x in range(column, min(column+factor_x, SIZE)):
                    if config['bands']:
                        if y*SIZE+x != 0:
                            contributors.append(2.*(y*SIZE+x+1))
                    else:
                        cell = (y//2)*(SIZE//2)+x//2
                        if cell not in (band-1, 2):
                            contributors.append(0. if cell == 4 else float(band*(cell+1)))
            method = config['method']
            if not contributors:
                value = None
            elif method == 'mean':
                value = math.fsum(contributors)/len(contributors)
            elif method == 'sum':
                value = math.fsum(contributors)
            elif method == 'min':
                value = min(contributors)
            else:
                value = max(contributors)
            values.append(value)
    return tuple(values)


def aggregate_pixels(case_name, band=1, level=1):
    config = aggregate_config(case_name)
    factor_x, factor_y = config['factors']
    width, height = math.ceil(SIZE/factor_x), math.ceil(SIZE/factor_y)
    pixels = aggregate_values(case_name, band)
    for row in range(0, height, level):
        for column in range(0, width, level):
            yield pixels[row*width+column]


def footprint_source_name(case_name):
    if case_name not in FOOTPRINT_CASES:
        raise FixtureError('unknown footprint case')
    return (TOPOLOGY_SOURCE if case_name in TOPOLOGY_BANDS else
            ROTATED_SOURCE if case_name == 'footprint-rotated' else
            'clip-outside.cog.tif' if case_name == 'footprint-holes' else 'multiband.tif')


def footprint_transform(case_name):
    return source_transform(footprint_source_name(case_name), False)


def source_transform(name, spatial):
    return ROTATED_TRANSFORM if name == ROTATED_SOURCE else SPATIAL_SOURCE_TRANSFORM if spatial else TRANSFORM


def source_grid(name, spatial):
    if name in MOSAIC_SOURCE_WINDOWS:
        start, width, _ = MOSAIC_SOURCE_WINDOWS[name]
        return width, SIZE, (start, 1, 0, SIZE, 0, -1), 3857
    return SIZE, SIZE, source_transform(name, spatial), 4326


def mosaic_source_value(name, band, row, column, role='source'):
    start, width, offset = MOSAIC_SOURCE_WINDOWS[name]
    offset += SOURCE_VALUE_OFFSETS[role]
    if not start <= column < start + width:
        return None
    if ((row, column) in ((128, 155), (255, 255)) or (row, column) == MOSAIC_SOURCE_HOLES[name, band]
        or (name == 'mosaic-a.tif' and band == 2 and (row, column) == (64, 10))):
        return None
    if (name, band, row, column) in (('mosaic-a.tif', 1, 0, 1), ('mosaic-c.tif', 2, 1, 240)):
        return 0.
    position = row * SIZE + column + 1
    return float(offset + position if band == 1 else -(offset + 2 * position))


def terrain_expectation():
    _, width, _ = MOSAIC_SOURCE_WINDOWS['mosaic-a.tif']
    count = sum(value is not None for value in terrain_pixels('dem-slope'))
    return {'width': width, 'height': SIZE, 'band_count': 1, 'extent_srid': 3857,
            'source_crs': 'EPSG:3857', 'transform': [0,1,0,SIZE,0,-1],
            'extent': [0,0,width,SIZE], 'band_nodata': None, 'valid_pixels': count}


@lru_cache(maxsize=6)
def terrain_pixels(case_name, level=1):
    if case_name not in TERRAIN_CASES:
        raise FixtureError('unknown terrain case')
    width = MOSAIC_SOURCE_WINDOWS['mosaic-a.tif'][1]
    azimuth, altitude = math.radians(315), math.radians(45)
    values=[]
    for row in range(0,SIZE,level):
        for column in range(0,width,level):
            if not (0<row<SIZE-1 and 0<column<width-1):
                values.append(None);continue
            z=[mosaic_source_value('mosaic-a.tif',1,row+dy,column+dx)
               for dy in (-1,0,1) for dx in (-1,0,1)]
            if any(value is None for value in z):
                values.append(None);continue
            east=(z[2]+2*z[5]+z[8]-z[0]-2*z[3]-z[6])/8
            north=(z[0]+2*z[1]+z[2]-z[6]-2*z[7]-z[8])/8
            magnitude=math.hypot(east,north)
            illumination=(-east*math.sin(azimuth)*math.cos(altitude)-north*math.cos(azimuth)*math.cos(altitude)
                          +math.sin(altitude))/math.sqrt(1+magnitude*magnitude)
            values.append(math.degrees(math.atan(magnitude)) if case_name=='dem-slope' else
                math.degrees(math.atan2(-east,-north))%360 if case_name=='dem-aspect' else
                float(math.floor(1+254*max(0,illumination)+.5)))
    return tuple(values)


def preterrain_target_names():
    names = {'result.cog.tif','mosaic-first.cog.tif','mosaic-last.cog.tif',TILE_KEEP_NAME}
    names.update(name+'.cog.tif' for name in GRID_CASES+MULTIBAND_CASES+UTILITY_CASES+FOUNDATION_CASES+
                 RECLASS_CASES+AGGREGATE_CASES+OUTSIDE_CASES+UPDATE_CASES+MOSAIC_CASES)
    names.update('tiles-basic/'+key+'.tif' for key in TILE_WINDOWS)
    for prefix,count in [(TILE_FAILURE_PREFIX,1)]+[(prefix,count) for prefix,(_,count) in TILE_CLEANUP_CASES.items()]:
        names.update(prefix+'/'+key+'.tif' for key in list(TILE_WINDOWS)[:count])
    return names


def verify_terrain_file(path, case_name):
    from osgeo import gdal, osr
    from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
    dataset = gdal.Open(str(path)); expected = terrain_expectation()
    _, errors, _ = validate(dataset, full_check=True)
    reference = osr.SpatialReference(); reference.ImportFromEPSG(3857)
    if (errors or dataset.GetMetadataItem('LAYOUT','IMAGE_STRUCTURE') != 'COG'
        or (dataset.RasterXSize,dataset.RasterYSize,dataset.RasterCount) != (expected['width'],SIZE,1)
        or dataset.GetGeoTransform() != tuple(expected['transform'])
        or not osr.SpatialReference(dataset.GetProjection()).IsSame(reference)):
        raise FixtureError('terrain COG structure/grid differs')
    band = dataset.GetRasterBand(1)
    if (band.DataType != gdal.GDT_Float64 or band.GetNoDataValue() is None
        or not math.isnan(band.GetNoDataValue()) or band.GetOverviewCount() != 1
        or gdal.GetColorInterpretationName(band.GetColorInterpretation()) != 'Gray'):
        raise FixtureError('terrain COG band facts differ')
    for level, current in ((1,band),(2,band.GetOverview(0))):
        wanted = terrain_pixels(case_name,level)
        if (current.XSize,current.YSize) != (expected['width']//level,SIZE//level):
            raise FixtureError('terrain overview grid differs')
        actual = struct.unpack(f'<{len(wanted)}d',current.ReadRaster(buf_type=gdal.GDT_Float64))
        # The oracle uses scalar analytic gradients. GDAL's Float32 angle
        # output is accepted only within this explicit absolute tolerance.
        tolerance = 0 if case_name=='dem-hillshade' else 5e-5
        for position,(value,desired) in enumerate(zip(actual,wanted)):
            if (not math.isnan(value) if desired is None else
                not math.isclose(value,desired,rel_tol=0,abs_tol=tolerance)):
                raise FixtureError(f'terrain {case_name} level {level} pixel {position}: {value} differs from {desired}')
        mask = current.GetMaskBand().ReadRaster(buf_type=gdal.GDT_Byte)
        if any(value != (0 if desired is None else 255) for value,desired in zip(mask,wanted)):
            raise FixtureError('terrain mask differs from independent oracle')
    band = dataset = None
    return {'case_name':case_name,'source_unchanged':True,'cog_valid':True,'has_overviews':True,
            'overview_sizes':[[expected['width']//2,SIZE//2]],'color_interpretations':['Gray'],
            'valid_pixels':expected['valid_pixels'],'invalid_pixels':expected['width']*SIZE-expected['valid_pixels'],
            **physical_file_facts(path)}


@lru_cache(maxsize=4 * len(MOSAIC_CASES))
def mosaic_source_pixels(case_name, band=1, level=1):
    if case_name not in MOSAIC_CASES:
        raise FixtureError('unknown independent-source mosaic case')
    names = MOSAIC_SOURCE_ORDER if case_name.endswith('-first') else tuple(reversed(MOSAIC_SOURCE_ORDER))
    roles = MOSAIC_SOURCE_ROLES if case_name == MOSAIC_EDITOR_CASES[1] else dict.fromkeys(names, 'source')
    return tuple(next((value for name in names
                       if (value := mosaic_source_value(name, band, row, column, roles[name])) is not None), None)
                 for row in range(0, SIZE, level) for column in range(0, SIZE, level))


def mosaic_source_expectation(case_name):
    counts = [sum(value is not None for value in mosaic_source_pixels(case_name, band)) for band in (1, 2)]
    return {**artifact_expectation(True), 'band_count': 2, 'valid_pixels': counts[0], 'band_valid_pixels': counts}


def footprint_pixels(case_name):
    """Independent source cell indices: separate missing bands and transparent cell."""
    if case_name not in FOOTPRINT_CASES:
        raise FixtureError('unknown footprint case')
    if case_name == 'footprint-holes':
        # The accepted outside-clip COG contains a hole and its valid central island.
        yield from (value is not None for value in outside_pixels('clip-outside'))
        return
    if case_name in TOPOLOGY_BANDS:
        for row in range(SIZE):
            for column in range(SIZE):
                yield (64 <= row < 68 and row == column if case_name == 'footprint-diagonal' else
                       64 <= row < 68 and 64 <= column < 68 and (row, column) not in ((65, 65), (66, 66)))
        return
    invalid_cells = {'footprint-any': (2,), 'footprint-all': (0, 1, 2), 'footprint-band-2': (1, 2), 'footprint-rotated': (2,)}[case_name]
    for row in range(SIZE):
        for column in range(SIZE):
            cell = (row // 2) * (SIZE // 2) + column // 2
            yield cell not in invalid_cells


def outside_pixels(case_name, band=1, level=1):
    """Independent row/column coverage: outer rectangle minus its central hole."""
    if case_name not in OUTSIDE_CASES:
        raise FixtureError('unknown outside clipping case')
    for row in range(0, SIZE, level):
        for column in range(0, SIZE, level):
            removed = 64 <= row < 192 and 64 <= column < 192 and not (
                96 <= row < 160 and 96 <= column < 160)
            valid = row * SIZE + column != 0 and not removed
            yield (255. if valid else 0.) if band == 3 else float(band * (row * SIZE + column + 1)) if valid else None


def outside_expectation(case_name):
    if case_name not in OUTSIDE_CASES:
        raise FixtureError('unknown outside clipping case')
    return {**artifact_expectation(), 'band_count': 3, 'valid_pixels': SIZE * SIZE - 1 - (128 * 128 - 64 * 64)}


def update_pixels(case_name, band=1, level=1):
    """Independent base/patch integer coverage; invalid patch cells keep the base."""
    if case_name not in UPDATE_CASES or band not in (1,2):
        raise FixtureError('unknown update case or data band')
    for row in range(0,SIZE,level):
        for column in range(0,SIZE,level):
            position=row*SIZE+column
            cell=(row//2)*(SIZE//2)+column//2
            if row<128 and column<128 and cell not in (band-1,2):
                yield 0. if cell==4 else float(band*(cell+1))
            else:
                yield None if position==0 else float(band*(position+1))


def update_expectation(case_name):
    if case_name not in UPDATE_CASES: raise FixtureError('unknown update case')
    return {**artifact_expectation(), 'band_count':2, 'band_valid_pixels':[65535,65536]}


def computed_expectation(case_name):
    if case_name in MOSAIC_CASES:
        return mosaic_source_expectation(case_name)
    if case_name in UPDATE_CASES:
        return update_expectation(case_name)
    if case_name in OUTSIDE_CASES:
        return outside_expectation(case_name)
    if case_name in AGGREGATE_CASES:
        return aggregate_expectation(case_name)
    if case_name in RECLASS_CASES:
        return reclass_expectation(case_name)
    if case_name in FOUNDATION_CASES:
        return foundation_expectation(case_name)
    return multiband_expectation(case_name) if case_name in MULTIBAND_CASES else grid_expectation(case_name)


class FixtureError(RuntimeError):
    pass


def command(args, *, timeout=300, **kwargs):
    result = subprocess.run(args, capture_output=True, text=True, timeout=timeout, **kwargs)
    if result.returncode:
        # Docker/GDAL/S3 exceptions can embed passwords or signed URLs.
        raise FixtureError(f'physical fixture command failed: {args[0]} ({result.returncode})')
    return result.stdout.strip()


def private_json(path, payload):
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w') as stream:
        json.dump(payload, stream)


def environment():
    if any(os.environ.get(key) != value for key, value in {
        'GITHUB_ACTIONS': 'true', 'RUNNER_OS': 'Linux', 'ADDP_ONLINE_HOST': '1', 'ADDP_ONLINE_HOSTED': '1'
    }.items()) or os.uname().sysname != 'Linux' or os.uname().machine != 'x86_64':
        raise FixtureError('raster fixture requires GitHub Hosted Linux x86_64')
    root = Path(os.environ['ADDP_ONLINE_SECRET_DIR']).resolve()
    runner = Path(os.environ['RUNNER_TEMP']).resolve()
    if root.parent != runner or not root.name.startswith('addp-online-secret-'):
        raise FixtureError('fixture secrets must use the dedicated runner.temp directory')
    if not root.is_dir() or stat.S_IMODE(root.stat().st_mode) & 0o077:
        raise FixtureError('fixture secret directory must be owner-only')
    return root


def owned(name, suite=SUITE):
    result = subprocess.run(['docker', 'container', 'inspect', name], capture_output=True, text=True, timeout=30)
    if result.returncode:
        # Distinguish absent containers from an unavailable Docker daemon.
        command(['docker', 'info', '--format', '{{.ServerVersion}}'])
        return False
    payload = json.loads(result.stdout)[0]
    if payload.get('Config', {}).get('Labels', {}).get('com.addp.online-fixture') != suite:
        raise FixtureError(f'refusing container owned by another task: {name}')
    return True


def stop():
    errors = []
    for name in reversed(CONTAINERS):
        try:
            if owned(name):
                command(['docker', 'rm', '-fv', name])
            if owned(name):
                raise FixtureError(f'container cleanup has residuals: {name}')
        except (FixtureError, subprocess.SubprocessError) as error:
            errors.append(str(error))
    if errors:
        raise FixtureError('; '.join(errors))


def cleanup_proxy_identity():
    # Producer and host reader must share the UID for owner-only event files.
    # Use the socket's group so the proxy can reach the owned Runtime without root.
    return f'{os.getuid()}:{os.stat("/var/run/docker.sock").st_gid}'


def start_minio(root, entry, name, suite, *, data_directory=None):
    """Shared disposable MinIO startup; large capacity data belongs on owned disk."""
    env_path = root / f'{entry["bucket"]}.env'
    with os.fdopen(os.open(env_path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w') as stream:
        stream.write(f'MINIO_ROOT_USER={entry["access_key"]}\nMINIO_ROOT_PASSWORD={entry["secret_key"]}\n')
    mount = ['--mount', 'type=tmpfs,destination=/data']
    if data_directory is not None:
        data_directory.mkdir(mode=0o700)
        mount = ['--user', f'{os.getuid()}:{os.getgid()}', '--mount', f'type=bind,source={data_directory},destination=/data']
    command(['docker', 'run', '-d', '--name', name,
             '--label', f'com.addp.online-fixture={suite}',
             '--label', f'com.addp.online-run-id={os.environ["ADDP_ONLINE_TEST_RUN_ID"]}',
             '--env-file', str(env_path), *mount, '-p', entry['endpoint'] + ':9000', MINIO_IMAGE, 'server', '/data'])
    deadline = time.monotonic() + 60
    while True:
        try:
            with urllib.request.urlopen(f'http://{entry["endpoint"]}/minio/health/ready', timeout=3) as response:
                if response.status == 200:
                    return
        except OSError:
            pass
        if time.monotonic() >= deadline:
            raise FixtureError('MinIO readiness timed out')
        time.sleep(1)


def start(root):
    for name in CONTAINERS:
        if owned(name):
            raise FixtureError('refusing to reuse an existing raster fixture')
    config = {}
    for role, (_, port) in MINIO_ROLES.items():
        config[role] = {
            'endpoint': f'127.0.0.1:{port}', 'access_key': f'raster-{role}',
            'secret_key': secrets.token_hex(32), 'bucket': f'raster-{role}',
        }
    config['target']['writer'] = {'access_key': 'online-raster-writer', 'secret_key': secrets.token_hex(32)}
    private_json(root / 'raster-fixture.json', config)
    try:
        for role, (container, _) in MINIO_ROLES.items():
            entry = config[role]
            start_minio(root, entry, container, SUITE)
            private_json(root / f'raster-{role}-engine.json', {
                'name': f'Hosted Raster {role} {os.environ["ADDP_ONLINE_TEST_RUN_ID"]}', 'description': 'Disposable raster T4 physical fixture',
                'engine_type': 'minio', 'engine_origin': 'general',
                'connection_info': {'endpoint': '127.0.0.1:59014' if role == 'target' else entry['endpoint'],
                    **(entry['writer'] if role == 'target' else {key: entry[key] for key in ('access_key', 'secret_key')}), 'use_ssl': False},
            })
        command(['docker', 'run', '-d', '--name', CONTAINERS[3], '--user', cleanup_proxy_identity(),
                 '--label', f'com.addp.online-fixture={SUITE}',
                 '--label', f'com.addp.online-run-id={os.environ["ADDP_ONLINE_TEST_RUN_ID"]}',
                 '--network', 'host', '-v', f'{Path(__file__).resolve()}:/fixture.py:ro',
                 '-v', f'{root}:/secrets', '-v', '/var/run/docker.sock:/var/run/docker.sock',
                 'localhost:5001/python:3.11-slim', 'python', '/fixture.py', 'worker-cleanup-proxy', '/secrets'])
        deadline = time.monotonic() + 60
        while True:
            try:
                with urllib.request.urlopen('http://127.0.0.1:59014/minio/health/ready', timeout=3) as response:
                    if response.status == 200: break
            except OSError: pass
            if time.monotonic() >= deadline: raise FixtureError('cleanup proxy readiness timed out')
            time.sleep(1)
    except BaseException:
        stop()
        raise


# Runs only inside the owned Runtime, with the actual PUT bytes as its oracle.
# Keep the COG intact: publish_target_file still reads its size after fput_object.
CLEANUP_OBSTACLE_SCRIPT = r"""
import hashlib, json, re, sys
from pathlib import Path
root, digest, size = Path(sys.argv[1]), sys.argv[2], int(sys.argv[3])
if not root.is_dir() or not re.fullmatch(r'[0-9a-f]{64}', digest) or not 0 < size <= 32 * 1024 * 1024:
    raise RuntimeError('invalid cleanup fixture input')
matches = []
for workspace in root.glob('addp-raster-*'):
    if workspace.is_symlink() or not workspace.is_dir(): continue
    for path in workspace.glob('*.tif'):
        if path.is_symlink() or not path.is_file() or path.stat().st_size != size: continue
        if hashlib.sha256(path.read_bytes()).hexdigest() == digest: matches.append(path)
if len(matches) != 1: raise RuntimeError('cleanup fixture needs exactly one matching COG')
path = matches[0]
Path(str(path) + '.msk').mkdir()  # Path.unlink must fail after publication is recorded.
print(json.dumps({'workspace': str(path.parent), 'size_bytes': size, 'sha256': digest, 'obstacle_created': True}))
"""


class DockerConnection(http.client.HTTPConnection):
    def __init__(self):
        super().__init__('localhost', timeout=30)

    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.settimeout(self.timeout)
        self.sock.connect('/var/run/docker.sock')


def docker_request(method, path, payload=None):
    connection = DockerConnection()
    try:
        connection.request(method, path, json.dumps(payload) if payload is not None else None,
                           {'Content-Type': 'application/json'})
        response = connection.getresponse()
        data = response.read()
        if response.status not in (200, 201): raise FixtureError('cleanup fixture Docker request failed')
        return data
    finally:
        connection.close()


def runtime_exec(arguments):
    name = 'addp-raster-online-runtime'
    container = json.loads(docker_request('GET', f'/containers/{name}/json'))
    if (container.get('Config', {}).get('Labels', {}).get('com.addp.online-runtime') != SUITE
        or container.get('State', {}).get('Running') is not True):
        raise FixtureError('cleanup fixture refuses an unowned or stopped Runtime')
    execution = json.loads(docker_request('POST', f'/containers/{name}/exec', {
        'AttachStdout': True, 'AttachStderr': True, 'Cmd': ['python', '-c', *arguments]}))['Id']
    output = docker_request('POST', f'/exec/{execution}/start', {'Detach': False, 'Tty': False})
    status = json.loads(docker_request('GET', f'/exec/{execution}/json'))
    if status.get('ExitCode') != 0 or status.get('Running') is not False:
        raise FixtureError('cleanup fixture Runtime command failed')
    stdout = bytearray()
    while output:
        if len(output) < 8: raise FixtureError('invalid Docker output framing')
        stream, length = output[0], int.from_bytes(output[4:8], 'big')
        if stream not in (1, 2) or len(output) < 8 + length: raise FixtureError('invalid Docker output framing')
        if stream == 1: stdout.extend(output[8:8+length])
        output = output[8+length:]
    return json.loads(stdout)


def cleanup_obstacle(root, prefix, key, body, backend_status):
    event = runtime_exec([CLEANUP_OBSTACLE_SCRIPT, '/tmp', hashlib.sha256(body).hexdigest(), str(len(body))])
    if (event.get('sha256') != hashlib.sha256(body).hexdigest() or event.get('size_bytes') != len(body)
        or event.get('obstacle_created') is not True):
        raise FixtureError('cleanup obstacle evidence differs from confirmed upload')
    private_json(root / (prefix + '-event.json'), {**event, 'prefix': prefix, 'tile_key': key,
                                                'backend_status': backend_status})


def cleanup_proxy(root, address=('127.0.0.1', 59014), inject=cleanup_obstacle):
    config = json.loads((root / 'raster-fixture.json').read_text())
    endpoint = config['target']['endpoint']
    triggers = {'/' + config['target']['bucket'] + '/' + prefix + '/' + key + '.tif': (prefix, key)
                for prefix, (key, _) in TILE_CLEANUP_CASES.items()}
    lock = threading.Lock()
    fired = set()
    hop_headers = {'connection', 'keep-alive', 'proxy-authenticate', 'proxy-authorization', 'te', 'trailer', 'transfer-encoding', 'upgrade'}

    class Handler(http.server.BaseHTTPRequestHandler):
        protocol_version = 'HTTP/1.1'

        def log_message(self, *args): pass  # S3 diagnostics/headers are private.

        def forward(self):
            connection = http.client.HTTPConnection(endpoint, timeout=60)
            try:
                if self.headers.get('Transfer-Encoding'): raise FixtureError('chunked fixture request is unsupported')
                length = int(self.headers.get('Content-Length', '0'))
                if not 0 <= length <= 32 * 1024 * 1024: raise FixtureError('fixture request body exceeds bound')
                body = self.rfile.read(length)
                if len(body) != length: raise FixtureError('incomplete fixture request body')
                headers = {key: value for key, value in self.headers.items() if key.lower() not in hop_headers}
                # Preserve the signed Host and body exactly while changing the TCP destination.
                connection.request(self.command, self.path, body, headers)
                response = connection.getresponse()
                data = response.read()
                if self.command == 'PUT' and self.path in triggers and 200 <= response.status < 300:
                    with lock:
                        if self.path in fired: raise FixtureError('cleanup trigger repeated')
                        prefix, key = triggers[self.path]
                        inject(root, prefix, key, body, response.status)
                        fired.add(self.path)
                self.send_response_only(response.status)
                for key, value in response.getheaders():
                    if key.lower() not in hop_headers | {'content-length'}: self.send_header(key, value)
                self.send_header('Content-Length', str(int(response.getheader('Content-Length', '0')) if self.command == 'HEAD' else len(data)))
                self.end_headers()
                self.wfile.write(data)
            except Exception:
                # Never return native error text, paths, credentials, or a false storage confirmation.
                self.send_response_only(502)
                self.send_header('Content-Length', '0')
                self.send_header('Connection', 'close')
                self.end_headers()
                self.close_connection = True
            finally:
                connection.close()

        do_GET = do_HEAD = do_PUT = do_POST = do_DELETE = forward

    return http.server.ThreadingHTTPServer(address, Handler)


def cleanup_evidence(root, prefix):
    try:
        event = json.loads((root / (prefix + '-event.json')).read_text())
    except PermissionError as error:
        raise FixtureError('cleanup_event_permission_denied') from error
    except FileNotFoundError as error:
        raise FixtureError('cleanup_event_missing') from error
    workspace = event.pop('workspace')
    if (Path(workspace).parent != Path('/tmp') or not Path(workspace).name.startswith('addp-raster-')
        or event.get('prefix') != prefix or event.get('tile_key') != TILE_CLEANUP_CASES[prefix][0]
        or event.get('backend_status') not in (200, 201) or event.get('obstacle_created') is not True):
        raise FixtureError('cleanup fault evidence is invalid')
    result = json.loads(command(['docker', 'exec', 'addp-raster-online-runtime', 'python', '-c',
        "import json,sys; from pathlib import Path; print(json.dumps({'workspace_removed': not Path(sys.argv[1]).exists()}))", workspace]))
    if result.get('workspace_removed') is not True: raise FixtureError('failed execution workspace has residuals')
    return {**event, **result}


def physical_worker(action, root):
    image = os.environ.get('ADDP_ONLINE_RASTER_RUNTIME_IMAGE', '').strip()
    if not image:
        raise FixtureError('product Runtime image is required for raster physical verification')
    try:
        result = json.loads(command([
            'docker', 'run', '--rm', '--name', CONTAINERS[2], '--label', f'com.addp.online-fixture={SUITE}',
            '--network', 'host', '--entrypoint', 'python',
            '-v', f'{Path(__file__).resolve()}:/fixture.py:ro', '-v', f'{root}:/secrets:ro',
            image, '/fixture.py', 'worker-' + action, '/secrets/raster-fixture.json',
        ]))
    except FixtureError as error:
        raise FixtureError('physical_worker_failed') from error
    if action == 'seed':
        private_json(root / 'raster-source-sha256.json', result['source_sha256'])
    if action == 'prepare-terrain':
        private_json(root / 'raster-terrain-baseline.json', result['preserved_sha256'])
    if action in CLEANUP_ACTIONS:
        result['cleanup_fault'] = cleanup_evidence(root, action.removeprefix('verify-'))
    return result


def provision_target_writer(entry, root):
    """Use non-root writes so an exact object Deny is effective in Hosted T4."""
    from minio import Minio
    from minio.credentials import StaticProvider
    from minio.error import S3Error
    from minio.minioadmin import MinioAdmin
    writer = entry['writer']
    if writer['access_key'] == entry['access_key']:
        raise FixtureError('target writer must not be MinIO root')
    denied = TILE_FAILURE_PREFIX + '/' + TILE_FAILURE_KEY + '.tif'
    policy = {'Version': '2012-10-17', 'Statement': [
        {'Effect': 'Allow', 'Action': ['s3:ListAllMyBuckets'], 'Resource': ['arn:aws:s3:::*']},
        {'Effect': 'Allow', 'Action': ['s3:*'], 'Resource': [
            'arn:aws:s3:::' + entry['bucket'], 'arn:aws:s3:::' + entry['bucket'] + '/*']},
        {'Effect': 'Deny', 'Action': ['s3:PutObject'], 'Resource': ['arn:aws:s3:::' + entry['bucket'] + '/' + denied]},
    ]}
    policy_file = root / 'target-writer-policy.json'
    policy_file.write_text(json.dumps(policy))
    admin = MinioAdmin(entry['endpoint'], StaticProvider(entry['access_key'], entry['secret_key']), secure=False)
    admin.user_add(writer['access_key'], writer['secret_key'])
    admin.policy_add('raster-fixture-writer', str(policy_file))
    admin.policy_set('raster-fixture-writer', user=writer['access_key'])
    client = Minio(entry['endpoint'], **writer, secure=False)
    probe = root / 'write-probe'
    probe.write_bytes(b'fixture write permission probe')
    client.fput_object(entry['bucket'], '_writer_probe', str(probe))
    client.remove_object(entry['bucket'], '_writer_probe')
    try:
        client.fput_object(entry['bucket'], denied, str(probe))
    except S3Error as error:
        if error.code != 'AccessDenied':
            raise FixtureError('target failure probe did not return AccessDenied') from None
    else:
        raise FixtureError('target failure probe unexpectedly wrote the denied tile')
    if list(client.list_objects(entry['bucket'], recursive=True)):
        raise FixtureError('target permission probe left residual objects')
    return {'writer_is_root': False, 'denied_object': denied, 'error_code': 'AccessDenied'}


def source_pixels(name, band, spatial, source_nodata, role='source'):
    if name in MOSAIC_SOURCE_WINDOWS:
        start, width, _ = MOSAIC_SOURCE_WINDOWS[name]
        return tuple(source_nodata if (value := mosaic_source_value(name, band, row, column, role)) is None else value
                     for row in range(SIZE) for column in range(start, start + width))
    if name == TOPOLOGY_SOURCE:
        case = next(case for case, index in TOPOLOGY_BANDS.items() if index == band)
        return tuple(0. if valid else source_nodata for valid in footprint_pixels(case))
    if name.startswith('multiband'):
        formula = average_pixels if name.startswith('multiband-average') else multiband_pixels
        return tuple(source_nodata if value is None else value for value in formula(band, source=True))
    return source_values(band, spatial)


def write_source_raster(path, name, spatial, source_nodata, role='source'):
    from osgeo import gdal, osr
    multiband = name.startswith('multiband')
    band_count = 3 if multiband else 2
    width, height, transform, srid = source_grid(name, spatial)
    dataset = gdal.GetDriverByName('GTiff').Create(str(path), width, height, band_count, gdal.GDT_Float64)
    dataset.SetGeoTransform(transform)
    crs = osr.SpatialReference(); crs.ImportFromEPSG(srid)
    dataset.SetProjection(crs.ExportToWkt())
    for index in range(1, band_count + 1):
        values = source_pixels(name, index, spatial, source_nodata, role)
        band = dataset.GetRasterBand(index)
        band.SetNoDataValue(source_nodata)
        if multiband and index == 3: band.SetColorInterpretation(gdal.GCI_AlphaBand)
        band.WriteRaster(0, 0, width, height, struct.pack(f'<{len(values)}d', *values), buf_type=gdal.GDT_Float64)
        band = None
    dataset = None
    return crs



def tile_expectation(key):
    x, y, width, height = TILE_WINDOWS[key]
    transform = [110+x*.01, .01, 0, 20.32-y*.01, 0, -.01]
    return {'width': width, 'height': height, 'band_count': 2, 'band_nodata': -9999.,
            'source_crs': 'EPSG:4326', 'extent_srid': 4326, 'transform': transform,
            'extent': [transform[0], transform[3]-height*.01, transform[0]+width*.01, transform[3]],
            'valid_pixels': width*height-(key == 'r0_c0')}


def verify_tile_file(path, key, compression='DEFLATE'):
    """Independent pixel/validity oracle for the four fixed Hosted windows."""
    from osgeo import gdal, osr
    from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
    dataset = gdal.Open(str(path))
    warnings, errors, _ = validate(dataset, full_check=True)
    expected = tile_expectation(key)
    x, y, width, height = TILE_WINDOWS[key]
    reference = osr.SpatialReference(); reference.ImportFromEPSG(4326)
    if (errors or dataset.GetMetadataItem('LAYOUT', 'IMAGE_STRUCTURE') != 'COG'
        or (dataset.RasterXSize, dataset.RasterYSize, dataset.RasterCount) != (width, height, 2)
        or not osr.SpatialReference(dataset.GetProjection()).IsSame(reference)
        or any(not math.isclose(a, b, rel_tol=0, abs_tol=1e-12) for a, b in zip(dataset.GetGeoTransform(), expected['transform']))
        or any(Path(file).resolve() != Path(path).resolve() for file in dataset.GetFileList())):
        raise FixtureError('tile layout, window, georeferencing or self-contained structure differs')
    levels = [[width//2, height//2]] if max(width, height) > 128 else []
    for index in (1, 2):
        band = dataset.GetRasterBand(index)
        pixels = tuple(-9999. if row == column == 0 else float(index*(row*256+column+1))
            for row in range(y, y+height) for column in range(x, x+width))
        actual = struct.unpack(f'<{width*height}d', band.ReadRaster(buf_type=gdal.GDT_Float64))
        mask = band.GetMaskBand().ReadRaster(buf_type=gdal.GDT_Byte)
        if (band.DataType != gdal.GDT_Float64 or band.GetNoDataValue() != -9999.
            or gdal.GetColorInterpretationName(band.GetColorInterpretation()) != ('Gray' if index == 1 else 'Undefined')
            or (dataset.GetMetadataItem('COMPRESSION', 'IMAGE_STRUCTURE') or 'NONE') != compression
            or actual != pixels or any(value != (0 if wanted == -9999. else 255) for value, wanted in zip(mask, pixels))
            or [[band.GetOverview(i).XSize, band.GetOverview(i).YSize] for i in range(band.GetOverviewCount())] != levels):
            raise FixtureError('tile pixels, dtype, validity, colour or overview grid differs')
    return {'cog_valid': True, 'cog_warnings': len(warnings), 'has_overviews': bool(levels),
            'overview_sizes': levels, 'valid_pixels': expected['valid_pixels'],
            'source_unchanged': True, 'compression': compression, 'size_bytes': Path(path).stat().st_size,
            'sha256': hashlib.sha256(Path(path).read_bytes()).hexdigest()}

def physical_file_facts(path):
    return {'size_bytes': path.stat().st_size, 'sha256': hashlib.sha256(path.read_bytes()).hexdigest()}


def worker(action, path):
    # GDAL stays in the Runtime image; the verifier never imports production operators.
    from minio import Minio
    from osgeo import gdal, osr
    from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
    if action not in ACTIONS:
        raise FixtureError('unknown worker action')
    gdal.UseExceptions()
    config = json.loads(Path(path).read_text())
    fingerprint_path = Path(path).with_name('raster-source-sha256.json')
    clients = {role: Minio(entry['endpoint'], access_key=entry['access_key'], secret_key=entry['secret_key'], secure=False)
               for role, entry in config.items()}
    with tempfile.TemporaryDirectory(prefix='raster-physical-') as directory:
        root = Path(directory)
        if action == 'seed':
            for role, client in clients.items():
                client.make_bucket(config[role]['bucket'])
            writer_probe = provision_target_writer(config['target'], root)
            fingerprints = {}
            for role, files in SOURCE_FILES_BY_ROLE.items():
                (root / role).mkdir()
                for name, spatial, source_nodata in files:
                    path = root / role / name
                    write_source_raster(path, name, spatial, source_nodata, role)
                    clients[role].fput_object(config[role]['bucket'], name, str(path), content_type='image/tiff')
                    fingerprints[role + '/' + name] = hashlib.sha256(path.read_bytes()).hexdigest()
            # A 256px striped TIFF can satisfy the GDAL layout validator. This
            # larger strip has a width > 1024 and is definitively non-tiled.
            path = root / NON_COG_SOURCE
            dataset = gdal.GetDriverByName('GTiff').Create(str(path), 1025, 1025, 1, gdal.GDT_Byte)
            dataset.SetGeoTransform(TRANSFORM)
            crs = osr.SpatialReference(); crs.ImportFromEPSG(4326)
            dataset.SetProjection(crs.ExportToWkt())
            dataset.GetRasterBand(1).Fill(7)
            dataset = None
            clients['source'].fput_object(config['source']['bucket'], NON_COG_SOURCE, str(path), content_type='image/tiff')
            fingerprints['source/' + NON_COG_SOURCE] = hashlib.sha256(path.read_bytes()).hexdigest()
            return {'seeded': True, 'source_pixels': SIZE * SIZE, 'spatial_source_pixels': SIZE * SIZE, 'target_objects': 0, 'source_sha256': fingerprints, 'writer_probe': writer_probe}
        if not fingerprint_path.is_file():
            raise FixtureError('source fingerprints are missing')
        fingerprints = json.loads(fingerprint_path.read_text())
        for role, files in SOURCE_FILES_BY_ROLE.items():
            (root / role).mkdir()
            for name, spatial, source_nodata in files:
                path = root / role / name
                clients[role].fget_object(config[role]['bucket'], name, str(path))
                if hashlib.sha256(path.read_bytes()).hexdigest() != fingerprints.get(role + '/' + name):
                    raise FixtureError('workflow modified source bytes')
                source = gdal.Open(str(path))
                width, height, transform, srid = source_grid(name, spatial)
                multiband = name.startswith('multiband')
                for index in range(1, 4 if multiband else 3):
                    band = source.GetRasterBand(index)
                    values = struct.unpack(f'<{width * height}d', band.ReadRaster(buf_type=gdal.GDT_Float64))
                    expected_values = source_pixels(name, index, spatial, source_nodata, role)
                    nodata = band.GetNoDataValue()
                    if (nodata is None or (not math.isnan(nodata) if math.isnan(source_nodata) else nodata != source_nodata)
                        or any(not math.isnan(actual) if math.isnan(expected) else actual != expected
                               for actual, expected in zip(values, expected_values))
                        or (multiband and index == 3 and band.GetColorInterpretation() != gdal.GCI_AlphaBand)):
                        raise FixtureError('workflow modified its source pixels/NoData')
                reference = osr.SpatialReference(); reference.ImportFromEPSG(srid)
                if (source.GetGeoTransform() != transform or (source.RasterXSize, source.RasterYSize) != (width, height)
                    or source.RasterCount != (3 if multiband else 2) or not osr.SpatialReference(source.GetProjection()).IsSame(reference)):
                    raise FixtureError('workflow modified source georeferencing/bands')
        spatial = action.startswith('verify-mosaic-')
        overlap = action.removeprefix('verify-mosaic-') if spatial else None
        target_name = f'mosaic-{overlap}.cog.tif' if spatial else 'result.cog.tif'

        if action == 'prepare-terrain' or action.startswith('verify-dem-'):
            names = set(item.object_name for item in clients['target'].list_objects(config['target']['bucket'],recursive=True))
            baseline_names = preterrain_target_names()
            baseline = {} if action == 'prepare-terrain' else json.loads(fingerprint_path.with_name('raster-terrain-baseline.json').read_text())
            if action != 'prepare-terrain' and set(baseline) != baseline_names:
                raise FixtureError('terrain baseline does not bind all prior accepted targets')
            prior = {}
            for name in baseline_names:
                path = root/'preserved'/name; path.parent.mkdir(parents=True,exist_ok=True)
                clients['target'].fget_object(config['target']['bucket'],name,str(path))
                prior[name] = hashlib.sha256(path.read_bytes()).hexdigest()
            if action != 'prepare-terrain' and prior != baseline:
                raise FixtureError('terrain changed an earlier accepted target')
            case_name = action.removeprefix('verify-')
            count = len(TERRAIN_CASES) if case_name=='dem-contour' else TERRAIN_CASES.index(case_name)+1 if action!='prepare-terrain' else 0
            expected_names = baseline_names | {case+'.cog.tif' for case in TERRAIN_CASES[:count]}
            if names != expected_names:
                raise FixtureError('terrain target set contains missing or unexpected artifacts')
            terrain = {}
            for case in TERRAIN_CASES[:count]:
                name=case+'.cog.tif';path=root/name
                clients['target'].fget_object(config['target']['bucket'],name,str(path))
                terrain[case]=verify_terrain_file(path,case)
            return {'source_unchanged':True,'preserved_sha256':prior,'terrain':terrain}

        def verify(name, spatial=False, overlap=None, factor=1, grid_case=None, utility_case=None):
            path = root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            clients['target'].fget_object(config['target']['bucket'], name, str(path))
            dataset = gdal.Open(str(path))
            warnings, errors, _ = validate(dataset, full_check=True)
            if errors or dataset.GetMetadataItem('LAYOUT', 'IMAGE_STRUCTURE') != 'COG':
                raise FixtureError('persisted target is not a valid COG')
            expectation = utility_expectation() if utility_case else computed_expectation(grid_case) if grid_case else artifact_expectation(spatial)
            reference = osr.SpatialReference(); reference.ImportFromEPSG(expectation['extent_srid'])
            if (not osr.SpatialReference(dataset.GetProjection()).IsSame(reference)
                or any(not math.isclose(actual, expected, rel_tol=0, abs_tol=1e-12) for actual, expected in
                       zip(dataset.GetGeoTransform(), expectation['transform']))):
                raise FixtureError('persisted target changed CRS/transform')
            if (dataset.RasterXSize, dataset.RasterYSize, dataset.RasterCount) != (expectation['width'], expectation['height'], expectation['band_count']):
                raise FixtureError('persisted target has invalid dimensions/bands')
            band = dataset.GetRasterBand(1)
            expected_nodata = expectation['band_nodata']
            if (band.DataType != gdal.GDT_Float64 or band.GetNoDataValue() is None
                or (not math.isnan(band.GetNoDataValue()) if expected_nodata is None else band.GetNoDataValue() != expected_nodata)):
                raise FixtureError('persisted target did not propagate NoData')
            count = expectation['width'] * expectation['height']
            values = struct.unpack(f'<{count}d', band.ReadRaster(buf_type=gdal.GDT_Float64))
            if utility_case:
                levels = (2, 4) if utility_case == 'build-overviews' else (2,)
                for index in (1, 2):
                    current = dataset.GetRasterBand(index)
                    if current.DataType != gdal.GDT_Float64 or current.GetNoDataValue() != -9999.:
                        raise FixtureError('utility target lost band dtype/finite NoData')
                    actual = struct.unpack(f'<{count}d', current.ReadRaster(buf_type=gdal.GDT_Float64))
                    if actual != source_values(index):
                        raise FixtureError('utility target changed source band pixels')
                    mask = current.GetMaskBand().ReadRaster(buf_type=gdal.GDT_Byte)
                    if any(value != (0 if position == 0 else 255) for position, value in enumerate(mask)):
                        raise FixtureError('utility target changed source validity')
                    if current.GetOverviewCount() != len(levels):
                        raise FixtureError('utility target lost explicit overview levels')
                    for level_index, level in enumerate(levels):
                        overview = current.GetOverview(level_index)
                        if (overview.XSize, overview.YSize) != (SIZE // level, SIZE // level):
                            raise FixtureError('utility overview dimensions differ from the explicit grid')
                        expected = tuple(-9999. if row == column == 0 else float(index * (row * SIZE + column + 1))
                            for row in range(0, SIZE, level) for column in range(0, SIZE, level))
                        actual = struct.unpack(f'<{len(expected)}d', overview.ReadRaster(buf_type=gdal.GDT_Float64))
                        if actual != expected:
                            raise FixtureError('utility overview pixels differ from independent nearest sampling')
                        mask = overview.GetMaskBand().ReadRaster(buf_type=gdal.GDT_Byte)
                        if any(value != (0 if position == 0 else 255) for position, value in enumerate(mask)):
                            raise FixtureError('utility overview lost NoData validity')
                return {'cog_valid': True, 'cog_warnings': len(warnings), 'has_overviews': True,
                    'valid_pixels': SIZE * SIZE - 1, 'band_valid_pixels': [SIZE * SIZE - 1] * 2,
                    'source_unchanged': True,
                    'overview_sizes': [[SIZE // level] * 2 for level in levels], 'crs': dataset.GetProjection(),
                    **physical_file_facts(path)}
            if grid_case in FOUNDATION_CASES + RECLASS_CASES + AGGREGATE_CASES + OUTSIDE_CASES + UPDATE_CASES + MOSAIC_CASES:
                colors = ['Gray', 'Undefined'] if grid_case in UPDATE_CASES + AGGREGATE_MULTIBAND_CASES + MOSAIC_CASES else ['Gray', 'Undefined', 'Alpha'] if grid_case in OUTSIDE_CASES else ['Red', 'Green', 'Blue'] if grid_case == 'multiraster-rgb' else ['Gray']
                label = 'outside clipping' if grid_case in OUTSIDE_CASES else 'aggregation' if grid_case in AGGREGATE_CASES else 'reclassification' if grid_case in RECLASS_CASES else 'multi-raster'
                counts = []
                for index in range(1, dataset.RasterCount + 1):
                    current = dataset.GetRasterBand(index)
                    if (current.DataType != gdal.GDT_Float64 or current.GetNoDataValue() is None
                        or not math.isnan(current.GetNoDataValue())
                        or gdal.GetColorInterpretationName(current.GetColorInterpretation()) != colors[index - 1]
                        or current.GetOverviewCount() != 1):
                        raise FixtureError(f'{label} target lost dtype/NaN/colour/overview')
                    for level, sampled in ((1, current), (2, current.GetOverview(0))):
                        if (sampled.XSize, sampled.YSize) != (expectation['width'] // level, expectation['height'] // level):
                            raise FixtureError(f'{label} overview grid differs from nearest oracle')
                        pixels = mosaic_source_pixels if grid_case in MOSAIC_CASES else update_pixels if grid_case in UPDATE_CASES else outside_pixels if grid_case in OUTSIDE_CASES else aggregate_pixels if grid_case in AGGREGATE_CASES else reclass_pixels if grid_case in RECLASS_CASES else foundation_pixels
                        expected = list(pixels(grid_case, index, level))
                        actual = struct.unpack(f'<{len(expected)}d', sampled.ReadRaster(buf_type=gdal.GDT_Float64))
                        if any(not math.isnan(value) if wanted is None else
                               not math.isclose(value, wanted, rel_tol=1e-12, abs_tol=1e-9) if grid_case in AGGREGATE_CASES and aggregate_config(grid_case)['method'] == 'mean' else value != wanted
                               for value, wanted in zip(actual, expected)):
                            raise FixtureError(f'{label} pixels/NaN differ from independent oracle')
                        mask = sampled.GetMaskBand().ReadRaster(buf_type=gdal.GDT_Byte)
                        if any(value != (0 if wanted is None else 255) for value, wanted in zip(mask, expected)):
                            raise FixtureError(f'{label} mask differs from independent oracle')
                    if grid_case not in OUTSIDE_CASES or index != 3:
                        counts.append(expectation.get('band_valid_pixels', [expectation['valid_pixels']]*dataset.RasterCount)[index-1])
                return {'cog_valid': True, 'cog_warnings': len(warnings), 'has_overviews': True,
                    'valid_pixels': counts[0], 'invalid_pixels': expectation['width'] * expectation['height'] - counts[0], 'band_valid_pixels': counts,
                    'color_interpretations': colors, 'overview_sizes': [[expectation['width']//2, expectation['height']//2]],
                    'source_unchanged': True,
                    **physical_file_facts(path)}
            if grid_case in MULTIBAND_CASES:
                joint = grid_case.endswith('-joint')
                average = 'average' in grid_case
                bilinear = 'bilinear' in grid_case
                pixel_formula = bilinear_pixels if bilinear else average_pixels if average else multiband_pixels
                valid_counts = []
                partial_alpha_pixels = 0
                for index in range(1, dataset.RasterCount + 1):
                    current = dataset.GetRasterBand(index)
                    nodata = current.GetNoDataValue()
                    if (current.DataType != gdal.GDT_Float64 or nodata is None
                        or (not math.isnan(nodata) if expected_nodata is None else nodata != expected_nodata)):
                        raise FixtureError('multiband target lost band dtype/NoData')
                    actual_values = struct.unpack(f'<{count}d', current.ReadRaster(buf_type=gdal.GDT_Float64))
                    expected_values = list(pixel_formula(index, joint=joint, **({'width': expectation['width']} if average else {})))
                    if any((not math.isnan(actual) if expected_nodata is None else actual != expected_nodata) if expected is None else
                           not math.isclose(actual, expected, rel_tol=1e-10, abs_tol=1e-8) if (average or bilinear) and index != 3 else actual != expected
                           for actual, expected in zip(actual_values, expected_values)):
                        raise FixtureError('multiband target pixels/NoData/alpha differ from independent oracle')
                    if index == 3:
                        if current.GetColorInterpretation() != gdal.GCI_AlphaBand:
                            raise FixtureError('multiband target lost alpha interpretation')
                        partial_alpha_pixels = sum(value == 128 for value in actual_values)
                    else:
                        valid_counts.append(sum(value is not None for value in expected_values))
                return {'cog_valid': True, 'cog_warnings': len(warnings), 'has_overviews': band.GetOverviewCount()>0,
                        'valid_pixels': valid_counts[0], 'invalid_pixels': count - valid_counts[0],
                        'band_valid_pixels': valid_counts, 'partial_alpha_pixels': partial_alpha_pixels,
                        'band_nodata': None if math.isnan(band.GetNoDataValue()) else band.GetNoDataValue(),
                        'band_nodata_is_nan': math.isnan(band.GetNoDataValue()),
                        'source_unchanged': True, **physical_file_facts(path)}
            if grid_case:
                expected_values = list(grid_pixels(grid_case))
                for actual, expected in zip(values, expected_values):
                    if (expected is None and not math.isnan(actual)) or (expected is not None and actual != expected):
                        raise FixtureError('persisted grid target pixels/NoData differ from the independent oracle')
                if grid_case == 'clip-polygon':
                    alpha = dataset.GetRasterBand(2)
                    if alpha.DataType != gdal.GDT_Float64 or alpha.GetColorInterpretation() != gdal.GCI_AlphaBand:
                        raise FixtureError('polygon target lost its alpha band')
                    alpha_values = struct.unpack(f'<{count}d', alpha.ReadRaster(buf_type=gdal.GDT_Float64))
                    if any(actual != (0 if expected is None else 255) for actual, expected in zip(alpha_values, expected_values)):
                        raise FixtureError('polygon alpha differs from boundary/hole/source NoData')
                return {'cog_valid': True, 'cog_warnings': len(warnings), 'has_overviews': band.GetOverviewCount()>0,
                        'valid_pixels': expectation['valid_pixels'],
                        'invalid_pixels': count - expectation['valid_pixels'], 'source_unchanged': True,
                        **physical_file_facts(path)}
            invalid = SPATIAL_NODATA if spatial else {0}
            # Near the equator, the projected pixel centres map strictly inside
            # the same source cells. No Warp/production math is used as an oracle.
            seam = 160 if overlap == 'first' else 96
            for position, value in enumerate(values):
                if position in invalid:
                    if not math.isnan(value):
                        raise FixtureError('persisted target did not propagate NoData')
                else:
                    scale = (3 if position % SIZE >= seam else 1) if spatial else factor
                    if value != float(scale * (position + 1)):
                        raise FixtureError('persisted target pixels do not match the expected expression/mosaic seam')
            return {'cog_valid': True, 'cog_warnings': len(warnings), 'has_overviews': band.GetOverviewCount()>0,
                    'valid_pixels': expectation['valid_pixels'],
                    'invalid_pixels': len(invalid), 'source_unchanged': True,
                    **physical_file_facts(path)}

        if action in ('verify-tiles', 'prepare-tiles-replace', 'verify-tiles-replace', 'verify-tiles-failure') + CLEANUP_ACTIONS:
            preserved = {'result.cog.tif': verify('result.cog.tif', factor=3)['sha256'],
                'mosaic-first.cog.tif': verify('mosaic-first.cog.tif', True, 'first')['sha256'],
                'mosaic-last.cog.tif': verify('mosaic-last.cog.tif', True, 'last')['sha256']}
            for prior in GRID_CASES + MULTIBAND_CASES + FOUNDATION_CASES + RECLASS_CASES + AGGREGATE_CASES + OUTSIDE_CASES + UPDATE_CASES + MOSAIC_CASES:
                preserved[prior+'.cog.tif'] = verify(prior+'.cog.tif', grid_case=prior)['sha256']
            for prior in UTILITY_CASES:
                preserved[prior+'.cog.tif'] = verify(prior+'.cog.tif', utility_case=prior)['sha256']
            tiles = {}
            for key in TILE_WINDOWS:
                name = 'tiles-basic/'+key+'.tif'
                path = root/name; path.parent.mkdir(parents=True, exist_ok=True)
                clients['target'].fget_object(config['target']['bucket'], name, str(path))
                tiles[key] = verify_tile_file(path, key, 'DEFLATE' if action in ('verify-tiles', 'prepare-tiles-replace') else 'NONE')
            names = set(preserved) | {'tiles-basic/'+key+'.tif' for key in TILE_WINDOWS}
            sentinel = root/'keep.txt'
            sentinel_sha = hashlib.sha256(TILE_KEEP_BYTES).hexdigest()
            if action in ('verify-tiles-replace', 'verify-tiles-failure') + CLEANUP_ACTIONS:
                names.add(TILE_KEEP_NAME)
                clients['target'].fget_object(config['target']['bucket'], TILE_KEEP_NAME, str(sentinel))
                if sentinel.read_bytes() != TILE_KEEP_BYTES:
                    raise FixtureError('batch replace modified the unrelated file')
            accepted_tiles = tiles
            preserved_failures = {}
            if action == 'verify-tiles-failure' or action in CLEANUP_ACTIONS:
                prefixes = [(TILE_FAILURE_PREFIX, 1)]
                if action in CLEANUP_ACTIONS:
                    for prefix, (_, count) in TILE_CLEANUP_CASES.items():
                        prefixes.append((prefix, count))
                        if action == 'verify-' + prefix: break
                for prefix, count in prefixes:
                    confirmed = {}
                    for key in list(TILE_WINDOWS)[:count]:
                        name = prefix + '/' + key + '.tif'
                        path = root / (prefix + '-' + key + '.tif')
                        clients['target'].fget_object(config['target']['bucket'], name, str(path))
                        confirmed[key] = verify_tile_file(path, key)
                        names.add(name)
                    if prefix == prefixes[-1][0]: tiles = confirmed
                    else: preserved_failures[prefix] = {key: fact['sha256'] for key, fact in confirmed.items()}
            if set(item.object_name for item in clients['target'].list_objects(config['target']['bucket'], recursive=True)) != names:
                raise FixtureError('tiling target contains unexpected or partial artifacts')
            if action == 'prepare-tiles-replace':
                sentinel.write_bytes(TILE_KEEP_BYTES)
                clients['target'].fput_object(config['target']['bucket'], TILE_KEEP_NAME, str(sentinel))
            return {'tiles': tiles, 'preserved_sha256': preserved, 'source_unchanged': True,
                **({'unrelated_sha256': sentinel_sha} if action != 'verify-tiles' else {}),
                **({'preserved_tiles_sha256': {key: fact['sha256'] for key, fact in accepted_tiles.items()}}
                   if action == 'verify-tiles-failure' or action in CLEANUP_ACTIONS else {}),
                **({'preserved_failure_sha256': preserved_failures} if action in CLEANUP_ACTIONS else {})}

        grid_case = action.removeprefix('verify-')
        if grid_case in UTILITY_CASES or action == 'verify-utility-queries':
            path = root / NON_COG_SOURCE
            clients['source'].fget_object(config['source']['bucket'], NON_COG_SOURCE, str(path))
            non_cog = gdal.Open(str(path))
            _, errors, _ = validate(non_cog, full_check=True)
            if (hashlib.sha256(path.read_bytes()).hexdigest() != fingerprints.get('source/' + NON_COG_SOURCE)
                or not errors or non_cog.GetRasterBand(1).ReadRaster() != bytes([7]) * (1025 * 1025)):
                raise FixtureError('negative COG source changed or no longer has an invalid layout')
            preserved = {'result.cog.tif': verify('result.cog.tif', factor=3)['sha256'],
                'mosaic-first.cog.tif': verify('mosaic-first.cog.tif', True, 'first')['sha256'],
                'mosaic-last.cog.tif': verify('mosaic-last.cog.tif', True, 'last')['sha256']}
            for prior in GRID_CASES + MULTIBAND_CASES:
                preserved[prior + '.cog.tif'] = verify(prior + '.cog.tif', grid_case=prior)['sha256']
            accepted = UTILITY_CASES if action == 'verify-utility-queries' else UTILITY_CASES[:UTILITY_CASES.index(grid_case)]
            for prior in accepted:
                preserved[prior + '.cog.tif'] = verify(prior + '.cog.tif', utility_case=prior)['sha256']
            evidence = verify('build-overviews.cog.tif' if action == 'verify-utility-queries' else grid_case + '.cog.tif',
                utility_case='build-overviews' if action == 'verify-utility-queries' else grid_case)
            evidence.update(case_name=grid_case, preserved_sha256=preserved)
            names = set(preserved) | ({grid_case + '.cog.tif'} if grid_case in UTILITY_CASES else set())
            if set(item.object_name for item in clients['target'].list_objects(config['target']['bucket'], recursive=True)) != names:
                raise FixtureError('utility target contains unexpected or partial artifacts')
            return evidence
        if grid_case in FOUNDATION_CASES + RECLASS_CASES + AGGREGATE_CASES + OUTSIDE_CASES + UPDATE_CASES + MOSAIC_CASES:
            preserved = {'result.cog.tif': verify('result.cog.tif', factor=3)['sha256'],
                'mosaic-first.cog.tif': verify('mosaic-first.cog.tif', True, 'first')['sha256'],
                'mosaic-last.cog.tif': verify('mosaic-last.cog.tif', True, 'last')['sha256']}
            cases = FOUNDATION_CASES + RECLASS_CASES + AGGREGATE_CASES + OUTSIDE_CASES + UPDATE_CASES + MOSAIC_CASES
            for prior in GRID_CASES + MULTIBAND_CASES + cases[:cases.index(grid_case)]:
                preserved[prior + '.cog.tif'] = verify(prior + '.cog.tif', grid_case=prior)['sha256']
            for prior in UTILITY_CASES:
                preserved[prior + '.cog.tif'] = verify(prior + '.cog.tif', utility_case=prior)['sha256']
            evidence = verify(grid_case + '.cog.tif', grid_case=grid_case)
            evidence.update(case_name=grid_case, preserved_sha256=preserved)
            if set(item.object_name for item in clients['target'].list_objects(config['target']['bucket'], recursive=True)) != set(preserved) | {grid_case + '.cog.tif'}:
                raise FixtureError('multi-raster target contains unexpected or partial artifacts')
            return evidence
        if grid_case in GRID_CASES + MULTIBAND_CASES:
            evidence = verify(grid_case + '.cog.tif', grid_case=grid_case)
            preserved = {'result.cog.tif': verify('result.cog.tif', factor=3)['sha256'],
                         'mosaic-first.cog.tif': verify('mosaic-first.cog.tif', True, 'first')['sha256'],
                         'mosaic-last.cog.tif': verify('mosaic-last.cog.tif', True, 'last')['sha256']}
            cases = GRID_CASES + MULTIBAND_CASES
            for prior in cases[:cases.index(grid_case)]:
                preserved[prior + '.cog.tif'] = verify(prior + '.cog.tif', grid_case=prior)['sha256']
            evidence.update(case_name=grid_case, preserved_sha256=preserved)
            names = list(preserved) + [grid_case + '.cog.tif']
            if sorted(item.object_name for item in clients['target'].list_objects(config['target']['bucket'], recursive=True)) != sorted(names):
                raise FixtureError('target contains unexpected or partial artifacts')
            return evidence

        evidence = verify(target_name, spatial, overlap, 3 if action in ('verify-replace', 'verify-analysis') else 1)
        names = ['result.cog.tif']
        if spatial:
            names.append('mosaic-first.cog.tif')
            evidence['baseline_sha256'] = verify('result.cog.tif', factor=3)['sha256']
            evidence['overlap'] = overlap
            if overlap == 'last':
                names.append('mosaic-last.cog.tif')
                evidence['first_sha256'] = verify('mosaic-first.cog.tif', True, 'first')['sha256']
        else:
            evidence['factor'] = 3 if action in ('verify-replace', 'verify-analysis') else 1
        if action == 'verify-analysis':
            # Analysis must leave all already accepted artifacts byte-for-byte intact.
            names.extend(['mosaic-first.cog.tif', 'mosaic-last.cog.tif'])
            evidence['first_sha256'] = verify('mosaic-first.cog.tif', True, 'first')['sha256']
            evidence['last_sha256'] = verify('mosaic-last.cog.tif', True, 'last')['sha256']
        if sorted(item.object_name for item in clients['target'].list_objects(config['target']['bucket'], recursive=True)) != sorted(names):
            raise FixtureError('target contains unexpected or partial artifacts')
        return evidence


def manager_physical(action, request=None):
    """Use the product GDAL image against this Hosted suite's owned MinIO objects."""
    if (os.environ.get('ADDP_ONLINE_HOSTED') != '1'
            or os.environ.get('ONLINE_SUITE') != 'manager-internal-artifact-lineage'):
        raise FixtureError('Manager raster physical verification requires its Hosted owner')
    if action not in ('seed', 'verify', 'deleted', 'pdf-verify', 'pdf-deleted', 'img-source', 'jpeg2000-source'):
        raise FixtureError('unknown Manager physical action')
    root = environment()
    image = os.environ.get('ADDP_ONLINE_RASTER_RUNTIME_IMAGE', '')
    if not image:
        raise FixtureError('product Runtime image is required for Manager raster verification')
    config_path = root / 'manager-raster-fixture.json'
    if action == 'seed':
        private_json(config_path, {
            'source': {'endpoint': '127.0.0.1:' + os.environ['ADDP_ONLINE_MANAGER_MINIO_PORT'],
                'access_key': os.environ['ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY'],
                'secret_key': os.environ['ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY'],
                'bucket': os.environ['ADDP_ONLINE_MANAGER_MINIO_BUCKET'],
                'object': os.environ['ADDP_ONLINE_MANAGER_MINIO_RASTER_OBJECT']},
            'pptx': {'endpoint': '127.0.0.1:' + os.environ['ADDP_ONLINE_MANAGER_MINIO_PORT'],
                'access_key': os.environ['ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY'],
                'secret_key': os.environ['ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY'],
                'bucket': os.environ['ADDP_ONLINE_MANAGER_MINIO_BUCKET'],
                'object': os.environ['ADDP_ONLINE_MANAGER_MINIO_PPTX_OBJECT'],
                'sha256': hashlib.sha256((Path(__file__).resolve().parents[1] / 'fixtures/manager/addp_online_preview_fixture.pptx').read_bytes()).hexdigest()},
            'infra': {'endpoint': '127.0.0.1:' + os.environ['MINIO_API_PORT'],
                'access_key': os.environ['MINIO_ROOT_USER'], 'secret_key': os.environ['MINIO_ROOT_PASSWORD']},
        })
    request_file = root / 'manager-raster-request.json'
    request_file.unlink(missing_ok=True)
    private_json(request_file, request or {})
    result = json.loads(command(['docker', 'run', '--rm', '--name', 'addp-manager-raster-verifier',
        '--label', 'com.addp.online-fixture=manager-internal-artifact-lineage', '--network', 'host',
        '--entrypoint', 'python', '-v', f'{Path(__file__).resolve()}:/fixture.py:ro',
        '-v', f'{root}:/secrets:ro', image, '/fixture.py', 'manager-worker-' + action, '/secrets/manager-raster-fixture.json']))
    if action == 'seed':
        private_json(root / 'manager-raster-source.json', result)
    return result


def manager_pdf_worker(action, path):
    """Observe PDF cleanup before disposable Infra teardown; never delete objects."""
    from minio import Minio
    from minio.error import S3Error
    from urllib.parse import urlsplit, parse_qs, unquote
    root = Path(path).parent
    config = json.loads(Path(path).read_text())
    request = json.loads((root / 'manager-raster-request.json').read_text())
    parsed = urlsplit(request['locator'])
    prefix = f"/manager/tenant_{request['tenant_id']}/document-preview/{request['fingerprint']}/"
    if (parsed.scheme != 'addp-infra' or parsed.netloc != 'minio'
            or not parsed.path.startswith(prefix) or not parsed.path.endswith('.pdf')
            or parse_qs(parsed.query) != {'type': ['object']}):
        raise FixtureError('Manager PDF output is not owned by this run')
    key = unquote(parsed.path.removeprefix('/manager/'))
    if any(part in ('', '.', '..') for part in key.split('/')):
        raise FixtureError('Manager PDF output contains unsafe segments')
    clients = {role: Minio(config[role]['endpoint'], access_key=config[role]['access_key'],
        secret_key=config[role]['secret_key'], secure=False) for role in ('pptx', 'infra')}
    source = config['pptx']
    stream = clients['pptx'].get_object(source['bucket'], source['object'])
    try:
        source_hash = hashlib.sha256(stream.read()).hexdigest()
    finally:
        stream.close(); stream.release_conn()
    if source_hash != source['sha256']:
        raise FixtureError('Manager source PPTX changed')
    if action == 'pdf-deleted':
        try:
            clients['infra'].stat_object('manager', key)
        except S3Error as error:
            if error.code != 'NoSuchKey': raise
        else:
            raise FixtureError('deleted Manager PDF still exists physically')
        if list(clients['infra'].list_objects('manager', prefix=key, recursive=True)):
            raise FixtureError('Manager PDF cleanup left residual objects')
        return {'object_deleted': True, 'source_unchanged': True, 'residual_objects': 0}
    if action != 'pdf-verify':
        raise FixtureError('unknown Manager PDF physical action')
    size = clients['infra'].stat_object('manager', key).size
    stream = clients['infra'].get_object('manager', key)
    try:
        header = stream.read(4)
    finally:
        stream.close(); stream.release_conn()
    if size != request['size_bytes'] or header != b'%PDF':
        raise FixtureError('Manager PDF physical size or header mismatch')
    return {'object_present': True, 'source_unchanged': True, 'source_sha256': source_hash,
            'size_bytes': size}


MANAGER_IMG_CASES = (
    ('wgs84', 4326, (113., .0001, 0., 28., 0., -.0001)),
    ('utm50n', 32650, (500000., 10., 0., 3100000., 0., -10.)),
    ('unlocated', None, None),
)


MANAGER_JPEG2000_CASES = (*MANAGER_IMG_CASES, ('codestream', None, None))

def manager_img_pixels(band):
    colors = ((255, 0, 255), (0, 255, 255), (255, 255, 0), (32, 32, 64))
    return bytes(colors[(x // 32 + y // 32) % 4][band - 1] for y in range(256) for x in range(256))


def manager_seed_imgs(client, bucket, directory, source_format='img'):
    from osgeo import gdal, osr
    sources = []
    cases = MANAGER_IMG_CASES if source_format == 'img' else MANAGER_JPEG2000_CASES
    for case, srid, transform in cases:
        extension = 'img' if source_format == 'img' else ('j2k' if case == 'codestream' else 'jp2')
        path = Path(directory) / f'{source_format}-{case}.{extension}'
        dataset = (gdal.GetDriverByName('HFA').Create(str(path),256,256,3,gdal.GDT_Byte,['COMPRESSED=YES'])
                   if source_format == 'img' else gdal.GetDriverByName('MEM').Create('',256,256,3,gdal.GDT_Byte))
        if srid:
            crs = osr.SpatialReference(); crs.ImportFromEPSG(srid)
            dataset.SetSpatialRef(crs); dataset.SetGeoTransform(transform)
        for band in range(1, 4):
            dataset.GetRasterBand(band).WriteRaster(0, 0, 256, 256, manager_img_pixels(band))
        if source_format == 'jpeg2000':
            options=['CODEC='+('J2K' if case=='codestream' else 'JP2'),'REVERSIBLE=YES','QUALITY=100','RESOLUTIONS=3',
                     'GeoJP2='+('YES' if case=='wgs84' else 'NO'),'GMLJP2='+('YES' if case=='utm50n' else 'NO')]
            dataset=gdal.GetDriverByName('JP2OpenJPEG').CreateCopy(str(path),dataset,options=options)
        dataset.FlushCache()
        if dataset.GetFileList() != [str(path)]:
            raise FixtureError('Manager IMG source must be self-contained')
        dataset = None
        key = f'raster/{source_format}-{case}.{extension}'
        client.fput_object(bucket, key, str(path), content_type='application/octet-stream')
        sources.append({'case': case, 'object': key, 'source_srid': srid,
                        'size_bytes': path.stat().st_size, 'sha256': hashlib.sha256(path.read_bytes()).hexdigest()})
    return sources


def manager_img_worker(action, path):
    from minio import Minio
    from minio.error import S3Error
    from osgeo import gdal, osr
    from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
    from urllib.parse import urlsplit, parse_qs, unquote
    root = Path(path).parent
    config = json.loads(Path(path).read_text())
    source = config['source']
    clients = {role: Minio(entry['endpoint'], access_key=entry['access_key'], secret_key=entry['secret_key'], secure=False)
               for role, entry in config.items()}
    request = {} if action in ('img-source','jpeg2000-source') else json.loads((root / 'manager-raster-request.json').read_text())
    source_format = action.removesuffix('-source') if action in ('img-source','jpeg2000-source') else request.get('source_format', 'img')
    if source_format not in ('img','jpeg2000'): raise FixtureError('unknown raster source format')
    cases = MANAGER_IMG_CASES if source_format == 'img' else MANAGER_JPEG2000_CASES
    seeded = json.loads((root / 'manager-raster-source.json').read_text())[source_format+'_sources']
    if [s['case'] for s in seeded] != [case for case, _, _ in cases]:
        raise FixtureError('Manager IMG source receipt must include all three exact cases')
    with tempfile.TemporaryDirectory(prefix='manager-img-physical-') as directory:
        for facts in seeded:
            extension = 'img' if source_format == 'img' else ('j2k' if facts['case'] == 'codestream' else 'jp2')
            local = Path(directory) / (facts['case'] + '.' + extension)
            if facts['object'] != 'raster/'+source_format+'-' + facts['case'] + '.' + extension:
                raise FixtureError('Manager IMG source object differs from its exact fixture')
            clients['source'].fget_object(source['bucket'], facts['object'], str(local))
            if local.stat().st_size != facts['size_bytes'] or hashlib.sha256(local.read_bytes()).hexdigest() != facts['sha256']:
                raise FixtureError('Manager IMG source changed')
        if action == source_format+'-source':
            return {'source_unchanged': True, source_format+'_sources': seeded}
        request = json.loads((root / 'manager-raster-request.json').read_text())
        matches = [(case, srid, transform) for case, srid, transform in cases if case == request.get('source_case') and srid]
        if len(matches) != 1:
            raise FixtureError('Manager IMG output requires one located source case')
        case, srid, transform = matches[0]
        parsed = urlsplit(request['locator'])
        prefix = f"/manager/tenant_{request['tenant_id']}/cog/{request['fingerprint']}/"
        if (parsed.scheme != 'addp-infra' or parsed.netloc != 'minio' or not parsed.path.startswith(prefix)
                or parse_qs(parsed.query) != {'type': ['object']} or not parsed.path.endswith('.cog.tif')):
            raise FixtureError('Manager IMG output is not owned by this run')
        key = unquote(parsed.path.removeprefix('/manager/'))
        if any(part in ('', '.', '..') for part in key.split('/')):
            raise FixtureError('Manager IMG output contains unsafe segments')
        if action == 'deleted':
            try:
                clients['infra'].stat_object('manager', key)
            except S3Error as error:
                if error.code != 'NoSuchKey': raise
            else:
                raise FixtureError('deleted Manager IMG COG still exists physically')
            if list(clients['infra'].list_objects('manager', prefix=key, recursive=True)):
                raise FixtureError('Manager IMG COG cleanup left residual objects')
            return {'object_deleted': True, 'source_unchanged': True, 'residual_objects': 0}
        if action != 'verify':
            raise FixtureError('unknown Manager IMG physical action')
        target = Path(directory) / 'result.tif'
        clients['infra'].fget_object('manager', key, str(target))
        raw = target.read_bytes()
        if (len(raw) != request['size_bytes'] or clients['infra'].stat_object('manager', key).size != len(raw)
                or hashlib.sha256(raw).hexdigest() != request['sha256']):
            raise FixtureError('Manager IMG COG size or HTTP hash mismatch')
        dataset = gdal.Open(str(target))
        if dataset is None or (dataset.RasterXSize, dataset.RasterYSize, dataset.RasterCount) != (256, 256, 3):
            raise FixtureError('Manager IMG COG dimensions changed')
        crs = osr.SpatialReference(); crs.ImportFromEPSG(srid)
        crs.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER)
        original = gdal.Open(str(Path(directory) / (case + ('.img' if source_format == 'img' else '.jp2'))))
        source_transform = tuple(original.GetGeoTransform())
        if (not original.GetSpatialRef().IsSame(crs)
                or source_transform != transform
                or not dataset.GetSpatialRef().IsSame(original.GetSpatialRef())
                or tuple(dataset.GetGeoTransform()) != source_transform):
            raise FixtureError(f'Manager IMG COG CRS or grid changed: fixture_crs={bool(original.GetSpatialRef().IsSame(crs))}, '
                               f'crs_preserved={bool(dataset.GetSpatialRef().IsSame(original.GetSpatialRef()))}, '
                               f'source_grid={source_transform}, expected_grid={transform}, target_grid={tuple(dataset.GetGeoTransform())}')
        _, errors, _ = validate(dataset, full_check=True)
        if errors or dataset.GetMetadataItem('LAYOUT', 'IMAGE_STRUCTURE') != 'COG':
            raise FixtureError('Manager IMG artifact is not a valid COG')
        for index in range(1, 4):
            band = dataset.GetRasterBand(index)
            if (band.DataType != gdal.GDT_Byte or band.GetNoDataValue() is not None
                    or band.ReadRaster() != manager_img_pixels(index)
                    or band.GetMaskBand().ReadRaster() != bytes([255]) * 65536):
                raise FixtureError('Manager IMG COG pixels, dtype, mask or NoData changed')
        return {'cog_valid': True, 'source_unchanged': True, 'pixels_verified': 196608,
                'size_bytes': len(raw), 'sha256': request['sha256'], 'source_case': case}


def manager_worker(action, path):
    if action.startswith('pdf-'):
        return manager_pdf_worker(action, path)
    request_file = Path(path).parent / 'manager-raster-request.json'
    if action in ('img-source','jpeg2000-source') or (request_file.exists() and json.loads(request_file.read_text()).get('source_case')):
        return manager_img_worker(action, path)
    from minio import Minio
    from minio.error import S3Error
    from osgeo import gdal, osr
    from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
    from urllib.parse import urlsplit, parse_qs, unquote
    gdal.UseExceptions()
    root = Path(path).parent
    config = json.loads(Path(path).read_text())
    clients = {role: Minio(entry['endpoint'], access_key=entry['access_key'], secret_key=entry['secret_key'], secure=False)
               for role, entry in config.items()}
    source = config['source']
    with tempfile.TemporaryDirectory(prefix='manager-cog-physical-') as directory:
        source_path = Path(directory) / 'source.tif'
        if action == 'seed':
            write_source_raster(source_path, 'source.tif', False, -9999.)
            clients['source'].fput_object(source['bucket'], source['object'], str(source_path), content_type='image/tiff')
            return {'sha256': hashlib.sha256(source_path.read_bytes()).hexdigest(), 'size_bytes': source_path.stat().st_size,
                    'img_sources': manager_seed_imgs(clients['source'], source['bucket'], directory),
                    'jpeg2000_sources': manager_seed_imgs(clients['source'], source['bucket'], directory, 'jpeg2000')}
        clients['source'].fget_object(source['bucket'], source['object'], str(source_path))
        seeded = json.loads((root / 'manager-raster-source.json').read_text())
        if hashlib.sha256(source_path.read_bytes()).hexdigest() != seeded['sha256']:
            raise FixtureError('Manager source changed')
        request = json.loads((root / 'manager-raster-request.json').read_text())
        parsed = urlsplit(request['locator'])
        prefix = f"/manager/tenant_{request['tenant_id']}/cog/{request['fingerprint']}/"
        if (parsed.scheme != 'addp-infra' or parsed.netloc != 'minio' or not parsed.path.startswith(prefix)
                or parse_qs(parsed.query) != {'type': ['object']} or not parsed.path.endswith('.cog.tif')):
            raise FixtureError('Manager raster output is not owned by this run')
        key = unquote(parsed.path.removeprefix('/manager/'))
        if any(part in ('', '.', '..') for part in key.split('/')):
            raise FixtureError('Manager output contains unsafe segments')
        if action == 'deleted':
            try:
                clients['infra'].stat_object('manager', key)
            except S3Error as error:
                if error.code != 'NoSuchKey': raise
            else:
                raise FixtureError('deleted Manager COG still exists physically')
            if list(clients['infra'].list_objects('manager', prefix=key, recursive=True)):
                raise FixtureError('Manager COG cleanup left residual objects')
            return {'object_deleted': True, 'source_unchanged': True, 'residual_objects': 0}
        if action != 'verify':
            raise FixtureError('unknown Manager physical action')
        target = Path(directory) / 'result.tif'
        actual_size = clients['infra'].stat_object('manager', key).size
        clients['infra'].fget_object('manager', key, str(target))
        raw = target.read_bytes()
        if actual_size != request['size_bytes'] or len(raw) != actual_size or hashlib.sha256(raw).hexdigest() != request['sha256']:
            raise FixtureError('Manager COG physical size or HTTP hash mismatch')
        dataset = gdal.Open(str(target))
        if dataset is None or (dataset.RasterXSize, dataset.RasterYSize, dataset.RasterCount) != (SIZE, SIZE, 2):
            raise FixtureError('Manager COG dimensions changed')
        crs = osr.SpatialReference(); crs.ImportFromEPSG(4326)
        crs.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER)
        if not dataset.GetSpatialRef().IsSame(crs) or tuple(dataset.GetGeoTransform()) != TRANSFORM:
            raise FixtureError('Manager COG CRS or grid changed')
        _, errors, _ = validate(dataset, full_check=True)
        if errors or dataset.GetMetadataItem('LAYOUT', 'IMAGE_STRUCTURE') != 'COG':
            raise FixtureError('Manager artifact is not a valid COG')
        for index in (1, 2):
            band = dataset.GetRasterBand(index)
            values = struct.unpack(f'<{SIZE * SIZE}d', band.ReadRaster(buf_type=gdal.GDT_Float64))
            mask = band.GetMaskBand().ReadRaster()
            if (band.DataType != gdal.GDT_Float64 or band.GetNoDataValue() != -9999.
                    or values != source_values(index) or mask != bytes([0] + [255] * (SIZE * SIZE - 1))):
                raise FixtureError('Manager COG pixels, dtype or NoData changed')
        return {'cog_valid': True, 'source_unchanged': True, 'pixels_verified': SIZE * SIZE * 2,
                'size_bytes': actual_size, 'sha256': request['sha256']}


def main():
    action = sys.argv[1]
    if action.startswith('manager-worker-'):
        result = manager_worker(action.removeprefix('manager-worker-'), sys.argv[2])
    elif action.startswith('manager-'):
        request = json.loads(Path(sys.argv[2]).read_text()) if len(sys.argv) == 3 else None
        result = manager_physical(action.removeprefix('manager-'), request)
    elif action == 'worker-cleanup-proxy':
        cleanup_proxy(Path(sys.argv[2])).serve_forever()
        return
    elif action.startswith('worker-'):
        if action.removeprefix('worker-') not in ACTIONS:
            raise FixtureError('unknown worker action')
        result = worker(action.removeprefix('worker-'), sys.argv[2])
    else:
        root = environment()
        if action == 'start':
            start(root); result = {'started': True}
        elif action == 'stop':
            stop(); result = {'containers': 0}
        elif action in ACTIONS:
            result = physical_worker(action, root)
        else:
            raise FixtureError('unknown physical fixture action')
    print(json.dumps(result, sort_keys=True))


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        # Never print native SDK/subprocess exception text: it may hold credentials.
        reason = str(error) if isinstance(error, FixtureError) else type(error).__name__
        print(f'Raster physical fixture failed: {reason}', file=sys.stderr)
        raise SystemExit(1)

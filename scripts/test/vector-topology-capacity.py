#!/usr/bin/env python3
"""Manual, bounded Native line-topology capacity; deliberately not business T4."""
from __future__ import annotations

import argparse
import ast
import types
from contextlib import contextmanager
from dataclasses import asdict, dataclass
import hashlib
import json
import math
import os
from pathlib import Path
import shutil
import signal
import statistics
import subprocess
import sys
import tempfile
import threading
import time
import uuid

CPUS = 4
MEMORY = 2 << 30
SECONDS = 180
SCHEMA = 'addp.vector-topology-capacity/v3'
NORMALIZE_SCHEMA = 'addp.vector-topology-capacity/v4'
LEGACY_COMMIT = '8a3006670e7f6cfaddf018253515ccb37ab5bc78'
BATCHED_COMMIT = 'adb605b67676bc4396307bc9ea7449c63de183bc'
GEOMETRY_MODULE = 'engines/geopython-workflow/operators/geometric_operators.py'
COMPARISON_DEFINITIONS = {'_self_overlap_intersections', '_self_overlap_candidate_batches', '_SELF_OVERLAP_PAIR_LIMIT'}


@dataclass(frozen=True)
class Case:
    kind: str
    size: int
    operator: str

    @property
    def key(self):
        return f'{self.kind}-{self.size}-{self.operator}'


def matrix():
    return [Case(kind, size, operator)
            for kind, sizes in [('many', (1000, 10000, 100000)),
                                ('long', (1000, 10000, 100000)),
                                ('dense', (100, 300, 1000))]
            for size in sizes for operator in ('overlap', 'self')
            if kind != 'long' or operator == 'self']


def chain_matrix():
    return [Case(kind, size, 'chains')
            for kind in ('chain-open', 'chain-branch', 'chain-ring', 'chain-rings', 'chain-hub')
            for size in ((128, 512, 2048) if kind == 'chain-hub' else (1000, 10000, 100000))]


def normalize_matrix():
    return [Case(kind, size, 'normalize')
            for kind, sizes in [('normalize-sparse', (1000, 10000, 100000)),
                                ('normalize-duplicates', (128, 512, 2048)),
                                ('normalize-parts', (128, 512, 2048)),
                                ('normalize-grid', (32, 128, 512)),
                                ('normalize-sampling', (32, 128, 512))]
            for size in sizes]


def measurement_plan(profile):
    if profile == 'normalize':
        return [{'id': case.key, 'case': asdict(case), 'observer': 'A', 'repetition': None}
                for case in normalize_matrix()]
    if profile == 'chains':
        return [{'id': case.key, 'case': asdict(case), 'observer': 'A', 'repetition': None}
                for case in chain_matrix()]
    if profile == 'matrix':
        return [{'id': case.key, 'case': asdict(case), 'observer': 'C', 'repetition': None} for case in matrix()]
    if profile == 'comparison':
        return [{'id': f'comparison-{repetition}-{implementation}',
                 'case': asdict(Case('many', 100000, 'self')), 'observer': 'A',
                 'repetition': repetition, 'implementation': implementation}
                for repetition, order in enumerate((('old', 'new'), ('new', 'old')) * 2, 1)
                for implementation in order]
    if profile != 'observation':
        raise ValueError('unknown capacity profile')
    case = asdict(Case('many', 100000, 'self'))
    return [{'id': f'observation-{repetition}-{observer.lower()}', 'case': case.copy(),
             'observer': observer, 'repetition': repetition}
            for repetition, order in enumerate(('ABC', 'BCA', 'CAB'), 1) for observer in order]


def definition_name(node):
    if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
        return node.name
    if isinstance(node, ast.Assign) and len(node.targets) == 1 and isinstance(node.targets[0], ast.Name):
        return node.targets[0].id
    return None


def implementation_identity(source, operator='self'):
    name = 'normalize_line_network' if operator == 'normalize' else 'merge_line_chains' if operator == 'chains' else '_self_overlap_intersections'
    collector = next(n for n in ast.parse(source).body if definition_name(n) == name)
    return {'module_sha256': hashlib.sha256(source.encode()).hexdigest(),
            ('normalize_line_network_sha256' if operator == 'normalize' else 'merge_line_chains_sha256' if operator == 'chains' else 'collector_sha256'):
                hashlib.sha256(ast.get_source_segment(source, collector).encode()).hexdigest()}


def loaded_implementation_identity(module, operator='self'):
    source = Path(module.__file__).read_text()
    code = compile(source, module.__file__, 'exec', dont_inherit=True, optimize=sys.flags.optimize)
    name = 'normalize_line_network' if operator == 'normalize' else 'merge_line_chains' if operator == 'chains' else '_self_overlap_intersections'
    expected = next(c for c in code.co_consts if isinstance(c, types.CodeType) and c.co_name == name)
    # Reject a stale pyc or a monkeypatched function even if the source file is correct.
    assert getattr(module, name).__code__ == expected, ('loaded normalize differs from source' if operator == 'normalize' else 'loaded merge differs from source' if operator == 'chains' else 'loaded collector differs from source')
    return {**implementation_identity(source, operator),
            ('loaded_normalize_matches_source' if operator == 'normalize' else 'loaded_merge_matches_source' if operator == 'chains' else 'loaded_collector_matches_source'): True}


def comparison_variant(current, legacy, batched):
    trees = [ast.parse(text) for text in (current, legacy, batched)]
    blocks = [{definition_name(n): n for n in tree.body if definition_name(n) in COMPARISON_DEFINITIONS} for tree in trees]
    assert set(blocks[0]) == set(blocks[2]) == COMPARISON_DEFINITIONS
    assert set(blocks[1]) == {'_self_overlap_intersections'}
    for name in COMPARISON_DEFINITIONS:
        assert ast.dump(blocks[0][name], include_attributes=False) == ast.dump(blocks[2][name], include_attributes=False), 'current batched implementation drifted'
    assert ast.dump(blocks[0]['_self_overlap_intersections'], include_attributes=False) != ast.dump(blocks[1]['_self_overlap_intersections'], include_attributes=False)
    lines = current.splitlines(keepends=True)
    for name, node in sorted(blocks[0].items(), key=lambda item: item[1].lineno, reverse=True):
        replacement = ast.get_source_segment(legacy, blocks[1][name]) + '\n' if name == '_self_overlap_intersections' else ''
        lines[node.lineno - 1:node.end_lineno] = [replacement]
    old = ''.join(lines)
    def rest(text):
        return ast.dump(ast.Module(body=[n for n in ast.parse(text).body if definition_name(n) not in COMPARISON_DEFINITIONS], type_ignores=[]), include_attributes=False)
    assert rest(old) == rest(current), 'comparison changed unrelated code'
    return {'old': old, 'new': current}


def comparison_sources(repository):
    current = (repository / GEOMETRY_MODULE).read_text()
    legacy = command(['git', '-C', str(repository), 'show', f'{LEGACY_COMMIT}:{GEOMETRY_MODULE}'])
    batched = command(['git', '-C', str(repository), 'show', f'{BATCHED_COMMIT}:{GEOMETRY_MODULE}'])
    variants = comparison_variant(current, legacy, batched)
    manifest = {'legacy_commit': LEGACY_COMMIT, 'batched_commit': BATCHED_COMMIT,
                'legacy_git_module_sha256': hashlib.sha256(legacy.encode()).hexdigest(),
                'only_changed_definitions': sorted(COMPARISON_DEFINITIONS),
                **{name: implementation_identity(text) for name, text in variants.items()}}
    return variants, manifest


def comparison_image_proof(base, old):
    assert base['Id'] != old['Id']
    layers = base['RootFS']['Layers']; old_layers = old['RootFS']['Layers']
    assert old_layers[:-1] == layers and len(old_layers) == len(layers) + 1
    keys = ('Cmd', 'Entrypoint', 'Env', 'WorkingDir', 'User', 'ExposedPorts', 'Volumes', 'StopSignal', 'Healthcheck', 'Shell')
    config = {key: base['Config'].get(key) for key in keys}
    assert config == {key: old['Config'].get(key) for key in keys}
    return {'base_image_id': base['Id'], 'base_layers': layers, 'old_layers': old_layers,
            'execution_config': config, 'images': {'old': old['Id'], 'new': base['Id']}}


def comparison_summary(groups, sources, proof):
    plan = measurement_plan('comparison')
    assert proof['base_image_id'] == proof['images']['new'] and proof['images']['old'] != proof['images']['new']
    assert len(groups) == len(plan)
    for group, item in zip(groups, plan):
        assert all(group[key] == value for key, value in item.items())
        assert group['result'] == group['cleanup'] == 'passed' and group['resource_samples'] > 0
        assert 0 < group['wall_seconds'] <= SECONDS
        assert group['image_id'] == proof['images'][item['implementation']]
        assert group['worker']['implementation_identity'] == {**sources[item['implementation']], 'loaded_collector_matches_source': True}
        validate_worker(group['worker'], Case(**item['case']), 'A', item['id'])
        assert group['resource_after']['cpu_usage_usec'] >= group['resource_before']['cpu_usage_usec']
        for metrics in (group['resource_before'], group['resource_after']):
            assert metrics['memory_limit_bytes'] == MEMORY and metrics['cpu_quota'] == CPUS
            assert metrics['swap_current_bytes'] == metrics['oom'] == metrics['oom_kill'] == 0
            assert 0 < metrics['memory_peak_bytes'] <= MEMORY
    for field in ('source_frame_sha256', 'versions'):
        assert len({json.dumps(g['worker'][field], sort_keys=True) for g in groups}) == 1
    assert len({g['worker']['physical']['full_result_sha256'] for g in groups}) == 1
    summary = {}
    for implementation in ('old', 'new'):
        selected = [g for g in groups if g['implementation'] == implementation]
        values = {'wall_seconds': [g['wall_seconds'] for g in selected],
                  'compute_seconds': [g['worker']['timings']['compute'] for g in selected],
                  'memory_peak_bytes': [g['resource_after']['memory_peak_bytes'] for g in selected],
                  'cpu_seconds': [(g['resource_after']['cpu_usage_usec'] - g['resource_before']['cpu_usage_usec']) / 1e6 for g in selected]}
        summary[implementation] = {key: {'median': statistics.median(v), 'min': min(v), 'max': max(v)} for key, v in values.items()}
    pairs = []
    for repetition in range(1, 5):
        pair = {g['implementation']: g for g in groups if g['repetition'] == repetition}
        old, new = pair['old'], pair['new']; a, b = old['worker']['timings']['compute'], new['worker']['timings']['compute']
        assert a > 0 and b > 0
        pairs.append({'repetition': repetition, 'old_id': old['id'], 'new_id': new['id'],
                      'compute_seconds_saved': a - b, 'compute_speedup': a / b, 'compute_reduction_percent': (a - b) / a * 100,
                      'wall_seconds_saved': old['wall_seconds'] - new['wall_seconds']})
    return {'result': 'passed', 'groups': summary, 'pairs': pairs,
            'paired_medians': {key: statistics.median(p[key] for p in pairs) for key in ('compute_seconds_saved', 'compute_speedup', 'compute_reduction_percent', 'wall_seconds_saved')},
            'interpretation': 'Four balanced interleaved pairs on one Runner/base image, only collector definitions differ; finite-sample workload-specific estimates, not SLA.'}


def observation_summary(groups):
    """Signed small-sample estimates, never a production throughput guarantee."""
    plan = measurement_plan('observation')
    assert len(groups) == len(plan)
    for group, measurement in zip(groups, plan):
        assert all(group[key] == value for key, value in measurement.items())
        assert group['result'] == group['cleanup'] == 'passed'
        assert 0 < group['wall_seconds'] <= SECONDS and group['resource_samples'] > 0
        assert group['image_id']
        before, after = group['resource_before'], group['resource_after']
        assert after['cpu_usage_usec'] >= before['cpu_usage_usec']
        for metrics in (before, after):
            assert metrics['memory_limit_bytes'] == MEMORY and metrics['cpu_quota'] == CPUS
            assert metrics['swap_current_bytes'] == metrics['oom'] == metrics['oom_kill'] == 0
            assert 0 < metrics['memory_peak_bytes'] <= MEMORY
        validate_worker(group['worker'], Case(**measurement['case']), measurement['observer'], measurement['id'])
    assert len({g['image_id'] for g in groups}) == 1
    for field in ('source_frame_sha256', 'versions'):
        assert len({json.dumps(g['worker'][field], sort_keys=True) for g in groups}) == 1
    assert len({g['worker']['physical']['full_result_sha256'] for g in groups}) == 1
    summary = {}
    for observer in 'ABC':
        selected = [g for g in groups if g['observer'] == observer]
        values = {
            'wall_seconds': [g['wall_seconds'] for g in selected],
            'compute_seconds': [g['worker']['timings']['compute'] for g in selected],
            'memory_peak_bytes': [g['resource_after']['memory_peak_bytes'] for g in selected],
            'cpu_seconds': [(g['resource_after']['cpu_usage_usec'] - g['resource_before']['cpu_usage_usec']) / 1e6 for g in selected],
        }
        summary[observer] = {key: {'median': statistics.median(v), 'min': min(v), 'max': max(v)} for key, v in values.items()}
    deltas = {label: {metric: summary[right][metric]['median'] - summary[left][metric]['median']
                      for metric in ('wall_seconds', 'compute_seconds')}
              for label, left, right in [('wrapping_B_minus_A', 'A', 'B'),
                                         ('sampling_writes_C_minus_B', 'B', 'C'), ('internal_C_minus_A', 'A', 'C')]}
    return {'result': 'passed', 'groups': summary, 'median_differences': deltas,
            'interpretation': 'Signed estimates from three interleaved repeats per mode; Runner drift and noise remain; no SLA or causal certainty.'}


def coordinates(case, row):
    if case.kind == 'many':
        y = (row // 2) * 20
        return [[x, y] for x in range(9)] + [[4, y], [0, y]]
    if case.kind == 'long':
        return [[x, 0] for x in range(case.size)] + [[case.size - 2, 0]]
    return [[0, 0], [8, 0]]


def source_rows(case):
    return case.size if case.kind == 'many' or case.operator == 'overlap' else 1


def source_id(index):
    return f'route-{index:09d}'


def expected_rows(case):
    if case.operator == 'self':
        return source_rows(case)
    return case.size // 2 if case.kind == 'many' else case.size * (case.size - 1) // 2


def expected(case):
    """Closed-form identities and coordinates, independent of GEOS intersection."""
    if case.operator == 'self':
        for i in range(source_rows(case)):
            if case.kind == 'many':
                y = (i // 2) * 20
                parts = [[[x, y], [x + 1, y]] for x in range(8)]
            elif case.kind == 'long':
                parts = [[[case.size - 2, 0], [case.size - 1, 0]]]
            else:
                parts = [[[0, 0], [8, 0]]]
            yield {'source_id': source_id(i), 'overlap_length': 1.0 if case.kind == 'long' else 8.0}, parts
    else:
        pairs = ((i, i + 1) for i in range(0, case.size, 2)) if case.kind == 'many' else (
            (i, j) for i in range(case.size) for j in range(i + 1, case.size))
        for i, j in pairs:
            y = (i // 2) * 20 if case.kind == 'many' else 0
            parts = [[[x, y], [x + 1, y]] for x in range(8)] if case.kind == 'many' else [[[0, 0], [8, 0]]]
            yield {'source_id_a': source_id(i), 'source_id_b': source_id(j), 'overlap_length': 8.0}, parts


def canonical(parts):
    return sorted(min(tuple(map(tuple, p)), tuple(map(tuple, reversed(p)))) for p in parts)


def digest(path):
    checksum = hashlib.sha256()
    with path.open('rb') as stream:
        for block in iter(lambda: stream.read(1 << 20), b''):
            checksum.update(block)
    return {'size_bytes': path.stat().st_size, 'sha256': checksum.hexdigest()}


def write_json(path, value):
    pending = path.with_suffix('.pending')
    pending.write_text(json.dumps(value, ensure_ascii=False, indent=2))
    pending.replace(path)


def seed(case, path):
    from osgeo import ogr, osr
    ogr.UseExceptions()
    crs = osr.SpatialReference(); crs.ImportFromEPSG(3857)
    ds = ogr.GetDriverByName('GPKG').CreateDataSource(str(path))
    layer = ds.CreateLayer('routes', crs, ogr.wkbUnknown, options=['GEOMETRY_NAME=shape'])
    for name in ('route_key', 'note'):
        layer.CreateField(ogr.FieldDefn(name, ogr.OFTString))
    layer.StartTransaction()
    for i in range(source_rows(case)):
        feature = ogr.Feature(layer.GetLayerDefn())
        feature.SetField('route_key', source_id(i)); feature.SetField('note', f'original-{i}')
        points = coordinates(case, i)
        shape = {'type': 'MultiLineString', 'coordinates': [points] * case.size} if case.kind == 'dense' and case.operator == 'self' else {'type': 'LineString', 'coordinates': points}
        feature.SetGeometry(ogr.CreateGeometryFromJson(json.dumps(shape)))
        if layer.CreateFeature(feature) != 0:
            raise RuntimeError('cannot seed source')
    layer.CommitTransaction(); ds = None


def frame_digest(frame):
    checksum = hashlib.sha256()
    checksum.update(str((frame.columns.tolist(), frame.dtypes.astype(str).tolist(), str(frame.crs))).encode())
    for index, row in frame.iterrows():
        checksum.update(str((index, row['route_key'], row['note'])).encode())
        checksum.update(row[frame.geometry.name].wkb)
    return checksum.hexdigest()


def verify_source(frame, case):
    assert len(frame) == source_rows(case) and frame.crs.to_epsg() == 3857
    for i, row in enumerate(frame.itertuples(index=False, name=None)):
        values = dict(zip(frame.columns, row)); shape = values[frame.geometry.name]
        assert values['route_key'] == source_id(i) and values['note'] == f'original-{i}'
        points = coordinates(case, i)
        if case.kind == 'dense' and case.operator == 'self':
            assert shape.geom_type == 'MultiLineString' and len(shape.geoms) == case.size
            assert all([list(p) for p in part.coords] == points for part in shape.geoms)
        else:
            assert shape.geom_type == 'LineString' and [list(p) for p in shape.coords] == points


def verify_output(case, path):
    from osgeo import ogr
    ds = ogr.Open(str(path), 0); assert ds and ds.GetLayerCount() == 1
    layer = ds.GetLayer(0); assert layer.GetSpatialRef().GetAuthorityCode(None) == '3857'
    fields = ['source_id', 'overlap_length'] if case.operator == 'self' else ['source_id_a', 'source_id_b', 'overlap_length']
    assert [f.GetName() for f in layer.schema] == fields
    assert all(layer.GetLayerDefn().GetFieldDefn(i).GetType() == (ogr.OFTReal if name == 'overlap_length' else ogr.OFTString) for i, name in enumerate(fields))
    assert layer.GetFeatureCount() == expected_rows(case)
    checksum = hashlib.sha256(); iterator = iter(layer)
    for i, (properties, parts) in enumerate(expected(case)):
        feature = next(iterator); shape = feature.GetGeometryRef()
        assert feature.GetFID() == i + 1 and shape.GetGeometryName() == 'MULTILINESTRING'
        assert shape.GetCoordinateDimension() == 2 and shape.IsValid()
        actual = {name: feature.GetField(name) for name in fields}; assert actual == properties
        actual_parts = [[list(p[:2]) for p in shape.GetGeometryRef(j).GetPoints()] for j in range(shape.GetGeometryCount())]
        assert canonical(actual_parts) == canonical(parts)
        length = math.fsum(math.hypot(b[0] - a[0], b[1] - a[1]) for part in actual_parts for a, b in zip(part, part[1:]))
        assert length == properties['overlap_length'] and all(actual[k] != str(feature.GetFID()) for k in fields if k != 'overlap_length')
        checksum.update(json.dumps([actual, canonical(actual_parts)], sort_keys=True).encode())
    assert next(iterator, None) is None
    return {'rows': expected_rows(case), 'full_result_sha256': checksum.hexdigest(), 'all_coordinates_ids_lengths_verified': True, 'crs': 'EPSG:3857'}



def chain_counts(case):
    assert case.operator == 'chains' and type(case.size) is int and case.size > 0
    if case.kind in ('chain-open', 'chain-ring'):
        assert case.size >= 4 and (case.kind != 'chain-ring' or case.size % 4 == 0)
        chains = 1
    elif case.kind == 'chain-rings':
        assert case.size % 4 == 0
        chains = case.size // 4
    elif case.kind == 'chain-branch':
        assert case.size % 10 == 0
        chains = case.size // 2
    else:
        assert case.kind == 'chain-hub' and case.size >= 3
        chains = case.size
    return {'sources': case.size, 'result': chains, 'members': case.size}


def chain_part(case, row):
    """Closed-form source record, independent of production graph operations."""
    properties = {'route_key': f'chain-{row:09d}', 'note': None if row % 7 == 0 else f'original-{row:09d}', 'grade': 7, 'road': 'main'}
    if case.kind in ('chain-open', 'chain-ring'):
        middle = case.size // 2
        edge = middle if row == 0 else 0 if row == middle else row
        if case.kind == 'chain-open':
            points = [[edge, 0], [edge + 1, 0]]
        else:
            length = case.size // 4
            def vertex(i):
                if i <= length: return [i, 0]
                if i <= 2 * length: return [length, i - length]
                if i <= 3 * length: return [3 * length - i, length]
                return [0, 4 * length - i]
            points = [vertex(edge), vertex(edge + 1)]
        if row % 2: points.reverse()
    elif case.kind == 'chain-rings':
        ring, local = divmod(row, 4); edge = (1, 0, 3, 2)[local]
        vertices = [[0,0],[2,0],[2,2],[0,2],[0,0]]
        points = [[x + ring * 4, y] for x, y in vertices[edge:edge + 2]]
        if local % 2: points.reverse()
        properties['road'] = None if ring % 3 == 0 else 'ring'
    elif case.kind == 'chain-branch':
        cell, local = divmod(row, 10)
        shapes = [[[2,0],[30,0]],[[-2,0],[0,0]],[[2,0],[0,0]],[[0,0],[0,2]],[[0,4],[0,2]],
                  [[12,6],[12,8]],[[0,4],[10,6]],[[10,6],[12,6]],[[10,8],[10,6]],[[12,8],[10,8]]]
        points = [[x + cell * 32, y] for x, y in shapes[local]]
        properties['road'] = 'main' if local < 3 else 'branch' if local < 5 else 'tail' if local == 6 else None
        properties['grade'] = 7 + cell
    else:
        assert case.kind == 'chain-hub'
        points = [[0,0],[1,row + 1]]
        if row % 2: points.reverse()
        properties['road'] = (None, 'one', 'two')[row % 3]
    return properties, points


def chain_walks(case):
    """Handwritten membership formulas, never production graph traversal."""
    chain_counts(case)
    if case.kind in ('chain-open', 'chain-ring'):
        middle = case.size // 2
        def members():
            for i in range(case.size):
                edge = i if case.kind == 'chain-open' else (middle + i) % case.size
                row = middle if edge == 0 else 0 if edge == middle else edge
                yield row, bool(row % 2)
        yield members()
    elif case.kind == 'chain-rings':
        for ring in range(case.size // 4):
            yield tuple((ring * 4 + local, reverse) for local, reverse in ((0,False),(3,True),(2,False),(1,True)))
    elif case.kind == 'chain-branch':
        for cell in range(case.size // 10):
            for walk in (((2,True),(0,False)),((1,False),),((3,False),(4,True)),
                         ((7,False),(5,False),(9,False),(8,False)),((6,False),)):
                yield tuple((cell * 10 + local, reverse) for local, reverse in walk)
    else:
        for row in range(case.size): yield ((row, False),)


def chain_expected(case, port):
    assert port in ('result', 'members')
    for chain_id, walk in enumerate(chain_walks(case), 1):
        points, count, common = [], 0, None
        for ordinal, (row, reverse) in enumerate(walk, 1):
            properties, original = chain_part(case, row)
            if port == 'members':
                yield {**properties, 'chain_id': chain_id, 'member_no': ordinal, 'source_part_no': 1, 'is_reversed': reverse}, original
                continue
            if common is None: common = {k: properties[k] for k in ('road','grade')}
            assert common == {k: properties[k] for k in ('road','grade')}
            oriented = original[::-1] if reverse else original
            if points: assert points[-1] == oriented[0]
            points.extend(oriented if not points else oriented[1:]); count += 1
        if port == 'result': yield {**common, 'chain_id': chain_id, 'member_count': count}, points


def seed_chains(case, path):
    from osgeo import ogr, osr
    chain_counts(case); ogr.UseExceptions()
    crs = osr.SpatialReference(); crs.ImportFromEPSG(3857)
    ds = ogr.GetDriverByName('GPKG').CreateDataSource(str(path))
    layer = ds.CreateLayer('chain_inputs', crs, ogr.wkbLineString, options=['GEOMETRY_NAME=chain_shape'])
    for name, kind in (('route_key',ogr.OFTString),('note',ogr.OFTString),('grade',ogr.OFTInteger64),('road',ogr.OFTString)):
        layer.CreateField(ogr.FieldDefn(name, kind))
    layer.StartTransaction()
    for i in range(case.size):
        properties, points = chain_part(case, i); feature = ogr.Feature(layer.GetLayerDefn())
        for name, value in properties.items():
            if value is not None: feature.SetField(name, value)
        shape = ogr.Geometry(ogr.wkbLineString)
        for x, y in points: shape.AddPoint_2D(x, y)
        feature.SetGeometry(shape); assert layer.CreateFeature(feature) == 0
    layer.CommitTransaction(); ds = None


def normalize_counts(case):
    assert case.operator == 'normalize' and type(case.size) is int and case.size > 0
    n = case.size
    if case.kind == 'normalize-sparse': return {'sources': n, 'result': n, 'members': n}
    if case.kind == 'normalize-duplicates': return {'sources': n, 'result': 1, 'members': n}
    if case.kind == 'normalize-parts': return {'sources': 1, 'result': 1, 'members': n}
    if case.kind == 'normalize-grid': return {'sources': 2*n, 'result': 2*n*(n+1), 'members': 2*n*(n+1)}
    assert case.kind == 'normalize-sampling'
    return {'sources': n, 'result': n, 'members': n*n}


def normalize_attributes(row):
    return {'route_key': f'norm-{row:09d}', 'note': None if row % 7 == 0 else f'original-{row:09d}',
            'grade': None if row % 5 == 1 else 2**53 + 1 + row % 3,
            'enabled': None if row % 7 == 1 else row % 2 == 0}


def normalize_source(case, row):
    assert 0 <= row < normalize_counts(case)['sources']
    n = case.size; attrs = normalize_attributes(row)
    if case.kind == 'normalize-parts':
        return attrs, [[[0,0],[1,0]] if i % 2 == 0 else [[1,0],[0,0]] for i in range(n)]
    if case.kind == 'normalize-sparse': points = [[row*4,0],[row*4+1,0]]
    elif case.kind == 'normalize-duplicates': points = [[0,0],[1,0]]
    elif case.kind == 'normalize-grid':
        points = [[0,row+1],[n+1,row+1]] if row < n else [[row-n+1,0],[row-n+1,n+1]]
    else:
        assert case.kind == 'normalize-sampling'
        points = [[i,0] for i in range(n+1)] if row == 0 else [[0,0],[n,0]]
    return attrs, [points[::-1] if row % 2 else points]


def normalize_visits(case):
    """Closed-form row/part/segment/piece/edge/orientation; no spatial operations."""
    n = case.size
    for row in range(normalize_counts(case)['sources']):
        if case.kind == 'normalize-parts':
            for part in range(n):
                reverse = bool(part % 2)
                yield row, part+1, 1, 1, 1, reverse, [[1,0],[0,0]] if reverse else [[0,0],[1,0]]
        elif case.kind in ('normalize-sparse', 'normalize-duplicates'):
            points = normalize_source(case,row)[1][0]
            yield row, 1, 1, 1, row+1 if case.kind == 'normalize-sparse' else 1, bool(row % 2), points
        elif case.kind == 'normalize-grid':
            for p in range(n+1):
                a = n-p if row % 2 else p
                points = ([[a,row+1],[a+1,row+1]] if row < n else [[row-n+1,a],[row-n+1,a+1]])
                yield row, 1, 1, p+1, row*(n+1)+p+1, bool(row % 2), points[::-1] if row % 2 else points
        else:
            assert case.kind == 'normalize-sampling'
            for p in range(n):
                edge = n-p if row % 2 else p+1
                points = [[edge-1,0],[edge,0]]
                yield row, 1, p+1 if row == 0 else 1, 1 if row == 0 else p+1, edge, bool(row % 2), points[::-1] if row % 2 else points


def normalize_expected(case, port):
    assert port in ('result','members')
    if port == 'result' and case.kind in ('normalize-duplicates','normalize-parts'):
        yield {'edge_id':1,'occurrence_count':case.size}, [[0,0],[1,0]]
    elif port == 'result' and case.kind == 'normalize-sampling':
        for i in range(case.size): yield {'edge_id':i+1,'occurrence_count':case.size}, [[i,0],[i+1,0]]
    else:
        for row,part,segment,piece,edge,reverse,points in normalize_visits(case):
            if port == 'result':
                yield {'edge_id':edge,'occurrence_count':1}, points[::-1] if reverse else points
            else:
                attrs = normalize_attributes(row)
                yield {**attrs,'edge_id':edge,'source_id':attrs['route_key'],'source_part_no':part,
                       'source_segment_no':segment,'piece_no':piece,'is_reversed':reverse}, points


def seed_normalize(case, path):
    from osgeo import ogr, osr
    ogr.UseExceptions(); crs = osr.SpatialReference(); crs.ImportFromEPSG(3857)
    ds = ogr.GetDriverByName('GPKG').CreateDataSource(str(path))
    layer = ds.CreateLayer('normalize_inputs',crs,ogr.wkbMultiLineString if case.kind == 'normalize-parts' else ogr.wkbLineString,
                           options=['GEOMETRY_NAME=network_shape'])
    for name,kind in [('route_key',ogr.OFTString),('note',ogr.OFTString),('grade',ogr.OFTInteger64),('enabled',ogr.OFTInteger)]:
        field = ogr.FieldDefn(name,kind)
        if name == 'enabled': field.SetSubType(ogr.OFSTBoolean)
        assert layer.CreateField(field) == 0
    layer.StartTransaction()
    for row in range(normalize_counts(case)['sources']):
        attrs,parts = normalize_source(case,row); feature = ogr.Feature(layer.GetLayerDefn())
        for name,value in attrs.items():
            if value is not None: feature.SetField(name,value)
        shape = ogr.Geometry(ogr.wkbMultiLineString) if case.kind == 'normalize-parts' else None
        for points in parts:
            line = ogr.Geometry(ogr.wkbLineString)
            for x,y in points: line.AddPoint_2D(x,y)
            if shape is None: shape = line
            else: shape.AddGeometry(line)
        feature.SetGeometry(shape); assert layer.CreateFeature(feature) == 0
    layer.CommitTransaction(); ds = None


def verify_normalize_source(frame, case):
    import pandas as pd
    assert len(frame) == normalize_counts(case)['sources'] and frame.crs.to_epsg() == 3857
    assert list(frame.columns) == ['route_key','note','grade','enabled',frame.geometry.name]
    assert str(frame['grade'].dtype) == 'Int64' and str(frame['enabled'].dtype) == 'boolean'
    assert isinstance(frame.index,pd.RangeIndex) and frame.index.equals(pd.RangeIndex(len(frame))) and frame.index.name is None
    for i,record in enumerate(frame.itertuples(index=False,name=None)):
        expected,parts = normalize_source(case,i)
        actual = {key: None if value is pd.NA or value is None else value.item() if hasattr(value,'item') else value
                  for key,value in zip(frame.columns[:-1],record[:-1])}
        assert actual == expected
        shape = record[-1]; assert shape.is_valid and not shape.has_z
        if case.kind == 'normalize-parts':
            assert shape.geom_type == 'MultiLineString'; actual_parts = [[list(p) for p in part.coords] for part in shape.geoms]
        else:
            assert shape.geom_type == 'LineString'; actual_parts = [[list(p) for p in shape.coords]]
        assert actual_parts == parts


def verify_normalize_output(case, path, port):
    from osgeo import ogr
    ds = ogr.Open(str(path),0); assert ds and ds.GetLayerCount() == 1
    layer = ds.GetLayer(0); assert layer.GetSpatialRef().GetAuthorityCode(None) == '3857'
    fields = ['edge_id','occurrence_count'] if port == 'result' else ['route_key','note','grade','enabled','edge_id','source_id','source_part_no','source_segment_no','piece_no','is_reversed']
    assert [f.GetName() for f in layer.schema] == fields
    for field in layer.schema:
        name = field.GetName(); native = ogr.OFTString if name in ('route_key','note','source_id') else ogr.OFTInteger if name in ('enabled','is_reversed') else ogr.OFTInteger64
        assert field.GetType() == native
        if name in ('enabled','is_reversed'): assert field.GetSubType() == ogr.OFSTBoolean
    rows = normalize_counts(case)[port]; assert layer.GetFeatureCount() == rows
    iterator = iter(layer); checksum = hashlib.sha256()
    for i,(properties,points) in enumerate(normalize_expected(case,port),1):
        feature = next(iterator); shape = feature.GetGeometryRef()
        assert feature.GetFID() == i and shape.GetGeometryName() == 'LINESTRING' and shape.GetCoordinateDimension() == 2 and not shape.IsMeasured() and shape.IsValid()
        actual = {name:feature.GetField(name) for name in fields}
        for name in ('enabled','is_reversed'):
            if name in actual and actual[name] is not None:
                assert type(actual[name]) in (int,bool) and actual[name] in (0,1); actual[name] = bool(actual[name])
        coordinates = [list(v) for v in shape.GetPoints()]
        assert actual == properties and coordinates == points
        for key,value in properties.items(): assert type(actual[key]) is type(value)
        checksum.update(json.dumps([actual,coordinates],sort_keys=True).encode())
    assert next(iterator,None) is None
    return {'rows':rows,'crs':'EPSG:3857','full_result_sha256':checksum.hexdigest(),'all_coordinates_attributes_membership_verified':True}


def attributed_frame_digest(frame):
    checksum = hashlib.sha256()
    checksum.update(str((frame.columns.tolist(), frame.dtypes.astype(str).tolist(), str(frame.crs))).encode())
    checksum.update(str((type(frame.index).__name__, str(frame.index.dtype), frame.index.names)).encode())
    geometry_index = frame.columns.get_loc(frame.geometry.name)
    for record in frame.itertuples(index=True, name=None):
        checksum.update(repr(record[:geometry_index + 1] + record[geometry_index + 2:]).encode())
        checksum.update(record[geometry_index + 1].wkb)
    return checksum.hexdigest()


def verify_chain_source(frame, case):
    import pandas as pd
    assert len(frame) == case.size and frame.crs.to_epsg() == 3857
    assert list(frame.columns) == ['route_key','note','grade','road',frame.geometry.name]
    # The physical fixture declares Integer64; load preserves that schema as
    # nullable Int64 even when this particular batch has no missing values.
    assert str(frame['grade'].dtype) == 'Int64'
    assert isinstance(frame.index, pd.RangeIndex) and frame.index.equals(pd.RangeIndex(case.size))
    assert frame.index.name is None
    for i, record in enumerate(frame.itertuples(index=False, name=None)):
        properties, points = chain_part(case, i)
        assert dict(zip(frame.columns[:-1], record[:-1])) == properties
        shape = record[-1]
        assert shape.geom_type == 'LineString' and shape.is_valid and shape.has_z is False
        assert [list(p) for p in shape.coords] == points


def verify_chain_output(case, path, port):
    from osgeo import ogr
    ds = ogr.Open(str(path), 0); assert ds and ds.GetLayerCount() == 1
    layer = ds.GetLayer(0); assert layer.GetSpatialRef().GetAuthorityCode(None) == '3857'
    fields = (['road','grade','chain_id','member_count'] if port == 'result' else
              ['route_key','note','grade','road','chain_id','member_no','source_part_no','is_reversed'])
    assert [field.GetName() for field in layer.schema] == fields
    integers = {'grade','chain_id','member_count','member_no','source_part_no'}
    for field in layer.schema:
        assert field.GetType() == (ogr.OFTInteger64 if field.GetName() in integers else ogr.OFTInteger if field.GetName() == 'is_reversed' else ogr.OFTString)
        if field.GetName() == 'is_reversed': assert field.GetSubType() == ogr.OFSTBoolean
    rows = chain_counts(case)[port]; assert layer.GetFeatureCount() == rows
    iterator = iter(layer); checksum = hashlib.sha256()
    for i, (properties, points) in enumerate(chain_expected(case, port), 1):
        feature = next(iterator); shape = feature.GetGeometryRef()
        assert feature.GetFID() == i and shape.GetGeometryName() == 'LINESTRING'
        assert shape.GetCoordinateDimension() == 2 and not shape.IsMeasured() and shape.IsValid()
        actual = {name: feature.GetField(name) for name in fields}
        if port == 'members':
            assert actual['is_reversed'] in (0, 1)
            actual['is_reversed'] = bool(actual['is_reversed'])
        coordinates = [list(p[:2]) for p in shape.GetPoints()]
        assert actual == properties and coordinates == points
        checksum.update(json.dumps([actual, coordinates], sort_keys=True).encode())
    assert next(iterator, None) is None
    return {'rows': rows, 'crs': 'EPSG:3857', 'full_result_sha256': checksum.hexdigest(), 'all_coordinates_attributes_membership_verified': True}


def run_dual_worker(case, root, observer, measurement_id):
    import resource
    sys.path.insert(0, '/app')
    from operators.io_operators import load, save
    from operators import geometric_operators
    assert observer == 'A' and case.operator in ('chains','normalize')
    normal = case.operator == 'normalize'
    counts = normalize_counts(case) if normal else chain_counts(case)
    started = time.monotonic()
    report = {'implementation_identity': loaded_implementation_identity(geometric_operators, case.operator), 'case': asdict(case),
              'observer': observer, 'measurement_id': measurement_id or case.key, 'result': 'incomplete', 'timings': {}}
    progress = root / 'progress.json'
    def phase(name, operation):
        report['stage'] = name; write_json(progress, report); before = time.monotonic()
        value = operation(); report['timings'][name] = time.monotonic() - before
        write_json(progress, report); return value
    source = root / 'source.gpkg'
    phase('generate', lambda: (seed_normalize if normal else seed_chains)(case, source)); original = digest(source)
    connection = {'engine_type':'nfs', 'mount_path':str(root)}
    frame = phase('read', lambda: load(connection_info=connection, path='source.gpkg'))
    phase('source_oracle', lambda: (verify_normalize_source if normal else verify_chain_source)(frame, case)); before = attributed_frame_digest(frame)
    result = phase('compute', lambda: geometric_operators.normalize_line_network(frame, 'route_key') if normal else geometric_operators.merge_line_chains(frame, 'route_key', ['road','grade']))
    assert set(result) == {'result','members'}
    for port in ('result','members'):
        assert len(result[port]) == counts[port]
        inherited = ([] if normal else ['road', 'grade']) if port == 'result' else frame.columns
        assert all(result[port][name].dtype == frame[name].dtype for name in inherited)
        phase('save_' + port, lambda port=port: save(result[port], connection_info=connection, path=port + '.gpkg', mode='fail'))
    physical = phase('reload_oracle', lambda: {port: (verify_normalize_output if normal else verify_chain_output)(case, root / (port + '.gpkg'), port) for port in ('result','members')})
    def unchanged():
        assert attributed_frame_digest(frame) == before and digest(source) == original
    phase('source_unchanged', unchanged)
    import platform, geopandas, shapely
    from osgeo import gdal
    report.update(result='passed', stage='complete', source_frame_sha256=before, source=original,
        outputs={port:digest(root / (port + '.gpkg')) for port in ('result','members')}, physical=physical,
        duration_seconds=time.monotonic() - started,
        worker_peak_rss_bytes=resource.getrusage(resource.RUSAGE_SELF).ru_maxrss * (1 if sys.platform == 'darwin' else 1024),
        versions={'python':platform.python_version(),'machine':platform.machine(),'geopandas':geopandas.__version__,
                  'shapely':shapely.__version__,'geos':shapely.geos_version_string,'gdal':gdal.VersionInfo('RELEASE_NAME')})
    write_json(progress, report); return report


def validate_dual_worker(report, case, observer, measurement_id):
    normal = case.operator == 'normalize'
    counts = normalize_counts(case) if normal else chain_counts(case)
    assert observer == report['observer'] == 'A' and report['measurement_id'] == (measurement_id or case.key)
    assert report['result'] == 'passed' and report['stage'] == 'complete' and report['case'] == asdict(case)
    assert math.isfinite(report['duration_seconds']) and 0 < report['duration_seconds'] <= SECONDS
    assert report['worker_peak_rss_bytes'] > 0 and len(report['source_frame_sha256']) == 64
    assert set(report['timings']) == {'generate','read','source_oracle','compute','save_result','save_members','reload_oracle','source_unchanged'}
    assert all(math.isfinite(v) and v >= 0 for v in report['timings'].values())
    assert sum(report['timings'].values()) <= report['duration_seconds']
    assert 'compute_observation' not in report
    identity = report['implementation_identity']; assert identity['loaded_normalize_matches_source' if normal else 'loaded_merge_matches_source'] is True
    for key in ('module_sha256','normalize_line_network_sha256' if normal else 'merge_line_chains_sha256'): assert len(identity[key]) == 64
    assert set(report['physical']) == set(report['outputs']) == {'result','members'}
    for port in ('result','members'):
        physical = report['physical'][port]
        assert physical['rows'] == counts[port] and physical['crs'] == 'EPSG:3857'
        assert physical['all_coordinates_attributes_membership_verified'] is True and len(physical['full_result_sha256']) == 64
    for fact in (report['source'], *report['outputs'].values()):
        assert type(fact['size_bytes']) is int and fact['size_bytes'] > 0
        assert len(fact['sha256']) == 64 and all(c in '0123456789abcdef' for c in fact['sha256'])


@contextmanager
def observe_compute(root, report, progress, observer='C'):
    """Observe the unchanged production calls; never replace their geometry work."""
    if observer not in 'ABC' or len(observer) != 1:
        raise ValueError('unknown observer')
    from operators import geometric_operators as operators
    original_collect = operators._self_overlap_intersections
    original_union = operators.shapely.union_all
    observation = {'observer': observer, 'wrapping_enabled': observer != 'A', 'sampling_enabled': observer == 'C',
                   'collection_seconds': 0.0, 'fusion_seconds': 0.0,
                   'collection_calls': 0, 'fusion_calls': 0, 'fusion_input_fragments': 0,
                   'sampling_interval_seconds': .25, 'samples': 0,
                   'observed_memory_current_peak_bytes': {},
                   'memory_method': 'sampled cgroup memory.current by active stage; memory.peak is cumulative, not per-stage'}
    if observer != 'C':
        observation['memory_method'] = 'internal sampling disabled; external cgroup measurements remain enabled'
    report['compute_observation'] = observation
    if observer == 'A':
        for key in ('collection_seconds', 'fusion_seconds', 'collection_calls', 'fusion_calls', 'fusion_input_fragments'):
            observation[key] = None
        observation['sampling_interval_seconds'] = None
        yield observation
        return
    if observer == 'B':
        observation['sampling_interval_seconds'] = None
    active = 'prepare'; stopped = threading.Event(); errors = []; started = time.monotonic()

    def collect(segments):
        nonlocal active
        active = 'collect'; before = time.monotonic()
        observation['collection_calls'] += 1
        try:
            yield from original_collect(segments)
        finally:
            observation['collection_seconds'] += time.monotonic() - before
            active = 'finalize'

    def merge(parts, *args, **kwargs):
        nonlocal active
        active = 'fuse'; before = time.monotonic()
        observation['fusion_calls'] += 1
        observation['fusion_input_fragments'] += len(parts)
        try:
            return original_union(parts, *args, **kwargs)
        finally:
            observation['fusion_seconds'] += time.monotonic() - before
            active = 'finalize'

    def sample(stream):
        stage = active
        sample = {'elapsed_seconds': time.monotonic() - started, 'stage': stage,
                  'memory_current_bytes': None, 'memory_peak_bytes': None, 'memory_limit_bytes': None}
        directory = Path('/sys/fs/cgroup')
        if sys.platform == 'linux' and (directory / 'memory.current').is_file():
            for field, filename in [('memory_current_bytes', 'memory.current'),
                                    ('memory_peak_bytes', 'memory.peak'), ('memory_limit_bytes', 'memory.max')]:
                value = (directory / filename).read_text().strip()
                sample[field] = None if value == 'max' else int(value)
            peaks = observation['observed_memory_current_peak_bytes']
            peaks[stage] = max(peaks.get(stage, 0), sample['memory_current_bytes'])
        observation['samples'] += 1
        stream.write(json.dumps(sample) + '\n'); stream.flush()
        write_json(progress, report)

    def sampling():
        try:
            with (root / 'compute-resources.jsonl').open('w') as stream:
                sample(stream)
                while not stopped.wait(.25):
                    sample(stream)
                sample(stream)
        except Exception as error:
            errors.append(error)

    operators._self_overlap_intersections = collect
    operators.shapely.union_all = merge
    sampler = None
    try:
        if observer == 'C':
            sampler = threading.Thread(target=sampling, daemon=True)
            sampler.start()
        yield observation
    finally:
        stopped.set()
        if sampler is not None and sampler.ident is not None:
            sampler.join()
        operators._self_overlap_intersections = original_collect
        operators.shapely.union_all = original_union
        if errors:
            raise RuntimeError(f'compute observation failed: {errors[0]}') from errors[0]


def run_worker(case, root, observer='C', measurement_id=None):
    """One process, one full measurement including generation and independent checks."""
    if case.operator in ('chains','normalize'):
        return run_dual_worker(case, root, observer, measurement_id)
    import resource
    sys.path.insert(0, '/app')
    from operators.io_operators import load, save
    from operators.geometric_operators import check_line_overlap, check_line_self_overlap
    from operators import geometric_operators
    identity = loaded_implementation_identity(geometric_operators)
    started = time.monotonic(); report = {'implementation_identity': identity, 'case': asdict(case), 'observer': observer,
        'measurement_id': measurement_id or case.key, 'result': 'incomplete', 'timings': {}}
    progress = root / 'progress.json'

    def phase(name, operation):
        report['stage'] = name; write_json(progress, report); before = time.monotonic()
        value = operation(); report['timings'][name] = time.monotonic() - before
        write_json(progress, report); return value

    source = root / 'source.gpkg'; target = root / 'result.gpkg'
    phase('generate', lambda: seed(case, source)); original = digest(source)
    connection = {'engine_type': 'nfs', 'mount_path': str(root)}
    frame = phase('read', lambda: load(connection_info=connection, path='source.gpkg'))
    phase('source_oracle', lambda: verify_source(frame, case)); before = frame_digest(frame)
    operator = check_line_self_overlap if case.operator == 'self' else check_line_overlap
    def compute():
        with observe_compute(root, report, progress, observer):
            return operator(frame, 'route_key')
    result = phase('compute', compute)
    assert len(result) == expected_rows(case)
    phase('save', lambda: save(result, connection_info=connection, path='result.gpkg', mode='fail'))
    physical = phase('reload_oracle', lambda: verify_output(case, target))
    assert frame_digest(frame) == before and digest(source) == original
    report.update(result='passed', stage='complete', source_frame_sha256=before, source=original, output=digest(target), physical=physical,
                  duration_seconds=time.monotonic() - started,
                  worker_peak_rss_bytes=resource.getrusage(resource.RUSAGE_SELF).ru_maxrss * (1 if sys.platform == 'darwin' else 1024))
    import platform
    import geopandas
    import shapely
    from osgeo import gdal
    report['versions'] = {'python': platform.python_version(), 'machine': platform.machine(),
                          'geopandas': geopandas.__version__, 'shapely': shapely.__version__,
                          'geos': shapely.geos_version_string, 'gdal': gdal.VersionInfo('RELEASE_NAME')}
    write_json(progress, report); return report


def command(args, timeout=60):
    result = subprocess.run(args, text=True, capture_output=True, timeout=timeout)
    if result.returncode:
        raise RuntimeError(f'{args[:3]} exited {result.returncode}: {result.stderr[-1200:]}')
    return result.stdout


def inspect(name):
    return json.loads(command(['docker', 'inspect', name]))[0]


def owned_remove(name, run_id):
    listing = ['docker', 'container', 'ls', '--all', '--filter', f'name=^/{name}$', '--format', '{{.Names}}']
    names = command(listing).splitlines()
    if not names:
        return
    if names != [name]:
        raise RuntimeError('ambiguous container name; cleanup refused')
    value = inspect(name)
    if value['Config']['Labels'].get('com.addp.capacity-run') != run_id:
        raise RuntimeError('foreign container; cleanup refused')
    command(['docker', 'rm', '-fv', name])
    if command(listing).strip():
        raise RuntimeError('owned container remains')


def classify(returncode, timed_out, state, metrics):
    if state.get('OOMKilled') or metrics.get('oom_kill', 0) or metrics.get('oom', 0):
        return 'memory_limit'
    if timed_out:
        return 'time_limit'
    return 'passed' if returncode == 0 and state.get('Running') else 'failed'


def validate_worker(report, case, observer='C', measurement_id=None):
    if case.operator in ('chains','normalize'):
        return validate_dual_worker(report, case, observer, measurement_id)
    assert observer in ('A', 'B', 'C')
    assert report['observer'] == observer and report['measurement_id'] == (measurement_id or case.key)
    assert len(report['source_frame_sha256']) == 64
    assert report['result'] == 'passed' and report['stage'] == 'complete' and report['case'] == asdict(case)
    assert report['physical']['rows'] == expected_rows(case) and report['physical']['all_coordinates_ids_lengths_verified'] is True
    assert report['physical']['crs'] == 'EPSG:3857' and 0 < report['duration_seconds'] <= SECONDS
    assert report['worker_peak_rss_bytes'] > 0
    observed = report['compute_observation']
    assert observed['observer'] == observer
    assert observed['wrapping_enabled'] is (observer != 'A') and observed['sampling_enabled'] is (observer == 'C')
    assert observed['sampling_interval_seconds'] == (.25 if observer == 'C' else None)
    assert (observed['samples'] > 0 if observer == 'C' else observed['samples'] == 0)
    if observer != 'C':
        assert observed['observed_memory_current_peak_bytes'] == {}
    fields = ('collection_seconds', 'fusion_seconds', 'collection_calls', 'fusion_calls', 'fusion_input_fragments')
    if observer == 'A':
        assert all(observed[key] is None for key in fields)
    else:
        rows = expected_rows(case)
        assert observed['fusion_calls'] == rows
        assert observed['fusion_input_fragments'] == rows * (8 if case.kind == 'many' else 1)
        assert observed['collection_calls'] == (source_rows(case) if case.operator == 'self' else 0)
        assert all(math.isfinite(observed[k]) and observed[k] >= 0 for k in ('collection_seconds', 'fusion_seconds'))
        assert observed['collection_seconds'] + observed['fusion_seconds'] <= report['timings']['compute']
    assert set(report['timings']) == {'generate', 'read', 'source_oracle', 'compute', 'save', 'reload_oracle'}
    assert all(math.isfinite(v) and v >= 0 for v in report['timings'].values())
    for field in ('source', 'output'):
        assert type(report[field]['size_bytes']) is int and report[field]['size_bytes'] > 0
        checksum = report[field]['sha256']; assert len(checksum) == 64 and all(c in '0123456789abcdef' for c in checksum)


def run_case(case, image, run_id, artifact, observer='C', measurement_id=None, repetition=None, implementation=None, expected_identity=None):
    from scripts.utils.capacity_resources import cgroup_directory, read_metrics
    if observer not in ('A', 'B', 'C'):
        raise ValueError('unknown observer')
    measurement_id = measurement_id or case.key
    if not measurement_id or any(c not in 'abcdefghijklmnopqrstuvwxyz0123456789-' for c in measurement_id):
        raise ValueError('invalid measurement id')
    name = f'addp-topology-capacity-{run_id}-{measurement_id}'
    root = artifact / measurement_id; root.mkdir(mode=0o700)
    workspace = root / 'data'; workspace.mkdir(mode=0o700)
    driver = Path(__file__).resolve(); outcome = {'id': measurement_id, 'case': asdict(case), 'observer': observer, 'repetition': repetition, 'result': 'incomplete'}
    if implementation is not None:
        assert implementation in ('old', 'new')
        outcome['implementation'] = implementation
    created = False; process = None
    try:
        created = True  # Record the attempt before mutation, including an unknown CLI outcome.
        command(['docker', 'create', '--name', name, '--network', 'none', '--user', f'{os.getuid()}:{os.getgid()}',
                 '--label', f'com.addp.capacity-run={run_id}', '--cpus', str(CPUS), '--memory', str(MEMORY), '--memory-swap', str(MEMORY),
                 '--mount', f'type=bind,source={workspace},destination=/capacity-data',
                 '--mount', f'type=bind,source={driver},destination=/capacity.py,readonly',
                 '-e', 'WORKFLOW_BIND_HOST=127.0.0.1', '-e', 'SYSTEM_URL=http://127.0.0.1:1', image])
        config = inspect(name)
        assert config['Config']['Cmd'] == ['sh', 'container_entrypoint.sh']
        assert config['HostConfig']['NanoCpus'] == CPUS * 1_000_000_000 and config['HostConfig']['Memory'] == config['HostConfig']['MemorySwap'] == MEMORY
        readiness_started = time.perf_counter()
        command(['docker', 'start', name])
        ready = False
        for _ in range(60):
            probe = subprocess.run(['docker', 'exec', name, 'python', '-c', "import urllib.request;urllib.request.urlopen('http://127.0.0.1:8099/health',timeout=1).read()"], capture_output=True, timeout=5)
            if probe.returncode == 0:
                ready = True; break
            time.sleep(1)
        if not ready:
            raise RuntimeError('product Runtime readiness failed')
        outcome['readiness_seconds'] = time.perf_counter() - readiness_started
        directory = cgroup_directory(inspect(name)['State']['Pid']); before = read_metrics(directory)
        timed_out = False; started = time.monotonic(); returncode = None
        metrics = before; peak_rss = before['process_peak_rss_bytes']; samples = 0
        with (root / 'worker.log').open('w') as log, (root / 'resources.jsonl').open('w') as series:
            process = subprocess.Popen(['docker', 'exec', name, 'python', '/capacity.py', '--worker', case.key, '--observer', observer, '--measurement', measurement_id], stdout=log, stderr=subprocess.STDOUT)
            while process.poll() is None:
                try:
                    metrics = read_metrics(directory)
                    peak_rss = max(peak_rss, metrics['process_peak_rss_bytes']); samples += 1
                    series.write(json.dumps({'elapsed_seconds': time.monotonic() - started, **metrics}) + '\n'); series.flush()
                except FileNotFoundError:
                    # The cgroup may disappear after OOM kills the whole container.
                    # Docker's terminal OOMKilled fact remains authoritative below.
                    pass
                if time.monotonic() - started >= SECONDS:
                    timed_out = True
                    command(['docker', 'kill', name])
                    process.kill(); process.wait(); break
                time.sleep(.25)
            returncode = process.poll()
        elapsed = time.monotonic() - started
        try: metrics = read_metrics(directory)
        except FileNotFoundError: pass
        state = inspect(name)['State']
        outcome.update(result=classify(returncode, timed_out, state, metrics), wall_seconds=elapsed,
                       resource_before=before, resource_after=metrics, sampled_peak_rss_bytes=peak_rss,
                       resource_samples=samples, timed_out=timed_out, exit_code=returncode,
                       image_id=config['Image'], container_state=state,
                       rss_method='sum-of-observed-container-process-VmHWM; not simultaneous RSS',
                       sampling_interval_seconds=.25)
        progress = workspace / 'progress.json'
        outcome['worker'] = json.loads(progress.read_text()) if progress.is_file() else None
        if progress.is_file(): shutil.copyfile(progress, root / 'worker-progress.json')
        if (workspace / 'compute-resources.jsonl').is_file():
            shutil.copyfile(workspace / 'compute-resources.jsonl', root / 'compute-resources.jsonl')
        if outcome['result'] == 'passed':
            validate_worker(outcome['worker'], case, observer, measurement_id)
            if expected_identity is not None:
                assert outcome['worker']['implementation_identity'] == {**expected_identity, ('loaded_normalize_matches_source' if case.operator == 'normalize' else 'loaded_merge_matches_source'): True}
            assert elapsed <= SECONDS
            assert metrics['swap_current_bytes'] == 0
    except Exception as error:
        outcome.update(result='failed', error=str(error))
    finally:
        if process is not None and process.poll() is None:
            process.kill(); process.wait()
        if created:
            try:
                logs = subprocess.run(['docker', 'logs', name], text=True, capture_output=True, timeout=5)
                (root / 'runtime.log').write_text(logs.stdout + logs.stderr)
            except Exception as error:
                outcome['runtime_log_error'] = str(error)
            try:
                owned_remove(name, run_id)
                outcome['cleanup'] = 'passed'
            except Exception as error:
                outcome.update(cleanup='failed', cleanup_error=str(error), result='failed')
        # All synthetic datasets and failure sidecars belong exclusively to this directory.
        shutil.rmtree(workspace)
        if workspace.exists(): outcome.update(result='failed', cleanup='failed')
        write_json(root / 'result.json', outcome)
    return outcome


def cleanup_images(attempts):
    errors = []
    for attempted, owned_image in attempts:
        if attempted and subprocess.run(['docker', 'image', 'inspect', owned_image], capture_output=True).returncode == 0:
            try:
                command(['docker', 'image', 'rm', owned_image])
                if subprocess.run(['docker', 'image', 'inspect', owned_image], capture_output=True).returncode == 0:
                    raise RuntimeError('private product image remains')
            except Exception as error: errors.append(str(error))
    return errors


def run_gate(profile='matrix'):
    plan = measurement_plan(profile)
    repository = Path(__file__).resolve().parents[2]
    if (os.getenv('GITHUB_ACTIONS') != 'true' or os.getenv('RUNNER_OS') != 'Linux'
            or os.getenv('GITHUB_EVENT_NAME') != 'workflow_dispatch' or sys.platform != 'linux'):
        raise RuntimeError('full capacity requires the manual isolated GitHub Linux job')
    if len(os.sched_getaffinity(0)) < CPUS:
        raise RuntimeError('capacity host has fewer than four available CPUs')
    source = command(['git', 'rev-parse', 'HEAD']).strip()
    if source != os.environ['GITHUB_SHA']:
        raise RuntimeError('capacity source differs from checkout')
    if command(['git', 'status', '--porcelain', '--untracked-files=normal']).strip():
        raise RuntimeError('capacity requires an unchanged checkout')
    run_id = uuid.uuid4().hex[:12]
    artifact = Path(os.environ['RUNNER_TEMP']) / f'addp-topology-capacity-{os.environ["GITHUB_RUN_ID"]}-{os.environ["GITHUB_RUN_ATTEMPT"]}'
    artifact.mkdir(mode=0o700)
    image = f'localhost:5001/addp-geopython-workflow-engine:topology-capacity-{run_id}'
    registry = f'addp-topology-capacity-registry-{run_id}'
    report = {'schema_version': NORMALIZE_SCHEMA if profile == 'normalize' else SCHEMA, 'source': source, 'experiment_profile': profile,
              'runner': {key: os.getenv(key) for key in ('GITHUB_RUN_ID', 'GITHUB_RUN_ATTEMPT', 'GITHUB_JOB', 'RUNNER_NAME', 'RUNNER_ARCH', 'ImageOS', 'ImageVersion')},
              'host': {'hostname': os.uname().nodename, 'available_cpus': sorted(os.sched_getaffinity(0)),
                       'cpuinfo_sha256': hashlib.sha256(Path('/proc/cpuinfo').read_bytes()).hexdigest()},
              'profile': {'cpu': CPUS, 'memory_bytes': MEMORY, 'swap_bytes': 0, 'seconds_per_case': SECONDS},
              'planned_measurements': plan, 'planned_cases': [item['case'] for item in plan], 'groups': [], 'result': 'incomplete'}
    if profile == 'normalize':
        report['normalization_implementation'] = implementation_identity((repository / GEOMETRY_MODULE).read_text(), 'normalize')
    if profile == 'chains':
        report['chain_implementation'] = implementation_identity((repository / GEOMETRY_MODULE).read_text(), 'chains')
    registry_created = False; image_attempted = False
    old_image = f'{image}-old'
    old_image_attempted = False
    try:
        registry_created = True
        command(['docker', 'create', '--name', registry, '--label', f'com.addp.capacity-run={run_id}', '-p', '127.0.0.1:5001:5000', 'registry:2'])
        command(['docker', 'start', registry])
        import urllib.request
        for attempt in range(30):
            try:
                with urllib.request.urlopen('http://127.0.0.1:5001/v2/', timeout=1) as response:
                    assert response.status == 200
                break
            except OSError:
                if attempt == 29: raise RuntimeError('private registry readiness failed')
                time.sleep(.5)
        command(['docker', 'pull', 'python:3.11-slim'], timeout=180)
        command(['docker', 'tag', 'python:3.11-slim', 'localhost:5001/python:3.11-slim'])
        command(['docker', 'push', 'localhost:5001/python:3.11-slim'], timeout=180)
        image_attempted = True
        build_started = time.monotonic()
        build = subprocess.run(['make', 'build-images', f'IMAGE_BUILD_ARGS=--services geopython-workflow-engine --tag topology-capacity-{run_id} --verify --jobs 1'], cwd=repository)
        report['image_build_seconds'] = time.monotonic() - build_started
        if build.returncode:
            raise RuntimeError('product image build failed')
        images = {'new': image}
        if profile == 'comparison':
            variants, report['comparison_sources'] = comparison_sources(repository)
            context = artifact / 'comparison'; context.mkdir()
            for name, text in variants.items(): (context / f'{name}.py').write_text(text)
            command(['docker', 'push', image], timeout=180)
            base = json.loads(command(['docker', 'image', 'inspect', image]))[0]
            references = [value for value in base['RepoDigests'] if value.startswith(image.rsplit(':', 1)[0] + '@sha256:')]
            assert len(references) == 1, 'comparison requires the private base manifest digest'
            report['comparison_base_manifest'] = references[0]
            (context / 'Dockerfile').write_text(f"FROM {references[0]}\nCOPY --chmod=0644 old.py /app/operators/geometric_operators.py\n")
            old_image_attempted = True
            command(['docker', 'build', '--network', 'none', '--pull=false', '-t', old_image, str(context)], timeout=180)
            old = json.loads(command(['docker', 'image', 'inspect', old_image]))[0]
            report['comparison_image_proof'] = comparison_image_proof(base, old)
            images = report['comparison_image_proof']['images']
        for index, item in enumerate(plan):
            report['groups'].append(run_case(Case(**item['case']), images[item.get('implementation', 'new')], run_id, artifact,
                observer=item['observer'], measurement_id=item['id'], repetition=item['repetition'], implementation=item.get('implementation'),
                expected_identity=report.get('normalization_implementation') if profile == 'normalize' else report.get('chain_implementation')))
            write_json(artifact / 'report.json', report)
            if report['groups'][-1].get('cleanup') != 'passed':
                report['not_run'] = {'reason': 'prior_case_cleanup_failed', 'measurements': plan[index + 1:]}
                break
        report['result'] = 'passed' if len(report['groups']) == len(plan) and all(group['result'] == 'passed' for group in report['groups']) else 'failed'
        if profile == 'observation' and report['result'] == 'passed':
            report['observation_summary'] = observation_summary(report['groups'])
        if profile == 'comparison' and report['result'] == 'passed':
            report['comparison_summary'] = comparison_summary(report['groups'], report['comparison_sources'], report['comparison_image_proof'])
    except Exception as error:
        report.update(result='failed', error=str(error))
    finally:
        errors = [f'case cleanup failed: {group["case"]}' for group in report['groups'] if group.get('cleanup') != 'passed']
        errors.extend(cleanup_images(((old_image_attempted, old_image), (image_attempted, image))))
        if registry_created:
            try: owned_remove(registry, run_id)
            except Exception as error: errors.append(str(error))
        report['cleanup'] = 'failed' if errors else 'passed'; report['cleanup_errors'] = errors
        if errors: report['result'] = 'failed'
        write_json(artifact / 'report.json', report)
    return 0 if report['result'] == 'passed' else 1


if __name__ == '__main__':
    def terminate(*_):
        raise KeyboardInterrupt('capacity interrupted')
    signal.signal(signal.SIGTERM, terminate)
    parser = argparse.ArgumentParser()
    parser.add_argument('--worker', choices=[case.key for case in matrix() + chain_matrix() + normalize_matrix()])
    parser.add_argument('--profile', choices=('matrix', 'observation', 'comparison', 'chains', 'normalize'), default=os.getenv('TOPOLOGY_CAPACITY_PROFILE', 'matrix'))
    parser.add_argument('--observer', choices=('A', 'B', 'C'), default='C')
    parser.add_argument('--measurement')
    args = parser.parse_args()
    measurement_plan(args.profile)  # Validate environment defaults before any mutation.
    if args.worker:
        run_worker(next(case for case in matrix() + chain_matrix() + normalize_matrix() if case.key == args.worker), Path('/capacity-data'), args.observer, args.measurement)
    else:
        raise SystemExit(run_gate(args.profile))

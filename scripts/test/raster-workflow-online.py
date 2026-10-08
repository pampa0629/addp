#!/usr/bin/env python3
"""Real Develop raster execution -> automatic Meta scan/lineage -> Monitor/Console."""
from __future__ import annotations

import importlib
import json
import math
import os
from pathlib import Path
import subprocess
import sys
import time
import urllib.parse

support = importlib.import_module('scripts.test.manager-internal-artifact-lineage-online')
fixture = importlib.import_module('business.scripts.online-raster-minio-fixture')
GatewayClient, SuiteError = support.GatewayClient, support.SuiteError
obj, array, positive = support._object, support._array, support.positive_int
PERMISSIONS = {'develop.task.read', 'develop.task.execute', 'develop.data_read.execute',
               'develop.data_write.execute', 'system.execution_authorization.create',
               'meta.catalog.read', 'meta.scan_task.read', 'meta.scan_task.execute',
               'meta.lineage.read', 'monitor.execution.read'}
SCHEMA = 'addp.raster-workflow-online/v1'


def validate_identity(client, tenant_id):
    context = obj(client.request('GET', '/api/v1/system/auth/context', (200,)).payload, 'AuthContext')
    principal = obj(context.get('principal'), 'principal')
    tenant = obj(context.get('context'), 'context')
    token = obj(context.get('token'), 'token')
    if principal.get('type') != 'user' or tenant.get('type') != 'tenant' or tenant.get('tenant_id') != str(tenant_id):
        raise SuiteError('raster acceptance requires the configured non-default Tenant User')
    if token.get('type') not in {'first_party_access_token', 'oauth_access_token'}:
        raise SuiteError('raster acceptance requires a User Access Token')
    assignments = array(obj(context.get('authorization'), 'authorization').get('role_assignments'), 'assignments')
    roles = {obj(assignment, 'assignment').get('role_key') for assignment in assignments}
    permissions = {key for assignment in assignments for key in array(assignment.get('permissions'), 'permissions')}
    if roles & support.FORBIDDEN_ADMIN_ROLES or permissions != PERMISSIONS:
        raise SuiteError('raster User must have exactly the scene permissions and no administrator role')
    return {'principal_id': str(positive(principal.get('id'), 'principal.id')), 'tenant_id': str(tenant_id),
            'principal_type': 'user', 'permissions_verified': sorted(PERMISSIONS)}


def wait_execution(client, module, execution_id, timeout, expected='success'):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        execution = obj(client.request('GET', f'/api/v1/{module}/executions/{urllib.parse.quote(execution_id)}', (200,)).payload, 'execution')
        if execution.get('status') in support.TERMINAL_STATUSES:
            if execution['status'] != expected:
                raise SuiteError(f'{module} execution ended with status {execution["status"]}, expected {expected}')
            return execution
        time.sleep(1)
    raise SuiteError(f'{module} execution convergence timed out')


def workflow(source_locator, target_engine_id, expression, mode):
    return {'tasks': [
        {'id': 'load', 'operator': 'raster_load', 'depends_on': [], 'params': {'locator': source_locator}},
        {'id': 'math', 'operator': 'raster_band_math', 'depends_on': ['load'],
         'params': {'input_raster': {'$ref': 'load', 'port': 'default'}, 'expression': expression}},
        {'id': 'save', 'operator': 'raster_save', 'depends_on': ['math'], 'params': {
            'input_raster': {'$ref': 'math', 'port': 'default'},
            'target_parent_locator': f'addp://engine/{target_engine_id}/path/raster-target?type=bucket',
            'target_name': 'result.cog.tif', 'write_mode': mode, 'profile': 'cog', 'blocksize': 128,
        }},
    ]}


def spatial_workflow(source_locator, target_engine_id, overlap):
    def reference(task): return {'$ref': task, 'port': 'default'}
    return {'tasks': [
        {'id': 'load', 'operator': 'raster_load', 'depends_on': [], 'params': {'locator': source_locator}},
        {'id': 'project', 'operator': 'raster_reproject', 'depends_on': ['load'], 'params': {
            'input_raster': reference('load'), 'target_crs': 'EPSG:3857', 'resolution': [1, 1], 'resampling': 'nearest'}},
        {'id': 'left_math', 'operator': 'raster_band_math', 'depends_on': ['project'], 'params': {
            'input_raster': reference('project'), 'expression': 'b2-b1'}},
        {'id': 'right_math', 'operator': 'raster_band_math', 'depends_on': ['project'], 'params': {
            'input_raster': reference('project'), 'expression': 'b2+b1'}},
        {'id': 'left_clip', 'operator': 'raster_clip', 'depends_on': ['left_math'], 'params': {
            'input_raster': reference('left_math'), 'boundary_crs': 'EPSG:3857', 'bbox': [0, 0, 160, 256]}},
        {'id': 'right_clip', 'operator': 'raster_clip', 'depends_on': ['right_math'], 'params': {
            'input_raster': reference('right_math'), 'boundary_crs': 'EPSG:3857', 'bbox': [96, 0, 256, 256]}},
        {'id': 'mosaic', 'operator': 'raster_mosaic', 'depends_on': ['left_clip', 'right_clip'], 'params': {
            'input_raster': reference('left_clip'), 'other_raster': reference('right_clip'),
            'target_crs': 'EPSG:3857', 'resolution': [1, 1], 'overlap': overlap, 'resampling': 'nearest'}},
        {'id': 'save', 'operator': 'raster_save', 'depends_on': ['mosaic'], 'params': {
            'input_raster': reference('mosaic'),
            'target_parent_locator': f'addp://engine/{target_engine_id}/path/raster-target?type=bucket',
            'target_name': f'mosaic-{overlap}.cog.tif', 'write_mode': 'create', 'profile': 'cog', 'blocksize': 128}},
    ]}


def analysis_workflow(source_locator, case_name):
    tasks = [{'id': 'load', 'operator': 'raster_load', 'depends_on': [], 'params': {'locator': source_locator}}]
    input_task = 'load'
    if case_name in ('statistics-all-invalid', 'histogram-auto'):
        input_task = 'math'
        tasks.append({'id': 'math', 'operator': 'raster_band_math', 'depends_on': ['load'], 'params': {
            'input_raster': {'$ref': 'load', 'port': 'default'},
            'expression': 'sqrt(-b1)' if case_name == 'statistics-all-invalid' else 'b2-b1'}})
    params = {'input_raster': {'$ref': input_task, 'port': 'default'},
              'band': 2 if case_name in ('statistics-band-2', 'histogram-range') else 1}
    if case_name in ('histogram-auto', 'histogram-range'):
        operator = 'raster_histogram'
        params['bins'] = 4
        if case_name == 'histogram-range': params['value_range'] = [32768, 98304]
    elif case_name in ('statistics-band-2', 'statistics-all-invalid'):
        operator = 'raster_statistics'
    else:
        raise SuiteError('unknown raster analysis case')
    tasks.append({'id': 'analysis', 'operator': operator, 'depends_on': [input_task], 'params': params})
    return {'tasks': tasks}


def grid_workflow(source_locator, target_engine_id, case_name):
    if case_name not in fixture.GRID_CASES:
        raise SuiteError('unknown raster grid case')
    polygon = case_name == 'clip-polygon'
    params = {'input_raster': {'$ref': 'math', 'port': 'default'}}
    if polygon:
        params.update(boundary_crs='EPSG:3857', geometry=fixture.clip_geometry())
    elif case_name == 'resample-size':
        params.update(size=[128, 128], resampling='nearest')
    else:
        params.update(resolution=[2 * fixture.ANGULAR_METRE, 4 * fixture.ANGULAR_METRE], resampling='nearest')
    return {'tasks': [
        {'id': 'load', 'operator': 'raster_load', 'depends_on': [], 'params': {'locator': source_locator}},
        {'id': 'math', 'operator': 'raster_band_math', 'depends_on': ['load'], 'params': {
            'input_raster': {'$ref': 'load', 'port': 'default'}, 'expression': 'b2-b1'}},
        {'id': 'grid', 'operator': 'raster_clip' if polygon else 'raster_resample', 'depends_on': ['math'], 'params': params},
        {'id': 'save', 'operator': 'raster_save', 'depends_on': ['grid'], 'params': {
            'input_raster': {'$ref': 'grid', 'port': 'default'},
            'target_parent_locator': f'addp://engine/{target_engine_id}/path/raster-target?type=bucket',
            'target_name': case_name + '.cog.tif', 'write_mode': 'create', 'profile': 'cog', 'blocksize': 128}},
    ]}


def multiband_workflow(source_locator, target_engine_id, case_name):
    if case_name not in fixture.MULTIBAND_CASES:
        raise SuiteError('unknown raster multiband case')
    joint = case_name.endswith('-joint')
    return {'tasks': [
        {'id': 'load', 'operator': 'raster_load', 'depends_on': [], 'params': {'locator': source_locator}},
        {'id': 'grid', 'operator': 'raster_band_math' if joint else 'raster_resample', 'depends_on': ['load'],
         'params': {'input_raster': {'$ref': 'load', 'port': 'default'},
                    **({'expression': 'b1+b2'} if joint else {'size': [fixture.multiband_expectation(case_name)['width']] * 2,
                        'resampling': 'bilinear' if 'bilinear' in case_name else 'average' if 'average' in case_name else 'nearest'})}},
        {'id': 'save', 'operator': 'raster_save', 'depends_on': ['grid'], 'params': {
            'input_raster': {'$ref': 'grid', 'port': 'default'},
            'target_parent_locator': f'addp://engine/{target_engine_id}/path/raster-target?type=bucket',
            'target_name': case_name + '.cog.tif', 'write_mode': 'create', 'profile': 'cog', 'blocksize': 128}},
    ]}


def utility_workflow(source_locator, target_engine_id, case_name):
    target = {'target_parent_locator': f'addp://engine/{target_engine_id}/path/raster-target?type=bucket',
              'target_name': case_name + '.cog.tif', 'write_mode': 'create'}
    if case_name == 'to-cog':
        return {'tasks': [{'id': 'convert', 'operator': 'raster_to_cog', 'depends_on': [], 'params': {
            'locator': source_locator, **target, 'blocksize': 128, 'overview_resampling': 'nearest'}}]}
    if case_name == 'build-overviews':
        return {'tasks': [
            {'id': 'load', 'operator': 'raster_load', 'depends_on': [], 'params': {'locator': source_locator}},
            {'id': 'overviews', 'operator': 'raster_build_overviews', 'depends_on': ['load'], 'params': {
                'input_raster': {'$ref': 'load', 'port': 'default'}, 'levels': [2, 4], 'resampling': 'nearest'}},
            {'id': 'save', 'operator': 'raster_save', 'depends_on': ['overviews'], 'params': {
                'input_raster': {'$ref': 'overviews', 'port': 'default'}, **target, 'profile': 'cog', 'blocksize': 128}},
        ]}
    if case_name in fixture.UTILITY_JSON_CASES:
        return {'tasks': [
            {'id': 'load', 'operator': 'raster_load', 'depends_on': [], 'params': {'locator': source_locator}},
            {'id': 'analysis', 'operator': 'raster_info' if case_name == 'info-overviews' else 'validate_cog',
             'depends_on': ['load'], 'params': {'input_raster': {'$ref': 'load', 'port': 'default'}}},
        ]}
    raise SuiteError('unknown raster utility case')


def foundation_workflow(source_locator, reference_locator, target_engine_id, case_name):
    if case_name not in fixture.FOUNDATION_CASES:
        raise SuiteError('unknown multi-raster case')
    def ref(node):
        return {'$ref': node, 'port': 'default'}
    return {'tasks': [
        {'id': 'load', 'operator': 'raster_load', 'depends_on': [], 'params': {'locator': source_locator}},
        {'id': 'reference', 'operator': 'raster_load', 'depends_on': [], 'params': {'locator': reference_locator}},
        {'id': 'align', 'operator': 'raster_align', 'depends_on': ['load', 'reference'], 'params': {
            'input_raster': ref('load'), 'reference_raster': ref('reference'), 'resampling': 'nearest'}},
        {'id': 'select', 'operator': 'raster_select_bands', 'depends_on': ['align'], 'params': {
            'input_raster': ref('align'), 'bands': [1]}},
        {'id': 'stack', 'operator': 'raster_stack', 'depends_on': ['select', 'reference'], 'params': {
            'input_raster': ref('select'), 'other_raster': ref('reference')}},
        {'id': 'compose', 'operator': 'raster_band_math' if case_name == 'multiraster-weighted' else 'raster_select_bands',
            'depends_on': ['stack'], 'params': {'input_raster': ref('stack'),
                **({'expression': '0.25*b1+0.75*b2'} if case_name == 'multiraster-weighted' else
                   {'bands': [1, 2, 1], 'color_model': 'rgb'})}},
        {'id': 'save', 'operator': 'raster_save', 'depends_on': ['compose'], 'params': {
            'input_raster': ref('compose'), 'target_parent_locator': f'addp://engine/{target_engine_id}/path/raster-target?type=bucket',
            'target_name': case_name + '.cog.tif', 'write_mode': 'create', 'profile': 'cog', 'blocksize': 128}},
    ]}


def reclass_workflow(source_locator, target_engine_id, case_name):
    if case_name not in fixture.RECLASS_CASES:
        raise SuiteError('unknown reclassification case')
    params = {'input_raster': {'$ref': 'load', 'port': 'default'}, 'band': 2,
              'rules': [{'value': 4, 'class': 0}, {'min': 6, 'max': 1024, 'class': 1},
                        {'min': 32768, 'max': None, 'class': 2}]}
    if case_name == 'reclassify-keep':
        params['unmatched'] = 'keep'
    return {'tasks': [
        {'id': 'load', 'operator': 'raster_load', 'depends_on': [], 'params': {'locator': source_locator}},
        {'id': 'classify', 'operator': 'raster_reclassify', 'depends_on': ['load'], 'params': params},
        {'id': 'save', 'operator': 'raster_save', 'depends_on': ['classify'], 'params': {
            'input_raster': {'$ref': 'classify', 'port': 'default'},
            'target_parent_locator': f'addp://engine/{target_engine_id}/path/raster-target?type=bucket',
            'target_name': case_name + '.cog.tif', 'write_mode': 'create', 'profile': 'cog', 'blocksize': 128}},
    ]}


def aggregate_workflow(source_locator, target_engine_id, case_name):
    if case_name not in fixture.AGGREGATE_CASES:
        raise SuiteError('unknown aggregation case')
    ref = lambda name: {'$ref': name, 'port': 'default'}
    return {'tasks': [
        {'id': 'load', 'operator': 'raster_load', 'depends_on': [], 'params': {'locator': source_locator}},
        {'id': 'select', 'operator': 'raster_select_bands', 'depends_on': ['load'], 'params': {'input_raster': ref('load'), 'bands': [2]}},
        {'id': 'aggregate', 'operator': 'raster_aggregate', 'depends_on': ['select'], 'params': {
            'input_raster': ref('select'), 'factors': [3, 5], 'method': case_name.removeprefix('aggregate-')}},
        {'id': 'levels', 'operator': 'raster_build_overviews', 'depends_on': ['aggregate'], 'params': {
            'input_raster': ref('aggregate'), 'levels': [2], 'resampling': 'nearest'}},
        {'id': 'save', 'operator': 'raster_save', 'depends_on': ['levels'], 'params': {
            'input_raster': ref('levels'), 'target_parent_locator': f'addp://engine/{target_engine_id}/path/raster-target?type=bucket',
            'target_name': case_name + '.cog.tif', 'write_mode': 'create', 'profile': 'cog', 'blocksize': 128}},
    ]}


def outside_workflow(source_locator, target_engine_id, case_name):
    if case_name not in fixture.OUTSIDE_CASES:
        raise SuiteError('unknown outside clipping case')
    ref = lambda name: {'$ref': name, 'port': 'default'}
    # Explicit geographical boundary; physical oracle independently uses pixel indices.
    geometry = {'type': 'Polygon', 'coordinates': [
        [[110.64, 18.4], [111.92, 18.4], [111.92, 19.68], [110.64, 19.68], [110.64, 18.4]],
        [[110.96, 18.72], [110.96, 19.36], [111.6, 19.36], [111.6, 18.72], [110.96, 18.72]],
    ]}
    return {'tasks': [
        {'id': 'load', 'operator': 'raster_load', 'depends_on': [], 'params': {'locator': source_locator}},
        {'id': 'clip', 'operator': 'raster_clip', 'depends_on': ['load'], 'params': {
            'input_raster': ref('load'), 'mode': 'outside', 'boundary_crs': 'EPSG:4326', 'geometry': geometry}},
        {'id': 'levels', 'operator': 'raster_build_overviews', 'depends_on': ['clip'], 'params': {
            'input_raster': ref('clip'), 'levels': [2], 'resampling': 'nearest'}},
        {'id': 'save', 'operator': 'raster_save', 'depends_on': ['levels'], 'params': {
            'input_raster': ref('levels'), 'target_parent_locator': f'addp://engine/{target_engine_id}/path/raster-target?type=bucket',
            'target_name': case_name + '.cog.tif', 'write_mode': 'create', 'profile': 'cog', 'blocksize': 128}},
    ]}


def footprint_workflow(source_locator, case_name):
    if case_name not in fixture.FOOTPRINT_CASES:
        raise SuiteError('unknown footprint case')
    params = {'input_raster': {'$ref': 'load', 'port': 'default'}}
    if case_name == 'footprint-all':
        params['validity'] = 'all'
    return {'tasks': [
        {'id': 'load', 'operator': 'raster_load', 'depends_on': [], 'params': {'locator': source_locator}},
        {'id': 'analysis', 'operator': 'raster_footprint', 'depends_on': ['load'], 'params': params},
    ]}


def validate_footprint(execution, case_name):
    actual = transient_result(execution)
    if actual.get('type') != 'FeatureCollection' or actual.get('crs') is not None or len(array(actual.get('features'), 'footprint features')) != 1:
        raise SuiteError('footprint must contain one geometry feature')
    feature = obj(actual['features'][0], 'footprint feature')
    geometry = obj(feature.get('geometry'), 'footprint geometry')
    if feature.get('type') != 'Feature' or feature.get('properties') != {} or geometry.get('type') != 'MultiPolygon':
        raise SuiteError('footprint lost its geometry-only MultiPolygon contract')
    polygons = []
    area = 0
    for polygon in array(geometry.get('coordinates'), 'footprint polygons'):
        rings = []
        for ring in array(polygon, 'footprint rings'):
            vertices = []
            for point in array(ring, 'footprint vertices'):
                if (not isinstance(point, list) or len(point) != 2 or
                    any(type(value) not in (int, float) or not math.isfinite(value) for value in point)):
                    raise SuiteError('footprint coordinates must be finite pairs')
                pixel = [(point[0]-110)/.01, (20.32-point[1])/.01]
                if any(not math.isclose(value, round(value), rel_tol=0, abs_tol=1e-8) or
                       not 0 <= round(value) <= fixture.SIZE for value in pixel):
                    raise SuiteError('footprint boundary left the source pixel grid')
                vertices.append(tuple(round(value) for value in pixel))
            if len(vertices) < 4 or vertices[0] != vertices[-1]:
                raise SuiteError('footprint ring must be closed')
            if any(a[0] != b[0] and a[1] != b[1] for a, b in zip(vertices, vertices[1:])):
                raise SuiteError('footprint boundary cut through source cells')
            rings.append(vertices)
        if not rings:
            raise SuiteError('footprint polygon has no exterior')
        areas = [abs(sum(a[0]*b[1]-b[0]*a[1] for a, b in zip(ring, ring[1:]))) / 2 for ring in rings]
        area += areas[0] - sum(areas[1:])
        polygons.append(rings)
    def inside(x, y, ring):
        covered = False
        for a, b in zip(ring, ring[1:]):
            if (a[1] > y) != (b[1] > y) and x < (b[0]-a[0]) * (y-a[1]) / (b[1]-a[1]) + a[0]:
                covered = not covered
        return covered
    expected = list(fixture.footprint_pixels(case_name))
    if area != sum(expected):
        raise SuiteError('footprint area differs from independent valid pixel count')
    for position, wanted in enumerate(expected):
        x, y = position % fixture.SIZE + .5, position // fixture.SIZE + .5
        covered = any(inside(x, y, rings[0]) and not any(inside(x, y, ring) for ring in rings[1:]) for rings in polygons)
        if covered != wanted:
            raise SuiteError('footprint coverage differs from independent source validity')
    return actual


def transient_result(execution):
    metadata = obj(execution.get('metadata'), 'Develop analysis metadata')
    result = obj(metadata.get('result'), 'Develop analysis result')
    if (execution.get('outputs') or metadata.get('lineage_facts') or result.get('produced_targets')
        or result.get('meta_scan_runs')):
        raise SuiteError('transient raster analysis published a persistent output, derive fact or target scan')
    if obj(result.get('summary'), 'analysis summary').get('has_result') is not True:
        raise SuiteError('Develop did not record its transient analysis result')
    actual = obj(result.get('final_result'), 'analysis JSON preview')
    return actual


def validate_analysis(execution, expectation):
    actual = transient_result(execution)

    def compare(value, expected):
        if isinstance(expected, dict):
            return isinstance(value, dict) and value.keys() == expected.keys() and all(
                compare(value[key], item) for key, item in expected.items())
        if isinstance(expected, list):
            return isinstance(value, list) and len(value) == len(expected) and all(
                compare(item, target) for item, target in zip(value, expected))
        if type(expected) is int:
            return type(value) in (int, float) and math.isfinite(value) and value == expected
        if type(expected) is float:
            return type(value) in (int, float) and math.isfinite(value) and math.isclose(value, expected, rel_tol=1e-12, abs_tol=1e-10)
        return type(value) is type(expected) and value == expected

    if not compare(actual, expectation):
        raise SuiteError('raster analysis JSON differs from the independent numeric oracle')
    return actual


def validate_utility_json(execution, case_name, physical_evidence):
    if case_name == 'info-overviews':
        crs = physical_evidence.get('crs')
        if not isinstance(crs, str) or not crs:
            raise SuiteError('physical raster projection evidence is missing')
        return validate_analysis(execution, fixture.utility_info_expectation(crs))
    if case_name not in ('validate-cog-invalid', 'validate-cog-valid'):
        raise SuiteError('unknown raster utility JSON case')
    actual = transient_result(execution)
    valid = case_name == 'validate-cog-valid'
    if (actual.keys() != {'valid', 'warnings', 'errors'} or actual['valid'] is not valid
        or any(not isinstance(actual[key], list) or any(not isinstance(value, str) or not value for value in actual[key])
               for key in ('warnings', 'errors')) or bool(actual['errors']) is valid):
        raise SuiteError('workflow COG validation disagrees with the known physical layout')
    return actual


def submit(client, engine_id, definition):
    response = obj(client.request('POST', '/api/v1/develop/executions', (200,), {
        'dev_type': 'workflow', 'trigger_type': 'manual', 'timeout': 180,
        'content': {'workflow_definition': definition}, 'execution_config': {'engine_id': engine_id},
    }).payload, 'execution submission')
    value = response.get('execution_id')
    if not isinstance(value, str) or not value:
        raise SuiteError('Develop did not return an execution_id')
    return value


def validate_success(execution, source_locators, target_locator, mode, expectation=None, output_node='save'):
    expectation = expectation or fixture.artifact_expectation()
    metadata = obj(execution.get('metadata'), 'Develop metadata')
    resource = obj(obj(obj(execution.get('outputs'), 'outputs').get(output_node), 'target output').get('resource'), 'resource')
    if resource != {'locator': target_locator, 'type': 'object', 'write_mode': mode}:
        raise SuiteError('Develop stable output has the wrong locator/type/write_mode')
    facts = obj(metadata.get('lineage_facts'), 'lineage facts')
    if facts.get('schema_version') != 'addp.lineage-facts/v1':
        raise SuiteError('Develop did not persist canonical lineage facts')
    inputs, outputs = array(facts.get('inputs'), 'inputs'), array(facts.get('outputs'), 'outputs')
    if len(inputs) != len(source_locators) or {item.get('locator') for item in inputs} != set(source_locators):
        raise SuiteError('Develop lineage does not bind the exact sources')
    if len(outputs) != 1 or outputs[0].get('locator') != target_locator or outputs[0].get('write_mode') != mode:
        raise SuiteError('Develop lineage does not bind the exact target/write mode')
    result = obj(metadata.get('result'), 'result')
    artifact = obj(result.get('final_result'), 'raster artifact')
    if (artifact.get('artifact_type'), artifact.get('format'), artifact.get('profile')) != ('raster', 'tiff', 'cog'):
        raise SuiteError('Develop result is not a public raster COG artifact')
    if (artifact.get('width'), artifact.get('height'), artifact.get('band_count')) != (expectation['width'], expectation['height'], expectation['band_count']):
        raise SuiteError('Develop raster artifact dimensions are invalid')
    if artifact.get('source_crs') != expectation['source_crs'] or artifact.get('extent_srid') != expectation['extent_srid']:
        raise SuiteError('Develop raster artifact CRS is invalid')
    transform = array(artifact.get('transform'), 'artifact transform')
    if len(transform) != 6 or any(not math.isclose(actual, expected, abs_tol=1e-10) for actual, expected in
                                 zip(transform, expectation['transform'])):
        raise SuiteError('Develop raster artifact transform is invalid')
    bands = array(artifact.get('bands'), 'artifact bands')
    expected_nodata = expectation['band_nodata']
    if len(bands) != expectation['band_count'] or any(
        band.get('dtype') != 'Float64' or band.get('nodata_is_nan') is not (expected_nodata is None)
        or band.get('nodata') != expected_nodata for band in bands
    ):
        raise SuiteError('Develop raster artifact did not preserve band dtype/NoData')
    forbidden = {'access_plan', 'connection_info', 'access_key', 'secret_key', 'password', 'path', 'workspace'}
    def check(value):
        if isinstance(value, dict):
            if forbidden & value.keys():
                raise SuiteError('public raster artifact exposes private runtime fields')
            for item in value.values(): check(item)
        elif isinstance(value, list):
            for item in value: check(item)
    check(artifact)
    runs = array(result.get('meta_scan_runs'), 'automatic scan runs')
    if len(runs) != 1 or runs[0].get('status') != 'submitted' or runs[0].get('target_locator') != target_locator:
        raise SuiteError('Develop did not automatically submit the target Meta scan')
    scan_id = runs[0].get('execution_id')
    if not isinstance(scan_id, str) or not scan_id:
        raise SuiteError('automatic scan execution_id is missing')
    return facts, scan_id


def validate_target_item(item, expectation, physical_evidence):
    if physical_evidence.get('cog_valid') is not True or not isinstance(physical_evidence.get('has_overviews'),bool):
        raise SuiteError('physical COG layout or overview evidence is incomplete')
    attributes = obj(item.get('attributes'), 'target attributes')
    if obj(attributes.get('item'), 'item facts').get('format') != 'tiff':
        raise SuiteError('Meta did not identify the target TIFF')
    media = obj(obj(attributes.get('type_info'), 'type info').get('media'), 'media facts')
    if (media.get('width'), media.get('height')) != (expectation['width'], expectation['height']):
        raise SuiteError('Meta target dimensions are invalid')
    tiff = obj(obj(attributes.get('format_info'), 'format info').get('tiff'), 'TIFF facts')
    has_overviews = physical_evidence['has_overviews']
    # Meta exposes a lightweight hint; GDAL validates the actual COG layout.
    profile_hint = 'cog' if has_overviews else 'geotiff'
    if (tiff.get('profile') != profile_hint or tiff.get('is_tiled') is not True
        or tiff.get('has_overviews') is not has_overviews):
        raise SuiteError('Meta TIFF structure/profile hint differs from the physical COG')
    spatial = obj(obj(attributes.get('capabilities'), 'capabilities').get('spatial'), 'spatial facts')
    extent = array(spatial.get('extent'), 'spatial extent')
    if spatial.get('srid') != expectation['extent_srid'] or len(extent) != 4 or any(
        not math.isclose(actual, expected, abs_tol=1e-10) for actual, expected in zip(extent, expectation['extent'])
    ):
        raise SuiteError('Meta target CRS/extent are invalid')


def wait_lineage(client, source_id, target_id, execution_id, timeout):
    query = urllib.parse.urlencode({'subject_kind': 'data_item', 'item_id': target_id, 'direction': 'upstream'})
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        graph = obj(client.request('GET', '/api/v1/meta/lineage/graph?' + query, (200,)).payload, 'lineage graph')
        if graph.get('truncated'):
            raise SuiteError('raster lineage graph is truncated')
        edges = array(graph.get('edges'), 'lineage edges')
        for edge in edges:
            if (edge.get('source', {}).get('item_id'), edge.get('target', {}).get('item_id'),
                edge.get('evidence', {}).get('execution_id'), edge.get('status'), edge.get('relation_kind')) == (source_id, target_id, execution_id, 'active', 'derive'):
                return {'source_item_id': source_id, 'target_item_id': target_id, 'execution_id': execution_id}
        time.sleep(1)
    raise SuiteError('automatic raster lineage did not converge')


def duplicate_create_diagnostic(repository, env, execution_id, timeout):
    """Confirm the Owner's exact failure in this disposable deployment's logs.

    Tenant observation intentionally omits raw errors. The Hosted harness has
    access to its own standard runtime logs; only a bounded cause is reported.
    """
    root = Path(env.get('ADDP_RUNTIME_LOG_ROOT', repository / 'logs/runtime'))
    if not root.is_absolute():
        root = repository / root
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        for path in (root / 'develop').glob('*/*.jsonl'):
            for line in path.read_text().splitlines():
                try:
                    event = json.loads(line)
                except json.JSONDecodeError:
                    continue  # The capture process may be appending the last line.
                message = event.get('message', '')
                if (event.get('module_name') == 'develop' and event.get('role') == 'backend'
                    and f'[DevExecutor] 引擎执行完成: execution_id={execution_id} errorMessage=' in message
                    and message.endswith('任务 save 执行失败: target object already exists: raster-target/result.cog.tif')):
                    return {'execution_id': execution_id, 'cause': 'target_already_exists',
                            'source': 'develop_runtime_log'}
        time.sleep(1)
    raise SuiteError('duplicate create is missing its exact Owner target-conflict diagnostic')


def physical(repository, env, action):
    result = subprocess.run([sys.executable, 'business/scripts/online-raster-minio-fixture.py', action],
                            cwd=repository, env=env, capture_output=True, text=True, timeout=300)
    if result.returncode:
        raise SuiteError(f'physical raster {action} failed ({result.returncode})')
    payload = obj(json.loads(result.stdout), 'physical raster evidence')
    case_name = action.removeprefix('verify-')
    expectation = fixture.utility_expectation() if case_name in fixture.UTILITY_CASES or action == 'verify-utility-queries' else fixture.computed_expectation(case_name) if case_name in fixture.GRID_CASES + fixture.MULTIBAND_CASES + fixture.FOUNDATION_CASES + fixture.RECLASS_CASES + fixture.AGGREGATE_CASES + fixture.OUTSIDE_CASES else fixture.artifact_expectation(action.startswith('verify-mosaic-'))
    if payload.get('cog_valid') is not True or payload.get('source_unchanged') is not True or payload.get('valid_pixels') != expectation['valid_pixels']:
        raise SuiteError('physical raster verification is incomplete')
    return payload


def browser(repository, env, evidence):
    report_path = Path(env['ADDP_ONLINE_ARTIFACT_DIR']) / f'raster-workflow-{evidence["case_name"]}-browser.json'
    report_path.unlink(missing_ok=True)
    browser_env = dict(env, ADDP_ONLINE_RASTER_EVIDENCE=json.dumps(evidence))
    result = subprocess.run(['npm', 'exec', '--', 'playwright', 'test', 'e2e/online/raster-workflow.spec.js',
                             '--config=playwright.online.config.js'], cwd=repository / 'console/frontend',
                            env=browser_env, stdout=sys.stderr, stderr=sys.stderr, timeout=240)
    if result.returncode or not report_path.is_file():
        raise SuiteError('raster Console acceptance failed or report is missing')
    report = obj(json.loads(report_path.read_text()), 'browser report')
    expected = {**evidence, 'schema_version': 'addp.raster-workflow-browser/v1', 'result': 'passed'}
    if report != expected:
        raise SuiteError('raster Console report does not bind the exact run/User/execution/items')
    return report


def inspect_output(repository, env, client, source, source_locator, target_engine, target_name,
                   execution_id, case_name, identity, timeout, browser_runner, expectation, physical_evidence, source_locators):
    target = support.find_fixture_item(client, target_engine, 'raster-target/' + target_name, 'COG target')
    # No manual target scan or collect: both facts must converge automatically.
    target = obj(client.request('GET', f'/api/v1/meta/items/{target["id"]}', (200,)).payload, 'target DataItem')
    validate_target_item(target, expectation, physical_evidence)
    lineage = wait_lineage(client, source['id'], target['id'], execution_id, timeout)
    target_locator = f'addp://engine/{target_engine}/path/raster-target/{target_name}?type=object'
    evidence = browser_runner(repository, env, {
        'run_id': env['ADDP_ONLINE_TEST_RUN_ID'], 'principal_id': identity['principal_id'],
        'tenant_id': identity['tenant_id'], 'execution_id': execution_id,
        'source_item_id': source['id'], 'target_item_id': target['id'],
        'source_locator': source_locator, 'source_locators': source_locators, 'target_locator': target_locator,
        'case_name': case_name, 'source_name': source['full_name'].rsplit('/', 1)[-1], 'target_name': target_name,
    })
    return lineage, evidence


def run_scenario(repository, env, client, physical_runner=physical, browser_runner=browser, diagnostic_runner=duplicate_create_diagnostic):
    tenant = positive(env['ADDP_ONLINE_TEST_TENANT_ID'], 'tenant')
    if tenant <= 1:
        raise SuiteError('raster acceptance forbids the default Tenant')
    source_engine = positive(env['ADDP_ONLINE_RASTER_SOURCE_ENGINE_ID'], 'source engine')
    target_engine = positive(env['ADDP_ONLINE_RASTER_TARGET_ENGINE_ID'], 'target engine')
    if source_engine == target_engine:
        raise SuiteError('raster fixture requires distinct source and target Engines')
    timeout = float(env.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS', '180'))
    identity = validate_identity(client, tenant)
    engines = array(client.request('GET', '/api/v1/develop/workflow-engines', (200,)).payload, 'workflow engines')
    runtime = [item for item in engines if item.get('engine_type') == 'geopython_workflow' and item.get('connection_status') == 'online']
    if len(runtime) != 1:
        raise SuiteError('exactly one online GeoPython Workflow Runtime must be registered')
    engine_id = positive(runtime[0].get('id'), 'runtime engine')
    support.wait_for_meta_scan(client, source_engine, time.monotonic() + timeout)
    source = support.find_fixture_item(client, source_engine, 'raster-source/source.tif', 'raster source')
    source_locator = support.build_item_locator(source_engine, source)
    target_locator = f'addp://engine/{target_engine}/path/raster-target/result.cog.tif?type=object'
    executions = []
    physical_evidence = []
    last_scan = ''
    conflict_evidence = None
    for mode, expression, status in [('create', 'b2-b1', 'success'), ('create', 'b2+b1', 'failed'), ('replace', 'b2+b1', 'success')]:
        identifier = submit(client, engine_id, workflow(source_locator, target_engine, expression, mode))
        execution = wait_execution(client, 'develop', identifier, timeout, status)
        metadata = obj(execution.get('metadata', {}), 'execution metadata')
        if status == 'success':
            facts, last_scan = validate_success(execution, [source_locator], target_locator, mode)
            wait_execution(client, 'meta', last_scan, timeout)
        else:
            if execution.get('outputs') or metadata.get('lineage_facts'):
                raise SuiteError('failed create published stable outputs or success lineage')
            if obj(execution.get('error_details'), 'failed create error').get('category') != 'execution_failed':
                raise SuiteError('duplicate create has an unexpected failure category')
            conflict_evidence = diagnostic_runner(repository, env, identifier, timeout)
        monitor = obj(client.request('GET', f'/api/v1/monitor/executions/by-execution-id/{identifier}', (200,)).payload, 'Monitor execution')
        if monitor.get('status') != status or monitor.get('module') != 'develop':
            raise SuiteError('Monitor does not match the Develop execution status/owner')
        if status == 'success':
            if obj(monitor.get('metadata'), 'Monitor metadata').get('lineage_facts') != facts:
                raise SuiteError('Monitor lineage differs from Develop facts')
        native = physical_runner(repository, env, 'verify-replace' if mode == 'replace' else 'verify-create')
        if len(physical_evidence) == 1 and native.get('sha256') != physical_evidence[0].get('sha256'):
            raise SuiteError('failed create modified the existing target')
        if mode == 'replace' and native.get('sha256') == physical_evidence[0].get('sha256'):
            raise SuiteError('replace did not change the target pixels')
        physical_evidence.append(native)
        executions.append({'execution_id': identifier, 'write_mode': mode, 'status': status})
    lineage, browser_evidence = inspect_output(repository, env, client, source, source_locator,
        target_engine, 'result.cog.tif', executions[-1]['execution_id'], 'band-math', identity,
        timeout, browser_runner, fixture.artifact_expectation(), physical_evidence[-1], [source_locator])
    spatial_source = support.find_fixture_item(client, source_engine, 'raster-source/spatial.tif', 'spatial source')
    spatial_locator = support.build_item_locator(source_engine, spatial_source)
    spatial_cases = []
    for overlap in ('first', 'last'):
        name = f'mosaic-{overlap}.cog.tif'
        locator = f'addp://engine/{target_engine}/path/raster-target/{name}?type=object'
        identifier = submit(client, engine_id, spatial_workflow(spatial_locator, target_engine, overlap))
        execution = wait_execution(client, 'develop', identifier, timeout)
        facts, scan_id = validate_success(execution, [spatial_locator], locator, 'create', fixture.artifact_expectation(True))
        wait_execution(client, 'meta', scan_id, timeout)
        monitor = obj(client.request('GET', f'/api/v1/monitor/executions/by-execution-id/{identifier}', (200,)).payload, 'Monitor spatial execution')
        if (monitor.get('status') != 'success' or monitor.get('module') != 'develop'
            or obj(monitor.get('metadata'), 'Monitor metadata').get('lineage_facts') != facts):
            raise SuiteError('Monitor spatial execution differs from Develop status/lineage')
        native = physical_runner(repository, env, 'verify-mosaic-' + overlap)
        if native.get('overlap') != overlap or native.get('baseline_sha256') != physical_evidence[-1]['sha256']:
            raise SuiteError('spatial execution modified the baseline or has the wrong overlap evidence')
        if overlap == 'last' and (native.get('first_sha256') != spatial_cases[0]['physical']['sha256']
                                  or native.get('sha256') == spatial_cases[0]['physical']['sha256']):
            raise SuiteError('last mosaic modified the first artifact or did not change overlap pixels')
        graph, browser_report = inspect_output(repository, env, client, spatial_source, spatial_locator,
            target_engine, name, identifier, 'mosaic-' + overlap, identity, timeout,
            browser_runner, fixture.artifact_expectation(True), native, [spatial_locator])
        spatial_cases.append({'case_name': 'mosaic-' + overlap, 'execution_id': identifier,
            'automatic_target_scan_execution_id': scan_id, 'lineage': graph, 'physical': native, 'browser': browser_report})
    analysis_cases = []
    for case_name, expectation in fixture.analysis_expectations().items():
        identifier = submit(client, engine_id, analysis_workflow(source_locator, case_name))
        execution = wait_execution(client, 'develop', identifier, timeout)
        numeric_result = validate_analysis(execution, expectation)
        monitor = obj(client.request('GET', f'/api/v1/monitor/executions/by-execution-id/{identifier}', (200,)).payload, 'Monitor analysis execution')
        if (monitor.get('status') != 'success' or monitor.get('module') != 'develop'
            or obj(monitor.get('metadata', {}), 'Monitor analysis metadata').get('lineage_facts')):
            raise SuiteError('Monitor analysis status/owner or transient output semantics are invalid')
        native = physical_runner(repository, env, 'verify-analysis')
        if (native.get('sha256') != physical_evidence[-1]['sha256']
            or native.get('first_sha256') != spatial_cases[0]['physical']['sha256']
            or native.get('last_sha256') != spatial_cases[1]['physical']['sha256']):
            raise SuiteError('raster analysis modified an existing persisted artifact')
        browser_report = browser_runner(repository, env, {
            'run_id': env['ADDP_ONLINE_TEST_RUN_ID'], 'principal_id': identity['principal_id'],
            'tenant_id': identity['tenant_id'], 'execution_id': identifier,
            'source_item_id': source['id'], 'source_locator': source_locator,
            'source_name': 'source.tif', 'case_name': case_name, 'result_kind': 'json', 'expected_result': numeric_result,
        })
        analysis_cases.append({'case_name': case_name, 'execution_id': identifier,
            'numeric_result': numeric_result, 'physical': native, 'browser': browser_report})
    grid_cases = []
    multiband_cases = []
    preserved = {'result.cog.tif': physical_evidence[-1]['sha256'],
                 'mosaic-first.cog.tif': spatial_cases[0]['physical']['sha256'],
                 'mosaic-last.cog.tif': spatial_cases[1]['physical']['sha256']}
    for case_name in fixture.GRID_CASES + fixture.MULTIBAND_CASES:
        expectation = fixture.computed_expectation(case_name)
        case_source, case_locator = spatial_source, spatial_locator
        definition = grid_workflow
        if case_name in fixture.MULTIBAND_CASES:
            joint = case_name.endswith('-joint')
            case_source = support.find_fixture_item(client, target_engine if joint else source_engine,
                ('raster-target/' if joint else 'raster-source/') + fixture.multiband_source_name(case_name), 'multiband source')
            case_locator = support.build_item_locator(target_engine if joint else source_engine, case_source)
            definition = multiband_workflow
        name = case_name + '.cog.tif'
        locator = f'addp://engine/{target_engine}/path/raster-target/{name}?type=object'
        identifier = submit(client, engine_id, definition(case_locator, target_engine, case_name))
        execution = wait_execution(client, 'develop', identifier, timeout)
        facts, scan_id = validate_success(execution, [case_locator], locator, 'create', expectation)
        wait_execution(client, 'meta', scan_id, timeout)
        monitor = obj(client.request('GET', f'/api/v1/monitor/executions/by-execution-id/{identifier}', (200,)).payload, 'Monitor grid execution')
        if (monitor.get('status') != 'success' or monitor.get('module') != 'develop'
            or obj(monitor.get('metadata'), 'Monitor metadata').get('lineage_facts') != facts):
            raise SuiteError('Monitor grid execution differs from Develop status/lineage')
        native = physical_runner(repository, env, 'verify-' + case_name)
        if native.get('case_name') != case_name or native.get('preserved_sha256') != preserved:
            raise SuiteError('grid execution changed an existing artifact or omitted preservation evidence')
        if case_name in fixture.MULTIBAND_CASES:
            counts = [expectation['valid_pixels']] * (1 if case_name.endswith('-joint') else 2)
            partial = 64 if case_name == 'multiband-bilinear' else 3 if case_name == 'multiband-alpha' else 0
            if (native.get('band_valid_pixels') != counts
                or native.get('invalid_pixels') != expectation['width'] * expectation['height'] - counts[0]
                or native.get('partial_alpha_pixels') != partial
                or native.get('band_nodata') != expectation['band_nodata']
                or native.get('band_nodata_is_nan') is not (expectation['band_nodata'] is None)):
                raise SuiteError('multiband independent validity/partial alpha evidence is incomplete')
        graph, browser_report = inspect_output(repository, env, client, case_source, case_locator,
            target_engine, name, identifier, case_name, identity, timeout, browser_runner, expectation, native, [case_locator])
        cases = multiband_cases if case_name in fixture.MULTIBAND_CASES else grid_cases
        cases.append({'case_name': case_name, 'execution_id': identifier,
            'automatic_target_scan_execution_id': scan_id, 'lineage': graph, 'physical': native, 'browser': browser_report})
        preserved[name] = native['sha256']
    utility_cases = []
    utility_sources = {}
    non_cog_source = support.find_fixture_item(client, source_engine, 'raster-source/' + fixture.NON_COG_SOURCE, 'non-COG source')
    for case_name in fixture.UTILITY_CASES:
        name = case_name + '.cog.tif'
        locator = f'addp://engine/{target_engine}/path/raster-target/{name}?type=object'
        identifier = submit(client, engine_id, utility_workflow(source_locator, target_engine, case_name))
        execution = wait_execution(client, 'develop', identifier, timeout)
        output_node = 'convert' if case_name == 'to-cog' else 'save'
        facts, scan_id = validate_success(execution, [source_locator], locator, 'create', fixture.utility_expectation(), output_node)
        wait_execution(client, 'meta', scan_id, timeout)
        monitor = obj(client.request('GET', f'/api/v1/monitor/executions/by-execution-id/{identifier}', (200,)).payload, 'Monitor utility execution')
        if (monitor.get('status') != 'success' or monitor.get('module') != 'develop'
            or obj(monitor.get('metadata'), 'Monitor utility metadata').get('lineage_facts') != facts):
            raise SuiteError('Monitor utility execution differs from Develop status/lineage')
        native = physical_runner(repository, env, 'verify-' + case_name)
        expected_levels = [[128, 128], [64, 64]] if case_name == 'build-overviews' else [[128, 128]]
        artifact = execution['metadata']['result']['final_result']
        if (native.get('preserved_sha256') != preserved or native.get('overview_sizes') != expected_levels
            or native.get('band_valid_pixels') != [65535, 65535]
            or type(artifact.get('size_bytes')) is not int
            or positive(native.get('size_bytes'), 'physical utility size') != artifact.get('size_bytes')
            or any(band.get('overviews') != expected_levels for band in artifact['bands'])):
            raise SuiteError('utility workflow lost physical size, band validity, explicit levels or existing artifacts')
        graph, browser_report = inspect_output(repository, env, client, source, source_locator,
            target_engine, name, identifier, case_name, identity, timeout, browser_runner, fixture.utility_expectation(), native, [source_locator])
        utility_sources[case_name] = support.find_fixture_item(client, target_engine, 'raster-target/' + name, 'utility source')
        if positive(utility_sources[case_name].get('size_bytes'), 'utility DataItem size') != native['size_bytes']:
            raise SuiteError('utility DataItem size differs from the physical artifact')
        utility_cases.append({'case_name': case_name, 'execution_id': identifier,
            'automatic_target_scan_execution_id': scan_id, 'lineage': graph, 'physical': native, 'browser': browser_report})
        preserved[name] = native['sha256']
    for case_name in fixture.UTILITY_JSON_CASES:
        query_source = non_cog_source if case_name == 'validate-cog-invalid' else utility_sources[
            'build-overviews' if case_name == 'info-overviews' else 'to-cog']
        query_locator = support.build_item_locator(source_engine if case_name == 'validate-cog-invalid' else target_engine, query_source)
        identifier = submit(client, engine_id, utility_workflow(query_locator, target_engine, case_name))
        execution = wait_execution(client, 'develop', identifier, timeout)
        native = physical_runner(repository, env, 'verify-utility-queries')
        if native.get('preserved_sha256') != preserved:
            raise SuiteError('read-only utility workflow modified an accepted artifact')
        result = validate_utility_json(execution, case_name, native)
        monitor = obj(client.request('GET', f'/api/v1/monitor/executions/by-execution-id/{identifier}', (200,)).payload, 'Monitor utility query')
        if (monitor.get('status') != 'success' or monitor.get('module') != 'develop'
            or obj(monitor.get('metadata', {}), 'Monitor query metadata').get('lineage_facts')):
            raise SuiteError('Monitor utility query has invalid status/owner or persistent lineage')
        browser_report = browser_runner(repository, env, {
            'run_id': env['ADDP_ONLINE_TEST_RUN_ID'], 'principal_id': identity['principal_id'],
            'tenant_id': identity['tenant_id'], 'execution_id': identifier,
            'source_item_id': query_source['id'], 'source_locator': query_locator,
            'source_name': query_source['full_name'].rsplit('/', 1)[-1], 'case_name': case_name,
            'result_kind': 'json', 'expected_result': result,
        })
        utility_cases.append({'case_name': case_name, 'execution_id': identifier,
            'json_result': result, 'physical': native, 'browser': browser_report})
    foundation_cases = []
    reclass_cases = []
    aggregate_cases = []
    outside_cases = []
    reference_source = support.find_fixture_item(client, target_engine, 'raster-target/mosaic-first.cog.tif', 'reference raster')
    reference_locator = support.build_item_locator(target_engine, reference_source)
    for case_name in fixture.FOUNDATION_CASES + fixture.RECLASS_CASES + fixture.AGGREGATE_CASES + fixture.OUTSIDE_CASES:
        single = case_name in fixture.RECLASS_CASES + fixture.AGGREGATE_CASES + fixture.OUTSIDE_CASES
        case_sources = [(source, source_locator, '')] if single else [
            (spatial_source, spatial_locator, '-spatial'), (reference_source, reference_locator, '-reference')]
        source_locators = [case_locator for _, case_locator, _ in case_sources]
        expectation = fixture.computed_expectation(case_name)
        name = case_name + '.cog.tif'
        locator = f'addp://engine/{target_engine}/path/raster-target/{name}?type=object'
        definition = (outside_workflow(source_locator, target_engine, case_name) if case_name in fixture.OUTSIDE_CASES else
                      aggregate_workflow(source_locator, target_engine, case_name) if case_name in fixture.AGGREGATE_CASES else
                      reclass_workflow(source_locator, target_engine, case_name) if case_name in fixture.RECLASS_CASES else
                      foundation_workflow(spatial_locator, reference_locator, target_engine, case_name))
        identifier = submit(client, engine_id, definition)
        execution = wait_execution(client, 'develop', identifier, timeout)
        facts, scan_id = validate_success(execution, source_locators, locator, 'create', expectation)
        wait_execution(client, 'meta', scan_id, timeout)
        monitor = obj(client.request('GET', f'/api/v1/monitor/executions/by-execution-id/{identifier}', (200,)).payload, 'Monitor multi-raster execution')
        if (monitor.get('status') != 'success' or monitor.get('module') != 'develop'
            or obj(monitor.get('metadata'), 'Monitor metadata').get('lineage_facts') != facts):
            raise SuiteError('Monitor multi-raster execution differs from Develop status/lineage')
        native = physical_runner(repository, env, 'verify-' + case_name)
        artifact = execution['metadata']['result']['final_result']
        colors = ['Gray', 'Undefined', 'Alpha'] if case_name in fixture.OUTSIDE_CASES else ['Red', 'Green', 'Blue'] if case_name == 'multiraster-rgb' else ['Gray']
        levels = [[expectation['width']//2, expectation['height']//2]]
        if (native.get('case_name') != case_name or native.get('preserved_sha256') != preserved
            or native.get('cog_valid') is not True or native.get('source_unchanged') is not True
            or native.get('has_overviews') is not True or native.get('overview_sizes') != levels
            or native.get('color_interpretations') != colors
            or [band.get('color_interpretation') for band in artifact['bands']] != colors
            or any(band.get('overviews') != levels for band in artifact['bands'])
            or native.get('valid_pixels') != expectation['valid_pixels']
            or native.get('invalid_pixels') != expectation['width'] * expectation['height'] - expectation['valid_pixels']
            or native.get('band_valid_pixels') != [expectation['valid_pixels']] * (expectation['band_count'] - (case_name in fixture.OUTSIDE_CASES))
            or type(artifact.get('size_bytes')) is not int
            or positive(native.get('size_bytes'), 'physical multi-raster size') != artifact['size_bytes']):
            raise SuiteError('multi-raster physical size, bands, colour, validity or preservation evidence differs')
        target = support.find_fixture_item(client, target_engine, 'raster-target/' + name, 'multi-raster target')
        if positive(target.get('size_bytes'), 'multi-raster DataItem size') != native['size_bytes']:
            raise SuiteError('multi-raster DataItem size differs from the physical artifact')
        graphs, browsers = [], []
        for case_source, case_locator, suffix in case_sources:
            graph, report = inspect_output(repository, env, client, case_source, case_locator, target_engine,
                name, identifier, case_name + suffix, identity, timeout, browser_runner, expectation, native, source_locators)
            graphs.append(graph)
            browsers.append(report)
        cases = outside_cases if case_name in fixture.OUTSIDE_CASES else aggregate_cases if case_name in fixture.AGGREGATE_CASES else reclass_cases if case_name in fixture.RECLASS_CASES else foundation_cases
        cases.append({'case_name': case_name, 'execution_id': identifier,
            'automatic_target_scan_execution_id': scan_id, 'lineage': graphs[0] if single else graphs,
            'physical': native, 'browser': browsers[0] if single else browsers})
        preserved[name] = native['sha256']
    footprint_cases = []
    footprint_source = support.find_fixture_item(client, source_engine, 'raster-source/multiband.tif', 'footprint source')
    footprint_locator = support.build_item_locator(source_engine, footprint_source)
    for case_name in fixture.FOOTPRINT_CASES:
        identifier = submit(client, engine_id, footprint_workflow(footprint_locator, case_name))
        execution = wait_execution(client, 'develop', identifier, timeout)
        result = validate_footprint(execution, case_name)
        monitor = obj(client.request('GET', f'/api/v1/monitor/executions/by-execution-id/{identifier}', (200,)).payload, 'Monitor footprint execution')
        if (monitor.get('status') != 'success' or monitor.get('module') != 'develop'
            or obj(monitor.get('metadata', {}), 'Monitor footprint metadata').get('lineage_facts')):
            raise SuiteError('Monitor footprint has invalid status/owner or persistent lineage')
        native = physical_runner(repository, env, 'verify-clip-outside')
        final_name = fixture.OUTSIDE_CASES[-1] + '.cog.tif'
        if (native.get('sha256') != preserved[final_name] or native.get('preserved_sha256') !=
            {name: digest for name, digest in preserved.items() if name != final_name}):
            raise SuiteError('footprint execution changed an accepted artifact')
        report = browser_runner(repository, env, {
            'run_id': env['ADDP_ONLINE_TEST_RUN_ID'], 'principal_id': identity['principal_id'],
            'tenant_id': identity['tenant_id'], 'execution_id': identifier,
            'source_item_id': footprint_source['id'], 'source_locator': footprint_locator,
            'source_name': 'multiband.tif', 'case_name': case_name, 'result_kind': 'json', 'expected_result': result,
        })
        footprint_cases.append({'case_name': case_name, 'execution_id': identifier,
            'json_result': result, 'physical': native, 'browser': report})
    return {'schema_version': SCHEMA, 'suite': 'raster-workflow', 'result': 'passed',
            'run_id': env['ADDP_ONLINE_TEST_RUN_ID'], 'identity': identity, 'executions': executions,
            'automatic_target_scan_execution_id': last_scan, 'lineage': lineage,
            'physical': physical_evidence, 'browser': browser_evidence, 'duplicate_create': conflict_evidence,
            'spatial_cases': spatial_cases,
            'analysis_cases': analysis_cases, 'grid_cases': grid_cases, 'multiband_cases': multiband_cases,
            'utility_cases': utility_cases, 'foundation_cases': foundation_cases, 'reclass_cases': reclass_cases,
            'aggregate_cases': aggregate_cases, 'outside_cases': outside_cases, 'footprint_cases': footprint_cases,
            'cleanup': {'scope': 'disposable-hosted-deployment', 'owner': 'online-hosted-raster-gate.sh'}}



def prepare_resource_policy(platform, tenant, consumer):
    engines = array(platform.request('GET', '/api/v1/system/platform/engine-raster-policies/engines', (200,)).payload, 'raster resource engines')
    if len(engines) != 1:
        raise SuiteError('resource policy acceptance requires exactly one shared GeoPython instance')
    engine = positive(engines[0].get('id'), 'resource policy engine')
    platform_path = f'/api/v1/system/platform/engine-raster-policies/{engine}'
    tenant_path = f'/api/v1/system/tenant/engine-raster-policies/{engine}'
    consumer.request('GET', tenant_path, (403,))
    tenant.request('GET', platform_path, (403,))
    original = obj(obj(platform.request('GET', platform_path, (200,)).payload, 'policy view')['policy'], 'policy')
    fields = ('running', 'waiting', 'cache_mib', 'default_tenant_running', 'default_tenant_waiting')
    if {key: original[key] for key in fields} != dict(running=2, waiting=2, cache_mib=256, default_tenant_running=2, default_tenant_waiting=2):
        raise SuiteError('fresh Hosted raster default differs from the declared definition')
    body = dict(version=original['version'], running=1, waiting=1, cache_mib=256, default_tenant_running=1, default_tenant_waiting=1)
    saved = obj(platform.request('PUT', platform_path, (200,), body).payload, 'saved platform policy')
    platform.request('PUT', platform_path, (409,), body)
    quota = obj(obj(tenant.request('GET', tenant_path, (200,)).payload, 'tenant policy')['quota'], 'quota')
    update = dict(version=quota['version'], running=1, waiting=0)
    tenant.request('PUT', tenant_path, (400,), {**update, 'cache_mib': 128})
    tenant.request('PUT', tenant_path, (400,), {**update, 'tenant_id': 1})
    changed = obj(tenant.request('PUT', tenant_path, (200,), update).payload, 'saved tenant quota')
    tenant.request('PUT', tenant_path, (409,), update)
    if changed['effective_running'] != 1 or changed['effective_waiting'] != 0:
        raise SuiteError('tenant effective resource quota differs from the saved policy')
    return {'engine_id': engine, 'platform_path': platform_path, 'tenant_path': tenant_path,
            'original': original, 'platform_version': saved['policy']['version'], 'quota_version': changed['quota']['version']}


def finish_resource_policy(platform, tenant, setup, timeout):
    def applied(version, restart):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            view = obj(platform.request('GET', setup['platform_path'], (200,)).payload, 'applied resource policy')
            facts = view.get('runtime')
            if (isinstance(facts, dict) and facts.get('enabled') is True and facts.get('applied_version') == version
                    and facts.get('cache_mib') == 256 and facts.get('pending_restart') is restart):
                return facts
            time.sleep(1)
        raise SuiteError('Runtime did not apply the saved resource policy/cache restart status')
    facts = applied(setup['platform_version'], False)
    if facts['running_limit'] != 1 or facts['waiting_limit'] != 1 or facts['running'] != 0 or facts['waiting'] != 0:
        raise SuiteError('Runtime resource counts/limits do not match the completed raster scene')
    body = {key: setup['original'][key] for key in ('running', 'waiting', 'cache_mib', 'default_tenant_running', 'default_tenant_waiting')}
    changed = platform.request('PUT', setup['platform_path'], (200,), {**body, 'version': setup['platform_version'], 'cache_mib': 128}).payload
    pending = applied(changed['policy']['version'], True)
    restored = platform.request('PUT', setup['platform_path'], (200,), {**body, 'version': changed['policy']['version']}).payload
    quota = tenant.request('PUT', setup['tenant_path'], (200,), {'version': setup['quota_version'], 'running': None, 'waiting': None}).payload
    recovered = applied(restored['policy']['version'], False)
    if quota['quota']['running'] is not None or quota['quota']['waiting'] is not None or quota['effective_running'] != 2 or quota['effective_waiting'] != 2:
        raise SuiteError('tenant quota did not restore inheritance')
    return {'engine_id': setup['engine_id'], 'configured_running': 1, 'configured_waiting': 1, 'tenant_waiting': 0,
            'platform_applied_version': facts['applied_version'], 'cache_restart_verified_version': pending['applied_version'],
            'restored_applied_version': recovered['applied_version'], 'result': 'passed',
            'management_identity': 'separate-disposable-platform-and-tenant-users', 'consumer_admin_permission': False}

def main():
    env = dict(os.environ)
    required = ('ADDP_ONLINE_TEST_RUN_ID', 'ADDP_ONLINE_TEST_TENANT_ID', 'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN',
                'ADDP_ONLINE_RASTER_SOURCE_ENGINE_ID', 'ADDP_ONLINE_RASTER_TARGET_ENGINE_ID',
                'ADDP_ONLINE_TEST_USER_USERNAME', 'ADDP_ONLINE_TEST_USER_PASSWORD',
                'ADDP_ONLINE_ARTIFACT_DIR', 'GATEWAY_URL', 'CONSOLE_URL',
                'ADDP_ONLINE_RASTER_POLICY_PLATFORM_TOKEN', 'ADDP_ONLINE_RASTER_POLICY_TENANT_TOKEN')
    if any(not env.get(key) for key in required):
        raise SuiteError('required raster Online environment is missing')
    client = GatewayClient(env['GATEWAY_URL'], env['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'], 30)
    platform = GatewayClient(env['GATEWAY_URL'], env['ADDP_ONLINE_RASTER_POLICY_PLATFORM_TOKEN'], 30)
    tenant = GatewayClient(env['GATEWAY_URL'], env['ADDP_ONLINE_RASTER_POLICY_TENANT_TOKEN'], 30)
    setup = prepare_resource_policy(platform, tenant, client)
    report = run_scenario(Path(__file__).resolve().parents[2], env, client)
    report['resource_policy'] = finish_resource_policy(platform, tenant, setup, 30)
    print(json.dumps(report, ensure_ascii=False, sort_keys=True))


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print(f'Raster Online acceptance failed: {error if isinstance(error, SuiteError) else type(error).__name__}', file=sys.stderr)
        raise SystemExit(1)

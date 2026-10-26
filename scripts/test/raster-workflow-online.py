#!/usr/bin/env python3
"""Real Develop raster execution -> automatic Meta scan/lineage -> Monitor/Console."""
from __future__ import annotations

import hashlib
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
EDITOR_PERMISSIONS = PERMISSIONS | {'develop.task.create', 'develop.task.delete'}
SCHEMA = 'addp.raster-workflow-online/v2'


def validate_identity(client, tenant_id, *, required_permissions=PERMISSIONS):
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
    if roles & support.FORBIDDEN_ADMIN_ROLES or permissions != required_permissions:
        raise SuiteError('raster User must have exactly the scene permissions and no administrator role')
    return {'principal_id': str(positive(principal.get('id'), 'principal.id')), 'tenant_id': str(tenant_id),
            'principal_type': 'user', 'permissions_verified': sorted(required_permissions)}


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
            'input_raster': reference('right_math'), 'boundary_crs': 'EPSG:3857', 'bbox': [96, 0, 176, 256]}},
        {'id': 'right_tail_clip', 'operator': 'raster_clip', 'depends_on': ['right_math'], 'params': {
            'input_raster': reference('right_math'), 'boundary_crs': 'EPSG:3857', 'bbox': [176, 0, 256, 256]}},
        {'id': 'mosaic', 'operator': 'raster_mosaic', 'depends_on': ['left_clip', 'right_clip', 'right_tail_clip'], 'params': {
            'input_rasters': [reference('left_clip'), reference('right_clip'), reference('right_tail_clip')],
            'target_crs': 'EPSG:3857', 'resolution': [1, 1], 'overlap': overlap, 'resampling': 'nearest'}},
        {'id': 'save', 'operator': 'raster_save', 'depends_on': ['mosaic'], 'params': {
            'input_raster': reference('mosaic'),
            'target_parent_locator': f'addp://engine/{target_engine_id}/path/raster-target?type=bucket',
            'target_name': f'mosaic-{overlap}.cog.tif', 'write_mode': 'create', 'profile': 'cog', 'blocksize': 128}},
    ]}


def mosaic_source_workflow(sources, target_engine_id, case_name):
    if (case_name not in fixture.MOSAIC_CASES or set(sources) != set(fixture.MOSAIC_SOURCE_WINDOWS)
        or len(set(sources.values())) != 3):
        raise SuiteError('independent mosaic requires three distinct registered sources')
    nodes = {name: 'load_' + name.removeprefix('mosaic-').removesuffix('.tif') for name in sources}
    ref = lambda name: {'$ref': name, 'port': 'default'}
    tasks = [{'id': nodes[name], 'operator': 'raster_load', 'depends_on': [], 'params': {'locator': sources[name]}}
             for name in fixture.MOSAIC_SOURCE_WINDOWS]
    tasks.extend([
        {'id': 'mosaic', 'operator': 'raster_mosaic', 'depends_on': sorted(nodes.values()), 'params': {
            'input_rasters': [ref(nodes[name]) for name in fixture.MOSAIC_SOURCE_ORDER],
            'target_crs': 'EPSG:3857', 'resolution': [1, 1],
            'overlap': 'first' if case_name.endswith('-first') else 'last', 'resampling': 'nearest'}},
        {'id': 'save', 'operator': 'raster_save', 'depends_on': ['mosaic'], 'params': {
            'input_raster': ref('mosaic'), 'target_parent_locator': f'addp://engine/{target_engine_id}/path/raster-target?type=bucket',
            'target_name': case_name + '.cog.tif', 'write_mode': 'create', 'profile': 'cog', 'blocksize': 128}}
    ])
    return {'tasks': tasks}


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


def terrain_workflow(source_locator, target_engine_id, case_name):
    if case_name not in fixture.TERRAIN_CASES + ('dem-contour',):
        raise SuiteError('unknown DEM terrain case')
    params = {'input_raster': {'$ref':'load'}, 'elevation_unit':'metre', 'band':1}
    if case_name=='dem-contour':params['levels']=list(fixture.TERRAIN_CONTOUR_LEVELS)
    tasks = [
        {'id':'load','operator':'raster_load','depends_on':[],'params':{'locator':source_locator}},
        {'id':'terrain','operator':case_name.replace('dem-','raster_'),'depends_on':['load'],'params':params}]
    if case_name!='dem-contour':
        tasks += [
            {'id':'overviews','operator':'raster_build_overviews','depends_on':['terrain'],
             'params':{'input_raster':{'$ref':'terrain'},'levels':[2],'resampling':'nearest'}},
            {'id':'save','operator':'raster_save','depends_on':['overviews'],'params':{
                'input_raster':{'$ref':'overviews'},'target_parent_locator':f'addp://engine/{target_engine_id}/path/raster-target?type=bucket',
                'target_name':case_name+'.cog.tif','write_mode':'create','profile':'cog','blocksize':128}}]
    return {'tasks':tasks}


def validate_terrain_contour(execution):
    result = transient_result(execution)
    if result.get('type')!='FeatureCollection':raise SuiteError('DEM contour must be a vector FeatureCollection')
    features=array(result.get('features'),'DEM contour features')
    levels=set()
    for feature in features:
        props=obj(feature.get('properties'),'DEM contour attributes')
        elevation=props.get('elevation')
        geometry=obj(feature.get('geometry'),'DEM contour geometry')
        if (set(props)!={'elevation'} or type(elevation) not in (int,float)
            or elevation not in fixture.TERRAIN_CONTOUR_LEVELS or geometry.get('type')!='LineString'):
            raise SuiteError('DEM contour level/schema differs')
        levels.add(elevation)
        points=array(geometry.get('coordinates'),'DEM contour vertices')
        if len(points)<2:raise SuiteError('DEM contour line is empty')
        for point in points:
            if (not isinstance(point,list) or len(point)!=2 or
                any(type(v) not in (int,float) or not math.isfinite(v) for v in point)):
                raise SuiteError('DEM contour vertex is invalid')
            x,y=point
            # GDAL extends samples to half-cell outer edges. Clamp to the
            # actual sample-centre domain before evaluating the plane.
            expected=100000.5+min(159.5,max(.5,x))+256*(256-min(255.5,max(.5,y))-.5)
            if not (0<=x<=160 and 0<=y<=256) or not math.isclose(expected,elevation,abs_tol=2e-5,rel_tol=0):
                raise SuiteError('DEM contour vertex left its independent elevation plane')
    if levels!=set(fixture.TERRAIN_CONTOUR_LEVELS):raise SuiteError('DEM contour lost a requested level')
    crs=obj(obj(result.get('crs'),'DEM contour CRS').get('properties'),'DEM contour CRS properties').get('name')
    if crs not in ('urn:ogc:def:crs:EPSG::3857','EPSG:3857'):raise SuiteError('DEM contour changed source CRS')
    return result


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
    config = fixture.aggregate_config(case_name)
    tasks = [{'id': 'load', 'operator': 'raster_load', 'depends_on': [], 'params': {'locator': source_locator}}]
    input_node = 'load'
    if config['bands'] is not None:
        tasks.append({'id': 'select', 'operator': 'raster_select_bands', 'depends_on': ['load'],
                      'params': {'input_raster': ref('load'), 'bands': config['bands']}})
        input_node = 'select'
    tasks.extend([
        {'id': 'aggregate', 'operator': 'raster_aggregate', 'depends_on': [input_node], 'params': {
            'input_raster': ref(input_node), 'factors': config['factors'], 'method': config['method']}},
        {'id': 'levels', 'operator': 'raster_build_overviews', 'depends_on': ['aggregate'], 'params': {
            'input_raster': ref('aggregate'), 'levels': [2], 'resampling': 'nearest'}},
        {'id': 'save', 'operator': 'raster_save', 'depends_on': ['levels'], 'params': {
            'input_raster': ref('levels'), 'target_parent_locator': f'addp://engine/{target_engine_id}/path/raster-target?type=bucket',
            'target_name': case_name + '.cog.tif', 'write_mode': 'create', 'profile': 'cog', 'blocksize': 128}},
    ])
    return {'tasks': tasks}


def update_workflow(base_locator, patch_locator, target_engine_id, case_name):
    if case_name not in fixture.UPDATE_CASES: raise SuiteError('unknown update case')
    ref=lambda name: {'$ref':name,'port':'default'}
    return {'tasks':[
        {'id':'base','operator':'raster_load','depends_on':[],'params':{'locator':base_locator}},
        {'id':'patch','operator':'raster_load','depends_on':[],'params':{'locator':patch_locator}},
        {'id':'clip','operator':'raster_clip','depends_on':['patch'],'params':{
            'input_raster':ref('patch'),'boundary_crs':'EPSG:4326','bbox':[110,19.04,111.28,20.32]}},
        {'id':'align','operator':'raster_align','depends_on':['clip','base'],'params':{
            'input_raster':ref('clip'),'reference_raster':ref('base'),'resampling':'nearest'}},
        {'id':'update','operator':'raster_update','depends_on':['base','align'],'params':{
            'input_raster':ref('base'),'update_raster':ref('align')}},
        {'id':'levels','operator':'raster_build_overviews','depends_on':['update'],'params':{
            'input_raster':ref('update'),'levels':[2],'resampling':'nearest'}},
        {'id':'save','operator':'raster_save','depends_on':['levels'],'params':{
            'input_raster':ref('levels'),'target_parent_locator':f'addp://engine/{target_engine_id}/path/raster-target?type=bucket',
            'target_name':case_name+'.cog.tif','write_mode':'create','profile':'cog','blocksize':128}},
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
    elif case_name == 'footprint-band-2':
        params['bands'] = [2]
    elif case_name in fixture.TOPOLOGY_BANDS:
        params['bands'] = [fixture.TOPOLOGY_BANDS[case_name]]
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
    origin_x, column_x, row_x, origin_y, column_y, row_y = fixture.footprint_transform(case_name)
    determinant = column_x*row_y-row_x*column_y
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
                dx, dy = point[0]-origin_x, point[1]-origin_y
                pixel = [(row_y*dx-row_x*dy)/determinant, (column_x*dy-column_y*dx)/determinant]
                if any(not math.isclose(value, round(value), rel_tol=0, abs_tol=1e-8) or
                       not 0 <= round(value) <= fixture.SIZE for value in pixel):
                    raise SuiteError('footprint boundary left the source pixel grid')
                vertices.append(tuple(round(value) for value in pixel))
            if len(vertices) < 4 or vertices[0] != vertices[-1]:
                raise SuiteError('footprint ring must be closed')
            if any(a[0] != b[0] and a[1] != b[1] for a, b in zip(vertices, vertices[1:])):
                raise SuiteError('footprint boundary cut through source cells')
            if case_name in fixture.TOPOLOGY_BANDS:
                if len(set(vertices[:-1])) != len(vertices)-1:
                    raise SuiteError('footprint ring contains a self-touch or zero-length edge')
                segments = list(zip(vertices, vertices[1:]))
                for index, (a, b) in enumerate(segments):
                    for other, (c, d) in enumerate(segments[index+1:], index+1):
                        if other == index+1 or (index == 0 and other == len(segments)-1):
                            continue
                        # Grid-aligned segments intersect iff their closed boxes intersect.
                        if (max(min(a[0], b[0]), min(c[0], d[0])) <= min(max(a[0], b[0]), max(c[0], d[0])) and
                            max(min(a[1], b[1]), min(c[1], d[1])) <= min(max(a[1], b[1]), max(c[1], d[1]))):
                            raise SuiteError('footprint ring has intersecting nonadjacent edges')
            rings.append(vertices)
        if not rings:
            raise SuiteError('footprint polygon has no exterior')
        areas = [abs(sum(a[0]*b[1]-b[0]*a[1] for a, b in zip(ring, ring[1:]))) / 2 for ring in rings]
        area += areas[0] - sum(areas[1:])
        polygons.append(rings)
    if case_name == 'footprint-holes' and (len(polygons) != 2 or sum(len(rings)-1 for rings in polygons) != 1):
        raise SuiteError('footprint hole/island topology differs from the independent source pattern')
    if case_name in fixture.TOPOLOGY_BANDS:
        wanted = (4, 0) if case_name == 'footprint-diagonal' else (1, 2)
        if (len(polygons), sum(len(rings)-1 for rings in polygons)) != wanted:
            raise SuiteError('footprint four-connected/pinch topology differs from the source pattern')
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


def submit(client, engine_id, definition, *, execution_timeout=180):
    response = obj(client.request('POST', '/api/v1/develop/executions', (200,), {
        'dev_type': 'workflow', 'trigger_type': 'manual', 'timeout': execution_timeout,
        'content': {'workflow_definition': definition}, 'execution_config': {'engine_id': engine_id},
    }).payload, 'execution submission')
    value = response.get('execution_id')
    if not isinstance(value, str) or not value:
        raise SuiteError('Develop did not return an execution_id')
    return value


def validate_success(execution, source_locators, target_locator, mode, expectation=None, output_node='save', resource_type='object'):
    expectation = expectation or fixture.artifact_expectation()
    metadata = obj(execution.get('metadata'), 'Develop metadata')
    resource = obj(obj(obj(execution.get('outputs'), 'outputs').get(output_node), 'target output').get('resource'), 'resource')
    if resource != {'locator': target_locator, 'type': resource_type, 'write_mode': mode}:
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
    validate_raster_artifact(artifact, expectation)
    runs = array(result.get('meta_scan_runs'), 'automatic scan runs')
    if len(runs) != 1 or runs[0].get('status') != 'submitted' or runs[0].get('target_locator') != target_locator:
        raise SuiteError('Develop did not automatically submit the target Meta scan')
    scan_id = runs[0].get('execution_id')
    if not isinstance(scan_id, str) or not scan_id:
        raise SuiteError('automatic scan execution_id is missing')
    return facts, scan_id



def validate_raster_artifact(artifact, expectation):
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

def strict_sizes(artifact, physical, item, target_name=None):
    sizes = tuple(facts.get('size_bytes') for facts in (artifact, physical, item))
    if any(type(value) is not int or value <= 0 for value in sizes) or len(set(sizes)) != 1:
        fields = ('artifact', 'object', 'DataItem')
        detail = ', '.join(f'{field}={type(value).__name__}:'+repr(value)
                          if type(value) in (int, float, bool, type(None)) else f'{field}={type(value).__name__}'
                          for field, value in zip(fields, sizes))
        label = f' [{target_name}]' if target_name is not None else ''
        raise SuiteError('raster artifact/object/DataItem sizes must be equal positive integers'+label+' ('+detail+')')
    return dict(zip(('artifact_size_bytes', 'object_size_bytes', 'data_item_size_bytes'), sizes))


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
                    and f'[DevExecutor] 工作流引擎调用失败: execution_id={execution_id} err=' in message
                    and message.endswith('任务 save 执行失败: target object already exists: raster-target/result.cog.tif')):
                    return {'execution_id': execution_id, 'cause': 'target_already_exists',
                            'source': 'develop_runtime_log'}
        time.sleep(1)
    raise SuiteError('duplicate create is missing its exact Owner target-conflict diagnostic')



def tiling_workflow(source_locator, target_engine, write_mode='create', compression='DEFLATE', target_name='tiles-basic'):
    return {'tasks': [
        {'id': 'load', 'operator': 'raster_load', 'depends_on': [], 'params': {'locator': source_locator}},
        {'id': 'tile', 'operator': 'raster_tile', 'depends_on': ['load'], 'params': {
            'input_raster': {'$ref': 'load', 'port': 'default'}, 'tile_size': [160, 160]}},
        {'id': 'save_tiles', 'operator': 'raster_save_tiles', 'depends_on': ['tile'], 'params': {
            'input_tiles': {'$ref': 'tile', 'port': 'default'},
            'target_parent_locator': f'addp://engine/{target_engine}/path/raster-target?type=bucket',
            'target_name': target_name, 'write_mode': write_mode, 'profile': 'cog', 'blocksize': 128,
            'compression': compression}},
    ]}


def validate_tiles_publication(execution, source_locator, target_engine, write_mode='create', failure_prefix=None):
    failed = failure_prefix is not None
    prefix = failure_prefix or 'tiles-basic'
    count = fixture.TILE_CLEANUP_CASES[prefix][1] if prefix in fixture.TILE_CLEANUP_CASES else 1 if failed else 4
    keys = set(list(fixture.TILE_WINDOWS)[:count])
    if execution.get('status') != ('failed' if failed else 'success'):
        raise SuiteError('tiling execution status differs from publication scenario')
    metadata = obj(execution.get('metadata'), 'tiling metadata')
    result = obj(metadata.get('result'), 'tiling result')
    manifest = obj(result.get('final_result'), 'tile manifest')
    expected = {'artifact_type': 'raster_tiles', 'source_width': 256, 'source_height': 256,
        'tile_size': [160,160], 'columns': 2, 'rows': 2, 'total_tiles': 4, 'completed_tiles': count}
    if failed and count < 4:
        expected.update(failed_tile=list(fixture.TILE_WINDOWS)[count],
                        publication='not_attempted' if prefix in fixture.TILE_CLEANUP_CASES else 'unknown')
    if any(manifest.get(key) != value for key, value in expected.items()) or set(manifest) != set(expected) | {'tiles'}:
        raise SuiteError('tiling manifest grid/count/completion differs')
    tiles = obj(manifest.get('tiles'), 'tiles')
    outputs = obj(execution.get('outputs'), 'tiling outputs')
    resources = obj(obj(outputs.get('save_tiles'), 'save tiles output').get('tiles'), 'tile resources')
    if set(outputs) != {'save_tiles'} or set(tiles) != keys or set(resources) != set(tiles):
        raise SuiteError('tiling outputs must identify every individual tile and no directory resource')
    locators = {key: f'addp://engine/{target_engine}/path/raster-target/{prefix}/{key}.tif?type=object' for key in tiles}
    for key, fact in tiles.items():
        if fact.get('name') != key+'.tif' or fact.get('window') != list(fixture.TILE_WINDOWS[key]) or fact.get('uploaded_files') != 1:
            raise SuiteError('tiling names/windows/publication facts differ')
        if type(fact.get('size_bytes')) is not int or fact['size_bytes'] <= 0:
            raise SuiteError('tile artifact size must be a positive integer')
        validate_raster_artifact(fact, fixture.tile_expectation(key))
        if resources[key] != {'resource': {'locator': locators[key], 'type': 'object', 'write_mode': write_mode}}:
            raise SuiteError('tile resource locator/type/write mode differs')
    if failed:
        if metadata.get('lineage_facts'):
            raise SuiteError('failed tiling must not claim successful lineage facts')
    else:
        facts = obj(metadata.get('lineage_facts'), 'tiling lineage facts')
        if (facts.get('schema_version') != 'addp.lineage-facts/v1'
            or [item.get('locator') for item in array(facts.get('inputs'), 'tile inputs')] != [source_locator]):
            raise SuiteError('tiling lineage source differs')
        lineage_outputs = array(facts.get('outputs'), 'tile lineage outputs')
        if (len(lineage_outputs) != 4 or {item.get('locator') for item in lineage_outputs} != set(locators.values())
            or any(item.get('write_mode') != write_mode for item in lineage_outputs)):
            raise SuiteError('tiling lineage does not identify the four actual writes')
    targets = array(result.get('produced_targets'), 'tiling produced targets')
    if (len(targets) != len(keys) or {item.get('locator') for item in targets} != set(locators.values())
        or any(item.get('task_id') != 'save_tiles' or item.get('tile_key') not in tiles or item.get('type') != 'object' for item in targets)):
        raise SuiteError('tiling produced targets differ from actual individual resources')
    scans = array(result.get('meta_scan_runs'), 'tile scans')
    if (len(scans) != len(keys) or {item.get('target_locator') for item in scans} != set(locators.values())
        or any(item.get('status') != 'submitted' or not item.get('execution_id') for item in scans)
        or len({item['execution_id'] for item in scans}) != len(keys)):
        raise SuiteError('tiling did not automatically submit one distinct Meta scan per confirmed tile')
    return manifest, locators, scans

def physical(repository, env, action):
    result = subprocess.run([sys.executable, 'business/scripts/online-raster-minio-fixture.py', action],
                            cwd=repository, env=env, capture_output=True, text=True, timeout=300)
    if result.returncode:
        # Forward only named fixture diagnostics; never SDK stderr or private paths.
        reason = next((code for code in ('cleanup_event_permission_denied', 'cleanup_event_missing', 'physical_worker_failed')
            if f'Raster physical fixture failed: {code}' in result.stderr.splitlines()), None)
        raise SuiteError(f'physical raster {action} failed ({result.returncode})' + (f': {reason}' if reason else ''))
    payload = obj(json.loads(result.stdout), 'physical raster evidence')
    if action in ('verify-tiles', 'prepare-tiles-replace', 'verify-tiles-replace', 'verify-tiles-failure') + fixture.CLEANUP_ACTIONS:
        prefix = action.removeprefix('verify-')
        count = fixture.TILE_CLEANUP_CASES[prefix][1] if prefix in fixture.TILE_CLEANUP_CASES else 1 if action == 'verify-tiles-failure' else 4
        if payload.get('source_unchanged') is not True or set(obj(payload.get('tiles'), 'physical tiles')) != set(list(fixture.TILE_WINDOWS)[:count]):
            raise SuiteError('physical tiling evidence is incomplete')
        return payload
    case_name = action.removeprefix('verify-')
    if action == 'prepare-terrain' or case_name in fixture.TERRAIN_CASES + ('dem-contour',):
        count = (0 if action == 'prepare-terrain' else len(fixture.TERRAIN_CASES)
                 if case_name == 'dem-contour' else fixture.TERRAIN_CASES.index(case_name) + 1)
        terrain = obj(payload.get('terrain'), 'physical DEM artifacts')
        preserved = obj(payload.get('preserved_sha256'), 'physical DEM baseline')
        if (payload.get('source_unchanged') is not True
            or set(preserved) != fixture.preterrain_target_names()
            or any(not isinstance(digest, str) or not digest for digest in preserved.values())
            or set(terrain) != set(fixture.TERRAIN_CASES[:count])):
            raise SuiteError('physical DEM manifest is incomplete')
        for name, evidence in terrain.items():
            evidence = obj(evidence, 'physical DEM artifact')
            if (evidence.get('case_name') != name or evidence.get('cog_valid') is not True
                or evidence.get('source_unchanged') is not True
                or type(evidence.get('valid_pixels')) is not int
                or evidence['valid_pixels'] != fixture.terrain_expectation()['valid_pixels']):
                raise SuiteError('physical DEM artifact verification is incomplete')
        return payload
    expectation = fixture.utility_expectation() if case_name in fixture.UTILITY_CASES or action == 'verify-utility-queries' else fixture.computed_expectation(case_name) if case_name in fixture.GRID_CASES + fixture.MULTIBAND_CASES + fixture.FOUNDATION_CASES + fixture.RECLASS_CASES + fixture.AGGREGATE_CASES + fixture.OUTSIDE_CASES + fixture.UPDATE_CASES + fixture.MOSAIC_CASES else fixture.artifact_expectation(action.startswith('verify-mosaic-'))
    if payload.get('cog_valid') is not True or payload.get('source_unchanged') is not True or payload.get('valid_pixels') != expectation['valid_pixels']:
        raise SuiteError('physical raster verification is incomplete')
    return payload


def raster_browser_report(repository, env, evidence, kind, test_name):
    report_path = Path(env['ADDP_ONLINE_ARTIFACT_DIR']) / f'raster-workflow-{evidence["case_name"]}-{kind}.json'
    report_path.unlink(missing_ok=True)
    browser_env = dict(env, ADDP_ONLINE_RASTER_EVIDENCE=json.dumps(evidence))
    result = subprocess.run(['npm', 'exec', '--', 'playwright', 'test', 'e2e/online/raster-workflow.spec.js',
                             '--config=playwright.online.config.js', '--grep', test_name], cwd=repository / 'console/frontend',
                            env=browser_env, stdout=sys.stderr, stderr=sys.stderr, timeout=240)
    if result.returncode or not report_path.is_file():
        raise SuiteError('raster Console acceptance failed or report is missing')
    return obj(json.loads(report_path.read_text()), 'browser report')


def browser(repository, env, evidence):
    report = raster_browser_report(repository, env, evidence, 'browser', '栅格 Console')
    expected = {**evidence, 'schema_version': 'addp.raster-workflow-browser/v1', 'result': 'passed'}
    if report != expected:
        raise SuiteError('raster Console report does not bind the exact run/User/execution/items')
    return report


def browser_editor(repository, env, evidence):
    report = raster_browser_report(repository, env, evidence, 'editor', '栅格页面多选')
    validate_editor_report(report, evidence)
    return report


def validate_editor_report(report, evidence):
    if evidence.get('case_name') not in fixture.MOSAIC_EDITOR_CASES:
        raise SuiteError('unknown raster editor scene')
    source_ids = evidence.get('source_engine_ids')
    source_names = evidence.get('source_engine_names')
    distinct = 3 if evidence['case_name'] == fixture.MOSAIC_EDITOR_CASES[1] else 1
    if (not isinstance(source_ids, list) or len(source_ids) != 3
        or any(type(value) is not int or value <= 0 for value in source_ids)
        or len(set(source_ids)) != distinct or evidence['target_engine_id'] in source_ids
        or not isinstance(source_names, list) or len(source_names) != 3
        or any(not isinstance(value, str) or not value for value in source_names)):
        raise SuiteError('editor source Engines do not match the scene or separate target')
    extra = {'schema_version', 'result', 'task_id', 'execution_id', 'workflow_definition',
             'save_node_id', 'source_task_id', 'reloaded', 'deleted', 'page_requests'}
    if (set(report) != set(evidence) | extra or any(report.get(key) != value for key, value in evidence.items())
        or report.get('schema_version') != 'addp.raster-workflow-editor/v2' or report.get('result') != 'passed'
        or type(report.get('task_id')) is not int or report['task_id'] <= 0
        or not isinstance(report.get('execution_id'), str) or not report['execution_id']
        or report.get('source_task_id') != str(report['task_id'])
        or report.get('reloaded') is not True or report.get('deleted') is not True
        or report.get('page_requests') != {'create': 1, 'update': 0, 'execute': 1}
        or any(type(value) is not int for value in report['page_requests'].values())):
        raise SuiteError('raster editor report does not bind the exact UI/save/execution/cleanup')
    definition = obj(report['workflow_definition'], 'saved editor workflow')
    tasks = array(definition.get('tasks'), 'editor tasks')
    ids = [task.get('id') for task in tasks]
    if (set(definition) != {'tasks'} or len(tasks) != 5
        or any(set(task) != {'id', 'operator', 'params', 'depends_on'} for task in tasks)
        or any(not isinstance(value, str) or not value for value in ids) or len(set(ids)) != 5):
        raise SuiteError('raster editor did not save exactly five independent nodes')
    sources = {task['id']: task for task in tasks if task.get('operator') == 'raster_load'}
    mosaics = [task for task in tasks if task.get('operator') == 'raster_mosaic']
    saves = [task for task in tasks if task.get('operator') == 'raster_save']
    if len(sources) != 3 or len(mosaics) != 1 or len(saves) != 1:
        raise SuiteError('raster editor saved the wrong operators')
    if (any(task.get('depends_on') != [] or set(task.get('params', {})) != {'locator', 'source_crs'}
            or task['params']['source_crs'] != '' or not isinstance(task['params']['locator'], str) for task in sources.values())
        or sorted(task['params']['locator'] for task in sources.values()) != sorted(evidence['source_locators'])):
        raise SuiteError('raster editor did not save all three exact public sources')

    def reference(value):
        value = obj(value, 'editor raster reference')
        if set(value) - {'$ref', 'port'} or value.get('port', 'default') != 'default':
            raise SuiteError('raster editor saved a non-default raster reference')
        return value.get('$ref')

    mosaic, save = mosaics[0], saves[0]
    params = obj(mosaic.get('params'), 'editor mosaic parameters')
    refs = [reference(value) for value in array(params.get('input_rasters'), 'editor ordered rasters')]
    if (len(refs) != 3 or set(refs) != set(sources)
        or [sources[value]['params']['locator'] for value in refs] != evidence['source_locators']
        or len(mosaic.get('depends_on', [])) != 3 or set(mosaic['depends_on']) != set(sources)
        or {key: value for key, value in params.items() if key != 'input_rasters'} != {
            'target_crs': 'EPSG:3857', 'resolution': [1, 1], 'overlap': 'first', 'resampling': 'nearest'}):
        raise SuiteError('raster editor changed the ordered mosaic or grid parameters')
    params = obj(save.get('params'), 'editor save parameters')
    parent = urllib.parse.urlsplit(params.get('target_parent_locator', ''))
    query = urllib.parse.parse_qs(parent.query)
    if (parent.scheme != 'addp' or parent.netloc != 'engine'
        or parent.path != f'/{evidence["target_engine_id"]}/path/raster-target' or parent.fragment
        or query.get('type') != ['bucket'] or set(query) - {'type', 'node_id'}
        or ('node_id' in query and (len(query['node_id']) != 1 or not query['node_id'][0].isdigit() or int(query['node_id'][0]) <= 0))
        or reference(params.get('input_raster')) != mosaic['id'] or save.get('depends_on') != [mosaic['id']]
        or save['id'] != report['save_node_id']
        or type(params.get('jpeg_quality')) is not int
        or {key: value for key, value in params.items() if key not in ('input_raster', 'target_parent_locator')} != {
            'target_name': evidence['target_name'], 'write_mode': 'create', 'profile': 'cog',
            'compression': 'DEFLATE', 'blocksize': 128, 'overview_resampling': 'nearest', 'jpeg_quality':90}):
        raise SuiteError('raster editor changed the exact public output target/profile')


def queue_output_inspection(pending, client, run_id, source, source_locator, target_engine, target_name,
                            execution_id, case_name, identity, browser_runner, expectation, physical_evidence, source_locators, artifact, size_evidence):
    target = support.find_fixture_item(client, target_engine, 'raster-target/' + target_name, 'COG target')
    # No manual target scan or collect: both facts must converge automatically.
    target = obj(client.request('GET', f'/api/v1/meta/items/{target["id"]}', (200,)).payload, 'target DataItem')
    validate_target_item(target, expectation, physical_evidence)
    size_evidence.setdefault(execution_id, {})[target_name] = strict_sizes(artifact, physical_evidence, target, target_name)
    target_locator = f'addp://engine/{target_engine}/path/raster-target/{target_name}?type=object'
    evidence = {
        'run_id': run_id, 'principal_id': identity['principal_id'],
        'tenant_id': identity['tenant_id'], 'execution_id': execution_id,
        'source_item_id': source['id'], 'target_item_id': target['id'],
        'source_locator': source_locator, 'source_locators': source_locators, 'target_locator': target_locator,
        'case_name': case_name, 'source_name': source['full_name'].rsplit('/', 1)[-1], 'target_name': target_name.rsplit('/', 1)[-1],
    }
    lineage, report = {}, {}
    pending.append((source['id'], target['id'], execution_id, evidence, browser_runner, lineage, report))
    return lineage, report


def finish_output_inspections(pending, repository, env, client, timeout):
    # Check every exact source/target pair after all writes, allowing the ordinary
    # collector to converge without serializing a one-minute tick per artifact.
    # The result dictionaries are filled only with actual checks, before returning
    # the suite report. Any missing relation or browser failure still fails it.
    for source_id, target_id, execution_id, evidence, browser_runner, lineage, report in pending:
        lineage.update(wait_lineage(client, source_id, target_id, execution_id, timeout))
        report.update(browser_runner(repository, env, evidence))


def run_scenario(repository, env, client, physical_runner=physical, browser_runner=browser, diagnostic_runner=duplicate_create_diagnostic, editor_runner=browser_editor):
    tenant = positive(env['ADDP_ONLINE_TEST_TENANT_ID'], 'tenant')
    if tenant <= 1:
        raise SuiteError('raster acceptance forbids the default Tenant')
    source_engine = positive(env['ADDP_ONLINE_RASTER_SOURCE_ENGINE_ID'], 'source engine')
    source_engines = {'source': source_engine,
                      'source-b': positive(env['ADDP_ONLINE_RASTER_SOURCE_B_ENGINE_ID'], 'source B engine'),
                      'source-c': positive(env['ADDP_ONLINE_RASTER_SOURCE_C_ENGINE_ID'], 'source C engine')}
    target_engine = positive(env['ADDP_ONLINE_RASTER_TARGET_ENGINE_ID'], 'target engine')
    if len(set(source_engines.values()) | {target_engine}) != 4:
        raise SuiteError('raster fixture requires three distinct source Engines and a separate target')
    timeout = float(env.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS', '180'))
    identity = validate_identity(client, tenant, required_permissions=EDITOR_PERMISSIONS)
    pending_outputs = []
    size_evidence = {}
    engines = array(client.request('GET', '/api/v1/develop/workflow-engines', (200,)).payload, 'workflow engines')
    runtime = [item for item in engines if item.get('engine_type') == 'geopython_workflow' and item.get('connection_status') == 'online']
    if len(runtime) != 1:
        raise SuiteError('exactly one online GeoPython Workflow Runtime must be registered')
    engine_id = positive(runtime[0].get('id'), 'runtime engine')
    for source_id in source_engines.values():
        support.wait_for_meta_scan(client, source_id, time.monotonic() + timeout)
    catalog_engines = array(client.request('GET', '/api/v1/meta/engines', (200,)).payload, 'source catalog engines')
    engine_names = {positive(item.get('id'), 'catalog engine'): item.get('name') for item in catalog_engines}
    if any(not isinstance(engine_names.get(identifier), str) or not engine_names[identifier]
           for identifier in source_engines.values()):
        raise SuiteError('all three registered source Engines must be visible to the same User')
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
        if status == 'success':
            target = support.find_fixture_item(client, target_engine, 'raster-target/result.cog.tif', 'band-math target')
            size_evidence[identifier] = {'result.cog.tif': strict_sizes(metadata['result']['final_result'], native, target, 'result.cog.tif')}
        physical_evidence.append(native)
        executions.append({'execution_id': identifier, 'write_mode': mode, 'status': status})
    lineage, browser_evidence = queue_output_inspection(pending_outputs, client, env['ADDP_ONLINE_TEST_RUN_ID'], source, source_locator,
        target_engine, 'result.cog.tif', executions[-1]['execution_id'], 'band-math', identity,
        browser_runner, fixture.artifact_expectation(), physical_evidence[-1], [source_locator], execution['metadata']['result']['final_result'], size_evidence)
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
        graph, browser_report = queue_output_inspection(pending_outputs, client, env['ADDP_ONLINE_TEST_RUN_ID'], spatial_source, spatial_locator,
            target_engine, name, identifier, 'mosaic-' + overlap, identity,
            browser_runner, fixture.artifact_expectation(True), native, [spatial_locator], execution['metadata']['result']['final_result'], size_evidence)
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
        graph, browser_report = queue_output_inspection(pending_outputs, client, env['ADDP_ONLINE_TEST_RUN_ID'], case_source, case_locator,
            target_engine, name, identifier, case_name, identity, browser_runner, expectation, native, [case_locator], execution['metadata']['result']['final_result'], size_evidence)
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
            or any(band.get('overviews') != expected_levels for band in artifact['bands'])):
            raise SuiteError('utility workflow lost band validity, explicit levels or existing artifacts')
        graph, browser_report = queue_output_inspection(pending_outputs, client, env['ADDP_ONLINE_TEST_RUN_ID'], source, source_locator,
            target_engine, name, identifier, case_name, identity, browser_runner, fixture.utility_expectation(), native, [source_locator], artifact, size_evidence)
        utility_sources[case_name] = support.find_fixture_item(client, target_engine, 'raster-target/' + name, 'utility source')
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
    update_cases = []
    mosaic_source_cases = []
    mosaic_editor_cases = []
    mosaic_sources = {name: support.find_fixture_item(client, source_engine, 'raster-source/' + name, 'independent mosaic source')
                      for name in fixture.MOSAIC_SOURCE_WINDOWS}
    mosaic_locators = {name: support.build_item_locator(source_engine, item) for name, item in mosaic_sources.items()}
    if len({item['id'] for item in mosaic_sources.values()}) != 3 or len(set(mosaic_locators.values())) != 3:
        raise SuiteError('independent mosaic sources do not have three distinct registered identities')
    cross_sources = {name: support.find_fixture_item(client, source_engines[role], 'raster-' + role + '/' + name,
                                                    'cross-engine mosaic source')
                     for name, role in fixture.MOSAIC_SOURCE_ROLES.items()}
    cross_locators = {name: support.build_item_locator(source_engines[fixture.MOSAIC_SOURCE_ROLES[name]], item)
                      for name, item in cross_sources.items()}
    if len({item['id'] for item in cross_sources.values()}) != 3 or len(set(cross_locators.values())) != 3:
        raise SuiteError('cross-engine sources must have three independent registered identities')
    patch_source = support.find_fixture_item(client, source_engine, 'raster-source/multiband.tif', 'update source')
    patch_locator = support.build_item_locator(source_engine, patch_source)
    reference_source = support.find_fixture_item(client, target_engine, 'raster-target/mosaic-first.cog.tif', 'reference raster')
    reference_locator = support.build_item_locator(target_engine, reference_source)
    aggregate_sources = {
        name: support.find_fixture_item(client, source_engine, 'raster-source/'+name, 'aggregation source')
        for name in {fixture.aggregate_config(case)['source_name'] for case in fixture.AGGREGATE_CASES}}
    for case_name in fixture.FOUNDATION_CASES + fixture.RECLASS_CASES + fixture.AGGREGATE_CASES + fixture.OUTSIDE_CASES + fixture.UPDATE_CASES + fixture.MOSAIC_CASES:
        single = case_name in fixture.RECLASS_CASES + fixture.AGGREGATE_CASES + fixture.OUTSIDE_CASES
        case_sources = [(source, source_locator, '')] if single else [
            (spatial_source, spatial_locator, '-spatial'), (reference_source, reference_locator, '-reference')]
        if case_name in fixture.UPDATE_CASES:
            case_sources = [(source,source_locator,'-base'),(patch_source,patch_locator,'-patch')]
        if case_name in fixture.AGGREGATE_CASES:
            case_source = aggregate_sources[fixture.aggregate_config(case_name)['source_name']]
            case_sources = [(case_source, support.build_item_locator(source_engine, case_source), '')]
        if case_name in fixture.MOSAIC_CASES:
            chosen_sources, chosen_locators = ((cross_sources, cross_locators) if case_name == fixture.MOSAIC_EDITOR_CASES[1]
                                               else (mosaic_sources, mosaic_locators))
            case_sources = [(chosen_sources[name], chosen_locators[name], '-' + name.removesuffix('.tif'))
                            for name in fixture.MOSAIC_SOURCE_ORDER]
        source_locators = [case_locator for _, case_locator, _ in case_sources]
        expectation = fixture.computed_expectation(case_name)
        name = case_name + '.cog.tif'
        locator = f'addp://engine/{target_engine}/path/raster-target/{name}?type=object'

        editor_report = None
        output_node = 'save'
        if case_name in fixture.MOSAIC_EDITOR_CASES:
            source_ids = ([source_engines[fixture.MOSAIC_SOURCE_ROLES[name]] for name in fixture.MOSAIC_SOURCE_ORDER]
                          if case_name == fixture.MOSAIC_EDITOR_CASES[1] else [source_engine] * 3)
            editor_evidence = {
                'run_id': env['ADDP_ONLINE_TEST_RUN_ID'], **identity,
                'case_name': case_name, 'workflow_engine_id': engine_id,
                'task_name': env['ADDP_ONLINE_TEST_RUN_ID'] + '-' + case_name,
                'source_locators': source_locators,
                'source_engine_ids': source_ids, 'source_engine_names': [engine_names[value] for value in source_ids],
                'source_names': [item['full_name'].rsplit('/', 1)[-1] for item, _, _ in case_sources],
                'source_item_ids': [item['id'] for item, _, _ in case_sources],
                'target_engine_id': target_engine, 'target_name': name,
            }
            editor_report = editor_runner(repository, env, editor_evidence)
            validate_editor_report(editor_report, editor_evidence)
            client.request('GET', f'/api/v1/develop/task-definitions/{editor_report["task_id"]}', (404,))
            identifier, output_node = editor_report['execution_id'], editor_report['save_node_id']
        else:
            definition = (mosaic_source_workflow(mosaic_locators, target_engine, case_name) if case_name in fixture.MOSAIC_CASES else
                          update_workflow(source_locator, patch_locator, target_engine, case_name) if case_name in fixture.UPDATE_CASES else
                          outside_workflow(source_locator, target_engine, case_name) if case_name in fixture.OUTSIDE_CASES else
                          aggregate_workflow(source_locators[0], target_engine, case_name) if case_name in fixture.AGGREGATE_CASES else
                          reclass_workflow(source_locator, target_engine, case_name) if case_name in fixture.RECLASS_CASES else
                          foundation_workflow(spatial_locator, reference_locator, target_engine, case_name))
            identifier = submit(client, engine_id, definition)
        execution = wait_execution(client, 'develop', identifier, timeout)
        if editor_report and execution.get('source_task_id') != str(editor_report['task_id']):
            raise SuiteError('raster editor execution belongs to another saved task')
        facts, scan_id = validate_success(execution, source_locators, locator, 'create', expectation, output_node=output_node)
        wait_execution(client, 'meta', scan_id, timeout)
        monitor = obj(client.request('GET', f'/api/v1/monitor/executions/by-execution-id/{identifier}', (200,)).payload, 'Monitor multi-raster execution')
        if (monitor.get('status') != 'success' or monitor.get('module') != 'develop'
            or obj(monitor.get('metadata'), 'Monitor metadata').get('lineage_facts') != facts):
            raise SuiteError('Monitor multi-raster execution differs from Develop status/lineage')
        native = physical_runner(repository, env, 'verify-' + case_name)
        artifact = execution['metadata']['result']['final_result']
        colors = ['Gray', 'Undefined'] if case_name in fixture.UPDATE_CASES + fixture.AGGREGATE_MULTIBAND_CASES + fixture.MOSAIC_CASES else ['Gray', 'Undefined', 'Alpha'] if case_name in fixture.OUTSIDE_CASES else ['Red', 'Green', 'Blue'] if case_name == 'multiraster-rgb' else ['Gray']
        levels = [[expectation['width']//2, expectation['height']//2]]
        if (native.get('case_name') != case_name or native.get('preserved_sha256') != preserved
            or native.get('cog_valid') is not True or native.get('source_unchanged') is not True
            or native.get('has_overviews') is not True or native.get('overview_sizes') != levels
            or native.get('color_interpretations') != colors
            or [band.get('color_interpretation') for band in artifact['bands']] != colors
            or any(band.get('overviews') != levels for band in artifact['bands'])
            or native.get('valid_pixels') != expectation['valid_pixels']
            or native.get('invalid_pixels') != expectation['width'] * expectation['height'] - expectation['valid_pixels']
            or native.get('band_valid_pixels') != expectation.get('band_valid_pixels', [expectation['valid_pixels']] * (expectation['band_count'] - (case_name in fixture.OUTSIDE_CASES)))):
            raise SuiteError('multi-raster bands, colour, validity or preservation evidence differs')
        if case_name == fixture.MOSAIC_SOURCE_CASES[-1] and native.get('sha256') == mosaic_source_cases[0]['physical']['sha256']:
            raise SuiteError('independent-source first/last outputs have identical bytes')
        if (case_name == fixture.MOSAIC_EDITOR_CASES[1]
            and native.get('sha256') == preserved[fixture.MOSAIC_EDITOR_CASES[0] + '.cog.tif']):
            raise SuiteError('cross-engine output reused the same-named original source pixels')
        target = support.find_fixture_item(client, target_engine, 'raster-target/' + name, 'multi-raster target')
        graphs, browsers = [], []
        for case_source, case_locator, suffix in case_sources:
            graph, report = queue_output_inspection(pending_outputs, client, env['ADDP_ONLINE_TEST_RUN_ID'], case_source, case_locator, target_engine,
                name, identifier, case_name + suffix, identity, browser_runner, expectation, native, source_locators, artifact, size_evidence)
            graphs.append(graph)
            browsers.append(report)
        cases = mosaic_source_cases if case_name in fixture.MOSAIC_SOURCE_CASES else update_cases if case_name in fixture.UPDATE_CASES else outside_cases if case_name in fixture.OUTSIDE_CASES else aggregate_cases if case_name in fixture.AGGREGATE_CASES else reclass_cases if case_name in fixture.RECLASS_CASES else foundation_cases
        case = {'case_name': case_name, 'execution_id': identifier,
            'artifact': artifact, 'data_item_size_bytes': target['size_bytes'],
            'automatic_target_scan_execution_id': scan_id, 'lineage': graphs[0] if single else graphs,
            'physical': native, 'browser': browsers[0] if single else browsers}
        if editor_report:
            if any(item['editor']['task_id'] == editor_report['task_id'] or item['execution_id'] == identifier
                   for item in mosaic_editor_cases):
                raise SuiteError('editor scenes reused a saved task or execution')
            mosaic_editor_cases.append({**case, 'editor': editor_report})
        else:
            cases.append(case)
        preserved[name] = native['sha256']
    footprint_cases = []
    footprint_sources = {
        fixture.TOPOLOGY_SOURCE: (source_engine, support.find_fixture_item(client, source_engine, 'raster-source/' + fixture.TOPOLOGY_SOURCE, 'topology footprint source')),
        fixture.ROTATED_SOURCE: (source_engine, support.find_fixture_item(client, source_engine, 'raster-source/' + fixture.ROTATED_SOURCE, 'rotated footprint source')),
        'multiband.tif': (source_engine, support.find_fixture_item(client, source_engine, 'raster-source/multiband.tif', 'footprint source')),
        'clip-outside.cog.tif': (target_engine, support.find_fixture_item(client, target_engine, 'raster-target/clip-outside.cog.tif', 'footprint holes source')),
    }
    for case_name in fixture.FOOTPRINT_CASES:
        source_name = fixture.footprint_source_name(case_name)
        footprint_engine, footprint_source = footprint_sources[source_name]
        footprint_locator = support.build_item_locator(footprint_engine, footprint_source)
        identifier = submit(client, engine_id, footprint_workflow(footprint_locator, case_name))
        execution = wait_execution(client, 'develop', identifier, timeout)
        result = validate_footprint(execution, case_name)
        monitor = obj(client.request('GET', f'/api/v1/monitor/executions/by-execution-id/{identifier}', (200,)).payload, 'Monitor footprint execution')
        if (monitor.get('status') != 'success' or monitor.get('module') != 'develop'
            or obj(monitor.get('metadata', {}), 'Monitor footprint metadata').get('lineage_facts')):
            raise SuiteError('Monitor footprint has invalid status/owner or persistent lineage')
        native = physical_runner(repository, env, 'verify-' + fixture.MOSAIC_CASES[-1])
        final_name = fixture.MOSAIC_CASES[-1] + '.cog.tif'
        if (native.get('sha256') != preserved[final_name] or native.get('preserved_sha256') !=
            {name: digest for name, digest in preserved.items() if name != final_name}):
            raise SuiteError('footprint execution changed an accepted artifact')
        report = browser_runner(repository, env, {
            'run_id': env['ADDP_ONLINE_TEST_RUN_ID'], 'principal_id': identity['principal_id'],
            'tenant_id': identity['tenant_id'], 'execution_id': identifier,
            'source_item_id': footprint_source['id'], 'source_locator': footprint_locator,
            'source_name': source_name, 'case_name': case_name, 'result_kind': 'json', 'expected_result': result,
        })
        footprint_cases.append({'case_name': case_name, 'execution_id': identifier,
            'json_result': result, 'physical': native, 'browser': report})
    tiling_results = {}
    for mode, compression in (('create', 'DEFLATE'), ('replace', 'NONE')):
        if mode == 'replace':
            # Current derive relations change on replace. Prove the first
            # execution before writing the same resource identities again.
            finish_output_inspections(pending_outputs, repository, env, client, timeout)
            pending_outputs.clear()
            prepared = physical_runner(repository, env, 'prepare-tiles-replace')
            first = tiling_results['create']['physical']
            if (prepared.get('tiles') != first['tiles'] or prepared.get('preserved_sha256') != preserved
                or prepared.get('source_unchanged') is not True
                or prepared.get('unrelated_sha256') != hashlib.sha256(fixture.TILE_KEEP_BYTES).hexdigest()):
                raise SuiteError('batch replace preparation changed accepted tiles or omitted the unrelated file')
        identifier = submit(client, engine_id, tiling_workflow(source_locator, target_engine, mode, compression))
        execution = wait_execution(client, 'develop', identifier, timeout)
        manifest, locators, scans = validate_tiles_publication(execution, source_locator, target_engine, mode)
        monitor = obj(client.request('GET', f'/api/v1/monitor/executions/by-execution-id/{identifier}', (200,)).payload, 'tiling Monitor')
        if (monitor.get('status') != 'success' or monitor.get('module') != 'develop'
            or monitor.get('metadata', {}).get('lineage_facts') != execution['metadata']['lineage_facts']):
            raise SuiteError('Monitor tiling status or lineage differs')
        for scan in scans:
            wait_execution(client, 'meta', scan['execution_id'], timeout)
        native = physical_runner(repository, env, 'verify-tiles' if mode == 'create' else 'verify-tiles-replace')
        if native.get('preserved_sha256') != preserved or native.get('source_unchanged') is not True or set(native.get('tiles', {})) != set(locators):
            raise SuiteError('tiling changed an accepted artifact/source or omitted physical tiles')
        tiling_cases = []
        for key in fixture.TILE_WINDOWS:
            physical_tile = obj(native['tiles'][key], 'physical tile')
            fact = manifest['tiles'][key]
            if (physical_tile.get('compression') != compression
                or physical_tile.get('valid_pixels') != fixture.tile_expectation(key)['valid_pixels']
                or [band.get('overviews') for band in fact['bands']] != [physical_tile.get('overview_sizes')]*2):
                raise SuiteError('tile physical validity or overview facts differ')
            target = support.find_fixture_item(client, target_engine, f'raster-target/tiles-basic/{key}.tif', 'tile target')
            if mode == 'replace':
                original = tiling_results['create']['cases'][len(tiling_cases)]
                if (target['id'] != original['browser']['target_item_id']
                    or physical_tile.get('sha256') == original['physical']['sha256']
                    or fact['size_bytes'] == original['artifact']['size_bytes']):
                    raise SuiteError('batch replace changed tile identity or did not overwrite the actual bytes/size')
            def tile_browser(repo, browser_env, evidence):
                return browser_runner(repo, browser_env, {**evidence, 'target_locators': list(locators.values())})
            graph, report = queue_output_inspection(pending_outputs, client, env['ADDP_ONLINE_TEST_RUN_ID'], source, source_locator, target_engine,
                'tiles-basic/'+key+'.tif', identifier, ('tiles-' if mode == 'create' else 'tiles-replace-')+key, identity, tile_browser,
                fixture.tile_expectation(key), physical_tile, [source_locator], fact, size_evidence)
            tiling_cases.append({'tile_key': key, 'execution_id': identifier, 'artifact': fact,
                'physical': physical_tile, 'data_item_size_bytes': target['size_bytes'],
                'lineage': graph, 'browser': report})
        tiling_results[mode] = {'cases': tiling_cases, 'scans': scans, 'physical': native}
        if mode == 'replace':
            if (native.get('unrelated_sha256') != prepared['unrelated_sha256']
                or {scan['execution_id'] for scan in scans} & {scan['execution_id'] for scan in tiling_results['create']['scans']}):
                raise SuiteError('batch replace lost the unrelated file or reused an earlier automatic scan')
    finish_output_inspections(pending_outputs, repository, env, client, timeout)
    failures = {}
    previous_scans = {scan['execution_id'] for mode in tiling_results.values() for scan in mode['scans']}
    prior_failure_hashes = {}
    for prefix, action in [(fixture.TILE_FAILURE_PREFIX, 'verify-tiles-failure')] + [(prefix, 'verify-' + prefix) for prefix in fixture.TILE_CLEANUP_CASES]:
        identifier = submit(client, engine_id, tiling_workflow(source_locator, target_engine, target_name=prefix))
        execution = wait_execution(client, 'develop', identifier, timeout, expected='failed')
        manifest, locators, scans = validate_tiles_publication(execution, source_locator, target_engine, failure_prefix=prefix)
        monitor = obj(client.request('GET', f'/api/v1/monitor/executions/by-execution-id/{identifier}', (200,)).payload, 'failed tiling Monitor')
        if (monitor.get('status') != 'failed' or monitor.get('module') != 'develop'
            or monitor.get('metadata', {}).get('lineage_facts')):
            raise SuiteError('Monitor failed tiling status/owner or lineage differs')
        if {scan['execution_id'] for scan in scans} & previous_scans:
            raise SuiteError('failed tiling reused an earlier automatic scan')
        previous_scans.update(scan['execution_id'] for scan in scans)
        for scan in scans: wait_execution(client, 'meta', scan['execution_id'], timeout)
        native = physical_runner(repository, env, action)
        if (native.get('source_unchanged') is not True or native.get('preserved_sha256') != preserved
            or native.get('preserved_tiles_sha256') != {key: fact['sha256'] for key, fact in tiling_results['replace']['physical']['tiles'].items()}
            or native.get('unrelated_sha256') != tiling_results['replace']['physical']['unrelated_sha256']
            or set(native.get('tiles', {})) != set(manifest['tiles'])):
            raise SuiteError('failed tiling lost its confirmed prefix or changed accepted bytes')
        if prefix in fixture.TILE_CLEANUP_CASES:
            trigger_key = fixture.TILE_CLEANUP_CASES[prefix][0]
            trigger = native['tiles'][trigger_key]
            if (native.get('cleanup_fault', {}).get('obstacle_created') is not True
                or native.get('cleanup_fault', {}).get('workspace_removed') is not True
                or type(native.get('cleanup_fault', {}).get('size_bytes')) is not int
                or type(native.get('cleanup_fault', {}).get('backend_status')) is not int
                or native.get('preserved_failure_sha256') != prior_failure_hashes
                or native.get('cleanup_fault') != {'prefix': prefix, 'tile_key': trigger_key,
                    'backend_status': 200, 'obstacle_created': True, 'workspace_removed': True,
                    'sha256': trigger['sha256'], 'size_bytes': trigger['size_bytes']}):
                raise SuiteError('cleanup failure lacks confirmed storage/fault/workspace evidence or changed earlier failures')
        confirmed_items = {}
        for key, fact in manifest['tiles'].items():
            tile = obj(native['tiles'][key], 'confirmed physical tile')
            if (tile.get('cog_valid') is not True or tile.get('compression') != 'DEFLATE'
                or tile.get('valid_pixels') != fixture.tile_expectation(key)['valid_pixels']
                or [band.get('overviews') for band in fact['bands']] != [tile.get('overview_sizes')]*2):
                raise SuiteError('confirmed partial tile physical facts differ')
            target = support.find_fixture_item(client, target_engine, 'raster-target/'+prefix+'/'+key+'.tif', 'confirmed partial tile')
            size_evidence.setdefault(identifier, {})[prefix+'/'+key+'.tif'] = strict_sizes(fact, tile, target, prefix+'/'+key+'.tif')
            confirmed_items[key] = {'target_item_id': target['id'], 'data_item_size_bytes': target['size_bytes']}
        items = array(client.request('GET', f'/api/v1/meta/engines/{target_engine}/items', (200,)).payload, 'target items')
        partial_names = [item['full_name'] for item in items if item.get('full_name', '').startswith('raster-target/'+prefix+'/') and item.get('item_type') == 'object']
        if set(partial_names) != {'raster-target/'+prefix+'/'+key+'.tif' for key in manifest['tiles']} or len(partial_names) != len(manifest['tiles']):
            raise SuiteError('unconfirmed failed tiles entered the Meta catalog')
        failures[prefix] = {'execution_id': identifier, 'status': 'failed', 'manifest': manifest,
            'outputs': execution['outputs'], 'automatic_scan_runs': scans, 'confirmed_items': confirmed_items, 'physical': native}
        prior_failure_hashes[prefix] = {key: fact['sha256'] for key, fact in native['tiles'].items()}
    # Append terrain after every original success/failure scene. Its baseline
    # binds the complete prior object set, including confirmed failed writes.
    terrain_source=mosaic_sources['mosaic-a.tif'];terrain_locator=mosaic_locators['mosaic-a.tif']
    baseline=physical_runner(repository,env,'prepare-terrain')
    if (baseline.get('source_unchanged') is not True or baseline.get('terrain')!={}
        or set(baseline.get('preserved_sha256',{}))!=fixture.preterrain_target_names()
        or any(not isinstance(v,str) or not v for v in baseline['preserved_sha256'].values())):
        raise SuiteError('DEM baseline does not preserve every earlier target')
    terrain_cases=[];terrain_hashes={}
    for case_name in fixture.TERRAIN_CASES+('dem-contour',):
        identifier=submit(client,engine_id,terrain_workflow(terrain_locator,target_engine,case_name))
        execution=wait_execution(client,'develop',identifier,timeout)
        monitor=obj(client.request('GET',f'/api/v1/monitor/executions/by-execution-id/{identifier}',(200,)).payload,'DEM Monitor execution')
        if monitor.get('status')!='success' or monitor.get('module')!='develop':raise SuiteError('DEM Monitor status/owner differs')
        native=physical_runner(repository,env,'verify-'+case_name)
        accepted=fixture.TERRAIN_CASES if case_name=='dem-contour' else fixture.TERRAIN_CASES[:len(terrain_cases)+1]
        if (native.get('source_unchanged') is not True or native.get('preserved_sha256')!=baseline['preserved_sha256']
            or set(native.get('terrain',{}))!=set(accepted)
            or any(native['terrain'][name].get('sha256')!=digest for name,digest in terrain_hashes.items())):
            raise SuiteError('DEM computation changed sources or an accepted output')
        if case_name=='dem-contour':
            result=validate_terrain_contour(execution)
            if monitor.get('metadata',{}).get('lineage_facts'):raise SuiteError('transient DEM contour has persistent lineage')
            browser=browser_runner(repository,env,{'run_id':env['ADDP_ONLINE_TEST_RUN_ID'],**identity,
                'execution_id':identifier,'source_item_id':terrain_source['id'],'source_locator':terrain_locator,
                'source_name':'mosaic-a.tif','case_name':case_name,'result_kind':'json','expected_result':result})
            terrain_cases.append({'case_name':case_name,'execution_id':identifier,'json_result':result,'physical':native,'browser':browser})
            continue
        name=case_name+'.cog.tif';locator=f'addp://engine/{target_engine}/path/raster-target/{name}?type=object'
        expectation=fixture.terrain_expectation()
        facts,scan=validate_success(execution,[terrain_locator],locator,'create',expectation)
        wait_execution(client,'meta',scan,timeout)
        if monitor.get('metadata',{}).get('lineage_facts')!=facts:raise SuiteError('DEM Monitor lineage differs')
        terrain_physical=native['terrain'][case_name];artifact=execution['metadata']['result']['final_result']
        if (terrain_physical.get('cog_valid') is not True or terrain_physical.get('has_overviews') is not True
            or terrain_physical.get('source_unchanged') is not True or terrain_physical.get('case_name')!=case_name
            or not isinstance(terrain_physical.get('sha256'),str) or not terrain_physical['sha256']
            or terrain_physical.get('valid_pixels')!=expectation['valid_pixels']
            or terrain_physical.get('invalid_pixels')!=expectation['width']*expectation['height']-expectation['valid_pixels']
            or terrain_physical.get('color_interpretations')!=['Gray']
            or terrain_physical.get('overview_sizes')!=[[80,128]]
            or artifact['bands'][0].get('overviews')!=[[80,128]]
            or artifact['bands'][0].get('color_interpretation')!='Gray'):
            raise SuiteError('DEM physical pixels/overview evidence differs')
        pending=[]
        terrain_lineage,browser=queue_output_inspection(pending,client,env['ADDP_ONLINE_TEST_RUN_ID'],terrain_source,
            terrain_locator,target_engine,name,identifier,case_name,identity,browser_runner,expectation,terrain_physical,
            [terrain_locator],artifact,size_evidence)
        finish_output_inspections(pending,repository,env,client,timeout)
        target=support.find_fixture_item(client,target_engine,'raster-target/'+name,'DEM target')
        terrain_hashes[case_name]=terrain_physical['sha256']
        terrain_cases.append({'case_name':case_name,'execution_id':identifier,'artifact':artifact,
            'data_item_size_bytes':target['size_bytes'],'automatic_target_scan_execution_id':scan,
            'lineage':terrain_lineage,'browser':browser,'physical':native})
    return {'schema_version': SCHEMA, 'suite': 'raster-workflow', 'result': 'passed',
            'run_id': env['ADDP_ONLINE_TEST_RUN_ID'], 'identity': identity, 'executions': executions,
            'automatic_target_scan_execution_id': last_scan, 'lineage': lineage,
            'physical': physical_evidence, 'size_evidence': size_evidence, 'browser': browser_evidence, 'duplicate_create': conflict_evidence,
            'spatial_cases': spatial_cases,
            'mosaic_source_cases': mosaic_source_cases, 'mosaic_editor_cases': mosaic_editor_cases,'terrain_cases':terrain_cases,
            'analysis_cases': analysis_cases, 'grid_cases': grid_cases, 'multiband_cases': multiband_cases,
            'utility_cases': utility_cases, 'foundation_cases': foundation_cases, 'reclass_cases': reclass_cases,
            'aggregate_cases': aggregate_cases, 'outside_cases': outside_cases, 'update_cases': update_cases, 'footprint_cases': footprint_cases, 'tiling_cases': tiling_results['create']['cases'], 'tiling_replace_cases': tiling_results['replace']['cases'],
            'tiling_automatic_scan_runs': tiling_results['create']['scans'], 'tiling_preserved_sha256': preserved,
            'tiling_replace_automatic_scan_runs': tiling_results['replace']['scans'],
            'tiling_replace_unrelated_sha256': tiling_results['replace']['physical']['unrelated_sha256'],
            'tiling_failed_publication': failures[fixture.TILE_FAILURE_PREFIX],
            'tiling_cleanup_failures': {prefix: failures[prefix] for prefix in fixture.TILE_CLEANUP_CASES},
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
                'ADDP_ONLINE_RASTER_SOURCE_ENGINE_ID', 'ADDP_ONLINE_RASTER_SOURCE_B_ENGINE_ID',
                'ADDP_ONLINE_RASTER_SOURCE_C_ENGINE_ID', 'ADDP_ONLINE_RASTER_TARGET_ENGINE_ID',
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
    images = importlib.import_module('scripts.test.raster-image-export-online')
    report['image_exports'] = images.run_scenario(sys.modules[__name__], Path(__file__).resolve().parents[2], env, client)
    report['resource_policy'] = finish_resource_policy(platform, tenant, setup, 30)
    print(json.dumps(report, ensure_ascii=False, sort_keys=True))


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print(f'Raster Online acceptance failed: {error if isinstance(error, SuiteError) else type(error).__name__}', file=sys.stderr)
        raise SystemExit(1)

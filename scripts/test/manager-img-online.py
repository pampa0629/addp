"""Native raster media subscenes of the existing disposable Manager Hosted suite."""
from __future__ import annotations

import base64
import hashlib
import importlib
import json
import os
import re
import subprocess
import sys
import time
from datetime import datetime, timedelta, timezone
from pathlib import Path
from urllib.parse import urlencode


SOURCES = (
    {'case': 'wgs84', 'object': 'raster/img-wgs84.img', 'source_srid': 4326,
     'extent': [113., 27.9744, 113.0256, 28.], 'band_count': 3},
    {'case': 'utm50n', 'object': 'raster/img-utm50n.img', 'source_srid': 32650,
     'extent': [500000., 3097440., 502560., 3100000.], 'band_count': 3},
    {'case': 'unlocated', 'object': 'raster/img-unlocated.img', 'source_srid': None, 'band_count': 3},
)

SOURCES = tuple(dict(spec, source_format='img', driver='HFA') for spec in SOURCES)
JPEG2000_SOURCES = tuple(dict(spec, source_format='jpeg2000', driver='JP2OpenJPEG',
    object='raster/jpeg2000-'+spec['case']+'.jp2') for spec in SOURCES) + (
    {'case':'codestream', 'object':'raster/jpeg2000-codestream.j2k', 'source_srid':None,
     'band_count':3, 'source_format':'jpeg2000', 'driver':'JP2OpenJPEG'},)

GRD_SOURCES = tuple({'case':case, 'object':'raster/grd-'+case+'.grd', 'source_srid':None,
    'source_format':'grd', 'driver':'GS7BG', 'width':5, 'height':3, 'band_count':1,
    'grid_facts':{'profile':'surfer7_binary', 'version':version, 'node_origin':origin,
                  'node_spacing':spacing, 'node_shape':[3,5], 'node_extent':extent,
                  'blank_value':blank}}
    for case,version,origin,spacing,extent,blank in (
        ('v1',1,[113.,28.],[.001,.002],[113.,28.,113.004,28.004],1.70141e38),
        ('v2',2,[500000.,3100000.],[10.,20.],[500000.,3100000.,500040.,3100040.],1000.))) + (
    {'case':'surfer6', 'object':'raster/grd-surfer6.grd', 'source_srid':None,
     'source_format':'grd', 'driver':'GSBG', 'width':5, 'height':3, 'band_count':1,
     'grid_facts':{'profile':'surfer6_binary', 'node_origin':[100.,200.], 'node_spacing':[2.,3.],
                   'node_shape':[3,5], 'node_extent':[100.,200.,108.,206.], 'blank_value':1.701410009187828e38}},
    {'case':'surfer6-ascii', 'object':'raster/grd-surfer6-ascii.grd', 'source_srid':None,
     'source_format':'grd', 'driver':'GSAG', 'width':5, 'height':3, 'band_count':1,
     'grid_facts':{'profile':'surfer6_ascii', 'node_origin':[100.,200.], 'node_spacing':[2.,3.],
                   'node_shape':[3,5], 'node_extent':[100.,200.,108.,206.], 'blank_value':1.70141e38}},)

TIFF_SOURCES=({'case':'unlocated-small-cog','object':'raster/tiff-unlocated-small-cog.tif',
    'source_srid':None,'source_format':'tiff','driver':'GTiff','width':240,'height':120,'band_count':1},)

NFS_TIFF_DIRECTORY = 'raster-tiff-authorization'
NFS_TIFF_NAME = NFS_TIFF_DIRECTORY + '/tiff-unlocated-small-cog.tif'

TIFF_COMPOSITIONS = {
    'msk': {'directory': 'raster-tiff-msk', 'single': 'without-mask.tif', 'multi': 'with-mask.tif',
            'auxiliary': [('with-mask.tif.msk', 'mask')], 'invalid_mask_pixels': 257},
    'sidecars': {'directory': 'raster-tiff-sidecars', 'single': 'without-sidecars.tif', 'multi': 'with-sidecars.tif',
                 'auxiliary': [('with-sidecars.hdr', 'header'), ('with-sidecars.tfw', 'world_file'),
                               ('with-sidecars.tif.aux.xml', 'auxiliary_metadata')], 'invalid_mask_pixels': 1},
}


def nfs_tiff_root(environment):
    if (any(environment.get(key) != value for key, value in {
        'ADDP_ONLINE_HOST': '1', 'ADDP_ONLINE_HOSTED': '1', 'GITHUB_ACTIONS': 'true',
        'RUNNER_OS': 'Linux', 'ADDP_ONLINE_MANAGER_CASE': 'native-raster-media',
    }.items()) or sys.platform != 'linux' or os.uname().machine != 'x86_64'):
        raise ValueError('NFS TIFF requires the exclusive Hosted Manager fixture')
    secret = Path(environment['ADDP_ONLINE_SECRET_DIR'])
    state = secret / 'nfs-root'
    if (not secret.is_absolute() or secret.is_symlink() or secret.stat().st_mode & 0o077
            or state.is_symlink() or not state.is_file()):
        raise ValueError('invalid private NFS fixture state')
    root = Path(state.read_text().strip())
    marker = root / '.addp-online-owner'
    if (not re.fullmatch(r'/tmp/addp-online-manager-nfs\.[A-Za-z0-9]{6}', str(root))
            or root.is_symlink() or not root.is_dir() or marker.is_symlink()
            or marker.read_text().strip() != environment['ADDP_ONLINE_TEST_RUN_ID']):
        raise ValueError('NFS TIFF export is not owned by this run')
    return root


def seed_nfs_tiff(environment, raw):
    root = nfs_tiff_root(environment)
    directory = root / NFS_TIFF_DIRECTORY
    directory.mkdir(mode=0o755)
    path = root / NFS_TIFF_NAME
    with path.open('xb') as stream:
        stream.write(raw)
    path.chmod(0o644)
    return path


def verify_nfs_tiff_authorization(owner, environment, client, evidence, raw, browser, report, timeout):
    grants = importlib.import_module('scripts.test.ftp-consumer-flow-online')
    engine = owner.positive_int(environment['ADDP_ONLINE_MANAGER_NFS_ENGINE_ID'], 'TIFF NFS Engine')
    tenant = owner.positive_int(environment['ADDP_ONLINE_TEST_TENANT_ID'], 'TIFF Tenant')
    issuer = owner.GatewayClient(environment['GATEWAY_URL'], environment['ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN'], 30)
    permissions = grants.REGISTRATION.SOURCE_INITIALIZER_PERMISSIONS | {'meta.scan_task.execute', 'meta.scan_task.read'}
    auth = grants.ModelSourceAuthorization(issuer, client, {
        'tenant_id': tenant, 'engine_id': engine, 'principal_id': evidence['identity']['principal_id'],
    }, source_type='nfs', consumer_permissions=owner.REQUIRED_PERMISSIONS, issuer_permissions=permissions)
    report.update(result='failed', engine_id=engine, issuer_principal_id=auth.operator,
                  consumer_principal_id=auth.recipient, initializer_permissions_verified=sorted(permissions), range_statuses={})
    grant_filter = {'recipient_type': 'user', 'recipient_id': auth.recipient,
                    'catalog_path': json.dumps(auth.target(NFS_TIFF_NAME))}
    receipt = None
    source = seed_nfs_tiff(environment, raw)
    try:
        execution = owner.wait_for_meta_scan(issuer, engine, time.monotonic() + timeout,
                                             scope={'catalog_paths': [NFS_TIFF_DIRECTORY]})
        scan = issuer.request('GET', f'/api/v1/meta/executions/{execution}', (200,)).payload
        if scan.get('triggered_by') != int(auth.operator) or scan.get('metadata', {}).get('items_scanned') != 1:
            raise owner.SuiteError('NFS TIFF scan did not identify exactly one owned file as the initializer')
        report['scan_execution_id'] = execution
        fingerprint = hashlib.sha256(f'{engine}:{NFS_TIFF_NAME}'.encode()).hexdigest()
        model = {'format': 'tiff', 'layout': 'single', 'full_name': NFS_TIFF_NAME, 'fingerprint': fingerprint,
                 'composition': [{'path': NFS_TIFF_NAME, 'role': 'image', 'required': True, 'primary': True}]}
        metadata_grant = auth.grant(model, action='metadata_read')
        item = owner.find_fixture_item(client, engine, NFS_TIFF_NAME, 'NFS TIFF')
        if item['item_type'] != 'file' or item['fingerprint'] != fingerprint:
            raise owner.SuiteError('NFS TIFF DataItem does not match the approved boundary')
        auth.revoke(metadata_grant['request_id'])
        locator = owner.build_item_locator(engine, item)
        item_path = f"/api/v1/meta/items/{item['id']}"
        range_path = '/api/v1/manager/storage-stream?' + urlencode({'locator': locator, 'storage_ref': NFS_TIFF_NAME})
        report.update(item_id=item['id'], locator=locator, metadata_grant_withdrawn=metadata_grant['request_id'])

        def check_range(phase, status):
            response = client.request('GET', range_path, (status,), headers={'Range': 'bytes=0-63'})
            content_range = next((value for key, value in response.headers.items() if key.lower() == 'content-range'), None)
            if status == 206:
                if content_range != f'bytes 0-63/{len(raw)}' or response.raw != raw[:64]:
                    raise owner.SuiteError('NFS TIFF Range bytes or Content-Range differ from the owned source')
            elif content_range is not None or not isinstance(response.payload, dict) or not response.payload.get('error'):
                raise owner.SuiteError('denied NFS TIFF Range returned an invalid error or byte response')
            report['range_statuses'][phase] = response.status

        client.request('GET', item_path, (404,))
        check_range('before_grant', 403)
        expiry = datetime.now(timezone.utc) + timedelta(seconds=90)
        receipt = auth.grant(model, expires_at=expiry)
        report['grant'] = receipt
        check_range('granted', 206)
        evidence['nfs_tiff_authorization'] = {'locator': locator, 'storage_ref': NFS_TIFF_NAME,
                                              'size_bytes': len(raw), 'item_id': item['id']}
        observed = validate_browser(owner, browser(evidence), evidence)
        time.sleep(max(0, expiry.timestamp() - time.time()) + 0.1)
        check_range('expired', 403)
        report['expired_meta_status'] = client.request('GET', item_path, (404,)).status
    finally:
        try:
            if receipt is not None and datetime.now(timezone.utc) >= expiry:
                history = issuer.request('GET', auth.base + '/access_grants/history?' + urlencode({
                    **grant_filter, 'state': 'expired',
                }), (200,)).payload
                rows = history.get('data')
                if (not isinstance(rows, list) or len(rows) != 1 or history.get('total') != 1
                        or any(rows[0].get(key) != value for key, value in receipt.items())
                        or rows[0].get('action') != 'read' or rows[0].get('revocation') is not None):
                    raise owner.SuiteError('NFS TIFF exact Grant natural expiry history is missing')
                auth.grants.remove(receipt['request_id'])
                auth.confirmed.remove(receipt['request_id'])
                report['natural_expiry_verified'] = True
            if source.read_bytes() != raw:
                raise owner.SuiteError('NFS TIFF source bytes changed')
            report['source_unchanged'] = True
        finally:
            auth.cleanup()
            current = issuer.request('GET', auth.base + '/access_grants?' + urlencode(grant_filter), (200,)).payload
            if current.get('total') != 0 or current.get('data') != []:
                raise owner.SuiteError('NFS TIFF left a current source Grant')
            report['zero_current_grants'] = True
    report['result'] = 'passed'
    return observed


def verify_nfs_tiff_composition(owner, environment, client, identity, physical, report, timeout, *, profile):
    grants = importlib.import_module('scripts.test.ftp-consumer-flow-online')
    spec = TIFF_COMPOSITIONS[profile]
    directory = spec['directory']
    root = nfs_tiff_root(environment)
    source = physical(profile + '-source', {})
    names = {spec['single'], spec['multi'], *(name for name, _ in spec['auxiliary'])}
    if (source.get('primary_bytes_equal') is not True or set(source.get('files', {})) != names
            or any(source.get('proofs', {}).get(layout, {}).get('invalid_mask_pixels') != count
                   for layout, count in (('single', 1), ('multi', spec['invalid_mask_pixels'])))):
        raise owner.SuiteError('TIFF composition physical source proof is incomplete')
    raw = {name: base64.b64decode(entry['base64'], validate=True) for name, entry in source['files'].items()}
    for name, data in raw.items():
        entry = source['files'][name]
        if len(data) != entry['size_bytes'] or hashlib.sha256(data).hexdigest() != entry['sha256']:
            raise owner.SuiteError('TIFF composition fixture bytes differ from the physical receipt')
    if raw[spec['single']] != raw[spec['multi']]:
        raise owner.SuiteError('TIFF composition paired TIFF primary bytes differ')
    (root / directory).mkdir(mode=0o755)
    archive = Path(environment['ADDP_ONLINE_ARTIFACT_DIR'])
    for name, data in raw.items():
        path = root / directory / name
        with path.open('xb') as stream: stream.write(data)
        path.chmod(0o644)
        (archive / ('nfs-' + profile + '-source-' + name)).write_bytes(data)
    engine = owner.positive_int(environment['ADDP_ONLINE_MANAGER_NFS_ENGINE_ID'], 'TIFF composition NFS Engine')
    tenant = owner.positive_int(environment['ADDP_ONLINE_TEST_TENANT_ID'], 'TIFF composition Tenant')
    issuer = owner.GatewayClient(environment['GATEWAY_URL'], environment['ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN'], 30)
    auth = grants.ModelSourceAuthorization(issuer, client, {
        'tenant_id': tenant, 'engine_id': engine, 'principal_id': identity['principal_id'],
    }, source_type='nfs', consumer_permissions=owner.REQUIRED_PERMISSIONS,
        issuer_permissions=grants.REGISTRATION.SOURCE_INITIALIZER_PERMISSIONS | {'meta.scan_task.execute', 'meta.scan_task.read'})
    report.update(result='failed', physical_sources=source['proofs'], items=[], cleanup_errors=[])
    fixture = None
    failure = None

    def read_member(item, member, status):
        data = raw[Path(member).name]
        response = client.request('GET', '/api/v1/manager/storage-stream?' + urlencode({
            'locator': item['locator'], 'storage_ref': member}), (status,), headers={'Range': f'bytes=0-{len(data)-1}'})
        content_range = next((v for k, v in response.headers.items() if k.lower() == 'content-range'), None)
        if status == 206:
            if response.raw != data or content_range != f'bytes 0-{len(data)-1}/{len(data)}':
                raise owner.SuiteError('TIFF composition member Range bytes or Content-Range differ')
        elif content_range is not None or response.raw == data or not isinstance(response.payload, dict) or not response.payload.get('error'):
            raise owner.SuiteError('denied TIFF composition member returned bytes or an invalid error')
        return {'path': member, 'status': response.status, 'size_bytes': len(response.raw),
                'sha256': hashlib.sha256(response.raw).hexdigest() if status == 206 else None}

    try:
        execution = owner.wait_for_meta_scan(issuer, engine, time.monotonic() + timeout,
                                             scope={'catalog_paths': [directory]})
        report['scan_execution_id'] = execution
        scan = issuer.request('GET', f'/api/v1/meta/executions/{execution}', (200,)).payload
        progress = scan.get('scan_progress')
        report['scan'] = {'status': scan.get('status'), 'triggered_by': scan.get('triggered_by'),
                          'items_scanned': scan.get('metadata', {}).get('items_scanned'), 'scan_progress': progress}
        # Meta counts physical inventory separately from resolved DataItems:
        # one single TIFF and one composite TIFF always resolve to two items.
        expected_progress = {'phase': 'finalizing', 'files_discovered': len(names), 'directories_discovered': 1,
                             'files_total_known': True, 'items_planned': 2, 'items_total_known': True,
                             'items_completed': 2, 'items_succeeded': 2, 'items_failed': 0}
        if (scan.get('status') != 'success' or scan.get('triggered_by') != int(auth.operator)
                or report['scan']['items_scanned'] != 2 or not isinstance(progress, dict)
                or any(type(progress.get(key)) is not type(value) or progress[key] != value
                       for key, value in expected_progress.items())):
            raise owner.SuiteError(f'{profile} TIFF scan must discover exactly {len(names)} files and successfully complete two logical DataItems')
        identities = issuer.request('GET', auth.base + '/data-items?' + urlencode({
            'search': directory, 'page': 1, 'page_size': 100}), (200,)).payload
        rows = identities.get('data', [])
        if identities.get('total') != 2 or {row.get('full_name') for row in rows} != {
                directory + '/' + spec['single'], directory + '/' + spec['multi']} or len(rows) != 2:
            raise owner.SuiteError('Meta must register exactly the single and multi TIFF DataItems')
        for name, layout in ((spec['single'], 'single'), (spec['multi'], 'multi')):
            full_name = directory + '/' + name
            row, = [row for row in rows if row['full_name'] == full_name]
            model = {'format': 'tiff', 'layout': layout, 'full_name': full_name,
                     'fingerprint': row['item_fingerprint'], 'composition': [
                         {'path': full_name, 'role': 'image', 'required': True, 'primary': True}]}
            if layout == 'multi':
                model['composition'].extend({'path': directory + '/' + member, 'role': role, 'required': False, 'primary': False}
                                            for member, role in spec['auxiliary'])
            facts = auth.facts(model)
            metadata_grant = auth.grant(model, action='metadata_read')
            item = owner.find_fixture_item(client, engine, full_name, 'TIFF composition')
            if item['fingerprint'] != row['item_fingerprint'] or item['item_type'] != 'file':
                raise owner.SuiteError('TIFF composition identity differs from formal Meta facts')
            auth.revoke(metadata_grant['request_id'])
            entry = {'layout': layout, 'item_id': item['id'], 'locator': owner.build_item_locator(engine, item),
                     'authorization_facts': facts, 'members': [ref['path'] for ref in model['composition']]}
            report['items'].append(entry)
            entry['before_grant'] = [read_member(entry, member, 403) for member in entry['members']]
            entry['grant'] = auth.grant(model)
            detail = client.request('GET', f"/api/v1/meta/items/{item['id']}", (200,)).payload
            if detail.get('attributes', {}).get('item', {}).get('layout') != layout:
                raise owner.SuiteError('Meta TIFF layout differs from its formal authorization boundary')
            entry['granted'] = [read_member(entry, member, 206) for member in entry['members']]
            if layout == 'multi':
                fixture = owner.ArtifactFixture('tiff-' + profile, item, entry['locator'])
        single, multi = report['items']
        report['cross_item_status'] = read_member(single, multi['members'][1], 400)['status']
        owner.generate_raster_cog(client, fixture, tenant, timeout, physical, source_facts={
            'case': profile, 'source_format': 'tiff', 'band_count': 1, 'source_srid': 4326,
            'extent': [113., 27.9744, 113.0256, 28.]})
        proof = fixture.artifact['physical']
        if proof.get('mask_verified') is not True or proof.get('invalid_mask_pixels') != spec['invalid_mask_pixels'] or proof.get('grid_verified') is not True:
            raise owner.SuiteError('generated TIFF composition COG complete mask proof is missing')
        report['cog'] = {'execution_id': fixture.execution_id, 'result_id': fixture.result_id,
                         'artifact': fixture.artifact}
        cog_raw = client.request('GET', fixture.artifact['preview_url'], (200,)).raw
        if len(cog_raw) != fixture.artifact['size_bytes'] or hashlib.sha256(cog_raw).hexdigest() != fixture.artifact['sha256']:
            raise owner.SuiteError('archived TIFF composition COG differs from the verified artifact')
        (archive / ('nfs-' + profile + '-result.cog.tif')).write_bytes(cog_raw)
    except BaseException as error:
        failure = error
        raise
    finally:
        try:
            if fixture is not None and fixture.task_id:
                request = fixture.artifact.get('physical_request') if fixture.artifact else None
                owner.cleanup_managed_artifact(client, fixture, timeout, 'raster_cog', owner.RASTER_TASK_TYPE)
                if request is None:
                    raise owner.SuiteError('TIFF composition generation has no physical cleanup identity')
                deleted = physical('deleted', request)
                if deleted != {'object_deleted': True, 'source_unchanged': True, 'residual_objects': 0}:
                    raise owner.SuiteError('TIFF composition COG object cleanup is incomplete')
                report['object_deleted'] = True
        except BaseException as error:
            report['cleanup_errors'].append(str(error))
        try:
            auth.cleanup()
            for entry in report['items']:
                entry['revoked'] = [read_member(entry, member, 403) for member in entry['members']]
                current = issuer.request('GET', auth.base + '/access_grants?' + urlencode({
                    'recipient_type': 'user', 'recipient_id': auth.recipient,
                    'catalog_path': json.dumps(auth.target(entry['authorization_facts']['full_name']))}), (200,)).payload
                if current.get('total') != 0 or current.get('data') != []:
                    raise owner.SuiteError('TIFF composition fixture left a current exact Grant')
            report['zero_current_grants'] = True
            if any((root / directory / name).read_bytes() != data for name, data in raw.items()):
                raise owner.SuiteError('TIFF composition TIFF source bytes changed')
            report['source_unchanged'] = True
        except BaseException as error:
            report['cleanup_errors'].append(str(error))
        if report['cleanup_errors'] and failure is None:
            raise owner.SuiteError('TIFF composition fixture cleanup failed: ' + '; '.join(report['cleanup_errors']))
    report['result'] = 'passed'


def validate_tiff_meta(owner, detail):
    attrs=owner._object(detail.get('attributes'),'TIFF attributes')
    item=attrs.get('item',{});media=attrs.get('type_info',{}).get('media',{});info=attrs.get('format_info',{}).get('tiff',{})
    if (any(item.get(k)!=v for k,v in {'data_type':'media','format':'tiff','layout':'single'}.items())
        or 'refs' in item or 'file_count' in item
        or any(media.get(k)!=v for k,v in {'kind':'image','encoding':'tiff','width':240,'height':120}.items())
        or info.get('profile')!='cog' or info.get('cog_check_level')!='heuristic'
        or info.get('is_cloud_optimized') is not True or info.get('is_tiled') is not True
        or info.get('has_overviews') is not False or info.get('big_tiff') is not False
        or info.get('page_count')!=1 or info.get('page_summary_status')!='parsed'
        or info.get('pages')!=[{'ifd_index':0,'width':240,'height':120}]):
        raise owner.SuiteError('small TIFF COG layout metadata differs from its physical source')
    if any(value.get('spatial') is not None for value in (attrs,attrs.get('type_info',{}),attrs.get('capabilities',{}))):
        raise owner.SuiteError('small TIFF acquired fabricated spatial facts')


def validate_tiff_physical(owner, facts):
    required={'case':'unlocated-small-cog','object':'raster/tiff-unlocated-small-cog.tif','source_srid':None,
        'driver':'GTiff','width':240,'height':120,'band_count':1,'dtype':'Float64','nodata_is_nan':True,
        'overview_count':0,'block_size':[512,512],'cog_valid':True,'cog_errors':0,'cog_warnings':0,
        'pixels_verified':28800,'valid_pixels':28765,'complete_mask_verified':True}
    if any(k not in facts or type(facts[k]) is not type(v) or facts[k]!=v for k,v in required.items()):
        raise owner.SuiteError('small TIFF complete physical proof is missing')


def read_content_metadata(owner, client, fixture):
    preview = client.request('GET', '/api/v1/manager/preview?' + urlencode({'locator': fixture.locator}), (200,)).payload
    data = owner._object(preview.get('data'), 'raster preview data')
    item = owner._object(data.get('item_meta'), 'raster preview metadata')
    obj = owner._object(data.get('object'), 'raster preview object')
    if (item.get('fingerprint') != fixture.item['fingerprint'] or item.get('full_name') != fixture.item['full_name']
            or not isinstance(item.get('attributes'), list)):
        raise owner.SuiteError('raster preview changed the source catalog identity')
    return {'attributes': {entry['key']: entry['value'] for entry in item['attributes']},
            'size_bytes': obj.get('size_bytes')}


def validate_structural_meta(owner, detail, spec):
    attrs = owner._object(detail.get('attributes'), 'raster structural attributes')
    expected = {'schema_version': 1, 'item': {'data_type': 'media', 'format': spec['source_format'], 'layout': 'single'}}
    if spec['source_srid'] is not None:
        spatial = owner._object(attrs.get('capabilities', {}).get('spatial'), 'declared raster CRS')
        if spatial.get('srid') != spec['source_srid'] or spatial.keys() - {
                'srid', 'crs_ref', 'crs_definitions', 'geometry_columns', 'primary_geometry_column'}:
            raise owner.SuiteError('Meta changed the declared CRS or disclosed content-derived spatial facts')
        expected['capabilities'] = {'spatial': spatial}
    if attrs != expected or 'size_bytes' in detail:
        raise owner.SuiteError('Meta raster projection must only disclose structure and declared CRS')


def refresh_tiff(owner, client, engine, fixture, detail, content, archive):
    snapshot_name = 'raster-meta-tiff-unlocated-small-cog'
    (archive / f'{snapshot_name}-before-refresh.json').write_text(
        json.dumps({'meta': detail, 'manager': content}, ensure_ascii=False, sort_keys=True) + '\n')
    path=f'/api/v1/manager/engines/{engine}/items/refresh'
    response=client.request('POST',path+'?'+urlencode({'locator':fixture.locator}),(200,)).payload
    if response.get('status')!='success' or type(response.get('items_scanned')) is not int or response['items_scanned']!=1:
        raise owner.SuiteError('small TIFF exact synchronous refresh did not succeed')
    after=client.request('GET',f"/api/v1/meta/items/{fixture.item['id']}",(200,)).payload
    try:
        started=datetime.fromisoformat(response['started_at'].replace('Z','+00:00'))
        scanned=datetime.fromisoformat(after['scanned_at'].replace('Z','+00:00'))
        previous=datetime.fromisoformat(detail['scanned_at'].replace('Z','+00:00'))
        if not started.tzinfo or not scanned.tzinfo or not previous.tzinfo or scanned<started or scanned<=previous:
            raise ValueError('refresh did not advance the exact item scan time')
    except (KeyError,TypeError,ValueError)as error:
        raise owner.SuiteError('small TIFF refresh timestamps are missing or inconsistent')from error
    full_name = fixture.item['full_name']
    if detail.get('full_name') != full_name:
        raise owner.SuiteError('small TIFF original catalog identity is inconsistent')
    after_content = read_content_metadata(owner, client, fixture)
    (archive / f'{snapshot_name}.json').write_text(
        json.dumps({'meta': after, 'manager': after_content, 'refresh_response': response},
                   ensure_ascii=False, sort_keys=True) + '\n')
    before_attrs, after_attrs = content['attributes'], after_content['attributes']
    try:
        previous_modified = datetime.fromisoformat(before_attrs['storage']['last_modified_at'])
        current_modified = datetime.fromisoformat(after_attrs['storage']['last_modified_at'])
    except (KeyError, TypeError, ValueError) as error:
        raise owner.SuiteError('small TIFF source modification timestamps are invalid') from error
    if previous_modified.utcoffset() is None or current_modified.utcoffset() is None or previous_modified != current_modified:
        raise owner.SuiteError('small TIFF refresh changed the source modification time')
    expected_attrs = {**before_attrs, 'storage': {
        **before_attrs['storage'], 'last_modified_at': after_attrs['storage']['last_modified_at'],
    }}
    if (after != {**detail, 'scanned_at': after['scanned_at']}
            or after_content != {**content, 'attributes': expected_attrs}):
        raise owner.SuiteError('small TIFF refresh changed source identity or format facts')
    validate_tiff_meta(owner,after_content)
    return after,after_content,{'path':path,'locator':fixture.locator,'response':response,
                  'full_name':full_name,'scanned_at_before':detail['scanned_at'],'scanned_at_after':after['scanned_at']}


def validate_meta(owner, detail, spec):
    if spec['source_format']=='tiff':
        return validate_tiff_meta(owner,detail)
    attrs = owner._object(detail.get('attributes'), 'IMG attributes')
    item = attrs.get('item', {})
    media = attrs.get('type_info', {}).get('media', {})
    info = attrs.get('format_info', {}).get(spec['source_format'], {})
    if (any(item.get(key) != value for key, value in {'data_type': 'media', 'format': spec['source_format'], 'layout': 'single'}.items())
            or any(media.get(key) != value for key, value in {'kind': 'image', 'encoding': spec['driver'], 'width': spec.get('width',256), 'height': spec.get('height',256)}.items())
            or info.get('scan_complete') is not True or info.get('driver') != spec['driver'] or info.get('band_count') != spec['band_count']):
        raise owner.SuiteError('IMG native Meta facts are incomplete')
    if any(info.get(key) != value for key,value in spec.get('grid_facts',{}).items()):
        raise owner.SuiteError('Surfer node grid or profile facts differ from the physical source')
    if spec['source_format']=='grd':
        bands=info.get('bands',[])
        dtype='Float32' if spec['driver']=='GSBG' else 'Float64'
        if (len(bands)!=1 or bands[0].get('dtype')!=dtype
            or bands[0].get('nodata')!=spec['grid_facts']['blank_value'] or bands[0].get('nodata_is_nan') is not False
            or (spec['driver'] in ('GSBG','GSAG') and 'version' in info)):
            raise owner.SuiteError('Surfer native dtype, exact NoData or header version facts changed')
    spatial = attrs.get('capabilities', {}).get('spatial', {})
    if spec['source_srid'] is None:
        if spatial:
            raise owner.SuiteError('unlocated IMG must not acquire inferred spatial facts')
    elif (spatial.get('srid') != spec['source_srid'] or len(spatial.get('extent', [])) != 4
            or any(abs(a - b) > 1e-9 for a, b in zip(spatial['extent'], spec['extent']))):
        raise owner.SuiteError('IMG source CRS or extent differs from its fixture')


def validate_browser(owner, report, evidence):
    for key, value in {'schema_version': 'addp.manager-raster-media-browser/v4', 'result': 'passed',
                       'run_id': evidence['run_id'], 'identity': evidence['identity'], 'source_format':evidence['source_format'],
                       'generation_requests': 0, 'failed_business_responses': 0, 'warnings_errors': 0}.items():
        if report.get(key) != value:
            raise owner.SuiteError('IMG browser report does not match ' + key)
    nfs_tiff = evidence.get('nfs_tiff_authorization')
    if nfs_tiff and report.get('nfs_tiff_authorization') != {
        'locator': nfs_tiff['locator'], 'item_id': nfs_tiff['item_id'],
        'range_status': 206, 'pixels_verified': True,
    }:
        raise owner.SuiteError('NFS TIFF browser identity, Range or pixel proof is incomplete')
    cases = report.get('cases', [])
    if len(cases) != len(evidence['cases']):
        raise owner.SuiteError('IMG browser must verify all three sources')
    for actual, expected in zip(cases, evidence['cases']):
        for key in ('case', 'item_id', 'locator', 'sha256', 'source_srid'):
            if actual.get(key) != expected.get(key):
                raise owner.SuiteError('IMG browser source identity changed')
        keys = ('metadata_verified', 'source_preview_verified')
        if evidence['source_format'] != 'envi':  # ENVI validates the two projections in its own report contract.
            keys += ('metadata_projection_verified', 'content_metadata_verified')
        if actual.get('download_sha256') != expected['sha256'] or any(actual.get(key) is not True for key in keys):
            raise owner.SuiteError('IMG browser source bytes or metadata were not verified')
        if expected['source_srid'] is None:
            keys=('map_absent','source_image_absent')
            if evidence['source_format']=='tiff':
                keys+=('tiff_pages_verified','tiff_pixels_verified','range_loaded','cog_metadata_visible',
                       'zero_overviews_verified','refreshed_metadata_verified','reopened')
                if actual.get('scanned_at')!=expected['refresh']['scanned_at_after']:
                    raise owner.SuiteError('small TIFF browser refresh differs')
            else:keys+=('unsupported_message_visible','generation_unavailable')
            if any(actual.get(key) is not True for key in keys):
                raise owner.SuiteError('unlocated IMG browser boundary is incomplete')
        else:
            if (actual.get('preview_url') != expected['preview_url']
                    or any(actual.get(key) is not True for key in ('range_loaded', 'map_loaded', 'reopened', 'reused_result'))
                    or any(type(actual.get('pixels', {}).get(key)) is not int or actual['pixels'][key] < 128
                           for key in ('cyan', 'magenta', 'yellow'))):
                raise owner.SuiteError('IMG browser COG pixels or reuse evidence is incomplete')
    return report


def run_browser(repository, environment, evidence, owner):
    path = Path(environment['ADDP_ONLINE_ARTIFACT_DIR']) / ('manager-'+evidence['source_format']+'-browser.json')
    path.unlink(missing_ok=True)
    env = {key: value for key, value in environment.items() if not key.endswith('_ACCESS_TOKEN')}
    env['ADDP_ONLINE_MANAGER_RASTER_MEDIA_JSON'] = json.dumps(evidence)
    result = subprocess.run(['npm', 'exec', '--', 'playwright', 'test', 'e2e/online/manager-img.spec.js',
                             '--config=playwright.online.config.js'], cwd=repository / 'console/frontend', env=env, check=False)
    if result.returncode or not path.is_file():
        raise owner.SuiteError('IMG Hosted browser acceptance did not succeed')
    return validate_browser(owner, json.loads(path.read_text()), evidence)


def run(repository, environment, owner, *, browser_runner=None, physical_runner=None, source_format="img"):
    sources = {"img": SOURCES, "jpeg2000": JPEG2000_SOURCES, "grd":GRD_SOURCES,'tiff':TIFF_SOURCES}[source_format]
    tenant = owner.positive_int(environment['ADDP_ONLINE_TEST_TENANT_ID'], 'IMG Tenant')
    engine = owner.positive_int(environment['ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID'], 'IMG Engine')
    client = owner.GatewayClient(environment['GATEWAY_URL'], environment['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'], 30)
    identity = owner.validate_user_identity(client, tenant)
    physical = physical_runner or (lambda action, request: owner.raster_physical(repository, environment, action, request))
    browser = browser_runner or (lambda evidence: run_browser(repository, environment, evidence, owner))
    timeout = float(environment.get('ADDP_ONLINE_MANAGER_LINEAGE_CONVERGENCE_TIMEOUT_SECONDS', '180'))
    seed = physical(source_format+'-source', {})
    if seed.get('source_unchanged') is not True or [x.get('case') for x in seed.get(source_format+'_sources', [])] != [x['case'] for x in sources]:
        raise owner.SuiteError('IMG physical source evidence is incomplete')
    fixtures, cases, snapshots = [], [], {}
    archive = Path(environment['ADDP_ONLINE_ARTIFACT_DIR'])
    report = {'schema_version': 'addp.manager-raster-media/v2', 'source_format': source_format, 'run_id': environment['ADDP_ONLINE_TEST_RUN_ID'],
              'identity': identity, 'result': 'incomplete', 'cases': cases, 'physical_sources': seed[source_format+'_sources'], 'cleanup': {}}
    error = None
    try:
        for spec, seeded in zip(sources, seed[source_format+'_sources']):
            if seeded.get('object') != spec['object'] or seeded.get('source_srid') != spec['source_srid']:
                raise owner.SuiteError('IMG physical receipt has a mismatched source')
            if source_format=='tiff':validate_tiff_physical(owner,seeded)
            item = owner.find_fixture_item(client, engine, environment['ADDP_ONLINE_MANAGER_MINIO_BUCKET'] + '/' + spec['object'], spec['case'])
            detail = client.request('GET', f"/api/v1/meta/items/{item['id']}", (200,)).payload
            validate_structural_meta(owner, detail, spec)
            fixture = owner.ArtifactFixture(source_format+'-' + spec['case'], item, owner.build_item_locator(engine, item))
            fixtures.append(fixture)
            content = read_content_metadata(owner, client, fixture)
            if content['size_bytes'] != seeded['size_bytes']:
                raise owner.SuiteError('Manager raster source size differs from its physical receipt')
            validate_meta(owner, content, spec)
            refresh=None
            if source_format=='tiff':
                detail, content, refresh = refresh_tiff(owner, client, engine, fixture, detail, content, archive)
            snapshots[item['id']] = (detail, content)
            downloaded = client.request('GET', '/api/v1/manager/downloads/file?' + urlencode({'locator': fixture.locator}), (200,))
            source_raw=downloaded.raw
            if len(source_raw) != seeded['size_bytes'] or hashlib.sha256(source_raw).hexdigest() != seeded['sha256']:
                raise owner.SuiteError('IMG original download differs from physical source')
            (archive / ('raster-source-'+source_format+'-'+spec['case']+Path(spec['object']).suffix)).write_bytes(source_raw)
            if source_format != 'tiff':
                (archive / ('raster-meta-'+source_format+'-'+spec['case']+'.json')).write_text(
                    json.dumps({'meta': detail, 'manager': content}, ensure_ascii=False, sort_keys=True)+'\n')
            case = {**spec, 'item_id': item['id'], 'locator': fixture.locator,
                    'sha256': seeded['sha256'], 'size_bytes': seeded['size_bytes'], 'metadata_verified': True}
            cases.append(case)
            if source_format=='tiff':case.update(refresh=refresh)
            if spec['source_srid'] is None:
                capability = client.request('GET', '/api/v1/manager/quick-view/capability?' + urlencode({'locator': fixture.locator}), (200,)).payload
                if capability.get('can_use_quick_view') or (source_format!='tiff' and 'generate_raster_cog' in capability.get('available_actions', [])):
                    raise owner.SuiteError('unlocated IMG unexpectedly permits a map or COG generation')
                if source_format!='tiff':case['generation_unavailable'] = True
                continue
            owner.generate_raster_cog(client, fixture, tenant, timeout, physical, source_facts=spec)
            task = client.request('GET', f'/api/v1/manager/tasks/{owner.RASTER_TASK_TYPE}/{fixture.task_id}', (200,)).payload
            if task.get('config', {}).get('raster', {}).get('source_format') != source_format:
                raise owner.SuiteError('IMG source-driven task lost its canonical source_format')
            raw_cog = client.request('GET', fixture.artifact['preview_url'], (200,)).raw
            if len(raw_cog)!=fixture.artifact['size_bytes'] or hashlib.sha256(raw_cog).hexdigest()!=fixture.artifact['sha256']:
                raise owner.SuiteError('archived raster COG differs from the verified artifact')
            (archive / ('raster-cog-'+source_format+'-'+spec['case']+'.tif')).write_bytes(raw_cog)
            case.update(task_id=fixture.task_id, execution_id=fixture.execution_id, result_id=fixture.result_id,
                        preview_url=fixture.artifact['preview_url'], artifact=fixture.artifact)
        browser_evidence = {'source_format': source_format, 'run_id': report['run_id'], 'identity': identity, 'cases': cases}
        if source_format == 'tiff' and environment.get('ADDP_ONLINE_MANAGER_CASE') == 'native-raster-media':
            report['browser'] = verify_nfs_tiff_authorization(owner, environment, client, browser_evidence,
                source_raw, browser, report.setdefault('nfs_tiff_authorization', {}), timeout)
            for profile in TIFF_COMPOSITIONS:
                verify_nfs_tiff_composition(owner, environment, client, identity, physical,
                    report.setdefault('nfs_tiff_' + profile, {}), timeout, profile=profile)
        else:
            report['browser'] = validate_browser(owner, browser(browser_evidence), browser_evidence)
        for fixture in fixtures:
            detail = client.request('GET', f"/api/v1/meta/items/{fixture.item['id']}", (200,)).payload
            if (detail, read_content_metadata(owner, client, fixture)) != snapshots[fixture.item['id']]:
                raise owner.SuiteError('raster generation or browser changed the source metadata')
            if fixture.result_id:
                query = urlencode({'task_id': fixture.task_id, 'page': 1, 'page_size': 100})
                results = client.request('GET', '/api/v1/manager/raster_cog?' + query, (200,)).payload
                rows = results.get('data', [])
                if results.get('total') != 1 or len(rows) != 1 or rows[0].get('id') != fixture.result_id or rows[0].get('last_execution_id') != fixture.execution_id:
                    raise owner.SuiteError('IMG cache reopen generated a different result or execution')
        report['result'] = 'passed'
    except BaseException as failure:
        error = failure
        report['result'] = 'failed'
        raise
    finally:
        cleanup_errors = []
        for fixture in reversed(fixtures):
            if fixture.task_id:
                try:
                    # Preserve the physical reference even when a later assertion failed.
                    request = fixture.artifact.get('physical_request')
                    if request is None and fixture.execution_id:
                        execution = client.request('GET', f'/api/v1/manager/executions/{fixture.execution_id}', (200,)).payload
                        if execution.get('status') == 'success':
                            facts = owner.validate_lineage(execution, item_locator=fixture.locator, item_id=fixture.item['id'],
                                fingerprint=fixture.item['fingerprint'], tenant_id=tenant, task_type=owner.RASTER_TASK_TYPE,
                                output_prefix='cog', output_suffix='.cog.tif', output_label='COG')
                            request = {'locator': facts['outputs'][0]['locator'], 'tenant_id': tenant,
                                       'fingerprint': fixture.item['fingerprint'], 'source_format': source_format, 'source_case': fixture.format.removeprefix(source_format+'-')}
                    owner.cleanup_managed_artifact(client, fixture, timeout, 'raster_cog', owner.RASTER_TASK_TYPE)
                    if request is not None:
                        deleted = physical('deleted', request)
                        if deleted != {'object_deleted': True, 'source_unchanged': True, 'residual_objects': 0}:
                            raise owner.SuiteError('IMG physical deletion was not verified')
                        fixture.cleanup.update(deleted)
                    elif fixture.result_id:
                        raise owner.SuiteError('IMG result has no owned physical cleanup reference')
                    report['cleanup'][fixture.format] = fixture.cleanup
                except Exception as failure:
                    cleanup_errors.append(str(failure))
        try:
            if physical(source_format+'-source', {}) != seed:
                raise owner.SuiteError('IMG sources changed during cleanup')
            report['source_unchanged'] = True
        except Exception as failure:
            cleanup_errors.append(str(failure))
        if cleanup_errors:
            report['result'] = 'failed'
            report['cleanup_errors'] = cleanup_errors
        (Path(environment['ADDP_ONLINE_ARTIFACT_DIR']) / ('manager-'+source_format+'.json')).write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
        if cleanup_errors:
            raise owner.SuiteError((str(error) + '; ' if error else '') + 'IMG cleanup failed: ' + '; '.join(cleanup_errors)) from error
    return report

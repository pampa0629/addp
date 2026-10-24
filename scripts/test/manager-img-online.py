"""Native raster media subscenes of the existing disposable Manager Hosted suite."""
from __future__ import annotations

import hashlib
import json
import subprocess
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



def validate_meta(owner, detail, spec):
    attrs = owner._object(detail.get('attributes'), 'IMG attributes')
    item = attrs.get('item', {})
    media = attrs.get('type_info', {}).get('media', {})
    info = attrs.get('format_info', {}).get(spec['source_format'], {})
    if (any(item.get(key) != value for key, value in {'data_type': 'media', 'format': spec['source_format'], 'layout': 'single'}.items())
            or any(media.get(key) != value for key, value in {'kind': 'image', 'encoding': spec['driver'], 'width': 256, 'height': 256}.items())
            or info.get('scan_complete') is not True or info.get('driver') != spec['driver'] or info.get('band_count') != 3):
        raise owner.SuiteError('IMG native Meta facts are incomplete')
    spatial = attrs.get('capabilities', {}).get('spatial', {})
    if spec['source_srid'] is None:
        if spatial:
            raise owner.SuiteError('unlocated IMG must not acquire inferred spatial facts')
    elif (spatial.get('srid') != spec['source_srid'] or len(spatial.get('extent', [])) != 4
            or any(abs(a - b) > 1e-9 for a, b in zip(spatial['extent'], spec['extent']))):
        raise owner.SuiteError('IMG source CRS or extent differs from its fixture')


def validate_browser(owner, report, evidence):
    for key, value in {'schema_version': 'addp.manager-raster-media-browser/v2', 'result': 'passed',
                       'run_id': evidence['run_id'], 'identity': evidence['identity'], 'source_format':evidence['source_format'],
                       'generation_requests': 0, 'failed_business_responses': 0, 'warnings_errors': 0}.items():
        if report.get(key) != value:
            raise owner.SuiteError('IMG browser report does not match ' + key)
    cases = report.get('cases', [])
    if len(cases) != len(evidence['cases']):
        raise owner.SuiteError('IMG browser must verify all three sources')
    for actual, expected in zip(cases, evidence['cases']):
        for key in ('case', 'item_id', 'locator', 'sha256', 'source_srid'):
            if actual.get(key) != expected.get(key):
                raise owner.SuiteError('IMG browser source identity changed')
        if actual.get('download_sha256') != expected['sha256'] or actual.get('metadata_verified') is not True:
            raise owner.SuiteError('IMG browser source bytes or metadata were not verified')
        if expected['source_srid'] is None:
            if any(actual.get(key) is not True for key in ('generation_unavailable', 'map_absent', 'source_image_absent', 'unsupported_message_visible')):
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
    env = dict(environment, ADDP_ONLINE_MANAGER_RASTER_MEDIA_JSON=json.dumps(evidence))
    result = subprocess.run(['npm', 'exec', '--', 'playwright', 'test', 'e2e/online/manager-img.spec.js',
                             '--config=playwright.online.config.js'], cwd=repository / 'console/frontend', env=env, check=False)
    if result.returncode or not path.is_file():
        raise owner.SuiteError('IMG Hosted browser acceptance did not succeed')
    return validate_browser(owner, json.loads(path.read_text()), evidence)


def run(repository, environment, owner, *, browser_runner=None, physical_runner=None, source_format="img"):
    sources = {"img": SOURCES, "jpeg2000": JPEG2000_SOURCES}[source_format]
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
    report = {'schema_version': 'addp.manager-raster-media/v1', 'source_format': source_format, 'run_id': environment['ADDP_ONLINE_TEST_RUN_ID'],
              'identity': identity, 'result': 'incomplete', 'cases': cases, 'cleanup': {}}
    error = None
    try:
        for spec, seeded in zip(sources, seed[source_format+'_sources']):
            if seeded.get('object') != spec['object'] or seeded.get('source_srid') != spec['source_srid']:
                raise owner.SuiteError('IMG physical receipt has a mismatched source')
            item = owner.find_fixture_item(client, engine, environment['ADDP_ONLINE_MANAGER_MINIO_BUCKET'] + '/' + spec['object'], spec['case'])
            if item.get('size_bytes') != seeded['size_bytes']:
                raise owner.SuiteError('IMG Meta source size differs from its physical receipt')
            detail = client.request('GET', f"/api/v1/meta/items/{item['id']}", (200,)).payload
            validate_meta(owner, detail, spec)
            snapshots[item['id']] = detail
            fixture = owner.ArtifactFixture(source_format+'-' + spec['case'], item, owner.build_item_locator(engine, item))
            fixtures.append(fixture)
            downloaded = client.request('GET', '/api/v1/manager/downloads/file?' + urlencode({'locator': fixture.locator}), (200,))
            if len(downloaded.raw) != seeded['size_bytes'] or hashlib.sha256(downloaded.raw).hexdigest() != seeded['sha256']:
                raise owner.SuiteError('IMG original download differs from physical source')
            archive = Path(environment['ADDP_ONLINE_ARTIFACT_DIR'])
            (archive / ('raster-source-'+source_format+'-'+spec['case']+Path(spec['object']).suffix)).write_bytes(downloaded.raw)
            (archive / ('raster-meta-'+source_format+'-'+spec['case']+'.json')).write_text(json.dumps(detail, ensure_ascii=False, sort_keys=True)+'\n')
            case = {**spec, 'item_id': item['id'], 'locator': fixture.locator,
                    'sha256': seeded['sha256'], 'size_bytes': seeded['size_bytes'], 'metadata_verified': True}
            cases.append(case)
            if spec['source_srid'] is None:
                capability = client.request('GET', '/api/v1/manager/quick-view/capability?' + urlencode({'locator': fixture.locator}), (200,)).payload
                if capability.get('can_use_quick_view') or 'generate_raster_cog' in capability.get('available_actions', []):
                    raise owner.SuiteError('unlocated IMG unexpectedly permits a map or COG generation')
                case['generation_unavailable'] = True
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
        report['browser'] = validate_browser(owner, browser(browser_evidence), browser_evidence)
        for fixture in fixtures:
            if client.request('GET', f"/api/v1/meta/items/{fixture.item['id']}", (200,)).payload != snapshots[fixture.item['id']]:
                raise owner.SuiteError('IMG generation or browser changed the source Meta snapshot')
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

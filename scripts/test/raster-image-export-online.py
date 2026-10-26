"""PNG/JPEG phase of the existing disposable raster-workflow Hosted suite."""
from __future__ import annotations

import copy
import math


SCHEMA = 'addp.raster-image-export-online/v1'
BROWSER_SCHEMA = 'addp.raster-image-editor/v1'


def image_definition(source, parent, name):
    # The real editor must change this TIFF default to PNG/JPEG before saving.
    return {'tasks': [
        {'id': 'load', 'operator': 'raster_load', 'depends_on': [], 'params': {'locator': source}},
        {'id': 'save', 'operator': 'raster_save', 'depends_on': ['load'], 'params': {
            'input_raster': {'$ref': 'load'}, 'target_parent_locator': parent,
            'target_name': name, 'write_mode': 'create', 'profile': 'geotiff'}}]}


def validate_editor(owner, report, evidence):
    if (set(report) != set(evidence) | {'schema_version', 'result', 'cases', 'page_requests'}
        or any(report.get(k) != v for k,v in evidence.items())
        or report.get('schema_version') != BROWSER_SCHEMA or report.get('result') != 'passed'
        or report.get('page_requests') != {'create':2,'update':0,'execute':2}
        or any(type(v) is not int for v in report['page_requests'].values())):
        raise owner.SuiteError('ordinary-image editor did not bind the exact run/User and two real writes')
    cases = owner.array(report.get('cases'), 'image editor cases')
    if [case.get('profile') for case in cases] != ['png', 'jpeg']:
        raise owner.SuiteError('ordinary-image editor is missing a codec or has extra cases')
    ids = set()
    for case in cases:
        profile = case['profile']
        if (type(case.get('task_id')) is not int or case['task_id'] <= 0
            or case.get('source_task_id') != str(case['task_id'])
            or not isinstance(case.get('execution_id'), str) or not case['execution_id']
            or case['execution_id'] in ids or case.get('reloaded') is not True or case.get('deleted') is not True):
            raise owner.SuiteError('ordinary-image task/reload/execution/cleanup receipt is incomplete')
        ids.add(case['execution_id'])
        tasks = owner.array(owner.obj(case.get('workflow_definition'), 'image definition').get('tasks'), 'image tasks')
        loads = [t for t in tasks if t.get('operator') == 'raster_load']
        saves = [t for t in tasks if t.get('operator') == 'raster_save']
        if len(tasks) != 2 or len(loads) != 1 or len(saves) != 1:
            raise owner.SuiteError('ordinary-image editor changed the two-node workflow')
        source, target = loads[0], saves[0]
        params = owner.obj(target.get('params'), 'image target params')
        if (source.get('params',{}).get('locator') != evidence['sources'][profile]['locator']
            or params.get('profile') != profile or params.get('target_name') != evidence['target_names'][profile]
            or params.get('target_parent_locator') != evidence['target_parent_locator']
            or params.get('write_mode') != 'create' or params.get('input_raster',{}).get('$ref') != source.get('id')
            or target.get('depends_on') != [source.get('id')]
            or (profile == 'jpeg' and params.get('jpeg_quality') != evidence['jpeg_quality'])
            or any(set(t.get('params',{})) & {'access_plan','connection_info','engine_id'} for t in tasks)):
            raise owner.SuiteError('ordinary-image editor changed public source/target/codec/quality or exposed private access')
    return cases


def validate_artifact(owner, artifact, profile, quality):
    expected = {'artifact_type':'raster','format':profile,'profile':profile,'width':37,'height':23,
        'band_count':4 if profile=='png' else 3,'lossy':profile=='jpeg',
        'crs':'','source_crs':'','extent_srid':0,'extent':[],'transform':[],'uploaded_files':1}
    if any(artifact.get(k) != v for k,v in expected.items()):
        raise owner.SuiteError('ordinary-image artifact changed its encoding, dimensions or spatial omission')
    if profile == 'jpeg' and (artifact.get('jpeg_quality') != quality or type(artifact['jpeg_quality']) is not int):
        raise owner.SuiteError('ordinary JPEG did not preserve the explicit quality option')
    if profile == 'png' and 'jpeg_quality' in artifact:
        raise owner.SuiteError('PNG artifact incorrectly claims JPEG quality')
    bands = owner.array(artifact.get('bands'), 'ordinary-image bands')
    colors = ['Red','Green','Blue'] + (['Alpha'] if profile=='png' else [])
    if len(bands) != len(colors) or any(b.get('dtype')!='Byte' or b.get('color_interpretation')!=c for b,c in zip(bands,colors)):
        raise owner.SuiteError('ordinary-image artifact changed bit depth or colour roles')


def run_scenario(owner, repository, env, client, *, physical_runner=None, editor_runner=None):
    physical_runner = physical_runner or owner.physical
    editor_runner = editor_runner or (lambda r,e,v: owner.raster_browser_report(r,e,v,'image-editor','栅格普通图片'))
    identity = owner.validate_identity(client, owner.positive(env['ADDP_ONLINE_TEST_TENANT_ID'],'tenant'),
                                       required_permissions=owner.EDITOR_PERMISSIONS)
    source_engine = owner.positive(env['ADDP_ONLINE_RASTER_SOURCE_ENGINE_ID'],'image source')
    target_engine = owner.positive(env['ADDP_ONLINE_RASTER_TARGET_ENGINE_ID'],'image target')
    if source_engine == target_engine: raise owner.SuiteError('ordinary-image source and target must be separate')
    runtime = [item for item in owner.array(client.request('GET','/api/v1/develop/workflow-engines',(200,)).payload,'runtimes')
               if item.get('engine_type')=='geopython_workflow' and item.get('connection_status')=='online']
    if len(runtime)!=1: raise owner.SuiteError('ordinary images require the exact product Runtime')
    sources, snapshots = {}, {}
    for profile,name in owner.fixture.IMAGE_SOURCES.items():
        item = owner.support.find_fixture_item(client,source_engine,'raster-source/'+name,'ordinary image source')
        snapshot = owner.obj(client.request('GET',f'/api/v1/meta/items/{item["id"]}',(200,)).payload,'image source item')
        snapshots[profile] = copy.deepcopy({k:snapshot.get(k) for k in ('attributes','fingerprint','full_name','size_bytes','updated_at')})
        sources[profile] = {'id':item['id'],'name':name,'locator':owner.support.build_item_locator(source_engine,item)}
    parent = f'addp://engine/{target_engine}/path/raster-target?type=bucket'
    evidence = {'run_id':env['ADDP_ONLINE_TEST_RUN_ID'],'case_name':'ordinary-images',**identity,
        'workflow_engine_id':owner.positive(runtime[0].get('id'),'image Runtime'),
        'sources':sources,'target_parent_locator':parent,'target_names':owner.fixture.IMAGE_TARGETS,
        'jpeg_quality':owner.fixture.IMAGE_QUALITY,
        'task_names':{p:env['ADDP_ONLINE_TEST_RUN_ID']+'-ordinary-'+p for p in ('png','jpeg')},
        'definitions':{p:image_definition(sources[p]['locator'],parent,name) for p,name in owner.fixture.IMAGE_TARGETS.items()}}
    baseline = physical_runner(repository,env,'prepare-images')
    if baseline.get('source_unchanged') is not True or baseline.get('images') != {} or not baseline.get('preserved_sha256'):
        raise owner.SuiteError('ordinary-image physical baseline is incomplete')
    browser = editor_runner(repository,env,evidence)
    cases = validate_editor(owner,browser,evidence)
    physical = physical_runner(repository,env,'verify-images')
    if (physical.get('source_unchanged') is not True or physical.get('preserved_sha256') != baseline['preserved_sha256']
        or set(physical.get('images',{})) != {'png','jpeg'}):
        raise owner.SuiteError('ordinary images changed sources/prior results or have incomplete physical evidence')
    results = []
    timeout = float(env.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS','180'))
    for case in cases:
        profile = case['profile'];name = owner.fixture.IMAGE_TARGETS[profile]
        execution = owner.wait_execution(client,'develop',case['execution_id'],timeout)
        if execution.get('source_task_id') != str(case['task_id']):
            raise owner.SuiteError('ordinary-image execution has a different saved task')
        result = owner.obj(owner.obj(execution.get('metadata'),'image metadata').get('result'),'image result')
        artifact = owner.obj(result.get('final_result'),'image artifact')
        validate_artifact(owner,artifact,profile,evidence['jpeg_quality'])
        locator = f'addp://engine/{target_engine}/path/raster-target/{name}?type=object'
        outputs = owner.obj(execution.get('outputs'),'image stable outputs')
        save = next(t for t in case['workflow_definition']['tasks'] if t['operator']=='raster_save')
        if outputs != {save['id']:{'resource':{'locator':locator,'type':'object','write_mode':'create'}}}:
            raise owner.SuiteError('ordinary-image stable output is not the exact selected resource')
        scans = owner.array(result.get('meta_scan_runs'),'image automatic scans')
        if len(scans)!=1 or scans[0].get('target_locator')!=locator or scans[0].get('status')!='submitted':
            raise owner.SuiteError('ordinary image is missing its exact automatic metadata scan')
        owner.wait_execution(client,'meta',scans[0]['execution_id'],timeout)
        target = owner.support.find_fixture_item(client,target_engine,'raster-target/'+name,'ordinary image target')
        target = owner.obj(client.request('GET',f'/api/v1/meta/items/{target["id"]}',(200,)).payload,'ordinary image Meta item')
        attrs = owner.obj(target.get('attributes'),'ordinary image attrs')
        media = owner.obj(owner.obj(attrs.get('type_info'),'image type info').get('media'),'image media facts')
        if (attrs.get('item',{}).get('format')!=profile or (media.get('width'),media.get('height'))!=(37,23)
            or attrs.get('capabilities',{}).get('spatial')):
            raise owner.SuiteError('ordinary-image metadata has a wrong format/size or fabricated spatial capability')
        native = physical['images'][profile]
        mean_error, max_error = native.get('mean_absolute_error'), native.get('max_absolute_error')
        valid_error = (type(mean_error) in (int, float) and math.isfinite(mean_error) and mean_error >= 0
                       and type(max_error) is int and max_error >= 0)
        if (native.get('format')!=profile or native.get('lossy') is not (profile=='jpeg')
            or (native.get('width'), native.get('height'), native.get('band_count')) != (37,23,4 if profile=='png' else 3)
            or native.get('georeferencing_absent') is not True or native.get('sidecars_absent') is not True
            or not native.get('sha256') or not native.get('decoded_sha256')
            or not valid_error or (profile=='png' and (mean_error!=0 or max_error!=0))
            or (profile=='jpeg' and (mean_error>=5 or max_error>=15))):
            raise owner.SuiteError('ordinary-image native pixel/codec/spatial evidence is incomplete')
        sizes = owner.strict_sizes(artifact,native,target,name)
        current = owner.obj(client.request('GET',f'/api/v1/meta/items/{sources[profile]["id"]}',(200,)).payload,'unchanged image source')
        if {k:current.get(k) for k in snapshots[profile]}!=snapshots[profile]:
            raise owner.SuiteError('ordinary-image export modified source metadata')
        results.append({'profile':profile,'execution_id':case['execution_id'],'task_id':case['task_id'],
            'target_item_id':target['id'],'target_locator':locator,'automatic_scan':scans[0]['execution_id'],
            'artifact':artifact,'physical':native,'size_evidence':sizes,'source_unchanged':True})
    return {'schema_version':SCHEMA,'result':'passed','run_id':evidence['run_id'],
            'identity':identity,'cases':results,'browser':browser,'preserved_sha256':physical['preserved_sha256']}

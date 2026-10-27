#!/usr/bin/env python3
"""Ordinary User topology/repair/snap/overlap/gap/endpoints -> exact scans/lineage -> Console."""
import importlib
import json
import os
from pathlib import Path
import subprocess
import sys
import time

raster = importlib.import_module('scripts.test.raster-workflow-online')
fixture = importlib.import_module('business.scripts.online-vector-topology-fixture')
support, SuiteError = raster.support, raster.SuiteError
obj, array, positive = raster.obj, raster.array, raster.positive
SUITE = fixture.SUITE
PERMISSIONS = raster.PERMISSIONS | {'manager.data_item.read'}
vector = importlib.import_module('scripts.utils.online_vector_persistence')
scan_ids = importlib.import_module('scripts.test.vector-pmtiles-workflow-online').scan_ids


def normalize_definition(source,parent,failed=False,case=fixture.NORMALIZE_CASE):
    profile=fixture.network_profile(case);composition=case==fixture.COMPOSITION_CASE
    tasks=[{'id':'normal_input','operator':'load','params':{'locator':source},'depends_on':[]},
        {'id':'normal','operator':'normalize_line_network','params':{'input_gdf':{'$ref':'normal_input'},'id_field':'route_key' if composition or not failed else 'missing_id'},'depends_on':['normal_input']}]
    if composition:
        tasks.append({'id':'chains','operator':'merge_line_chains','params':{'input_gdf':{'$ref':'normal'},
            'id_field':'missing_id' if failed else 'edge_id','match_fields':[]},'depends_on':['normal']})
    for port,name in zip(profile['ports'],profile['targets']):
        ref=({'$ref':'chains'} if port=='chains' else {'$ref':'chains','port':'members'} if port=='chain_edges' else {'$ref':'normal','port':'members'}) if composition else ({'$ref':'normal'} if port=='result' else {'$ref':'normal','port':'members'})
        tasks.append({'id':'save_'+port,'operator':'save','params':{'input_df':ref,
            'target_parent_locator':parent,'target_name':('failed-' if failed else '')+name,'mode':'fail'},
            'depends_on':['normal','chains'] if composition and port=='edge_sources' else ['chains'] if composition else ['normal']})
    return {'tasks':tasks}


def validate_normalize_success(execution,source,targets,case=fixture.NORMALIZE_CASE):
    count=len(fixture.network_profile(case)['ports'])
    outputs=obj(execution.get('outputs'),'normalize stable outputs')
    expected={'save_'+port:{'resource':{'locator':locator,'type':'file','write_mode':'create'}} for port,locator in targets.items()}
    if outputs!=expected:raise SuiteError('normalization must publish exactly the selected named ports')
    metadata=obj(execution.get('metadata'),'normalize metadata');facts=obj(metadata.get('lineage_facts'),'normalize facts')
    inputs=array(facts.get('inputs'),'normalize inputs');produced=array(facts.get('outputs'),'normalize outputs')
    if (facts.get('schema_version')!='addp.lineage-facts/v1' or
        [(x.get('port'),x.get('locator')) for x in inputs]!=[('input.normal_input',source)] or
        len(produced)!=count or {x.get('locator') for x in produced}!=set(targets.values()) or
        any(x.get('write_mode')!='create' for x in produced)):
        raise SuiteError('normalization source port or selected create facts differ')
    scans=array(obj(metadata.get('result'),'normalize result').get('meta_scan_runs'),'normalize scans')
    if (len(scans)!=count or {s.get('target_locator') for s in scans}!=set(targets.values()) or
        any(s.get('status')!='submitted' or not isinstance(s.get('execution_id'),str) or not s['execution_id'] for s in scans) or
        len({s['execution_id'] for s in scans})!=count):raise SuiteError('normalization requires exactly the selected distinct automatic scans')
    return facts,scans


def run_normalize_scenario(repository,env,client,physical_runner,browser_runner,diagnostic_runner):
    case=fixture.topology_case(env);profile=fixture.network_profile(case)
    ports,targets_names=profile['ports'],profile['targets'];source_name=profile['source'];schema=profile['schema']
    composition=case==fixture.COMPOSITION_CASE
    tenant=positive(env['ADDP_ONLINE_TEST_TENANT_ID'],'tenant')
    if tenant<=1:raise SuiteError('normalization forbids default Tenant')
    identity=raster.validate_identity(client,tenant,required_permissions=PERMISSIONS)
    timeout=float(env.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS','600'))
    engine=positive(env['ADDP_ONLINE_TOPOLOGY_NFS_ENGINE_ID'],'NFS engine')
    scope=f'addp://engine/{engine}/path/{source_name}?type=file'
    scan=support.wait_for_meta_scan(client,engine,time.monotonic()+timeout,scope={'targets':[scope]})
    item=support.find_fixture_item(client,engine,source_name,'normalization source')
    before=client.request('GET',f'/api/v1/meta/items/{item["id"]}',(200,)).payload
    source={'name':source_name,'id':item['id'],'locator':scope+f'&item_id={item["id"]}','scan_execution_id':scan,'before':before}
    selected=[r for r in array(client.request('GET','/api/v1/develop/workflow-engines',(200,)).payload,'runtimes')
              if r.get('engine_type')=='geopython_workflow' and r.get('connection_status')=='online']
    if len(selected)!=1:raise SuiteError('normalization requires one online product Runtime')
    runtime=positive(selected[0]['id'],'Runtime');parent=f'addp://engine/{engine}/path/outputs?type=directory'
    targets={port:f'addp://engine/{engine}/path/outputs/{name}?type=file' for port,name in zip(ports,targets_names)}
    identifier=raster.submit(client,runtime,normalize_definition(source['locator'],parent,case=case))
    execution=raster.wait_execution(client,'develop',identifier,timeout)
    facts,scans=validate_normalize_success(execution,source['locator'],targets,case)
    observed=[]
    for scan in scans:
        result=raster.wait_execution(client,'meta',scan['execution_id'],timeout)
        if (type(result.get('tenant_id')) is not int or result['tenant_id']!=tenant or
            type(result.get('triggered_by')) is not int or result['triggered_by']!=int(identity['principal_id'])):
            raise SuiteError('normalization automatic scan public User/Tenant differs')
        observed.append({'execution_id':scan['execution_id'],'tenant_id':result['tenant_id'],'triggered_by':result['triggered_by'],'status':result['status']})
    monitor=obj(client.request('GET',f'/api/v1/monitor/executions/by-execution-id/{identifier}',(200,)).payload,'normalize Monitor')
    if monitor.get('status')!='success' or monitor.get('module')!='develop' or obj(monitor.get('metadata'),'Monitor metadata').get('lineage_facts')!=facts:
        raise SuiteError('normalization Monitor status or facts differ')
    native=physical_runner(repository,env)
    if (native.get('exact_inventory_verified') is not True or set(native.get('sources',{}))!={source_name} or
        set(native.get('targets',{}))!=set(targets_names)):raise SuiteError('normalization exact one-source two-output physical inventory differs')
    outputs=[]
    for port,name in zip(ports,targets_names):
        target=support.find_fixture_item(client,engine,'outputs/'+name,'normalized output')
        detail=obj(client.request('GET',f'/api/v1/meta/items/{target["id"]}',(200,)).payload,'normalization target')
        sizes=validate_item(detail,native['targets'][name],name)
        outputs.append({'port':port,'name':name,'locator':targets[port],'item_id':target['id'],
                        'automatic_scan_execution_id':next(s['execution_id'] for s in scans if s['target_locator']==targets[port]),
                        **native['targets'][name],'size_evidence':sizes})
    checkpoint={'schema_version':schema+'-checkpoint/v1','case':case,'result':'incomplete',
                'stage':'before_lineage','run_id':env['ADDP_ONLINE_TEST_RUN_ID'],'identity':identity,'execution_id':identifier,
                'source':source,'outputs':outputs,'lineage_facts':facts,'stable_outputs':execution['outputs'],
                'automatic_scan_observations':observed,'monitor':monitor,'physical':native}
    directory=Path(env['ADDP_ONLINE_ARTIFACT_DIR'])
    (directory/'topology-before-lineage.json').write_text(json.dumps(checkpoint,indent=2)+'\n')
    for output in outputs:
        output['lineage']=[raster.wait_lineage(client,source['id'],output['item_id'],identifier,timeout)]
    if client.request('GET',f'/api/v1/meta/items/{source["id"]}',(200,)).payload!=before:raise SuiteError('normalization source Meta changed')
    scans_before=scan_ids(client)
    if scans_before!=sorted([source['scan_execution_id'],*[s['execution_id'] for s in scans]]):raise SuiteError('normalization source/output scan inventory differs')
    failed_id=raster.submit(client,runtime,normalize_definition(source['locator'],parent,failed=True,case=case))
    failed=raster.wait_execution(client,'develop',failed_id,timeout,expected='failed')
    fm=obj(failed.get('metadata'),'failed normalize metadata');fr=fm.get('result')
    if failed.get('status')!='failed' or failed.get('outputs') or fm.get('lineage_facts') or (isinstance(fr,dict) and fr.get('meta_scan_runs')):
        raise SuiteError('normalization validation failure must publish neither port nor scan/facts')
    diagnostic=diagnostic_runner(repository,env,failed_id,task='chains' if composition else 'normal',
        reason='id_field must explicitly name one existing non-geometry scalar column',cause='invalid_chain_id_field' if composition else 'invalid_normalize_id_field')
    failed_monitor=obj(client.request('GET',f'/api/v1/monitor/executions/by-execution-id/{failed_id}',(200,)).payload,'failed normalize Monitor')
    if failed_monitor.get('status')!='failed' or failed_monitor.get('module')!='develop':raise SuiteError('normalization failure Monitor differs')
    if scan_ids(client)!=scans_before or physical_runner(repository,env)!=native:raise SuiteError('normalization failure changed scans or physical files')
    failure={'execution_id':failed_id,'status':'failed','no_publication_or_scan':True,'diagnostic':diagnostic,
             'observation':{'status':failed['status'],'outputs':failed.get('outputs'),'lineage_facts':fm.get('lineage_facts'),
                            'meta_scan_runs':fr.get('meta_scan_runs') if isinstance(fr,dict) else None},'monitor':failed_monitor}
    (directory/'topology-normalize-failure.json').write_text(json.dumps({**checkpoint,'stage':'field_failure_validated','failed_normalize':failure},indent=2)+'\n')
    evidence={'case':case,'run_id':env['ADDP_ONLINE_TEST_RUN_ID'],**identity,'execution_id':identifier,
              'failed_execution_id':failed_id,'source':source,'outputs':[{k:v for k,v in o.items() if k not in ('lineage','size_evidence')} for o in outputs],
              'expected':{port:fixture.network_features(case,port) for port in ports}}
    report=browser_runner(repository,env,evidence)
    final=physical_runner(repository,env)
    if final!=native:raise SuiteError('normalization browser changed source or outputs')
    return {'schema_version':schema+'/v1','case':case,'suite':SUITE,'result':'passed',
            'run_id':env['ADDP_ONLINE_TEST_RUN_ID'],'identity':identity,'execution_id':identifier,'source':source,
            'outputs':outputs,'stable_outputs':execution['outputs'],'lineage_facts':facts,'automatic_scans':scans,
            'automatic_scan_observations':observed,'monitor':monitor,'physical':native,'post_browser_physical':final,
            'failed_normalize':failure,'browser':report,'source_unchanged':True,
            'cleanup_boundary':'owned NFS export/directory and disposable deployment; no business DELETE acceptance'}


def definition(source, parent, invalid_source, snap_source, snap_reference, overlap_source, gap_source, gap_coverage, dangling_source, line_overlap_source, line_self_overlap_source, line_intersection_source, line_self_intersection_source, split_line_source, line_component_source, line_chain_source):
    return {'tasks': [
        {'id': 'load', 'operator': 'load', 'params': {'locator': source}, 'depends_on': []},
        {'id': 'build', 'operator': 'polygonize', 'params': {'input_gdf': {'$ref': 'load'}}, 'depends_on': ['load']},
        *[{'id': 'save_' + port, 'operator': 'save', 'depends_on': ['build'], 'params': {
            'input_df': {'$ref': 'build'} if port == 'faces' else {'$ref': 'build', 'port': 'diagnostics'},
            'target_parent_locator': parent, 'target_name': name, 'mode': 'fail'}}
          for port, name in zip(('faces','diagnostics'), fixture.TARGETS[:2])],
        {'id': 'load_invalid', 'operator': 'load', 'params': {'locator': invalid_source}, 'depends_on': []},
        {'id': 'check', 'operator': 'check_geometry', 'params': {'input_gdf': {'$ref': 'load_invalid'}}, 'depends_on': ['load_invalid']},
        {'id': 'repair', 'operator': 'repair_geometry', 'params': {'input_gdf': {'$ref': 'check'}}, 'depends_on': ['check']},
        {'id': 'recheck', 'operator': 'check_geometry', 'params': {'input_gdf': {'$ref': 'repair'}, 'valid_field': 'after_valid', 'reason_field': 'after_reason'}, 'depends_on': ['repair']},
        {'id': 'save_repaired', 'operator': 'save', 'params': {'input_df': {'$ref': 'recheck'}, 'target_parent_locator': parent, 'target_name': 'repaired.geojson', 'mode': 'fail'}, 'depends_on': ['recheck']},
        *snap_definition(snap_source, snap_reference, parent)['tasks'],
        {'id': 'overlap_input', 'operator': 'load', 'params': {'locator': overlap_source}, 'depends_on': []},
        {'id': 'overlap', 'operator': 'check_polygon_overlap', 'params': {'input_gdf': {'$ref': 'overlap_input'}, 'id_field': 'parcel_id'}, 'depends_on': ['overlap_input']},
        {'id': 'save_overlaps', 'operator': 'save', 'params': {'input_df': {'$ref': 'overlap'}, 'target_parent_locator': parent, 'target_name': 'overlaps.gpkg', 'mode': 'fail'}, 'depends_on': ['overlap']},
        {'id': 'gap_input', 'operator': 'load', 'params': {'locator': gap_source}, 'depends_on': []},
        {'id': 'gap_coverage', 'operator': 'load', 'params': {'locator': gap_coverage}, 'depends_on': []},
        {'id': 'gap', 'operator': 'check_polygon_gap', 'params': {'input_gdf': {'$ref': 'gap_input'}, 'coverage_gdf': {'$ref': 'gap_coverage'}}, 'depends_on': ['gap_input','gap_coverage']},
        {'id': 'save_gaps', 'operator': 'save', 'params': {'input_df': {'$ref': 'gap'}, 'target_parent_locator': parent, 'target_name': 'gaps.gpkg', 'mode': 'fail'}, 'depends_on': ['gap']},
        {'id': 'dangling_input', 'operator': 'load', 'params': {'locator': dangling_source}, 'depends_on': []},
        {'id': 'dangling', 'operator': 'check_dangling_points', 'params': {'input_gdf': {'$ref': 'dangling_input'}, 'id_field': 'route_key'}, 'depends_on': ['dangling_input']},
        {'id': 'save_dangling', 'operator': 'save', 'params': {'input_df': {'$ref': 'dangling'}, 'target_parent_locator': parent, 'target_name': 'dangling.gpkg', 'mode': 'fail'}, 'depends_on': ['dangling']},
        {'id': 'line_overlap_input', 'operator': 'load', 'params': {'locator': line_overlap_source}, 'depends_on': []},
        {'id': 'line_overlap', 'operator': 'check_line_overlap', 'params': {'input_gdf': {'$ref': 'line_overlap_input'}, 'id_field': 'route_key'}, 'depends_on': ['line_overlap_input']},
        {'id': 'save_line_overlaps', 'operator': 'save', 'params': {'input_df': {'$ref': 'line_overlap'}, 'target_parent_locator': parent, 'target_name': 'line-overlaps.gpkg', 'mode': 'fail'}, 'depends_on': ['line_overlap']},
        {'id': 'line_self_overlap_input', 'operator': 'load', 'params': {'locator': line_self_overlap_source}, 'depends_on': []},
        {'id': 'line_self_overlap', 'operator': 'check_line_self_overlap', 'params': {'input_gdf': {'$ref': 'line_self_overlap_input'}, 'id_field': 'route_key'}, 'depends_on': ['line_self_overlap_input']},
        {'id': 'save_line_self_overlaps', 'operator': 'save', 'params': {'input_df': {'$ref': 'line_self_overlap'}, 'target_parent_locator': parent, 'target_name': 'line-self-overlaps.gpkg', 'mode': 'fail'}, 'depends_on': ['line_self_overlap']},
        {'id': 'line_intersection_input', 'operator': 'load', 'params': {'locator': line_intersection_source}, 'depends_on': []},
        {'id': 'line_intersection', 'operator': 'check_line_intersection', 'params': {'input_gdf': {'$ref': 'line_intersection_input'}, 'id_field': 'route_key'}, 'depends_on': ['line_intersection_input']},
        {'id': 'save_line_intersections', 'operator': 'save', 'params': {'input_df': {'$ref': 'line_intersection'}, 'target_parent_locator': parent, 'target_name': 'line-intersections.gpkg', 'mode': 'fail'}, 'depends_on': ['line_intersection']},
        {'id': 'line_self_intersection_input', 'operator': 'load', 'params': {'locator': line_self_intersection_source}, 'depends_on': []},
        {'id': 'line_self_intersection', 'operator': 'check_line_self_intersection', 'params': {'input_gdf': {'$ref': 'line_self_intersection_input'}, 'id_field': 'route_key'}, 'depends_on': ['line_self_intersection_input']},
        {'id': 'save_line_self_intersections', 'operator': 'save', 'params': {'input_df': {'$ref': 'line_self_intersection'}, 'target_parent_locator': parent, 'target_name': 'line-self-intersections.gpkg', 'mode': 'fail'}, 'depends_on': ['line_self_intersection']},
        {'id': 'split_line_input', 'operator': 'load', 'params': {'locator': split_line_source}, 'depends_on': []},
        {'id': 'split_line', 'operator': 'split_line_intersections', 'params': {'input_gdf': {'$ref': 'split_line_input'}, 'id_field': 'route_key', 'scope': 'all'}, 'depends_on': ['split_line_input']},
        {'id': 'save_split_lines', 'operator': 'save', 'params': {'input_df': {'$ref': 'split_line'}, 'target_parent_locator': parent, 'target_name': 'split-lines.gpkg', 'mode': 'fail'}, 'depends_on': ['split_line']},
        {'id': 'line_component_input', 'operator': 'load', 'params': {'locator': line_component_source}, 'depends_on': []},
        {'id': 'line_component', 'operator': 'group_line_components', 'params': {'input_gdf': {'$ref': 'line_component_input'}, 'id_field': 'route_key'}, 'depends_on': ['line_component_input']},
        {'id': 'save_line_components', 'operator': 'save', 'params': {'input_df': {'$ref': 'line_component'}, 'target_parent_locator': parent, 'target_name': 'line-components.gpkg', 'mode': 'fail'}, 'depends_on': ['line_component']},
        {'id':'line_chain_input','operator':'load','params':{'locator':line_chain_source},'depends_on':[]},
        {'id':'line_chain','operator':'merge_line_chains','params':{'input_gdf':{'$ref':'line_chain_input'},'id_field':'route_key','match_fields':['road','grade']},'depends_on':['line_chain_input']},
        {'id':'save_line_chains','operator':'save','params':{'input_df':{'$ref':'line_chain'},'target_parent_locator':parent,'target_name':'line-chains.gpkg','mode':'fail'},'depends_on':['line_chain']},
        {'id':'save_line_chain_members','operator':'save','params':{'input_df':{'$ref':'line_chain','port':'members'},'target_parent_locator':parent,'target_name':'line-chain-members.gpkg','mode':'fail'},'depends_on':['line_chain']},
    ]}


def snap_definition(source, reference, parent, *, failed=False):
    return {'tasks': [
        {'id': 'snap_input', 'operator': 'load', 'params': {'locator': source}, 'depends_on': []},
        {'id': 'snap_reference', 'operator': 'load', 'params': {'locator': reference}, 'depends_on': []},
        {'id': 'snap', 'operator': 'snap_geometry', 'params': {'input_gdf': {'$ref': 'snap_input'},
            'reference_gdf': {'$ref': 'snap_reference'}, 'tolerance': 1.25 if failed else .75},
            'depends_on': ['snap_input', 'snap_reference']},
        {'id': 'save_snapped', 'operator': 'save', 'params': {'input_df': {'$ref': 'snap'},
            'target_parent_locator': parent, 'target_name': 'failed-snap.gpkg' if failed else 'snapped.gpkg',
            'mode': 'fail'}, 'depends_on': ['snap']},
    ]}


def output_sources(port):
    return {'faces':(0,), 'diagnostics':(0,), 'repaired':(1,), 'snapped':(2,3), 'overlaps':(4,), 'gaps':(5,6), 'dangling':(7,), 'line_overlaps':(8,), 'line_self_overlaps':(9,), 'line_intersections':(10,), 'line_self_intersections':(11,), 'split_lines':(12,), 'line_components':(13,), 'line_chains':(14,), 'line_chain_members':(14,)}[port]


def validate_failed_snap(execution):
    metadata = obj(execution.get('metadata'), 'failed metadata')
    result = metadata.get('result')
    if result is not None: result = obj(result, 'failed result')
    if (execution.get('status') != 'failed' or execution.get('outputs') or metadata.get('lineage_facts')
        or (result is not None and result.get('meta_scan_runs'))):
        raise SuiteError('collapsed snap must fail without publication or automatic scan')


def collapsed_snap_diagnostic(repository, env, execution_id, timeout=30, *, task='snap',
        reason='snapping produced invalid geometries; reduce tolerance or revise reference', cause='invalid_snapped_geometry'):
    # Public tenant observations intentionally omit raw Runtime failure text.
    # Match only this execution's fixed cause in the disposable owner's log.
    root = Path(env.get('ADDP_RUNTIME_LOG_ROOT', repository / 'logs/runtime'))
    if not root.is_absolute(): root = repository / root
    prefix = f'[DevExecutor] 工作流引擎调用失败: execution_id={execution_id} err='
    suffix = f'任务 {task} 执行失败: {reason}'
    deadline = time.monotonic() + timeout
    while True:
        for path in (root / 'develop').glob('*/*.jsonl'):
            for line in path.read_text().splitlines():
                try: event = json.loads(line)
                except json.JSONDecodeError: continue
                message = event.get('message', '')
                if (event.get('module_name') == 'develop' and event.get('role') == 'backend'
                    and prefix in message and message.endswith(suffix)):
                    return {'execution_id': execution_id, 'cause': cause, 'source': 'develop_runtime_log'}
        if time.monotonic() >= deadline:
            raise SuiteError('collapsed snap lacks its exact owner diagnostic')
        time.sleep(.2)


def validate_success(execution, sources, targets):
    metadata = obj(execution.get('metadata'), 'execution metadata')
    outputs = obj(execution.get('outputs'), 'stable outputs')
    expected = {'save_' + key: {'resource': {'locator': target, 'type': 'file', 'write_mode': 'create'}} for key, target in targets.items()}
    if outputs != expected:
        raise SuiteError('expected fifteen exact named file outputs')
    facts = obj(metadata.get('lineage_facts'), 'lineage facts')
    expected_inputs = [('input.'+node, source) for node, source in zip(
        ('load', 'load_invalid', 'snap_input', 'snap_reference', 'overlap_input', 'gap_input', 'gap_coverage', 'dangling_input', 'line_overlap_input', 'line_self_overlap_input', 'line_intersection_input', 'line_self_intersection_input', 'split_line_input', 'line_component_input', 'line_chain_input'), sources)]
    actual_inputs = [(entry.get('port'), entry.get('locator')) for entry in array(facts.get('inputs'), 'inputs')]
    if (facts.get('schema_version') != 'addp.lineage-facts/v1'
        or len(sources) != len(fixture.SOURCES) or len(actual_inputs) != len(expected_inputs)
        or any(actual_inputs.count(binding) != 1 for binding in expected_inputs)
        or sorted(entry.get('locator') for entry in array(facts.get('outputs'), 'outputs')) != sorted(targets.values())
        or any(entry.get('write_mode') != 'create' for entry in facts['outputs'])):
        raise SuiteError('topology, repair, snap, overlap, gap, endpoint line overlap self overlap and point intersection resource lineage differs')
    scans = array(obj(metadata.get('result'), 'result').get('meta_scan_runs'), 'automatic scans')
    if (len(scans) != len(fixture.TARGETS) or {scan.get('target_locator') for scan in scans} != set(targets.values())
        or any(scan.get('status') != 'submitted' or not isinstance(scan.get('execution_id'), str) or not scan['execution_id'] for scan in scans)
        or len({scan['execution_id'] for scan in scans}) != len(fixture.TARGETS)):
        raise SuiteError('expected exactly fifteen distinct automatic precise file scans')
    return facts, scans


def physical(repository, env):
    result = subprocess.run([sys.executable, 'business/scripts/online-vector-topology-fixture.py', 'verify'],
        cwd=repository, env=dict(env), text=True, capture_output=True, timeout=150)
    if result.returncode:
        raise SuiteError('independent topology physical verification failed: ' + result.stderr[-1000:])
    return obj(json.loads(result.stdout), 'physical evidence')


def validate_item(item, native, name):
    if name.endswith('.gpkg'):
        return vector.validate_item(item, native, name)
    size = item.get('size_bytes')
    if type(size) is not int or size <= 0 or type(native.get('size_bytes')) is not int or size != native['size_bytes'] or not item.get('scanned_at'):
        raise SuiteError('physical and Meta sizes must be equal positive integers')
    if item.get('item_type') != 'file' or item.get('full_name') != 'outputs/' + name or obj(obj(item.get('attributes'), 'attributes').get('item'), 'item facts').get('format') != 'geojson':
        raise SuiteError('GeoJSON file identity differs')
    return {'physical_size_bytes': size, 'meta_size_bytes': size}


def browser(repository, env, evidence):
    path = Path(env['ADDP_ONLINE_ARTIFACT_DIR']) / 'topology-browser.json'
    path.unlink(missing_ok=True)
    case=fixture.topology_case(env)
    # Playwright prefixes file/project names in grep titles; match the exact test-name suffix.
    pattern=fixture.network_profile(case)['title']+'$' if case in (fixture.NORMALIZE_CASE,fixture.COMPOSITION_CASE) else '同一普通User核对构面、修复、严格吸附、面叠面缝悬点线叠自叠线交自交及打断连通分组链路合并地图及塌缩失败$'
    result = subprocess.run(['npm', 'exec', '--', 'playwright', 'test', 'e2e/online/vector-topology-persistence.spec.js',
        '--config=playwright.online.config.js','--grep',pattern], cwd=repository/'console/frontend',
        env=dict(env, ADDP_ONLINE_TOPOLOGY_EVIDENCE=json.dumps(evidence)), stdout=sys.stderr, stderr=sys.stderr, timeout=240)
    if result.returncode or not path.is_file():
        raise SuiteError('topology Console evidence missing or failed')
    report = obj(json.loads(path.read_text()), 'browser evidence')
    schema=fixture.network_profile(case)['schema']+'-browser/v1' if case in (fixture.NORMALIZE_CASE,fixture.COMPOSITION_CASE) else 'addp.topology-browser/v14'
    if report != {**evidence, 'schema_version': schema, 'result': 'passed'}:
        raise SuiteError('browser report does not bind the exact run, User and outputs')
    return report


def run_scenario(repository, env, client, physical_runner=physical, browser_runner=browser,
                 diagnostic_runner=collapsed_snap_diagnostic):
    if fixture.topology_case(env) in (fixture.NORMALIZE_CASE,fixture.COMPOSITION_CASE):
        return run_normalize_scenario(repository,env,client,physical_runner,browser_runner,diagnostic_runner)
    tenant = positive(env['ADDP_ONLINE_TEST_TENANT_ID'], 'tenant')
    if tenant <= 1:
        raise SuiteError('topology forbids the default Tenant')
    identity = raster.validate_identity(client, tenant, required_permissions=PERMISSIONS)
    timeout = float(env.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS', '600'))
    engine = positive(env['ADDP_ONLINE_TOPOLOGY_NFS_ENGINE_ID'], 'NFS engine')
    sources = []
    for name in fixture.SOURCES:
        scope = f'addp://engine/{engine}/path/{name}?type=file'
        scan = support.wait_for_meta_scan(client, engine, time.monotonic()+timeout, scope={'targets': [scope]})
        item = support.find_fixture_item(client, engine, name, 'topology source')
        before = client.request('GET', f'/api/v1/meta/items/{item["id"]}', (200,)).payload
        sources.append({'name': name, 'id': item['id'], 'locator': scope+f'&item_id={item["id"]}', 'scan_execution_id': scan, 'before': before})
    runtimes = array(client.request('GET', '/api/v1/develop/workflow-engines', (200,)).payload, 'runtimes')
    selected = [value for value in runtimes if value.get('engine_type') == 'geopython_workflow' and value.get('connection_status') == 'online']
    if len(selected) != 1:
        raise SuiteError('expected exactly one online product GeoPython Runtime')
    parent = f'addp://engine/{engine}/path/outputs?type=directory'
    targets = {key: f'addp://engine/{engine}/path/outputs/{name}?type=file' for key, name in zip(fixture.PORTS, fixture.TARGETS)}
    identifier = raster.submit(client, positive(selected[0]['id'], 'Runtime'), definition(sources[0]["locator"], parent, *[source['locator'] for source in sources[1:]]))
    execution = raster.wait_execution(client, 'develop', identifier, timeout)
    facts, scans = validate_success(execution, [source["locator"] for source in sources], targets)
    scan_observations = []
    for scan in scans:
        scanned = raster.wait_execution(client, 'meta', scan['execution_id'], timeout)
        if (type(scanned.get('tenant_id')) is not int or scanned['tenant_id'] != tenant
            or type(scanned.get('triggered_by')) is not int or scanned['triggered_by'] != int(identity['principal_id'])):
            raise SuiteError('automatic scan User or Tenant differs')
        scan_observations.append({'execution_id': scan['execution_id'], 'tenant_id': scanned['tenant_id'],
                                  'triggered_by': scanned['triggered_by'], 'status': scanned['status']})
    monitor = obj(client.request('GET', f'/api/v1/monitor/executions/by-execution-id/{identifier}', (200,)).payload, 'Monitor')
    if monitor.get('status') != 'success' or monitor.get('module') != 'develop' or obj(monitor.get('metadata'), 'metadata').get('lineage_facts') != facts:
        raise SuiteError('Monitor status, owner or lineage differs')
    native = physical_runner(repository, env)
    if (native.get('exact_inventory_verified') is not True or set(native.get('targets', {})) != set(fixture.TARGETS)
        or set(native.get('sources', {})) != set(fixture.SOURCES)):
        raise SuiteError('missing exact physical target inventory')
    outputs = []
    for key, name in zip(fixture.PORTS, fixture.TARGETS):
        item = support.find_fixture_item(client, engine, 'outputs/' + name, 'topology output')
        item = obj(client.request('GET', f'/api/v1/meta/items/{item["id"]}', (200,)).payload, 'output item')
        sizes = validate_item(item, native['targets'][name], name)
        outputs.append({'port': key, 'name': name, 'locator': targets[key], 'item_id': item['id'], 'size_evidence': sizes,
                        **({'fid_column': native['targets'][name]['fid_column']} if key in ('overlaps','gaps','dangling','line_overlaps','line_self_overlaps','line_intersections','line_self_intersections','split_lines','line_components','line_chains','line_chain_members') else {}),
                        **({'layer_name': native['targets'][name]['layer_name'], 'geometry_column': native['targets'][name]['geometry_column']} if name.endswith('.gpkg') else {})})
    # Persist the verified physical facts before any potentially long convergence
    # wait. This checkpoint cannot stand in for the final successful suite report.
    checkpoint = {'schema_version': 'addp.topology-checkpoint/v14', 'suite': SUITE, 'result': 'incomplete',
        'stage': 'before_lineage', 'split_line_scope': 'all', 'run_id': env['ADDP_ONLINE_TEST_RUN_ID'], 'identity': identity,
        'execution_id': identifier, 'sources': [{key: source[key] for key in ('name', 'id', 'locator', 'scan_execution_id')} for source in sources], 'lineage_facts': facts,
        'automatic_scans': scan_observations, 'outputs': outputs, 'stable_outputs': execution['outputs'], 'physical': native, 'monitor_lineage_verified': True, 'monitor': monitor}
    (Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/'topology-before-lineage.json').write_text(json.dumps(checkpoint, indent=2)+'\n')
    for output in outputs:
        output['lineage'] = [raster.wait_lineage(client, sources[index]['id'], output['item_id'], identifier, timeout)
                             for index in output_sources(output['port'])]
    for source in sources:
        if client.request('GET', f'/api/v1/meta/items/{source["id"]}', (200,)).payload != source['before']:
            raise SuiteError('source metadata changed')
    before_scans = scan_ids(client)
    if before_scans != sorted([source['scan_execution_id'] for source in sources] + [scan['execution_id'] for scan in scans]):
        raise SuiteError('unexpected source or automatic scan inventory')
    failed_id = raster.submit(client, positive(selected[0]['id'], 'Runtime'),
        snap_definition(sources[2]['locator'], sources[3]['locator'], parent, failed=True))
    failed = raster.wait_execution(client, 'develop', failed_id, timeout, expected='failed')
    validate_failed_snap(failed)
    diagnostic = diagnostic_runner(repository, env, failed_id)
    failed_monitor = obj(client.request('GET', f'/api/v1/monitor/executions/by-execution-id/{failed_id}', (200,)).payload, 'failed Monitor')
    if failed_monitor.get('status') != 'failed' or failed_monitor.get('module') != 'develop':
        raise SuiteError('collapsed snap Monitor owner or terminal status differs')
    if scan_ids(client) != before_scans or physical_runner(repository, env) != native:
        raise SuiteError('collapsed snap changed scans, sources or saved outputs')
    failure = {'execution_id': failed_id, 'status': 'failed', 'no_publication_or_scan': True,
               'monitor': failed_monitor, 'diagnostic': diagnostic}
    failed_metadata = obj(failed.get('metadata'), 'failed metadata')
    failed_result = failed_metadata.get('result')
    failure['observation'] = {'status': failed['status'], 'outputs': failed.get('outputs'),
        'lineage_facts': failed_metadata.get('lineage_facts'),
        'meta_scan_runs': failed_result.get('meta_scan_runs') if isinstance(failed_result, dict) else None}
    # Preserve the already verified negative case even if a later map fails.
    # This remains an incomplete suite checkpoint, never a successful T4 report.
    (Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/'topology-snap-failure.json').write_text(json.dumps({
        'schema_version': 'addp.topology-snap-failure/v1', 'result': 'incomplete',
        'stage': 'collapsed_snap_validated', 'run_id': env['ADDP_ONLINE_TEST_RUN_ID'],
        'identity': identity, 'failed_snap': failure, 'scan_inventory': before_scans,
        'physical_unchanged': True}, indent=2)+'\n')
    # Browser evidence deliberately contains only stable identities and expected content.
    evidence = {'split_line_scope': 'all', 'run_id': env['ADDP_ONLINE_TEST_RUN_ID'], **identity, 'execution_id': identifier,
                'outputs': [{key: value for key, value in output.items() if key not in ('lineage', 'size_evidence')} for output in outputs],
                'failed_execution_id': failed_id,
                'overlap_expected': fixture.overlap_expected_features(),
                'gap_expected': fixture.gap_expected_features(),
                'dangling_expected': fixture.dangling_expected_features(),
                'line_overlap_expected': fixture.line_overlap_expected_features(),
                'line_self_overlap_expected': fixture.line_self_overlap_expected_features(),
                'line_intersection_expected': fixture.line_intersection_expected_features(),
                'line_self_intersection_expected': fixture.line_self_intersection_expected_features(),
                'split_line_expected': fixture.split_line_expected_features(),
                'line_component_expected': fixture.line_component_expected_features(),
                'line_chain_expected':fixture.line_chain_expected_features(),
                'line_chain_members_expected':fixture.line_chain_expected_features(members=True),
                'sources': [{'id': source['id'], 'before': source['before']} for source in sources]}
    report = browser_runner(repository, env, evidence)
    final_native = physical_runner(repository, env)
    if final_native != native:
        raise SuiteError('browser reads changed source or saved outputs')
    return {'schema_version': 'addp.vector-topology-persistence/v14', 'split_line_scope': 'all', 'suite': SUITE, 'result': 'passed',
        'run_id': env['ADDP_ONLINE_TEST_RUN_ID'], 'identity': identity, 'execution_id': identifier,
        'sources': [{key: source[key] for key in ('name', 'id', 'locator', 'scan_execution_id')} for source in sources], 'source_unchanged': True, 'automatic_scans': scans, 'automatic_scan_observations': scan_observations, 'monitor': monitor,
        'outputs': outputs, 'stable_outputs': execution['outputs'], 'physical': native, 'post_browser_physical': final_native, 'browser': report, 'failed_snap': failure,
        'cleanup_boundary': 'owned NFS export/directory and disposable deployment; no business DELETE acceptance'}


if __name__ == '__main__':
    try:
        env = dict(os.environ)
        report = run_scenario(Path(__file__).resolve().parents[2], env,
            raster.GatewayClient(env['GATEWAY_URL'], env['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'], 30))
        (Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/'topology-persistence.json').write_text(json.dumps(report, indent=2)+'\n')
        print(json.dumps(report, sort_keys=True))
    except Exception as error:
        print('Topology acceptance failed: ' + (str(error) if isinstance(error, SuiteError) else type(error).__name__), file=sys.stderr)
        raise SystemExit(1)

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


DIAGNOSTIC_SPECS = (
    ('field-conflict','batch_buffer','operator_field_conflict','input_gdf','distance'),
    ('number-array','batch_buffer','operator_number_array_invalid','distances',''),
    ('crs-mismatch','distance_to','operator_crs_mismatch','',''),
    ('positive-number','check_line_proximity','operator_positive_number_required','tolerance',''),
)
RINGS_DIAGNOSTIC_SPECS = (
    ('ring-field-conflict','extract_polygon_rings','operator_field_conflict','input_gdf','ring_id'),
    ('ring-geometry-invalid','extract_polygon_rings','operator_geometry_invalid','input_gdf',''),
)


def input_diagnostic_specs(case):
    if case==fixture.DIAGNOSTIC_CASE:return DIAGNOSTIC_SPECS
    if case==fixture.RINGS_DIAGNOSTIC_CASE:return RINGS_DIAGNOSTIC_SPECS
    raise SuiteError('unknown input diagnostic case')


DIAGNOSTIC_PRIVATE = ('PRIVATE_DIAGNOSTIC_NODE','PRIVATE_DIAGNOSTIC_SOURCE_VALUE','Traceback','site-packages','/tmp/')


def diagnostic_definition(key, sources, parent):
    spec=next((value for value in (*DIAGNOSTIC_SPECS,*RINGS_DIAGNOSTIC_SPECS) if value[0]==key),None)
    if spec is None:raise SuiteError('unknown operator diagnostic example')
    node='PRIVATE_DIAGNOSTIC_NODE'
    primary=sources[1] if key=='ring-geometry-invalid' else sources[3] if key=='field-conflict' else sources[0]
    tasks=[{'id':node,'operator':spec[1],'depends_on':['source'], 'params':{'input_gdf':{'$ref':'source'}}},
           {'id':'source','operator':'load','depends_on':[],'params':{'locator':primary}}]
    params=tasks[0]['params']
    if spec[1]=='extract_polygon_rings':params['id_field']='parcel_key'
    elif spec[1]=='batch_buffer':params.update(distances=[True] if key=='number-array' else [1],resolution=1)
    else:
        tasks[0]['depends_on'].append('reference')
        tasks.append({'id':'reference','operator':'load','depends_on':[], 'params':{'locator':sources[2] if key=='crs-mismatch' else sources[1]}})
        params['target_geom' if key=='crs-mismatch' else 'reference_gdf']={'$ref':'reference'}
        if key=='positive-number':params.update(tolerance=0,id_field='route_key',reference_id_field='route_key')
    tasks.append({'id':'save','operator':'save','depends_on':[node],'params':{'input_df':{'$ref':node},
                  'target_parent_locator':parent,'target_name':key+'.gpkg','mode':'fail'}})
    return {'tasks':tasks}


def expected_input_diagnostic(spec, *, node_index=1):
    _,operator,reason,parameter,field=spec
    return {'schema_version':'addp.execution-failure/v3','stage':'execution','reason':reason,
            'operator':{'node_index':node_index,'operator_id':operator,**({'parameter':parameter} if parameter else {}),**({'field':field} if field else {})}}


def validate_input_failure(execution, spec, identifier, *, node_index=1):
    if execution.get('execution_id')!=identifier or execution.get('status')!='failed' or execution.get('outputs'):
        raise SuiteError('operator parameter failure identity/status/output differs')
    details=obj(execution.get('error_details'),'operator error details')
    if (details.get('code')!='develop.workflow.invalid_params' or details.get('diagnostic')!=expected_input_diagnostic(spec, node_index=node_index)
        or type(details.get('diagnostic',{}).get('operator',{}).get('node_index')) is not int
        or set(details)-{'code','category','message','diagnostic'}):
        raise SuiteError('operator parameter failure controlled public diagnostic differs')
    if any(value in json.dumps(details,ensure_ascii=False) for value in DIAGNOSTIC_PRIVATE):
        raise SuiteError('operator public diagnostic leaked private runtime context')
    if spec[1]=='extract_polygon_rings' and any(token in json.dumps(details,ensure_ascii=False) for token in ('parcel_shape','parcel_key','rings-conflict.gpkg','rings-invalid.gpkg','相邻乙','重复丙','地块属性','普通业务几何')):
        raise SuiteError('ring diagnostic leaked source identifiers or values')
    metadata=obj(execution.get('metadata'),'operator failure metadata')
    facts=metadata.get('lineage_facts') or {}
    result=metadata.get('result') or {}
    if facts.get('outputs') or result.get('meta_scan_runs'):
        raise SuiteError('operator parameter failure published facts or automatic scans')
    return details


def run_diagnostic_scenario(repository, env, client, physical_runner, browser_runner):
    case=fixture.topology_case(env);profile=fixture.network_profile(case);specs=input_diagnostic_specs(case)
    source_names=profile['diagnostic_sources'];schema=profile['schema']
    tenant=positive(env['ADDP_ONLINE_TEST_TENANT_ID'],'tenant')
    if tenant<=1:raise SuiteError('operator diagnostics forbid default Tenant')
    identity=raster.validate_identity(client,tenant,required_permissions=PERMISSIONS)
    timeout=float(env.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS','600'));deadline=time.monotonic()+timeout
    def remaining():
        seconds=deadline-time.monotonic()
        if seconds<=0:raise SuiteError('operator diagnostic business budget exhausted')
        return seconds
    engine=positive(env['ADDP_ONLINE_TOPOLOGY_NFS_ENGINE_ID'],'NFS engine')
    sources=[]
    for name in source_names:
        scope=f'addp://engine/{engine}/path/{name}?type=file'
        scan=support.wait_for_meta_scan(client,engine,time.monotonic()+remaining(),scope={'targets':[scope]})
        item=support.find_fixture_item(client,engine,name,'operator diagnostic source')
        before=client.request('GET',f'/api/v1/meta/items/{item["id"]}',(200,)).payload
        sources.append({'name':name,'id':item['id'],'locator':scope+f'&item_id={item["id"]}', 'before':before,'scan_execution_id':scan})
    inventory=scan_ids(client)
    if inventory!=sorted(value['scan_execution_id'] for value in sources):raise SuiteError('operator diagnostic initial scan inventory differs')
    runtimes=[value for value in array(client.request('GET','/api/v1/develop/workflow-engines',(200,)).payload,'runtimes')
              if value.get('engine_type')=='geopython_workflow' and value.get('connection_status')=='online']
    if len(runtimes)!=1:raise SuiteError('operator diagnostic requires one online product Runtime')
    native=physical_runner(repository,env)
    if native.get('exact_inventory_verified') is not True or set(native.get('sources',{}))!=set(source_names) or native.get('targets')!={}:raise SuiteError('operator diagnostic source physical inventory differs')
    for source in sources:
        size=source['before'].get('size_bytes');physical_size=native['sources'][source['name']].get('size_bytes')
        if type(size) is not int or size<=0 or type(physical_size) is not int or physical_size!=size:raise SuiteError('diagnostic source physical and Meta positive sizes differ')
    runs=[];directory=Path(env['ADDP_ONLINE_ARTIFACT_DIR'])
    def checkpoint():
        (directory/'topology-input-diagnostics-checkpoint.json').write_text(json.dumps({'schema_version':schema+'-checkpoint/v1',
            'result':'incomplete','case':case,'run_id':env['ADDP_ONLINE_TEST_RUN_ID'],'identity':identity,'sources':sources,
            'initial_scan_inventory':inventory,'physical':native,'executions':runs},ensure_ascii=False,indent=2)+'\n')
    checkpoint()
    for spec in specs:
        definition=diagnostic_definition(spec[0],[value['locator'] for value in sources],f'addp://engine/{engine}/path/outputs?type=directory')
        identifier=raster.submit(client,positive(runtimes[0]['id'],'Runtime'),definition)
        execution=raster.wait_execution(client,'develop',identifier,remaining(),expected='failed')
        details=validate_input_failure(execution,spec,identifier)
        frozen=obj(obj(execution.get('execution_config'),'diagnostic frozen config').get('content'),'diagnostic frozen content').get('workflow_definition')
        if frozen!=definition:raise SuiteError('diagnostic frozen definition differs')
        if type(execution.get('triggered_by')) is not int or execution['triggered_by']!=int(identity['principal_id']):
            raise SuiteError('operator diagnostic public execution actor differs')
        monitor=obj(client.request('GET',f'/api/v1/monitor/executions/by-execution-id/{identifier}',(200,)).payload,'operator Monitor')
        if monitor.get('execution_id')!=identifier or monitor.get('module')!='develop' or monitor.get('status')!='failed' or monitor.get('error_details')!=details or type(monitor.get('triggered_by')) is not int or monitor['triggered_by']!=int(identity['principal_id']):
            raise SuiteError('operator Monitor differs from public Develop diagnostic')
        graphs=[]
        for source in sources:
            graph=obj(client.request('GET',f'/api/v1/meta/lineage/graph?subject_kind=data_item&item_id={source["id"]}&direction=downstream',(200,)).payload,'failure lineage')
            if graph.get('truncated') is not False or any(edge.get('relation_kind')=='derive' and edge.get('evidence',{}).get('execution_id')==identifier for edge in array(graph.get('edges'),'failure edges')):
                raise SuiteError('operator failure published derive or incomplete graph')
            graphs.append({'source_item_id':source['id'],'graph':graph})
        runs.append({'key':spec[0],'execution_id':identifier,'definition':definition,'expected_diagnostic':expected_input_diagnostic(spec),
                     'execution':execution,'monitor':monitor,'source_lineage':graphs})
        if scan_ids(client)!=inventory or physical_runner(repository,env)!=native:raise SuiteError('operator failure changed scans or files')
        checkpoint()
    if len({value['execution_id'] for value in runs})!=len(specs):raise SuiteError('operator diagnostics require distinct executions for the selected case')
    evidence={'case':case,'run_id':env['ADDP_ONLINE_TEST_RUN_ID'],**identity,'executions':runs}
    remaining();report=browser_runner(repository,env,evidence);remaining()
    final=physical_runner(repository,env)
    if final!=native or scan_ids(client)!=inventory:raise SuiteError('operator diagnostic browser changed physical files or scans')
    for source in sources:
        if client.request('GET',f'/api/v1/meta/items/{source["id"]}',(200,)).payload!=source['before']:raise SuiteError('operator diagnostic source Meta changed')
    return {'schema_version':schema+'/v1','suite':SUITE,'case':case,'result':'passed',
            'run_id':env['ADDP_ONLINE_TEST_RUN_ID'],'identity':identity,'sources':sources,'executions':runs,'physical':native,
            'post_browser_physical':final,'initial_scan_inventory':inventory,'final_scan_inventory':scan_ids(client),'browser':report,
            'source_unchanged':True,'no_publication_or_scan_or_derive':True,
            'cleanup_boundary':'owned disposable deployment; no business DELETE acceptance'}


def rings_definition(sources,parent,failure=None):
    index=0 if failure is None else 1+[key for key,_ in fixture.RINGS_FAILURES].index(failure)
    return {'tasks':[
        {'id':'rings_input','operator':'load','params':{'locator':sources[index]},'depends_on':[]},
        {'id':'rings','operator':'extract_polygon_rings','params':{'input_gdf':{'$ref':'rings_input'},'id_field':'parcel_key'},'depends_on':['rings_input']},
        {'id':'save_result','operator':'save','params':{'input_df':{'$ref':'rings'},'target_parent_locator':parent,
            'target_name':('failed-'+failure+'-' if failure else '')+fixture.RINGS_TARGET,'mode':'fail'},'depends_on':['rings']}]}


def validate_rings_graph(graph,source,target,identifier):
    if graph.get('truncated') is not False:raise SuiteError('rings lineage graph is truncated')
    edges=[e for e in array(graph.get('edges'),'rings edges') if e.get('relation_kind')=='derive' and e.get('status')=='active']
    if len(edges)!=1 or edges[0].get('source',{}).get('item_id')!=source or edges[0].get('target',{}).get('item_id')!=target or edges[0].get('evidence',{}).get('execution_id')!=identifier:
        raise SuiteError('rings require exactly one current execution source derive')


def validate_rings_preview_text(text):
    # Parse the original HTTP text in Python: JS Number cannot prove Int64 values.
    data=obj(json.loads(text).get('data'),'rings raw preview');rows=array(data.get('rows'),'rings rows')
    expected=fixture.rings_features('result')
    if len(rows)!=len(expected):raise SuiteError('rings raw preview count differs')
    for row,wanted in zip(rows,expected):
        for name,value in wanted['properties'].items():
            if row.get(name)!=value or type(row.get(name)) is not type(value):raise SuiteError('rings raw preview attribute value or type differs')


def run_rings_scenario(repository,env,client,physical_runner,browser_runner,diagnostic_runner):
    tenant=positive(env['ADDP_ONLINE_TEST_TENANT_ID'],'tenant')
    if tenant<=1:raise SuiteError('rings forbid default Tenant')
    identity=raster.validate_identity(client,tenant,required_permissions=PERMISSIONS);principal=int(identity['principal_id'])
    deadline=time.monotonic()+float(env.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS','600'))
    def remaining():
        value=deadline-time.monotonic()
        if value<=0:raise SuiteError('rings business budget exhausted')
        return value
    engine=positive(env['ADDP_ONLINE_TOPOLOGY_NFS_ENGINE_ID'],'NFS engine');sources=[]
    for name in fixture.RINGS_SOURCES:
        scope=f'addp://engine/{engine}/path/{name}?type=file'
        scan=support.wait_for_meta_scan(client,engine,time.monotonic()+remaining(),scope={'targets':[scope]})
        item=support.find_fixture_item(client,engine,name,'rings source');before=client.request('GET',f'/api/v1/meta/items/{item["id"]}',(200,)).payload
        sources.append({'id':item['id'],'name':name,'locator':scope+f'&item_id={item["id"]}','scan_execution_id':scan,'before':before})
    initial=scan_ids(client)
    if initial!=sorted(s['scan_execution_id'] for s in sources):raise SuiteError('rings initial scan inventory differs')
    runtimes=[r for r in array(client.request('GET','/api/v1/develop/workflow-engines',(200,)).payload,'runtimes') if r.get('engine_type')=='geopython_workflow' and r.get('connection_status')=='online']
    if len(runtimes)!=1:raise SuiteError('rings require one product Runtime')
    runtime=positive(runtimes[0]['id'],'Runtime');parent=f'addp://engine/{engine}/path/outputs?type=directory'
    target=f'addp://engine/{engine}/path/outputs/{fixture.RINGS_TARGET}?type=file';locators=[s['locator'] for s in sources]
    workflow=rings_definition(locators,parent);identifier=raster.submit(client,runtime,workflow);execution=raster.wait_execution(client,'develop',identifier,remaining())
    def frozen(value,uid,definition):
        if value.get('execution_id')!=uid or type(value.get('triggered_by')) is not int or value['triggered_by']!=principal or obj(obj(value.get('execution_config'),'rings frozen config').get('content'),'rings content').get('workflow_definition')!=definition:raise SuiteError('rings execution identity or frozen DAG differs')
    frozen(execution,identifier,workflow)
    facts,scans=validate_saved_success(execution,[("input.rings_input",locators[0])],{'result':target},1)
    observed=[]
    for scan in scans:
        result=raster.wait_execution(client,'meta',scan['execution_id'],remaining())
        if type(result.get('tenant_id')) is not int or result['tenant_id']!=tenant or type(result.get('triggered_by')) is not int or result['triggered_by']!=principal:raise SuiteError('rings scan public identity differs')
        observed.append(result)
    monitor=obj(client.request('GET',f'/api/v1/monitor/executions/by-execution-id/{identifier}',(200,)).payload,'rings Monitor')
    if monitor.get('execution_id')!=identifier or monitor.get('status')!='success' or monitor.get('module')!='develop' or type(monitor.get('triggered_by')) is not int or monitor['triggered_by']!=principal or obj(monitor.get('metadata'),'rings Monitor metadata').get('lineage_facts')!=facts:raise SuiteError('rings Monitor differs')
    native=physical_runner(repository,env)
    if native.get('exact_inventory_verified') is not True or set(native.get('sources',{}))!=set(fixture.RINGS_SOURCES) or set(native.get('targets',{}))!={fixture.RINGS_TARGET}:raise SuiteError('rings physical inventory differs')
    for source,kind in zip(sources,('source','conflict','invalid')):
        detail=source['before'];rows=fixture.rings_features(kind);children=detail.get('attributes',{}).get('type_info',{}).get('container',{}).get('children',[])
        if type(detail.get('size_bytes')) is not int or detail['size_bytes']<=0 or detail['size_bytes']!=native['sources'][source['name']]['size_bytes'] or len(children)!=1 or type(children[0].get('row_count')) is not int or children[0]['row_count']!=len(rows) or type(children[0].get('column_count')) is not int or children[0]['column_count']!=len(rows[0]['properties'])+2:raise SuiteError('rings source Meta differs')
    item=support.find_fixture_item(client,engine,'outputs/'+fixture.RINGS_TARGET,'rings output');detail=obj(client.request('GET',f'/api/v1/meta/items/{item["id"]}',(200,)).payload,'rings output')
    physical=native['targets'][fixture.RINGS_TARGET];sizes=validate_item(detail,physical,fixture.RINGS_TARGET);child=detail['attributes']['type_info']['container']['children'][0]
    if type(child.get('row_count')) is not int or child['row_count']!=8 or type(child.get('column_count')) is not int or child['column_count']!=13:raise SuiteError('rings Meta rows/columns differ')
    output={'name':fixture.RINGS_TARGET,'port':'result','locator':target,'item_id':item['id'],**physical,'before':detail,'size_evidence':sizes}
    directory=Path(env['ADDP_ONLINE_ARTIFACT_DIR']);path=directory/'topology-rings-checkpoint.json'
    checkpoint={'schema_version':fixture.RINGS_SCHEMA+'-checkpoint/v1','case':fixture.RINGS_CASE,'result':'incomplete','stage':'before_lineage','identity':identity,'run_id':env['ADDP_ONLINE_TEST_RUN_ID'],
        'execution_id':identifier,'definition':workflow,'execution':execution,'sources':sources,'outputs':[output],'physical':native,'lineage_facts':facts,'stable_outputs':execution['outputs'],'automatic_scan_observations':observed,'monitor':monitor,'failures':[]}
    path.write_text(json.dumps(checkpoint,ensure_ascii=False,indent=2)+'\n')
    raster.wait_lineage(client,sources[0]['id'],item['id'],identifier,remaining())
    graph=obj(client.request('GET',f'/api/v1/meta/lineage/graph?subject_kind=data_item&item_id={item["id"]}&direction=upstream',(200,)).payload,'rings graph');validate_rings_graph(graph,sources[0]['id'],item['id'],identifier)
    expected_scans=sorted([*initial,scans[0]['execution_id']])
    if scan_ids(client)!=expected_scans:raise SuiteError('rings scan inventory before failure differs')
    for index,(key,reason) in enumerate(fixture.RINGS_FAILURES,1):
        definition=rings_definition(locators,parent,key);uid=raster.submit(client,runtime,definition);failed=raster.wait_execution(client,'develop',uid,remaining(),expected='failed');frozen(failed,uid,definition);validate_extension_failure(failed,uid,principal)
        diagnostic=diagnostic_runner(repository,env,uid,timeout=min(30,remaining()),task='rings',reason=reason,cause=key)
        if diagnostic!={'execution_id':uid,'cause':key,'source':'develop_runtime_log'}:raise SuiteError('rings failure diagnostic binding differs')
        fm=obj(client.request('GET',f'/api/v1/monitor/executions/by-execution-id/{uid}',(200,)).payload,'rings failed Monitor')
        if fm.get('execution_id')!=uid or fm.get('status')!='failed' or fm.get('module')!='develop' or type(fm.get('triggered_by')) is not int or fm['triggered_by']!=principal:raise SuiteError('rings failed Monitor differs')
        fg=obj(client.request('GET',f'/api/v1/meta/lineage/graph?subject_kind=data_item&item_id={sources[index]["id"]}&direction=downstream',(200,)).payload,'rings failed source graph')
        if fg.get('truncated') is not False or any(e.get('relation_kind')=='derive' and e.get('evidence',{}).get('execution_id')==uid for e in array(fg.get('edges'),'failed edges')):raise SuiteError('rings failed execution published derive')
        checkpoint['failures'].append({'key':key,'execution_id':uid,'definition':definition,'execution':failed,'monitor':fm,'diagnostic':diagnostic,'source_lineage':fg,'no_publication_or_scan':True})
        checkpoint.update(stage='failure_'+key+'_verified',raw_lineage=graph);path.write_text(json.dumps(checkpoint,ensure_ascii=False,indent=2)+'\n')
        if scan_ids(client)!=expected_scans or physical_runner(repository,env)!=native:raise SuiteError('rings failure changed physical data or scans')
    if len({identifier,*[f['execution_id'] for f in checkpoint['failures']]})!=3:raise SuiteError('rings executions must be distinct')
    expected=fixture.rings_features('result')
    # Decimal strings bind large integer expectations without passing through JS Number.
    expected=[{**r,'properties':{**r['properties'],'grade':str(r['properties']['grade']) if r['properties']['grade'] is not None else None}} for r in expected]
    evidence={'case':fixture.RINGS_CASE,'run_id':env['ADDP_ONLINE_TEST_RUN_ID'],**identity,'execution_id':identifier,'sources':sources,'outputs':[output],'expected':{'result':expected},'failures':[{'key':f['key'],'execution_id':f['execution_id']} for f in checkpoint['failures']]}
    remaining();browser_report=browser_runner(repository,env,evidence);remaining();final=physical_runner(repository,env)
    if final!=native or scan_ids(client)!=expected_scans:raise SuiteError('rings browser changed physical data or scans')
    meta_after={}
    for entry in [*sources,output]:
        after=client.request('GET',f'/api/v1/meta/items/{entry.get("item_id",entry.get("id"))}',(200,)).payload
        if after!=entry['before']:raise SuiteError('rings source or output Meta changed')
        meta_after[entry['name']]=after
    remaining()
    return {**checkpoint,'schema_version':fixture.RINGS_SCHEMA+'/v1','result':'passed','stage':'complete','browser':browser_report,'meta_after':meta_after,'post_browser_physical':final,'initial_scan_inventory':initial,'final_scan_inventory':expected_scans,'sources_and_outputs_unchanged':True,'cleanup_boundary':'owned disposable NFS/Runtime/deployment; no business DELETE acceptance'}


def extension_definition(sources,parent,failed=False):
    tasks=[{'id':'load_source','operator':'load','params':{'locator':sources[0]},'depends_on':[]},
        {'id':'load_reference','operator':'load','params':{'locator':sources[2] if failed else sources[1]},'depends_on':[]},
        {'id':'extend','operator':'extend_line_endpoints','params':{'input_gdf':{'$ref':'load_source'},'reference_gdf':{'$ref':'load_reference'},
          'id_field':'route_key','reference_id_field':'ref_key','max_distance':2.},'depends_on':['load_source','load_reference']}]
    for port,name in zip(fixture.EXTENSION_PORTS,fixture.EXTENSION_TARGETS):
        tasks.append({'id':'save_'+port,'operator':'save','params':{'input_df':{'$ref':'extend',**({'port':port} if port!='result' else {})},
            'target_parent_locator':parent,'target_name':('failed-' if failed else '')+name,'mode':'fail'},'depends_on':['extend']})
    return {'tasks':tasks}



def endpoint_definition(sources,parent,failure=None):
    workflow=extension_definition(sources,parent)
    tasks=workflow['tasks'];tasks[0]['params']['locator']=sources[2] if failure else sources[0]
    tasks[1]['params']['locator']=sources[3+[key for key,_ in fixture.ENDPOINT_FAILURES].index(failure)] if failure else sources[1]
    tasks[2].update(id='snap',operator='snap_line_endpoints')
    tasks[2]['params'].pop('max_distance');tasks[2]['params']['tolerance']=1.
    for task,port,name in zip(tasks[3:],fixture.ENDPOINT_PORTS,fixture.ENDPOINT_TARGETS):
        task['params']['input_df']['$ref']='snap';task['depends_on']=['snap']
        task['params']['target_name']=('failed-'+failure+'-' if failure else '')+name
    return workflow

def validate_extension_success(execution,sources,targets):
    stable={'save_'+port:{'resource':{'locator':target,'type':'file','write_mode':'create'}} for port,target in targets.items()}
    if execution.get('status')!='success' or execution.get('outputs')!=stable:raise SuiteError('extension requires exactly two stable create outputs')
    metadata=obj(execution.get('metadata'),'extension metadata');facts=obj(metadata.get('lineage_facts'),'extension facts')
    inputs=array(facts.get('inputs'),'extension inputs');outputs=array(facts.get('outputs'),'extension output facts')
    bindings=[(value.get('port'),value.get('locator')) for value in inputs]
    if (facts.get('schema_version')!='addp.lineage-facts/v1' or len(bindings)!=2 or
        set(bindings)!={('input.load_source',sources[0]),('input.load_reference',sources[1])} or
        len(outputs)!=2 or {o.get('locator') for o in outputs}!=set(targets.values()) or any(o.get('write_mode')!='create' for o in outputs)):
        raise SuiteError('extension dual-source ports or two create facts differ')
    scans=array(obj(metadata.get('result'),'extension result').get('meta_scan_runs'),'extension scans')
    if (len(scans)!=2 or {s.get('target_locator') for s in scans}!=set(targets.values()) or
        any(s.get('status')!='submitted' or not isinstance(s.get('execution_id'),str) or not s['execution_id'] for s in scans) or len({s['execution_id'] for s in scans})!=2):
        raise SuiteError('extension requires exactly two distinct automatic output scans')
    return facts,scans


def validate_extension_graph(graph,sources,target,identifier):
    if graph.get('truncated') is not False:raise SuiteError('extension lineage graph is truncated')
    edges=[e for e in array(graph.get('edges'),'extension graph edges') if e.get('relation_kind')=='derive' and e.get('status')=='active']
    if (len(edges)!=2 or any(e.get('evidence',{}).get('execution_id')!=identifier for e in edges) or
        {(e.get('source',{}).get('item_id'),e.get('target',{}).get('item_id')) for e in edges}!={(s['id'],target) for s in sources[:2]}):
        raise SuiteError('extension requires exactly two current-execution active source edges per output')
    return edges


def validate_extension_failure(execution,identifier,principal):
    metadata=obj(execution.get('metadata'),'extension failure metadata');result=metadata.get('result')
    if (execution.get('execution_id')!=identifier or execution.get('status')!='failed' or execution.get('outputs') or
        type(execution.get('triggered_by')) is not int or execution['triggered_by']!=principal or metadata.get('lineage_facts') or
        isinstance(result,dict) and result.get('meta_scan_runs')):
        raise SuiteError('extension failure must publish neither port nor facts/scans under the same User')


def trim_definition(sources,parent,failure=None):
    spec=next((v for v in fixture.TRIM_FAILURES if v[0]==failure),None)
    if failure is not None and spec is None:raise SuiteError('unknown trim failure')
    a,b=(spec[1:3] if spec else (0,1))
    return {'tasks':[
        {'id':'trim','operator':'trim_line_endpoints','params':{'input_gdf':{'$ref':'load_source'},'reference_gdf':{'$ref':'load_reference'},
            'id_field':'route_key','reference_id_field':'ref_key','max_distance':0 if failure=='limit' else 4.},'depends_on':['load_source','load_reference']},
        {'id':'load_source','operator':'load','params':{'locator':sources[a]},'depends_on':[]},
        {'id':'load_reference','operator':'load','params':{'locator':sources[b]},'depends_on':[]},
        *[{'id':'save_'+port,'operator':'save','params':{'input_df':{'$ref':'trim','port':port},'target_parent_locator':parent,
            'target_name':('failed-'+failure+'-' if failure else '')+name,'mode':'fail'},'depends_on':['trim']}
          for port,name in zip(fixture.EXTENSION_PORTS,fixture.TRIM_TARGETS)]]}


def trim_diagnostic(spec):
    return {'schema_version':'addp.execution-failure/v3','stage':'execution','reason':spec[3],
        'operator':{'node_index':1,'operator_id':'trim_line_endpoints',**({'parameter':spec[4]} if spec[4] else {})}}


def validate_trim_failure(execution,identifier,principal,spec):
    validate_extension_failure(execution,identifier,principal)
    details=obj(execution.get('error_details'),'trim failure error details')
    if spec[3]:
        if (details.get('code')!='develop.workflow.invalid_params' or details.get('diagnostic')!=trim_diagnostic(spec)
            or type(obj(obj(details.get('diagnostic'),'trim diagnostic').get('operator'),'trim operator').get('node_index')) is not int):
            raise SuiteError('trim exact typed diagnostic differs')
    elif details.get('diagnostic') or details.get('code')!='develop.workflow.execution_failed':
        raise SuiteError('trim numerical failure must remain execution failure')
    if set(details)-{'code','category','message','diagnostic'}:raise SuiteError('trim public failure contains unknown fields')
    if any(value in json.dumps(details,ensure_ascii=False) for value in (*DIAGNOSTIC_PRIVATE,'source_shape','reference_shape','route_key','ref_key','trim-source.gpkg','普通业务几何')):
        raise SuiteError('trim public failure leaked private input context')
    return details


def run_trim_scenario(repository,env,client,physical_runner,browser_runner,diagnostic_runner):
    return run_line_edit_scenario(repository,env,client,physical_runner,browser_runner,diagnostic_runner,fixture.TRIM_CASE)


def run_extension_scenario(repository,env,client,physical_runner,browser_runner,diagnostic_runner):
    return run_line_edit_scenario(repository,env,client,physical_runner,browser_runner,diagnostic_runner,fixture.EXTENSION_CASE)


def run_endpoint_scenario(repository,env,client,physical_runner,browser_runner,diagnostic_runner):
    return run_line_edit_scenario(repository,env,client,physical_runner,browser_runner,diagnostic_runner,fixture.ENDPOINT_CASE)


def run_line_edit_scenario(repository,env,client,physical_runner,browser_runner,diagnostic_runner,case):
    trim=case==fixture.TRIM_CASE
    endpoint=case==fixture.ENDPOINT_CASE
    names=fixture.TRIM_SOURCES if trim else fixture.ENDPOINT_SOURCES if endpoint else fixture.EXTENSION_SOURCES
    kinds=fixture.TRIM_KINDS if trim else fixture.ENDPOINT_KINDS if endpoint else ('source','reference','failure_reference')
    ports=fixture.ENDPOINT_PORTS if endpoint else fixture.EXTENSION_PORTS
    targets_names=fixture.TRIM_TARGETS if trim else fixture.ENDPOINT_TARGETS if endpoint else fixture.EXTENSION_TARGETS
    schema=fixture.network_profile(case)['schema'];prefix='endpoint' if endpoint else 'extension'
    if trim:prefix='trim'
    expected=lambda kind:fixture.line_edit_features(case,kind)
    definition=trim_definition if trim else endpoint_definition if endpoint else extension_definition
    tenant=positive(env['ADDP_ONLINE_TEST_TENANT_ID'],'tenant')
    if tenant<=1:raise SuiteError('extension forbids default Tenant')
    identity=raster.validate_identity(client,tenant,required_permissions=PERMISSIONS);principal=int(identity['principal_id'])
    deadline=time.monotonic()+float(env.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS','600'))
    def remaining():
        value=deadline-time.monotonic()
        if value<=0:raise SuiteError('extension business budget exhausted')
        return value
    engine=positive(env['ADDP_ONLINE_TOPOLOGY_NFS_ENGINE_ID'],'NFS engine');sources=[]
    for name in names:
        scope=f'addp://engine/{engine}/path/{name}?type=file'
        scan=support.wait_for_meta_scan(client,engine,time.monotonic()+remaining(),scope={'targets':[scope]})
        item=support.find_fixture_item(client,engine,name,'extension source')
        before=client.request('GET',f'/api/v1/meta/items/{item["id"]}',(200,)).payload
        sources.append({'id':item['id'],'name':name,'locator':scope+f'&item_id={item["id"]}','scan_execution_id':scan,'before':before})
    initial=scan_ids(client)
    if initial!=sorted(s['scan_execution_id'] for s in sources):raise SuiteError('extension initial scan inventory differs')
    runtimes=[r for r in array(client.request('GET','/api/v1/develop/workflow-engines',(200,)).payload,'runtimes') if r.get('engine_type')=='geopython_workflow' and r.get('connection_status')=='online']
    if len(runtimes)!=1:raise SuiteError('extension requires one product Runtime')
    runtime=positive(runtimes[0]['id'],'Runtime');parent=f'addp://engine/{engine}/path/outputs?type=directory'
    targets={port:f'addp://engine/{engine}/path/outputs/{name}?type=file' for port,name in zip(ports,targets_names)}
    workflow=definition([s['locator'] for s in sources],parent)
    identifier=raster.submit(client,runtime,workflow);execution=raster.wait_execution(client,'develop',identifier,remaining())
    if execution.get('execution_id')!=identifier or type(execution.get('triggered_by')) is not int or execution['triggered_by']!=principal:raise SuiteError('extension execution actor differs')
    if obj(obj(execution.get('execution_config'),'extension frozen config').get('content'),'extension content').get('workflow_definition')!=workflow:raise SuiteError('extension frozen DAG differs')
    facts,scans=validate_extension_success(execution,[s['locator'] for s in sources],targets)
    observed=[]
    for scan in scans:
        value=raster.wait_execution(client,'meta',scan['execution_id'],remaining())
        if type(value.get('tenant_id')) is not int or value['tenant_id']!=tenant or type(value.get('triggered_by')) is not int or value['triggered_by']!=principal:raise SuiteError('extension automatic scan public User/Tenant differs')
        observed.append(value)
    monitor=obj(client.request('GET',f'/api/v1/monitor/executions/by-execution-id/{identifier}',(200,)).payload,'extension Monitor')
    if monitor.get('execution_id')!=identifier or monitor.get('module')!='develop' or monitor.get('status')!='success' or type(monitor.get('triggered_by')) is not int or monitor['triggered_by']!=principal or obj(monitor.get('metadata'),'Monitor metadata').get('lineage_facts')!=facts:raise SuiteError('extension Monitor facts differ')
    native=physical_runner(repository,env)
    if native.get('exact_inventory_verified') is not True or set(native.get('sources',{}))!=set(names) or set(native.get('targets',{}))!=set(targets_names):raise SuiteError('extension physical inventory differs')
    for source,kind in zip(sources,kinds):
        rows=expected(kind);count=len(rows);columns=len(rows[0]['properties'])+2
        before=source['before'];size=before.get('size_bytes');children=before.get('attributes',{}).get('type_info',{}).get('container',{}).get('children',[])
        if type(size) is not int or size<=0 or size!=native['sources'][source['name']]['size_bytes'] or len(children)!=1 or type(children[0].get('row_count')) is not int or children[0]['row_count']!=count or type(children[0].get('column_count')) is not int or children[0]['column_count']!=columns:raise SuiteError('extension source Meta size/rows/columns differ')
    outputs=[]
    for port,name in zip(ports,targets_names):
        rows=expected(port);count=len(rows);columns=len(rows[0]['properties'])+2
        item=support.find_fixture_item(client,engine,'outputs/'+name,'extension output');detail=obj(client.request('GET',f'/api/v1/meta/items/{item["id"]}',(200,)).payload,'extension output Meta')
        physical=native['targets'][name];sizes=validate_item(detail,physical,name);child=detail['attributes']['type_info']['container']['children'][0]
        if type(child.get('row_count')) is not int or child['row_count']!=count or type(child.get('column_count')) is not int or child['column_count']!=columns:raise SuiteError('extension output Meta rows/columns differ')
        outputs.append({'name':name,'port':port,'locator':targets[port],'item_id':item['id'],**physical,'before':detail,'size_evidence':sizes})
    directory=Path(env['ADDP_ONLINE_ARTIFACT_DIR'])
    checkpoint={'schema_version':schema+'-checkpoint/v1','case':case,'result':'incomplete','stage':'before_lineage','run_id':env['ADDP_ONLINE_TEST_RUN_ID'],
        'identity':identity,'execution_id':identifier,'definition':workflow,'execution':execution,'sources':sources,'outputs':outputs,'physical':native,'automatic_scans':observed,'monitor':monitor,'stable_outputs':execution['outputs'],'lineage_facts':facts}
    path=directory/('topology-'+prefix+'-checkpoint.json');path.write_text(json.dumps(checkpoint,ensure_ascii=False,indent=2)+'\n')
    graphs={}
    for output in outputs:
        for source in sources[:2]:raster.wait_lineage(client,source['id'],output['item_id'],identifier,remaining())
        graph=obj(client.request('GET',f'/api/v1/meta/lineage/graph?subject_kind=data_item&item_id={output["item_id"]}&direction=upstream',(200,)).payload,'extension original graph')
        validate_extension_graph(graph,sources,output['item_id'],identifier);graphs[output['port']]=graph
    expected_scans=sorted([*initial,*[s['execution_id'] for s in scans]])
    if scan_ids(client)!=expected_scans:raise SuiteError('extension scan inventory differs before failure')
    failures=[]
    specs=[(v[0],fixture.TRIM_LANDING_FAILURE if not v[3] else '') for v in fixture.TRIM_FAILURES] if trim else fixture.ENDPOINT_FAILURES if endpoint else (('unrepresentable_first_intersection',fixture.EXTENSION_FAILURE),)
    checkpoint['failures']=failures
    for key,reason in specs:
        failed_workflow=definition([s['locator'] for s in sources],parent,**({'failure':key} if endpoint or trim else {'failed':True}))
        failed_id=raster.submit(client,runtime,failed_workflow);failed=raster.wait_execution(client,'develop',failed_id,remaining(),expected='failed')
        trim_spec=next((v for v in fixture.TRIM_FAILURES if v[0]==key),None) if trim else None
        if trim:validate_trim_failure(failed,failed_id,principal,trim_spec)
        else:validate_extension_failure(failed,failed_id,principal)
        if obj(obj(failed.get('execution_config'),'failed config').get('content'),'failed content').get('workflow_definition')!=failed_workflow:raise SuiteError('failed frozen DAG differs')
        diagnostic=trim_diagnostic(trim_spec) if trim and trim_spec[3] else diagnostic_runner(repository,env,failed_id,timeout=min(30,remaining()),task='trim' if trim else 'snap' if endpoint else 'extend',reason=reason,cause=key)
        if not (trim and trim_spec[3]) and diagnostic!={'execution_id':failed_id,'cause':key,'source':'develop_runtime_log'}:raise SuiteError('exact failure diagnostic binding differs')
        failed_monitor=obj(client.request('GET',f'/api/v1/monitor/executions/by-execution-id/{failed_id}',(200,)).payload,'failed Monitor')
        if failed_monitor.get('execution_id')!=failed_id or failed_monitor.get('status')!='failed' or failed_monitor.get('module')!='develop' or type(failed_monitor.get('triggered_by')) is not int or failed_monitor['triggered_by']!=principal:raise SuiteError('failure Monitor identity differs')
        failure={'key':key,'execution_id':failed_id,'definition':failed_workflow,'execution':failed,'monitor':failed_monitor,'diagnostic':diagnostic,'no_publication_or_scan':True}
        if endpoint or trim:
            if trim:failure['expected_diagnostic']=trim_diagnostic(trim_spec) if trim_spec[3] else None
            failure['source_lineage']=[]
            for source in ([sources[i] for i in trim_spec[1:3]] if trim else (sources[2],sources[3+[k for k,_ in fixture.ENDPOINT_FAILURES].index(key)])):
                graph=obj(client.request('GET',f'/api/v1/meta/lineage/graph?subject_kind=data_item&item_id={source["id"]}&direction=downstream',(200,)).payload,'failed source graph')
                if graph.get('truncated') is not False or any(e.get('relation_kind')=='derive' and e.get('evidence',{}).get('execution_id')==failed_id for e in array(graph.get('edges'),'failed edges')):raise SuiteError('endpoint failure published derive or incomplete graph')
                failure['source_lineage'].append({'source_item_id':source['id'],'graph':graph})
        failures.append(failure)
        checkpoint.update(stage='failure_'+key+'_verified',raw_lineage=graphs)
        path.write_text(json.dumps(checkpoint,ensure_ascii=False,indent=2)+'\n')
        if scan_ids(client)!=expected_scans or physical_runner(repository,env)!=native:raise SuiteError('failure changed physical files or scans')
    if len({f['execution_id'] for f in failures})!=len(specs):raise SuiteError('failure executions must be distinct')
    checkpoint.update(stage='before_browser',raw_lineage=graphs)
    if not endpoint and not trim:checkpoint['failure']=failures[0]
    path.write_text(json.dumps(checkpoint,ensure_ascii=False,indent=2)+'\n')
    evidence={'case':case,'run_id':env['ADDP_ONLINE_TEST_RUN_ID'],**identity,'execution_id':identifier,'sources':sources,'outputs':outputs,
        'expected':{port:expected(port) for port in ports}}
    if endpoint or trim:evidence['failures']=[{'key':f['key'],'execution_id':f['execution_id'],**({'expected_diagnostic':f['expected_diagnostic'],'error_details':f['execution'].get('error_details')} if trim else {})} for f in failures]
    else:evidence['failed_execution_id']=failed_id
    remaining();browser_report=browser_runner(repository,env,evidence);remaining()
    final=physical_runner(repository,env)
    if final!=native or scan_ids(client)!=expected_scans:raise SuiteError('extension browser changed physical files or scan inventory')
    meta_after={}
    for entry in [*sources,*outputs]:
        item_id=entry.get('item_id',entry.get('id'));after=client.request('GET',f'/api/v1/meta/items/{item_id}',(200,)).payload
        if after!=entry['before']:raise SuiteError('extension source or output Meta changed')
        meta_after[entry['name']]=after
    remaining()
    return {**checkpoint,'schema_version':schema+'/v1','result':'passed','stage':'complete','browser':browser_report,'meta_after':meta_after,'post_browser_physical':final,
        'initial_scan_inventory':initial,'final_scan_inventory':expected_scans,'sources_and_outputs_unchanged':True,'cleanup_boundary':'owned disposable NFS/Runtime/deployment; no business DELETE acceptance'}


def dissolve_definition(sources,parent,variant='grouped',failure=None):
    spec=next((s for s in fixture.DISSOLVE_FAILURES if s[0]==failure),None)
    if failure is not None and spec is None:raise SuiteError('unknown dissolve failure')
    groups=[]if variant=='global'else['zone','rank']
    if failure=='dissolve-groups-type':groups='zone'
    if failure=='dissolve-groups-duplicate':groups=['zone','zone']
    if failure=='dissolve-groups-missing':groups=['PRIVATE_DISSOLVE_MISSING']
    prefix='failed-'+failure if failure else variant
    return {'tasks':[
        {'id':'PRIVATE_DISSOLVE_NODE','operator':'dissolve','depends_on':['load_source'],'params':{'input_gdf':{'$ref':'load_source'},'id_field':'PRIVATE_DISSOLVE_ID'if failure=='dissolve-id-missing'else'parcel_key','group_fields':groups}},
        {'id':'load_source','operator':'load','depends_on':[],'params':{'locator':sources[spec[1]if spec else 0]}},
        *[{'id':'save_'+port,'operator':'save','depends_on':['PRIVATE_DISSOLVE_NODE'],'params':{'input_df':{'$ref':'PRIVATE_DISSOLVE_NODE','port':port},'target_parent_locator':parent,'target_name':prefix+'-'+port+'.gpkg','mode':'fail'}}for port in fixture.DISSOLVE_PORTS]]}


def dissolve_diagnostic(spec):
    return {'schema_version':'addp.execution-failure/v3','stage':'execution','reason':spec[2],
        'operator':{'node_index':1,'operator_id':'dissolve',**({'parameter':spec[3]}if spec[3]else{})}}


def validate_dissolve_failure(execution,identifier,principal,spec):
    if type(execution.get('triggered_by'))is not int or execution['triggered_by']!=principal:raise SuiteError('dissolve failure User differs')
    details=validate_input_failure(execution,(spec[0],'dissolve',spec[2],spec[3],''),identifier)
    if any(t in json.dumps(details,ensure_ascii=False)for t in (*DIAGNOSTIC_PRIVATE,'PRIVATE_DISSOLVE','parcel_key','parcel_shape',*fixture.DISSOLVE_SOURCES)):raise SuiteError('dissolve diagnosis leaked source data')
    return details


def validate_dissolve_graph(graph,source,target,identifier):
    if graph.get('truncated')is not False:raise SuiteError('dissolve graph incomplete')
    edges=[e for e in array(graph.get('edges'),'dissolve edges')if e.get('relation_kind')=='derive'and e.get('status')=='active'and e.get('evidence',{}).get('execution_id')==identifier]
    if len(edges)!=1:raise SuiteError('dissolve needs one current-execution source edge per output')
    edge=edges[0];a=edge.get('source',{}).get('item_id');b=edge.get('target',{}).get('item_id')
    if type(a)is not int or type(b)is not int or(a,b)!=(source,target):raise SuiteError('dissolve source/target/execution binding differs')
    return edges


def run_dissolve_scenario(repository,env,client,physical_runner,browser_runner):
    case=fixture.DISSOLVE_CASE;schema=fixture.DISSOLVE_SCHEMA;names=fixture.DISSOLVE_SOURCES
    tenant=positive(env['ADDP_ONLINE_TEST_TENANT_ID'],'dissolve tenant')
    if tenant<=1:raise SuiteError('dissolve forbids default Tenant')
    identity=raster.validate_identity(client,tenant,required_permissions=PERMISSIONS);principal=positive(identity['principal_id'],'dissolve User')
    deadline=time.monotonic()+float(env.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS','600'))
    def remaining():
        value=deadline-time.monotonic()
        if value<=0:raise SuiteError('dissolve total business budget exhausted')
        return value
    engine=positive(env['ADDP_ONLINE_TOPOLOGY_NFS_ENGINE_ID'],'NFS engine');sources=[];preflight=[]
    for name in names:
        scope=f'addp://engine/{engine}/path/{name}?type=file';scan=support.wait_for_meta_scan(client,engine,time.monotonic()+remaining(),scope={'targets':[scope]})
        item=support.find_fixture_item(client,engine,name,'dissolve source');before=client.request('GET',f'/api/v1/meta/items/{item["id"]}',(200,)).payload
        graph=obj(client.request('GET',f'/api/v1/meta/lineage/graph?subject_kind=data_item&item_id={item["id"]}&direction=downstream',(200,)).payload,'dissolve resource preflight')
        if graph.get('truncated')is not False:raise SuiteError('dissolve requires complete authorized source lineage before execution')
        preflight.append({'source_item_id':item['id'],'graph':graph});sources.append({'id':item['id'],'name':name,'locator':scope+f'&item_id={item["id"]}','scan_execution_id':scan,'before':before})
    initial=scan_ids(client)
    if initial!=sorted(s['scan_execution_id']for s in sources):raise SuiteError('dissolve initial scans differ')
    runtimes=[r for r in array(client.request('GET','/api/v1/develop/workflow-engines',(200,)).payload,'runtimes')if r.get('engine_type')=='geopython_workflow'and r.get('connection_status')=='online']
    if len(runtimes)!=1:raise SuiteError('dissolve requires one product Runtime')
    runtime=positive(runtimes[0]['id'],'Runtime');parent=f'addp://engine/{engine}/path/outputs?type=directory';locators=[s['locator']for s in sources]
    directory=Path(env['ADDP_ONLINE_ARTIFACT_DIR']);path=directory/'topology-dissolve-checkpoint.json'
    checkpoint={'schema_version':schema+'-checkpoint/v1','case':case,'result':'incomplete','run_id':env['ADDP_ONLINE_TEST_RUN_ID'],'identity':identity,'sources':sources,'resource_preflight':preflight,'successes':[],'outputs':[],'failures':[]}
    outputs=checkpoint['outputs'];successes=checkpoint['successes'];all_scans=[];native=None
    def persist(stage):
        checkpoint['stage']=stage;path.write_text(json.dumps(checkpoint,ensure_ascii=False,indent=2)+'\n')
    def unchanged_meta():
        after={}
        for entry in [*sources,*outputs]:
            item_id=entry.get('item_id',entry.get('id'));value=client.request('GET',f'/api/v1/meta/items/{item_id}',(200,)).payload
            if value!=entry['before']:raise SuiteError('dissolve source or result Meta changed')
            after[entry['name']]=value
        return after
    def check_physical(value,expected_targets):
        if value.get('exact_inventory_verified')is not True or set(value.get('sources',{}))!=set(names)or set(value.get('targets',{}))!=set(expected_targets):raise SuiteError('dissolve physical inventory differs')
        if native and(value['sources']!=native['sources']or any(value['targets'].get(n)!=v for n,v in native['targets'].items())):raise SuiteError('dissolve existing physical files changed')
        for source,kind in zip(sources,fixture.DISSOLVE_KINDS):
            rows=fixture.dissolve_features(kind);detail=source['before'];size=detail.get('size_bytes');children=detail.get('attributes',{}).get('type_info',{}).get('container',{}).get('children',[])
            if type(size)is not int or size<=0 or size!=value['sources'][source['name']]['size_bytes']or len(children)!=1 or type(children[0].get('row_count'))is not int or children[0]['row_count']!=len(rows)or type(children[0].get('column_count'))is not int or children[0]['column_count']!=len(rows[0]['properties'])+2:raise SuiteError('dissolve source Meta rows/columns/size differ')
    for variant in ('grouped','global'):
        workflow=dissolve_definition(locators,parent,variant);identifier=raster.submit(client,runtime,workflow);execution=raster.wait_execution(client,'develop',identifier,remaining())
        if execution.get('execution_id')!=identifier or type(execution.get('triggered_by'))is not int or execution['triggered_by']!=principal:raise SuiteError('dissolve execution User differs')
        if obj(obj(execution.get('execution_config'),'dissolve config').get('content'),'dissolve content').get('workflow_definition')!=workflow:raise SuiteError('dissolve frozen workflow differs')
        targets={port:parent.removesuffix('?type=directory')+'/'+variant+'-'+port+'.gpkg?type=file'for port in fixture.DISSOLVE_PORTS}
        facts,scans=validate_saved_success(execution,[('input.load_source',locators[0])],targets,2);observed=[]
        for scan in scans:
            value=raster.wait_execution(client,'meta',scan['execution_id'],remaining())
            if type(value.get('tenant_id'))is not int or value['tenant_id']!=tenant or type(value.get('triggered_by'))is not int or value['triggered_by']!=principal:raise SuiteError('dissolve automatic scan User/Tenant differs')
            observed.append(value)
        all_scans.extend(s['execution_id']for s in scans)
        monitor=obj(client.request('GET',f'/api/v1/monitor/executions/by-execution-id/{identifier}',(200,)).payload,'dissolve Monitor')
        if monitor.get('execution_id')!=identifier or monitor.get('module')!='develop'or monitor.get('status')!='success'or type(monitor.get('triggered_by'))is not int or monitor['triggered_by']!=principal or obj(monitor.get('metadata'),'dissolve Monitor metadata').get('lineage_facts')!=facts:raise SuiteError('dissolve Monitor/facts differ')
        value=physical_runner(repository,env);check_physical(value,fixture.DISSOLVE_TARGETS[:2]if variant=='grouped'else fixture.DISSOLVE_TARGETS);native=value
        record={'variant':variant,'execution_id':identifier,'definition':workflow,'execution':execution,'stable_outputs':execution['outputs'],'lineage_facts':facts,'automatic_scans':observed,'monitor':monitor,'outputs':[]};successes.append(record)
        for port,target in targets.items():
            name=variant+'-'+port+'.gpkg';item=support.find_fixture_item(client,engine,'outputs/'+name,'dissolve result');detail=obj(client.request('GET',f'/api/v1/meta/items/{item["id"]}',(200,)).payload,'dissolve output Meta');physical=native['targets'][name];sizes=validate_item(detail,physical,name)
            child=detail['attributes']['type_info']['container']['children'][0];expected=fixture.dissolve_features(variant+'-'+port)
            if type(child.get('row_count'))is not int or child['row_count']!=len(expected)or type(child.get('column_count'))is not int or child['column_count']!=len(expected[0]['properties'])+2:raise SuiteError('dissolve result Meta rows/columns differ')
            output={'name':name,'port':port,'variant':variant,'execution_id':identifier,'locator':target,'item_id':item['id'],**physical,'before':detail,'size_evidence':sizes};outputs.append(output);record['outputs'].append(output);persist('before_lineage_'+name)
            raster.wait_lineage(client,sources[0]['id'],output['item_id'],identifier,remaining());graph=obj(client.request('GET',f'/api/v1/meta/lineage/graph?subject_kind=data_item&item_id={output["item_id"]}&direction=upstream',(200,)).payload,'dissolve original graph')
            validate_dissolve_graph(graph,sources[0]['id'],output['item_id'],identifier);output['raw_lineage']=graph
        unchanged_meta()
        if scan_ids(client)!=sorted([*initial,*all_scans])or len(set(all_scans))!=len(all_scans):raise SuiteError('dissolve automatic scan inventory differs')
        persist('success_'+variant+'_verified')
    checkpoint['physical']=native;expected_scans=sorted([*initial,*all_scans])
    for spec in fixture.DISSOLVE_FAILURES:
        key=spec[0];definition=dissolve_definition(locators,parent,failure=key);identifier=raster.submit(client,runtime,definition);execution=raster.wait_execution(client,'develop',identifier,remaining(),expected='failed');validate_dissolve_failure(execution,identifier,principal,spec)
        if obj(obj(execution.get('execution_config'),'dissolve failed config').get('content'),'dissolve failed content').get('workflow_definition')!=definition:raise SuiteError('dissolve failed frozen workflow differs')
        monitor=obj(client.request('GET',f'/api/v1/monitor/executions/by-execution-id/{identifier}',(200,)).payload,'dissolve failed Monitor');validate_dissolve_failure(monitor,identifier,principal,spec)
        if monitor.get('module')!='develop'or monitor.get('error_details')!=execution.get('error_details'):raise SuiteError('dissolve failed Monitor differs')
        graph=obj(client.request('GET',f'/api/v1/meta/lineage/graph?subject_kind=data_item&item_id={sources[spec[1]]["id"]}&direction=downstream',(200,)).payload,'dissolve failed source graph')
        if graph.get('truncated')is not False or any(e.get('relation_kind')=='derive'and e.get('evidence',{}).get('execution_id')==identifier for e in array(graph.get('edges'),'dissolve failed edges')):raise SuiteError('dissolve failure published derive or incomplete graph')
        if scan_ids(client)!=expected_scans or physical_runner(repository,env)!=native:raise SuiteError('dissolve failure changed physical files or scans')
        checkpoint['failures'].append({'key':key,'execution_id':identifier,'definition':definition,'execution':execution,'monitor':monitor,'expected_diagnostic':dissolve_diagnostic(spec),'source_lineage':graph,'meta_after':unchanged_meta()});persist('failure_'+key+'_verified')
    if len({r['execution_id']for r in [*successes,*checkpoint['failures']]})!=10:raise SuiteError('dissolve requires ten distinct executions')
    persist('before_browser')
    evidence={'case':case,'run_id':env['ADDP_ONLINE_TEST_RUN_ID'],**identity,'sources':sources,'successes':successes,'outputs':outputs,'failures':checkpoint['failures']}
    remaining();browser_report=browser_runner(repository,env,evidence);remaining();final=physical_runner(repository,env)
    if final!=native or scan_ids(client)!=expected_scans:raise SuiteError('dissolve browser changed physical files or scans')
    meta_after=unchanged_meta();remaining()
    return {**checkpoint,'schema_version':schema+'/v1','result':'passed','stage':'complete','browser':browser_report,'meta_after':meta_after,'post_browser_physical':final,'initial_scan_inventory':initial,'final_scan_inventory':expected_scans,'sources_and_outputs_unchanged':True,'cleanup_boundary':'owned disposable deployment; not business DELETE acceptance'}


def difference_definition(sources,parent,failure=None):
    spec=next((v for v in fixture.DIFFERENCE_FAILURES if v[0]==failure),None)
    if failure is not None and spec is None:raise SuiteError('unknown difference failure')
    a,b=spec[1:3]if spec else(0,1)
    tasks = [
        {'id':'PRIVATE_DIFFERENCE_NODE','operator':'difference','depends_on':['load_a','load_b'],'params':{'input_gdf':{'$ref':'load_a'},'gdf_b':{'$ref':'load_b'},'id_field':'PRIVATE_DIFFERENCE_MISSING'if failure=='difference-id'else'a_key'}},
        {'id':'load_a','operator':'load','depends_on':[],'params':{'locator':sources[a]}},
        {'id':'load_b','operator':'load','depends_on':[],'params':{'locator':sources[b]}},
    ]
    if spec is None:
        tasks.append({'id':'save_result','operator':'save','depends_on':['PRIVATE_DIFFERENCE_NODE'],
                      'params':{'input_df':{'$ref':'PRIVATE_DIFFERENCE_NODE'},'target_parent_locator':parent,
                                'target_name':fixture.DIFFERENCE_TARGET,'mode':'fail'}})
    return {'tasks': tasks}


def difference_diagnostic(spec):
    return {'schema_version':'addp.execution-failure/v3','stage':'execution','reason':spec[3],
        'operator':{'node_index':1,'operator_id':'difference',**({'parameter':spec[4]}if spec[4]else{})}}


def validate_difference_failure(execution,identifier,principal,spec):
    if type(execution.get('triggered_by'))is not int or execution['triggered_by']!=principal:raise SuiteError('difference failure User differs')
    details=validate_input_failure(execution,(spec[0],'difference',spec[3],spec[4],''),identifier)
    if any(t in json.dumps(details,ensure_ascii=False)for t in(*DIAGNOSTIC_PRIVATE,'PRIVATE_DIFFERENCE','a_key','a_shape','b_shape',*fixture.DIFFERENCE_SOURCES)):raise SuiteError('difference diagnosis leaked private input')
    return details


def adjacency_definition(sources,parent,failure=None):
    spec=next((s for s in fixture.ADJACENCY_FAILURES if s[0]==failure),None)
    if failure is not None and spec is None:raise SuiteError('unknown adjacency failure')
    tasks=[{'id':'PRIVATE_ADJACENCY_NODE','operator':'check_polygon_adjacency','depends_on':['load_source'],
            'params':{'input_gdf':{'$ref':'load_source'},'id_field':'parcel_key'}},
           {'id':'load_source','operator':'load','depends_on':[],'params':{'locator':sources[spec[1] if spec else 0]}}]
    if failure is None:tasks.append({'id':'save_result','operator':'save','depends_on':['PRIVATE_ADJACENCY_NODE'],'params':{'input_df':{'$ref':'PRIVATE_ADJACENCY_NODE'},'target_parent_locator':parent,'target_name':fixture.ADJACENCY_TARGET,'mode':'fail'}})
    return {'tasks':tasks}


def adjacency_diagnostic(spec):
    return {'schema_version':'addp.execution-failure/v3','stage':'execution','reason':spec[3],
        'operator':{'node_index':1,'operator_id':'check_polygon_adjacency',**({'parameter':spec[4]}if spec[4]else{})}}


def validate_adjacency_failure(execution,identifier,principal,spec):
    if type(execution.get('triggered_by')) is not int or execution['triggered_by']!=principal:raise SuiteError('adjacency failure User differs')
    details=validate_input_failure(execution,(spec[0],'check_polygon_adjacency',spec[3],spec[4],''),identifier)
    if any(t in json.dumps(details,ensure_ascii=False)for t in ('PRIVATE_ADJACENCY','parcel_key','source_shape',*fixture.ADJACENCY_SOURCES)):raise SuiteError('adjacency public diagnosis leaked private input')
    return details


def mixed_overlay_profile(case):
    physical=fixture.mixed_fixture_profile(case)
    if case==fixture.ADJACENCY_CASE:return {**physical,'schema':fixture.ADJACENCY_SCHEMA,'definition':adjacency_definition,'diagnostic':adjacency_diagnostic,'failure_validator':validate_adjacency_failure,'failures':fixture.ADJACENCY_FAILURES,'prefix':'adjacency','checkpoint':'topology-adjacency-checkpoint.json','input_nodes':('load_source',)}
    if case==fixture.OVERLAY_CASE:return {**physical,'schema':fixture.OVERLAY_SCHEMA,'definition':overlay_definition,'diagnostic':overlay_diagnostic,'failure_validator':validate_overlay_failure,'failures':fixture.OVERLAY_FAILURES,'prefix':'overlay','checkpoint':'topology-overlay-checkpoint.json'}
    if case==fixture.DIFFERENCE_CASE:return {**physical,'schema':fixture.DIFFERENCE_SCHEMA,'definition':difference_definition,'diagnostic':difference_diagnostic,'failure_validator':validate_difference_failure,'failures':fixture.DIFFERENCE_FAILURES,'prefix':'difference','checkpoint':'topology-difference-checkpoint.json'}
    raise SuiteError('unknown mixed overlay owner case')


def run_difference_scenario(repository,env,client,physical_runner,browser_runner):
    return run_prepared_file_scenario(repository,env,client,physical_runner,browser_runner,fixture.DIFFERENCE_CASE)


def run_adjacency_scenario(repository,env,client,physical_runner,browser_runner):
    return run_prepared_file_scenario(repository,env,client,physical_runner,browser_runner,fixture.ADJACENCY_CASE)


def run_prepared_file_scenario(repository,env,client,physical_runner,browser_runner,case):
    publication=_FilePublication(env,client,case)
    try:
        result=run_mixed_overlay_scenario(repository,env,client,physical_runner,browser_runner,case,publication)
    except Exception as error:
        publication.record(dict(stage='business_failed',error=publication.registration.metadata_error_evidence(error)))
        raise
    finally:
        publication.cleanup()
    publication.evidence['result']='passed'
    publication.write()
    result['publication']=publication.evidence
    return result


def overlay_definition(sources,parent,failure=None):
    spec=next((s for s in fixture.OVERLAY_FAILURES if s[0]==failure),None)
    if failure is not None and spec is None:raise SuiteError('unknown intersection failure')
    a,b=spec[1:3] if spec else (0,1)
    return {'tasks':[
        {'id':'PRIVATE_OVERLAY_NODE','operator':'intersection','depends_on':['load_a','load_b'],
         'params':{'input_gdf':{'$ref':'load_a'},'gdf_b':{'$ref':'load_b'},
                   'id_field':'PRIVATE_OVERLAY_MISSING_A' if failure=='a-id' else 'a_key',
                   'reference_id_field':'PRIVATE_OVERLAY_MISSING_B' if failure=='b-id' else 'b_key'}},
        {'id':'load_a','operator':'load','depends_on':[],'params':{'locator':sources[a]}},
        {'id':'load_b','operator':'load','depends_on':[],'params':{'locator':sources[b]}},
        {'id':'save_result','operator':'save','depends_on':['PRIVATE_OVERLAY_NODE'],
         'params':{'input_df':{'$ref':'PRIVATE_OVERLAY_NODE'},'target_parent_locator':parent,
                   'target_name':('failed-'+failure+'-' if failure else '')+fixture.OVERLAY_TARGET,'mode':'fail'}}]}


def overlay_diagnostic(spec):
    return {'schema_version':'addp.execution-failure/v3','stage':'execution','reason':spec[3],
            'operator':{'node_index':1,'operator_id':'intersection',**({'parameter':spec[4]} if spec[4] else {})}}


def validate_overlay_failure(execution,identifier,principal,spec):
    if type(execution.get('triggered_by')) is not int or execution['triggered_by']!=principal:
        raise SuiteError('intersection failure User differs')
    details=validate_input_failure(execution,(spec[0],'intersection',spec[3],spec[4],''),identifier)
    if any(token in json.dumps(details,ensure_ascii=False) for token in (*DIAGNOSTIC_PRIVATE,'PRIVATE_OVERLAY','a_key','b_key','a_shape','b_shape','a_note',*fixture.OVERLAY_SOURCES)):
        raise SuiteError('intersection public diagnosis leaked private input')
    return details


def vertices_definition(sources,parent,failure=None):
    if failure is not None:
        index=3+[v[0] for v in fixture.VERTICES_FAILURES].index(failure)
        return {'tasks':[{'id':'vertices','operator':'extract_vertices','params':{'input_gdf':{'$ref':'load_source'},'id_field':'feature_key'},'depends_on':['load_source']},
          {'id':'load_source','operator':'load','params':{'locator':sources[index]},'depends_on':[]},
          {'id':'save_result','operator':'save','params':{'input_df':{'$ref':'vertices'},'target_parent_locator':parent,'target_name':'failed-'+failure+'.gpkg','mode':'fail'},'depends_on':['vertices']}]}
    tasks=[]
    for index,(family,name) in enumerate(zip(fixture.VERTICES_FAMILIES,fixture.VERTICES_TARGETS)):
        tasks.extend([{'id':'load_'+family,'operator':'load','params':{'locator':sources[index]},'depends_on':[]},
          {'id':'vertices_'+family,'operator':'extract_vertices','params':{'input_gdf':{'$ref':'load_'+family},'id_field':'feature_key'},'depends_on':['load_'+family]},
          {'id':'save_'+family,'operator':'save','params':{'input_df':{'$ref':'vertices_'+family},'target_parent_locator':parent,'target_name':name,'mode':'fail'},'depends_on':['vertices_'+family]}])
    return {'tasks':tasks}


def vertices_diagnostic(spec):
    return {'schema_version':'addp.execution-failure/v3','stage':'execution','reason':spec[1],
            'operator':{'node_index':1,'operator_id':'extract_vertices',**({} if spec[0]=='crs' else {'parameter':'input_gdf'}),**({'field':spec[2]} if spec[2] else {})}}


def validate_vertices_failure(execution,identifier,principal,spec):
    if type(execution.get('triggered_by')) is not int or execution['triggered_by']!=principal:raise SuiteError('vertex failure User differs')
    details=validate_input_failure(execution,(spec[0],'extract_vertices',spec[1],'' if spec[0]=='crs' else 'input_gdf',spec[2]),identifier)
    if any(word in json.dumps(details,ensure_ascii=False) for word in (*DIAGNOSTIC_PRIVATE,'source_shape','feature_key','完整属性',*fixture.VERTICES_SOURCES)):raise SuiteError('vertex diagnostic leaked private input')
    return details


def validate_vertices_graph(graph,sources,target,identifier):
    if graph.get('permission_filtered') is not False:raise SuiteError('vertex lineage is permission filtered or lacks visibility evidence')
    edges=validate_nodes_graph(graph,sources,target,identifier)
    active=[e for e in graph['edges'] if e.get('relation_kind')=='derive' and e.get('status')=='active']
    if len(active)!=1 or any(type(e.get(side,{}).get('item_id')) is not int for e in edges for side in ('source','target')):raise SuiteError('vertex lineage has foreign edges or invalid endpoint identities')
    return edges


def vertices_clipboard(failure,locale):
    messages=json.loads((Path(__file__).resolve().parents[2]/'common-frontend/basic/src/i18n'/f'{locale}.json').read_text())['common']['executionFailure']
    diagnostic=failure['expected_diagnostic'];operator=diagnostic['operator'];field=operator.get('field','');reason=diagnostic['reason']
    values=[messages['vertices']['reasons'][reason].format(field=field),messages['stages']['execution'],messages['operatorNames']['extract_vertices'],
            messages['operatorNodeIndex'].format(index=1),messages['operatorParameters']['input_gdf'] if operator.get('parameter') else '',field,
            messages['vertices']['actions'][reason].format(field=field),'develop.workflow.invalid_params',failure['execution_id']]
    return '\n'.join(value for value in values if value)


def run_vertices_scenario(repository,env,client,physical_runner,browser_runner):
    return run_point_outputs_scenario(repository,env,client,physical_runner,browser_runner,fixture.VERTICES_CASE)


def nodes_definition(sources,parent,failure=None):
    index=0 if failure is None else 1+[s[0] for s in fixture.NODES_FAILURES].index(failure)
    return {'tasks':[
        {'id':'nodes','operator':'extract_line_nodes','params':{'input_gdf':{'$ref':'load_source'},'id_field':'route_key'},'depends_on':['load_source']},
        {'id':'load_source','operator':'load','params':{'locator':sources[index]},'depends_on':[]},
        *[{'id':'save_'+port,'operator':'save','params':{'input_df':{'$ref':'nodes','port':port},'target_parent_locator':parent,
            'target_name':('failed-'+failure+'-' if failure else '')+name,'mode':'fail'},'depends_on':['nodes']}
          for port,name in zip(fixture.NODES_PORTS,fixture.NODES_TARGETS)]]}


def nodes_diagnostic(spec):
    return {'schema_version':'addp.execution-failure/v3','stage':'execution','reason':spec[1],
            'operator':{'node_index':1,'operator_id':'extract_line_nodes',
                        **({} if spec[0]=='crs' else {'parameter':'input_gdf'}),**({'field':spec[2]} if spec[2] else {})}}


def validate_nodes_failure(execution,identifier,principal,spec):
    validate_extension_failure(execution,identifier,principal)
    details=obj(execution.get('error_details'),'nodes error details')
    if (details.get('code')!='develop.workflow.invalid_params' or details.get('diagnostic')!=nodes_diagnostic(spec)
        or type(details['diagnostic']['operator']['node_index']) is not int or set(details)-{'code','category','message','diagnostic'}):
        raise SuiteError('nodes exact controlled diagnostic differs')
    if any(word in json.dumps(details,ensure_ascii=False) for word in (*DIAGNOSTIC_PRIVATE,'source_shape','route_key','nodes-source.gpkg','节点业务几何')):
        raise SuiteError('nodes diagnostic leaked private input')
    return details


def validate_nodes_graph(graph,sources,target,identifier):
    if graph.get('truncated') is not False:raise SuiteError('nodes graph incomplete')
    edges=[e for e in array(graph.get('edges'),'nodes graph edges') if e.get('relation_kind')=='derive' and e.get('status')=='active' and e.get('evidence',{}).get('execution_id')==identifier]
    if len(edges)!=1 or {(e.get('source',{}).get('item_id'),e.get('target',{}).get('item_id')) for e in edges}!={(sources[0]['id'],target)}:
        raise SuiteError('nodes requires one precise current-execution source edge per output')
    return edges


def run_overlay_scenario(repository,env,client,physical_runner,browser_runner):
    return run_mixed_overlay_scenario(repository,env,client,physical_runner,browser_runner,fixture.OVERLAY_CASE)


class _FilePublication:
    """One formal preparation route for the approved single-file topology cases."""
    def __init__(self, env, consumer, case):
        if case not in fixture.FILE_PUBLICATION_CASES or fixture.topology_case(env)!=case:
            raise SuiteError('formal file publication case binding differs')
        profile=mixed_overlay_profile(case)
        registration = importlib.import_module('scripts.test.online-engine-registration')
        token = env.get('ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN')
        if not token:
            raise SuiteError('topology file case requires its separate source initializer and approver')
        self.path = Path(env['ADDP_ONLINE_ARTIFACT_DIR']) / ('topology-'+profile['prefix']+'-publication.json')
        self.evidence = {'schema_version':profile['schema']+'-publication/v1', 'case':case, 'result':'incomplete',
                         'run_id':env['ADDP_ONLINE_TEST_RUN_ID'], 'stages':[], 'pending_grants':[]}
        self.consumer = consumer
        self.issuer = raster.GatewayClient(env['GATEWAY_URL'], token, 30)
        self.tenant = positive(env['ADDP_ONLINE_TEST_TENANT_ID'], 'difference Tenant')
        self.registration = registration
        self.grants = registration.ExactFileReadGrants(self.issuer, consumer, self.tenant,
                        scan_preparation=True, approval_preparation=True, on_change=self.pending)
        self.target = None

    def record(self, value):
        self.evidence['stages'].append(value)
        self.write()

    def write(self):
        self.path.write_text(json.dumps(self.evidence, ensure_ascii=False, indent=2)+'\n')

    def pending(self, rows):
        self.evidence['pending_grants'] = list(rows)
        self.write()

    def source(self, engine, name, scope, remaining):
        basic = support.wait_for_meta_scan(self.issuer, engine, time.monotonic()+remaining(), depth='basic', scope={'targets':[scope]})
        self.record(dict(stage='source_basic_identified', target=scope, execution_id=basic))
        receipt = self.grants.grant(engine, name)
        self.record(dict(stage='source_read_granted', target=scope, grant=receipt))
        # Deep inspection provides the separate physical/schema oracle only.
        deep = support.wait_for_meta_scan(self.issuer, engine, time.monotonic()+remaining(), depth='deep', scope={'targets':[scope]})
        self.record(dict(stage='source_deep_inspected', target=scope, execution_id=deep))
        return deep

    def before_submit(self, identity, engine, runtime, sources, definition, *, failure=None):
        # Current public identity contains no bearer secret. Preserve the full
        # requested range before POST, even when no execution UUID is returned.
        context=self.consumer.request('GET','/api/v1/system/auth/context',(200,)).payload
        self.record(dict(stage='before_business_submit',identity=identity,auth_context=context,
                         engine_id=engine,runtime_id=runtime,sources=sources,definition=definition,
                         failure=failure,execution_id=None,scope_state='not_yet_observed'))

    def approve(self, identifier, principal, engine, name):
        self.evidence['execution_id'] = identifier
        self.write()
        target = self.grants.target(engine, name, 'nfs')
        return self.registration.approve_geopackage_save(self.issuer, identifier, self.tenant, principal,
                                                        target, on_evidence=self.record)

    def produced(self, engine, name, identifier, principal, initial, remaining):
        target = f'addp://engine/{engine}/path/{name}?type=file'
        while True:
            remaining()
            added = sorted(set(scan_ids(self.consumer))-set(initial))
            if len(added)>1:
                raise SuiteError('topology file case requires exactly one automatic production scan')
            if added:
                break
            time.sleep(min(0.2, remaining()))
        scan = raster.wait_execution(self.consumer, 'meta', added[0], remaining())
        config = obj(scan.get('scan_context'), 'production scan context')
        if (scan.get('source')!='develop' or scan.get('parent_execution_id')!=identifier
            or type(scan.get('tenant_id')) is not int or scan['tenant_id']!=self.tenant
            or type(scan.get('triggered_by')) is not int or scan['triggered_by']!=principal
            or config.get('scan_depth')!='basic' or type(config.get('engine_id')) is not int or config['engine_id']!=engine):
            raise SuiteError('topology file automatic basic scan binding differs')
        self.record(dict(stage='result_basic_identified', execution=scan))
        while True:
            remaining()
            receipt = self.grants.production_grant(engine, name, identifier)
            if receipt is not None:
                break
            time.sleep(min(0.2, remaining()))
        self.target = self.grants.target(engine, name, 'nfs')
        self.record(dict(stage='result_initial_read_granted', grant=receipt))
        deep = support.wait_for_meta_scan(self.issuer, engine, time.monotonic()+remaining(), depth='deep', scope={'targets':[target]})
        self.record(dict(stage='result_deep_inspected', execution_id=deep))
        return [dict(execution_id=added[0], target_locator=target, status='submitted')], [scan], deep

    def check_preparation_scans(self):
        expected=sorted(row['execution_id'] for row in self.evidence['stages']
                        if row['stage'] in {'source_basic_identified','source_deep_inspected','result_deep_inspected'})
        if scan_ids(self.issuer)!=expected:
            raise SuiteError('topology file initializer scan inventory differs')
        self.evidence['preparation_scan_inventory']=expected
        self.write()

    def cleanup(self):
        # Persist observations before revocation, including when business validation failed.
        self.write()
        sources = [row for row in self.grants.pending if row['target']!=self.target]
        errors = []
        try:
            self.grants.cleanup(rows=sources)
            self.record(dict(stage='source_grants_revoked'))
            if self.target:
                self.grants.observe(self.target, True, phase='result_independent_of_source_revocation')
                self.record(dict(stage='materialized_result_still_readable'))
        except Exception as error:
            errors.append(self.registration.metadata_error_evidence(error, 'source_cleanup'))
        try:
            self.grants.cleanup()
            self.record(dict(stage='all_grants_revoked'))
        except Exception as error:
            errors.append(self.registration.metadata_error_evidence(error, 'result_cleanup'))
        self.evidence['cleanup'] = {'result':'failed' if errors else 'passed', 'errors':errors}
        self.write()
        if errors:
            raise SuiteError('topology file Grant cleanup incomplete; original evidence retained')



def run_mixed_overlay_scenario(repository,env,client,physical_runner,browser_runner,case,publication=None):
    profile=mixed_overlay_profile(case);input_nodes=profile.get('input_nodes',('load_a','load_b'));schema=profile['schema'];names=profile['sources'];target_name=profile['target'];count=profile['result_count']
    definition=profile['definition'];validate_failure=profile['failure_validator'];diagnostic=profile['diagnostic'];features=profile['features']
    tenant=positive(env['ADDP_ONLINE_TEST_TENANT_ID'],'mixed overlay tenant')
    if tenant<=1:raise SuiteError('mixed overlay forbids default Tenant')
    identity=raster.validate_identity(client,tenant,required_permissions=PERMISSIONS);principal=int(identity['principal_id'])
    deadline=time.monotonic()+float(env.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS','600'))
    def remaining():
        value=deadline-time.monotonic()
        if value<=0:raise SuiteError('mixed overlay total business budget exhausted')
        return value
    engine=positive(env['ADDP_ONLINE_TOPOLOGY_NFS_ENGINE_ID'],'NFS engine');sources=[];preflight=[]
    for name in names:
        scope=f'addp://engine/{engine}/path/{name}?type=file'
        scan=(publication.source(engine,name,scope,remaining) if publication else support.wait_for_meta_scan(client,engine,time.monotonic()+remaining(),scope={'targets':[scope]}))
        item=support.find_fixture_item(client,engine,name,'mixed overlay source')
        before=client.request('GET',f'/api/v1/meta/items/{item["id"]}',(200,)).payload
        graph=obj(client.request('GET',f'/api/v1/meta/lineage/graph?subject_kind=data_item&item_id={item["id"]}&direction=downstream',(200,)).payload,'mixed overlay resource preflight')
        if graph.get('truncated') is not False:raise SuiteError('mixed overlay requires complete authorized source lineage before execution')
        preflight.append({'source_item_id':item['id'],'graph':graph})
        sources.append({'id':item['id'],'name':name,'locator':scope+f'&item_id={item["id"]}','scan_execution_id':scan,'before':before})
    initial=scan_ids(client)
    expected_initial=sorted(s['scan_execution_id'] for s in sources) if publication is None else []
    if publication:publication.check_preparation_scans()
    if initial!=expected_initial:raise SuiteError('mixed overlay initial scan inventory differs')
    runtimes=[r for r in array(client.request('GET','/api/v1/develop/workflow-engines',(200,)).payload,'runtimes') if r.get('engine_type')=='geopython_workflow' and r.get('connection_status')=='online']
    if len(runtimes)!=1:raise SuiteError('mixed overlay requires one product Runtime')
    runtime=positive(runtimes[0]['id'],'Runtime');parent=f'addp://engine/{engine}/path/outputs?type=directory'
    target=f'addp://engine/{engine}/path/outputs/{target_name}?type=file';locators=[s['locator'] for s in sources]
    workflow=definition(locators,parent)
    if publication:publication.before_submit(identity,engine,runtime,sources,workflow)
    identifier=raster.submit(client,runtime,workflow)
    if publication:
        publication.record(dict(stage='execution_submitted',execution_id=identifier,failure=None))
        publication.approve(identifier,principal,engine,'outputs/'+target_name)
    execution=raster.wait_execution(client,'develop',identifier,remaining())
    if execution.get('execution_id')!=identifier or type(execution.get('triggered_by')) is not int or execution['triggered_by']!=principal:raise SuiteError('mixed overlay execution actor differs')
    if obj(obj(execution.get('execution_config'),'mixed overlay frozen config').get('content'),'mixed overlay content').get('workflow_definition')!=workflow:raise SuiteError('mixed overlay frozen DAG differs')
    bindings=[(f'input.{node}',locator)for node,locator in zip(input_nodes,locators)]
    inspection_scans=[]
    if publication:
        facts=validate_saved_facts(execution,bindings,{'result':target},1)
        scans,observed,deep=publication.produced(engine,'outputs/'+target_name,identifier,principal,initial,remaining)
        publication.check_preparation_scans()
    else:
        facts,scans=validate_saved_success(execution,bindings,{'result':target},1)
        observed=[]
    for scan in ([] if publication else scans):
        value=raster.wait_execution(client,'meta',scan['execution_id'],remaining())
        if type(value.get('tenant_id')) is not int or value['tenant_id']!=tenant or type(value.get('triggered_by')) is not int or value['triggered_by']!=principal:raise SuiteError('mixed overlay automatic scan public User/Tenant differs')
        observed.append(value)
    monitor=obj(client.request('GET',f'/api/v1/monitor/executions/by-execution-id/{identifier}',(200,)).payload,'mixed overlay Monitor')
    if monitor.get('execution_id')!=identifier or monitor.get('module')!='develop' or monitor.get('status')!='success' or type(monitor.get('triggered_by')) is not int or monitor['triggered_by']!=principal or obj(monitor.get('metadata'),'mixed overlay Monitor metadata').get('lineage_facts')!=facts:raise SuiteError('mixed overlay Monitor/facts differ')
    native=physical_runner(repository,env)
    if native.get('exact_inventory_verified') is not True or set(native.get('sources',{}))!=set(names) or set(native.get('targets',{}))!={target_name}:raise SuiteError('mixed overlay physical inventory differs')
    for source,kind in zip(sources,profile['kinds']):
        rows=features(kind);before=source['before'];size=before.get('size_bytes');children=before.get('attributes',{}).get('type_info',{}).get('container',{}).get('children',[])
        if type(size) is not int or size<=0 or size!=native['sources'][source['name']]['size_bytes'] or len(children)!=1 or type(children[0].get('row_count')) is not int or children[0]['row_count']!=len(rows) or type(children[0].get('column_count')) is not int or children[0]['column_count']!=len(rows[0]['properties'])+2:raise SuiteError('mixed overlay source Meta size/rows/columns differ')
    item=support.find_fixture_item(client,engine,'outputs/'+target_name,'mixed overlay output')
    detail=obj(client.request('GET',f'/api/v1/meta/items/{item["id"]}',(200,)).payload,'mixed overlay output Meta')
    physical=native['targets'][target_name];sizes=validate_item(detail,physical,target_name)
    child=detail['attributes']['type_info']['container']['children'][0]
    if type(child.get('row_count')) is not int or child['row_count']!=count or type(child.get('column_count')) is not int or child['column_count']!=len(features('result')[0]['properties'])+2:raise SuiteError('mixed overlay output rows/columns differ')
    output={'name':target_name,'port':'result','locator':target,'item_id':item['id'],**physical,'before':detail,'size_evidence':sizes}
    def unchanged_meta():
        result={}
        for entry in [*sources,output]:
            item_id=entry.get('item_id',entry.get('id'));after=client.request('GET',f'/api/v1/meta/items/{item_id}',(200,)).payload
            if after!=entry['before']:raise SuiteError('mixed overlay source or result Meta changed')
            result[entry['name']]=after
        return result
    directory=Path(env['ADDP_ONLINE_ARTIFACT_DIR']);path=directory/profile['checkpoint']
    checkpoint={'schema_version':schema+'-checkpoint/v1','case':case,'result':'incomplete','stage':'before_lineage','run_id':env['ADDP_ONLINE_TEST_RUN_ID'],
        'identity':identity,'execution_id':identifier,'definition':workflow,'execution':execution,'sources':sources,'outputs':[output],'physical':native,'automatic_scans':observed,'monitor':monitor,'stable_outputs':execution['outputs'],'lineage_facts':facts,'resource_preflight':preflight,'failures':[]}
    def persist(stage):
        checkpoint['stage']=stage;path.write_text(json.dumps(checkpoint,ensure_ascii=False,indent=2)+'\n')
    persist('before_lineage')
    for source in sources[:len(input_nodes)]:raster.wait_lineage(client,source['id'],output['item_id'],identifier,remaining())
    graph=obj(client.request('GET',f'/api/v1/meta/lineage/graph?subject_kind=data_item&item_id={output["item_id"]}&direction=upstream',(200,)).payload,'mixed overlay original dual-source graph')
    validate_proximity_graph(graph,sources[:len(input_nodes)],output['item_id'],identifier);checkpoint['raw_lineage']=graph
    expected_scans=sorted([*initial,*[s['execution_id'] for s in scans],*inspection_scans])
    if scan_ids(client)!=expected_scans:raise SuiteError('mixed overlay scan inventory differs before failures')
    for spec in profile['failures']:
        key=spec[0];failed_workflow=definition(locators,parent,key)
        if publication:publication.before_submit(identity,engine,runtime,sources,failed_workflow,failure=key)
        failed_id=raster.submit(client,runtime,failed_workflow)
        if publication:publication.record(dict(stage='execution_submitted',execution_id=failed_id,failure=key))
        failed=raster.wait_execution(client,'develop',failed_id,remaining(),expected='failed')
        validate_failure(failed,failed_id,principal,spec)
        if obj(obj(failed.get('execution_config'),'mixed overlay failed config').get('content'),'mixed overlay failed content').get('workflow_definition')!=failed_workflow:raise SuiteError('mixed overlay failed frozen DAG differs')
        failed_monitor=obj(client.request('GET',f'/api/v1/monitor/executions/by-execution-id/{failed_id}',(200,)).payload,'mixed overlay failed Monitor')
        validate_failure(failed_monitor,failed_id,principal,spec)
        if failed_monitor.get('module')!='develop' or failed_monitor.get('error_details')!=failed.get('error_details'):raise SuiteError('mixed overlay failure Monitor differs')
        failure={'key':key,'execution_id':failed_id,'definition':failed_workflow,'execution':failed,'monitor':failed_monitor,'expected_diagnostic':diagnostic(spec),'source_lineage':[]}
        for index in dict.fromkeys(spec[1:3]):
            source=sources[index]
            failed_graph=obj(client.request('GET',f'/api/v1/meta/lineage/graph?subject_kind=data_item&item_id={source["id"]}&direction=downstream',(200,)).payload,'mixed overlay failed source lineage')
            if failed_graph.get('truncated') is not False or any(e.get('relation_kind')=='derive' and e.get('evidence',{}).get('execution_id')==failed_id for e in array(failed_graph.get('edges'),'mixed overlay failed edges')):raise SuiteError('mixed overlay failure published derive or incomplete graph')
            failure['source_lineage'].append({'source_item_id':source['id'],'graph':failed_graph})
        checkpoint['failures'].append(failure);persist('failure_'+key+'_verified')
        if scan_ids(client)!=expected_scans or physical_runner(repository,env)!=native:raise SuiteError('mixed overlay failure changed physical files or scans')
        failure['meta_after']=unchanged_meta()
    if len({f['execution_id'] for f in checkpoint['failures']})!=len(profile['failures']):raise SuiteError('mixed overlay failures require distinct executions')
    persist('before_browser')
    evidence={'case':case,'run_id':env['ADDP_ONLINE_TEST_RUN_ID'],**identity,'execution_id':identifier,'sources':sources,'outputs':[output],
        'expected':{'result':physical['features']},'failures':[{'key':f['key'],'execution_id':f['execution_id'],'expected_diagnostic':f['expected_diagnostic'],'error_details':f['execution']['error_details']} for f in checkpoint['failures']]}
    remaining();browser_report=browser_runner(repository,env,evidence);remaining()
    final=physical_runner(repository,env)
    if final!=native or scan_ids(client)!=expected_scans:raise SuiteError('mixed overlay browser changed physical files or scans')
    if publication:publication.check_preparation_scans()
    meta_after=unchanged_meta()
    remaining()
    return {**checkpoint,'schema_version':schema+'/v1','result':'passed','stage':'complete','browser':browser_report,'meta_after':meta_after,'post_browser_physical':final,
        'initial_scan_inventory':initial,'final_scan_inventory':expected_scans,'sources_and_outputs_unchanged':True,'cleanup_boundary':'owned disposable NFS/Runtime/deployment; no business DELETE acceptance'}


def run_nodes_scenario(repository,env,client,physical_runner,browser_runner):
    return run_point_outputs_scenario(repository,env,client,physical_runner,browser_runner,fixture.NODES_CASE)


def run_nodes_lineage_permissions(repository, env, client, physical_runner):
    if any(env.get(key) != value for key, value in {
        'ADDP_ONLINE_HOST': '1', 'ADDP_ONLINE_HOSTED': '1', 'GITHUB_ACTIONS': 'true', 'RUNNER_OS': 'Linux',
    }.items()):
        raise SuiteError('nodes lineage permissions requires the disposable Hosted runner')
    for key in ('ADDP_ONLINE_READ_USER_ACCESS_TOKEN', 'ADDP_ONLINE_READ_USER_USERNAME',
                'ADDP_ONLINE_READ_USER_PASSWORD', 'ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN'):
        if not env.get(key):
            raise SuiteError(f'nodes lineage permissions fixture missing {key}')
    registration = importlib.import_module('scripts.test.online-engine-registration')
    tenant = positive(env['ADDP_ONLINE_TEST_TENANT_ID'], 'disposable Tenant')
    actor = raster.validate_identity(client, tenant, required_permissions=PERMISSIONS)
    reader = raster.GatewayClient(env['GATEWAY_URL'], env['ADDP_ONLINE_READ_USER_ACCESS_TOKEN'], 30)
    issuer = raster.GatewayClient(env['GATEWAY_URL'], env['ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN'], 30)
    identity = raster.validate_identity(reader, tenant, required_permissions={
        'manager.data_item.read', 'meta.catalog.read', 'meta.lineage.read'})
    if identity['principal_id'] == actor['principal_id']:
        raise SuiteError('the lineage reader must be separate from the execution User')
    engine = positive(env['ADDP_ONLINE_TOPOLOGY_NFS_ENGINE_ID'], 'NFS engine')
    deadline = time.monotonic() + float(env['ADDP_ONLINE_TEST_TIMEOUT_SECONDS'])

    def remaining():
        seconds = deadline - time.monotonic()
        if seconds <= 0:
            raise SuiteError('nodes lineage permissions business budget exhausted')
        return seconds

    directory = Path(env['ADDP_ONLINE_ARTIFACT_DIR'])
    report = dict(schema_version='addp.line-nodes-lineage-permissions/v1',
                  run_id=env['ADDP_ONLINE_TEST_RUN_ID'], engine_id=engine,
                  identity=identity, execution_actor=actor, result='incomplete')
    path = directory/'topology-lineage-permissions.json'

    def persist():
        path.write_text(json.dumps(report, ensure_ascii=False, indent=2)+'\n')

    grants = registration.ExactFileMetadataGrants(issuer, reader, tenant, scan_preparation=True)
    report['issuer_id'] = grants.operator
    report['grants'] = {}

    def grant(name):
        receipt = grants.grant(engine, name, 'nfs', data_type='container', format_name='geopackage', layout='single')
        report['grants'][name] = receipt['request_id']
        persist()
        return receipt['request_id']

    stage = 'source_scan'
    persist()
    try:
        source_name = fixture.NODES_SOURCES[0]
        source_target = f'addp://engine/{engine}/path/{source_name}?type=file'
        report['source_scan'] = support.wait_for_meta_scan(issuer, engine, deadline, scope={'targets': [source_target]})
        stage = 'source_grant'
        source_grant = grant(source_name)
        source = support.find_fixture_item(reader, engine, source_name, 'nodes source')
        source_locator = support.build_item_locator(engine, source)
        stage = 'workflow'
        runtimes = [r for r in array(client.request('GET', '/api/v1/develop/workflow-engines', (200,)).payload, 'runtimes')
                    if r.get('engine_type') == 'geopython_workflow' and r.get('connection_status') == 'online']
        if len(runtimes) != 1:
            raise SuiteError('nodes requires one product Runtime')
        parent = f'addp://engine/{engine}/path/outputs?type=directory'
        workflow = nodes_definition([source_locator], parent)
        identifier = raster.submit(client, positive(runtimes[0]['id'], 'Runtime'), workflow)
        report['execution_id'] = identifier
        persist()
        execution = raster.wait_execution(client, 'develop', identifier, remaining())
        if execution.get('triggered_by') != int(actor['principal_id']):
            raise SuiteError('nodes execution actor differs')
        targets = {port: f'addp://engine/{engine}/path/outputs/{name}?type=file'
                   for port, name in zip(fixture.NODES_PORTS, fixture.NODES_TARGETS)}
        _, scans = validate_saved_success(execution, [('input.load_source', source_locator)], targets, len(targets))
        report['outputs'] = []
        stage = 'automatic_output_scans'
        for scan in scans:
            raster.wait_execution(client, 'meta', scan['execution_id'], remaining())
        stage = 'authorized_baseline'
        for name in fixture.NODES_TARGETS:
            full_name = 'outputs/'+name
            request_id = grant(full_name)
            item = support.find_fixture_item(reader, engine, full_name, 'nodes output')
            raster.wait_lineage(reader, source['id'], item['id'], identifier, remaining())
            graph_path = f'/api/v1/meta/lineage/graph?subject_kind=data_item&item_id={item["id"]}&direction=upstream'
            graph = reader.request('GET', graph_path, (200,)).payload
            validate_nodes_graph(graph, [source], item['id'], identifier)
            if graph.get('permission_filtered') is not False:
                raise SuiteError('the authorized baseline omitted a lineage relationship')
            report['outputs'].append(dict(name=name, item_id=item['id'], grant_id=request_id,
                                          locator=support.build_item_locator(engine, item), graph=graph))
        report['source'] = dict(id=source['id'], name=source_name, grant_id=source_grant)
        native = physical_runner(repository, env)
        persist()
        binding = {**identity, **{key: report[key] for key in ('run_id', 'execution_id', 'engine_id', 'source')},
                   'output': {key: value for key, value in report['outputs'][0].items() if key != 'graph'}}
        browser_path = directory/'topology-lineage-permissions-browser.json'
        browser_path.unlink(missing_ok=True)
        browser_env = dict(env, ADDP_ONLINE_TOPOLOGY_EVIDENCE=json.dumps(binding))
        for key in ('ADDP_ONLINE_TEST_USER_ACCESS_TOKEN', 'ADDP_ONLINE_TEST_USER_USERNAME',
                    'ADDP_ONLINE_TEST_USER_PASSWORD', 'ADDP_ONLINE_READ_USER_ACCESS_TOKEN'):
            browser_env.pop(key, None)
        stage = 'browser'
        result = subprocess.run(['npm', 'exec', '--', 'playwright', 'test',
            'e2e/online/vector-topology-persistence.spec.js', '--config=playwright.online.config.js',
            '--grep', 'NFS GeoPackage 同一普通用户撤权血缘$'], cwd=repository/'console/frontend',
            env=browser_env, stdout=sys.stderr, stderr=sys.stderr,
            timeout=min(200, remaining()))
        if result.returncode or not browser_path.is_file():
            raise SuiteError('nodes lineage permissions browser evidence missing or failed')
        browser_report = obj(json.loads(browser_path.read_text()), 'nodes lineage permissions browser evidence')
        if browser_report.get('binding') != binding or browser_report.get('result') != 'passed':
            raise SuiteError('nodes lineage permissions browser evidence did not bind the exact scene')
        if physical_runner(repository, env) != native:
            raise SuiteError('nodes lineage permission checks changed physical files')
        report['browser'] = browser_report
        report['physical_unchanged'] = True
    except Exception as error:
        report['failure'] = registration.metadata_error_evidence(error, stage)
        raise
    finally:
        try:
            report['cleanup'] = grants.cleanup()
        except Exception as error:
            report['cleanup_error'] = registration.metadata_error_evidence(error, grants.phase)
            if 'failure' not in report:
                raise
        finally:
            persist()
    report['result'] = 'passed'
    persist()
    return report


def run_point_outputs_scenario(repository,env,client,physical_runner,browser_runner,case):
    vertices=case==fixture.VERTICES_CASE
    names=fixture.VERTICES_SOURCES if vertices else fixture.NODES_SOURCES
    kinds=fixture.VERTICES_KINDS if vertices else fixture.NODES_KINDS
    ports=fixture.VERTICES_FAMILIES if vertices else fixture.NODES_PORTS
    targets_names=fixture.VERTICES_TARGETS if vertices else fixture.NODES_TARGETS
    schema=fixture.VERTICES_SCHEMA if vertices else fixture.NODES_SCHEMA
    prefix='vertices' if vertices else 'nodes'
    expected=lambda kind:fixture.vertices_features(kind+'-result' if kind in ports else kind) if vertices else fixture.nodes_features(kind)
    definition=vertices_definition if vertices else nodes_definition
    validate_failure=validate_vertices_failure if vertices else validate_nodes_failure
    diagnostic_for=vertices_diagnostic if vertices else nodes_diagnostic
    specs=fixture.VERTICES_FAILURES if vertices else fixture.NODES_FAILURES
    source_count=3 if vertices else 1
    tenant=positive(env['ADDP_ONLINE_TEST_TENANT_ID'],'tenant')
    if tenant<=1:raise SuiteError('nodes forbids default Tenant')
    identity=raster.validate_identity(client,tenant,required_permissions=PERMISSIONS);principal=int(identity['principal_id'])
    deadline=time.monotonic()+float(env.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS','600'))
    def remaining():
        value=deadline-time.monotonic()
        if value<=0:raise SuiteError('nodes business budget exhausted')
        return value
    engine=positive(env['ADDP_ONLINE_TOPOLOGY_NFS_ENGINE_ID'],'NFS engine');sources=[]
    for name in names:
        scope=f'addp://engine/{engine}/path/{name}?type=file'
        scan=support.wait_for_meta_scan(client,engine,time.monotonic()+remaining(),scope={'targets':[scope]})
        item=support.find_fixture_item(client,engine,name,'nodes source')
        before=client.request('GET',f'/api/v1/meta/items/{item["id"]}',(200,)).payload
        sources.append({'id':item['id'],'name':name,'locator':scope+f'&item_id={item["id"]}','scan_execution_id':scan,'before':before})
    preflight=[]
    if vertices:
        for source in sources:
            graph=obj(client.request('GET',f'/api/v1/meta/lineage/graph?subject_kind=data_item&item_id={source["id"]}&direction=downstream',(200,)).payload,'vertex authorized source lineage')
            if graph.get('truncated') is not False or graph.get('permission_filtered') is not False:raise SuiteError('vertices require complete formally authorized source lineage before execution')
            preflight.append({'source_item_id':source['id'],'graph':graph})
    initial=scan_ids(client)
    if initial!=sorted(s['scan_execution_id'] for s in sources):raise SuiteError('nodes initial scan inventory differs')
    runtimes=[r for r in array(client.request('GET','/api/v1/develop/workflow-engines',(200,)).payload,'runtimes') if r.get('engine_type')=='geopython_workflow' and r.get('connection_status')=='online']
    if len(runtimes)!=1:raise SuiteError('nodes requires one product Runtime')
    runtime=positive(runtimes[0]['id'],'Runtime');parent=f'addp://engine/{engine}/path/outputs?type=directory'
    targets={port:f'addp://engine/{engine}/path/outputs/{name}?type=file' for port,name in zip(ports,targets_names)}
    workflow=definition([s['locator'] for s in sources],parent)
    identifier=raster.submit(client,runtime,workflow);execution=raster.wait_execution(client,'develop',identifier,remaining())
    if execution.get('execution_id')!=identifier or type(execution.get('triggered_by')) is not int or execution['triggered_by']!=principal:raise SuiteError('nodes execution actor differs')
    if obj(obj(execution.get('execution_config'),'nodes frozen config').get('content'),'nodes content').get('workflow_definition')!=workflow:raise SuiteError('nodes frozen DAG differs')
    inputs=[('input.load_'+family,sources[index]['locator']) for index,family in enumerate(ports)] if vertices else [('input.load_source',sources[0]['locator'])]
    facts,scans=validate_saved_success(execution,inputs,targets,len(ports))
    observed=[]
    for scan in scans:
        value=raster.wait_execution(client,'meta',scan['execution_id'],remaining())
        if type(value.get('tenant_id')) is not int or value['tenant_id']!=tenant or type(value.get('triggered_by')) is not int or value['triggered_by']!=principal:raise SuiteError('nodes automatic scan public User/Tenant differs')
        observed.append(value)
    monitor=obj(client.request('GET',f'/api/v1/monitor/executions/by-execution-id/{identifier}',(200,)).payload,'nodes Monitor')
    if monitor.get('execution_id')!=identifier or monitor.get('module')!='develop' or monitor.get('status')!='success' or type(monitor.get('triggered_by')) is not int or monitor['triggered_by']!=principal or obj(monitor.get('metadata'),'Monitor metadata').get('lineage_facts')!=facts:raise SuiteError('nodes Monitor facts differ')
    native=physical_runner(repository,env)
    if native.get('exact_inventory_verified') is not True or set(native.get('sources',{}))!=set(names) or set(native.get('targets',{}))!=set(targets_names):raise SuiteError('nodes physical inventory differs')
    for source,kind in zip(sources,kinds):
        rows=fixture.vertices_features(kind) if vertices else expected(kind);count=len(rows);columns=len(rows[0]['properties'])+2
        before=source['before'];size=before.get('size_bytes');children=before.get('attributes',{}).get('type_info',{}).get('container',{}).get('children',[])
        if type(size) is not int or size<=0 or size!=native['sources'][source['name']]['size_bytes'] or len(children)!=1 or type(children[0].get('row_count')) is not int or children[0]['row_count']!=count or type(children[0].get('column_count')) is not int or children[0]['column_count']!=columns:raise SuiteError('nodes source Meta size/rows/columns differ')
    outputs=[]
    for port,name in zip(ports,targets_names):
        rows=expected(port);count=len(rows);columns=len(rows[0]['properties'])+2
        item=support.find_fixture_item(client,engine,'outputs/'+name,'nodes output');detail=obj(client.request('GET',f'/api/v1/meta/items/{item["id"]}',(200,)).payload,'nodes output Meta')
        physical=native['targets'][name];sizes=validate_item(detail,physical,name);child=detail['attributes']['type_info']['container']['children'][0]
        if type(child.get('row_count')) is not int or child['row_count']!=count or type(child.get('column_count')) is not int or child['column_count']!=columns:raise SuiteError('nodes output Meta rows/columns differ')
        outputs.append({'name':name,'port':port,'locator':targets[port],'item_id':item['id'],**physical,'before':detail,'size_evidence':sizes})
    directory=Path(env['ADDP_ONLINE_ARTIFACT_DIR'])
    checkpoint={'schema_version':schema+'-checkpoint/v1','case':case,'result':'incomplete','stage':'before_lineage','run_id':env['ADDP_ONLINE_TEST_RUN_ID'],
        'identity':identity,'execution_id':identifier,'definition':workflow,'execution':execution,'sources':sources,'outputs':outputs,'physical':native,'automatic_scans':observed,'monitor':monitor,'stable_outputs':execution['outputs'],'lineage_facts':facts,**({'source_authorization_preflight':preflight} if vertices else {})}
    path=directory/('topology-'+prefix+'-checkpoint.json');path.write_text(json.dumps(checkpoint,ensure_ascii=False,indent=2)+'\n')
    graphs={}
    for output_index,output in enumerate(outputs):
        bound_sources=[sources[output_index]] if vertices else sources[:1]
        for source in bound_sources:raster.wait_lineage(client,source['id'],output['item_id'],identifier,remaining())
        graph=obj(client.request('GET',f'/api/v1/meta/lineage/graph?subject_kind=data_item&item_id={output["item_id"]}&direction=upstream',(200,)).payload,'nodes original graph')
        (validate_vertices_graph if vertices else validate_nodes_graph)(graph,bound_sources,output['item_id'],identifier);graphs[output['port']]=graph
    expected_scans=sorted([*initial,*[s['execution_id'] for s in scans]])
    if scan_ids(client)!=expected_scans:raise SuiteError('nodes scan inventory differs before failure')
    failures=[]
    checkpoint['failures']=failures
    for spec in specs:
        key=spec[0]
        failed_workflow=definition([s['locator'] for s in sources],parent,failure=key)
        failed_id=raster.submit(client,runtime,failed_workflow);failed=raster.wait_execution(client,'develop',failed_id,remaining(),expected='failed')
        validate_failure(failed,failed_id,principal,spec)
        if obj(obj(failed.get('execution_config'),'nodes failed config').get('content'),'nodes failed content').get('workflow_definition')!=failed_workflow:raise SuiteError('nodes failed frozen DAG differs')
        diagnostic=diagnostic_for(spec)
        failed_monitor=obj(client.request('GET',f'/api/v1/monitor/executions/by-execution-id/{failed_id}',(200,)).payload,'failed Monitor')
        if failed_monitor.get('execution_id')!=failed_id or failed_monitor.get('status')!='failed' or failed_monitor.get('module')!='develop' or type(failed_monitor.get('triggered_by')) is not int or failed_monitor['triggered_by']!=principal:raise SuiteError('failure Monitor identity differs')
        failure={'key':key,'execution_id':failed_id,'definition':failed_workflow,'execution':failed,'monitor':failed_monitor,'diagnostic':diagnostic,'no_publication_or_scan':True}
        validate_failure(failed_monitor,failed_id,principal,spec)
        if failed_monitor.get('error_details')!=failed.get('error_details'):raise SuiteError('nodes Monitor diagnostic differs')
        failure['expected_diagnostic']=diagnostic
        failure['source_lineage']=[]
        source=sources[source_count+[v[0] for v in specs].index(key)]
        graph=obj(client.request('GET',f'/api/v1/meta/lineage/graph?subject_kind=data_item&item_id={source["id"]}&direction=downstream',(200,)).payload,'nodes failed source graph')
        if graph.get('truncated') is not False or any(e.get('relation_kind')=='derive' and e.get('evidence',{}).get('execution_id')==failed_id for e in array(graph.get('edges'),'nodes failed edges')):raise SuiteError('nodes failure published derive or incomplete graph')
        failure['source_lineage'].append({'source_item_id':source['id'],'graph':graph})
        failures.append(failure)
        checkpoint.update(stage='failure_'+key+'_verified',raw_lineage=graphs)
        path.write_text(json.dumps(checkpoint,ensure_ascii=False,indent=2)+'\n')
        if scan_ids(client)!=expected_scans or physical_runner(repository,env)!=native:raise SuiteError('failure changed physical files or scans')
    if len({identifier,*[f['execution_id'] for f in failures]})!=len(specs)+1:raise SuiteError('failure executions must be distinct')
    checkpoint.update(stage='before_browser',raw_lineage=graphs)
    path.write_text(json.dumps(checkpoint,ensure_ascii=False,indent=2)+'\n')
    evidence={'case':case,'run_id':env['ADDP_ONLINE_TEST_RUN_ID'],**identity,'execution_id':identifier,'sources':sources,'outputs':outputs,
        'expected':{port:expected(port) for port in ports}}
    evidence['failures']=[{'key':f['key'],'execution_id':f['execution_id'],'expected_diagnostic':f['expected_diagnostic'],'error_details':f['execution']['error_details']} for f in failures]
    remaining();browser_report=browser_runner(repository,env,evidence);remaining()
    final=physical_runner(repository,env)
    if final!=native or scan_ids(client)!=expected_scans:raise SuiteError('nodes browser changed physical files or scan inventory')
    meta_after={}
    for entry in [*sources,*outputs]:
        item_id=entry.get('item_id',entry.get('id'));after=client.request('GET',f'/api/v1/meta/items/{item_id}',(200,)).payload
        if after!=entry['before']:raise SuiteError('nodes source or output Meta changed')
        meta_after[entry['name']]=after
    remaining()
    return {**checkpoint,'schema_version':schema+'/v1','result':'passed','stage':'complete','browser':browser_report,'meta_after':meta_after,'post_browser_physical':final,
        'initial_scan_inventory':initial,'final_scan_inventory':expected_scans,'sources_and_outputs_unchanged':True,'cleanup_boundary':'owned disposable NFS/Runtime/deployment; no business DELETE acceptance'}


def proximity_definition(sources,parent):
    return {'tasks':[
        {'id':'load_source','operator':'load','params':{'locator':sources[0]},'depends_on':[]},
        {'id':'load_reference','operator':'load','params':{'locator':sources[1]},'depends_on':[]},
        {'id':'near','operator':'check_line_proximity','params':{'input_gdf':{'$ref':'load_source'},'reference_gdf':{'$ref':'load_reference'},
         'id_field':'route_key','reference_id_field':'ref_key','tolerance':1.},'depends_on':['load_source','load_reference']},
        {'id':'save_result','operator':'save','params':{'input_df':{'$ref':'near'},'target_parent_locator':parent,'target_name':fixture.PROXIMITY_TARGET,'mode':'fail'},'depends_on':['near']}]}


def validate_proximity_success(execution,sources,target):
    if execution.get('status')!='success' or execution.get('outputs')!={'save_result':{'resource':{'locator':target,'type':'file','write_mode':'create'}}}:
        raise SuiteError('proximity must publish exactly one stable create output')
    metadata=obj(execution.get('metadata'),'proximity metadata');facts=obj(metadata.get('lineage_facts'),'proximity facts')
    inputs=array(facts.get('inputs'),'proximity inputs');outputs=array(facts.get('outputs'),'proximity outputs')
    if (facts.get('schema_version')!='addp.lineage-facts/v1' or len(inputs)!=2 or
        {(value.get('port'),value.get('locator')) for value in inputs}!={('input.load_source',sources[0]),('input.load_reference',sources[1])} or
        len(outputs)!=1 or outputs[0].get('locator')!=target or outputs[0].get('write_mode')!='create'):
        raise SuiteError('proximity dual read ports or create facts differ')
    scans=array(obj(metadata.get('result'),'proximity result').get('meta_scan_runs'),'proximity automatic scan')
    if len(scans)!=1 or scans[0].get('status')!='submitted' or scans[0].get('target_locator')!=target or not isinstance(scans[0].get('execution_id'),str) or not scans[0]['execution_id']:
        raise SuiteError('proximity requires exactly one automatic output scan')
    return facts,scans[0]


def validate_proximity_graph(graph,sources,target,identifier):
    if graph.get('truncated') is not False:raise SuiteError('proximity lineage graph is truncated or incomplete')
    edges=[edge for edge in array(graph.get('edges'),'proximity graph edges') if edge.get('relation_kind')=='derive' and edge.get('status')=='active' and edge.get('evidence',{}).get('execution_id')==identifier]
    if len(edges)!=len(sources) or any(type(e.get(side,{}).get('item_id')) is not int for e in edges for side in ('source','target')) or {(e.get('source',{}).get('item_id'),e.get('target',{}).get('item_id')) for e in edges}!={(s['id'],target) for s in sources}:
        raise SuiteError('lineage requires exactly the declared current-execution active source edges')
    return edges


def run_proximity_scenario(repository,env,client,physical_runner,browser_runner):
    tenant=positive(env['ADDP_ONLINE_TEST_TENANT_ID'],'tenant')
    if tenant<=1:raise SuiteError('proximity forbids default Tenant')
    identity=raster.validate_identity(client,tenant,required_permissions=PERMISSIONS)
    deadline=time.monotonic()+float(env.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS','600'))
    def remaining():
        value=deadline-time.monotonic()
        if value<=0:raise SuiteError('proximity business budget exhausted')
        return value
    engine=positive(env['ADDP_ONLINE_TOPOLOGY_NFS_ENGINE_ID'],'NFS engine');sources=[]
    for name in fixture.PROXIMITY_SOURCES:
        scope=f'addp://engine/{engine}/path/{name}?type=file'
        scan=support.wait_for_meta_scan(client,engine,time.monotonic()+remaining(),scope={'targets':[scope]})
        item=support.find_fixture_item(client,engine,name,'proximity source')
        before=client.request('GET',f'/api/v1/meta/items/{item["id"]}',(200,)).payload
        sources.append({'id':item['id'],'name':name,'locator':scope+f'&item_id={item["id"]}','scan_execution_id':scan,'before':before})
    initial_scans=scan_ids(client)
    if initial_scans!=sorted(source['scan_execution_id'] for source in sources):raise SuiteError('proximity initial scan inventory differs')
    runtimes=[r for r in array(client.request('GET','/api/v1/develop/workflow-engines',(200,)).payload,'runtimes') if r.get('engine_type')=='geopython_workflow' and r.get('connection_status')=='online']
    if len(runtimes)!=1:raise SuiteError('proximity requires one online product Runtime')
    parent=f'addp://engine/{engine}/path/outputs?type=directory';target=f'addp://engine/{engine}/path/outputs/{fixture.PROXIMITY_TARGET}?type=file'
    workflow=proximity_definition([source['locator'] for source in sources],parent)
    identifier=raster.submit(client,positive(runtimes[0]['id'],'Runtime'),workflow)
    execution=raster.wait_execution(client,'develop',identifier,remaining())
    if execution.get('execution_id')!=identifier or type(execution.get('triggered_by')) is not int or execution['triggered_by']!=int(identity['principal_id']):raise SuiteError('proximity execution actor differs')
    if obj(obj(execution.get('execution_config'),'proximity frozen config').get('content'),'proximity frozen content').get('workflow_definition')!=workflow:raise SuiteError('proximity frozen workflow differs')
    facts,scan=validate_proximity_success(execution,[source['locator'] for source in sources],target)
    observed=raster.wait_execution(client,'meta',scan['execution_id'],remaining())
    if type(observed.get('tenant_id')) is not int or observed['tenant_id']!=tenant or type(observed.get('triggered_by')) is not int or observed['triggered_by']!=int(identity['principal_id']):raise SuiteError('proximity public automatic scan identity differs')
    monitor=obj(client.request('GET',f'/api/v1/monitor/executions/by-execution-id/{identifier}',(200,)).payload,'proximity Monitor')
    if monitor.get('execution_id')!=identifier or monitor.get('module')!='develop' or monitor.get('status')!='success' or type(monitor.get('triggered_by')) is not int or monitor['triggered_by']!=int(identity['principal_id']) or obj(monitor.get('metadata'),'Monitor metadata').get('lineage_facts')!=facts:raise SuiteError('proximity Monitor status or lineage differs')
    native=physical_runner(repository,env)
    if native.get('exact_inventory_verified') is not True or set(native.get('sources',{}))!=set(fixture.PROXIMITY_SOURCES) or set(native.get('targets',{}))!={fixture.PROXIMITY_TARGET}:raise SuiteError('proximity physical inventory differs')
    for source,kind in zip(sources,('source','reference')):
        size=source['before'].get('size_bytes')
        children=source['before'].get('attributes',{}).get('type_info',{}).get('container',{}).get('children',[])
        if type(size) is not int or size<=0 or size!=native['sources'][source['name']]['size_bytes'] or len(children)!=1 or type(children[0].get('row_count')) is not int or children[0]['row_count']!=len(fixture.proximity_features(kind)):
            raise SuiteError('proximity source Meta size or child rows differ')
    item=support.find_fixture_item(client,engine,'outputs/'+fixture.PROXIMITY_TARGET,'proximity output')
    detail=obj(client.request('GET',f'/api/v1/meta/items/{item["id"]}',(200,)).payload,'proximity output Meta')
    child=detail['attributes']['type_info']['container']['children']
    if len(child)!=1 or type(child[0].get('row_count')) is not int or child[0]['row_count']!=7 or child[0].get('column_count')!=7:raise SuiteError('proximity Meta child row/column count differs')
    output={'name':fixture.PROXIMITY_TARGET,'port':'result','locator':target,'item_id':item['id'],**native['targets'][fixture.PROXIMITY_TARGET],
            'before':detail,'size_evidence':validate_item(detail,native['targets'][fixture.PROXIMITY_TARGET],fixture.PROXIMITY_TARGET)}
    directory=Path(env['ADDP_ONLINE_ARTIFACT_DIR'])
    checkpoint={'schema_version':fixture.PROXIMITY_SCHEMA+'-checkpoint/v1','case':fixture.PROXIMITY_CASE,'result':'incomplete','stage':'before_lineage',
                'run_id':env['ADDP_ONLINE_TEST_RUN_ID'],'identity':identity,'execution_id':identifier,'definition':workflow,'execution':execution,
                'sources':sources,'output':output,'physical':native,'automatic_scan':observed,'monitor':monitor}
    (directory/'topology-proximity-checkpoint.json').write_text(json.dumps(checkpoint,ensure_ascii=False,indent=2)+'\n')
    for source in sources:raster.wait_lineage(client,source['id'],item['id'],identifier,remaining())
    graph=obj(client.request('GET',f'/api/v1/meta/lineage/graph?subject_kind=data_item&item_id={item["id"]}&direction=upstream',(200,)).payload,'proximity raw graph')
    validate_proximity_graph(graph,sources,item['id'],identifier)
    evidence={'case':fixture.PROXIMITY_CASE,'run_id':env['ADDP_ONLINE_TEST_RUN_ID'],**identity,'execution_id':identifier,'sources':sources,'output':output,'expected':fixture.proximity_features('result')}
    remaining();browser_report=browser_runner(repository,env,evidence);remaining()
    final=physical_runner(repository,env)
    expected_scans=sorted([*initial_scans,scan['execution_id']])
    if scan_ids(client)!=expected_scans or final!=native:raise SuiteError('proximity browser changed physical files or scan inventory')
    meta_after={}
    for source in sources:
        after=client.request('GET',f'/api/v1/meta/items/{source["id"]}',(200,)).payload
        if after!=source['before']:raise SuiteError('proximity source Meta changed')
        meta_after[source['name']]=after
    after=client.request('GET',f'/api/v1/meta/items/{item["id"]}',(200,)).payload
    if after!=detail:raise SuiteError('proximity output Meta changed during browser')
    meta_after[fixture.PROXIMITY_TARGET]=after
    return {**checkpoint,'schema_version':fixture.PROXIMITY_SCHEMA+'/v1','result':'passed','stage':'complete','lineage_facts':facts,'raw_lineage':graph,
            'initial_scan_inventory':initial_scans,'final_scan_inventory':expected_scans,'automatic_scan_reference':scan,'meta_after':meta_after,
            'post_browser_physical':final,'browser':browser_report,'sources_and_output_unchanged':True,
            'cleanup_boundary':'owned disposable NFS/Runtime/deployment; no business DELETE acceptance'}


def normalize_definition(source,parent,failed=False,case=fixture.NORMALIZE_CASE):
    profile=fixture.network_profile(case);composition=case==fixture.COMPOSITION_CASE
    if case==fixture.BUFFER_CASE:
        return {'tasks':[{'id':'normal_input','operator':'load','params':{'locator':source},'depends_on':[]},
            {'id':'normal','operator':'batch_buffer','params':{'input_gdf':{'$ref':'normal_input'},'distances':[2,1,2],'resolution':1},'depends_on':['normal_input']},
            {'id':'save_result','operator':'save','params':{'input_df':{'$ref':'normal'},'target_parent_locator':parent,
             'target_name':('failed-' if failed else '')+profile['targets'][0],'mode':'fail'},'depends_on':['normal']}] }
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


def validate_saved_facts(execution,bindings,targets,count):
    outputs=obj(execution.get('outputs'),'normalize stable outputs')
    expected={'save_'+port:{'resource':{'locator':locator,'type':'file','write_mode':'create'}} for port,locator in targets.items()}
    if execution.get('status')!='success' or outputs!=expected:raise SuiteError('normalization must publish exactly the selected named ports')
    metadata=obj(execution.get('metadata'),'normalize metadata');facts=obj(metadata.get('lineage_facts'),'normalize facts')
    inputs=array(facts.get('inputs'),'normalize inputs');produced=array(facts.get('outputs'),'normalize outputs')
    if (facts.get('schema_version')!='addp.lineage-facts/v1' or
        len(inputs)!=len(bindings) or {(x.get('port'),x.get('locator')) for x in inputs}!=set(bindings) or
        len(produced)!=count or {x.get('locator') for x in produced}!=set(targets.values()) or
        any(x.get('write_mode')!='create' for x in produced)):
        raise SuiteError('normalization source port or selected create facts differ')
    return facts



def validate_saved_success(execution,bindings,targets,count):
    facts=validate_saved_facts(execution,bindings,targets,count)
    scans=array(obj(obj(execution.get('metadata'),'normalize metadata').get('result'),'normalize result').get('meta_scan_runs'),'normalize scans')
    if (len(scans)!=count or {s.get('target_locator') for s in scans}!=set(targets.values()) or
        any(s.get('status')!='submitted' or not isinstance(s.get('execution_id'),str) or not s['execution_id'] for s in scans) or
        len({s['execution_id'] for s in scans})!=count):raise SuiteError('normalization requires exactly the selected distinct automatic scans')
    return facts,scans


def validate_normalize_success(execution,source,targets,case=fixture.NORMALIZE_CASE):
    return validate_saved_success(execution,[("input.normal_input",source)],targets,len(fixture.network_profile(case)["ports"]))

def run_normalize_scenario(repository,env,client,physical_runner,browser_runner,diagnostic_runner):
    case=fixture.topology_case(env);profile=fixture.network_profile(case)
    ports,targets_names=profile['ports'],profile['targets'];source_name=profile['source'];schema=profile['schema']
    composition=case==fixture.COMPOSITION_CASE;buffer=case==fixture.BUFFER_CASE
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
    conflict_source=None
    if buffer:
        name=fixture.BUFFER_SOURCES[1];conflict_scope=f'addp://engine/{engine}/path/{name}?type=file'
        conflict_scan=support.wait_for_meta_scan(client,engine,time.monotonic()+timeout,scope={'targets':[conflict_scope]})
        conflict_item=support.find_fixture_item(client,engine,name,'buffer conflict source')
        conflict_before=client.request('GET',f'/api/v1/meta/items/{conflict_item["id"]}',(200,)).payload
        conflict_source={'name':name,'id':conflict_item['id'],'locator':conflict_scope+f'&item_id={conflict_item["id"]}',
                         'scan_execution_id':conflict_scan,'before':conflict_before}
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
    if (native.get('exact_inventory_verified') is not True or set(native.get('sources',{}))!=(set(fixture.BUFFER_SOURCES) if buffer else {source_name}) or
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
                'automatic_scan_observations':observed,'monitor':monitor,'physical':native,**({'conflict_source':conflict_source} if buffer else {})}
    directory=Path(env['ADDP_ONLINE_ARTIFACT_DIR'])
    (directory/'topology-before-lineage.json').write_text(json.dumps(checkpoint,indent=2)+'\n')
    for output in outputs:
        output['lineage']=[raster.wait_lineage(client,source['id'],output['item_id'],identifier,timeout)]
    if client.request('GET',f'/api/v1/meta/items/{source["id"]}',(200,)).payload!=before:raise SuiteError('normalization source Meta changed')
    scans_before=scan_ids(client)
    if scans_before!=sorted([source['scan_execution_id'],*([conflict_source['scan_execution_id']] if buffer else []),*[s['execution_id'] for s in scans]]):raise SuiteError('normalization source/output scan inventory differs')
    failed_id=raster.submit(client,runtime,normalize_definition(conflict_source['locator'] if buffer else source['locator'],parent,failed=True,case=case))
    failed=raster.wait_execution(client,'develop',failed_id,timeout,expected='failed')
    fm=obj(failed.get('metadata'),'failed normalize metadata');fr=fm.get('result')
    if failed.get('status')!='failed' or failed.get('outputs') or fm.get('lineage_facts') or (isinstance(fr,dict) and fr.get('meta_scan_runs')):
        raise SuiteError('normalization validation failure must publish neither port nor scan/facts')
    if buffer:
        diagnostic=diagnostic_runner(repository,env,failed_id,task='normal',
            reason='源图层的列名 distance 与多距缓冲输出列冲突；请先手动重命名源列，再执行多距缓冲。',
            cause='distance_conflict')
    else:
        if composition:
            spec = ('invalid_chain_id_field', 'merge_line_chains', 'operator_input_invalid', 'input_gdf', '')
            node_index = 3
        else:
            spec = ('invalid_normalize_id_field', 'normalize_line_network', 'operator_input_invalid', 'input_gdf', '')
            node_index = 2
        diagnostic = validate_input_failure(failed, spec, failed_id, node_index=node_index)
    failed_monitor=obj(client.request('GET',f'/api/v1/monitor/executions/by-execution-id/{failed_id}',(200,)).payload,'failed normalize Monitor')
    if failed_monitor.get('status')!='failed' or failed_monitor.get('module')!='develop':raise SuiteError('normalization failure Monitor differs')
    if not buffer:
        if validate_input_failure(failed_monitor, spec, failed_id, node_index=node_index) != diagnostic:
            raise SuiteError('normalization failure Monitor diagnostic differs from Develop')
    if scan_ids(client)!=scans_before or physical_runner(repository,env)!=native:raise SuiteError('normalization failure changed scans or physical files')
    failure={'execution_id':failed_id,'status':'failed','no_publication_or_scan':True,'diagnostic':diagnostic,
             'observation':{'status':failed['status'],'outputs':failed.get('outputs'),'lineage_facts':fm.get('lineage_facts'),
                            'meta_scan_runs':fr.get('meta_scan_runs') if isinstance(fr,dict) else None},'monitor':failed_monitor}
    (directory/'topology-normalize-failure.json').write_text(json.dumps({**checkpoint,'stage':'field_failure_validated','failed_normalize':failure},indent=2)+'\n')
    evidence={'case':case,'run_id':env['ADDP_ONLINE_TEST_RUN_ID'],**identity,'execution_id':identifier,
              'failed_execution_id':failed_id,'source':source,'outputs':[{k:v for k,v in o.items() if k not in ('lineage','size_evidence')} for o in outputs],
              'expected':{port:fixture.network_features(case,port) for port in ports},**({'conflict_source':conflict_source} if buffer else {})}
    report=browser_runner(repository,env,evidence)
    final=physical_runner(repository,env)
    if final!=native:raise SuiteError('normalization browser changed source or outputs')
    for original in [source,*([conflict_source] if buffer else [])]:
        if client.request('GET',f'/api/v1/meta/items/{original["id"]}',(200,)).payload!=original['before']:raise SuiteError('independent case source Meta changed')
    return {'schema_version':schema+'/v1','case':case,'suite':SUITE,'result':'passed',
            'run_id':env['ADDP_ONLINE_TEST_RUN_ID'],'identity':identity,'execution_id':identifier,'source':source,
            'outputs':outputs,'stable_outputs':execution['outputs'],'lineage_facts':facts,'automatic_scans':scans,
            'automatic_scan_observations':observed,'monitor':monitor,'physical':native,'post_browser_physical':final,
            'failed_normalize':failure,'browser':report,'source_unchanged':True,
            'cleanup_boundary':'owned NFS export/directory and disposable deployment; no business DELETE acceptance',
            **({'conflict_source':conflict_source} if buffer else {})}


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


def validate_trim_browser_raw(raw,evidence):
    if (raw.get('schema_version')!=fixture.TRIM_SCHEMA+'-browser-raw/v1' or raw.get('result')!='passed' or raw.get('stage')!='complete'
        or any(raw.get(k)!=evidence.get(k) for k in ('case','run_id','execution_id'))):raise SuiteError('trim browser raw incomplete or foreign')
    principal=positive(evidence['principal_id'],'trim browser principal')
    execution=obj(raw.get('execution'),'trim browser success')
    if execution.get('execution_id')!=evidence['execution_id'] or execution.get('status')!='success' or type(execution.get('triggered_by')) is not int or execution['triggered_by']!=principal:
        raise SuiteError('trim browser success identity differs')
    observations=array(raw.get('observations'),'trim browser observations')
    if len(observations)!=2 or {v.get('port') for v in observations}!={'result','details'}:raise SuiteError('trim browser requires both actual maps')
    for observation in observations:
        output=next(v for v in evidence['outputs'] if v['port']==observation['port'])
        if observation.get('output_item_id')!=output['item_id'] or observation.get('output')!=output['before'] or observation.get('sources')!=[{'item_id':v['id'],'item':v['before']} for v in evidence['sources']]:
            raise SuiteError('trim browser source or output Meta differs')
        validate_extension_graph(observation['graph'],evidence['sources'],output['item_id'],evidence['execution_id'])
        rendered=obj(observation.get('rendered'),'trim actual OL render')
        if rendered.get('projection')!='EPSG:3857' or len(array(rendered.get('features'),'trim OL features'))!=len(evidence['expected'][output['port']]) or type(rendered.get('painted_pixels')) is not int or rendered['painted_pixels']<=30:
            raise SuiteError('trim browser actual render is incomplete')
        if output['port']=='result' and (type(observation.get('removed_segment_pixels')) is not int or observation['removed_segment_pixels']!=0 or rendered.get('new_segment_pixels',0)<=0):
            raise SuiteError('trim browser removed segment or cut point witness differs')
    failures=array(raw.get('failures'),'trim raw failures')
    if len(failures)!=8 or [f.get('key') for f in failures]!=[s[0] for s in fixture.TRIM_FAILURES]:raise SuiteError('trim browser requires all eight failures')
    for observed,expected,spec in zip(failures,evidence['failures'],fixture.TRIM_FAILURES):
        if observed.get('execution_id')!=expected['execution_id']:raise SuiteError('trim raw failure identity differs')
        for value in (observed.get('execution'),observed.get('monitor'),obj(observed.get('monitor_page'),'trim Monitor page').get('execution')):
            validate_trim_failure(obj(value,'trim browser failure'),expected['execution_id'],principal,spec)
            if value.get('error_details')!=expected['error_details']:raise SuiteError('trim browser public error differs')


def validate_nodes_browser_raw(raw,evidence):
    return validate_point_outputs_browser_raw(raw,evidence,False)


def validate_vertices_browser_raw(raw,evidence):
    return validate_point_outputs_browser_raw(raw,evidence,True)


def validate_point_outputs_browser_raw(raw,evidence,vertices):
    schema=fixture.VERTICES_SCHEMA if vertices else fixture.NODES_SCHEMA
    ports=fixture.VERTICES_FAMILIES if vertices else fixture.NODES_PORTS
    specs=fixture.VERTICES_FAILURES if vertices else fixture.NODES_FAILURES
    validate_failure=validate_vertices_failure if vertices else validate_nodes_failure
    import base64,struct
    if (raw.get('schema_version')!=schema+'-browser-raw/v1' or raw.get('stage')!='complete' or raw.get('result')!='passed'
        or any(raw.get(k)!=evidence.get(k) for k in ('case','run_id','execution_id'))):raise SuiteError('nodes browser raw incomplete or foreign')
    principal=positive(evidence['principal_id'],'nodes browser principal')
    inputs=[('input.load_'+family,evidence['sources'][i]['locator']) for i,family in enumerate(ports)] if vertices else [('input.load_source',evidence['sources'][0]['locator'])]
    validate_saved_success(obj(raw.get('execution'),'Point browser execution'),inputs,{o['port']:o['locator'] for o in evidence['outputs']},len(ports))
    if raw['execution'].get('execution_id')!=evidence['execution_id'] or type(raw['execution'].get('triggered_by')) is not int or raw['execution']['triggered_by']!=principal:raise SuiteError('nodes browser execution identity differs')
    observations=array(raw.get('observations'),'nodes browser maps')
    if [v.get('port') for v in observations]!=list(ports):raise SuiteError('nodes browser requires exactly both Point maps')
    for index,(value,output) in enumerate(zip(observations,evidence['outputs'])):
        bound_sources=[evidence['sources'][index]] if vertices else evidence['sources']
        expected=evidence['expected'][output['port']];fields=list(expected[0]['properties'])
        if value.get('output_item_id')!=output['item_id'] or value.get('output')!=output['before'] or value.get('sources')!=[{'item_id':s['id'],'item':s['before']} for s in evidence['sources']]:raise SuiteError('nodes browser source/output Meta changed')
        if vertices:
            try:wire_preview=json.loads(value['preview_text'])
            except (KeyError,TypeError,ValueError) as exc:raise SuiteError('vertex raw preview wire missing') from exc
            if wire_preview!=value.get('preview'):raise SuiteError('vertex raw preview wire disagrees with parsed response')
        data=obj(obj(value.get('preview'),'nodes preview').get('data'),'nodes preview data')
        if (value['preview'].get('preview_type')!='table' or [data.get(k) for k in ('total','source_srid','source_crs','transform_status','preview_hint')]!=[len(expected),3857,'EPSG:3857','not_transformed','frontend_transform_required']
            or data.get('geometry_columns')!=[output['geometry_column']] or sorted(data.get('columns',[]))!=sorted([output['fid_column'],output['geometry_column'],*fields])):raise SuiteError('nodes actual preview metadata differs')
        rows=array(data.get('rows'),'nodes preview rows')
        if len(rows)!=len(expected):raise SuiteError('nodes preview rows missing')
        for i,(row,wanted) in enumerate(zip(rows,expected),1):
            if type(row.get(output['fid_column'])) is not int or row[output['fid_column']]!=i or any(type(row.get(k)) is not type(v) or row.get(k)!=v for k,v in wanted['properties'].items()):raise SuiteError('nodes preview properties/types/FID differ')
            try:
                encoded=row[output['geometry_column']];wire=base64.b64decode(encoded,validate=True);order='<' if wire[0]==1 else '>' if wire[0]==0 else None
                if order is None or len(wire)!=25 or struct.unpack(order+'II',wire[1:9])!=(0x20000001,3857) or base64.b64encode(wire).decode()!=encoded:raise ValueError('noncanonical Point EWKB')
                point=list(struct.unpack(order+'dd',wire[9:]))
            except (KeyError,ValueError,TypeError,struct.error) as e:raise SuiteError('nodes preview canonical EPSG3857 Point EWKB missing') from e
            if point!=wanted['geometry']['coordinates']:raise SuiteError('nodes preview exact original coordinate differs')
        (validate_vertices_graph if vertices else validate_nodes_graph)(value['graph'],bound_sources,output['item_id'],evidence['execution_id'])
        (validate_vertices_graph if vertices else validate_nodes_graph)({**value['graph'],'edges':value['lineage_rendered']['edges']},bound_sources,output['item_id'],evidence['execution_id'])
        rendered=obj(value.get('rendered'),'nodes actual Point layer');features=array(rendered.get('features'),'nodes OL features')
        if rendered.get('projection')!='EPSG:3857' or len(features)!=len(expected) or type(rendered.get('painted_pixels')) is not int or rendered['painted_pixels']<=30 or any(v.get('type')!='Point' for v in features):raise SuiteError('nodes Point rendering incomplete')
        # Browser independently asserts exact Mercator handoff coordinates; owner also preserves all repeated property rows.
        def key(props):return json.dumps(props,sort_keys=True,ensure_ascii=False)
        if sorted(key(v.get('properties')) for v in features)!=sorted(key(v['properties']) for v in expected):raise SuiteError('nodes OL original property rows differ')
    failures=array(raw.get('failures'),'nodes browser failure list')
    if [f.get('key') for f in failures]!=[s[0] for s in specs]:raise SuiteError('nodes browser must prove all nine failures')
    for record,failed,spec in zip(failures,evidence['failures'],specs):
        if record.get('execution_id')!=failed['execution_id']:raise SuiteError('nodes browser failed UUID differs')
        reads=array(record.get('observations'),'nodes failure bilingual owner pages')
        if len(reads)!=4 or {(v.get('owner'),v.get('locale')) for v in reads}!={(o,l) for o in ('develop','monitor') for l in ('zh-cn','en')}:raise SuiteError('nodes failure requires both owners and locales')
        for read in reads:
            value=obj(read.get('execution'),'nodes failed page raw');execution=value if read['owner']=='develop' else obj(value.get('execution'),'nodes Monitor raw')
            validate_failure(execution,failed['execution_id'],principal,spec)
            if vertices and read.get('clipboard')!=vertices_clipboard(failed,read['locale']):raise SuiteError('vertex exact clipboard differs')
            if execution.get('error_details')!=failed['error_details'] or not isinstance(read.get('clipboard'),str) or not read['clipboard'] or any(t in read['clipboard'] for t in (*DIAGNOSTIC_PRIVATE,'route_key','source_shape','节点业务几何')):raise SuiteError('nodes browser error/copy differs or leaks')


def validate_dissolve_browser_raw(raw,evidence):
    import base64,struct
    if(raw.get('schema_version')!=fixture.DISSOLVE_SCHEMA+'-browser-raw/v1'or raw.get('result')!='passed'or raw.get('stage')!='complete'or any(raw.get(k)!=evidence.get(k)for k in('case','run_id'))or raw.get('publication_requests')!=[]):raise SuiteError('dissolve browser raw incomplete or foreign')
    principal=positive(evidence['principal_id'],'dissolve browser User')
    successes=array(raw.get('successes'),'dissolve browser successes')
    if len(successes)!=2:raise SuiteError('dissolve needs both success execution pages')
    for got,want in zip(successes,evidence['successes']):
        if got!={'variant':want['variant'],'execution_id':want['execution_id'],'execution':want['execution']}:raise SuiteError('dissolve browser success execution differs')
    observations=array(raw.get('observations'),'dissolve map observations')
    if len(observations)!=4:raise SuiteError('dissolve needs exactly four actual maps')
    for observation,output in zip(observations,evidence['outputs']):
        expected=output['features'];fields=list(expected[0]['properties']);count=len(expected)
        if any(observation.get(k)!=output[k]for k in('name','port','execution_id'))or type(observation.get('output_item_id'))is not int or observation['output_item_id']!=output['item_id']or observation.get('output')!=output['before']or observation.get('sources')!=[{'item_id':s['id'],'item':s['before']}for s in evidence['sources']]:raise SuiteError('dissolve browser source/output identity differs')
        preview=obj(observation.get('preview'),'dissolve preview');data=obj(preview.get('data'),'dissolve preview data');rows=array(data.get('rows'),'dissolve preview rows')
        if preview.get('preview_type')!='table'or any(type(data.get(k))is not int for k in('page','total','source_srid'))or(data['page'],data['total'],data['source_srid'],data.get('source_crs'),data.get('transform_status'),data.get('preview_hint'))!=(1,count,3857,'EPSG:3857','not_transformed','frontend_transform_required')or data.get('geometry_columns')!=[output['geometry_column']]:raise SuiteError('dissolve preview count/CRS/geometry differs')
        if len(rows)!=count or len(array(data.get('columns'),'dissolve columns'))!=len(fields)+2 or set(data['columns'])!={output['fid_column'],output['geometry_column'],*fields}or[r.get(output['fid_column'])for r in rows]!=list(range(1,count+1))or any(type(r.get(output['fid_column']))is not int for r in rows):raise SuiteError('dissolve preview FID/columns/rows differ')
        for row,want in zip(rows,expected):
            if set(row)!=set(data['columns'])or{k:row.get(k)for k in fields}!=want['properties']or any(type(row.get(k))is not type(want['properties'][k])for k in fields):raise SuiteError('dissolve preview types/null/properties differ')
            try:
                encoded=row[output['geometry_column']];wire=base64.b64decode(encoded,validate=True);order='<'if wire[0]==1 else'>'if wire[0]==0 else None
                code,srid=struct.unpack(order+'II',wire[1:9])if order and len(wire)>9 else(0,0)
                if srid!=3857 or code!=(0x20000000|{'Polygon':3,'MultiPolygon':6}[want['geometry']['type']])or base64.b64encode(wire).decode()!=encoded:raise ValueError('wrong EWKB')
            except(ValueError,TypeError,IndexError,KeyError,struct.error)as exc:raise SuiteError('dissolve standard XY EWKB missing')from exc
        popup=obj(observation.get('popup'),'dissolve actual popup');properties=obj(popup.get('properties'),'dissolve popup properties')
        matches=[i for i,row in enumerate(expected)if row['properties']==properties and all(type(properties.get(k))is type(v)for k,v in row['properties'].items())]
        entries=array(popup.get('rows'),'dissolve popup DOM rows')
        wanted={k+':':('空值'if v is None else str(v).lower()if type(v)is bool else str(v))for k,v in properties.items()}
        wanted[output['fid_column']+':']=str(matches[0]+1)if matches else''
        if len(matches)!=1 or len(entries)!=len(wanted)or{r.get('key'):r.get('value')for r in entries}!=wanted:raise SuiteError('dissolve popup original properties/null/type differs')
        decoded=array(observation.get('decoded_rows'),'dissolve decoded rows')
        if decoded!=expected or any(type(a['properties'][k])is not type(b['properties'][k])for a,b in zip(decoded,expected)for k in fields):raise SuiteError('dissolve original geometry/properties differ')
        for graph in(observation.get('graph'),observation.get('lineage_page'),{**observation.get('lineage_page',{}),'edges':observation.get('lineage_rendered',{}).get('edges')}):validate_dissolve_graph(obj(graph,'dissolve graph'),evidence['sources'][0]['id'],output['item_id'],output['execution_id'])
        canvas=obj(observation.get('lineage_canvas'),'dissolve painted graph');labels=array(canvas.get('rows'),'dissolve painted labels')
        if not all(any(n in row.get('text','')for row in labels)for n in(evidence['sources'][0]['name'],output['name']))or len(array(canvas.get('paths'),'dissolve paths'))<1:raise SuiteError('dissolve lineage not painted')
        rendered=obj(observation.get('rendered'),'dissolve rendered map')
        if rendered.get('projection')!='EPSG:3857'or len(array(rendered.get('features'),'dissolve map features'))!=count or type(rendered.get('painted_pixels'))is not int or rendered['painted_pixels']<=30:raise SuiteError('dissolve map incomplete')
        script="""import assert from 'node:assert/strict';import{pathToFileURL}from'node:url';
const{webMercatorPreviewGeometry}=await import(pathToFileURL(process.argv[1]));const{decodePreviewEWKB}=await import(pathToFileURL(process.argv[2]));let text='';for await(const p of process.stdin)text+=p;
const v=JSON.parse(text),keys=rows=>rows.map(r=>JSON.stringify(r)).sort();assert.deepEqual(v.rows.map(r=>decodePreviewEWKB(r[v.column])),v.expected.map(r=>r.geometry));
assert.deepEqual(keys(v.rendered),keys(v.expected.map(r=>({properties:r.properties,geometry:webMercatorPreviewGeometry(r.geometry)}))));"""
        repo=Path(__file__).resolve().parents[2];result=subprocess.run(['node','--input-type=module','-e',script,str(repo/'manager/frontend/e2e/geometry-preview-coordinate.js'),str(repo/'manager/frontend/e2e/geometry-preview-support.js')],input=json.dumps({'rows':rows,'column':output['geometry_column'],'expected':expected,'rendered':rendered['features']}),capture_output=True,text=True,timeout=30)
        if result.returncode:raise SuiteError('dissolve raw EWKB/OL coordinates or properties differ')
    failures=array(raw.get('failures'),'dissolve browser failures')
    if[f.get('key')for f in failures]!=[s[0]for s in fixture.DISSOLVE_FAILURES]:raise SuiteError('dissolve browser needs eight exact failures')
    for record,failed,spec in zip(failures,evidence['failures'],fixture.DISSOLVE_FAILURES):
        reads=array(record.get('observations'),'dissolve owner/locale pages')
        if record.get('execution_id')!=failed['execution_id']or len(reads)!=4 or{(v.get('owner'),v.get('locale'))for v in reads}!={(o,l)for o in('develop','monitor')for l in('zh-cn','en')}:raise SuiteError('dissolve failure UUID/owner/locale differs')
        for read in reads:
            value=obj(read.get('raw'),'dissolve failure page');execution=value if read['owner']=='develop'else obj(value.get('execution'),'dissolve Monitor page');validate_dissolve_failure(execution,failed['execution_id'],principal,spec)
            if read.get('key')!=spec[0]or read.get('execution_id')!=failed['execution_id']or execution.get('error_details')!=failed['execution']['error_details']or not isinstance(read.get('clipboard'),str)or not read['clipboard']or any(t in read['clipboard']for t in(*DIAGNOSTIC_PRIVATE,'PRIVATE_DISSOLVE','parcel_key','parcel_shape',*fixture.DISSOLVE_SOURCES)):raise SuiteError('dissolve failure page/copy differs or leaks')
    validate_frozen_diagnostic_clipboards(failures)


def validate_frozen_diagnostic_clipboards(failures):
    script="""import assert from 'node:assert/strict';import{readFileSync}from'node:fs';let text='';for await(const p of process.stdin)text+=p;
const source=readFileSync(process.argv[1],'utf8'),start=source.indexOf('const inputDiagnosticText'),end=source.indexOf('async function assertInputDiagnosticPanel(',start);
const expected=new Function(source.slice(start,end)+';return inputDiagnosticPageExpectation')();
for(const f of JSON.parse(text))for(const r of f.observations)assert.equal(r.clipboard,expected(f.key,r.locale,f.execution_id).clipboard);"""
    result=subprocess.run(['node','--input-type=module','-e',script,str(Path(__file__).resolve().parents[2]/'console/frontend/e2e/online/vector-topology-persistence.spec.js')],input=json.dumps(failures),capture_output=True,text=True,timeout=30)
    if result.returncode:raise SuiteError('independent literal clipboard differs')


def validate_overlay_browser_raw(raw,evidence):return validate_mixed_overlay_browser_raw(raw,evidence,fixture.OVERLAY_CASE)


def validate_difference_browser_raw(raw,evidence):return validate_mixed_overlay_browser_raw(raw,evidence,fixture.DIFFERENCE_CASE)


def validate_mixed_overlay_browser_raw(raw,evidence,case):
    profile=mixed_overlay_profile(case);input_count=len(profile.get('input_nodes',('load_a','load_b')));count=profile['result_count'];validate_failure=profile['failure_validator']
    import base64,struct
    if (raw.get('schema_version')!=profile['schema']+'-browser-raw/v1' or raw.get('result')!='passed' or raw.get('stage')!='complete'
        or any(raw.get(k)!=evidence.get(k) for k in ('case','run_id','execution_id')) or raw.get('publication_requests')!=[]):raise SuiteError('intersection browser raw incomplete or foreign')
    principal=positive(evidence['principal_id'],'intersection browser principal');execution=obj(raw.get('execution'),'intersection browser execution')
    if execution.get('execution_id')!=evidence['execution_id'] or execution.get('status')!='success' or type(execution.get('triggered_by')) is not int or execution['triggered_by']!=principal or set(obj(execution.get('outputs'),'intersection browser output'))!={'save_result'}:raise SuiteError('intersection browser execution identity/status differs')
    observations=array(raw.get('observations'),'intersection raw observations')
    if len(observations)!=1:raise SuiteError('intersection requires one actual mixed map')
    observation=obj(observations[0],'intersection map observation');output=evidence['outputs'][0];expected=evidence['expected']['result']
    if observation.get('port')!='result' or observation.get('output_item_id')!=output['item_id'] or observation.get('output')!=output['before'] or observation.get('sources')!=[{'item_id':s['id'],'item':s['before']} for s in evidence['sources']]:raise SuiteError('intersection browser source/output Meta differs')
    preview=obj(observation.get('preview'),'intersection original preview');data=obj(preview.get('data'),'intersection preview data')
    if preview.get('preview_type')!='table' or any(type(data.get(k)) is not int for k in ('page','total','source_srid')) or (data['page'],data['total'],data['source_srid'],data.get('source_crs'),data.get('transform_status'),data.get('preview_hint'))!=(1,count,3857,'EPSG:3857','not_transformed','frontend_transform_required') or data.get('geometry_columns')!=[output['geometry_column']]:raise SuiteError('intersection browser mixed preview identity/CRS differs')
    fields=list(expected[0]['properties']);rows=array(data.get('rows'),'intersection preview rows')
    if len(rows)!=count or len(array(data.get('columns'),'intersection preview columns'))!=len(fields)+2 or set(data['columns'])!={output['fid_column'],output['geometry_column'],*fields} or [r.get(output['fid_column']) for r in rows]!=list(range(1,count+1)) or any(type(r.get(output['fid_column'])) is not int for r in rows):raise SuiteError('intersection preview FID/columns/rows differ')
    for row,wanted in zip(rows,expected):
        if set(row)!=set(data['columns']) or {k:row.get(k) for k in fields}!=wanted['properties'] or any(type(row.get(k)) is not type(wanted['properties'][k]) for k in fields):raise SuiteError('intersection preview property types/null/values differ')
        encoded=row.get(output['geometry_column'])
        try:
            wire=base64.b64decode(encoded,validate=True);order='<' if wire[0]==1 else '>' if wire[0]==0 else None
            code,srid=struct.unpack(order+'II',wire[1:9]) if order and len(wire)>9 else (0,0)
            if srid!=3857 or code!=(0x20000000|{'Point':1,'LineString':2,'Polygon':3,'MultiPoint':4,'MultiLineString':5,'MultiPolygon':6,'GeometryCollection':7}[wanted['geometry']['type']]) or base64.b64encode(wire).decode()!=encoded:raise ValueError('invalid EWKB')
        except (ValueError,TypeError,IndexError,struct.error) as e:raise SuiteError('intersection canonical XY EPSG3857 EWKB missing') from e
    decoded=array(observation.get('decoded_rows'),'intersection decoded original geometry')
    if decoded!=expected or any(type(a['properties'][k]) is not type(b['properties'][k]) for a,b in zip(decoded,expected) for k in fields):raise SuiteError('intersection decoded native coordinates/attributes differ')
    validate_proximity_graph(observation['graph'],evidence['sources'][:input_count],output['item_id'],evidence['execution_id'])
    validate_proximity_graph(observation['lineage_page'],evidence['sources'][:input_count],output['item_id'],evidence['execution_id'])
    validate_proximity_graph({**observation['lineage_page'],'edges':observation['lineage_rendered']['edges']},evidence['sources'][:input_count],output['item_id'],evidence['execution_id'])
    labels=array(obj(observation.get('lineage_canvas'),'intersection painted graph').get('rows'),'intersection painted labels')
    if not all(any(s['name'] in row.get('text','') for row in labels) for s in [*evidence['sources'][:input_count],output]) or len(array(observation['lineage_canvas'].get('paths'),'intersection painted paths'))<input_count:raise SuiteError('intersection source graph not painted')
    rendered=obj(observation.get('rendered'),'intersection OL data');features=array(rendered.get('features'),'intersection OL features')
    if profile.get('line_only'):
        if rendered.get('projection')!='EPSG:3857' or len(features)!=count or any(v.get('geometry',{}).get('type')!='MultiLineString'for v in features) or type(rendered.get('painted_pixels'))is not int or rendered['painted_pixels']<=30 or type(rendered.get('shared_edge_pixels'))is not int or rendered['shared_edge_pixels']<=0:raise SuiteError('adjacency line rendering incomplete')
    else:
        if rendered.get('projection')!='EPSG:3857' or len(features)!=count or sum(v.get('geometry',{}).get('type')=='GeometryCollection' for v in features)!=1 or any(type(rendered.get('collection_pixels',{}).get(k)) is not int or rendered['collection_pixels'][k]<=10 for k in ('point','line','polygon')):raise SuiteError('intersection mixed/collection rendering incomplete')
        collection=next(row['geometry']for row in expected if row['geometry']['type']=='GeometryCollection')
        members=array(rendered['collection_pixels'].get('members'),'mixed collection member pixels')
        kinds={'Point':'point','LineString':'line','Polygon':'polygon'}
        if [v.get('kind')for v in members]!=[kinds[v['type']]for v in collection['geometries']]or any(type(v.get('pixels'))is not int or v['pixels']<=10 for v in members):raise SuiteError('mixed collection individual members not painted')
    script="""import assert from 'node:assert/strict';import{pathToFileURL}from'node:url';
const{webMercatorPreviewGeometry}=await import(pathToFileURL(process.argv[1]));const{decodePreviewEWKB}=await import(pathToFileURL(process.argv[2]));let text='';for await(const part of process.stdin)text+=part;
const{actual,expected,preview,geometry_column}=JSON.parse(text),keys=rows=>rows.map(row=>JSON.stringify(row)).sort();
assert.deepEqual(preview.map(row=>decodePreviewEWKB(row[geometry_column])),expected.map(row=>row.geometry));
assert.deepEqual(keys(actual),keys(expected.map(row=>({properties:row.properties,geometry:webMercatorPreviewGeometry(row.geometry)}))));"""
    result=subprocess.run(['node','--input-type=module','-e',script,str(Path(__file__).resolve().parents[2]/'manager/frontend/e2e/geometry-preview-coordinate.js'),str(Path(__file__).resolve().parents[2]/'manager/frontend/e2e/geometry-preview-support.js')],input=json.dumps({'actual':features,'expected':expected,'preview':rows,'geometry_column':output['geometry_column']}),capture_output=True,text=True,timeout=30)
    if result.returncode:raise SuiteError('intersection raw OL coordinates/properties differ from exact independent projection')
    failures=array(raw.get('failures'),'intersection browser failures')
    if [f.get('key') for f in failures]!=[s[0] for s in profile['failures']]:raise SuiteError('intersection browser needs four distinct failure causes')
    for record,failed,spec in zip(failures,evidence['failures'],profile['failures']):
        if record.get('execution_id')!=failed['execution_id']:raise SuiteError('intersection browser failure UUID differs')
        reads=array(record.get('observations'),'intersection owner/language pages')
        if len(reads)!=4 or {(v.get('owner'),v.get('locale')) for v in reads}!={(owner,locale) for owner in ('develop','monitor') for locale in ('zh-cn','en')}:raise SuiteError('intersection needs both owners and locales')
        for read in reads:
            value=obj(read.get('raw'),'intersection real failure response');execution=value if read['owner']=='develop' else obj(value.get('execution'),'intersection Monitor page')
            validate_failure(execution,failed['execution_id'],principal,spec)
            if execution.get('error_details')!=failed['error_details'] or not isinstance(read.get('clipboard'),str) or not read['clipboard'] or any(t in read['clipboard'] for t in (*DIAGNOSTIC_PRIVATE,'PRIVATE_OVERLAY','PRIVATE_DIFFERENCE','a_key','b_key','a_shape','b_shape','a_note',*profile['sources'])):raise SuiteError('intersection browser diagnostic/copy differs or leaks')


    validate_frozen_diagnostic_clipboards(failures)


def browser(repository, env, evidence):
    path = Path(env['ADDP_ONLINE_ARTIFACT_DIR']) / 'topology-browser.json'
    path.unlink(missing_ok=True)
    case=fixture.topology_case(env)
    # Playwright prefixes file/project names in grep titles; match the exact test-name suffix.
    pattern=fixture.network_profile(case)['title']+'$' if case in fixture.INDEPENDENT_CASES else '同一普通User核对构面、修复、严格吸附、面叠面缝悬点线叠自叠线交自交及打断连通分组链路合并地图及塌缩失败$'
    result = subprocess.run(['npm', 'exec', '--', 'playwright', 'test', 'e2e/online/vector-topology-persistence.spec.js',
        '--config=playwright.online.config.js','--grep',pattern], cwd=repository/'console/frontend',
        env=dict(env, ADDP_ONLINE_TOPOLOGY_EVIDENCE=json.dumps(evidence)), stdout=sys.stderr, stderr=sys.stderr, timeout=240)
    if result.returncode or not path.is_file():
        raise SuiteError('topology Console evidence missing or failed')
    report = obj(json.loads(path.read_text()), 'browser evidence')
    schema=fixture.network_profile(case)['schema']+'-browser/v1' if case in fixture.INDEPENDENT_CASES else 'addp.topology-browser/v14'
    if case in (fixture.DIAGNOSTIC_CASE,fixture.RINGS_DIAGNOSTIC_CASE):
        specs=input_diagnostic_specs(case);count=len(specs)*4
        observations=array(report.get('observations'),'diagnostic browser observations')
        if len(observations)!=count or {(v.get('key'),v.get('owner'),v.get('locale'),v.get('execution_id')) for v in observations}!={
            (run['key'],owner,locale,run['execution_id']) for run in evidence['executions'] for owner in ('develop','monitor') for locale in ('zh-cn','en')}:
            raise SuiteError('diagnostic browser requires exact sixteen same-UUID observations')
        for value in observations:
            spec=next(spec for spec in specs if spec[0]==value['key'])
            raw=obj(value.get('raw'),'raw browser owner response')
            validate_input_failure(raw if value['owner']=='develop' else obj(raw.get('execution'),'raw Monitor execution'),spec,value['execution_id'])
            if not isinstance(value.get('clipboard'),str) or not value['clipboard'] or any(token in value['clipboard'] for token in DIAGNOSTIC_PRIVATE):
                raise SuiteError('diagnostic browser clipboard is missing or unsafe')
        binding={key:value for key,value in report.items() if key!='observations'}
    else:binding=report
    if binding != {**evidence, 'schema_version': schema, 'result': 'passed'}:
        raise SuiteError('browser report does not bind the exact run, User and outputs')
    if case==fixture.ADJACENCY_CASE:
        raw=obj(json.loads((path.parent/'topology-adjacency-browser-raw.json').read_text()),'adjacency browser raw');validate_mixed_overlay_browser_raw(raw,evidence,case)
    if case==fixture.DIFFERENCE_CASE:
        validate_difference_browser_raw(obj(json.loads((path.parent/'topology-difference-browser-raw.json').read_text()),'difference browser raw'),evidence)
    if case==fixture.DISSOLVE_CASE:
        validate_dissolve_browser_raw(obj(json.loads((path.parent/'topology-dissolve-browser-raw.json').read_text()),'dissolve browser raw'),evidence)
    if case==fixture.OVERLAY_CASE:
        validate_overlay_browser_raw(obj(json.loads((path.parent/'topology-overlay-browser-raw.json').read_text()),'intersection browser raw'),evidence)
    if case==fixture.VERTICES_CASE:
        validate_vertices_browser_raw(obj(json.loads((path.parent/'topology-vertices-browser-raw.json').read_text()),'vertices browser raw'),evidence)
    if case==fixture.NODES_CASE:
        validate_nodes_browser_raw(obj(json.loads((path.parent/'topology-nodes-browser-raw.json').read_text()),'nodes browser raw'),evidence)
    if case==fixture.TRIM_CASE:
        validate_trim_browser_raw(obj(json.loads((path.parent/'topology-trim-browser-raw.json').read_text()),'trim browser raw'),evidence)
    if case==fixture.RINGS_CASE:
        raw=obj(json.loads((path.parent/'topology-rings-browser-raw.json').read_text()),'rings browser raw')
        if (raw.get('stage')!='complete' or raw.get('result')!='passed' or any(raw.get(k)!=evidence.get(k) for k in ('case','run_id','execution_id'))):raise SuiteError('rings browser raw incomplete or foreign')
        validate_rings_preview_text(raw['preview_text'])
    return report


def run_scenario(repository, env, client, physical_runner=physical, browser_runner=browser,
                 diagnostic_runner=collapsed_snap_diagnostic):
    if env.get('ADDP_ONLINE_TOPOLOGY_LINEAGE_PERMISSIONS') == '1':
        if fixture.topology_case(env) != fixture.NODES_CASE:
            raise SuiteError('the lineage permission acceptance requires the original nodes fixture')
        return run_nodes_lineage_permissions(repository, env, client, physical_runner)
    if fixture.topology_case(env)==fixture.VERTICES_CASE:
        return run_vertices_scenario(repository,env,client,physical_runner,browser_runner)
    if fixture.topology_case(env)==fixture.ADJACENCY_CASE:return run_adjacency_scenario(repository,env,client,physical_runner,browser_runner)
    if fixture.topology_case(env)==fixture.DIFFERENCE_CASE:
        return run_difference_scenario(repository,env,client,physical_runner,browser_runner)
    if fixture.topology_case(env)==fixture.DISSOLVE_CASE:
        return run_dissolve_scenario(repository,env,client,physical_runner,browser_runner)
    if fixture.topology_case(env)==fixture.OVERLAY_CASE:
        return run_overlay_scenario(repository,env,client,physical_runner,browser_runner)
    if fixture.topology_case(env)==fixture.NODES_CASE:
        return run_nodes_scenario(repository,env,client,physical_runner,browser_runner)
    if fixture.topology_case(env)==fixture.TRIM_CASE:
        return run_trim_scenario(repository,env,client,physical_runner,browser_runner,diagnostic_runner)
    if fixture.topology_case(env)==fixture.RINGS_CASE:
        return run_rings_scenario(repository,env,client,physical_runner,browser_runner,diagnostic_runner)
    if fixture.topology_case(env)==fixture.ENDPOINT_CASE:
        return run_endpoint_scenario(repository,env,client,physical_runner,browser_runner,diagnostic_runner)
    if fixture.topology_case(env)==fixture.EXTENSION_CASE:
        return run_extension_scenario(repository,env,client,physical_runner,browser_runner,diagnostic_runner)
    if fixture.topology_case(env)==fixture.PROXIMITY_CASE:
        return run_proximity_scenario(repository,env,client,physical_runner,browser_runner)
    if fixture.topology_case(env) in (fixture.DIAGNOSTIC_CASE,fixture.RINGS_DIAGNOSTIC_CASE):
        return run_diagnostic_scenario(repository,env,client,physical_runner,browser_runner)
    if fixture.topology_case(env) in fixture.INDEPENDENT_CASES:
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

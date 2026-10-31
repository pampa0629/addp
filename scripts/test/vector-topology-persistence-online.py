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
DIAGNOSTIC_PRIVATE = ('PRIVATE_DIAGNOSTIC_NODE','PRIVATE_DIAGNOSTIC_SOURCE_VALUE','Traceback','site-packages','/tmp/')


def diagnostic_definition(key, sources, parent):
    spec=next((value for value in DIAGNOSTIC_SPECS if value[0]==key),None)
    if spec is None:raise SuiteError('unknown operator diagnostic example')
    node='PRIVATE_DIAGNOSTIC_NODE'
    primary=sources[3] if key=='field-conflict' else sources[0]
    tasks=[{'id':node,'operator':spec[1],'depends_on':['source'], 'params':{'input_gdf':{'$ref':'source'}}},
           {'id':'source','operator':'load','depends_on':[],'params':{'locator':primary}}]
    params=tasks[0]['params']
    if spec[1]=='batch_buffer':params.update(distances=[True] if key=='number-array' else [1],resolution=1)
    else:
        tasks[0]['depends_on'].append('reference')
        tasks.append({'id':'reference','operator':'load','depends_on':[], 'params':{'locator':sources[2] if key=='crs-mismatch' else sources[1]}})
        params['target_geom' if key=='crs-mismatch' else 'reference_gdf']={'$ref':'reference'}
        if key=='positive-number':params.update(tolerance=0,id_field='route_key',reference_id_field='route_key')
    tasks.append({'id':'save','operator':'save','depends_on':[node],'params':{'input_df':{'$ref':node},
                  'target_parent_locator':parent,'target_name':key+'.gpkg','mode':'fail'}})
    return {'tasks':tasks}


def expected_input_diagnostic(spec):
    _,operator,reason,parameter,field=spec
    return {'schema_version':'addp.execution-failure/v3','stage':'execution','reason':reason,
            'operator':{'node_index':1,'operator_id':operator,**({'parameter':parameter} if parameter else {}),**({'field':field} if field else {})}}


def validate_input_failure(execution, spec, identifier):
    if execution.get('execution_id')!=identifier or execution.get('status')!='failed' or execution.get('outputs'):
        raise SuiteError('operator parameter failure identity/status/output differs')
    details=obj(execution.get('error_details'),'operator error details')
    if (details.get('code')!='develop.workflow.invalid_params' or details.get('diagnostic')!=expected_input_diagnostic(spec)
        or type(details.get('diagnostic',{}).get('operator',{}).get('node_index')) is not int
        or set(details)-{'code','category','message','diagnostic'}):
        raise SuiteError('operator parameter failure controlled public diagnostic differs')
    if any(value in json.dumps(details,ensure_ascii=False) for value in DIAGNOSTIC_PRIVATE):
        raise SuiteError('operator public diagnostic leaked private runtime context')
    metadata=obj(execution.get('metadata'),'operator failure metadata')
    facts=metadata.get('lineage_facts') or {}
    result=metadata.get('result') or {}
    if facts.get('outputs') or result.get('meta_scan_runs'):
        raise SuiteError('operator parameter failure published facts or automatic scans')
    return details


def run_diagnostic_scenario(repository, env, client, physical_runner, browser_runner):
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
    for name in fixture.DIAGNOSTIC_SOURCES:
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
    if native.get('exact_inventory_verified') is not True or set(native.get('sources',{}))!=set(fixture.DIAGNOSTIC_SOURCES) or native.get('targets')!={}:raise SuiteError('operator diagnostic source physical inventory differs')
    runs=[];directory=Path(env['ADDP_ONLINE_ARTIFACT_DIR'])
    def checkpoint():
        (directory/'topology-input-diagnostics-checkpoint.json').write_text(json.dumps({'schema_version':fixture.DIAGNOSTIC_SCHEMA+'-checkpoint/v1',
            'result':'incomplete','case':fixture.DIAGNOSTIC_CASE,'run_id':env['ADDP_ONLINE_TEST_RUN_ID'],'identity':identity,'sources':sources,
            'initial_scan_inventory':inventory,'physical':native,'executions':runs},ensure_ascii=False,indent=2)+'\n')
    checkpoint()
    for spec in DIAGNOSTIC_SPECS:
        definition=diagnostic_definition(spec[0],[value['locator'] for value in sources],f'addp://engine/{engine}/path/outputs?type=directory')
        identifier=raster.submit(client,positive(runtimes[0]['id'],'Runtime'),definition)
        execution=raster.wait_execution(client,'develop',identifier,remaining(),expected='failed')
        details=validate_input_failure(execution,spec,identifier)
        if type(execution.get('triggered_by')) is not int or execution['triggered_by']!=int(identity['principal_id']):
            raise SuiteError('operator diagnostic public execution actor differs')
        monitor=obj(client.request('GET',f'/api/v1/monitor/executions/by-execution-id/{identifier}',(200,)).payload,'operator Monitor')
        if monitor.get('execution_id')!=identifier or monitor.get('module')!='develop' or monitor.get('status')!='failed' or monitor.get('error_details')!=details or type(monitor.get('triggered_by')) is not int or monitor['triggered_by']!=int(identity['principal_id']):
            raise SuiteError('operator Monitor differs from public Develop diagnostic')
        graphs=[]
        for source in sources:
            graph=obj(client.request('GET',f'/api/v1/meta/lineage/graph?subject_kind=data_item&item_id={source["id"]}&direction=downstream',(200,)).payload,'failure lineage')
            if graph.get('truncated') is True or any(edge.get('relation_kind')=='derive' and edge.get('evidence',{}).get('execution_id')==identifier for edge in array(graph.get('edges'),'failure edges')):
                raise SuiteError('operator failure published derive or incomplete graph')
            graphs.append({'source_item_id':source['id'],'graph':graph})
        runs.append({'key':spec[0],'execution_id':identifier,'definition':definition,'expected_diagnostic':expected_input_diagnostic(spec),
                     'execution':execution,'monitor':monitor,'source_lineage':graphs})
        if scan_ids(client)!=inventory or physical_runner(repository,env)!=native:raise SuiteError('operator failure changed scans or files')
        checkpoint()
    if len({value['execution_id'] for value in runs})!=4:raise SuiteError('operator diagnostics require four distinct executions')
    evidence={'case':fixture.DIAGNOSTIC_CASE,'run_id':env['ADDP_ONLINE_TEST_RUN_ID'],**identity,'executions':runs}
    remaining();report=browser_runner(repository,env,evidence);remaining()
    final=physical_runner(repository,env)
    if final!=native or scan_ids(client)!=inventory:raise SuiteError('operator diagnostic browser changed physical files or scans')
    for source in sources:
        if client.request('GET',f'/api/v1/meta/items/{source["id"]}',(200,)).payload!=source['before']:raise SuiteError('operator diagnostic source Meta changed')
    return {'schema_version':fixture.DIAGNOSTIC_SCHEMA+'/v1','suite':SUITE,'case':fixture.DIAGNOSTIC_CASE,'result':'passed',
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


def run_extension_scenario(repository,env,client,physical_runner,browser_runner,diagnostic_runner):
    return run_line_edit_scenario(repository,env,client,physical_runner,browser_runner,diagnostic_runner,fixture.EXTENSION_CASE)


def run_endpoint_scenario(repository,env,client,physical_runner,browser_runner,diagnostic_runner):
    return run_line_edit_scenario(repository,env,client,physical_runner,browser_runner,diagnostic_runner,fixture.ENDPOINT_CASE)


def run_line_edit_scenario(repository,env,client,physical_runner,browser_runner,diagnostic_runner,case):
    endpoint=case==fixture.ENDPOINT_CASE
    names=fixture.ENDPOINT_SOURCES if endpoint else fixture.EXTENSION_SOURCES
    kinds=fixture.ENDPOINT_KINDS if endpoint else ('source','reference','failure_reference')
    ports=fixture.ENDPOINT_PORTS if endpoint else fixture.EXTENSION_PORTS
    targets_names=fixture.ENDPOINT_TARGETS if endpoint else fixture.EXTENSION_TARGETS
    schema=fixture.network_profile(case)['schema'];prefix='endpoint' if endpoint else 'extension'
    expected=lambda kind:fixture.line_edit_features(case,kind)
    definition=endpoint_definition if endpoint else extension_definition
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
    specs=fixture.ENDPOINT_FAILURES if endpoint else (('unrepresentable_first_intersection',fixture.EXTENSION_FAILURE),)
    checkpoint['failures']=failures
    for key,reason in specs:
        failed_workflow=definition([s['locator'] for s in sources],parent,**({'failure':key} if endpoint else {'failed':True}))
        failed_id=raster.submit(client,runtime,failed_workflow);failed=raster.wait_execution(client,'develop',failed_id,remaining(),expected='failed')
        validate_extension_failure(failed,failed_id,principal)
        if obj(obj(failed.get('execution_config'),'failed config').get('content'),'failed content').get('workflow_definition')!=failed_workflow:raise SuiteError('failed frozen DAG differs')
        diagnostic=diagnostic_runner(repository,env,failed_id,timeout=min(30,remaining()),task='snap' if endpoint else 'extend',reason=reason,cause=key)
        if diagnostic!={'execution_id':failed_id,'cause':key,'source':'develop_runtime_log'}:raise SuiteError('exact failure diagnostic binding differs')
        failed_monitor=obj(client.request('GET',f'/api/v1/monitor/executions/by-execution-id/{failed_id}',(200,)).payload,'failed Monitor')
        if failed_monitor.get('execution_id')!=failed_id or failed_monitor.get('status')!='failed' or failed_monitor.get('module')!='develop' or type(failed_monitor.get('triggered_by')) is not int or failed_monitor['triggered_by']!=principal:raise SuiteError('failure Monitor identity differs')
        failure={'key':key,'execution_id':failed_id,'definition':failed_workflow,'execution':failed,'monitor':failed_monitor,'diagnostic':diagnostic,'no_publication_or_scan':True}
        if endpoint:
            failure['source_lineage']=[]
            for source in (sources[2],sources[3+[k for k,_ in fixture.ENDPOINT_FAILURES].index(key)]):
                graph=obj(client.request('GET',f'/api/v1/meta/lineage/graph?subject_kind=data_item&item_id={source["id"]}&direction=downstream',(200,)).payload,'failed source graph')
                if graph.get('truncated') is not False or any(e.get('relation_kind')=='derive' and e.get('evidence',{}).get('execution_id')==failed_id for e in array(graph.get('edges'),'failed edges')):raise SuiteError('endpoint failure published derive or incomplete graph')
                failure['source_lineage'].append({'source_item_id':source['id'],'graph':graph})
        failures.append(failure)
        checkpoint.update(stage='failure_'+key+'_verified',raw_lineage=graphs)
        path.write_text(json.dumps(checkpoint,ensure_ascii=False,indent=2)+'\n')
        if scan_ids(client)!=expected_scans or physical_runner(repository,env)!=native:raise SuiteError('failure changed physical files or scans')
    if len({f['execution_id'] for f in failures})!=len(specs):raise SuiteError('failure executions must be distinct')
    checkpoint.update(stage='before_browser',raw_lineage=graphs)
    if not endpoint:checkpoint['failure']=failures[0]
    path.write_text(json.dumps(checkpoint,ensure_ascii=False,indent=2)+'\n')
    evidence={'case':case,'run_id':env['ADDP_ONLINE_TEST_RUN_ID'],**identity,'execution_id':identifier,'sources':sources,'outputs':outputs,
        'expected':{port:expected(port) for port in ports}}
    if endpoint:evidence['failures']=[{'key':f['key'],'execution_id':f['execution_id']} for f in failures]
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
    if len(edges)!=2 or {(e.get('source',{}).get('item_id'),e.get('target',{}).get('item_id')) for e in edges}!={(s['id'],target) for s in sources}:
        raise SuiteError('proximity requires exactly two current-execution active source edges')
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


def validate_saved_success(execution,bindings,targets,count):
    outputs=obj(execution.get('outputs'),'normalize stable outputs')
    expected={'save_'+port:{'resource':{'locator':locator,'type':'file','write_mode':'create'}} for port,locator in targets.items()}
    if execution.get('status')!='success' or outputs!=expected:raise SuiteError('normalization must publish exactly the selected named ports')
    metadata=obj(execution.get('metadata'),'normalize metadata');facts=obj(metadata.get('lineage_facts'),'normalize facts')
    inputs=array(facts.get('inputs'),'normalize inputs');produced=array(facts.get('outputs'),'normalize outputs')
    if (facts.get('schema_version')!='addp.lineage-facts/v1' or
        [(x.get('port'),x.get('locator')) for x in inputs]!=bindings or
        len(produced)!=count or {x.get('locator') for x in produced}!=set(targets.values()) or
        any(x.get('write_mode')!='create' for x in produced)):
        raise SuiteError('normalization source port or selected create facts differ')
    scans=array(obj(metadata.get('result'),'normalize result').get('meta_scan_runs'),'normalize scans')
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
    diagnostic=diagnostic_runner(repository,env,failed_id,task='chains' if composition else 'normal',
        reason='源图层的列名 distance 与多距缓冲输出列冲突；请先手动重命名源列，再执行多距缓冲。' if buffer else 'id_field must explicitly name one existing non-geometry scalar column',
        cause='distance_conflict' if buffer else 'invalid_chain_id_field' if composition else 'invalid_normalize_id_field')
    failed_monitor=obj(client.request('GET',f'/api/v1/monitor/executions/by-execution-id/{failed_id}',(200,)).payload,'failed normalize Monitor')
    if failed_monitor.get('status')!='failed' or failed_monitor.get('module')!='develop':raise SuiteError('normalization failure Monitor differs')
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
    if case==fixture.DIAGNOSTIC_CASE:
        observations=array(report.get('observations'),'diagnostic browser observations')
        if len(observations)!=16 or {(v.get('key'),v.get('owner'),v.get('locale'),v.get('execution_id')) for v in observations}!={
            (run['key'],owner,locale,run['execution_id']) for run in evidence['executions'] for owner in ('develop','monitor') for locale in ('zh-cn','en')}:
            raise SuiteError('diagnostic browser requires exact sixteen same-UUID observations')
        for value in observations:
            spec=next(spec for spec in DIAGNOSTIC_SPECS if spec[0]==value['key'])
            raw=obj(value.get('raw'),'raw browser owner response')
            validate_input_failure(raw if value['owner']=='develop' else obj(raw.get('execution'),'raw Monitor execution'),spec,value['execution_id'])
            if not isinstance(value.get('clipboard'),str) or not value['clipboard'] or any(token in value['clipboard'] for token in DIAGNOSTIC_PRIVATE):
                raise SuiteError('diagnostic browser clipboard is missing or unsafe')
        binding={key:value for key,value in report.items() if key!='observations'}
    else:binding=report
    if binding != {**evidence, 'schema_version': schema, 'result': 'passed'}:
        raise SuiteError('browser report does not bind the exact run, User and outputs')
    if case==fixture.RINGS_CASE:
        raw=obj(json.loads((path.parent/'topology-rings-browser-raw.json').read_text()),'rings browser raw')
        if (raw.get('stage')!='complete' or raw.get('result')!='passed' or any(raw.get(k)!=evidence.get(k) for k in ('case','run_id','execution_id'))):raise SuiteError('rings browser raw incomplete or foreign')
        validate_rings_preview_text(raw['preview_text'])
    return report


def run_scenario(repository, env, client, physical_runner=physical, browser_runner=browser,
                 diagnostic_runner=collapsed_snap_diagnostic):
    if fixture.topology_case(env)==fixture.RINGS_CASE:
        return run_rings_scenario(repository,env,client,physical_runner,browser_runner,diagnostic_runner)
    if fixture.topology_case(env)==fixture.ENDPOINT_CASE:
        return run_endpoint_scenario(repository,env,client,physical_runner,browser_runner,diagnostic_runner)
    if fixture.topology_case(env)==fixture.EXTENSION_CASE:
        return run_extension_scenario(repository,env,client,physical_runner,browser_runner,diagnostic_runner)
    if fixture.topology_case(env)==fixture.PROXIMITY_CASE:
        return run_proximity_scenario(repository,env,client,physical_runner,browser_runner)
    if fixture.topology_case(env)==fixture.DIAGNOSTIC_CASE:
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

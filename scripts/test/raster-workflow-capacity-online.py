#!/usr/bin/env python3
"""Manual full-scene T4 capacity evidence through authoritative policy and Develop."""
from __future__ import annotations
import importlib
import hashlib
from datetime import datetime, timedelta, timezone
import json
import math
import os
import re
import subprocess
from pathlib import Path
import sys
import threading
import time
import urllib.parse

scene=importlib.import_module('scripts.test.raster-workflow-online')
fixture=importlib.import_module('business.scripts.online-raster-capacity-fixture')
registration=importlib.import_module('scripts.test.online-engine-registration')
SUITE=fixture.SUITE
RUNTIME='addp-raster-capacity-runtime'
CACHE_VALUES=(64,128,256)
REPETITIONS=2


def workflow(locator,target_engine,name):
    ref=lambda node:{'$ref':node,'port':'default'}
    return {'tasks':[
        {'id':'load','operator':'raster_load','depends_on':[],'params':{'locator':locator}},
        {'id':'stats','operator':'raster_statistics','depends_on':['load'],'params':{'input_raster':ref('load')}},
        {'id':'hist','operator':'raster_histogram','depends_on':['load','stats'],'params':{'input_raster':ref('load')}},
        {'id':'math','operator':'raster_band_math','depends_on':['load','hist'],'params':{'input_raster':ref('load'),'expression':'(b1+b2+b3+b4)*2+1'}},
        {'id':'resample','operator':'raster_resample','depends_on':['math'],'params':{'input_raster':ref('math'),'size':[5490,5490],'resampling':'average'}},
        {'id':'save','operator':'raster_save','depends_on':['resample'],'params':{'input_raster':ref('resample'),
            'target_parent_locator':f'addp://engine/{target_engine}/path/raster-target?type=bucket','target_name':name,
            'write_mode':'create','profile':'cog','blocksize':512}}]}


def expectation():
    return {'width':5490,'height':5490,'band_count':1,'source_crs':'EPSG:32617','extent_srid':32617,
            'transform':[600000,20,0,4000020,0,-20],'extent':[600000,3890220,709800,4000020],'band_nodata':None}


def owned_runtime(image):
    container=json.loads(fixture.shared.command(['docker','inspect',RUNTIME]))[0]
    if (container['Config']['Labels'].get('com.addp.online-runtime')!=SUITE
        or container['Config']['Labels'].get('com.addp.online-run-id')!=os.environ['ADDP_ONLINE_TEST_RUN_ID']
        or container['Image']!=image or container['Config']['Cmd']!=['sh','container_entrypoint.sh']):
        raise scene.SuiteError('capacity Runtime ownership/image/default entry mismatch')
    return container


def restart_runtime(image):
    old=owned_runtime(image)
    if not old['State']['Running']: raise scene.SuiteError('capacity Runtime stopped unexpectedly')
    fixture.shared.command(['docker','rm','-fv',RUNTIME])
    fixture.shared.command(['bash','scripts/test/online-hosted-raster-capacity-gate.sh','--start-runtime'])
    current=owned_runtime(image)
    if current['HostConfig']['Memory']!=2*(1<<30) or current['HostConfig']['MemorySwap']!=2*(1<<30) or current['HostConfig']['NanoCpus']!=4_000_000_000:
        raise scene.SuiteError('capacity container limits differ from the registered profile')
    return current


from scripts.utils import capacity_resources


def _capacity_read(function, *args, **kwargs):
    try:
        return function(*args, **kwargs)
    except capacity_resources.ResourceError as error:
        raise scene.SuiteError(str(error)) from error


def cgroup_directory(pid, proc=Path('/proc'), root=Path('/sys/fs/cgroup')):
    return _capacity_read(capacity_resources.cgroup_directory, pid, proc, root)


def read_metrics(directory, proc=Path('/proc')):
    return _capacity_read(capacity_resources.read_metrics, directory, proc)


class Sampler:
    def __init__(self,directory,platform,path):
        self.directory,self.platform,self.path=directory,platform,path
        self.samples=[];self.error=None;self.event=threading.Event()
        self.thread=threading.Thread(target=self.run,daemon=True)
    def sample(self):
        metrics=read_metrics(self.directory)
        sample={'elapsed_seconds':time.monotonic()-self.started,**metrics,'runtime':None}
        self.samples.append(sample)
        view=self.platform.request('GET',self.path,(200,)).payload
        runtime=view.get('runtime')
        if not isinstance(runtime,dict): raise scene.SuiteError('capacity runtime observation is missing')
        sample['runtime']=runtime
    def run(self):
        while not self.event.wait(.25):
            try:self.sample()
            except BaseException as error:self.error=error;return
    def start(self):
        self.started=time.monotonic();self.before=read_metrics(self.directory);self.thread.start()
    def finish(self):
        self.event.set();self.thread.join(35)
        try:
            if self.thread.is_alive(): raise scene.SuiteError('capacity sampler did not stop')
            if self.error:
                reason=str(self.error) if isinstance(self.error,scene.SuiteError) else type(self.error).__name__
                raise scene.SuiteError('capacity sampler failed: '+reason) from self.error
            self.sample()
        finally:
            self.after=read_metrics(self.directory)
        return self.samples


def wait_applied(platform,path,cache,running,version):
    deadline=time.monotonic()+90
    while time.monotonic()<deadline:
        runtime=platform.request('GET',path,(200,)).payload.get('runtime')
        if (isinstance(runtime,dict) and runtime.get('enabled') is True
            and runtime.get('cache_mib')==cache and runtime.get('applied_version')==version
            and runtime.get('pending_restart') is False and runtime.get('running_limit')==running
            and runtime.get('waiting_limit')==2):
            return
        time.sleep(1)
    raise scene.SuiteError('capacity policy did not become ready before measurement')


def matrix():
    for repetition in range(1,REPETITIONS+1):
        caches=CACHE_VALUES if repetition%2 else tuple(reversed(CACHE_VALUES))
        limits=(1,2) if repetition%2 else (2,1)
        for cache in caches:
            for running in limits:
                yield repetition,cache,running


def validate_measurement(samples,before,after,cache,running,version,*,submitted_tasks=2):
    if not samples or any(sample['oom'] or sample['oom_kill'] for sample in [before,*samples,after]):
        raise scene.SuiteError('capacity OOM or missing samples')
    # A fresh Runtime loads policy lazily on its first raster submission.
    # Accept only the initial, empty/uninitialized prefix; a later pause is a failure.
    applied=[]
    for sample in samples:
        runtime=sample.get('runtime')
        if (not applied and isinstance(runtime,dict) and runtime.get('enabled') is False
            and runtime.get('cache_mib') is None and runtime.get('applied_version') is None
            and runtime.get('running')==0 and runtime.get('waiting')==0):
            continue
        if not isinstance(runtime,dict) or runtime.get('enabled') is not True:
            raise scene.SuiteError('capacity Runtime paused or lost policy after initialization')
        applied.append(runtime)
    if not applied or any(r.get('cache_mib')!=cache or r.get('pending_restart') is not False or r.get('applied_version')!=version for r in applied):
        raise scene.SuiteError('capacity saved/applied cache and version disagree')
    if any(type(r.get('running')) is not int or not 0<=r['running']<=running
        or type(r.get('waiting')) is not int or not 0<=r['waiting']<=2
        or r.get('running_limit')!=running or r.get('waiting_limit')!=2 for r in applied):
        raise scene.SuiteError('capacity admission exceeded the configured limit')
    if max(r.get('running',0) for r in applied)!=running:
        raise scene.SuiteError('capacity did not exercise the requested concurrency')
    if running==1 and submitted_tasks>1 and max(r.get('waiting',0) for r in applied)<1:
        raise scene.SuiteError('capacity serial admission did not queue the second task')
    if (after['memory_peak_bytes']<=0 or after['cpu_usage_usec']<before['cpu_usage_usec']):
        raise scene.SuiteError('capacity metrics are invalid or did not reset')
    final=applied[-1]
    if final.get('running')!=0 or final.get('waiting')!=0:
        raise scene.SuiteError('capacity admission did not drain')
    if (final.get('memory_limit_bytes')!=2*(1<<30) or not math.isclose(final.get('effective_cpu',0),4)
        or final.get('advice')!={'running':2,'waiting':2,'cache_mib':64,'basis':'cpu-and-memory-budget-heuristic'}):
        raise scene.SuiteError('capacity resource observation/advice differs from the 4 CPU/2 GiB profile')
    return {'peak_rss_bytes':max(s['process_peak_rss_bytes'] for s in samples),
        'container_memory_peak_bytes':after['memory_peak_bytes'],
        'memory_max_events':after.get('memory_max_events',0),
        'peak_exceeded_configured_limit':after['memory_peak_bytes']>2*(1<<30),'cpu_seconds':(after['cpu_usage_usec']-before['cpu_usage_usec'])/1e6,
        'oom':after['oom'],'oom_kill':after['oom_kill'],'max_running':max(r['running'] for r in applied),
        'max_waiting':max(r['waiting'] for r in applied),'rss_method':'sum-of-container-process-VmHWM',
        'resource_observation':final}


def save_progress(root,report):
    checkpoint={'run_id':report['run_id'],'completed_groups':len(report['groups']),'groups':report['groups'],
                'completed_mosaic_groups':len(report.get('mosaic_groups',[])),'mosaic_groups':report.get('mosaic_groups',[])}
    (root/'capacity-progress.json').write_text(json.dumps(checkpoint,sort_keys=True))


def physical(action,names):
    return fixture.physical(action,names)


MANAGER_PERMISSIONS={'manager.derived_artifact.create','manager.derived_artifact.read'}
MOSAIC_TASK_TYPE='raster_mosaic_generation'
DIRECTORY_TIMING_METHOD='submit-one-Manager-task-through-parent-terminal-includes-automatic-scan-excludes-oracle'


def directory_matrix():
    for repetition in (1,2):
        for leaf_concurrency in ((1,4) if repetition==1 else (4,1)):
            yield repetition,leaf_concurrency


def directory_task(source_engine,target_engine,name,leaf_concurrency):
    return {'name':name,'enabled':True,'config':{
        'source':{'node_locator':f'addp://engine/{source_engine}/path/raster-source/{fixture.MOSAIC_SOURCE_PREFIX}?type=prefix',
                  'source_engine_id':source_engine,'recursive':True,'include_patterns':['*.tif'],'exclude_patterns':[]},
        'target':{'storage_locator':f'addp://engine/{target_engine}/path/raster-target?type=bucket',
                  'target_engine_id':target_engine,'dataset_name':name},
        'placement':{'mode':'detached'},
        'cog':{'compression':'DEFLATE','blocksize':512,'overview_resampling':'NEAREST',
               'validate_source_cog':True,'leaf_concurrency':leaf_concurrency,'num_threads':1,'leaf_retry_attempts':1},
        'overview':{'enabled':True,'max_pixels':(fixture.SIZE//2)**2,'resampling':'NEAREST'},
        'tiles':{'enabled':False,'min_zoom':0,'max_zoom':0,'format':'webp'}}}


def validate_directory_execution(execution,source_locator,name,target_engine):
    if execution.get('module')!='manager' or execution.get('task_type')!=MOSAIC_TASK_TYPE or execution.get('status')!='success':
        raise scene.SuiteError('capacity mosaic parent execution owner/type/status differ')
    metadata=scene.obj(execution.get('metadata'),'mosaic metadata')
    runtime=scene.obj(metadata.get('workflow_runtime'),'mosaic Runtime')
    if (runtime.get('engine_type'),runtime.get('operator'),runtime.get('mode'))!=('geopython_workflow','build_raster_mosaic','direct'):
        raise scene.SuiteError('capacity mosaic did not invoke the product direct operator')
    facts=scene.obj(metadata.get('lineage_facts'),'mosaic Owner facts')
    inputs=scene.array(facts.get('inputs'),'mosaic inputs');outputs=scene.array(facts.get('outputs'),'mosaic outputs')
    operations=scene.array(facts.get('operations'),'mosaic operations');scans=scene.array(facts.get('meta_scan_refs'),'mosaic auto scans')
    if (facts.get('schema_version')!='addp.lineage-facts/v1' or len(inputs)!=1 or inputs[0].get('locator')!=source_locator
        or len(outputs)!=1
        or len(operations)!=1 or operations[0].get('kind')!='derive' or operations[0].get('operator')!=MOSAIC_TASK_TYPE
        or len(scans)!=1 or not isinstance(scans[0],str) or not scans[0]):
        raise scene.SuiteError('capacity mosaic scope/output/automatic scan facts differ')
    output=scene.obj(outputs[0],'mosaic whole output')
    if type(output.get('item_id')) is not int:
        raise scene.SuiteError('capacity mosaic whole output item id is not an integer')
    item_id=scene.positive(output.get('item_id'),'mosaic whole output item id')
    fingerprint=output.get('item_fingerprint');locator=output.get('locator')
    if not isinstance(fingerprint,str) or not fingerprint or not isinstance(locator,str):
        raise scene.SuiteError('capacity mosaic whole output identity is missing')
    try:
        uri=urllib.parse.urlsplit(locator)
        query=urllib.parse.parse_qs(uri.query,keep_blank_values=True,strict_parsing=True)
    except ValueError as error:
        raise scene.SuiteError('capacity mosaic whole output locator is invalid') from error
    if ((uri.scheme,uri.netloc,uri.path)!=('addp','engine',f'/{target_engine}/path/raster-target/{name}')
        or uri.fragment or query!={'type':['object'],'item_id':[str(item_id)]}):
        raise scene.SuiteError('capacity mosaic whole output locator differs from its exact item scope')
    return facts,scans[0]


def validate_directory_scan(parent,scan,scan_id,identity,target_engine):
    principal=scene.positive(int(identity['principal_id']),'mosaic initiating User')
    tenant=scene.positive(int(identity['tenant_id']),'mosaic Tenant')
    parent_id=parent.get('execution_id')
    context=scene.obj(scan.get('scan_context'),'mosaic scan context')
    if (not isinstance(parent_id,str) or not parent_id
        or type(parent.get('triggered_by')) is not int or parent['triggered_by']!=principal
        or type(scan.get('triggered_by')) is not int or scan['triggered_by']!=principal
        or type(scan.get('tenant_id')) is not int or scan['tenant_id']!=tenant
        or scan.get('execution_id')!=scan_id or scan.get('parent_execution_id')!=parent_id
        or (scan.get('module'),scan.get('task_type'),scan.get('status'))!=('meta','scan','success')
        or scan.get('source_task_id') is not None
        or type(context.get('engine_id')) is not int or context['engine_id']!=target_engine
        or context.get('scan_depth')!='deep'):
        raise scene.SuiteError('capacity mosaic automatic scan actor, parent, Tenant or target differ')
    return {**{key:scan[key] for key in ('execution_id','parent_execution_id','module','task_type','status','triggered_by','tenant_id')},
            'scan_context':{'engine_id':context['engine_id'],'scan_depth':context['scan_depth']}}


def validate_directory_item_identity(item,output,target_engine,name):
    if (type(item.get('id')) is not int or item['id']!=output.get('item_id')
        or type(item.get('engine_id')) is not int or item['engine_id']!=target_engine
        or item.get('full_name')!='raster-target/'+name
        or not isinstance(item.get('fingerprint'),str) or not item['fingerprint']
        or item['fingerprint']!=output.get('item_fingerprint')):
        raise scene.SuiteError('capacity mosaic whole output differs from the actual Meta item identity')


def validate_directory_item(item,physical):
    attrs=scene.obj(item.get('attributes'),'mosaic attributes');facts=scene.obj(attrs.get('item'),'mosaic item facts')
    if (facts.get('data_type'),facts.get('format'),facts.get('layout'))!=('media','raster_mosaic','whole'):
        raise scene.SuiteError('capacity mosaic must resolve to a whole DataItem')
    spatial=scene.obj(scene.obj(attrs.get('capabilities'),'mosaic capabilities').get('spatial'),'mosaic spatial')
    fmt=scene.obj(scene.obj(attrs.get('format_info'),'mosaic formats').get('raster_mosaic'),'mosaic format')
    if (spatial.get('srid')!=32617 or spatial.get('extent')!=physical['summary']['extent']
        or any(fmt.get(k)!=physical['summary'][k] for k in ('leaf_count','source_count','failed_count','vrt_width','vrt_height','overview_width','overview_height'))
        or (fmt.get('manifest_ref'),fmt.get('index_ref'),fmt.get('overview_ref'))!=('mosaic.addp.json','index/source-index.json','overviews/overview.cog.tif')):
        raise scene.SuiteError('capacity mosaic Meta grid or component facts differ')
    # Same strict integer/equality rule, with whole-scope physical/stat totals.
    scene.strict_sizes({'size_bytes':physical['physical_size_bytes']},{'size_bytes':physical['object_size_bytes']},item)
    return {'physical_total_size_bytes':physical['physical_size_bytes'],
            'object_total_size_bytes':physical['object_size_bytes'],'data_item_size_bytes':item['size_bytes']}



class CapacityMetadataGrants:
    """Exact, expiring metadata visibility; never an execution/content Allow."""
    def __init__(self, env):
        self.env = env
        self.registration = registration
        self.client = scene.GatewayClient(env['GATEWAY_URL'], env['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'], 30)
        issuer = scene.GatewayClient(env['GATEWAY_URL'], env['ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN'], 30)
        self.path = Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/'capacity-metadata-grants.json'
        self.report = {'issued': [], 'pending': [], 'already_visible': [], 'facts': [], 'failures': [], 'cleanup': 'pending'}
        self.phase='participants'
        self.operation_active=False
        self.expires_at = (datetime.now(timezone.utc)+timedelta(seconds=10800)).isoformat()
        def checkpoint(pending):
            self.report['pending'] = list(pending)
            self.save()
        self.save()
        try:
            self.grants = self.registration.ExactFileMetadataGrants(issuer,self.client,
                int(env['ADDP_ONLINE_TEST_TENANT_ID']),on_change=checkpoint,on_evidence=self.capture_facts)
        except Exception as error:
            self.record_failure('initialization',error)
            raise
        self.phase='idle'
        self.save()

    def save(self):
        self.path.write_text(json.dumps(self.report, sort_keys=True))

    def capture_facts(self, evidence):
        self.report['facts'].append(evidence)
        self.save()

    def record_failure(self, role, error):
        stage=self.phase if role=='initialization' else role
        if role=='business' and self.operation_active:
            stage=getattr(self.grants,'phase',self.phase) if self.phase in {'grant','facts_observe'} else self.phase
        detail=capacity_error_evidence(error,stage)
        self.report['failures'].append({'role':role,**detail})
        self.save()
        (self.path.parent/'capacity-failure.json').write_text(json.dumps({
            'schema_version':'addp.raster-capacity-failure/v1','run_id':self.env.get('ADDP_ONLINE_TEST_RUN_ID'),
            'failures':self.report['failures']},sort_keys=True))

    def ensure(self, engine, full_name):
        self.phase='fixture_scope';self.operation_active=True
        source = int(self.env['ADDP_ONLINE_RASTER_SOURCE_ENGINE_ID'])
        target = int(self.env['ADDP_ONLINE_RASTER_TARGET_ENGINE_ID'])
        if engine == source:
            allowed = {'raster-source/'+fixture.SOURCE_NAME} | {
                'raster-source/'+fixture.MOSAIC_SOURCE_PREFIX+'/'+name for name in fixture.mosaic_windows()}
            valid = full_name in allowed
            layouts=('single','multi'); format_name='tiff'
        elif engine == target:
            name = full_name.removeprefix('raster-target/')
            singles = {f'capacity-c{cache}-n{running}-r{repetition}-{index}.cog.tif'
                       for repetition,cache,running in matrix() for index in range(2)}
            wholes = {f'capacity-mosaic-l{concurrency}-r{repetition}' for repetition,concurrency in directory_matrix()}
            valid = full_name == 'raster-target/'+name and (name in singles or name in wholes)
            layouts=('whole',) if name in wholes else ('single','multi')
            format_name='raster_mosaic' if name in wholes else 'tiff'
        else:
            valid = False
        if not valid:
            raise self.registration.MetadataGrantError('fixture_scope','target_out_of_scope')
        self.phase='facts_observe'
        facts=self.grants.metadata_facts(engine,full_name,'minio',data_type='media',format_name=format_name,layouts=layouts)
        layout=facts['boundary']['layout']
        self.phase='target_visibility'
        if engine == target:
            path = self.registration.ExactFileMetadataGrants.target(engine,full_name,'minio',layout=layout)
            visibility = scene.obj(self.client.request('POST','/api/v1/system/engine-access/metadata-checks/meta-query',
                (200,),{'targets':[path]}).payload,'current target metadata visibility')
            rows = visibility.get('targets')
            if (not visibility.get('observed_at') or not isinstance(rows,list) or len(rows)!=1
                or rows[0].get('target')!=path or type(rows[0].get('visible')) is not bool):
                raise scene.SuiteError('capacity target metadata visibility is incomplete')
            if rows[0]['visible']:
                self.report['already_visible'].append({'target':path,'observed_at':visibility['observed_at']}); self.save()
                self.operation_active=False;self.phase='idle'
                return None
        self.phase='grant'
        receipt = self.grants.grant(engine, full_name, 'minio', data_type='media',
            format_name=format_name, layout=layout, expires_at=self.expires_at)
        self.report['issued'].append(receipt); self.save()
        self.operation_active=False;self.phase='idle'
        return receipt

    def cleanup(self):
        try:
            result = self.grants.cleanup()
        except BaseException:
            self.report['cleanup'] = 'failed'; self.save(); raise
        self.report['cleanup'] = result; self.save()
        return result


SNAPSHOT_FIELDS = {
    'execution': {'execution_id','module','task_type','tenant_id','triggered_by','status','metadata','outputs'},
    'produced_item': {'id','tenant_id','engine_id','node_id','name','full_name','item_type','fingerprint',
                      'size_bytes','attributes','scanned_depth'},
    'whole_state': {'scan','identity','seed_items_remaining','seed_nodes_remaining','internal_nodes',
                    'internal_items','outside_items','outside_nodes'},
    'seed_fragments': {'nodes','items'},
}


class SnapshotQueryError(scene.SuiteError):
    """Only controlled labels and bounded shape diagnostics may leave the fixture."""
    def __init__(self, operation, phase, code, diagnostics=None):
        self.stage, self.code = 'snapshot.'+operation+'.'+phase, code
        self.diagnostics = diagnostics or {}
        super().__init__(self.stage+'.'+code)


def capacity_error_evidence(error, stage='scenario'):
    result=registration.metadata_error_evidence(error,stage)
    if isinstance(error,SnapshotQueryError):
        result.update(stage=error.stage,code=error.code,diagnostics=error.diagnostics)
    return result


LINEAGE_RESOURCE_FIELDS = ('port','locator','item_id','item_fingerprint','field_name','write_mode')


def projection_signature(value, *, present=True):
    """The artifact never receives original field values or unknown field names."""
    if not present:return {'present':False}
    kind={dict:'object',list:'array',str:'string',int:'number',float:'number',bool:'boolean',type(None):'null'}
    result={'present':True,'type':kind.get(type(value),'other')}
    try:
        encoded=json.dumps(value,sort_keys=True,ensure_ascii=False,allow_nan=False,separators=(',',':')).encode()
        result.update(bytes=len(encoded),sha256=hashlib.sha256(encoded).hexdigest())
    except (TypeError,ValueError,RecursionError):result['digest_unavailable']=True
    return result


MANAGER_OPERATION_FIELDS = ('kind','operator','input_ports','output_ports')


def manager_operation_projection(owner):
    """Match the declared ManagerExecutionOperation DTO, including omitempty."""
    projected=[]
    for raw in scene.array(owner.get('operations'),'Owner Manager lineage operations'):
        row=scene.obj(raw,'Owner Manager lineage operation')
        if not isinstance(row.get('kind'),str):
            raise scene.SuiteError('Owner Manager lineage operation has invalid declared fields')
        operation={'kind':row['kind']}
        for field in MANAGER_OPERATION_FIELDS[1:]:
            value=row.get(field)
            if value is not None and (not isinstance(value,str) if field=='operator'
                else not isinstance(value,list) or any(not isinstance(port,str) for port in value)):
                raise scene.SuiteError('Owner Manager lineage operation has invalid declared fields')
            if value:operation[field]=value
        projected.append(operation)
    return projected


def lineage_projection_evidence(public, owner, *, module):
    """Bounded diagnostics only; the shared validator remains the acceptance gate."""
    evidence={'operation':'execution','phase':'public_projection','result':'passed','code':None,
              'contract':'manager_professional' if module=='manager' else 'develop_professional',
              'public_facts':projection_signature(public),'differences':[],'differences_truncated':False}
    def difference(direction, field, actual, expected, *, index=None, actual_present=True, expected_present=True):
        if len(evidence['differences'])>=16:
            evidence['differences_truncated']=True;return
        row={'direction':direction,'field':field,'actual':projection_signature(actual,present=actual_present),
             'expected':projection_signature(expected,present=expected_present)}
        if index is not None:row['index']=index
        evidence['differences'].append(row)
    if not isinstance(public,dict) or not isinstance(owner,dict):
        evidence['owner_projection']=projection_signature(None)
        difference('facts','shape',public,owner);return evidence
    projected={'schema_version':owner.get('schema_version')}
    if public.get('schema_version')!=owner.get('schema_version'):
        difference('facts','schema_version',public.get('schema_version'),owner.get('schema_version'))
    if module=='manager':
        try:expected_operations=manager_operation_projection(owner)
        except (scene.SuiteError,KeyError,TypeError):expected_operations=None
        projected['operations']=expected_operations
        actual_operations=public.get('operations')
        if not isinstance(actual_operations,list) or not isinstance(expected_operations,list):
            difference('operations','shape',actual_operations,expected_operations)
        else:
            if len(actual_operations)!=len(expected_operations):
                difference('operations','cardinality',actual_operations,expected_operations)
            if max(len(actual_operations),len(expected_operations))>16:evidence['differences_truncated']=True
            for index,(got,wanted) in enumerate(zip(actual_operations[:16],expected_operations[:16])):
                if not isinstance(got,dict):
                    difference('operations','shape',got,wanted,index=index);continue
                unknown={k:v for k,v in got.items() if k not in MANAGER_OPERATION_FIELDS}
                if unknown:difference('operations','unrecognized_fields',unknown,{},index=index)
                for field in MANAGER_OPERATION_FIELDS:
                    if (field in got)!=(field in wanted) or got.get(field)!=wanted.get(field):
                        difference('operations',field,got.get(field),wanted.get(field),index=index,
                                   actual_present=field in got,expected_present=field in wanted)
    elif public.get('operations') not in (None,[]):
        difference('facts','operations',public['operations'],None)

    def locator(value):
        if not isinstance(value,str):return value
        try:
            uri=urllib.parse.urlsplit(value)
            return (uri.scheme,uri.netloc,uri.path,urllib.parse.parse_qs(uri.query,keep_blank_values=True))
        except ValueError:return None
    for direction in ('inputs','outputs'):
        actual,expected=public.get(direction),owner.get(direction)
        if not isinstance(actual,list) or not isinstance(expected,list):
            difference(direction,'shape',actual,expected);continue
        projected[direction]=[{k:v for k,v in ref.items() if k in LINEAGE_RESOURCE_FIELDS}
                              if isinstance(ref,dict) else None for ref in expected]
        if len(actual)!=len(expected):difference(direction,'cardinality',actual,projected[direction])
        if max(len(actual),len(expected))>16:evidence['differences_truncated']=True
        for index,(got,wanted) in enumerate(zip(actual[:16],expected[:16])):
            if not isinstance(got,dict) or not isinstance(wanted,dict):
                difference(direction,'shape',got,wanted,index=index);continue
            unknown={k:v for k,v in got.items() if k not in LINEAGE_RESOURCE_FIELDS}
            if unknown:difference(direction,'unrecognized_fields',unknown,{},index=index)
            for field in LINEAGE_RESOURCE_FIELDS:
                left,right=got.get(field),wanted.get(field)
                if field=='locator':left,right=locator(left),locator(right)
                invalid_locator=field=='locator' and (
                    not isinstance(got.get(field),str) or not isinstance(wanted.get(field),str)
                    or left is None or right is None)
                if (field in got)!=(field in wanted) or left!=right or invalid_locator:
                    difference(direction,field,got.get(field),wanted.get(field),index=index,
                               actual_present=field in got,expected_present=field in wanted)
    evidence['owner_projection']=projection_signature(projected)
    return evidence


def lineage_projection_reason(error):
    if isinstance(error,ValueError):return 'locator_invalid'
    reasons={
        'Monitor lineage schema_version is invalid':'schema_version_mismatch',
        'Manager lineage operations differ from Owner declared projection':'manager_operations_mismatch',
        'Owner Manager lineage operation has invalid declared fields':'manager_operation_shape_mismatch',
        'Manager execution exposed internal fields':'manager_private_execution_fields',
        'Manager execution metadata exposed undeclared fields':'manager_private_metadata_fields',
        'Manager lineage exposed undeclared fields':'manager_private_lineage_fields',
        'Manager lineage resource exposed undeclared fields':'manager_private_resource_fields',
        'Manager lineage operation exposed undeclared fields':'manager_private_operation_fields',
        'Monitor exposes lineage operations':'private_operations_exposed',
        'Monitor exposes private lineage resource fields':'private_resource_fields_exposed',
        'Monitor lineage resource locator is missing':'locator_missing',
        'Monitor lineage locator contains private context':'locator_private_context',
        'Monitor lineage resource differs from Owner safe projection':'resource_mismatch',
        'Monitor lineage inputs count differs from Owner':'input_cardinality_mismatch',
        'Monitor lineage outputs count differs from Owner':'output_cardinality_mismatch',
    }
    return reasons.get(str(error),'shape_or_comparison_failed')


class MosaicScopeFixture:
    """Meta-owned wrong-record fixture, confined to this disposable capacity run."""
    def __init__(self, env):
        self.env = env
        self.base = importlib.import_module('scripts.test.execution-retention-readonly-online')
        try:
            self.boundary()
            self.owner = self.inspect()
            self.validate_owner(self.owner)
        except Exception:
            raise SnapshotQueryError('owner','ownership','owner_rejected') from None
        self.query_report = {'schema_version':'addp.capacity-snapshots/v1',
            'run_id':env['ADDP_ONLINE_TEST_RUN_ID'],'queries':[],'projections':[]}

        (Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/'mosaic-scope-owner.json').write_text(json.dumps({
            'container_id': self.owner['Id'], 'started_at': self.owner['State']['StartedAt'],
            'run_id': env['ADDP_ONLINE_TEST_RUN_ID']}, sort_keys=True))

    def boundary(self):
        self.base.boundary(SUITE)
        keys = ('ONLINE_SUITE', 'ADDP_ONLINE_TEST_RUN_ID', 'ADDP_ONLINE_SECRET_DIR',
                'ADDP_ONLINE_ARTIFACT_DIR', 'GITHUB_RUN_ID', 'GITHUB_RUN_ATTEMPT')
        if (any(self.env.get(k) != os.environ.get(k) for k in keys)
            or self.env.get('ADDP_ONLINE_TEST_RUN_ID') !=
                f"raster-capacity-{self.env.get('GITHUB_RUN_ID')}-{self.env.get('GITHUB_RUN_ATTEMPT')}"
            or not re.fullmatch(r'raster-capacity-[0-9]+-[0-9]+', self.env.get('ADDP_ONLINE_TEST_RUN_ID', ''))):
            raise scene.SuiteError('mosaic scope fixture run differs from its Hosted deployment')

    @staticmethod
    def inspect():
        try:
            result = subprocess.run(['docker', 'inspect', 'addp-postgres'], check=True,
                                    capture_output=True, text=True, timeout=10)
            container, = json.loads(result.stdout)
            return container
        except Exception:
            raise scene.SuiteError('mosaic scope PostgreSQL inspection unavailable') from None

    def validate_owner(self, container):
        values = dict(v.split('=', 1) for v in container.get('Config', {}).get('Env', []) if '=' in v)
        try:
            user = self.base.validate_container(container, self.owner['Id'], values)
        except self.base.API.SuiteError:
            raise scene.SuiteError('mosaic scope PostgreSQL ownership differs') from None
        if (not re.fullmatch(r'[0-9a-f]{64}', container['Id']) or not self.owner['State'].get('StartedAt')
            or container['State'].get('StartedAt') != self.owner['State']['StartedAt']):
            raise scene.SuiteError('mosaic scope PostgreSQL restarted or changed')
        return user

    def state(self, item, parent, identity, *, seed=False, seeds=None, scan=None):
        self.boundary()
        user = self.validate_owner(self.inspect())
        name = item.get('name', '')
        try:
            fixture.mosaic_names_checked([name])
        except fixture.shared.FixtureError:
            raise scene.SuiteError('invalid mosaic scope dataset name') from None
        for field in ('id', 'node_id', 'engine_id'):
            scene.positive(item.get(field), 'whole ' + field)
        tenant = scene.positive(int(identity['tenant_id']), 'whole Tenant')
        principal = scene.positive(int(identity['principal_id']), 'whole User')
        if (tenant <= 1 or item.get('full_name') != 'raster-target/' + name
            or not re.fullmatch(r'[0-9a-f]{64}', item.get('fingerprint', ''))
            or not re.fullmatch(r'[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}', parent)):
            raise scene.SuiteError('mosaic scope fixture needs the exact current whole identity')
        seeds = seeds or {'items': [], 'nodes': []}
        if (set(seeds) != {'items', 'nodes'} or any(type(v) is not int or v <= 0
            for values in seeds.values() for v in values) or len(seeds['items']) > 2 or len(seeds['nodes']) > 1):
            raise scene.SuiteError('invalid mosaic seed record IDs')
        if scan is not None and not re.fullmatch(r'[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}', scan):
            raise scene.SuiteError('invalid mosaic rescan UUID')
        # The successful, current Manager execution must itself declare this item.
        owned = """WITH RECURSIVE owned AS (
 SELECT i.*, n.name AS node_name, n.full_name AS node_full_name,
 n.node_type, n.scanned_depth AS node_scanned_depth, n.depth AS node_depth
 FROM meta.meta_item i JOIN meta.meta_node n ON n.id=i.node_id
 AND n.tenant_id=i.tenant_id AND n.engine_id=i.engine_id AND n.deleted_at IS NULL
 WHERE i.id=:'item'::integer AND i.node_id=:'node'::integer
 AND i.engine_id=:'engine'::integer AND i.tenant_id=:'tenant'::integer
 AND i.fingerprint=:'fingerprint' AND i.full_name=:'full_name' AND i.name=:'name'
 AND n.name=i.name AND n.full_name=i.full_name AND n.node_type='prefix'
 AND i.deleted_at IS NULL AND i.attributes->'item'->>'layout'='whole'
 AND i.attributes->'item'->>'format'='raster_mosaic'
 AND EXISTS (SELECT 1 FROM common.task_executions e
 WHERE e.execution_id=:'parent' AND e.tenant_id=i.tenant_id
 AND e.actor_principal_id=:'principal'::bigint AND e.triggered_by=:'principal'::integer
 AND e.module='manager' AND e.task_type='raster_mosaic_generation' AND e.status='success'
 AND e.created_at>=:'started_at'::timestamptz
 AND e.metadata->'lineage_facts'->'outputs' @> jsonb_build_array(jsonb_build_object(
 'item_id',i.id,'item_fingerprint',i.fingerprint))))"""
        if seed:
            # Two real component names, one direct and one below a child node.
            statement = "BEGIN; SET LOCAL statement_timeout='3s'; SET LOCAL lock_timeout='1s';\n" + owned + """,
 child AS (INSERT INTO meta.meta_node
 (tenant_id,engine_id,parent_node_id,node_type,name,depth,full_name,path,scanned_depth,created_at)
 SELECT tenant_id,engine_id,node_id,'prefix','index',node_depth+1,full_name||'/index',
 full_name||'/index','basic',now() FROM owned RETURNING *),
 fragments AS (INSERT INTO meta.meta_item
 (tenant_id,engine_id,node_id,item_type,name,full_name,fingerprint,scanned_depth,attributes,created_at)
 SELECT o.tenant_id,o.engine_id,v.node_id,'object',v.name,v.full_name,
 encode(sha256(convert_to(o.engine_id::text||':'||v.full_name,'UTF8')),'hex'),
 'basic','{"item":{"layout":"single"}}'::jsonb,now() FROM owned o
 CROSS JOIN LATERAL (SELECT o.node_id,'mosaic.addp.json'::text AS name,
 o.full_name||'/mosaic.addp.json' AS full_name UNION ALL
 SELECT c.id,'source-index.json',c.full_name||'/source-index.json' FROM child c) v
 RETURNING id)
 SELECT json_build_object('nodes',(SELECT array_agg(id) FROM child),
 'items',(SELECT array_agg(id) FROM fragments)); COMMIT;
"""
        else:
            statement = "BEGIN READ ONLY; SET LOCAL statement_timeout='3s'; SET LOCAL lock_timeout='1s';\n" + owned + """,
 descendants(id) AS (SELECT node_id FROM owned UNION
 SELECT n.id FROM meta.meta_node n JOIN descendants d ON n.parent_node_id=d.id
 WHERE n.tenant_id=:'tenant'::integer AND n.engine_id=:'engine'::integer)
 SELECT json_build_object('scan',(SELECT json_build_object('execution_id',execution_id,
 'config',execution_config,'status',status) FROM common.task_executions e
 WHERE e.execution_id=:'scan' AND e.tenant_id=o.tenant_id AND e.actor_principal_id=:'principal'::bigint
 AND e.triggered_by=:'principal'::integer AND e.module='meta' AND e.task_type='scan'
 AND e.created_at>=:'started_at'::timestamptz), 'identity',json_build_object('id',o.id,'node_id',o.node_id,
 'engine_id',o.engine_id,'tenant_id',o.tenant_id,'name',o.name,'full_name',o.full_name,
 'fingerprint',o.fingerprint,'scanned_depth',o.scanned_depth,'node_name',o.node_name,
 'node_full_name',o.node_full_name,'node_type',o.node_type,'node_scanned_depth',o.node_scanned_depth),
 'seed_items_remaining',(SELECT count(*) FROM meta.meta_item WHERE id=ANY(:'seed_items'::integer[])),
 'seed_nodes_remaining',(SELECT count(*) FROM meta.meta_node WHERE id=ANY(:'seed_nodes'::integer[])),
 'internal_nodes',(SELECT count(*) FROM descendants WHERE id<>o.node_id),
 'internal_items',(SELECT count(*) FROM meta.meta_item i WHERE i.tenant_id=o.tenant_id
 AND i.engine_id=o.engine_id AND i.id<>o.id AND i.node_id IN (SELECT id FROM descendants)),
 'outside_items',(SELECT md5(coalesce(string_agg(json_build_array(id,node_id,name,full_name,
 fingerprint,deleted_at)::text,'|' ORDER BY id),'')) FROM meta.meta_item
 WHERE tenant_id=o.tenant_id AND engine_id=o.engine_id AND node_id NOT IN (SELECT id FROM descendants)),
 'outside_nodes',(SELECT md5(coalesce(string_agg(json_build_array(id,parent_node_id,name,
 full_name,node_type,deleted_at)::text,'|' ORDER BY id),'')) FROM meta.meta_node
 WHERE tenant_id=o.tenant_id AND engine_id=o.engine_id AND id NOT IN (SELECT id FROM descendants)))
 FROM owned o; COMMIT;
"""
        values = dict(item=item['id'], node=item['node_id'], engine=item['engine_id'], tenant=tenant,
                      principal=principal, fingerprint=item['fingerprint'], full_name=item['full_name'],
                      name=name, parent=parent, scan=scan or '', started_at=self.owner['State']['StartedAt'],
                      seed_items='{'+','.join(map(str,seeds['items']))+'}',
                      seed_nodes='{'+','.join(map(str,seeds['nodes']))+'}')
        return self.query(statement, values, operation='seed_fragments' if seed else 'whole_state')

    def archive_query(self, receipt):
        self.query_report['queries'].append(receipt.copy())
        self.save_snapshot_report(receipt)

    def archive_projection(self, receipt):
        if len(self.query_report['projections'])>=128:
            raise SnapshotQueryError('execution','archive','projection_limit_exceeded')
        self.query_report['projections'].append(receipt)
        self.save_snapshot_report(receipt)

    def save_snapshot_report(self, receipt):
        try:
            (Path(self.env['ADDP_ONLINE_ARTIFACT_DIR'])/'capacity-snapshots.json').write_text(
                json.dumps(self.query_report,sort_keys=True))
        except OSError:
            raise SnapshotQueryError(receipt['operation'],'archive','write_failed',
                {'primary':receipt.copy()}) from None

    def query(self, statement, values, *, operation):
        if operation not in SNAPSHOT_FIELDS:
            raise ValueError('unknown snapshot operation')
        if len(self.query_report['queries']) >= 128:
            raise SnapshotQueryError(operation,'archive','query_limit_exceeded')
        receipt={'operation':operation,'statement_sha256':hashlib.sha256(statement.encode()).hexdigest(),
                 'phase':'ownership','result':'failed','code':None,'sqlstate':None,'returncode':None}
        def reject(phase, code):
            receipt.update(phase=phase,code=code)
            self.archive_query(receipt)
            raise SnapshotQueryError(operation,phase,code,receipt.copy()) from None
        try:
            self.boundary()
            user = self.validate_owner(self.inspect())
        except Exception:
            reject('ownership','owner_rejected')
        command = ['docker', 'exec', '-i', self.owner['Id'], 'psql', '-X', '-qAt',
                   '-v', 'ON_ERROR_STOP=1', '-v', 'VERBOSITY=sqlstate', '-U', user, '-d', 'addp_online']
        for key, value in values.items(): command.extend(['-v', key + '=' + str(value)])
        try:
            result = subprocess.run(command, input=statement, capture_output=True, text=True, timeout=8)
        except subprocess.TimeoutExpired:
            receipt['sqlstate']='unavailable'
            reject('execute','timeout')
        except Exception:
            receipt['sqlstate']='unavailable'
            reject('execute','command_failed')
        if (type(getattr(result,'returncode',None)) is not int
            or not isinstance(getattr(result,'stdout',None),str)
            or not isinstance(getattr(result,'stderr',None),str)):
            receipt['sqlstate']='unavailable'
            reject('execute','transport_shape_invalid')
        stdout,stderr=result.stdout.encode(),result.stderr.encode()
        receipt.update(returncode=result.returncode,stdout_bytes=len(stdout),stderr_bytes=len(stderr),
            stdout_sha256=hashlib.sha256(stdout).hexdigest(),stderr_sha256=hashlib.sha256(stderr).hexdigest(),
            stdout_rows=sum(bool(line.strip()) for line in result.stdout.split('\n')))
        if result.returncode:
            match=re.search(r'(?m)^(?:psql:[^\r\n]*: )?ERROR:\s+([0-9A-Z]{5})[ \t]*$',result.stderr)
            receipt['sqlstate']=match.group(1) if match else 'unavailable'
            reject('execute','sql_failed' if match else 'process_failed')
        if len(stdout)>65536: reject('response_bounds','oversize')
        if not stdout.strip(): reject('response_bounds','empty')
        if receipt['stdout_rows']!=1: reject('response_bounds','multiple_rows')
        try:
            def invalid_constant(_): raise ValueError()
            payload=json.loads(result.stdout,parse_constant=invalid_constant)
        except (ValueError,RecursionError):
            reject('decode','invalid_json')
        receipt['json_type']={dict:'object',list:'array',str:'string',int:'number',float:'number',
                              bool:'boolean',type(None):'null'}[type(payload)]
        if not isinstance(payload,dict): reject('shape','not_object')
        receipt['field_count']=len(payload)
        missing=SNAPSHOT_FIELDS[operation]-payload.keys()
        receipt['missing_fields']=sorted(missing)
        receipt['unexpected_field_count']=len(payload.keys()-SNAPSHOT_FIELDS[operation])
        if missing or receipt['unexpected_field_count']: reject('shape','fields_mismatch')
        receipt.update(phase='complete',result='passed')
        self.archive_query(receipt)
        return payload


    def execution(self, observation, identity, module):
        identifier = observation.get('execution_id', '')
        if module not in {'develop', 'manager'} or not re.fullmatch(r'[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}', identifier):
            raise scene.SuiteError('capacity snapshot requires a current production execution')
        statement = """BEGIN READ ONLY; SET LOCAL statement_timeout='3s'; SET LOCAL lock_timeout='1s';
 SELECT json_build_object('execution_id',execution_id,'module',module,'task_type',task_type,
 'tenant_id',tenant_id,'triggered_by',triggered_by,'status',status,
 'metadata',json_build_object('lineage_facts',metadata->'lineage_facts',
 'workflow_runtime',metadata->'workflow_runtime','result',json_build_object(
 'final_result',metadata->'result'->'final_result','meta_scan_runs',metadata->'result'->'meta_scan_runs')),
 'outputs',metadata->'outputs') FROM common.task_executions
 WHERE execution_id=:'execution' AND module=:'module' AND tenant_id=:'tenant'::integer
 AND actor_principal_id=:'principal'::bigint AND triggered_by=:'principal'::integer
 AND status='success' AND task_type=CASE WHEN module='manager' THEN 'raster_mosaic_generation' ELSE 'workflow' END
 AND created_at>=:'started_at'::timestamptz
 AND octet_length(coalesce((metadata->'lineage_facts')::text,''))<=16384
 AND octet_length(coalesce((metadata->'result'->'final_result')::text,''))<=16384
 AND octet_length(coalesce((metadata->'result'->'meta_scan_runs')::text,''))<=8192
 AND octet_length(coalesce((metadata->'workflow_runtime')::text,''))<=4096
 AND octet_length(coalesce((metadata->'outputs')::text,''))<=8192;
 COMMIT;
"""
        result = self.query(statement, dict(execution=identifier,module=module,tenant=identity['tenant_id'],
            principal=identity['principal_id'],started_at=self.owner['State']['StartedAt']), operation='execution')
        if any(result.get(k) != observation.get(k) for k in ('execution_id', 'module', 'task_type', 'status', 'triggered_by')):
            raise SnapshotQueryError('execution','identity','public_identity_mismatch')
        # Professional execution metadata may contain result; only the lineage
        # contract is shared with Monitor. Full Monitor DTOs are checked separately.
        metadata=observation.get('metadata')
        public=metadata.get('lineage_facts') if isinstance(metadata,dict) else None
        internal=result.get('metadata')
        owner=internal.get('lineage_facts') if isinstance(internal,dict) else None
        proof=lineage_projection_evidence(public,owner,module=module)
        try:
            scene.obj(owner,'Owner lineage facts')
            resources=public
            if module=='manager':
                scene.support.assert_manager_execution_projection(observation)
                if public['operations']!=manager_operation_projection(owner):
                    raise scene.SuiteError('Manager lineage operations differ from Owner declared projection')
                # Operations have already been checked against Manager's DTO.
                # Only the resource comparison view uses the Monitor projection.
                resources={**public,'operations':None}
            scene.support.validate_monitor_lineage({'metadata':{'lineage_facts':resources}},owner)
        except (scene.SuiteError,KeyError,TypeError,ValueError) as error:
            proof.update(result='failed',code='lineage_mismatch',reason=lineage_projection_reason(error),
                         cause_sha256=hashlib.sha256(str(error).encode()).hexdigest())
            self.archive_projection(proof)
            raise SnapshotQueryError('execution','public_projection','lineage_mismatch',proof) from None
        self.archive_projection(proof)
        return {**observation, **result}

    def produced_item(self, item, execution, scan, identity):
        module = execution.get('module'); name = item.get('full_name', '')
        engine = item.get('engine_id'); identifier = execution.get('execution_id', '')
        if (module not in {'develop','manager'} or type(engine) is not int or engine<=0
            or not name.startswith('raster-target/') or type(item.get('id')) is not int or item['id']<=0
            or not re.fullmatch(r'[0-9a-f]{64}', item.get('fingerprint',''))
            or not re.fullmatch(r'[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}', scan)):
            raise scene.SuiteError('capacity snapshot requires exact produced item identities')
        if module == 'develop': fixture.names_checked([name.removeprefix('raster-target/')])
        else: fixture.mosaic_names_checked([name.removeprefix('raster-target/')])
        statement = """BEGIN READ ONLY; SET LOCAL statement_timeout='3s'; SET LOCAL lock_timeout='1s';
 SELECT json_build_object('id',i.id,'tenant_id',i.tenant_id,'engine_id',i.engine_id,'node_id',i.node_id,
 'name',i.name,'full_name',i.full_name,'item_type',i.item_type,'fingerprint',i.fingerprint,
 'size_bytes',i.size_bytes,'attributes',i.attributes,'scanned_depth',i.scanned_depth)
 FROM meta.meta_item i JOIN common.task_executions e ON e.execution_id=:'execution'
 JOIN common.task_executions s ON s.execution_id=:'scan' AND s.parent_execution_id=e.execution_id
 WHERE i.id=:'item'::integer AND i.tenant_id=:'tenant'::integer AND i.engine_id=:'engine'::integer
 AND i.full_name=:'full_name' AND i.fingerprint=:'fingerprint' AND i.deleted_at IS NULL
 AND e.tenant_id=i.tenant_id AND s.tenant_id=i.tenant_id
 AND e.actor_principal_id=:'principal'::bigint AND s.actor_principal_id=e.actor_principal_id
 AND e.triggered_by=:'principal'::integer AND s.triggered_by=e.triggered_by
 AND e.module=:'module' AND e.status='success' AND s.module='meta' AND s.task_type='scan' AND s.status='success'
 AND e.created_at>=:'started_at'::timestamptz AND s.execution_config->>'engine_id'=i.engine_id::text
 AND (e.metadata->'lineage_facts'->'outputs' @> jsonb_build_array(jsonb_build_object('item_id',i.id,'item_fingerprint',i.fingerprint))
 OR e.metadata->'lineage_facts'->'outputs' @> jsonb_build_array(jsonb_build_object('locator',:'locator')))
 AND octet_length(i.attributes::text)<=32768;
 COMMIT;
"""
        locator = f'addp://engine/{engine}/path/{name}?type=object'
        result = self.query(statement, dict(item=item['id'],tenant=identity['tenant_id'],principal=identity['principal_id'],
            engine=engine,full_name=name,fingerprint=item['fingerprint'],execution=identifier,scan=scan,module=module,
            locator=locator,started_at=self.owner['State']['StartedAt']), operation='produced_item')
        if any(result.get(k) != item.get(k) for k in ('id','engine_id','node_id','name','full_name','fingerprint')):
            raise SnapshotQueryError('produced_item','identity','public_identity_mismatch')
        attrs = item.get('attributes', {})
        if (item.get('size_bytes') is not None or 'format_info' in attrs
            or attrs.get('capabilities', {}).get('spatial', {}).get('extent') is not None):
            raise scene.SuiteError('public Meta disclosed content-derived facts')
        scene.obj(result.get('attributes'), 'persisted item facts')
        return result


def whole_tree(client, item):
    tree = scene.obj(client.request('GET', f'/api/v1/meta/resource-tree/{item["engine_id"]}?expand_depth=-1', (200,)).payload,
                     'mosaic resource tree')
    matches = []
    def visit(node):
        node = scene.obj(node, 'resource node')
        locator = urllib.parse.urlparse(node.get('locator', ''))
        path = urllib.parse.unquote(locator.path).removeprefix(f'/{item["engine_id"]}/path/')
        if path == item['full_name'] or path.startswith(item['full_name'] + '/'):
            matches.append(node)
        for child in node.get('children') or []: visit(child)
    visit(tree)
    if len(matches) != 1:
        raise scene.SuiteError('whole resource tree must have exactly one scope leaf')
    leaf = matches[0]
    query = urllib.parse.parse_qs(urllib.parse.urlparse(leaf['locator']).query)
    if (leaf.get('label') != item['name'] or leaf.get('type') != 'object'
        or leaf.get('hasChildren') is not False or leaf.get('children')
        or leaf.get('metadata', {}).get('item_id') != item['id']
        or leaf.get('metadata', {}).get('node_id') is not None
        or urllib.parse.urlparse(leaf['locator']).netloc != 'engine'
        or query.get('item_id') != [str(item['id'])] or query.get('type') != ['object']):
        raise scene.SuiteError('whole resource tree identity or leaf shape differs')
    return tree


def tree_identity_shape(tree):
    """Compare visible identities and structure; scan timestamps may advance."""
    metadata = tree.get('metadata') or {}
    return {**{key: tree.get(key) for key in ('id', 'locator', 'label', 'type', 'hasChildren')},
            'identity': {key: metadata.get(key) for key in ('item_id', 'node_id', 'fingerprint', 'layout')},
            'children': [tree_identity_shape(child) for child in tree.get('children') or []]}


def verify_basic_rescans(env, client, item, parent, identity, scopes):
    root = Path(env['ADDP_ONLINE_ARTIFACT_DIR'])
    proof = {'before': scopes.state(item, parent, identity), 'scans': []}
    path = root/(item['name']+'-basic-rescans.json')
    def save(): path.write_text(json.dumps(proof, sort_keys=True))
    save()
    if proof['before'].get('internal_items') != 0 or proof['before'].get('internal_nodes') != 0:
        raise scene.SuiteError('automatic mosaic scan left internal fragments')
    proof['tree_before'] = whole_tree(client, item); save()
    proof['seed'] = scopes.state(item, parent, identity, seed=True); save()
    if len(proof['seed'].get('nodes') or []) != 1 or len(proof['seed'].get('items') or []) != 2:
        raise scene.SuiteError('mosaic wrong-fragment fixture is incomplete')
    proof['seed_state'] = scopes.state(item, parent, identity, seeds=proof['seed']); save()
    expected_seed = {**proof['before'], 'internal_items': 2, 'internal_nodes': 1,
                     'seed_items_remaining': 2, 'seed_nodes_remaining': 1}
    if proof['seed_state'] != expected_seed:
        raise scene.SuiteError('mosaic wrong-fragment fixture altered its boundary')
    target = f'addp://engine/{item["engine_id"]}/path/{item["full_name"]}?type=prefix'
    for _ in range(2):
        identifier = scene.support.wait_for_meta_scan(client, item['engine_id'], time.monotonic()+120,
            depth='basic', scope={'targets': [target], 'source': 'meta'}, force=False)
        execution = scene.wait_execution(client, 'meta', identifier, 1)
        observation = {'execution': execution}
        proof['scans'].append(observation); save()
        context = execution.get('scan_context', {})
        if (identifier in [s['execution']['execution_id'] for s in proof['scans'][:-1]]
            or execution.get('execution_id') != identifier or execution.get('module') != 'meta'
            or execution.get('task_type') != 'scan' or execution.get('status') != 'success'
            or str(execution.get('tenant_id')) != identity['tenant_id']
            or str(execution.get('triggered_by')) != identity['principal_id']
            or context.get('engine_id') != item['engine_id'] or context.get('scan_depth') != 'basic'):
            raise scene.SuiteError('mosaic basic rescan execution scope or identity differs')
        observation['state'] = scopes.state(item, parent, identity, seeds=proof['seed'], scan=identifier); save()
        frozen = observation['state'].get('scan') or {}
        config = frozen.get('config', {})
        if (frozen.get('execution_id') != identifier or frozen.get('status') != 'success'
            or config.get('engine_id') != item['engine_id'] or config.get('scan_depth') != 'basic'
            or config.get('force') is not False or config.get('targets') != [target]):
            raise scene.SuiteError('persisted basic rescan scope differs')
        observation['frozen_scan'] = frozen
        observation['state']['scan'] = None
        if observation['state'] != proof['before']:
            raise scene.SuiteError('basic rescan left fragments or changed whole/outside identities')
        observation['tree'] = whole_tree(client, item); save()
        # Compare the full visible identity tree, excluding mutable scan observations.
        if tree_identity_shape(observation['tree']) != tree_identity_shape(proof['tree_before']):
            raise scene.SuiteError('basic rescan changed the resource tree')
    proof['result'] = 'passed'; save()
    return proof


def run_directories(env,client,platform,path,image,source_engine,target_engine,report,scans,authorization,scopes):
    source=physical('seed-mosaic',[])
    if source.get('source_unchanged') is not True or set(source.get('tiles',{}))!=set(fixture.mosaic_windows()):
        raise scene.SuiteError('capacity mosaic source preparation is incomplete')
    report['mosaic_source']=source
    scene.support.wait_for_meta_scan(client,source_engine,time.monotonic()+600)
    for tile in fixture.mosaic_windows():
        authorization.ensure(source_engine,'raster-source/'+fixture.MOSAIC_SOURCE_PREFIX+'/'+tile)
    source_locator=f'addp://engine/{source_engine}/path/raster-source/{fixture.MOSAIC_SOURCE_PREFIX}?type=prefix'
    expected_hashes=None
    for repetition,leaf_concurrency in directory_matrix():
        name=f'capacity-mosaic-l{leaf_concurrency}-r{repetition}'
        body=directory_task(source_engine,target_engine,name,leaf_concurrency)
        task=scene.obj(client.request('POST',f'/api/v1/manager/tasks/{MOSAIC_TASK_TYPE}',(201,),body).payload,'mosaic task')
        task_id=scene.positive(task.get('id'),'mosaic task id');task_path=f'/api/v1/manager/tasks/{MOSAIC_TASK_TYPE}/{task_id}'
        detail=scene.obj(client.request('GET',task_path,(200,)).payload,'mosaic task detail')
        if (detail.get('config')!=body['config'] or detail.get('task_type')!=MOSAIC_TASK_TYPE
            or str(detail.get('created_by'))!=report['identity']['principal_id']):
            raise scene.SuiteError('capacity mosaic task config or initiating User differ')
        # The ordinary consumer gains only create/read; deletion remains forbidden.
        client.request('DELETE',task_path,(403,))
        current=platform.request('GET',path,(200,)).payload['policy']
        fields=('running','waiting','cache_mib','default_tenant_running','default_tenant_waiting')
        saved=platform.request('PUT',path,(200,),{**{k:current[k] for k in fields},'version':current['version'],
            'running':1,'waiting':2,'cache_mib':64,'default_tenant_running':1,'default_tenant_waiting':2}).payload['policy']
        fixture.require_disk(Path(env['ADDP_ONLINE_SECRET_DIR']))
        container=restart_runtime(image);sampler=Sampler(cgroup_directory(container['State']['Pid']),platform,path);sampler.start()
        started=time.monotonic()
        try:
            submitted=scene.obj(client.request('POST',task_path+'/execute',(202,),{}).payload,'mosaic submitted execution')
            identifier=submitted.get('execution_id')
            if not isinstance(identifier,str) or not identifier:raise scene.SuiteError('capacity mosaic execution id is missing')
            wait_applied(platform,path,64,1,saved['version'])
            execution=scene.wait_execution(client,'manager',identifier,1800)
            elapsed=time.monotonic()-started
        finally:
            try:samples=sampler.finish()
            finally:
                evidence={'leaf_concurrency':leaf_concurrency,'repetition':repetition,'samples':sampler.samples,
                          'before':sampler.before,'after':getattr(sampler,'after',None)}
                (Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/(name+'-measurements.json')).write_text(json.dumps(evidence,sort_keys=True))
        metrics=validate_measurement(samples,sampler.before,sampler.after,64,1,saved['version'],submitted_tasks=1)
        public_execution=execution
        execution=scopes.execution(public_execution,report['identity'],'manager')
        facts,scan=validate_directory_execution(execution,source_locator,name,target_engine)
        if scan in scans:raise scene.SuiteError('capacity mosaic automatic scan reused')
        verification={'dataset_name':name,'task_id':task_id,'execution_id':identifier,
            'leaf_concurrency':leaf_concurrency,'repetition':repetition,'elapsed_seconds':elapsed,
            'timing_method':DIRECTORY_TIMING_METHOD,
            'policy_version':saved['version'],'runtime_container_id':container['Id'],
            'measurement':metrics,'owner_facts':facts,'automatic_scan_execution_id':scan,
            'physical_verified':False,'physical':None}
        verification_path=Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/(name+'-verification.json')
        verification_path.write_text(json.dumps(verification,sort_keys=True))
        actual=physical('verify-mosaic',[name])
        if actual.get('source_unchanged') is not True:raise scene.SuiteError('capacity mosaic source changed')
        hashes={key:value['sha256'] for key,value in actual['rasters'].items()}
        if expected_hashes is not None and hashes!=expected_hashes:raise scene.SuiteError('capacity mosaic raster bytes differ between runs')
        expected_hashes=hashes
        verification.update(physical_verified=True,physical=actual)
        verification_path.write_text(json.dumps(verification,sort_keys=True))
        scans.add(scan);scan_execution=scene.wait_execution(client,'meta',scan,600)
        scan_observation=validate_directory_scan(execution,scan_execution,scan,report['identity'],target_engine)
        verification['automatic_scan_observation']=scan_observation
        verification_path.write_text(json.dumps(verification,sort_keys=True))
        authorization.ensure(target_engine,'raster-target/'+name)
        item=scene.support.find_fixture_item(client,target_engine,'raster-target/'+name,'mosaic whole target')
        item=client.request('GET',f'/api/v1/meta/items/{item["id"]}',(200,)).payload
        validate_directory_item_identity(item,facts['outputs'][0],target_engine,name)
        persisted=scopes.produced_item(item,execution,scan,report['identity'])
        sizes=validate_directory_item(persisted,actual)
        verification.update(target_item_id=item['id'],target_item_fingerprint=item['fingerprint'],size_evidence=sizes,monitor_verified=False)
        verification_path.write_text(json.dumps(verification,sort_keys=True))
        monitor=scene.obj(client.request('GET',f'/api/v1/monitor/executions/by-execution-id/{identifier}',(200,)).payload,'mosaic Monitor')
        if monitor.get('status')!='success' or monitor.get('module')!='manager' or monitor.get('task_type')!=MOSAIC_TASK_TYPE:
            raise scene.SuiteError('capacity mosaic Monitor differs from its parent')
        scene.support.validate_monitor_lineage(monitor,facts)
        verification['monitor_verified']=True
        verification_path.write_text(json.dumps(verification,sort_keys=True))
        rescan_proof=verify_basic_rescans(env,client,item,identifier,report['identity'],scopes)
        verification['basic_rescans']=rescan_proof
        verification_path.write_text(json.dumps(verification,sort_keys=True))
        if physical('purge-mosaic',[name]).get('source_unchanged') is not True:
            raise scene.SuiteError('capacity mosaic purge changed source')
        report['mosaic_groups'].append({'dataset_name':name,'task_id':task_id,'execution_id':identifier,
            'leaf_concurrency':leaf_concurrency,'repetition':repetition,'configured_cache_mib':64,'running_limit':1,
            'elapsed_seconds':elapsed,'timing_method':DIRECTORY_TIMING_METHOD,
            'policy_version':saved['version'],'runtime_container_id':container['Id'],'runtime_pid':container['State']['Pid'],
            'measurement':metrics,'samples':samples,'target_item_id':item['id'],'target_item_fingerprint':item['fingerprint'],
            'automatic_scan_execution_id':scan,'basic_rescans':rescan_proof,
            'automatic_scan_observation':scan_observation,
            'owner_facts':facts,'size_evidence':sizes,'physical':actual,'purge':'passed'})
        save_progress(Path(env['ADDP_ONLINE_ARTIFACT_DIR']),report)
    if len(report['mosaic_groups'])!=4 or expected_hashes is None:
        raise scene.SuiteError('capacity mosaic matrix is incomplete')
    report['mosaic_raster_sha256']=expected_hashes


def run_capacity(env,authorization,scopes):
    client=scene.GatewayClient(env['GATEWAY_URL'],env['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'],30)
    platform=scene.GatewayClient(env['GATEWAY_URL'],env['ADDP_ONLINE_RASTER_POLICY_PLATFORM_TOKEN'],30)
    tenant=scene.GatewayClient(env['GATEWAY_URL'],env['ADDP_ONLINE_RASTER_POLICY_TENANT_TOKEN'],30)
    identity=scene.validate_identity(client,scene.positive(int(env['ADDP_ONLINE_TEST_TENANT_ID']),'tenant'),
                                     required_permissions=scene.PERMISSIONS|MANAGER_PERMISSIONS)
    engines=platform.request('GET','/api/v1/system/platform/engine-raster-policies/engines',(200,)).payload
    if len(engines)!=1:raise scene.SuiteError('capacity requires one shared runtime')
    engine=scene.positive(engines[0]['id'],'engine');path=f'/api/v1/system/platform/engine-raster-policies/{engine}'
    tenant_path=f'/api/v1/system/tenant/engine-raster-policies/{engine}'
    client.request('GET',path,(403,));tenant.request('GET',path,(403,))
    original=platform.request('GET',path,(200,)).payload['policy']
    quota=tenant.request('GET',tenant_path,(200,)).payload['quota']
    if quota['running'] is not None or quota['waiting'] is not None:raise scene.SuiteError('capacity tenant must inherit policy')
    source_engine=int(env['ADDP_ONLINE_RASTER_SOURCE_ENGINE_ID']);target_engine=int(env['ADDP_ONLINE_RASTER_TARGET_ENGINE_ID'])
    scene.support.wait_for_meta_scan(client,source_engine,time.monotonic()+600)
    authorization.ensure(source_engine,'raster-source/'+fixture.SOURCE_NAME)
    source=scene.support.find_fixture_item(client,source_engine,'raster-source/'+fixture.SOURCE_NAME,'capacity source')
    locator=scene.support.build_item_locator(source_engine,source)
    image=json.loads(fixture.shared.command(['docker','image','inspect',env['ADDP_ONLINE_RASTER_RUNTIME_IMAGE']]))[0]['Id']
    report={'schema_version':'addp.raster-workflow-capacity-online/v1','suite':SUITE,'run_id':env['ADDP_ONLINE_TEST_RUN_ID'],
        'identity':identity,'image_id':image,'source_item_id':source['id'],'groups':[],'mosaic_groups':[],'result':'failed',
        'source':json.loads((Path(env['ADDP_ONLINE_SECRET_DIR'])/'capacity-source-evidence.json').read_text())}
    scans=set(); hashes=set()
    try:
        for repetition,cache,running in matrix():
            current=platform.request('GET',path,(200,)).payload['policy']
            body={key:current[key] for key in ('running','waiting','cache_mib','default_tenant_running','default_tenant_waiting')}
            body.update(version=current['version'],running=running,waiting=2,cache_mib=cache,default_tenant_running=running,default_tenant_waiting=2)
            saved=platform.request('PUT',path,(200,),body).payload['policy']
            fixture.require_disk(Path(env['ADDP_ONLINE_SECRET_DIR']))
            container=restart_runtime(image);directory=cgroup_directory(container['State']['Pid'])
            sampler=Sampler(directory,platform,path);sampler.start()
            started=time.monotonic();names=[f'capacity-c{cache}-n{running}-r{repetition}-{index}.cog.tif' for index in range(2)]
            executions=[]
            try:
                identifiers=[scene.submit(client,engine,workflow(locator,target_engine,name),execution_timeout=900) for name in names]
                wait_applied(platform,path,cache,running,saved['version'])
                for identifier in identifiers:executions.append(scene.wait_execution(client,'develop',identifier,1200))
                elapsed=time.monotonic()-started
            finally:
                try:
                    samples=sampler.finish()
                finally:
                    # Preserve observations even when execution, OOM or admission validation fails.
                    evidence={'cache_mib':cache,'running_limit':running,'repetition':repetition,
                        'policy_version':saved['version'],'runtime_container_id':container['Id'],
                        'before':sampler.before,'after':getattr(sampler,'after',None),'samples':sampler.samples}
                    evidence_path=Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/f'capacity-c{cache}-n{running}-r{repetition}-measurements.json'
                    evidence_path.write_text(json.dumps(evidence,sort_keys=True))
            metrics=validate_measurement(samples,sampler.before,sampler.after,cache,running,saved['version'])
            native=physical('verify',names);cases=[]
            if native['source_unchanged'] is not True or native['source_sha256']!=report['source']['source_sha256']:
                raise scene.SuiteError('capacity source changed')
            for name,identifier,execution in zip(names,identifiers,executions):
                target_locator=f'addp://engine/{target_engine}/path/raster-target/{name}?type=object'
                public_execution=execution
                execution=scopes.execution(public_execution,identity,'develop')
                facts,scan=scene.validate_success(execution,[locator],target_locator,'create',expectation())
                if scan in scans:raise scene.SuiteError('capacity scan reused')
                scans.add(scan);scene.wait_execution(client,'meta',scan,600)
                authorization.ensure(target_engine,'raster-target/'+name)
                item=scene.support.find_fixture_item(client,target_engine,'raster-target/'+name,'capacity target')
                item=scopes.produced_item(item,execution,scan,identity)
                artifact=execution['metadata']['result']['final_result'];actual=native['outputs'][name]
                scene.strict_sizes(artifact,actual,item);scene.validate_target_item(item,expectation(),actual)
                graph=scene.wait_lineage(client,source['id'],item['id'],identifier,120)
                monitor=client.request('GET',f'/api/v1/monitor/executions/by-execution-id/{identifier}',(200,)).payload
                if monitor.get('status')!='success' or monitor.get('execution_id')!=identifier or monitor.get('module')!='develop':
                    raise scene.SuiteError('capacity Monitor differs from Develop')
                scene.support.validate_monitor_lineage(monitor,facts)
                hashes.add(actual['sha256'])
                cases.append({'execution_id':identifier,'target_name':name,'target_item_id':item['id'],
                    'artifact':artifact,'data_item_size_bytes':item['size_bytes'],'physical':actual,
                    'automatic_scan_execution_id':scan,'lineage':graph})
            if physical('purge',names)['source_unchanged'] is not True:raise scene.SuiteError('capacity purge changed source')
            group={'cache_mib':cache,'running_limit':running,'repetition':repetition,'elapsed_seconds':elapsed,
                'policy_version':saved['version'],'runtime_container_id':container['Id'],'runtime_pid':container['State']['Pid'],
                'timing_method':'submit-two-through-parent-terminal-excludes-oracle-and-scan','measurement':metrics,'samples':samples,'cases':cases,'purge':'passed'}
            report['groups'].append(group)
            save_progress(Path(env['ADDP_ONLINE_ARTIFACT_DIR']),report)
        if len(hashes)!=1 or len(scans)!=24:raise scene.SuiteError('capacity results differ between groups')
        run_directories(env,client,platform,path,image,source_engine,target_engine,report,scans,authorization,scopes)
        if len(scans)!=28:raise scene.SuiteError('capacity automatic scans are incomplete')
        report.update(result='passed',automatic_scans=28,basic_rescans=8,output_sha256=next(iter(hashes)))
        return report
    finally:
        current=platform.request('GET',path,(200,)).payload['policy']
        fields=('running','waiting','cache_mib','default_tenant_running','default_tenant_waiting')
        platform.request('PUT',path,(200,),{**{key:original[key] for key in fields},'version':current['version']})


def run(env):
    authorization=CapacityMetadataGrants(env)
    failure=None; report=None; evidence_errors=[]
    try:
        report=run_capacity(env,authorization,MosaicScopeFixture(env))
    except BaseException as error:
        failure=error
        try:authorization.record_failure('business',error)
        except Exception:evidence_errors.append('business')
    try:
        cleanup=authorization.cleanup()
    except BaseException as error:
        try:authorization.record_failure('cleanup',error)
        except Exception:evidence_errors.append('cleanup')
        if failure is None: raise
        first=capacity_error_evidence(failure)
        raise scene.SuiteError('capacity business failed ('+first['stage']+'.'+first['code']+') and metadata Grant cleanup also failed') from None
    if evidence_errors:
        raise scene.SuiteError('capacity failure evidence could not be persisted; cleanup was attempted') from None
    if failure is not None: raise failure
    report['metadata_authorization']={**authorization.report,'cleanup':cleanup}
    return report


def capacity_error_message(error):
    return 'Raster capacity Online failed: '+json.dumps(capacity_error_evidence(error),sort_keys=True)


def main():
    report=run(dict(os.environ));print(json.dumps(report,sort_keys=True))


if __name__=='__main__':
    try:main()
    except Exception as error:
        print(capacity_error_message(error),file=sys.stderr)
        raise SystemExit(1)

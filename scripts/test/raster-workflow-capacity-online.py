#!/usr/bin/env python3
"""Manual full-scene T4 capacity evidence through authoritative policy and Develop."""
from __future__ import annotations
import importlib
import json
import math
import os
from pathlib import Path
import sys
import threading
import time

scene=importlib.import_module('scripts.test.raster-workflow-online')
fixture=importlib.import_module('business.scripts.online-raster-capacity-fixture')
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


def cgroup_directory(pid,proc=Path('/proc'),root=Path('/sys/fs/cgroup')):
    groups=[line.split(':',2) for line in (proc/str(pid)/'cgroup').read_text().splitlines()]
    unified=[parts[2] for parts in groups if len(parts)==3 and parts[0]=='0' and not parts[1]]
    if len(unified)!=1 or '..' in Path(unified[0]).parts:
        raise scene.SuiteError('capacity requires an unambiguous cgroup v2 path')
    return root/unified[0].lstrip('/')


def read_metrics(directory,proc=Path('/proc')):
    events={key:int(value) for key,value in (line.split() for line in (directory/'memory.events').read_text().splitlines())}
    cpu={key:int(value) for key,value in (line.split() for line in (directory/'cpu.stat').read_text().splitlines())}
    maximum=int((directory/'memory.max').read_text()); quota,period=map(int,(directory/'cpu.max').read_text().split())
    if maximum!=2*(1<<30) or quota/period!=4 or int((directory/'memory.swap.max').read_text())!=0:
        raise scene.SuiteError('capacity observed cgroup limits are wrong')
    high_water={}
    for value in (directory/'cgroup.procs').read_text().split():
        try:
            status=(proc/value/'status').read_text()
            for line in status.splitlines():
                if line.startswith('VmHWM:'): high_water[value]=int(line.split()[1])*1024
        except FileNotFoundError:
            continue
    return {'memory_current_bytes':int((directory/'memory.current').read_text()),
        'memory_peak_bytes':int((directory/'memory.peak').read_text()),'memory_limit_bytes':maximum,
        'cpu_quota':quota/period,'cpu_usage_usec':cpu['usage_usec'],
        'oom':events.get('oom',0),'oom_kill':events.get('oom_kill',0),
        'memory_max_events':events.get('max',0),'swap_current_bytes':int((directory/'memory.swap.current').read_text()),
        'process_peak_rss_bytes':sum(high_water.values())}


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


def validate_directory_execution(execution,source_locator,name):
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
        or len(outputs)!=1 or outputs[0].get('locator')!=f'/vsis3/raster-target/{name}/mosaic.addp.json'
        or len(operations)!=1 or operations[0].get('kind')!='derive' or operations[0].get('operator')!=MOSAIC_TASK_TYPE
        or len(scans)!=1 or not isinstance(scans[0],str) or not scans[0]):
        raise scene.SuiteError('capacity mosaic scope/output/automatic scan facts differ')
    return facts,scans[0]


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


def run_directories(env,client,platform,path,image,source_engine,target_engine,report,scans):
    source=physical('seed-mosaic',[])
    if source.get('source_unchanged') is not True or set(source.get('tiles',{}))!=set(fixture.mosaic_windows()):
        raise scene.SuiteError('capacity mosaic source preparation is incomplete')
    report['mosaic_source']=source
    scene.support.wait_for_meta_scan(client,source_engine,time.monotonic()+600)
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
        facts,scan=validate_directory_execution(execution,source_locator,name)
        if scan in scans:raise scene.SuiteError('capacity mosaic automatic scan reused')
        scans.add(scan);scene.wait_execution(client,'meta',scan,600)
        item=scene.support.find_fixture_item(client,target_engine,'raster-target/'+name,'mosaic whole target')
        item=client.request('GET',f'/api/v1/meta/items/{item["id"]}',(200,)).payload
        actual=physical('verify-mosaic',[name]);sizes=validate_directory_item(item,actual)
        if actual.get('source_unchanged') is not True:raise scene.SuiteError('capacity mosaic source changed')
        hashes={key:value['sha256'] for key,value in actual['rasters'].items()}
        if expected_hashes is not None and hashes!=expected_hashes:raise scene.SuiteError('capacity mosaic raster bytes differ between runs')
        expected_hashes=hashes
        monitor=scene.obj(client.request('GET',f'/api/v1/monitor/executions/by-execution-id/{identifier}',(200,)).payload,'mosaic Monitor')
        if monitor.get('status')!='success' or monitor.get('module')!='manager' or monitor.get('task_type')!=MOSAIC_TASK_TYPE:
            raise scene.SuiteError('capacity mosaic Monitor differs from its parent')
        scene.support.validate_monitor_lineage(monitor,facts)
        if physical('purge-mosaic',[name]).get('source_unchanged') is not True:
            raise scene.SuiteError('capacity mosaic purge changed source')
        report['mosaic_groups'].append({'dataset_name':name,'task_id':task_id,'execution_id':identifier,
            'leaf_concurrency':leaf_concurrency,'repetition':repetition,'configured_cache_mib':64,'running_limit':1,
            'elapsed_seconds':elapsed,'timing_method':'submit-one-Manager-task-through-parent-terminal-excludes-oracle-and-scan',
            'policy_version':saved['version'],'runtime_container_id':container['Id'],'runtime_pid':container['State']['Pid'],
            'measurement':metrics,'samples':samples,'target_item_id':item['id'],'automatic_scan_execution_id':scan,
            'owner_facts':facts,'size_evidence':sizes,'physical':actual,'purge':'passed'})
        save_progress(Path(env['ADDP_ONLINE_ARTIFACT_DIR']),report)
    if len(report['mosaic_groups'])!=4 or expected_hashes is None:
        raise scene.SuiteError('capacity mosaic matrix is incomplete')
    report['mosaic_raster_sha256']=expected_hashes


def run(env):
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
                facts,scan=scene.validate_success(execution,[locator],target_locator,'create',expectation())
                if scan in scans:raise scene.SuiteError('capacity scan reused')
                scans.add(scan);scene.wait_execution(client,'meta',scan,600)
                item=scene.support.find_fixture_item(client,target_engine,'raster-target/'+name,'capacity target')
                artifact=execution['metadata']['result']['final_result'];actual=native['outputs'][name]
                scene.strict_sizes(artifact,actual,item);scene.validate_target_item(item,expectation(),actual)
                graph=scene.wait_lineage(client,source['id'],item['id'],identifier,120)
                monitor=client.request('GET',f'/api/v1/monitor/executions/by-execution-id/{identifier}',(200,)).payload
                if monitor.get('status')!='success' or monitor.get('metadata',{}).get('lineage_facts')!=facts:
                    raise scene.SuiteError('capacity Monitor differs from Develop')
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
        run_directories(env,client,platform,path,image,source_engine,target_engine,report,scans)
        if len(scans)!=28:raise scene.SuiteError('capacity automatic scans are incomplete')
        report.update(result='passed',automatic_scans=28,output_sha256=next(iter(hashes)))
        return report
    finally:
        current=platform.request('GET',path,(200,)).payload['policy']
        fields=('running','waiting','cache_mib','default_tenant_running','default_tenant_waiting')
        platform.request('PUT',path,(200,),{**{key:original[key] for key in fields},'version':current['version']})


def main():
    report=run(dict(os.environ));print(json.dumps(report,sort_keys=True))


if __name__=='__main__':
    try:main()
    except Exception as error:
        print('Raster capacity Online failed: '+(str(error) if isinstance(error,scene.SuiteError) else type(error).__name__),file=sys.stderr)
        raise SystemExit(1)

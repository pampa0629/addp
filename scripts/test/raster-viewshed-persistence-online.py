#!/usr/bin/env python3
"""Sixteen complete COGs, thirty-six exact bilingual failures and 712 actual rereads."""
from __future__ import annotations

import base64
import importlib
import json
import math
import os
from pathlib import Path
import re
import subprocess
import sys
import time

base = importlib.import_module('scripts.test.raster-statistics-percentile-workflow-online')
file_flow = importlib.import_module('scripts.test.raster-zonal-statistics-persistence-online')
fixture = importlib.import_module('business.scripts.online-raster-viewshed-fixture')
lineage = importlib.import_module('scripts.utils.online_raster_lineage')
raster, support, readonly = base.raster, base.support, base.readonly
obj, array, positive, SuiteError = base.obj, base.array, base.positive, base.SuiteError
SubmittedLanguageClient, BudgetClient = base.SubmittedLanguageClient, base.BudgetClient
encoded, immutable = base.encoded, base.immutable
SUITE = fixture.SUITE
PREFIX='viewshed'
ARCHIVE_NAME='viewshed-cogs'
MANIFEST_KEY='archived_cogs'
COUNTS=(16, 36, 712, 53)


# Each case is an ordinary JSON request that must reach the actual sight node.
FAILURES = {
 'band_zero':('band','flat.tif',{'band':0}), 'band_high':('band','flat.tif',{'band':99}),
 'band_alpha':('alpha','coverage.tif',{'band':4}), 'rotated':('grid','rotated.tif',{}),
 'south_up':('grid','south-up.tif',{}), 'geographic':('grid','geographic.tif',{}),
 'no_crs':('grid','no-crs.tif',{}), 'no_grid':('grid','no-grid.tif',{}),
 'unit':('unit','flat.tif',{'elevation_unit':'yard'}), 'unit_conflict':('unit-conflict','flat.tif',{'elevation_unit':'foot'}),
 'scale':('scale','scaled.tif',{}), 'offset':('scale','offset.tif',{}),
 'observer_short':('xy','flat.tif',{'observer_xy':[.5]}),
 'observer_outside':('outside','flat.tif',{'observer_xy':[-.5,258.5]}),
 'observer_invalid':('invalid','unknown.tif',{'observer_xy':[257.5,258.5]}),
 'observer_negative':('height','flat.tif',{'observer_height_m':-1.}),
 'target_negative':('height','flat.tif',{'target_height_m':-1.}),
 'radius_zero':('radius','flat.tif',{'radius_metres':0.})}
ERRORS = {
 'xy':('DEM 单观察点可视域的观察点必须是两个有限投影坐标 [x,y]','DEM viewshed requires two finite projected observer coordinates [x,y]'),
 'height':('DEM 单观察点可视域的两端离地高度必须是有限非负米数','DEM viewshed requires finite nonnegative heights above ground in metres'),
 'outside':('DEM 单观察点可视域的端点超出栅格范围','DEM viewshed endpoint is outside the raster'),
 'invalid':('DEM 单观察点可视域的端点落入无效高程像元','DEM viewshed endpoint has no valid elevation'),
 'band':('DEM 单观察点可视域必须显式选择有效的一基高程数据波段','DEM viewshed requires an explicit valid one-based elevation band'),
 'alpha':('DEM 单观察点可视域不能选择 alpha 波段作为高程','DEM viewshed cannot use an alpha band as elevation'),
 'grid':('DEM 单观察点可视域需要已知线性单位的二维投影及保持北向轴手性的正交网格，请显式重投影','DEM viewshed requires a two-dimensional projected orthogonal grid with known linear units and north-up handedness; reproject explicitly'),
 'unit':('DEM 单观察点可视域必须明确声明 metre、foot 或 us_survey_foot 高程单位','DEM viewshed requires elevation_unit metre, foot or us_survey_foot'),
 'unit-conflict':('DEM 单观察点可视域高程波段单位未知或与声明单位冲突','DEM viewshed band unit is unknown or conflicts with elevation_unit'),
 'scale':('DEM 单观察点可视域前必须显式应用高程 scale/offset','DEM viewshed requires elevation scale/offset to be applied first'),
 'radius':('DEM 单观察点可视域必须显式提供有限正米制半径','DEM viewshed requires an explicit finite positive radius in metres')}


class DiagnosticClient(base.DiagnosticClient):
    diagnostic_name = 'viewshed-submission-error.json'
    diagnostic_schema = 'addp.raster-viewshed-submission-diagnostic/v1'


def definition(sources, parent, name, *, invalid=None, language='zh-cn'):
    if (name not in fixture.CASES or invalid not in (None, *FAILURES)
            or language not in ('zh-cn', 'en') or (invalid and name != 'flat.cog.tif')):
        raise SuiteError('unexpected viewshed case')
    source = fixture.CASES[name][0]
    params = fixture.parameters(name)
    if invalid:
        _, source, changed = FAILURES[invalid]
        params.update(changed)
    tasks = [
        {'id': 'source', 'operator': 'raster_load', 'depends_on': [], 'params': {'locator': sources[source]}},
    ]
    prior='source'
    if fixture.CASES[name][1]=='fill':
        tasks.append({'id':'fill','operator':'raster_fill_depressions','depends_on':['source'],
                      'params':{'input_raster':{'$ref':'source'},'band':2,'elevation_unit':params['elevation_unit']}})
        prior='fill';params['band']=1
    tasks.extend([
        {'id':'view','operator':'raster_viewshed','depends_on':[prior], 'params':{'input_raster':{'$ref':prior},**params}},
        {'id':'save','operator':'raster_save','depends_on':['view'],'params':{'input_raster':{'$ref':'view'},
            'target_parent_locator':parent,'target_name':f'failed-{invalid}-{language}.cog.tif'if invalid else name,
            'write_mode':'create','profile':'cog','blocksize':128,'overview_resampling':'nearest'}}])
    return {'tasks':tasks}



def expected_lineage(source, target):
    return {'schema_version': 'addp.lineage-facts/v1',
            'inputs': [{'port': 'input.source', 'locator': source}],
            'outputs': [{'port': 'output.save', 'locator': target, 'write_mode': 'create'}],
            'operations': [{'kind': 'derive', 'operator': 'raster_save', 'input_ports': ['input.source'],
                            'output_ports': ['output.save']}]}


def validate_artifact(artifact, name):
    expected=fixture.artifact_expectation(name)
    raster.validate_raster_artifact(artifact,expected,dtype='Byte')
    if artifact.get('transform')!=expected['transform'] or artifact.get('extent')!=expected['extent']:
        raise SuiteError('exact full affine grid or extent differs')


def physical(repository, env, action, names, timeout):
    result = subprocess.run(
        [sys.executable, 'business/scripts/online-raster-viewshed-fixture.py', action, *names],
        cwd=repository, env=env, capture_output=True, text=True, timeout=min(150, timeout))
    if result.returncode:
        raise SuiteError('owned DEM/COG physical verification failed: '+result.stderr[-1200:])
    return obj(json.loads(result.stdout), 'physical report')


def verify_sources(facts):
    if set(facts) != set(fixture.SOURCES):
        raise SuiteError('physical source inventory differs')
    for fact in facts.values():
        if (type(fact.get('size_bytes')) is not int or fact['size_bytes'] <= 0
                or not isinstance(fact.get('sha256'), str) or not re.fullmatch('[0-9a-f]{64}', fact['sha256'])):
            raise SuiteError('invalid source size or SHA-256')


def check_and_archive(report, names, sources, frozen, archive, previous, scope_hash):
    if report.get('runtime_workspaces_removed')is not True or report.get('sources')!=sources:
        raise SuiteError('source bytes changed or Runtime workspace cleanup proof is missing')
    targets=obj(report.get('targets'),'physical targets');manifest=obj(report.get('archived_cogs'),'original COG manifest')
    if set(targets)!=set(names)or set(manifest)!=set(names)or {p.name for p in archive.iterdir()}!=set(names):
        raise SuiteError('physical or archived COG inventory differs')
    if names and report.get('oracle_cache')!={'scope_sha256':scope_hash,'read_only_expected':True,'expected_arrays_loaded':len(names),'full_target_checks':len(names)}:
        raise SuiteError('frozen oracle scope or complete reread count differs')
    receipts={}
    for name,fact in targets.items():
        raw=fixture.digest(archive/name)
        receipt={'path':archive.name+'/'+name,**raw}
        if (type(fact.get('size_bytes'))is not int or fact['size_bytes']<=0 or
            any(fact.get(k)!=v for k,v in raw.items())or manifest[name]!=receipt or
            any(fact.get(k)is not True for k in('cog_valid','full_pixel_and_mask_verification','independent_cell_clipping_verified','nearest_overviews_verified'))or
            type(fact.get('has_overviews'))is not bool or type(fact.get('overview_count'))is not int or type(fact.get('valid_pixels'))is not int or not isinstance(fact.get('state_counts'),dict)or any(type(v)is not int for v in fact['state_counts'].values())or fact.get('state_counts')!=frozen[name]['state_counts']or
            fact.get('valid_pixels')!=sum(frozen[name]['state_counts'][str(code)]for code in(0,1,2))):
            raise SuiteError('complete physical COG or original size/hash differs')
        if name in previous and previous[name]!=receipt:raise SuiteError('previous COG changed')
        receipts[name]=receipt
    return receipts


def diagnostic_fact(identifier, problem, language):
    return {'execution_id': identifier, 'task': 'view', 'problem': problem, 'language': language,
            'exact_cause': ERRORS[FAILURES[problem][0]][language == 'en'], 'source': 'develop_runtime_log'}


def invalid_diagnostic(repository, env, identifier, problem, language, timeout):
    expected = diagnostic_fact(identifier, problem, language)
    root = Path(env.get('ADDP_RUNTIME_LOG_ROOT', repository/'logs/runtime'))
    if not root.is_absolute():
        root = repository/root
    deadline = time.monotonic()+timeout
    while time.monotonic() < deadline:
        for path in (root/'develop').glob('*/*.jsonl'):
            for line in path.read_text().splitlines():
                try:
                    event = json.loads(line)
                except json.JSONDecodeError:
                    continue
                message = event.get('message', '')
                if (event.get('module_name') == 'develop' and event.get('role') == 'backend'
                        and f'[DevExecutor] 工作流引擎调用失败: execution_id={identifier} err=' in message
                        and message.endswith('任务 view 执行失败: '+expected['exact_cause'])):
                    return expected
        time.sleep(min(1, max(0, deadline-time.monotonic())))
    raise SuiteError('missing exact viewshed node and submitted-language diagnostic')


def active_derivations(client, item, direction):
    graph = obj(client.request('GET', f'/api/v1/meta/lineage/graph?subject_kind=data_item&item_id={item}&direction={direction}',
                               (200,)).payload, 'source lineage graph')
    if graph.get('truncated') is not False:
        raise SuiteError('Meta lineage graph is incomplete')
    return [edge for edge in array(graph.get('edges'), 'edges')
            if edge.get('status') == 'active' and edge.get('relation_kind') == 'derive']


def source_edges(client, item):
    return sorted(json.dumps(edge, sort_keys=True) for edge in active_derivations(client, item, 'downstream'))


def item_snapshot(client, item):
    return obj(client.request('GET', f'/api/v1/meta/items/{item}', (200,)).payload, 'Meta item')


def current(execution, identity, identifier, dag, runtime, language, status):
    readonly.actor(execution, identity, 'develop')
    readonly.execution_snapshot(execution, dag, runtime)
    if (execution.get('execution_id') != identifier or 'tenant_id' in execution or execution.get('task_type') != 'workflow'
            or execution.get('status') != status or execution['execution_config'].get('display_language') != language):
        raise SuiteError('current execution identity, status or submitted language differs')


class ViewshedMetadata(raster.WorkflowMetadata):
    """Only this NFS suite: exact structural Grants and run-bound persisted facts."""
    scan_preparation = False

    def source_scan_principal(self):
        return self.identity['principal_id']

    prefix=PREFIX
    suite=SUITE
    fixture=fixture

    def file_contract(self,name):
        return 'media','tiff',('single','multi')

    def __init__(self, env, client, engine, identity, source_scan, remaining):
        from datetime import datetime,timedelta,timezone
        self.env,self.client,self.engine,self.identity=env,client,engine,identity
        self.registration=importlib.import_module('scripts.test.online-engine-registration')
        self.observer=lineage
        self.boundary();self.owner=self.observer.inspect();user=self.container_user(self.owner)
        secret,suite,run=lineage.boundary(env);receipt=secret/lineage.RECEIPT
        if (receipt.is_symlink()or not receipt.is_file()or receipt.stat().st_uid!=os.getuid()
            or receipt.stat().st_mode&0o777!=0o600):raise SuiteError('private Hosted owner receipt unavailable')
        owner=json.loads(receipt.read_text())
        if owner!={'schema':'addp.raster-lineage-owner/v1','suite':suite,'run_id':run,
            'container_id':self.owner['Id'],'started_at':self.owner['State']['StartedAt'],'postgres_user':user}:
            raise SuiteError('metadata observer differs from initial Hosted PostgreSQL owner')
        self.path=Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/(self.prefix+'-metadata.json')
        self.source_scan=source_scan;self.bindings={};self.issued={};self.items={}
        self.report={'schema_version':'addp.'+self.prefix+'-metadata/v1','run_id':env['ADDP_ONLINE_TEST_RUN_ID'],
            'identity':identity,'container_id':self.owner['Id'],'started_at':self.owner['State']['StartedAt'],
            'pending':[],'issued':[],'facts':[],'queries':[],'failures':[],'cleanup':'pending'}
        self.expires_at=(datetime.now(timezone.utc)+timedelta(seconds=remaining())).isoformat();self.save()
        issuer=BudgetClient(support.GatewayClient(env['GATEWAY_URL'],env['ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN'],30),remaining)
        self.grants=self.registration.ExactFileMetadataGrants(issuer,client,int(identity['tenant_id']),
            on_change=self.checkpoint,on_evidence=self.capture,scan_preparation=self.scan_preparation)

    def boundary(self):
        _,suite,run=lineage.boundary(self.env)
        if suite!=self.suite or self.env.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS')!='600' or run!=f"{self.prefix}-{self.env.get('GITHUB_RUN_ID')}-{self.env.get('GITHUB_RUN_ATTEMPT')}":
            raise SuiteError('viewshed metadata requires its exact 600-second Hosted run')

    def execution(self,identifier,dag,runtime,language,status):
        if (not re.fullmatch(raster.UUID_PATTERN,identifier) or status not in ('success','failed')
            or language not in ('zh-cn','en') or len(encoded(dag))>65536):
            raise SuiteError('invalid current execution evidence scope')
        statement="""BEGIN READ ONLY; SET LOCAL statement_timeout='3s'; SET LOCAL lock_timeout='1s';
SELECT json_build_object('execution_id',e.execution_id,'tenant_id',e.tenant_id,
 'principal_id',e.actor_principal_id,'triggered_by',e.triggered_by,'module',e.module,
 'task_type',e.task_type,'status',e.status,
 'snapshot_matches',e.execution_config->'content'->'workflow_definition'=:'workflow'::jsonb
 AND e.execution_config->>'engine_id'=:'runtime'
 AND e.execution_config->>'display_language'=:'language')
FROM common.task_executions e WHERE e.execution_id=:'execution'
 AND e.tenant_id=:'tenant'::integer AND e.actor_principal_id=:'principal'::bigint
 AND e.triggered_by=:'principal'::integer AND e.module='develop' AND e.task_type='workflow'
 AND e.status=:'status' AND e.created_at>=:'started_at'::timestamptz LIMIT 1;
COMMIT;
"""
        expected={'execution_id':identifier,'tenant_id':int(self.identity['tenant_id']),
            'principal_id':int(self.identity['principal_id']),'triggered_by':int(self.identity['principal_id']),
            'module':'develop','task_type':'workflow','status':status,'snapshot_matches':True}
        result=self.query(statement,{'execution':identifier,'tenant':self.identity['tenant_id'],
            'principal':self.identity['principal_id'],'workflow':encoded(dag).decode(),'runtime':runtime,
            'language':language,'status':status,'started_at':self.owner['State']['StartedAt']},set(expected))
        if any(type(result[k])is not type(v)or result[k]!=v for k,v in expected.items()):
            raise SuiteError('private execution User/Tenant, state or frozen snapshot differs')
        self.report.setdefault('executions',[]).append(result);self.save()
        return result

    def ensure(self,name):
        self.boundary();self.container_user(self.observer.inspect())
        if name not in self.fixture.SOURCES and name not in {'outputs/'+n for n in self.fixture.TARGETS}:
            raise SuiteError('metadata target is outside fixed viewshed fixture')
        if name.startswith('outputs/')and name not in self.bindings:raise SuiteError('unconfirmed viewshed target')
        data_type,format_name,layouts=self.file_contract(name)
        facts=self.grants.metadata_facts(self.engine,name,'nfs',data_type=data_type,format_name=format_name,layouts=layouts)
        target=self.grants.target(self.engine,name,'nfs',layout=facts['boundary']['layout'])
        expected=(facts['boundary_hash'],facts['item_fingerprint'],target)
        if name in self.issued and self.issued[name]!=expected:raise SuiteError('metadata fingerprint or boundary changed')
        response=obj(self.client.request('POST','/api/v1/system/engine-access/metadata-checks/meta-query',(200,),{'targets':[target]}).payload,'metadata visibility')
        rows=response.get('targets')
        if not response.get('observed_at')or not isinstance(rows,list)or len(rows)!=1 or rows[0].get('target')!=target or type(rows[0].get('visible'))is not bool:raise SuiteError('invalid metadata visibility facts')
        if not rows[0]['visible']:
            if name in self.issued:raise SuiteError('metadata visibility was lost')
            receipt=self.grants.grant(self.engine,name,'nfs',data_type=data_type,format_name=format_name,layout=facts['boundary']['layout'],expires_at=self.expires_at)
            if (receipt.get('boundary_hash'),receipt.get('item_fingerprint'))!=expected[:2]:raise SuiteError('Grant identity changed')
            self.report['issued'].append(receipt);self.save()
        self.issued[name]=expected
        return facts

    def find(self,name,binding=None):
        if binding is not None:
            if (name not in {'outputs/'+n for n in self.fixture.TARGETS} or set(binding)!={'execution','scan','locator'}
                or any(not re.fullmatch(raster.UUID_PATTERN,binding.get(key,''))for key in('execution','scan'))
                or binding['locator']!=f'addp://engine/{self.engine}/path/{name}?type=file'):
                raise SuiteError('wrong current viewshed execution or automatic scan binding')
            self.bindings[name]=binding
        facts=self.ensure(name)
        public=support.find_fixture_item(self.client,self.engine,name,'viewshed TIFF')
        attrs=obj(public.get('attributes'),'public structural metadata')
        if (public.get('fingerprint')!=facts['item_fingerprint']or any(public.get(k)is not None for k in('size_bytes','row_count','data_updated_at'))
            or set(attrs)-{'schema_version','item','type_info','capabilities'}or attrs.get('type_info',{}).get('media')
            or attrs.get('capabilities',{}).get('spatial',{}).get('extent')is not None):
            raise SuiteError('public structural projection or exact Grant identity differs')
        self.items[positive(public.get('id'),'public item')]=public
        return public

    def snapshot(self,identifier):
        if identifier not in self.items:raise SuiteError('private snapshot has no ordinary public item')
        public=self.items[identifier];name=public['full_name'];self.find(name)
        output=name.startswith('outputs/');binding=self.bindings.get(name)
        joins="JOIN common.task_executions s ON s.execution_id=:'scan'"
        extra=" AND s.parent_execution_id IS NULL"
        values={'item':identifier,'engine':self.engine,'tenant':self.identity['tenant_id'],'principal':self.identity['principal_id'] if output else self.source_scan_principal(),
                'full_name':name,'fingerprint':public['fingerprint'],'started_at':self.owner['State']['StartedAt'],'scan':self.source_scan}
        if output:
            joins+=" JOIN common.task_executions e ON e.execution_id=:'execution' AND s.parent_execution_id=e.execution_id"
            extra=""" AND e.module='develop' AND e.task_type='workflow' AND e.status='success'
 AND e.tenant_id=i.tenant_id AND e.actor_principal_id=s.actor_principal_id AND e.triggered_by=s.triggered_by
 AND e.created_at>=:'started_at'::timestamptz
 AND e.metadata->'lineage_facts'->'outputs' @> jsonb_build_array(jsonb_build_object('locator',:'locator'))
 AND e.metadata->'result'->'meta_scan_runs' @> jsonb_build_array(jsonb_build_object(
 'execution_id',s.execution_id,'target_locator',:'locator','status','submitted'))"""
            values.update(scan=binding['scan'],execution=binding['execution'],locator=binding['locator'])
        statement="""BEGIN READ ONLY; SET LOCAL statement_timeout='3s'; SET LOCAL lock_timeout='1s';
SELECT json_build_object('id',i.id,'tenant_id',i.tenant_id,'engine_id',i.engine_id,'node_id',i.node_id,
 'name',i.name,'full_name',i.full_name,'item_type',i.item_type,'fingerprint',i.fingerprint,
 'size_bytes',i.size_bytes,'attributes',i.attributes,'scanned_depth',i.scanned_depth,
 'scanned_at',i.scanned_at,'data_updated_at',i.data_updated_at)
FROM meta.meta_item i """+joins+"""
WHERE i.id=:'item'::integer AND i.tenant_id=:'tenant'::integer AND i.engine_id=:'engine'::integer
 AND i.full_name=:'full_name' AND i.fingerprint=:'fingerprint' AND i.deleted_at IS NULL
 AND i.created_at>=:'started_at'::timestamptz AND s.created_at>=:'started_at'::timestamptz
 AND s.tenant_id=i.tenant_id AND s.actor_principal_id=:'principal'::bigint AND s.triggered_by=:'principal'::integer
 AND s.module='meta' AND s.task_type='scan' AND s.status='success'
 AND s.execution_config->>'engine_id'=i.engine_id::text AND octet_length(i.attributes::text)<=32768"""+extra+";\nCOMMIT;\n"
        fields={'id','tenant_id','engine_id','node_id','name','full_name','item_type','fingerprint','size_bytes','attributes','scanned_depth','scanned_at','data_updated_at'}
        result=self.query(statement,values,fields)
        if any(result.get(k)!=public.get(k)for k in('id','tenant_id','engine_id','node_id','name','full_name','item_type','fingerprint')):
            raise SuiteError('persisted and ordinary public item identity differ')
        return result


def success_language(index):
    return 'zh-cn' if index%2==0 else 'en'


def validate_frozen(baseline):
    frozen=obj(baseline.get('expectations'),'frozen independent reference')
    if (set(frozen)!=set(fixture.CASES) or baseline.get('expected_array_count')!=16
        or any(frozen[n]['artifact']!=fixture.artifact_expectation(n)for n in frozen)):
        raise SuiteError('sixteen frozen independent three-state references required')
    for field in('scope_sha256','anchor_sha256'):
        if not isinstance(baseline.get(field),str)or not re.fullmatch('[0-9a-f]{64}',baseline[field]):raise SuiteError('invalid frozen oracle scope or anchor')
    for fact in frozen.values():
        counts=fact.get('state_counts')
        if not isinstance(counts,dict)or set(counts)!={'0','1','2','255'}or any(type(v)is not int or v<0 for v in counts.values())or sum(counts.values())!=fixture.WIDTH*fixture.HEIGHT:raise SuiteError('invalid frozen complete state counts')


def validate_final(result,name,frozen):
    validate_artifact(obj(result.get('final_result'),'Byte COG artifact'),name)


def validate_persisted(result,native,item,name):
    raster.validate_target_item(item,fixture.artifact_expectation(name),native)
    return raster.strict_sizes(result['final_result'],native,item,name)


def source_scan_client(env, client, identity, remaining):
    return client, identity


def check_scan_inventory(client, initializer, scan, scans):
    if base.scan_ids(client) != sorted([scan, *scans]):
        raise SuiteError('unexpected source or business scan inventory')


def run_scenario(repository, env, client, physical_runner=physical, diagnostic_runner=invalid_diagnostic,
                 lineage_reader=lineage.read_current, metadata_factory=None, profile=None):
    profile = profile or sys.modules[__name__]
    fixture = profile.fixture
    prefix = profile.PREFIX
    suite = profile.SUITE
    anchor_key = "ADDP_ONLINE_"+prefix.upper().replace("-","_")+"_ORACLE_ANCHOR_SHA256"
    started = time.monotonic()
    budget = float(env.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS', '600'))
    if budget != 600:
        raise SuiteError('viewshed business budget must be exactly 600 seconds')

    def remaining():
        left = budget-(time.monotonic()-started)
        if left <= 0:
            raise SuiteError('viewshed business budget exhausted')
        return left

    client = BudgetClient(client, remaining)
    directory = Path(env['ADDP_ONLINE_ARTIFACT_DIR'])
    directory.mkdir(parents=True, exist_ok=True)
    archive = directory/profile.ARCHIVE_NAME
    archive.mkdir(exist_ok=False)
    stages = []

    def progress(status):
        (directory/(prefix+'-progress.json')).write_bytes(encoded({
            'suite': suite, 'result': status, 'budget_ms': 600000,
            'elapsed_ms': round((time.monotonic()-started)*1000), 'stages': stages,
        }))

    def record_terminal(identifier, dag, language, status):
        execution = raster.wait_execution(client, 'develop', identifier, remaining(), expected=status)
        current(execution, identity, identifier, dag, runtime, language, status)
        metadata.execution(identifier,dag,runtime,language,status)
        monitor = client.request('GET', '/api/v1/monitor/executions/by-execution-id/'+identifier, (200,)).payload
        readonly.actor(monitor, identity, 'develop')
        if (monitor.get('execution_id') != identifier or 'tenant_id' in monitor or monitor.get('status') != status):
            raise SuiteError('Monitor execution identity or terminal state differs')
        case = dag['tasks'][-1]['params']['target_name']
        immutable(directory/f'develop-{case}.json', encoded(execution))
        immutable(directory/f'monitor-{case}.json', encoded(monitor))
        return execution, monitor

    progress('running')
    try:
        tenant = positive(env['ADDP_ONLINE_TEST_TENANT_ID'], 'tenant')
        if tenant <= 1:
            raise SuiteError('viewshed acceptance forbids the default tenant')
        identity = raster.validate_identity(client, tenant, required_permissions=raster.EDITOR_PERMISSIONS)
        principal = int(identity['principal_id'])
        engine = positive(env['ADDP_ONLINE_'+prefix.upper().replace('-','_')+'_NFS_ENGINE_ID'], 'NFS engine')
        scopes = {name: f'addp://engine/{engine}/path/{name}?type=file' for name in fixture.SOURCES}
        initializer, source_identity = profile.source_scan_client(env, client, identity, remaining)
        scan = support.wait_for_meta_scan(initializer, engine, time.monotonic()+remaining(), scope={'targets': list(scopes.values())})
        metadata = (metadata_factory or profile.ViewshedMetadata)(env,client,engine,identity,scan,remaining)
        producer_env={k:v for k,v in env.items()if k!='ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN'}
        items = {name: metadata.find(name) for name in scopes}
        sources = {name: scopes[name]+f'&item_id={items[name]["id"]}' for name in scopes}
        before = {name: metadata.snapshot(item['id']) for name, item in items.items()}
        immutable(directory/'source-meta-before.json', encoded(before))
        scanned = raster.wait_execution(initializer, 'meta', scan, remaining())
        source_scan = base.validate_scan(scanned, scan, None, tenant, int(source_identity['principal_id']), engine)
        immutable(directory/'meta-source-scan.json', encoded(scanned))
        profile.check_scan_inventory(client, initializer, scan, [])
        runtimes = array(client.request('GET', '/api/v1/develop/workflow-engines', (200,)).payload, 'Runtime engines')
        selected = [value for value in runtimes if value.get('engine_type') == 'geopython_workflow'
                    and value.get('connection_status') == 'online']
        if len(selected) != 1:
            raise SuiteError('requires exactly one online same-source product Runtime')
        runtime = positive(selected[0].get('id'), 'Runtime')
        parent = f'addp://engine/{engine}/path/outputs?type=directory'

        baseline = physical_runner(repository, producer_env, 'prepare-oracle', [], remaining())
        env=dict(env,**{anchor_key:baseline['anchor_sha256']})
        producer_env=dict(producer_env,**{anchor_key:baseline['anchor_sha256']})
        frozen = obj(baseline.get('expectations'), 'frozen independent reference')
        source_bytes = obj(baseline.get('sources'), 'physical sources')
        profile.verify_sources(source_bytes)
        profile.validate_frozen(baseline)
        oracle = immutable(directory/(prefix+'-expectations.json'), encoded(frozen))
        profile.check_and_archive(baseline, [], source_bytes, frozen, archive, {},baseline['scope_sha256'])
        if any(type(before[n].get('size_bytes')) is not int or before[n]['size_bytes'] != fact['size_bytes']
               for n, fact in source_bytes.items()):
            raise SuiteError('source physical and Meta sizes differ')

        cases, failures, names, scans = [], [], [], []
        target_metadata, previous = {}, {}
        full_checks, physical_calls = 0, 1
        execution_ids = set()

        def submit(dag, language, status):
            identifier = raster.submit(SubmittedLanguageClient(client, language), runtime, dag,
                                       execution_timeout=min(180, max(1, int(remaining()))))
            if identifier in execution_ids:
                raise SuiteError('duplicate Develop execution identity')
            execution_ids.add(identifier)
            execution, monitor = record_terminal(identifier, dag, language, status)
            return identifier, execution, monitor

        for index, name in enumerate(fixture.TARGETS):
            stages.append({'case': name, 'status': 'running'})
            progress('running')
            language = profile.success_language(index)
            dag = profile.definition(sources, parent, name)
            identifier, execution, monitor = submit(dag, language, 'success')
            source_name = fixture.CASES[name][0]
            source = sources[source_name]
            target = f'addp://engine/{engine}/path/outputs/{name}?type=file'
            expected = profile.expected_lineage(source, target)
            facts, automatic_scan, result = file_flow.validate_success(execution, [source], target)
            lineage.validate(facts, expected, public=True)
            proof = lineage.collect(env, suite, identifier, name, expected, execution, tenant, principal, dag, lineage_reader)
            if obj(monitor.get('metadata'), 'Monitor metadata').get('lineage_facts') != facts:
                raise SuiteError('Monitor current public lineage differs')
            profile.validate_final(result,name,frozen[name])
            if automatic_scan in [scan, *scans]:
                raise SuiteError('duplicate automatic COG scan')
            scans.append(automatic_scan)
            scanned = raster.wait_execution(client, 'meta', automatic_scan, remaining())
            scan_identity = base.validate_scan(scanned, automatic_scan, identifier, tenant, principal, engine)
            immutable(directory/f'meta-{name}.json', encoded(scanned))
            names.append(name)
            native = physical_runner(repository, producer_env, 'verify', names, remaining())
            previous = profile.check_and_archive(native, names, source_bytes, frozen, archive, previous,baseline['scope_sha256'])
            physical_calls += 1
            full_checks += len(names)
            hit = metadata.find('outputs/'+name,binding={'execution':identifier,'scan':automatic_scan,'locator':target})
            item = metadata.snapshot(hit['id'])
            descriptor = obj(obj(item.get('attributes'), 'Meta attributes').get('item'), 'Meta file')
            if (item.get('id') != hit['id'] or hit['id'] in target_metadata or not item.get('scanned_at')
                    or item.get('item_type') != 'file' or item.get('full_name') != 'outputs/'+name
                    or descriptor.get('format') != ('csv' if name.endswith('.csv') else 'tiff') or type(item.get('size_bytes')) is not int
                    or item['size_bytes'] != previous[name]['size_bytes']):
                raise SuiteError('COG identity or equal positive physical/Meta size differs')
            sizes=profile.validate_persisted(result,native['targets'][name],item,name)
            target_metadata[hit['id']] = item
            immutable(directory/f'target-{name}-meta-before.json', encoded(item))
            source_id = items[source_name]['id']
            relation = raster.wait_lineage(client, source_id, hit['id'], identifier, remaining())
            edges = active_derivations(client, hit['id'], 'upstream')
            if len(edges) != 1 or (edges[0].get('source', {}).get('item_id'),
                                   edges[0].get('target', {}).get('item_id'),
                                   edges[0].get('evidence', {}).get('execution_id')) != (source_id, hit['id'], identifier):
                raise SuiteError('Meta must expose exactly one current DEM-to-COG derivation')
            cases.append({'name': name, 'execution_id': identifier, 'target_item_id': hit['id'],
                          'target_locator': target, 'automatic_scan_execution_id': automatic_scan,
                          'scan_identity': scan_identity, 'lineage_evidence': proof, 'meta_relation': relation,
                          'physical_size_bytes': previous[name]['size_bytes'], 'meta_size_bytes': item['size_bytes'],'size_evidence':sizes})
            stages[-1]['status'] = 'passed'
            progress('running')

        profile.check_scan_inventory(client, initializer, scan, scans)
        edges = {name: source_edges(client, item['id']) for name, item in items.items()}
        for problem in profile.FAILURES:
            for language in ('zh-cn', 'en'):
                stages.append({'problem': problem, 'language': language, 'status': 'running'})
                progress('running')
                dag = profile.definition(sources, parent, fixture.TARGETS[0], invalid=problem, language=language)
                identifier, execution, monitor = submit(dag, language, 'failed')
                for terminal in (execution, monitor):
                    readonly.no_publication(terminal)
                    if obj(terminal.get('error_details'), 'terminal error').get('category') != 'execution_failed':
                        raise SuiteError('wrong failed execution category')
                diagnostic = diagnostic_runner(repository, env, identifier, problem, language, remaining())
                if diagnostic != profile.diagnostic_fact(identifier, problem, language):
                    raise SuiteError('wrong exact failure node, language or cause')
                opposite = SubmittedLanguageClient(client, 'en' if language == 'zh-cn' else 'zh-cn')
                queried = opposite.request('GET', '/api/v1/develop/executions/'+identifier, (200,)).payload
                if any(queried.get(key) != execution.get(key) for key in ('error_msg', 'error_details', 'execution_config')):
                    raise SuiteError('query language rewrote stored failure')
                profile.check_scan_inventory(client, initializer, scan, scans)
                if any(source_edges(client, item['id']) != edges[n] for n, item in items.items()):
                    raise SuiteError('failed execution added derived lineage')
                native = physical_runner(repository, producer_env, 'verify', names, remaining())
                previous = profile.check_and_archive(native, names, source_bytes, frozen, archive, previous,baseline['scope_sha256'])
                physical_calls += 1
                full_checks += len(names)
                failures.append({**diagnostic, 'no_publication_or_scan_or_lineage': True})
                stages[-1]['status'] = 'passed'
                progress('running')

        if (len(cases), len(failures), full_checks, physical_calls) != profile.COUNTS:
            raise SuiteError(f'{suite} requires exact success/failure/reread/call counts {profile.COUNTS}')
        final_targets = {key: metadata.snapshot(key) for key in target_metadata}
        final_sources = {name: metadata.snapshot(item['id']) for name, item in items.items()}
        immutable(directory/'target-meta-after.json', encoded(final_targets))
        immutable(directory/'source-meta-after.json', encoded(final_sources))
        if final_targets != target_metadata or final_sources != before:
            raise SuiteError('source or prior target Meta snapshot changed')
        lineage.verify_archive(env, suite, cases)
        cog_manifest = {name: {key: value for key, value in fact.items() if key != 'row'} for name, fact in previous.items()}
        manifest = immutable(directory/(profile.ARCHIVE_NAME+'-manifest.json'), encoded(cog_manifest))
        progress('passed')
        remaining()
    except BaseException:
        progress('failed')
        raise
    finally:
        pending_error=sys.exc_info()[1];cleanup_errors=[]
        if 'metadata'in locals():
            try:metadata.cleanup()
            except BaseException as failure:cleanup_errors.append(type(failure).__name__)
        if 'producer_env'in locals():
            try:
                cleaned=physical_runner(repository,producer_env,'cleanup-oracle',[],max(.1,budget-(time.monotonic()-started)))
                if cleaned.get('oracle_cache_removed')is not True:raise SuiteError('oracle cleanup proof missing')
            except BaseException as failure:cleanup_errors.append(type(failure).__name__)
        if cleanup_errors:
            progress('failed')
            if pending_error is None:raise SuiteError('viewshed cleanup failed: '+','.join(cleanup_errors))
            immutable(directory/(prefix+'-cleanup-errors.json'),encoded({'business_error':type(pending_error).__name__,'cleanup_errors':cleanup_errors}))
        if pending_error is None:remaining()
    return {'schema_version': 'addp.'+suite+'/v1', 'suite': suite, 'result': 'passed',
            'run_id': env['ADDP_ONLINE_TEST_RUN_ID'], 'identity': identity, 'runtime_engine_id': runtime,
            'sources': sources, 'physical_sources': source_bytes, 'source_scan_identity': source_scan,
            'cases': cases, 'failed_parameters': failures, profile.MANIFEST_KEY: cog_manifest,
            'manifest': manifest, 'oracle': oracle, 'source_and_target_metadata_unchanged': True,
            'full_target_checks': full_checks, 'physical_verification_calls': physical_calls,
            'elapsed_ms': round((time.monotonic()-started)*1000), 'business_budget_ms': 600000,'metadata_evidence':prefix+'-metadata.json','oracle_cache_removed':True}


def main():
    env = dict(os.environ)
    client = DiagnosticClient(env['GATEWAY_URL'], env['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'], 30,
                              env['ADDP_ONLINE_ARTIFACT_DIR'])
    report = run_scenario(Path(__file__).resolve().parents[2], env, client)
    immutable(Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/'viewshed-persistence.json', encoded(report))
    print(json.dumps(report, ensure_ascii=False, sort_keys=True))


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print(f'Viewshed acceptance failed: {error if isinstance(error, SuiteError) else type(error).__name__}',
              file=sys.stderr)
        raise SystemExit(1)

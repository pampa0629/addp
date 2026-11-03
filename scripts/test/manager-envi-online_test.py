import copy
import io
import zipfile
import subprocess
from urllib.parse import urlparse,parse_qs
import hashlib
import importlib
from pathlib import Path
import unittest
import tempfile
import json
import sys
from types import SimpleNamespace
from unittest import mock

FIXTURE=importlib.import_module('business.scripts.online-envi-fixture')
OWNER=importlib.import_module('scripts.test.manager-internal-artifact-lineage-online')
CASE=importlib.import_module('scripts.test.manager-envi-online')
class ENVIHostedCaseTest(unittest.TestCase):
    def test_read_approval_precedes_consumer_catalog_and_is_withdrawn_on_failure(self):
        events=[]
        class Authorization:
            operator='34';recipient='33'
            def __init__(self,*args,**kwargs):pass
            def grant(self,model):
                events.append(('approve',model))
                return dict(request_id='owned-read',boundary_hash='a'*64)
            def cleanup(self):events.append(('withdraw',))
        def find(*args):
            self.assertEqual(events[-1][0],'approve','catalog read preceded exact source approval')
            raise RuntimeError('owned catalog failure')
        def physical(action,request=None):
            events.append((action,))
            return {'sources':FIXTURE.sources()} if action=='seed' else dict(objects_removed=True,files_removed=True)
        client=mock.Mock();client.request.return_value=SimpleNamespace(payload=[],raw=b'')
        owner=SimpleNamespace(GatewayClient=lambda *a:client,positive_int=lambda v,*a:int(v),
            REQUIRED_PERMISSIONS=OWNER.REQUIRED_PERMISSIONS,validate_user_identity=lambda *a:{'principal_id':'33'},
            wait_for_meta_scan=lambda *a,**k:None,find_fixture_item=find)
        with tempfile.TemporaryDirectory() as directory:
            env=dict(ADDP_ONLINE_MANAGER_CASE='native-envi',ADDP_ONLINE_ARTIFACT_DIR=directory,
                ADDP_ONLINE_TEST_TENANT_ID='2',ADDP_ONLINE_TEST_RUN_ID='test-envi',GATEWAY_URL='http://fixture',
                ADDP_ONLINE_TEST_USER_ACCESS_TOKEN='consumer',ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN='issuer',
                ADDP_ONLINE_MANAGER_MINIO_BUCKET='bucket',ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID='12',ADDP_ONLINE_MANAGER_NFS_ENGINE_ID='14')
            with mock.patch.object(CASE.GRANTS,'ModelSourceAuthorization',Authorization):
                with self.assertRaisesRegex(RuntimeError,'owned catalog failure'):
                    CASE.run(Path('/repo'),env,owner,physical_runner=physical)
            report=json.loads(Path(directory,'manager-envi.json').read_text())
            self.assertEqual((report['stage'],report['error']),('metadata','RuntimeError'))
        self.assertEqual([e[0]for e in events],['seed','approve','withdraw','delete'])
        model=events[1][1]
        self.assertEqual(model['fingerprint'],hashlib.sha256(b'12:bucket/envi/bsq.img').hexdigest())
        self.assertEqual([r['role']for r in model['composition']],['raster','header'])

    def metadata_pair(self,spec,prefix='envi/'):
        expected=[dict(path=prefix+spec['name'],role='raster',primary=True,required=True),dict(path=prefix+spec['header'],role='header',primary=False,required=True)]
        safe=dict(schema_version=1,item=dict(data_type='media',format='envi',layout='multi'))
        if spec['source_srid']:safe['capabilities']={'spatial':{'srid':spec['source_srid']}}
        attrs=copy.deepcopy(safe)
        attrs['item']['refs']=[dict(expected[0],extension='.img'),{k:v for k,v in dict(expected[1],extension='.hdr').items()if k!='primary'}]
        attrs.update(storage={'total_size':spec['size_bytes']},format_info={'envi':dict(profile='envi-standard-pair/v1',scan_complete=True,composition_complete=True,interleave=spec['interleave'],band_count=3,header_sha256=spec['members'][spec['header']]['sha256'],raster_sha256=spec['sha256'])},type_info={'media':{'width':256,'height':256}})
        return dict(attributes=safe),dict(data={'object':{'attributes':attrs}}),expected

    def test_public_projection_and_guarded_content_are_separate_strict_proofs(self):
        for spec in FIXTURE.sources():
            detail,content,refs=self.metadata_pair(spec)
            CASE.validate_content_metadata(detail,content,spec,refs)
            for change in (lambda d:d['attributes'].update(format_info={'envi':{'header_sha256':'leak'}}),
                           lambda d:d['attributes']['item'].update(refs=refs),
                           lambda d:d['attributes'].update(storage={'total_size':12})):
                bad=copy.deepcopy(detail);change(bad)
                with self.assertRaises(ValueError):CASE.validate_content_metadata(bad,content,spec,refs)
            for change in (lambda d:d['data']['object']['attributes']['format_info']['envi'].update(header_sha256='wrong'),
                           lambda d:d['data']['object']['attributes']['item']['refs'].pop(),
                           lambda d:d['data']['object']['attributes']['storage'].update(total_size=0)):
                bad=copy.deepcopy(content);change(bad)
                with self.assertRaises(ValueError):CASE.validate_content_metadata(detail,bad,spec,refs)
            if spec['source_srid']:
                bad=copy.deepcopy(detail);bad['attributes']['capabilities']['spatial']['extent']=[1,2,3,4]
                with self.assertRaises(ValueError):CASE.validate_structural_metadata(bad,spec)

    def test_real_browser_metadata_adapter_rejects_meta_leaks_and_content_bypass(self):
        root=Path(__file__).resolve().parents[2]
        text=(root/'console/frontend/e2e/online/manager-img.spec.js').read_text()
        fn=text[text.index('export function enviContentMetadata'):text.index("test('IMG native")].replace('export function','function',1)
        cases=[]
        for spec in FIXTURE.sources():
            detail,content,_=self.metadata_pair(spec);cases.append(dict(meta=detail,content=content,spec=spec))
        script="const assert = require('node:assert/strict');\n"+fn+"\nconst cases = JSON.parse(process.argv[1]); for(const c of cases){ assert.deepEqual(enviContentMetadata(c.meta,c.content,c.spec).attributes,c.content.data.object.attributes); for(const key of ['format_info','storage','type_info']){ const bad=structuredClone(c.meta); bad.attributes[key]={leak:true}; assert.throws(()=>enviContentMetadata(bad,c.content,c.spec)); } assert.throws(()=>enviContentMetadata(c.meta,{},c.spec)); }"
        subprocess.run(['node','-e',script,json.dumps(cases)],cwd=root,check=True,capture_output=True)
        self.assertIn('enviContentMetadata(structuralMeta, await json(await api.get',text)

    def full_scenario(self,directory,*,browser_failure=False):
        specs=FIXTURE.sources();specs_by_name={s['name']:s for s in specs};pairs={s['name']:FIXTURE.pair(s)for s in specs}
        active={};layout={};version={};ids={};records={};events=[];receipts=[];tasks={};self_test=self
        def key_name(engine,name):return (engine,name)
        def authorized(key):
            model=active.get(key)
            return bool(model) and model['composition'][1]['path'].endswith((key[1].split('/')[-1]+'.hdr')if layout.get(key)else specs_by_name[key[1].split('/')[-1]]['header'])
        class Authorization:
            operator='34';recipient='33'
            def __init__(self,issuer,consumer,report,**options):
                self.engine=report['engine_id'];self.owned=[]
                self_test.assertEqual(options['consumer_permissions'],OWNER.REQUIRED_PERMISSIONS)
                self_test.assertEqual(options['issuer_permissions'],CASE.GRANTS.REGISTRATION.SOURCE_INITIALIZER_PERMISSIONS)
            def facts(self,model):
                return {'boundary_hash':hashlib.sha256(json.dumps(model['composition']).encode()).hexdigest()}
            def grant(self,model):
                key=key_name(self.engine,model['full_name']);self_test.assertFalse(authorized(key))
                self_test.assertEqual(model['fingerprint'],hashlib.sha256(f'{self.engine}:{model["full_name"]}'.encode()).hexdigest())
                active[key]=copy.deepcopy(model);receipt=dict(request_id=str(len(receipts)+1),boundary_hash=self.facts(model)['boundary_hash'])
                self.owned.append((receipt['request_id'],key));receipts.append(receipt);events.append(('approve',key));return receipt
            def revoke(self,command):
                pair=next(p for p in self.owned if p[0]==command);self.owned.remove(pair)
                active.pop(pair[1],None);events.append(('withdraw',pair[1]))
            def cleanup(self):
                for command,key in list(self.owned):self.revoke(command)
        def raw_zip(key):
            spec=specs_by_name[key[1].split('/')[-1]];payload=pairs[spec['name']]
            out=io.BytesIO()
            with zipfile.ZipFile(out,'w',compression=zipfile.ZIP_DEFLATED)as z:
                for name,raw in payload.items():z.writestr(spec['name']+'.hdr'if layout.get(key)and name==spec['header']else name,raw)
            return out.getvalue()
        def request(method,url,statuses,body=None,headers=None):
            q=parse_qs(urlparse(url).query);path=urlparse(url).path;status=200;payload={};raw=b''
            if path.startswith('/api/v1/meta/engines/'):
                engine=int(path.split('/')[5]);payload=[copy.deepcopy(item)for key,item in records.items()if key[0]==engine and authorized(key)]
            elif path.startswith('/api/v1/meta/items/'):
                key=ids[int(path.split('/')[-1])];status=200 if authorized(key)else 404;payload=records[key]
            elif path in ('/api/v1/manager/downloads/file','/api/v1/manager/preview','/api/v1/manager/quick-view/capability'):
                locator=urlparse(q['locator'][0]);engine=int(locator.path.split('/')[1])
                locator_query=parse_qs(locator.query)
                self_test.assertEqual(set(locator_query),{'type','item_id'},'content locator must identify a scanned data item')
                self_test.assertEqual(ids[int(locator_query['item_id'][0])],(engine,locator.path.split('/path/',1)[1]))
                name=locator.path.split('/path/',1)[1];key=key_name(engine,name);status=200 if authorized(key)else 403
                if path.endswith('downloads/file') and status==200:raw=raw_zip(key)
                elif path.endswith('preview') and status==200:payload=self_test.metadata_pair(specs_by_name[name.split('/')[-1]],name.rsplit('/',1)[0]+'/')[1]
                else:payload=dict(can_use_quick_view=False,available_actions=[])
            elif path.startswith('/cached/'):
                key=tasks[int(path.split('/')[-1])];status=206 if authorized(key)and not version.get(key)else 403;raw=b'c'*64 if status==206 else b''
            elif path=='/api/v1/manager/raster_cog':payload={'data':[{'id':int(q['task_id'][0]),'storage_ref':'owned-cog'}]}
            elif path.startswith('/api/v1/manager/raster_cog/'):payload={'id':int(path.split('/')[-1]),'storage_ref':'owned-cog'}
            else:raise AssertionError((method,url,statuses))
            self_test.assertIn(status,statuses,(path,status,statuses));return SimpleNamespace(payload=payload,raw=raw)
        client=SimpleNamespace(request=request)
        def find(client,engine,name,label):
            key=key_name(engine,name);self_test.assertTrue(authorized(key),'consumer catalog before read Grant')
            spec=specs_by_name[name.split('/')[-1]];detail,_,_=self_test.metadata_pair(spec)
            item=dict(id=len(ids)+1,engine_id=engine,full_name=name,fingerprint=hashlib.sha256(f'{engine}:{name}'.encode()).hexdigest(),attributes=detail['attributes'],item_type='object'if engine==12 else 'file')
            ids[item['id']]=key;records[key]=item;return item
        def generate(client,fixture,tenant,timeout,physical,source_facts):
            key=ids[fixture.item['id']];self_test.assertTrue(authorized(key));fixture.task_id=fixture.item['id'];fixture.execution_id='owned-execution';fixture.result_id=fixture.task_id
            fixture.artifact={'preview_url':'/cached/'+str(fixture.task_id)};tasks[fixture.task_id]=key;events.append(('generate',key))
            self_test.assertTrue(physical('verify',{'sha256':'owned'})['source_unchanged'])
        def physical(action,body=None):
            events.append((action,))
            if action=='seed':return {'sources':specs}
            if action in ('header-layout','header-version'):
                key=key_name(12 if body['source_type']=='minio'else 14,('bucket/'if body['source_type']=='minio'else '')+'envi/bsq.img')
                (layout if action=='header-layout'else version)[key]=body['changed'];return {}
            if action=='verify':return {'sources_unchanged':True}
            if action=='delete':return dict(objects_removed=True,files_removed=True)
            if action=='residuals':return dict(objects_absent=True,temporary_inputs_removed=True,source_unchanged=True)
            return dict(object_deleted=True,temporary_inputs_removed=True,source_unchanged=True)
        identity=dict(principal_id='33',tenant_id='2',permissions_verified=sorted(OWNER.REQUIRED_PERMISSIONS))
        def browser(repo,env,evidence,owner):
            self_test.assertNotIn('ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN',env);self_test.assertNotIn('ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',env)
            if browser_failure:raise RuntimeError('owned browser failure')
            cases=[]
            for source in evidence['cases']:
                actual={k:source[k]for k in ('case','item_id','locator','sha256','source_srid')}
                actual.update(download_sha256=source['sha256'],download_bundle={'members':source['download_members']},metadata_verified=True,source_preview_verified=True,metadata_projection_verified=True,content_metadata_verified=True,current_user_download_verified=True)
                if source['source_srid']is None:actual.update(map_absent=True,source_image_absent=True,unsupported_message_visible=True,generation_unavailable=True)
                else:actual.update(preview_url=source['preview_url'],range_loaded=True,map_loaded=True,reopened=True,reused_result=True,pixels=dict(cyan=128,magenta=128,yellow=128))
                Path(directory,'envi-'+source['case']+'.png').write_bytes(b'\x89PNG\r\n\x1a\nmock')
                cases.append(actual)
            return dict(schema_version='addp.manager-raster-media-browser/v4',result='passed',run_id=evidence['run_id'],identity=identity,source_format='envi',generation_requests=0,failed_business_responses=0,warnings_errors=0,cases=cases)
        def cleanup(client,fixture,*args):fixture.cleanup=dict(task_deleted=True);events.append(('task-delete',));return [{'id':fixture.result_id}]
        owner=SimpleNamespace(GatewayClient=lambda *a:client,positive_int=lambda v,*a:int(v),validate_user_identity=lambda *a:identity,REQUIRED_PERMISSIONS=OWNER.REQUIRED_PERMISSIONS,SuiteError=OWNER.SuiteError,
            wait_for_meta_scan=lambda *a,**k:None,find_fixture_item=find,ArtifactFixture=OWNER.ArtifactFixture,build_item_locator=OWNER.build_item_locator,
            validate_model_bundle=OWNER.validate_model_bundle,generate_raster_cog=generate,cleanup_managed_artifact=cleanup,RASTER_TASK_TYPE=OWNER.RASTER_TASK_TYPE)
        env=dict(ADDP_ONLINE_MANAGER_CASE='native-envi',ADDP_ONLINE_ARTIFACT_DIR=directory,ADDP_ONLINE_TEST_TENANT_ID='2',ADDP_ONLINE_TEST_RUN_ID='envi-test',GATEWAY_URL='http://fixture',ADDP_ONLINE_TEST_USER_ACCESS_TOKEN='consumer',ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN='issuer',ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN='engine',ADDP_ONLINE_MANAGER_MINIO_BUCKET='bucket',ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID='12',ADDP_ONLINE_MANAGER_NFS_ENGINE_ID='14')
        with mock.patch.object(CASE.GRANTS,'ModelSourceAuthorization',Authorization),mock.patch.object(CASE.MEDIA,'run_browser',browser):
            if browser_failure:
                with self.assertRaisesRegex(RuntimeError,'owned browser failure'):CASE.run(Path('/repo'),env,owner,physical_runner=physical)
                report=json.loads(Path(directory,'manager-envi.json').read_text())
            else:report=CASE.run(Path('/repo'),env,owner,physical_runner=physical)
        self.assertEqual(active,{});self.assertEqual(len(receipts),20)
        self.assertEqual((report['source_authorization']['operator_principal_id'],report['source_authorization']['recipient_principal_id']),('34','33'))
        self.assertEqual(report['source_authorization']['cleanup'],dict(read_grants_revoked=True,metadata_details_hidden=6))
        self.assertEqual(sum(e[0]=='generate'for e in events),4);self.assertEqual(sum(e[0]=='task-delete'for e in events),4)
        self.assertEqual(report['source_cleanup'],dict(objects_removed=True,files_removed=True));return report

    def test_complete_six_source_owner_flow_and_failure_cleanup(self):
        for failure in (False,True):
            with self.subTest(failure=failure),tempfile.TemporaryDirectory()as directory:
                report=self.full_scenario(directory,browser_failure=failure)
                self.assertEqual(report['schema_version'],'addp.manager-envi/v3')
                self.assertEqual(report['result'],'failed'if failure else 'passed')
                self.assertEqual(len(report['cases']),6)
                self.assertTrue(all(c['original_withdrawal_verified']for c in report['cases']))
                self.assertTrue(all(c['original_withdrawal']['revoked_request_id']!=c['original_withdrawal']['renewed_request_id']for c in report['cases']))
                if failure:self.assertEqual((report['stage'],report['error']),('browser','RuntimeError'))
                for key in ('metadata_projection_verified','content_metadata_verified','current_user_download_verified'):
                    if not failure:
                        observed=copy.deepcopy(report['browser']);observed['cases'][0][key]=False
                        evidence=dict(identity=report['identity'],run_id=report['run_id'],source_format='envi',cases=report['cases'])
                        with self.assertRaises(ValueError):CASE.validate_browser(OWNER,observed,evidence,directory)

    def test_failed_generation_cleanup_uses_actual_results_and_never_invents_output(self):
        for rows in ([],[dict(id=31,storage_ref='owned-partial-result')]):
            with self.subTest(rows=rows):
                fixture=SimpleNamespace(cleanup=dict(task_deleted=True,residual_resources=0))
                client=object();calls=[]
                def clean(actual_client,actual_fixture,timeout,resource,task_type):
                    self.assertIs(actual_client,client);self.assertIs(actual_fixture,fixture)
                    self.assertEqual((timeout,resource,task_type),(12,'raster_cog','cog-task'))
                    return rows
                def physical(action,request):
                    calls.append((action,request))
                    return dict(object_deleted=True,temporary_inputs_removed=True,source_unchanged=True)
                proof=CASE.cleanup_fixture(client,fixture,12,2,SimpleNamespace(cleanup_managed_artifact=clean,RASTER_TASK_TYPE='cog-task'),physical)
                self.assertEqual(proof['results_observed'],len(rows))
                self.assertEqual(calls,[('deleted-cog',dict(result=row,tenant_id=2)) for row in rows])
                self.assertEqual('object_deleted' in proof,bool(rows))
        with self.assertRaisesRegex(ValueError,'incomplete exact physical cleanup'):
            CASE.cleanup_fixture(client,fixture,12,2,SimpleNamespace(cleanup_managed_artifact=clean,RASTER_TASK_TYPE='cog-task'),lambda *_:{})

    def test_cleanup_failure_preserves_original_business_error_and_reports_both(self):
        with tempfile.TemporaryDirectory() as directory:
            env=dict(ADDP_ONLINE_MANAGER_CASE='native-envi',ADDP_ONLINE_ARTIFACT_DIR=directory,ADDP_ONLINE_TEST_TENANT_ID='2',ADDP_ONLINE_TEST_RUN_ID='owned-envi',GATEWAY_URL='http://fixture',ADDP_ONLINE_TEST_USER_ACCESS_TOKEN='user',ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN='issuer')
            failure=RuntimeError('original generation failure')
            calls=[]
            def physical(action,request=None):
                calls.append(action)
                if action=='seed':raise failure
                raise ValueError('separate cleanup error')
            owner=SimpleNamespace(positive_int=lambda *_:2,GatewayClient=lambda *_:object(),validate_user_identity=lambda *_:{})
            with self.assertRaises(RuntimeError) as caught:CASE.run(Path('.'),env,owner,physical_runner=physical)
            self.assertIs(caught.exception,failure)
            report=json.loads(Path(directory,'manager-envi.json').read_text())
            self.assertEqual((report['result'],report['error'],report['cleanup_errors']),('failed','RuntimeError',['ValueError']))
            self.assertEqual(calls,['seed','delete'])

    def test_final_physical_residual_check_is_read_only_and_rejects_orphans(self):
        for leftover in (None,'cog','input'):
            with self.subTest(leftover=leftover),tempfile.TemporaryDirectory() as directory:
                calls=[]
                def listing(bucket,*,prefix,recursive):
                    self.assertEqual(bucket,'manager');self.assertTrue(recursive);calls.append(prefix)
                    return [object()] if (leftover=='cog' and prefix=='tenant_2/cog/') or (leftover=='input' and prefix=='temp/raster-input/tenant_2/') else []
                storage=SimpleNamespace(list_objects=listing)
                request=dict(action='residuals',tenant_id=2,nfs_root=directory,storage=dict(endpoint='fixture',access_key='a',secret_key='b',bucket='sources'),infra=dict(endpoint='infra',access_key='c',secret_key='d',bucket='manager'))
                with mock.patch.dict(sys.modules,{'minio':SimpleNamespace(Minio=lambda *a,**k:storage)}),mock.patch.object(FIXTURE,'sources',return_value=[]):
                    if leftover:
                        with self.assertRaisesRegex(ValueError,'remain'):FIXTURE.worker(request)
                    else:self.assertEqual(FIXTURE.worker(request),dict(objects_absent=True,temporary_inputs_removed=True,source_unchanged=True))
                self.assertEqual(calls[0],'tenant_2/cog/')

    def test_exact_pair_inventory_and_independent_interleave(self):
        specs=FIXTURE.sources()
        self.assertEqual([s['interleave'] for s in specs],['bsq','bil','bip'])
        self.assertEqual([s['source_srid'] for s in specs],[4326,32650,None])
        for spec in specs:
            files=FIXTURE.pair(spec)
            self.assertEqual(set(files),{spec['name'],spec['header']})
            for name,raw in files.items():
                self.assertEqual(spec['members'][name],dict(size_bytes=len(raw),sha256=hashlib.sha256(raw).hexdigest()))
            self.assertEqual(len(files[spec['name']]),196608)
            self.assertTrue(files[spec['header']].startswith(b'ENVI\n'))
        self.assertEqual(list(FIXTURE.pair(specs[0]).values())[0][:3],b'\x00\x00\x00')
        self.assertEqual(list(FIXTURE.pair(specs[2]).values())[0][:3],b'\x00\xff\xff')
    def test_explicit_case_and_owned_physical_guard(self):
        self.assertEqual(OWNER.manager_case({'ADDP_ONLINE_MANAGER_CASE':'native-envi'}),'native-envi')
        with self.assertRaises(ValueError):FIXTURE.physical('seed',Path('.'),{})
        with self.assertRaises(ValueError):FIXTURE.physical('unknown',Path('.'),{})
    def test_same_gate_wires_all_evidence_and_cleanup(self):
        root=Path(__file__).resolve().parents[2]
        runner=(root/'scripts/test/manager-envi-online.py').read_text()
        for check in ('validate_model_bundle','current_source_revocation_verified','generate_raster_cog','deleted-cog','grant.cleanup()','manager-envi.json','composition_change_reapproval_verified','changed_header_cache_rejected'):
            self.assertIn(check,runner)
        browser=(root/'console/frontend/e2e/online/manager-img.spec.js').read_text()
        self.assertIn("envi: ['minio-bsq','minio-bil','minio-bip','nfs-bsq','nfs-bil','nfs-bip']",browser)
        self.assertIn('source.download_members',browser)
        self.assertIn('actual.current_user_download_verified = true',browser)
        self.assertIn('await browserAPIFromResponse(response, env.GATEWAY_URL, expectedIdentity)',browser)
        self.assertIn('manager-envi-online_test.py',(root/'Makefile').read_text())

    def test_source_revocation_rechecks_current_grant_and_preserves_cached_bytes(self):
        old=dict(request_id='00000000-0000-4000-8000-000000000001',boundary_hash='a'*64)
        new=dict(request_id='00000000-0000-4000-8000-000000000002',boundary_hash='a'*64)
        model=dict(format='envi',composition=[{'role':'raster'},{'role':'header'}])
        class SourceLedger:
            def __init__(self,ineffective=False,changed=False):self.active=True;self.renewed=False;self.ineffective=ineffective;self.changed=changed
            def revoke(self,command):
                self.assert_command(command,old['request_id'])
                if not self.ineffective:self.active=False
            def assert_command(self,actual,expected):
                if actual!=expected:raise ValueError('wrong owned command')
            def grant(self,actual):
                if actual!=model or self.active:raise ValueError('new approval must follow the exact withdrawal')
                self.active=True;self.renewed=True;return new
            def request(self,method,path,statuses,headers=None):
                if path=='/api/v1/meta/items/1':
                    if (200 if self.active else 404) not in statuses:raise ValueError('metadata withdrawal ineffective')
                    return SimpleNamespace(payload={'id':1})
                status=206 if self.active else 403
                if method!='GET' or path!='/cached/result' or status not in statuses:raise ValueError('current source decision did not enforce withdrawal')
                if status==206 and headers!={'Range':'bytes=0-63'}:raise ValueError('unbounded cached request')
                return SimpleNamespace(raw=(b'x'*64 if self.renewed and self.changed else b'c'*64) if self.active else b'')
        ledger=SourceLedger()
        receipt,proof=CASE.verify_source_revocation(ledger,ledger,old,model,'/cached/result',item_id=1)
        self.assertEqual(receipt,new)
        self.assertEqual(proof,dict(revoked_request_id=old['request_id'],renewed_request_id=new['request_id'],boundary_hash='a'*64,range_bytes=64,range_sha256=hashlib.sha256(b'c'*64).hexdigest()))
        for ledger in (SourceLedger(ineffective=True),SourceLedger(changed=True)):
            with self.assertRaises(ValueError):CASE.verify_source_revocation(ledger,ledger,old,model,'/cached/result',item_id=1)
        root=Path(__file__).resolve().parents[2]
        runner=(root/'scripts/test/manager-envi-online.py').read_text()
        self.assertIn('issuer_permissions=GRANTS.REGISTRATION.SOURCE_INITIALIZER_PERMISSIONS',runner)
        self.assertNotIn('grant.deny(',runner)
        self.assertNotIn('grant.release(',runner)
        self.assertIn('addp.manager-envi/v3',runner)

    def test_infra_failure_diagnostic_is_owned_bounded_and_redacted(self):
        with tempfile.TemporaryDirectory() as directory:
            env=dict(ADDP_ONLINE_HOSTED='1',GITHUB_ACTIONS='true',RUNNER_OS='Linux',ADDP_ONLINE_MANAGER_CASE='native-envi',ONLINE_SUITE='manager-internal-artifact-lineage',ADDP_ONLINE_ARTIFACT_DIR=directory,INFRA_KAFKA_ADMIN_PASSWORD='private-fixture-password')
            calls=[]
            def docker(command,**options):
                calls.append(command)
                self.assertEqual(options,dict(capture_output=True,text=True,timeout=5))
                if command[1]=='container':
                    data=[{'Config':{'Labels':{'com.docker.compose.project':'addp-infra'},'Env':['DO_NOT_PUBLISH=private-fixture-password']},'State':{'Status':'exited','ExitCode':1,'OOMKilled':False,'Error':'failure private-fixture-password','Health':{'Status':'unhealthy','Log':['DO_NOT_PUBLISH']}}}]
                    return SimpleNamespace(returncode=0,stdout=json.dumps(data))
                self.assertEqual(command,['docker','logs','--tail','80','addp-redpanda'])
                return SimpleNamespace(returncode=0,stdout='x'*10000+'\nboot failure private-fixture-password',stderr='')
            result=FIXTURE.capture_infra_diagnostic(env,runner=docker)
            saved=Path(directory,'envi-infra-redpanda.json').read_text()
            self.assertEqual(json.loads(saved),result)
            self.assertEqual(result['status'],'captured')
            self.assertEqual(result['state']['ExitCode'],1)
            self.assertLessEqual(len(result['log_tail'].encode()),8192)
            self.assertIn('[REDACTED]',saved)
            self.assertNotIn('private-fixture-password',saved)
            self.assertNotIn('DO_NOT_PUBLISH',saved)
            self.assertEqual(len(calls),2)
            calls.clear()
            with self.assertRaises(ValueError):FIXTURE.capture_infra_diagnostic(dict(env,GITHUB_ACTIONS='false'),runner=docker)
            self.assertEqual(calls,[])
            def foreign(command,**options):
                self.assertEqual(command,['docker','container','inspect','addp-redpanda'])
                return SimpleNamespace(returncode=0,stdout=json.dumps([{'Config':{'Labels':{'com.docker.compose.project':'personal'}}}]))
            self.assertEqual(FIXTURE.capture_infra_diagnostic(env,runner=foreign)['status'],'not_owned')

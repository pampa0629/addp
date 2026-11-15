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
    @staticmethod
    def complete_root_stage(*args,**kwargs):
        proof=kwargs['evidence']
        proof.update(result='passed',checks=sorted(CASE.MINIO_ROOT_CHECKS))
        return proof

    @staticmethod
    def complete_workspace_stage(*args,**kwargs):
        proof=kwargs['evidence'];proof.update(result='passed',cleanup=dict(zero_current_source_rules=True))
        return proof

    def test_expired_minio_node_cleanup_requires_database_history(self):
        original=dict(request_id='owned',catalog_path={'engine_id':12},node_recursive=False,recipient_type='user',
            recipient_id='33',action='read',expiry_mode='at_time',expires_at='2000-01-01T00:00:00Z',
            engine_id='12',operator_principal_id='34',revocation=None)
        for changed in (None,dict(action='metadata_read'),dict(revocation={'request_id':'owned'}),dict(expires_at='2001-01-01T00:00:00Z')):
            obj=CASE.GRANTS.ModelSourceAuthorization.__new__(CASE.GRANTS.ModelSourceAuthorization)
            obj.grants=['owned'];obj.confirmed={'owned'};obj.denies=[];obj.node_receipts={'owned':original};obj.base='/owned';obj.recipient='33'
            calls=[]
            def request(method,path,statuses,*args):
                calls.append((method,path))
                if method!='GET':raise RuntimeError('expired Grant must not be revoked again')
                self.assertEqual(parse_qs(urlparse(path).query)['state'],['expired'])
                row=dict(original,**(changed or {}))
                return SimpleNamespace(payload=dict(total=1,data=[row]))
            obj.issuer=SimpleNamespace(request=request)
            if changed:
                with self.assertRaises(CASE.GRANTS.SuiteError):obj.cleanup()
                self.assertEqual(obj.grants,['owned'])
            else:
                obj.cleanup();self.assertEqual(obj.grants,[]);self.assertEqual(obj.node_receipts,{})
                self.assertEqual([method for method,_ in calls],['GET'])

    def test_minio_node_grant_is_one_owned_strict_command(self):
        grant_class=CASE.GRANTS.ModelSourceAuthorization
        target=dict(version='catalog.path/v1',engine_id=12,segments=[
            dict(term='service',kind='service',name=''),dict(term='bucket',kind='bucket',name='bucket')])
        for action in ('metadata_read','read'):
            for recursive in (False,True):
                obj=grant_class.__new__(grant_class)
                obj.source_type='minio';obj.engine_id=12;obj.operator='34';obj.recipient='33';obj.base='/owned';obj.grants=[];obj.confirmed=set();obj.node_receipts={}
                sent=[]
                def request(method,path,statuses,body):
                    self.assertIn(body['request_id'],obj.grants)
                    sent.append(copy.deepcopy(body))
                    return SimpleNamespace(payload=dict(body,engine_id='12',operator_principal_id='34',approval_mode='independent',revocation=None))
                obj.issuer=SimpleNamespace(request=request)
                receipt=obj.node_grant(target,action=action,recursive=recursive)
                self.assertEqual(len(sent),1);self.assertEqual(receipt['node_recursive'],recursive)
                self.assertNotIn('item_fingerprint',sent[0]);self.assertNotIn('expected_boundary_hash',sent[0])
                self.assertEqual(obj.confirmed,{sent[0]['request_id']})
        for change in (dict(operator_principal_id='other'),dict(node_recursive=1),dict(boundary_hash='a'*64),dict(expires_at='2000-01-01T00:00:00+00:00')):
            obj.grants=[];obj.confirmed=set();obj.node_receipts={}
            def wrong(method,path,statuses,body):
                return SimpleNamespace(payload=dict(dict(body,engine_id='12',operator_principal_id='34',approval_mode='independent',revocation=None),**change))
            obj.issuer=SimpleNamespace(request=wrong)
            with self.assertRaises((ValueError,CASE.GRANTS.SuiteError)):obj.node_grant(target,action='read',recursive=True)
            self.assertEqual(len(obj.grants),1);self.assertEqual(obj.confirmed,set())

    def test_minio_future_physical_fixture_has_fixed_bytes_and_exact_cleanup(self):
        class S3Error(Exception):
            def __init__(self,code):self.code=code
        class Response(io.BytesIO):
            def release_conn(self):pass
        objects={f'envi/{key}':value for spec in FIXTURE.sources()for key,value in FIXTURE.pair(spec).items()}
        class Storage:
            def __init__(self,*args,**kwargs):pass
            def get_object(self,bucket,key):
                if key not in objects:raise S3Error('NoSuchKey')
                return Response(objects[key])
            def put_object(self,bucket,key,stream,size,**kwargs):
                objects[key]=stream.read();self_test.assertEqual(len(objects[key]),size)
            def remove_object(self,bucket,key):objects.pop(key,None)
            def list_objects(self,bucket,*,prefix,recursive):return [SimpleNamespace(object_name=k)for k in objects if k.startswith(prefix)]
        self_test=self
        with tempfile.TemporaryDirectory()as directory:
            root=Path(directory);(root/'envi').mkdir()
            for spec in FIXTURE.sources():
                for name,raw in FIXTURE.pair(spec).items():(root/'envi'/name).write_bytes(raw)
            args=dict(storage=dict(endpoint='fixture',access_key='fake',secret_key='fake',bucket='bucket'),nfs_root=str(root))
            with mock.patch.dict(sys.modules,{'minio':SimpleNamespace(Minio=Storage),'minio.error':SimpleNamespace(S3Error=S3Error)}):
                self.assertEqual(FIXTURE.worker(dict(args,action='node-create')),dict(objects_created=6,sources_unchanged=True))
                self.assertTrue(all(objects[k]==v for k,v in FIXTURE.node_sources().items()))
                with self.assertRaisesRegex(ValueError,'collided'):FIXTURE.worker(dict(args,action='node-create'))
                key=next(iter(FIXTURE.node_sources()));objects[key]=b'foreign'
                with self.assertRaisesRegex(ValueError,'changed'):FIXTURE.worker(dict(args,action='node-delete'))
                objects[key]=FIXTURE.node_sources()[key]
                self.assertEqual(FIXTURE.worker(dict(args,action='node-delete')),dict(objects_removed=6,sources_unchanged=True))
                self.assertTrue(all(k.startswith('envi/') and '/'not in k[5:] for k in objects))

    def root_physical_fixture(self,directory,*,failure=None):
        root=Path(directory);(root/'envi').mkdir()
        baseline={key:raw for spec in FIXTURE.sources() for key,raw in FIXTURE.pair(spec).items()}
        for key,raw in baseline.items():(root/'envi'/key).write_bytes(raw)
        run_id='manager-42-1';names=FIXTURE.root_bucket_names(run_id)
        objects={'sources':{'envi/'+key:raw for key,raw in baseline.items()}}
        events=[];test=self
        class Response(io.BytesIO):
            def release_conn(self):pass
        class Storage:
            def __init__(self,*a,**k):pass
            def bucket_exists(self,name):return name in objects
            def make_bucket(self,name):
                owned=json.loads((root/'.addp-envi-root-buckets.json').read_text())
                test.assertIn(name,owned['buckets']);test.assertEqual(owned['run_id'],run_id)
                objects[name]={};events.append(('make',name))
            def remove_bucket(self,name):test.assertEqual(objects[name],{});del objects[name];events.append(('delete',name))
            def put_object(self,name,key,stream,size,**kwargs):
                objects[name][key]=stream.read();test.assertEqual(len(objects[name][key]),size)
                if failure=='partial_put':raise RuntimeError('partial source write')
            def get_object(self,name,key):return Response(objects[name][key])
            def remove_object(self,name,key):del objects[name][key]
            def list_objects(self,name,*,recursive):return [SimpleNamespace(object_name=k) for k in objects[name]]
        def invoke(action):
            request=dict(action=action,run_id=run_id,nfs_root=str(root),storage=dict(endpoint='fixture',access_key='fake',secret_key='fake',bucket='sources'))
            with mock.patch.dict(sys.modules,{'minio':SimpleNamespace(Minio=Storage)}):return FIXTURE.worker(request)
        return invoke,names,objects,events,baseline

    def test_root_physical_buckets_are_preregistered_exact_and_removed(self):
        with tempfile.TemporaryDirectory() as directory:
            invoke,names,objects,events,baseline=self.root_physical_fixture(directory)
            self.assertEqual(invoke('root-empty-create'),dict(buckets=names,sources_unchanged=True))
            self.assertEqual(objects[names['empty']],{})
            self.assertEqual(invoke('root-future-create'),dict(objects_created=2,sources_unchanged=True))
            self.assertEqual(objects[names['future']],FIXTURE.pair(FIXTURE.sources()[0]))
            self.assertEqual(invoke('root-delete'),dict(root_buckets_removed=True,sources_unchanged=True))
            self.assertEqual(objects,{'sources':{'envi/'+k:v for k,v in baseline.items()}})
            self.assertFalse(Path(directory,'.addp-envi-root-buckets.json').exists())
            self.assertEqual(len([e for e in events if e[0]=='delete']),2)

    def test_root_physical_partial_creation_is_cleaned_without_touching_originals(self):
        with tempfile.TemporaryDirectory() as directory:
            invoke,names,objects,events,baseline=self.root_physical_fixture(directory,failure='partial_put')
            invoke('root-empty-create')
            with self.assertRaisesRegex(RuntimeError,'partial source write'):invoke('root-future-create')
            self.assertEqual(len(objects[names['future']]),1)
            self.assertTrue(invoke('root-delete')['root_buckets_removed'])
            self.assertEqual(set(objects),{'sources'})
            self.assertEqual(objects['sources'],{'envi/'+k:v for k,v in baseline.items()})

    def test_root_physical_refuses_foreign_buckets_objects_and_changed_bytes(self):
        for fault in ('collision','foreign_object','changed_bytes','foreign_owner'):
            with self.subTest(fault=fault),tempfile.TemporaryDirectory() as directory:
                invoke,names,objects,events,baseline=self.root_physical_fixture(directory)
                if fault=='collision':
                    objects[names['empty']]={'foreign':b'preserve'}
                    with self.assertRaises(ValueError):invoke('root-empty-create')
                    self.assertEqual(objects[names['empty']],{'foreign':b'preserve'});continue
                invoke('root-empty-create');invoke('root-future-create')
                if fault=='foreign_object':objects[names['future']]['foreign']=b'preserve'
                if fault=='changed_bytes':objects[names['future']]['bsq.img']=b'changed'
                if fault=='foreign_owner':
                    Path(directory,'.addp-envi-root-buckets.json').write_text(json.dumps(dict(run_id='manager-999-1',buckets=list(names.values()))))
                with self.assertRaises(ValueError):invoke('root-delete')
                self.assertIn(names['future'],objects)
                self.assertEqual(objects['sources'],{'envi/'+k:v for k,v in baseline.items()})

    def workspace_scenario(self, directory, fault=None):
        state={'rows':{},'cleanup':False,'revoked':[]};spec=FIXTURE.sources()[0]
        class Authorization:
            engine_id=12;operator='34';recipient='33';base='/api/v1/system/engines/12'
            def __init__(self):self.grants=[];self.confirmed=set();self.node_receipts={}
            def cleanup(self):
                state['cleanup']=True
                for command in list(self.grants):state['revoked'].append(command);self.grants.remove(command);state['rows'].pop(command,None)
                if fault=='cleanup':raise ValueError('cleanup failed')
        auth=Authorization()
        def request(method,url,statuses):
            rows=list(state['rows'].values())
            if '/access_grants/history?' not in url:rows=[row for row in rows if not row.get('revocation')]
            return SimpleNamespace(payload={'data':rows,'total':len(rows)})
        auth.issuer=SimpleNamespace(request=request);client=SimpleNamespace(token='same-consumer')
        env=dict(ADDP_ONLINE_ARTIFACT_DIR=directory,ADDP_ONLINE_TEST_RUN_ID='manager-42-1',ADDP_ONLINE_TEST_TENANT_ID='2',
            ADDP_ONLINE_MANAGER_MINIO_BUCKET='bucket',ADDP_ONLINE_MANAGER_AUTHORIZER_USERNAME='issuer',ADDP_ONLINE_MANAGER_AUTHORIZER_PASSWORD='private-issuer-password',
            ADDP_ONLINE_TEST_USER_USERNAME='consumer',ADDP_ONLINE_TEST_USER_PASSWORD='private-consumer-password',ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN='must-not-pass')
        def runner(args,*,cwd,env,stdout,stderr,check):
            self.assertNotIn('ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN',env)
            binding=json.loads(env['ADDP_MINIO_WORKSPACE_BINDING']);commands=[]
            for index,action in enumerate(('metadata_read','read')):
                body=dict(request_id=f'00000000-0000-4000-8000-{index+1:012d}',catalog_path=dict(version='catalog.path/v1',engine_id=12,segments=[dict(term='service',kind='service',name='')]),
                    node_recursive=True,recipient_type='user',recipient_id='33',action=action,expiry_mode='until_revoked',expires_at=None,reason='owned')
                commands.append(body);state['rows'][body['request_id']]=dict(body,engine_id='12',operator_principal_id='34',approval_mode='independent',revocation={'revoked_at':'2026-11-15T00:00:00Z'})
            if fault=='unrevoked':state['rows'][commands[1]['request_id']]['revocation']=None
            Path(env['ADDP_MINIO_WORKSPACE_JOURNAL']).write_text(json.dumps(dict(run_id=binding['run_id'],commands=commands)))
            bundle=dict(members=spec['members'],size_bytes=4110,sha256='a'*64)
            proof=dict(binding,schema_version='addp.minio-root-workspace/v1',result='failed' if fault=='browser' else 'passed',phase='read_submit' if fault=='browser' else 'completed',
                checks=list(CASE.MINIO_WORKSPACE_CHECKS),commands=commands,revocations=[body['request_id']for body in commands],login_requests=2,leaf_fact_reads=0,failures=[],
                observations=[dict(visible=v,download_status=status,**({'bundle':bundle}if status==200 else {}))for v,status in [(False,None),(True,403),(False,403),(True,200),(False,403)]])
            if fault=='identity':proof['issuer_id']='999'
            if fault=='missing_check':proof['checks'].pop()
            if fault=='bytes':proof['observations'][3]['bundle']['members']={}
            if fault=='sha':proof['observations'][3]['bundle']['sha256']='Z'*64
            if fault=='leaf_facts':proof['leaf_fact_reads']=1
            if fault!='missing_report':Path(directory,'manager-minio-root-workspace.json').write_text(json.dumps(proof))
            return SimpleNamespace(returncode=int(fault=='browser'))
        report={}
        def run():return CASE.verify_minio_root_workspace(Path('/repo'),env,OWNER,client,auth,spec,runner=runner,evidence=report)
        return run,state,report

    def test_minio_workspace_requires_both_browser_grants_and_formal_revoked_history(self):
        with tempfile.TemporaryDirectory() as directory:
            run,state,report=self.workspace_scenario(directory);self.assertIs(run(),report)
            self.assertEqual(report['result'],'passed');self.assertEqual(set(report['browser']['checks']),set(CASE.MINIO_WORKSPACE_CHECKS))
            self.assertEqual(report['cleanup'],dict(zero_current_source_rules=True));self.assertTrue(state['cleanup'])
            self.assertEqual(state['revoked'],[])
            self.assertNotIn('private-',json.dumps(report));self.assertNotIn('must-not-pass',json.dumps(report))

    def test_minio_workspace_failures_clean_owned_commands_without_passing_old_scenes(self):
        for fault in ('identity','missing_check','bytes','sha','leaf_facts','unrevoked','browser','missing_report','cleanup'):
            with self.subTest(fault=fault),tempfile.TemporaryDirectory() as directory:
                run,state,report=self.workspace_scenario(directory,fault)
                with self.assertRaises(ValueError):run()
                self.assertEqual(report['result'],'failed');self.assertTrue(state['cleanup'])
                self.assertFalse(any(not row.get('revocation') for row in state['rows'].values()))
                if fault=='unrevoked':self.assertIn('00000000-0000-4000-8000-000000000002',state['revoked'])
                if fault=='cleanup':self.assertIn('errors',report['cleanup'])

    def root_scope_scenario(self, fault=None):
        from datetime import datetime,timezone
        spec=FIXTURE.sources()[0];run_id='manager-42-1'
        names=FIXTURE.root_bucket_names(run_id)
        original='bucket/envi/'+spec['name'];future=names['future']+'/'+spec['name']
        state={'now':1000.,'records':{},'next':0,'future':False,'empty':False,'deleted':False};events=[]
        def active():return [r for r in state['records'].values() if r['revocation'] is None and (r['expires_at'] is None or datetime.fromisoformat(r['expires_at']).timestamp()>state['now'])]
        class Authorization:
            operator='34';recipient='33';engine_id=12;base='/api/v1/system/engines/12'
            def node_grant(self,target,*,action,recursive,seconds=300):
                state['next']+=1
                row=dict(request_id=str(state['next']),catalog_path=copy.deepcopy(target),node_recursive=recursive,
                    recipient_type='user',recipient_id=self.recipient,action=action,expiry_mode='until_revoked' if seconds is None else 'at_time',
                    expires_at=None if seconds is None else datetime.fromtimestamp(state['now']+seconds,timezone.utc).isoformat(),
                    operator_principal_id=self.operator,revocation=None)
                state['records'][row['request_id']]=row;events.append(('grant',copy.deepcopy(row)));return copy.deepcopy(row)
            def grant(self,model):
                row=self.node_grant(dict(version='catalog.path/v1',engine_id=12,segments=[dict(term=t,kind=t,name=n) for t,n in [('service',''),('bucket','bucket'),('prefix','envi'),('object',spec['name'])]]),action='read',recursive=False,seconds=None)
                saved=state['records'][row['request_id']];saved['item_fingerprint']=model['fingerprint'];return copy.deepcopy(saved)
            def revoke(self,command):
                events.append(('revoke',command))
                if fault=='root_retained' and command=='1':return
                state['records'][command]['revocation']={'reason':'owned withdrawal'}
            def cleanup(self):
                events.append(('cleanup',))
                for row in active():row['revocation']={'reason':'owned cleanup'}
        auth=Authorization()
        def root_active():return any(len(r['catalog_path']['segments'])==1 for r in active())
        def leaf_active():return any(r.get('item_fingerprint') for r in active())
        def empty_active():return root_active() or any(r['catalog_path']['segments'][-1]['name']==names['empty'] for r in active())
        def issuer_request(method,url,statuses,*args):
            if '/access_grants/history' in url:
                rows=copy.deepcopy(list(state['records'].values()))
                if fault=='forged_expiry':
                    for row in rows:
                        if row.get('expires_at') and datetime.fromisoformat(row['expires_at']).timestamp()<=state['now']:row['revocation']={'reason':'forged'}
                if fault=='future_precise' and state['future']:rows.append(dict(request_id='foreign',item_fingerprint=hashlib.sha256(f'12:{future}'.encode()).hexdigest()))
                return SimpleNamespace(payload={'data':rows,'total':len(rows)+(fault=='truncated_history')})
            if '/access_grants?' in url:return SimpleNamespace(payload={'data':copy.deepcopy(active()),'total':len(active())})
            if '/data-items?' in url:return SimpleNamespace(payload={'data':[dict(full_name=original)],'total':1})
            if '/meta/executions/' in url:
                empty='empty-scan' in url
                return SimpleNamespace(payload={'status':'success','triggered_by':35 if fault=='scan_identity' else 34,'scan_context':{'engine_id':12,'scan_depth':'basic'},'metadata':{'items_scanned':int(not empty or fault=='empty_scan_has_item'),'catalog_nodes_scanned':1}})
            raise AssertionError((method,url))
        auth.issuer=SimpleNamespace(request=issuer_request)
        def user_request(method,url,statuses,*args,**kwargs):
            if url=='/api/v1/manager/engines':return SimpleNamespace(payload={'data':[{'id':12}] if active() else []})
            if '/meta/resource-tree/' in url:
                if not active():return SimpleNamespace(payload={'error':'hidden'})
                leaves=([original]+([future] if state['future'] else [])) if root_active() else ([original] if leaf_active() else [])
                if fault=='future_leak' and state['future'] and leaf_active() and not root_active():leaves.append(future)
                children=[]
                for i,name in enumerate(leaves):children.append(dict(type='object',metadata={'item_id':i+1,'full_name':name},children=[]))
                if empty_active() and fault!='empty_hidden':children.append(dict(type='bucket',children=[],hasChildren=False,metadata={'full_name':names['empty'],'item_count':int(fault=='empty_item_count')}))
                return SimpleNamespace(payload={'metadata':{'engine_id':12},'children':children})
            if '/manager/downloads/file?' in url:
                name=parse_qs(urlparse(url).query)['locator'][0];ok=root_active() or name==original and leaf_active()
                if fault=='independent_read' and not root_active():ok=False
                status=200 if ok else 403
                if status not in statuses:raise ValueError('unexpected root content status')
                data=io.BytesIO()
                if ok:
                    with zipfile.ZipFile(data,'w',compression=zipfile.ZIP_DEFLATED) as z:
                        for key,raw in FIXTURE.pair(spec).items():z.writestr(key,raw)
                if fault=='credential':client.token='changed'
                return SimpleNamespace(status=status,payload={} if ok else {'error':'denied'},raw=b'wrong' if fault=='bytes' and ok else data.getvalue())
            raise AssertionError((method,url))
        client=SimpleNamespace(token='consumer',request=user_request)
        def find(actor,engine,name,label):
            return dict(id=2 if name==future else 1,full_name=name,fingerprint=hashlib.sha256(f'{engine}:{name}'.encode()).hexdigest())
        def physical(action):
            events.append((action,))
            if action=='root-empty-create':
                state['empty']=True
                if fault=='empty_create':raise RuntimeError('partial empty bucket')
                return dict(buckets=names,sources_unchanged=True)
            if action=='root-future-create':
                self.assertTrue(root_active());state['future']=True
                if fault=='future_create':raise RuntimeError('partial future bucket')
                return dict(objects_created=2,sources_unchanged=True)
            self.assertEqual(action,'root-delete');state['deleted']=True
            if fault=='cleanup':raise RuntimeError('owned cleanup failed')
            return dict(root_buckets_removed=True,sources_unchanged=True)
        def scan(actor,engine,deadline,*,depth,scope):
            self.assertIs(actor,auth.issuer);self.assertEqual(depth,'basic')
            self.assertIn(scope,({'catalog_paths':[names['empty']]},{'catalog_paths':[names['future']]}))
            events.append(('scan',scope));return 'empty-scan' if scope['catalog_paths']==[names['empty']] else 'future-scan'
        owner=SimpleNamespace(wait_for_meta_scan=scan,find_fixture_item=find,build_item_locator=lambda engine,item:item['full_name'],validate_model_bundle=OWNER.validate_model_bundle)
        report={}
        def run():return CASE.verify_minio_engine_root(client,auth,'bucket',spec,owner,physical,180,run_id,clock=lambda:state['now'],sleep=lambda n:state.update(now=state['now']+n),evidence=report)
        return run,state,events,report,active

    def test_minio_engine_root_covers_new_bucket_and_preserves_independent_item(self):
        run,state,events,report,active=self.root_scope_scenario();self.assertIs(run(),report)
        self.assertEqual(report['result'],'passed');self.assertEqual(set(report['checks']),CASE.MINIO_ROOT_CHECKS)
        self.assertEqual(len(report['checks']),6);self.assertEqual(active(),[]);self.assertTrue(state['deleted'])
        self.assertTrue(report['same_credential']);self.assertTrue(report['no_future_leaf_grants'])
        self.assertTrue(report['natural_expiry_without_revocation'])
        self.assertEqual(report['baseline_names'],['bucket/envi/bsq.img'])
        self.assertEqual(report['cleanup'],dict(zero_current_source_rules=True,root_buckets_removed=True,sources_unchanged=True))
        self.assertLess(next(i for i,e in enumerate(events) if e[0]=='grant'),next(i for i,e in enumerate(events) if e[0]=='root-future-create'))

    def test_minio_engine_root_failures_preserve_assertions_and_cleanup(self):
        phases=dict(empty_hidden='root_navigation',empty_item_count='root_navigation',root_retained='root_withdrawal',
            future_leak='root_withdrawal',future_precise='future_read',truncated_history='future_read',forged_expiry='natural_expiry',
            independent_read='root_withdrawal',bytes='future_read',credential='natural_expiry',empty_create='empty_bucket_create',
            future_create='future_bucket_create',cleanup='natural_expiry',scan_identity='empty_bucket_scan',empty_scan_has_item='empty_bucket_scan')
        for fault,phase in phases.items():
            with self.subTest(fault=fault):
                run,state,events,report,active=self.root_scope_scenario(fault)
                with self.assertRaises((ValueError,RuntimeError,zipfile.BadZipFile)):run()
                self.assertEqual(active(),[]);self.assertTrue(state['deleted']);self.assertEqual(report['result'],'failed')
                self.assertEqual(report['phase'],phase)
                if fault=='cleanup':self.assertIn('errors',report['cleanup'])

    def node_scope_scenario(self, *, failure=None):
        from datetime import datetime, timezone
        spec=FIXTURE.sources()[0];state={'now':1000.,'active':{},'created':False,'removed':False,'next':0};events=[]
        class Authorization:
            recipient='33';base='/api/v1/system/engines/12'
            def node_grant(self,target,*,action,recursive,seconds=300):
                state['next']+=1;key=str(state['next']);events.append(('grant',action,recursive))
                row=dict(request_id=key,catalog_path=target,action=action,node_recursive=recursive,
                    expires_at=datetime.fromtimestamp(state['now']+seconds,timezone.utc).isoformat())
                state['active'][key]=row;return row
            def revoke(self,key):events.append(('revoke',key));del state['active'][key]
            def cleanup(self):events.append(('cleanup',));state['active'].clear()
        auth=Authorization()
        def allowed(name,action):
            for row in state['active'].values():
                if state['now']>=datetime.fromisoformat(row['expires_at']).timestamp():continue
                if action=='read' and row['action']!='read':continue
                parts=[p['name']for p in row['catalog_path']['segments'][1:]];target=name.split('/')
                if target[:len(parts)]==parts and (row['node_recursive'] or len(target)==len(parts)+1):return True
            return False
        def request(method,path,statuses):
            parsed=urlparse(path)
            if parsed.path.endswith('/access_grants'):
                return SimpleNamespace(payload={'total':len(state['active']),'data':list(state['active'].values())})
            if '/meta/items/' in path:
                code=404 if not state['active'] else 200;payload={};raw=b''
            else:
                name=parse_qs(parsed.query)['locator'][0];permitted=allowed(name,'read')
                code=200 if permitted else 403
                payload={} if permitted else {'error':'denied','error_code':'source_access_denied'};raw=b''
                if permitted:
                    if failure=='read':raise RuntimeError('source read failed')
                    if name.endswith('node-future-'+spec['name']):label='node-future-'
                    elif '/future-' in name:label='future-'
                    else:label=''
                    data=io.BytesIO()
                    with zipfile.ZipFile(data,'w',compression=zipfile.ZIP_DEFLATED)as archive:
                        for key,value in FIXTURE.pair(spec).items():archive.writestr(label+key,value)
                    raw=data.getvalue()
                    if failure=='bytes':raw=b'changed'
                elif failure=='denial':payload['source_data']='leaked'
            if code not in statuses:raise ValueError('unexpected status '+str(code))
            return SimpleNamespace(payload=payload,raw=raw)
        client=SimpleNamespace(request=request);auth.issuer=client
        def find(client,engine,name,label):
            if not allowed(name,'metadata_read'):raise RuntimeError('metadata hidden')
            return dict(id=name,full_name=name)
        def physical(action):
            events.append((action,))
            if action=='node-create':
                self.assertTrue(any(row['action']=='read' and not row['node_recursive']for row in state['active'].values()))
                state['created']=True
                if failure=='create':raise RuntimeError('partial source creation')
                return dict(objects_created=6,sources_unchanged=True)
            self.assertEqual(action,'node-delete');state['removed']=True
            if failure=='cleanup':raise RuntimeError('source cleanup failed')
            return dict(objects_removed=6,sources_unchanged=True)
        owner=SimpleNamespace(find_fixture_item=find,build_item_locator=lambda engine,item:item['full_name'],
            validate_model_bundle=OWNER.validate_model_bundle,wait_for_meta_scan=lambda *a,**k:events.append(('scan',k['scope'])))
        report=dict(checks={},commands=[],cleanup={})
        def run():return CASE.verify_minio_node_scope(client,auth,12,'bucket',spec,owner,physical,180,
            clock=lambda:state['now'],sleep=lambda n:state.update(now=state['now']+n),evidence=report)
        return run,state,events,report

    def test_minio_nodes_future_depth_expiry_and_independent_metadata(self):
        run,state,events,report=self.node_scope_scenario();self.assertIs(run(),report)
        self.assertEqual(set(report['checks']),{'metadata_does_not_read','nonrecursive_future_and_sibling_isolation',
            'withdrawal_retains_independent_metadata','recursive_prefix_isolation','bucket_recursion','natural_expiry'})
        self.assertTrue(all(report['checks'].values()));self.assertEqual(len(report['commands']),6)
        self.assertTrue(all(row['item_id']==row['full_name'] for row in report['downloads']))
        self.assertTrue(all(row['bundle']['members'] and row['bundle']['sha256'] for row in report['downloads'] if row['status']==200))
        self.assertTrue(all(row['error_code']=='source_access_denied' for row in report['downloads'] if row['status']==403))
        self.assertEqual(state['active'],{});self.assertTrue(state['removed'])
        self.assertEqual(report['cleanup'],dict(zero_current_source_rules=True,future_objects_removed=True))
        self.assertEqual(events[0],('grant','metadata_read',True))

    def test_minio_node_failure_always_cleans_owned_grants_and_future_objects(self):
        for failure in ('read','bytes','denial','create','cleanup'):
            with self.subTest(failure=failure):
                run,state,events,report=self.node_scope_scenario(failure=failure)
                with self.assertRaises((ValueError,RuntimeError,zipfile.BadZipFile)):run()
                self.assertEqual(state['active'],{});self.assertIn(('cleanup',),events)
                if failure in ('create','cleanup'):self.assertTrue(state['removed'])
                if failure=='cleanup':self.assertIn('errors',report['cleanup'])

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
            wait_for_meta_scan=lambda *a,**k:'initial-scan',find_fixture_item=find)
        with tempfile.TemporaryDirectory() as directory:
            env=dict(ADDP_ONLINE_MANAGER_CASE='native-envi',ADDP_ONLINE_ARTIFACT_DIR=directory,
                ADDP_ONLINE_TEST_TENANT_ID='2',ADDP_ONLINE_TEST_RUN_ID='test-envi',GATEWAY_URL='http://fixture',
                ADDP_ONLINE_TEST_USER_ACCESS_TOKEN='consumer',ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN='issuer',
                ADDP_ONLINE_MANAGER_MINIO_BUCKET='bucket',ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID='12',ADDP_ONLINE_MANAGER_NFS_ENGINE_ID='14')
            with mock.patch.object(CASE.GRANTS,'ModelSourceAuthorization',Authorization),mock.patch.object(CASE,'verify_minio_root_workspace',side_effect=self.complete_workspace_stage),mock.patch.object(CASE,'verify_minio_engine_root',side_effect=self.complete_root_stage),mock.patch.object(CASE,'verify_minio_node_scope',return_value={}):
                with self.assertRaisesRegex(RuntimeError,'owned catalog failure'):
                    CASE.run(Path('/repo'),env,owner,physical_runner=physical)
            report=json.loads(Path(directory,'manager-envi.json').read_text())
            self.assertEqual((report['stage'],report['error']),('metadata','RuntimeError'))
        self.assertEqual([e[0]for e in events],['seed','approve','withdraw','delete'])
        model=events[1][1]
        self.assertEqual(model['fingerprint'],hashlib.sha256(b'12:bucket/envi/bsq.img').hexdigest())
        self.assertEqual([r['role']for r in model['composition']],['raster','header'])

    def test_root_stage_failure_or_incomplete_proof_stops_old_scenes_and_cleans_seed(self):
        for gate,mode in ((gate,mode) for gate in ('minio_root_workspace','minio_engine_root') for mode in ('failed','incomplete')):
            with self.subTest(gate=gate,mode=mode),tempfile.TemporaryDirectory() as directory:
                events=[]
                class Authorization:
                    operator='34';recipient='33'
                    def __init__(self,*a,**k):pass
                    def cleanup(self):events.append('withdraw')
                    def grant(self,*a):raise AssertionError('original exact source phase ran after root failure')
                client=mock.Mock();client.request.return_value=SimpleNamespace(payload=[])
                owner=SimpleNamespace(GatewayClient=lambda *a:client,positive_int=lambda v,*a:int(v),
                    REQUIRED_PERMISSIONS=OWNER.REQUIRED_PERMISSIONS,validate_user_identity=lambda *a:{'principal_id':'33'},
                    wait_for_meta_scan=lambda *a,**k:'initial-scan')
                def physical(action,*args):
                    events.append(action)
                    return {'sources':FIXTURE.sources()} if action=='seed' else dict(objects_removed=True,files_removed=True)
                def root_stage(*args,**kwargs):
                    kwargs['evidence'].update(result=mode,phase='root_navigation',checks=[])
                    if mode=='failed':raise ValueError('root failure')
                env=dict(ADDP_ONLINE_MANAGER_CASE='native-envi',ADDP_ONLINE_ARTIFACT_DIR=directory,
                    ADDP_ONLINE_TEST_TENANT_ID='2',ADDP_ONLINE_TEST_RUN_ID='manager-42-1',GATEWAY_URL='http://fixture',
                    ADDP_ONLINE_TEST_USER_ACCESS_TOKEN='consumer',ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN='issuer',
                    ADDP_ONLINE_MANAGER_MINIO_BUCKET='bucket',ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID='12',ADDP_ONLINE_MANAGER_NFS_ENGINE_ID='14')
                with mock.patch.object(CASE.GRANTS,'ModelSourceAuthorization',Authorization),mock.patch.object(CASE,'verify_minio_root_workspace',side_effect=root_stage if gate=='minio_root_workspace' else self.complete_workspace_stage),mock.patch.object(CASE,'verify_minio_engine_root',side_effect=root_stage if gate=='minio_engine_root' else self.complete_root_stage),mock.patch.object(CASE,'verify_minio_node_scope') as old_nodes:
                    with self.assertRaises(ValueError):CASE.run(Path('/repo'),env,owner,physical_runner=physical)
                    old_nodes.assert_not_called()
                report=json.loads(Path(directory,'manager-envi.json').read_text())
                self.assertEqual(report['stage'],'minio-root-workspace' if gate=='minio_root_workspace' else 'minio-engine-root');self.assertEqual(report['result'],'failed')
                self.assertEqual(report[gate]['phase'],'root_navigation')
                self.assertEqual(events,['seed','withdraw','delete'])

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
        fn=text[text.index('function assertStructuralMetadata'):text.index('function rasterContentMetadata')].replace('export function','function',1)
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
                self_test.assertEqual(options['issuer_permissions'],CASE.ENVI_INITIALIZER_PERMISSIONS)
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
        client=SimpleNamespace(request=request);issuer=SimpleNamespace(request=mock.Mock(side_effect=AssertionError('initializer must not consume content')))
        scans=[]
        def scan(actor,engine,deadline,scope):
            self_test.assertIs(actor,issuer if engine==14 else client,'NFS scans must use the approved initializer, never the consumer')
            self_test.assertEqual(scope,{'catalog_paths':['envi'if engine==14 else 'bucket/envi']})
            execution='scan-'+str(len(scans)+1);scans.append((engine,execution));return execution
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
            self_test.assertNotIn('ADDP_ONLINE_MANAGER_AUTHORIZER_USERNAME',env);self_test.assertNotIn('ADDP_ONLINE_MANAGER_AUTHORIZER_PASSWORD',env)
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
        owner=SimpleNamespace(GatewayClient=lambda url,token,*a:issuer if token=='issuer' else client,positive_int=lambda v,*a:int(v),validate_user_identity=lambda *a:identity,REQUIRED_PERMISSIONS=OWNER.REQUIRED_PERMISSIONS,SuiteError=OWNER.SuiteError,
            wait_for_meta_scan=scan,find_fixture_item=find,ArtifactFixture=OWNER.ArtifactFixture,build_item_locator=OWNER.build_item_locator,
            validate_model_bundle=OWNER.validate_model_bundle,generate_raster_cog=generate,cleanup_managed_artifact=cleanup,RASTER_TASK_TYPE=OWNER.RASTER_TASK_TYPE)
        env=dict(ADDP_ONLINE_MANAGER_CASE='native-envi',ADDP_ONLINE_ARTIFACT_DIR=directory,ADDP_ONLINE_TEST_TENANT_ID='2',ADDP_ONLINE_TEST_RUN_ID='envi-test',GATEWAY_URL='http://fixture',ADDP_ONLINE_TEST_USER_ACCESS_TOKEN='consumer',ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN='issuer',ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN='engine',ADDP_ONLINE_MANAGER_AUTHORIZER_USERNAME='issuer-name',ADDP_ONLINE_MANAGER_AUTHORIZER_PASSWORD='private-authorizer',ADDP_ONLINE_MANAGER_MINIO_BUCKET='bucket',ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID='12',ADDP_ONLINE_MANAGER_NFS_ENGINE_ID='14')
        def refresh(actor,scanner,tenant,principal,engine,environment,evidence,*,initializer):
            self.assertIs(actor,issuer)
            self.assertIs(scanner,client)
            self.assertEqual((tenant,principal,engine,initializer),(2,'33',14,'34'))
            self.assertIs(environment,env)
            evidence.update(result='passed',cleanup='zero_current_source_rules_and_active_executions')
            events.append(('scoped-refresh',))
        with mock.patch.object(CASE.GRANTS,'ModelSourceAuthorization',Authorization),mock.patch.object(CASE,'verify_minio_root_workspace',side_effect=self.complete_workspace_stage),mock.patch.object(CASE.MEDIA,'run_browser',browser),mock.patch.object(CASE,'verify_minio_engine_root',side_effect=self.complete_root_stage),mock.patch.object(CASE,'verify_minio_node_scope',return_value={}),mock.patch.object(CASE.REFRESH,'run_nfs_refresh',side_effect=refresh):
            if browser_failure:
                with self.assertRaisesRegex(RuntimeError,'owned browser failure'):CASE.run(Path('/repo'),env,owner,physical_runner=physical)
                report=json.loads(Path(directory,'manager-envi.json').read_text())
            else:report=CASE.run(Path('/repo'),env,owner,physical_runner=physical)
        self.assertEqual(active,{});self.assertEqual(len(receipts),20)
        self.assertEqual(sum(e[0]=='scoped-refresh' for e in events),1)
        self.assertEqual(report['nfs_scoped_refresh']['cleanup'],'zero_current_source_rules_and_active_executions')
        self.assertEqual(len(CASE.ENVI_INITIALIZER_PERMISSIONS),13)
        self.assertEqual(report['source_authorization']['initializer_permissions_verified'],sorted(CASE.ENVI_INITIALIZER_PERMISSIONS))
        self.assertEqual(len(scans),10);self.assertEqual(len(report['source_scans']),10)
        for storage,engine,principal,path in [('minio',12,'33','bucket/envi'),('nfs',14,'34','envi')]:
            rows=[row for row in report['source_scans']if row['storage']==storage]
            self.assertEqual([r['phase']for r in rows],['initial','composition-change','composition-restore','header-version-change','header-version-restore'])
            self.assertTrue(all(r['principal_id']==principal and r['engine_id']==engine and r['catalog_paths']==[path]and (engine,r['execution_id'])in scans for r in rows))
        self.assertEqual((report['source_authorization']['operator_principal_id'],report['source_authorization']['recipient_principal_id']),('34','33'))
        self.assertEqual(report['source_authorization']['cleanup'],dict(read_grants_revoked=True,metadata_details_hidden=6))
        self.assertEqual(sum(e[0]=='generate'for e in events),4);self.assertEqual(sum(e[0]=='task-delete'for e in events),4)
        self.assertEqual(report['source_cleanup'],dict(objects_removed=True,files_removed=True));return report

    def test_complete_six_source_owner_flow_and_failure_cleanup(self):
        for failure in (False,True):
            with self.subTest(failure=failure),tempfile.TemporaryDirectory()as directory:
                report=self.full_scenario(directory,browser_failure=failure)
                self.assertEqual(report['schema_version'],'addp.manager-envi/v6')
                self.assertEqual(set(report['minio_engine_root']['checks']),CASE.MINIO_ROOT_CHECKS)
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
        self.assertIn('issuer_permissions=ENVI_INITIALIZER_PERMISSIONS',runner)
        self.assertNotIn('grant.deny(',runner)
        self.assertNotIn('grant.release(',runner)
        self.assertIn('addp.manager-envi/v6',runner)

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

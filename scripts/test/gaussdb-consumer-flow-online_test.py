from importlib import import_module
import copy
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import Mock, patch
from types import SimpleNamespace

FLOW=import_module('scripts.test.readonly-sql-consumer-flow-online')
PROFILE=FLOW.PROFILES['gaussdb']
FIXTURE=import_module('business.scripts.online-gaussdb-consumer-fixture')


class GaussDBConsumerTest(unittest.TestCase):
    def sample(self):
        return {'preview_type':'table','data':{'total':2,'columns':['id','name','amount','optional_text'],'rows':[
            {'id':'9007199254740993','name':'高斯正式接入中文','amount':'12345678901234567890.123456789012345678','optional_text':None},
            {'id':'2','name':'保留尾空格  ','amount':'-.000000000000000001','optional_text':None}]}}

    def test_exact_native_precision_and_nulls(self):
        self.assertEqual(len(FLOW.validate_preview(self.sample(),PROFILE)),2)

    def test_rounded_integer_numeric_decimal_and_trimmed_text_rejected(self):
        for field,value,row in [('id',9007199254740992,0),('id','9007199254740992',0),('amount',123.5,0),('amount','12345678901234567890.1',0),('name','保留尾空格',1),('optional_text','',1)]:
            data=self.sample()
            data['data']['rows'][row][field]=value
            with self.subTest(field=field,value=value), self.assertRaises(FLOW.API.SuiteError):
                FLOW.validate_preview(data,PROFILE)

    def test_scanned_item_requires_precise_schema_and_primary_key(self):
        item={'item_type':'table','full_name':'addp_acceptance.source','fingerprint':'demo',
              'attributes':{'schema_version':1,'item':{'data_type':'table'},'type_info':{'table':{
                  'primary_key':['name','id'],'row_count':2,'fields':[
                      {'name':'id','type':'bigint','primary_key':True},
                      {'name':'name','type':'string','primary_key':True},
                      {'name':'amount','type':'decimal','precision':38,'scale':18},
                      {'name':'optional_text','type':'string'}]}}}}
        FLOW.validate_item(item,PROFILE)
        for change in ('precision','primary_key','type','uncertified','wrong_partition'):
            wrong=copy.deepcopy(item)
            if change=='precision':wrong['attributes']['type_info']['table']['fields'][2]['precision']=18
            elif change=='type':wrong['attributes']['type_info']['table']['fields'][0]['type']='double'
            elif change=='primary_key':wrong['attributes']['type_info']['table']['primary_key']=[]
            elif change=='wrong_partition':wrong['attributes']['table']=wrong['attributes'].pop('type_info')['table']
            else:wrong['attributes']['capabilities']={'indexes':[],'indexing':{}}
            with self.subTest(change=change),self.assertRaises(FLOW.API.SuiteError):FLOW.validate_item(wrong,PROFILE)

    def test_workbench_evidence_requires_two_distinct_successful_exact_reads(self):
        report={'run_id':'run','tenant_id':2,'principal_id':'33','engine_id':17}
        rows=self.sample()['data']['rows']
        fields=[{'name':'id','type':'bigint'},{'name':'name','type':'string'},{'name':'amount','type':'decimal','precision':38,'scale':18},{'name':'optional_text','type':'string'}]
        evidence={**report,'queries':[
            {'execution_id':'first','status':'success','effect':'read','rows':rows,'fields':fields},
            {'execution_id':'second','status':'success','effect':'read','rows':[{**row,'bound_text':"中文' OR 1=1 --"} for row in rows if int(row['id'])>=3],'fields':fields+[{'name':'bound_text','type':'string'}]}]}
        FLOW.validate_browser_report(evidence,report,PROFILE)
        for change in ('identity','missing','duplicate','failed','write','precision','schema','schema_order','parameter','integer_filter'):
            bad=copy.deepcopy(evidence)
            if change=='identity':bad['principal_id']='other'
            elif change=='missing':bad['queries'].pop()
            elif change=='duplicate':bad['queries'][1]['execution_id']='first'
            elif change=='failed':bad['queries'][0]['status']='failed'
            elif change=='write':bad['queries'][0]['effect']='write'
            elif change=='precision':bad['queries'][1]['rows'][0]['amount']='123.5'
            elif change=='schema':bad['queries'][1]['fields'][2]['scale']=0
            elif change=='schema_order':bad['queries'][1]['fields'].reverse()
            elif change=='integer_filter':bad['queries'][1]['rows'].append(rows[1])
            else:bad['queries'][1]['rows'][0]['bound_text']='rewritten'
            with self.subTest(change=change),self.assertRaises(FLOW.API.SuiteError):
                FLOW.validate_browser_report(bad,report,PROFILE)

    def test_fixture_rejects_public_or_repository_state_before_docker(self):
        with tempfile.TemporaryDirectory() as temp:
            path=Path(temp).resolve()
            for directory in (str(FIXTURE.ROOT),str(path)):
                path.chmod(0o755)
                with patch.dict(os.environ,{'ADDP_GAUSSDB_FIXTURE_DIR':directory}),patch.object(FIXTURE,'run') as run:
                    with self.assertRaises(RuntimeError):FIXTURE.directory()
                    run.assert_not_called()

    def test_foreign_cleanup_never_mutates_docker(self):
        with tempfile.TemporaryDirectory() as temp:
            path=Path(temp).resolve();path.chmod(0o700)
            (path/'gaussdb-state.json').write_text(json.dumps({'name':'owned','run_id':'expected'}))
            with patch.dict(os.environ,{'ADDP_GAUSSDB_FIXTURE_DIR':str(path)}),patch.object(FIXTURE,'inspect',return_value={'Config':{'Labels':{FIXTURE.LABEL:'foreign'}}}),patch.object(FIXTURE,'run') as run:
                with self.assertRaisesRegex(RuntimeError,'foreign'):FIXTURE.stop()
                run.assert_not_called()
                self.assertTrue((path/'gaussdb-state.json').exists())

    def test_cleanup_removes_owned_resources_and_checks_each_absence(self):
        with tempfile.TemporaryDirectory() as temp:
            path=Path(temp).resolve();path.chmod(0o700)
            (path/'gaussdb-state.json').write_text(json.dumps({'name':'owned','run_id':'expected'}))
            with patch.dict(os.environ,{'ADDP_GAUSSDB_FIXTURE_DIR':str(path)}),patch.object(FIXTURE,'owned',return_value={}),patch.object(FIXTURE,'inspect',return_value=None) as inspect,patch.object(FIXTURE,'run') as run:
                FIXTURE.stop()
                self.assertEqual(run.call_count,4)
                self.assertEqual(inspect.call_count,4)
                self.assertEqual(json.loads((path/'gaussdb-cleanup.json').read_text()),{'result':'passed','remaining_resources':0})
                self.assertFalse((path/'gaussdb-state.json').exists())

    def test_secret_state_is_exclusive_and_private(self):
        with tempfile.TemporaryDirectory() as temp:
            path=Path(temp)/'state'
            FIXTURE.private(path,{'secret':'private'})
            self.assertEqual(path.stat().st_mode&0o777,0o600)
            with self.assertRaises(FileExistsError):FIXTURE.private(path,{})

    def test_command_failure_does_not_expose_password_or_sql(self):
        with patch.object(FIXTURE.subprocess,'run',return_value=subprocess.CompletedProcess([],1,'secret SQL','private password')):
            with self.assertRaises(RuntimeError) as error:FIXTURE.run('docker','exec','owned',input='secret')
            self.assertNotIn('secret',str(error.exception));self.assertNotIn('private',str(error.exception))

    def test_common_driver_pin_matches_certification_facts(self):
        facts=FIXTURE.IMAGE.media_facts('amd64')
        result=subprocess.run(['go','mod','edit','-json'],cwd=FIXTURE.ROOT/'common',capture_output=True,text=True,check=True,env={**os.environ,'GOWORK':'off'})
        requirement=[row for row in json.loads(result.stdout)['Require'] if row['Path']==facts['GAUSSDB_GO_MODULE']]
        self.assertEqual(requirement,[{'Path':facts['GAUSSDB_GO_MODULE'],'Version':facts['GAUSSDB_GO_VERSION']}])
        sums=(FIXTURE.ROOT/'common/go.sum').read_text().splitlines()
        for suffix,key in [('', 'GAUSSDB_GO_SUM'),('/go.mod','GAUSSDB_GO_MOD_SUM')]:
            self.assertIn(f"{facts['GAUSSDB_GO_MODULE']} {facts['GAUSSDB_GO_VERSION']}{suffix} {facts[key]}",sums)

    def test_hosted_scope_contains_no_transfer_or_service_registration_privileges(self):
        self.assertFalse(any(p.startswith(('transfer.','service.','system.engine.')) for p in FLOW.PERMISSIONS))
        self.assertEqual({p for p in FLOW.PERMISSIONS if p.startswith('develop.')}, {'develop.task.read','develop.task.execute','develop.data_read.execute'})
        self.assertIn('system.execution_authorization.create',FLOW.PERMISSIONS)
        self.assertEqual(len(FLOW.PERMISSIONS),8)
        hosted=(FIXTURE.ROOT/'scripts/test/online-hosted-gaussdb-gate.sh').read_text()
        self.assertIn('source "$ROOT_DIR/scripts/utils/hosted-online.sh"',hosted)
        self.assertIn('online-gaussdb-consumer-fixture.py stop',hosted)
        self.assertIn('scripts/test/online-engine-registration.py',hosted)
        self.assertNotIn('restart.sh',hosted)

    def test_public_structure_excludes_statistics_without_weakening_protected_count(self):
        for profile in FLOW.PROFILES.values():
            fields={
                'id':dict(name='id',type='bigint',primary_key=True),
                'name':dict(name='name',type='string',primary_key=True),
                'amount':dict(name='amount',type='decimal',precision=38,scale=18),
                'optional_text':dict(name='optional_text',type='string')}
            item=dict(item_type='table',full_name=profile.namespace+'.source',fingerprint='test',attributes={
                'schema_version':1,'item':{'data_type':'table'},'type_info':{'table':{
                    'primary_key':['name','id'],'fields':[fields[name] for name in profile.source_columns]}}})
            FLOW.validate_structure(item,profile)
            with self.assertRaises(FLOW.API.SuiteError): FLOW.validate_item(item,profile)
            item['attributes']['type_info']['table']['row_count']=2
            FLOW.validate_item(item,profile)
            with self.assertRaises(FLOW.API.SuiteError): FLOW.validate_structure(item,profile)

    def test_exact_preparation_scopes_and_private_intents_for_both_profiles(self):
        for profile in FLOW.PROFILES.values():
            for fail_after_intent in (False,True):
                with tempfile.TemporaryDirectory() as temp:
                    root=Path(temp)
                    consumer,preparer=Mock(),Mock()
                    metadata=Mock()
                    metadata.target.side_effect=FLOW.REGISTRATION.ExactRecordGrants.target
                    def reads(authorizer,recipient,tenant,targets,**options):
                        self.assertIs(authorizer,preparer);self.assertIs(recipient,consumer)
                        self.assertEqual(targets,[(17,profile.namespace_term,profile.namespace,'source')])
                        self.assertTrue(options['scan_preparation'])
                        target={'engine_id':17,'segments':[{'name':'source'}]}
                        options['on_pending'](17,'read-source',target)
                        pending=json.loads((root/'readonly-sql-grants.json').read_text())
                        self.assertEqual(pending,[dict(engine=17,request_id='read-source',target=target,action='read')])
                        if fail_after_intent: raise RuntimeError('unknown write outcome')
                    def create_metadata(*args,**options):
                        self.assertEqual(options['action'],'metadata_read')
                        self.assertTrue(options['scan_preparation'])
                        self.assertIs(args[0],preparer);self.assertIs(args[1],consumer)
                        pending=[]
                        def grant(engine,namespace,name,term):
                            pending.append(dict(engine=engine,request_id=name,target={'name':name},action='metadata_read'))
                            options['on_change'](pending)
                        metadata.grant.side_effect=grant
                        return metadata
                    with patch.object(FLOW,'private_directory',return_value=root), patch.object(FLOW.SUPPORT,'required_environment',return_value='token'), \
                         patch.object(FLOW.SUPPORT,'GatewayClient',side_effect=[consumer,preparer]), patch.object(FLOW.API,'validate_user_identity'), \
                         patch.object(FLOW.REGISTRATION,'source_grant_participants'), patch.object(FLOW.SUPPORT,'wait_for_scan',return_value='scan-id') as scan, \
                         patch.object(FLOW.REGISTRATION,'initialize_exact_record_read_grants',side_effect=reads), \
                         patch.object(FLOW.REGISTRATION,'ExactRecordGrants',side_effect=create_metadata):
                        if fail_after_intent:
                            with self.assertRaisesRegex(RuntimeError,'unknown write'): FLOW.prepare(profile,2,17)
                        else: FLOW.prepare(profile,2,17)
                    self.assertIs(scan.call_args.args[0],preparer)
                    self.assertEqual(scan.call_args.kwargs,dict(scan_depth='deep',targets=[f'addp://engine/17/path/{profile.namespace}/{n}?type=table' for n in profile.catalog_tables]))
                    pending=json.loads((root/'readonly-sql-grants.json').read_text())
                    self.assertEqual(len(pending),1 if fail_after_intent else len(profile.catalog_tables))
                    self.assertEqual((root/'readonly-sql-grants.json').stat().st_mode&0o777,0o600)
                    self.assertEqual(metadata.grant.call_count,0 if fail_after_intent else len(profile.catalog_tables)-1)
                    self.assertTrue(all(c.args[2]==(403,) for c in consumer.request.call_args_list))
        self.assertEqual(len(FLOW.PERMISSIONS),8)
        self.assertFalse(any(p.startswith('meta.scan_task.') for p in FLOW.PERMISSIONS))

    def test_cleanup_continues_revocations_and_checks_hidden_and_content_denied(self):
        profile=FLOW.PROFILES['starrocks']
        for failure in (False,True):
            with tempfile.TemporaryDirectory() as temp:
                root=Path(temp)
                pending=[dict(engine=17,request_id=name,target={'name':name},action='read' if name=='source' else 'metadata_read') for name in profile.catalog_tables]
                (root/'readonly-sql-grants.json').write_text(json.dumps(pending))
                preparer,consumer=Mock(),Mock()
                preparer.request.side_effect=[RuntimeError('fail') if failure else None]+[None]*3
                consumer.request.side_effect=lambda method,path,status,body: SimpleNamespace(payload={'observed_at':'now','targets':[dict(target=body['targets'][0],visible=False)]})
                with patch.object(FLOW,'private_directory',return_value=root), patch.object(FLOW.SUPPORT,'required_environment',side_effect=lambda key: temp if key=='ADDP_ONLINE_ARTIFACT_DIR' else 'token'), \
                     patch.object(FLOW.SUPPORT,'GatewayClient',side_effect=[preparer,consumer]):
                    if failure:
                        with self.assertRaisesRegex(RuntimeError,'cleanup incomplete'): FLOW.cleanup(profile)
                    else: FLOW.cleanup(profile)
                self.assertEqual(preparer.request.call_count,4)
                receipt=json.loads((root/'starrocks-grant-cleanup.json').read_text())
                self.assertEqual(receipt['read_denied'],not failure)
                self.assertEqual(receipt['metadata_hidden'],not failure)
                self.assertEqual(len(receipt['grants']),3 if failure else 4)
                self.assertEqual(sum(c.args[2]==(403,) for c in consumer.request.call_args_list),3 if failure else 4)

    def test_consumer_rejects_preparation_credential_before_any_scan_or_preview(self):
        client=Mock()
        client.request.side_effect=[SimpleNamespace(payload={'authorization':{'role_assignments':[{'permissions':list(FLOW.PERMISSIONS)}]}}),
            SimpleNamespace(payload=[{'id':17,'engine_type':'gaussdb','capabilities':{'storage':{'store':{'batch_read':True}}}}])]
        with patch.object(FLOW.API,'validate_user_identity'), patch.dict(FLOW.os.environ,ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN='private'), \
             patch.object(FLOW.SUPPORT,'wait_for_scan') as scan, self.assertRaisesRegex(RuntimeError,'credentials leaked'):
            FLOW.run(client,2,17,10,PROFILE)
        scan.assert_not_called()
        self.assertTrue(all(call.args[0]=='GET' for call in client.request.call_args_list))

    def test_consumer_uses_public_structure_protected_facts_and_metadata_only_denial(self):
        for profile in FLOW.PROFILES.values():
            fields={'id':dict(name='id',type='bigint',primary_key=True),'name':dict(name='name',type='string',primary_key=True),
                    'amount':dict(name='amount',type='decimal',precision=38,scale=18),'optional_text':dict(name='optional_text',type='string')}
            source=dict(id=3,node_id=2,item_type='table',full_name=profile.namespace+'.source',fingerprint='test',attributes={
                'schema_version':1,'item':{'data_type':'table'},'type_info':{'table':{'primary_key':['name','id'],'fields':[fields[n] for n in profile.source_columns]}}})
            records=[source]+[dict(source,id=i+4,full_name=profile.namespace+'.'+name) for i,name in enumerate(profile.catalog_tables[1:])]
            protected=copy.deepcopy(source['attributes']);protected['type_info']['table']['row_count']=2
            preview=self.sample()
            preview['data']['columns']=list(profile.source_columns)
            preview['data']['rows'][0]['name']=profile.sample_name
            preview['data']['rows'][1]['optional_text']=profile.empty_value
            preview['data']['item_meta']={'attributes':[dict(key=k,value=v) for k,v in protected.items()]}
            client=Mock()
            def request(method,path,status,body=None):
                if path.endswith('auth/context'): payload={'principal':{'id':'33'},'authorization':{'role_assignments':[{'permissions':list(FLOW.PERMISSIONS)}]}}
                elif path.endswith('engine-catalog/engines'): payload=[dict(id=17,name='fixture',engine_type=profile.engine_type,capabilities={'storage':{'store':{'batch_read':True}}})]
                elif '/items?' in path: payload=records
                elif status==(403,): payload=None
                else: payload=preview
                return SimpleNamespace(payload=payload)
            client.request.side_effect=request
            with tempfile.TemporaryDirectory() as temp:
                root=Path(temp);(root/'readonly-sql-scan.json').write_text('"scan"')
                with patch.object(FLOW,'private_directory',return_value=root),patch.object(FLOW.API,'validate_user_identity',return_value={}), \
                     patch.object(FLOW.SUPPORT,'required_environment',return_value='run'),patch.object(FLOW,'run_browser',return_value={}), \
                     patch.dict(FLOW.os.environ,ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN='',ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN=''), \
                     patch.object(FLOW.SUPPORT,'wait_for_scan') as scan:
                    report=FLOW.run(client,2,17,20,profile)
                scan.assert_not_called()
                self.assertEqual(len(report['metadata_only']),len(profile.catalog_tables)-1)
                self.assertEqual(sum(c.args[0]=='GET' and c.args[2]==(403,) for c in client.request.call_args_list),len(profile.catalog_tables)-1)
                self.assertEqual(report['rows'],preview['data']['rows'])

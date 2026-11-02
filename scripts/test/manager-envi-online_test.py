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
                status=206 if self.active else 403
                if method!='GET' or path!='/cached/result' or status not in statuses:raise ValueError('current source decision did not enforce withdrawal')
                if status==206 and headers!={'Range':'bytes=0-63'}:raise ValueError('unbounded cached request')
                return SimpleNamespace(raw=(b'x'*64 if self.renewed and self.changed else b'c'*64) if self.active else b'')
        ledger=SourceLedger()
        receipt,proof=CASE.verify_source_revocation(ledger,ledger,old,model,'/cached/result')
        self.assertEqual(receipt,new)
        self.assertEqual(proof,dict(revoked_request_id=old['request_id'],renewed_request_id=new['request_id'],boundary_hash='a'*64,range_bytes=64,range_sha256=hashlib.sha256(b'c'*64).hexdigest()))
        for ledger in (SourceLedger(ineffective=True),SourceLedger(changed=True)):
            with self.assertRaises(ValueError):CASE.verify_source_revocation(ledger,ledger,old,model,'/cached/result')
        root=Path(__file__).resolve().parents[2]
        runner=(root/'scripts/test/manager-envi-online.py').read_text()
        self.assertIn('issuer_permissions=GRANTS.REGISTRATION.SOURCE_INITIALIZER_PERMISSIONS',runner)
        self.assertNotIn('grant.deny(',runner)
        self.assertNotIn('grant.release(',runner)
        self.assertIn('addp.manager-envi/v2',runner)

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

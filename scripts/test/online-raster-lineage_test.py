"""Approved observer boundaries, exact two-layer facts and immutable rejection evidence."""
import copy,importlib,json,os,tempfile,unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch
m=importlib.import_module('scripts.utils.online_raster_lineage')
IDENTIFIER='12345678-1234-1234-1234-123456789abc'
EXPECTED={'schema_version':'addp.lineage-facts/v1','inputs':[{'port':'input.source','locator':'addp://engine/3/path/dem.tif?type=file&item_id=10'}],
          'outputs':[{'port':'output.save','locator':'addp://engine/3/path/outputs/tpi.cog.tif?type=file','write_mode':'create'}],
          'operations':[{'kind':'derive','operator':'raster_save','input_ports':['input.source'],'output_ports':['output.save']}]}
PUBLIC={**EXPECTED,'operations':None}

class RasterLineageTest(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup);self.root=Path(self.temp.name);self.secret=self.root/'secret';self.secret.mkdir(mode=0o700);self.artifacts=self.root/'artifacts';self.artifacts.mkdir()
        self.env={'ONLINE_SUITE':'raster-tpi-persistence','ADDP_ONLINE_TEST_RUN_ID':'tpi-123-1','ADDP_ONLINE_TEST':'1','GITHUB_RUN_ID':'123','GITHUB_RUN_ATTEMPT':'1','ADDP_ONLINE_ARTIFACT_DIR':str(self.artifacts),'POSTGRES_USER':'addp','POSTGRES_PASSWORD':'private'}
        self.container={'Id':'a'*64,'Config':{'Labels':{'com.docker.compose.project':'addp-infra','com.docker.compose.service':'postgres','com.docker.compose.project.working_dir':str(m.ROOT)},'Env':['POSTGRES_DB=addp_online','POSTGRES_USER=addp','POSTGRES_PASSWORD=private']},'State':{'Running':True,'StartedAt':'2026-10-28T01:00:00Z'}}
        self.owner={'schema':'addp.raster-lineage-owner/v1','suite':'raster-tpi-persistence','run_id':'tpi-123-1','container_id':'a'*64,'started_at':self.container['State']['StartedAt'],'postgres_user':'addp'}
        self.path=self.secret/m.RECEIPT;self.path.write_text(json.dumps(self.owner));self.path.chmod(0o600)
        self.value={'execution_id':IDENTIFIER,'tenant_id':2,'principal_id':33,'triggered_by':33,'module':'develop','task_type':'workflow','status':'success','workflow_matches':True,'facts':copy.deepcopy(EXPECTED)}

    def read(self,value=None,container=None,result=None,identifier=IDENTIFIER,tenant=2,principal=33,workflow=None):
        with patch.dict(os.environ,self.env,clear=True),patch.object(m,'boundary',return_value=(self.secret,'raster-tpi-persistence','tpi-123-1')),patch.object(m,'inspect',return_value=container or self.container),patch.object(m.subprocess,'run',return_value=result or SimpleNamespace(returncode=0,stdout=json.dumps(value or self.value))) as run:
            answer=m.read_current(os.environ,identifier,tenant,principal,workflow or {'tasks':[]});self.command=run.call_args;return answer

    def collect(self,raw=EXPECTED,public=PUBLIC,reader=None,name='tpi.cog.tif'):
        return m.collect(self.env,'raster-tpi-persistence',IDENTIFIER,name,EXPECTED,{'metadata':{'lineage_facts':copy.deepcopy(public)}},2,33,{'tasks':[]},reader or (lambda *args:copy.deepcopy(raw)))

    def test_public_null_and_complete_internal_are_both_required(self):
        receipt=self.collect();self.assertEqual(receipt['path'],'tpi-lineage/tpi.cog.tif.json')
        m.verify_archive(self.env,'raster-tpi-persistence',[{'name':'tpi.cog.tif','lineage_evidence':receipt}])

    def test_public_cannot_expose_operations_or_omit_them(self):
        for ops in ([],EXPECTED['operations'],'missing'):
            value=copy.deepcopy(PUBLIC)
            if ops=='missing':value.pop('operations')
            else:value['operations']=ops
            with self.subTest(ops=ops),self.assertRaises(m.SuiteError):m.validate(value,EXPECTED,public=True)

    def test_internal_null_empty_missing_extra_and_wrong_operation_rejected(self):
        for fault in ('null','empty','missing','extra','kind','operator','source-port','output-port','duplicate-port'):
            value=copy.deepcopy(EXPECTED)
            if fault=='null':value['operations']=None
            elif fault=='empty':value['operations']=[]
            elif fault=='missing':value.pop('operations')
            elif fault=='extra':value['operations']*=2
            elif fault in ('kind','operator'):value['operations'][0][fault]='wrong'
            elif fault=='source-port':value['operations'][0]['input_ports']=['input.foreign']
            elif fault=='output-port':value['operations'][0]['output_ports']=['output.foreign']
            else:value['operations'][0]['input_ports']*=2
            with self.subTest(fault=fault),self.assertRaises(m.SuiteError):m.validate(value,EXPECTED,public=False)

    def test_resource_role_source_target_create_schema_and_duplicates_rejected_in_both_layers(self):
        for public in (True,False):
            for fault in ('source','source-port','target','mode','schema','duplicate','missing','extra'):
                value=copy.deepcopy(PUBLIC if public else EXPECTED)
                if fault=='source':value['inputs'][0]['locator']='foreign'
                elif fault=='source-port':value['inputs'][0]['port']='input.foreign'
                elif fault=='target':value['outputs'][0]['locator']='foreign'
                elif fault=='mode':value['outputs'][0]['write_mode']='replace'
                elif fault=='schema':value['schema_version']='other'
                elif fault=='duplicate':value['inputs']*=2
                elif fault=='missing':value['inputs']=[]
                else:value['private']='NEVER_ARCHIVE'
                with self.subTest(public=public,fault=fault),self.assertRaises(m.SuiteError):m.validate(value,EXPECTED,public=public)

    def test_rejection_archives_closed_public_and_internal_before_validation(self):
        bad=copy.deepcopy(EXPECTED);bad['operations']=None;bad['secret']='NEVER_ARCHIVE'
        with self.assertRaises(m.SuiteError):self.collect(raw=bad)
        text=(self.artifacts/'tpi-lineage/tpi.cog.tif.json').read_text();self.assertNotIn('NEVER_ARCHIVE',text)
        value=json.loads(text);self.assertEqual(value['actual_internal']['operations'],{'invalid_type':'NoneType'});self.assertEqual(value['actual_public']['operations'],{'invalid_type':'NoneType'})

    def test_read_error_archived_and_never_treated_as_internal_success(self):
        def fail(*args):raise ValueError('NEVER_ARCHIVE')
        with self.assertRaises(m.SuiteError):self.collect(reader=fail)
        text=(self.artifacts/'tpi-lineage/tpi.cog.tif.json').read_text();self.assertNotIn('NEVER_ARCHIVE',text);self.assertEqual(json.loads(text)['read_error'],'ValueError')

    def test_unexpected_contract_string_is_fingerprinted_not_exposed(self):
        value=copy.deepcopy(EXPECTED);value['inputs'][0]['locator']='NEVER_ARCHIVE';self.assertNotIn('NEVER_ARCHIVE',json.dumps(m.closed(value,EXPECTED)))

    def test_archive_cannot_be_replaced_or_have_extra_files(self):
        receipt=self.collect();case={'name':'tpi.cog.tif','lineage_evidence':receipt};p=self.artifacts/receipt['path'];original=p.read_bytes();p.write_bytes(original+b' ')
        with self.assertRaises(m.SuiteError):m.verify_archive(self.env,'raster-tpi-persistence',[case])
        p.write_bytes(original);(p.parent/'extra').touch()
        with self.assertRaises(m.SuiteError):m.verify_archive(self.env,'raster-tpi-persistence',[case])
        with self.assertRaises(FileExistsError):self.collect()

    def test_archive_file_and_directory_symlinks_rejected(self):
        receipt=self.collect();case={'name':'tpi.cog.tif','lineage_evidence':receipt};p=self.artifacts/receipt['path'];p.unlink();p.symlink_to(self.path)
        with self.assertRaises(m.SuiteError):m.verify_archive(self.env,'raster-tpi-persistence',[case])
        p.unlink();p.parent.rmdir();p.parent.symlink_to(self.secret,target_is_directory=True)
        with self.assertRaises(m.SuiteError):self.collect()

    def test_only_approved_suites_and_exact_run_are_admitted(self):
        for key,value in [('ONLINE_SUITE','raster-workflow'),('ADDP_ONLINE_TEST_RUN_ID','tpi-old-1'),('ADDP_ONLINE_TEST','0'),('ADDP_ONLINE_OWNER_MANAGED','1')]:
            with patch.dict(os.environ,{**self.env,key:value},clear=True),patch.object(m.BASE.HOSTED,'deployment_secret',return_value=self.secret),self.subTest(key=key),self.assertRaises(m.SuiteError):m.boundary(os.environ)
        with patch.dict(os.environ,self.env,clear=True),patch.object(m.BASE.HOSTED,'deployment_secret',return_value=self.secret):self.assertEqual(m.boundary(os.environ)[1:2],('raster-tpi-persistence',))

    def test_tri_is_only_an_additional_closed_suite_and_not_a_cross_suite_receipt(self):
        for suite,prefix in [('raster-cut-fill-persistence','cut-fill'),('raster-tpi-persistence','tpi'),('raster-tri-persistence','tri')]:
            env={**self.env,'ONLINE_SUITE':suite,'ADDP_ONLINE_TEST_RUN_ID':prefix+'-123-1'}
            with patch.dict(os.environ,env,clear=True),patch.object(m.BASE.HOSTED,'deployment_secret',return_value=self.secret):self.assertEqual(m.boundary(os.environ)[1:],(suite,prefix+'-123-1'))
            with patch.dict(os.environ,{**env,'ADDP_ONLINE_TEST_RUN_ID':'wrong-123-1'},clear=True),patch.object(m.BASE.HOSTED,'deployment_secret',return_value=self.secret),self.assertRaises(m.SuiteError):m.boundary(os.environ)

    def test_tri_read_and_closed_archive_require_its_own_receipt(self):
        env={**self.env,'ONLINE_SUITE':'raster-tri-persistence','ADDP_ONLINE_TEST_RUN_ID':'tri-123-1'}
        expected=copy.deepcopy(EXPECTED);expected['outputs'][0]['locator']=expected['outputs'][0]['locator'].replace('tpi.cog','tri.cog')
        public={**copy.deepcopy(expected),'operations':None};value={**self.value,'facts':expected}
        with patch.dict(os.environ,env,clear=True),patch.object(m,'boundary',return_value=(self.secret,'raster-tri-persistence','tri-123-1')),patch.object(m,'inspect',return_value=self.container),patch.object(m.subprocess,'run',return_value=SimpleNamespace(returncode=0,stdout=json.dumps(value)))as query:
            with self.assertRaises(m.SuiteError):m.read_current(os.environ,IDENTIFIER,2,33,{'tasks':[]})
            query.assert_not_called()
            self.path.write_text(json.dumps({**self.owner,'suite':'raster-tri-persistence','run_id':'tri-123-1'}))
            actual=m.read_current(os.environ,IDENTIFIER,2,33,{'tasks':[]});self.assertEqual(actual,expected)
            self.assertIn('BEGIN READ ONLY;',query.call_args.kwargs['input'])
            proof=m.collect(env,'raster-tri-persistence',IDENTIFIER,'tri.cog.tif',expected,{'metadata':{'lineage_facts':public}},2,33,{'tasks':[]},lambda *args:actual)
            self.assertEqual(proof['path'],'tri-lineage/tri.cog.tif.json')
            m.verify_archive(env,'raster-tri-persistence',[{'name':'tri.cog.tif','lineage_evidence':proof}])

    def test_personal_environment_rejected_before_docker(self):
        with patch.dict(os.environ,{},clear=True),patch.object(m,'inspect') as inspected,self.assertRaises(Exception):m.prepare()
        inspected.assert_not_called()

    def test_prepare_receipt_is_private_immutable_and_contains_no_credentials(self):
        self.path.unlink()
        with patch.dict(os.environ,self.env,clear=True),patch.object(m,'boundary',return_value=(self.secret,'raster-tpi-persistence','tpi-123-1')),patch.object(m,'inspect',return_value=self.container):
            m.prepare();self.assertEqual(self.path.stat().st_mode&0o777,0o600);text=self.path.read_text();self.assertNotIn('private',text);self.assertNotIn('PASSWORD',text)
            with self.assertRaises(FileExistsError):m.prepare()

    def test_fixed_read_only_query_quotes_variables_and_bounds_output(self):
        self.assertEqual(self.read(workflow={'tasks':[{"note":"'; DELETE FROM common.task_executions;--"}]}),EXPECTED)
        args=self.command.args[0];sql=self.command.kwargs['input'];self.assertIn('BEGIN READ ONLY;',sql);self.assertIn('statement_timeout',sql);self.assertIn('LIMIT 1',sql);self.assertNotIn('DELETE',sql);self.assertIn('workflow_matches',sql);self.assertNotIn('POSTGRES_PASSWORD',' '.join(args));self.assertEqual(self.command.kwargs['timeout'],8)

    def test_invalid_execution_user_or_tenant_cannot_read(self):
        for kwargs in ({'identifier':"x';DELETE"},{'tenant':1},{'tenant':True},{'principal':0},{'principal':True}):
            with self.subTest(kwargs=kwargs),self.assertRaises(m.SuiteError):self.read(**kwargs)

    def test_current_execution_identity_and_workflow_are_required(self):
        for key,value in [('execution_id','foreign'),('tenant_id',3),('tenant_id',True),('principal_id',34),('triggered_by',34),('module','meta'),('task_type','query'),('status','running'),('workflow_matches',False),('workflow_matches',1)]:
            changed=copy.deepcopy(self.value);changed[key]=value
            with self.subTest(key=key,value=value),self.assertRaises(m.SuiteError):self.read(value=changed)

    def test_container_identity_restart_labels_and_database_cannot_change(self):
        for fault in ('id','restart','project','service','root','database','stopped'):
            c=copy.deepcopy(self.container)
            if fault=='id':c['Id']='b'*64
            elif fault=='restart':c['State']['StartedAt']='2026-10-28T02:00:00Z'
            elif fault=='database':c['Config']['Env'][0]='POSTGRES_DB=addp'
            elif fault=='stopped':c['State']['Running']=False
            else:c['Config']['Labels'][{'project':'com.docker.compose.project','service':'com.docker.compose.service','root':'com.docker.compose.project.working_dir'}[fault]]='foreign'
            with self.subTest(fault=fault),self.assertRaises(Exception):self.read(container=c)

    def test_private_receipt_scope_mode_and_symlink_cannot_change(self):
        for key,value in [('run_id','old'),('suite','raster-workflow'),('schema','old'),('postgres_user','foreign')]:
            changed={**self.owner,key:value};self.path.write_text(json.dumps(changed))
            with self.subTest(key=key),self.assertRaises(Exception):self.read()
        self.path.write_text(json.dumps(self.owner));self.path.chmod(0o644)
        with self.assertRaises(m.SuiteError):self.read()
        self.path.unlink();self.path.symlink_to(self.root/'missing')
        with self.assertRaises(m.SuiteError):self.read()

    def test_sql_failure_empty_malformed_and_oversized_results_fail_closed(self):
        for result in (SimpleNamespace(returncode=1,stdout='NEVER_ARCHIVE'),SimpleNamespace(returncode=0,stdout=''),SimpleNamespace(returncode=0,stdout='null'),SimpleNamespace(returncode=0,stdout='{}'),SimpleNamespace(returncode=0,stdout='x'*40001)):
            with self.subTest(result=result),self.assertRaises(Exception):self.read(result=result)

if __name__=='__main__':unittest.main()

from importlib import import_module
import copy
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

FLOW=import_module('scripts.test.gaussdb-consumer-flow-online')
FIXTURE=import_module('business.scripts.online-gaussdb-consumer-fixture')


class GaussDBConsumerTest(unittest.TestCase):
    def sample(self):
        return {'preview_type':'table','data':{'total':2,'columns':['id','name','amount','optional_text'],'rows':[
            {'id':'9007199254740993','name':'高斯正式接入中文','amount':'12345678901234567890.123456789012345678','optional_text':None},
            {'id':'2','name':'保留尾空格  ','amount':'-.000000000000000001','optional_text':None}]}}

    def test_exact_native_precision_and_nulls(self):
        self.assertEqual(len(FLOW.validate_preview(self.sample())),2)

    def test_rounded_integer_numeric_decimal_and_trimmed_text_rejected(self):
        for field,value,row in [('id',9007199254740992,0),('id','9007199254740992',0),('amount',123.5,0),('amount','12345678901234567890.1',0),('name','保留尾空格',1),('optional_text','',1)]:
            data=self.sample()
            data['data']['rows'][row][field]=value
            with self.subTest(field=field,value=value), self.assertRaises(FLOW.API.SuiteError):
                FLOW.validate_preview(data)

    def test_scanned_item_requires_precise_schema_and_primary_key(self):
        item={'item_type':'table','full_name':'addp_acceptance.source','fingerprint':'demo',
              'attributes':{'schema_version':1,'item':{'data_type':'table'},'type_info':{'table':{
                  'primary_key':['name','id'],'row_count':2,'fields':[
                      {'name':'id','type':'bigint','primary_key':True},
                      {'name':'name','type':'string','primary_key':True},
                      {'name':'amount','type':'decimal','precision':38,'scale':18},
                      {'name':'optional_text','type':'string'}]}}}}
        FLOW.validate_item(item)
        for change in ('precision','primary_key','type','uncertified','wrong_partition'):
            wrong=copy.deepcopy(item)
            if change=='precision':wrong['attributes']['type_info']['table']['fields'][2]['precision']=18
            elif change=='type':wrong['attributes']['type_info']['table']['fields'][0]['type']='double'
            elif change=='primary_key':wrong['attributes']['type_info']['table']['primary_key']=[]
            elif change=='wrong_partition':wrong['attributes']['table']=wrong['attributes'].pop('type_info')['table']
            else:wrong['attributes']['capabilities']={'indexes':[],'indexing':{}}
            with self.subTest(change=change),self.assertRaises(FLOW.API.SuiteError):FLOW.validate_item(wrong)

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
        self.assertFalse(any(p.startswith(('transfer.','develop.','system.engine.')) for p in FLOW.PERMISSIONS))
        hosted=(FIXTURE.ROOT/'scripts/test/online-hosted-gaussdb-gate.sh').read_text()
        self.assertIn('source "$ROOT_DIR/scripts/utils/hosted-online.sh"',hosted)
        self.assertIn('online-gaussdb-consumer-fixture.py stop',hosted)
        self.assertIn('scripts/test/online-engine-registration.py',hosted)
        self.assertNotIn('restart.sh',hosted)

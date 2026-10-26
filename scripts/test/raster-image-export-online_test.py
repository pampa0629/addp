import copy
import importlib
from pathlib import Path
from types import SimpleNamespace
import unittest
from unittest.mock import patch

owner = importlib.import_module('scripts.test.raster-workflow-online')
m = importlib.import_module('scripts.test.raster-image-export-online')


def identity():
    return {'principal_id':'33','tenant_id':'2','principal_type':'user',
            'permissions_verified':sorted(owner.EDITOR_PERMISSIONS)}


def artifact(profile):
    return {'artifact_type':'raster','format':profile,'profile':profile,'width':37,'height':23,
        'band_count':4 if profile=='png' else 3,'lossy':profile=='jpeg','crs':'','source_crs':'',
        'extent_srid':0,'extent':[],'transform':[],'uploaded_files':1,'size_bytes':1024,
        **({'jpeg_quality':85} if profile=='jpeg' else {}),
        'bands':[{'dtype':'Byte','color_interpretation':c} for c in
                 ['Red','Green','Blue']+(['Alpha'] if profile=='png' else [])]}


def physical(action):
    return {'source_unchanged':True,'preserved_sha256':{'old.tif':'old-hash'},
        'images':{} if action=='prepare-images' else {p:{'format':p,'width':37,'height':23,
            'band_count':4 if p=='png' else 3,'lossy':p=='jpeg','georeferencing_absent':True,
            'sidecars_absent':True,'sha256':p+'-bytes','decoded_sha256':p+'-pixels',
            'mean_absolute_error':0 if p=='png' else 1,'max_absolute_error':0 if p=='png' else 3,
            'size_bytes':1024} for p in ('png','jpeg')}}


class Client:
    def __init__(self):
        self.items={}
        for i,(p,name) in enumerate(owner.fixture.IMAGE_SOURCES.items(),10):
            self.items[i]={'id':i,'full_name':'raster-source/'+name,'item_type':'object',
                'fingerprint':name,'size_bytes':2048,'updated_at':'unchanged','attributes':{'item':{'format':'tiff'}}}
        for i,(p,name) in enumerate(owner.fixture.IMAGE_TARGETS.items(),20):
            self.items[i]={'id':i,'full_name':'raster-target/'+name,'item_type':'object','size_bytes':1024,
                'attributes':{'item':{'format':p},'type_info':{'media':{'width':37,'height':23}}}}
    def request(self,method,path,statuses):
        assert method=='GET'
        if path=='/api/v1/develop/workflow-engines':
            return SimpleNamespace(payload=[{'id':8,'engine_type':'geopython_workflow','connection_status':'online'}])
        if path.startswith('/api/v1/meta/items/'):
            return SimpleNamespace(payload=copy.deepcopy(self.items[int(path.rsplit('/',1)[1])]))
        raise AssertionError(path)
    def find(self,client,engine,name,label):
        return next(copy.deepcopy(item) for item in self.items.values() if item['full_name']==name)


def browser_report(evidence):
    cases=[]
    for index,p in enumerate(('png','jpeg'),500):
        definition=copy.deepcopy(evidence['definitions'][p])
        definition['tasks'][1]['params']['profile']=p
        if p=='jpeg': definition['tasks'][1]['params']['jpeg_quality']=85
        cases.append({'profile':p,'task_id':index,'source_task_id':str(index),'execution_id':'exec-'+p,
                      'workflow_definition':definition,'reloaded':True,'deleted':True})
    return {**copy.deepcopy(evidence),'schema_version':m.BROWSER_SCHEMA,'result':'passed',
            'cases':cases,'page_requests':{'create':2,'update':0,'execute':2}}


def execution(module,execution_id):
    if module=='meta':return {'status':'success'}
    p=execution_id.removeprefix('exec-')
    return {'status':'success','source_task_id':'500' if p=='png' else '501',
        'outputs':{'save':{'resource':{'locator':'addp://engine/2/path/raster-target/'+owner.fixture.IMAGE_TARGETS[p]+'?type=object',
                   'type':'object','write_mode':'create'}}},
        'metadata':{'result':{'final_result':artifact(p),'meta_scan_runs':[{'status':'submitted',
            'target_locator':'addp://engine/2/path/raster-target/'+owner.fixture.IMAGE_TARGETS[p]+'?type=object',
            'execution_id':'scan-'+p}]}}}


class OrdinaryImages(unittest.TestCase):
    def run_scene(self,fault=None):
        client=Client();observed=[]
        def native(repo,env,action):
            observed.append(action);data=physical(action)
            if fault=='changed_prior' and action=='verify-images':data['preserved_sha256']['old.tif']='changed'
            if fault=='missing_image' and action=='verify-images':del data['images']['jpeg']
            if fault=='png_loss' and action=='verify-images':data['images']['png']['max_absolute_error']=1
            if fault=='jpeg_loss' and action=='verify-images':data['images']['jpeg']['mean_absolute_error']=5
            if fault=='jpeg_nan' and action=='verify-images':data['images']['jpeg']['mean_absolute_error']=float('nan')
            if fault=='native_size' and action=='verify-images':data['images']['png']['width']=38
            return data
        def editor(repo,env,evidence):
            report=browser_report(evidence)
            if fault=='quality':report['cases'][1]['workflow_definition']['tasks'][1]['params']['jpeg_quality']=50
            if fault=='private_plan':report['cases'][0]['workflow_definition']['tasks'][1]['params']['access_plan']={}
            if fault=='not_deleted':report['cases'][0]['deleted']=False
            if fault=='not_reloaded':report['cases'][0]['reloaded']=False
            if fault=='wrong_writes':report['page_requests']['execute']=1
            return report
        def wait(client,module,eid,timeout):
            result=execution(module,eid)
            if fault=='spatial' and module=='develop':result['metadata']['result']['final_result']['crs']='EPSG:4326'
            if fault=='missing_scan' and module=='develop':result['metadata']['result']['meta_scan_runs']=[]
            if fault=='wrong_size' and module=='develop':result['metadata']['result']['final_result']['size_bytes']=True
            if fault=='wrong_source_task' and module=='develop':result['source_task_id']='999'
            return result
        env={'ADDP_ONLINE_TEST_TENANT_ID':'2','ADDP_ONLINE_RASTER_SOURCE_ENGINE_ID':'1',
             'ADDP_ONLINE_RASTER_TARGET_ENGINE_ID':'2','ADDP_ONLINE_TEST_RUN_ID':'raster-test'}
        with (patch.object(owner,'validate_identity',return_value=identity()) as validate,
              patch.object(owner.support,'find_fixture_item',side_effect=client.find),
              patch.object(owner,'wait_execution',side_effect=wait)):
            result=m.run_scenario(owner,Path('.'),env,client,physical_runner=native,editor_runner=editor)
        self.assertEqual(validate.call_args.kwargs['required_permissions'],owner.EDITOR_PERMISSIONS)
        self.assertEqual(observed,['prepare-images','verify-images'])
        return result

    def test_two_real_ui_export_receipts_require_full_evidence(self):
        report=self.run_scene()
        self.assertEqual(report['schema_version'],m.SCHEMA)
        self.assertEqual(report['result'],'passed')
        self.assertEqual([c['profile'] for c in report['cases']],['png','jpeg'])
        self.assertTrue(all(c['source_unchanged'] for c in report['cases']))

    def test_incomplete_or_modified_evidence_is_rejected(self):
        for fault in ('changed_prior','missing_image','png_loss','jpeg_loss','jpeg_nan','native_size','quality','private_plan','not_deleted',
                      'not_reloaded','wrong_writes','spatial','missing_scan','wrong_size','wrong_source_task'):
            with self.subTest(fault=fault), self.assertRaises(owner.SuiteError):self.run_scene(fault)

    def test_image_fixture_is_independent_and_has_partial_alpha(self):
        data=owner.fixture.ordinary_image_pixels('png',4)
        self.assertEqual(len(data),37*23)
        self.assertEqual(set(data),{0,127,254})
        self.assertEqual(owner.fixture.ordinary_image_pixels('jpeg',1)[:4],bytes([30,32,34,36]))
        self.assertEqual(owner.fixture.ordinary_image_pixels('jpeg',2)[37],72)
        self.assertEqual(owner.fixture.ordinary_image_pixels('jpeg',3)[0],150)

    def test_registered_standard_entry_and_full_hosted_phase(self):
        root=Path(__file__).resolve().parents[2]
        self.assertIn('scripts/test/raster-image-export-online_test.py',(root/'Makefile').read_text())
        main=(root/'scripts/test/raster-workflow-online.py').read_text()
        self.assertIn("report['image_exports'] = images.run_scenario",main)
        browser=(root/'console/frontend/e2e/online/raster-workflow.spec.js').read_text()
        for marker in ('栅格普通图片首次保存、重载与真实执行','setInputFiles','jpeg_quality','page.reload()',
                       "schema_version: 'addp.raster-image-editor/v1'"):
            self.assertIn(marker,browser)

    def test_ci_registration_rejects_removed_image_phase_or_standard_entry(self):
        registry=importlib.import_module('scripts.ci.check-online-ci-registration')
        root=Path(__file__).resolve().parents[2]
        original=Path.read_text
        for relative,marker in (
            ('Makefile','scripts/test/raster-image-export-online_test.py'),
            ('Makefile','test_raster_image_export.py'),
            ('scripts/test/raster-workflow-online.py',"report['image_exports']"),
            ('scripts/test/raster-image-export-online.py','preserved_sha256'),
            ('console/frontend/e2e/online/raster-workflow.spec.js','栅格普通图片首次保存、重载与真实执行')):
            def read(path,*args,**kwargs):
                text=original(path,*args,**kwargs)
                return text.replace(marker,'REMOVED') if path==root/relative else text
            with self.subTest(relative=relative,marker=marker), patch.object(Path,'read_text',read):
                with self.assertRaises(registry.RegistrationError):
                    registry.validate_raster_workflow_profile(root,{'raster-workflow'})


if __name__=='__main__':unittest.main()

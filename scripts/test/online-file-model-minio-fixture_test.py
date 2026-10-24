from importlib import import_module
import json
import hashlib
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

M = import_module('scripts.test.online-file-model-minio-fixture')
FLOW = import_module('scripts.test.ftp-consumer-flow-online')

class MinIOModelFixtureTest(unittest.TestCase):
    def test_refuses_personal_environment_and_foreign_container(self):
        with patch.dict(os.environ, {'ADDP_ONLINE_HOSTED':'0','ADDP_ONLINE_SECRET_DIR':'/tmp'}):
            with self.assertRaises(RuntimeError): M.guard()
        with tempfile.TemporaryDirectory() as directory, patch.object(M,'guard',return_value=(Path(directory),'ftp-1-1')), \
             patch.object(M,'docker',side_effect=[b'id',json.dumps([{'Config':{'Labels':{'com.addp.online-run-id':'other'}}}]).encode()]):
            with self.assertRaises(RuntimeError): M.owned()

    def test_object_target_preserves_bucket_prefix_and_spaces(self):
        target=FLOW.file_target(12,'bucket/models/ tex.png   ','minio')
        self.assertEqual([part['term'] for part in target['segments']], ['service','bucket','prefix','object'])
        self.assertEqual(target['segments'][-1]['name'],' tex.png   ')
        with self.assertRaises(FLOW.SuiteError): FLOW.file_target(12,'bucket','minio')

    def test_same_exact_model_fixture_is_seeded_as_objects_and_descriptor_is_private(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory); store={}; calls=[]
            facts={'NetworkSettings':{'Ports':{'9000/tcp':[{'HostIp':'127.0.0.1','HostPort':'49155'}]}}}
            class Response:
                status=200
                def __enter__(self):return self
                def __exit__(self,*args):pass
            with patch.object(M,'guard',return_value=(root,'ftp-1-1')),patch.object(M,'owned',side_effect=[None,facts]), \
                 patch.object(M,'docker',side_effect=lambda *args,**kw:calls.append(args)),patch.object(M,'mc'), \
                 patch.object(M,'put',side_effect=lambda key,data:store.update({key:data})),patch.object(M,'read',side_effect=lambda key:store[key]), \
                 patch.object(M.urllib.request,'urlopen',return_value=Response()):
                M.start()
            self.assertEqual(len(store),14)
            self.assertIn('models/dae/model.dae',store)
            self.assertIn('models/dae/texture.png',store)
            descriptor=root/'ftp-minio-engine.json'
            self.assertEqual(descriptor.stat().st_mode & 0o777,0o600)
            self.assertEqual(json.loads(descriptor.read_text())['engine_origin'],'general')
            self.assertTrue(all(row['full_name'].startswith('model-source/') for row in json.loads((root/'minio-model-samples.json').read_text())))
            self.assertIn('127.0.0.1::9000',calls[0])
            manifests=json.loads((root/'minio-model-samples.json').read_text())
            items=[]
            for index,sample in enumerate(manifests):
                kind=sample['format']
                if kind not in {'ifc','3ds','dae','obj','stl'}: continue
                name=sample['full_name']
                refs=[{'path':name,'role':'model','required':True,'primary':True}]
                if kind in {'3ds','dae'}: refs.append({'path':name.rsplit('/',1)[0]+'/texture.png','role':'texture','required':True,'primary':False})
                if kind=='obj':
                    refs.extend({'path':name.rsplit('/',1)[0]+'/'+leaf,'role':role,'required':True,'primary':False}
                        for leaf,role in [('paint blue.mtl','material_library'),('paint red.mtl','material_library_2'),('blue/color.png','texture'),('red/color.png','texture_2')])
                items.append({'id':index+1,'engine_id':17,'item_type':'object','full_name':name,
                    'fingerprint':hashlib.sha256(f'17:{name}'.encode()).hexdigest(),
                    'attributes':{'schema_version':1,'item':{'data_type':'model_3d','layout':'single','format':kind,'refs':refs},
                        'storage':{'physical_path':name},'format_info':{kind:{'scan_complete':True,'texture_refs':['texture.png']}}}})
                if kind=='stl':
                    items[-1]['attributes']['format_info']={'stl':{'composition_complete':True}}
                if kind=='obj':
                    items[-1]['attributes']['format_info']={'obj':{'composition_complete':True,'material_libraries':[{'reference':'paint blue.mtl','texture_refs':['blue/color.png']},{'reference':'paint red.mtl','texture_refs':['red/color.png']}]}}
            with patch.dict(os.environ,{'ADDP_ONLINE_MODEL_FILE_ENGINE_TYPE':'minio'}):
                models=FLOW.prepare_models(items,17,manifests)
                self.assertEqual([model['format'] for model in models],['ifc','3ds','dae','obj','stl'])
                dae=next(row for row in models if row['format']=='dae')
                self.assertEqual([ref['path'] for ref in dae['composition']],
                    ['model-source/models/dae/model.dae','model-source/models/dae/texture.png'])
                dae_item=next(row for row in items if row['full_name'].endswith('model.dae'))
                dae_item['attributes']['format_info']['dae']['scan_complete']=False
                with self.assertRaises(FLOW.SuiteError): FLOW.prepare_models(items,17,manifests)


    def test_obj_mtl_change_preserves_primary_and_restores_exact_owned_objects(self):
        fixture=Path(__file__).resolve().parents[2]/'business/fixtures/manager'
        for fail in (False,True):
            with self.subTest(fail=fail):
                original={relative:(fixture/source).read_bytes() for _,relative,source in M.SEED.MODEL_SAMPLE_FILES if relative.startswith('models/obj/')}
                store=dict(original)
                model={'format':'obj','full_name':'model-source/models/obj/model.obj','sha256':hashlib.sha256(store['models/obj/model.obj']).hexdigest(),
                       'composition':[{'role':'texture_2','path':'model-source/models/obj/red/color.png'}]}
                def mc(action,*args):
                    if action=='ls':return b''
                    if action=='rm':del store[args[0].removeprefix('fixture/model-source/')];return b''
                    raise AssertionError(action)
                with patch.object(M,'guard',return_value=(Path('/tmp'),'owned')),patch.object(M,'mc',side_effect=mc), \
                     patch.object(M,'read',side_effect=lambda key:store[key]),patch.object(M,'put',side_effect=lambda key,raw:store.__setitem__(key,raw)):
                    try:
                        with M.changed_texture(model) as changed:
                            self.assertEqual(changed['sha256'],model['sha256'])
                            self.assertEqual(store['models/obj/model.obj'],original['models/obj/model.obj'])
                            self.assertIn(b'red/changed.png',store['models/obj/paint red.mtl'])
                            self.assertEqual(changed['composition'][0]['path'],'model-source/models/obj/red/changed.png')
                            if fail:raise RuntimeError('synthetic consumer')
                    except RuntimeError:
                        if not fail:raise
                self.assertEqual(store,original)

    def test_changed_composition_restores_original_and_removes_only_its_object(self):
        fixture=Path(__file__).resolve().parents[2]/'business/fixtures/manager'
        for kind in ('3ds', 'dae'):
            for fail in (False, True):
                with self.subTest(kind=kind, fail=fail):
                    main=(fixture/f'addp_online_model_fixture.{kind}').read_bytes()
                    texture=(fixture/'addp_online_exchange_texture.png').read_bytes()
                    primary=f'models/{kind}/model.{kind}'
                    original={primary:main,f'models/{kind}/texture.png':texture}
                    store=dict(original)
                    replacement='changed.png' if kind=='dae' else ' tex.png   '
                    model={'format':kind,'full_name':'model-source/'+primary,
                           'composition':[{'role':'texture','path':f'model-source/models/{kind}/texture.png'}]}
                    def mc(action,*args):
                        if action=='ls':return b''
                        self.assertEqual(action,'rm'); del store[args[0].removeprefix('fixture/model-source/')]
                    with patch.object(M,'guard',return_value=(Path('/tmp'),'ftp-1-1')),patch.object(M,'read',side_effect=lambda key:store[key]), \
                         patch.object(M,'put',side_effect=lambda key,data:store.update({key:data})),patch.object(M,'mc',side_effect=mc):
                        try:
                            with M.changed_texture(model) as changed:
                                self.assertEqual(changed['composition'][0]['path'],f'model-source/models/{kind}/{replacement}')
                                self.assertNotEqual(store[primary],main)
                                if fail: raise RuntimeError('consumer failed')
                        except RuntimeError as error:
                            self.assertTrue(fail)
                            self.assertEqual(str(error),'consumer failed')
                    self.assertEqual(store,original)

from importlib import import_module
import json
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
            self.assertEqual(len(store),6)
            self.assertNotIn('models/dae/model.dae',store)
            descriptor=root/'ftp-minio-engine.json'
            self.assertEqual(descriptor.stat().st_mode & 0o777,0o600)
            self.assertEqual(json.loads(descriptor.read_text())['engine_origin'],'general')
            self.assertTrue(all(row['full_name'].startswith('model-source/') for row in json.loads((root/'minio-model-samples.json').read_text())))
            self.assertIn('127.0.0.1::9000',calls[0])

    def test_changed_composition_restores_original_and_removes_only_its_object(self):
        fixture=Path(__file__).resolve().parents[2]/'business/fixtures/manager'
        main=(fixture/'addp_online_model_fixture.3ds').read_bytes()
        texture=(fixture/'addp_online_exchange_texture.png').read_bytes()
        store={'models/3ds/model.3ds':main,'models/3ds/texture.png':texture}
        model={'full_name':'model-source/models/3ds/model.3ds','composition':[{'role':'texture','path':'model-source/models/3ds/texture.png'}]}
        def mc(action,*args):
            if action=='ls':return b''
            self.assertEqual(action,'rm'); del store[args[0].removeprefix('fixture/model-source/')]
        with patch.object(M,'guard',return_value=(Path('/tmp'),'ftp-1-1')),patch.object(M,'read',side_effect=lambda key:store[key]), \
             patch.object(M,'put',side_effect=lambda key,data:store.update({key:data})),patch.object(M,'mc',side_effect=mc):
            with M.changed_texture(model) as changed:
                self.assertEqual(changed['composition'][0]['path'],'model-source/models/3ds/ tex.png   ')
                self.assertNotEqual(store['models/3ds/model.3ds'],main)
            self.assertEqual(store,{'models/3ds/model.3ds':main,'models/3ds/texture.png':texture})

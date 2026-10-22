from importlib import import_module
import json
from types import SimpleNamespace
import os
import shutil
import stat
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

M = import_module('scripts.test.online-file-model-nfs-fixture')
FTP = import_module('scripts.test.ftp-consumer-flow-online')

class NFSModelFixtureTest(unittest.TestCase):
    def test_nfs_fixture_is_hosted_only_and_subdirectory_is_not_an_arbitrary_path(self):
        with self.assertRaises(M.NFS.FixtureError):
            M.NFS.NFSExport('ftp', M.command, source_subdirectory='../data')
        with self.assertRaises(M.NFS.FixtureError):
            M.NFS.NFSExport('ftp', M.command, source_subdirectory='data').guard({})

    def test_nfs_composition_mutation_uses_its_independent_owner_not_ftp_container(self):
        with tempfile.TemporaryDirectory() as directory, patch.dict(os.environ, ADDP_ONLINE_MODEL_FILE_ENGINE_TYPE='nfs'):
            root=Path(directory)
            with patch.object(M,'owned_root',return_value=root) as owner, patch.object(FTP.subprocess,'run',side_effect=AssertionError('FTP inspect bypass')):
                self.assertEqual(FTP.owned_ftp_fixture(),root)
                owner.assert_called_once_with()

    def test_owned_lifecycle_exports_only_data_seeds_models_and_removes_everything(self):
        with tempfile.TemporaryDirectory() as directory, tempfile.TemporaryDirectory(prefix='addp-ftp-',dir='/tmp') as source:
            root=Path(source).resolve(); secret=Path(directory).resolve()
            exports=[]; calls=[]
            def command(args,**kwargs):
                calls.append(args)
                if args==['uname','-sm']: return 'Linux x86_64'
                if args==['sudo','exportfs','-s']: return ''.join(exports)
                if args[:2]==['sudo','tee']: exports[:]=[kwargs['input']]; return kwargs['input']
                if args[:2]==['showmount','-e']: return 'Export list for 127.0.0.1:\n'+''.join(exports)
                if args[:3]==['sudo','rm','-f']: exports.clear()
                if args[:4]==['sudo','rm','-rf','--']: shutil.rmtree(args[4])
                return ''
            environment={'GITHUB_ACTIONS':'true','RUNNER_OS':'Linux','ADDP_ONLINE_HOST':'1',
                'ADDP_ONLINE_HOSTED':'1','ADDP_ONLINE_TEST_RUN_ID':'ftp-42-1','ADDP_ONLINE_SECRET_DIR':str(secret)}
            with patch.dict(os.environ,environment),patch.object(M,'command',side_effect=command), \
                    patch.object(M.NFS.tempfile,'mkdtemp',return_value=str(root)),patch('builtins.print'):
                M.main('start')
                self.assertEqual(exports,[f'{root}/data 127.0.0.1(ro,sync,no_subtree_check,insecure,root_squash)\n'])
                self.assertEqual(M.owned_root(),root)
                self.assertTrue((root/'data/models/ifc/model.ifc').is_file())
                self.assertTrue((root/'data/models/3ds/texture.png').is_file())
                self.assertTrue((root/'data/unsupported/inside-link').is_symlink())
                self.assertTrue(stat.S_ISFIFO((root/'data/unsupported/fifo').lstat().st_mode))
                M.main('stop')
                self.assertFalse(root.exists())
                self.assertFalse(exports)
                self.assertFalse((secret/'ftp-root').exists())
                self.assertFalse((secret/'ftp-nfs-engine.json').exists())
                self.assertFalse((secret/'nfs-model-samples.json').exists())

    def test_both_profiles_are_required_and_keep_separate_evidence(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory)
            for name in ('ftp-samples.json','ftp-model-samples.json','nfs-model-samples.json','minio-samples.json','minio-model-samples.json'):
                (root/name).write_text('[]')
            environment={'ADDP_ONLINE_TEST':'1','ADDP_ONLINE_HOSTED':'1',
                'ADDP_ONLINE_TEST_TENANT_ID':'7','ADDP_ONLINE_FTP_ENGINE_ID':'17','ADDP_ONLINE_NFS_ENGINE_ID':'18','ADDP_ONLINE_MINIO_ENGINE_ID':'19',
                'ADDP_FTP_ADMISSION_DIR':directory,'ADDP_ONLINE_SECRET_DIR':directory,
                'ADDP_ONLINE_ARTIFACT_DIR':directory,'GATEWAY_URL':'http://localhost',
                'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN':'synthetic'}
            observations=[]
            def run_profile(client, tenant, engine, samples, models, timeout, kind):
                observations.append((engine,kind,os.environ['ADDP_ONLINE_ARTIFACT_DIR']))
                return {'engine_id':engine,'source_engine_type':kind}
            with patch.dict(os.environ,environment), patch.object(FTP,'run_profile',side_effect=run_profile), \
                    patch.object(FTP.SUPPORT,'GatewayClient',return_value=SimpleNamespace()),patch('builtins.print'):
                FTP.main()
                self.assertEqual(os.environ['ADDP_ONLINE_ARTIFACT_DIR'],directory)
                self.assertNotIn('ADDP_ONLINE_MODEL_FILE_ENGINE_TYPE',os.environ)
            self.assertEqual(observations,[(17,'ftp',directory),(18,'nfs',str(root/'nfs')),(19,'minio',str(root/'minio'))])
            report=json.loads((root/'ftp-consumer-flow.json').read_text())
            self.assertEqual(report['nfs_profile'],{'engine_id':18,'source_engine_type':'nfs'})

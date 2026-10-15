import importlib
import io
import json
import os
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

m = importlib.import_module('business.scripts.online-raster-capacity-fixture')


class CapacityFixtureTest(unittest.TestCase):
    def test_directory_names_windows_and_inventory_cannot_expand_cleanup_scope(self):
        for repetition in (1,2):
            for concurrency in (1,4):
                names=[f'capacity-mosaic-l{concurrency}-r{repetition}']
                self.assertEqual(m.mosaic_names_checked(names),names)
        for names in ([],['keep'],['capacity-mosaic-l2-r1'],['capacity-mosaic-l1-r3'],
                      ['capacity-mosaic-l1-r1/../keep'],['capacity-mosaic-l1-r1']*2,[True]):
            with self.subTest(names=names),self.assertRaises(m.shared.FixtureError):m.mosaic_names_checked(names)
        self.assertEqual(set(m.mosaic_windows().values()),{(0,0,5490,5490),(5490,0,5490,5490),
                                                        (0,5490,5490,5490),(5490,5490,5490,5490)})
        self.assertEqual(len(m.mosaic_paths()),7)
        for size in (0,63,True):
            with patch.object(m,'SIZE',size),self.assertRaises(m.shared.FixtureError):m.mosaic_windows()

    def test_directory_markers_are_bounded_empty_objects_and_are_not_data_files(self):
        name='capacity-mosaic-l1-r1';objects={name+'/'+p:123 for p in m.mosaic_paths()}
        self.assertEqual(m.mosaic_objects_checked(objects,name),[])
        objects.update({name+'/':0,name+'/leaf/':0,name+'/index/':0,name+'/overviews/':0})
        self.assertEqual(len(m.mosaic_objects_checked(objects,name)),4)
        for key,value in ((name+'/keep/',0),(name+'/leaf/',1),(name+'/leaf/',False),('other/',0)):
            invalid={**objects,key:value}
            with self.subTest(key=key,value=value),self.assertRaises(m.shared.FixtureError):m.mosaic_objects_checked(invalid,name)
        del objects[name+'/mosaic.addp.json']
        with self.assertRaises(m.shared.FixtureError):m.mosaic_objects_checked(objects,name)

    def test_target_names_are_bounded_unique_and_reject_arbitrary_deletion(self):
        valid = ['capacity-c64-n1-r1-0.cog.tif','capacity-c64-n1-r1-1.cog.tif']
        self.assertEqual(m.names_checked(valid), valid)
        for names in ([], valid*2, [valid[0]]*2, ['keep.txt'], ['../'+valid[0]], [True], valid+[valid[0]],
                      ['capacity-c32-n1-r1-0.cog.tif']):
            with self.subTest(names=names), self.assertRaises(m.shared.FixtureError): m.names_checked(names)

    def test_download_fails_closed_on_length_content_or_changed_checksum(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            asset = ('B02',3,'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad')
            for content, length, passes in ((b'abc','3',True),(b'abc','4',False),(b'ab','3',False),(b'xyz','3',False)):
                response = io.BytesIO(content); response.headers={'Content-Length':length}
                with patch.object(m.urllib.request,'urlopen',return_value=response) as opened:
                    if passes:
                        result=m.download_asset(root,asset)
                        self.assertEqual(result['size_bytes'],3)
                    else:
                        with self.assertRaises(m.shared.FixtureError): m.download_asset(root,asset)
                    self.assertEqual(opened.call_args.args[0],m.PUBLIC_ROOT+'/B02.tif')

    def test_disk_minio_has_owned_directory_user_and_not_tmpfs(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            entry=dict(endpoint='127.0.0.1:59012',bucket='raster-source',access_key='private',secret_key='private')
            with patch.dict(os.environ,ADDP_ONLINE_TEST_RUN_ID='test'), patch.object(m.shared,'command',return_value=''), patch.object(m.shared.urllib.request,'urlopen') as opened:
                opened.return_value.__enter__.return_value=SimpleNamespace(status=200)
                m.shared.start_minio(root,entry,m.CONTAINERS[0],m.SUITE,data_directory=root/'data-source')
                calls=m.shared.command.call_args_list
            launch=calls[0].args[0]
            self.assertIn(f'com.addp.online-fixture={m.SUITE}',launch)
            self.assertIn('--user',launch)
            self.assertIn(f'{os.getuid()}:{os.getgid()}',launch)
            self.assertIn(f'type=bind,source={root}/data-source,destination=/data',launch)
            self.assertNotIn('type=tmpfs,destination=/data',launch)
            self.assertEqual((root/'data-source').stat().st_mode & 0o777,0o700)

    def test_cleanup_continues_after_owner_refusal_and_reports_failure(self):
        def owned(name,suite):
            self.assertEqual(suite,m.SUITE)
            if name==m.CONTAINERS[2]: raise m.shared.FixtureError('foreign')
            return False
        with patch.object(m.shared,'owned',side_effect=owned) as ownership, patch.object(m.shared,'command') as command:
            with self.assertRaises(m.shared.FixtureError): m.stop()
            self.assertEqual(ownership.call_count,5)
            command.assert_not_called()

    def test_same_suite_foreign_run_is_never_deleted(self):
        container={'Config':{'Labels':{'com.addp.online-run-id':'foreign'}}}
        with patch.dict(os.environ,ADDP_ONLINE_TEST_RUN_ID='ours'), patch.object(m.shared,'owned',return_value=True), patch.object(m.shared,'command',return_value=json.dumps([container])) as command:
            with self.assertRaises(m.shared.FixtureError): m.stop()
            self.assertTrue(command.call_args_list)
            self.assertTrue(all(call.args[0][1]=='inspect' for call in command.call_args_list))

    def test_unknown_physical_action_rejected_before_environment_or_mutation(self):
        with patch.object(m.shared,'environment') as environment,patch.object(m.shared,'command') as command:
            with self.assertRaises(m.shared.FixtureError): m.physical('foreign-delete',[])
            environment.assert_not_called(); command.assert_not_called()

    def test_capacity_disk_preflight_is_fail_closed(self):
        with patch.object(m.shutil,'disk_usage',return_value=SimpleNamespace(free=6*(1<<30))): m.require_disk(Path('/tmp'))
        with patch.object(m.shutil,'disk_usage',return_value=SimpleNamespace(free=6*(1<<30)-1)):
            with self.assertRaises(m.shared.FixtureError): m.require_disk(Path('/tmp'))


if __name__=='__main__': unittest.main()

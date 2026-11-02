import hashlib
import importlib
from pathlib import Path
import unittest

FIXTURE=importlib.import_module('business.scripts.online-envi-fixture')
OWNER=importlib.import_module('scripts.test.manager-internal-artifact-lineage-online')
class ENVIHostedCaseTest(unittest.TestCase):
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
        for check in ('validate_model_bundle','current_source_deny_verified','generate_raster_cog','deleted-cog','grant.cleanup()','manager-envi.json','composition_change_reapproval_verified','changed_header_cache_rejected'):
            self.assertIn(check,runner)
        browser=(root/'console/frontend/e2e/online/manager-img.spec.js').read_text()
        self.assertIn("envi: ['minio-bsq','minio-bil','minio-bip','nfs-bsq','nfs-bil','nfs-bip']",browser)
        self.assertIn('source.download_members',browser)
        self.assertIn('manager-envi-online_test.py',(root/'Makefile').read_text())

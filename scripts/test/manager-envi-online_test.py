import hashlib
import importlib
from pathlib import Path
import unittest
from types import SimpleNamespace

FIXTURE=importlib.import_module('business.scripts.online-envi-fixture')
OWNER=importlib.import_module('scripts.test.manager-internal-artifact-lineage-online')
CASE=importlib.import_module('scripts.test.manager-envi-online')
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

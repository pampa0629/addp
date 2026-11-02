"""Deterministic MinIO metrics-only account ownership and failure transitions."""
import base64
import copy
import hashlib
import hmac
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import Mock, patch

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('minio_observation', ROOT/'scripts/infra/minio-observation.py')
M = importlib.util.module_from_spec(spec); spec.loader.exec_module(M)


class MinIOObservationTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(); self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name).resolve()/'account'
        self.account = None
        self.db = Mock()
        self.db.key = {'volume': 'owned'}
        self.db.parent = 'deployment-root'
        self.db.info.side_effect = lambda key: copy.deepcopy(self.account)
        self.db.create.side_effect = self.create
        self.db.enable.side_effect = lambda state, on: self.account.update(accountStatus='on' if on else 'off')
        self.db.probe.side_effect = lambda token: 200 if self.account['accountStatus'] == 'on' else 403
        self.manager = M.Lifecycle('infra', self.directory, lambda: self.db)

    def create(self, state):
        self.account = {'accessKey': state['access_key'], 'parentUser': self.db.parent, 'description': state['owner'],
                        'name': 'addp-infra-observer', 'policy': copy.deepcopy(M.NATIVE_POLICY), 'accountStatus': 'on'}
        return {'accessKey': state['access_key'], 'secretKey': 'private-observer-password'}

    def test_native_jwt_has_only_short_prometheus_claims_and_correct_signature(self):
        token = M.jwt('observer', 'private', 1000)
        head, payload, signature = token.split('.')
        decode = lambda part: base64.urlsafe_b64decode(part+'='*((-len(part)) % 4))
        self.assertEqual(json.loads(decode(payload)), {'iss': 'prometheus', 'sub': 'observer', 'exp': 1300})
        self.assertEqual(json.loads(decode(head))['alg'], 'HS512')
        self.assertTrue(hmac.compare_digest(decode(signature), hmac.new(b'private', (head+'.'+payload).encode(), hashlib.sha512).digest()))
        self.assertEqual(json.loads(decode(M.jwt('observer', 'private', 1000, expired=True).split('.')[1]))['exp'], 940)

    def test_installation_persists_one_identity_and_disable_preserves_its_credentials(self):
        self.manager.run(True); first = self.manager.read()
        self.assertEqual(first['phase'], 'enabled')
        self.manager.run(False); disabled = self.manager.read()
        self.assertEqual(disabled['phase'], 'disabled')
        self.assertEqual(self.account['accountStatus'], 'off')
        self.manager.run(True); again = self.manager.read()
        for key in ('access_key', 'secret_key', 'owner', 'source_key'):
            self.assertEqual(first[key], again[key])
        self.assertEqual(first['secret_key'], disabled['secret_key'])
        self.assertEqual((self.directory/'owner.json').stat().st_mode & 0o777, 0o600)
        self.db.create.assert_called_once()

    def test_policy_parent_or_description_drift_is_not_repaired_or_disabled(self):
        self.manager.run(True); before = copy.deepcopy(self.account)
        for field, value in [('policy', {'Statement': [{'Action': ['s3:*']}]}), ('parentUser', 'foreign'), ('description', 'foreign')]:
            self.account = copy.deepcopy(before); self.account[field] = value
            self.db.enable.reset_mock()
            with self.assertRaises(M.Error): self.manager.run(False)
            self.db.enable.assert_not_called()

    def test_another_volume_or_missing_credentials_are_not_adopted(self):
        self.manager.run(True)
        self.db.key = {'volume': 'foreign'}
        self.db.enable.reset_mock()
        with self.assertRaises(M.Error): self.manager.run(True)
        self.db.enable.assert_not_called()
        self.db.key = {'volume': 'owned'}
        state = self.manager.read(); state['secret_key'] = ''; state['phase'] = 'prepared'; self.manager.write(state)
        with self.assertRaises(M.Error): self.manager.run(True)
        self.assertEqual(self.account['accountStatus'], 'off')
        self.db.create.assert_called_once()

    def test_failed_readiness_disables_the_owned_key(self):
        self.db.probe.side_effect = lambda token: 503 if self.account['accountStatus'] == 'on' else 403
        with self.assertRaises(M.Error): self.manager.run(True)
        self.assertEqual(self.account['accountStatus'], 'off')
        self.assertEqual(self.manager.read()['phase'], 'disabled')

    def test_previously_ready_account_is_disabled_on_probe_transport_failure(self):
        self.manager.run(True)
        def probe(token):
            if self.account['accountStatus'] == 'on': raise M.Error('closed transport failure')
            return 403
        self.db.probe.side_effect = probe
        with self.assertRaises(M.Error): self.manager.run(True)
        self.assertEqual(self.account['accountStatus'], 'off')
        self.assertEqual(self.manager.read()['phase'], 'disabled')

    def test_unknown_creation_result_disables_verified_key_without_recreating(self):
        def create(state):
            self.create(state); raise M.Error('closed transport failure')
        self.db.create.side_effect = create
        with self.assertRaises(M.Error): self.manager.run(True)
        self.assertEqual(self.account['accountStatus'], 'off')
        with self.assertRaises(M.Error): self.manager.run(True)
        self.db.create.assert_called_once()

    def test_private_state_permission_and_links_reject_before_management(self):
        self.manager.run(True)
        path = self.directory/'owner.json'; path.chmod(0o644)
        self.db.enable.reset_mock()
        with self.assertRaises(ValueError): self.manager.run(False)
        self.db.enable.assert_not_called()
        path.chmod(0o600); target = self.directory/'real'; path.rename(target); path.symlink_to(target)
        with self.assertRaises(OSError): self.manager.run(False)

    def test_uninstalled_disabled_path_does_not_contact_docker(self):
        env = {'ADDP_MINIO_METRICS_STATE_DIR': str(self.directory)}
        with patch.object(M.deployment, 'Docker', side_effect=AssertionError('no Docker')):
            M.run('up', 'infra', env)
            M.run('down', 'business', env)

    def test_private_state_directory_and_selection_are_closed(self):
        for value in ('relative', str(ROOT/'unsafe')):
            with self.assertRaises(M.Error): M.state_directory({'ADDP_MINIO_METRICS_STATE_DIR': value}, 'infra')
        with self.assertRaises(M.Error): M.run('up', 'infra', {'ADDP_MINIO_METRICS_ENABLED': 'yes'})

    def test_missing_account_is_not_inferred_from_shared_error_code(self):
        missing = {'Code': 'XMinioInvalidIAMCredentials', 'Message': 'The specified service account is not found (Specified service account does not exist)'}
        self.assertTrue(M.Database._missing({'error': {'cause': {'error': missing}}}))
        self.assertFalse(M.Database._missing(dict(missing, Message='other authentication failure')))
        self.assertFalse(M.Database._missing({'Code': 'AccessDenied'}))
        self.assertTrue(M.Database._denied({'error': {'Code': 'AccessDenied'}}))
        self.assertFalse(M.Database._denied({'Code': 'InternalError'}))

    def test_native_enabled_account_is_not_toggled_on_idempotent_up(self):
        self.manager.run(True)
        self.db.enable.reset_mock()
        self.manager.run(True)
        self.db.enable.assert_not_called()

    def test_other_scope_record_and_concurrent_owner_lock_are_rejected(self):
        self.manager.run(True)
        with self.assertRaises(M.Error):
            M.Lifecycle('business', self.directory, lambda: self.db).run(True)
        with M.private.locked(self.directory):
            with self.assertRaises(ValueError): self.manager.run(False)

    def test_status_update_uses_private_stdin_and_no_unrestricted_cli(self):
        db = object.__new__(M.Database)
        db.identity = 'owned-container'; db.same = Mock()
        db.config = {'aliases': {'owned': {'accessKey': 'root-user', 'secretKey': 'root-secret'}}}
        db.docker = Mock(); db.docker.run.return_value = '{"status":"success"}'
        db.enable({'access_key': 'ADDP'+'A'*16}, True)
        call = db.docker.run.call_args
        self.assertEqual(call.args[0], ['exec', '-i', 'owned-container', 'minio-observation-status'])
        self.assertNotIn('root-secret', repr(call.args))
        self.assertEqual(json.loads(call.kwargs['input']), {'root_access': 'root-user', 'root_secret': 'root-secret', 'access_key': 'ADDP'+'A'*16, 'enabled': True})


if __name__ == '__main__':
    unittest.main()

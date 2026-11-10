"""Deterministic MinIO metrics-only account ownership and failure transitions."""
import base64
import copy
import hashlib
import hmac
import importlib.util
import json
import os
from pathlib import Path
from subprocess import CompletedProcess
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
        self.manager = M.AccountLifecycle('infra', self.directory, lambda: self.db)

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

    def test_account_creation_failure_keeps_the_original_exception_and_stage(self):
        failure=M.Error('account operation failed')
        self.db.create.side_effect=failure
        with self.assertRaises(M.Error) as result:self.manager.run(True)
        self.assertIs(result.exception,failure)
        self.assertEqual(result.exception.observation_stage,'account-update')
        self.db.enable.assert_not_called()

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

    def test_invalid_enable_flag_never_blocks_explicit_owned_shutdown(self):
        directory=self.directory/'infra'
        M.private.directory(directory,0o700)
        M.private.atomic(directory/'owner.json','{}',0o600)
        env={'ADDP_MINIO_METRICS_STATE_DIR':str(self.directory),'ADDP_MINIO_METRICS_ENABLED':'invalid'}
        with patch.object(M.deployment,'Docker'),patch.object(M,'Lifecycle') as lifecycle:
            M.run('down','infra',env)
            lifecycle.return_value.run.assert_called_once_with(False,env)
            lifecycle.reset_mock()
            with self.assertRaises(M.Error): M.run('recover','infra',env)
            lifecycle.assert_not_called()

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
            M.AccountLifecycle('business', self.directory, lambda: self.db).run(True)
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

    def source_manager(self):
        self.db.identity = 'a'*64
        self.db.address = '10.0.0.2:9000'
        self.db.facts = {'started_at': '2026-01-01T00:00:00Z'}
        manager = M.Lifecycle(Mock(), 'infra', self.directory, lambda: self.db, 'owned-source')
        manager.source = Mock()
        return manager

    def test_source_creation_failure_removes_source_and_disables_verified_account(self):
        manager = self.source_manager()
        manager.source.start.side_effect = M.Error('fixed startup failure')
        with patch.object(M.sources, 'transport', return_value=('127.0.0.1', 9443, {})), patch.object(M, 'bind_source'):
            with self.assertRaisesRegex(M.Error, 'fixed startup failure') as failure: manager.run(True, {})
        self.assertFalse(getattr(failure.exception, 'minio_cleanup_failed', False))
        self.assertEqual(self.account['accountStatus'], 'off')
        self.assertEqual(manager.account.read()['phase'], 'disabled')
        self.assertEqual(manager.source.stop.call_count, 2)
        config = manager.source.start.call_args.args[2]
        self.assertEqual(set(config), {'address', 'scope', 'access_key', 'secret_key', 'container_id', 'container_started_at'})
        self.assertNotIn(self.db.parent, json.dumps(config))

    def test_source_preflight_failure_disables_old_account_without_rebuilding(self):
        self.manager.run(True)
        manager = self.source_manager()
        with patch.object(M.sources, 'transport', side_effect=M.Error('TLS rejected')):
            with self.assertRaisesRegex(M.Error, 'TLS rejected'): manager.run(True, {})
        manager.source.build.assert_not_called(); manager.source.start.assert_not_called()
        self.assertEqual(self.account['accountStatus'], 'off')

    def test_source_cleanup_failure_is_explicit_and_still_attempts_native_disable(self):
        self.manager.run(True)
        manager = self.source_manager()
        manager.source.stop.side_effect = [None, M.Error('source cleanup failed')]
        with patch.object(M.sources, 'transport', side_effect=M.Error('TLS rejected')):
            with self.assertRaisesRegex(M.Error, 'TLS rejected') as failure: manager.run(True, {})
        self.assertTrue(failure.exception.minio_cleanup_failed)
        self.assertEqual(self.account['accountStatus'], 'off')

    def test_foreign_source_is_not_removed_or_followed_by_account_mutation(self):
        self.manager.run(True)
        manager = self.source_manager(); self.db.enable.reset_mock()
        manager.source.stop.side_effect = M.Error('foreign source')
        with self.assertRaisesRegex(M.Error, 'foreign source'): manager.run(False, {})
        self.db.enable.assert_not_called()

    def test_uninstalled_account_disable_does_not_open_management_connection(self):
        manager = M.AccountLifecycle('infra', self.directory, Mock(side_effect=AssertionError('no management')))
        manager.run(False)

    def test_recovery_is_readonly_for_account_and_private_state_even_when_source_fails(self):
        self.manager.run(True)
        manager=self.source_manager(); before=(self.directory/'owner.json').read_bytes()
        self.db.enable.reset_mock();self.db.create.reset_mock()
        manager.source.recover.side_effect=lambda *args:(args[5](),'healthy; source unchanged')[1]
        with patch.object(M,'bind_source'),patch.object(M.sources,'transport',return_value=('127.0.0.1',9443,{})):
            self.assertIn('unchanged',manager.recover({}))
            manager.source.recover.side_effect=M.Error('source start failed')
            with self.assertRaises(M.Error): manager.recover({})
        self.assertEqual((self.directory/'owner.json').read_bytes(),before)
        self.db.enable.assert_not_called();self.db.create.assert_not_called()
        manager.source.stop.assert_not_called();manager.source.build.assert_not_called()

    def test_recovery_refuses_disabled_or_drifted_account_and_missing_installation(self):
        self.manager.run(True)
        manager=self.source_manager()
        baseline=copy.deepcopy(self.account)
        for problem in ('disabled','policy','source'):
            with self.subTest(problem=problem),patch.object(M,'bind_source'):
                self.account=copy.deepcopy(baseline)
                if problem=='disabled': self.account['accountStatus']='off'
                if problem=='policy': self.account['policy']={}
                state=manager.account.read()
                if problem=='source': state['source_key']={};manager.account.write(state)
                self.db.enable.reset_mock()
                with self.assertRaises(M.Error): manager.recover({})
                self.db.enable.assert_not_called();manager.source.recover.assert_not_called()
        with patch.object(M.deployment,'Docker') as docker:
            for enabled in ('false','true'):
                M.run('recover','infra',{'ADDP_MINIO_METRICS_ENABLED':enabled,'ADDP_MINIO_METRICS_STATE_DIR':str(self.directory/'absent')})
            docker.assert_not_called()

    def network_database(self, *, project='owned', driver='bridge', address='10.1.2.3', duplicate=False):
        db = Mock()
        db.identity = 'a'*64
        db.facts = {'labels': {'com.docker.compose.project': 'owned'}, 'networks': {
            'owned_main': {'NetworkID': 'main', 'IPAddress': address, 'GlobalIPv6Address': ''},
            'owned_trace': {'NetworkID': 'trace', 'IPAddress': '10.2.3.4', 'GlobalIPv6Address': ''}}}
        def command(args, **kwargs):
            if args[:2] == ['image', 'inspect']:
                return CompletedProcess(args, 0, 'sha256:'+'b'*64, '')
            if args[0] == 'network':
                main = args[-1] == 'main'
                return json.dumps([{'Name': 'owned_main' if main else 'owned_trace', 'Driver': driver,
                                    'Labels': {'com.docker.compose.project': project,
                                               'com.docker.compose.network': 'addp-network' if main or duplicate else 'trace-backend'}}])
            return 'sha256:'+'b'*64
        db.docker.run.side_effect = command
        return db

    def test_source_uses_only_the_exact_standard_network_on_a_multi_network_database(self):
        db = self.network_database()
        M.bind_source(db, 'infra')
        self.assertEqual((db.network, db.address, db.image), ('owned_main', '10.1.2.3:9000', 'sha256:'+'b'*64))
        db.same.assert_called_once()
        db.docker.run.assert_called_with(['image', 'inspect', '--format', '{{.Id}}', db.image], check=False)

    def test_missing_minio_image_is_reported_before_initializing_the_private_volume(self):
        db = self.network_database()
        read = db.docker.run.side_effect
        def image_missing(args, **kwargs):
            if args[:2] == ['image', 'inspect']:
                return CompletedProcess(args, 1, '', 'private Docker diagnostic')
            return read(args, **kwargs)
        db.docker.run.side_effect = image_missing
        with self.assertRaises(M.Error) as caught:
            M.bind_source(db, 'infra')
        self.assertEqual(caught.exception.observation_stage, 'source-bootstrap-image')
        self.assertNotIn('private Docker diagnostic', str(caught.exception))
        db.same.assert_not_called()

    def test_source_network_drift_ambiguity_or_public_address_is_rejected(self):
        for values in ({'project': 'foreign'}, {'driver': 'host'}, {'address': '8.8.8.8'}, {'duplicate': True}):
            with self.subTest(values=values):
                with self.assertRaises(M.Error): M.bind_source(self.network_database(**values), 'infra')


if __name__ == '__main__':
    unittest.main()

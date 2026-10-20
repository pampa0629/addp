import importlib
import copy
import io
import json
import os
import re
import tempfile
import unittest
import sys
from pathlib import Path
from contextlib import redirect_stderr
from unittest.mock import patch
from urllib.error import HTTPError

SCENE = importlib.import_module('scripts.test.platform-administrator-governance-online')
API = SCENE.API


def environment():
    env = {'GITHUB_ACTIONS': 'true', 'RUNNER_OS': 'Linux', 'ADDP_ONLINE_HOST': '1',
           'ADDP_ONLINE_HOSTED': '1', 'ADDP_ONLINE_TEST': '1', 'ONLINE_SUITE': SCENE.SUITE,
           'POSTGRES_DB': 'addp_online', 'ADDP_ONLINE_TEST_RUN_ID': 'governance-test',
           'ADDP_ONLINE_TEST_TENANT_ID': '2',
           'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN': 'tenant-token', 'ADDP_ONLINE_SECRET_DIR': '/tmp/private',
           'GATEWAY_URL': 'http://127.0.0.1:8000', 'SYSTEM_URL': 'http://127.0.0.1:8180',
           'CONSOLE_URL': 'http://127.0.0.1:5170'}
    for n, role in enumerate((*SCENE.ROLES, 'successor'), 1):
        for key, value in {'id': str(n), 'username': role, 'password': 'fixture-password', 'totp': 'JBSWY3DPEHPK3PXP'}.items():
            env[f'ADDP_ONLINE_GOV_{role.upper()}_{key.upper()}'] = value
    return env


def identity(role):
    actions = ['read'] + ([] if role == 'audit' else ['create']) + ([] if role == 'security' else ['approve', 'reject'])
    return {'principal': {'id': '7', 'type': 'user'}, 'context': {'type': 'platform'},
            'authentication': {'assurance_level': 'aal2'}, 'token': {'type': 'first_party_access_token'},
            'authorization': {'role_assignments': [{'role_key': f'platform.{role}_administrator',
             'scope': {'type': 'platform'}, 'permissions': ['iam.platform_administrator_change.'+a for a in actions]}]}}


class AdministratorGovernanceOnlineTest(unittest.TestCase):
    def test_offline_recovery_rejects_development_before_starting_a_terminal(self):
        scene = SCENE.Scenario({**environment(), 'GITHUB_ACTIONS': 'false'})
        with patch.object(SCENE, 'RecoveryTerminal') as terminal, patch.object(API.GatewayClient, 'request') as request:
            with self.assertRaises(API.SuiteError): scene.offline_recovery()
            terminal.assert_not_called()
            request.assert_not_called()

    def recovery_holders(self):
        credentials = SCENE.Scenario(environment()).credentials
        return {item['id']: {**item, 'role_key': f'platform.{"system" if actor == "successor" else actor}_administrator'}
                for actor, item in credentials.items()}

    def collect_dialog(self, holders, order=('audit', 'security', 'system', 'successor'), mutate=None, interrupt_before_last=False):
        by_username = {item['username']: item for item in holders.values()}
        outputs = []
        secrets = ('GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ', 'JBSWY3DPEHPK3PXP',
                   'MFRGGZDFMZTWQ2LK', 'ONSWG4TFOQYTEMZU')
        for username, secret in zip(order, secrets):
            item = by_username[username]
            outputs += [f'恢复 fixture（{item["role_key"]}，账号 {username}）',
                        '输入新密码（至少 14 字符）: ', '再次输入新密码: ',
                        '备用 TOTP Secret: '+secret+'\n', '输入当前 TOTP 验证码: ',
                        '等待验证码变化后输入下一个 TOTP 验证码: ']
        outputs += ['IAM 管理员凭据恢复已完成于 now（Attempt 7）']
        outputs += [f'{holders[pid]["role_key"]} -> Principal {pid}\n' for pid in sorted(holders, key=int)]
        if mutate: mutate(outputs)
        class Dialog:
            def __init__(self): self.values, self.writes, self.exits = outputs, [], []
            def expect(self, pattern):
                match = re.search(pattern, self.values.pop(0))
                SCENE.require(match is not None, 'simulated recovery dialog differs')
                return match
            def send_line(self, value, hidden=False): self.writes.append((value, hidden))
            def finish(self, code): self.exits.append(code)
            def interrupt(self): self.exits.append(-SCENE.signal.SIGTERM)
        terminal = Dialog()
        now = [1800000001.0]
        def advance(seconds): now[0] += seconds
        with patch.object(SCENE.time, 'time', side_effect=lambda: now[0]), patch.object(SCENE.time, 'sleep', side_effect=advance):
            result = SCENE.collect_recovery_credentials(terminal, holders, **({'interrupt_before_last': True} if interrupt_before_last else {}))
        return terminal, result, secrets

    def test_offline_dialog_binds_each_principal_including_same_role_and_consecutive_counters(self):
        holders = self.recovery_holders()
        terminal, (recovered, counters, attempt), secrets = self.collect_dialog(holders)
        self.assertEqual(set(recovered), set(holders))
        self.assertEqual(attempt, '7')
        self.assertEqual(terminal.exits, [0])
        self.assertFalse(terminal.values)
        for index, username in enumerate(('audit', 'security', 'system', 'successor')):
            item = next(item for item in holders.values() if item['username'] == username)
            password, confirmation, first, second = terminal.writes[index*4:index*4+4]
            self.assertEqual(password, confirmation)
            self.assertTrue(password[1])
            self.assertEqual(recovered[item['id']]['username'], username)
            self.assertEqual(first, (SCENE.totp(secrets[index], 1800000001+index*30), False))
            self.assertEqual(second, (SCENE.totp(secrets[index], 1800000031+index*30), False))
            self.assertEqual(counters[secrets[index]], 1800000000//30+index+1)
        self.assertEqual(len({item['password'] for item in recovered.values()}), 4)
        self.assertEqual(recovered['1']['role_key'], recovered['4']['role_key'])
        self.assertNotEqual(recovered['1']['totp'], recovered['4']['totp'])

    def test_interrupted_dialog_stops_after_three_validated_holders_before_fourth_password(self):
        terminal, (partial, counters, attempt), _ = self.collect_dialog(self.recovery_holders(), interrupt_before_last=True)
        self.assertEqual(set(partial), {'1', '2', '3'})
        self.assertEqual(len(counters), 3)
        self.assertIsNone(attempt)
        self.assertEqual(terminal.exits, [-SCENE.signal.SIGTERM])
        self.assertEqual(len(terminal.writes), 12)
        self.assertTrue(terminal.values[0].startswith('再次输入新密码'))
        self.assertTrue(any(item['role_key'] == 'platform.system_administrator' for item in partial.values()))

    def test_interrupted_recovery_requires_prepared_only_audit_and_no_credentials(self):
        valid = {'total': 1, 'data': [{'event_name': 'iam.recovery.prepared', 'entity_id': '7',
                  'result': 'succeeded', 'details': {'administrator_count': 4}}]}
        self.assertEqual(SCENE.assert_recovery_prepared_audit(valid, 4, ['private-secret']), '7')
        cases = []
        value = copy.deepcopy(valid); value['data'][0]['event_name'] = 'iam.recovery.completed'; cases.append(value)
        value = copy.deepcopy(valid); value['data'][0]['details']['administrator_count'] = 3; cases.append(value)
        value = copy.deepcopy(valid); value['data'][0]['details']['secret'] = 'private-secret'; cases.append(value)
        value = copy.deepcopy(valid); value['data'][0]['entity_id'] = None; cases.append(value)
        value = copy.deepcopy(valid); value['total'] = 2; cases.append(value)
        value = copy.deepcopy(valid); value['data'].append(copy.deepcopy(value['data'][0])); cases.append(value)
        for value in cases:
            with self.assertRaises(API.SuiteError): SCENE.assert_recovery_prepared_audit(value, 4, ['private-secret'])

    def test_offline_dialog_rejects_duplicate_unexpected_wrong_role_and_missing_principal(self):
        def replace(index, value): return lambda outputs: outputs.__setitem__(index, value)
        mutations = [replace(0, '恢复 fixture（platform.audit_administrator，账号 different）'),
                     replace(0, '恢复 fixture（platform.system_administrator，账号 audit）'),
                     replace(6, '恢复 fixture（platform.audit_administrator，账号 audit）'),
                     replace(18, 'IAM 管理员凭据恢复已完成于 now（Attempt 7）'),
                     replace(-1, 'platform.system_administrator -> Principal 999\n')]
        for mutate in mutations:
            with self.subTest(mutation=mutate), self.assertRaises(API.SuiteError):
                self.collect_dialog(self.recovery_holders(), mutate=mutate)
        holders = self.recovery_holders(); holders['4']['username'] = holders['1']['username']
        with self.assertRaises(API.SuiteError): SCENE.collect_recovery_credentials(None, holders)

    def test_offline_audit_requires_all_principals_and_rejects_missing_or_sensitive_evidence(self):
        holders = self.recovery_holders()
        rows = [{'event_name': name, 'entity_id': '7', 'result': 'succeeded', 'details': {}}
                for name in ('iam.recovery.prepared', 'iam.recovery.completed')]
        rows += [{'event_name': 'iam.recovery.administrator_credentials_replaced', 'entity_id': '7',
                  'result': 'succeeded', 'principal_id': item['id'],
                  'details': {'role_key': item['role_key']}} for item in holders.values()]
        valid = {'data': rows, 'total': 6}
        sensitive = ['ephemeral-proof-not-for-log']
        SCENE.assert_recovery_audit(valid, '7', holders, sensitive)
        cases = []
        value = copy.deepcopy(valid); value['data'].pop(); cases.append(value)
        value = copy.deepcopy(valid); value['data'][2]['principal_id'] = '999'; cases.append(value)
        value = copy.deepcopy(valid); value['data'][2]['entity_id'] = '8'; cases.append(value)
        value = copy.deepcopy(valid); value['data'][2]['details']['role_key'] = 'platform.audit_administrator'; cases.append(value)
        value = copy.deepcopy(valid); value['data'][-1] = copy.deepcopy(value['data'][2]); cases.append(value)
        value = copy.deepcopy(valid); value['data'][2]['details']['leak'] = sensitive[0]; cases.append(value)
        value = copy.deepcopy(valid); value['data'].append(value['data'][0]); cases.append(value)
        for value in cases:
            with self.assertRaises(API.SuiteError): SCENE.assert_recovery_audit(value, '7', holders, sensitive)

    def test_offline_flow_checks_four_holders_then_revokes_only_added_assignment(self):
        scene = SCENE.Scenario(environment())
        holders = self.recovery_holders()
        recovered = {pid: {**item, 'password': 'new-'+pid, 'totp': 'MFRGGZDFMZTWQ2LK'}
                     for pid, item in holders.items()}
        checks, governance = [], []
        state = {'recovered': False}
        def context(actor):
            result = identity('system' if actor == 'successor' else actor)
            result['principal']['id'] = scene.credentials[actor]['id']
            result['authorization']['authorization_version'] = str(10 + ((3 if actor == 'audit' else 1) if state['recovered'] else 0))
            return result
        class Client:
            def __init__(self, actor, old=False): self.actor, self.old = actor, old
            def request(self, method, path, expected, body=None):
                checks.append((self.actor, method, path, expected, body))
                if path == SCENE.CONTEXT:
                    return API.Response(expected[0], context(self.actor))
                if 'audit/events' in path:
                    if not state['recovered']:
                        return API.Response(200, {'total': 1, 'data': [{'event_name': 'iam.recovery.prepared',
                            'entity_id': '7', 'result': 'succeeded', 'details': {'administrator_count': 4}}]})
                    rows = [{'event_name': 'iam.recovery.'+name, 'entity_id': '7', 'result': 'succeeded', 'details': {}}
                            for name in ('prepared', 'completed')]
                    rows += [{'event_name': 'iam.recovery.administrator_credentials_replaced', 'entity_id': '7',
                              'result': 'succeeded', 'principal_id': pid, 'details': {'role_key': item['role_key']}}
                             for pid, item in holders.items()]
                    return API.Response(200, {'data': rows, 'total': 6})
        class Public:
            def request(self, method, path, expected, body=None):
                checks.append(('public', method, path, expected, body))
                return API.Response(expected[0], {'next_action': 'verify_mfa', 'mfa': {'challenge_token': 'challenge'}})
        class Terminal:
            def __enter__(self): return self
            def __exit__(self, *_): pass
            def expect(self, pattern): return ('match', 'addp_ir_ephemeral')
            def send_line(self, *_, **__): pass
            def finish(self, *_): pass
        scene.public = Public()
        scene.actors = {actor: Client(actor, old=True) for actor in (*SCENE.ROLES, 'successor')}
        def login(actor): scene.actors[actor] = Client(actor)
        def create(author, kind, target, **extra):
            governance.append((author, kind, target, extra))
            return {'kind': kind}
        def collect(_, expected, *, interrupt_before_last=False):
            self.assertEqual(expected, holders)
            checks.append(('collector', interrupt_before_last))
            if interrupt_before_last:
                return {pid: item for pid, item in recovered.items() if pid != '4'}, {}, None
            state['recovered'] = True
            return recovered, {}, '7'
        tenant = {'principal': {'id': '4', 'type': 'user'},
                  'context': {'type': 'tenant', 'tenant_id': '2', 'tenant_membership_id': '42'},
                  'authentication': {'assurance_level': 'aal2'}, 'token': {'type': 'first_party_access_token'},
                  'authorization': {'role_assignments': []}}
        class Tenant:
            def request(self, *_): return API.Response(200, tenant)
        with patch.object(SCENE, 'validate_hosted'), patch.object(SCENE.Path, 'is_file', return_value=True), \
             patch.object(SCENE.os, 'access', return_value=True), patch.object(SCENE, 'RecoveryTerminal', return_value=Terminal()), \
             patch.object(SCENE.subprocess, 'run', return_value=type('Result', (), {'returncode': 1, 'stderr': '必须直接运行在交互终端'})()), \
             patch.object(scene, 'create', side_effect=create), patch.object(scene, 'review', return_value={'status': 'applied'}), \
             patch.object(scene, 'login', side_effect=login), patch.object(scene, 'status', return_value='suspended'), \
             patch.object(SCENE, 'collect_recovery_credentials', side_effect=collect), \
             patch.object(scene, 'authenticate', return_value={'next_action': 'session_issued', 'session': {'access_token': 'addp_at_candidate'}}), \
             patch.object(API, 'GatewayClient', return_value=Tenant()):
            result = scene.offline_recovery()
        self.assertEqual(result, {key: True for key in SCENE.OFFLINE_PROOFS})
        self.assertEqual([row[1] for row in governance], ['platform_role_grant', 'platform_identity_suspend',
                                                       'platform_identity_reactivate', 'platform_role_revoke'])
        self.assertEqual(governance[0], ('security', 'platform_role_grant', 'successor', {'target_role_key': 'platform.system_administrator'}))
        self.assertEqual(governance[-1], ('security', 'platform_role_revoke', 'successor', {'target_role_key': 'platform.system_administrator'}))
        requests = [row for row in checks if row[0] != 'collector']
        for actor in (*SCENE.ROLES, 'successor'):
            self.assertTrue(any(row[:4] == (actor, 'GET', SCENE.CONTEXT, (401,)) for row in requests))
            self.assertTrue(any(row[:4] == (actor, 'GET', SCENE.CONTEXT, (200,)) for row in requests))
            self.assertTrue(any(row[2] == '/api/v1/system/login' and row[3] == (401,)
                                and row[4]['username'] == actor and row[4]['password'] == 'fixture-password' for row in requests))
            self.assertEqual(scene.credentials[actor], recovered[holders[scene.credentials[actor]['id']]['id']])
        self.assertEqual([row[1] for row in checks if row[0] == 'collector'], [True, False])
        requests = [row for row in checks if row[0] != 'collector']
        self.assertEqual(sum(row[2] == '/api/v1/system/auth/mfa-verifications' and row[3] == (401,) for row in requests), 7)

    def test_offline_credentials_never_reenter_browser_child_environment(self):
        scene = SCENE.Scenario(environment())
        class Client:
            def request(self, *_): return API.Response(200, {'context': {'type': 'tenant'}, 'total': 0})
        scene.actors['audit'] = Client()
        def browser():
            self.assertFalse(any(item['password'].startswith('CLI-only-') for item in scene.credentials.values()))
            return {key: True for key in SCENE.BROWSER_PROOFS}
        def offline():
            for role in SCENE.ROLES: scene.credentials[role]['password'] = 'CLI-only-password'
            return {key: True for key in SCENE.OFFLINE_PROOFS}
        with patch.object(API, 'GatewayClient', return_value=Client()), patch.object(scene, 'login'), \
             patch.object(scene, 'assert_legacy_route_removed'), \
             patch.object(scene, 'pause_restore'), patch.object(scene, 'recovery'), patch.object(scene, 'handover'), \
             patch.object(scene, 'browser', side_effect=browser), patch.object(scene, 'offline_recovery', side_effect=offline):
            result = scene.run()
        self.assertEqual(set(result['offline_recovery']), SCENE.OFFLINE_PROOFS)
        self.assertNotIn('CLI-only-password', json.dumps(result))

    def test_recovery_terminal_handles_fragmented_utf8_and_hidden_input_in_memory(self):
        child = '''import os,sys,termios,time
attributes=termios.tcgetattr(0); attributes[3] &= ~termios.ECHO; termios.tcsetattr(0,termios.TCSANOW,attributes)
prompt='隐藏凭据: '.encode()
os.write(1,prompt[:2]); time.sleep(.02); os.write(1,prompt[2:])
value=sys.stdin.readline().strip()
print('accepted' if len(value)>5 else 'rejected',flush=True)
'''
        terminal = SCENE.RecoveryTerminal([sys.executable, '-c', child], dict(os.environ))
        with terminal:
            terminal.expect('隐藏凭据: ')
            terminal.send_line('ephemeral-proof', hidden=True)
            terminal.expect('accepted')
            terminal.finish(0)
        self.assertEqual(terminal.pending, '')
        self.assertEqual(terminal.process.returncode, 0)

    def test_recovery_terminal_interrupts_only_its_private_process_and_clears_memory(self):
        child = 'import time; print("ready",flush=True); time.sleep(20)'
        terminal = SCENE.RecoveryTerminal([sys.executable, '-c', child], dict(os.environ))
        with terminal:
            terminal.expect('ready')
            terminal.interrupt()
            self.assertEqual(terminal.process.returncode, -SCENE.signal.SIGTERM)
        self.assertEqual(terminal.pending, '')
        self.assertIsNone(terminal.decoder)

    def test_recovery_terminal_rejects_echo_and_closes_children_on_failure(self):
        child = 'import sys; print("ready",flush=True); sys.stdin.readline()'
        terminal = SCENE.RecoveryTerminal([sys.executable, '-c', child], dict(os.environ))
        with self.assertRaises(API.SuiteError), terminal:
            terminal.expect('ready')
            terminal.send_line('must-not-echo', hidden=True)
        self.assertIsNotNone(terminal.process.returncode)
        self.assertEqual(terminal.pending, '')

    def test_recovery_terminal_does_not_expose_transcript_on_unexpected_exit(self):
        terminal = SCENE.RecoveryTerminal([sys.executable, '-c', 'print("private-transcript"); raise SystemExit(7)'], dict(os.environ))
        with self.assertRaises(API.SuiteError) as failure, terminal:
            terminal.expect('never-present', timeout=2)
        self.assertNotIn('private-transcript', str(failure.exception))
        self.assertEqual(terminal.pending, '')
        self.assertIsNotNone(terminal.process.returncode)

    def test_recovery_cli_does_not_inherit_online_identity_credentials(self):
        env = {**environment(), 'PATH': '/test', 'POSTGRES_HOST': '127.0.0.1', 'IAM_MFA_ENCRYPTION_KEY': 'database-config'}
        result = SCENE.recovery_cli_environment(env)
        self.assertFalse(any(key.startswith('ADDP_ONLINE_') for key in result))
        self.assertEqual(result['POSTGRES_HOST'], '127.0.0.1')
        self.assertEqual(result['IAM_MFA_ENCRYPTION_KEY'], 'database-config')

    def test_removed_route_requires_404_without_assuming_a_json_error_contract(self):
        scene = SCENE.Scenario(environment())
        scene.actors['system'] = API.GatewayClient(environment()['GATEWAY_URL'], 'officer-token', 10)
        for status in (404, 200, 401, 403):
            with self.subTest(status=status):
                response = HTTPError('http://127.0.0.1/removed', status, 'status', {}, io.BytesIO(b'404 page not found'))
                with patch('urllib.request.urlopen', side_effect=response):
                    if status == 404:
                        scene.assert_legacy_route_removed()
                    else:
                        with self.assertRaises(API.SuiteError): scene.assert_legacy_route_removed()

    def test_failure_diagnostics_do_not_copy_http_response_or_credentials(self):
        failure = API.SuiteError('POST /login returned HTTP 401 (raw-secret-never-export)')
        with patch.object(SCENE, 'validate_hosted'), patch.object(SCENE.PREFLIGHT, 'run_preflight'), \
             patch.dict(os.environ, environment(), clear=True), patch.object(SCENE.Scenario, 'run', side_effect=failure):
            output = io.StringIO()
            with redirect_stderr(output): self.assertEqual(SCENE.main(), 1)
            self.assertIn('http-401', output.getvalue())
            self.assertNotIn('raw-secret-never-export', output.getvalue())

    def test_profile_rejects_personal_development_before_any_request(self):
        valid = environment()
        self.addCleanup(patch.stopall)
        patch.object(SCENE.sys, 'platform', 'linux').start()
        patch.object(SCENE.platform, 'machine', return_value='x86_64').start()
        SCENE.validate_hosted(valid)
        for key, value in [('GITHUB_ACTIONS', 'false'), ('RUNNER_OS', 'macOS'), ('ADDP_ONLINE_HOSTED', '0'),
                           ('POSTGRES_DB', 'addp'), ('POSTGRES_DB', 'addp_iam_test'), ('ONLINE_SUITE', 'platform-node-metrics'),
                           ('ADDP_ONLINE_OWNER_MANAGED', '1'), ('GATEWAY_URL', 'http://example.com:8000'),
                           ('CONSOLE_URL', 'http://127.0.0.1:5170/?token=secret')]:
            with self.subTest(key=key, value=value), self.assertRaises((API.SuiteError, SCENE.PREFLIGHT.PreflightError)):
                SCENE.validate_hosted({**valid, key: value})
        with patch.object(SCENE.sys, 'platform', 'darwin'), self.assertRaises(API.SuiteError):
            SCENE.validate_hosted(valid)
        with patch.dict(os.environ, {}, clear=True), patch.object(API.GatewayClient, 'request') as request:
            self.assertEqual(SCENE.main(), 1)
            request.assert_not_called()

    def test_authoritative_officers_reject_wrong_scope_role_assurance_and_daily_audit_write(self):
        for role in SCENE.ROLES:
            SCENE.assert_identity(identity(role), role, '7')
        cases = []
        for key, value in [('principal', {'id': '8', 'type': 'user'}), ('context', {'type': 'tenant', 'tenant_id': '2'}),
                           ('token', {'type': 'service_access_token'}), ('authentication', {'assurance_level': 'aal1'}),
                           ('delegation', {'id': '9'})]:
            cases.append({**identity('audit'), key: value})
        extra = identity('audit');extra['authorization']['role_assignments'][0]['permissions'].append('iam.user.create');cases.append(extra)
        extra = identity('audit');extra['authorization']['role_assignments'].append(identity('system')['authorization']['role_assignments'][0]);cases.append(extra)
        for value in cases:
            with self.assertRaises(API.SuiteError): SCENE.assert_identity(value, 'audit', '7')

    def test_review_uses_observed_version_and_requires_real_reviewer_attribution(self):
        scene = SCENE.Scenario(environment())
        calls = []
        class Client:
            def request(self, method, path, expected, body):
                calls.append((method, path, expected, body))
                return API.Response(200, {'review': {'principal_id': '3'}})
        scene.actors['audit'] = Client()
        scene.review('audit', {'id': '11', 'version': '4'})
        self.assertEqual(calls[0][3], {'version': '4', 'reason': 'governance-test:approve'})
        scene.credentials['audit']['id'] = '7'
        with self.assertRaises(API.SuiteError): scene.review('audit', {'id': '11', 'version': '4'})

    def test_browser_receipt_cannot_hide_failure_or_export_secrets(self):
        with tempfile.TemporaryDirectory() as folder:
            env = {**environment(), 'ADDP_ONLINE_ARTIFACT_DIR': folder}
            scene = SCENE.Scenario(env)
            receipt = Path(folder)/'administrator-browser.json'
            def browser(*args, **kwargs):
                self.assertIn('--output=/tmp/private/governance-browser-output', args[0])
                self.assertTrue(kwargs['capture_output'])
                receipt.write_text(json.dumps({key: True for key in SCENE.BROWSER_PROOFS}))
                return type('Result', (), {'returncode': 0})()
            with patch.object(SCENE.subprocess, 'run', side_effect=browser):
                self.assertEqual(set(scene.browser()), SCENE.BROWSER_PROOFS)
                with self.assertRaises(API.SuiteError): scene.browser()
            receipt.unlink()
            def bad(*args, **kwargs):
                value = {key: True for key in SCENE.BROWSER_PROOFS};value['recovery_token'] = 'never-export'
                receipt.write_text(json.dumps(value));return type('Result', (), {'returncode': 0})()
            with patch.object(SCENE.subprocess, 'run', side_effect=bad), self.assertRaises(API.SuiteError):scene.browser()

    def test_totp_matches_known_rfc_counter(self):
        self.assertEqual(SCENE.totp('GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ', 59), '287082')


if __name__ == '__main__': unittest.main()

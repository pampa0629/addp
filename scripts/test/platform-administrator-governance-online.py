"""Disposable Hosted platform officer governance through real Gateway APIs."""
from __future__ import annotations

import base64
import hashlib
import hmac
import json
import os
import platform
import secrets
import struct
import subprocess
import sys
import time
from importlib import import_module
from pathlib import Path

API = import_module('scripts.utils.online-api')
PREFLIGHT = import_module('scripts.test.online-preflight')
ROOT = Path(__file__).resolve().parents[2]
SUITE = 'platform-administrator-governance'
CHANGES = '/api/v1/system/platform/administrator_changes'
RECOVERY = '/api/v1/system/auth/administrator-recovery'
CONTEXT = '/api/v1/system/auth/context'
ROLES = ('system', 'security', 'audit')
BROWSER_PROOFS = {'real_mfa', 'role_matrix', 'request_created', 'independent_review',
                  'one_time_recovery', 'secret_memory_only', 'old_session_revoked', 'new_login'}


def require(condition, message):
    if not condition:
        raise API.SuiteError(message)


def totp(secret, at):
    key = base64.b32decode(secret + '=' * (-len(secret) % 8))
    digest = hmac.new(key, struct.pack('>Q', int(at)//30), hashlib.sha1).digest()
    offset = digest[-1] & 15
    return f'{(struct.unpack(">I", digest[offset:offset+4])[0] & 0x7fffffff) % 1000000:06d}'


def validate_hosted(environment):
    require(sys.platform == 'linux' and platform.machine() == 'x86_64', 'governance requires actual Hosted Linux x86_64')
    for key, value in {'GITHUB_ACTIONS': 'true', 'RUNNER_OS': 'Linux', 'ADDP_ONLINE_HOST': '1',
                       'ADDP_ONLINE_HOSTED': '1', 'ADDP_ONLINE_TEST': '1',
                       'ONLINE_SUITE': SUITE, 'POSTGRES_DB': 'addp_online'}.items():
        require(environment.get(key) == value, 'governance requires its disposable Hosted profile')
    require(environment.get('ADDP_ONLINE_OWNER_MANAGED', '0') != '1', 'Hosted profiles must be exclusive')
    for key in ('GATEWAY_URL', 'SYSTEM_URL', 'CONSOLE_URL'):
        PREFLIGHT.parse_service(key.lower().replace('_', '-') + '=' + environment.get(key, ''))


def assert_identity(payload, role, principal_id):
    require(payload.get('principal') == {'id': principal_id, 'type': 'user'}, 'officer principal mismatch')
    require(payload.get('context') == {'type': 'platform'} and payload.get('delegation') is None,
            'officer context must be nondelegated Platform')
    require(payload.get('token', {}).get('type') == 'first_party_access_token'
            and payload.get('authentication', {}).get('assurance_level') in {'aal2', 'aal3'},
            'officer requires real first-party MFA')
    assignments = payload.get('authorization', {}).get('role_assignments', [])
    require(len(assignments) == 1 and assignments[0].get('role_key') == f'platform.{role}_administrator'
            and assignments[0].get('scope') == {'type': 'platform'}, 'officer roles must remain separate')
    permissions = set(assignments[0].get('permissions', []))
    expected = {'read'} | ({'create'} if role != 'audit' else set()) | ({'approve', 'reject'} if role != 'security' else set())
    actual = {key.removeprefix('iam.platform_administrator_change.') for key in permissions if key.startswith('iam.platform_administrator_change.')}
    require(actual == expected, 'governance permission matrix differs')
    if role == 'audit':
        require(not permissions.intersection({'iam.user.create', 'iam.user.suspend', 'iam.local_account.reset',
                                             'iam.mfa_credential.reset', 'iam.security_policy.update'}),
                'audit reviewer gained daily management writes')


class Scenario:
    def __init__(self, environment):
        self.env = dict(environment)
        self.public = API.GatewayClient(environment['GATEWAY_URL'], '', 10)
        self.actors = {}
        self.credentials = {role: {key: environment[f'ADDP_ONLINE_GOV_{role.upper()}_{key.upper()}']
                                   for key in ('id', 'username', 'password', 'totp')}
                            for role in (*ROLES, 'successor')}
        self.used_steps = {}
        self.run_id = environment['ADDP_ONLINE_TEST_RUN_ID']
        self.stage = 'authentication'

    def login(self, role):
        credentials = self.credentials[role]
        result = self.public.request('POST', '/api/v1/system/login', (200,),
                                     {key: credentials[key] for key in ('username', 'password')}).payload
        require(result.get('next_action') == 'verify_mfa' and not result.get('session'), 'login skipped real MFA')
        now = time.time()
        previous = self.used_steps.get(credentials['totp'], -1)
        if int(now)//30 <= previous:
            # Wait for a fresh real counter, never retry a rejected MFA operation.
            time.sleep(max(0, (previous+1)*30 - now) + .15)
        now = time.time()
        self.used_steps[credentials['totp']] = int(now)//30
        result = self.public.request('POST', '/api/v1/system/auth/mfa-verifications', (200,),
                    {'challenge_token': result['mfa']['challenge_token'], 'code': totp(credentials['totp'], now)}).payload
        if result.get('next_action') == 'select_context':
            result = {'session': self.public.request('POST', '/api/v1/system/auth/context-selections', (200,),
                        {'selection_ticket': result['selection']['selection_ticket'], 'context_type': 'platform'}).payload}
        token = result.get('session', {}).get('access_token')
        require(isinstance(token, str) and token.startswith('addp_at_'), 'login omitted access token')
        client = API.GatewayClient(self.env['GATEWAY_URL'], token, 10)
        assert_identity(client.request('GET', CONTEXT, (200,)).payload,
                        'system' if role == 'successor' else role, credentials['id'])
        self.actors[role] = client
        return client

    def create(self, role, kind, target, **extra):
        body = {'change_type': kind, 'target_principal_id': self.credentials[target]['id'],
                'reason': f'{self.run_id}:{kind}', **extra}
        return self.actors[role].request('POST', CHANGES, (201,), body).payload

    def review(self, role, request, decision='approve', status=200, version=None):
        result = self.actors[role].request('POST', f'{CHANGES}/{request["id"]}/{decision}', (status,),
                     {'version': version or request['version'], 'reason': f'{self.run_id}:{decision}'}).payload
        if status == 200:
            require(result.get('review', {}).get('principal_id') == self.credentials[role]['id'],
                    'review attribution mismatch')
        return result

    def status(self, target):
        return self.actors['security'].request('GET', '/api/v1/system/platform/users/' + self.credentials[target]['id'], (200,)).payload['status']

    def pause_restore(self, target):
        self.stage = 'pause-restore-' + target
        author, reviewer = ('system', 'audit') if target == 'security' else ('security', 'system' if target == 'audit' else 'audit')
        old = self.actors[target]
        old.request('POST', CHANGES, (403,), {'change_type': 'platform_identity_suspend',
                    'target_principal_id': self.credentials[target]['id'], 'reason': self.run_id+':wrong-applicant'})
        req = self.create(author, 'platform_identity_suspend', target)
        self.review(target, req, status=403)
        self.review(reviewer, req, status=409, version=str(int(req['version'])+1))
        applied = self.review(reviewer, req)
        require(applied.get('status') == 'applied', 'suspension was not atomic')
        old.request('GET', CONTEXT, (401,))
        self.review(reviewer, req, status=409)
        restored = self.review(reviewer, self.create(author, 'platform_identity_reactivate', target))
        require(restored.get('status') == 'applied', 'identity did not reactivate')
        old.request('GET', CONTEXT, (401,))
        self.login(target)

    def recovery(self):
        self.stage = 'credential-recovery'
        old = self.actors['system']
        self.review('audit', self.create('security', 'platform_identity_suspend', 'system'))
        approved = self.review('audit', self.create('security', 'platform_credential_recovery', 'system'))
        require(approved.get('status') == 'approved' and isinstance(approved.get('recovery_token'), str), 'recovery approval omitted proof')
        request = self.actors['audit'].request('GET', CHANGES + '/' + approved['id'], (200,)).payload
        require('recovery_token' not in request, 'recovery proof leaked in query')
        proof = approved['recovery_token']
        enrollment = self.public.request('POST', RECOVERY+'/enrollments', (201,), {'recovery_token': proof}).payload
        self.public.request('POST', RECOVERY+'/enrollments', (401,), {'recovery_token': proof})
        password = 'Recovered-' + secrets.token_urlsafe(24)
        new_secret = enrollment['secret']
        completion = {'enrollment_token': enrollment['enrollment_token'], 'password': password, 'code': totp(new_secret, time.time())}
        response = self.public.request('POST', RECOVERY+'/completions', (204,), completion)
        require(response.payload == {} and self.status('system') == 'suspended', 'credential recovery issued a session or unpaused identity')
        self.public.request('POST', RECOVERY+'/completions', (401,), completion)
        old.request('GET', CONTEXT, (401,))
        self.review('audit', self.create('security', 'platform_identity_reactivate', 'system'))
        previous_password = self.credentials['system']['password']
        self.credentials['system'].update(password=password, totp=new_secret)
        self.used_steps[new_secret] = int(time.time())//30
        self.login('system')
        self.public.request('POST', '/api/v1/system/login', (401,),
                            {'username': self.credentials['system']['username'], 'password': previous_password})

    def handover(self):
        self.stage = 'role-handover'
        original = self.actors['system']
        req = self.create('security', 'platform_role_replace', 'system', target_role_key='platform.system_administrator',
                          successor_principal_id=self.credentials['successor']['id'])
        require(self.review('audit', req).get('status') == 'applied', 'handover did not apply atomically')
        original.request('GET', CONTEXT, (401,))
        successor = self.login('successor')
        req = self.create('security', 'platform_role_replace', 'successor', target_role_key='platform.system_administrator',
                          successor_principal_id=self.credentials['system']['id'])
        require(self.review('audit', req).get('status') == 'applied', 'handover did not return original role')
        successor.request('GET', CONTEXT, (401,))
        self.login('system')

    def browser(self):
        self.stage = 'browser'
        receipt = Path(self.env['ADDP_ONLINE_ARTIFACT_DIR'])/'administrator-browser.json'
        require(not receipt.exists(), 'browser evidence path must be new')
        env = dict(self.env)
        for role in ROLES:
            for key, value in self.credentials[role].items():
                env[f'ADDP_ONLINE_GOV_{role.upper()}_{key.upper()}'] = value
        # Do not retain Playwright output: exception formatting can include form
        # values. Its only public evidence is the strict boolean receipt below.
        result = subprocess.run(['npm', 'exec', '--', 'playwright', 'test', '--config=playwright.online.config.js',
                                'platform-administrator-governance.spec.js', '--output='+str(Path(self.env['ADDP_ONLINE_SECRET_DIR'])/'governance-browser-output')], cwd=ROOT/'console/frontend',
                                env=env, capture_output=True, text=True, timeout=300)
        require(result.returncode == 0 and receipt.is_file(), 'real governance browser acceptance failed')
        value = json.loads(receipt.read_text())
        require(set(value) == BROWSER_PROOFS and all(value[key] is True for key in BROWSER_PROOFS),
                'browser receipt is incomplete or contains unapproved data')
        return value

    def run(self):
        for role in ROLES:
            self.login(role)
        negative = API.GatewayClient(self.env['GATEWAY_URL'], self.env['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'], 10)
        identity = negative.request('GET', CONTEXT, (200,)).payload
        require(identity.get('context', {}).get('type') == 'tenant', 'negative identity is not Tenant scoped')
        negative.request('GET', CHANGES, (403,))
        self.actors['audit'].request('POST', '/api/v1/system/platform/users', (403,), {})
        self.actors['system'].request('GET', '/api/v1/system/platform/identity_changes', (404,))
        for role in ROLES:
            self.pause_restore(role)
        self.recovery()
        self.handover()
        browser = self.browser()
        self.stage = 'final-state'
        require(self.actors['audit'].request('GET', CHANGES+'?status=pending', (200,)).payload.get('total') == 0,
                'pending governance requests remain')
        require(self.actors['audit'].request('GET', CHANGES+'?status=approved', (200,)).payload.get('total') == 0,
                'unconsumed approved recoveries remain')
        return {'schema_version': 'addp.online-suite/v1', 'suite': SUITE, 'run_id': self.run_id,
                'role_matrix': 'passed', 'version_and_replay': 'passed', 'suspension_and_session_revocation': 'passed',
                'one_time_recovery_preserves_suspension': 'passed', 'atomic_handover_and_return': 'passed',
                'browser': browser, 'pending_requests': 0, 'identity_cleanup': 'owned_by_disposable_hosted_infra'}


def main():
    scene = None
    try:
        validate_hosted(os.environ)
        args = type('Args', (), {'repository': ROOT, 'timeout': 10, 'service': [
            'system='+os.environ['SYSTEM_URL'], 'gateway='+os.environ['GATEWAY_URL']]})()
        PREFLIGHT.run_preflight(args)
        scene = Scenario(os.environ)
        report = scene.run()
    except Exception:
        # No raw exception, HTTP body, password, proof or TOTP in public logs.
        stage = scene.stage if scene else 'preflight'
        print('Platform administrator governance acceptance failed at ' + stage + '; no sensitive response retained', file=sys.stderr)
        return 1
    print(json.dumps(report, sort_keys=True))
    return 0


if __name__ == '__main__':
    raise SystemExit(main())

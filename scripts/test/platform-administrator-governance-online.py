"""Disposable Hosted platform officer governance through real Gateway APIs."""
from __future__ import annotations

import base64
import codecs
import datetime
import errno
import hashlib
import hmac
import json
import os
import platform
import pty
import re
import secrets
import select
import signal
import struct
import subprocess
import sys
import termios
import time
import uuid
import urllib.error
import urllib.request
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
CLI_DIR = ROOT/'dist/release-linux-amd64'
OFFLINE_PROOFS = {'tty_only', 'two_real_totp_counters', 'all_principals_recovered',
                  'old_credentials_rejected', 'old_sessions_revoked', 'roles_unchanged', 'suspension_preserved',
                  'secret_single_use', 'bootstrap_not_reopened', 'audit_complete', 'credentials_memory_only',
                  'tenant_boundary_preserved', 'same_role_multiple_holders', 'additional_holder_recovered',
                  'additional_role_revoked', 'interrupted_before_apply', 'interrupted_credentials_unchanged',
                  'interrupted_sessions_valid', 'interrupted_audit_prepared_only', 'same_secret_retry_completed',
                  'prepared_resolved_after_cli_exit', 'unknown_attempt_not_retry_permission',
                  'apply_process_exited_before_resolution', 'original_submission_resolved',
                  'other_submission_distinguished', 'historical_receipt_preserved', 'resolution_has_no_side_effects'}


class GovernanceAssertion(API.SuiteError):
    """Only fixed, owner-authored assertion messages may be reported publicly."""


def require(condition, message):
    if not condition:
        raise GovernanceAssertion(message)


def totp(secret, at):
    key = base64.b32decode(secret + '=' * (-len(secret) % 8))
    digest = hmac.new(key, struct.pack('>Q', int(at)//30), hashlib.sha1).digest()
    offset = digest[-1] & 15
    return f'{(struct.unpack(">I", digest[offset:offset+4])[0] & 0x7fffffff) % 1000000:06d}'


def recovery_cli_environment(environment):
    # The CLI uses normal deployment configuration, never Online User tokens,
    # passwords or TOTP secrets inherited from the scenario fixture.
    return {key: value for key, value in environment.items() if not key.startswith('ADDP_ONLINE_')}


class RecoveryTerminal:
    """A private PTY with bounded, memory-only output and no transcript logging."""
    def __init__(self, command, environment):
        self.command, self.environment = command, environment
        self.pending = ''

    def __enter__(self):
        self.master, slave = pty.openpty()
        try:
            self.process = subprocess.Popen(self.command, stdin=slave, stdout=slave, stderr=slave,
                    cwd=ROOT, env=self.environment, close_fds=True, start_new_session=True)
        except Exception:
            os.close(self.master)
            raise
        finally:
            os.close(slave)
        self.decoder = codecs.getincrementaldecoder('utf-8')()
        return self

    def expect(self, pattern, timeout=30):
        deadline = time.monotonic() + timeout
        while True:
            match = re.search(pattern, self.pending)
            if match:
                self.pending = self.pending[match.end():]
                return match
            require(time.monotonic() < deadline, 'offline terminal prompt timed out')
            ready, _, _ = select.select([self.master], [], [], min(.1, max(0, deadline-time.monotonic())))
            if not ready:
                continue
            try:
                chunk = os.read(self.master, 8192)
            except OSError as error:
                if error.errno != errno.EIO:
                    raise
                chunk = b''
            require(bool(chunk), 'offline terminal ended before expected prompt')
            self.pending += self.decoder.decode(chunk)
            require(len(self.pending) <= 262144, 'offline terminal output exceeded its bound')

    def send_line(self, value, hidden=False):
        require('\n' not in value and '\r' not in value, 'offline terminal input must be one line')
        if hidden:
            # ReadPassword writes its prompt before disabling echo. Wait until
            # termios confirms the hidden-input boundary before supplying it.
            deadline = time.monotonic() + 2
            while termios.tcgetattr(self.master)[3] & termios.ECHO:
                require(time.monotonic() < deadline, 'offline hidden input did not disable echo')
                time.sleep(.01)
        data = (value+'\n').encode()
        while data:
            data = data[os.write(self.master, data):]

    def finish(self, expected_exit):
        require(self.process.wait(timeout=10) == expected_exit, 'offline CLI returned an unexpected exit status')

    def interrupt(self):
        require(self.process.poll() is None, 'offline CLI exited before the interruption boundary')
        os.killpg(self.process.pid, signal.SIGTERM)
        self.finish(-signal.SIGTERM)

    def __exit__(self, *_):
        try:
            if self.process.poll() is None:
                try:
                    os.killpg(self.process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                self.process.wait(timeout=5)
        finally:
            os.close(self.master)
            self.pending = ''
            self.decoder = None


def collect_recovery_credentials(terminal, credentials, *, interrupt_before_last=False):
    replacements, counters = {}, {}
    by_username = {item['username']: pid for pid, item in credentials.items()}
    require(len(by_username) == len(credentials) and all(pid == item['id'] for pid, item in credentials.items()),
            'offline expected administrator identities are ambiguous')
    for _ in credentials:
        target = terminal.expect(r'恢复 [^\r\n]+（(platform\.(system|security|audit)_administrator)，账号 ([^\r\n）]+)）')
        principal_id = by_username.get(target[3])
        require(principal_id in credentials and principal_id not in replacements
                and target[1] == credentials[principal_id]['role_key'],
                'offline CLI selected an unexpected administrator identity')
        if interrupt_before_last and len(replacements) == len(credentials)-1:
            require(any(item['role_key'] == credentials[principal_id]['role_key'] for item in replacements.values()),
                    'interruption must separate holders of the same role')
            terminal.expect('输入新密码（至少 14 字符）: ')
            terminal.interrupt()
            return replacements, counters, None, None
        password = 'Offline-recovered-' + secrets.token_urlsafe(24)
        terminal.expect('输入新密码（至少 14 字符）: ')
        terminal.send_line(password, hidden=True)
        terminal.expect('再次输入新密码: ')
        terminal.send_line(password, hidden=True)
        secret = terminal.expect(r'备用 TOTP Secret: ([A-Z2-7]+=*)\r?\n')[1]
        require(secret not in counters, 'offline CLI reused a TOTP secret across identities')
        terminal.expect('输入当前 TOTP 验证码: ')
        now = time.time()
        counter = int(now)//30
        terminal.send_line(totp(secret, now))
        terminal.expect('等待验证码变化后输入下一个 TOTP 验证码: ')
        time.sleep(max(0, (counter+1)*30 - time.time()) + .2)
        require(int(time.time())//30 == counter+1, 'offline consecutive TOTP window was missed')
        terminal.send_line(totp(secret, time.time()))
        counters[secret] = counter+1
        replacements[principal_id] = {**credentials[principal_id], 'password': password, 'totp': secret}
    submission = terminal.expect(r'Submission ID: ([0-9a-f-]{36})\r?\n')[1]
    require(str(uuid.UUID(submission)) == submission, 'offline submission identity is malformed')
    attempt = terminal.expect(r'IAM 管理员凭据恢复已完成于 [^\r\n]+（Attempt ([1-9][0-9]*)）')[1]
    for principal_id in sorted(credentials, key=int):
        terminal.expect(re.escape(f'{credentials[principal_id]["role_key"]} -> Principal {principal_id}') + r'\r?\n')
    terminal.finish(0)
    return replacements, counters, attempt, submission


def read_recovery_resolution(terminal, attempt_id, expected_state, *, winner=None, holders=None, versions=None):
    terminal.expect(re.escape(f'Attempt ID: {attempt_id}') + r'\r?\n')
    terminal.expect(re.escape('state=' + expected_state) + r'\r?\n')
    receipt = {'attempt_id': str(attempt_id), 'state': expected_state}
    if expected_state.startswith('completed_'):
        terminal.expect(re.escape(f'Submission ID: {winner}') + r'\r?\n')
        receipt['submission_id'] = winner
        timestamp = terminal.expect(r'completed_at=([0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:Z|[+-][0-9]{2}:[0-5][0-9]))\r?\n')[1]
        try:
            completed_at = datetime.datetime.fromisoformat(timestamp.replace('Z', '+00:00'))
        except ValueError:
            raise GovernanceAssertion('offline resolution completion time is not a valid RFC3339 timestamp') from None
        receipt['completed_at'] = completed_at.astimezone(datetime.timezone.utc).isoformat()
        for pid in sorted(holders, key=int):
            terminal.expect(re.escape(f'{holders[pid]["role_key"]} -> Principal {pid}; authorization_version={versions[pid]}') + r'\r?\n')
        receipt['principals'] = {pid: (holders[pid]['role_key'], versions[pid]) for pid in holders}
    terminal.finish(1 if expected_state == 'unknown' else 0)
    return receipt


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


def assert_recovery_audit(events, attempt_id, credentials, sensitive):
    rows = events.get('data', [])
    expected = {'iam.recovery.prepared': 1, 'iam.recovery.administrator_credentials_replaced': len(credentials), 'iam.recovery.completed': 1}
    require(events.get('total') == len(credentials)+2 and len(rows) == len(credentials)+2 and
            {name: sum(row.get('event_name') == name for row in rows) for name in expected} == expected,
            'offline recovery audit events are incomplete')
    require(all(row.get('entity_id') == attempt_id and row.get('result') == 'succeeded' for row in rows),
            'offline recovery audit binding differs')
    replaced = [row for row in rows if row['event_name'] == 'iam.recovery.administrator_credentials_replaced']
    require({(row.get('principal_id'), row['details'].get('role_key')) for row in replaced} ==
            {(item['id'], item['role_key']) for item in credentials.values()},
            'offline audit did not cover every recovered principal')
    assert_audit_credentials_absent(rows, sensitive)


def assert_audit_credentials_absent(rows, sensitive):
    audit = json.dumps(rows)
    for value in sensitive:
        require(value not in audit, 'offline recovery audit contains a credential')


def assert_recovery_prepared_audit(events, holder_count, sensitive):
    rows = events.get('data', [])
    require(events.get('total') == 1 and len(rows) == 1, 'interrupted recovery changed more than preparation audit')
    row = rows[0]
    attempt = row.get('entity_id')
    require(row.get('event_name') == 'iam.recovery.prepared' and row.get('result') == 'succeeded'
            and row.get('details', {}).get('administrator_count') == holder_count
            and isinstance(attempt, str) and re.fullmatch('[1-9][0-9]*', attempt) is not None,
            'interrupted recovery preparation audit differs')
    assert_audit_credentials_absent(rows, sensitive)
    return attempt


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

    def authenticate(self, role):
        phase = self.stage
        self.stage = phase + '-password'
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
        self.stage = phase + '-mfa'
        result = self.public.request('POST', '/api/v1/system/auth/mfa-verifications', (200,),
                    {'challenge_token': result['mfa']['challenge_token'], 'code': totp(credentials['totp'], now)}).payload
        self.stage = phase
        return result

    def login(self, role):
        phase = self.stage
        credentials = self.credentials[role]
        result = self.authenticate(role)
        if result.get('next_action') == 'select_context':
            self.stage = phase + '-context-selection'
            result = {'session': self.public.request('POST', '/api/v1/system/auth/context-selections', (200,),
                        {'selection_ticket': result['selection']['selection_ticket'], 'context_type': 'platform'}).payload}
        token = result.get('session', {}).get('access_token')
        require(isinstance(token, str) and token.startswith('addp_at_'), 'login omitted access token')
        client = API.GatewayClient(self.env['GATEWAY_URL'], token, 10)
        self.stage = phase + '-auth-context'
        assert_identity(client.request('GET', CONTEXT, (200,)).payload,
                        'system' if role == 'successor' else role, credentials['id'])
        self.actors[role] = client
        self.stage = phase
        return client

    def emergency_session_revocation(self):
        self.stage = 'emergency-session-revocation'
        # Enrollment already consumed a real counter; wait for a new one instead of retrying a rejected code.
        self.used_steps.setdefault(self.credentials['successor']['totp'], int(time.time())//30)
        result = self.authenticate('successor')
        token = result.get('session', {}).get('access_token')
        require(isinstance(token, str), 'emergency executor must resolve to its single Tenant context')
        executor = API.GatewayClient(self.env['GATEWAY_URL'], token, 10)
        context = executor.request('GET', CONTEXT, (200,)).payload
        require(context.get('context', {}).get('type') == 'tenant'
                and context['context'].get('tenant_id') == self.env['ADDP_ONLINE_TEST_TENANT_ID'],
                'emergency executor escaped its Tenant')
        target = API.GatewayClient(self.env['GATEWAY_URL'], self.env['ADDP_ONLINE_GOV_TARGET_ACCESS_TOKEN'], 10)
        other = API.GatewayClient(self.env['GATEWAY_URL'], self.env['ADDP_ONLINE_GOV_TARGET_OTHER_ACCESS_TOKEN'], 10)
        before = target.request('GET', CONTEXT, (200,)).payload
        endpoint = '/api/v1/system/tenant/emergency_session_requests'
        platform_endpoint = '/api/v1/system/platform/emergency_session_requests'
        request_id, execution_id = str(uuid.uuid4()), str(uuid.uuid4())
        body = {'id': request_id, 'target_principal_id': before['principal']['id'],
                'target_family_id': self.env['ADDP_ONLINE_GOV_TARGET_FAMILY_ID'],
                'reason': 'Disposable suspicious Web session', 'incident_reference': self.run_id}
        request = executor.request('POST', endpoint, (201,), body).payload
        require(request.get('tenant_id') == self.env['ADDP_ONLINE_TEST_TENANT_ID']
                and request.get('executor_principal_id') == self.credentials['successor']['id']
                and request.get('tenant_name') and request.get('target_name'), 'emergency request binding differs')
        self.actors['audit'].request('POST', platform_endpoint+'/'+request_id+'/approve', (403,),
                                     {'version': request['version'], 'reason': 'audit must only observe'})
        executor.request('POST', endpoint+'/'+request_id+'/execute', (409,),
                         {'version': request['version'], 'execution_id': execution_id})
        for role in ('system', 'security'):
            request = self.actors[role].request('POST', platform_endpoint+'/'+request_id+'/approve', (200,),
                         {'version': request['version'], 'reason': 'Independent '+role+' review'}).payload
            if role == 'system':
                executor.request('POST', endpoint+'/'+request_id+'/execute', (409,),
                                 {'version': request['version'], 'execution_id': execution_id})
        require(request.get('status') == 'approved' and len(request.get('approvals', [])) == 2,
                'two distinct platform approvals were not persisted')
        parse_time = lambda value: datetime.datetime.fromisoformat(value.replace('Z', '+00:00'))
        require((parse_time(request['approval_expires_at'])-parse_time(request['requested_at'])).total_seconds() == 600
                and (parse_time(request['execute_before'])-parse_time(request['approved_at'])).total_seconds() == 900,
                'emergency ten/fifteen minute windows differ')
        command = {'version': request['version'], 'execution_id': execution_id}
        receipt = executor.request('POST', endpoint+'/'+request_id+'/execute', (200,), command).payload
        require(receipt.get('status') == 'executed' and receipt.get('outcome') == 'revoked'
                and receipt.get('execution_id') == execution_id, 'emergency execution receipt differs')
        target.request('GET', CONTEXT, (401,))
        unchanged = other.request('GET', CONTEXT, (200,)).payload
        require(unchanged['authorization'] == before['authorization'], 'another family or global authorization changed')
        replay = executor.request('POST', endpoint+'/'+request_id+'/execute', (200,), command).payload
        require(replay == receipt, 'same execution did not return immutable receipt')
        executor.request('POST', endpoint+'/'+request_id+'/execute', (409,),
                         {'version': receipt['version'], 'execution_id': str(uuid.uuid4())})
        resolved = self.actors['audit'].request('GET', platform_endpoint+'/'+request_id, (200,)).payload
        require(resolved == receipt, 'independent audit could not resolve original execution')
        events = self.actors['audit'].request('GET', '/api/v1/system/platform/audit/events?entity_type=emergency_session_request&page_size=100', (200,)).payload
        rows = [row for row in events.get('data', []) if row.get('entity_id') == request_id]
        require(len(rows) == 4 and sum(row.get('event_name') == 'iam.emergency_session_revocation.approve' for row in rows) == 2,
                'emergency independent business audit count differs')
        assert_audit_credentials_absent(rows, [token, target.token, other.token])
        return {'exact_tenant_family': True, 'two_independent_approvals': True,
                'ten_fifteen_minute_windows': True, 'single_consumption_receipt': True,
                'unrelated_session_unchanged': True, 'independent_audit': True}

    def assert_legacy_route_removed(self):
        # An unregistered Gin route has no JSON response contract. Check its
        # actual HTTP status without passing it through the JSON API transport.
        client = self.actors['system']
        request = urllib.request.Request(client.base_url + '/api/v1/system/platform/identity_changes',
                    headers={'Authorization': 'Bearer ' + client.token}, method='GET')
        try:
            with urllib.request.urlopen(request, timeout=client.timeout) as response:
                status = response.status
        except urllib.error.HTTPError as error:
            with error:
                status = error.code
        require(status == 404, 'removed governance route must return HTTP 404')

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
        self.review(target, req, status=403 if target == 'security' else 409)
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

    def reject_totp(self, actor, rejected_secret):
        credentials = self.credentials[actor]
        # A six-digit collision with the current credential is not evidence of
        # acceptance of the rejected secret. Submit only a distinct value.
        now = time.time()
        while totp(rejected_secret, now) in {totp(credentials['totp'], now+offset) for offset in (-30, 0, 30)}:
            time.sleep(30 - now % 30 + .2)
            now = time.time()
        challenge = self.public.request('POST', '/api/v1/system/login', (200,),
                    {key: credentials[key] for key in ('username', 'password')}).payload
        require(challenge.get('next_action') == 'verify_mfa', 'offline credential verification bypassed MFA')
        self.public.request('POST', '/api/v1/system/auth/mfa-verifications', (401,),
                    {'challenge_token': challenge['mfa']['challenge_token'], 'code': totp(rejected_secret, now)})

    def offline_recovery(self):
        self.stage = 'offline-admission'
        validate_hosted(self.env)
        binaries = {name: CLI_DIR/('addp-iam-'+name) for name in ('recovery', 'bootstrap')}
        require(all(path.is_file() and os.access(path, os.X_OK) for path in binaries.values()),
                'offline acceptance requires the standard CLI build artifacts')
        cli_env = recovery_cli_environment(self.env)
        def terminal(name, action, *identifiers):
            command = [str(binaries[name]), action, *map(str, identifiers)]
            if name == "recovery" and action == "apply":
                command.append(str(recovery_attempt))
            return RecoveryTerminal(command, cli_env)

        self.stage = 'offline-additional-holder'
        granted = self.review('audit', self.create('security', 'platform_role_grant', 'successor',
                                                    target_role_key='platform.system_administrator'))
        require(granted.get('status') == 'applied', 'additional same-role holder was not approved')
        self.login('successor')
        holder_actors = (*ROLES, 'successor')
        before = {actor: self.actors[actor].request('GET', CONTEXT, (200,)).payload['authorization']
                  for actor in holder_actors}
        holders = {self.credentials[actor]['id']: {**self.credentials[actor],
                   'role_key': before[actor]['role_assignments'][0]['role_key']} for actor in holder_actors}
        require(len(holders) == 4 and sum(item['role_key'] == 'platform.system_administrator'
                for item in holders.values()) == 2, 'offline recovery requires four distinct current holders')
        old_credentials = {actor: dict(self.credentials[actor]) for actor in holder_actors}
        old_sessions = {actor: self.actors[actor] for actor in holder_actors}
        self.stage = 'offline-tty-boundary'
        for action in ('prepare', 'apply', 'status'):
            identifiers = [] if action == 'prepare' else ['1'] if action == 'apply' else ['1', str(uuid.uuid4())]
            result = subprocess.run([str(binaries['recovery']), action, *identifiers], stdin=subprocess.DEVNULL,
                        capture_output=True, text=True, env=cli_env, cwd=ROOT, timeout=20)
            require(result.returncode == 1 and '必须直接运行在交互终端' in result.stderr,
                    'offline CLI accepted nonterminal input or output')

        self.stage = 'offline-prepare'
        with terminal('recovery', 'prepare') as console:
            recovery_attempt = int(console.expect(r'Attempt ID: ([1-9][0-9]*)\r?\n')[1])
            proof = console.expect(r'\r?\n(addp_ir_[A-Za-z0-9_-]+)\r?\n')[1]
            console.finish(0)
        with terminal('recovery', 'prepare') as console:
            console.expect('an IAM recovery attempt is already prepared')
            console.finish(1)
        with terminal('recovery', 'apply') as console:
            console.expect('Recovery Secret: ')
            console.send_line('addp_ir_' + secrets.token_urlsafe(32), hidden=True)
            console.expect('IAM Recovery 失败: 未授权')
            console.finish(1)

        self.stage = 'offline-interrupted-apply'
        with terminal('recovery', 'apply') as console:
            console.expect('Recovery Secret: ')
            console.send_line(proof, hidden=True)
            interrupted_pid = console.process.pid
            uncommitted, _, interrupted_attempt, interrupted_submission = collect_recovery_credentials(console, holders, interrupt_before_last=True)
        require(interrupted_attempt is None and interrupted_submission is None and len(uncommitted) == len(holders)-1,
                'offline CLI did not stop before the final holder')
        self.stage = 'offline-interrupted-resolution'
        with terminal('recovery', 'status', recovery_attempt, uuid.uuid4()) as status:
            require(status.process.pid != interrupted_pid, 'resolution reused the interrupted CLI process')
            read_recovery_resolution(status, recovery_attempt, 'prepared')
        with terminal('recovery', 'status', recovery_attempt + 1000000, uuid.uuid4()) as status:
            read_recovery_resolution(status, recovery_attempt + 1000000, 'unknown')
        self.stage = 'offline-interrupted-identities'
        for actor in holder_actors:
            current = old_sessions[actor].request('GET', CONTEXT, (200,)).payload
            assert_identity(current, 'system' if actor == 'successor' else actor, self.credentials[actor]['id'])
            require(current['authorization']['role_assignments'] == before[actor]['role_assignments']
                    and current['authorization']['authorization_version'] == before[actor]['authorization_version'],
                    'interrupted recovery changed a principal authorization')
            # All four original passwords and TOTP credentials must still
            # complete real MFA, including the two same-role principals.
            self.login(actor)
            partial = uncommitted.get(self.credentials[actor]['id'])
            if partial:
                self.public.request('POST', '/api/v1/system/login', (401,),
                            {key: partial[key] for key in ('username', 'password')})
                self.reject_totp(actor, partial['totp'])
        interrupted_sessions = dict(self.actors)
        self.stage = 'offline-interrupted-audit'
        sensitive = [proof, *(item[key] for item in uncommitted.values() for key in ('password', 'totp'))]
        events = self.actors['audit'].request('GET', '/api/v1/system/platform/audit/events?entity_type=iam_recovery_attempt&page_size=100', (200,)).payload
        prepared_attempt = assert_recovery_prepared_audit(events, len(holders), sensitive)
        with terminal('recovery', 'prepare') as console:
            console.expect('an IAM recovery attempt is already prepared')
            console.finish(1)
        # Preserve the original suspension acceptance after checking that all
        # four original identities survived the interrupted input collection.
        self.review('system', self.create('security', 'platform_identity_suspend', 'audit'))
        self.stage = 'offline-apply'
        with terminal('recovery', 'apply') as console:
            console.expect('Recovery Secret: ')
            console.send_line(proof, hidden=True)
            apply_pid = console.process.pid
            recovered, counters, attempt_id, submission_id = collect_recovery_credentials(console, holders)
        require(attempt_id == prepared_attempt == str(recovery_attempt), 'offline retry consumed a different recovery attempt')
        require(console.process.poll() == 0, 'Apply CLI had not exited before result resolution')
        receipt_versions = {self.credentials[actor]['id']: int(before[actor]['authorization_version'])
                            + (2 if actor == 'audit' else 1) for actor in holder_actors}
        self.stage = 'offline-completed-resolution'
        with terminal('recovery', 'status', attempt_id, submission_id) as status:
            require(status.process.pid != apply_pid, 'resolution reused the Apply CLI process')
            original_receipt = read_recovery_resolution(status, attempt_id, 'completed_this_submission',
                    winner=submission_id, holders=holders, versions=receipt_versions)
        other_submission = str(uuid.uuid4())
        require(other_submission != submission_id, 'resolution reference must differ from the winner')
        with terminal('recovery', 'status', attempt_id, other_submission) as status:
            read_recovery_resolution(status, attempt_id, 'completed_other_submission',
                    winner=submission_id, holders=holders, versions=receipt_versions)
        self.stage = 'offline-single-use'
        with terminal('recovery', 'apply') as console:
            console.expect('Recovery Secret: ')
            console.send_line(proof, hidden=True)
            console.expect('IAM Recovery 失败: 未授权')
            console.finish(1)
        with terminal('bootstrap', 'prepare') as console:
            console.expect('IAM bootstrap was already prepared')
            console.finish(1)
        for actor in holder_actors:
            old_sessions[actor].request('GET', CONTEXT, (401,))
            interrupted_sessions[actor].request('GET', CONTEXT, (401,))
            self.credentials[actor] = recovered[self.credentials[actor]['id']]
        self.used_steps.update(counters)

        self.stage = 'offline-suspension-preserved'
        self.public.request('POST', '/api/v1/system/login', (401,),
                    {key: self.credentials['audit'][key] for key in ('username', 'password')})
        self.login('security')
        self.login('system')
        require(self.status('audit') == 'suspended', 'offline recovery silently reactivated a suspended identity')
        applied = self.review('system', self.create('security', 'platform_identity_reactivate', 'audit'))
        require(applied.get('status') == 'applied', 'recovered officers could not independently restore identity')
        self.login('audit')
        self.login('successor')

        self.stage = 'offline-old-credentials'
        for role in holder_actors:
            self.public.request('POST', '/api/v1/system/login', (401,),
                        {key: old_credentials[role][key] for key in ('username', 'password')})
            self.reject_totp(role, old_credentials[role]['totp'])
            current = self.actors[role].request('GET', CONTEXT, (200,)).payload
            require(current['authorization']['role_assignments'] == before[role]['role_assignments'],
                    'offline recovery changed role assignments')
            require(int(current['authorization']['authorization_version']) ==
                    int(before[role]['authorization_version']) + (3 if role == 'audit' else 1),
                    'offline recovery did not advance every principal authorization version')

        tenant = API.GatewayClient(self.env['GATEWAY_URL'], self.env['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'], 10)
        identity = tenant.request('GET', CONTEXT, (200,)).payload
        require(identity.get('context', {}).get('tenant_id') == self.env['ADDP_ONLINE_TEST_TENANT_ID'],
                'offline recovery affected an unrelated Tenant identity')

        self.stage = 'offline-audit'
        events = self.actors['audit'].request('GET', '/api/v1/system/platform/audit/events?entity_type=iam_recovery_attempt&page_size=100', (200,)).payload
        assert_recovery_audit(events, attempt_id, recovered,
                [proof, *(item[key] for item in [*old_credentials.values(), *uncommitted.values(), *recovered.values()] for key in ('password', 'totp'))])
        self.stage = 'offline-remove-additional-holder'
        additional_session = self.actors['successor']
        revoked = self.review('audit', self.create('security', 'platform_role_revoke', 'successor',
                                                   target_role_key='platform.system_administrator'))
        require(revoked.get('status') == 'applied', 'additional same-role assignment was not revoked')
        additional_session.request('GET', CONTEXT, (401,))
        # Revocation must leave the candidate's original Tenant membership and
        # the three original Platform assignments intact.
        result = self.authenticate('successor')
        token = result.get('session', {}).get('access_token')
        require(result.get('next_action') == 'session_issued' and isinstance(token, str) and token.startswith('addp_at_'),
                'candidate retained another context after role revocation')
        candidate_context = API.GatewayClient(self.env['GATEWAY_URL'], token, 10).request('GET', CONTEXT, (200,)).payload
        context = candidate_context.get('context', {})
        require(candidate_context.get('principal') == {'id': self.credentials['successor']['id'], 'type': 'user'}
                and context.get('type') == 'tenant' and context.get('tenant_id') == self.env['ADDP_ONLINE_TEST_TENANT_ID']
                and bool(context.get('tenant_membership_id'))
                and candidate_context.get('authentication', {}).get('assurance_level') in {'aal2', 'aal3'}
                and candidate_context.get('token', {}).get('type') == 'first_party_access_token'
                and all(row.get('scope', {}).get('type') == 'tenant' for row in
                        candidate_context.get('authorization', {}).get('role_assignments', [])),
                'candidate Tenant membership changed after role revocation')
        for role in ROLES:
            current = self.actors[role].request('GET', CONTEXT, (200,)).payload
            assert_identity(current, role, self.credentials[role]['id'])
            require(current['authorization']['role_assignments'] == before[role]['role_assignments'],
                    'removing the additional holder changed an original assignment')
        self.stage = 'offline-historical-resolution'
        current_contexts = {role: self.actors[role].request('GET', CONTEXT, (200,)).payload for role in ROLES}
        with terminal('recovery', 'status', attempt_id, submission_id) as status:
            repeated_receipt = read_recovery_resolution(status, attempt_id, 'completed_this_submission',
                    winner=submission_id, holders=holders, versions=receipt_versions)
        require(repeated_receipt == original_receipt, 'later identity governance changed the historical recovery receipt')
        after_resolution = self.actors['audit'].request('GET', '/api/v1/system/platform/audit/events?entity_type=iam_recovery_attempt&page_size=100', (200,)).payload
        require(after_resolution.get('data') == events.get('data'), 'resolution wrote or changed recovery audit facts')
        for role in ROLES:
            after = self.actors[role].request('GET', CONTEXT, (200,)).payload
            require(after.get('principal') == current_contexts[role].get('principal')
                    and after.get('authorization') == current_contexts[role].get('authorization'),
                    'resolution changed a current principal, role or authorization version')
        after_candidate = API.GatewayClient(self.env['GATEWAY_URL'], token, 10).request('GET', CONTEXT, (200,)).payload
        require(after_candidate.get('principal') == candidate_context.get('principal')
                and after_candidate.get('context') == candidate_context.get('context'),
                'resolution invalidated the candidate Tenant session')
        # Only booleans are returned; no PTY output or recovery credential leaves
        # this process or enters the artifact directory.
        return {key: True for key in OFFLINE_PROOFS}

    def browser(self):
        self.stage = 'browser'
        receipt = Path(self.env['ADDP_ONLINE_ARTIFACT_DIR'])/'administrator-browser.json'
        require(not receipt.exists(), 'browser evidence path must be new')
        env = {key: value for key, value in self.env.items() if not key.startswith('ADDP_ONLINE_GOV_TARGET_')}
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
            self.stage = 'authentication-' + role
            self.login(role)
        self.stage = 'tenant-boundary'
        negative = API.GatewayClient(self.env['GATEWAY_URL'], self.env['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'], 10)
        identity = negative.request('GET', CONTEXT, (200,)).payload
        require(identity.get('context', {}).get('type') == 'tenant', 'negative identity is not Tenant scoped')
        negative.request('GET', CHANGES, (403,))
        emergency = self.emergency_session_revocation()
        self.stage = 'audit-write-boundary'
        self.actors['audit'].request('POST', '/api/v1/system/platform/users', (403,), {})
        self.stage = 'legacy-route-removed'
        self.assert_legacy_route_removed()
        for role in ROLES:
            self.pause_restore(role)
        self.recovery()
        self.handover()
        browser = self.browser()
        # The browser intentionally replaces System credentials without an
        # export. Re-enroll through the existing approved API flow so this
        # process owns a current pre-offline session for every officer. Run
        # offline last: none of its new credentials enter a child environment.
        self.recovery()
        offline = self.offline_recovery()
        self.stage = 'final-state'
        require(self.actors['audit'].request('GET', CHANGES+'?status=pending', (200,)).payload.get('total') == 0,
                'pending governance requests remain')
        require(self.actors['audit'].request('GET', CHANGES+'?status=approved', (200,)).payload.get('total') == 0,
                'unconsumed approved recoveries remain')
        return {'schema_version': 'addp.online-suite/v1', 'suite': SUITE, 'run_id': self.run_id,
                'role_matrix': 'passed', 'version_and_replay': 'passed', 'suspension_and_session_revocation': 'passed',
                'one_time_recovery_preserves_suspension': 'passed', 'atomic_handover_and_return': 'passed',
                'offline_recovery': offline, 'browser': browser, 'emergency_session_revocation': emergency, 'pending_requests': 0,
                'identity_cleanup': 'owned_by_disposable_hosted_infra'}


def main():
    scene = None
    try:
        validate_hosted(os.environ)
        args = type('Args', (), {'repository': ROOT, 'timeout': 10, 'service': [
            'system='+os.environ['SYSTEM_URL'], 'gateway='+os.environ['GATEWAY_URL']]})()
        PREFLIGHT.run_preflight(args)
        scene = Scenario(os.environ)
        report = scene.run()
    except Exception as error:
        # No raw exception, HTTP body, password, proof or TOTP in public logs.
        stage = scene.stage if scene else 'preflight'
        diagnostic = 'unexpected-exception'
        if isinstance(error, GovernanceAssertion):
            diagnostic = str(error)
        elif isinstance(error, API.SuiteError):
            status = re.search(r' returned HTTP ([1-5][0-9]{2}) ', str(error))
            if status:
                diagnostic = 'http-' + status[1]
            elif str(error).endswith('returned invalid JSON'):
                diagnostic = 'invalid-json'
            else:
                diagnostic = 'api-transport'
        print('Platform administrator governance acceptance failed at ' + stage + '; ' + diagnostic + '; no sensitive response retained', file=sys.stderr)
        return 1
    print(json.dumps(report, sort_keys=True))
    return 0


if __name__ == '__main__':
    raise SystemExit(main())

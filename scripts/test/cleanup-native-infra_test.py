"""Deterministic native profile admission, routing, evidence and destruction tests."""
from copy import deepcopy
from importlib import import_module
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

N = import_module('scripts.test.cleanup-native-infra')
G = import_module('scripts.test.online-gate')
P = import_module('scripts.test.online-preflight')
ROOT = Path(__file__).resolve().parents[2]
CONTRACT = json.loads((ROOT / 'scripts/test/execution-online-contract.json').read_text())


def config():
    services = {name: {'ports': [{'target': target, 'published': str(port), 'host_ip': '127.0.0.1'}]}
                for name, target, port in (('minio', 9000, 19000), ('meilisearch', 7700, 17700),
                                           ('kafka-connect', 8083, 18083), ('redpanda', 9092, 19092))}
    services['redpanda'].update(image='canonical-redpanda',
        command=['redpanda', 'start', '--advertise-kafka-addr', 'internal://redpanda:29092,external://localhost:19092',
                 '--set', 'redpanda.enable_sasl=true'])
    return {'name': 'addp-infra', 'services': services, 'networks': {'addp-network': {'external': True}}}


class Harness(N.Lifecycle):
    def __init__(self, secret):
        self.secret, self.case, self.run_id = secret, 'manager-success', 'native-test'
        self.state_file = secret / 'native-container-pins.json'
        self.state = {'run_id': self.run_id, 'pins': {}}
        self.calls, self.rows = [], {}

    def command(self, args, **kwargs):
        self.calls.append(args)
        if args[:2] == ['docker', 'run']:
            name = args[args.index('--name') + 1]
            identifier = f'{len(self.rows) + 1:064x}'
            self.rows[identifier] = dict(Id=identifier, Name='/' + name, Image='sha256:fixed',
                State={'Running': True, 'StartedAt': '2026-10-30T01:00:00Z'},
                Config={'Labels': {'com.addp.online-native-run': self.run_id, 'com.addp.online-fixture': 'cleanup-native-effects',
                                  'com.addp.online-run': self.run_id, 'com.docker.compose.project.working_dir': str(ROOT)},
                        'Env': ['ADDP_ONLINE_HOSTED=1','ADDP_ONLINE_TEST=1','ADDP_ONLINE_TEST_RUN_ID='+self.run_id]})
            return identifier
        if args[:2] == ['docker', 'rm']:
            self.rows.pop(args[-1]); return ''
        if args[-3:] == ['config', '--format', 'json']:
            return json.dumps(config())
        return ''

    def inspect(self, name):
        return deepcopy(self.rows[name])

    def docker_result(self, args, **kwargs):
        identifier = args[-1]
        rows = [r for r in self.rows.values() if r['Id'] == identifier or r['Name'] == '/' + identifier]
        return subprocess.CompletedProcess(args, 0 if rows else 1,
            stdout=json.dumps(rows) if kwargs.get('text') else json.dumps(rows).encode(),
            stderr=('' if rows else 'Error: No such container') if kwargs.get('text') else (b'' if rows else b'Error: No such container'))


class NativeInfraTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.secret = Path(self.temp.name) / 'addp-online-secret-test'
        self.secret.mkdir(mode=0o700)
        self.life = Harness(self.secret)

    def test_kafka_changes_only_internal_advertised_listener_and_rejects_drift(self):
        original = config()
        changed = N.advertised_config(original)
        expected = deepcopy(original)
        expected['services']['redpanda']['command'][3] = 'internal://cleanup-native-dlq-proxy:29092,external://localhost:19092'
        self.assertEqual(changed, expected)
        self.assertEqual(original, config())
        for mutation in ('name', 'listener', 'duplicate'):
            invalid = config()
            if mutation == 'name': invalid['name'] = 'personal'
            elif mutation == 'listener': invalid['services']['redpanda']['command'][3] = 'internal://other:29092'
            else: invalid['services']['redpanda']['command'].append('--advertise-kafka-addr')
            with self.subTest(mutation=mutation), self.assertRaises(ValueError): N.advertised_config(invalid)

    def test_proxy_controls_are_private_and_ready_before_broker_and_application(self):
        self.life.prepare()
        self.assertEqual(len(self.life.state['pins']), 5)
        calls = self.life.calls
        broker = next(i for i, c in enumerate(calls) if '--force-recreate' in c)
        proxy_starts = [i for i, c in enumerate(calls) if 'python:3.12-alpine' in c]
        self.assertEqual(len(proxy_starts), 4)
        self.assertTrue(all(i < broker for i in proxy_starts))
        for i in proxy_starts:
            call = calls[i]
            self.assertNotIn('-p', call)
            self.assertNotIn('--privileged', call)
            self.assertIn('--config', call)
            self.assertIn('GITHUB_ACTIONS=true', call)
            self.assertIn('RUNNER_OS=Linux', call)
            self.assertIn('--read-only', call)
        probe = next(c for c in calls if N.PROBE in c)
        self.assertEqual(probe[probe.index('--network') + 1], 'host')
        self.assertEqual(len([c for c in calls if 'host' in c]), 1)
        for path in self.secret.iterdir(): self.assertEqual(path.stat().st_mode & 0o777, 0o600)
        self.assertEqual(N.published(config(), 'redpanda', 9092), '127.0.0.1:19092')
        invalid = config(); invalid['services']['redpanda']['ports'][0]['host_ip'] = '0.0.0.0'
        with self.assertRaises(ValueError): N.published(invalid, 'redpanda', 9092)

    def test_destructive_cleanup_rejects_replaced_pin_and_still_removes_other_owned_containers(self):
        self.life.prepare()
        changed = next(iter(self.life.rows))
        self.life.rows[changed]['State']['StartedAt'] = 'restarted'
        with mock.patch.object(N.subprocess, 'run', side_effect=self.life.docker_result):
            with self.assertRaisesRegex(ValueError, 'zero-residual'): self.life.stop()
        self.assertEqual(list(self.life.rows), [changed])
        self.assertEqual(len(self.life.state['pins']), 1)

    def test_cleanup_removes_complete_owned_set_and_requires_no_residuals(self):
        self.life.prepare()
        with mock.patch.object(N.subprocess, 'run', side_effect=self.life.docker_result): self.life.stop()
        self.assertEqual(self.life.rows, {})
        self.assertEqual(json.loads(self.life.state_file.read_text())['pins'], {})
        with mock.patch.object(N.subprocess, 'run', return_value=subprocess.CompletedProcess([], 125, stderr=b'daemon unavailable')):
            with self.assertRaisesRegex(ValueError, 'zero-residual'): self.life.stop()

    def test_private_manifest_distinguishes_original_source_from_output_and_never_overwrites(self):
        self.life.prepare()
        N.private_write(self.secret / 'manager-engine.json', {})
        env = dict(MINIO_ROOT_USER='infra-user', MINIO_ROOT_PASSWORD='infra-secret', MEILISEARCH_MASTER_KEY='index-secret',
                   INFRA_KAFKA_ADMIN_USERNAME='admin', INFRA_KAFKA_ADMIN_PASSWORD='kafka-secret',
                   ADDP_ONLINE_MANAGER_MINIO_PORT='9002', ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY='source-user',
                   ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY='source-secret')
        with mock.patch.dict(os.environ, env): self.life.manifest()
        path = self.secret / 'cleanup-native-fixture.json'
        report = json.loads(path.read_text())
        self.assertEqual(report['native']['minio']['endpoint'], '127.0.0.1:19000')
        self.assertEqual(report['native']['source_minio']['endpoint'], '127.0.0.1:9002')
        self.assertEqual(set(report['proxies']), {'object', 'index', 'dlq', 'capture'})
        self.assertEqual(path.stat().st_mode & 0o777, 0o600)
        with mock.patch.dict(os.environ, env), self.assertRaises(FileExistsError): self.life.manifest()

    def test_durable_start_intent_allows_exact_owned_cleanup_after_uncertain_start(self):
        def uncertain(args, **kwargs):
            value = Harness.command(self.life, args, **kwargs)
            if args[:2] == ['docker', 'run']: raise ValueError('response lost')
            return value
        with mock.patch.object(self.life, 'command', side_effect=uncertain):
            with self.assertRaises(ValueError): self.life.start(N.PROBE, ['image'])
        self.assertEqual(self.life.state['pins'][N.PROBE], None)
        with mock.patch.object(N.subprocess, 'run', side_effect=self.life.docker_result): self.life.stop()
        self.assertEqual(self.life.rows, {})

    def test_interrupted_writer_recovery_uses_owned_proxy_state_and_releases_once(self):
        self.life.prepare(); self.life.case = 'manager-late-writer'
        value = dict(run_id=self.life.run_id, key='11111111-1111-4111-8111-111111111111',
                     entered=1, forwarded=0, upstream_response=False, blocked_replays=0, released=False, expired=False)
        original = self.life.command; released = []
        def control(args, **kwargs):
            if args[-1] == 'state': return json.dumps(value)
            if args[-1] == 'release':
                released.append(json.loads(kwargs['input'])); value['released'] = True
                return json.dumps(value)
            return original(args, **kwargs)
        with mock.patch.object(self.life, 'command', side_effect=control):
            self.life.recover_writer(); self.life.recover_writer()
            self.assertEqual(released, [{'run_id': self.life.run_id, 'key': value['key']}])
            for field, replacement in (('run_id', 'other'), ('expired', True), ('key', 'not-uuid'),
                                       ('blocked_replays', True), ('forwarded', -1), ('upstream_response', True)):
                saved = value[field]; value[field] = replacement
                with self.subTest(field=field), self.assertRaises(ValueError): self.life.recover_writer()
                value[field] = saved
            expected = self.life.state['pins'][N.PREFIX + 'object-proxy']
            self.life.rows[expected['id']]['Config']['Env'][-1] = 'ADDP_ONLINE_TEST_RUN_ID=other'
            with self.assertRaisesRegex(ValueError, 'scope'): self.life.recover_writer()

    def test_unused_writer_transport_is_not_armed_or_released_by_recovery(self):
        self.life.prepare(); self.life.case = 'manager-late-writer'
        value = dict(run_id=self.life.run_id, key=None, forwarded=0, upstream_response=False, blocked_replays=0)
        original = self.life.command
        def control(args, **kwargs):
            if args[-1] == 'state': return json.dumps(value)
            self.assertNotEqual(args[-1], 'release')
            return original(args, **kwargs)
        with mock.patch.object(self.life, 'command', side_effect=control): self.life.recover_writer()
        self.life.case = 'manager-success'
        with self.assertRaisesRegex(ValueError, 'outside'): self.life.recover_writer()


class NativeEvidenceTest(unittest.TestCase):
    def report(self, case):
        report = dict(schema_version='addp.cleanup-native-effects/v1', suite='cleanup-native-effects', case=case,
            result='passed', control_cleanup='passed', hosted_destruction='required', run_id='native-test', tenant_id='7',
            engines=[11], tasks=[] if case.startswith('manager-') else [22], requests=['11111111-1111-4111-8111-111111111111'],
            executions=[] if case.startswith('manager-') else ['22222222-2222-4222-8222-222222222222'],
            artifact_id=33, generation_execution_id='33333333-3333-4333-8333-333333333333',
            principals={'manager': 'user-a', 'transfer': 'user-b', 'engine': 'user-c'}, checks=CONTRACT['native_case_checks'][case].copy(),
            entry='transfer_task_delete' if case == 'transfer-dlq-unknown' else 'engine_deletion_then_transfer_task_delete' if case == 'transfer-engine' else 'engine_deletion',
            producer_refusal_http_status=500, native_late_writer_acceptance='not_run',
            graph_native_write_acceptance='not_run', positive_reopen_acceptance='not_run')
        if case == 'manager-late-writer':
            report.update(native_late_writer_acceptance='passed', native_late_writer_scope='manager_glb_upload_after_owner_crash',
                          entry='engine_deletion_assessment', tasks=[22], executions=[report['generation_execution_id']])
        return report

    def environment(self, case):
        return dict(ADDP_ONLINE_NATIVE_CASE=case, ADDP_ONLINE_TEST_RUN_ID='native-test', ADDP_ONLINE_TEST_TENANT_ID='7')

    def test_each_case_requires_its_exact_contract_and_scope(self):
        for case in N.CASES:
            env = self.environment(case); report = self.report(case)
            G.validate_execution_report('cleanup-native', report, env)
            for field, value in (('case', 'all'), ('run_id', 'other'), ('result', 'failed'),
                                 ('checks', report['checks'][:-1]), ('checks', report['checks'] * 2),
                                 ('checks', report['checks'] + ['not_in_this_case']), ('control_cleanup', 'failed'),
                                 ('positive_reopen_acceptance', 'passed'), ('engines', []), ('engines', [True]),
                                 ('executions', ['not-a-uuid']), ('requests', []), ('principals', dict(manager='same', transfer='same', engine='other'))):
                if case == 'transfer-dlq-unknown' and field == 'requests': continue
                bad = {**report, field: value}
                with self.subTest(case=case, field=field), self.assertRaises(G.OnlineStageError):
                    G.validate_execution_report('cleanup-native', bad, env)
        bad = self.report('transfer-dlq-unknown'); bad['producer_refusal_http_status'] = 503
        with self.assertRaises(G.OnlineStageError): G.validate_execution_report('cleanup-native', bad, self.environment('transfer-dlq-unknown'))

    def test_late_writer_proof_cannot_be_promoted_from_other_cases_or_assessment_to_delete(self):
        for case in N.CASES:
            report = self.report(case)
            mutations = [('native_late_writer_scope', 'other_scope'), ('entry', 'engine_deletion')]
            if case == 'manager-late-writer':
                mutations += [('native_late_writer_acceptance', 'not_run'), ('executions', []), ('tasks', [])]
            else:
                mutations = [('native_late_writer_acceptance', 'passed'),
                             ('native_late_writer_scope', 'manager_glb_upload_after_owner_crash')]
            for field, value in mutations:
                with self.subTest(case=case, field=field), self.assertRaises(G.OnlineStageError):
                    G.validate_execution_report('cleanup-native', {**report, field: value}, self.environment(case))

    def test_native_archive_cannot_substitute_stdout_or_a_prior_case(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'cleanup-native-effects.json'; report = self.report('manager-success')
            env = self.environment('manager-success')
            with self.assertRaises(G.OnlineStageError): G.validate_execution_archive('cleanup-native', report, path.parent, env)
            path.write_text(json.dumps(report))
            G.validate_execution_archive('cleanup-native', report, path.parent, env)
            path.write_text(json.dumps(self.report('transfer-engine')))
            with self.assertRaises(G.OnlineStageError): G.validate_execution_archive('cleanup-native', report, path.parent, env)

    def test_native_preflight_requires_bounded_manifest_and_private_identities(self):
        with tempfile.TemporaryDirectory() as directory:
            secret = Path(directory) / 'addp-online-secret-native'; secret.mkdir(mode=0o700)
            env = dict(self.environment('manager-success'), RUNNER_TEMP=directory, ADDP_ONLINE_SECRET_DIR=str(secret),
                       ADDP_ONLINE_ARTIFACT_DIR=directory + '/artifacts', GITHUB_ACTIONS='true', RUNNER_OS='Linux',
                       ADDP_ONLINE_HOSTED='1', ADDP_ONLINE_HOST='1', ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN='source')
            env.update({'ADDP_ONLINE_CLEANUP_NATIVE_' + k + '_ACCESS_TOKEN': k for k in ('MANAGER', 'TRANSFER', 'ENGINE')})
            services = [P.Service(owner, f'http://127.0.0.1:{8200+i}') for i, (owner, _) in enumerate(G.SUITES['cleanup-native-effects'].services)]
            manifest = dict(schema='addp.cleanup-native-fixture/v1', run_id='native-test', case='manager-success',
                            descriptors={}, source_bucket='addp-online', proxies={k: {} for k in N.PROXIES}, native={})
            path = secret / 'cleanup-native-fixture.json'; N.private_write(path, manifest)
            with mock.patch.object(P.sys, 'platform', 'linux'), mock.patch.dict(os.environ, env, clear=True):
                P.validate_execution_environment('cleanup-native', ROOT, services)
                for key, value in (('ADDP_ONLINE_NATIVE_CASE', ''), ('ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN', ''),
                                   ('ADDP_ONLINE_CLEANUP_NATIVE_ENGINE_ACCESS_TOKEN', '')):
                    with mock.patch.dict(os.environ, {key: value}), self.assertRaises(P.PreflightError):
                        P.validate_execution_environment('cleanup-native', ROOT, services)
                manifest['case'] = 'manager-late-writer'; path.write_text(json.dumps(manifest))
                late_env = dict(ADDP_ONLINE_NATIVE_CASE='manager-late-writer', MANAGER_EXECUTION_LEASE_DURATION='30s',
                                MANAGER_EXECUTION_HEARTBEAT_INTERVAL='5s')
                with mock.patch.dict(os.environ, late_env):
                    P.validate_execution_environment('cleanup-native', ROOT, services)
                    for key in ('MANAGER_EXECUTION_LEASE_DURATION', 'MANAGER_EXECUTION_HEARTBEAT_INTERVAL'):
                        with mock.patch.dict(os.environ, {key: '2m'}), self.assertRaises(P.PreflightError):
                            P.validate_execution_environment('cleanup-native', ROOT, services)
                manifest['case'] = 'manager-success'
                manifest['run_id'] = 'other'; path.write_text(json.dumps(manifest))
                with self.assertRaises(P.PreflightError): P.validate_execution_environment('cleanup-native', ROOT, services)

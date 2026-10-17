import importlib
import os
import shutil
import subprocess
import unittest
from pathlib import Path

SUPPORT = importlib.import_module('scripts.test.online-hosted-opengauss-gate_test')
SCRIPT = Path(__file__).with_name('online-hosted-pg-authorization-gate.sh')


class HostedPGAuthorizationTest(unittest.TestCase):
    def setUp(self):
        self.host = SUPPORT.OnlineHostedOpenGaussGateTest()
        self.host.setUp()
        self.addCleanup(self.host.tearDown)
        self.host.prepare_run_fixture()
        shutil.copy2(SCRIPT, self.host.repository / 'scripts/test' / SCRIPT.name)
        self.host._write_repository_script('business/scripts/online-metric-postgres-fixture.sh', '''
            #!/usr/bin/env bash
            echo "pg-fixture:$1" >> "$ADDP_TEST_GATE_TRACE"
            if [ "$1" = start ]; then
              printf '{}\\n' > "$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE"
              [ "${ADDP_TEST_SOURCE_FAIL:-0}" != 1 ]
            fi
        ''')
        self.host._executable('make', '''
            #!/usr/bin/env bash
            [ -z "${ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN:-}" ] || exit 9
            echo "make:$*" >> "$ADDP_TEST_GATE_TRACE"
            [ "${ADDP_TEST_SUITE_FAIL:-0}" != 1 ]
        ''')
        self.host._executable('npm', '''
            #!/usr/bin/env bash
            [ "$*" = '--prefix console/frontend exec -- playwright install --with-deps chromium' ] || exit 2
            echo 'browser-install' >> "$ADDP_TEST_GATE_TRACE"
            [ "${ADDP_TEST_BROWSER_INSTALL_FAIL:-0}" != 1 ]
        ''')
        subprocess.run(['git', 'add', '.'], cwd=self.host.repository, check=True)
        subprocess.run(['git', 'commit', '-qm', 'PG fixture'], cwd=self.host.repository, check=True)

    def run_gate(self, check=False, **overrides):
        env = dict(os.environ, PATH=f'{self.host.bin}:{os.environ["PATH"]}', GITHUB_ACTIONS='true',
                   RUNNER_OS='Linux', RUNNER_TEMP=str(self.host.root), ADDP_ONLINE_HOST='1', ADDP_ONLINE_HOSTED='1',
                   ONLINE_SUITE_INPUT='pg-source-authorization', ADDP_ONLINE_ARTIFACT_DIR=str(self.host.artifacts),
                   ADDP_ONLINE_SECRET_DIR=str(self.host.secrets), ADDP_TEST_GATE_TRACE=str(self.host.trace),
                   ADDP_TEST_BACKGROUND_PIDS=str(self.host.background_pids))
        env.update(overrides)
        return subprocess.run(['bash', 'scripts/test/' + SCRIPT.name] + (['--check-only'] if check else []),
                              cwd=self.host.repository, env=env, capture_output=True, text=True, timeout=20)

    def test_check_only_never_starts_or_creates_credentials(self):
        result = self.run_gate(check=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(self.host.trace.exists())
        self.assertFalse(self.host.secrets.exists())

    def test_refuses_personal_environment_before_starting(self):
        result = self.run_gate(check=True, GITHUB_ACTIONS='false')
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.host.trace.exists())

    def test_source_waits_for_final_tcp_database_not_temporary_socket_readiness(self):
        source = SCRIPT.parents[2] / 'business/scripts/online-metric-postgres-fixture.sh'
        destination = self.host.repository / 'business/scripts' / source.name
        shutil.copy2(source, destination)
        self.host.secrets.mkdir(exist_ok=True)
        self.host._executable('sleep', '#!/usr/bin/env bash\nexit 0\n')
        self.host._executable('docker', '''
            #!/usr/bin/env python3
            import os, pathlib, sys
            args = sys.argv[1:]
            counter = pathlib.Path(os.environ['ADDP_TEST_DATABASE_PROBES'])
            if args[:2] == ['container', 'inspect']:
                sys.exit(1)
            if args[0] == 'run':
                sys.exit(0)
            if 'pg_isready' in args:
                sys.exit(0)  # The socket-only bootstrap server accepts connections.
            if args[0] == 'exec' and 'psql' in args:
                if 'SELECT current_database()' in args:
                    assert args[1:3] == ['-e', 'PGPASSWORD']
                    assert args[args.index('-h') + 1] == '127.0.0.1'
                    assert args[args.index('-d') + 1] == 'metric_fixture'
                    assert os.environ['PGPASSWORD'] and all(os.environ['PGPASSWORD'] not in value for value in args)
                    count = int(counter.read_text()) + 1 if counter.exists() else 1
                    counter.write_text(str(count))
                    if count == 1 or os.environ.get('ADDP_TEST_DATABASE_NEVER_READY') == '1':
                        sys.exit(1)
                    print('metric_fixture')
                    sys.exit(0)
                if not counter.exists() or int(counter.read_text()) < 2:
                    sys.exit(2)  # Reproduce seed-before-CREATE DATABASE.
                pathlib.Path(os.environ['ADDP_TEST_DATABASE_SEEDED']).write_text('seeded')
                sys.exit(0)
            sys.exit(3)
        ''')
        for never_ready in (False, True):
            with self.subTest(never_ready=never_ready):
                counter = self.host.root / 'database-probes'
                seed = self.host.root / 'database-seeded'
                descriptor = self.host.secrets / 'source.json'
                for path in (counter, seed, descriptor):
                    path.unlink(missing_ok=True)
                env = dict(os.environ, PATH=f'{self.host.bin}:{os.environ["PATH"]}', GITHUB_ACTIONS='true',
                    RUNNER_OS='Linux', ADDP_ONLINE_HOST='1', ADDP_ONLINE_HOSTED='1',
                    ADDP_ONLINE_SECRET_DIR=str(self.host.secrets), ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE=str(descriptor),
                    ADDP_TEST_DATABASE_PROBES=str(counter), ADDP_TEST_DATABASE_SEEDED=str(seed),
                    ADDP_TEST_DATABASE_NEVER_READY='1' if never_ready else '0')
                result = subprocess.run(['bash', str(destination), 'start'], cwd=self.host.repository,
                    env=env, capture_output=True, text=True, timeout=20)
                self.assertEqual(result.returncode == 0, not never_ready, result.stderr)
                self.assertEqual(seed.exists(), not never_ready)
                self.assertEqual(descriptor.exists(), not never_ready)
                if counter.exists():
                    self.assertEqual(int(counter.read_text()), 60 if never_ready else 2)

    def test_content_mutation_only_touches_the_owned_pg_fixture_and_requires_its_suite(self):
        source = SCRIPT.parents[2] / 'business/scripts/online-metric-postgres-fixture.sh'
        destination = self.host.repository / 'business/scripts' / source.name
        shutil.copy2(source, destination)
        self.host._executable('docker', '''
            #!/usr/bin/env python3
            import os, pathlib, sys
            args = sys.argv[1:]
            if args[0] == 'inspect':
                assert args[-1] == 'addp-metric-online-disposable'
                print(os.environ.get('ADDP_TEST_CONTENT_OWNER', 'metric'))
            elif args[:3] == ['exec', '-i', 'addp-metric-online-disposable']:
                assert args[args.index('-d') + 1] == 'metric_fixture'
                sql = sys.stdin.read()
                assert 'LOCK TABLE public.metric_people' in sql
                assert 'BEGIN;' in sql and 'COMMIT;' in sql and 'ON_ERROR_STOP=1' in args
                assert 'INSERT INTO public.metric_people' in sql and 'DELETE FROM public.metric_people' in sql
                assert not any(term in sql for term in ('metric_events', 'GRANT ', 'CREATE ', 'DROP ', 'meta.'))
                pathlib.Path(os.environ['ADDP_TEST_CONTENT_EXECUTED']).write_text('owned-source')
                print('pg-content:changed' if 'change=1' in args else 'pg-content:baseline')
            else:
                sys.exit(2)
        ''')
        marker = self.host.root / 'content-executed'
        env = dict(os.environ, PATH=f'{self.host.bin}:{os.environ["PATH"]}', GITHUB_ACTIONS='true',
            RUNNER_OS='Linux', ADDP_ONLINE_HOST='1', ADDP_ONLINE_HOSTED='1', ADDP_ONLINE_TEST='1',
            POSTGRES_DB='addp_online', ONLINE_SUITE='pg-source-authorization', ADDP_TEST_CONTENT_EXECUTED=str(marker))
        cases = [('content-change', {}, 'pg-content:changed'), ('content-reset', {}, 'pg-content:baseline'),
                 ('content-change', {'ONLINE_SUITE': 'other-suite'}, None),
                 ('content-reset', {'POSTGRES_DB': 'personal'}, None),
                 ('content-change', {'ADDP_TEST_CONTENT_OWNER': 'personal'}, None),
                 ('content-change', {'GITHUB_ACTIONS': 'false'}, None)]
        for action, overrides, receipt in cases:
            with self.subTest(action=action, overrides=overrides):
                marker.unlink(missing_ok=True)
                result = subprocess.run(['bash', str(destination), action], cwd=self.host.repository,
                    env=dict(env, **overrides), capture_output=True, text=True, timeout=20)
                self.assertEqual(result.returncode == 0, receipt is not None, result.stderr)
                self.assertEqual(marker.exists(), receipt is not None)
                if receipt:
                    self.assertEqual(result.stdout.strip(), receipt)

    def test_lifecycle_and_source_failure_and_suite_failure_clean_owned_resources(self):
        # Each call needs a new secret directory; the lifecycle removes it on exit.
        for overrides in ({}, {'ADDP_TEST_SUITE_FAIL': '1'}, {'ADDP_TEST_SOURCE_FAIL': '1'},
                          {'ADDP_TEST_BROWSER_INSTALL_FAIL': '1'}):
            with self.subTest(overrides=overrides):
                result = self.run_gate(**overrides)
                self.assertEqual(result.returncode == 0, not bool(overrides), result.stderr)
                trace = self.host.trace.read_text()
                self.assertIn('pg-fixture:stop', trace)
                self.assertIn('infra-down', trace)
                self.assertFalse(self.host.secrets.exists())
                self.assertIn('infra_cleanup=zero_residuals', (self.host.artifacts / 'summary.txt').read_text())
                if not overrides:
                    self.assertIn('start:-meta', trace)
                    self.assertIn('start:-manager', trace)
                    self.assertIn('start:-catalog', trace)
                    self.assertIn('browser-install', trace)
                    self.assertLess(trace.index('browser-install'), trace.index('make:test-online'))
                    self.assertIn('make:test-online ONLINE_SUITE=pg-source-authorization', trace)
                    self.assertLess(trace.index('application-stop'), trace.index('pg-fixture:stop'))
                    self.assertNotIn('start:-develop', trace)
                self.host.trace.unlink()


if __name__ == '__main__':
    unittest.main()

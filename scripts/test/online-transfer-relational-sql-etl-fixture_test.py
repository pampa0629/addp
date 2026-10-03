import json
import os
import shutil
import stat
import subprocess
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).parents[2] / 'business/scripts/online-transfer-relational-sql-etl-fixture.sh'


class OnlineTransferRelationalSQLETLFixtureTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='addp-online-transfer-sql-etl-')
        self.root = Path(self.temporary.name)
        self.bin = self.root / 'bin'
        self.postgres_state = self.root / 'postgres-running'
        self.log = self.root / 'fixture.log'
        self.secrets = self.root / 'addp-online-secret-test'
        (self.root / 'business/scripts').mkdir(parents=True)
        self.bin.mkdir()
        self.secrets.mkdir(mode=0o700)
        shutil.copy2(SCRIPT, self.root / 'business/scripts/online-transfer-relational-sql-etl-fixture.sh')
        self._executable('uname', '#!/bin/bash\n[ "$1" != -s ] || { echo "${ADDP_TEST_OS:-Linux}"; exit; }\necho x86_64\n')
        self._executable('docker', '''#!/bin/bash
printf 'docker:%s:%s\n' "$1" "${2:-}" >> "$ADDP_TEST_FIXTURE_LOG"
case "$1" in
  container)
    [ -f "$ADDP_TEST_POSTGRES_STATE" ]
    ;;
  inspect)
    [ -f "$ADDP_TEST_POSTGRES_STATE" ] || exit 1
    case "$*" in
      *com.addp.online-fixture*) cat "$ADDP_TEST_POSTGRES_STATE" ;;
      *) echo true ;;
    esac
    ;;
  run)
    printf '%s\n' "$*" >> "$ADDP_TEST_FIXTURE_LOG"
    echo transfer-relational-sql-etl > "$ADDP_TEST_POSTGRES_STATE"
    ;;
  ps)
    [ "${ADDP_TEST_VERIFY_FAIL:-0}" != 1 ] || exit 1
    [ ! -f "$ADDP_TEST_POSTGRES_STATE" ] || echo fixture-container
    ;;
  rm)
    [ "${ADDP_TEST_REMOVE_FAIL:-0}" = 1 ] || rm -f "$ADDP_TEST_POSTGRES_STATE"
    ;;
  exec)
    [ -f "$ADDP_TEST_POSTGRES_STATE" ] || exit 1
    case " $* " in
      *pg_isready*) exit 0 ;;
      *" -Atc "*|*" -c "*) ;;
      *) input=$(cat)
         printf 'stdin:%s\n' "$input" >> "$ADDP_TEST_FIXTURE_LOG"
         if [[ "$input" == *"CREATE ROLE"* ]] && [ "${ADDP_TEST_SEED_FAIL:-0}" = 1 ]; then
           echo "SECRET_MARKER: $*" >&2
           exit 1
         fi ;;
    esac
    case " $* " in
      *"numeric_precision"*) echo '8|2' ;;
      *"generated_label"*) echo "${ADDP_TEST_NATIVE_VALUES:-5|1411.50|3|2|5}" ;;
      *"WHERE area"*) echo '5|1411.50|3|2' ;;
      *"CONCAT_WS"*) echo '2|3|4|701.00' ;;
      *"string_agg(column_name"*) echo 'id,region,amount' ;;
      *"COUNT(*) FROM public.addp_online_transfer_sql_etl_source"*) echo 5 ;;
    esac
    ;;
esac
''')
        self.environment = dict(os.environ, PATH=str(self.bin) + os.pathsep + os.environ['PATH'],
                                GITHUB_ACTIONS='true', RUNNER_OS='Linux', RUNNER_TEMP=str(self.root),
                                ADDP_ONLINE_HOST='1', ADDP_ONLINE_HOSTED='1',
                                ADDP_ONLINE_SECRET_DIR=str(self.secrets),
                                ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE=str(self.secrets / 'transfer-engine.json'),
                                ADDP_TEST_POSTGRES_STATE=str(self.postgres_state), ADDP_TEST_FIXTURE_LOG=str(self.log))

    def tearDown(self):
        self.temporary.cleanup()

    def _executable(self, name, content):
        path = self.bin / name
        path.write_text(content)
        path.chmod(0o755)

    def run_fixture(self, action, **overrides):
        environment = dict(self.environment)
        environment.update(overrides)
        return subprocess.run(['bash', 'business/scripts/online-transfer-relational-sql-etl-fixture.sh', action],
                              cwd=self.root, env=environment, capture_output=True, text=True, timeout=20)

    def test_owns_projection_filter_verification_and_cleanup(self):
        for action in ('start', 'status', 'verify', 'stop'):
            result = self.run_fixture(action)
            self.assertEqual(result.returncode, 0, result.stderr)
        commands = self.log.read_text()
        self.assertIn('CREATE TABLE public.addp_online_transfer_sql_etl_source', commands)
        self.assertIn("(3, 'north', 'active', 300.25, 'hidden-3')", commands)
        self.assertIn('GRANT SELECT ON public.addp_online_transfer_sql_etl_source', commands)
        self.assertIn('GRANT USAGE, CREATE ON SCHEMA public TO transfer_writer', commands)
        self.assertNotIn('GRANT ALL', commands)
        self.assertIn('--tmpfs /var/lib/postgresql/data', commands)
        self.assertNotIn('business-postgres', commands)
        self.assertFalse(self.postgres_state.exists())
        descriptor = self.secrets / 'transfer-engine.json'
        self.assertEqual(stat.S_IMODE(descriptor.stat().st_mode), 0o600)
        engine = json.loads(descriptor.read_text())
        self.assertEqual(engine['connection_info']['user'], 'transfer_writer')
        self.assertEqual(engine['connection_info']['database'], 'transfer_fixture')
        self.assertEqual(engine['connection_info']['host'], '127.0.0.1')
        self.assertNotIn(engine['connection_info']['password'], commands)

    def test_requires_github_hosted_linux_before_docker(self):
        for flags in ({'ADDP_ONLINE_HOST': '0'}, {'ADDP_ONLINE_HOSTED': '0'},
                      {'GITHUB_ACTIONS': 'false'}, {'RUNNER_OS': 'macOS'},
                      {'ADDP_ONLINE_OWNER_MANAGED': '1'}, {'ADDP_TEST_OS': 'Darwin'}):
            result = self.run_fixture('start', **flags)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn('GitHub Hosted Linux', result.stderr)
            self.assertFalse(self.log.exists())

    def test_refuses_existing_source_and_foreign_ownership(self):
        self.postgres_state.write_text('foreign-owner')
        self.assertNotEqual(self.run_fixture('start').returncode, 0)
        for action in ('stop', 'verify', 'status'):
            result = self.run_fixture(action)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn('ownership mismatch', result.stderr)
            self.assertTrue(self.postgres_state.exists())

    def test_rejects_native_rows_that_do_not_prove_replace_and_generation(self):
        self.assertEqual(self.run_fixture('start').returncode, 0)
        result = self.run_fixture('verify', ADDP_TEST_NATIVE_VALUES='5|1411.50|5|0|5')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('native target does not prove', result.stderr)

    def test_cleanup_fails_if_container_remains(self):
        self.assertEqual(self.run_fixture('start').returncode, 0)
        result = self.run_fixture('stop', ADDP_TEST_REMOVE_FAIL='1')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('container remains', result.stderr)
        self.assertTrue(self.postgres_state.exists())

    def test_cleanup_cannot_claim_zero_residuals_when_docker_verification_fails(self):
        self.assertEqual(self.run_fixture('start').returncode, 0)
        result = self.run_fixture('stop', ADDP_TEST_VERIFY_FAIL='1')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('cannot verify container cleanup', result.stderr)

    def test_seed_error_keeps_credentials_out_of_output(self):
        result = self.run_fixture('start', ADDP_TEST_SEED_FAIL='1')
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn('SECRET_MARKER', result.stdout + result.stderr)
        self.assertEqual(self.run_fixture('stop').returncode, 0)
        self.assertFalse(self.postgres_state.exists())


if __name__ == '__main__':
    unittest.main()

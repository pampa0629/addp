#!/usr/bin/env python3
"""Business lifecycle contract tests; native dual-architecture T5 covers the kernel."""
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
ENTRY = ROOT / 'business/gaussdb/entrypoint.sh'
SPEC = importlib.util.spec_from_file_location('business_gaussdb_image', ROOT / 'business/gaussdb/image.py')
IMAGE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(IMAGE)


class BusinessGaussDBTest(unittest.TestCase):
    def run_shell(self, script, extra=None):
        env = dict(os.environ, ENTRY=str(ENTRY), GAUSSDB_PASSWORD='Admin9@123456',
                   GAUSSDB_APP_PASSWORD="App9@'\\:$12345", GAUSSDB_USER='business', GAUSSDB_VERSION='V2.0-10.0.0')
        env.update(extra or {})
        return subprocess.run(['bash', '-ec', 'source "$ENTRY"; ' + script], env=env,
                              capture_output=True, text=True)

    def test_config_rejects_missing_equal_multiline_and_overlong_passwords(self):
        for value in ('', 'short', 'a\nbbbbbbbbb', 'a\rbbbbbbbbbb', 'a' * 33, 'Admin9@123456'):
            with self.subTest(value=repr(value)):
                result = self.run_shell('validate_config', {'GAUSSDB_APP_PASSWORD': value})
                self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.run_shell('validate_config').returncode, 0)

    def test_user_rejects_privileged_names_and_sql_fragments(self):
        for value in ('omm', 'postgres', 'pg_catalog', 'a"; SELECT 1;', 'UPPER', ''):
            result = self.run_shell('GAUSSDB_USER="$TEST_USER"; validate_config', {'TEST_USER': value})
            # Empty config deliberately selects the canonical ordinary account.
            self.assertEqual(result.returncode == 0, value == '')

    def test_sql_literal_keeps_quotes_and_backslashes_literal(self):
        result = self.run_shell('sql_literal "$GAUSSDB_APP_PASSWORD"')
        self.assertEqual(result.stdout, "E'App9@''\\\\:$12345'")

    def test_native_schema_is_created_once_before_idempotent_table_seeds(self):
        for fresh in ('true', 'false'):
            result = self.run_shell('fresh="$TEST_FRESH"; business_sql "$TEST_SQL"',
                                    {'TEST_FRESH': fresh, 'TEST_SQL': str(ROOT / 'business/gaussdb/init.sql')})
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual('CREATE SCHEMA ' in result.stdout, fresh == 'true')
            self.assertNotIn('CREATE SCHEMA IF NOT EXISTS', result.stdout)
            self.assertIn('SET search_path=addp_business;', result.stdout)
            self.assertIn('BEGIN;', result.stdout)
            self.assertIn('COMMIT;', result.stdout)

    def test_empty_volume_is_fresh_but_partial_data_is_never_reinitialized(self):
        with tempfile.TemporaryDirectory() as directory:
            extra = {'TEST_DATA': directory}
            run = lambda: self.run_shell('DATA_DIR="$TEST_DATA"; validate_config; check_volume; echo "$fresh"', extra)
            self.assertEqual(run().stdout.strip(), 'true')
            db = Path(directory) / 'db'
            db.mkdir()
            (db / 'partial').write_text('do not erase')
            self.assertNotEqual(run().returncode, 0)
            self.assertEqual((db / 'partial').read_text(), 'do not erase')
            (db / 'PG_VERSION').write_text('9.2')
            self.assertNotEqual(run().returncode, 0)
            marker = Path(directory) / 'business-ready'
            marker.write_text('V2.0-10.0.0|business|business\n')
            self.assertEqual(run().stdout.strip(), 'false')
            for marker_value in ('other|business|business', 'V2.0-10.0.0|business|other'):
                marker.write_text(marker_value)
                self.assertNotEqual(run().returncode, 0)

    def test_native_pipeline_keeps_password_and_sql_separate_and_preserves_failure(self):
        for status in (0, 3):
            with self.subTest(status=status), tempfile.TemporaryDirectory() as directory:
                result = self.run_shell('''
gsql() {
    IFS= read -r supplied_password
    [ "$supplied_password" = "$GAUSSDB_APP_PASSWORD" ] || return 98
    [[ " $* " = *" -2 "* && " $* " = *" -f /dev/fd/3 "* ]] || return 96
    # -q does not suppress this kernel's -f total-time feedback. Query output
    # must use the native -o channel; feedback cannot enter the caller's data.
    [[ " $* " = *" -q "* ]] || return 94
    [[ " $* " = *" -o /dev/fd/4 "* ]] || return 93
    [ "$(cat /dev/fd/3)" = "SELECT 1;" ] || return 95
    [[ "$*" != *"$GAUSSDB_APP_PASSWORD"* ]] || return 99
    # The fixed native gsql has no PostgreSQL psql -w/--no-password option.
    for argument in "$@"; do
        [[ "$argument" != -w && "$argument" != --no-password ]] || return 97
    done
    if [ "$TEST_STATUS" = 0 ]; then printf '1\\n' >/dev/fd/4; fi
    printf 'total time: 8 ms\\n'
    return "$TEST_STATUS"
}
status=0
observed=$(printf 'SELECT 1;\\n' | tcp_sql business "$GAUSSDB_APP_PASSWORD" business) || status=$?
test "$status" = "$TEST_STATUS"
if [ "$status" = 0 ]; then test "$observed" = 1; else test -z "$observed"; fi
''', {'TEST_STATUS': str(status)})
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertNotIn("App9@", result.stdout + result.stderr)

    def test_health_retains_unexpected_native_output_and_query_errors(self):
        for output, status, expected in [('1', 0, 0), ('native feedback\\n1', 0, 1), ('', 3, 3)]:
            result = self.run_shell('''
tcp_sql() { cat >/dev/null; printf '%b' "$TEST_OUTPUT"; return "$TEST_STATUS"; }
health_sql
''', {'TEST_OUTPUT': output, 'TEST_STATUS': str(status)})
            self.assertEqual(result.returncode, expected, result.stderr)
            if status == 0 and expected:
                self.assertIn('native feedback\n1', result.stderr)

    def test_stop_preserves_volume_and_rejects_foreign_container(self):
        with tempfile.TemporaryDirectory() as directory:
            docker = Path(directory) / 'docker'
            log = Path(directory) / 'commands'
            docker.write_text('''#!/usr/bin/env bash
printf '%s\\n' "$*" >> "$TEST_LOG"
case "$1" in
 container) exit 0 ;;
 inspect) printf '%s\\n' "$TEST_OWNER" ;;
 compose) exit 0 ;;
 *) exit 1 ;;
esac
''')
            docker.chmod(0o755)
            env = dict(os.environ, PATH=directory+os.pathsep+os.environ['PATH'], TEST_LOG=str(log),
                       TEST_OWNER=f'business|gaussdb|{ROOT}/business')
            args = ['bash', str(ROOT / 'business/scripts/stop.sh'), '-gaussdb']
            result = subprocess.run(args, env=env, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn('stop gaussdb', log.read_text())
            self.assertNotIn('volume rm', log.read_text())
            log.write_text('')
            env['TEST_OWNER'] = 'other|gaussdb|/other'
            result = subprocess.run(args, env=env, capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertNotIn('compose ', log.read_text())

    def test_compose_has_explicit_profile_loopback_and_persistent_volume(self):
        result = subprocess.run(['docker', 'compose', '--env-file', '/dev/null', '-f', str(ROOT / 'business/docker-compose.yml'),
                                 '--profile', 'gaussdb', 'config', '--format', 'json'], capture_output=True, text=True, check=True)
        service = json.loads(result.stdout)['services']['gaussdb']
        self.assertEqual(service['profiles'], ['gaussdb'])
        self.assertEqual(service['image'], 'addp/gaussdb:developer-10.0.0')
        self.assertEqual(service['ports'][0]['host_ip'], '127.0.0.1')
        self.assertEqual(service['ports'][0]['published'], '5437')
        self.assertEqual(service['volumes'][0]['source'], 'gaussdb_data')
        self.assertEqual(service['volumes'][0]['target'], '/data')
        self.assertIn('--health', service['healthcheck']['test'])
        result = subprocess.run(['docker', 'compose', '--env-file', '/dev/null', '-f', str(ROOT / 'business/docker-compose.yml'),
                                 'config', '--services'], capture_output=True, text=True, check=True)
        self.assertNotIn('gaussdb', result.stdout.splitlines())


if __name__ == '__main__':
    unittest.main()

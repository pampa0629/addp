import json
import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


class SecurityOwnerFixtureTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.bin = self.root / 'bin'; self.bin.mkdir()
        self.secrets = self.root / 'secrets'; self.secrets.mkdir(mode=0o700)
        self.state = self.root / 'state'; self.state.mkdir()
        self.sql = self.root / 'postgres.sql'
        self.trace = self.root / 'trace'
        self.passwords = self.root / 'passwords'; self.passwords.mkdir(mode=0o700)
        self.seed = self.root / 'business/mysql'; self.seed.mkdir(parents=True)
        fixture = self.root / 'business/scripts'; fixture.mkdir()
        shutil.copy2(ROOT / 'business/scripts/online-security-owner-fixture.sh', fixture)
        (self.seed / 'test-data.sh').write_text('''#!/usr/bin/env bash
            [ "$MYSQL_CONTAINER" = addp-security-online-mysql ]
            [ "$MYSQL_DATABASE" = security_fixture ]
            [ "$MYSQL_USER" = root ]
            [ "${ADDP_TEST_SEED_FAIL:-0}" != 1 ]
        ''')
        self.executable('uname', '#!/usr/bin/env bash\n[ "$1" = -m ] && echo x86_64 || echo Linux\n')
        self.executable('sleep', '#!/usr/bin/env bash\nexit 0\n')
        self.executable('docker', '''#!/usr/bin/env bash
            set -euo pipefail
            case "$1" in
              info) exit 0 ;;
              container) [ -f "$ADDP_TEST_STATE/$3" ]; exit ;;
              inspect) echo "${ADDP_TEST_OWNER:-security}"; exit ;;
              run)
                previous=
                for item in "$@"; do
                  if [ "$previous" = --name ]; then
                    touch "$ADDP_TEST_STATE/$item"
                    if [ "$item" = addp-security-online-postgres ]; then
                      printf '%s' "$POSTGRES_PASSWORD" > "$ADDP_TEST_PASSWORDS/postgres-root"
                    else
                      printf '%s' "$MYSQL_ROOT_PASSWORD" > "$ADDP_TEST_PASSWORDS/mysql-root"
                    fi
                    echo "run:$item" >> "$ADDP_TEST_TRACE"
                    [ "${ADDP_TEST_RUN_FAIL:-}" != "$item" ] || exit 1
                  fi
                  previous=$item
                done
                exit 0 ;;
              rm)
                echo "remove:$3" >> "$ADDP_TEST_TRACE"
                [ "${ADDP_TEST_RETAIN:-0}" = 1 ] || rm -f "$ADDP_TEST_STATE/$3"
                exit 0 ;;
              ps) ls "$ADDP_TEST_STATE"; exit ;;
              exec)
                if [[ "$*" == *pg_isready* ]]; then exit 0; fi
                if [[ "$*" == *psql* ]]; then cat > "$ADDP_TEST_SQL"; exit 0; fi
                if [[ "$*" == *'SELECT 1'* ]]; then exit 0; fi
                cat >/dev/null
                exit 0 ;;
            esac
            exit 2
        ''')
        self.env = dict(os.environ, PATH=f'{self.bin}:{os.environ["PATH"]}', GITHUB_ACTIONS='true', RUNNER_OS='Linux',
                        ADDP_ONLINE_HOSTED='1', ADDP_ONLINE_HOST='1', ADDP_ONLINE_SECRET_DIR=str(self.secrets),
                        ADDP_TEST_STATE=str(self.state), ADDP_TEST_PASSWORDS=str(self.passwords), ADDP_TEST_SQL=str(self.sql), ADDP_TEST_TRACE=str(self.trace))

    def executable(self, name, content):
        path = self.bin / name
        path.write_text(content); path.chmod(0o755)

    def run_fixture(self, action, **overrides):
        return subprocess.run(['bash', 'business/scripts/online-security-owner-fixture.sh', action], cwd=self.root,
                              env=dict(self.env, **overrides), capture_output=True, text=True, timeout=30)

    def test_creates_spatial_fixture_and_separate_owner_only_descriptors(self):
        result = self.run_fixture('start')
        self.assertEqual(result.returncode, 0, result.stderr)
        sql = self.sql.read_text()
        for fragment in ('GRANT CONNECT, CREATE ON DATABASE security_fixture TO security_writer', 'CREATE EXTENSION postgis', 'CREATE TABLE addp_online_security.exemption_source', 'GRANT SELECT ON addp_online_security.exemption_source', 'location_point geometry(Point, 4326)', 'NULL::text', '张三abc',
                         'ST_SetSRID(ST_MakePoint(100 + id, 20 + id), 4326)', 'GRANT SELECT ON addp_online_security.spatial_algorithm_source',
                         'spatial_algorithm_transfer OWNER TO security_writer', 'mysql_email_transfer OWNER TO security_writer'):
            self.assertIn(fragment, sql)
        for kind, user, port in (('postgresql', 'security_writer', 55433), ('mysql', 'security_reader', 53306)):
            name = 'postgres' if kind == 'postgresql' else 'mysql'
            path = self.secrets / f'security-{name}.json'
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            data = json.loads(path.read_text())
            self.assertEqual(data['engine_type'], kind)
            self.assertEqual(data['connection_info']['user'], user)
            self.assertEqual(data['connection_info']['port'], port)
            self.assertNotIn(data['connection_info']['password'], result.stdout + result.stderr)
            self.assertNotEqual(data['connection_info']['password'], (self.passwords / f'{name}-root').read_text())
        for path in self.secrets.iterdir():
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
        stopped = self.run_fixture('stop')
        self.assertEqual(stopped.returncode, 0, stopped.stderr)
        self.assertFalse(list(self.state.iterdir()))

    def test_rejects_personal_or_existing_resources_before_creation(self):
        result = self.run_fixture('start', GITHUB_ACTIONS='false')
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.trace.exists())
        (self.state / 'addp-security-online-mysql').touch()
        result = self.run_fixture('start')
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.trace.exists())

    def test_rejects_unknown_retained_fixture_before_creation(self):
        (self.state / 'retained-security-fixture').touch()
        result = self.run_fixture('start')
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.trace.exists())
        self.assertIn('refusing existing fixture resources', result.stderr)

    def test_checks_all_container_owners_before_deleting_and_reports_residual(self):
        for name in ('addp-security-online-postgres', 'addp-security-online-mysql'):
            (self.state / name).touch()
        result = self.run_fixture('stop', ADDP_TEST_OWNER='other')
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.trace.exists())
        self.assertEqual(len(list(self.state.iterdir())), 2)
        result = self.run_fixture('stop', ADDP_TEST_RETAIN='1')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('remains after cleanup', result.stderr)

    def test_partial_start_or_seed_failure_can_be_cleaned_by_hosted_owner(self):
        for values in ({'ADDP_TEST_RUN_FAIL': 'addp-security-online-mysql'}, {'ADDP_TEST_SEED_FAIL': '1'}):
            with self.subTest(values=values):
                for path in self.secrets.iterdir(): path.unlink()
                result = self.run_fixture('start', **values)
                self.assertNotEqual(result.returncode, 0)
                stopped = self.run_fixture('stop')
                self.assertEqual(stopped.returncode, 0, stopped.stderr)
                self.assertFalse(list(self.state.iterdir()))


if __name__ == '__main__':
    unittest.main()

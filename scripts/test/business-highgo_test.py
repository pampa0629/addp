#!/usr/bin/env python3
"""HighGo 初始化、失败清理与 Business profile 回归，不启动数据库。"""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
ENTRYPOINT = ROOT / 'business/highgo/entrypoint.sh'


class HighGoInitializationTest(unittest.TestCase):
    def run_bash(self, script, password='Configured@123456', extra=None):
        env = dict(os.environ, POSTGRES_PASSWORD=password, ENTRYPOINT=str(ENTRYPOINT))
        env.update(extra or {})
        return subprocess.run(['bash', '-c', 'set -e; source "$ENTRYPOINT"; addp_highgo_install_initializers; ' + script],
                              env=env, text=True, capture_output=True)

    def test_empty_and_multiline_password_rejected(self):
        for password in ('', 'a\nb', 'a\rb'):
            with self.subTest(password=repr(password)):
                result = self.run_bash('addp_highgo_validate_password', password)
                self.assertNotEqual(result.returncode, 0)

    def test_password_is_literal_and_file_is_removed_on_success_or_failure(self):
        for code in (0, 17):
            with self.subTest(code=code), tempfile.TemporaryDirectory() as directory:
                password = "Configured'$@123456\\"
                result = self.run_bash('''
                    export PGDATA="$TEST_DIR/data" TMPDIR="$TEST_DIR"
                    mkdir "$PGDATA"
                    initdb() {
                        test "$2 $3 $4 $5" = '-A sm3 -E UTF8'
                        file="${1#--pwfile=}"
                        export PWFILE="$file"
                        python3 -c 'import os,stat,pathlib; p=pathlib.Path(os.environ["PWFILE"]); assert stat.S_IMODE(p.stat().st_mode)==0o600; assert p.read_text()==(os.environ["POSTGRES_PASSWORD"]+"\\n")*3'
                        printf '%s' "$file" > "$TEST_DIR/path"
                        return "$EXIT_CODE"
                    }
                    cp() { printf 'certs' > "$TEST_DIR/copied"; touch "$PGDATA/server.key"; }
                    status=0
                    docker_init_database_dir || status=$?
                    test "$status" = "$EXIT_CODE"
                    test ! -e "$(cat "$TEST_DIR/path")"
                    if [ "$status" = 0 ]; then test -e "$TEST_DIR/copied"; else test ! -e "$TEST_DIR/copied"; fi
                ''', password, {'TEST_DIR': directory, 'EXIT_CODE': str(code)})
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertNotIn(password, result.stdout + result.stderr)

    def test_hba_requires_sm3_and_tcp_tls(self):
        with tempfile.TemporaryDirectory() as directory:
            result = self.run_bash('export PGDATA="$TEST_DIR"; pg_setup_hba_conf', extra={'TEST_DIR': directory})
            self.assertEqual(result.returncode, 0, result.stderr)
            lines = [line.split() for line in (Path(directory) / 'pg_hba.conf').read_text().splitlines()]
            self.assertEqual(lines, [['local', 'all', 'all', 'sm3'],
                                     ['hostssl', 'all', 'all', '0.0.0.0/0', 'sm3'],
                                     ['hostssl', 'all', 'all', '::/0', 'sm3']])

    def test_certificate_copy_failure_is_not_hidden(self):
        with tempfile.TemporaryDirectory() as directory:
            result = self.run_bash('''
                export PGDATA="$TEST_DIR" TMPDIR="$TEST_DIR"
                touch "$PGDATA/server.key"
                initdb() { return 0; }
                cp() { return 9; }
                docker_init_database_dir
            ''', extra={'TEST_DIR': directory})
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual([p.name for p in Path(directory).iterdir()], ['server.key'])

    def test_compose_owns_isolated_profile_and_persistent_data(self):
        result = subprocess.run(['docker', 'compose', '--env-file', '/dev/null', '-f', str(ROOT / 'business/docker-compose.yml'),
                                 '--profile', 'highgo', 'config', '--format', 'json'],
                                text=True, capture_output=True, check=True)
        config = json.loads(result.stdout)
        service = config['services']['highgo']
        self.assertEqual(service['profiles'], ['highgo'])
        self.assertEqual(service['container_name'], 'business-highgo')
        self.assertEqual(service['image'], 'qiuchenjun/hgdb-see:4.5.10.3@sha256:e4dd0ac4877c3a8818ec4cef68108be8d54c410782a6248a01257ea66dd49725')
        self.assertEqual(service['ports'][0]['host_ip'], '127.0.0.1')
        self.assertEqual(service['ports'][0]['target'], 5866)
        mounts = {volume['target']: volume for volume in service['volumes']}
        self.assertEqual(mounts['/home/highgo/hgdb']['source'], 'highgo_data')
        self.assertTrue(mounts['/docker-entrypoint-initdb.d']['read_only'])
        self.assertTrue(mounts['/docker-entrypoint-initdb.d']['source'].endswith('/business/highgo/init'))
        self.assertEqual(service['environment']['POSTGRES_DB'], 'highgo')
        self.assertIn('PGSSLMODE=require', service['healthcheck']['test'][1])
        self.assertIn('password_encryption=sm3', service['command'])
        result = subprocess.run(['docker', 'compose', '--env-file', '/dev/null', '-f', str(ROOT / 'business/docker-compose.yml'),
                                 'config', '--services'], text=True, capture_output=True, check=True)
        self.assertNotIn('highgo', result.stdout.splitlines())

    def test_stop_preserves_container_and_volume_and_refuses_foreign_owner(self):
        with tempfile.TemporaryDirectory() as directory:
            mock = Path(directory) / 'docker'
            log = Path(directory) / 'commands'
            mock.write_text('''#!/usr/bin/env bash
printf '%s\\n' "$*" >> "$TEST_LOG"
case "$1" in
 container) exit 0 ;;
 inspect) printf '%s\\n' "$TEST_OWNER" ;;
 compose) exit 0 ;;
 *) exit 1 ;;
esac
''')
            mock.chmod(0o755)
            env = dict(os.environ, PATH=directory+os.pathsep+os.environ['PATH'], TEST_LOG=str(log),
                       TEST_OWNER=f'business|highgo|{ROOT}/business')
            args = ['bash', str(ROOT / 'business/scripts/stop.sh'), '-highgo']
            result = subprocess.run(args, env=env, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn('stop highgo', log.read_text())
            self.assertNotIn('rm ', log.read_text())
            log.write_text('')
            env['TEST_OWNER'] = 'another|highgo|/another'
            result = subprocess.run(args, env=env, capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse(any(line.startswith('compose ') for line in log.read_text().splitlines()))


if __name__ == '__main__':
    unittest.main()

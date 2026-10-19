from importlib import import_module
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

CONFIG = import_module('business.starrocks.config')
ROOT = CONFIG.ROOT
ENTRY = ROOT / 'business/starrocks/entrypoint.sh'

class BusinessStarRocksTest(unittest.TestCase):
    def shell(self, code, *args, **kwargs):
        env = dict(os.environ, STARROCKS_USER='business_reader', STARROCKS_PASSWORD='Admin1234@abcdef', STARROCKS_APP_PASSWORD='Reader1234@abcdef')
        env.update(kwargs.pop('env', {}))
        return subprocess.run(['bash', '-c', 'source "$1"; ' + code, 'test', str(ENTRY), *args], env=env, capture_output=True, text=True, **kwargs)

    def test_compose_is_the_pinned_image_owner(self):
        self.assertEqual(CONFIG.image(), 'starrocks/allin1-ubuntu:3.5.21@sha256:faf5f6b17a301b276b829482af11d350004833dc9162232e1083a2aad0fa8b2a')
        result = subprocess.run(['docker', 'compose', '--env-file', str(ROOT / 'business/.env.example'), '-f', str(ROOT / 'business/docker-compose.yml'), '--profile', 'starrocks', 'config', '--format', 'json'], env=dict(os.environ, STARROCKS_PORT='9031', STARROCKS_HTTP_PORT='8031', STARROCKS_BE_HTTP_PORT='8041'), capture_output=True, text=True, check=True)
        value = json.loads(result.stdout)['services']['starrocks']
        self.assertEqual(value['image'], CONFIG.image())
        self.assertEqual(value['hostname'], 'business-starrocks')
        self.assertEqual(value['profiles'], ['starrocks'])
        self.assertEqual({(p['host_ip'], int(p['published']), p['target']) for p in value['ports']}, {('127.0.0.1',9031,9030),('127.0.0.1',8031,8030),('127.0.0.1',8041,8040)})
        self.assertEqual({v['source'] for v in value['volumes'] if v['type']=='volume'}, {'starrocks_fe_data','starrocks_be_data'})

    def test_configuration_rejects_root_weak_and_shared_passwords_without_disclosure(self):
        for env in ({'STARROCKS_USER':'root'}, {'STARROCKS_PASSWORD':''}, {'STARROCKS_APP_PASSWORD':'Admin1234@abcdef'}, {'STARROCKS_USER':'bad;name'}):
            result = self.shell('validate_config', env=env)
            self.assertNotEqual(result.returncode, 0)
            self.assertNotIn('Admin1234@abcdef', result.stdout + result.stderr)
        self.assertEqual(self.shell('validate_config').returncode, 0)

    def test_backend_health_uses_native_header_not_column_positions(self):
        for text, passed in [('Host\tAlive\tBackendId\n127.0.0.1\ttrue\t1001\n',True), ('Alive\tHost\nfalse\t127.0.0.1\n',False), ('Alive\tHost\n',False), ('Alive\tHost\ntrue\ta\ntrue\tb\n',False)]:
            result = self.shell('backend_output="$2"; mysql() { printf "%s" "$backend_output"; }; backend_ready', text)
            self.assertEqual(result.returncode == 0, passed, result.stderr)

    def test_existing_unmarked_metadata_is_never_reinitialized(self):
        with tempfile.TemporaryDirectory() as tmp:
            meta = Path(tmp, 'fe/meta'); meta.mkdir(parents=True)
            existing = meta / 'existing-data'; existing.write_text('keep')
            result = self.shell('main', env={'SR_HOME':tmp})
            self.assertNotEqual(result.returncode, 0)
            self.assertIn('refusing authentication reset', result.stderr)
            self.assertEqual(existing.read_text(), 'keep')

    def test_explicit_selection_cannot_be_mixed(self):
        for script in ('start.sh','stop.sh'):
            result = subprocess.run(['bash',str(ROOT/'business/scripts'/script),'-starrocks','-postgres'], capture_output=True,text=True)
            self.assertNotEqual(result.returncode,0)
            self.assertIn('-starrocks 必须独立使用',result.stderr)

    def test_initialization_is_additive_and_does_not_rotate_existing_reader(self):
        sql = (ENTRY.parent/'init.sql').read_text()
        self.assertIn('WHERE NOT EXISTS', sql)
        for word in ('DROP ', 'TRUNCATE ', 'DELETE ', 'UPDATE '): self.assertNotIn(word, sql.upper())
        source = ENTRY.read_text()
        self.assertIn('CREATE USER IF NOT EXISTS', source)
        self.assertNotIn('ALTER USER', source)
        self.assertIn('supervisorctl', source)
        self.assertIn('/data/deploy/entrypoint.sh', source)

if __name__ == '__main__': unittest.main()

"""RustFS deployment contracts without running containers."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]


class RustFSDeploymentTest(unittest.TestCase):
    def test_profile_has_a_pinned_isolated_store_and_two_health_probes(self):
        result = subprocess.run(['docker', 'compose', '--env-file', '/dev/null', '-f',
                                 str(ROOT/'business/docker-compose.yml'), '--profile', 'rustfs',
                                 'config', '--format', 'json'], capture_output=True, text=True, check=True,
                                env={k:v for k,v in os.environ.items() if not k.startswith('RUSTFS_')})
        service = json.loads(result.stdout)['services']['rustfs']
        self.assertEqual(service['image'], 'rustfs/rustfs:1.0.5@sha256:b81d16a90ee25bd9511280127ca1471788c6e874cf0f0da759645f46b2dc1d74')
        self.assertEqual(service['profiles'], ['rustfs'])
        self.assertEqual(service['volumes'][0]['source'], 'rustfs_data')
        self.assertEqual({(p['host_ip'], p['published'], p['target']) for p in service['ports']},
                         {('127.0.0.1', '9004', 9000), ('127.0.0.1', '9005', 9001)})
        self.assertNotIn('user', service)  # Keep the official non-root runtime.
        self.assertIsNone(service.get('entrypoint'))
        self.assertIn('/health', str(service['healthcheck']))
        self.assertIn('/rustfs/console/health', str(service['healthcheck']))
        default = subprocess.run(['docker','compose','--env-file','/dev/null','-f',str(ROOT/'business/docker-compose.yml'),
                                  'config','--services'], capture_output=True,text=True,check=True)
        self.assertNotIn('rustfs', default.stdout.splitlines())

    def test_explicit_selection_is_unambiguous(self):
        for script in ('start', 'stop', 'restart'):
            for args in (['-rustfs', '-postgres'], ['-all', '-rustfs']):
                with self.subTest(script=script, args=args):
                    result = subprocess.run(['bash', str(ROOT/f'business/scripts/{script}.sh'), *args],
                                            capture_output=True, text=True)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn('-rustfs 必须独立使用', result.stdout+result.stderr)

    def test_invalid_configuration_and_foreign_ownership_never_mutate_docker(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root/'scripts').mkdir()
            shutil.copy(ROOT/'business/scripts/rustfs.sh', root/'scripts/rustfs.sh')
            (root/'scripts/ports.sh').write_text('addp_business_network_override(){ echo /nonexistent; }\n'
                                                'addp_business_ensure_network(){ :; }\n')
            docker = root/'docker'
            docker.write_text('#!/bin/bash\n'
                              'if [ "$1 $2" = "container inspect" ]; then [ "${FOREIGN:-}" = 1 ]; exit; fi\n'
                              'if [ "$1" = inspect ]; then echo "business|rustfs|/foreign"; exit; fi\n'
                              'echo "$*" >> "$TRACE"\nexit "${COMPOSE_STATUS:-0}"\n')
            docker.chmod(0o755)
            base = dict(os.environ, PATH=str(root)+os.pathsep+os.environ['PATH'], TRACE=str(root/'trace'),
                        RUSTFS_ACCESS_KEY='independent-ak', RUSTFS_SECRET_KEY='independent-secret-123',
                        RUSTFS_API_PORT='9004', RUSTFS_CONSOLE_PORT='9005')
            for override in ({'RUSTFS_SECRET_KEY':''}, {'RUSTFS_ACCESS_KEY':'rustfsadmin'},
                             {'RUSTFS_API_PORT':'09004'}, {'RUSTFS_API_PORT':'65536'},
                             {'RUSTFS_CONSOLE_PORT':'9004'}, {'FOREIGN':'1'}):
                with self.subTest(override=override):
                    result = subprocess.run(['bash',str(root/'scripts/rustfs.sh'),'start'],
                                            env=dict(base, **override),capture_output=True,text=True)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertFalse((root/'trace').exists())
                    self.assertNotIn(base['RUSTFS_SECRET_KEY'], result.stdout+result.stderr)
            result = subprocess.run(['bash',str(root/'scripts/rustfs.sh'),'start'],
                                    env=dict(base, COMPOSE_STATUS='17'),capture_output=True,text=True)
            self.assertEqual(result.returncode,17)
            self.assertNotIn('RustFS API:',result.stdout)
            self.assertIn('up -d --wait --wait-timeout 150 rustfs',(root/'trace').read_text())


if __name__ == '__main__':
    unittest.main()

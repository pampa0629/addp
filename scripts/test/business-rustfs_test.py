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

    def test_disposable_gate_refreshes_endpoint_and_retains_volume_after_recreation(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            docker = root/'docker'
            docker.write_text(r'''#!/bin/bash
set -eu
case "$1 $2" in
    'compose --env-file')
        test "${8-} ${9-} ${10-}" = 'config --format json'
        exec "$COMPOSE_DOCKER" compose --env-file /dev/null --file "$5" --profile rustfs config --format json
        ;;
    'compose --project-name')
        case "$6" in
            up)
                cp "$5" "$STATE.compose.json"
                echo "$(( $(cat "$STATE") + 1 ))" > "$STATE" ;;
            port) echo "127.0.0.1:$((32767 + $(cat "$STATE")))" ;;
            ps) echo "container-$(cat "$STATE")" ;;
            rm|down) ;;
            *) exit 2 ;;
        esac
        ;;
    'inspect -f') echo "${RUSTFS_T2_PROJECT}_rustfs_data" ;;
    'container ls'|'volume ls'|'network ls') ;;
    *) exit 2 ;;
esac
''')
            go = root/'go'
            go.write_text(r'''#!/bin/bash
set -eu
case "$PWD/$2" in
    */infra/objectstore/s3) contract=TestPlatformAreaAgainstRustFS ;;
    */common/-p)
        for name in TestManagerObjectAreaAgainstRustFS TestExportAreaAgainstRustFS/manager TestExportAreaAgainstRustFS/develop TestExportAreaAgainstRustFS/shapefile_zip; do
            printf -- '--- PASS: %s (0.00s)\n' "$name"
        done
        exit 0 ;;
    */standard/backend/./internal/service) contract=TestDocumentFileLifecycleAgainstPostgresAndS3 ;;
    */engine/plugins/rustfs)
        case "$ADDP_RUSTFS_TEST_PHASE" in
            initial) test "$ADDP_RUSTFS_TEST_ENDPOINT" = 127.0.0.1:32768 ;;
            persisted) test "$ADDP_RUSTFS_TEST_ENDPOINT" = 127.0.0.1:32769 ;;
            *) exit 2 ;;
        esac
        contract=TestIntegrationRustFSProvider ;;
    *)
        test "$ADDP_RUSTFS_TEST_ENDPOINT" = 127.0.0.1:32769
        case "$PWD/$2" in
            */system/backend/./internal/service) contract=TestIntegrationRustFSSystemConnection ;;
            */meta/backend/./internal/service) contract=TestIntegrationRustFSDeepScan ;;
            */manager/backend/./internal/preview) contract=TestIntegrationRustFSPreview ;;
            */manager/backend/./internal/api) contract=TestIntegrationRustFSAuthorizedPreview ;;
            */transfer/backend/./internal/planner) contract=TestIntegrationRustFSParquetImport ;;
            *) exit 2 ;;
        esac
        ;;
esac
printf -- '--- PASS: %s (0.00s)\n' "$contract"
''')
            for executable in (docker, go):
                executable.chmod(0o755)
            state = root/'state'
            state.write_text('0')
            env = dict(os.environ, PATH=str(root)+os.pathsep+os.environ['PATH'], STATE=str(state),
                       COMPOSE_DOCKER=shutil.which('docker'),
                       GITHUB_ACTIONS='true', ADDP_TEST_POSTGRES_PORT='5432',
                       ADDP_TEST_POSTGRES_PASSWORD='fixture')
            run = subprocess.run(['bash', str(ROOT/'scripts/test/common-rustfs-gate.sh')],
                                 env=env, capture_output=True, text=True, timeout=30)
            self.assertEqual(run.returncode, 0, run.stdout+run.stderr)
            service = json.loads((root/'state.compose.json').read_text())['services']['rustfs']
            self.assertEqual(service['environment']['RUSTFS_OBS_LOGGER_LEVEL'], 'warn,rustfs::app::object::get=debug')
            self.assertEqual(service['environment']['RUSTFS_OBS_LOG_STDOUT_ENABLED'], 'true')
            self.assertIn('container=container-1 endpoint=127.0.0.1:32768', run.stdout)
            self.assertIn('container=container-2 endpoint=127.0.0.1:32769', run.stdout)
            volumes = [line.split('data_volume=')[1] for line in run.stdout.splitlines() if 'data_volume=' in line]
            self.assertEqual(len(volumes), 2)
            self.assertEqual(volumes[0], volumes[1])
            self.assertIn('RUSTFS_T2_PASS', run.stdout)

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

import importlib.util
import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location('graph_fixture', ROOT / 'business/scripts/online-graph-falkordb-fixture.py')
FIXTURE = importlib.util.module_from_spec(SPEC); SPEC.loader.exec_module(FIXTURE)


class GraphFixtureTest(unittest.TestCase):
    def test_admission_refuses_developer_machine_before_docker(self):
        with patch.dict(os.environ, {'GITHUB_ACTIONS': 'false'}), patch.object(FIXTURE, 'command') as docker:
            with self.assertRaises(RuntimeError): FIXTURE.admission()
            docker.assert_not_called()

    def test_creates_only_two_synthetic_graphs_and_private_descriptor(self):
        queries = []
        def native(graph, query, readonly=True):
            queries.append((graph, query, readonly))
            return [[], [[3 if 'count(n)' in query else 2]], []]
        with tempfile.TemporaryDirectory() as directory, patch.object(FIXTURE, 'owned_resources', return_value=[]), \
             patch.object(FIXTURE, 'compose') as compose, patch.object(FIXTURE, 'command', return_value='null'), \
             patch.object(FIXTURE, 'native', side_effect=native):
            FIXTURE.start(Path(directory))
            path = Path(directory) / 'graph-falkordb.json'
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            data = json.loads(path.read_text())
            self.assertEqual(data['engine_type'], 'falkordb')
            self.assertEqual(data['connection_info']['host'], FIXTURE.CONTAINER)
            self.assertEqual(data['connection_info']['port'], 6379)
            self.assertIs(type(data['connection_info']['database']), int)
            self.assertEqual(data['connection_info']['database'], 0)
            self.assertEqual(len(data['connection_info']['password']), 64)
            self.assertEqual(len([q for q in queries if not q[2]]), 2)
            self.assertEqual({q[0] for q in queries}, set(FIXTURE.GRAPHS))
            self.assertIn('Fixture one', queries[0][1])
            compose.assert_called_once()

    def test_existing_resources_and_wrong_binding_stop_before_seeding(self):
        with tempfile.TemporaryDirectory() as directory:
            with patch.object(FIXTURE, 'owned_resources', return_value=['existing']), patch.object(FIXTURE, 'compose') as compose:
                with self.assertRaises(RuntimeError): FIXTURE.start(Path(directory))
                compose.assert_not_called()
            with patch.object(FIXTURE, 'owned_resources', return_value=[]), patch.object(FIXTURE, 'compose'), \
                 patch.object(FIXTURE, 'command', return_value='{"6379/tcp":[{"HostIp":"0.0.0.0"}]}'), patch.object(FIXTURE, 'native') as native:
                with self.assertRaises(RuntimeError): FIXTURE.start(Path(directory))
                native.assert_not_called()

    def test_cleanup_refuses_foreign_container_and_detects_residuals(self):
        with tempfile.TemporaryDirectory() as directory:
            with patch.object(FIXTURE, 'command', side_effect=[FIXTURE.CONTAINER, 'foreign']), patch.object(FIXTURE, 'compose') as compose:
                with self.assertRaises(RuntimeError): FIXTURE.stop(Path(directory))
                compose.assert_not_called()
            with patch.object(FIXTURE, 'command', return_value=''), patch.object(FIXTURE, 'compose') as compose, \
                 patch.object(FIXTURE, 'owned_resources', return_value=['residual-volume']):
                with self.assertRaises(RuntimeError): FIXTURE.stop(Path(directory))
                self.assertIn('--volumes', compose.call_args.args[0])

    def test_owned_compose_uses_central_image_and_tmpfs(self):
        content = (ROOT / 'business/docker-compose.online-graph-falkordb.yml').read_text()
        self.assertIn('../scripts/infra/falkordb.yml', content)
        self.assertNotIn('ports:', content)
        self.assertIn('external: true', content)
        self.assertIn('name: addp-network', content)
        self.assertIn('tmpfs:', content)
        self.assertNotIn('image:', content)


if __name__ == '__main__': unittest.main()

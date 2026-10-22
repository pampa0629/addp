import importlib.util
import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location('graph_fixture', ROOT / 'business/scripts/online-graph-fixture.py')
FIXTURE = importlib.util.module_from_spec(SPEC); SPEC.loader.exec_module(FIXTURE)


class GraphFixtureTest(unittest.TestCase):
    def setUp(self):
        environment = patch.dict(os.environ, {'ADDP_ONLINE_GRAPH_ENGINE': 'falkordb'})
        environment.start()
        self.addCleanup(environment.stop)

    def test_admission_refuses_developer_machine_before_docker(self):
        with patch.dict(os.environ, {'GITHUB_ACTIONS': 'false'}), patch.object(FIXTURE, 'command') as docker:
            with self.assertRaises(RuntimeError): FIXTURE.admission()
            docker.assert_not_called()

    def test_keeps_two_protection_graphs_and_independent_review_graph(self):
        queries = []
        def native(graph, query, readonly=True):
            queries.append((graph, query, readonly))
            return (2 if graph == 'graph_review_fixture' else 3) if 'count(n)' in query else (0 if graph == 'graph_review_fixture' else 2)
        with tempfile.TemporaryDirectory() as directory, patch.object(FIXTURE, 'owned_resources', return_value=[]), \
             patch.object(FIXTURE, 'compose') as compose, patch.object(FIXTURE, 'command', return_value='null'), \
             patch.object(FIXTURE, 'native', side_effect=native):
            FIXTURE.start(Path(directory))
            path = Path(directory) / 'graph-target.json'
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            data = json.loads(path.read_text())
            self.assertEqual(data['engine_type'], 'falkordb')
            self.assertEqual(data['connection_info']['host'], FIXTURE.CONTAINER)
            self.assertEqual(data['connection_info']['port'], 6379)
            self.assertIs(type(data['connection_info']['database']), int)
            self.assertEqual(data['connection_info']['database'], 0)
            self.assertEqual(len(data['connection_info']['password']), 64)
            self.assertEqual(len([q for q in queries if not q[2]]), 3)
            self.assertEqual({q[0] for q in queries}, set(FIXTURE.GRAPHS))
            self.assertIn('Fixture one', queries[0][1])
            compose.assert_called_once()

    def test_neo4j_uses_three_instances_private_descriptors_and_exact_counts(self):
        with tempfile.TemporaryDirectory() as directory, patch.dict(os.environ, {'ADDP_ONLINE_GRAPH_ENGINE': 'neo4j'}), \
             patch.object(FIXTURE, 'owned_resources', return_value=[]), patch.object(FIXTURE, 'compose') as compose, \
             patch.object(FIXTURE, 'command', return_value='null'), \
             patch.object(FIXTURE, 'native', side_effect=lambda graph, query, readonly=True: (2 if graph == 'graph_review_fixture' else 3) if 'count(n)' in query else (0 if graph == 'graph_review_fixture' else 2)) as native:
            FIXTURE.start(Path(directory))
            descriptors = [Path(directory) / ('graph-' + role + '.json') for role in ('target', 'control', 'review')]
            data = [json.loads(path.read_text()) for path in descriptors]
            self.assertEqual([item['connection_info']['host'] for item in data], FIXTURE.containers())
            self.assertEqual(len({item['connection_info']['host'] for item in data}), 3)
            for path, item in zip(descriptors, data):
                self.assertEqual(path.stat().st_mode & 0o777, 0o600)
                self.assertEqual(item['engine_type'], 'neo4j')
                self.assertEqual(item['connection_info']['database'], 'neo4j')
                self.assertEqual(item['connection_info']['port'], 7687)
            self.assertEqual(native.call_count, 9)
            compose.assert_called_once()

    def test_native_neo4j_counts_use_actual_shell_output_and_fail_closed(self):
        with patch.dict(os.environ, {'ADDP_ONLINE_GRAPH_ENGINE': 'neo4j'}):
            with patch.object(FIXTURE, 'command', return_value='count(n)\n3') as command:
                self.assertEqual(FIXTURE.native(FIXTURE.GRAPHS[0], 'MATCH (n) RETURN count(n)'), 3)
                self.assertIn(FIXTURE.containers()[0], command.call_args.args[0])
                self.assertEqual(command.call_args.kwargs['stdin'], 'MATCH (n) RETURN count(n);\n')
            with patch.object(FIXTURE, 'command', return_value='count(n)\nunknown'):
                with self.assertRaises(ValueError): FIXTURE.native(FIXTURE.GRAPHS[1], 'MATCH (n) RETURN count(n)')

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
        content = (ROOT / 'business/docker-compose.online-graph.yml').read_text()
        self.assertIn('../scripts/infra/falkordb.yml', content)
        self.assertIn('ports: !reset []', content)
        self.assertIn('../scripts/test/docker-compose.neo4j-t2.yml', content)
        self.assertIn('external: true', content)
        self.assertIn('name: addp-network', content)
        self.assertIn('tmpfs:', content)
        self.assertNotIn('image:', content)


    def test_dense_phase_reuses_only_owned_original_control_and_checks_counts(self):
        for engine in ('falkordb', 'neo4j'):
            with self.subTest(engine=engine), tempfile.TemporaryDirectory() as directory, patch.dict(os.environ, {'ADDP_ONLINE_GRAPH_ENGINE': engine}):
                folder = Path(directory)
                host = FIXTURE.containers()[1 if engine == 'neo4j' else 0]
                (folder / ('graph-control.json' if engine == 'neo4j' else 'graph-target.json')).write_text(json.dumps({'engine_type': engine, 'connection_info': {'host': host}}))
                with patch.object(FIXTURE, 'command', return_value=FIXTURE.PROJECT), patch.object(FIXTURE, 'native', side_effect=[3, 2, 3, None, None, 12, 72]) as native:
                    FIXTURE.exploration(folder)
                    self.assertTrue(all(call.args[0] == FIXTURE.GRAPHS[1] for call in native.call_args_list))
                    writes = [call for call in native.call_args_list if len(call.args) > 2 and call.args[2] is False]
                    self.assertEqual(len(writes), 2)
                    self.assertEqual(writes[0].args[1], 'MATCH (n) DETACH DELETE n')
                    self.assertIn(':Employee:Person', writes[1].args[1])
                    self.assertIn('(n12)-[:Link]->(n12)', writes[1].args[1])
                    self.assertIn('(n01)-[:Parallel]->(n02)', writes[1].args[1])
                with patch.object(FIXTURE, 'command', return_value='foreign'), patch.object(FIXTURE, 'native') as native:
                    with self.assertRaisesRegex(RuntimeError, 'ownership'): FIXTURE.exploration(folder)
                    native.assert_not_called()
                with patch.object(FIXTURE, 'command', return_value=FIXTURE.PROJECT), patch.object(FIXTURE, 'native', return_value=4) as native:
                    with self.assertRaisesRegex(RuntimeError, 'changed control'): FIXTURE.exploration(folder)
                    self.assertTrue(all(len(call.args) == 2 for call in native.call_args_list))


if __name__ == '__main__': unittest.main()

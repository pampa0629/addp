import unittest
from unittest.mock import MagicMock, patch

import doris_publication as publication
from mysql_publication import PublicationUncertain


class DorisPublicationTest(unittest.TestCase):
    def setUp(self):
        self.frame = MagicMock()
        self.frame.columns = ['id', 'value']
        self.frame.is_cached = True
        self.frame.count.return_value = 2
        self.conn = {'engine_type': 'doris', 'host': 'fe.example', 'http_port': 18030, 'database': 'results'}
        self.adapter = MagicMock()
        self.adapter._geometry_column_names.return_value = []
        self.adapter._doris_create_table_sql.return_value = 'CREATE TABLE owned_stage (id BIGINT)'
        self.connection = self.adapter._open_jdbc_connection.return_value
        self.connection.createStatement.return_value.getLargeUpdateCount.return_value = 2
        self.params = {'schema': 'results', 'table': 'target', 'mode': 'overwrite'}

    def test_requires_explicit_http_deployment_endpoint(self):
        self.assertEqual(publication.http_endpoint(self.conn), 'fe.example:18030')
        self.assertEqual(publication.http_endpoint(dict(self.conn, host='::1')), '[::1]:18030')
        for port in (None, '8030', True, 0, 65536):
            with self.subTest(port=port), self.assertRaises(ValueError):
                publication.http_endpoint(dict(self.conn, http_port=port))

    def test_connector_is_strict_two_phase_and_only_appends_to_stage(self):
        publication.write_stage(self.frame, self.conn, 'results', 'owned_stage', 'owned_label')
        self.frame.write.format.assert_called_once_with('doris')
        options = self.frame.write.format.return_value.options.call_args.kwargs
        self.assertEqual(options['doris.fenodes'], 'fe.example:18030')
        self.assertEqual(options['doris.sink.enable-2pc'], 'true')
        self.assertEqual(options['doris.sink.max-retries'], '0')
        self.assertEqual(options['doris.sink.properties.max_filter_ratio'], '0')
        self.assertEqual(options['doris.table.identifier'], 'results.owned_stage')
        self.frame.write.format.return_value.options.return_value.mode.assert_called_once_with('append')

    def test_waits_for_complete_stage_visibility_and_rejects_excess_rows(self):
        with patch.object(publication, 'query', side_effect=[[('0',)], [('2',)]]), \
                patch.object(publication.time, 'sleep'):
            self.assertEqual(publication.wait_stage(self.connection, '`db`.`stage`', 2), 2)
        with patch.object(publication, 'query', return_value=[('3',)]), self.assertRaises(ValueError):
            publication.wait_stage(self.connection, '`db`.`stage`', 2)

    def test_overwrite_publishes_only_after_stage_and_waits_for_load(self):
        order = []
        with patch.object(publication, 'validate_target', return_value=True), \
                patch.object(publication, 'write_stage', side_effect=lambda *args: order.append('stage')), \
                patch.object(publication, 'wait_stage', side_effect=lambda *args: order.append('visible') or 2), \
                patch.object(publication, 'wait_publication', side_effect=lambda *args: order.append('published')):
            self.assertEqual(publication.save(self.frame, self.params, 'url', 'driver', self.conn, self.adapter), 2)
        self.assertEqual(order, ['stage', 'visible', 'published'])
        commands = [call.args[0] for call in self.connection.createStatement.return_value.execute.call_args_list]
        self.assertTrue(any(command.startswith('INSERT OVERWRITE TABLE `results`.`target` WITH LABEL') for command in commands))
        self.assertFalse(any(command.startswith('DROP TABLE') and 'stage_' not in command for command in commands))
        self.frame.unpersist.assert_not_called()  # A caller-owned cache must survive.

    def test_stage_failure_never_modifies_target(self):
        with patch.object(publication, 'validate_target', return_value=True), \
                patch.object(publication, 'write_stage', side_effect=OSError('Worker stopped')):
            with self.assertRaises(OSError):
                publication.save(self.frame, self.params, 'url', 'driver', self.conn, self.adapter)
        commands = [call.args[0] for call in self.connection.createStatement.return_value.execute.call_args_list]
        self.assertFalse(any(command.startswith('INSERT') or command.startswith('ALTER') for command in commands))

    def test_staging_table_matches_doris_native_identifier_grammar(self):
        with patch.object(publication, 'validate_target', return_value=True), \
                patch.object(publication, 'write_stage') as write, \
                patch.object(publication, 'wait_stage', return_value=2), \
                patch.object(publication, 'wait_publication'):
            publication.save(self.frame, self.params, 'url', 'driver', self.conn, self.adapter)
        stage = write.call_args.args[3]
        self.assertRegex(stage, r'^[a-zA-Z][a-zA-Z0-9_-]*$')
        self.assertNotEqual(stage, self.params['table'])
        self.adapter._doris_create_table_sql.assert_called_once_with(self.frame, 'results', stage)

    def test_unconfirmed_publication_is_not_replayed_or_cleaned(self):
        with patch.object(publication, 'validate_target', return_value=True), \
                patch.object(publication, 'write_stage'), patch.object(publication, 'wait_stage', return_value=2), \
                patch.object(publication, 'wait_publication', side_effect=PublicationUncertain('not visible')) as wait:
            with self.assertRaises(PublicationUncertain):
                publication.save(self.frame, self.params, 'url', 'driver', self.conn, self.adapter)
        wait.assert_called_once()
        commands = [call.args[0] for call in self.connection.createStatement.return_value.execute.call_args_list]
        self.assertFalse(any(command.startswith('DROP') for command in commands))


if __name__ == '__main__':
    unittest.main()

import unittest
from unittest.mock import MagicMock, patch
from types import SimpleNamespace

import mysql_publication as publication


class MySQLPublicationTest(unittest.TestCase):
    def test_stage_types_preserve_microseconds_and_explicit_innodb(self):
        frame = SimpleNamespace(schema=SimpleNamespace(fields=[
            SimpleNamespace(dataType=SimpleNamespace(typeName=lambda: 'timestamp'), nullable=False),
            SimpleNamespace(dataType=SimpleNamespace(typeName=lambda: 'timestamp_ntz'), nullable=True),
        ]))
        ddl = publication.stage_definition(frame, 'db', 'stage', ['a', 'b'])
        self.assertIn('`a` TIMESTAMP(6) NOT NULL', ddl)
        self.assertIn('`b` DATETIME(6)', ddl)
        self.assertIn('ENGINE=InnoDB DEFAULT CHARSET=utf8mb4', ddl)

    def test_temporal_precision_mismatch_is_rejected(self):
        stage = [('at', 'timestamp', 'timestamp(6)', '', None, None, '6')]
        target = [('at', 'timestamp', 'timestamp', '', None, None, '0')]
        with self.assertRaisesRegex(ValueError, 'type differs'):
            publication.validate_columns(stage, target)

    def test_column_validation_preserves_numeric_and_decimal_types(self):
        bigint = ('id', 'bigint', 'bigint', '', '19', '0', None)
        decimal = ('value', 'decimal', 'decimal(38,18)', '', '38', '18', None)
        publication.validate_columns([bigint, decimal], [decimal, bigint])
        for changed in (
            [bigint], [bigint, ('value', 'decimal', 'decimal(38,10)', '', '38', '10', None)],
            [('id', 'bigint', 'bigint unsigned', '', '20', '0', None), decimal],
            [('id', 'bigint', 'bigint', 'auto_increment', '19', '0', None), decimal],
            [('id', 'bigint', 'bigint', 'STORED GENERATED', '19', '0', None), decimal],
        ):
            with self.subTest(changed=changed), self.assertRaises(ValueError):
                publication.validate_columns([bigint, decimal], changed)

    def test_names_are_checked_and_identifiers_are_quoted(self):
        publication.validate_names('业务', 'result.table', ['a`b', 'id'])
        self.assertEqual(publication.table_ref('业务', 'a`b'), '`业务`.`a``b`')
        for columns in ([], ['id', 'ID'], [''], ['a\x00b'], ['x' * 65]):
            with self.subTest(columns=columns), self.assertRaises(ValueError):
                publication.validate_names('results', 'table', columns)

    def test_overwrite_is_one_data_transaction_and_does_not_drop_target(self):
        connection = MagicMock()
        statement = connection.createStatement.return_value
        statement.executeLargeUpdate.side_effect = [3, 2]
        with patch.object(publication, 'validate_target', return_value=True), \
                patch.object(publication, 'query', side_effect=[[('1',)], [('2',)], [('3',)], [('2',)], [('1',)]]):
            self.assertEqual(publication.finalize(connection, 'results', 'stage', 'target', ['id'], 'overwrite'), 2)
        connection.setTransactionIsolation.assert_called_once_with(8)
        connection.commit.assert_called_once()
        connection.rollback.assert_not_called()
        statements = [call.args[0] for call in statement.executeLargeUpdate.call_args_list]
        self.assertEqual(statements, ['DELETE FROM `results`.`target`',
                                     'INSERT INTO `results`.`target` (`id`) SELECT `id` FROM `results`.`stage`'])

    def test_append_does_not_delete_and_count_mismatch_rolls_back(self):
        for inserted in (2, 1):
            connection = MagicMock()
            connection.createStatement.return_value.executeLargeUpdate.return_value = inserted
            answers = [[('1',)], [('2',)], [('3',)], [('5',)], [('1',)]]
            if inserted == 1:
                answers.pop(3)
            with self.subTest(inserted=inserted), patch.object(publication, 'validate_target', return_value=True), \
                    patch.object(publication, 'query', side_effect=answers):
                if inserted == 2:
                    self.assertEqual(publication.finalize(connection, 'db', 'stage', 'target', ['id'], 'append'), 2)
                    connection.commit.assert_called_once()
                else:
                    with self.assertRaisesRegex(ValueError, 'row count'):
                        publication.finalize(connection, 'db', 'stage', 'target', ['id'], 'append')
                    connection.rollback.assert_called_once()
                    connection.commit.assert_not_called()
                self.assertFalse(any('DELETE' in call.args[0] for call in connection.createStatement.return_value.executeLargeUpdate.call_args_list))

    def test_commit_response_loss_is_uncertain_not_reported_as_rollback(self):
        connection = MagicMock()
        connection.createStatement.return_value.executeLargeUpdate.return_value = 2
        connection.commit.side_effect = OSError('connection lost')
        with patch.object(publication, 'validate_target', return_value=True), \
                patch.object(publication, 'query', side_effect=[[('1',)], [('2',)], [('0',)], [('2',)], [('1',)]]):
            with self.assertRaises(publication.PublicationUncertain):
                publication.finalize(connection, 'db', 'stage', 'target', ['id'], 'append')
        connection.commit.assert_called_once()
        connection.rollback.assert_not_called()

    def test_new_target_only_renames_owned_stage(self):
        connection = MagicMock()
        with patch.object(publication, 'validate_target', return_value=False), \
                patch.object(publication, 'query', side_effect=[[('1',)], [('2',)], [('1',)]]):
            self.assertEqual(publication.finalize(connection, 'db', 'stage', 'target', ['id'], 'overwrite'), 2)
        connection.createStatement.return_value.execute.assert_any_call('RENAME TABLE `db`.`stage` TO `db`.`target`')
        connection.commit.assert_not_called()

    def test_pretransaction_rejection_never_rolls_back_autocommit_connection(self):
        connection = MagicMock()
        with patch.object(publication, 'validate_target', side_effect=ValueError('target fields')), \
                patch.object(publication, 'query', side_effect=[[('1',)], [('2',)], [('1',)]]):
            with self.assertRaisesRegex(ValueError, 'target fields'):
                publication.finalize(connection, 'db', 'stage', 'target', ['id'], 'overwrite')
        connection.rollback.assert_not_called()
        connection.commit.assert_not_called()


if __name__ == '__main__':
    unittest.main()
